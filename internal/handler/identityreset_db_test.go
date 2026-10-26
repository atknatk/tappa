package handler

// identityreset_db_test.go -- M14-14 over HTTP against real Postgres, TAPPA_ADMIN_IDENTITY on: the
// harness's row is linked to an identity (linkHarness, under the harness's shared operator-tables lock
// -- holdPanelTablesLock, this package's re-entrant holder) whose REGISTERED address is a different
// spelling of the row's.

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// identityResetHarness is a flag-on harness whose row is linked; it returns the owner connection, the
// identity and its registered address (the row's, upper-cased). The identity's links and events are
// taken back before linkHarness takes the identity back (cleanups run last-in first-out).
func identityResetHarness(t *testing.T) (*panelHarness, *pgx.Conn, uuid.UUID, string) {
	t.Helper()
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true})
	owner := linkHarness(t, p)
	ctx := context.Background()
	var ident uuid.UUID
	if err := owner.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, p.adminID).Scan(&ident); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM admin_identity_resets WHERE identity_id = $1`, `DELETE FROM admin_identity_events WHERE identity_id = $1`} {
			if _, err := owner.Exec(context.Background(), sql, ident); err != nil {
				t.Errorf("take the identity's rows back (%s): %v", sql, err)
			}
		}
	})
	registered := strings.ToUpper(p.email)
	if _, err := owner.Exec(ctx, `UPDATE admin_identities SET email = $2 WHERE id = $1`, ident, registered); err != nil {
		t.Fatal(err)
	}
	return p, owner, ident, registered
}

// TestPanelIdentityReset_EndToEnd walks the per-identity recovery with the flag on: the request
// delivers ONE "every business" link to the identity's REGISTERED address (not the typed spelling,
// not the row's) and writes no audit row; spending it changes the identity's and the row's digest,
// revokes the session, leaves the business ONE admin.recovery.completed row (the definer's), and the
// person gets ONE notice -- every business -- at the registered address, with no notice audit row.
//
// ⚠️ It pays one shipped-cost bcrypt (the Consume), like TestPanelRecoveryDB_EndToEnd.
func TestPanelIdentityReset_EndToEnd(t *testing.T) {
	p, owner, ident, registered := identityResetHarness(t)
	ctx := context.Background()
	p.signIn(t)

	_, page := p.get(t, adminResetPath)
	typed := strings.ToUpper(p.email[:1]) + p.email[1:]
	if res, _ := p.post(t, adminResetPath, url.Values{"csrf": {csrfFrom(t, page)}, "email": {typed}}); res.StatusCode != http.StatusOK {
		t.Fatalf("the request answered %d", res.StatusCode)
	}
	p.drainReset(t)
	got := p.mail.all()
	if len(got) != 1 {
		t.Fatalf("%d deliveries, want the identity's one (the linked row gets no per-row link)", len(got))
	}
	if got[0].Recipient != registered || !got[0].EveryBusiness {
		t.Fatalf("delivered to %q (every business %v); want admin_identities.email as stored, %q", got[0].Recipient, got[0].EveryBusiness, registered)
	}
	if n := p.auditCount(t, ActionAdminResetRequested); n != 0 {
		t.Errorf("%d requested rows for a per-identity link, want 0", n)
	}
	before := p.storedDigest(t)

	link := got[0].Link
	const next = "a-new-identity-password"
	if res, _ := p.get(t, strings.TrimPrefix(link, p.server.URL)); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("opening the link answered %d", res.StatusCode)
	}
	_, form := p.get(t, adminResetNewPath)
	res, _ := p.post(t, adminResetNewPath, url.Values{"csrf": {csrfFrom(t, form)}, "password": {next}, "password_confirm": {next}})
	if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), adminLoginPath) {
		t.Fatalf("the spend answered %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	if after := p.storedDigest(t); after == before {
		t.Error("the row's digest did not change")
	}
	var same bool
	if err := owner.QueryRow(ctx, `SELECT a.password_hash = i.password_hash FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id WHERE a.id = $1 AND i.id = $2`, p.adminID, ident).Scan(&same); err != nil || !same {
		t.Errorf("the row and the identity differ after the reset (%v)", err)
	}
	if n := p.liveSessionCount(t); n != 0 {
		t.Errorf("%d live sessions survived", n)
	}
	if n := p.auditCount(t, ActionAdminResetCompleted); n != 1 {
		t.Errorf("%d recovery.completed rows, want the definer's 1", n)
	}
	notices := p.mail.allNotices()
	if len(notices) != 1 || notices[0].Recipient != registered || !notices[0].EveryBusiness || notices[0].IdentityID != ident {
		t.Fatalf("notices = %+v; want ONE, every business, to %q", notices, registered)
	}
	if n := p.auditCount(t, ActionAdminPasswordNoticeSent) + p.auditCount(t, ActionAdminPasswordNoticeUndelivered); n != 0 {
		t.Errorf("%d notice rows in the business, want 0 (the person's notice belongs to no business)", n)
	}
}

// TestPanelIdentityReset_TimingIsFlatOverHTTP measures the flag-on request on real Postgres, three
// arms interleaved: an address with an identity that ISSUES a link (its live link withdrawn by the
// owner before each sample -- the heaviest path: resolver, identity_open_reset, the row resolver, a
// queued send), the same address while its link is live (kept), and an address nobody has. Every
// answer pays resetRequestFloor and the medians are within 60 ms of each other (the same band as
// TestAdminReset_TimingIsFlat). The structural claim -- every exit below the budget pays the floor --
// is that test's; this one is the honest measurement of the identity path's work under it.
func TestPanelIdentityReset_TimingIsFlatOverHTTP(t *testing.T) {
	p, owner, ident, _ := identityResetHarness(t)
	ctx := context.Background()
	const samples = 6
	one := func(email string) time.Duration {
		t.Helper()
		_, page := p.get(t, adminResetPath)
		form := url.Values{"csrf": {csrfFrom(t, page)}, "email": {email}}
		start := time.Now()
		res, _ := p.post(t, adminResetPath, form)
		d := time.Since(start)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("a request answered %d (the request budget is %d per address)", res.StatusCode, adminResetRequestLimit)
		}
		return d
	}
	var issued, kept, unknown []time.Duration
	for i := 0; i < samples; i++ {
		if _, err := owner.Exec(ctx, `UPDATE admin_identity_resets SET cancelled_at = now() WHERE identity_id = $1 AND used_at IS NULL AND cancelled_at IS NULL`, ident); err != nil {
			t.Fatal(err)
		}
		issued = append(issued, one(p.email))
		kept = append(kept, one(p.email))
		unknown = append(unknown, one("nobody-"+uuid.NewString()+"@identity.example"))
	}
	if n := len(p.mail.all()); n < samples-1 {
		t.Fatalf("CONTROL: %d links delivered over %d issuing samples -- the issuing arm did not issue", n, samples)
	}
	med := func(d []time.Duration) time.Duration {
		s := append([]time.Duration(nil), d...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return s[len(s)/2]
	}
	for _, d := range append(append(append([]time.Duration{}, issued...), kept...), unknown...) {
		if d < resetRequestFloor {
			t.Fatalf("an answer took %v, under the floor of %v", d, resetRequestFloor)
		}
	}
	mi, mk, mu := med(issued), med(kept), med(unknown)
	all3 := []time.Duration{mi, mk, mu}
	spread := slices.Max(all3) - slices.Min(all3)
	t.Logf("medians: issued %v, kept %v, unknown %v (spread %v, floor %v)", mi, mk, mu, spread, resetRequestFloor)
	if spread > 60*time.Millisecond {
		t.Errorf("medians issued %v / kept %v / unknown %v differ by %v: the identity path's work leaks through the clock", mi, mk, mu, spread)
	}
}
