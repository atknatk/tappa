package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/billing"
)

// The BILLING section end to end — M6-12 phase B, against REAL Postgres.
//
// 🔴 WHY THESE EXIST BESIDE THE DOUBLE-DRIVEN TESTS, which is a question this
// repository has had to answer for every section. A double agrees with whatever it is
// told; the three things measured here cannot be measured against one:
//
//	the read path freezes nothing      counted on billing_periods and audit_log,
//	                                   with a positive control on the counter
//	the handler writes no SECOND       counted on audit_log by ACTION, so the one row
//	  audit row                        billing.Close writes is distinguished from a
//	                                   duplicate written here
//	the frozen row really is frozen    the roster is CHANGED after the close and the
//	                                   screen is re-read; a double would echo whatever
//	                                   it stored
//
// They also drive the REAL *billing.Book (adminlogin_db_test.go's harness wires it),
// so the interface the panel declares and the type production passes are the same
// thing rather than two that happen to agree.
//
// ⚠️ THESE SKIP WITHOUT DATABASE_URL, and a bare `go test` therefore proves nothing
// about them. Run `make test`, which loads .env.

// billingDBMonth is a month that has certainly ENDED, derived from the clock rather
// than written down: the month before the current one in UTC, stepped back one more so
// the assertion cannot land on a boundary while the suite is running.
func billingDBMonth() billing.Month {
	return billing.MonthOf(time.Now(), time.UTC).Add(-2)
}

// seedBillingTenant gives the harness's tenant a zone, a plan, a price and n employees
// whose employment covers the month under test.
//
// THE EMPLOYEES CARRY BOTH LIFECYCLE STAMPS, because tappa_employee_is_billable
// requires the status to agree with them — a fixture that wrote a status and no stamp
// would be counted into unstamped_employees instead, which is a different test.
func seedBillingTenant(t *testing.T, p *panelHarness, m billing.Month, plan string, n int) {
	t.Helper()
	// Signed up well before the month under test, so period_is_after_signup holds.
	signup := time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, time.UTC).AddDate(0, -6, 0)
	activated := signup.AddDate(0, 1, 0)
	// THE VENUE IS PER CALL, not a package variable. Each test builds its own tenant
	// and `locations` is keyed on id alone, so a shared id would collide on the second
	// test and then fail the same-tenant composite FK on the employees below — a
	// fixture failing for a reason that has nothing to do with what is being measured.
	venue := uuid.New()
	// 🔴 THE COMMERCIAL TERMS GO THROUGH THE OPERATOR CONNECTION, AND THAT IS THE
	// NARROWING WORKING RATHER THAN A WORKAROUND. The first version of this fixture set
	// them as the application role and got `permission denied for table tenants`
	// (SQLSTATE 42501): migration 0016 took plan, created_at and
	// price_per_employee_month away from tappa_app on both INSERT and UPDATE, precisely
	// so a panel request can never move what a business is charged. A fixture that sets
	// a tenant's terms is doing an OPERATOR's job, so it connects as one — exactly the
	// argument internal/domain/billing's own ownerExec makes, and if this could be done
	// as tappa_app that package's TestBillingDB_TheAppCannotWriteTheTerms would be
	// asserting something untrue.
	billingOwnerExec(t, `UPDATE tenants SET timezone = 'Europe/Malta', plan = $2,
	                            price_per_employee_month = 1.50, created_at = $3
	                      WHERE id = $1`, p.tenantID, plan, signup)
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Billing Venue')`,
			venue, p.tenantID); e != nil {
			return fmt.Errorf("location: %w", e)
		}
		for i := 0; i < n; i++ {
			if _, e := tx.Exec(ctx,
				`INSERT INTO employees (id, tenant_id, location_id, full_name, status, activated_at)
				 VALUES ($1, $2, $3, $4, 'active', $5)`,
				uuid.New(), p.tenantID, venue,
				fmt.Sprintf("Billing Person %02d", i), activated); e != nil {
				return fmt.Errorf("employee %d: %w", i, e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding the billing tenant: %v", err)
	}
}

// billingOwnerExec runs one statement through the migrate (superuser) connection.
//
// IT IS A SECOND COPY OF internal/domain/billing's ownerExec BY NECESSITY rather than
// by choice: that one is unexported in another package. The reason it exists is stated
// at its only caller above, and it SKIPS rather than fails when the operator connection
// is absent, so a run with DATABASE_URL but no DATABASE_MIGRATE_URL says why it did
// nothing instead of reporting a permission error as a product defect.
func billingOwnerExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; skipping the billing panel DB tests (the " +
			"operator connection is required to set a tenant's commercial terms — migration " +
			"0016 took them away from tappa_app). Run `make test`, which loads .env.")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("owner exec: %v", err)
	}
}

// billingAuditCount counts audit rows by ACTION for this tenant.
//
// ⚠️ IT IS NOT panelHarness.auditCount, WHICH FILTERS ON target = the admin's id. A
// billing row's target is the MONTH (billing.Close sets it), so the shared helper
// would count zero for every state and the obligation-5 assertion would pass over
// nothing.
func billingAuditCount(t *testing.T, p *panelHarness, action string) int {
	t.Helper()
	var n int
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			p.tenantID, action).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// billingDBCounts counts every table this section could reach.
func billingDBCounts(t *testing.T, p *panelHarness) map[string]int {
	t.Helper()
	out := map[string]int{}
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for _, table := range []string{"billing_periods", "audit_log", "employees", "transactions"} {
			var n int
			if e := tx.QueryRow(ctx,
				`SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, p.tenantID).Scan(&n); e != nil {
				return fmt.Errorf("%s: %w", table, e)
			}
			out[table] = n
		}
		return nil
	})
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	return out
}

// TestBillingDB_ReadingTheScreenFreezesNothing is the obligation measured against the
// one table where a stray write is permanent.
//
// 🔴 A COUNTER THAT CANNOT SEE A CHANGE WOULD REPORT "no writes" FOR A READER THAT
// FROZE A YEAR OF MONTHS, so the same counting method is shown moving at the end.
func TestBillingDB_ReadingTheScreenFreezesNothing(t *testing.T) {
	p := newPanelHarness(t)
	m := billingDBMonth()
	seedBillingTenant(t, p, m, "standard", 4)
	p.signIn(t)

	before := billingDBCounts(t, p)
	exportsBefore := billingAuditCount(t, p, ActionBillingExported)

	for _, path := range []string{
		billingHref,
		billingHref + "?month=" + m.String(),
		billingHref + "?month=" + m.Add(-1).String(),
		billingHref + "?month=rubbish",
		billingCSVHref,
		billingCSVHref + "?month=" + m.String(),
	} {
		res, body := p.get(t, path)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", path, res.StatusCode)
		}
		// POSITIVE CONTROL FOR THE READ: the response really is about this tenant, so
		// the counts below are about requests that did the work.
		if !strings.Contains(body, "Panel E2E Ltd") {
			t.Fatalf("GET %s did not render the tenant, so 'reading it froze nothing' would "+
				"be a statement about a request that read nothing", path)
		}
	}

	after := billingDBCounts(t, p)
	for table, n := range before {
		// 🔴 audit_log IS EXCLUDED FROM THE BLANKET CHECK AND MEASURED SEPARATELY BELOW,
		// because ONE of the six addresses above deliberately writes to it: the CSV export
		// appends an access row, exactly as the hours export does (billingcsv.go carries
		// the argument). Excluding it silently would have been the wrong repair — the
		// point of this test is billing_periods, and the audit rows are asserted to be
		// EXACTLY the export's own, so a stray write from any other address still fails.
		if table == "audit_log" {
			continue
		}
		if after[table] != n {
			t.Errorf("§4.3 BREACH: reading the billing screens changed %s from %d to %d.\n"+
				"billing_periods takes no UPDATE and no DELETE from anybody: a page view that "+
				"appended a row would have created a permanent, uncorrectable invoice.",
				table, n, after[table])
		}
	}
	// TWO OF THE SIX ADDRESSES ARE THE CSV, so exactly two access rows are expected —
	// and every one of them must be the export's own action. A row with any other action
	// means some other read path started writing.
	if got := billingAuditCount(t, p, ActionBillingExported) - exportsBefore; got != 2 {
		t.Errorf("two CSV downloads wrote %d %q row(s), want exactly 2", got, ActionBillingExported)
	}
	if got, want := after["audit_log"]-before["audit_log"], 2; got != want {
		t.Errorf("reading the billing screens wrote %d audit row(s), want %d — the only "+
			"read path here that may write is the export, and only its own access row",
			got, want)
	}

	// POSITIVE CONTROL FOR THE COUNTER.
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO audit_log (tenant_id, action, target, detail)
			 VALUES ($1, 'test.counter.probe', 'billing', '{}'::jsonb)`, p.tenantID)
		return e
	}); err != nil {
		t.Fatalf("positive control insert: %v", err)
	}
	if got := billingDBCounts(t, p); got["audit_log"] == before["audit_log"] {
		t.Fatal("the row counter did not move after an INSERT, so the assertion above " +
			"could not have failed for a reader that wrote")
	}
}

// TestBillingDB_FreezingAMonthWritesExactlyOneRowAndOneAuditRow is obligations 4 and 5
// together, and the SECOND count is the one obligation 5 asks for.
//
// 🔴 THE AUDIT ROW IS COUNTED BY ACTION, NOT IN TOTAL. billing.Close writes exactly one
// `billing.period_closed` inside the transaction that inserts the frozen row; a second
// row written by the handler would be a duplicate on a table that takes no UPDATE and
// no DELETE. A total would not tell those apart from the session's own trail.
func TestBillingDB_FreezingAMonthWritesExactlyOneRowAndOneAuditRow(t *testing.T) {
	p := newPanelHarness(t)
	m := billingDBMonth()
	seedBillingTenant(t, p, m, "standard", 4)
	p.signIn(t)

	before := billingDBCounts(t, p)
	auditBefore := billingAuditCount(t, p, billing.ActionPeriodClosed)

	// STEP 1: the warning. It must write nothing.
	res, body := p.post(t, billingCloseHref, url.Values{"month": {m.String()}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: %d, want the warning screen", billingCloseHref, res.StatusCode)
	}
	token := confirmTokenFrom(t, body)
	if mid := billingDBCounts(t, p); mid["billing_periods"] != before["billing_periods"] {
		t.Fatalf("the warning step froze a period (billing_periods %d -> %d)",
			before["billing_periods"], mid["billing_periods"])
	}

	// STEP 2: the write.
	res, _ = p.post(t, billingFreezeHref, url.Values{"month": {m.String()}, confirmField: {token}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST %s: %d, want 303", billingFreezeHref, res.StatusCode)
	}

	after := billingDBCounts(t, p)
	if got := after["billing_periods"] - before["billing_periods"]; got != 1 {
		t.Fatalf("freezing a month wrote %d billing_periods row(s), want exactly 1", got)
	}
	if got := billingAuditCount(t, p, billing.ActionPeriodClosed) - auditBefore; got != 1 {
		t.Errorf("freezing a month wrote %d %q audit row(s), want exactly 1.\n"+
			"billing.Close writes it in the same transaction as the frozen row; a second "+
			"one from the handler would be a permanent duplicate.", got, billing.ActionPeriodClosed)
	}

	// THE STORED ROW CARRIES WHAT THE DEFINITION PRODUCES, not what any form posted:
	// there is no parameter an amount could be posted into.
	var count int
	var amount, unit string
	var closedBy uuid.UUID
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT employee_count, amount_due::text, unit_price::text, closed_by
			   FROM billing_periods
			  WHERE tenant_id = $1 AND period_month = $2::date`,
			p.tenantID, m.String()+"-01").Scan(&count, &amount, &unit, &closedBy)
	}); err != nil {
		t.Fatalf("reading the frozen row: %v", err)
	}
	if count != 4 {
		t.Errorf("the frozen headcount is %d, want 4", count)
	}
	if unit != "1.50" || amount != "6.00" {
		t.Errorf("the frozen row is %s x %d = %s, want 1.50 x 4 = 6.00", unit, count, amount)
	}
	if closedBy != p.adminID {
		t.Errorf("closed_by is %s, want the signed-in admin %s", closedBy, p.adminID)
	}

	// AND THE SCREEN NOW SHOWS THE FROZEN DOCUMENT rather than a draft — M6-04's rule
	// that where it goes afterwards is the thing itself.
	_, page := p.get(t, billingHref+"?month="+m.String())
	if !strings.Contains(page, ">FROZEN<") {
		t.Error("after freezing, the month still renders as a DRAFT")
	}
	if !strings.Contains(page, "€6.00") {
		t.Error("the frozen page does not show the stored amount with its symbol")
	}
}

// TestBillingDB_AFrozenMonthDoesNotMoveWhenTheRosterDoes is the M6-12 card's fourth
// criterion, measured the only way it can be: by CHANGING THE ROSTER after the close.
//
// 🔴 A DOUBLE CANNOT MEASURE THIS. The claim is that no figure is recomputed, and a
// fake that stored a Draft and handed it back would satisfy it trivially. Here the
// employees really are deactivated between the two reads, so a screen that recomputed
// would print a different number.
func TestBillingDB_AFrozenMonthDoesNotMoveWhenTheRosterDoes(t *testing.T) {
	p := newPanelHarness(t)
	m := billingDBMonth()
	seedBillingTenant(t, p, m, "standard", 4)
	p.signIn(t)

	_, body := p.post(t, billingCloseHref, url.Values{"month": {m.String()}})
	token := confirmTokenFrom(t, body)
	if res, _ := p.post(t, billingFreezeHref, url.Values{"month": {m.String()}, confirmField: {token}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("freeze: %d, want 303", res.StatusCode)
	}
	_, frozenPage := p.get(t, billingHref+"?month="+m.String())
	if !strings.Contains(frozenPage, "€6.00") {
		t.Fatalf("the month did not freeze at 4 x 1.50; this test would assert over nothing")
	}

	// THE ROSTER CHANGES: everybody leaves, and the price doubles. A recomputing screen
	// would answer differently on both counts.
	//
	// The roster half is the APPLICATION's own act — deactivating people is what the
	// employees section does — so it runs as tappa_app; the price is an operator's, for
	// the reason seedBillingTenant states. Splitting them is not tidiness: doing the
	// deactivation as the operator would prove the frozen row survives a change nobody
	// can actually make through the product.
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE employees SET status = 'deactivated', deactivated_at = now()
			  WHERE tenant_id = $1`, p.tenantID)
		return e
	}); err != nil {
		t.Fatalf("deactivating the roster: %v", err)
	}
	billingOwnerExec(t, `UPDATE tenants SET price_per_employee_month = 3.00 WHERE id = $1`, p.tenantID)

	_, again := p.get(t, billingHref+"?month="+m.String())
	if !strings.Contains(again, "€6.00") {
		t.Error("the frozen month changed after the roster and the price did.\n" +
			"A recomputed invoice is one that quietly disagrees with the one that was sent.")
	}
	if strings.Contains(again, "€12.00") || strings.Contains(again, "€0.00") {
		t.Error("the frozen month re-priced itself against today's roster")
	}
	// AND THE FILE AGREES WITH THE SCREEN — one figure, two surfaces.
	_, file := p.get(t, billingCSVHref+"?month="+m.String())
	if !strings.Contains(file, "Amount due,6.00,EUR") {
		t.Errorf("the export disagrees with the screen about the frozen amount")
	}
}

// TestBillingDB_TheFrozenRowCannotBeEditedOrRemoved is the schema half of the promise
// this screen makes in words. The panel offers no such route, so this drives SQL
// directly as the application role — which is exactly the level migration 0016's
// REVOKE and trigger are aimed at.
func TestBillingDB_TheFrozenRowCannotBeEditedOrRemoved(t *testing.T) {
	p := newPanelHarness(t)
	m := billingDBMonth()
	seedBillingTenant(t, p, m, "standard", 2)
	p.signIn(t)

	_, body := p.post(t, billingCloseHref, url.Values{"month": {m.String()}})
	token := confirmTokenFrom(t, body)
	if res, _ := p.post(t, billingFreezeHref, url.Values{"month": {m.String()}, confirmField: {token}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("freeze: %d, want 303", res.StatusCode)
	}

	for _, stmt := range []struct{ what, sql string }{
		{"UPDATE", `UPDATE billing_periods SET employee_count = 99 WHERE tenant_id = $1`},
		{"DELETE", `DELETE FROM billing_periods WHERE tenant_id = $1`},
	} {
		err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, stmt.sql, p.tenantID)
			return e
		})
		if err == nil {
			t.Errorf("%s on billing_periods SUCCEEDED as the application role.\n"+
				"The whole meaning of a frozen figure is that it cannot be one that was edited.",
				stmt.what)
		}
	}

	// A SECOND CLOSE OF THE SAME MONTH IS REFUSED AND SAYS SO, rather than adding a
	// second answer beside the first.
	_, second := p.post(t, billingCloseHref, url.Values{"month": {m.String()}})
	_ = second
	res, _ := p.post(t, billingCloseHref, url.Values{"month": {m.String()}})
	if got, want := res.Header.Get("Location"), billingReturn("already-closed"); got != want {
		t.Errorf("a second close redirected to %q, want %q", got, want)
	}
}

// TestBillingDB_TheFoundingWarningComesFromTheRowAndNotFromAConstant is obligation 3's
// database half.
//
// ⚠️ NO DATE IS ASSERTED. The tenant's signup is written by the fixture and the first
// chargeable month is read back from the DATABASE's own function, so this compares the
// screen against tappa_first_chargeable_month rather than against anything written
// down here. test/fixtures/seed.sql's design partners use `now() - interval '90 days'`,
// which is why a repository must never carry that date as a fact.
func TestBillingDB_TheFoundingWarningComesFromTheRowAndNotFromAConstant(t *testing.T) {
	p := newPanelHarness(t)
	m := billingDBMonth()
	seedBillingTenant(t, p, m, "founding", 3)
	p.signIn(t)

	var first time.Time
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT tappa_first_chargeable_month(plan, created_at, timezone) FROM tenants WHERE id = $1`,
			p.tenantID).Scan(&first)
	}); err != nil {
		t.Fatalf("reading the first chargeable month: %v", err)
	}
	want := first.Format("January 2006")

	_, page := p.get(t, billingHref+"?month="+m.String())
	if !strings.Contains(page, want) {
		t.Errorf("the founding notice does not name %q, which is what the database's own "+
			"tappa_first_chargeable_month returns for this tenant", want)
	}
	// The fixture signs up six months before the month under test, so the free window
	// has certainly run out by now and the notice must say so rather than promise more
	// free months.
	if !strings.Contains(page, htmlText("free months have ended")) {
		t.Errorf("a founding tenant whose window ran out months ago is not warned.\n" +
			"The card: \"otherwise the free period quietly extends itself\".")
	}
}

// TestBillingDB_NoEmployeeNameReachesTheScreenOrTheFile is §4.7 measured on a tenant
// whose employees really have names.
//
// 🔴 WITHOUT THE FIXTURE THIS WOULD ASSERT THAT A PAGE WITH NO NAMES ON IT HAS NO
// NAMES ON IT. The four people below are the ones being counted, so if the count's
// query ever grew a name column, or a view model grew a field, the page would carry
// them and this goes red. The wall is the domain's — db/queries/billing.sql touches
// `employees` as a COUNT and never as a row — and this is the measurement that the
// wall is still where the comment says it is.
//
// IT COVERS THE EXPORT TOO, which is the surface that matters more: a screen is looked
// at once by somebody signed in; a file is emailed to an accountant and forwarded.
func TestBillingDB_NoEmployeeNameReachesTheScreenOrTheFile(t *testing.T) {
	p := newPanelHarness(t)
	m := billingDBMonth()
	seedBillingTenant(t, p, m, "standard", 4)
	p.signIn(t)

	// The names the fixture planted, read back from the database rather than restated,
	// so a change to seedBillingTenant cannot make this test stop covering them.
	var names []string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT full_name FROM employees WHERE tenant_id = $1`, p.tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if e := rows.Scan(&n); e != nil {
				return e
			}
			names = append(names, n)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("reading the planted names: %v", err)
	}
	if len(names) != 4 {
		t.Fatalf("the fixture planted %d names, want 4; this test would assert over nothing", len(names))
	}

	// Freeze it too, so BOTH documents — the live draft and the frozen row — are covered.
	_, warning := p.post(t, billingCloseHref, url.Values{"month": {m.String()}})
	token := confirmTokenFrom(t, warning)
	if res, _ := p.post(t, billingFreezeHref, url.Values{"month": {m.String()}, confirmField: {token}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("freeze: %d, want 303", res.StatusCode)
	}

	for _, path := range []string{
		billingHref + "?month=" + m.String(),
		billingCSVHref + "?month=" + m.String(),
		billingHref,
		billingCSVHref,
	} {
		res, body := p.get(t, path)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", path, res.StatusCode)
		}
		// POSITIVE CONTROL: the document really is about this tenant and really carries
		// the count, so "no name in it" is a statement about a populated document.
		if !strings.Contains(body, "4") {
			t.Fatalf("GET %s does not carry the headcount, so the §4.7 check below would be "+
				"about an empty document", path)
		}
		for _, name := range names {
			if strings.Contains(body, name) {
				t.Errorf("§4.7 BREACH: %s carries the employee name %q.\n"+
					"An invoice for this product names ONE party: the business being billed.",
					path, name)
			}
		}
	}
}

// TestBillingDB_ANonOwnerCannotFreezeAMonth is S1's answer against a REAL admin_users
// row, so the role being compared is the one the resolver read from the database rather
// than one a double supplied.
func TestBillingDB_ANonOwnerCannotFreezeAMonth(t *testing.T) {
	p := newPanelHarness(t)
	m := billingDBMonth()
	seedBillingTenant(t, p, m, "standard", 2)

	// DEMOTE THE HARNESS'S OWN ADMIN BEFORE SIGNING IN, so the session resolves a
	// manager. The role travels on the resolved session and cannot be posted.
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE admin_users SET role = 'manager' WHERE id = $1`, p.adminID)
		return e
	}); err != nil {
		t.Fatalf("demoting the admin: %v", err)
	}
	p.signIn(t)

	before := billingDBCounts(t, p)
	refusalsBefore := billingAuditCount(t, p, ActionPeriodCloseRefused)

	res, _ := p.post(t, billingCloseHref, url.Values{"month": {m.String()}})
	if got, want := res.Header.Get("Location"), billingReturn("not-permitted"); got != want {
		t.Errorf("a manager's close attempt redirected to %q, want %q", got, want)
	}
	if after := billingDBCounts(t, p); after["billing_periods"] != before["billing_periods"] {
		t.Errorf("a manager froze a period (billing_periods %d -> %d)",
			before["billing_periods"], after["billing_periods"])
	}
	// 🔴 K1: THE REFUSAL REACHES audit_log, AGAINST REAL POSTGRES. A double would agree
	// that it did; what this measures is that the row survives audit.Record's own
	// validation and lands inside the tenant's RLS scope, which is the half that matters
	// for the reader it exists for — the OWNER, next quarter.
	if got := billingAuditCount(t, p, ActionPeriodCloseRefused) - refusalsBefore; got != 1 {
		t.Errorf("a manager's refused close wrote %d %q row(s), want exactly 1",
			got, ActionPeriodCloseRefused)
	}
	// A SECOND ATTEMPT AT THE WRITE ROUTE IS A SECOND ROW, because it is a second
	// deliberate act — the manager was refused at the warning step and holds no token,
	// so reaching the write route at all means the UI was bypassed again.
	p.post(t, billingFreezeHref, url.Values{"month": {m.String()}})
	if got := billingAuditCount(t, p, ActionPeriodCloseRefused) - refusalsBefore; got != 2 {
		t.Errorf("two refused attempts wrote %d row(s), want 2 — each attempt really happened", got)
	}
	// AND THE STORED DETAIL IS READABLE BY THE READER IT IS FOR: the month, the step and
	// the two roles, from the jsonb column rather than from the Go value.
	var month, step, role, required string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT detail->>'month', detail->>'step', detail->>'role', detail->>'required_role'
			   FROM audit_log
			  WHERE tenant_id = $1 AND action = $2
			  ORDER BY at DESC LIMIT 1`,
			p.tenantID, ActionPeriodCloseRefused).Scan(&month, &step, &role, &required)
	}); err != nil {
		t.Fatalf("reading the refusal row: %v", err)
	}
	if month != m.String() || step != billingStepWrite || role != "manager" || required != adminRoleOwner {
		t.Errorf("the stored refusal reads month=%q step=%q role=%q required=%q; want %q/%q/manager/owner",
			month, step, role, required, m.String(), billingStepWrite)
	}
	// §4.7: THERE IS NOTHING IN THE ROW A SECRET COULD TRAVEL IN. Asserted on the stored
	// jsonb's own key set rather than on the Go struct, because the struct is what was
	// intended and the column is what shipped.
	//
	// ⚠️ THE ROW IS NARROWED BEFORE THE KEYS ARE EXPANDED, and the first version of this
	// query did it the other way round: jsonb_object_keys is a SET-RETURNING function in
	// the select list, so `... ORDER BY at DESC LIMIT 1` applied to its OUTPUT and
	// returned exactly one KEY rather than one ROW's keys. It reported `[role]` and the
	// stored row was perfectly correct — a test failing for a reason that was its own.
	var keys []string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT array_agg(k ORDER BY k) FROM (
			   SELECT jsonb_object_keys(one.d) AS k FROM (
			     SELECT detail AS d FROM audit_log
			      WHERE tenant_id = $1 AND action = $2
			      ORDER BY at DESC LIMIT 1) one) s`,
			p.tenantID, ActionPeriodCloseRefused).Scan(&keys)
	}); err != nil {
		t.Fatalf("reading the refusal row's keys: %v", err)
	}
	want := "[month outcome reason required_role role step]"
	if got := fmt.Sprint(keys); got != want {
		t.Errorf("the refusal row's detail carries %s, want %s — a new key here is a new "+
			"thing being written into an append-only table, and §4.7 wants it deliberate",
			got, want)
	}
	// AND THE SECTION ITSELF IS CLOSED TO THEM (O1), on BOTH addresses — not merely the
	// button withheld. The refusal is a 403 that SAYS WHY (§4.6), never a 404 and never
	// a blank page.
	for _, path := range []string{billingHref + "?month=" + m.String(), billingCSVHref} {
		res, page := p.get(t, path)
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("GET %s as a manager: %d, want 403", path, res.StatusCode)
		}
		if !strings.Contains(page, htmlText("Billing is the owner's")) {
			t.Errorf("GET %s as a manager does not explain the refusal", path)
		}
		// AND IT LEAKS NO FIGURE — the whole point of the gate is that a manager does not
		// learn what the business owes. The fixture is 2 people at 1.50 = 3.00.
		for _, leak := range []string{"€3.00", "3.00", "People counted"} {
			if strings.Contains(page, leak) {
				t.Errorf("GET %s as a manager leaks %q from the section it refused", path, leak)
			}
		}
	}
	// AND NO ACCESS ROW IS WRITTEN FOR A REFUSED READ: the gate runs before anything,
	// so a manager reloading the CSV url cannot append rows.
	if got := billingAuditCount(t, p, ActionBillingExported); got != 0 {
		t.Errorf("a manager's refused CSV read wrote %d export row(s), want 0", got)
	}
}
