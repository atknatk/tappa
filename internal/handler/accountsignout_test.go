package handler

import (
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
)

// accountsignout_test.go -- M14-11 "Sign out everywhere" against fakes: the flag gate (closed = a
// path the panel never had), the handler's two paths (through the identity: no row of its own;
// today's unlinked row: one ordinary sign-out row per revoked session), the Origin gate and a
// failed revocation. The database half -- every business's sessions dead after one press, one
// ordinary row per business, none for a disabled membership, a suspended business -- is
// accountsignout_db_test.go.

// signOutBrowser is the real panel under the given identity flag, with a browser carrying a panel
// cookie of a live admin of role (panelTest* ids).
func signOutBrowser(t *testing.T, flag bool, admins *fakeAdmins, trail *fakeTrail, role string) *browser {
	t.Helper()
	if admins.verify == nil {
		admins.verify = func() (adminauth.Resolved, error) {
			return adminauth.Resolved{
				SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
				Role: role, FullName: "Admin Person",
			}, nil
		}
	}
	cfg := adminTestConfig()
	cfg.AdminIdentity = flag
	b := newBrowser(t, newAdminRouterWithActionsUnder(t, cfg, admins, trail, newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// clearsTheCookie reports whether the response deletes the panel cookie.
func clearsTheCookie(rec *httptest.ResponseRecorder) bool {
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == adminauth.CookieName && (ck.MaxAge < 0 || ck.Value == "") {
			return true
		}
	}
	return false
}

// TestSignOutEverywhere_FlagOffIsAPathThePanelNeverHad (ADR 0031 §8): with TAPPA_ADMIN_IDENTITY off
// the route answers, for EVERY method, signed in and signed out, exactly what a sibling path the
// panel never had answers -- status, every header, body -- and nothing reaches RevokeAll or the
// resolver; the account page draws no trace of the card. CONTROL: with the flag on the same POST
// reaches the handler (303 to the sign-in).
//
// MUTATION (measured): the route registered with a plain r.Post instead of mountGated -> the POST
// is the handler's 303 and the other methods 405 with an Allow header, red; the card drawn without
// the flag (the view's href set unconditionally) -> the account page names it, red.
func TestSignOutEverywhere_FlagOffIsAPathThePanelNeverHad(t *testing.T) {
	for _, method := range gateMethods {
		for _, signedIn := range []bool{false, true} {
			admins := &fakeAdmins{}
			b := signOutBrowser(t, false, admins, &fakeTrail{}, "owner")
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
			gs, gh, gb := answer(accountSignOutEverywhereHref)
			ns, nh, nb := answer(accountHref + "/zz-never-mounted")
			if gs != http.StatusNotFound || gs != ns || gb != nb || !maps.EqualFunc(gh, nh, slices.Equal[[]string]) {
				t.Errorf("%s signed-in=%v: closed route %d %v %q; never-had path %d %v %q", method, signedIn, gs, gh, gb, ns, nh, nb)
			}
			if n := len(admins.revokeAllCalls()); n != 0 || admins.verifiedCount() != 0 {
				t.Errorf("%s signed-in=%v: the closed route reached RevokeAll %d time(s), the resolver %d", method, signedIn, n, admins.verifiedCount())
			}
		}
	}

	off := signOutBrowser(t, false, &fakeAdmins{}, &fakeTrail{}, "owner")
	page := off.do(http.MethodGet, accountHref, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("CONTROL: the account page answered %d", page.Code)
	}
	for _, trace := range []string{"Sign out everywhere", accountSignOutEverywhereHref, "account-everywhere-heading"} {
		if strings.Contains(page.Body.String(), trace) {
			t.Errorf("with the flag off the account page carries %q", trace)
		}
	}

	on := signOutBrowser(t, true, &fakeAdmins{}, &fakeTrail{}, "owner")
	if rec := on.do(http.MethodPost, accountSignOutEverywhereHref, nil); rec.Code != http.StatusSeeOther {
		t.Errorf("CONTROL: with the flag on the POST answered %d, want the handler's 303", rec.Code)
	}
}

// TestSignOutEverywhere_TheAccountPageOffersItToEveryLiveAdmin: flag on, an owner and a manager
// both get the card -- one form, posting to the route, a submit button, no field (nothing from the
// request is an input to the identity).
func TestSignOutEverywhere_TheAccountPageOffersItToEveryLiveAdmin(t *testing.T) {
	for _, role := range []string{"owner", "manager"} {
		b := signOutBrowser(t, true, &fakeAdmins{}, &fakeTrail{}, role)
		rec := b.do(http.MethodGet, accountHref, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: the account page answered %d", role, rec.Code)
		}
		if n := strings.Count(body, `action="`+accountSignOutEverywhereHref+`"`); n != 1 {
			t.Fatalf("%s: %d form(s) post to %s, want 1", role, n, accountSignOutEverywhereHref)
		}
		i := strings.Index(body, `action="`+accountSignOutEverywhereHref+`"`)
		form := body[i : i+strings.Index(body[i:], "</form>")]
		if !strings.Contains(form, `type="submit">Sign out everywhere</button>`) {
			t.Errorf("%s: the form has no \"Sign out everywhere\" submit button: %q", role, form)
		}
		if strings.Contains(form, "<input") {
			t.Errorf("%s: the form carries a field; it must carry none: %q", role, form)
		}
	}
}

// TestSignOutEverywhere_SignsThePersonOutAndLandsOnTheSignIn is the handler's contract over both
// paths. Every press: RevokeAll once, with the SESSION's tenant, admin and session and
// keepCurrent=false (this device too); the cookie is cleared; 303 to the sign-in form.
//
//	through the identity  the database wrote every business its rows: the handler writes NONE
//	unlinked row          today's case: one ordinary admin.logout row per revoked session, this
//	                      business, this admin -- adminLoggedOutEvent, byte for byte -- and no
//	                      count anywhere in them
//
// MUTATION (measured): keepCurrent=true -> red; the rows written on the identity path too -> red
// (doubled trail); the loop over Sessions dropped -> red; the cookie not cleared -> red.
func TestSignOutEverywhere_SignsThePersonOutAndLandsOnTheSignIn(t *testing.T) {
	other := uuid.New()
	for _, tc := range []struct {
		name  string
		rv    adminauth.Revocation
		owned []uuid.UUID // the rows the handler must write, by session
	}{
		{"through the identity", adminauth.Revocation{Revoked: 3, ThroughIdentity: true}, nil},
		{"unlinked row", adminauth.Revocation{Revoked: 2, Sessions: []uuid.UUID{panelTestSession, other}}, []uuid.UUID{panelTestSession, other}},
	} {
		for _, role := range []string{"owner", "manager"} {
			t.Run(tc.name+"/"+role, func(t *testing.T) {
				rv := tc.rv
				admins := &fakeAdmins{revokeAll: func(uuid.UUID, uuid.UUID, uuid.UUID, bool) (adminauth.Revocation, error) { return rv, nil }}
				trail := &fakeTrail{}
				b := signOutBrowser(t, true, admins, trail, role)
				rec := b.do(http.MethodPost, accountSignOutEverywhereHref, nil)
				if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != adminLoginPath {
					t.Fatalf("answered %d %q, want 303 to %s", rec.Code, rec.Header().Get("Location"), adminLoginPath)
				}
				if !clearsTheCookie(rec) {
					t.Error("the panel cookie was not cleared")
				}
				calls := admins.revokeAllCalls()
				want := revokeAllArgs{panelTestTenant, panelTestAdmin, panelTestSession, false}
				if len(calls) != 1 || calls[0] != want {
					t.Fatalf("RevokeAll calls %+v, want exactly %+v", calls, want)
				}
				var wantRows []audit.Event
				for _, s := range tc.owned {
					wantRows = append(wantRows, adminLoggedOutEvent(panelTestTenant, panelTestAdmin, s))
				}
				if got := trail.eventsSnapshot(); !slices.EqualFunc(got, wantRows, func(a, b audit.Event) bool {
					return a.TenantID == b.TenantID && *a.ActorID == *b.ActorID && a.Action == b.Action &&
						a.Target == b.Target && a.Detail == b.Detail
				}) {
					t.Errorf("the handler wrote %+v, want %+v", got, wantRows)
				}
			})
		}
	}
}

// TestSignOutEverywhere_CrossOriginNeverReachesTheRevocation: the CSRF defence is the writing
// chain's sameOriginGate, ahead of the resolver -- a POST from another origin (or with none)
// revokes nothing, writes nothing and keeps the cookie.
func TestSignOutEverywhere_CrossOriginNeverReachesTheRevocation(t *testing.T) {
	for _, origin := range []string{"https://evil.example", "null", ""} {
		admins := &fakeAdmins{}
		trail := &fakeTrail{}
		b := signOutBrowser(t, true, admins, trail, "owner")
		b.origin = origin
		rec := b.do(http.MethodPost, accountSignOutEverywhereHref, nil)
		if n := len(admins.revokeAllCalls()); n != 0 || len(trail.eventsSnapshot()) != 0 || clearsTheCookie(rec) {
			t.Errorf("origin %q: %d RevokeAll call(s), %d row(s), cookie cleared %v (answer %d)",
				origin, n, len(trail.eventsSnapshot()), clearsTheCookie(rec), rec.Code)
		}
	}
}

// TestSignOutEverywhere_AFailedRevocationKeepsTheCookie: RevokeAll fails -> 500 problem page, the
// cookie is KEPT (clearing it would leave live sessions behind a browser that thinks it signed out
// -- Logout's rule), no row.
func TestSignOutEverywhere_AFailedRevocationKeepsTheCookie(t *testing.T) {
	admins := &fakeAdmins{revokeAll: func(uuid.UUID, uuid.UUID, uuid.UUID, bool) (adminauth.Revocation, error) {
		return adminauth.Revocation{}, errors.New("database down")
	}}
	trail := &fakeTrail{}
	b := signOutBrowser(t, true, admins, trail, "owner")
	rec := b.do(http.MethodPost, accountSignOutEverywhereHref, nil)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), problemSignOutEverywhereUnavailable.Title) {
		t.Fatalf("answered %d, want the 500 problem page", rec.Code)
	}
	if clearsTheCookie(rec) || len(trail.eventsSnapshot()) != 0 {
		t.Errorf("a failed revocation cleared the cookie (%v) or wrote %d row(s)", clearsTheCookie(rec), len(trail.eventsSnapshot()))
	}
}
