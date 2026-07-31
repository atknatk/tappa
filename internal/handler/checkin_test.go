package handler

// Unit tests for the POST boundary: what it parses, what it refuses, and what it
// hands to the domain. Everything that needs a real decision, a real counter or a
// real row lives in checkin_db_test.go against real Postgres — a fake cannot
// prove atomicity and cannot prove a record exists.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/session"
)

// fixedResolved is what fixedSessions() resolves every request to. The tests
// need the same ids the handler will see, because the tap context is signed over
// the SESSION ID and a fresh uuid per call would make every context unverifiable.
func fixedResolved() session.Resolved {
	return session.Resolved{ID: tapFixedSessionID, TenantID: testTenant, EmployeeID: testEmployee}
}

// fakeCheckins records what the handler asked for and answers with what the test
// wants. It exists so the BOUNDARY can be tested without a database; every test
// that asserts something about a DECISION uses the real service instead.
type fakeCheckins struct {
	calls  []checkin.Request
	result checkin.Result
	err    error
}

func (f *fakeCheckins) Record(_ context.Context, req checkin.Request) (checkin.Result, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return checkin.Result{}, f.err
	}
	res := f.result
	if res.Outcome == "" {
		res.Outcome = checkin.OutcomeRecorded
	}
	return res, nil
}

func (f *fakeCheckins) last(t *testing.T) checkin.Request {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatal("the handler never called the checkin service")
	}
	return f.calls[len(f.calls)-1]
}

// newCheckinHandler mounts the real routes — through Mount, so the middleware
// order under test is production's.
func newCheckinHandler(t *testing.T, svc *fakeCheckins, sess *fakeSessions) (http.Handler, *Tap) {
	t.Helper()
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		sess, svc, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	r := chi.NewRouter()
	tp.Mount(r)
	return r, tp
}

// mintedContext produces the signed context GET /t would have handed this
// session. It goes through the PRODUCTION minting function with the production
// key, so nothing about the value is faked — the only thing the test chooses is
// what the CMAC check concluded, which is exactly the one bit the page carries.
func mintedContext(t *testing.T, tp *Tap, sessionID uuid.UUID, c tapContext) string {
	t.Helper()
	v, err := tp.contexts.mint(c, sessionID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return v
}

func postForm(t *testing.T, h http.Handler, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// --- §5 row 3 at the boundary ------------------------------------------------

// TestCheckin_NoSessionRedirectsAndNeverReachesTheDomain is §5 row 3 as this
// endpoint can see it: no session, so the activation page and NO record. The
// second half is the one worth asserting — the domain service is not called at
// all, so there is no path by which a row could be written.
func TestCheckin_NoSessionRedirectsAndNeverReachesTheDomain(t *testing.T) {
	svc := &fakeCheckins{}
	h, _ := newCheckinHandler(t, svc, &fakeSessions{})

	w := postForm(t, h, url.Values{"ctx": {"anything"}})

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/activate" {
		t.Fatalf("Location = %q, want /activate", loc)
	}
	if len(svc.calls) != 0 {
		t.Fatalf("the domain was called %d times for a session-less tap: §5 row 3 writes NOTHING", len(svc.calls))
	}
}

// TestCheckin_UnresolvedIdentityIsNotNoSession. The zero Identity has Err == nil
// AND Live() == false, so a handler written as `if !id.Live() { activate }` would
// answer a database outage — or a route that forgot the middleware — with a
// stream of activation redirects. Here the handler is called DIRECTLY, which is
// that wiring mistake.
func TestCheckin_UnresolvedIdentityIsNotNoSession(t *testing.T) {
	svc := &fakeCheckins{}
	_, tp := newCheckinHandler(t, svc, &fakeSessions{})

	req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader("ctx=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	tp.Checkin(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — an unmetered, unidentified request must be loud", w.Code)
	}
	if strings.Contains(w.Body.String(), "activate") {
		t.Fatal("an unresolved identity was answered as if there were no session")
	}
	if len(svc.calls) != 0 {
		t.Fatal("the domain was called without a resolved identity")
	}
}

// --- what the server derives, and what the client cannot ---------------------

// TestCheckin_TagFactsComeFromTheSignedContextOnly is hand-off N2 at the
// boundary: the channel, the uid, the counter and the CMAC outcome are read from
// the SIGNED context, and form fields claiming otherwise are ignored.
//
// The exploit it closes: a client that could declare channel=qr would shed
// sys:sun-invalid and sys:tap-freshness, both of which are NFC-only, and turn a
// failed SUN into an ordinary QR tap.
func TestCheckin_TagFactsComeFromTheSignedContextOnly(t *testing.T) {
	svc := &fakeCheckins{}
	sess := fixedSessions()
	h, tp := newCheckinHandler(t, svc, sess)
	res := fixedResolved()

	ctxValue := mintedContext(t, tp, res.ID, tapContext{
		UID: "04AC7E55000601", Ctr: 7, Channel: "nfc", CMACVerified: true,
		TagTenantID: res.TenantID, LocationID: uuid.New(),
	})

	w := postForm(t, h, url.Values{
		"ctx": {ctxValue},
		// Every one of these is a claim the server must ignore.
		"channel":      {"qr"},
		"tag":          {"DEADBEEFDEADBE"},
		"ctr":          {"1"},
		"sun_valid":    {"true"},
		"practice":     {"true"},
		"verdict":      {"ok"},
		"employee_id":  {uuid.New().String()},
		"cmacverified": {"true"},
	}, sessionCookie())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	got := svc.last(t)
	if got.Channel != tap.ChannelNFC {
		t.Fatalf("channel = %q, want nfc: a form field overrode the signed context", got.Channel)
	}
	if got.TagUID != "04AC7E55000601" {
		t.Fatalf("tag uid = %q: a form field overrode the signed context", got.TagUID)
	}
	if got.Ctr != 7 {
		t.Fatalf("ctr = %d, want 7: a form field overrode the signed context", got.Ctr)
	}
	if !got.CMACVerified {
		t.Fatal("the CMAC outcome did not survive the round trip")
	}
	if got.EmployeeID != res.EmployeeID || got.SessionTenantID != res.TenantID {
		t.Fatal("identity came from the form rather than from the session")
	}
	if got.EnteredBy != nil {
		t.Fatal("entered_by was accepted from a tap request: it is a manual-entry field (M6-04)")
	}
}

// TestCheckin_TamperedContextIsRefusedAndNothingIsRecorded. The signature is what
// makes the context worth trusting; a single flipped byte must take the whole
// value out of play, and the domain must never see it.
func TestCheckin_TamperedContextIsRefusedAndNothingIsRecorded(t *testing.T) {
	svc := &fakeCheckins{}
	sess := fixedSessions()
	h, tp := newCheckinHandler(t, svc, sess)

	valid := mintedContext(t, tp, fixedResolved().ID, tapContext{
		UID: "04AC7E55000601", Ctr: 7, Channel: "nfc", CMACVerified: true,
		TagTenantID: fixedResolved().TenantID, LocationID: uuid.New(),
	})

	for name, tampered := range map[string]string{
		"payload edited":  "A" + valid[1:],
		"signature cut":   strings.Split(valid, ".")[0] + ".AAAA",
		"no signature":    strings.Split(valid, ".")[0],
		"empty":           "",
		"unrelated value": "not-a-context",
	} {
		t.Run(name, func(t *testing.T) {
			before := len(svc.calls)
			w := postForm(t, h, url.Values{"ctx": {tampered}}, sessionCookie())
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}
			if len(svc.calls) != before {
				t.Fatal("a tampered context reached the domain")
			}
		})
	}
}

// TestCheckin_ContextIsBoundToTheSessionThatWasServed. A context minted for one
// phone must not be spendable from another, which is what the session id in the
// MAC buys.
func TestCheckin_ContextIsBoundToTheSessionThatWasServed(t *testing.T) {
	svc := &fakeCheckins{}
	sess := fixedSessions()
	h, tp := newCheckinHandler(t, svc, sess)

	forSomebodyElse := mintedContext(t, tp, uuid.New(), tapContext{
		UID: "04AC7E55000601", Ctr: 7, Channel: "nfc", CMACVerified: true,
		TagTenantID: fixedResolved().TenantID, LocationID: uuid.New(),
	})

	w := postForm(t, h, url.Values{"ctx": {forSomebodyElse}}, sessionCookie())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if len(svc.calls) != 0 {
		t.Fatal("a context minted for another session was spent")
	}
}

// --- the client inputs that ARE accepted, and their bounds -------------------

// TestCheckin_GPSIsAcceptedAndBadFixesBecomeNoFix. GPS is BACKUP evidence: a
// missing or nonsensical coordinate must cost trust, never the record (§5 rows 6
// and 7 both handle "no fix"), so nothing here answers with an error.
func TestCheckin_GPSIsAcceptedAndBadFixesBecomeNoFix(t *testing.T) {
	sess := fixedSessions()
	cases := []struct {
		name     string
		lat, lng string
		want     *geo.Point
	}{
		{"a real fix", "35.918", "14.489", &geo.Point{Lat: 35.918, Lng: 14.489}},
		{"no fix at all", "", "", nil},
		{"half a fix", "35.918", "", nil},
		{"latitude out of range", "91.0", "14.489", nil},
		{"longitude out of range", "35.918", "181", nil},
		{"not a number", "north", "14.489", nil},
		{"NaN", "NaN", "14.489", nil},
		{"infinity", "Inf", "14.489", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeCheckins{}
			h, tp := newCheckinHandler(t, svc, sess)
			ctxValue := mintedContext(t, tp, fixedResolved().ID, tapContext{
				UID: "04AC7E55000601", Ctr: 7, Channel: "nfc", CMACVerified: true,
				TagTenantID: fixedResolved().TenantID, LocationID: uuid.New(),
			})
			w := postForm(t, h, url.Values{"ctx": {ctxValue}, "lat": {tc.lat}, "lng": {tc.lng}}, sessionCookie())
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: a bad fix must not cost the record", w.Code)
			}
			got := svc.last(t).GPS
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("a bad coordinate was passed on as %v", *got)
			case tc.want != nil && got == nil:
				t.Fatal("a valid coordinate was dropped")
			case tc.want != nil && (got.Lat != tc.want.Lat || got.Lng != tc.want.Lng):
				t.Fatalf("coordinate = %v, want %v", *got, *tc.want)
			}
		})
	}
}

// TestCheckin_OccurredAtIsOptionalAndAMalformedOneIsRefused.
//
// ABSENT means "now" and the domain substitutes the server clock — the normal
// case, since nothing on the tap page sends a time. PRESENT-BUT-MALFORMED is
// REFUSED rather than silently treated as absent: §7 forbids silent acceptance,
// and a client that meant to declare a time must not have its record quietly
// stamped with a different one.
//
// A WELL-FORMED value is passed THROUGH, unjudged. Whether it is plausible is
// sys:occurred-at-bound's answer, it is a guardrail, and the guardrail produces a
// RECORDED reject where a refusal here would produce nothing at all.
func TestCheckin_OccurredAtIsOptionalAndAMalformedOneIsRefused(t *testing.T) {
	sess := fixedSessions()
	far := time.Now().UTC().Add(-96 * time.Hour).Truncate(time.Second)
	// 0001-01-01T00:00:00Z is a WELL-FORMED RFC 3339 value and it used to collide
	// with the "nothing was declared" sentinel: the tap was accepted and stamped
	// with the server's clock, while a value one second later was rejected by
	// sys:occurred-at-bound. It must now arrive as a DECLARED time like any other.
	zero := time.Time{}.UTC()
	cases := []struct {
		name   string
		raw    string
		status int
		want   *time.Time
	}{
		{"absent means the server times it", "", http.StatusOK, nil},
		{"a queued tap declares its time", far.Format(time.RFC3339), http.StatusOK, &far},
		{"the zero instant is a declaration, not silence", "0001-01-01T00:00:00Z", http.StatusOK, &zero},
		{"malformed is refused", "yesterday", http.StatusBadRequest, nil},
		{"a naive local time is refused", "2026-07-31T09:00:00", http.StatusBadRequest, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeCheckins{}
			h, tp := newCheckinHandler(t, svc, sess)
			ctxValue := mintedContext(t, tp, fixedResolved().ID, tapContext{
				UID: "04AC7E55000601", Ctr: 7, Channel: "nfc", CMACVerified: true,
				TagTenantID: fixedResolved().TenantID, LocationID: uuid.New(),
			})
			w := postForm(t, h, url.Values{"ctx": {ctxValue}, occurredAtField: {tc.raw}}, sessionCookie())
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if tc.status != http.StatusOK {
				if len(svc.calls) != 0 {
					t.Fatal("a malformed occurred_at reached the domain")
				}
				return
			}
			got := svc.last(t).OccurredAt
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("an absent occurred_at was passed on as %v", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("a declared occurred_at (%v) was dropped to nil: the server would silently stamp "+
					"its own clock on a time the caller stated", *tc.want)
			case tc.want != nil && !got.Equal(*tc.want):
				t.Fatalf("occurred_at = %v, want %v", *got, *tc.want)
			}
		})
	}
}

// --- outcomes ----------------------------------------------------------------

// TestCheckin_ForeignTenantIsNotAnActivationRedirect. tap.Decide maps every
// redirect to RedirectActivation, so the two have to be told apart by the
// deciding sid — the note M4-03 left behind. Sending somebody with a perfectly
// valid session to an activation page is a nonsense instruction, and the page
// must not name the other organisation (§4.5).
func TestCheckin_ForeignTenantIsNotAnActivationRedirect(t *testing.T) {
	sess := fixedSessions()
	svc := &fakeCheckins{result: checkin.Result{Outcome: checkin.OutcomeForeignTenant}}
	h, tp := newCheckinHandler(t, svc, sess)
	ctxValue := mintedContext(t, tp, fixedResolved().ID, tapContext{
		UID: "04AC7E55000601", Ctr: 7, Channel: "nfc", CMACVerified: true,
		TagTenantID: uuid.New(), LocationID: uuid.New(),
	})

	w := postForm(t, h, url.Values{"ctx": {ctxValue}}, sessionCookie())

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "" {
		t.Fatalf("a foreign-tenant tap was redirected to %q", loc)
	}
	if body := w.Body.String(); strings.Contains(body, "activate") {
		t.Fatal("a foreign-tenant tap was sent to activation")
	}
}

// TestCheckin_AFailedWriteSaysSoAndDoesNotClaimSuccess is the §4.6 failure path
// at the boundary: when nothing was recorded, the screen must not imply that
// something was.
func TestCheckin_AFailedWriteSaysSoAndDoesNotClaimSuccess(t *testing.T) {
	sess := fixedSessions()
	svc := &fakeCheckins{err: errors.New("database is on fire")}
	h, tp := newCheckinHandler(t, svc, sess)
	ctxValue := mintedContext(t, tp, fixedResolved().ID, tapContext{
		UID: "04AC7E55000601", Ctr: 7, Channel: "nfc", CMACVerified: true,
		TagTenantID: fixedResolved().TenantID, LocationID: uuid.New(),
	})

	w := postForm(t, h, url.Values{"ctx": {ctxValue}}, sessionCookie())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	body := w.Body.String()
	for _, forbidden := range []string{"All done", "APPROVED", "Tapped in"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("a failed write rendered %q — the screen claimed a record that does not exist", forbidden)
		}
	}
	if !strings.Contains(body, "again") {
		t.Fatalf("a failed write did not tell the person what to do next: %s", body)
	}
}

// TestCheckin_CrossOriginPostIsRefused. Depth rather than the defence — the
// signature and SameSite=Lax are what actually stop a forged POST — but a request
// that announces itself as coming from elsewhere is refused, and refusing costs a
// legitimate tap nothing (a browser sends Origin on every cross-origin POST).
func TestCheckin_CrossOriginPostIsRefused(t *testing.T) {
	sess := fixedSessions()
	svc := &fakeCheckins{}
	h, tp := newCheckinHandler(t, svc, sess)
	ctxValue := mintedContext(t, tp, fixedResolved().ID, tapContext{
		UID: "04AC7E55000601", Ctr: 7, Channel: "nfc", CMACVerified: true,
		TagTenantID: fixedResolved().TenantID, LocationID: uuid.New(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/checkin",
		strings.NewReader(url.Values{"ctx": {ctxValue}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(sessionCookie())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if len(svc.calls) != 0 {
		t.Fatal("a cross-origin post reached the domain")
	}

	// POSITIVE CONTROL: the same request from our own origin goes through, so the
	// check above is measuring the Origin and not simply refusing everything.
	req = httptest.NewRequest(http.MethodPost, "/api/checkin",
		strings.NewReader(url.Values{"ctx": {ctxValue}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://localhost:8080")
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(sessionCookie())
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("same-origin status = %d, want 200", w.Code)
	}
}

// TestNewTap_RefusesANilCheckinService. POST /api/checkin exists to write a
// record; a nil service would not degrade it, it would delete it — and would do
// so at the first tap, as a panic, rather than at boot. A TYPED nil is refused
// too, since a nil pointer in a non-nil interface is not nil.
func TestNewTap_RefusesANilCheckinService(t *testing.T) {
	var typedNil *checkin.Service

	for name, svc := range map[string]checkinRecorder{"untyped nil": nil, "typed nil": typedNil} {
		t.Run(name, func(t *testing.T) {
			_, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
				&fakeSessions{}, svc, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil {
				t.Fatal("a nil checkin service was accepted: the endpoint would panic at the first tap")
			}
		})
	}
}

// TestCheckin_BodyIsBounded. ParseForm reads the body into memory and this
// endpoint sits in front of a database write, so an unbounded body would be a
// free way to spend the server's memory before any shield has looked at it.
func TestCheckin_BodyIsBounded(t *testing.T) {
	sess := fixedSessions()
	svc := &fakeCheckins{}
	h, _ := newCheckinHandler(t, svc, sess)

	huge := url.Values{"ctx": {strings.Repeat("A", checkinMaxBody*2)}}
	w := postForm(t, h, huge, sessionCookie())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an oversized body", w.Code)
	}
	if len(svc.calls) != 0 {
		t.Fatal("an oversized body reached the domain")
	}
}
