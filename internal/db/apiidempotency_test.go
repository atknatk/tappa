package db

// apiidempotency_test.go -- migration 00052 (M11 API-4, ADR 0028 §1, §8): api_idempotency against
// real Postgres. The matrix row (tappa_app's verbs and columns) is in m11schema_test.go; what is here
// is what a matrix row cannot say. The concurrency of the ledger -- N requests with one key, a
// process dying mid-request, the lazy delete -- is measured through the product's code path in
// internal/apikey (idempotency_db_test.go).

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const op00052File = "00052_create_api_idempotency.sql"

// idemAs runs fn as tappa_app in tenant's context, inside the owner's test transaction.
func idemAs(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, fn func(sp pgx.Tx) error) error {
	t.Helper()
	return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
			return err
		}
		return fn(sp)
	})
}

const idemClaim = `INSERT INTO api_idempotency (tenant_id, key_id, idem_key, request_hash) VALUES ($1, $2, $3, $4)`

// TestApiCatalog_Idempotency is plan §5 API-4's "katalog api_idempotency" beyond the matrix row:
//
//	RLS five   ENABLE + FORCE, the standard NULLIF policy (USING and WITH CHECK, no OR branch), a
//	           tenant_id index (the primary key's leading column), tenant_id NOT NULL
//	§8 UNIQUE  (tenant_id, key_id, idem_key) -- a second claim of one key is a conflict, the same
//	           idempotency key under ANOTHER API key of the tenant is not
//	the key    the API key must be the tenant's own: a claim naming another tenant's key is refused
//	           by the composite foreign key (23503), not merely hidden
//	shapes     the idempotency key's 1-255 visible ASCII, the hash's 64 hex, a status in 200-299
//	verbs      tappa_app writes the result into status and result_ref alone (any other column 42501),
//	           and cannot TRUNCATE
func TestApiCatalog_Idempotency(t *testing.T) {
	ctx, tx := opTx(t)
	var enabled, forced, notNull bool
	if err := tx.QueryRow(ctx, `
		SELECT c.relrowsecurity, c.relforcerowsecurity, a.attnotnull
		  FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id'
		 WHERE c.oid = 'public.api_idempotency'::regclass`).Scan(&enabled, &forced, &notNull); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced || !notNull {
		t.Errorf("ENABLE %v FORCE %v tenant_id NOT NULL %v; all three are required (CLAUDE.md §6)", enabled, forced, notNull)
	}
	var using, check string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_expr(polqual, polrelid), pg_get_expr(polwithcheck, polrelid) FROM pg_policy
		 WHERE polrelid = 'public.api_idempotency'::regclass`).Scan(&using, &check); err != nil {
		t.Fatalf("the one policy: %v", err)
	}
	const want = "(tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)"
	if using != want || check != want {
		t.Errorf("policy USING %q WITH CHECK %q, want both %q", using, check, want)
	}
	if n := opInt(t, ctx, tx, `
		SELECT count(*) FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
		 WHERE i.indrelid = 'public.api_idempotency'::regclass AND a.attname = 'tenant_id'`); n < 1 {
		t.Error("no index leads with tenant_id")
	}
	var uniq string
	if err := tx.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
	                             WHERE conrelid = 'public.api_idempotency'::regclass AND contype = 'p'`).Scan(&uniq); err != nil {
		t.Fatal(err)
	}
	if uniq != "PRIMARY KEY (tenant_id, key_id, idem_key)" {
		t.Errorf("the key is %q, want §8's (tenant_id, key_id, idem_key)", uniq)
	}

	a, b := newAPIKeyWorld(t, ctx, tx), newAPIKeyWorld(t, ctx, tx)
	keyA := a.addKey(t, ctx, tx, "A1", randHash(t))
	keyA2 := a.addKey(t, ctx, tx, "A2", randHash(t))
	keyB := b.addKey(t, ctx, tx, "B1", randHash(t))
	hash := randHash(t)
	if err := idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, idemClaim, a.tenant, keyA, "order-1", hash)
		return err
	}); err != nil {
		t.Fatalf("CONTROL: the first claim: %v", err)
	}
	opWant(t, idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, idemClaim, a.tenant, keyA, "order-1", hash)
		return err
	}), "23505", "a second claim of one idempotency key under one API key")
	if err := idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, idemClaim, a.tenant, keyA2, "order-1", hash)
		return err
	}); err != nil {
		t.Errorf("the same idempotency key under the tenant's OTHER API key was refused: %v", err)
	}
	opWant(t, idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, idemClaim, a.tenant, keyB, "order-2", hash)
		return err
	}), sqlstateForeignKeyViolation, "A's claim naming B's API key")
	for _, c := range []struct{ name, key, hash string }{
		{"an empty idempotency key", "", hash},
		{"a 256-character idempotency key", repeatByte('k', 256), hash},
		{"a blank in the idempotency key", "a b", hash},
		{"a control character in the idempotency key", "a\tb", hash},
		{"a non-ASCII idempotency key", "sipariş", hash},
		{"a short hash", "order-3", "abc"},
		{"an upper-case hash", "order-4", "A" + hash[1:]},
	} {
		opWant(t, idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, idemClaim, a.tenant, keyA, c.key, c.hash)
			return err
		}), sqlstateCheckViolation, c.name)
	}
	if err := idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, idemClaim, a.tenant, keyA, repeatByte('k', 255), hash)
		return err
	}); err != nil {
		t.Errorf("CONTROL: a 255-character idempotency key was refused: %v", err)
	}
	for _, st := range []int{199, 300, 404, 500} {
		opWant(t, idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, `UPDATE api_idempotency SET status = $1 WHERE tenant_id = $2 AND key_id = $3 AND idem_key = 'order-1'`, st, a.tenant, keyA)
			return err
		}), sqlstateCheckViolation, "a recorded status outside 200-299")
	}
	if err := idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `UPDATE api_idempotency SET status = 201, result_ref = $1 WHERE tenant_id = $2 AND key_id = $3 AND idem_key = 'order-1'`, uuid.New(), a.tenant, keyA)
		return err
	}); err != nil {
		t.Errorf("CONTROL: recording the result: %v", err)
	}
	for _, col := range []string{"request_hash = 'x'", "idem_key = 'other'", "key_id = key_id", "created_at = now()", "tenant_id = tenant_id"} {
		opWant(t, idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, `UPDATE api_idempotency SET `+col+` WHERE tenant_id = $1`, a.tenant)
			return err
		}), sqlstateInsufficientPrivi, "tappa_app writing "+col)
	}
	opWant(t, idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `TRUNCATE api_idempotency`)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app truncating the ledger")
}

func repeatByte(b byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return string(out)
}

// TestRLS_ApiIdempotency_AnotherTenantsRowsAreInvisible is CLAUDE.md §8's RLS test for the ledger, in
// the FILTERLESS shape (§6): in B's context a bare `WHERE idem_key = …` finds none of A's rows and
// the positive control in A's context finds one; B cannot delete or update A's row (0 rows), and
// cannot claim a row stamped with A's tenant (WITH CHECK refuses it, 42501) -- so an idempotency key B sends can
// never return A's recorded result.
func TestRLS_ApiIdempotency_AnotherTenantsRowsAreInvisible(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newAPIKeyWorld(t, ctx, tx), newAPIKeyWorld(t, ctx, tx)
	keyA := a.addKey(t, ctx, tx, "A", randHash(t))
	keyB := b.addKey(t, ctx, tx, "B", randHash(t))
	idem := "shared-" + uuid.NewString()
	if err := idemAs(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, idemClaim, a.tenant, keyA, idem, randHash(t))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	count := func(tenant uuid.UUID, sql string, args ...any) int64 {
		var n int64
		if err := idemAs(t, ctx, tx, tenant, func(sp pgx.Tx) error { return sp.QueryRow(ctx, sql, args...).Scan(&n) }); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	if n := count(b.tenant, `SELECT count(*) FROM api_idempotency WHERE idem_key = $1`, idem); n != 0 {
		t.Fatalf("B's context sees %d of A's rows, want 0", n)
	}
	if n := count(a.tenant, `SELECT count(*) FROM api_idempotency WHERE idem_key = $1`, idem); n != 1 {
		t.Fatalf("POSITIVE CONTROL: A's context sees %d of its rows, want 1", n)
	}
	if n := count(b.tenant, `WITH d AS (DELETE FROM api_idempotency WHERE idem_key = $1 RETURNING 1) SELECT count(*) FROM d`, idem); n != 0 {
		t.Fatalf("B deleted %d of A's rows", n)
	}
	if n := count(b.tenant, `WITH u AS (UPDATE api_idempotency SET status = 200 WHERE idem_key = $1 RETURNING 1) SELECT count(*) FROM u`, idem); n != 0 {
		t.Fatalf("B updated %d of A's rows", n)
	}
	opWant(t, idemAs(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, idemClaim, a.tenant, keyA, "forged", randHash(t))
		return err
	}), sqlstateInsufficientPrivi, "B claiming a row stamped with A's tenant")
	// B's own claim of the same idempotency key is its own row: no conflict with A's.
	if err := idemAs(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, idemClaim, b.tenant, keyB, idem, randHash(t))
		return err
	}); err != nil {
		t.Errorf("B's claim of an idempotency key A also used was refused: %v", err)
	}
	if n := count(uuid.Nil, `SELECT count(*) FROM api_idempotency WHERE idem_key = $1`, idem); n != 0 {
		t.Fatalf("the nil tenant's context sees %d rows", n)
	}
}

// TestApiIdempotency00052_DownGivesBackAndUpTakesItAgain: 00052's Down removes the table (and with it
// tappa_app's grants), and the Up builds it again with the same reach. Walked in a
// holdingLockedSavepoint (the Down's DROP TABLE takes tenants and api_keys, which holdingDownTables
// lists).
func TestApiIdempotency00052_DownGivesBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00052File)
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00052 Down")
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_class WHERE relname = 'api_idempotency'`); n != 0 {
			t.Fatal("api_idempotency survives the Down")
		}
		opRunSection(t, ctx, sp, up, "00052 Up again")
		if got := opColumns(t, ctx, sp, "tappa_app", "api_idempotency", "UPDATE"); got != "status,result_ref" {
			t.Fatalf("tappa_app updates %q after the Up, want status,result_ref", got)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_class WHERE oid = 'public.api_idempotency'::regclass AND relforcerowsecurity`); n != 1 {
			t.Fatal("the table is not back with FORCE ROW LEVEL SECURITY")
		}
	})
}
