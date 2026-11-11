package db

// tagbirth_test.go -- migration 00100 (T76, step 3 of 3): tags_is_born_in_stock, a BEFORE INSERT
// trigger that refuses any tags row not born in the shape the encode flow writes (ADR 0017 §5.1
// step 3, InsertUnassigned): status 'unassigned', no location, unstamped, counter 0, never retired.
// Before it an INSERT that SAID 'active' landed incident A-1's shape -- on a wall, in service, never
// stamped -- around every gate the UPDATE path has (00025 refuses the transition, not the birth).
//
// Every case runs against real Postgres (CLAUDE.md §8). The assertions name the SQLSTATE (23001) AND
// the phrase this trigger's message carries, so a refusal by another guard -- 00022's stamp trigger,
// a CHECK, the RLS policy -- does not pass for this one.
//
// Fixtures are not cleaned up (tappa_app has no DELETE on tags; a tags row is never deleted).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

const (
	tagBirthFile = "00100_plaques_are_born_in_stock.sql"
	// bornInStockRefusal is a fragment of tappa_forbid_birth_outside_stock()'s message.
	bornInStockRefusal = "is born in stock"
	tagBirthTriggerDef = "CREATE TRIGGER tags_is_born_in_stock BEFORE INSERT ON public.tags FOR EACH ROW WHEN " +
		"(((new.status IS DISTINCT FROM 'unassigned'::text) OR (new.location_id IS NOT NULL) OR " +
		"(new.encoded_at IS NOT NULL) OR (new.last_ctr IS DISTINCT FROM 0) OR (new.retired_at IS NOT NULL) OR " +
		"(new.replaced_by IS NOT NULL))) EXECUTE FUNCTION tappa_forbid_birth_outside_stock()"
)

// wantBornInStockRefusal asserts err is 00100's refusal (23001 and its phrase).
func wantBornInStockRefusal(t *testing.T, err error, what string) {
	t.Helper()
	wantSQLSTATE(t, err, sqlstateRestrictViolation, what)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || !strings.Contains(pg.Message, bornInStockRefusal) {
		t.Fatalf("%s: refused, but not by tags_is_born_in_stock: %v", what, err)
	}
}

// TestTags00100_APlaqueIsBornInStock: as tappa_app, inside its own tenant (so neither RLS nor a
// privilege is what refuses), every birth shape but stock is refused by the trigger, and the refusal
// carries no uid, tenant or location; a stamped INSERT keeps 00022's own refusal (the trigger is named
// to fire after it). CONTROL: the product's own InsertUnassigned lands, and the product's UPDATEs
// then take the same row to every state the refused INSERTs named.
func TestTags00100_APlaqueIsBornInStock(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)
	successor := randUID(t)
	addPlaque(t, app, fx, successor, fx.locationID, "active", 0)

	for _, tc := range []struct {
		name string
		sql  string
		args []any
	}{
		{"A-1's shape: active on a wall, never stamped",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'active')`,
			[]any{fx.locationID}},
		{"active with no wall",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'active')`, nil},
		{"lost, in stock",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'lost')`, nil},
		{"lost, on a wall",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'lost')`,
			[]any{fx.locationID}},
		{"retired, with its successor",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status, retired_at, replaced_by) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'retired', now(), $4)`,
			[]any{fx.locationID, successor}},
		{"unassigned holding a wall",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'unassigned')`,
			[]any{fx.locationID}},
		{"unassigned with its counter already moved",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 5, 'unassigned')`, nil},
		{"unassigned carrying a retirement date",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status, retired_at) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned', now())`, nil},
		{"unassigned naming a successor",
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status, replaced_by) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned', $3)`,
			[]any{successor}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid := randUID(t)
			_, err := execAs(t, app, fx.tenantID, tc.sql, append([]any{uid, fx.tenantID}, tc.args...)...)
			wantBornInStockRefusal(t, err, tc.name)
			for _, id := range []string{uid, fx.tenantID.String(), fx.locationID.String(), successor} {
				if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(id)) {
					t.Fatalf("the refusal names %q; it must carry the status and nothing else: %v", id, err)
				}
			}
			if n := tagCount(t, app, fx, uid); n != 0 {
				t.Fatalf("after the refusal %d row(s) carry the uid, want 0", n)
			}
		})
	}

	// A stamped INSERT is 00022's to refuse, with 00022's words: BEFORE triggers fire in name order
	// and tags_encoded_at_not_settable_at_insert sorts before tags_is_born_in_stock.
	_, err := execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status, encoded_at) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned', now())`,
		randUID(t), fx.tenantID)
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "a stamped INSERT")
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || !strings.Contains(pg.Message, "not settable at INSERT") {
		t.Fatalf("a stamped INSERT was refused, but not by 00022's trigger: %v", err)
	}

	// CONTROL: the product's own load, then the product's own statements, to every state above.
	ctx := context.Background()
	uid := randUID(t)
	if err := app.WithTenant(ctx, fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).InsertUnassigned(ctx, store.InsertUnassignedParams{
			Uid: uid, TenantID: fx.tenantID, AesKeyRef: fixtures.FakeKeyRef(),
		})
		return e
	}); err != nil {
		t.Fatalf("CONTROL: store.InsertUnassigned was refused: %v -- the trigger refuses the product's own load", err)
	}
	for _, p := range []fixtures.Plaque{
		{Wall: fx.locationID, LastCtr: 9},
		{Wall: fx.locationID, Status: fixtures.PlaqueLost},
		{Status: fixtures.PlaqueLost, Unstamped: true},
		{Wall: fx.locationID, Status: fixtures.PlaqueRetired, ReplacedBy: successor},
	} {
		p.UID, p.TenantID = randUID(t), fx.tenantID
		if err := app.WithTenant(ctx, fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return fixtures.LoadPlaque(ctx, tx, p)
		}); err != nil {
			t.Fatalf("CONTROL: load -> stamp -> mount -> %s: %v", p.Status, err)
		}
	}
}

// TestTags00100_ASecondLoadOfTheSeedShapeIsStillANoOp is T76's ordering made concrete: a BEFORE
// INSERT trigger fires before ON CONFLICT is resolved, so test/fixtures/seed.sql's re-run survives
// only because its INSERT is in the stock shape. The stock shape on an existing uid inserts nothing
// and raises nothing; any other shape on the same existing uid is refused by the trigger -- it never
// reaches the conflict.
func TestTags00100_ASecondLoadOfTheSeedShapeIsStillANoOp(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)
	uid := randUID(t)
	addPlaque(t, app, fx, uid, fx.locationID, "active", 0)

	n, err := execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned')
		 ON CONFLICT (uid) DO NOTHING`, uid, fx.tenantID)
	if err != nil || n != 0 {
		t.Fatalf("the stock shape on an existing uid: rows=%d err=%v, want 0/nil", n, err)
	}
	_, err = execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'active')
		 ON CONFLICT (uid) DO NOTHING`, uid, fx.tenantID, fx.locationID)
	wantBornInStockRefusal(t, err, "the active shape on an existing uid, ON CONFLICT DO NOTHING")
	status, _, stamped, _ := tagState(t, app, fx, uid)
	if status != "active" || !stamped {
		t.Fatalf("the existing plaque reads (%s, stamped=%v) after the two loads, want (active, true)", status, stamped)
	}
}

// TestTags00100_TheOwnerIsBoundToo: tappa_owner (a superuser in development and CI, so neither RLS
// nor a REVOKE reaches it) is refused like the app -- the hand-typed INSERT at a psql prompt. No
// tenant context on purpose.
func TestTags00100_TheOwnerIsBoundToo(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	fx := newTagTenant(t, app)
	_, err := owner.pool.Exec(context.Background(),
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'active')`,
		randUID(t), fx.tenantID, fx.locationID)
	wantBornInStockRefusal(t, err, "tappa_owner loading a plaque straight onto a wall")
}

// TestTags00100_TheTriggerItsFunctionAndTheirDown pins the catalogue -- the trigger's exact
// definition, its function a SECURITY INVOKER one with EXECUTE in its owner alone and a fixed
// search_path -- and that the function refuses when bound anywhere else; then the Down drops exactly
// the trigger and its function (every other trigger on tags reads the same, and an 'active' INSERT
// lands again, as it did at 00099) and the Up puts both back.
func TestTags00100_TheTriggerItsFunctionAndTheirDown(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, tagBirthFile)
	for name, section := range map[string]string{"Up": up, "Down": down} {
		var first string
		for _, line := range strings.Split(section, "\n")[1:] {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "--") {
				first = line
				break
			}
		}
		if first != "SET LOCAL lock_timeout = '1s';" {
			t.Errorf("00100's %s opens with %q, want the lock_timeout", name, first)
		}
	}
	if got := downTables(down); strings.Join(got, ",") != "tags" {
		t.Fatalf("00100's Down names %v, want tags alone", got)
	}
	lockForDowns(t, ctx, tx, []string{"tags"})

	triggerDef := func(q opQuerier) string {
		t.Helper()
		var def string
		if err := q.QueryRow(ctx, `SELECT coalesce((SELECT pg_get_triggerdef(oid) FROM pg_trigger
		                            WHERE tgrelid = 'public.tags'::regclass AND tgname = 'tags_is_born_in_stock' AND tgenabled = 'O'), '')`).Scan(&def); err != nil {
			t.Fatal(err)
		}
		return def
	}
	otherTriggers := func(q opQuerier) string {
		t.Helper()
		var s string
		if err := q.QueryRow(ctx, `SELECT coalesce(string_agg(pg_get_triggerdef(oid), '; ' ORDER BY tgname), '')
		                             FROM pg_trigger WHERE tgrelid = 'public.tags'::regclass AND NOT tgisinternal
		                              AND tgname <> 'tags_is_born_in_stock'`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	const fn = `SELECT count(*) FROM pg_proc WHERE oid = to_regprocedure('public.tappa_forbid_birth_outside_stock()')
	             AND NOT prosecdef AND proowner = 'tappa_owner'::regrole AND proacl::text = '{tappa_owner=X/tappa_owner}'
	             AND proconfig = '{"search_path=pg_catalog, pg_temp"}'`
	if got := triggerDef(tx); got != tagBirthTriggerDef {
		t.Fatalf("the trigger is\n  %q\nwant\n  %q", got, tagBirthTriggerDef)
	}
	if opInt(t, ctx, tx, fn) != 1 {
		t.Fatal("tappa_forbid_birth_outside_stock() is not an INVOKER function owned by tappa_owner with EXECUTE in its owner alone and search_path pinned")
	}
	others := otherTriggers(tx)
	if others == "" {
		t.Fatal("PREMISE: tags carries no other trigger, so the Down's \"exactly what the Up created\" compares nothing")
	}

	// A copy bound to another event refuses rather than guarding nothing (00099's shape).
	fx := opTagWorld(t, ctx, tx)
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `CREATE TRIGGER tags_birth_probe BEFORE UPDATE ON public.tags FOR EACH ROW
		                            EXECUTE FUNCTION public.tappa_forbid_birth_outside_stock()`); err != nil {
			t.Fatal(err)
		}
		_, err := sp.Exec(ctx, `UPDATE tags SET encoded_at = encoded_at WHERE uid = $1`, fx.uid)
		wantSQLSTATE(t, err, "55000", "the function bound BEFORE UPDATE")
	})

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00100 Down")
		if got := triggerDef(sp); got != "" {
			t.Fatalf("after the Down the trigger is still %q", got)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE proname = 'tappa_forbid_birth_outside_stock'`); n != 0 {
			t.Fatalf("after the Down %d function(s) named tappa_forbid_birth_outside_stock remain", n)
		}
		if got := otherTriggers(sp); got != others {
			t.Fatalf("the Down changed tags' other triggers:\n %s\nwant\n %s", got, others)
		}
		aiSavepoint(t, ctx, sp, func(probe pgx.Tx) {
			if _, err := probe.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
			                               VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'active')`,
				randUID(t), fx.tenant, fx.location); err != nil {
				t.Fatalf("after the Down an 'active' INSERT is still refused: %v -- the Down left a guard behind", err)
			}
		})
		opRunSection(t, ctx, sp, up, "00100 Up again")
		if got := triggerDef(sp); got != tagBirthTriggerDef {
			t.Fatalf("after the Up the trigger is %q", got)
		}
		if opInt(t, ctx, sp, fn) != 1 {
			t.Fatal("after the Up the function's attributes are not the Up's")
		}
		if got := otherTriggers(sp); got != others {
			t.Fatalf("Down then Up changed tags' other triggers:\n %s\nwant\n %s", got, others)
		}
		_, err := sp.Exec(ctx, `SAVEPOINT refused`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sp.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
		                       VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'active')`,
			randUID(t), fx.tenant, fx.location)
		wantBornInStockRefusal(t, err, "an 'active' INSERT after the Up again")
		if _, err := sp.Exec(ctx, `ROLLBACK TO SAVEPOINT refused`); err != nil {
			t.Fatal(err)
		}
	})
}

// tagWorld / opTagWorld: one tenant, one wall and one mounted plaque, written by the owner inside tx (rolled
// back with it).
type tagWorld struct {
	tenant, location uuid.UUID
	uid              string
}

func opTagWorld(t *testing.T, ctx context.Context, tx pgx.Tx) tagWorld {
	t.Helper()
	w := tagWorld{tenant: opNewTenant(t, ctx, tx, "t76 birth "+randUID(t), time.Duration(0)), location: uuid.New(), uid: randUID(t)}
	if _, err := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 't76 door')`, w.location, w.tenant); err != nil {
		t.Fatal(err)
	}
	if err := fixtures.LoadPlaque(ctx, tx, fixtures.Plaque{UID: w.uid, TenantID: w.tenant, Wall: w.location}); err != nil {
		t.Fatal(err)
	}
	return w
}

// tagCount counts the rows carrying uid in fx's tenant, as tappa_app.
func tagCount(t *testing.T, d *DB, fx tagFixture, uid string) int {
	t.Helper()
	var n int
	if err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE tenant_id = $1 AND uid = $2`, fx.tenantID, uid).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}
