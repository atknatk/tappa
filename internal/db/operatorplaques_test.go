package db

// operatorplaques_test.go -- migration 00030 (M10 OP-13, phase A): the operator reads ONE
// tenant's plaque inventory through op_read_tenant_plaques. The catalogue half (the
// signature, the plaque keys the definer cannot read, the grants, the Down, the
// precondition, the temp-table shadow, the definer's EXECUTE set) and the behaviour half
// (op_begin_read's new kind, the read, the Go accessor) of ADR 0021 §6's list, again for
// the new op_read_* (m10-platform.md, OP-4 block, and the OP-13 card).
//
// HOW EACH TEST TOUCHES THE SHARED DATABASE -- operatortenants_test.go's three shapes:
//   - most tests run in opTx: one rolled-back REPEATABLE READ transaction as the owner,
//     identities switched with SET LOCAL SESSION AUTHORIZATION, the operator-tables lock
//     taken EXCLUSIVE. Every tenant, location and PLAQUE they need is written inside that
//     transaction and goes with it;
//   - the read side is reached WITHOUT a commit through a ticket the owner writes with
//     created_xact naming a transaction that really committed (opCommittedXact) and a hash
//     computed HERE, in Go (opDetailTicketHash: the inventory binds the same {tenant_id}
//     text the overview does), so a change in what the function hashes turns a test red;
//   - two tests need a COMMIT because a commit is their subject
//     (TestOpReadTenantPlaques_TwoPhaseLifecycle, TestTenantPlaques_OnThePoolTheTwoPhasesAreTwoTransactions).
//     They take the lock SHARED through opLiveFixture and write their plaque fixtures only
//     inside transactions that are rolled back. What they leave per run, by construction:
//     one disabled account each, its revoked sessions, and the 'read' rows they committed
//     (the lifecycle one, the pool test three since OP-13 phase B drove the method there
//     too -- two when the database holds no plaque at all) -- operator_audit_log is
//     append-only and its foreign keys keep the account and the sessions those rows name.
//
// 🔴 NO TEST HERE COMMITS A ROW TO `tags`. A plaque row is the one record of a chip's keys
// and is never deleted by any cleanup (agent-brief, 2026-09-26): a fixture plaque that
// committed would be permanent residue indistinguishable, in the table, from a real one.
// Every plaque below is written inside a transaction that is rolled back; the pool test
// reads plaques that already exist and writes none.

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// op00030Read is the function 00030 creates, by its exact catalogue identity: the session,
// the RAW ticket and the tenant -- no actor -- and the fixed column list of ADR 0021 §2 ii,
// which is the whole of what the read can return (a key column, or a flag computed from
// one, cannot join it without TestOperator00030_TheFunctionAndItsExactSignature turning
// red).
var op00030Read = struct{ name, args, result string }{
	"op_read_tenant_plaques",
	"p_session text, p_ticket text, p_tenant_id uuid",
	"TABLE(tenant_id uuid, tenant_name text, uid text, status text, location_id uuid, location_name text, " +
		"encoded_at timestamp with time zone, created_at timestamp with time zone, retired_at timestamp with time zone, " +
		"replaced_by text, last_ctr integer, plaque_count bigint)",
}

const (
	tenantPlaquesRefusal = "op_read_tenant_plaques: read refused"
	op00030File          = "00030_read_plaques_from_the_operator.sql"

	// The ticket kind CHECK at 00030 and, after its Down, at 00029.
	opKinds30 = `CHECK ((kind = ANY (ARRAY['legal_versions'::text, 'tenants'::text, 'tenant_detail'::text, 'tenant_plaques'::text])))`
	opKinds29 = `CHECK ((kind = ANY (ARRAY['legal_versions'::text, 'tenants'::text, 'tenant_detail'::text])))`

	// tappa_opdefiner's SELECT columns, in attnum order, at 00030 and (after its Down) at
	// 00029. 00030's list on tags is every plaque column but the two keys.
	opDefinerTags30      = "uid,tenant_id,location_id,last_ctr,status,retired_at,replaced_by,created_at,encoded_at"
	opDefinerTags29      = "tenant_id,status"
	opDefinerLocations30 = "id,tenant_id,name"
	opDefinerLocations29 = "tenant_id"
)

// opKindsAt30 is the closed set of read kinds at 00030 (opAtVersion's argument).
var opKindsAt30 = []string{"legal_versions", "tenants", "tenant_detail", "tenant_plaques"}

// ------------------------------------------------------------------ helpers --

// opAtVersion takes the test's transaction to migration `version`: from the newest file
// down, it runs the Down section of every migration numbered above `version` -- after
// deleting, inside the transaction, every ticket whose kind is not in `kinds` (the closed
// set at `version`), so each Down restores its kind CHECK VALIDATED, the shape the
// version's own tests expect. The database stays at HEAD for everyone else: the
// transaction is rolled back when the test ends. With no later migration it does nothing
// but the delete. (Since OP-13: 00029's Down test reaches 00029 through 00030's Down.)
func opAtVersion(t *testing.T, ctx context.Context, tx pgx.Tx, version int, kinds ...string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("read db/migrations: %v", err)
	}
	type file struct {
		n    int
		name string
	}
	var later []file
	for _, e := range entries {
		head, _, ok := strings.Cut(e.Name(), "_")
		n, err := strconv.Atoi(head)
		if !ok || err != nil || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if n > version {
			later = append(later, file{n, e.Name()})
		}
	}
	sort.Slice(later, func(i, j int) bool { return later[i].n > later[j].n })
	if _, err := tx.Exec(ctx, `DELETE FROM public.operator_read_tickets WHERE kind <> ALL ($1)`, kinds); err != nil {
		t.Fatalf("remove the tickets version %d does not know, inside the transaction: %v", version, err)
	}
	for _, f := range later {
		_, down := opMigrationSections(t, f.name)
		opRunSection(t, ctx, tx, down, f.name+" Down, to reach version "+strconv.Itoa(version))
	}
}

// opBeginReadBody returns the body (prosrc) of op_begin_read as a migration file's UP
// section defines it. 00029's Down defines another body (00027's), so the Down section is
// not searched.
func opBeginReadBody(t *testing.T, file string) string {
	t.Helper()
	up, _ := opMigrationSections(t, file)
	m := regexp.MustCompile(`(?s)CREATE OR REPLACE FUNCTION public\.op_begin_read\(.*?\nAS \$\$(.*?)\$\$;`).FindStringSubmatch(up)
	if m == nil {
		t.Fatalf("%s's Up does not define op_begin_read", file)
	}
	return m[1]
}

// opPlaqueRow is one row of op_read_tenant_plaques (or of the owner's statement that
// reads the same thing), every plaque column a pointer: a tenant without plaques reads one
// row whose plaque columns are NULL.
type opPlaqueRow struct {
	tenantID     uuid.UUID
	tenantName   string
	uid, status  *string
	locationID   *uuid.UUID
	locationName *string
	encodedAt    *time.Time
	createdAt    *time.Time
	retiredAt    *time.Time
	replacedBy   *string
	lastCtr      *int32
	count        int64
}

// text is the row as one comparable line (times in UTC to the microsecond).
func (r opPlaqueRow) text() string {
	s := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	tm := func(p *time.Time) string {
		if p == nil {
			return "<nil>"
		}
		return p.UTC().Format("2006-01-02T15:04:05.000000Z")
	}
	loc, ctr := "<nil>", "<nil>"
	if r.locationID != nil {
		loc = r.locationID.String()
	}
	if r.lastCtr != nil {
		ctr = strconv.Itoa(int(*r.lastCtr))
	}
	return strings.Join([]string{r.tenantID.String(), r.tenantName, s(r.uid), s(r.status), loc, s(r.locationName),
		tm(r.encodedAt), tm(r.createdAt), tm(r.retiredAt), s(r.replacedBy), ctr, strconv.FormatInt(r.count, 10)}, "|")
}

func opPlaqueTexts(rows []opPlaqueRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.text())
	}
	return out
}

func opScanPlaqueRows(rows pgx.Rows) ([]opPlaqueRow, error) {
	defer rows.Close()
	var out []opPlaqueRow
	for rows.Next() {
		var r opPlaqueRow
		if err := rows.Scan(&r.tenantID, &r.tenantName, &r.uid, &r.status, &r.locationID, &r.locationName,
			&r.encodedAt, &r.createdAt, &r.retiredAt, &r.replacedBy, &r.lastCtr, &r.count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// opScanPlaques runs the SHIPPED statement and scans every row, the database's error
// untouched (operatorErr would turn 28000 into a sentinel; these tests want the SQLSTATE).
func opScanPlaques(ctx context.Context, q opQuerier, hash, ticket, tenant any) ([]opPlaqueRow, error) {
	rows, err := q.Query(ctx, readTenantPlaquesSQL, hash, ticket, tenant)
	if err != nil {
		return nil, err
	}
	return opScanPlaqueRows(rows)
}

// opReadPlaques runs the read as tappa_operator inside a savepoint of tx; on success the
// savepoint is released, so the consumption stays in tx.
func opReadPlaques(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket string, tenant uuid.UUID) ([]opPlaqueRow, error) {
	t.Helper()
	var out []opPlaqueRow
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		out, e = opScanPlaques(ctx, sp, hash, ticket, tenant)
		return e
	})
	return out, err
}

// opForgePlaques forges an inventory ticket bound to the tenant and returns the raw ticket.
func opForgePlaques(t *testing.T, ctx context.Context, tx pgx.Tx, session, admin, tenant uuid.UUID, xact string) string {
	t.Helper()
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, admin, tenantPlaquesReadKind, opDetailTicketHash(raw, tenant), &tenant, xact, "30 seconds")
	return raw
}

// opOwnerPlaques is the ground truth: the owner (a superuser -- no row level security)
// reads the same columns of the same tenant with a statement written HERE: every plaque
// whose tenant_id is the tenant's, its location by id alone, the tenant's own list order.
func opOwnerPlaques(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) []opPlaqueRow {
	t.Helper()
	rows, err := q.Query(ctx, `
		SELECT t.id, t.name, g.uid::text, g.status, g.location_id, l.name, g.encoded_at, g.created_at,
		       g.retired_at, g.replaced_by::text, g.last_ctr, count(*) OVER ()
		  FROM public.tags g
		  JOIN public.tenants t ON t.id = g.tenant_id
		  LEFT JOIN public.locations l ON l.id = g.location_id
		 WHERE g.tenant_id = $1
		 ORDER BY g.location_id NULLS FIRST, g.uid`, tenant)
	if err != nil {
		t.Fatalf("the owner's read of %s's plaques: %v", tenant, err)
	}
	out, err := opScanPlaqueRows(rows)
	if err != nil {
		t.Fatalf("the owner's read of %s's plaques: %v", tenant, err)
	}
	return out
}

// opPlaqueFixture is one tenant written by the owner inside the test's transaction, with
// two named entrances and ONE plaque of every state the schema allows, each carrying its
// own last_ctr: uid[shape] (three lost ones: on a wall, on a wall with a stamp, in stock).
type opPlaqueFixture struct {
	id    uuid.UUID
	name  string
	doors []uuid.UUID
	uid   map[string]string
	want  map[string]PlaqueShape
}

// The fixture's plaques, by a name of their own -- and the shape each must read as.
var opPlaqueStates = map[string]PlaqueShape{
	"wall":       PlaqueOnAWall,
	"wall-a1":    PlaqueOnAWallNeverEncoded,
	"stock":      PlaqueInStock,
	"stock-bare": PlaqueInStockNotEncoded,
	"retired":    PlaqueRetired,
	"lost-wall":  PlaqueLost,
	"lost-stock": PlaqueLost,
	// 2nd round (B1): a lost plaque on a wall WITH an encode stamp -- the schema allows it
	// (the development database holds such a row) and it must read as lost, not as a
	// plaque on a wall.
	"lost-wall-stamped": PlaqueLost,
}

// opPlaqueTenant writes the fixture. The states are reached the way the product reaches
// them where that is possible -- loaded unassigned, stamped (an UPDATE: a row is born
// unstamped, 00022), mounted (00025 lets only a stamped plaque go active) -- and by the
// INSERT path where it is not (an 'active' row born unstamped is the A-1 shape, backlog
// T76: the INSERT path still allows it; a retired, a lost plaque). Half carry a key-0
// envelope (app_key_ref), half do not: nothing the read returns may tell them apart.
func opPlaqueTenant(t *testing.T, ctx context.Context, tx pgx.Tx, name string, ahead time.Duration) opPlaqueFixture {
	t.Helper()
	f := opPlaqueFixture{id: opNewTenant(t, ctx, tx, name, ahead), name: name, uid: map[string]string{}, want: opPlaqueStates}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("plaque fixture %s: %s: %v", name, strings.Fields(sql)[0], err)
		}
	}
	for i := 0; i < 2; i++ {
		id := uuid.New()
		exec(`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`, id, f.id, "op13 door "+strconv.Itoa(i)+" of "+name)
		f.doors = append(f.doors, id)
	}
	load := func(key, status string, door *uuid.UUID, ctr int, keyZero bool, replacedBy any) {
		t.Helper()
		uid := opUID(t)
		exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, app_key_ref, status, last_ctr, retired_at, replaced_by)
		      VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'),
		              CASE WHEN $4 THEN decode(repeat('beef', 22), 'hex') END, $5, $6,
		              CASE WHEN $5 = 'retired' THEN clock_timestamp() END, $7)`,
			uid, f.id, door, keyZero, status, ctr, replacedBy)
		f.uid[key] = uid
	}
	stamp := func(key string) { exec(`UPDATE tags SET encoded_at = clock_timestamp() WHERE uid = $1`, f.uid[key]) }
	load("wall", "unassigned", nil, 11, true, nil)
	stamp("wall")
	exec(`UPDATE tags SET status = 'active', location_id = $2 WHERE uid = $1`, f.uid["wall"], f.doors[0])
	load("wall-a1", "active", &f.doors[1], 7, false, nil)
	load("stock", "unassigned", nil, 0, true, nil)
	stamp("stock")
	load("stock-bare", "unassigned", nil, 0, false, nil)
	load("retired", "retired", &f.doors[0], 3, true, f.uid["wall"])
	load("lost-wall", "lost", &f.doors[1], 5, false, nil)
	load("lost-stock", "lost", nil, 0, true, nil)
	load("lost-wall-stamped", "lost", &f.doors[0], 9, true, nil)
	stamp("lost-wall-stamped")
	return f
}

// --------------------------------------------------------------- catalogue --

// TestOperator00030_TheFunctionAndItsExactSignature pins what ADR 0021 §6's generic pins
// leave open for the read 00030 creates: the exact argument list and result (the whole of
// what the read can return), the owner, SECURITY DEFINER, proconfig, one overload, no
// EXECUTE for PUBLIC, tappa_app or tappa_resolver and EXECUTE for tappa_operator; that the
// forward, frozen-clock and consumption scans walked it and raise nothing about it; that
// op_begin_read keeps its identity; and the ticket kind CHECK at 00030 -- exactly the four
// kinds.
func TestOperator00030_TheFunctionAndItsExactSignature(t *testing.T) {
	ctx, tx := opTx(t)
	var args, result, owner string
	var retset, secdef bool
	var config []string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
		       pg_get_userbyid(p.proowner), p.proretset, p.prosecdef, p.proconfig
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.proname = $1`, op00030Read.name).Scan(&args, &result, &owner, &retset, &secdef, &config); err != nil {
		t.Fatalf("%s: %v", op00030Read.name, err)
	}
	if args != op00030Read.args {
		t.Errorf("%s(%s), want (%s)", op00030Read.name, args, op00030Read.args)
	}
	if result != op00030Read.result {
		t.Errorf("%s returns %s,\n want %s", op00030Read.name, result, op00030Read.result)
	}
	if owner != "tappa_opdefiner" || !secdef || !retset {
		t.Errorf("owner %s, SECURITY DEFINER %v, set-returning %v; want tappa_opdefiner, true, true", owner, secdef, retset)
	}
	if len(config) != 1 || config[0] != "search_path=pg_catalog, pg_temp" {
		t.Errorf("proconfig %v", config)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
	                             WHERE n.nspname = 'public' AND p.proname = $1`, op00030Read.name); n != 1 {
		t.Errorf("%d functions named %s; an overload would be a second door", n, op00030Read.name)
	}
	for who, want := range map[string]bool{"public": false, "tappa_app": false, "tappa_resolver": false, "tappa_operator": true} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, 'public.op_read_tenant_plaques(text, text, uuid)', 'EXECUTE')`, who).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may != want {
			t.Errorf("has_function_privilege(%s, op_read_tenant_plaques, EXECUTE) = %v, want %v", who, may, want)
		}
	}
	if got := opIdentity(t, ctx, tx, "op_begin_read"); got != "p_session text, p_kind text, p_params jsonb -> text" {
		t.Errorf("op_begin_read is now %s; the replacement keeps 00027's identity", got)
	}

	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	if !slices.Contains(names, op00030Read.name) {
		t.Errorf("anti-vacuity: the forward scan did not walk %s", op00030Read.name)
	}
	for _, f := range findings {
		if strings.Contains(f, op00030Read.name+"(") || strings.Contains(f, "op_begin_read(") {
			t.Error(f)
		}
	}
	clock, _, err := opFrozenClockFindings(ctx, tx)
	if err != nil {
		t.Fatalf("frozen-clock scan: %v", err)
	}
	for _, f := range clock {
		t.Error(f)
	}
	consumption, read, err := opReadConsumptionFindings(ctx, tx)
	if err != nil {
		t.Fatalf("consumption scan: %v", err)
	}
	if !slices.Contains(read, op00030Read.name) {
		t.Errorf("anti-vacuity: the consumption scan did not read %s (it read %v)", op00030Read.name, read)
	}
	for _, f := range consumption {
		if strings.Contains(f, op00030Read.name+"(") {
			t.Error(f)
		}
	}
	var kinds string
	if err := tx.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
	                            WHERE conrelid = 'public.operator_read_tickets'::regclass
	                              AND conname = 'operator_read_tickets_kind_check'`).Scan(&kinds); err != nil {
		t.Fatal(err)
	}
	if kinds != opKinds30 {
		t.Errorf("operator_read_tickets_kind_check is %s, want %s", kinds, opKinds30)
	}
}

// opIdentity is a public function's identity arguments and result, "args -> result".
func opIdentity(t *testing.T, ctx context.Context, q opQuerier, name string) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `SELECT pg_get_function_identity_arguments(p.oid) || ' -> ' || pg_get_function_result(p.oid)
	                             FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
	                            WHERE n.nspname = 'public' AND p.proname = $1`, name).Scan(&s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return s
}

// opPlaqueKeyColumns derives, from the live catalogue, the plaque table's key-shaped
// columns -- opSecretShaped's rule (a name with hash, key, secret, sealed, token, cmac,
// password or sha in it, or a bytea) restricted to tags. Derived, so a key column a later
// migration adds to tags is covered -- and turns the named check below red until it is
// named.
func opPlaqueKeyColumns(t *testing.T, ctx context.Context, q opQuerier) []string {
	t.Helper()
	var out []string
	for _, c := range opSecretShaped(t, ctx, q) {
		if col, ok := strings.CutPrefix(c, "tags."); ok {
			out = append(out, col)
		}
	}
	return out
}

// opKeyNamingFindings returns every function tappa_opdefiner owns whose source names one
// of the given columns (word-bounded, case-blind): the plaque keys appear in no op_* body,
// not in a result, a WHERE or an expression.
func opKeyNamingFindings(ctx context.Context, q opQuerier, keys []string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT p.oid::regprocedure::text FROM pg_proc p
		 WHERE p.proowner = 'tappa_opdefiner'::regrole AND p.prosrc ~* $1
		 ORDER BY 1`, `\m(`+strings.Join(keys, "|")+`)\M`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// TestOperator00030_TheDefinerCannotReadAPlaqueKey is OP-13's acceptance, "anahtar
// sütunlarına erişim yok (katalog testi)", on the catalogue, as statements and as a
// changed function body:
//   - the plaque's key-shaped columns, DERIVED from the catalogue, are exactly aes_key_ref
//     and app_key_ref (named), and tappa_opdefiner holds no SELECT, INSERT or UPDATE on
//     either; nor SELECT on any secret-shaped column of any tenant table;
//   - tappa_opdefiner's SELECT on tags is every other plaque column, and on locations id,
//     tenant_id and name -- the whole lists;
//   - as tappa_opdefiner, naming a key is 42501 wherever it is named: in the select list,
//     in a WHERE (`app_key_ref IS NOT NULL` -- "has a key 0" is a read of the column), in
//     an aggregate, through a whole-row reference and through *. CONTROL: the granted
//     columns read;
//   - a function body CHANGED to name a key -- a copy of op_read_tenant_plaques owned by
//     tappa_opdefiner with count(g.app_key_ref) for count(g.uid), or with an
//     aes_key_ref predicate in its WHERE -- fails with 42501 when it runs; the unchanged
//     copy reads (CONTROL). A body runs as its owner, so no op_* can return a key or a
//     fact about one;
//   - no function tappa_opdefiner owns names a key in its source; the changed copy is
//     flagged (CONTROL);
//   - tappa_operator holds nothing on tags or locations and reading either directly is
//     42501; tappa_app may not EXECUTE the read, and calling it is 42501.
func TestOperator00030_TheDefinerCannotReadAPlaqueKey(t *testing.T) {
	ctx, tx := opTx(t)
	keys := opPlaqueKeyColumns(t, ctx, tx)
	if !slices.Equal(keys, []string{"aes_key_ref", "app_key_ref"}) {
		t.Fatalf("the plaque's key-shaped columns are %v, want [aes_key_ref app_key_ref] -- name a new one here and in ADR 0021 §1", keys)
	}
	for _, key := range keys {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_column_privilege('tappa_opdefiner', 'public.tags', $1, $2)`, key, priv).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may {
				t.Errorf("has_column_privilege(tappa_opdefiner, tags, %s, %s) = true", key, priv)
			}
		}
	}
	for _, col := range opSecretShaped(t, ctx, tx) {
		table, column, _ := strings.Cut(col, ".")
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_column_privilege('tappa_opdefiner', 'public.' || $1, $2, 'SELECT')`, table, column).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may {
			t.Errorf("tappa_opdefiner may SELECT %s", col)
		}
	}
	if got := opColumns(t, ctx, tx, "tappa_opdefiner", "tags", "SELECT"); got != opDefinerTags30 {
		t.Errorf("tappa_opdefiner SELECT on tags = (%s), want (%s)", got, opDefinerTags30)
	}
	if got := opColumns(t, ctx, tx, "tappa_opdefiner", "locations", "SELECT"); got != opDefinerLocations30 {
		t.Errorf("tappa_opdefiner SELECT on locations = (%s), want (%s)", got, opDefinerLocations30)
	}

	f := opPlaqueTenant(t, ctx, tx, "op13 keys "+opToken(t), 0)
	for _, probe := range []string{
		`SELECT aes_key_ref FROM public.tags WHERE tenant_id = $1`,
		`SELECT app_key_ref FROM public.tags WHERE tenant_id = $1`,
		`SELECT count(*) FROM public.tags WHERE tenant_id = $1 AND app_key_ref IS NOT NULL`,
		`SELECT uid FROM public.tags WHERE tenant_id = $1 AND aes_key_ref IS NULL`,
		`SELECT bool_or(app_key_ref IS NOT NULL) FROM public.tags WHERE tenant_id = $1`,
		`SELECT octet_length(aes_key_ref) FROM public.tags WHERE tenant_id = $1`,
		`SELECT g FROM public.tags AS g WHERE g.tenant_id = $1`,
		`SELECT * FROM public.tags WHERE tenant_id = $1`,
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_opdefiner", probe, f.id), sqlstateInsufficientPrivi, "tappa_opdefiner: "+probe)
	}
	if err := opExecAs(t, ctx, tx, "tappa_opdefiner", `SELECT `+opDefinerTags30+` FROM public.tags WHERE tenant_id = $1`, f.id); err != nil {
		t.Fatalf("CONTROL: tappa_opdefiner cannot read the granted plaque columns (%v); the refusals above would not be the keys'", err)
	}
	if err := opExecAs(t, ctx, tx, "tappa_opdefiner", `SELECT `+opDefinerLocations30+` FROM public.locations WHERE tenant_id = $1`, f.id); err != nil {
		t.Fatalf("CONTROL: tappa_opdefiner cannot read the granted location columns: %v", err)
	}
	// 2nd round (B5): more spellings of "read the row", each 42501 as tappa_opdefiner --
	// the whole-row forms (row_to_json, ::text, to_jsonb, jsonb_each over it, IS NOT NULL,
	// pg_column_size, a field of the whole row, tags.*), a key in ORDER BY / GROUP BY / IS
	// DISTINCT FROM / a NATURAL JOIN / an aggregate / a sub-select, a CTE and TABLE that
	// expand *, and the RETURNING * of the three writes (which it holds no grant for either).
	for _, probe := range []string{
		`SELECT count(*) FROM (SELECT row_to_json(g) FROM public.tags AS g LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT g::text FROM public.tags AS g LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT to_jsonb(g) FROM public.tags AS g LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT j.key FROM public.tags AS g, jsonb_each(to_jsonb(g)) AS j LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT g IS NOT NULL FROM public.tags AS g LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT pg_column_size(g) FROM public.tags AS g LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT (g).uid FROM public.tags AS g LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT (tags.*)::text FROM public.tags LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT uid FROM public.tags ORDER BY aes_key_ref LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT count(*) FROM public.tags GROUP BY app_key_ref LIMIT 1) s`,
		`SELECT count(*) FROM (SELECT uid FROM public.tags WHERE app_key_ref IS DISTINCT FROM NULL LIMIT 1) s`,
		`SELECT count(*) FROM public.tags NATURAL JOIN (SELECT NULL::bytea AS aes_key_ref) AS k`,
		`SELECT max(octet_length(app_key_ref)) FROM public.tags`,
		`SELECT count(*) FROM (SELECT uid FROM public.tags WHERE uid IN (SELECT uid FROM public.tags WHERE aes_key_ref IS NOT NULL) LIMIT 1) s`,
		`WITH t AS (SELECT * FROM public.tags) SELECT count(*) FROM t`,
		`SELECT count(*) FROM (TABLE public.tags LIMIT 1) s`,
		`UPDATE public.tags SET status = status WHERE false RETURNING *`,
		`DELETE FROM public.tags WHERE false RETURNING *`,
		`INSERT INTO public.tags SELECT * FROM public.tags WHERE false RETURNING *`,
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_opdefiner", probe), sqlstateInsufficientPrivi, "tappa_opdefiner: "+probe)
	}
	// COPY, as the table, a column list and a query: 42501 before a byte is sent (the output
	// would go nowhere -- io.Discard).
	for _, copySQL := range []string{
		`COPY public.tags TO STDOUT`,
		`COPY public.tags (aes_key_ref) TO STDOUT`,
		`COPY (SELECT app_key_ref FROM public.tags LIMIT 1) TO STDOUT`,
	} {
		opWant(t, opAs(t, ctx, tx, "tappa_opdefiner", func(sp pgx.Tx) error {
			_, e := sp.Conn().PgConn().CopyTo(ctx, io.Discard, copySQL)
			return e
		}), sqlstateInsufficientPrivi, "tappa_opdefiner: "+copySQL)
	}
	// pg_stats shows a column's statistics (its most common values among them) only to a
	// role that may SELECT the column: the owner sees the two key columns' rows, the definer
	// none -- and does see a granted column's (CONTROL). Statistics are gathered first if the
	// table has none yet (ANALYZE inside this transaction, rolled back with it).
	statsRows := func(role, cols string) int64 {
		t.Helper()
		var n int64
		q := func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, `SELECT count(*) FROM pg_stats WHERE schemaname = 'public' AND tablename = 'tags'
			                          AND attname = ANY (string_to_array($1, ','))`, cols).Scan(&n)
		}
		if role == "" {
			if err := q(tx); err != nil {
				t.Fatal(err)
			}
			return n
		}
		if err := opAs(t, ctx, tx, role, q); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if statsRows("", "aes_key_ref,app_key_ref") != 2 {
		if _, err := tx.Exec(ctx, `ANALYZE public.tags`); err != nil {
			t.Fatalf("ANALYZE tags: %v", err)
		}
	}
	if n := statsRows("", "aes_key_ref,app_key_ref"); n != 2 {
		t.Fatalf("CONTROL: the owner sees %d pg_stats rows for the two key columns, want 2", n)
	}
	if n := statsRows("tappa_opdefiner", "aes_key_ref,app_key_ref"); n != 0 {
		t.Errorf("tappa_opdefiner sees %d pg_stats row(s) for the plaque keys, want 0", n)
	}
	if n := statsRows("tappa_opdefiner", "status"); n != 1 {
		t.Errorf("CONTROL: tappa_opdefiner sees %d pg_stats row(s) for tags.status, want 1", n)
	}

	// A changed body. The copy is op_read_tenant_plaques' own definition under another
	// op_read_ name, owned by tappa_opdefiner and executable by tappa_operator, inside a
	// savepoint that is rolled back.
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	var def string
	if err := tx.QueryRow(ctx, `SELECT pg_get_functiondef('public.op_read_tenant_plaques(text, text, uuid)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatalf("read the definition: %v", err)
	}
	def = strings.Replace(def, "public.op_read_tenant_plaques(", "public.op_read_zz_probe(", 1)
	for _, c := range []struct{ name, old, new, want string }{
		{"unchanged copy", "", "", ""},
		{"a key in the result", "count(g.uid) OVER ()", "count(g.app_key_ref) OVER ()", sqlstateInsufficientPrivi},
		{"a key in the WHERE", "WHERE t.id = p_tenant_id", "WHERE t.id = p_tenant_id AND (g.aes_key_ref IS NULL OR g.aes_key_ref IS NOT NULL)", sqlstateInsufficientPrivi},
	} {
		probe := def
		if c.old != "" {
			if strings.Count(probe, c.old) != 1 {
				t.Fatalf("%s: the anchor occurs %d times in the definition", c.name, strings.Count(probe, c.old))
			}
			probe = strings.Replace(probe, c.old, c.new, 1)
		}
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{probe,
			`ALTER FUNCTION public.op_read_zz_probe(text, text, uuid) OWNER TO tappa_opdefiner`,
			`REVOKE ALL ON FUNCTION public.op_read_zz_probe(text, text, uuid) FROM PUBLIC`,
			`GRANT EXECUTE ON FUNCTION public.op_read_zz_probe(text, text, uuid) TO tappa_operator`} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: create the probe: %v", c.name, err)
			}
		}
		named, err := opKeyNamingFindings(ctx, sp, keys)
		if err != nil {
			t.Fatal(err)
		}
		flagged := slices.ContainsFunc(named, func(s string) bool { return strings.Contains(s, "op_read_zz_probe") })
		if flagged != (c.want != "") {
			t.Errorf("%s: the key-naming source scan flagged the probe = %v (findings %v)", c.name, flagged, named)
		}
		raw := opForgePlaques(t, ctx, sp, session, a.id, f.id, xact)
		var rows int
		callErr := opAs(t, ctx, sp, "tappa_operator", func(q pgx.Tx) error {
			r, e := q.Query(ctx, `SELECT * FROM public.op_read_zz_probe($1, $2, $3)`, hash, raw, f.id)
			if e != nil {
				return e
			}
			defer r.Close()
			for r.Next() {
				rows++
			}
			return r.Err()
		})
		if c.want == "" {
			if callErr != nil || rows != len(f.uid) {
				t.Errorf("CONTROL: the unchanged copy read %d row(s), err %v; want %d and no error", rows, callErr, len(f.uid))
			}
		} else {
			opWant(t, callErr, c.want, c.name+": the changed body, run as its owner")
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	named, err := opKeyNamingFindings(ctx, tx, keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range named {
		t.Errorf("%s names a plaque key in its source", n)
	}

	// "Is there a key 0" is not an answer either: two plaques alike in every column but
	// app_key_ref (one carries an envelope, one does not; written in this transaction, so
	// their created_at is the same) read as the same row but for the uid.
	twins := []string{opUID(t), opUID(t)}
	for i, uid := range twins {
		if _, err := tx.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, app_key_ref, status)
		                           VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'),
		                                   CASE WHEN $3 THEN decode(repeat('beef', 22), 'hex') END, 'unassigned')`, uid, f.id, i == 0); err != nil {
			t.Fatalf("the twin plaques: %v", err)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tags WHERE uid = ANY ($1) AND app_key_ref IS NOT NULL`, twins); n != 1 {
		t.Fatalf("PREMISE: %d of the twins carry a key 0, want 1", n)
	}
	rows, err := opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, f.id, xact), f.id)
	if err != nil {
		t.Fatalf("op_read_tenant_plaques: %v", err)
	}
	var texts []string
	for _, r := range rows {
		if r.uid != nil && slices.Contains(twins, *r.uid) {
			blank := "<twin>"
			r.uid = &blank
			texts = append(texts, r.text())
		}
	}
	if len(texts) != 2 || texts[0] != texts[1] {
		t.Errorf("the two twins read differently (a key 0 told apart):\n%s", strings.Join(texts, "\n"))
	}

	for _, table := range []string{"tags", "locations"} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			if got := opColumns(t, ctx, tx, "tappa_operator", table, priv); got != "" {
				t.Errorf("tappa_operator %s on %s = (%s), want ()", priv, table, got)
			}
		}
		opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT count(*) FROM public.`+table), sqlstateInsufficientPrivi,
			"tappa_operator reading "+table+" directly")
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", readTenantPlaquesSQL, opRandHex(t), opRandHex(t), f.id), sqlstateInsufficientPrivi,
		"tappa_app calling op_read_tenant_plaques")
}

// opDefinerForeignExecFindings returns every function NOT owned by tappa_opdefiner that it
// may EXECUTE and PUBLIC may not (a grant naming the definer or a role it could inherit
// from), and every SECURITY DEFINER function not owned by it that it may EXECUTE at all --
// in every schema, pg_catalog included.
func opDefinerForeignExecFindings(ctx context.Context, q opQuerier) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT p.oid::regprocedure::text || ' (owner ' || pg_get_userbyid(p.proowner) ||
		       CASE WHEN p.prosecdef THEN ', SECURITY DEFINER' ELSE '' END || ')'
		  FROM pg_proc p
		 WHERE p.proowner <> 'tappa_opdefiner'::regrole
		   AND has_function_privilege('tappa_opdefiner', p.oid, 'EXECUTE')
		   AND (p.prosecdef OR NOT has_function_privilege('public', p.oid, 'EXECUTE'))
		 ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// TestOperator00030_TheDefinerExecutesOnlyItsOwnAndPublicFunctions pins the definer's
// EXECUTE set outside the op_* it owns (the OP-12 plan noted no test held it): every other
// function tappa_opdefiner may call is one PUBLIC may call too, and none of them is a
// SECURITY DEFINER -- so a body of an op_* cannot reach data through another role's
// function. The case that matters is named: resolve_tag_by_uid (tappa_resolver's) returns
// aes_key_ref, and the definer may not call it. 00030 grants no EXECUTE outside its own
// function, so the set is the one before it. CONTROLS (each in a savepoint): EXECUTE on
// resolve_tag_by_uid granted to the definer; a function of the owner's with PUBLIC's
// EXECUTE revoked and the definer's granted -- each is reported.
func TestOperator00030_TheDefinerExecutesOnlyItsOwnAndPublicFunctions(t *testing.T) {
	ctx, tx := opTx(t)
	findings, err := opDefinerForeignExecFindings(ctx, tx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Errorf("tappa_opdefiner may EXECUTE %s", f)
	}
	// ANTI-VACUITY: the foreign definers exist and the resolver that returns a plaque key
	// is among them.
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE prosecdef AND proowner <> 'tappa_opdefiner'::regrole`); n < 6 {
		t.Fatalf("anti-vacuity: %d SECURITY DEFINER functions outside tappa_opdefiner; the six resolvers should be there", n)
	}
	var keyed string
	if err := tx.QueryRow(ctx, `SELECT pg_get_function_result('public.resolve_tag_by_uid(character)'::regprocedure)`).Scan(&keyed); err != nil {
		t.Fatalf("anti-vacuity: resolve_tag_by_uid: %v", err)
	}
	if !strings.Contains(keyed, "aes_key_ref") {
		t.Fatalf("anti-vacuity: resolve_tag_by_uid returns %s; the case this test names is gone", keyed)
	}
	var may bool
	if err := tx.QueryRow(ctx, `SELECT has_function_privilege('tappa_opdefiner', 'public.resolve_tag_by_uid(character)', 'EXECUTE')`).Scan(&may); err != nil || may {
		t.Errorf("tappa_opdefiner may EXECUTE resolve_tag_by_uid (%v, err %v)", may, err)
	}
	for _, c := range []struct{ sql, want string }{
		{`GRANT EXECUTE ON FUNCTION public.resolve_tag_by_uid(character) TO tappa_opdefiner`, "resolve_tag_by_uid"},
		{`CREATE FUNCTION public.zz_probe_owned() RETURNS int LANGUAGE sql AS 'SELECT 1';
		  REVOKE ALL ON FUNCTION public.zz_probe_owned() FROM PUBLIC;
		  GRANT EXECUTE ON FUNCTION public.zz_probe_owned() TO tappa_opdefiner`, "zz_probe_owned"},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Conn().PgConn().Exec(ctx, c.sql).ReadAll(); err != nil {
			t.Fatalf("control %q: %v", c.want, err)
		}
		got, err := opDefinerForeignExecFindings(ctx, sp)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, c.want) }) {
			t.Errorf("CONTROL: the scan did not report %s; findings: %v", c.want, got)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// opQuotedList splits a SQL list of quoted literals ("'a', 'b'") into its values.
func opQuotedList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		out = append(out, strings.Trim(strings.TrimSpace(v), "'"))
	}
	return out
}

// TestOperator00030_DownGivesBack00029AndUpTakesItAgain runs 00030's Down and Up from the
// migration file inside the test's transaction (first at 00030 through opAtVersion, so a
// later migration does not stand in the way).
//   - THE SET: the kinds the Down's NOT VALID condition and its two CHECKs name are 00029's
//     closed set, derived from 00029's file -- exactly legal_versions, tenants, tenant_detail.
//   - Down removes the read, gives op_begin_read back 00029's Up body VERBATIM (compared with
//     00029's file), takes back EXACTLY the column privileges 00030 granted -- the definer's
//     lists and ACL entries on tags and locations are 00029's after it, and the tenant
//     overview, which reads 00029's columns, still reads (the trap of a REVOKE ALL) -- and
//     puts the ticket kind CHECK back to 00029's set.
//   - The CHECK comes back NOT VALID whenever a ticket of a kind outside 00029's set exists,
//     VALIDATED otherwise. Four branches (2nd round, B2/B3): an unconsumed 'tenant_plaques'
//     ticket (a NEW one is then refused); a CONSUMED one alone -- the usual state of a live
//     database; a LATER migration's kind alone -- simulated: its kind added to the CHECK, one
//     ticket, then its Down leaving that ticket behind under a NOT VALID four-kind CHECK,
//     which is the chain OP-12 and OP-14 will build (a Down that asked for its own kind only
//     failed here with 23514); and none.
//   - COUNTED LIMITS, measured: with a ticket of a kind outside 00029's set present (a
//     'tenant_plaques' one, and in branch 3 a later migration's) and NO ticket of 00029's own
//     two kinds ('tenants', 'tenant_detail' -- the branches clear the tickets of every kind
//     but those two and 'legal_versions', and the development database holds none of them),
//     00030's Down succeeds and 00029's Down (applied, immutable) then fails with 23514 -- its
//     NOT VALID branch asks only for its own two kinds (L9; with one of those present it takes
//     that branch and succeeds, which this test does not drive); and with the later kind's
//     ticket present 00030's Up again fails with 23514 too -- its section 1 adds the four-kind
//     CHECK VALIDATED (L11).
//   - Up takes it all again, with this file's op_begin_read body.
func TestOperator00030_DownGivesBack00029AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 30, opKindsAt30...)
	up, down := opMigrationSections(t, op00030File)
	up29, down29 := opMigrationSections(t, op00029File)
	body29, body30 := opBeginReadBody(t, op00029File), opBeginReadBody(t, op00030File)
	if body29 == body30 {
		t.Fatal("PREMISE: 00029's and 00030's op_begin_read bodies are the same text")
	}

	// THE SET, from 00029's file, and the Down's three uses of it.
	m := regexp.MustCompile(`ADD CONSTRAINT operator_read_tickets_kind_check\s+CHECK \(kind IN \(([^)]*)\)\);`).FindStringSubmatch(up29)
	if m == nil {
		t.Fatal("00029's Up does not add the ticket kind CHECK")
	}
	set29 := opQuotedList(m[1])
	if !slices.Equal(set29, []string{legalVersionsReadKind, tenantsReadKind, tenantDetailReadKind}) {
		t.Fatalf("00029's closed set is %v", set29)
	}
	i := strings.LastIndex(down, "ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check")
	if i < 0 {
		t.Fatal("00030's Down does not drop the ticket kind CHECK")
	}
	tail := down[i:]
	conds := regexp.MustCompile(`WHERE kind <> ALL \(ARRAY\[([^\]]*)\]\)`).FindAllStringSubmatch(tail, -1)
	if len(conds) != 1 || !slices.Equal(opQuotedList(conds[0][1]), set29) {
		t.Errorf("00030's Down asks for NOT VALID with %v, want exactly one `WHERE kind <> ALL (ARRAY[%s])` -- every kind outside 00029's set", conds, strings.Join(set29, ", "))
	}
	checks := regexp.MustCompile(`CHECK \(kind IN \(([^)]*)\)\)`).FindAllStringSubmatch(tail, -1)
	if len(checks) != 2 {
		t.Errorf("00030's Down adds %d ticket kind CHECKs, want 2 (NOT VALID and VALIDATED)", len(checks))
	}
	for _, c := range checks {
		if !slices.Equal(opQuotedList(c[1]), set29) {
			t.Errorf("00030's Down restores the CHECK as %v, want 00029's %v", opQuotedList(c[1]), set29)
		}
	}

	type state struct {
		reads                   int64
		tags, locations         string
		kinds                   string
		validated               bool
		begin                   string
		appExecute, opExecute29 bool
		definerACL              string
	}
	read := func(q pgx.Tx) state {
		t.Helper()
		var s state
		if err := q.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM pg_proc WHERE proname = 'op_read_tenant_plaques'),
			       (SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'operator_read_tickets_kind_check'),
			       (SELECT convalidated FROM pg_constraint WHERE conname = 'operator_read_tickets_kind_check'),
			       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_begin_read(text, text, jsonb)'::regprocedure),
			       has_function_privilege('tappa_app', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE'),
			       has_function_privilege('tappa_operator', 'public.op_read_tenant_detail(text, text, uuid)', 'EXECUTE')
			       AND has_function_privilege('tappa_operator', 'public.op_read_tenants(text, text, text, integer, integer)', 'EXECUTE'),
			       (SELECT coalesce(string_agg(c.relname || '.' || a.attname || ':' || x.privilege_type, ',' ORDER BY c.relname, a.attnum), '')
			          FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid, aclexplode(a.attacl) AS x
			         WHERE c.oid IN ('public.tags'::regclass, 'public.locations'::regclass)
			           AND a.attnum > 0 AND x.grantee = 'tappa_opdefiner'::regrole)`).
			Scan(&s.reads, &s.kinds, &s.validated, &s.begin, &s.appExecute, &s.opExecute29, &s.definerACL); err != nil {
			t.Fatalf("read the state: %v", err)
		}
		s.tags = opColumns(t, ctx, q, "tappa_opdefiner", "tags", "SELECT")
		s.locations = opColumns(t, ctx, q, "tappa_opdefiner", "locations", "SELECT")
		return s
	}
	const acl30 = "locations.id:SELECT,locations.tenant_id:SELECT,locations.name:SELECT," +
		"tags.uid:SELECT,tags.tenant_id:SELECT,tags.location_id:SELECT,tags.last_ctr:SELECT,tags.status:SELECT," +
		"tags.retired_at:SELECT,tags.replaced_by:SELECT,tags.created_at:SELECT,tags.encoded_at:SELECT"
	const acl29 = "locations.tenant_id:SELECT,tags.tenant_id:SELECT,tags.status:SELECT"
	at30 := func(s state, when string) {
		t.Helper()
		if s.reads != 1 || s.tags != opDefinerTags30 || s.locations != opDefinerLocations30 || s.kinds != opKinds30 ||
			!s.validated || s.begin != body30 || s.appExecute || !s.opExecute29 || s.definerACL != acl30 {
			t.Errorf("%s: reads=%d tags=(%s) locations=(%s) kinds=%s validated=%v begin is 00030's=%v app EXECUTE=%v operator keeps 00029's=%v acl=(%s); want 00030's state",
				when, s.reads, s.tags, s.locations, s.kinds, s.validated, s.begin == body30, s.appExecute, s.opExecute29, s.definerACL)
		}
	}
	at29 := func(s state, when string, notValid bool) {
		t.Helper()
		kinds := opKinds29
		if notValid {
			kinds += " NOT VALID"
		}
		if s.reads != 0 || s.tags != opDefinerTags29 || s.locations != opDefinerLocations29 || s.kinds != kinds ||
			s.validated == notValid || s.begin != body29 || s.appExecute || !s.opExecute29 || s.definerACL != acl29 {
			t.Errorf("%s: reads=%d tags=(%s) locations=(%s) kinds=%s validated=%v begin is 00029's=%v app EXECUTE=%v operator keeps 00029's=%v acl=(%s); want 00029's state, the CHECK %s",
				when, s.reads, s.tags, s.locations, s.kinds, s.validated, s.begin == body29, s.appExecute, s.opExecute29, s.definerACL, kinds)
		}
	}
	at30(read(tx), "PREMISE before Down")

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := &opFixtureTenant{name: "op13 down " + opToken(t), locations: 1, activeEmp: 1, activeTags: 2, retiredTags: 1, activeAdmins: 1}
	f.id = opNewTenant(t, ctx, tx, f.name, 0)
	opPopulate(t, ctx, tx, f)
	xact := opCommittedXact(t, ctx)
	// branch opens a savepoint holding no ticket of a kind outside 00029's set.
	branch := func() pgx.Tx {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, `DELETE FROM operator_read_tickets WHERE kind <> ALL ($1)`, set29); err != nil {
			t.Fatalf("clear the kinds 00029 does not know, inside the transaction: %v", err)
		}
		return sp
	}
	done := func(sp pgx.Tx) {
		t.Helper()
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// Branch 1: an unconsumed 'tenant_plaques' ticket -> NOT VALID.
	sp := branch()
	opForgePlaques(t, ctx, sp, session, a.id, f.id, xact)
	opRunSection(t, ctx, sp, down, "00030 Down with a 'tenant_plaques' ticket present")
	at29(read(sp), "after Down (an unconsumed 'tenant_plaques' ticket)", true)
	// The trap a REVOKE ALL would have sprung: 00029's overview still reads, counts and all.
	overview, err := opReadDetail(t, ctx, sp, hash, opForgeDetail(t, ctx, sp, session, a.id, f.id, xact), f.id)
	if err != nil || len(overview) != 1 || overview[0].ActivePlaques != 2 || overview[0].Locations != 1 {
		t.Errorf("after Down the tenant overview reads %+v, err %v; want one row counting 2 active plaques and 1 location (00029's grants intact)", overview, err)
	}
	_, err = opBegin(t, ctx, sp, hash, tenantPlaquesReadKind, opDetailParams(f.id.String()))
	opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "after Down, a 'tenant_plaques' first phase")
	if _, err := opBegin(t, ctx, sp, hash, tenantDetailReadKind, opDetailParams(f.id.String())); err != nil {
		t.Errorf("after Down the 'tenant_detail' first phase is refused: %v", err)
	}
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at)
	                              SELECT $1, $2, 'tenant_plaques', l.id, clock_timestamp() + interval '30 seconds'
	                                FROM operator_audit_log l WHERE l.session_id = $2 LIMIT 1`, opRandHex(t), session),
		sqlstateCheckViolation, "a NEW 'tenant_plaques' ticket after Down (NOT VALID still binds new rows)")
	opRunSection(t, ctx, sp, up, "00030 Up again")
	at30(read(sp), "after Up again")
	done(sp)

	// Branch 2: a CONSUMED 'tenant_plaques' ticket alone -> NOT VALID.
	sp = branch()
	consumed := opForgeRead(t, ctx, sp, session, a.id, tenantPlaquesReadKind, opRandHex(t), &f.id, xact, "30 seconds")
	if _, err := sp.Exec(ctx, `UPDATE operator_read_tickets SET consumed_at = clock_timestamp() WHERE id = $1`, consumed); err != nil {
		t.Fatalf("consume the ticket: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00030 Down with a consumed 'tenant_plaques' ticket alone")
	at29(read(sp), "after Down (a consumed 'tenant_plaques' ticket alone)", true)
	done(sp)

	// Branch 3: a LATER migration's kind alone. Its Up widened the set, a ticket of its kind
	// was written, its Down put the four-kind set back NOT VALID and left the ticket.
	sp = branch()
	for _, s := range []string{
		`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
		`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
		     CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'zz_later_kind'))`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Up: %v", err)
		}
	}
	opForgeRead(t, ctx, sp, session, a.id, "zz_later_kind", opRandHex(t), &f.id, xact, "30 seconds")
	for _, s := range []string{
		`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
		`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
		     CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques')) NOT VALID`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Down: %v", err)
		}
	}
	if n := opInt(t, ctx, sp, `SELECT count(*) FROM operator_read_tickets WHERE kind = 'tenant_plaques'`); n != 0 {
		t.Fatalf("PREMISE: %d 'tenant_plaques' ticket(s) in the later-kind branch, want 0", n)
	}
	opRunSection(t, ctx, sp, down, "00030 Down after a later migration's Down left its ticket")
	at29(read(sp), "after Down (a later migration's kind alone)", true)
	// COUNTED LIMITS with that ticket still there (3rd round, F2): 00030's Up again -- its
	// section 1 adds the four-kind CHECK VALIDATED -- fails with 23514 (L11: the Up side does
	// not compose either; goose rolls the step back, nothing is left half-done), and so does
	// 00029's Down (L9 holds for every kind outside 00029's set, not 'tenant_plaques' alone --
	// while no 'tenants'/'tenant_detail' ticket exists, which would send 00029's Down to its
	// NOT VALID branch).
	for _, c := range []struct{ what, sql string }{
		{"00030's Up again (L11)", up},
		{"00029's Down (L9)", down29},
	} {
		inner, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, e := inner.Conn().PgConn().Exec(ctx, c.sql).ReadAll()
		if code, _ := opCode(e); code != sqlstateCheckViolation {
			t.Errorf("COUNTED LIMIT moved: %s with a later kind's ticket present answered %v; measured was 23514 -- "+
				"update the limit in 00030's Down comment and ADR 0021", c.what, e)
		}
		if err := inner.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	done(sp)

	// Branch 4: no ticket outside 00029's set -> VALIDATED, exactly 00029's set.
	sp = branch()
	opRunSection(t, ctx, sp, down, "00030 Down with no ticket outside 00029's set")
	at29(read(sp), "after Down (no ticket outside 00029's set)", false)
	done(sp)

	// The counted limit: 30 -> 29 succeeds with a 'tenant_plaques' ticket, 29 -> 28 does not.
	sp = branch()
	opForgePlaques(t, ctx, sp, session, a.id, f.id, xact)
	opRunSection(t, ctx, sp, down, "00030 Down before 00029's")
	_, chainErr := sp.Conn().PgConn().Exec(ctx, down29).ReadAll()
	if code, _ := opCode(chainErr); code != sqlstateCheckViolation {
		t.Errorf("COUNTED LIMIT moved: 00029's Down after 00030's, with a 'tenant_plaques' ticket present, answered %v; "+
			"measured was 23514 (00029's Down asks only for its own two kinds) -- update the limit in 00030's Down comment and ADR 0021", chainErr)
	}
	done(sp)
}

// TestOperator00030_PreconditionRefusesAWrongCluster: 00030's first statement refuses the
// role shapes 00026's, 00027's and 00029's refuse (absent, over-privileged, joined by
// membership in either direction), with SQLSTATE 55000 naming 00030, and passes the
// cluster this suite runs on.
func TestOperator00030_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	up, _ := opMigrationSections(t, op00030File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00030's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	cases := []struct {
		what  string
		setup []string
		want  string
	}{
		{"roles present", nil, ""},
		{"both roles absent", []string{`ALTER ROLE tappa_operator RENAME TO zz_op13_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op13_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner without BYPASSRLS", []string{`ALTER ROLE tappa_opdefiner NOBYPASSRLS`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_opdefiner is a member", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
	}
	for _, c := range cases {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		for _, s := range c.setup {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: setup %q: %v", c.what, s, err)
			}
		}
		_, runErr := sp.Conn().PgConn().Exec(ctx, pre).ReadAll()
		code, msg := opCode(runErr)
		switch {
		case c.want == "" && runErr != nil:
			t.Errorf("%s: the precondition refuses a correct cluster: %v", c.what, runErr)
		case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00030")):
			t.Errorf("%s: precondition answered %q %q, want %s naming 00030 and containing %q", c.what, code, msg, sqlstatePrerequisiteState, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("%s: rollback: %v", c.what, err)
		}
	}
}

// TestOperator00030_CallersTempTableIsNeverRead is ADR 0021 §6's temp-table shadow for the
// read and the replaced first phase: the caller creates temp tables with every name they
// touch (tenants, tags, locations and the four operator tables), fills them with forged
// rows -- a forged session, the real tenant under a forged name, forged plaques and
// entrances of the real tenant -- and GRANTs them to tappa_opdefiner (the step without
// which a broken search_path would fail with "permission denied" and look refused); the
// functions still read and write the real ones.
func TestOperator00030_CallersTempTableIsNeverRead(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	real := opPlaqueTenant(t, ctx, tx, "op13 real "+opToken(t), 0)
	forgedSession := opRandHex(t)

	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		for _, s := range []string{
			`CREATE TEMP TABLE tenants (id uuid, name text)`,
			`CREATE TEMP TABLE tags (uid text, tenant_id uuid, location_id uuid, last_ctr integer, status text,
			     retired_at timestamptz, replaced_by text, created_at timestamptz, encoded_at timestamptz)`,
			`CREATE TEMP TABLE locations (id uuid, tenant_id uuid, name text)`,
			`CREATE TEMP TABLE platform_sessions (id uuid DEFAULT gen_random_uuid(), admin_id uuid, token_hash text,
			     created_at timestamptz DEFAULT clock_timestamp(), mfa_verified_at timestamptz,
			     last_used_at timestamptz DEFAULT clock_timestamp(), revoked_at timestamptz)`,
			`CREATE TEMP TABLE platform_admins (id uuid, email text, display_name text, status text)`,
			`CREATE TEMP TABLE operator_audit_log (id uuid DEFAULT gen_random_uuid(), at timestamptz DEFAULT clock_timestamp(),
			     kind text, session_id uuid, actor_admin_id uuid, target_admin_id uuid, target_tenant_id uuid,
			     target_scope text, page_number integer, page_size integer, detail jsonb DEFAULT '{}')`,
			`CREATE TEMP TABLE operator_read_tickets (id uuid DEFAULT gen_random_uuid(), ticket_hash text, session_id uuid,
			     kind text, target_tenant_id uuid, audit_id uuid, created_at timestamptz DEFAULT clock_timestamp(),
			     created_xact xid8 DEFAULT '3'::xid8, expires_at timestamptz, consumed_at timestamptz)`,
			`GRANT ALL ON pg_temp.tenants, pg_temp.tags, pg_temp.locations, pg_temp.platform_sessions,
			     pg_temp.platform_admins, pg_temp.operator_audit_log, pg_temp.operator_read_tickets TO tappa_opdefiner`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return err
			}
		}
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO pg_temp.tenants VALUES ($1, 'SHADOW name of the real id')`, []any{real.id}},
			{`INSERT INTO pg_temp.locations VALUES ($1, $2, 'SHADOW door')`, []any{real.doors[0], real.id}},
			{`INSERT INTO pg_temp.tags SELECT 'SHADOW' || g, $1::uuid, NULL, 99, 'active', NULL, NULL, clock_timestamp(), NULL
			    FROM generate_series(1, 7) AS g`, []any{real.id}},
			{`INSERT INTO pg_temp.platform_sessions (admin_id, token_hash, mfa_verified_at) VALUES ($1, $2, clock_timestamp())`, []any{a.id, forgedSession}},
		} {
			if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("build the shadow as the caller: %v", err)
	}
	// CONTROL: the shadow is reachable by the definer role.
	var reachable int64
	if err := opAs(t, ctx, tx, "tappa_opdefiner", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.tenants) + (SELECT count(*) FROM pg_temp.tags)
		                              + (SELECT count(*) FROM pg_temp.locations) + (SELECT count(*) FROM pg_temp.platform_sessions)`).Scan(&reachable)
	}); err != nil {
		t.Fatalf("the definer role cannot read the shadow (%v); the GRANT step is what makes this test mean anything", err)
	}
	if reachable != 10 {
		t.Fatalf("the definer role sees %d forged rows, want 10", reachable)
	}

	// The forged SESSION exists only in the shadow.
	_, err := opBegin(t, ctx, tx, forgedSession, tenantPlaquesReadKind, opDetailParams(real.id.String()))
	opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read with a session that exists only in the caller's temp table")
	// The real session writes its 'read' row into the REAL log.
	audit0 := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session)
	if _, err := opBegin(t, ctx, tx, hash, tenantPlaquesReadKind, opDetailParams(real.id.String())); err != nil {
		t.Fatalf("op_begin_read with the real session: %v", err)
	}
	if d := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session) - audit0; d != 1 {
		t.Errorf("%d 'read' row(s) reached the REAL log, want 1", d)
	}
	rows, err := opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, real.id, opCommittedXact(t, ctx)), real.id)
	if err != nil {
		t.Fatalf("op_read_tenant_plaques: %v", err)
	}
	if got, want := opPlaqueTexts(rows), opPlaqueTexts(opOwnerPlaques(t, ctx, tx, real.id)); !slices.Equal(got, want) {
		t.Errorf("op_read_tenant_plaques read\n%s\nwant the real plaques\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, r := range rows {
		if strings.Contains(r.tenantName, "SHADOW") || (r.uid != nil && strings.HasPrefix(*r.uid, "SHADOW")) ||
			(r.locationName != nil && strings.Contains(*r.locationName, "SHADOW")) {
			t.Errorf("op_read_tenant_plaques returned a row of the caller's temp tables: %s", r.text())
		}
	}
	var shadowAudit, shadowTickets int64
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.operator_audit_log), (SELECT count(*) FROM pg_temp.operator_read_tickets)`).
			Scan(&shadowAudit, &shadowTickets)
	}); err != nil {
		t.Fatal(err)
	}
	if shadowAudit != 0 || shadowTickets != 0 {
		t.Errorf("the CALLER's temp tables were written: audit=%d tickets=%d, want 0 and 0", shadowAudit, shadowTickets)
	}
}

// ------------------------------------------------------------ op_begin_read --

// TestOpBeginRead_ThePlaqueKindBindsTheTenantAndNothingElse: the kind 00030 adds to the
// first phase.
//   - 'tenant_plaques': one 'read' row -- scope 'tenant_plaques', the tenant in
//     target_tenant_id, no page, detail exactly {} -- and one ticket carrying the same
//     tenant whose stored hash is sha256(raw ticket || the canonical {tenant_id}); an
//     upper-case id binds the same lower-case text;
//   - 22023 and no row for every parameter object the kind does not name (listed below),
//     28000 and no row for the six dead sessions;
//   - CONTROLS: the three kinds 00029 named still pass phase one with their own objects
//     (the replacement kept their branches).
func TestOpBeginRead_ThePlaqueKindBindsTheTenantAndNothingElse(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	rowsOf := func() (audit, tickets int64) {
		return opAudit(t, ctx, tx), opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets`)
	}
	id := uuid.New()
	a0, k0 := rowsOf()
	raw, err := opBegin(t, ctx, tx, hash, tenantPlaquesReadKind, opDetailParams(strings.ToUpper(id.String())))
	if err != nil {
		t.Fatalf("op_begin_read 'tenant_plaques': %v", err)
	}
	if a1, k1 := rowsOf(); a1-a0 != 1 || k1-k0 != 1 {
		t.Fatalf("op_begin_read 'tenant_plaques' wrote %d audit row(s) and %d ticket(s), want 1 and 1", a1-a0, k1-k0)
	}
	var (
		kind, scope, detail, ticketHash, ticketKind string
		tenant, ticketTenant                        *uuid.UUID
		number, size                                *int
	)
	if err := tx.QueryRow(ctx, `
		SELECT l.kind, l.target_scope, l.detail::text, l.target_tenant_id, l.page_number, l.page_size,
		       k.ticket_hash, k.kind, k.target_tenant_id
		  FROM operator_audit_log l JOIN operator_read_tickets k ON k.audit_id = l.id
		 WHERE l.session_id = $1 ORDER BY l.at DESC, l.id DESC LIMIT 1`, session).
		Scan(&kind, &scope, &detail, &tenant, &number, &size, &ticketHash, &ticketKind, &ticketTenant); err != nil {
		t.Fatalf("read the audit row and its ticket: %v", err)
	}
	if kind != "read" || scope != tenantPlaquesReadKind || detail != "{}" || tenant == nil || *tenant != id ||
		number != nil || size != nil || ticketKind != tenantPlaquesReadKind || ticketTenant == nil || *ticketTenant != id {
		t.Errorf("the 'tenant_plaques' audit row: kind=%s scope=%s detail=%s tenant=%v page=%v/%v; ticket kind=%s tenant=%v",
			kind, scope, detail, tenant, number, size, ticketKind, ticketTenant)
	}
	if ticketHash != opDetailTicketHash(raw, id) {
		t.Error("the stored hash is not sha256(raw ticket || canonical {tenant_id}) with the id in lower case")
	}

	for _, c := range []struct{ name, params string }{
		{"no key", `{}`},
		{"an extra key", `{"tenant_id": "` + id.String() + `", "page_number": 1}`},
		{"the list's keys", opTenantsParams("", 1, 10)},
		{"the legal list's keys", opLegalParams(1, 10)},
		{"not a uuid", opDetailParams("op13 not a uuid")},
		{"a uuid in braces", opDetailParams("{" + id.String() + "}")},
		{"a uuid without hyphens", opDetailParams(strings.ReplaceAll(id.String(), "-", ""))},
		{"a uuid with a trailing newline", opDetailParams(id.String() + "\n")},
		{"a number", `{"tenant_id": 7}`},
		{"null", `{"tenant_id": null}`},
		{"an array", `{"tenant_id": ["` + id.String() + `"]}`},
		{"a string, not an object", `"` + id.String() + `"`},
	} {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, hash, tenantPlaquesReadKind, c.params)
		opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "op_begin_read 'tenant_plaques', "+c.name, hash, c.params)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read 'tenant_plaques', %s: %d audit row(s) and %d ticket(s) written", c.name, a1-a0, k1-k0)
		}
	}
	for _, d := range opDeadSessions(t, ctx, tx) {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, d.hash, tenantPlaquesReadKind, opDetailParams(id.String()))
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read 'tenant_plaques', "+d.name, d.hash)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read 'tenant_plaques', %s: %d audit row(s) and %d ticket(s) written", d.name, a1-a0, k1-k0)
		}
	}
	for _, c := range []struct{ kind, params string }{
		{legalVersionsReadKind, opLegalParams(1, 10)},
		{tenantsReadKind, opTenantsParams("x", 1, 10)},
		{tenantDetailReadKind, opDetailParams(id.String())},
	} {
		if _, err := opBegin(t, ctx, tx, hash, c.kind, c.params); err != nil {
			t.Errorf("CONTROL: op_begin_read %q refused its own parameter object: %v", c.kind, err)
		}
	}
}

// --------------------------------------------------------------- the read --

// TestOpReadTenantPlaques_ReturnsOnlyTheNamedTenantsPlaques is the belt test ADR 0021 §6
// ("kemer") asks of a tenant-naming op_*: no row level security applies inside it, so the
// tenant filter on each table reference is the ONLY barrier. Two tenants, each with a
// plaque of every state on two named entrances; the read of each is EXACTLY what the owner
// reads for that tenant (every column, the order, plaque_count on every row) and holds no
// uid and no entrance of the other. Through the accessor's own scan the same read is the
// tenant's name, its eight plaques with the closed shape each must have -- the A-1 shape
// (active, never encoded) among them -- and Total; and the read writes no audit row (phase
// one did).
func TestOpReadTenantPlaques_ReturnsOnlyTheNamedTenantsPlaques(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	tok := opToken(t)
	one := opPlaqueTenant(t, ctx, tx, "op13 one "+tok, 0)
	two := opPlaqueTenant(t, ctx, tx, "op13 two "+tok, 0)
	for _, c := range []struct{ self, other opPlaqueFixture }{{one, two}, {two, one}} {
		rows, err := opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, c.self.id, xact), c.self.id)
		if err != nil {
			t.Fatalf("op_read_tenant_plaques %s: %v", c.self.name, err)
		}
		want := opOwnerPlaques(t, ctx, tx, c.self.id)
		if len(want) != len(opPlaqueStates) {
			t.Fatalf("PREMISE: the owner reads %d plaques for %s, want %d", len(want), c.self.name, len(opPlaqueStates))
		}
		if got, w := opPlaqueTexts(rows), opPlaqueTexts(want); !slices.Equal(got, w) {
			t.Errorf("%s read\n%s\nwant\n%s", c.self.name, strings.Join(got, "\n"), strings.Join(w, "\n"))
		}
		for _, r := range rows {
			for _, uid := range c.other.uid {
				if r.uid != nil && *r.uid == uid {
					t.Errorf("%s's read returned %s's plaque %s", c.self.name, c.other.name, uid)
				}
			}
			if r.locationID != nil && slices.Contains(c.other.doors, *r.locationID) {
				t.Errorf("%s's read returned %s's entrance", c.self.name, c.other.name)
			}
		}
	}

	before := opAudit(t, ctx, tx)
	raw := opForgePlaques(t, ctx, tx, session, a.id, one.id, xact)
	afterForge := opAudit(t, ctx, tx)
	var inv TenantPlaqueInventory
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		inv, e = readTenantPlaques(ctx, sp, hash, readTicket{v: &raw}, one.id)
		return e
	}); err != nil {
		t.Fatalf("readTenantPlaques: %v", err)
	}
	if d := opAudit(t, ctx, tx) - afterForge; d != 0 || afterForge-before != 1 {
		t.Errorf("op_read_tenant_plaques wrote %d audit row(s), want 0 (the one row of a read is phase one's)", d)
	}
	if inv.TenantID != one.id || inv.TenantName != one.name || inv.Total != int64(len(opPlaqueStates)) ||
		len(inv.Plaques) != len(opPlaqueStates) || inv.Truncated() {
		t.Fatalf("the inventory: tenant %v %q total %d plaques %d truncated %v; want %v %q %d %d false",
			inv.TenantID, inv.TenantName, inv.Total, len(inv.Plaques), inv.Truncated(), one.id, one.name, len(opPlaqueStates), len(opPlaqueStates))
	}
	// 2nd round (B4): EVERY field the accessor fills, plaque by plaque in order, equals the
	// owner's read of the same columns -- the counter K13-1 returns reaches a screen only
	// through TenantPlaque.LastCtr, the entrance's name only through LocationName.
	var fromAccessor []string
	for _, p := range inv.Plaques {
		uid, status, ctr, created := p.UID, p.Status, p.LastCtr, p.CreatedAt
		fromAccessor = append(fromAccessor, opPlaqueRow{tenantID: inv.TenantID, tenantName: inv.TenantName, uid: &uid,
			status: &status, locationID: p.LocationID, locationName: p.LocationName, encodedAt: p.EncodedAt,
			createdAt: &created, retiredAt: p.RetiredAt, replacedBy: p.ReplacedBy, lastCtr: &ctr, count: inv.Total}.text())
	}
	if want := opPlaqueTexts(opOwnerPlaques(t, ctx, tx, one.id)); !slices.Equal(fromAccessor, want) {
		t.Errorf("the accessor's plaques\n%s\nwant the owner's\n%s", strings.Join(fromAccessor, "\n"), strings.Join(want, "\n"))
	}
	shapeOf := map[string]PlaqueShape{}
	for _, p := range inv.Plaques {
		shapeOf[p.UID] = p.Shape()
	}
	for key, want := range one.want {
		if got := shapeOf[one.uid[key]]; got != want {
			t.Errorf("the %s plaque reads as %q, want %q", key, got, want)
		}
	}
	for _, p := range inv.Plaques {
		if p.UID == one.uid["wall-a1"] && (p.Status != "active" || p.EncodedAt != nil || p.LocationID == nil) {
			t.Errorf("the A-1 plaque: status %q encoded %v mounted %v; want active, never encoded, mounted", p.Status, p.EncodedAt, p.LocationID != nil)
		}
		if p.UID == one.uid["retired"] && (p.ReplacedBy == nil || *p.ReplacedBy != one.uid["wall"] || p.RetiredAt == nil) {
			t.Errorf("the retired plaque: replaced by %v, retired at %v; want the wall plaque and a time", p.ReplacedBy, p.RetiredAt)
		}
	}
}

// TestOpReadTenantPlaques_MatchesTheTenantsOwnList is the cross test: for one fixture, the
// operator's read and the tenant's OWN list -- db/queries/tags.sql ListTagsForTenant, run
// as tappa_app in the tenant's context, under row level security -- hold the same plaques
// in the same order with the same columns (uid, location, last_ctr, status, retired_at,
// replaced_by, created_at, encoded_at). The tenant's list carries no key either; the two
// views of one tenant's plaques are one list.
func TestOpReadTenantPlaques_MatchesTheTenantsOwnList(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := opPlaqueTenant(t, ctx, tx, "op13 cross "+opToken(t), 0)
	rows, err := opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, f.id, opCommittedXact(t, ctx)), f.id)
	if err != nil {
		t.Fatalf("op_read_tenant_plaques: %v", err)
	}
	var own []store.ListTagsForTenantRow
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.id.String()); err != nil {
			return err
		}
		var e error
		own, e = store.New(sp).ListTagsForTenant(ctx, f.id)
		return e
	}); err != nil {
		t.Fatalf("the tenant's own list as tappa_app: %v", err)
	}
	ptr := func(s string) *string { return &s }
	var got, want []string
	for _, r := range rows {
		got = append(got, opPlaqueRow{tenantID: f.id, uid: r.uid, status: r.status, locationID: r.locationID,
			encodedAt: r.encodedAt, createdAt: r.createdAt, retiredAt: r.retiredAt, replacedBy: r.replacedBy, lastCtr: r.lastCtr}.text())
	}
	for _, o := range own {
		ctr, created := o.LastCtr, o.CreatedAt
		want = append(want, opPlaqueRow{tenantID: o.TenantID, uid: ptr(o.Uid), status: ptr(o.Status), locationID: o.LocationID,
			encodedAt: o.EncodedAt, createdAt: &created, retiredAt: o.RetiredAt, replacedBy: o.ReplacedBy, lastCtr: &ctr}.text())
	}
	if len(own) != len(opPlaqueStates) {
		t.Fatalf("PREMISE: the tenant's own list holds %d plaques, want %d", len(own), len(opPlaqueStates))
	}
	if !slices.Equal(got, want) {
		t.Errorf("the operator's read\n%s\nthe tenant's own list\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestOpReadTenantPlaques_AnUnknownTenantReadsNothingAndAnEmptyOneItsName: an id that names
// no tenant reads ZERO rows -- no error, so not a refusal (28000) and not an argument error
// (22023) -- and through the accessor's scan it is ErrNoSuchTenant, distinct from
// ErrOperatorRefused. A tenant with an entrance and no plaque reads ONE row: its id and
// name, every plaque column NULL, plaque_count 0 -- through the accessor its name, Total 0
// and no plaque.
func TestOpReadTenantPlaques_AnUnknownTenantReadsNothingAndAnEmptyOneItsName(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	unknown := uuid.New()
	rows, err := opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, unknown, xact), unknown)
	if err != nil || len(rows) != 0 {
		t.Errorf("an unknown tenant: %d row(s), err %v; want 0 rows and no error", len(rows), err)
	}
	raw := opForgePlaques(t, ctx, tx, session, a.id, unknown, xact)
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		_, e := readTenantPlaques(ctx, sp, hash, readTicket{v: &raw}, unknown)
		if !errors.Is(e, ErrNoSuchTenant) || errors.Is(e, ErrOperatorRefused) {
			return fmt.Errorf("readTenantPlaques of an unknown tenant: %v, want ErrNoSuchTenant", e)
		}
		return nil
	}); err != nil {
		t.Error(err)
	}

	name := "op13 empty " + opToken(t)
	empty := opNewTenant(t, ctx, tx, name, 0)
	if _, err := tx.Exec(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'op13 door with no plaque')`, empty); err != nil {
		t.Fatal(err)
	}
	rows, err = opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, empty, xact), empty)
	if err != nil {
		t.Fatalf("a tenant without plaques: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("a tenant without plaques read %d rows, want 1", len(rows))
	}
	if r := rows[0]; r.tenantID != empty || r.tenantName != name || r.count != 0 || r.uid != nil || r.status != nil ||
		r.locationID != nil || r.locationName != nil || r.encodedAt != nil || r.createdAt != nil || r.lastCtr != nil {
		t.Errorf("a tenant without plaques read %s; want its id and name, NULL plaque columns, 0", r.text())
	}
	raw = opForgePlaques(t, ctx, tx, session, a.id, empty, xact)
	var inv TenantPlaqueInventory
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		inv, e = readTenantPlaques(ctx, sp, hash, readTicket{v: &raw}, empty)
		return e
	}); err != nil {
		t.Fatalf("readTenantPlaques of a tenant without plaques: %v", err)
	}
	if inv.TenantID != empty || inv.TenantName != name || inv.Total != 0 || len(inv.Plaques) != 0 || inv.Truncated() {
		t.Errorf("the empty inventory: %+v; want the name, Total 0, no plaque", inv)
	}
}

// TestOpReadTenantPlaques_TheFirst200AndTheWholeCount: ADR 0021 §2 iii's ceiling lives in
// the read's body. A tenant with 205 plaques (in stock and on two entrances) reads 200
// rows -- the FIRST 200 of the owner's list in its order -- and every row's plaque_count is
// 205 (counted before the cap); through the accessor Total 205 and Truncated. A tenant with
// exactly 200 reads all of them and is not truncated.
func TestOpReadTenantPlaques_TheFirst200AndTheWholeCount(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	tok := opToken(t)
	for _, n := range []int{205, 200} {
		tenant := opNewTenant(t, ctx, tx, "op13 many "+strconv.Itoa(n)+" "+tok, 0)
		doors := []uuid.UUID{uuid.New(), uuid.New()}
		for i, d := range doors {
			if _, err := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`, d, tenant, "op13 many door "+strconv.Itoa(i)); err != nil {
				t.Fatal(err)
			}
		}
		prefix := strings.ToUpper(opUID(t)[:6])
		if _, err := tx.Exec(ctx, `
			INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
			SELECT $2 || lpad(upper(to_hex(g)), 8, '0'), $1,
			       CASE WHEN g % 3 = 0 THEN NULL WHEN g % 2 = 0 THEN $3::uuid ELSE $4::uuid END,
			       decode(repeat('dead', 22), 'hex'),
			       CASE WHEN g % 3 = 0 THEN 'unassigned' ELSE 'active' END
			  FROM generate_series(1, $5::int) AS g`, tenant, prefix, doors[0], doors[1], n); err != nil {
			t.Fatalf("%d plaques: %v", n, err)
		}
		owner := opOwnerPlaques(t, ctx, tx, tenant)
		if len(owner) != n {
			t.Fatalf("PREMISE: the owner reads %d plaques, want %d", len(owner), n)
		}
		rows, err := opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, tenant, xact), tenant)
		if err != nil {
			t.Fatalf("op_read_tenant_plaques (%d): %v", n, err)
		}
		if len(rows) != MaxTenantPlaques {
			t.Errorf("a tenant with %d plaques read %d rows, want %d", n, len(rows), MaxTenantPlaques)
		}
		if got, want := opPlaqueTexts(rows), opPlaqueTexts(owner[:min(n, MaxTenantPlaques)]); !slices.Equal(got, want) {
			t.Errorf("a tenant with %d plaques: the rows are not the first %d of the owner's list in its order", n, MaxTenantPlaques)
		}
		raw := opForgePlaques(t, ctx, tx, session, a.id, tenant, xact)
		var inv TenantPlaqueInventory
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			var e error
			inv, e = readTenantPlaques(ctx, sp, hash, readTicket{v: &raw}, tenant)
			return e
		}); err != nil {
			t.Fatalf("readTenantPlaques (%d): %v", n, err)
		}
		if inv.Total != int64(n) || len(inv.Plaques) != MaxTenantPlaques || inv.Truncated() != (n > MaxTenantPlaques) {
			t.Errorf("a tenant with %d plaques: Total %d, %d plaques, truncated %v", n, inv.Total, len(inv.Plaques), inv.Truncated())
		}
	}
}

// TestOpReadTenantPlaques_ATicketFromThisTransactionIsRefused is ADR 0021 §2 v 3's rows
// A1/A2 for the read: a ticket op_begin_read made in THIS transaction -- at the top level,
// in an open savepoint, in a released one -- is refused (28000). CONTROL: a ticket whose
// transaction committed is read.
func TestOpReadTenantPlaques_ATicketFromThisTransactionIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := uuid.New()
	begin := func(q pgx.Tx) string {
		t.Helper()
		raw, err := opBegin(t, ctx, q, hash, tenantPlaquesReadKind, opDetailParams(tenant.String()))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := begin(tx)
	_, err := opReadPlaques(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, "A1: a ticket of this transaction's top level", raw)
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw = begin(sp)
	_, err = opReadPlaques(t, ctx, sp, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, "A2: a ticket of an open savepoint", raw)
	if err := sp.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = opReadPlaques(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, "A2: a ticket of a released savepoint", raw)

	if _, err := opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, tenant, opCommittedXact(t, ctx)), tenant); err != nil {
		t.Fatalf("CONTROL: a ticket whose transaction COMMITTED was refused: %v", err)
	}
}

// TestOpReadTenantPlaques_AForgedTicketIsRefused: a ticket is bound to its session, its
// KIND and its tenant. Refused (28000, ticket not consumed): another session of the same
// operator; another tenant; a ticket no op_begin_read issued, and NULL; and the KIND
// condition's own case, which this kind makes load-bearing -- 'tenant_plaques' and
// 'tenant_detail' hash the SAME {tenant_id} text, so an overview ticket carries exactly the
// hash the inventory computes: shown to the inventory it is refused, and an inventory
// ticket shown to the overview is refused; the same hash under 'tenants' and
// 'legal_versions' is refused too. CONTROL: the same hash under 'tenant_plaques' is read.
func TestOpReadTenantPlaques_AForgedTicketIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	otherHash, _ := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	tenant, other := uuid.New(), uuid.New()
	consumed := func() int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, session)
	}
	before := consumed()
	ticket := opForgePlaques(t, ctx, tx, session, a.id, tenant, xact)
	_, err := opReadPlaques(t, ctx, tx, otherHash, ticket, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, "another session", ticket)
	_, err = opReadPlaques(t, ctx, tx, hash, ticket, other)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, "another tenant", ticket)
	for _, raw := range []any{opRandHex(t), nil} {
		err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			_, e := opScanPlaques(ctx, sp, hash, raw, tenant)
			return e
		})
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, fmt.Sprintf("a ticket never issued (%T)", raw))
	}
	for _, kind := range []string{tenantDetailReadKind, tenantsReadKind, legalVersionsReadKind} {
		raw := opRandHex(t)
		opForgeRead(t, ctx, tx, session, a.id, kind, opDetailTicketHash(raw, tenant), &tenant, xact, "30 seconds")
		_, err := opReadPlaques(t, ctx, tx, hash, raw, tenant)
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, "the inventory's own hash under kind '"+kind+"'", raw)
	}
	// The other direction: an inventory ticket shown to the overview.
	raw := opForgePlaques(t, ctx, tx, session, a.id, tenant, xact)
	_, err = opReadDetail(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantDetailRefusal, "op_read_tenant_detail with a 'tenant_plaques' ticket", raw)
	if n := consumed() - before; n != 0 {
		t.Errorf("%d ticket(s) consumed by refused reads", n)
	}
	// CONTROL: the right kind, session and tenant.
	if _, err := opReadPlaques(t, ctx, tx, hash, ticket, tenant); err != nil {
		t.Errorf("CONTROL: the inventory's ticket with its own session and tenant was refused: %v", err)
	}
	if _, err := opReadPlaques(t, ctx, tx, hash, raw, tenant); err != nil {
		t.Errorf("CONTROL: the ticket the overview refused is the inventory's: %v", err)
	}
}

// TestOpReadTenantPlaques_RefusesEveryDeadSession: the read resolves the session through
// op_touch_session before anything else, so each of the six dead sessions is the touch
// refusal (28000) and no ticket is consumed -- even one that would otherwise match.
func TestOpReadTenantPlaques_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	xact := opCommittedXact(t, ctx)
	tenant := uuid.New()
	for _, d := range opDeadSessions(t, ctx, tx) {
		if d.id == uuid.Nil {
			_, err := opReadPlaques(t, ctx, tx, d.hash, opRandHex(t), tenant)
			opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash)
			continue
		}
		var admin uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT admin_id FROM platform_sessions WHERE id = $1`, d.id).Scan(&admin); err != nil {
			t.Fatal(err)
		}
		raw := opForgePlaques(t, ctx, tx, d.id, admin, tenant, xact)
		_, err := opReadPlaques(t, ctx, tx, d.hash, raw, tenant)
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash, raw)
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, d.id); n != 0 {
			t.Errorf("%s: %d ticket(s) consumed", d.name, n)
		}
	}
}

// TestOpReadTenantPlaques_ExpiryIsTheWallClock is ADR 0021 §6 "bilet süresi" for the read:
// an expired ticket is refused, and a ticket that expires WHILE the reading transaction is
// open is refused -- through savepoints rolled back three times, and through three
// exception sub-transactions of ONE DO statement whose sleep is INSIDE it (a frozen
// statement_timestamp() would still call the ticket alive). CONTROL first: the same
// ticket, unexpired, is read (in a savepoint that is rolled back, so it stays unconsumed).
func TestOpReadTenantPlaques_ExpiryIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := uuid.New()
	ticket := opForgePlaques(t, ctx, tx, session, a.id, tenant, opCommittedXact(t, ctx))
	expireIn := func(interval string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `UPDATE operator_read_tickets SET expires_at = clock_timestamp() + $2::interval WHERE session_id = $1`, session, interval); err != nil {
			t.Fatalf("set the expiry: %v", err)
		}
	}
	readThenUndo := func() error {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, e := opReadPlaques(t, ctx, sp, hash, ticket, tenant)
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return e
	}
	if err := readThenUndo(); err != nil {
		t.Fatalf("CONTROL: the unexpired ticket was refused: %v", err)
	}
	expireIn("-1 second")
	opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, tenantPlaquesRefusal, "an expired ticket", ticket)

	expireIn("1 second")
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, tenantPlaquesRefusal,
			"savepoint "+strconv.Itoa(i+1)+" after the ticket expired in the open transaction", ticket)
	}

	expireIn("1 second")
	var result string
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('tappa_test.session', $1, true), set_config('tappa_test.ticket', $2, true),
		                                  set_config('tappa_test.tenant', $3, true)`, hash, ticket, tenant.String()); err != nil {
			return err
		}
		if _, err := sp.Exec(ctx, `
			DO $d$
			DECLARE
			    n_data    integer := 0;
			    n_refused integer := 0;
			BEGIN
			    PERFORM pg_sleep(1.5);
			    FOR i IN 1..3 LOOP
			        BEGIN
			            PERFORM * FROM public.op_read_tenant_plaques(current_setting('tappa_test.session'),
			                                                         current_setting('tappa_test.ticket'),
			                                                         current_setting('tappa_test.tenant')::uuid);
			            n_data := n_data + 1;
			        EXCEPTION WHEN invalid_authorization_specification THEN
			            n_refused := n_refused + 1;
			        END;
			    END LOOP;
			    PERFORM set_config('tappa_test.result', n_data || '/' || n_refused, true);
			END
			$d$`); err != nil {
			return err
		}
		return sp.QueryRow(ctx, `SELECT current_setting('tappa_test.result')`).Scan(&result)
	}); err != nil {
		t.Fatalf("the DO block: %v", err)
	}
	if result != "0/3" {
		t.Errorf("inside one DO statement whose sleep outlived the ticket: data/refused = %s, want 0/3 (a frozen clock reads the ticket alive)", result)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, session); n != 0 {
		t.Errorf("%d expired ticket(s) consumed", n)
	}
}

// TestOpReadTenantPlaques_TheReadWritesNoPlaque is CLAUDE.md §4.4 for a read that RETURNS
// last_ctr: the read changes no plaque row -- each fixture row's physical version (ctid and
// xmin) and last_ctr are the same after it -- because tappa_opdefiner holds no INSERT,
// UPDATE, DELETE or TRUNCATE on tags (catalogue, asserted here as well as in the privilege
// matrix). The atomic advance is untouched: after the read, the shipped AdvanceTagCounter,
// as tappa_app in the tenant's context, advances the counter, a replay of the same value is
// pgx.ErrNoRows, and a second read returns the advanced value.
func TestOpReadTenantPlaques_TheReadWritesNoPlaque(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	f := opPlaqueTenant(t, ctx, tx, "op13 counter "+opToken(t), 0)
	versions := func() string {
		t.Helper()
		var s string
		if err := tx.QueryRow(ctx, `SELECT string_agg(uid || ':' || ctid::text || ':' || xmin::text || ':' || last_ctr, ',' ORDER BY uid)
		                             FROM tags WHERE tenant_id = $1`, f.id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := versions()
	rows, err := opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, f.id, xact), f.id)
	if err != nil || len(rows) != len(opPlaqueStates) {
		t.Fatalf("the read: %d row(s), err %v", len(rows), err)
	}
	if after := versions(); after != before {
		t.Errorf("the read changed a plaque row:\n before %s\n after  %s", before, after)
	}
	for _, priv := range []string{"INSERT", "UPDATE"} {
		if got := opColumns(t, ctx, tx, "tappa_opdefiner", "tags", priv); got != "" {
			t.Errorf("tappa_opdefiner %s on tags = (%s), want ()", priv, got)
		}
	}
	for _, priv := range []string{"DELETE", "TRUNCATE"} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_table_privilege('tappa_opdefiner', 'public.tags', $1)`, priv).Scan(&may); err != nil || may {
			t.Errorf("has_table_privilege(tappa_opdefiner, tags, %s) = %v (err %v), want false", priv, may, err)
		}
	}

	uid := f.uid["wall"]
	var ctrBefore int32
	for _, r := range rows {
		if r.uid != nil && *r.uid == uid && r.lastCtr != nil {
			ctrBefore = *r.lastCtr
		}
	}
	if ctrBefore != 11 {
		t.Fatalf("PREMISE: the read returned last_ctr %d for the wall plaque, want the fixture's 11", ctrBefore)
	}
	advance := func(ctr int32) error {
		return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.id.String()); err != nil {
				return err
			}
			_, e := store.New(sp).AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{Ctr: ctr, TenantID: f.id, Uid: uid})
			return e
		})
	}
	if err := advance(ctrBefore + 5); err != nil {
		t.Fatalf("AdvanceTagCounter after the read: %v", err)
	}
	if err := advance(ctrBefore + 5); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("a replay of the same counter after the read: %v, want pgx.ErrNoRows", err)
	}
	rows, err = opReadPlaques(t, ctx, tx, hash, opForgePlaques(t, ctx, tx, session, a.id, f.id, xact), f.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.uid != nil && *r.uid == uid && (r.lastCtr == nil || *r.lastCtr != ctrBefore+5) {
			t.Errorf("the second read returned last_ctr %v for the wall plaque, want %d", r.lastCtr, ctrBefore+5)
		}
	}
}

// --------------------------------------------------------- with real commits --

// TestOpReadTenantPlaques_TwoPhaseLifecycle is ADR 0021 §2 v 3's table on the shipped read
// with REAL commits: the accessor's phase one (beginOperatorRead) is committed and its
// 'read' row -- scope 'tenant_plaques', the tenant named -- is permanent; another session
// and another tenant are refused; the accessor's phase two (readTenantPlaques) returns the
// inventory (a tenant, an entrance and two plaques written inside the read's own
// transaction, which is rolled back -- no plaque is committed); the rolled-back read leaves
// the row and -- ADR 0021 limit 4 -- lets the same ticket read again; a COMMITTED read of an
// id that names no committed tenant is ErrNoSuchTenant and consumes the ticket, after which
// it is refused; and through all of it the read has exactly ONE 'read' row.
func TestOpReadTenantPlaques_TwoPhaseLifecycle(t *testing.T) {
	ctx, f := opLiveFixture(t)
	otherHash, otherSession := f.newSession(t, ctx)
	conn := f.connect(t, ctx)
	tenant := uuid.New()
	name := "op13 lifecycle " + opToken(t)

	tx := asOperatorTx(t, ctx, conn)
	ticket, err := beginOperatorRead(ctx, tx, f.hash, tenantPlaquesReadKind, []byte(opDetailParams(tenant.String())))
	if err != nil {
		t.Fatalf("phase one: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit phase one: %v", err)
	}
	scoped := func() int64 {
		return opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1
		                                 AND target_scope = 'tenant_plaques' AND target_tenant_id = $2`, f.session, tenant)
	}
	if n := scoped(); n != 1 {
		t.Fatalf("after the committed phase one: %d 'tenant_plaques' row(s) naming the tenant, want 1", n)
	}
	refused := func(what, hash string, id uuid.UUID) {
		t.Helper()
		rtx := asOperatorTx(t, ctx, conn)
		_, e := opScanPlaques(ctx, rtx, hash, ticket.reveal(), id)
		opWantClean(t, e, sqlstateInvalidAuthorization, tenantPlaquesRefusal, what)
		if err := rtx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refused("the ticket from another session of the same operator", otherHash, tenant)
	refused("the ticket for another tenant", f.hash, uuid.New())

	read := func(commit bool) (TenantPlaqueInventory, error) {
		t.Helper()
		rtx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rtx.Rollback(context.Background()) }()
		if !commit {
			door := uuid.New()
			for _, s := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO tenants (id, name, vat_number, business_type, structure, created_at)
				  VALUES ($1, $2, $3, 'hotel', 'single', clock_timestamp() + interval '1 day')`, []any{tenant, name, "VAT-OP13-" + tenant.String()}},
				{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'op13 lifecycle door')`, []any{door, tenant}},
				{`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'active')`, []any{opUID(t), tenant, door}},
				{`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned')`, []any{opUID(t), tenant}},
			} {
				if _, err := rtx.Exec(ctx, s.sql, s.args...); err != nil {
					t.Fatalf("fixture: %v", err)
				}
			}
		}
		if _, err := rtx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
			t.Fatal(err)
		}
		inv, rerr := readTenantPlaques(ctx, rtx, f.hash, ticket, tenant)
		if commit {
			if err := rtx.Commit(ctx); err != nil {
				t.Fatalf("commit the read: %v", err)
			}
		}
		return inv, rerr
	}
	inv, err := read(false)
	if err != nil || inv.TenantID != tenant || inv.TenantName != name || inv.Total != 2 || len(inv.Plaques) != 2 {
		t.Errorf("the read (rolled back) returned %+v, err %v; want the fixture tenant and its two plaques", inv, err)
	}
	if n := scoped(); n != 1 {
		t.Errorf("after the rolled-back read: %d 'read' row(s), want the 1 committed by phase one", n)
	}
	// ADR 0021 limit 4, measured: the rolled-back read un-consumed the ticket.
	if _, err := read(true); !errors.Is(err, ErrNoSuchTenant) {
		t.Errorf("the committed read of an id no committed tenant has: %v, want ErrNoSuchTenant", err)
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, f.session); n != 1 {
		t.Errorf("after the committed read %d ticket(s) of the session are consumed, want 1", n)
	}
	refused("the ticket after a COMMITTED read consumed it", f.hash, tenant)
	if n := scoped(); n != 1 {
		t.Errorf("at the end: %d 'read' row(s), want exactly 1", n)
	}
	if n := f.liveReads(t, ctx, otherSession); n != 0 {
		t.Errorf("the other session has %d 'read' row(s)", n)
	}
}

// TestTenantPlaques_OnThePoolTheTwoPhasesAreTwoTransactions: on a pool built by the
// production constructor (*pgxpool.Pool -- what *OperatorDB holds) TenantPlaques' two
// phases are two transactions: for an id that names NO tenant the answer is
// ErrNoSuchTenant AFTER the 'read' row naming it committed. Inside one transaction it is
// ErrOperatorRefused; an unknown session is ErrOperatorRefused and writes nothing. Then a
// committed tenant that holds plaques (the database's tenant with the most; this test
// writes none): the inventory carries the owner's name for it, and every plaque it returns
// is one the owner reads as that tenant's, in the list's order; Total is not below the rows
// returned. Since OP-13 phase B the METHOD (*OperatorDB).TenantPlaques -- the one the
// plaque screen calls -- is driven on the same pool too: an unknown id is ErrNoSuchTenant
// after one more committed 'read' row (inside one transaction it would be
// ErrOperatorRefused). It commits three 'read' rows (two when the database holds no
// plaque).
func TestTenantPlaques_OnThePoolTheTwoPhasesAreTwoTransactions(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("open the operator pool: %v", err)
	}
	defer o.Close()

	unknown := uuid.New()
	if _, err := TenantPlaques(ctx, o.pool, f.hash, unknown); !errors.Is(err, ErrNoSuchTenant) {
		t.Errorf("TenantPlaques of an unknown tenant on the pool: %v, want ErrNoSuchTenant", err)
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1
	                                 AND target_scope = 'tenant_plaques' AND target_tenant_id = $2`, f.session, unknown); n != 1 {
		t.Errorf("the inventory of an unknown tenant committed %d 'read' row(s) naming it, want 1", n)
	}
	tx := asOperatorTx(t, ctx, f.connect(t, ctx))
	if _, err := TenantPlaques(ctx, tx, f.hash, unknown); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantPlaques inside ONE transaction: %v, want ErrOperatorRefused", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := TenantPlaques(ctx, o.pool, opRandHex(t), unknown); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantPlaques with an unknown session: %v, want ErrOperatorRefused", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 1 {
		t.Errorf("the session has %d committed 'read' row(s), want 1 (the refused calls none)", n)
	}
	// Through the method (OP-13 phase B), on the production-built pool.
	if _, err := o.TenantPlaques(ctx, f.hash, unknown); !errors.Is(err, ErrNoSuchTenant) {
		t.Errorf("(*OperatorDB).TenantPlaques of an unknown tenant on its pool: %v, want ErrNoSuchTenant", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 2 {
		t.Errorf("the method's read left the session with %d committed 'read' row(s), want 2", n)
	}

	var real uuid.UUID
	err = f.owner.QueryRow(ctx, `SELECT tenant_id FROM tags GROUP BY tenant_id ORDER BY count(*) DESC, tenant_id LIMIT 1`).Scan(&real)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("the database holds no plaque (a migrate-only database; `make seed` loads some): the committed-tenant half was not run")
	}
	if err != nil {
		t.Fatal(err)
	}
	inv, err := TenantPlaques(ctx, o.pool, f.hash, real)
	if err != nil {
		t.Fatalf("TenantPlaques of a committed tenant with plaques: %v", err)
	}
	var name string
	if err := f.owner.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, real).Scan(&name); err != nil {
		t.Fatal(err)
	}
	own := map[string]bool{}
	for _, r := range opOwnerPlaques(t, ctx, f.owner, real) {
		own[*r.uid] = true
	}
	if inv.TenantID != real || inv.TenantName != name || len(inv.Plaques) == 0 || inv.Total < int64(len(inv.Plaques)) {
		t.Errorf("the inventory: tenant %v %q, %d plaques, Total %d; want %v %q, at least one plaque, Total >= rows",
			inv.TenantID, inv.TenantName, len(inv.Plaques), inv.Total, real, name)
	}
	for i, p := range inv.Plaques {
		if !own[p.UID] {
			t.Errorf("plaque %s is not one the owner reads as the tenant's", p.UID)
		}
		if i > 0 {
			prev := inv.Plaques[i-1]
			if opLocationAfter(prev.LocationID, p.LocationID) || (opSameLocation(prev.LocationID, p.LocationID) && prev.UID >= p.UID) {
				t.Errorf("plaques %s and %s are out of the list's order (stock first, then by uid)", prev.UID, p.UID)
			}
		}
	}
	if n := f.liveReads(t, ctx, f.session); n != 3 {
		t.Errorf("the session has %d committed 'read' row(s), want 3", n)
	}
}

// opLocationAfter reports whether location a sorts after b under ORDER BY location_id
// NULLS FIRST (a uuid compares as its canonical text does).
func opLocationAfter(a, b *uuid.UUID) bool {
	switch {
	case a == nil:
		return false
	case b == nil:
		return true
	}
	return a.String() > b.String()
}

func opSameLocation(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ------------------------------------------------------------- without a DB --

// opShapeCell is one cell of the shape mapping: a status the schema names, on a wall or
// not, with an encode stamp or not -- and the shape it must read as, with the reason.
type opShapeCell struct {
	status           string
	mounted, encoded bool
	want             PlaqueShape
	why              string
}

// opShapeCells is the WHOLE product of tags_status_check's four values x location x
// encode stamp: sixteen cells, every one written out. A cell the schema forbids reads
// PlaqueUnrecognised -- never the neighbouring state (2nd round, B1: the first table held
// eleven cells and four mutants that read a forbidden cell, or a lost plaque on a wall, as
// a neighbour stayed green).
var opShapeCells = []opShapeCell{
	{"active", true, true, PlaqueOnAWall, "in service, encode recorded"},
	{"active", true, false, PlaqueOnAWallNeverEncoded, "A-1 (T75): in service, no record that the chip took its keys"},
	{"active", false, true, PlaqueUnrecognised, "forbidden: tags_active_requires_location"},
	{"active", false, false, PlaqueUnrecognised, "forbidden: tags_active_requires_location"},
	{"unassigned", false, true, PlaqueInStock, "stock, encoded: ready to mount"},
	{"unassigned", false, false, PlaqueInStockNotEncoded, "stock, not encoded: the panel refuses to mount it (00025)"},
	{"unassigned", true, true, PlaqueUnrecognised, "forbidden: tags_unassigned_has_no_location"},
	{"unassigned", true, false, PlaqueUnrecognised, "forbidden: tags_unassigned_has_no_location"},
	{"retired", true, true, PlaqueRetired, "replaced; the stamp changes nothing for a retired plaque"},
	{"retired", true, false, PlaqueRetired, "replaced; the stamp changes nothing for a retired plaque"},
	{"retired", false, true, PlaqueUnrecognised, "forbidden: tags_retired_keeps_its_location"},
	{"retired", false, false, PlaqueUnrecognised, "forbidden: tags_retired_keeps_its_location"},
	{"lost", true, true, PlaqueLost, "allowed (the development database holds one): lost, never 'on a wall'"},
	{"lost", true, false, PlaqueLost, "allowed: lost wherever it was"},
	{"lost", false, true, PlaqueLost, "allowed: lost from stock"},
	{"lost", false, false, PlaqueLost, "allowed: lost from stock"},
}

// TestTenantPlaque_ShapeIsAClosedMapping: the shape is read from status, encoded_at and
// location together, and the mapping is CLOSED.
//   - The sixteen cells of opShapeCells -- asserted to be the whole product of the four
//     schema statuses x on a wall x stamped, each exactly once -- read as written there:
//     six shapes for the states the schema allows (A-1 its own, a lost plaque lost in all
//     four cells) and PlaqueUnrecognised for the six it forbids.
//   - A status the mapping does not name reads PlaqueUnrecognised in all four location x
//     stamp cells: a fifth value, an empty one, another case, a leading or trailing space,
//     a line break, a NUL, a zero-width space.
//
// Such a row is never read as a neighbouring state.
func TestTenantPlaque_ShapeIsAClosedMapping(t *testing.T) {
	door := uuid.New()
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	shape := func(status string, mounted, encoded bool) PlaqueShape {
		p := TenantPlaque{UID: "04AABBCCDDEEFF", Status: status}
		if mounted {
			p.LocationID = &door
		}
		if encoded {
			p.EncodedAt = &at
		}
		return p.Shape()
	}
	type key struct {
		status           string
		mounted, encoded bool
	}
	cells := map[key]bool{}
	for _, c := range opShapeCells {
		k := key{c.status, c.mounted, c.encoded}
		if cells[k] {
			t.Fatalf("PREMISE: the cell %+v is written twice", k)
		}
		cells[k] = true
		if got := shape(c.status, c.mounted, c.encoded); got != c.want {
			t.Errorf("%s, on a wall %v, stamped %v: %q, want %q (%s)", c.status, c.mounted, c.encoded, got, c.want, c.why)
		}
	}
	for _, status := range []string{"active", "unassigned", "retired", "lost"} {
		for _, mounted := range []bool{true, false} {
			for _, encoded := range []bool{true, false} {
				if !cells[key{status, mounted, encoded}] {
					t.Errorf("PREMISE: the cell %s / on a wall %v / stamped %v is not in the table", status, mounted, encoded)
				}
			}
		}
	}
	if len(cells) != 16 {
		t.Errorf("PREMISE: %d cells, want the 16 of the product", len(cells))
	}
	for _, status := range []string{"quarantined", "", "Active", "LOST", " retired", "active ", "lost\n", "unassigned\x00", "lost\u200b"} {
		for _, mounted := range []bool{true, false} {
			for _, encoded := range []bool{true, false} {
				if got := shape(status, mounted, encoded); got != PlaqueUnrecognised {
					t.Errorf("the status %q, on a wall %v, stamped %v: %q, want %q", status, mounted, encoded, got, PlaqueUnrecognised)
				}
			}
		}
	}
}

// opPlaqueShapeNames are the seven PlaqueShape values the package declares.
var opPlaqueShapeNames = []string{"PlaqueInStock", "PlaqueInStockNotEncoded", "PlaqueLost", "PlaqueOnAWall",
	"PlaqueOnAWallNeverEncoded", "PlaqueRetired", "PlaqueUnrecognised"}

// opShapePin type-checks one package (files, with the standard library's go/types and
// whichever importer it is given) and reads TenantPlaque.Shape against it:
//   - consts: EVERY package-level constant whose type is the package's PlaqueShape, whichever
//     of the given files declares it and however it is spelled (typed spec, typeless spec
//     with a conversion, a const block of its own);
//   - returned: the constants Shape's return statements name;
//   - findings: every return statement of Shape (a function literal inside it included)
//     that is NOT a single identifier resolving to one of those package-level constants --
//     a literal, a conversion, a call, a local or untyped constant, a variable, a bare
//     return. Fail-closed: the rule names the one accepted form, not the rejected ones.
//
// A type error is an error, not an empty answer.
func opShapePin(fset *token.FileSet, files []*ast.File, imp types.Importer) (consts, returned, findings []string, err error) {
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	var typeErrs []error
	conf := types.Config{Importer: imp, Error: func(e error) { typeErrs = append(typeErrs, e) }}
	pkg, _ := conf.Check("db", fset, files, info)
	if len(typeErrs) > 0 {
		return nil, nil, nil, fmt.Errorf("type-check: %d error(s), the first: %w", len(typeErrs), typeErrs[0])
	}
	shapeType, ok := pkg.Scope().Lookup("PlaqueShape").(*types.TypeName)
	if !ok {
		return nil, nil, nil, errors.New("the package declares no type PlaqueShape")
	}
	set := map[types.Object]bool{}
	for _, name := range pkg.Scope().Names() {
		if c, ok := pkg.Scope().Lookup(name).(*types.Const); ok && types.Identical(c.Type(), shapeType.Type()) {
			set[c] = true
			consts = append(consts, name)
		}
	}
	plaque, ok := pkg.Scope().Lookup("TenantPlaque").(*types.TypeName)
	if !ok {
		return nil, nil, nil, errors.New("the package declares no type TenantPlaque")
	}
	method, _, _ := types.LookupFieldOrMethod(plaque.Type(), false, pkg, "Shape")
	if method == nil {
		return nil, nil, nil, errors.New("TenantPlaque has no method Shape")
	}
	var body *ast.BlockStmt
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && info.Defs[fd.Name] == method {
				body = fd.Body
			}
		}
	}
	if body == nil {
		return nil, nil, nil, errors.New("the declaration of TenantPlaque.Shape was not found")
	}
	returns := 0
	ast.Inspect(body, func(n ast.Node) bool {
		r, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		returns++
		where := fset.Position(r.Pos()).String()
		if len(r.Results) != 1 {
			findings = append(findings, fmt.Sprintf("%s: a return with %d results", where, len(r.Results)))
			return true
		}
		id, ok := r.Results[0].(*ast.Ident)
		if !ok {
			findings = append(findings, fmt.Sprintf("%s: returns a %T, not a named PlaqueShape constant", where, r.Results[0]))
			return true
		}
		if obj := info.Uses[id]; !set[obj] {
			findings = append(findings, fmt.Sprintf("%s: returns %s, which is not a package-level PlaqueShape constant", where, id.Name))
			return true
		}
		if !slices.Contains(returned, id.Name) {
			returned = append(returned, id.Name)
		}
		return true
	})
	if returns == 0 {
		findings = append(findings, "Shape has no return statement")
	}
	sort.Strings(consts)
	sort.Strings(returned)
	return consts, returned, findings, nil
}

// TestPlaqueShape_TheSevenValuesAreTheOnesShapeReturns pins, by TYPE-CHECKING package db's
// product source (go/build's GoFiles; go/types with the standard library's source importer,
// no other dependency), that:
//   - (a) the package-level constants of type PlaqueShape -- every file go/build selects for
//     the test's own build context, every spelling -- are exactly the seven of
//     opPlaqueShapeNames;
//   - (b) every return statement of TenantPlaque.Shape is one of those constants by name --
//     a literal, a conversion, a helper's result or any other expression is a finding;
//   - (c) Shape returns each of the seven.
//
// So the phase-B screen's dictionary (one sentence per value) cannot meet a value that is
// declared and never returned, returned and never declared, or produced some other way.
// Threat model: an ACCIDENTAL change -- a new state added in one place and not the other.
// Code written to slip past a type-checked reading of Shape is for code review.
// CONTROLS: the same reading of synthetic packages reports a literal, a conversion and a
// helper's result as findings, and counts a typeless constant spec and a constant in a
// second file. (3rd round, F1: the first version read operator.go's syntax alone -- a typed
// spec, an identifier return -- and four spellings passed it.)
// COUNTED LIMITS (4th round, N3): "every file" is the files go/build selects for the
// context the test runs in -- its GOOS/GOARCH, its cgo setting, its tags. CI runs the
// tests with CGO_ENABLED=1 (-race needs it on linux/amd64) while the product binary is
// built with CGO_ENABLED=0 (Makefile `build`, the Dockerfile's build), so a constant in a
// file constrained to `!cgo` would pass this pin in CI and be in the shipped binary. And the
// pin reads return statements, not what a deferred function does to a named result.
func TestPlaqueShape_TheSevenValuesAreTheOnesShapeReturns(t *testing.T) {
	bp, err := build.Default.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("read the package: %v", err)
	}
	if !slices.Contains(bp.GoFiles, "operator.go") || len(bp.CgoFiles) != 0 {
		t.Fatalf("PREMISE: GoFiles %v, CgoFiles %v", bp.GoFiles, bp.CgoFiles)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range bp.GoFiles {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	consts, returned, findings, err := opShapePin(fset, files, importer.ForCompiler(fset, "source", nil))
	if err != nil {
		t.Fatalf("package db: %v", err)
	}
	for _, f := range findings {
		t.Errorf("TenantPlaque.Shape: %s", f)
	}
	if !slices.Equal(consts, opPlaqueShapeNames) {
		t.Errorf("the package declares the PlaqueShape constants %v, want %v", consts, opPlaqueShapeNames)
	}
	if !slices.Equal(returned, opPlaqueShapeNames) {
		t.Errorf("TenantPlaque.Shape returns %v, want each of %v", returned, opPlaqueShapeNames)
	}
	seen := map[PlaqueShape]bool{}
	for _, s := range []PlaqueShape{PlaqueOnAWall, PlaqueOnAWallNeverEncoded, PlaqueInStock, PlaqueInStockNotEncoded,
		PlaqueRetired, PlaqueLost, PlaqueUnrecognised} {
		if s == "" || seen[s] {
			t.Errorf("the shape %q is empty or named twice", s)
		}
		seen[s] = true
	}

	// CONTROLS: synthetic packages, no imports.
	const head = "package db\n\ntype PlaqueShape string\n\ntype TenantPlaque struct{ Status string }\n\n"
	for _, c := range []struct {
		name          string
		srcs          []string
		consts, rets  string
		wantFinding   string
		wantNoFinding bool
	}{
		{"accepted shape", []string{head + "const (\n\tA PlaqueShape = \"a\"\n\tB PlaqueShape = \"b\"\n)\n\nfunc (p TenantPlaque) Shape() PlaqueShape {\n\tif p.Status == \"a\" {\n\t\treturn A\n\t}\n\treturn B\n}\n"},
			"A,B", "A,B", "", true},
		{"a literal", []string{head + "const A PlaqueShape = \"a\"\n\nfunc (p TenantPlaque) Shape() PlaqueShape {\n\tif p.Status == \"x\" {\n\t\treturn \"x\"\n\t}\n\treturn A\n}\n"},
			"A", "A", "*ast.BasicLit", false},
		{"a conversion", []string{head + "const A PlaqueShape = \"a\"\n\nfunc (p TenantPlaque) Shape() PlaqueShape {\n\tif p.Status == \"x\" {\n\t\treturn PlaqueShape(\"x\")\n\t}\n\treturn A\n}\n"},
			"A", "A", "*ast.CallExpr", false},
		{"a helper's result", []string{head + "const A PlaqueShape = \"a\"\n\nfunc helper() PlaqueShape { return A }\n\nfunc (p TenantPlaque) Shape() PlaqueShape {\n\treturn helper()\n}\n"},
			"A", "", "*ast.CallExpr", false},
		{"an untyped constant", []string{head + "const A PlaqueShape = \"a\"\n\nconst U = \"u\"\n\nfunc (p TenantPlaque) Shape() PlaqueShape {\n\tif p.Status == \"u\" {\n\t\treturn U\n\t}\n\treturn A\n}\n"},
			"A", "A", "which is not a package-level PlaqueShape constant", false},
		{"a typeless spec, never returned", []string{head + "const (\n\tA PlaqueShape = \"a\"\n\tD = PlaqueShape(\"d\")\n)\n\nfunc (p TenantPlaque) Shape() PlaqueShape {\n\treturn A\n}\n"},
			"A,D", "A", "", true},
		{"a constant in a second file", []string{head + "const A PlaqueShape = \"a\"\n\nfunc (p TenantPlaque) Shape() PlaqueShape {\n\treturn A\n}\n",
			"package db\n\nconst D PlaqueShape = \"d\"\n"},
			"A,D", "A", "", true},
	} {
		cfset := token.NewFileSet()
		var cfiles []*ast.File
		for i, src := range c.srcs {
			f, err := parser.ParseFile(cfset, fmt.Sprintf("control%d.go", i), src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("control %q: parse: %v", c.name, err)
			}
			cfiles = append(cfiles, f)
		}
		gotConsts, gotRets, gotFindings, err := opShapePin(cfset, cfiles, importer.ForCompiler(cfset, "source", nil))
		if err != nil {
			t.Fatalf("control %q: %v", c.name, err)
		}
		if strings.Join(gotConsts, ",") != c.consts || strings.Join(gotRets, ",") != c.rets {
			t.Errorf("CONTROL %q: consts %v returned %v, want [%s] [%s]", c.name, gotConsts, gotRets, c.consts, c.rets)
		}
		switch {
		case c.wantNoFinding && len(gotFindings) != 0:
			t.Errorf("CONTROL %q: findings %v, want none", c.name, gotFindings)
		case !c.wantNoFinding && !slices.ContainsFunc(gotFindings, func(f string) bool { return strings.Contains(f, c.wantFinding) }):
			t.Errorf("CONTROL %q: findings %v, want one containing %q", c.name, gotFindings, c.wantFinding)
		}
	}
}
