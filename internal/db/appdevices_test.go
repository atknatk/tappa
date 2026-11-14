package db

// appdevices_test.go -- migration 00104 (M16-03, ADR 0045 §2.3): app_devices, app_request_nonces
// and the device resolver, resolve_app_device_by_id, against real Postgres. Nothing here prints a
// key, a digest, a nonce or its hash: values are compared, never shown (§4.7, §7).

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

const op00104File = "00104_create_app_devices_and_request_nonces.sql"

// appDevicesMigration is 00104's number.
const appDevicesMigration = 104

// appDevicesSetAsideSQL removes every device and nonce inside a walk's transaction so 00104's Down --
// which refuses while either table holds a row -- can run; the rollback brings them back. No trigger
// guards a delete on either table (a device is protected from tappa_app by the privilege, not by a
// trigger), so the owner needs nothing switched off.
const appDevicesSetAsideSQL = `DELETE FROM public.app_request_nonces;
DELETE FROM public.app_devices;`

// appTestKey is a fresh P-256 public key in the stored form (65 bytes, 0x04 || X || Y). The
// private half is thrown away: no test here signs anything.
func appTestKey(t *testing.T) []byte {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a P-256 key: %v", err)
	}
	return k.PublicKey().Bytes()
}

// appDeviceWorld is a tenant with a venue and an active employee, inside the owner's test
// transaction (rolled back with it).
type appDeviceWorld struct {
	tenant, venue, employee uuid.UUID
}

func newAppDeviceWorld(t *testing.T, ctx context.Context, tx pgx.Tx) appDeviceWorld {
	t.Helper()
	w := appDeviceWorld{tenant: uuid.New(), venue: uuid.New(), employee: uuid.New()}
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'm16-03', $2, 'restaurant', 'multi')`,
			[]any{w.tenant, "VAT-M1603-" + w.tenant.String()}},
		{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm16-03 venue')`, []any{w.venue, w.tenant}},
		{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Maria Borg', 'active')`,
			[]any{w.employee, w.tenant, w.venue}},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("app device world: %v", err)
		}
	}
	return w
}

// asApp runs fn as tappa_app in tenant's context, inside a savepoint of the owner's transaction.
// uuid.Nil means NO context: the setting is written ” -- what a cleared GUC reads as (CLAUDE.md
// §6, Q27) -- because a released savepoint keeps the previous call's set_config(..., true) alive in
// the parent transaction, so leaving it alone would inherit the last tenant.
func (w appDeviceWorld) asApp(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, fn func(sp pgx.Tx) error) error {
	t.Helper()
	return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		value := ""
		if tenant != uuid.Nil {
			value = tenant.String()
		}
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, value); err != nil {
			return err
		}
		return fn(sp)
	})
}

// addDevice creates a device through the product's query, as tappa_app in the world's context.
func (w appDeviceWorld) addDevice(t *testing.T, ctx context.Context, tx pgx.Tx, key []byte) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		label := "iPhone"
		row, err := store.New(sp).CreateAppDevice(ctx, store.CreateAppDeviceParams{
			TenantID: w.tenant, EmployeeID: w.employee, Platform: "ios", PublicKey: key, DeviceLabel: &label,
		})
		id = row.ID
		return err
	}); err != nil {
		t.Fatalf("create app device: %v", err)
	}
	return id
}

// issueNonce issues a nonce for device through the product's query and returns its hash.
func (w appDeviceWorld) issueNonce(t *testing.T, ctx context.Context, tx pgx.Tx, device uuid.UUID) string {
	t.Helper()
	hash := opRandHex(t)
	if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).IssueAppRequestNonce(ctx, store.IssueAppRequestNonceParams{
			NonceHash: hash, TtlSeconds: 60, DeviceID: device, TenantID: w.tenant,
		})
		return err
	}); err != nil {
		t.Fatalf("issue nonce: %v", err)
	}
	return hash
}

// TestAppDevicesCatalog measures, in the live catalog, ADR 0002 md.7's five constraints on the
// device resolver and the RLS five on both tables:
//
//	definer   owner tappa_resolver (NOSUPERUSER, BYPASSRLS), SECURITY DEFINER, proconfig exactly
//	          search_path = pg_catalog, pg_temp, STABLE, one function of that name (no overload)
//	(i)       the argument list is exactly the device id
//	(ii)      at most one row: id is the primary key -- measured live: a known id 1 row, unknown 0
//	(iii)     the result is exactly (id, tenant_id, employee_id, public_key, revoked_at)
//	(iv)      EXECUTE: tappa_app, PUBLIC and the other roles not; tappa_resolver reads exactly the
//	          five columns of app_devices and nothing of app_request_nonces; tappa_app cannot read
//	          public_key or key_digest, and a context-less tappa_app sees no device
//	(v)       each table's one policy is the standard NULLIF form with no OR branch
//	RLS five  ENABLE + FORCE, an index led by tenant_id, tenant_id NOT NULL
//	grants    tappa_app's verbs and columns, both tables (ADR 0028 §1's `arwd` rule)
//	triggers  both one-way functions refuse another table (TG_RELID); EXECUTE in their owner alone
func TestAppDevicesCatalog(t *testing.T) {
	ctx, tx := opTx(t)
	var (
		owner, cfg, args, result, body, volatile string
		super, bypass, secdef                    bool
	)
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_userbyid(p.proowner), r.rolsuper, r.rolbypassrls, p.prosecdef, array_to_string(p.proconfig, '|'),
		       pg_get_function_arguments(p.oid), pg_get_function_result(p.oid), p.prosrc, p.provolatile::text
		  FROM pg_proc p JOIN pg_roles r ON r.oid = p.proowner
		 WHERE p.oid = 'public.resolve_app_device_by_id(uuid)'::regprocedure`).
		Scan(&owner, &super, &bypass, &secdef, &cfg, &args, &result, &body, &volatile); err != nil {
		t.Fatalf("read the resolver: %v", err)
	}
	if owner != "tappa_resolver" || super || !bypass || !secdef {
		t.Errorf("definer: owner %s super %v bypassrls %v secdef %v; want tappa_resolver, false, true, true", owner, super, bypass, secdef)
	}
	if cfg != "search_path=pg_catalog, pg_temp" || volatile != "s" {
		t.Errorf("proconfig %q, volatility %q; want the fixed search_path and STABLE", cfg, volatile)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname = 'resolve_app_device_by_id'`); n != 1 {
		t.Errorf("%d functions named resolve_app_device_by_id; an overload is a second door", n)
	}
	if args != "p_device_id uuid" {
		t.Errorf("(i) arguments %q, want exactly the device id", args)
	}
	if want := "TABLE(id uuid, tenant_id uuid, employee_id uuid, public_key bytea, revoked_at timestamp with time zone)"; result != want {
		t.Errorf("(iii) result %q, want %q", result, want)
	}
	if !strings.Contains(body, "public.app_devices") || strings.Contains(body, "app_request_nonces") ||
		strings.Contains(strings.ToUpper(body), "EXECUTE") || strings.Contains(body, "format(") || strings.Contains(body, "*") {
		t.Errorf("body: want qualified app_devices only, no SELECT * and no dynamic SQL:\n%s", body)
	}
	if n := opInt(t, ctx, tx, `
		SELECT count(*) FROM pg_constraint c JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		 WHERE c.conrelid = 'public.app_devices'::regclass AND c.contype = 'p' AND array_length(c.conkey, 1) = 1
		   AND a.attname = 'id'`); n != 1 {
		t.Errorf("(ii) app_devices' primary key is not (id) alone (%d)", n)
	}
	for _, c := range []struct {
		who  string
		want bool
	}{{"public", false}, {"tappa_app", true}, {"tappa_operator", false}, {"tappa_opdefiner", false},
		{"tappa_holdingdefiner", false}, {"tappa_feedwriter", false}, {"tappa_identitydefiner", false}} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, 'public.resolve_app_device_by_id(uuid)', 'EXECUTE')`, c.who).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may != c.want {
			t.Errorf("(iv) %s EXECUTE = %v, want %v", c.who, may, c.want)
		}
	}

	for _, table := range []string{"app_devices", "app_request_nonces"} {
		var policy string
		if err := tx.QueryRow(ctx, `SELECT pg_get_expr(polqual, polrelid) || ' / ' || pg_get_expr(polwithcheck, polrelid)
		                              FROM pg_policy WHERE polrelid = ('public.' || $1)::regclass`, table).Scan(&policy); err != nil {
			t.Fatalf("(v) %s's one policy: %v", table, err)
		}
		if strings.Count(policy, "NULLIF") != 2 || strings.Contains(strings.ToUpper(policy), " OR ") {
			t.Errorf("(v) %s policy %q, want the NULLIF form in USING and WITH CHECK and no OR branch", table, policy)
		}
		var enabled, forced, notNull bool
		if err := tx.QueryRow(ctx, `SELECT c.relrowsecurity, c.relforcerowsecurity, a.attnotnull
		                              FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id'
		                             WHERE c.oid = ('public.' || $1)::regclass`, table).Scan(&enabled, &forced, &notNull); err != nil {
			t.Fatal(err)
		}
		if !enabled || !forced || !notNull {
			t.Errorf("%s: RLS ENABLE %v FORCE %v, tenant_id NOT NULL %v", table, enabled, forced, notNull)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
		                            WHERE i.indrelid = ('public.' || $1)::regclass AND a.attname = 'tenant_id'`, table); n < 1 {
			t.Errorf("%s: no index leads with tenant_id", table)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy WHERE polrelid = ('public.' || $1)::regclass`, table); n != 1 {
			t.Errorf("%s holds %d policies, want 1 (a second permissive policy ORs into the first)", table, n)
		}
	}

	// tappa_app's reach, column by column (ADR 0028 §1).
	for _, c := range []struct{ table, priv, want string }{
		{"app_devices", "SELECT", "id,tenant_id,employee_id,platform,device_label,created_at,last_used_at,revoked_at"},
		{"app_devices", "INSERT", "tenant_id,employee_id,platform,public_key,device_label"},
		{"app_devices", "UPDATE", "last_used_at,revoked_at"},
		{"app_request_nonces", "SELECT", "id,tenant_id,device_id,nonce_hash,created_at,expires_at,consumed_at"},
		{"app_request_nonces", "INSERT", "tenant_id,device_id,nonce_hash,expires_at"},
		{"app_request_nonces", "UPDATE", "consumed_at"},
	} {
		if got := opColumns(t, ctx, tx, "tappa_app", c.table, c.priv); got != c.want {
			t.Errorf("tappa_app %s on %s: %q, want %q", c.priv, c.table, got, c.want)
		}
	}
	for _, c := range []struct {
		table, priv string
		want        bool
	}{
		{"app_devices", "DELETE", false}, {"app_devices", "TRUNCATE", false}, {"app_devices", "TRIGGER", false},
		{"app_devices", "REFERENCES", false},
		{"app_request_nonces", "DELETE", true}, {"app_request_nonces", "TRUNCATE", false},
		{"app_request_nonces", "TRIGGER", false}, {"app_request_nonces", "REFERENCES", false},
	} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', ('public.' || $1)::regclass, $2)`, c.table, c.priv).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may != c.want {
			t.Errorf("tappa_app %s on %s = %v, want %v", c.priv, c.table, may, c.want)
		}
	}

	// tappa_resolver: the five columns of app_devices, SELECT only; nothing of app_request_nonces.
	if got := opColumns(t, ctx, tx, "tappa_resolver", "app_devices", "SELECT"); got != "id,tenant_id,employee_id,public_key,revoked_at" {
		t.Errorf("tappa_resolver SELECT on app_devices: %q, want the resolution's five", got)
	}
	for _, priv := range []string{"INSERT", "UPDATE", "REFERENCES"} {
		if got := opColumns(t, ctx, tx, "tappa_resolver", "app_devices", priv); got != "" {
			t.Errorf("tappa_resolver %s on app_devices: %q, want none", priv, got)
		}
	}
	for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "REFERENCES"} {
		if got := opColumns(t, ctx, tx, "tappa_resolver", "app_request_nonces", priv); got != "" {
			t.Errorf("tappa_resolver %s on app_request_nonces: %q, want none", priv, got)
		}
	}

	// Live: (ii) one row for a known id, none for an unknown one; the revocation is carried.
	w := newAppDeviceWorld(t, ctx, tx)
	key := appTestKey(t)
	device := w.addDevice(t, ctx, tx, key)
	if err := w.asApp(t, ctx, tx, uuid.Nil, func(sp pgx.Tx) error {
		var got ResolvedAppDevice
		if err := sp.QueryRow(ctx, getAppDeviceByIDSQL, device).Scan(&got.ID, &got.TenantID, &got.EmployeeID, &got.PublicKey, &got.RevokedAt); err != nil {
			return err
		}
		if got.ID != device || got.TenantID != w.tenant || got.EmployeeID != w.employee || got.RevokedAt != nil {
			t.Errorf("resolved %v/%v/%v revoked %v", got.ID, got.TenantID, got.EmployeeID, got.RevokedAt != nil)
		}
		if string(got.PublicKey) != string(key) {
			t.Error("the resolved public key is not the stored one")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM resolve_app_device_by_id($1)`, uuid.New()); n != 0 {
			t.Errorf("an unknown device id: %d rows", n)
		}
		// Without the function and without a context, tappa_app sees no device (FORCE RLS; NULLIF).
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM app_devices WHERE id = $1`, device); n != 0 {
			t.Errorf("a context-less tappa_app sees %d devices, want 0", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("resolve as tappa_app: %v", err)
	}
	opWant(t, w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `SELECT public_key FROM app_devices WHERE id = $1`, device)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app reading public_key directly")
	opWant(t, w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `SELECT key_digest FROM app_devices WHERE id = $1`, device)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app reading key_digest directly")

	// The trigger functions: EXECUTE in their owner alone; bound elsewhere they refuse.
	for _, fn := range []string{"app_devices_revocation_is_one_way", "app_request_nonces_consumption_is_one_way"} {
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname = $1
		                            AND NOT prosecdef AND proacl::text = '{tappa_owner=X/tappa_owner}'`, fn); n != 1 {
			t.Errorf("%s is not an INVOKER function with EXECUTE in its owner alone", fn)
		}
	}
	for _, s := range []string{
		`CREATE TEMP TABLE zz_m1603_bound (revoked_at timestamptz, consumed_at timestamptz)`,
		`CREATE TRIGGER zz_m1603_a BEFORE UPDATE ON zz_m1603_bound FOR EACH ROW EXECUTE FUNCTION public.app_devices_revocation_is_one_way()`,
		`INSERT INTO zz_m1603_bound VALUES (now(), now())`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	opWant(t, opTry(t, ctx, tx, `UPDATE zz_m1603_bound SET revoked_at = NULL`), sqlstatePrerequisiteState,
		"the device trigger function on a table that is not app_devices")
	if _, err := tx.Exec(ctx, `DROP TRIGGER zz_m1603_a ON zz_m1603_bound`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `CREATE TRIGGER zz_m1603_n BEFORE UPDATE ON zz_m1603_bound FOR EACH ROW EXECUTE FUNCTION public.app_request_nonces_consumption_is_one_way()`); err != nil {
		t.Fatal(err)
	}
	opWant(t, opTry(t, ctx, tx, `UPDATE zz_m1603_bound SET consumed_at = NULL`), sqlstatePrerequisiteState,
		"the nonce trigger function on a table that is not app_request_nonces")
}

// TestRLS_AppDevices_TenantIsolation is CLAUDE.md §8's RLS test for both new tables, in the
// FILTERLESS shape (§6): as tappa_app in B's context a bare `WHERE id = <A's row>` is 0 rows and
// the positive control in A's context is 1 -- so the 0 is RLS's, not a WHERE's. B cannot write a
// row stamped with A's tenant -- WITH CHECK refuses it, 42501 -- cannot revoke A's device or consume A's nonce
// (0 rows), and a context-less tappa_app sees nothing (NULLIF).
//
// MUTANT (measured, inside the test's transaction): the policy dropped and RLS disabled on
// app_devices -> B's bare read sees A's device; the test's own control at the end.
func TestRLS_AppDevices_TenantIsolation(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	deviceA := a.addDevice(t, ctx, tx, appTestKey(t))
	hashA := a.issueNonce(t, ctx, tx, deviceA)
	count := func(ctxTenant uuid.UUID, sql string, args ...any) int64 {
		var n int64
		if err := a.asApp(t, ctx, tx, ctxTenant, func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, sql, args...).Scan(&n)
		}); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	for _, c := range []struct {
		sql string
		arg any
	}{
		{`SELECT count(*) FROM app_devices WHERE id = $1`, deviceA},
		{`SELECT count(*) FROM app_request_nonces WHERE device_id = $1`, deviceA},
	} {
		if n := count(b.tenant, c.sql, c.arg); n != 0 {
			t.Fatalf("B's context: %q = %d, want 0", c.sql, n)
		}
		if n := count(a.tenant, c.sql, c.arg); n != 1 {
			t.Fatalf("POSITIVE CONTROL: A's context: %q = %d, want 1", c.sql, n)
		}
		if n := count(uuid.Nil, c.sql, c.arg); n != 0 {
			t.Fatalf("no context: %q = %d, want 0", c.sql, n)
		}
	}
	if n := count(b.tenant, `WITH u AS (UPDATE app_devices SET revoked_at = now() WHERE id = $1 RETURNING 1) SELECT count(*) FROM u`, deviceA); n != 0 {
		t.Fatalf("B revoked %d of A's devices, want 0", n)
	}
	if n := count(b.tenant, `WITH u AS (UPDATE app_request_nonces SET consumed_at = now() WHERE nonce_hash = $1 RETURNING 1) SELECT count(*) FROM u`, hashA); n != 0 {
		t.Fatalf("B consumed %d of A's nonces, want 0", n)
	}
	if n := count(b.tenant, `WITH d AS (DELETE FROM app_request_nonces WHERE nonce_hash = $1 RETURNING 1) SELECT count(*) FROM d`, hashA); n != 0 {
		t.Fatalf("B deleted %d of A's nonces, want 0", n)
	}
	opWant(t, b.asApp(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `INSERT INTO app_devices (tenant_id, employee_id, platform, public_key)
		                         VALUES ($1, $2, 'android', $3)`, a.tenant, a.employee, appTestKey(t))
		return err
	}), sqlstateInsufficientPrivi, "B inserting a device stamped with A's tenant")
	opWant(t, b.asApp(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `INSERT INTO app_request_nonces (tenant_id, device_id, nonce_hash, expires_at)
		                         VALUES ($1, $2, $3, now() + interval '1 minute')`, a.tenant, deviceA, opRandHex(t))
		return err
	}), sqlstateInsufficientPrivi, "B inserting a nonce stamped with A's tenant for A's device")
	// B's own tenant on A's device: the composite key refuses it (a nonce cannot name another
	// tenant's device).
	opWant(t, b.asApp(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `INSERT INTO app_request_nonces (tenant_id, device_id, nonce_hash, expires_at)
		                         VALUES ($1, $2, $3, now() + interval '1 minute')`, b.tenant, deviceA, opRandHex(t))
		return err
	}), sqlstateForeignKeyViolation, "B inserting a nonce for A's device under B's tenant")
	// A's device stays live and its nonce unconsumed after all of B's attempts.
	if n := count(a.tenant, `SELECT count(*) FROM app_devices WHERE id = $1 AND revoked_at IS NULL`, deviceA); n != 1 {
		t.Fatal("A's device was touched by B's attempts")
	}
	if n := count(a.tenant, `SELECT count(*) FROM app_request_nonces WHERE nonce_hash = $1 AND consumed_at IS NULL`, hashA); n != 1 {
		t.Fatal("A's nonce was touched by B's attempts")
	}

	// Control: with RLS off on app_devices, B's bare read sees A's device -- so the 0 above is RLS's.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`ALTER TABLE app_devices NO FORCE ROW LEVEL SECURITY`, `ALTER TABLE app_devices DISABLE ROW LEVEL SECURITY`} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	var n int64
	if err := opAs(t, ctx, sp, "tappa_app", func(as pgx.Tx) error {
		if _, err := as.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, b.tenant.String()); err != nil {
			return err
		}
		return as.QueryRow(ctx, `SELECT count(*) FROM app_devices WHERE id = $1`, deviceA).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CONTROL: with RLS off B's bare read sees %d of A's devices, want 1 -- the probe is not reaching the row", n)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestAppDevices_RevocationIsOneWay (00099's shape): as tappa_app, a revoked device cannot be made
// live again nor re-stamped, and a first stamp cannot be back-dated or set in the future; the
// owner is held by the trigger too. The product's statements still work: the single revoke (and
// its idempotent repeat, same stamp back), the bulk revoke, the last-use refresh -- which refuses a
// revoked device. tappa_app cannot DELETE a device or rewrite its key, person or tenant.
//
// MUTANT (measured, in a savepoint): the trigger dropped -> the un-revoke succeeds, the control.
func TestAppDevices_RevocationIsOneWay(t *testing.T) {
	ctx, tx := opTx(t)
	w := newAppDeviceWorld(t, ctx, tx)
	first, second, third := w.addDevice(t, ctx, tx, appTestKey(t)), w.addDevice(t, ctx, tx, appTestKey(t)), w.addDevice(t, ctx, tx, appTestKey(t))
	asApp := func(fn func(q *store.Queries) error) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error { return fn(store.New(sp)) })
	}
	exec := func(sql string, args ...any) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
	}

	if err := asApp(func(q *store.Queries) error {
		_, err := q.TouchAppDevice(ctx, store.TouchAppDeviceParams{ID: first, TenantID: w.tenant})
		return err
	}); err != nil {
		t.Fatalf("touch a live device: %v", err)
	}
	var stamp1, stamp2 store.RevokeAppDeviceRow
	if err := asApp(func(q *store.Queries) error {
		var err error
		if stamp1, err = q.RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: first, TenantID: w.tenant}); err != nil {
			return err
		}
		stamp2, err = q.RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: first, TenantID: w.tenant})
		return err
	}); err != nil {
		t.Fatalf("revoke, then revoke again: %v", err)
	}
	if stamp1.RevokedAt == nil || stamp2.RevokedAt == nil || !stamp1.RevokedAt.Equal(*stamp2.RevokedAt) {
		t.Fatal("the repeated revoke did not give the first stamp back")
	}
	if err := asApp(func(q *store.Queries) error {
		_, err := q.TouchAppDevice(ctx, store.TouchAppDeviceParams{ID: first, TenantID: w.tenant})
		return err
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("touching a revoked device: %v, want pgx.ErrNoRows (fail closed)", err)
	}
	if err := asApp(func(q *store.Queries) error {
		_, err := q.RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: uuid.New(), TenantID: w.tenant})
		return err
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revoking an unknown device: %v, want pgx.ErrNoRows", err)
	}

	opWant(t, exec(`UPDATE app_devices SET revoked_at = NULL WHERE id = $1`, first), sqlstateRestrictViolation, "tappa_app un-revoking")
	opWant(t, exec(`UPDATE app_devices SET revoked_at = revoked_at + interval '1 second' WHERE id = $1`, first), sqlstateRestrictViolation, "tappa_app moving the stamp")
	opWant(t, exec(`UPDATE app_devices SET revoked_at = now() - interval '1 minute' WHERE id = $1`, second), sqlstateRestrictViolation, "a back-dated first stamp")
	opWant(t, exec(`UPDATE app_devices SET revoked_at = clock_timestamp() + interval '1 minute' WHERE id = $1`, second), sqlstateRestrictViolation, "a future first stamp")
	opWant(t, opTry(t, ctx, tx, `UPDATE app_devices SET revoked_at = NULL WHERE id = $1`, first), sqlstateRestrictViolation, "the owner un-revoking")
	opWant(t, exec(`DELETE FROM app_devices WHERE id = $1`, first), sqlstateInsufficientPrivi, "tappa_app deleting a device")
	opWant(t, exec(`UPDATE app_devices SET public_key = $2 WHERE id = $1`, second, appTestKey(t)), sqlstateInsufficientPrivi, "tappa_app rewriting the key")
	opWant(t, exec(`UPDATE app_devices SET employee_id = employee_id WHERE id = $1`, second), sqlstateInsufficientPrivi, "tappa_app moving a device to another person")
	opWant(t, exec(`UPDATE app_devices SET tenant_id = tenant_id WHERE id = $1`, second), sqlstateInsufficientPrivi, "tappa_app moving a device's tenant")

	var revoked []uuid.UUID
	if err := asApp(func(q *store.Queries) error {
		var err error
		revoked, err = q.RevokeAppDevicesForEmployee(ctx, store.RevokeAppDevicesForEmployeeParams{TenantID: w.tenant, EmployeeID: w.employee})
		return err
	}); err != nil {
		t.Fatalf("the bulk revoke: %v", err)
	}
	if len(revoked) != 2 || !((revoked[0] == second && revoked[1] == third) || (revoked[0] == third && revoked[1] == second)) {
		t.Fatalf("the bulk revoke returned %v, want the two live devices (the revoked one untouched)", revoked)
	}

	// Control: without the trigger the un-revoke would have gone through.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `DROP TRIGGER app_devices_revocation_one_way ON app_devices`); err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `UPDATE app_devices SET revoked_at = NULL WHERE id = $1`, first); err != nil {
		t.Fatalf("CONTROL: with the trigger dropped the un-revoke still failed: %v", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestAppDevices_KeyShapeAndDigest: the digest is the database's -- hex SHA-256 of the
// stored key, GENERATED, not writable by anyone -- and globally UNIQUE, so one key is one device
// across every tenant, revoked or not. The key is the 65-byte uncompressed point; a 64-byte or
// compressed form, a platform outside the two, and a label that is not coarse are refused.
func TestAppDevices_KeyShapeAndDigest(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	key := appTestKey(t)
	device := a.addDevice(t, ctx, tx, key)
	sum := sha256.Sum256(key)
	var match bool
	if err := tx.QueryRow(ctx, `SELECT key_digest = $2 FROM app_devices WHERE id = $1`, device, hex.EncodeToString(sum[:])).Scan(&match); err != nil {
		t.Fatal(err)
	}
	if !match {
		t.Fatal("key_digest is not the hex SHA-256 of the stored key")
	}
	opWant(t, opTry(t, ctx, tx, `UPDATE app_devices SET key_digest = repeat('0', 64) WHERE id = $1`, device),
		"428C9", "the owner writing the generated digest")

	insert := func(w appDeviceWorld, platform string, pk []byte, label *string) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
			_, err := store.New(sp).CreateAppDevice(ctx, store.CreateAppDeviceParams{
				TenantID: w.tenant, EmployeeID: w.employee, Platform: platform, PublicKey: pk, DeviceLabel: label,
			})
			return err
		})
	}
	opWant(t, insert(a, "ios", key, nil), sqlstateUniqueViolation, "the same key again in the same tenant")
	opWant(t, insert(b, "android", key, nil), sqlstateUniqueViolation, "the same key in another tenant")
	if err := a.asApp(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: device, TenantID: a.tenant})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	opWant(t, insert(a, "ios", key, nil), sqlstateUniqueViolation, "a revoked device's key under a new row")

	compressed := append([]byte{0x02}, appTestKey(t)[1:33]...)
	badPrefix := appTestKey(t)
	badPrefix[0] = 0x05
	long := "x" + strings.Repeat("y", 64)
	blank, ctrl := " iPhone", "iPhone\n"
	for _, c := range []struct {
		what     string
		platform string
		pk       []byte
		label    *string
	}{
		{"the X coordinate alone (64 bytes)", "ios", appTestKey(t)[1:], nil},
		{"a compressed point", "ios", compressed, nil},
		{"a 65-byte value not prefixed 0x04", "ios", badPrefix, nil},
		{"a platform outside ios/android", "windows", appTestKey(t), nil},
		{"a label longer than 64", "ios", appTestKey(t), &long},
		{"a label with a leading blank", "ios", appTestKey(t), &blank},
		{"a label with a control character", "ios", appTestKey(t), &ctrl},
	} {
		opWant(t, insert(a, c.platform, c.pk, c.label), sqlstateCheckViolation, c.what)
	}
	if err := insert(a, "android", appTestKey(t), nil); err != nil {
		t.Fatalf("POSITIVE CONTROL: a well-formed android device with no label: %v", err)
	}
}

// TestAppRequestNonces_SingleUse drives the product's four nonce queries through every answer the
// verifier (M16-05) has to tell apart from success: a live nonce is consumed once; again, it is
// refused; another device's nonce, an expired nonce and an unknown hash are refused; a revoked
// device is issued nothing; a lifetime over 5 minutes is refused; the live count and the dead sweep
// see what they should -- and the sweep never takes a live nonce. consumed_at is one way.
func TestAppRequestNonces_SingleUse(t *testing.T) {
	ctx, tx := opTx(t)
	w := newAppDeviceWorld(t, ctx, tx)
	device, other := w.addDevice(t, ctx, tx, appTestKey(t)), w.addDevice(t, ctx, tx, appTestKey(t))
	q := func(fn func(q *store.Queries) error) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error { return fn(store.New(sp)) })
	}
	consume := func(dev uuid.UUID, hash string) error {
		return q(func(q *store.Queries) error {
			_, err := q.ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{TenantID: w.tenant, DeviceID: dev, NonceHash: hash})
			return err
		})
	}

	live := w.issueNonce(t, ctx, tx, device)
	spare := w.issueNonce(t, ctx, tx, device)
	otherNonce := w.issueNonce(t, ctx, tx, other)
	if err := consume(other, live); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another device consuming this device's nonce: %v, want pgx.ErrNoRows", err)
	}
	if err := consume(device, live); err != nil {
		t.Fatalf("POSITIVE CONTROL: consuming a live nonce: %v", err)
	}
	if err := consume(device, live); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("consuming it again: %v, want pgx.ErrNoRows (replay)", err)
	}
	if err := consume(device, opRandHex(t)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an unknown hash: %v, want pgx.ErrNoRows", err)
	}
	// An expired nonce: written by the owner with a past lifetime (tappa_app cannot set created_at).
	expired := opRandHex(t)
	if _, err := tx.Exec(ctx, `INSERT INTO app_request_nonces (tenant_id, device_id, nonce_hash, created_at, expires_at)
	                           VALUES ($1, $2, $3, now() - interval '2 minutes', now() - interval '1 minute')`, w.tenant, device, expired); err != nil {
		t.Fatal(err)
	}
	if err := consume(device, expired); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an expired nonce: %v, want pgx.ErrNoRows", err)
	}

	// consumed_at is one way, for tappa_app and for the owner.
	exec := func(sql string, args ...any) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
	}
	opWant(t, exec(`UPDATE app_request_nonces SET consumed_at = NULL WHERE nonce_hash = $1`, live), sqlstateRestrictViolation, "tappa_app un-consuming a nonce")
	opWant(t, opTry(t, ctx, tx, `UPDATE app_request_nonces SET consumed_at = NULL WHERE nonce_hash = $1`, live), sqlstateRestrictViolation, "the owner un-consuming a nonce")
	opWant(t, exec(`UPDATE app_request_nonces SET consumed_at = now() - interval '1 second' WHERE nonce_hash = $1`, spare), sqlstateRestrictViolation, "a back-dated consumption")
	opWant(t, exec(`UPDATE app_request_nonces SET expires_at = expires_at + interval '1 minute' WHERE nonce_hash = $1`, spare), sqlstateInsufficientPrivi, "tappa_app extending a nonce")
	opWant(t, exec(`INSERT INTO app_request_nonces (tenant_id, device_id, nonce_hash, created_at, expires_at)
	                 VALUES ($1, $2, $3, now() - interval '1 hour', now())`, w.tenant, device, opRandHex(t)), sqlstateInsufficientPrivi, "tappa_app writing created_at")

	// The lifetime cap.
	opWant(t, q(func(q *store.Queries) error {
		_, err := q.IssueAppRequestNonce(ctx, store.IssueAppRequestNonceParams{NonceHash: opRandHex(t), TtlSeconds: 301, DeviceID: device, TenantID: w.tenant})
		return err
	}), sqlstateCheckViolation, "a nonce living longer than 5 minutes")
	opWant(t, q(func(q *store.Queries) error {
		_, err := q.IssueAppRequestNonce(ctx, store.IssueAppRequestNonceParams{NonceHash: "NOT-HEX", TtlSeconds: 60, DeviceID: device, TenantID: w.tenant})
		return err
	}), sqlstateCheckViolation, "a hash that is not 64 lower-case hex")

	// The count and the sweep: device holds spare (live), live (consumed), expired (dead).
	var n int32
	if err := q(func(q *store.Queries) error {
		var err error
		n, err = q.CountLiveAppRequestNonces(ctx, store.CountLiveAppRequestNoncesParams{TenantID: w.tenant, DeviceID: device})
		return err
	}); err != nil || n != 1 {
		t.Fatalf("live nonces of the device: %d (%v), want 1", n, err)
	}
	var swept int64
	if err := q(func(q *store.Queries) error {
		var err error
		swept, err = q.DeleteDeadAppRequestNonces(ctx, store.DeleteDeadAppRequestNoncesParams{TenantID: w.tenant, DeviceID: device})
		return err
	}); err != nil || swept != 2 {
		t.Fatalf("the sweep deleted %d (%v), want 2 (the consumed and the expired)", swept, err)
	}
	if err := consume(device, spare); err != nil {
		t.Fatalf("the live nonce after the sweep: %v, want it consumable", err)
	}
	if err := consume(other, otherNonce); err != nil {
		t.Fatalf("the other device's nonce after this device's sweep: %v, want it consumable", err)
	}

	// A revoked device is issued nothing, and an unknown device too.
	if err := q(func(q *store.Queries) error {
		_, err := q.RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: device, TenantID: w.tenant})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, dev := range []uuid.UUID{device, uuid.New()} {
		if err := q(func(q *store.Queries) error {
			_, err := q.IssueAppRequestNonce(ctx, store.IssueAppRequestNonceParams{NonceHash: opRandHex(t), TtlSeconds: 60, DeviceID: dev, TenantID: w.tenant})
			return err
		}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("a nonce for a revoked or unknown device: %v, want pgx.ErrNoRows", err)
		}
	}
}

// TestAppRequestNonces_ConcurrentConsumeExactlyOnce is CLAUDE.md §8's replay test for the nonce:
// N goroutines, each in its own WithTenant transaction on its own pooled connection, consume the
// SAME nonce at once -- exactly one RETURNING row, every other one pgx.ErrNoRows. Run with -race.
// The rows are committed (they must be visible to other connections) and stay inert, the
// rls_test.go fixture convention.
//
// MUTANT (measured 2026-11-14 on the lane database, generated query edited and restored): with
// `AND consumed_at IS NULL` removed from ConsumeAppRequestNonce this test is red -- 31 of 32 fail
// with 23001 from the one-way trigger, the second line of defence -- and TestAppRequestNonces_SingleUse
// is red too: inside one transaction now() is constant, the re-stamp writes the same value, the
// trigger's WHEN is false and the replay SUCCEEDS. The guard in the statement is what refuses it.
func TestAppRequestNonces_ConcurrentConsumeExactlyOnce(t *testing.T) {
	d := appDB(t)
	tenantID, venue := m11Tenant(t, d)
	employee, hash := uuid.New(), opRandHex(t)
	var device uuid.UUID
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status)
		                          VALUES ($1, $2, $3, 'Maria Borg', 'active')`, employee, tenantID, venue); e != nil {
			return e
		}
		q := store.New(tx)
		row, e := q.CreateAppDevice(ctx, store.CreateAppDeviceParams{TenantID: tenantID, EmployeeID: employee, Platform: "ios", PublicKey: appTestKey(t)})
		if e != nil {
			return e
		}
		device = row.ID
		_, e = q.IssueAppRequestNonce(ctx, store.IssueAppRequestNonceParams{NonceHash: hash, TtlSeconds: 120, DeviceID: device, TenantID: tenantID})
		return e
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	const n = 32
	var (
		wg            sync.WaitGroup
		mu            sync.Mutex
		wins, replays int
		others        []error
		start         = make(chan struct{})
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{TenantID: tenantID, DeviceID: device, NonceHash: hash})
				return e
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, pgx.ErrNoRows):
				replays++
			default:
				others = append(others, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(others) > 0 {
		t.Fatalf("%d consumption(s) failed for another reason, first: %v", len(others), others[0])
	}
	if wins != 1 || replays != n-1 {
		t.Fatalf("%d goroutines consumed one nonce: %d succeeded, %d were refused; want exactly 1 and %d", n, wins, replays, n-1)
	}
	t.Logf("%d concurrent consumptions of one nonce: %d succeeded, %d refused", n, wins, replays)
}

// TestAppDevices00104_DownRefusesWithRowsAndRoundTrips: the Down refuses while either table holds
// a row (55000) and changes nothing; with both empty (the rows set aside inside the transaction) it
// takes the tables and the three functions away -- and tappa_resolver's grant with them -- and the
// Up builds the same catalogue again. The tables the Down locks are taken first (lockForDowns: its
// DROPs take ACCESS EXCLUSIVE on tenants and employees, the two keys' tables).
func TestAppDevices00104_DownRefusesWithRowsAndRoundTrips(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00104File)
	lockForDowns(t, ctx, tx, []string{"app_request_nonces", "app_devices", "employees", "tenants"})
	objects := `SELECT (SELECT count(*) FROM pg_class WHERE relname IN ('app_devices', 'app_request_nonces') AND relnamespace = 'public'::regnamespace)
	                 + (SELECT count(*) FROM pg_proc WHERE proname IN ('resolve_app_device_by_id', 'app_devices_revocation_is_one_way',
	                                                                  'app_request_nonces_consumption_is_one_way'))`
	if n := opInt(t, ctx, tx, objects); n != 5 {
		t.Fatalf("%d of 00104's five objects at HEAD", n)
	}
	w := newAppDeviceWorld(t, ctx, tx)
	w.addDevice(t, ctx, tx, appTestKey(t))
	err := holdingTryDown(t, ctx, tx, down)
	opWant(t, err, sqlstatePrerequisiteState, "00104's Down while a device exists")
	if n := opInt(t, ctx, tx, objects); n != 5 {
		t.Fatalf("the refused Down changed something: %d objects", n)
	}
	if _, err := tx.Exec(ctx, appDevicesSetAsideSQL); err != nil {
		t.Fatalf("set the rows aside inside the transaction: %v", err)
	}
	opRunSection(t, ctx, tx, down, "00104 Down")
	if n := opInt(t, ctx, tx, objects); n != 0 {
		t.Fatalf("%d of 00104's objects survive the Down", n)
	}
	opRunSection(t, ctx, tx, up, "00104 Up again")
	if n := opInt(t, ctx, tx, objects); n != 5 {
		t.Fatalf("%d of 00104's objects after the Up, want 5", n)
	}
	if got := opColumns(t, ctx, tx, "tappa_resolver", "app_devices", "SELECT"); got != "id,tenant_id,employee_id,public_key,revoked_at" {
		t.Fatalf("after the Up tappa_resolver reads %q", got)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE oid = 'public.resolve_app_device_by_id(uuid)'::regprocedure AND prosecdef
	                            AND pg_get_userbyid(proowner) = 'tappa_resolver'`); n != 1 {
		t.Fatal("the resolver is not back as tappa_resolver's definer")
	}
}

// TestResolveAppDevice_CarriesTheRevocation: the resolver returns a revoked device -- one row, with
// RevokedAt set -- rather than hiding it. A resolver that filtered on revoked_at would answer
// "unknown device" for a revoked one and the caller could not tell §5 row 3's twin from noise (the
// M16-03 review's mutant `AND d.revoked_at IS NULL` survived every test before this one).
func TestResolveAppDevice_CarriesTheRevocation(t *testing.T) {
	ctx, tx := opTx(t)
	w := newAppDeviceWorld(t, ctx, tx)
	device := w.addDevice(t, ctx, tx, appTestKey(t))
	if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: device, TenantID: w.tenant})
		return err
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := w.asApp(t, ctx, tx, uuid.Nil, func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, getAppDeviceByIDSQL, device)
		if err != nil {
			return err
		}
		got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ResolvedAppDevice, error) {
			var a ResolvedAppDevice
			err := r.Scan(&a.ID, &a.TenantID, &a.EmployeeID, &a.PublicKey, &a.RevokedAt)
			return a, err
		})
		if err != nil {
			return err
		}
		if len(got) != 1 {
			t.Fatalf("a revoked device resolves to %d rows, want 1 (the resolver carries revoked_at, it does not filter on it)", len(got))
		}
		if got[0].ID != device || got[0].TenantID != w.tenant || got[0].RevokedAt == nil {
			t.Fatalf("the revoked device resolved as %v/%v with RevokedAt set = %v; want it, its tenant and a stamp",
				got[0].ID, got[0].TenantID, got[0].RevokedAt != nil)
		}
		return nil
	}); err != nil {
		t.Fatalf("resolve the revoked device: %v", err)
	}
}

// TestAppRequestNonces_RevocationKillsTheNonces (M16-03 security review): issue -> revoke -> consume
// is 0 rows, for the single revoke and for the bulk one, and in this SEQUENTIAL order the revoking
// statement leaves the device no committed nonce; another person's device keeps its nonces. A
// revocation written WITHOUT the nonce delete (here the owner's bare UPDATE) is still refused at
// consumption -- the belt. ⚠️ The delete is not complete under a race: a nonce an issue in flight
// commits behind the revoke's statement snapshot survives it (the security re-check measured it),
// and only the belt refuses that one -- so the belt is load-bearing, never a redundant second check.
func TestAppRequestNonces_RevocationKillsTheNonces(t *testing.T) {
	ctx, tx := opTx(t)
	w, other := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	single, bulk1, bulk2, bare := w.addDevice(t, ctx, tx, appTestKey(t)), w.addDevice(t, ctx, tx, appTestKey(t)),
		w.addDevice(t, ctx, tx, appTestKey(t)), w.addDevice(t, ctx, tx, appTestKey(t))
	otherDevice := other.addDevice(t, ctx, tx, appTestKey(t))
	q := func(fn func(q *store.Queries) error) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error { return fn(store.New(sp)) })
	}
	consume := func(dev uuid.UUID, hash string) error {
		return q(func(q *store.Queries) error {
			_, err := q.ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{TenantID: w.tenant, DeviceID: dev, NonceHash: hash})
			return err
		})
	}
	nonces := func(dev uuid.UUID) int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM app_request_nonces WHERE device_id = $1`, dev)
	}
	singleHash, _ := w.issueNonce(t, ctx, tx, single), w.issueNonce(t, ctx, tx, single)
	bulk1Hash, bulk2Hash := w.issueNonce(t, ctx, tx, bulk1), w.issueNonce(t, ctx, tx, bulk2)
	bareHash := w.issueNonce(t, ctx, tx, bare)
	otherHash := other.issueNonce(t, ctx, tx, otherDevice)

	if err := q(func(q *store.Queries) error {
		_, err := q.RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: single, TenantID: w.tenant})
		return err
	}); err != nil {
		t.Fatalf("revoke one device: %v", err)
	}
	if n := nonces(single); n != 0 {
		t.Fatalf("the revoked device still holds %d nonce(s), want 0", n)
	}
	if err := consume(single, singleHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("issue -> revoke -> consume: %v, want pgx.ErrNoRows", err)
	}
	if n := nonces(bulk1) + nonces(bulk2); n != 2 {
		t.Fatalf("revoking one device touched the person's other devices' nonces (%d left, want 2)", n)
	}

	// The bare revocation: the owner stamps revoked_at with no nonce delete. Its nonce survives the
	// write and is still refused -- the EXISTS in ConsumeAppRequestNonce.
	if _, err := tx.Exec(ctx, `UPDATE app_devices SET revoked_at = now() WHERE id = $1`, bare); err != nil {
		t.Fatal(err)
	}
	if n := nonces(bare); n != 1 {
		t.Fatalf("CONTROL: the bare revocation left %d nonce(s), want its 1 (the belt would not be tested)", n)
	}
	if err := consume(bare, bareHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a surviving nonce of a revoked device: %v, want pgx.ErrNoRows (the belt)", err)
	}

	var revoked []uuid.UUID
	if err := q(func(q *store.Queries) error {
		var err error
		revoked, err = q.RevokeAppDevicesForEmployee(ctx, store.RevokeAppDevicesForEmployeeParams{TenantID: w.tenant, EmployeeID: w.employee})
		return err
	}); err != nil {
		t.Fatalf("the bulk revoke: %v", err)
	}
	if len(revoked) != 2 {
		t.Fatalf("the bulk revoke returned %d id(s), want the 2 live devices", len(revoked))
	}
	if n := nonces(bulk1) + nonces(bulk2); n != 0 {
		t.Fatalf("the bulk revoke left %d nonce(s), want 0", n)
	}
	for _, c := range []struct {
		dev  uuid.UUID
		hash string
	}{{bulk1, bulk1Hash}, {bulk2, bulk2Hash}} {
		if err := consume(c.dev, c.hash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("issue -> bulk revoke -> consume: %v, want pgx.ErrNoRows", err)
		}
	}
	if err := other.asApp(t, ctx, tx, other.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{TenantID: other.tenant, DeviceID: otherDevice, NonceHash: otherHash})
		return err
	}); err != nil {
		t.Fatalf("POSITIVE CONTROL: another business's live device's nonce: %v, want it consumable", err)
	}
}

// appCommittedDevice commits a tenant, an active employee and one device, and returns the tenant and
// the device (the rls_test.go convention: fresh ids, the rows stay inert).
func appCommittedDevice(t *testing.T, d *DB) (tenant, device uuid.UUID) {
	t.Helper()
	tenant, venue := m11Tenant(t, d)
	employee := uuid.New()
	if err := d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status)
		                          VALUES ($1, $2, $3, 'Maria Borg', 'active')`, employee, tenant, venue); e != nil {
			return e
		}
		row, e := store.New(tx).CreateAppDevice(ctx, store.CreateAppDeviceParams{TenantID: tenant, EmployeeID: employee, Platform: "android", PublicKey: appTestKey(t)})
		device = row.ID
		return e
	}); err != nil {
		t.Fatalf("committed device: %v", err)
	}
	return tenant, device
}

// appCommittedNonce commits one live nonce for the device and returns its hash.
func appCommittedNonce(t *testing.T, d *DB, tenant, device uuid.UUID) string {
	t.Helper()
	hash := opRandHex(t)
	if err := d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).IssueAppRequestNonce(ctx, store.IssueAppRequestNonceParams{NonceHash: hash, TtlSeconds: 120, DeviceID: device, TenantID: tenant})
		return e
	}); err != nil {
		t.Fatalf("committed nonce: %v", err)
	}
	return hash
}

// appOpenTx begins a tappa_app transaction in tenant's context on its own pooled connection and
// leaves it open for the caller to commit -- the "in flight" half of an interleaving.
func appOpenTx(t *testing.T, d *DB, tenant uuid.UUID) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '20s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	return tx
}

// appStillWaiting reports whether done has not fired after a grace period: the other side is
// blocked on a lock the open transaction holds.
func appStillWaiting(done <-chan error) bool {
	select {
	case <-done:
		return false
	case <-time.After(300 * time.Millisecond):
		return true
	}
}

// TestAppRequestNonces_RevocationAndConsumptionRace (M16-03 security review) measures the lock
// behaviour on real connections, committed, under -race:
//
//	revoke in flight, then consume   the consume WAITS for the revoke and then finds nothing (0 rows)
//	bare revoke in flight, consume   the same, through the belt alone: the EXISTS ... FOR SHARE waits
//	                                 on the device row and re-checks revoked_at once it commits
//	consume in flight, then revoke   the revoke WAITS for the consume, which therefore commits first;
//	                                 afterwards the device is revoked and holds no nonce
//	revoke in flight, then issue     the issue waits and inserts nothing (FOR SHARE OF d)
//
// and then races the two N times with no ordering imposed: no error but pgx.ErrNoRows on either side
// (no deadlock, no 23001), afterwards every device is revoked with no nonce left, and a consume that
// STARTED after the revoke's commit had returned never succeeded.
//
// MUTANTS (measured 2026-11-14 on the lane database, generated query edited and restored): FOR SHARE
// removed from ConsumeAppRequestNonce's EXISTS -> the "bare revoke" case is red (the consume does not
// wait for the revocation); FOR SHARE OF d removed from IssueAppRequestNonce -> the issue case is red
// (a nonce is issued after the revocation committed); the nonce delete removed from RevokeAppDevice
// -> "consume in flight, then revoke" here and TestAppRequestNonces_RevocationKillsTheNonces are red;
// `AND d.revoked_at IS NULL` added to the resolver -> TestResolveAppDevice_CarriesTheRevocation is red.
func TestAppRequestNonces_RevocationAndConsumptionRace(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	consumeAsync := func(tenant, device uuid.UUID, hash string) <-chan error {
		done := make(chan error, 1)
		go func() {
			done <- d.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
				_, err := store.New(tx).ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{TenantID: tenant, DeviceID: device, NonceHash: hash})
				return err
			})
		}()
		return done
	}
	liveNonces := func(tenant, device uuid.UUID) int32 {
		var n int32
		if err := d.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*)::int FROM app_request_nonces WHERE tenant_id = $1 AND device_id = $2`, tenant, device).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("revoke in flight, then consume", func(t *testing.T) {
		tenant, device := appCommittedDevice(t, d)
		hash := appCommittedNonce(t, d, tenant, device)
		r := appOpenTx(t, d, tenant)
		if _, err := store.New(r).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: device, TenantID: tenant}); err != nil {
			t.Fatal(err)
		}
		done := consumeAsync(tenant, device, hash)
		if !appStillWaiting(done) {
			t.Fatal("the consume did not wait for the in-flight revocation")
		}
		if err := r.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("the consume after the revocation committed: %v, want pgx.ErrNoRows", err)
		}
		got, err := d.GetAppDeviceByID(ctx, device)
		if err != nil || got.RevokedAt == nil {
			t.Fatalf("the resolver after the revocation: RevokedAt set %v (%v), want set", got.RevokedAt != nil, err)
		}
	})

	t.Run("bare revoke in flight, then consume", func(t *testing.T) {
		tenant, device := appCommittedDevice(t, d)
		hash := appCommittedNonce(t, d, tenant, device)
		r := appOpenTx(t, d, tenant)
		if _, err := r.Exec(ctx, `UPDATE app_devices SET revoked_at = now() WHERE id = $1 AND tenant_id = $2`, device, tenant); err != nil {
			t.Fatal(err)
		}
		done := consumeAsync(tenant, device, hash)
		if !appStillWaiting(done) {
			t.Fatal("the consume did not wait for the in-flight bare revocation (EXISTS ... FOR SHARE)")
		}
		if err := r.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("the consume after the bare revocation committed: %v, want pgx.ErrNoRows (the belt)", err)
		}
		if n := liveNonces(tenant, device); n != 1 {
			t.Fatalf("CONTROL: the bare revocation left %d nonce(s), want 1 -- the belt alone refused it", n)
		}
	})

	t.Run("consume in flight, then revoke", func(t *testing.T) {
		tenant, device := appCommittedDevice(t, d)
		hash := appCommittedNonce(t, d, tenant, device)
		c := appOpenTx(t, d, tenant)
		if _, err := store.New(c).ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{TenantID: tenant, DeviceID: device, NonceHash: hash}); err != nil {
			t.Fatalf("the in-flight consume: %v", err)
		}
		done := make(chan error, 1)
		go func() {
			done <- d.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
				_, err := store.New(tx).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: device, TenantID: tenant})
				return err
			})
		}()
		if !appStillWaiting(done) {
			t.Fatal("the revocation did not wait for the in-flight consume, so the consume could commit after it")
		}
		if err := c.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatalf("the revocation after the consume committed: %v", err)
		}
		if n := liveNonces(tenant, device); n != 0 {
			t.Fatalf("after the revocation the device holds %d nonce(s), want 0", n)
		}
	})

	t.Run("revoke in flight, then issue", func(t *testing.T) {
		tenant, device := appCommittedDevice(t, d)
		r := appOpenTx(t, d, tenant)
		if _, err := store.New(r).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: device, TenantID: tenant}); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			done <- d.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
				_, err := store.New(tx).IssueAppRequestNonce(ctx, store.IssueAppRequestNonceParams{NonceHash: opRandHex(t), TtlSeconds: 60, DeviceID: device, TenantID: tenant})
				return err
			})
		}()
		if !appStillWaiting(done) {
			t.Fatal("the issue did not wait for the in-flight revocation (FOR SHARE OF d)")
		}
		if err := r.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("the issue after the revocation committed: %v, want pgx.ErrNoRows", err)
		}
		if n := liveNonces(tenant, device); n != 0 {
			t.Fatalf("a revoked device was issued %d nonce(s)", n)
		}
	})

	t.Run("unordered race", func(t *testing.T) {
		const rounds = 40
		wins, refusals, late := 0, 0, 0
		for i := range rounds {
			tenant, device := appCommittedDevice(t, d)
			hash := appCommittedNonce(t, d, tenant, device)
			var (
				wg                  sync.WaitGroup
				revokeErr, consErr  error
				revokedAt, consFrom time.Time
				start               = make(chan struct{})
			)
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				revokeErr = d.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
					_, err := store.New(tx).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: device, TenantID: tenant})
					return err
				})
				revokedAt = time.Now()
			}()
			go func() {
				defer wg.Done()
				<-start
				// A small, varying head start for either side across the rounds.
				time.Sleep(time.Duration(i%5) * 200 * time.Microsecond)
				consFrom = time.Now()
				consErr = d.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
					_, err := store.New(tx).ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{TenantID: tenant, DeviceID: device, NonceHash: hash})
					return err
				})
			}()
			close(start)
			wg.Wait()
			if revokeErr != nil {
				t.Fatalf("round %d: the revocation failed: %v", i, revokeErr)
			}
			switch {
			case consErr == nil:
				wins++
				if consFrom.After(revokedAt) {
					t.Fatalf("round %d: a consume that started after the revocation committed SUCCEEDED", i)
				}
			case errors.Is(consErr, pgx.ErrNoRows):
				refusals++
			default:
				t.Fatalf("round %d: the consume failed for another reason (a deadlock or a trigger?): %v", i, consErr)
			}
			if n := liveNonces(tenant, device); n != 0 {
				t.Fatalf("round %d: after the race the revoked device holds %d nonce(s)", i, n)
			}
			if err := <-consumeAsync(tenant, device, hash); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("round %d: a consume after both finished: %v, want pgx.ErrNoRows", i, err)
			} else {
				late++
			}
		}
		t.Logf("%d unordered rounds: %d consume(s) won the race (committed before the revocation), %d refused, %d late consumes refused",
			rounds, wins, refusals, late)
	})
}
