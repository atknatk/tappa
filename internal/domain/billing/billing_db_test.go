package billing

// billing_db_test.go -- M6-12 phase A against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and the list is longer here than usual because
// this package deliberately keeps its arithmetic in SQL. Under test: the billable
// definition itself (migration 0016's tappa_employee_is_billable), a month's
// boundaries resolved in the tenant's own zone by tzdata, the founding-offer window,
// exact numeric money on the wire, the UNIQUE that makes "frozen" real, the
// append-only privileges and trigger, and RLS. A mock would agree with all of them
// unconditionally.
//
// FIXTURES ARE NOT CLEANED UP, and there is no working way to clean them up:
// billing_periods takes no DELETE from anybody (0016), employees has REVOKE DELETE
// (00003) and audit_log is append-only (00005). Fresh random ids keep runs apart.
//
// ⚠️ AND `make db-reset` IS NOT THE ESCAPE HATCH -- backlog T22, measured: 00013's
// Down catches on accumulated development data, so the target is currently unusable
// and the last clean base had to be rebuilt by hand. Sibling test files still name it
// as the cleanup path; they were written before T22 was raised. What that means for
// this file is that every run leaves rows behind permanently, which is exactly why
// nothing here asserts a whole-table count.
//
// ⚠️ THE TENANTS HERE ARE BUILT WITH AN EXPLICIT created_at, which is what makes the
// founding-offer assertions deterministic. The seeded tenants signed up on a real
// date that moves relative to "now" as the calendar advances; a test that depended
// on it would rot.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/test/fixtures"
)

const maltaZone = "Europe/Malta"

type fixture struct {
	data *db.DB
	book *Book

	tenantID   uuid.UUID
	locationID uuid.UUID
	adminID    uuid.UUID
}

// newFixture builds one tenant that signed up at signedUp, in zone, on plan.
func newFixture(t *testing.T, signedUp time.Time, zone, plan string) *fixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping billing DB tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	book, err := NewBook(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewBook: %v", err)
	}

	f := &fixture{
		data: data, book: book,
		tenantID: uuid.New(), locationID: uuid.New(), adminID: uuid.New(),
	}
	err = data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		// 🔴 THE COMMERCIAL TERMS ARE NOT IN THIS INSERT, AND THEY CANNOT BE. 0016
		// narrows tappa_app's INSERT on `tenants` to the tenant's own facts: `plan`,
		// `created_at` and the price are the OPERATOR's, so the row is created here
		// with this table's defaults and the terms are applied below through the
		// superuser connection. If this statement could name them, the narrowing
		// would be a fiction -- which is why the split is left visible rather than
		// hidden in a helper that connects as whoever is available.
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			 VALUES ($1, 'Billing Test Ltd', $2, 'restaurant', 'multi', $3)`,
			f.tenantID, "VAT-"+f.tenantID.String(), zone); e != nil {
			return fmt.Errorf("tenant: %w", e)
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
			 VALUES ($1, $2, 'St Julians', 35.918, 14.489)`,
			f.locationID, f.tenantID); e != nil {
			return fmt.Errorf("location: %w", e)
		}
		// The admin exists because billing_periods.closed_by is NOT NULL with a
		// same-tenant composite FK: a period is somebody's deliberate act.
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
			 VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
			f.adminID, f.tenantID, "owner-"+uuid.NewString()+"@billing.example", fixtures.UnusablePasswordHash)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	// The operator's half: the plan and the signup instant every founding-window
	// assertion depends on. See ownerExec.
	ownerExec(t, `UPDATE tenants SET plan = $2, created_at = $3 WHERE id = $1`,
		f.tenantID, plan, signedUp)
	return f
}

// ownerExec runs one statement through the migrate (superuser) connection.
//
// 🔴 IT EXISTS BECAUSE 0016 TOOK plan, created_at AND price_per_employee_month AWAY
// FROM tappa_app, on both INSERT and UPDATE. A fixture that sets a tenant's
// commercial terms is doing an OPERATOR's job, so it connects as one. That is not a
// workaround for the narrowing -- it is the narrowing working: if these tests could
// set the terms as the application role, TestBillingDB_TheAppCannotWriteTheTerms
// would be asserting something that is not true.
func ownerExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; skipping billing DB tests (the operator " +
			"connection is required to set a tenant's commercial terms)")
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

// hire inserts one employee with exactly the lifecycle stamps given. Nil means the
// stamp is absent, which is how the disagreement rows are built.
func (f *fixture) hire(t *testing.T, name, status string, activated, deactivated *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, activated_at, deactivated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, f.tenantID, f.locationID, name, status, activated, deactivated)
		return e
	})
	if err != nil {
		t.Fatalf("hire %s: %v", name, err)
	}
	return id
}

func at(s string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return &t
}

// ============================================================================
// The definition of "billable", one row per case (§8, table-driven).
// ============================================================================

// TestBillingDB_BillableDefinition is the M6-12 card's first criterion made
// executable: "somebody who was active on ANY day of the month".
//
// The month under test is JULY 2026 in Europe/Malta, which is [2026-06-30 22:00Z,
// 2026-07-31 22:00Z) -- summer, so UTC+2. Every instant below is written in UTC on
// purpose: reading them as local times is exactly the mistake the boundary rows
// exist to catch.
func TestBillingDB_BillableDefinition(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")

	// The two boundary instants of July 2026 in Malta, written out by hand rather
	// than computed -- a test that derived them the way the query does could not
	// disagree with it.
	const julyStartsZ = "2026-06-30T22:00:00Z"
	const julyEndsZ = "2026-07-31T22:00:00Z"

	cases := []struct {
		name        string
		status      string
		activated   *time.Time
		deactivated *time.Time
		billable    bool
		// disagrees marks a row whose status contradicts its stamps: excluded from
		// the count AND counted into unstamped_employees.
		disagrees bool
	}{
		{
			name:      "hired during the month",
			status:    "active",
			activated: at("2026-07-10T09:00:00Z"),
			billable:  true,
		},
		{
			name:        "left during the month",
			status:      "deactivated",
			activated:   at("2026-03-01T09:00:00Z"),
			deactivated: at("2026-07-10T17:00:00Z"),
			billable:    true,
		},
		{
			name:      "active for the whole month",
			status:    "active",
			activated: at("2026-01-01T09:00:00Z"),
			billable:  true,
		},
		{
			name:     "invited but never activated",
			status:   "invited",
			billable: false,
		},
		{
			name:        "activated and gone inside the same month",
			status:      "deactivated",
			activated:   at("2026-07-05T09:00:00Z"),
			deactivated: at("2026-07-20T17:00:00Z"),
			billable:    true,
		},
		{
			name:        "left before the month began",
			status:      "deactivated",
			activated:   at("2026-01-01T09:00:00Z"),
			deactivated: at("2026-06-15T17:00:00Z"),
			billable:    false,
		},
		{
			name:      "hired after the month ended",
			status:    "active",
			activated: at("2026-08-05T09:00:00Z"),
			billable:  false,
		},
		{
			// 🔴 BOUNDARY, OPENING SIDE. Activated at the FIRST instant of the local
			// month. Read in UTC this looks like the 30th of June, which is why the
			// instant is written in UTC here.
			name:      "activated at the first local instant of the month",
			status:    "active",
			activated: at(julyStartsZ),
			billable:  true,
		},
		{
			// The month's local end IS the next month's local start, so somebody who
			// arrives at that instant belongs to August, not to July.
			name:      "activated at the first local instant of the NEXT month",
			status:    "active",
			activated: at(julyEndsZ),
			billable:  false,
		},
		{
			// 🔴 BOUNDARY, CLOSING SIDE, and this row is why the predicate uses a
			// STRICT `>`. Their last active instant was 23:59:59 on the 30th of June
			// in Malta -- June's invoice, not July's. An earlier draft of migration
			// 0016 wrote `>=` here and billed them twice.
			name:        "deactivated at the first local instant of the month",
			status:      "deactivated",
			activated:   at("2026-01-01T09:00:00Z"),
			deactivated: at(julyStartsZ),
			billable:    false,
		},
		{
			name:        "deactivated one microsecond into the month",
			status:      "deactivated",
			activated:   at("2026-01-01T09:00:00Z"),
			deactivated: at("2026-06-30T22:00:00.000001Z"),
			billable:    true,
		},
		{
			// 🔴 THE ROW THE FIRST CONJUNCT EXISTS FOR, and the naive interval reading
			// bills it in the closing month and in every month after it, forever.
			//
			// ⚠️ IT IS NOT PRODUCED BY ANY PRODUCT PATH, and an earlier version of this
			// comment blamed the demo seed for it, wrongly. DeactivateEmployee writes
			// `deactivated_at = COALESCE(deactivated_at, now())` and seed.sql derives
			// both stamps from the status it inserts. What makes these rows is a TEST
			// FIXTURE inserting employees directly -- internal/handler's
			// seedflow_db_test.go hire() and day_db_test.go. So the count in a
			// development database GROWS PER RUN and is worth no citation; the command
			// that answers it for any tenant is in migration 0016's header.
			//
			// Which is why this row is a fixture rather than a reference to real data:
			// the guarantee is that a row of this SHAPE is not billed, whatever put it
			// there.
			name:      "marked as left but carrying no leaving date",
			status:    "deactivated",
			activated: at("2026-01-01T09:00:00Z"),
			billable:  false,
			disagrees: true,
		},
		{
			name:      "marked active but carrying no activation date",
			status:    "active",
			billable:  false,
			disagrees: true,
		},
		{
			name:      "marked invited but carrying an activation date",
			status:    "invited",
			activated: at("2026-07-01T09:00:00Z"),
			billable:  false,
			disagrees: true,
		},
	}

	wantBillable, wantDisagree := 0, 0
	for _, c := range cases {
		f.hire(t, c.name, c.status, c.activated, c.deactivated)
		if c.billable {
			wantBillable++
		}
		if c.disagrees {
			wantDisagree++
		}
	}

	// Each row is asserted on its OWN, so a failure names the case rather than a
	// total that is off by some amount.
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got bool
			err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `
					SELECT tappa_employee_is_billable(status, activated_at, deactivated_at,
					           tappa_local_month_start('2026-07-01', $2),
					           tappa_local_month_start('2026-08-01', $2))
					FROM employees WHERE tenant_id = $1 AND full_name = $3`,
					f.tenantID, maltaZone, c.name).Scan(&got)
			})
			if err != nil {
				t.Fatalf("probe: %v", err)
			}
			if got != c.billable {
				t.Fatalf("billable = %v, want %v", got, c.billable)
			}
		})
	}

	// And the whole-tenant totals the invoice is actually built from.
	d, err := f.book.Preview(context.Background(), f.tenantID, Month{2026, time.July})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if d.EmployeeCount != wantBillable {
		t.Errorf("EmployeeCount = %d, want %d", d.EmployeeCount, wantBillable)
	}
	if d.UnstampedEmployees != wantDisagree {
		t.Errorf("UnstampedEmployees = %d, want %d -- excluded must not mean invisible (§4.6)",
			d.UnstampedEmployees, wantDisagree)
	}
	if wantDisagree == 0 {
		t.Fatal("the fixture has no disagreeing rows, so the honesty counter is untested")
	}
}

// TestBillingDB_MonthBoundsAreResolvedInTheTenantsZone -- §6's "a month begins in
// Malta, not in UTC", with a DST pair so the offset itself is under test.
func TestBillingDB_MonthBoundsAreResolvedInTheTenantsZone(t *testing.T) {
	malta := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	utc := newFixture(t, *at("2020-01-01T00:00:00Z"), "UTC", "standard")

	cases := []struct {
		name       string
		f          *fixture
		month      Month
		from, to   string
		zoneLegend string
	}{
		{
			name: "July in Malta is UTC+2", f: malta, month: Month{2026, time.July},
			from: "2026-06-30T22:00:00Z", to: "2026-07-31T22:00:00Z",
		},
		{
			name: "January in Malta is UTC+1", f: malta, month: Month{2026, time.January},
			from: "2025-12-31T23:00:00Z", to: "2026-01-31T23:00:00Z",
		},
		{
			// THE DST SEAM ITSELF: March 2026 starts at +1 and ends at +2.
			name: "March in Malta starts on winter time and ends on summer time",
			f:    malta, month: Month{2026, time.March},
			from: "2026-02-28T23:00:00Z", to: "2026-03-31T22:00:00Z",
		},
		{
			name: "a UTC tenant gets UTC midnights", f: utc, month: Month{2026, time.July},
			from: "2026-07-01T00:00:00Z", to: "2026-08-01T00:00:00Z",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := c.f.book.Preview(context.Background(), c.f.tenantID, c.month)
			if err != nil {
				t.Fatalf("Preview: %v", err)
			}
			if got := d.From.UTC().Format(time.RFC3339); got != c.from {
				t.Errorf("From = %s, want %s", got, c.from)
			}
			if got := d.To.UTC().Format(time.RFC3339); got != c.to {
				t.Errorf("To = %s, want %s", got, c.to)
			}
			if d.Month != c.month {
				t.Errorf("Month = %v, want %v", d.Month, c.month)
			}
		})
	}
}

// TestBillingDB_FoundingOfferGivesThreeFreeMonths -- the card's third criterion, on
// the arithmetic side. What the SCREEN does with the warning is phase B.
func TestBillingDB_FoundingOfferGivesThreeFreeMonths(t *testing.T) {
	// A MID-MONTH signup, deliberately: a partial first month is the case the
	// month-aligned reading has to answer, and the one where the two readings of
	// "three months free" disagree. The instant is this test's own -- see the file
	// header on why the seeded tenants' dates are not usable as a reference: they
	// are `now() - interval '90 days'`, so their day-of-month is a property of when
	// the database was seeded.
	founding := newFixture(t, *at("2026-05-14T10:36:27Z"), maltaZone, "founding")
	standard := newFixture(t, *at("2026-05-14T10:36:27Z"), maltaZone, "standard")

	cases := []struct {
		name     string
		f        *fixture
		month    Month
		wantFree bool
	}{
		{"founding: the signup month is free", founding, Month{2026, time.May}, true},
		{"founding: the second month is free", founding, Month{2026, time.June}, true},
		{"founding: the third month is free", founding, Month{2026, time.July}, true},
		{"founding: the fourth month is the FIRST INVOICE", founding, Month{2026, time.August}, false},
		{"founding: and every month after it", founding, Month{2026, time.September}, false},
		{"standard: the signup month is already chargeable", standard, Month{2026, time.May}, false},
		{"standard: and so is every month after it", standard, Month{2026, time.August}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := c.f.book.Preview(context.Background(), c.f.tenantID, c.month)
			if err != nil {
				t.Fatalf("Preview: %v", err)
			}
			if d.Free != c.wantFree {
				t.Fatalf("Free = %v, want %v (first chargeable month is %s)",
					d.Free, c.wantFree, d.FirstChargeableMonth)
			}
		})
	}

	// The window itself, named once so phase B's warning has something to read.
	d, err := founding.book.Preview(context.Background(), founding.tenantID, Month{2026, time.August})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if got, want := d.FirstChargeableMonth, (Month{2026, time.August}); got != want {
		t.Fatalf("FirstChargeableMonth = %v, want %v", got, want)
	}
	if s := standardFirstChargeable(t, standard); s != (Month{2026, time.May}) {
		t.Fatalf("standard plan's first chargeable month = %v, want 2026-05", s)
	}
}

func standardFirstChargeable(t *testing.T, f *fixture) Month {
	t.Helper()
	d, err := f.book.Preview(context.Background(), f.tenantID, Month{2026, time.August})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	return d.FirstChargeableMonth
}

// ============================================================================
// Freezing.
// ============================================================================

// TestBillingDB_ClosingFreezesTheFigures is the card's fourth criterion: a past
// month's number is NOT recomputed. It is measured by changing the inputs after the
// close and reading the period again.
func TestBillingDB_ClosingFreezesTheFigures(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	// Three billable people in a month that is safely in the past.
	past := Month{2025, time.June}
	for i := 0; i < 3; i++ {
		f.hire(t, fmt.Sprintf("Billable %d", i), "active", at("2025-01-01T09:00:00Z"), nil)
	}

	ctx := context.Background()
	frozen, err := f.book.Close(ctx, f.tenantID, past, f.adminID)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if frozen.EmployeeCount != 3 {
		t.Fatalf("EmployeeCount at close = %d, want 3", frozen.EmployeeCount)
	}
	if !frozen.Frozen {
		t.Error("the closed period does not report itself as frozen")
	}
	// 3 x the default 1.50 = 4.50, exactly.
	if got := frozen.AmountDue.String(); got != "4.50" {
		t.Fatalf("AmountDue = %q, want 4.50 (3 x 1.50)", got)
	}
	if got := frozen.UnitPrice.String(); got != "1.50" {
		t.Fatalf("UnitPrice = %q, want 1.50 -- the price comes from the tenant row", got)
	}
	if frozen.AmountDue.Currency() != DefaultCurrency {
		t.Errorf("currency = %q, want %q", frozen.AmountDue.Currency(), DefaultCurrency)
	}
	if frozen.ClosedBy != f.adminID {
		t.Errorf("ClosedBy = %s, want %s", frozen.ClosedBy, f.adminID)
	}

	// 🔴 NOW MOVE EVERY INPUT. Two more people, everybody else deactivated, and a
	// different price. A recomputed period would answer differently; a frozen one
	// may not.
	for i := 0; i < 2; i++ {
		f.hire(t, fmt.Sprintf("Later hire %d", i), "active", at("2025-01-01T09:00:00Z"), nil)
	}
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`UPDATE employees SET status = 'deactivated', deactivated_at = now()
			 WHERE tenant_id = $1 AND full_name LIKE 'Billable %'`, f.tenantID); e != nil {
			return e
		}
		return nil
	}); err != nil {
		t.Fatalf("moving the inputs: %v", err)
	}
	// The price is changed as the OWNER, because 0016 deliberately takes that
	// column away from tappa_app -- which is asserted separately below.
	setPriceAsOwner(t, f.tenantID, "9.99")

	again, err := f.book.Period(ctx, f.tenantID, past)
	if err != nil {
		t.Fatalf("Period after the inputs moved: %v", err)
	}
	if !again.Frozen {
		t.Fatal("Period returned a live preview for a month that HAS been closed")
	}
	if again.EmployeeCount != 3 || again.AmountDue.String() != "4.50" || again.UnitPrice.String() != "1.50" {
		t.Fatalf("the frozen period moved: count=%d amount=%s price=%s, want 3 / 4.50 / 1.50",
			again.EmployeeCount, again.AmountDue.String(), again.UnitPrice.String())
	}

	// 🔴 THE FIGURES ARE FROZEN AND THE ADDRESSEE IS NOT, which is the one asymmetry
	// GetBillingPeriod documents. Renaming the business must change the name the
	// document is addressed to and move NO figure.
	if again.TenantName != "Billing Test Ltd" {
		t.Errorf("the frozen period is addressed to %q, want the tenant name", again.TenantName)
	}
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE tenants SET name = 'Renamed Holdings Ltd' WHERE id = $1`, f.tenantID)
		return e
	}); err != nil {
		t.Fatalf("renaming the tenant: %v", err)
	}
	renamed, err := f.book.Period(ctx, f.tenantID, past)
	if err != nil {
		t.Fatalf("Period after the rename: %v", err)
	}
	if renamed.TenantName != "Renamed Holdings Ltd" {
		t.Errorf("after the rename the period is addressed to %q, want the new name", renamed.TenantName)
	}
	if renamed.EmployeeCount != 3 || renamed.AmountDue.String() != "4.50" {
		t.Fatalf("the rename moved a FIGURE: count=%d amount=%s, want 3 / 4.50",
			renamed.EmployeeCount, renamed.AmountDue.String())
	}
	if renamed.ID == uuid.Nil {
		t.Error("a frozen period carries no row id")
	}

	// Positive control: an UNCLOSED month DOES see the new inputs, so the assertion
	// above is about freezing and not about a query that ignores its inputs.
	live, err := f.book.Period(ctx, f.tenantID, Month{2025, time.July})
	if err != nil {
		t.Fatalf("Period on an unclosed month: %v", err)
	}
	if live.Frozen {
		t.Fatal("an unclosed month reported itself as frozen")
	}
	if live.UnitPrice.String() != "9.99" {
		t.Fatalf("the live preview still shows the old price %s; the control does not control",
			live.UnitPrice.String())
	}
}

// setPriceAsOwner changes the price through the operator connection, because
// tappa_app is not allowed to -- see TestBillingDB_TheAppCannotWriteTheTerms.
func setPriceAsOwner(t *testing.T, tenantID uuid.UUID, price string) {
	t.Helper()
	ownerExec(t, `UPDATE tenants SET price_per_employee_month = $2 WHERE id = $1`, tenantID, price)
}

// TestBillingDB_ClosingRefusesWhatItMust -- the three guards, each with the sentence
// a manager should see.
func TestBillingDB_ClosingRefusesWhatItMust(t *testing.T) {
	f := newFixture(t, *at("2025-03-10T09:00:00Z"), maltaZone, "standard")
	ctx := context.Background()

	// 1. A month that has not ended. "Now" is used rather than a literal so this does
	// not rot; the current month is by definition still running.
	//
	// 🔴 IT IS RESOLVED IN THE TENANT'S ZONE AND NOT IN UTC, AND THE UTC VERSION WAS A
	// LATENT FLAKE OF A CLASS THAT FIRED ELSEWHERE IN THIS SUITE. CloseBillingPeriod
	// decides "has this month ended" as `to_at <= now()`, where
	// to_at = tappa_local_month_start(month + 1 month, tenants.timezone) — i.e. in THIS
	// fixture's zone (Europe/Malta), not UTC. For the last two hours of the last UTC day
	// of a month, Malta is already in the next month, so "the current UTC month" is a
	// month that has ENDED in Malta and Close would answer nil instead of
	// ErrPeriodNotEnded. Two hours a month rather than two hours a day, which is only a
	// difference in how long it hides — CLAUDE.md §6 names the class either way.
	//
	// Found while fixing the daily version of the same defect in
	// internal/domain/ledger/advisories_db_test.go; the sweep for siblings is recorded
	// there.
	tenantZone, err := time.LoadLocation(maltaZone)
	if err != nil {
		t.Fatalf("loading %s: %v", maltaZone, err)
	}
	nowMonth := MonthOf(time.Now(), tenantZone)
	if _, err := f.book.Close(ctx, f.tenantID, nowMonth, f.adminID); !errors.Is(err, ErrPeriodNotEnded) {
		t.Errorf("closing the CURRENT month gave %v, want ErrPeriodNotEnded -- the headcount "+
			"is still moving and an append-only row could never be corrected", err)
	}

	// 2. A month from before the business existed.
	if _, err := f.book.Close(ctx, f.tenantID, Month{2019, time.March}, f.adminID); !errors.Is(err, ErrBeforeSignup) {
		t.Errorf("closing a pre-signup month gave %v, want ErrBeforeSignup", err)
	}

	// 3. A second close of the same month.
	done := Month{2025, time.June}
	if _, err := f.book.Close(ctx, f.tenantID, done, f.adminID); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if _, err := f.book.Close(ctx, f.tenantID, done, f.adminID); !errors.Is(err, ErrAlreadyClosed) {
		t.Errorf("the second close gave %v, want ErrAlreadyClosed", err)
	}
	// And there is still exactly ONE row: the refusal is not a silent overwrite.
	if n := countPeriods(t, f, done); n != 1 {
		t.Errorf("%d rows for %s after two closes, want 1", n, done)
	}

	// The signup month itself IS closeable -- otherwise guard 2 would be hiding a
	// permanent off-by-one that nobody could correct.
	if _, err := f.book.Close(ctx, f.tenantID, Month{2025, time.March}, f.adminID); err != nil {
		t.Errorf("closing the SIGNUP month gave %v, want success", err)
	}
}

func countPeriods(t *testing.T, f *fixture, m Month) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM billing_periods WHERE tenant_id = $1 AND period_month = $2`,
			f.tenantID, m.date()).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count periods: %v", err)
	}
	return n
}

// TestBillingDB_ClosingWritesItsTrailInTheSameTransaction -- the audit row exists,
// carries the figures as TEXT (never a JSON number), and shares the period's fate.
func TestBillingDB_ClosingWritesItsTrailInTheSameTransaction(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	f.hire(t, "One Person", "active", at("2020-02-01T09:00:00Z"), nil)
	m := Month{2025, time.May}

	if _, err := f.book.Close(context.Background(), f.tenantID, m, f.adminID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var action, target, actor string
	var detail map[string]any
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT action, target, actor_id::text, detail FROM audit_log
			  WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			f.tenantID, ActionPeriodClosed, m.String()).Scan(&action, &target, &actor, &detail)
	})
	if err != nil {
		t.Fatalf("reading the trail: %v", err)
	}
	if actor != f.adminID.String() {
		t.Errorf("actor_id = %s, want %s", actor, f.adminID)
	}
	// 🔴 MONEY IS TEXT IN THE TRAIL. jsonb numbers decode as float64 in every reader,
	// which is the one place this package could still let a float touch an amount.
	for _, key := range []string{"unit_price", "amount_due"} {
		v, ok := detail[key]
		if !ok {
			t.Fatalf("the trail has no %s", key)
		}
		if _, isText := v.(string); !isText {
			t.Errorf("detail[%q] is %T, want string -- a jsonb number decodes as float64", key, v)
		}
	}
	if detail["amount_due"] != "1.50" {
		t.Errorf("detail amount_due = %v, want \"1.50\"", detail["amount_due"])
	}
	// 🔴 §4.7: no employee name reaches the trail.
	if _, ok := detail["employees"]; ok {
		t.Error("the trail carries a list of employees; an invoice needs a COUNT, not people")
	}

	// 🔴 THE HALF THIS TEST'S NAME PROMISES, AND IT USED TO BE MISSING. Everything
	// above only shows that a trail row EXISTS beside a frozen period; it would look
	// identical if the two were written in separate transactions. What makes them one
	// fate is that Close runs both inside a single WithTenant, so BREAKING THE TRAIL
	// MUST TAKE THE PERIOD WITH IT.
	//
	// The break is injected at the Trail interface -- the seam this package declares
	// for exactly this reason (§7: the interface is the consumer's) -- so the real
	// statement, the real transaction and the real rollback are all exercised. A
	// second month is used so the assertion cannot be confused with the one above.
	broken, err := NewBook(f.data, failingTrail{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewBook with a failing trail: %v", err)
	}
	doomed := Month{2025, time.April}
	if _, err := broken.Close(context.Background(), f.tenantID, doomed, f.adminID); err == nil {
		t.Fatal("Close SUCCEEDED while its audit row could not be written; a frozen, " +
			"permanent invoice with no trail is worse than no invoice")
	}
	if n := countPeriods(t, f, doomed); n != 0 {
		t.Fatalf("%d billing period(s) survived a failed audit write, want 0 -- the two "+
			"do NOT share a transaction, and this table takes no DELETE from anybody, "+
			"so the row is permanent", n)
	}
	// Positive control: the same month closes once the trail works, so the assertion
	// above is about the rollback and not about a month that could never be closed.
	if _, err := f.book.Close(context.Background(), f.tenantID, doomed, f.adminID); err != nil {
		t.Fatalf("the same month would not close with a working trail: %v -- the "+
			"rollback assertion above proves nothing", err)
	}
}

// failingTrail is the injected break for the transaction test above. It refuses every
// event, which is what a full disk, a lost connection or a revoked privilege look
// like from Close's side.
type failingTrail struct{}

func (failingTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, errors.New("billing test: the audit trail is deliberately broken")
}

// ============================================================================
// Append-only, privileges, and isolation -- all §4.3 / §4.5.
// ============================================================================

// TestBillingDB_TheFrozenRowCannotBeChangedByTheApp -- brace 1 (privilege) with a
// positive control, so a failure is the missing privilege and not invisibility.
func TestBillingDB_TheFrozenRowCannotBeChangedByTheApp(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	m := Month{2025, time.April}
	if _, err := f.book.Close(context.Background(), f.tenantID, m, f.adminID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Positive control: the app CAN read it.
	if n := countPeriods(t, f, m); n != 1 {
		t.Fatalf("positive control: the app sees %d rows for its own period, want 1", n)
	}

	for _, c := range []struct {
		name string
		sql  string
	}{
		{"UPDATE the count", `UPDATE billing_periods SET employee_count = 999 WHERE id = $1`},
		{"UPDATE the price", `UPDATE billing_periods SET unit_price = 0.01 WHERE id = $1`},
		{"DELETE the row", `DELETE FROM billing_periods WHERE id = $1`},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, c.sql, periodIDOf(t, f, m))
				return e
			})
			if err == nil {
				t.Fatalf("%s SUCCEEDED against an append-only table", c.name)
			}
			if !strContains(err.Error(), "permission denied") {
				t.Fatalf("%s failed with %v, want a permission denial", c.name, err)
			}
		})
	}
}

// TestBillingDB_TheFrozenRowCannotBeChangedByTheOWNEREither -- belt 2 (the trigger),
// which is the only thing that binds a superuser. Without this, "frozen" would hold
// only for the application role.
func TestBillingDB_TheFrozenRowCannotBeChangedByTheOWNEREither(t *testing.T) {
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; skipping the superuser append-only proof")
	}
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	m := Month{2025, time.February}
	if _, err := f.book.Close(context.Background(), f.tenantID, m, f.adminID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	id := periodIDOf(t, f, m)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	defer conn.Close(ctx)

	// Positive control: the owner bypasses RLS and SEES the row, so the FOR EACH ROW
	// trigger has a row to fire on. A WHERE matching nothing would be a silent pass.
	var before int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM billing_periods WHERE id = $1`, id).Scan(&before); err != nil {
		t.Fatalf("owner count: %v", err)
	}
	if before != 1 {
		t.Fatalf("the owner sees %d rows, want 1 (the trigger would have nothing to fire on)", before)
	}
	var isSuper bool
	if err := conn.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&isSuper); err != nil {
		t.Fatalf("role probe: %v", err)
	}
	if !isSuper {
		t.Fatalf("the migrate role is not a superuser, so this test proves nothing about the trigger")
	}

	for _, c := range []struct {
		name string
		sql  string
	}{
		{"owner UPDATE", `UPDATE billing_periods SET employee_count = 999 WHERE id = $1`},
		{"owner DELETE", `DELETE FROM billing_periods WHERE id = $1`},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := conn.Exec(ctx, c.sql, id)
			if err == nil {
				t.Fatalf("%s SUCCEEDED; the append-only trigger is the only thing that binds "+
					"a superuser and it did not fire", c.name)
			}
			if !strContains(err.Error(), "append-only table") {
				t.Fatalf("%s failed with %v, want the append-only trigger", c.name, err)
			}
		})
	}

	var after int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM billing_periods WHERE id = $1`, id).Scan(&after); err != nil {
		t.Fatalf("owner re-count: %v", err)
	}
	if after != 1 {
		t.Fatalf("row count after the blocked mutations = %d, want 1", after)
	}
}

// TestBillingDB_TheAppCannotWriteTheTerms -- 0016 narrows tenants' UPDATE **and
// INSERT** grants to the tenant's own facts, so the price a business is billed at,
// the plan that decides whether it is billed at all, and the signup instant the free
// window is measured from are the operator's on both verbs.
//
// 🔴 BOTH VERBS, BECAUSE ONE OF THEM IS NOT A NARROWING. Closing UPDATE alone leaves
// the identical power one statement away: measured before this test existed, as
// tappa_app, `INSERT INTO tenants (..., plan, price_per_employee_month) VALUES (...,
// 'founding', 0.00)` succeeded and produced a tenant that is free forever. A
// self-service signup is the natural shape that reaches an INSERT here, and it does
// not exist yet -- which is why the list is closed now rather than after it does.
func TestBillingDB_TheAppCannotWriteTheTerms(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "founding")
	ctx := context.Background()

	fresh := uuid.New()
	for _, c := range []struct {
		name string
		sql  string
		args []any
	}{
		{"UPDATE the price", `UPDATE tenants SET price_per_employee_month = 0.01 WHERE id = $1`, []any{f.tenantID}},
		{"UPDATE the plan", `UPDATE tenants SET plan = 'founding' WHERE id = $1`, []any{f.tenantID}},
		{"UPDATE the signup instant", `UPDATE tenants SET created_at = now() WHERE id = $1`, []any{f.tenantID}},
		{"INSERT a free-forever tenant", `INSERT INTO tenants (id, name, vat_number, business_type, structure, plan, price_per_employee_month)
		  VALUES ($1, 'Free Forever Ltd', $2, 'bar', 'single', 'founding', 0.00)`, []any{fresh, "VAT-" + fresh.String()}},
		{"INSERT a backdated signup", `INSERT INTO tenants (id, name, vat_number, business_type, structure, created_at)
		  VALUES ($1, 'Backdated Ltd', $2, 'bar', 'single', now() - interval '10 years')`, []any{fresh, "VAT2-" + fresh.String()}},
	} {
		t.Run(c.name, func(t *testing.T) {
			// The forging INSERTs run in the FRESH tenant's own context so RLS is
			// satisfied and the only thing left to refuse them is the privilege.
			scope := f.tenantID
			if len(c.args) > 0 {
				if id, ok := c.args[0].(uuid.UUID); ok && id != f.tenantID {
					scope = id
				}
			}
			err := f.data.WithTenant(ctx, scope, func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, c.sql, c.args...)
				return e
			})
			if err == nil {
				t.Fatalf("the app wrote a commercial term (%s)", c.name)
			}
			if !strContains(err.Error(), "permission denied") {
				t.Fatalf("%s failed with %v, want a permission denial", c.name, err)
			}
		})
	}

	// POSITIVE CONTROLS, one per verb, so the refusals above are the narrowing and
	// not a blanket revoke that would have broken every panel write and every fixture.
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE tenants SET business_type = 'cafe' WHERE id = $1`, f.tenantID)
		return e
	}); err != nil {
		t.Fatalf("the app can no longer UPDATE business_type: %v -- the narrowing is too wide", err)
	}

	ok := uuid.New()
	if err := f.data.WithTenant(ctx, ok, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			 VALUES ($1, 'Ordinary Signup Ltd', $2, 'bar', 'single', 'Europe/Malta')`,
			ok, "VAT-OK-"+ok.String())
		return e
	}); err != nil {
		t.Fatalf("the app can no longer INSERT a tenant at all: %v -- the narrowing is too "+
			"wide, and a signup that cannot create its own row is not a signup", err)
	}
	// And the row it created took the DEFAULTS rather than nothing, which is what
	// makes the narrowing costless: a new customer is founding at the list price.
	var plan, price string
	if err := f.data.WithTenant(ctx, ok, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT plan, price_per_employee_month::text FROM tenants WHERE id = $1`, ok).
			Scan(&plan, &price)
	}); err != nil {
		t.Fatalf("reading the new tenant back: %v", err)
	}
	if plan != "founding" || price != "1.50" {
		t.Fatalf("a tenant created without the terms got plan=%q price=%q, want founding / 1.50 "+
			"(the column DEFAULTs are what make the privilege unnecessary)", plan, price)
	}
}

// TestBillingDB_PeriodsAreTenantIsolated -- §4.5. The probe identifies A's row by a
// NON-scope column (its id), never by tenant_id, so a zero result is RLS and not a
// WHERE clause (CLAUDE.md §6: an isolation test must not carry the filter a
// production query is required to carry).
func TestBillingDB_PeriodsAreTenantIsolated(t *testing.T) {
	a := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	b := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	a.hire(t, "A Person", "active", at("2020-02-01T09:00:00Z"), nil)

	m := Month{2025, time.January}
	if _, err := a.book.Close(context.Background(), a.tenantID, m, a.adminID); err != nil {
		t.Fatalf("A closes its period: %v", err)
	}
	id := periodIDOf(t, a, m)

	count := func(f *fixture) int {
		t.Helper()
		var n int
		err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			// NO tenant_id PREDICATE, on purpose.
			return tx.QueryRow(ctx, `SELECT count(*) FROM billing_periods WHERE id = $1`, id).Scan(&n)
		})
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		return n
	}
	if self := count(a); self != 1 {
		t.Fatalf("positive control: A's own context sees %d rows, want 1 (the isolation check "+
			"below would be vacuous)", self)
	}
	if cross := count(b); cross != 0 {
		t.Fatalf("RLS FAILED: B's context read %d of A's billing periods, want 0", cross)
	}

	// The write side: B cannot forge a row stamped with A's tenant_id.
	err := b.data.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO billing_periods
			   (tenant_id, period_month, period_from, period_to, timezone, plan, free_period,
			    employee_count, unit_price, closed_by)
			 VALUES ($1, '2025-02-01', now() - interval '60 days', now() - interval '30 days',
			         'Europe/Malta', 'standard', false, 999, 0.01, $2)`,
			a.tenantID, a.adminID)
		return e
	})
	if err == nil {
		t.Fatal("B forged a billing period stamped with A's tenant_id")
	}
	if !strContains(err.Error(), "row-level security") {
		t.Fatalf("the forgery failed with %v, want the RLS WITH CHECK", err)
	}

	// And B's OWN insert of the same shape succeeds, so the refusal above is the
	// tenant mismatch and not the statement being invalid.
	if err := b.data.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO billing_periods
			   (tenant_id, period_month, period_from, period_to, timezone, plan, free_period,
			    employee_count, unit_price, closed_by)
			 VALUES ($1, '2025-02-01', now() - interval '60 days', now() - interval '30 days',
			         'Europe/Malta', 'standard', false, 1, 1.50, $2)`,
			b.tenantID, b.adminID)
		return e
	}); err != nil {
		t.Fatalf("B's own insert was refused too (%v); the control does not control", err)
	}
}

// TestBillingDB_UnknownTenantIsAnErrorNotAnEmptyInvoice -- an invoice for nobody must
// not render as a quiet zero (§4.6's habit).
func TestBillingDB_UnknownTenantIsAnErrorNotAnEmptyInvoice(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	// A tenant id that exists nowhere: the context is established for it, so RLS
	// hides everything and the scope CTE finds no row.
	ghost := uuid.New()
	if _, err := f.book.Preview(context.Background(), ghost, Month{2025, time.January}); !errors.Is(err, ErrUnknownTenant) {
		t.Fatalf("Preview for an unknown tenant gave %v, want ErrUnknownTenant", err)
	}
}

// TestBillingDB_HistoryIsNewestFirstAndScopedToTheTenant.
func TestBillingDB_HistoryIsNewestFirstAndScopedToTheTenant(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	other := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	if _, err := other.book.Close(context.Background(), other.tenantID, Month{2025, time.January}, other.adminID); err != nil {
		t.Fatalf("the other tenant closes a period: %v", err)
	}

	want := []Month{{2024, time.December}, {2025, time.January}, {2025, time.February}}
	for _, m := range want {
		if _, err := f.book.Close(context.Background(), f.tenantID, m, f.adminID); err != nil {
			t.Fatalf("Close %s: %v", m, err)
		}
	}

	got, truncated, err := f.book.History(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if truncated {
		t.Errorf("three periods reported as truncated against a cap of %d", HistoryCap)
	}
	if len(got) != len(want) {
		t.Fatalf("History returned %d periods, want %d (another tenant's rows must not appear)", len(got), len(want))
	}
	for i, m := range []Month{{2025, time.February}, {2025, time.January}, {2024, time.December}} {
		if got[i].Month != m {
			t.Fatalf("History[%d] = %v, want %v (newest first)", i, got[i].Month, m)
		}
		if !got[i].Frozen {
			t.Fatalf("History[%d] does not report itself as frozen", i)
		}
		if got[i].TenantName == "" || got[i].ID == uuid.Nil {
			t.Fatalf("History[%d] carries no addressee or no row id (name=%q id=%s)",
				i, got[i].TenantName, got[i].ID)
		}
	}
}

// TestBillingDB_DefaultCurrencyMatchesTheColumnDefault -- the Go constant and the
// schema DEFAULT are two statements of one fact, and this is what keeps them equal.
// A hand-copied constant nothing checks is the class of stale fact this repository
// keeps finding.
func TestBillingDB_DefaultCurrencyMatchesTheColumnDefault(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	var def string
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT column_default FROM information_schema.columns
			  WHERE table_name = 'billing_periods' AND column_name = 'currency'`).Scan(&def)
	})
	if err != nil {
		t.Fatalf("reading the column default: %v", err)
	}
	// The default is stored as a typed literal, e.g. 'EUR'::text.
	if !strContains(def, "'"+DefaultCurrency+"'") {
		t.Fatalf("billing_periods.currency DEFAULT is %s but billing.DefaultCurrency is %q",
			def, DefaultCurrency)
	}
}

// TestBillingDB_MoneyScaleMatchesTheSchema -- moneyScale and the numeric(_, 2)
// columns are the same decision written twice.
func TestBillingDB_MoneyScaleMatchesTheSchema(t *testing.T) {
	f := newFixture(t, *at("2020-01-01T00:00:00Z"), maltaZone, "standard")
	cases := []struct{ table, column string }{
		{"billing_periods", "unit_price"},
		{"billing_periods", "amount_due"},
		{"tenants", "price_per_employee_month"},
	}
	for _, c := range cases {
		t.Run(c.table+"."+c.column, func(t *testing.T) {
			var scale int
			var dataType string
			err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx,
					`SELECT numeric_scale, data_type FROM information_schema.columns
					  WHERE table_name = $1 AND column_name = $2`, c.table, c.column).Scan(&scale, &dataType)
			})
			if err != nil {
				t.Fatalf("reading the column: %v", err)
			}
			if dataType != "numeric" {
				t.Fatalf("%s.%s is %s, want numeric -- money is never float (§6)", c.table, c.column, dataType)
			}
			if scale != moneyScale {
				t.Fatalf("%s.%s has scale %d but billing.moneyScale is %d", c.table, c.column, scale, moneyScale)
			}
		})
	}
}

func periodIDOf(t *testing.T, f *fixture, m Month) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id FROM billing_periods WHERE tenant_id = $1 AND period_month = $2`,
			f.tenantID, m.date()).Scan(&id)
	})
	if err != nil {
		t.Fatalf("period id: %v", err)
	}
	return id
}

func strContains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
