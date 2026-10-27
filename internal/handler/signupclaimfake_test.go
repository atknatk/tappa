package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/signup"
)

// fakeClaims is the registration drafts in memory (M14-17): what adminauth.SignupClaims does, without
// bcrypt and without a database. internal/adminauth and internal/domain/signup test the real ones on
// Postgres; this measures the boundary.
//
// It hands out real ResetTokens (adminauth.ParseResetToken) and recognises one through the only window
// a ResetToken has -- ClaimGrant.Link -- so the handler under test cannot tell it from the real thing.
type fakeClaims struct {
	mu     sync.Mutex
	claims map[string]*fakeClaim
	// known are the addresses that "have a sign-in" (resolve_identity_by_email answers a row).
	known map[string]bool
	// signInless are the known addresses whose sign-in has NO active membership (M14-17b), with that
	// sign-in's password: their confirmation asks for it.
	signInless map[string]string
	// identityProofs counts the identity-password proofs asked for.
	identityProofs int
	// openErr, when set, is Open's answer.
	openErr error
	// onOpen is called with every opened draft (the provisioner fake records them).
	onOpen func(signup.Draft)
	// proved is the draft the last successful proof proved; Provision reads it.
	proved *fakeClaim
	// sessionProofs counts the panel-session proofs asked for.
	sessionProofs int
	now           time.Time
}

type fakeClaim struct {
	id              uuid.UUID
	email, password string
	draft           []byte
	expires         time.Time
	used            bool
}

func newFakeClaims() *fakeClaims {
	return &fakeClaims{claims: map[string]*fakeClaim{}, known: map[string]bool{}, signInless: map[string]string{}, now: time.Now()}
}

// rawToken reads a ResetToken back through ClaimGrant.Link, its one window.
func rawToken(t adminauth.ResetToken) string {
	return strings.TrimPrefix(adminauth.ClaimGrant{Token: t}.Link(""), "?t=")
}

func (f *fakeClaims) Open(_ context.Context, email, password string, draft []byte) (adminauth.ClaimGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.openErr != nil {
		return adminauth.ClaimGrant{}, f.openErr
	}
	d, err := signup.DecodeDraft(draft)
	if err != nil {
		return adminauth.ClaimGrant{}, err
	}
	d.Account.Email, d.Account.Password = email, password
	if f.onOpen != nil {
		f.onOpen(d)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return adminauth.ClaimGrant{}, err
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	c := &fakeClaim{id: uuid.New(), email: email, password: password, draft: draft, expires: f.now.Add(adminauth.SignupClaimTTL)}
	f.claims[raw] = c
	return adminauth.ClaimGrant{ClaimID: c.id, Token: adminauth.ParseResetToken(raw), Recipient: email,
		ExpiresAt: c.expires, KnownAddress: f.known[strings.ToLower(email)]}, nil
}

func (f *fakeClaims) Resolve(_ context.Context, t adminauth.ResetToken) (adminauth.SignupClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.claims[rawToken(t)]
	if !ok {
		return adminauth.SignupClaim{}, adminauth.ErrNoSuchClaim
	}
	_, signInless := f.signInless[strings.ToLower(c.email)]
	sc := adminauth.SignupClaim{ID: c.id, Email: c.email, Draft: c.draft, ExpiresAt: c.expires,
		KnownAddress: f.known[strings.ToLower(c.email)], IdentityPassword: f.known[strings.ToLower(c.email)] && signInless}
	if c.used {
		u := f.now
		sc.UsedAt = &u
	}
	return sc, nil
}

func (f *fakeClaims) byID(id uuid.UUID) *fakeClaim {
	for _, c := range f.claims {
		if c.id == id {
			return c
		}
	}
	return nil
}

func (f *fakeClaims) ProveWithPassword(c adminauth.SignupClaim, password string) (adminauth.ClaimProof, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fc := f.byID(c.ID)
	if fc == nil || fc.password != password {
		return adminauth.ClaimProof{}, false
	}
	f.proved = fc
	return adminauth.ClaimProof{}, true
}

func (f *fakeClaims) ProveWithSession(c adminauth.SignupClaim, _ adminauth.Token) (adminauth.ClaimProof, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionProofs++
	fc := f.byID(c.ID)
	if fc == nil {
		return adminauth.ClaimProof{}, false
	}
	f.proved = fc
	return adminauth.ClaimProof{}, true
}

func (f *fakeClaims) ProveWithIdentityPassword(c adminauth.SignupClaim, password string) (adminauth.ClaimProof, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.identityProofs++
	fc := f.byID(c.ID)
	if fc == nil {
		return adminauth.ClaimProof{}, false
	}
	want, ok := f.signInless[strings.ToLower(fc.email)]
	if !ok || !c.IdentityPassword || want != password {
		return adminauth.ClaimProof{}, false
	}
	f.proved = fc
	return adminauth.ClaimProof{}, true
}

func (f *fakeClaims) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClaims) Sweep(context.Context) (int, error) { return 0, nil }

// takeProved hands the provisioner fake the proven draft once and spends it (identity_claim_signup's
// used_at); nil when nothing was proved.
func (f *fakeClaims) takeProved() *fakeClaim {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.proved
	f.proved = nil
	if c == nil || c.used {
		return nil
	}
	return c
}

func (f *fakeClaims) spend(c *fakeClaim) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c.used = true
}

var screenLinkRE = regexp.MustCompile(`href="(https?://[^"]*` + regexp.QuoteMeta(signupConfirmPath) + `\?t=[A-Za-z0-9_-]+)"`)

// finish drives "Check your inbox" to the done page the way a person does on a dev installation (the link
// is on screen): GET /signup/sent, follow the link (its GET stashes the token, the 303 lands on the
// page), and POST the page's form with the account form's password. It returns the POST's response.
func (d *wizardDriver) finish(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	sent := d.get(signupSentPath)
	if sent.Code != http.StatusOK {
		t.Fatalf("GET %s = %d\n%s", signupSentPath, sent.Code, sent.Body.String())
	}
	d.lastSent = sent.Body.String()
	m := screenLinkRE.FindStringSubmatch(sent.Body.String())
	if m == nil {
		t.Fatalf("no registration link on %s", signupSentPath)
	}
	u, err := url.Parse(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if rec := d.get(u.RequestURI()); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != signupConfirmPath {
		t.Fatalf("GET the link = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	page := d.get(signupConfirmPath)
	if page.Code != http.StatusOK {
		t.Fatalf("GET %s = %d\n%s", signupConfirmPath, page.Code, page.Body.String())
	}
	d.lastConfirm = page.Body.String()
	d.confirmForm = url.Values{"csrf": {d.token(page)}, "password": {accountForm("").Get("password")}}
	return d.post(signupConfirmPath, d.confirmForm)
}

// refinish posts the last confirmation form again (a refresh of the done page).
func (d *wizardDriver) refinish(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	if d.confirmForm == nil {
		t.Fatal("refinish before finish")
	}
	return d.post(signupConfirmPath, d.confirmForm)
}
