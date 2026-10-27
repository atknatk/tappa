package adminauth

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// identity_shape_test.go -- the EXACT half of the identity sign-in's flat timing (ADR 0031 §5.6,
// R14, S15; M14-07). The clock-based half is identity_timing_db_test.go; this file needs no clock:
// it counts the bcrypt comparisons of every exit through Manager.compareFn and records every call
// the sign-in makes to its database, and requires the five failure cases to be ONE sequence.

// shapeDB is a Database whose identity half is scripted and whose every call is recorded.
type shapeDB struct {
	mu    sync.Mutex
	ops   []string
	ident *db.ResolvedIdentity
	rows  []db.ResolvedAdmin
	// openErr, when set, is what OpenIdentitySession answers.
	openErr error
	// opened records the digest OpenIdentitySession was handed.
	opened []string
	events []string
}

func (s *shapeDB) op(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, name)
}

func (s *shapeDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	s.op("with-tenant")
	return errors.New("shapeDB: no tenant transactions in this test")
}

func (s *shapeDB) GetAdminByEmail(context.Context, string) ([]db.ResolvedAdmin, error) {
	s.op("resolve-admins")
	return s.rows, nil
}

func (s *shapeDB) GetAdminSessionByTokenHash(context.Context, string) (db.ResolvedAdminSession, error) {
	return db.ResolvedAdminSession{}, pgx.ErrNoRows
}

func (s *shapeDB) ResolveIdentityByEmail(context.Context, string) (db.ResolvedIdentity, error) {
	s.op("resolve-identity")
	if s.ident == nil {
		return db.ResolvedIdentity{}, fmt.Errorf("fake: %w", pgx.ErrNoRows)
	}
	return *s.ident, nil
}

func (s *shapeDB) OpenIdentitySession(_ context.Context, identityID, _ uuid.UUID, _ string, verified db.PasswordHash) (db.IdentitySession, error) {
	s.op("open")
	s.mu.Lock()
	s.opened = append(s.opened, verified.RevealForPasswordComparison())
	s.mu.Unlock()
	if s.openErr != nil {
		return db.IdentitySession{}, s.openErr
	}
	return db.IdentitySession{SessionID: uuid.New(), TenantID: uuid.New(), AdminUserID: identityID}, nil
}

func (s *shapeDB) IdentitySessionDecoy(context.Context) error {
	s.op("open")
	return nil
}

func (s *shapeDB) LinkLegacySession(context.Context, string, string) (db.LinkedMembership, error) {
	s.op("link")
	return db.LinkedMembership{}, pgx.ErrNoRows
}

func (s *shapeDB) RecordFailedIdentityLogin(_ context.Context, id uuid.UUID, event string) (bool, error) {
	s.op("record")
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == uuid.Nil {
		s.events = append(s.events, "nobody")
		return false, nil
	}
	s.events = append(s.events, event)
	return true, nil
}

// The session-keyed four (M14-08) are not the sign-in's: AuthenticatePerson never reaches them.
func (s *shapeDB) SwitchIdentitySession(context.Context, string, uuid.UUID, string) (db.IdentitySession, error) {
	s.op("switch")
	return db.IdentitySession{}, errNoIdentityData
}

func (s *shapeDB) IdentityMemberships(context.Context, string) ([]db.IdentityMembership, error) {
	s.op("memberships")
	return nil, errNoIdentityData
}

func (s *shapeDB) RevokeIdentitySessions(context.Context, string, bool) (int, error) {
	s.op("revoke-all")
	return 0, errNoIdentityData
}

func (s *shapeDB) IdentityPasswordForSession(context.Context, string) (db.PasswordHash, error) {
	s.op("password-for-session")
	return db.PasswordHash{}, errNoIdentityData
}

func (s *shapeDB) SetIdentityPassword(context.Context, string, string) (int, error) {
	s.op("set-password")
	return 0, errNoIdentityData
}

// The account page's six (M14-18) are not the sign-in's either.
func (s *shapeDB) RequestIdentityVerification(context.Context, string, string) (db.IdentityVerification, error) {
	s.op("request-verification")
	return db.IdentityVerification{}, errNoIdentityData
}

func (s *shapeDB) VerifyIdentityEmail(context.Context, string, string) (bool, error) {
	s.op("verify-email")
	return false, errNoIdentityData
}

func (s *shapeDB) ConfirmWindow(context.Context, string) ([]db.ConfirmWindowRow, error) {
	s.op("confirm-window")
	return nil, errNoIdentityData
}

func (s *shapeDB) KeepAll(context.Context, string) error {
	s.op("keep-all")
	return errNoIdentityData
}

func (s *shapeDB) UnlinkMembership(context.Context, string, uuid.UUID) (db.UnlinkedMembership, error) {
	s.op("unlink")
	return db.UnlinkedMembership{}, errNoIdentityData
}

func (s *shapeDB) RelinkMembership(context.Context, string, uuid.UUID) (uuid.UUID, error) {
	s.op("relink")
	return uuid.Nil, errNoIdentityData
}

// countingBudget is a FailureBudget with a small limit, so the over-limit branch is reachable; it is
// safe for concurrent use, like httpx.Limiter.
type countingBudget struct {
	mu    sync.Mutex
	limit int
	n     map[string]int
}

func newCountingBudget(limit int) *countingBudget {
	return &countingBudget{limit: limit, n: map[string]int{}}
}

func (b *countingBudget) TryCharge(key string) (bool, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.n[key]++
	return b.n[key] <= b.limit, b.n[key]
}

func (b *countingBudget) FirstOverLimit(count int) bool { return count == b.limit+1 }

// shapeManager is a Manager over d whose comparisons are counted (and cheap: the counter answers
// "matched" when the digest is the password itself, so no bcrypt runs at all).
func shapeManager(t *testing.T, d *shapeDB) (*Manager, *int) {
	t.Helper()
	m, err := New(d, testConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	n := 0
	m.compareFn = func(digest, password string) bool {
		n++
		return digest == "digest:"+password
	}
	m.dummy = "digest:the-dummy"
	return m, &n
}

// shapeCase is one sign-in this file runs.
type shapeCase struct {
	name     string
	ident    *db.ResolvedIdentity
	rows     []db.ResolvedAdmin
	email    string
	password string
}

func shapeCases() (failures []shapeCase, idID uuid.UUID) {
	idID = uuid.New()
	active := &db.ResolvedIdentity{ID: idID, PasswordHash: db.NewPasswordHash("digest:right"), HasActiveMembership: true}
	allDisabled := &db.ResolvedIdentity{ID: idID, PasswordHash: db.NewPasswordHash("digest:right"), HasActiveMembership: false}
	own := db.ResolvedAdmin{ID: uuid.New(), TenantID: uuid.New(), PasswordHash: db.NewPasswordHash("digest:right"), Status: "active", IdentityID: &idID}
	unlinked := db.ResolvedAdmin{ID: uuid.New(), TenantID: uuid.New(), PasswordHash: db.NewPasswordHash("digest:legacy"), Status: "active"}
	return []shapeCase{
		{name: "an identity, no unlinked row, wrong password", ident: active, rows: []db.ResolvedAdmin{own}, email: "p@example.test", password: "wrong"},
		{name: "an identity and an unlinked row, wrong password", ident: active, rows: []db.ResolvedAdmin{own, unlinked}, email: "p@example.test", password: "wrong"},
		{name: "no identity, an unlinked row, wrong password", rows: []db.ResolvedAdmin{unlinked}, email: "p@example.test", password: "wrong"},
		{name: "an unknown address", email: "nobody@example.test", password: "wrong"},
		{name: "the right password, every membership disabled", ident: allDisabled, rows: []db.ResolvedAdmin{own}, email: "p@example.test", password: "right"},
	}, idID
}

// TestAuthenticatePerson_EveryFailureIsOneShape is ADR 0031 §5.6's five cases, exactly: each pays
// 1 + MaxCandidates comparisons and makes the same calls in the same order -- the two resolvers, ONE
// open step (the decoy) and ONE event step -- and returns ErrBadCredentials and nothing else. The
// fifth case (the right password, every membership disabled) differs from the first only in what
// has_active_membership says, and that is read after the comparisons: same count, same calls.
//
// MUTATIONS (each turns this red): drop the decoy from any branch; drop the event step; skip the
// identity's comparison or the dummy that stands in for it; pad the legacy window to MaxCandidates - 1;
// branch on has_active_membership before the comparisons.
func TestAuthenticatePerson_EveryFailureIsOneShape(t *testing.T) {
	cases, _ := shapeCases()
	want := []string{"resolve-identity", "resolve-admins", "open", "record"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &shapeDB{ident: c.ident, rows: c.rows}
			m, n := shapeManager(t, d)
			in, err := m.AuthenticatePerson(context.Background(), c.email, c.password, uuid.Nil, newCountingBudget(10))
			if !errors.Is(err, ErrBadCredentials) || err != ErrBadCredentials {
				t.Fatalf("err = %v, want exactly ErrBadCredentials (one value for every failure)", err)
			}
			if in.Session != nil || len(in.Legacy.Verified()) != 0 {
				t.Fatalf("a failure carried a session or a verified row: %+v", in)
			}
			if *n != 1+MaxCandidates {
				t.Errorf("%d bcrypt comparisons, want 1 + MaxCandidates = %d", *n, 1+MaxCandidates)
			}
			if !slices.Equal(d.ops, want) {
				t.Errorf("calls = %v, want %v", d.ops, want)
			}
			if len(d.opened) != 0 {
				t.Errorf("identity_open_session was called on a failure (%d times); the decoy stands in for it", len(d.opened))
			}
		})
	}
}

// TestAuthenticatePerson_SuccessPaysTheSameComparisons: a proven identity with an active membership
// pays 1 + MaxCandidates too, opens through identity_open_session (and no decoy), records no failure,
// and hands the function the resolver's digest UNCHANGED (the user's decision of 2026-10-23).
func TestAuthenticatePerson_SuccessPaysTheSameComparisons(t *testing.T) {
	cases, idID := shapeCases()
	d := &shapeDB{ident: cases[0].ident, rows: cases[0].rows}
	m, n := shapeManager(t, d)
	in, err := m.AuthenticatePerson(context.Background(), "p@example.test", "right", uuid.Nil, newCountingBudget(10))
	if err != nil || in.Session == nil {
		t.Fatalf("AuthenticatePerson = %+v, %v; want an identity session", in, err)
	}
	if in.Session.Session.AdminUserID != idID {
		t.Fatalf("the session opened for %s, want the identity %s", in.Session.Session.AdminUserID, idID)
	}
	if *n != 1+MaxCandidates {
		t.Errorf("%d comparisons on success, want %d", *n, 1+MaxCandidates)
	}
	if want := []string{"resolve-identity", "resolve-admins", "open"}; !slices.Equal(d.ops, want) {
		t.Errorf("calls = %v, want %v", d.ops, want)
	}
	if len(d.opened) != 1 || d.opened[0] != "digest:right" {
		t.Errorf("identity_open_session got %v, want the resolver's digest unchanged", d.opened)
	}
	if len(in.Legacy.Attempts) != 0 || in.Linked != 0 {
		t.Errorf("an identity sign-in carried a legacy outcome: %+v", in)
	}
}

// TestAuthenticatePerson_ARefusedOpenIsNoSession: identity_open_session answering 0 rows (a reset
// committed under the person's lock, the last membership went) is a failure like any other -- one
// value, one event row -- and the open step is not paid twice.
func TestAuthenticatePerson_ARefusedOpenIsNoSession(t *testing.T) {
	cases, _ := shapeCases()
	d := &shapeDB{ident: cases[0].ident, rows: cases[0].rows, openErr: fmt.Errorf("db: open identity session: %w", pgx.ErrNoRows)}
	m, n := shapeManager(t, d)
	in, err := m.AuthenticatePerson(context.Background(), "p@example.test", "right", uuid.Nil, newCountingBudget(10))
	if err != ErrBadCredentials || in.Session != nil {
		t.Fatalf("AuthenticatePerson = %+v, %v; want ErrBadCredentials", in, err)
	}
	if want := []string{"resolve-identity", "resolve-admins", "open", "record"}; !slices.Equal(d.ops, want) || *n != 1+MaxCandidates {
		t.Errorf("calls = %v, %d comparisons; want %v and %d", d.ops, *n, want, 1+MaxCandidates)
	}
}

// TestAuthenticatePerson_TheLegacyArmComparesUnlinkedRowsOnly (T2) without a database: eight of the
// identity's own rows and one unlinked row -- the unlinked one is compared (and verifies), the
// identity's are not candidates. The eight-row window is the legacy arm's alone.
func TestAuthenticatePerson_TheLegacyArmComparesUnlinkedRowsOnly(t *testing.T) {
	idID := uuid.New()
	var rows []db.ResolvedAdmin
	for i := 0; i < MaxCandidates; i++ {
		rows = append(rows, db.ResolvedAdmin{ID: uuid.New(), TenantID: uuid.New(), PasswordHash: db.NewPasswordHash("digest:pw"), Status: "active", IdentityID: &idID})
	}
	unlinked := db.ResolvedAdmin{ID: uuid.New(), TenantID: uuid.New(), PasswordHash: db.NewPasswordHash("digest:pw"), Status: "active"}
	rows = append(rows, unlinked)
	// No identity resolves (the identity's own rows are the resolver's; the legacy arm still must
	// not take them): the sign-in is legacy-only and the unlinked row is the one that verifies.
	d := &shapeDB{rows: rows}
	m, n := shapeManager(t, d)
	in, err := m.AuthenticatePerson(context.Background(), "p@example.test", "pw", uuid.Nil, newCountingBudget(10))
	if err != nil {
		t.Fatalf("AuthenticatePerson: %v", err)
	}
	v := in.Legacy.Verified()
	if len(v) != 1 || v[0].AdminUserID != unlinked.ID || v[0].Proof.IsZero() {
		t.Fatalf("legacy verified = %+v, want the unlinked row alone, with its proof", v)
	}
	if len(in.Legacy.Attempts) != 1 || in.Legacy.Resolved != 1 {
		t.Errorf("legacy compared %d rows (resolved %d), want 1 -- linked rows are no candidates", len(in.Legacy.Attempts), in.Legacy.Resolved)
	}
	if *n != 1+MaxCandidates {
		t.Errorf("%d comparisons, want %d", *n, 1+MaxCandidates)
	}
}

// TestAuthenticatePerson_TheFailureBudgetWritesOneOverLimitRow: within the per-identity budget each
// failure is one login_failed row; the first failure past it is the ONE login_limited row; after
// that the event step still runs (the shape is the same) but names no identity and writes nothing.
// An unknown address never names one.
func TestAuthenticatePerson_TheFailureBudgetWritesOneOverLimitRow(t *testing.T) {
	cases, _ := shapeCases()
	d := &shapeDB{ident: cases[0].ident, rows: cases[0].rows}
	m, _ := shapeManager(t, d)
	budget := newCountingBudget(3)
	for i := 0; i < 6; i++ {
		if _, err := m.AuthenticatePerson(context.Background(), "p@example.test", "wrong", uuid.Nil, budget); err != ErrBadCredentials {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	want := []string{"login_failed", "login_failed", "login_failed", "login_limited", "nobody", "nobody"}
	if !slices.Equal(d.events, want) {
		t.Fatalf("events = %v, want %v", d.events, want)
	}

	unknown := &shapeDB{}
	m, _ = shapeManager(t, unknown)
	if _, err := m.AuthenticatePerson(context.Background(), "nobody@example.test", "wrong", uuid.Nil, budget); err != ErrBadCredentials {
		t.Fatal(err)
	}
	if !slices.Equal(unknown.events, []string{"nobody"}) {
		t.Fatalf("an unknown address recorded %v, want one call naming nobody", unknown.events)
	}
}

// TestAuthenticatePerson_UnlookupableInputPaysAndAsksNothing: an empty or unencodable address is
// decided by the caller's own bytes; it still pays 1 + MaxCandidates and asks the database nothing
// (Authenticate's rule). A nil budget is a wiring error, refused before anything.
func TestAuthenticatePerson_UnlookupableInputPaysAndAsksNothing(t *testing.T) {
	for _, c := range []struct{ email, password string }{{"", "pw"}, {"p@example.test", ""}, {"p\x00@example.test", "pw"}, {"p\xff@example.test", "pw"}} {
		d := &shapeDB{}
		m, n := shapeManager(t, d)
		if _, err := m.AuthenticatePerson(context.Background(), c.email, c.password, uuid.Nil, newCountingBudget(10)); err != ErrBadCredentials {
			t.Fatalf("%q: %v", c.email, err)
		}
		if *n != 1+MaxCandidates || len(d.ops) != 0 {
			t.Errorf("%q: %d comparisons and calls %v; want %d and none", c.email, *n, d.ops, 1+MaxCandidates)
		}
	}
	m, _ := shapeManager(t, &shapeDB{})
	if _, err := m.AuthenticatePerson(context.Background(), "p@example.test", "pw", uuid.Nil, nil); err == nil || errors.Is(err, ErrBadCredentials) {
		t.Fatalf("a nil budget: %v, want a wiring error", err)
	}
}

// TestVerifiedIdentity_HasOneProducerAndOneConsumer holds ADR 0031 §5.2's named exception 1 in the
// source (00011 OBLIGATION 5's twin; manager.go:251-292's two-leg honesty, with one leg): in this
// package's non-test files the `verifiedIdentity{...}` literals are exactly AuthenticatePerson's and
// (M14-18, the account page's fresh sessions) proveIdentity's -- each right after its comparison
// returned true; exactly ONE call of the database's OpenIdentitySession, inside
// openIdentitySession; and openIdentitySession takes a verifiedIdentity. A second producer or a
// second caller is a red test, and the fix is to name it here, not to loosen the count.
func TestVerifiedIdentity_HasOneProducerAndOneConsumer(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var literals, calls []string
	consumerTakesIt := false
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if fn.Name.Name == "openIdentitySession" {
					for _, p := range fn.Type.Params.List {
						if id, ok := p.Type.(*ast.Ident); ok && id.Name == "verifiedIdentity" {
							consumerTakesIt = true
						}
					}
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.CompositeLit:
						if id, ok := x.Type.(*ast.Ident); ok && id.Name == "verifiedIdentity" {
							literals = append(literals, fn.Name.Name)
						}
					case *ast.SelectorExpr:
						if x.Sel.Name == "OpenIdentitySession" {
							calls = append(calls, fn.Name.Name)
						}
					}
					return true
				})
			}
		}
	}
	// M14-18: proveIdentity is the second producer (the account page's fresh sessions), built the
	// same way -- right after its one comparison returned true.
	slices.Sort(literals)
	if !slices.Equal(literals, []string{"AuthenticatePerson", "proveIdentity"}) {
		t.Errorf("verifiedIdentity literals in %v; want exactly two, one in AuthenticatePerson and one in proveIdentity", literals)
	}
	if !slices.Equal(calls, []string{"openIdentitySession"}) {
		t.Errorf("OpenIdentitySession is reached from %v; want exactly openIdentitySession", calls)
	}
	if !consumerTakesIt {
		t.Error("openIdentitySession no longer takes a verifiedIdentity")
	}
}

// openIdentitySessionCallers names, by package directory and enclosing function, every non-test call
// of a method called OpenIdentitySession in internal/... and cmd/... -- the database's caller of
// identity_open_session (ADR 0031 §5.2 named exception 1) and its one consumer. A new entry is a new
// way to open an identity session without the verified-identity type; it is added here by name, with
// its reason, or it is a red test.
var openIdentitySessionCallers = map[string]bool{
	"internal/adminauth.openIdentitySession": true,
}

// TestOpenIdentitySession_HasNoCallerOutsideTheVerifiedType widens the single-consumer check to the
// whole tree (the security audit's fifth low finding): any package that calls (*db.DB).
// OpenIdentitySession -- or anything else by that name -- directly is found and must be listed in
// openIdentitySessionCallers. The scan proves it reads the tree: it must find the listed caller.
func TestOpenIdentitySession_HasNoCallerOutsideTheVerifiedType(t *testing.T) {
	found := map[string]bool{}
	files := 0
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{filepath.Join(repo, "internal"), filepath.Join(repo, "cmd")} {
		err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			files++
			rel, err := filepath.Rel(repo, filepath.Dir(path))
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if c, ok := n.(*ast.CallExpr); ok {
						if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "OpenIdentitySession" {
							found[filepath.ToSlash(rel)+"."+fn.Name.Name] = true
						}
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files < 100 {
		t.Fatalf("the scan read %d files; it is not reading the tree", files)
	}
	for caller := range found {
		if !openIdentitySessionCallers[caller] {
			t.Errorf("%s calls OpenIdentitySession directly; identity_open_session is reached only through adminauth's verifiedIdentity (ADR 0031 §5.2 named exception 1) -- name it in openIdentitySessionCallers with its reason, or route it through adminauth", caller)
		}
	}
	for caller := range openIdentitySessionCallers {
		if !found[caller] {
			t.Errorf("%s is listed but the scan did not find it: the list describes a tree that no longer exists, or the scan went blind", caller)
		}
	}
}
