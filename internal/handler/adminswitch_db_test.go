package handler

// adminswitch_db_test.go -- M14-10 "switch businesses" end to end: the real panel (flag on), the
// real adminauth.Manager over the real pool, identity_switch_session (00071) and the real audit
// trail. A person is ONE identity linked to admin rows in several test-made businesses; the
// identity is committed, so each test holds internal/db's operator-tables lock SHARED through its
// panel harness (holdPanelTablesLock, the re-entrant holder) and takes the identity back at the end
// (linkHarness).

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
)

// switchPerson is a linked person with a business A (the harness's) and B, signed in to A.
type switchPerson struct {
	p     *panelHarness
	owner *pgx.Conn
	ident uuid.UUID
	a, b  everywhereBusiness
}

func newSwitchPerson(t *testing.T, label string) *switchPerson {
	t.Helper()
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: label + " A Ltd"})
	owner := linkHarness(t, p)
	var ident uuid.UUID
	if err := owner.QueryRow(context.Background(), `SELECT identity_id FROM admin_users WHERE id = $1`, p.adminID).Scan(&ident); err != nil {
		t.Fatalf("the identity: %v", err)
	}
	s := &switchPerson{p: p, owner: owner, ident: ident,
		a: everywhereBusiness{tenant: p.tenantID, admin: p.adminID, name: label + " A Ltd"}}
	s.b = addBusiness(t, p, owner, ident, label+" B Ltd")
	landOn(t, owner, ident, s.a.tenant)
	p.signInTo(t, s.a.tenant)
	return s
}

// switchTo loads the switch page in br and presses the form for target, as a browser does.
func switchTo(t *testing.T, br *panelHarness, target uuid.UUID) *http.Response {
	t.Helper()
	res, page := br.get(t, adminSwitchHref)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", adminSwitchHref, res.StatusCode)
	}
	out, _ := br.post(t, adminSwitchHref, url.Values{"csrf": {csrfFrom(t, page)}, "tenant_id": {target.String()}})
	return out
}

// rowCount is how many audit rows of the two actions tenant holds.
func (s *switchPerson) rowCount(t *testing.T, tenant uuid.UUID) int {
	t.Helper()
	return len(tenantRows(t, s.owner, tenant, ActionAdminLoggedOut)) + len(tenantRows(t, s.owner, tenant, ActionAdminLoginSucceeded))
}

// TestAdminSwitchDB_MovesTheSessionAndLeavesOrdinaryRows is the card end to end, flag on. A
// person signed in to A presses "Continue to B":
//
//   - 303 into the panel, a new cookie; the panel now answers as B; A holds no live session and B
//     one; the OLD cookie resolves to nothing (the old session is revoked);
//   - A gets exactly one ordinary sign-out row and B one ordinary sign-in row (actor/target their
//     own membership, detail {outcome, session_id}), neither naming the other business, the other
//     membership or the identity; the handler wrote nothing besides;
//   - the A page's switch form replayed after the switch is refused before anything is written
//     (its binding and its synchronizer belong to the old session);
//   - a target whose membership is disabled is the ONE refusal, 403, the session untouched.
func TestAdminSwitchDB_MovesTheSessionAndLeavesOrdinaryRows(t *testing.T) {
	s := newSwitchPerson(t, "Switch")
	p := s.p
	c := addBusiness(t, p, s.owner, s.ident, "Switch C Ltd")
	if _, err := s.owner.Exec(context.Background(), `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, c.admin); err != nil {
		t.Fatalf("disable the membership in C: %v", err)
	}

	// The page lists B (identity_memberships through the real Manager), not the disabled C, and
	// names A as the current business without a form for it.
	if _, page := p.get(t, adminSwitchHref); !strings.Contains(page, "Continue to Switch B Ltd") ||
		strings.Contains(page, "Switch C Ltd") || strings.Contains(page, "Continue to Switch A Ltd") || !strings.Contains(page, "You are in Switch A Ltd") {
		t.Fatalf("the switch page does not list exactly B as a target: %s", page)
	}

	// The refusal first: C's membership is disabled.
	before := p.panelCookie(t)
	if res := switchTo(t, p, c.tenant); res.StatusCode != http.StatusForbidden || len(res.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("a disabled membership: %d, Set-Cookie %v; want 403 and none", res.StatusCode, res.Header.Values("Set-Cookie"))
	}
	if p.panelCookie(t) != before || !p.signedIn(t) {
		t.Fatal("a refused switch changed or ended the session")
	}

	// The A page, kept open: its form will be replayed after the switch.
	_, staleA := p.get(t, adminSwitchHref)
	staleCSRF := csrfFrom(t, staleA)
	staleAction := boundPath(p.server.Config.Handler, before, adminSwitchHref)
	aRows, bRows := s.rowCount(t, s.a.tenant), s.rowCount(t, s.b.tenant)
	aOut := len(tenantRows(t, s.owner, s.a.tenant, ActionAdminLoggedOut))
	bIn := len(tenantRows(t, s.owner, s.b.tenant, ActionAdminLoginSucceeded))

	res := switchTo(t, p, s.b.tenant)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != transactionsHref {
		t.Fatalf("the switch answered %d %q, want 303 to %s", res.StatusCode, res.Header.Get("Location"), transactionsHref)
	}
	after := p.panelCookie(t)
	if after == "" || after == before {
		t.Fatal("the switch did not set a new cookie")
	}
	if !p.signedIn(t) {
		t.Fatal("the switched session does not open the panel")
	}
	if a, b := p.liveSessionsOf(t, s.a.tenant, s.a.admin), p.liveSessionsOf(t, s.b.tenant, s.b.admin); a != 0 || b != 1 {
		t.Fatalf("live sessions after the switch: A %d, B %d; want 0 and 1", a, b)
	}
	old := p.anotherBrowser(t)
	old.setCookie(t, adminauth.CookieName, before)
	if old.signedIn(t) {
		t.Fatal("the old cookie still opens the panel")
	}

	foreignOf := func(other everywhereBusiness) []string {
		return []string{other.tenant.String(), other.admin.String(), other.name, s.ident.String()}
	}
	checkOrdinaryLogouts(t, "A sign-out", tenantRows(t, s.owner, s.a.tenant, ActionAdminLoggedOut)[aOut:], 1, s.a,
		p.sessionsOf(t, s.a.tenant, s.a.admin), foreignOf(s.b))
	checkOrdinaryLogouts(t, "B sign-in", tenantRows(t, s.owner, s.b.tenant, ActionAdminLoginSucceeded)[bIn:], 1, s.b,
		p.sessionsOf(t, s.b.tenant, s.b.admin), foreignOf(s.a))
	if da, db := s.rowCount(t, s.a.tenant)-aRows, s.rowCount(t, s.b.tenant)-bRows; da != 1 || db != 1 {
		t.Errorf("new sign-in/out rows: A %d, B %d; want one each", da, db)
	}

	// The A page's form, replayed from the same browser now in B: refused, nothing written.
	req, err := http.NewRequest(http.MethodPost, p.server.URL+staleAction,
		strings.NewReader(url.Values{"csrf": {staleCSRF}, "tenant_id": {s.a.tenant.String()}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", p.server.URL)
	stale, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = readAll(t, stale)
	if stale.StatusCode != http.StatusConflict {
		t.Errorf("the stale A form: %d, want 409", stale.StatusCode)
	}
	if p.panelCookie(t) != after || p.liveSessionsOf(t, s.b.tenant, s.b.admin) != 1 || s.rowCount(t, s.a.tenant)-aRows != 1 {
		t.Error("the stale A form moved the session or wrote a row")
	}
}

// TestAdminSwitchDB_TheBudgetIsThePersonsAcrossAlternatingSwitches (bulgu 8): A -> B -> A -> ...
// from one address. Every switch is a NEW session, yet switch adminSwitchLimit+1 is a 429 -- and
// from there the audit trail of both businesses stops growing (two rows per switch until the
// limit, none after) and the session stays where it was.
//
// MUTATION (measured): adminSwitchKey keyed by the session id (id.Admin.SessionID) instead of the
// person's bucket -> every hop starts a fresh count and switch #31 goes through, red.
func TestAdminSwitchDB_TheBudgetIsThePersonsAcrossAlternatingSwitches(t *testing.T) {
	s := newSwitchPerson(t, "Budget")
	p := s.p
	rows0 := s.rowCount(t, s.a.tenant) + s.rowCount(t, s.b.tenant)
	targets := []uuid.UUID{s.b.tenant, s.a.tenant}
	for i := 1; i <= adminSwitchLimit; i++ {
		if res := switchTo(t, p, targets[(i-1)%2]); res.StatusCode != http.StatusSeeOther {
			t.Fatalf("switch #%d: %d, want 303", i, res.StatusCode)
		}
	}
	rows := s.rowCount(t, s.a.tenant) + s.rowCount(t, s.b.tenant)
	if rows-rows0 != 2*adminSwitchLimit {
		t.Fatalf("PREMISE: %d switches wrote %d rows, want %d", adminSwitchLimit, rows-rows0, 2*adminSwitchLimit)
	}
	cookie := p.panelCookie(t)
	res := switchTo(t, p, targets[adminSwitchLimit%2])
	if res.StatusCode != http.StatusTooManyRequests || len(res.Header.Values("Set-Cookie")) != 0 {
		t.Fatalf("switch #%d: %d, Set-Cookie %v; want 429 and none", adminSwitchLimit+1, res.StatusCode, res.Header.Values("Set-Cookie"))
	}
	if got := s.rowCount(t, s.a.tenant) + s.rowCount(t, s.b.tenant); got != rows {
		t.Errorf("the refused switch wrote %d audit row(s)", got-rows)
	}
	if p.panelCookie(t) != cookie || !p.signedIn(t) {
		t.Error("the refused switch moved or ended the session")
	}
}

// TestAdminSwitchDB_ConcurrentSwitchesHaveOneWinner (bulgu 29): N presses of the same form, same
// cookie, in parallel. Exactly one is a 303 with the new session's cookie; every other answer is
// the one refusal (403) with NO Set-Cookie -- neither a cookie of its own nor a deletion of the
// winner's. B holds one live session, A none, and one sign-out / sign-in pair was written.
func TestAdminSwitchDB_ConcurrentSwitchesHaveOneWinner(t *testing.T) {
	s := newSwitchPerson(t, "Race")
	p := s.p
	_, page := p.get(t, adminSwitchHref)
	csrf := csrfFrom(t, page)
	cookie := p.panelCookie(t)
	action := boundPath(p.server.Config.Handler, cookie, adminSwitchHref)
	body := url.Values{"csrf": {csrf}, "tenant_id": {s.b.tenant.String()}}.Encode()
	rows0 := s.rowCount(t, s.a.tenant) + s.rowCount(t, s.b.tenant)

	const n = 8
	type answer struct {
		status   int
		location string
		cookies  []string
	}
	answers := make([]answer, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, p.server.URL+action, strings.NewReader(body))
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", p.server.URL)
			req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: cookie})
			<-start
			res, err := (&http.Client{CheckRedirect: p.client.CheckRedirect}).Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_ = readAll(t, res)
			answers[i] = answer{res.StatusCode, res.Header.Get("Location"), res.Header.Values("Set-Cookie")}
		}()
	}
	close(start)
	wg.Wait()

	winners := 0
	for i, a := range answers {
		switch {
		case a.status == http.StatusSeeOther && a.location == transactionsHref:
			winners++
			if len(a.cookies) != 1 || !strings.HasPrefix(a.cookies[0], adminauth.CookieName+"=") {
				t.Errorf("the winner's Set-Cookie is %v", a.cookies)
			}
		case a.status == http.StatusForbidden,
			// A loser whose resolver already saw the old session revoked: the gate's redirect to
			// the sign-in.
			a.status == http.StatusSeeOther && a.location == adminLoginPath:
			if len(a.cookies) != 0 {
				t.Errorf("loser %d (%d %q) wrote Set-Cookie %v", i, a.status, a.location, a.cookies)
			}
		default:
			t.Errorf("answer %d: %d %q", i, a.status, a.location)
		}
	}
	if winners != 1 {
		t.Fatalf("%d winners, want exactly 1: %+v", winners, answers)
	}
	if a, b := p.liveSessionsOf(t, s.a.tenant, s.a.admin), p.liveSessionsOf(t, s.b.tenant, s.b.admin); a != 0 || b != 1 {
		t.Errorf("live sessions after the race: A %d, B %d; want 0 and 1", a, b)
	}
	if got := s.rowCount(t, s.a.tenant) + s.rowCount(t, s.b.tenant) - rows0; got != 2 {
		t.Errorf("the race wrote %d sign-in/out rows, want 2", got)
	}
}

// TestAdminSwitchDB_IntoASuspendedBusinessIsReadOnlyThere (ADR 0031 §5.4, ADR 0025's M14 note,
// K-10): B is suspended. The switch into B succeeds (303, no tenant.write_refused row -- the route
// is on protectWritingThroughSuspension); the next write in B meets the suspension gate (403 and
// B's refusal row); and switching back OUT of the suspended B succeeds too.
//
// MUTATION (measured): the suspension ring put in front of the switch route -> the switch into B is
// the suspension's 403, red (and TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException).
func TestAdminSwitchDB_IntoASuspendedBusinessIsReadOnlyThere(t *testing.T) {
	s := newSwitchPerson(t, "Suspended")
	p := s.p
	suspendAsOwner(t, ownerPoolForTest(t), s.b.tenant, db.SuspensionNonPayment)
	refused0 := len(tenantRows(t, s.owner, s.b.tenant, ActionTenantWriteRefused))

	if res := switchTo(t, p, s.b.tenant); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the switch into the suspended B: %d, want 303", res.StatusCode)
	}
	if n := len(tenantRows(t, s.owner, s.b.tenant, ActionTenantWriteRefused)) - refused0; n != 0 {
		t.Fatalf("the switch into B wrote %d refusal row(s)", n)
	}
	if p.liveSessionsOf(t, s.b.tenant, s.b.admin) != 1 {
		t.Fatal("no session in B after the switch")
	}
	// A write in B: the account save (owner, ProtectWriting's main group).
	res, _ := p.post(t, accountHref, url.Values{})
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a write in the suspended B: %d, want the suspension's 403", res.StatusCode)
	}
	if n := len(tenantRows(t, s.owner, s.b.tenant, ActionTenantWriteRefused)) - refused0; n != 1 {
		t.Errorf("the write in B left %d refusal row(s), want 1", n)
	}
	if res := switchTo(t, p, s.a.tenant); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the switch out of the suspended B: %d, want 303", res.StatusCode)
	}
	if p.liveSessionsOf(t, s.a.tenant, s.a.admin) != 1 || p.liveSessionsOf(t, s.b.tenant, s.b.admin) != 0 {
		t.Error("the switch out of B did not move the session to A")
	}
}
