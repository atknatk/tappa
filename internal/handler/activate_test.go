package handler

// Handler tests against FAKES. What they measure is the HTTP behaviour the flow
// promises — the no-oracle property, the consent gate, the order of revoke and
// issue, the rate limit, and that no response body ever carries the code. The
// database-backed end-to-end proof lives in e2e_db_test.go; neither replaces the
// other.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

// fakeCode is an obviously fake, searchable 43-character stand-in (agent-brief
// madde 2: no real secret is written anywhere). Its length matches a real code so
// nothing takes a different path.
const fakeCode = "FAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake123"

var (
	testTenant   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	testEmployee = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	testInvite   = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

func okContext(status string) invite.Context {
	return invite.Context{
		TenantID:     testTenant,
		EmployeeID:   testEmployee,
		InviteID:     testInvite,
		LocationID:   uuid.New(),
		FullName:     "Maria Borg",
		TenantName:   "Kebab Factory Ltd",
		LocationName: "St Julians",
		WiFiSSID:     "KF-StJulians-Staff",
		Status:       status,
	}
}

type fakeInvites struct {
	lookup        func(invite.Code) (invite.Context, error)
	activate      func(invite.Code) (invite.Activation, error)
	consentErr    error
	activateCalls int
	consentCalls  int
	steps         *[]string
}

func (f *fakeInvites) RecordConsent(_ context.Context, c invite.Code, _ invite.Binding) (invite.Context, error) {
	f.consentCalls++
	if f.steps != nil {
		*f.steps = append(*f.steps, "consent")
	}
	ictx, err := f.Lookup(context.Background(), c)
	if err != nil {
		return ictx, err
	}
	return ictx, f.consentErr
}

func (f *fakeInvites) Lookup(_ context.Context, c invite.Code) (invite.Context, error) {
	if f.lookup == nil {
		return okContext("invited"), nil
	}
	return f.lookup(c)
}

func (f *fakeInvites) Activate(_ context.Context, c invite.Code, _ invite.Binding) (invite.Activation, error) {
	f.activateCalls++
	if f.steps != nil {
		*f.steps = append(*f.steps, "activate")
	}
	if f.activate == nil {
		return invite.Activation{Context: okContext("active")}, nil
	}
	return f.activate(c)
}

func (f *fakeInvites) ActivationContext(_ context.Context, _, _ uuid.UUID) (invite.Context, error) {
	return okContext("active"), nil
}

type fakeSessions struct {
	issueErr   error
	revokeErr  error
	issued     int
	revoked    int
	steps      *[]string
	verify     func() (session.Resolved, error)
	tokenValue string
	// sessionID, when set, is the id Issue hands out and Verify resolves to, so a
	// test can line the activated marker up with the live session (or not).
	sessionID uuid.UUID
	// employeeID, when set, is who Verify says holds the session.
	employeeID uuid.UUID
	// tok is the token Issue hands back; newHandler builds it, because a token
	// can only be obtained through the session package's public door.
	tok session.Token
}

// token builds a real session.Token from outside internal/session, the only way
// a caller can: by reading a request cookie. The fake needs a NON-ZERO token
// because session.Cookies.Set refuses an empty one — which is itself a guarantee
// worth exercising here.
func (f *fakeSessions) token(t *testing.T) session.Token {
	t.Helper()
	v := f.tokenValue
	if v == "" {
		v = "FAKEsessionFAKEsessionFAKEsessionFAKEsess12"
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: v})
	var c session.Cookies
	tok, err := c.Read(r)
	if err != nil {
		t.Fatalf("building a fake token: %v", err)
	}
	return tok
}

func (f *fakeSessions) Issue(_ context.Context, p session.IssueParams) (session.Issued, error) {
	f.issued++
	if f.steps != nil {
		*f.steps = append(*f.steps, "issue")
	}
	if f.issueErr != nil {
		return session.Issued{}, f.issueErr
	}
	id := f.sessionID
	if id == uuid.Nil {
		id = uuid.New()
	}
	return session.Issued{
		Session: session.Session{ID: id, TenantID: p.TenantID, EmployeeID: p.EmployeeID, DeviceInfo: p.DeviceInfo},
		Token:   f.tok,
	}, nil
}

func (f *fakeSessions) Verify(context.Context, session.Token) (session.Resolved, error) {
	if f.verify == nil {
		id, emp := f.sessionID, f.employeeID
		if id == uuid.Nil {
			id = uuid.New()
		}
		if emp == uuid.Nil {
			emp = testEmployee
		}
		return session.Resolved{ID: id, TenantID: testTenant, EmployeeID: emp}, nil
	}
	return f.verify()
}

func (f *fakeSessions) RevokeAllForEmployee(context.Context, uuid.UUID, uuid.UUID) (int, error) {
	f.revoked++
	if f.steps != nil {
		*f.steps = append(*f.steps, "revoke")
	}
	if f.revokeErr != nil {
		return 0, f.revokeErr
	}
	return 2, nil
}

type fakeAudit struct {
	events []audit.Event
	err    error
}

func (f *fakeAudit) Record(_ context.Context, e audit.Event) (uuid.UUID, error) {
	f.events = append(f.events, e)
	return uuid.New(), f.err
}

func (f *fakeAudit) actions() []string {
	out := make([]string, 0, len(f.events))
	for _, e := range f.events {
		out = append(out, e.Action)
	}
	return out
}

func (f *fakeAudit) has(action string) bool {
	for _, e := range f.events {
		if e.Action == action {
			return true
		}
	}
	return false
}

// fakeVerifier stands in for sun.Verifier's ADVANCING entry point. By default it
// answers like a genuine first touch of an active, mounted plaque of testTenant.
type fakeVerifier struct {
	calls    int // ADVANCING Verify calls
	previews int
	verify   func(sun.Params) (sun.Result, error)
}

// testPlaqueUID is a well-formed 7-byte uid for the activating tap's URL.
const testPlaqueUID = "04AC7E55000601"

var testPlaqueWall = uuid.MustParse("44444444-4444-4444-8444-444444444444")

func genuineTap() sun.Result {
	wall := testPlaqueWall
	return sun.Result{
		SUNValid: true,
		Tag:      db.ResolvedTag{UID: testPlaqueUID, TenantID: testTenant, LocationID: &wall, Status: "active"},
		Location: &wall,
	}
}

func (f *fakeVerifier) Verify(_ context.Context, p sun.Params) (sun.Result, error) {
	f.calls++
	if f.verify == nil {
		return genuineTap(), nil
	}
	return f.verify(p)
}

// PreviewWithoutReplayProtection answers from the same scripted result, without
// counting as a Verify: calls counts ADVANCES only, which is what the R5 tests
// assert on.
func (f *fakeVerifier) PreviewWithoutReplayProtection(_ context.Context, p sun.Params) (sun.Preview, error) {
	f.previews++
	res := genuineTap()
	if f.verify != nil {
		var err error
		if res, err = f.verify(p); err != nil {
			return sun.Preview{}, err
		}
	}
	return sun.Preview{CMACValid: res.SUNValid, TenantID: res.Tag.TenantID, TagStatus: res.Tag.Status, Location: res.Location}, nil
}

// activationTapURL is a syntactically valid NFC tap URL (the fake verifier does the
// cryptography's job, so the cmac's value does not matter here).
const activationTapURL = "/t?tag=" + testPlaqueUID + "&ctr=0005F2&cmac=EA2AA40369E4FAE0"

// activationQRURL is the same plaque's static QR URL: no SUN.
const activationQRURL = "/t?tag=" + testPlaqueUID

// handlerOpts lets a test swap the verifier; the zero value is a genuine tap.
type handlerOpts struct{ verifier *fakeVerifier }

func newHandler(t *testing.T, inv *fakeInvites, sess *fakeSessions, rec *fakeAudit) http.Handler {
	t.Helper()
	return newHandlerWith(t, inv, sess, rec, handlerOpts{})
}

// newHandlerWith mounts the activation routes plus a stand-in for GET /t that
// does exactly what the Tap handler does with a consented activation: parse the
// SUN URL, ask Pending, hand the tap to CompleteByTap. A non-pending tap answers
// 303 to /activate, which is the Tap handler's §5 row 3 answer for no session.
func newHandlerWith(t *testing.T, inv *fakeInvites, sess *fakeSessions, rec *fakeAudit, o handlerOpts) http.Handler {
	t.Helper()
	if o.verifier == nil {
		o.verifier = &fakeVerifier{}
	}
	sess.tok = sess.token(t)
	cfg := &config.Config{
		Env:            config.EnvDev,
		BaseURL:        "http://localhost:8080",
		RetentionYears: 2,
	}
	// Discard log output: these tests assert on responses and audit events, and a
	// test that also asserted on log text would fail for cosmetic reasons.
	a, err := NewActivation(inv, sess, o.verifier, rec, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}
	r := chi.NewRouter()
	a.Mount(r)
	r.Get(TapPath, func(w http.ResponseWriter, r *http.Request) {
		p, err := sun.Parse(r.URL.Query())
		if err != nil {
			http.Error(w, "bad url", http.StatusBadRequest)
			return
		}
		if a.Pending(r) {
			a.CompleteByTap(w, r, p)
			return
		}
		http.Redirect(w, r, activationFromTap, http.StatusSeeOther)
	})
	return r
}

func get(t *testing.T, h http.Handler, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = "203.0.113.5:41234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func post(t *testing.T, h http.Handler, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/activate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// fakeCSRF is the synchronizer token the test fixtures pair with fakeCode. It is
// not a secret and is rendered into the page by design.
const fakeCSRF = "TESTcsrfTESTcsrfTESTcsrfTESTcsrfTESTcsrf123"

func codeCookie() *http.Cookie {
	return &http.Cookie{Name: activationCookieName, Value: fakeCSRF + "." + fakeCode}
}

// fakeBinding is the consent binding a consented browser carries (ADR 0025).
const fakeBinding = "BINDbindBINDbindBINDbindBINDbindBINDbind123"

// pendingCookie is the activation cookie AFTER consent: code plus binding. A
// browser holding it is waiting for its first NFC tap.
func pendingCookie() *http.Cookie {
	return &http.Cookie{Name: activationCookieName, Value: fakeCSRF + "." + fakeCode + "." + fakeBinding}
}

// tap is GET /t as the plaque opens it.
func doTap(t *testing.T, h http.Handler, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return get(t, h, target, cookies...)
}

// cookieNamed finds a Set-Cookie on a response.
func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// consent is a well-formed submission: the consent box AND the form token.
func consent() url.Values { return url.Values{"consent": {"yes"}, "csrf": {fakeCSRF}} }

// consentNoToken is a submission that skipped the synchronizer token — the shape
// a forged cross-site POST would have if SameSite had let it through.
func consentNoToken() url.Values { return url.Values{"consent": {"yes"}} }

// TestPage_BareVisitIsALanding: /activate with no code and no cookie is a
// legitimate arrival (§5 row 3 sends session-less taps here once M5-04 exists),
// so it must be a calm page, not an error.
func TestPage_BareVisitIsALanding(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	w := get(t, h, "/activate")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "You need your activation link") {
		t.Fatal("the landing page must explain what to do")
	}
}

// TestPage_ValidCodeMovesIntoCookieAndRedirects is the mechanism that keeps the
// code out of the HTML and out of the address bar.
func TestPage_ValidCodeMovesIntoCookieAndRedirects(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	w := get(t, h, "/activate?code="+fakeCode)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/activate" {
		t.Fatalf("Location = %q, want /activate (the code must not survive in the URL)", loc)
	}
	if strings.Contains(w.Body.String(), fakeCode) {
		t.Fatal("the redirect body carries the code")
	}

	var found *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == activationCookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("no activation cookie was set")
	}
	csrf, code, ok := strings.Cut(found.Value, ".")
	if !ok || code != fakeCode {
		t.Fatalf("the cookie must carry <csrf>.<code> and NO binding before consent, got a value that does not split to the code")
	}
	if csrf == "" || csrf == fakeCode {
		t.Fatal("the synchronizer token must be present and must NOT be derived from the code")
	}
	if !found.HttpOnly {
		t.Error("the activation cookie must be HttpOnly: page script has no business reading a credential")
	}
	if found.SameSite != http.SameSiteLaxMode {
		t.Error("SameSite must be Lax: it is the CSRF defence for POST /api/activate")
	}
	// ADR 0025: the cookie lives until the invitation expires. The fake context
	// carries no expiry, so this lands on the 1-second floor — never 0 (a session
	// cookie) and never negative (a deletion).
	if found.MaxAge < 1 {
		t.Errorf("MaxAge = %d, want >= 1", found.MaxAge)
	}
}

// TestActivationCookie_LivesUntilTheInvitationExpires pins the user decision of
// ADR 0025 at the arithmetic: the cookie's lifetime is the invitation's remaining
// life, bounded above by the ceiling and below by one second.
func TestActivationCookie_LivesUntilTheInvitationExpires(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		expires time.Time
		want    int
	}{
		{"seven days left", now.Add(7 * 24 * time.Hour), 7 * 24 * 3600},
		{"ninety minutes left", now.Add(90 * time.Minute), 90 * 60},
		{"beyond the ceiling", now.Add(400 * 24 * time.Hour), int(activationCookieCeiling / time.Second)},
		{"already expired", now.Add(-time.Hour), 1},
		{"zero expiry", time.Time{}, 1},
	} {
		if got := activationCookieMaxAge(tc.expires, now); got != tc.want {
			t.Errorf("%s: MaxAge = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestPage_RendersTheNoticeAndTheWiFiStep covers the two things the card
// requires this screen to show.
func TestPage_RendersTheNoticeAndTheWiFiStep(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	privacy := get(t, h, "/activate?step=2", codeCookie()).Body.String()
	for _, want := range []string{
		"Maria Borg",             // identity
		"Kebab Factory Ltd",      // GDPR Art. 13(1)(a) controller
		"internet (IP) address",  // what a tap records
		"location",               // ditto
		"No fingerprints",        // §4.1, stated to the employee
		"2",                      // retention, from configuration
		"years",                  //
		"Agree and continue",     // the consent button
		`action="/api/activate"`, // the endpoint the card names
		`name="consent"`,         // the box
	} {
		if !strings.Contains(privacy, want) {
			t.Errorf("the privacy step does not mention %q", want)
		}
	}
	// The Wi-Fi step is step 3, which exists only after consent.
	ready := get(t, h, "/activate?step=3", pendingCookie()).Body.String()
	for _, want := range []string{"KF-StJulians-Staff", "You can skip this", "Stay in this browser"} {
		if !strings.Contains(ready, want) {
			t.Errorf("the get-ready step does not mention %q", want)
		}
	}
	// No step carries the code or the binding.
	for _, body := range []string{privacy, ready} {
		if strings.Contains(body, fakeCode) || strings.Contains(body, fakeBinding) {
			t.Fatal("a rendered wizard step contains the invite code or the consent binding")
		}
	}
}

// TestWizard_StepsAreGETLinksAndConsentGatesTheLaterOnes: the wizard works
// without JavaScript (every step is a GET), and steps 3 and 4 do not exist for a
// browser that has not consented — they would describe a tap that cannot finish.
func TestWizard_StepsAreGETLinksAndConsentGatesTheLaterOnes(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for _, tc := range []struct {
		target string
		cookie *http.Cookie
		want   string // a phrase only that step renders
	}{
		{"/activate", codeCookie(), "set up your phone"},
		{"/activate?step=1", codeCookie(), "set up your phone"},
		{"/activate?step=2", codeCookie(), "Agree and continue"},
		{"/activate?step=3", codeCookie(), "Agree and continue"}, // clamped: no consent yet
		{"/activate?step=4", codeCookie(), "Agree and continue"}, // clamped
		{"/activate?step=nonsense", codeCookie(), "set up your phone"},
		{"/activate?step=3", pendingCookie(), "Two quick things before you tap"},
		{"/activate?step=4", pendingCookie(), "Now tap the plaque"},
		{"/activate", pendingCookie(), "Now tap the plaque"}, // a consented return lands on the wait
	} {
		w := get(t, h, tc.target, tc.cookie)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", tc.target, w.Code)
			continue
		}
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("%s (pending=%v): want the step that says %q", tc.target, strings.Count(tc.cookie.Value, ".") == 2, tc.want)
		}
	}
	welcome := get(t, h, "/activate", codeCookie()).Body.String()
	if !strings.Contains(welcome, `href="/activate?step=2"`) {
		t.Error("step 1's Next must be a plain link (works without JavaScript)")
	}
}

// TestWizard_WaitingScreenLoadsOnlyItsOwnScript: the waiting screen is the one
// step with a script — same-origin, external, deferred — and it is told where to
// poll. Every activation screen carries the CSP.
func TestWizard_WaitingScreenLoadsOnlyItsOwnScript(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	w := get(t, h, "/activate?step=4", pendingCookie())
	body := w.Body.String()
	if !strings.Contains(body, `<script src="/static/js/activate.js" defer>`) {
		t.Error("the waiting screen must load web/static/js/activate.js")
	}
	if strings.Count(body, "<script") != 1 {
		t.Errorf("want exactly one script element, got %d", strings.Count(body, "<script"))
	}
	if !strings.Contains(body, `data-status-url="`+ActivationStatusPath+`"`) {
		t.Error("the waiting screen must name the status endpoint")
	}
	if !strings.Contains(body, "data-wait-done hidden") {
		t.Error("the success block must start hidden")
	}
	if got := w.Header().Get("Content-Security-Policy"); got != activationCSP {
		t.Errorf("CSP = %q, want %q", got, activationCSP)
	}
	if !strings.Contains(activationCSP, "connect-src 'self'") || !strings.Contains(activationCSP, "frame-ancestors 'none'") {
		t.Errorf("activationCSP lost a directive: %q", activationCSP)
	}
	other := get(t, h, "/activate?step=2", codeCookie())
	if strings.Contains(other.Body.String(), "<script") {
		t.Error("the other steps carry no script")
	}
	if other.Header().Get("Content-Security-Policy") == "" {
		t.Error("every activation screen carries the CSP")
	}
}

// TestPage_NonProdShowsTheConfigNotice: the retention figure is a development
// placeholder outside production and the page has to say so, rather than letting
// a dev deployment make a legal-looking claim.
func TestPage_NonProdShowsTheConfigNotice(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate?step=2", codeCookie()).Body.String()
	if !strings.Contains(body, "placeholder value, not legal advice") {
		t.Fatal("a non-production deployment must mark the retention figure as configuration")
	}
}

// TestFailures_AreIndistinguishable is the no-oracle measurement (§4.7): four
// different internal outcomes must produce BYTE-IDENTICAL responses.
func TestFailures_AreIndistinguishable(t *testing.T) {
	kinds := map[string]struct {
		ctx invite.Context
		err error
	}{
		"unknown":         {invite.Context{}, invite.ErrUnknownCode},
		"expired":         {okContext("invited"), invite.ErrCodeExpired},
		"already used":    {okContext("invited"), invite.ErrCodeUsed},
		"not activatable": {okContext("deactivated"), invite.ErrNotActivatable},
	}

	var firstBody string
	var firstStatus int
	for name, k := range kinds {
		inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return k.ctx, k.err }}
		h := newHandler(t, inv, &fakeSessions{}, &fakeAudit{})
		w := post(t, h, consent(), codeCookie())

		if firstBody == "" {
			firstBody, firstStatus = w.Body.String(), w.Code
			if firstStatus != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", firstStatus)
			}
			continue
		}
		if w.Code != firstStatus {
			t.Errorf("%s: status %d differs from %d — the status code is an oracle", name, w.Code, firstStatus)
		}
		if w.Body.String() != firstBody {
			t.Errorf("%s: body differs — the page tells the visitor WHICH failure happened", name)
		}
	}
	// Positive control: the shared body is really the failure page and not "".
	if !strings.Contains(firstBody, "Ask your manager for a new one") {
		t.Fatal("the shared body is not the failure page; the comparison above proves nothing")
	}
}

// TestFailures_AreAudited is the §4.6 half of the same behaviour: the visitor
// learns nothing, the trail learns everything — except for the one case that
// genuinely cannot be attributed.
// TestActivationReasons_CoverEverySentinel DERIVES the sentinel set from
// internal/invite instead of listing it, which is the difference between a net and a
// change detector.
//
// 🔴 IT EXISTS BECAUSE A CLOSED TABLE LET A SENTINEL SHIP UNTESTED. ErrCodeCancelled
// — the whole point of migration 00012, the signal that a retired credential was
// presented — reached production with `grep -rn "ErrCodeCancelled" --include='*_test.go'`
// returning ZERO matches, because the audited-reason table beside it was a fixed list
// and nobody had to add to it. The natural repair for a red fixed-list test is to
// extend the list; the natural repair for THIS one is to give the new sentinel a
// reason, which is the correct move.
//
// ⚠️ WHAT IT CANNOT SEE, named rather than implied: a sentinel declared somewhere
// other than a top-level `Err… = errors.New(…)` in that package — a wrapped error
// type, or one built in a function. The scan is a go/ast walk over the package's
// value declarations, which is the shape internal/invite actually uses for all of them.
//
// 🔴 TWO SCOPES SINCE M10 EM-7B, AND THE INTENT IS UNCHANGED: every refusal
// internal/invite can name reaches audit_log with a word of its own (§4.6). EM-7B added
// refusals that are born when an invitation is MINTED, not when one is SPENT — the
// e-mail route's address refusals and limits, and the on-screen fallback's "has an
// address" — and their words live in the issuance side's tables, not in activation's
// (writing them into inviteFailureReasons would make the activation page claim
// failures it can never see). So the sentinel set is still DERIVED from the package,
// and each sentinel must have its word in EXACTLY ONE of three production tables, each
// read from its source with go/ast (keys and words, not a copy):
//
//	activation  internal/handler activate.go     inviteFailureReasons
//	issuance    internal/invite email.go         refusalReasons      (invite.email_refused)
//	            internal/handler inviteemail.go  showRefusalReasons  (invite.show_refused)
//
// A sentinel in none is red (a new one cannot be silently exempt); one in two is red
// (one refusal, one word). And the named set of sentinels that CANNOT reach audit_log,
// each with its reason — the only list kept by hand, so it cannot be quietly widened:
//
//	ErrUnknownCode  resolves to no tenant, and audit_log.tenant_id is NOT NULL.
//	ErrNotRecorded  is not a refusal: it marks that an outcome's OWN row could not be
//	                written (EM-7B). A row about a failed row is the same write failing
//	                again, so its home is the panel's log — a line of its own, pinned by
//	                TestInviteEmailDB_ARefusalWhoseRowFailsStillSaysWhy and
//	                TestInviteEmail_AnUnrecordedOutcomeIsLoggedNotSwallowed.
func TestActivationReasons_CoverEverySentinel(t *testing.T) {
	sentinels := inviteSentinelNames(t)
	// ANTI-VACUITY: a walk that read nothing would pass over everything.
	if len(sentinels) < 4 {
		t.Fatalf("found %d sentinel(s) in internal/invite (%v); the package declares "+
			"more, so this scan is reading the wrong directory", len(sentinels), sentinels)
	}
	isSentinel := map[string]bool{}
	for _, s := range sentinels {
		isSentinel[s] = true
	}

	tables := []struct {
		scope, file, name string
		words             map[string]string
	}{
		{"activation", "activate.go", "inviteFailureReasons", nil},
		{"issuance (invite.email_refused)", filepath.Join("..", "invite", "email.go"), "refusalReasons", nil},
		{"issuance (invite.show_refused)", "inviteemail.go", "showRefusalReasons", nil},
	}
	for i := range tables {
		tables[i].words = sentinelTable(t, tables[i].file, tables[i].name)
		// ANTI-VACUITY, PER TABLE: a table read as empty would excuse nothing and catch
		// nothing.
		if len(tables[i].words) == 0 {
			t.Fatalf("read no entries from %s in %s; the table moved or changed shape", tables[i].name, tables[i].file)
		}
	}
	unwritable := map[string]string{
		"ErrUnknownCode": "resolves to no tenant; audit_log.tenant_id is NOT NULL",
		"ErrNotRecorded": "marks that an outcome's own row could not be written; logged, not audited",
	}

	for _, name := range sentinels {
		var in []string
		for _, tb := range tables {
			if w, ok := tb.words[name]; ok {
				in = append(in, tb.scope)
				if w == "" {
					t.Errorf("invite.%s has an EMPTY word in %s", name, tb.name)
				}
			}
		}
		_, exempt := unwritable[name]
		switch {
		case exempt && len(in) > 0:
			t.Errorf("invite.%s cannot reach audit_log (%s) and yet has a word in %v", name, unwritable[name], in)
		case exempt:
		case len(in) == 0:
			t.Errorf("invite.%s has no audited word. Every refusal internal/invite can name must reach "+
				"audit_log with a word of its own (§4.6) — give it one in inviteFailureReasons (a refusal "+
				"of SPENDING a link), or in refusalReasons / showRefusalReasons (a refusal of MINTING "+
				"one). This test exists because ErrCodeCancelled shipped without one.", name)
		case len(in) > 1:
			t.Errorf("invite.%s has a word in %d tables (%v); one refusal, one word", name, len(in), in)
		}
	}
	// NO DEAD ENTRIES: a word for a sentinel that no longer exists is a claim about a
	// refusal the product can no longer make.
	for _, tb := range tables {
		for name, w := range tb.words {
			if !isSentinel[name] {
				t.Errorf("%s carries %q for invite.%s, which no longer exists in that package", tb.name, w, name)
			}
		}
	}
	for name := range unwritable {
		if !isSentinel[name] {
			t.Errorf("invite.%s is named as unwritable but no longer exists; drop it from the list", name)
		}
	}
	// THE ACTIVATION TABLE AS READ IS THE ONE THE BINARY RUNS: same size, same words.
	act := tables[0].words
	if len(act) != len(inviteFailureReasons) {
		t.Errorf("inviteFailureReasons has %d entries at run time and %d in its source", len(inviteFailureReasons), len(act))
	}
	for _, w := range inviteFailureReasons {
		found := false
		for _, aw := range act {
			found = found || aw == w
		}
		if !found {
			t.Errorf("inviteFailureReasons stores %q at run time, which its source table does not show", w)
		}
	}
	// THE ISSUANCE WORDS ARE DISTINCT: two refusals sharing a word would be one row to an
	// investigator.
	seen := map[string]string{}
	for _, tb := range tables[1:] {
		for name, w := range tb.words {
			if other, dup := seen[w]; dup {
				t.Errorf("invite.%s and invite.%s share the issuance word %q", name, other, w)
			}
			seen[w] = name
		}
	}
}

// sentinelTable reads a top-level `var <name> = map[error]string{ invite.ErrX: "word",
// … }` (or ErrX: "word" inside internal/invite) out of file, keyed by the sentinel's
// NAME, so the coverage test reads the production table itself rather than a copy.
func sentinelTable(t *testing.T, file, name string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	out := map[string]string{}
	found := false
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name != name || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					t.Fatalf("%s in %s is not a composite literal", name, file)
				}
				found = true
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						t.Fatalf("%s in %s has an element that is not key: value", name, file)
					}
					var key string
					switch k := kv.Key.(type) {
					case *ast.Ident:
						key = k.Name
					case *ast.SelectorExpr:
						key = k.Sel.Name
					default:
						t.Fatalf("%s in %s has a key that is not a sentinel name", name, file)
					}
					word, ok := kv.Value.(*ast.BasicLit)
					if !ok || word.Kind != token.STRING {
						t.Fatalf("%s in %s maps %s to something other than a string literal", name, file, key)
					}
					out[key] = strings.Trim(word.Value, `"`)
				}
			}
		}
	}
	if !found {
		t.Fatalf("%s declares no %s", file, name)
	}
	return out
}

// inviteSentinelNames reads every top-level `Err… = errors.New(…)` out of
// internal/invite with go/ast — the same derivation internal/domain/*/query_test.go
// uses, and for the same reason: a regexp over source is beaten by whitespace.
func inviteSentinelNames(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, filepath.Join("..", "invite"), func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing internal/invite: %v", err)
	}
	var out []string
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, decl := range f.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if !strings.HasPrefix(name.Name, "Err") || i >= len(vs.Values) {
							continue
						}
						call, ok := vs.Values[i].(*ast.CallExpr)
						if !ok {
							continue
						}
						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok || sel.Sel.Name != "New" {
							continue
						}
						out = append(out, name.Name)
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func TestFailures_AreAudited(t *testing.T) {
	cases := []struct {
		name       string
		ctx        invite.Context
		err        error
		wantAudit  bool
		wantReason string
	}{
		{"expired", okContext("invited"), invite.ErrCodeExpired, true, "expired"},
		{"already used", okContext("invited"), invite.ErrCodeUsed, true, "already_used"},
		// 🔴 A RETIRED LINK. It shipped with no row here at all, and an audit measured
		// what that cost: deleting its case dropped a PRESENTED TAKEOVER CREDENTIAL
		// into the unclassified branch, which wrote nothing. This row is the cheap
		// half; TestActivationReasons_CoverEverySentinel is the half that catches the
		// NEXT sentinel somebody adds.
		{"cancelled", okContext("invited"), invite.ErrCodeCancelled, true, "cancelled"},
		{"not activatable", okContext("deactivated"), invite.ErrNotActivatable, true, "employee_not_activatable"},
		{"unknown code has no tenant", invite.Context{}, invite.ErrUnknownCode, false, ""},
		// 🔴 AND AN UNCLASSIFIED FAILURE IS RECORDED TOO (§4.6). It is not a bad link —
		// the visitor gets a 500 — but "we could not tell why" is not a reason to lose
		// the attempt when a tenant is known.
		{"an unclassified failure", okContext("invited"), errors.New("connection refused"), true, "unclassified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &fakeAudit{}
			inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return tc.ctx, tc.err }}
			h := newHandler(t, inv, &fakeSessions{}, rec)
			post(t, h, consent(), codeCookie())

			if !tc.wantAudit {
				if len(rec.events) != 0 {
					t.Fatalf("an unattributable attempt wrote %v; audit_log.tenant_id is NOT NULL and there is no tenant", rec.actions())
				}
				return
			}
			if len(rec.events) != 1 {
				t.Fatalf("audit events = %v, want exactly one activation.failed", rec.actions())
			}
			e := rec.events[0]
			if e.Action != ActionActivationFailed {
				t.Errorf("action = %q", e.Action)
			}
			if e.TenantID != testTenant {
				t.Errorf("the row must be attributed to the tenant, got %s", e.TenantID)
			}
			d, ok := e.Detail.(activationDetail)
			if !ok {
				t.Fatalf("detail type = %T, want a purpose-built struct", e.Detail)
			}
			if d.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", d.Reason, tc.wantReason)
			}
		})
	}
}

// TestSubmit_ConsentIsRequired: the GDPR gate. Nothing is consumed, the refusal
// is recorded, and the visitor gets the form back with the error on the box.
func TestSubmit_ConsentIsRequired(t *testing.T) {
	inv := &fakeInvites{}
	rec := &fakeAudit{}
	h := newHandler(t, inv, &fakeSessions{}, rec)

	w := post(t, h, url.Values{"csrf": {fakeCSRF}}, codeCookie())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if inv.activateCalls != 0 || inv.consentCalls != 0 {
		t.Fatal("consent was recorded (or the code consumed) without the box being ticked")
	}
	if !strings.Contains(w.Body.String(), "Please tick the box") {
		t.Error("the form must come back with the error attached")
	}
	if !rec.has(ActionActivationFailed) {
		t.Error("a refused activation must leave a trace (§4.6)")
	}
}

// TestSubmit_RecordsConsentAndIssuesNoSession is ADR 0025's first half: the
// wizard's consent POST records consent, binds it to this browser and moves on to
// the get-ready step — and it consumes NOTHING and issues NO session.
func TestSubmit_RecordsConsentAndIssuesNoSession(t *testing.T) {
	inv := &fakeInvites{}
	sess := &fakeSessions{}
	rec := &fakeAudit{}
	h := newHandler(t, inv, sess, rec)

	w := post(t, h, consent(), codeCookie())

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (POST/redirect/GET keeps a refresh from re-posting)", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/activate?step=3" {
		t.Fatalf("Location = %q, want /activate?step=3", loc)
	}
	if inv.consentCalls != 1 {
		t.Fatalf("RecordConsent calls = %d, want 1", inv.consentCalls)
	}
	if inv.activateCalls != 0 {
		t.Fatal("the consent POST consumed the invitation; only the NFC tap may (ADR 0025)")
	}
	if sess.issued != 0 || sess.revoked != 0 {
		t.Fatalf("the consent POST issued %d / revoked %d sessions; it must do neither", sess.issued, sess.revoked)
	}
	if cookieNamed(w, session.CookieName) != nil {
		t.Fatal("the consent POST set a session cookie")
	}
	ck := cookieNamed(w, activationCookieName)
	if ck == nil {
		t.Fatal("the activation cookie was not rewritten with the consent binding")
	}
	parts := strings.Split(ck.Value, ".")
	if len(parts) != 3 || parts[0] != fakeCSRF || parts[1] != fakeCode || len(parts[2]) != 43 {
		t.Fatalf("the cookie must become <csrf>.<code>.<binding> with a fresh 43-char binding; got %d parts", len(parts))
	}
	if !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.MaxAge < 1 {
		t.Errorf("the rebound cookie lost a hardening attribute: HttpOnly=%v SameSite=%v MaxAge=%d", ck.HttpOnly, ck.SameSite, ck.MaxAge)
	}
	if !rec.has(ActionActivationConsented) || rec.has(ActionActivationCompleted) {
		t.Errorf("audit = %v, want activation.consented and NOT activation.completed", rec.actions())
	}

	// A SECOND consent mints a DIFFERENT binding: the browser that agreed last wins.
	w2 := post(t, h, consent(), codeCookie())
	if b2 := cookieNamed(w2, activationCookieName); b2 == nil || strings.Split(b2.Value, ".")[2] == parts[2] {
		t.Fatal("a repeated consent must mint a fresh binding")
	}
}

// TestTap_CompletesAConsentedActivation is ADR 0025's second half: the first NFC
// tap from the consenting browser verifies the SUN with the ADVANCING path,
// consumes the invitation, issues exactly one session, clears the activation
// cookie, records the plaque — and renders a confirmation with no button.
func TestTap_CompletesAConsentedActivation(t *testing.T) {
	inv := &fakeInvites{}
	sess := &fakeSessions{}
	rec := &fakeAudit{}
	ver := &fakeVerifier{}
	h := newHandlerWith(t, inv, sess, rec, handlerOpts{verifier: ver})

	w := doTap(t, h, activationTapURL, pendingCookie())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ver.calls != 1 {
		t.Fatalf("sun.Verify calls = %d, want 1 (the atomic advance, §4.4)", ver.calls)
	}
	if inv.activateCalls != 1 || sess.issued != 1 {
		t.Fatalf("activate=%d issued=%d, want 1/1", inv.activateCalls, sess.issued)
	}
	if c := cookieNamed(w, session.CookieName); c == nil || !c.HttpOnly {
		t.Fatal("no HttpOnly session cookie was set")
	}
	if c := cookieNamed(w, activationCookieName); c == nil || c.MaxAge >= 0 {
		t.Fatal("the spent activation cookie was left in the browser")
	}
	body := w.Body.String()
	for _, want := range []string{"Activation complete", "You can close this page", "tap the plaque again", "Maria Borg"} {
		if !strings.Contains(body, want) {
			t.Errorf("the confirmation does not say %q", want)
		}
	}
	if strings.Contains(body, "<button") || strings.Contains(body, "<form") {
		t.Error("the confirmation has a button; §9 says the next action is a physical tap")
	}
	var done *audit.Event
	for i := range rec.events {
		if rec.events[i].Action == ActionActivationCompleted {
			done = &rec.events[i]
		}
	}
	if done == nil {
		t.Fatalf("audit = %v, want activation.completed", rec.actions())
	}
	d := done.Detail.(activationDetail)
	if d.TagUID != testPlaqueUID || d.LocationID != testPlaqueWall.String() {
		t.Errorf("the completed row must name the plaque: tag_uid=%q location_id=%q", d.TagUID, d.LocationID)
	}
}

// TestTap_RefusalsActivateNothing: every way the activating tap can be refused —
// a replayed or forged SUN, a plaque out of service or on no wall, another
// employer's plaque, an unknown plaque, a QR scan — consumes nothing, issues
// nothing, is audited with its own reason and says what to do.
func TestTap_RefusalsActivateNothing(t *testing.T) {
	other := uuid.MustParse("99999999-9999-4999-8999-999999999999")
	for _, tc := range []struct {
		name       string
		target     string
		verify     func(sun.Params) (sun.Result, error)
		wantStatus int
		wantReason string
		wantTagUID string
		wantWords  string
		wantVerify int
	}{
		{"replayed or forged sun", activationTapURL, func(sun.Params) (sun.Result, error) {
			r := genuineTap()
			r.SUNValid = false
			return r, nil
		}, http.StatusBadRequest, "sun_invalid", testPlaqueUID, "didn't finish setup", 1},
		{"retired plaque", activationTapURL, func(sun.Params) (sun.Result, error) {
			r := genuineTap()
			r.SUNValid, r.Tag.Status = false, "retired"
			return r, nil
		}, http.StatusConflict, "tag_not_active", testPlaqueUID, "isn't in service", 0},
		{"lost plaque", activationTapURL, func(sun.Params) (sun.Result, error) {
			r := genuineTap()
			r.SUNValid, r.Tag.Status = false, "lost"
			return r, nil
		}, http.StatusConflict, "tag_not_active", testPlaqueUID, "isn't in service", 0},
		{"plaque on no wall", activationTapURL, func(sun.Params) (sun.Result, error) {
			r := genuineTap()
			r.SUNValid, r.Tag.Status, r.Location, r.Tag.LocationID = false, "unassigned", nil, nil
			return r, nil
		}, http.StatusConflict, "tag_not_active", testPlaqueUID, "isn't in service", 0},
		// R5: another employer's plaque is refused on the NON-advancing preview, so
		// its counter is never touched — and with the same screen and status as an
		// unknown plaque or a bad signature (§4.7).
		{"another employer's plaque", activationTapURL, func(sun.Params) (sun.Result, error) {
			r := genuineTap()
			r.Tag.TenantID = other
			return r, nil
		}, http.StatusBadRequest, "foreign_tenant_tag", "", "didn't finish setup", 0},
		{"another employer's plaque with a forged signature", activationTapURL, func(sun.Params) (sun.Result, error) {
			r := genuineTap()
			r.Tag.TenantID, r.SUNValid = other, false
			return r, nil
		}, http.StatusBadRequest, "foreign_tenant_tag", "", "didn't finish setup", 0},
		{"unknown plaque", activationTapURL, func(sun.Params) (sun.Result, error) {
			return sun.Result{}, sun.ErrUnknownTag
		}, http.StatusBadRequest, "unknown_tag", "", "didn't finish setup", 0},
		{"a QR scan cannot activate", activationQRURL, nil, http.StatusBadRequest, "no_sun", "", "Hold your phone against the plaque", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := &fakeInvites{}
			sess := &fakeSessions{}
			rec := &fakeAudit{}
			ver := &fakeVerifier{verify: tc.verify}
			h := newHandlerWith(t, inv, sess, rec, handlerOpts{verifier: ver})

			w := doTap(t, h, tc.target, pendingCookie())

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if ver.calls != tc.wantVerify {
				t.Errorf("ADVANCING sun.Verify calls = %d, want %d", ver.calls, tc.wantVerify)
			}
			if inv.activateCalls != 0 || sess.issued != 0 || sess.revoked != 0 {
				t.Fatalf("a refused tap activated=%d issued=%d revoked=%d", inv.activateCalls, sess.issued, sess.revoked)
			}
			if cookieNamed(w, session.CookieName) != nil {
				t.Fatal("a refused tap set a session cookie")
			}
			if !strings.Contains(strings.ReplaceAll(w.Body.String(), "&#39;", "'"), tc.wantWords) {
				t.Errorf("the refusal does not say %q", tc.wantWords)
			}
			if len(rec.events) != 1 || rec.events[0].Action != ActionActivationFailed {
				t.Fatalf("audit = %v, want exactly one activation.failed", rec.actions())
			}
			d := rec.events[0].Detail.(activationDetail)
			if d.Reason != tc.wantReason || d.TagUID != tc.wantTagUID {
				t.Errorf("reason=%q tag_uid=%q, want %q/%q", d.Reason, d.TagUID, tc.wantReason, tc.wantTagUID)
			}
		})
	}
}

// TestTap_DeadInvitationIsRefusedAndClearsTheCookie: an invitation that expired
// (or was used, or cancelled) between consent and tap cannot complete, and the
// cookie that can no longer do anything is dropped so the phone's next tap is an
// ordinary one. The plaque's counter is never touched for it.
func TestTap_DeadInvitationIsRefusedAndClearsTheCookie(t *testing.T) {
	for _, sentinel := range []error{invite.ErrCodeExpired, invite.ErrCodeUsed, invite.ErrCodeCancelled, invite.ErrNotActivatable} {
		t.Run(sentinel.Error(), func(t *testing.T) {
			inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return okContext("invited"), sentinel }}
			sess := &fakeSessions{}
			rec := &fakeAudit{}
			ver := &fakeVerifier{}
			h := newHandlerWith(t, inv, sess, rec, handlerOpts{verifier: ver})

			w := doTap(t, h, activationTapURL, pendingCookie())
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
			if ver.calls != 0 || inv.activateCalls != 0 || sess.issued != 0 {
				t.Fatalf("verify=%d activate=%d issued=%d, want 0/0/0", ver.calls, inv.activateCalls, sess.issued)
			}
			if c := cookieNamed(w, activationCookieName); c == nil || c.MaxAge >= 0 {
				t.Error("a dead invitation's cookie must be cleared")
			}
			if !rec.has(ActionActivationFailed) {
				t.Errorf("audit = %v, want activation.failed", rec.actions())
			}
		})
	}
}

// TestTap_ConsentMissingSendsBackToTheWizard: the row's consent belongs to another
// browser (it consented later), so this browser's binding no longer matches.
// Nothing is activated, the stale binding is dropped from the cookie, and the
// person is told to go through the steps again.
func TestTap_ConsentMissingSendsBackToTheWizard(t *testing.T) {
	inv := &fakeInvites{activate: func(invite.Code) (invite.Activation, error) {
		return invite.Activation{Context: okContext("invited")}, invite.ErrConsentMissing
	}}
	sess := &fakeSessions{}
	rec := &fakeAudit{}
	h := newHandler(t, inv, sess, rec)

	w := doTap(t, h, activationTapURL, pendingCookie())
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
	if sess.issued != 0 {
		t.Fatal("a consent-less activation issued a session")
	}
	if !strings.Contains(w.Body.String(), "Finish the setup steps first") {
		t.Error("the refusal must send the person back through the wizard")
	}
	ck := cookieNamed(w, activationCookieName)
	if ck == nil || strings.Count(ck.Value, ".") != 1 || ck.MaxAge < 1 {
		t.Fatal("the cookie must keep the code and drop the stale binding")
	}
}

// TestTap_WithoutConsentIsNotPending: a cookie WITHOUT a binding — the state of a
// browser that has not agreed yet, and of a PLANTED cookie (cookies.go, measure 3)
// — does not make a tap an activation. It falls through to §5 row 3 (the wizard),
// and the plaque's counter is not touched.
func TestTap_WithoutConsentIsNotPending(t *testing.T) {
	inv := &fakeInvites{}
	sess := &fakeSessions{}
	ver := &fakeVerifier{}
	h := newHandlerWith(t, inv, sess, &fakeAudit{}, handlerOpts{verifier: ver})

	for _, c := range []*http.Cookie{codeCookie(), nil} {
		var w *httptest.ResponseRecorder
		if c == nil {
			w = doTap(t, h, activationTapURL)
		} else {
			w = doTap(t, h, activationTapURL, c)
		}
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != activationFromTap {
			t.Errorf("status = %d Location = %q, want 303 /activate", w.Code, w.Header().Get("Location"))
		}
	}
	if ver.calls != 0 || inv.activateCalls != 0 || sess.issued != 0 {
		t.Fatalf("an unconsented tap reached verify=%d activate=%d issue=%d", ver.calls, inv.activateCalls, sess.issued)
	}
	for _, v := range []string{"", ".", "a.b.c.d", "a..c", ".b.c"} {
		r := httptest.NewRequest(http.MethodGet, activationTapURL, nil)
		r.AddCookie(&http.Cookie{Name: activationCookieName, Value: v})
		if st, ok := (codeCookies{}).read(r); ok && st.pending() {
			t.Errorf("cookie %q read as a pending activation", v)
		}
	}
}

// TestTap_SecondDeviceRevokesBeforeIssuing is the ORDER test, and the order is
// the whole decision: RevokeAllForEmployee kills every LIVE session of the
// employee, so issuing first would kill the session just issued and the new phone
// would be signed out at the very tap that activated it.
func TestTap_SecondDeviceRevokesBeforeIssuing(t *testing.T) {
	var steps []string
	inv := &fakeInvites{
		steps: &steps,
		activate: func(invite.Code) (invite.Activation, error) {
			return invite.Activation{Context: okContext("active"), SecondDeviceReplaced: true}, nil
		},
	}
	sess := &fakeSessions{steps: &steps}
	rec := &fakeAudit{}
	h := newHandler(t, inv, sess, rec)

	w := doTap(t, h, activationTapURL, pendingCookie())

	if got := strings.Join(steps, ","); got != "activate,revoke,issue" {
		t.Fatalf("order = %q, want activate,revoke,issue", got)
	}
	if !strings.Contains(w.Body.String(), "Your other phone has been signed out") {
		t.Error("the confirmation must say the other phone was signed out")
	}
	if !rec.has(ActionDeviceReplaced) || !rec.has(ActionActivationCompleted) {
		t.Errorf("audit = %v, want activation.device_replaced and activation.completed", rec.actions())
	}
}

// TestSubmit_SecondDeviceWarnsOnTheForm: before consent, an already-active
// employee is told what is about to happen to their other phone.
func TestSubmit_SecondDeviceWarnsOnTheForm(t *testing.T) {
	inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return okContext("active"), nil }}
	h := newHandler(t, inv, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate", codeCookie()).Body.String()
	if !strings.Contains(body, "This is a new phone") {
		t.Fatal("a second activation must warn before it signs the other phone out")
	}
}

// TestSubmit_WithoutTheCookieIsRefused: no activation cookie means either the
// link was never opened here or the POST is cross-site (SameSite=Lax withholds
// the cookie). Both are refused before anything is consumed.
func TestSubmit_WithoutTheCookieIsRefused(t *testing.T) {
	inv := &fakeInvites{}
	h := newHandler(t, inv, &fakeSessions{}, &fakeAudit{})

	w := post(t, h, consent())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if inv.activateCalls != 0 || inv.consentCalls != 0 {
		t.Fatal("a code was consumed (or consented) without one being presented")
	}
}

// TestTap_SessionIssueFailureIsLoud: the code is already spent, so this cannot
// be dressed up as "your link is invalid" — that would send the employee to their
// manager with the wrong story and hide a server fault.
func TestTap_SessionIssueFailureIsLoud(t *testing.T) {
	rec := &fakeAudit{}
	sess := &fakeSessions{issueErr: errors.New("boom")}
	h := newHandler(t, &fakeInvites{}, sess, rec)

	w := doTap(t, h, activationTapURL, pendingCookie())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "Ask your manager for a new one") {
		t.Fatal("a server fault must not be reported as an invalid link")
	}
	if !rec.has(ActionActivationFailed) {
		t.Errorf("audit = %v, want a row recording the consumed-but-unusable invitation", rec.actions())
	}
}

// TestRateLimit_PerInviteTripsAndIsAudited: the narrow, activation-only limit the
// card requires — and the 429 leaves a row, because a tenant is known by then.
func TestRateLimit_PerInviteTripsAndIsAudited(t *testing.T) {
	rec := &fakeAudit{}
	inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
		return okContext("invited"), invite.ErrCodeUsed
	}}
	h := newHandler(t, inv, &fakeSessions{}, rec)

	// Legitimate traffic never reaches the limit: only FAILURES count, so drive
	// exactly the number of failures the window allows.
	for i := 0; i < inviteFailureLimit; i++ {
		if w := post(t, h, consent(), codeCookie()); w.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: status = %d, want 400", i, w.Code)
		}
	}
	w := post(t, h, consent(), codeCookie())
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after %d failures", w.Code, inviteFailureLimit)
	}
	if !strings.Contains(w.Body.String(), "Too many attempts") {
		t.Error("the visitor should be told to wait, not that the link is broken")
	}
	if !rec.has(ActionActivationLimited) {
		t.Errorf("audit = %v, want activation.rate_limited — a 429 must not vanish (§4.6)", rec.actions())
	}
}

// TestRateLimit_SuccessNeverConsumesBudget is the "meşru akış sınıra değmez"
// property, measured rather than asserted by choosing a big number: neither a
// consent nor a completed activation costs anything, so a whole venue onboarding
// cannot trip the limit.
func TestRateLimit_SuccessNeverConsumesBudget(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for i := 0; i < inviteFailureLimit*3; i++ {
		if w := post(t, h, consent(), codeCookie()); w.Code != http.StatusSeeOther {
			t.Fatalf("consent %d was throttled: status %d", i, w.Code)
		}
		if w := doTap(t, h, activationTapURL, pendingCookie()); w.Code != http.StatusOK {
			t.Fatalf("activating tap %d was throttled: status %d", i, w.Code)
		}
	}
}

// TestStatus_ReportsTheStateWithoutNamingAnybody is the waiting screen's poll:
// "waiting" from the consent binding alone, "done" ONLY when the tappa_activated
// marker names the live session this browser holds (the activation THIS browser
// completed), "none" otherwise — including any OTHER live session. It carries no
// name and no id, is never cached, and is metered on its own budget.
func TestStatus_ReportsTheStateWithoutNamingAnybody(t *testing.T) {
	read := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		var body struct{ State string }
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("status body is not JSON: %v (%q)", err, w.Body.String())
		}
		return body.State
	}
	sid := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	live := &fakeSessions{sessionID: sid}
	h := newHandler(t, &fakeInvites{}, live, &fakeAudit{})
	sessionCookie := &http.Cookie{Name: session.CookieName, Value: "FAKEsessionFAKEsessionFAKEsessionFAKEsess12"}
	marker := &http.Cookie{Name: activatedCookieName, Value: sid.String()}
	otherMarker := &http.Cookie{Name: activatedCookieName, Value: uuid.New().String()}

	for _, tc := range []struct {
		name    string
		cookies []*http.Cookie
		want    string
	}{
		{"consented, waiting", []*http.Cookie{pendingCookie()}, "waiting"},
		{"consented, and an older session on the phone", []*http.Cookie{pendingCookie(), sessionCookie}, "waiting"},
		{"activated here: marker names the live session", []*http.Cookie{sessionCookie, marker}, "done"},
		// THE SWITCH CASE: the phone holds somebody's live session but THIS browser
		// completed no activation — no marker, or a marker for another session.
		{"a live session but no marker (someone else's phone)", []*http.Cookie{sessionCookie}, "none"},
		{"a marker for a different session", []*http.Cookie{sessionCookie, otherMarker}, "none"},
		{"code without consent, plus a live session", []*http.Cookie{codeCookie(), sessionCookie}, "none"},
		{"a marker with no session", []*http.Cookie{marker}, "none"},
		{"nothing at all", nil, "none"},
	} {
		w := get(t, h, ActivationStatusPath, tc.cookies...)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d", tc.name, w.Code)
		}
		if got := read(w); got != tc.want {
			t.Errorf("%s: state = %q, want %q", tc.name, got, tc.want)
		}
		if w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: headers %v", tc.name, w.Header())
		}
		if strings.Contains(w.Body.String(), "Maria") || strings.Contains(w.Body.String(), testEmployee.String()) {
			t.Errorf("%s: the status names somebody: %s", tc.name, w.Body.String())
		}
	}

	revoked := &fakeSessions{sessionID: sid, verify: func() (session.Resolved, error) { return session.Resolved{}, session.ErrRevoked }}
	h2 := newHandler(t, &fakeInvites{}, revoked, &fakeAudit{})
	if got := read(get(t, h2, ActivationStatusPath, sessionCookie, marker)); got != "none" {
		t.Errorf("a revoked session: state = %q, want none", got)
	}

	// END TO END THROUGH THE HANDLER: the activating tap sets the marker for the
	// session it issued, and that marker makes the poll say done.
	h4 := newHandler(t, &fakeInvites{}, &fakeSessions{sessionID: sid}, &fakeAudit{})
	tw := doTap(t, h4, activationTapURL, pendingCookie())
	m := cookieNamed(tw, activatedCookieName)
	if m == nil || m.Value != sid.String() || !m.HttpOnly || m.MaxAge != activatedCookieMaxAge {
		t.Fatalf("the activating tap did not set the activated marker for its session: %+v", m)
	}
	if got := read(get(t, h4, ActivationStatusPath, sessionCookie, m)); got != "done" {
		t.Errorf("after the tap: state = %q, want done", got)
	}

	// Its own budget: past statusLimit the poll is refused, and the activation
	// flow from the same address is NOT (the flood ceiling was not charged).
	h3 := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for i := 0; i < statusLimit; i++ {
		get(t, h3, ActivationStatusPath, pendingCookie())
	}
	if w := get(t, h3, ActivationStatusPath, pendingCookie()); w.Code != http.StatusTooManyRequests {
		t.Fatalf("past statusLimit: status %d, want 429", w.Code)
	}
	if w := get(t, h3, "/activate?step=4", pendingCookie()); w.Code != http.StatusOK {
		t.Fatalf("polling spent the flood ceiling: the wizard answered %d", w.Code)
	}
}

// TestPage_LandingWithoutALinkSaysWhatIsTrue covers the three people who reach
// /activate with no activation cookie (third eye #2, #3): an already set-up phone
// is told so; a tap from a browser that never ran the wizard is told to open the
// link HERE (the link still works); anybody else gets "you need your link".
func TestPage_LandingWithoutALinkSaysWhatIsTrue(t *testing.T) {
	sessionCookie := &http.Cookie{Name: session.CookieName, Value: "FAKEsessionFAKEsessionFAKEsessionFAKEsess12"}
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})

	if body := get(t, h, "/activate", sessionCookie).Body.String(); !strings.Contains(body, "This phone is already set up") ||
		strings.Contains(body, "Ask your manager") {
		t.Error("an activated phone reloading /activate must be told it is already set up")
	}
	if body := get(t, h, activationFromTap).Body.String(); !strings.Contains(body, "Finish setup in this browser") ||
		strings.Contains(body, "Ask your manager") {
		t.Error("a tap from a browser without the wizard must be told to open the link here")
	}
	if body := get(t, h, "/activate").Body.String(); !strings.Contains(body, "You need your activation link") {
		t.Error("a bare visit keeps the original landing")
	}

	// A SPENT link reopened on the phone holding that employee's session: already
	// set up, recorded, and not charged to the invitation's budget.
	rec := &fakeAudit{}
	used := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return okContext("active"), invite.ErrCodeUsed }}
	hu := newHandler(t, used, &fakeSessions{employeeID: testEmployee}, rec)
	for i := 0; i < inviteFailureLimit+2; i++ {
		w := get(t, hu, "/activate?code="+fakeCode, sessionCookie)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "This phone is already set up") {
			t.Fatalf("reopen %d: status %d, want the already-set-up page", i, w.Code)
		}
	}
	if !rec.has(ActionActivationFailed) {
		t.Error("the spent-code attempt must still leave a trace (§4.6)")
	}
	// ...but on ANOTHER employee's phone the same spent link is the generic refusal.
	hx := newHandler(t, used, &fakeSessions{employeeID: uuid.New()}, &fakeAudit{})
	if w := get(t, hx, "/activate?code="+fakeCode, sessionCookie); w.Code != http.StatusBadRequest {
		t.Errorf("a spent link on someone else's phone answered %d, want 400", w.Code)
	}
}

// TestWizard_NoStrayPunctuationAfterNames: an employer name that ends in a full
// stop ("Kebab Factory Ltd.") must not produce "Ltd. ." or "Ltd.." anywhere on
// the wizard (third eye #5).
func TestWizard_NoStrayPunctuationAfterNames(t *testing.T) {
	ltd := func(status string) func(invite.Code) (invite.Context, error) {
		return func(invite.Code) (invite.Context, error) {
			c := okContext(status)
			c.TenantName = "Kebab Factory Ltd."
			return c, nil
		}
	}
	victim := &http.Cookie{Name: session.CookieName, Value: victimSession}
	for _, tc := range []struct {
		inv    *fakeInvites
		sess   *fakeSessions
		target string
		cookie []*http.Cookie
	}{
		{&fakeInvites{lookup: ltd("invited")}, &fakeSessions{}, "/activate?step=1", []*http.Cookie{codeCookie()}},
		{&fakeInvites{lookup: ltd("invited")}, &fakeSessions{}, "/activate?step=2", []*http.Cookie{codeCookie()}},
		{&fakeInvites{lookup: ltd("invited")}, &fakeSessions{}, "/activate?step=4", []*http.Cookie{pendingCookie()}},
		{&fakeInvites{lookup: ltd("active")}, &fakeSessions{employeeID: uuid.New()}, "/activate?step=2", []*http.Cookie{codeCookie(), victim}},
	} {
		h := newHandler(t, tc.inv, tc.sess, &fakeAudit{})
		text := strings.Join(strings.Fields(screenText(t, get(t, h, tc.target, tc.cookie...).Body.String())), " ")
		for _, bad := range []string{" .", "..", " ,", "( "} {
			if strings.Contains(text, bad) {
				t.Errorf("%s renders %q: %s", tc.target, bad, text)
			}
		}
	}
}

// TestNoResponseBodyEverCarriesTheCode drives the whole flow and greps every
// byte of every body for the code AND the consent binding. The Set-Cookie header
// is the ONE place either appears, by construction.
func TestNoResponseBodyEverCarriesTheCode(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})

	responses := []*httptest.ResponseRecorder{
		get(t, h, "/activate?code="+fakeCode),
		get(t, h, "/activate", codeCookie()),
		get(t, h, "/activate?step=2", codeCookie()),
		post(t, h, url.Values{"csrf": {fakeCSRF}}, codeCookie()), // consent missing: re-renders the form
		post(t, h, consent(), codeCookie()),
		get(t, h, "/activate?step=3", pendingCookie()),
		get(t, h, "/activate?step=4", pendingCookie()),
		get(t, h, ActivationStatusPath, pendingCookie()),
		doTap(t, h, activationTapURL, pendingCookie()),
	}
	for i, w := range responses {
		for _, secret := range []string{fakeCode, fakeBinding} {
			if strings.Contains(w.Body.String(), secret) {
				t.Errorf("response %d carries a credential in its BODY", i)
			}
		}
		if w.Body.Len() == 0 && w.Code != http.StatusSeeOther {
			t.Errorf("response %d is empty; the assertion above proves nothing", i)
		}
		for name, values := range w.Header() {
			if name == "Set-Cookie" {
				continue // the declared, intended carrier
			}
			for _, v := range values {
				if strings.Contains(v, fakeCode) || strings.Contains(v, fakeBinding) {
					t.Errorf("response %d leaks a credential in header %s", i, name)
				}
			}
		}
	}
	// Positive control: the code IS findable where it is supposed to be, so the
	// negative assertions above are not passing because the value never existed.
	if !strings.Contains(strings.Join(responses[0].Header().Values("Set-Cookie"), " "), fakeCode) {
		t.Fatal("positive control failed: the activation cookie does not carry the code")
	}
}

// TestNoCacheHeaders: these pages are personal and are reached from a
// credential-bearing URL.
func TestNoCacheHeaders(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for _, w := range []*httptest.ResponseRecorder{
		get(t, h, "/activate", codeCookie()),
		get(t, h, "/activate?code="+fakeCode),
		post(t, h, consent(), codeCookie()),
		get(t, h, "/activate?step=4", pendingCookie()),
		get(t, h, ActivationStatusPath, pendingCookie()),
		doTap(t, h, activationTapURL, pendingCookie()),
	} {
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	}
}

// TestCoarseDevice covers the §4.1 boundary: what reaches sessions.device_info is
// a value from a fixed vocabulary, never a byte the client chose.
func TestCoarseDevice(t *testing.T) {
	cases := []struct{ name, ua, want string }{
		{"iphone safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1", "iPhone Safari"},
		{"android chrome", "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36", "Android Chrome"},
		{"samsung wins over chrome", "Mozilla/5.0 (Linux; Android 13; SAMSUNG SM-S918B) AppleWebKit/537.36 SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36", "Android Samsung Internet"},
		{"edge wins over chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124.0.0.0 Safari/537.36 Edg/124.0.0.0", "Windows Edge"},
		{"firefox ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 FxiOS/125.0 Mobile/15E148 Safari/605.1.15", "iPhone Firefox"},
		{"empty", "", ""},
		{"junk", "!!!", ""},
		{"attacker text is not stored", strings.Repeat("A", 5000), ""},
		{"control characters never survive", "iPhone\x00\x1b[31m Safari/604", "iPhone Safari"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := coarseDevice(tc.ua)
			if got != tc.want {
				t.Fatalf("coarseDevice = %q, want %q", got, tc.want)
			}
			// Whatever comes out must be one of the fixed labels: no byte of the
			// input may survive into the column.
			if got != "" && strings.ContainsAny(got, "\x00\x1b<>&\"'") {
				t.Fatalf("label %q carries input bytes", got)
			}
		})
	}
}

// TestCodeCookies_ZeroValueIsSecure is the M5-01 round-3 lesson applied to the
// second cookie in the codebase: the DANGEROUS state must not be the zero value,
// because Go lets any package obtain a zero struct even when it cannot name the
// field.
func TestCodeCookies_ZeroValueIsSecure(t *testing.T) {
	var zero codeCookies
	if !zero.secure() {
		t.Fatal("the zero codeCookies writes a cookie without Secure")
	}
	if !(codeCookies{}).secure() {
		t.Fatal("an empty composite literal writes a cookie without Secure")
	}
	// A handler struct that simply forgets the field — not a compile error in Go.
	var forgotten struct {
		name  string
		codes codeCookies
	}
	forgotten.name = "x"
	if !forgotten.codes.secure() {
		t.Fatal("a struct field left unwritten writes a cookie without Secure")
	}
	// Production config is always Secure.
	if !newCodeCookies(&config.Config{Env: config.EnvProd, BaseURL: "http://oops"}).secure() {
		t.Fatal("prod must be Secure regardless of BaseURL")
	}
	// Positive control: the ONE relaxation still exists, so the assertions above
	// are not passing because secure() is hard-wired to true.
	if newCodeCookies(&config.Config{Env: config.EnvDev, BaseURL: "http://localhost:8080"}).secure() {
		t.Fatal("dev over http should relax, or this test proves nothing")
	}
}

// TestNoLogLineEverCarriesTheCode captures the handler's REAL log output while
// driving every branch that logs, and greps it. §7 lists the invite code among
// the values that are never logged, and §4.7 puts the code HASH in the same
// class (it is a bearer credential: hash -> resolver -> tenant -> consumption),
// so both shapes are checked.
//
// scripts/redline-check.sh R7 is a mechanical net over the SHAPE of a log call;
// this is the behavioural half — it would catch a leak through a neutrally-named
// variable, which the scanner cannot see.
func TestNoLogLineEverCarriesTheCode(t *testing.T) {
	var logged strings.Builder

	// One shared log sink, one router per branch, so every logging path in the
	// flow writes into the same buffer.
	build := func(inv *fakeInvites, sess *fakeSessions) http.Handler {
		t.Helper()
		sess.tok = sess.token(t)
		cfg := &config.Config{Env: config.EnvDev, BaseURL: "http://localhost:8080", RetentionYears: 2}
		a, err := NewActivation(inv, sess, &fakeVerifier{}, &fakeAudit{}, cfg,
			slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
		if err != nil {
			t.Fatalf("NewActivation: %v", err)
		}
		r := chi.NewRouter()
		a.Mount(r)
		r.Get(TapPath, func(w http.ResponseWriter, r *http.Request) {
			p, err := sun.Parse(r.URL.Query())
			if err == nil && a.Pending(r) {
				a.CompleteByTap(w, r, p)
			}
		})
		return r
	}

	// Happy path (which logs nothing) plus every branch that does.
	happy := build(&fakeInvites{}, &fakeSessions{})
	get(t, happy, "/activate?code="+fakeCode)
	post(t, happy, consent(), codeCookie())
	doTap(t, happy, activationTapURL, pendingCookie())

	for _, e := range []error{
		invite.ErrUnknownCode,
		invite.ErrCodeExpired,
		invite.ErrCodeUsed,
		invite.ErrNotActivatable,
		errors.New("db down"),
	} {
		err := e
		inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
			if errors.Is(err, invite.ErrUnknownCode) {
				return invite.Context{}, err
			}
			return okContext("invited"), err
		}}
		h := build(inv, &fakeSessions{})
		get(t, h, "/activate?code="+fakeCode)
		post(t, h, consent(), codeCookie())
		doTap(t, h, activationTapURL, pendingCookie())
	}

	// A session-issue failure, and a rate-limit trip.
	broken := build(&fakeInvites{}, &fakeSessions{issueErr: errors.New("boom")})
	doTap(t, broken, activationTapURL, pendingCookie())

	limited := build(&fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
		return okContext("invited"), invite.ErrCodeUsed
	}}, &fakeSessions{})
	for i := 0; i < inviteFailureLimit+2; i++ {
		post(t, limited, consent(), codeCookie())
	}

	out := logged.String()
	if out == "" {
		t.Fatal("nothing was logged: the assertions below would prove nothing")
	}
	if strings.Contains(out, fakeCode) {
		t.Fatal("a log line carries the raw invite code")
	}
	if strings.Contains(out, fakeBinding) || strings.Contains(out, fakeBinding[:8]) {
		t.Fatal("a log line carries the consent binding")
	}
	if strings.Contains(out, fakeCode[:8]) {
		t.Fatal("a log line carries a prefix of the invite code")
	}
	if hexRun(out) {
		t.Fatal("a log line carries a 64-hex value, which is the shape of a code hash")
	}
	// Positive controls: the buffer really holds this handler's output, and it
	// really reached the branches that are supposed to be loud.
	for _, want := range []string{"activation attempt rejected", "activation rate limited", "issuing the session failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("the captured log never reached %q; the negative assertions cover less than they claim", want)
		}
	}
}

// --- B1: activation fixation -------------------------------------------------
//
// The audit drove this end to end against the first implementation and it
// WORKED: a cross-site GET planted the attacker's code in the victim's browser,
// the next same-site GET rendered another tenant's form, and one same-site POST
// (which SameSite=Lax does carry) bound the victim's phone to a stranger's
// employee record while silently overwriting their session cookie.
//
// The tests below re-run that scenario step by step against the three measures.
// Each one names which measure it exercises, so removing a measure fails a
// specific test rather than "something".

// victimSession is the cookie value standing in for the victim's live session.
const victimSession = "VICTIMsessionVICTIMsessionVICTIMsessio1234"

// attackerContext is an invite belonging to a DIFFERENT tenant — the payload of
// the fixation attack.
//
// The invite id is FIXED, not fresh per call: an attacker replaying one link
// presents the same invitation every time, and that is what the rate-limit
// meters key on. A fixture that minted a new id per request would silently
// measure a different scenario (see the note on auditRowsAreBounded).
func attackerContext() invite.Context {
	return invite.Context{
		TenantID:     uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		EmployeeID:   uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		InviteID:     uuid.MustParse("77777777-7777-4777-8777-777777777777"),
		LocationID:   uuid.New(),
		FullName:     "Probe Beta Employee",
		TenantName:   "Probe Beta Ltd",
		LocationName: "Beta",
		Status:       "invited",
	}
}

// victimHolds makes the fake session layer report a live session for someone
// else — the victim whose phone is being hijacked.
func victimHolds(t *testing.T) (*fakeInvites, *fakeSessions) {
	t.Helper()
	victim := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return attackerContext(), nil }}
	sess := &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{ID: uuid.New(), TenantID: testTenant, EmployeeID: victim}, nil
	}}
	return inv, sess
}

// TestB1_CrossSiteGetPlantsNothingOnAnOccupiedPhone — MEASURE 3.
func TestB1_CrossSiteGetPlantsNothingOnAnOccupiedPhone(t *testing.T) {
	inv, sess := victimHolds(t)
	rec := &fakeAudit{}
	h := newHandler(t, inv, sess, rec)

	req := httptest.NewRequest(http.MethodGet, "/activate?code="+fakeCode, nil)
	req.RemoteAddr = "203.0.113.9:5000"
	req.Header.Set("Referer", "https://evil.example/")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: victimSession})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	for _, c := range w.Result().Cookies() {
		if c.Name == activationCookieName && c.MaxAge > 0 {
			t.Fatal("a cross-site GET planted an activation cookie on a phone that already has a session")
		}
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the confirmation step)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "This phone is already in use") {
		t.Fatal("the visitor must be shown an explicit confirmation, not the form")
	}
	if !rec.has(ActionActivationBlocked) {
		t.Errorf("audit = %v, want activation.blocked", rec.actions())
	}

	// POSITIVE CONTROL: the SAME request without the cross-site header is the
	// normal flow (a link tapped in a chat app on a fresh phone) and must still
	// set the cookie — otherwise this test would be passing because activation is
	// broken for everybody.
	ok := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	if w2 := get(t, ok, "/activate?code="+fakeCode); w2.Code != http.StatusSeeOther {
		t.Fatalf("the ordinary flow now answers %d; measure 3 has broken it", w2.Code)
	}
}

// TestB1_ForgedPostWithoutTheTokenIsRefused — MEASURE 1. This is the step the
// attacker must take even after planting a cookie, and it is the step they
// cannot: the token was generated by the server and never left the victim's
// browser.
func TestB1_ForgedPostWithoutTheTokenIsRefused(t *testing.T) {
	inv, sess := victimHolds(t)
	rec := &fakeAudit{}
	h := newHandler(t, inv, sess, rec)

	victimCookie := &http.Cookie{Name: session.CookieName, Value: victimSession}
	w := post(t, h, consentNoToken(), codeCookie(), victimCookie)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if inv.activateCalls != 0 || sess.issued != 0 {
		t.Fatal("a submission with no synchronizer token completed the activation")
	}
	if !rec.has(ActionActivationFailed) {
		t.Errorf("audit = %v, want the attempt recorded", rec.actions())
	}
	// A GUESSED token must fail too, and the comparison is constant-time.
	w2 := post(t, h, url.Values{"consent": {"yes"}, "csrf": {"WRONGtokenWRONGtokenWRONGtokenWRONGtoke123"}}, codeCookie(), victimCookie)
	if w2.Code != http.StatusBadRequest || sess.issued != 0 {
		t.Fatal("a wrong synchronizer token was accepted")
	}
}

// TestB1_TakeoverNeedsAnExplicitConfirmation — MEASURE 2. Even a victim who is
// phished into pressing the button on a page bearing a stranger's name does not
// lose their session silently: the form demands a second, separate tick that says
// what is about to happen.
func TestB1_TakeoverNeedsAnExplicitConfirmation(t *testing.T) {
	inv, sess := victimHolds(t)
	rec := &fakeAudit{}
	// The plaque the hand-over is completed on belongs to the OFFERED employee's
	// employer — the same tenant as the invitation, or the tap is refused.
	ver := &fakeVerifier{verify: func(sun.Params) (sun.Result, error) {
		r := genuineTap()
		r.Tag.TenantID = attackerContext().TenantID
		return r, nil
	}}
	h := newHandlerWith(t, inv, sess, rec, handlerOpts{verifier: ver})

	victimCookie := &http.Cookie{Name: session.CookieName, Value: victimSession}
	w := post(t, h, consent(), codeCookie(), victimCookie)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if inv.activateCalls != 0 || inv.consentCalls != 0 || sess.issued != 0 {
		t.Fatal("the activation replaced another employee's session without being told to")
	}
	body := w.Body.String()
	if !strings.Contains(body, "This phone belongs to someone else") {
		t.Error("the form must name the conflict")
	}
	if !strings.Contains(body, "Probe Beta Employee") || !strings.Contains(body, "Probe Beta Ltd") {
		t.Error("the form must show WHOSE activation this is, so a takeover cannot be mistaken for a normal one")
	}
	if !rec.has(ActionActivationBlocked) {
		t.Errorf("audit = %v, want activation.blocked", rec.actions())
	}

	// With the explicit tick the CONSENT goes through — a genuine hand-over of a
	// shared phone must remain possible — and the hand-over itself happens on the
	// tap, which takes precedence over the session the phone still carries.
	w2 := post(t, h, url.Values{"consent": {"yes"}, "csrf": {fakeCSRF}, "switch": {"yes"}}, codeCookie(), victimCookie)
	if w2.Code != http.StatusSeeOther || inv.consentCalls != 1 || sess.issued != 0 {
		t.Fatalf("a confirmed switch: status %d, consent %d, issued %d — want 303/1/0", w2.Code, inv.consentCalls, sess.issued)
	}
	w3 := doTap(t, h, activationTapURL, pendingCookie(), victimCookie)
	if w3.Code != http.StatusOK || sess.issued != 1 {
		t.Fatalf("the confirmed hand-over's tap: status %d, issued %d — want 200/1", w3.Code, sess.issued)
	}
}

// TestB1_FullAuditScenarioNowFails re-runs the audit's three steps in order.
func TestB1_FullAuditScenarioNowFails(t *testing.T) {
	inv, sess := victimHolds(t)
	h := newHandler(t, inv, sess, &fakeAudit{})

	// STEP 1 — cross-site GET.
	req := httptest.NewRequest(http.MethodGet, "/activate?code="+fakeCode, nil)
	req.RemoteAddr = "203.0.113.9:5000"
	req.Header.Set("Referer", "https://evil.example/")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: victimSession})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == activationCookieName && c.MaxAge > 0 {
			t.Fatal("STEP 1 still plants the attacker's code")
		}
	}

	// STEP 2 — even if the cookie is planted some other way, the same-site GET
	// now renders the takeover warning rather than a clean form.
	req2 := httptest.NewRequest(http.MethodGet, "/activate", nil)
	req2.RemoteAddr = "203.0.113.9:5000"
	req2.AddCookie(codeCookie())
	req2.AddCookie(&http.Cookie{Name: session.CookieName, Value: victimSession})
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if !strings.Contains(w2.Body.String(), "This phone belongs to someone else") {
		t.Fatal("STEP 2 renders another tenant's form with no warning")
	}

	// STEP 3 — the completing POST is refused on two independent grounds.
	req3 := httptest.NewRequest(http.MethodPost, "/api/activate",
		strings.NewReader(consentNoToken().Encode()))
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req3.RemoteAddr = "203.0.113.9:5000"
	req3.AddCookie(codeCookie())
	req3.AddCookie(&http.Cookie{Name: session.CookieName, Value: victimSession})
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)
	if w3.Code == http.StatusSeeOther || sess.issued != 0 || inv.consentCalls != 0 {
		t.Fatalf("STEP 3 completed the takeover: status %d, sessions issued %d, consents %d", w3.Code, sess.issued, inv.consentCalls)
	}

	// STEP 4 (ADR 0025) — the victim taps a plaque while the planted code sits in
	// their browser. A planted cookie carries no consent binding, so the tap is
	// not an activation: nothing is verified, consumed or issued.
	w4 := doTap(t, h, activationTapURL, codeCookie(), &http.Cookie{Name: session.CookieName, Value: victimSession})
	if w4.Code == http.StatusOK || sess.issued != 0 || inv.activateCalls != 0 {
		t.Fatalf("STEP 4 activated a planted code on a tap: status %d, issued %d", w4.Code, sess.issued)
	}
}

// TestB1_ForeignOriginPostIsRefused: the Origin header is sent by every current
// browser on a cross-origin POST, so it is a real check for form submissions —
// unlike Sec-Fetch-Site, which older browsers omit.
func TestB1_ForeignOriginPostIsRefused(t *testing.T) {
	inv := &fakeInvites{}
	sess := &fakeSessions{}
	h := newHandler(t, inv, sess, &fakeAudit{})

	req := httptest.NewRequest(http.MethodPost, "/api/activate", strings.NewReader(consent().Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	req.RemoteAddr = "203.0.113.9:5000"
	req.AddCookie(codeCookie())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || sess.issued != 0 {
		t.Fatalf("a POST from a foreign origin was accepted: status %d", w.Code)
	}
	// Positive control: our OWN origin is accepted, so the check is a comparison
	// and not a blanket refusal.
	req2 := httptest.NewRequest(http.MethodPost, "/api/activate", strings.NewReader(consent().Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.Header.Set("Origin", "http://localhost:8080")
	req2.RemoteAddr = "203.0.113.9:5000"
	req2.AddCookie(codeCookie())
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("our own origin was refused: status %d", w2.Code)
	}
}

// TestB4_ConsentSlipDoesNotBurnTheInviteBudget: forgetting the box is a user
// slip, not abuse, and must not lock an employee out of their own link.
func TestB4_ConsentSlipDoesNotBurnTheInviteBudget(t *testing.T) {
	sess := &fakeSessions{}
	h := newHandler(t, &fakeInvites{}, sess, &fakeAudit{})

	// Three times the invite budget, all consent slips.
	for i := 0; i < inviteFailureLimit*3; i++ {
		w := post(t, h, url.Values{"csrf": {fakeCSRF}}, codeCookie())
		if w.Code != http.StatusBadRequest {
			t.Fatalf("slip %d answered %d, want 400 (a 429 means the slip burned the budget)", i, w.Code)
		}
	}
	// The employee can still give their consent.
	if w := post(t, h, consent(), codeCookie()); w.Code != http.StatusSeeOther {
		t.Fatalf("after %d consent slips the employee is locked out: status %d", inviteFailureLimit*3, w.Code)
	}

	// POSITIVE CONTROL: a genuine abuse signal DOES still burn the budget, so the
	// separation above is a distinction and not a disabled limiter.
	abusive := newHandler(t, &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
		return okContext("invited"), invite.ErrCodeUsed
	}}, &fakeSessions{}, &fakeAudit{})
	for i := 0; i < inviteFailureLimit; i++ {
		post(t, abusive, consent(), codeCookie())
	}
	if w := post(t, abusive, consent(), codeCookie()); w.Code != http.StatusTooManyRequests {
		t.Fatalf("replaying a dead code no longer trips the invite window: status %d", w.Code)
	}
}

// --- Audit-log flood: every refusal must charge a window ---------------------
//
// THE FINDING, in the auditor's numbers: two branches wrote an audit_log row per
// request and charged NOTHING. 300 GETs with a dead code produced 300 rows while
// 290 of them answered 429; 500 cross-site GETs produced 500 rows and zero 429s.
// The precondition was one dead invite link — the one every activated employee
// still has in their chat history — and audit_log is append-only IN THE DATABASE
// (even tappa_owner gets "append-only table audit_log: DELETE is forbidden"), so
// those rows are permanent. An unbounded writer into an indelible table is a
// denial of service against the trail §4.6 exists to keep.
//
// auditRowsAreBounded is the shape of the fix: rows written must stay near the
// window's limit no matter how many requests arrive.
func auditRowsAreBounded(t *testing.T, label string, rows, requests int) {
	t.Helper()
	// The invitation's window meters these rows, plus the ONE row written when it
	// trips. The ceiling is deliberately a little loose — what matters is that it
	// does not scale with the number of requests.
	//
	// SCOPE OF THIS BOUND, stated because the fixture makes it easy to miss: it is
	// PER INVITATION. A caller holding N distinct valid invitations can write
	// roughly N x this many rows, and what bounds THAT is the flood ceiling —
	// floodLimit requests per address per window, so never more rows than
	// requests. Both bounds are real and neither is the other.
	const ceiling = inviteFailureLimit + 2
	if rows > ceiling {
		t.Errorf("%s: %d requests wrote %d audit rows (ceiling %d) — the refusal is not charged to any window",
			label, requests, rows, ceiling)
	}
	if rows == 0 {
		t.Errorf("%s: no audit rows at all; §4.6 wants the attributable failures recorded", label)
	}
}

func TestFlood_DeadCodeOnGetIsBounded(t *testing.T) {
	rec := &fakeAudit{}
	h := newHandler(t, &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
		return okContext("invited"), invite.ErrCodeUsed
	}}, &fakeSessions{}, rec)

	const n = 300
	limited := 0
	for i := 0; i < n; i++ {
		if get(t, h, "/activate?code="+fakeCode).Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("nothing was rate limited; the bound below would prove nothing")
	}
	auditRowsAreBounded(t, "GET with a dead code", len(rec.events), n)
}

func TestFlood_DeadCodeOnPostIsBounded(t *testing.T) {
	rec := &fakeAudit{}
	h := newHandler(t, &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
		return okContext("invited"), invite.ErrCodeUsed
	}}, &fakeSessions{}, rec)

	const n = 400
	limited := 0
	for i := 0; i < n; i++ {
		if post(t, h, consent(), codeCookie()).Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("nothing was rate limited; the bound below would prove nothing")
	}
	auditRowsAreBounded(t, "POST with a dead code", len(rec.events), n)
}

func TestFlood_CrossSiteConflictIsBoundedAndLimited(t *testing.T) {
	rec := &fakeAudit{}
	inv, sess := victimHolds(t)
	h := newHandler(t, inv, sess, rec)

	const n = 500
	limited := 0
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodGet, "/activate?code="+fakeCode, nil)
		req.RemoteAddr = "203.0.113.9:5000"
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: victimSession})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			limited++
		}
	}
	// This branch used to return 200 forever and count nothing.
	if limited == 0 {
		t.Fatal("blocked cross-site attempts are still not rate limited at all")
	}
	auditRowsAreBounded(t, "cross-site conflict", len(rec.events), n)
}

// TestFlood_LegitimateFlowIsUnaffected is the positive control for all three:
// the bound must come from charging FAILURES, not from throttling everybody.
func TestFlood_LegitimateFlowIsUnaffected(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for i := 0; i < 200; i++ {
		if w := post(t, h, consent(), codeCookie()); w.Code != http.StatusSeeOther {
			t.Fatalf("successful consent %d was throttled: status %d", i, w.Code)
		}
	}
}

// TestFlood_RefusedTapsAreBounded: a loop of bad activating taps (a reloaded,
// replayed URL) writes rows only up to the invitation's window.
func TestFlood_RefusedTapsAreBounded(t *testing.T) {
	rec := &fakeAudit{}
	ver := &fakeVerifier{verify: func(sun.Params) (sun.Result, error) {
		r := genuineTap()
		r.SUNValid = false
		return r, nil
	}}
	h := newHandlerWith(t, &fakeInvites{}, &fakeSessions{}, rec, handlerOpts{verifier: ver})
	const n = 200
	limited := 0
	for i := 0; i < n; i++ {
		if doTap(t, h, activationTapURL, pendingCookie()).Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("nothing was rate limited; the bound below would prove nothing")
	}
	auditRowsAreBounded(t, "replayed activating tap", len(rec.events), n)
}

// TestCrossSite_IsCaseInsensitive: measured before the fix, "Cross-Site" and
// "CROSS-SITE" both planted the cookie that "cross-site" refused. Same class of
// bug as the `HTTPS://` prefix test M5-01 shipped.
func TestCrossSite_IsCaseInsensitive(t *testing.T) {
	inv, sess := victimHolds(t)
	for _, v := range []string{"cross-site", "Cross-Site", "CROSS-SITE", " cross-site ", "cross-origin"} {
		t.Run(v, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/activate?code="+fakeCode, nil)
			req.RemoteAddr = "198.51.100.7:5000"
			req.Header.Set("Sec-Fetch-Site", v)
			req.AddCookie(&http.Cookie{Name: session.CookieName, Value: victimSession})
			w := httptest.NewRecorder()
			newHandler(t, inv, sess, &fakeAudit{}).ServeHTTP(w, req)
			for _, c := range w.Result().Cookies() {
				if c.Name == activationCookieName && c.MaxAge > 0 {
					t.Fatalf("Sec-Fetch-Site %q planted the cookie", v)
				}
			}
		})
	}
	// Positive control: a same-site value still plants it, so the test is
	// measuring the comparison and not a blanket refusal.
	req := httptest.NewRequest(http.MethodGet, "/activate?code="+fakeCode, nil)
	req.RemoteAddr = "198.51.100.7:5000"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}).ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("a same-origin arrival was refused: status %d", w.Code)
	}
}

// TestHeldBy_FailsClosedOnADatabaseError. Measured before the fix: with the
// session lookup erroring, Submit skipped the 409 and issued a session — the
// takeover protection switching itself off exactly when the system was unwell.
func TestHeldBy_FailsClosedOnADatabaseError(t *testing.T) {
	inv := &fakeInvites{}
	sess := &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{}, errors.New("connection refused")
	}}
	h := newHandler(t, inv, sess, &fakeAudit{})
	victimCookie := &http.Cookie{Name: session.CookieName, Value: victimSession}

	w := post(t, h, consent(), codeCookie(), victimCookie)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: an unknown holder must not be treated as no holder", w.Code)
	}
	if inv.activateCalls != 0 || inv.consentCalls != 0 || sess.issued != 0 {
		t.Fatal("consent was recorded while the session lookup was failing")
	}

	// The page must fail closed too.
	if g := get(t, h, "/activate", codeCookie(), victimCookie); g.Code != http.StatusInternalServerError {
		t.Errorf("GET status = %d, want 500", g.Code)
	}

	// POSITIVE CONTROL: with a WORKING lookup and no cookie at all, the same
	// requests succeed — so the 500s above are the error path, not a broken flow.
	ok := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	if w2 := post(t, ok, consent(), codeCookie()); w2.Code != http.StatusSeeOther {
		t.Fatalf("the ordinary flow now answers %d", w2.Code)
	}
}

// TestActivationState_DoesNotRenderTheCode is the regression test for the second
// blocker: handler.activationState held the code as a BARE STRING, so %v, %+v,
// %#v and %s all printed it — including from a wrapping struct with unexported
// fields, which is precisely the shape a handler writes when it logs its own
// state. That is the same defect internal/session's Token was RED for in M5-01
// round 2, reproduced in a new package the moment the value left its type.
//
// The type is unexported, so this test must live IN the package; the external
// half of the proof is invite.Code's own leak_external_test.go, which covers a
// caller holding one in an unexported field.
func TestActivationState_DoesNotRenderTheCode(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(codeCookie())
	st, ok := (codeCookies{}).read(r)
	if !ok {
		t.Fatal("the fixture cookie did not parse")
	}

	// The wrapping struct with unexported fields — the shape that broke.
	wrapper := struct {
		ip    string
		state activationState
	}{"203.0.113.9", st}

	renderings := map[string]string{
		"%v state":  fmt.Sprintf("%v", st),
		"%+v state": fmt.Sprintf("%+v", st),
		"%#v state": fmt.Sprintf("%#v", st),
		// %s on the struct itself is not in this table: `go vet` rejects it
		// (activationState is not a Stringer), so it is a shape nobody can commit.
		// %s on the CODE is the reachable one and invite.Code redacts it.
		"%s code":      fmt.Sprintf("%s", st.code),
		"%v wrapper":   fmt.Sprintf("%v", wrapper),
		"%+v wrapper":  fmt.Sprintf("%+v", wrapper),
		"%#v wrapper":  fmt.Sprintf("%#v", wrapper),
		"slice":        fmt.Sprintf("%v", []activationState{st}),
		"error chain":  fmt.Errorf("activating: %w", fmt.Errorf("state %+v", st)).Error(),
		"code alone":   fmt.Sprintf("%+v", st.code),
		"slog attr":    slogLine(t, st),
		"json wrapper": jsonLine(t, st),
	}
	for name, got := range renderings {
		if strings.Contains(got, fakeCode) {
			t.Errorf("%s LEAKED the invite code", name)
		}
		if strings.Contains(got, fakeCode[:8]) {
			t.Errorf("%s leaked a prefix of the invite code", name)
		}
		if got == "" {
			t.Errorf("%s rendered nothing; the assertion proves nothing", name)
		}
	}
	// POSITIVE CONTROLS. The csrf half is NOT a secret and must still be visible
	// (it is the mechanism), and a plain string field in the same position really
	// does render — so the negatives above are the type doing its job.
	if !strings.Contains(renderings["%+v wrapper"], fakeCSRF) {
		t.Error("the csrf token vanished too; the test is not rendering what it thinks")
	}
	if plain := fmt.Sprintf("%+v", struct{ code string }{fakeCode}); !strings.Contains(plain, fakeCode) {
		t.Fatal("a bare string field does not render the value, so this test cannot detect the regression")
	}
}

func slogLine(t *testing.T, st activationState) string {
	t.Helper()
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("activating", "state", st, "code", st.code)
	return buf.String()
}

func jsonLine(t *testing.T, st activationState) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"code": st.code})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// --- Budget separation -------------------------------------------------------
//
// THE FINDING: one per-IP window was the first check in every handler, so 60
// requests carrying UNKNOWN codes — no identity, no session, no valid code — made
// the next request carrying a GENUINE invitation answer 429. Aggravating: the
// planned deployment is a single VPS behind a reverse proxy and clientIP does not
// read X-Forwarded-For yet (M5-03), so one address is EVERY address. Measured
// before the split: "after 60 unknown codes, a VALID code answers 429".

// TestBudgets_UnknownFloodDoesNotStarveAValidActivation is that measurement,
// kept.
func TestBudgets_UnknownFloodDoesNotStarveAValidActivation(t *testing.T) {
	calls := 0
	valid := okContext("invited")
	inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
		calls++
		if calls <= unknownLimit {
			return invite.Context{}, invite.ErrUnknownCode
		}
		return valid, nil
	}}
	h := newHandler(t, inv, &fakeSessions{}, &fakeAudit{})

	for i := 0; i < unknownLimit; i++ {
		if w := get(t, h, "/activate?code="+fakeCode); w.Code != http.StatusBadRequest {
			t.Fatalf("flood request %d answered %d, want 400", i, w.Code)
		}
	}
	if w := get(t, h, "/activate?code="+fakeCode); w.Code != http.StatusSeeOther {
		t.Fatalf("after %d unknown codes a VALID activation answered %d, want 303 — the anonymous flood is starving the employee", unknownLimit, w.Code)
	}
}

// TestBudgets_FloodCeilingStillRefuses is the other half: the split must not have
// removed the DoS shield, only moved it somewhere a legitimate request can
// survive. This is also the honest statement of what CAN still refuse a valid
// activation.
func TestBudgets_FloodCeilingStillRefuses(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	served := 0
	for i := 0; i < floodLimit; i++ {
		if post(t, h, consent(), codeCookie()).Code == http.StatusSeeOther {
			served++
		}
	}
	if served != floodLimit {
		t.Fatalf("only %d of the first %d requests were served; the ceiling is biting too early", served, floodLimit)
	}
	if w := post(t, h, consent(), codeCookie()); w.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d answered %d, want 429 — the flood ceiling no longer bites", floodLimit+1, w.Code)
	}
}

// TestBudgets_AnonymousRefusalsStopFillingTheLog: the unknown budget's job. Past
// its limit the branch answers identically and stops writing lines, after one
// final line saying so — otherwise an anonymous caller writes to the disk for
// free (the same shape as the audit_log finding, one layer down).
func TestBudgets_AnonymousRefusalsStopFillingTheLog(t *testing.T) {
	var logged strings.Builder
	inv := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
		return invite.Context{}, invite.ErrUnknownCode
	}}
	sess := &fakeSessions{}
	sess.tok = sess.token(t)
	cfg := &config.Config{Env: config.EnvDev, BaseURL: "http://localhost:8080", RetentionYears: 2}
	a, err := NewActivation(inv, sess, &fakeVerifier{}, &fakeAudit{}, cfg,
		slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}
	r := chi.NewRouter()
	a.Mount(r)

	const n = 400
	for i := 0; i < n; i++ {
		get(t, r, "/activate?code="+fakeCode)
	}
	lines := strings.Count(logged.String(), "\n")
	if lines > unknownLimit+5 {
		t.Errorf("%d requests wrote %d log lines (limit %d): an anonymous caller can fill the disk", n, lines, unknownLimit)
	}
	if lines == 0 {
		t.Error("nothing was logged at all; a refusal must leave some trace")
	}
	if !strings.Contains(logged.String(), "will not be logged this window") {
		t.Error("the suppression itself must be announced once, or the log lies by omission")
	}
}

// TestTap_IndistinguishableRefusals: an unknown uid, another employer's plaque
// and a forged signature on our own plaque produce BYTE-IDENTICAL responses, so
// the activation path is no oracle for which plaques exist or whose they are.
func TestTap_IndistinguishableRefusals(t *testing.T) {
	other := uuid.MustParse("99999999-9999-4999-8999-999999999999")
	shapes := map[string]func(sun.Params) (sun.Result, error){
		"unknown": func(sun.Params) (sun.Result, error) { return sun.Result{}, sun.ErrUnknownTag },
		"foreign": func(sun.Params) (sun.Result, error) {
			r := genuineTap()
			r.Tag.TenantID = other
			return r, nil
		},
		"forged": func(sun.Params) (sun.Result, error) {
			r := genuineTap()
			r.SUNValid = false
			return r, nil
		},
	}
	var first string
	var firstCode int
	for name, v := range shapes {
		h := newHandlerWith(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}, handlerOpts{verifier: &fakeVerifier{verify: v}})
		w := doTap(t, h, activationTapURL, pendingCookie())
		if first == "" {
			first, firstCode = w.Body.String(), w.Code
			continue
		}
		if w.Code != firstCode || w.Body.String() != first {
			t.Errorf("%s: answer differs from the others (status %d vs %d)", name, w.Code, firstCode)
		}
	}
}
