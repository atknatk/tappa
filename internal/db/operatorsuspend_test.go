package db

// operatorsuspend_test.go -- migration 00063 (M10 OP-15, phase A; written 2026-10-08 as 00035 onto
// 00034, ported 2026-10-20 onto 00062): the operator SUSPENDS a tenant
// through op_suspend_tenant and lifts the suspension through op_reinstate_tenant -- two one-phase
// writes, RETURNS void, each changing the tenant's two suspension columns, writing a row in the
// tenant's own audit_log (K6) and a row in the operator's log in one call. The catalogue half
// (signatures, the replaced and re-created functions, the CHECKs, the grants -- tappa_app reads
// the suspension and cannot write it --, the reason set's copies, the Down and its refusal, the
// precondition and its drift check, the temp-table shadow) and the behaviour half (the argument
// refusal, the exact rows, the first time kept, state independence and B14, the wall clock taken
// AFTER the row lock, one transaction, the belt, the counted statistics and width channels) of
// ADR 0025 "Güvenlik iddiası" PART I A and ADR 0021 §6, for the two new op_*.
//
// HOW EACH TEST TOUCHES THE SHARED DATABASE -- operatorvat_test.go's shapes:
//   - every test runs in opTx: one rolled-back REPEATABLE READ transaction as the owner,
//     identities switched with SET LOCAL SESSION AUTHORIZATION, the operator-tables lock taken
//     EXCLUSIVE. Every tenant a test suspends is written inside that transaction and goes with it.
//     🔴 NO TEST HERE COMMITS A TENANT, A SUSPENSION, A TENANT AUDIT ROW OR AN OPERATOR ROW: every
//     call of either write runs inside a transaction that is rolled back;
//   - the three tests whose subject is ANOTHER session (the row lock lets the tenant's writes
//     through, a lock wait, the clock read after the lock) need a tenant that other session can
//     see: the database's OLDEST committed tenant, read into memory. The suspension written to it
//     lives in a savepoint of this test's transaction and is rolled back at once (the
//     orchestrator's rule: no real tenant is ever suspended -- a rolled-back transaction is not a
//     suspension); the other session only locks the row or writes rows it rolls back;
//   - the reads (the overview's and the list's new columns) are reached WITHOUT a commit through
//     tickets the owner forges with created_xact naming a transaction that really committed
//     (opCommittedXact), as every op_read_* test does.
//
// THE LOCKS (ADR 0025 limit 9): 00063's Down drops two columns of tenants, so a test that runs it
// -- the Down test and the precondition test here, and every earlier migration's test through
// opAtVersion (which takes the lock first: downTables names tenants) -- holds ACCESS EXCLUSIVE on
// tenants until its rollback. TestOperator00063_DownGivesBack00062AndUpTakesItAgain measures it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/importer"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// op00063Functions are the two writes 00063 creates, by their exact catalogue identity: the
// session, the tenant and (for the suspension) the reason -- no actor -- returning void (ADR 0021
// §2 v 7).
var op00063Functions = map[string]struct{ args, result string }{
	"op_suspend_tenant":   {"p_session text, p_tenant_id uuid, p_reason text", "void"},
	"op_reinstate_tenant": {"p_session text, p_tenant_id uuid", "void"},
}

// op00063Reads are the two reads 00063 re-creates, by HEAD's identity: 00029's arguments, the
// earlier columns (00029's list; 00029's overview and 00057's API state) and the suspension at
// the END (Go scans by position).
var op00063Reads = map[string]struct{ args, result string }{
	"op_read_tenants":       {"p_session text, p_ticket text, p_query text, p_page_number integer, p_page_size integer", opReadTenantsResult63},
	"op_read_tenant_detail": {"p_session text, p_ticket text, p_tenant_id uuid", opReadDetailResult63},
}

const (
	op00063File = "00063_suspend_a_tenant_from_the_operator.sql"

	suspendRefusal   = "op_suspend_tenant: suspension refused"
	reinstateRefusal = "op_reinstate_tenant: reinstatement refused"

	// The tenant audit_log actions of 00063's two rows.
	opSuspendAction   = "tenant.suspended"
	opReinstateAction = "tenant.reinstated"

	// tenants' two CHECKs, as pg_get_constraintdef writes them. (HEAD's tenant-write shape is
	// opTenantWriteShapeDefOf(opTenantWriteKinds): 00063 widened 00039's three kinds by two.)
	opSuspensionPair       = "tenants_suspension_both_or_neither"
	opSuspensionPairDef    = `CHECK (((suspended_at IS NULL) = (suspension_reason IS NULL)))`
	opSuspensionReasonName = "tenants_suspension_reason_check"
	opSuspensionReasonDef  = `CHECK ((suspension_reason = ANY (ARRAY['non_payment'::text, 'terms_breach'::text, 'security_hold'::text, 'customer_request'::text])))`

	// tappa_opdefiner's lists on tenants at HEAD, in attnum order (00063's two columns last, after
	// 00039's and 00042's API columns).
	opDefinerTenantsSel63 = opDefinerTenantsSel42 + ",suspended_at,suspension_reason"
	opDefinerTenantsUpd63 = opDefinerTenantsUpd39 + ",suspended_at,suspension_reason"

	// tappa_app's lists on tenants, which 00063 does not touch: 00024's UPDATE, 00016/00017's INSERT.
	opAppTenantsUpd = "name,business_type,timezone"
	opAppTenantsIns = "id,name,vat_number,business_type,structure,timezone,vat_verified,vat_checked_at"

	// The two reads' results: the list at 00029 (and after 00063's Down) and at HEAD; the overview
	// at 00029, at 00057 (and after 00063's Down) and at HEAD.
	opReadTenantsResult29 = "TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text)"
	opReadTenantsResult63 = "TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text, " +
		"suspended_at timestamp with time zone)"
	opReadDetailResult29 = "TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text, business_type text, " +
		"location_count bigint, active_employee_count bigint, active_plaque_count bigint, active_admin_count bigint)"
	opReadDetailResult57 = "TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text, business_type text, " +
		"location_count bigint, active_employee_count bigint, active_plaque_count bigint, active_admin_count bigint, " +
		"api_enabled_at timestamp with time zone, api_code text)"
	opReadDetailResult63 = "TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text, business_type text, " +
		"location_count bigint, active_employee_count bigint, active_plaque_count bigint, active_admin_count bigint, " +
		"api_enabled_at timestamp with time zone, api_code text, suspended_at timestamp with time zone, suspension_reason text)"
)

// opSuspensionReasons is the closed reason set in the CHECK's order -- the list the copies are
// held to (TestOperator00063_TheReasonSetIsOneSetInEveryCopy against the database,
// TestSuspensionReasons_TheTypedConstantsAreTheList against the Go constants).
var opSuspensionReasons = []string{"non_payment", "terms_breach", "security_hold", "customer_request"}

// op00063Edits are the edits 00063 makes to the bodies it replaces (00042's op_begin_read and
// op_read_audit) and re-creates (00029's op_read_tenants, 00057's op_read_tenant_detail) --
// compose-time text, in order, each with the migration whose body it edits. Undoing them must give
// that body back byte for byte.
var op00063Edits = map[string]struct {
	create, earlierFile, earlierCreate string
	edits                              [][2]string
}{
	"op_begin_read": {"CREATE OR REPLACE FUNCTION", op00042File, "CREATE OR REPLACE FUNCTION", [][2]string{
		{"                                                   'holding_member_removed') THEN\n",
			"                                                   'holding_member_removed', 'tenant_suspended',\n" +
				"                                                   'tenant_reinstated') THEN\n"},
	}},
	"op_read_audit": {"CREATE OR REPLACE FUNCTION", op00042File, "CREATE OR REPLACE FUNCTION", [][2]string{
		{"                                                            'holding_member_removed')\n",
			"                                                            'holding_member_removed', 'tenant_suspended',\n" +
				"                                                            'tenant_reinstated')\n"},
	}},
	"op_read_tenants": {"CREATE FUNCTION", op00029File, "CREATE FUNCTION", [][2]string{
		{"        SELECT t.id, t.name, t.created_at, t.plan\n", "        SELECT t.id, t.name, t.created_at, t.plan, t.suspended_at\n"},
	}},
	"op_read_tenant_detail": {"CREATE FUNCTION", op00057File, "CREATE FUNCTION", [][2]string{
		{"               t.api_enabled_at, t.api_code\n          FROM public.tenants AS t\n",
			"               t.api_enabled_at, t.api_code, t.suspended_at, t.suspension_reason\n          FROM public.tenants AS t\n"},
	}},
}

// ------------------------------------------------------------------ helpers --

// opSuspFixture is one tenant the owner wrote inside the test's transaction.
type opSuspFixture struct {
	id     uuid.UUID
	name   string
	at     *time.Time
	reason string
}

// opSuspStates are the states a fixture starts in. The times are WRITTEN, not read from a clock.
var opSuspStates = []struct{ name, at, reason string }{
	{"not suspended", "", ""},
	{"suspended for non-payment", "2025-04-05 06:07:08.123456+00", "non_payment"},
	{"suspended on a security hold", "2025-07-08 09:10:11.654321+00", "security_hold"},
}

// opSuspTenant writes a tenant as the owner, suspended at `at` for `reason` ("" and "" for a
// tenant that is not suspended).
func opSuspTenant(t *testing.T, ctx context.Context, q opQuerier, name, at, reason string) opSuspFixture {
	t.Helper()
	f := opSuspFixture{id: uuid.New(), name: name, reason: reason}
	if err := q.QueryRow(ctx, `
		INSERT INTO public.tenants (id, name, vat_number, business_type, structure, plan, suspended_at, suspension_reason)
		VALUES ($1, $2, $3, 'cafe', 'single', 'standard', NULLIF($4, '')::timestamptz, NULLIF($5, ''))
		RETURNING suspended_at`, f.id, name, "VAT-OP15-"+f.id.String(), at, reason).Scan(&f.at); err != nil {
		t.Fatalf("insert the suspension fixture tenant: %v", err)
	}
	return f
}

// opSuspend and opReinstate run the SHIPPED statements as tappa_operator inside a savepoint of
// tx, the database's error untouched; on success the savepoint is released, so the writes stay.
func opSuspend(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenant, reason any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", suspendTenantSQL, hash, tenant, reason)
}

func opReinstate(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenant any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", reinstateTenantSQL, hash, tenant)
}

// opReadDetailHead and opReadTenantsHead run the two SHIPPED read statements (readTenantDetailSQL,
// readTenantsSQL -- HEAD's columns, the suspension last) as tappa_operator inside a savepoint of tx,
// with scans written HERE, independent of readTenantDetail's and readTenants'. operatortenants_test.go's
// opReadDetail and opReadTenants read 00029's named columns, which every version from 00029 on has
// (a test that walks below 00063 reads with them); the suspension is read through these.
func opReadDetailHead(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket string, tenant uuid.UUID) ([]TenantOverview, error) {
	t.Helper()
	var out []TenantOverview
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, readTenantDetailSQL, hash, ticket, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o TenantOverview
			var reason *string
			if err := rows.Scan(&o.ID, &o.Name, &o.CreatedAt, &o.Plan, &o.BusinessType, &o.Locations, &o.ActiveEmployees,
				&o.ActivePlaques, &o.ActiveAdmins, &o.APIEnabledAt, &o.APICode, &o.SuspendedAt, &reason); err != nil {
				return err
			}
			if reason != nil {
				o.SuspensionReason = SuspensionReason(*reason)
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

func opReadTenantsHead(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket, query string, number, size int) ([]TenantSummary, error) {
	t.Helper()
	var out []TenantSummary
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, readTenantsSQL, hash, ticket, query, number, size)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s TenantSummary
			if err := rows.Scan(&s.ID, &s.Name, &s.CreatedAt, &s.Plan, &s.SuspendedAt); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	return out, err
}

// opSuspendAccessor and opReinstateAccessor run the PRODUCT's accessors as tappa_operator inside a
// savepoint of tx.
func opSuspendAccessor(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenant uuid.UUID, reason SuspensionReason) error {
	t.Helper()
	return opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error { return SuspendTenant(ctx, sp, hash, tenant, reason) })
}

func opReinstateAccessor(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenant uuid.UUID) error {
	t.Helper()
	return opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error { return ReinstateTenant(ctx, sp, hash, tenant) })
}

// opSuspState is a tenant's suspension as the owner reads it, with the row's physical version
// (ctid and xmin): a call that "changed nothing" must not have rewritten the row either.
type opSuspState struct {
	at      *time.Time
	reason  *string
	version string
}

func opSuspOf(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) opSuspState {
	t.Helper()
	var s opSuspState
	if err := q.QueryRow(ctx, `SELECT suspended_at, suspension_reason, ctid::text || '/' || xmin::text
	                             FROM public.tenants WHERE id = $1`, tenant).Scan(&s.at, &s.reason, &s.version); err != nil {
		t.Fatalf("read a tenant's suspension: %v", err)
	}
	return s
}

func (s opSuspState) same(o opSuspState) bool {
	return opSameTime(s.at, o.at) && opDeref(s.reason) == opDeref(o.reason) && (s.reason == nil) == (o.reason == nil) && s.version == o.version
}

// opTenantTrail is a tenant's audit_log rows of one action, oldest first (the owner's read).
func opTenantTrail(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID, action string) []opVATTrailRow {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT actor_id, target, detail::text, at FROM public.audit_log
	                            WHERE tenant_id = $1 AND action = $2 ORDER BY at, id`, tenant, action)
	if err != nil {
		t.Fatalf("read the tenant's trail: %v", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (opVATTrailRow, error) {
		var x opVATTrailRow
		return x, r.Scan(&x.actor, &x.target, &x.detail, &x.at)
	})
	if err != nil {
		t.Fatalf("scan the tenant's trail: %v", err)
	}
	return out
}

// opSuspActions counts 00063's two actions in EVERY tenant's audit_log the test can see: under
// REPEATABLE READ only this transaction's writes move it.
func opSuspActions(t *testing.T, ctx context.Context, q opQuerier) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM public.audit_log WHERE action IN ($1, $2)`, opSuspendAction, opReinstateAction)
}

// opSuspOperatorRows counts the operator rows of one kind, one session, naming one tenant, of
// EXACTLY the tenant-write shape (no account, scope or page; detail {}).
func opSuspOperatorRows(t *testing.T, ctx context.Context, q opQuerier, kind string, session, tenant uuid.UUID) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM public.operator_audit_log
	                          WHERE kind = $1 AND session_id = $2 AND target_tenant_id = $3 AND actor_admin_id IS NOT NULL
	                            AND target_admin_id IS NULL AND target_scope IS NULL AND page_number IS NULL
	                            AND page_size IS NULL AND detail = '{}'::jsonb`, kind, session, tenant)
}

// opSuspendDetail and opReinstateDetail are the exact text jsonb gives the tenant rows' detail
// (keys shorter first).
func opSuspendDetail(reason string) string {
	b, _ := json.Marshal(reason)
	return `{"reason": ` + string(b) + `, "actor_kind": "operator"}`
}

const opReinstateDetail = `{"actor_kind": "operator"}`

// opBlockOf is the heap page of a tenant's current row version (the owner's read).
func opBlockOf(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT (ctid::text::point)[0]::bigint FROM public.tenants WHERE id = $1`, tenant)
}

// opOldestTenant is the database's oldest committed tenant -- one ANOTHER session can see -- read
// into memory. It is never printed.
func opOldestTenant(t *testing.T, ctx context.Context, q opQuerier) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := q.QueryRow(ctx, `SELECT id FROM public.tenants ORDER BY created_at, id LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("PREMISE: no committed tenant: %v", err)
	}
	return id
}

// opSuspSourceRules are the NAMED pins on a write's normalised source: the statements that carry
// ADR 0025 §2's decisions, spelled out -- each fragment must occur EXACTLY once. fn is the
// function, refusal its argument refusal, set the UPDATE's SET list, action/detail the tenant
// row's, kind the operator row's.
func opSuspSourceRules(fn, refusal, set, action, detail, kind string) []struct{ name, fragment string } {
	msg := map[string]string{"op_suspend_tenant": suspendRefusal, "op_reinstate_tenant": reinstateRefusal}[fn]
	raise := `raise exception '` + msg + `' using errcode = 'invalid_parameter_value';`
	return []struct{ name, fragment string }{
		{"the session first, then the argument refusal",
			`begin select t.session_id, t.admin_id into v_session, v_admin from public.op_touch_session(p_session) as t; if ` + refusal + ` then ` + raise + ` end if;`},
		{"the argument refusal, then at once the row lock (nothing read in between)",
			raise + ` end if; begin perform t.id from public.tenants as t where t.id = p_tenant_id for no key update;`},
		{"THE LOCK FIRST: by the tenant's primary key alone, FOR NO KEY UPDATE, nothing returned into a variable; then IF FOUND; then the ONE clock read; then the UPDATE (Y1)",
			`perform t.id from public.tenants as t where t.id = p_tenant_id for no key update; if found then v_at := clock_timestamp(); update public.tenants as t`},
		{"the UPDATE by the tenant's primary key, then the tenant row",
			`update public.tenants as t set ` + set + ` where t.id = p_tenant_id; insert into public.audit_log`},
		{"the tenant row: K6's columns, the tenant, the operator, the action, the tenant id as target, the detail, `at` the one clock read -- the guard's last statement (B14)",
			`insert into public.audit_log (tenant_id, actor_id, action, target, detail, at) values (p_tenant_id, v_admin, '` + action + `', p_tenant_id::text, ` + detail + `, v_at); end if;`},
		{"the operator row on every accepted call, after the guard",
			`end if; insert into public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) values ('` + kind + `', v_session, v_admin, p_tenant_id); exception when`},
		{"the caught constraint becomes the one refusal",
			`exception when integrity_constraint_violation then get stacked diagnostics v_constraint = constraint_name, v_state = returned_sqlstate; raise log '` + fn + `: a write failed constraint "%" (sqlstate %); refused', v_constraint, v_state; ` + raise + ` end; end;`},
	}
}

// opSuspSourceFindings returns one line per broken rule, plus: one statement writes tenants, one
// reads it, one writes each log; the clock is read once and AFTER the row lock (Y1); no other row
// lock, no EXISTS, no frozen clock, no RETURNING; p_tenant_id occurs exactly six times (the
// refusal, the lock's WHERE, the UPDATE's WHERE, the tenant row's tenant_id and target, the
// operator row's target_tenant_id -- the belt); the only RAISEs are the rule's three.
func opSuspSourceFindings(src string, rules []struct{ name, fragment string }) []string {
	var out []string
	for _, r := range rules {
		if n := strings.Count(src, r.fragment); n != 1 {
			out = append(out, fmt.Sprintf("%s: %d occurrence(s), want 1", r.name, n))
		}
	}
	for frag, want := range map[string]int{"update public.tenants": 1, "from public.tenants": 1, "public.tenants": 2,
		"insert into public.audit_log": 1, "insert into public.operator_audit_log": 1, "clock_timestamp()": 1,
		"raise ": 3, "p_tenant_id": 6, "where t.id = p_tenant_id": 2, "exists": 0, "returning": 0,
		"for update": 0, "for share": 0, "for key share": 0, "now()": 0, "statement_timestamp": 0} {
		if n := strings.Count(src, frag); n != want {
			out = append(out, fmt.Sprintf("%q occurs %d time(s), want %d", frag, n, want))
		}
	}
	if i, j := strings.Index(src, "for no key update"), strings.Index(src, "clock_timestamp()"); i < 0 || j < 0 || j < i {
		out = append(out, "the clock is read before the row lock (Y1: a call that waited would date its write back by the wait)")
	}
	return out
}

func opSuspendRules() []struct{ name, fragment string } {
	return opSuspSourceRules("op_suspend_tenant",
		`p_tenant_id is null or p_reason is null or p_reason not in ('non_payment', 'terms_breach', 'security_hold', 'customer_request')`,
		`suspended_at = coalesce(t.suspended_at, v_at), suspension_reason = p_reason`,
		opSuspendAction, `jsonb_build_object('actor_kind', 'operator', 'reason', p_reason)`, "tenant_suspended")
}

func opReinstateRules() []struct{ name, fragment string } {
	return opSuspSourceRules("op_reinstate_tenant", `p_tenant_id is null`,
		`suspended_at = null, suspension_reason = null`,
		opReinstateAction, `jsonb_build_object('actor_kind', 'operator')`, "tenant_reinstated")
}

// opBodyOf returns the body (AS $$ ... $$) of a function a migration section defines with
// `create`, and the whole statement from `create` to its `$$;`.
func opBodyOf(t *testing.T, section, create, fn string) (body, statement string) {
	t.Helper()
	m := regexp.MustCompile(`(?s)(` + regexp.QuoteMeta(create) + ` public\.` + fn + `\(.*?\nAS \$\$(.*?)\$\$;)`).FindStringSubmatch(section)
	if m == nil {
		t.Fatalf("the section does not define %s with %q", fn, create)
	}
	return m[2], m[1]
}

// opUndo undoes a list of compose-time edits on text, each of whose new text must occur exactly
// once; it reports the edits that did not.
func opUndo(text string, edits [][2]string) (string, []int) {
	var missing []int
	for i := len(edits) - 1; i >= 0; i-- {
		if strings.Count(text, edits[i][1]) != 1 {
			missing = append(missing, i+1)
			continue
		}
		text = strings.Replace(text, edits[i][1], edits[i][0], 1)
	}
	return text, missing
}

// ---------------------------------------------------------------- catalogue --

// TestOperator00063_TheFunctionsAndTheirExactSignatures pins what ADR 0021 §6's generic pins
// leave open for the functions 00063 creates, re-creates and replaces, at HEAD:
//   - op_suspend_tenant and op_reinstate_tenant: exact arguments and void, the owner, SECURITY
//     DEFINER, proconfig, not set-returning, one overload, EXECUTE for tappa_operator alone;
//   - op_read_tenants and op_read_tenant_detail: 00029's arguments and HEAD's result -- the
//     suspension at the END, after 00057's API state in the overview --, the same contract; their
//     bodies are 00029's (the list) and 00057's (the overview) with one edit each (undoing it
//     gives that body byte for byte), and the live bodies are 00063's;
//   - op_begin_read and op_read_audit keep their identities; their bodies are 00042's with one
//     list edit each (undoing it gives 00042's byte for byte), and the live bodies are 00063's;
//   - HEAD's exact CHECKs: the read kinds (unchanged since 00042, eight), the audit kinds
//     (twenty-two), the tenant-write shape (five kinds), actor_shape unchanged, tenants' two
//     suspension CHECKs -- all validated; NO index names a suspension column (ADR 0025 §1: the
//     columns are unindexed on purpose), and tenants' triggers are exactly 00041's and 00048's
//     two, neither of which fires on an UPDATE of the suspension (a BEFORE UPDATE WHEN the
//     timezone changes, an AFTER INSERT);
//   - the forward, frozen-clock, consumption and foreign-EXECUTE scans raise nothing and walked
//     the new functions;
//   - the definer's functions that write tenants, that name audit_log, that name each new kind and
//     that name the suspension columns are exactly the expected sets;
//   - each write's NAMED source pins hold (opSuspSourceRules), and the two reads' RETURN QUERY
//     carry the columns at the end. CONTROLS, on each write's live source: the clock read before
//     the lock (Y1); the guard as WHERE EXISTS instead of the locked read's FOUND; the tenant row
//     outside the guard; the operator row inside it; coalesce dropped (the first time
//     overwritten); `at` from now() and from statement_timestamp(); FOR UPDATE; no row lock; the
//     UPDATE, the tenant row and the operator row each naming another value than p_tenant_id (the
//     belt, one mutation per statement); a wider and a narrower reason list; the catch dropped; a
//     RETURNING -- each is reported.
func TestOperator00063_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	contract := func(name, wantArgs, wantResult string, wantRetset bool) {
		t.Helper()
		var args, result, owner string
		var retset, secdef bool
		var config []string
		if err := tx.QueryRow(ctx, `
			SELECT pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
			       pg_get_userbyid(p.proowner), p.proretset, p.prosecdef, p.proconfig
			  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			 WHERE n.nspname = 'public' AND p.proname = $1`, name).Scan(&args, &result, &owner, &retset, &secdef, &config); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if args != wantArgs || result != wantResult {
			t.Errorf("%s(%s) returns %s,\n want (%s) returning %s", name, args, result, wantArgs, wantResult)
		}
		if owner != "tappa_opdefiner" || !secdef || retset != wantRetset {
			t.Errorf("%s: owner %s, SECURITY DEFINER %v, set-returning %v", name, owner, secdef, retset)
		}
		if len(config) != 1 || config[0] != "search_path=pg_catalog, pg_temp" {
			t.Errorf("%s: proconfig %v", name, config)
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
	for name, want := range op00063Functions {
		contract(name, want.args, want.result, false)
	}
	for name, want := range op00063Reads {
		contract(name, want.args, want.result, true)
	}
	if got := opIdentity(t, ctx, tx, "op_begin_read"); got != "p_session text, p_kind text, p_params jsonb -> text" {
		t.Errorf("op_begin_read is now %s; the replacement keeps 00027's identity", got)
	}
	if got, want := opIdentity(t, ctx, tx, "op_read_audit"), op00031Read.args+" -> "+op00031Read.result; got != want {
		t.Errorf("op_read_audit is now %s; the replacement keeps 00031's identity %s", got, want)
	}

	// The replaced and re-created bodies: the earlier file's with this file's edits.
	up, _ := opMigrationSections(t, op00063File)
	live := func(fn string) string {
		t.Helper()
		var src string
		if err := tx.QueryRow(ctx, `SELECT p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, fn).Scan(&src); err != nil {
			t.Fatal(err)
		}
		return src
	}
	for fn, c := range op00063Edits {
		b63, _ := opBodyOf(t, up, c.create, fn)
		earlierUp, _ := opMigrationSections(t, c.earlierFile)
		earlier, _ := opBodyOf(t, earlierUp, c.earlierCreate, fn)
		undone, missing := opUndo(b63, c.edits)
		if len(missing) > 0 || undone != earlier {
			t.Errorf("00063's %s is not %s's body with its edits (edits not held exactly once: %v)", fn, c.earlierFile, missing)
		}
		if got := live(fn); got != b63 || got != opNewestUpBody(t, fn) {
			t.Errorf("the live %s is not 00063's body (or 00063 is not its newest definition)", fn)
		}
	}

	// HEAD's exact CHECKs.
	for _, c := range []struct{ table, name, want string }{
		{"operator_read_tickets", "operator_read_tickets_kind_check", opKindCheckDef(opKindsAt42)},
		{"operator_audit_log", "operator_audit_log_kind_check", opKindCheckDef(opAuditKinds)},
		{"operator_audit_log", opTenantWriteShape, opTenantWriteShapeDefOf(opTenantWriteKinds)},
		{"operator_audit_log", "operator_audit_log_actor_shape", opActorShapeDef33(opAuthEventKinds, opOwnerKinds)},
		{"tenants", opSuspensionPair, opSuspensionPairDef},
		{"tenants", opSuspensionReasonName, opSuspensionReasonDef},
	} {
		def, valid := opConstraint(t, ctx, tx, c.table, c.name)
		if def != c.want || !valid {
			t.Errorf("%s is %s (validated %v),\n want %s, validated", c.name, def, valid, c.want)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_index i
	                            WHERE i.indrelid = 'public.tenants'::regclass
	                              AND pg_get_indexdef(i.indexrelid) ~ 'suspen'`); n != 0 {
		t.Errorf("%d index(es) on tenants name a suspension column; ADR 0025 §1 keeps them unindexed (an index decides HOT by the previous state)", n)
	}
	// tenants' triggers, whole: the suspension's two writes assume none fires on their UPDATE.
	trows, err := tx.Query(ctx, `SELECT pg_get_triggerdef(oid) FROM pg_trigger
	                              WHERE tgrelid = 'public.tenants'::regclass AND NOT tgisinternal ORDER BY tgname`)
	if err != nil {
		t.Fatal(err)
	}
	triggers, err := pgx.CollectRows(trows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{
		"CREATE TRIGGER tenants_feed_counter AFTER INSERT ON public.tenants FOR EACH ROW EXECUTE FUNCTION feed_open_counter()",
		"CREATE TRIGGER tenants_holding_timezone_lock BEFORE UPDATE ON public.tenants FOR EACH ROW " +
			"WHEN ((old.timezone IS DISTINCT FROM new.timezone)) EXECUTE FUNCTION holding_timezone_lock()",
	}; !slices.Equal(triggers, want) {
		t.Errorf("tenants' triggers are %q,\n want exactly 00048's AFTER INSERT and 00041's BEFORE UPDATE WHEN the timezone changes "+
			"(neither fires on the suspension's UPDATE)", triggers)
	}

	// The scans.
	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for _, fn := range []string{"op_suspend_tenant", "op_reinstate_tenant", "op_read_tenants", "op_read_tenant_detail", "op_begin_read", "op_read_audit"} {
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
	consumption, read, err := opReadConsumptionFindings(ctx, tx)
	if err != nil {
		t.Fatalf("consumption scan: %v", err)
	}
	for _, fn := range []string{"op_read_tenants", "op_read_tenant_detail"} {
		if !slices.Contains(read, fn) {
			t.Errorf("anti-vacuity: the consumption scan did not read %s (it read %v)", fn, read)
		}
	}
	for _, f := range consumption {
		t.Error(f)
	}
	foreign, err := opDefinerForeignExecFindings(ctx, tx)
	if err != nil {
		t.Fatalf("definer EXECUTE scan: %v", err)
	}
	for _, f := range foreign {
		t.Errorf("tappa_opdefiner may EXECUTE %s", f)
	}

	// Who writes and names what.
	naming := func(re string) []string {
		t.Helper()
		qr, err := tx.Query(ctx, `SELECT proname FROM pg_proc WHERE proowner = 'tappa_opdefiner'::regrole
		                            AND lower(prosrc) ~ $1 ORDER BY 1`, re)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pgx.CollectRows(qr, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, c := range []struct {
		what, re string
		want     []string
	}{
		// HEAD's whole sets: 00034's VAT write, 00039's API pair, 00042's membership writers (K6
		// rows) and 00063's two.
		{"write tenants", `(update|insert into|delete from)\s+public\.tenants`, []string{"op_disable_tenant_api", "op_enable_tenant_api",
			"op_record_vat_check", "op_reinstate_tenant", "op_suspend_tenant"}},
		{"name audit_log", `public\.audit_log\M`, []string{"op_add_holding_member", "op_disable_tenant_api", "op_enable_tenant_api",
			"op_record_vat_check", "op_reinstate_tenant", "op_remove_holding_member", "op_suspend_tenant"}},
		{"name the kind tenant_suspended", `tenant_suspended`, []string{"op_begin_read", "op_read_audit", "op_suspend_tenant"}},
		{"name the kind tenant_reinstated", `tenant_reinstated`, []string{"op_begin_read", "op_read_audit", "op_reinstate_tenant"}},
		{"name a suspension column", `suspended_at|suspension_reason`, []string{"op_read_tenant_detail", "op_read_tenants", "op_reinstate_tenant", "op_suspend_tenant"}},
	} {
		if got := naming(c.re); !slices.Equal(got, c.want) {
			t.Errorf("the definer's functions that %s are %v, want %v", c.what, got, c.want)
		}
	}

	// The named source pins, and a control per pin on the live source.
	for _, w := range []struct {
		sig   string
		rules []struct{ name, fragment string }
	}{
		{"public.op_suspend_tenant(text, uuid, text)", opSuspendRules()},
		{"public.op_reinstate_tenant(text, uuid)", opReinstateRules()},
	} {
		src := opNormalisedBody(t, ctx, tx, w.sig)
		for _, f := range opSuspSourceFindings(src, w.rules) {
			t.Errorf("%s: %s", w.sig, f)
		}
		controls := []struct{ name, old, new string }{
			{"the clock read before the lock (Y1)", "begin perform t.id from public.tenants", "begin v_at := clock_timestamp(); perform t.id from public.tenants"},
			{"the guard as WHERE EXISTS instead of the locked read's FOUND", "if found then", "if exists (select 1 from public.tenants as x where x.id = p_tenant_id) then"},
			{"the tenant row outside the guard", ", v_at); end if; insert into public.operator_audit_log", ", v_at); insert into public.operator_audit_log"},
			{"the operator row inside the guard", "end if; insert into public.operator_audit_log", "insert into public.operator_audit_log"},
			{"`at` from now()", "v_at := clock_timestamp();", "v_at := now();"},
			{"`at` from statement_timestamp()", "v_at := clock_timestamp();", "v_at := statement_timestamp();"},
			{"FOR UPDATE in place of FOR NO KEY UPDATE", " for no key update;", " for update;"},
			{"no row lock", " for no key update;", ";"},
			{"the belt: the lock by another value", "where t.id = p_tenant_id for no key update", "where t.id = v_admin for no key update"},
			{"the belt: the UPDATE by another value", " where t.id = p_tenant_id; insert into public.audit_log", " where t.id = v_admin; insert into public.audit_log"},
			{"the belt: the tenant row naming another tenant", "values (p_tenant_id, v_admin, '", "values (v_admin, v_admin, '"},
			{"the belt: the operator row naming another tenant", "v_session, v_admin, p_tenant_id); exception", "v_session, v_admin, null); exception"},
			{"the catch dropped", "exception when integrity_constraint_violation then", "exception when division_by_zero then"},
			{"a RETURNING on the tenant row", ", v_at); end if;", ", v_at) returning id; end if;"},
		}
		if strings.Contains(w.sig, "suspend_tenant(") {
			controls = append(controls,
				struct{ name, old, new string }{"coalesce dropped (the first suspension's time overwritten)", "suspended_at = coalesce(t.suspended_at, v_at)", "suspended_at = v_at"},
				struct{ name, old, new string }{"a wider reason list", "'customer_request') then", "'customer_request', 'other') then"},
				struct{ name, old, new string }{"a narrower reason list", "'security_hold', 'customer_request') then", "'security_hold') then"},
				struct{ name, old, new string }{"no reason refusal", " or p_reason not in", " and p_reason not in"})
		}
		for _, c := range controls {
			if strings.Count(src, c.old) != 1 {
				t.Errorf("%s CONTROL %q: its anchor occurs %d times in the live source", w.sig, c.name, strings.Count(src, c.old))
				continue
			}
			if got := opSuspSourceFindings(strings.Replace(src, c.old, c.new, 1), w.rules); len(got) == 0 {
				t.Errorf("%s CONTROL %q: the source pins did not report it", w.sig, c.name)
			}
		}
	}
	for sig, want := range map[string]string{
		"public.op_read_tenants(text, text, text, integer, integer)": "return query select t.id, t.name, t.created_at, t.plan, t.suspended_at from public.tenants as t where",
		"public.op_read_tenant_detail(text, text, uuid)": "where a.tenant_id = p_tenant_id and a.status = 'active'), t.api_enabled_at, t.api_code, t.suspended_at, t.suspension_reason " +
			"from public.tenants as t where t.id = p_tenant_id;",
	} {
		if n := strings.Count(opNormalisedBody(t, ctx, tx, sig), want); n != 1 {
			t.Errorf("%s's RETURN QUERY does not end its row with the suspension (%d occurrence(s))", sig, n)
		}
	}
}

// opTenantWriteReach names, as "who -> role", every role tappa_app or tappa_operator can use --
// pg_has_role MEMBER: itself, an inherited membership and one it reaches only by SET ROLE; each
// role's privilege counts PUBLIC's -- that holds a table-level UPDATE or INSERT on tenants, or an
// UPDATE or INSERT on either suspension column. The column half is the third eye's F3 (3rd round):
// has_column_privilege asked of tappa_app alone does not see a grant it can only SET ROLE to.
func opTenantWriteReach(t *testing.T, ctx context.Context, q opQuerier) []string {
	t.Helper()
	rows, err := q.Query(ctx, `
		SELECT who.rolname || ' -> ' || r.rolname
		  FROM pg_roles who JOIN pg_roles r ON pg_has_role(who.oid, r.oid, 'MEMBER')
		 WHERE who.rolname IN ('tappa_app', 'tappa_operator')
		   AND (has_table_privilege(r.oid, 'public.tenants', 'UPDATE')
		        OR has_table_privilege(r.oid, 'public.tenants', 'INSERT')
		        OR EXISTS (SELECT 1 FROM unnest(ARRAY['suspended_at', 'suspension_reason']) AS c (col),
		                                 unnest(ARRAY['UPDATE', 'INSERT']) AS p (priv)
		                    WHERE has_column_privilege(r.oid, 'public.tenants', c.col, p.priv)))
		 ORDER BY 1`)
	if err != nil {
		t.Fatalf("read the roles that reach tenants' writes: %v", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read the roles that reach tenants' writes: %v", err)
	}
	return out
}

// TestOperator00063_TheAppCannotWriteASuspensionAndTheDefinerWritesOnlyItsColumns is OP-15's
// "who writes what" on the catalogue and as statements (ADR 0025 §1; the card's acceptance
// "tappa_app suspended_at UPDATE edemiyor"):
//   - tappa_app on tenants: UPDATE exactly 00024's three columns and INSERT exactly 00016/00017's
//     list -- neither suspension column on either --, SELECT every column (its table-level
//     grant, derived from the catalogue: the suspension is READ), no table-level UPDATE or INSERT
//     and no UPDATE or INSERT on a suspension column through ANY role it can become
//     (opTenantWriteReach: itself, PUBLIC, inherited and SET ROLE memberships; CONTROL, the third
//     eye's N6b: a suspension column UPDATE reached only by SET ROLE is named), no DELETE or
//     TRUNCATE; as statements, in its own tenant's context: the
//     suspension set, the suspension cleared, one column alone, and a tenant INSERT naming the
//     columns are each 42501. CONTROL: it reads its own suspension and saves its own name;
//   - tappa_opdefiner on tenants: SELECT and UPDATE exactly HEAD's lists (the two columns last), no
//     INSERT; as statements, UPDATE of name, of vat_number and of a suspension column together with
//     the name are 42501, an INSERT and a DELETE are 42501; CONTROL: the suspension's UPDATE goes
//     through (in a savepoint rolled back);
//   - tappa_operator holds nothing on tenants; its direct read and write of the suspension are
//     42501; tappa_app calling either write is 42501.
func TestOperator00063_TheAppCannotWriteASuspensionAndTheDefinerWritesOnlyItsColumns(t *testing.T) {
	ctx, tx := opTx(t)
	for _, c := range []struct{ role, priv, want string }{
		{"tappa_app", "UPDATE", opAppTenantsUpd},
		{"tappa_app", "INSERT", opAppTenantsIns},
		{"tappa_app", "SELECT", opAllColumns(t, ctx, tx, "tenants")},
		{"tappa_opdefiner", "SELECT", opDefinerTenantsSel63},
		{"tappa_opdefiner", "UPDATE", opDefinerTenantsUpd63},
		{"tappa_opdefiner", "INSERT", ""},
		{"tappa_operator", "SELECT", ""},
		{"tappa_operator", "INSERT", ""},
		{"tappa_operator", "UPDATE", ""},
	} {
		if got := opColumns(t, ctx, tx, c.role, "tenants", c.priv); got != c.want {
			t.Errorf("%s %s on tenants = (%s), want (%s)", c.role, c.priv, got, c.want)
		}
	}
	if !strings.HasSuffix(opAllColumns(t, ctx, tx, "tenants"), ",suspended_at,suspension_reason") {
		t.Error("PREMISE: the two suspension columns are not tenants' last columns")
	}
	if reach := opTenantWriteReach(t, ctx, tx); len(reach) != 0 {
		t.Errorf("roles tappa_app or tappa_operator can use write tenants or a suspension column: %v", reach)
	}
	// CONTROL -- the third eye's N6b (3rd round): a column UPDATE on the suspension that tappa_app
	// reaches ONLY by SET ROLE (a membership WITH INHERIT FALSE). tappa_app's own column privilege
	// does not show it, yet SET ROLE uses it; opTenantWriteReach must name it. In a savepoint,
	// rolled back.
	n6b, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`GRANT UPDATE (suspended_at, suspension_reason) ON public.tenants TO tappa_resolver`,
		`GRANT tappa_resolver TO tappa_app WITH INHERIT FALSE`,
	} {
		if _, err := n6b.Exec(ctx, s); err != nil {
			t.Fatalf("N6b setup %q: %v", s, err)
		}
	}
	if got := opColumns(t, ctx, n6b, "tappa_app", "tenants", "UPDATE"); got != opAppTenantsUpd {
		t.Fatalf("PREMISE (N6b): tappa_app's own UPDATE columns moved to (%s); the gap is that they do not", got)
	}
	errSetRoleWrote := errors.New("the SET ROLE path wrote")
	if err := opAs(t, ctx, n6b, "tappa_app", func(as pgx.Tx) error {
		if _, err := as.Exec(ctx, `SET LOCAL ROLE tappa_resolver`); err != nil {
			return err
		}
		if _, err := as.Exec(ctx, `UPDATE public.tenants SET suspended_at = NULL, suspension_reason = NULL WHERE false`); err != nil {
			return err
		}
		return errSetRoleWrote
	}); !errors.Is(err, errSetRoleWrote) {
		t.Fatalf("PREMISE (N6b): tappa_app could not SET ROLE and UPDATE the suspension (%v); the control would prove nothing", err)
	}
	if reach := opTenantWriteReach(t, ctx, n6b); !slices.Contains(reach, "tappa_app -> tappa_resolver") {
		t.Errorf("CONTROL (N6b): a suspension column UPDATE tappa_app reaches only by SET ROLE is not seen (%v)", reach)
	}
	if err := n6b.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"tappa_app", "tappa_opdefiner", "tappa_operator"} {
		for _, priv := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.tenants', $2)`, role, priv).Scan(&may); err != nil || may {
				t.Errorf("has_table_privilege(%s, tenants, %s) = %v (err %v), want false", role, priv, may, err)
			}
		}
	}

	f := opSuspTenant(t, ctx, tx, "op15 grants "+opToken(t), "", "")
	asApp := func(sql string, args ...any) error {
		t.Helper()
		return opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.id.String()); err != nil {
				return err
			}
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
	}
	for _, probe := range []struct {
		what, sql string
		args      []any
	}{
		{"setting its own suspension", `UPDATE tenants SET suspended_at = clock_timestamp(), suspension_reason = 'customer_request' WHERE id = $1`, []any{f.id}},
		{"clearing its own suspension", `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, []any{f.id}},
		{"the reason alone", `UPDATE tenants SET suspension_reason = suspension_reason WHERE id = $1`, []any{f.id}},
		{"the time alone, beside its own name", `UPDATE tenants SET name = name, suspended_at = suspended_at WHERE id = $1`, []any{f.id}},
		{"a tenant born suspended", `INSERT INTO tenants (id, name, vat_number, business_type, structure, suspended_at, suspension_reason)
		                             VALUES ($1, 'op15 x', $2, 'cafe', 'single', clock_timestamp(), 'non_payment')`, []any{f.id, "VAT-OP15-X-" + uuid.NewString()}},
	} {
		opWant(t, asApp(probe.sql, probe.args...), sqlstateInsufficientPrivi, "tappa_app: "+probe.what)
	}
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.id.String()); err != nil {
			return err
		}
		var at *time.Time
		var reason *string
		if err := sp.QueryRow(ctx, `SELECT suspended_at, suspension_reason FROM tenants WHERE id = $1`, f.id).Scan(&at, &reason); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `UPDATE tenants SET name = 'op15 renamed' WHERE id = $1`, f.id)
		return err
	}); err != nil {
		t.Fatalf("CONTROL: tappa_app cannot read its own suspension or save its own name (%v); the refusals above would not be the missing grants", err)
	}

	for _, probe := range []struct {
		what, sql string
		args      []any
	}{
		{"the name", `UPDATE public.tenants SET name = 'op15 renamed' WHERE id = $1`, []any{f.id}},
		{"the number", `UPDATE public.tenants SET vat_number = $2 WHERE id = $1`, []any{f.id, "VAT-OP15-Y-" + uuid.NewString()}},
		{"the suspension with the name", `UPDATE public.tenants SET suspended_at = clock_timestamp(), suspension_reason = 'non_payment', name = 'x' WHERE id = $1`, []any{f.id}},
		{"a new tenant", `INSERT INTO public.tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'op15 y', $2, 'cafe', 'single')`, []any{uuid.New(), "VAT-OP15-Z-" + uuid.NewString()}},
		{"a tenant row", `DELETE FROM public.tenants WHERE id = $1`, []any{f.id}},
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_opdefiner", probe.sql, probe.args...), sqlstateInsufficientPrivi, "tappa_opdefiner: "+probe.what)
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := opExecAs(t, ctx, sp, "tappa_opdefiner", `UPDATE public.tenants SET suspended_at = coalesce(suspended_at, clock_timestamp()),
	                                                   suspension_reason = 'terms_breach' WHERE id = $1`, f.id); err != nil {
		t.Errorf("CONTROL: tappa_opdefiner cannot write the suspension (%v); the refusals above would not be the missing grants", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT suspended_at FROM public.tenants WHERE id = $1`, f.id),
		sqlstateInsufficientPrivi, "tappa_operator reading a suspension directly")
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `UPDATE public.tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, f.id),
		sqlstateInsufficientPrivi, "tappa_operator lifting a suspension directly")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", suspendTenantSQL, opRandHex(t), f.id, "non_payment"), sqlstateInsufficientPrivi,
		"tappa_app calling op_suspend_tenant")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", reinstateTenantSQL, opRandHex(t), f.id), sqlstateInsufficientPrivi,
		"tappa_app calling op_reinstate_tenant")
}

// TestOperator00063_TheTenantsChecksBindTheOwnerToo drives tenants' two suspension CHECKs as
// statements, AS THE OWNER (a CHECK binds every writer -- ADR 0025 §1): a time without a reason
// and a reason without a time are refused by tenants_suspension_both_or_neither, on an UPDATE and
// on an INSERT; a reason outside the closed set -- another word, the empty string, another case,
// a trailing space, a hyphen -- by tenants_suspension_reason_check (23514, each CHECK named).
// CONTROLS: each of the four reasons with a time, and both NULL, are written.
func TestOperator00063_TheTenantsChecksBindTheOwnerToo(t *testing.T) {
	ctx, tx := opTx(t)
	f := opSuspTenant(t, ctx, tx, "op15 checks "+opToken(t), "", "")
	refusedBy := func(what, constraint, sql string, args ...any) {
		t.Helper()
		err := opTry(t, ctx, tx, sql, args...)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != sqlstateCheckViolation || pg.ConstraintName != constraint {
			t.Errorf("%s: %v, want 23514 from %s", what, err, constraint)
		}
	}
	refusedBy("a time without a reason", opSuspensionPair, `UPDATE tenants SET suspended_at = clock_timestamp() WHERE id = $1`, f.id)
	refusedBy("a reason without a time", opSuspensionPair, `UPDATE tenants SET suspension_reason = 'non_payment' WHERE id = $1`, f.id)
	refusedBy("a tenant born with a time and no reason", opSuspensionPair,
		`INSERT INTO tenants (id, name, vat_number, business_type, structure, suspended_at) VALUES ($1, 'op15 c', $2, 'cafe', 'single', clock_timestamp())`,
		uuid.New(), "VAT-OP15-C-"+uuid.NewString())
	for _, reason := range []string{"other", "", "NON_PAYMENT", "non_payment ", " non_payment", "non-payment", "suspended"} {
		refusedBy(fmt.Sprintf("the reason %q", reason), opSuspensionReasonName,
			`UPDATE tenants SET suspended_at = clock_timestamp(), suspension_reason = $2 WHERE id = $1`, f.id, reason)
	}
	for _, reason := range opSuspensionReasons {
		if err := opTry(t, ctx, tx, `UPDATE tenants SET suspended_at = clock_timestamp(), suspension_reason = $2 WHERE id = $1`, f.id, reason); err != nil {
			t.Errorf("CONTROL: the reason %q with a time: %v", reason, err)
		}
	}
	if err := opTry(t, ctx, tx, `UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, f.id); err != nil {
		t.Errorf("CONTROL: both NULL: %v", err)
	}
}

// TestOperator00063_TheTenantWriteKindsNameTheirTenantAndNothingElse drives
// operator_audit_log_tenant_write_shape and the kind CHECK for 00063's two kinds, as the owner:
//   - a row of either kind with its session, its operator and a tenant is written (CONTROL);
//   - without a tenant, with an account, a scope, a page number, a page size, or a detail naming
//     the reason, it is refused by the tenant-write CHECK (23514, named); without a session by
//     actor_shape;
//   - WHO may write one: tappa_operator and tappa_app are refused (42501); op_record_auth_event
//     refuses the kinds (22023, no row); and -- COUNTED, 00034's LV6 class -- a statement run AS
//     the definer writes one with 00026's INSERT columns: only op_* bodies run as the definer, and
//     the only ones naming the kinds outside the two filter lists are the two writes
//     (TestOperator00063_TheFunctionsAndTheirExactSignatures).
func TestOperator00063_TheTenantWriteKindsNameTheirTenantAndNothingElse(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	_, session := opNewSession(t, ctx, tx, a.id, true)
	f := opSuspTenant(t, ctx, tx, "op15 shape "+opToken(t), "", "")
	refusedBy := func(what, constraint, sql string, args ...any) {
		t.Helper()
		err := opTry(t, ctx, tx, sql, args...)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != sqlstateCheckViolation || pg.ConstraintName != constraint {
			t.Errorf("%s: %v, want 23514 from %s", what, err, constraint)
		}
	}
	for _, kind := range []string{"tenant_suspended", "tenant_reinstated"} {
		if err := opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) VALUES ($1, $2, $3, $4)`,
			kind, session, a.id, f.id); err != nil {
			t.Errorf("CONTROL: a %s row naming its session, operator and tenant: %v", kind, err)
		}
		for _, c := range []struct {
			what, sql string
			args      []any
		}{
			{"without a tenant", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ($1, $2, $3)`, []any{kind, session, a.id}},
			{"with an account", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, target_admin_id) VALUES ($1, $2, $3, $4, $3)`, []any{kind, session, a.id, f.id}},
			{"with a scope", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, target_scope) VALUES ($1, $2, $3, $4, 'tenant_detail')`, []any{kind, session, a.id, f.id}},
			{"with a page number", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, page_number) VALUES ($1, $2, $3, $4, 1)`, []any{kind, session, a.id, f.id}},
			{"with a page size", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, page_size) VALUES ($1, $2, $3, $4, 1)`, []any{kind, session, a.id, f.id}},
			{"with the reason in detail", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, detail) VALUES ($1, $2, $3, $4, '{"reason": "non_payment"}')`, []any{kind, session, a.id, f.id}},
		} {
			refusedBy(kind+" "+c.what, opTenantWriteShape, c.sql, c.args...)
		}
		refusedBy(kind+" without a session", "operator_audit_log_actor_shape",
			`INSERT INTO operator_audit_log (kind, target_tenant_id) VALUES ($1, $2)`, kind, f.id)
		for _, role := range []string{"tappa_operator", "tappa_app"} {
			opWant(t, opExecAs(t, ctx, tx, role, `INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) VALUES ($1, $2, $3, $4)`,
				kind, session, a.id, f.id), sqlstateInsufficientPrivi, role+" writing "+kind+" directly")
		}
		before := opAudit(t, ctx, tx)
		opWantClean(t, opRecord(t, ctx, tx, kind, nil, a.id), sqlstateInvalidParameter, recordKindRefusal31, "op_record_auth_event("+kind+")")
		if d := opAudit(t, ctx, tx) - before; d != 0 {
			t.Errorf("op_record_auth_event(%s) refused and left %d row(s)", kind, d)
		}
		if err := opExecAs(t, ctx, tx, "tappa_opdefiner", `INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) VALUES ($1, $2, $3, $4)`,
			kind, session, a.id, f.id); err != nil {
			t.Errorf("COUNTED LIMIT moved: as tappa_opdefiner a %s row is refused (%v); measured was written -- update ADR 0021's OP-15 note", kind, err)
		}
	}
}

// TestOperator00063_TheReasonSetIsOneSetInEveryCopy holds the copies of the closed reason set
// equal (ADR 0025 §1, "Kopyalar"; the orchestrator's K15-1). In phase A they are three: the
// CHECK (tenants_suspension_reason_check), op_suspend_tenant's refusal list, and internal/db's
// SuspensionReasons (whose typed constants TestSuspensionReasons_TheTypedConstantsAreTheList holds
// to the same list). Phase B adds the panel's sentences and phase C the operator's labels -- each
// joins THIS test. And as behaviour: op_suspend_tenant takes every reason the CHECK names (the
// stored reason is the one sent) and refuses, with its one 22023, a value outside it; the Go side
// refuses that value before a round trip.
func TestOperator00063_TheReasonSetIsOneSetInEveryCopy(t *testing.T) {
	ctx, tx := opTx(t)
	def, _ := opConstraint(t, ctx, tx, "tenants", opSuspensionReasonName)
	arrays := opQuotedArrays(def)
	if len(arrays) != 1 || !slices.Equal(arrays[0], opSuspensionReasons) {
		t.Errorf("tenants_suspension_reason_check names %v, want %v", arrays, opSuspensionReasons)
	}
	src := opNormalisedBody(t, ctx, tx, "public.op_suspend_tenant(text, uuid, text)")
	m := regexp.MustCompile(`p_reason not in \(([^)]*)\)`).FindAllStringSubmatch(src, -1)
	if len(m) != 1 || !slices.Equal(opQuotedList(m[0][1]), opSuspensionReasons) {
		t.Errorf("op_suspend_tenant's refusal names %v, want exactly one list %v", m, opSuspensionReasons)
	}
	var goList []string
	for _, r := range SuspensionReasons() {
		goList = append(goList, string(r))
		if !r.Known() {
			t.Errorf("Known(%q) = false", r)
		}
	}
	if !slices.Equal(goList, opSuspensionReasons) {
		t.Errorf("SuspensionReasons() is %v, want %v", goList, opSuspensionReasons)
	}

	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	f := opSuspTenant(t, ctx, tx, "op15 reasons "+opToken(t), "", "")
	for _, reason := range arrays[0] {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := opSuspend(t, ctx, sp, hash, f.id, reason); err != nil {
			t.Errorf("op_suspend_tenant refuses the CHECK's reason %q: %v", reason, err)
		} else if s := opSuspOf(t, ctx, sp, f.id); opDeref(s.reason) != reason || s.at == nil {
			t.Errorf("op_suspend_tenant(%q) stored %q", reason, opDeref(s.reason))
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, reason := range []string{"other", "", "NON_PAYMENT", "non_payment ", "non-payment", "suspended"} {
		opWantClean(t, opSuspend(t, ctx, tx, hash, f.id, reason), sqlstateInvalidParameter, suspendRefusal,
			fmt.Sprintf("op_suspend_tenant(%q)", reason), hash, f.id.String())
		if SuspensionReason(reason).Known() {
			t.Errorf("Known(%q) = true", reason)
		}
	}
}

// TestOperator00063_CallersTempTableIsNeverRead is ADR 0021 §6's temp-table shadow for the two
// writes and the re-created overview: the caller creates temp tables with every name they touch
// (tenants, audit_log, operator_audit_log, platform_sessions, platform_admins), fills them with
// forged rows -- a forged session; the REAL tenant's id, suspended in the shadow; a tenant that
// exists ONLY in the shadow -- and GRANTs them to tappa_opdefiner (the step without which a broken
// search_path would fail with "permission denied" and look refused). The functions still read and
// write the real tables: the forged session is refused by both writes; the overview returns the
// real tenant's suspension (none), not the shadow's; a suspension of the real tenant changes the
// real row and writes the real rows; a call for the shadow-only id finds no tenant (no tenant row
// anywhere -- B14 against the real table) and writes its operator row to the real log; the shadow
// tables are not written.
func TestOperator00063_CallersTempTableIsNeverRead(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	real := opSuspTenant(t, ctx, tx, "op15 real "+opToken(t), "", "")
	shadowOnly := uuid.New()
	forgedSession := opRandHex(t)
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		for _, s := range []string{
			`CREATE TEMP TABLE tenants (id uuid, name text, suspended_at timestamptz, suspension_reason text)`,
			`CREATE TEMP TABLE audit_log (id uuid DEFAULT gen_random_uuid(), tenant_id uuid, actor_id uuid, action text, target text,
			     detail jsonb DEFAULT '{}', at timestamptz DEFAULT clock_timestamp())`,
			`CREATE TEMP TABLE platform_sessions (id uuid DEFAULT gen_random_uuid(), admin_id uuid, token_hash text,
			     created_at timestamptz DEFAULT clock_timestamp(), mfa_verified_at timestamptz,
			     last_used_at timestamptz DEFAULT clock_timestamp(), revoked_at timestamptz)`,
			`CREATE TEMP TABLE platform_admins (id uuid, email text, display_name text, status text)`,
			`CREATE TEMP TABLE operator_audit_log (id uuid DEFAULT gen_random_uuid(), at timestamptz DEFAULT clock_timestamp(),
			     kind text, session_id uuid, actor_admin_id uuid, target_admin_id uuid, target_tenant_id uuid,
			     target_scope text, page_number integer, page_size integer, detail jsonb DEFAULT '{}')`,
			`GRANT ALL ON pg_temp.tenants, pg_temp.audit_log, pg_temp.platform_sessions, pg_temp.platform_admins,
			     pg_temp.operator_audit_log TO tappa_opdefiner`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return err
			}
		}
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO pg_temp.tenants VALUES ($1, 'SHADOW name of the real id', '2001-02-03 04:05:06+00', 'security_hold')`, []any{real.id}},
			{`INSERT INTO pg_temp.tenants VALUES ($1, 'SHADOW-only tenant', NULL, NULL)`, []any{shadowOnly}},
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
	var reachable int64
	if err := opAs(t, ctx, tx, "tappa_opdefiner", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.tenants) + (SELECT count(*) FROM pg_temp.platform_sessions)
		                              + (SELECT count(*) FROM pg_temp.audit_log)`).Scan(&reachable)
	}); err != nil {
		t.Fatalf("the definer role cannot read the shadow (%v); the GRANT step is what makes this test mean anything", err)
	}
	if reachable != 3 {
		t.Fatalf("the definer role sees %d forged rows, want 3", reachable)
	}

	opWantClean(t, opSuspend(t, ctx, tx, forgedSession, real.id, "non_payment"), sqlstateInvalidAuthorization, touchRefusal00026,
		"op_suspend_tenant with a session that exists only in the caller's temp table")
	opWantClean(t, opReinstate(t, ctx, tx, forgedSession, real.id), sqlstateInvalidAuthorization, touchRefusal00026,
		"op_reinstate_tenant with a session that exists only in the caller's temp table")

	rows, err := opReadDetailHead(t, ctx, tx, hash, opForgeDetail(t, ctx, tx, session, a.id, real.id, opCommittedXact(t, ctx)), real.id)
	if err != nil || len(rows) != 1 || rows[0].SuspendedAt != nil || rows[0].SuspensionReason != "" || rows[0].Name != real.name {
		t.Errorf("the overview read %d row(s) (err %v); want the REAL row: its name and no suspension", len(rows), err)
	}

	if err := opSuspend(t, ctx, tx, hash, real.id, "terms_breach"); err != nil {
		t.Fatalf("op_suspend_tenant with the real session: %v", err)
	}
	if s := opSuspOf(t, ctx, tx, real.id); s.at == nil || opDeref(s.reason) != "terms_breach" || len(opTenantTrail(t, ctx, tx, real.id, opSuspendAction)) != 1 ||
		opSuspOperatorRows(t, ctx, tx, "tenant_suspended", session, real.id) != 1 {
		t.Error("the suspension did not reach the REAL tenant, its REAL trail and the REAL operator log")
	}
	actions := opSuspActions(t, ctx, tx)
	if err := opSuspend(t, ctx, tx, hash, shadowOnly, "non_payment"); err != nil {
		t.Fatalf("op_suspend_tenant for the shadow-only id: %v", err)
	}
	if err := opReinstate(t, ctx, tx, hash, shadowOnly); err != nil {
		t.Fatalf("op_reinstate_tenant for the shadow-only id: %v", err)
	}
	if d := opSuspActions(t, ctx, tx) - actions; d != 0 || opSuspOperatorRows(t, ctx, tx, "tenant_suspended", session, shadowOnly) != 1 ||
		opSuspOperatorRows(t, ctx, tx, "tenant_reinstated", session, shadowOnly) != 1 {
		t.Errorf("the shadow-only id: %d tenant row(s) (want 0 -- it names no REAL tenant) and its operator rows not in the REAL log", d)
	}
	var shadowAudit, shadowLog int64
	var shadowAt *time.Time
	var shadowReason *string
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.audit_log), (SELECT count(*) FROM pg_temp.operator_audit_log),
		                                t.suspended_at, t.suspension_reason FROM pg_temp.tenants t WHERE t.id = $1`, real.id).
			Scan(&shadowAudit, &shadowLog, &shadowAt, &shadowReason)
	}); err != nil {
		t.Fatal(err)
	}
	if shadowAudit != 0 || shadowLog != 0 || shadowAt == nil || opDeref(shadowReason) != "security_hold" {
		t.Errorf("the CALLER's temp tables were written: audit_log=%d operator log=%d shadow reason=%q; want 0, 0, the planted security_hold",
			shadowAudit, shadowLog, opDeref(shadowReason))
	}
}

// opSuspSchemaState is the catalogue state 00063's Down and Up move between.
type opSuspSchemaState struct {
	writes                      int64
	begin, audit                string
	tenantsResult, detailResult string
	tenantsBody, detailBody     string
	columns, checks             int64
	readsOwned, readsOperatorX  bool
	readsForeignX               bool
	kinds, shape                string
	kindsValid, shapeValid      bool
	tenantsSel, tenantsUpd      string
}

func opSuspReadSchema(t *testing.T, ctx context.Context, q pgx.Tx) opSuspSchemaState {
	t.Helper()
	var s opSuspSchemaState
	if err := q.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM pg_proc WHERE proname IN ('op_suspend_tenant', 'op_reinstate_tenant')),
		       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_begin_read(text, text, jsonb)'::regprocedure),
		       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_read_audit(text, text, text, integer, integer)'::regprocedure),
		       pg_get_function_result('public.op_read_tenants(text, text, text, integer, integer)'::regprocedure),
		       pg_get_function_result('public.op_read_tenant_detail(text, text, uuid)'::regprocedure),
		       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_read_tenants(text, text, text, integer, integer)'::regprocedure),
		       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_read_tenant_detail(text, text, uuid)'::regprocedure),
		       (SELECT count(*) FROM pg_attribute WHERE attrelid = 'public.tenants'::regclass AND NOT attisdropped
		                                            AND attname IN ('suspended_at', 'suspension_reason')),
		       (SELECT count(*) FROM pg_constraint WHERE conrelid = 'public.tenants'::regclass
		                                             AND conname IN ($1, $2)),
		       (SELECT bool_and(pg_get_userbyid(proowner) = 'tappa_opdefiner') FROM pg_proc
		         WHERE proname IN ('op_read_tenants', 'op_read_tenant_detail')),
		       has_function_privilege('tappa_operator', 'public.op_read_tenants(text, text, text, integer, integer)', 'EXECUTE')
		         AND has_function_privilege('tappa_operator', 'public.op_read_tenant_detail(text, text, uuid)', 'EXECUTE'),
		       has_function_privilege('tappa_app', 'public.op_read_tenants(text, text, text, integer, integer)', 'EXECUTE')
		         OR has_function_privilege('tappa_app', 'public.op_read_tenant_detail(text, text, uuid)', 'EXECUTE')
		         OR has_function_privilege('public', 'public.op_read_tenants(text, text, text, integer, integer)', 'EXECUTE')
		         OR has_function_privilege('public', 'public.op_read_tenant_detail(text, text, uuid)', 'EXECUTE')`,
		opSuspensionPair, opSuspensionReasonName).Scan(&s.writes, &s.begin, &s.audit, &s.tenantsResult, &s.detailResult,
		&s.tenantsBody, &s.detailBody, &s.columns, &s.checks, &s.readsOwned, &s.readsOperatorX, &s.readsForeignX); err != nil {
		t.Fatalf("read the state: %v", err)
	}
	s.kinds, s.kindsValid = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_kind_check")
	s.shape, s.shapeValid = opConstraint(t, ctx, q, "operator_audit_log", opTenantWriteShape)
	s.tenantsSel = opColumns(t, ctx, q, "tappa_opdefiner", "tenants", "SELECT")
	s.tenantsUpd = opColumns(t, ctx, q, "tappa_opdefiner", "tenants", "UPDATE")
	return s
}

// opSuspHeld is the relation locks this session holds on tenants, audit_log and
// operator_audit_log, sorted.
func opSuspHeld(t *testing.T, ctx context.Context, q opQuerier) []string {
	t.Helper()
	qr, err := q.Query(ctx, `SELECT relation::regclass::text || ':' || mode FROM pg_locks
	                          WHERE locktype = 'relation' AND granted AND pid = pg_backend_pid()
	                            AND relation IN ('public.tenants'::regclass, 'public.audit_log'::regclass,
	                                             'public.operator_audit_log'::regclass) ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(qr, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestOperator00063_DownGivesBack00062AndUpTakesItAgain runs 00063's Down and Up from the
// migration file inside the test's transaction (HEAD is 00063; opAtVersion(63) only clears the
// tickets of kinds 00063 does not know -- none).
//   - THE TEXT: the Down's op_begin_read and op_read_audit are 00042's Up bodies VERBATIM; its
//     op_read_tenants is 00029's and its op_read_tenant_detail 00057's whole CREATE statement
//     VERBATIM (result columns included); its FIRST statement is the 1 s lock_timeout (00054's
//     rule), its second tenants' SHARE ROW EXCLUSIVE lock, its third the refusal (F1, 3rd round --
//     the lock before the check; its behaviour is
//     TestOperator00063_TheDownLocksOutTenantWritesBeforeItLooksForASuspension's) and its LAST the
//     tenants DDL (ADR 0025 limit 9); the Up's first statement is the same lock_timeout; the Down
//     holds no REVOKE ALL; its GRANTs are exactly the two re-created reads' EXECUTE for
//     tappa_operator; its column REVOKEs are exactly the Up's two column GRANTs, and its two other
//     REVOKEs take the re-created reads' EXECUTE from PUBLIC; the Up's REVOKE ALLs are function
//     REVOKEs from PUBLIC only.
//   - THE SETS AND THE CONDITIONS, WHOLE: the Up's audit condition and two kind CHECKs name the
//     twenty-two, the Down's 00042's twenty (derived from 00042's file); each condition is read to
//     its end and compared whole.
//   - Down gives every body back, drops the two writes, the two columns and their CHECKs, takes
//     back exactly the column grants (00029's, 00032's, 00034's, 00039's and 00042's tenants
//     columns stay: the overview still reads in 00029's columns, while the shipped statement --
//     00063's columns -- is 42703), puts the tenant-write shape (00039's three kinds) and the kind
//     CHECK back; after it 'tenant_suspended' is refused by op_begin_read's filter (22023) and by
//     the kind CHECK (23514). Up takes it all again.
//   - THE REFUSAL (K15-6): with a tenant suspended the Down is 55000 naming 00063 and nothing
//     runs; once the operator reinstated it, the Down runs.
//   - NOT VALID, branch by branch: nothing outside -> VALIDATED; a suspension's rows (suspended and
//     reinstated: the rows stay, the tenant is not suspended) -> the kind CHECK NOT VALID, the
//     rows STAY; a later migration's kinds -> the Down NOT VALID and the Up NOT VALID too. The chain
//     63 -> 42 runs without a 23514 with this file's rows present (00042's Down leaves 00039's set
//     NOT VALID; the tables those two Downs name locked first, as opAtVersion does).
//   - LIMIT 9, MEASURED AND PINNED: after the Down this session holds ACCESS EXCLUSIVE on tenants,
//     and another session's ACCESS SHARE on tenants -- what a plain SELECT takes -- is NOT granted
//     at once (55P03 NOWAIT), while its ROW EXCLUSIVE on audit_log is; the Up's duration (its CHECK
//     validation scans tenants) is logged.
func TestOperator00063_DownGivesBack00062AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 63, opKindsAt42...)
	up, down := opMigrationSections(t, op00063File)
	up42, down42 := opMigrationSections(t, op00042File)
	up29, _ := opMigrationSections(t, op00029File)
	up57, _ := opMigrationSections(t, op00057File)
	begin42, _ := opBodyOf(t, up42, "CREATE OR REPLACE FUNCTION", "op_begin_read")
	audit42, _ := opBodyOf(t, up42, "CREATE OR REPLACE FUNCTION", "op_read_audit")
	begin63, _ := opBodyOf(t, up, "CREATE OR REPLACE FUNCTION", "op_begin_read")
	audit63, _ := opBodyOf(t, up, "CREATE OR REPLACE FUNCTION", "op_read_audit")
	tenants29, tenants29Stmt := opBodyOf(t, up29, "CREATE FUNCTION", "op_read_tenants")
	detail57, detail57Stmt := opBodyOf(t, up57, "CREATE FUNCTION", "op_read_tenant_detail")
	tenants63, _ := opBodyOf(t, up, "CREATE FUNCTION", "op_read_tenants")
	detail63, _ := opBodyOf(t, up, "CREATE FUNCTION", "op_read_tenant_detail")
	if begin42 == begin63 || audit42 == audit63 || tenants29 == tenants63 || detail57 == detail63 {
		t.Fatal("PREMISE: a replaced body is the same text as the one it replaces")
	}
	for _, c := range []struct{ fn, create, want string }{
		{"op_begin_read", "CREATE OR REPLACE FUNCTION", begin42},
		{"op_read_audit", "CREATE OR REPLACE FUNCTION", audit42},
	} {
		if got, _ := opBodyOf(t, down, c.create, c.fn); got != c.want {
			t.Errorf("00063's Down does not give %s 00042's Up body verbatim", c.fn)
		}
	}
	for _, c := range []struct{ fn, from, want string }{
		{"op_read_tenants", "00029", tenants29Stmt},
		{"op_read_tenant_detail", "00057", detail57Stmt},
	} {
		if _, got := opBodyOf(t, down, "CREATE FUNCTION", c.fn); got != c.want {
			t.Errorf("00063's Down does not give %s %s's whole definition verbatim", c.fn, c.from)
		}
	}

	// THE STATEMENTS of the Down: first the lock_timeout, then the tenants lock, then the refusal,
	// last the tenants DDL; no REVOKE ALL; the GRANTs and REVOKEs, named. The Up opens with the
	// same lock_timeout.
	downStatements, upStatements := opStatementsOnly(down), opStatementsOnly(up)
	if !strings.HasPrefix(strings.TrimSpace(upStatements), opMigrationLockTimeout+"\n") {
		t.Errorf("00063's Up does not open with %q (00054's rule)", opMigrationLockTimeout)
	}
	if i, j := strings.Index(downStatements, "IF EXISTS (SELECT 1 FROM public.tenants AS t"), strings.Index(downStatements, "DROP FUNCTION"); i < 0 || j < 0 || i > j {
		t.Error("00063's Down does not open with its refusal (the suspension check before the first DROP)")
	}
	afterTimeout, timed := strings.CutPrefix(strings.TrimSpace(downStatements), opMigrationLockTimeout)
	afterLock, locked := strings.CutPrefix(strings.TrimSpace(afterTimeout), opDownTenantsLock)
	if !timed || !locked || strings.Count(downStatements, "LOCK TABLE") != 1 || strings.Count(downStatements, "lock_timeout") != 1 ||
		!strings.HasPrefix(strings.TrimSpace(afterLock), "DO $$\nBEGIN\n    IF EXISTS (SELECT 1 FROM public.tenants AS t") {
		t.Errorf("00063's Down does not open with %q, then %q and, next, its refusal (F1: the lock before the check)", opMigrationLockTimeout, opDownTenantsLock)
	}
	if n := strings.Count(downStatements, "ALTER TABLE tenants"); n != 1 || !strings.HasSuffix(strings.TrimSpace(downStatements), "DROP COLUMN suspended_at;") {
		t.Errorf("00063's Down does not END with its one tenants DDL (%d ALTER TABLE tenants)", n)
	}
	if regexp.MustCompile(`(?i)\bREVOKE\s+ALL\b`).MatchString(downStatements) {
		t.Error("00063's Down holds a REVOKE ALL")
	}
	for _, m := range regexp.MustCompile(`(?mi)^REVOKE ALL\b.*$`).FindAllString(upStatements, -1) {
		if !regexp.MustCompile(`^REVOKE ALL ON FUNCTION public\.op_[a-z_]+\([a-z, ]*\) FROM PUBLIC;$`).MatchString(m) {
			t.Errorf("00063's Up holds a REVOKE ALL that is not a function's from PUBLIC: %s", m)
		}
	}
	grants := regexp.MustCompile(`(?mi)^GRANT\b.*$`).FindAllString(downStatements, -1)
	sort.Strings(grants)
	if want := []string{
		"GRANT EXECUTE ON FUNCTION public.op_read_tenant_detail(text, text, uuid) TO tappa_operator;",
		"GRANT EXECUTE ON FUNCTION public.op_read_tenants(text, text, text, integer, integer) TO tappa_operator;",
	}; !slices.Equal(grants, want) {
		t.Errorf("00063's Down GRANTs %v, want exactly the two re-created reads' EXECUTE %v", grants, want)
	}
	colGrant := regexp.MustCompile(`(?m)^GRANT (SELECT|UPDATE) \(([^)]*)\) ON (tenants|audit_log) TO tappa_opdefiner;$`)
	colRevoke := regexp.MustCompile(`(?m)^REVOKE (SELECT|UPDATE) \(([^)]*)\) ON (tenants|audit_log) FROM tappa_opdefiner;$`)
	set := func(re *regexp.Regexp, text string) []string {
		var out []string
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			out = append(out, m[1]+" "+m[3]+" ("+strings.Join(strings.Fields(m[2]), " ")+")")
		}
		sort.Strings(out)
		return out
	}
	if g, r := set(colGrant, upStatements), set(colRevoke, downStatements); len(g) != 2 || !slices.Equal(g, r) {
		t.Errorf("the Up grants %v and the Down revokes %v; want the same two column grants", g, r)
	}
	revokes := regexp.MustCompile(`(?mi)^REVOKE\b.*$`).FindAllString(downStatements, -1)
	fnRevokes := regexp.MustCompile(`(?m)^REVOKE EXECUTE ON FUNCTION public\.op_read_(tenants|tenant_detail)\([a-z, ]*\) FROM PUBLIC;$`).FindAllString(downStatements, -1)
	if len(revokes) != 4 || len(fnRevokes) != 2 {
		t.Errorf("00063's Down holds %d REVOKE(s), %d of them the re-created reads' EXECUTE from PUBLIC; want 4 and 2", len(revokes), len(fnRevokes))
	}

	// THE SETS, from 00042's file, and every condition and CHECK of both halves.
	quotedList := func(text, re string) []string {
		m := regexp.MustCompile(`(?s)` + re).FindStringSubmatch(text)
		if m == nil {
			return nil
		}
		return opQuotedList(strings.Join(strings.Fields(m[1]), " "))
	}
	if got := quotedList(up42, `ADD CONSTRAINT operator_audit_log_kind_check\s+CHECK \(kind IN \(([^)]*)\)\);`); !slices.Equal(got, opAuditKinds42) {
		t.Fatalf("00042's audit kind set is %v, want %v", got, opAuditKinds42)
	}
	part := func(section string) string {
		const drop = "ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check"
		i := strings.Index(section, drop)
		if i < 0 {
			t.Fatalf("the section does not hold %q", drop)
		}
		rest := section[i:]
		if j := strings.Index(rest, "-- +goose StatementEnd"); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	whole := func(set []string) string { return "kind <> ALL (ARRAY['" + strings.Join(set, "', '") + "'])" }
	for _, c := range []struct {
		what, text string
		set        []string
	}{
		{"00063's Up", part(up), opAuditKinds},
		{"00063's Down", part(down), opAuditKinds42},
	} {
		var conds []string
		for _, mm := range regexp.MustCompile(`(?s)IF EXISTS \(SELECT 1 FROM public\.operator_audit_log\s+WHERE (.*?)\) THEN`).FindAllStringSubmatch(c.text, -1) {
			conds = append(conds, strings.Join(strings.Fields(mm[1]), " "))
		}
		if len(conds) != 1 || conds[0] != whole(c.set) {
			t.Errorf("%s: the NOT VALID condition is %q; want exactly one, whole: %q", c.what, conds, whole(c.set))
		}
		var lists [][]string
		for _, mm := range regexp.MustCompile(`(?s)operator_audit_log_kind_check\s+CHECK \(kind IN \(([^)]*)\)\)`).FindAllStringSubmatch(c.text, -1) {
			lists = append(lists, opQuotedList(strings.Join(strings.Fields(mm[1]), " ")))
		}
		if len(lists) != 2 || !slices.Equal(lists[0], c.set) || !slices.Equal(lists[1], c.set) {
			t.Errorf("%s: the CHECKs name %v, want %v twice (NOT VALID and VALIDATED)", c.what, lists, c.set)
		}
	}

	notValid := func(def string, nv bool) string {
		if nv {
			return def + " NOT VALID"
		}
		return def
	}
	at63 := func(s opSuspSchemaState, when string, kindsNV bool) {
		t.Helper()
		if s.writes != 2 || s.begin != begin63 || s.audit != audit63 || s.tenantsBody != tenants63 || s.detailBody != detail63 ||
			s.tenantsResult != opReadTenantsResult63 || s.detailResult != opReadDetailResult63 ||
			s.columns != 2 || s.checks != 2 || !s.readsOwned || !s.readsOperatorX || s.readsForeignX ||
			s.kinds != notValid(opKindCheckDef(opAuditKinds), kindsNV) || s.kindsValid == kindsNV ||
			s.shape != opTenantWriteShapeDefOf(opTenantWriteKinds) || !s.shapeValid ||
			s.tenantsSel != opDefinerTenantsSel63 || s.tenantsUpd != opDefinerTenantsUpd63 {
			t.Errorf("%s: writes=%d bodies are 00063's=%v/%v/%v/%v results=%q/%q columns=%d checks=%d owned=%v operator=%v foreign=%v\n kinds=%s (%v)\n shape=%s (%v)\n definer SELECT=(%s) UPDATE=(%s)\n want 00063's state, kinds NOT VALID=%v",
				when, s.writes, s.begin == begin63, s.audit == audit63, s.tenantsBody == tenants63, s.detailBody == detail63,
				s.tenantsResult, s.detailResult, s.columns, s.checks, s.readsOwned, s.readsOperatorX, s.readsForeignX,
				s.kinds, s.kindsValid, s.shape, s.shapeValid, s.tenantsSel, s.tenantsUpd, kindsNV)
		}
	}
	at62 := func(s opSuspSchemaState, when string, kindsNV bool) {
		t.Helper()
		if s.writes != 0 || s.begin != begin42 || s.audit != audit42 || s.tenantsBody != tenants29 || s.detailBody != detail57 ||
			s.tenantsResult != opReadTenantsResult29 || s.detailResult != opReadDetailResult57 ||
			s.columns != 0 || s.checks != 0 || !s.readsOwned || !s.readsOperatorX || s.readsForeignX ||
			s.kinds != notValid(opKindCheckDef(opAuditKinds42), kindsNV) || s.kindsValid == kindsNV ||
			s.shape != opTenantWriteShapeDefOf(opTenantWriteKinds39) || !s.shapeValid ||
			s.tenantsSel != opDefinerTenantsSel42 || s.tenantsUpd != opDefinerTenantsUpd39 {
			t.Errorf("%s: writes=%d bodies are 00042's/00029's/00057's=%v/%v/%v/%v results=%q/%q columns=%d checks=%d owned=%v operator=%v foreign=%v\n kinds=%s (%v)\n shape=%s (%v)\n definer SELECT=(%s) UPDATE=(%s)\n want 00062's state, kinds NOT VALID=%v",
				when, s.writes, s.begin == begin42, s.audit == audit42, s.tenantsBody == tenants29, s.detailBody == detail57,
				s.tenantsResult, s.detailResult, s.columns, s.checks, s.readsOwned, s.readsOperatorX, s.readsForeignX,
				s.kinds, s.kindsValid, s.shape, s.shapeValid, s.tenantsSel, s.tenantsUpd, kindsNV)
		}
	}
	at63(opSuspReadSchema(t, ctx, tx), "PREMISE before Down", false)

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := opSuspTenant(t, ctx, tx, "op15 down "+opToken(t), "", "")
	overview := &opFixtureTenant{name: "op15 down overview " + opToken(t), locations: 1, activeEmp: 1}
	overview.id = opNewTenant(t, ctx, tx, overview.name, 0)
	opPopulate(t, ctx, tx, overview)
	xact := opCommittedXact(t, ctx)
	// branch opens a savepoint holding no audit row of a kind outside 00042's set (the append-only
	// trigger disabled inside the savepoint for the delete, as 00034's Down test does) and no
	// suspended tenant (opAtVersion's step, for a committed one a branch did not write).
	branch := func() pgx.Tx {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`ALTER TABLE operator_audit_log DISABLE TRIGGER operator_audit_log_append_only`, nil},
			{`DELETE FROM operator_audit_log WHERE kind <> ALL ($1)`, []any{opAuditKinds42}},
			{`ALTER TABLE operator_audit_log ENABLE TRIGGER operator_audit_log_append_only`, nil},
			{`UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE suspended_at IS NOT NULL`, nil},
		} {
			if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
				t.Fatalf("clear what 00062 does not know, inside the transaction: %v", err)
			}
		}
		return sp
	}
	done := func(sp pgx.Tx) {
		t.Helper()
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	runUp := func(sp pgx.Tx, what string) {
		t.Helper()
		start := time.Now()
		opRunSection(t, ctx, sp, up, what)
		t.Logf("OP15-UP: %s took %v (the CHECKs' validation scans tenants)", what, time.Since(start).Round(time.Millisecond))
	}

	// Branch 1: nothing outside -> VALIDATED; what Down took away; the lock it holds; Up again.
	sp := branch()
	heldBefore := opSuspHeld(t, ctx, sp)
	opRunSection(t, ctx, sp, down, "00063 Down with nothing outside the previous sets")
	heldAfter := opSuspHeld(t, ctx, sp)
	t.Logf("OP15-LIMIT9-LOCK: this session's locks before the Down %v, after it %v", heldBefore, heldAfter)
	if !slices.Contains(heldAfter, "tenants:AccessExclusiveLock") {
		t.Errorf("COUNTED LIMIT moved: after the Down this session holds %v on tenants; measured was AccessExclusiveLock -- update ADR 0021's OP-15 note", heldAfter)
	}
	other, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("a second owner connection: %v", err)
	}
	probeLock := func(sql string) error {
		t.Helper()
		otx, err := other.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, locked := otx.Exec(ctx, sql)
		if err := otx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return locked
	}
	tenantsProbe := probeLock(`LOCK TABLE public.tenants IN ACCESS SHARE MODE NOWAIT`)
	auditProbe := probeLock(`LOCK TABLE public.audit_log IN ROW EXCLUSIVE MODE NOWAIT`)
	if err := other.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("OP15-LIMIT9-LOCK: another session's ACCESS SHARE on tenants: %v; its ROW EXCLUSIVE on audit_log: %v", tenantsProbe, auditProbe)
	if code, _ := opCode(tenantsProbe); code != sqlstateLockNotAvailable {
		t.Errorf("COUNTED LIMIT moved: another session's ACCESS SHARE on tenants after the Down answered %v; measured was 55P03 -- update ADR 0021's OP-15 note", tenantsProbe)
	}
	if auditProbe != nil {
		t.Errorf("after the Down another session's ROW EXCLUSIVE on audit_log was not granted at once: %v", auditProbe)
	}
	at62(opSuspReadSchema(t, ctx, sp), "after Down (nothing outside)", false)
	_, err = opBegin(t, ctx, sp, hash, operatorAuditReadKind, opLogParams("tenant_suspended", 1, 50))
	opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "after Down, an audit read filtered to 'tenant_suspended'")
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) VALUES ('tenant_suspended', $1, $2, $3)`,
		session, a.id, f.id), sqlstateCheckViolation, "after Down, a 'tenant_suspended' row")
	if got, err := opReadDetail(t, ctx, sp, hash, opForgeDetail(t, ctx, sp, session, a.id, overview.id, xact), overview.id); err != nil ||
		len(got) != 1 || got[0].ActiveEmployees != 1 || got[0].Locations != 1 {
		t.Errorf("after Down the overview reads %d row(s) in 00029's columns, err %v; want one row counting 1 employee and 1 location", len(got), err)
	}
	_, err = opReadDetailHead(t, ctx, sp, hash, opForgeDetail(t, ctx, sp, session, a.id, overview.id, xact), overview.id)
	opWant(t, err, "42703", "after Down, the SHIPPED overview statement (00063's columns)")
	runUp(sp, "00063 Up again")
	at63(opSuspReadSchema(t, ctx, sp), "after Up again", false)
	done(sp)

	// Branch 2: a suspension's rows (suspended, then reinstated) -> the kind CHECK NOT VALID; the
	// rows STAY.
	sp = branch()
	if err := opSuspend(t, ctx, sp, hash, f.id, "non_payment"); err != nil {
		t.Fatalf("a suspension before Down: %v", err)
	}
	if err := opReinstate(t, ctx, sp, hash, f.id); err != nil {
		t.Fatalf("a reinstatement before Down: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00063 Down with a suspension's rows present")
	at62(opSuspReadSchema(t, ctx, sp), "after Down (a suspension's rows)", true)
	if opSuspOperatorRows(t, ctx, sp, "tenant_suspended", session, f.id) != 1 || opSuspOperatorRows(t, ctx, sp, "tenant_reinstated", session, f.id) != 1 ||
		len(opTenantTrail(t, ctx, sp, f.id, opSuspendAction)) != 1 || len(opTenantTrail(t, ctx, sp, f.id, opReinstateAction)) != 1 {
		t.Error("after Down a suspension's operator row or tenant row is gone; a Down takes a door away, not a fact")
	}
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) VALUES ('tenant_reinstated', $1, $2, $3)`,
		session, a.id, f.id), sqlstateCheckViolation, "a NEW 'tenant_reinstated' row after Down (NOT VALID still binds new rows)")
	runUp(sp, "00063 Up again over a suspension's rows")
	at63(opSuspReadSchema(t, ctx, sp), "after Up again (a suspension's rows)", false)
	done(sp)

	// Branch 3: a SUSPENDED tenant -> the Down refuses (K15-6); reinstated -> it runs.
	sp = branch()
	if err := opSuspend(t, ctx, sp, hash, f.id, "security_hold"); err != nil {
		t.Fatalf("a suspension before Down: %v", err)
	}
	inner, err := sp.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, downErr := inner.Conn().PgConn().Exec(ctx, down).ReadAll()
	code, msg := opCode(downErr)
	if code != sqlstatePrerequisiteState || !strings.Contains(msg, "00063") || !strings.Contains(msg, "a tenant is suspended") {
		t.Errorf("the Down with a suspended tenant answered %q %q; want 55000 naming 00063 and the suspension", code, msg)
	}
	if strings.Contains(msg, f.id.String()) {
		t.Error("the Down's refusal names the suspended tenant")
	}
	if err := inner.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	at63(opSuspReadSchema(t, ctx, sp), "after the refused Down", false)
	if s := opSuspOf(t, ctx, sp, f.id); s.at == nil || opDeref(s.reason) != "security_hold" {
		t.Error("after the refused Down the tenant is no longer suspended")
	}
	if err := opReinstate(t, ctx, sp, hash, f.id); err != nil {
		t.Fatalf("reinstate before Down: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00063 Down once the suspension is lifted")
	at62(opSuspReadSchema(t, ctx, sp), "after Down (the suspension lifted)", true)
	done(sp)

	// Branch 4: a LATER migration's kinds alone. Its Up widened the set, a row of its kind was
	// written, its Down put 00063's set back NOT VALID and left the row.
	sp = branch()
	for _, s := range []string{
		`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`,
		`ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN ('` + strings.Join(opAuditKinds, "', '") + `', 'zz_later_kind'))`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Up: %v", err)
		}
	}
	if _, err := sp.Exec(ctx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ('zz_later_kind', $1, $2)`, session, a.id); err != nil {
		t.Fatalf("a later migration's row: %v", err)
	}
	for _, s := range []string{
		`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`,
		`ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN ('` + strings.Join(opAuditKinds, "', '") + `')) NOT VALID`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Down: %v", err)
		}
	}
	opRunSection(t, ctx, sp, down, "00063 Down after a later migration's Down left its rows")
	at62(opSuspReadSchema(t, ctx, sp), "after Down (a later migration's kinds)", true)
	runUp(sp, "00063 Up again with a later migration's rows present (it composes)")
	at63(opSuspReadSchema(t, ctx, sp), "after Up again (a later migration's kinds)", true)
	done(sp)

	// The chain composes downward: 63 -> 42 with this file's rows present -- 00042's Down is the
	// next one that rewrites the kind CHECK, and it leaves 00039's set NOT VALID over them. Every
	// table the two Downs name is locked first (opAtVersion's lockForDowns), inside the branch.
	sp = branch()
	if err := opSuspend(t, ctx, sp, hash, f.id, "terms_breach"); err != nil {
		t.Fatalf("a suspension before the chain: %v", err)
	}
	if err := opReinstate(t, ctx, sp, hash, f.id); err != nil {
		t.Fatalf("a reinstatement before the chain: %v", err)
	}
	lockForDowns(t, ctx, sp, append(downTables(down), downTables(down42)...))
	opRunSection(t, ctx, sp, down, "00063 Down (chain)")
	opRunSection(t, ctx, sp, down42, "00042 Down after 00063's, this file's rows present")
	if def, valid := opConstraint(t, ctx, sp, "operator_audit_log", "operator_audit_log_kind_check"); valid ||
		def != notValid(opKindCheckDef(opAuditKinds39), true) {
		t.Errorf("after 63 -> 42 the audit kind CHECK is %s (validated %v); want 00039's set NOT VALID", def, valid)
	}
	done(sp)

	// And with nothing outside the sets, Down and Up once more: VALIDATED both ways.
	sp = branch()
	opRunSection(t, ctx, sp, down, "00063 Down")
	runUp(sp, "00063 Up")
	at63(opSuspReadSchema(t, ctx, sp), "after Down and Up", false)
	done(sp)
}

// opMigrationLockTimeout is the first statement of 00063's Up and Down (00054's rule,
// deploy/README.md), and opDownTenantsLock the Down's next (the third eye's F1, 3rd round).
const (
	opMigrationLockTimeout = "SET LOCAL lock_timeout = '1s';"
	opDownTenantsLock      = "LOCK TABLE public.tenants IN SHARE ROW EXCLUSIVE MODE;"
)

// TestOperator00063_TheDownLocksOutTenantWritesBeforeItLooksForASuspension is the third eye's F1
// (3rd round): the Down's refusal (K15-6) must still hold when the columns are dropped, so no
// suspension may commit between the check and the tenants DDL at the end. The Down's head -- its
// statements up to and including the refusal: the tenants lock and the check -- runs inside a
// savepoint of the test's transaction (rolled back; nothing commits), and another session asks:
//   - a suspension's UPDATE (no row: WHERE false) under a 300 ms lock_timeout -- it WAITS, then
//     55P03; without the lock the check holds ACCESS SHARE only and the UPDATE is granted at once;
//   - ROW EXCLUSIVE NOWAIT, what every tenants writer takes -- 55P03;
//   - THE NARROWNESS, CONTROLS: ACCESS SHARE (a tap's read) and ROW SHARE (a foreign-key check's
//     FOR KEY SHARE) NOWAIT are granted while the lock is held;
//   - CONTROLS around it: the same UPDATE is granted at once before the head runs and after the
//     savepoint is rolled back (the 55P03 is the head's lock, not the test's).
func TestOperator00063_TheDownLocksOutTenantWritesBeforeItLooksForASuspension(t *testing.T) {
	ctx, tx := opTx(t)
	_, down := opMigrationSections(t, op00063File)
	i := strings.Index(down, "IF EXISTS (SELECT 1 FROM public.tenants AS t")
	if i < 0 {
		t.Fatal("00063's Down holds no refusal")
	}
	j := strings.Index(down[i:], "-- +goose StatementEnd")
	if j < 0 {
		t.Fatal("00063's Down's refusal does not end")
	}
	head := down[:i+j]
	if strings.Contains(opStatementsOnly(head), "DROP ") || strings.Contains(opStatementsOnly(head), "ALTER ") {
		t.Fatal("PREMISE: the Down's head (up to its refusal) holds more than the lock and the check")
	}
	// opAtVersion's step: a committed suspension would make the head refuse; lifted inside the
	// transaction. This UPDATE's own ROW EXCLUSIVE does not conflict with the other session's.
	if _, err := tx.Exec(ctx, `UPDATE public.tenants SET suspended_at = NULL, suspension_reason = NULL
	                             WHERE suspended_at IS NOT NULL OR suspension_reason IS NOT NULL`); err != nil {
		t.Fatal(err)
	}

	other, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("a second owner connection: %v", err)
	}
	t.Cleanup(func() {
		if err := other.Close(context.Background()); err != nil {
			t.Logf("close the second connection: %v", err)
		}
	})
	const suspensionWrite = `UPDATE public.tenants SET suspended_at = NULL, suspension_reason = NULL WHERE false`
	ask := func(sql string, wait bool) (time.Duration, error) {
		t.Helper()
		otx, err := other.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if wait {
			if _, err := otx.Exec(ctx, `SET LOCAL lock_timeout = '300ms'`); err != nil {
				t.Fatal(err)
			}
		}
		start := time.Now()
		_, asked := otx.Exec(ctx, sql)
		took := time.Since(start)
		if err := otx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return took, asked
	}

	if _, err := ask(suspensionWrite, true); err != nil {
		t.Fatalf("CONTROL: before the Down's head ran, another session's suspension UPDATE was refused (%v)", err)
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	opRunSection(t, ctx, sp, head, "00063 Down's head (its lock_timeout, its lock and its refusal)")
	if held := opSuspHeld(t, ctx, sp); !slices.Contains(held, "tenants:ShareRowExclusiveLock") {
		t.Errorf("after the Down's head this session holds %v; want ShareRowExclusiveLock on tenants", held)
	}
	took, writeErr := ask(suspensionWrite, true)
	t.Logf("OP15-F1: with the Down's head run, another session's suspension UPDATE answered %v after %v", writeErr, took.Round(time.Millisecond))
	if code, _ := opCode(writeErr); code != sqlstateLockNotAvailable || took < 250*time.Millisecond {
		t.Errorf("another session's suspension UPDATE answered %v after %v while the Down's check held; want it to wait and get 55P03 -- "+
			"a suspension committed there would be dropped with the columns in silence", writeErr, took)
	}
	for _, c := range []struct {
		what, sql string
		granted   bool
	}{
		{"a tenants writer's ROW EXCLUSIVE", `LOCK TABLE public.tenants IN ROW EXCLUSIVE MODE NOWAIT`, false},
		{"a tap's read (ACCESS SHARE)", `LOCK TABLE public.tenants IN ACCESS SHARE MODE NOWAIT`, true},
		{"a foreign-key check's FOR KEY SHARE (ROW SHARE)", `LOCK TABLE public.tenants IN ROW SHARE MODE NOWAIT`, true},
	} {
		_, err := ask(c.sql, false)
		code, _ := opCode(err)
		switch {
		case c.granted && err != nil:
			t.Errorf("%s was refused while the Down's check held (%v); the lock is wider than the writers", c.what, err)
		case !c.granted && code != sqlstateLockNotAvailable:
			t.Errorf("%s answered %v while the Down's check held; want 55P03", c.what, err)
		}
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(suspensionWrite, true); err != nil {
		t.Errorf("CONTROL: after the savepoint's rollback another session's suspension UPDATE was refused (%v)", err)
	}
}

// TestOperator00063_PreconditionRefusesAWrongCluster: 00063's precondition refuses the role
// shapes 00026-00057's refuse (absent, over-privileged, joined by membership in either direction);
// a database that is not at 00062 on the operator surface -- one of the four functions it replaces
// missing or not the definer's, a read already returning another result (the list with the
// suspension; the overview at 00029's columns, 00057 walked back), a write, a column or a CHECK of
// this file already there, the audit kind CHECK or the tenant-write CHECK missing, narrower (without
// 00042's last kind, without 00039's) or already naming this file's kinds, and a replaced body
// changed by another migration (each of the four, by md5); and a DRIFTED reach this file's column
// grants could not narrow: the definer reaching more or less of tenants than 00034, 00039 and 00042
// left it (an extra or a missing SELECT or UPDATE column, a table-level UPDATE, an INSERT, a
// DELETE, a TRUNCATE) or of audit_log (a SELECT, an extra or a missing INSERT column, an UPDATE, a
// DELETE), and a table-level UPDATE or INSERT on tenants reaching tappa_app -- directly, through
// PUBLIC, through a role it inherits and through one it can only SET ROLE to -- or tappa_operator.
// Each with SQLSTATE 55000 naming 00063; the cluster this suite runs on (taken to 00062 first)
// passes, and so does one whose kind CHECK a later Down left NOT VALID (CONTROL: the suffix is
// read aside).
func TestOperator00063_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 62, opKindsAt42...)
	up, _ := opMigrationSections(t, op00063File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00063's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	_, down57 := opMigrationSections(t, op00057File)
	const kinds = `ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
	               ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN (%s)) NOT VALID`
	const shape = `ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_tenant_write_shape;
	               ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_tenant_write_shape CHECK (kind <> ALL (ARRAY[%s]) OR target_tenant_id IS NOT NULL)`
	quoted := func(set []string) string { return "'" + strings.Join(set, "', '") + "'" }
	// bodyDrift re-creates fn as the live body with one comment line added: what a migration
	// between 00062 and this one that changed only a body leaves.
	bodyDrift := func(sig string) string {
		var def string
		if err := tx.QueryRow(ctx, `SELECT pg_get_functiondef(to_regprocedure($1))`, sig).Scan(&def); err != nil {
			t.Fatalf("read %s: %v", sig, err)
		}
		if !strings.Contains(def, "\nAS $function$") {
			t.Fatalf("PREMISE: %s's definition has no $function$ body", sig)
		}
		return strings.Replace(def, "\nAS $function$", "\nAS $function$\n-- changed by a migration between 00062 and 00063", 1)
	}
	const state, constraints, body, reach = "00062's state", "as migrations 00042 and 00039 left them", "a live body is not that",
		"a privilege no migration gave"
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"roles present, at 00062", nil, ""},
		{"CONTROL: the kind CHECK left NOT VALID by a later Down", []string{fmtKinds(kinds, quoted(opAuditKinds42))}, ""},
		{"both roles absent", []string{`ALTER ROLE tappa_operator RENAME TO zz_op15_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op15_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner without BYPASSRLS", []string{`ALTER ROLE tappa_opdefiner NOBYPASSRLS`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_opdefiner is a member", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
		{"op_begin_read missing", []string{`ALTER FUNCTION public.op_begin_read(text, text, jsonb) RENAME TO zz_op15_was_begin_read`}, state},
		{"op_read_audit not the definer's", []string{`ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) OWNER TO tappa_owner`}, state},
		{"op_read_tenants not the definer's", []string{`ALTER FUNCTION public.op_read_tenants(text, text, text, integer, integer) OWNER TO tappa_owner`}, state},
		{"op_read_tenant_detail missing", []string{`ALTER FUNCTION public.op_read_tenant_detail(text, text, uuid) RENAME TO zz_op15_was_detail`}, state},
		{"op_read_tenants already returning the suspension", []string{
			`DROP FUNCTION public.op_read_tenants(text, text, text, integer, integer)`,
			`CREATE FUNCTION public.op_read_tenants(p_session text, p_ticket text, p_query text, p_page_number integer, p_page_size integer)
			   RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text, suspended_at timestamptz)
			   LANGUAGE sql AS 'SELECT NULL::uuid, NULL::text, NULL::timestamptz, NULL::text, NULL::timestamptz'`,
			`ALTER FUNCTION public.op_read_tenants(text, text, text, integer, integer) OWNER TO tappa_opdefiner`}, state},
		{"op_read_tenant_detail at 00029's columns (00057 walked back)", []string{down57}, state},
		{"op_suspend_tenant already there", []string{`CREATE FUNCTION public.op_suspend_tenant(p_session text, p_tenant_id uuid, p_reason text) RETURNS void LANGUAGE sql AS 'SELECT 1'`}, state},
		{"op_reinstate_tenant already there", []string{`CREATE FUNCTION public.op_reinstate_tenant(p_session text, p_tenant_id uuid) RETURNS void LANGUAGE sql AS 'SELECT 1'`}, state},
		{"suspended_at already there", []string{`ALTER TABLE tenants ADD COLUMN suspended_at timestamptz`}, state},
		{"suspension_reason already there", []string{`ALTER TABLE tenants ADD COLUMN suspension_reason text`}, state},
		{"this file's CHECK already there", []string{`ALTER TABLE tenants ADD CONSTRAINT tenants_suspension_reason_check CHECK (true)`}, state},
		{"the audit kind CHECK missing", []string{`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`}, constraints},
		{"the tenant-write CHECK missing", []string{`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_tenant_write_shape`}, constraints},
		{"the audit kind CHECK without the holding kinds (00039's set)", []string{fmtKinds(kinds, quoted(opAuditKinds39))}, constraints},
		{"the audit kind CHECK already naming tenant_suspended", []string{fmtKinds(kinds, quoted(append(slices.Clone(opAuditKinds42), "tenant_suspended")))}, constraints},
		{"the audit kind CHECK already naming tenant_reinstated", []string{fmtKinds(kinds, quoted(append(slices.Clone(opAuditKinds42), "tenant_reinstated")))}, constraints},
		{"the tenant-write CHECK without the API kinds (00034's)", []string{fmtKinds(shape, "'tenant_vat_checked'")}, constraints},
		{"the tenant-write CHECK already naming tenant_suspended", []string{fmtKinds(shape, quoted(append(slices.Clone(opTenantWriteKinds39), "tenant_suspended")))}, constraints},
		{"the tenant-write CHECK already naming tenant_reinstated", []string{fmtKinds(shape, quoted(append(slices.Clone(opTenantWriteKinds39), "tenant_reinstated")))}, constraints},
		{"op_begin_read's body changed", []string{bodyDrift("public.op_begin_read(text, text, jsonb)")}, body},
		{"op_read_audit's body changed", []string{bodyDrift("public.op_read_audit(text, text, text, integer, integer)")}, body},
		{"op_read_tenants' body changed", []string{bodyDrift("public.op_read_tenants(text, text, text, integer, integer)")}, body},
		{"op_read_tenant_detail's body changed", []string{bodyDrift("public.op_read_tenant_detail(text, text, uuid)")}, body},
		{"the definer may SELECT structure", []string{`GRANT SELECT (structure) ON tenants TO tappa_opdefiner`}, reach},
		{"the definer lost a SELECT 00029 gave", []string{`REVOKE SELECT (plan) ON tenants FROM tappa_opdefiner`}, reach},
		{"the definer lost the SELECT 00042 gave", []string{`REVOKE SELECT (api_code) ON tenants FROM tappa_opdefiner`}, reach},
		{"the definer may UPDATE the name", []string{`GRANT UPDATE (name) ON tenants TO tappa_opdefiner`}, reach},
		{"the definer lost the UPDATE 00034 gave", []string{`REVOKE UPDATE (vat_verified) ON tenants FROM tappa_opdefiner`}, reach},
		{"the definer lost an UPDATE 00039 gave", []string{`REVOKE UPDATE (api_code) ON tenants FROM tappa_opdefiner`}, reach},
		{"the definer may UPDATE every tenant column", []string{`GRANT UPDATE ON tenants TO tappa_opdefiner`}, reach},
		{"the definer may INSERT a tenant", []string{`GRANT INSERT (name) ON tenants TO tappa_opdefiner`}, reach},
		{"the definer may DELETE a tenant", []string{`GRANT DELETE ON tenants TO tappa_opdefiner`}, reach},
		{"the definer may TRUNCATE tenants", []string{`GRANT TRUNCATE ON tenants TO tappa_opdefiner`}, reach},
		{"the definer may SELECT audit_log", []string{`GRANT SELECT (action) ON audit_log TO tappa_opdefiner`}, reach},
		{"the definer may INSERT an audit row's id", []string{`GRANT INSERT (id) ON audit_log TO tappa_opdefiner`}, reach},
		{"the definer lost the audit row's time", []string{`REVOKE INSERT (at) ON audit_log FROM tappa_opdefiner`}, reach},
		{"the definer may UPDATE audit_log", []string{`GRANT UPDATE (detail) ON audit_log TO tappa_opdefiner`}, reach},
		{"the definer may DELETE from audit_log", []string{`GRANT DELETE ON audit_log TO tappa_opdefiner`}, reach},
		{"tappa_app may UPDATE every tenant column", []string{`GRANT UPDATE ON tenants TO tappa_app`}, reach},
		{"tappa_app may INSERT every tenant column", []string{`GRANT INSERT ON tenants TO tappa_app`}, reach},
		{"PUBLIC may UPDATE every tenant column", []string{`GRANT UPDATE ON tenants TO PUBLIC`}, reach},
		{"tappa_app inherits a table-level UPDATE", []string{`GRANT UPDATE ON tenants TO tappa_resolver`, `GRANT tappa_resolver TO tappa_app`}, reach},
		{"tappa_app may SET ROLE to a table-level INSERT", []string{`GRANT INSERT ON tenants TO tappa_resolver`,
			`GRANT tappa_resolver TO tappa_app WITH INHERIT FALSE`}, reach},
		{"tappa_operator may UPDATE every tenant column", []string{`GRANT UPDATE ON tenants TO tappa_operator`}, reach},
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
		case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00063")):
			t.Errorf("%s: precondition answered %q %q, want %s naming 00063 and containing %q", c.what, code, msg, sqlstatePrerequisiteState, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("%s: rollback: %v", c.what, err)
		}
	}
}

// ----------------------------------------------------------- without a database --

// TestSuspensionReasons_TheTypedConstantsAreTheList pins, by TYPE-CHECKING package db's product
// source (operatoraudit_test.go's reader), that the package-level constants of type
// SuspensionReason hold exactly opSuspensionReasons' values, each once; that SuspensionReasons()
// returns that list in the CHECK's order and a copy; and that Known() is true for each and false
// for the empty value, another case, a trailing space, a hyphen and a word no migration names.
// The list itself is held to the database by TestOperator00063_TheReasonSetIsOneSetInEveryCopy.
func TestSuspensionReasons_TheTypedConstantsAreTheList(t *testing.T) {
	fset, files := opDBPackageFiles(t)
	got, err := opTypedConstValues(fset, files, importer.ForCompiler(fset, "source", nil), "SuspensionReason")
	if err != nil {
		t.Fatalf("package db: %v", err)
	}
	if v := opSortedValues(got["SuspensionReason"]); !slices.Equal(v, opSorted(opSuspensionReasons)) || len(got["SuspensionReason"]) != len(opSuspensionReasons) {
		t.Errorf("the SuspensionReason constants hold %v (%d constants), want %v once each", v, len(got["SuspensionReason"]), opSuspensionReasons)
	}
	var list []string
	for _, r := range SuspensionReasons() {
		list = append(list, string(r))
		if !r.Known() {
			t.Errorf("Known(%q) = false", r)
		}
	}
	if !slices.Equal(list, opSuspensionReasons) {
		t.Errorf("SuspensionReasons() = %v, want %v", list, opSuspensionReasons)
	}
	for _, r := range []SuspensionReason{"", "Non_payment", "non_payment ", "non-payment", "other", "suspended", "security_hold\x00"} {
		if r.Known() {
			t.Errorf("Known(%q) = true", r)
		}
	}
	l := SuspensionReasons()
	l[0] = "zz"
	if SuspensionReasons()[0] != SuspensionNonPayment {
		t.Error("SuspensionReasons hands out the array itself")
	}
	if _, err := os.Stat(filepath.Join("..", "..", "db", "migrations", op00063File)); err != nil {
		t.Errorf("the migration this file tests is not there: %v", err)
	}
}

// opCountingConn is an OperatorConn that records every statement it is handed and answers Exec
// with err -- for the accessors' Go-side half, without a database.
type opCountingConn struct {
	calls []string
	args  [][]any
	err   error
}

func (c *opCountingConn) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.calls = append(c.calls, sql)
	c.args = append(c.args, args)
	return pgconn.NewCommandTag("SELECT 1"), c.err
}

func (c *opCountingConn) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	c.calls = append(c.calls, sql)
	return nil
}

func (c *opCountingConn) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	c.calls = append(c.calls, sql)
	return nil, errors.New("opCountingConn: no rows")
}

// TestSuspendTenant_RefusesAnUnknownReasonWithoutARoundTrip is the Go side of the reason set and
// of the two accessors' contract, without a database:
//   - SuspendTenant with a reason outside the closed set answers ErrSuspensionReasonRefused and
//     hands the connection NOTHING (the value has no business in a statement);
//   - with each reason of the set it sends exactly ONE statement, suspendTenantSQL, with the
//     session hash, the tenant and the reason's code as its three bound arguments; ReinstateTenant
//     sends exactly reinstateTenantSQL with the hash and the tenant;
//   - the answer is operatorErr's: a 28000 is ErrOperatorRefused, another SQLSTATE an error naming
//     the code and carrying neither the server's DETAIL nor the PgError (errors.As cannot reach it).
func TestSuspendTenant_RefusesAnUnknownReasonWithoutARoundTrip(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	for _, r := range []SuspensionReason{"", "other", "NON_PAYMENT", "non_payment ", "security_hold\x00"} {
		c := &opCountingConn{}
		if err := SuspendTenant(ctx, c, "hash", id, r); !errors.Is(err, ErrSuspensionReasonRefused) || len(c.calls) != 0 {
			t.Errorf("SuspendTenant(%q): %v with %d statement(s); want ErrSuspensionReasonRefused and none", r, err, len(c.calls))
		}
	}
	for _, r := range SuspensionReasons() {
		c := &opCountingConn{}
		if err := SuspendTenant(ctx, c, "hash", id, r); err != nil || len(c.calls) != 1 || c.calls[0] != suspendTenantSQL ||
			len(c.args[0]) != 3 || c.args[0][0] != "hash" || c.args[0][1] != id || c.args[0][2] != string(r) {
			t.Errorf("SuspendTenant(%q): %v, statements %q, arguments %v", r, err, c.calls, c.args)
		}
	}
	c := &opCountingConn{}
	if err := ReinstateTenant(ctx, c, "hash", id); err != nil || len(c.calls) != 1 || c.calls[0] != reinstateTenantSQL ||
		len(c.args[0]) != 2 || c.args[0][0] != "hash" || c.args[0][1] != id {
		t.Errorf("ReinstateTenant: %v, statements %q, arguments %v", err, c.calls, c.args)
	}
	for _, call := range []func(OperatorConn) error{
		func(c OperatorConn) error { return SuspendTenant(ctx, c, "hash", id, SuspensionTermsBreach) },
		func(c OperatorConn) error { return ReinstateTenant(ctx, c, "hash", id) },
	} {
		if err := call(&opCountingConn{err: &pgconn.PgError{Code: "28000", Message: "refused"}}); !errors.Is(err, ErrOperatorRefused) {
			t.Errorf("a 28000 answered %v, want ErrOperatorRefused", err)
		}
		err := call(&opCountingConn{err: &pgconn.PgError{Code: "22023", Message: "m", Detail: "SECRET-DETAIL"}})
		var pg *pgconn.PgError
		if err == nil || !strings.Contains(err.Error(), "SQLSTATE 22023") || strings.Contains(err.Error(), "SECRET-DETAIL") || errors.As(err, &pg) {
			t.Errorf("a 22023 answered %v; want an error naming the code, without the DETAIL or the PgError", err)
		}
	}
}
