package handler

// accountidentity_db_test.go -- M14-18 end to end: the real panel (flag on), the real
// adminauth.Manager over the real pool, the identity functions (00072, 00079) and the real trail.
// The identity is committed (linkHarness or identityOf below) and taken back at the end; the panel
// harness holds internal/db's operator-tables lock shared.

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
)

// verifyLinkRe finds the verification link in an e-mail's text part.
var verifyLinkRe = regexp.MustCompile(`https?://\S+/admin/verify-email\?t=[A-Za-z0-9_-]+`)

// forgetIdentityRows takes back what the acts of a test wrote under the identity behind admin --
// its verification links and events (by the address, whatever is linked by then) -- before linkHarness takes the identity itself (t.Cleanup is
// last-in, first-out).
func forgetIdentityRows(t *testing.T, owner *pgx.Conn, email string) {
	t.Helper()
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM admin_identity_verifications WHERE identity_id IN (SELECT id FROM admin_identities WHERE email = $1)`,
			`DELETE FROM admin_identity_events WHERE identity_id IN (SELECT id FROM admin_identities WHERE email = $1)`,
		} {
			if _, err := owner.Exec(context.Background(), sql, email); err != nil {
				t.Errorf("cleanup %s: %v", sql, err)
			}
		}
	})
}

// TestAccountIdentityDB_VerifyOnlyFromTheSameIdentitysBrowser is R2 / R19 / S16 over HTTP: a linked
// person asks for a link -- ONE e-mail, to the identity's registered address, under this business's
// scope, its link at /admin/verify-email; ANOTHER browser opening it (no session there) verifies
// nothing -- the address stays unverified, the link unspent; the person's own browser opening it
// still verifies nothing until the press, and the press verifies and opens the first_verify window,
// which the account page then shows.
func TestAccountIdentityDB_VerifyOnlyFromTheSameIdentitysBrowser(t *testing.T) {
	sender := &fakeMailSender{}
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: "Verify E2E Ltd",
		invitations: func(t *testing.T, cfg *config.Config) *EmailInvitations {
			transport, err := NewEmailInvitations(sender, cfg.BaseURL)
			if err != nil {
				t.Fatalf("NewEmailInvitations: %v", err)
			}
			return transport
		}})
	owner := linkHarness(t, p)
	forgetIdentityRows(t, owner, p.email)
	p.signInTo(t, p.tenantID)
	unverified := func() bool {
		var n int
		if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM admin_identities i JOIN admin_users a ON a.identity_id = i.id
		                                                  WHERE a.id = $1 AND i.email_verified_at IS NULL`, p.adminID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	if !unverified() {
		t.Fatal("PREMISE: the identity starts verified")
	}

	res, _ := p.post(t, accountVerifyHref, url.Values{})
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "done=verify-sent") {
		t.Fatalf("the request: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	if sender.count() != 1 || sender.sent[0].To != p.email || sender.sentScopes[0] != p.tenantID.String() {
		t.Fatalf("mail: %d sent (to the address: %v, scope %v)", sender.count(), sender.count() == 1 && sender.sent[0].To == p.email, sender.sentScopes)
	}
	link := verifyLinkRe.FindString(sender.sent[0].Text)
	if link == "" {
		t.Fatal("no verification link in the e-mail")
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	path := u.RequestURI()

	// Another browser: the link's GET stores it, its press has no session to verify with.
	q := p.anotherBrowser(t)
	if res, _ := q.get(t, path); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != accountHref {
		t.Fatalf("another browser's GET: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	if res, _ := q.post(t, accountVerifyConfirmHref, url.Values{}); res.StatusCode == http.StatusSeeOther && strings.Contains(res.Header.Get("Location"), "done=verified") {
		t.Fatal("another browser verified the address")
	}
	if !unverified() {
		t.Fatal("another browser's click verified the address")
	}

	// The person's browser: the GET spends nothing, the press does.
	if res, _ := p.get(t, path); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the person's GET: %d", res.StatusCode)
	}
	if !unverified() {
		t.Fatal("the link's GET verified the address (R19)")
	}
	_, page := p.get(t, accountHref)
	if !strings.Contains(page, "Confirm my email address") {
		t.Fatal("the account page does not offer the press")
	}
	res, _ = p.post(t, accountVerifyConfirmHref, url.Values{})
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "done=verified") {
		t.Fatalf("the press: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	if unverified() {
		t.Fatal("the press did not verify the address")
	}
	if _, page := p.get(t, accountHref); !strings.Contains(page, "Check the businesses on your sign-in") || !strings.Contains(page, "Verify E2E Ltd") {
		t.Fatal("the first_verify window is not drawn after the first verification")
	}
}

// TestAccountIdentityDB_AnUnlinkedSessionLinksItselfFromTheAccountPage is the card's last criterion
// end to end: the person's older per-business sign-in -- UNLINKED, its own password -- signs in with
// the flag on (the identity's password differs, so the legacy arm signs it in; M14-13's Choose
// paragraph would send them to the account page), and the account page's "Link to your Taptime
// sign-in" with both passwords links THIS row to the identity: the same browser stays signed in, its
// session now resolves linked ("Your businesses" lists the other business too), no session opened
// for the proof is left live.
func TestAccountIdentityDB_AnUnlinkedSessionLinksItselfFromTheAccountPage(t *testing.T) {
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: "Reverse E2E A Ltd"})
	owner := linkHarness(t, p)
	forgetIdentityRows(t, owner, p.email)
	ctx := context.Background()
	var ident uuid.UUID
	if err := owner.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, p.adminID).Scan(&ident); err != nil {
		t.Fatal(err)
	}
	// The identity's password becomes another one (B carries it), and A -- this browser's row -- is an
	// older, unlinked sign-in of the same address with the harness's own password.
	b := addBusiness(t, p, owner, uuid.Nil, "Reverse E2E B Ltd")
	for _, sql := range []string{
		`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE id = $1`,
		`UPDATE admin_identities SET password_hash = $2 WHERE id = $1`,
	} {
		args := []any{p.adminID}
		if strings.Contains(sql, "$2") {
			args = []any{ident, victimDigest}
		}
		if _, err := owner.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	// B carries the identity's digest, then is linked (the trigger refuses a digest write to a linked row).
	if _, err := owner.Exec(ctx, `UPDATE admin_users SET password_hash = $2 WHERE id = $1`, b.admin, victimDigest); err != nil {
		t.Fatalf("B's digest copy: %v", err)
	}
	if _, err := owner.Exec(ctx, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() WHERE id = $1`, b.admin, ident); err != nil {
		t.Fatalf("link B: %v", err)
	}
	t.Cleanup(func() {
		// A is linked by the test: linkHarness's cleanup unlinks every row of the identity.
		if _, err := owner.Exec(context.Background(), `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE id = $1`, b.admin); err != nil {
			t.Errorf("cleanup B: %v", err)
		}
	})
	p.signInTo(t, p.tenantID)
	_, page := p.get(t, accountHref)
	if !strings.Contains(page, "Link to your Taptime sign-in") {
		t.Fatal("the unlinked session is not offered the reverse link")
	}

	res, _ := p.post(t, accountLinkHref, url.Values{"current_password": {p.password}, "other_password": {victimPassword}})
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "done=linked") {
		t.Fatalf("the reverse link: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	var linked *uuid.UUID
	if err := owner.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, p.adminID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked == nil || *linked != ident {
		t.Fatalf("A's row is linked to %v, want the identity", linked)
	}
	if !p.signedIn(t) {
		t.Fatal("the browser was signed out by its own link")
	}
	if _, page := p.get(t, accountHref); !strings.Contains(page, "Reverse E2E B Ltd") || strings.Contains(page, "Link to your Taptime sign-in") {
		t.Fatal("after the link the page does not list B as one of the person's businesses on a linked sign-in")
	}
	if n := p.liveSessionsOf(t, b.tenant, b.admin); n != 0 {
		t.Errorf("%d live sessions in B (the fresh identity session must be revoked)", n)
	}
	if n := p.liveSessionsOf(t, p.tenantID, p.adminID); n != 1 {
		t.Errorf("%d live sessions in A, want the browser's one", n)
	}
}

// TestAccountIdentityDB_AnUnlinkedRowCannotSpendThePersonsBudget is the security audit's YÜKSEK, end to
// end (ADR 0031 §5.8, S3/T1's pre-seeded attacker): the attacker's UNLINKED row Z carries the person's
// address and its own password. From Z's browser: eleven junk unlinks, eleven junk links and six
// verification asks. The person's own unlink press and verification ask are then NOT refused by a
// budget (no 429); the attacker's own link presses are (its row and the reverse key). MUTATION:
// AccountBucket giving an unlinked row the bucket its address names -> the person's unlink is 429, red.
func TestAccountIdentityDB_AnUnlinkedRowCannotSpendThePersonsBudget(t *testing.T) {
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: "Budget E2E A Ltd"})
	owner := linkHarness(t, p)
	forgetIdentityRows(t, owner, p.email)
	ctx := context.Background()
	var ident uuid.UUID
	if err := owner.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, p.adminID).Scan(&ident); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `UPDATE admin_identities SET email_verified_at = now(), confirm_opened_at = now(),
	                                 confirm_open_until = now() + interval '7 days', confirm_scope = 'reset' WHERE id = $1`, ident); err != nil {
		t.Fatal(err)
	}
	// Z: the attacker's business, a row of the person's address with the attacker's password.
	z := addBusiness(t, p, owner, uuid.Nil, "Budget E2E Z Ltd")
	if _, err := owner.Exec(ctx, `UPDATE admin_users SET password_hash = $2 WHERE id = $1`, z.admin, victimDigest); err != nil {
		t.Fatal(err)
	}
	attacker := p.anotherBrowser(t)
	attacker.password = victimPassword
	attacker.signInTo(t, z.tenant)
	p.signInTo(t, p.tenantID)

	junk := url.Values{"tenant_id": {p.tenantID.String()}, "password": {"junk"}}
	linked := 0
	for i := 0; i < adminIdentityActLimit+1; i++ {
		attacker.post(t, accountUnlinkHref, junk)
		res, _ := attacker.post(t, accountLinkHref, url.Values{"current_password": {victimPassword}, "other_password": {"junk"}})
		if res.StatusCode == http.StatusTooManyRequests {
			linked++
		}
	}
	for i := 0; i < adminVerifyRequestLimit+1; i++ {
		attacker.post(t, accountVerifyHref, url.Values{})
	}
	if linked == 0 {
		t.Error("CONTROL: the attacker's own link presses were never limited")
	}
	res, _ := p.post(t, accountUnlinkHref, url.Values{"tenant_id": {z.tenant.String()}, "password": {"not-the-password"}})
	if res.StatusCode == http.StatusTooManyRequests {
		t.Fatal("the person's unlink was refused by a budget an unlinked row spent")
	}
	res, _ = p.post(t, accountVerifyHref, url.Values{})
	if res.StatusCode == http.StatusTooManyRequests {
		t.Fatal("the person's verification ask was refused by a budget an unlinked row spent")
	}
}
