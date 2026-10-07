package db

// operatorowneraudit_test.go -- migration 00033 (M10 OP-14 D, K14-2): the platform owner's
// three opadmin actions each leave one operator_audit_log row, and a row's `at` is the wall
// clock whoever writes it. The catalogue half (the two CHECKs, the trigger and its function,
// the replaced functions, the Down, the precondition) and the behaviour half (the trigger, the
// third arm of actor_shape, the read of the owner's rows). The rows opadmin's own SQL writes
// are measured in cmd/opadmin (TestAudit_EachActionLeavesExactlyOneRowAndARefusalNone).
//
// HOW EACH TEST TOUCHES THE SHARED DATABASE: every test runs in opTx -- one rolled-back
// REPEATABLE READ transaction as the owner, identities switched with SET LOCAL SESSION
// AUTHORIZATION, the operator-tables lock taken EXCLUSIVE. Every row, every disabled trigger
// and every Down/Up run here goes with the transaction. 🔴 NO TEST HERE COMMITS AN OWNER ROW:
// once one exists, 00033's Down returns the audit CHECKs NOT VALID (its Down test measures it)
// and the earlier migrations' Down tests would meet that state -- opAtVersion removes such
// rows inside its transaction for the same reason.

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	op00033File = "00033_audit_opadmin_actions.sql"

	// op00033Trigger and op00033Function are the trigger 00033 creates and the function it
	// runs; op00033TriggerDef is pg_get_triggerdef's text of the trigger.
	op00033Trigger    = "operator_audit_log_at_is_the_wall_clock"
	op00033Function   = "tappa_audit_at_is_the_wall_clock"
	op00033TriggerDef = "CREATE TRIGGER operator_audit_log_at_is_the_wall_clock BEFORE INSERT ON public.operator_audit_log " +
		"FOR EACH ROW EXECUTE FUNCTION tappa_audit_at_is_the_wall_clock()"
	// op00033FunctionBody is the function's whole body, white space folded: it overwrites
	// NEW.at -- always, not only a missing one -- and returns the row.
	op00033FunctionBody = "BEGIN NEW.at := pg_catalog.clock_timestamp(); RETURN NEW; END;"
)

// opActorShapeDef33 is pg_get_constraintdef's text of operator_audit_log_actor_shape at 00033:
// the pre-session arm (pre), the owner arm (owner: no session, no actor, an account and
// nothing else) and the session arm, which names both lists as the kinds it is not for.
func opActorShapeDef33(pre, owner []string) string {
	arr := func(kinds []string) string {
		q := make([]string, len(kinds))
		for i, k := range kinds {
			q[i] = "'" + k + "'::text"
		}
		return "ARRAY[" + strings.Join(q, ", ") + "]"
	}
	both := append(slices.Clone(pre), owner...)
	return "CHECK ((((kind = ANY (" + arr(pre) + ")) AND (session_id IS NULL) AND (actor_admin_id IS NULL)) OR " +
		"((kind = ANY (" + arr(owner) + ")) AND (session_id IS NULL) AND (actor_admin_id IS NULL) AND " +
		"(target_admin_id IS NOT NULL) AND (target_tenant_id IS NULL) AND (target_scope IS NULL) AND " +
		"(page_number IS NULL) AND (page_size IS NULL) AND (detail = '{}'::jsonb)) OR " +
		"((kind <> ALL (" + arr(both) + ")) AND (session_id IS NOT NULL) AND (actor_admin_id IS NOT NULL))))"
}

// op00033ListEdits are the two list edits 00033 makes to 00032's bodies, as compose-time text:
// op_begin_read's filter list and op_read_audit's filter shape. Undoing them must give 00032's
// body back byte for byte.
var op00033ListEdits = map[string][2]string{
	"op_begin_read": {
		"'logout', 'read', 'legal_publish') THEN\n",
		"'logout', 'read', 'legal_publish', 'operator_created',\n" +
			"                                                   'operator_mfa_reset', 'operator_disabled') THEN\n",
	},
	"op_read_audit": {
		"'logout', 'read', 'legal_publish')\n",
		"'logout', 'read', 'legal_publish',\n" +
			"                                                            'operator_created', 'operator_mfa_reset',\n" +
			"                                                            'operator_disabled')\n",
	},
}

// opStatementsOnly is a migration section without its comment lines.
func opStatementsOnly(section string) string {
	var b strings.Builder
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// opClock is the database's wall clock, read now.
func opClock(t *testing.T, ctx context.Context, q opQuerier) time.Time {
	t.Helper()
	var now time.Time
	if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now
}

// --------------------------------------------------------------- catalogue --

// TestOperator00033_TheKindsTheShapeAndTheClock pins 00033 at HEAD on the catalogue:
//   - operator_audit_log_kind_check is exactly the fourteen kinds (00031's eleven and the three
//     owner kinds) and operator_audit_log_actor_shape exactly the three arms -- the owner arm
//     an account and nothing else -- both validated;
//   - the trigger: on operator_audit_log, BEFORE INSERT FOR EACH ROW, enabled, running
//     tappa_audit_at_is_the_wall_clock(); the table's triggers are exactly it and 00026's two;
//     the function belongs to the table's owner, is SECURITY INVOKER and VOLATILE, takes no
//     argument, returns trigger, pins the schema's trigger search_path, and its body is exactly
//     "NEW.at := pg_catalog.clock_timestamp(); RETURN NEW;" -- no condition, no frozen clock;
//   - op_begin_read and op_read_audit keep their identities, one overload each, EXECUTE for
//     tappa_operator alone; their live bodies are 00033's, and 00033's are 00032's but for the
//     two lists (undoing the edits gives 00032's byte for byte); op_record_auth_event is
//     00031's body, untouched;
//   - no function tappa_opdefiner owns names an owner kind except those two lists -- no op_*
//     writes an owner row; the forward, frozen-clock and consumption scans and the definer's
//     foreign EXECUTE scan raise nothing; the definer's INSERT list on the log still lacks `at`.
func TestOperator00033_TheKindsTheShapeAndTheClock(t *testing.T) {
	ctx, tx := opTx(t)
	for _, c := range []struct{ name, want string }{
		{"operator_audit_log_kind_check", opKindCheckDef(opAuditKinds)},
		{"operator_audit_log_actor_shape", opActorShapeDef33(opAuthEventKinds, opOwnerKinds)},
	} {
		def, valid := opConstraint(t, ctx, tx, "operator_audit_log", c.name)
		if def != c.want || !valid {
			t.Errorf("%s is %s (validated %v),\n want %s, validated", c.name, def, valid, c.want)
		}
	}

	// The trigger and its function.
	rows, err := tx.Query(ctx, `SELECT t.tgname, t.tgenabled::text, pg_get_triggerdef(t.oid)
	                              FROM pg_trigger t
	                             WHERE t.tgrelid = 'public.operator_audit_log'::regclass AND NOT t.tgisinternal
	                             ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	type trg struct{ name, enabled, def string }
	triggers, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (trg, error) {
		var x trg
		return x, r.Scan(&x.name, &x.enabled, &x.def)
	})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range triggers {
		names = append(names, x.name)
		if x.name == op00033Trigger && (x.enabled != "O" || x.def != op00033TriggerDef) {
			t.Errorf("the trigger is enabled=%q %s,\n want enabled=O %s", x.enabled, x.def, op00033TriggerDef)
		}
	}
	if want := []string{"operator_audit_log_append_only", op00033Trigger, "operator_audit_log_no_truncate"}; !slices.Equal(names, want) {
		t.Errorf("operator_audit_log's triggers are %v, want %v", names, want)
	}
	var owner, tableOwner, body, ret, vol string
	var secdef bool
	var nargs int
	var config []string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_userbyid(p.proowner), (SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid = 'public.operator_audit_log'::regclass),
		       p.prosrc, p.prorettype::regtype::text, p.provolatile::text, p.prosecdef, p.pronargs, p.proconfig
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.proname = $1`, op00033Function).
		Scan(&owner, &tableOwner, &body, &ret, &vol, &secdef, &nargs, &config); err != nil {
		t.Fatalf("%s: %v", op00033Function, err)
	}
	if owner != tableOwner || secdef || vol != "v" || nargs != 0 || ret != "trigger" ||
		len(config) != 1 || config[0] != "search_path=pg_catalog, pg_temp" {
		t.Errorf("%s: owner %s (the table's %s), SECURITY DEFINER %v, volatility %s, %d args, returns %s, proconfig %v",
			op00033Function, owner, tableOwner, secdef, vol, nargs, ret, config)
	}
	if got := strings.Join(strings.Fields(body), " "); got != op00033FunctionBody {
		t.Errorf("the trigger function's body is %q,\n want %q", got, op00033FunctionBody)
	}
	var frozen bool
	if err := tx.QueryRow(ctx, `SELECT $1::text ~ $2::text`, body, opFrozenClockRE).Scan(&frozen); err != nil || frozen {
		t.Errorf("the trigger function's body names a frozen clock (%v)", err)
	}

	// The two replaced functions, and the one 00033 does not touch.
	up, _ := opMigrationSections(t, op00033File)
	up32, _ := opMigrationSections(t, op00032File)
	up31, _ := opMigrationSections(t, op00031File)
	for _, fn := range []string{"op_begin_read", "op_read_audit"} {
		b33 := opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", fn)
		b32 := opLogFunctionBody(t, up32, "CREATE OR REPLACE FUNCTION", fn)
		edit := op00033ListEdits[fn]
		if strings.Count(b33, edit[1]) != 1 || strings.Replace(b33, edit[1], edit[0], 1) != b32 {
			t.Errorf("00033's %s is not 00032's body with its one list widened by the three owner kinds", fn)
		}
		var live string
		if err := tx.QueryRow(ctx, `SELECT p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, fn).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != b33 {
			t.Errorf("the live %s is not 00033's body", fn)
		}
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
	if got, want := opIdentity(t, ctx, tx, "op_read_audit"), op00031Read.args+" -> "+op00031Read.result; got != want {
		t.Errorf("op_read_audit is now %s; the replacement keeps 00031's identity %s", got, want)
	}
	var record string
	if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = 'public.op_record_auth_event(text, text, uuid)'::regprocedure`).Scan(&record); err != nil {
		t.Fatal(err)
	}
	if record != opLogFunctionBody(t, up31, "CREATE OR REPLACE FUNCTION", "op_record_auth_event") {
		t.Error("op_record_auth_event is not 00031's body; 00033 does not touch the pre-session writer")
	}

	// No op_* writes an owner row: the owner kinds appear in the definer's functions only as
	// the two filter lists.
	qr, err := tx.Query(ctx, `SELECT proname FROM pg_proc WHERE proowner = 'tappa_opdefiner'::regrole
	                            AND prosrc ~ 'operator_(created|mfa_reset|disabled)' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	naming, err := pgx.CollectRows(qr, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"op_begin_read", "op_read_audit"}; !slices.Equal(naming, want) {
		t.Errorf("the definer's functions naming an owner kind are %v, want %v (the two filter lists)", naming, want)
	}
	if got := opColumns(t, ctx, tx, "tappa_opdefiner", "operator_audit_log", "INSERT"); got !=
		"kind,session_id,actor_admin_id,target_admin_id,target_tenant_id,target_scope,page_number,page_size,detail" {
		t.Errorf("the definer's INSERT on the log is (%s); 00033 grants nothing", got)
	}

	findings, names2, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for _, fn := range []string{"op_begin_read", "op_read_audit"} {
		if !slices.Contains(names2, fn) {
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
	consumption, _, err := opReadConsumptionFindings(ctx, tx)
	if err != nil {
		t.Fatalf("consumption scan: %v", err)
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
}

// ---------------------------------------------------------------- the clock --

// TestOperator00033_TheRowsTimeIsTheWallClockWhoeverWritesIt measures the trigger, in a
// transaction that has slept 1.5 s first (so the transaction's start and the wall clock are
// told apart):
//   - every path the OWNER has -- an INSERT dated 1999, one dated 2999, an INSERT ... SELECT of
//     three rows dated in the future, a COPY dated 1999, an INSERT naming no `at` -- leaves rows
//     dated inside the statement, by the database's wall clock (read before and after), at least
//     1.4 s after the transaction's start; every other column is as written;
//   - a DEFINER's rows are unchanged in meaning: op_record_auth_event's and op_begin_read's rows
//     are dated inside their call, with the trigger and -- in a savepoint where it is disabled,
//     i.e. by the column's DEFAULT alone -- without it;
//   - an UPDATE of `at` is refused by the append-only trigger (23001): this one fires on INSERT
//     alone;
//   - THE COUNTED LIMIT (ADR 0021 limit 5), measured: with the trigger DISABLED -- the table
//     owner's statement (dropping it or replacing its function are the owner's other two paths;
//     TestOperator00033_TheClockFunctionRunsOnlyAsItsTrigger takes the last) -- the owner's 2999
//     stands;
//   - ADR 0021 limit L2 closed: a row the owner dates in the future no longer heads the read --
//     the viewer's own 'read' row, written after it, comes first.
func TestOperator00033_TheRowsTimeIsTheWallClockWhoeverWritesIt(t *testing.T) {
	ctx, tx := opTx(t)
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE at > clock_timestamp()`); n != 0 {
		t.Fatalf("PREMISE: %d audit row(s) are dated in the future", n)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatal(err)
	}
	inside := func(what string, before, after time.Time, ats []time.Time) {
		t.Helper()
		if len(ats) == 0 {
			t.Errorf("%s: no row was written", what)
		}
		for _, at := range ats {
			var sinceStart float64
			if err := tx.QueryRow(ctx, `SELECT extract(epoch FROM ($1::timestamptz - transaction_timestamp()))::float8`, at).Scan(&sinceStart); err != nil {
				t.Fatal(err)
			}
			if at.Before(before) || at.After(after) || sinceStart < 1.4 {
				t.Errorf("%s: a row is dated %v, %.3f s after the transaction's start; want inside [%v, %v] and >= 1.4 s",
					what, at, sinceStart, before, after)
			}
		}
	}
	atsOf := func(target uuid.UUID) []time.Time {
		t.Helper()
		qr, err := tx.Query(ctx, `SELECT at FROM operator_audit_log WHERE target_admin_id = $1 ORDER BY at`, target)
		if err != nil {
			t.Fatal(err)
		}
		ats, err := pgx.CollectRows(qr, pgx.RowTo[time.Time])
		if err != nil {
			t.Fatal(err)
		}
		return ats
	}
	for _, c := range []struct {
		what  string
		write func(target uuid.UUID) error
	}{
		{"the owner's INSERT dated 1999", func(id uuid.UUID) error {
			_, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (at, kind, target_admin_id) VALUES ('1999-01-01 00:00:00+00', 'operator_created', $1)`, id)
			return err
		}},
		{"the owner's INSERT dated 2999", func(id uuid.UUID) error {
			_, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (at, kind, target_admin_id) VALUES ('2999-01-01 00:00:00+00', 'operator_disabled', $1)`, id)
			return err
		}},
		{"the owner's INSERT ... SELECT of three future rows", func(id uuid.UUID) error {
			_, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (at, kind, target_admin_id)
			                        SELECT '2999-01-01 00:00:00+00'::timestamptz + make_interval(days => g), 'locked', $1
			                          FROM generate_series(1, 3) AS g`, id)
			return err
		}},
		{"the owner's COPY dated 1999", func(id uuid.UUID) error {
			_, err := tx.Conn().PgConn().CopyFrom(ctx, strings.NewReader("1999-01-01 00:00:00+00\toperator_mfa_reset\t"+id.String()+"\n"),
				`COPY public.operator_audit_log (at, kind, target_admin_id) FROM STDIN`)
			return err
		}},
		{"the owner's INSERT naming no at", func(id uuid.UUID) error {
			_, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('operator_created', $1)`, id)
			return err
		}},
		{"op_record_auth_event (the definer)", func(id uuid.UUID) error {
			return opRecord(t, ctx, tx, "login_failed", nil, id)
		}},
	} {
		a := opNewActive(t, ctx, tx)
		before := opClock(t, ctx, tx)
		if err := c.write(a.id); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		after := opClock(t, ctx, tx)
		inside(c.what, before, after, atsOf(a.id))
	}
	// Every other column is as written: the COPY's row, read back.
	b := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (at, kind, target_admin_id) VALUES ('1999-01-01 00:00:00+00', 'operator_created', $1)`, b.id); err != nil {
		t.Fatal(err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE target_admin_id = $1 AND kind = 'operator_created'
	                             AND session_id IS NULL AND actor_admin_id IS NULL AND detail = '{}'::jsonb`, b.id); n != 1 {
		t.Errorf("the owner's row read back %d time(s) as written (kind, account, no session, no actor, detail {}), want 1", n)
	}

	// The definer's read row, with the trigger and by the DEFAULT alone.
	v := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, v.id, true)
	readRow := func(q pgx.Tx, what string) {
		t.Helper()
		n0 := opInt(t, ctx, q, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1`, session)
		before := opClock(t, ctx, q)
		if err := opAs(t, ctx, q, "tappa_operator", func(sp pgx.Tx) error {
			var raw string
			return sp.QueryRow(ctx, beginOperatorReadSQL, hash, operatorAuditReadKind, opLogParams("", 1, 5)).Scan(&raw)
		}); err != nil {
			t.Fatalf("%s: op_begin_read: %v", what, err)
		}
		after := opClock(t, ctx, q)
		qr, err := q.Query(ctx, `SELECT at FROM operator_audit_log WHERE session_id = $1 ORDER BY at`, session)
		if err != nil {
			t.Fatal(err)
		}
		ats, err := pgx.CollectRows(qr, pgx.RowTo[time.Time])
		if err != nil || int64(len(ats)) != n0+1 {
			t.Fatalf("%s: %d read row(s) (err %v), want %d", what, len(ats), err, n0+1)
		}
		inside(what, before, after, ats[len(ats)-1:])
	}
	readRow(tx, "op_begin_read's row (the definer), with the trigger")
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The lock a DISABLE TRIGGER takes, measured (the condition opOwnerDatesRows keeps): the
	// modes this session holds on the table before and after it, and another session's INSERT
	// waiting on it until its own lock_timeout (that session's transaction is rolled back).
	opMustHoldTheTablesLockExclusive(t, ctx, sp)
	modes := func() []string {
		t.Helper()
		qr, err := sp.Query(ctx, `SELECT mode FROM pg_locks WHERE locktype = 'relation' AND granted
		                            AND relation = 'public.operator_audit_log'::regclass AND pid = pg_backend_pid() ORDER BY mode`)
		if err != nil {
			t.Fatal(err)
		}
		m, err := pgx.CollectRows(qr, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	held := modes()
	if _, err := sp.Exec(ctx, `ALTER TABLE operator_audit_log DISABLE TRIGGER `+op00033Trigger); err != nil {
		t.Fatal(err)
	}
	var taken []string
	for _, m := range modes() {
		if !slices.Contains(held, m) {
			taken = append(taken, m)
		}
	}
	t.Logf("OP14D-LOCK: DISABLE TRIGGER took %v on operator_audit_log (held before: %v)", taken, held)
	if !slices.Equal(taken, []string{"ShareRowExclusiveLock"}) {
		t.Errorf("DISABLE TRIGGER took %v on operator_audit_log; measured (PostgreSQL 17) was ShareRowExclusiveLock", taken)
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
	if _, err := otx.Exec(ctx, `SET LOCAL lock_timeout = '300ms'`); err != nil {
		t.Fatal(err)
	}
	_, blocked := otx.Exec(ctx, `INSERT INTO public.operator_audit_log (kind) VALUES ('unknown_email')`)
	if err := otx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	opWant(t, blocked, sqlstateLockNotAvailable, "another session's INSERT while the trigger is disabled in this transaction")
	readRow(sp, "op_begin_read's row (the definer), by the DEFAULT alone")
	// THE COUNTED LIMIT: with the trigger disabled the owner's 2999 stands.
	var standing time.Time
	if err := sp.QueryRow(ctx, `INSERT INTO operator_audit_log (at, kind, target_admin_id) VALUES ('2999-01-01 00:00:00+00', 'operator_created', $1)
	                             RETURNING at`, b.id).Scan(&standing); err != nil {
		t.Fatal(err)
	}
	if !standing.Equal(time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("COUNTED LIMIT moved: with the trigger disabled the owner's row is dated %v, measured was 2999-01-01", standing)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// An UPDATE of `at` is the append-only trigger's.
	opWant(t, opTry(t, ctx, tx, `UPDATE operator_audit_log SET at = '2999-01-01 00:00:00+00' WHERE target_admin_id = $1`, b.id),
		sqlstateRestrictViolation, "the owner's UPDATE of at")

	// L2 closed: the owner's future-dated row does not head the read.
	if _, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (at, kind, target_admin_id) VALUES ('2999-01-01 00:00:00+00', 'operator_disabled', $1)`, b.id); err != nil {
		t.Fatal(err)
	}
	raw, err := opBegin(t, ctx, tx, hash, operatorAuditReadKind, opLogParams("", 1, 5))
	if err != nil {
		t.Fatalf("phase one: %v", err)
	}
	var own uuid.UUID
	if err := tx.QueryRow(ctx, `UPDATE operator_read_tickets SET created_xact = $1::xid8 WHERE ticket_hash = $2 RETURNING audit_id`,
		opCommittedXact(t, ctx), opLogTicketHash(raw, "", 1, 5)).Scan(&own); err != nil {
		t.Fatalf("name a committed transaction on the ticket: %v", err)
	}
	page, err := opReadLog(t, ctx, tx, hash, raw, "", 1, 5)
	if err != nil {
		t.Fatalf("op_read_audit: %v", err)
	}
	if len(page) == 0 || page[0].ID != own {
		t.Errorf("the viewer's own row is not first: a row the owner dated 2999 still heads the read")
	}
}

// TestOperator00033_TheClockFunctionRunsOnlyAsItsTrigger measures the two facts the decision to
// leave PUBLIC's EXECUTE on tappa_audit_at_is_the_wall_clock -- as on the schema's eight trigger
// functions before it -- rests on (ADR 0021's OP-14 D note, md. 4):
//   - a direct call is refused by PostgreSQL itself, whoever calls it -- the owner, tappa_app and
//     tappa_operator (EXECUTE through PUBLIC), the definer -- with SQLSTATE 0A000 "trigger
//     functions can only be called as triggers";
//   - EXECUTE is NOT checked when the trigger fires: with PUBLIC's EXECUTE revoked (in a
//     savepoint) the definer may not call the function (has_function_privilege false; a direct
//     call 42501), yet op_record_auth_event's INSERT -- which runs as the definer -- fires it: a
//     body replaced in the same savepoint by one that stamps a marker date leaves that date on
//     the row. CONTROL: after the savepoint, the same write is dated by the clock.
func TestOperator00033_TheClockFunctionRunsOnlyAsItsTrigger(t *testing.T) {
	ctx, tx := opTx(t)
	const call = `SELECT public.tappa_audit_at_is_the_wall_clock()`
	const refusal = "trigger functions can only be called as triggers"
	if code, msg := opCode(opTry(t, ctx, tx, call)); code != "0A000" || msg != refusal {
		t.Errorf("the owner's direct call: %s %q, want 0A000 %q", code, msg, refusal)
	}
	for _, role := range []string{"tappa_app", "tappa_operator", "tappa_opdefiner"} {
		if code, msg := opCode(opExecAs(t, ctx, tx, role, call)); code != "0A000" || msg != refusal {
			t.Errorf("%s's direct call: %s %q, want 0A000 %q", role, code, msg, refusal)
		}
	}

	a := opNewActive(t, ctx, tx)
	atOf := func(q pgx.Tx) []time.Time {
		t.Helper()
		qr, err := q.Query(ctx, `SELECT at FROM operator_audit_log WHERE target_admin_id = $1 ORDER BY at`, a.id)
		if err != nil {
			t.Fatal(err)
		}
		ats, err := pgx.CollectRows(qr, pgx.RowTo[time.Time])
		if err != nil {
			t.Fatal(err)
		}
		return ats
	}
	marker := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`CREATE OR REPLACE FUNCTION public.tappa_audit_at_is_the_wall_clock()
		     RETURNS trigger LANGUAGE plpgsql VOLATILE SET search_path = pg_catalog, pg_temp
		 AS $$ BEGIN NEW.at := '2001-02-03 04:05:06+00'; RETURN NEW; END; $$`,
		`REVOKE EXECUTE ON FUNCTION public.tappa_audit_at_is_the_wall_clock() FROM PUBLIC`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("savepoint setup: %v", err)
		}
	}
	for _, role := range []string{"tappa_opdefiner", "tappa_operator", "tappa_app"} {
		var may bool
		if err := sp.QueryRow(ctx, `SELECT has_function_privilege($1, 'public.tappa_audit_at_is_the_wall_clock()', 'EXECUTE')`, role).Scan(&may); err != nil || may {
			t.Fatalf("PREMISE: with PUBLIC's EXECUTE revoked, %s may EXECUTE the function (%v, err %v)", role, may, err)
		}
	}
	opWant(t, opExecAs(t, ctx, sp, "tappa_opdefiner", call), sqlstateInsufficientPrivi, "the definer's direct call without EXECUTE")
	if err := opRecord(t, ctx, sp, "login_failed", nil, a.id); err != nil {
		t.Fatalf("op_record_auth_event (the definer's INSERT) with the function's EXECUTE revoked: %v", err)
	}
	if ats := atOf(sp); len(ats) != 1 || !ats[0].Equal(marker) {
		t.Errorf("the definer's row is dated %v; want the marker %v -- the trigger fired although the definer may not EXECUTE its function", ats, marker)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	before := opClock(t, ctx, tx)
	if err := opRecord(t, ctx, tx, "login_failed", nil, a.id); err != nil {
		t.Fatal(err)
	}
	after := opClock(t, ctx, tx)
	if ats := atOf(tx); len(ats) != 1 || ats[0].Before(before) || ats[0].After(after) {
		t.Errorf("CONTROL: after the savepoint the row is dated %v, want inside [%v, %v]", ats, before, after)
	}
}

// ------------------------------------------------------------ the owner arm --

// TestOperator00033_TheOwnerArmIsExactlyAnAccountAndNothingElse drives actor_shape's third arm
// and the kind CHECK as statements, as the owner (the CHECK binds the owner too):
//   - an owner row -- each of the three kinds -- with the account and nothing else is written
//     (the CONTROL);
//   - with a session and an actor, an actor alone, a session alone, no account, a tenant, a
//     scope, a page number, a page size, a detail naming an address or a name, it is refused,
//     each by actor_shape (23514, the constraint named);
//   - a kind outside the fourteen ('operator_enabled', the right kind in another case or with a
//     trailing space), written with a session so that actor_shape lets it through, is refused by
//     the kind CHECK;
//   - the other two arms are as 00031 left them: a pre-session row without a session passes and
//     with one is refused; a session row with its session passes and without is refused;
//   - WHO may write one: tappa_operator and tappa_app are refused (42501); op_record_auth_event,
//     the one op_* that takes a kind, refuses each owner kind (22023, no row); and -- COUNTED,
//     measured -- tappa_opdefiner itself holds the two INSERT columns an owner row needs, so a
//     statement run AS the definer writes one: only an op_* body runs as the definer, and none
//     names an owner kind outside the two filter lists (TestOperator00033_TheKindsTheShapeAndTheClock).
func TestOperator00033_TheOwnerArmIsExactlyAnAccountAndNothingElse(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	_, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := opNewTenant(t, ctx, tx, "op14d arm "+opToken(t), 0)
	refusedBy := func(what, constraint, sql string, args ...any) {
		t.Helper()
		err := opTry(t, ctx, tx, sql, args...)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != sqlstateCheckViolation || pg.ConstraintName != constraint {
			t.Errorf("%s: %v, want 23514 from %s", what, err, constraint)
		}
	}
	for _, kind := range opOwnerKinds {
		if err := opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ($1, $2)`, kind, a.id); err != nil {
			t.Errorf("CONTROL: an owner row %s naming the account and nothing else: %v", kind, err)
		}
		const shape = "operator_audit_log_actor_shape"
		for _, c := range []struct {
			what, sql string
			args      []any
		}{
			{"with a session and an actor", `INSERT INTO operator_audit_log (kind, target_admin_id, session_id, actor_admin_id) VALUES ($1, $2, $3, $2)`, []any{kind, a.id, session}},
			{"with an actor alone", `INSERT INTO operator_audit_log (kind, target_admin_id, actor_admin_id) VALUES ($1, $2, $2)`, []any{kind, a.id}},
			{"with a session alone", `INSERT INTO operator_audit_log (kind, target_admin_id, session_id) VALUES ($1, $2, $3)`, []any{kind, a.id, session}},
			{"with no account", `INSERT INTO operator_audit_log (kind) VALUES ($1)`, []any{kind}},
			{"with a tenant", `INSERT INTO operator_audit_log (kind, target_admin_id, target_tenant_id) VALUES ($1, $2, $3)`, []any{kind, a.id, tenant}},
			{"with a scope", `INSERT INTO operator_audit_log (kind, target_admin_id, target_scope) VALUES ($1, $2, 'tenants')`, []any{kind, a.id}},
			{"with a page number", `INSERT INTO operator_audit_log (kind, target_admin_id, page_number) VALUES ($1, $2, 1)`, []any{kind, a.id}},
			{"with a page size", `INSERT INTO operator_audit_log (kind, target_admin_id, page_size) VALUES ($1, $2, 50)`, []any{kind, a.id}},
			{"with an address in detail", `INSERT INTO operator_audit_log (kind, target_admin_id, detail) VALUES ($1, $2, jsonb_build_object('email', $3::text))`, []any{kind, a.id, a.email}},
			{"with a name in detail", `INSERT INTO operator_audit_log (kind, target_admin_id, detail) VALUES ($1, $2, '{"name": "Op Fourteen D"}')`, []any{kind, a.id}},
		} {
			refusedBy(kind+" "+c.what, shape, c.sql, c.args...)
		}
	}
	// THE ORDER THE CHECKs RUN IN, measured (README's actor_shape refusal of a script on a
	// database at 00032 rests on it): a row that breaks actor_shape AND the kind CHECK is reported
	// by actor_shape; one that breaks the kind CHECK AND target_scope's by the kind CHECK -- name
	// order.
	refusedBy("a row breaking actor_shape and the kind CHECK", "operator_audit_log_actor_shape",
		`INSERT INTO operator_audit_log (kind) VALUES ('zz_not_a_kind')`)
	refusedBy("a row breaking the kind CHECK and target_scope's", "operator_audit_log_kind_check",
		`INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_scope) VALUES ('zz_not_a_kind', $1, $2, 'Not A Scope')`,
		session, a.id)
	// A kind outside the fourteen, written with a session and an actor so that actor_shape -- the
	// CHECKs run in name order, actor_shape first -- lets it through to the kind CHECK.
	for _, kind := range []string{"operator_enabled", "Operator_Created", "operator_created "} {
		refusedBy("the kind "+kind, "operator_audit_log_kind_check",
			`INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ($1, $2, $3)`, kind, session, a.id)
	}
	if err := opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('login_failed', $1)`, a.id); err != nil {
		t.Errorf("CONTROL: a pre-session row: %v", err)
	}
	refusedBy("a pre-session row with a session", "operator_audit_log_actor_shape",
		`INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ('login_failed', $1, $2)`, session, a.id)
	if err := opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ('logout', $1, $2)`, session, a.id); err != nil {
		t.Errorf("CONTROL: a session row: %v", err)
	}
	refusedBy("a session row without its session", "operator_audit_log_actor_shape",
		`INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('logout', $1)`, a.id)

	for _, kind := range opOwnerKinds {
		for _, role := range []string{"tappa_operator", "tappa_app"} {
			opWant(t, opExecAs(t, ctx, tx, role, `INSERT INTO public.operator_audit_log (kind, target_admin_id) VALUES ($1, $2)`, kind, a.id),
				sqlstateInsufficientPrivi, role+" writing "+kind+" directly")
		}
		before := opAudit(t, ctx, tx)
		opWantClean(t, opRecord(t, ctx, tx, kind, nil, a.id), sqlstateInvalidParameter, recordKindRefusal31,
			"op_record_auth_event("+kind+")")
		if d := opAudit(t, ctx, tx) - before; d != 0 {
			t.Errorf("op_record_auth_event(%s) refused and left %d row(s)", kind, d)
		}
	}
	// COUNTED: the definer's own INSERT columns are enough for an owner row.
	if err := opExecAs(t, ctx, tx, "tappa_opdefiner", `INSERT INTO public.operator_audit_log (kind, target_admin_id) VALUES ('operator_created', $1)`, a.id); err != nil {
		t.Errorf("COUNTED LIMIT moved: as tappa_opdefiner an owner row is refused (%v); measured was written -- update ADR 0021's OP-14 D note", err)
	}
}

// ------------------------------------------------------------------ the read --

// TestOpReadAudit_TheOwnersRowsReadAsTheirKindAndAccount: op_read_audit reads the owner's rows
// as what they are, through the PRODUCT's scan (readOperatorAudit):
//   - each owner kind's row comes back with its kind verbatim -- a kind internal/db knows and
//     ByOwner names --, the account's id and display name, no session, no actor and no actor
//     name, no scope, no page, every value read out of detail empty, and detail_recognised TRUE
//     (the closed list's last line: scope NULL, detail {});
//   - a log read filtered to each owner kind -- {"filter": <kind>} -- is recognised with the
//     kind read out, and a read with that filter returns rows of that kind only;
//   - op_begin_read accepts each owner kind as a filter and records it.
func TestOpReadAudit_TheOwnersRowsReadAsTheirKindAndAccount(t *testing.T) {
	ctx, tx := opTx(t)
	opNoRowAtOrAfterBase(t, ctx, tx)
	viewer := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, viewer.id, true)
	target := opNewActive(t, ctx, tx)
	targetName := opNamed(t, ctx, tx, target.id)
	xact := opCommittedXact(t, ctx)

	var ids []uuid.UUID
	for i, kind := range opOwnerKinds {
		ids = append(ids, opLogInsert(t, ctx, tx, opLogRow{kind: kind, target: &target.id, after: strconv.Itoa(len(opOwnerKinds)*2-i) + " seconds"}))
	}
	var filterIDs []uuid.UUID
	for i, kind := range opOwnerKinds {
		filterIDs = append(filterIDs, opLogInsert(t, ctx, tx, opLogRow{kind: "read", session: &session, actor: &viewer.id,
			scope: operatorAuditReadKind, number: 1, size: 50, detail: `{"filter": "` + kind + `"}`,
			after: strconv.Itoa(len(opOwnerKinds)-i) + " seconds"}))
	}
	var rows []OperatorAuditEntry
	raw := opForgeLog(t, ctx, tx, session, viewer.id, "", 1, 200, xact)
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		rows, e = readOperatorAudit(ctx, sp, hash, readTicket{v: &raw}, OperatorAuditQuery{Number: 1, Size: 200})
		return e
	}); err != nil {
		t.Fatalf("readOperatorAudit: %v", err)
	}
	want := append(slices.Clone(ids), filterIDs...)
	if len(rows) < len(want) {
		t.Fatalf("the read returned %d rows, want at least %d", len(rows), len(want))
	}
	for i, id := range want {
		if rows[i].ID != id {
			t.Fatalf("row %d is not the fixture row it should be (the fixtures do not head the page in their order)", i)
		}
	}
	for i, kind := range opOwnerKinds {
		r := rows[i]
		k := OperatorAuditKind(r.Kind)
		if r.Kind != kind || !k.Known() || !k.ByOwner() || r.SessionID != nil || r.ActorID != nil || r.ActorName != nil ||
			!opSameID(r.TargetAdminID, &target.id) || opDeref(r.TargetAdminName) != targetName || r.TargetTenantID != nil ||
			r.Scope != nil || r.PageNumber != nil || r.PageSize != nil || r.SearchClass != nil || r.FilterKind != nil ||
			r.LegalSlug != nil || r.LegalBytes != nil || !r.DetailRecognised {
			t.Errorf("the %s row reads kind=%s known=%v byOwner=%v session=%v actor=%v target name=%s scope=%s recognised=%v",
				kind, r.Kind, k.Known(), k.ByOwner(), r.SessionID != nil, r.ActorID != nil, opDeref(r.TargetAdminName), opDeref(r.Scope), r.DetailRecognised)
		}
		f := rows[len(opOwnerKinds)+i]
		if !f.DetailRecognised || opDeref(f.FilterKind) != kind || opDeref(f.Scope) != operatorAuditReadKind {
			t.Errorf("a log read filtered to %s reads recognised=%v filter=%s scope=%s", kind, f.DetailRecognised, opDeref(f.FilterKind), opDeref(f.Scope))
		}
	}
	for _, kind := range opOwnerKinds {
		got, err := opReadLog(t, ctx, tx, hash, opForgeLog(t, ctx, tx, session, viewer.id, kind, 1, 50, xact), kind, 1, 50)
		if err != nil {
			t.Fatalf("op_read_audit filtered to %s: %v", kind, err)
		}
		if len(got) == 0 || got[0].Kind != kind {
			t.Errorf("the %s filter's first row is not one of its kind", kind)
		}
		for _, r := range got {
			if r.Kind != kind {
				t.Errorf("the %s filter returned a %q row", kind, r.Kind)
			}
		}
		n0 := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1 AND detail = jsonb_build_object('filter', $2::text)`, session, kind)
		if _, err := opBegin(t, ctx, tx, hash, operatorAuditReadKind, opLogParams(kind, 1, 50)); err != nil {
			t.Errorf("op_begin_read with the filter %s: %v", kind, err)
		}
		if d := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1 AND detail = jsonb_build_object('filter', $2::text)`, session, kind) - n0; d != 1 {
			t.Errorf("op_begin_read with the filter %s recorded %d row(s) naming it, want 1", kind, d)
		}
	}
}

// -------------------------------------------------------------- Down / Up --

// TestOperator00033_DownGivesBack00032AndUpTakesItAgain runs 00033's Down and Up from the
// migration file inside the test's transaction.
//   - THE TEXT: the Down's op_begin_read and op_read_audit are 00032's Up bodies VERBATIM (read
//     from 00032's file); its statements hold no REVOKE, no GRANT and no DISABLE TRIGGER, and
//     they drop the trigger and its function.
//   - THE SETS AND THE CONDITIONS, WHOLE: the Down's audit condition and its two kind CHECKs
//     name 00031's eleven (derived from 00031's file); its four actor_shape lists are 00031's
//     pre-session six; the Up's condition and kind CHECKs name the fourteen, its actor_shape
//     lists are the six, the three owner kinds and the nine (twice each). Each NOT VALID
//     condition is read to its end and compared whole.
//   - Down gives both functions 00032's bodies back (the live prosrc), drops the trigger and
//     its function -- the owner's chosen `at` stands again, measured -- and puts 00031's CHECKs
//     back; opadmin's audit INSERT is then refused by operator_audit_log_actor_shape (23514 --
//     the name a script of this change reports on a database at 00032), and an owner kind as a
//     log-read filter by op_begin_read (22023). Up takes it all again, and the owner's chosen
//     `at` is overwritten again.
//   - NOT VALID, branch by branch: nothing outside 00031's set -> VALIDATED; an owner row ->
//     both CHECKs NOT VALID (a NEW owner row is refused); a LATER migration's kind -> the Down
//     NOT VALID and the Up NOT VALID too (it composes). Up again: VALIDATED where nothing is
//     outside the fourteen.
//   - THE CHAIN COMPOSES DOWNWARD: with an owner row present, 00033's, 00032's and 00031's
//     Downs run one after another without a 23514; the kind CHECK ends as 00027's set, NOT VALID.
func TestOperator00033_DownGivesBack00032AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 33, opKindsAt32...)
	up, down := opMigrationSections(t, op00033File)
	up32, down32 := opMigrationSections(t, op00032File)
	up31, down31 := opMigrationSections(t, op00031File)
	begin32, begin33 := opLogFunctionBody(t, up32, "CREATE OR REPLACE FUNCTION", "op_begin_read"), opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_begin_read")
	audit32, audit33 := opLogFunctionBody(t, up32, "CREATE OR REPLACE FUNCTION", "op_read_audit"), opLogFunctionBody(t, up, "CREATE OR REPLACE FUNCTION", "op_read_audit")
	if begin32 == begin33 || audit32 == audit33 {
		t.Fatal("PREMISE: a replaced body is the same text as the one it replaces")
	}
	if got := opLogFunctionBody(t, down, "CREATE OR REPLACE FUNCTION", "op_begin_read"); got != begin32 {
		t.Error("00033's Down does not give op_begin_read 00032's Up body verbatim")
	}
	if got := opLogFunctionBody(t, down, "CREATE OR REPLACE FUNCTION", "op_read_audit"); got != audit32 {
		t.Error("00033's Down does not give op_read_audit 00032's Up body verbatim")
	}
	downStatements := opStatementsOnly(down)
	for _, want := range []string{
		"DROP TRIGGER IF EXISTS " + op00033Trigger + " ON operator_audit_log;",
		"DROP FUNCTION IF EXISTS public." + op00033Function + "();",
	} {
		if !strings.Contains(downStatements, want) {
			t.Errorf("00033's Down does not hold %q", want)
		}
	}
	if regexp.MustCompile(`(?i)\b(REVOKE|GRANT)\b|\bDISABLE\s+TRIGGER\b`).MatchString(downStatements) {
		t.Error("00033's Down holds a REVOKE, a GRANT or a DISABLE TRIGGER; it grants nothing back and takes nothing away")
	}

	// THE SETS: 00031's eleven, from its file.
	m := regexp.MustCompile(`(?s)ADD CONSTRAINT operator_audit_log_kind_check\s+CHECK \(kind IN \(([^)]*)\)\);`).FindStringSubmatch(up31)
	set31 := []string{}
	if m != nil {
		set31 = opQuotedList(strings.Join(strings.Fields(m[1]), " "))
	}
	if !slices.Equal(set31, opAuditKinds31) {
		t.Fatalf("00031's audit kind set is %v, want %v", set31, opAuditKinds31)
	}
	ia := strings.Index(down, "ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check")
	if ia < 0 {
		t.Fatal("00033's Down does not restore the audit CHECKs")
	}
	auditPart := down[ia:]
	conds := func(part string) []string {
		var out []string
		re := regexp.MustCompile(`(?s)IF EXISTS \(SELECT 1 FROM public\.operator_audit_log\s+WHERE (.*?)\) THEN`)
		for _, mm := range re.FindAllStringSubmatch(part, -1) {
			out = append(out, strings.Join(strings.Fields(mm[1]), " "))
		}
		return out
	}
	whole := func(set []string) string { return "kind <> ALL (ARRAY['" + strings.Join(set, "', '") + "'])" }
	upAudit := up[strings.Index(up, "ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check"):]
	for _, c := range []struct {
		what, part string
		set        []string
	}{
		{"00033's Down: the audit condition", auditPart, opAuditKinds31},
		{"00033's Up: the audit condition", upAudit, opAuditKinds},
	} {
		if got, want := conds(c.part), whole(c.set); len(got) != 1 || got[0] != want {
			t.Errorf("%s is %q; want exactly one, whole: %q", c.what, got, want)
		}
	}
	lists := func(part, re string) [][]string {
		var out [][]string
		for _, mm := range regexp.MustCompile(`(?s)`+re).FindAllStringSubmatch(part, -1) {
			out = append(out, opQuotedList(strings.Join(strings.Fields(mm[1]), " ")))
		}
		return out
	}
	sessionless := append(slices.Clone(opAuthEventKinds), opOwnerKinds...)
	for _, c := range []struct {
		what string
		got  [][]string
		want [][]string
	}{
		{"00033's Down: the kind CHECKs", lists(auditPart, `operator_audit_log_kind_check\s+CHECK \(kind IN \(([^)]*)\)\)`), [][]string{opAuditKinds31, opAuditKinds31}},
		{"00033's Down: the actor_shape lists", lists(auditPart, `kind (?:NOT )?IN \(([^)]*)\)\s+AND session_id`),
			[][]string{opAuthEventKinds, opAuthEventKinds, opAuthEventKinds, opAuthEventKinds}},
		{"00033's Up: the kind CHECKs", lists(upAudit, `operator_audit_log_kind_check\s+CHECK \(kind IN \(([^)]*)\)\)`), [][]string{opAuditKinds, opAuditKinds}},
		{"00033's Up: the actor_shape lists", lists(upAudit, `kind (?:NOT )?IN \(([^)]*)\)\s+AND session_id`),
			[][]string{opAuthEventKinds, opOwnerKinds, sessionless, opAuthEventKinds, opOwnerKinds, sessionless}},
	} {
		if len(c.got) != len(c.want) {
			t.Errorf("%s: %d list(s), want %d", c.what, len(c.got), len(c.want))
			continue
		}
		for i := range c.got {
			if !slices.Equal(c.got[i], c.want[i]) {
				t.Errorf("%s: list %d names %v, want %v", c.what, i+1, c.got[i], c.want[i])
			}
		}
	}

	type state struct {
		trigger, function      int64
		begin, audit           string
		kinds, shape           string
		kindsValid, shapeValid bool
		operatorExec, appExec  bool
		definerACL             string
	}
	read := func(q pgx.Tx) state {
		t.Helper()
		var s state
		if err := q.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM pg_trigger WHERE tgrelid = 'public.operator_audit_log'::regclass AND tgname = $1 AND tgenabled = 'O'),
			       (SELECT count(*) FROM pg_proc WHERE proname = $2),
			       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_begin_read(text, text, jsonb)'::regprocedure),
			       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_read_audit(text, text, text, integer, integer)'::regprocedure),
			       has_function_privilege('tappa_operator', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE')
			       AND has_function_privilege('tappa_operator', 'public.op_read_audit(text, text, text, integer, integer)', 'EXECUTE'),
			       has_function_privilege('tappa_app', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE')
			       OR has_function_privilege('tappa_app', 'public.op_read_audit(text, text, text, integer, integer)', 'EXECUTE'),
			       (SELECT coalesce(string_agg(a.attname || ':' || x.privilege_type, ',' ORDER BY a.attnum, x.privilege_type), '')
			          FROM pg_attribute a, aclexplode(a.attacl) AS x
			         WHERE a.attrelid = 'public.operator_audit_log'::regclass AND a.attnum > 0
			           AND x.grantee = 'tappa_opdefiner'::regrole)`, op00033Trigger, op00033Function).
			Scan(&s.trigger, &s.function, &s.begin, &s.audit, &s.operatorExec, &s.appExec, &s.definerACL); err != nil {
			t.Fatalf("read the state: %v", err)
		}
		s.kinds, s.kindsValid = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_kind_check")
		s.shape, s.shapeValid = opConstraint(t, ctx, q, "operator_audit_log", "operator_audit_log_actor_shape")
		return s
	}
	const acl = "id:SELECT,at:SELECT,kind:INSERT,kind:SELECT,session_id:INSERT,session_id:SELECT,actor_admin_id:INSERT,actor_admin_id:SELECT," +
		"target_admin_id:INSERT,target_admin_id:SELECT,target_tenant_id:INSERT,target_tenant_id:SELECT,target_scope:INSERT,target_scope:SELECT," +
		"page_number:INSERT,page_number:SELECT,page_size:INSERT,page_size:SELECT,detail:INSERT,detail:SELECT"
	notValid := func(def string, nv bool) string {
		if nv {
			return def + " NOT VALID"
		}
		return def
	}
	at33 := func(s state, when string, nv bool) {
		t.Helper()
		if s.trigger != 1 || s.function != 1 || s.begin != begin33 || s.audit != audit33 || !s.operatorExec || s.appExec || s.definerACL != acl ||
			s.kinds != notValid(opKindCheckDef(opAuditKinds), nv) || s.kindsValid == nv ||
			s.shape != notValid(opActorShapeDef33(opAuthEventKinds, opOwnerKinds), nv) || s.shapeValid == nv {
			t.Errorf("%s: trigger=%d function=%d begin is 00033's=%v audit is 00033's=%v operator=%v app=%v acl=(%s)\n kinds=%s (%v)\n shape=%s (%v)\n want 00033's state, NOT VALID=%v",
				when, s.trigger, s.function, s.begin == begin33, s.audit == audit33, s.operatorExec, s.appExec, s.definerACL,
				s.kinds, s.kindsValid, s.shape, s.shapeValid, nv)
		}
	}
	at32 := func(s state, when string, nv bool) {
		t.Helper()
		if s.trigger != 0 || s.function != 0 || s.begin != begin32 || s.audit != audit32 || !s.operatorExec || s.appExec || s.definerACL != acl ||
			s.kinds != notValid(opKindCheckDef(opAuditKinds31), nv) || s.kindsValid == nv ||
			s.shape != notValid(opActorShapeDef(opAuthEventKinds), nv) || s.shapeValid == nv {
			t.Errorf("%s: trigger=%d function=%d begin is 00032's=%v audit is 00032's=%v operator=%v app=%v acl=(%s)\n kinds=%s (%v)\n shape=%s (%v)\n want 00032's state, NOT VALID=%v",
				when, s.trigger, s.function, s.begin == begin32, s.audit == audit32, s.operatorExec, s.appExec, s.definerACL,
				s.kinds, s.kindsValid, s.shape, s.shapeValid, nv)
		}
	}
	at33(read(tx), "PREMISE before Down", false)

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	// branch opens a savepoint holding no audit row outside 00031's set (the append-only trigger
	// disabled inside the savepoint for the delete, as 00031's Down test does).
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
			{`DELETE FROM operator_audit_log WHERE kind <> ALL ($1)`, []any{opAuditKinds31}},
			{`ALTER TABLE operator_audit_log ENABLE TRIGGER operator_audit_log_append_only`, nil},
		} {
			if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
				t.Fatalf("clear the kinds 00031 does not know, inside the transaction: %v", err)
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
	ownerAt := func(q pgx.Tx) time.Time {
		t.Helper()
		var at time.Time
		if err := q.QueryRow(ctx, `INSERT INTO operator_audit_log (at, kind, session_id, actor_admin_id)
		                           VALUES ('1999-01-01 00:00:00+00', 'logout', $1, $2) RETURNING at`, session, a.id).Scan(&at); err != nil {
			t.Fatalf("the owner's dated row: %v", err)
		}
		return at
	}
	past := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)

	// Branch 1: nothing outside 00031's set -> VALIDATED; Up again -> 00033.
	sp := branch()
	opRunSection(t, ctx, sp, down, "00033 Down with nothing outside 00031's set")
	at32(read(sp), "after Down (nothing outside 00031's set)", false)
	if at := ownerAt(sp); !at.Equal(past) {
		t.Errorf("after Down the owner's chosen `at` does not stand (%v); the trigger should be gone", at)
	}
	// opadmin's own statement after Down -- what a script of this change meets on a database
	// at 00032: refused, and by actor_shape (the CHECKs run in name order; the owner kind falls
	// to 00031's session arm), the name opadmin's constraint handler then reports.
	{
		err := opTry(t, ctx, sp, `INSERT INTO public.operator_audit_log (kind, target_admin_id) VALUES ('operator_created', $1)`, a.id)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != sqlstateCheckViolation || pg.ConstraintName != "operator_audit_log_actor_shape" {
			t.Errorf("after Down, opadmin's audit INSERT: %v, want 23514 from operator_audit_log_actor_shape", err)
		}
	}
	_, err := opBegin(t, ctx, sp, hash, operatorAuditReadKind, opLogParams("operator_created", 1, 50))
	opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "after Down, an owner kind as a log-read filter")
	opRunSection(t, ctx, sp, up, "00033 Up again")
	at33(read(sp), "after Up again", false)
	if at := ownerAt(sp); at.Equal(past) {
		t.Error("after Up again the owner's chosen `at` stands; the trigger is not back")
	}
	done(sp)

	// Branch 2: an owner row -> both CHECKs NOT VALID; a new owner row refused; Up VALIDATED.
	sp = branch()
	if _, err := sp.Exec(ctx, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('operator_disabled', $1)`, a.id); err != nil {
		t.Fatalf("an owner row: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00033 Down with an owner row present")
	at32(read(sp), "after Down (an owner row)", true)
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('operator_created', $1)`, a.id),
		sqlstateCheckViolation, "a NEW owner row after Down (NOT VALID still binds new rows)")
	opRunSection(t, ctx, sp, up, "00033 Up again over the owner row")
	at33(read(sp), "after Up again (an owner row)", false)
	done(sp)

	// Branch 3: a LATER migration's kind alone. Its Up widened the set, a row of its kind was
	// written, its Down put 00033's set back NOT VALID and left it.
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
	opRunSection(t, ctx, sp, down, "00033 Down after a later migration's Down left its row")
	at32(read(sp), "after Down (a later migration's kind)", true)
	opRunSection(t, ctx, sp, up, "00033 Up again with a later migration's row present (it composes)")
	at33(read(sp), "after Up again (a later migration's kind)", true)
	done(sp)

	// The chain composes downward: 33 -> 32 -> 31 with an owner row present.
	sp = branch()
	if _, err := sp.Exec(ctx, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('operator_mfa_reset', $1)`, a.id); err != nil {
		t.Fatalf("an owner row: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00033 Down (chain)")
	opRunSection(t, ctx, sp, down32, "00032 Down after 00033's, an owner row present")
	opRunSection(t, ctx, sp, down31, "00031 Down after 00032's, an owner row present")
	if def, valid := opConstraint(t, ctx, sp, "operator_audit_log", "operator_audit_log_kind_check"); valid ||
		def != notValid(opKindCheckDef(opAuditKinds27), true) {
		t.Errorf("after 33 -> 32 -> 31 the audit kind CHECK is %s (validated %v); want 00027's set NOT VALID", def, valid)
	}
	done(sp)

	// And with nothing outside the sets, Down and Up once more: VALIDATED both ways.
	sp = branch()
	opRunSection(t, ctx, sp, down, "00033 Down")
	opRunSection(t, ctx, sp, up, "00033 Up")
	at33(read(sp), "after Down and Up", false)
	done(sp)
}

// ------------------------------------------------------------- precondition --

// TestOperator00033_PreconditionRefusesAWrongCluster: 00033's first statement refuses the role
// shapes 00026-00032's refuse (absent, over-privileged, joined by membership in either
// direction) and a database that is not at 00032 -- op_read_audit or op_begin_read missing or
// not the definer's, 00032's read missing, the audit kind CHECK or actor_shape missing, the kind
// CHECK without 00031's 'password_ok' or already naming this file's 'operator_created' -- with
// SQLSTATE 55000 naming 00033, and passes the cluster this suite runs on (taken to 00032 first).
func TestOperator00033_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 32, opKindsAt32...)
	up, _ := opMigrationSections(t, op00033File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00033's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	const kinds = `ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
	               ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check CHECK (kind IN (%s)) NOT VALID`
	quoted := func(set []string) string { return "'" + strings.Join(set, "', '") + "'" }
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"roles present, at 00032", nil, ""},
		{"both roles absent", []string{`ALTER ROLE tappa_operator RENAME TO zz_op14d_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op14d_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner without BYPASSRLS", []string{`ALTER ROLE tappa_opdefiner NOBYPASSRLS`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_opdefiner is a member", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
		{"op_read_audit missing", []string{`ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) RENAME TO zz_op14d_was_read_audit`}, "00032's functions"},
		{"op_read_audit not the definer's", []string{`ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) OWNER TO tappa_owner`}, "00032's functions"},
		{"op_begin_read not the definer's", []string{`ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_owner`}, "00032's functions"},
		{"00032's read missing", []string{`ALTER FUNCTION public.op_read_tenant_billing(text, text, uuid, integer) RENAME TO zz_op14d_was_billing`}, "00032's functions"},
		{"the kind CHECK missing", []string{`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check`}, "as migration 00031 left them"},
		{"actor_shape missing", []string{`ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_actor_shape`}, "as migration 00031 left them"},
		{"the kind CHECK without password_ok", []string{fmtKinds(kinds, quoted(opAuditKinds27))}, "as migration 00031 left them"},
		{"the kind CHECK already naming operator_created", []string{fmtKinds(kinds, quoted(opAuditKinds))}, "as migration 00031 left them"},
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
		case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00033")):
			t.Errorf("%s: precondition answered %q %q, want %s naming 00033 and containing %q", c.what, code, msg, sqlstatePrerequisiteState, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("%s: rollback: %v", c.what, err)
		}
	}
}

// fmtKinds puts a quoted kind list into a statement template's one %s.
func fmtKinds(template, list string) string { return strings.Replace(template, "%s", list, 1) }
