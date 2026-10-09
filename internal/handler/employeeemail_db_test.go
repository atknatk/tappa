package handler

// employeeemail_db_test.go -- M10 EM-6 end to end over real HTTP and real Postgres:
// the owner's change reaches the column, the card reads it back, and the activation
// link minted before the change no longer activates anybody; a manager's attempt
// lands as a real trail row in the business's own audit_log; and (round 3) a change
// that fails half-way writes nothing, and pressing it again writes it once.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// TestEmployeeEmailDB_AChangedAddressKillsTheLinkSentBefore is ADR 0022 §7's reason
// for EM-6, walked as a manager and an employee would: invite somebody (the REAL
// invite manager mints a link), change their address, and open the old link — it
// must not activate. The card then shows the new address, and the business's trail
// holds one employee.email_changed row about the person.
//
// THE CONTROL IS THE SAME WALK WITHOUT THE CHANGE: a second invitation, opened
// straight away, activates — so the refusal above is the change's doing and not a
// harness that cannot activate at all.
func TestEmployeeEmailDB_AChangedAddressKillsTheLinkSentBefore(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Rita Zammit", "invited")
	p.signIn(t)

	_, issued := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	stale := codeFromLink(t, activationLinkIn(t, issued))

	// CAPITALS ON PURPOSE (EM-6 round 2): the card must show what was posted, byte for
	// byte, and an all-lower-case fixture cannot tell that from a lower-cased write.
	address := "Rita." + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10]) + "@EM6.example.test"
	res, _ := p.post(t, employeeEmailHref, url.Values{"id": {employee.String()}, "email": {address}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("change answered %d, want 303", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "done=email") {
		t.Fatalf("change redirected to %q, want done=email", loc)
	}
	if pendingInvitations(t, p, employee) != 0 {
		t.Error("an invitation is still spendable after the address changed")
	}
	if consentOverHTTP(t, p, stale) {
		t.Fatal("the link minted BEFORE the address changed still activated a phone")
	}
	_, card := p.get(t, loc)
	if !strings.Contains(card, address) {
		t.Error("the card does not show the address that was just stored")
	}
	if n := employeeAuditRows(t, p, tenant.ActionEmployeeEmailChanged, employee); n != 1 {
		t.Errorf("%d employee.email_changed row(s), want 1", n)
	}

	// CONTROL: a link minted AFTER the change, opened at once, works.
	_, fresh := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if !consentOverHTTP(t, p, codeFromLink(t, activationLinkIn(t, fresh))) {
		t.Fatal("control: a fresh link did not activate, so the refusal above proves nothing")
	}
}

// TestEmployeeEmailDB_AManagersAttemptLandsInTheBusinessesTrail: the refusal row is a
// REAL audit_log row in the session's tenant — keys exactly outcome/reason/
// required_role/role, the manager as actor, the person as target, no address — and
// the domain was never reached. An owner on the same fixture is the control: no
// refusal row.
func TestEmployeeEmailDB_AManagersAttemptLandsInTheBusinessesTrail(t *testing.T) {
	employee := uuid.New()
	address := "ZZ-Refused-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8] + "@EM6.example.test"
	form := url.Values{"id": {employee.String()}, "email": {address}}

	f := newRefusalFixture(t, "manager")
	rec := f.browser(t).do(http.MethodPost, employeeEmailHref, form)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "problem=not-permitted") {
		t.Fatalf("manager POST: %d %q, want 303 with problem=not-permitted", rec.Code, rec.Header().Get("Location"))
	}
	rows := f.trailRows(t, ActionEmployeeEmailChangeRefused)
	if len(rows) != 1 {
		t.Fatalf("%d refusal row(s) in the business's trail, want 1", len(rows))
	}
	r := rows[0]
	if r.Target != employee.String() || r.Actor == nil || *r.Actor != f.adminID || r.TenetID != f.tenantID {
		t.Errorf("refusal row target %q actor %v tenant %s; want the person, the manager and the session's tenant",
			r.Target, r.Actor, r.TenetID)
	}
	if keys := f.detailKeys(t, ActionEmployeeEmailChangeRefused, employee.String()); strings.Join(keys, ",") != "outcome,reason,required_role,role" {
		t.Errorf("refusal detail keys = %v, want exactly outcome, reason, required_role, role", keys)
	}
	if strings.Contains(strings.ToLower(r.Detail), "zz-refused") || strings.Contains(r.Detail, "@") {
		t.Errorf("the refusal row carries the posted address: %s", r.Detail)
	}

	owner := newRefusalFixture(t, "owner")
	if rec := owner.browser(t).do(http.MethodPost, employeeEmailHref, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("owner POST answered %d", rec.Code)
	}
	if rows := owner.trailRows(t, ActionEmployeeEmailChangeRefused); len(rows) != 0 {
		t.Errorf("an owner's change wrote %d refusal row(s)", len(rows))
	}
	// Nothing about either fixture's people reached a real employees row: the fixture's
	// staff is a fake, which is the point — a refused request must not need one.
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM employees WHERE tenant_id = $1 AND email = $2`,
			f.tenantID, address).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			t.Errorf("%d employee row(s) hold the refused address", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("read employees: %v", err)
	}
}

// TestEmployeeEmailDB_AFailedChangeWritesNothingAndARetryWritesOnce measures, for THIS
// route, the two claims of the page its 500 now shows (problemPanelWriteFailed, EM-6
// round 3): "nothing was written" and "pressing again will not enter it twice". The
// page was written for the manual entry and measured there; a call site that adopts it
// adopts its claims, so they are counted here against the address change's own
// statements rather than borrowed.
//
// 🔴 THE BREAK IS THE LAST STEP, AND THAT IS MEASURED RATHER THAN READ OFF THE CODE.
// The change is five statements in one transaction — retire the links, lock the
// person, retire again, write the address, write the trail row — and the trail is
// broken (peekingTrail, below). Before it refuses, it reads THROUGH THE TRANSACTION IT
// WAS HANDED what the change has done so far, and the test requires that state to be
// the finished change: the new address and no spendable link. EM-6 round 4: the first
// version only counted the trail's calls and inferred "the four before it have run"
// from the order in staffemail.go — an audit moved the trail write to the START of the
// same transaction (its E6b) and this test stayed green, because a change that never
// started also leaves zeros behind. Then three counts after the 500: the address is
// still the old one byte for byte, the link minted before is still spendable, and the
// business's trail holds no new row of ANY action about the person (the handler's own
// trail is the real one, so a row it wrote would be counted). Together they say the
// new address and the retirement WERE in the transaction and are NOT in the database.
//
// THE RETRY IS THE CONTROL AND THE SECOND CLAIM. Pressed again on a healthy panel the
// same change writes exactly once (one new trail row, the link retired), which also
// proves the broken arm was not failing for a reason the fixture introduced. Pressed
// a THIRD time — the case of an outage that answered 500 after all — it is refused as
// same-email: the address is unchanged, no row is written, and a link minted in
// between is NOT retired.
func TestEmployeeEmailDB_AFailedChangeWritesNothingAndARetryWritesOnce(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Rita Zammit", "invited")
	p.signIn(t)

	// CAPITALS ON PURPOSE: "the old value" is compared byte for byte, and a fixture a
	// lower-casing path would leave unchanged cannot tell the two apart.
	suffix := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10])
	oldAddress := "Rita.Old." + suffix + "@EM6.example.test"
	newAddress := "Rita.New." + suffix + "@EM6.example.test"
	change := url.Values{"id": {employee.String()}, "email": {newAddress}}

	// THE STARTING STATE, through the product: an address on file, then a link.
	if res, _ := p.post(t, employeeEmailHref, url.Values{"id": {employee.String()}, "email": {oldAddress}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("seeding the old address answered %d, want 303", res.StatusCode)
	}
	_, issued := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	stale := codeFromLink(t, activationLinkIn(t, issued))
	if n := pendingInvitations(t, p, employee); n != 1 {
		t.Fatalf("the fixture holds %d spendable link(s), want 1", n)
	}
	changedBefore := employeeAuditRows(t, p, tenant.ActionEmployeeEmailChanged, employee)
	rowsBefore := trailRowsAbout(t, p, employee)
	if changedBefore != 1 {
		t.Fatalf("the fixture holds %d employee.email_changed row(s), want 1", changedBefore)
	}

	// THE BROKEN PANEL: the same database, the same business and owner, the REAL Staff —
	// only its trail refuses, after looking.
	broken := &peekingTrail{tenantID: p.tenantID, employee: employee}
	staff, err := tenant.NewStaff(p.data, broken, discardLogger())
	if err != nil {
		t.Fatalf("tenant.NewStaff: %v", err)
	}
	trail, err := audit.New(p.data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	sessionID := uuid.New()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: sessionID, TenantID: p.tenantID, AdminUserID: p.adminID,
			Role: adminRoleOwner, FullName: "E2E Owner",
		}, nil
	}}
	logs := &strings.Builder{}
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{},
		staff, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(),
		nil, &fakeNotices{}, adminTestConfig(), slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	rec := b.do(http.MethodPost, employeeEmailHref, change)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("the change with a broken trail answered %d, want 500", rec.Code)
	}
	if broken.calls != 1 {
		t.Fatalf("the broken trail was reached %d time(s), want 1 -- without it the counts "+
			"below would be about a change that never ran", broken.calls)
	}
	// 🔴 WHAT THE TRANSACTION HELD WHEN THE TRAIL WAS REACHED. Without this the zeros
	// below cannot tell a rolled-back change from one that had not started.
	if broken.readErr != nil {
		t.Fatalf("the broken trail could not read the change's own transaction: %v", broken.readErr)
	}
	t.Logf("MEASURED inside the change's transaction when the trail was reached: "+
		"address is the new one=%t, spendable links=%d",
		broken.address != nil && *broken.address == newAddress, broken.pending)
	if broken.address == nil || *broken.address != newAddress {
		t.Errorf("when the trail was reached the transaction held address %s, want the new one "+
			"-- the trail is not the change's last step, so the counts below say nothing about "+
			"a rollback", showAddress(broken.address))
	}
	if broken.pending != 0 {
		t.Errorf("when the trail was reached the transaction still held %d spendable link(s), "+
			"want 0 -- the retirement had not run yet", broken.pending)
	}
	page := htmlOf(t, rec)
	for _, want := range []string{"We could not save that", "nothing was written"} {
		if !strings.Contains(page, want) {
			t.Errorf("the 500 does not say %q", want)
		}
	}
	if strings.Contains(strings.ToLower(page), strings.ToLower(newAddress)) {
		t.Error("the 500 carries the posted address")
	}
	if !strings.Contains(logs.String(), "could not change the employee") {
		t.Error("the failure did not reach the process log, so the next check reads nothing")
	}
	if strings.Contains(strings.ToLower(logs.String()), "rita.new.") {
		t.Error("the failure's log line carries the posted address (R7b)")
	}

	// 🔴 "NOTHING WAS WRITTEN", COUNTED.
	address := addressOnFile(t, p, employee)
	pending := pendingInvitations(t, p, employee)
	changed := employeeAuditRows(t, p, tenant.ActionEmployeeEmailChanged, employee)
	rows := trailRowsAbout(t, p, employee)
	t.Logf("MEASURED after the broken change: address unchanged=%t, spendable links=%d, "+
		"employee.email_changed rows=%d (before %d), trail rows about the person=%d (before %d), "+
		"broken trail calls=%d",
		address != nil && *address == oldAddress, pending, changed, changedBefore, rows, rowsBefore, broken.calls)
	if address == nil || *address != oldAddress {
		t.Errorf("the address on file is %s, want the old one byte for byte -- the write "+
			"survived its own missing trail row", showAddress(address))
	}
	if pending != 1 {
		t.Errorf("%d spendable link(s) after the failed change, want 1 -- the retirement "+
			"survived the failure", pending)
	}
	if changed != changedBefore || rows != rowsBefore {
		t.Errorf("the failed change left %d new trail row(s) about the person (%d of them "+
			"employee.email_changed)", rows-rowsBefore, changed-changedBefore)
	}

	// THE RETRY, on the healthy panel: written once.
	res, _ := p.post(t, employeeEmailHref, change)
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "done=email") {
		t.Fatalf("the retry answered %d %q, want 303 with done=email", res.StatusCode, res.Header.Get("Location"))
	}
	if a := addressOnFile(t, p, employee); a == nil || *a != newAddress {
		t.Errorf("after the retry the address on file is %s, want the new one", showAddress(a))
	}
	if n := pendingInvitations(t, p, employee); n != 0 {
		t.Errorf("%d spendable link(s) after the retry, want 0", n)
	}
	if consentOverHTTP(t, p, stale) {
		t.Error("the link minted before the retry still activated a phone")
	}
	retried := employeeAuditRows(t, p, tenant.ActionEmployeeEmailChanged, employee)
	if retried != changedBefore+1 {
		t.Errorf("the retry left %d employee.email_changed row(s), want %d", retried, changedBefore+1)
	}

	// PRESSED AGAIN: the same address a third time, with a link minted in between.
	if res, _ := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}}); res.StatusCode != http.StatusOK {
		t.Fatalf("minting a fresh link answered %d, want 200", res.StatusCode)
	}
	if n := pendingInvitations(t, p, employee); n != 1 {
		t.Fatalf("minting a fresh link left %d spendable, want 1", n)
	}
	rowsBeforeAgain := trailRowsAbout(t, p, employee)
	res, _ = p.post(t, employeeEmailHref, change)
	loc := res.Header.Get("Location")
	again := employeeAuditRows(t, p, tenant.ActionEmployeeEmailChanged, employee)
	rowsAgain := trailRowsAbout(t, p, employee)
	pendingAgain := pendingInvitations(t, p, employee)
	t.Logf("MEASURED pressing the same address again: %d %s, employee.email_changed rows=%d "+
		"(before %d), trail rows about the person=%d (before %d), spendable links=%d (before 1)",
		res.StatusCode, loc, again, retried, rowsAgain, rowsBeforeAgain, pendingAgain)
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "problem=same-email") {
		t.Errorf("pressing again answered %d %q, want 303 with problem=same-email", res.StatusCode, loc)
	}
	if again != retried || rowsAgain != rowsBeforeAgain {
		t.Errorf("pressing again wrote %d trail row(s) (%d of them employee.email_changed), want 0",
			rowsAgain-rowsBeforeAgain, again-retried)
	}
	if pendingAgain != 1 {
		t.Errorf("pressing again left %d spendable link(s), want the fresh one (1)", pendingAgain)
	}
	if a := addressOnFile(t, p, employee); a == nil || *a != newAddress {
		t.Errorf("pressing again changed the address to %s", showAddress(a))
	}
}

// peekingTrail is this test's own broken trail (review_db_test.go's failingTrail is
// shared by other tests and stays as it is). Before refusing, it reads the person's
// address and spendable links THROUGH THE TRANSACTION IT WAS HANDED — the change's own,
// so the read sees the change's uncommitted work and nothing a separate connection
// could. The transaction is already scoped to the business (WithTenant), and the read
// carries the explicit tenant predicate as well.
type peekingTrail struct {
	tenantID uuid.UUID
	employee uuid.UUID

	calls   int
	address *string
	pending int
	readErr error
}

func (p *peekingTrail) RecordTx(ctx context.Context, tx pgx.Tx, _ audit.Event) (uuid.UUID, error) {
	p.calls++
	p.readErr = tx.QueryRow(ctx,
		`SELECT e.email::text,
		        (SELECT count(*) FROM employee_invites i
		          WHERE i.tenant_id = e.tenant_id AND i.employee_id = e.id
		            AND i.used_at IS NULL AND i.cancelled_at IS NULL AND now() < i.expires_at)
		   FROM employees e WHERE e.tenant_id = $1 AND e.id = $2`,
		p.tenantID, p.employee).Scan(&p.address, &p.pending)
	return uuid.Nil, errors.New("the audit trail is unavailable")
}

// addressOnFile reads the column the change writes, in the business's own context.
func addressOnFile(t *testing.T, p *panelHarness, employee uuid.UUID) *string {
	t.Helper()
	var email *string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT email::text FROM employees WHERE tenant_id = $1 AND id = $2`,
			p.tenantID, employee).Scan(&email)
	}); err != nil {
		t.Fatalf("read the address: %v", err)
	}
	return email
}

// trailRowsAbout counts every audit_log row about one person, of ANY action, so a row
// written under a name nobody thought to count is still counted.
func trailRowsAbout(t *testing.T, p *panelHarness, employee uuid.UUID) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2`,
			p.tenantID, employee.String()).Scan(&n)
	}); err != nil {
		t.Fatalf("count trail rows: %v", err)
	}
	return n
}

// showAddress prints a nullable address for a failure message (a pointer would print
// as a memory address and say nothing).
func showAddress(a *string) string {
	if a == nil {
		return "NULL"
	}
	return "\"" + *a + "\""
}
