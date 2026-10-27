package adminauth

import (
	"context"
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
// ProveWithPassword (after the comparison returned true) and ProveWithSession (with a session cookie's
// hash), and db.ClaimSignup -- identity_claim_signup's one caller -- is reached from ClaimProof.Claim alone.
func TestSignupClaimProof_HasItsProducersAndOneConsumer(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var producers, consumers []string
	comparedFirst := false
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
						if fn.Name.Name == "ProveWithPassword" && x.Sel.Name == "compare" {
							comparedFirst = true
						}
					}
					return true
				})
			}
		}
	}
	slices.Sort(producers)
	if !slices.Equal(producers, []string{"ProveWithPassword", "ProveWithSession"}) {
		t.Errorf("ClaimProof is built in %v; want exactly ProveWithPassword and ProveWithSession", producers)
	}
	if !slices.Equal(consumers, []string{"Claim"}) {
		t.Errorf("db.ClaimSignup is reached from %v; want exactly ClaimProof.Claim", consumers)
	}
	if !comparedFirst {
		t.Error("ProveWithPassword no longer compares the password")
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
	known  map[string]bool
	digest string
	stored map[string]db.ResolvedSignupClaim
	opens  int
}

func (f *fakeClaimDB) ResolveIdentityByEmail(_ context.Context, email string) (db.ResolvedIdentity, error) {
	if f.known[strings.ToLower(email)] {
		// The REGISTERED spelling is the lower-case one here; the caller may have typed another.
		return db.ResolvedIdentity{ID: uuid.New(), Email: strings.ToLower(email)}, nil
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
// resolver read and ONE open; only KnownAddress (the letter's choice) differs; and the session proof is the
// panel's own hash of the cookie (not the claim key's).
func TestSignupClaims_OpenDoesTheSameWorkForEveryAddress(t *testing.T) {
	data := &fakeClaimDB{known: map[string]bool{"known@example.mt": true}, stored: map[string]db.ResolvedSignupClaim{}}
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
	if data.opens != 2 || gn.KnownAddress || !gk.KnownAddress {
		t.Fatalf("opens %d, known flags %v/%v", data.opens, gn.KnownAddress, gk.KnownAddress)
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
