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
	"github.com/atknatk/tappa/test/fixtures"
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
// REDLINE NET (scripts/redline-check.sh, CLAUDE.md section 4): this test issues
// statements that its own production-code rules forbid -- UPDATE/DELETE on the
// immutable transactions table (R3, section 4.3) and use of the migration role
// (R5) -- precisely to prove they are BLOCKED, not to violate them. The scanner
// excludes _test.go from exactly those two rules (see the R3 and R5 migrate-URL
// waivers in redline-check.sh), so the SQL below reads as plain literals while
// real production violations are still caught. The role is proven at RUNTIME
// (assertAppRole / assertOwnerRole): naming a pool "appPool" is not evidence -- a
// test could hand an owner-identity pool that name and pass vacuously.
//
// ISOLATION PROBES ARE RAW (escape-path (a), M0-03 3rd round): the isolation SQL
// is written inline here (not a sqlc store call) and carries NO `tenant_id =`
// predicate. A filtered query would return 0 rows because of the WHERE, not RLS,
// and would pass with RLS OFF. The one place a filtered store query is exercised
// is TestStoreQueryFiltersByTenant, which is explicitly NOT an isolation proof.
//
// NEW TABLE? When a migration adds a tenant-scoped table, add it to the read table
// list in TestRLS_ReadIsolation_AllTables (case 1/7 read) and to the write table
// list in TestRLS_WriteWithCheck_AllTables (case 2/7 write), and -- if it is
// append-only -- to TestRLS_OwnerMutationsHitAppendOnlyTrigger (case 5). Currently
// covered: tenants, locations, departments, employees, sessions, tags, transactions,
// audit_log, transaction_reviews, admin_users, admin_sessions, password_resets,
// policies, policy_versions, policy_attachments, employee_invites, billing_periods.

// ---------------------------------------------------------------- pools -----

// ownerDB opens a pool that connects as tappa_owner via the migration-role URL.
// It is a SUPERUSER and bypasses RLS, so it is used ONLY for case 5 (proving the
// trigger stops a superuser). Reuses appDB/testDB from store_test.go /
// tenant_test.go for the tappa_app pools. Skips with a clear message when the URL
// is absent.
//
// Using the migration role is legitimate here: this is test-only owner access
// needed to prove case 5. Redline R5 forbids the migration role in PRODUCTION code
// (it bypasses RLS) and excludes _test.go, so the env key is a plain literal.
func ownerDB(t *testing.T) *DB {
	t.Helper()
	raw := os.Getenv("DATABASE_MIGRATE_URL")
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
	adminUserID    uuid.UUID // admin_users owner (M1-11 panel identity)
	adminEmail     string    // per-fixture unique (tenant-scoped citext unique)
	adminSessionID uuid.UUID // admin_sessions (separate from employee sessions)
	adminTokenHash string    // admin_sessions.token_hash (never a token, section 4.7)
	resetID        uuid.UUID // password_resets (single-use)
	resetTokenHash string    // password_resets.token_hash (never a token, section 4.7)
	policyID       uuid.UUID // policies (M3-02 policy engine schema)
	policyVersID   uuid.UUID // policy_versions (append-only), version_no = 1
	policyAttachID uuid.UUID // policy_attachments (resource = '*')
	inviteID       uuid.UUID // employee_invites (M5-02), live and unconsumed
	inviteCodeHash string    // employee_invites.code_hash (never a code, section 4.7)
	billingID      uuid.UUID // billing_periods (M6-12), APPEND-ONLY, one frozen month
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
		adminUserID:    uuid.New(),
		adminEmail:     "admin-" + uuid.NewString() + "@iso.example",
		adminSessionID: uuid.New(),
		adminTokenHash: "admin-hash-" + uuid.NewString(),
		resetID:        uuid.New(),
		// 64 random hex chars: the shape internal/adminauth's HMAC produces and the
		// shape 00019's CHECK enforces. It was "reset-hash-<uuid>" until that
		// migration, and it is a pre-image of nothing -- no test holds a reset token.
		resetTokenHash: randTokenHash(t),
		policyID:       uuid.New(),
		policyVersID:   uuid.New(),
		policyAttachID: uuid.New(),
		inviteID:       uuid.New(),
		inviteCodeHash: randCodeHash(t),
		billingID:      uuid.New(),
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
		  VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 0, 'active')`,
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
		// admin_users (M1-11) -- panel identity, separate from employees. password_hash
		// is a SHAPE-VALID placeholder that hashes nothing (fixtures.UnusablePasswordHash);
		// this fixture never authenticates. It used to be 'x', which migration 00018 no
		// longer permits -- the column must hold a digest bcrypt will actually process.
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
		  VALUES ($1, $2, 'iso-admin', $3, $4, 'owner')`,
			[]any{fx.adminUserID, fx.tenantID, fx.adminEmail, fixtures.UnusablePasswordHash}},
		// admin_sessions (M1-11) -- separate from employee sessions; composite FK to
		// admin_users(id, tenant_id) proves the session's admin is same-tenant.
		{`INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash)
		  VALUES ($1, $2, $3, $4)`,
			[]any{fx.adminSessionID, fx.tenantID, fx.adminUserID, fx.adminTokenHash}},
		// password_resets (M1-11) -- single-use; expires_at is NOT NULL.
		{`INSERT INTO password_resets (id, tenant_id, admin_user_id, token_hash, expires_at)
		  VALUES ($1, $2, $3, $4, now() + interval '1 hour')`,
			[]any{fx.resetID, fx.tenantID, fx.adminUserID, fx.resetTokenHash}},
		// policies (M3-02) -- a tenant-layer policy container. depends only on tenants.
		{`INSERT INTO policies (id, tenant_id, name, layer)
		  VALUES ($1, $2, 'iso-policy', 'tenant')`,
			[]any{fx.policyID, fx.tenantID}},
		// policy_versions (M3-02) -- APPEND-ONLY; version_no = 1. Composite FK
		// (policy_id, tenant_id) -> policies proves same-tenant. document is free-form.
		{`INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document)
		  VALUES ($1, $2, $3, 1, '{}'::jsonb)`,
			[]any{fx.policyVersID, fx.tenantID, fx.policyID}},
		// policy_attachments (M3-02) -- binds the policy to resource '*'.
		{`INSERT INTO policy_attachments (id, tenant_id, policy_id, resource)
		  VALUES ($1, $2, $3, '*')`,
			[]any{fx.policyAttachID, fx.tenantID, fx.policyID}},
		// employee_invites (M5-02) -- a live (unconsumed, unexpired) invite for the
		// fixture's employee. Composite FK (employee_id, tenant_id) -> employees
		// proves same-tenant. code_hash is 64 random hex chars: the shape 00009's
		// CHECK demands, and the pre-image of nothing -- the invite CODE exists
		// nowhere, not even in tests (section 4.7).
		{`INSERT INTO employee_invites (id, tenant_id, employee_id, code_hash, expires_at)
		  VALUES ($1, $2, $3, $4, now() + interval '1 hour')`,
			[]any{fx.inviteID, fx.tenantID, fx.employeeID, fx.inviteCodeHash}},
		// billing_periods (M6-12) -- one FROZEN month, APPEND-ONLY like transactions
		// and policy_versions. Composite FK (closed_by, tenant_id) -> admin_users
		// proves the closer is same-tenant. The period is deliberately in the past
		// and the amount is GENERATED, so it is absent from the column list here.
		{`INSERT INTO billing_periods
		    (id, tenant_id, period_month, period_from, period_to, timezone, plan,
		     free_period, employee_count, unit_price, closed_by)
		  VALUES ($1, $2, '2025-01-01', '2024-12-31 23:00+00', '2025-01-31 23:00+00',
		          'Europe/Malta', 'standard', false, 1, 1.50, $3)`,
			[]any{fx.billingID, fx.tenantID, fx.adminUserID}},
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
		{"admin_users", `SELECT count(*) FROM admin_users WHERE id = $1`, a.adminUserID},
		{"admin_sessions", `SELECT count(*) FROM admin_sessions WHERE id = $1`, a.adminSessionID},
		{"password_resets", `SELECT count(*) FROM password_resets WHERE id = $1`, a.resetID},
		{"policies", `SELECT count(*) FROM policies WHERE id = $1`, a.policyID},
		{"policy_versions", `SELECT count(*) FROM policy_versions WHERE id = $1`, a.policyVersID},
		{"policy_attachments", `SELECT count(*) FROM policy_attachments WHERE id = $1`, a.policyAttachID},
		{"employee_invites", `SELECT count(*) FROM employee_invites WHERE id = $1`, a.inviteID},
		{"billing_periods", `SELECT count(*) FROM billing_periods WHERE id = $1`, a.billingID},
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
				_, e := tx.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'))`, randUID(t), a.tenantID, a.locationID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'))`, randUID(t), b.tenantID, b.locationID)
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
		// THE DIGEST MUST BE ONE MIGRATION 00018 ACCEPTS, AND THE ARM THAT ENFORCES
		// THAT IS THE POSITIVE CONTROL -- measured, because the obvious guess is wrong.
		//
		// The guess is that a rejected digest would make the FORGE arm fail with 23514
		// instead of 42501 and be caught by assertWriteBlocked. It would not: Postgres
		// evaluates the RLS WITH CHECK BEFORE the table's CHECK constraints, so a row
		// that is wrong in BOTH ways reports only the RLS violation (measured directly:
		// cross-tenant tenant_id + password_hash 'x' -> "new row violates row-level
		// security policy", never the check constraint). Mutation-tested: putting 'x'
		// back in the forge arm alone leaves this test GREEN.
		//
		// It is the SECOND closure -- B writing its OWN row -- that fails loudly, with
		// "positive control: B could not insert its OWN admin_users row ... 23514". So
		// the fixture is pinned by the arm that is supposed to SUCCEED, which is exactly
		// what a positive control is for.
		{"admin_users", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Forge', $3, $4, 'manager')`, uuid.New(), a.tenantID, "forge-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Own', $3, $4, 'manager')`, uuid.New(), b.tenantID, "own-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash)
				return e
			}},
		{"admin_sessions", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				// tenant_id = A (fails WITH CHECK under B); FK to A's admin user resolves
				// because FK checks bypass RLS, so only WITH CHECK stands in the way.
				_, e := tx.Exec(ctx, `INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`, uuid.New(), a.tenantID, a.adminUserID, "forge-"+uuid.NewString())
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`, uuid.New(), b.tenantID, b.adminUserID, "own-"+uuid.NewString())
				return e
			}},
		// 🔴 BOTH ARMS CARRY A HASH-SHAPED token_hash SINCE 00019, and that is what
		// keeps this case measuring RLS. The values used to be "forge-<uuid>" /
		// "own-<uuid>"; migration 00019 added a CHECK requiring ^[0-9a-f]{64}$, and a
		// negative arm that trips a CHECK proves nothing about row-level security --
		// it would go green with RLS switched off.
		{"password_resets", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO password_resets (id, tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4, now() + interval '1 hour')`, uuid.New(), a.tenantID, a.adminUserID, randTokenHash(t))
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO password_resets (id, tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4, now() + interval '1 hour')`, uuid.New(), b.tenantID, b.adminUserID, randTokenHash(t))
				return e
			}},
		{"policies", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO policies (id, tenant_id, name, layer) VALUES ($1, $2, 'forge', 'tenant')`, uuid.New(), a.tenantID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO policies (id, tenant_id, name, layer) VALUES ($1, $2, 'own', 'tenant')`, uuid.New(), b.tenantID)
				return e
			}},
		{"policy_versions", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				// tenant_id = A (fails WITH CHECK under B); composite FK to A's policy
				// resolves (FK bypasses RLS). version_no = 99 avoids any unique clash so
				// the ONLY failure is WITH CHECK (RLS evaluates before the unique index).
				_, e := tx.Exec(ctx, `INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document) VALUES ($1, $2, $3, 99, '{}'::jsonb)`, uuid.New(), a.tenantID, a.policyID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				// B's OWN policy; version_no = 2 (fixture already inserted version 1).
				_, e := tx.Exec(ctx, `INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document) VALUES ($1, $2, $3, 2, '{}'::jsonb)`, uuid.New(), b.tenantID, b.policyID)
				return e
			}},
		{"policy_attachments", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				// resource differs from the fixture's '*' so no unique clash masks WITH CHECK.
				_, e := tx.Exec(ctx, `INSERT INTO policy_attachments (id, tenant_id, policy_id, resource) VALUES ($1, $2, $3, $4)`, uuid.New(), a.tenantID, a.policyID, "location/"+uuid.NewString())
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO policy_attachments (id, tenant_id, policy_id, resource) VALUES ($1, $2, $3, $4)`, uuid.New(), b.tenantID, b.policyID, "location/"+uuid.NewString())
				return e
			}},
		{"employee_invites", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				// tenant_id = A (fails WITH CHECK under B); the composite FK to A's
				// employee resolves because FK checks bypass RLS, so only WITH CHECK
				// stands in the way. A fresh code_hash avoids any unique clash masking it.
				_, e := tx.Exec(ctx, `INSERT INTO employee_invites (id, tenant_id, employee_id, code_hash, expires_at) VALUES ($1, $2, $3, $4, now() + interval '1 hour')`, uuid.New(), a.tenantID, a.employeeID, randCodeHash(t))
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO employee_invites (id, tenant_id, employee_id, code_hash, expires_at) VALUES ($1, $2, $3, $4, now() + interval '1 hour')`, uuid.New(), b.tenantID, b.employeeID, randCodeHash(t))
				return e
			}},
		{"billing_periods", blockRLS,
			func(ctx context.Context, tx pgx.Tx) error {
				// tenant_id = A (fails WITH CHECK under B); the composite FK to A's
				// admin resolves because FK checks bypass RLS, so only WITH CHECK
				// stands in the way. period_month differs from the fixture's so the
				// (tenant_id, period_month) UNIQUE cannot mask it -- RLS is evaluated
				// before the unique index, but a clash would make the reason ambiguous.
				_, e := tx.Exec(ctx, `INSERT INTO billing_periods (id, tenant_id, period_month, period_from, period_to, timezone, plan, free_period, employee_count, unit_price, closed_by) VALUES ($1, $2, '2025-02-01', '2025-01-31 23:00+00', '2025-02-28 23:00+00', 'Europe/Malta', 'standard', false, 999, 0.01, $3)`, uuid.New(), a.tenantID, a.adminUserID)
				return e
			},
			func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `INSERT INTO billing_periods (id, tenant_id, period_month, period_from, period_to, timezone, plan, free_period, employee_count, unit_price, closed_by) VALUES ($1, $2, '2025-02-01', '2025-01-31 23:00+00', '2025-02-28 23:00+00', 'Europe/Malta', 'standard', false, 1, 1.50, $3)`, uuid.New(), b.tenantID, b.adminUserID)
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
// Composite same-tenant FK (M1-11): admin_sessions(admin_user_id, tenant_id) ->
// admin_users(id, tenant_id) structurally forbids linking a session to an admin of
// ANOTHER tenant. This is a DIFFERENT guarantee from WITH CHECK: here tenant_id = B
// matches the context (so WITH CHECK passes), but admin_user_id points at A's admin,
// for which no (A's id, B) row exists -> a foreign_key_violation. The positive
// control (B's OWN admin, checked last) proves the FK fails ONLY on the cross-tenant
// link, not on some unrelated cause.
// ============================================================================

func TestRLS_AdminSession_CompositeFKBlocksCrossTenant(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app) // foreign tenant (owns the admin we try to borrow)
	b := buildFixture(t, app) // own tenant (the context we insert under)

	// Negative: B's context, tenant_id = B (passes WITH CHECK), admin_user_id = A's.
	// The composite FK finds no (A's admin id, B) row -> foreign_key_violation.
	errX := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`,
			uuid.New(), b.tenantID, a.adminUserID, "xfk-"+uuid.NewString())
		return e
	})
	if errX == nil {
		t.Fatalf("cross-tenant admin_session link succeeded, want composite-FK rejection")
	}
	if pg := asPgErr(errX); pg == nil || pg.Code != "23503" {
		t.Fatalf("want foreign_key_violation (23503) for cross-tenant admin link, got %v", errX)
	}

	// Positive control (same-tenant, checked last): B linking to its OWN admin user
	// must SUCCEED -- otherwise the rejection above could be for an unrelated reason.
	if errOwn := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`,
			uuid.New(), b.tenantID, b.adminUserID, "own-"+uuid.NewString())
		return e
	}); errOwn != nil {
		t.Fatalf("positive control: B could not link a session to its OWN admin: %v", errOwn)
	}
}

// ============================================================================
// Composite same-tenant FK (M3-02): policy_versions(policy_id, tenant_id) ->
// policies(id, tenant_id) structurally forbids a version pointing at a policy of
// ANOTHER tenant. Distinct from WITH CHECK: here tenant_id = A matches the context
// (WITH CHECK passes), but policy_id points at B's policy, for which no (B's id, A)
// row exists -> foreign_key_violation. Same guarantee applies to policy_attachments
// (identical composite FK), proven structurally identical. The positive control
// (A's OWN policy, checked last) proves the FK fails ONLY on the cross-tenant link.
// ============================================================================

func TestRLS_PolicyVersion_CompositeFKBlocksCrossTenant(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app) // the context we insert under
	b := buildFixture(t, app) // owns the policy we try to borrow

	// Negative: A's context, tenant_id = A (passes WITH CHECK), policy_id = B's.
	// The composite FK finds no (B's policy id, A) row -> foreign_key_violation.
	errX := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document) VALUES ($1, $2, $3, 5, '{}'::jsonb)`,
			uuid.New(), a.tenantID, b.policyID)
		return e
	})
	if errX == nil {
		t.Fatalf("cross-tenant policy_version link succeeded, want composite-FK rejection")
	}
	if pg := asPgErr(errX); pg == nil || pg.Code != "23503" {
		t.Fatalf("want foreign_key_violation (23503) for cross-tenant policy link, got %v", errX)
	}

	// Positive control (same-tenant, checked last): A adding a version to its OWN
	// policy must SUCCEED (version_no 2; fixture inserted 1) -- otherwise the
	// rejection above could be for an unrelated reason.
	if errOwn := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document) VALUES ($1, $2, $3, 2, '{}'::jsonb)`,
			uuid.New(), a.tenantID, a.policyID)
		return e
	}); errOwn != nil {
		t.Fatalf("positive control: A could not add a version to its OWN policy: %v", errOwn)
	}
}

// ============================================================================
// Composite same-tenant FK (M5-02): employee_invites(employee_id, tenant_id) ->
// employees(id, tenant_id) structurally forbids an invite that activates ANOTHER
// tenant's employee -- the worst shape this table could take, since consuming such
// an invite would hand a cross-tenant identity a session. Distinct from WITH CHECK:
// here tenant_id = B matches the context (WITH CHECK passes), but employee_id points
// at A's employee, for which no (A's id, B) row exists -> foreign_key_violation. The
// positive control (B's OWN employee, checked last) proves the FK fails ONLY on the
// cross-tenant link.
// ============================================================================

func TestRLS_EmployeeInvite_CompositeFKBlocksCrossTenant(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app) // owns the employee we try to borrow
	b := buildFixture(t, app) // the context we insert under

	errX := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employee_invites (id, tenant_id, employee_id, code_hash, expires_at) VALUES ($1, $2, $3, $4, now() + interval '1 hour')`,
			uuid.New(), b.tenantID, a.employeeID, randCodeHash(t))
		return e
	})
	if errX == nil {
		t.Fatalf("cross-tenant invite link succeeded, want composite-FK rejection")
	}
	if pg := asPgErr(errX); pg == nil || pg.Code != "23503" {
		t.Fatalf("want foreign_key_violation (23503) for a cross-tenant invite link, got %v", errX)
	}

	// Positive control (same-tenant, checked last): B inviting its OWN employee
	// must SUCCEED -- otherwise the rejection above could be for another reason.
	if errOwn := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employee_invites (id, tenant_id, employee_id, code_hash, expires_at) VALUES ($1, $2, $3, $4, now() + interval '1 hour')`,
			uuid.New(), b.tenantID, b.employeeID, randCodeHash(t))
		return e
	}); errOwn != nil {
		t.Fatalf("positive control: B could not invite its OWN employee: %v", errOwn)
	}
}

// ============================================================================
// M3-07 (migration 0008): transactions.{policy_version_id, matched_sid,
// policy_layer, policy_context} bind the policy DECISION to the immutable record.
// These tests prove the four new columns (a) are subject to RLS like every other
// column of the row, (b) are structurally same-tenant via the composite FK, and
// (c) obey the decision-consistency CHECK. Immutability of the new columns is
// proven in Case 4 (belt 1, per-column app UPDATE denied) and Case 5 (belt 2,
// superuser trigger on an M3-07 column).
// ============================================================================

// insertPolicyTx inserts, in fx's OWN tenant context, a flag transaction carrying
// the M3-07 policy-decision columns and citing fx's own append-only policy version
// (fx.policyVersID) -- FK-valid, and CHECK branch (c): baseline layer + version +
// sid. Returns the new transaction id. Like every fixture row it is append-only and
// stays inert (see the buildFixture teardown note).
func insertPolicyTx(t *testing.T, d *DB, fx fixture) uuid.UUID {
	t.Helper()
	txID := uuid.New()
	if err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions
			   (id, tenant_id, employee_id, location_id, occurred_at, verdict, channel,
			    policy_layer, matched_sid, policy_version_id, policy_context)
			 VALUES ($1, $2, $3, $4, now(), 'flag', 'nfc',
			    'baseline', 'base:no-evidence-review', $5, '{"tap:gpsDistanceM":842}'::jsonb)`,
			txID, fx.tenantID, fx.employeeID, fx.locationID, fx.policyVersID)
		return e
	}); err != nil {
		t.Fatalf("insertPolicyTx: %v", err)
	}
	return txID
}

// RLS is row-level, so a row invisible cross-tenant hides ALL its columns; this
// proves the M3-07 columns are covered by the SAME transactions policy (no new RLS
// statement was needed for the added columns). A policy-POPULATED row is used so the
// probe is over a row that actually exercises the new columns.
func TestRLS_Transaction_PolicyColumns_Isolation(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app)
	b := buildFixture(t, app)

	txID := insertPolicyTx(t, app, a) // A's policy-populated row

	const query = `SELECT count(*) FROM transactions WHERE id = $1`
	// Positive control: A sees its own policy-populated row (else the 0 below is vacuous).
	if self := rawCountCtx(t, app, a.tenantID, query, txID); self != 1 {
		t.Fatalf("positive control: A sees %d of its own policy-populated tx, want 1", self)
	}
	// Isolation: B cannot see A's row -> the new columns are hidden with it.
	if cross := rawCountCtx(t, app, b.tenantID, query, txID); cross != 0 {
		t.Fatalf("RLS FAILED: B read %d of A's policy-populated tx, want 0", cross)
	}
}

// Composite same-tenant FK (M3-07): transactions(policy_version_id, tenant_id) ->
// policy_versions(id, tenant_id) forbids a record citing a policy version of ANOTHER
// tenant. Distinct from WITH CHECK: here tenant_id = A matches the context (WITH
// CHECK passes) and the consistency CHECK passes (baseline + version + sid), but
// policy_version_id points at B's version, for which no (B's id, A) row exists ->
// foreign_key_violation. The positive control (A's OWN version, checked last) proves
// the FK fails ONLY on the cross-tenant link.
func TestRLS_Transaction_PolicyVersionFK_BlocksCrossTenant(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := buildFixture(t, app) // the context we insert under
	b := buildFixture(t, app) // owns the version we try to borrow

	// Negative: A's context, tenant_id = A, policy_version_id = B's version.
	errX := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions
			   (id, tenant_id, employee_id, occurred_at, verdict, channel,
			    policy_layer, matched_sid, policy_version_id)
			 VALUES ($1, $2, $3, now(), 'flag', 'nfc', 'baseline', 'base:x', $4)`,
			uuid.New(), a.tenantID, a.employeeID, b.policyVersID)
		return e
	})
	if errX == nil {
		t.Fatalf("cross-tenant policy_version_id on a transaction succeeded, want composite-FK rejection")
	}
	if pg := asPgErr(errX); pg == nil || pg.Code != "23503" {
		t.Fatalf("want foreign_key_violation (23503) for cross-tenant policy_version_id, got %v", errX)
	}

	// Positive control (same-tenant, checked last): A citing its OWN version succeeds.
	if errOwn := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions
			   (id, tenant_id, employee_id, occurred_at, verdict, channel,
			    policy_layer, matched_sid, policy_version_id)
			 VALUES ($1, $2, $3, now(), 'flag', 'nfc', 'baseline', 'base:x', $4)`,
			uuid.New(), a.tenantID, a.employeeID, a.policyVersID)
		return e
	}); errOwn != nil {
		t.Fatalf("positive control: A could not cite its OWN policy version: %v", errOwn)
	}
}

// The decision-consistency CHECK (transactions_policy_decision_consistent) mirrors
// EXACTLY what policy.Evaluate can emit, so every legitimate decision inserts and
// only an inconsistent (buggy) combination is rejected -- the same bargain as the
// verdict CHECK (migration 0005). It also GUARANTEES the report invariant M3-07 is
// about: "policy_version_id IS NULL <=> a guardrail/default decided" and "every
// decided row names a sid". Accept: the shapes the engine returns; reject: three
// inconsistent shapes, each a check_violation (23514).
func TestTransactions_PolicyDecisionConsistencyCheck(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := buildFixture(t, app)

	// exec runs one INSERT in fx's context and returns the error (nil on success).
	exec := func(cols, vals string, args ...any) error {
		return app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx,
				`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, `+cols+`)
				 VALUES ($1, $2, now(), 'flag', 'nfc', `+vals+`)`,
				append([]any{fx.tenantID, fx.employeeID}, args...)...)
			return e
		})
	}

	// ACCEPT: the shapes policy.Evaluate actually produces.
	accepts := []struct {
		name string
		cols string
		vals string
		args []any
	}{
		{"all-absent (pre-M5 / fixture shape)", "", "", nil},
		{"guardrail decision (sys: sid, no version)",
			"policy_layer, matched_sid", "'guardrail', 'sys:sun-invalid'", nil},
		{"default decision (\"default\" sid, no version)",
			"policy_layer, matched_sid", "'guardrail', 'default'", nil},
		{"baseline decision (version + sid)",
			"policy_layer, matched_sid, policy_version_id", "'baseline', 'base:no-evidence-review', $3", []any{fx.policyVersID}},
	}
	for _, tc := range accepts {
		if tc.cols == "" {
			// all-absent: no extra columns.
			if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx,
					`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel)
					 VALUES ($1, $2, now(), 'flag', 'nfc')`, fx.tenantID, fx.employeeID)
				return e
			}); err != nil {
				t.Fatalf("accept %q: unexpected error: %v", tc.name, err)
			}
			continue
		}
		if err := exec(tc.cols, tc.vals, tc.args...); err != nil {
			t.Fatalf("accept %q: unexpected error: %v", tc.name, err)
		}
	}

	// REJECT: inconsistent shapes the engine can never produce -> 23514.
	rejects := []struct {
		name string
		cols string
		vals string
		args []any
	}{
		{"guardrail layer WITH a version",
			"policy_layer, matched_sid, policy_version_id", "'guardrail', 'sys:x', $3", []any{fx.policyVersID}},
		{"unknown layer value",
			"policy_layer, matched_sid", "'bogus', 'base:x'", nil},
		{"baseline decision WITHOUT a sid",
			"policy_layer, policy_version_id", "'baseline', $3", []any{fx.policyVersID}},
	}
	for _, tc := range rejects {
		err := exec(tc.cols, tc.vals, tc.args...)
		if err == nil {
			t.Fatalf("reject %q: insert succeeded, want check_violation", tc.name)
		}
		if pg := asPgErr(err); pg == nil || pg.Code != "23514" {
			t.Fatalf("reject %q: want check_violation (23514), got %v", tc.name, err)
		}
	}
}

// ============================================================================
// layer='guardrail' is STRUCTURALLY unwritable (M3-02): guardrails are compiled
// into internal/policy (CLAUDE.md section 5 lines 1-5, sys:*), never stored -- a
// stored guardrail would let a single SQL write disable a section 4 red line. The
// CHECK (layer IN ('baseline','tenant')) is that structural block. Positive control:
// both allowed layers insert; negative: 'guardrail' is a check_violation (23514).
// ============================================================================

func TestPolicies_LayerCheckRejectsGuardrail(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := buildFixture(t, app)

	// Positive control: the two allowed layers are accepted.
	for _, layer := range []string{"baseline", "tenant"} {
		if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO policies (id, tenant_id, name, layer) VALUES ($1, $2, 'ok', $3)`, uuid.New(), fx.tenantID, layer)
			return e
		}); err != nil {
			t.Fatalf("positive control: layer=%q was rejected: %v", layer, err)
		}
	}

	// Negative: 'guardrail' cannot be spelled into a row.
	err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO policies (id, tenant_id, name, layer) VALUES ($1, $2, 'evil', 'guardrail')`, uuid.New(), fx.tenantID)
		return e
	})
	if err == nil {
		t.Fatalf("layer='guardrail' was accepted; the CHECK must reject it (guardrails are code, not data)")
	}
	if pg := asPgErr(err); pg == nil || pg.Code != "23514" {
		t.Fatalf("want check_violation (23514) for layer='guardrail', got %v", err)
	}
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

	// These are the real mutation statements at runtime; the test issues them and
	// asserts the REVOKE denies them (redline R3 excludes _test.go, so plain SQL).
	errU := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE transactions SET note = 'x' WHERE id = $1", fx.txReviewedID)
		return e
	})
	assertPermissionDenied(t, "app UPDATE on transactions", errU)

	errD := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "DELETE FROM transactions WHERE id = $1", fx.txReviewedID)
		return e
	})
	assertPermissionDenied(t, "app DELETE on transactions", errD)

	// M3-07 (migration 0008): the new policy-decision columns inherit immutability
	// WITHOUT any new statement -- the table-level REVOKE UPDATE covers ALL columns
	// (there is no column-level UPDATE grant), so an UPDATE of a NEW column is denied
	// exactly like `note` above (belt 1). Proven per-column so a future column-level
	// GRANT regression would fail here.
	errNew := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE transactions SET matched_sid = 'x' WHERE id = $1", fx.txReviewedID)
		return e
	})
	assertPermissionDenied(t, "app UPDATE on transactions.matched_sid (M3-07 column)", errNew)
}

// ============================================================================
// Case 4 (policy_versions, M3-02): tappa_app cannot UPDATE or DELETE an append-only
// policy version -- the REVOKE (belt 1) denies it at the privilege check, BEFORE the
// trigger. policies stays UPDATE-able (the enabled toggle), proven as a control.
// ============================================================================

func TestRLS_AppCannotMutatePolicyVersions(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := buildFixture(t, app)

	// Positive control: app CAN SELECT its own policy version, so the failures below
	// are the missing UPDATE/DELETE privilege, not invisibility.
	if got := rawCountCtx(t, app, fx.tenantID, `SELECT count(*) FROM policy_versions WHERE id = $1`, fx.policyVersID); got != 1 {
		t.Fatalf("positive control: app cannot SELECT its own policy version (got %d rows)", got)
	}

	errU := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE policy_versions SET version_no = 2 WHERE id = $1", fx.policyVersID)
		return e
	})
	assertPermissionDenied(t, "app UPDATE on policy_versions", errU)

	errD := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "DELETE FROM policy_versions WHERE id = $1", fx.policyVersID)
		return e
	})
	assertPermissionDenied(t, "app DELETE on policy_versions", errD)

	// Control: policies IS mutable (enabled toggle) -- UPDATE must succeed, proving
	// the REVOKE above is targeted at the append-only table, not blanket.
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE policies SET enabled = false WHERE id = $1", fx.policyID)
		return e
	}); err != nil {
		t.Fatalf("policies must stay UPDATE-able (enabled toggle), got: %v", err)
	}
}

// ============================================================================
// Case 5: the append-only trigger stops mutation of transactions / audit_log /
// transaction_reviews / policy_versions even for the SUPERUSER owner (RLS + REVOKE
// cannot -- a superuser bypasses both, so this is the belt on top of those braces,
// section 4.3). policy_versions reuses the SAME tappa_forbid_mutation() trigger fn.
// ============================================================================

func TestRLS_OwnerMutationsHitAppendOnlyTrigger(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner) // runtime proof: tappa_owner is a superuser

	fx := buildFixture(t, app) // create the rows as tappa_app (committed)

	// Table-driven over the append-only tables, so the mutation SQL is assembled per
	// case from table/setExpr. setExpr (not just a column name) because policy_versions
	// has no free-text column: 'x' cast to its jsonb/int/uuid columns would raise
	// BEFORE the trigger fires and mask the append-only proof. The assembled SQL is
	// the real mutation at runtime and is asserted to be blocked by the trigger.
	// `label` is a distinct subtest name so two cases can target the SAME real table
	// (transactions, once on a pre-existing column and once on an M3-07 column).
	targets := []struct {
		label   string
		table   string
		setExpr string
		id      any
	}{
		{"transactions", "transactions", "note = 'x'", fx.txReviewedID},
		// M3-07 (migration 0008): the trigger is a BEFORE UPDATE OR DELETE FOR EACH
		// ROW guard with no column condition, so it fires on an update of a NEW column
		// exactly as for `note`. Proven for a new column so belt 2 (the superuser path)
		// is shown to cover the M3-07 columns too, not just the pre-existing ones.
		{"transactions_policy_column", "transactions", "matched_sid = 'x'", fx.txReviewedID},
		{"audit_log", "audit_log", "action = 'x'", fx.auditID},
		{"transaction_reviews", "transaction_reviews", "note = 'x'", fx.reviewID},
		{"policy_versions", "policy_versions", "version_no = 2", fx.policyVersID},
		// M6-12 (migration 0016): a frozen invoice line that can be edited is not
		// frozen, so billing_periods joins the same family. employee_count rather
		// than a text column because the row has no free-text one, and because
		// changing the count is exactly the mutation that would change a bill.
		{"billing_periods", "billing_periods", "employee_count = 999", fx.billingID},
	}

	for _, tc := range targets {
		t.Run(tc.label, func(t *testing.T) {
			// Positive control: the owner (bypasses RLS) SEES the row, so the
			// FOR EACH ROW trigger has a row to fire on -- the failure below is the
			// trigger, not an empty match (a WHERE that matches nothing would be
			// a silent success).
			var before int
			if err := owner.pool.QueryRow(context.Background(),
				"SELECT count(*) FROM "+tc.table+" WHERE id = $1", tc.id).Scan(&before); err != nil {
				t.Fatalf("owner count %s: %v", tc.table, err)
			}
			if before != 1 {
				t.Fatalf("owner sees %d %s rows, want 1 (positive control for the trigger)", before, tc.table)
			}

			updateStmt := "UPDATE " + tc.table + " SET " + tc.setExpr + " WHERE id = $1"
			deleteStmt := "DELETE FROM " + tc.table + " WHERE id = $1"

			_, errU := owner.pool.Exec(context.Background(), updateStmt, tc.id)
			assertAppendOnlyTrigger(t, "owner UPDATE "+tc.label, errU)

			_, errD := owner.pool.Exec(context.Background(), deleteStmt, tc.id)
			assertAppendOnlyTrigger(t, "owner DELETE "+tc.label, errD)

			// Still present: the blocked mutations changed nothing.
			var after int
			if err := owner.pool.QueryRow(context.Background(),
				`SELECT count(*) FROM `+tc.table+` WHERE id = $1`, tc.id).Scan(&after); err != nil {
				t.Fatalf("owner re-count %s: %v", tc.table, err)
			}
			if after != 1 {
				t.Fatalf("%s row count after blocked mutations = %d, want 1 (still present, unchanged)", tc.table, after)
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
	// LocationID is a POINTER since M6-06 phase B (00013 made the column nullable
	// for the inventory model), so "mounted here" is asserted as non-nil AND equal —
	// a nil that compared equal to nothing would pass a `!=` check silently.
	if tag.LocationID == nil || *tag.LocationID != fx.locationID {
		t.Errorf("tag.LocationID = %v, want %s", tag.LocationID, fx.locationID)
	}
	if tag.Status != "active" {
		t.Errorf("tag.Status = %q, want active", tag.Status)
	}
	if tag.LastCtr != 0 {
		t.Errorf("tag.LastCtr = %d, want 0", tag.LastCtr)
	}
	// aes_key_ref was inserted as decode(repeat('dead', 22), 'hex') -- 44 bytes of
	// alternating DE AD. The LENGTH is asserted first and separately because it is
	// the schema rule migration 00021 added (tags_aes_key_ref_is_kek_envelope, ADR
	// 0003 article 4); the bytes are asserted after it because this test's job is
	// to prove the resolver returns the column it was handed, byte for byte, and a
	// length-only check would pass on any 44-byte value.
	if len(tag.AESKeyRef) != 44 {
		t.Errorf("len(tag.AESKeyRef) = %d, want 44 (nonce 12 || ciphertext 16 || tag 16)", len(tag.AESKeyRef))
	}
	for i, b := range tag.AESKeyRef {
		want := byte(0xDE)
		if i%2 == 1 {
			want = 0xAD
		}
		if b != want {
			t.Errorf("tag.AESKeyRef[%d] = %02X, want %02X (fixture is DE AD repeated)", i, b, want)
			break
		}
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

// TestPolicyTables_KeepTheirRLSAndPrivilegesAfterTheNameIndex is the assertion
// migration 00015's header CLAIMED existed and did not.
//
// 🔴 THE CLAIM WAS THE DEFECT, NOT THE FACT. 00015 adds UNIQUE (tenant_id, layer,
// name) on `policies` and its header says the change touches no GRANT, no policy and
// no RLS setting -- "asserted mechanically by internal/db/rls_test.go, which reads
// relrowsecurity/relforcerowsecurity and the privilege bits rather than trusting this
// paragraph". An audit measured that this file made no such assertion for ANY of the
// three policy tables: the repository does it for locations, tags, employee_invites
// and audit_log, and the policy tables were simply missing. The fact was true; the
// guarantee was not, so the NEXT migration that broke a grant here would have hit
// nothing.
//
// WHAT IT PINS, per table, is exactly what 00007 installed and what 00015 must not
// have moved:
//
//	policies            SELECT, INSERT, UPDATE   and NO DELETE  (section 4.6: disable, never delete)
//	policy_versions     SELECT, INSERT           and neither UPDATE nor DELETE (section 4.3: append-only)
//	policy_attachments  all four                                 (a binding carries no decision history)
//
// plus ENABLE + FORCE ROW LEVEL SECURITY and exactly one policy on each -- the "RLS
// five" section 6 requires, read from the catalog rather than from the migration text.
//
// ⚠️ THE CATALOG ANSWER IS NOT ACCEPTED ALONE for the one bit that matters most:
// `policies` must actually REFUSE a delete, so the privilege is exercised as well
// (M1-04's lesson, the same reason TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy
// exercises its grant).
func TestPolicyTables_KeepTheirRLSAndPrivilegesAfterTheNameIndex(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)

	for _, tc := range []struct {
		table                  string
		wantSelect, wantInsert bool
		wantUpdate, wantDelete bool
		why                    string
	}{
		{"policies", true, true, true, false,
			"section 4.6: a policy is switched off, never deleted -- its version history is evidence"},
		{"policy_versions", true, true, false, false,
			"section 4.3: append-only, because every transaction pins the version that decided it"},
		{"policy_attachments", true, true, true, true,
			"a binding records where a rule applies today and carries no decision history"},
	} {
		t.Run(tc.table, func(t *testing.T) {
			var sel, ins, upd, del, rowSec, forceSec bool
			var policies int
			if err := d.pool.QueryRow(context.Background(), `
				SELECT has_table_privilege('tappa_app', $1, 'SELECT'),
				       has_table_privilege('tappa_app', $1, 'INSERT'),
				       has_table_privilege('tappa_app', $1, 'UPDATE'),
				       has_table_privilege('tappa_app', $1, 'DELETE'),
				       (SELECT relrowsecurity    FROM pg_class WHERE oid = $1::regclass),
				       (SELECT relforcerowsecurity FROM pg_class WHERE oid = $1::regclass),
				       (SELECT count(*) FROM pg_policies WHERE tablename = $1)`,
				tc.table,
			).Scan(&sel, &ins, &upd, &del, &rowSec, &forceSec, &policies); err != nil {
				t.Fatalf("read catalog for %s: %v", tc.table, err)
			}
			if sel != tc.wantSelect || ins != tc.wantInsert || upd != tc.wantUpdate || del != tc.wantDelete {
				t.Errorf("tappa_app on %s: select=%v insert=%v update=%v delete=%v, want %v/%v/%v/%v.\n%s",
					tc.table, sel, ins, upd, del,
					tc.wantSelect, tc.wantInsert, tc.wantUpdate, tc.wantDelete, tc.why)
			}
			if !rowSec || !forceSec || policies != 1 {
				t.Errorf("%s RLS state: rowsecurity=%v force=%v policies=%d, want true/true/1 "+
					"(00007 installed them; no later migration may disturb them)",
					tc.table, rowSec, forceSec, policies)
			}
		})
	}

	// AND THE ONE THAT IS EXERCISED RATHER THAN READ: `policies` must refuse a DELETE
	// even for a row this tenant owns. A catalog bit that says "no privilege" and a
	// statement that succeeds anyway is the M1-04 shape.
	// ⚠️ THE PROBE ROW CANNOT BE CLEANED UP, WHICH IS THE POINT AND ALSO THE COST. It is
	// written into buildFixture's THROWAWAY tenant (a fresh uuid per run) rather than
	// into a seeded business, so it does not accumulate anywhere a person looks — but it
	// does accumulate: `policies` GRANTs no DELETE, so every run of this test leaves one
	// more row in one more fixture tenant, exactly like every other DB test in this
	// repository leaves its fixtures. Counted rather than solved; `make db-reset` is the
	// only thing that clears it.
	fx := buildFixture(t, d)
	var id uuid.UUID
	if err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`INSERT INTO policies (tenant_id, name, layer) VALUES ($1, $2, 'tenant') RETURNING id`,
			fx.tenantID, "delete-probe-"+uuid.NewString()).Scan(&id)
	}); err != nil {
		t.Fatalf("seeding a policy to try deleting: %v", err)
	}
	err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM policies WHERE tenant_id = $1 AND id = $2`, fx.tenantID, id)
		return e
	})
	// 🔴 THE REFUSAL MUST BE THE PRIVILEGE ONE, NOT ANY ERROR. A probe that accepts
	// "something went wrong" would pass on a typo, a dead connection or a constraint
	// that happens to fire first — and would go on passing the day the GRANT came back.
	// 42501 is insufficient_privilege, which is what REVOKE DELETE produces.
	switch {
	case err == nil:
		t.Error("tappa_app DELETED a policy row. 00007 revokes DELETE precisely so a rule is " +
			"switched off rather than removed, and M6-09 phase B's duplicate-name guarantee " +
			"rests on a duplicate being unremovable rather than merely discouraged.")
	default:
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Errorf("the DELETE was refused with %v, want SQLSTATE 42501 "+
				"(insufficient_privilege). Accepting any error would let this pass for the "+
				"wrong reason — including on the day the privilege comes back and something "+
				"else fails first.", err)
		}
	}
}
