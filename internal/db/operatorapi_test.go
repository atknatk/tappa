package db

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// operatorapi_test.go -- migrations 00038 and 00039 (M11 API-1, K-22): the tenant's integration
// API gate and its one writer, op_enable_tenant_api / op_disable_tenant_api (ADR 0028 §2, ADR
// 0021's op_* contract and its M11 note). Everything runs inside opTx's rolled-back transaction
// as the owner, becoming tappa_operator / tappa_app / tappa_opdefiner with opAs where the role
// is the point.

const (
	op00039File = "00039_open_the_api_from_the_operator.sql"

	enableAPIRefusal  = "op_enable_tenant_api: change refused"
	disableAPIRefusal = "op_disable_tenant_api: change refused"

	// tappa_opdefiner's lists on tenants at 00039 (HEAD), in attnum order: 00034's plus the API
	// gate's columns -- SELECT api_enabled_at alone (the enable keeps the first opening
	// instant), UPDATE both.
	opDefinerTenantsSel39 = opDefinerTenantsSel34 + ",api_enabled_at"
	opDefinerTenantsUpd39 = opDefinerTenantsUpd34 + ",api_enabled_at,api_code"
)

// op00039Functions are the two writes 00039 creates, by their exact catalogue identity: the
// session first, no actor, void (ADR 0021 §2 i, v 7).
var op00039Functions = map[string]struct{ args, result string }{
	"op_enable_tenant_api":  {"p_session text, p_tenant_id uuid, p_api_code text", "void"},
	"op_disable_tenant_api": {"p_session text, p_tenant_id uuid", "void"},
}

// op00039ListEdits are the edits 00039 makes to 00034's bodies, in order. Undoing them must give
// 00034's body back byte for byte.
var op00039ListEdits = map[string][][2]string{
	"op_begin_read": {
		{"'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked') THEN\n",
			"'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked',\n" +
				"                                                   'tenant_api_enabled', 'tenant_api_disabled') THEN\n"},
	},
	"op_read_audit": {
		{"'operator_disabled', 'tenant_vat_checked')\n",
			"'operator_disabled', 'tenant_vat_checked',\n" +
				"                                                            'tenant_api_enabled', 'tenant_api_disabled')\n"},
	},
}

// opAPIOf is the tenant's two gate columns as the owner reads them.
type opAPIState struct {
	enabledAt *time.Time
	code      *string
}

func opAPIOf(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) opAPIState {
	t.Helper()
	var s opAPIState
	if err := q.QueryRow(ctx, `SELECT api_enabled_at, api_code FROM tenants WHERE id = $1`, tenant).
		Scan(&s.enabledAt, &s.code); err != nil {
		t.Fatalf("read the API gate of %s: %v", tenant, err)
	}
	return s
}

// opAPITrail is one tenant audit_log row the gate wrote.
type opAPITrail struct {
	action string
	actor  *uuid.UUID
	target *string
	detail map[string]any
	at     time.Time
}

func opAPITrailOf(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) []opAPITrail {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT action, actor_id, target, detail, at FROM audit_log
	                            WHERE tenant_id = $1 AND action LIKE 'tenant.api_%' ORDER BY at, id`, tenant)
	if err != nil {
		t.Fatalf("read the tenant trail: %v", err)
	}
	defer rows.Close()
	var out []opAPITrail
	for rows.Next() {
		var r opAPITrail
		var raw []byte
		if err := rows.Scan(&r.action, &r.actor, &r.target, &raw, &r.at); err != nil {
			t.Fatalf("scan the tenant trail: %v", err)
		}
		if err := json.Unmarshal(raw, &r.detail); err != nil {
			t.Fatalf("the tenant trail's detail is not an object: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the tenant trail: %v", err)
	}
	return out
}

// opAPIOperatorRows counts the operator's rows of one kind, for one session and one tenant,
// whose shape is the tenant-write shape (no account, scope, page; detail {}).
func opAPIOperatorRows(t *testing.T, ctx context.Context, q opQuerier, kind string, session, tenant uuid.UUID) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM operator_audit_log
	                          WHERE kind = $1 AND session_id = $2 AND target_tenant_id = $3
	                            AND target_admin_id IS NULL AND target_scope IS NULL
	                            AND page_number IS NULL AND page_size IS NULL AND detail = '{}'::jsonb`,
		kind, session, tenant)
}

// opEnableAPI / opDisableAPI run the shipped writes as tappa_operator inside a savepoint of tx,
// the database's error untouched; on success the writes stay in tx.
func opEnableAPI(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenant, code any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", `SELECT op_enable_tenant_api($1, $2, $3)`, hash, tenant, code)
}

func opDisableAPI(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenant any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", `SELECT op_disable_tenant_api($1, $2)`, hash, tenant)
}

// TestOperator00039_TheFunctionsAndTheirExactSignatures: the two writes' identities, owner,
// SECURITY DEFINER, proconfig, one overload each, EXECUTE for tappa_operator alone (not PUBLIC,
// tappa_app, tappa_resolver); the two replaced reads are 00034's bodies plus exactly 00039's list
// edits and are the live, newest definitions; HEAD's kind CHECK and tenant-write shape name the
// two kinds; and ADR 0021 §6's forward and frozen-clock scans walk the new functions and find
// nothing.
func TestOperator00039_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	for name, want := range op00039Functions {
		if got := opIdentity(t, ctx, tx, name); got != want.args+" -> "+want.result {
			t.Errorf("%s is %s, want (%s) -> %s", name, got, want.args, want.result)
		}
		var owner string
		var secdef, retset bool
		var config []string
		if err := tx.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner), p.prosecdef, p.proretset, p.proconfig
		                             FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                            WHERE n.nspname = 'public' AND p.proname = $1`, name).Scan(&owner, &secdef, &retset, &config); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if owner != "tappa_opdefiner" || !secdef || retset || len(config) != 1 || config[0] != "search_path=pg_catalog, pg_temp" {
			t.Errorf("%s: owner %s, SECURITY DEFINER %v, set-returning %v, proconfig %v", name, owner, secdef, retset, config)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, name); n != 1 {
			t.Errorf("%d functions named %s; an overload would be a second door", n, name)
		}
		for who, may := range map[string]bool{"public": false, "tappa_app": false, "tappa_resolver": false, "tappa_operator": true} {
			var got bool
			if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, p.oid, 'EXECUTE') FROM pg_proc p
			                             JOIN pg_namespace n ON n.oid = p.pronamespace
			                            WHERE n.nspname = 'public' AND p.proname = $2`, who, name).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != may {
				t.Errorf("has_function_privilege(%s, %s, EXECUTE) = %v, want %v", who, name, got, may)
			}
		}
	}

	up, _ := opMigrationSections(t, op00039File)
	up34, _ := opMigrationSections(t, op00034File)
	for fn, edits := range op00039ListEdits {
		b39 := opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", fn)
		undone := b39
		for i := len(edits) - 1; i >= 0; i-- {
			if strings.Count(undone, edits[i][1]) != 1 {
				t.Errorf("00039's %s does not hold edit %d exactly once", fn, i+1)
				continue
			}
			undone = strings.Replace(undone, edits[i][1], edits[i][0], 1)
		}
		if undone != opLogFunctionBody(t, up34, "CREATE OR REPLACE FUNCTION", fn) {
			t.Errorf("00039's %s is not 00034's body with its list edits", fn)
		}
		var live string
		if err := tx.QueryRow(ctx, `SELECT p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, fn).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != b39 || live != opNewestUpBody(t, fn) {
			t.Errorf("the live %s is not 00039's body (or 00039 is not its newest definition)", fn)
		}
	}

	for _, c := range []struct{ name, want string }{
		{"operator_audit_log_kind_check", opKindCheckDef(opAuditKinds)},
		{opTenantWriteShape, opTenantWriteShapeDefOf(opTenantWriteKinds)},
	} {
		if def, valid := opConstraint(t, ctx, tx, "operator_audit_log", c.name); def != c.want || !valid {
			t.Errorf("%s is %s (validated %v),\n want %s, validated", c.name, def, valid, c.want)
		}
	}

	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for fn := range op00039Functions {
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
}

// TestOpEnableTenantAPI_OpensTheGateAndWritesBothTrails: an accepted enable writes the two
// columns, the tenant's own row (action 'tenant.api_enabled', the operator as actor, the tenant
// as target, detail exactly {actor_kind, api_code}, `at` the instant written into
// api_enabled_at) and the operator's row (the tenant-write shape). A second enable with another
// code KEEPS the first opening instant, replaces the code and writes both rows again.
func TestOpEnableTenantAPI_OpensTheGateAndWritesBothTrails(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := opNewTenant(t, ctx, tx, "m11 api enable "+opToken(t), 0)

	if s := opAPIOf(t, ctx, tx, tenant); s.enabledAt != nil || s.code != nil {
		t.Fatalf("PREMISE: a new tenant's gate is (%v, %v), want closed and uncoded", s.enabledAt, s.code)
	}
	if err := opEnableAPI(t, ctx, tx, hash, tenant, "KF"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	first := opAPIOf(t, ctx, tx, tenant)
	if first.enabledAt == nil || first.code == nil || *first.code != "KF" {
		t.Fatalf("after the enable the gate is (%v, %v), want open with code KF", first.enabledAt, first.code)
	}
	trail := opAPITrailOf(t, ctx, tx, tenant)
	if len(trail) != 1 {
		t.Fatalf("%d tenant rows after one enable, want 1", len(trail))
	}
	r := trail[0]
	if r.action != "tenant.api_enabled" || r.actor == nil || *r.actor != a.id || r.target == nil || *r.target != tenant.String() {
		t.Errorf("the tenant row is %q by %v on %v, want tenant.api_enabled by the operator on the tenant", r.action, r.actor, r.target)
	}
	if len(r.detail) != 2 || r.detail["actor_kind"] != "operator" || r.detail["api_code"] != "KF" {
		t.Errorf("the tenant row's detail is %v, want exactly {actor_kind: operator, api_code: KF}", r.detail)
	}
	if !r.at.Equal(*first.enabledAt) {
		t.Errorf("the tenant row's at %v is not the instant written into api_enabled_at %v (one clock read)", r.at, *first.enabledAt)
	}
	if n := opAPIOperatorRows(t, ctx, tx, "tenant_api_enabled", session, tenant); n != 1 {
		t.Errorf("%d operator rows of the tenant-write shape after one enable, want 1", n)
	}

	if err := opEnableAPI(t, ctx, tx, hash, tenant, "KM2"); err != nil {
		t.Fatalf("second enable: %v", err)
	}
	second := opAPIOf(t, ctx, tx, tenant)
	if second.enabledAt == nil || !second.enabledAt.Equal(*first.enabledAt) || second.code == nil || *second.code != "KM2" {
		t.Errorf("after a second enable the gate is (%v, %v), want the FIRST opening instant %v and code KM2",
			second.enabledAt, second.code, *first.enabledAt)
	}
	if trail := opAPITrailOf(t, ctx, tx, tenant); len(trail) != 2 || trail[1].detail["api_code"] != "KM2" {
		t.Errorf("after a second enable the tenant trail is %v, want two rows, the second naming KM2", trail)
	}
	if n := opAPIOperatorRows(t, ctx, tx, "tenant_api_enabled", session, tenant); n != 2 {
		t.Errorf("%d operator rows after two enables, want 2", n)
	}
}

// TestOpDisableTenantAPI_ClosesTheGateAndKeepsTheCode: the disable clears api_enabled_at and
// KEEPS api_code (punches still carry it as their company), writes 'tenant.api_disabled' with
// detail exactly {actor_kind} and the operator's 'tenant_api_disabled' row; disabling a closed
// gate writes both rows again (the row says the act was applied). Re-enabling after a disable
// opens at a NEW instant.
func TestOpDisableTenantAPI_ClosesTheGateAndKeepsTheCode(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := opNewTenant(t, ctx, tx, "m11 api disable "+opToken(t), 0)
	if err := opEnableAPI(t, ctx, tx, hash, tenant, "KF"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	opened := opAPIOf(t, ctx, tx, tenant)

	for i := 1; i <= 2; i++ {
		if err := opDisableAPI(t, ctx, tx, hash, tenant); err != nil {
			t.Fatalf("disable %d: %v", i, err)
		}
		s := opAPIOf(t, ctx, tx, tenant)
		if s.enabledAt != nil || s.code == nil || *s.code != "KF" {
			t.Errorf("after disable %d the gate is (%v, %v), want closed and still coded KF", i, s.enabledAt, s.code)
		}
		if n := opAPIOperatorRows(t, ctx, tx, "tenant_api_disabled", session, tenant); n != int64(i) {
			t.Errorf("%d operator rows after %d disable(s)", n, i)
		}
	}
	trail := opAPITrailOf(t, ctx, tx, tenant)
	if len(trail) != 3 {
		t.Fatalf("%d tenant rows, want the enable and the two disables", len(trail))
	}
	for _, r := range trail[1:] {
		if r.action != "tenant.api_disabled" || len(r.detail) != 1 || r.detail["actor_kind"] != "operator" ||
			r.actor == nil || *r.actor != a.id {
			t.Errorf("a disable's tenant row is %q %v by %v, want tenant.api_disabled {actor_kind: operator} by the operator", r.action, r.detail, r.actor)
		}
	}
	if err := opEnableAPI(t, ctx, tx, hash, tenant, "KF"); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if s := opAPIOf(t, ctx, tx, tenant); s.enabledAt == nil || !s.enabledAt.After(*opened.enabledAt) {
		t.Errorf("a re-enable after a disable opened at %v, want a new instant after %v", s.enabledAt, *opened.enabledAt)
	}
}

// TestOpTenantAPI_RefusesBadArgumentsAndDeadSessionsWritingNothing: a NULL tenant, a NULL code
// and every code outside 00038's shape are 22023 with one constant message; ADR 0021 §6's six
// dead sessions are 28000; in every case NOTHING is written -- no column, no tenant row, no
// operator row.
func TestOpTenantAPI_RefusesBadArgumentsAndDeadSessionsWritingNothing(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	tenant := opNewTenant(t, ctx, tx, "m11 api refuse "+opToken(t), 0)
	before := opAudit(t, ctx, tx)
	nothing := func(what string) {
		t.Helper()
		if s := opAPIOf(t, ctx, tx, tenant); s.enabledAt != nil || s.code != nil {
			t.Errorf("%s: the gate moved to (%v, %v)", what, s.enabledAt, s.code)
		}
		if n := len(opAPITrailOf(t, ctx, tx, tenant)); n != 0 {
			t.Errorf("%s: %d tenant rows written", what, n)
		}
		if n := opAudit(t, ctx, tx); n != before {
			t.Errorf("%s: %d operator rows written", what, n-before)
		}
	}
	for _, c := range []struct {
		what         string
		tenant, code any
	}{
		{"a NULL tenant", nil, "KF"},
		{"a NULL code", tenant, nil},
		{"an empty code", tenant, ""},
		{"one letter", tenant, "K"},
		{"lower case", tenant, "kf"},
		{"a leading digit", tenant, "1KF"},
		{"seventeen characters", tenant, "KABCDEFGHIJKLMNOP"},
		{"a hyphen", tenant, "K-F"},
		{"a space", tenant, "K F"},
	} {
		opWantClean(t, opEnableAPI(t, ctx, tx, hash, c.tenant, c.code), sqlstateInvalidParameter, enableAPIRefusal, "enable with "+c.what)
		nothing("enable with " + c.what)
	}
	opWantClean(t, opDisableAPI(t, ctx, tx, hash, nil), sqlstateInvalidParameter, disableAPIRefusal, "disable with a NULL tenant")
	nothing("disable with a NULL tenant")

	// CONTROL: the boundary of the shape is accepted -- two and sixteen characters.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"KM", "K234567890ABCDEF"} {
		if err := opEnableAPI(t, ctx, sp, hash, tenant, code); err != nil {
			t.Errorf("CONTROL: the code %q (%d characters) is refused: %v", code, len(code), err)
		}
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	for _, d := range opDeadSessions(t, ctx, tx) {
		before = opAudit(t, ctx, tx)
		opWant(t, opEnableAPI(t, ctx, tx, d.hash, tenant, "KF"), sqlstateInvalidAuthorization, "enable with a session that is "+d.name)
		opWant(t, opDisableAPI(t, ctx, tx, d.hash, tenant), sqlstateInvalidAuthorization, "disable with a session that is "+d.name)
		nothing("a session that is " + d.name)
	}
}

// TestOpTenantAPI_AnUnknownTenantIsTheSameVoid: B14 -- for an id that names no tenant both writes
// answer exactly what they answer for a real one (void), write the operator's row and nothing
// else: no tenant row (no foreign key fires) and no tenant changed.
func TestOpTenantAPI_AnUnknownTenantIsTheSameVoid(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	ghost := uuid.New()
	changedBefore := opInt(t, ctx, tx, `SELECT count(*) FROM tenants WHERE api_enabled_at IS NOT NULL OR api_code IS NOT NULL`)
	if err := opEnableAPI(t, ctx, tx, hash, ghost, "KF"); err != nil {
		t.Errorf("enable for a tenant that does not exist answered %v, want the same void", err)
	}
	if err := opDisableAPI(t, ctx, tx, hash, ghost); err != nil {
		t.Errorf("disable for a tenant that does not exist answered %v, want the same void", err)
	}
	if n := opAPIOperatorRows(t, ctx, tx, "tenant_api_enabled", session, ghost) + opAPIOperatorRows(t, ctx, tx, "tenant_api_disabled", session, ghost); n != 2 {
		t.Errorf("%d operator rows for the unknown tenant, want the enable's and the disable's", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, ghost); n != 0 {
		t.Errorf("%d tenant rows for a tenant that does not exist", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tenants WHERE api_enabled_at IS NOT NULL OR api_code IS NOT NULL`); n != changedBefore {
		t.Errorf("a call for an unknown tenant changed %d tenant(s)", n-changedBefore)
	}
}

// TestOpTenantAPI_ACallForOneTenantTouchesNoOther: ADR 0021 §6's belt -- no RLS runs inside the
// definer, so `t.id = p_tenant_id` is the only barrier: enabling and disabling A leaves B's
// gate and B's trail exactly as they were.
func TestOpTenantAPI_ACallForOneTenantTouchesNoOther(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	ta := opNewTenant(t, ctx, tx, "m11 api belt a "+opToken(t), 0)
	tb := opNewTenant(t, ctx, tx, "m11 api belt b "+opToken(t), 0)
	if err := opEnableAPI(t, ctx, tx, hash, tb, "KM"); err != nil {
		t.Fatalf("enable B: %v", err)
	}
	bBefore, bTrail := opAPIOf(t, ctx, tx, tb), len(opAPITrailOf(t, ctx, tx, tb))
	if err := opEnableAPI(t, ctx, tx, hash, ta, "KF"); err != nil {
		t.Fatalf("enable A: %v", err)
	}
	if err := opDisableAPI(t, ctx, tx, hash, ta); err != nil {
		t.Fatalf("disable A: %v", err)
	}
	bAfter := opAPIOf(t, ctx, tx, tb)
	if bAfter.enabledAt == nil || !bAfter.enabledAt.Equal(*bBefore.enabledAt) || bAfter.code == nil || *bAfter.code != "KM" {
		t.Errorf("B's gate moved from (%v, %v) to (%v, %v) on calls for A", bBefore.enabledAt, bBefore.code, bAfter.enabledAt, bAfter.code)
	}
	if n := len(opAPITrailOf(t, ctx, tx, tb)); n != bTrail {
		t.Errorf("B's trail grew from %d to %d rows on calls for A", bTrail, n)
	}
	if n := len(opAPITrailOf(t, ctx, tx, ta)); n != 2 {
		t.Errorf("A's trail has %d rows, want the enable's and the disable's", n)
	}
}

// TestOpTenantAPI_TheTenantRowsTimeIsTheWallClock: ADR 0021 §2 vii, by behaviour (a prosrc scan
// cannot see which value an INSERT puts into `at`): a write made after its transaction has been
// open for over a second stamps api_enabled_at and the tenant row's `at` with the WALL clock, not
// the transaction's start (00005's DEFAULT now() would be).
func TestOpTenantAPI_TheTenantRowsTimeIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	tenant := opNewTenant(t, ctx, tx, "m11 api clock "+opToken(t), 0)
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.2)`); err != nil {
		t.Fatal(err)
	}
	if err := opEnableAPI(t, ctx, tx, hash, tenant, "KF"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	var start time.Time
	if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&start); err != nil {
		t.Fatal(err)
	}
	s := opAPIOf(t, ctx, tx, tenant)
	trail := opAPITrailOf(t, ctx, tx, tenant)
	if s.enabledAt == nil || len(trail) != 1 {
		t.Fatalf("PREMISE: gate %v, %d tenant rows", s.enabledAt, len(trail))
	}
	if s.enabledAt.Sub(start) < time.Second || trail[0].at.Sub(start) < time.Second {
		t.Errorf("api_enabled_at %v and the row's at %v are within a second of the transaction's start %v; a frozen clock wrote them",
			*s.enabledAt, trail[0].at, start)
	}
}

// TestTenantApiColumns_AppCannotWrite: ADR 0028 §1's `tenants` row and API-1's acceptance --
// tappa_app reads the two gate columns and can neither UPDATE nor INSERT them, by privilege
// (has_column_privilege, which counts role membership) and by behaviour (42501 inside its own
// tenant, where RLS would admit the row); tappa_operator cannot write them either and can only
// call the op_*; tappa_app cannot call the op_* at all.
func TestTenantApiColumns_AppCannotWrite(t *testing.T) {
	ctx, tx := opTx(t)
	for _, role := range []string{"tappa_app", "tappa_operator"} {
		for _, col := range []string{"api_enabled_at", "api_code"} {
			for _, priv := range []string{"UPDATE", "INSERT"} {
				var may bool
				if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1, 'public.tenants', $2, $3)`, role, col, priv).Scan(&may); err != nil {
					t.Fatal(err)
				}
				if may {
					t.Errorf("has_column_privilege(%s, tenants.%s, %s) = true", role, col, priv)
				}
			}
		}
	}
	var reads bool
	if err := tx.QueryRow(ctx, `SELECT has_column_privilege('tappa_app', 'public.tenants', 'api_enabled_at', 'SELECT')
	                                AND has_column_privilege('tappa_app', 'public.tenants', 'api_code', 'SELECT')`).Scan(&reads); err != nil {
		t.Fatal(err)
	}
	if !reads {
		t.Error("tappa_app cannot read the gate; the API's resolver path and the panel need to")
	}

	tenant := opNewTenant(t, ctx, tx, "m11 api app "+opToken(t), 0)
	asApp := func(sql string, args ...any) error {
		return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
				return err
			}
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
	}
	// CONTROL: in its own context tappa_app may write a column 00024 granted -- so the refusals
	// below are the column's, not RLS's.
	if err := asApp(`UPDATE tenants SET name = name WHERE id = $1`, tenant); err != nil {
		t.Fatalf("CONTROL: tappa_app cannot UPDATE its own tenant's name: %v", err)
	}
	opWant(t, asApp(`UPDATE tenants SET api_enabled_at = clock_timestamp(), api_code = 'KF' WHERE id = $1`, tenant),
		sqlstateInsufficientPrivi, "tappa_app opening its own API")
	opWant(t, asApp(`UPDATE tenants SET api_code = 'KF' WHERE id = $1`, tenant),
		sqlstateInsufficientPrivi, "tappa_app writing its own API code")
	newID := uuid.New()
	opWant(t, opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, newID.String()); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, api_enabled_at, api_code)
		                        VALUES ($1, 'signup', $2, 'bar', 'single', clock_timestamp(), 'KF')`, newID, "VAT-M11-"+newID.String())
		return err
	}), sqlstateInsufficientPrivi, "tappa_app signing up with an open API")
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `UPDATE public.tenants SET api_code = 'KF' WHERE id = $1`, tenant),
		sqlstateInsufficientPrivi, "tappa_operator writing the code directly")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `SELECT op_enable_tenant_api($1, $2, 'KF')`, opRandHex(t), tenant),
		sqlstateInsufficientPrivi, "tappa_app calling op_enable_tenant_api")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `SELECT op_disable_tenant_api($1, $2)`, opRandHex(t), tenant),
		sqlstateInsufficientPrivi, "tappa_app calling op_disable_tenant_api")
	if s := opAPIOf(t, ctx, tx, tenant); s.enabledAt != nil || s.code != nil {
		t.Errorf("after the refusals the gate is (%v, %v)", s.enabledAt, s.code)
	}
	// And the schema's own two CHECKs, as the owner: an open gate needs a code; a code has its shape.
	opWant(t, opTry(t, ctx, tx, `UPDATE tenants SET api_enabled_at = clock_timestamp() WHERE id = $1`, tenant),
		sqlstateCheckViolation, "an open gate without a code")
	opWant(t, opTry(t, ctx, tx, `UPDATE tenants SET api_code = 'kf' WHERE id = $1`, tenant),
		sqlstateCheckViolation, "a lower-case code")
}

// TestOperator00039_DownGivesBack00034AndUpTakesItAgain: the Down gives op_begin_read and
// op_read_audit 00034's Up bodies verbatim, drops the two writes, takes back exactly the two
// column grants and restores 00034's kind CHECK and tenant-write shape -- VALIDATED with nothing
// outside 00034's set, NOT VALID with a write's rows present (and the rows, the trail and the
// columns' values STAY); the Up takes 00039's state again, over those rows too.
func TestOperator00039_DownGivesBack00034AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00039File)
	up34, _ := opMigrationSections(t, op00034File)
	begin34, audit34 := opLogFunctionBody(t, up34, "CREATE OR REPLACE FUNCTION", "op_begin_read"), opLogFunctionBody(t, up34, "CREATE OR REPLACE FUNCTION", "op_read_audit")
	begin39, audit39 := opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_begin_read"), opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_read_audit")
	if opLogFunctionBody(t, down, "CREATE OR REPLACE FUNCTION", "op_begin_read") != begin34 ||
		opLogFunctionBody(t, down, "CREATE OR REPLACE FUNCTION", "op_read_audit") != audit34 {
		t.Error("00039's Down does not give the two reads 00034's Up bodies verbatim")
	}
	downStatements := opStatementsOnly(down)
	if regexp.MustCompile(`(?i)\bREVOKE\s+ALL\s+ON\s+(?:TABLE\s+)?tenants\b`).MatchString(downStatements) ||
		regexp.MustCompile(`(?im)^GRANT\s+(?:SELECT|INSERT|UPDATE|DELETE)\b`).MatchString(downStatements) {
		t.Error("00039's Down holds a REVOKE ALL on tenants or a table GRANT; it takes back the Up's column grants by name and gives nothing")
	}
	if n := len(regexp.MustCompile(`(?m)^REVOKE (?:SELECT|UPDATE) \(`).FindAllString(downStatements, -1)); n != 2 {
		t.Errorf("00039's Down holds %d column REVOKEs, want the two the Up granted", n)
	}

	type state struct {
		fns                 int64
		begin, audit        string
		kinds, shape        string
		kindsOK, shapeOK    bool
		tenantsSel, updates string
	}
	read := func(q pgx.Tx) state {
		t.Helper()
		var s state
		s.fns = opInt(t, ctx, q, `SELECT count(*) FROM pg_proc WHERE proname IN ('op_enable_tenant_api', 'op_disable_tenant_api')`)
		for fn, dst := range map[string]*string{"op_begin_read": &s.begin, "op_read_audit": &s.audit} {
			if err := q.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE proname = $1`, fn).Scan(dst); err != nil {
				t.Fatal(err)
			}
		}
		s.kinds, s.kindsOK = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_kind_check")
		s.shape, s.shapeOK = opConstraint(t, ctx, q, "operator_audit_log", opTenantWriteShape)
		s.tenantsSel = opColumns(t, ctx, q, "tappa_opdefiner", "tenants", "SELECT")
		s.updates = opColumns(t, ctx, q, "tappa_opdefiner", "tenants", "UPDATE")
		return s
	}
	at39 := func(s state, when string) {
		t.Helper()
		if s.fns != 2 || s.begin != begin39 || s.audit != audit39 || s.kinds != opKindCheckDef(opAuditKinds) || !s.kindsOK ||
			s.shape != opTenantWriteShapeDefOf(opTenantWriteKinds) || !s.shapeOK ||
			s.tenantsSel != opDefinerTenantsSel39 || s.updates != opDefinerTenantsUpd39 {
			t.Errorf("%s: not 00039's state: %+v", when, s)
		}
	}
	at34 := func(s state, when string, kindsNV bool) {
		t.Helper()
		kinds := opKindCheckDef(opAuditKinds34)
		if kindsNV {
			kinds += " NOT VALID"
		}
		if s.fns != 0 || s.begin != begin34 || s.audit != audit34 || s.kinds != kinds || s.kindsOK == kindsNV ||
			s.shape != opTenantWriteShapeDef || !s.shapeOK ||
			s.tenantsSel != opDefinerTenantsSel34 || s.updates != opDefinerTenantsUpd34 {
			t.Errorf("%s: not 00034's state (kinds NOT VALID=%v): %+v", when, kindsNV, s)
		}
	}
	at39(read(tx), "PREMISE at HEAD")

	// Branch 1: no row of this file's kinds -> VALIDATED; Up again.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n := opInt(t, ctx, sp, `SELECT count(*) FROM operator_audit_log WHERE kind = ANY ($1)`, opAuditKindsAddedBy[39]); n > 0 {
		opMustHoldTheTablesLockExclusive(t, ctx, sp)
		for _, s := range []string{
			`ALTER TABLE operator_audit_log DISABLE TRIGGER operator_audit_log_append_only`,
			`DELETE FROM operator_audit_log WHERE kind = ANY ($1)`,
			`ALTER TABLE operator_audit_log ENABLE TRIGGER operator_audit_log_append_only`,
		} {
			var args []any
			if strings.Contains(s, "$1") {
				args = []any{opAuditKindsAddedBy[39]}
			}
			if _, err := sp.Exec(ctx, s, args...); err != nil {
				t.Fatalf("clear this file's kinds inside the transaction: %v", err)
			}
		}
	}
	opRunSection(t, ctx, sp, down, "00039 Down")
	at34(read(sp), "after Down", false)
	opRunSection(t, ctx, sp, up, "00039 Up again")
	at39(read(sp), "after Up again")
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Branch 2: a write's rows present -> the kind CHECK NOT VALID, the rows and the values stay.
	sp, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a := opNewActive(t, ctx, sp)
	hash, session := opNewSession(t, ctx, sp, a.id, true)
	tenant := opNewTenant(t, ctx, sp, "m11 api down "+opToken(t), 0)
	if err := opEnableAPI(t, ctx, sp, hash, tenant, "KF"); err != nil {
		t.Fatalf("a write before Down: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00039 Down with a write's rows present")
	at34(read(sp), "after Down (a write's rows)", true)
	if s := opAPIOf(t, ctx, sp, tenant); s.enabledAt == nil || s.code == nil ||
		len(opAPITrailOf(t, ctx, sp, tenant)) != 1 || opAPIOperatorRows(t, ctx, sp, "tenant_api_enabled", session, tenant) != 1 {
		t.Error("after Down the gate's value, its tenant row or its operator row is gone; a Down takes a door away, not a fact")
	}
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) VALUES ('tenant_api_enabled', $1, $2, $3)`,
		session, a.id, tenant), sqlstateCheckViolation, "a NEW 'tenant_api_enabled' row after Down (NOT VALID still binds new rows)")
	opRunSection(t, ctx, sp, up, "00039 Up again over the write's rows")
	at39(read(sp), "after Up again (a write's rows)")
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestOperator00039_PreconditionRefusesAWrongCluster: 00039's first statement passes this
// cluster taken to 00038 (00039's Down) and refuses, with 55000 naming 00039, a role of the wrong
// shape, a database not at 00034's operator surface -- in particular a kind CHECK or a tenant-write
// shape ANOTHER migration widened in between (OP-15's case: this file must stop, not undo it) --,
// 00038's columns missing, and a drifted ACL its column grants cannot narrow.
func TestOperator00039_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 38, opKindsAt34...)
	up, _ := opMigrationSections(t, op00039File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00039's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	widened := `ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
	            ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN ('` +
		strings.Join(opAuditKinds34, "', '") + `', 'tenant_suspended')) NOT VALID`
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"at 00038", nil, ""},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
		{"op_record_vat_check missing", []string{`ALTER FUNCTION public.op_record_vat_check(text, uuid, text, boolean) RENAME TO zz_m11_was_vat`}, "that state is not there"},
		{"op_begin_read not the definer's", []string{`ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_owner`}, "that state is not there"},
		{"00038's api_code missing", []string{`ALTER TABLE tenants DROP CONSTRAINT tenants_api_needs_a_code`, `ALTER TABLE tenants DROP COLUMN api_code`}, "that state is not there"},
		{"the kind CHECK widened by another migration", []string{widened}, "rebase this file onto it"},
		{"the tenant-write shape widened by another migration", []string{
			`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_tenant_write_shape`,
			`ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_tenant_write_shape CHECK (kind <> ALL (ARRAY['tenant_vat_checked', 'tenant_suspended']) OR detail = '{}'::jsonb)`},
			"rebase this file onto it"},
		{"the definer already reaches the code", []string{`GRANT UPDATE (api_code) ON tenants TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"the definer holds UPDATE on all of tenants", []string{`GRANT UPDATE ON tenants TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"tappa_app may open its own API", []string{`GRANT UPDATE (api_enabled_at) ON tenants TO tappa_app`}, "a privilege no migration gave"},
		{"tappa_app may write the code through a role it is a member of",
			[]string{`GRANT UPDATE (api_code) ON tenants TO tappa_resolver`, `GRANT tappa_resolver TO tappa_app`}, "a privilege no migration gave"},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		for _, s := range c.setup {
			if _, err := sp.Conn().PgConn().Exec(ctx, s).ReadAll(); err != nil {
				t.Fatalf("%s: setup %q: %v", c.what, s, err)
			}
		}
		_, runErr := sp.Conn().PgConn().Exec(ctx, pre).ReadAll()
		code, msg := opCode(runErr)
		switch {
		case c.want == "" && runErr != nil:
			t.Errorf("%s: the precondition refuses a correct cluster: %v", c.what, runErr)
		case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00039")):
			t.Errorf("%s: precondition answered %q %q, want %s naming 00039 and containing %q", c.what, code, msg, sqlstatePrerequisiteState, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("%s: rollback: %v", c.what, err)
		}
	}
}

// TestOpAtVersion_SkipsOnlyMigrationsWithoutAnOperatorObject holds opAtVersionSkips to its
// premise: every skipped migration exists, and neither half of it names an operator object --
// an op_* function, an operator_* or platform_* table, or one of the two operator roles. A
// migration that did would change the operator surface at an older version and must be walked.
func TestOpAtVersion_SkipsOnlyMigrationsWithoutAnOperatorObject(t *testing.T) {
	operatorObject := regexp.MustCompile(`(?i)\bop_[a-z_]+\s*\(|\boperator_[a-z_]+|\bplatform_[a-z_]+|\btappa_opdefiner\b|\btappa_operator\b`)
	entries, err := os.ReadDir(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for n, why := range opAtVersionSkips {
		var name string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), opMigrationPrefix(n)) {
				name = e.Name()
			}
		}
		if name == "" {
			t.Errorf("opAtVersionSkips names %d (%s), which is no migration", n, why)
			continue
		}
		up, down := opMigrationSections(t, name)
		if m := operatorObject.FindString(opStatementsOnly(up) + opStatementsOnly(down)); m != "" {
			t.Errorf("%s is skipped by opAtVersion but its statements name %q, an operator object", name, m)
		}
	}
}

// opMigrationPrefix is a migration number as its file name starts.
func opMigrationPrefix(n int) string {
	s := strconv.Itoa(n)
	return strings.Repeat("0", 5-len(s)) + s + "_"
}
