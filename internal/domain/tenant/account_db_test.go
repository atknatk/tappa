package tenant

// account_db_test.go -- M7-05's read and write against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and each property below is one a mock would agree
// with unconditionally:
//
//   - the UPDATE touches THREE COLUMNS and leaves five alone. The privilege that would
//     stop a fourth lives in migration 00016 and 00017, in the database; a double would
//     happily "store" a plan or a VAT number and report success.
//   - the audit row SHARES the write's transaction. `tenants` has no updated_at
//     (migration 00001), so if the trail can be lost while the change survives, the zone
//     every shift is judged against can move with NO TRACE ANYWHERE. Measured by
//     breaking the trail and re-reading the row.
//   - section 4.5. A second business's context cannot read or rewrite this one's row,
//     and the reason is RLS plus the explicit predicate -- neither of which exists in a
//     fake.
//   - the business_type CHECK. The Go side keeps no second copy of the eight values on
//     purpose, so the ONLY thing that refuses a ninth is the database.
//   - tappa_app holds NO UPDATE on vat_verified / vat_checked_at. That is the M7-05
//     card's third ask, measured as still absent rather than argued.
//
// FIXTURES ARE NOT CLEANED UP, for the reason staff_db_test.go states: audit_log is
// append-only (00005) and deleting the rest would need the migration role. Fresh random
// ids keep runs from colliding, and `make db-reset` clears the development database.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// accountFixture is one business, plus a SECOND one to attack it from.
type accountFixture struct {
	data     *db.DB
	accounts *Accounts
	trail    *audit.Recorder

	tenantID uuid.UUID
	actorID  uuid.UUID

	foreignTenant uuid.UUID
}

func newAccountFixture(t *testing.T) *accountFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping account tests (real Postgres required)")
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
	accounts, err := NewAccounts(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAccounts: %v", err)
	}
	f := &accountFixture{
		data: data, accounts: accounts, trail: trail,
		tenantID: uuid.New(), actorID: uuid.New(), foreignTenant: uuid.New(),
	}
	f.seed(t, f.tenantID)
	f.seed(t, f.foreignTenant)
	return f
}

// seed writes one business. THE VAT COLUMNS ARE WRITTEN because migration 00017 grants
// tappa_app INSERT (and only INSERT) on them, which is the same privilege the sign-up
// wizard uses -- so the fixture reaches the row the same way the product does.
func (f *accountFixture) seed(t *testing.T, tenantID uuid.UUID) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone,
			                      vat_verified, vat_checked_at)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi', 'Europe/Malta', true, now())`,
			tenantID, "VAT-"+tenantID.String())
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

// row reads the columns this file asserts on, as the DATABASE holds them.
func (f *accountFixture) row(t *testing.T, tenantID uuid.UUID) (name, vat, businessType, structure, zone, plan, price string) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT name, vat_number, business_type, structure, timezone, plan,
			        price_per_employee_month::text
			   FROM tenants WHERE id = $1`, tenantID).
			Scan(&name, &vat, &businessType, &structure, &zone, &plan, &price)
	})
	if err != nil {
		t.Fatalf("read tenants row: %v", err)
	}
	return
}

// trailFor returns EVERY audit detail for one action in this tenant, with jsonb's
// rendering whitespace removed.
//
// IT RETURNS THE WHOLE SET RATHER THAN THE NEWEST ROW, because `audit_log.at` defaults
// to now() — transaction START time — and a test that assumed an order would be a test
// whose colour depends on clock resolution.
func (f *accountFixture) trailFor(t *testing.T, tenantID uuid.UUID, action string) (int, []string) {
	t.Helper()
	var out []string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			tenantID, action)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if e := rows.Scan(&d); e != nil {
				return e
			}
			out = append(out, compactJSON(d))
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read trail: %v", err)
	}
	return len(out), out
}

// --- what a save may and may not touch --------------------------------------------

// TestAccountDB_SavesTheThreeFieldsAndLeavesTheRestAlone is the shape of this whole
// task in one assertion: three columns move, five do not, and two of the five are the
// commercial terms migration 00016 took away from the application role entirely.
func TestAccountDB_SavesTheThreeFieldsAndLeavesTheRestAlone(t *testing.T) {
	f := newAccountFixture(t)
	beforeName, beforeVAT, _, beforeStructure, _, beforePlan, beforePrice := f.row(t, f.tenantID)

	got, err := f.accounts.Save(context.Background(), AccountCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		Name: "Rusty Bar Ltd", BusinessType: "bar", Timezone: "Europe/Rome",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	name, vat, businessType, structure, zone, plan, price := f.row(t, f.tenantID)
	if name != "Rusty Bar Ltd" || businessType != "bar" || zone != "Europe/Rome" {
		t.Errorf("the three editable columns read back as (%q, %q, %q), want "+
			"(Rusty Bar Ltd, bar, Europe/Rome)", name, businessType, zone)
	}
	if name == beforeName {
		t.Error("the name did not change at all; this test would pass over a no-op save")
	}
	// 🔴 THE FIVE THAT MUST NOT MOVE. vat_number and structure are absent from the
	// statement by decision; plan and price are absent by PRIVILEGE (tappa_app has no
	// UPDATE on either, migration 00016).
	if vat != beforeVAT {
		t.Errorf("the VAT number moved from %q to %q. It is globally UNIQUE, so a "+
			"self-service edit reaches OUTSIDE this tenant.", beforeVAT, vat)
	}
	if structure != beforeStructure {
		t.Errorf("structure moved from %q to %q; nothing reads it and nothing should write it",
			beforeStructure, structure)
	}
	if plan != beforePlan || price != beforePrice {
		t.Errorf("the commercial terms moved: plan %q->%q, price %q->%q. They are the "+
			"operator's (migration 00016) and the application role holds no UPDATE on either.",
			beforePlan, plan, beforePrice, price)
	}
	// AND THE RETURNED SETTINGS ARE THE STORED ROW, not the command.
	if got.Name != name || got.BusinessType != businessType || got.Timezone != zone {
		t.Errorf("Save returned (%q, %q, %q) and the row holds (%q, %q, %q)",
			got.Name, got.BusinessType, got.Timezone, name, businessType, zone)
	}
	if got.VATNumber != vat {
		t.Errorf("Save returned VAT %q and the row holds %q", got.VATNumber, vat)
	}
}

// TestAccountDB_TheChangeAndItsAuditRowShareATransaction.
//
// 🔴 IT MATTERS MORE HERE THAN ALMOST ANYWHERE. `tenants` has no updated_at, so a
// change that survived a lost trail would move the wall clock every shift and every
// lateness verdict is resolved against with NOTHING anywhere recording that it moved,
// or by whom.
func TestAccountDB_TheChangeAndItsAuditRowShareATransaction(t *testing.T) {
	f := newAccountFixture(t)
	beforeName, _, beforeType, _, beforeZone, _, _ := f.row(t, f.tenantID)

	broken, err := NewAccounts(f.data, brokenTrail{err: errors.New("audit is down")},
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAccounts: %v", err)
	}
	if _, err := broken.Save(context.Background(), AccountCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		Name: "Should Not Survive Ltd", BusinessType: "cafe", Timezone: "UTC",
	}); err == nil {
		t.Fatal("a save whose trail could not be written reported success")
	}

	name, _, businessType, _, zone, _, _ := f.row(t, f.tenantID)
	if name != beforeName || businessType != beforeType || zone != beforeZone {
		t.Errorf("the row changed to (%q, %q, %q) although the trail failed; want it "+
			"unchanged at (%q, %q, %q)", name, businessType, zone, beforeName, beforeType, beforeZone)
	}
}

// TestAccountDB_TheTrailSaysWhatMovedAndWhatDidNot.
//
// 🔴 THE `changed` LIST IS WHAT MAKES A SAVE READABLE A YEAR LATER. Without it, an
// owner reading the trail cannot tell somebody correcting a typo from somebody moving
// the business into another timezone — the before and after values are all there, but
// deciding which of three pairs differ is work a reader should not have to do on an
// append-only table.
func TestAccountDB_TheTrailSaysWhatMovedAndWhatDidNot(t *testing.T) {
	f := newAccountFixture(t)

	// One field moves.
	if _, err := f.accounts.Save(context.Background(), AccountCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		Name: "Kebab Factory Ltd", BusinessType: "restaurant", Timezone: "Europe/Rome",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	n, details := f.trailFor(t, f.tenantID, ActionAccountUpdated)
	if n != 1 {
		t.Fatalf("the trail holds %d %s row(s), want 1", n, ActionAccountUpdated)
	}
	detail := details[0]
	for _, want := range []string{
		`"changed":["timezone"]`,
		`"timezone_before":"Europe/Malta"`,
		`"timezone_after":"Europe/Rome"`,
		`"name_before":"Kebab Factory Ltd"`,
		`"name_after":"Kebab Factory Ltd"`,
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("the trail row does not carry %s\ndetail: %s", want, detail)
		}
	}
	// 🔴 §4.7 AND THE BLOAT RULE: THE VAT NUMBER IS NOT IN THE ROW. It cannot change on
	// this path, so recording it would be recording something that did not happen.
	if strings.Contains(detail, "VAT-") {
		t.Errorf("the trail row carries the VAT number, which this act cannot change\ndetail: %s", detail)
	}

	// Nothing moves — and the row is still written, saying so.
	if _, err := f.accounts.Save(context.Background(), AccountCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		Name: "Kebab Factory Ltd", BusinessType: "restaurant", Timezone: "Europe/Rome",
	}); err != nil {
		t.Fatalf("Save (no-op): %v", err)
	}
	n, details = f.trailFor(t, f.tenantID, ActionAccountUpdated)
	if n != 2 {
		t.Fatalf("a save that changed nothing wrote %d row(s) in total, want 2 — pressing "+
			"Save is an act and the trail records acts", n)
	}
	// ⚠️ THE TWO ROWS ARE COUNTED RATHER THAN ORDERED. `audit_log.at` defaults to now(),
	// which is TRANSACTION START time; two saves a microsecond apart are distinguishable
	// in principle and asserting on "the newest" would make this test's colour depend on
	// clock resolution. Counting which shapes are present does not.
	var noop, moved int
	for _, d := range details {
		if strings.Contains(d, `"changed":[]`) {
			noop++
		}
		if strings.Contains(d, `"changed":["timezone"]`) {
			moved++
		}
	}
	if noop != 1 || moved != 1 {
		t.Errorf("the two trail rows are %d no-op and %d timezone-change; want 1 and 1.\n"+
			"`changed` must be an EMPTY LIST rather than null on a save that moved nothing, "+
			"or a reader cannot tell it from `not recorded`.\ndetails: %v", noop, moved, details)
	}
}

// --- section 4.5 ---------------------------------------------------------------------

// TestAccountDB_OneBusinessCannotRewriteAnother is the BRACES on the write: a statement
// running under one business's context, naming another's id, changes nothing.
//
// 🔴 IT DRIVES RAW SQL RATHER THAN Save, AND THAT IS THE POINT. Save takes ONE tenant id
// and uses it for both the transaction's context and the predicate, so the two can never
// disagree through that door — which means calling Save proves nothing about the
// policy. The shape worth measuring is the one a bug would produce: a context set to A
// and a statement naming B. Postgres answers 0 rows.
func TestAccountDB_OneBusinessCannotRewriteAnother(t *testing.T) {
	f := newAccountFixture(t)
	foreignBefore, _, _, _, _, _, _ := f.row(t, f.foreignTenant)

	var affected int64
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx,
			`UPDATE tenants SET name = 'Taken Over Ltd' WHERE id = $1`, f.foreignTenant)
		if e != nil {
			return e
		}
		affected = tag.RowsAffected()
		return nil
	})
	if err != nil {
		t.Fatalf("cross-tenant update probe: %v", err)
	}
	if affected != 0 {
		t.Fatalf("a statement under one business's context changed %d row(s) of another's", affected)
	}
	if foreignAfter, _, _, _, _, _, _ := f.row(t, f.foreignTenant); foreignAfter != foreignBefore {
		t.Errorf("the other business's name moved from %q to %q", foreignBefore, foreignAfter)
	}

	// AND A READ OF A BUSINESS THAT DOES NOT EXIST IS A TYPED REFUSAL rather than an
	// empty Settings, which is what stops the screen printing blanks for it.
	if _, err := f.accounts.Settings(context.Background(), uuid.New()); !errors.Is(err, ErrUnknownTenantAccount) {
		t.Errorf("reading a tenant that does not exist returned %v, want ErrUnknownTenantAccount", err)
	}
}

// TestAccountDB_RLSHidesAnotherBusinessesRow is the isolation half, asserted WITHOUT
// the explicit predicate.
//
// 🔴 IT DELIBERATELY OMITS `WHERE id = ...` (CLAUDE.md §6): with the filter in place, 0
// rows would prove the WHERE works and say nothing about RLS — and the test would stay
// green with the policy dropped. Measured the other way round: under tenant A's
// context, `SELECT count(*) FROM tenants` must see exactly one row, its own.
func TestAccountDB_RLSHidesAnotherBusinessesRow(t *testing.T) {
	f := newAccountFixture(t)
	var n int
	var name string
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM tenants`).Scan(&n); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT name FROM tenants`).Scan(&name)
	})
	if err != nil {
		t.Fatalf("unfiltered read: %v", err)
	}
	if n != 1 {
		t.Fatalf("an unfiltered SELECT under one business's context saw %d tenants, want 1.\n"+
			"The development database holds many; anything but 1 means the policy is not "+
			"what is doing the filtering.", n)
	}
	// AND THE ONE ROW IS OURS, which is what stops "1" being a coincidence.
	if name != "Kebab Factory Ltd" {
		t.Errorf("the single visible row is %q", name)
	}
}

// --- the database is the authority on the vocabulary --------------------------------

// TestAccountDB_RefusesABusinessTypeTheSchemaRefuses. The Go side keeps no second copy
// of the eight values on purpose, so this CHECK is the only thing that says no.
func TestAccountDB_RefusesABusinessTypeTheSchemaRefuses(t *testing.T) {
	f := newAccountFixture(t)
	_, _, beforeType, _, _, _, _ := f.row(t, f.tenantID)

	_, err := f.accounts.Save(context.Background(), AccountCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		Name: "Kebab Factory Ltd", BusinessType: "nightclub", Timezone: "Europe/Malta",
	})
	if !errors.Is(err, ErrBusinessType) {
		t.Fatalf("a business type outside the CHECK returned %v, want ErrBusinessType", err)
	}
	if _, _, afterType, _, _, _, _ := f.row(t, f.tenantID); afterType != beforeType {
		t.Errorf("the refused type still reached the column: %q", afterType)
	}
	// AND NO TRAIL ROW WAS WRITTEN, because nothing happened.
	if n, _ := f.trailFor(t, f.tenantID, ActionAccountUpdated); n != 0 {
		t.Errorf("a refused save wrote %d trail row(s), want 0", n)
	}
}

// TestAccountDB_ARefusedZoneNeverReachesTheColumn — the domain refuses before the
// transaction opens, so there is nothing to roll back.
func TestAccountDB_ARefusedZoneNeverReachesTheColumn(t *testing.T) {
	f := newAccountFixture(t)
	for _, bad := range []string{"Europe/Maltaa", "Local", "", "../../etc/passwd"} {
		_, err := f.accounts.Save(context.Background(), AccountCommand{
			TenantID: f.tenantID, ActorID: f.actorID,
			Name: "Kebab Factory Ltd", BusinessType: "restaurant", Timezone: bad,
		})
		if !errors.Is(err, ErrTimezone) {
			t.Errorf("zone %q returned %v, want ErrTimezone", bad, err)
		}
	}
	if _, _, _, _, zone, _, _ := f.row(t, f.tenantID); zone != "Europe/Malta" {
		t.Errorf("the stored zone is %q; a refused zone reached the column", zone)
	}
}

// --- what the VAT columns can say ----------------------------------------------------

// TestAccountDB_ReadsTheFourVATStates is the card's 2026-08-13 addition from the DATA
// side: the four states migration 00017 describes are four distinguishable reads.
//
// 🔴 THE ROW IS WRITTEN WITH RAW SQL AS tappa_app, WHICH IS ITSELF THE MEASUREMENT.
// The INSERT grant covers these two columns and the UPDATE grant does not, so each
// state needs its own tenant — and that constraint is exactly why the panel offers no
// re-check.
func TestAccountDB_ReadsTheFourVATStates(t *testing.T) {
	f := newAccountFixture(t)
	tests := []struct {
		name     string
		verified *bool
		asked    bool
		want     VATState
	}{
		{name: "never asked", verified: nil, asked: false, want: VATNeverChecked},
		{name: "asked, no answer", verified: nil, asked: true, want: VATNoAnswer},
		{name: "the register says yes", verified: boolPtr(true), asked: true, want: VATValid},
		{name: "the register says no", verified: boolPtr(false), asked: true, want: VATInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			err := f.data.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
				var asked *time.Time
				if tc.asked {
					now := time.Now().UTC()
					asked = &now
				}
				_, e := tx.Exec(ctx,
					`INSERT INTO tenants (id, name, vat_number, business_type, structure,
					                      timezone, vat_verified, vat_checked_at)
					 VALUES ($1, 'Probe Ltd', $2, 'other', 'single', 'Europe/Malta', $3, $4)`,
					id, "VAT-"+id.String(), tc.verified, asked)
				return e
			})
			if err != nil {
				t.Fatalf("seed: %v", err)
			}
			s, err := f.accounts.Settings(context.Background(), id)
			if err != nil {
				t.Fatalf("Settings: %v", err)
			}
			if got := s.VAT.State(); got != tc.want {
				t.Errorf("the state reads %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAccountDB_TheAppRoleHoldsNoUpdateOnTheVATColumnsOrTheTerms is the M7-05 card's
// third ask, MEASURED as still absent rather than argued away.
//
// 🔴 THE CARD ASKED FOR `GRANT UPDATE (vat_verified, vat_checked_at)` "for a re-check".
// It is not shipped, and this test is what makes that a fact instead of an omission: if
// a later migration hands the application role that privilege, this turns red and
// whoever wrote it has to say what triggers the outbound call and what budgets it.
//
// THE SAME ASSERTION COVERS THE COMMERCIAL TERMS, which migration 00016 revoked for a
// different reason and which this screen must never be able to reach either.
func TestAccountDB_TheAppRoleHoldsNoUpdateOnTheVATColumnsOrTheTerms(t *testing.T) {
	f := newAccountFixture(t)
	closed := []string{"vat_verified", "vat_checked_at", "plan", "price_per_employee_month", "created_at"}
	open := []string{"name", "business_type", "timezone"}

	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for _, col := range closed {
			var may bool
			if e := tx.QueryRow(ctx,
				`SELECT has_column_privilege(current_user, 'tenants', $1, 'UPDATE')`, col).Scan(&may); e != nil {
				return e
			}
			if may {
				t.Errorf("the application role may UPDATE tenants.%s.\nvat_verified and "+
					"vat_checked_at were withheld by migration 00017 on purpose (INSERT only), "+
					"and plan/price/created_at by 00016. A screen that can write any of them is "+
					"a different threat model from the one this section was built for.", col)
			}
		}
		for _, col := range open {
			var may bool
			if e := tx.QueryRow(ctx,
				`SELECT has_column_privilege(current_user, 'tenants', $1, 'UPDATE')`, col).Scan(&may); e != nil {
				return e
			}
			if !may {
				t.Errorf("the application role may NOT UPDATE tenants.%s, so this whole section "+
					"cannot work. This half of the assertion is what stops the half above passing "+
					"because the role has no privileges at all.", col)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("privilege probe: %v", err)
	}
}

func boolPtr(b bool) *bool { return &b }

// TestAccountDB_ATimezoneEditMayNotMoveTheFirstChargeableMonth is backlog T37's
// positive control, and it asserts BOTH directions on the SAME business.
//
// 🔴 WHAT IT GUARDS. tappa_first_chargeable_month reads tenants.timezone, M7-05 made
// that column editable from the panel, and billing derives `free_period` as
// `month < first_chargeable` -- so a business that signed up within about fourteen
// hours of a local month boundary could retype its zone and push its first PAID month
// a month later. billing_periods is append-only, so once that month freezes the
// discount is permanent. Measured on this fixture before the guard existed:
// created_at 2026-01-31T23:30Z gives 2026-04-01 under UTC and 2026-05-01 under
// Europe/Malta.
//
// THE SECOND HALF IS NOT OPTIONAL: a guard that refused every zone edit would pass
// the first half perfectly and break a legitimate feature (M7-05 exists to let a
// business fix its zone). The control below moves a business whose signup instant is
// nowhere near a boundary, and that edit must still be saved.
func TestAccountDB_ATimezoneEditMayNotMoveTheFirstChargeableMonth(t *testing.T) {
	f := newAccountFixture(t)
	ctx := context.Background()

	// created_at is NOT writable by the application role (migration 00016), which is
	// itself a red line -- so the boundary case is planted with the owner role, the
	// way internal/db/rls_test.go's ownerDB and signup's ownerData do it.
	raw := os.Getenv("DATABASE_MIGRATE_URL")
	if raw == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the boundary case needs the owner role to plant created_at")
	}
	owner, err := db.New(ctx, &config.Config{DatabaseURL: raw})
	if err != nil {
		t.Fatalf("db.New(owner): %v", err)
	}
	t.Cleanup(owner.Close)

	// 23:30 UTC on the last day of a month: Europe/Malta is already in the NEXT
	// month, UTC is not. That single hour is the whole attack surface.
	boundary := uuid.New()
	f.seed(t, boundary)
	if err := owner.WithTenant(ctx, boundary, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE tenants SET created_at = $2 WHERE id = $1`,
			boundary, time.Date(2026, 1, 31, 23, 30, 0, 0, time.UTC))
		return e
	}); err != nil {
		t.Fatalf("planting the boundary signup instant: %v", err)
	}

	if _, err := f.accounts.Save(ctx, AccountCommand{
		TenantID: boundary, ActorID: f.actorID,
		Name: "Kebab Factory Ltd", BusinessType: "restaurant", Timezone: "UTC",
	}); !errors.Is(err, ErrTimezoneMovesBilling) {
		t.Fatalf("a zone edit that moves the first chargeable month answered %v, want "+
			"ErrTimezoneMovesBilling", err)
	}

	// AND IT WAS NOT WRITTEN ANYWAY. The refusal happens inside the transaction, so a
	// guard that returned the error after the UPDATE would pass the line above and
	// still have changed the row.
	var stored string
	if err := f.data.WithTenant(ctx, boundary, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT timezone FROM tenants WHERE id = $1`, boundary).Scan(&stored)
	}); err != nil {
		t.Fatalf("reading the zone back: %v", err)
	}
	if stored != "Europe/Malta" {
		t.Errorf("the refused zone was stored anyway (timezone = %q); billing_periods is "+
			"append-only, so a month frozen after this would carry it forever", stored)
	}

	// THE CONTROL. The default fixture signed up at now(), which is not within a
	// month boundary's reach of any zone, so an ordinary correction must still work.
	out, err := f.accounts.Save(ctx, AccountCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		Name: "Kebab Factory Ltd", BusinessType: "restaurant", Timezone: "Europe/Rome",
	})
	if err != nil {
		t.Fatalf("an ordinary timezone correction was refused: %v -- the guard is too wide "+
			"and M7-05's whole screen stops working", err)
	}
	if out.Timezone != "Europe/Rome" {
		t.Errorf("the accepted zone stored %q, want Europe/Rome", out.Timezone)
	}
}
