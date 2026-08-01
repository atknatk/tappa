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
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
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
	activateCalls int
	steps         *[]string
}

func (f *fakeInvites) Lookup(_ context.Context, c invite.Code) (invite.Context, error) {
	if f.lookup == nil {
		return okContext("invited"), nil
	}
	return f.lookup(c)
}

func (f *fakeInvites) Activate(_ context.Context, c invite.Code) (invite.Activation, error) {
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
	return session.Issued{
		Session: session.Session{ID: uuid.New(), TenantID: p.TenantID, EmployeeID: p.EmployeeID, DeviceInfo: p.DeviceInfo},
		Token:   f.tok,
	}, nil
}

func (f *fakeSessions) Verify(context.Context, session.Token) (session.Resolved, error) {
	if f.verify == nil {
		return session.Resolved{ID: uuid.New(), TenantID: testTenant, EmployeeID: testEmployee}, nil
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

func newHandler(t *testing.T, inv *fakeInvites, sess *fakeSessions, rec *fakeAudit) http.Handler {
	t.Helper()
	sess.tok = sess.token(t)
	cfg := &config.Config{
		Env:            config.EnvDev,
		BaseURL:        "http://localhost:8080",
		RetentionYears: 2,
	}
	// Discard log output: these tests assert on responses and audit events, and a
	// test that also asserted on log text would fail for cosmetic reasons.
	a, err := NewActivation(inv, sess, rec, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}
	r := chi.NewRouter()
	a.Mount(r)
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
		t.Fatalf("the cookie must carry <csrf>.<code>, got a value that does not split to the code")
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
	if found.MaxAge != activationCookieMaxAge {
		t.Errorf("MaxAge = %d, want %d", found.MaxAge, activationCookieMaxAge)
	}
}

// TestPage_RendersTheNoticeAndTheWiFiStep covers the two things the card
// requires this screen to show.
func TestPage_RendersTheNoticeAndTheWiFiStep(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate", codeCookie()).Body.String()

	for _, want := range []string{
		"Maria Borg",             // identity
		"Kebab Factory Ltd",      // GDPR Art. 13(1)(a) controller
		"internet (IP) address",  // what a tap records
		"location",               // ditto
		"No fingerprints",        // §4.1, stated to the employee
		"2",                      // retention, from configuration
		"years",                  //
		"KF-StJulians-Staff",     // the Wi-Fi step
		"You can skip this",      // the step is optional
		"Activate this phone",    // the single button
		`action="/api/activate"`, // the endpoint the card names
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the activation page does not mention %q", want)
		}
	}
	// The form must NOT carry the code.
	if strings.Contains(body, fakeCode) {
		t.Fatal("the rendered form contains the invite code")
	}
}

// TestPage_NonProdShowsTheConfigNotice: the retention figure is a development
// placeholder outside production and the page has to say so, rather than letting
// a dev deployment make a legal-looking claim.
func TestPage_NonProdShowsTheConfigNotice(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	body := get(t, h, "/activate", codeCookie()).Body.String()
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
		{"not activatable", okContext("deactivated"), invite.ErrNotActivatable, true, "employee_not_activatable"},
		{"unknown code has no tenant", invite.Context{}, invite.ErrUnknownCode, false, ""},
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
	if inv.activateCalls != 0 {
		t.Fatal("the code was consumed without consent")
	}
	if !strings.Contains(w.Body.String(), "Please tick the box") {
		t.Error("the form must come back with the error attached")
	}
	if !rec.has(ActionActivationFailed) {
		t.Error("a refused activation must leave a trace (§4.6)")
	}
}

// TestSubmit_HappyPath: session cookie set, activation cookie cleared, 303 to the
// mini tour, one audit row.
//
// THE DESTINATION CHANGED IN M5-07 and the reason is on Submit: a FIRST
// activation lands on /activate/tour, a second device still lands on the
// confirmation. The split is what keeps the tour's third slide ("your first tap
// is a practice run") true of everyone who is shown it.
func TestSubmit_HappyPath(t *testing.T) {
	sess := &fakeSessions{}
	rec := &fakeAudit{}
	h := newHandler(t, &fakeInvites{}, sess, rec)

	w := post(t, h, consent(), codeCookie())

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (POST/redirect/GET keeps a refresh from re-posting)", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/activate/tour" {
		t.Fatalf("Location = %q, want /activate/tour (a first activation is shown the tour)", loc)
	}
	if sess.issued != 1 {
		t.Fatalf("sessions issued = %d, want 1", sess.issued)
	}

	var sessionSet, activationCleared bool
	for _, c := range w.Result().Cookies() {
		switch c.Name {
		case session.CookieName:
			sessionSet = true
			if !c.HttpOnly {
				t.Error("the session cookie must be HttpOnly")
			}
		case activationCookieName:
			if c.MaxAge < 0 {
				activationCleared = true
			}
		}
	}
	if !sessionSet {
		t.Error("no session cookie was set: activation did not finish")
	}
	if !activationCleared {
		t.Error("the spent activation cookie was left in the browser")
	}
	if !rec.has(ActionActivationCompleted) {
		t.Errorf("audit = %v, want activation.completed", rec.actions())
	}
}

// TestSubmit_SecondDeviceRevokesBeforeIssuing is the ORDER test, and the order is
// the whole decision: RevokeAllForEmployee kills every LIVE session of the
// employee, so issuing first would kill the session just issued and the new phone
// would be signed out before it ever tapped.
func TestSubmit_SecondDeviceRevokesBeforeIssuing(t *testing.T) {
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

	w := post(t, h, consent(), codeCookie())

	if got := strings.Join(steps, ","); got != "activate,revoke,issue" {
		t.Fatalf("order = %q, want activate,revoke,issue", got)
	}
	if loc := w.Header().Get("Location"); loc != "/activate/done?replaced=1" {
		t.Errorf("Location = %q: the confirmation must be able to say the other phone was signed out, "+
			"and a second device must NOT be sent through the tour — see Submit for why the tour's "+
			"practice promise is false for somebody who has already tapped", loc)
	}
	if !rec.has(ActionDeviceReplaced) {
		t.Errorf("audit = %v, want activation.device_replaced", rec.actions())
	}
	if !rec.has(ActionActivationCompleted) {
		t.Errorf("audit = %v, want activation.completed too", rec.actions())
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
	if inv.activateCalls != 0 {
		t.Fatal("a code was consumed without one being presented")
	}
}

// TestSubmit_SessionIssueFailureIsLoud: the code is already spent, so this cannot
// be dressed up as "your link is invalid" — that would send the employee to their
// manager with the wrong story and hide a server fault.
func TestSubmit_SessionIssueFailureIsLoud(t *testing.T) {
	rec := &fakeAudit{}
	sess := &fakeSessions{issueErr: errors.New("boom")}
	h := newHandler(t, &fakeInvites{}, sess, rec)

	w := post(t, h, consent(), codeCookie())

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
// property, measured rather than asserted by choosing a big number: a successful
// activation costs nothing, so a whole venue onboarding cannot trip the limit.
func TestRateLimit_SuccessNeverConsumesBudget(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	for i := 0; i < inviteFailureLimit*3; i++ {
		if w := post(t, h, consent(), codeCookie()); w.Code != http.StatusSeeOther {
			t.Fatalf("successful activation %d was throttled: status %d", i, w.Code)
		}
	}
}

// TestDone_RequiresTheSessionCookie: the confirmation is the moment a browser
// that silently dropped the cookie can be caught, with a sentence that says what
// to change.
func TestDone_RequiresTheSessionCookie(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})

	w := get(t, h, "/activate/done")
	if !strings.Contains(w.Body.String(), "keep the sign-in") {
		t.Fatal("a missing session cookie must be explained here, not at the plaque tomorrow")
	}

	sess := &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{}, session.ErrNoSession
	}}
	h2 := newHandler(t, &fakeInvites{}, sess, &fakeAudit{})
	w2 := get(t, h2, "/activate/done", &http.Cookie{Name: session.CookieName, Value: fakeCode})
	if !strings.Contains(w2.Body.String(), "keep the sign-in") {
		t.Fatal("an unknown session must land on the same explanation")
	}
}

// TestDone_ShowsTheConfirmationWithoutAButton (skill tappa-brand: the
// confirmation has no button — the next action is a physical touch).
func TestDone_ShowsTheConfirmationWithoutAButton(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	w := get(t, h, "/activate/done", &http.Cookie{Name: session.CookieName, Value: fakeCode})

	body := w.Body.String()
	if !strings.Contains(body, "All done — you can close this page.") {
		t.Error("the confirmation must use the brand's closing line")
	}
	if strings.Contains(body, "<button") || strings.Contains(body, "<form") {
		t.Error("the confirmation screen must carry no button and no form")
	}
	if !strings.Contains(body, "KF-StJulians-Staff") {
		t.Error("the Wi-Fi reminder belongs on the confirmation too")
	}
}

// TestNoResponseBodyEverCarriesTheCode drives the whole flow and greps every
// byte of every body. The Set-Cookie header is the ONE place the code appears,
// by construction, and the test states that rather than pretending otherwise.
func TestNoResponseBodyEverCarriesTheCode(t *testing.T) {
	h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})

	responses := []*httptest.ResponseRecorder{
		get(t, h, "/activate?code="+fakeCode),
		get(t, h, "/activate", codeCookie()),
		post(t, h, url.Values{"csrf": {fakeCSRF}}, codeCookie()), // consent missing: re-renders the form
		post(t, h, consent(), codeCookie()),
		get(t, h, "/activate/done", &http.Cookie{Name: session.CookieName, Value: fakeCode}),
	}
	for i, w := range responses {
		if strings.Contains(w.Body.String(), fakeCode) {
			t.Errorf("response %d carries the invite code in its BODY", i)
		}
		if w.Body.Len() == 0 && w.Code != http.StatusSeeOther {
			t.Errorf("response %d is empty; the assertion above proves nothing", i)
		}
		for name, values := range w.Header() {
			if name == "Set-Cookie" {
				continue // the declared, intended carrier
			}
			for _, v := range values {
				if strings.Contains(v, fakeCode) {
					t.Errorf("response %d leaks the code in header %s", i, name)
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
		a, err := NewActivation(inv, sess, &fakeAudit{}, cfg,
			slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
		if err != nil {
			t.Fatalf("NewActivation: %v", err)
		}
		r := chi.NewRouter()
		a.Mount(r)
		return r
	}

	// Happy path (which logs nothing) plus every branch that does.
	happy := build(&fakeInvites{}, &fakeSessions{})
	get(t, happy, "/activate?code="+fakeCode)
	post(t, happy, consent(), codeCookie())

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
	}

	// A session-issue failure, and a rate-limit trip.
	broken := build(&fakeInvites{}, &fakeSessions{issueErr: errors.New("boom")})
	post(t, broken, consent(), codeCookie())

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
	h := newHandler(t, inv, sess, rec)

	victimCookie := &http.Cookie{Name: session.CookieName, Value: victimSession}
	w := post(t, h, consent(), codeCookie(), victimCookie)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if inv.activateCalls != 0 || sess.issued != 0 {
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

	// With the explicit tick it goes through — a genuine hand-over of a shared
	// phone must remain possible.
	w2 := post(t, h, url.Values{"consent": {"yes"}, "csrf": {fakeCSRF}, "switch": {"yes"}}, codeCookie(), victimCookie)
	if w2.Code != http.StatusSeeOther || sess.issued != 1 {
		t.Fatalf("a confirmed switch was refused: status %d, issued %d", w2.Code, sess.issued)
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
	if w3.Code == http.StatusSeeOther || sess.issued != 0 {
		t.Fatalf("STEP 3 completed the takeover: status %d, sessions issued %d", w3.Code, sess.issued)
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
	// The employee can still complete their activation.
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
			t.Fatalf("successful activation %d was throttled: status %d", i, w.Code)
		}
	}
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
	if inv.activateCalls != 0 || sess.issued != 0 {
		t.Fatal("an activation completed while the session lookup was failing")
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
	a, err := NewActivation(inv, sess, &fakeAudit{}, cfg,
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
