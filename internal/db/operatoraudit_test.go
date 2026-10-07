package db

// operatoraudit_test.go -- migration 00031 (M10 OP-14, phase A): the operator reads its OWN
// audit log through op_read_audit, and op_record_auth_event takes 'password_ok'. The
// catalogue half (signatures, the four copies of the kind set, the grants, the Down, the
// precondition, the temp-table shadow) and the behaviour half (op_begin_read's new kind,
// the read and its closed list of detail shapes, the replaced pre-session writer, the Go
// accessor) of ADR 0021 §6's list, for the new op_read_* and the two replaced functions.
//
// HOW EACH TEST TOUCHES THE SHARED DATABASE -- operatorplaques_test.go's three shapes:
//   - most tests run in opTx: one rolled-back REPEATABLE READ transaction as the owner,
//     identities switched with SET LOCAL SESSION AUTHORIZATION, the operator-tables lock
//     taken EXCLUSIVE. Every row they need is written inside that transaction and goes with
//     it. Rows whose PLACE in the newest-first order matters are written by the owner with a
//     time far in the future (opLogBase): the owner may write `at` (00031's header names
//     that fact and the OP-14 note counts it), REPEATABLE READ hides every other session's
//     commits, and a precondition checks that no committed row is dated at or after the base
//     -- so those rows head the read in the order they were given;
//   - the read side is reached WITHOUT a commit through a ticket the owner writes with
//     created_xact naming a transaction that really committed (opCommittedXact), its hash
//     computed HERE in Go from the canonical text (opLogTicketHash);
//   - two tests need a COMMIT because a commit is their subject
//     (TestOpReadAudit_TwoPhaseLifecycle, TestOperatorAudit_OnThePoolTheTwoPhasesAreTwoTransactions).
//     They take the lock SHARED through opLiveFixture. What they leave per run, by
//     construction: one disabled account each, its revoked sessions, and the 'read' rows
//     they committed (the lifecycle one, the pool test two) -- operator_audit_log is
//     append-only and its foreign keys keep the account and the sessions those rows name.
//
// 🔴 NO TEST HERE COMMITS A 'password_ok' ROW. 00031's Down returns the audit CHECKs NOT
// VALID when one exists, and 00027's applied Up cannot be re-run over one (the Down test
// measures both); a committed one would change what the other migrations' Down tests meet.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// op00031Read is the function 00031 creates, by its exact catalogue identity: the session,
// the RAW ticket and the read's own parameters -- no actor -- and the fixed column list of
// ADR 0021 §2 ii. There is no detail column: the result type is the whole of what the read
// can return, so the raw detail cannot join it without this identity turning red.
var op00031Read = struct{ name, args, result string }{
	"op_read_audit",
	"p_session text, p_ticket text, p_kind text, p_page_number integer, p_page_size integer",
	"TABLE(audit_id uuid, at timestamp with time zone, kind text, session_id uuid, actor_admin_id uuid, " +
		"actor_name text, target_admin_id uuid, target_admin_name text, target_tenant_id uuid, " +
		"target_tenant_name text, target_scope text, page_number integer, page_size integer, " +
		"search_class text, filter_kind text, legal_slug text, legal_bytes integer, detail_recognised boolean)",
}

const (
	opAuditReadRefusal = "op_read_audit: read refused"
	op00031File        = "00031_read_the_operator_audit_log.sql"
	op00027File        = "00027_move_legal_publishing_to_the_operator.sql"
	op00026File        = "00026_create_platform_operator.sql"

	// opLogBase is the time the tests' ordered fixture rows are dated after: far enough in
	// the future that no row a product path writes can be later.
	opLogBase = "2999-01-01 00:00:00+00"

	// The two messages op_record_auth_event's 22023 carries at 00031, and the refusal a
	// caught constraint becomes.
	recordKindRefusal31  = "op_record_auth_event: kind is not a pre-session kind"
	recordPasswordById31 = "op_record_auth_event: password_ok names its account by id alone"
	recordRowRefused31   = "op_record_auth_event: the audit row was refused"
)

// opAuditKinds is the closed set of operator_audit_log kinds at 00031, in the CHECK's order --
// the list the four copies are held to (TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree
// against the database, TestOperatorAuditKinds_TheTypedConstantsAreTheList against the Go
// constants).
var opAuditKinds = []string{"login_failed", "unknown_email", "totp_failed", "locked", "enrollment_failed",
	"password_ok", "login", "enrollment", "logout", "read", "legal_publish"}

// opAuthEventKinds is op_record_auth_event's closed set at 00031: the pre-session kinds --
// actor_shape's no-session arm.
var opAuthEventKinds = []string{"login_failed", "unknown_email", "totp_failed", "locked", "enrollment_failed", "password_ok"}

// opKindsAt31 is the closed set of read kinds at 00031 (opAtVersion's argument).
var opKindsAt31 = []string{"legal_versions", "tenants", "tenant_detail", "tenant_plaques", "operator_audit"}

// opAuditKinds27 and opAuthEventKinds26 are the sets 00031's Down returns to: 00027's ten
// audit kinds and 00026's five pre-session kinds. The Down test derives both from those files
// and compares.
var (
	opAuditKinds27     = []string{"login_failed", "unknown_email", "totp_failed", "locked", "enrollment_failed", "login", "enrollment", "logout", "read", "legal_publish"}
	opAuthEventKinds26 = []string{"login_failed", "unknown_email", "totp_failed", "locked", "enrollment_failed"}
)

// ------------------------------------------------------------------ helpers --

// opKindCheckDef is pg_get_constraintdef's text of a `CHECK (kind IN (...))` over kinds.
func opKindCheckDef(kinds []string) string {
	q := make([]string, len(kinds))
	for i, k := range kinds {
		q[i] = "'" + k + "'::text"
	}
	return "CHECK ((kind = ANY (ARRAY[" + strings.Join(q, ", ") + "])))"
}

// opActorShapeDef is pg_get_constraintdef's text of operator_audit_log_actor_shape whose
// pre-session arm names pre.
func opActorShapeDef(pre []string) string {
	q := make([]string, len(pre))
	for i, k := range pre {
		q[i] = "'" + k + "'::text"
	}
	arr := "ARRAY[" + strings.Join(q, ", ") + "]"
	return "CHECK ((((kind = ANY (" + arr + ")) AND (session_id IS NULL) AND (actor_admin_id IS NULL)) OR " +
		"((kind <> ALL (" + arr + ")) AND (session_id IS NOT NULL) AND (actor_admin_id IS NOT NULL))))"
}

// opConstraint reads one constraint's definition and whether it is validated.
func opConstraint(t *testing.T, ctx context.Context, q opQuerier, table, name string) (string, bool) {
	t.Helper()
	var def string
	var valid bool
	if err := q.QueryRow(ctx, `SELECT pg_get_constraintdef(oid), convalidated FROM pg_constraint
	                            WHERE conrelid = ('public.' || $1)::regclass AND conname = $2`, table, name).Scan(&def, &valid); err != nil {
		t.Fatalf("read %s.%s: %v", table, name, err)
	}
	return def, valid
}

// opQuotedArrays returns the values of every ARRAY['a'::text, ...] in a constraint's text.
func opQuotedArrays(def string) [][]string {
	var out [][]string
	for _, m := range regexp.MustCompile(`ARRAY\[([^\]]*)\]`).FindAllStringSubmatch(def, -1) {
		var vals []string
		for _, v := range regexp.MustCompile(`'([^']*)'::text`).FindAllStringSubmatch(m[1], -1) {
			vals = append(vals, v[1])
		}
		out = append(out, vals)
	}
	return out
}

// opLogCanonical is jsonb's text of the read's parameter object: keys shorter-first (kind,
// page_size, page_number).
func opLogCanonical(kind string, number, size int) string {
	return fmt.Sprintf(`{"kind": %s, "page_size": %d, "page_number": %d}`, opPGJSONString(kind), size, number)
}

func opLogTicketHash(raw, kind string, number, size int) string {
	sum := sha256.Sum256([]byte(raw + opLogCanonical(kind, number, size)))
	return hex.EncodeToString(sum[:])
}

// opLogParams is the read's parameter object as JSON text (Go's spelling; the database
// rebuilds the hashed text itself).
func opLogParams(kind string, number, size int) string {
	return fmt.Sprintf(`{"kind": %s, "page_number": %d, "page_size": %d}`, strconv.Quote(kind), number, size)
}

// opForgeLog forges a log-read ticket bound to (kind, page) and returns the raw ticket. Its
// forged 'read' row is dated by the wall clock, i.e. after every opLogBase-less row and
// before every opLogBase row.
func opForgeLog(t *testing.T, ctx context.Context, tx pgx.Tx, session, admin uuid.UUID, kind string, number, size int, xact string) string {
	t.Helper()
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, admin, operatorAuditReadKind, opLogTicketHash(raw, kind, number, size), nil, xact, "30 seconds")
	return raw
}

// opScanLogRows scans op_read_audit's columns with a scan written HERE, independent of the
// product's (readOperatorAudit), the database's error untouched.
func opScanLogRows(rows pgx.Rows) ([]OperatorAuditEntry, error) {
	defer rows.Close()
	var out []OperatorAuditEntry
	for rows.Next() {
		var e OperatorAuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.SessionID, &e.ActorID, &e.ActorName, &e.TargetAdminID,
			&e.TargetAdminName, &e.TargetTenantID, &e.TargetTenantName, &e.Scope, &e.PageNumber, &e.PageSize,
			&e.SearchClass, &e.FilterKind, &e.LegalSlug, &e.LegalBytes, &e.DetailRecognised); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func opScanLog(ctx context.Context, q opQuerier, hash, ticket, kind, number, size any) ([]OperatorAuditEntry, error) {
	rows, err := q.Query(ctx, readOperatorAuditSQL, hash, ticket, kind, number, size)
	if err != nil {
		return nil, err
	}
	return opScanLogRows(rows)
}

// opReadLog runs the read as tappa_operator inside a savepoint of tx; on success the
// savepoint is released, so the consumption stays in tx.
func opReadLog(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket, kind string, number, size int) ([]OperatorAuditEntry, error) {
	t.Helper()
	var out []OperatorAuditEntry
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		out, e = opScanLog(ctx, sp, hash, ticket, kind, number, size)
		return e
	})
	return out, err
}

// opLogRow is one row a test writes into operator_audit_log as the OWNER, who may write
// every column, `at` included. after is an interval after opLogBase; "" writes the column's
// DEFAULT (the wall clock).
type opLogRow struct {
	kind                   string
	session, actor, target *uuid.UUID
	tenant                 *uuid.UUID
	scope                  string // "" = NULL
	number, size           int    // 0 = NULL
	detail                 string // JSON; "" = {}
	after                  string
}

func opLogInsert(t *testing.T, ctx context.Context, q opQuerier, r opLogRow) uuid.UUID {
	t.Helper()
	detail := r.detail
	if detail == "" {
		detail = "{}"
	}
	var id uuid.UUID
	if err := q.QueryRow(ctx, `
		INSERT INTO public.operator_audit_log (at, kind, session_id, actor_admin_id, target_admin_id, target_tenant_id,
		                                       target_scope, page_number, page_size, detail)
		VALUES (CASE WHEN $1 = '' THEN clock_timestamp() ELSE $2::timestamptz + $1::interval END,
		        $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9::int, 0), NULLIF($10::int, 0), $11::jsonb)
		RETURNING id`, r.after, opLogBase, r.kind, r.session, r.actor, r.target, r.tenant, r.scope,
		r.number, r.size, detail).Scan(&id); err != nil {
		t.Fatalf("write the audit row %+v: %v", r, err)
	}
	return id
}

// opNoRowAtOrAfterBase is the ordered fixtures' precondition: no row the test can see is
// dated at or after opLogBase, so the fixtures head the read in the order they were given.
func opNoRowAtOrAfterBase(t *testing.T, ctx context.Context, q opQuerier) {
	t.Helper()
	if n := opInt(t, ctx, q, `SELECT count(*) FROM operator_audit_log WHERE at >= $1::timestamptz`, opLogBase); n != 0 {
		t.Fatalf("PREMISE: %d audit row(s) are dated at or after %s; the newest-first order would put them before this test's rows", n, opLogBase)
	}
}

// opDeref is *p as text, or "<nil>".
func opDeref[T any](p *T) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprint(*p)
}

// opLogFunctionBody returns a function's body as a migration section defines it, for the
// spelling given ("CREATE FUNCTION" or "CREATE OR REPLACE FUNCTION").
func opLogFunctionBody(t *testing.T, section, create, fn string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(create) + ` public\.` + fn + `\(.*?\nAS \$\$(.*?)\$\$;`).FindStringSubmatch(section)
	if m == nil {
		t.Fatalf("the section does not define %s with %q", fn, create)
	}
	return m[1]
}

// --------------------------------------------------------------- catalogue --

// TestOperator00031_TheFunctionsAndTheirExactSignatures pins what ADR 0021 §6's generic pins
// leave open for the read 00031 creates and the two it replaces: op_read_audit's exact
// argument list and result (no detail column), the owner, SECURITY DEFINER, proconfig, one
// overload, EXECUTE for tappa_operator alone; op_begin_read and op_record_auth_event keep their
// identities; the forward, frozen-clock and consumption scans walked them and raise nothing;
// and the three CHECKs at HEAD -- the audit kinds exactly the eleven, actor_shape's
// pre-session arm exactly the six, both validated, and the ticket kinds a closed set holding
// 00031's five (00032 widened it: HEAD's exact set is TestOperator00032_TheFunctionsAndTheirExactSignatures'
// pin, and 00031's exact set after 00032's Down is TestOperator00032_DownGivesBack00031AndUpTakesItAgain's).
func TestOperator00031_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	var args, result, owner string
	var retset, secdef bool
	var config []string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
		       pg_get_userbyid(p.proowner), p.proretset, p.prosecdef, p.proconfig
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.proname = $1`, op00031Read.name).Scan(&args, &result, &owner, &retset, &secdef, &config); err != nil {
		t.Fatalf("%s: %v", op00031Read.name, err)
	}
	if args != op00031Read.args {
		t.Errorf("%s(%s), want (%s)", op00031Read.name, args, op00031Read.args)
	}
	if result != op00031Read.result {
		t.Errorf("%s returns %s,\n want %s", op00031Read.name, result, op00031Read.result)
	}
	if owner != "tappa_opdefiner" || !secdef || !retset {
		t.Errorf("owner %s, SECURITY DEFINER %v, set-returning %v; want tappa_opdefiner, true, true", owner, secdef, retset)
	}
	if len(config) != 1 || config[0] != "search_path=pg_catalog, pg_temp" {
		t.Errorf("proconfig %v", config)
	}
	for _, fn := range []string{op00031Read.name, "op_begin_read", "op_record_auth_event"} {
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, fn); n != 1 {
			t.Errorf("%d functions named %s; an overload would be a second door", n, fn)
		}
		for who, want := range map[string]bool{"public": false, "tappa_app": false, "tappa_resolver": false, "tappa_operator": true} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, p.oid, 'EXECUTE') FROM pg_proc p
			                             JOIN pg_namespace n ON n.oid = p.pronamespace
			                            WHERE n.nspname = 'public' AND p.proname = $2`, who, fn).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may != want {
				t.Errorf("has_function_privilege(%s, %s, EXECUTE) = %v, want %v", who, fn, may, want)
			}
		}
	}
	if got := opIdentity(t, ctx, tx, "op_begin_read"); got != "p_session text, p_kind text, p_params jsonb -> text" {
		t.Errorf("op_begin_read is now %s; the replacement keeps 00027's identity", got)
	}
	if got := opIdentity(t, ctx, tx, "op_record_auth_event"); got != "p_kind text, p_email text, p_admin uuid -> void" {
		t.Errorf("op_record_auth_event is now %s; the replacement keeps 00026's identity", got)
	}

	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for _, fn := range []string{op00031Read.name, "op_begin_read", "op_record_auth_event"} {
		if !slices.Contains(names, fn) {
			t.Errorf("anti-vacuity: the forward scan did not walk %s", fn)
		}
		for _, f := range findings {
			if strings.Contains(f, fn+"(") {
				t.Error(f)
			}
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
	if !slices.Contains(read, op00031Read.name) {
		t.Errorf("anti-vacuity: the consumption scan did not read %s (it read %v)", op00031Read.name, read)
	}
	for _, f := range consumption {
		if strings.Contains(f, op00031Read.name+"(") {
			t.Error(f)
		}
	}

	tickets, _ := opConstraint(t, ctx, tx, "operator_read_tickets", "operator_read_tickets_kind_check")
	for _, kind := range opKindsAt31 {
		if !strings.Contains(tickets, "'"+kind+"'::text") || strings.Contains(tickets, "~") {
			t.Errorf("operator_read_tickets_kind_check is %s, want a closed set holding %q", tickets, kind)
		}
	}
	for _, c := range []struct{ table, name, want string }{
		{"operator_audit_log", "operator_audit_log_kind_check", opKindCheckDef(opAuditKinds)},
		{"operator_audit_log", "operator_audit_log_actor_shape", opActorShapeDef(opAuthEventKinds)},
	} {
		def, valid := opConstraint(t, ctx, tx, c.table, c.name)
		if def != c.want || !valid {
			t.Errorf("%s is %s (validated %v),\n want %s, validated", c.name, def, valid, c.want)
		}
	}
}

// TestOperator00031_TheDefinerReadsTheLogAndTheOperatorDoesNot is the OP-14 acceptance "the
// two-phase read is the only reader" on the catalogue and as statements:
//   - tappa_opdefiner may SELECT every column of operator_audit_log (00027's id and 00031's
//     ten), still INSERTs the nine 00026 granted and nothing else, and never `at`; it holds
//     no UPDATE, DELETE or TRUNCATE (the log is append-only);
//   - tappa_operator holds NO privilege on operator_audit_log: a direct SELECT, a COPY and a
//     SELECT of detail are 42501; tappa_app holds none either, cannot call op_read_audit and
//     cannot write a 'password_ok' row (42501 both);
//   - the owner DOES hold INSERT on `at` -- measured, the fact 00031's header and the OP-14
//     note count -- and the append-only triggers refuse it an UPDATE and a DELETE of a
//     'password_ok' row like any other;
//   - the definer still EXECUTEs nothing outside its own functions that PUBLIC may not
//     (00030's pin: 00031 grants no foreign EXECUTE).
func TestOperator00031_TheDefinerReadsTheLogAndTheOperatorDoesNot(t *testing.T) {
	ctx, tx := opTx(t)
	const allColumns = "id,at,kind,session_id,actor_admin_id,target_admin_id,target_tenant_id,target_scope,page_number,page_size,detail"
	const insertColumns = "kind,session_id,actor_admin_id,target_admin_id,target_tenant_id,target_scope,page_number,page_size,detail"
	for _, c := range []struct{ role, priv, want string }{
		{"tappa_opdefiner", "SELECT", allColumns},
		{"tappa_opdefiner", "INSERT", insertColumns},
		{"tappa_opdefiner", "UPDATE", ""},
		{"tappa_operator", "SELECT", ""},
		{"tappa_operator", "INSERT", ""},
		{"tappa_operator", "UPDATE", ""},
		{"tappa_app", "SELECT", ""},
		{"tappa_app", "INSERT", ""},
		{"tappa_app", "UPDATE", ""},
	} {
		if got := opColumns(t, ctx, tx, c.role, "operator_audit_log", c.priv); got != c.want {
			t.Errorf("%s %s on operator_audit_log = (%s), want (%s)", c.role, c.priv, got, c.want)
		}
	}
	for _, c := range []struct {
		role, column, priv string
		want               bool
	}{
		{"tappa_opdefiner", "at", "INSERT", false},
		{"tappa_operator", "detail", "SELECT", false},
		{"tappa_owner", "at", "INSERT", true},
	} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1, 'public.operator_audit_log', $2, $3)`, c.role, c.column, c.priv).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may != c.want {
			t.Errorf("has_column_privilege(%s, operator_audit_log, %s, %s) = %v, want %v", c.role, c.column, c.priv, may, c.want)
		}
	}
	for _, role := range []string{"tappa_opdefiner", "tappa_operator", "tappa_app"} {
		for _, priv := range []string{"DELETE", "TRUNCATE"} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.operator_audit_log', $2)`, role, priv).Scan(&may); err != nil || may {
				t.Errorf("has_table_privilege(%s, operator_audit_log, %s) = %v (err %v), want false", role, priv, may, err)
			}
		}
	}

	a := opNewActive(t, ctx, tx)
	for _, probe := range []string{
		`SELECT count(*) FROM public.operator_audit_log`,
		`SELECT detail FROM public.operator_audit_log LIMIT 1`,
		`SELECT count(*) FROM public.operator_audit_log WHERE kind = 'password_ok'`,
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_operator", probe), sqlstateInsufficientPrivi, "tappa_operator: "+probe)
	}
	opWant(t, opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		_, e := sp.Conn().PgConn().CopyTo(ctx, io.Discard, `COPY public.operator_audit_log TO STDOUT`)
		return e
	}), sqlstateInsufficientPrivi, "tappa_operator: COPY operator_audit_log")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", readOperatorAuditSQL, opRandHex(t), opRandHex(t), "", 1, 50),
		sqlstateInsufficientPrivi, "tappa_app calling op_read_audit")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `INSERT INTO public.operator_audit_log (kind, target_admin_id) VALUES ('password_ok', $1)`, a.id),
		sqlstateInsufficientPrivi, "tappa_app writing a 'password_ok' row directly")
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `SELECT public.op_record_auth_event('password_ok', NULL, $1)`, a.id),
		sqlstateInsufficientPrivi, "tappa_app writing a 'password_ok' row through op_record_auth_event")
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `INSERT INTO public.operator_audit_log (kind, target_admin_id) VALUES ('password_ok', $1)`, a.id),
		sqlstateInsufficientPrivi, "tappa_operator writing a 'password_ok' row directly")

	// The owner: append-only binds it too, for the new kind as for every other.
	id := opLogInsert(t, ctx, tx, opLogRow{kind: "password_ok", target: &a.id})
	for _, c := range []struct{ what, sql string }{
		{"owner UPDATE of a 'password_ok' row", `UPDATE operator_audit_log SET kind = 'locked' WHERE id = '` + id.String() + `'`},
		{"owner DELETE of a 'password_ok' row", `DELETE FROM operator_audit_log WHERE id = '` + id.String() + `'`},
	} {
		opWant(t, opTry(t, ctx, tx, c.sql), sqlstateRestrictViolation, c.what)
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_opdefiner", `UPDATE public.operator_audit_log SET kind = 'locked' WHERE id = $1`, id),
		sqlstateInsufficientPrivi, "the definer updating an audit row")

	findings, err := opDefinerForeignExecFindings(ctx, tx)
	if err != nil {
		t.Fatalf("definer EXECUTE scan: %v", err)
	}
	for _, f := range findings {
		t.Errorf("tappa_opdefiner may EXECUTE %s", f)
	}
}

// TestOperator00031_DownGivesBack00030AndUpTakesItAgain runs 00031's Down and Up from the
// migration file inside the test's transaction (opAtVersion first, so a later migration does
// not stand in the way).
//   - THE SETS: the ticket kinds the Down's NOT VALID condition and its two CHECKs name are
//     00030's (derived from 00030's file); the audit kinds of its condition and its two kind
//     CHECKs are 00027's (from 00027's file); the pre-session arm of its two actor_shapes is
//     00026's (from 00026's file).
//   - THE CONDITIONS, WHOLE (round 2 of the OP-14 A review): each of the four NOT VALID
//     conditions -- the Down's two and the Up's two -- is read to its end, its entire WHERE
//     clause compared, not only its ARRAY: an added "AND consumed_at IS NULL" left the
//     earlier, ARRAY-only pin green.
//   - Down removes the read, gives op_begin_read back 00030's Up body and op_record_auth_event
//     00026's body VERBATIM (compared with those files), takes back EXACTLY the ten column
//     SELECTs (the definer's ACL entries on operator_audit_log are 00030's after it, and
//     op_begin_read -- which needs 00027's id -- still writes: the trap of a REVOKE ALL),
//     and puts the three CHECKs back.
//   - NOT VALID, branch by branch: none outside the old sets -> all VALIDATED; an
//     unconsumed 'operator_audit' ticket -> the ticket CHECK NOT VALID (a new such ticket is
//     refused); a 'password_ok' row -> the audit kind CHECK and actor_shape NOT VALID (a new
//     'password_ok' row is refused, and the restored writer refuses the kind); a LATER
//     migration's ticket kind and audit kind -> all three NOT VALID, and 00031's Up again
//     COMPOSES (its own NOT VALID branch) -- the step 00030's Up could not take (L11).
//   - CONSUMED TICKETS COUNT (round 2; production's tickets are consumed): an
//     'operator_audit' ticket the shipped read has consumed, and nothing else outside
//     00030's set -> the Down's ticket CHECK NOT VALID; a LATER migration's consumed ticket,
//     and nothing else outside 00031's set -> the Down's AND the Up's ticket CHECK NOT VALID
//     (the Up composes over it), the audit CHECKs VALIDATED.
//   - COUNTED LIMITS, measured: with a 'password_ok' row present, 00027's applied Up fails
//     with 23514 (its ten-kind CHECK is re-added VALIDATED), and so does 00027's Down when no
//     'read' or 'legal_publish' row exists (its VALIDATED branch).
//   - Up takes it all again, with this file's bodies.
func TestOperator00031_DownGivesBack00030AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 31, opKindsAt31...)
	up, down := opMigrationSections(t, op00031File)
	up30, _ := opMigrationSections(t, op00030File)
	up27, down27 := opMigrationSections(t, op00027File)
	up26, _ := opMigrationSections(t, op00026File)
	body30, body31 := opBeginReadBody(t, op00030File), opBeginReadBody(t, op00031File)
	rec26 := opLogFunctionBody(t, up26, "CREATE FUNCTION", "op_record_auth_event")
	rec31 := opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_record_auth_event")
	if body30 == body31 || rec26 == rec31 {
		t.Fatal("PREMISE: a replaced body is the same text as the one it replaces")
	}

	// THE SETS, from the earlier files, and the Down's uses of them.
	m := regexp.MustCompile(`ADD CONSTRAINT operator_read_tickets_kind_check\s+CHECK \(kind IN \(([^)]*)\)\);`).FindStringSubmatch(up30)
	if m == nil || !slices.Equal(opQuotedList(m[1]), opKindsAt30) {
		t.Fatalf("00030's ticket kind set is not %v (%v)", opKindsAt30, m)
	}
	m = regexp.MustCompile(`(?s)ADD CONSTRAINT operator_audit_log_kind_check\s+CHECK \(kind IN \(([^)]*)\)\);`).FindStringSubmatch(up27)
	set27 := []string{}
	if m != nil {
		set27 = opQuotedList(strings.Join(strings.Fields(m[1]), " "))
	}
	if !slices.Equal(set27, opAuditKinds27) {
		t.Fatalf("00027's audit kind set is %v, want %v", set27, opAuditKinds27)
	}
	m = regexp.MustCompile(`(?s)CONSTRAINT operator_audit_log_actor_shape CHECK \(\s*\(kind IN \(([^)]*)\)`).FindStringSubmatch(up26)
	pre26 := []string{}
	if m != nil {
		pre26 = opQuotedList(strings.Join(strings.Fields(m[1]), " "))
	}
	if !slices.Equal(pre26, opAuthEventKinds26) {
		t.Fatalf("00026's pre-session arm is %v, want %v", pre26, opAuthEventKinds26)
	}
	it := strings.Index(down, "ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check")
	ia := strings.Index(down, "ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check")
	if it < 0 || ia < it {
		t.Fatal("00031's Down does not restore the ticket CHECK and then the audit CHECKs")
	}
	ticketPart, auditPart := down[it:ia], down[ia:]
	lists := func(part, re string) [][]string {
		var out [][]string
		for _, mm := range regexp.MustCompile(`(?s)`+re).FindAllStringSubmatch(part, -1) {
			out = append(out, opQuotedList(strings.Join(strings.Fields(mm[1]), " ")))
		}
		return out
	}
	// THE CONDITIONS, WHOLE: every "IF EXISTS (SELECT 1 FROM public.<table> WHERE ...) THEN"
	// read to its ") THEN", whitespace folded, and compared with the one clause it may be.
	conds := func(part, table string) []string {
		var out []string
		re := regexp.MustCompile(`(?s)IF EXISTS \(SELECT 1 FROM public\.` + table + `\s+WHERE (.*?)\) THEN`)
		for _, mm := range re.FindAllStringSubmatch(part, -1) {
			out = append(out, strings.Join(strings.Fields(mm[1]), " "))
		}
		return out
	}
	whole := func(set []string) string { return "kind <> ALL (ARRAY['" + strings.Join(set, "', '") + "'])" }
	for _, c := range []struct {
		what, part, table string
		set               []string
	}{
		{"00031's Down: the ticket condition", ticketPart, "operator_read_tickets", opKindsAt30},
		{"00031's Down: the audit condition", auditPart, "operator_audit_log", opAuditKinds27},
		{"00031's Up: the ticket condition", up, "operator_read_tickets", opKindsAt31},
		{"00031's Up: the audit condition", up, "operator_audit_log", opAuditKinds},
	} {
		if got, want := conds(c.part, c.table), whole(c.set); len(got) != 1 || got[0] != want {
			t.Errorf("%s is %q; want exactly one, whole: %q", c.what, got, want)
		}
	}
	for _, c := range []struct {
		what  string
		got   [][]string
		want  []string
		count int
	}{
		{"the ticket CHECKs", lists(ticketPart, `CHECK \(kind IN \(([^)]*)\)\)`), opKindsAt30, 2},
		{"the audit kind CHECKs", lists(auditPart, `operator_audit_log_kind_check\s+CHECK \(kind IN \(([^)]*)\)\)`), opAuditKinds27, 2},
		{"the actor_shape arms", lists(auditPart, `kind (?:NOT )?IN \(([^)]*)\)\s+AND session_id`), opAuthEventKinds26, 4},
	} {
		if len(c.got) != c.count {
			t.Errorf("00031's Down: %s occur %d time(s), want %d", c.what, len(c.got), c.count)
		}
		for _, g := range c.got {
			if !slices.Equal(g, c.want) {
				t.Errorf("00031's Down: %s name %v, want %v", c.what, g, c.want)
			}
		}
	}

	type state struct {
		reads                                     int64
		tickets, kinds, shape                     string
		ticketsValid, kindsValid, shapeValid      bool
		begin, record                             string
		definerSelect, operatorSelect, definerACL string
		appExecute, opExecute                     bool
	}
	read := func(q pgx.Tx) state {
		t.Helper()
		var s state
		if err := q.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM pg_proc WHERE proname = 'op_read_audit'),
			       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_begin_read(text, text, jsonb)'::regprocedure),
			       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_record_auth_event(text, text, uuid)'::regprocedure),
			       has_function_privilege('tappa_app', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE')
			       OR has_function_privilege('tappa_app', 'public.op_record_auth_event(text, text, uuid)', 'EXECUTE'),
			       has_function_privilege('tappa_operator', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE')
			       AND has_function_privilege('tappa_operator', 'public.op_record_auth_event(text, text, uuid)', 'EXECUTE'),
			       (SELECT coalesce(string_agg(a.attname || ':' || x.privilege_type, ',' ORDER BY a.attnum, x.privilege_type), '')
			          FROM pg_attribute a, aclexplode(a.attacl) AS x
			         WHERE a.attrelid = 'public.operator_audit_log'::regclass AND a.attnum > 0
			           AND x.grantee = 'tappa_opdefiner'::regrole)`).
			Scan(&s.reads, &s.begin, &s.record, &s.appExecute, &s.opExecute, &s.definerACL); err != nil {
			t.Fatalf("read the state: %v", err)
		}
		s.tickets, s.ticketsValid = opConstraint(t, ctx, q, "operator_read_tickets", "operator_read_tickets_kind_check")
		s.kinds, s.kindsValid = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_kind_check")
		s.shape, s.shapeValid = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_actor_shape")
		s.definerSelect = opColumns(t, ctx, q, "tappa_opdefiner", "operator_audit_log", "SELECT")
		s.operatorSelect = opColumns(t, ctx, q, "tappa_operator", "operator_audit_log", "SELECT")
		return s
	}
	const insertACL = "kind:INSERT,session_id:INSERT,actor_admin_id:INSERT,target_admin_id:INSERT,target_tenant_id:INSERT," +
		"target_scope:INSERT,page_number:INSERT,page_size:INSERT,detail:INSERT"
	acl30 := "id:SELECT," + insertACL
	acl31 := "id:SELECT,at:SELECT,kind:INSERT,kind:SELECT,session_id:INSERT,session_id:SELECT,actor_admin_id:INSERT,actor_admin_id:SELECT," +
		"target_admin_id:INSERT,target_admin_id:SELECT,target_tenant_id:INSERT,target_tenant_id:SELECT,target_scope:INSERT,target_scope:SELECT," +
		"page_number:INSERT,page_number:SELECT,page_size:INSERT,page_size:SELECT,detail:INSERT,detail:SELECT"
	notValid := func(def string, nv bool) string {
		if nv {
			return def + " NOT VALID"
		}
		return def
	}
	at31 := func(s state, when string, ticketsNV, auditNV bool) {
		t.Helper()
		if s.reads != 1 || s.begin != body31 || s.record != rec31 || s.appExecute || !s.opExecute ||
			s.definerACL != acl31 || s.operatorSelect != "" ||
			s.tickets != notValid(opKindCheckDef(opKindsAt31), ticketsNV) || s.ticketsValid == ticketsNV ||
			s.kinds != notValid(opKindCheckDef(opAuditKinds), auditNV) || s.kindsValid == auditNV ||
			s.shape != notValid(opActorShapeDef(opAuthEventKinds), auditNV) || s.shapeValid == auditNV {
			t.Errorf("%s: reads=%d begin is 00031's=%v record is 00031's=%v app=%v operator=%v acl=(%s) operator SELECT=(%s)\n tickets=%s (%v)\n kinds=%s (%v)\n shape=%s (%v)\n want 00031's state, tickets NOT VALID=%v, audit NOT VALID=%v",
				when, s.reads, s.begin == body31, s.record == rec31, s.appExecute, s.opExecute, s.definerACL, s.operatorSelect,
				s.tickets, s.ticketsValid, s.kinds, s.kindsValid, s.shape, s.shapeValid, ticketsNV, auditNV)
		}
	}
	at30 := func(s state, when string, ticketsNV, auditNV bool) {
		t.Helper()
		if s.reads != 0 || s.begin != body30 || s.record != rec26 || s.appExecute || !s.opExecute ||
			s.definerACL != acl30 || s.definerSelect != "id" || s.operatorSelect != "" ||
			s.tickets != notValid(opKindCheckDef(opKindsAt30), ticketsNV) || s.ticketsValid == ticketsNV ||
			s.kinds != notValid(opKindCheckDef(opAuditKinds27), auditNV) || s.kindsValid == auditNV ||
			s.shape != notValid(opActorShapeDef(opAuthEventKinds26), auditNV) || s.shapeValid == auditNV {
			t.Errorf("%s: reads=%d begin is 00030's=%v record is 00026's=%v app=%v operator=%v acl=(%s) definer SELECT=(%s) operator SELECT=(%s)\n tickets=%s (%v)\n kinds=%s (%v)\n shape=%s (%v)\n want 00030's state, tickets NOT VALID=%v, audit NOT VALID=%v",
				when, s.reads, s.begin == body30, s.record == rec26, s.appExecute, s.opExecute, s.definerACL, s.definerSelect, s.operatorSelect,
				s.tickets, s.ticketsValid, s.kinds, s.kindsValid, s.shape, s.shapeValid, ticketsNV, auditNV)
		}
	}
	at31(read(tx), "PREMISE before Down", false, false)

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	// branch opens a savepoint holding no ticket outside 00030's set and no audit row outside
	// 00027's (the append-only trigger is disabled inside the savepoint for the delete, as
	// TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain's second branch does).
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
			{`DELETE FROM operator_read_tickets WHERE kind <> ALL ($1)`, []any{opKindsAt30}},
			{`ALTER TABLE operator_audit_log DISABLE TRIGGER operator_audit_log_append_only`, nil},
			{`DELETE FROM operator_audit_log WHERE kind <> ALL ($1) AND NOT EXISTS (SELECT 1 FROM operator_read_tickets k WHERE k.audit_id = operator_audit_log.id)`, []any{opAuditKinds27}},
			{`ALTER TABLE operator_audit_log ENABLE TRIGGER operator_audit_log_append_only`, nil},
		} {
			if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
				t.Fatalf("clear the kinds 00030/00027 do not know, inside the transaction: %v", err)
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

	// Branch 1: nothing outside the old sets -> all VALIDATED; Up again -> 00031.
	sp := branch()
	opRunSection(t, ctx, sp, down, "00031 Down with nothing outside the old sets")
	at30(read(sp), "after Down (nothing outside the old sets)", false, false)
	// The trap a REVOKE ALL would have sprung: op_begin_read (00030's) still writes its row
	// and ticket, which needs 00027's SELECT (id) for its RETURNING.
	if _, err := opBegin(t, ctx, sp, hash, legalVersionsReadKind, opLegalParams(1, 10)); err != nil {
		t.Errorf("after Down a 'legal_versions' first phase fails (00027's grant gone?): %v", err)
	}
	_, err := opBegin(t, ctx, sp, hash, operatorAuditReadKind, opLogParams("", 1, 50))
	opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "after Down, an 'operator_audit' first phase")
	opRunSection(t, ctx, sp, up, "00031 Up again")
	at31(read(sp), "after Up again", false, false)
	done(sp)

	// Branch 2: an unconsumed 'operator_audit' ticket -> the ticket CHECK NOT VALID.
	sp = branch()
	if _, err := opBegin(t, ctx, sp, hash, operatorAuditReadKind, opLogParams("read", 1, 50)); err != nil {
		t.Fatalf("an 'operator_audit' first phase: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00031 Down with an 'operator_audit' ticket present")
	at30(read(sp), "after Down (an 'operator_audit' ticket)", true, false)
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at)
	                              SELECT $1, $2, 'operator_audit', l.id, clock_timestamp() + interval '30 seconds'
	                                FROM operator_audit_log l WHERE l.session_id = $2 LIMIT 1`, opRandHex(t), session),
		sqlstateCheckViolation, "a NEW 'operator_audit' ticket after Down (NOT VALID still binds new rows)")
	opRunSection(t, ctx, sp, up, "00031 Up again over the 'operator_audit' ticket")
	at31(read(sp), "after Up again (an 'operator_audit' ticket)", false, false)
	done(sp)

	// Branch 3: a 'password_ok' row -> the audit kind CHECK and actor_shape NOT VALID.
	sp = branch()
	opLogInsert(t, ctx, sp, opLogRow{kind: "password_ok", target: &a.id, after: ""})
	opRunSection(t, ctx, sp, down, "00031 Down with a 'password_ok' row present")
	at30(read(sp), "after Down (a 'password_ok' row)", false, true)
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('password_ok', $1)`, a.id),
		sqlstateCheckViolation, "a NEW 'password_ok' row after Down (NOT VALID still binds new rows)")
	opWantClean(t, opRecord(t, ctx, sp, "password_ok", nil, a.id), sqlstateInvalidParameter,
		"op_record_auth_event: kind is not a pre-session failure kind", "after Down, 00026's writer and 'password_ok'")
	// COUNTED LIMITS (00031's Down comment): over that row 00027's Up fails, and 00027's Down
	// does too once no 'read'/'legal_publish' row is left to send it to its NOT VALID branch.
	for _, c := range []struct {
		what  string
		setup []string
		sql   string
	}{
		{"00027's Up with a 'password_ok' row present", nil, up27},
		{"00027's Down with a 'password_ok' row and no 'read' or 'legal_publish' row", []string{
			`DELETE FROM operator_read_tickets`,
			`ALTER TABLE operator_audit_log DISABLE TRIGGER operator_audit_log_append_only`,
			`DELETE FROM operator_audit_log WHERE kind IN ('read', 'legal_publish')`,
			`ALTER TABLE operator_audit_log ENABLE TRIGGER operator_audit_log_append_only`,
		}, down27},
	} {
		inner, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range c.setup {
			if _, err := inner.Exec(ctx, s); err != nil {
				t.Fatalf("%s: setup %q: %v", c.what, s, err)
			}
		}
		_, e := inner.Conn().PgConn().Exec(ctx, c.sql).ReadAll()
		if code, _ := opCode(e); code != sqlstateCheckViolation {
			t.Errorf("COUNTED LIMIT moved: %s answered %v; measured was 23514 -- update 00031's Down comment and ADR 0021's OP-14 note", c.what, e)
		}
		if err := inner.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	opRunSection(t, ctx, sp, up, "00031 Up again over the 'password_ok' row")
	at31(read(sp), "after Up again (a 'password_ok' row)", false, false)
	done(sp)

	// Branch 4: a LATER migration's kinds alone. Its Up widened both sets, a ticket and a row
	// of its kinds were written, its Down put 00031's sets back NOT VALID and left them.
	sp = branch()
	for _, s := range []string{
		`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
		`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN ('` + strings.Join(opKindsAt31, "', '") + `', 'zz_later_read'))`,
		`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`,
		`ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN ('` + strings.Join(opAuditKinds, "', '") + `', 'zz_later_kind'))`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Up: %v", err)
		}
	}
	opForgeRead(t, ctx, sp, session, a.id, "zz_later_read", opRandHex(t), nil, opCommittedXact(t, ctx), "30 seconds")
	opLogInsert(t, ctx, sp, opLogRow{kind: "zz_later_kind", session: &session, actor: &a.id})
	for _, s := range []string{
		`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
		`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN ('` + strings.Join(opKindsAt31, "', '") + `')) NOT VALID`,
		`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`,
		`ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN ('` + strings.Join(opAuditKinds, "', '") + `')) NOT VALID`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Down: %v", err)
		}
	}
	opRunSection(t, ctx, sp, down, "00031 Down after a later migration's Down left its ticket and its row")
	at30(read(sp), "after Down (a later migration's kinds)", true, true)
	opRunSection(t, ctx, sp, up, "00031 Up again with a later migration's ticket and row present (it composes)")
	at31(read(sp), "after Up again (a later migration's kinds)", true, true)
	done(sp)

	// consumedOnly is the premise of the two consumed-ticket branches: outside set there is
	// exactly one ticket, and it is consumed -- so only a condition that counts consumed
	// tickets sends the CHECK to its NOT VALID branch.
	consumedOnly := func(q pgx.Tx, set []string, what string) {
		t.Helper()
		var consumed, unconsumed int64
		if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE consumed_at IS NOT NULL), count(*) FILTER (WHERE consumed_at IS NULL)
		                             FROM operator_read_tickets WHERE kind <> ALL ($1)`, set).Scan(&consumed, &unconsumed); err != nil {
			t.Fatal(err)
		}
		if consumed != 1 || unconsumed != 0 {
			t.Fatalf("PREMISE (%s): %d consumed and %d unconsumed ticket(s) outside %v; want one consumed, none unconsumed", what, consumed, unconsumed, set)
		}
	}

	// Branch 5 (round 2): ONLY a CONSUMED 'operator_audit' ticket -- production's case. The
	// shipped first phase writes it, the shipped read consumes it; it is still a row 00030's
	// CHECK refuses, so the Down's ticket CHECK goes NOT VALID.
	sp = branch()
	raw, err := opBegin(t, ctx, sp, hash, operatorAuditReadKind, opLogParams("read", 1, 50))
	if err != nil {
		t.Fatalf("an 'operator_audit' first phase: %v", err)
	}
	if _, err := sp.Exec(ctx, `UPDATE operator_read_tickets SET created_xact = $1::xid8 WHERE ticket_hash = $2`,
		opCommittedXact(t, ctx), opLogTicketHash(raw, "read", 1, 50)); err != nil {
		t.Fatalf("name a committed transaction on the ticket: %v", err)
	}
	if _, err := opReadLog(t, ctx, sp, hash, raw, "read", 1, 50); err != nil {
		t.Fatalf("the shipped read consumes the ticket: %v", err)
	}
	consumedOnly(sp, opKindsAt30, "a consumed 'operator_audit' ticket")
	opRunSection(t, ctx, sp, down, "00031 Down with only a CONSUMED 'operator_audit' ticket outside 00030's set")
	at30(read(sp), "after Down (a consumed 'operator_audit' ticket)", true, false)
	opRunSection(t, ctx, sp, up, "00031 Up again over the consumed 'operator_audit' ticket")
	at31(read(sp), "after Up again (a consumed 'operator_audit' ticket)", false, false)
	done(sp)

	// Branch 6 (round 2): ONLY a LATER migration's CONSUMED ticket. Its Up widened the ticket
	// set, its read wrote and consumed a ticket, its Down put 00031's set back NOT VALID and
	// left the row. 00031's Down: the ticket CHECK NOT VALID; 00031's Up again COMPOSES over
	// it (its own ticket condition counts the consumed ticket); the audit CHECKs VALIDATED.
	sp = branch()
	for _, s := range []string{
		`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`,
		`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN ('` + strings.Join(opKindsAt31, "', '") + `', 'zz_later_read'))`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("simulate a later migration's Up: %v", err)
		}
	}
	later := opForgeRead(t, ctx, sp, session, a.id, "zz_later_read", opRandHex(t), nil, opCommittedXact(t, ctx), "30 seconds")
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE operator_read_tickets SET consumed_at = clock_timestamp() WHERE id = $1`, []any{later}},
		{`ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check`, nil},
		{`ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check CHECK (kind IN ('` + strings.Join(opKindsAt31, "', '") + `')) NOT VALID`, nil},
	} {
		if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("consume the later ticket and simulate the later migration's Down: %v", err)
		}
	}
	consumedOnly(sp, opKindsAt31, "a later migration's consumed ticket")
	opRunSection(t, ctx, sp, down, "00031 Down with only a later migration's CONSUMED ticket outside the sets")
	at30(read(sp), "after Down (a later migration's consumed ticket)", true, false)
	opRunSection(t, ctx, sp, up, "00031 Up again over a later migration's CONSUMED ticket (it composes)")
	at31(read(sp), "after Up again (a later migration's consumed ticket)", true, false)
	done(sp)

	// And with nothing outside the sets, Down and Up once more: VALIDATED both ways.
	sp = branch()
	opRunSection(t, ctx, sp, down, "00031 Down")
	opRunSection(t, ctx, sp, up, "00031 Up")
	at31(read(sp), "after Down and Up", false, false)
	done(sp)
}

// TestOperator00031_PreconditionRefusesAWrongCluster: 00031's first statement refuses the role
// shapes 00026's, 00027's, 00029's and 00030's refuse (absent, over-privileged, joined by
// membership in either direction), with SQLSTATE 55000 naming 00031, and passes the cluster
// this suite runs on.
func TestOperator00031_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	up, _ := opMigrationSections(t, op00031File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00031's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"roles present", nil, ""},
		{"both roles absent", []string{`ALTER ROLE tappa_operator RENAME TO zz_op14_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op14_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner without BYPASSRLS", []string{`ALTER ROLE tappa_opdefiner NOBYPASSRLS`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_opdefiner is a member", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
	} {
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
		case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00031")):
			t.Errorf("%s: precondition answered %q %q, want %s naming 00031 and containing %q", c.what, code, msg, sqlstatePrerequisiteState, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("%s: rollback: %v", c.what, err)
		}
	}
}

// TestOperator00031_CallersTempTableIsNeverRead is ADR 0021 §6's temp-table shadow for the
// read and the two replaced functions: the caller creates temp tables with every name they
// touch (the four operator tables, tenants), fills them with forged rows -- a forged session,
// forged names for a REAL operator and a REAL tenant, a forged audit row dated after
// everything -- and GRANTs them to tappa_opdefiner (the step without which a broken
// search_path would fail with "permission denied" and look refused); the functions still
// read and write the real ones.
func TestOperator00031_CallersTempTableIsNeverRead(t *testing.T) {
	ctx, tx := opTx(t)
	opNoRowAtOrAfterBase(t, ctx, tx)
	a := opNewActive(t, ctx, tx)
	name := opNamed(t, ctx, tx, a.id)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenantName := "op14 shadow tenant " + opToken(t)
	tenant := opNewTenant(t, ctx, tx, tenantName, 0)
	real := opLogInsert(t, ctx, tx, opLogRow{kind: "read", session: &session, actor: &a.id, tenant: &tenant,
		scope: tenantDetailReadKind, after: "1 second"})
	forgedSession := opRandHex(t)

	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		for _, s := range []string{
			`CREATE TEMP TABLE tenants (id uuid, name text)`,
			`CREATE TEMP TABLE platform_sessions (id uuid DEFAULT gen_random_uuid(), admin_id uuid, token_hash text,
			     created_at timestamptz DEFAULT clock_timestamp(), mfa_verified_at timestamptz,
			     last_used_at timestamptz DEFAULT clock_timestamp(), revoked_at timestamptz)`,
			`CREATE TEMP TABLE platform_admins (id uuid, email text, display_name text, status text, totp_failures integer,
			     totp_locked_until timestamptz)`,
			`CREATE TEMP TABLE operator_audit_log (id uuid DEFAULT gen_random_uuid(), at timestamptz DEFAULT clock_timestamp(),
			     kind text, session_id uuid, actor_admin_id uuid, target_admin_id uuid, target_tenant_id uuid,
			     target_scope text, page_number integer, page_size integer, detail jsonb DEFAULT '{}')`,
			`CREATE TEMP TABLE operator_read_tickets (id uuid DEFAULT gen_random_uuid(), ticket_hash text, session_id uuid,
			     kind text, target_tenant_id uuid, audit_id uuid, created_at timestamptz DEFAULT clock_timestamp(),
			     created_xact xid8 DEFAULT '3'::xid8, expires_at timestamptz, consumed_at timestamptz)`,
			`GRANT ALL ON pg_temp.tenants, pg_temp.platform_sessions, pg_temp.platform_admins, pg_temp.operator_audit_log,
			     pg_temp.operator_read_tickets TO tappa_opdefiner`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return err
			}
		}
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO pg_temp.tenants VALUES ($1, 'SHADOW name of the real tenant')`, []any{tenant}},
			{`INSERT INTO pg_temp.platform_admins VALUES ($1, 'shadow@example.test', 'SHADOW name of the real operator', 'active', 0, NULL)`, []any{a.id}},
			{`INSERT INTO pg_temp.platform_sessions (admin_id, token_hash, mfa_verified_at) VALUES ($1, $2, clock_timestamp())`, []any{a.id, forgedSession}},
			{`INSERT INTO pg_temp.operator_audit_log (at, kind, session_id, actor_admin_id, detail)
			  VALUES ($1::timestamptz + interval '1 year', 'logout', $2, $3, '{"SHADOW": true}')`, []any{opLogBase, session, a.id}},
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
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.tenants) + (SELECT count(*) FROM pg_temp.platform_admins)
		                              + (SELECT count(*) FROM pg_temp.platform_sessions) + (SELECT count(*) FROM pg_temp.operator_audit_log)`).Scan(&reachable)
	}); err != nil {
		t.Fatalf("the definer role cannot read the shadow (%v); the GRANT step is what makes this test mean anything", err)
	}
	if reachable != 4 {
		t.Fatalf("the definer role sees %d forged rows, want 4", reachable)
	}

	_, err := opBegin(t, ctx, tx, forgedSession, operatorAuditReadKind, opLogParams("", 1, 50))
	opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read with a session that exists only in the caller's temp table")
	audit0 := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session)
	if _, err := opBegin(t, ctx, tx, hash, operatorAuditReadKind, opLogParams("", 1, 50)); err != nil {
		t.Fatalf("op_begin_read with the real session: %v", err)
	}
	if d := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session) - audit0; d != 1 {
		t.Errorf("%d 'read' row(s) reached the REAL log, want 1", d)
	}
	if err := opRecord(t, ctx, tx, "password_ok", nil, a.id); err != nil {
		t.Fatalf("op_record_auth_event 'password_ok': %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE kind = 'password_ok' AND target_admin_id = $1`, a.id); n != 1 {
		t.Errorf("%d 'password_ok' row(s) reached the REAL log, want 1", n)
	}
	rows, err := opReadLog(t, ctx, tx, hash, opForgeLog(t, ctx, tx, session, a.id, "", 1, 50, opCommittedXact(t, ctx)), "", 1, 50)
	if err != nil {
		t.Fatalf("op_read_audit: %v", err)
	}
	if len(rows) == 0 || rows[0].ID != real {
		t.Fatalf("the first row is not the real log's newest row (%d rows); the shadow's row, dated a year later, must not be read", len(rows))
	}
	if opDeref(rows[0].ActorName) != name || opDeref(rows[0].TargetTenantName) != tenantName {
		t.Errorf("the names come from the shadow: actor %s, tenant %s; want %q and %q", opDeref(rows[0].ActorName),
			opDeref(rows[0].TargetTenantName), name, tenantName)
	}
	for _, r := range rows {
		if strings.Contains(opDeref(r.ActorName)+opDeref(r.TargetTenantName)+opDeref(r.TargetAdminName), "SHADOW") {
			t.Errorf("op_read_audit returned a name of the caller's temp tables: %+v", r)
		}
	}
	var shadowAudit, shadowTickets int64
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.operator_audit_log), (SELECT count(*) FROM pg_temp.operator_read_tickets)`).
			Scan(&shadowAudit, &shadowTickets)
	}); err != nil {
		t.Fatal(err)
	}
	if shadowAudit != 1 || shadowTickets != 0 {
		t.Errorf("the CALLER's temp tables were written: audit rows=%d (want the 1 forged) tickets=%d (want 0)", shadowAudit, shadowTickets)
	}
}

// ---------------------------------------------------- the kind set, four copies --

// TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree holds the four copies of the
// audit kind set equal on the database (the fifth, the Go constants, is held to the same
// list by TestOperatorAuditKinds_TheTypedConstantsAreTheList):
//   - operator_audit_log_kind_check names exactly opAuditKinds, and actor_shape's pre-session
//     arm exactly opAuthEventKinds;
//   - OperatorAuditKinds() is opAuditKinds, in order;
//   - op_record_auth_event, offered EVERY kind the CHECK names, writes a row for exactly
//     opAuthEventKinds and refuses the session kinds (22023, no row);
//   - op_begin_read's 'operator_audit' first phase takes EVERY kind the CHECK names and the empty filter,
//     and refuses a kind outside it;
//   - op_read_audit reads {"filter": k} out of a filter row for EVERY kind the CHECK names and
//     for "all", and not for a kind outside it.
//
// So a migration that widens the kind CHECK (OP-15, OP-16) and not the two functions' lists
// and the Go list turns this red, and the other way round.
func TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree(t *testing.T) {
	ctx, tx := opTx(t)
	opNoRowAtOrAfterBase(t, ctx, tx)
	def, _ := opConstraint(t, ctx, tx, "operator_audit_log", "operator_audit_log_kind_check")
	arrays := opQuotedArrays(def)
	if len(arrays) != 1 {
		t.Fatalf("the kind CHECK is not one closed list: %s", def)
	}
	schema := arrays[0]
	if !slices.Equal(schema, opAuditKinds) {
		t.Errorf("operator_audit_log_kind_check names %v, want %v", schema, opAuditKinds)
	}
	shape, _ := opConstraint(t, ctx, tx, "operator_audit_log", "operator_audit_log_actor_shape")
	if arms := opQuotedArrays(shape); len(arms) != 2 || !slices.Equal(arms[0], opAuthEventKinds) || !slices.Equal(arms[1], opAuthEventKinds) {
		t.Errorf("actor_shape's arms name %v, want %v twice", arms, opAuthEventKinds)
	}
	var goList []string
	for _, k := range OperatorAuditKinds() {
		goList = append(goList, string(k))
	}
	if !slices.Equal(goList, opAuditKinds) {
		t.Errorf("OperatorAuditKinds() is %v, want %v", goList, opAuditKinds)
	}

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	for _, k := range schema {
		before := opAudit(t, ctx, tx)
		err := opRecord(t, ctx, tx, k, nil, a.id)
		if slices.Contains(opAuthEventKinds, k) {
			if err != nil || opAudit(t, ctx, tx)-before != 1 {
				t.Errorf("op_record_auth_event(%q): %v, %d row(s); want one row", k, err, opAudit(t, ctx, tx)-before)
			}
		} else {
			opWantClean(t, err, sqlstateInvalidParameter, recordKindRefusal31, "op_record_auth_event("+k+")")
			if d := opAudit(t, ctx, tx) - before; d != 0 {
				t.Errorf("op_record_auth_event(%q) refused and left %d row(s)", k, d)
			}
		}
	}
	for _, k := range append(slices.Clone(schema), "") {
		if _, err := opBegin(t, ctx, tx, hash, operatorAuditReadKind, opLogParams(k, 1, 50)); err != nil {
			t.Errorf("op_begin_read 'operator_audit' with the filter %q: %v", k, err)
		}
	}
	for _, k := range []string{"zz_not_a_kind", "all", "Login", "read "} {
		_, err := opBegin(t, ctx, tx, hash, operatorAuditReadKind, opLogParams(k, 1, 50))
		opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "op_begin_read 'operator_audit' with the filter "+strconv.Quote(k))
	}

	// The read's filter shape: one filter row per value, dated in the order written.
	values := append(append([]string{"all"}, schema...), "zz_not_a_kind")
	ids := map[uuid.UUID]string{}
	for i, v := range values {
		id := opLogInsert(t, ctx, tx, opLogRow{kind: "read", session: &session, actor: &a.id, scope: operatorAuditReadKind,
			number: 1, size: 50, detail: `{"filter": ` + strconv.Quote(v) + `}`, after: strconv.Itoa(len(values)-i) + " seconds"})
		ids[id] = v
	}
	rows, err := opReadLog(t, ctx, tx, hash, opForgeLog(t, ctx, tx, session, a.id, "", 1, 200, opCommittedXact(t, ctx)), "", 1, 200)
	if err != nil {
		t.Fatalf("op_read_audit: %v", err)
	}
	if len(rows) < len(values) {
		t.Fatalf("the read returned %d rows, want at least the %d filter rows", len(rows), len(values))
	}
	for i, r := range rows[:len(values)] {
		v, ok := ids[r.ID]
		if !ok || v != values[i] {
			t.Fatalf("row %d is not the filter row %q (the fixture rows do not head the page)", i, values[i])
		}
		known := v != "zz_not_a_kind"
		if r.DetailRecognised != known || (known && opDeref(r.FilterKind) != v) || (!known && r.FilterKind != nil) {
			t.Errorf("the filter row %q reads recognised=%v filter=%s; want recognised=%v and the value", v, r.DetailRecognised, opDeref(r.FilterKind), known)
		}
	}
}

// opDBPackageFiles parses package db's product source the way the go tool selects it for the
// test's own build context (go/build's GoFiles: no _test.go, no file of another tag).
func opDBPackageFiles(t *testing.T) (*token.FileSet, []*ast.File) {
	t.Helper()
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
	return fset, files
}

// opTypedConstValues type-checks one package (files, with the standard library's go/types and
// the importer given) and returns, for each of typeNames, EVERY package-level constant whose
// type is that named type -- whichever file declares it, however it is spelled -- as
// name -> value. A type error is an error, not an empty answer.
func opTypedConstValues(fset *token.FileSet, files []*ast.File, imp types.Importer, typeNames ...string) (map[string]map[string]string, error) {
	var typeErrs []error
	conf := types.Config{Importer: imp, Error: func(e error) { typeErrs = append(typeErrs, e) }}
	pkg, _ := conf.Check("db", fset, files, nil)
	if len(typeErrs) > 0 {
		return nil, fmt.Errorf("type-check: %d error(s), the first: %w", len(typeErrs), typeErrs[0])
	}
	out := map[string]map[string]string{}
	for _, tn := range typeNames {
		named, ok := pkg.Scope().Lookup(tn).(*types.TypeName)
		if !ok {
			return nil, fmt.Errorf("the package declares no type %s", tn)
		}
		out[tn] = map[string]string{}
		for _, name := range pkg.Scope().Names() {
			c, ok := pkg.Scope().Lookup(name).(*types.Const)
			if !ok || !types.Identical(c.Type(), named.Type()) {
				continue
			}
			if c.Val().Kind() != constant.String {
				return nil, fmt.Errorf("%s is a %s constant of a non-string value", name, tn)
			}
			out[tn][name] = constant.StringVal(c.Val())
		}
	}
	return out, nil
}

// opSortedValues is a name -> value map's values, sorted.
func opSortedValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func opSorted(s []string) []string {
	out := slices.Clone(s)
	sort.Strings(out)
	return out
}

// TestOperatorAuditKinds_TheTypedConstantsAreTheList pins, by TYPE-CHECKING package db's
// product source (go/build's GoFiles; go/types with the standard library's source importer,
// no other dependency), that:
//   - the package-level constants of type OperatorAuditKind -- every file go/build selects for
//     the test's own build context, every spelling -- hold exactly opAuditKinds' values, each
//     once; and OperatorAuditKinds() returns that list in the CHECK's order;
//   - the package-level constants of type OperatorAuthEvent hold exactly opAuthEventKinds;
//   - Known() is true for each kind and false for the empty value, another case, a trailing
//     space, "all" (the filter's word for no filter, not a kind) and a kind no migration adds.
//
// opAuditKinds and opAuthEventKinds are held to the database by
// TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree; this half needs no database.
// Threat model: an ACCIDENTAL change -- a kind added to one copy and not another. Code written
// to slip past a type-checked reading is for code review. CONTROLS: the same reader, on
// synthetic packages, counts a constant in a second file and a typeless spec with a
// conversion, and does not count an untyped constant or a constant of another type.
// COUNTED LIMIT (OP-13's L12, the same reader): "every file" is the files go/build selects for
// the context the test runs in.
func TestOperatorAuditKinds_TheTypedConstantsAreTheList(t *testing.T) {
	fset, files := opDBPackageFiles(t)
	got, err := opTypedConstValues(fset, files, importer.ForCompiler(fset, "source", nil), "OperatorAuditKind", "OperatorAuthEvent")
	if err != nil {
		t.Fatalf("package db: %v", err)
	}
	if v := opSortedValues(got["OperatorAuditKind"]); !slices.Equal(v, opSorted(opAuditKinds)) || len(got["OperatorAuditKind"]) != len(opAuditKinds) {
		t.Errorf("the OperatorAuditKind constants hold %v (%d constants), want %v once each", v, len(got["OperatorAuditKind"]), opAuditKinds)
	}
	if v := opSortedValues(got["OperatorAuthEvent"]); !slices.Equal(v, opSorted(opAuthEventKinds)) || len(got["OperatorAuthEvent"]) != len(opAuthEventKinds) {
		t.Errorf("the OperatorAuthEvent constants hold %v (%d constants), want %v once each", v, len(got["OperatorAuthEvent"]), opAuthEventKinds)
	}
	var list []string
	for _, k := range OperatorAuditKinds() {
		list = append(list, string(k))
		if !k.Known() {
			t.Errorf("Known(%q) = false", k)
		}
	}
	if !slices.Equal(list, opAuditKinds) {
		t.Errorf("OperatorAuditKinds() = %v, want %v", list, opAuditKinds)
	}
	for _, k := range []OperatorAuditKind{"", "Login", "login ", " read", "all", "zz_not_a_kind", "password_ok\x00"} {
		if k.Known() {
			t.Errorf("Known(%q) = true", k)
		}
	}
	// A copy, not the array: changing what OperatorAuditKinds returned changes nothing.
	l := OperatorAuditKinds()
	l[0] = "zz"
	if OperatorAuditKinds()[0] != OperatorAuditLoginFailed {
		t.Error("OperatorAuditKinds hands out the array itself")
	}

	const head = "package db\n\ntype OperatorAuditKind string\n\ntype OperatorAuthEvent string\n\ntype Other string\n\n"
	for _, c := range []struct {
		name string
		srcs []string
		want string
	}{
		{"typed specs", []string{head + "const (\n\tA OperatorAuditKind = \"a\"\n\tB OperatorAuditKind = \"b\"\n)\n"}, "a,b"},
		{"a constant in a second file", []string{head + "const A OperatorAuditKind = \"a\"\n", "package db\n\nconst D OperatorAuditKind = \"d\"\n"}, "a,d"},
		{"a typeless spec with a conversion", []string{head + "const (\n\tA OperatorAuditKind = \"a\"\n\tD = OperatorAuditKind(\"d\")\n)\n"}, "a,d"},
		{"an untyped constant and another type", []string{head + "const A OperatorAuditKind = \"a\"\n\nconst U = \"u\"\n\nconst O Other = \"o\"\n\nconst E OperatorAuthEvent = \"e\"\n"}, "a"},
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
		cg, err := opTypedConstValues(cfset, cfiles, importer.ForCompiler(cfset, "source", nil), "OperatorAuditKind")
		if err != nil {
			t.Fatalf("control %q: %v", c.name, err)
		}
		if v := strings.Join(opSortedValues(cg["OperatorAuditKind"]), ","); v != c.want {
			t.Errorf("CONTROL %q: the reader found %s, want %s", c.name, v, c.want)
		}
	}
}

// TestOperatorAudit_AnUnknownFilterIsRefusedWithoutARoundTrip: OperatorAudit refuses a kind
// filter outside the closed set itself -- ErrOperatorAuditFilterRefused, and NOT ONE statement
// sent (measured on a connection that counts what it is sent) -- and sends the first phase for
// every member and for "" (the control: the same fake connection sees exactly one statement).
func TestOperatorAudit_AnUnknownFilterIsRefusedWithoutARoundTrip(t *testing.T) {
	ctx := context.Background()
	for _, k := range []OperatorAuditKind{"zz_not_a_kind", "Login", "all", "read' OR '1'='1", "login\x00"} {
		c := &opNoCallConn{}
		if _, err := OperatorAudit(ctx, c, "x", OperatorAuditQuery{Kind: k, Number: 1, Size: 50}); !errors.Is(err, ErrOperatorAuditFilterRefused) {
			t.Errorf("OperatorAudit with the filter %q: %v, want ErrOperatorAuditFilterRefused", k, err)
		}
		if c.calls != 0 {
			t.Errorf("OperatorAudit with the filter %q sent %d statement(s); it is refused before a round trip", k, c.calls)
		}
	}
	for _, k := range append(OperatorAuditKinds(), "") {
		c := &opNoCallConn{}
		if _, err := OperatorAudit(ctx, c, "x", OperatorAuditQuery{Kind: k, Number: 1, Size: 50}); err == nil ||
			errors.Is(err, ErrOperatorAuditFilterRefused) || c.calls != 1 {
			t.Errorf("CONTROL: the filter %q reached the connection %d time(s) (err %v), want 1 and the fake's error", k, c.calls, err)
		}
	}
}

// ------------------------------------------------------------ op_begin_read --

// TestOpBeginRead_TheAuditKindBindsItsFilterAndPage: the kind 00031 adds to the first phase.
//   - 'operator_audit' with a filter (a kind) or without (the empty filter) and a page: one 'read' row --
//     scope 'operator_audit', the page, no tenant, detail exactly {"filter": <the kind>} or
//     {"filter": "all"} -- and one ticket whose stored hash is sha256(raw ticket || the
//     canonical {kind, page_size, page_number});
//   - 22023 and no row for every parameter object the kind does not name (listed below) --
//     page 1001 among them, the OFFSET bound -- and 28000 and no row for the six dead sessions;
//   - CONTROLS: page 1000 and size 200 pass; the four kinds 00030 named still pass phase one
//     with their own objects (the replacement kept their branches).
func TestOpBeginRead_TheAuditKindBindsItsFilterAndPage(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	rowsOf := func() (audit, tickets int64) {
		return opAudit(t, ctx, tx), opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets`)
	}
	for _, c := range []struct {
		filter, detail string
		number, size   int
	}{
		{"", `{"filter": "all"}`, 1, 50},
		{"read", `{"filter": "read"}`, 7, 20},
		{"password_ok", `{"filter": "password_ok"}`, MaxOperatorAuditPage, 200},
	} {
		a0, k0 := rowsOf()
		raw, err := opBegin(t, ctx, tx, hash, operatorAuditReadKind, opLogParams(c.filter, c.number, c.size))
		if err != nil {
			t.Fatalf("op_begin_read 'operator_audit' %q page %d/%d: %v", c.filter, c.number, c.size, err)
		}
		if a1, k1 := rowsOf(); a1-a0 != 1 || k1-k0 != 1 {
			t.Fatalf("op_begin_read 'operator_audit' wrote %d audit row(s) and %d ticket(s), want 1 and 1", a1-a0, k1-k0)
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
		if kind != "read" || scope != operatorAuditReadKind || detail != c.detail || tenant != nil || number == nil || *number != c.number ||
			size == nil || *size != c.size || ticketKind != operatorAuditReadKind || ticketTenant != nil {
			t.Errorf("the 'operator_audit' row for %q: kind=%s scope=%s detail=%s tenant=%v page=%s/%s; ticket kind=%s tenant=%v",
				c.filter, kind, scope, detail, tenant, opDeref(number), opDeref(size), ticketKind, ticketTenant)
		}
		if ticketHash != opLogTicketHash(raw, c.filter, c.number, c.size) {
			t.Errorf("the stored hash is not sha256(raw ticket || canonical {kind, page_size, page_number}) for %q", c.filter)
		}
	}

	for _, c := range []struct{ name, params string }{
		{"no key", `{}`},
		{"the kind missing", `{"page_number": 1, "page_size": 50}`},
		{"an extra key", `{"kind": "", "page_number": 1, "page_size": 50, "query": ""}`},
		{"the list's keys", opTenantsParams("", 1, 50)},
		{"the overview's key", opDetailParams(uuid.NewString())},
		{"a kind outside the set", opLogParams("zz_not_a_kind", 1, 50)},
		{"another case", opLogParams("Read", 1, 50)},
		{"a trailing space", opLogParams("read ", 1, 50)},
		{"the word all", opLogParams("all", 1, 50)},
		{"SQL in the filter", opLogParams("read' OR '1'='1", 1, 50)},
		{"a number for a kind", `{"kind": 7, "page_number": 1, "page_size": 50}`},
		{"null for a kind", `{"kind": null, "page_number": 1, "page_size": 50}`},
		{"page 1001", opLogParams("", MaxOperatorAuditPage+1, 50)},
		{"page 0", opLogParams("", 0, 50)},
		{"size 201", opLogParams("", 1, 201)},
		{"size 0", opLogParams("", 1, 0)},
		{"page as a string", `{"kind": "", "page_number": "1", "page_size": 50}`},
		{"a string, not an object", `"read"`},
	} {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, hash, operatorAuditReadKind, c.params)
		opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "op_begin_read 'operator_audit', "+c.name, hash, c.params)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read 'operator_audit', %s: %d audit row(s) and %d ticket(s) written", c.name, a1-a0, k1-k0)
		}
	}
	for _, d := range opDeadSessions(t, ctx, tx) {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, d.hash, operatorAuditReadKind, opLogParams("", 1, 50))
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read 'operator_audit', "+d.name, d.hash)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read 'operator_audit', %s: %d audit row(s) and %d ticket(s) written", d.name, a1-a0, k1-k0)
		}
	}
	id := uuid.New()
	for _, c := range []struct{ kind, params string }{
		{legalVersionsReadKind, opLegalParams(1, 10)},
		{tenantsReadKind, opTenantsParams("x", 1, 10)},
		{tenantDetailReadKind, opDetailParams(id.String())},
		{tenantPlaquesReadKind, opDetailParams(id.String())},
	} {
		if _, err := opBegin(t, ctx, tx, hash, c.kind, c.params); err != nil {
			t.Errorf("CONTROL: op_begin_read %q refused its own parameter object: %v", c.kind, err)
		}
	}
}

// ----------------------------------------------------------- op_record_auth_event --

// TestOpRecordAuthEvent_PasswordOKNamesItsAccountAndTouchesNoCounter is the OP-14 acceptance
// for the new pre-session kind:
//   - 'password_ok' by account id: one row -- no session, no actor (actor_shape's pre-session
//     arm), the account as its target, no tenant, scope, page or detail;
//   - the lock is not touched: the account's failure counter and lock stamp are the same after
//     it -- on an account with failures counted and on a LOCKED one (neither locked further
//     nor unlocked); CONTROL: 'totp_failed' in the same setup does move the counter;
//   - by id ALONE: with an address, with an address and an id, and with no id it is 22023,
//     its own fixed message, nothing in the error, no row;
//   - an id no account has is a row with no target -- not an error (00026's rule: the function
//     is no existence oracle by its result); a pending or a disabled account's id is a row
//     naming that account;
//   - through the Go accessor, as the C phase will call it, and refused with an address;
//   - the schema's half: a 'password_ok' row that claims a session is refused by actor_shape
//     (the owner too); without one it is accepted.
func TestOpRecordAuthEvent_PasswordOKNamesItsAccountAndTouchesNoCounter(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_failures = 3 WHERE id = $1`, a.id); err != nil {
		t.Fatal(err)
	}
	lock := func(id uuid.UUID) string {
		t.Helper()
		var s string
		if err := tx.QueryRow(ctx, `SELECT totp_failures || '/' || coalesce(totp_locked_until::text, '-') FROM platform_admins WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	rowsFor := func(id any) int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'password_ok' AND target_admin_id IS NOT DISTINCT FROM $1::uuid`, id)
	}

	before, lock0 := rowsFor(a.id), lock(a.id)
	if err := opRecord(t, ctx, tx, "password_ok", nil, a.id); err != nil {
		t.Fatalf("op_record_auth_event('password_ok', NULL, id): %v", err)
	}
	if d := rowsFor(a.id) - before; d != 1 {
		t.Fatalf("'password_ok' wrote %d row(s) naming the account, want 1", d)
	}
	var session, actor, tenant, scope, page, size bool
	var detail string
	if err := tx.QueryRow(ctx, `
		SELECT session_id IS NULL, actor_admin_id IS NULL, target_tenant_id IS NULL, target_scope IS NULL,
		       page_number IS NULL, page_size IS NULL, detail::text
		  FROM operator_audit_log WHERE kind = 'password_ok' AND target_admin_id = $1 ORDER BY at DESC LIMIT 1`, a.id).
		Scan(&session, &actor, &tenant, &scope, &page, &size, &detail); err != nil {
		t.Fatal(err)
	}
	if !session || !actor || !tenant || !scope || !page || !size || detail != "{}" {
		t.Errorf("the 'password_ok' row: session-null=%v actor-null=%v tenant-null=%v scope-null=%v page-null=%v/%v detail=%s; want a bare pre-session row",
			session, actor, tenant, scope, page, size, detail)
	}
	if got := lock(a.id); got != lock0 {
		t.Errorf("'password_ok' moved the lock state %s -> %s; it must not touch the counter", lock0, got)
	}
	// CONTROL: the counter CAN move in this setup -- 'totp_failed' moves it by one.
	if err := opRecord(t, ctx, tx, "totp_failed", nil, a.id); err != nil {
		t.Fatal(err)
	}
	if got := lock(a.id); !strings.HasPrefix(got, "4/") {
		t.Fatalf("CONTROL: after 'totp_failed' the lock state is %s, want 4 failures", got)
	}
	// A LOCKED account: neither locked further nor unlocked.
	locked := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_failures = 5, totp_locked_until = clock_timestamp() + interval '10 minutes' WHERE id = $1`, locked.id); err != nil {
		t.Fatal(err)
	}
	lockedBefore := lock(locked.id)
	if err := opRecord(t, ctx, tx, "password_ok", nil, locked.id); err != nil {
		t.Fatal(err)
	}
	if got := lock(locked.id); got != lockedBefore {
		t.Errorf("'password_ok' on a locked account moved its lock %s -> %s", lockedBefore, got)
	}

	// By id ALONE.
	for _, c := range []struct {
		name         string
		email, admin any
	}{
		{"an address", a.email, nil},
		{"an address and an id", a.email, a.id},
		{"no id", nil, nil},
	} {
		n0 := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'password_ok'`)
		err := opRecord(t, ctx, tx, "password_ok", c.email, c.admin)
		opWantClean(t, err, sqlstateInvalidParameter, recordPasswordById31, "'password_ok' with "+c.name, a.email, a.id.String())
		if d := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'password_ok'`) - n0; d != 0 {
			t.Errorf("'password_ok' with %s: %d row(s) written", c.name, d)
		}
	}

	// An unknown id: a row with no target. A pending and a disabled account: rows naming them.
	n0 := rowsFor(nil)
	if err := opRecord(t, ctx, tx, "password_ok", nil, uuid.New()); err != nil {
		t.Fatalf("'password_ok' for an id no account has must not be an error (existence oracle): %v", err)
	}
	if d := rowsFor(nil) - n0; d != 1 {
		t.Errorf("'password_ok' for an unknown id: %d target-less row(s), want 1", d)
	}
	pending, _ := opNewPending(t, ctx, tx, 0, 0)
	disabled := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`, disabled.id); err != nil {
		t.Fatal(err)
	}
	for _, acc := range []opAccount{pending, disabled} {
		if err := opRecord(t, ctx, tx, "password_ok", nil, acc.id); err != nil || rowsFor(acc.id) != 1 {
			t.Errorf("'password_ok' for a non-active account: err %v, %d row(s) naming it; want one", err, rowsFor(acc.id))
		}
	}

	// Through the accessor, as phase C will call it.
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return RecordOperatorAuthEvent(ctx, sp, OperatorPasswordOK, "", a.id)
	}); err != nil {
		t.Errorf("RecordOperatorAuthEvent(OperatorPasswordOK, \"\", id): %v", err)
	}
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return RecordOperatorAuthEvent(ctx, sp, OperatorPasswordOK, a.email, a.id)
	})
	if err == nil || !strings.Contains(err.Error(), "SQLSTATE 22023") || strings.Contains(err.Error(), a.email) {
		t.Errorf("RecordOperatorAuthEvent(OperatorPasswordOK, address, id): %v, want a 22023 database error without the address", err)
	}

	// The schema's half (actor_shape), the owner writing.
	_, sid := opNewSession(t, ctx, tx, a.id, true)
	opWantConstraint(t, opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_admin_id)
	                                         VALUES ('password_ok', $1, $2, $2)`, sid, a.id),
		"operator_audit_log_actor_shape", "a 'password_ok' row claiming a session and an actor")
	if err := opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('password_ok', $1)`, a.id); err != nil {
		t.Fatalf("CONTROL: a bare 'password_ok' row was refused: %v", err)
	}
}

// TestOpRecordAuthEvent_AConstraintRefusalCarriesNoRow measures and closes the OP-14 card's T1
// hole. A constraint the writer's INSERT meets -- forced here with a NOT VALID CHECK added
// inside the transaction, the shape a set drifting from the CHECKs would take -- comes back
// with the function's fixed message, the constraint's own SQLSTATE (23514), NO DETAIL and no
// HINT, and nothing about the target account in any field; nothing is written, and for
// 'totp_failed' the counter did not move either (the row and the counter go together).
// THE LOG LINE (round 2 of the OP-14 A review; ADR 0021 limit L10's channel): the handler's
// LOG line is read through the one channel a test can read it on -- the caller's, under
// client_min_messages = log -- and its message is compared WORD FOR WORD: the constraint's
// name and the SQLSTATE, nothing else; no DETAIL, no HINT, a CONTEXT that is the function's
// line and nothing more, and no value of the call (id, address) in any field of any notice
// the function raises. Under the default client_min_messages the caller gets no such LOG
// line (measured: the line reaches only a caller that asks). The server log itself is not
// read here (the same ereport; counted in the OP-14 note).
// THE HOLE, measured in the same test: the very INSERT the function makes, run bare as
// tappa_opdefiner under the same CHECK, answers with a DETAIL that holds the failing row --
// the target account's id among it. CONTROL: without the forced CHECK the calls write.
func TestOpRecordAuthEvent_AConstraintRefusalCarriesNoRow(t *testing.T) {
	var mu sync.Mutex
	var notices []pgconn.Notice
	ctx, tx := opTx(t, func(_ *pgconn.PgConn, n *pgconn.Notice) {
		mu.Lock()
		defer mu.Unlock()
		notices = append(notices, *n)
	})
	take := func() []pgconn.Notice {
		mu.Lock()
		defer mu.Unlock()
		out := notices
		notices = nil
		return out
	}
	// raised are the notices the call's PL/pgSQL raised (their CONTEXT names a PL/pgSQL
	// function). The other LOG lines a caller gets under client_min_messages = log are the
	// server's own statement logging -- on in development (log_statement = all,
	// log_min_duration_statement = 0, bind parameters included: ADR 0021's "Geliştirme
	// veritabanı bunun tersidir" note), off in production (measured 2026-09-26, the same
	// ADR) -- the server's configuration, not this function's line, so not its subject.
	raised := func(ns []pgconn.Notice) []pgconn.Notice {
		var out []pgconn.Notice
		for _, n := range ns {
			if strings.HasPrefix(n.Where, "PL/pgSQL function ") {
				out = append(out, n)
			}
		}
		return out
	}
	logLines := func(ns []pgconn.Notice) []pgconn.Notice {
		var out []pgconn.Notice
		for _, n := range raised(ns) {
			if n.SeverityUnlocalized == "LOG" {
				out = append(out, n)
			}
		}
		return out
	}
	where := regexp.MustCompile(`^PL/pgSQL function public\.op_record_auth_event\(text,text,uuid\) line [0-9]+ at RAISE$`)
	a := opNewActive(t, ctx, tx)
	failures := func() int64 {
		return opInt(t, ctx, tx, `SELECT totp_failures FROM platform_admins WHERE id = $1`, a.id)
	}
	for _, kind := range []string{"password_ok", "totp_failed"} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, `ALTER TABLE operator_audit_log ADD CONSTRAINT zz_op14_refuse CHECK (kind <> '`+kind+`') NOT VALID`); err != nil {
			t.Fatalf("force a refusal: %v", err)
		}
		before, f0 := opInt(t, ctx, sp, `SELECT count(*) FROM operator_audit_log`), failures()
		take()
		// The default client_min_messages: the caller is sent no LOG line.
		err = opExecAs(t, ctx, sp, "tappa_operator", `SELECT public.op_record_auth_event($1, NULL, $2)`, kind, a.id)
		opWantClean(t, err, sqlstateCheckViolation, recordRowRefused31, "'"+kind+"' whose row a constraint refuses",
			a.id.String(), a.email, "zz_op14_refuse")
		if l := logLines(take()); len(l) != 0 {
			t.Errorf("'%s': under the default client_min_messages the caller got %d LOG line(s); measured was none", kind, len(l))
		}
		// client_min_messages = log: exactly ONE LOG line, word for word.
		if _, err := sp.Exec(ctx, `SET LOCAL client_min_messages = log`); err != nil {
			t.Fatal(err)
		}
		err = opExecAs(t, ctx, sp, "tappa_operator", `SELECT public.op_record_auth_event($1, NULL, $2)`, kind, a.id)
		opWantClean(t, err, sqlstateCheckViolation, recordRowRefused31, "'"+kind+"' whose row a constraint refuses (client_min_messages = log)",
			a.id.String(), a.email, "zz_op14_refuse")
		got := take()
		want := `op_record_auth_event: the audit row failed constraint "zz_op14_refuse" (SQLSTATE ` + sqlstateCheckViolation + `); refused`
		if l := logLines(got); len(l) != 1 || l[0].Message != want || l[0].Detail != "" || l[0].Hint != "" || !where.MatchString(l[0].Where) {
			t.Errorf("'%s': the LOG line(s) the caller got under client_min_messages = log: %+v\n want exactly one: message %q, no DETAIL, no HINT, CONTEXT matching %s", kind, l, want, where)
		}
		for _, n := range raised(got) {
			for _, v := range []string{a.id.String(), a.email} {
				if strings.Contains(n.Message+"|"+n.Detail+"|"+n.Hint+"|"+n.Where+"|"+n.InternalQuery, v) {
					t.Errorf("'%s': a %s notice carries a value of the call (%d characters)", kind, n.SeverityUnlocalized, len(v))
				}
			}
		}
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.ConstraintName != "" {
			t.Errorf("'%s': the refusal names the constraint %q to the caller; it names it in the server's LOG line only", kind, pg.ConstraintName)
		}
		if d := opInt(t, ctx, sp, `SELECT count(*) FROM operator_audit_log`) - before; d != 0 {
			t.Errorf("'%s': the refused calls left %d row(s)", kind, d)
		}
		if f := failures(); kind == "totp_failed" && f != f0 {
			t.Errorf("'totp_failed' whose row was refused moved the counter %d -> %d; the row and the counter go together", f0, f)
		}
		// THE HOLE the handler closes: the same INSERT, uncaught.
		bare := opExecAs(t, ctx, sp, "tappa_opdefiner", `INSERT INTO public.operator_audit_log (kind, target_admin_id) VALUES ($1, $2)`, kind, a.id)
		if !errors.As(bare, &pg) || pg.Code != sqlstateCheckViolation || !strings.Contains(pg.Detail, a.id.String()) {
			t.Errorf("PREMISE: the bare INSERT under the forced CHECK answered %v; measured was a 23514 whose DETAIL holds the failing row", bare)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err := opRecord(t, ctx, tx, kind, nil, a.id); err != nil {
			t.Errorf("CONTROL: '%s' without the forced CHECK: %v", kind, err)
		}
	}
}

// -------------------------------------------------------------- the read --

// TestOpReadAudit_NewestFirstAndTheViewersOwnRowLeads is the OP-14 acceptance "the viewer's own
// 'read' row is the first row of the first page", in one transaction that sees no other
// session's commits:
//   - phase one (the shipped op_begin_read) writes the viewer's row; its ticket is made to name
//     a committed transaction by the owner (the predicate is reached without a commit); phase
//     two returns, FIRST, exactly that row -- kind 'read', scope 'operator_audit', the filter
//     read out as "all", the viewer's session, id and name;
//   - the page is the owner's own newest-first list (at DESC, id DESC) of the same rows;
//   - a tie in `at` is broken by id, descending, and the order is the same on a second read;
//   - a filter returns that kind only, in the same order;
//   - THE COUNTED LIMIT, measured: rows the OWNER writes dated after the viewer's -- it may
//     write `at` -- head the page; with the three above, the viewer's new row is fourth.
func TestOpReadAudit_NewestFirstAndTheViewersOwnRowLeads(t *testing.T) {
	ctx, tx := opTx(t)
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE at > clock_timestamp()`); n != 0 {
		t.Fatalf("PREMISE: %d audit row(s) are dated in the future; the viewer's row could not lead", n)
	}
	a := opNewActive(t, ctx, tx)
	name := opNamed(t, ctx, tx, a.id)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	// begin runs the shipped first phase and makes its ticket readable in this transaction.
	begin := func(kind string, number, size int) (string, uuid.UUID) {
		t.Helper()
		raw, err := opBegin(t, ctx, tx, hash, operatorAuditReadKind, opLogParams(kind, number, size))
		if err != nil {
			t.Fatalf("phase one: %v", err)
		}
		var auditID uuid.UUID
		if err := tx.QueryRow(ctx, `UPDATE operator_read_tickets SET created_xact = $1::xid8
		                             WHERE ticket_hash = $2 RETURNING audit_id`, xact, opLogTicketHash(raw, kind, number, size)).Scan(&auditID); err != nil {
			t.Fatalf("name a committed transaction on the ticket: %v", err)
		}
		return raw, auditID
	}
	raw, own := begin("", 1, 50)
	rows, err := opReadLog(t, ctx, tx, hash, raw, "", 1, 50)
	if err != nil {
		t.Fatalf("op_read_audit: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the first page is empty; it holds at least the viewer's own row")
	}
	r := rows[0]
	if r.ID != own || r.Kind != "read" || opDeref(r.Scope) != operatorAuditReadKind || opDeref(r.FilterKind) != "all" || !r.DetailRecognised ||
		r.SessionID == nil || *r.SessionID != session || r.ActorID == nil || *r.ActorID != a.id || opDeref(r.ActorName) != name ||
		opDeref(r.PageNumber) != "1" || opDeref(r.PageSize) != "50" {
		t.Errorf("the first row is not the viewer's own read: id ok=%v kind=%s scope=%s filter=%s recognised=%v session ok=%v actor ok=%v name=%s page=%s/%s",
			r.ID == own, r.Kind, opDeref(r.Scope), opDeref(r.FilterKind), r.DetailRecognised, r.SessionID != nil && *r.SessionID == session,
			r.ActorID != nil && *r.ActorID == a.id, opDeref(r.ActorName), opDeref(r.PageNumber), opDeref(r.PageSize))
	}
	ownerList := func(kind string, limit int) []uuid.UUID {
		t.Helper()
		qr, err := tx.Query(ctx, `SELECT id FROM operator_audit_log WHERE $1 = '' OR kind = $1 ORDER BY at DESC, id DESC LIMIT $2`, kind, limit)
		if err != nil {
			t.Fatal(err)
		}
		ids, err := pgx.CollectRows(qr, pgx.RowTo[uuid.UUID])
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	idsOf := func(rows []OperatorAuditEntry) []uuid.UUID {
		var out []uuid.UUID
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}
	// The page is the owner's newest-first list of the same rows (the read itself writes none).
	if want := ownerList("", 50); !slices.Equal(idsOf(rows), want) {
		t.Errorf("the first page (%d rows) is not the owner's newest-first list (%d rows)", len(rows), len(want))
	}

	// A tie in `at`: id breaks it, descending; and a second read gives the same order.
	t1 := opLogInsert(t, ctx, tx, opLogRow{kind: "logout", session: &session, actor: &a.id, after: "10 seconds"})
	t2 := opLogInsert(t, ctx, tx, opLogRow{kind: "logout", session: &session, actor: &a.id, after: "10 seconds"})
	other := opLogInsert(t, ctx, tx, opLogRow{kind: "locked", target: &a.id, after: "5 seconds"})
	hi, lo := t1, t2
	if hi.String() < lo.String() {
		hi, lo = lo, hi
	}
	for i := 0; i < 2; i++ {
		rows, err := opReadLog(t, ctx, tx, hash, opForgeLog(t, ctx, tx, session, a.id, "", 1, 3, xact), "", 1, 3)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(idsOf(rows), []uuid.UUID{hi, lo, other}) {
			t.Errorf("read %d: the tie in `at` is not broken by id descending (got %v, want %v)", i+1, idsOf(rows), []uuid.UUID{hi, lo, other})
		}
	}
	// The tie decides which page a row is on, not only its place on the page: page 1 of size 1
	// is the higher id, page 2 the lower.
	for i, want := range []uuid.UUID{hi, lo} {
		rows, err := opReadLog(t, ctx, tx, hash, opForgeLog(t, ctx, tx, session, a.id, "", i+1, 1, xact), "", i+1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(idsOf(rows), []uuid.UUID{want}) {
			t.Errorf("page %d of size 1 is %v, want %v (the tie is not broken by id descending when the page is cut)", i+1, idsOf(rows), want)
		}
	}
	// A filter: that kind only, in the same order.
	rows, err = opReadLog(t, ctx, tx, hash, opForgeLog(t, ctx, tx, session, a.id, "logout", 1, 20, xact), "logout", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Kind != "logout" {
			t.Errorf("the 'logout' filter returned a %q row", r.Kind)
		}
	}
	if want := ownerList("logout", 20); !slices.Equal(idsOf(rows), want) {
		t.Errorf("the 'logout' page is not the owner's newest-first list of 'logout' rows")
	}

	// THE COUNTED LIMIT: the owner's future-dated rows head the page, above a new viewer row.
	raw, own = begin("", 1, 5)
	rows, err = opReadLog(t, ctx, tx, hash, raw, "", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 4 || rows[0].ID != hi || rows[3].ID != own {
		t.Errorf("COUNTED LIMIT moved: with three owner rows dated in the future the viewer's row is not fourth (%v)", idsOf(rows))
	}
}

// opLogShape is one row of the closed-shape table: what the owner writes and what the read
// must answer.
type opLogShape struct {
	name   string
	row    opLogRow
	recog  bool
	scope  string // "" = NULL
	search string // "" = NULL
	filter string // "" = NULL
	slug   string // "" = NULL
	bytes  int32  // 0 = NULL
}

// TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList is the OP-14 acceptance "detail ham
// dönmez" through the PRODUCT's scan (readOperatorAudit), on rows the owner writes in every
// shape:
//   - the shapes ON the list read out their values -- the search class of a tenant-list read,
//     the filter of a log read, the slug and byte length of a legal publication -- and every
//     other shipped shape (every other kind, the other read kinds) is recognised with nothing
//     to read out;
//   - every shape OFF the list -- the five the development database holds that no shipped
//     code writes (a 'tenants' read with {}, {"search": true} and {"kind": "text"}, a
//     'legal_versions' and a 'tenant_detail' read with {"search": "id"}), an unknown, a
//     missing or an extra key, another type, a value outside its set, a misplaced or an
//     unknown scope -- is detail_recognised = false with every value column NULL; the row is
//     there all the same;
//   - values planted in detail -- a ticket-shaped hex string, a TOTP-shaped code, a token, an
//     address, a search term -- appear in NO field of ANY row returned;
//   - the scope is returned for the six read kinds (00032 added 'tenant_billing') and NULL for
//     an unknown one; names come from platform_admins and tenants by the row's ids (an unknown
//     tenant: no name);
//   - every read kind the ticket CHECK names is a scope the list knows, with its shipped
//     detail -- a read kind added without a shape here turns this red.
func TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList(t *testing.T) {
	ctx, tx := opTx(t)
	opNoRowAtOrAfterBase(t, ctx, tx)
	a := opNewActive(t, ctx, tx)
	actorName := opNamed(t, ctx, tx, a.id)
	target := opNewActive(t, ctx, tx)
	targetName := opNamed(t, ctx, tx, target.id)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenantName := "op14 shapes " + opToken(t)
	tenant := opNewTenant(t, ctx, tx, tenantName, 0)
	ghost := uuid.New()
	doc := uuid.New()

	ticketLike := opRandHex(t)
	totpLike := "907153"
	tokenLike, _ := opRandToken(t)
	addressLike := "planted-" + opToken(t) + "@example.test"
	termLike := "op14 planted term " + opToken(t)
	planted := []string{ticketLike, totpLike, tokenLike, addressLike, termLike}

	pre := func(kind, detail string) opLogRow { return opLogRow{kind: kind, target: &target.id, detail: detail} }
	sess := func(kind, scope, detail string) opLogRow {
		return opLogRow{kind: kind, session: &session, actor: &a.id, scope: scope, detail: detail}
	}
	readOf := func(scope, detail string) opLogRow {
		r := sess("read", scope, detail)
		switch scope {
		case tenantDetailReadKind, tenantPlaquesReadKind:
			r.tenant = &tenant
		case tenantBillingReadKind:
			r.tenant = &tenant
			r.number, r.size = 1, TenantBillingMonthsPerPage
		default:
			r.number, r.size = 1, 50
		}
		return r
	}
	legal := func(slug, docID, bytes string) string {
		return `{"slug": ` + slug + `, "document_id": ` + docID + `, "bytes": ` + bytes + `}`
	}
	q := strconv.Quote
	shapes := []opLogShape{
		// ON the list.
		{name: "login_failed {}", row: pre("login_failed", ""), recog: true},
		{name: "unknown_email {}", row: pre("unknown_email", ""), recog: true},
		{name: "totp_failed {}", row: pre("totp_failed", ""), recog: true},
		{name: "locked {}", row: pre("locked", ""), recog: true},
		{name: "enrollment_failed {}", row: pre("enrollment_failed", ""), recog: true},
		{name: "password_ok {}", row: pre("password_ok", ""), recog: true},
		{name: "login {}", row: sess("login", "", ""), recog: true},
		{name: "enrollment {}", row: sess("enrollment", "", ""), recog: true},
		{name: "logout {}", row: sess("logout", "", ""), recog: true},
		{name: "read legal_versions {}", row: readOf(legalVersionsReadKind, ""), recog: true, scope: legalVersionsReadKind},
		{name: "read tenant_detail {}", row: readOf(tenantDetailReadKind, ""), recog: true, scope: tenantDetailReadKind},
		{name: "read tenant_plaques {}", row: readOf(tenantPlaquesReadKind, ""), recog: true, scope: tenantPlaquesReadKind},
		{name: "read tenant_billing {}", row: readOf(tenantBillingReadKind, ""), recog: true, scope: tenantBillingReadKind},
		{name: "read tenants none", row: readOf(tenantsReadKind, `{"search": "none"}`), recog: true, scope: tenantsReadKind, search: "none"},
		{name: "read tenants text", row: readOf(tenantsReadKind, `{"search": "text"}`), recog: true, scope: tenantsReadKind, search: "text"},
		{name: "read tenants address", row: readOf(tenantsReadKind, `{"search": "address"}`), recog: true, scope: tenantsReadKind, search: "address"},
		{name: "read tenants id", row: readOf(tenantsReadKind, `{"search": "id"}`), recog: true, scope: tenantsReadKind, search: "id"},
		{name: "read operator_audit all", row: readOf(operatorAuditReadKind, `{"filter": "all"}`), recog: true, scope: operatorAuditReadKind, filter: "all"},
		{name: "read operator_audit password_ok", row: readOf(operatorAuditReadKind, `{"filter": "password_ok"}`), recog: true, scope: operatorAuditReadKind, filter: "password_ok"},
		{name: "legal_publish privacy", row: sess("legal_publish", "", legal(q("privacy"), q(doc.String()), "1")), recog: true, slug: "privacy", bytes: 1},
		{name: "legal_publish terms", row: sess("legal_publish", "", legal(q("terms"), q(doc.String()), "262144")), recog: true, slug: "terms", bytes: 262144},
		{name: "legal_publish imprint", row: sess("legal_publish", "", legal(q("imprint"), q(doc.String()), "4711")), recog: true, slug: "imprint", bytes: 4711},
		{name: "legal_publish cookies", row: sess("legal_publish", "", legal(q("cookies"), q(doc.String()), "99")), recog: true, slug: "cookies", bytes: 99},
		// OFF the list: the five the development database holds.
		{name: "read tenants {} (dev residue)", row: readOf(tenantsReadKind, ""), scope: tenantsReadKind},
		{name: "read tenants search:true (dev residue)", row: readOf(tenantsReadKind, `{"search": true}`), scope: tenantsReadKind},
		{name: "read tenants kind:text (dev residue)", row: readOf(tenantsReadKind, `{"kind": "text"}`), scope: tenantsReadKind},
		{name: "read legal_versions search:id (dev residue)", row: readOf(legalVersionsReadKind, `{"search": "id"}`), scope: legalVersionsReadKind},
		{name: "read tenant_detail search:id (dev residue)", row: readOf(tenantDetailReadKind, `{"search": "id"}`), scope: tenantDetailReadKind},
		// OFF the list: every other way out.
		{name: "search class in another case", row: readOf(tenantsReadKind, `{"search": "Text"}`), scope: tenantsReadKind},
		{name: "search class with an extra key (a term)", row: readOf(tenantsReadKind, `{"search": "text", "term": `+q(termLike)+`}`), scope: tenantsReadKind},
		{name: "a ticket-shaped search value", row: readOf(tenantsReadKind, `{"search": `+q(ticketLike)+`}`), scope: tenantsReadKind},
		{name: "a nested search value", row: readOf(tenantsReadKind, `{"search": {"q": `+q(termLike)+`}}`), scope: tenantsReadKind},
		{name: "search null", row: readOf(tenantsReadKind, `{"search": null}`), scope: tenantsReadKind},
		{name: "a log read with {}", row: readOf(operatorAuditReadKind, ""), scope: operatorAuditReadKind},
		{name: "a filter outside the set", row: readOf(operatorAuditReadKind, `{"filter": "zz_not_a_kind"}`), scope: operatorAuditReadKind},
		{name: "a token-shaped filter", row: readOf(operatorAuditReadKind, `{"filter": `+q(tokenLike)+`}`), scope: operatorAuditReadKind},
		{name: "a boolean filter", row: readOf(operatorAuditReadKind, `{"filter": true}`), scope: operatorAuditReadKind},
		{name: "a filter with an extra key", row: readOf(operatorAuditReadKind, `{"filter": "all", "code": `+q(totpLike)+`}`), scope: operatorAuditReadKind},
		{name: "a plaque read carrying its tenant id", row: readOf(tenantPlaquesReadKind, `{"tenant_id": `+q(tenant.String())+`}`), scope: tenantPlaquesReadKind},
		{name: "a billing read carrying its page", row: readOf(tenantBillingReadKind, `{"page_number": 1}`), scope: tenantBillingReadKind},
		{name: "a read of an unknown scope", row: readOf("zz_later_scope", "")},
		{name: "legal_publish {}", row: sess("legal_publish", "", "")},
		{name: "legal_publish with a slug in another case", row: sess("legal_publish", "", legal(q("PRIVACY"), q(doc.String()), "10"))},
		{name: "legal_publish with a slug outside the set", row: sess("legal_publish", "", legal(q("refunds"), q(doc.String()), "10"))},
		{name: "legal_publish with a document id that is not a uuid", row: sess("legal_publish", "", legal(q("privacy"), q(ticketLike), "10"))},
		{name: "legal_publish with an upper-case document id", row: sess("legal_publish", "", legal(q("privacy"), q(strings.ToUpper(doc.String())), "10"))},
		{name: "legal_publish with bytes as a string", row: sess("legal_publish", "", legal(q("privacy"), q(doc.String()), q("10")))},
		{name: "legal_publish with fractional bytes", row: sess("legal_publish", "", legal(q("privacy"), q(doc.String()), "1.5"))},
		{name: "legal_publish with zero bytes", row: sess("legal_publish", "", legal(q("privacy"), q(doc.String()), "0"))},
		{name: "legal_publish over the ceiling", row: sess("legal_publish", "", legal(q("privacy"), q(doc.String()), "262145"))},
		{name: "legal_publish carrying its body", row: sess("legal_publish", "", `{"slug": "privacy", "document_id": `+q(doc.String())+`, "bytes": 10, "body": `+q(termLike)+`}`)},
		{name: "legal_publish with a scope", row: sess("legal_publish", tenantsReadKind, legal(q("privacy"), q(doc.String()), "10")), scope: tenantsReadKind},
		{name: "a failure carrying an address", row: pre("login_failed", `{"address": `+q(addressLike)+`}`)},
		{name: "a logout with a scope", row: sess("logout", tenantsReadKind, ""), scope: tenantsReadKind},
		{name: "a login carrying a code", row: sess("login", "", `{"code": `+q(totpLike)+`}`)},
	}
	// The read kinds the ticket CHECK names, each with its shipped detail.
	shipped := map[string]string{legalVersionsReadKind: "", tenantsReadKind: `{"search": "text"}`, tenantDetailReadKind: "",
		tenantPlaquesReadKind: "", operatorAuditReadKind: `{"filter": "all"}`, tenantBillingReadKind: ""}
	ticketKinds, _ := opConstraint(t, ctx, tx, "operator_read_tickets", "operator_read_tickets_kind_check")
	for _, arr := range opQuotedArrays(ticketKinds) {
		for _, k := range arr {
			d, ok := shipped[k]
			if !ok {
				t.Fatalf("the read kind %q has no shipped detail in this test: add it here and its shape to op_read_audit's list", k)
			}
			s := opLogShape{name: "every read kind: " + k, row: readOf(k, d), recog: true, scope: k}
			if k == tenantsReadKind {
				s.search = "text"
			}
			if k == operatorAuditReadKind {
				s.filter = "all"
			}
			shapes = append(shapes, s)
		}
	}
	// One row naming a tenant no row of tenants has.
	ghostRow := readOf(tenantDetailReadKind, "")
	ghostRow.tenant = &ghost
	shapes = append(shapes, opLogShape{name: "a tenant that does not exist", row: ghostRow, recog: true, scope: tenantDetailReadKind})
	if len(shapes) > 200 {
		t.Fatalf("PREMISE: %d shapes do not fit one page of 200", len(shapes))
	}

	ids := make([]uuid.UUID, len(shapes))
	for i := range shapes {
		shapes[i].row.after = strconv.Itoa(len(shapes)-i) + " seconds"
		ids[i] = opLogInsert(t, ctx, tx, shapes[i].row)
	}
	raw := opForgeLog(t, ctx, tx, session, a.id, "", 1, 200, opCommittedXact(t, ctx))
	var rows []OperatorAuditEntry
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		rows, e = readOperatorAudit(ctx, sp, hash, readTicket{v: &raw}, OperatorAuditQuery{Number: 1, Size: 200})
		return e
	}); err != nil {
		t.Fatalf("readOperatorAudit: %v", err)
	}
	if len(rows) < len(shapes) {
		t.Fatalf("the read returned %d rows, want at least the %d shapes", len(rows), len(shapes))
	}
	for i, s := range shapes {
		r := rows[i]
		if r.ID != ids[i] {
			t.Fatalf("row %d is not the shape %q (the fixture rows do not head the page in their order)", i, s.name)
		}
		want := func(v string) string {
			if v == "" {
				return "<nil>"
			}
			return v
		}
		wantBytes := "<nil>"
		if s.bytes != 0 {
			wantBytes = strconv.Itoa(int(s.bytes))
		}
		if r.DetailRecognised != s.recog || opDeref(r.Scope) != want(s.scope) || opDeref(r.SearchClass) != want(s.search) ||
			opDeref(r.FilterKind) != want(s.filter) || opDeref(r.LegalSlug) != want(s.slug) || opDeref(r.LegalBytes) != wantBytes {
			t.Errorf("%s: recognised=%v scope=%s search=%s filter=%s slug=%s bytes=%s; want recognised=%v scope=%s search=%s filter=%s slug=%s bytes=%s",
				s.name, r.DetailRecognised, opDeref(r.Scope), opDeref(r.SearchClass), opDeref(r.FilterKind), opDeref(r.LegalSlug), opDeref(r.LegalBytes),
				s.recog, want(s.scope), want(s.search), want(s.filter), want(s.slug), wantBytes)
		}
		// The row is all there and the names are the ones the ids name.
		if r.Kind != s.row.kind || !opSameID(r.SessionID, s.row.session) || !opSameID(r.ActorID, s.row.actor) ||
			!opSameID(r.TargetAdminID, s.row.target) || !opSameID(r.TargetTenantID, s.row.tenant) {
			t.Errorf("%s: kind=%s session/actor/target/tenant do not match the row written", s.name, r.Kind)
		}
		wantActor, wantTarget, wantTenant := "<nil>", "<nil>", "<nil>"
		if s.row.actor != nil {
			wantActor = actorName
		}
		if s.row.target != nil {
			wantTarget = targetName
		}
		if s.row.tenant != nil && *s.row.tenant == tenant {
			wantTenant = tenantName
		}
		if opDeref(r.ActorName) != wantActor || opDeref(r.TargetAdminName) != wantTarget || opDeref(r.TargetTenantName) != wantTenant {
			t.Errorf("%s: names actor=%s target=%s tenant=%s; want %s, %s, %s", s.name, opDeref(r.ActorName), opDeref(r.TargetAdminName),
				opDeref(r.TargetTenantName), wantActor, wantTarget, wantTenant)
		}
		if (s.row.number != 0) != (r.PageNumber != nil) {
			t.Errorf("%s: page %s, want it %v", s.name, opDeref(r.PageNumber), s.row.number != 0)
		}
	}
	// Every TEXT field of every row returned (the uuid fields are typed and cannot carry text;
	// a six-digit code could match a uuid's hex by chance, so the numbers are compared whole).
	for _, r := range rows {
		fields := strings.Join([]string{r.Kind, opDeref(r.ActorName), opDeref(r.TargetAdminName), opDeref(r.TargetTenantName),
			opDeref(r.Scope), opDeref(r.SearchClass), opDeref(r.FilterKind), opDeref(r.LegalSlug)}, "|")
		for _, p := range planted {
			if strings.Contains(fields, p) {
				t.Errorf("a planted value (%d characters) came back in a row of kind %s", len(p), r.Kind)
			}
		}
		for _, n := range []string{opDeref(r.PageNumber), opDeref(r.PageSize), opDeref(r.LegalBytes)} {
			if n == totpLike {
				t.Errorf("the planted code came back as a number in a row of kind %s", r.Kind)
			}
		}
	}

	// An audit kind no migration names yet (a later migration's, its CHECK widened here): its
	// bare row is recognised -- nothing to read out -- and its kind comes back as itself, which
	// the Go side does not know; with a detail it is not recognised.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`); err != nil {
		t.Fatal(err)
	}
	bare := opLogInsert(t, ctx, sp, opLogRow{kind: "zz_later_kind", session: &session, actor: &a.id, after: "1 year"})
	full := opLogInsert(t, ctx, sp, opLogRow{kind: "zz_later_kind", session: &session, actor: &a.id, detail: `{"x": 1}`, after: "1 year 1 second"})
	later, err := opReadLog(t, ctx, sp, hash, opForgeLog(t, ctx, sp, session, a.id, "", 1, 2, opCommittedXact(t, ctx)), "", 1, 2)
	if err != nil {
		t.Fatalf("op_read_audit with a later kind: %v", err)
	}
	if len(later) != 2 || later[0].ID != full || later[1].ID != bare || later[0].DetailRecognised || !later[1].DetailRecognised ||
		later[1].Kind != "zz_later_kind" || OperatorAuditKind(later[1].Kind).Known() {
		t.Errorf("a later kind: %+v; want its detailed row unrecognised, its bare row recognised, the kind verbatim and unknown to Go", later)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// opSameID reports whether a returned id is the one written (both nil, or equal).
func opSameID(got, want *uuid.UUID) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

// TestOpReadAudit_PagesAreCappedOrderedAndBounded: ADR 0021 §2 iii's ceiling and 00031's OFFSET
// bound live in the read's BODY, measured with tickets the owner forges past what
// op_begin_read would issue. With 1002 rows of one kind dated in the order written:
//   - size 201 reads 200 rows -- the first 200 of the owner's list;
//   - page 2 of 50 is the owner's rows 51-100;
//   - page 1001 of 1 reads the row at offset 999 -- page 1000's -- not the 1001st row: the
//     body bounds the page at MaxOperatorAuditPage; page 1000 of 1 reads the same row.
func TestOpReadAudit_PagesAreCappedOrderedAndBounded(t *testing.T) {
	ctx, tx := opTx(t)
	opNoRowAtOrAfterBase(t, ctx, tx)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO operator_audit_log (at, kind, target_admin_id)
		SELECT $1::timestamptz + make_interval(secs => 2000 - g), 'locked', $2
		  FROM generate_series(1, 1002) AS g`, opLogBase, a.id); err != nil {
		t.Fatalf("1002 rows: %v", err)
	}
	qr, err := tx.Query(ctx, `SELECT id FROM operator_audit_log WHERE kind = 'locked' ORDER BY at DESC, id DESC LIMIT 1002`)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgx.CollectRows(qr, pgx.RowTo[uuid.UUID])
	if err != nil || len(owner) != 1002 {
		t.Fatalf("the owner's list: %d rows, err %v", len(owner), err)
	}
	read := func(number, size int) []uuid.UUID {
		t.Helper()
		rows, err := opReadLog(t, ctx, tx, hash, opForgeLog(t, ctx, tx, session, a.id, "locked", number, size, xact), "locked", number, size)
		if err != nil {
			t.Fatalf("page %d of %d: %v", number, size, err)
		}
		var ids []uuid.UUID
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	if got := read(1, 201); !slices.Equal(got, owner[:200]) {
		t.Errorf("size 201 read %d rows, want the owner's first 200", len(got))
	}
	if got := read(2, 50); !slices.Equal(got, owner[50:100]) {
		t.Errorf("page 2 of 50 is not the owner's rows 51-100")
	}
	if got := read(MaxOperatorAuditPage+1, 1); !slices.Equal(got, owner[MaxOperatorAuditPage-1:MaxOperatorAuditPage]) {
		t.Errorf("page %d of 1 is not page %d's row (the OFFSET is not bounded in the body)", MaxOperatorAuditPage+1, MaxOperatorAuditPage)
	}
	if got := read(MaxOperatorAuditPage, 1); !slices.Equal(got, owner[MaxOperatorAuditPage-1:MaxOperatorAuditPage]) {
		t.Errorf("page %d of 1 is not the owner's row %d", MaxOperatorAuditPage, MaxOperatorAuditPage)
	}
}

// TestOpReadAudit_ATicketFromThisTransactionIsRefused is ADR 0021 §2 v 3's rows A1/A2 for the
// read: a ticket op_begin_read made in THIS transaction -- at the top level, in an open
// savepoint, in a released one -- is refused (28000). CONTROL: a ticket whose transaction
// committed is read.
func TestOpReadAudit_ATicketFromThisTransactionIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	begin := func(q pgx.Tx) string {
		t.Helper()
		raw, err := opBegin(t, ctx, q, hash, operatorAuditReadKind, opLogParams("", 1, 50))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := begin(tx)
	_, err := opReadLog(t, ctx, tx, hash, raw, "", 1, 50)
	opWantClean(t, err, sqlstateInvalidAuthorization, opAuditReadRefusal, "A1: a ticket of this transaction's top level", raw)
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw = begin(sp)
	_, err = opReadLog(t, ctx, sp, hash, raw, "", 1, 50)
	opWantClean(t, err, sqlstateInvalidAuthorization, opAuditReadRefusal, "A2: a ticket of an open savepoint", raw)
	if err := sp.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = opReadLog(t, ctx, tx, hash, raw, "", 1, 50)
	opWantClean(t, err, sqlstateInvalidAuthorization, opAuditReadRefusal, "A2: a ticket of a released savepoint", raw)
	if _, err := opReadLog(t, ctx, tx, hash, opForgeLog(t, ctx, tx, session, a.id, "", 1, 50, opCommittedXact(t, ctx)), "", 1, 50); err != nil {
		t.Fatalf("CONTROL: a ticket whose transaction COMMITTED was refused: %v", err)
	}
}

// TestOpReadAudit_AForgedTicketIsRefused: a ticket is bound to its session, its KIND, its
// filter and its page. Refused (28000, ticket not consumed): another session of the same
// operator; another filter (a kind, and the empty one for a kind's ticket); another page number; another
// page size; a ticket no op_begin_read issued, and NULL; and the KIND condition's own case --
// the log read's own hash under every other read kind (five since 00032); and, the other direction, a
// log-read ticket shown to the version list with the version list's hash. CONTROL: the right
// session, filter and page are read.
func TestOpReadAudit_AForgedTicketIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	otherHash, _ := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	consumed := func() int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, session)
	}
	before := consumed()
	ticket := opForgeLog(t, ctx, tx, session, a.id, "read", 2, 50, xact)
	for _, c := range []struct {
		name, hash, kind string
		number, size     int
	}{
		{"another session", otherHash, "read", 2, 50},
		{"another filter", hash, "login", 2, 50},
		{"no filter for a filtered ticket", hash, "", 2, 50},
		{"another page", hash, "read", 3, 50},
		{"another size", hash, "read", 2, 49},
	} {
		_, err := opReadLog(t, ctx, tx, c.hash, ticket, c.kind, c.number, c.size)
		opWantClean(t, err, sqlstateInvalidAuthorization, opAuditReadRefusal, c.name, ticket)
	}
	for _, raw := range []any{opRandHex(t), nil} {
		err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			_, e := opScanLog(ctx, sp, hash, raw, "read", 2, 50)
			return e
		})
		opWantClean(t, err, sqlstateInvalidAuthorization, opAuditReadRefusal, fmt.Sprintf("a ticket never issued (%T)", raw))
	}
	for _, kind := range []string{legalVersionsReadKind, tenantsReadKind, tenantDetailReadKind, tenantPlaquesReadKind, tenantBillingReadKind} {
		raw := opRandHex(t)
		opForgeRead(t, ctx, tx, session, a.id, kind, opLogTicketHash(raw, "read", 2, 50), nil, xact, "30 seconds")
		_, err := opReadLog(t, ctx, tx, hash, raw, "read", 2, 50)
		opWantClean(t, err, sqlstateInvalidAuthorization, opAuditReadRefusal, "the log read's own hash under kind '"+kind+"'", raw)
	}
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, a.id, operatorAuditReadKind, opTicketHash(raw, 1, 10), nil, xact, "30 seconds")
	_, err := opRead(t, ctx, tx, hash, raw, 1, 10)
	opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, "op_read_legal_versions with an 'operator_audit' ticket", raw)
	if n := consumed() - before; n != 0 {
		t.Errorf("%d ticket(s) consumed by refused reads", n)
	}
	if _, err := opReadLog(t, ctx, tx, hash, ticket, "read", 2, 50); err != nil {
		t.Errorf("CONTROL: the ticket with its own session, filter and page was refused: %v", err)
	}
}

// TestOpReadAudit_RefusesEveryDeadSession: the read resolves the session through
// op_touch_session before anything else, so each of the six dead sessions is the touch refusal
// (28000) and no ticket is consumed -- even one that would otherwise match.
func TestOpReadAudit_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	xact := opCommittedXact(t, ctx)
	for _, d := range opDeadSessions(t, ctx, tx) {
		if d.id == uuid.Nil {
			_, err := opReadLog(t, ctx, tx, d.hash, opRandHex(t), "", 1, 50)
			opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash)
			continue
		}
		var admin uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT admin_id FROM platform_sessions WHERE id = $1`, d.id).Scan(&admin); err != nil {
			t.Fatal(err)
		}
		raw := opForgeLog(t, ctx, tx, d.id, admin, "", 1, 50, xact)
		_, err := opReadLog(t, ctx, tx, d.hash, raw, "", 1, 50)
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, d.name, d.hash, raw)
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, d.id); n != 0 {
			t.Errorf("%s: %d ticket(s) consumed", d.name, n)
		}
	}
}

// TestOpReadAudit_ExpiryIsTheWallClock is ADR 0021 §6 "bilet süresi" for the read: an expired
// ticket is refused, and a ticket that expires WHILE the reading transaction is open is
// refused -- through savepoints rolled back three times, and through three exception
// sub-transactions of ONE DO statement whose sleep is INSIDE it (a frozen statement_timestamp()
// would still call the ticket alive). CONTROL first: the same ticket, unexpired, is read (in a
// savepoint that is rolled back, so it stays unconsumed).
func TestOpReadAudit_ExpiryIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	ticket := opForgeLog(t, ctx, tx, session, a.id, "", 1, 50, opCommittedXact(t, ctx))
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
		_, e := opReadLog(t, ctx, sp, hash, ticket, "", 1, 50)
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return e
	}
	if err := readThenUndo(); err != nil {
		t.Fatalf("CONTROL: the unexpired ticket was refused: %v", err)
	}
	expireIn("-1 second")
	opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, opAuditReadRefusal, "an expired ticket", ticket)

	expireIn("1 second")
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, opAuditReadRefusal,
			"savepoint "+strconv.Itoa(i+1)+" after the ticket expired in the open transaction", ticket)
	}

	expireIn("1 second")
	var result string
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('tappa_test.session', $1, true), set_config('tappa_test.ticket', $2, true)`, hash, ticket); err != nil {
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
			            PERFORM * FROM public.op_read_audit(current_setting('tappa_test.session'),
			                                                current_setting('tappa_test.ticket'), '', 1, 50);
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

// opLeads reports whether every row before `own` in rows is dated after it (or at the same
// time with a greater id) -- the newest-first order with the viewer's row where it belongs.
// It is the form of "the viewer's own row is first" that holds on a database other sessions
// write to: a row committed between the two phases may precede it, and only such a row.
func opLeads(rows []OperatorAuditEntry, own uuid.UUID) (bool, int) {
	for i, r := range rows {
		if r.ID == own {
			for _, p := range rows[:i] {
				if p.At.Before(r.At) || (p.At.Equal(r.At) && p.ID.String() < r.ID.String()) {
					return false, i
				}
			}
			return true, i
		}
	}
	return false, -1
}

// TestOpReadAudit_TwoPhaseLifecycle is ADR 0021 §2 v 3's table on the shipped read with REAL
// commits: the accessor's phase one (beginOperatorRead) is committed and its 'read' row --
// scope 'operator_audit', detail {"filter": "read"} -- is permanent; another session and
// another filter are refused; the accessor's phase two (readOperatorAudit) returns the page --
// 'read' rows only, the viewer's own row among them with every row before it dated later; the
// rolled-back read leaves the row and -- ADR 0021 limit 4 -- lets the same ticket read again;
// the committed read consumes the ticket, after which it is refused; and through all of it the
// read has exactly ONE 'read' row.
func TestOpReadAudit_TwoPhaseLifecycle(t *testing.T) {
	ctx, f := opLiveFixture(t)
	otherHash, otherSession := f.newSession(t, ctx)
	conn := f.connect(t, ctx)
	q := OperatorAuditQuery{Kind: OperatorAuditRead, Number: 1, Size: 50}

	tx := asOperatorTx(t, ctx, conn)
	ticket, err := beginOperatorRead(ctx, tx, f.hash, operatorAuditReadKind, []byte(opLogParams(string(q.Kind), 1, 50)))
	if err != nil {
		t.Fatalf("phase one: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit phase one: %v", err)
	}
	var own uuid.UUID
	scoped := func() int64 {
		return opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1
		                                 AND target_scope = 'operator_audit' AND detail = '{"filter": "read"}'::jsonb`, f.session)
	}
	if n := scoped(); n != 1 {
		t.Fatalf("after the committed phase one: %d 'operator_audit' row(s) with the filter, want 1", n)
	}
	if err := f.owner.QueryRow(ctx, `SELECT id FROM operator_audit_log WHERE kind = 'read' AND session_id = $1 AND target_scope = 'operator_audit'`, f.session).Scan(&own); err != nil {
		t.Fatal(err)
	}
	refused := func(what, hash, kind string) {
		t.Helper()
		rtx := asOperatorTx(t, ctx, conn)
		_, e := opScanLog(ctx, rtx, hash, ticket.reveal(), kind, 1, 50)
		opWantClean(t, e, sqlstateInvalidAuthorization, opAuditReadRefusal, what)
		if err := rtx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refused("the ticket from another session of the same operator", otherHash, "read")
	refused("the ticket with another filter", f.hash, "")

	read := func(commit bool) ([]OperatorAuditEntry, error) {
		t.Helper()
		rtx := asOperatorTx(t, ctx, conn)
		rows, rerr := readOperatorAudit(ctx, rtx, f.hash, ticket, q)
		if commit {
			if err := rtx.Commit(ctx); err != nil {
				t.Fatalf("commit the read: %v", err)
			}
		} else if err := rtx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return rows, rerr
	}
	rows, err := read(false)
	if err != nil {
		t.Fatalf("the read (rolled back): %v", err)
	}
	for _, r := range rows {
		if r.Kind != "read" {
			t.Errorf("the 'read' filter returned a %q row", r.Kind)
		}
	}
	if ok, i := opLeads(rows, own); !ok {
		t.Errorf("the viewer's own row is at %d of %d and a row before it is dated earlier (or it is missing)", i, len(rows))
	}
	if n := scoped(); n != 1 {
		t.Errorf("after the rolled-back read: %d row(s), want the 1 committed by phase one", n)
	}
	// ADR 0021 limit 4, measured: the rolled-back read un-consumed the ticket.
	if _, err := read(true); err != nil {
		t.Errorf("the committed read after a rolled-back one: %v", err)
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, f.session); n != 1 {
		t.Errorf("after the committed read %d ticket(s) of the session are consumed, want 1", n)
	}
	refused("the ticket after a COMMITTED read consumed it", f.hash, "read")
	if n := scoped(); n != 1 {
		t.Errorf("at the end: %d 'read' row(s), want exactly 1", n)
	}
	if n := f.liveReads(t, ctx, otherSession); n != 0 {
		t.Errorf("the other session has %d 'read' row(s)", n)
	}
}

// TestOperatorAudit_OnThePoolTheTwoPhasesAreTwoTransactions: on a pool built by the production
// constructor (*pgxpool.Pool -- what *OperatorDB holds) OperatorAudit's two phases are two
// transactions: the first page carries the viewer's own committed row with every row before it
// dated later, read out as the filter it was ("all"); a 'read' filter returns 'read' rows only.
// Inside one transaction it is ErrOperatorRefused; an unknown session is ErrOperatorRefused; a
// filter outside the set is ErrOperatorAuditFilterRefused and page 1001 a 22023 database
// error -- none of the refused calls writes a row. It commits two 'read' rows.
func TestOperatorAudit_OnThePoolTheTwoPhasesAreTwoTransactions(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("open the operator pool: %v", err)
	}
	defer o.Close()

	rows, err := OperatorAudit(ctx, o.pool, f.hash, OperatorAuditQuery{Number: 1, Size: 50})
	if err != nil {
		t.Fatalf("OperatorAudit on the pool: %v", err)
	}
	var own uuid.UUID
	if err := f.owner.QueryRow(ctx, `SELECT id FROM operator_audit_log WHERE kind = 'read' AND session_id = $1 AND target_scope = 'operator_audit'
	                                 AND detail = '{"filter": "all"}'::jsonb`, f.session).Scan(&own); err != nil {
		t.Fatalf("the committed 'read' row: %v", err)
	}
	ok, i := opLeads(rows, own)
	if !ok {
		t.Fatalf("the viewer's own row is at %d of %d and a row before it is dated earlier (or it is missing)", i, len(rows))
	}
	if r := rows[i]; opDeref(r.FilterKind) != "all" || opDeref(r.Scope) != operatorAuditReadKind || !r.DetailRecognised || r.Kind != "read" {
		t.Errorf("the viewer's row reads kind=%s scope=%s filter=%s recognised=%v", r.Kind, opDeref(r.Scope), opDeref(r.FilterKind), r.DetailRecognised)
	}
	rows, err = OperatorAudit(ctx, o.pool, f.hash, OperatorAuditQuery{Kind: OperatorAuditRead, Number: 1, Size: 50})
	if err != nil || len(rows) == 0 {
		t.Fatalf("OperatorAudit 'read' on the pool: %d rows, err %v", len(rows), err)
	}
	for _, r := range rows {
		if r.Kind != "read" {
			t.Errorf("the 'read' filter returned a %q row", r.Kind)
		}
	}
	if n := f.liveReads(t, ctx, f.session); n != 2 {
		t.Errorf("the session has %d committed 'read' row(s), want 2", n)
	}

	tx := asOperatorTx(t, ctx, f.connect(t, ctx))
	if _, err := OperatorAudit(ctx, tx, f.hash, OperatorAuditQuery{Number: 1, Size: 50}); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("OperatorAudit inside ONE transaction: %v, want ErrOperatorRefused", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := OperatorAudit(ctx, o.pool, opRandHex(t), OperatorAuditQuery{Number: 1, Size: 50}); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("OperatorAudit with an unknown session: %v, want ErrOperatorRefused", err)
	}
	if _, err := OperatorAudit(ctx, o.pool, f.hash, OperatorAuditQuery{Kind: "zz_not_a_kind", Number: 1, Size: 50}); !errors.Is(err, ErrOperatorAuditFilterRefused) {
		t.Errorf("OperatorAudit with a filter outside the set: %v, want ErrOperatorAuditFilterRefused", err)
	}
	_, err = OperatorAudit(ctx, o.pool, f.hash, OperatorAuditQuery{Number: MaxOperatorAuditPage + 1, Size: 50})
	if err == nil || errors.Is(err, ErrOperatorRefused) || !strings.Contains(err.Error(), "SQLSTATE 22023") {
		t.Errorf("OperatorAudit page %d: %v, want a 22023 database error", MaxOperatorAuditPage+1, err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 2 {
		t.Errorf("after the refused calls the session has %d committed 'read' row(s), want still 2", n)
	}
}
