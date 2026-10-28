package handler

import (
	"bytes"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// adminswitch_test.go -- M14-10 "switch businesses" against fakes: the flag gate (closed = a path
// the panel never had, both routes), the forged/cross-site refusals (the
// TestAdminAuth_RefusesForgedAndCrossSiteRequests pattern), the one refusal that touches no
// cookie, and the budget. The database half -- a real switch between two businesses, the ordinary
// audit rows, the person-keyed budget over alternating switches, concurrent switches, a suspended
// target -- is adminswitch_db_test.go.

// switchOther is the other business the fakes offer.
var switchOther = uuid.MustParse("9f1c0a2e-0000-4000-8000-0000000000b2")

// switchBrowser is the real panel under the identity flag with a browser carrying a live owner's
// cookie (panelTest* ids). The fake lists the current business and switchOther, and a switch to
// switchOther succeeds with a fresh token unless the test sets its own hook.
func switchBrowser(t *testing.T, flag bool, admins *fakeAdmins) *browser {
	t.Helper()
	if admins.memberships == nil {
		admins.memberships = func() ([]adminauth.Membership, error) {
			return []adminauth.Membership{
				{TenantID: panelTestTenant, TenantName: "Current Ltd", Role: "owner", Current: true},
				{TenantID: switchOther, TenantName: "Other Ltd", Role: "manager"},
			}, nil
		}
	}
	if admins.switchTo == nil {
		admins.switchTo = func(target uuid.UUID) (adminauth.Issued, error) {
			if target != switchOther {
				return adminauth.Issued{}, adminauth.ErrSwitchRefused
			}
			return adminauth.Issued{
				Session: adminauth.Session{ID: uuid.New(), TenantID: switchOther, AdminUserID: uuid.New()},
				Token:   fakeIssuedToken(),
			}, nil
		}
	}
	return signOutBrowser(t, flag, admins, &fakeTrail{}, "owner")
}

// switchCSRF loads the switch page and returns its synchronizer token (the browser learns the
// page's tenant binding on the way, as a real one submits the form it was shown).
func switchCSRF(t *testing.T, b *browser) string {
	t.Helper()
	rec := b.do(http.MethodGet, adminSwitchHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", adminSwitchHref, rec.Code)
	}
	return csrfFrom(t, rec.Body.String())
}

// setsAnyCookie reports whether the response writes any Set-Cookie at all.
func setsAnyCookie(rec *httptest.ResponseRecorder) bool {
	return len(rec.Result().Header.Values("Set-Cookie")) > 0
}

// TestAdminSwitch_FlagOffIsAPathThePanelNeverHad (ADR 0031 §8): with TAPPA_ADMIN_IDENTITY off,
// /admin/switch answers -- for EVERY method, signed in and out -- exactly what a sibling path the
// panel never had answers, and nothing reaches the resolver, Switch or Memberships. CONTROL: with
// the flag on the GET is the page and the POST reaches the handler.
//
// MUTATION (measured): either route registered with a plain r.Get / r.Post instead of mountGated
// -> red.
func TestAdminSwitch_FlagOffIsAPathThePanelNeverHad(t *testing.T) {
	for _, method := range gateMethods {
		for _, signedIn := range []bool{false, true} {
			admins := &fakeAdmins{}
			b := switchBrowser(t, false, admins)
			if !signedIn {
				delete(b.cookies, adminauth.CookieName)
			}
			answer := func(path string) (int, http.Header, string) {
				req := httptest.NewRequest(method, path, strings.NewReader("x=1"))
				req.RemoteAddr = b.ip
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", testBaseURL)
				for n, v := range b.cookies {
					req.AddCookie(&http.Cookie{Name: n, Value: v})
				}
				rec := httptest.NewRecorder()
				b.h.ServeHTTP(rec, req)
				return rec.Code, rec.Header(), rec.Body.String()
			}
			gs, gh, gb := answer(adminSwitchHref)
			ns, nh, nb := answer("/admin/zz-never-mounted")
			if gs != http.StatusNotFound || gs != ns || gb != nb || !maps.EqualFunc(gh, nh, slices.Equal[[]string]) {
				t.Errorf("%s signed-in=%v: closed route %d %v %q; never-had path %d %v %q", method, signedIn, gs, gh, gb, ns, nh, nb)
			}
			if len(admins.switchCalls()) != 0 || admins.verifiedCount() != 0 {
				t.Errorf("%s signed-in=%v: the closed route reached Switch %d time(s), the resolver %d",
					method, signedIn, len(admins.switchCalls()), admins.verifiedCount())
			}
		}
	}

	admins := &fakeAdmins{}
	on := switchBrowser(t, true, admins)
	csrf := switchCSRF(t, on)
	if rec := on.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {csrf}, "tenant_id": {switchOther.String()}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("CONTROL: with the flag on the switch answered %d", rec.Code)
	}
}

// TestAdminSwitch_RefusesForgedAndCrossSiteRequests is TestAdminAuth_RefusesForgedAndCrossSiteRequests
// for the switch: a foreign Origin, no Origin, a forged synchronizer token, none, another
// session's token, a form without the tenant binding, and no panel cookie are each refused --
// Switch is never called and no Set-Cookie is written. CONTROL: the page's own form switches.
//
// MUTATION (measured): the synchronizer check removed from adminSwitch -> the forged and empty
// rows reach Switch, red.
func TestAdminSwitch_RefusesForgedAndCrossSiteRequests(t *testing.T) {
	otherSession, err := adminauth.NewSwitchTokens(adminTestConfig().SessionHMACKey)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		origin     string
		csrf       func(real string, b *browser) string
		unbound    bool
		dropCookie bool
		wantStatus int
	}{
		{"a foreign Origin", "https://evil.example", func(s string, _ *browser) string { return s }, false, false, http.StatusSeeOther},
		{"no Origin and no fetch metadata", "", func(s string, _ *browser) string { return s }, false, false, http.StatusSeeOther},
		{"a forged synchronizer token", testBaseURL, func(string, *browser) string { return strings.Repeat("A", adminauth.SwitchTokenLen) }, false, false, http.StatusBadRequest},
		{"no synchronizer token", testBaseURL, func(string, *browser) string { return "" }, false, false, http.StatusBadRequest},
		{"another session's synchronizer token", testBaseURL, func(_ string, b *browser) string {
			req := httptest.NewRequest(http.MethodGet, adminauth.CookiePath, nil)
			req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: b.cookies[adminauth.CookieName]})
			var c adminauth.Cookies
			tok, _ := c.Read(req)
			v, err := otherSession.Mint(tok, uuid.New())
			if err != nil {
				t.Fatal(err)
			}
			return v
		}, false, false, http.StatusBadRequest},
		{"no tenant binding on the action", testBaseURL, func(s string, _ *browser) string { return s }, true, false, http.StatusConflict},
		{"no panel cookie at all", testBaseURL, func(s string, _ *browser) string { return s }, false, true, http.StatusSeeOther},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			admins := &fakeAdmins{}
			b := switchBrowser(t, true, admins)
			csrf := tc.csrf(switchCSRF(t, b), b)
			if tc.dropCookie {
				delete(b.cookies, adminauth.CookieName)
			}
			b.origin = tc.origin
			b.unbound = tc.unbound
			rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {csrf}, "tenant_id": {switchOther.String()}})
			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if n := len(admins.switchCalls()); n != 0 {
				t.Fatalf("Switch ran %d time(s) despite %s", n, tc.name)
			}
			if setsAnyCookie(rec) {
				t.Fatalf("the refusal wrote a cookie: %v", rec.Result().Header.Values("Set-Cookie"))
			}
		})
	}

	// THE ORDER IS PINNED (M14-10's third-eye audit): adminSwitchLimit forged POSTs from one
	// browser -- each a 400 -- spend nothing of the budget, so the page's real form still switches.
	// The loop runs past BOTH limits, so either bucket charged ahead of the synchronizer is red.
	// MUTATION (measured): both budgets charged before the synchronizer -> the real form is a 429,
	// red; only the person bucket's charge moved before it -> red as well.
	t.Run("forged posts spend no budget", func(t *testing.T) {
		admins := &fakeAdmins{}
		b := switchBrowser(t, true, admins)
		csrf := switchCSRF(t, b)
		forged := strings.Repeat("A", adminauth.SwitchTokenLen)
		for i := 1; i <= adminSwitchPersonLimit+1; i++ {
			if rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {forged}, "tenant_id": {switchOther.String()}}); rec.Code != http.StatusBadRequest {
				t.Fatalf("forged POST #%d: %d, want 400", i, rec.Code)
			}
		}
		rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {csrf}, "tenant_id": {switchOther.String()}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("the real form after %d forged POSTs: %d, want 303 -- the forged ones spent the budget", adminSwitchPersonLimit+1, rec.Code)
		}
	})

	t.Run("control: the page's own form", func(t *testing.T) {
		admins := &fakeAdmins{}
		b := switchBrowser(t, true, admins)
		before := b.cookies[adminauth.CookieName]
		rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {switchCSRF(t, b)}, "tenant_id": {switchOther.String()}})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != transactionsHref {
			t.Fatalf("status %d %q, want 303 to %s", rec.Code, rec.Header().Get("Location"), transactionsHref)
		}
		if got := admins.switchCalls(); !slices.Equal(got, []uuid.UUID{switchOther}) {
			t.Fatalf("Switch calls %v, want one to %s", got, switchOther)
		}
		if after := b.cookies[adminauth.CookieName]; after == before || after == "" {
			t.Fatal("the switch did not set the new session's cookie")
		}
		if strings.Contains(rec.Header().Get("Location"), "tb=") {
			t.Fatal("the redirect carries the binding")
		}
	})
}

// TestAdminSwitch_ARefusalIsOneAnswerAndTouchesNoCookie (ADR 0031 §5.4, bulgu 29): a target the
// person holds no membership in, a target that is not a uuid, and a lost concurrent race are ONE
// answer -- the same status and body -- and none writes a Set-Cookie (neither a new cookie nor a
// deletion). A database failure is a 500 that touches no cookie either.
//
// MUTATION (measured): the refusal branch clearing the cookie (a.cookies.Clear) -> red here and in
// TestAdminSwitchDB_ConcurrentSwitchesHaveOneWinner.
func TestAdminSwitch_ARefusalIsOneAnswerAndTouchesNoCookie(t *testing.T) {
	answers := map[string]*httptest.ResponseRecorder{}
	for name, target := range map[string]string{
		"not a member":    uuid.NewString(),
		"not a uuid":      "kebab",
		"the current one": panelTestTenant.String(),
		"lost the race":   switchOther.String(),
	} {
		admins := &fakeAdmins{}
		if name == "lost the race" {
			admins.switchTo = func(uuid.UUID) (adminauth.Issued, error) { return adminauth.Issued{}, adminauth.ErrSwitchRefused }
		}
		b := switchBrowser(t, true, admins)
		rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {switchCSRF(t, b)}, "tenant_id": {target}})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, rec.Code)
		}
		if setsAnyCookie(rec) {
			t.Errorf("%s: the refusal wrote a cookie: %v", name, rec.Result().Header.Values("Set-Cookie"))
		}
		answers[name] = rec
	}
	var first []byte
	for name, rec := range answers {
		if first == nil {
			first = rec.Body.Bytes()
			continue
		}
		if !bytes.Equal(rec.Body.Bytes(), first) {
			t.Errorf("%s: the refusal's body differs from another reason's -- the reasons are told apart", name)
		}
	}

	admins := &fakeAdmins{switchTo: func(uuid.UUID) (adminauth.Issued, error) { return adminauth.Issued{}, errors.New("db down") }}
	b := switchBrowser(t, true, admins)
	rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {switchCSRF(t, b)}, "tenant_id": {switchOther.String()}})
	if rec.Code != http.StatusInternalServerError || setsAnyCookie(rec) {
		t.Errorf("a failed switch: %d, cookies %v; want 500 and none", rec.Code, rec.Result().Header.Values("Set-Cookie"))
	}
}

// TestAdminSwitch_TheBudgetRefusesAtLimitPlusOne: adminSwitchLimit switches from one address pass,
// the next is a 429 that never reaches Switch; another address is not refused (the address is
// part of the key). MUTATION (measured): the 429 branch disabled -> red. The person-keyed half -- the count surviving a new session on every hop -- is
// adminswitch_db_test.go's, against the real Resolved.
func TestAdminSwitch_TheBudgetRefusesAtLimitPlusOne(t *testing.T) {
	admins := &fakeAdmins{switchTo: func(uuid.UUID) (adminauth.Issued, error) { return adminauth.Issued{}, adminauth.ErrSwitchRefused }}
	b := switchBrowser(t, true, admins)
	csrf := switchCSRF(t, b)
	for i := 1; i <= adminSwitchLimit+1; i++ {
		rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {csrf}, "tenant_id": {switchOther.String()}})
		want := http.StatusForbidden
		if i > adminSwitchLimit {
			want = http.StatusTooManyRequests
		}
		if rec.Code != want {
			t.Fatalf("switch #%d: %d, want %d", i, rec.Code, want)
		}
	}
	if n := len(admins.switchCalls()); n != adminSwitchLimit {
		t.Fatalf("Switch ran %d times, want %d -- the refused one reached it", n, adminSwitchLimit)
	}
	b.ip = "198.51.100.7:4321"
	if rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {csrf}, "tenant_id": {switchOther.String()}}); rec.Code != http.StatusForbidden {
		t.Fatalf("another address: %d, want the ordinary refusal (403)", rec.Code)
	}
}

// TestAdminSwitch_ThePageListsTheOtherBusinessesWithBoundForms: the GET names the current business
// and draws one form per OTHER business, each posting to the switch with this session's binding
// and a synchronizer field -- and no form for the current one.
func TestAdminSwitch_ThePageListsTheOtherBusinessesWithBoundForms(t *testing.T) {
	b := switchBrowser(t, true, &fakeAdmins{})
	b.raw = true
	rec := b.do(http.MethodGet, adminSwitchHref, nil)
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(page, "Current Ltd") || !strings.Contains(page, "Continue to Other Ltd") {
		t.Fatalf("the page (%d) does not list the businesses: %s", rec.Code, page)
	}
	if strings.Contains(page, `value="`+panelTestTenant.String()+`"`) {
		t.Error("the page offers a switch to the current business")
	}
	if !strings.Contains(page, `value="`+switchOther.String()+`"`) {
		t.Error("the page offers no switch to the other business")
	}
	want := bindingFor(b.h, b.cookies[adminauth.CookieName])
	if want == "" || !strings.Contains(page, `action="`+adminSwitchHref+`?tb=`+want+`"`) {
		t.Errorf("the switch form does not post with this session's binding")
	}
}
