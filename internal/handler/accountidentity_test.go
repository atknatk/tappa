package handler

import (
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
)

// accountidentity_test.go -- M14-18's handler half against fakes: the flag gate, the link's GET that
// spends nothing, the confirmation POST, the two per-person budgets at limit + 1, Keep all's one
// click, the window's refusals, and "Link a business" from a linked and from an unlinked session.
// The database half is accountidentity_db_test.go and internal/adminauth/identityaccount_db_test.go.

// identityPanel is the real panel over fakes, signed in as an owner whose session resolves linked or
// not, with the identity flag as given and an e-mail transport over sender (nil: no mail).
func identityPanel(t *testing.T, flag, linked bool, admins *fakeAdmins, sender *fakeMailSender) (*browser, *fakeTrail) {
	t.Helper()
	if admins.verify == nil {
		admins.verify = func() (adminauth.Resolved, error) {
			return adminauth.Resolved{
				SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
				Role: "owner", FullName: "KF Owner", Linked: linked, TenantName: "KF Ltd",
			}, nil
		}
	}
	cfg := adminTestConfig()
	cfg.AdminIdentity = flag
	var opts []AdminAuthOption
	if sender != nil {
		cfg.InviteDelivery = config.InviteDeliveryEmail
		transport, err := NewEmailInvitations(sender, cfg.BaseURL)
		if err != nil {
			t.Fatalf("NewEmailInvitations: %v", err)
		}
		opts = append(opts, InvitationsByEmail(transport))
	}
	trail := &fakeTrail{}
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, slog.New(slog.DiscardHandler), opts...)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = panelCookie().Value
	return b, trail
}

// accountIdentityRoutes are M14-18's routes, by method.
func accountIdentityRoutes() [][2]string {
	return [][2]string{
		{http.MethodGet, verifyLinkPath},
		{http.MethodPost, accountVerifyHref},
		{http.MethodPost, accountVerifyConfirmHref},
		{http.MethodPost, accountKeepAllHref},
		{http.MethodPost, accountUnlinkHref},
		{http.MethodPost, accountUndoHref},
		// M14-22: the link by e-mail (M14-18's two-password route is gone).
		{http.MethodGet, linkLandingPath},
		{http.MethodGet, accountLinkBusinessesHref},
		{http.MethodPost, accountLinkBusinessesHref},
		{http.MethodPost, accountLinkBusinessesConfirmHref},
	}
}

// TestAccountIdentity_FlagOffIsAPathThePanelNeverHad (ADR 0031 §8): with TAPPA_ADMIN_IDENTITY off each
// of the ten routes answers every method with the router's own 404 -- a path that never existed --
// and the account page draws none of the sections; with it on each is mounted (not 404 / 405).
// MUTATION (measured): one route mounted outside mountGated -> red.
func TestAccountIdentity_FlagOffIsAPathThePanelNeverHad(t *testing.T) {
	b, _ := identityPanel(t, false, true, &fakeAdmins{}, nil)
	never := b.do(http.MethodGet, "/admin/never-had-this", nil)
	for _, rt := range accountIdentityRoutes() {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			got := b.do(method, rt[1], url.Values{})
			if got.Code != http.StatusNotFound || got.Body.String() != never.Body.String() {
				t.Errorf("flag off, %s %s: %d %q, want the never-had 404", method, rt[1], got.Code, got.Body.String())
			}
		}
	}
	page := b.do(http.MethodGet, accountHref, nil).Body.String()
	for _, s := range []string{"Your businesses", "Link a business", "Link to your Taptime sign-in", "Keep all", "verify-email", "confirm/", "link-businesses", "Another business uses your email."} {
		if strings.Contains(page, s) {
			t.Errorf("flag off, the account page carries %q", s)
		}
	}
	on, _ := identityPanel(t, true, true, &fakeAdmins{}, nil)
	for _, rt := range accountIdentityRoutes() {
		if got := on.do(rt[0], rt[1], url.Values{}); got.Code == http.StatusNotFound || got.Code == http.StatusMethodNotAllowed {
			t.Errorf("flag on, %s %s: %d, want it mounted", rt[0], rt[1], got.Code)
		}
	}
}

// TestAccountIdentity_TheLinksGETSpendsNothing (R19): the e-mailed link's GET verifies nothing -- no
// call to VerifyEmail -- moves the value into a short-lived cookie under the panel path and sends
// the browser to the account page, which then offers the confirmation press; the press spends THAT
// value with THIS browser's session and clears the cookie, whatever the answer.
func TestAccountIdentity_TheLinksGETSpendsNothing(t *testing.T) {
	admins := &fakeAdmins{}
	verified := false
	admins.ident.verify = func(raw string) (bool, error) { verified = true; return true, nil }
	b, _ := identityPanel(t, true, true, admins, nil)
	const value = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
	got := b.do(http.MethodGet, verifyLinkPath+"?t="+value, nil)
	if got.Code != http.StatusSeeOther || got.Header().Get("Location") != accountHref {
		t.Fatalf("the link's GET: %d %q, want 303 to the account page", got.Code, got.Header().Get("Location"))
	}
	if got.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("the link's GET does not send no-referrer")
	}
	for _, c := range admins.identCalls() {
		if c == "verify-email" {
			t.Fatal("the link's GET reached VerifyEmail")
		}
	}
	if b.cookies[accountVerifyCookieName] != value {
		t.Fatalf("the cookie holds %q, want the link's value", b.cookies[accountVerifyCookieName])
	}
	if page := b.do(http.MethodGet, accountHref, nil).Body.String(); !strings.Contains(page, "Confirm my email address") {
		t.Fatal("the account page does not offer the confirmation press for the link this browser opened")
	}
	res := b.do(http.MethodPost, accountVerifyConfirmHref, url.Values{})
	if !verified || res.Code != http.StatusSeeOther || !strings.Contains(res.Header().Get("Location"), "done=verified") {
		t.Fatalf("the press: verified %v, %d %q", verified, res.Code, res.Header().Get("Location"))
	}
	if admins.ident.verified[0] != value {
		t.Errorf("VerifyEmail was handed %q, want the cookie's value", admins.ident.verified[0])
	}
	if _, ok := b.cookies[accountVerifyCookieName]; ok {
		t.Error("the cookie survived the press")
	}
	// A refused press (another browser's link, a spent one) says so and verifies nothing.
	admins.ident.verify = nil
	b.do(http.MethodGet, verifyLinkPath+"?t="+value, nil)
	if res := b.do(http.MethodPost, accountVerifyConfirmHref, url.Values{}); !strings.Contains(res.Header().Get("Location"), "problem=verify-refused") {
		t.Errorf("a refused press: %q, want problem=verify-refused", res.Header().Get("Location"))
	}
	// A value no cookie can carry is refused at the GET, nothing stored.
	delete(b.cookies, accountVerifyCookieName)
	if res := b.do(http.MethodGet, verifyLinkPath+"?t=a;b", nil); !strings.Contains(res.Header().Get("Location"), "problem=verify-refused") || b.cookies[accountVerifyCookieName] != "" {
		t.Errorf("an unsafe value: %q, cookie %q", res.Header().Get("Location"), b.cookies[accountVerifyCookieName])
	}
}

// TestAccountIdentity_TheVerificationBudgetRefusesTheSixthAsk is M14-18's inherited obligation 1 at
// limit + 1: adminVerifyRequestLimit asks reach RequestVerification; the next one is 429 and mints
// nothing (the call count stays). The bucket is the PERSON's (Resolved.SwitchBucket), not the
// session's. MUTATION (measured): the Charge removed -> the sixth reaches RequestVerification, red.
func TestAccountIdentity_TheVerificationBudgetRefusesTheSixthAsk(t *testing.T) {
	admins := &fakeAdmins{}
	admins.ident.request = func(adminauth.Resolved) (adminauth.IssuedVerification, error) {
		return adminauth.IssuedVerification{}, adminauth.ErrAlreadyVerified
	}
	b, _ := identityPanel(t, true, true, admins, &fakeMailSender{})
	asks := func() int {
		n := 0
		for _, c := range admins.identCalls() {
			if c == "request-verification" {
				n++
			}
		}
		return n
	}
	for i := 0; i < adminVerifyRequestLimit; i++ {
		if res := b.do(http.MethodPost, accountVerifyHref, url.Values{}); res.Code != http.StatusSeeOther {
			t.Fatalf("ask %d: %d", i+1, res.Code)
		}
	}
	if asks() != adminVerifyRequestLimit {
		t.Fatalf("%d asks reached RequestVerification, want %d", asks(), adminVerifyRequestLimit)
	}
	res := b.do(http.MethodPost, accountVerifyHref, url.Values{})
	if res.Code != http.StatusTooManyRequests || asks() != adminVerifyRequestLimit {
		t.Fatalf("ask %d: %d with %d calls; want 429 and no further call", adminVerifyRequestLimit+1, res.Code, asks())
	}
	// Another person's bucket is untouched.
	admins.ident.bucket = func(adminauth.Resolved) (string, error) { return "someone-else", nil }
	if res := b.do(http.MethodPost, accountVerifyHref, url.Values{}); res.Code != http.StatusSeeOther || asks() != adminVerifyRequestLimit+1 {
		t.Fatalf("another person's first ask: %d, %d calls", res.Code, asks())
	}
}

// TestAccountIdentity_TheActBudgetRefusesTheEleventhPress: adminIdentityActLimit unlink presses reach
// Unlink; the next is 429 and reaches nothing, and so is an Undo -- one bucket for the window's acts.
// (M14-18 counted "Link a business" presses here; M14-22 removed that form.)
func TestAccountIdentity_TheActBudgetRefusesTheEleventhPress(t *testing.T) {
	admins := &fakeAdmins{}
	b, _ := identityPanel(t, true, true, admins, nil)
	acts := func() int {
		n := 0
		for _, c := range admins.identCalls() {
			if c == "unlink" || c == "relink" {
				n++
			}
		}
		return n
	}
	form := url.Values{"tenant_id": {uuid.NewString()}, "password": {"a"}}
	for i := 0; i < adminIdentityActLimit; i++ {
		b.do(http.MethodPost, accountUnlinkHref, form)
	}
	if acts() != adminIdentityActLimit {
		t.Fatalf("%d presses reached Unlink, want %d", acts(), adminIdentityActLimit)
	}
	if res := b.do(http.MethodPost, accountUnlinkHref, form); res.Code != http.StatusTooManyRequests || acts() != adminIdentityActLimit {
		t.Fatalf("press %d: %d, %d calls; want 429 and none", adminIdentityActLimit+1, res.Code, acts())
	}
	if res := b.do(http.MethodPost, accountUndoHref, form); res.Code != http.StatusTooManyRequests || acts() != adminIdentityActLimit {
		t.Fatalf("an Undo past the budget: %d, %d calls", res.Code, acts())
	}
}

// TestAccountIdentity_AnUnlinkedSessionIsPointedToItsSignIn: an UNLINKED (older per-business) session's
// account page lists its business and says how to reach the others -- the banner of the Taptime
// sign-in (M14-22) -- with no verification, no window and no two-password form (M14-18's reverse link
// is gone).
func TestAccountIdentity_AnUnlinkedSessionIsPointedToItsSignIn(t *testing.T) {
	admins := &fakeAdmins{}
	b, _ := identityPanel(t, true, false, admins, nil)
	page := html.UnescapeString(b.do(http.MethodGet, accountHref, nil).Body.String())
	for _, want := range []string{"Your businesses", "sign in with your Taptime sign-in"} {
		if !strings.Contains(page, want) {
			t.Errorf("the unlinked page lacks %q", want)
		}
	}
	for _, not := range []string{"Send me a confirmation link", "Keep all", "Link to your Taptime sign-in", "Link a business", "other_password", "Another business uses your email."} {
		if strings.Contains(page, not) {
			t.Errorf("the unlinked page offers %q", not)
		}
	}
	for _, c := range admins.identCalls() {
		if c == "window" || c == "has-business-to-add" {
			t.Errorf("an unlinked session's page asked %q", c)
		}
	}
}

// TestAccountIdentity_TheWindow: an open window draws the list first -- Keep all (no password field
// in its form), a password field per row, the owner warning, the Undo for a refused row; Keep all is
// one click; an unlink with a wrong password says so beside its row; an unlink of THIS business ends
// the cookie and lands on the sign-in.
func TestAccountIdentity_TheWindow(t *testing.T) {
	other, here := uuid.New(), panelTestTenant
	admins := &fakeAdmins{}
	admins.ident.window = func() (adminauth.Window, error) {
		return adminauth.Window{Linked: true, Verified: true, Open: true, Scope: "reset", Rows: []adminauth.WindowRow{
			{TenantID: other, TenantName: "Other Ltd", Role: "owner", LastActiveOwner: true},
			{TenantID: uuid.New(), TenantName: "Refused Ltd", Role: "manager", Refused: true},
		}}, nil
	}
	kept := false
	admins.ident.keepAll = func() error { kept = true; return nil }
	admins.ident.unlink = func(_ adminauth.Resolved, password string, target uuid.UUID) (adminauth.FreshAct, bool, error) {
		if password != "right" {
			return adminauth.FreshAct{}, false, adminauth.ErrBadCredentials
		}
		return adminauth.FreshAct{}, target == here, nil
	}
	b, _ := identityPanel(t, true, true, admins, nil)
	page := b.do(http.MethodGet, accountHref, nil).Body.String()
	for _, want := range []string{"Check the businesses on your sign-in", "Keep all", "Other Ltd", "You are its only owner", "Put it back", "Remove from my sign-in"} {
		if !strings.Contains(page, want) {
			t.Errorf("the window lacks %q", want)
		}
	}
	if i, j := strings.Index(page, "Check the businesses on your sign-in"), strings.Index(page, "Identity"); i < 0 || j < 0 || i > j {
		t.Error("the open window is not the first card")
	}
	if res := b.do(http.MethodPost, accountKeepAllHref, url.Values{}); !kept || !strings.Contains(res.Header().Get("Location"), "done=kept") {
		t.Fatalf("Keep all: kept %v, %q", kept, res.Header().Get("Location"))
	}
	res := b.do(http.MethodPost, accountUnlinkHref, url.Values{"tenant_id": {other.String()}, "password": {"wrong"}})
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Your password is not right.") {
		t.Fatalf("a wrong password: %d", res.Code)
	}
	if res := b.do(http.MethodPost, accountUnlinkHref, url.Values{"tenant_id": {other.String()}, "password": {"right"}}); !strings.Contains(res.Header().Get("Location"), "done=unlinked") {
		t.Fatalf("an unlink: %q", res.Header().Get("Location"))
	}
	res = b.do(http.MethodPost, accountUnlinkHref, url.Values{"tenant_id": {here.String()}, "password": {"right"}})
	if res.Header().Get("Location") != adminLoginPath {
		t.Fatalf("an unlink of this business: %q, want the sign-in", res.Header().Get("Location"))
	}
	if _, ok := b.cookies[adminauth.CookieName]; ok {
		t.Error("the cookie survived the unlink of its own business")
	}
}

// TestAccountIdentity_AFreshSessionThatCouldNotBeRevokedIsRecordedAbandoned (D-sec2): when adminauth
// reports a fresh session's revocation failed, the business gets the sign-in's abandoned-session row
// (admin.login.failed, outcome session_abandoned, the session's id) -- never an ordinary sign-out that
// would claim it ended. A revoked one gets the ordinary sign-out row.
func TestAccountIdentity_AFreshSessionThatCouldNotBeRevokedIsRecordedAbandoned(t *testing.T) {
	stuck := adminauth.Session{ID: uuid.New(), TenantID: panelTestTenant, AdminUserID: panelTestAdmin}
	ended := adminauth.Session{ID: uuid.New(), TenantID: panelTestTenant, AdminUserID: panelTestAdmin}
	admins := &fakeAdmins{}
	admins.ident.unlink = func(adminauth.Resolved, string, uuid.UUID) (adminauth.FreshAct, bool, error) {
		return adminauth.FreshAct{Temporary: []adminauth.Session{stuck, ended}, Revoked: []bool{false, true}}, false, nil
	}
	b, trail := identityPanel(t, true, true, admins, nil)
	b.do(http.MethodPost, accountUnlinkHref, url.Values{"tenant_id": {uuid.NewString()}, "password": {"pw"}})
	var abandoned, outs int
	for _, e := range trail.eventsSnapshot() {
		switch e.Action {
		case ActionAdminLoginFailed:
			if d, ok := e.Detail.(adminLoginDetail); ok && d.Outcome == "session_abandoned" && d.SessionID == stuck.ID.String() {
				abandoned++
			}
		case ActionAdminLoggedOut:
			outs++
		}
	}
	if abandoned != 1 || outs != 1 {
		t.Fatalf("%d abandoned rows for the stuck session and %d sign-out rows; want 1 and 1 (the revoked one)", abandoned, outs)
	}
}
