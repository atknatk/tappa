package handler

// plaqueencodebusiness_test.go -- the encode relay's one read (2026-11-10): which business the
// Android app's panel session encodes for, asked before a plaque is touched
// (plaqueencode.go -> plaqueEncodeBusiness; the chain is AdminAuth.Mount's).

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
)

// businessReadName needs escaping in JSON and carries Maltese letters, like the shell's own
// fixture: the read must hand the session's name over byte for byte.
const businessReadName = `Ħaġar <Qim> & "Kebabs"`

// businessReadAdmins is a live owner of panelTestTenant whose session resolves with
// businessReadName and the given suspension. The membership list -- the switch page's
// cross-business read -- fails the test if anything asks for it: this read must not.
func businessReadAdmins(t *testing.T, code *db.SuspensionReason) *fakeAdmins {
	t.Helper()
	admins := suspendedIdentity(panelTestTenant, panelTestAdmin, panelTestSession, code)
	inner := admins.verify
	admins.verify = func() (adminauth.Resolved, error) {
		r, err := inner()
		r.TenantName = businessReadName
		return r, err
	}
	admins.memberships = func() ([]adminauth.Membership, error) {
		t.Errorf("the business read asked for the person's membership list")
		return nil, nil
	}
	return admins
}

// businessRouter is the REAL AdminAuth and router (encodeRouter's shape) with the identity flag as
// given and the caller's trail and log.
func businessRouter(t *testing.T, admins *fakeAdmins, enc PlaqueEncoder, flag bool, trail *fakeTrail, log *slog.Logger) http.Handler {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if trail == nil {
		trail = &fakeTrail{}
	}
	cfg := adminTestConfig()
	cfg.AdminIdentity = flag
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{},
		newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(),
		enc, &fakeNotices{}, cfg, log)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	return r
}

// readBusiness is the app's request: a GET with the given Origin ("" sends none) and
// Sec-Fetch-Site ("" sends none), with the panel cookie when signedIn.
func readBusiness(h http.Handler, origin, fetchSite string, signedIn bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, plaqueEncodeBusinessHref, nil)
	req.RemoteAddr = "203.0.113.9:1234"
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", fetchSite)
	}
	if signedIn {
		req.AddCookie(panelCookie())
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// wantBusinessBody is the exact body: three fields, in the struct's order, nothing else.
func wantBusinessBody(t *testing.T, canSwitch, suspended bool) string {
	t.Helper()
	// Its own struct, not encodeBusiness: the expectation must not move when the type does.
	b, err := json.Marshal(struct {
		B   string `json:"business"`
		Sw  bool   `json:"switch"`
		Sus bool   `json:"suspended"`
	}{businessReadName, canSwitch, suspended})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// TestPlaqueEncodeBusiness_AnswersOnlyTheSessionsOwnBusiness: the body is byte for byte the
// session's name and the two flags -- no id, no role, no other business -- and the switch flag
// is the flag row (open with TAPPA_ADMIN_IDENTITY on, for one business as for many), never a
// membership count: the list is never asked for (businessReadAdmins fails the test if it is).
func TestPlaqueEncodeBusiness_AnswersOnlyTheSessionsOwnBusiness(t *testing.T) {
	for _, flag := range []bool{true, false} {
		admins := businessReadAdmins(t, nil)
		h := businessRouter(t, admins, &fakeEncoder{}, flag, nil, nil)
		rec := readBusiness(h, testBaseURL, "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("flag=%v: GET %s answered %d, want 200. body=%q", flag, plaqueEncodeBusinessHref, rec.Code, rec.Body.String())
		}
		if got, want := rec.Body.String(), wantBusinessBody(t, flag, false); got != want {
			t.Fatalf("flag=%v: body = %q, want exactly %q", flag, got, want)
		}
		for name, want := range map[string]string{
			"Content-Type":           "application/json; charset=utf-8",
			"Cache-Control":          "no-store",
			"X-Content-Type-Options": "nosniff",
		} {
			if got := rec.Header().Get(name); got != want {
				t.Errorf("flag=%v: %s = %q, want %q", flag, name, got, want)
			}
		}
		if n := len(rec.Result().Cookies()); n != 0 {
			t.Errorf("flag=%v: the read set %d cookie(s); it must touch none", flag, n)
		}
	}
}

// TestPlaqueEncodeBusiness_TheBodyHasExactlyThreeFields pins the type the one writer serialises,
// as TestEncodeReply_HasExactlyTheFourFieldsTheRelayNeeds pins the round's: a fourth field is a
// decision about what leaves the process, not an edit.
func TestPlaqueEncodeBusiness_TheBodyHasExactlyThreeFields(t *testing.T) {
	typ := reflect.TypeOf(encodeBusiness{})
	var got []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		got = append(got, f.Name+":"+f.Type.Kind().String()+":"+f.Tag.Get("json"))
	}
	want := []string{"Business:string:business", "Switch:bool:switch", "Suspended:bool:suspended"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("encodeBusiness is %v, want %v", got, want)
	}
}

// TestPlaqueEncodeBusiness_NeedsASignedInSession: no cookie -> requireAdmin's 303 to the sign-in,
// no JSON, and the encoder untouched.
func TestPlaqueEncodeBusiness_NeedsASignedInSession(t *testing.T) {
	enc := &fakeEncoder{}
	anonymous := &fakeAdmins{verify: func() (adminauth.Resolved, error) { return adminauth.Resolved{}, adminauth.ErrNoSession }}
	h := businessRouter(t, anonymous, enc, true, nil, nil)
	for _, signedIn := range []bool{false, true} {
		rec := readBusiness(h, testBaseURL, "", signedIn)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != adminLoginPath {
			t.Fatalf("cookie=%v, no live session: %d %q, want 303 %s", signedIn, rec.Code, rec.Header().Get("Location"), adminLoginPath)
		}
		if strings.Contains(rec.Body.String(), "business") {
			t.Fatalf("an unauthenticated read got a body naming a business: %q", rec.Body.String())
		}
	}
	if n := len(enc.beganWith()); n != 0 {
		t.Fatalf("the read opened %d round(s)", n)
	}
}

// TestPlaqueEncodeBusiness_TheAppMustDeclareItsOrigin: the round routes' Origin rule, ahead of the
// resolver. No Origin -> 303 /admin without the session being looked up; a wrong Origin, or a
// same-site / cross-site fetch beside the right one (GATE-1), likewise; the panel's own Origin,
// or a same-origin fetch, is served. The positive controls keep the refusals from passing over a
// route that refuses everything.
func TestPlaqueEncodeBusiness_TheAppMustDeclareItsOrigin(t *testing.T) {
	admins := businessReadAdmins(t, nil)
	h := businessRouter(t, admins, &fakeEncoder{}, true, nil, nil)
	refused := []struct{ origin, fetchSite string }{
		{"", ""},
		{"https://evil.example", ""},
		{testBaseURL, "same-site"},
		{testBaseURL, "cross-site"},
		{"", "none"},
	}
	for _, c := range refused {
		before := admins.verifyCalls
		rec := readBusiness(h, c.origin, c.fetchSite, true)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
			t.Fatalf("Origin %q, Sec-Fetch-Site %q: %d %q, want 303 /admin (sameOriginGate)",
				c.origin, c.fetchSite, rec.Code, rec.Header().Get("Location"))
		}
		if admins.verifyCalls != before {
			t.Fatalf("Origin %q, Sec-Fetch-Site %q: the session was resolved before the refusal; "+
				"the Origin check must run ahead of the resolver", c.origin, c.fetchSite)
		}
	}
	for _, c := range []struct{ origin, fetchSite string }{{testBaseURL, ""}, {"", "same-origin"}} {
		if rec := readBusiness(h, c.origin, c.fetchSite, true); rec.Code != http.StatusOK {
			t.Fatalf("Origin %q, Sec-Fetch-Site %q: %d, want 200. body=%q", c.origin, c.fetchSite, rec.Code, rec.Body.String())
		}
	}
}

// TestPlaqueEncodeBusiness_ASuspendedBusinessIsNamedAndNoRefusalIsRecorded: the read answers a
// suspended business with its name and suspended=true, so the app can say which business is paused
// and offer the switch, and starts no round -- as it stops on the round routes' `suspended` word.
// It writes NO tenant_write_refused row (it is a read; ADR 0025 §4). The control: the SAME session's
// round begin still meets the suspension ring -- 403 `suspended` and exactly one refusal row.
func TestPlaqueEncodeBusiness_ASuspendedBusinessIsNamedAndNoRefusalIsRecorded(t *testing.T) {
	code := db.SuspensionNonPayment
	enc := &fakeEncoder{}
	trail := &fakeTrail{}
	h := businessRouter(t, businessReadAdmins(t, &code), enc, true, trail, nil)

	rec := readBusiness(h, testBaseURL, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("a suspended business's read answered %d, want 200. body=%q", rec.Code, rec.Body.String())
	}
	if got, want := rec.Body.String(), wantBusinessBody(t, true, true); got != want {
		t.Fatalf("body = %q, want exactly %q", got, want)
	}
	if n := len(trail.events); n != 0 {
		t.Fatalf("the read wrote %d audit row(s) (%v); a read is not a refused write", n, trail.events)
	}

	b := newBrowser(t, h)
	b.cookies[adminauth.CookieName] = panelCookie().Value
	begin := b.do(http.MethodPost, plaqueEncodeHref, url.Values{})
	if begin.Code != http.StatusForbidden || faultOf(t, begin) != faultSuspended {
		t.Fatalf("the control: the round begin answered %d %q, want 403 %q", begin.Code, begin.Body.String(), faultSuspended)
	}
	if n := len(trail.events); n != 1 || trail.events[0].Action != ActionTenantWriteRefused {
		t.Fatalf("the control: %d row(s) %v, want exactly one %s", n, trail.events, ActionTenantWriteRefused)
	}
	if n := len(enc.beganWith()); n != 0 {
		t.Fatalf("a suspended business opened %d round(s)", n)
	}
}

// TestPlaqueEncodeBusiness_WithoutAnEncoderSaysSoBeforeAPlaqueIsTouched: the round routes' 503
// word, so the app can stop before a plaque is laid on the phone.
func TestPlaqueEncodeBusiness_WithoutAnEncoderSaysSoBeforeAPlaqueIsTouched(t *testing.T) {
	h := businessRouter(t, businessReadAdmins(t, nil), nil, true, nil, nil)
	rec := readBusiness(h, testBaseURL, "", true)
	if rec.Code != http.StatusServiceUnavailable || faultOf(t, rec) != faultUnavailable {
		t.Fatalf("no encoder: %d %q, want 503 %q", rec.Code, rec.Body.String(), faultUnavailable)
	}
}

// TestPlaqueEncodeBusiness_SpendsNoRoundBudget: the read is not behind encodeGate. More reads than
// the whole per-session round budget (adminEncodeLimit) still leave a round to open. Under
// adminSessionLimit, so the panel's own session budget is not what is measured.
func TestPlaqueEncodeBusiness_SpendsNoRoundBudget(t *testing.T) {
	if adminEncodeLimit+2 > adminSessionLimit {
		t.Fatalf("the measurement needs adminEncodeLimit+2 (%d) <= adminSessionLimit (%d)", adminEncodeLimit+2, adminSessionLimit)
	}
	enc := &fakeEncoder{}
	h := businessRouter(t, businessReadAdmins(t, nil), enc, true, nil, nil)
	for i := 0; i <= adminEncodeLimit; i++ {
		if rec := readBusiness(h, testBaseURL, "", true); rec.Code != http.StatusOK {
			t.Fatalf("read %d answered %d", i+1, rec.Code)
		}
	}
	b := newBrowser(t, h)
	b.cookies[adminauth.CookieName] = panelCookie().Value
	if rec := b.do(http.MethodPost, plaqueEncodeHref, url.Values{}); rec.Code != http.StatusOK {
		t.Fatalf("after %d reads the round begin answered %d %q, want 200: the read spent the round budget",
			adminEncodeLimit+1, rec.Code, rec.Body.String())
	}
}

// TestPlaqueEncodeBusiness_NeverLogsTheName: the name is display data for the person's own phone.
func TestPlaqueEncodeBusiness_NeverLogsTheName(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := businessRouter(t, businessReadAdmins(t, nil), &fakeEncoder{}, true, nil, log)
	if rec := readBusiness(h, testBaseURL, "", true); rec.Code != http.StatusOK {
		t.Fatalf("read answered %d", rec.Code)
	}
	if strings.Contains(logged.String(), "Qim") {
		t.Fatalf("the business name reached the log:\n%s", logged.String())
	}
}
