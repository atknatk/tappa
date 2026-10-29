package handler

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/adminauth"
)

// accountlinkbusinesses_test.go -- M14-22's handler half against fakes: the banner (who sees it, what it
// says and does not say), the request's budget, the link's GET that spends nothing, the press and its
// three answers. The database half is accountlinkbusinesses_db_test.go and
// internal/adminauth/identitylinkemail_db_test.go.

// countCalls counts the fake's calls named name.
func countCalls(admins *fakeAdmins, name string) int {
	n := 0
	for _, c := range admins.identCalls() {
		if c == name {
			n++
		}
	}
	return n
}

// TestLinkBusinesses_TheBannerNamesNothingAndOnlyALinkedSessionSeesIt (criterion 1): with another
// business to add, a LINKED session sees the banner on every panel section -- "Another business uses
// your email. Add it to this sign-in." and one bound "Add it" form posting to the request -- and the
// banner names no business and no number. With nothing to add, on an UNLINKED session (adminauth is not
// even asked) and with the flag off, there is no banner. MUTATION: the Linked test removed from
// addBusinessBanner -> the unlinked session is asked and shown it, red.
func TestLinkBusinesses_TheBannerNamesNothingAndOnlyALinkedSessionSeesIt(t *testing.T) {
	const words = "Another business uses your email."
	yes := func(adminauth.Resolved) (bool, error) { return true, nil }
	admins := &fakeAdmins{}
	admins.ident.toAdd = yes
	b, _ := identityPanel(t, true, true, admins, nil)
	for _, path := range []string{"/admin", accountHref, adminSwitchHref + "-never"} {
		res := b.do(http.MethodGet, path, nil)
		if res.Code != http.StatusOK {
			continue // the third is a 404: no shell
		}
		page := html.UnescapeString(res.Body.String())
		if !strings.Contains(page, words) || !strings.Contains(page, "Add it to this sign-in.") || !strings.Contains(page, ">Add it</button>") {
			t.Errorf("%s: no banner for a linked session with a business to add", path)
		}
		// (The binding itself is FormBinding's inventory: this fake harness renders unbound forms.)
		if !strings.Contains(page, `<form method="post" action="`+accountLinkBusinessesHref) {
			t.Errorf("%s: the banner's form is not the request", path)
		}
		for _, leak := range []string{"KF Ltd 2", "businesses use"} {
			if strings.Contains(page, leak) {
				t.Errorf("%s: the banner says %q", path, leak)
			}
		}
	}

	none := &fakeAdmins{}
	none.ident.toAdd = func(adminauth.Resolved) (bool, error) { return false, nil }
	nb, _ := identityPanel(t, true, true, none, nil)
	if strings.Contains(html.UnescapeString(nb.do(http.MethodGet, "/admin", nil).Body.String()), words) {
		t.Error("a banner with nothing to add")
	}

	unlinked := &fakeAdmins{}
	unlinked.ident.toAdd = yes
	ub, _ := identityPanel(t, true, false, unlinked, nil)
	if strings.Contains(html.UnescapeString(ub.do(http.MethodGet, "/admin", nil).Body.String()), words) {
		t.Error("an unlinked session sees the banner")
	}
	if n := countCalls(unlinked, "has-business-to-add"); n != 0 {
		t.Errorf("an unlinked session's page asked adminauth %d times", n)
	}

	off := &fakeAdmins{}
	off.ident.toAdd = yes
	ob, _ := identityPanel(t, false, true, off, nil)
	if strings.Contains(html.UnescapeString(ob.do(http.MethodGet, "/admin", nil).Body.String()), words) || countCalls(off, "has-business-to-add") != 0 {
		t.Error("the flag is off, yet the banner is drawn or asked for")
	}
}

// TestLinkBusinesses_TheRequestsBudgetRefusesTheSixthAsk (criterion 2): adminVerifyRequestLimit "Add it"
// presses reach RequestLink; the next is 429 and reaches nothing. The budget is the link's own (the
// verification's asks are counted apart), charged BEFORE the request; an unlinked session is refused
// before any charge. MUTATION: the TryCharge removed -> the sixth reaches RequestLink, red.
func TestLinkBusinesses_TheRequestsBudgetRefusesTheSixthAsk(t *testing.T) {
	admins := &fakeAdmins{}
	admins.ident.requestLink = func(adminauth.Resolved) (adminauth.IssuedLink, error) {
		return adminauth.IssuedLink{}, adminauth.ErrNothingToAdd
	}
	b, _ := identityPanel(t, true, true, admins, &fakeMailSender{})
	for i := 0; i < adminVerifyRequestLimit; i++ {
		if res := b.do(http.MethodPost, accountLinkBusinessesHref, url.Values{}); !strings.Contains(res.Header().Get("Location"), "done=none") {
			t.Fatalf("ask %d: %d %q", i+1, res.Code, res.Header().Get("Location"))
		}
	}
	res := b.do(http.MethodPost, accountLinkBusinessesHref, url.Values{})
	if res.Code != http.StatusTooManyRequests || countCalls(admins, "request-link") != adminVerifyRequestLimit {
		t.Fatalf("ask %d: %d with %d calls; want 429 and no further call", adminVerifyRequestLimit+1, res.Code, countCalls(admins, "request-link"))
	}
	if !strings.Contains(html.UnescapeString(res.Body.String()), "You asked for several links in the last hour.") {
		t.Error("the 429 page does not say why")
	}
	// The verification's budget is another count.
	if res := b.do(http.MethodPost, accountVerifyHref, url.Values{}); res.Code == http.StatusTooManyRequests {
		t.Error("the link's asks spent the verification's budget")
	}

	ua := &fakeAdmins{}
	ub, _ := identityPanel(t, true, false, ua, &fakeMailSender{})
	if res := ub.do(http.MethodPost, accountLinkBusinessesHref, url.Values{}); !strings.Contains(res.Header().Get("Location"), "problem=unavailable") || countCalls(ua, "request-link") != 0 {
		t.Errorf("an unlinked session's ask: %q, %d calls; want unavailable and none", res.Header().Get("Location"), countCalls(ua, "request-link"))
	}
}

// TestLinkBusinesses_TheLinksGETSpendsNothingAndThePressDoes (criterion 3, R19): the e-mailed link's GET
// links nothing -- no call to LinkByEmail -- moves the value into a short-lived cookie under the panel
// path and sends the browser to the page, which offers the one press; the press spends THAT value with
// THIS browser's session, clears the cookie, and answers "added" (some linked), "none" (nothing left) or
// "refused". The page the request lands on says "Check your inbox" and names nothing.
func TestLinkBusinesses_TheLinksGETSpendsNothingAndThePressDoes(t *testing.T) {
	admins := &fakeAdmins{}
	admins.ident.toAdd = func(adminauth.Resolved) (bool, error) { return true, nil }
	admins.ident.linkByEmail = func(string) (adminauth.LinkedBusinesses, error) {
		return adminauth.LinkedBusinesses{Linked: 2}, nil
	}
	b, _ := identityPanel(t, true, true, admins, nil)
	if page := html.UnescapeString(b.do(http.MethodGet, accountLinkBusinessesHref+"?done=sent", nil).Body.String()); !strings.Contains(page, "Check your inbox") {
		t.Error(`the page after the request does not say "Check your inbox"`)
	}
	const value = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
	got := b.do(http.MethodGet, linkLandingPath+"?t="+value, nil)
	if got.Code != http.StatusSeeOther || got.Header().Get("Location") != accountLinkBusinessesHref || got.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("the link's GET: %d %q", got.Code, got.Header().Get("Location"))
	}
	if countCalls(admins, "link-by-email") != 0 {
		t.Fatal("the link's GET reached LinkByEmail")
	}
	if b.cookies[accountLinkCookieName] != value {
		t.Fatalf("the cookie holds %q, want the link's value", b.cookies[accountLinkCookieName])
	}
	page := html.UnescapeString(b.do(http.MethodGet, accountLinkBusinessesHref, nil).Body.String())
	if !strings.Contains(page, "Add your businesses") || !strings.Contains(page, `<form method="post" action="`+accountLinkBusinessesConfirmHref) {
		t.Fatal("the page does not offer the bound press for the link this browser opened")
	}
	if strings.Contains(page, "Another business uses your email.") {
		t.Error("the page draws the banner that leads to itself")
	}
	if countCalls(admins, "link-by-email") != 0 {
		t.Fatal("the page's GET reached LinkByEmail")
	}
	res := b.do(http.MethodPost, accountLinkBusinessesConfirmHref, url.Values{})
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != accountLinkBusinessesHref+"?done=added" {
		t.Fatalf("the press: %d %q", res.Code, res.Header().Get("Location"))
	}
	if admins.ident.verified[0] != value {
		t.Errorf("LinkByEmail was handed %q, want the cookie's value", admins.ident.verified[0])
	}
	if _, ok := b.cookies[accountLinkCookieName]; ok {
		t.Error("the cookie survived the press")
	}
	if page := html.UnescapeString(b.do(http.MethodGet, accountLinkBusinessesHref+"?done=added", nil).Body.String()); !strings.Contains(page, `href="`+adminSwitchHref+`"`) {
		t.Error("the page after the press does not lead to Switch business")
	}
	// Nothing left to add, and a refused press.
	admins.ident.linkByEmail = func(string) (adminauth.LinkedBusinesses, error) { return adminauth.LinkedBusinesses{}, nil }
	b.do(http.MethodGet, linkLandingPath+"?t="+value, nil)
	if res := b.do(http.MethodPost, accountLinkBusinessesConfirmHref, url.Values{}); !strings.Contains(res.Header().Get("Location"), "done=none") {
		t.Errorf("a press with nothing left: %q", res.Header().Get("Location"))
	}
	admins.ident.linkByEmail = nil
	b.do(http.MethodGet, linkLandingPath+"?t="+value, nil)
	if res := b.do(http.MethodPost, accountLinkBusinessesConfirmHref, url.Values{}); !strings.Contains(res.Header().Get("Location"), "problem=refused") {
		t.Errorf("a refused press: %q", res.Header().Get("Location"))
	}
	// A press with no link in this browser reaches nothing.
	before := countCalls(admins, "link-by-email")
	if res := b.do(http.MethodPost, accountLinkBusinessesConfirmHref, url.Values{}); !strings.Contains(res.Header().Get("Location"), "problem=refused") || countCalls(admins, "link-by-email") != before {
		t.Errorf("a press without a link: %q", res.Header().Get("Location"))
	}
	// A value no cookie can carry is refused at the GET, nothing stored.
	if res := b.do(http.MethodGet, linkLandingPath+"?t=a;b", nil); !strings.Contains(res.Header().Get("Location"), "problem=refused") || b.cookies[accountLinkCookieName] != "" {
		t.Errorf("an unsafe value: %q, cookie %q", res.Header().Get("Location"), b.cookies[accountLinkCookieName])
	}
	// A browser holding the link but signed in elsewhere is led back by the banner.
	b.do(http.MethodGet, linkLandingPath+"?t="+value, nil)
	if page := html.UnescapeString(b.do(http.MethodGet, "/admin", nil).Body.String()); !strings.Contains(page, "Finish adding it") {
		t.Error("the banner does not lead a browser holding the link to the press")
	}
}
