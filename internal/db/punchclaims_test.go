package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

// punchclaims_test.go -- M15-15, migration 00068 (ADR 0032 §C.2-§C.3): punch_claims and
// punch_claim_decisions. The isolation probes follow rls_test.go's rules: tappa_app proven at
// runtime, a positive control beside every negative, and NO tenant_id predicate in an isolation
// probe (a 0 there must be RLS, not a WHERE -- CLAUDE.md §6). The fixtures are committed and never
// cleaned up, as rls_test.go's are: both tables are append-only for every role. Probes that would
// MUTATE a committed row if a guard were missing (the owner's UPDATE/DELETE, every negative INSERT)
// run in a transaction that is always rolled back.

const pc00068File = "00068_create_punch_claims_and_their_decisions.sql"

// pcWorld is one tenant's rows for the claim tests, committed under its own context as tappa_app.
// Each anchor is a separate transactions row, so a test spends one per claim and never trips the
// one-claim-per-anchor key by accident.
type pcWorld struct {
	tenant, location, employee, other, admin, admin2 uuid.UUID
	openIn, openIn2, spareIn, orphanOut              uuid.UUID // directed, with a business day
	otherIn, refused, noDay, practice                uuid.UUID
	inAt, outAt                                      time.Time // the anchors' moments (openIn*, spareIn at inAt)
}

func pcBuildWorld(t *testing.T, d *DB) pcWorld {
	t.Helper()
	w := pcWorld{
		tenant: uuid.New(), location: uuid.New(), employee: uuid.New(), other: uuid.New(),
		admin: uuid.New(), admin2: uuid.New(),
		openIn: uuid.New(), openIn2: uuid.New(), spareIn: uuid.New(), orphanOut: uuid.New(),
		otherIn: uuid.New(), refused: uuid.New(), noDay: uuid.New(), practice: uuid.New(),
		inAt:  time.Date(2026, 10, 19, 9, 12, 0, 0, time.UTC),
		outAt: time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC),
	}
	day := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC)
	const tx = `INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, verdict, channel, type, business_day_start, practice)
	            VALUES ($1, $2, $3, $4, $5, $6, 'qr', $7, $8, $9)`
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'pc-fixture', $2, 'bar', 'single')`,
			[]any{w.tenant, "VAT-PC-" + w.tenant.String()}},
		{`INSERT INTO locations (id, tenant_id, name, static_ips) VALUES ($1, $2, 'loc', '{}')`, []any{w.location, w.tenant}},
		{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Emp', 'active')`,
			[]any{w.employee, w.tenant, w.location}},
		{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Other', 'active')`,
			[]any{w.other, w.tenant, w.location}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Mgr', $3, $4, 'manager')`,
			[]any{w.admin, w.tenant, "pc-" + w.admin.String() + "@iso.example", fixtures.UnusablePasswordHash}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Mgr2', $3, $4, 'manager')`,
			[]any{w.admin2, w.tenant, "pc-" + w.admin2.String() + "@iso.example", fixtures.UnusablePasswordHash}},
		{tx, []any{w.openIn, w.tenant, w.employee, w.location, w.inAt, "ok", "in", day, false}},
		{tx, []any{w.openIn2, w.tenant, w.employee, w.location, w.inAt.Add(time.Minute), "ok", "in", day, false}},
		{tx, []any{w.spareIn, w.tenant, w.employee, w.location, w.inAt.Add(2 * time.Minute), "ok", "in", day, false}},
		{tx, []any{w.orphanOut, w.tenant, w.employee, w.location, w.outAt, "ok", "out", day, false}},
		{tx, []any{w.otherIn, w.tenant, w.other, w.location, w.inAt, "ok", "in", day, false}},
		{tx, []any{w.refused, w.tenant, w.employee, w.location, w.inAt, "reject", nil, nil, false}},
		{tx, []any{w.noDay, w.tenant, w.employee, w.location, w.inAt, "ok", "in", nil, false}},
		{tx, []any{w.practice, w.tenant, w.employee, w.location, w.inAt, "ok", "in", day, true}},
	}
	if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		for i, s := range stmts {
			if _, e := tx.Exec(ctx, s.sql, s.args...); e != nil {
				return fmt.Errorf("pc fixture stmt %d: %w", i, e)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("pcBuildWorld: %v", err)
	}
	return w
}

// pcClaim commits a claim through the shipped statement and returns its id.
func pcClaim(t *testing.T, d *DB, w pcWorld, anchor uuid.UUID, kind string, claimed time.Time, prefilled *time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).InsertPunchClaim(ctx, store.InsertPunchClaimParams{
			Kind: kind, ClaimedAt: claimed, PrefilledAt: prefilled,
			AnchorTransactionID: anchor, TenantID: w.tenant, EmployeeID: w.employee,
		})
		id = row.ID
		return e
	}); err != nil {
		t.Fatalf("claim on %s: %v", anchor, err)
	}
	return id
}

// pcManual commits a manual record the way a claim's approval writes one (channel manual,
// entered_by the approver) and returns its id.
func pcManual(t *testing.T, d *DB, w pcWorld, employee uuid.UUID, typ string, at time.Time, by uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, verdict, channel, type, entered_by, trust)
		                      VALUES ($1, $2, $3, $4, $5, 'ok', 'manual', $6, $7, 0)`, id, w.tenant, employee, w.location, at, typ, by)
		return e
	}); err != nil {
		t.Fatalf("manual record: %v", err)
	}
	return id
}

// errPCRollback is what a probe's closure returns so that WithTenant rolls its transaction back.
var errPCRollback = errors.New("pc probe: rolled back on purpose")

// pcRolledBack runs fn in ctxTenant's context and always rolls the transaction back; it fails the
// test if WithTenant itself failed for another reason, and returns what fn recorded.
func pcRolledBack(t *testing.T, d *DB, ctxTenant uuid.UUID, fn func(ctx context.Context, tx pgx.Tx) error) error {
	t.Helper()
	var probeErr error
	err := d.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
		probeErr = fn(ctx, tx)
		return errPCRollback
	})
	if !errors.Is(err, errPCRollback) {
		t.Fatalf("the probe's transaction: %v", err)
	}
	return probeErr
}

// pcTry runs one statement in ctxTenant's context, always rolled back, and returns its error.
func pcTry(t *testing.T, d *DB, ctxTenant uuid.UUID, sql string, args ...any) error {
	t.Helper()
	return pcRolledBack(t, d, ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, sql, args...)
		return e
	})
}

// pcOwnerTry runs one statement as the migration role with NO tenant context (app.tenant_id
// cleared for the transaction), always rolled back, and returns its error. The superuser bypasses
// RLS, and with no context the claim triggers defer an unseen row to the keys and CHECKs -- the
// only place those are reached first (00068's header).
func pcOwnerTry(t *testing.T, owner *DB, sql string, args ...any) error {
	t.Helper()
	ctx := context.Background()
	tx, err := owner.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if rb := tx.Rollback(ctx); rb != nil && !errors.Is(rb, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", rb)
		}
	}()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', '', true)`); err != nil {
		t.Fatalf("clear the context: %v", err)
	}
	_, err = tx.Exec(ctx, sql, args...)
	return err
}

// pcWant asserts err is the SQLSTATE code and, when msg is not empty, carries msg.
func pcWant(t *testing.T, what string, err error, code, msg string) {
	t.Helper()
	pg := asPgErr(err)
	if pg == nil {
		t.Fatalf("%s: got %v, want SQLSTATE %s", what, err, code)
	}
	if pg.Code != code || (msg != "" && !strings.Contains(pg.Message+" "+pg.ConstraintName, msg)) {
		t.Fatalf("%s: got %s %q (constraint %q), want %s containing %q", what, pg.Code, pg.Message, pg.ConstraintName, code, msg)
	}
}

const (
	pcInsertClaim = `INSERT INTO punch_claims (tenant_id, employee_id, anchor_transaction_id, kind, claimed_at, prefilled_at)
	                 VALUES ($1, $2, $3, $4, $5, $6)`
	pcInsertDecision = `INSERT INTO punch_claim_decisions (tenant_id, claim_id, outcome, decided_by, approved_at, transaction_id, note)
	                    VALUES ($1, $2, $3, $4, $5, $6, NULL)`
)

// TestRLS_PunchClaims_TenantIsolation is CLAUDE.md §8's mandatory isolation proof for both tables:
// tenant B's context neither reads A's claim and decision (filterless probes) nor writes a row
// stamped with A's tenant_id (WITH CHECK); a connection that has carried a context and lost it
// reads nothing and does not error (the NULLIF policy -- Q27).
func TestRLS_PunchClaims_TenantIsolation(t *testing.T) {
	app := testDB(t) // one pinned connection: the context-less probe below must reuse a used backend
	assertAppRole(t, app)
	a, b := pcBuildWorld(t, app), pcBuildWorld(t, app)

	claimA := pcClaim(t, app, a, a.openIn, "out", a.inAt.Add(8*time.Hour), nil)
	pendingA := pcClaim(t, app, a, a.orphanOut, "in", a.outAt.Add(-6*time.Hour), nil)
	manualA := pcManual(t, app, a, a.employee, "out", a.inAt.Add(8*time.Hour), a.admin)
	if err := app.WithTenant(context.Background(), a.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, pcInsertDecision, a.tenant, claimA, "approved", a.admin, a.inAt.Add(8*time.Hour), manualA)
		return e
	}); err != nil {
		t.Fatalf("A's approval: %v", err)
	}
	var decisionA uuid.UUID
	if err := app.WithTenant(context.Background(), a.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM punch_claim_decisions WHERE claim_id = $1`, claimA).Scan(&decisionA)
	}); err != nil {
		t.Fatalf("read A's decision id: %v", err)
	}

	t.Run("read", func(t *testing.T) {
		for _, p := range []struct {
			name, query string
			id          uuid.UUID
		}{
			{"punch_claims", `SELECT count(*) FROM punch_claims WHERE id = $1`, claimA},
			{"punch_claim_decisions", `SELECT count(*) FROM punch_claim_decisions WHERE id = $1`, decisionA},
		} {
			if self := rawCountCtx(t, app, a.tenant, p.query, p.id); self != 1 {
				t.Fatalf("positive control: A's context sees %d of its own %s rows, want 1", self, p.name)
			}
			if cross := rawCountCtx(t, app, b.tenant, p.query, p.id); cross != 0 {
				t.Fatalf("RLS FAILED: B's context read %d of A's %s rows, want 0", cross, p.name)
			}
			// The same backend, after a tenant transaction: app.tenant_id is '' now, not NULL. The
			// NULLIF policy reads that as no tenant -- 0 rows; a bare ::uuid cast would ERROR here.
			var n int
			if err := app.pool.QueryRow(context.Background(), p.query, p.id).Scan(&n); err != nil {
				t.Fatalf("%s: the context-less read on a used connection errored (%v); the NULLIF policy yields 0 rows", p.name, err)
			}
			if n != 0 {
				t.Fatalf("RLS FAILED: a context-less read saw %d of A's %s rows", n, p.name)
			}
		}
	})

	t.Run("write", func(t *testing.T) {
		// B forges a claim stamped with A's tenant on A's unclaimed anchor (FK checks bypass RLS, so
		// the keys resolve, and the trigger cannot see the anchor under B and defers): WITH CHECK.
		err := pcTry(t, app, b.tenant, pcInsertClaim, a.tenant, a.employee, a.spareIn, "out", a.inAt.Add(4*time.Hour), nil)
		assertWriteBlocked(t, "punch_claims", blockRLS, err)
		if e := pcTry(t, app, b.tenant, pcInsertClaim, b.tenant, b.employee, b.spareIn, "out", b.inAt.Add(4*time.Hour), nil); e != nil {
			t.Fatalf("positive control: B could not insert its OWN claim: %v", e)
		}
		// B forges a rejection of A's pending claim, as A's manager.
		err = pcTry(t, app, b.tenant, pcInsertDecision, a.tenant, pendingA, "rejected", a.admin, nil, nil)
		assertWriteBlocked(t, "punch_claim_decisions", blockRLS, err)
		pendingB := pcClaim(t, app, b, b.orphanOut, "in", b.outAt.Add(-6*time.Hour), nil)
		if e := pcTry(t, app, b.tenant, pcInsertDecision, b.tenant, pendingB, "rejected", b.admin, nil, nil); e != nil {
			t.Fatalf("positive control: B could not reject its OWN claim: %v", e)
		}
	})
}

// TestRLS_PunchClaims_AForeignRowIsRefusedByTheKey: a claim on another tenant's anchor, a decision
// on another tenant's claim and an approval naming another tenant's record. In the claimant's OWN
// context tappa_app's trigger cannot see the foreign row, which is stamped with the context's own
// tenant, and refuses it (23514 -- 00068's header: such a row is never deferred). The composite keys
// are proven where the trigger does defer: the migration role with no context, which RLS does not
// bind and whose context matches no stamp -- there each is refused by its key (23503), which a key
// on the id alone would let through. The decider's key is reached from tappa_app too (no trigger
// reads admin_users).
func TestRLS_PunchClaims_AForeignRowIsRefusedByTheKey(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	a, b := pcBuildWorld(t, app), pcBuildWorld(t, app)
	claimB := pcClaim(t, app, b, b.openIn, "out", b.inAt.Add(8*time.Hour), nil)
	claimA := pcClaim(t, app, a, a.openIn, "out", a.inAt.Add(8*time.Hour), nil)
	manualB := pcManual(t, app, b, b.employee, "out", b.inAt.Add(8*time.Hour), b.admin)
	manualA := pcManual(t, app, a, a.employee, "out", a.inAt.Add(8*time.Hour), a.admin)

	for _, c := range []struct {
		name, sql string
		args      []any
		appMsg    string
		key       string
	}{
		{"a claim on another tenant's anchor", pcInsertClaim,
			[]any{a.tenant, a.employee, b.spareIn, "out", a.inAt.Add(4 * time.Hour), nil},
			"the anchor is not visible in its own tenant's context", "punch_claims_anchor_fk"},
		{"a decision on another tenant's claim", pcInsertDecision,
			[]any{a.tenant, claimB, "rejected", a.admin, nil, nil},
			"the claim is not visible in its own tenant's context", "punch_claim_decisions_claim_fk"},
		{"an approval naming another tenant's record", pcInsertDecision,
			[]any{a.tenant, claimA, "approved", a.admin, a.inAt.Add(8 * time.Hour), manualB},
			"the approving record is not visible in its own tenant's context", "punch_claim_decisions_transaction_fk"},
	} {
		pcWant(t, c.name+" (tappa_app, own context)", pcTry(t, app, a.tenant, c.sql, c.args...), "23514", c.appMsg)
		pcWant(t, c.name+" (migration role, no context)", pcOwnerTry(t, owner, c.sql, c.args...), "23503", c.key)
	}
	pcWant(t, "a decision by another tenant's admin",
		pcTry(t, app, a.tenant, pcInsertDecision, a.tenant, claimA, "rejected", b.admin, nil, nil),
		"23503", "punch_claim_decisions_decided_by_fk")
	// Positive controls: the same approval with A's own record and admin is written, in both roles.
	if e := pcTry(t, app, a.tenant, pcInsertDecision, a.tenant, claimA, "approved", a.admin, a.inAt.Add(8*time.Hour), manualA); e != nil {
		t.Fatalf("positive control: A's own approval was refused: %v", e)
	}
	if e := pcOwnerTry(t, owner, pcInsertDecision, a.tenant, claimA, "approved", a.admin, a.inAt.Add(8*time.Hour), manualA); e != nil {
		t.Fatalf("positive control: A's own approval was refused to the migration role: %v", e)
	}
}

// TestRLS_PunchClaims_AppCannotMutate: tappa_app holds SELECT and INSERT alone on both tables --
// UPDATE and DELETE are refused at the privilege check (42501), before any trigger.
func TestRLS_PunchClaims_AppCannotMutate(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	w := pcBuildWorld(t, app)
	claim := pcClaim(t, app, w, w.orphanOut, "in", w.outAt.Add(-time.Hour), nil)
	if err := app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, pcInsertDecision, w.tenant, claim, "rejected", w.admin, nil, nil)
		return e
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	for _, c := range []struct{ table, update, del string }{
		{"punch_claims", `UPDATE punch_claims SET claimed_at = claimed_at - interval '1 hour' WHERE id = $1`, `DELETE FROM punch_claims WHERE id = $1`},
		{"punch_claim_decisions", `UPDATE punch_claim_decisions SET outcome = 'approved' WHERE claim_id = $1`, `DELETE FROM punch_claim_decisions WHERE claim_id = $1`},
	} {
		key := `SELECT count(*) FROM punch_claims WHERE id = $1`
		if c.table == "punch_claim_decisions" {
			key = `SELECT count(*) FROM punch_claim_decisions WHERE claim_id = $1`
		}
		if got := rawCountCtx(t, app, w.tenant, key, claim); got != 1 {
			t.Fatalf("positive control: tappa_app sees %d of its own %s rows, want 1", got, c.table)
		}
		assertPermissionDenied(t, "app UPDATE on "+c.table, pcTry(t, app, w.tenant, c.update, claim))
		assertPermissionDenied(t, "app DELETE on "+c.table, pcTry(t, app, w.tenant, c.del, claim))
	}
}

// TestRLS_PunchClaims_OwnerMutationsHitAppendOnlyTrigger: the migration role is a superuser and
// passes every privilege and RLS check; the row trigger is what stops its UPDATE and DELETE. Run in
// a transaction that is always rolled back, so a missing trigger could not change the committed row.
func TestRLS_PunchClaims_OwnerMutationsHitAppendOnlyTrigger(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	w := pcBuildWorld(t, app)
	claim := pcClaim(t, app, w, w.orphanOut, "in", w.outAt.Add(-time.Hour), nil)
	if err := app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, pcInsertDecision, w.tenant, claim, "rejected", w.admin, nil, nil)
		return e
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	for _, stmt := range []string{
		`UPDATE punch_claims SET claimed_at = claimed_at - interval '1 hour' WHERE id = $1`,
		`DELETE FROM punch_claims WHERE id = $1`,
		`UPDATE punch_claim_decisions SET outcome = 'approved' WHERE claim_id = $1`,
		`DELETE FROM punch_claim_decisions WHERE claim_id = $1`,
	} {
		ctx := context.Background()
		tx, err := owner.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, stmt, claim)
		if rb := tx.Rollback(ctx); rb != nil && !errors.Is(rb, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", rb)
		}
		assertAppendOnlyTrigger(t, "owner "+stmt, err)
	}
}

// TestPunchClaims_OneClaimPerAnchorAndOneDecisionPerClaim: ADR 0032 §C.2's two keys and the one
// that ties an approval's record to one claim -- each second write is 23505 on its constraint.
func TestPunchClaims_OneClaimPerAnchorAndOneDecisionPerClaim(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	w := pcBuildWorld(t, app)
	at := w.inAt.Add(8 * time.Hour)
	first := pcClaim(t, app, w, w.openIn, "out", at, nil)
	// A second claim of the same person and direction, written before the manual record below:
	// an approval's record is written after its claim (tappa_check_punch_claim_decision).
	second := pcClaim(t, app, w, w.openIn2, "out", at, nil)

	err := app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).InsertPunchClaim(ctx, store.InsertPunchClaimParams{
			Kind: "out", ClaimedAt: at.Add(time.Hour), AnchorTransactionID: w.openIn, TenantID: w.tenant, EmployeeID: w.employee,
		})
		return e
	})
	pcWant(t, "a second claim on the same anchor", err, "23505", "punch_claims_one_per_anchor")

	manual := pcManual(t, app, w, w.employee, "out", at, w.admin)
	if e := pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, first, "approved", w.admin, at, manual); e != nil {
		t.Fatalf("positive control: the first decision was refused: %v", e)
	}
	if err := app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, pcInsertDecision, w.tenant, first, "approved", w.admin, at, manual)
		return e
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	pcWant(t, "a second decision on the same claim",
		pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, first, "rejected", w.admin, nil, nil),
		"23505", "punch_claim_decisions_one_per_claim")

	// The second claim, approved with the FIRST approval's record.
	pcWant(t, "one manual record answering two claims",
		pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, second, "approved", w.admin, at, manual),
		"23505", "punch_claim_decisions_one_per_record")
}

// TestPunchClaims_TheInsertIsBuiltFromTheAnchor is ADR 0032 §C.3 on the shipped statement: an
// anchor that is not the caller's tenant's, not the caller's person's, or not there at all is ZERO
// ROWS (pgx.ErrNoRows) and writes nothing; the claim's tenant and person are the anchor's own.
func TestPunchClaims_TheInsertIsBuiltFromTheAnchor(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	a, b := pcBuildWorld(t, app), pcBuildWorld(t, app)
	insert := func(ctxTenant uuid.UUID, p store.InsertPunchClaimParams) error {
		return pcRolledBack(t, app, ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			_, e := store.New(tx).InsertPunchClaim(ctx, p)
			return e
		})
	}
	claim := func(anchor, tenant, employee uuid.UUID) store.InsertPunchClaimParams {
		return store.InsertPunchClaimParams{Kind: "out", ClaimedAt: a.inAt.Add(8 * time.Hour),
			AnchorTransactionID: anchor, TenantID: tenant, EmployeeID: employee}
	}
	for _, c := range []struct {
		name      string
		ctxTenant uuid.UUID
		p         store.InsertPunchClaimParams
	}{
		{"another tenant's anchor, named as mine", a.tenant, claim(b.openIn, a.tenant, a.employee)},
		{"another tenant's anchor, named as its own", a.tenant, claim(b.openIn, b.tenant, b.employee)},
		{"my tenant's anchor in another tenant's context", b.tenant, claim(a.openIn, a.tenant, a.employee)},
		{"another person's anchor", a.tenant, claim(a.otherIn, a.tenant, a.employee)},
		{"no such anchor", a.tenant, claim(uuid.New(), a.tenant, a.employee)},
	} {
		if err := insert(c.ctxTenant, c.p); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("%s: got %v, want pgx.ErrNoRows (zero rows, nothing written)", c.name, err)
		}
	}
	if n := rawCountCtx(t, app, a.tenant, `SELECT count(*) FROM punch_claims WHERE anchor_transaction_id = ANY ($1)`,
		[]uuid.UUID{a.openIn, a.otherIn}); n != 0 {
		t.Fatalf("the refused inserts left %d claims", n)
	}

	pre := a.inAt.Add(8 * time.Hour)
	id := pcClaim(t, app, a, a.openIn, "out", a.inAt.Add(7*time.Hour), &pre)
	var tenant, employee, anchor uuid.UUID
	var kind string
	var prefilled *time.Time
	if err := app.WithTenant(context.Background(), a.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT tenant_id, employee_id, anchor_transaction_id, kind, prefilled_at FROM punch_claims WHERE id = $1`, id).
			Scan(&tenant, &employee, &anchor, &kind, &prefilled)
	}); err != nil {
		t.Fatal(err)
	}
	if tenant != a.tenant || employee != a.employee || anchor != a.openIn || kind != "out" || prefilled == nil || !prefilled.Equal(pre) {
		t.Fatalf("the claim reads tenant %s, employee %s, anchor %s, kind %q, prefilled %v; want the anchor's own and the prefill",
			tenant, employee, anchor, kind, prefilled)
	}
}

// TestPunchClaimQueries_CarryTheirOwnTenantPredicate is §4.5's belt on InsertPunchClaim, seen apart
// from RLS's braces (inviteemail_test.go's shape): as the migration role, which RLS does not bind,
// the statement given A's tenant id and B's anchor and person writes nothing -- only its own
// `a.tenant_id = @tenant_id` refuses it. TestPunchClaims_TheInsertIsBuiltFromTheAnchor cannot see
// that predicate: as tappa_app, RLS alone already hides B's anchor. Control: the same superuser
// with B's own tenant id writes B's claim. One transaction, rolled back. (The ledger belt,
// TestPanelQueries_CarryAnExplicitTenantPredicate, reads the queries its own package calls; since
// M15-16 internal/domain/claim calls this one and carries its own copy over the statements it added --
// TestPunchClaimQueries_TheNewStatementsCarryTheirOwnTenantPredicate -- and internal/domain/tenant's
// TestPunchClaimsQueries_CarryAnExplicitTenantPredicate belts the whole file statically.)
func TestPunchClaimQueries_CarryTheirOwnTenantPredicate(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	a, b := pcBuildWorld(t, app), pcBuildWorld(t, app)
	errRollback := errors.New("rollback: this probe keeps nothing")
	err := owner.WithTenant(context.Background(), a.tenant, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		p := store.InsertPunchClaimParams{Kind: "out", ClaimedAt: b.inAt.Add(8 * time.Hour),
			AnchorTransactionID: b.openIn, TenantID: a.tenant, EmployeeID: b.employee}
		if _, e := q.InsertPunchClaim(ctx, p); !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("InsertPunchClaim(A's tenant, B's anchor and person) as a superuser: %v, want no row -- "+
				"its tenant predicate is gone", e)
		}
		p.TenantID = b.tenant
		row, e := q.InsertPunchClaim(ctx, p)
		if e != nil {
			return fmt.Errorf("control: the superuser cannot write B's claim with B's tenant: %w", e)
		}
		var tenant uuid.UUID
		if e := tx.QueryRow(ctx, `SELECT tenant_id FROM punch_claims WHERE id = $1`, row.ID).Scan(&tenant); e != nil {
			return e
		}
		if tenant != b.tenant {
			t.Errorf("control: the claim reads tenant %s, want the anchor's %s", tenant, b.tenant)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("owner probe: %v", err)
	}
}

// TestPunchClaims_TheAnchorMustBeTheClaimantsDirectedRecord is tappa_check_punch_claim, one refusal
// per immutable fact of the anchor (raw INSERTs in the claimant's own context, rolled back), with
// the valid claim as the positive control -- and once as the migration role, which RLS does not
// bind, to show the trigger does.
func TestPunchClaims_TheAnchorMustBeTheClaimantsDirectedRecord(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	w := pcBuildWorld(t, app)
	after, before := w.inAt.Add(4*time.Hour), w.inAt.Add(-time.Hour)
	for _, c := range []struct {
		name               string
		employee, anchor   uuid.UUID
		kind               string
		claimed, prefilled any
		msg                string
	}{
		{"another person's anchor", w.employee, w.otherIn, "out", after, nil, "another person's record"},
		{"an undirected anchor", w.employee, w.refused, "out", after, nil, "has no direction"},
		{"the anchor's own direction", w.employee, w.spareIn, "in", before, nil, "opposite the anchor"},
		{"the orphan out's own direction", w.employee, w.orphanOut, "out", w.outAt.Add(time.Minute), nil, "opposite the anchor"},
		{"an anchor before the business day", w.employee, w.noDay, "out", after, nil, "no business day"},
		{"a practice anchor", w.employee, w.practice, "out", after, nil, "practice tap"},
		{"an out at the open in's own moment", w.employee, w.spareIn, "out", w.inAt.Add(2 * time.Minute), nil, "claimed moment is on the wrong side"},
		{"an out before the open in", w.employee, w.spareIn, "out", before, nil, "claimed moment is on the wrong side"},
		{"an in after the orphan out", w.employee, w.orphanOut, "in", w.outAt.Add(time.Minute), nil, "claimed moment is on the wrong side"},
		{"an in at the orphan out's own moment", w.employee, w.orphanOut, "in", w.outAt, nil, "claimed moment is on the wrong side"},
		{"a prefill before the open in", w.employee, w.spareIn, "out", after, before, "prefilled moment is on the wrong side"},
		{"a prefill after the orphan out", w.employee, w.orphanOut, "in", w.outAt.Add(-time.Hour), w.outAt.Add(time.Hour), "prefilled moment is on the wrong side"},
		{"a prefill at the open in's own moment", w.employee, w.spareIn, "out", after, w.inAt.Add(2 * time.Minute), "prefilled moment is on the wrong side"},
		{"a prefill at the orphan out's own moment", w.employee, w.orphanOut, "in", w.outAt.Add(-time.Hour), w.outAt, "prefilled moment is on the wrong side"},
	} {
		pcWant(t, c.name, pcTry(t, app, w.tenant, pcInsertClaim, w.tenant, c.employee, c.anchor, c.kind, c.claimed, c.prefilled),
			"23514", c.msg)
	}
	for _, ok := range []struct {
		name               string
		anchor             uuid.UUID
		kind               string
		claimed, prefilled any
	}{
		{"an out claim with a prefill", w.spareIn, "out", after, after.Add(time.Hour)},
		{"an in claim without one", w.orphanOut, "in", w.outAt.Add(-6 * time.Hour), nil},
	} {
		if e := pcTry(t, app, w.tenant, pcInsertClaim, w.tenant, w.employee, ok.anchor, ok.kind, ok.claimed, ok.prefilled); e != nil {
			t.Fatalf("positive control (%s) was refused: %v", ok.name, e)
		}
	}

	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	ctx := context.Background()
	tx, err := owner.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, pcInsertClaim, w.tenant, w.employee, w.otherIn, "out", after, nil)
	if rb := tx.Rollback(ctx); rb != nil && !errors.Is(rb, pgx.ErrTxClosed) {
		t.Errorf("rollback: %v", rb)
	}
	pcWant(t, "the migration role's claim on another person's anchor", err, "23514", "another person's record")
}

// TestPunchClaimDecisions_TheApprovalIsTheClaimedHalf is the decision's shape CHECK and
// tappa_check_punch_claim_decision: an approval names the manual record that is the claimed half --
// the claim's person, its direction, the approved moment, entered by the decider -- and a rejection
// names no record and no moment. The self-decision clause is driven with an admin row given the
// claimant's id (two id spaces: the product cannot reach it -- ADR 0032 §C.4, G-O1).
func TestPunchClaimDecisions_TheApprovalIsTheClaimedHalf(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	w := pcBuildWorld(t, app)
	at := w.inAt.Add(8 * time.Hour)
	claim := pcClaim(t, app, w, w.openIn, "out", at, nil)
	good := pcManual(t, app, w, w.employee, "out", at, w.admin)
	others := pcManual(t, app, w, w.other, "out", at, w.admin)
	wrongWay := pcManual(t, app, w, w.employee, "in", at, w.admin)
	elsewhen := pcManual(t, app, w, w.employee, "out", at.Add(time.Minute), w.admin)
	byAdmin2 := pcManual(t, app, w, w.employee, "out", at, w.admin2)
	if err := app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Twin', $3, $4, 'manager')`,
			w.employee, w.tenant, "pc-twin-"+w.employee.String()+"@iso.example", fixtures.UnusablePasswordHash)
		return e
	}); err != nil {
		t.Fatalf("an admin row with the claimant's id: %v", err)
	}
	twin := pcManual(t, app, w, w.employee, "out", at, w.employee)

	for _, c := range []struct {
		name     string
		outcome  string
		by       uuid.UUID
		approved any
		record   any
		code     string
		msg      string
	}{
		{"an approval naming no record", "approved", w.admin, at, nil, "23514", "punch_claim_decisions_shape_by_outcome"},
		// The trigger runs before the CHECKs: with a record it can read, a missing moment is its
		// refusal; with one it cannot (here: none at all) in its own tenant's context, the unseen
		// record is. The shape CHECK under these two is reached below, as the migration role.
		{"an approval naming no moment", "approved", w.admin, nil, good, "23514", "not at the approved moment"},
		{"an approval naming no moment and an unseen record", "approved", w.admin, nil, uuid.New(), "23514", "approving record is not visible"},
		{"a rejection naming a readable record", "rejected", w.admin, nil, good, "23514", "not at the approved moment"},
		{"a rejection naming an unseen record", "rejected", w.admin, nil, uuid.New(), "23514", "approving record is not visible"},
		{"a rejection naming a moment", "rejected", w.admin, at, nil, "23514", "punch_claim_decisions_shape_by_outcome"},
		{"an unknown outcome", "withdrawn", w.admin, nil, nil, "23514", "punch_claim_decisions_outcome_is_known"},
		{"an approval naming a tap, not a manual record", "approved", w.admin, w.inAt, w.openIn, "23514", "names the manual record"},
		{"an approval naming another person's record", "approved", w.admin, at, others, "23514", "another person's"},
		{"an approval naming the wrong direction", "approved", w.admin, at, wrongWay, "23514", "not the claimed direction"},
		{"an approval at another moment than its record", "approved", w.admin, at, elsewhen, "23514", "not at the approved moment"},
		{"an approval whose record another admin entered", "approved", w.admin, at, byAdmin2, "23514", "entered by somebody else"},
		{"the claimant deciding", "approved", w.employee, at, twin, "23514", "the decider is the claimant"},
		{"a decider who is no admin", "rejected", uuid.New(), nil, nil, "23503", "punch_claim_decisions_decided_by_fk"},
	} {
		pcWant(t, c.name, pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, claim, c.outcome, c.by, c.approved, c.record), c.code, c.msg)
	}
	if e := pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, claim, "approved", w.admin, at, good); e != nil {
		t.Fatalf("positive control: the approval with its own record was refused: %v", e)
	}
	if e := pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, claim, "rejected", w.admin, nil, nil); e != nil {
		t.Fatalf("positive control: the rejection was refused: %v", e)
	}

	// With no context the trigger defers an unseen record, so the shape CHECK is what refuses
	// a record-naming approval without a moment and a rejection that names a record.
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	for _, outcome := range []string{"approved", "rejected"} {
		pcWant(t, "the migration role's "+outcome+" naming an unseen record and no moment",
			pcOwnerTry(t, owner, pcInsertDecision, w.tenant, claim, outcome, w.admin, nil, uuid.New()),
			"23514", "punch_claim_decisions_shape_by_outcome")
	}
}

// TestPunchClaimDecisions_TheApprovalIsOnTheAnchorsOpenSideAndAfterTheClaim is the rest of
// tappa_check_punch_claim_decision: the approved moment obeys the claim's own rule against the
// anchor -- an out strictly after the open in, an in strictly before the orphan out (the security
// lens's D8: an open in's out approved with a manual out dated 2020 was written) -- and the
// approving record was written no earlier than the claim, so a manual row entered before the claim
// existed is not its approval. Every other fact of each record is the claimed half, so the one
// refusal is the one under test; the two valid approvals are the positive controls.
func TestPunchClaimDecisions_TheApprovalIsOnTheAnchorsOpenSideAndAfterTheClaim(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	w := pcBuildWorld(t, app)
	outOK, inOK := w.inAt.Add(8*time.Hour), w.outAt.Add(-6*time.Hour)
	early := pcManual(t, app, w, w.employee, "out", outOK, w.admin) // before either claim
	outClaim := pcClaim(t, app, w, w.openIn, "out", outOK, nil)
	inClaim := pcClaim(t, app, w, w.orphanOut, "in", inOK, nil)
	in2020 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, c := range []struct {
		name      string
		claim     uuid.UUID
		typ       string
		approved  time.Time
		wantError string
	}{
		{"an out approved before the open in (D8: 2020)", outClaim, "out", in2020, "approved moment is on the wrong side"},
		{"an out approved a minute before the open in", outClaim, "out", w.inAt.Add(-time.Minute), "approved moment is on the wrong side"},
		{"an out approved at the open in's own moment", outClaim, "out", w.inAt, "approved moment is on the wrong side"},
		{"an in approved after the orphan out", inClaim, "in", w.outAt.Add(time.Hour), "approved moment is on the wrong side"},
		{"an in approved at the orphan out's own moment", inClaim, "in", w.outAt, "approved moment is on the wrong side"},
	} {
		record := pcManual(t, app, w, w.employee, c.typ, c.approved, w.admin) // after the claim
		pcWant(t, c.name, pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, c.claim, "approved", w.admin, c.approved, record),
			"23514", c.wantError)
	}
	pcWant(t, "a manual record entered before the claim",
		pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, outClaim, "approved", w.admin, outOK, early),
		"23514", "written before the claim")

	for _, ok := range []struct {
		name     string
		claim    uuid.UUID
		typ      string
		approved time.Time
	}{
		{"an out approved after the open in", outClaim, "out", outOK},
		{"an in approved before the orphan out", inClaim, "in", inOK},
	} {
		record := pcManual(t, app, w, w.employee, ok.typ, ok.approved, w.admin)
		if e := pcTry(t, app, w.tenant, pcInsertDecision, w.tenant, ok.claim, "approved", w.admin, ok.approved, record); e != nil {
			t.Fatalf("positive control (%s) was refused: %v", ok.name, e)
		}
	}
	// The claim, the record and the approval in ONE transaction share now(), so the record's
	// created_at EQUALS the claim's: "no earlier than" is >=, and this is its boundary. (M15-16
	// writes the claim and the approval in two requests; the record and the approval in one.)
	if e := pcRolledBack(t, app, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).InsertPunchClaim(ctx, store.InsertPunchClaimParams{
			Kind: "out", ClaimedAt: outOK, AnchorTransactionID: w.spareIn, TenantID: w.tenant, EmployeeID: w.employee,
		})
		if err != nil {
			return err
		}
		id := uuid.New()
		if _, err := tx.Exec(ctx, `INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, verdict, channel, type, entered_by, trust)
		                           VALUES ($1, $2, $3, $4, $5, 'ok', 'manual', 'out', $6, 0)`, id, w.tenant, w.employee, w.location, outOK, w.admin); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, pcInsertDecision, w.tenant, row.ID, "approved", w.admin, outOK, id)
		return err
	}); e != nil {
		t.Fatalf("positive control (the claim, its record and its approval in one transaction) was refused: %v", e)
	}
}

// TestPunchClaims_ATriggerThatCannotSeeItsOwnTenantsRowRefuses is 00068's "when a trigger cannot
// see the row": a RESTRICTIVE SELECT policy that does not exist today, created inside the probe's
// rolled-back transaction, hides one row of the context's own tenant from tappa_app. The FK
// (which bypasses RLS) and the WITH CHECK (which reads the new row) would both let the INSERT
// through; the trigger is what refuses it. One probe per lookup the two triggers make, each with
// its control -- the same INSERT, without the policy, is written -- and its premise: under the
// policy tappa_app sees 0 of the hidden row.
func TestPunchClaims_ATriggerThatCannotSeeItsOwnTenantsRowRefuses(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	w := pcBuildWorld(t, app)
	at := w.inAt.Add(8 * time.Hour)
	claim := pcClaim(t, app, w, w.openIn, "out", at, nil)
	manual := pcManual(t, app, w, w.employee, "out", at, w.admin)

	ctx, tx := opTx(t)
	lockForDowns(t, ctx, tx, []string{"transactions", "punch_claims"})
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1::text, true)`, w.tenant); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		name, table string
		hidden      uuid.UUID
		sql         string
		args        []any
		msg         string
	}{
		{"a claim whose anchor is hidden", "transactions", w.spareIn,
			pcInsertClaim, []any{w.tenant, w.employee, w.spareIn, "out", at, nil}, "the anchor is not visible"},
		{"a decision whose claim is hidden", "punch_claims", claim,
			pcInsertDecision, []any{w.tenant, claim, "rejected", w.admin, nil, nil}, "the claim is not visible"},
		{"an approval whose record is hidden", "transactions", manual,
			pcInsertDecision, []any{w.tenant, claim, "approved", w.admin, at, manual}, "the approving record is not visible"},
		{"an approval whose claim's anchor is hidden", "transactions", w.openIn,
			pcInsertDecision, []any{w.tenant, claim, "approved", w.admin, at, manual}, "the claim's anchor is not visible"},
	} {
		control, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if e := opExecAs(t, ctx, control, "tappa_app", p.sql, p.args...); e != nil {
			t.Fatalf("%s: control without the policy was refused: %v", p.name, e)
		}
		if err := control.Rollback(ctx); err != nil {
			t.Fatalf("%s: roll the control back: %v", p.name, err)
		}

		probe, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := probe.Exec(ctx, fmt.Sprintf(`CREATE POLICY pc_probe_hide ON %s AS RESTRICTIVE FOR SELECT TO tappa_app USING (id <> '%s'::uuid)`,
			pgx.Identifier{p.table}.Sanitize(), p.hidden)); err != nil {
			t.Fatalf("%s: create the probe policy: %v", p.name, err)
		}
		var seen int
		if e := opAs(t, ctx, probe, "tappa_app", func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE id = $1`, pgx.Identifier{p.table}.Sanitize()), p.hidden).Scan(&seen)
		}); e != nil || seen != 0 {
			t.Fatalf("%s: PREMISE: under the probe policy tappa_app sees %d of the hidden row (err %v), want 0", p.name, seen, e)
		}
		err = opExecAs(t, ctx, probe, "tappa_app", p.sql, p.args...)
		if rb := probe.Rollback(ctx); rb != nil {
			t.Fatalf("%s: roll the probe back: %v", p.name, rb)
		}
		pcWant(t, p.name, err, "23514", p.msg)
	}
}

// TestPunchClaims00068_DownRefusesWhileAClaimExists is ADR 0032 "Geri alma": with a claim in the
// table 00068's Down is 55000 naming itself and changes nothing. Run in the migration role's
// transaction (rolled back), the claim written inside it, the tables the Down locks -- its own two
// and the four its DROPs reach through the keys -- taken first. The path with no rows (Down, then
// Up again) is goose's own walk, measured on a disposable database at the card's build.
func TestPunchClaims00068_DownRefusesWhileAClaimExists(t *testing.T) {
	ctx, tx := opTx(t)
	_, down := opMigrationSections(t, pc00068File)
	tables := append(downTables(down), "transactions", "employees", "admin_users", "tenants")
	if got := strings.Join(downTables(down), ","); got != "punch_claims,punch_claim_decisions,punch_claims" {
		t.Fatalf("PREMISE: the Down's own statements name %q, want its LOCK and its two DROPs", got)
	}
	lockForDowns(t, ctx, tx, tables)

	w := pcWorld{tenant: uuid.New(), location: uuid.New(), employee: uuid.New(), openIn: uuid.New()}
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`SELECT set_config('app.tenant_id', $1::text, true)`, []any{w.tenant}},
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'pc-down', $2, 'bar', 'single')`,
			[]any{w.tenant, "VAT-PC-DOWN-" + w.tenant.String()}},
		{`INSERT INTO locations (id, tenant_id, name, static_ips) VALUES ($1, $2, 'loc', '{}')`, []any{w.location, w.tenant}},
		{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Emp', 'active')`,
			[]any{w.employee, w.tenant, w.location}},
		{`INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, verdict, channel, type, business_day_start)
		  VALUES ($1, $2, $3, $4, '2026-10-19 09:12+00', 'ok', 'qr', 'in', '2026-10-19 03:00+00')`,
			[]any{w.openIn, w.tenant, w.employee, w.location}},
		{pcInsertClaim, []any{w.tenant, w.employee, w.openIn, "out", time.Date(2026, 10, 19, 17, 0, 0, 0, time.UTC), nil}},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
	before := opInt(t, ctx, tx, `SELECT count(*) FROM punch_claims`)

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sp.Conn().PgConn().Exec(ctx, down).ReadAll()
	if rb := sp.Rollback(ctx); rb != nil {
		t.Fatalf("rollback to savepoint: %v", rb)
	}
	pcWant(t, "00068's Down over a claim", err, "55000", "migration 00068 Down")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class WHERE oid IN (to_regclass('public.punch_claims'), to_regclass('public.punch_claim_decisions'))`); n != 2 {
		t.Fatalf("after the refused Down %d of the two tables exist", n)
	}
	if after := opInt(t, ctx, tx, `SELECT count(*) FROM punch_claims`); after != before {
		t.Fatalf("the refused Down changed punch_claims: %d -> %d rows", before, after)
	}
}

// TestPunchClaims00068_OpensWithASecondsLockTimeoutAndLocksBeforeItLooks pins what no walk can see
// (the 00064/00065 arrangement): the Up's and the Down's first statement is the 1 s lock_timeout --
// the Up's keys take SHARE ROW EXCLUSIVE on transactions, the Down's DROPs ACCESS EXCLUSIVE -- and
// the Down's next is the LOCK on its two tables, ahead of the DO block that looks for rows.
func TestPunchClaims00068_OpensWithASecondsLockTimeoutAndLocksBeforeItLooks(t *testing.T) {
	up, down := opMigrationSections(t, pc00068File)
	first := func(section string, n int) []string {
		var out []string
		for _, line := range strings.Split(section, "\n") {
			l := strings.TrimSpace(line)
			if l == "" || strings.HasPrefix(l, "--") {
				continue
			}
			out = append(out, l)
			if len(out) == n {
				break
			}
		}
		return out
	}
	if got := first(up, 1); len(got) != 1 || got[0] != "SET LOCAL lock_timeout = '1s';" {
		t.Errorf("00068's Up opens with %q, want the 1 s lock_timeout", got)
	}
	want := []string{"SET LOCAL lock_timeout = '1s';", "LOCK TABLE punch_claims, punch_claim_decisions IN ACCESS EXCLUSIVE MODE;", "DO $$"}
	if got := first(down, 3); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("00068's Down opens with %q, want %q", got, want)
	}
}

// pc00068Catalogue is the two tables' keys, CHECKs, indexes, policies and triggers as 00068 writes
// them -- read from the catalogue at the card's build (2026-10-23) and line by line against the
// file. The behavioural tests above prove what each one DOES; this pins that each is THERE, for the
// objects no behaviour can see go missing (punch_claims_tenant_created_idx, the queue's read, has no
// semantics a test could observe) and as the second net under the rest: a key turned tenant-less, a
// policy without its NULLIF, a trigger disabled or rebound.
const pc00068Catalogue = `
con punch_claim_decisions punch_claim_decisions_claim_fk FOREIGN KEY (claim_id, tenant_id) REFERENCES punch_claims(id, tenant_id) ON DELETE RESTRICT
con punch_claim_decisions punch_claim_decisions_decided_by_fk FOREIGN KEY (decided_by, tenant_id) REFERENCES admin_users(id, tenant_id) ON DELETE RESTRICT
con punch_claim_decisions punch_claim_decisions_one_per_claim UNIQUE (tenant_id, claim_id)
con punch_claim_decisions punch_claim_decisions_one_per_record UNIQUE (tenant_id, transaction_id)
con punch_claim_decisions punch_claim_decisions_outcome_is_known CHECK ((outcome = ANY (ARRAY['approved'::text, 'rejected'::text])))
con punch_claim_decisions punch_claim_decisions_pkey PRIMARY KEY (id)
con punch_claim_decisions punch_claim_decisions_shape_by_outcome CHECK ((((outcome = 'approved'::text) AND (transaction_id IS NOT NULL) AND (approved_at IS NOT NULL)) OR ((outcome = 'rejected'::text) AND (transaction_id IS NULL) AND (approved_at IS NULL))))
con punch_claim_decisions punch_claim_decisions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT
con punch_claim_decisions punch_claim_decisions_transaction_fk FOREIGN KEY (transaction_id, tenant_id) REFERENCES transactions(id, tenant_id) ON DELETE RESTRICT
con punch_claims punch_claims_anchor_fk FOREIGN KEY (anchor_transaction_id, tenant_id) REFERENCES transactions(id, tenant_id) ON DELETE RESTRICT
con punch_claims punch_claims_employee_fk FOREIGN KEY (employee_id, tenant_id) REFERENCES employees(id, tenant_id) ON DELETE RESTRICT
con punch_claims punch_claims_id_tenant_key UNIQUE (id, tenant_id)
con punch_claims punch_claims_kind_is_a_direction CHECK ((kind = ANY (ARRAY['in'::text, 'out'::text])))
con punch_claims punch_claims_one_per_anchor UNIQUE (tenant_id, anchor_transaction_id)
con punch_claims punch_claims_pkey PRIMARY KEY (id)
con punch_claims punch_claims_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT
idx punch_claim_decisions CREATE UNIQUE INDEX punch_claim_decisions_one_per_claim ON public.punch_claim_decisions USING btree (tenant_id, claim_id)
idx punch_claim_decisions CREATE UNIQUE INDEX punch_claim_decisions_one_per_record ON public.punch_claim_decisions USING btree (tenant_id, transaction_id)
idx punch_claim_decisions CREATE UNIQUE INDEX punch_claim_decisions_pkey ON public.punch_claim_decisions USING btree (id)
idx punch_claims CREATE INDEX punch_claims_tenant_created_idx ON public.punch_claims USING btree (tenant_id, created_at DESC)
idx punch_claims CREATE UNIQUE INDEX punch_claims_id_tenant_key ON public.punch_claims USING btree (id, tenant_id)
idx punch_claims CREATE UNIQUE INDEX punch_claims_one_per_anchor ON public.punch_claims USING btree (tenant_id, anchor_transaction_id)
idx punch_claims CREATE UNIQUE INDEX punch_claims_pkey ON public.punch_claims USING btree (id)
pol punch_claim_decisions punch_claim_decisions_tenant_isolation * true (tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid) | (tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)
pol punch_claims punch_claims_tenant_isolation * true (tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid) | (tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)
trg punch_claim_decisions O CREATE TRIGGER punch_claim_decisions_no_mutation BEFORE DELETE OR UPDATE ON public.punch_claim_decisions FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation()
trg punch_claim_decisions O CREATE TRIGGER punch_claim_decisions_no_truncate BEFORE TRUNCATE ON public.punch_claim_decisions FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation()
trg punch_claim_decisions O CREATE TRIGGER punch_claim_decisions_validate BEFORE INSERT ON public.punch_claim_decisions FOR EACH ROW EXECUTE FUNCTION tappa_check_punch_claim_decision()
trg punch_claims O CREATE TRIGGER punch_claims_no_mutation BEFORE DELETE OR UPDATE ON public.punch_claims FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation()
trg punch_claims O CREATE TRIGGER punch_claims_no_truncate BEFORE TRUNCATE ON public.punch_claims FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation()
trg punch_claims O CREATE TRIGGER punch_claims_validate BEFORE INSERT ON public.punch_claims FOR EACH ROW EXECUTE FUNCTION tappa_check_punch_claim()
`

// TestPunchClaims00068_TheCatalogueIsTheOneWritten reads pc00068Catalogue's four kinds of line from
// the live database and compares the two sets.
func TestPunchClaims00068_TheCatalogueIsTheOneWritten(t *testing.T) {
	ctx, tx := opTx(t)
	rows, err := tx.Query(ctx, `
		SELECT line FROM (
		  SELECT 'con ' || c.conrelid::regclass::text || ' ' || c.conname || ' ' || pg_get_constraintdef(c.oid) AS line
		    FROM pg_constraint c WHERE c.conrelid IN ('public.punch_claims'::regclass, 'public.punch_claim_decisions'::regclass)
		  UNION ALL
		  SELECT 'idx ' || i.tablename || ' ' || i.indexdef
		    FROM pg_indexes i WHERE i.schemaname = 'public' AND i.tablename IN ('punch_claims', 'punch_claim_decisions')
		  UNION ALL
		  SELECT 'pol ' || p.polrelid::regclass::text || ' ' || p.polname || ' ' || p.polcmd::text || ' ' || p.polpermissive::text
		         || ' ' || coalesce(pg_get_expr(p.polqual, p.polrelid), '-') || ' | ' || coalesce(pg_get_expr(p.polwithcheck, p.polrelid), '-')
		    FROM pg_policy p WHERE p.polrelid IN ('public.punch_claims'::regclass, 'public.punch_claim_decisions'::regclass)
		  UNION ALL
		  SELECT 'trg ' || g.tgrelid::regclass::text || ' ' || g.tgenabled::text || ' ' || pg_get_triggerdef(g.oid)
		    FROM pg_trigger g WHERE g.tgrelid IN ('public.punch_claims'::regclass, 'public.punch_claim_decisions'::regclass) AND NOT g.tgisinternal
		) s ORDER BY line COLLATE "C"`)
	if err != nil {
		t.Fatalf("read the catalogue: %v", err)
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read the catalogue: %v", err)
	}
	want := strings.Split(strings.TrimSpace(pc00068Catalogue), "\n")
	have := map[string]bool{}
	for _, l := range got {
		have[l] = true
	}
	for _, l := range want {
		if !have[l] {
			t.Errorf("00068 wrote this and the database does not hold it:\n  %s", l)
		}
		delete(have, l)
	}
	for l := range have {
		t.Errorf("the database holds this and 00068's pin does not:\n  %s", l)
	}
}
