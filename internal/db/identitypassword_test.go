package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
)

// identitypassword_test.go -- migration 00072 (M14-05, ADR 0031 §5.1, §5.2, §5.6, §5.8, §5.9)
// against real Postgres: the identity's reset / verification / event tables, the thirteen functions
// of the closed list this card owns, and resolve_admin_by_email's redefinition.
//
// 🔴 NOTHING HERE COMMITS. Every test runs in opTx's transaction, rolled back (00069's and 00072's
// Downs refuse while an identity, a refused row or an identity event exists).
//
// SIGNATURES AND MATRIX: ipFunctions and ipMatrix are THIS card's rows of ADR 0031 §5.2's signature
// catalogue and the definer's verb x column matrix. M14-04 (00071, a parallel lane) owns the
// catalogue test itself; when the lanes meet, these two tables are unioned into its lists (the
// matrix cells become the union of both files' grants) and this file's two catalogue tests go.

const ip00072File = "00072_set_identity_passwords_and_link_legacy_rows.sql"

// ---------------------------------------------------------------- fixtures --

// ipDigest is a REAL bcrypt digest of pw at cost 4 (pgcrypto, fast), so a test can ask whether a
// password opens a row.
func ipDigest(t *testing.T, ctx context.Context, q pgx.Tx, pw string) string {
	t.Helper()
	var d string
	if err := q.QueryRow(ctx, `SELECT public.crypt($1, public.gen_salt('bf', 4))`, pw).Scan(&d); err != nil {
		t.Fatalf("digest: %v", err)
	}
	return d
}

func ipOpens(t *testing.T, ctx context.Context, q pgx.Tx, pw, digest string) bool {
	t.Helper()
	var ok bool
	if err := q.QueryRow(ctx, `SELECT public.crypt($1, $2) = $2`, pw, digest).Scan(&ok); err != nil {
		t.Fatalf("compare: %v", err)
	}
	return ok
}

func ipIdentity(t *testing.T, ctx context.Context, q pgx.Tx, email, digest string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := q.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`, email, digest).Scan(&id); err != nil {
		t.Fatalf("insert an identity: %v", err)
	}
	return id
}

func ipMember(t *testing.T, ctx context.Context, q pgx.Tx, tenant uuid.UUID, email, digest string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	aiExec(t, ctx, q, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
	                   VALUES ($1, $2, 'M14-05 Admin', $3, $4, 'owner')`, id, tenant, email, digest)
	return id
}

// ipLink links a row as the owner, the way a backfill or an earlier function would have: the
// identity, the way, linked_at `ago` before now, and the identity's digest (the invariant).
func ipLink(t *testing.T, ctx context.Context, q pgx.Tx, admin, identity uuid.UUID, via, ago string) {
	t.Helper()
	aiExec(t, ctx, q, `UPDATE admin_users SET identity_id = $2, linked_via = $3, linked_at = now() - $4::interval,
	                          password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2)
	                    WHERE id = $1`, admin, identity, via, ago)
}

// ipAuth is how fresh a fixture session is.
type ipAuth string

const (
	ipFresh  ipAuth = "now()"
	ipStale  ipAuth = "now() - interval '6 minutes'"
	ipNull   ipAuth = "NULL"
	ipFuture ipAuth = "now() + interval '1 hour'"
)

// ipSession inserts a live session (as the owner) and returns its token hash -- the value the
// functions take.
func ipSession(t *testing.T, ctx context.Context, q pgx.Tx, tenant, admin uuid.UUID, auth ipAuth) string {
	t.Helper()
	h := opRandHex(t)
	aiExec(t, ctx, q, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, authenticated_at)
	                   VALUES ($1, $2, $3, `+string(auth)+`)`, tenant, admin, h)
	return h
}

func ipLive(t *testing.T, ctx context.Context, q pgx.Tx, hash string) bool {
	t.Helper()
	return opInt(t, ctx, q, `SELECT (revoked_at IS NULL)::int FROM admin_sessions WHERE token_hash = $1`, hash) == 1
}

func ipOpenWindow(t *testing.T, ctx context.Context, q pgx.Tx, identity uuid.UUID, scope string) {
	t.Helper()
	aiExec(t, ctx, q, `UPDATE admin_identities SET email_verified_at = now(), confirm_opened_at = now(),
	                          confirm_open_until = now() + interval '7 days', confirm_scope = $2 WHERE id = $1`, identity, scope)
}

// ipApp runs one statement as tappa_app with NO tenant context (the identity functions need none)
// and returns its rows.
func ipApp(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) ([][]any, error) {
	t.Helper()
	var out [][]any
	err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := rows.Values()
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

// ipRows is ipApp that fails the test on an error and returns the row count.
func ipRows(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) int {
	t.Helper()
	rows, err := ipApp(t, ctx, tx, sql, args...)
	if err != nil {
		t.Fatalf("tappa_app: %s: %v", sql, err)
	}
	return len(rows)
}

// ipInvariant is ADR 0031 §5.1's tested invariant (bulgu 6): every LINKED row of these identities
// carries its identity's digest (a refused row is not linked).
func ipInvariant(t *testing.T, ctx context.Context, q pgx.Tx, identities ...uuid.UUID) {
	t.Helper()
	if n := opInt(t, ctx, q, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                           WHERE a.identity_id = ANY ($1) AND a.unlinked_at IS NULL
	                             AND a.password_hash IS DISTINCT FROM i.password_hash`, identities); n != 0 {
		t.Errorf("INVARIANT: %d linked rows do not carry their identity's digest", n)
	}
}

// ipAudit returns the audit rows of a tenant written in this transaction, action|target|detail.
func ipAudit(t *testing.T, ctx context.Context, q pgx.Tx, tenant uuid.UUID) []string {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT action || '|' || coalesce(target, '') || '|' || detail::text || '|' || coalesce(actor_id::text, '')
	                             FROM audit_log WHERE tenant_id = $1 AND at = now() ORDER BY action, target`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ipAuditClean fails if an audit row names something it must not (ADR 0031 §5.2, §6): the other
// tenant, the identity, linked_via's leaking values, an address.
func ipAuditClean(t *testing.T, rows []string, forbidden ...string) {
	t.Helper()
	for _, r := range rows {
		for _, f := range forbidden {
			if f != "" && strings.Contains(r, f) {
				t.Errorf("audit row %q names %q", r, f)
			}
		}
		for _, f := range []string{"identity", "password\"", "account\"", "linked_via", "@"} {
			if strings.Contains(r, f) {
				t.Errorf("audit row %q carries %q", r, f)
			}
		}
	}
}

func ipEmail() string { return "m14-05-" + uuid.NewString() + "@identity.example" }

// ipPerson is one identity with its address, password and digest.
type ipPerson struct {
	id                uuid.UUID
	email, pw, digest string
}

func ipNewPerson(t *testing.T, ctx context.Context, q pgx.Tx) ipPerson {
	t.Helper()
	p := ipPerson{email: ipEmail(), pw: "pw-" + uuid.NewString()}
	p.digest = ipDigest(t, ctx, q, p.pw)
	p.id = ipIdentity(t, ctx, q, p.email, p.digest)
	return p
}

// ---------------------------------------------------------- the catalogue --

// ipFunctions is this card's part of ADR 0031 §5.2's signature catalogue: identity arguments,
// result, owner, volatility. Every one is SECURITY DEFINER, search_path pinned, EXECUTE to
// tappa_app and nobody else but its owner.
var ipFunctions = []struct{ name, args, result, owner, volatile string }{
	{"resolve_admin_by_email", "p_email citext", "TABLE(id uuid, tenant_id uuid, password_hash text, status text, identity_id uuid)", "tappa_resolver", "s"},
	{"resolve_identity_reset_by_token_hash", "p_token_hash text", "TABLE(id uuid, identity_id uuid, expires_at timestamp with time zone, used_at timestamp with time zone, cancelled_at timestamp with time zone)", "tappa_resolver", "s"},
	{"resolve_identity_verification_by_token_hash", "p_token_hash text", "TABLE(id uuid, identity_id uuid, expires_at timestamp with time zone, used_at timestamp with time zone)", "tappa_resolver", "s"},
	{"identity_password_for_session", "p_session_hash text", "TABLE(password_hash text)", "tappa_identitydefiner", "s"},
	{"identity_set_password", "p_session_hash text, p_new_hash text", "TABLE(revoked_other_sessions integer)", "tappa_identitydefiner", "v"},
	{"identity_consume_row_reset", "p_reset_hash text, p_new_hash text", "TABLE(admin_user_id uuid, tenant_id uuid, reset_id uuid, revoked_sessions integer)", "tappa_identitydefiner", "v"},
	{"identity_consume_reset", "p_reset_hash text, p_new_hash text", "TABLE(identity_id uuid, revoked_sessions integer)", "tappa_identitydefiner", "v"},
	{"identity_link_legacy", "p_identity_session_hash text, p_legacy_session_hash text", "TABLE(admin_user_id uuid, tenant_id uuid)", "tappa_identitydefiner", "v"},
	{"identity_unlink_backfilled", "p_session_hash text, p_tenant uuid", "TABLE(admin_user_id uuid, revoked_sessions integer)", "tappa_identitydefiner", "v"},
	{"identity_relink_refused", "p_session_hash text, p_tenant uuid", "TABLE(admin_user_id uuid)", "tappa_identitydefiner", "v"},
	{"identity_request_verification", "p_session_hash text, p_token_hash text", "TABLE(verification_id uuid, expires_at timestamp with time zone)", "tappa_identitydefiner", "v"},
	{"identity_verify_email", "p_verification_hash text, p_session_hash text", "TABLE(first_verification boolean)", "tappa_identitydefiner", "v"},
	{"identity_record_failed_login", "p_identity_id uuid, p_event text", "TABLE(recorded boolean)", "tappa_identitydefiner", "v"},
	{"identity_sweep_events", "", "TABLE(deleted bigint)", "tappa_identitydefiner", "v"},
}

// ipMatrix is this card's part of ADR 0031 §5.2's verb x column matrix (and T6's resolver grants):
// tappa_identitydefiner's and tappa_resolver's EXACT columns at 00072 (attnum order). M14-04's
// columns join these cells when the lanes meet.
var ipMatrix = []struct{ role, table, priv, want string }{
	{"tappa_identitydefiner", "admin_identities", "SELECT", "id,email,password_hash,email_verified_at,confirm_opened_at,confirm_open_until,confirm_scope"},
	{"tappa_identitydefiner", "admin_identities", "INSERT", ""},
	{"tappa_identitydefiner", "admin_identities", "UPDATE", "password_hash,email_verified_at,confirm_opened_at,confirm_open_until,confirm_scope"},
	{"tappa_identitydefiner", "admin_identity_resets", "SELECT", "id,identity_id,token_hash,expires_at,used_at,cancelled_at"},
	{"tappa_identitydefiner", "admin_identity_resets", "INSERT", ""},
	{"tappa_identitydefiner", "admin_identity_resets", "UPDATE", "used_at,cancelled_at"},
	{"tappa_identitydefiner", "admin_identity_verifications", "SELECT", "id,identity_id,token_hash,expires_at,used_at"},
	{"tappa_identitydefiner", "admin_identity_verifications", "INSERT", "identity_id,token_hash,expires_at"},
	{"tappa_identitydefiner", "admin_identity_verifications", "UPDATE", "used_at"},
	{"tappa_identitydefiner", "admin_identity_events", "SELECT", "created_at"},
	{"tappa_identitydefiner", "admin_identity_events", "INSERT", "identity_id,event,detail"},
	{"tappa_identitydefiner", "admin_identity_events", "UPDATE", ""},
	{"tappa_identitydefiner", "admin_users", "SELECT", "id,tenant_id,email,status,identity_id,linked_via,linked_at,unlinked_at"},
	{"tappa_identitydefiner", "admin_users", "INSERT", ""},
	{"tappa_identitydefiner", "admin_users", "UPDATE", "password_hash,status,identity_id,linked_via,linked_at,unlinked_at"},
	{"tappa_identitydefiner", "admin_sessions", "SELECT", "id,tenant_id,admin_user_id,token_hash,revoked_at,authenticated_at"},
	{"tappa_identitydefiner", "admin_sessions", "INSERT", ""},
	{"tappa_identitydefiner", "admin_sessions", "UPDATE", "revoked_at"},
	{"tappa_identitydefiner", "password_resets", "SELECT", "id,tenant_id,admin_user_id,token_hash,expires_at,used_at,cancelled_at"},
	{"tappa_identitydefiner", "password_resets", "UPDATE", "used_at,cancelled_at"},
	{"tappa_identitydefiner", "audit_log", "INSERT", "tenant_id,actor_id,action,target,detail"},
	{"tappa_identitydefiner", "tenants", "SELECT", ""},
	{"tappa_resolver", "admin_users", "SELECT", "id,tenant_id,email,password_hash,status,created_at,identity_id,unlinked_at"},
	{"tappa_resolver", "admin_identity_resets", "SELECT", "id,identity_id,token_hash,expires_at,used_at,cancelled_at"},
	{"tappa_resolver", "admin_identity_verifications", "SELECT", "id,identity_id,token_hash,expires_at,used_at"},
}

// TestIdentity00072_TheFunctionsAndTheMatrixAreTheADRs pins the signatures (T5: the redefined
// resolve_admin_by_email included -- prosecdef, proconfig, its owner and tappa_app's EXECUTE after
// the DROP + CREATE), the definer's and the resolver's columns, the table-level verbs (DELETE on the
// event log alone), that tappa_operator / tappa_opdefiner reach no function and no table here, and
// the three tables' shape (no tenant_id, ENABLE + FORCE, no policy, no sequence, nothing for
// tappa_app).
func TestIdentity00072_TheFunctionsAndTheMatrixAreTheADRs(t *testing.T) {
	ctx, tx := opTx(t)
	for _, f := range ipFunctions {
		var (
			n                                     int
			args, result, owner, config, vol, exe string
			definer                               bool
		)
		if err := tx.QueryRow(ctx, `
			SELECT count(*) OVER (), pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
			       pg_get_userbyid(p.proowner), p.prosecdef, coalesce(array_to_string(p.proconfig, ';'), ''), p.provolatile::text,
			       coalesce((SELECT string_agg(CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, ','
			                                   ORDER BY CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END)
			                   FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a WHERE a.privilege_type = 'EXECUTE'), '')
			  FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace AND p.proname = $1`, f.name).
			Scan(&n, &args, &result, &owner, &definer, &config, &vol, &exe); err != nil {
			t.Errorf("%s: %v", f.name, err)
			continue
		}
		wantExe := "tappa_app," + f.owner
		if n != 1 || args != f.args || result != f.result || owner != f.owner || !definer ||
			config != "search_path=pg_catalog, pg_temp" || vol != f.volatile || exe != wantExe {
			t.Errorf("%s: overloads=%d args=%q result=%q owner=%s definer=%v config=%q volatile=%s execute=%q;\n want 1, %q, %q, %s, true, the pinned search_path, %s, %q",
				f.name, n, args, result, owner, definer, config, vol, exe, f.args, f.result, f.owner, f.volatile, wantExe)
		}
		for _, role := range []string{"public", "tappa_operator", "tappa_opdefiner", "tappa_holdingdefiner", "tappa_feedwriter"} {
			if opInt(t, ctx, tx, `SELECT has_function_privilege($1, p.oid, 'EXECUTE')::int FROM pg_proc p
			                       WHERE p.pronamespace = 'public'::regnamespace AND p.proname = $2`, role, f.name) != 0 {
				t.Errorf("%s may EXECUTE %s (ADR 0031 §5.2: tappa_app alone)", role, f.name)
			}
		}
	}
	for _, c := range ipMatrix {
		if got := opColumns(t, ctx, tx, c.role, c.table, c.priv); got != c.want {
			t.Errorf("%s %s on %s = %q, want %q (ADR 0031 §5.2 matrix, this card's cells)", c.role, c.priv, c.table, got, c.want)
		}
	}
	for _, table := range []string{"admin_identities", "admin_identity_resets", "admin_identity_verifications", "admin_identity_events",
		"admin_users", "admin_sessions", "password_resets", "audit_log"} {
		for _, verb := range []string{"DELETE", "TRUNCATE", "TRIGGER", "REFERENCES"} {
			want := table == "admin_identity_events" && verb == "DELETE"
			if got := opInt(t, ctx, tx, `SELECT has_table_privilege('tappa_identitydefiner', ('public.' || $1)::regclass, $2)::int`, table, verb) == 1; got != want {
				t.Errorf("tappa_identitydefiner %s on %s = %v, want %v", verb, table, got, want)
			}
		}
	}
	if opInt(t, ctx, tx, `SELECT (has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'email', 'UPDATE')
	                          OR has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email', 'UPDATE'))::int`) != 0 {
		t.Error("the identity definer may UPDATE an e-mail (K-15 keeps it out of M14)")
	}

	for _, table := range []string{"admin_identity_resets", "admin_identity_verifications", "admin_identity_events"} {
		var enabled, forced bool
		if err := tx.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = ('public.' || $1)::regclass`, table).Scan(&enabled, &forced); err != nil {
			t.Fatal(err)
		}
		if !enabled || !forced {
			t.Errorf("%s: ENABLE=%v FORCE=%v, want both (CLAUDE.md §6)", table, enabled, forced)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy WHERE polrelid = ('public.' || $1)::regclass`, table); n != 0 {
			t.Errorf("%s carries %d policies", table, n)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_attribute WHERE attrelid = ('public.' || $1)::regclass AND attname = 'tenant_id' AND NOT attisdropped`, table); n != 0 {
			t.Errorf("%s has a column called tenant_id (CLAUDE.md §6: R5 and the RLS tests would read it as tenant-scoped)", table)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_depend d JOIN pg_class s ON s.oid = d.objid AND s.relkind = 'S'
		                           WHERE d.refobjid = ('public.' || $1)::regclass`, table); n != 0 {
			t.Errorf("%s owns a sequence", table)
		}
		for _, role := range []string{"public", "tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_holdingdefiner", "tappa_feedwriter"} {
			if opInt(t, ctx, tx, `SELECT (has_any_column_privilege($1, ('public.' || $2)::regclass, 'SELECT')
			                          OR has_any_column_privilege($1, ('public.' || $2)::regclass, 'INSERT')
			                          OR has_any_column_privilege($1, ('public.' || $2)::regclass, 'UPDATE')
			                          OR has_table_privilege($1, ('public.' || $2)::regclass, 'DELETE')
			                          OR has_table_privilege($1, ('public.' || $2)::regclass, 'TRUNCATE'))::int`, role, table) != 0 {
				t.Errorf("%s reaches %s (ADR 0031 §6)", role, table)
			}
		}
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", ip00072File))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^[[:space:]]*-- redline: no-tenant-scope\(([a-z0-9_.]+)\) .+`)
	var waived []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		waived = append(waived, m[1])
	}
	if strings.Join(waived, ",") != "admin_identity_resets,admin_identity_verifications,admin_identity_events" {
		t.Errorf("00072 waives %v, want the three identity tables (redline R5)", waived)
	}
}

// TestRLS_IdentityTables_TheApplicationReadsAndWritesNothing is the isolation test a
// tenant-scope-less table has (CLAUDE.md §6, §8): tappa_app, in a tenant's context, is refused on
// each of the three tables (no privilege); with the restore residue (SELECT, INSERT handed back) it
// reads 0 rows -- a row of the same tenant's identity exists -- and inserts nothing (no policy).
// The probe carries no filter at all.
func TestRLS_IdentityTables_TheApplicationReadsAndWritesNothing(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := aiTenant(t, ctx, tx)
	p := ipNewPerson(t, ctx, tx)
	admin := ipMember(t, ctx, tx, tenant, p.email, p.digest)
	ipLink(t, ctx, tx, admin, p.id, "backfill", "1 day")
	aiExec(t, ctx, tx, `INSERT INTO admin_identity_resets (identity_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, p.id, opRandHex(t))
	aiExec(t, ctx, tx, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, p.id, opRandHex(t))
	aiExec(t, ctx, tx, `INSERT INTO admin_identity_events (identity_id, event) VALUES ($1, 'login_failed')`, p.id)
	for _, table := range []string{"admin_identity_resets", "admin_identity_verifications", "admin_identity_events"} {
		opWant(t, aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, `SELECT count(*) FROM public.`+table)
			return err
		}), sqlstateInsufficientPrivi, "tappa_app reading "+table)
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `GRANT SELECT, INSERT ON public.`+table+` TO tappa_app`)
			if err := aiApp(t, ctx, sp, tenant, func(app pgx.Tx) error {
				var n int64
				if err := app.QueryRow(ctx, `SELECT count(*) FROM public.`+table).Scan(&n); err != nil {
					return err
				}
				if n != 0 {
					t.Errorf("tappa_app with the restore residue reads %d rows of %s, want 0", n, table)
				}
				return nil
			}); err != nil {
				t.Fatalf("read %s with the residue: %v", table, err)
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM public.`+table+` WHERE identity_id = $1`, p.id); n != 1 {
				t.Fatalf("PREMISE: the owner sees %d rows of the fixture identity in %s", n, table)
			}
		})
	}
}

// ipSecretScope / ipIdentityScope are the two read markers of 00072's belt (its header).
var (
	ipTenantTable  = regexp.MustCompile(`(?i)\bpublic\.(admin_users|admin_sessions|password_resets|audit_log|tenants)\b`)
	ipWriteStmt    = regexp.MustCompile(`(?i)\b(update\s+public\.(admin_users|admin_sessions|password_resets|tenants)|insert\s+into\s+public\.(audit_log|admin_users|admin_sessions|password_resets)|delete\s+from\s+public\.(admin_users|admin_sessions|password_resets|audit_log|tenants))\b`)
	ipBoundTenant  = regexp.MustCompile(`(?i)\btenant_id\s*=\s*(v_\w+|p_\w+|m\.tenant_id)\b`)
	ipAuditInsert  = regexp.MustCompile(`(?is)insert\s+into\s+public\.audit_log\s*\(\s*tenant_id\b[^)]*\)\s*values\s*\(\s*(v_\w+|p_\w+|m\.tenant_id)\b`)
	ipSecretRead   = regexp.MustCompile(`(?i)token_hash\s*=\s*p_\w+\s*/\* secret-scope \*/`)
	ipIdentityRead = regexp.MustCompile(`(?i)identity_id\s*=\s*v_identity\s*/\* identity-scope \*/`)
	ipEmailKey     = regexp.MustCompile(`(?i)email\s*(=|operator\s*\(\s*public\.=\s*\))\s*p_\w+`)
	ipChunkSplit   = regexp.MustCompile(`(?i);|\bloop\b|\bthen\b|\belse\b`)
	ipLineComment  = regexp.MustCompile(`--[^\n]*`)
)

// ipBeltFindings reads one body (prosrc) and returns each statement that breaks 00072's belt:
// a write on a tenant table without its tenant bound to a variable, a marker on a write, a read on a
// tenant table that neither binds its tenant nor is a marked secret or identity lookup, and any
// comparison of an e-mail with a parameter (no writer resolves by e-mail -- bulgu 2).
func ipBeltFindings(body string) []string {
	var out []string
	body = ipLineComment.ReplaceAllString(body, "")
	if ipEmailKey.MatchString(body) {
		out = append(out, "an e-mail compared with a parameter: "+ipEmailKey.FindString(body))
	}
	for _, chunk := range ipChunkSplit.Split(body, -1) {
		if !ipTenantTable.MatchString(chunk) {
			continue
		}
		flat := strings.Join(strings.Fields(chunk), " ")
		switch {
		case ipWriteStmt.MatchString(chunk):
			if strings.Contains(chunk, "-scope */") {
				out = append(out, "a scope marker on a write: "+flat)
			}
			if regexp.MustCompile(`(?i)insert\s+into\s+public\.audit_log`).MatchString(chunk) {
				if !ipAuditInsert.MatchString(chunk) {
					out = append(out, "an audit row whose tenant is not a variable: "+flat)
				}
			} else if !ipBoundTenant.MatchString(chunk) {
				out = append(out, "a write without its tenant: "+flat)
			}
		default:
			if !ipBoundTenant.MatchString(chunk) && !ipSecretRead.MatchString(chunk) && !ipIdentityRead.MatchString(chunk) {
				out = append(out, "a read that neither binds its tenant nor is a marked lookup: "+flat)
			}
		}
	}
	return out
}

// TestIdentity00072_EveryTenantStatementCarriesItsTenant is the card's text scan (ADR 0031 §5.2,
// §6): the bodies of this card's eleven identity_* writers, read from pg_proc, keep 00072's belt
// and never key on an e-mail parameter. The controls show the scan sees a missing predicate, a
// marker on a write and an e-mail key.
func TestIdentity00072_EveryTenantStatementCarriesItsTenant(t *testing.T) {
	ctx, tx := opTx(t)
	checked := 0
	for _, f := range ipFunctions {
		if f.owner != "tappa_identitydefiner" {
			continue
		}
		var body string
		if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE pronamespace = 'public'::regnamespace AND proname = $1`, f.name).Scan(&body); err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		for _, finding := range ipBeltFindings(body) {
			t.Errorf("%s: %s", f.name, finding)
		}
		checked++
	}
	if checked != 11 {
		t.Fatalf("anti-vacuity: scanned %d bodies, want 11", checked)
	}
	for _, c := range []struct{ what, body string }{
		{"a write with no tenant", "UPDATE public.admin_users a SET password_hash = p_new_hash WHERE a.id = m.id;"},
		{"a join is not a bound tenant", "UPDATE public.admin_sessions s SET revoked_at = now() FROM public.admin_users a WHERE s.tenant_id = a.tenant_id;"},
		{"a marker on a write", "UPDATE public.admin_users a SET x = 1 WHERE a.token_hash = p_h /* secret-scope */ AND a.tenant_id = v_tenant;"},
		{"an unmarked read", "SELECT a.id FROM public.admin_users a WHERE a.identity_id = v_identity;"},
		{"an audit row from a column", "INSERT INTO public.audit_log (tenant_id, action) SELECT a.tenant_id, 'x' FROM public.admin_users a WHERE a.tenant_id = v_t;"},
		{"an e-mail key", "SELECT i.id FROM public.admin_identities i WHERE i.email OPERATOR(public.=) p_email;"},
	} {
		if len(ipBeltFindings(c.body)) == 0 {
			t.Errorf("control %q: the scan found nothing", c.what)
		}
	}
}

// --------------------------------------------------- resolve_admin_by_email --

// TestResolveAdminByEmail00072_ReturnsTheLinkAndHidesARefusedRow is T2 / S8 / T5: the redefined
// resolver returns identity_id, keeps 00017's incumbent-first order and no LIMIT (eight linked rows
// and one unlinked row of one person are all returned, the unlinked one told apart by its NULL),
// never returns a refused row, and the binary before 00072's statement (getAdminByEmailSQL, four
// columns by name) runs on it as tappa_app.
func TestResolveAdminByEmail00072_ReturnsTheLinkAndHidesARefusedRow(t *testing.T) {
	ctx, tx := opTx(t)
	p := ipNewPerson(t, ctx, tx)
	var linked []uuid.UUID
	for i := 0; i < 8; i++ {
		tenant := aiTenant(t, ctx, tx)
		a := ipMember(t, ctx, tx, tenant, p.email, p.digest)
		aiExec(t, ctx, tx, `UPDATE admin_users SET created_at = now() - make_interval(days => $2) WHERE id = $1`, a, 20-i)
		ipLink(t, ctx, tx, a, p.id, "backfill", "1 day")
		linked = append(linked, a)
	}
	unlinkedTenant := aiTenant(t, ctx, tx)
	unlinked := ipMember(t, ctx, tx, unlinkedTenant, strings.ToUpper(p.email), p.digest)
	refusedTenant := aiTenant(t, ctx, tx)
	refused := ipMember(t, ctx, tx, refusedTenant, p.email, p.digest)
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now(), linked_via = 'backfill', linked_at = now() - interval '2 days' WHERE id = $1`, refused)

	rows, err := ipApp(t, ctx, tx, `SELECT id, identity_id FROM resolve_admin_by_email($1::citext)`, p.email)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for _, r := range rows {
		id := uuid.UUID(r[0].([16]byte))
		ids = append(ids, id)
		var identity *uuid.UUID
		if r[1] != nil {
			u := uuid.UUID(r[1].([16]byte))
			identity = &u
		}
		switch {
		case id == unlinked && identity != nil:
			t.Errorf("the unlinked row carries identity_id %v", identity)
		case id != unlinked && (identity == nil || *identity != p.id):
			t.Errorf("linked row %s carries identity_id %v, want %s", id, identity, p.id)
		}
	}
	if want := append(slices.Clone(linked), unlinked); !slices.Equal(ids, want) {
		t.Errorf("resolve_admin_by_email = %v, want the eight linked rows oldest first, then the unlinked one %v (and never the refused %s)", ids, want, refused)
	}
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		r, err := sp.Query(ctx, getAdminByEmailSQL, p.email)
		if err != nil {
			return err
		}
		defer r.Close()
		n := 0
		for r.Next() {
			var id, tenant uuid.UUID
			var hash, status string
			if err := r.Scan(&id, &tenant, &hash, &status); err != nil {
				return err
			}
			if id == refused {
				t.Error("the old binary's statement returned the refused row")
			}
			n++
		}
		if n != 9 {
			t.Errorf("the old binary's statement returned %d rows, want 9", n)
		}
		return r.Err()
	}); err != nil {
		t.Fatalf("the binary before 00072's statement on the new resolver: %v", err)
	}
}

// ------------------------------------------------------------ the password --

// TestIdentitySetPassword_WritesTheIdentityAndEveryLinkedRowOnly: a linked session writes the
// identity and both linked rows (A, C), revokes every OTHER session of both (the calling one kept),
// leaves an unlinked row of the same address (B) and its session alone, and writes one ordinary
// audit row in A and in C naming neither the other tenant nor the identity. The unlinked row's
// session -- whose address IS the identity's -- gets 0 rows and changes nothing (no e-mail
// resolution: bulgu 2's mutation), as do a revoked session and an unknown hash.
// identity_password_for_session answers the digest for a linked session, 0 rows for B's.
func TestIdentitySetPassword_WritesTheIdentityAndEveryLinkedRowOnly(t *testing.T) {
	ctx, tx := opTx(t)
	p := ipNewPerson(t, ctx, tx)
	a, b, c := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	rowA, rowB, rowC := ipMember(t, ctx, tx, a, p.email, p.digest), ipMember(t, ctx, tx, b, p.email, p.digest), ipMember(t, ctx, tx, c, p.email, p.digest)
	ipLink(t, ctx, tx, rowA, p.id, "backfill", "1 day")
	ipLink(t, ctx, tx, rowC, p.id, "password", "1 day")
	sA, sA2, sC, sB := ipSession(t, ctx, tx, a, rowA, ipNull), ipSession(t, ctx, tx, a, rowA, ipFresh), ipSession(t, ctx, tx, c, rowC, ipStale), ipSession(t, ctx, tx, b, rowB, ipFresh)
	bDigest := p.digest
	newDigest := ipDigest(t, ctx, tx, "new-"+p.pw)

	got, err := ipApp(t, ctx, tx, `SELECT password_hash FROM identity_password_for_session($1)`, sA)
	if err != nil || len(got) != 1 || got[0][0] != p.digest {
		t.Errorf("identity_password_for_session(A) = %v, %v; want the identity's digest", got, err)
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_password_for_session($1)`, sB); n != 0 {
		t.Errorf("identity_password_for_session on the unlinked row's session = %d rows, want 0", n)
	}

	for _, c := range []struct{ what, hash string }{{"the unlinked row's session (same address)", sB}, {"an unknown hash", opRandHex(t)}} {
		if n := ipRows(t, ctx, tx, `SELECT * FROM identity_set_password($1, $2)`, c.hash, newDigest); n != 0 {
			t.Errorf("identity_set_password with %s = %d rows, want 0", c.what, n)
		}
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_identities WHERE id = $1 AND password_hash = $2`, p.id, p.digest) != 1 {
		t.Fatal("a refused identity_set_password changed the identity's digest (resolved by e-mail?)")
	}

	rows, err := ipApp(t, ctx, tx, `SELECT revoked_other_sessions FROM identity_set_password($1, $2)`, sA, newDigest)
	if err != nil || len(rows) != 1 || rows[0][0] != int32(1) {
		t.Fatalf("identity_set_password(A) = %v, %v; want one row, 1 other session of A revoked", rows, err)
	}
	for _, r := range []uuid.UUID{rowA, rowC} {
		if opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE id = $1 AND password_hash = $2`, r, newDigest) != 1 {
			t.Errorf("linked row %s did not take the new digest", r)
		}
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE id = $1 AND password_hash = $2 AND identity_id IS NULL`, rowB, bDigest) != 1 {
		t.Error("the unlinked row of the same address was written or linked")
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_identities WHERE id = $1 AND password_hash = $2`, p.id, newDigest) != 1 {
		t.Error("the identity did not take the new digest")
	}
	for _, c := range []struct {
		hash string
		live bool
	}{{sA, true}, {sA2, false}, {sC, false}, {sB, true}} {
		if ipLive(t, ctx, tx, c.hash) != c.live {
			t.Errorf("session live=%v after the change, want %v", !c.live, c.live)
		}
	}
	ipInvariant(t, ctx, tx, p.id)
	for _, tenant := range []uuid.UUID{a, c} {
		rows := ipAudit(t, ctx, tx, tenant)
		if len(rows) != 1 || !strings.HasPrefix(rows[0], "admin.password_changed|") {
			t.Errorf("audit in %s = %v, want one admin.password_changed", tenant, rows)
		}
		ipAuditClean(t, rows, p.id.String(), a.String(), b.String(), c.String())
	}
	if rows := ipAudit(t, ctx, tx, b); len(rows) != 0 {
		t.Errorf("the unlinked row's tenant got audit rows %v", rows)
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_set_password($1, $2)`, sC, newDigest); n != 0 {
		t.Errorf("identity_set_password with a revoked session = %d rows, want 0", n)
	}
}

// ---------------------------------------------------------------- resets --

// ipResetWorld: identity P linked in A and C (sessions on both, one of each with a live row link),
// an unlinked row of the same address in B with its own session and link, and a live identity link.
type ipResetWorld struct {
	p                      ipPerson
	a, b, c                uuid.UUID
	rowA, rowB, rowC       uuid.UUID
	sA, sB, sC             string
	linkA, linkA2, linkB   string
	linkC, identityLink    string
	newDigest, newPassword string
}

func ipNewResetWorld(t *testing.T, ctx context.Context, tx pgx.Tx) ipResetWorld {
	t.Helper()
	w := ipResetWorld{p: ipNewPerson(t, ctx, tx)}
	w.a, w.b, w.c = aiTenant(t, ctx, tx), aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	w.rowA, w.rowB, w.rowC = ipMember(t, ctx, tx, w.a, w.p.email, w.p.digest), ipMember(t, ctx, tx, w.b, w.p.email, w.p.digest), ipMember(t, ctx, tx, w.c, w.p.email, w.p.digest)
	ipLink(t, ctx, tx, w.rowA, w.p.id, "backfill", "1 day")
	ipLink(t, ctx, tx, w.rowC, w.p.id, "invite", "1 day")
	w.sA, w.sB, w.sC = ipSession(t, ctx, tx, w.a, w.rowA, ipFresh), ipSession(t, ctx, tx, w.b, w.rowB, ipFresh), ipSession(t, ctx, tx, w.c, w.rowC, ipFresh)
	link := func(tenant, admin uuid.UUID) string {
		h := opRandHex(t)
		aiExec(t, ctx, tx, `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, tenant, admin, h)
		return h
	}
	w.linkA, w.linkA2, w.linkB, w.linkC = link(w.a, w.rowA), link(w.a, w.rowA), link(w.b, w.rowB), link(w.c, w.rowC)
	w.identityLink = opRandHex(t)
	aiExec(t, ctx, tx, `INSERT INTO admin_identity_resets (identity_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, w.p.id, w.identityLink)
	w.newPassword = "reset-" + w.p.pw
	w.newDigest = ipDigest(t, ctx, tx, w.newPassword)
	return w
}

// ipAfterReset is what both reset consumptions must leave: the identity and both linked rows on the
// new digest, the address verified, a RESET window opened now for seven days, every session of the
// linked rows revoked, every other live link of them and the identity cancelled, the unlinked row
// of B untouched (not linked, its digest, its session and its link alive), one recovery audit row
// in A and in C, none in B.
func ipAfterReset(t *testing.T, ctx context.Context, tx pgx.Tx, w ipResetWorld, spentRow, spentIdentity bool) {
	t.Helper()
	var scope string
	var verified, windowNow bool
	if err := tx.QueryRow(ctx, `SELECT email_verified_at IS NOT NULL, confirm_opened_at = now() AND confirm_open_until = now() + interval '7 days', confirm_scope
	                              FROM admin_identities WHERE id = $1 AND password_hash = $2`, w.p.id, w.newDigest).Scan(&verified, &windowNow, &scope); err != nil {
		t.Fatalf("the identity did not take the new digest: %v", err)
	}
	if !verified || !windowNow || scope != "reset" {
		t.Errorf("after a reset: verified=%v window opened now for 7 days=%v scope=%q; want true, true, reset (R2, S3, S5)", verified, windowNow, scope)
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE id = ANY ($1) AND password_hash = $2`, []uuid.UUID{w.rowA, w.rowC}, w.newDigest) != 2 {
		t.Error("a linked row did not take the new digest")
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id IS NULL AND password_hash = $2`, w.rowB, w.p.digest) != 1 {
		t.Error("the unlinked row of the same address was linked or written (a reset links nothing)")
	}
	if ipLive(t, ctx, tx, w.sA) || ipLive(t, ctx, tx, w.sC) || !ipLive(t, ctx, tx, w.sB) {
		t.Errorf("sessions live A=%v C=%v B=%v, want false, false, true", ipLive(t, ctx, tx, w.sA), ipLive(t, ctx, tx, w.sC), ipLive(t, ctx, tx, w.sB))
	}
	state := func(table, hash string) string {
		var s string
		if err := tx.QueryRow(ctx, `SELECT CASE WHEN used_at IS NOT NULL THEN 'used' WHEN cancelled_at IS NOT NULL THEN 'cancelled' ELSE 'live' END
		                              FROM `+table+` WHERE token_hash = $1`, hash).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	wantA := "cancelled"
	if spentRow {
		wantA = "used"
	}
	wantI := "cancelled"
	if spentIdentity {
		wantI = "used"
	}
	for _, c := range []struct{ table, hash, want string }{
		{"password_resets", w.linkA, wantA}, {"password_resets", w.linkA2, "cancelled"}, {"password_resets", w.linkC, "cancelled"},
		{"password_resets", w.linkB, "live"}, {"admin_identity_resets", w.identityLink, wantI},
	} {
		if got := state(c.table, c.hash); got != c.want {
			t.Errorf("%s link: %s, want %s", c.table, got, c.want)
		}
	}
	ipInvariant(t, ctx, tx, w.p.id)
	for _, tenant := range []uuid.UUID{w.a, w.c} {
		rows := ipAudit(t, ctx, tx, tenant)
		if len(rows) != 1 || !strings.HasPrefix(rows[0], "admin.recovery.completed|") {
			t.Errorf("audit in a linked tenant = %v, want one admin.recovery.completed", rows)
		}
		ipAuditClean(t, rows, w.p.id.String(), w.a.String(), w.b.String(), w.c.String())
	}
	if rows := ipAudit(t, ctx, tx, w.b); len(rows) != 0 {
		t.Errorf("the unlinked row's tenant got audit rows %v", rows)
	}
}

// TestIdentityConsumeRowReset_AnOldLinkOfALinkedRowWritesTheIdentity is ADR 0031 §5.9 from M14-08
// on (ADR 0015's change of object): the linked row's old link writes the identity and every linked
// row, issues no session, opens the reset window, and is single-use. The unlinked row's link, an
// expired link, and a disabled row's link answer 0 rows (the legacy consumption decides).
func TestIdentityConsumeRowReset_AnOldLinkOfALinkedRowWritesTheIdentity(t *testing.T) {
	ctx, tx := opTx(t)
	w := ipNewResetWorld(t, ctx, tx)
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_consume_row_reset($1, $2)`, w.linkB, w.newDigest); n != 0 {
		t.Errorf("the unlinked row's link = %d rows, want 0 (the legacy path)", n)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE password_resets SET expires_at = now() - interval '1 second', created_at = now() - interval '1 hour' WHERE token_hash = $1`, w.linkA)
		if n := ipRows(t, ctx, sp, `SELECT * FROM identity_consume_row_reset($1, $2)`, w.linkA, w.newDigest); n != 0 {
			t.Errorf("an expired link = %d rows, want 0", n)
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, w.rowA)
		if n := ipRows(t, ctx, sp, `SELECT * FROM identity_consume_row_reset($1, $2)`, w.linkA, w.newDigest); n != 0 {
			t.Errorf("a disabled row's link = %d rows, want 0", n)
		}
	})
	rows, err := ipApp(t, ctx, tx, `SELECT admin_user_id, tenant_id, revoked_sessions FROM identity_consume_row_reset($1, $2)`, w.linkA, w.newDigest)
	if err != nil || len(rows) != 1 || uuid.UUID(rows[0][0].([16]byte)) != w.rowA || uuid.UUID(rows[0][1].([16]byte)) != w.a || rows[0][2] != int32(1) {
		t.Fatalf("identity_consume_row_reset = %v, %v; want A's row, A, 1 session", rows, err)
	}
	ipAfterReset(t, ctx, tx, w, true, false)
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_consume_row_reset($1, $2)`, w.linkA, w.newDigest); n != 0 {
		t.Errorf("the same link again = %d rows, want 0 (single use)", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_sessions WHERE admin_user_id = ANY ($1) AND created_at = now() AND revoked_at IS NULL AND token_hash <> ALL ($2)`,
		[]uuid.UUID{w.rowA, w.rowC}, []string{w.sA, w.sC}); n != 0 {
		t.Errorf("the consumption issued %d sessions, want 0", n)
	}
}

// TestIdentityConsumeReset_TheIdentityLinkWritesEverythingOnce is the identity link's consumption
// (M14-14): resolve_identity_reset_by_token_hash finds the link; the consumption writes the identity
// and the linked rows, revokes, links nothing, opens the reset window; a second use, a cancelled
// link and an expired one answer 0 rows.
func TestIdentityConsumeReset_TheIdentityLinkWritesEverythingOnce(t *testing.T) {
	ctx, tx := opTx(t)
	w := ipNewResetWorld(t, ctx, tx)
	rows, err := ipApp(t, ctx, tx, `SELECT identity_id, used_at IS NULL, cancelled_at IS NULL FROM resolve_identity_reset_by_token_hash($1)`, w.identityLink)
	if err != nil || len(rows) != 1 || uuid.UUID(rows[0][0].([16]byte)) != w.p.id || rows[0][1] != true || rows[0][2] != true {
		t.Fatalf("resolve_identity_reset_by_token_hash = %v, %v", rows, err)
	}
	for _, c := range []struct{ what, set string }{
		{"a cancelled link", `cancelled_at = now()`},
		{"an expired link", `expires_at = now() - interval '1 second', created_at = now() - interval '1 hour'`},
	} {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_identity_resets SET `+c.set+` WHERE token_hash = $1`, w.identityLink)
			if n := ipRows(t, ctx, sp, `SELECT * FROM identity_consume_reset($1, $2)`, w.identityLink, w.newDigest); n != 0 {
				t.Errorf("%s = %d rows, want 0", c.what, n)
			}
		})
	}
	rows, err = ipApp(t, ctx, tx, `SELECT identity_id, revoked_sessions FROM identity_consume_reset($1, $2)`, w.identityLink, w.newDigest)
	if err != nil || len(rows) != 1 || uuid.UUID(rows[0][0].([16]byte)) != w.p.id || rows[0][1] != int32(2) {
		t.Fatalf("identity_consume_reset = %v, %v; want the identity, 2 sessions", rows, err)
	}
	ipAfterReset(t, ctx, tx, w, false, true)
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_consume_reset($1, $2)`, w.identityLink, w.newDigest); n != 0 {
		t.Errorf("the same link again = %d rows, want 0 (single use)", n)
	}
}

// ---------------------------------------------------------------- linking --

// ipLinkWorld: identity P with a linked row in A, an unlinked row of the same address in B, fresh
// sessions on both.
type ipLinkWorld struct {
	p          ipPerson
	a, b       uuid.UUID
	rowA, rowB uuid.UUID
	sA, sB     string
}

func ipNewLinkWorld(t *testing.T, ctx context.Context, tx pgx.Tx) ipLinkWorld {
	t.Helper()
	w := ipLinkWorld{p: ipNewPerson(t, ctx, tx)}
	w.a, w.b = aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	w.rowA = ipMember(t, ctx, tx, w.a, w.p.email, w.p.digest)
	w.rowB = ipMember(t, ctx, tx, w.b, w.p.email, opFakeDigest("b"))
	ipLink(t, ctx, tx, w.rowA, w.p.id, "backfill", "1 day")
	w.sA, w.sB = ipSession(t, ctx, tx, w.a, w.rowA, ipFresh), ipSession(t, ctx, tx, w.b, w.rowB, ipFresh)
	return w
}

// TestIdentityLinkLegacy_TwoFreshSessionsOrNothing is R3 / K-2: the link needs the identity's live
// session AND the legacy row's live session, both authenticated within five minutes and not in the
// future. Every 0-row case below leaves the legacy row unlinked: the arguments swapped, the same
// session twice, two linked sessions, two unlinked sessions, a different address, a stale, a NULL or
// a future authenticated_at on either side, a revoked legacy session, a refused or a disabled legacy
// row, an identity that already has a membership in the legacy tenant. A refusal with a valid
// identity session writes one link_refused event. The success: the legacy row takes the identity,
// 'password', linked_at now and the identity's digest; its session is revoked; the legacy tenant gets
// admin.membership.linked naming neither A nor the identity; A gets nothing. S6: a six-minute-old
// session refuses, a fresh one opened after re-entering the password links.
func TestIdentityLinkLegacy_TwoFreshSessionsOrNothing(t *testing.T) {
	ctx, tx := opTx(t)
	type args func(w ipLinkWorld, sp pgx.Tx) (string, string)
	session := func(sp pgx.Tx, tenant, admin uuid.UUID, auth ipAuth) string {
		return ipSession(t, ctx, sp, tenant, admin, auth)
	}
	cases := []struct {
		what  string
		args  args
		event bool
	}{
		{"the arguments swapped", func(w ipLinkWorld, _ pgx.Tx) (string, string) { return w.sB, w.sA }, false},
		{"the identity's session twice", func(w ipLinkWorld, _ pgx.Tx) (string, string) { return w.sA, w.sA }, true},
		{"the legacy session twice", func(w ipLinkWorld, _ pgx.Tx) (string, string) { return w.sB, w.sB }, false},
		{"two linked sessions", func(w ipLinkWorld, sp pgx.Tx) (string, string) {
			c := aiTenant(t, ctx, sp)
			rowC := ipMember(t, ctx, sp, c, w.p.email, w.p.digest)
			ipLink(t, ctx, sp, rowC, w.p.id, "backfill", "1 day")
			return w.sA, session(sp, c, rowC, ipFresh)
		}, true},
		{"a legacy row linked to another identity (the same address on the row)", func(w ipLinkWorld, sp pgx.Tx) (string, string) {
			q := ipNewPerson(t, ctx, sp)
			ipLink(t, ctx, sp, w.rowB, q.id, "backfill", "1 day")
			return w.sA, w.sB
		}, true},
		{"two unlinked sessions", func(w ipLinkWorld, sp pgx.Tx) (string, string) {
			c := aiTenant(t, ctx, sp)
			return session(sp, c, ipMember(t, ctx, sp, c, w.p.email, w.p.digest), ipFresh), w.sB
		}, false},
		{"a different address on the legacy row", func(w ipLinkWorld, sp pgx.Tx) (string, string) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET email = $2 WHERE id = $1`, w.rowB, ipEmail())
			return w.sA, w.sB
		}, true},
		{"a stale identity session", func(w ipLinkWorld, sp pgx.Tx) (string, string) { return session(sp, w.a, w.rowA, ipStale), w.sB }, false},
		{"a stale legacy session", func(w ipLinkWorld, sp pgx.Tx) (string, string) { return w.sA, session(sp, w.b, w.rowB, ipStale) }, true},
		{"a NULL identity authenticated_at", func(w ipLinkWorld, sp pgx.Tx) (string, string) { return session(sp, w.a, w.rowA, ipNull), w.sB }, false},
		{"a NULL legacy authenticated_at", func(w ipLinkWorld, sp pgx.Tx) (string, string) { return w.sA, session(sp, w.b, w.rowB, ipNull) }, true},
		{"a future identity authenticated_at", func(w ipLinkWorld, sp pgx.Tx) (string, string) { return session(sp, w.a, w.rowA, ipFuture), w.sB }, false},
		{"a future legacy authenticated_at", func(w ipLinkWorld, sp pgx.Tx) (string, string) { return w.sA, session(sp, w.b, w.rowB, ipFuture) }, true},
		{"a revoked legacy session", func(w ipLinkWorld, sp pgx.Tx) (string, string) {
			aiExec(t, ctx, sp, `UPDATE admin_sessions SET revoked_at = now() WHERE token_hash = $1`, w.sB)
			return w.sA, w.sB
		}, true},
		{"a refused legacy row", func(w ipLinkWorld, sp pgx.Tx) (string, string) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, w.rowB)
			return w.sA, w.sB
		}, true},
		{"a disabled legacy row", func(w ipLinkWorld, sp pgx.Tx) (string, string) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, w.rowB)
			return w.sA, w.sB
		}, true},
		{"the identity already a member of the legacy tenant", func(w ipLinkWorld, sp pgx.Tx) (string, string) {
			other := ipMember(t, ctx, sp, w.b, ipEmail(), w.p.digest)
			ipLink(t, ctx, sp, other, w.p.id, "invite", "1 day")
			return w.sA, w.sB
		}, true},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := ipNewLinkWorld(t, ctx, sp)
				x, y := c.args(w, sp)
				if n := ipRows(t, ctx, sp, `SELECT * FROM identity_link_legacy($1, $2)`, x, y); n != 0 {
					t.Errorf("identity_link_legacy = %d rows, want 0", n)
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id IS DISTINCT FROM $2`, w.rowB, w.p.id) != 1 {
					t.Error("the legacy row was linked to the identity")
				}
				events := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'link_refused' AND detail = '{}'`, w.p.id)
				if want := map[bool]int64{true: 1, false: 0}[c.event]; events != want {
					t.Errorf("link_refused events = %d, want %d", events, want)
				}
			})
		})
	}

	t.Run("two fresh sessions link", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w := ipNewLinkWorld(t, ctx, sp)
			rows, err := ipApp(t, ctx, sp, `SELECT admin_user_id, tenant_id FROM identity_link_legacy($1, $2)`, w.sA, w.sB)
			if err != nil || len(rows) != 1 || uuid.UUID(rows[0][0].([16]byte)) != w.rowB || uuid.UUID(rows[0][1].([16]byte)) != w.b {
				t.Fatalf("identity_link_legacy = %v, %v; want B's row", rows, err)
			}
			if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id = $2 AND linked_via = 'password'
			                        AND linked_at = now() AND unlinked_at IS NULL AND password_hash = $3`, w.rowB, w.p.id, w.p.digest) != 1 {
				t.Error("the legacy row is not linked with 'password', now, and the identity's digest")
			}
			if ipLive(t, ctx, sp, w.sB) || !ipLive(t, ctx, sp, w.sA) {
				t.Error("the legacy session must be revoked and the identity's kept")
			}
			ipInvariant(t, ctx, sp, w.p.id)
			rowsB := ipAudit(t, ctx, sp, w.b)
			if len(rowsB) != 1 || !strings.HasPrefix(rowsB[0], "admin.membership.linked|"+w.rowB.String()+"|") {
				t.Errorf("B's audit = %v, want one admin.membership.linked by B's row", rowsB)
			}
			ipAuditClean(t, rowsB, w.p.id.String(), w.a.String(), w.rowA.String())
			if rowsA := ipAudit(t, ctx, sp, w.a); len(rowsA) != 0 {
				t.Errorf("A's audit = %v, want nothing (the link is B's)", rowsA)
			}
		})
	})

	t.Run("S6: a six-minute session re-authenticated links", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w := ipNewLinkWorld(t, ctx, sp)
			old := ipSession(t, ctx, sp, w.a, w.rowA, ipStale)
			if n := ipRows(t, ctx, sp, `SELECT * FROM identity_link_legacy($1, $2)`, old, w.sB); n != 0 {
				t.Fatalf("the six-minute session linked (%d rows)", n)
			}
			fresh := ipSession(t, ctx, sp, w.a, w.rowA, ipFresh) // the account page re-asks the password and opens it
			if n := ipRows(t, ctx, sp, `SELECT * FROM identity_link_legacy($1, $2)`, fresh, w.sB); n != 1 {
				t.Errorf("the re-authenticated session = %d rows, want 1", n)
			}
		})
	})
}

// --------------------------------------------------------- unlink / relink --

// ipUnlinkWorld: the victim V (identity, a row in its own tenant H, fresh session sH) and the
// memberships linked to V's identity in other tenants, each with a way and a link time.
type ipUnlinkWorld struct {
	v  ipPerson
	h  uuid.UUID
	rH uuid.UUID
	sH string
}

func ipNewUnlinkWorld(t *testing.T, ctx context.Context, tx pgx.Tx) ipUnlinkWorld {
	t.Helper()
	w := ipUnlinkWorld{v: ipNewPerson(t, ctx, tx)}
	w.h = aiTenant(t, ctx, tx)
	w.rH = ipMember(t, ctx, tx, w.h, w.v.email, w.v.digest)
	ipLink(t, ctx, tx, w.rH, w.v.id, "backfill", "30 days")
	w.sH = ipSession(t, ctx, tx, w.h, w.rH, ipFresh)
	return w
}

// member adds a membership of V's identity in a new tenant, linked `via`, `ago` before now.
func (w ipUnlinkWorld) member(t *testing.T, ctx context.Context, tx pgx.Tx, via, ago string) (tenant, row uuid.UUID) {
	t.Helper()
	tenant = aiTenant(t, ctx, tx)
	row = ipMember(t, ctx, tx, tenant, w.v.email, w.v.digest)
	ipLink(t, ctx, tx, row, w.v.id, via, ago)
	return tenant, row
}

func ipUnlink(t *testing.T, ctx context.Context, tx pgx.Tx, session string, tenant uuid.UUID) int {
	t.Helper()
	return ipRows(t, ctx, tx, `SELECT * FROM identity_unlink_backfilled($1, $2)`, session, tenant)
}

// TestIdentityUnlink_TheWindowsScopeDecidesWhatIsListed is §5.8's two scopes (S3, T1): under
// first_verify a backfill / reconcile / signup link is listed and a password / claim / invite /
// account link is not; under reset EVERY link made before the window opened is listed -- the
// pre-planted attacker's password link (S3) and a claim made with a forwarded link (T1; modelled as
// an admin_users row linked 'claim', since M14-17's claim tables do not exist yet) included -- and a
// link made at or after the opening is not. The S3 / T1 scenarios run through the real reset
// consumption.
func TestIdentityUnlink_TheWindowsScopeDecidesWhatIsListed(t *testing.T) {
	ctx, tx := opTx(t)
	for _, c := range []struct {
		scope, via, ago string
		listed          bool
	}{
		{"first_verify", "backfill", "1 day", true},
		{"first_verify", "reconcile", "1 day", true},
		{"first_verify", "signup", "1 day", true},
		{"first_verify", "password", "1 day", false},
		{"first_verify", "claim", "1 day", false},
		{"first_verify", "invite", "1 day", false},
		{"first_verify", "account", "1 day", false},
		{"reset", "password", "1 day", true},
		{"reset", "claim", "1 day", true},
		{"reset", "invite", "1 day", true},
		{"reset", "backfill", "1 day", true},
		{"reset", "password", "0 seconds", false},
	} {
		t.Run(c.scope+"/"+c.via+"/"+c.ago, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w := ipNewUnlinkWorld(t, ctx, sp)
				x, row := w.member(t, ctx, sp, c.via, c.ago)
				ipOpenWindow(t, ctx, sp, w.v.id, c.scope)
				got := ipUnlink(t, ctx, sp, w.sH, x) == 1
				if got != c.listed {
					t.Errorf("unlink of a %s link (%s ago) under %s = %v, want %v", c.via, c.ago, c.scope, got, c.listed)
				}
				if unlinked := opInt(t, ctx, sp, `SELECT (identity_id IS NULL)::int FROM admin_users WHERE id = $1`, row) == 1; unlinked != c.listed {
					t.Errorf("the row is unlinked=%v, want %v", unlinked, c.listed)
				}
				ipInvariant(t, ctx, sp, w.v.id)
			})
		})
	}

	for _, c := range []struct{ what, via string }{
		{"S3: a pre-planted identity's password link", "password"},
		{"T1: a claim made with a forwarded link", "claim"},
	} {
		t.Run(c.what+" -- the victim's reset lists it", func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				// The attacker holds the identity of the victim's address (pre-planted, their password),
				// and linked a business of their own to it before the victim ever reset.
				w := ipNewUnlinkWorld(t, ctx, sp)
				x, row := w.member(t, ctx, sp, c.via, "3 days")
				link := opRandHex(t)
				aiExec(t, ctx, sp, `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, w.h, w.rH, link)
				victimPW := "victim-" + uuid.NewString()
				if n := ipRows(t, ctx, sp, `SELECT * FROM identity_consume_row_reset($1, $2)`, link, ipDigest(t, ctx, sp, victimPW)); n != 1 {
					t.Fatalf("the victim's reset = %d rows", n)
				}
				fresh := ipSession(t, ctx, sp, w.h, w.rH, ipFresh) // the victim signs in with the new password
				if n := ipUnlink(t, ctx, sp, fresh, x); n != 1 {
					t.Fatalf("the attacker's %s link is not on the victim's reset list (%d rows)", c.via, n)
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id IS NULL AND unlinked_at = now()`, row) != 1 {
					t.Error("the attacker's membership was not refused")
				}
			})
		})
	}
}

// TestIdentityUnlink_RefusesForGoodWithASentinel is R1 / S4 / S8 / S20 / T5. The 0-row cases: a
// stale, a NULL and a future authenticated_at, a closed window, an unverified address, a tenant with
// no membership of the identity. The success: identity_id NULL, unlinked_at now, linked_via and
// linked_at kept, a cost-12 bcrypt sentinel that is neither the identity's digest nor opened by the
// person's password, the row's unexpired reset link cancelled, its sessions revoked, an audit row in
// that tenant and an event. Then the refusal holds on every path: the same password through the
// legacy resolver (the row is not offered), through identity_link_legacy with a fresh session on the
// refused row (0, not reopened), the row's old link through identity_consume_row_reset (0) and
// through the legacy consumption as tappa_app (no row -- not a 500), a link minted afterwards for it
// by the owner and spent through the legacy consumption (the trigger's second arm: 23001), and the
// owner's own UPDATE of its digest (23001).
func TestIdentityUnlink_RefusesForGoodWithASentinel(t *testing.T) {
	ctx, tx := opTx(t)
	w := ipNewUnlinkWorld(t, ctx, tx)
	x, row := w.member(t, ctx, tx, "backfill", "2 days")
	sX := ipSession(t, ctx, tx, x, row, ipFresh)
	oldLink := opRandHex(t)
	aiExec(t, ctx, tx, `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, x, row, oldLink)
	ipOpenWindow(t, ctx, tx, w.v.id, "first_verify")

	for _, c := range []struct {
		what  string
		setup func(sp pgx.Tx) (string, uuid.UUID)
	}{
		{"a stale session", func(sp pgx.Tx) (string, uuid.UUID) { return ipSession(t, ctx, sp, w.h, w.rH, ipStale), x }},
		{"a NULL authenticated_at", func(sp pgx.Tx) (string, uuid.UUID) { return ipSession(t, ctx, sp, w.h, w.rH, ipNull), x }},
		{"a future authenticated_at", func(sp pgx.Tx) (string, uuid.UUID) { return ipSession(t, ctx, sp, w.h, w.rH, ipFuture), x }},
		{"a closed window", func(sp pgx.Tx) (string, uuid.UUID) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET confirm_opened_at = now() - interval '8 days', confirm_open_until = now() - interval '1 day' WHERE id = $1`, w.v.id)
			return w.sH, x
		}},
		{"an unverified address", func(sp pgx.Tx) (string, uuid.UUID) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET email_verified_at = NULL WHERE id = $1`, w.v.id)
			return w.sH, x
		}},
		{"a tenant without a membership", func(sp pgx.Tx) (string, uuid.UUID) { return w.sH, aiTenant(t, ctx, sp) }},
		{"control: the row's own fresh session", func(sp pgx.Tx) (string, uuid.UUID) { return sX, x }},
	} {
		t.Run(c.what, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				s, tenant := c.setup(sp)
				if strings.HasPrefix(c.what, "control:") {
					// sX is the row's own (linked) session: allowed in principle -- the control that the
					// fixture works, the same call succeeds.
					if n := ipUnlink(t, ctx, sp, s, tenant); n != 1 {
						t.Errorf("control: unlink through the row's own fresh session = %d rows, want 1", n)
					}
					return
				}
				if n := ipUnlink(t, ctx, sp, s, tenant); n != 0 {
					t.Errorf("unlink with %s = %d rows, want 0", c.what, n)
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id = $2 AND unlinked_at IS NULL`, row, w.v.id) != 1 {
					t.Error("the row changed")
				}
			})
		})
	}

	rows, err := ipApp(t, ctx, tx, `SELECT admin_user_id, revoked_sessions FROM identity_unlink_backfilled($1, $2)`, w.sH, x)
	if err != nil || len(rows) != 1 || uuid.UUID(rows[0][0].([16]byte)) != row || rows[0][1] != int32(1) {
		t.Fatalf("identity_unlink_backfilled = %v, %v; want the row, 1 session", rows, err)
	}
	var sentinel, via string
	if err := tx.QueryRow(ctx, `SELECT password_hash, linked_via FROM admin_users
	                             WHERE id = $1 AND identity_id IS NULL AND unlinked_at = now() AND linked_at = now() - interval '2 days'`, row).Scan(&sentinel, &via); err != nil {
		t.Fatalf("the refused row is not identity NULL, unlinked now, its linked_at kept: %v", err)
	}
	if via != "backfill" || !regexp.MustCompile(`^\$2a\$12\$[./A-Za-z0-9]{53}$`).MatchString(sentinel) || sentinel == w.v.digest || ipOpens(t, ctx, tx, w.v.pw, sentinel) {
		t.Errorf("sentinel: via=%s shape-ok=%v equals-identity=%v opened-by-the-password=%v; want backfill, a cost-12 bcrypt, false, false (S20)",
			via, regexp.MustCompile(`^\$2a\$12\$`).MatchString(sentinel), sentinel == w.v.digest, ipOpens(t, ctx, tx, w.v.pw, sentinel))
	}
	if ipLive(t, ctx, tx, sX) || !ipLive(t, ctx, tx, w.sH) {
		t.Error("the refused row's session must be revoked and the calling one kept")
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM password_resets WHERE token_hash = $1 AND cancelled_at = now() AND used_at IS NULL`, oldLink) != 1 {
		t.Error("the refused row's unexpired link was not cancelled (T5)")
	}
	audit := ipAudit(t, ctx, tx, x)
	if len(audit) != 1 || !strings.HasPrefix(audit[0], "admin.membership.unlinked|"+row.String()+"|") {
		t.Errorf("X's audit = %v, want one admin.membership.unlinked", audit)
	}
	ipAuditClean(t, audit, w.v.id.String(), w.h.String())
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'membership_unlinked'
	                           AND detail = jsonb_build_object('tenant', $2::text, 'scope', 'first_verify')`, w.v.id, x); n != 1 {
		t.Errorf("membership_unlinked events = %d, want 1", n)
	}
	ipInvariant(t, ctx, tx, w.v.id)

	// The refusal holds.
	got, err := ipApp(t, ctx, tx, `SELECT id FROM resolve_admin_by_email($1::citext)`, w.v.email)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if uuid.UUID(r[0].([16]byte)) == row {
			t.Error("the legacy resolver offers the refused row to the next login (S8)")
		}
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_link_legacy($1, $2)`, w.sH, ipSession(t, ctx, tx, x, row, ipFresh)); n != 0 {
		t.Errorf("auto-link of the refused row with the same password = %d rows, want 0 (R1)", n)
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id IS NULL AND unlinked_at IS NOT NULL AND password_hash = $2`, row, sentinel) != 1 {
		t.Error("the refused row was reopened")
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_consume_row_reset($1, $2)`, oldLink, ipDigest(t, ctx, tx, "x")); n != 0 {
		t.Errorf("the refused row's old link through the identity = %d rows, want 0", n)
	}
	err = aiApp(t, ctx, tx, x, func(sp pgx.Tx) error {
		_, err := store.New(sp).ConsumePasswordResetAndSetPassword(ctx, store.ConsumePasswordResetAndSetPasswordParams{
			PasswordHash: opFakeDigest("z"), TenantID: x, TokenHash: oldLink})
		return err
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("the refused row's old link through the legacy consumption: %v, want no row (a uniform refusal, not a 500 -- T5)", err)
	}
	minted := opRandHex(t)
	aiExec(t, ctx, tx, `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, x, row, minted)
	opWant(t, aiApp(t, ctx, tx, x, func(sp pgx.Tx) error {
		_, err := store.New(sp).ConsumePasswordResetAndSetPassword(ctx, store.ConsumePasswordResetAndSetPasswordParams{
			PasswordHash: opFakeDigest("z"), TenantID: x, TokenHash: minted})
		return err
	}), sqlstateRestrictViolation, "tappa_app spending a link minted for a refused row (the trigger's second arm, S8)")
	opWant(t, opTry(t, ctx, tx, `UPDATE admin_users SET password_hash = $2 WHERE id = $1`, row, w.v.digest),
		sqlstateRestrictViolation, "the owner writing a usable digest over the sentinel")
}

// TestIdentityUnlink_AfterContractBItDisablesTheMembership is T1 / ONAY notu 2 with a stand-in for
// contract B's VALID CHECK (a row born after the contract moment must keep identity_id): the unlink
// keeps the link, stamps unlinked_at and disables the row; the sessions are revoked as before.
func TestIdentityUnlink_AfterContractBItDisablesTheMembership(t *testing.T) {
	ctx, tx := opTx(t)
	w := ipNewUnlinkWorld(t, ctx, tx)
	x, row := w.member(t, ctx, tx, "backfill", "2 days")
	sX := ipSession(t, ctx, tx, x, row, ipFresh)
	ipOpenWindow(t, ctx, tx, w.v.id, "first_verify")
	var moment string
	if err := tx.QueryRow(ctx, `SELECT now()::text`).Scan(&moment); err != nil {
		t.Fatal(err)
	}
	aiExec(t, ctx, tx, `ALTER TABLE admin_users ADD CONSTRAINT zz_m14_05_contract_b
	                    CHECK (identity_id IS NOT NULL OR created_at < '`+moment+`'::timestamptz) NOT VALID`)
	if n := ipUnlink(t, ctx, tx, w.sH, x); n != 1 {
		t.Fatalf("the post-contract unlink = %d rows, want 1", n)
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id = $2 AND unlinked_at = now() AND status = 'disabled'`, row, w.v.id) != 1 {
		t.Error("after contract B the unlink must keep the link, stamp unlinked_at and disable the membership")
	}
	if ipLive(t, ctx, tx, sX) {
		t.Error("the disabled membership's session is live")
	}
	ipInvariant(t, ctx, tx, w.v.id)
}

// TestIdentityRelink_OnlyThisWindowsRefusal is S4 / T4: inside the window a refused row is linked
// again ('account', its ORIGINAL linked_at, unlinked_at NULL, the identity's digest) and stays on the
// reset list (it can be unlinked again); the 0-row cases: a stale, a NULL and a future session, a
// closed window, a row refused BEFORE this window opened, another membership of the identity in that
// tenant, another tenant.
func TestIdentityRelink_OnlyThisWindowsRefusal(t *testing.T) {
	ctx, tx := opTx(t)
	setup := func(sp pgx.Tx) (ipUnlinkWorld, uuid.UUID, uuid.UUID) {
		w := ipNewUnlinkWorld(t, ctx, sp)
		x, row := w.member(t, ctx, sp, "password", "2 days")
		ipOpenWindow(t, ctx, sp, w.v.id, "reset")
		if n := ipUnlink(t, ctx, sp, w.sH, x); n != 1 {
			t.Fatalf("PREMISE: unlink = %d rows", n)
		}
		return w, x, row
	}
	for _, c := range []struct {
		what string
		do   func(sp pgx.Tx, w ipUnlinkWorld, x uuid.UUID) (string, uuid.UUID)
	}{
		{"a stale session", func(sp pgx.Tx, w ipUnlinkWorld, x uuid.UUID) (string, uuid.UUID) {
			return ipSession(t, ctx, sp, w.h, w.rH, ipStale), x
		}},
		{"a NULL authenticated_at", func(sp pgx.Tx, w ipUnlinkWorld, x uuid.UUID) (string, uuid.UUID) {
			return ipSession(t, ctx, sp, w.h, w.rH, ipNull), x
		}},
		{"a future authenticated_at", func(sp pgx.Tx, w ipUnlinkWorld, x uuid.UUID) (string, uuid.UUID) {
			return ipSession(t, ctx, sp, w.h, w.rH, ipFuture), x
		}},
		{"a closed window", func(sp pgx.Tx, w ipUnlinkWorld, x uuid.UUID) (string, uuid.UUID) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET confirm_opened_at = now() - interval '8 days', confirm_open_until = now() - interval '1 day' WHERE id = $1`, w.v.id)
			return w.sH, x
		}},
		{"a row refused before this window", func(sp pgx.Tx, w ipUnlinkWorld, x uuid.UUID) (string, uuid.UUID) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() - interval '1 second' WHERE tenant_id = $1 AND unlinked_at IS NOT NULL`, x)
			return w.sH, x
		}},
		{"another membership of the identity in that tenant", func(sp pgx.Tx, w ipUnlinkWorld, x uuid.UUID) (string, uuid.UUID) {
			other := ipMember(t, ctx, sp, x, ipEmail(), w.v.digest)
			ipLink(t, ctx, sp, other, w.v.id, "invite", "1 hour")
			return w.sH, x
		}},
		{"another tenant", func(sp pgx.Tx, w ipUnlinkWorld, x uuid.UUID) (string, uuid.UUID) { return w.sH, w.h }},
	} {
		t.Run(c.what, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				w, x, row := setup(sp)
				s, tenant := c.do(sp, w, x)
				if n := ipRows(t, ctx, sp, `SELECT * FROM identity_relink_refused($1, $2)`, s, tenant); n != 0 {
					t.Errorf("relink with %s = %d rows, want 0", c.what, n)
				}
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id IS NULL AND unlinked_at IS NOT NULL`, row) != 1 {
					t.Error("the refused row changed")
				}
			})
		})
	}
	t.Run("inside the window", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			w, x, row := setup(sp)
			if n := ipRows(t, ctx, sp, `SELECT * FROM identity_relink_refused($1, $2)`, w.sH, x); n != 1 {
				t.Fatalf("relink = %d rows, want 1", n)
			}
			if opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id = $2 AND linked_via = 'account'
			                        AND linked_at = now() - interval '2 days' AND unlinked_at IS NULL AND password_hash = $3`, row, w.v.id, w.v.digest) != 1 {
				t.Error("the row is not linked again with 'account', its original linked_at and the identity's digest")
			}
			ipInvariant(t, ctx, sp, w.v.id)
			audit := ipAudit(t, ctx, sp, x)
			ipAuditClean(t, audit, w.v.id.String(), w.h.String())
			if !slices.ContainsFunc(audit, func(s string) bool { return strings.HasPrefix(s, "admin.membership.relinked|") }) {
				t.Errorf("X's audit = %v, want an admin.membership.relinked", audit)
			}
			if n := ipUnlink(t, ctx, sp, w.sH, x); n != 1 {
				t.Errorf("the relinked row is no longer on the list (unlink again = %d rows, want 1 -- T4)", n)
			}
		})
	})
}

// ---------------------------------------------------------- verification --

// TestIdentityVerifyEmail_TheSameIdentitysSessionInTheClickingBrowser is R2 / S16: the link is
// bound to the identity that asked; a click with another identity's session, an unlinked session of
// the same address, no session, an expired link -> 0 rows and nothing verified; a click with a NEW
// session of the same identity in another business (a switch between request and click) verifies,
// opens a first_verify window and writes one event; the link is single-use; a verified identity gets
// no new link; the resolver returns the link.
func TestIdentityVerifyEmail_TheSameIdentitysSessionInTheClickingBrowser(t *testing.T) {
	ctx, tx := opTx(t)
	p, other := ipNewPerson(t, ctx, tx), ipNewPerson(t, ctx, tx)
	a, b := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	rowA, rowB := ipMember(t, ctx, tx, a, p.email, p.digest), ipMember(t, ctx, tx, b, p.email, p.digest)
	ipLink(t, ctx, tx, rowA, p.id, "backfill", "1 day")
	ipLink(t, ctx, tx, rowB, p.id, "backfill", "1 day")
	legacy := ipMember(t, ctx, tx, aiTenant(t, ctx, tx), p.email, p.digest)
	otherRow := ipMember(t, ctx, tx, a, other.email, other.digest)
	ipLink(t, ctx, tx, otherRow, other.id, "backfill", "1 day")
	sA := ipSession(t, ctx, tx, a, rowA, ipNull)

	token := opRandHex(t)
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_request_verification($1, $2)`, opRandHex(t), opRandHex(t)); n != 0 {
		t.Errorf("a request with an unknown session = %d rows, want 0", n)
	}
	rows, err := ipApp(t, ctx, tx, `SELECT verification_id, expires_at = now() + interval '24 hours' FROM identity_request_verification($1, $2)`, sA, token)
	if err != nil || len(rows) != 1 || rows[0][1] != true {
		t.Fatalf("identity_request_verification = %v, %v", rows, err)
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_identity_verifications WHERE token_hash = $1 AND identity_id = $2`, token, p.id) != 1 {
		t.Fatal("the link is not bound to the identity that asked")
	}
	if got, err := ipApp(t, ctx, tx, `SELECT identity_id FROM resolve_identity_verification_by_token_hash($1)`, token); err != nil || len(got) != 1 || uuid.UUID(got[0][0].([16]byte)) != p.id {
		t.Errorf("resolve_identity_verification_by_token_hash = %v, %v", got, err)
	}
	// The business switch: A's session revoked, a new one in B.
	aiExec(t, ctx, tx, `UPDATE admin_sessions SET revoked_at = now() WHERE token_hash = $1`, sA)
	sB := ipSession(t, ctx, tx, b, rowB, ipNull)

	for _, c := range []struct{ what, session string }{
		{"another identity's session", ipSession(t, ctx, tx, a, otherRow, ipFresh)},
		{"an unlinked session of the same address", ipSession(t, ctx, tx, tenantOf(t, ctx, tx, legacy), legacy, ipFresh)},
		{"no session (another browser)", opRandHex(t)},
		{"the revoked session that asked", sA},
	} {
		if n := ipRows(t, ctx, tx, `SELECT * FROM identity_verify_email($1, $2)`, token, c.session); n != 0 {
			t.Errorf("a click with %s = %d rows, want 0 (R2)", c.what, n)
		}
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET expires_at = now() - interval '1 second', created_at = now() - interval '1 hour' WHERE token_hash = $1`, token)
		if n := ipRows(t, ctx, sp, `SELECT * FROM identity_verify_email($1, $2)`, token, sB); n != 0 {
			t.Errorf("an expired link = %d rows, want 0", n)
		}
	})
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at IS NULL AND confirm_scope IS NULL`, p.id) != 1 {
		t.Fatal("a refused click verified the identity")
	}
	rows, err = ipApp(t, ctx, tx, `SELECT first_verification FROM identity_verify_email($1, $2)`, token, sB)
	if err != nil || len(rows) != 1 || rows[0][0] != true {
		t.Fatalf("the click with the same identity's session after a switch = %v, %v; want first_verification (S16)", rows, err)
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at = now() AND confirm_scope = 'first_verify'
	                       AND confirm_opened_at = now() AND confirm_open_until = now() + interval '7 days'`, p.id) != 1 {
		t.Error("the first verification did not open a first_verify window")
	}
	if opInt(t, ctx, tx, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'email_verified'`, p.id) != 1 {
		t.Error("no email_verified event")
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_verify_email($1, $2)`, token, sB); n != 0 {
		t.Errorf("the same link again = %d rows, want 0", n)
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_request_verification($1, $2)`, sB, opRandHex(t)); n != 0 {
		t.Errorf("a verified identity got a new link (%d rows)", n)
	}
	ipInvariant(t, ctx, tx, p.id, other.id)
}

func tenantOf(t *testing.T, ctx context.Context, q pgx.Tx, admin uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := q.QueryRow(ctx, `SELECT tenant_id FROM admin_users WHERE id = $1`, admin).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// ---------------------------------------------------------------- events --

// TestIdentityEvents_TheFailedLoginWritesOneRowAndTheSweepNinetyDays is R9 / R17: the named
// exception writes ONE admin_identity_events row (login_failed / login_limited, detail empty) and
// nothing else -- no audit row, no other table -- and 0 rows for an unknown identity or another
// event; the detail CHECK refuses an address, an IP-shaped key and a malformed tenant; the sweep
// deletes what is older than 90 days and nothing younger, and is the only delete (tappa_app holds
// no DELETE).
func TestIdentityEvents_TheFailedLoginWritesOneRowAndTheSweepNinetyDays(t *testing.T) {
	ctx, tx := opTx(t)
	p := ipNewPerson(t, ctx, tx)
	snapshot := func() string {
		var s string
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_log)::text || '/' || (SELECT count(*) FROM admin_users)::text || '/' ||
		                                   (SELECT count(*) FROM admin_sessions)::text || '/' || (SELECT md5(string_agg(password_hash || coalesce(email_verified_at::text, ''), ',' ORDER BY id)) FROM admin_identities)`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := snapshot()
	for _, ev := range []string{"login_failed", "login_limited"} {
		if n := ipRows(t, ctx, tx, `SELECT * FROM identity_record_failed_login($1, $2)`, p.id, ev); n != 1 {
			t.Errorf("record %s = %d rows, want 1", ev, n)
		}
	}
	for _, c := range []struct {
		id uuid.UUID
		ev string
	}{{uuid.New(), "login_failed"}, {p.id, "membership_unlinked"}, {p.id, "email_verified"}} {
		if n := ipRows(t, ctx, tx, `SELECT * FROM identity_record_failed_login($1, $2)`, c.id, c.ev); n != 0 {
			t.Errorf("record %s for %s = %d rows, want 0", c.ev, c.id, n)
		}
	}
	if after := snapshot(); after != before {
		t.Errorf("identity_record_failed_login touched another table: %s -> %s", before, after)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND detail = '{}'`, p.id); n != 2 {
		t.Errorf("%d event rows, want 2 with an empty detail", n)
	}
	for _, d := range []string{`{"email": "x@y.example"}`, `{"ip": "203.0.113.7"}`, `{"tenant": "not-a-uuid"}`, `{"scope": "claim"}`, `[]`} {
		if code, name := m11Constraint(opTry(t, ctx, tx, `INSERT INTO admin_identity_events (identity_id, event, detail) VALUES ($1, 'login_failed', $2::jsonb)`, p.id, d)); code != sqlstateCheckViolation || name != "admin_identity_events_detail_closed" {
			t.Errorf("detail %s: %s on %q, want 23514 on admin_identity_events_detail_closed", d, code, name)
		}
	}

	aiExec(t, ctx, tx, `INSERT INTO admin_identity_events (identity_id, event, created_at) VALUES ($1, 'login_failed', now() - interval '91 days'), ($1, 'login_failed', now() - interval '89 days')`, p.id)
	rows, err := ipApp(t, ctx, tx, `SELECT deleted FROM identity_sweep_events()`)
	if err != nil || len(rows) != 1 || rows[0][0].(int64) < 1 {
		t.Fatalf("identity_sweep_events = %v, %v", rows, err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND created_at < now() - interval '90 days'`, p.id); n != 0 {
		t.Error("the 91-day row survived the sweep")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1`, p.id); n != 3 {
		t.Errorf("%d rows after the sweep, want 3 (the 89-day row and the two of today)", n)
	}
}

// ------------------------------------------------------------- Down / Up --

// ipCatalogue is what 00072 changes, as text: its functions and resolve_admin_by_email (signature,
// owner, ACL, body md5), the three tables, and every tappa_* role's effective column privileges on
// the tables its grants name.
func ipCatalogue(t *testing.T, ctx context.Context, q pgx.Tx) string {
	t.Helper()
	var b strings.Builder
	for _, sql := range []string{
		`SELECT coalesce(string_agg(p.oid::regprocedure::text || ' ' || pg_get_function_result(p.oid) || ' ' || pg_get_userbyid(p.proowner) || ' ' ||
		                            coalesce(p.proacl::text, '-') || ' ' || md5(p.prosrc), E'\n' ORDER BY p.oid::regprocedure::text), '')
		   FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace
		    AND (p.proname LIKE 'identity\_%' OR p.proname LIKE 'resolve\_identity\_%' OR p.proname = 'resolve_admin_by_email')`,
		`SELECT coalesce(string_agg(relname, ',' ORDER BY relname), '') FROM pg_class
		  WHERE relnamespace = 'public'::regnamespace AND relname LIKE 'admin\_identity\_%'`,
		`SELECT coalesce(string_agg(r.rolname || ' ' || c.relname || '.' || a.attname || ' ' ||
		          CASE WHEN has_column_privilege(r.oid, c.oid, a.attnum, 'SELECT') THEN 'S' ELSE '' END ||
		          CASE WHEN has_column_privilege(r.oid, c.oid, a.attnum, 'INSERT') THEN 'I' ELSE '' END ||
		          CASE WHEN has_column_privilege(r.oid, c.oid, a.attnum, 'UPDATE') THEN 'U' ELSE '' END, E'\n' ORDER BY r.rolname, c.relname, a.attnum), '')
		   FROM pg_roles r CROSS JOIN pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		  WHERE r.rolname ~ '^tappa_' AND NOT r.rolsuper AND c.relnamespace = 'public'::regnamespace
		    AND c.relname IN ('admin_users', 'admin_sessions', 'password_resets', 'audit_log', 'admin_identities')
		    AND (has_column_privilege(r.oid, c.oid, a.attnum, 'SELECT') OR has_column_privilege(r.oid, c.oid, a.attnum, 'INSERT')
		         OR has_column_privilege(r.oid, c.oid, a.attnum, 'UPDATE'))`,
	} {
		var s string
		if err := q.QueryRow(ctx, sql).Scan(&s); err != nil {
			t.Fatalf("read the catalogue: %v", err)
		}
		b.WriteString(s + "\n--\n")
	}
	return b.String()
}

// TestIdentity00072_DownRefusesWhileItHoldsSomethingAndOtherwiseWalksBackAndUp: with an event row
// (or a refused membership) the Down is 55000 naming 00072 and changes nothing; without, it gives
// resolve_admin_by_email back with 00017's four columns and grants, drops every identity function
// and table, takes away every grant 00072 ALONE gives and keeps the ones 00071 gives too, taking no
// strong lock outside the tables it names; the Up gives HEAD's catalogue back byte for byte.
func TestIdentity00072_DownRefusesWhileItHoldsSomethingAndOtherwiseWalksBackAndUp(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ip00072File)
	tables := downTables(down)
	slices.Sort(tables)
	tables = slices.Compact(tables)
	for _, want := range []string{"admin_identities", "admin_identity_events", "admin_identity_resets", "admin_identity_verifications", "admin_users"} {
		if !slices.Contains(tables, want) {
			t.Fatalf("PREMISE: downTables found %v in 00072's Down, not %s", tables, want)
		}
	}
	lockForDowns(t, ctx, tx, tables)
	head := ipCatalogue(t, ctx, tx)

	for _, c := range []struct{ what, setup string }{
		{"an identity event", `INSERT INTO admin_identity_events (identity_id, event) SELECT id, 'login_failed' FROM admin_identities LIMIT 1`},
		{"a refused membership", `UPDATE admin_users SET unlinked_at = now() WHERE id = (SELECT id FROM admin_users LIMIT 1)`},
	} {
		t.Run(c.what, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities`) == 0 {
					ipNewPerson(t, ctx, sp)
				}
				aiExec(t, ctx, sp, c.setup)
				err := holdingNestedRun(t, ctx, sp, down)
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != sqlstatePrerequisiteState || !strings.Contains(pg.Message, "migration 00072 Down") {
					t.Fatalf("the Down with %s answered %v, want 55000 naming 00072", c.what, err)
				}
				if after := ipCatalogue(t, ctx, sp); after != head {
					t.Error("the refused Down changed the catalogue")
				}
			})
		})
	}

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		for _, tbl := range []string{"admin_identity_events", "admin_identity_verifications", "admin_identity_resets"} {
			aiExec(t, ctx, sp, `DELETE FROM public.`+tbl)
		}
		aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = NULL WHERE unlinked_at IS NOT NULL`)
		opRunSection(t, ctx, sp, down, "00072 Down")
		at70 := ipCatalogue(t, ctx, sp)
		for _, gone := range []string{"identity_password_for_session", "identity_sweep_events", "resolve_identity_", "admin_identity_",
			"tappa_identitydefiner admin_users.email", "tappa_identitydefiner admin_users.linked_via", "tappa_identitydefiner admin_users.password_hash",
			"tappa_identitydefiner admin_users.identity_id SU", "tappa_identitydefiner admin_users.status SU", "tappa_identitydefiner password_resets.",
			"tappa_identitydefiner admin_identities.email", "tappa_identitydefiner admin_identities.password_hash", "tappa_identitydefiner admin_identities.confirm_"} {
			if strings.Contains(at70, gone) {
				t.Errorf("after 00072's Down the catalogue still holds %q (a grant 00072 alone gives)", gone)
			}
		}
		// The grants 00071 (M14-04) gives too stay: revoking them would break a database walked down
		// to 71 (column privileges are not counted).
		for _, kept := range []string{"tappa_identitydefiner admin_sessions.revoked_at SU\n", "tappa_identitydefiner admin_sessions.authenticated_at S\n",
			"tappa_identitydefiner audit_log.detail I\n", "tappa_identitydefiner admin_users.identity_id S\n", "tappa_identitydefiner admin_users.unlinked_at S\n",
			"tappa_identitydefiner admin_identities.id S\n", "tappa_resolver admin_users.identity_id S\n", "tappa_resolver admin_users.unlinked_at S\n"} {
			if !strings.Contains(at70, kept) {
				t.Errorf("after 00072's Down the catalogue lost %q, which 00071 grants too", strings.TrimSpace(kept))
			}
		}
		if !strings.Contains(at70, "resolve_admin_by_email(citext) TABLE(id uuid, tenant_id uuid, password_hash text, status text) tappa_resolver ") ||
			opInt(t, ctx, sp, `SELECT (p.prosecdef AND has_function_privilege('tappa_app', p.oid, 'EXECUTE')
			                          AND NOT has_function_privilege('public', p.oid, 'EXECUTE'))::int
			                     FROM pg_proc p WHERE p.oid = 'public.resolve_admin_by_email(citext)'::regprocedure`) != 1 {
			t.Errorf("after 00072's Down resolve_admin_by_email is not 00017's (four columns, definer, tappa_app alone):\n%s", at70)
		}
		if extra := strongTableLocksOutside(t, ctx, sp, tables); len(extra) > 0 {
			t.Errorf("the Down took a strong lock on %v, outside the tables it names", extra)
		}
		opRunSection(t, ctx, sp, up, "00072 Up again")
		if again := ipCatalogue(t, ctx, sp); again != head {
			t.Errorf("after the Up again:\n%s\nwant HEAD's\n%s", again, head)
		}
	})
}
