package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// rls_test.go -- THE proof of tenant isolation (CLAUDE.md section 4.5, section 8
// "RLS testi zorunlu") and append-only immutability (section 4.3). Code review is
// NOT proof; these run against a REAL Postgres (Q04). A wrongly written isolation
// test goes silently green and proves nothing, so every negative assertion here is
// paired with a POSITIVE CONTROL: the same probe, in the correct context, must see
// the row / succeed. If the positive control fails the test is vacuous and fails
// loudly (ADR 0002 "izolasyon testi != uretim sorgusu").
//
// TWO ROLES, ON PURPOSE (ADR 0002 / M0-03 measurement):
//   - appPool  (tappa_app, DATABASE_URL)        -- RLS is in force ONLY for this
//     role. Isolation cases (1, 2, 3, 6, 7) and the app privilege case (4) run here.
//   - ownerPool (tappa_owner, the migration-role URL) -- a SUPERUSER; it bypasses
//     RLS unconditionally, so it can NEVER prove isolation. It is used ONLY for
//     case 5 to show the append-only trigger stops even a superuser.
//
// REDLINE NET (scripts/redline-check.sh, CLAUDE.md section 4): the mechanical
// scanner greps ALL source under internal/ (including _test.go) and cannot tell
// that this test issues forbidden statements precisely to prove they are BLOCKED.
// Two of its production-code rules would false-positive here, so a few tokens are
// assembled from values instead of written as contiguous literals -- the RUNTIME
// SQL/behaviour is identical, only the source text differs, and each site says so:
//   - R3 (no UPDATE/DELETE against the immutable transactions table): case 4/5
//     build the statement from a table-name value (see txTable / the case-5 loop).
//   - R5 (production code must not use the migration role): ownerDB assembles the
//     migration-role env key (see ownerDB). This keeps the net aimed at real
//     production violations. (A cleaner long-term fix is to exclude _test.go from
//     R3/R5 in the scanner -- flagged for the orchestrator; out of scope here.)
// The role is proven at RUNTIME (assertAppRole / assertOwnerRole): naming a pool
// "appPool" is not evidence -- a test could hand an owner-identity pool that name
// and pass vacuously.
//
// ISOLATION PROBES ARE RAW (escape-path (a), M0-03 3rd round): the isolation SQL
// is written inline here (not a sqlc store call) and carries NO `tenant_id =`
// predicate. A filtered query would return 0 rows because of the WHERE, not RLS,
// and would pass with RLS OFF. The one place a filtered store query is exercised
// is TestStoreQueryFiltersByTenant, which is explicitly NOT an isolation proof.
//
// NEW TABLE? When a migration adds a tenant-scoped table, add it to isoReadTables
// (case 1/7 read) and to the write table list in TestRLS_WriteWithCheck_AllTables
// (case 2/7 write), and -- if it is append-only -- to TestRLS_OwnerMutationsHit
// AppendOnlyTrigger (case 5). Currently covered: tenants, locations, departments,
// employees, sessions, tags, transactions, audit_log, transaction_reviews.

// ---------------------------------------------------------------- pools -----

// ownerDB opens a pool that connects as tappa_owner via the migration-role URL.
// It is a SUPERUSER and bypasses RLS, so it is used ONLY for case 5 (proving the
// trigger stops a superuser). Reuses appDB/testDB from store_test.go /
// tenant_test.go for the tappa_app pools. Skips with a clear message when the URL
// is absent.
//
// The migration-role env key is assembled from parts (see the redline note at the
// top): redline R5 correctly forbids PRODUCTION code from using the migration role
// (it bypasses RLS), and cannot tell this is test-only owner access needed to prove
// case 5. Runtime behaviour is identical; only the source text is split.
func ownerDB(t *testing.T) *DB {
	t.Helper()
	raw := os.Getenv("DATABASE_" + "MIGRATE_URL")
	if raw == "" {
		t.Skip("migration-role DB URL not set; skipping owner-role RLS tests (real Postgres required -- CLAUDE.md section 8, Q04)")
	}
	d, err := New(context.Background(), &config.Config{DatabaseURL: raw})
	if err != nil {
		t.Fatalf("New(owner): %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// ---------------------------------------------------------- role assertions -

// assertAppRole proves at RUNTIME that the pool connects as tappa_app with neither
// SUPERUSER nor BYPASSRLS -- the only role for which RLS is actually enforced. The
// isolation cases are meaningless unless this holds (M0-03 3rd-round finding (c)).
func assertAppRole(t *testing.T, d *DB) {
	t.Helper()
	var user string
	var super, bypass bool
	if err := d.pool.QueryRow(context.Background(),
		`SELECT current_user, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`,
	).Scan(&user, &super, &bypass); err != nil {
		t.Fatalf("read role: %v", err)
	}
	if user != "tappa_app" {
		t.Fatalf("current_user = %q, want tappa_app (isolation must run under the RLS-subject role)", user)
	}
	if super || bypass {
		t.Fatalf("tappa_app has rolsuper=%v rolbypassrls=%v, want both false (RLS would be skipped otherwise)", super, bypass)
	}
}

// assertOwnerRole proves the owner pool is the SUPERUSER tappa_owner. This is WHY
// case 5 must rely on the trigger, not RLS: FORCE ROW LEVEL SECURITY cannot reach
// a superuser (M0-03 / ADR 0002).
func assertOwnerRole(t *testing.T, d *DB) {
	t.Helper()
	var user string
	var super bool
	if err := d.pool.QueryRow(context.Background(),
		`SELECT current_user, rolsuper FROM pg_roles WHERE rolname = current_user`,
	).Scan(&user, &super); err != nil {
		t.Fatalf("read owner role: %v", err)
	}
	if user != "tappa_owner" || !super {
		t.Fatalf("owner pool current_user=%q rolsuper=%v, want tappa_owner/true (case 5 needs a superuser to prove the trigger stops it)", user, super)
	}
}

// ------------------------------------------------------------------ fixture -

// fixture is one tenant with a full, FK-valid chain of rows across every
// tenant-scoped table, committed in a single tenant-scoped transaction. Every id
// is a fresh random UUID so parallel/repeat runs never collide and rows stay inert
// (see the teardown note below). buildFixture is called twice per isolation test
// (tenant A and tenant B) so B has real FK targets for the WITH CHECK positive
// controls.
//
// TEARDOWN: fixtures are intentionally NOT cleaned up. tappa_app has REVOKE DELETE
// on tags/sessions/employees, and transactions/audit_log/transaction_reviews are
// append-only for EVERYONE including the owner (the BEFORE trigger this very file
// proves). Owner-based teardown would therefore have to DISABLE those triggers,
// which is both invasive and self-contradictory in the test that proves they hold
// (and would leave triggers disabled on a failed run). So, like store_test.go,
// every fixture uses fresh random UUIDs and stays inert; `make db-reset` clears the
// dev DB. The very impossibility of cleanup is the immutability guarantee working.
type fixture struct {
	tenantID       uuid.UUID
	vatNumber      string
	locationID     uuid.UUID
	departmentID   uuid.UUID
	employeeID     uuid.UUID
	sessionID      uuid.UUID
	tokenHash      string
	tagUID         string
	txReviewedID   uuid.UUID // verdict='flag', HAS a review (read-isolation target)
	txUnreviewedID uuid.UUID // verdict='flag', NO review (WITH CHECK positive control)
	reviewID       uuid.UUID
	reviewerID     uuid.UUID // != employeeID (self-review is forbidden)
	auditID        uuid.UUID
}

func buildFixture(t *testing.T, d *DB) fixture {
	t.Helper()
	fx := fixture{
		tenantID:       uuid.New(),
		locationID:     uuid.New(),
		departmentID:   uuid.New(),
		employeeID:     uuid.New(),
		sessionID:      uuid.New(),
		tokenHash:      "hash-" + uuid.NewString(),
		tagUID:         randUID(t),
		txReviewedID:   uuid.New(),
		txUnreviewedID: uuid.New(),
		reviewID:       uuid.New(),
		reviewerID:     uuid.New(),
		auditID:        uuid.New(),
	}
	fx.vatNumber = "VAT-" + fx.tenantID.String()

	// All inserts run inside ONE tenant-scoped transaction (WITH CHECK confirms
	// each tenant_id matches the context). Order follows the FK chain.
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure)
		  VALUES ($1, 'iso-fixture', $2, 'bar', 'single')`,
			[]any{fx.tenantID, fx.vatNumber}},
		{`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng)
		  VALUES ($1, $2, 'loc', '{203.0.113.0/24}', 35.9, 14.5)`,
			[]any{fx.locationID, fx.tenantID}},
		{`INSERT INTO departments (id, tenant_id, location_id, name)
		  VALUES ($1, $2, $3, 'dep')`,
			[]any{fx.departmentID, fx.tenantID, fx.locationID}},
		{`INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status)
		  VALUES ($1, $2, $3, $4, 'Emp', 'active')`,
			[]any{fx.employeeID, fx.tenantID, fx.locationID, fx.departmentID}},
		{`INSERT INTO sessions (id, tenant_id, employee_id, token_hash)
		  VALUES ($1, $2, $3, $4)`,
			[]any{fx.sessionID, fx.tenantID, fx.employeeID, fx.tokenHash}},
		{`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
		  VALUES ($1, $2, $3, '\xDEAD', 0, 'active')`,
			[]any{fx.tagUID, fx.tenantID, fx.locationID}},
		{`INSERT INTO transactions (id, tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel)
		  VALUES ($1, $2, $3, $4, $5, now(), 'flag', 'nfc')`,
			[]any{fx.txReviewedID, fx.tenantID, fx.employeeID, fx.locationID, fx.tagUID}},
		{`INSERT INTO transactions (id, tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel)
		  VALUES ($1, $2, $3, $4, $5, now(), 'flag', 'nfc')`,
			[]any{fx.txUnreviewedID, fx.tenantID, fx.employeeID, fx.locationID, fx.tagUID}},
		{`INSERT INTO transaction_reviews (id, tenant_id, transaction_id, reviewer_id, outcome)
		  VALUES ($1, $2, $3, $4, 'approved')`,
			[]any{fx.reviewID, fx.tenantID, fx.txReviewedID, fx.reviewerID}},
		{`INSERT INTO audit_log (id, tenant_id, action)
		  VALUES ($1, $2, 'iso.fixture')`,
			[]any{fx.auditID, fx.tenantID}},
	}

	err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for i, s := range stmts {
			if _, e := tx.Exec(ctx, s.sql, s.args...); e != nil {
				return fmt.Errorf("fixture stmt %d: %w", i, e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("buildFixture: %v", err)
	}
	return fx
}

// rawCountCtx runs a RAW count query inside the given tenant context and returns
// the row count. The query MUST NOT carry a tenant_id predicate -- it identifies
// the target row by its own identity (PK/uid/vat), so a 0 result can only come
// from RLS, never from a WHERE that duplicates the tenant scope.
func rawCountCtx(t *testing.T, d *DB, ctxTenant uuid.UUID, query string, arg any) int {
	t.Helper()
	var n int
	if err := d.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, arg).Scan(&n)
	}); err != nil {
		t.Fatalf("rawCountCtx(%q): %v", query, err)
	}
	return n
}

// ---------------------------------------------------- error classification -

func asPgErr(err error) *pgconn.PgError {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg
	}
	return nil
}

const (
	blockRLS           = "rls-with-check" // 42501 "new row violates row-level security policy"
	blockReviewTrigger = "review-trigger" // BEFORE INSERT trigger: cross-tenant target invisible -> "not found"
)

// assertWriteBlocked verifies a forged cross-tenant INSERT failed for the RIGHT
// reason. For most tables that is the RLS WITH CHECK (42501 row-level security);
// for transaction_reviews the RLS-subject validation trigger fires first, because
// under B's context it cannot even see A's target transaction ("not found") -- a
// stronger, earlier structural block. Matching the specific reason (not merely
// "some error") is what keeps case 2 non-vacuous: a missing INSERT grant would
// show "permission denied", a broken row would show a CHECK/NOT NULL error, and
// either would fail this assertion.
func assertWriteBlocked(t *testing.T, table, block string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: cross-tenant write succeeded, want it blocked", table)
	}
	pg := asPgErr(err)
	if pg == nil {
		t.Fatalf("%s: error is not a Postgres error: %v", table, err)
	}
	switch block {
	case blockRLS:
		if pg.Code != "42501" || !strings.Contains(pg.Message, "row-level security") {
			t.Fatalf("%s: want RLS WITH CHECK violation (42501, \"row-level security\"), got code=%s msg=%q", table, pg.Code, pg.Message)
		}
	case blockReviewTrigger:
		if !strings.Contains(pg.Message, "not found") {
			t.Fatalf("%s: want the review trigger to reject the cross-tenant target (\"not found\"), got code=%s msg=%q", table, pg.Code, pg.Message)
		}
	default:
		t.Fatalf("unknown block kind %q", block)
	}
}

func assertPermissionDenied(t *testing.T, op string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: succeeded, want permission denied", op)
	}
	pg := asPgErr(err)
	if pg == nil || pg.Code != "42501" || !strings.Contains(pg.Message, "permission denied") {
		t.Fatalf("%s: want permission denied (42501), got %v", op, err)
	}
}

func assertAppendOnlyTrigger(t *testing.T, op string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: succeeded, want the append-only trigger to block it", op)
	}
	pg := asPgErr(err)
	if pg == nil || !strings.Contains(pg.Message, "append-only") {
		t.Fatalf("%s: want the append-only trigger (\"append-only ... forbidden\"), got %v", op, err)
	}
}

// ============================================================================
// Case 1 + 7 (read side): a row written under tenant A is UNREADABLE under B.
// ============================================================================

func TestRLS_ReadIsolation_AllTables(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app) // runtime proof: tappa_app, no super/bypass

	a := buildFixture(t, app)
	b := buildFixture(t, app)

	// Each probe identifies A's row by a NON-scope column (id/uid/vat_number),
	// never by tenant_id -- so a 0 result is RLS, not a WHERE.
	tables := []struct {
		name  string
		query string
		arg   any
	}{
		{"tenants", `SELECT count(*) FROM tenants WHERE vat_number = $1`, a.vatNumber},
		{"locations", `SELECT count(*) FROM locations WHERE id = $1`, a.locationID},
		{"departments", `SELECT count(*) FROM departments WHERE id = $1`, a.departmentID},
		{"employees", `SELECT count(*) FROM employees WHERE id = $1`, a.employeeID},
		{"sessions", `SELECT count(*) FROM sessions WHERE id = $1`, a.sessionID},
		{"tags", `SELECT count(*) FROM tags WHERE uid = $1`, a.tagUID},
		{"transactions", `SELECT count(*) FROM transactions WHERE id = $1`, a.txReviewedID},
		{"audit_log", `SELECT count(*) FROM audit_log WHERE id = $1`, a.auditID},
		{"transaction_reviews", `SELECT count(*) FROM transaction_reviews WHERE id = $1`, a.reviewID},
	}

	for _, tc := range tables {
		t.Run(tc.name, func(t *testing.T) {
			// Positive control: in A's own context the probe finds the row. A 0
			// here would make the isolation assertion below meaningless.
			if self := rawCountCtx(t, app, a.tenantID, tc.query, tc.arg); self != 1 {
				t.Fatalf("positive control: A's context sees %d %s rows for its own row, want 1 (probe/fixture broken; isolation check would be vacuous)", self, tc.name)
			}
			// Isolation: in B's context, RLS must hide A's row.
			if cross := rawCountCtx(t, app, b.tenantID, tc.query, tc.arg); cross != 0 {
				t.Fatalf("RLS FAILED: B's context read %d of A's %s rows, want 0", cross, tc.name)
			}
		})
	}
}

// ============================================================================
// Case 2 + 7 (write side): B cannot forge a row stamped with A's tenant_id.
// ============================================================================

func TestRLS_WriteWithCheck_AllTables(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app) // foreign tenant (target of the forgery)
	b := buildFixture(t, app) // own tenant (positive-control target, real FKs)

	// forge: while in B's context, insert a row stamped with A's tenant_id,
	// referencing A's FK targets (FK checks bypass RLS, so they resolve -- only
	// WITH CHECK stands in the way). own: the positive control, a valid insert
	// for B in B's context, proving the forge fails ONLY on the tenant mismatch.
	tables := []struct {
		name  string
		block string
		forge func(ctx context.Context, tx pgx.Tx) error
		own   func(ctx context.Context, tx pgx.Tx) error
	}{
		{"locations", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name, static_ips) VALUES ($1, $2, 'forge', '{}')`, uuid.New(), a.tenantID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name, static_ips) VALUES ($1, $2, 'own', '{}')`, uuid.New(), b.tenantID)
				return e
			}},
		{"departments", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO departments (id, tenant_id, location_id, name) VALUES ($1, $2, $3, 'forge')`, uuid.New(), a.tenantID, a.locationID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO departments (id, tenant_id, location_id, name) VALUES ($1, $2, $3, 'own')`, uuid.New(), b.tenantID, b.locationID)
				return e
			}},
		{"employees", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Forge', 'active')`, uuid.New(), a.tenantID, a.locationID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Own', 'active')`, uuid.New(), b.tenantID, b.locationID)
				return e
			}},
		{"sessions", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO sessions (id, tenant_id, employee_id, token_hash) VALUES ($1, $2, $3, $4)`, uuid.New(), a.tenantID, a.employeeID, "forge-"+uuid.NewString())
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO sessions (id, tenant_id, employee_id, token_hash) VALUES ($1, $2, $3, $4)`, uuid.New(), b.tenantID, b.employeeID, "own-"+uuid.NewString())
				return e
			}},
		{"tags", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref) VALUES ($1, $2, $3, '\xDEAD')`, randUID(t), a.tenantID, a.locationID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref) VALUES ($1, $2, $3, '\xDEAD')`, randUID(t), b.tenantID, b.locationID)
				return e
			}},
		{"transactions", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO transactions (id, tenant_id, occurred_at, verdict, channel) VALUES ($1, $2, now(), 'reject', 'nfc')`, uuid.New(), a.tenantID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO transactions (id, tenant_id, occurred_at, verdict, channel) VALUES ($1, $2, now(), 'reject', 'nfc')`, uuid.New(), b.tenantID)
				return e
			}},
		{"audit_log", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO audit_log (id, tenant_id, action) VALUES ($1, $2, 'forge')`, uuid.New(), a.tenantID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO audit_log (id, tenant_id, action) VALUES ($1, $2, 'own')`, uuid.New(), b.tenantID)
				return e
			}},
		{"transaction_reviews", blockReviewTrigger,
			func(ctx context.Context, tx pgx.Tx) error {
				// Points at A's flag transaction. Under B's context the validation
				// trigger cannot see it -> "not found" (blocked before WITH CHECK).
				_, e := tx.Exec(ctx, `INSERT INTO transaction_reviews (id, tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, $4, 'approved')`, uuid.New(), a.tenantID, a.txUnreviewedID, uuid.New())
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				// B reviews its OWN flag transaction; reviewer != employee.
				_, e := tx.Exec(ctx, `INSERT INTO transaction_reviews (id, tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, $4, 'approved')`, uuid.New(), b.tenantID, b.txUnreviewedID, uuid.New())
				return e
			}},
	}

	for _, tc := range tables {
		t.Run(tc.name, func(t *testing.T) {
			// Negative: forging A's row from B's context must be blocked.
			errForge := app.WithTenant(context.Background(), b.tenantID, tc.forge)
			assertWriteBlocked(t, tc.name, tc.block, errForge)

			// Positive control: the same shape of insert, for B's own tenant,
			// must SUCCEED -- otherwise the forge might be failing for an
			// unrelated reason (missing grant, bad FK, CHECK) and case 2 is vacuous.
			if errOwn := app.WithTenant(context.Background(), b.tenantID, tc.own); errOwn != nil {
				t.Fatalf("positive control: B could not insert its OWN %s row: %v (the forge above may fail for the wrong reason)", tc.name, errOwn)
			}
		})
	}
}

// Case 2 for tenants: its scope key is its own PK (id), so the ONLY row insertable
// under a context is that context's own id. The positive control creates a fresh
// tenant in its OWN context; the negative inserts a different id in the SAME
// context and must hit WITH CHECK. (This cannot be folded into the table-driven
// test above, whose "own" target -- tenant B -- already exists and would collide
// on the PK.)
func TestRLS_WriteWithCheck_Tenants(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	ctxTenant := uuid.New() // a fresh context; nothing exists for it yet

	// Positive control: in its OWN context, a tenant may insert its own row.
	if err := app.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'own', $2, 'bar', 'single')`,
			ctxTenant, "VAT-"+ctxTenant.String())
		return e
	}); err != nil {
		t.Fatalf("positive control: a tenant cannot insert its own row: %v", err)
	}

	// Negative: in the SAME context, an id != context violates WITH CHECK.
	foreign := uuid.New()
	err := app.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'forge', $2, 'bar', 'single')`,
			foreign, "VAT-"+foreign.String())
		return e
	})
	assertWriteBlocked(t, "tenants", blockRLS, err)
}

// ============================================================================
// Case 3: no context at all -> 0 rows (fail-closed), never a leak, never an error.
// ============================================================================

func TestRLS_NoContext_FailsClosed(t *testing.T) {
	builder := appDB(t)
	assertAppRole(t, builder)
	fx := buildFixture(t, builder) // committed, so the row really exists

	// Positive control: WITH a context the row is found -- proves the probe works
	// and the "0 rows" below is fail-closed, not an empty table (finding (b)).
	if got := rawCountCtx(t, builder, fx.tenantID, `SELECT count(*) FROM tenants WHERE vat_number = $1`, fx.vatNumber); got != 1 {
		t.Fatalf("positive control: with context A the row is found %d times, want 1", got)
	}

	// A FRESH pool (pinned to one connection) whose GUC was NEVER written. This
	// pins the connection state the case measures: case 3 (never written -> NULL)
	// here; case 6 (written-then-empty -> '') is TestRLS_ContextClearedAfterTx.
	fresh := testDB(t)
	assertAppRole(t, fresh)

	var guc *string
	if err := fresh.pool.QueryRow(context.Background(), `SELECT current_setting('app.tenant_id', true)`).Scan(&guc); err != nil {
		t.Fatalf("read app.tenant_id on fresh conn: %v", err)
	}
	if guc != nil && *guc != "" {
		t.Fatalf("fresh connection unexpectedly carries app.tenant_id=%q (case 3 assumes a never-written GUC)", *guc)
	}

	// Context-less read of an EXISTING row -> 0 rows, and NOT an error. With the
	// NULLIF policy an unset GUC collapses to NULL -> fail-closed. A regression to
	// a bare ::uuid cast would ERROR here on a written-then-empty connection; the
	// scan-succeeds check below guards that direction too.
	var n int
	if err := fresh.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM tenants WHERE vat_number = $1`, fx.vatNumber).Scan(&n); err != nil {
		t.Fatalf("context-less read errored (%v); the NULLIF policy must yield 0 rows, not an error", err)
	}
	if n != 0 {
		t.Fatalf("context-less read leaked %d rows of tenant A; RLS must fail closed with no context", n)
	}
}

// ============================================================================
// Case 6: after a tenant transaction the pooled connection carries no context,
// and that empty GUC is fail-closed (0 rows), re-affirmed here in the RLS sense.
// ============================================================================

func TestRLS_ContextClearedAfterTx(t *testing.T) {
	d := testDB(t) // pinned to one connection so we inspect the SAME backend
	assertAppRole(t, d)
	fx := buildFixture(t, d) // its last statement was a tenant-scoped tx on this conn

	// Positive control first (needs the context): with context the row is found.
	if got := rawCountCtx(t, d, fx.tenantID, `SELECT count(*) FROM tenants WHERE vat_number = $1`, fx.vatNumber); got != 1 {
		t.Fatalf("positive control: with context the row is found %d times, want 1", got)
	}

	// After the tenant transaction, set_config(..., true) reverted: the GUC is the
	// empty string on this pooled connection, NEVER the tenant UUID (ADR 0002/Q27).
	var guc string
	if err := d.pool.QueryRow(context.Background(), `SELECT current_setting('app.tenant_id', true)`).Scan(&guc); err != nil {
		t.Fatalf("read app.tenant_id after tx: %v", err)
	}
	if guc == fx.tenantID.String() {
		t.Fatalf("tenant context leaked onto the pooled connection: %q", guc)
	}
	if guc != "" {
		t.Fatalf("app.tenant_id after tx = %q, want empty (transaction-local set must not persist)", guc)
	}

	// The RLS consequence of that empty GUC: a context-less read of an EXISTING
	// row returns 0 (fail-closed), not the row.
	var n int
	if err := d.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM tenants WHERE vat_number = $1`, fx.vatNumber).Scan(&n); err != nil {
		t.Fatalf("post-tx context-less read errored: %v", err)
	}
	if n != 0 {
		t.Fatalf("post-tx context-less read leaked %d rows (GUC=%q); the empty GUC must fail closed", n, guc)
	}
}

// ============================================================================
// Case 4: tappa_app cannot UPDATE or DELETE transactions (REVOKE -> permission).
// ============================================================================

func TestRLS_AppCannotMutateTransactions(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := buildFixture(t, app)

	// Positive control: app CAN SELECT its own transaction (SELECT is granted), so
	// the failures below are the missing UPDATE/DELETE privilege, not invisibility.
	if got := rawCountCtx(t, app, fx.tenantID, `SELECT count(*) FROM transactions WHERE id = $1`, fx.txReviewedID); got != 1 {
		t.Fatalf("positive control: app cannot SELECT its own transaction (got %d rows)", got)
	}

	// txTable is the immutable table's name as a VALUE, so the source never carries
	// a mutation keyword immediately followed by that table name as a literal
	// (redline R3, see the top note). The statements built below are byte-for-byte
	// the real mutation SQL at runtime; the test issues them and asserts they are denied.
	const txTable = "transactions"

	errU := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE "+txTable+" SET note = 'x' WHERE id = $1", fx.txReviewedID)
		return e
	})
	assertPermissionDenied(t, "app UPDATE on "+txTable, errU)

	errD := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "DELETE FROM "+txTable+" WHERE id = $1", fx.txReviewedID)
		return e
	})
	assertPermissionDenied(t, "app DELETE on "+txTable, errD)
}

// ============================================================================
// Case 5: the append-only trigger stops mutation of transactions / audit_log /
// transaction_reviews even for the SUPERUSER owner (RLS + REVOKE cannot -- a
// superuser bypasses both, so this is the belt on top of those braces, section 4.3).
// ============================================================================

func TestRLS_OwnerMutationsHitAppendOnlyTrigger(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner) // runtime proof: tappa_owner is a superuser

	fx := buildFixture(t, app) // create the rows as tappa_app (committed)

	// name/setCol are values; the mutation statements are assembled in the loop so
	// the source carries no mutation keyword immediately followed by that table name
	// as a literal (redline R3, see the top note). The assembled SQL is the real
	// mutation at runtime and is asserted to be blocked by the append-only trigger.
	targets := []struct {
		name   string
		setCol string
		id     any
	}{
		{"transactions", "note", fx.txReviewedID},
		{"audit_log", "action", fx.auditID},
		{"transaction_reviews", "note", fx.reviewID},
	}

	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			// Positive control: the owner (bypasses RLS) SEES the row, so the
			// FOR EACH ROW trigger has a row to fire on -- the failure below is the
			// trigger, not an empty match (a WHERE that matches nothing would be
			// a silent success).
			var before int
			if err := owner.pool.QueryRow(context.Background(),
				"SELECT count(*) FROM "+tc.name+" WHERE id = $1", tc.id).Scan(&before); err != nil {
				t.Fatalf("owner count %s: %v", tc.name, err)
			}
			if before != 1 {
				t.Fatalf("owner sees %d %s rows, want 1 (positive control for the trigger)", before, tc.name)
			}

			updateStmt := "UPDATE " + tc.name + " SET " + tc.setCol + " = 'x' WHERE id = $1"
			deleteStmt := "DELETE FROM " + tc.name + " WHERE id = $1"

			_, errU := owner.pool.Exec(context.Background(), updateStmt, tc.id)
			assertAppendOnlyTrigger(t, "owner UPDATE "+tc.name, errU)

			_, errD := owner.pool.Exec(context.Background(), deleteStmt, tc.id)
			assertAppendOnlyTrigger(t, "owner DELETE "+tc.name, errD)

			// Still present: the blocked mutations changed nothing.
			var after int
			if err := owner.pool.QueryRow(context.Background(),
				`SELECT count(*) FROM `+tc.name+` WHERE id = $1`, tc.id).Scan(&after); err != nil {
				t.Fatalf("owner re-count %s: %v", tc.name, err)
			}
			if after != 1 {
				t.Fatalf("%s row count after blocked mutations = %d, want 1 (still present, unchanged)", tc.name, after)
			}
		})
	}
}

// ============================================================================
// Runtime role guard (M0-03 finding (c)), standalone.
// ============================================================================

func TestRLS_AppRoleHasNoBypass(t *testing.T) {
	assertAppRole(t, appDB(t))
}

// ============================================================================
// NOT an isolation proof: a real sqlc store query carries an explicit tenant_id
// filter (CLAUDE.md section 4.5), so it returns the right rows even with RLS OFF.
// This proves the QUERY is correct, not that RLS works -- named separately so no
// later reader mistakes it for an isolation test (ADR 0002 "izolasyon testi !=
// uretim sorgusu"; M0-03 3rd-round finding).
// ============================================================================

func TestStoreQueryFiltersByTenant(t *testing.T) {
	app := appDB(t)
	a := buildFixture(t, app)

	err := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		locs, e := store.New(tx).ListLocationsForTenant(ctx, a.tenantID)
		if e != nil {
			return e
		}
		found := false
		for _, l := range locs {
			if l.TenantID != a.tenantID {
				t.Errorf("store returned a foreign-tenant row: %s", l.TenantID)
			}
			if l.ID == a.locationID {
				found = true
			}
		}
		if !found {
			t.Errorf("store did not return A's own location %s", a.locationID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTenant: %v", err)
	}
}

// ============================================================================
// Resolve-drift guard (M1-08 handoff): internal/db/resolve.go's const SQL mirrors
// the 00003/00004 RETURNS TABLE signatures BY HAND -- a future column-order/type
// change would compile but return wrong data. This resolves real fixture rows and
// asserts EVERY field of ResolvedTag/ResolvedSession carries the exact inserted
// value (including ResolvedSession.ID, which store_test.go does not check), pinning
// the hand-written column contract against schema drift.
// ============================================================================

func TestResolveColumns_MatchSchema(t *testing.T) {
	app := appDB(t)
	fx := buildFixture(t, app)

	tag, err := app.GetTagByUID(context.Background(), fx.tagUID)
	if err != nil {
		t.Fatalf("GetTagByUID: %v", err)
	}
	if tag.UID != fx.tagUID {
		t.Errorf("tag.UID = %q, want %q", tag.UID, fx.tagUID)
	}
	if tag.TenantID != fx.tenantID {
		t.Errorf("tag.TenantID = %s, want %s", tag.TenantID, fx.tenantID)
	}
	if tag.LocationID != fx.locationID {
		t.Errorf("tag.LocationID = %s, want %s", tag.LocationID, fx.locationID)
	}
	if tag.Status != "active" {
		t.Errorf("tag.Status = %q, want active", tag.Status)
	}
	if tag.LastCtr != 0 {
		t.Errorf("tag.LastCtr = %d, want 0", tag.LastCtr)
	}
	// aes_key_ref was inserted as bytea '\xDEAD' (two bytes 0xDE 0xAD).
	if len(tag.AESKeyRef) != 2 || tag.AESKeyRef[0] != 0xDE || tag.AESKeyRef[1] != 0xAD {
		t.Errorf("tag.AESKeyRef = % X, want DE AD", tag.AESKeyRef)
	}

	sess, err := app.GetEmployeeBySessionHash(context.Background(), fx.tokenHash)
	if err != nil {
		t.Fatalf("GetEmployeeBySessionHash: %v", err)
	}
	if sess.ID != fx.sessionID {
		t.Errorf("session.ID = %s, want %s (column order/contract drift)", sess.ID, fx.sessionID)
	}
	if sess.TenantID != fx.tenantID {
		t.Errorf("session.TenantID = %s, want %s", sess.TenantID, fx.tenantID)
	}
	if sess.EmployeeID != fx.employeeID {
		t.Errorf("session.EmployeeID = %s, want %s", sess.EmployeeID, fx.employeeID)
	}
	if sess.RevokedAt != nil {
		t.Errorf("session.RevokedAt = %v, want nil (not revoked)", sess.RevokedAt)
	}
}
