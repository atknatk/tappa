package handler

// accountlinkbusinesses_db_test.go -- M14-22 end to end: the real panel (flag on), the real
// adminauth.Manager over the real pool, identity_request_verification ('link') and
// identity_link_by_email (00084), the real trail and a fake mail relay. The user's smoke, in a test:
// the banner -> "Add it" -> the e-mail -> its link -> the press -> "Switch business" lists every
// business and switches in one click; an old tab's form is 409; "Sign out everywhere" reaches every
// business.

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
)

// linkBusinessesLinkRe finds the link by e-mail's link in an e-mail's text part.
var linkBusinessesLinkRe = regexp.MustCompile(`https?://\S+/admin/link-businesses\?t=[A-Za-z0-9_-]+`)

// TestLinkBusinessesDB_TheBannerToSwitchBusinessEndToEnd is criteria 1-7 over HTTP: a person signed in
// to KF (linked) whose address also runs KM and KX with their OWN passwords (unlinked) sees the banner,
// which names neither; "Add it" sends ONE e-mail to the identity's address and says "Check your inbox";
// another browser opening the link adds nothing; the person's browser opens it (nothing spent), presses,
// and both businesses join the sign-in -- the notice e-mail counts two and names none; the banner is
// gone; "Switch business" lists all three and switches in one click; the KF tab's old form is 409 after
// the switch; KM's old password no longer signs in; "Sign out everywhere" signs out every business.
func TestLinkBusinessesDB_TheBannerToSwitchBusinessEndToEnd(t *testing.T) {
	sender := &fakeMailSender{}
	const kfName = "KF Link E2E Ltd"
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: kfName,
		invitations: func(t *testing.T, cfg *config.Config) *EmailInvitations {
			transport, err := NewEmailInvitations(sender, cfg.BaseURL)
			if err != nil {
				t.Fatalf("NewEmailInvitations: %v", err)
			}
			return transport
		}})
	owner := linkHarness(t, p)
	forgetIdentityRows(t, owner, p.email)
	ctx := context.Background()
	km := addBusiness(t, p, owner, uuid.Nil, "KM Link E2E Ltd")
	kx := addBusiness(t, p, owner, uuid.Nil, "KX Link E2E Ltd")
	for _, b := range []everywhereBusiness{km, kx} {
		// Their own password (M14-06's backfill left such rows unlinked).
		if _, err := owner.Exec(ctx, `UPDATE admin_users SET password_hash = $2 WHERE id = $1`, b.admin, victimDigest); err != nil {
			t.Fatal(err)
		}
	}
	// A KM session opened with KM's own password (the flag-off-shaped sign-in: today's picker).
	kmTab := p.anotherBrowser(t)
	kmTab.password = victimPassword
	kmTab.signInTo(t, km.tenant)

	p.signInTo(t, p.tenantID)
	_, page := p.get(t, "/admin")
	if !strings.Contains(page, "Another business uses your email.") || !strings.Contains(page, "Add it to this sign-in.") {
		t.Fatal("no banner on the shell")
	}
	for _, name := range []string{km.name, kx.name, "2 businesses", "two businesses"} {
		if strings.Contains(page, name) {
			t.Errorf("the banner page names %q before the mailbox proof", name)
		}
	}

	res, _ := p.post(t, accountLinkBusinessesHref, url.Values{})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != accountLinkBusinessesHref+"?done=sent" {
		t.Fatalf("Add it: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	if _, page := p.get(t, res.Header.Get("Location")); !strings.Contains(page, "Check your inbox") || strings.Contains(page, km.name) || strings.Contains(page, kx.name) {
		t.Fatal("the page after Add it does not say \"Check your inbox\", or names a business")
	}
	if sender.count() != 1 || sender.sent[0].To != p.email {
		t.Fatalf("mail: %d sent, want one to the identity's address", sender.count())
	}
	letter := sender.sent[0].Text
	for _, name := range []string{km.name, kx.name, kfName} {
		if strings.Contains(letter, name) {
			t.Errorf("the link e-mail names %q", name)
		}
	}
	link := linkBusinessesLinkRe.FindString(letter)
	if link == "" {
		t.Fatal("no link in the e-mail")
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}

	// Another browser (no session): opening and pressing adds nothing.
	q := p.anotherBrowser(t)
	if res, _ := q.get(t, u.RequestURI()); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != accountLinkBusinessesHref {
		t.Fatalf("another browser's GET: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	q.post(t, accountLinkBusinessesConfirmHref, url.Values{})
	linkedTo := func(b everywhereBusiness) *uuid.UUID {
		var id *uuid.UUID
		if err := owner.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, b.admin).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	if linkedTo(km) != nil || linkedTo(kx) != nil {
		t.Fatal("a browser without the person's session added a business")
	}

	// The person's browser: the GET spends nothing, the press adds both.
	if res, _ := p.get(t, u.RequestURI()); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the person's GET: %d", res.StatusCode)
	}
	if linkedTo(km) != nil {
		t.Fatal("the link's GET added a business (R19)")
	}
	// A KF tab kept open: its password form's bound action, as the page drew it.
	staleAction := boundPath(p.server.Config.Handler, p.panelCookie(t), accountPasswordHref)
	res, _ = p.post(t, accountLinkBusinessesConfirmHref, url.Values{})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != accountLinkBusinessesHref+"?done=added" {
		t.Fatalf("the press: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	var ident uuid.UUID
	if err := owner.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, p.adminID).Scan(&ident); err != nil {
		t.Fatal(err)
	}
	for _, b := range []everywhereBusiness{km, kx} {
		if id := linkedTo(b); id == nil || *id != ident {
			t.Fatalf("%s is not on the person's sign-in", b.name)
		}
	}
	if sender.count() != 2 || sender.sent[1].To != p.email || !strings.Contains(sender.sent[1].Text, "2 businesses were just added") {
		t.Fatalf("the notice: %d mails; want a second one to the address counting two", sender.count())
	}
	for _, name := range []string{km.name, kx.name, kfName} {
		if strings.Contains(sender.sent[1].Text, name) || strings.Contains(sender.sent[1].HTML, name) {
			t.Errorf("the notice names %q", name)
		}
	}
	if kmTab.signedIn(t) {
		t.Error("KM's old-password session survived the link")
	}
	if _, page := p.get(t, "/admin"); strings.Contains(page, "Another business uses your email.") {
		t.Error("the banner is still drawn with nothing left to add")
	}

	// "Switch business" lists all three; one click switches.
	if _, page := p.get(t, adminSwitchHref); !strings.Contains(page, "Continue to "+km.name) || !strings.Contains(page, "Continue to "+kx.name) {
		t.Fatal("the switch page does not list the added businesses")
	}
	if res := switchTo(t, p, km.tenant); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the switch to KM: %d", res.StatusCode)
	}
	if _, page := p.get(t, "/admin"); !strings.Contains(page, km.name) {
		t.Error("after one click the shell is not KM's")
	}
	// The KF tab's form after the switch: 409, nothing written.
	if res, _ := p.post(t, staleAction, url.Values{"current_password": {"x"}, "new_password": {"y"}, "confirm_password": {"y"}}); res.StatusCode != http.StatusConflict {
		t.Errorf("the old KF tab's form after the switch: %d, want 409", res.StatusCode)
	}
	// KM's old password signs in nowhere now.
	stranger := p.anotherBrowser(t)
	_, lp := stranger.get(t, adminLoginPath)
	if res, _ := stranger.post(t, adminLoginPath, url.Values{"csrf": {csrfFrom(t, lp)}, "email": {p.email}, "password": {victimPassword}}); res.StatusCode == http.StatusSeeOther {
		t.Errorf("KM's old password still signs in (%q)", res.Header.Get("Location"))
	}
	// "Sign out everywhere" now reaches every business of the sign-in.
	other := p.anotherBrowser(t)
	other.signInTo(t, p.tenantID)
	if res, _ := p.post(t, accountSignOutEverywhereHref, url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign out everywhere: %d", res.StatusCode)
	}
	for _, b := range []everywhereBusiness{km, kx, {tenant: p.tenantID, admin: p.adminID, name: kfName}} {
		if n := p.liveSessionsOf(t, b.tenant, b.admin); n != 0 {
			t.Errorf("%s: %d live session(s) after sign out everywhere", b.name, n)
		}
	}
}
