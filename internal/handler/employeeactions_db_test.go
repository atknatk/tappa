package handler

// employeeactions_db_test.go -- M6-05 phase B over real HTTP and real Postgres.
//
// WHAT IS MEASURED HERE AND NOWHERE ELSE:
//
//	the CONSEQUENCE of deactivating somebody -- the next tap is refused, RECORDED and
//	alerted on (§5 row 4) -- driven through the panel's own write path rather than by
//	seeding a 'deactivated' row, which is what makes the panel the CAUSE;
//	that the session is NOT revoked, with the positive control beside it;
//	the invitation link, minted by the REAL internal/invite manager and shown once;
//	§4.5 through the whole stack: another business's employee id, posted by a
//	signed-in manager, changes nothing.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
)

// TestEmployeeActionsDB_DeactivationMakesTheNextTapARejectThatIsRECORDED is the
// card's acceptance criterion, and it is deliberately driven END TO END: the tap
// harness taps once successfully, the PANEL's domain deactivates, and the same phone
// taps again.
//
// 🔴 WHY NOT REUSE TestCheckinDB_Row4, WHICH ALREADY TAPS AS A DEACTIVATED PERSON.
// That test SEEDS a 'deactivated' row, so it proves the guardrail reads the column.
// What it cannot prove is that the panel's button is what writes it — the two halves
// could be correct and unconnected, which is exactly the M5-04 shape this repository
// has paid for (a capability delivered, approved and dead in the wired product).
//
// 🔴 AND THE NEGATIVE HALF IS PINNED WITH A POSITIVE CONTROL BESIDE IT. Deactivation
// does NOT revoke sessions (user decision, 2026-08-07), so the assertion "revoked_at
// is still NULL" is a claim about an absence — and a bare absence assertion is the
// shape that quietly empties out. What makes it mean something is the control in the
// same test: the person's phone is still signed in AND their tap is still refused AND
// the refusal is still written. If revocation were added, the reject would come from
// the "revoked session" branch instead, and this test would go red on the first
// assertion rather than silently change which mechanism produced the answer.
func TestEmployeeActionsDB_DeactivationMakesTheNextTapARejectThatIsRECORDED(t *testing.T) {
	h := newTapHarness(t)
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)

	staff, err := tenant.NewStaff(h.data, h.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("tenant.NewStaff: %v", err)
	}

	// CONTROL 1: while they are active, the tap is accepted and recorded.
	before := h.countFor(t, emp)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("the first tap answered %d, want 200", w.Code)
	}
	if got := h.countFor(t, emp); got != before+1 {
		t.Fatalf("the first tap wrote %d record(s), want 1", got-before)
	}
	if rec := h.lastRecord(t, emp); rec.Verdict == "reject" {
		t.Fatalf("an ACTIVE employee's tap was rejected (%v); the measurement below "+
			"would then prove nothing about deactivation", deref(rec.MatchedSid))
	}

	alertsBefore := h.auditCount(t, "tap.security_alert")
	if _, err := staff.Deactivate(context.Background(), tenant.DeactivateCommand{
		TenantID: h.tenantID, EmployeeID: emp, ActorID: uuid.New(),
	}); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	// THE PHONE IS STILL SIGNED IN. This is the decision, measured rather than
	// described: revocation would move the next tap onto a branch whose correctness
	// depends on every caller remembering to write the record.
	if live := liveSessionsFor(t, h, emp); live != 1 {
		t.Fatalf("%d live session(s) after deactivation, want 1 — deactivation must not "+
			"revoke (db/queries/sessions.sql says why)", live)
	}

	// AND THE NEXT TAP IS REFUSED, RECORDED AND RAISED.
	countBefore := h.countFor(t, emp)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+2), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("the second tap answered %d, want 200", w.Code)
	}
	if got := h.countFor(t, emp); got != countBefore+1 {
		t.Fatalf("records went %d -> %d, want +1: §5 row 4 REFUSES and RECORDS",
			countBefore, h.countFor(t, emp))
	}
	rec := h.lastRecord(t, emp)
	if rec.Verdict != "reject" || rec.MatchedSid == nil || *rec.MatchedSid != "sys:employee-deactivated" {
		t.Fatalf("verdict/sid = %q/%v, want reject/sys:employee-deactivated",
			rec.Verdict, deref(rec.MatchedSid))
	}
	if got := h.auditCount(t, "tap.security_alert") - alertsBefore; got != 1 {
		t.Fatalf("security alerts = %d, want 1", got)
	}
	// AND THE PANEL'S OWN TRAIL ENTRY EXISTS, so an investigator can join the refusal
	// to the administrative act that caused it.
	if got := h.auditCount(t, tenant.ActionEmployeeDeactivated); got != 1 {
		t.Fatalf("%d employee.deactivated row(s), want 1", got)
	}
}

func liveSessionsFor(t *testing.T, h *tapHarness, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM sessions
			 WHERE tenant_id = $1 AND employee_id = $2 AND revoked_at IS NULL`,
			h.tenantID, employeeID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

// --- the panel over real HTTP ---------------------------------------------------

// seedPanelPerson gives the panel harness's tenant a venue, a department and one
// employee, and returns their ids. The harness's own fixture has none: it exists to
// sign somebody in.
func seedPanelPerson(t *testing.T, p *panelHarness, tenantID uuid.UUID, name, status string) (venue, department, employee uuid.UUID) {
	t.Helper()
	venue, department, employee = uuid.New(), uuid.New(), uuid.New()
	err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`,
			venue, tenantID, name+" Venue"); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO departments (id, tenant_id, location_id, name) VALUES ($1, $2, $3, $4)`,
			department, tenantID, venue, name+" Kitchen"); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, $4, $5, now())`,
			employee, tenantID, venue, name, status)
		return e
	})
	if err != nil {
		t.Fatalf("seed panel person: %v", err)
	}
	return venue, department, employee
}

func employeeAuditRows(t *testing.T, p *panelHarness, action string, target uuid.UUID) int {
	t.Helper()
	var n int
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			p.tenantID, action, target.String()).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// TestPanelEmployeesDB_TheInviteLinkIsShownOnceAndNeverReadBack is §4.7 on the one
// screen that renders a credential, measured against the REAL invite manager.
//
// FOUR PLACES ARE CHECKED, and the database is the one a fake cannot reach: the
// response body (must have it), the roster afterwards (must not), audit_log's detail
// (must not, and the disclosure row must exist), and employee_invites (must hold a
// HASH and never the code).
func TestPanelEmployeesDB_TheInviteLinkIsShownOnceAndNeverReadBack(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Maria Borg", "invited")
	p.signIn(t)

	res, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("invite answered %d, want 200 with the link rendered", res.StatusCode)
	}
	link := activationLinkIn(t, body)
	code := codeFromLink(t, link)

	// THE INVITATION IS REALLY THERE, and what is stored is the HASH.
	var stored int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM employee_invites
			 WHERE tenant_id = $1 AND employee_id = $2 AND used_at IS NULL`,
			p.tenantID, employee).Scan(&stored)
	}); err != nil {
		t.Fatalf("count invites: %v", err)
	}
	if stored != 1 {
		t.Fatalf("%d pending invitation(s) in the database, want 1", stored)
	}
	assertCodeAbsentFromColumn(t, p, "employee_invites", "code_hash", code)

	// THE DISCLOSURE IS TRAILED AND THE TRAIL DOES NOT CARRY THE CODE.
	if n := employeeAuditRows(t, p, invite.ActionCodeShownToManager, employee); n != 1 {
		t.Fatalf("%d invite.code_shown_to_manager row(s), want 1 — ADR 0005 Y-D's "+
			"detection signal is this row", n)
	}
	assertCodeAbsentFromColumn(t, p, "audit_log", "detail::text", code)

	// NO GET CAN SHOW IT AGAIN. The roster is the only other screen that names this
	// person, and the card is the only place an action could put it.
	_, roster := p.get(t, employeesHref+"?manage="+employee.String())
	if strings.Contains(roster, code) {
		t.Error("the roster renders the activation code; it must exist in exactly one " +
			"response body")
	}

	// A SECOND PRESS MINTS A SECOND INVITATION with a DIFFERENT code, and trails it
	// again. That is what "shown once" costs, measured rather than described.
	res, body2 := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the second invite answered %d", res.StatusCode)
	}
	if second := codeFromLink(t, activationLinkIn(t, body2)); second == code {
		t.Error("the second invitation carries the SAME code; each press must mint a new one")
	}
	if n := employeeAuditRows(t, p, invite.ActionCodeShownToManager, employee); n != 2 {
		t.Errorf("%d disclosure row(s) after two presses, want 2", n)
	}
}

// TestPanelEmployeesDB_ASecondInvitationRETIRESTheFirst is the INVERTED ledger:
// until 2026-08-08 this test asserted the opposite, and the comment it carried said
// it must be inverted rather than deleted when the fix landed. This is that.
//
// 🔴 WHAT IT USED TO MEASURE, end to end over HTTP on both sides:
//
//	two presses of Send invite  -> 2 simultaneously valid invitations
//	activate with the NEWEST    -> the employee is 'active', 1 still pending
//	activate with the OLDEST    -> IT STILL WORKED
//
// The last line was an account takeover that needed no malicious manager: a link
// that "did not arrive" sits in a chat history for its whole TTL, and opening it
// later takes the SECOND-DEVICE path — which revokes the real employee's sessions
// and issues one to whoever opened it.
//
// 🔴 WHAT CLOSED IT (user decision, 2026-08-08). Migration 00012 added
// employee_invites.cancelled_at plus a column-level GRANT, because the measurement
// that produced the decision was that tappa_app could write used_at AND NOTHING ELSE
// (`UPDATE ... SET expires_at` -> permission denied, DELETE -> permission denied).
// invite.Manager.IssueAndDeliver now retires the employee's pending invitations in
// the SAME transaction as the insert, and the three deciding queries eliminate
// used_at and cancelled_at SEPARATELY — the two are different facts and
// db/queries/invites.sql forbids conflating them.
//
// THE POSITIVE CONTROL IS INSIDE: the NEWEST link must still activate. Without it a
// product that had simply broken activation would pass the refusal below.
func TestPanelEmployeesDB_ASecondInvitationRETIRESTheFirst(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Maria Borg", "invited")
	p.signIn(t)

	_, first := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	codeA := codeFromLink(t, activationLinkIn(t, first))
	_, second := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	codeB := codeFromLink(t, activationLinkIn(t, second))
	if codeA == codeB {
		t.Fatal("the two presses produced the same code; the measurement below is void")
	}

	// 🔴 THE FIRST ASSERTION IS THE FIX: pressing again leaves exactly ONE spendable
	// invitation, not two.
	if n := pendingInvitations(t, p, employee); n != 1 {
		t.Fatalf("%d simultaneously spendable invitation(s) after two presses, want 1 — "+
			"issuing must retire the employee's earlier ones", n)
	}
	// AND THE RETIRED ONE IS CANCELLED, NOT CONSUMED. Overloading used_at would
	// corrupt the answer to "when was this code spent?", which is why 00012 exists.
	cancelled, used := invitationStamps(t, p, employee)
	if cancelled != 1 || used != 0 {
		t.Fatalf("after two presses: %d cancelled / %d used, want 1 / 0 — a retired "+
			"invitation must not be recorded as a spent one", cancelled, used)
	}

	// POSITIVE CONTROL: the newest link activates.
	if !activateOverHTTP(t, p, codeB) {
		t.Fatal("the newest link did not activate anybody; the refusal below would then " +
			"be about a broken flow rather than about the stale code")
	}
	if got := employeeStatusOf(t, p, p.tenantID, employee); got != "active" {
		t.Fatalf("status = %q after activation, want active", got)
	}

	// 🔴 AND THE STALE LINK IS REFUSED. This is the takeover path, closed.
	if activateOverHTTP(t, p, codeA) {
		t.Error("the RETIRED invitation still activated. That is the account-takeover " +
			"path this migration exists to close: whoever holds an older link would sign " +
			"in as this person and sign their phone out.")
	}
	// NOTHING WAS CONSUMED BY THE REFUSED ATTEMPT either: a refusal that burned the
	// row would be a different defect wearing the same green.
	cancelled, used = invitationStamps(t, p, employee)
	if cancelled != 1 || used != 1 {
		t.Errorf("after the refused attempt: %d cancelled / %d used, want 1 / 1", cancelled, used)
	}

	// THE SCREEN SAYS WHAT IT NOW DOES. The sentence it used to carry — that earlier
	// links stay usable — is measured false by this very test.
	if !strings.Contains(second, "no longer works") {
		t.Error("the invitation screen does not say that the previous link was retired")
	}
	if strings.Contains(second, "cancel the earlier ones") {
		t.Error("the invitation screen still carries the warning from before the fix; it " +
			"now tells the manager the opposite of what the server does")
	}
}

// invitationStamps counts the two facts that must never be confused: retired without
// being used, and actually spent.
func invitationStamps(t *testing.T, p *panelHarness, employee uuid.UUID) (cancelled, used int) {
	t.Helper()
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT
				count(*) FILTER (WHERE cancelled_at IS NOT NULL),
				count(*) FILTER (WHERE used_at IS NOT NULL)
			FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2`,
			p.tenantID, employee).Scan(&cancelled, &used)
	}); err != nil {
		t.Fatalf("count stamps: %v", err)
	}
	return cancelled, used
}

// TestPanelEmployeesDB_AnExpiredInvitationIsNotRetired is N3: cancellation retires
// what could still be SPENT, and nothing else.
//
// 🔴 IT WAS MEASURED WRONG. The statement's first line promised "every invitation
// that could still be spent" and its WHERE had no expiry predicate, so a single press
// stamped cancelled_at on invitations that had died an hour earlier — and the screen
// counted them, telling the manager "The 2 earlier links no longer work" when the
// press had retired ZERO spendable ones. The screen half was cosmetic; the trail half
// was not, because it answered "why is this dead?" with "cancelled" about a code that
// had simply run out.
func TestPanelEmployeesDB_AnExpiredInvitationIsNotRetired(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Maria Borg", "invited")

	// Two invitations that died an hour ago, inserted directly: the product cannot
	// mint an expired one, which is the point — this is the state a week of Mondays
	// leaves behind.
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for range 2 {
			if _, e := tx.Exec(ctx,
				`INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at)
				 VALUES ($1, $2, $3, now() - interval '1 hour')`,
				p.tenantID, employee, deadCodeHash(t)); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed expired invitations: %v", err)
	}

	p.signIn(t)
	_, page := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})

	// NOTHING WAS RETIRED, so the screen must not say anything was.
	cancelled, _ := invitationStamps(t, p, employee)
	if cancelled != 0 {
		t.Errorf("%d expired invitation(s) were stamped cancelled_at; expiry and "+
			"cancellation are different answers to 'why is this dead?' and the trail "+
			"must not merge them", cancelled)
	}
	if strings.Contains(page, "earlier links") || strings.Contains(page, "no longer works") {
		t.Error("the screen reported retiring links when it retired none")
	}
	// AND THE SCREEN STILL SAYS THE TRUE THING about the link it just made.
	if !strings.Contains(page, "the only link that will sign") {
		t.Error("the screen says nothing about this being the only usable link")
	}

	// POSITIVE CONTROL: a LIVE invitation IS retired by the next press, so the zero
	// above is about expiry and not about a cancellation that stopped working.
	_, second := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if cancelled, _ := invitationStamps(t, p, employee); cancelled != 1 {
		t.Errorf("%d cancelled after a second press, want exactly 1 (the live one)", cancelled)
	}
	if !strings.Contains(second, "no longer works") {
		t.Error("the screen did not report retiring the live link")
	}
}

// deadCodeHash builds a fixture code_hash: 64 lowercase hex characters, which is the
// shape 00009's CHECK requires. It is OBVIOUSLY FAKE (agent-brief madde 2) and hashes
// nothing.
//
// ⚠️ IT IS RANDOM PER CALL, AND THE FIRST VERSION WAS NOT. code_hash is GLOBALLY
// unique and this database is never cleaned (tappa_app has no DELETE on the table),
// so a deterministic fixture passed on the first run and failed on the second with a
// 23505 — measured, on the mutation run that was supposed to be measuring something
// else. A fixture that only works once is a fixture that hides the result it was
// built to show.
func deadCodeHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// TestPanelEmployeesDB_ARepeatedConfirmationWritesOnlyOnce is the DATABASE half of
// the one-shot limit: the confirmation can be spent again, and the second spend
// changes nothing.
//
// 🔴 IT IS WHY THE LIMIT IS COUNTED RATHER THAN CLOSED. Server-side single use needs
// server-side state, and the gain was measured at zero: the actor is the panel's own
// operator (who can mint a fresh confirmation with one GET), and every spend after
// the first meets DeactivateEmployee's `status <> 'deactivated'` predicate — no
// second employees write, no second audit row, no second lifecycle stamp.
func TestPanelEmployeesDB_ARepeatedConfirmationWritesOnlyOnce(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Maria Borg", "active")
	p.signIn(t)

	_, page := p.get(t, employeesHref+"?manage="+employee.String()+"&confirm=deactivate")
	token := confirmTokenIn(t, page)
	form := url.Values{"id": {employee.String()}, confirmField: {token}}

	// FIRST SPEND — the positive control.
	res, _ := p.post(t, employeeDeactivateHref, form)
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "done=deactivated") {
		t.Fatalf("the first spend redirected to %q", loc)
	}
	if got := employeeStatusOf(t, p, p.tenantID, employee); got != "deactivated" {
		t.Fatalf("status = %q after the first spend", got)
	}
	rows := employeeAuditRows(t, p, tenant.ActionEmployeeDeactivated, employee)
	if rows != 1 {
		t.Fatalf("%d audit row(s) after the first spend, want 1", rows)
	}
	stamp := deactivatedStamp(t, p, employee)

	// SECOND SPEND, cookie re-printed by hand — what a script does. The harness jar
	// honoured the clear, so putting it back is the whole attack.
	p.setCookie(t, adminConfirmCookieName, token)
	res, _ = p.post(t, employeeDeactivateHref, form)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the second spend answered %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "problem=already-deactivated") {
		t.Errorf("the second spend redirected to %q, want problem=already-deactivated — "+
			"if it is now confirm-required a server-side ledger exists and the limit in "+
			"docs/adr/0010 must be deleted", loc)
	}

	// AND NOTHING MOVED.
	if got := employeeAuditRows(t, p, tenant.ActionEmployeeDeactivated, employee); got != rows {
		t.Errorf("audit rows went %d -> %d; a repeated confirmation must write nothing", rows, got)
	}
	if got := deactivatedStamp(t, p, employee); got != stamp {
		t.Errorf("deactivated_at moved from %q to %q; the stamp answers WHEN this ended",
			stamp, got)
	}
}

func deactivatedStamp(t *testing.T, p *panelHarness, employee uuid.UUID) string {
	t.Helper()
	var at string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT coalesce(deactivated_at::text, '') FROM employees WHERE tenant_id = $1 AND id = $2`,
			p.tenantID, employee).Scan(&at)
	}); err != nil {
		t.Fatalf("read deactivated_at: %v", err)
	}
	return at
}

// harnessDeactivate walks the confirmation step over real HTTP and posts the
// deactivation. The harness client keeps cookies, so the signed value flows exactly
// as it does in a browser (user decision, 2026-08-08 — the gate is server-side now).
func harnessDeactivate(t *testing.T, p *panelHarness, employee uuid.UUID) *http.Response {
	t.Helper()
	_, page := p.get(t, employeesHref+"?manage="+employee.String()+"&confirm=deactivate")
	res, _ := p.post(t, employeeDeactivateHref, url.Values{
		"id": {employee.String()}, confirmField: {confirmTokenIn(t, page)},
	})
	return res
}

// pendingInvitations counts the invitations that could be spent RIGHT NOW — the same
// THREE conditions the consuming statement tests.
//
// ⚠️ IT WAS TWO CONDITIONS AND THE MISSING ONE WAS THE POINT OF THE FIX. Written
// against the pre-00012 schema it asked only "not used, not expired", so on the first
// run after cancellation landed it counted a RETIRED invitation as spendable and the
// test failed for the wrong reason. A helper that mirrors a statement has to mirror
// all of it: `used_at IS NULL AND cancelled_at IS NULL AND now() < expires_at`.
func pendingInvitations(t *testing.T, p *panelHarness, employee uuid.UUID) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM employee_invites
			WHERE tenant_id = $1 AND employee_id = $2
			  AND used_at IS NULL AND cancelled_at IS NULL AND now() < expires_at`,
			p.tenantID, employee).Scan(&n)
	}); err != nil {
		t.Fatalf("count invitations: %v", err)
	}
	return n
}

// activateOverHTTP walks the employee's own two-step flow with a FRESH cookie jar, so
// each activation is a different phone rather than the same browser twice.
func activateOverHTTP(t *testing.T, p *panelHarness, code string) bool {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	c := &http.Client{Jar: jar}
	res, err := c.Get(p.server.URL + "/activate?code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatalf("GET /activate: %v", err)
	}
	page := readAll(t, res)
	if res.StatusCode != http.StatusOK {
		return false
	}
	done, err := c.PostForm(p.server.URL+"/api/activate", url.Values{
		"consent": {"yes"}, "csrf": {formToken(t, page)},
	})
	if err != nil {
		t.Fatalf("POST /api/activate: %v", err)
	}
	readAll(t, done)
	// The two landing places of a SUCCESSFUL activation: a first one lands on the mini
	// tour, a second device on the confirmation.
	return done.Request.URL.Path == "/activate/tour" || done.Request.URL.Path == "/activate/done"
}

// TestPanelEmployeesDB_ADeactivatedPersonGetsNoInvitation. The consuming statement
// refuses to spend an invitation for somebody deactivated (db/queries/invites.sql),
// so minting one would create a credential that can never work — and the manager is
// told why rather than handed a dead link.
func TestPanelEmployeesDB_ADeactivatedPersonGetsNoInvitation(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Paul Spiteri", "deactivated")
	p.signIn(t)

	res, _ := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("invite answered %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "problem=not-invitable") {
		t.Errorf("redirected to %q, want problem=not-invitable", loc)
	}
	var invites int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2`,
			p.tenantID, employee).Scan(&invites)
	}); err != nil {
		t.Fatalf("count invites: %v", err)
	}
	if invites != 0 {
		t.Errorf("%d invitation(s) were created for a deactivated employee, want 0", invites)
	}
	if n := employeeAuditRows(t, p, invite.ActionCodeShownToManager, employee); n != 0 {
		t.Errorf("%d disclosure row(s) for an invitation that was never created", n)
	}
}

// TestPanelEmployeesDB_AnotherBusinessesEmployeeIsUntouched is §4.5 through the whole
// stack: a signed-in manager of business A posts business B's employee id at all
// three routes.
//
// 🔴 THE POSITIVE CONTROL COMES FIRST. A's manager is required to deactivate A's OWN
// employee successfully; only then is B's row being unchanged evidence of isolation
// rather than of a broken form.
func TestPanelEmployeesDB_AnotherBusinessesEmployeeIsUntouched(t *testing.T) {
	p := newPanelHarness(t)
	ownVenue, _, own := seedPanelPerson(t, p, p.tenantID, "Alpha Person", "active")
	p.signIn(t)

	// Business B: a complete second business that never signs in.
	other := uuid.New()
	if err := p.data.WithTenant(context.Background(), other, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Bravo Ltd', $2, 'bar', 'single')`,
			other, "VAT-"+other.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	otherVenue, _, otherEmployee := seedPanelPerson(t, p, other, "Bravo Person", "active")

	// CONTROL: the manager can act on their own roster.
	res := harnessDeactivate(t, p, own)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("deactivating own employee answered %d, want 303", res.StatusCode)
	}
	if got := employeeStatusOf(t, p, p.tenantID, own); got != "deactivated" {
		t.Fatalf("the control deactivation did not take effect (status %q)", got)
	}

	// NOW THE FOREIGN ID, at every route.
	// ⚠️ THE FOREIGN DEACTIVATION CARRIES A REAL CONFIRMATION, obtained for that id,
	// so this measures the TENANT boundary rather than the confirmation gate. Without
	// it the refusal below would be the wrong refusal and would prove nothing about
	// §4.5. The confirmation screen renders for an id this tenant cannot see, which is
	// itself the answer: the card is absent, so the token is minted only when the
	// person resolves — see the assertion after the loop.
	_, foreignPage := p.get(t, employeesHref+"?manage="+otherEmployee.String()+"&confirm=deactivate")
	if strings.Contains(foreignPage, confirmField) {
		t.Error("the panel minted a deactivation confirmation for ANOTHER business's " +
			"employee id; the card must not render for somebody this tenant cannot read")
	}

	for _, tc := range []struct {
		target string
		form   url.Values
	}{
		{employeeDeactivateHref, url.Values{"id": {otherEmployee.String()}}},
		{employeeInviteHref, url.Values{"id": {otherEmployee.String()}}},
		{employeeMoveHref, url.Values{
			"id": {otherEmployee.String()}, "to_location": {ownVenue.String()},
		}},
		// AND THE OTHER DIRECTION: this business's own employee, moved to ANOTHER
		// business's venue. The composite foreign key refuses it structurally; what is
		// measured here is that the manager gets a sentence and the row is untouched.
		{employeeMoveHref, url.Values{
			"id": {own.String()}, "to_location": {otherVenue.String()},
		}},
	} {
		res, _ := p.post(t, tc.target, tc.form)
		if res.StatusCode != http.StatusSeeOther {
			t.Errorf("POST %s with a foreign id answered %d, want 303", tc.target, res.StatusCode)
		}
		if loc := res.Header.Get("Location"); !strings.Contains(loc, "problem=") {
			t.Errorf("POST %s with a foreign id redirected to %q with no problem word; "+
				"§4.6 forbids a silent refusal", tc.target, loc)
		}
	}

	if got := employeeStatusOf(t, p, other, otherEmployee); got != "active" {
		t.Errorf("the other business's employee is now %q", got)
	}
	if n := employeeAuditRows(t, p, tenant.ActionEmployeeDeactivated, otherEmployee); n != 0 {
		t.Errorf("%d audit row(s) about another business's employee", n)
	}
	var foreignInvites int
	if err := p.data.WithTenant(context.Background(), other, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2`,
			other, otherEmployee).Scan(&foreignInvites)
	}); err != nil {
		t.Fatalf("count foreign invites: %v", err)
	}
	if foreignInvites != 0 {
		t.Errorf("%d invitation(s) were minted for another business's employee", foreignInvites)
	}
	// The control employee stayed where they were, so the cross-venue move wrote
	// nothing either.
	var venue uuid.UUID
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT location_id FROM employees WHERE tenant_id = $1 AND id = $2`,
			p.tenantID, own).Scan(&venue)
	}); err != nil {
		t.Fatalf("read venue: %v", err)
	}
	if venue != ownVenue {
		t.Errorf("the employee was moved to %s, which is not this business's venue", venue)
	}
}

// TestPanelEmployeesDB_TheMoveIsSavedAndConfirmedFromTheDatabase is the ordinary
// path, plus the property the confirmation banner rests on: what the screen says
// after a move is READ BACK rather than echoed from the redirect.
func TestPanelEmployeesDB_TheMoveIsSavedAndConfirmedFromTheDatabase(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Maria Borg", "active")
	secondVenue, secondDepartment, _ := seedPanelPerson(t, p, p.tenantID, "Sliema", "active")
	p.signIn(t)

	res, _ := p.post(t, employeeMoveHref, url.Values{
		"id":            {employee.String()},
		"to_location":   {secondVenue.String()},
		"to_department": {secondDepartment.String()},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("move answered %d, want 303", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "done=moved") {
		t.Fatalf("redirected to %q, want done=moved", loc)
	}
	if n := employeeAuditRows(t, p, tenant.ActionEmployeeMoved, employee); n != 1 {
		t.Fatalf("%d employee.moved row(s), want 1", n)
	}

	_, page := p.get(t, loc)
	if !strings.Contains(page, "Sliema Venue") {
		t.Errorf("the confirmation does not name the venue the database now holds")
	}

	// 🔴 THE BANNER IS NOT A CLAIM THE URL CAN MAKE ON ITS OWN. `done=deactivated` on
	// somebody who is still active must render nothing, which is the M6-04 defect
	// (a URL that asserted a decision with zero rows behind it) not being repeated.
	_, page = p.get(t, employeesHref+"?manage="+employee.String()+"&done=deactivated")
	if strings.Contains(page, "Deactivated</") {
		t.Error("a hand-edited done=deactivated rendered the confirmation for somebody " +
			"who is not deactivated")
	}
}

// TestPanelEmployeesDB_TheMovedBannerCannotBeClaimedByAURL is N1: the confirmation a
// hand-edited or forwarded address can produce must be a sentence about the STATE the
// server just read, never about an act it did not see.
//
// 🔴 THE OLD HEADING WAS "Placement saved" AND AN AUDIT PRODUCED IT WITH ONE GET,
// from a browser that had never posted anything. It is the M6-04 defect in the branch
// that was not verified — the deactivated twin IS verified and is measured going red.
func TestPanelEmployeesDB_TheMovedBannerCannotBeClaimedByAURL(t *testing.T) {
	p := newPanelHarness(t)
	venue, _, employee := seedPanelPerson(t, p, p.tenantID, "Maria Borg", "active")
	_ = venue
	p.signIn(t)

	// A browser that has moved NOBODY asks for the confirmation directly.
	_, page := p.get(t, employeesHref+"?manage="+employee.String()+"&done=moved")
	if strings.Contains(page, "Placement saved") {
		t.Error("a URL alone rendered \"Placement saved\" — an act the server never saw")
	}
	// What it MAY say is where the person works, because that was read in this request.
	if !strings.Contains(page, "Maria Borg now works at") {
		t.Error("the banner says nothing at all; the honest replacement for the claim is " +
			"the STATE, and it has to actually render")
	}
	if !strings.Contains(page, "Maria Borg Venue") {
		t.Error("the banner does not name the venue the database returned, so it is not " +
			"printing what it read")
	}
}

// activationLinkIn lifts the rendered activation URL off the page. It is the value
// the screen is SUPPOSED to expose — this is the one response body that carries it —
// which is why reading it here is not a leak.
func activationLinkIn(t *testing.T, body string) string {
	t.Helper()
	const marker = "/activate?code="
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no activation link on the page; the invitation screen must render one")
	}
	start := strings.LastIndex(body[:i], ">") + 1
	rest := body[start:]
	if j := strings.IndexAny(rest, "<\n"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// codeFromLink is the raw code, for the absence assertions below.
func codeFromLink(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(strings.ReplaceAll(link, "&amp;", "&"))
	if err != nil {
		t.Fatalf("parse activation link: %v", err)
	}
	code := u.Query().Get("code")
	if len(code) < 16 {
		t.Fatalf("the activation link carries a %d-character code; the fixture is not "+
			"exercising the real minting path", len(code))
	}
	return code
}

// assertCodeAbsentFromColumn is the §4.7 canary: the raw code must not be findable in
// any row of the named column.
//
// 🔴 IT ALSO PROVES ITSELF. A search for a value that is stored NOWHERE would pass
// against an empty table, so the query counts the rows it looked at and fails if
// there were none.
func assertCodeAbsentFromColumn(t *testing.T, p *panelHarness, table, column, code string) {
	t.Helper()
	var rows, hits int
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*), count(*) FILTER (WHERE position($2 in `+column+`) > 0)
			 FROM `+table+` WHERE tenant_id = $1`,
			p.tenantID, code).Scan(&rows, &hits)
	})
	if err != nil {
		t.Fatalf("scan %s.%s: %v", table, column, err)
	}
	if rows == 0 {
		t.Fatalf("%s has no rows in this tenant, so the absence of the code in %s proves "+
			"nothing", table, column)
	}
	if hits != 0 {
		t.Errorf("the raw invitation code appears in %d row(s) of %s.%s (§4.7)", hits, table, column)
	}
}

func employeeStatusOf(t *testing.T, p *panelHarness, tenantID, employeeID uuid.UUID) string {
	t.Helper()
	var status string
	err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status FROM employees WHERE tenant_id = $1 AND id = $2`,
			tenantID, employeeID).Scan(&status)
	})
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}
