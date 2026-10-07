package db

// operatorvat_test.go -- migration 00034 (M10 OP-16, phase A): the operator reads ONE tenant's
// VAT number and verdict through op_read_tenant_vat, and records a VIES verdict through
// op_record_vat_check -- the FIRST op_* that CHANGES a tenant: its verdict, a row in that
// tenant's audit_log (K6) and a row in the operator's log, in one call. The catalogue half
// (signatures, the five copies of the audit kind set and the read kind's copies, the grants --
// the definer writes the verdict and NOT the number --, the tenant-write CHECK, the Down, the
// precondition, the temp-table shadow) and the behaviour half (op_begin_read's new kind, the
// read, the write: the NULL verdict, the number binding, B14, the exact before/after, the wall
// clock, one transaction, the belt) of ADR 0021 §6's list, for the two new op_* (the OP-16 card,
// md. 5 A6).
//
// HOW EACH TEST TOUCHES THE SHARED DATABASE -- operatorplaques_test.go's three shapes:
//   - most tests run in opTx: one rolled-back REPEATABLE READ transaction as the owner,
//     identities switched with SET LOCAL SESSION AUTHORIZATION, the operator-tables lock taken
//     EXCLUSIVE. Every tenant they need is written inside that transaction and goes with it.
//     🔴 NO TEST HERE COMMITS A TENANT, A VERDICT, A TENANT AUDIT ROW OR A 'tenant_vat_checked'
//     ROW: every call of the write runs inside a transaction that is rolled back;
//   - the read side is reached WITHOUT a commit through a ticket the owner writes with
//     created_xact naming a transaction that really committed (opCommittedXact) and a hash
//     computed HERE, in Go (opDetailTicketHash: the VAT read binds the same {tenant_id} text the
//     overview and the inventory do -- the KIND tells them apart);
//   - two tests need a COMMIT because a commit is their subject
//     (TestOpReadTenantVAT_TwoPhaseLifecycle, TestTenantVAT_OnThePoolTheTwoPhasesAreTwoTransactions).
//     They take the lock SHARED through opLiveFixture and commit READS only: per run, one
//     disabled account each, its revoked sessions and the 'read' rows they committed (scope
//     'tenant_vat'); no tenant, no verdict, no write row.
//
// ROUND 3: the write is bound to a COMMITTED read of the tenant by the same session (00034
// section 7 (b2)). Every test that wants a call past the binding gives it one first through
// opVATReadFor -- the same forged, never-committed read the read side's tests use (a ticket whose
// created_xact names a transaction that really committed) -- so no test commits more than
// before; TestOpRecordVATCheck_IsBoundToACommittedReadOfTheTenant measures the binding itself.
//
// 🔴 NO VAT NUMBER IS PRINTED. The fixtures' numbers are synthetic, but the pool test reads a
// committed tenant's real number: no failure message here carries a number -- only whether two
// values are equal (the orchestrator's K16-4).
//
// THE LOCKS (the OP-16 card's T9): the tests that run 00034's Down (the Down test, the
// precondition test, and every earlier migration's Down test through opAtVersion) revoke column
// grants on tenants and audit_log inside their transaction. TestOperator00034_DownGivesBack00033AndUpTakesItAgain
// measures what that holds: another session's ROW EXCLUSIVE on the two tables -- what an INSERT
// or an UPDATE takes -- is granted at once.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// op00034Functions are the two functions 00034 creates, by their exact catalogue identity: the
// read takes the session, the RAW ticket and the tenant; the write the session, the tenant,
// the number and the verdict -- no actor -- and returns void (ADR 0021 §2 v 7).
var op00034Functions = map[string]struct{ args, result string }{
	"op_read_tenant_vat": {"p_session text, p_ticket text, p_tenant_id uuid",
		"TABLE(tenant_id uuid, tenant_name text, vat_number text, vat_verified boolean, vat_checked_at timestamp with time zone)"},
	"op_record_vat_check": {"p_session text, p_tenant_id uuid, p_vat_number text, p_valid boolean", "void"},
}

const (
	op00034File = "00034_recheck_a_tenants_vat_from_the_operator.sql"

	tenantVATRefusal = "op_read_tenant_vat: read refused"
	vatCheckRefusal  = "op_record_vat_check: check refused"

	// opVATAction is the tenant audit_log action of 00034's row.
	opVATAction = "tenant.vat_rechecked"

	// opTenantWriteShape and its text at 00034 (pg_get_constraintdef).
	opTenantWriteShape    = "operator_audit_log_tenant_write_shape"
	opTenantWriteShapeDef = `CHECK (((kind <> ALL (ARRAY['tenant_vat_checked'::text])) OR ((target_tenant_id IS NOT NULL) AND ` +
		`(target_admin_id IS NULL) AND (target_scope IS NULL) AND (page_number IS NULL) AND (page_size IS NULL) AND ` +
		`(detail = '{}'::jsonb))))`

	// tappa_opdefiner's lists on the two tables, in attnum order: at 00034, and (after its Down)
	// at 00033.
	opDefinerTenantsSel34 = "id,name,vat_number,business_type,plan,timezone,created_at,price_per_employee_month,vat_verified,vat_checked_at"
	opDefinerTenantsSel33 = "id,name,business_type,plan,timezone,created_at,price_per_employee_month"
	opDefinerTenantsUpd34 = "vat_verified,vat_checked_at"
	opDefinerAuditIns34   = "tenant_id,actor_id,action,target,detail,at"

	// opVATTimeText is how the tenant row's detail writes an instant: UTC, six fractional
	// digits, a Z (00034: to_char of the instant AT TIME ZONE 'UTC').
	opVATTimeText = "2006-01-02T15:04:05.000000Z"
)

// opKindsAt34 is the closed set of read kinds at 00034 (opAtVersion's argument; HEAD).
var opKindsAt34 = []string{"legal_versions", "tenants", "tenant_detail", "tenant_plaques", "operator_audit", "tenant_billing", "tenant_vat"}

// op00034ListEdits are the edits 00034 makes to 00033's bodies, as compose-time text, in order.
// Undoing them must give 00033's body back byte for byte.
var op00034ListEdits = map[string][][2]string{
	"op_begin_read": {
		{"'operator_audit', 'tenant_billing') THEN\n", "'operator_audit', 'tenant_billing', 'tenant_vat') THEN\n"},
		{"IF p_kind IN ('tenant_detail', 'tenant_plaques') THEN\n", "IF p_kind IN ('tenant_detail', 'tenant_plaques', 'tenant_vat') THEN\n"},
		{"'operator_mfa_reset', 'operator_disabled') THEN\n", "'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked') THEN\n"},
	},
	"op_read_audit": {
		{"'operator_disabled')\n", "'operator_disabled', 'tenant_vat_checked')\n"},
		{"p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing')\n",
			"p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',\n" +
				"                                                'tenant_vat')\n"},
		{"'tenant_plaques', 'operator_audit', 'tenant_billing')\n                    THEN s.target_scope END",
			"'tenant_plaques', 'operator_audit', 'tenant_billing',\n                                            'tenant_vat')\n                    THEN s.target_scope END"},
	},
}

// ------------------------------------------------------------------ helpers --

// opNewestUpBody returns a function's body as the NEWEST migration whose Up defines it writes it
// -- what a database at HEAD runs.
func opNewestUpBody(t *testing.T, fn string) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "db", "migrations", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("read db/migrations: %v", err)
	}
	sort.Strings(files)
	re := regexp.MustCompile(`(?s)CREATE (?:OR REPLACE )?FUNCTION public\.` + regexp.QuoteMeta(fn) + `\(.*?\nAS \$\$(.*?)\$\$;`)
	var body string
	for _, f := range files {
		up, _ := opMigrationSections(t, filepath.Base(f))
		if all := re.FindAllStringSubmatch(up, -1); len(all) > 0 {
			body = all[len(all)-1][1]
		}
	}
	if body == "" {
		t.Fatalf("no migration's Up defines %s", fn)
	}
	return body
}

// opVATBool is a *bool for a fixture's verdict.
func opVATBool(b bool) *bool { return &b }

// opVATStates are 00017's four states, each with the verdict and the time a fixture starts in.
// The times are WRITTEN, not read from a clock, so a test knows the exact text the tenant row's
// "before" must carry.
var opVATStates = []struct {
	name     string
	verified *bool
	checked  string // a timestamptz literal; "" is NULL
}{
	{"never asked", nil, ""},
	{"asked, no answer", nil, "2025-03-04 05:06:07.123456+00"},
	{"valid", opVATBool(true), "2025-06-07 08:09:10.654321+00"},
	{"invalid", opVATBool(false), "2025-09-10 11:12:13.000001+00"},
}

// opVATFixture is one tenant the owner wrote inside the test's transaction.
type opVATFixture struct {
	id       uuid.UUID
	name     string
	number   string
	verified *bool
	checked  *time.Time
}

// opVATTenant writes a tenant with a synthetic, unique VAT number in the given state.
func opVATTenant(t *testing.T, ctx context.Context, q opQuerier, name string, verified *bool, checked string) opVATFixture {
	t.Helper()
	f := opVATFixture{id: uuid.New(), name: name, verified: verified}
	f.number = "VAT-OP16-" + f.id.String()
	if err := q.QueryRow(ctx, `
		INSERT INTO public.tenants (id, name, vat_number, business_type, structure, plan, vat_verified, vat_checked_at)
		VALUES ($1, $2, $3, 'cafe', 'single', 'standard', $4, NULLIF($5, '')::timestamptz)
		RETURNING vat_checked_at`, f.id, name, f.number, verified, checked).Scan(&f.checked); err != nil {
		t.Fatalf("insert the VAT fixture tenant: %v", err)
	}
	return f
}

// opForgeVAT forges a VAT-read ticket bound to the tenant id and returns the raw ticket.
func opForgeVAT(t *testing.T, ctx context.Context, tx pgx.Tx, session, admin, tenant uuid.UUID, xact string) string {
	t.Helper()
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, admin, tenantVATReadKind, opDetailTicketHash(raw, tenant), &tenant, xact, "30 seconds")
	return raw
}

// opScanVAT runs the shipped read statement and scans every row it returns with a scan
// written HERE, the database's error untouched.
func opScanVAT(ctx context.Context, q opQuerier, hash, ticket, tenant any) ([]TenantVATStatus, error) {
	rows, err := q.Query(ctx, readTenantVATSQL, hash, ticket, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TenantVATStatus
	for rows.Next() {
		var s TenantVATStatus
		if err := rows.Scan(&s.TenantID, &s.TenantName, &s.Number, &s.Verified, &s.CheckedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// opReadVAT runs the read as tappa_operator inside a savepoint of tx; on success the savepoint
// is released, so the consumption stays in tx.
func opReadVAT(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket string, tenant uuid.UUID) ([]TenantVATStatus, error) {
	t.Helper()
	var out []TenantVATStatus
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		out, e = opScanVAT(ctx, sp, hash, ticket, tenant)
		return e
	})
	return out, err
}

func opSameBool(a, b *bool) bool      { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
func opSameTime(a, b *time.Time) bool { return (a == nil) == (b == nil) && (a == nil || a.Equal(*b)) }
func opVATSame(s TenantVATStatus, f opVATFixture) bool {
	return s.TenantID == f.id && s.TenantName == f.name && s.Number == f.number &&
		opSameBool(s.Verified, f.verified) && opSameTime(s.CheckedAt, f.checked)
}

// opRecordVAT runs the shipped write as tappa_operator inside a savepoint of tx, the database's
// error untouched; on success the savepoint is released, so the writes stay in tx.
func opRecordVAT(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenant, number, valid any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", recordTenantVATCheckSQL, hash, tenant, number, valid)
}

// opRecordVATAccessor runs the PRODUCT's accessor, RecordTenantVATCheck, as tappa_operator
// inside a savepoint of tx.
func opRecordVATAccessor(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenant uuid.UUID, number string, valid bool) error {
	t.Helper()
	return opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return RecordTenantVATCheck(ctx, sp, hash, tenant, number, valid)
	})
}

// opVATReadFor gives each tenant a COMMITTED VAT read by the session hash names: the 'read' row
// and the 'tenant_vat' ticket op_begin_read writes, the ticket's created_xact a transaction that
// really committed (opCommittedXact) -- written by the owner INSIDE the test's transaction, so
// nothing is committed. Round 3 binds the write to such a read (00034 section 7 (b2)); every
// test that wants the write past the binding calls this first.
func opVATReadFor(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, tenants ...uuid.UUID) {
	t.Helper()
	var session, admin uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id, admin_id FROM public.platform_sessions WHERE token_hash = $1`, hash).Scan(&session, &admin); err != nil {
		t.Fatalf("the session a VAT read is forged for: %v", err)
	}
	xact := opCommittedXact(t, ctx)
	for _, tenant := range tenants {
		opForgeVAT(t, ctx, tx, session, admin, tenant, xact)
	}
}

// opVATVerdict is a tenant's verdict and its time as the owner reads them, with the row's
// physical version (ctid and xmin) -- a write that "changed nothing" must not have rewritten
// the row either.
type opVATVerdict struct {
	verified *bool
	checked  *time.Time
	version  string
}

func opVATOf(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) opVATVerdict {
	t.Helper()
	var v opVATVerdict
	if err := q.QueryRow(ctx, `SELECT vat_verified, vat_checked_at, ctid::text || '/' || xmin::text
	                             FROM public.tenants WHERE id = $1`, tenant).Scan(&v.verified, &v.checked, &v.version); err != nil {
		t.Fatalf("read a tenant's verdict: %v", err)
	}
	return v
}

func (v opVATVerdict) same(w opVATVerdict) bool {
	return opSameBool(v.verified, w.verified) && opSameTime(v.checked, w.checked) && v.version == w.version
}

// opVATTrailRow is one tenant audit_log row of 00034's action.
type opVATTrailRow struct {
	actor  *uuid.UUID
	target *string
	detail string
	at     time.Time
}

// opVATTrail is a tenant's 00034 audit rows, oldest first (the owner's read).
func opVATTrail(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID) []opVATTrailRow {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT actor_id, target, detail::text, at FROM public.audit_log
	                            WHERE tenant_id = $1 AND action = $2 ORDER BY at, id`, tenant, opVATAction)
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

// opVATActions counts 00034's action in EVERY tenant's audit_log the test can see: under
// REPEATABLE READ only this transaction's writes move it.
func opVATActions(t *testing.T, ctx context.Context, q opQuerier) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM public.audit_log WHERE action = $1`, opVATAction)
}

// opVATOperatorRows counts the 'tenant_vat_checked' rows of one session, naming one tenant.
func opVATOperatorRows(t *testing.T, ctx context.Context, q opQuerier, session, tenant uuid.UUID) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM public.operator_audit_log
	                          WHERE kind = 'tenant_vat_checked' AND session_id = $1 AND target_tenant_id = $2`, session, tenant)
}

// opVATDetailText is the exact text jsonb gives a tenant row's detail: keys in jsonb's order
// (shorter first), the verdicts as booleans or null, the times as UTC text or null, no number.
func opVATDetailText(beforeV *bool, beforeAt *time.Time, afterV bool, afterAt time.Time) string {
	b := "null"
	if beforeV != nil {
		b = strconv.FormatBool(*beforeV)
	}
	ts := func(at *time.Time) string {
		if at == nil {
			return "null"
		}
		return `"` + at.UTC().Format(opVATTimeText) + `"`
	}
	return fmt.Sprintf(`{"after": {"verified": %t, "checked_at": %s}, "before": {"verified": %s, "checked_at": %s}, "actor_kind": "operator"}`,
		afterV, ts(&afterAt), b, ts(beforeAt))
}

// opNormalisedBody is a function's live prosrc, normalised (opNormalisedSource).
func opNormalisedBody(t *testing.T, ctx context.Context, q opQuerier, sig string) string {
	t.Helper()
	var src string
	if err := q.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = $1::regprocedure`, sig).Scan(&src); err != nil {
		t.Fatalf("read %s: %v", sig, err)
	}
	return opNormalisedSource(src)
}

// opVATWriteSourceRules are the NAMED pins on op_record_vat_check's source (normalised): the
// statements that carry the decisions the behaviour tests measure, spelled out -- so an edit
// that keeps a behaviour test green by accident (a fixture that does not exercise it) still
// meets a named line. Each rule is a fragment that must occur EXACTLY once.
var opVATWriteSourceRules = []struct{ name, fragment string }{
	{"the NULL refusal, first", `if p_tenant_id is null or p_vat_number is null or p_valid is null then raise exception 'op_record_vat_check: check refused' using errcode = 'invalid_parameter_value'; end if;`},
	{"the binding to a COMMITTED read of the tenant by the session, right after the NULL refusal, refused with the same constant 22023 (round 3)",
		`end if; if not exists (select 1 from public.operator_read_tickets as k join public.operator_audit_log as r on r.id = k.audit_id ` +
			`where k.session_id = v_session and k.kind = 'tenant_vat' and k.target_tenant_id = p_tenant_id ` +
			`and pg_xact_status(k.created_xact) = 'committed' and r.kind = 'read' and r.session_id = v_session ` +
			`and r.target_scope = 'tenant_vat' and r.target_tenant_id = p_tenant_id) then ` +
			`raise exception 'op_record_vat_check: check refused' using errcode = 'invalid_parameter_value'; end if; begin select t.vat_number,`},
	{"the locking read, by the tenant ALONE, FOR NO KEY UPDATE, returning the number", `select t.vat_number, t.vat_verified, t.vat_checked_at into v_number, v_before_valid, v_before_checked from public.tenants as t where t.id = p_tenant_id for no key update;`},
	{"the guard, right after the locking read: a found tenant whose number is p_vat_number, compared in a variable", `for no key update; if found and v_number = p_vat_number then v_at := clock_timestamp(); update public.tenants`},
	{"one wall-clock read", `v_at := clock_timestamp();`},
	{"the UPDATE: the verdict and its time, by the tenant", `update public.tenants as t set vat_verified = p_valid, vat_checked_at = v_at where t.id = p_tenant_id;`},
	{"the tenant row: K6's columns, the tenant, the operator, the action, the tenant id as target", `insert into public.audit_log (tenant_id, actor_id, action, target, detail, at) values (p_tenant_id, v_admin, 'tenant.vat_rechecked', p_tenant_id::text,`},
	{"the tenant row: `at` is the one clock read, and the row is the guard's last statement (B14)", `'yyyy-mm-dd"t"hh24:mi:ss.us"z"'))), v_at); end if; insert into public.operator_audit_log`},
	{"the operator row on every accepted call, after the guard", `end if; insert into public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) values ('tenant_vat_checked', v_session, v_admin, p_tenant_id);`},
	{"the caught constraint becomes the one refusal", `when integrity_constraint_violation then get stacked diagnostics v_constraint = constraint_name, v_state = returned_sqlstate; raise log 'op_record_vat_check: a write failed constraint "%" (sqlstate %); refused', v_constraint, v_state; raise exception 'op_record_vat_check: check refused' using errcode = 'invalid_parameter_value';`},
}

// opVATWriteSourceFindings returns one line per broken rule, plus: exactly one statement writes
// tenants, one writes audit_log, one writes operator_audit_log, one reads tenants, one reads the
// tickets; the NULL refusal and then the binding come before the first reference to tenants; the
// only RAISEs are the rules' four; and -- the third eye's F1, round 2 -- p_vat_number occurs
// exactly twice, in the NULL refusal and in the guard: no statement names the number, so no plan
// can answer one from tenants_vat_number_key, which reaches whichever tenant holds the number.
// The one EXISTS is the binding's (round 3).
func opVATWriteSourceFindings(src string) []string {
	var out []string
	for _, r := range opVATWriteSourceRules {
		if n := strings.Count(src, r.fragment); n != 1 {
			out = append(out, fmt.Sprintf("%s: %d occurrence(s), want 1", r.name, n))
		}
	}
	for frag, want := range map[string]int{"update public.tenants": 1, "insert into public.audit_log": 1,
		"insert into public.operator_audit_log": 1, "from public.tenants": 1, "from public.operator_read_tickets": 1,
		"raise ": 4, "p_vat_number": 2, "vat_number =": 0, "exists": 1, " like ": 0, "similar to": 0, "~": 0} {
		if n := strings.Count(src, frag); n != want {
			out = append(out, fmt.Sprintf("%q occurs %d time(s), want %d", frag, n, want))
		}
	}
	i, b, j := strings.Index(src, "p_valid is null"), strings.Index(src, "from public.operator_read_tickets"), strings.Index(src, "public.tenants")
	if i < 0 || j < 0 || i > j {
		out = append(out, "the NULL refusal does not come before the first reference to tenants")
	}
	if b < 0 || j < 0 || b > j || b < i {
		out = append(out, "the binding to a committed read does not come between the NULL refusal and the first reference to tenants")
	}
	// The orchestrator's decision (OP-16 A, phase 2): the row lock is FOR NO KEY UPDATE -- what
	// the UPDATE takes anyway -- and never FOR UPDATE, which would hold every foreign-key check
	// of the tenant (a tap's INSERT among them) for as long as the call runs. Nor FOR SHARE /
	// FOR KEY SHARE (which would not serialise two re-checks).
	for _, lock := range []string{"for update", "for share", "for key share"} {
		if strings.Contains(src, lock) {
			out = append(out, fmt.Sprintf("the body takes a %s lock; the row lock is FOR NO KEY UPDATE", strings.ToUpper(lock)))
		}
	}
	return out
}

// ---------------------------------------------------------------- catalogue --

// TestOperator00034_TheFunctionsAndTheirExactSignatures pins what ADR 0021 §6's generic pins
// leave open for the two functions 00034 creates and the two it replaces, at HEAD:
//   - op_read_tenant_vat and op_record_vat_check: exact arguments and result (the read's fixed
//     column list; the write's void), the owner, SECURITY DEFINER, proconfig, set-returning only
//     for the read, one overload, EXECUTE for tappa_operator alone;
//   - op_begin_read and op_read_audit keep their identities; their live bodies are 00034's, and
//     00034's are 00033's but for the six list edits (undoing them gives 00033's byte for byte);
//   - HEAD's exact CHECKs: the read kinds (seven), the audit kinds (fifteen), the tenant-write
//     shape, actor_shape unchanged (00033's three arms) -- all validated;
//   - the forward, frozen-clock, consumption and foreign-EXECUTE scans raise nothing and walked
//     the new functions;
//   - the definer's functions that write tenants or audit_log are exactly op_record_vat_check;
//     those that name 'tenant_vat_checked' are it and the two filter lists' functions;
//   - op_record_vat_check's NAMED source pins (opVATWriteSourceRules) hold, and the read's
//     RETURN QUERY is the fixed column list by the tenant. CONTROLS: each pin's mutation of the
//     live source -- the number in the SET list, the UPDATE by the number, round 1's locking
//     read by the tenant and the number, the guard without the number, the guard dropped, the
//     operator row inside the guard, `at` from now(), no row lock, FOR UPDATE in place of
//     FOR NO KEY UPDATE, a NULL verdict let through; round 3's binding switched off, accepting a
//     read of this transaction, ignoring the tenant or the session; the guard by LIKE -- is
//     reported.
func TestOperator00034_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	for name, want := range op00034Functions {
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
		if args != want.args || result != want.result {
			t.Errorf("%s(%s) returns %s,\n want (%s) returning %s", name, args, result, want.args, want.result)
		}
		if owner != "tappa_opdefiner" || !secdef || retset != strings.HasPrefix(name, "op_read_") {
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
	if got := opIdentity(t, ctx, tx, "op_begin_read"); got != "p_session text, p_kind text, p_params jsonb -> text" {
		t.Errorf("op_begin_read is now %s; the replacement keeps 00027's identity", got)
	}
	if got, want := opIdentity(t, ctx, tx, "op_read_audit"), op00031Read.args+" -> "+op00031Read.result; got != want {
		t.Errorf("op_read_audit is now %s; the replacement keeps 00031's identity %s", got, want)
	}

	// The two replaced functions: 00033's bodies and six list edits.
	up, _ := opMigrationSections(t, op00034File)
	up33, _ := opMigrationSections(t, op00033File)
	for fn, edits := range op00034ListEdits {
		b34 := opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", fn)
		undone := b34
		for i := len(edits) - 1; i >= 0; i-- {
			if strings.Count(undone, edits[i][1]) != 1 {
				t.Errorf("00034's %s does not hold edit %d exactly once", fn, i+1)
				continue
			}
			undone = strings.Replace(undone, edits[i][1], edits[i][0], 1)
		}
		if undone != opLogFunctionBody(t, up33, "CREATE OR REPLACE FUNCTION", fn) {
			t.Errorf("00034's %s is not 00033's body with its list edits", fn)
		}
		var live string
		if err := tx.QueryRow(ctx, `SELECT p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, fn).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != b34 || live != opNewestUpBody(t, fn) {
			t.Errorf("the live %s is not 00034's body (or 00034 is not its newest definition)", fn)
		}
	}

	// HEAD's exact CHECKs.
	for _, c := range []struct{ table, name, want string }{
		{"operator_read_tickets", "operator_read_tickets_kind_check", opKindCheckDef(opKindsAt34)},
		{"operator_audit_log", "operator_audit_log_kind_check", opKindCheckDef(opAuditKinds)},
		{"operator_audit_log", opTenantWriteShape, opTenantWriteShapeDef},
		{"operator_audit_log", "operator_audit_log_actor_shape", opActorShapeDef33(opAuthEventKinds, opOwnerKinds)},
	} {
		def, valid := opConstraint(t, ctx, tx, c.table, c.name)
		if def != c.want || !valid {
			t.Errorf("%s is %s (validated %v),\n want %s, validated", c.name, def, valid, c.want)
		}
	}

	// The scans.
	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for _, fn := range []string{"op_read_tenant_vat", "op_record_vat_check", "op_begin_read", "op_read_audit"} {
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
	if !slices.Contains(read, "op_read_tenant_vat") {
		t.Errorf("anti-vacuity: the consumption scan did not read op_read_tenant_vat (it read %v)", read)
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

	// Who writes what: the one writer of tenants and audit_log; the names of the new kind.
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
		{"write tenants", `(update|insert into|delete from)\s+public\.tenants`, []string{"op_record_vat_check"}},
		{"name audit_log", `public\.audit_log\M`, []string{"op_record_vat_check"}},
		{"name the kind tenant_vat_checked", `tenant_vat_checked`, []string{"op_begin_read", "op_read_audit", "op_record_vat_check"}},
	} {
		if got := naming(c.re); !slices.Equal(got, c.want) {
			t.Errorf("the definer's functions that %s are %v, want %v", c.what, got, c.want)
		}
	}

	// The named source pins, and a control per pin on the live source.
	src := opNormalisedBody(t, ctx, tx, "public.op_record_vat_check(text, uuid, text, boolean)")
	for _, f := range opVATWriteSourceFindings(src) {
		t.Errorf("op_record_vat_check: %s", f)
	}
	for _, c := range []struct{ name, old, new string }{
		{"the number in the SET list", "set vat_verified = p_valid,", "set vat_number = p_vat_number, vat_verified = p_valid,"},
		{"the UPDATE by the number instead of the tenant", "vat_checked_at = v_at where t.id = p_tenant_id;", "vat_checked_at = v_at where t.vat_number = v_number;"},
		{"round 1's locking read: by the tenant AND the number", "where t.id = p_tenant_id for no key update;", "where t.id = p_tenant_id and t.vat_number = p_vat_number for no key update;"},
		{"the guard without the number", "if found and v_number = p_vat_number then", "if found then"},
		{"the guard dropped", "if found and v_number = p_vat_number then", "if true then"},
		{"the operator row inside the guard", "v_at); end if; insert into public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) values ('tenant_vat_checked', v_session, v_admin, p_tenant_id);",
			"v_at); insert into public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) values ('tenant_vat_checked', v_session, v_admin, p_tenant_id); end if;"},
		{"`at` from the transaction's clock", "v_at := clock_timestamp();", "v_at := now();"},
		{"no row lock", " for no key update;", ";"},
		{"FOR UPDATE in place of FOR NO KEY UPDATE", " for no key update;", " for update;"},
		{"a NULL verdict let through", " or p_valid is null then", " then"},
		{"the binding switched off (round 3)", "if not exists (select 1 from public.operator_read_tickets", "if false and not exists (select 1 from public.operator_read_tickets"},
		{"the binding accepts a read of this transaction", "pg_xact_status(k.created_xact) = 'committed'", "pg_xact_status(k.created_xact) <> 'aborted'"},
		{"the binding ignores the tenant", " and k.target_tenant_id = p_tenant_id", ""},
		{"the binding ignores the session", " and r.session_id = v_session", ""},
		{"the guard by LIKE", "if found and v_number = p_vat_number then", "if found and v_number like p_vat_number then"},
	} {
		if strings.Count(src, c.old) != 1 {
			t.Errorf("CONTROL %q: its anchor occurs %d times in the live source", c.name, strings.Count(src, c.old))
			continue
		}
		if got := opVATWriteSourceFindings(strings.Replace(src, c.old, c.new, 1)); len(got) == 0 {
			t.Errorf("CONTROL %q: the source pins did not report it", c.name)
		}
	}
	if read := opNormalisedBody(t, ctx, tx, "public.op_read_tenant_vat(text, text, uuid)"); strings.Count(read,
		"return query select t.id, t.name, t.vat_number, t.vat_verified, t.vat_checked_at from public.tenants as t where t.id = p_tenant_id;") != 1 {
		t.Error("op_read_tenant_vat's RETURN QUERY is not the fixed column list by the tenant")
	}
}

// TestOperator00034_TheDefinerWritesTheVerdictAndNotTheNumber is OP-16's "who writes what" on
// the catalogue and as statements (ADR 0021 §3.3's named decision; the card's acceptance "the
// application role still cannot UPDATE vat_*"):
//   - tappa_opdefiner on tenants: SELECT exactly 00029's, 00032's and 00034's columns; UPDATE
//     exactly vat_verified and vat_checked_at -- NOT vat_number; no INSERT, DELETE, TRUNCATE.
//     On audit_log: INSERT exactly K6's six columns, `at` among them and NOT id; no SELECT,
//     UPDATE, DELETE, TRUNCATE;
//   - as tappa_opdefiner: UPDATE of vat_number, of name, and of vat_verified together with the
//     number are 42501; reading, updating or deleting audit_log rows is 42501, and an INSERT
//     naming id; CONTROLS: the verdict's UPDATE and K6's INSERT go through (in savepoints that
//     are rolled back);
//   - tappa_operator holds nothing on either table, and its direct SELECT and UPDATE are 42501;
//   - tappa_app's UPDATE on tenants is 00024's three columns, none of the VAT ones, and calling
//     either function is 42501.
func TestOperator00034_TheDefinerWritesTheVerdictAndNotTheNumber(t *testing.T) {
	ctx, tx := opTx(t)
	for _, c := range []struct{ role, table, priv, want string }{
		{"tappa_opdefiner", "tenants", "SELECT", opDefinerTenantsSel34},
		{"tappa_opdefiner", "tenants", "UPDATE", opDefinerTenantsUpd34},
		{"tappa_opdefiner", "tenants", "INSERT", ""},
		{"tappa_opdefiner", "audit_log", "INSERT", opDefinerAuditIns34},
		{"tappa_opdefiner", "audit_log", "SELECT", ""},
		{"tappa_opdefiner", "audit_log", "UPDATE", ""},
		{"tappa_operator", "tenants", "SELECT", ""},
		{"tappa_operator", "tenants", "UPDATE", ""},
		{"tappa_operator", "audit_log", "SELECT", ""},
		{"tappa_operator", "audit_log", "INSERT", ""},
		{"tappa_app", "tenants", "UPDATE", "name,business_type,timezone"},
	} {
		if got := opColumns(t, ctx, tx, c.role, c.table, c.priv); got != c.want {
			t.Errorf("%s %s on %s = (%s), want (%s)", c.role, c.priv, c.table, got, c.want)
		}
	}
	for _, c := range []struct {
		role, column string
		want         bool
	}{
		{"tappa_opdefiner", "vat_number", false},
		{"tappa_opdefiner", "vat_verified", true},
		{"tappa_opdefiner", "vat_checked_at", true},
		{"tappa_app", "vat_number", false},
		{"tappa_app", "vat_verified", false},
		{"tappa_app", "vat_checked_at", false},
	} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1, 'public.tenants', $2, 'UPDATE')`, c.role, c.column).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may != c.want {
			t.Errorf("has_column_privilege(%s, tenants, %s, UPDATE) = %v, want %v", c.role, c.column, may, c.want)
		}
	}
	for _, table := range []string{"tenants", "audit_log"} {
		for _, priv := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			for _, role := range []string{"tappa_opdefiner", "tappa_operator"} {
				var may bool
				if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.' || $2, $3)`, role, table, priv).Scan(&may); err != nil || may {
					t.Errorf("has_table_privilege(%s, %s, %s) = %v (err %v), want false", role, table, priv, may, err)
				}
			}
		}
	}

	f := opVATTenant(t, ctx, tx, "op16 grants "+opToken(t), opVATBool(true), "2025-06-07 08:09:10+00")
	other := opVATTenant(t, ctx, tx, "op16 grants other "+opToken(t), nil, "")
	actor := uuid.New()
	for _, probe := range []struct {
		what, sql string
		args      []any
	}{
		{"the number", `UPDATE public.tenants SET vat_number = $2 WHERE id = $1`, []any{f.id, other.number + "-x"}},
		{"the number with the verdict", `UPDATE public.tenants SET vat_verified = false, vat_number = $2 WHERE id = $1`, []any{f.id, other.number + "-x"}},
		{"the name", `UPDATE public.tenants SET name = 'op16 renamed' WHERE id = $1`, []any{f.id}},
		{"a new tenant", `INSERT INTO public.tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'op16 x', $2, 'cafe', 'single')`, []any{uuid.New(), "VAT-OP16-NEW-" + uuid.NewString()}},
		{"the tenant's audit rows", `SELECT count(*) FROM public.audit_log WHERE tenant_id = $1`, []any{f.id}},
		{"an audit row's detail", `UPDATE public.audit_log SET detail = '{}' WHERE tenant_id = $1`, []any{f.id}},
		{"an audit row", `DELETE FROM public.audit_log WHERE tenant_id = $1`, []any{f.id}},
		{"an audit row naming its id", `INSERT INTO public.audit_log (id, tenant_id, action, at) VALUES (gen_random_uuid(), $1, 'tenant.vat_rechecked', clock_timestamp())`, []any{f.id}},
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_opdefiner", probe.sql, probe.args...), sqlstateInsufficientPrivi, "tappa_opdefiner: "+probe.what)
	}
	for _, ctl := range []struct {
		what, sql string
		args      []any
	}{
		{"the verdict", `UPDATE public.tenants SET vat_verified = false, vat_checked_at = clock_timestamp() WHERE id = $1 AND vat_number = $2`, []any{f.id, f.number}},
		{"K6's row", `INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at) VALUES ($1, $2, 'tenant.vat_rechecked', $3, '{}', clock_timestamp())`, []any{f.id, actor, f.id.String()}},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := opExecAs(t, ctx, sp, "tappa_opdefiner", ctl.sql, ctl.args...); err != nil {
			t.Errorf("CONTROL: tappa_opdefiner cannot write %s (%v); the refusals above would not be the missing grants", ctl.what, err)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"tenants", "audit_log"} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT count(*) FROM public.`+table), sqlstateInsufficientPrivi,
			"tappa_operator reading "+table+" directly")
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `UPDATE public.tenants SET vat_verified = true WHERE id = $1`, f.id),
		sqlstateInsufficientPrivi, "tappa_operator writing a verdict directly")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", readTenantVATSQL, opRandHex(t), opRandHex(t), f.id), sqlstateInsufficientPrivi,
		"tappa_app calling op_read_tenant_vat")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", recordTenantVATCheckSQL, opRandHex(t), f.id, f.number, true), sqlstateInsufficientPrivi,
		"tappa_app calling op_record_vat_check")
}

// TestOperator00034_TheTenantWriteKindNamesItsTenantAndNothingElse drives
// operator_audit_log_tenant_write_shape and the kind CHECK as statements, as the owner (the
// CHECKs bind the owner too):
//   - a 'tenant_vat_checked' row with its session, its operator and a tenant is written (the
//     CONTROL);
//   - without a tenant, with an account, a scope, a page number, a page size, or a detail
//     naming a number, it is refused by the tenant-write CHECK (23514, named); without a
//     session it is refused by actor_shape (a tenant-writing row is a session row);
//   - the CHECK constrains its own kinds only: a 'read' row with its tenant, scope and page is
//     written (CONTROL);
//   - WHO may write one: tappa_operator and tappa_app are refused (42501); op_record_auth_event
//     refuses the kind (22023, no row); and -- COUNTED, as for 00033's owner rows -- a statement
//     run AS the definer writes one with 00026's INSERT columns: only op_* bodies run as the
//     definer, and the only one naming the kind outside the two filter lists is
//     op_record_vat_check (TestOperator00034_TheFunctionsAndTheirExactSignatures).
func TestOperator00034_TheTenantWriteKindNamesItsTenantAndNothingElse(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	_, session := opNewSession(t, ctx, tx, a.id, true)
	f := opVATTenant(t, ctx, tx, "op16 shape "+opToken(t), nil, "")
	const kind = "tenant_vat_checked"
	refusedBy := func(what, constraint, sql string, args ...any) {
		t.Helper()
		err := opTry(t, ctx, tx, sql, args...)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != sqlstateCheckViolation || pg.ConstraintName != constraint {
			t.Errorf("%s: %v, want 23514 from %s", what, err, constraint)
		}
	}
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
		{"with a scope", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, target_scope) VALUES ($1, $2, $3, $4, 'tenant_vat')`, []any{kind, session, a.id, f.id}},
		{"with a page number", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, page_number) VALUES ($1, $2, $3, $4, 1)`, []any{kind, session, a.id, f.id}},
		{"with a page size", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, page_size) VALUES ($1, $2, $3, $4, 1)`, []any{kind, session, a.id, f.id}},
		{"with a number in detail", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, detail) VALUES ($1, $2, $3, $4, jsonb_build_object('vat_number', $5::text))`, []any{kind, session, a.id, f.id, f.number}},
		{"with a verdict in detail", `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, detail) VALUES ($1, $2, $3, $4, '{"valid": true}')`, []any{kind, session, a.id, f.id}},
	} {
		refusedBy(kind+" "+c.what, opTenantWriteShape, c.sql, c.args...)
	}
	refusedBy(kind+" without a session", "operator_audit_log_actor_shape",
		`INSERT INTO operator_audit_log (kind, target_tenant_id) VALUES ($1, $2)`, kind, f.id)
	if err := opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, target_scope, page_number, page_size)
	                              VALUES ('read', $1, $2, $3, 'tenant_billing', 1, 12)`, session, a.id, f.id); err != nil {
		t.Errorf("CONTROL: a 'read' row with a tenant, a scope and a page (the CHECK binds its own kinds only): %v", err)
	}
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
		t.Errorf("COUNTED LIMIT moved: as tappa_opdefiner a %s row is refused (%v); measured was written -- update ADR 0021's OP-16 note", kind, err)
	}
}

// TestOperator00034_CallersTempTableIsNeverRead is ADR 0021 §6's temp-table shadow for the read,
// the write and the replaced first phase: the caller creates temp tables with every name they
// touch (tenants, audit_log and the four operator tables), fills them with forged rows -- a
// forged session; the REAL tenant's id with a forged name, number and verdict -- and GRANTs them
// to tappa_opdefiner (the step without which a broken search_path would fail with "permission
// denied" and look refused). The functions still read and write the real tables: the forged
// session is refused; the read returns the real row; the write changes the real tenant and
// writes the real rows; a write naming the shadow's number for the real id changes nothing; a
// write for a second real tenant whose committed VAT read exists ONLY in the shadow log and
// tickets is refused by round 3's binding (it reads the real ones); the shadow tables are not
// written.
func TestOperator00034_CallersTempTableIsNeverRead(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	real := opVATTenant(t, ctx, tx, "op16 real "+opToken(t), nil, "")
	// Round 3: a second real tenant whose VAT read exists ONLY in the caller's shadow.
	shadowRead := opVATTenant(t, ctx, tx, "op16 shadow read "+opToken(t), nil, "")
	shadowReadRow := uuid.New()
	xact := opCommittedXact(t, ctx)
	forgedSession := opRandHex(t)
	shadowNumber := "VAT-OP16-SHADOW-" + uuid.NewString()

	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		for _, s := range []string{
			`CREATE TEMP TABLE tenants (id uuid, name text, vat_number text, vat_verified boolean, vat_checked_at timestamptz)`,
			`CREATE TEMP TABLE audit_log (id uuid DEFAULT gen_random_uuid(), tenant_id uuid, actor_id uuid, action text, target text,
			     detail jsonb DEFAULT '{}', at timestamptz DEFAULT clock_timestamp())`,
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
			`GRANT ALL ON pg_temp.tenants, pg_temp.audit_log, pg_temp.platform_sessions, pg_temp.platform_admins,
			     pg_temp.operator_audit_log, pg_temp.operator_read_tickets TO tappa_opdefiner`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return err
			}
		}
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO pg_temp.tenants VALUES ($1, 'SHADOW name of the real id', $2, true, '2001-02-03 04:05:06+00')`, []any{real.id, shadowNumber}},
			{`INSERT INTO pg_temp.platform_sessions (admin_id, token_hash, mfa_verified_at) VALUES ($1, $2, clock_timestamp())`, []any{a.id, forgedSession}},
			{`INSERT INTO pg_temp.operator_audit_log (id, kind, session_id, actor_admin_id, target_scope, target_tenant_id)
			  VALUES ($1, 'read', $2, $3, 'tenant_vat', $4)`, []any{shadowReadRow, session, a.id, shadowRead.id}},
			{`INSERT INTO pg_temp.operator_read_tickets (ticket_hash, session_id, kind, target_tenant_id, audit_id, expires_at, created_xact)
			  VALUES ($1, $2, 'tenant_vat', $3, $4, clock_timestamp() + interval '30 seconds', $5::xid8)`,
				[]any{opRandHex(t), session, shadowRead.id, shadowReadRow, xact}},
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
	if reachable != 2 {
		t.Fatalf("the definer role sees %d forged rows, want 2", reachable)
	}

	// The forged SESSION exists only in the shadow: both doors refuse it.
	_, err := opBegin(t, ctx, tx, forgedSession, tenantVATReadKind, opDetailParams(real.id.String()))
	opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read with a session that exists only in the caller's temp table")
	opWantClean(t, opRecordVAT(t, ctx, tx, forgedSession, real.id, real.number, true), sqlstateInvalidAuthorization, touchRefusal00026,
		"op_record_vat_check with a session that exists only in the caller's temp table")

	// The real session: the 'read' row reaches the REAL log, the read returns the REAL row.
	audit0 := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session)
	if _, err := opBegin(t, ctx, tx, hash, tenantVATReadKind, opDetailParams(real.id.String())); err != nil {
		t.Fatalf("op_begin_read with the real session: %v", err)
	}
	if d := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session) - audit0; d != 1 {
		t.Errorf("%d 'read' row(s) reached the REAL log, want 1", d)
	}
	rows, err := opReadVAT(t, ctx, tx, hash, opForgeVAT(t, ctx, tx, session, a.id, real.id, opCommittedXact(t, ctx)), real.id)
	if err != nil {
		t.Fatalf("op_read_tenant_vat: %v", err)
	}
	if len(rows) != 1 || !opVATSame(rows[0], real) {
		t.Errorf("op_read_tenant_vat read %d row(s), the real tenant's = %v; want the real row", len(rows), len(rows) == 1 && opVATSame(rows[0], real))
	}

	// The write: the shadow's number for the real id is no match; the real number is.
	before := opVATOf(t, ctx, tx, real.id)
	if err := opRecordVAT(t, ctx, tx, hash, real.id, shadowNumber, true); err != nil {
		t.Fatalf("op_record_vat_check with the shadow's number: %v", err)
	}
	if after := opVATOf(t, ctx, tx, real.id); !after.same(before) || len(opVATTrail(t, ctx, tx, real.id)) != 0 {
		t.Error("a write naming the SHADOW's number changed the real tenant or wrote its trail")
	}
	if err := opRecordVAT(t, ctx, tx, hash, real.id, real.number, false); err != nil {
		t.Fatalf("op_record_vat_check with the real number: %v", err)
	}
	if after := opVATOf(t, ctx, tx, real.id); after.verified == nil || *after.verified || len(opVATTrail(t, ctx, tx, real.id)) != 1 {
		t.Error("the write did not change the REAL tenant to false with one REAL trail row")
	}
	if n := opVATOperatorRows(t, ctx, tx, session, real.id); n != 2 {
		t.Errorf("%d 'tenant_vat_checked' row(s) reached the REAL log, want 2", n)
	}
	// Round 3's binding reads the REAL log and tickets: a read that exists only in the shadow
	// binds nothing.
	shadowBefore := opVATOf(t, ctx, tx, shadowRead.id)
	opWantClean(t, opRecordVAT(t, ctx, tx, hash, shadowRead.id, shadowRead.number, true), sqlstateInvalidParameter, vatCheckRefusal,
		"a write whose committed read exists only in the caller's temp tables", shadowRead.number, hash)
	if !opVATOf(t, ctx, tx, shadowRead.id).same(shadowBefore) || len(opVATTrail(t, ctx, tx, shadowRead.id)) != 0 ||
		opVATOperatorRows(t, ctx, tx, session, shadowRead.id) != 0 {
		t.Error("a write bound only by the SHADOW's read changed the tenant or wrote a row")
	}
	var shadowAudit, shadowLog, shadowTickets int64
	var shadowVerified bool
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.audit_log), (SELECT count(*) FROM pg_temp.operator_audit_log),
		                                (SELECT count(*) FROM pg_temp.operator_read_tickets),
		                                (SELECT vat_verified FROM pg_temp.tenants WHERE id = $1)`, real.id).
			Scan(&shadowAudit, &shadowLog, &shadowTickets, &shadowVerified)
	}); err != nil {
		t.Fatal(err)
	}
	if shadowAudit != 0 || shadowLog != 1 || shadowTickets != 1 || !shadowVerified {
		t.Errorf("the CALLER's temp tables were written: audit_log=%d operator log=%d tickets=%d shadow verdict=%v; want 0, 1, 1 (the planted read), true",
			shadowAudit, shadowLog, shadowTickets, shadowVerified)
	}
}

// opVATState is the catalogue state 00034's Down and Up move between.
type opVATState struct {
	fns                                int64
	begin, audit                       string
	kinds, tickets                     string
	kindsValid, ticketsValid           bool
	shape                              string
	shapeValid                         bool
	tenantsSel, tenantsUpd, auditIns   string
	operatorBegin, operatorAudit, appX bool
}

func opVATReadState(t *testing.T, ctx context.Context, q pgx.Tx) opVATState {
	t.Helper()
	var s opVATState
	if err := q.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM pg_proc WHERE proname IN ('op_read_tenant_vat', 'op_record_vat_check')),
		       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_begin_read(text, text, jsonb)'::regprocedure),
		       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_read_audit(text, text, text, integer, integer)'::regprocedure),
		       coalesce((SELECT pg_get_constraintdef(oid) FROM pg_constraint
		                  WHERE conrelid = 'public.operator_audit_log'::regclass AND conname = $1), ''),
		       coalesce((SELECT convalidated FROM pg_constraint
		                  WHERE conrelid = 'public.operator_audit_log'::regclass AND conname = $1), false),
		       has_function_privilege('tappa_operator', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE'),
		       has_function_privilege('tappa_operator', 'public.op_read_audit(text, text, text, integer, integer)', 'EXECUTE'),
		       has_function_privilege('tappa_app', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE')
		       OR has_function_privilege('tappa_app', 'public.op_read_audit(text, text, text, integer, integer)', 'EXECUTE')`,
		opTenantWriteShape).Scan(&s.fns, &s.begin, &s.audit, &s.shape, &s.shapeValid, &s.operatorBegin, &s.operatorAudit, &s.appX); err != nil {
		t.Fatalf("read the state: %v", err)
	}
	s.kinds, s.kindsValid = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_kind_check")
	s.tickets, s.ticketsValid = opConstraint(t, ctx, q, "operator_read_tickets", "operator_read_tickets_kind_check")
	s.tenantsSel = opColumns(t, ctx, q, "tappa_opdefiner", "tenants", "SELECT")
	s.tenantsUpd = opColumns(t, ctx, q, "tappa_opdefiner", "tenants", "UPDATE")
	s.auditIns = opColumns(t, ctx, q, "tappa_opdefiner", "audit_log", "INSERT")
	return s
}

// TestOperator00034_DownGivesBack00033AndUpTakesItAgain runs 00034's Down and Up from the
// migration file inside the test's transaction (at 00034 through opAtVersion).
//   - THE TEXT: the Down's op_begin_read and op_read_audit are 00033's Up bodies VERBATIM; its
//     statements (comments aside) hold no REVOKE ALL and no GRANT, and exactly three REVOKEs
//     whose column lists are the Up's three GRANTs' (and no table-wide REVOKE).
//   - THE SETS AND THE CONDITIONS, WHOLE: the Down's audit condition and two kind CHECKs name
//     00033's fourteen (derived from 00033's file), its ticket condition and two ticket CHECKs
//     00032's six (from 00032's file); the Up's name the fifteen and the seven. Each NOT VALID
//     condition is read to its end and compared whole.
//   - Down gives both functions 00033's bodies back (the live prosrc), drops the two new ones and
//     the tenant-write CHECK, takes back exactly the column grants (00029's and 00032's tenants
//     columns stay: the overview and the billing columns still read), and puts the two kind
//     CHECKs back; after it 'tenant_vat' is refused by op_begin_read (22023), a
//     'tenant_vat_checked' row by the kind CHECK (23514), and the definer's verdict UPDATE and
//     K6 INSERT are 42501. Up takes it all again.
//   - NOT VALID, branch by branch: nothing outside -> both VALIDATED; a write's rows (a
//     'tenant_vat_checked' row, its tenant row and its verdict) -> the kind CHECK NOT VALID and
//     the rows and the verdict STAY; an unconsumed and a consumed 'tenant_vat' ticket -> the
//     ticket CHECK NOT VALID; a later migration's kinds -> the Down NOT VALID and the Up NOT VALID
//     too (it composes). The chain 34 -> 33 -> 32 runs without a 23514 with this file's rows
//     present.
//   - T9, MEASURED: after the Down's REVOKEs on tenants and audit_log (and before the rollback),
//     another session's ROW EXCLUSIVE lock on both tables -- what an INSERT or an UPDATE takes --
//     is granted at once (NOWAIT); the modes this session holds on the two tables are logged.
func TestOperator00034_DownGivesBack00033AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 34, opKindsAt34...)
	up, down := opMigrationSections(t, op00034File)
	up33, _ := opMigrationSections(t, op00033File)
	up32, down32 := opMigrationSections(t, op00032File)
	_, down33 := opMigrationSections(t, op00033File)
	begin33, begin34 := opLogFunctionBody(t, up33, "CREATE OR REPLACE FUNCTION", "op_begin_read"), opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_begin_read")
	audit33, audit34 := opLogFunctionBody(t, up33, "CREATE OR REPLACE FUNCTION", "op_read_audit"), opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_read_audit")
	if begin33 == begin34 || audit33 == audit34 {
		t.Fatal("PREMISE: a replaced body is the same text as the one it replaces")
	}
	if got := opLogFunctionBody(t, down, "CREATE OR REPLACE FUNCTION", "op_begin_read"); got != begin33 {
		t.Error("00034's Down does not give op_begin_read 00033's Up body verbatim")
	}
	if got := opLogFunctionBody(t, down, "CREATE OR REPLACE FUNCTION", "op_read_audit"); got != audit33 {
		t.Error("00034's Down does not give op_read_audit 00033's Up body verbatim")
	}

	// THE GRANTS: the Down takes back exactly what the Up gave, by column.
	downStatements, upStatements := opStatementsOnly(down), opStatementsOnly(up)
	if regexp.MustCompile(`(?i)\bREVOKE\s+ALL\b`).MatchString(downStatements) || regexp.MustCompile(`(?i)\bGRANT\b`).MatchString(downStatements) {
		t.Error("00034's Down holds a REVOKE ALL or a GRANT; it takes back the Up's column grants by name and gives nothing")
	}
	grantRE := regexp.MustCompile(`(?m)^GRANT (SELECT|UPDATE|INSERT) \(([^)]*)\) ON (tenants|audit_log) TO tappa_opdefiner;$`)
	revokeRE := regexp.MustCompile(`(?m)^REVOKE (SELECT|UPDATE|INSERT) \(([^)]*)\) ON (tenants|audit_log) FROM tappa_opdefiner;$`)
	set := func(re *regexp.Regexp, text string) []string {
		var out []string
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			out = append(out, m[1]+" "+m[3]+" ("+strings.Join(strings.Fields(m[2]), " ")+")")
		}
		sort.Strings(out)
		return out
	}
	grants, revokes := set(grantRE, upStatements), set(revokeRE, downStatements)
	if len(grants) != 3 || !slices.Equal(grants, revokes) {
		t.Errorf("the Up grants %v and the Down revokes %v; want the same three column grants", grants, revokes)
	}
	if n := len(regexp.MustCompile(`(?mi)^REVOKE\b`).FindAllString(downStatements, -1)); n != 3 {
		t.Errorf("00034's Down holds %d REVOKE statements, want the three column ones", n)
	}

	// THE SETS, from 00033's and 00032's files, and every condition and CHECK of both halves.
	quotedList := func(text, re string) []string {
		m := regexp.MustCompile(`(?s)` + re).FindStringSubmatch(text)
		if m == nil {
			return nil
		}
		return opQuotedList(strings.Join(strings.Fields(m[1]), " "))
	}
	if got := quotedList(up33, `ADD CONSTRAINT operator_audit_log_kind_check\s+CHECK \(kind IN \(([^)]*)\)\);`); !slices.Equal(got, opAuditKinds33) {
		t.Fatalf("00033's audit kind set is %v, want %v", got, opAuditKinds33)
	}
	if got := quotedList(up32, `ADD CONSTRAINT operator_read_tickets_kind_check\s+CHECK \(kind IN \(([^)]*)\)\);`); !slices.Equal(got, opKindsAt32) {
		t.Fatalf("00032's ticket kind set is %v, want %v", got, opKindsAt32)
	}
	part := func(section, from, to string) string {
		i := strings.Index(section, from)
		if i < 0 {
			t.Fatalf("the section does not hold %q", from)
		}
		rest := section[i:]
		if to != "" {
			if j := strings.Index(rest[1:], to); j >= 0 {
				rest = rest[:j+1]
			}
		}
		return rest
	}
	const auditDrop, ticketDrop = "ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check", "ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check"
	whole := func(set []string) string { return "kind <> ALL (ARRAY['" + strings.Join(set, "', '") + "'])" }
	conds := func(text, table string) []string {
		var out []string
		for _, mm := range regexp.MustCompile(`(?s)IF EXISTS \(SELECT 1 FROM public\.`+table+`\s+WHERE (.*?)\) THEN`).FindAllStringSubmatch(text, -1) {
			out = append(out, strings.Join(strings.Fields(mm[1]), " "))
		}
		return out
	}
	lists := func(text, constraint string) [][]string {
		var out [][]string
		for _, mm := range regexp.MustCompile(`(?s)`+constraint+`\s+CHECK \(kind IN \(([^)]*)\)\)`).FindAllStringSubmatch(text, -1) {
			out = append(out, opQuotedList(strings.Join(strings.Fields(mm[1]), " ")))
		}
		return out
	}
	for _, c := range []struct {
		what, text, table, constraint string
		set                           []string
	}{
		{"00034's Up: the audit kinds", part(up, auditDrop, "-- +goose StatementEnd"), "operator_audit_log", "operator_audit_log_kind_check", opAuditKinds},
		{"00034's Up: the ticket kinds", part(up, ticketDrop, "-- +goose StatementEnd"), "operator_read_tickets", "operator_read_tickets_kind_check", opKindsAt34},
		{"00034's Down: the audit kinds", part(down, auditDrop, "-- +goose StatementEnd"), "operator_audit_log", "operator_audit_log_kind_check", opAuditKinds33},
		{"00034's Down: the ticket kinds", part(down, ticketDrop, "-- +goose StatementEnd"), "operator_read_tickets", "operator_read_tickets_kind_check", opKindsAt32},
	} {
		if got := conds(c.text, c.table); len(got) != 1 || got[0] != whole(c.set) {
			t.Errorf("%s: the NOT VALID condition is %q; want exactly one, whole: %q", c.what, got, whole(c.set))
		}
		got := lists(c.text, c.constraint)
		if len(got) != 2 || !slices.Equal(got[0], c.set) || !slices.Equal(got[1], c.set) {
			t.Errorf("%s: the CHECKs name %v, want %v twice (NOT VALID and VALIDATED)", c.what, got, c.set)
		}
	}

	notValid := func(def string, nv bool) string {
		if nv {
			return def + " NOT VALID"
		}
		return def
	}
	at34 := func(s opVATState, when string, kindsNV, ticketsNV bool) {
		t.Helper()
		if s.fns != 2 || s.begin != begin34 || s.audit != audit34 || !s.operatorBegin || !s.operatorAudit || s.appX ||
			s.kinds != notValid(opKindCheckDef(opAuditKinds), kindsNV) || s.kindsValid == kindsNV ||
			s.tickets != notValid(opKindCheckDef(opKindsAt34), ticketsNV) || s.ticketsValid == ticketsNV ||
			s.shape != opTenantWriteShapeDef || !s.shapeValid ||
			s.tenantsSel != opDefinerTenantsSel34 || s.tenantsUpd != opDefinerTenantsUpd34 || s.auditIns != opDefinerAuditIns34 {
			t.Errorf("%s: functions=%d begin is 00034's=%v audit is 00034's=%v operator=%v/%v app=%v\n kinds=%s (%v)\n tickets=%s (%v)\n shape=%s (%v)\n definer tenants SELECT=(%s) UPDATE=(%s) audit_log INSERT=(%s)\n want 00034's state, kinds NOT VALID=%v, tickets NOT VALID=%v",
				when, s.fns, s.begin == begin34, s.audit == audit34, s.operatorBegin, s.operatorAudit, s.appX, s.kinds, s.kindsValid,
				s.tickets, s.ticketsValid, s.shape, s.shapeValid, s.tenantsSel, s.tenantsUpd, s.auditIns, kindsNV, ticketsNV)
		}
	}
	at33 := func(s opVATState, when string, kindsNV, ticketsNV bool) {
		t.Helper()
		if s.fns != 0 || s.begin != begin33 || s.audit != audit33 || !s.operatorBegin || !s.operatorAudit || s.appX ||
			s.kinds != notValid(opKindCheckDef(opAuditKinds33), kindsNV) || s.kindsValid == kindsNV ||
			s.tickets != notValid(opKindCheckDef(opKindsAt32), ticketsNV) || s.ticketsValid == ticketsNV ||
			s.shape != "" || s.tenantsSel != opDefinerTenantsSel33 || s.tenantsUpd != "" || s.auditIns != "" {
			t.Errorf("%s: functions=%d begin is 00033's=%v audit is 00033's=%v operator=%v/%v app=%v\n kinds=%s (%v)\n tickets=%s (%v)\n shape=%q\n definer tenants SELECT=(%s) UPDATE=(%s) audit_log INSERT=(%s)\n want 00033's state, kinds NOT VALID=%v, tickets NOT VALID=%v",
				when, s.fns, s.begin == begin33, s.audit == audit33, s.operatorBegin, s.operatorAudit, s.appX, s.kinds, s.kindsValid,
				s.tickets, s.ticketsValid, s.shape, s.tenantsSel, s.tenantsUpd, s.auditIns, kindsNV, ticketsNV)
		}
	}
	at34(opVATReadState(t, ctx, tx), "PREMISE before Down", false, false)

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := opVATTenant(t, ctx, tx, "op16 down "+opToken(t), nil, "")
	overview := &opFixtureTenant{name: "op16 down overview " + opToken(t), locations: 1, activeEmp: 1, activeTags: 1, activeAdmins: 1}
	overview.id = opNewTenant(t, ctx, tx, overview.name, 0)
	opPopulate(t, ctx, tx, overview)
	xact := opCommittedXact(t, ctx)
	// branch opens a savepoint holding no audit row outside 00033's set and no ticket outside
	// 00032's (the append-only trigger disabled inside the savepoint for the delete, as 00033's
	// Down test does).
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
			{`DELETE FROM operator_read_tickets WHERE kind <> ALL ($1)`, []any{opKindsAt32}},
			{`ALTER TABLE operator_audit_log DISABLE TRIGGER operator_audit_log_append_only`, nil},
			{`DELETE FROM operator_audit_log WHERE kind <> ALL ($1)`, []any{opAuditKinds33}},
			{`ALTER TABLE operator_audit_log ENABLE TRIGGER operator_audit_log_append_only`, nil},
		} {
			if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
				t.Fatalf("clear the kinds 00033 does not know, inside the transaction: %v", err)
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

	// Branch 1: nothing outside the previous sets -> VALIDATED; what Down took away; Up again.
	sp := branch()
	held := func(q opQuerier) []string {
		t.Helper()
		qr, err := q.Query(ctx, `SELECT relation::regclass::text || ':' || mode FROM pg_locks
		                           WHERE locktype = 'relation' AND granted AND pid = pg_backend_pid()
		                             AND relation IN ('public.tenants'::regclass, 'public.audit_log'::regclass) ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pgx.CollectRows(qr, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	heldBefore := held(sp)
	opRunSection(t, ctx, sp, down, "00034 Down with nothing outside the previous sets")
	heldAfter := held(sp)
	t.Logf("OP16-T9-LOCK: this session's locks on tenants and audit_log before the Down %v, after it %v", heldBefore, heldAfter)
	for _, m := range heldAfter {
		if !slices.Contains(heldBefore, m) && !strings.HasSuffix(m, ":AccessShareLock") && !strings.HasSuffix(m, ":RowExclusiveLock") {
			t.Errorf("T9: the Down took %s -- more than a writer of the table takes", m)
		}
	}
	other, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("a second owner connection: %v", err)
	}
	otx, err := other.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, locked := otx.Exec(ctx, `LOCK TABLE public.tenants, public.audit_log IN ROW EXCLUSIVE MODE NOWAIT`)
	if err := otx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := other.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if locked != nil {
		t.Errorf("T9: after the Down's REVOKEs another session's ROW EXCLUSIVE on tenants and audit_log was not granted at once: %v", locked)
	}
	at33(opVATReadState(t, ctx, sp), "after Down (nothing outside the previous sets)", false, false)
	_, err = opBegin(t, ctx, sp, hash, tenantVATReadKind, opDetailParams(f.id.String()))
	opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "after Down, a 'tenant_vat' first phase")
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) VALUES ('tenant_vat_checked', $1, $2, $3)`,
		session, a.id, f.id), sqlstateCheckViolation, "after Down, a 'tenant_vat_checked' row")
	opWant(t, opExecAs(t, ctx, sp, "tappa_opdefiner", `UPDATE public.tenants SET vat_verified = true WHERE id = $1`, f.id),
		sqlstateInsufficientPrivi, "after Down, the definer's verdict UPDATE")
	opWant(t, opExecAs(t, ctx, sp, "tappa_opdefiner", `INSERT INTO public.audit_log (tenant_id, action) VALUES ($1, 'tenant.vat_rechecked')`, f.id),
		sqlstateInsufficientPrivi, "after Down, the definer's K6 INSERT")
	// The trap a REVOKE ALL would have sprung: 00029's overview and 00032's billing columns still read.
	if got, err := opReadDetail(t, ctx, sp, hash, opForgeDetail(t, ctx, sp, session, a.id, overview.id, xact), overview.id); err != nil ||
		len(got) != 1 || got[0].ActiveEmployees != 1 || got[0].Locations != 1 {
		t.Errorf("after Down the tenant overview reads %d row(s), err %v; want one row counting 1 employee and 1 location (00029's grants intact)", len(got), err)
	}
	if err := opExecAs(t, ctx, sp, "tappa_opdefiner", `SELECT id, name, plan, timezone, price_per_employee_month FROM public.tenants WHERE id = $1`, f.id); err != nil {
		t.Errorf("after Down the definer cannot read 00029's and 00032's tenants columns: %v", err)
	}
	opRunSection(t, ctx, sp, up, "00034 Up again")
	at34(opVATReadState(t, ctx, sp), "after Up again", false, false)
	done(sp)

	// Branch 2: a write's rows -> the kind CHECK NOT VALID; the rows and the verdict STAY. Since
	// round 3 a write needs a committed read first, so the read's 'tenant_vat' ticket is there
	// too and the ticket CHECK goes NOT VALID as well (branch 3 has the ticket alone).
	sp = branch()
	opForgeVAT(t, ctx, sp, session, a.id, f.id, xact)
	if err := opRecordVAT(t, ctx, sp, hash, f.id, f.number, true); err != nil {
		t.Fatalf("a write before Down: %v", err)
	}
	verdict := opVATOf(t, ctx, sp, f.id)
	opRunSection(t, ctx, sp, down, "00034 Down with a write's rows present")
	at33(opVATReadState(t, ctx, sp), "after Down (a write's rows and its read)", true, true)
	if after := opVATOf(t, ctx, sp, f.id); !after.same(verdict) || after.verified == nil || !*after.verified ||
		len(opVATTrail(t, ctx, sp, f.id)) != 1 || opVATOperatorRows(t, ctx, sp, session, f.id) != 1 {
		t.Error("after Down the recorded verdict, its tenant row or its operator row is gone; a Down takes a door away, not a fact")
	}
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id) VALUES ('tenant_vat_checked', $1, $2, $3)`,
		session, a.id, f.id), sqlstateCheckViolation, "a NEW 'tenant_vat_checked' row after Down (NOT VALID still binds new rows)")
	opRunSection(t, ctx, sp, up, "00034 Up again over the write's rows")
	at34(opVATReadState(t, ctx, sp), "after Up again (a write's rows)", false, false)
	done(sp)

	// Branch 3: an unconsumed 'tenant_vat' ticket -> the ticket CHECK NOT VALID.
	sp = branch()
	opForgeVAT(t, ctx, sp, session, a.id, f.id, xact)
	opRunSection(t, ctx, sp, down, "00034 Down with a 'tenant_vat' ticket present")
	at33(opVATReadState(t, ctx, sp), "after Down (an unconsumed 'tenant_vat' ticket)", false, true)
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at)
	                              SELECT $1, $2, 'tenant_vat', l.id, clock_timestamp() + interval '30 seconds'
	                                FROM operator_audit_log l WHERE l.session_id = $2 LIMIT 1`, opRandHex(t), session),
		sqlstateCheckViolation, "a NEW 'tenant_vat' ticket after Down (NOT VALID still binds new rows)")
	opRunSection(t, ctx, sp, up, "00034 Up again over the ticket")
	at34(opVATReadState(t, ctx, sp), "after Up again (a 'tenant_vat' ticket)", false, false)
	done(sp)

	// Branch 4: a CONSUMED 'tenant_vat' ticket alone -> the ticket CHECK NOT VALID.
	sp = branch()
	consumed := opForgeRead(t, ctx, sp, session, a.id, tenantVATReadKind, opRandHex(t), &f.id, xact, "30 seconds")
	if _, err := sp.Exec(ctx, `UPDATE operator_read_tickets SET consumed_at = clock_timestamp() WHERE id = $1`, consumed); err != nil {
		t.Fatalf("consume the ticket: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00034 Down with a consumed 'tenant_vat' ticket alone")
	at33(opVATReadState(t, ctx, sp), "after Down (a consumed 'tenant_vat' ticket)", false, true)
	done(sp)

	// Branch 5: a LATER migration's kinds alone. Its Up widened both sets, rows of its kinds were
	// written, its Down put 00034's sets back NOT VALID and left the rows.
	sp = branch()
	for _, s := range []string{
		`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`,
		`ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN ('` + strings.Join(opAuditKinds, "', '") + `', 'zz_later_kind'))`,
		`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
		`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN ('` + strings.Join(opKindsAt34, "', '") + `', 'zz_later_read'))`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Up: %v", err)
		}
	}
	if _, err := sp.Exec(ctx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ('zz_later_kind', $1, $2)`, session, a.id); err != nil {
		t.Fatalf("a later migration's row: %v", err)
	}
	opForgeRead(t, ctx, sp, session, a.id, "zz_later_read", opRandHex(t), &f.id, xact, "30 seconds")
	for _, s := range []string{
		`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`,
		`ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN ('` + strings.Join(opAuditKinds, "', '") + `')) NOT VALID`,
		`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
		`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN ('` + strings.Join(opKindsAt34, "', '") + `')) NOT VALID`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Down: %v", err)
		}
	}
	opRunSection(t, ctx, sp, down, "00034 Down after a later migration's Down left its rows")
	at33(opVATReadState(t, ctx, sp), "after Down (a later migration's kinds)", true, true)
	opRunSection(t, ctx, sp, up, "00034 Up again with a later migration's rows present (it composes)")
	at34(opVATReadState(t, ctx, sp), "after Up again (a later migration's kinds)", true, true)
	done(sp)

	// The chain composes downward: 34 -> 33 -> 32 with this file's rows present.
	sp = branch()
	opForgeVAT(t, ctx, sp, session, a.id, f.id, xact)
	if err := opRecordVAT(t, ctx, sp, hash, f.id, f.number, false); err != nil {
		t.Fatalf("a write before the chain: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00034 Down (chain)")
	opRunSection(t, ctx, sp, down33, "00033 Down after 00034's, this file's rows present")
	opRunSection(t, ctx, sp, down32, "00032 Down after 00033's, this file's rows present")
	if def, valid := opConstraint(t, ctx, sp, "operator_audit_log", "operator_audit_log_kind_check"); valid ||
		def != notValid(opKindCheckDef(opAuditKinds31), true) {
		t.Errorf("after 34 -> 33 -> 32 the audit kind CHECK is %s (validated %v); want 00031's set NOT VALID", def, valid)
	}
	if def, valid := opConstraint(t, ctx, sp, "operator_read_tickets", "operator_read_tickets_kind_check"); valid ||
		def != notValid(opKindCheckDef(opKindsAt31), true) {
		t.Errorf("after 34 -> 33 -> 32 the ticket kind CHECK is %s (validated %v); want 00031's set NOT VALID", def, valid)
	}
	done(sp)

	// And with nothing outside the sets, Down and Up once more: VALIDATED both ways.
	sp = branch()
	opRunSection(t, ctx, sp, down, "00034 Down")
	opRunSection(t, ctx, sp, up, "00034 Up")
	at34(opVATReadState(t, ctx, sp), "after Down and Up", false, false)
	done(sp)
}

// TestOperator00034_PreconditionRefusesAWrongCluster: 00034's first statement refuses the role
// shapes 00026-00033's refuse (absent, over-privileged, joined by membership in either
// direction), a database that is not at 00033 -- op_read_audit or op_begin_read missing or not
// the definer's, 00032's read missing, 00033's `at` trigger missing, either kind CHECK missing,
// the audit kind CHECK without 00033's operator_disabled or already naming this file's kind, the
// ticket kind CHECK without 00032's tenant_billing or already naming this file's kind, this
// file's CHECK already there -- and a drifted ACL this file's column grants cannot narrow: the
// definer holding UPDATE on tenants or SELECT, INSERT, UPDATE or DELETE on audit_log, tappa_app
// holding UPDATE on any of the three VAT columns, directly or through a role it is a member of.
// Each with SQLSTATE 55000 naming 00034; the cluster this suite runs on (taken to 00033 first)
// passes. Nine role shapes, thirteen traces of 00033, ten drifted ACLs.
func TestOperator00034_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 33, opKindsAt32...)
	up, _ := opMigrationSections(t, op00034File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00034's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	const kinds = `ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
	               ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN (%s)) NOT VALID`
	const tickets = `ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
	                 ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN (%s)) NOT VALID`
	quoted := func(set []string) string { return "'" + strings.Join(set, "', '") + "'" }
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"roles present, at 00033", nil, ""},
		{"both roles absent", []string{`ALTER ROLE tappa_operator RENAME TO zz_op16_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op16_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner without BYPASSRLS", []string{`ALTER ROLE tappa_opdefiner NOBYPASSRLS`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_opdefiner is a member", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
		{"op_read_audit missing", []string{`ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) RENAME TO zz_op16_was_read_audit`}, "00033's state"},
		{"op_read_audit not the definer's", []string{`ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) OWNER TO tappa_owner`}, "00033's state"},
		{"op_begin_read missing", []string{`ALTER FUNCTION public.op_begin_read(text, text, jsonb) RENAME TO zz_op16_was_begin_read`}, "00033's state"},
		{"op_begin_read not the definer's", []string{`ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_owner`}, "00033's state"},
		{"00032's read missing", []string{`ALTER FUNCTION public.op_read_tenant_billing(text, text, uuid, integer) RENAME TO zz_op16_was_billing`}, "00033's state"},
		{"00033's trigger missing", []string{`DROP TRIGGER operator_audit_log_at_is_the_wall_clock ON operator_audit_log`}, "00033's state"},
		{"the audit kind CHECK missing", []string{`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`}, "as migration 00033 left them"},
		{"the ticket kind CHECK missing", []string{`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`}, "as migration 00033 left them"},
		{"the audit kind CHECK without operator_disabled", []string{fmtKinds(kinds, quoted(opAuditKinds31))}, "as migration 00033 left them"},
		{"the audit kind CHECK already naming tenant_vat_checked", []string{fmtKinds(kinds, quoted(opAuditKinds))}, "as migration 00033 left them"},
		{"the ticket kind CHECK without tenant_billing", []string{fmtKinds(tickets, quoted(opKindsAt31))}, "as migration 00033 left them"},
		{"the ticket kind CHECK already naming tenant_vat", []string{fmtKinds(tickets, quoted(opKindsAt34))}, "as migration 00033 left them"},
		{"this file's CHECK already there", []string{`ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_tenant_write_shape CHECK (true)`}, "as migration 00033 left them"},
		{"the definer may UPDATE a tenant's name", []string{`GRANT UPDATE (name) ON tenants TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"the definer may UPDATE every tenant column", []string{`GRANT UPDATE ON tenants TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"the definer may SELECT audit_log", []string{`GRANT SELECT (action) ON audit_log TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"the definer may INSERT into audit_log", []string{`GRANT INSERT (tenant_id) ON audit_log TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"the definer may UPDATE audit_log", []string{`GRANT UPDATE (detail) ON audit_log TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"the definer may DELETE from audit_log", []string{`GRANT DELETE ON audit_log TO tappa_opdefiner`}, "a privilege no migration gave"},
		{"tappa_app may UPDATE the verdict", []string{`GRANT UPDATE (vat_verified) ON tenants TO tappa_app`}, "a privilege no migration gave"},
		{"tappa_app may UPDATE the number", []string{`GRANT UPDATE (vat_number) ON tenants TO tappa_app`}, "a privilege no migration gave"},
		{"tappa_app may UPDATE the verdict's time", []string{`GRANT UPDATE (vat_checked_at) ON tenants TO tappa_app`}, "a privilege no migration gave"},
		// Round 3 (the third eye's D3): the privilege through a role tappa_app is a member of --
		// an arm that read only tappa_app's own ACL entries would pass this cluster.
		{"tappa_app may UPDATE the verdict through a role it is a member of",
			[]string{`GRANT UPDATE (vat_verified) ON tenants TO tappa_resolver`, `GRANT tappa_resolver TO tappa_app`}, "a privilege no migration gave"},
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
		case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00034")):
			t.Errorf("%s: precondition answered %q %q, want %s naming 00034 and containing %q", c.what, code, msg, sqlstatePrerequisiteState, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("%s: rollback: %v", c.what, err)
		}
	}
}

// ------------------------------------------------------------ op_begin_read --

// TestOpBeginRead_TheVATKindBindsTheTenantAndNothingElse: the kind 00034 adds to the first phase.
//   - 'tenant_vat': one 'read' row -- scope 'tenant_vat', the tenant in target_tenant_id, no
//     page, detail exactly {} -- and one ticket of the same kind carrying the same tenant whose
//     stored hash is sha256(raw ticket || the canonical {tenant_id}); an upper-case id binds the
//     same lower-case text;
//   - 22023 and no row for every parameter object the kind does not name, 28000 and no row for
//     the six dead sessions;
//   - CONTROLS: the two kinds that share its branch, and the other four, still pass phase one
//     with their own objects (the replacement kept their branches).
func TestOpBeginRead_TheVATKindBindsTheTenantAndNothingElse(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	rowsOf := func() (audit, tickets int64) {
		return opAudit(t, ctx, tx), opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets`)
	}
	id := uuid.New()
	a0, k0 := rowsOf()
	raw, err := opBegin(t, ctx, tx, hash, tenantVATReadKind, opDetailParams(strings.ToUpper(id.String())))
	if err != nil {
		t.Fatalf("op_begin_read 'tenant_vat': %v", err)
	}
	if a1, k1 := rowsOf(); a1-a0 != 1 || k1-k0 != 1 {
		t.Fatalf("op_begin_read 'tenant_vat' wrote %d audit row(s) and %d ticket(s), want 1 and 1", a1-a0, k1-k0)
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
	if kind != "read" || scope != tenantVATReadKind || detail != "{}" || tenant == nil || *tenant != id ||
		number != nil || size != nil || ticketKind != tenantVATReadKind || ticketTenant == nil || *ticketTenant != id {
		t.Errorf("the 'tenant_vat' audit row: kind=%s scope=%s detail=%s tenant=%v page=%v/%v; ticket kind=%s tenant=%v",
			kind, scope, detail, tenant, number, size, ticketKind, ticketTenant)
	}
	if ticketHash != opDetailTicketHash(raw, id) {
		t.Error("the stored hash is not sha256(raw ticket || canonical {tenant_id}) with the id in lower case")
	}
	for _, c := range []struct{ name, params string }{
		{"no key", `{}`},
		{"an extra key (a number)", `{"tenant_id": "` + id.String() + `", "vat_number": "VAT-OP16-ZZ"}`},
		{"an extra key (a page)", `{"tenant_id": "` + id.String() + `", "page_number": 1}`},
		{"the list's keys", opTenantsParams("", 1, 10)},
		{"not a uuid", opDetailParams("op16 not a uuid")},
		{"a uuid in braces", opDetailParams("{" + id.String() + "}")},
		{"a uuid without hyphens", opDetailParams(strings.ReplaceAll(id.String(), "-", ""))},
		{"a number", `{"tenant_id": 7}`},
		{"null", `{"tenant_id": null}`},
		{"a string, not an object", `"` + id.String() + `"`},
	} {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, hash, tenantVATReadKind, c.params)
		opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "op_begin_read 'tenant_vat', "+c.name, hash, c.params)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read 'tenant_vat', %s: %d audit row(s) and %d ticket(s) written", c.name, a1-a0, k1-k0)
		}
	}
	for _, d := range opDeadSessions(t, ctx, tx) {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, d.hash, tenantVATReadKind, opDetailParams(id.String()))
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read 'tenant_vat', "+d.name, d.hash)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read 'tenant_vat', %s: %d audit row(s) and %d ticket(s) written", d.name, a1-a0, k1-k0)
		}
	}
	for _, c := range []struct{ kind, params string }{
		{tenantDetailReadKind, opDetailParams(id.String())},
		{tenantPlaquesReadKind, opDetailParams(id.String())},
		{legalVersionsReadKind, opLegalParams(1, 10)},
		{tenantsReadKind, opTenantsParams("x", 1, 10)},
		{operatorAuditReadKind, opLogParams(string(OperatorAuditTenantVATChecked), 1, 50)},
		{tenantBillingReadKind, opBillingParams(id.String(), 1)},
	} {
		if _, err := opBegin(t, ctx, tx, hash, c.kind, c.params); err != nil {
			t.Errorf("CONTROL: op_begin_read %q refused its own parameter object: %v", c.kind, err)
		}
	}
}

// --------------------------------------------------------------- the read --

// TestOpReadTenantVAT_ReturnsOnlyTheNamedTenantsNumberAndState is the belt test ADR 0021 §6
// ("kemer") asks of a tenant-naming op_*: no row level security applies inside it, so
// `t.id = p_tenant_id` is the ONLY barrier. Four tenants, one in each of 00017's states; the
// read of each is EXACTLY its own row -- id, name, number, verdict, time -- and through the
// accessor's own scan the same; no read returns another tenant's number; the read writes no
// audit row (phase one did) and rewrites no tenant row.
func TestOpReadTenantVAT_ReturnsOnlyTheNamedTenantsNumberAndState(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	tok := opToken(t)
	var fixtures []opVATFixture
	for _, s := range opVATStates {
		fixtures = append(fixtures, opVATTenant(t, ctx, tx, "op16 "+s.name+" "+tok, s.verified, s.checked))
	}
	versions := func() []opVATVerdict {
		var out []opVATVerdict
		for _, f := range fixtures {
			out = append(out, opVATOf(t, ctx, tx, f.id))
		}
		return out
	}
	before := versions()
	for i, f := range fixtures {
		rows, err := opReadVAT(t, ctx, tx, hash, opForgeVAT(t, ctx, tx, session, a.id, f.id, xact), f.id)
		if err != nil {
			t.Fatalf("op_read_tenant_vat (%s): %v", opVATStates[i].name, err)
		}
		if len(rows) != 1 || !opVATSame(rows[0], f) {
			t.Errorf("the %s tenant's read: %d row(s), its own row = %v; want exactly its own row", opVATStates[i].name, len(rows), len(rows) == 1 && opVATSame(rows[0], f))
		}
		for _, r := range rows {
			for j, g := range fixtures {
				if j != i && (r.Number == g.number || r.TenantID == g.id) {
					t.Errorf("the %s tenant's read returned the %s tenant's row", opVATStates[i].name, opVATStates[j].name)
				}
			}
		}
		afterForge := opAudit(t, ctx, tx)
		raw := opForgeVAT(t, ctx, tx, session, a.id, f.id, xact)
		forged := opAudit(t, ctx, tx)
		var s TenantVATStatus
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			var e error
			s, e = readTenantVAT(ctx, sp, hash, readTicket{v: &raw}, f.id)
			return e
		}); err != nil {
			t.Fatalf("readTenantVAT (%s): %v", opVATStates[i].name, err)
		}
		if !opVATSame(s, f) {
			t.Errorf("the accessor's row for the %s tenant is not its own", opVATStates[i].name)
		}
		if d := opAudit(t, ctx, tx) - forged; d != 0 || forged-afterForge != 1 {
			t.Errorf("op_read_tenant_vat wrote %d audit row(s), want 0 (the one row of a read is phase one's)", d)
		}
	}
	for i, v := range versions() {
		if !v.same(before[i]) {
			t.Errorf("the reads rewrote the %s tenant's row", opVATStates[i].name)
		}
	}
}

// TestOpReadTenantVAT_AnUnknownTenantReadsNothing: an id that names no tenant reads ZERO rows --
// no error, so not a refusal (28000) and not an argument error (22023) -- and through the
// accessor's scan it is ErrNoSuchTenant, distinct from ErrOperatorRefused.
func TestOpReadTenantVAT_AnUnknownTenantReadsNothing(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	unknown := uuid.New()
	rows, err := opReadVAT(t, ctx, tx, hash, opForgeVAT(t, ctx, tx, session, a.id, unknown, xact), unknown)
	if err != nil || len(rows) != 0 {
		t.Errorf("an unknown tenant: %d row(s), err %v; want 0 rows and no error", len(rows), err)
	}
	raw := opForgeVAT(t, ctx, tx, session, a.id, unknown, xact)
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		_, e := readTenantVAT(ctx, sp, hash, readTicket{v: &raw}, unknown)
		if !errors.Is(e, ErrNoSuchTenant) || errors.Is(e, ErrOperatorRefused) {
			return fmt.Errorf("readTenantVAT of an unknown tenant: %v, want ErrNoSuchTenant", e)
		}
		return nil
	}); err != nil {
		t.Error(err)
	}
}

// TestOpReadTenantVAT_ATicketFromThisTransactionIsRefused is ADR 0021 §2 v 3's table, rows A1
// and A2, for this read: a ticket created in THIS transaction -- at the top level, or inside a
// savepoint (open, or released) -- is refused. CONTROL: the same session, kind and tenant
// through a ticket whose created_xact names a committed transaction is read.
func TestOpReadTenantVAT_ATicketFromThisTransactionIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := uuid.New()
	begin := func(q pgx.Tx) string {
		t.Helper()
		raw, err := opBegin(t, ctx, q, hash, tenantVATReadKind, opDetailParams(tenant.String()))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := begin(tx)
	_, err := opReadVAT(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantVATRefusal, "A1: a ticket of this transaction's top level", raw)
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw = begin(sp)
	_, err = opReadVAT(t, ctx, sp, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantVATRefusal, "A2: a ticket of an open savepoint", raw)
	if err := sp.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = opReadVAT(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantVATRefusal, "A2: a ticket of a released savepoint", raw)
	if _, err := opReadVAT(t, ctx, tx, hash, opForgeVAT(t, ctx, tx, session, a.id, tenant, opCommittedXact(t, ctx)), tenant); err != nil {
		t.Fatalf("CONTROL: a ticket whose transaction COMMITTED was refused: %v", err)
	}
}

// TestOpReadTenantVAT_AForgedTicketIsRefused: a ticket is bound to its session, its KIND and its
// tenant. Refused (28000, ticket not consumed): another session of the same operator; another
// tenant; a ticket no op_begin_read issued, and NULL; and the KIND condition's own case, which
// this kind makes load-bearing -- 'tenant_vat', 'tenant_detail' and 'tenant_plaques' hash the
// SAME {tenant_id} text, so an overview or inventory ticket carries exactly the hash this read
// computes: shown to the VAT read it is refused; the same hash under 'tenants' and
// 'legal_versions' is refused too; and the other direction, a VAT ticket shown to the overview
// and to the inventory, is refused. CONTROL: the same hash under 'tenant_vat' is read.
func TestOpReadTenantVAT_AForgedTicketIsRefused(t *testing.T) {
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
	ticket := opForgeVAT(t, ctx, tx, session, a.id, tenant, xact)
	_, err := opReadVAT(t, ctx, tx, otherHash, ticket, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantVATRefusal, "another session", ticket)
	_, err = opReadVAT(t, ctx, tx, hash, ticket, other)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantVATRefusal, "another tenant", ticket)
	for _, raw := range []any{opRandHex(t), nil} {
		err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			_, e := opScanVAT(ctx, sp, hash, raw, tenant)
			return e
		})
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantVATRefusal, fmt.Sprintf("a ticket never issued (%T)", raw))
	}
	for _, kind := range []string{tenantDetailReadKind, tenantPlaquesReadKind, tenantsReadKind, legalVersionsReadKind} {
		raw := opRandHex(t)
		opForgeRead(t, ctx, tx, session, a.id, kind, opDetailTicketHash(raw, tenant), &tenant, xact, "30 seconds")
		_, err := opReadVAT(t, ctx, tx, hash, raw, tenant)
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantVATRefusal, "the VAT read's own hash under kind '"+kind+"'", raw)
	}
	raw := opForgeVAT(t, ctx, tx, session, a.id, tenant, xact)
	_, err = opReadDetail(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantDetailRefusal, "op_read_tenant_detail with a 'tenant_vat' ticket", raw)
	_, err = opReadPlaques(t, ctx, tx, hash, raw, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantPlaquesRefusal, "op_read_tenant_plaques with a 'tenant_vat' ticket", raw)
	if n := consumed() - before; n != 0 {
		t.Errorf("%d ticket(s) consumed by refused reads", n)
	}
	if _, err := opReadVAT(t, ctx, tx, hash, ticket, tenant); err != nil {
		t.Errorf("CONTROL: the VAT ticket with its own session and tenant was refused: %v", err)
	}
	if _, err := opReadVAT(t, ctx, tx, hash, raw, tenant); err != nil {
		t.Errorf("CONTROL: the ticket the overview and the inventory refused is the VAT read's: %v", err)
	}
}

// TestOpReadTenantVAT_RefusesEveryDeadSession: the read resolves the session through
// op_touch_session before anything else, so each of the six dead sessions is the touch refusal
// (28000) and no ticket is consumed -- even one that would otherwise match.
func TestOpReadTenantVAT_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	xact := opCommittedXact(t, ctx)
	tenant := uuid.New()
	for _, d := range opDeadSessions(t, ctx, tx) {
		if d.id == uuid.Nil {
			_, err := opReadVAT(t, ctx, tx, d.hash, opRandHex(t), tenant)
			opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash)
			continue
		}
		var admin uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT admin_id FROM platform_sessions WHERE id = $1`, d.id).Scan(&admin); err != nil {
			t.Fatal(err)
		}
		raw := opForgeVAT(t, ctx, tx, d.id, admin, tenant, xact)
		_, err := opReadVAT(t, ctx, tx, d.hash, raw, tenant)
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash, raw)
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, d.id); n != 0 {
			t.Errorf("%s: %d ticket(s) consumed", d.name, n)
		}
	}
}

// TestOpReadTenantVAT_ExpiryIsTheWallClock is ADR 0021 §6 "bilet süresi" for the read: an expired
// ticket is refused, and a ticket that expires WHILE the reading transaction is open is refused
// -- through savepoints rolled back three times, and through three exception sub-transactions of
// ONE DO statement whose sleep is INSIDE it. CONTROL first: the same ticket, unexpired, is read
// (in a savepoint that is rolled back, so it stays unconsumed).
func TestOpReadTenantVAT_ExpiryIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := uuid.New()
	ticket := opForgeVAT(t, ctx, tx, session, a.id, tenant, opCommittedXact(t, ctx))
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
		_, e := opReadVAT(t, ctx, sp, hash, ticket, tenant)
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return e
	}
	if err := readThenUndo(); err != nil {
		t.Fatalf("CONTROL: the unexpired ticket was refused: %v", err)
	}
	expireIn("-1 second")
	opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, tenantVATRefusal, "an expired ticket", ticket)

	expireIn("1 second")
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, tenantVATRefusal,
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
			            PERFORM * FROM public.op_read_tenant_vat(current_setting('tappa_test.session'),
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

// --------------------------------------------------------- with real commits --

// TestOpReadTenantVAT_TwoPhaseLifecycle is ADR 0021 §2 v 3's table on the shipped read with REAL
// commits: the accessor's phase one (beginOperatorRead) is committed and its 'read' row -- scope
// 'tenant_vat', the tenant named -- is permanent; another session and another tenant are refused;
// the accessor's phase two (readTenantVAT) returns the row (a tenant written inside the read's
// own transaction, which is rolled back -- no tenant is committed); the rolled-back read leaves
// the row and -- ADR 0021 limit 4 -- lets the same ticket read again; a COMMITTED read of an id
// that names no committed tenant is ErrNoSuchTenant and consumes the ticket, after which it is
// refused; and through all of it the read has exactly ONE 'read' row.
func TestOpReadTenantVAT_TwoPhaseLifecycle(t *testing.T) {
	ctx, f := opLiveFixture(t)
	otherHash, otherSession := f.newSession(t, ctx)
	conn := f.connect(t, ctx)
	tenant := uuid.New()
	name := "op16 lifecycle " + opToken(t)
	number := "VAT-OP16-" + tenant.String()

	tx := asOperatorTx(t, ctx, conn)
	ticket, err := beginOperatorRead(ctx, tx, f.hash, tenantVATReadKind, []byte(opDetailParams(tenant.String())))
	if err != nil {
		t.Fatalf("phase one: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit phase one: %v", err)
	}
	scoped := func() int64 {
		return opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1
		                                 AND target_scope = 'tenant_vat' AND target_tenant_id = $2`, f.session, tenant)
	}
	if n := scoped(); n != 1 {
		t.Fatalf("after the committed phase one: %d 'tenant_vat' row(s) naming the tenant, want 1", n)
	}
	refused := func(what, hash string, id uuid.UUID) {
		t.Helper()
		rtx := asOperatorTx(t, ctx, conn)
		_, e := opScanVAT(ctx, rtx, hash, ticket.reveal(), id)
		opWantClean(t, e, sqlstateInvalidAuthorization, tenantVATRefusal, what)
		if err := rtx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refused("the ticket from another session of the same operator", otherHash, tenant)
	refused("the ticket for another tenant", f.hash, uuid.New())

	read := func(commit bool) (TenantVATStatus, error) {
		t.Helper()
		rtx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rtx.Rollback(context.Background()) }()
		if !commit {
			if _, err := rtx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, vat_verified, vat_checked_at)
			                            VALUES ($1, $2, $3, 'hotel', 'single', false, '2025-01-02 03:04:05+00')`, tenant, name, number); err != nil {
				t.Fatalf("fixture: %v", err)
			}
		}
		if _, err := rtx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
			t.Fatal(err)
		}
		s, rerr := readTenantVAT(ctx, rtx, f.hash, ticket, tenant)
		if commit {
			if err := rtx.Commit(ctx); err != nil {
				t.Fatalf("commit the read: %v", err)
			}
		}
		return s, rerr
	}
	s, err := read(false)
	if err != nil || s.TenantID != tenant || s.TenantName != name || s.Number != number || s.Verified == nil || *s.Verified ||
		s.CheckedAt == nil || !s.CheckedAt.Equal(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("the read (rolled back) returned the fixture tenant = %v, err %v; want its id, name, number, false and its time",
			s.TenantID == tenant && s.Number == number, err)
	}
	if n := scoped(); n != 1 {
		t.Errorf("after the rolled-back read: %d 'read' row(s), want the 1 committed by phase one", n)
	}
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

// TestTenantVAT_OnThePoolTheTwoPhasesAreTwoTransactions: on a pool built by the production
// constructor (*pgxpool.Pool -- what *OperatorDB holds) TenantVAT's two phases are two
// transactions: for an id that names NO tenant the answer is ErrNoSuchTenant AFTER the 'read' row
// naming it committed. Inside one transaction it is ErrOperatorRefused; an unknown session is
// ErrOperatorRefused and writes nothing. Then a committed tenant (the database's oldest; this
// test writes none): the row is the owner's read of it, field for field -- compared, never
// printed. And RecordTenantVATCheck on the pool with an unknown session is ErrOperatorRefused and
// commits nothing. Round 3: after TenantVAT of that committed tenant, RecordTenantVATCheck on the
// same pool and session passes the binding to a committed read (OP-16 B's flow) -- in a
// transaction that is ROLLED BACK, with a number that is not the tenant's, so no verdict and no
// row is committed -- and for a tenant the session never read it is the binding's 22023. It
// commits two 'read' rows.
func TestTenantVAT_OnThePoolTheTwoPhasesAreTwoTransactions(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("open the operator pool: %v", err)
	}
	defer o.Close()

	unknown := uuid.New()
	if _, err := TenantVAT(ctx, o.pool, f.hash, unknown); !errors.Is(err, ErrNoSuchTenant) {
		t.Errorf("TenantVAT of an unknown tenant on the pool: %v, want ErrNoSuchTenant", err)
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1
	                                 AND target_scope = 'tenant_vat' AND target_tenant_id = $2`, f.session, unknown); n != 1 {
		t.Errorf("the VAT read of an unknown tenant committed %d 'read' row(s) naming it, want 1", n)
	}
	tx := asOperatorTx(t, ctx, f.connect(t, ctx))
	if _, err := TenantVAT(ctx, tx, f.hash, unknown); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantVAT inside ONE transaction: %v, want ErrOperatorRefused", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := TenantVAT(ctx, o.pool, opRandHex(t), unknown); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantVAT with an unknown session: %v, want ErrOperatorRefused", err)
	}
	writes := func() int64 {
		return opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'tenant_vat_checked' AND target_tenant_id = $1`, unknown)
	}
	if err := RecordTenantVATCheck(ctx, o.pool, opRandHex(t), unknown, "VAT-OP16-ZZ", true); !errors.Is(err, ErrOperatorRefused) || writes() != 0 {
		t.Errorf("RecordTenantVATCheck with an unknown session on the pool: %v, %d row(s); want ErrOperatorRefused and none", err, writes())
	}
	if n := f.liveReads(t, ctx, f.session); n != 1 {
		t.Errorf("the session has %d committed 'read' row(s), want 1 (the refused calls none)", n)
	}

	var id uuid.UUID
	if err := f.owner.QueryRow(ctx, `SELECT id FROM tenants ORDER BY created_at, id LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("PREMISE: no committed tenant to read: %v", err)
	}
	s, err := TenantVAT(ctx, o.pool, f.hash, id)
	if err != nil {
		t.Fatalf("TenantVAT of a committed tenant: %v", err)
	}
	var want TenantVATStatus
	if err := f.owner.QueryRow(ctx, `SELECT id, name, vat_number, vat_verified, vat_checked_at FROM tenants WHERE id = $1`, id).
		Scan(&want.TenantID, &want.TenantName, &want.Number, &want.Verified, &want.CheckedAt); err != nil {
		t.Fatal(err)
	}
	if s.TenantID != want.TenantID || s.TenantName != want.TenantName || s.Number != want.Number ||
		!opSameBool(s.Verified, want.Verified) || !opSameTime(s.CheckedAt, want.CheckedAt) {
		t.Error("TenantVAT's row for a committed tenant is not the owner's read of it (values not printed)")
	}
	if n := f.liveReads(t, ctx, f.session); n != 2 {
		t.Errorf("the session has %d committed 'read' row(s), want 2", n)
	}

	// OP-16 B's flow meets round 3's binding by construction: TenantVAT committed its read, so
	// RecordTenantVATCheck on the same pool and session is past the binding -- void. It runs in
	// a transaction that is ROLLED BACK, with a number that is not the tenant's (nothing of the
	// tenant is written; the operator row goes with the rollback). CONTROL: for a committed
	// tenant this session never read, the same call is the binding's 22023.
	unregistered := "VAT-OP16-UNREGISTERED-" + uuid.NewString()
	write := func(tenant uuid.UUID) error {
		t.Helper()
		ptx, err := o.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ptx.Rollback(context.Background()) }()
		return RecordTenantVATCheck(ctx, ptx, f.hash, tenant, unregistered, true)
	}
	if err := write(id); err != nil {
		t.Errorf("OP-16 B's flow (TenantVAT, then RecordTenantVATCheck) did not pass the binding: %v", err)
	}
	var unread uuid.UUID
	if err := f.owner.QueryRow(ctx, `SELECT id FROM tenants WHERE id <> $1 ORDER BY created_at, id LIMIT 1`, id).Scan(&unread); err != nil {
		t.Fatalf("PREMISE: no second committed tenant: %v", err)
	}
	if err := write(unread); err == nil || !strings.Contains(err.Error(), "SQLSTATE 22023") {
		t.Errorf("RecordTenantVATCheck for a tenant this session never read: %v; want the binding's 22023", err)
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'tenant_vat_checked' AND session_id = $1`, f.session); n != 0 {
		t.Errorf("the rolled-back writes left %d committed operator row(s)", n)
	}
}

// --------------------------------------------------------------- the write --

// TestOpRecordVATCheck_ANullVerdictIsRefusedAndWritesNothing is §4.6 in the database: "VIES did
// not answer" cannot be recorded. A NULL verdict -- and a NULL tenant or number -- is SQLSTATE
// 22023 with one constant message and no DETAIL, no error field carries the number, and nothing
// is written: the tenant's verdict and row version are as they were, no tenant row (anywhere),
// no operator row. The session is resolved first: a dead session with a NULL verdict is the
// session's refusal (28000).
func TestOpRecordVATCheck_ANullVerdictIsRefusedAndWritesNothing(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := opVATTenant(t, ctx, tx, "op16 null "+opToken(t), nil, "2025-03-04 05:06:07.123456+00")
	before, actions := opVATOf(t, ctx, tx, f.id), opVATActions(t, ctx, tx)
	for _, c := range []struct {
		what                  string
		tenant, number, valid any
	}{
		{"a NULL verdict (VIES did not answer)", f.id, f.number, nil},
		{"a NULL tenant", nil, f.number, true},
		{"a NULL number", f.id, nil, false},
		{"all three NULL", nil, nil, nil},
	} {
		opWantClean(t, opRecordVAT(t, ctx, tx, hash, c.tenant, c.number, c.valid), sqlstateInvalidParameter, vatCheckRefusal, c.what, f.number, hash)
	}
	if after := opVATOf(t, ctx, tx, f.id); !after.same(before) {
		t.Error("a refused call changed the tenant's verdict or rewrote its row")
	}
	if d := opVATActions(t, ctx, tx) - actions; d != 0 {
		t.Errorf("refused calls wrote %d tenant row(s)", d)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1`, session); n != 0 {
		t.Errorf("refused calls wrote %d operator row(s)", n)
	}
	dead := opDeadSessions(t, ctx, tx)[0]
	opWantClean(t, opRecordVAT(t, ctx, tx, dead.hash, f.id, f.number, nil), sqlstateInvalidAuthorization, touchRefusal00026,
		"a dead session with a NULL verdict (the session first)", dead.hash)
}

// TestOpRecordVATCheck_IsBoundToACommittedReadOfTheTenant is round 3's binding (the third eye's
// D1; the orchestrator's decision): the write runs only after THIS session read THIS tenant's VAT
// number in an EARLIER, COMMITTED transaction -- op_begin_read's 'read' row and 'tenant_vat'
// ticket, the ticket's created_xact committed (op_read_tenant_vat's own test). Each refusal is
// the one 22023 with the constant message and no DETAIL, no field carrying the number, the tenant
// id or the session hash, and it writes nothing (verdict, row version, tenant row, operator row):
//   - no read at all;
//   - a committed read of ANOTHER tenant by this session;
//   - a committed read of this tenant of ANOTHER kind (the overview, 'tenant_detail');
//   - a committed read of this tenant by ANOTHER session of the same operator, and by ANOTHER
//     operator;
//   - a read of this tenant opened in THIS transaction (op_begin_read called for real here);
//   - THE STATISTICS of a refusal, rolled back inside a savepoint: for an existing tenant and for
//     an absent one every counter of pg_stat_xact_user_tables moves alike, and tenants' not at
//     all -- the refusal reads, locks and writes nothing of tenants;
//   - CONTROL: a committed read of this tenant by this session whose ticket has EXPIRED and was
//     CONSUMED -- the bound is the session, not the ticket's lifetime (OP-16 B asks VIES between
//     the read and the write) -- and the call records the verdict with its two rows.
func TestOpRecordVATCheck_IsBoundToACommittedReadOfTheTenant(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	_, otherSession := opNewSession(t, ctx, tx, a.id, true)
	b := opNewActive(t, ctx, tx)
	_, bSession := opNewSession(t, ctx, tx, b.id, true)
	tok := opToken(t)
	f := opVATTenant(t, ctx, tx, "op16 bound "+tok, nil, "")
	g := opVATTenant(t, ctx, tx, "op16 bound other "+tok, nil, "")
	xact := opCommittedXact(t, ctx)
	before, actions := opVATOf(t, ctx, tx, f.id), opVATActions(t, ctx, tx)
	refused := func(what string) {
		t.Helper()
		opWantClean(t, opRecordVAT(t, ctx, tx, hash, f.id, f.number, true), sqlstateInvalidParameter, vatCheckRefusal,
			what, f.number, f.id.String(), hash)
		if !opVATOf(t, ctx, tx, f.id).same(before) || opVATActions(t, ctx, tx) != actions || opVATOperatorRows(t, ctx, tx, session, f.id) != 0 {
			t.Errorf("%s: the refused call wrote something", what)
		}
	}
	refused("no read at all")
	opForgeVAT(t, ctx, tx, session, a.id, g.id, xact)
	refused("a committed read of another tenant by this session")
	opForgeDetail(t, ctx, tx, session, a.id, f.id, xact)
	refused("a committed read of this tenant's overview (another kind)")
	opForgeVAT(t, ctx, tx, otherSession, a.id, f.id, xact)
	refused("a committed read of this tenant by another session of the same operator")
	opForgeVAT(t, ctx, tx, bSession, b.id, f.id, xact)
	refused("a committed read of this tenant by another operator")
	if _, err := opBegin(t, ctx, tx, hash, tenantVATReadKind, opDetailParams(f.id.String())); err != nil {
		t.Fatalf("op_begin_read in this transaction: %v", err)
	}
	refused("a read of this tenant opened in this transaction")

	// THE STATISTICS of a refusal: an existing tenant and an absent one, unbound alike.
	absent := uuid.New()
	call := func(tenant uuid.UUID) map[string]opStatCounters {
		t.Helper()
		s0 := opStatXact(t, ctx, tx)
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		opWantClean(t, opRecordVAT(t, ctx, sp, hash, tenant, f.number, true), sqlstateInvalidParameter, vatCheckRefusal,
			"a measured refusal", f.number, tenant.String(), hash)
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return opStatDelta(s0, opStatXact(t, ctx, tx))
	}
	for range 6 {
		call(f.id)
		call(absent)
	}
	present, missing := call(f.id), call(absent)
	t.Logf("OP16-BIND: a refusal for an existing tenant: %s", opStatText(present))
	t.Logf("OP16-BIND: a refusal for an absent tenant: %s", opStatText(missing))
	if opStatText(present) != opStatText(missing) {
		t.Errorf("a refusal moves the counters differently for an existing tenant (%s) and an absent one (%s)", opStatText(present), opStatText(missing))
	}
	if c, ok := present["public.tenants"]; ok {
		t.Errorf("a refusal touched tenants: %+v; the binding comes before every reference to it", c)
	}

	// CONTROL: a committed read by this session, its ticket expired and consumed.
	ticket := opForgeRead(t, ctx, tx, session, a.id, tenantVATReadKind, opRandHex(t), &f.id, xact, "0 seconds")
	if _, err := tx.Exec(ctx, `UPDATE operator_read_tickets SET consumed_at = clock_timestamp() WHERE id = $1`, ticket); err != nil {
		t.Fatalf("consume the ticket: %v", err)
	}
	if err := opRecordVATAccessor(t, ctx, tx, hash, f.id, f.number, true); err != nil {
		t.Fatalf("CONTROL: the call after a committed read of the tenant: %v", err)
	}
	if now := opVATOf(t, ctx, tx, f.id); now.verified == nil || !*now.verified || len(opVATTrail(t, ctx, tx, f.id)) != 1 ||
		opVATOperatorRows(t, ctx, tx, session, f.id) != 1 {
		t.Error("CONTROL: the bound call did not record the verdict with its two rows")
	}
}

// TestOpRecordVATCheck_TheDetailIsTheBeforeAndAfterInEveryStartingState: for each of 00017's four
// starting states and each verdict (true, false), through the PRODUCT's accessor, with the
// caller's TimeZone set far from UTC:
//   - the tenant now holds the verdict and a time inside the call (the database's wall clock);
//   - exactly one tenant row: action 'tenant.vat_rechecked', the operator as actor_id, the
//     tenant's id as target, `at` equal to the new vat_checked_at (one clock read), and the
//     detail EXACTLY {"actor_kind": "operator", "before": {...}, "after": {...}} -- the verdicts
//     as booleans or null, the times as UTC text with six digits and a Z (not the caller's zone),
//     no other key, no number;
//   - exactly one operator row: 'tenant_vat_checked', the session, the operator, the tenant, no
//     account, scope or page, detail {};
//   - a second call on the same tenant chains: its "before" is the first call's "after".
func TestOpRecordVATCheck_TheDetailIsTheBeforeAndAfterInEveryStartingState(t *testing.T) {
	ctx, tx := opTx(t)
	if _, err := tx.Exec(ctx, `SET LOCAL TimeZone = 'Pacific/Kiritimati'`); err != nil {
		t.Fatal(err)
	}
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	for _, s := range opVATStates {
		for _, verdict := range []bool{true, false} {
			what := s.name + " -> " + strconv.FormatBool(verdict)
			f := opVATTenant(t, ctx, tx, "op16 detail "+what+" "+tok, s.verified, s.checked)
			opVATReadFor(t, ctx, tx, hash, f.id)
			start := opClock(t, ctx, tx)
			if err := opRecordVATAccessor(t, ctx, tx, hash, f.id, f.number, verdict); err != nil {
				t.Fatalf("%s: RecordTenantVATCheck: %v", what, err)
			}
			end := opClock(t, ctx, tx)
			now := opVATOf(t, ctx, tx, f.id)
			if now.verified == nil || *now.verified != verdict || now.checked == nil || now.checked.Before(start) || now.checked.After(end) {
				t.Errorf("%s: the tenant holds verified=%v checked=%v; want %v and a time inside [%v, %v]", what, now.verified, now.checked, verdict, start, end)
				continue
			}
			trail := opVATTrail(t, ctx, tx, f.id)
			if len(trail) != 1 {
				t.Errorf("%s: %d tenant row(s), want 1", what, len(trail))
				continue
			}
			r := trail[0]
			if r.actor == nil || *r.actor != a.id || r.target == nil || *r.target != f.id.String() || !r.at.Equal(*now.checked) {
				t.Errorf("%s: the tenant row's actor is the operator=%v, target the tenant=%v, at the verdict's time=%v", what,
					r.actor != nil && *r.actor == a.id, r.target != nil && *r.target == f.id.String(), r.at.Equal(*now.checked))
			}
			if want := opVATDetailText(f.verified, f.checked, verdict, *now.checked); r.detail != want {
				t.Errorf("%s: the tenant row's detail is\n %s\n want\n %s", what, r.detail, want)
			}
			if strings.Contains(r.detail, f.number) {
				t.Errorf("%s: the tenant row's detail carries the number", what)
			}
			if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log
			                            WHERE kind = 'tenant_vat_checked' AND session_id = $1 AND actor_admin_id = $2 AND target_tenant_id = $3
			                              AND target_admin_id IS NULL AND target_scope IS NULL AND page_number IS NULL AND page_size IS NULL
			                              AND detail = '{}'::jsonb`, session, a.id, f.id); n != 1 {
				t.Errorf("%s: %d operator row(s) of the exact shape, want 1", what, n)
			}
			if s.name == "valid" && verdict {
				first := now
				if err := opRecordVATAccessor(t, ctx, tx, hash, f.id, f.number, false); err != nil {
					t.Fatalf("a second call: %v", err)
				}
				second := opVATOf(t, ctx, tx, f.id)
				trail := opVATTrail(t, ctx, tx, f.id)
				if len(trail) != 2 || second.checked == nil || trail[1].detail != opVATDetailText(first.verified, first.checked, false, *second.checked) {
					t.Errorf("a second call does not chain: %d row(s); its before is not the first call's after", len(trail))
				}
			}
		}
	}
}

// TestOpRecordVATCheck_AMismatchedNumberChangesNothingAndLeavesTheOperatorsRow is the number
// binding (the card's T3): the write is bound to the number the operator saw. For a tenant and a
// number that is not its own -- another tenant's, a random one, its own in lower case, its own
// with a trailing space, an empty one, and (the third eye's S2) "%" and its own with the last
// character as "_", which a LIKE would match -- the call returns void, the tenant's verdict and row
// version are unchanged, neither it nor the other tenant gets a tenant row, the other tenant is
// unchanged, and the operator's row naming the tenant is written each time.
func TestOpRecordVATCheck_AMismatchedNumberChangesNothingAndLeavesTheOperatorsRow(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	f := opVATTenant(t, ctx, tx, "op16 mismatch "+tok, opVATBool(true), "2025-06-07 08:09:10.654321+00")
	other := opVATTenant(t, ctx, tx, "op16 mismatch other "+tok, nil, "")
	opVATReadFor(t, ctx, tx, hash, f.id)
	before, otherBefore, actions := opVATOf(t, ctx, tx, f.id), opVATOf(t, ctx, tx, other.id), opVATActions(t, ctx, tx)
	cases := []struct{ what, number string }{
		{"another tenant's number", other.number},
		{"a random number", "VAT-OP16-" + uuid.NewString()},
		{"its own number in lower case", strings.ToLower(f.number)},
		{"its own number with a trailing space", f.number + " "},
		{"an empty number", ""},
		// The third eye's S2: a guard that compared by LIKE would take these as a match.
		{"a LIKE pattern matching every number", "%"},
		{"its own number with its last character as a LIKE wildcard", f.number[:len(f.number)-1] + "_"},
	}
	for i, c := range cases {
		if err := opRecordVATAccessor(t, ctx, tx, hash, f.id, c.number, false); err != nil {
			t.Errorf("%s: %v, want the same void as a match", c.what, err)
		}
		if n := opVATOperatorRows(t, ctx, tx, session, f.id); n != int64(i+1) {
			t.Errorf("%s: %d operator row(s) naming the tenant, want %d", c.what, n, i+1)
		}
	}
	if after := opVATOf(t, ctx, tx, f.id); !after.same(before) {
		t.Error("a mismatched number changed the tenant's verdict or rewrote its row")
	}
	if after := opVATOf(t, ctx, tx, other.id); !after.same(otherBefore) {
		t.Error("a call naming the other tenant's number changed the other tenant")
	}
	if d := opVATActions(t, ctx, tx) - actions; d != 0 {
		t.Errorf("mismatched calls wrote %d tenant row(s), want 0", d)
	}
	if err := opRecordVATAccessor(t, ctx, tx, hash, f.id, f.number, false); err != nil {
		t.Fatalf("CONTROL: the matching number: %v", err)
	}
	if after := opVATOf(t, ctx, tx, f.id); after.verified == nil || *after.verified || len(opVATTrail(t, ctx, tx, f.id)) != 1 {
		t.Error("CONTROL: the matching number did not record the verdict with one tenant row")
	}
}

// TestOpRecordVATCheck_AnUnknownTenantIsTheSameVoid is B14: a tenant that does not exist gets
// the same void as one that does, no tenant row anywhere (no foreign key fires), no tenant
// changes, and the operator's row naming the id is written. And THE STATISTICS a call leaves
// when it is rolled back inside a savepoint (the card's T10, ADR 0021 limit 15; the third eye's
// F1, round 2), MEASURED in pg_stat_xact_user_tables:
//   - CLOSED, PINNED: for an unknown tenant and for a known one, an UNREGISTERED number and
//     ANOTHER tenant's number move every counter of every table alike. Round 1 named the number
//     in its statements, the planner answered them from tenants_vat_number_key, and tenants'
//     idx_tup_fetch moved only when the number was registered to SOME tenant -- a cross-tenant
//     membership test with no committed trail;
//   - COUNTED, PINNED AT THE MEASURED VALUES: whether the id names a tenant, and for one that
//     does whether the number is its own, stay visible in tenants' idx_tup_fetch and n_tup_upd
//     (the operator reads the tenant's number through the audited read anyway).
func TestOpRecordVATCheck_AnUnknownTenantIsTheSameVoid(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := opVATTenant(t, ctx, tx, "op16 b14 "+opToken(t), nil, "")
	unknown := uuid.New()
	// op_begin_read does not look the tenant up (B14), so a committed read can name any id.
	opVATReadFor(t, ctx, tx, hash, f.id, unknown)
	start := opClock(t, ctx, tx)
	actions := opVATActions(t, ctx, tx)
	for _, number := range []string{f.number, "VAT-OP16-" + unknown.String()} {
		if err := opRecordVATAccessor(t, ctx, tx, hash, unknown, number, true); err != nil {
			t.Errorf("an unknown tenant: %v, want the same void as a known one", err)
		}
	}
	if d := opVATActions(t, ctx, tx) - actions; d != 0 {
		t.Errorf("calls for an unknown tenant wrote %d tenant row(s)", d)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tenants WHERE vat_checked_at >= $1`, start); n != 0 {
		t.Errorf("calls for an unknown tenant changed %d tenant(s)", n)
	}
	if n := opVATOperatorRows(t, ctx, tx, session, unknown); n != 2 {
		t.Errorf("%d operator row(s) naming the unknown id, want 2", n)
	}

	// THE STATISTICS: what a rolled-back call leaves in this transaction's counters.
	g := opVATTenant(t, ctx, tx, "op16 b14 other "+opToken(t), nil, "")
	unregistered := "VAT-OP16-UNREGISTERED-" + uuid.NewString()
	cases := []struct {
		what   string
		tenant uuid.UUID
		number string
	}{
		{"an unknown tenant, an unregistered number", unknown, unregistered},
		{"an unknown tenant, another tenant's number", unknown, g.number},
		{"a known tenant, an unregistered number", f.id, unregistered},
		{"a known tenant, another tenant's number", f.id, g.number},
		{"a known tenant, its own number", f.id, f.number},
	}
	call := func(tenant uuid.UUID, number string) map[string]opStatCounters {
		t.Helper()
		before := opStatXact(t, ctx, tx)
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := opRecordVATAccessor(t, ctx, sp, hash, tenant, number, false); err != nil {
			t.Fatalf("a measured call: %v", err)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return opStatDelta(before, opStatXact(t, ctx, tx))
	}
	// Warm-up: every statement of the function past PostgreSQL's five custom plans, so no case
	// meets a plan the case before it did not.
	for range 6 {
		for _, c := range cases {
			call(c.tenant, c.number)
		}
	}
	measured := make([]map[string]opStatCounters, len(cases))
	for i, c := range cases {
		measured[i] = call(c.tenant, c.number)
		if again := call(c.tenant, c.number); opStatText(again) != opStatText(measured[i]) {
			t.Fatalf("PREMISE: %s moved the counters differently twice (%s, then %s)", c.what, opStatText(measured[i]), opStatText(again))
		}
		t.Logf("OP16-STATS: %s: %s", c.what, opStatText(measured[i]))
	}
	// CLOSED: the number argument moves nothing beyond "is it this tenant's number".
	for _, pair := range [][2]int{{0, 1}, {2, 3}} {
		if a, b := opStatText(measured[pair[0]]), opStatText(measured[pair[1]]); a != b {
			t.Errorf("THE NUMBER LEAKS: %s moved %s, %s moved %s -- a statement reads by the number", cases[pair[0]].what, a, cases[pair[1]].what, b)
		}
	}
	// COUNTED: the tenant's existence and the match, at the measured values.
	for i, want := range []struct{ fetch, upd int64 }{{0, 0}, {0, 0}, {1, 0}, {1, 0}, {3, 1}} {
		got := measured[i]["public.tenants"]
		if got.idxTupFetch != want.fetch || got.nTupUpd != want.upd {
			t.Errorf("COUNTED LIMIT moved: after %s rolled back, tenants' idx_tup_fetch moved by %d and n_tup_upd by %d; measured was %d and %d -- update ADR 0021's OP-16 note",
				cases[i].what, got.idxTupFetch, got.nTupUpd, want.fetch, want.upd)
		}
	}
}

// opStatCounters are one table's counters in pg_stat_xact_user_tables that a statement's PLAN
// decides -- scans, tuples read and fetched, rows inserted, updated, deleted. n_tup_hot_upd and
// n_tup_newpage_upd are left out: whether an UPDATE stays on its page depends on the free space
// the rolled-back versions before it left, not on the arguments.
type opStatCounters struct {
	seqScan, seqTupRead, idxScan, idxTupFetch, nTupIns, nTupUpd, nTupDel int64
}

// opStatXact is this transaction's counters, by schema-qualified table.
func opStatXact(t *testing.T, ctx context.Context, q opQuerier) map[string]opStatCounters {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT schemaname || '.' || relname, seq_scan, seq_tup_read, coalesce(idx_scan, 0),
	                                  coalesce(idx_tup_fetch, 0), n_tup_ins, n_tup_upd, n_tup_del
	                             FROM pg_stat_xact_user_tables`)
	if err != nil {
		t.Fatalf("read pg_stat_xact_user_tables: %v", err)
	}
	defer rows.Close()
	out := map[string]opStatCounters{}
	for rows.Next() {
		var name string
		var c opStatCounters
		if err := rows.Scan(&name, &c.seqScan, &c.seqTupRead, &c.idxScan, &c.idxTupFetch, &c.nTupIns, &c.nTupUpd, &c.nTupDel); err != nil {
			t.Fatalf("scan pg_stat_xact_user_tables: %v", err)
		}
		out[name] = c
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read pg_stat_xact_user_tables: %v", err)
	}
	return out
}

// opStatDelta is after minus before, the tables that did not move left out.
func opStatDelta(before, after map[string]opStatCounters) map[string]opStatCounters {
	out := map[string]opStatCounters{}
	for name, a := range after {
		b := before[name]
		d := opStatCounters{a.seqScan - b.seqScan, a.seqTupRead - b.seqTupRead, a.idxScan - b.idxScan,
			a.idxTupFetch - b.idxTupFetch, a.nTupIns - b.nTupIns, a.nTupUpd - b.nTupUpd, a.nTupDel - b.nTupDel}
		if d != (opStatCounters{}) {
			out[name] = d
		}
	}
	return out
}

// opStatText renders a delta in one comparable line, tables in name order.
func opStatText(d map[string]opStatCounters) string {
	names := make([]string, 0, len(d))
	for name := range d {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		c := d[name]
		fmt.Fprintf(&b, "%s{seq %d/%d idx %d/%d ins %d upd %d del %d} ", name, c.seqScan, c.seqTupRead, c.idxScan, c.idxTupFetch, c.nTupIns, c.nTupUpd, c.nTupDel)
	}
	return strings.TrimSpace(b.String())
}

// TestOpRecordVATCheck_TheAnswerIsTheSameWhateverTheTenantsState is ADR 0021 §2 v 7 for the
// write: the answer does not depend on what the tenant held. For a tenant in each of 00017's
// four states (with its number), a mismatched number and an unknown tenant, the shipped
// statement answers the same command tag and no error, and the accessor nil; a dead session is
// the same 28000 for each, and a NULL verdict the same 22023.
func TestOpRecordVATCheck_TheAnswerIsTheSameWhateverTheTenantsState(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	dead := opDeadSessions(t, ctx, tx)[3] // revoked
	tok := opToken(t)
	type target struct {
		what   string
		tenant uuid.UUID
		number string
	}
	var targets []target
	for _, s := range opVATStates {
		f := opVATTenant(t, ctx, tx, "op16 answer "+s.name+" "+tok, s.verified, s.checked)
		targets = append(targets, target{s.name, f.id, f.number})
	}
	targets = append(targets, target{"a mismatched number", targets[0].tenant, targets[1].number},
		target{"an unknown tenant", uuid.New(), "VAT-OP16-" + uuid.NewString()})
	for _, c := range targets {
		opVATReadFor(t, ctx, tx, hash, c.tenant)
	}
	tags := map[string]bool{}
	for _, c := range targets {
		var tag pgconn.CommandTag
		err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			var e error
			tag, e = sp.Exec(ctx, recordTenantVATCheckSQL, hash, c.tenant, c.number, true)
			return e
		})
		if err != nil {
			t.Errorf("%s: %v", c.what, err)
		}
		tags[tag.String()] = true
		if err := opRecordVATAccessor(t, ctx, tx, hash, c.tenant, c.number, false); err != nil {
			t.Errorf("%s: the accessor answered %v, want nil", c.what, err)
		}
		opWantClean(t, opRecordVAT(t, ctx, tx, dead.hash, c.tenant, c.number, true), sqlstateInvalidAuthorization, touchRefusal00026,
			c.what+", a dead session", dead.hash)
		opWantClean(t, opRecordVAT(t, ctx, tx, hash, c.tenant, c.number, nil), sqlstateInvalidParameter, vatCheckRefusal,
			c.what+", a NULL verdict")
	}
	if len(tags) != 1 {
		t.Errorf("the write answered %d different command tags across the tenant's states: %v", len(tags), tags)
	}
}

// TestOpRecordVATCheck_TheTenantRowsTimeIsTheWallClock is K6's `at` (ADR 0021 §2 vii, "Yazılan
// zaman damgaları"): a write inside ONE DO statement that first sleeps 1.5 s -- the sleep INSIDE
// the statement, so the transaction's and the statement's start are both left behind -- leaves
// the tenant row's `at`, the tenant's vat_checked_at and the detail's "after" time all equal and
// inside the call: at least 1.4 s after the transaction's start and after the statement's start
// (read before it), at most the wall clock after it. A frozen clock (now(), statement_timestamp())
// would date them before the sleep.
func TestOpRecordVATCheck_TheTenantRowsTimeIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	f := opVATTenant(t, ctx, tx, "op16 clock "+opToken(t), nil, "")
	opVATReadFor(t, ctx, tx, hash, f.id)
	before := opClock(t, ctx, tx)
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('tappa_test.session', $1, true), set_config('tappa_test.tenant', $2, true),
		                                  set_config('tappa_test.number', $3, true)`, hash, f.id.String(), f.number); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `
			DO $d$
			BEGIN
			    PERFORM pg_sleep(1.5);
			    PERFORM public.op_record_vat_check(current_setting('tappa_test.session'),
			                                       current_setting('tappa_test.tenant')::uuid,
			                                       current_setting('tappa_test.number'), true);
			END
			$d$`)
		return err
	}); err != nil {
		t.Fatalf("the DO block: %v", err)
	}
	after := opClock(t, ctx, tx)
	trail := opVATTrail(t, ctx, tx, f.id)
	now := opVATOf(t, ctx, tx, f.id)
	if len(trail) != 1 || now.checked == nil {
		t.Fatalf("PREMISE: %d tenant row(s), verdict time set=%v; want 1 and set", len(trail), now.checked != nil)
	}
	at := trail[0].at
	var sinceStart float64
	if err := tx.QueryRow(ctx, `SELECT extract(epoch FROM ($1::timestamptz - transaction_timestamp()))::float8`, at).Scan(&sinceStart); err != nil {
		t.Fatal(err)
	}
	if at.Before(before.Add(1400*time.Millisecond)) || at.After(after) || sinceStart < 1.4 {
		t.Errorf("the tenant row is dated %v, %.3f s after the transaction's start; want inside [%v + 1.4 s, %v] (the wall clock, not a frozen one)",
			at, sinceStart, before, after)
	}
	if !at.Equal(*now.checked) || trail[0].detail != opVATDetailText(nil, nil, true, at) {
		t.Error("the row's `at`, the tenant's vat_checked_at and the detail's \"after\" time are not one clock read")
	}
}

// TestOpRecordVATCheck_TheThreeWritesShareOneTransaction is K6's "same transaction" (the
// emsal: internal/handler's TestManualDB_TheRecordAndItsAuditRowSHAREATransaction):
//   - (i) the calling transaction rolled back takes all three writes with it -- the verdict, the
//     tenant row, the operator row;
//   - (ii) the tenant row refused (the definer's INSERT on audit_log revoked, inside a savepoint):
//     the call fails (42501) and neither the verdict nor the operator row is there;
//   - (iii) the operator row refused (its INSERT column revoked): the same, no verdict and no
//     tenant row;
//   - (iv) the operator row refused by a CHECK (added NOT VALID inside a savepoint): the caught
//     constraint is the one 22023, no DETAIL, no number in any field -- and nothing is written.
func TestOpRecordVATCheck_TheThreeWritesShareOneTransaction(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	f := opVATTenant(t, ctx, tx, "op16 one tx "+opToken(t), opVATBool(false), "2025-09-10 11:12:13.000001+00")
	opVATReadFor(t, ctx, tx, hash, f.id)
	before := opVATOf(t, ctx, tx, f.id)
	nothing := func(q pgx.Tx, what string) {
		t.Helper()
		if now := opVATOf(t, ctx, q, f.id); !now.same(before) {
			t.Errorf("%s: the verdict was written without its rows", what)
		}
		if n := len(opVATTrail(t, ctx, q, f.id)); n != 0 {
			t.Errorf("%s: %d tenant row(s) without the rest", what, n)
		}
		if n := opVATOperatorRows(t, ctx, q, session, f.id); n != 0 {
			t.Errorf("%s: %d operator row(s) without the rest", what, n)
		}
	}

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := opRecordVATAccessor(t, ctx, sp, hash, f.id, f.number, true); err != nil {
		t.Fatalf("(i) the call: %v", err)
	}
	if now := opVATOf(t, ctx, sp, f.id); now.verified == nil || !*now.verified || len(opVATTrail(t, ctx, sp, f.id)) != 1 ||
		opVATOperatorRows(t, ctx, sp, session, f.id) != 1 {
		t.Fatal("PREMISE (i): the call did not write its three writes")
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	nothing(tx, "(i) after the calling transaction rolled back")

	for _, c := range []struct {
		what, ddl, code string
	}{
		{"(ii) the tenant row refused", `REVOKE INSERT (tenant_id, actor_id, action, target, detail, at) ON audit_log FROM tappa_opdefiner`, sqlstateInsufficientPrivi},
		{"(iii) the operator row refused", `REVOKE INSERT (target_tenant_id) ON operator_audit_log FROM tappa_opdefiner`, sqlstateInsufficientPrivi},
		{"(iv) the operator row refused by a CHECK", `ALTER TABLE operator_audit_log ADD CONSTRAINT zz_op16_refuse CHECK (kind <> 'tenant_vat_checked') NOT VALID`, sqlstateInvalidParameter},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, c.ddl); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		err = opRecordVAT(t, ctx, sp, hash, f.id, f.number, true)
		if c.code == sqlstateInvalidParameter {
			opWantClean(t, err, c.code, vatCheckRefusal, c.what, f.number)
		} else {
			opWant(t, err, c.code, c.what)
		}
		nothing(sp, c.what)
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestOpRecordVATCheck_TheRowLockLetsTheTenantsWritesThrough measures the orchestrator's decision
// on the write's row lock (OP-16 A, phase 2: FOR NO KEY UPDATE, not FOR UPDATE). While a write
// for a COMMITTED tenant -- the database's oldest; its number read into memory, never printed --
// holds its lock inside this test's transaction (a savepoint, rolled back: the verdict, the two
// rows and the lock go with it), ANOTHER session's writes that name the tenant -- a tap's
// INSERT into transactions (a refused NFC tap: the tenant and nothing else to point at) and an
// INSERT into its audit_log, each a foreign-key check that takes FOR KEY SHARE on the tenant row
// -- go through at once (lock_timeout 300 ms; that session's transaction is rolled back, no row
// stays). CONTROL: with the tenant row locked FOR UPDATE in a savepoint instead, the same tap
// INSERT waits and is cancelled by its lock_timeout (55P03) -- so the probe can see the lock the
// decision avoids. And the database's refusals of a write naming that real number carry it in no
// field, and the accessor's errors carry it in no text (the orchestrator's decision 3).
func TestOpRecordVATCheck_TheRowLockLetsTheTenantsWritesThrough(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	var tenant uuid.UUID
	var number string
	if err := tx.QueryRow(ctx, `SELECT id, vat_number FROM public.tenants ORDER BY created_at, id LIMIT 1`).Scan(&tenant, &number); err != nil {
		t.Fatalf("PREMISE: no committed tenant to lock: %v", err)
	}
	opVATReadFor(t, ctx, tx, hash, tenant)
	other, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("a second owner connection: %v", err)
	}
	defer func() { _ = other.Close(context.Background()) }()
	// probe runs the other session's two writes in a transaction it rolls back, and returns the
	// first error and how long the writes took.
	probe := func() (time.Duration, error) {
		t.Helper()
		otx, err := other.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = otx.Rollback(context.Background()) }()
		if _, err := otx.Exec(ctx, `SET LOCAL lock_timeout = '300ms'`); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		for _, s := range []string{
			`INSERT INTO public.transactions (tenant_id, occurred_at, verdict, channel) VALUES ($1, clock_timestamp(), 'reject', 'nfc')`,
			`INSERT INTO public.audit_log (tenant_id, action) VALUES ($1, 'zz_op16_lock_probe')`,
		} {
			if _, err := otx.Exec(ctx, s, tenant); err != nil {
				return time.Since(start), err
			}
		}
		return time.Since(start), nil
	}
	if took, err := probe(); err != nil {
		t.Fatalf("PREMISE: with no lock held the other session's writes failed (%v, %v)", err, took)
	}

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := opRecordVATAccessor(t, ctx, sp, hash, tenant, number, true); err != nil {
		if strings.Contains(err.Error(), number) {
			t.Fatal("the accessor's error carries the number")
		}
		t.Fatalf("the write for the committed tenant: %v", err)
	}
	if n := len(opVATTrail(t, ctx, sp, tenant)); n < 1 {
		t.Fatal("PREMISE: the write did not reach the tenant (no tenant row), so it holds no lock on it")
	}
	took, err := probe()
	t.Logf("OP16-LOCK: with the write's row lock held, the other session's tap INSERT and audit INSERT took %v (err %v)", took.Round(time.Millisecond), err)
	if err != nil {
		t.Errorf("while the write holds its row lock another session's writes naming the tenant were held up: %v", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// CONTROL: a FOR UPDATE lock on the same row does hold the tap's INSERT.
	sp, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `SELECT 1 FROM public.tenants WHERE id = $1 FOR UPDATE`, tenant); err != nil {
		t.Fatal(err)
	}
	took, err = probe()
	t.Logf("OP16-LOCK: with a FOR UPDATE lock held, the other session's writes answered %v after %v", err, took.Round(time.Millisecond))
	opWant(t, err, sqlstateLockNotAvailable, "CONTROL: another session's tap INSERT under a FOR UPDATE lock")
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// The number in the errors: the database's refusals carry it in no field, the accessor's in
	// no text.
	opWantClean(t, opRecordVAT(t, ctx, tx, hash, tenant, number, nil), sqlstateInvalidParameter, vatCheckRefusal,
		"a NULL verdict for the committed tenant", number)
	opWantClean(t, opRecordVAT(t, ctx, tx, opRandHex(t), tenant, number, true), sqlstateInvalidAuthorization, touchRefusal00026,
		"an unknown session for the committed tenant", number)
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return RecordTenantVATCheck(ctx, sp, opRandHex(t), tenant, number, true)
	}); !errors.Is(err, ErrOperatorRefused) || strings.Contains(err.Error(), number) {
		t.Errorf("the accessor with an unknown session answered a refusal=%v, its text carrying the number=%v",
			errors.Is(err, ErrOperatorRefused), err != nil && strings.Contains(err.Error(), number))
	}
}

// TestOpRecordVATCheck_ALockWaitTellsTheTenantNotTheNumber measures LV3 after round 2 (the third
// eye's F2). ANOTHER session holds a COMMITTED tenant's row -- the oldest with a number;
// FOR NO KEY UPDATE, what a second re-check of it holds; that session's transaction is rolled
// back and writes nothing -- while this transaction's calls run under lock_timeout 300 ms:
//   - for that tenant, its own number, ANOTHER committed tenant's number and an unregistered
//     number alike wait and are cancelled: 55P03 with the server's own message, no number and
//     no session hash in any field. The wait tells that the tenant exists, and nothing about
//     whether or where a number is registered (round 1's locking read by the number did not
//     wait on a mismatch);
//   - for an id that names no tenant, with the held tenant's number or an unregistered one, the
//     call does not wait: void;
//   - round 3 (the third eye's D2): a session that never read the held tenant does not reach
//     its row at all -- the binding refuses first, the one 22023, no wait: holding a tenant's
//     row through this function now needs a committed 'read' row of that tenant;
//   - CONTROL: once the other session lets go, the same call for the held tenant is void.
//
// The numbers are read into memory, compared, and never printed.
func TestOpRecordVATCheck_ALockWaitTellsTheTenantNotTheNumber(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	unread, _ := opNewSession(t, ctx, tx, a.id, true)
	var tenant uuid.UUID
	var own, others, timeout string
	if err := tx.QueryRow(ctx, `SELECT id, vat_number FROM public.tenants WHERE vat_number IS NOT NULL
	                             ORDER BY created_at, id LIMIT 1`).Scan(&tenant, &own); err != nil {
		t.Fatalf("PREMISE: no committed tenant with a number to hold: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT vat_number FROM public.tenants WHERE vat_number IS NOT NULL AND id <> $1
	                             ORDER BY created_at, id LIMIT 1`, tenant).Scan(&others); err != nil {
		t.Fatalf("PREMISE: no second committed tenant with a number: %v", err)
	}
	unregistered := "VAT-OP16-UNREGISTERED-" + uuid.NewString()
	unknown1, unknown2 := uuid.New(), uuid.New()
	opVATReadFor(t, ctx, tx, hash, tenant, unknown1, unknown2)
	if err := tx.QueryRow(ctx, `SELECT current_setting('lock_timeout')`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}

	other, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("a second owner connection: %v", err)
	}
	defer func() { _ = other.Close(context.Background()) }()
	otx, err := other.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = otx.Rollback(context.Background()) }()
	if _, err := otx.Exec(ctx, `SELECT 1 FROM public.tenants WHERE id = $1 FOR NO KEY UPDATE`, tenant); err != nil {
		t.Fatalf("the other session's row lock: %v", err)
	}

	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '300ms'`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		what, session string
		tenant        uuid.UUID
		number        string
		want          string // the SQLSTATE; "" is void
	}{
		{"the held tenant, its own number", hash, tenant, own, sqlstateLockNotAvailable},
		{"the held tenant, another tenant's number", hash, tenant, others, sqlstateLockNotAvailable},
		{"the held tenant, an unregistered number", hash, tenant, unregistered, sqlstateLockNotAvailable},
		{"the held tenant, its own number, a session that never read it", unread, tenant, own, sqlstateInvalidParameter},
		{"an unknown tenant, the held tenant's number", hash, unknown1, own, ""},
		{"an unknown tenant, an unregistered number", hash, unknown2, unregistered, ""},
	} {
		start := time.Now()
		err := opRecordVAT(t, ctx, tx, c.session, c.tenant, c.number, true)
		took := time.Since(start).Round(time.Millisecond)
		code, _ := opCode(err)
		t.Logf("OP16-LV3: %s: SQLSTATE %q after %v", c.what, code, took)
		switch c.want {
		case sqlstateLockNotAvailable:
			opWantClean(t, err, sqlstateLockNotAvailable, "canceling statement due to lock timeout", c.what, own, others, hash)
		case sqlstateInvalidParameter:
			// A wait would have ended in 55P03: the 22023 is the binding's, before the row.
			opWantClean(t, err, sqlstateInvalidParameter, vatCheckRefusal, c.what, own, others, unread, tenant.String())
		default:
			if err != nil {
				t.Errorf("%s: %v, want void without a wait", c.what, code)
			}
		}
	}

	// CONTROL: the other session lets go; the same call for the held tenant is void.
	if err := otx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := opRecordVAT(t, ctx, sp, hash, tenant, unregistered, true); err != nil {
		code, _ := opCode(err)
		t.Errorf("CONTROL: with no lock held the call for the tenant answered SQLSTATE %q, want void", code)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true)`, timeout); err != nil {
		t.Fatal(err)
	}
}

// TestOpRecordVATCheck_ACallForOneTenantTouchesNoOther is the belt ADR 0021 §6 ("kemer") asks of
// a tenant-naming WRITE: no row level security applies inside it, and `p_tenant_id` is the only
// barrier in its locking read, its UPDATE and its two INSERTs. Two tenants, A and B,
// in the same state:
//   - a call for A with A's number changes A alone -- B's verdict and row version are unchanged
//     -- and adds exactly one tenant row in the whole of audit_log, A's;
//   - a call for A with B's number, and one for B with A's number, change nobody and add no
//     tenant row anywhere;
//   - every operator row names the tenant the call named.
func TestOpRecordVATCheck_ACallForOneTenantTouchesNoOther(t *testing.T) {
	ctx, tx := opTx(t)
	adm := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, adm.id, true)
	tok := opToken(t)
	a := opVATTenant(t, ctx, tx, "op16 belt A "+tok, opVATBool(true), "2025-06-07 08:09:10+00")
	b := opVATTenant(t, ctx, tx, "op16 belt B "+tok, opVATBool(true), "2025-06-07 08:09:10+00")
	opVATReadFor(t, ctx, tx, hash, a.id, b.id)
	bBefore, actions := opVATOf(t, ctx, tx, b.id), opVATActions(t, ctx, tx)
	if err := opRecordVATAccessor(t, ctx, tx, hash, a.id, a.number, false); err != nil {
		t.Fatalf("a call for A: %v", err)
	}
	if now := opVATOf(t, ctx, tx, a.id); now.verified == nil || *now.verified {
		t.Error("the call for A did not change A")
	}
	if now := opVATOf(t, ctx, tx, b.id); !now.same(bBefore) {
		t.Error("a call for A changed B")
	}
	if d, na, nb := opVATActions(t, ctx, tx)-actions, len(opVATTrail(t, ctx, tx, a.id)), len(opVATTrail(t, ctx, tx, b.id)); d != 1 || na != 1 || nb != 0 {
		t.Errorf("a call for A added %d tenant row(s) in all -- A %d, B %d; want 1, A's", d, na, nb)
	}
	aAfter := opVATOf(t, ctx, tx, a.id)
	for _, c := range []struct {
		what       string
		tenant     uuid.UUID
		number     string
		opsA, opsB int64
	}{
		{"a call for A with B's number", a.id, b.number, 2, 0},
		{"a call for B with A's number", b.id, a.number, 2, 1},
	} {
		if err := opRecordVATAccessor(t, ctx, tx, hash, c.tenant, c.number, true); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if !opVATOf(t, ctx, tx, a.id).same(aAfter) || !opVATOf(t, ctx, tx, b.id).same(bBefore) {
			t.Errorf("%s changed a tenant", c.what)
		}
		if d := opVATActions(t, ctx, tx) - actions; d != 1 {
			t.Errorf("%s added a tenant row somewhere (%d in all since the start, want 1)", c.what, d)
		}
		if na, nb := opVATOperatorRows(t, ctx, tx, session, a.id), opVATOperatorRows(t, ctx, tx, session, b.id); na != c.opsA || nb != c.opsB {
			t.Errorf("%s: operator rows naming A %d, B %d; want %d, %d", c.what, na, nb, c.opsA, c.opsB)
		}
	}
}

// TestOpRecordVATCheck_RefusesEveryDeadSession: the write resolves the session through
// op_touch_session before anything else, so each of the six dead sessions is the touch refusal
// (28000, no DETAIL) and nothing is written -- the verdict, the tenant row, the operator row.
func TestOpRecordVATCheck_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	f := opVATTenant(t, ctx, tx, "op16 dead "+opToken(t), nil, "")
	before, actions := opVATOf(t, ctx, tx, f.id), opVATActions(t, ctx, tx)
	audit := opAudit(t, ctx, tx)
	for _, d := range opDeadSessions(t, ctx, tx) {
		opWantClean(t, opRecordVAT(t, ctx, tx, d.hash, f.id, f.number, true), sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash, f.number)
	}
	if !opVATOf(t, ctx, tx, f.id).same(before) || opVATActions(t, ctx, tx) != actions {
		t.Error("a dead session's call changed the tenant or wrote its row")
	}
	if d := opAudit(t, ctx, tx) - audit; d != 0 {
		t.Errorf("dead sessions' calls left %d operator row(s)", d)
	}
}

// opVATDetailShape is the tenant row's detail decoded -- used by nothing but a guard that the
// text form compared above is JSON at all (a test helper that keeps opVATDetailText honest).
func opVATDetailShape(t *testing.T, detail string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(detail), &m); err != nil {
		t.Fatalf("the detail is not JSON: %v", err)
	}
	return m
}

// TestOpVATDetailText_IsTheShapeTheWriteBuilds holds the test's own expectation to the decided
// shape without a database: three keys, actor_kind "operator", before and after each exactly
// {verified, checked_at}, a NULL before rendered as two nulls, times as UTC text with six digits
// and a Z whatever the zone of the instant given.
func TestOpVATDetailText_IsTheShapeTheWriteBuilds(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 34, 56, 789000, time.FixedZone("far", 14*3600))
	m := opVATDetailShape(t, opVATDetailText(nil, nil, true, at))
	if len(m) != 3 || m["actor_kind"] != "operator" {
		t.Fatalf("the shape: %v", m)
	}
	after, _ := m["after"].(map[string]any)
	before, _ := m["before"].(map[string]any)
	// 12:34:56 at +14:00 is 22:34:56 UTC the day before.
	if len(after) != 2 || after["verified"] != true || after["checked_at"] != "2026-10-06T22:34:56.000789Z" {
		t.Errorf("after: %v", after)
	}
	if len(before) != 2 || before["verified"] != nil || before["checked_at"] != nil {
		t.Errorf("before: %v", before)
	}
	if _, ok := before["verified"]; !ok {
		t.Error("before carries no verified key")
	}
	if _, err := os.Stat(filepath.Join("..", "..", "db", "migrations", op00034File)); err != nil {
		t.Errorf("the migration this file tests is not there: %v", err)
	}
}
