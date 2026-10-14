package db

// holdingops_test.go -- M11 API-1a: the holding's writers (op_create_holding,
// op_add_holding_member, op_remove_holding_member -- migration 00042), their audit, the
// membership invariants, the timezone lock (00041) and the API code rule in both directions,
// against real Postgres. Single-session cases run in opTx's rolled-back transaction; the races
// run on COMMITTED fixtures over several connections (opLiveFixture's class -- what they leave
// is listed at holdingLiveFixture).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// ------------------------------------------------------------------ helpers --

// holdingCreateAs is op_create_holding as tappa_operator in a savepoint of tx.
func holdingCreateAs(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, name any) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT public.op_create_holding($1, $2)`, hash, name).Scan(&id)
	})
	return id, err
}

func holdingAddAs(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, holding, tenant any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_add_holding_member($1, $2, $3)`, hash, holding, tenant)
}

func holdingRemoveAs(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, holding, tenant any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_remove_holding_member($1, $2, $3)`, hash, holding, tenant)
}

// holdingTenant is a new tenant of the zone, with the API code (or none), written by the owner.
func holdingTenant(t *testing.T, ctx context.Context, tx pgx.Tx, zone string, code any) uuid.UUID {
	t.Helper()
	id := opNewTenant(t, ctx, tx, "api1a "+opToken(t), 0)
	if _, err := tx.Exec(ctx, `UPDATE tenants SET timezone = $2, api_code = $3 WHERE id = $1`, id, zone, code); err != nil {
		t.Fatalf("zone and code: %v", err)
	}
	return id
}

// holdingMembership is one row of holding_members, as the owner reads it.
type holdingMembership struct {
	holding, tenant uuid.UUID
	joined          time.Time
	left            *time.Time
}

func holdingMembershipsOf(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) []holdingMembership {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT holding_id, member_tenant_id, joined_at, left_at FROM holding_members
	                            WHERE member_tenant_id = $1 ORDER BY joined_at`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (holdingMembership, error) {
		var m holdingMembership
		err := r.Scan(&m.holding, &m.tenant, &m.joined, &m.left)
		return m, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// holdingLiveIn reports whether tenant has a live membership of holding.
func holdingLiveIn(t *testing.T, ctx context.Context, q opQuerier, holding, tenant uuid.UUID) bool {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM holding_members WHERE holding_id = $1 AND member_tenant_id = $2 AND left_at IS NULL`,
		holding, tenant) == 1
}

// holdingTrailRow is one tenant audit_log row a holding writer wrote.
type holdingTrailRow struct {
	action string
	actor  *uuid.UUID
	target *string
	detail map[string]any
	at     time.Time
}

func holdingTrailOf(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) []holdingTrailRow {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT action, actor_id, target, detail, at FROM audit_log
	                            WHERE tenant_id = $1 AND action LIKE 'tenant.holding\_%' ORDER BY at, id`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []holdingTrailRow
	for rows.Next() {
		var r holdingTrailRow
		var raw []byte
		if err := rows.Scan(&r.action, &r.actor, &r.target, &raw, &r.at); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &r.detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// holdingOperatorRow is one operator_audit_log row of a holding kind.
type holdingOperatorRow struct {
	kind    string
	tenant  *uuid.UUID
	holding string
}

func holdingOperatorRowsOf(t *testing.T, ctx context.Context, q opQuerier, session uuid.UUID) []holdingOperatorRow {
	t.Helper()
	rows, err := q.Query(ctx, `
		SELECT kind, target_tenant_id, detail ->> 'holding_id' FROM operator_audit_log
		 WHERE session_id = $1 AND kind LIKE 'holding\_%'
		   AND target_admin_id IS NULL AND target_scope IS NULL AND page_number IS NULL AND page_size IS NULL
		   AND (detail - 'holding_id') = '{}'::jsonb
		 ORDER BY at, id`, session)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (holdingOperatorRow, error) {
		var o holdingOperatorRow
		err := r.Scan(&o.kind, &o.tenant, &o.holding)
		return o, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// --------------------------------------------------------------- the writers --

// TestOperator00042_TheFunctionsAndTheirExactSignatures pins what ADR 0021 §6's generic pins
// leave open for the four functions 00042 creates and the three it replaces: the exact
// identities (the session first, no actor; writes return void or the uuid they created; the
// read takes the RAW ticket and its page), owner, SECURITY DEFINER, proconfig, one overload,
// EXECUTE for tappa_operator alone; op_begin_read and op_read_audit are 00039's bodies plus
// exactly 00042's list edits and are the live, newest definitions; HEAD's three CHECKs name the
// holding kinds and the read kind; the forward, frozen-clock and read-consumption scans walk the
// new functions and find nothing.
func TestOperator00042_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	for name, want := range map[string]string{
		"op_create_holding":        "p_session text, p_name text -> uuid",
		"op_add_holding_member":    "p_session text, p_holding_id uuid, p_tenant_id uuid -> void",
		"op_remove_holding_member": "p_session text, p_holding_id uuid, p_tenant_id uuid -> void",
		"op_read_holdings": "p_session text, p_ticket text, p_page_number integer, p_page_size integer -> TABLE(holding_id uuid, " +
			"holding_name text, holding_created_at timestamp with time zone, member_tenant_id uuid, member_name text, " +
			"member_timezone text, member_api_code text, joined_at timestamp with time zone, left_at timestamp with time zone)",
		"op_enable_tenant_api": "p_session text, p_tenant_id uuid, p_api_code text -> void",
	} {
		if got := opIdentity(t, ctx, tx, name); got != want {
			t.Errorf("%s is %s, want %s", name, got, want)
		}
		var owner string
		var secdef bool
		var config []string
		var overloads int64
		if err := tx.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner), p.prosecdef, p.proconfig,
		                                   (SELECT count(*) FROM pg_proc q WHERE q.proname = p.proname)
		                             FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                            WHERE n.nspname = 'public' AND p.proname = $1`, name).Scan(&owner, &secdef, &config, &overloads); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if owner != "tappa_opdefiner" || !secdef || len(config) != 1 || config[0] != "search_path=pg_catalog, pg_temp" || overloads != 1 {
			t.Errorf("%s: owner %s, SECURITY DEFINER %v, proconfig %v, %d overload(s)", name, owner, secdef, config, overloads)
		}
		for who, may := range map[string]bool{"public": false, "tappa_app": false, "tappa_resolver": false, "tappa_holdingdefiner": false, "tappa_operator": true} {
			var got bool
			if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, p.oid, 'EXECUTE') FROM pg_proc p
			                             JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'public' AND p.proname = $2`, who, name).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != may {
				t.Errorf("has_function_privilege(%s, %s, EXECUTE) = %v, want %v", who, name, got, may)
			}
		}
	}

	up, _ := opMigrationSections(t, op00042File)
	up39, _ := opMigrationSections(t, op00039File)
	for fn, edits := range op00042ListEdits {
		b42 := opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", fn)
		undone := b42
		for i := len(edits) - 1; i >= 0; i-- {
			if strings.Count(undone, edits[i][1]) != 1 {
				t.Errorf("00042's %s does not hold edit %d exactly once", fn, i+1)
				continue
			}
			undone = strings.Replace(undone, edits[i][1], edits[i][0], 1)
		}
		if undone != opLogFunctionBody(t, up39, "CREATE OR REPLACE FUNCTION", fn) {
			t.Errorf("00042's %s is not 00039's body with its list edits", fn)
		}
		var live string
		if err := tx.QueryRow(ctx, `SELECT p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, fn).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != b42 || live != opNewestUpBody(t, fn) {
			t.Errorf("the live %s is not 00042's body (or 00042 is not its newest definition)", fn)
		}
	}
	var live string
	if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE proname = 'op_enable_tenant_api'`).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_enable_tenant_api") || live != opNewestUpBody(t, "op_enable_tenant_api") {
		t.Error("the live op_enable_tenant_api is not 00042's body (or 00042 is not its newest definition)")
	}

	for _, c := range []struct{ table, name, want string }{
		{"operator_audit_log", "operator_audit_log_kind_check", opKindCheckDef(opAuditKinds)},
		{"operator_read_tickets", "operator_read_tickets_kind_check", opKindCheckDef(opKindsAt42)},
		{"operator_audit_log", opTenantWriteShape, opTenantWriteShapeDefOf(opTenantWriteKinds)},
		{"operator_audit_log", "operator_audit_log_holding_write_shape", opHoldingWriteShapeDef},
	} {
		if def, valid := opConstraint(t, ctx, tx, c.table, c.name); def != c.want || !valid {
			t.Errorf("%s is %s (validated %v),\n want %s, validated", c.name, def, valid, c.want)
		}
	}

	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for _, fn := range []string{"op_create_holding", "op_add_holding_member", "op_remove_holding_member", "op_read_holdings"} {
		if !slices.Contains(names, fn) {
			t.Errorf("anti-vacuity: the forward scan did not walk %s", fn)
		}
	}
	for _, f := range findings {
		t.Error(f)
	}
	clock, _, err := opFrozenClockFindings(ctx, tx)
	if err != nil {
		t.Fatalf("frozen-clock scan: %v", err)
	}
	for _, f := range clock {
		t.Error(f)
	}
	consumption, readNames, err := opReadConsumptionFindings(ctx, tx)
	if err != nil {
		t.Fatalf("consumption scan: %v", err)
	}
	if !slices.Contains(readNames, "op_read_holdings") {
		t.Error("anti-vacuity: the consumption scan did not read op_read_holdings")
	}
	for _, f := range consumption {
		t.Error(f)
	}
}

// TestHoldingOps_OnlyOperatorFunctionsWriteMembership is ADR 0027 §1's "üyeliği yalnız op_*
// yazar": on the catalogue, no role but the two definers holds a writing privilege on the two
// tables (tappa_opdefiner: INSERT (name) on holdings; INSERT (holding_id, member_tenant_id) and
// UPDATE (left_at) on holding_members -- tappa_holdingdefiner's UPDATE (lock_anchor) writes one
// value, the one there) and no role at all holds DELETE or TRUNCATE; by behaviour, tappa_app (in
// a tenant's context), tappa_operator and tappa_resolver are 42501 on a direct INSERT, UPDATE or
// DELETE; and the functions whose bodies write either table are exactly the three op_* --
// op_create_holding the holdings, op_add_holding_member and op_remove_holding_member the
// memberships.
func TestHoldingOps_OnlyOperatorFunctionsWriteMembership(t *testing.T) {
	ctx, tx := opTx(t)
	rows, err := tx.Query(ctx, `
		SELECT c.relname || ' ' || r.rolname || ' ' || p.priv
		  FROM pg_class c
		 CROSS JOIN pg_roles r
		 CROSS JOIN (VALUES ('INSERT'), ('UPDATE')) AS p(priv)
		 WHERE c.relname IN ('holdings', 'holding_members') AND c.relnamespace = 'public'::regnamespace
		   AND r.rolname LIKE 'tappa\_%' AND r.rolname <> 'tappa_owner'
		   AND has_any_column_privilege(r.oid, c.oid, p.priv)
		UNION ALL
		SELECT c.relname || ' ' || r.rolname || ' ' || p.priv
		  FROM pg_class c
		 CROSS JOIN pg_roles r
		 CROSS JOIN (VALUES ('DELETE'), ('TRUNCATE')) AS p(priv)
		 WHERE c.relname IN ('holdings', 'holding_members') AND c.relnamespace = 'public'::regnamespace
		   AND r.rolname LIKE 'tappa\_%' AND r.rolname <> 'tappa_owner'
		   AND has_table_privilege(r.oid, c.oid, p.priv)
		 ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	writers, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"holding_members tappa_holdingdefiner UPDATE",
		"holding_members tappa_opdefiner INSERT",
		"holding_members tappa_opdefiner UPDATE",
		"holdings tappa_opdefiner INSERT",
	}
	if !slices.Equal(writers, want) {
		t.Errorf("the roles that may write the holding tables are %v, want %v", writers, want)
	}
	h := holdingNew(t, ctx, tx, "api1a writers "+opToken(t))
	tenant := opNewTenant(t, ctx, tx, "api1a writers "+opToken(t), 0)
	holdingInsertMember(t, ctx, tx, h, holdingMember{tenant, "1 minute", ""})
	for _, role := range []string{"tappa_app", "tappa_operator", "tappa_resolver"} {
		for _, stmt := range []string{
			`INSERT INTO public.holding_members (holding_id, member_tenant_id) VALUES ($1, $2)`,
			`UPDATE public.holding_members SET left_at = clock_timestamp() WHERE holding_id = $1 AND member_tenant_id = $2`,
			`DELETE FROM public.holding_members WHERE holding_id = $1 AND member_tenant_id = $2`,
			`INSERT INTO public.holdings (id, name) SELECT $1, 'x' WHERE $2::uuid IS NOT NULL`,
		} {
			err := opAs(t, ctx, tx, role, func(sp pgx.Tx) error {
				if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
					return err
				}
				_, err := sp.Exec(ctx, stmt, h, tenant)
				return err
			})
			opWant(t, err, sqlstateInsufficientPrivi, role+": "+stmt)
		}
	}
	write := regexp.MustCompile(`(?i)(insert\s+into|update|delete\s+from|merge\s+into)\s+(public\.)?(holding_members|holdings)([^a-z0-9_]|$)`)
	fnRows, err := tx.Query(ctx, `SELECT p.proname, pg_get_functiondef(p.oid) FROM pg_proc p WHERE p.prokind IN ('f', 'p') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer fnRows.Close()
	got := map[string][]string{}
	for fnRows.Next() {
		var name, def string
		if err := fnRows.Scan(&name, &def); err != nil {
			t.Fatal(err)
		}
		for _, m := range write.FindAllStringSubmatch(strings.ReplaceAll(holdingStripComments(def), `"`, ""), -1) {
			table := strings.ToLower(m[3])
			if !slices.Contains(got[table], name) {
				got[table] = append(got[table], name)
			}
		}
	}
	fnRows.Close()
	for table, fns := range map[string][]string{
		"holdings":        {"op_create_holding"},
		"holding_members": {"op_add_holding_member", "op_remove_holding_member"},
	} {
		slices.Sort(got[table])
		if !slices.Equal(got[table], fns) {
			t.Errorf("the functions that write %s are %v, want %v", table, got[table], fns)
		}
	}
}

// TestHoldingOps_CreateAuditsOnlyTheOperatorLog: op_create_holding answers the new holding's id,
// writes the holding under the name given (its time the wall clock, its id the column's DEFAULT),
// and one operator row -- 'holding_created', no tenant, detail exactly {"holding_id": <the id>} --
// and NO tenant row (ADR 0027 §1's table: a new holding has no member). A name outside
// holdings_name_shape (empty, padded, 121 characters, a control character, NULL) is 22023 with
// one constant message and writes nothing; 120 characters are accepted. A dead session is 28000.
func TestHoldingOps_CreateAuditsOnlyTheOperatorLog(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenantRows := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log`)
	name := "KF & KM group " + opToken(t)
	id, err := holdingCreateAs(t, ctx, tx, hash, name)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var stored string
	var created time.Time
	if err := tx.QueryRow(ctx, `SELECT name, created_at FROM holdings WHERE id = $1`, id).Scan(&stored, &created); err != nil {
		t.Fatalf("the holding the answer names: %v", err)
	}
	if stored != name {
		t.Errorf("stored name %q, want %q", stored, name)
	}
	if rows := holdingOperatorRowsOf(t, ctx, tx, session); len(rows) != 1 || rows[0].kind != "holding_created" || rows[0].tenant != nil || rows[0].holding != id.String() {
		t.Errorf("operator rows %+v, want one holding_created naming %s and no tenant", rows, id)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log`); n != tenantRows {
		t.Errorf("creating a holding wrote %d tenant row(s)", n-tenantRows)
	}
	before := opAudit(t, ctx, tx)
	holdings := opInt(t, ctx, tx, `SELECT count(*) FROM holdings`)
	for _, bad := range []any{"", " padded", "padded ", strings.Repeat("x", 121), "tab\there", "line\nbreak", nil} {
		_, err := holdingCreateAs(t, ctx, tx, hash, bad)
		opWantClean(t, err, sqlstateInvalidParameter, "op_create_holding: change refused", fmt.Sprintf("the name %q", bad))
	}
	if opAudit(t, ctx, tx) != before || opInt(t, ctx, tx, `SELECT count(*) FROM holdings`) != holdings {
		t.Error("a refused name wrote a row")
	}
	if _, err := holdingCreateAs(t, ctx, tx, hash, strings.Repeat("y", 120)); err != nil {
		t.Errorf("CONTROL: a 120-character name was refused: %v", err)
	}
	for _, d := range opDeadSessions(t, ctx, tx) {
		_, err := holdingCreateAs(t, ctx, tx, d.hash, "dead "+opToken(t))
		opWant(t, err, sqlstateInvalidAuthorization, "create with a session that is "+d.name)
	}
}

// TestHoldingOps_FirstAddAuditsTheOperatorAndTheAddedTenant: the first member's add writes the
// membership (joined_at the wall clock), one 'tenant.holding_joined' row in the tenant's own
// audit_log (the operator as actor, the holding as target, detail exactly {actor_kind, holding_id},
// `at` the instant joined_at took) and the operator's 'holding_member_added' row naming the tenant
// and the holding -- and nothing in any other tenant's log.
func TestHoldingOps_FirstAddAuditsTheOperatorAndTheAddedTenant(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a first "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	kf := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	others := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id <> $1`, kf)
	if err := holdingAddAs(t, ctx, tx, hash, h, kf); err != nil {
		t.Fatalf("add: %v", err)
	}
	ms := holdingMembershipsOf(t, ctx, tx, kf)
	if len(ms) != 1 || ms[0].holding != h || ms[0].left != nil {
		t.Fatalf("memberships %+v, want one live membership of %s", ms, h)
	}
	trail := holdingTrailOf(t, ctx, tx, kf)
	if len(trail) != 1 {
		t.Fatalf("%d tenant rows for the added tenant, want 1", len(trail))
	}
	r := trail[0]
	if r.action != "tenant.holding_joined" || r.actor == nil || *r.actor != a.id || r.target == nil || *r.target != h.String() ||
		len(r.detail) != 2 || r.detail["actor_kind"] != "operator" || r.detail["holding_id"] != h.String() || !r.at.Equal(ms[0].joined) {
		t.Errorf("the tenant row is %+v; want tenant.holding_joined by the operator on the holding, {actor_kind, holding_id}, at joined_at %v", r, ms[0].joined)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id <> $1`, kf); n != others {
		t.Errorf("%d rows in other tenants' logs", n-others)
	}
	rows := holdingOperatorRowsOf(t, ctx, tx, session)
	if len(rows) != 2 || rows[1].kind != "holding_member_added" || rows[1].tenant == nil || *rows[1].tenant != kf || rows[1].holding != h.String() {
		t.Errorf("operator rows %+v, want holding_created then holding_member_added naming the tenant and the holding", rows)
	}
}

// TestHoldingOps_SecondAddAuditsTheOperatorAndBothMembers: the second member's add writes
// 'tenant.holding_joined' in its own log and 'tenant.holding_member_joined' in the first member's
// -- detail {actor_kind, holding_id, member_tenant_id: the one who joined}, the same `at` -- and the
// operator's row; a third tenant's log is untouched. With three members, the third add writes into
// BOTH other members' logs (ADR 0027 §1: "her diğer üye"); an ended membership gets no row.
func TestHoldingOps_SecondAddAuditsTheOperatorAndBothMembers(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a second "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	kf := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	km := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	k3 := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	gone := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	holdingInsertMember(t, ctx, tx, h, holdingMember{gone, "10 minutes", "5 minutes"})
	for _, x := range []uuid.UUID{kf, km} {
		if err := holdingAddAs(t, ctx, tx, hash, h, x); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	joined := holdingMembershipsOf(t, ctx, tx, km)[0].joined
	kfTrail := holdingTrailOf(t, ctx, tx, kf)
	if len(kfTrail) != 2 || kfTrail[1].action != "tenant.holding_member_joined" || kfTrail[1].detail["member_tenant_id"] != km.String() ||
		kfTrail[1].detail["holding_id"] != h.String() || kfTrail[1].detail["actor_kind"] != "operator" || len(kfTrail[1].detail) != 3 ||
		!kfTrail[1].at.Equal(joined) || kfTrail[1].actor == nil || *kfTrail[1].actor != a.id {
		t.Errorf("the first member's log is %+v; want its own join, then tenant.holding_member_joined naming the second at its joined_at", kfTrail)
	}
	if kmTrail := holdingTrailOf(t, ctx, tx, km); len(kmTrail) != 1 || kmTrail[0].action != "tenant.holding_joined" {
		t.Errorf("the second member's log is %+v, want its own join only", kmTrail)
	}
	if len(holdingTrailOf(t, ctx, tx, k3)) != 0 || len(holdingTrailOf(t, ctx, tx, gone)) != 0 {
		t.Error("a tenant outside the add (or a member that had left) got a row")
	}
	if err := holdingAddAs(t, ctx, tx, hash, h, k3); err != nil {
		t.Fatal(err)
	}
	for _, x := range []uuid.UUID{kf, km} {
		tr := holdingTrailOf(t, ctx, tx, x)
		if last := tr[len(tr)-1]; last.action != "tenant.holding_member_joined" || last.detail["member_tenant_id"] != k3.String() {
			t.Errorf("the third add did not write into %s's log: %+v", x, last)
		}
	}
	if len(holdingTrailOf(t, ctx, tx, gone)) != 0 {
		t.Error("the member that had left got a row of the third add")
	}
	if n := len(holdingOperatorRowsOf(t, ctx, tx, session)); n != 4 {
		t.Errorf("%d operator rows, want the create and three adds", n)
	}
}

// TestHoldingOps_RemoveAuditsTheOperatorTheLeaverAndTheRemainingMember: the remove ends the live
// membership at the wall clock (left_at), writes 'tenant.holding_left' in the leaver's log and
// 'tenant.holding_member_left' (naming the leaver) in the remaining member's, both at left_at, and
// the operator's 'holding_member_removed' row. A second remove of the same tenant changes nothing
// and writes no tenant row -- and still the operator's row (the act was asked for).
func TestHoldingOps_RemoveAuditsTheOperatorTheLeaverAndTheRemainingMember(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a remove "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	kf := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	km := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	for _, x := range []uuid.UUID{kf, km} {
		if err := holdingAddAs(t, ctx, tx, hash, h, x); err != nil {
			t.Fatal(err)
		}
	}
	if err := holdingRemoveAs(t, ctx, tx, hash, h, km); err != nil {
		t.Fatalf("remove: %v", err)
	}
	ms := holdingMembershipsOf(t, ctx, tx, km)
	if len(ms) != 1 || ms[0].left == nil {
		t.Fatalf("after remove: %+v, want the membership ended", ms)
	}
	left := *ms[0].left
	km1 := holdingTrailOf(t, ctx, tx, km)
	kf1 := holdingTrailOf(t, ctx, tx, kf)
	if last := km1[len(km1)-1]; last.action != "tenant.holding_left" || len(last.detail) != 2 || last.detail["holding_id"] != h.String() || !last.at.Equal(left) {
		t.Errorf("the leaver's last row is %+v, want tenant.holding_left {actor_kind, holding_id} at left_at", last)
	}
	if last := kf1[len(kf1)-1]; last.action != "tenant.holding_member_left" || last.detail["member_tenant_id"] != km.String() || !last.at.Equal(left) {
		t.Errorf("the remaining member's last row is %+v, want tenant.holding_member_left naming the leaver at left_at", last)
	}
	if err := holdingRemoveAs(t, ctx, tx, hash, h, km); err != nil {
		t.Fatalf("second remove: %v", err)
	}
	if again := holdingMembershipsOf(t, ctx, tx, km); len(again) != 1 || !again[0].left.Equal(left) {
		t.Errorf("a second remove moved left_at: %+v", again)
	}
	if len(holdingTrailOf(t, ctx, tx, km)) != len(km1) || len(holdingTrailOf(t, ctx, tx, kf)) != len(kf1) {
		t.Error("a second remove wrote tenant rows")
	}
	rows := holdingOperatorRowsOf(t, ctx, tx, session)
	if n := len(rows); n != 5 || rows[3].kind != "holding_member_removed" || rows[4].kind != "holding_member_removed" ||
		rows[4].tenant == nil || *rows[4].tenant != km {
		t.Errorf("operator rows %+v, want create, two adds and two holding_member_removed naming the leaver", rows)
	}
}

// TestHoldingOps_WritesAnswerTheSameWhateverTheyFind is ADR 0021 §2 v 7 and B14 for the two
// membership writers: an unknown holding, an unknown tenant, a refused add (every rule) and a
// remove of a tenant that is not a member each answer exactly what an accepted call answers
// (void), write the operator's row and no tenant row. NULL arguments are 22023 (decided by the
// arguments alone) and a dead session 28000; both write nothing.
func TestHoldingOps_WritesAnswerTheSameWhateverTheyFind(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a void "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	member := holdingTenant(t, ctx, tx, "Europe/Malta", "KF")
	if err := holdingAddAs(t, ctx, tx, hash, h, member); err != nil {
		t.Fatal(err)
	}
	outsider := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	utc := holdingTenant(t, ctx, tx, "UTC", nil)
	sameCode := holdingTenant(t, ctx, tx, "Europe/Malta", "KF")
	trailRows := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log`)
	members := opInt(t, ctx, tx, `SELECT count(*) FROM holding_members`)
	for _, c := range []struct {
		what            string
		add             bool
		holding, tenant any
	}{
		{"add to an unknown holding", true, uuid.New(), outsider},
		{"add an unknown tenant", true, h, uuid.New()},
		{"add a tenant of another zone", true, h, utc},
		{"add a tenant with a member's code", true, h, sameCode},
		{"add a member again", true, h, member},
		{"remove a tenant that is no member", false, h, outsider},
		{"remove from an unknown holding", false, uuid.New(), member},
	} {
		before := len(holdingOperatorRowsOf(t, ctx, tx, session))
		var err error
		if c.add {
			err = holdingAddAs(t, ctx, tx, hash, c.holding, c.tenant)
		} else {
			err = holdingRemoveAs(t, ctx, tx, hash, c.holding, c.tenant)
		}
		if err != nil {
			t.Errorf("%s: answered %v, want the same void as an accepted call", c.what, err)
		}
		if n := len(holdingOperatorRowsOf(t, ctx, tx, session)); n != before+1 {
			t.Errorf("%s: %d operator rows written, want 1", c.what, n-before)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log`); n != trailRows {
		t.Errorf("the refused calls wrote %d tenant rows", n-trailRows)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM holding_members`); n != members {
		t.Errorf("the refused calls wrote %d memberships", n-members)
	}
	before := opAudit(t, ctx, tx)
	for _, args := range [][2]any{{nil, outsider}, {h, nil}} {
		opWantClean(t, holdingAddAs(t, ctx, tx, hash, args[0], args[1]), sqlstateInvalidParameter, "op_add_holding_member: change refused", "add with a NULL argument")
		opWantClean(t, holdingRemoveAs(t, ctx, tx, hash, args[0], args[1]), sqlstateInvalidParameter, "op_remove_holding_member: change refused", "remove with a NULL argument")
	}
	for _, d := range opDeadSessions(t, ctx, tx) {
		opWant(t, holdingAddAs(t, ctx, tx, d.hash, h, outsider), sqlstateInvalidAuthorization, "add with a session that is "+d.name)
		opWant(t, holdingRemoveAs(t, ctx, tx, d.hash, h, member), sqlstateInvalidAuthorization, "remove with a session that is "+d.name)
	}
	if opAudit(t, ctx, tx) != before {
		t.Error("a refused argument or a dead session wrote an operator row")
	}
}

// TestHoldingOps_AddRefusesADifferentTimezone is K-36's add half: a tenant whose zone is not the
// holding's members' is not added -- whether the member is live or in its 60-second exit window
// (its tolerant rows are still worked out in the old zone) -- and a tenant of the same zone is.
// An empty holding takes any zone (there is nothing to compare with), and the first member's zone
// is then the holding's.
func TestHoldingOps_AddRefusesADifferentTimezone(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a zone "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	first := holdingTenant(t, ctx, tx, "Asia/Tokyo", nil)
	if err := holdingAddAs(t, ctx, tx, hash, h, first); err != nil || !holdingLiveIn(t, ctx, tx, h, first) {
		t.Fatalf("an empty holding refused its first member (%v)", err)
	}
	other := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	if err := holdingAddAs(t, ctx, tx, hash, h, other); err != nil || holdingLiveIn(t, ctx, tx, h, other) {
		t.Errorf("a tenant of another zone: %v, member=%v; want void and not added", err, holdingLiveIn(t, ctx, tx, h, other))
	}
	same := holdingTenant(t, ctx, tx, "Asia/Tokyo", nil)
	if err := holdingAddAs(t, ctx, tx, hash, h, same); err != nil || !holdingLiveIn(t, ctx, tx, h, same) {
		t.Errorf("CONTROL: a tenant of the holding's zone was not added (%v)", err)
	}
	// Within the exit window the leaver's zone still counts.
	h2, err := holdingCreateAs(t, ctx, tx, hash, "api1a zone window "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	leaver := holdingTenant(t, ctx, tx, "Asia/Tokyo", nil)
	holdingInsertMember(t, ctx, tx, h2, holdingMember{leaver, "5 minutes", "30 seconds"})
	if err := holdingAddAs(t, ctx, tx, hash, h2, other); err != nil || holdingLiveIn(t, ctx, tx, h2, other) {
		t.Errorf("a tenant of another zone than a member in its exit window: %v, member=%v; want void and not added", err, holdingLiveIn(t, ctx, tx, h2, other))
	}
}

// TestHoldingTimezone_OwnerCannotChangeAMembersTimezone is K-36's lock, through the owner's own
// panel statement (UpdateTenantAccount, as tappa_app in the tenant's context, committed): a
// LIVE member's zone change is refused with 23514 naming holding_timezone_lock, and so is the
// change of a tenant that left 30 seconds ago (the exit window); 61 seconds after leaving, and for
// a tenant that never was a member, the same statement changes the zone. A member's save that
// keeps its zone (a rename) goes through -- the trigger fires on a zone CHANGE only.
func TestHoldingTimezone_OwnerCannotChangeAMembersTimezone(t *testing.T) {
	f := holdingLiveFixture(t)
	d := appDB(t)
	h := f.holding(t)
	live := f.tenant(t, "Europe/Malta", nil)
	window := f.tenant(t, "Europe/Malta", nil)
	past := f.tenant(t, "Europe/Malta", nil)
	never := f.tenant(t, "Europe/Malta", nil)
	f.member(t, h, live, "10 minutes", "")
	f.member(t, h, window, "10 minutes", "30 seconds")
	f.member(t, h, past, "10 minutes", "61 seconds")
	save := func(tenant uuid.UUID, name, zone string) error {
		return d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, err := store.New(tx).UpdateTenantAccount(ctx, store.UpdateTenantAccountParams{
				TenantID: tenant, Name: name, BusinessType: "restaurant", Timezone: zone})
			return err
		})
	}
	for _, c := range []struct {
		what   string
		tenant uuid.UUID
		locked bool
	}{
		{"a live member", live, true},
		{"a member that left 30 seconds ago", window, true},
		{"a member that left 61 seconds ago", past, false},
		{"a tenant that never was a member", never, false},
	} {
		err := save(c.tenant, "api1a zone "+opToken(t), "Asia/Tokyo")
		code, constraint := m11Constraint(err)
		switch {
		case c.locked && (code != sqlstateCheckViolation || constraint != "holding_timezone_lock"):
			t.Errorf("%s: the zone change answered %q on %q (%v), want 23514 on holding_timezone_lock", c.what, code, constraint, err)
		case !c.locked && err != nil:
			t.Errorf("%s: the zone change was refused: %v", c.what, err)
		}
	}
	if err := save(live, "api1a renamed "+opToken(t), "Europe/Malta"); err != nil {
		t.Errorf("a member's save that keeps its zone was refused: %v", err)
	}
	if zone := f.zoneOf(t, live); zone != "Europe/Malta" {
		t.Errorf("the live member's zone is %s after the refusals", zone)
	}
}

// TestHoldingOps_RowLocksNeedNoNewPrivilegeAndFailClosed is ADR 0027 §1's "kilitler yeni bir
// yetki istemez ... o yetki bir gün kaldırılırsa kilit permission denied ile düşer": the add's
// FOR SHARE / FOR NO KEY UPDATE on tenants rows is met by tappa_opdefiner's existing UPDATE on
// four tenants columns (00034's two, 00039's two) -- the add works with them, and with the four
// revoked (in a savepoint) it is 42501, not an add without the lock.
func TestHoldingOps_RowLocksNeedNoNewPrivilegeAndFailClosed(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a locks "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	member := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	holdingInsertMember(t, ctx, tx, h, holdingMember{member, "1 minute", ""})
	if got := opColumns(t, ctx, tx, "tappa_opdefiner", "tenants", "UPDATE"); got != opDefinerTenantsUpd39 {
		t.Fatalf("PREMISE: tappa_opdefiner UPDATE on tenants = (%s), want (%s) -- 00042 adds none", got, opDefinerTenantsUpd39)
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `REVOKE UPDATE (vat_verified, vat_checked_at, api_enabled_at, api_code) ON tenants FROM tappa_opdefiner`); err != nil {
			t.Fatal(err)
		}
		joining := holdingTenant(t, ctx, sp, "Europe/Malta", nil)
		opWant(t, holdingAddAs(t, ctx, sp, hash, h, joining), sqlstateInsufficientPrivi, "an add whose row locks lost their privilege")
	})
	joining := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	if err := holdingAddAs(t, ctx, tx, hash, h, joining); err != nil || !holdingLiveIn(t, ctx, tx, h, joining) {
		t.Errorf("CONTROL: the add with today's grants: %v", err)
	}
}

// TestHoldingOps_OneMembershipAtATimeIncludingTheExitWindow: a live member of one holding is not
// added to another (nor to its own again); after its remove it is refused by both for 60 seconds
// (the exit window -- a tap that was under way may still land on either side, and two
// memberships would make it ambiguous); the partial UNIQUE index refuses a second live row
// whatever writes it.
func TestHoldingOps_OneMembershipAtATimeIncludingTheExitWindow(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	h1, err := holdingCreateAs(t, ctx, tx, hash, "api1a one "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	h2, err := holdingCreateAs(t, ctx, tx, hash, "api1a two "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	tenant := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	if err := holdingAddAs(t, ctx, tx, hash, h1, tenant); err != nil {
		t.Fatal(err)
	}
	count := func() int { return len(holdingMembershipsOf(t, ctx, tx, tenant)) }
	for _, h := range []uuid.UUID{h2, h1} {
		if err := holdingAddAs(t, ctx, tx, hash, h, tenant); err != nil || count() != 1 {
			t.Errorf("a live member added to %s: %v, %d memberships; want void and 1", h, err, count())
		}
	}
	if err := holdingRemoveAs(t, ctx, tx, hash, h1, tenant); err != nil {
		t.Fatal(err)
	}
	for _, h := range []uuid.UUID{h2, h1} {
		if err := holdingAddAs(t, ctx, tx, hash, h, tenant); err != nil || count() != 1 {
			t.Errorf("a tenant in its exit window added to %s: %v, %d memberships; want void and 1", h, err, count())
		}
	}
	opWant(t, opTry(t, ctx, tx, `INSERT INTO holding_members (holding_id, member_tenant_id) SELECT $1, $2 FROM generate_series(1, 2)`, h2, holdingTenant(t, ctx, tx, "UTC", nil)),
		"23505", "two live rows of one tenant written by the owner")
}

// TestHoldingOps_RejoinWaitsForTheToleranceWindow: with left_at 59 seconds back the tenant is
// still refused, with 61 seconds back it joins (a NEW row; the old one is history) -- and the
// copies of the 60 seconds agree: holding_peer's is_member answers true at 59 and false at 61,
// and every interval literal in holding_peer, op_add_holding_member and op_enable_tenant_api is
// '60 seconds' (ADR 0027 §1: "iki kopyanın eşitliği" -- the third copy is the code rule's, 00042).
func TestHoldingOps_RejoinWaitsForTheToleranceWindow(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a rejoin "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		ago    string
		joins  bool
		member bool
	}{{"59 seconds", false, true}, {"61 seconds", true, false}} {
		tenant := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
		holdingInsertMember(t, ctx, tx, h, holdingMember{tenant, "10 minutes", c.ago})
		if got, err := holdingPeerAs(t, ctx, tx, tenant, nil, "is_member"); err != nil || got == nil || *got != c.member {
			t.Errorf("left %s ago: is_member = %v (%v), want %v", c.ago, opDeref(got), err, c.member)
		}
		if err := holdingAddAs(t, ctx, tx, hash, h, tenant); err != nil {
			t.Fatal(err)
		}
		if got := holdingLiveIn(t, ctx, tx, h, tenant); got != c.joins {
			t.Errorf("left %s ago: rejoined=%v, want %v", c.ago, got, c.joins)
		}
		if c.joins && len(holdingMembershipsOf(t, ctx, tx, tenant)) != 2 {
			t.Errorf("a rejoin did not open a new row beside the old one")
		}
	}
	interval := regexp.MustCompile(`(?i)interval\s*'([^']*)'`)
	for _, fn := range []string{"public.holding_peer(uuid, uuid, text)", "public.op_add_holding_member(text, uuid, uuid)", "public.op_enable_tenant_api(text, uuid, text)"} {
		var src string
		if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = $1::regprocedure`, fn).Scan(&src); err != nil {
			t.Fatal(err)
		}
		found := interval.FindAllStringSubmatch(src, -1)
		if len(found) == 0 {
			t.Errorf("%s holds no interval literal -- its copy of the tolerance is gone", fn)
		}
		for _, m := range found {
			if m[1] != "60 seconds" {
				t.Errorf("%s holds interval '%s'; every copy of the tolerance is '60 seconds'", fn, m[1])
			}
		}
	}
}

// TestHoldingOps_LeftAtCannotBeUndone is ADR 0027 §1's "left_at geri alınamaz": a second remove
// leaves left_at where it was (no tenant row either); the only role that may UPDATE a membership
// column is tappa_opdefiner, and only left_at; and every UPDATE of holding_members in every body it
// owns sets left_at alone and carries `left_at IS NULL` -- so no op_* clears or moves an ended
// membership. (The migration role is a superuser and is outside this rule, as everywhere.)
func TestHoldingOps_LeftAtCannotBeUndone(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a final "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	tenant := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	if err := holdingAddAs(t, ctx, tx, hash, h, tenant); err != nil {
		t.Fatal(err)
	}
	if err := holdingRemoveAs(t, ctx, tx, hash, h, tenant); err != nil {
		t.Fatal(err)
	}
	left := *holdingMembershipsOf(t, ctx, tx, tenant)[0].left
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(0.05)`); err != nil {
		t.Fatal(err)
	}
	if err := holdingRemoveAs(t, ctx, tx, hash, h, tenant); err != nil {
		t.Fatal(err)
	}
	if got := holdingMembershipsOf(t, ctx, tx, tenant)[0].left; got == nil || !got.Equal(left) {
		t.Errorf("a second remove moved left_at from %v to %v", left, got)
	}
	for _, role := range []string{"tappa_app", "tappa_operator", "tappa_resolver", "tappa_opdefiner", "tappa_holdingdefiner"} {
		want := map[string]string{"tappa_opdefiner": "left_at", "tappa_holdingdefiner": "lock_anchor"}[role]
		if got := opColumns(t, ctx, tx, role, "holding_members", "UPDATE"); got != want {
			t.Errorf("%s UPDATE on holding_members = (%s), want (%s)", role, got, want)
		}
	}
	// The scan reads the body without comments and without double quotes, as its sibling
	// (TestHoldingOps_OnlyOperatorFunctionsWriteMembership) does -- `UPDATE public."holding_members"`
	// escaped it (the API-1a review's DÜŞÜK-5) -- and with the schema and ONLY optional. Every
	// UPDATE naming the table must be one the detailed pattern reads (`updates` == `named`).
	update := regexp.MustCompile(`(?is)\bupdate\s+(?:only\s+)?(?:public\s*\.\s*)?holding_members\b\s*(?:(?:as\s+)?\w+\s+)?set\s+(.*?)\s+where\s+(.*?);`)
	named := regexp.MustCompile(`(?i)\bupdate\s+(?:only\s+)?(?:public\s*\.\s*)?holding_members\b`)
	rows, err := tx.Query(ctx, `SELECT p.proname, p.prosrc FROM pg_proc p WHERE p.proowner = 'tappa_opdefiner'::regrole`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	updates, names := 0, 0
	for rows.Next() {
		var name, src string
		if err := rows.Scan(&name, &src); err != nil {
			t.Fatal(err)
		}
		body := strings.ReplaceAll(holdingStripComments(src), `"`, "")
		names += len(named.FindAllString(body, -1))
		for _, m := range update.FindAllStringSubmatch(body, -1) {
			updates++
			if !regexp.MustCompile(`^left_at\s*=\s*clock_timestamp\(\)$`).MatchString(strings.TrimSpace(m[1])) {
				t.Errorf("%s: an UPDATE of holding_members sets %q, want left_at = clock_timestamp() alone", name, m[1])
			}
			if !regexp.MustCompile(`(?i)(\w+\.)?left_at\s+is\s+null`).MatchString(m[2]) {
				t.Errorf("%s: an UPDATE of holding_members without `left_at IS NULL` in its WHERE (%q)", name, m[2])
			}
		}
	}
	if updates != 1 || names != updates {
		t.Errorf("%d UPDATE(s) of holding_members read in the definer's bodies out of %d naming it, want op_remove_holding_member's one",
			updates, names)
	}
}

// ------------------------------------------------------------- the API code rule --

// TestHoldingAPICode_AddRefusesAPeersCode (K-22 x ADR 0027, the API-1 review's hand-over): a
// tenant whose API code a member of the holding carries -- live or within its exit window -- is
// not added; a different code, or none, is; a member that left over 60 seconds ago no longer
// blocks its code; NULL codes never collide with each other.
func TestHoldingAPICode_AddRefusesAPeersCode(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a code "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	holdingInsertMember(t, ctx, tx, h, holdingMember{holdingTenant(t, ctx, tx, "Europe/Malta", "KF"), "10 minutes", ""})
	holdingInsertMember(t, ctx, tx, h, holdingMember{holdingTenant(t, ctx, tx, "Europe/Malta", "KW"), "10 minutes", "30 seconds"})
	holdingInsertMember(t, ctx, tx, h, holdingMember{holdingTenant(t, ctx, tx, "Europe/Malta", "KP"), "10 minutes", "61 seconds"})
	holdingInsertMember(t, ctx, tx, h, holdingMember{holdingTenant(t, ctx, tx, "Europe/Malta", nil), "10 minutes", ""})
	for _, c := range []struct {
		code  any
		joins bool
		why   string
	}{
		{"KF", false, "a live member's code"},
		{"KW", false, "the code of a member in its exit window"},
		{"KP", true, "the code of a member that left 61 seconds ago"},
		{"KM", true, "a code no member carries"},
		{nil, true, "no code, beside a member with none"},
	} {
		tenant := holdingTenant(t, ctx, tx, "Europe/Malta", c.code)
		if err := holdingAddAs(t, ctx, tx, hash, h, tenant); err != nil {
			t.Fatalf("%s: %v", c.why, err)
		}
		if got := holdingLiveIn(t, ctx, tx, h, tenant); got != c.joins {
			t.Errorf("%s: joined=%v, want %v", c.why, got, c.joins)
		}
	}
}

// TestHoldingAPICode_EnableRefusesAPeersCode is the reverse direction: op_enable_tenant_api
// (replaced by 00042) does not write, into a member, a code a peer of its holding carries (live
// or within its exit window) -- the answer is the same void, the operator's row is written, the
// tenant's row and the code are not; a code no peer carries is written; the same code is free
// outside the holding (codes are not unique across tenants -- 00038) and once the peer left over
// 60 seconds ago; a member may write its OWN code again; and a member in its own exit window is
// still bound by its old peers.
func TestHoldingAPICode_EnableRefusesAPeersCode(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	h, err := holdingCreateAs(t, ctx, tx, hash, "api1a enable "+opToken(t))
	if err != nil {
		t.Fatal(err)
	}
	kf := holdingTenant(t, ctx, tx, "Europe/Malta", "KF")
	km := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	window := holdingTenant(t, ctx, tx, "Europe/Malta", "KW")
	past := holdingTenant(t, ctx, tx, "Europe/Malta", "KP")
	outsider := holdingTenant(t, ctx, tx, "Europe/Malta", nil)
	holdingInsertMember(t, ctx, tx, h, holdingMember{kf, "10 minutes", ""})
	holdingInsertMember(t, ctx, tx, h, holdingMember{km, "10 minutes", ""})
	holdingInsertMember(t, ctx, tx, h, holdingMember{window, "10 minutes", "30 seconds"})
	holdingInsertMember(t, ctx, tx, h, holdingMember{past, "10 minutes", "61 seconds"})
	for _, c := range []struct {
		why          string
		tenant       uuid.UUID
		code, stored string
	}{
		{"a live peer's code", km, "KF", ""},
		{"the code of a peer in its exit window", km, "KW", ""},
		{"the code of a peer that left 61 seconds ago", km, "KP", "KP"},
		{"a code no peer carries", km, "KM", "KM"},
		{"a member writing its own code again", kf, "KF", "KF"},
		{"the code a peer carries, outside the holding", outsider, "KF", "KF"},
		{"a member in its exit window, a live peer's code", window, "KF", "KW"},
	} {
		beforeRows := opAPIOperatorRows(t, ctx, tx, "tenant_api_enabled", session, c.tenant)
		beforeTrail := len(opAPITrailOf(t, ctx, tx, c.tenant))
		if err := opEnableAPI(t, ctx, tx, hash, c.tenant, c.code); err != nil {
			t.Fatalf("%s: answered %v, want void", c.why, err)
		}
		s := opAPIOf(t, ctx, tx, c.tenant)
		got := ""
		if s.code != nil {
			got = *s.code
		}
		refused := c.stored == "" || (c.tenant == window && c.stored == "KW")
		switch {
		case refused && got != c.stored:
			t.Errorf("%s: the code is %q, want %q (refused)", c.why, got, c.stored)
		case !refused && got != c.code:
			t.Errorf("%s: the code is %q, want %q (written)", c.why, got, c.code)
		}
		if n := opAPIOperatorRows(t, ctx, tx, "tenant_api_enabled", session, c.tenant); n != beforeRows+1 {
			t.Errorf("%s: %d operator rows, want one more", c.why, n-beforeRows)
		}
		wantTrail := beforeTrail + 1
		if refused {
			wantTrail = beforeTrail
		}
		if n := len(opAPITrailOf(t, ctx, tx, c.tenant)); n != wantTrail {
			t.Errorf("%s: %d tenant rows, want %d", c.why, n, wantTrail)
		}
	}
}

// ---------------------------------------------------------- committed fixtures --

// holdingLive is a COMMITTED holding fixture over opLiveFixture (an operator and its session):
// tenants, holdings and memberships written by the owner and visible to every connection, and
// connections to race on. ITS CLEANUP removes the memberships and the holdings it made (the
// owner may: there is no guard on the two tables but their grants), and leaves the tenants (an
// op_* call writes audit_log rows naming them, append-only) and opLive's leftovers.
type holdingLive struct {
	*opLive
	ctx      context.Context
	holdings []uuid.UUID
	tenants  []uuid.UUID
	conns    []*pgx.Conn
}

func holdingLiveFixture(t *testing.T) *holdingLive {
	t.Helper()
	ctx, op := opLiveFixture(t)
	f := &holdingLive{opLive: op, ctx: ctx}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := f.owner.Exec(ctx, `DELETE FROM holding_members WHERE holding_id = ANY($1) OR member_tenant_id = ANY($2)`, f.holdings, f.tenants); err != nil {
			t.Errorf("cleanup: memberships: %v", err)
		}
		if _, err := f.owner.Exec(ctx, `DELETE FROM holdings h WHERE h.id = ANY($1)
		                                 AND NOT EXISTS (SELECT 1 FROM holding_members m WHERE m.holding_id = h.id)`, f.holdings); err != nil {
			t.Errorf("cleanup: holdings: %v", err)
		}
	})
	return f
}

func (f *holdingLive) holding(t *testing.T) uuid.UUID {
	t.Helper()
	id := holdingNew(t, f.ctx, f.owner, "api1a live "+opToken(t))
	f.holdings = append(f.holdings, id)
	return id
}

// adopt records a holding an op_* created, for the cleanup.
func (f *holdingLive) adopt(id uuid.UUID) { f.holdings = append(f.holdings, id) }

func (f *holdingLive) tenant(t *testing.T, zone string, code any) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := f.owner.Exec(f.ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone, api_code)
	                                   VALUES ($1, $2, $3, 'restaurant', 'multi', $4, $5)`,
		id, "api1a live "+opToken(t), "VAT-HOLDING-"+id.String(), zone, code); err != nil {
		t.Fatalf("commit a tenant: %v", err)
	}
	f.tenants = append(f.tenants, id)
	return id
}

func (f *holdingLive) member(t *testing.T, holding, tenant uuid.UUID, joinedAgo, leftAgo string) {
	t.Helper()
	holdingInsertMember(t, f.ctx, f.owner, holding, holdingMember{tenant, joinedAgo, leftAgo})
}

func (f *holdingLive) zoneOf(t *testing.T, tenant uuid.UUID) string {
	t.Helper()
	var zone string
	if err := f.owner.QueryRow(f.ctx, `SELECT timezone FROM tenants WHERE id = $1`, tenant).Scan(&zone); err != nil {
		t.Fatal(err)
	}
	return zone
}

func (f *holdingLive) codeOf(t *testing.T, tenant uuid.UUID) *string {
	t.Helper()
	var code *string
	if err := f.owner.QueryRow(f.ctx, `SELECT api_code FROM tenants WHERE id = $1`, tenant).Scan(&code); err != nil {
		t.Fatal(err)
	}
	return code
}

// conn opens one more owner connection for a race, recorded so a wait can be seen on it.
func (f *holdingLive) conn(t *testing.T) *pgx.Conn {
	t.Helper()
	c := f.connect(t, f.ctx)
	f.conns = append(f.conns, c)
	return c
}

// holdingBegin begins a READ COMMITTED transaction on c (the isolation the operator pool and
// the customer pool run in) and, when role is set, becomes it for the transaction.
func holdingBegin(t *testing.T, ctx context.Context, c *pgx.Conn, role string) pgx.Tx {
	t.Helper()
	tx, err := c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Logf("rollback: %v", err)
		}
	})
	if role != "" {
		if _, err := tx.Exec(ctx, "SET LOCAL SESSION AUTHORIZATION "+pgx.Identifier{role}.Sanitize()); err != nil {
			t.Fatalf("become %s: %v", role, err)
		}
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '20s'`); err != nil {
		t.Fatal(err)
	}
	return tx
}

// holdingWaitForWaiter waits until n of f's race connections wait on a lock (pg_locks, not
// granted) -- the measured moment a statement is blocked behind another transaction. When none
// comes to wait within 5 s it reports the error and returns, so the race's OUTCOME is still
// checked (a mutant without the lock is red on what it lets through, not only on the missing
// wait).
func holdingWaitForWaiter(t *testing.T, f *holdingLive, n int) {
	t.Helper()
	pids := make([]uint32, 0, len(f.conns))
	for _, c := range f.conns {
		pids = append(pids, c.PgConn().PID())
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := opInt(t, f.ctx, f.owner, `SELECT count(DISTINCT pid) FROM pg_locks WHERE NOT granted AND pid = ANY($1)`, pids); got >= int64(n) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("no %d race connection(s) came to wait on a lock within 5 s -- the lock that orders this race is not taken", n)
}

// holdingCall runs one op_* as tappa_operator in a transaction of its own on c (autocommit),
// returning the error.
func holdingCall(ctx context.Context, c *pgx.Conn, sql string, args ...any) error {
	tx, err := c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const (
	holdingAddSQL    = `SELECT public.op_add_holding_member($1, $2, $3)`
	holdingRemoveSQL = `SELECT public.op_remove_holding_member($1, $2, $3)`
	holdingEnableSQL = `SELECT public.op_enable_tenant_api($1, $2, $3)`
)

// ------------------------------------------------------------------- the races --

// TestHoldingOps_ConcurrentAddsForOneTenantOneWins: eight concurrent adds of ONE tenant -- four
// to each of two empty holdings, each on its own connection, released together -- end in exactly
// one live membership, one 'tenant.holding_joined' row and eight operator rows, and every call
// answers void (no unique-index error reaches a caller: the tenant's row lock FOR NO KEY UPDATE
// queues them, and each re-reads the memberships after it waits). -race.
func TestHoldingOps_ConcurrentAddsForOneTenantOneWins(t *testing.T) {
	f := holdingLiveFixture(t)
	h1, h2 := f.holding(t), f.holding(t)
	tenant := f.tenant(t, "Europe/Malta", nil)
	const n = 8
	conns := make([]*pgx.Conn, n)
	for i := range conns {
		conns[i] = f.conn(t)
	}
	// One operator session per caller: op_touch_session writes the session's row, so calls of ONE
	// session queue on it and could never race (measured: with one session the mutants below stay
	// green).
	hashes := make([]string, n)
	for i := range hashes {
		hashes[i], _ = f.newSession(t, f.ctx)
	}
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			h := h1
			if i%2 == 1 {
				h = h2
			}
			errs[i] = holdingCall(f.ctx, conns[i], holdingAddSQL, hashes[i], h, tenant)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("add %d answered %v, want void", i, err)
		}
	}
	live := opInt(t, f.ctx, f.owner, `SELECT count(*) FROM holding_members WHERE member_tenant_id = $1 AND left_at IS NULL`, tenant)
	joins := opInt(t, f.ctx, f.owner, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'tenant.holding_joined'`, tenant)
	ops := opInt(t, f.ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE session_id = ANY($1) AND kind = 'holding_member_added' AND target_tenant_id = $2`, f.sessions, tenant)
	if live != 1 || joins != 1 || ops != n {
		t.Errorf("after %d concurrent adds: %d live memberships, %d joined rows, %d operator rows; want 1, 1, %d", n, live, joins, ops, n)
	}
}

// TestHoldingOps_ConcurrentAddsOfTwoTenantsKeepOneTimezone (ADR 0027 §1's round-2 finding): two
// tenants of DIFFERENT zones added at once to an EMPTY holding -- neither sees a member to compare
// with unless the holding is serialised -- end with exactly one member, round after round (twenty
// rounds, a new holding each). The mutant (the advisory lock removed) gives a two-zone holding --
// measured on a throwaway database (the API-1a report). -race.
func TestHoldingOps_ConcurrentAddsOfTwoTenantsKeepOneTimezone(t *testing.T) {
	f := holdingLiveFixture(t)
	c1, c2 := f.conn(t), f.conn(t)
	hash1, _ := f.newSession(t, f.ctx)
	hash2, _ := f.newSession(t, f.ctx)
	for round := 0; round < 20; round++ {
		h := f.holding(t)
		malta := f.tenant(t, "Europe/Malta", nil)
		tokyo := f.tenant(t, "Asia/Tokyo", nil)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var e1, e2 error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; e1 = holdingCall(f.ctx, c1, holdingAddSQL, hash1, h, malta) }()
		go func() { defer wg.Done(); <-start; e2 = holdingCall(f.ctx, c2, holdingAddSQL, hash2, h, tokyo) }()
		close(start)
		wg.Wait()
		if e1 != nil || e2 != nil {
			t.Fatalf("round %d: %v / %v", round, e1, e2)
		}
		var members, zones int64
		if err := f.owner.QueryRow(f.ctx, `SELECT count(*), count(DISTINCT t.timezone) FROM holding_members m JOIN tenants t ON t.id = m.member_tenant_id
		                                    WHERE m.holding_id = $1 AND m.left_at IS NULL`, h).Scan(&members, &zones); err != nil {
			t.Fatal(err)
		}
		if members != 1 || zones != 1 {
			t.Fatalf("round %d: %d members in %d zones, want one member", round, members, zones)
		}
	}
}

// holdingWaitOn waits until connection c waits on a lock (pg_locks, not granted) and reports
// whether it came to wait within 5 s and on which lock types. When it does not come to wait the
// caller reports it and goes on, so the race's OUTCOME is checked too.
func holdingWaitOn(t *testing.T, f *holdingLive, c *pgx.Conn) (bool, string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var types *string
		if err := f.owner.QueryRow(f.ctx, `SELECT string_agg(DISTINCT locktype, ',') FROM pg_locks WHERE NOT granted AND pid = $1`,
			c.PgConn().PID()).Scan(&types); err != nil {
			t.Fatal(err)
		}
		if types != nil {
			return true, *types
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false, ""
}

// TestHoldingOps_RemoveWaitsForAnUncommittedAdd (the API-1a review's ORTA-2): X is a live member;
// Y's add is called and NOT yet committed when X's remove is called. The remove queues on the
// holding's advisory lock (measured on pg_locks: the remover's connection waits, on an advisory
// lock), re-reads the members after the add commits, and writes 'tenant.holding_member_left'
// naming X into Y's trail too -- every member live when X leaves is told. The mutant
// (op_remove_holding_member without its advisory lock) does not wait, reads the members before
// Y's add commits, and Y never gets the row -- measured on a throwaway database (waited=false,
// Y's row missing; the original: waited on advisory, Y's row there). Two operator sessions: calls
// of ONE session queue on op_touch_session's row whatever the holding's lock does.
func TestHoldingOps_RemoveWaitsForAnUncommittedAdd(t *testing.T) {
	f := holdingLiveFixture(t)
	h := f.holding(t)
	x := f.tenant(t, "Europe/Malta", nil)
	f.member(t, h, x, "10 minutes", "")
	y := f.tenant(t, "Europe/Malta", nil)
	second, _ := f.newSession(t, f.ctx)
	c1, c2 := f.conn(t), f.conn(t)
	add := holdingBegin(t, f.ctx, c1, "tappa_operator")
	if _, err := add.Exec(f.ctx, holdingAddSQL, f.hash, h, y); err != nil {
		t.Fatalf("Y's add: %v", err)
	}
	errs := make(chan error, 1)
	go func() { errs <- holdingCall(f.ctx, c2, holdingRemoveSQL, second, h, x) }()
	waited, types := holdingWaitOn(t, f, c2)
	if !waited || types != "advisory" {
		t.Errorf("X's remove waited=%v on {%s} while Y's add was uncommitted, want a wait on the holding's advisory lock", waited, types)
	}
	if err := add.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatalf("X's remove answered %v, want void", err)
	}
	if holdingLiveIn(t, f.ctx, f.owner, h, x) || !holdingLiveIn(t, f.ctx, f.owner, h, y) {
		t.Errorf("X live=%v, Y live=%v; want X removed and Y a member", holdingLiveIn(t, f.ctx, f.owner, h, x), holdingLiveIn(t, f.ctx, f.owner, h, y))
	}
	told := 0
	for _, r := range holdingTrailOf(t, f.ctx, f.owner, y) {
		if r.action == "tenant.holding_member_left" && r.detail["member_tenant_id"] == x.String() && r.detail["holding_id"] == h.String() {
			told++
		}
	}
	if told != 1 {
		t.Errorf("Y's trail has %d 'tenant.holding_member_left' row(s) naming X, want 1 -- a member live when X left was not told", told)
	}
	left := 0
	for _, r := range holdingTrailOf(t, f.ctx, f.owner, x) {
		if r.action == "tenant.holding_left" {
			left++
		}
	}
	if left != 1 {
		t.Errorf("X's trail has %d 'tenant.holding_left' row(s), want 1", left)
	}
}

// TestHoldingOps_TimezoneComparisonRacesTheOwnersUpdate (ADR 0027 §1's round-1 finding): an add
// and the joining tenant's own zone change, in both orders, never leave a member whose zone is not
// the holding's.
//   - ADD FIRST: the add (uncommitted) holds the tenant's row FOR NO KEY UPDATE; the owner's
//     UPDATE waits (measured on pg_locks); the add commits; the UPDATE's K-36 trigger sees the
//     committed membership and refuses (23514 holding_timezone_lock). The tenant is a member, in
//     the holding's zone.
//   - UPDATE FIRST: the owner's change (uncommitted) holds the row; the add waits; the UPDATE
//     commits; the add's READ COMMITTED re-read sees the new zone and does not add (void). The
//     tenant is not a member.
//
// The mutant (op_add_holding_member without its tenants row locks) ends both orders with a member
// in another zone -- measured on a throwaway database (the API-1a report): a foreign key's KEY
// SHARE does not conflict with the owner's NO KEY UPDATE, so neither side waits.
func TestHoldingOps_TimezoneComparisonRacesTheOwnersUpdate(t *testing.T) {
	f := holdingLiveFixture(t)
	ownerUpdate := func(tx pgx.Tx, tenant uuid.UUID) error {
		if _, err := tx.Exec(f.ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
			return err
		}
		_, err := tx.Exec(f.ctx, `UPDATE tenants SET timezone = 'Asia/Tokyo' WHERE id = $1`, tenant)
		return err
	}
	// ADD FIRST.
	{
		h := f.holding(t)
		f.member(t, h, f.tenant(t, "Europe/Malta", nil), "10 minutes", "")
		joining := f.tenant(t, "Europe/Malta", nil)
		c1, c2 := f.conn(t), f.conn(t)
		add := holdingBegin(t, f.ctx, c1, "tappa_operator")
		if _, err := add.Exec(f.ctx, holdingAddSQL, f.hash, h, joining); err != nil {
			t.Fatalf("add first: %v", err)
		}
		upd := holdingBegin(t, f.ctx, c2, "tappa_app")
		errs := make(chan error, 1)
		go func() { errs <- ownerUpdate(upd, joining) }()
		holdingWaitForWaiter(t, f, 1)
		if err := add.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		err := <-errs
		if code, constraint := m11Constraint(err); code != sqlstateCheckViolation || constraint != "holding_timezone_lock" {
			t.Errorf("add first: the owner's UPDATE answered %v, want 23514 on holding_timezone_lock", err)
		}
		_ = upd.Rollback(f.ctx)
		if !holdingLiveIn(t, f.ctx, f.owner, h, joining) || f.zoneOf(t, joining) != "Europe/Malta" {
			t.Errorf("add first: member=%v zone=%s, want a member in Europe/Malta", holdingLiveIn(t, f.ctx, f.owner, h, joining), f.zoneOf(t, joining))
		}
	}
	// UPDATE FIRST.
	{
		h := f.holding(t)
		f.member(t, h, f.tenant(t, "Europe/Malta", nil), "10 minutes", "")
		joining := f.tenant(t, "Europe/Malta", nil)
		c1, c2 := f.conn(t), f.conn(t)
		upd := holdingBegin(t, f.ctx, c2, "tappa_app")
		if err := ownerUpdate(upd, joining); err != nil {
			t.Fatalf("update first: %v", err)
		}
		errs := make(chan error, 1)
		go func() { errs <- holdingCall(f.ctx, c1, holdingAddSQL, f.hash, h, joining) }()
		holdingWaitForWaiter(t, f, 1)
		if err := upd.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-errs; err != nil {
			t.Errorf("update first: the add answered %v, want void", err)
		}
		if holdingLiveIn(t, f.ctx, f.owner, h, joining) {
			t.Errorf("update first: the tenant joined in %s, the holding's members are in Europe/Malta", f.zoneOf(t, joining))
		}
	}
}

// TestHoldingAPICode_EnableAndAddRaceUnderOneLock: the code rule holds in both directions under
// concurrency.
//   - ADD, THEN A PEER'S CODE ON THE JOINING TENANT: the add (uncommitted) holds the tenant's row;
//     op_enable_tenant_api for that tenant with a member's code waits on it, re-reads after the add
//     commits, finds the tenant in a holding it did not lock for, and writes nothing.
//   - THE CODE, THEN THE ADD: the enable (uncommitted) holds the row; the add waits, re-reads the
//     tenant's new code, finds a member carrying it, and does not add.
//   - TWO PEERS, ONE CODE: two members' enables with the same code queue on the holding's advisory
//     lock; the second sees the first's code and writes nothing.
//
// In every order at most one tenant of the holding carries the code.
func TestHoldingAPICode_EnableAndAddRaceUnderOneLock(t *testing.T) {
	f := holdingLiveFixture(t)
	c1, c2 := f.conn(t), f.conn(t)
	// The second caller is another operator session: op_touch_session writes the session's row,
	// so two calls of ONE session would queue on it whatever the holding's locks do.
	second, _ := f.newSession(t, f.ctx)
	{
		h := f.holding(t)
		f.member(t, h, f.tenant(t, "Europe/Malta", "KF"), "10 minutes", "")
		joining := f.tenant(t, "Europe/Malta", nil)
		add := holdingBegin(t, f.ctx, c1, "tappa_operator")
		if _, err := add.Exec(f.ctx, holdingAddSQL, f.hash, h, joining); err != nil {
			t.Fatal(err)
		}
		errs := make(chan error, 1)
		go func() { errs <- holdingCall(f.ctx, c2, holdingEnableSQL, second, joining, "KF") }()
		holdingWaitForWaiter(t, f, 1)
		if err := add.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-errs; err != nil {
			t.Errorf("add, then the code: the enable answered %v, want void", err)
		}
		if code := f.codeOf(t, joining); !holdingLiveIn(t, f.ctx, f.owner, h, joining) || code != nil {
			t.Errorf("add, then the code: member=%v code=%v; want a member without the peer's code", holdingLiveIn(t, f.ctx, f.owner, h, joining), opDeref(code))
		}
	}
	{
		h := f.holding(t)
		f.member(t, h, f.tenant(t, "Europe/Malta", "KF"), "10 minutes", "")
		joining := f.tenant(t, "Europe/Malta", nil)
		enable := holdingBegin(t, f.ctx, c1, "tappa_operator")
		if _, err := enable.Exec(f.ctx, holdingEnableSQL, f.hash, joining, "KF"); err != nil {
			t.Fatal(err)
		}
		errs := make(chan error, 1)
		go func() { errs <- holdingCall(f.ctx, c2, holdingAddSQL, second, h, joining) }()
		holdingWaitForWaiter(t, f, 1)
		if err := enable.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-errs; err != nil {
			t.Errorf("the code, then the add: the add answered %v, want void", err)
		}
		if holdingLiveIn(t, f.ctx, f.owner, h, joining) {
			t.Error("the code, then the add: a tenant carrying a member's code joined")
		}
	}
	{
		h := f.holding(t)
		p1, p2 := f.tenant(t, "Europe/Malta", nil), f.tenant(t, "Europe/Malta", nil)
		f.member(t, h, p1, "10 minutes", "")
		f.member(t, h, p2, "10 minutes", "")
		first := holdingBegin(t, f.ctx, c1, "tappa_operator")
		if _, err := first.Exec(f.ctx, holdingEnableSQL, f.hash, p1, "KZ"); err != nil {
			t.Fatal(err)
		}
		errs := make(chan error, 1)
		go func() { errs <- holdingCall(f.ctx, c2, holdingEnableSQL, second, p2, "KZ") }()
		holdingWaitForWaiter(t, f, 1)
		if err := first.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-errs; err != nil {
			t.Errorf("two peers: the second enable answered %v, want void", err)
		}
		if a, b := f.codeOf(t, p1), f.codeOf(t, p2); opDeref(a) != "KZ" || b != nil {
			t.Errorf("two peers: codes %s and %s, want KZ and none", opDeref(a), opDeref(b))
		}
	}
}
