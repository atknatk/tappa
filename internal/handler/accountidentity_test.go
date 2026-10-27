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
		{http.MethodPost, accountLinkHref},
	}
}

// TestAccountIdentity_FlagOffIsAPathThePanelNeverHad (ADR 0031 §8): with TAPPA_ADMIN_IDENTITY off each
// of the seven routes answers every method with the router's own 404 -- a path that never existed --
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
	for _, s := range []string{"Your businesses", "Link a business", "Link to your Taptime sign-in", "Keep all", "verify-email", "confirm/"} {
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

// TestAccountIdentity_TheActBudgetRefusesTheEleventhPress: adminIdentityActLimit presses of "Link a
// business" reach LinkBusiness; the next is 429 and reaches nothing. The same bucket covers unlink
// and Undo (a press of either after the link presses is refused too).
func TestAccountIdentity_TheActBudgetRefusesTheEleventhPress(t *testing.T) {
	admins := &fakeAdmins{}
	b, _ := identityPanel(t, true, true, admins, nil)
	links := func() int {
		n := 0
		for _, c := range admins.identCalls() {
			if c == "link" || c == "unlink" {
				n++
			}
		}
		return n
	}
	form := url.Values{"current_password": {"a"}, "other_password": {"b"}}
	for i := 0; i < adminIdentityActLimit; i++ {
		b.do(http.MethodPost, accountLinkHref, form)
	}
	if links() != adminIdentityActLimit {
		t.Fatalf("%d presses reached LinkBusiness, want %d", links(), adminIdentityActLimit)
	}
	if res := b.do(http.MethodPost, accountLinkHref, form); res.Code != http.StatusTooManyRequests || links() != adminIdentityActLimit {
		t.Fatalf("press %d: %d, %d calls; want 429 and none", adminIdentityActLimit+1, res.Code, links())
	}
	if res := b.do(http.MethodPost, accountUnlinkHref, url.Values{"tenant_id": {uuid.NewString()}, "password": {"a"}}); res.Code != http.StatusTooManyRequests || links() != adminIdentityActLimit {
		t.Fatalf("an unlink past the budget: %d, %d calls", res.Code, links())
	}
}

// TestAccountIdentity_LinkABusinessHasOneAnswer (ADR 0031 §5.7 (2)): "linked one" and "linked none"
// are the same redirect; only the person's own wrong password is said (on the page, 200, nothing in
// the address bar). Each fresh session adminauth hands back gets one ordinary sign-out row.
func TestAccountIdentity_LinkABusinessHasOneAnswer(t *testing.T) {
	fresh := adminauth.Session{ID: uuid.New(), TenantID: panelTestTenant, AdminUserID: panelTestAdmin}
	var answers []string
	for _, linked := range []int{0, 1} {
		admins := &fakeAdmins{}
		admins.ident.link = func(adminauth.Resolved, string, string) (adminauth.LinkResult, error) {
			return adminauth.LinkResult{Linked: linked, FreshAct: adminauth.FreshAct{Temporary: []adminauth.Session{fresh}, Revoked: []bool{true}}}, nil
		}
		b, trail := identityPanel(t, true, true, admins, nil)
		res := b.do(http.MethodPost, accountLinkHref, url.Values{"current_password": {"a"}, "other_password": {"b"}})
		answers = append(answers, res.Header().Get("Location")+"|"+res.Body.String())
		outs := 0
		for _, e := range trail.eventsSnapshot() {
			if e.Action == ActionAdminLoggedOut && e.TenantID == fresh.TenantID {
				outs++
			}
		}
		if outs != 1 {
			t.Errorf("linked %d: %d sign-out rows for the fresh session, want 1", linked, outs)
		}
	}
	if answers[0] != answers[1] || !strings.Contains(answers[0], "done=linked") {
		t.Fatalf("the two answers differ or are not the one redirect: %q vs %q", answers[0], answers[1])
	}
	admins := &fakeAdmins{}
	admins.ident.link = func(adminauth.Resolved, string, string) (adminauth.LinkResult, error) {
		return adminauth.LinkResult{}, adminauth.ErrBadCredentials
	}
	b, _ := identityPanel(t, true, true, admins, nil)
	res := b.do(http.MethodPost, accountLinkHref, url.Values{"current_password": {"a"}, "other_password": {"b"}})
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Your password for this sign-in is not right.") {
		t.Fatalf("a wrong own password: %d", res.Code)
	}
}

// TestAccountIdentity_AnUnlinkedSessionIsOfferedTheReverseLink is the card's last criterion's handler
// half: an UNLINKED (older per-business) session's account page offers "Link to your Taptime sign-in"
// with this business's password and the sign-in's -- no verification, no window -- and the press
// reaches LinkBusiness with the session's own unlinked Resolved.
func TestAccountIdentity_AnUnlinkedSessionIsOfferedTheReverseLink(t *testing.T) {
	admins := &fakeAdmins{}
	var got adminauth.Resolved
	admins.ident.link = func(s adminauth.Resolved, _, _ string) (adminauth.LinkResult, error) {
		got = s
		return adminauth.LinkResult{Linked: 1}, nil
	}
	b, _ := identityPanel(t, true, false, admins, nil)
	page := html.UnescapeString(b.do(http.MethodGet, accountHref, nil).Body.String())
	for _, want := range []string{"Link to your Taptime sign-in", "This business's password", "Your Taptime sign-in's password", "Your businesses"} {
		if !strings.Contains(page, want) {
			t.Errorf("the unlinked page lacks %q", want)
		}
	}
	for _, not := range []string{"Send me a confirmation link", "Keep all"} {
		if strings.Contains(page, not) {
			t.Errorf("the unlinked page offers %q", not)
		}
	}
	for _, c := range admins.identCalls() {
		if c == "window" {
			t.Error("an unlinked session's page read a window")
		}
	}
	if res := b.do(http.MethodPost, accountLinkHref, url.Values{"current_password": {"a"}, "other_password": {"b"}}); !strings.Contains(res.Header().Get("Location"), "done=linked") {
		t.Fatalf("the reverse link: %q", res.Header().Get("Location"))
	}
	if got.Linked || got.AdminUserID != panelTestAdmin || got.TenantID != panelTestTenant {
		t.Errorf("LinkBusiness was handed %+v, want this unlinked session", got)
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
