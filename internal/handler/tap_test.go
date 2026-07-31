package handler

// Tap page tests against FAKES. What they measure is the HTTP behaviour the
// screen promises: §5 row 3's redirect, the SessionUnresolved / SessionAbsent
// split, that the page never reaches for a counter, that it carries the tag's
// tenant forward, and that the rendered HTML is one button and no crypto.
//
// The database-backed proof — no transactions row, no counter movement, real
// middleware order — is in tap_db_test.go. Neither file replaces the other.

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web"
)

var (
	tapTagTenant = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	tapLocation  = uuid.MustParse("55555555-5555-4555-8555-555555555555")
)

// A syntactically valid SUN URL. The values are FAKE (agent-brief madde 2) and
// never reach cryptography here: the previewer is a fake.
const (
	tapUID  = "04AC7E55000601"
	tapCtr  = "000641"
	tapCMAC = "83c0104d8ac39077"
)

func tapURL() string { return "/t?tag=" + tapUID + "&ctr=" + tapCtr + "&cmac=" + tapCMAC }

// fakePreviewer stands in for internal/sun. Note the shape of the interface it
// satisfies: there is no Verify here and there cannot be, because handler.Tap's
// consumer-side interface does not name one. A fake that WANTED to advance a
// counter would have nothing to implement.
type fakePreviewer struct {
	preview sun.Preview
	err     error
	calls   int
}

func (f *fakePreviewer) PreviewWithoutReplayProtection(_ context.Context, _ sun.Params) (sun.Preview, error) {
	f.calls++
	if f.err != nil {
		return sun.Preview{}, f.err
	}
	return f.preview, nil
}

func okPreview(cmacValid bool) sun.Preview { return previewWithStatus(cmacValid, "active") }

func previewWithStatus(cmacValid bool, status string) sun.Preview {
	return sun.Preview{
		CMACValid: cmacValid,
		TenantID:  tapTagTenant,
		TagStatus: status,
		Location:  tapLocation,
	}
}

type fakeDirectory struct {
	facts tenant.TapPageFacts
	err   error
	// got records the arguments, so the tenant the venue is read under can be
	// asserted rather than assumed.
	gotTenant, gotEmployee, gotLocation uuid.UUID
}

func (f *fakeDirectory) TapPage(_ context.Context, tenantID, employeeID, locationID uuid.UUID) (tenant.TapPageFacts, error) {
	f.gotTenant, f.gotEmployee, f.gotLocation = tenantID, employeeID, locationID
	return f.facts, f.err
}

func okFacts() tenant.TapPageFacts {
	return tenant.TapPageFacts{EmployeeName: "Maria Borg", LocationName: "St Julians"}
}

func tapCfg() *config.Config {
	return &config.Config{
		Env:            config.EnvDev,
		BaseURL:        "http://localhost:8080",
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"),
		InviteHMACKey:  []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		RetentionYears: 2,
	}
}

// newTapHandler mounts the screen the way production does — through Mount, so
// the middleware order under test is the real one and not a test-only stack.
func newTapHandler(t *testing.T, pv *fakePreviewer, dir *fakeDirectory, sess *fakeSessions) (http.Handler, *Tap) {
	t.Helper()
	h, tp, _ := newTapHandlerWithAudit(t, pv, dir, sess)
	return h, tp
}

// newTapHandlerWithAudit is the same wiring plus a handle on the trail, for the
// tests that assert what a refusal leaves behind.
func newTapHandlerWithAudit(t *testing.T, pv *fakePreviewer, dir *fakeDirectory, sess *fakeSessions) (http.Handler, *Tap, *fakeAudit) {
	t.Helper()
	rec := &fakeAudit{}
	tp, err := NewTap(pv, dir, sess, rec, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	r := chi.NewRouter()
	tp.Mount(r)
	return r, tp, rec
}

// sessionCookie is what a phone that has activated carries.
func sessionCookie() *http.Cookie {
	return &http.Cookie{Name: session.CookieName, Value: "FAKEsessionFAKEsessionFAKEsessionFAKEsess12"}
}

// ---------------------------------------------------------------------------
// §5 row 3 and the SessionUnresolved / SessionAbsent split.
// ---------------------------------------------------------------------------

// TestTapPage_NoSessionRedirectsToActivation is §5 row 3: no session -> the
// activation page, and NO record. The "no record" half cannot be seen from
// here (nothing in this handler can write one); tap_db_test.go counts the
// transactions table instead.
func TestTapPage_NoSessionRedirectsToActivation(t *testing.T) {
	pv := &fakePreviewer{preview: okPreview(true)}
	h, _ := newTapHandler(t, pv, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	// No cookie at all -> SessionAbsent.
	w := get(t, h, tapURL())

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (activation page, §5 row 3)", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/activate" {
		t.Fatalf("Location = %q, want /activate", loc)
	}
	if pv.calls != 0 {
		t.Fatalf("the tag was resolved %d times for a session-less visitor, want 0: "+
			"answering before identity turns GET /t into an oracle for which plaque uids exist", pv.calls)
	}
}

// TestTapPage_RevokedSessionRedirectsToActivation: a revoked session is "no
// session" for §5 row 3 — the phone was signed out (stolen handset, second
// device). It is NOT the deactivated-employee case, which keeps a LIVE session
// on purpose so the attempt reaches row 4 and gets recorded; see the next test.
func TestTapPage_RevokedSessionRedirectsToActivation(t *testing.T) {
	sess := &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{ID: uuid.New(), TenantID: testTenant, EmployeeID: testEmployee}, session.ErrRevoked
	}}
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, sess)

	w := get(t, h, tapURL(), sessionCookie())

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/activate" {
		t.Fatalf("status = %d Location = %q, want 303 /activate", w.Code, w.Header().Get("Location"))
	}
}

// TestTapPage_LiveSessionOfADeactivatedEmployeeStillRenders is the other side of
// that split, and it is the one §4.6 depends on. Deactivation deliberately does
// NOT revoke sessions (M5-01), so a deactivated employee reaches this page with
// a live session, presses the button, and sys:employee-deactivated writes the
// rejected attempt at POST time. A page that refused here would send them to the
// activation screen instead and the attempt would leave no trace.
func TestTapPage_LiveSessionOfADeactivatedEmployeeStillRenders(t *testing.T) {
	// From this handler's position a deactivated employee is simply a live
	// session — it never reads employees.status, and that is the point.
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	w := get(t, h, tapURL(), sessionCookie())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: the refusal belongs to the guardrail at POST time, which RECORDS it (§5 row 4, §4.6)", w.Code)
	}
}

// TestTapPage_UnresolvedIdentityIsNotNoSession is state.md hand-off 3, made
// mechanical. The zero Identity has Err == nil AND Live() == false, so a handler
// written as `if id.Err != nil {500} else if !id.Live() {activate}` reads a
// missing middleware — or a database outage — as a genuine session-less tap.
// Here the handler is called DIRECTLY, without Identify in front of it, which is
// exactly that wiring mistake.
func TestTapPage_UnresolvedIdentityIsNotNoSession(t *testing.T) {
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		&fakeSessions{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, tapURL(), nil)
	req.RemoteAddr = "203.0.113.5:41234"
	w := httptest.NewRecorder()

	tp.Page(w, req) // no Identify middleware: Identity is the zero value

	if w.Code == http.StatusSeeOther {
		t.Fatal("an unresolved identity was answered with the activation redirect: " +
			"a wiring bug or an outage would look exactly like a session-less tap (state.md hand-off 3)")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — 'we do not know who this is' must be loud", w.Code)
	}
}

// ---------------------------------------------------------------------------
// The page renders for outcomes the decision engine will REJECT, on purpose.
// ---------------------------------------------------------------------------

// TestTapPage_RendersEvenWhenThePreCheckSaysNo. A retired plaque (§5 row 1) and
// a bad CMAC (§5 row 2) are both rejects — and both must be RECORDED (§4.6).
// The record is written by POST /api/checkin, so a page that refused to render
// would mean the button is never pressed and the attempt vanishes.
func TestTapPage_RendersEvenWhenThePreCheckSaysNo(t *testing.T) {
	tests := []struct {
		name    string
		preview sun.Preview
	}{
		{"bad cmac", okPreview(false)},
		{"retired tag", previewWithStatus(false, "retired")},
		{"lost tag", previewWithStatus(false, "lost")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTapHandler(t, &fakePreviewer{preview: tc.preview}, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

			w := get(t, h, tapURL(), sessionCookie())

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 — the reject has to reach the decision engine to be recorded (§4.6)", w.Code)
			}
			if !strings.Contains(w.Body.String(), `name="ctx"`) {
				t.Fatal("the page rendered without a tap context, so the button could not produce a recorded decision")
			}
		})
	}
}

// TestTapPage_UnknownPlaqueIsRefused: the ONE case that cannot be recorded. An
// unknown uid resolves to no tenant and no location, so there is nothing to
// render a page around and nothing to write a row against — internal/sun states
// the same reasoning where ErrUnknownTag is defined.
func TestTapPage_UnknownPlaqueIsRefused(t *testing.T) {
	h, _ := newTapHandler(t, &fakePreviewer{err: sun.ErrUnknownTag}, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	w := get(t, h, tapURL(), sessionCookie())

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if strings.Contains(w.Body.String(), `name="ctx"`) {
		t.Fatal("an unknown plaque was given a signed tap context")
	}
}

// TestTapPage_PreviewOutageIsNotAnUnknownPlaque: an outage must not be rendered
// as "we don't know that plaque", which would send an employee to their manager
// about a door that is fine.
func TestTapPage_PreviewOutageIsNotAnUnknownPlaque(t *testing.T) {
	h, _ := newTapHandler(t, &fakePreviewer{err: errors.New("connection reset by peer")},
		&fakeDirectory{facts: okFacts()}, &fakeSessions{})

	w := get(t, h, tapURL(), sessionCookie())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "don't know that plaque") {
		t.Fatal("an outage was presented as an unknown plaque")
	}
}

func TestTapPage_MalformedURLIsRefused(t *testing.T) {
	pv := &fakePreviewer{preview: okPreview(true)}
	h, _ := newTapHandler(t, pv, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	for _, target := range []string{"/t", "/t?tag=zzzz", "/t?tag=" + tapUID + "&ctr=" + tapCtr} {
		w := get(t, h, target, sessionCookie())
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, w.Code)
		}
	}
	if pv.calls != 0 {
		t.Fatalf("a malformed url reached the tag lookup %d times, want 0", pv.calls)
	}
}

// ---------------------------------------------------------------------------
// Tenancy: what the venue is read under, and what is carried forward.
// ---------------------------------------------------------------------------

// TestTapPage_VenueIsReadUnderTheSessionTenant. The read is scoped to the
// SESSION's tenant so a plaque belonging to someone else returns nothing rather
// than leaking that tenant's venue name onto this employee's screen.
func TestTapPage_VenueIsReadUnderTheSessionTenant(t *testing.T) {
	dir := &fakeDirectory{facts: okFacts()}
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, dir, &fakeSessions{})

	get(t, h, tapURL(), sessionCookie())

	if dir.gotTenant != testTenant {
		t.Fatalf("venue read under tenant %s, want the SESSION's %s", dir.gotTenant, testTenant)
	}
	if dir.gotTenant == tapTagTenant {
		t.Fatal("venue read under the TAG's tenant: one tenant's venue name would render on another tenant's screen")
	}
	if dir.gotLocation != tapLocation {
		t.Fatalf("venue read for location %s, want the TAPPED %s (§5: not the employee's profile location)", dir.gotLocation, tapLocation)
	}
}

// TestTapPage_ForeignPlaqueStillRenders: a cross-tenant tap renders WITHOUT a
// venue name and still gets a context, because refusing here would decide
// sys:tenant-mismatch — and decide it without a record. M5-05 owns that call.
func TestTapPage_ForeignPlaqueStillRenders(t *testing.T) {
	dir := &fakeDirectory{
		facts: tenant.TapPageFacts{EmployeeName: "Maria Borg"},
		err:   tenant.ErrForeignLocation,
	}
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, dir, &fakeSessions{})

	w := get(t, h, tapURL(), sessionCookie())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the tenant decision belongs to the guardrail, which records it", w.Code)
	}
	if strings.Contains(w.Body.String(), "St Julians") {
		t.Fatal("another tenant's venue name was rendered")
	}
	if !strings.Contains(w.Body.String(), "Maria Borg") {
		t.Fatal("the employee's own name was dropped along with the foreign venue")
	}
}

// TestTapPage_CarriesTheTagTenantForward is the N5 hand-off, measured: the tag's
// tenant reaches the signed context so M5-05 can put BOTH tenants into
// tap.Input and finally make sys:tenant-mismatch bite. Carrying is all that is
// claimed — the page compares nothing.
func TestTapPage_CarriesTheTagTenantForward(t *testing.T) {
	h, tp := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, fixedSessions())

	w := get(t, h, tapURL(), sessionCookie())
	signed := contextFieldOf(t, w.Body.String())

	tc, err := tp.contexts.parse(signed, tapFixedSessionID)
	if err != nil {
		t.Fatalf("parsing the page's own context: %v", err)
	}
	if tc.TagTenantID != tapTagTenant {
		t.Fatalf("context carries tag tenant %s, want %s (hand-off N5)", tc.TagTenantID, tapTagTenant)
	}
	if tc.LocationID != tapLocation {
		t.Fatalf("context carries location %s, want %s", tc.LocationID, tapLocation)
	}
	if tc.UID != tapUID || tc.Ctr != 0x000641 {
		t.Fatalf("context carries uid %q ctr %d, want %q %d", tc.UID, tc.Ctr, tapUID, 0x000641)
	}
	if tc.Channel != sun.ChannelNFC {
		t.Fatalf("context carries channel %q, want nfc (server-derived, hand-off N2)", tc.Channel)
	}
	if !tc.CMACVerified {
		t.Fatal("context says the CMAC did not verify, but the preview said it did")
	}
}

// ---------------------------------------------------------------------------
// What the HTML is, and what it is not.
// ---------------------------------------------------------------------------

// TestTapPage_IsOneScreenOneButton pins CLAUDE.md §9 mechanically. It is a
// coarse test on purpose: it cannot judge a design, but it CAN notice a second
// button, a nav element or a link appearing on the screen the rule calls sacred.
func TestTapPage_IsOneScreenOneButton(t *testing.T) {
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	body := get(t, h, tapURL(), sessionCookie()).Body.String()

	if n := strings.Count(body, "<button"); n != 1 {
		t.Fatalf("%d buttons on the tap screen, want exactly 1 (§9: one screen, one button)", n)
	}
	for _, forbidden := range []string{"<nav", "<a href", "<select", "<details"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the tap screen contains %q — no menu, no tabs, no settings (§9)", forbidden)
		}
	}
	if !strings.Contains(body, "Hello Maria Borg") {
		t.Fatal(`the greeting is missing: the screen is "Hello Maria" + venue + one button`)
	}
	if !strings.Contains(body, "St Julians") {
		t.Fatal("the tapped venue is missing")
	}
	if !strings.Contains(body, "tap-button") {
		t.Fatal("the button does not carry .tap-button, which is what gives it the 64px target (input.css)")
	}
	// One stylesheet, one script, and the script is ours.
	if n := strings.Count(body, "<script"); n != 1 {
		t.Fatalf("%d script tags, want exactly 1 (the geolocation one)", n)
	}
	if !strings.Contains(body, `src="/static/js/tap.js"`) {
		t.Fatal("the script is not the self-hosted one")
	}
}

// TestTapPage_BodyCarriesNoCryptographicMaterial is the M5-04 card's trap, and
// its name is the accurate version of what it checks.
//
// WHAT IT ACTUALLY ASSERTS, since an audit found the earlier comment claiming
// more: the chip's CMAC appears nowhere in the response (every byte scanned,
// both cases, and again inside the decoded payload), and tag/ctr/cmac are not
// hidden FORM FIELDS. It does NOT assert that the uid and the counter are
// absent, because they are not — they travel inside the signed payload and
// page script can decode them. That is fine and deliberate: the chip already
// printed both in the address bar. The CMAC is the one value that must not be
// there, and it is the one this scans for.
func TestTapPage_BodyCarriesNoCryptographicMaterial(t *testing.T) {
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	body := get(t, h, tapURL(), sessionCookie()).Body.String()

	for _, secret := range []string{tapCMAC, strings.ToUpper(tapCMAC)} {
		if strings.Contains(body, secret) {
			t.Fatalf("the chip's MAC appears in the page body (§4.7, M5-04 trap)")
		}
	}
	// The counter and the uid are not secrets — the chip prints them in the
	// address bar — but they have no business being form state either.
	if strings.Contains(body, `name="ctr"`) || strings.Contains(body, `name="cmac"`) || strings.Contains(body, `name="tag"`) {
		t.Fatal("tag/ctr/cmac are hidden form fields: the card asks for a signed context instead")
	}
	// And the signed context, decoded, must not contain the MAC either.
	raw := decodedPayloadOf(t, contextFieldOf(t, body))
	if strings.Contains(strings.ToLower(raw), strings.ToLower(tapCMAC)) {
		t.Fatalf("the signed context's payload carries the chip's MAC: %q", raw)
	}
}

// TestTapPage_GPSFieldsAreEmptyOnEveryRender. §4.2: the coordinates exist only
// after the button is pressed, so the page as served carries none.
//
// The other half of §4.2 — that nothing in this product SUBSCRIBES to position —
// is deliberately NOT asserted here. scripts/redline-check.sh R2 scans the
// entire repository for the three API names that would do it and fails the build
// on any match, which is both wider than a per-page grep and the reason those
// names are not written anywhere in the source, including in comments.
func TestTapPage_GPSFieldsAreEmptyOnEveryRender(t *testing.T) {
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	body := get(t, h, tapURL(), sessionCookie()).Body.String()

	for _, field := range []string{"lat", "lng", "acc"} {
		if !strings.Contains(body, `name="`+field+`" value=""`) {
			t.Fatalf("the %s field is missing or pre-filled; GPS is read at the moment of a tap and nowhere else (§4.2)", field)
		}
	}
	// The one position API this product may use, and it is in the script rather
	// than inline, so the page itself carries no location code at all.
	if strings.Contains(body, "getCurrentPosition") {
		t.Fatal("location code is inline in the page; it belongs in the one self-hosted script")
	}
}

// TestTapScript_DoublePressCannotEscapeAsANativeSubmit guards the fix for an
// audit finding: the second press, arriving while the first is still waiting for
// a position fix, used to `return` out of the submit handler WITHOUT cancelling
// the event — so the browser submitted natively with empty coordinates and the
// tap lost its GPS evidence (an `ok` becomes a `flag`).
//
// ⚠️ WHAT THIS TEST IS, STATED PLAINLY: a SOURCE-LEVEL net, not a behavioural
// proof. There is no JavaScript runtime in this repository and there will not be
// one — Node is forbidden (CLAUDE.md §1) — so nothing here can press a button
// twice and observe a request. What it CAN do is fail if the specific shape that
// caused the defect comes back, and pin the two invariants the file rests on.
// The behaviour itself was verified by reading; that limit is written here
// rather than left for a reader to discover.
func TestTapScript_DoublePressCannotEscapeAsANativeSubmit(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "js/tap.js")
	if err != nil {
		t.Fatalf("read the served script: %v", err)
	}
	src := string(raw)

	// The exact defect: an early return from the re-entry guard with nothing
	// cancelling the event.
	for _, bad := range []string{"if (submitting) return;", "if (submitting) { return; }"} {
		if strings.Contains(src, bad) {
			t.Fatalf("the re-entry guard is back to %q: a second press escapes as a native, "+
				"coordinate-less submit", bad)
		}
	}
	// The guard must cancel before it returns.
	guard := "if (submitting) {"
	i := strings.Index(src, guard)
	if i < 0 {
		t.Fatal("the re-entry guard is gone: a second press would start a second position request")
	}
	rest := src[i:]
	end := strings.Index(rest, "\n    }")
	if end < 0 {
		t.Fatal("could not delimit the re-entry guard block")
	}
	if !strings.Contains(rest[:end], "event.preventDefault()") {
		t.Fatalf("the re-entry guard does not call preventDefault before returning:\n%s", rest[:end])
	}

	// Exactly one position CALL, and it is the single-shot one. The match is on
	// the invocation rather than the bare identifier, which also appears in the
	// file's own prose. (The three forbidden continuous-location APIs are
	// scanned repo-wide by redline-check R2, which is why they are not named
	// here either.)
	if n := strings.Count(src, "navigator.geolocation.getCurrentPosition("); n != 1 {
		t.Fatalf("%d position calls, want exactly 1", n)
	}
	// A cached fix is refused: a remembered position is "where you were", which
	// is precisely the signal §4.2 keeps out of this product.
	if !strings.Contains(src, "maximumAge: 0") {
		t.Fatal("maximumAge is not pinned to 0: a cached position could vouch for someone who has not moved since this morning")
	}
}

func TestTapPage_IsNotCached(t *testing.T) {
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	w := get(t, h, tapURL(), sessionCookie())

	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store: the page greets somebody by name on a shared phone", got)
	}
}

// ---------------------------------------------------------------------------
// The rate limiter is mounted, in the contract order, with a branded body.
// ---------------------------------------------------------------------------

// TestTapPage_RateLimitIsMountedAndBranded. The default budgets are far too
// wide to reach in a test (3000 per ten minutes, by design — see TapLimiter),
// so this replaces the handler's limiter with a narrow one and drives it. What
// it proves is that Mount really puts a shield in front of GET /t and that the
// refusal is the brand's page rather than the plain-text fallback (state.md
// hand-off 6).
func TestTapPage_RateLimitIsMountedAndBranded(t *testing.T) {
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		&fakeSessions{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	tp.limiter = httpx.NewTapLimiter(httpx.TapLimitParams{
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Refused:       tp.renderTooManyRequests,
		AddressLimit:  2,
		AddressPeriod: time.Minute,
	})
	r := chi.NewRouter()
	tp.Mount(r)

	for i := 0; i < 2; i++ {
		if w := get(t, r, tapURL(), sessionCookie()); w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (still inside the budget)", i, w.Code)
		}
	}
	w := get(t, r, tapURL(), sessionCookie())
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 — the shield is not mounted in front of GET /t", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("429 Content-Type = %q, want text/html: the refusal is the brand's page now, not plain text", got)
	}
	if !strings.Contains(w.Body.String(), "Too many taps from here") {
		t.Fatalf("429 body is not the branded page: %q", w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After was dropped when the body became a page")
	}
}

// TestTapPage_RefusalWithASessionLeavesExactlyOneAuditRow is the criterion an
// audit found dead in production: TapLimiter's doc and the M5-03 card both
// promise that a refused tap whose session RESOLVED writes one audit_log row
// per window — and the first version of this file constructed the limiter
// without a recorder, so the promise was true of the middleware and false of
// the product (measured live: 285 served, 15 refused, audit_log unchanged).
//
// 🔑 IT DRIVES THE LIMITER NewTap BUILT, at its PRODUCTION budget, and that is
// the whole point of the test. The first attempt at this test replaced
// tp.limiter with one it constructed itself — passing Audit explicitly — and so
// it measured the middleware while the wiring it was supposed to guard stayed
// broken: deleting `Audit: rec` from NewTap left it GREEN. A test that
// configures the thing it is checking is not checking it. Reaching the real
// budget costs tapSessionLimit+2 requests against fakes, which is cheap, and it
// is the only version of this test that fails when the defect returns.
//
// ONE row, not N: audit_log cannot be deleted from even by tappa_owner, so a
// refusal path that wrote per request would be a cheaper attack on the trail
// than on the endpoint (M5-02's blocking finding #1). FirstOverLimit bounds it.
func TestTapPage_RefusalWithASessionLeavesExactlyOneAuditRow(t *testing.T) {
	h, _, rec := newTapHandlerWithAudit(t, &fakePreviewer{preview: okPreview(true)},
		&fakeDirectory{facts: okFacts()}, fixedSessions())

	served, refused := 0, 0
	for i := 0; i < httpx.TapSessionLimit()+3; i++ {
		switch get(t, h, tapURL(), sessionCookie()).Code {
		case http.StatusOK:
			served++
		case http.StatusTooManyRequests:
			refused++
		}
	}
	if served != httpx.TapSessionLimit() || refused != 3 {
		t.Fatalf("served=%d refused=%d, want %d and 3", served, refused, httpx.TapSessionLimit())
	}

	rows := 0
	for _, a := range rec.actions() {
		if a == httpx.ActionTapRateLimited {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("%d %s rows for %d refusals, want exactly 1 per window — "+
			"0 means the recorder was never wired into the limiter NewTap builds (the audit's finding), "+
			"more than 1 means the append-only trail can be flooded from the refusal path",
			rows, httpx.ActionTapRateLimited, refused)
	}
}

// TestTapPage_RefusalWithoutASessionLeavesNoAuditRow is the other half of the
// same criterion, and it is a LIMIT rather than a gap: audit_log.tenant_id is
// NOT NULL with an FK to tenants, a tap has no tenant until its session
// resolves, and no "system tenant" is invented to paper over that
// (db/queries/audit.sql). Such refusals leave a WARN line instead.
//
// It too runs against the limiter NewTap built, for the reason above.
func TestTapPage_RefusalWithoutASessionLeavesNoAuditRow(t *testing.T) {
	h, _, rec := newTapHandlerWithAudit(t, &fakePreviewer{preview: okPreview(true)},
		&fakeDirectory{facts: okFacts()}, fixedSessions())

	refused := 0
	for i := 0; i < httpx.TapAddressLimit()+3; i++ {
		if get(t, h, tapURL()).Code == http.StatusTooManyRequests { // no session cookie
			refused++
		}
	}
	if refused == 0 {
		t.Fatal("nothing was refused, so this measures nothing")
	}
	if n := len(rec.actions()); n != 0 {
		t.Fatalf("%d audit rows for refusals with no resolved session, want 0: %v", n, rec.actions())
	}
}

// TestTapPage_RefusalBodyIsBrandedAtTheProductWiring closes the twin of the
// audit-recorder finding, one field along.
//
// `Refused: t.renderTooManyRequests` in NewTap had NO test holding it: an audit
// set it to nil and the entire suite stayed green, because the only test that
// looked at a 429 body built its own limiter and passed Refused explicitly —
// the same "a test that configures the thing it checks" mistake, in the same
// constructor, on the adjacent line. So this drives the limiter THE PRODUCT
// BUILDS, at its production address budget, and reads what comes back.
//
// It is not a red line (a plain-text 429 loses no record and no secret), but it
// is the M5-03 hand-off the card claims is closed, and a claim with nothing
// behind it is what this round keeps finding.
func TestTapPage_RefusalBodyIsBrandedAtTheProductWiring(t *testing.T) {
	h, _, _ := newTapHandlerWithAudit(t, &fakePreviewer{preview: okPreview(true)},
		&fakeDirectory{facts: okFacts()}, fixedSessions())

	// The ADDRESS budget: it refuses before identity resolves, so it is the
	// cheapest of the two to reach and it exercises the same Refused renderer.
	var refused *httptest.ResponseRecorder
	for i := 0; i < httpx.TapAddressLimit()+3; i++ {
		w := get(t, h, tapURL())
		if w.Code == http.StatusTooManyRequests {
			refused = w
			break
		}
	}
	if refused == nil {
		t.Fatalf("nothing was refused within %d requests", httpx.TapAddressLimit()+3)
	}

	if ct := refused.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("429 Content-Type = %q, want text/html — the product wiring fell back to the "+
			"plain-text body, i.e. Refused is not connected (the audit's finding)", ct)
	}
	if !strings.Contains(refused.Body.String(), "Too many taps from here") {
		t.Fatalf("429 body is not the branded page: %q", refused.Body.String())
	}
	// The parts the middleware owns must survive a custom body.
	if refused.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After was dropped when the body became a page")
	}
	if refused.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", refused.Header().Get("Cache-Control"))
	}
}

// TestNewTap_RefusesATypedNilAuditRecorder. `rec == nil` is false for a nil
// POINTER carried in a non-nil interface, so the earlier check let one through
// and the failure would have surfaced as a panic at the first refusal rather
// than at boot. Unreachable in production (audit.New returns an error instead of
// a nil recorder) — which is precisely why the weaker check survived review.
func TestNewTap_RefusesATypedNilAuditRecorder(t *testing.T) {
	var typedNil *audit.Recorder // nil pointer, non-nil interface once passed

	_, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		&fakeSessions{}, typedNil, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("a typed-nil audit recorder was accepted: the trail would fail at the first " +
			"refused tap instead of at startup")
	}
}

// TestTapPage_CarriesAContentPolicy. Defence in depth — no injection vector was
// found here — with one clause that answers a concrete threat: this screen is a
// single large button that records attendance, which is the ideal thing to frame
// invisibly beneath somebody else's page.
func TestTapPage_CarriesAContentPolicy(t *testing.T) {
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, &fakeSessions{})

	csp := get(t, h, tapURL(), sessionCookie()).Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy on the tap page")
	}
	for _, want := range []string{
		"default-src 'none'",
		"script-src 'self'",
		"style-src 'self'",
		"font-src 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP is missing %q: %s", want, csp)
		}
	}
	// The policy must not have been written so tightly that the page's own
	// assets stop loading; both are same-origin, so 'self' has to cover them.
	body := get(t, h, tapURL(), sessionCookie()).Body.String()
	if !strings.Contains(body, `href="/static/css/app.css"`) || !strings.Contains(body, `src="/static/js/tap.js"`) {
		t.Fatal("the page no longer references its own assets")
	}
}

// TestNewTap_RefusesANilAuditRecorder: the omission that caused the finding is
// now a startup error, so it cannot be made again by leaving an argument out.
func TestNewTap_RefusesANilAuditRecorder(t *testing.T) {
	_, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		&fakeSessions{}, nil, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("a nil audit recorder was accepted: half of an accepted M5-03 criterion would silently not happen")
	}
}

// TestTapPage_MountOrderMetersTheSession. BySession must sit AFTER Identify. If
// Mount ever reorders them, BySession sees SessionUnresolved and answers 500 —
// deliberately loud — so this both proves the session budget is live and would
// catch the reordering.
func TestTapPage_MountOrderMetersTheSession(t *testing.T) {
	// The session budget is keyed on the session id, so the fake must resolve a
	// STABLE one — a fresh uuid per request would silently spread three requests
	// across three buckets and the test would pass while measuring nothing.
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		fixedSessions(), &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	tp.limiter = httpx.NewTapLimiter(httpx.TapLimitParams{
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Refused:       tp.renderTooManyRequests,
		SessionLimit:  2,
		SessionPeriod: time.Minute,
	})
	r := chi.NewRouter()
	tp.Mount(r)

	for i := 0; i < 2; i++ {
		if w := get(t, r, tapURL(), sessionCookie()); w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200", i, w.Code)
		}
	}
	w := get(t, r, tapURL(), sessionCookie())
	if w.Code == http.StatusInternalServerError {
		t.Fatal("500 from the session budget means Identify ran AFTER BySession — the mount order is broken")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 from the SESSION budget", w.Code)
	}
	// A session-less tap is legitimate (§5 row 3) and passes BySession unmetered;
	// it must still be answered, not refused.
	if w := get(t, r, tapURL()); w.Code != http.StatusSeeOther {
		t.Fatalf("session-less tap: status = %d, want 303 — SessionAbsent is not SessionUnresolved", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

// tapFixedSessionID is used wherever a test has to reproduce the handler's own
// signature, which is bound to the session id (tapcontext.go). fakeSessions
// mints a fresh uuid per call by default, so those tests pin it.
var tapFixedSessionID = uuid.MustParse("66666666-6666-4666-8666-666666666666")

func fixedSessions() *fakeSessions {
	return &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{ID: tapFixedSessionID, TenantID: testTenant, EmployeeID: testEmployee}, nil
	}}
}

var ctxFieldRE = regexp.MustCompile(`name="ctx" value="([^"]*)"`)

func contextFieldOf(t *testing.T, body string) string {
	t.Helper()
	m := ctxFieldRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no tap context field in the page")
	}
	return m[1]
}

// decodedPayloadOf returns the readable half of a signed context, so a test can
// assert on what actually travels rather than on base64.
func decodedPayloadOf(t *testing.T, signed string) string {
	t.Helper()
	encoded, _, ok := strings.Cut(signed, ".")
	if !ok {
		t.Fatalf("tap context has no signature separator: %q", signed)
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("tap context payload is not base64url: %v", err)
	}
	return string(raw)
}
