package db

// apikeys_test.go -- migration 00051 (M11 API-3, ADR 0028 §3): api_keys and the seventh resolver,
// resolve_api_key_by_hash, against real Postgres. The matrix rows (every role's reach on the
// table, the resolver's and the trigger function's EXECUTE) are in m11schema_test.go; what is
// here is what a matrix row cannot say.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/test/fixtures"
)

const op00051File = "00051_create_api_keys.sql"

// randHash is a token_hash of the stored shape (64 lower-case hex), not derived from any key.
func randHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// apiKeyWorld is a tenant with an owner, inside the owner's test transaction.
type apiKeyWorld struct {
	tenant, owner uuid.UUID
}

func newAPIKeyWorld(t *testing.T, ctx context.Context, tx pgx.Tx) apiKeyWorld {
	t.Helper()
	w := apiKeyWorld{tenant: uuid.New(), owner: uuid.New()}
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'api-3', $2, 'restaurant', 'multi')`,
			[]any{w.tenant, "VAT-API3-" + w.tenant.String()}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status) VALUES ($1, $2, 'API-3 Owner', $3, $4, 'owner', 'active')`,
			[]any{w.owner, w.tenant, "api3-" + w.owner.String() + "@m11.example", fixtures.UnusablePasswordHash}},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("api key world: %v", err)
		}
	}
	return w
}

// addKey inserts a key as tappa_app in the tenant's context, the product's statement shape.
func (w apiKeyWorld) addKey(t *testing.T, ctx context.Context, tx pgx.Tx, name, hash string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, w.tenant.String()); err != nil {
			return err
		}
		return sp.QueryRow(ctx, `INSERT INTO api_keys (tenant_id, name, token_hash, prefix_hint, created_by)
		                          VALUES ($1, $2, $3, 'tt_live_AbCd', $4) RETURNING id`, w.tenant, name, hash, w.owner).Scan(&id)
	}); err != nil {
		t.Fatalf("add key: %v", err)
	}
	return id
}

// TestApiKeysCatalog is plan §5 API-3's "katalog api_keys" -- ADR 0002 md.7's five constraints on
// the seventh resolver, MEASURED in the live catalog, plus what ADR 0028 §3 adds:
//
//	definer   owner tappa_resolver (NOSUPERUSER, BYPASSRLS), SECURITY DEFINER, proconfig exactly
//	          search_path = pg_catalog, pg_temp, STABLE, one function of that name (no overload)
//	(i)       the argument list is exactly the hash
//	(ii)      at most one row: token_hash carries a single-column UNIQUE index (global, not per
//	          tenant), and tenants is joined by its primary key -- measured live: a known hash 1
//	          row, an unknown one 0
//	(iii)     the result is exactly (tenant_id, key_id, revoked_at, api_enabled_at)
//	(iv)      EXECUTE: tappa_app and the owner, PUBLIC not; tappa_app reads no token_hash and a
//	          context-less direct read is refused
//	(v)       the table's one policy is the standard NULLIF form with no OR branch
//	tenants   tappa_resolver's reach on tenants is SELECT on (id, api_enabled_at) and nothing else
//	          -- every column, every verb, table-level DELETE/TRUNCATE (ADR 0002's M11 note)
//	body      reads public.api_keys and public.tenants only, names no dynamic SQL
//	RLS five  ENABLE + FORCE, a tenant_id index (the leading column of one), the policy
//	trigger   the one-way trigger function refuses another table (TG_RELID) and tappa_app
//	          cannot bind it
func TestApiKeysCatalog(t *testing.T) {
	ctx, tx := opTx(t)
	var (
		owner, cfg, args, result, body string
		super, bypass, secdef          bool
		volatile                       string
	)
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_userbyid(p.proowner), r.rolsuper, r.rolbypassrls, p.prosecdef, array_to_string(p.proconfig, '|'),
		       pg_get_function_arguments(p.oid), pg_get_function_result(p.oid), p.prosrc, p.provolatile::text
		  FROM pg_proc p JOIN pg_roles r ON r.oid = p.proowner
		 WHERE p.oid = 'public.resolve_api_key_by_hash(text)'::regprocedure`).
		Scan(&owner, &super, &bypass, &secdef, &cfg, &args, &result, &body, &volatile); err != nil {
		t.Fatalf("read the resolver: %v", err)
	}
	if owner != "tappa_resolver" || super || !bypass || !secdef {
		t.Errorf("definer: owner %s super %v bypassrls %v secdef %v; want tappa_resolver, false, true, true", owner, super, bypass, secdef)
	}
	if cfg != "search_path=pg_catalog, pg_temp" || volatile != "s" {
		t.Errorf("proconfig %q, volatility %q; want the fixed search_path and STABLE", cfg, volatile)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname = 'resolve_api_key_by_hash'`); n != 1 {
		t.Errorf("%d functions named resolve_api_key_by_hash; an overload is a second door", n)
	}
	if args != "p_token_hash text" {
		t.Errorf("(i) arguments %q, want exactly the hash", args)
	}
	if want := "TABLE(tenant_id uuid, key_id uuid, revoked_at timestamp with time zone, api_enabled_at timestamp with time zone)"; result != want {
		t.Errorf("(iii) result %q, want %q", result, want)
	}
	if !strings.Contains(body, "public.api_keys") || !strings.Contains(body, "public.tenants") ||
		strings.Contains(strings.ToUpper(body), "EXECUTE") || strings.Contains(body, "format(") {
		t.Errorf("body: want qualified api_keys and tenants and no dynamic SQL:\n%s", body)
	}
	if n := opInt(t, ctx, tx, `
		SELECT count(*) FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
		 WHERE i.indrelid = 'public.api_keys'::regclass AND i.indisunique AND i.indnatts = 1 AND a.attname = 'token_hash'
		   AND i.indpred IS NULL`); n != 1 {
		t.Errorf("(ii) %d single-column, unconditional UNIQUE indexes on token_hash, want 1 (global, not per tenant)", n)
	}
	for _, c := range []struct {
		who  string
		want bool
	}{{"public", false}, {"tappa_app", true}, {"tappa_operator", false}, {"tappa_opdefiner", false}, {"tappa_holdingdefiner", false}, {"tappa_feedwriter", false}} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, 'public.resolve_api_key_by_hash(text)', 'EXECUTE')`, c.who).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may != c.want {
			t.Errorf("(iv) %s EXECUTE = %v, want %v", c.who, may, c.want)
		}
	}
	var policy string
	if err := tx.QueryRow(ctx, `SELECT pg_get_expr(polqual, polrelid) || ' / ' || pg_get_expr(polwithcheck, polrelid)
	                              FROM pg_policy WHERE polrelid = 'public.api_keys'::regclass`).Scan(&policy); err != nil {
		t.Fatalf("(v) the one policy: %v", err)
	}
	if strings.Count(policy, "NULLIF") != 2 || strings.Contains(strings.ToUpper(policy), " OR ") {
		t.Errorf("(v) policy %q, want the NULLIF form in USING and WITH CHECK and no OR branch", policy)
	}
	var enabled, forced bool
	if err := tx.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.api_keys'::regclass`).Scan(&enabled, &forced); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced {
		t.Errorf("RLS ENABLE %v FORCE %v", enabled, forced)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
	                            WHERE i.indrelid = 'public.api_keys'::regclass AND a.attname = 'tenant_id'`); n < 1 {
		t.Error("no index leads with tenant_id")
	}

	// tappa_resolver on tenants: (id, api_enabled_at) SELECT, nothing else anywhere.
	rows, err := tx.Query(ctx, `
		SELECT a.attname, has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT'),
		       has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'INSERT'),
		       has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'UPDATE'),
		       has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'REFERENCES')
		  FROM pg_attribute a WHERE a.attrelid = 'public.tenants'::regclass AND a.attnum > 0 AND NOT a.attisdropped`)
	if err != nil {
		t.Fatal(err)
	}
	columns := 0
	for rows.Next() {
		var name string
		var s, i, u, ref bool
		if err := rows.Scan(&name, &s, &i, &u, &ref); err != nil {
			t.Fatal(err)
		}
		columns++
		want := name == "id" || name == "api_enabled_at"
		if s != want || i || u || ref {
			t.Errorf("tappa_resolver on tenants.%s: SELECT %v INSERT %v UPDATE %v REFERENCES %v; want SELECT %v only", name, s, i, u, ref, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if columns < 10 {
		t.Fatalf("anti-vacuity: %d columns of tenants read", columns)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM (VALUES ('DELETE'), ('TRUNCATE'), ('TRIGGER')) v(p)
	                            WHERE has_table_privilege('tappa_resolver', 'public.tenants', v.p)`); n != 0 {
		t.Errorf("tappa_resolver holds %d table-level verbs on tenants, want 0", n)
	}

	// Live: (ii) one row for a known hash, none for an unknown one; (iv) a context-less direct read
	// by tappa_app is refused -- it holds no SELECT on token_hash at all.
	w := newAPIKeyWorld(t, ctx, tx)
	hash := randHash(t)
	keyID := w.addKey(t, ctx, tx, "catalog", hash)
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		var gotTenant, gotKey uuid.UUID
		var revoked, enabled *string
		if err := sp.QueryRow(ctx, `SELECT tenant_id, key_id, revoked_at::text, api_enabled_at::text FROM resolve_api_key_by_hash($1)`, hash).
			Scan(&gotTenant, &gotKey, &revoked, &enabled); err != nil {
			return err
		}
		if gotTenant != w.tenant || gotKey != keyID || revoked != nil || enabled != nil {
			t.Errorf("resolved %v/%v revoked %v enabled %v", gotTenant, gotKey, revoked, enabled)
		}
		n := 0
		if err := sp.QueryRow(ctx, `SELECT count(*) FROM resolve_api_key_by_hash($1)`, randHash(t)).Scan(&n); err != nil || n != 0 {
			t.Errorf("an unknown hash: %d rows, %v", n, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("resolve as tappa_app: %v", err)
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `SELECT token_hash FROM api_keys WHERE token_hash = $1`, hash),
		sqlstateInsufficientPrivi, "tappa_app reading token_hash directly")

	// The trigger function: bound elsewhere it refuses (TG_RELID); tappa_app cannot bind it.
	for _, s := range []string{
		`CREATE TEMP TABLE zz_api3_bound (revoked_at timestamptz)`,
		`CREATE TRIGGER zz_api3_bound_trg BEFORE UPDATE ON zz_api3_bound FOR EACH ROW EXECUTE FUNCTION public.api_keys_revocation_is_one_way()`,
		`INSERT INTO zz_api3_bound VALUES (now())`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	opWant(t, opTry(t, ctx, tx, `UPDATE zz_api3_bound SET revoked_at = NULL`), sqlstatePrerequisiteState,
		"the trigger function on a table that is not api_keys")
	opWant(t, opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `CREATE TEMP TABLE zz_api3_app (revoked_at timestamptz)`); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `CREATE TRIGGER zz_api3_app_trg BEFORE UPDATE ON zz_api3_app FOR EACH ROW EXECUTE FUNCTION public.api_keys_revocation_is_one_way()`)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app binding the trigger function")
}

// TestApiKeyRevocation_IsOneWay is plan §5 API-3's "iptal geri alınamaz" (00011's precedent): once
// revoked_at is written it does not go back to NULL and does not move to another instant -- for
// tappa_app AND for the owner (the trigger binds the table's owner too); the idempotent
// COALESCE(revoked_at, now()) on a revoked key is a no-op the trigger lets through; a first stamp
// before the key existed is refused by api_keys_revoked_after_created (00011's BOUNDARY 1); and
// tappa_app cannot DELETE a key (no verb) or rewrite its hash (no UPDATE on the column).
//
// MUTANT (measured, in a savepoint): the trigger dropped -> the un-revoke succeeds, the test's
// own control.
func TestApiKeyRevocation_IsOneWay(t *testing.T) {
	ctx, tx := opTx(t)
	w := newAPIKeyWorld(t, ctx, tx)
	id := w.addKey(t, ctx, tx, "one-way", randHash(t))
	asApp := func(sql string) error {
		return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, w.tenant.String()); err != nil {
				return err
			}
			_, err := sp.Exec(ctx, sql, id)
			return err
		})
	}
	if err := asApp(`UPDATE api_keys SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1 AND revoked_at IS NULL`); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := asApp(`UPDATE api_keys SET revoked_at = COALESCE(revoked_at, now()) WHERE id = $1`); err != nil {
		t.Fatalf("the idempotent COALESCE on a revoked key: %v", err)
	}
	opWant(t, asApp(`UPDATE api_keys SET revoked_at = NULL WHERE id = $1`), sqlstateRestrictViolation, "tappa_app un-revoking")
	opWant(t, asApp(`UPDATE api_keys SET revoked_at = revoked_at + interval '1 second' WHERE id = $1`), sqlstateRestrictViolation, "tappa_app moving the stamp")
	opWant(t, opTry(t, ctx, tx, `UPDATE api_keys SET revoked_at = NULL WHERE id = $1`, id), sqlstateRestrictViolation, "the owner un-revoking")
	opWant(t, asApp(`DELETE FROM api_keys WHERE id = $1`), sqlstateInsufficientPrivi, "tappa_app deleting a key")
	opWant(t, asApp(`UPDATE api_keys SET token_hash = repeat('a', 64) WHERE id = $1`), sqlstateInsufficientPrivi, "tappa_app rewriting the hash")
	opWant(t, asApp(`UPDATE api_keys SET tenant_id = tenant_id WHERE id = $1`), sqlstateInsufficientPrivi, "tappa_app moving a key's tenant")

	fresh := w.addKey(t, ctx, tx, "too early", randHash(t))
	opWant(t, opTry(t, ctx, tx, `UPDATE api_keys SET revoked_at = created_at - interval '1 second' WHERE id = $1`, fresh),
		sqlstateCheckViolation, "a first stamp before the key existed")

	// Control: without the trigger the un-revoke would have gone through.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `DROP TRIGGER api_keys_revocation_one_way ON api_keys`); err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `UPDATE api_keys SET revoked_at = NULL WHERE id = $1`, id); err != nil {
		t.Fatalf("CONTROL: with the trigger dropped the un-revoke still failed: %v", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestRLS_ApiKeys_AnotherTenantsKeysAreInvisible is CLAUDE.md §8's RLS test for the new table, in
// the FILTERLESS shape (§6): as tappa_app in B's context a bare `WHERE id = <A's key>` is 0 rows
// and the positive control in A's context is 1; B cannot insert a key stamped with A's tenant
// (42501, WITH CHECK), cannot revoke A's key (0 rows) and cannot read A's key through the
// resolver's columns either way -- the resolver is the one context-less door and it takes a hash.
func TestRLS_ApiKeys_AnotherTenantsKeysAreInvisible(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newAPIKeyWorld(t, ctx, tx), newAPIKeyWorld(t, ctx, tx)
	keyA := a.addKey(t, ctx, tx, "A's", randHash(t))
	count := func(ctxTenant uuid.UUID, sql string, args ...any) int64 {
		var n int64
		if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, ctxTenant.String()); err != nil {
				return err
			}
			return sp.QueryRow(ctx, sql, args...).Scan(&n)
		}); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	if n := count(b.tenant, `SELECT count(*) FROM api_keys WHERE id = $1`, keyA); n != 0 {
		t.Fatalf("B's context sees %d of A's keys, want 0", n)
	}
	if n := count(a.tenant, `SELECT count(*) FROM api_keys WHERE id = $1`, keyA); n != 1 {
		t.Fatalf("POSITIVE CONTROL: A's context sees %d of its own keys, want 1", n)
	}
	if n := count(b.tenant, `WITH u AS (UPDATE api_keys SET revoked_at = now() WHERE id = $1 RETURNING 1) SELECT count(*) FROM u`, keyA); n != 0 {
		t.Fatalf("B revoked %d of A's keys, want 0", n)
	}
	opWant(t, opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, b.tenant.String()); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `INSERT INTO api_keys (tenant_id, name, token_hash, prefix_hint, created_by)
		                         VALUES ($1, 'forged', $2, 'tt_live_AbCd', $3)`, a.tenant, randHash(t), a.owner)
		return err
	}), sqlstateInsufficientPrivi, "B inserting a key stamped with A's tenant")
	// No context at all: 0 rows, not an error (NULLIF).
	if n := count(uuid.Nil, `SELECT count(*) FROM api_keys WHERE id = $1`, keyA); n != 0 {
		t.Fatalf("the nil tenant's context sees %d keys", n)
	}
}

// TestApiKeys00051_DownGivesBackAndUpTakesItAgain: 00051's Down removes the table, both functions
// and tappa_resolver's two columns of tenants -- the role reaches no column of tenants after it,
// as before -- and the Up builds them again to the same catalogue (the resolver's signature and
// the grants). Walked in a holdingLockedSavepoint (the Down's DROP TABLE takes tenants and
// admin_users, which holdingDownTables lists). Since API-4, 00052's api_idempotency keys on
// api_keys, so its Down runs first and its Up last.
func TestApiKeys00051_DownGivesBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00051File)
	up52, down52 := opMigrationSections(t, op00052File)
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down52, "00052 Down")
		opRunSection(t, ctx, sp, down, "00051 Down")
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_class WHERE relname = 'api_keys'`); n != 0 {
			t.Fatalf("api_keys survives the Down")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE proname IN ('resolve_api_key_by_hash', 'api_keys_revocation_is_one_way')`); n != 0 {
			t.Fatalf("%d of 00051's functions survive the Down", n)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_attribute a WHERE a.attrelid = 'public.tenants'::regclass AND a.attnum > 0
		                            AND has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT')`); n != 0 {
			t.Fatalf("tappa_resolver still reads %d columns of tenants after the Down", n)
		}
		opRunSection(t, ctx, sp, up, "00051 Up again")
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_attribute a WHERE a.attrelid = 'public.tenants'::regclass AND a.attnum > 0
		                            AND has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT')`); n != 2 {
			t.Fatalf("tappa_resolver reads %d columns of tenants after the Up, want 2", n)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE oid = 'public.resolve_api_key_by_hash(text)'::regprocedure AND prosecdef`); n != 1 {
			t.Fatal("the resolver is not back as a definer")
		}
		opRunSection(t, ctx, sp, up52, "00052 Up again")
	})
}
