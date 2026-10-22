package db

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
)

// adminidentity_test.go -- migrations 00069 and 00070 (M14-03, ADR 0031 §5.1) against real
// Postgres: the tenant-scope-less admin_identities table, the four link columns on admin_users, the
// session's authenticated_at, tappa_app's narrowed reach, the INVOKER trigger that keeps a linked or
// refused row's credentials in tappa_identitydefiner's hands, the RLS isolation of an identity's
// memberships, the binary before 00069 on the new schema, and the Down / Up walk with the role's
// fresh-pod block.
//
// 🔴 NOTHING HERE COMMITS AN IDENTITY OR A LINK. Every test runs in opTx's transaction, rolled
// back: 00069's Down refuses while admin_identities holds a row (a committed fixture would make the
// Down walk below -- and a real `goose down` on a development database -- refuse forever).

const (
	ai00069File = "00069_add_admin_identities.sql"
	ai00070File = "00070_index_admin_memberships_by_identity.sql"
	aiTrigger   = "admin_users_identity_credentials"
	aiFunction  = "admin_users_guard_identity_credentials()"
)

// aiAppSelect is ADR 0031 §5.1's column list (S9): what tappa_app reads of admin_users.
const aiAppSelect = "id,tenant_id,full_name,email,password_hash,role,status,created_at,last_login_at,identity_id"

// ---------------------------------------------------------------- fixtures --

func aiExec(t *testing.T, ctx context.Context, q pgx.Tx, sql string, args ...any) {
	t.Helper()
	if _, err := q.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// aiTenant inserts a tenant as the owner (the feed's counter trigger sets its own context).
func aiTenant(t *testing.T, ctx context.Context, q pgx.Tx) uuid.UUID {
	t.Helper()
	id := uuid.New()
	aiExec(t, ctx, q, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
	                   VALUES ($1, 'm14 identity fixture', $2, 'restaurant', 'single')`, id, "VAT-M14I-"+id.String())
	return id
}

// aiAdmin inserts an unlinked admin_users row as the owner.
func aiAdmin(t *testing.T, ctx context.Context, q pgx.Tx, tenant uuid.UUID, email string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	aiExec(t, ctx, q, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
	                   VALUES ($1, $2, 'M14 Admin', $3, $4, 'owner')`, id, tenant, email, opFakeDigest("a"))
	return id
}

// aiIdentity inserts an identity as the owner (no application path writes one before M14-04).
func aiIdentity(t *testing.T, ctx context.Context, q pgx.Tx, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := q.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, opFakeDigest("a")).Scan(&id); err != nil {
		t.Fatalf("insert an identity: %v", err)
	}
	return id
}

// aiLink links a row the way M14-06's backfill will: identity_id, linked_via and linked_at together.
// The row is unlinked before, so the trigger's WHEN is false and it does not fire.
func aiLink(t *testing.T, ctx context.Context, q pgx.Tx, admin, identity uuid.UUID) {
	t.Helper()
	aiExec(t, ctx, q, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() WHERE id = $1`, admin, identity)
}

func aiEmail() string { return "m14-" + uuid.NewString() + "@identity.example" }

// aiApp runs fn as tappa_app in tenant's context (no context for uuid.Nil), inside opAs's
// savepoint; the GUC is cleared again before the savepoint is released.
func aiApp(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, fn func(sp pgx.Tx) error) error {
	t.Helper()
	return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		guc := ""
		if tenant != uuid.Nil {
			guc = tenant.String()
		}
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, guc); err != nil {
			return err
		}
		if err := fn(sp); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', '', true)`)
		return err
	})
}

// aiRows runs one statement and returns the rows it changed, or its error.
func aiRows(ctx context.Context, sp pgx.Tx, sql string, args ...any) (int64, error) {
	tag, err := sp.Exec(ctx, sql, args...)
	return tag.RowsAffected(), err
}

// aiSavepoint runs body in a savepoint of tx that is ALWAYS rolled back (grants a case needs, rows
// it writes).
func aiSavepoint(t *testing.T, ctx context.Context, tx pgx.Tx, body func(sp pgx.Tx)) {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	defer func() {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
	}()
	body(sp)
}

// ------------------------------------------------------ the identity table --

// TestAdminIdentities00069_TheTableIsTheADRsAndScopeLess pins ADR 0031 §5.1's table: exactly its
// columns (no status, nothing called tenant_id), ENABLE and FORCE with no policy, no sequence, the
// citext UNIQUE (a case variant is the same address), the bcrypt CHECK, the confirmation window's
// shape, and no role -- PUBLIC, tappa_app, the operator's two, the resolver, the other definers,
// the identity definer itself -- reaching it at 00069. The restore residue (01-roles.sql's default
// hands tappa_app SELECT/INSERT on a table a pg_dump re-creates) reads and writes nothing.
func TestAdminIdentities00069_TheTableIsTheADRsAndScopeLess(t *testing.T) {
	ctx, tx := opTx(t)

	var cols string
	if err := tx.QueryRow(ctx, `
		SELECT string_agg(a.attname || ' ' || format_type(a.atttypid, a.atttypmod) || CASE WHEN a.attnotnull THEN ' notnull' ELSE '' END, ',' ORDER BY a.attnum)
		  FROM pg_attribute a WHERE a.attrelid = 'public.admin_identities'::regclass AND a.attnum > 0 AND NOT a.attisdropped`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	const wantCols = "id uuid notnull,email citext notnull,password_hash text notnull,email_verified_at timestamp with time zone," +
		"confirm_opened_at timestamp with time zone,confirm_open_until timestamp with time zone,confirm_scope text," +
		"last_used_tenant uuid,created_at timestamp with time zone notnull,last_login_at timestamp with time zone"
	if cols != wantCols {
		t.Errorf("admin_identities columns:\n %s\nwant (ADR 0031 §5.1 -- no status, no tenant_id)\n %s", cols, wantCols)
	}

	var enabled, forced bool
	if err := tx.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.admin_identities'::regclass`).Scan(&enabled, &forced); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced {
		t.Errorf("admin_identities: ENABLE=%v FORCE=%v; CLAUDE.md §6 wants both on a tenant-scope-less table", enabled, forced)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy WHERE polrelid = 'public.admin_identities'::regclass`); n != 0 {
		t.Errorf("admin_identities carries %d policies; a policy would be a door for some role that obeys RLS", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_depend d JOIN pg_class s ON s.oid = d.objid AND s.relkind = 'S'
	                            WHERE d.refobjid = 'public.admin_identities'::regclass`); n != 0 {
		t.Errorf("admin_identities owns %d sequences; a sequence carries the default `rU` a table REVOKE does not reach", n)
	}

	for _, role := range []string{"public", "tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_resolver",
		"tappa_holdingdefiner", "tappa_feedwriter", "tappa_identitydefiner"} {
		var reach bool
		if err := tx.QueryRow(ctx, `
			SELECT has_any_column_privilege($1, 'public.admin_identities', 'SELECT')
			    OR has_any_column_privilege($1, 'public.admin_identities', 'INSERT')
			    OR has_any_column_privilege($1, 'public.admin_identities', 'UPDATE')
			    OR has_any_column_privilege($1, 'public.admin_identities', 'REFERENCES')
			    OR has_table_privilege($1, 'public.admin_identities', 'DELETE')
			    OR has_table_privilege($1, 'public.admin_identities', 'TRUNCATE')
			    OR has_table_privilege($1, 'public.admin_identities', 'TRIGGER')`, role).Scan(&reach); err != nil {
			t.Fatal(err)
		}
		if reach {
			t.Errorf("%s reaches admin_identities at 00069 (ADR 0031 §6: no role but the definers, and they only through the grants their functions bring)", role)
		}
	}
	opWant(t, aiApp(t, ctx, tx, uuid.Nil, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `SELECT count(*) FROM admin_identities`)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app reading admin_identities")

	email := aiEmail()
	aiIdentity(t, ctx, tx, email)
	err := opTry(t, ctx, tx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2)`, strings.ToUpper(email), opFakeDigest("b"))
	if code, name := m11Constraint(err); code != sqlstateUniqueViolation || name != "admin_identities_email_key" {
		t.Errorf("a case variant of an identity's address: %v, want 23505 on admin_identities_email_key (citext UNIQUE)", err)
	}
	for _, c := range []struct{ what, sql, constraint string }{
		{"a digest that is not bcrypt", `INSERT INTO admin_identities (email, password_hash) VALUES ('` + aiEmail() + `', 'plaintext')`,
			"admin_identities_password_hash_is_bcrypt"},
		{"a cost-15 bcrypt digest", `INSERT INTO admin_identities (email, password_hash) VALUES ('` + aiEmail() + `', '$2a$15$` + strings.Repeat("a", 53) + `')`,
			"admin_identities_password_hash_is_bcrypt"},
		{"a scope outside the two", `INSERT INTO admin_identities (email, password_hash, confirm_opened_at, confirm_open_until, confirm_scope)
			VALUES ('` + aiEmail() + `', '` + opFakeDigest("a") + `', now(), now() + interval '7 days', 'claim')`, "admin_identities_confirm_scope_known"},
		{"a window with no scope", `INSERT INTO admin_identities (email, password_hash, confirm_opened_at, confirm_open_until)
			VALUES ('` + aiEmail() + `', '` + opFakeDigest("a") + `', now(), now() + interval '7 days')`, "admin_identities_confirm_window_whole"},
		{"a window that ends before it opens", `INSERT INTO admin_identities (email, password_hash, confirm_opened_at, confirm_open_until, confirm_scope)
			VALUES ('` + aiEmail() + `', '` + opFakeDigest("a") + `', now(), now() - interval '1 second', 'reset')`, "admin_identities_confirm_window_forward"},
	} {
		if code, name := m11Constraint(opTry(t, ctx, tx, c.sql)); code != sqlstateCheckViolation || name != c.constraint {
			t.Errorf("%s: SQLSTATE %s on %q, want 23514 on %s", c.what, code, name, c.constraint)
		}
	}
	if err := opTry(t, ctx, tx, `INSERT INTO admin_identities (email, password_hash, confirm_opened_at, confirm_open_until, confirm_scope)
		VALUES ($1, $2, now(), now() + interval '7 days', 'first_verify')`, aiEmail(), opFakeDigest("a")); err != nil {
		t.Errorf("a whole first_verify window was refused: %v", err)
	}

	// The restore residue: tappa_app with SELECT and INSERT on the table (a pg_dump re-applies the
	// default to the re-created table, without the REVOKE). No policy names it, so it reads 0 rows
	// and inserts nothing.
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `GRANT SELECT, INSERT ON admin_identities TO tappa_app`)
		if err := aiApp(t, ctx, sp, uuid.Nil, func(app pgx.Tx) error {
			var n int64
			if err := app.QueryRow(ctx, `SELECT count(*) FROM admin_identities`).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Errorf("tappa_app with the restore residue reads %d identities, want 0", n)
			}
			return nil
		}); err != nil {
			t.Fatalf("read with the residue: %v", err)
		}
		opWant(t, aiApp(t, ctx, sp, uuid.Nil, func(app pgx.Tx) error {
			_, err := app.Exec(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2)`, aiEmail(), opFakeDigest("a"))
			return err
		}), sqlstateInsufficientPrivi, "tappa_app inserting an identity with the restore residue (RLS, no policy)")
	})
}

// ---------------------------------------------------------- tappa_app's reach --

// TestAdminUsers00069_TheApplicationCannotWriteTheLink is the 🔴 of ADR 0031 §5.1: tappa_app reads
// exactly the ten columns of S9 (the table-level SELECT is gone, so `*` is refused), cannot read
// linked_via / linked_at / unlinked_at (R16), cannot INSERT or UPDATE any link column, cannot UPDATE
// email (K-14's mirror) or admin_sessions.authenticated_at (R15) -- each measured in the catalogue
// and by the statement itself, on the application's OWN row in its own context, so the 42501 is the
// privilege's and not RLS's. The operator's two roles and the resolver reach none of the new columns.
func TestAdminUsers00069_TheApplicationCannotWriteTheLink(t *testing.T) {
	ctx, tx := opTx(t)

	for _, c := range []struct{ role, table, priv, want string }{
		{"tappa_app", "admin_users", "SELECT", aiAppSelect},
		{"tappa_app", "admin_users", "INSERT", "id,tenant_id,full_name,email,password_hash,role,status"},
		{"tappa_app", "admin_users", "UPDATE", "full_name,password_hash,role,status,last_login_at"},
		{"tappa_app", "admin_sessions", "UPDATE", "last_used_at,revoked_at"},
		{"tappa_operator", "admin_users", "SELECT", ""},
		{"tappa_opdefiner", "admin_users", "SELECT", "tenant_id,email,status"},
		{"tappa_resolver", "admin_users", "SELECT", "id,tenant_id,email,password_hash,status,created_at"},
		{"tappa_identitydefiner", "admin_users", "SELECT", ""},
		{"tappa_identitydefiner", "admin_sessions", "SELECT", ""},
	} {
		if got := opColumns(t, ctx, tx, c.role, c.table, c.priv); got != c.want {
			t.Errorf("%s %s on %s = %q, want %q", c.role, c.priv, c.table, got, c.want)
		}
	}
	if opInt(t, ctx, tx, `SELECT has_table_privilege('tappa_app', 'public.admin_users', 'SELECT')::int`) != 0 {
		t.Error("tappa_app holds table-level SELECT on admin_users: it covers linked_via and every future column (S9)")
	}
	for _, role := range []string{"tappa_operator", "tappa_opdefiner", "tappa_resolver", "tappa_holdingdefiner", "tappa_feedwriter"} {
		for _, col := range []string{"identity_id", "linked_via", "linked_at", "unlinked_at"} {
			if opInt(t, ctx, tx, `SELECT (has_column_privilege($1, 'public.admin_users', $2, 'SELECT')
			                           OR has_column_privilege($1, 'public.admin_users', $2, 'INSERT')
			                           OR has_column_privilege($1, 'public.admin_users', $2, 'UPDATE'))::int`, role, col) != 0 {
				t.Errorf("%s reaches admin_users.%s", role, col)
			}
		}
	}

	tenant := aiTenant(t, ctx, tx)
	admin := aiAdmin(t, ctx, tx, tenant, aiEmail())
	identity := aiIdentity(t, ctx, tx, aiEmail())
	session := uuid.New()
	aiExec(t, ctx, tx, `INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`,
		session, tenant, admin, opRandHex(t))

	for _, c := range []struct{ what, sql string }{
		{"UPDATE identity_id (the twin of manager.go:285-292)", `UPDATE admin_users SET identity_id = $2 WHERE id = $1`},
		{"UPDATE linked_via", `UPDATE admin_users SET linked_via = 'password' WHERE id = $1 AND $2::uuid IS NOT NULL`},
		{"UPDATE linked_at", `UPDATE admin_users SET linked_at = now() WHERE id = $1 AND $2::uuid IS NOT NULL`},
		{"UPDATE unlinked_at", `UPDATE admin_users SET unlinked_at = NULL WHERE id = $1 AND $2::uuid IS NOT NULL`},
		{"UPDATE email (K-14's mirror)", `UPDATE admin_users SET email = 'other@m14.example' WHERE id = $1 AND $2::uuid IS NOT NULL`},
		{"INSERT a row naming identity_id", `INSERT INTO admin_users (tenant_id, full_name, email, password_hash, role, identity_id)
			SELECT tenant_id, 'x', 'x-' || id || '@m14.example', password_hash, 'manager', $2 FROM admin_users WHERE id = $1`},
		{"SELECT linked_via (R16)", `SELECT linked_via FROM admin_users WHERE id = $1 AND $2::uuid IS NOT NULL`},
		{"SELECT linked_at", `SELECT linked_at FROM admin_users WHERE id = $1 AND $2::uuid IS NOT NULL`},
		{"SELECT unlinked_at", `SELECT unlinked_at FROM admin_users WHERE id = $1 AND $2::uuid IS NOT NULL`},
		{"SELECT * (no table-level SELECT)", `SELECT * FROM admin_users WHERE id = $1 AND $2::uuid IS NOT NULL`},
	} {
		opWant(t, aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, c.sql, admin, identity)
			return err
		}), sqlstateInsufficientPrivi, "tappa_app: "+c.what)
	}
	opWant(t, aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `UPDATE admin_sessions SET authenticated_at = now() + interval '1 day' WHERE id = $1`, session)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app: UPDATE admin_sessions.authenticated_at (R15)")

	// Positive control: the same connection, the same row, a column it may read and one it may write.
	if err := aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		var got *uuid.UUID
		if err := sp.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, admin).Scan(&got); err != nil {
			return err
		}
		n, err := aiRows(ctx, sp, `UPDATE admin_users SET last_login_at = now() WHERE id = $1`, admin)
		if err == nil && n != 1 {
			t.Errorf("positive control: tappa_app's own last_login_at write touched %d rows", n)
		}
		return err
	}); err != nil {
		t.Fatalf("positive control: tappa_app on its own row: %v", err)
	}
}

// TestAdminUsers00069_TheLinkCarriesItsTimeAndAKnownWay pins the two CHECKs and the foreign key:
// identity_id without linked_at is refused (T12 -- a backfill cannot skip it), linked_via is the
// ADR's closed set ('claim' included -- S7), identity_id names an existing identity.
func TestAdminUsers00069_TheLinkCarriesItsTimeAndAKnownWay(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := aiTenant(t, ctx, tx)
	admin := aiAdmin(t, ctx, tx, tenant, aiEmail())
	identity := aiIdentity(t, ctx, tx, aiEmail())

	if code, name := m11Constraint(opTry(t, ctx, tx, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill' WHERE id = $1`, admin, identity)); code != sqlstateCheckViolation || name != "admin_users_link_has_a_time" {
		t.Errorf("a link with no linked_at: %s on %q, want 23514 on admin_users_link_has_a_time", code, name)
	}
	if code, name := m11Constraint(opTry(t, ctx, tx, `UPDATE admin_users SET linked_via = 'operator' WHERE id = $1`, admin)); code != sqlstateCheckViolation || name != "admin_users_linked_via_known" {
		t.Errorf("linked_via outside the set: %s on %q, want 23514 on admin_users_linked_via_known", code, name)
	}
	for _, via := range []string{"backfill", "reconcile", "password", "account", "invite", "signup", "claim"} {
		if err := opTry(t, ctx, tx, `UPDATE admin_users SET identity_id = $2, linked_via = $3, linked_at = now() WHERE id = $1`, admin, identity, via); err != nil {
			t.Errorf("linked_via = %q was refused: %v", via, err)
		}
		// Back to unlinked, as the owner: the trigger fires (the row is linked) and lets it through --
		// it guards the credentials, not the link, which tappa_app cannot write at all.
		if err := opTry(t, ctx, tx, `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE id = $1`, admin); err != nil {
			t.Fatalf("unlink: %v", err)
		}
	}
	if code, _ := m11Constraint(opTry(t, ctx, tx, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() WHERE id = $1`, admin, uuid.New())); code != "23503" {
		t.Errorf("identity_id naming no identity: SQLSTATE %s, want 23503 (admin_users_identity_fk)", code)
	}
}

// ------------------------------------------------------------------ the trigger --

// TestAdminUsers00069_TheCredentialTriggerIsInvokerAndItsOwnersAlone is ADR 0031 §5.1's catalogue
// half (R6): the function is SECURITY INVOKER (prosecdef = false -- a DEFINER body would read its
// owner as current_user and never see the writer), plpgsql, search_path pinned, EXECUTE in its owner
// alone (ADR 0028 §1, 00061); the trigger is BEFORE UPDATE FOR EACH ROW on admin_users, enabled,
// bound to it, the one trigger that calls it. Bound to another table it refuses (TG_RELID), and
// tappa_app cannot bind it at all.
func TestAdminUsers00069_TheCredentialTriggerIsInvokerAndItsOwnersAlone(t *testing.T) {
	ctx, tx := opTx(t)

	var (
		definer                         bool
		owner, config, execute, lang, r string
	)
	if err := tx.QueryRow(ctx, `
		SELECT p.prosecdef, pg_get_userbyid(p.proowner), coalesce(array_to_string(p.proconfig, ';'), ''),
		       coalesce((SELECT string_agg(CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, ','
		                                           ORDER BY CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END)
		                   FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a WHERE a.privilege_type = 'EXECUTE'), ''),
		       l.lanname, p.prorettype::regtype::text
		  FROM pg_proc p JOIN pg_language l ON l.oid = p.prolang
		 WHERE p.oid = ('public.' || $1)::regprocedure`, aiFunction).Scan(&definer, &owner, &config, &execute, &lang, &r); err != nil {
		t.Fatalf("read %s: %v", aiFunction, err)
	}
	if definer {
		t.Errorf("%s is SECURITY DEFINER: current_user in its body is then its owner and the arms never see the writer (ADR 0031 §5.1, R6)", aiFunction)
	}
	if owner != "tappa_owner" || config != "search_path=pg_catalog, pg_temp" || execute != "tappa_owner" || lang != "plpgsql" || r != "trigger" {
		t.Errorf("%s: owner=%s config=%q execute=%q lang=%s returns=%s; want tappa_owner, the pinned search_path, EXECUTE in the owner alone, plpgsql, trigger",
			aiFunction, owner, config, execute, lang, r)
	}
	for _, role := range []string{"public", "tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_identitydefiner", "tappa_resolver"} {
		if opInt(t, ctx, tx, `SELECT has_function_privilege($1, ('public.' || $2)::regprocedure, 'EXECUTE')::int`, role, aiFunction) != 0 {
			t.Errorf("%s may EXECUTE %s", role, aiFunction)
		}
	}

	var enabled string
	var tgtype int16
	var fn, def string
	if err := tx.QueryRow(ctx, `
		SELECT t.tgenabled::text, t.tgtype, t.tgfoid::regprocedure::text, pg_get_triggerdef(t.oid)
		  FROM pg_trigger t WHERE t.tgrelid = 'public.admin_users'::regclass AND t.tgname = $1 AND NOT t.tgisinternal`, aiTrigger).
		Scan(&enabled, &tgtype, &fn, &def); err != nil {
		t.Fatalf("read the trigger %s: %v", aiTrigger, err)
	}
	// tgtype bits: ROW 1, BEFORE 2, UPDATE 16.
	if enabled != "O" || tgtype != 1|2|16 || fn != aiFunction {
		t.Errorf("trigger %s: enabled=%s tgtype=%d function=%s; want O (fires on origin), BEFORE UPDATE FOR EACH ROW (19), %s", aiTrigger, enabled, tgtype, fn, aiFunction)
	}
	if !strings.Contains(def, "WHEN (((old.identity_id IS NOT NULL) OR (old.unlinked_at IS NOT NULL)))") {
		t.Errorf("trigger %s's WHEN is not the two arms' rows: %s", aiTrigger, def)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_trigger WHERE tgfoid = ('public.' || $1)::regprocedure`, aiFunction); n != 1 {
		t.Errorf("%d triggers call %s, want 1", n, aiFunction)
	}

	for _, s := range []string{
		`CREATE TEMP TABLE zz_m14_bound (id int, identity_id uuid, unlinked_at timestamptz, password_hash text, email text)`,
		`CREATE TRIGGER zz_m14_bound_guard BEFORE UPDATE ON zz_m14_bound FOR EACH ROW EXECUTE FUNCTION public.` + aiFunction,
		`INSERT INTO zz_m14_bound VALUES (1, gen_random_uuid(), NULL, 'x', 'x')`,
	} {
		aiExec(t, ctx, tx, s)
	}
	opWant(t, opTry(t, ctx, tx, `UPDATE zz_m14_bound SET password_hash = 'y'`), sqlstatePrerequisiteState,
		"the trigger function fired on a table that is not admin_users")
	opWant(t, opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `CREATE TEMP TABLE zz_m14_app (id int, identity_id uuid, unlinked_at timestamptz, password_hash text, email text)`); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `CREATE TRIGGER zz_m14_app_bound BEFORE UPDATE ON zz_m14_app FOR EACH ROW EXECUTE FUNCTION public.`+aiFunction)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app binding the trigger function to a table of its own")
}

// TestAdminUsers00069_TheCredentialTriggerKeepsALinkedOrRefusedRowToTheDefiner is ADR 0031 §5.1's
// two arms, each measured against the writer that must pass and the writers that must not:
//
//   - a LINKED row: password_hash or e-mail changed by tappa_app or the owner -> 23001 (the e-mail
//     with tappa_app's UPDATE (email) handed back inside the case, so the trigger and not the
//     privilege answers; a case-only change counts); changed by tappa_identitydefiner (its column
//     grants handed it inside the case, as M14-04/05 will) -> written. Every other column of the
//     linked row (last_login_at, full_name, role, status) stays writable for tappa_app;
//   - a REFUSED row (unlinked_at stamped, identity_id NULL -- the second arm, S8): its password_hash
//     by tappa_app or the owner -> 23001, by the definer -> written; its last_login_at -> written;
//   - an UNLINKED row -- every row today -- keeps today's writes, password included.
func TestAdminUsers00069_TheCredentialTriggerKeepsALinkedOrRefusedRowToTheDefiner(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := aiTenant(t, ctx, tx)
	identityEmail := aiEmail()
	identity := aiIdentity(t, ctx, tx, identityEmail)
	linked := aiAdmin(t, ctx, tx, tenant, identityEmail)
	aiLink(t, ctx, tx, linked, identity)
	unlinked := aiAdmin(t, ctx, tx, tenant, aiEmail())
	refused := aiAdmin(t, ctx, tx, tenant, aiEmail())
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now(), linked_via = 'backfill', linked_at = now() - interval '1 day' WHERE id = $1`, refused)

	newDigest := opFakeDigest("c")
	appWrite := func(sql string, args ...any) error {
		t.Helper()
		return aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
			n, err := aiRows(ctx, sp, sql, args...)
			if err == nil && n != 1 {
				t.Errorf("tappa_app: %s touched %d rows, want 1", sql, n)
			}
			return err
		})
	}
	definerWrite := func(sql string, args ...any) error {
		t.Helper()
		var out error
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `GRANT SELECT (id) ON admin_users TO tappa_identitydefiner`)
			aiExec(t, ctx, sp, `GRANT UPDATE (password_hash, email) ON admin_users TO tappa_identitydefiner`)
			out = opAs(t, ctx, sp, "tappa_identitydefiner", func(d pgx.Tx) error {
				n, err := aiRows(ctx, d, sql, args...)
				if err == nil && n != 1 {
					t.Errorf("tappa_identitydefiner: %s touched %d rows, want 1", sql, n)
				}
				return err
			})
		})
		return out
	}

	// The linked row, first arm.
	opWant(t, appWrite(`UPDATE admin_users SET password_hash = $2 WHERE id = $1`, linked, newDigest),
		sqlstateRestrictViolation, "tappa_app changing a linked row's password_hash")
	opWant(t, opTry(t, ctx, tx, `UPDATE admin_users SET password_hash = $2 WHERE id = $1`, linked, newDigest),
		sqlstateRestrictViolation, "the owner changing a linked row's password_hash (fail closed)")
	opWant(t, opTry(t, ctx, tx, `UPDATE admin_users SET email = upper(email::text) WHERE id = $1`, linked),
		sqlstateRestrictViolation, "the owner changing only the case of a linked row's e-mail")
	opWant(t, appWrite(`UPDATE admin_users SET email = 'moved@m14.example' WHERE id = $1`, linked),
		sqlstateInsufficientPrivi, "tappa_app changing a linked row's e-mail (the privilege answers first)")
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `GRANT UPDATE (email) ON admin_users TO tappa_app`)
		opWant(t, aiApp(t, ctx, sp, tenant, func(app pgx.Tx) error {
			_, err := app.Exec(ctx, `UPDATE admin_users SET email = 'moved@m14.example' WHERE id = $1`, linked)
			return err
		}), sqlstateRestrictViolation, "tappa_app with UPDATE (email) handed back changing a linked row's e-mail (the trigger answers)")
	})
	if err := definerWrite(`UPDATE admin_users SET password_hash = $2 WHERE id = $1`, linked, newDigest); err != nil {
		t.Errorf("tappa_identitydefiner changing a linked row's password_hash was refused: %v", err)
	}
	if err := definerWrite(`UPDATE admin_users SET email = 'moved@m14.example' WHERE id = $1`, linked); err != nil {
		t.Errorf("tappa_identitydefiner changing a linked row's e-mail was refused: %v", err)
	}
	for _, s := range []string{
		`UPDATE admin_users SET last_login_at = now() WHERE id = $1`,
		`UPDATE admin_users SET full_name = 'Renamed' WHERE id = $1`,
		`UPDATE admin_users SET role = 'manager' WHERE id = $1`,
		`UPDATE admin_users SET status = 'disabled' WHERE id = $1`,
		`UPDATE admin_users SET password_hash = password_hash WHERE id = $1`,
	} {
		if err := appWrite(s, linked); err != nil {
			t.Errorf("tappa_app on a linked row, %s: %v", s, err)
		}
	}

	// The refused row, second arm.
	opWant(t, appWrite(`UPDATE admin_users SET password_hash = $2 WHERE id = $1`, refused, newDigest),
		sqlstateRestrictViolation, "tappa_app changing a refused row's password_hash (the second arm)")
	opWant(t, opTry(t, ctx, tx, `UPDATE admin_users SET password_hash = $2 WHERE id = $1`, refused, newDigest),
		sqlstateRestrictViolation, "the owner changing a refused row's password_hash")
	if err := definerWrite(`UPDATE admin_users SET password_hash = $2 WHERE id = $1`, refused, newDigest); err != nil {
		t.Errorf("tappa_identitydefiner changing a refused row's password_hash was refused: %v", err)
	}
	if err := appWrite(`UPDATE admin_users SET last_login_at = now() WHERE id = $1`, refused); err != nil {
		t.Errorf("tappa_app's last_login_at on a refused row: %v", err)
	}

	// The unlinked row: today's writes.
	for _, s := range []string{
		`UPDATE admin_users SET password_hash = $2 WHERE id = $1`,
		`UPDATE admin_users SET last_login_at = now() WHERE id = $1 AND $2::text IS NOT NULL`,
	} {
		if err := appWrite(s, unlinked, newDigest); err != nil {
			t.Errorf("tappa_app on an unlinked row, %s: %v", s, err)
		}
	}
	if err := opTry(t, ctx, tx, `UPDATE admin_users SET email = upper(email::text) WHERE id = $1`, unlinked); err != nil {
		t.Errorf("the owner changing an unlinked row's e-mail: %v", err)
	}
}

// ------------------------------------------------------------------ RLS --

// TestRLS_AdminUsers_AnIdentitysMembershipInAnotherTenantIsInvisible is CLAUDE.md §8's RLS test for
// what 00069 adds (ADR 0031 §9 "RLS (filtresiz sonda)"): one identity, a membership row in A and one
// in B. tappa_app in A's context asks by identity_id ALONE -- no tenant_id predicate, so a 0 can only
// be RLS's -- and sees A's row only; B's row by its own id is invisible; a write by identity_id
// touches A's row only. Positive control: in B's context the same statement sees B's row; with no
// context, nothing.
func TestRLS_AdminUsers_AnIdentitysMembershipInAnotherTenantIsInvisible(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	email := aiEmail()
	identity := aiIdentity(t, ctx, tx, email)
	rowA, rowB := aiAdmin(t, ctx, tx, a, email), aiAdmin(t, ctx, tx, b, email)
	aiLink(t, ctx, tx, rowA, identity)
	aiLink(t, ctx, tx, rowB, identity)

	seen := func(tenant uuid.UUID) []uuid.UUID {
		t.Helper()
		var ids []uuid.UUID
		if err := aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
			rows, err := sp.Query(ctx, `SELECT id FROM admin_users WHERE identity_id = $1`, identity)
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			return err
		}); err != nil {
			t.Fatalf("tappa_app reading by identity_id: %v", err)
		}
		return ids
	}
	if got := seen(a); !slices.Equal(got, []uuid.UUID{rowA}) {
		t.Errorf("in A's context the identity's rows are %v, want only A's %v", got, rowA)
	}
	if got := seen(b); !slices.Equal(got, []uuid.UUID{rowB}) {
		t.Errorf("positive control: in B's context the identity's rows are %v, want only B's %v", got, rowB)
	}
	if got := seen(uuid.Nil); len(got) != 0 {
		t.Errorf("with no tenant context tappa_app sees %v", got)
	}
	if err := aiApp(t, ctx, tx, a, func(sp pgx.Tx) error {
		var n int64
		if err := sp.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE id = $1`, rowB).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("in A's context B's row of the same identity is visible by its id")
		}
		n, err := aiRows(ctx, sp, `UPDATE admin_users SET full_name = 'written from A' WHERE identity_id = $1`, identity)
		if err == nil && n != 1 {
			t.Errorf("in A's context a write by identity_id touched %d rows, want 1 (A's)", n)
		}
		return err
	}); err != nil {
		t.Fatalf("tappa_app in A: %v", err)
	}
	var nameB string
	if err := tx.QueryRow(ctx, `SELECT full_name FROM admin_users WHERE id = $1`, rowB).Scan(&nameB); err != nil {
		t.Fatal(err)
	}
	if nameB == "written from A" {
		t.Error("a write in A's context by identity_id reached B's row")
	}
}

// ------------------------------------------------------------ the old binary --

// TestAdminUsers00069_TheBinaryBeforeItRunsOnIt is ADR 0031 §8's expand rule, through the real
// generated statements as tappa_app: CreateAdminUser (no link column named -- the row is born
// unlinked), CreateAdminSession (authenticated_at takes the DEFAULT), TouchAdminSession,
// MarkAdminLoggedIn and SetOwnAdminPassword on the unlinked row all work; SetOwnAdminPassword on a
// LINKED row is the named cost of the rollout window (23001, ADR 0031 §5.1).
func TestAdminUsers00069_TheBinaryBeforeItRunsOnIt(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := aiTenant(t, ctx, tx)
	email := aiEmail()
	var created store.CreateAdminUserRow
	var session store.CreateAdminSessionRow
	if err := aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		q := store.New(sp)
		var err error
		if created, err = q.CreateAdminUser(ctx, store.CreateAdminUserParams{
			FullName: "M14 Old Binary", Email: &email, PasswordHash: opFakeDigest("a"), Role: "owner", TenantID: tenant,
		}); err != nil {
			return err
		}
		if session, err = q.CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TenantID: tenant, AdminUserID: created.ID, TokenHash: opRandHex(t),
		}); err != nil {
			return err
		}
		touched, err := q.TouchAdminSession(ctx, store.TouchAdminSessionParams{ID: session.ID, TenantID: tenant})
		if err != nil {
			return err
		}
		if touched.AdminUserID != created.ID || touched.Role != "owner" {
			t.Errorf("TouchAdminSession = %+v, want the new owner", touched)
		}
		if _, err := q.MarkAdminLoggedIn(ctx, store.MarkAdminLoggedInParams{ID: created.ID, TenantID: tenant}); err != nil {
			return err
		}
		id, err := q.SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{PasswordHash: opFakeDigest("b"), ID: created.ID, TenantID: tenant})
		if err == nil && id != created.ID {
			t.Errorf("SetOwnAdminPassword wrote %s, want %s", id, created.ID)
		}
		return err
	}); err != nil {
		t.Fatalf("the old binary's statements on the new schema: %v", err)
	}
	var linkedNull, freshAuth bool
	if err := tx.QueryRow(ctx, `SELECT a.identity_id IS NULL AND a.linked_via IS NULL AND a.linked_at IS NULL AND a.unlinked_at IS NULL,
	                                   s.authenticated_at = now()
	                              FROM admin_users a JOIN admin_sessions s ON s.admin_user_id = a.id
	                             WHERE a.id = $1 AND s.id = $2`, created.ID, session.ID).Scan(&linkedNull, &freshAuth); err != nil {
		t.Fatal(err)
	}
	if !linkedNull || !freshAuth {
		t.Errorf("the old binary's row: unlinked=%v, session authenticated_at = its login's now()=%v; want both", linkedNull, freshAuth)
	}

	identity := aiIdentity(t, ctx, tx, email)
	aiLink(t, ctx, tx, created.ID, identity)
	opWant(t, aiApp(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).SetOwnAdminPassword(ctx, store.SetOwnAdminPasswordParams{PasswordHash: opFakeDigest("c"), ID: created.ID, TenantID: tenant})
		return err
	}), sqlstateRestrictViolation, "the old binary's SetOwnAdminPassword on a linked row (the rollout window's named cost)")
}

// aiStoreStatements returns every generated statement (internal/store, sqlc's `-- name:` constants)
// whose text names admin_users or admin_sessions, by its name.
func aiStoreStatements(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join("..", "store", "*.sql.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("glob internal/store: %v (%d files)", err, len(files))
	}
	name := regexp.MustCompile(`^-- name: (\w+) :`)
	out := map[string]string{}
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, v := range spec.Values {
				s, ok := aiConstString(v)
				if !ok {
					continue
				}
				if m := name.FindStringSubmatch(s); m != nil && (strings.Contains(s, "admin_users") || strings.Contains(s, "admin_sessions")) {
					out[m[1]] = s
				}
			}
			return true
		})
	}
	return out
}

// aiConstString folds a constant string expression -- sqlc writes a backquote inside a raw string
// as `...` + "`" + `...` -- into its value; ok is false for anything that is not literals and +.
func aiConstString(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, ok := aiConstString(x.X)
		if !ok {
			return "", false
		}
		r, ok := aiConstString(x.Y)
		return l + r, ok
	case *ast.ParenExpr:
		return aiConstString(x.X)
	}
	return "", false
}

// TestAdminUsers00069_EveryGeneratedStatementOnTheAdminTablesStillPlansAsTheApp is the "bugünkü
// bütün sorgular yeşil kalır" half of S9: each generated statement that names admin_users or
// admin_sessions is planned as tappa_app (EXPLAIN (GENERIC_PLAN) -- the executor's privilege check
// runs at plan time, so a column the narrowed SELECT no longer covers, a `*`, or a write of a link
// column is 42501 here without a row being touched). The control shows EXPLAIN does check:
// linked_via is refused.
func TestAdminUsers00069_EveryGeneratedStatementOnTheAdminTablesStillPlansAsTheApp(t *testing.T) {
	ctx, tx := opTx(t)
	statements := aiStoreStatements(t)
	for _, must := range []string{"CreateAdminUser", "SetOwnAdminPassword", "TouchAdminSession", "CreateAdminSession", "MarkAdminLoggedIn"} {
		if _, ok := statements[must]; !ok {
			t.Errorf("anti-vacuity: the sweep did not find %s", must)
		}
	}
	if len(statements) < 15 {
		t.Fatalf("anti-vacuity: the sweep found %d statements on the admin tables", len(statements))
	}
	explain := func(sql string) error {
		return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			_, err := sp.Conn().PgConn().Exec(ctx, "EXPLAIN (GENERIC_PLAN) "+sql).ReadAll()
			return err
		})
	}
	names := make([]string, 0, len(statements))
	for n := range statements {
		names = append(names, n)
	}
	slices.Sort(names)
	t.Logf("planning %d generated statements on the admin tables as tappa_app: %s", len(names), strings.Join(names, ", "))
	for _, n := range names {
		if err := explain(statements[n]); err != nil {
			t.Errorf("%s does not plan as tappa_app on the 00069 schema: %v", n, err)
		}
	}
	opWant(t, explain(`SELECT linked_via FROM admin_users WHERE id = $1`), sqlstateInsufficientPrivi,
		"control: EXPLAIN of a hidden column as tappa_app")
}

// ------------------------------------------------------------ Down / Up --

// aiCatalogue is what 00069 changes, read as EFFECTIVE privileges and catalogue text, so a Down /
// Up walk can be compared with HEAD.
func aiCatalogue(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var b strings.Builder
	for _, sql := range []string{
		`SELECT coalesce(string_agg(c.relname || '.' || a.attname || ' ' || format_type(a.atttypid, a.atttypmod) || ' nn=' || a.attnotnull
		                            || ' def=' || coalesce(pg_get_expr(d.adbin, d.adrelid), '-'), E'\n' ORDER BY c.relname, a.attnum), '')
		   FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		   LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
		  WHERE c.relnamespace = 'public'::regnamespace AND c.relname IN ('admin_users', 'admin_sessions', 'admin_identities')`,
		`SELECT coalesce(string_agg(conrelid::regclass::text || ' ' || conname || ' ' || pg_get_constraintdef(oid), E'\n' ORDER BY conrelid::regclass::text, conname), '')
		   FROM pg_constraint WHERE conrelid IN (SELECT oid FROM pg_class WHERE relnamespace = 'public'::regnamespace
		                                          AND relname IN ('admin_users', 'admin_sessions', 'admin_identities'))`,
		`SELECT coalesce(string_agg(pg_get_indexdef(indexrelid), E'\n' ORDER BY pg_get_indexdef(indexrelid)), '')
		   FROM pg_index WHERE indrelid IN (SELECT oid FROM pg_class WHERE relnamespace = 'public'::regnamespace
		                                     AND relname IN ('admin_users', 'admin_sessions', 'admin_identities'))`,
		`SELECT coalesce(string_agg(pg_get_triggerdef(oid), E'\n' ORDER BY tgname), '')
		   FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN ('public.admin_users'::regclass, 'public.admin_sessions'::regclass)`,
		`SELECT coalesce(string_agg(p.oid::regprocedure::text || ' definer=' || p.prosecdef || ' acl=' || coalesce(p.proacl::text, '-'), E'\n'), '')
		   FROM pg_proc p WHERE p.pronamespace = 'public'::regnamespace AND p.proname = 'admin_users_guard_identity_credentials'`,
		`SELECT coalesce(string_agg(c.relname || ' rls=' || c.relrowsecurity || ',' || c.relforcerowsecurity, E'\n'), '')
		   FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relname = 'admin_identities'`,
		// Effective privileges, column by column, for every role but the owner.
		`SELECT coalesce(string_agg(r.rolname || ' ' || c.relname || '.' || a.attname || ' ' ||
		          CASE WHEN has_column_privilege(r.oid, c.oid, a.attnum, 'SELECT') THEN 'S' ELSE '' END ||
		          CASE WHEN has_column_privilege(r.oid, c.oid, a.attnum, 'INSERT') THEN 'I' ELSE '' END ||
		          CASE WHEN has_column_privilege(r.oid, c.oid, a.attnum, 'UPDATE') THEN 'U' ELSE '' END, E'\n' ORDER BY r.rolname, c.relname, a.attnum), '')
		   FROM pg_roles r CROSS JOIN pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		  WHERE r.rolname ~ '^tappa_' AND NOT r.rolsuper
		    AND c.relnamespace = 'public'::regnamespace AND c.relname IN ('admin_users', 'admin_sessions', 'admin_identities')`,
		`SELECT coalesce(string_agg(r.rolname || ' ' || c.relname || ' table-SELECT=' || has_table_privilege(r.oid, c.oid, 'SELECT')
		          || ' DELETE=' || has_table_privilege(r.oid, c.oid, 'DELETE'), E'\n' ORDER BY r.rolname, c.relname), '')
		   FROM pg_roles r CROSS JOIN pg_class c
		  WHERE r.rolname ~ '^tappa_' AND NOT r.rolsuper
		    AND c.relnamespace = 'public'::regnamespace AND c.relname IN ('admin_users', 'admin_sessions', 'admin_identities')`,
	} {
		var s string
		if err := q.QueryRow(ctx, sql).Scan(&s); err != nil {
			t.Fatalf("read the catalogue: %v", err)
		}
		b.WriteString(s + "\n--\n")
	}
	return b.String()
}

// aiIdentityRolesBlock returns the IDENTITY ROLE block of scripts/db-init/01-roles.sql exactly as the
// runbook's sed cuts it (markers included).
func aiIdentityRolesBlock(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "db-init", "01-roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	const begin, end = "-- >>> IDENTITY ROLE (M14-03) >>>", "-- <<< IDENTITY ROLE (M14-03) <<<"
	src := string(b)
	i, j := strings.Index(src, begin), strings.Index(src, end)
	if i < 0 || j < i {
		t.Fatalf("01-roles.sql does not carry the IDENTITY ROLE markers in order (begin=%d end=%d)", i, j)
	}
	return src[i : j+len(end)]
}

// TestAdminIdentities00069_DownRefusesAnIdentityAndOtherwiseWalksBackAndUp is the card's Down
// criterion and the role's fresh-pod run, in this test's transaction (rolled back), with the tables
// the Down names -- and tenants, whose key the Up's CREATE TABLE references -- locked first
// (lockForDowns):
//
//   - with an identity, the Down is 55000 naming 00069 and changes nothing;
//   - without one, the Down gives 00064's catalogue back (tappa_app's table-level SELECT and
//     UPDATE (email) again, no link column, no authenticated_at, no table, no function), taking no
//     strong lock outside its tables; a session born then keeps authenticated_at NULL through the
//     Up again (an old session is not freshly authenticated -- R15), a session born after it takes
//     now(); the Up (and 00070's index, built here in the transaction) gives HEAD's catalogue back;
//   - in between, with tappa_identitydefiner renamed away (a cluster that never had it), 00069's
//     precondition refuses; the IDENTITY ROLE block -- cut as the runbook cuts it -- makes the role,
//     is a no-op the second time and takes LOGIN and NOBYPASSRLS away again; the precondition
//     refuses a member, a membership and a default privilege; and the runbook names the section.
func TestAdminIdentities00069_DownRefusesAnIdentityAndOtherwiseWalksBackAndUp(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ai00069File)
	up70, _ := opMigrationSections(t, ai00070File)
	tables := append(downTables(down), "tenants")
	slices.Sort(tables)
	tables = slices.Compact(tables)
	if !slices.Contains(tables, "admin_users") || !slices.Contains(tables, "admin_sessions") || !slices.Contains(tables, "admin_identities") {
		t.Fatalf("PREMISE: downTables found %v in 00069's Down", tables)
	}
	lockForDowns(t, ctx, tx, tables)
	head := aiCatalogue(t, ctx, tx)
	for _, want := range []string{
		"admin_users.identity_id uuid nn=false def=-", "admin_sessions.authenticated_at timestamp with time zone nn=false def=now()",
		"tappa_app admin_users table-SELECT=false", "tappa_app admin_users.email SI\n",
		"CREATE UNIQUE INDEX admin_users_tenant_identity_key ON public.admin_users USING btree (tenant_id, identity_id)",
	} {
		if !strings.Contains(head, want) {
			t.Errorf("PREMISE at HEAD: the catalogue lacks %q", want)
		}
	}

	t.Run("an identity: the Down refuses and changes nothing", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aiIdentity(t, ctx, sp, aiEmail())
			runErr := holdingNestedRun(t, ctx, sp, down)
			var pg *pgconn.PgError
			if !errors.As(runErr, &pg) || pg.Code != sqlstatePrerequisiteState || !strings.Contains(pg.Message, "migration 00069 Down") {
				t.Fatalf("the Down over an identity answered %v, want 55000 naming 00069", runErr)
			}
			if after := aiCatalogue(t, ctx, sp); after != head {
				t.Errorf("the refused Down changed the catalogue:\n%s\nwant HEAD's\n%s", after, head)
			}
		})
	})

	t.Run("none: the Down gives 00064 back, the role block makes the role, the Up takes HEAD again", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			// An identity another package committed would make the Down refuse; it is set aside inside
			// the savepoint and the rollback restores it.
			aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = NULL, unlinked_at = NULL WHERE identity_id IS NOT NULL OR unlinked_at IS NOT NULL`)
			aiExec(t, ctx, sp, `DELETE FROM admin_identities`)

			opRunSection(t, ctx, sp, down, "00069 Down")
			at64 := aiCatalogue(t, ctx, sp)
			for _, gone := range []string{"identity_id", "linked_via", "linked_at", "unlinked_at", "authenticated_at", "admin_identities", "admin_users_guard_identity_credentials"} {
				if strings.Contains(at64, gone) {
					t.Errorf("after 00069's Down the catalogue still names %s", gone)
				}
			}
			for _, want := range []string{"tappa_app admin_users table-SELECT=true", "tappa_app admin_users.email SIU\n"} {
				if !strings.Contains(at64, want) {
					t.Errorf("after 00069's Down tappa_app's 00064 reach is not back: no %q", want)
				}
			}
			if extra := strongTableLocksOutside(t, ctx, sp, tables); len(extra) > 0 {
				t.Errorf("the Down took a strong lock on %v, outside the tables it names", extra)
			}
			tenant := aiTenant(t, ctx, sp)
			admin := aiAdmin(t, ctx, sp, tenant, aiEmail())
			oldSession := uuid.New()
			aiExec(t, ctx, sp, `INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`, oldSession, tenant, admin, opRandHex(t))

			// The fresh pod: a cluster without the role.
			pre := holdingPrecondition(t, ai00069File)
			readme, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(pre, `section "Identity role (M14-03)"`) || !strings.Contains(pre, "scripts/db-init/01-roles.sql") {
				t.Error("00069's refusal does not name the runbook section and the block")
			}
			if !regexp.MustCompile(`(?m)^#+ .*Identity role \(M14-03\)`).Match(readme) {
				t.Error(`deploy/README.md has no heading "Identity role (M14-03)"`)
			}
			for _, marker := range []string{">>> IDENTITY ROLE (M14-03) >>>", "<<< IDENTITY ROLE (M14-03) <<<"} {
				if !strings.Contains(string(readme), marker) {
					t.Errorf("deploy/README.md's runbook does not cut the block by %q", marker)
				}
			}
			block := aiIdentityRolesBlock(t)
			aiExec(t, ctx, sp, `ALTER ROLE tappa_identitydefiner RENAME TO zz_m14_was_identitydefiner`)
			opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00069's precondition with no tappa_identitydefiner")
			shape := func() string {
				var s string
				if err := sp.QueryRow(ctx, `SELECT format('login=%s super=%s bypassrls=%s createdb=%s createrole=%s replication=%s usage=%s members=%s member_of=%s',
				                                   rolcanlogin, rolsuper, rolbypassrls, rolcreatedb, rolcreaterole, rolreplication,
				                                   has_schema_privilege(oid, 'public', 'USAGE'),
				                                   (SELECT count(*) FROM pg_auth_members m WHERE m.roleid = r.oid),
				                                   (SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid))
				                              FROM pg_roles r WHERE rolname = 'tappa_identitydefiner'`).Scan(&s); err != nil {
					t.Fatalf("read the role: %v", err)
				}
				return s
			}
			const wantShape = "login=f super=f bypassrls=t createdb=f createrole=f replication=f usage=t members=0 member_of=0"
			for k := 0; k < 2; k++ {
				opRunSection(t, ctx, sp, block, "the IDENTITY ROLE block")
				if got := shape(); got != wantShape {
					t.Errorf("run %d of the block: %s, want %s", k+1, got, wantShape)
				}
			}
			aiExec(t, ctx, sp, `ALTER ROLE tappa_identitydefiner LOGIN NOBYPASSRLS`)
			opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00069's precondition with a definer that logs in and obeys RLS")
			opRunSection(t, ctx, sp, block, "the IDENTITY ROLE block over a role that logs in and obeys RLS")
			if got := shape(); got != wantShape {
				t.Errorf("the block left the role %s, want %s", got, wantShape)
			}
			for _, c := range []struct{ what, do, undo string }{
				{"a member", `GRANT tappa_identitydefiner TO tappa_app`, `REVOKE tappa_identitydefiner FROM tappa_app`},
				{"a membership", `GRANT tappa_resolver TO tappa_identitydefiner`, `REVOKE tappa_resolver FROM tappa_identitydefiner`},
				{"a default privilege", `ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public GRANT SELECT ON TABLES TO tappa_identitydefiner`,
					`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public REVOKE SELECT ON TABLES FROM tappa_identitydefiner`},
				{"a grant some other hand gave", `GRANT SELECT (id) ON tenants TO tappa_identitydefiner`, `REVOKE SELECT (id) ON tenants FROM tappa_identitydefiner`},
			} {
				aiExec(t, ctx, sp, c.do)
				opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00069's precondition with "+c.what)
				aiExec(t, ctx, sp, c.undo)
			}
			if err := holdingNestedRun(t, ctx, sp, pre); err != nil {
				t.Fatalf("00069's precondition refuses the role the block made: %v", err)
			}

			opRunSection(t, ctx, sp, up, "00069 Up again")
			opRunSection(t, ctx, sp, strings.ReplaceAll(up70, " CONCURRENTLY", ""), "00070 Up again (in the transaction)")
			if again := aiCatalogue(t, ctx, sp); again != head {
				t.Errorf("after the Up again:\n%s\nwant HEAD's\n%s", again, head)
			}
			newSession := uuid.New()
			aiExec(t, ctx, sp, `INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3, $4)`, newSession, tenant, admin, opRandHex(t))
			var oldNull, newNow bool
			if err := sp.QueryRow(ctx, `SELECT (SELECT authenticated_at IS NULL FROM admin_sessions WHERE id = $1),
			                                   (SELECT authenticated_at = now() FROM admin_sessions WHERE id = $2)`, oldSession, newSession).
				Scan(&oldNull, &newNow); err != nil {
				t.Fatal(err)
			}
			if !oldNull || !newNow {
				t.Errorf("authenticated_at: the session born before the Up is NULL=%v, the one after = now()=%v; want both (R15)", oldNull, newNow)
			}
		})
	})
}
