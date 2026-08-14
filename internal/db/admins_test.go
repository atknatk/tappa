package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// admins_test.go -- the M6-01 (phase A) data-layer proofs, against a REAL Postgres
// (CLAUDE.md section 8 / Q04). Migration 00011 adds no table; it adds two
// context-less resolvers, a column-scoped UPDATE grant and a monotonicity trigger,
// so every claim below is measured in the live catalog or with a real statement:
//
//   - resolve_admin_by_email meets the ADR 0002 madde 7 constraints, with (ii)
//     replaced by its measured bound: at most one row PER TENANT, never two rows of
//     the same tenant, zero for an unknown address.
//   - That bound is exercised at 1, 2 and 3 tenants -- the tenant-picker case.
//   - The lookup is CASE-INSENSITIVE, with a permanent NEGATIVE CONTROL showing the
//     same body without the schema-qualified citext operator silently becomes case
//     SENSITIVE under the pinned search_path.
//   - resolve_admin_session_by_token_hash is the employee resolver's twin, and the
//     two identity graphs do not overlap: an employee token does not resolve as an
//     admin session and vice versa (M1-11's claim, tested rather than asserted).
//   - revoked_at is MONOTONIC in the database now (trigger), not by file
//     discipline -- and the un-revoke that state.md records as possible for
//     sessions.revoked_at is refused here for tappa_app AND for the superuser.
//   - tappa_app can no longer rewrite admin_sessions.token_hash or admin_user_id:
//     both were privilege-legal before 00011 (measured) and are permission denied
//     after.
//   - A disabled admin gets no session and passes no request, enforced in SQL by
//     CreateAdminSession / TouchAdminSession rather than by a handler remembering.
//
// Cross-tenant invisibility for admin_users/admin_sessions is proven in
// rls_test.go's read/write isolation tables (both are listed there); this file adds
// the isolation proof over the SURFACE 00011 introduces -- rows written through the
// new queries, probed WITHOUT a tenant_id predicate so a 0 can only come from RLS.
//
// No test here holds a password or a token: password_hash fixtures are literal
// non-secret placeholders that are never verified, and token hashes are random
// strings that are pre-images of nothing (section 4.7).
//
// Fixtures are NOT cleaned up, for the reason store_test.go/rls_test.go give:
// tappa_app has REVOKE DELETE on both admin tables, so the impossibility of
// teardown IS the audit guarantee.

// ---------------------------------------------------------------- fixtures --

// newAdmin inserts a committed admin_users row in its own tenant context. There is
// deliberately no CreateAdminUser query yet (panel-side admin management is M6-05),
// so the fixture writes the row directly.
func newAdmin(t *testing.T, d *DB, tenantID uuid.UUID, email, status, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'Admin Test', $3, $4, $5, $6)`,
			id, tenantID, email, fixtures.UnusablePasswordHash, role, status)
		return e
	}); err != nil {
		t.Fatalf("newAdmin(%s/%s): %v", status, role, err)
	}
	return id
}

// randAdminEmail returns a fresh address. Per-test uniqueness matters: the lookup
// under test is GLOBAL, so a shared address would make two tests see each other.
func randAdminEmail(t *testing.T) string {
	t.Helper()
	return "admin-" + uuid.NewString() + "@m6.example"
}

// newAdminSession issues a session through the real query and returns its id plus
// the token hash it was stored under (a random string: no token exists anywhere).
func newAdminSession(t *testing.T, d *DB, tenantID, adminID uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	tokenHash := "admin-" + uuid.NewString()
	var row store.CreateAdminSessionRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TenantID: tenantID, AdminUserID: adminID, TokenHash: tokenHash,
		})
		return e
	}); err != nil {
		t.Fatalf("CreateAdminSession: %v", err)
	}
	if row.RevokedAt != nil {
		t.Fatalf("a fresh admin session has revoked_at = %v, want NULL", row.RevokedAt)
	}
	return row.ID, tokenHash
}

// ------------------------------------------------------- the N-row bound ----

// TestGetAdminByEmail_RowBound measures the constraint that REPLACES ADR 0002 madde
// 7's "at most one row" for this lookup: one row PER TENANT, so the count grows
// with the number of businesses the address is an admin of, and never two rows of
// the same tenant. This is the tenant-picker case -- the user decision of
// 2026-08-02 exists precisely because the same person owns Kebab Factory and Kebab
// Manufacturing.
//
// The 1 -> 2 -> 3 progression is the assertion: a lookup that returned only the
// first match would pass at 1 and fail at 2, and a broken WHERE would return
// everyone's admins at every step.
func TestGetAdminByEmail_RowBound(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	email := randAdminEmail(t)

	// Zero first: an address nobody uses resolves to an EMPTY slice and no error --
	// zero is an ordinary cardinality here, not pgx.ErrNoRows.
	got, err := d.GetAdminByEmail(ctx, randAdminEmail(t))
	if err != nil {
		t.Fatalf("GetAdminByEmail(unknown): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unknown email resolved %d rows, want 0", len(got))
	}

	wantIDs := map[uuid.UUID]uuid.UUID{} // adminID -> tenantID
	for n := 1; n <= 3; n++ {
		tenantID, _ := newTenant(t, d)
		adminID := newAdmin(t, d, tenantID, email, "active", "owner")
		wantIDs[adminID] = tenantID

		got, err := d.GetAdminByEmail(ctx, email)
		if err != nil {
			t.Fatalf("GetAdminByEmail (%d tenants): %v", n, err)
		}
		if len(got) != n {
			t.Fatalf("email registered in %d tenants resolved %d rows, want %d", n, len(got), n)
		}

		seenTenants := map[uuid.UUID]bool{}
		for _, a := range got {
			if seenTenants[a.TenantID] {
				t.Fatalf("two rows for tenant %s: the partial UNIQUE (tenant_id, email) must bound this to one per tenant", a.TenantID)
			}
			seenTenants[a.TenantID] = true
			if wantIDs[a.ID] != a.TenantID {
				t.Fatalf("resolved admin %s with tenant %s, which is not one of the fixtures", a.ID, a.TenantID)
			}
			// Read through the ONE named reader: the field is the redacting
			// db.PasswordHash type now (passwordhash.go), not a bare string, so
			// this is also a live check that the scan wraps a non-empty digest
			// rather than silently producing zero values.
			if a.PasswordHash.RevealForPasswordComparison() == "" {
				t.Error("the digest came back empty; the caller would compare against nothing")
			}
			if a.Status != "active" {
				t.Errorf("status = %q, want active", a.Status)
			}
		}
	}
}

// TestGetAdminByEmail_IsCaseInsensitive is the regression test for the measured
// citext trap in 00011. Its NEGATIVE CONTROL is the separate test below (kept
// apart so that a missing owner-role URL skips only the control, never this).
//
// admin_users.email is citext, but the citext `=` operator lives in schema public,
// and the resolver pins search_path to `pg_catalog, pg_temp`. Postgres does not
// error on the missing operator: it falls back to text = text through citext's
// implicit cast, and the lookup silently becomes case SENSITIVE -- an operator who
// types their own address in lowercase cannot log in, and the function's equality
// stops being the equality that the UNIQUE index (and therefore the row bound
// above) is built on.
func TestGetAdminByEmail_IsCaseInsensitive(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()

	tenantID, _ := newTenant(t, d)
	mixed := "Owner." + uuid.NewString() + "@M6.Example"
	adminID := newAdmin(t, d, tenantID, mixed, "active", "owner")

	for _, spelling := range []string{mixed, strings.ToLower(mixed), strings.ToUpper(mixed)} {
		got, err := d.GetAdminByEmail(ctx, spelling)
		if err != nil {
			t.Fatalf("GetAdminByEmail(%q): %v", spelling, err)
		}
		if len(got) != 1 || got[0].ID != adminID {
			t.Fatalf("GetAdminByEmail(%q) resolved %d rows, want exactly the fixture admin; the citext operator must be schema-qualified", spelling, len(got))
		}
	}
}

// TestGetAdminByEmail_CaseInsensitivityNegativeControl builds the BROKEN variant of
// the resolver body -- same pinned search_path, but an unqualified `=` -- shows it
// misses the row for a lowercase spelling, and drops it again. Without this, the
// test above would prove only that citext exists somewhere, not that
// OPERATOR(public.=) in 00011 is load-bearing.
//
// It runs as tappa_owner because only the owner may create a function; the probe is
// uniquely named, is dropped in t.Cleanup, and touches no production object.
func TestGetAdminByEmail_CaseInsensitivityNegativeControl(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()

	tenantID, _ := newTenant(t, d)
	mixed := "Owner." + uuid.NewString() + "@M6.Example"
	newAdmin(t, d, tenantID, mixed, "active", "owner")

	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	probe := "probe_citext_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := owner.pool.Exec(ctx, `
		CREATE FUNCTION `+probe+`(p_email citext)
		    RETURNS TABLE (id uuid)
		    LANGUAGE sql STABLE SECURITY DEFINER
		    SET search_path = pg_catalog, pg_temp
		    AS $$ SELECT a.id FROM public.admin_users AS a WHERE a.email = p_email; $$`); err != nil {
		t.Fatalf("create negative-control function: %v", err)
	}
	t.Cleanup(func() {
		if _, err := owner.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS `+probe+`(citext)`); err != nil {
			t.Errorf("drop negative-control function: %v", err)
		}
	})

	count := func(spelling string) int {
		var n int
		if err := owner.pool.QueryRow(ctx, `SELECT count(*) FROM `+probe+`($1::citext)`, spelling).Scan(&n); err != nil {
			t.Fatalf("negative control query: %v", err)
		}
		return n
	}
	if got := count(mixed); got != 1 {
		t.Fatalf("negative control found %d rows for the EXACT spelling, want 1 (the control itself is broken)", got)
	}
	if got := count(strings.ToLower(mixed)); got != 0 {
		t.Fatalf("negative control found %d rows for the lowercase spelling, want 0; if an unqualified `=` is already case-insensitive under a pinned search_path, the OPERATOR(public.=) in 00011 is not load-bearing and this test proves nothing", got)
	}
}

// TestGetAdminByEmail_CarriesDisabledWithoutFiltering: a disabled admin RESOLVES,
// carrying status='disabled'. Hiding it would make a disabled account look exactly
// like an unknown address, so the attempt could not be attributed to anyone
// (section 4.6) -- and the refusal is the caller's job, backed by
// CreateAdminSession refusing in SQL (tested separately).
func TestGetAdminByEmail_CarriesDisabledWithoutFiltering(t *testing.T) {
	d := appDB(t)
	tenantID, _ := newTenant(t, d)
	email := randAdminEmail(t)
	adminID := newAdmin(t, d, tenantID, email, "disabled", "manager")

	got, err := d.GetAdminByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetAdminByEmail: %v", err)
	}
	if len(got) != 1 || got[0].ID != adminID {
		t.Fatalf("a disabled admin resolved %d rows, want 1 (the resolver carries truth, it does not decide)", len(got))
	}
	if got[0].Status != "disabled" {
		t.Fatalf("status = %q, want disabled", got[0].Status)
	}
}

// ------------------------------------------------- admin session resolver ---

// TestGetAdminSessionByTokenHash_ResolvesContextLess: the panel cookie resolves to
// its tenant and admin with NO tenant context, on the bare pool.
func TestGetAdminSessionByTokenHash_ResolvesContextLess(t *testing.T) {
	d := appDB(t)
	tenantID, _ := newTenant(t, d)
	adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	sessionID, tokenHash := newAdminSession(t, d, tenantID, adminID)

	got, err := d.GetAdminSessionByTokenHash(context.Background(), tokenHash)
	if err != nil {
		t.Fatalf("GetAdminSessionByTokenHash: %v", err)
	}
	if got.ID != sessionID || got.TenantID != tenantID || got.AdminUserID != adminID {
		t.Fatalf("resolved %+v, want session %s / tenant %s / admin %s", got, sessionID, tenantID, adminID)
	}
	if got.RevokedAt != nil {
		t.Fatalf("revoked_at = %v, want nil for a fresh session", got.RevokedAt)
	}
}

// TestGetAdminSessionByTokenHash_UnknownReturnsNoRows: an unknown hash yields
// pgx.ErrNoRows, never a zero-valued struct a caller might treat as a hit.
func TestGetAdminSessionByTokenHash_UnknownReturnsNoRows(t *testing.T) {
	d := appDB(t)
	_, err := d.GetAdminSessionByTokenHash(context.Background(), "admin-"+uuid.NewString())
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("error = %v, want pgx.ErrNoRows", err)
	}
}

// TestGetAdminSessionByTokenHash_CarriesRevocation: a revoked session still
// resolves, reporting its revocation. The caller can then log the attempt instead
// of seeing "unknown token" and losing it (the 00003 principle).
func TestGetAdminSessionByTokenHash_CarriesRevocation(t *testing.T) {
	d := appDB(t)
	tenantID, _ := newTenant(t, d)
	adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	sessionID, tokenHash := newAdminSession(t, d, tenantID, adminID)

	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RevokeAdminSession(ctx, store.RevokeAdminSessionParams{ID: sessionID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("RevokeAdminSession: %v", err)
	}

	got, err := d.GetAdminSessionByTokenHash(context.Background(), tokenHash)
	if err != nil {
		t.Fatalf("a revoked session must still resolve: %v", err)
	}
	if got.RevokedAt == nil {
		t.Fatal("revoked_at = nil for a revoked session; the resolver must carry the truth")
	}
}

// TestAdminAndEmployeeSessionsDoNotOverlap is M1-11's claim, measured: the two
// identity graphs are separate TABLES reached by separate FUNCTIONS, so a token
// hash minted for one cannot authenticate the other -- not because a flag says so,
// but because the row is not there.
func TestAdminAndEmployeeSessionsDoNotOverlap(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	tenantID, locationID := newTenant(t, d)

	adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	_, adminHash := newAdminSession(t, d, tenantID, adminID)

	employeeID := newEmployee(t, d, tenantID, locationID, "active")
	employeeHash := "emp-" + uuid.NewString()
	if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).CreateSession(ctx, store.CreateSessionParams{
			TenantID: tenantID, EmployeeID: employeeID, TokenHash: employeeHash,
		})
		return e
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Positive controls first: each hash resolves through ITS OWN resolver.
	if _, err := d.GetAdminSessionByTokenHash(ctx, adminHash); err != nil {
		t.Fatalf("positive control: the admin hash does not resolve as an admin session: %v", err)
	}
	if _, err := d.GetEmployeeBySessionHash(ctx, employeeHash); err != nil {
		t.Fatalf("positive control: the employee hash does not resolve as an employee session: %v", err)
	}

	// The crossings: neither resolves through the other's resolver.
	if _, err := d.GetAdminSessionByTokenHash(ctx, employeeHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an EMPLOYEE token resolved as an admin session (err = %v); a tap cookie must never reach the panel", err)
	}
	if _, err := d.GetEmployeeBySessionHash(ctx, adminHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an ADMIN token resolved as an employee session (err = %v); a panel cookie must never reach the tap flow", err)
	}
}

// --------------------------------------------- ADR 0002 madde 7 properties --

// TestResolveAdmin_SecurityDefinerProperties measures, in the LIVE catalog and
// against live data, the ADR 0002 madde 7 constraints for BOTH functions 00011
// adds -- rather than trusting the migration text.
//
// It covers TWO different requirements, kept apart on purpose (the M5-02 shape):
//
//	A. The ADR's LEAST-PRIVILEGE clause on the definer: owner tappa_resolver (not a
//	   superuser, which would make the body bypass RLS database-wide), BYPASSRLS
//	   (without it the body sees 0 rows), SECURITY DEFINER, fixed search_path.
//	B. The FIVE interface constraints (i)..(v) -- with (ii) taking its MEASURED
//	   BOUND form for the email lookup, since an admin email is unique only within
//	   a tenant.
func TestResolveAdmin_SecurityDefinerProperties(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d) // (iv) is meaningless unless we are the RLS-subject role
	ctx := context.Background()

	funcs := []struct {
		name       string
		signature  string
		wantArgs   string
		wantResult string
	}{
		{
			name:       "resolve_admin_by_email",
			signature:  "resolve_admin_by_email(citext)",
			wantArgs:   "p_email citext",
			wantResult: "TABLE(id uuid, tenant_id uuid, password_hash text, status text)",
		},
		{
			name:       "resolve_admin_session_by_token_hash",
			signature:  "resolve_admin_session_by_token_hash(text)",
			wantArgs:   "p_token_hash text",
			wantResult: "TABLE(id uuid, tenant_id uuid, admin_user_id uuid, revoked_at timestamp with time zone)",
		},
	}

	for _, fn := range funcs {
		t.Run(fn.name, func(t *testing.T) {
			// --- A. least privilege on the definer ------------------------------
			var owner string
			var ownerSuper, ownerBypass, secDef bool
			var config []string
			if err := d.pool.QueryRow(ctx, `
				SELECT pg_get_userbyid(p.proowner), r.rolsuper, r.rolbypassrls, p.prosecdef, p.proconfig
				FROM pg_proc p
				JOIN pg_roles r ON r.oid = p.proowner
				WHERE p.proname = $1`, fn.name,
			).Scan(&owner, &ownerSuper, &ownerBypass, &secDef, &config); err != nil {
				t.Fatalf("read function catalog: %v", err)
			}
			if owner != "tappa_resolver" {
				t.Errorf("owner = %q, want tappa_resolver (the least-privileged definer)", owner)
			}
			if ownerSuper {
				t.Error("the definer role is a SUPERUSER: the function body would bypass RLS database-wide")
			}
			if !ownerBypass {
				t.Error("the definer role lacks BYPASSRLS: context-less resolution would return 0 rows")
			}
			if !secDef {
				t.Error("prosecdef = false: the function does not run as its owner")
			}
			wantCfg := "search_path=pg_catalog, pg_temp"
			if len(config) != 1 || config[0] != wantCfg {
				t.Errorf("proconfig = %v, want [%q] (a mutable search_path is an injection vector)", config, wantCfg)
			}

			var publicExec, appExec bool
			if err := d.pool.QueryRow(ctx,
				`SELECT has_function_privilege('public', $1, 'EXECUTE'),
				        has_function_privilege('tappa_app', $1, 'EXECUTE')`, fn.signature,
			).Scan(&publicExec, &appExec); err != nil {
				t.Fatalf("read function privileges: %v", err)
			}
			if publicExec {
				t.Error("PUBLIC holds EXECUTE (functions get it by default; the REVOKE must remove it)")
			}
			if !appExec {
				t.Error("tappa_app lacks EXECUTE: the resolution path would be dead")
			}

			// --- (i) key-only input, (iii) no SELECT * surface -------------------
			var args, result string
			if err := d.pool.QueryRow(ctx, `
				SELECT pg_get_function_arguments(p.oid), pg_get_function_result(p.oid)
				FROM pg_proc p WHERE p.proname = $1`, fn.name,
			).Scan(&args, &result); err != nil {
				t.Fatalf("read function signature: %v", err)
			}
			if args != fn.wantArgs {
				t.Errorf("(i) arguments = %q, want exactly %q (key-only input)", args, fn.wantArgs)
			}
			if result != fn.wantResult {
				t.Errorf("(iii) result = %q, want %q (fixed minimal column list)", result, fn.wantResult)
			}
		})
	}

	// --- (ii) the bound, per function ---------------------------------------
	// admin_sessions.token_hash: a single-column GLOBAL UNIQUE index -- the same
	// structural "<=1 row" the other resolvers rely on.
	var sessionUnique int
	if err := d.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = i.indkey[0]
		WHERE c.relname = 'admin_sessions'
		  AND i.indisunique AND i.indnatts = 1 AND a.attname = 'token_hash'`,
	).Scan(&sessionUnique); err != nil {
		t.Fatalf("read admin_sessions unique index: %v", err)
	}
	if sessionUnique != 1 {
		t.Errorf("(ii) admin_sessions.token_hash single-column UNIQUE indexes = %d, want 1 (this is what bounds the context-less lookup to <=1 row)", sessionUnique)
	}

	// admin_users email: there is deliberately NO global unique index -- one address
	// may be an admin of several businesses. What bounds the email lookup is the
	// TENANT-SCOPED partial unique index, which is why the ADR's (ii) takes its
	// per-tenant form here. Both facts are asserted: the per-tenant unique exists,
	// and no global unique on email exists (a global one would silently forbid the
	// two-business case the picker was built for).
	var perTenantUnique, globalUnique int
	if err := d.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE indexdef LIKE '%UNIQUE%' AND indexdef LIKE '%(tenant_id, email)%'),
		  count(*) FILTER (WHERE indexdef LIKE '%UNIQUE%' AND indexdef LIKE '%(email)%')
		FROM pg_indexes WHERE tablename = 'admin_users'`,
	).Scan(&perTenantUnique, &globalUnique); err != nil {
		t.Fatalf("read admin_users indexes: %v", err)
	}
	if perTenantUnique != 1 {
		t.Errorf("(ii) UNIQUE (tenant_id, email) indexes on admin_users = %d, want 1 (this is what bounds the email lookup to one row per tenant)", perTenantUnique)
	}
	if globalUnique != 0 {
		t.Errorf("(ii) a GLOBAL unique index on admin_users(email) exists (%d); it would forbid one person owning two businesses, which is the case the tenant picker exists for", globalUnique)
	}

	// --- (v) no naive "context is NULL -> show the row" RLS branch ------------
	for _, table := range []string{"admin_users", "admin_sessions"} {
		var qual, withCheck string
		if err := d.pool.QueryRow(ctx,
			`SELECT qual, with_check FROM pg_policies WHERE tablename = $1`, table,
		).Scan(&qual, &withCheck); err != nil {
			t.Fatalf("read %s policy: %v", table, err)
		}
		for _, e := range []struct{ name, expr string }{{"USING", qual}, {"WITH CHECK", withCheck}} {
			if !strings.Contains(e.expr, "NULLIF(current_setting('app.tenant_id'::text, true), ''::text)") {
				t.Errorf("(v) %s %s expression %q is not the normative NULLIF form", table, e.name, e.expr)
			}
			if strings.Contains(strings.ToLower(e.expr), " or ") {
				t.Errorf("(v) %s %s expression %q contains an OR branch; a context-less permit branch is forbidden", table, e.name, e.expr)
			}
		}
	}

	// --- (iv) tappa_app cannot read cross-tenant directly, only via EXECUTE ---
	// The live proof, not a privilege reading: with NO tenant context a direct
	// SELECT returns 0 rows (FORCE RLS, fail-closed) while the function returns 1
	// for the same key. Both run as tappa_app on the same pool. Without the positive
	// control (the 1) the 0 would be vacuous.
	tenantID, _ := newTenant(t, d)
	email := randAdminEmail(t)
	adminID := newAdmin(t, d, tenantID, email, "active", "owner")
	_, tokenHash := newAdminSession(t, d, tenantID, adminID)

	probes := []struct {
		name   string
		direct string
		viaFn  string
		arg    any
	}{
		{"admin_users / resolve_admin_by_email",
			`SELECT count(*) FROM admin_users WHERE email = $1::citext`,
			`SELECT count(*) FROM resolve_admin_by_email($1::citext)`, email},
		{"admin_sessions / resolve_admin_session_by_token_hash",
			`SELECT count(*) FROM admin_sessions WHERE token_hash = $1`,
			`SELECT count(*) FROM resolve_admin_session_by_token_hash($1)`, tokenHash},
	}
	for _, p := range probes {
		var direct, viaFunc int
		if err := d.pool.QueryRow(ctx, p.direct, p.arg).Scan(&direct); err != nil {
			t.Fatalf("(iv) %s: context-less direct read errored (%v); it must return 0 rows, not fail", p.name, err)
		}
		if direct != 0 {
			t.Errorf("(iv) %s: a context-less direct SELECT returned %d rows; RLS must hide the row from tappa_app outside a tenant context", p.name, direct)
		}
		if err := d.pool.QueryRow(ctx, p.viaFn, p.arg).Scan(&viaFunc); err != nil {
			t.Fatalf("(iv) %s: context-less resolution errored: %v", p.name, err)
		}
		if viaFunc != 1 {
			t.Errorf("(iv) %s: the bounded interface returned %d rows, want 1 (otherwise the 0 above proves nothing)", p.name, viaFunc)
		}
	}
}

// ------------------------------------------------------------- privilege ----

// TestAdminSessions_UpdateIsColumnScoped proves the 00011 column grant with real
// statements, not catalog readings. Before 00011 all three of these succeeded as
// tappa_app inside its own tenant context (measured); only the first was ever
// mentioned anywhere in the repo:
//
//   - SET revoked_at = NULL          -- un-revoke (state.md's inherited finding)
//   - SET admin_user_id = <owner>    -- a MANAGER's session repointed at the OWNER:
//     privilege escalation in one statement, same tenant, RLS satisfied
//   - SET token_hash = <known hash>  -- session hijack: bind a live session row to
//     a token of the attacker's choosing
//
// The last two are closed at the PRIVILEGE level here. The first cannot be (a
// column grant constrains WHICH column, never WHAT value) and is closed by the
// trigger, tested next.
func TestAdminSessions_UpdateIsColumnScoped(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	tenantID, _ := newTenant(t, d)
	ownerAdmin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	managerAdmin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")
	sessionID, _ := newAdminSession(t, d, tenantID, managerAdmin)

	var tableUpdate, cLastUsed, cRevoked, cToken, cAdminUser, cTenant bool
	if err := d.pool.QueryRow(context.Background(), `
		SELECT has_table_privilege('tappa_app', 'admin_sessions', 'UPDATE'),
		       has_column_privilege('tappa_app', 'admin_sessions', 'last_used_at',  'UPDATE'),
		       has_column_privilege('tappa_app', 'admin_sessions', 'revoked_at',    'UPDATE'),
		       has_column_privilege('tappa_app', 'admin_sessions', 'token_hash',    'UPDATE'),
		       has_column_privilege('tappa_app', 'admin_sessions', 'admin_user_id', 'UPDATE'),
		       has_column_privilege('tappa_app', 'admin_sessions', 'tenant_id',     'UPDATE')`,
	).Scan(&tableUpdate, &cLastUsed, &cRevoked, &cToken, &cAdminUser, &cTenant); err != nil {
		t.Fatalf("read privileges: %v", err)
	}
	if tableUpdate {
		t.Error("tappa_app holds TABLE-WIDE UPDATE on admin_sessions; only (last_used_at, revoked_at) may be granted")
	}
	if !cLastUsed || !cRevoked {
		t.Errorf("tappa_app cannot UPDATE last_used_at=%v revoked_at=%v; refresh and revocation would be impossible", cLastUsed, cRevoked)
	}
	if cToken || cAdminUser || cTenant {
		t.Errorf("tappa_app can UPDATE token_hash=%v admin_user_id=%v tenant_id=%v, want all false", cToken, cAdminUser, cTenant)
	}

	exec := func(sql string, args ...any) error {
		return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, sql, args...)
			return e
		})
	}
	assertPermissionDenied(t, "repoint a manager's session at the owner (SET admin_user_id)",
		exec(`UPDATE admin_sessions SET admin_user_id = $3 WHERE id = $1 AND tenant_id = $2`, sessionID, tenantID, ownerAdmin))
	assertPermissionDenied(t, "hijack a session (SET token_hash)",
		exec(`UPDATE admin_sessions SET token_hash = $3 WHERE id = $1 AND tenant_id = $2`, sessionID, tenantID, "attacker-"+uuid.NewString()))

	// Positive control: the two writes the application MUST be able to make still
	// work through the real queries, so the REVOKE did not break the panel.
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).TouchAdminSession(ctx, store.TouchAdminSessionParams{ID: sessionID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("positive control: TouchAdminSession broke under the column grant: %v", err)
	}
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RevokeAdminSession(ctx, store.RevokeAdminSessionParams{ID: sessionID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("positive control: RevokeAdminSession broke under the column grant: %v", err)
	}
}

// assertMonotonicTrigger checks a statement failed because of the 00011 trigger and
// not for some neighbouring reason (a missing grant would say "permission denied").
func assertMonotonicTrigger(t *testing.T, op string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: succeeded, want the monotonic-revocation trigger to block it", op)
	}
	pg := asPgErr(err)
	if pg == nil || !strings.Contains(pg.Message, "revocation is monotonic") {
		t.Fatalf("%s: want the monotonicity trigger (\"revocation is monotonic\"), got %v", op, err)
	}
}

// TestAdminSessions_RevocationIsMonotonic is the structural fix state.md records as
// missing for sessions.revoked_at ("tappa_app can UPDATE it back to NULL -- the
// structural fix is a trigger and needs a NEW migration"). 00011 is that migration
// for the admin table, and this test measures all four transitions, including the
// two that must still WORK.
//
// It matters because revocation is the panel's only instant kill switch: if
// revoked_at can go back to NULL, "sign out everywhere" is advisory, and the audit
// answer to "when did this session die" is rewritable.
func TestAdminSessions_RevocationIsMonotonic(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	tenantID, _ := newTenant(t, d)
	adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	sessionID, _ := newAdminSession(t, d, tenantID, adminID)

	exec := func(sql string, args ...any) error {
		return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, sql, args...)
			return e
		})
	}
	revokedAt := func() *time.Time {
		var at *time.Time
		if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT revoked_at FROM admin_sessions WHERE id = $1 AND tenant_id = $2`,
				sessionID, tenantID).Scan(&at)
		}); err != nil {
			t.Fatalf("read revoked_at: %v", err)
		}
		return at
	}

	// 1. NULL -> a timestamp is ALLOWED (otherwise nothing could be revoked).
	if err := exec(`UPDATE admin_sessions SET revoked_at = now() WHERE id = $1 AND tenant_id = $2`, sessionID, tenantID); err != nil {
		t.Fatalf("revoking a live session must be allowed: %v", err)
	}
	first := revokedAt()
	if first == nil {
		t.Fatal("revocation did not take effect")
	}

	// 2. non-NULL -> NULL is REFUSED (the un-revoke).
	assertMonotonicTrigger(t, "un-revoke (SET revoked_at = NULL)",
		exec(`UPDATE admin_sessions SET revoked_at = NULL WHERE id = $1 AND tenant_id = $2`, sessionID, tenantID))

	// 3. non-NULL -> a DIFFERENT instant is REFUSED (rewriting when it died).
	assertMonotonicTrigger(t, "re-stamp (SET revoked_at = another instant)",
		exec(`UPDATE admin_sessions SET revoked_at = now() + interval '1 day' WHERE id = $1 AND tenant_id = $2`, sessionID, tenantID))

	// 4. The REAL query stays idempotent: COALESCE writes the SAME value, so the
	//    trigger's WHEN clause is false and nothing fires.
	var row store.RevokeAdminSessionRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).RevokeAdminSession(ctx, store.RevokeAdminSessionParams{ID: sessionID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("a repeated RevokeAdminSession must stay idempotent, got: %v", err)
	}
	if row.RevokedAt == nil || !row.RevokedAt.Equal(*first) {
		t.Fatalf("repeated revoke returned %v, want the FIRST stamp %v", row.RevokedAt, first)
	}
	if after := revokedAt(); after == nil || !after.Equal(*first) {
		t.Fatalf("revoked_at changed to %v after a repeat revoke, want %v", after, first)
	}

	// A refresh of a DIFFERENT column on the revoked row is still refused -- but by
	// the query's own guard, not the trigger: TouchAdminSession requires
	// revoked_at IS NULL, so it matches 0 rows.
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).TouchAdminSession(ctx, store.TouchAdminSessionParams{ID: sessionID, TenantID: tenantID})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("TouchAdminSession on a revoked session: error = %v, want pgx.ErrNoRows", err)
	}
}

// TestAdminSessions_RevocationTriggerBindsTheOwnerToo: the trigger's whole point
// over a REVOKE is that it also stops the SUPERUSER, which FORCE RLS and privilege
// grants cannot (M0-03). Same belt 00005 puts over transactions/audit_log.
func TestAdminSessions_RevocationTriggerBindsTheOwnerToo(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)

	tenantID, _ := newTenant(t, app)
	adminID := newAdmin(t, app, tenantID, randAdminEmail(t), "active", "owner")
	sessionID, _ := newAdminSession(t, app, tenantID, adminID)
	if err := app.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RevokeAdminSession(ctx, store.RevokeAdminSessionParams{ID: sessionID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("prepare revoked session: %v", err)
	}

	// The owner bypasses RLS and holds every privilege; only the trigger stands in
	// the way. (No tenant context is set: the owner does not need one.)
	_, err := owner.pool.Exec(context.Background(),
		`UPDATE admin_sessions SET revoked_at = NULL WHERE id = $1`, sessionID)
	assertMonotonicTrigger(t, "owner un-revoke", err)
}

// ------------------------------------------------------ the query surface ---

// TestCreateAdminSession_RefusesDisabledAdmin: "a disabled admin never gets a
// session" is a property of the STATEMENT, not of a handler remembering to check.
// The careless-caller shape is used deliberately -- the error is swallowed and the
// transaction COMMITS -- so the assertion is about the database, exactly like the
// M5-02 invite proof.
func TestCreateAdminSession_RefusesDisabledAdmin(t *testing.T) {
	d := appDB(t)
	tenantID, _ := newTenant(t, d)
	disabled := newAdmin(t, d, tenantID, randAdminEmail(t), "disabled", "manager")
	active := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")

	tokenHash := "admin-" + uuid.NewString()
	var queryErr error
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, queryErr = store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TenantID: tenantID, AdminUserID: disabled, TokenHash: tokenHash,
		})
		return nil // deliberately NOT propagated: the transaction commits
	}); err != nil {
		t.Fatalf("the transaction itself must commit for this test to mean anything: %v", err)
	}
	if !errors.Is(queryErr, pgx.ErrNoRows) {
		t.Fatalf("CreateAdminSession for a disabled admin: error = %v, want pgx.ErrNoRows", queryErr)
	}
	// And nothing was written: the hash resolves to nothing at all.
	if _, err := d.GetAdminSessionByTokenHash(context.Background(), tokenHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a session row was created for a disabled admin despite the committed transaction (err = %v)", err)
	}

	// Positive control: the same call for an ACTIVE admin succeeds, so the refusal
	// above is the status test and not something structural. newAdminSession fails
	// the test itself if the insert does not return a row.
	newAdminSession(t, d, tenantID, active)
}

// TestCreateAdminSession_RefusesForeignAdmin: an admin of ANOTHER tenant cannot be
// given a session in this context. Two independent things stand in the way (RLS
// hides the admin row from the INSERT's SELECT, and the composite FK would refuse
// the link), which is the belt + braces section 4.5 asks for.
func TestCreateAdminSession_RefusesForeignAdmin(t *testing.T) {
	d := appDB(t)
	tenantA, _ := newTenant(t, d)
	tenantB, _ := newTenant(t, d)
	adminA := newAdmin(t, d, tenantA, randAdminEmail(t), "active", "owner")

	err := d.WithTenant(context.Background(), tenantB, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TenantID: tenantB, AdminUserID: adminA, TokenHash: "admin-" + uuid.NewString(),
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("issuing a session for another tenant's admin: error = %v, want pgx.ErrNoRows", err)
	}
}

// TestResolvedAdmin_ZeroValueFailsClosed is the DATABASE half of the M5-01 polar
// lesson that passwordhash.go states: the dangerous state must not be the state you
// get by forgetting. A ResolvedAdmin that was never populated -- a phase-B handler
// that took a branch without assigning it, a candidate slice indexed past a bound
// check that was not there -- carries uuid.Nil for both identities and an empty
// digest. It must authenticate NOBODY.
//
// The rendering half (the zero digest reveals "" and every print path tolerates the
// nil pointer) is measured in resolve_leak_external_test.go; what only a real
// Postgres can show is that the zero IDENTITY buys nothing: uuid.Nil matches no
// admin row, so the INSERT ... SELECT produces zero rows and sqlc returns
// pgx.ErrNoRows -- the same outcome as a disabled or foreign admin.
//
// An empty digest failing every KDF comparison is NOT measured here and is not
// claimed as measured: go.mod carries no KDF (bcrypt is phase B's dependency, Q03).
// It is stated as reasoning in passwordhash.go, and this test covers the half that
// does not depend on it.
func TestResolvedAdmin_ZeroValueFailsClosed(t *testing.T) {
	d := appDB(t)
	tenantA, _ := newTenant(t, d)

	var zero ResolvedAdmin
	if got := zero.PasswordHash.RevealForPasswordComparison(); got != "" {
		t.Fatalf("zero value carries a digest %q; it must carry none", got)
	}
	if zero.ID != uuid.Nil || zero.TenantID != uuid.Nil {
		t.Fatalf("zero value identity = %s/%s, want nil uuids", zero.ID, zero.TenantID)
	}

	// The zero identity, offered inside a REAL tenant context (the most generous
	// case for an attacker: the context is valid, only the candidate is empty).
	err := d.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TenantID: tenantA, AdminUserID: zero.ID, TokenHash: "admin-" + uuid.NewString(),
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("issuing a session for the zero admin id: error = %v, want pgx.ErrNoRows", err)
	}

	// And with the zero TENANT too, which is the shape a handler that forgot the
	// struct entirely would produce. RLS refuses the write before the SELECT even
	// matters, so the error is not ErrNoRows -- what matters is that it is an error
	// and no session exists.
	err = d.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TenantID: zero.TenantID, AdminUserID: zero.ID, TokenHash: "admin-" + uuid.NewString(),
		})
		return e
	})
	if err == nil {
		t.Fatal("issuing a session for a wholly zero ResolvedAdmin succeeded")
	}
}

// TestTouchAdminSession_IsTheAuthority covers the three ways a panel request must
// fail closed and the one way it must succeed. All failures are the SAME outcome
// (0 rows), which is deliberate: the caller cannot use them as an oracle, and all
// three mean "go to the login page".
func TestTouchAdminSession_IsTheAuthority(t *testing.T) {
	d := appDB(t)
	tenantID, _ := newTenant(t, d)

	touch := func(sessionID, ctxTenant uuid.UUID) (store.TouchAdminSessionRow, error) {
		var row store.TouchAdminSessionRow
		err := d.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			var e error
			row, e = store.New(tx).TouchAdminSession(ctx, store.TouchAdminSessionParams{
				ID: sessionID, TenantID: ctxTenant,
			})
			return e
		})
		return row, err
	}

	t.Run("live session of an active admin refreshes and names the actor", func(t *testing.T) {
		adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")
		sessionID, _ := newAdminSession(t, d, tenantID, adminID)

		row, err := touch(sessionID, tenantID)
		if err != nil {
			t.Fatalf("TouchAdminSession: %v", err)
		}
		if row.AdminUserID != adminID {
			t.Fatalf("admin_user_id = %s, want %s", row.AdminUserID, adminID)
		}
		if row.Role != "manager" {
			t.Fatalf("role = %q, want manager (the caller needs it for actor:role without a second query)", row.Role)
		}
		if row.LastUsedAt == nil {
			t.Fatal("last_used_at is still NULL after a refresh")
		}
	})

	t.Run("revoked session", func(t *testing.T) {
		adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")
		sessionID, _ := newAdminSession(t, d, tenantID, adminID)
		if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := store.New(tx).RevokeAdminSession(ctx, store.RevokeAdminSessionParams{ID: sessionID, TenantID: tenantID})
			return e
		}); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if _, err := touch(sessionID, tenantID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("error = %v, want pgx.ErrNoRows", err)
		}
	})

	// The one that matters most: DISABLING an admin ends their panel access on the
	// very next request, with no revocation issued. This is the inherited employee
	// trap (state.md: the session resolver does not carry employees.status, so every
	// surface must check it itself) closed in SQL for the admin surface.
	t.Run("admin disabled after the session was issued", func(t *testing.T) {
		adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")
		sessionID, tokenHash := newAdminSession(t, d, tenantID, adminID)
		if _, err := touch(sessionID, tenantID); err != nil {
			t.Fatalf("positive control before disabling: %v", err)
		}
		if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1 AND tenant_id = $2`, adminID, tenantID)
			return e
		}); err != nil {
			t.Fatalf("disable admin: %v", err)
		}
		if _, err := touch(sessionID, tenantID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("a disabled admin still passed TouchAdminSession (err = %v); the panel would keep serving them", err)
		}
		// The session row itself is untouched -- not revoked, not deleted. The trail
		// says "this session existed and stopped being usable", which is what a later
		// audit needs (section 4.6).
		got, err := d.GetAdminSessionByTokenHash(context.Background(), tokenHash)
		if err != nil {
			t.Fatalf("the session row must survive a disable: %v", err)
		}
		if got.RevokedAt != nil {
			t.Fatalf("disabling an admin revoked the session (revoked_at = %v); it must fail closed WITHOUT rewriting history", got.RevokedAt)
		}
	})

	t.Run("another tenant's context", func(t *testing.T) {
		adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")
		sessionID, _ := newAdminSession(t, d, tenantID, adminID)
		otherTenant, _ := newTenant(t, d)
		if _, err := touch(sessionID, otherTenant); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("error = %v, want pgx.ErrNoRows", err)
		}
		// Positive control: in its OWN context the same session touches fine, so the
		// refusal above is the tenant scope and not a broken fixture.
		if _, err := touch(sessionID, tenantID); err != nil {
			t.Fatalf("positive control: own-tenant touch failed: %v", err)
		}
	})
}

// TestRevokeAdminSessionsForAdmin_SignsOutEverywhere: the bulk revoke hits every
// live session of ONE admin and nobody else's, and is idempotent.
func TestRevokeAdminSessionsForAdmin_SignsOutEverywhere(t *testing.T) {
	d := appDB(t)
	tenantID, _ := newTenant(t, d)
	adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	otherAdmin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")

	s1, _ := newAdminSession(t, d, tenantID, adminID)
	s2, _ := newAdminSession(t, d, tenantID, adminID)
	sOther, _ := newAdminSession(t, d, tenantID, otherAdmin)

	revokeAll := func(id uuid.UUID) []uuid.UUID {
		var ids []uuid.UUID
		if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			var e error
			ids, e = store.New(tx).RevokeAdminSessionsForAdmin(ctx, store.RevokeAdminSessionsForAdminParams{
				TenantID: tenantID, AdminUserID: id,
			})
			return e
		}); err != nil {
			t.Fatalf("RevokeAdminSessionsForAdmin: %v", err)
		}
		return ids
	}

	got := revokeAll(adminID)
	if len(got) != 2 {
		t.Fatalf("revoked %d sessions, want 2", len(got))
	}
	seen := map[uuid.UUID]bool{got[0]: true, got[1]: true}
	if !seen[s1] || !seen[s2] {
		t.Fatalf("revoked %v, want %s and %s", got, s1, s2)
	}

	// Idempotent: a second call finds nothing live.
	if again := revokeAll(adminID); len(again) != 0 {
		t.Fatalf("a repeated bulk revoke returned %d ids, want 0", len(again))
	}

	// The other admin's session is untouched (positive control).
	var otherRevoked *time.Time
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT revoked_at FROM admin_sessions WHERE id = $1 AND tenant_id = $2`,
			sOther, tenantID).Scan(&otherRevoked)
	}); err != nil {
		t.Fatalf("read other admin's session: %v", err)
	}
	if otherRevoked != nil {
		t.Fatalf("another admin's session was revoked (revoked_at = %v)", otherRevoked)
	}
}

// TestAdminReads_TenantScopedSurface covers the two in-context reads the login flow
// needs after resolution, including the picker row. It is NOT an isolation proof --
// these queries carry explicit tenant_id predicates, so a 0 could come from the
// WHERE; the isolation proof is TestRLS_AdminAuthSurface_Isolation below and the
// tables in rls_test.go.
func TestAdminReads_TenantScopedSurface(t *testing.T) {
	d := appDB(t)
	tenantID, _ := newTenant(t, d)
	adminID := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	disabledID := newAdmin(t, d, tenantID, randAdminEmail(t), "disabled", "manager")

	q := func(fn func(q *store.Queries, ctx context.Context) error) error {
		return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return fn(store.New(tx), ctx)
		})
	}

	// GetAdminByID returns status rather than filtering on it: a caller must be able
	// to say "this account is disabled" instead of "no such admin".
	var disabledRow store.GetAdminByIDRow
	if err := q(func(s *store.Queries, ctx context.Context) error {
		var e error
		disabledRow, e = s.GetAdminByID(ctx, store.GetAdminByIDParams{ID: disabledID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("GetAdminByID(disabled): %v", err)
	}
	if disabledRow.Status != "disabled" || disabledRow.Role != "manager" {
		t.Fatalf("GetAdminByID returned status=%q role=%q, want disabled/manager", disabledRow.Status, disabledRow.Role)
	}

	// GetAdminForTenantChoice: the picker row. It carries the BUSINESS NAME, which
	// the resolver deliberately cannot supply, and it refuses a disabled admin so the
	// picker never offers a door CreateAdminSession would then refuse to open.
	var choice store.GetAdminForTenantChoiceRow
	if err := q(func(s *store.Queries, ctx context.Context) error {
		var e error
		choice, e = s.GetAdminForTenantChoice(ctx, store.GetAdminForTenantChoiceParams{ID: adminID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("GetAdminForTenantChoice: %v", err)
	}
	if choice.TenantName == "" || choice.Role != "owner" {
		t.Fatalf("picker row = %+v, want a tenant name and role=owner", choice)
	}
	err := q(func(s *store.Queries, ctx context.Context) error {
		_, e := s.GetAdminForTenantChoice(ctx, store.GetAdminForTenantChoiceParams{ID: disabledID, TenantID: tenantID})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the picker offered a DISABLED admin's tenant (err = %v), want pgx.ErrNoRows", err)
	}

	// MarkAdminLoggedIn stamps an active admin and refuses a disabled one.
	var stamp store.MarkAdminLoggedInRow
	if err := q(func(s *store.Queries, ctx context.Context) error {
		var e error
		stamp, e = s.MarkAdminLoggedIn(ctx, store.MarkAdminLoggedInParams{ID: adminID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("MarkAdminLoggedIn: %v", err)
	}
	if stamp.LastLoginAt == nil {
		t.Fatal("last_login_at is still NULL after a login stamp")
	}
	err = q(func(s *store.Queries, ctx context.Context) error {
		_, e := s.MarkAdminLoggedIn(ctx, store.MarkAdminLoggedInParams{ID: disabledID, TenantID: tenantID})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a disabled admin got a login stamp (err = %v), want pgx.ErrNoRows", err)
	}

	// ListAdminSessionsForAdmin shows revoked rows too: the history stays visible.
	sessionID, _ := newAdminSession(t, d, tenantID, adminID)
	if err := q(func(s *store.Queries, ctx context.Context) error {
		_, e := s.RevokeAdminSession(ctx, store.RevokeAdminSessionParams{ID: sessionID, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	var list []store.ListAdminSessionsForAdminRow
	if err := q(func(s *store.Queries, ctx context.Context) error {
		var e error
		list, e = s.ListAdminSessionsForAdmin(ctx, store.ListAdminSessionsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID,
		})
		return e
	}); err != nil {
		t.Fatalf("ListAdminSessionsForAdmin: %v", err)
	}
	found := false
	for _, r := range list {
		if r.ID == sessionID {
			found = true
			if r.RevokedAt == nil {
				t.Error("the revoked session is listed without its revocation stamp")
			}
		}
	}
	if !found {
		t.Error("a revoked session vanished from the list; the trail must stay visible (section 4.6)")
	}
}

// -------------------------------------------------------------- isolation ---

// TestRLS_AdminAuthSurface_Isolation is the mandatory isolation proof over the rows
// THIS task's queries write (rls_test.go proves the same for hand-built fixtures).
//
// The probes carry NO tenant_id predicate on purpose (state.md / CLAUDE.md section
// 6): a filtered query would return 0 rows because of the WHERE, not because of
// RLS, and would stay green with RLS switched off. Each negative is paired with a
// positive control in the row's own context, so a broken fixture fails loudly
// instead of passing vacuously.
//
// Verified non-vacuous by a MUTATION run: with
// `ALTER TABLE admin_sessions DISABLE ROW LEVEL SECURITY` this test fails on the
// admin_sessions case, and passes again once it is re-enabled.
func TestRLS_AdminAuthSurface_Isolation(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app) // runtime proof: tappa_app, no super/bypass

	tenantA, _ := newTenant(t, app)
	tenantB, _ := newTenant(t, app)
	adminA := newAdmin(t, app, tenantA, randAdminEmail(t), "active", "owner")
	sessionA, _ := newAdminSession(t, app, tenantA, adminA)

	cases := []struct {
		name  string
		query string
		arg   any
	}{
		{"admin_users", `SELECT count(*) FROM admin_users WHERE id = $1`, adminA},
		{"admin_sessions", `SELECT count(*) FROM admin_sessions WHERE id = $1`, sessionA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if self := rawCountCtx(t, app, tenantA, tc.query, tc.arg); self != 1 {
				t.Fatalf("positive control: A's context sees %d %s rows for its own row, want 1 (the isolation check below would be vacuous)", self, tc.name)
			}
			if cross := rawCountCtx(t, app, tenantB, tc.query, tc.arg); cross != 0 {
				t.Fatalf("RLS FAILED: B's context read %d of A's %s rows, want 0", cross, tc.name)
			}
		})
	}

	// The write side for the surface 00011 adds: B cannot stamp a session with A's
	// tenant_id even while naming A's admin (FK checks bypass RLS, so only WITH
	// CHECK stands in the way).
	errForge := app.WithTenant(context.Background(), tenantB, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`,
			uuid.New(), tenantA, adminA, "forge-"+uuid.NewString())
		return e
	})
	assertWriteBlocked(t, "admin_sessions", blockRLS, errForge)
}

// ------------------------------------------------- incumbent-first ordering --

// TestGetAdminByEmail_TheIncumbentIsAlwaysCompared — migration 00017's whole
// content, as a test.
//
// 🔴 WHAT IT DEFENDS. Migration 00011 handed M7-02 a bound it could not close in the
// database: internal/adminauth compares at most MaxCandidates rows per login, so
// WHICH rows the resolver puts first decides whose password is tested when several
// tenants hold one address. Under 00011's `ORDER BY tenant_id` that order is a
// random permutation with respect to when a row was written — a tenant id is
// gen_random_uuid() — so an attacker registering enough tenants carrying a victim's
// address pushed the victim out of the compared window and the victim could no
// longer sign in. Measured on this schema before 00017: with twenty attacker rows
// the victim landed outside the first eight in three of five runs.
//
// 00017 orders by created_at first, so the OLDEST row is compared first and the one
// thing an attacker cannot do is register earlier than somebody who is already a
// customer.
//
// ⚠️ IT IS NOT A CPU BOUND AND DOES NOT PRETEND TO BE ONE. What bounds the bcrypt
// work per request is adminauth.MaxCandidates; this decides WHICH rows are spent on.
//
// ⚠️ AND IT DOES NOT CLOSE THE PRE-REGISTRATION VARIANT: rows planted BEFORE an
// address ever signs up still sort first. Migration 00017 states that limit and
// names what closes it (email verification, which needs a transport this repository
// does not have — Q02, M7-04). This test measures the half that IS closed.
func TestGetAdminByEmail_TheIncumbentIsAlwaysCompared(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	email := randAdminEmail(t)

	// The incumbent: a paying customer whose admin row was written a year ago.
	victimTenant, _ := newTenant(t, d)
	victim := newAdmin(t, d, victimTenant, email, "active", "owner")
	backdateAdmin(t, d, victimTenant, victim, "365 days")

	// The attack: twenty later registrations carrying the same address. Twenty is
	// deliberately more than twice adminauth.MaxCandidates, so under a random
	// ordering the victim would be outside the window most of the time.
	const attackers = 20
	for i := 0; i < attackers; i++ {
		tenantID, _ := newTenant(t, d)
		newAdmin(t, d, tenantID, email, "active", "owner")
	}

	rows, err := d.GetAdminByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetAdminByEmail: %v", err)
	}
	if len(rows) != attackers+1 {
		t.Fatalf("resolved %d row(s), want %d; the fixture is not what this test thinks",
			len(rows), attackers+1)
	}
	if rows[0].ID != victim {
		pos := -1
		for i, r := range rows {
			if r.ID == victim {
				pos = i + 1
			}
		}
		t.Fatalf("the incumbent's row is at position %d of %d rather than first.\n"+
			"internal/adminauth compares only the first MaxCandidates, so a customer who "+
			"sorts late CANNOT LOG IN and is told only that the credentials are wrong. "+
			"Migration 00017 orders by created_at for exactly this.", pos, len(rows))
	}

	// ANTI-VACUITY: the attackers must genuinely be in the result, or "the victim is
	// first" would be true of a list of one.
	if len(rows) < 9 {
		t.Fatalf("only %d row(s) resolved; with fewer than adminauth.MaxCandidates+1 this "+
			"test cannot observe a truncation at all", len(rows))
	}
}

// backdateAdmin moves an admin row's created_at into the past, which is what makes it
// the INCUMBENT.
//
// 🔴 IT RUNS AS THE OWNER, AND THAT IS THE POINT RATHER THAN A CONVENIENCE. Migration
// 00017 narrowed tappa_app's UPDATE on admin_users to a column list that deliberately
// EXCLUDES created_at, because the whole incumbent-first argument rests on that column
// ("the one thing an attacker cannot do is register earlier"). This helper needed the
// privilege that was taken away, so it moves to the migration role — which is exactly
// the right outcome: a test fixture doing something the application is no longer
// allowed to do should have to say so.
//
// ⚠️ IF THIS EVER SUCCEEDS AS tappa_app AGAIN, THE GRANT HAS BEEN WIDENED. That is
// asserted directly rather than left implicit — see the probe below.
func backdateAdmin(t *testing.T, d *DB, tenantID, adminID uuid.UUID, interval string) {
	t.Helper()
	// THE APPLICATION ROLE MUST NOT BE ABLE TO DO THIS. Checked here, where the
	// capability is used, so the fixture itself is the tripwire on the grant.
	appErr := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE admin_users SET created_at = now() - $3::interval
			 WHERE id = $1 AND tenant_id = $2`, adminID, tenantID, interval)
		return e
	})
	if appErr == nil {
		t.Fatalf("tappa_app rewrote admin_users.created_at. Migration 00017's incumbent-first " +
			"ordering — and every argument built on it — assumes the application cannot " +
			"move a row's position in that order. The column grant has been widened.")
	}
	owner := ownerDB(t)
	if err := owner.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE admin_users SET created_at = now() - $3::interval
			 WHERE id = $1 AND tenant_id = $2`, adminID, tenantID, interval)
		return e
	}); err != nil {
		t.Fatalf("backdating the incumbent as the owner: %v", err)
	}
}
