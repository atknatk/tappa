package db

// operatorbilling_test.go -- migration 00032 (M10 OP-12, phase A): the operator reads ONE
// tenant's billing months through op_read_tenant_billing. The catalogue half (the signature,
// the grants and the definer's EXECUTE set, the Down, the precondition, the temp-table
// shadow) and the behaviour half (op_begin_read's new kind, the read and its agreement with
// the tenant's own path, the Go accessor) of ADR 0021 §6's list, for the new op_read_*.
//
// THE CORE IS ONE TEST: TestOpReadTenantBilling_EveryMonthIsTheTenantsOwnFigure. The read is
// a third copy of the glue around 00016's five billing functions (db/queries/billing.sql's
// Preview and Close are the other two), and that test is the wire that holds the copies
// equal: every month the read returns is asked of the tenant's own path -- GetBillingPeriod,
// or PreviewBillingPeriod where no frozen row exists, through the GENERATED store, as
// tappa_app in the tenant's context -- and compared field by field.
//
// HOW EACH TEST TOUCHES THE SHARED DATABASE -- operatoraudit_test.go's three shapes:
//   - most tests run in opTx: one rolled-back REPEATABLE READ transaction as the owner,
//     identities switched with SET LOCAL SESSION AUTHORIZATION, the operator-tables lock
//     taken EXCLUSIVE. Every tenant, location, admin, employee and billing_periods row they
//     need is written inside that transaction and goes with it;
//   - the read side is reached WITHOUT a commit through a ticket the owner writes with
//     created_xact naming a transaction that really committed (opCommittedXact) and a hash
//     computed HERE, in Go (opBillingTicketHash), so a change in what the function hashes
//     turns a test red instead of following along;
//   - two tests need a COMMIT because a commit is their subject
//     (TestOpReadTenantBilling_TwoPhaseLifecycle, TestTenantBilling_OnThePoolTheTwoPhasesAreTwoTransactions).
//     They take the lock SHARED through opLiveFixture and write no tenant data: they read a
//     tenant that already exists (one without employees, so the read is cheap) or an id no
//     tenant has. What they leave per run, by construction: one disabled account each, its
//     revoked sessions, and the 'read' rows they committed (the lifecycle one, the pool test
//     three) -- operator_audit_log is append-only and its foreign keys keep the account and
//     the sessions those rows name.
//
// 🔴 NO TEST HERE COMMITS A TENANT, AN EMPLOYEE OR A billing_periods ROW. billing_periods is
// append-only (00016): a committed fixture month would be permanent.
//
// 🔴 NO TEST ASSUMES WHICH MONTH IT IS (the agent-brief's "22:30" lesson). The fixtures place
// their months in the tenant's OWN zone, relative to the month the database's wall clock is
// in THERE, and the assertions name months by their dates; the uncertain instant -- a month
// boundary between two statements -- is bracketed (read before and after) wherever a test
// names "the month the tenant is in". The far zones Etc/GMT-14 and Etc/GMT+12 are 26 hours
// apart, so at any instant at least one of them is on another calendar DAY than UTC (a
// PREMISE the core test checks rather than assumes) -- but not on another MONTH: that happens
// only within about a day of a month boundary, so a read that took "the month the tenant is
// in" from UTC would agree with the zone's on almost every run. The wall clock cannot be moved,
// so TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant moves the read's clock
// instead: it runs the read's own query, taken from the catalogue, at chosen instants either
// side of a UTC month boundary. The signup month has no such limit -- it is a stored instant --
// and the core test places signups where a zone's month and UTC's differ.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// op00032Read is the function 00032 creates, by its exact catalogue identity: the session,
// the RAW ticket, the tenant and the page -- no actor -- and the fixed column list of ADR 0021
// §2 ii. closed_by is not in it, no employee column is in it, and the two money columns are
// numeric (RETURNS TABLE drops numeric's type modifier: the scale is the expression's).
var op00032Read = struct{ name, args, result string }{
	"op_read_tenant_billing",
	"p_session text, p_ticket text, p_tenant_id uuid, p_page_number integer",
	"TABLE(tenant_id uuid, tenant_name text, period_month date, after_signup boolean, frozen boolean, " +
		"period_from timestamp with time zone, period_to timestamp with time zone, period_timezone text, plan text, " +
		"first_chargeable_month date, free_period boolean, employee_count integer, unstamped_employees integer, " +
		"unit_price numeric, currency text, amount_due numeric, closed_at timestamp with time zone, period_has_ended boolean)",
}

const (
	tenantBillingRefusal = "op_read_tenant_billing: read refused"
	op00032File          = "00032_read_billing_from_the_operator.sql"

	// The three zones of the fixtures: the product's own, and the two whose month starts lie
	// furthest from UTC's on either side.
	opZoneMalta = "Europe/Malta"
	opZoneEast  = "Etc/GMT-14" // UTC+14 (POSIX sign)
	opZoneWest  = "Etc/GMT+12" // UTC-12
)

// opKindsAt32 is the closed set of read kinds at 00032 (opAtVersion's argument).
var opKindsAt32 = []string{"legal_versions", "tenants", "tenant_detail", "tenant_plaques", "operator_audit", "tenant_billing"}

// opBillingDefinerTenants / Employees / Periods are tappa_opdefiner's SELECT columns at 00032,
// in attnum order (and, after its Down, the first two are 00029's).
const (
	opBillingDefinerTenants   = "id,name,business_type,plan,timezone,created_at,price_per_employee_month"
	opBillingDefinerEmployees = "tenant_id,status,activated_at,deactivated_at"
	opBillingDefinerPeriods   = "tenant_id,period_month,period_from,period_to,timezone,plan,free_period,employee_count," +
		"unstamped_employees,unit_price,currency,amount_due,closed_at"
	opDefinerTenants29   = "id,name,business_type,plan,created_at"
	opDefinerEmployees29 = "tenant_id,status"
)

// ------------------------------------------------------------------ helpers --

// opBillingCanonical is jsonb's text of the read's parameter object: keys shorter-first
// (tenant_id, page_number), the uuid in its canonical lower-case text.
func opBillingCanonical(id uuid.UUID, page int) string {
	return fmt.Sprintf(`{"tenant_id": "%s", "page_number": %d}`, id.String(), page)
}

func opBillingTicketHash(raw string, id uuid.UUID, page int) string {
	sum := sha256.Sum256([]byte(raw + opBillingCanonical(id, page)))
	return hex.EncodeToString(sum[:])
}

// opBillingParams is the read's parameter object as JSON text (Go's spelling; the database
// rebuilds the hashed text itself).
func opBillingParams(id string, page any) string {
	b, _ := json.Marshal(map[string]any{"tenant_id": id, "page_number": page})
	return string(b)
}

// opForgeBilling forges a billing ticket bound to (tenant, page) and returns the raw ticket.
func opForgeBilling(t *testing.T, ctx context.Context, tx pgx.Tx, session, admin, tenant uuid.UUID, page int, xact string) string {
	t.Helper()
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, admin, tenantBillingReadKind, opBillingTicketHash(raw, tenant, page), &tenant, xact, "30 seconds")
	return raw
}

// opReadBilling runs the PRODUCT's phase two (readTenantBilling: the shipped statement and
// the shipped scan) as tappa_operator inside a savepoint of tx; on success the savepoint is
// released, so the consumption stays in tx. The error is the product's (operatorErr).
func opReadBilling(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket string, tenant uuid.UUID, page int) (TenantBillingTimeline, error) {
	t.Helper()
	var out TenantBillingTimeline
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		out, e = readTenantBilling(ctx, sp, hash, readTicket{v: &ticket}, tenant, int32(page))
		return e
	})
	return out, err
}

// opScanBilling runs the shipped statement with a scan written HERE and returns the raw
// rows' months and the database's error untouched (these tests want the SQLSTATE).
func opScanBilling(ctx context.Context, q opQuerier, hash, ticket, tenant, page any) ([]time.Time, error) {
	rows, err := q.Query(ctx, readTenantBillingSQL, hash, ticket, tenant, page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m, ok := vals[2].(time.Time)
		if !ok {
			return nil, fmt.Errorf("period_month is %T", vals[2])
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// opRawBilling runs opScanBilling as tappa_operator inside a savepoint of tx.
func opRawBilling(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket, tenant, page any) ([]time.Time, error) {
	t.Helper()
	var out []time.Time
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		out, e = opScanBilling(ctx, sp, hash, ticket, tenant, page)
		return e
	})
	return out, err
}

// opBillingFixture is one tenant the owner writes inside the test's transaction: a zone, a
// plan, a price, a signup instant placed in the tenant's OWN zone, one location and one
// owner-role admin (billing_periods.closed_by names a same-tenant admin).
type opBillingFixture struct {
	id    uuid.UUID
	name  string
	zone  string
	admin uuid.UUID
	loc   uuid.UUID
	// m0 is the local month the tenant was in when the fixture was written (a date at UTC
	// midnight); signup is the local month it signed up in.
	m0, signup time.Time
	created    time.Time
}

// month is the fixture's local month k months before m0.
func (f opBillingFixture) month(k int) time.Time { return f.m0.AddDate(0, -k, 0) }

// opLocalMonth is the local month the database's wall clock is in, in zone.
func opLocalMonth(t *testing.T, ctx context.Context, q opQuerier, zone string) time.Time {
	t.Helper()
	var m time.Time
	if err := q.QueryRow(ctx, `SELECT date_trunc('month', clock_timestamp() AT TIME ZONE $1)::date`, zone).Scan(&m); err != nil {
		t.Fatalf("the local month in %s: %v", zone, err)
	}
	return m
}

// opMonthBounds is [from, to) of a local month, by 00016's own function (the owner calls it).
func opMonthBounds(t *testing.T, ctx context.Context, q opQuerier, month time.Time, zone string) (from, to time.Time) {
	t.Helper()
	if err := q.QueryRow(ctx, `SELECT public.tappa_local_month_start($1::date, $2),
	                                  public.tappa_local_month_start(($1::date + interval '1 month')::date, $2)`,
		month, zone).Scan(&from, &to); err != nil {
		t.Fatalf("the bounds of %s in %s: %v", month.Format("2006-01"), zone, err)
	}
	return from, to
}

// opBillingTenant writes the fixture tenant. signupAgo is how many local months before the
// current one it signed up in -- on the 15th at noon, local time, an instant in the same month
// in every zone (opBillingTenantAt places it elsewhere).
func opBillingTenant(t *testing.T, ctx context.Context, tx pgx.Tx, name, zone, plan, price string, signupAgo int) opBillingFixture {
	t.Helper()
	return opBillingTenantAt(t, ctx, tx, name, zone, plan, price, signupAgo, "14 days 12 hours")
}

// opBillingTenantAt is opBillingTenant with the signup instant at local wall time `into` after
// the start of its local month (an interval, e.g. "30 minutes" or "1 month -30 minutes").
func opBillingTenantAt(t *testing.T, ctx context.Context, tx pgx.Tx, name, zone, plan, price string, signupAgo int, into string) opBillingFixture {
	t.Helper()
	f := opBillingFixture{id: uuid.New(), name: name, zone: zone, admin: uuid.New(), loc: uuid.New()}
	f.m0 = opLocalMonth(t, ctx, tx, zone)
	f.signup = f.month(signupAgo)
	if err := tx.QueryRow(ctx, `SELECT ($1::date::timestamp + $3::interval) AT TIME ZONE $2`, f.signup, zone, into).Scan(&f.created); err != nil {
		t.Fatalf("the signup instant: %v", err)
	}
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure, plan, timezone, price_per_employee_month, created_at)
		  VALUES ($1, $2, $3, 'restaurant', 'single', $4, $5, $6::numeric, $7)`,
			[]any{f.id, name, "VAT-OP12-" + f.id.String(), plan, zone, price, f.created}},
		{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'op12 door')`, []any{f.loc, f.id}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		  VALUES ($1, $2, 'op12 owner', $3, $4, 'owner', 'active')`,
			[]any{f.admin, f.id, "op12-" + f.admin.String()[:12] + "@example.test", opFakeDigest("b")}},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("billing fixture %s: %s: %v", name, strings.Fields(s.sql)[2], err)
		}
	}
	return f
}

// opHire writes one employee of the fixture tenant with the status and stamps given.
func opHire(t *testing.T, ctx context.Context, tx pgx.Tx, f opBillingFixture, name, status string, activated, deactivated *time.Time) {
	t.Helper()
	if _, err := tx.Exec(ctx, `INSERT INTO employees (tenant_id, location_id, full_name, status, activated_at, deactivated_at)
	                           VALUES ($1, $2, $3, $4, $5, $6)`, f.id, f.loc, "op12 "+name, status, activated, deactivated); err != nil {
		t.Fatalf("hire %s at %s: %v", name, f.name, err)
	}
}

// opCloseMonth freezes one month through the PRODUCT's statement (store.CloseBillingPeriod), as
// tappa_app in the tenant's context, closed by the fixture's admin.
func opCloseMonth(t *testing.T, ctx context.Context, tx pgx.Tx, f opBillingFixture, month time.Time) {
	t.Helper()
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.id.String()); err != nil {
			return err
		}
		_, err := store.New(sp).CloseBillingPeriod(ctx, store.CloseBillingPeriodParams{
			ClosedBy: f.admin, PeriodMonth: pgtype.Date{Time: month, Valid: true}, TenantID: f.id})
		return err
	}); err != nil {
		t.Fatalf("close %s at %s: %v", month.Format("2006-01"), f.name, err)
	}
}

// opTenantPath is what the tenant's OWN path answers for one month: the frozen row, if
// GetBillingPeriod finds one, and the live preview (always asked, so a test can show a frozen
// month and today's figures differ).
type opTenantPath struct {
	frozen  bool
	get     store.GetBillingPeriodRow
	preview store.PreviewBillingPeriodRow
}

// opBillingTenantPath asks the GENERATED store, as tappa_app in the tenant's context -- row
// level security on, the tenant's own statements -- inside a savepoint of tx.
func opBillingTenantPath(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, month pgtype.Date) opTenantPath {
	t.Helper()
	var p opTenantPath
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
			return err
		}
		q := store.New(sp)
		g, err := q.GetBillingPeriod(ctx, store.GetBillingPeriodParams{TenantID: tenant, PeriodMonth: month})
		switch {
		case err == nil:
			p.frozen, p.get = true, g
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return fmt.Errorf("GetBillingPeriod: %w", err)
		}
		p.preview, err = q.PreviewBillingPeriod(ctx, store.PreviewBillingPeriodParams{PeriodMonth: month, TenantID: tenant})
		if err != nil {
			return fmt.Errorf("PreviewBillingPeriod: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("the tenant's own path for %s, %s: %v", tenant, month.Time.Format("2006-01"), err)
	}
	return p
}

// opNumericEqual reports whether two numerics are the same NUMBER, exactly (mantissa x 10^exp,
// compared as integers at a common exponent -- no float on the way).
func opNumericEqual(a, b pgtype.Numeric) bool {
	if !a.Valid || !b.Valid || a.NaN || b.NaN || a.InfinityModifier != pgtype.Finite || b.InfinityModifier != pgtype.Finite {
		return false
	}
	ai, bi := a.Int, b.Int
	if ai == nil {
		ai = big.NewInt(0)
	}
	if bi == nil {
		bi = big.NewInt(0)
	}
	e := min(a.Exp, b.Exp)
	scale := func(i *big.Int, exp int32) *big.Int {
		return new(big.Int).Mul(i, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp-e)), nil))
	}
	return scale(ai, a.Exp).Cmp(scale(bi, b.Exp)) == 0
}

// opNumericText is a numeric as exact decimal text, for messages.
func opNumericText(n pgtype.Numeric) string {
	v, err := n.Value()
	if err != nil || v == nil {
		return "<nil>"
	}
	return fmt.Sprint(v)
}

func opTimeEq(a *time.Time, b time.Time) bool { return a != nil && a.Equal(b) }
func opStrEq(a *string, b string) bool        { return a != nil && *a == b }
func opBoolEq(a *bool, b bool) bool           { return a != nil && *a == b }
func opIntEq(a *int32, b int32) bool          { return a != nil && *a == b }

// opMonthDiffs compares one month of the operator's read with the tenant's own path and
// returns every field that differs. txStart and wall bracket the live preview's frozen clock
// (PreviewBillingPeriod's period_has_ended reads now(), the read's clock_timestamp()): a
// month that ended between them is the one place the two may differ, and is not compared.
func opMonthDiffs(m TenantBillingMonth, name string, p opTenantPath, txStart, wall time.Time) []string {
	var d []string
	bad := func(f string, a ...any) { d = append(d, fmt.Sprintf(f, a...)) }
	switch {
	case p.frozen:
		g := p.get
		if !m.Frozen || !m.AfterSignup || !opBoolEq(m.HasEnded, true) || m.FirstChargeableMonth.Valid {
			bad("frozen=%v after_signup=%v has_ended=%s first_chargeable valid=%v; want a frozen row (true, true, true, not valid)",
				m.Frozen, m.AfterSignup, opDeref(m.HasEnded), m.FirstChargeableMonth.Valid)
		}
		if name != g.TenantName {
			bad("tenant name %q, GetBillingPeriod %q", name, g.TenantName)
		}
		if !opTimeEq(m.From, g.PeriodFrom) || !opTimeEq(m.To, g.PeriodTo) {
			bad("period [%s, %s), GetBillingPeriod [%s, %s)", opDeref(m.From), opDeref(m.To), g.PeriodFrom, g.PeriodTo)
		}
		if !opStrEq(m.Zone, g.Timezone) || !opStrEq(m.Plan, g.Plan) || !opBoolEq(m.Free, g.FreePeriod) {
			bad("zone/plan/free %s/%s/%s, GetBillingPeriod %s/%s/%v", opDeref(m.Zone), opDeref(m.Plan), opDeref(m.Free), g.Timezone, g.Plan, g.FreePeriod)
		}
		if !opIntEq(m.EmployeeCount, g.EmployeeCount) || !opIntEq(m.UnstampedEmployees, g.UnstampedEmployees) {
			bad("counts %s/%s, GetBillingPeriod %d/%d", opDeref(m.EmployeeCount), opDeref(m.UnstampedEmployees), g.EmployeeCount, g.UnstampedEmployees)
		}
		if !opNumericEqual(m.UnitPrice, g.UnitPrice) || !opNumericEqual(m.AmountDue, g.AmountDue) {
			bad("price/amount %s/%s, GetBillingPeriod %s/%s", opNumericText(m.UnitPrice), opNumericText(m.AmountDue), opNumericText(g.UnitPrice), opNumericText(g.AmountDue))
		}
		if !opStrEq(m.Currency, g.Currency) || !opTimeEq(m.ClosedAt, g.ClosedAt) {
			bad("currency/closed_at %s/%s, GetBillingPeriod %s/%s", opDeref(m.Currency), opDeref(m.ClosedAt), g.Currency, g.ClosedAt)
		}
	case p.preview.PeriodIsAfterSignup:
		v := p.preview
		if m.Frozen || !m.AfterSignup || m.Currency != nil || m.ClosedAt != nil {
			bad("frozen=%v after_signup=%v currency=%s closed_at=%s; want a live row (false, true, NULL, NULL)",
				m.Frozen, m.AfterSignup, opDeref(m.Currency), opDeref(m.ClosedAt))
		}
		if name != v.TenantName {
			bad("tenant name %q, PreviewBillingPeriod %q", name, v.TenantName)
		}
		if !opTimeEq(m.From, v.PeriodFrom) || !opTimeEq(m.To, v.PeriodTo) {
			bad("period [%s, %s), PreviewBillingPeriod [%s, %s)", opDeref(m.From), opDeref(m.To), v.PeriodFrom, v.PeriodTo)
		}
		if !opStrEq(m.Zone, v.Timezone) || !opStrEq(m.Plan, v.Plan) || !opBoolEq(m.Free, v.FreePeriod) {
			bad("zone/plan/free %s/%s/%s, PreviewBillingPeriod %s/%s/%v", opDeref(m.Zone), opDeref(m.Plan), opDeref(m.Free), v.Timezone, v.Plan, v.FreePeriod)
		}
		if !m.FirstChargeableMonth.Valid || !m.FirstChargeableMonth.Time.Equal(v.FirstChargeableMonth.Time) {
			bad("first chargeable month %v, PreviewBillingPeriod %v", m.FirstChargeableMonth.Time, v.FirstChargeableMonth.Time)
		}
		if !opIntEq(m.EmployeeCount, v.EmployeeCount) || !opIntEq(m.UnstampedEmployees, v.UnstampedEmployees) {
			bad("counts %s/%s, PreviewBillingPeriod %d/%d", opDeref(m.EmployeeCount), opDeref(m.UnstampedEmployees), v.EmployeeCount, v.UnstampedEmployees)
		}
		if !opNumericEqual(m.UnitPrice, v.UnitPrice) || !opNumericEqual(m.AmountDue, v.AmountDue) {
			bad("price/amount %s/%s, PreviewBillingPeriod %s/%s", opNumericText(m.UnitPrice), opNumericText(m.AmountDue), opNumericText(v.UnitPrice), opNumericText(v.AmountDue))
		}
		boundary := v.PeriodTo.After(txStart) && !v.PeriodTo.After(wall)
		if !boundary && !opBoolEq(m.HasEnded, v.PeriodHasEnded) {
			bad("has_ended %s, PreviewBillingPeriod %v", opDeref(m.HasEnded), v.PeriodHasEnded)
		}
	default:
		if m.Frozen || m.AfterSignup || m.From != nil || m.To != nil || m.Zone != nil || m.Plan != nil || m.FirstChargeableMonth.Valid ||
			m.Free != nil || m.EmployeeCount != nil || m.UnstampedEmployees != nil || m.UnitPrice.Valid || m.Currency != nil ||
			m.AmountDue.Valid || m.ClosedAt != nil || m.HasEnded != nil {
			bad("a month before sign-up carries a figure or a state: %+v", m)
		}
		if name != p.preview.TenantName {
			bad("tenant name %q, PreviewBillingPeriod %q", name, p.preview.TenantName)
		}
	}
	return d
}

// opConsecutiveDesc reports whether months are consecutive local months, newest first,
// starting at newest.
func opConsecutiveDesc(months []time.Time, newest time.Time) bool {
	for i, m := range months {
		if !m.Equal(newest.AddDate(0, -i, 0)) {
			return false
		}
	}
	return true
}

// opMonthTexts is a page as comparable lines: every field of every month, times in UTC to the
// microsecond, numerics as exact decimal text.
func opMonthTexts(tl TenantBillingTimeline) []string {
	tm := func(p *time.Time) string {
		if p == nil {
			return "<nil>"
		}
		return p.UTC().Format("2006-01-02T15:04:05.000000Z")
	}
	var out []string
	for _, m := range tl.Months {
		out = append(out, strings.Join([]string{m.Month.Time.Format("2006-01-02"), strconv.FormatBool(m.AfterSignup),
			strconv.FormatBool(m.Frozen), tm(m.From), tm(m.To), opDeref(m.Zone), opDeref(m.Plan),
			fmt.Sprint(m.FirstChargeableMonth.Valid, m.FirstChargeableMonth.Time.Format("2006-01-02")), opDeref(m.Free),
			opDeref(m.EmployeeCount), opDeref(m.UnstampedEmployees), opNumericText(m.UnitPrice), opDeref(m.Currency),
			opNumericText(m.AmountDue), tm(m.ClosedAt), opDeref(m.HasEnded)}, "|"))
	}
	return out
}

func opMonthsOf(tl TenantBillingTimeline) []time.Time {
	out := make([]time.Time, 0, len(tl.Months))
	for _, m := range tl.Months {
		out = append(out, m.Month.Time)
	}
	return out
}

// opBillingScales reads, through a forged ticket and the function itself, the SCALE of every
// non-NULL money value of a page, in SQL (pgx carries no scale for a zero -- a numeric with no
// digits arrives as 0 x 10^0 -- so the Go side cannot see the scale of 0.00).
func opBillingScales(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, session, admin, tenant uuid.UUID, page int, xact string) map[string][2]*int32 {
	t.Helper()
	raw := opForgeBilling(t, ctx, tx, session, admin, tenant, page, xact)
	out := map[string][2]*int32{}
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, `SELECT period_month::text, scale(unit_price), scale(amount_due)
		                              FROM public.op_read_tenant_billing($1, $2, $3, $4)`, hash, raw, tenant, page)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m string
			var p, a *int32
			if err := rows.Scan(&m, &p, &a); err != nil {
				return err
			}
			out[m] = [2]*int32{p, a}
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read the scales: %v", err)
	}
	return out
}

// --------------------------------------------------------------- catalogue --

// TestOperator00032_TheFunctionsAndTheirExactSignatures pins what ADR 0021 §6's generic pins
// leave open for the read 00032 creates and the two it replaces: op_read_tenant_billing's
// exact argument list and result (no closed_by, no employee column), the owner, SECURITY
// DEFINER, proconfig, one overload, EXECUTE for tappa_operator alone; op_begin_read and
// op_read_audit keep their identities (op_read_audit's is 00031's, column for column); the
// forward, frozen-clock and consumption scans walked them and raise nothing; the ticket kind
// CHECK at HEAD is exactly the six kinds, validated; and, read from the catalogue, NO column
// of the result is a float (float4, float8) or money, and the two money columns are numeric.
func TestOperator00032_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	var args, result, owner string
	var retset, secdef bool
	var config []string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
		       pg_get_userbyid(p.proowner), p.proretset, p.prosecdef, p.proconfig
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.proname = $1`, op00032Read.name).Scan(&args, &result, &owner, &retset, &secdef, &config); err != nil {
		t.Fatalf("%s: %v", op00032Read.name, err)
	}
	if args != op00032Read.args {
		t.Errorf("%s(%s), want (%s)", op00032Read.name, args, op00032Read.args)
	}
	if result != op00032Read.result {
		t.Errorf("%s returns %s,\n want %s", op00032Read.name, result, op00032Read.result)
	}
	if owner != "tappa_opdefiner" || !secdef || !retset {
		t.Errorf("owner %s, SECURITY DEFINER %v, set-returning %v; want tappa_opdefiner, true, true", owner, secdef, retset)
	}
	if len(config) != 1 || config[0] != "search_path=pg_catalog, pg_temp" {
		t.Errorf("proconfig %v", config)
	}
	for _, fn := range []string{op00032Read.name, "op_begin_read", "op_read_audit"} {
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, fn); n != 1 {
			t.Errorf("%d functions named %s; an overload would be a second door", n, fn)
		}
		for who, want := range map[string]bool{"public": false, "tappa_app": false, "tappa_resolver": false, "tappa_operator": true} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, p.oid, 'EXECUTE') FROM pg_proc p
			                             JOIN pg_namespace n ON n.oid = p.pronamespace
			                            WHERE n.nspname = 'public' AND p.proname = $2`, who, fn).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may != want {
				t.Errorf("has_function_privilege(%s, %s, EXECUTE) = %v, want %v", who, fn, may, want)
			}
		}
	}
	if got := opIdentity(t, ctx, tx, "op_begin_read"); got != "p_session text, p_kind text, p_params jsonb -> text" {
		t.Errorf("op_begin_read is now %s; the replacement keeps 00027's identity", got)
	}
	if got, want := opIdentity(t, ctx, tx, "op_read_audit"), op00031Read.args+" -> "+op00031Read.result; got != want {
		t.Errorf("op_read_audit is now %s; the replacement keeps 00031's identity %s", got, want)
	}

	// The result's types, from the catalogue: no float, and the money columns numeric.
	rows, err := tx.Query(ctx, `
		SELECT a.name, a.type::regtype::text
		  FROM pg_proc p,
		       unnest(p.proallargtypes, p.proargmodes, p.proargnames) AS a(type, mode, name)
		 WHERE p.oid = 'public.op_read_tenant_billing(text, text, uuid, integer)'::regprocedure AND a.mode = 't'`)
	if err != nil {
		t.Fatalf("read the result columns: %v", err)
	}
	types := map[string]string{}
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatal(err)
		}
		types[name] = typ
	}
	rows.Close()
	if len(types) != 18 {
		t.Fatalf("anti-vacuity: %d result columns read, want 18", len(types))
	}
	for name, typ := range types {
		if typ == "real" || typ == "double precision" || typ == "money" {
			t.Errorf("result column %s is %s; money and counts are never a float (CLAUDE.md §6)", name, typ)
		}
	}
	for _, c := range []string{"unit_price", "amount_due"} {
		if types[c] != "numeric" {
			t.Errorf("result column %s is %q, want numeric", c, types[c])
		}
	}
	if _, ok := types["closed_by"]; ok {
		t.Error("the result carries closed_by (the tenant administrator who closed the month)")
	}

	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for _, fn := range []string{op00032Read.name, "op_begin_read", "op_read_audit"} {
		if !slices.Contains(names, fn) {
			t.Errorf("anti-vacuity: the forward scan did not walk %s", fn)
		}
		for _, f := range findings {
			if strings.Contains(f, fn+"(") {
				t.Error(f)
			}
		}
	}
	clock, _, err := opFrozenClockFindings(ctx, tx)
	if err != nil {
		t.Fatalf("frozen-clock scan: %v", err)
	}
	for _, f := range clock {
		t.Error(f)
	}
	consumption, read, err := opReadConsumptionFindings(ctx, tx)
	if err != nil {
		t.Fatalf("consumption scan: %v", err)
	}
	if !slices.Contains(read, op00032Read.name) {
		t.Errorf("anti-vacuity: the consumption scan did not read %s (it read %v)", op00032Read.name, read)
	}
	for _, f := range consumption {
		if strings.Contains(f, op00032Read.name+"(") {
			t.Error(f)
		}
	}
	def, valid := opConstraint(t, ctx, tx, "operator_read_tickets", "operator_read_tickets_kind_check")
	if def != opKindCheckDef(opKindsAt32) || !valid {
		t.Errorf("operator_read_tickets_kind_check is %s (validated %v),\n want %s, validated", def, valid, opKindCheckDef(opKindsAt32))
	}
}

// TestOperator00032_TheDefinerReadsBillingAndTheOperatorDoesNot is OP-12's "who reads what"
// on the catalogue and as statements:
//   - tappa_opdefiner's SELECT on tenants, employees and billing_periods is EXACTLY the lists
//     the read needs (00029's columns and 00032's) -- not closed_by, id or created_at of a
//     frozen month, not a name, address or id of an employee; it holds no INSERT, UPDATE,
//     DELETE or TRUNCATE on any of the three (billing_periods is append-only; the definer
//     closes nothing);
//   - as tappa_opdefiner, reading closed_by, an employee's name or address, or writing a
//     frozen month is 42501; CONTROL: the granted columns read;
//   - tappa_operator holds nothing on the three tables and reading each directly -- SELECT or
//     COPY -- is 42501;
//     tappa_app may not call the read (42501) and keeps its own EXECUTE on 00016's five
//     functions (00032 grants beside it and revokes nothing of it).
func TestOperator00032_TheDefinerReadsBillingAndTheOperatorDoesNot(t *testing.T) {
	ctx, tx := opTx(t)
	for _, c := range []struct{ role, table, priv, want string }{
		{"tappa_opdefiner", "tenants", "SELECT", opBillingDefinerTenants},
		{"tappa_opdefiner", "employees", "SELECT", opBillingDefinerEmployees},
		{"tappa_opdefiner", "billing_periods", "SELECT", opBillingDefinerPeriods},
		{"tappa_opdefiner", "tenants", "INSERT", ""},
		{"tappa_opdefiner", "tenants", "UPDATE", ""},
		{"tappa_opdefiner", "employees", "INSERT", ""},
		{"tappa_opdefiner", "employees", "UPDATE", ""},
		{"tappa_opdefiner", "billing_periods", "INSERT", ""},
		{"tappa_opdefiner", "billing_periods", "UPDATE", ""},
		{"tappa_operator", "tenants", "SELECT", ""},
		{"tappa_operator", "employees", "SELECT", ""},
		{"tappa_operator", "billing_periods", "SELECT", ""},
		{"tappa_operator", "billing_periods", "INSERT", ""},
	} {
		if got := opColumns(t, ctx, tx, c.role, c.table, c.priv); got != c.want {
			t.Errorf("%s %s on %s = (%s), want (%s)", c.role, c.priv, c.table, got, c.want)
		}
	}
	for _, c := range []struct {
		column string
		want   bool
	}{{"closed_by", false}, {"id", false}, {"created_at", false}, {"amount_due", true}, {"closed_at", true}} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_column_privilege('tappa_opdefiner', 'public.billing_periods', $1, 'SELECT')`, c.column).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may != c.want {
			t.Errorf("has_column_privilege(tappa_opdefiner, billing_periods, %s, SELECT) = %v, want %v", c.column, may, c.want)
		}
	}
	for _, table := range []string{"tenants", "employees", "billing_periods"} {
		for _, priv := range []string{"DELETE", "TRUNCATE"} {
			for _, role := range []string{"tappa_opdefiner", "tappa_operator"} {
				var may bool
				if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.' || $2, $3)`, role, table, priv).Scan(&may); err != nil || may {
					t.Errorf("has_table_privilege(%s, %s, %s) = %v (err %v), want false", role, table, priv, may, err)
				}
			}
		}
		opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT count(*) FROM public.`+table), sqlstateInsufficientPrivi,
			"tappa_operator reading "+table+" directly")
		opWant(t, opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			_, e := sp.Conn().PgConn().CopyTo(ctx, io.Discard, `COPY public.`+table+` TO STDOUT`)
			return e
		}), sqlstateInsufficientPrivi, "tappa_operator: COPY "+table)
	}
	f := opBillingTenant(t, ctx, tx, "op12 grants "+opToken(t), opZoneMalta, "standard", "1.10", 3)
	at := f.created.Add(time.Hour)
	opHire(t, ctx, tx, f, "a", "active", &at, nil)
	opCloseMonth(t, ctx, tx, f, f.month(1))
	for _, probe := range []string{
		`SELECT closed_by FROM public.billing_periods WHERE tenant_id = $1`,
		`SELECT count(*) FROM public.billing_periods WHERE tenant_id = $1 AND closed_by IS NOT NULL`,
		`SELECT full_name FROM public.employees WHERE tenant_id = $1`,
		`SELECT email FROM public.employees WHERE tenant_id = $1`,
		`SELECT id FROM public.employees WHERE tenant_id = $1`,
		`SELECT b FROM public.billing_periods AS b WHERE b.tenant_id = $1`,
		`UPDATE public.billing_periods SET employee_count = 0 WHERE tenant_id = $1`,
		`DELETE FROM public.billing_periods WHERE tenant_id = $1`,
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_opdefiner", probe, f.id), sqlstateInsufficientPrivi, "tappa_opdefiner: "+probe)
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_opdefiner", `INSERT INTO public.billing_periods (tenant_id, period_month, period_from, period_to,
	         timezone, plan, free_period, employee_count, unit_price, closed_by)
	         SELECT tenant_id, period_month, period_from, period_to, timezone, plan, free_period, employee_count, unit_price, $2
	           FROM public.billing_periods WHERE tenant_id = $1`, f.id, f.admin), sqlstateInsufficientPrivi, "tappa_opdefiner closing a month")
	for _, ctl := range []string{
		`SELECT ` + opBillingDefinerPeriods + ` FROM public.billing_periods WHERE tenant_id = $1`,
		`SELECT ` + opBillingDefinerEmployees + ` FROM public.employees WHERE tenant_id = $1`,
		`SELECT ` + opBillingDefinerTenants + ` FROM public.tenants WHERE id = $1`,
	} {
		if err := opExecAs(t, ctx, tx, "tappa_opdefiner", ctl, f.id); err != nil {
			t.Fatalf("CONTROL: tappa_opdefiner cannot read the granted columns (%v); the refusals above would not be the missing grants", err)
		}
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", readTenantBillingSQL, opRandHex(t), opRandHex(t), f.id, 1),
		sqlstateInsufficientPrivi, "tappa_app calling op_read_tenant_billing")
	for _, fn := range opDefinerForeignExecAllowed {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_function_privilege('tappa_app', $1::text::regprocedure, 'EXECUTE')`, fn).Scan(&may); err != nil || !may {
			t.Errorf("tappa_app may not EXECUTE %s (err %v); 00016's grant must stand", fn, err)
		}
	}
}

// opDefinerForeignExecSet is the set the new pin is about, read three ways for one function
// list: the functions tappa_opdefiner does not own and may EXECUTE although PUBLIC may not,
// the functions it does not own whose ACL names it, and the five 00032 names.
func opDefinerForeignExecSet(t *testing.T, ctx context.Context, q opQuerier) (nonPublic, aclNamed []string) {
	t.Helper()
	read := func(sql string) []string {
		t.Helper()
		rows, err := q.Query(ctx, sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var oid uint32
			if err := rows.Scan(&oid); err != nil {
				t.Fatal(err)
			}
			out = append(out, strconv.FormatUint(uint64(oid), 10))
		}
		return out
	}
	nonPublic = read(`SELECT p.oid::oid FROM pg_proc p
	                    WHERE p.proowner <> 'tappa_opdefiner'::regrole
	                      AND has_function_privilege('tappa_opdefiner', p.oid, 'EXECUTE')
	                      AND NOT has_function_privilege('public', p.oid, 'EXECUTE') ORDER BY 1`)
	aclNamed = read(`SELECT DISTINCT p.oid::oid FROM pg_proc p, aclexplode(p.proacl) AS x
	                   WHERE p.proowner <> 'tappa_opdefiner'::regrole AND x.grantee = 'tappa_opdefiner'::regrole ORDER BY 1`)
	return nonPublic, aclNamed
}

// TestOperator00032_TheDefinerExecutesExactlyTheFiveBillingHelpers is the new pin (the OP-12
// card's T3): outside the functions it owns, tappa_opdefiner EXECUTEs EXACTLY 00016's five
// billing functions -- read two ways, both the named list: the functions it may call and
// PUBLIC may not, and the functions whose ACL names it -- and none of the five is a SECURITY
// DEFINER. opDefinerForeignExecFindings (00030's pin, extended) holds the same set with three
// rules and reports nothing.
//   - T3, MEASURED: tappa_employee_is_billable runs as its caller and calls
//     tappa_employee_lifecycle_status inside; with that one EXECUTE revoked, the billable
//     predicate itself, called as the definer, is 42501 -- and so is the read. CONTROL: with
//     it, the predicate answers.
//   - CONTROLS (each in a savepoint; each must be reported): EXECUTE on a function PUBLIC may
//     call, granted to the definer by name -- the case 00030's rule alone does NOT see (shown
//     here: that rule's query returns nothing for it); a sixth function PUBLIC may not call;
//     resolve_tag_by_uid (a SECURITY DEFINER whose result carries a plaque key); one of the
//     five made SECURITY DEFINER; one of the five's EXECUTE revoked.
func TestOperator00032_TheDefinerExecutesExactlyTheFiveBillingHelpers(t *testing.T) {
	ctx, tx := opTx(t)
	findings, err := opDefinerForeignExecFindings(ctx, tx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Errorf("tappa_opdefiner may EXECUTE %s", f)
	}
	var listed []string
	for _, fn := range opDefinerForeignExecAllowed {
		var oid uint32
		var secdef bool
		if err := tx.QueryRow(ctx, `SELECT p.oid::oid, p.prosecdef FROM pg_proc p WHERE p.oid = $1::text::regprocedure`, fn).Scan(&oid, &secdef); err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		if secdef {
			t.Errorf("%s is a SECURITY DEFINER", fn)
		}
		listed = append(listed, strconv.FormatUint(uint64(oid), 10))
	}
	slices.Sort(listed)
	nonPublic, aclNamed := opDefinerForeignExecSet(t, ctx, tx)
	slices.Sort(nonPublic)
	slices.Sort(aclNamed)
	if !slices.Equal(nonPublic, listed) || !slices.Equal(aclNamed, listed) {
		t.Errorf("the definer's foreign EXECUTE set: non-PUBLIC %v, ACL-named %v; want exactly the five %v", nonPublic, aclNamed, listed)
	}

	// T3, measured: the nested call needs its own EXECUTE.
	const billable = `SELECT public.tappa_employee_is_billable('active', clock_timestamp() - interval '1 day', NULL,
	                          clock_timestamp() - interval '2 days', clock_timestamp())`
	if err := opExecAs(t, ctx, tx, "tappa_opdefiner", billable); err != nil {
		t.Fatalf("CONTROL: the billable predicate as the definer: %v", err)
	}
	f := opBillingTenant(t, ctx, tx, "op12 nested "+opToken(t), opZoneMalta, "standard", "1.10", 2)
	at := f.created.Add(time.Hour)
	opHire(t, ctx, tx, f, "a", "active", &at, nil)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `REVOKE EXECUTE ON FUNCTION public.tappa_employee_lifecycle_status(timestamptz, timestamptz) FROM tappa_opdefiner`); err != nil {
		t.Fatal(err)
	}
	opWant(t, opExecAs(t, ctx, sp, "tappa_opdefiner", billable), sqlstateInsufficientPrivi,
		"the billable predicate as the definer without EXECUTE on the function it calls inside")
	_, rerr := opReadBilling(t, ctx, sp, hash, opForgeBilling(t, ctx, sp, session, a.id, f.id, 1, xact), f.id, 1)
	if rerr == nil || !strings.Contains(rerr.Error(), "SQLSTATE 42501") {
		t.Errorf("the read without EXECUTE on tappa_employee_lifecycle_status: %v, want a 42501 database error", rerr)
	}
	if got, err := opDefinerForeignExecFindings(ctx, sp); err != nil || !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "tappa_employee_lifecycle_status") }) {
		t.Errorf("CONTROL: a listed function the definer may not call is not reported: %v (err %v)", got, err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// The 00030 rule alone, as it stood before 00032 (kept here as a query, for the control
	// below to measure what it could not see).
	const rule00030 = `SELECT count(*) FROM pg_proc p
	                    WHERE p.proowner <> 'tappa_opdefiner'::regrole
	                      AND has_function_privilege('tappa_opdefiner', p.oid, 'EXECUTE')
	                      AND (p.prosecdef OR NOT has_function_privilege('public', p.oid, 'EXECUTE'))
	                      AND p.proname = 'zz_probe_public'`
	for _, c := range []struct{ name, sql, want string }{
		{"a PUBLIC function granted to the definer by name", `CREATE FUNCTION public.zz_probe_public() RETURNS int LANGUAGE sql AS 'SELECT 1';
		  GRANT EXECUTE ON FUNCTION public.zz_probe_public() TO tappa_opdefiner`, "zz_probe_public"},
		{"a sixth function PUBLIC may not call", `CREATE FUNCTION public.zz_probe_owned() RETURNS int LANGUAGE sql AS 'SELECT 1';
		  REVOKE ALL ON FUNCTION public.zz_probe_owned() FROM PUBLIC;
		  GRANT EXECUTE ON FUNCTION public.zz_probe_owned() TO tappa_opdefiner`, "zz_probe_owned"},
		{"resolve_tag_by_uid", `GRANT EXECUTE ON FUNCTION public.resolve_tag_by_uid(character) TO tappa_opdefiner`, "resolve_tag_by_uid"},
		{"a listed function made SECURITY DEFINER", `ALTER FUNCTION public.tappa_billing_amount_due(boolean, numeric, integer) SECURITY DEFINER`, "tappa_billing_amount_due"},
		{"a listed function's EXECUTE revoked", `REVOKE EXECUTE ON FUNCTION public.tappa_local_month_start(date, text) FROM tappa_opdefiner`, "tappa_local_month_start"},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Conn().PgConn().Exec(ctx, c.sql).ReadAll(); err != nil {
			t.Fatalf("control %q: %v", c.name, err)
		}
		got, err := opDefinerForeignExecFindings(ctx, sp)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, c.want) }) {
			t.Errorf("CONTROL %q: the scan did not report %s; findings: %v", c.name, c.want, got)
		}
		if c.want == "zz_probe_public" {
			if n := opInt(t, ctx, sp, rule00030); n != 0 {
				t.Errorf("the 00030 rule alone now sees the PUBLIC function granted by name (%d); this control no longer shows the gap the ACL rule closes", n)
			}
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestOperator00032_DownGivesBack00031AndUpTakesItAgain runs 00032's Down and Up from the
// migration file inside the test's transaction (opAtVersion first, so a later migration does
// not stand in the way).
//   - THE TEXT: the Down's op_begin_read and op_read_audit are 00031's Up bodies VERBATIM (read
//     from 00031's file); the Down revokes by column and by function -- it holds no REVOKE ALL
//     (the OP-12 card's T2).
//   - THE SETS AND THE CONDITIONS, WHOLE: the ticket set of the Down's NOT VALID condition and
//     of its two CHECKs is 00031's (derived from 00031's file); the Up's is the six; each of the
//     two conditions is read to its end and compared whole.
//   - Down removes the read, gives both functions 00031's bodies back (the live prosrc),
//     takes back EXACTLY section 2's grants -- the definer's lists on tenants and employees are
//     00029's after it, it holds nothing on billing_periods and no EXECUTE on the five, while
//     tappa_app keeps its EXECUTE on them -- and the tenant overview, which reads 00029's
//     columns, still reads (the trap of a REVOKE ALL). After Down op_read_audit no longer knows
//     the billing scope: such a 'read' row comes back with no scope and unrecognised.
//   - NOT VALID, branch by branch: nothing outside 00031's set -> VALIDATED; an unconsumed
//     'tenant_billing' ticket -> NOT VALID (a new such ticket is refused); ONLY a CONSUMED one
//     (the shipped read consumed it -- production's case) -> NOT VALID; ONLY a later
//     migration's ticket, unconsumed and consumed -> the Down NOT VALID and the Up NOT VALID
//     too (it composes). Up again takes it all, VALIDATED where nothing is outside the six.
//   - THE CHAIN COMPOSES DOWNWARD: with a 'tenant_billing' ticket present, 00032's, 00031's and
//     00030's Downs run one after another without a 23514.
func TestOperator00032_DownGivesBack00031AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 32, opKindsAt32...)
	up, down := opMigrationSections(t, op00032File)
	up31, down31 := opMigrationSections(t, op00031File)
	_, down30 := opMigrationSections(t, op00030File)
	begin31, begin32 := opBeginReadBody(t, op00031File), opBeginReadBody(t, op00032File)
	audit31 := opLogFunctionBody(t, up31, "CREATE FUNCTION", "op_read_audit")
	audit32 := opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_read_audit")
	if begin31 == begin32 || audit31 == audit32 {
		t.Fatal("PREMISE: a replaced body is the same text as the one it replaces")
	}
	if got := opLogFunctionBody(t, down, "CREATE OR REPLACE FUNCTION", "op_begin_read"); got != begin31 {
		t.Error("00032's Down does not give op_begin_read 00031's Up body verbatim")
	}
	if got := opLogFunctionBody(t, down, "CREATE OR REPLACE FUNCTION", "op_read_audit"); got != audit31 {
		t.Error("00032's Down does not give op_read_audit 00031's Up body verbatim")
	}
	// The statements only: the Down's comments name REVOKE ALL to say why it is not written.
	var downStatements strings.Builder
	for _, line := range strings.Split(down, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			downStatements.WriteString(line + "\n")
		}
	}
	if !strings.Contains(downStatements.String(), "REVOKE SELECT (timezone, price_per_employee_month) ON tenants FROM tappa_opdefiner;") {
		t.Fatal("anti-vacuity: the Down's statements do not hold its own column REVOKE")
	}
	if regexp.MustCompile(`(?i)\bREVOKE\s+ALL\b`).MatchString(downStatements.String()) {
		t.Error("00032's Down writes REVOKE ALL; it takes back its own grants, by column and by function")
	}

	// THE SETS, from 00031's file, and the two sections' uses of them.
	m := regexp.MustCompile(`(?s)ADD CONSTRAINT operator_read_tickets_kind_check\s+CHECK \(kind IN \(([^)]*)\)\);`).FindStringSubmatch(up31)
	set31 := []string{}
	if m != nil {
		set31 = opQuotedList(strings.Join(strings.Fields(m[1]), " "))
	}
	if !slices.Equal(set31, opKindsAt31) {
		t.Fatalf("00031's ticket kind set is %v, want %v", set31, opKindsAt31)
	}
	i := strings.Index(down, "ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check")
	if i < 0 {
		t.Fatal("00032's Down does not restore the ticket CHECK")
	}
	ticketPart := down[i:]
	conds := func(part string) []string {
		var out []string
		re := regexp.MustCompile(`(?s)IF EXISTS \(SELECT 1 FROM public\.operator_read_tickets\s+WHERE (.*?)\) THEN`)
		for _, mm := range re.FindAllStringSubmatch(part, -1) {
			out = append(out, strings.Join(strings.Fields(mm[1]), " "))
		}
		return out
	}
	whole := func(set []string) string { return "kind <> ALL (ARRAY['" + strings.Join(set, "', '") + "'])" }
	for _, c := range []struct {
		what, part string
		set        []string
	}{
		{"00032's Down: the ticket condition", ticketPart, opKindsAt31},
		{"00032's Up: the ticket condition", up, opKindsAt32},
	} {
		if got, want := conds(c.part), whole(c.set); len(got) != 1 || got[0] != want {
			t.Errorf("%s is %q; want exactly one, whole: %q", c.what, got, want)
		}
	}
	checks := func(part string) [][]string {
		var out [][]string
		for _, mm := range regexp.MustCompile(`(?s)CHECK \(kind IN \(([^)]*)\)\)`).FindAllStringSubmatch(part, -1) {
			out = append(out, opQuotedList(strings.Join(strings.Fields(mm[1]), " ")))
		}
		return out
	}
	for _, c := range []struct {
		what string
		got  [][]string
		want []string
	}{
		{"00032's Down", checks(ticketPart), opKindsAt31},
		{"00032's Up", checks(up), opKindsAt32},
	} {
		if len(c.got) != 2 {
			t.Errorf("%s adds %d ticket kind CHECKs, want 2 (NOT VALID and VALIDATED)", c.what, len(c.got))
		}
		for _, g := range c.got {
			if !slices.Equal(g, c.want) {
				t.Errorf("%s: a ticket CHECK names %v, want %v", c.what, g, c.want)
			}
		}
	}

	type state struct {
		reads                          int64
		begin, audit                   string
		tickets                        string
		ticketsValid                   bool
		definerACL                     string
		tenantsSel, employeesSel       string
		periodsSel                     string
		definerExec, appExec           int64
		appBegin, opBegin, opReadAudit bool
	}
	read := func(q pgx.Tx) state {
		t.Helper()
		var s state
		if err := q.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM pg_proc WHERE proname = 'op_read_tenant_billing'),
			       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_begin_read(text, text, jsonb)'::regprocedure),
			       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_read_audit(text, text, text, integer, integer)'::regprocedure),
			       (SELECT coalesce(string_agg(c.relname || '.' || a.attname || ':' || x.privilege_type, ',' ORDER BY c.relname, a.attnum), '')
			          FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid, aclexplode(a.attacl) AS x
			         WHERE c.oid IN ('public.tenants'::regclass, 'public.employees'::regclass, 'public.billing_periods'::regclass)
			           AND a.attnum > 0 AND x.grantee = 'tappa_opdefiner'::regrole),
			       (SELECT count(*) FROM unnest($1::text[]::regprocedure[]) AS f WHERE has_function_privilege('tappa_opdefiner', f, 'EXECUTE')),
			       (SELECT count(*) FROM unnest($1::text[]::regprocedure[]) AS f WHERE has_function_privilege('tappa_app', f, 'EXECUTE')),
			       has_function_privilege('tappa_app', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE'),
			       has_function_privilege('tappa_operator', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE'),
			       has_function_privilege('tappa_operator', 'public.op_read_audit(text, text, text, integer, integer)', 'EXECUTE')`,
			opDefinerForeignExecAllowed).
			Scan(&s.reads, &s.begin, &s.audit, &s.definerACL, &s.definerExec, &s.appExec, &s.appBegin, &s.opBegin, &s.opReadAudit); err != nil {
			t.Fatalf("read the state: %v", err)
		}
		s.tickets, s.ticketsValid = opConstraint(t, ctx, q, "operator_read_tickets", "operator_read_tickets_kind_check")
		s.tenantsSel = opColumns(t, ctx, q, "tappa_opdefiner", "tenants", "SELECT")
		s.employeesSel = opColumns(t, ctx, q, "tappa_opdefiner", "employees", "SELECT")
		s.periodsSel = opColumns(t, ctx, q, "tappa_opdefiner", "billing_periods", "SELECT")
		return s
	}
	const acl31 = "employees.tenant_id:SELECT,employees.status:SELECT," +
		"tenants.id:SELECT,tenants.name:SELECT,tenants.business_type:SELECT,tenants.plan:SELECT,tenants.created_at:SELECT"
	const acl32 = "billing_periods.tenant_id:SELECT,billing_periods.period_month:SELECT,billing_periods.period_from:SELECT," +
		"billing_periods.period_to:SELECT,billing_periods.timezone:SELECT,billing_periods.plan:SELECT,billing_periods.free_period:SELECT," +
		"billing_periods.employee_count:SELECT,billing_periods.unstamped_employees:SELECT,billing_periods.unit_price:SELECT," +
		"billing_periods.currency:SELECT,billing_periods.amount_due:SELECT,billing_periods.closed_at:SELECT," +
		"employees.tenant_id:SELECT,employees.status:SELECT,employees.activated_at:SELECT,employees.deactivated_at:SELECT," +
		"tenants.id:SELECT,tenants.name:SELECT,tenants.business_type:SELECT,tenants.plan:SELECT,tenants.timezone:SELECT," +
		"tenants.created_at:SELECT,tenants.price_per_employee_month:SELECT"
	notValid := func(def string, nv bool) string {
		if nv {
			return def + " NOT VALID"
		}
		return def
	}
	at32 := func(s state, when string, nv bool) {
		t.Helper()
		if s.reads != 1 || s.begin != begin32 || s.audit != audit32 || s.definerACL != acl32 ||
			s.tenantsSel != opBillingDefinerTenants || s.employeesSel != opBillingDefinerEmployees || s.periodsSel != opBillingDefinerPeriods ||
			s.definerExec != 5 || s.appExec != 5 || s.appBegin || !s.opBegin || !s.opReadAudit ||
			s.tickets != notValid(opKindCheckDef(opKindsAt32), nv) || s.ticketsValid == nv {
			t.Errorf("%s: reads=%d begin is 00032's=%v audit is 00032's=%v acl=(%s) definer EXECUTE=%d app EXECUTE=%d app begin=%v operator=%v/%v\n tickets=%s (%v)\n want 00032's state, tickets NOT VALID=%v",
				when, s.reads, s.begin == begin32, s.audit == audit32, s.definerACL, s.definerExec, s.appExec, s.appBegin, s.opBegin, s.opReadAudit,
				s.tickets, s.ticketsValid, nv)
		}
	}
	at31 := func(s state, when string, nv bool) {
		t.Helper()
		if s.reads != 0 || s.begin != begin31 || s.audit != audit31 || s.definerACL != acl31 ||
			s.tenantsSel != opDefinerTenants29 || s.employeesSel != opDefinerEmployees29 || s.periodsSel != "" ||
			s.definerExec != 0 || s.appExec != 5 || s.appBegin || !s.opBegin || !s.opReadAudit ||
			s.tickets != notValid(opKindCheckDef(opKindsAt31), nv) || s.ticketsValid == nv {
			t.Errorf("%s: reads=%d begin is 00031's=%v audit is 00031's=%v acl=(%s) tenants=(%s) employees=(%s) periods=(%s) definer EXECUTE=%d app EXECUTE=%d\n tickets=%s (%v)\n want 00031's state, tickets NOT VALID=%v",
				when, s.reads, s.begin == begin31, s.audit == audit31, s.definerACL, s.tenantsSel, s.employeesSel, s.periodsSel, s.definerExec, s.appExec,
				s.tickets, s.ticketsValid, nv)
		}
	}
	at32(read(tx), "PREMISE before Down", false)

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	ov := &opFixtureTenant{name: "op12 down " + opToken(t), locations: 1, activeEmp: 2, activeTags: 1, activeAdmins: 1}
	ov.id = opNewTenant(t, ctx, tx, ov.name, 0)
	opPopulate(t, ctx, tx, ov)
	xact := opCommittedXact(t, ctx)
	branch := func() pgx.Tx {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, `DELETE FROM operator_read_tickets WHERE kind <> ALL ($1)`, opKindsAt31); err != nil {
			t.Fatalf("clear the kinds 00031 does not know, inside the transaction: %v", err)
		}
		return sp
	}
	done := func(sp pgx.Tx) {
		t.Helper()
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	consumedOnly := func(q pgx.Tx, set []string, what string) {
		t.Helper()
		var consumed, unconsumed int64
		if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE consumed_at IS NOT NULL), count(*) FILTER (WHERE consumed_at IS NULL)
		                             FROM operator_read_tickets WHERE kind <> ALL ($1)`, set).Scan(&consumed, &unconsumed); err != nil {
			t.Fatal(err)
		}
		if consumed != 1 || unconsumed != 0 {
			t.Fatalf("PREMISE (%s): %d consumed and %d unconsumed ticket(s) outside %v; want one consumed, none unconsumed", what, consumed, unconsumed, set)
		}
	}

	// Branch 1: nothing outside 00031's set -> VALIDATED; the overview still reads; Up again.
	sp := branch()
	billingRow := opLogInsert(t, ctx, sp, opLogRow{kind: "read", session: &session, actor: &a.id, tenant: &ov.id,
		scope: tenantBillingReadKind, number: 1, size: 12})
	opRunSection(t, ctx, sp, down, "00032 Down with nothing outside 00031's set")
	at31(read(sp), "after Down (nothing outside 00031's set)", false)
	overview, err := opReadDetail(t, ctx, sp, hash, opForgeDetail(t, ctx, sp, session, a.id, ov.id, xact), ov.id)
	if err != nil || len(overview) != 1 || overview[0].ActiveEmployees != 2 {
		t.Errorf("after Down the tenant overview reads %+v, err %v; want one row counting 2 active employees (00029's grants intact)", overview, err)
	}
	_, err = opBegin(t, ctx, sp, hash, tenantBillingReadKind, opBillingParams(ov.id.String(), 1))
	opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "after Down, a 'tenant_billing' first phase")
	logRows, err := opReadLog(t, ctx, sp, hash, opForgeLog(t, ctx, sp, session, a.id, "read", 1, 200, xact), "read", 1, 200)
	if err != nil {
		t.Fatalf("after Down, op_read_audit: %v", err)
	}
	found := false
	for _, r := range logRows {
		if r.ID == billingRow {
			found = true
			if r.Scope != nil || r.DetailRecognised {
				t.Errorf("after Down a billing 'read' row reads scope=%s recognised=%v; 00031's list does not know the scope", opDeref(r.Scope), r.DetailRecognised)
			}
		}
	}
	if !found {
		t.Error("anti-vacuity: the billing 'read' row is not on the first 200 'read' rows after Down")
	}
	opRunSection(t, ctx, sp, up, "00032 Up again")
	at32(read(sp), "after Up again", false)
	done(sp)

	// Branch 2: an unconsumed 'tenant_billing' ticket -> NOT VALID; a new one refused.
	sp = branch()
	if _, err := opBegin(t, ctx, sp, hash, tenantBillingReadKind, opBillingParams(ov.id.String(), 2)); err != nil {
		t.Fatalf("a 'tenant_billing' first phase: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00032 Down with a 'tenant_billing' ticket present")
	at31(read(sp), "after Down (a 'tenant_billing' ticket)", true)
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at)
	                              SELECT $1, $2, 'tenant_billing', l.id, clock_timestamp() + interval '30 seconds'
	                                FROM operator_audit_log l WHERE l.session_id = $2 LIMIT 1`, opRandHex(t), session),
		sqlstateCheckViolation, "a NEW 'tenant_billing' ticket after Down (NOT VALID still binds new rows)")
	opRunSection(t, ctx, sp, up, "00032 Up again over the 'tenant_billing' ticket")
	at32(read(sp), "after Up again (a 'tenant_billing' ticket)", false)
	done(sp)

	// Branch 3: ONLY a CONSUMED 'tenant_billing' ticket -- the shipped read consumed it.
	sp = branch()
	raw, err := opBegin(t, ctx, sp, hash, tenantBillingReadKind, opBillingParams(ov.id.String(), 1))
	if err != nil {
		t.Fatalf("a 'tenant_billing' first phase: %v", err)
	}
	if _, err := sp.Exec(ctx, `UPDATE operator_read_tickets SET created_xact = $1::xid8 WHERE ticket_hash = $2`,
		xact, opBillingTicketHash(raw, ov.id, 1)); err != nil {
		t.Fatalf("name a committed transaction on the ticket: %v", err)
	}
	if _, err := opReadBilling(t, ctx, sp, hash, raw, ov.id, 1); err != nil {
		t.Fatalf("the shipped read consumes the ticket: %v", err)
	}
	consumedOnly(sp, opKindsAt31, "a consumed 'tenant_billing' ticket")
	opRunSection(t, ctx, sp, down, "00032 Down with only a CONSUMED 'tenant_billing' ticket outside 00031's set")
	at31(read(sp), "after Down (a consumed 'tenant_billing' ticket)", true)
	opRunSection(t, ctx, sp, up, "00032 Up again over the consumed 'tenant_billing' ticket")
	at32(read(sp), "after Up again (a consumed 'tenant_billing' ticket)", false)
	done(sp)

	// Branches 4 and 5: ONLY a LATER migration's ticket, unconsumed then consumed. Its Up widened
	// the set, its read wrote a ticket, its Down put 00032's set back NOT VALID and left it.
	for _, consume := range []bool{false, true} {
		sp = branch()
		for _, s := range []string{
			`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
			`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN ('` + strings.Join(opKindsAt32, "', '") + `', 'zz_later_read'))`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("simulate a later migration's Up: %v", err)
			}
		}
		later := opForgeRead(t, ctx, sp, session, a.id, "zz_later_read", opRandHex(t), nil, xact, "30 seconds")
		if consume {
			if _, err := sp.Exec(ctx, `UPDATE operator_read_tickets SET consumed_at = clock_timestamp() WHERE id = $1`, later); err != nil {
				t.Fatal(err)
			}
		}
		for _, s := range []string{
			`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
			`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN ('` + strings.Join(opKindsAt32, "', '") + `')) NOT VALID`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("simulate a later migration's Down: %v", err)
			}
		}
		if consume {
			consumedOnly(sp, opKindsAt32, "a later migration's consumed ticket")
		}
		what := fmt.Sprintf("a later migration's ticket (consumed=%v)", consume)
		opRunSection(t, ctx, sp, down, "00032 Down over "+what)
		at31(read(sp), "after Down ("+what+")", true)
		opRunSection(t, ctx, sp, up, "00032 Up again over "+what+" (it composes)")
		at32(read(sp), "after Up again ("+what+")", true)
		done(sp)
	}

	// The chain composes downward: 32 -> 31 -> 30 with a 'tenant_billing' ticket present.
	sp = branch()
	opForgeBilling(t, ctx, sp, session, a.id, ov.id, 1, xact)
	opRunSection(t, ctx, sp, down, "00032 Down (chain)")
	opRunSection(t, ctx, sp, down31, "00031 Down after 00032's, a 'tenant_billing' ticket present")
	opRunSection(t, ctx, sp, down30, "00030 Down after 00031's, a 'tenant_billing' ticket present")
	if def, valid := opConstraint(t, ctx, sp, "operator_read_tickets", "operator_read_tickets_kind_check"); valid ||
		def != notValid(`CHECK ((kind = ANY (ARRAY['legal_versions'::text, 'tenants'::text, 'tenant_detail'::text])))`, true) {
		t.Errorf("after 32 -> 31 -> 30 the ticket CHECK is %s (validated %v); want 00029's set NOT VALID", def, valid)
	}
	done(sp)

	// And with nothing outside the sets, Down and Up once more: VALIDATED both ways.
	sp = branch()
	opRunSection(t, ctx, sp, down, "00032 Down")
	opRunSection(t, ctx, sp, up, "00032 Up")
	at32(read(sp), "after Down and Up", false)
	done(sp)
}

// TestOperator00032_PreconditionRefusesAWrongCluster: 00032's first statement refuses the role
// shapes 00026-00031's refuse (absent, over-privileged, joined by membership in either
// direction) and a database that is not at 00031 -- op_read_audit or op_begin_read missing or
// not the definer's, one of 00016's five functions missing under its name, the ticket CHECK
// missing, without 'operator_audit', or already naming 'tenant_billing' -- with SQLSTATE 55000
// naming 00032, and passes the cluster this suite runs on (taken to 00031 first). The server
// version floor cannot be driven on a 17 server: its text is pinned instead (a counted limit).
func TestOperator00032_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 31, opKindsAt31...)
	up, _ := opMigrationSections(t, op00032File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00032's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	if !strings.Contains(pre, "IF pg_catalog.current_setting('server_version_num')::integer < 170000 THEN") {
		t.Error("the precondition no longer refuses a server below PostgreSQL 17 (the floor its measurements rest on)")
	}
	var version int
	if err := tx.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer`).Scan(&version); err != nil || version < 170000 {
		t.Fatalf("PREMISE: this suite runs on PostgreSQL 17 or later (server_version_num %d, err %v)", version, err)
	}
	const tickets31 = `ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
	                   ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN (%s))`
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"roles present, at 00031", nil, ""},
		{"both roles absent", []string{`ALTER ROLE tappa_operator RENAME TO zz_op12_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op12_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner without BYPASSRLS", []string{`ALTER ROLE tappa_opdefiner NOBYPASSRLS`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_opdefiner is a member", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
		{"op_read_audit missing", []string{`ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) RENAME TO zz_op12_was_read_audit`}, "00031's functions"},
		{"op_read_audit not the definer's", []string{`ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) OWNER TO tappa_owner`}, "00031's functions"},
		{"op_begin_read not the definer's", []string{`ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_owner`}, "00031's functions"},
		{"a billing function missing", []string{`ALTER FUNCTION public.tappa_local_month_start(date, text) RENAME TO zz_op12_was_month_start`}, "five billing functions"},
		{"the ticket CHECK missing", []string{`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`}, "as migration 00031 left it"},
		{"the ticket CHECK without operator_audit", []string{`DELETE FROM operator_read_tickets WHERE kind = 'operator_audit'`,
			fmt.Sprintf(tickets31, `'legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques'`)}, "as migration 00031 left it"},
		{"the ticket CHECK already naming tenant_billing", []string{fmt.Sprintf(tickets31, `'`+strings.Join(opKindsAt32, `', '`)+`'`)}, "as migration 00031 left it"},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		for _, s := range c.setup {
			if _, err := sp.Conn().PgConn().Exec(ctx, s).ReadAll(); err != nil {
				t.Fatalf("%s: setup %q: %v", c.what, s, err)
			}
		}
		_, runErr := sp.Conn().PgConn().Exec(ctx, pre).ReadAll()
		code, msg := opCode(runErr)
		switch {
		case c.want == "" && runErr != nil:
			t.Errorf("%s: the precondition refuses a correct cluster: %v", c.what, runErr)
		case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00032")):
			t.Errorf("%s: precondition answered %q %q, want %s naming 00032 and containing %q", c.what, code, msg, sqlstatePrerequisiteState, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("%s: rollback: %v", c.what, err)
		}
	}
}

// TestOperator00032_CallersTempTableIsNeverRead is ADR 0021 §6's temp-table shadow for the
// read and the replaced first phase: the caller creates temp tables with every name they touch
// (tenants, employees, billing_periods and the operator tables), fills them with forged rows --
// a forged session, the real tenant under a forged name, zone and price, forged employees and a
// forged frozen month of the real tenant -- and GRANTs them to tappa_opdefiner (the step without
// which a broken search_path would fail with "permission denied" and look refused); the
// functions still read and write the real ones: the page is the one read before the shadow
// existed, row for row.
func TestOperator00032_CallersTempTableIsNeverRead(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := opBillingTenant(t, ctx, tx, "op12 real "+opToken(t), opZoneMalta, "founding", "1.10", 4)
	at := f.created.Add(time.Hour)
	opHire(t, ctx, tx, f, "a", "active", &at, nil)
	opHire(t, ctx, tx, f, "b", "active", &at, nil)
	opCloseMonth(t, ctx, tx, f, f.month(1))
	forgedSession := opRandHex(t)
	xact := opCommittedXact(t, ctx)
	before, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, f.id, 1, xact), f.id, 1)
	if err != nil {
		t.Fatalf("the read before the shadow: %v", err)
	}

	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		for _, s := range []string{
			`CREATE TEMP TABLE tenants (id uuid, name text, plan text, timezone text, created_at timestamptz, price_per_employee_month numeric)`,
			`CREATE TEMP TABLE employees (tenant_id uuid, status text, activated_at timestamptz, deactivated_at timestamptz)`,
			`CREATE TEMP TABLE billing_periods (tenant_id uuid, period_month date, period_from timestamptz, period_to timestamptz,
			     timezone text, plan text, free_period boolean, employee_count integer, unstamped_employees integer,
			     unit_price numeric, currency text, amount_due numeric, closed_at timestamptz)`,
			`CREATE TEMP TABLE platform_sessions (id uuid DEFAULT gen_random_uuid(), admin_id uuid, token_hash text,
			     created_at timestamptz DEFAULT clock_timestamp(), mfa_verified_at timestamptz,
			     last_used_at timestamptz DEFAULT clock_timestamp(), revoked_at timestamptz)`,
			`CREATE TEMP TABLE platform_admins (id uuid, email text, display_name text, status text)`,
			`CREATE TEMP TABLE operator_audit_log (id uuid DEFAULT gen_random_uuid(), at timestamptz DEFAULT clock_timestamp(),
			     kind text, session_id uuid, actor_admin_id uuid, target_admin_id uuid, target_tenant_id uuid,
			     target_scope text, page_number integer, page_size integer, detail jsonb DEFAULT '{}')`,
			`CREATE TEMP TABLE operator_read_tickets (id uuid DEFAULT gen_random_uuid(), ticket_hash text, session_id uuid,
			     kind text, target_tenant_id uuid, audit_id uuid, created_at timestamptz DEFAULT clock_timestamp(),
			     created_xact xid8 DEFAULT '3'::xid8, expires_at timestamptz, consumed_at timestamptz)`,
			`GRANT ALL ON pg_temp.tenants, pg_temp.employees, pg_temp.billing_periods, pg_temp.platform_sessions,
			     pg_temp.platform_admins, pg_temp.operator_audit_log, pg_temp.operator_read_tickets TO tappa_opdefiner`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return err
			}
		}
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO pg_temp.tenants VALUES ($1, 'SHADOW name of the real id', 'standard', 'Etc/GMT-14', $2, 999.99)`, []any{f.id, f.created}},
			{`INSERT INTO pg_temp.employees SELECT $1::uuid, 'active', $2::timestamptz, NULL FROM generate_series(1, 7)`, []any{f.id, f.created}},
			{`INSERT INTO pg_temp.billing_periods VALUES ($1, $2, $3, $4, 'Etc/GMT-14', 'standard', false, 77, 0, 999.99, 'XXX', 76999.23, $4)`,
				[]any{f.id, f.month(2), f.created, f.created.Add(24 * time.Hour)}},
			{`INSERT INTO pg_temp.platform_sessions (admin_id, token_hash, mfa_verified_at) VALUES ($1, $2, clock_timestamp())`, []any{a.id, forgedSession}},
		} {
			if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("build the shadow as the caller: %v", err)
	}
	var reachable int64
	if err := opAs(t, ctx, tx, "tappa_opdefiner", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.tenants) + (SELECT count(*) FROM pg_temp.employees)
		                              + (SELECT count(*) FROM pg_temp.billing_periods) + (SELECT count(*) FROM pg_temp.platform_sessions)`).Scan(&reachable)
	}); err != nil {
		t.Fatalf("the definer role cannot read the shadow (%v); the GRANT step is what makes this test mean anything", err)
	}
	if reachable != 10 {
		t.Fatalf("the definer role sees %d forged rows, want 10", reachable)
	}

	_, err = opBegin(t, ctx, tx, forgedSession, tenantBillingReadKind, opBillingParams(f.id.String(), 1))
	opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read with a session that exists only in the caller's temp table")
	audit0 := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session)
	if _, err := opBegin(t, ctx, tx, hash, tenantBillingReadKind, opBillingParams(f.id.String(), 1)); err != nil {
		t.Fatalf("op_begin_read with the real session: %v", err)
	}
	if d := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session) - audit0; d != 1 {
		t.Errorf("%d 'read' row(s) reached the REAL log, want 1", d)
	}
	after, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, f.id, 1, xact), f.id, 1)
	if err != nil {
		t.Fatalf("op_read_tenant_billing with the shadow in place: %v", err)
	}
	if after.TenantName != before.TenantName || !slices.Equal(opMonthTexts(after), opMonthTexts(before)) {
		t.Errorf("the read with the shadow in place differs from the read before it (name %q vs %q):\n%s\nwant\n%s",
			after.TenantName, before.TenantName, strings.Join(opMonthTexts(after), "\n"), strings.Join(opMonthTexts(before), "\n"))
	}
	if strings.Contains(after.TenantName, "SHADOW") {
		t.Error("op_read_tenant_billing returned the caller's temp tenant name")
	}
	for _, m := range after.Months {
		if opStrEq(m.Zone, "Etc/GMT-14") || opStrEq(m.Currency, "XXX") || opIntEq(m.EmployeeCount, 77) || opIntEq(m.EmployeeCount, 7) {
			t.Errorf("op_read_tenant_billing returned a figure of the caller's temp tables: %+v", m)
		}
	}
	var shadowAudit, shadowTickets int64
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.operator_audit_log), (SELECT count(*) FROM pg_temp.operator_read_tickets)`).
			Scan(&shadowAudit, &shadowTickets)
	}); err != nil {
		t.Fatal(err)
	}
	if shadowAudit != 0 || shadowTickets != 0 {
		t.Errorf("the CALLER's temp tables were written: audit=%d tickets=%d, want 0 and 0", shadowAudit, shadowTickets)
	}
}

// ------------------------------------------------------------ op_begin_read --

// TestOpBeginRead_TheBillingKindBindsTheTenantAndAPage: the kind 00032 adds to the first phase.
//   - 'tenant_billing' with a tenant and a page: one 'read' row -- scope 'tenant_billing', the
//     tenant in target_tenant_id, the page in page_number, page_size 12, detail exactly {} --
//     and one ticket carrying the same tenant whose stored hash is sha256(raw ticket || the
//     canonical {tenant_id, page_number}); an upper-case id binds the same lower-case text;
//     page 1 and page MaxTenantBillingPage pass;
//   - 22023 and no row for every parameter object the kind does not name (listed below) --
//     page MaxTenantBillingPage+1 among them, THE PAGE BOUND -- and 28000 and no row for the six
//     dead sessions;
//   - CONTROLS: the five kinds 00031 named still pass phase one with their own objects.
func TestOpBeginRead_TheBillingKindBindsTheTenantAndAPage(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	rowsOf := func() (audit, tickets int64) {
		return opAudit(t, ctx, tx), opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets`)
	}
	id := uuid.New()
	for _, c := range []struct {
		spelled string
		page    int
	}{
		{id.String(), 1},
		{strings.ToUpper(id.String()), MaxTenantBillingPage},
		{id.String(), 3},
	} {
		a0, k0 := rowsOf()
		raw, err := opBegin(t, ctx, tx, hash, tenantBillingReadKind, opBillingParams(c.spelled, c.page))
		if err != nil {
			t.Fatalf("op_begin_read 'tenant_billing' %s page %d: %v", c.spelled, c.page, err)
		}
		if a1, k1 := rowsOf(); a1-a0 != 1 || k1-k0 != 1 {
			t.Fatalf("op_begin_read 'tenant_billing' wrote %d audit row(s) and %d ticket(s), want 1 and 1", a1-a0, k1-k0)
		}
		var (
			kind, scope, detail, ticketHash, ticketKind string
			tenant, ticketTenant                        *uuid.UUID
			number, size                                *int
		)
		if err := tx.QueryRow(ctx, `
			SELECT l.kind, l.target_scope, l.detail::text, l.target_tenant_id, l.page_number, l.page_size,
			       k.ticket_hash, k.kind, k.target_tenant_id
			  FROM operator_audit_log l JOIN operator_read_tickets k ON k.audit_id = l.id
			 WHERE l.session_id = $1 ORDER BY l.at DESC, l.id DESC LIMIT 1`, session).
			Scan(&kind, &scope, &detail, &tenant, &number, &size, &ticketHash, &ticketKind, &ticketTenant); err != nil {
			t.Fatalf("read the audit row and its ticket: %v", err)
		}
		if kind != "read" || scope != tenantBillingReadKind || detail != "{}" || tenant == nil || *tenant != id ||
			number == nil || *number != c.page || size == nil || *size != TenantBillingMonthsPerPage ||
			ticketKind != tenantBillingReadKind || ticketTenant == nil || *ticketTenant != id {
			t.Errorf("the 'tenant_billing' row for page %d: kind=%s scope=%s detail=%s tenant=%v page=%s/%s; ticket kind=%s tenant=%v",
				c.page, kind, scope, detail, tenant, opDeref(number), opDeref(size), ticketKind, ticketTenant)
		}
		if ticketHash != opBillingTicketHash(raw, id, c.page) {
			t.Errorf("the stored hash is not sha256(raw ticket || canonical {tenant_id, page_number}) for %s page %d", c.spelled, c.page)
		}
	}

	for _, c := range []struct{ name, params string }{
		{"no key", `{}`},
		{"the tenant alone", opDetailParams(id.String())},
		{"the page alone", `{"page_number": 1}`},
		{"an extra key", `{"tenant_id": "` + id.String() + `", "page_number": 1, "page_size": 12}`},
		{"the list's keys", opTenantsParams("", 1, 12)},
		{"the log's keys", opLogParams("", 1, 12)},
		{"a tenant that is not a uuid", opBillingParams("op12-not-a-uuid", 1)},
		{"a tenant in braces", opBillingParams("{"+id.String()+"}", 1)},
		{"a tenant as a number", `{"tenant_id": 7, "page_number": 1}`},
		{"a null tenant", `{"tenant_id": null, "page_number": 1}`},
		{"page MaxTenantBillingPage+1", opBillingParams(id.String(), MaxTenantBillingPage+1)},
		{"page 0", opBillingParams(id.String(), 0)},
		{"page -1", opBillingParams(id.String(), -1)},
		{"page as a string", opBillingParams(id.String(), "1")},
		{"page 1.5", `{"tenant_id": "` + id.String() + `", "page_number": 1.5}`},
		{"page 1.0", `{"tenant_id": "` + id.String() + `", "page_number": 1.0}`},
		{"a null page", `{"tenant_id": "` + id.String() + `", "page_number": null}`},
		{"page with ten digits", opBillingParams(id.String(), 1000000000)},
		{"a string, not an object", `"` + id.String() + `"`},
	} {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, hash, tenantBillingReadKind, c.params)
		opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "op_begin_read 'tenant_billing', "+c.name, hash, c.params)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read 'tenant_billing', %s: %d audit row(s) and %d ticket(s) written", c.name, a1-a0, k1-k0)
		}
	}
	for _, d := range opDeadSessions(t, ctx, tx) {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, d.hash, tenantBillingReadKind, opBillingParams(id.String(), 1))
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read 'tenant_billing', "+d.name, d.hash)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read 'tenant_billing', %s: %d audit row(s) and %d ticket(s) written", d.name, a1-a0, k1-k0)
		}
	}
	for _, c := range []struct{ kind, params string }{
		{legalVersionsReadKind, opLegalParams(1, 10)},
		{tenantsReadKind, opTenantsParams("x", 1, 10)},
		{tenantDetailReadKind, opDetailParams(id.String())},
		{tenantPlaquesReadKind, opDetailParams(id.String())},
		{operatorAuditReadKind, opLogParams("", 1, 50)},
	} {
		if _, err := opBegin(t, ctx, tx, hash, c.kind, c.params); err != nil {
			t.Errorf("CONTROL: op_begin_read %q refused its own parameter object: %v", c.kind, err)
		}
	}
}

// -------------------------------------------------- op_read_tenant_billing --

// TestOpReadTenantBilling_EveryMonthIsTheTenantsOwnFigure is OP-12's acceptance, "tutarlar
// tenant önizlemesiyle aynı (fixture'da çapraz test); numeric, float değil", at the SQL level.
// In EACH of three zones -- Europe/Malta, Etc/GMT-14, Etc/GMT+12 -- two tenants:
//   - a FOUNDING tenant at 1.10 (no binary fraction is 1.10) that signed up five local months
//     ago, whose roster straddles last month's boundaries: one person deactivated AT its local
//     start (not billable in it) and one a microsecond after (billable), one activated AT its
//     end (not billable in it) and one a microsecond before (billable), one row whose status
//     disagrees with its stamps (never billable, counted as unstamped) and one invited. Two
//     months are CLOSED through the product's own statement -- the last free month and the
//     first chargeable one -- and AFTER the close the roster grows by one and the price moves
//     to 1.30: the frozen months must not move, the live ones must;
//   - a STANDARD tenant at 1.10 that signed up fourteen months ago, read on pages 1 and 2 (the
//     second reaches back before its signup);
//   - in each FAR zone, an EDGE tenant that signed up three local months ago at an instant whose
//     month in UTC is not its month in the zone: Etc/GMT-14 on the 1st at 00:30 local (still the
//     previous month in UTC), Etc/GMT+12 on the last day at 23:30 local (already the next month
//     in UTC). A signup month taken in UTC instead of the zone moves its first live month by one
//     in either direction -- the read and the tenant's path then disagree on one month, and the
//     read's first month after sign-up is no longer the local signup month (both asserted).
//
// For every month every read returns, the tenant's own path -- the generated store's
// GetBillingPeriod, and PreviewBillingPeriod where there is no frozen row, as tappa_app in the
// tenant's context -- is asked, and the month must agree on every field: tenant name, frozen
// or live, the bounds, the zone, the plan, free_period, first_chargeable_month (live), both
// counts, unit_price and amount_due (exact numeric equality), currency and closed_at
// (frozen), after_signup and period_has_ended; a month before sign-up must carry no figure at
// all. The SCALE of every money value is read in SQL and must be 2; a non-zero amount arrives
// in Go at exponent -2.
// ANTI-VACUITY, asserted: per zone, every kind of month occurs (frozen free, frozen
// chargeable, live free ended and not closed, live chargeable ended and not closed, the
// running month, before sign-up); the boundary month counts exactly the four the construction
// says; the frozen chargeable month differs from today's preview in both its count and its
// price; each edge tenant's signup instant is in another month in UTC than in its zone
// (measured in SQL, not assumed); and at least one zone is on another calendar day than UTC
// while the test runs. That last premise is about a DAY: it does not make the month the tenant
// is in differ from UTC's (only near a month boundary does it), so this test cannot tell a read
// whose newest month is taken in UTC from the shipped one on most days --
// TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant can.
func TestOpReadTenantBilling_EveryMonthIsTheTenantsOwnFigure(t *testing.T) {
	ctx, tx := opTx(t)
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '60s'`); err != nil {
		t.Fatal(err)
	}
	var txStart time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&txStart); err != nil {
		t.Fatal(err)
	}
	var otherDay int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM unnest($1::text[]) AS z
	                             WHERE (clock_timestamp() AT TIME ZONE z)::date <> (clock_timestamp() AT TIME ZONE 'UTC')::date`,
		[]string{opZoneEast, opZoneWest}).Scan(&otherDay); err != nil || otherDay == 0 {
		t.Fatalf("PREMISE: neither far zone is on another calendar day than UTC (%d, err %v)", otherDay, err)
	}
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)

	type read struct {
		f    opBillingFixture
		page int
	}
	var reads []read
	boundary := map[uuid.UUID]time.Time{}
	frozenCharge := map[uuid.UUID]time.Time{}
	for _, zone := range []string{opZoneMalta, opZoneEast, opZoneWest} {
		f := opBillingTenant(t, ctx, tx, "op12 founding "+zone+" "+opToken(t), zone, "founding", "1.10", 5)
		from1, to1 := opMonthBounds(t, ctx, tx, f.month(1), zone)
		signedIn := f.created.Add(time.Hour)
		atFrom, afterFrom := from1, from1.Add(time.Microsecond)
		atTo, beforeTo := to1, to1.Add(-time.Microsecond)
		opHire(t, ctx, tx, f, "all along", "active", &signedIn, nil)
		opHire(t, ctx, tx, f, "left at the boundary", "deactivated", &signedIn, &atFrom)
		opHire(t, ctx, tx, f, "left a microsecond later", "deactivated", &signedIn, &afterFrom)
		opHire(t, ctx, tx, f, "status without its stamp", "active", nil, nil)
		opHire(t, ctx, tx, f, "invited", "invited", nil, nil)
		opHire(t, ctx, tx, f, "joined at the end", "active", &atTo, nil)
		opHire(t, ctx, tx, f, "joined a microsecond before", "active", &beforeTo, nil)
		opCloseMonth(t, ctx, tx, f, f.month(3)) // the last free month
		opCloseMonth(t, ctx, tx, f, f.month(2)) // the first chargeable month
		from2, _ := opMonthBounds(t, ctx, tx, f.month(2), zone)
		late := from2.Add(24 * time.Hour)
		opHire(t, ctx, tx, f, "joined after the close", "active", &late, nil)
		if _, err := tx.Exec(ctx, `UPDATE tenants SET price_per_employee_month = 1.30 WHERE id = $1`, f.id); err != nil {
			t.Fatalf("move the price after the close: %v", err)
		}
		boundary[f.id], frozenCharge[f.id] = f.month(1), f.month(2)
		reads = append(reads, read{f, 1})

		s := opBillingTenant(t, ctx, tx, "op12 standard "+zone+" "+opToken(t), zone, "standard", "1.10", 14)
		from6, _ := opMonthBounds(t, ctx, tx, s.month(6), zone)
		in := s.created.Add(time.Hour)
		at6, after6 := from6, from6.Add(time.Microsecond)
		opHire(t, ctx, tx, s, "all along", "active", &in, nil)
		opHire(t, ctx, tx, s, "left at a boundary", "deactivated", &in, &at6)
		opHire(t, ctx, tx, s, "left a microsecond later", "deactivated", &in, &after6)
		opHire(t, ctx, tx, s, "status without its stamp", "deactivated", &in, nil)
		reads = append(reads, read{s, 1}, read{s, 2})
	}
	// The edge tenants: a signup instant whose month in UTC is not its month in the zone.
	edges := map[uuid.UUID]opBillingFixture{}
	for _, e := range []struct{ zone, into string }{{opZoneEast, "30 minutes"}, {opZoneWest, "1 month -30 minutes"}} {
		f := opBillingTenantAt(t, ctx, tx, "op12 edge "+e.zone+" "+opToken(t), e.zone, "standard", "1.10", 3, e.into)
		var utcMonth time.Time
		if err := tx.QueryRow(ctx, `SELECT date_trunc('month', $1::timestamptz AT TIME ZONE 'UTC')::date`, f.created).Scan(&utcMonth); err != nil {
			t.Fatalf("the edge signup's month in UTC: %v", err)
		}
		if utcMonth.Equal(f.signup) {
			t.Fatalf("PREMISE: %s signed up at %s, in %s both in UTC and in its zone -- the edge is not one",
				f.name, f.created.UTC(), f.signup.Format("2006-01"))
		}
		in := f.created.Add(time.Minute)
		opHire(t, ctx, tx, f, "all along", "active", &in, nil)
		edges[f.id] = f
		reads = append(reads, read{f, 1})
	}

	type kinds struct{ frozenFree, frozenCharge, liveFreeEnded, liveChargeEnded, running, before int }
	seen := map[string]*kinds{}
	compared := 0
	for _, r := range reads {
		if seen[r.f.zone] == nil {
			seen[r.f.zone] = &kinds{}
		}
		k := seen[r.f.zone]
		newestBefore := opLocalMonth(t, ctx, tx, r.f.zone)
		tl, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, r.f.id, r.page, xact), r.f.id, r.page)
		if err != nil {
			t.Fatalf("%s page %d: %v", r.f.name, r.page, err)
		}
		var wall time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&wall); err != nil {
			t.Fatal(err)
		}
		newestAfter := opLocalMonth(t, ctx, tx, r.f.zone)
		months := opMonthsOf(tl)
		offset := (r.page - 1) * TenantBillingMonthsPerPage
		if len(months) != TenantBillingMonthsPerPage || tl.TenantID != r.f.id || tl.TenantName != r.f.name ||
			!(opConsecutiveDesc(months, newestBefore.AddDate(0, -offset, 0)) || opConsecutiveDesc(months, newestAfter.AddDate(0, -offset, 0))) {
			t.Fatalf("%s page %d: %d months %v for %s %q; want twelve consecutive local months, newest first, from the month the tenant is in",
				r.f.name, r.page, len(months), months, tl.TenantID, tl.TenantName)
		}
		scales := opBillingScales(t, ctx, tx, hash, session, a.id, r.f.id, r.page, xact)
		for _, m := range tl.Months {
			label := fmt.Sprintf("%s page %d, %s", r.f.name, r.page, m.Month.Time.Format("2006-01"))
			p := opBillingTenantPath(t, ctx, tx, r.f.id, m.Month)
			for _, d := range opMonthDiffs(m, tl.TenantName, p, txStart, wall) {
				t.Errorf("%s: %s", label, d)
			}
			compared++
			sc := scales[m.Month.Time.Format("2006-01-02")]
			for i, n := range []pgtype.Numeric{m.UnitPrice, m.AmountDue} {
				if n.Valid != (sc[i] != nil) || (sc[i] != nil && *sc[i] != 2) {
					t.Errorf("%s: money column %d has scale %s in SQL (valid in Go %v); want 2", label, i, opDeref(sc[i]), n.Valid)
				}
				if n.Valid && n.Int != nil && n.Int.Sign() != 0 && n.Exp != -2 {
					t.Errorf("%s: money column %d arrives in Go at exponent %d, want -2", label, i, n.Exp)
				}
			}
			switch {
			case m.Frozen && opBoolEq(m.Free, true):
				k.frozenFree++
			case m.Frozen:
				k.frozenCharge++
			case m.AfterSignup && opBoolEq(m.HasEnded, true) && opBoolEq(m.Free, true):
				k.liveFreeEnded++
			case m.AfterSignup && opBoolEq(m.HasEnded, true):
				k.liveChargeEnded++
			case m.AfterSignup:
				k.running++
			default:
				k.before++
			}
			if want, ok := boundary[r.f.id]; ok && m.Month.Time.Equal(want) {
				if !opIntEq(m.EmployeeCount, 4) || !opIntEq(m.UnstampedEmployees, 1) || m.Frozen {
					t.Errorf("%s: the boundary month counts %s (unstamped %s, frozen %v); the construction says 4 and 1, live -- the fixture does not straddle the boundary it was built to",
						label, opDeref(m.EmployeeCount), opDeref(m.UnstampedEmployees), m.Frozen)
				}
			}
			if want, ok := frozenCharge[r.f.id]; ok && m.Month.Time.Equal(want) {
				if !m.Frozen || !p.frozen || p.preview.EmployeeCount == p.get.EmployeeCount || opNumericEqual(p.preview.UnitPrice, p.get.UnitPrice) {
					t.Errorf("%s: PREMISE: the frozen chargeable month (frozen %v) and today's preview agree (count %d/%d, price %s/%s) -- the roster or the price did not move after the close",
						label, m.Frozen, p.get.EmployeeCount, p.preview.EmployeeCount, opNumericText(p.get.UnitPrice), opNumericText(p.preview.UnitPrice))
				}
			}
			if e, ok := edges[r.f.id]; ok {
				if want := !m.Month.Time.Before(e.signup); m.AfterSignup != want || p.preview.PeriodIsAfterSignup != want {
					t.Errorf("%s: after_signup %v, the tenant's path %v; the local signup month is %s, so want %v",
						label, m.AfterSignup, p.preview.PeriodIsAfterSignup, e.signup.Format("2006-01"), want)
				}
			}
		}
	}
	for zone, k := range seen {
		if k.frozenFree == 0 || k.frozenCharge == 0 || k.liveFreeEnded == 0 || k.liveChargeEnded == 0 || k.running == 0 || k.before == 0 {
			t.Errorf("anti-vacuity, %s: %+v; every kind of month must occur", zone, *k)
		}
	}
	if want := (3*3 + len(edges)) * TenantBillingMonthsPerPage; len(edges) != 2 || compared != want {
		t.Errorf("anti-vacuity: %d months compared over %d edge tenants, want %d over 2", compared, len(edges), want)
	}
}

// opBillingResultColumns names the read's eighteen result columns (the RETURNS TABLE list), so
// a query run outside the function can carry the same row shape.
const opBillingResultColumns = "tenant_id, tenant_name, period_month, after_signup, frozen, period_from, period_to, " +
	"period_timezone, plan, first_chargeable_month, free_period, employee_count, unstamped_employees, unit_price, " +
	"currency, amount_due, closed_at, period_has_ended"

// TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant: page 1's newest month is the
// month the tenant is in IN ITS ZONE. Read at the wall clock, a newest month taken in UTC (or in
// any zone but the tenant's) agrees with the zone's on every day but the hours around a month
// boundary, so the core test cannot tell the two apart on most runs. The wall clock cannot be
// moved; the read's clock can. The test takes the read's RETURN QUERY from the catalogue --
// pg_proc.prosrc of the function as it stands in THIS transaction -- binds its two PL/pgSQL
// names (p_tenant_id, v_page) and every clock_timestamp() as parameters, and runs it as
// tappa_opdefiner, the function's owner, under the function's search_path:
//   - CONTROL, the substituted query IS the read: a real read through a forged ticket, for a
//     tenant in each of the three zones, returns, text for text, what the substituted query
//     returns at the instant just before or just after it (the read's clock lies between);
//   - four hours BEFORE the current UTC month began, Etc/GMT-14 is already in that month and
//     UTC is not; five hours AFTER, Etc/GMT+12 is still in the previous month and UTC is not
//     (both PREMISES, measured in SQL): at both instants, for every tenant, the twelve months
//     are consecutive, newest first, from the month the test computes for that instant in
//     that tenant's zone.
//
// Fail-closed: a body whose query reads a clock the substitution does not move (now(),
// statement_timestamp(), CURRENT_DATE, ...) or names a PL/pgSQL variable it does not bind (the
// month computed into a variable before RETURN QUERY, say) fails here instead of passing. What
// this measures is the query's text at an instant, run outside the function; the CONTROL is
// what ties it to the function at the real instant.
func TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)

	var src string
	if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc
	                             WHERE oid = 'public.op_read_tenant_billing(text, text, uuid, integer)'::regprocedure`).Scan(&src); err != nil {
		t.Fatalf("the read's body: %v", err)
	}
	if n := strings.Count(src, "RETURN QUERY"); n != 1 {
		t.Fatalf("the read's body holds %d RETURN QUERY, want exactly 1", n)
	}
	found := regexp.MustCompile(`(?s)RETURN QUERY\s+(.*?);\s*END;\s*$`).FindStringSubmatch(src)
	if found == nil || strings.Contains(found[1], ";") {
		t.Fatal("the read's RETURN QUERY is not the body's last statement, ending at the body's last semicolon")
	}
	clockCalls := strings.Count(found[1], "clock_timestamp()")
	q := strings.ReplaceAll(found[1], "clock_timestamp()", "($3::timestamptz)")
	q = regexp.MustCompile(`\bp_tenant_id\b`).ReplaceAllLiteralString(q, "($1::uuid)")
	q = regexp.MustCompile(`\bv_page\b`).ReplaceAllLiteralString(q, "($2::integer)")
	if clockCalls == 0 {
		t.Fatal("PREMISE: the read's query calls clock_timestamp() nowhere -- there is no clock to move")
	}
	if left := regexp.MustCompile(`\b[pv]_[a-z0-9_]+\b`).FindAllString(q, -1); len(left) > 0 {
		t.Fatalf("the read's query names PL/pgSQL variables the substitution does not bind: %v", left)
	}
	if clocks := regexp.MustCompile(`(?i)\b(now|statement_timestamp|transaction_timestamp|timeofday|clock_timestamp)\s*\(|\b(current_timestamp|current_date|current_time|localtime|localtimestamp)\b`).FindAllString(q, -1); len(clocks) > 0 {
		t.Fatalf("the read's query reads a clock the substitution does not move: %v", clocks)
	}

	// run is the substituted query at an instant: sel over its rows, newest month first.
	run := func(sel string, tenant uuid.UUID, at time.Time) []string {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		for _, s := range []string{`SET LOCAL SESSION AUTHORIZATION tappa_opdefiner`, `SET LOCAL search_path = pg_catalog, pg_temp`} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		rows, err := sp.Query(ctx, "SELECT "+sel+" FROM ("+q+") AS x("+opBillingResultColumns+") ORDER BY x.period_month DESC", tenant, 1, at)
		if err != nil {
			t.Fatalf("the read's query at %s: %v", at.UTC(), err)
		}
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatalf("the read's query at %s: %v", at.UTC(), err)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
		return out
	}
	// zoneMonths is what the test itself says page 1 is at an instant in a zone.
	zoneMonths := func(at time.Time, zone string) []string {
		t.Helper()
		rows, err := tx.Query(ctx, `SELECT (date_trunc('month', $1::timestamptz AT TIME ZONE $2)::date - make_interval(months => g))::date::text
		                              FROM generate_series(0, 11) AS g ORDER BY g`, at, zone)
		if err != nil {
			t.Fatalf("the months at %s in %s: %v", at.UTC(), zone, err)
		}
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatalf("the months at %s in %s: %v", at.UTC(), zone, err)
		}
		return out
	}
	now := func() time.Time {
		t.Helper()
		var at time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}

	var fixtures []opBillingFixture
	for _, zone := range []string{opZoneMalta, opZoneEast, opZoneWest} {
		f := opBillingTenant(t, ctx, tx, "op12 instant "+zone+" "+opToken(t), zone, "standard", "1.10", 3)
		in := f.created.Add(time.Hour)
		opHire(t, ctx, tx, f, "all along", "active", &in, nil)
		fixtures = append(fixtures, f)
	}

	// CONTROL: the substituted query is the read.
	for _, f := range fixtures {
		raw := opForgeBilling(t, ctx, tx, session, a.id, f.id, 1, xact)
		before := now()
		var read []string
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			rows, err := sp.Query(ctx, `SELECT f::text FROM public.op_read_tenant_billing($1, $2, $3, 1) AS f
			                             ORDER BY f.period_month DESC`, hash, raw, f.id)
			if err != nil {
				return err
			}
			read, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			t.Fatalf("%s: the read: %v", f.name, err)
		}
		after := now()
		if len(read) != TenantBillingMonthsPerPage {
			t.Fatalf("%s: the read returned %d rows, want %d", f.name, len(read), TenantBillingMonthsPerPage)
		}
		if atBefore, atAfter := run("x::text", f.id, before), run("x::text", f.id, after); !slices.Equal(read, atBefore) && !slices.Equal(read, atAfter) {
			t.Errorf("CONTROL, %s: the read's own query, run outside it at the instants either side of the read, returns other rows:\n read   %q\n before %q\n after  %q",
				f.name, read, atBefore, atAfter)
		}
	}

	// Either side of a UTC month boundary.
	var eastAt, westAt time.Time
	if err := tx.QueryRow(ctx, `SELECT u - interval '4 hours', u + interval '5 hours'
	                              FROM (SELECT date_trunc('month', clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS u) AS b`).Scan(&eastAt, &westAt); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		at   time.Time
		zone string
	}{{eastAt, opZoneEast}, {westAt, opZoneWest}} {
		if zoneMonths(p.at, p.zone)[0] == zoneMonths(p.at, "UTC")[0] {
			t.Fatalf("PREMISE: at %s %s is in the same month as UTC -- the instant does not separate them", p.at.UTC(), p.zone)
		}
	}
	for _, at := range []time.Time{eastAt, westAt} {
		for _, f := range fixtures {
			if got, want := run("x.period_month::text", f.id, at), zoneMonths(at, f.zone); !slices.Equal(got, want) {
				t.Errorf("%s at %s (UTC): page 1 is %v; in %s it is %v", f.name, at.UTC().Format(time.RFC3339), got, f.zone, want)
			}
		}
	}
}

// TestOpReadTenantBilling_ReturnsOnlyTheNamedTenantsFigures is ADR 0021 §6 "kemer" for the
// read: no row level security applies inside it, so the tenant filter on each of its three
// table references is the only thing between one tenant's invoice and another's. Two tenants
// in one zone, each with a roster of a different size, a different price and the SAME month
// closed: each tenant's read carries only its own tenant id and name, exactly ONE frozen month
// with its own figures (not the other's, which exists for the same month) and, in every month
// after signup, its own headcount. A statement timeout bounds the test: a dropped roster filter
// counts every tenant's employees in the database.
func TestOpReadTenantBilling_ReturnsOnlyTheNamedTenantsFigures(t *testing.T) {
	ctx, tx := opTx(t)
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '30s'`); err != nil {
		t.Fatal(err)
	}
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	type side struct {
		f      opBillingFixture
		staff  int
		price  string
		amount string
	}
	sides := []*side{{staff: 2, price: "1.10", amount: "2.20"}, {staff: 5, price: "2.00", amount: "10.00"}}
	for i, s := range sides {
		s.f = opBillingTenant(t, ctx, tx, "op12 belt "+string(rune('A'+i))+" "+opToken(t), opZoneMalta, "standard", s.price, 6)
		in := s.f.created.Add(time.Hour)
		for j := 0; j < s.staff; j++ {
			opHire(t, ctx, tx, s.f, "staff "+strconv.Itoa(j), "active", &in, nil)
		}
		opCloseMonth(t, ctx, tx, s.f, s.f.month(2))
	}
	if !sides[0].f.month(2).Equal(sides[1].f.month(2)) {
		t.Fatal("PREMISE: the two tenants' closed months are not the same month")
	}
	for i, s := range sides {
		other := sides[1-i]
		tl, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, s.f.id, 1, xact), s.f.id, 1)
		if err != nil {
			t.Fatalf("%s: %v", s.f.name, err)
		}
		if tl.TenantID != s.f.id || tl.TenantName != s.f.name || len(tl.Months) != TenantBillingMonthsPerPage {
			t.Fatalf("%s: read %s %q with %d months", s.f.name, tl.TenantID, tl.TenantName, len(tl.Months))
		}
		frozen := 0
		for _, m := range tl.Months {
			if !m.AfterSignup {
				continue
			}
			label := s.f.name + " " + m.Month.Time.Format("2006-01")
			if !opIntEq(m.EmployeeCount, int32(s.staff)) {
				t.Errorf("%s: %s employees, want its own %d (the other tenant has %d)", label, opDeref(m.EmployeeCount), s.staff, other.staff)
			}
			if m.Frozen {
				frozen++
				var want pgtype.Numeric
				if err := want.Scan(s.amount); err != nil {
					t.Fatal(err)
				}
				if !opNumericEqual(m.AmountDue, want) {
					t.Errorf("%s: the frozen amount is %s, want its own %s (the other tenant's is %s)", label, opNumericText(m.AmountDue), s.amount, other.amount)
				}
			}
		}
		if frozen != 1 {
			t.Errorf("%s: %d frozen months, want exactly its own one (the other tenant's row for the same month must not join)", s.f.name, frozen)
		}
	}
}

// TestOpReadTenantBilling_AnUnknownTenantReadsNothingAndAKnownOneTwelveMonths: one read, three
// answers. An id no tenant has reads ZERO rows -- ErrNoSuchTenant through the product's scan --
// after its 'read' row was written (phase one does not look the tenant up). A tenant reads
// EXACTLY twelve months, its name on every row: one with no employee that signed up two
// months ago reads live months counting 0 at 0.00 (not NULL, not an error); one whose signup
// lies in the future reads twelve months before sign-up, every figure NULL.
func TestOpReadTenantBilling_AnUnknownTenantReadsNothingAndAKnownOneTwelveMonths(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	ghost := uuid.New()
	raw, err := opBegin(t, ctx, tx, hash, tenantBillingReadKind, opBillingParams(ghost.String(), 1))
	if err != nil {
		t.Fatalf("phase one for an unknown tenant: %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1 AND target_tenant_id = $2 AND target_scope = 'tenant_billing'`, session, ghost); n != 1 {
		t.Errorf("phase one for an unknown tenant wrote %d 'read' row(s) naming it, want 1", n)
	}
	if _, err := tx.Exec(ctx, `UPDATE operator_read_tickets SET created_xact = $1::xid8 WHERE ticket_hash = $2`, xact, opBillingTicketHash(raw, ghost, 1)); err != nil {
		t.Fatal(err)
	}
	if rows, err := opRawBilling(t, ctx, tx, hash, raw, ghost, 1); err != nil || len(rows) != 0 {
		t.Errorf("an unknown tenant: %d row(s), err %v; want 0 and no error", len(rows), err)
	}
	if _, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, ghost, 1, xact), ghost, 1); !errors.Is(err, ErrNoSuchTenant) {
		t.Errorf("an unknown tenant through the product's scan: %v, want ErrNoSuchTenant", err)
	}

	empty := opBillingTenant(t, ctx, tx, "op12 empty "+opToken(t), opZoneMalta, "standard", "1.50", 2)
	future := opBillingTenant(t, ctx, tx, "op12 future "+opToken(t), opZoneMalta, "standard", "1.50", 0)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET created_at = clock_timestamp() + interval '70 days' WHERE id = $1`, future.id); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		f    opBillingFixture
		live int
	}{{empty, 3}, {future, 0}} {
		tl, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, c.f.id, 1, xact), c.f.id, 1)
		if err != nil {
			t.Fatalf("%s: %v", c.f.name, err)
		}
		if len(tl.Months) != TenantBillingMonthsPerPage || tl.TenantName != c.f.name || tl.TenantID != c.f.id {
			t.Fatalf("%s: %d months for %q", c.f.name, len(tl.Months), tl.TenantName)
		}
		live := 0
		for _, m := range tl.Months {
			if !m.AfterSignup {
				if m.EmployeeCount != nil || m.AmountDue.Valid || m.UnitPrice.Valid {
					t.Errorf("%s %s: a month before sign-up carries a figure", c.f.name, m.Month.Time.Format("2006-01"))
				}
				continue
			}
			live++
			var zero pgtype.Numeric
			if err := zero.Scan("0.00"); err != nil {
				t.Fatal(err)
			}
			if !opIntEq(m.EmployeeCount, 0) || !opIntEq(m.UnstampedEmployees, 0) || !opNumericEqual(m.AmountDue, zero) {
				t.Errorf("%s %s: a tenant with no employee reads count %s, unstamped %s, amount %s; want 0, 0, 0.00",
					c.f.name, m.Month.Time.Format("2006-01"), opDeref(m.EmployeeCount), opDeref(m.UnstampedEmployees), opNumericText(m.AmountDue))
			}
		}
		if live != c.live {
			t.Errorf("%s: %d live months, want %d", c.f.name, live, c.live)
		}
	}
}

// TestOpReadTenantBilling_AnUnknownZoneIsAnErrorNotAZeroInvoice: a tenant whose stored zone the
// server does not recognise (the development database holds such residue; nothing in the schema
// refuses one) cannot be read -- through the product's accessor the read is a 22023 database
// error, not ErrNoSuchTenant, not the operator's refusal and not a page of zeros -- and the
// tenant's own preview fails the same way, in the same helper. The ticket is not consumed (the
// failed statement takes its UPDATE with it). ADR 0021's OP-12 note, limit L6.
func TestOpReadTenantBilling_AnUnknownZoneIsAnErrorNotAZeroInvoice(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := opBillingTenant(t, ctx, tx, "op12 zone "+opToken(t), opZoneMalta, "standard", "1.10", 3)
	in := f.created.Add(time.Hour)
	opHire(t, ctx, tx, f, "a", "active", &in, nil)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET timezone = 'Zz/Not_A_Zone' WHERE id = $1`, f.id); err != nil {
		t.Fatalf("store an unrecognised zone: %v", err)
	}
	tl, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, f.id, 1, opCommittedXact(t, ctx)), f.id, 1)
	if err == nil || errors.Is(err, ErrNoSuchTenant) || errors.Is(err, ErrOperatorRefused) || !strings.Contains(err.Error(), "SQLSTATE 22023") {
		t.Errorf("the read of a tenant with an unrecognised zone: %d month(s), err %v; want a 22023 database error", len(tl.Months), err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, session); n != 0 {
		t.Errorf("%d ticket(s) consumed by the failed read", n)
	}
	perr := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.id.String()); err != nil {
			return err
		}
		_, err := store.New(sp).PreviewBillingPeriod(ctx, store.PreviewBillingPeriodParams{
			PeriodMonth: pgtype.Date{Time: f.month(1), Valid: true}, TenantID: f.id})
		return err
	})
	opWant(t, perr, sqlstateInvalidParameter, "the tenant's own preview with an unrecognised zone")
}

// TestOpReadTenantBilling_MoneyIsNumericAtScaleTwo is CLAUDE.md §6 for the read, three ways:
//   - in SQL: scale(unit_price) and scale(amount_due) are 2 on every row that has them -- a free
//     live month (0.00: tappa_billing_amount_due answers 0::numeric, scale 0, and the read's
//     ::numeric(12, 2) is what sets it), a chargeable live month, a frozen one; RETURNS TABLE
//     dropped the declared type modifier, so this is the expression's doing;
//   - in Go: a non-zero amount arrives as mantissa x 10^-2 (pgx carries no scale for a zero --
//     a numeric with no digits arrives as 0 x 10^0 -- which is why the SQL half exists);
//   - the row type: no field of TenantBillingMonth is a float, at any depth of pointer.
func TestOpReadTenantBilling_MoneyIsNumericAtScaleTwo(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	f := opBillingTenant(t, ctx, tx, "op12 money "+opToken(t), opZoneMalta, "founding", "1.10", 4)
	in := f.created.Add(time.Hour)
	for i := 0; i < 3; i++ {
		opHire(t, ctx, tx, f, "staff "+strconv.Itoa(i), "active", &in, nil)
	}
	opCloseMonth(t, ctx, tx, f, f.month(1))
	scales := opBillingScales(t, ctx, tx, hash, session, a.id, f.id, 1, xact)
	tl, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, f.id, 1, xact), f.id, 1)
	if err != nil {
		t.Fatal(err)
	}
	var freeLive, chargeLive, frozen int
	for _, m := range tl.Months {
		if !m.AfterSignup {
			continue
		}
		sc := scales[m.Month.Time.Format("2006-01-02")]
		if sc[0] == nil || sc[1] == nil || *sc[0] != 2 || *sc[1] != 2 {
			t.Errorf("%s: scale(unit_price)=%s scale(amount_due)=%s, want 2 and 2", m.Month.Time.Format("2006-01"), opDeref(sc[0]), opDeref(sc[1]))
		}
		switch {
		case m.Frozen:
			frozen++
		case opBoolEq(m.Free, true):
			freeLive++
		default:
			chargeLive++
		}
		if m.AmountDue.Valid && m.AmountDue.Int != nil && m.AmountDue.Int.Sign() != 0 && m.AmountDue.Exp != -2 {
			t.Errorf("%s: amount %s arrives at exponent %d, want -2", m.Month.Time.Format("2006-01"), opNumericText(m.AmountDue), m.AmountDue.Exp)
		}
	}
	if freeLive == 0 || chargeLive == 0 || frozen == 0 {
		t.Errorf("anti-vacuity: free live %d, chargeable live %d, frozen %d; each must occur", freeLive, chargeLive, frozen)
	}
	var floats []string
	var walk func(path string, typ reflect.Type)
	walk = func(path string, typ reflect.Type) {
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		switch typ.Kind() {
		case reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
			floats = append(floats, path)
		case reflect.Struct:
			if typ.PkgPath() != "" && typ.PkgPath() != reflect.TypeOf(TenantBillingMonth{}).PkgPath() {
				return // pgtype.Numeric, pgtype.Date, time.Time: their own types, not a float field of the row
			}
			for i := 0; i < typ.NumField(); i++ {
				walk(path+"."+typ.Field(i).Name, typ.Field(i).Type)
			}
		}
	}
	walk("TenantBillingMonth", reflect.TypeOf(TenantBillingMonth{}))
	walk("TenantBillingTimeline", reflect.TypeOf(TenantBillingTimeline{}))
	if len(floats) != 0 {
		t.Errorf("float fields on the billing read's types: %v", floats)
	}
	for _, f := range []string{"UnitPrice", "AmountDue"} {
		if ft, _ := reflect.TypeOf(TenantBillingMonth{}).FieldByName(f); ft.Type != reflect.TypeOf(pgtype.Numeric{}) {
			t.Errorf("TenantBillingMonth.%s is %v, want pgtype.Numeric", f, ft.Type)
		}
	}
}

// TestOpReadTenantBilling_PagesAreBoundedInTheBody: op_begin_read refuses a page outside
// 1..MaxTenantBillingPage, and the read's body does not trust that -- a ticket the owner forges
// past phase one reads the nearest bound: page 0 and a NULL page read page 1, page
// MaxTenantBillingPage+1 and the largest integer read page MaxTenantBillingPage (no date
// arithmetic overflows). CONTROL: pages 1..MaxTenantBillingPage are consecutive runs of twelve.
func TestOpReadTenantBilling_PagesAreBoundedInTheBody(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	f := opBillingTenant(t, ctx, tx, "op12 pages "+opToken(t), opZoneWest, "standard", "1.10", 70)
	pages := map[int][]time.Time{}
	for p := 1; p <= MaxTenantBillingPage; p++ {
		tl, err := opReadBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, f.id, p, xact), f.id, p)
		if err != nil {
			t.Fatalf("page %d: %v", p, err)
		}
		pages[p] = opMonthsOf(tl)
		if p > 1 && !pages[p][0].Equal(pages[p-1][TenantBillingMonthsPerPage-1].AddDate(0, -1, 0)) {
			t.Errorf("CONTROL: page %d does not continue page %d", p, p-1)
		}
	}
	for _, c := range []struct {
		name string
		page any
		want int
	}{
		{"page 0", 0, 1},
		{"page MaxTenantBillingPage+1", MaxTenantBillingPage + 1, MaxTenantBillingPage},
		{"the largest integer", int32(2147483647), MaxTenantBillingPage},
		{"a NULL page", nil, 1},
	} {
		raw := opRandHex(t)
		canonical := `{"tenant_id": "` + f.id.String() + `", "page_number": null}`
		if c.page != nil {
			canonical = fmt.Sprintf(`{"tenant_id": "%s", "page_number": %v}`, f.id.String(), c.page)
		}
		sum := sha256.Sum256([]byte(raw + canonical))
		opForgeRead(t, ctx, tx, session, a.id, tenantBillingReadKind, hex.EncodeToString(sum[:]), &f.id, xact, "30 seconds")
		got, err := opRawBilling(t, ctx, tx, hash, raw, f.id, c.page)
		if err != nil {
			t.Errorf("%s through a forged ticket: %v", c.name, err)
			continue
		}
		if !slices.EqualFunc(got, pages[c.want], func(x, y time.Time) bool { return x.Equal(y) }) {
			t.Errorf("%s reads %v, want page %d's months", c.name, got, c.want)
		}
	}
}

// TestOpReadTenantBilling_ATicketFromThisTransactionIsRefused is ADR 0021 §2 v 3's rows A1/A2 for
// the read: a ticket op_begin_read made in THIS transaction -- at the top level, in an open
// savepoint, in a released one -- is refused (28000). CONTROL: a ticket whose transaction
// committed is read.
func TestOpReadTenantBilling_ATicketFromThisTransactionIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := uuid.New()
	begin := func(q pgx.Tx) string {
		t.Helper()
		raw, err := opBegin(t, ctx, q, hash, tenantBillingReadKind, opBillingParams(tenant.String(), 1))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := begin(tx)
	_, err := opRawBilling(t, ctx, tx, hash, raw, tenant, 1)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantBillingRefusal, "A1: a ticket of this transaction's top level", raw)
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw = begin(sp)
	_, err = opRawBilling(t, ctx, sp, hash, raw, tenant, 1)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantBillingRefusal, "A2: a ticket of an open savepoint", raw)
	if err := sp.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = opRawBilling(t, ctx, tx, hash, raw, tenant, 1)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantBillingRefusal, "A2: a ticket of a released savepoint", raw)
	if _, err := opRawBilling(t, ctx, tx, hash, opForgeBilling(t, ctx, tx, session, a.id, tenant, 1, opCommittedXact(t, ctx)), tenant, 1); err != nil {
		t.Fatalf("CONTROL: a ticket whose transaction COMMITTED was refused: %v", err)
	}
}

// TestOpReadTenantBilling_AForgedTicketIsRefused: a ticket is bound to its session, its KIND,
// its tenant and its page. Refused (28000, ticket not consumed): another session of the same
// operator; another tenant; another page; a ticket no op_begin_read issued, and NULL; the
// KIND condition's own case -- the billing read's own hash under every other read kind -- and,
// the other direction, a billing ticket shown to the plaque inventory and to the overview with
// THEIR hashes. CONTROL: the right session, tenant and page are read.
func TestOpReadTenantBilling_AForgedTicketIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	otherHash, _ := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	tenant, other := uuid.New(), uuid.New()
	consumed := func() int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, session)
	}
	before := consumed()
	ticket := opForgeBilling(t, ctx, tx, session, a.id, tenant, 2, xact)
	for _, c := range []struct {
		name, hash string
		tenant     uuid.UUID
		page       int
	}{
		{"another session", otherHash, tenant, 2},
		{"another tenant", hash, other, 2},
		{"another page", hash, tenant, 3},
	} {
		_, err := opRawBilling(t, ctx, tx, c.hash, ticket, c.tenant, c.page)
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantBillingRefusal, c.name, ticket)
	}
	for _, raw := range []any{opRandHex(t), nil} {
		_, err := opRawBilling(t, ctx, tx, hash, raw, tenant, 2)
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantBillingRefusal, fmt.Sprintf("a ticket never issued (%T)", raw))
	}
	for _, kind := range []string{legalVersionsReadKind, tenantsReadKind, tenantDetailReadKind, tenantPlaquesReadKind, operatorAuditReadKind} {
		raw := opRandHex(t)
		opForgeRead(t, ctx, tx, session, a.id, kind, opBillingTicketHash(raw, tenant, 2), &tenant, xact, "30 seconds")
		_, err := opRawBilling(t, ctx, tx, hash, raw, tenant, 2)
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantBillingRefusal, "the billing read's own hash under kind '"+kind+"'", raw)
	}
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, a.id, tenantBillingReadKind, opDetailTicketHash(raw, tenant), &tenant, xact, "30 seconds")
	_, err := opReadPlaques(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, "op_read_tenant_plaques with a 'tenant_billing' ticket", raw)
	_, err = opReadDetail(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantDetailRefusal, "op_read_tenant_detail with a 'tenant_billing' ticket", raw)
	if n := consumed() - before; n != 0 {
		t.Errorf("%d ticket(s) consumed by refused reads", n)
	}
	if _, err := opRawBilling(t, ctx, tx, hash, ticket, tenant, 2); err != nil {
		t.Errorf("CONTROL: the ticket with its own session, tenant and page was refused: %v", err)
	}
}

// TestOpReadTenantBilling_RefusesEveryDeadSession: the read resolves the session through
// op_touch_session before anything else, so each of the six dead sessions is the touch refusal
// (28000) and no ticket is consumed -- even one that would otherwise match.
func TestOpReadTenantBilling_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	xact := opCommittedXact(t, ctx)
	tenant := uuid.New()
	for _, d := range opDeadSessions(t, ctx, tx) {
		if d.id == uuid.Nil {
			_, err := opRawBilling(t, ctx, tx, d.hash, opRandHex(t), tenant, 1)
			opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash)
			continue
		}
		var admin uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT admin_id FROM platform_sessions WHERE id = $1`, d.id).Scan(&admin); err != nil {
			t.Fatal(err)
		}
		raw := opForgeBilling(t, ctx, tx, d.id, admin, tenant, 1, xact)
		_, err := opRawBilling(t, ctx, tx, d.hash, raw, tenant, 1)
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash, raw)
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, d.id); n != 0 {
			t.Errorf("%s: %d ticket(s) consumed", d.name, n)
		}
	}
}

// TestOpReadTenantBilling_ExpiryIsTheWallClock is ADR 0021 §6 "bilet süresi" for the read: an
// expired ticket is refused, and a ticket that expires WHILE the reading transaction is open is
// refused -- through savepoints rolled back three times, and through three exception
// sub-transactions of ONE DO statement whose sleep is INSIDE it (a frozen statement_timestamp()
// would still call the ticket alive). CONTROL first: the same ticket, unexpired, is read (in a
// savepoint that is rolled back, so it stays unconsumed).
func TestOpReadTenantBilling_ExpiryIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := uuid.New()
	ticket := opForgeBilling(t, ctx, tx, session, a.id, tenant, 1, opCommittedXact(t, ctx))
	expireIn := func(interval string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `UPDATE operator_read_tickets SET expires_at = clock_timestamp() + $2::interval WHERE session_id = $1`, session, interval); err != nil {
			t.Fatalf("set the expiry: %v", err)
		}
	}
	readThenUndo := func() error {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, e := opRawBilling(t, ctx, sp, hash, ticket, tenant, 1)
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return e
	}
	if err := readThenUndo(); err != nil {
		t.Fatalf("CONTROL: the unexpired ticket was refused: %v", err)
	}
	expireIn("-1 second")
	opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, tenantBillingRefusal, "an expired ticket", ticket)

	expireIn("1 second")
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, tenantBillingRefusal,
			"savepoint "+strconv.Itoa(i+1)+" after the ticket expired in the open transaction", ticket)
	}

	expireIn("1 second")
	var result string
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('tappa_test.session', $1, true), set_config('tappa_test.ticket', $2, true),
		                                  set_config('tappa_test.tenant', $3, true)`, hash, ticket, tenant.String()); err != nil {
			return err
		}
		if _, err := sp.Exec(ctx, `
			DO $d$
			DECLARE
			    n_data    integer := 0;
			    n_refused integer := 0;
			BEGIN
			    PERFORM pg_sleep(1.5);
			    FOR i IN 1..3 LOOP
			        BEGIN
			            PERFORM * FROM public.op_read_tenant_billing(current_setting('tappa_test.session'),
			                                                         current_setting('tappa_test.ticket'),
			                                                         current_setting('tappa_test.tenant')::uuid, 1);
			            n_data := n_data + 1;
			        EXCEPTION WHEN invalid_authorization_specification THEN
			            n_refused := n_refused + 1;
			        END;
			    END LOOP;
			    PERFORM set_config('tappa_test.result', n_data || '/' || n_refused, true);
			END
			$d$`); err != nil {
			return err
		}
		return sp.QueryRow(ctx, `SELECT current_setting('tappa_test.result')`).Scan(&result)
	}); err != nil {
		t.Fatalf("the DO block: %v", err)
	}
	if result != "0/3" {
		t.Errorf("inside one DO statement whose sleep outlived the ticket: data/refused = %s, want 0/3 (a frozen clock reads the ticket alive)", result)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, session); n != 0 {
		t.Errorf("%d expired ticket(s) consumed", n)
	}
}

// --------------------------------------------------------- with real commits --

// opCommittedTenantWithoutStaff is a tenant that exists in the committed database and has no
// employee row, so its read is cheap and these tests write no tenant data. "" if none exists.
func opCommittedTenantWithoutStaff(t *testing.T, ctx context.Context, q opQuerier) (uuid.UUID, string, bool) {
	t.Helper()
	var id uuid.UUID
	var name string
	err := q.QueryRow(ctx, `SELECT t.id, t.name FROM tenants t
	                         WHERE NOT EXISTS (SELECT 1 FROM employees e WHERE e.tenant_id = t.id)
	                         ORDER BY t.id LIMIT 1`).Scan(&id, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", false
	}
	if err != nil {
		t.Fatalf("find a committed tenant without employees: %v", err)
	}
	return id, name, true
}

// TestOpReadTenantBilling_TwoPhaseLifecycle is ADR 0021 §2 v 3's table on the shipped read with
// REAL commits: the accessor's phase one (beginOperatorRead) is committed and its 'read' row --
// scope 'tenant_billing', the tenant, page 1 of size 12, detail {} -- is permanent; another
// session, another tenant and another page are refused; the accessor's phase two
// (readTenantBilling) returns the page; the rolled-back read leaves the row and -- ADR 0021
// limit 4 -- lets the same ticket read again; the committed read consumes the ticket, after
// which it is refused; and through all of it the read has exactly ONE 'read' row. It reads a
// committed tenant without employees, or (with none in the database) an id no tenant has.
func TestOpReadTenantBilling_TwoPhaseLifecycle(t *testing.T) {
	ctx, f := opLiveFixture(t)
	otherHash, otherSession := f.newSession(t, ctx)
	tenant, name, known := opCommittedTenantWithoutStaff(t, ctx, f.owner)
	if !known {
		tenant = uuid.New()
	}
	conn := f.connect(t, ctx)

	tx := asOperatorTx(t, ctx, conn)
	ticket, err := beginOperatorRead(ctx, tx, f.hash, tenantBillingReadKind, []byte(opBillingParams(tenant.String(), 1)))
	if err != nil {
		t.Fatalf("phase one: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit phase one: %v", err)
	}
	scoped := func() int64 {
		return opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1
		                                 AND target_scope = 'tenant_billing' AND target_tenant_id = $2
		                                 AND page_number = 1 AND page_size = 12 AND detail = '{}'::jsonb`, f.session, tenant)
	}
	if n := scoped(); n != 1 {
		t.Fatalf("after the committed phase one: %d 'tenant_billing' row(s), want 1", n)
	}
	refused := func(what, hash string, id uuid.UUID, page int) {
		t.Helper()
		rtx := asOperatorTx(t, ctx, conn)
		_, e := opScanBilling(ctx, rtx, hash, ticket.reveal(), id, page)
		opWantClean(t, e, sqlstateInvalidAuthorization, tenantBillingRefusal, what)
		if err := rtx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refused("the ticket from another session of the same operator", otherHash, tenant, 1)
	refused("the ticket with another tenant", f.hash, uuid.New(), 1)
	refused("the ticket with another page", f.hash, tenant, 2)

	read := func(commit bool) (TenantBillingTimeline, error) {
		t.Helper()
		rtx := asOperatorTx(t, ctx, conn)
		tl, rerr := readTenantBilling(ctx, rtx, f.hash, ticket, tenant, 1)
		if commit {
			if err := rtx.Commit(ctx); err != nil {
				t.Fatalf("commit the read: %v", err)
			}
		} else if err := rtx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return tl, rerr
	}
	tl, err := read(false)
	switch {
	case known && (err != nil || len(tl.Months) != TenantBillingMonthsPerPage || tl.TenantName != name):
		t.Errorf("the read (rolled back) of a committed tenant: %d months, name %q, err %v; want twelve and its name", len(tl.Months), tl.TenantName, err)
	case !known && !errors.Is(err, ErrNoSuchTenant):
		t.Errorf("the read (rolled back) of an unknown id: %v, want ErrNoSuchTenant", err)
	}
	if n := scoped(); n != 1 {
		t.Errorf("after the rolled-back read: %d row(s), want the 1 committed by phase one", n)
	}
	if _, err := read(true); err != nil && !(!known && errors.Is(err, ErrNoSuchTenant)) {
		t.Errorf("the committed read after a rolled-back one: %v", err)
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, f.session); n != 1 {
		t.Errorf("after the committed read %d ticket(s) of the session are consumed, want 1", n)
	}
	refused("the ticket after a COMMITTED read consumed it", f.hash, tenant, 1)
	if n := scoped(); n != 1 {
		t.Errorf("at the end: %d 'read' row(s), want exactly 1", n)
	}
	if n := f.liveReads(t, ctx, otherSession); n != 0 {
		t.Errorf("the other session has %d 'read' row(s)", n)
	}
}

// TestTenantBilling_OnThePoolTheTwoPhasesAreTwoTransactions: on a pool built by the production
// constructor (*pgxpool.Pool -- what *OperatorDB holds) TenantBilling's two phases are two
// transactions: pages 1 and 2 of a committed tenant are twelve consecutive months each, the
// second continuing the first, with the tenant's name; an id no tenant has is ErrNoSuchTenant
// with its 'read' row committed. Inside one transaction it is ErrOperatorRefused; an unknown
// session is ErrOperatorRefused; page 0 and page MaxTenantBillingPage+1 are 22023 database
// errors -- none of the refused calls writes a row. It commits three 'read' rows (two with no
// committed tenant in the database: then the pages are skipped and the unknown id stays).
func TestTenantBilling_OnThePoolTheTwoPhasesAreTwoTransactions(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("open the operator pool: %v", err)
	}
	defer o.Close()
	want := int64(0)
	if tenant, name, known := opCommittedTenantWithoutStaff(t, ctx, f.owner); known {
		var pages [][]time.Time
		for p := int32(1); p <= 2; p++ {
			tl, err := TenantBilling(ctx, o.pool, f.hash, tenant, p)
			if err != nil {
				t.Fatalf("TenantBilling page %d on the pool: %v", p, err)
			}
			want++
			if tl.TenantID != tenant || tl.TenantName != name || tl.Page != p || len(tl.Months) != TenantBillingMonthsPerPage ||
				!opConsecutiveDesc(opMonthsOf(tl), tl.Months[0].Month.Time) {
				t.Errorf("page %d: %s %q page %d, %d months %v", p, tl.TenantID, tl.TenantName, tl.Page, len(tl.Months), opMonthsOf(tl))
			}
			pages = append(pages, opMonthsOf(tl))
		}
		if len(pages) == 2 && !pages[1][0].Equal(pages[0][TenantBillingMonthsPerPage-1].AddDate(0, -1, 0)) {
			t.Error("page 2 does not continue page 1")
		}
	} else {
		t.Log("no committed tenant without employees: the page half is skipped (an empty database)")
	}
	if _, err := TenantBilling(ctx, o.pool, f.hash, uuid.New(), 1); !errors.Is(err, ErrNoSuchTenant) {
		t.Errorf("TenantBilling of an unknown id: %v, want ErrNoSuchTenant", err)
	}
	want++
	if n := f.liveReads(t, ctx, f.session); n != want {
		t.Errorf("the session has %d committed 'read' row(s), want %d", n, want)
	}

	tx := asOperatorTx(t, ctx, f.connect(t, ctx))
	if _, err := TenantBilling(ctx, tx, f.hash, uuid.New(), 1); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantBilling inside ONE transaction: %v, want ErrOperatorRefused", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := TenantBilling(ctx, o.pool, opRandHex(t), uuid.New(), 1); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantBilling with an unknown session: %v, want ErrOperatorRefused", err)
	}
	for _, p := range []int32{0, MaxTenantBillingPage + 1} {
		_, err := TenantBilling(ctx, o.pool, f.hash, uuid.New(), p)
		if err == nil || errors.Is(err, ErrOperatorRefused) || !strings.Contains(err.Error(), "SQLSTATE 22023") {
			t.Errorf("TenantBilling page %d: %v, want a 22023 database error", p, err)
		}
	}
	if n := f.liveReads(t, ctx, f.session); n != want {
		t.Errorf("after the refused calls the session has %d committed 'read' row(s), want still %d", n, want)
	}
}
