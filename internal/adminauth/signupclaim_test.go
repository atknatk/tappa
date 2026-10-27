package adminauth

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// signupclaim_test.go -- M14-17 (ADR 0031 §5.10, S12): the registration draft's proof type and Open, without
// Postgres (internal/db and internal/domain/signup drive the real functions).

// TestSignupClaimProof_HasItsProducersAndOneConsumer is the S12 guarantee read from the source, named like
// TestVerifiedIdentity_HasOneProducerAndOneConsumer: a ClaimProof literal with fields exists only in
// ProveWithPassword (after the comparison returned true), ProveWithSession (with a session cookie's
// hash) and ProveWithIdentityPassword (M14-17b, after ITS comparison returned true), and db.ClaimSignup --
// identity_claim_signup's one caller -- is reached from ClaimProof.Claim alone.
func TestSignupClaimProof_HasItsProducersAndOneConsumer(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var producers, consumers []string
	compared := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.CompositeLit:
						if id, ok := x.Type.(*ast.Ident); ok && id.Name == "ClaimProof" && len(x.Elts) > 0 {
							producers = append(producers, fn.Name.Name)
						}
					case *ast.SelectorExpr:
						if x.Sel.Name == "ClaimSignup" {
							consumers = append(consumers, fn.Name.Name)
						}
						if x.Sel.Name == "compare" {
							compared[fn.Name.Name] = true
						}
					}
					return true
				})
			}
		}
	}
	slices.Sort(producers)
	if !slices.Equal(producers, []string{"ProveWithIdentityPassword", "ProveWithPassword", "ProveWithSession"}) {
		t.Errorf("ClaimProof is built in %v; want exactly ProveWithIdentityPassword, ProveWithPassword and ProveWithSession", producers)
	}
	if !slices.Equal(consumers, []string{"Claim"}) {
		t.Errorf("db.ClaimSignup is reached from %v; want exactly ClaimProof.Claim", consumers)
	}
	for _, fn := range []string{"ProveWithPassword", "ProveWithIdentityPassword"} {
		if !compared[fn] {
			t.Errorf("%s no longer compares the password", fn)
		}
	}
}

// claimSignupCallers names, by package directory and enclosing function, every non-test call of a
// function called ClaimSignup in internal/... and cmd/...: the database's caller (internal/db's wrapper is
// a declaration, not a call) and adminauth's proof. A new entry is a way to claim a draft without the
// proof type.
var claimSignupCallers = map[string]bool{"internal/adminauth.Claim": true}

// TestClaimSignup_HasNoCallerOutsideTheProof widens the single-consumer check to the whole tree.
func TestClaimSignup_HasNoCallerOutsideTheProof(t *testing.T) {
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
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
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
						if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ClaimSignup" {
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
		if !claimSignupCallers[caller] {
			t.Errorf("%s calls ClaimSignup directly; identity_claim_signup is reached only through adminauth.ClaimProof (S12)", caller)
		}
	}
	for caller := range claimSignupCallers {
		if !found[caller] {
			t.Errorf("%s is listed but the scan did not find it", caller)
		}
	}
}

// fakeClaimDB is SignupClaimDatabase in memory.
type fakeClaimDB struct {
	known map[string]bool
	// ids keeps each known address's identity id stable across reads; events are the
	// identity_record_failed_login calls (id, event), the uuid.Nil ones included.
	ids    map[string]uuid.UUID
	events [][2]string
	// signInless: known addresses whose identity has NO active membership, with the identity's digest.
	signInless map[string]string
	digest     string
	stored     map[string]db.ResolvedSignupClaim
	opens      int
}

func (f *fakeClaimDB) ResolveIdentityByEmail(_ context.Context, email string) (db.ResolvedIdentity, error) {
	if f.known[strings.ToLower(email)] {
		if f.ids == nil {
			f.ids = map[string]uuid.UUID{}
		}
		if _, ok := f.ids[strings.ToLower(email)]; !ok {
			f.ids[strings.ToLower(email)] = uuid.New()
		}
		id := f.ids[strings.ToLower(email)]
		// The REGISTERED spelling is the lower-case one here; the caller may have typed another.
		if d, ok := f.signInless[strings.ToLower(email)]; ok {
			return db.ResolvedIdentity{ID: id, Email: strings.ToLower(email), PasswordHash: db.NewPasswordHash(d)}, nil
		}
		return db.ResolvedIdentity{ID: id, Email: strings.ToLower(email), PasswordHash: db.NewPasswordHash("$2a$04$" + strings.Repeat("m", 53)),
			HasActiveMembership: true}, nil
	}
	return db.ResolvedIdentity{}, pgx.ErrNoRows
}

func (f *fakeClaimDB) OpenSignupClaim(_ context.Context, h, email string, draft []byte, digest string, exp time.Time) (uuid.UUID, error) {
	f.opens++
	f.digest = digest
	id := uuid.New()
	f.stored[h] = db.ResolvedSignupClaim{ID: id, Email: email, PasswordHash: db.NewPasswordHash(digest), ExpiresAt: exp, Draft: draft}
	return id, nil
}

func (f *fakeClaimDB) GetSignupClaimByTokenHash(_ context.Context, h string) (db.ResolvedSignupClaim, error) {
	r, ok := f.stored[h]
	if !ok {
		return db.ResolvedSignupClaim{}, pgx.ErrNoRows
	}
	return r, nil
}

func (f *fakeClaimDB) SweepSignupClaims(context.Context) (int, error) { return 0, nil }

func (f *fakeClaimDB) RecordFailedIdentityLogin(_ context.Context, id uuid.UUID, event string) (bool, error) {
	f.events = append(f.events, [2]string{id.String(), event})
	return id != uuid.Nil, nil
}

func newTestSignupClaims(t *testing.T, data SignupClaimDatabase) *SignupClaims {
	t.Helper()
	s, err := NewSignupClaims(data, &config.Config{SessionHMACKey: []byte(strings.Repeat("s", 32))})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestSignupClaims_OpenHashesAtTheShippedCost: the draft's digest is adminauth.Hash's -- bcrypt at Cost --
// and the chosen password verifies against it; the link round-trips to the draft; a wrong password proves
// nothing; a malformed link resolves to nothing. ONE real bcrypt (the shipped cost is the point).
func TestSignupClaims_OpenHashesAtTheShippedCost(t *testing.T) {
	data := &fakeClaimDB{known: map[string]bool{}, stored: map[string]db.ResolvedSignupClaim{}}
	s := newTestSignupClaims(t, data)
	g, err := s.Open(context.Background(), "maria@example.mt", "a passphrase nobody guesses", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(data.digest, "$2a$12$") || !Compare(data.digest, "a passphrase nobody guesses") {
		t.Fatalf("the stored digest is not Hash's at cost %d", Cost)
	}
	s.compare = func(d, p string) bool { return p == "a passphrase nobody guesses" } // the comparison's cost is not this test's
	link := g.Link("https://x.example/admin/signup/confirm")
	c, err := s.Resolve(context.Background(), ParseSignupClaimToken(link[strings.Index(link, "?t=")+3:]))
	if err != nil || c.ID != g.ClaimID || c.Email != "maria@example.mt" || c.KnownAddress {
		t.Fatalf("resolve: %v %+v", err, c)
	}
	if _, ok := s.ProveWithPassword(c, "not it"); ok {
		t.Fatal("a wrong password proved the draft")
	}
	if p, ok := s.ProveWithPassword(c, "a passphrase nobody guesses"); !ok || p.hash == "" || p.session != nil {
		t.Fatal("the chosen password did not prove the draft as a new address")
	}
	if _, err := s.Resolve(context.Background(), ParseSignupClaimToken("short")); err != ErrNoSuchClaim {
		t.Fatalf("a malformed token: %v", err)
	}
	var zero ClaimProof
	if _, err := zero.Claim(context.Background(), nil, uuid.New()); err == nil {
		t.Fatal("the zero ClaimProof claimed")
	}
}

// TestSignupClaims_OpenDoesTheSameWorkForEveryAddress (R18): a new and a known address each cost ONE
// resolver read and ONE open; only KnownAddress (the letter's choice) differs -- and a known address whose
// identity has no active membership (M14-17b) gets the known address's grant exactly (the letter is not
// chosen by the businesses); and the session proof is the panel's own hash of the cookie (not the claim
// key's).
func TestSignupClaims_OpenDoesTheSameWorkForEveryAddress(t *testing.T) {
	data := &fakeClaimDB{known: map[string]bool{"known@example.mt": true, "lapsed@example.mt": true},
		signInless: map[string]string{"lapsed@example.mt": "$2a$04$" + strings.Repeat("l", 53)}, stored: map[string]db.ResolvedSignupClaim{}}
	s := newTestSignupClaims(t, data)
	s.digest = func(string) (string, error) { return "$2a$04$" + strings.Repeat("x", 53), nil }
	gn, err := s.Open(context.Background(), "new@example.mt", "pw", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	gk, err := s.Open(context.Background(), "Known@example.mt", "pw", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	gl, err := s.Open(context.Background(), "Lapsed@example.mt", "pw", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if data.opens != 3 || gn.KnownAddress || !gk.KnownAddress || !gl.KnownAddress {
		t.Fatalf("opens %d, known flags %v/%v/%v", data.opens, gn.KnownAddress, gk.KnownAddress, gl.KnownAddress)
	}
	link := gk.Link("")
	c, err := s.Resolve(context.Background(), ParseSignupClaimToken(strings.TrimPrefix(link, "?t=")))
	if err != nil || !c.KnownAddress {
		t.Fatalf("resolve a known address's draft: %v %v", err, c.KnownAddress)
	}
	session, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := s.ProveWithSession(c, session)
	want, _ := session.hash(s.sessionKey)
	if !ok || p.session == nil || *p.session != want {
		t.Fatal("the session proof is not the panel's hash of the cookie")
	}
	if _, ok := s.ProveWithSession(c, Token{}); ok {
		t.Fatal("a malformed cookie proved the draft")
	}
}

// TestSignupClaims_AKnownAddressIsWrittenToAtItsRegisteredSpelling (M14-17's audit): the known address's
// letter goes to the identity's REGISTERED address (the resolver row's, M14-14's identityRecipient), not
// to the visitor's typing; a new address's goes to what was typed.
//
// MUTATION (measured): signupRecipientOf returning the typed address on both branches -> red.
func TestSignupClaims_AKnownAddressIsWrittenToAtItsRegisteredSpelling(t *testing.T) {
	data := &fakeClaimDB{known: map[string]bool{"known@example.mt": true}, stored: map[string]db.ResolvedSignupClaim{}}
	s := newTestSignupClaims(t, data)
	s.digest = func(string) (string, error) { return "$2a$04$" + strings.Repeat("x", 53), nil }
	gk, err := s.Open(context.Background(), "KNOWN@Example.MT", "pw", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if gk.Recipient != "known@example.mt" {
		t.Errorf("a known address's letter goes to %q, want the registered %q", gk.Recipient, "known@example.mt")
	}
	gn, err := s.Open(context.Background(), "New@Example.MT", "pw", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if gn.Recipient != "New@Example.MT" {
		t.Errorf("a new address's letter goes to %q, want the typed address", gn.Recipient)
	}
}

// TestSignupClaims_ASignInlessIdentityProvesWithItsOwnPassword (M14-17b, the user's decision of 2026-10-27):
// Resolve marks a known address whose identity has NO active membership (IdentityPassword) and keeps the
// identity's digest; ProveWithIdentityPassword compares the re-entered password with THAT digest -- the
// draft's own password proves nothing there -- and the proof carries the digest for identity_claim_signup
// to re-check under the person's lock. A known address that CAN sign in is not marked and no password
// proves it (its proof is the session); neither does a new address's.
//
// MUTATION (measured): Resolve keeping the digest for every known address (the HasActiveMembership test
// removed) -> "a known address that can sign in" red; ProveWithIdentityPassword comparing the draft's
// digest -> "the draft's password" red.
func TestSignupClaims_ASignInlessIdentityProvesWithItsOwnPassword(t *testing.T) {
	identityDigest := "$2a$04$" + strings.Repeat("i", 53)
	data := &fakeClaimDB{known: map[string]bool{"lapsed@example.mt": true, "active@example.mt": true},
		signInless: map[string]string{"lapsed@example.mt": identityDigest}, stored: map[string]db.ResolvedSignupClaim{}}
	s := newTestSignupClaims(t, data)
	s.digest = func(string) (string, error) { return "$2a$04$" + strings.Repeat("d", 53), nil }
	s.compare = func(d, p string) bool {
		return (d == identityDigest && p == "the identity's password") || (d == "$2a$04$"+strings.Repeat("d", 53) && p == "the draft's password")
	}
	resolve := func(email string) SignupClaim {
		t.Helper()
		g, err := s.Open(context.Background(), email, "the draft's password", []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.Resolve(context.Background(), ParseSignupClaimToken(strings.TrimPrefix(g.Link(""), "?t=")))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	lapsed := resolve("Lapsed@example.mt")
	if !lapsed.KnownAddress || !lapsed.IdentityPassword {
		t.Fatalf("a sign-in with no active membership: known %v, identity password %v; want both", lapsed.KnownAddress, lapsed.IdentityPassword)
	}
	budget := newCountingBudget(1000)
	proves := func(c SignupClaim, pw string) bool {
		t.Helper()
		_, err := s.ProveWithIdentityPassword(context.Background(), c, pw, budget)
		if err != nil && !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("prove: %v", err)
		}
		return err == nil
	}
	if proves(lapsed, "the draft's password") {
		t.Error("the draft's password proved a sign-in's draft")
	}
	if proves(lapsed, "not it") {
		t.Error("a wrong password proved the draft")
	}
	p, err := s.ProveWithIdentityPassword(context.Background(), lapsed, "the identity's password", budget)
	if err != nil || p.hash == "" || p.session != nil || p.verified.RevealForPasswordComparison() != identityDigest {
		t.Fatal("the identity's password did not prove the draft, or the proof does not carry the identity's digest")
	}
	t.Run("a known address that can sign in", func(t *testing.T) {
		active := resolve("active@example.mt")
		if !active.KnownAddress || active.IdentityPassword {
			t.Fatalf("known %v, identity password %v; want known only", active.KnownAddress, active.IdentityPassword)
		}
		for _, pw := range []string{"the identity's password", "the draft's password"} {
			if proves(active, pw) {
				t.Errorf("%q proved a sign-in that can sign in -- its proof is its session", pw)
			}
		}
	})
	t.Run("a new address", func(t *testing.T) {
		fresh := resolve("new@example.mt")
		if fresh.KnownAddress || fresh.IdentityPassword {
			t.Fatal("a new address reads as known")
		}
		if proves(fresh, "the draft's password") {
			t.Error("the identity-password proof proved a new address")
		}
	})
}

// TestSignupClaims_EveryIdentityPasswordRefusalTakesOneShape (§5.6's flat failures, as the sign-in's): a
// wrong password and a draft that is not this branch's (a new address) each pay ONE comparison and ONE
// identity_record_failed_login round trip -- the uuid.Nil call when nothing is owed; a spent budget, right
// password or wrong, is refused BEFORE any comparison (the third eye's ORTA-1) and pays the round trip
// alone, its first refusal naming the identity (the one login_limited row).
//
// MUTATION (measured): the spent budget's refusal returning before the event call -> red; the budget
// checked after the comparison -> red.
func TestSignupClaims_EveryIdentityPasswordRefusalTakesOneShape(t *testing.T) {
	identityDigest := "$2a$04$" + strings.Repeat("i", 53)
	data := &fakeClaimDB{known: map[string]bool{"lapsed@example.mt": true},
		signInless: map[string]string{"lapsed@example.mt": identityDigest}, stored: map[string]db.ResolvedSignupClaim{}}
	s := newTestSignupClaims(t, data)
	s.digest = func(string) (string, error) { return "$2a$04$" + strings.Repeat("d", 53), nil }
	compares := 0
	s.compare = func(d, p string) bool { compares++; return d == identityDigest && p == "right" }
	resolve := func(email string) SignupClaim {
		t.Helper()
		g, err := s.Open(context.Background(), email, "pw", []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.Resolve(context.Background(), ParseSignupClaimToken(strings.TrimPrefix(g.Link(""), "?t=")))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	lapsed, fresh := resolve("lapsed@example.mt"), resolve("new@example.mt")
	spent := newCountingBudget(0)
	for _, c := range []struct {
		name     string
		claim    SignupClaim
		pw       string
		budget   ClaimBudget
		want     error
		compares int
		named    bool
	}{
		{"a wrong password", lapsed, "wrong", newCountingBudget(10), ErrBadCredentials, 1, true},
		{"a spent budget, the right password (its first refusal)", lapsed, "right", spent, ErrClaimBudgetSpent, 0, true},
		{"a spent budget, a wrong password", lapsed, "wrong", spent, ErrClaimBudgetSpent, 0, false},
		{"a new address's draft", fresh, "right", newCountingBudget(10), ErrBadCredentials, 1, false},
	} {
		compares, data.events = 0, nil
		if _, err := s.ProveWithIdentityPassword(context.Background(), c.claim, c.pw, c.budget); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
		if compares != c.compares || len(data.events) != 1 {
			t.Errorf("%s: %d comparison(s), %d event call(s); want %d and 1", c.name, compares, len(data.events), c.compares)
			continue
		}
		if named := data.events[0][0] != uuid.Nil.String(); named != c.named {
			t.Errorf("%s: the event call names an identity: %v, want %v", c.name, named, c.named)
		}
	}
}
