package operatorauth_test

// surface_external_test.go -- M10 OP-8's sign-in acceptances that need THIS package's
// bcrypt work COUNTED, driven through internal/handler/operator's real handlers on the
// shipped router (httpx.NewRouter with an operator host configured). They live here and
// not beside the handlers because the comparer and the digest maker are unexported
// fields: this package's test build wraps them (export_test.go, CountWork), and the
// product has no hook for it.
//
// The store is a fake that answers as internal/db answers: the login lookup returns
// db.ErrNoOperator for an address it has no ACTIVE account for -- the database's own
// shape, where tappa_operator's RLS policy shows the active rows, so a pending, a
// disabled and an unknown address are one answer (internal/db's ErrNoOperator doc), and
// an address internal/db cannot store is answered the same way before the database
// (OP-6 md. 8; TestOperatorAccessors_AnUnstorableAddressIsNoAnswerNotAnError). The
// same arms against PostgreSQL's real RLS: internal/handler/operator's
// TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes.

import (
	"bytes"
	"context"
	"encoding/base32"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/sun"
)

// surfStore is the fake: an active account by address, the auth events written, and a
// count of its calls.
type surfStore struct {
	mu     sync.Mutex
	calls  int
	active map[string]db.OperatorAccount
	events []db.OperatorAuthEvent
}

func (s *surfStore) n() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *surfStore) kinds() []db.OperatorAuthEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]db.OperatorAuthEvent(nil), s.events...)
}

func (s *surfStore) OperatorByEmail(_ context.Context, email string) (db.OperatorAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if a, ok := s.active[email]; ok {
		return a, nil
	}
	return db.OperatorAccount{}, db.ErrNoOperator
}

func (s *surfStore) OperatorByID(_ context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	for _, a := range s.active {
		if a.ID == id {
			return a, nil
		}
	}
	return db.OperatorAccount{}, db.ErrNoOperator
}

func (s *surfStore) RecordOperatorAuthEvent(_ context.Context, kind db.OperatorAuthEvent, _ string, _ uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.events = append(s.events, kind)
	return nil
}

func (s *surfStore) OpenOperatorSession(context.Context, uuid.UUID, string, int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return nil
}

func (s *surfStore) CompleteOperatorEnrollment(context.Context, uuid.UUID, string, string, []byte, int64, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return db.ErrOperatorRefused
}

func (s *surfStore) TouchOperatorSession(context.Context, string) (db.OperatorSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return db.OperatorSession{}, db.ErrOperatorRefused
}

func (s *surfStore) CloseOperatorSession(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return db.ErrOperatorRefused
}

// surfLegal is the operator surface's legal store and snapshot (OP-10) for a rig whose
// tests send no legal request: every call is refused, the snapshot is empty.
type surfLegal struct{}

func (surfLegal) LegalVersions(context.Context, string, db.LegalVersionsPage) ([]db.LegalVersion, error) {
	return nil, db.ErrOperatorRefused
}
func (surfLegal) PublishLegal(context.Context, string, string, string) error {
	return db.ErrOperatorRefused
}
func (surfLegal) Published() map[string]legal.Doc { return map[string]legal.Doc{} }
func (surfLegal) Refresh(context.Context) error   { return nil }

// surfTenants is the operator surface's tenant store (OP-11) for the same rig: every
// call is refused.
type surfTenants struct{}

func (surfTenants) TenantList(context.Context, string, db.TenantListQuery) ([]db.TenantSummary, error) {
	return nil, db.ErrOperatorRefused
}
func (surfTenants) TenantDetail(context.Context, string, uuid.UUID) (db.TenantOverview, error) {
	return db.TenantOverview{}, db.ErrOperatorRefused
}

const (
	surfHost   = "ops.taptime.mt"
	surfOrigin = "https://ops.taptime.mt"
	surfPass   = "op8 surface passphrase"
	surfEmail  = "op8-surface@example.test"
)

// surfRig is the Authenticator (built by New -- the production constructor, with its own
// cost-12 dummy digest), the configured Surface and the shipped router, with the
// comparer and the digest maker counted.
type surfRig struct {
	store                *surfStore
	h                    http.Handler
	now                  time.Time
	key                  []byte
	comparisons, digests func() int64
}

func newSurfRig(t *testing.T) *surfRig {
	t.Helper()
	kek := make([]byte, 32)
	copy(kek, "op8 surface test KEK, 32 bytes!!")
	g := &surfRig{store: &surfStore{active: map[string]db.OperatorAccount{}}, now: time.Unix(1_900_000_005, 0), key: []byte("op8 surface TOTP key")}
	digest, err := bcrypt.GenerateFromPassword([]byte(surfPass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	sealed, err := sun.Seal(kek, id[:], g.key)
	if err != nil {
		t.Fatal(err)
	}
	g.store.active[surfEmail] = db.OperatorAccount{ID: id, Digest: db.NewPasswordHash(string(digest)), Sealed: db.NewSealedSecret(sealed)}
	log := debugCapture(&bytes.Buffer{})
	a, err := operatorauth.New(g.store, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(kek), TokenHMACKey: operatorauth.NewKey([]byte("op8 surface token HMAC key, 32 b")),
		Now: func() time.Time { return g.now }, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	g.comparisons, g.digests = operatorauth.CountWork(a)
	s, err := operator.New(a, surfLegal{}, surfTenants{}, surfLegal{}, surfHost, "https://taptime.mt", log)
	if err != nil {
		t.Fatal(err)
	}
	g.h = httpx.NewRouter(&config.Config{OperatorHost: surfHost, BaseURL: "https://taptime.mt"}, log, s)
	return g
}

// post sends a form (raw, so an invalid byte can be put in a field) to the operator host.
func (g *surfRig) post(path, body, origin, site string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://"+surfHost+path, strings.NewReader(body))
	r.Host = surfHost
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if site != "" {
		r.Header.Set("Sec-Fetch-Site", site)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	r.RemoteAddr = "192.0.2.44:1"
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	return w
}

// TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike is the OP-8 acceptance
// "bilinmeyen e-posta = yanlış parola (gövde + bcrypt sayısı)" through the HTTP surface:
// an unknown address, a wrong password, a pending and a disabled account (each an
// address the login lookup does not return), an address with a NUL byte, one that is
// not UTF-8, one 1000 bytes long, and an empty form -- each of the eight is 401 with the
// SAME headers and the SAME body bytes, pays EXACTLY ONE bcrypt comparison, makes exactly
// one login lookup, and leaves exactly one pre-session audit event (the "Every" of the
// name is these eight). CONTROL: the right password also pays exactly one comparison,
// and sets one cookie where the eight set none.
func TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike(t *testing.T) {
	g := newSurfRig(t)
	form := func(email, password string) string {
		return "email=" + url.QueryEscape(email) + "&password=" + url.QueryEscape(password)
	}
	arms := []struct {
		name, body string
		kind       db.OperatorAuthEvent
	}{
		{"an unknown address", form("nobody@example.test", surfPass), db.OperatorUnknownEmail},
		{"a wrong password", form(surfEmail, surfPass+"x"), db.OperatorLoginFailed},
		{"a pending account (not returned by the lookup)", form("pending@example.test", surfPass), db.OperatorUnknownEmail},
		{"a disabled account (not returned by the lookup)", form("disabled@example.test", surfPass), db.OperatorUnknownEmail},
		{"an address with a NUL byte", form("a\x00b@example.test", surfPass), db.OperatorUnknownEmail},
		{"an address that is not UTF-8", form("\xff\xfe@example.test", surfPass), db.OperatorUnknownEmail},
		{"an address 1000 bytes long", form(strings.Repeat("a", 990)+"@x.test", surfPass), db.OperatorUnknownEmail},
		{"an empty form", "", db.OperatorUnknownEmail},
	}
	var first *httptest.ResponseRecorder
	for i, a := range arms {
		c0, n0, e0 := g.comparisons(), g.store.n(), len(g.store.kinds())
		w := g.post("/operator/login", a.body, surfOrigin, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", a.name, w.Code)
		}
		if got := g.comparisons() - c0; got != 1 {
			t.Errorf("%s: %d bcrypt comparison(s), want exactly 1", a.name, got)
		}
		kinds := g.store.kinds()
		if len(kinds)-e0 != 1 || kinds[len(kinds)-1] != a.kind {
			t.Errorf("%s: audit events %v after %d, want one %s", a.name, kinds, e0, a.kind)
		}
		if got := g.store.n() - n0; got != 2 { // the lookup and the one row
			t.Errorf("%s: %d store call(s), want 2 (lookup + row)", a.name, got)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Errorf("%s: set a cookie", a.name)
		}
		if i == 0 {
			first = w
			continue
		}
		if w.Body.String() != first.Body.String() {
			t.Errorf("%s: the body differs from the unknown address's", a.name)
		}
		for k := range first.Result().Header {
			if w.Result().Header.Get(k) != first.Result().Header.Get(k) {
				t.Errorf("%s: header %s = %q, the unknown address's %q", a.name, k, w.Result().Header.Get(k), first.Result().Header.Get(k))
			}
		}
		if len(w.Result().Header) != len(first.Result().Header) {
			t.Errorf("%s: %d headers, the unknown address's %d", a.name, len(w.Result().Header), len(first.Result().Header))
		}
	}
	// CONTROL: the right password -- one comparison too, and one cookie.
	c0 := g.comparisons()
	w := g.post("/operator/login", form(surfEmail, surfPass), surfOrigin, "")
	if w.Code != http.StatusSeeOther || g.comparisons()-c0 != 1 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("CONTROL: the right password = %d, %d comparison(s), %d cookie(s)", w.Code, g.comparisons()-c0, len(w.Result().Cookies()))
	}
	// The body-size bound is checked ahead of Password: an oversized body is answered
	// with 0 comparisons and 0 store calls.
	c0, n0 := g.comparisons(), g.store.n()
	if w := g.post("/operator/login", form(strings.Repeat("a", 20<<10), surfPass), surfOrigin, ""); w.Code != http.StatusRequestEntityTooLarge ||
		g.comparisons() != c0 || g.store.n() != n0 {
		t.Errorf("an oversized form = %d with %d comparison(s), %d store call(s)", w.Code, g.comparisons()-c0, g.store.n()-n0)
	}
}

var (
	surfKeyRE  = regexp.MustCompile(`<p class="op-key">([A-Z2-7 ]+)</p>`)
	surfBlobRE = regexp.MustCompile(`name="blob" value="([^"]+)"`)
)

// TestSurface_ACrossOriginPostPaysNothing is the bcrypt half of the OP-8 acceptance
// "cross-origin POST'ta çözümleyici çağrısı 0": a POST to each of the four POST routes
// that did not come from the operator origin pays ZERO bcrypt comparisons, ZERO bcrypt
// digests and ZERO store calls (internal/handler/operator's
// TestSameOriginGate_ACrossOriginPostReachesNoStore counts the store under more shapes).
// CONTROL: the same sign-in from the operator origin pays one comparison; the same
// enrollment -- a real page, its first code, a well-formed token the store refuses --
// pays one digest.
func TestSurface_ACrossOriginPostPaysNothing(t *testing.T) {
	g := newSurfRig(t)
	id := uuid.New()
	r := httptest.NewRequest(http.MethodGet, "http://"+surfHost+"/operator/enroll?id="+id.String(), nil)
	r.Host = surfHost
	page := httptest.NewRecorder()
	g.h.ServeHTTP(page, r)
	km, bm := surfKeyRE.FindStringSubmatch(page.Body.String()), surfBlobRE.FindStringSubmatch(page.Body.String())
	if page.Code != http.StatusOK || km == nil || bm == nil {
		t.Fatalf("PREMISE: the enrollment page = %d without its key or blob", page.Code)
	}
	key, err := base32.StdEncoding.DecodeString(strings.ReplaceAll(km[1], " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	enroll := "id=" + id.String() + "&blob=" + url.QueryEscape(html.UnescapeString(bm[1])) +
		"&token=" + strings.Repeat("T", 43) + "&password=" + url.QueryEscape(surfPass) + "&password_again=" + url.QueryEscape(surfPass) +
		"&code=" + otpAt(key, g.now)
	login := "email=" + url.QueryEscape(surfEmail) + "&password=" + url.QueryEscape(surfPass)
	bodies := map[string]string{
		"/operator/login": login, "/operator/login/totp": "code=" + otpAt(g.key, g.now), "/operator/enroll": enroll, "/operator/logout": "",
	}
	cookies := []*http.Cookie{
		{Name: operatorauth.SessionCookieName, Value: strings.Repeat("S", 43)},
		{Name: operatorauth.ChallengeCookieName, Value: "x.y"},
	}
	for path, body := range bodies {
		for _, o := range []struct{ origin, site string }{
			{"https://taptime.mt", "same-site"}, {"https://evil.example", "cross-site"}, {"null", ""}, {"", "same-site"}, {"", ""},
		} {
			c0, d0, n0 := g.comparisons(), g.digests(), g.store.n()
			w := g.post(path, body, o.origin, o.site, cookies...)
			if w.Code != http.StatusForbidden || g.comparisons() != c0 || g.digests() != d0 || g.store.n() != n0 {
				t.Errorf("POST %s from %q/%q = %d, paid %d comparison(s), %d digest(s), %d store call(s)", path, o.origin, o.site,
					w.Code, g.comparisons()-c0, g.digests()-d0, g.store.n()-n0)
			}
		}
	}
	// CONTROL: the operator origin pays.
	c0 := g.comparisons()
	if w := g.post("/operator/login", login, surfOrigin, ""); g.comparisons()-c0 != 1 {
		t.Fatalf("CONTROL: a same-origin sign-in = %d with %d comparison(s)", w.Code, g.comparisons()-c0)
	}
	d0 := g.digests()
	if w := g.post("/operator/enroll", enroll, surfOrigin, ""); w.Code != http.StatusBadRequest || g.digests()-d0 != 1 {
		t.Fatalf("CONTROL: a same-origin enrollment the store refuses = %d with %d digest(s), want 400 and 1", w.Code, g.digests()-d0)
	}
}
