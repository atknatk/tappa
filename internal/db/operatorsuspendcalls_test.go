package db

// operatorsuspendcalls_test.go -- migration 00063 (M10 OP-15, phase A): the BEHAVIOUR of
// op_suspend_tenant and op_reinstate_tenant and of the two reads' new columns (ADR 0025 §2 and
// "Güvenlik iddiası" PART I A). How the tests touch the shared database, and why no test commits
// a tenant or a suspension: operatorsuspend_test.go's header.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// opIndexPathsOnly is TestOpRecordVATCheck_AnUnknownTenantIsTheSameVoid's measure (a32c7fa), for
// the two tests here that compare pg_stat_xact_user_tables vectors between calls: set before
// anything in the transaction is planned (the RI checks' and the functions' plans are cached on
// the connection). Every call runs op_touch_session, whose UPDATE writes a new version of the
// session row, and in a SMALL platform_sessions -- a fresh CI database -- the planner takes that
// UPDATE and the operator row's foreign-key check by SEQUENTIAL scan, whose seq_tup_read counts
// the visible rows physically before the new version: heap layout, not the arguments
// (n_tup_hot_upd's class). By index each fetches exactly one version, as a large database plans
// it. jit = off: disable_cost lifts the remaining sequential scans past jit_above_cost, and every
// call would pay JIT compilation for nothing -- JIT changes no row read.
func opIndexPathsOnly(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	for _, s := range []string{`SET LOCAL enable_seqscan = off`, `SET LOCAL jit = off`} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// TestOpSuspendTenant_ArgumentsAreRefusedBeforeAnythingIsRead is ADR 0025 §2 step 2: a NULL
// tenant, a NULL reason, and a reason outside the closed set -- the empty string, another word,
// another case, a leading or trailing space, a hyphen, a quote, three hundred characters -- are the
// one 22023 with the constant message, no DETAIL, and no field carrying the tenant id, the session
// hash or the reason; op_reinstate_tenant's NULL tenant the same with its own message. Nothing is
// written: no tenant changes (row version included), no tenant row, no operator row. THE
// STATISTICS of a refusal, rolled back inside a savepoint: for an existing and an absent tenant
// every counter of pg_stat_xact_user_tables moves alike, and tenants' not at all (the refusal reads
// nothing of it). The session is resolved first: a dead session with a bad reason is 28000.
func TestOpSuspendTenant_ArgumentsAreRefusedBeforeAnythingIsRead(t *testing.T) {
	ctx, tx := opTx(t)
	opIndexPathsOnly(t, ctx, tx)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	f := opSuspTenant(t, ctx, tx, "op15 args "+tok, "", "")
	g := opSuspTenant(t, ctx, tx, "op15 args suspended "+tok, "2025-04-05 06:07:08.123456+00", "non_payment")
	fBefore, gBefore, actions := opSuspOf(t, ctx, tx, f.id), opSuspOf(t, ctx, tx, g.id), opSuspActions(t, ctx, tx)
	for _, c := range []struct {
		what           string
		tenant, reason any
		value          string
	}{
		{"a NULL tenant", nil, "non_payment", ""},
		{"a NULL reason", f.id, nil, ""},
		{"both NULL", nil, nil, ""},
		{"an empty reason", f.id, "", ""},
		{"another word", f.id, "other", "other"},
		{"another case", g.id, "NON_PAYMENT", "NON_PAYMENT"},
		{"a trailing space", g.id, "security_hold ", "security_hold "},
		{"a leading space", f.id, " terms_breach", " terms_breach"},
		{"a hyphen", f.id, "non-payment", "non-payment"},
		{"a quote", f.id, "non_payment'); --", "non_payment'); --"},
		{"three hundred characters", f.id, strings.Repeat("z", 300), strings.Repeat("z", 300)},
	} {
		opWantClean(t, opSuspend(t, ctx, tx, hash, c.tenant, c.reason), sqlstateInvalidParameter, suspendRefusal, c.what,
			hash, f.id.String(), g.id.String(), c.value)
	}
	opWantClean(t, opReinstate(t, ctx, tx, hash, nil), sqlstateInvalidParameter, reinstateRefusal, "op_reinstate_tenant: a NULL tenant", hash)
	if !opSuspOf(t, ctx, tx, f.id).same(fBefore) || !opSuspOf(t, ctx, tx, g.id).same(gBefore) {
		t.Error("a refused call changed a tenant or rewrote its row")
	}
	if d := opSuspActions(t, ctx, tx) - actions; d != 0 {
		t.Errorf("refused calls wrote %d tenant row(s)", d)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1`, session); n != 0 {
		t.Errorf("refused calls wrote %d operator row(s)", n)
	}

	absent := uuid.New()
	call := func(tenant uuid.UUID) map[string]opStatCounters {
		t.Helper()
		s0 := opStatXact(t, ctx, tx)
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		opWantClean(t, opSuspend(t, ctx, sp, hash, tenant, "other"), sqlstateInvalidParameter, suspendRefusal, "a measured refusal", hash, tenant.String())
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
	t.Logf("OP15-ARGS: a refusal for an existing tenant: %s", opStatText(present))
	t.Logf("OP15-ARGS: a refusal for an absent tenant: %s", opStatText(missing))
	if opStatText(present) != opStatText(missing) {
		t.Errorf("a refusal moves the counters differently for an existing tenant (%s) and an absent one (%s)", opStatText(present), opStatText(missing))
	}
	if c, ok := present["public.tenants"]; ok {
		t.Errorf("a refusal touched tenants: %+v; the refusal comes before every reference to it", c)
	}

	dead := opDeadSessions(t, ctx, tx)[0]
	opWantClean(t, opSuspend(t, ctx, tx, dead.hash, f.id, "other"), sqlstateInvalidAuthorization, touchRefusal00026,
		"a dead session with a bad reason (the session first)", dead.hash)
}

// TestOpSuspendTenant_WritesTheSuspensionAndBothRowsInOneCall: for each reason of the closed set,
// through the PRODUCT's accessors, with the caller's TimeZone far from UTC:
//   - SuspendTenant: the tenant now holds a time inside the call (the database's wall clock) and the
//     reason; exactly one tenant row -- action 'tenant.suspended', the operator as actor_id, the
//     tenant's id as target, `at` EQUAL to the new suspended_at (one clock read), detail EXACTLY
//     {"reason": <code>, "actor_kind": "operator"}; exactly one operator row of the exact
//     tenant-write shape ('tenant_suspended', the session, the operator, the tenant, nothing else);
//   - ReinstateTenant: both columns NULL; exactly one 'tenant.reinstated' row -- the operator, the
//     tenant id, `at` inside the call, detail EXACTLY {"actor_kind": "operator"} --; exactly one
//     'tenant_reinstated' operator row of the exact shape.
func TestOpSuspendTenant_WritesTheSuspensionAndBothRowsInOneCall(t *testing.T) {
	ctx, tx := opTx(t)
	if _, err := tx.Exec(ctx, `SET LOCAL TimeZone = 'Pacific/Kiritimati'`); err != nil {
		t.Fatal(err)
	}
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	for _, reason := range SuspensionReasons() {
		f := opSuspTenant(t, ctx, tx, "op15 writes "+string(reason)+" "+tok, "", "")
		start := opClock(t, ctx, tx)
		if err := opSuspendAccessor(t, ctx, tx, hash, f.id, reason); err != nil {
			t.Fatalf("SuspendTenant(%s): %v", reason, err)
		}
		end := opClock(t, ctx, tx)
		s := opSuspOf(t, ctx, tx, f.id)
		if s.at == nil || s.at.Before(start) || s.at.After(end) || opDeref(s.reason) != string(reason) {
			t.Errorf("%s: the tenant holds %v / %q; want a time inside [%v, %v] and the reason", reason, s.at, opDeref(s.reason), start, end)
			continue
		}
		trail := opTenantTrail(t, ctx, tx, f.id, opSuspendAction)
		if len(trail) != 1 {
			t.Errorf("%s: %d tenant row(s), want 1", reason, len(trail))
			continue
		}
		r := trail[0]
		if r.actor == nil || *r.actor != a.id || r.target == nil || *r.target != f.id.String() || !r.at.Equal(*s.at) {
			t.Errorf("%s: the tenant row's actor is the operator=%v, target the tenant=%v, at the suspension's time=%v", reason,
				r.actor != nil && *r.actor == a.id, r.target != nil && *r.target == f.id.String(), r.at.Equal(*s.at))
		}
		if want := opSuspendDetail(string(reason)); r.detail != want {
			t.Errorf("%s: the tenant row's detail is\n %s\n want\n %s", reason, r.detail, want)
		}
		if n := opSuspOperatorRows(t, ctx, tx, "tenant_suspended", session, f.id); n != 1 {
			t.Errorf("%s: %d operator row(s) of the exact shape, want 1", reason, n)
		}

		start = opClock(t, ctx, tx)
		if err := opReinstateAccessor(t, ctx, tx, hash, f.id); err != nil {
			t.Fatalf("ReinstateTenant: %v", err)
		}
		end = opClock(t, ctx, tx)
		if s := opSuspOf(t, ctx, tx, f.id); s.at != nil || s.reason != nil {
			t.Errorf("%s: after the reinstatement the tenant holds %v / %q; want both NULL", reason, s.at, opDeref(s.reason))
		}
		back := opTenantTrail(t, ctx, tx, f.id, opReinstateAction)
		if len(back) != 1 || back[0].actor == nil || *back[0].actor != a.id || back[0].target == nil || *back[0].target != f.id.String() ||
			back[0].at.Before(start) || back[0].at.After(end) || back[0].detail != opReinstateDetail {
			t.Errorf("%s: the reinstatement's tenant rows are %+v; want one: the operator, the tenant, a time inside the call, %s", reason, back, opReinstateDetail)
		}
		if n := opSuspOperatorRows(t, ctx, tx, "tenant_reinstated", session, f.id); n != 1 {
			t.Errorf("%s: %d reinstatement operator row(s) of the exact shape, want 1", reason, n)
		}
	}
}

// TestOpSuspendTenant_ASecondSuspensionKeepsTheFirstTime is ADR 0025's idempotence (the
// orchestrator's K15-4 and K15-11):
//   - a second suspension keeps suspended_at -- the FIRST call's time -- and replaces the reason;
//     each call writes its own tenant row (the second's `at` is later than the first's, its detail
//     names the second reason) and its own operator row;
//   - a reinstatement of a tenant that is not suspended still writes its tenant row and its
//     operator row (the row says the action was applied; the body never reads the previous state),
//     and leaves both columns NULL; so does a second reinstatement.
func TestOpSuspendTenant_ASecondSuspensionKeepsTheFirstTime(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	f := opSuspTenant(t, ctx, tx, "op15 twice "+tok, "", "")
	if err := opSuspendAccessor(t, ctx, tx, hash, f.id, SuspensionNonPayment); err != nil {
		t.Fatal(err)
	}
	first := opSuspOf(t, ctx, tx, f.id)
	// Fatal, not Errorf: everything below compares against the first time, and a nil one panicked
	// the whole package (the third eye's F2: under M19 the tests after this one never ran).
	if first.at == nil {
		t.Fatalf("the first suspension left the tenant with %v / %q; want a time and the reason", first.at, opDeref(first.reason))
	}
	if err := opSuspendAccessor(t, ctx, tx, hash, f.id, SuspensionTermsBreach); err != nil {
		t.Fatal(err)
	}
	second := opSuspOf(t, ctx, tx, f.id)
	if second.at == nil || !second.at.Equal(*first.at) || opDeref(second.reason) != "terms_breach" {
		t.Errorf("after a second suspension the tenant holds %v / %q; want the first time %v and the second reason", second.at, opDeref(second.reason), first.at)
	}
	trail := opTenantTrail(t, ctx, tx, f.id, opSuspendAction)
	if len(trail) != 2 || !trail[0].at.Equal(*first.at) || !trail[1].at.After(trail[0].at) ||
		trail[0].detail != opSuspendDetail("non_payment") || trail[1].detail != opSuspendDetail("terms_breach") {
		t.Errorf("the two suspensions' tenant rows are %+v; want two, the second later and naming the second reason", trail)
	}
	if n := opSuspOperatorRows(t, ctx, tx, "tenant_suspended", session, f.id); n != 2 {
		t.Errorf("%d suspension operator row(s), want 2", n)
	}

	g := opSuspTenant(t, ctx, tx, "op15 never suspended "+tok, "", "")
	before := opSuspOf(t, ctx, tx, g.id)
	for i := 1; i <= 2; i++ {
		if err := opReinstateAccessor(t, ctx, tx, hash, g.id); err != nil {
			t.Fatal(err)
		}
		if s := opSuspOf(t, ctx, tx, g.id); s.at != nil || s.reason != nil || s.version == before.version {
			t.Errorf("reinstatement %d of a tenant never suspended: %v / %q, row rewritten=%v; want both NULL and the row written", i, s.at, opDeref(s.reason), s.version != before.version)
		}
		if n, m := len(opTenantTrail(t, ctx, tx, g.id, opReinstateAction)), opSuspOperatorRows(t, ctx, tx, "tenant_reinstated", session, g.id); n != i || m != int64(i) {
			t.Errorf("after reinstatement %d of a tenant never suspended: %d tenant row(s), %d operator row(s); want %d each", i, n, m, i)
		}
	}
}

// TestOpSuspendTenant_TheAnswerIsTheSameWhateverTheTenantsState is ADR 0021 §2 v 7 and B14 for
// both writes: for a tenant that is not suspended, one suspended for each of two reasons and an id
// that names no tenant, the shipped statements answer the same command tag and no error, and the
// accessors nil; a dead session is the same 28000 for each, a bad reason the same 22023. For the
// unknown id: no tenant row anywhere (no foreign key fires), no tenant changes, and the operator's
// row naming the id is written by each call.
func TestOpSuspendTenant_TheAnswerIsTheSameWhateverTheTenantsState(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	dead := opDeadSessions(t, ctx, tx)[3] // revoked
	tok := opToken(t)
	type target struct {
		what   string
		tenant uuid.UUID
	}
	var targets []target
	for _, s := range opSuspStates {
		f := opSuspTenant(t, ctx, tx, "op15 answer "+s.name+" "+tok, s.at, s.reason)
		targets = append(targets, target{s.name, f.id})
	}
	unknown := uuid.New()
	targets = append(targets, target{"an unknown tenant", unknown})

	actions, start := opSuspActions(t, ctx, tx), opClock(t, ctx, tx)
	for range 2 {
		if err := opSuspendAccessor(t, ctx, tx, hash, unknown, SuspensionSecurityHold); err != nil {
			t.Errorf("an unknown tenant's suspension: %v, want the same nil as a known one", err)
		}
		if err := opReinstateAccessor(t, ctx, tx, hash, unknown); err != nil {
			t.Errorf("an unknown tenant's reinstatement: %v, want the same nil as a known one", err)
		}
	}
	if d := opSuspActions(t, ctx, tx) - actions; d != 0 {
		t.Errorf("calls for an unknown tenant wrote %d tenant row(s)", d)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tenants WHERE suspended_at >= $1`, start); n != 0 {
		t.Errorf("calls for an unknown tenant suspended %d tenant(s)", n)
	}
	if s, r := opSuspOperatorRows(t, ctx, tx, "tenant_suspended", session, unknown), opSuspOperatorRows(t, ctx, tx, "tenant_reinstated", session, unknown); s != 2 || r != 2 {
		t.Errorf("%d / %d operator row(s) naming the unknown id, want 2 / 2", s, r)
	}

	tags := map[string]map[string]bool{"suspend": {}, "reinstate": {}}
	for _, c := range targets {
		for name, run := range map[string]func(sp pgx.Tx) (pgconn.CommandTag, error){
			"suspend": func(sp pgx.Tx) (pgconn.CommandTag, error) {
				return sp.Exec(ctx, suspendTenantSQL, hash, c.tenant, "customer_request")
			},
			"reinstate": func(sp pgx.Tx) (pgconn.CommandTag, error) { return sp.Exec(ctx, reinstateTenantSQL, hash, c.tenant) },
		} {
			var tag pgconn.CommandTag
			if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
				var e error
				tag, e = run(sp)
				return e
			}); err != nil {
				t.Errorf("%s, %s: %v", c.what, name, err)
			}
			tags[name][tag.String()] = true
		}
		if err := opSuspendAccessor(t, ctx, tx, hash, c.tenant, SuspensionNonPayment); err != nil {
			t.Errorf("%s: SuspendTenant answered %v, want nil", c.what, err)
		}
		if err := opReinstateAccessor(t, ctx, tx, hash, c.tenant); err != nil {
			t.Errorf("%s: ReinstateTenant answered %v, want nil", c.what, err)
		}
		opWantClean(t, opSuspend(t, ctx, tx, dead.hash, c.tenant, "non_payment"), sqlstateInvalidAuthorization, touchRefusal00026,
			c.what+", a dead session's suspension", dead.hash)
		opWantClean(t, opReinstate(t, ctx, tx, dead.hash, c.tenant), sqlstateInvalidAuthorization, touchRefusal00026,
			c.what+", a dead session's reinstatement", dead.hash)
		opWantClean(t, opSuspend(t, ctx, tx, hash, c.tenant, "other"), sqlstateInvalidParameter, suspendRefusal, c.what+", a bad reason")
	}
	for name, set := range tags {
		if len(set) != 1 {
			t.Errorf("%s answered %d different command tags across the tenant's states: %v", name, len(set), set)
		}
	}
}

// TestOpSuspendTenant_TheTenantRowsTimeIsTheWallClock is K6's `at` (ADR 0021 §2 vii): each write
// inside ONE DO statement that first sleeps 1.5 s -- the sleep INSIDE the statement, so the
// transaction's and the statement's start are both left behind -- leaves the tenant row's `at`
// (and, for the suspension, suspended_at, the same read) inside the call: at least 1.4 s after the
// transaction's start and after the clock read before the statement, at most the wall clock after
// it. A frozen clock (now(), statement_timestamp()) would date it before the sleep.
func TestOpSuspendTenant_TheTenantRowsTimeIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	f := opSuspTenant(t, ctx, tx, "op15 clock "+opToken(t), "", "")
	for _, c := range []struct{ action, call string }{
		{opSuspendAction, `PERFORM public.op_suspend_tenant(current_setting('tappa_test.session'), current_setting('tappa_test.tenant')::uuid, 'security_hold');`},
		{opReinstateAction, `PERFORM public.op_reinstate_tenant(current_setting('tappa_test.session'), current_setting('tappa_test.tenant')::uuid);`},
	} {
		before := opClock(t, ctx, tx)
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('tappa_test.session', $1, true), set_config('tappa_test.tenant', $2, true)`, hash, f.id.String()); err != nil {
				return err
			}
			_, err := sp.Exec(ctx, "DO $d$ BEGIN PERFORM pg_sleep(1.5); "+c.call+" END $d$")
			return err
		}); err != nil {
			t.Fatalf("%s: the DO block: %v", c.action, err)
		}
		after := opClock(t, ctx, tx)
		trail := opTenantTrail(t, ctx, tx, f.id, c.action)
		if len(trail) != 1 {
			t.Fatalf("PREMISE: %d %s row(s), want 1", len(trail), c.action)
		}
		at := trail[0].at
		var sinceStart float64
		if err := tx.QueryRow(ctx, `SELECT extract(epoch FROM ($1::timestamptz - transaction_timestamp()))::float8`, at).Scan(&sinceStart); err != nil {
			t.Fatal(err)
		}
		if at.Before(before.Add(1400*time.Millisecond)) || at.After(after) || sinceStart < 1.4 {
			t.Errorf("%s: the tenant row is dated %v, %.3f s after the transaction's start; want inside [%v + 1.4 s, %v] (the wall clock)", c.action, at, sinceStart, before, after)
		}
		if c.action == opSuspendAction {
			if s := opSuspOf(t, ctx, tx, f.id); s.at == nil || !s.at.Equal(at) {
				t.Error("the suspension's time and its tenant row's `at` are not one clock read")
			}
		}
	}
}

// TestOpSuspendTenant_TheWritesShareOneTransaction is K6's "same transaction" for both writes (the
// emsal: TestOpRecordVATCheck_TheThreeWritesShareOneTransaction), one arm per write:
//   - (i) the calling transaction rolled back takes all three writes with it;
//   - (ii) the tenant's UPDATE refused (the definer's UPDATE on the two columns revoked): 42501,
//     no tenant row and no operator row;
//   - (iii) the tenant row refused (the definer's INSERT on audit_log revoked): 42501, the tenant
//     unchanged and no operator row;
//   - (iv) the operator row refused (its INSERT column revoked): 42501, the tenant unchanged and no
//     tenant row;
//   - (v) the operator row refused by a CHECK, and (vi) the tenant's UPDATE refused by a CHECK on
//     tenants (each added NOT VALID inside a savepoint): the caught constraint is the one 22023,
//     no DETAIL, no argument value in any field -- and nothing is written.
func TestOpSuspendTenant_TheWritesShareOneTransaction(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	for _, w := range []struct {
		name, kind, action, refusal string
		start                       [2]string
		call                        func(q pgx.Tx, tenant uuid.UUID) error
		tenantCheck                 string
	}{
		{"op_suspend_tenant", "tenant_suspended", opSuspendAction, suspendRefusal, [2]string{"", ""},
			func(q pgx.Tx, tenant uuid.UUID) error { return opSuspend(t, ctx, q, hash, tenant, "terms_breach") },
			`ALTER TABLE tenants ADD CONSTRAINT zz_op15_refuse CHECK (id <> '%s' OR suspended_at IS NULL) NOT VALID`},
		{"op_reinstate_tenant", "tenant_reinstated", opReinstateAction, reinstateRefusal, [2]string{"2025-07-08 09:10:11.654321+00", "security_hold"},
			func(q pgx.Tx, tenant uuid.UUID) error { return opReinstate(t, ctx, q, hash, tenant) },
			`ALTER TABLE tenants ADD CONSTRAINT zz_op15_refuse CHECK (id <> '%s' OR suspended_at IS NOT NULL) NOT VALID`},
	} {
		f := opSuspTenant(t, ctx, tx, "op15 one tx "+w.name+" "+tok, w.start[0], w.start[1])
		before := opSuspOf(t, ctx, tx, f.id)
		nothing := func(q pgx.Tx, what string) {
			t.Helper()
			if now := opSuspOf(t, ctx, q, f.id); !now.same(before) {
				t.Errorf("%s %s: the tenant changed without its rows", w.name, what)
			}
			if n := len(opTenantTrail(t, ctx, q, f.id, w.action)); n != 0 {
				t.Errorf("%s %s: %d tenant row(s) without the rest", w.name, what, n)
			}
			if n := opSuspOperatorRows(t, ctx, q, w.kind, session, f.id); n != 0 {
				t.Errorf("%s %s: %d operator row(s) without the rest", w.name, what, n)
			}
		}

		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.call(sp, f.id); err != nil {
			t.Fatalf("%s (i) the call: %v", w.name, err)
		}
		if now := opSuspOf(t, ctx, sp, f.id); now.same(before) || len(opTenantTrail(t, ctx, sp, f.id, w.action)) != 1 ||
			opSuspOperatorRows(t, ctx, sp, w.kind, session, f.id) != 1 {
			t.Fatalf("PREMISE %s (i): the call did not write its three writes", w.name)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		nothing(tx, "(i) after the calling transaction rolled back")

		for _, c := range []struct{ what, ddl, code string }{
			{"(ii) the tenant's UPDATE refused", `REVOKE UPDATE (suspended_at, suspension_reason) ON tenants FROM tappa_opdefiner`, sqlstateInsufficientPrivi},
			{"(iii) the tenant row refused", `REVOKE INSERT (tenant_id, actor_id, action, target, detail, at) ON audit_log FROM tappa_opdefiner`, sqlstateInsufficientPrivi},
			{"(iv) the operator row refused", `REVOKE INSERT (target_tenant_id) ON operator_audit_log FROM tappa_opdefiner`, sqlstateInsufficientPrivi},
			{"(v) the operator row refused by a CHECK", `ALTER TABLE operator_audit_log ADD CONSTRAINT zz_op15_refuse CHECK (kind <> '` + w.kind + `') NOT VALID`, sqlstateInvalidParameter},
			{"(vi) the tenant's UPDATE refused by a CHECK", fmt.Sprintf(w.tenantCheck, f.id), sqlstateInvalidParameter},
		} {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sp.Exec(ctx, c.ddl); err != nil {
				t.Fatalf("%s %s: %v", w.name, c.what, err)
			}
			err = w.call(sp, f.id)
			if c.code == sqlstateInvalidParameter {
				opWantClean(t, err, c.code, w.refusal, w.name+" "+c.what, hash, f.id.String())
			} else {
				opWant(t, err, c.code, w.name+" "+c.what)
			}
			nothing(sp, c.what)
			if err := sp.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// opRunAsOperator runs one statement as tappa_operator on sp from ANOTHER goroutine: no t helper
// may be called there (t.Fatal belongs to the test's goroutine), so it reports through done. The
// session authorization is put back by the caller.
func opRunAsOperator(ctx context.Context, sp pgx.Tx, done chan<- error, sql string, args ...any) {
	if _, err := sp.Exec(ctx, "SET LOCAL SESSION AUTHORIZATION tappa_operator"); err != nil {
		done <- err
		return
	}
	_, err := sp.Exec(ctx, sql, args...)
	done <- err
}

// TestOpSuspendTenant_TheLockIsTakenBeforeTheClockIsRead is ADR 0025 §2 step 4's order (Y1),
// measured with two sessions. ANOTHER session holds a committed tenant's row -- the database's
// oldest; FOR NO KEY UPDATE, the lock a concurrent call holds until it commits -- while this
// test's call for the same tenant runs (inside a savepoint that is rolled back: the tenant is never
// suspended). The call WAITS (pg_locks shows it); the other session reads the wall clock and lets
// go; the call then completes. The time it wrote -- the tenant row's `at`, and for the suspension
// suspended_at -- is AFTER the other session's last clock read: a call that waited dates its write
// from when it held the row, so the times two racing calls write follow the order they were
// applied (a clock read before the lock would date this write back by the whole wait -- the
// mutation that moves the clock above the lock is red here). Both writes.
func TestOpSuspendTenant_TheLockIsTakenBeforeTheClockIsRead(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	tenant := opOldestTenant(t, ctx, tx)
	pid := opInt(t, ctx, tx, `SELECT pg_backend_pid()`)
	other, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("a second owner connection: %v", err)
	}
	defer func() { _ = other.Close(context.Background()) }()
	for _, c := range []struct {
		action, sql string
		args        []any
	}{
		{opSuspendAction, suspendTenantSQL, []any{hash, tenant, "customer_request"}},
		{opReinstateAction, reinstateTenantSQL, []any{hash, tenant}},
	} {
		otx, err := other.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := otx.Exec(ctx, `SELECT 1 FROM public.tenants WHERE id = $1 FOR NO KEY UPDATE`, tenant); err != nil {
			t.Fatalf("the other session's row lock: %v", err)
		}
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go opRunAsOperator(ctx, sp, done, c.sql, c.args...)
		waited := false
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			select {
			case err := <-done:
				_ = otx.Rollback(ctx)
				t.Fatalf("%s: the call returned (%v) while another session held the row; it did not wait", c.action, err)
			default:
			}
			if n := opInt(t, ctx, otx, `SELECT count(*) FROM pg_locks WHERE pid = $1 AND NOT granted`, pid); n > 0 {
				waited = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !waited {
			_ = otx.Rollback(ctx)
			t.Fatalf("%s: the call never waited for the other session's lock", c.action)
		}
		var released time.Time
		if err := otx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&released); err != nil {
			t.Fatal(err)
		}
		if err := otx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: the call after the lock was let go: %v", c.action, err)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("%s: the call did not complete after the lock was let go", c.action)
		}
		if _, err := sp.Exec(ctx, "SET LOCAL SESSION AUTHORIZATION DEFAULT"); err != nil {
			t.Fatal(err)
		}
		trail := opTenantTrail(t, ctx, sp, tenant, c.action)
		if len(trail) == 0 {
			t.Fatalf("PREMISE: the call wrote no %s row for the tenant", c.action)
		}
		at := trail[len(trail)-1].at
		t.Logf("OP15-Y1: %s: the holder's last clock read %v, the waiting call's write dated %v (%v later)",
			c.action, released.UTC().Format(time.RFC3339Nano), at.UTC().Format(time.RFC3339Nano), at.Sub(released))
		if !at.After(released) {
			t.Errorf("%s: the waiting call dated its write %v, not after the holder let go at %v -- the clock was read before the lock", c.action, at, released)
		}
		if c.action == opSuspendAction {
			if s := opSuspOf(t, ctx, sp, tenant); s.at == nil || !s.at.Equal(at) {
				t.Errorf("the suspension's time %v is not its tenant row's %v", s.at, at)
			}
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestOpSuspendTenant_TheRowLockLetsTheTenantsWritesThrough measures ADR 0025 §2 step 3's lock
// (FOR NO KEY UPDATE -- the UPDATE's own): while a write for a COMMITTED tenant -- the database's
// oldest -- holds that tenant's row inside this test's transaction (a savepoint, rolled back: the
// suspension, the two rows and the lock go with it), ANOTHER session's writes that name the
// tenant -- a tap's INSERT into transactions and an INSERT into its audit_log, each a foreign-key
// check taking FOR KEY SHARE on the tenant row -- go through at once (lock_timeout 300 ms; that
// session's transaction is rolled back, no row stays): a suspension never holds up a tap
// (CLAUDE.md §4.6). CONTROL: with the row locked FOR UPDATE instead, the same tap INSERT is
// cancelled by its lock_timeout (55P03).
func TestOpSuspendTenant_TheRowLockLetsTheTenantsWritesThrough(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	tenant := opOldestTenant(t, ctx, tx)
	other, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("a second owner connection: %v", err)
	}
	defer func() { _ = other.Close(context.Background()) }()
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
			`INSERT INTO public.audit_log (tenant_id, action) VALUES ($1, 'zz_op15_lock_probe')`,
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
	for _, c := range []struct {
		what string
		call func(sp pgx.Tx) error
	}{
		{"the suspension", func(sp pgx.Tx) error { return opSuspendAccessor(t, ctx, sp, hash, tenant, SuspensionCustomerRequest) }},
		{"the reinstatement", func(sp pgx.Tx) error { return opReinstateAccessor(t, ctx, sp, hash, tenant) }},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.call(sp); err != nil {
			t.Fatalf("%s for the committed tenant: %v", c.what, err)
		}
		if n := len(opTenantTrail(t, ctx, sp, tenant, opSuspendAction)) + len(opTenantTrail(t, ctx, sp, tenant, opReinstateAction)); n < 1 {
			t.Fatalf("PREMISE: %s did not reach the tenant, so it holds no lock on it", c.what)
		}
		took, err := probe()
		t.Logf("OP15-LOCK: with %s's row lock held, the other session's tap INSERT and audit INSERT took %v (err %v)", c.what, took.Round(time.Millisecond), err)
		if err != nil {
			t.Errorf("while %s holds its row lock another session's writes naming the tenant were held up: %v", c.what, err)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `SELECT 1 FROM public.tenants WHERE id = $1 FOR UPDATE`, tenant); err != nil {
		t.Fatal(err)
	}
	took, err := probe()
	t.Logf("OP15-LOCK: with a FOR UPDATE lock held, the other session's writes answered %v after %v", err, took.Round(time.Millisecond))
	opWant(t, err, sqlstateLockNotAvailable, "CONTROL: another session's tap INSERT under a FOR UPDATE lock")
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestOpSuspendTenant_ALockWaitTellsOnlyThatTheTenantExists measures ADR 0025 limit 5 (00034's
// LV3 class). ANOTHER session holds a committed tenant's row -- the oldest; FOR NO KEY UPDATE;
// rolled back, nothing written -- while this transaction's calls run under lock_timeout 300 ms:
// for that tenant both writes wait and are cancelled -- 55P03 with the server's own message, no
// tenant id and no session hash in any field --; for an id that names no tenant both answer void
// without a wait. The wait tells that the tenant exists, and nothing else (no state, no reason).
// CONTROL: once the other session lets go, the same calls for the held tenant are void.
func TestOpSuspendTenant_ALockWaitTellsOnlyThatTheTenantExists(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	tenant := opOldestTenant(t, ctx, tx)
	unknown := uuid.New()
	var timeout string
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
		what string
		call func() error
		wait bool
	}{
		{"the held tenant, a suspension", func() error { return opSuspend(t, ctx, tx, hash, tenant, "non_payment") }, true},
		{"the held tenant, a reinstatement", func() error { return opReinstate(t, ctx, tx, hash, tenant) }, true},
		{"an unknown tenant, a suspension", func() error { return opSuspend(t, ctx, tx, hash, unknown, "non_payment") }, false},
		{"an unknown tenant, a reinstatement", func() error { return opReinstate(t, ctx, tx, hash, unknown) }, false},
	} {
		start := time.Now()
		err := c.call()
		took := time.Since(start).Round(time.Millisecond)
		code, _ := opCode(err)
		t.Logf("OP15-LIMIT5: %s: SQLSTATE %q after %v", c.what, code, took)
		if c.wait {
			opWantClean(t, err, sqlstateLockNotAvailable, "canceling statement due to lock timeout", c.what, hash, tenant.String())
		} else if err != nil {
			t.Errorf("%s: %v, want void without a wait", c.what, code)
		}
	}
	if err := otx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := opSuspend(t, ctx, sp, hash, tenant, "non_payment"); err != nil {
		t.Errorf("CONTROL: with no lock held the suspension answered %v, want void", err)
	}
	if err := opReinstate(t, ctx, sp, hash, tenant); err != nil {
		t.Errorf("CONTROL: with no lock held the reinstatement answered %v, want void", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true)`, timeout); err != nil {
		t.Fatal(err)
	}
}

// TestOpSuspendTenant_ACallForOneTenantTouchesNoOther is the belt (ADR 0025 §2, "Kemer"; ADR 0021
// §3.2): no row level security applies inside either write, and p_tenant_id is the only barrier in
// the locking read, the UPDATE, the tenant row and the operator row. Two tenants, A and B:
//   - suspending A changes A alone -- B's columns and row version are unchanged -- and adds exactly
//     one tenant row in the whole of audit_log, A's; the operator row names A;
//   - reinstating B (not suspended) writes B's row only and leaves A suspended and unrewritten;
//   - suspending B for another reason leaves A's reason and time;
//   - every operator row names the tenant its call named.
//
// (Each statement's p_tenant_id is also pinned by its own source-pin mutation in
// TestOperator00063_TheFunctionsAndTheirExactSignatures.)
func TestOpSuspendTenant_ACallForOneTenantTouchesNoOther(t *testing.T) {
	ctx, tx := opTx(t)
	adm := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, adm.id, true)
	tok := opToken(t)
	a := opSuspTenant(t, ctx, tx, "op15 belt A "+tok, "", "")
	b := opSuspTenant(t, ctx, tx, "op15 belt B "+tok, "", "")
	bBefore, actions := opSuspOf(t, ctx, tx, b.id), opSuspActions(t, ctx, tx)
	if err := opSuspendAccessor(t, ctx, tx, hash, a.id, SuspensionNonPayment); err != nil {
		t.Fatal(err)
	}
	aAfter := opSuspOf(t, ctx, tx, a.id)
	if aAfter.at == nil || opDeref(aAfter.reason) != "non_payment" {
		t.Error("the suspension of A did not suspend A")
	}
	if !opSuspOf(t, ctx, tx, b.id).same(bBefore) {
		t.Error("the suspension of A changed B")
	}
	if d, na, nb := opSuspActions(t, ctx, tx)-actions, len(opTenantTrail(t, ctx, tx, a.id, opSuspendAction)), len(opTenantTrail(t, ctx, tx, b.id, opSuspendAction)); d != 1 || na != 1 || nb != 0 {
		t.Errorf("the suspension of A added %d tenant row(s) in all -- A %d, B %d; want 1, A's", d, na, nb)
	}
	if err := opReinstateAccessor(t, ctx, tx, hash, b.id); err != nil {
		t.Fatal(err)
	}
	if !opSuspOf(t, ctx, tx, a.id).same(aAfter) || len(opTenantTrail(t, ctx, tx, a.id, opReinstateAction)) != 0 ||
		len(opTenantTrail(t, ctx, tx, b.id, opReinstateAction)) != 1 {
		t.Error("the reinstatement of B touched A, or did not write B's row")
	}
	if err := opSuspendAccessor(t, ctx, tx, hash, b.id, SuspensionTermsBreach); err != nil {
		t.Fatal(err)
	}
	if !opSuspOf(t, ctx, tx, a.id).same(aAfter) {
		t.Error("the suspension of B for another reason changed A")
	}
	if d := opSuspActions(t, ctx, tx) - actions; d != 3 {
		t.Errorf("three calls added %d tenant row(s) in all, want 3", d)
	}
	for _, c := range []struct {
		kind   string
		tenant uuid.UUID
		want   int64
	}{
		{"tenant_suspended", a.id, 1}, {"tenant_suspended", b.id, 1}, {"tenant_reinstated", a.id, 0}, {"tenant_reinstated", b.id, 1},
	} {
		if n := opSuspOperatorRows(t, ctx, tx, c.kind, session, c.tenant); n != c.want {
			t.Errorf("%d %s operator row(s) naming %v, want %d", n, c.kind, c.tenant == a.id, c.want)
		}
	}
}

// TestOpSuspendTenant_RefusesEveryDeadSession: both writes resolve the session through
// op_touch_session before anything else, so each of the six dead sessions is the touch refusal
// (28000, no DETAIL) and nothing is written -- the tenant, the tenant row, the operator row.
func TestOpSuspendTenant_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	f := opSuspTenant(t, ctx, tx, "op15 dead "+opToken(t), "2025-04-05 06:07:08.123456+00", "non_payment")
	before, actions, audit := opSuspOf(t, ctx, tx, f.id), opSuspActions(t, ctx, tx), opAudit(t, ctx, tx)
	for _, d := range opDeadSessions(t, ctx, tx) {
		opWantClean(t, opSuspend(t, ctx, tx, d.hash, f.id, "terms_breach"), sqlstateInvalidAuthorization, touchRefusal00026, "suspend, "+d.name, d.hash, f.id.String())
		opWantClean(t, opReinstate(t, ctx, tx, d.hash, f.id), sqlstateInvalidAuthorization, touchRefusal00026, "reinstate, "+d.name, d.hash, f.id.String())
	}
	if !opSuspOf(t, ctx, tx, f.id).same(before) || opSuspActions(t, ctx, tx) != actions {
		t.Error("a dead session's call changed the tenant or wrote its row")
	}
	if d := opAudit(t, ctx, tx) - audit; d != 0 {
		t.Errorf("dead sessions' calls left %d operator row(s)", d)
	}
}

// TestOpSuspendTenant_TheStatisticsTellOnlyWhetherTheTenantExists measures ADR 0025 limit 4's
// statistics half (ADR 0021 limit 15; 00034's LV2 shape): a call rolled back inside a savepoint
// leaves no row but moves pg_stat_xact_user_tables. After a warm-up past PostgreSQL's five custom
// plans, each case is measured twice (the PREMISE that it is stable) and logged:
//   - CLOSED, PINNED: for an existing tenant, its previous state -- not suspended, suspended for
//     the reason sent, suspended for another reason -- moves every counter of every table ALIKE,
//     for the suspension and for the reinstatement (the body never reads the previous state, and
//     no write's existence depends on it; the columns are unindexed). n_tup_hot_upd is not among
//     these counters: whether an UPDATE stays on its page is the width channel,
//     TestOpSuspendTenant_TheWidthChannelsAreMeasured's;
//   - COUNTED, PINNED AT THE MEASURED VALUES: whether the id names a tenant -- B14's bit -- stays
//     visible in tenants' idx_tup_fetch and n_tup_upd and in audit_log's n_tup_ins.
func TestOpSuspendTenant_TheStatisticsTellOnlyWhetherTheTenantExists(t *testing.T) {
	ctx, tx := opTx(t)
	opIndexPathsOnly(t, ctx, tx)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	notSuspended := opSuspTenant(t, ctx, tx, "op15 stats none "+tok, "", "")
	sameReason := opSuspTenant(t, ctx, tx, "op15 stats same "+tok, "2025-04-05 06:07:08.123456+00", "non_payment")
	otherReason := opSuspTenant(t, ctx, tx, "op15 stats other "+tok, "2025-07-08 09:10:11.654321+00", "security_hold")
	unknown := uuid.New()
	type kase struct {
		what   string
		tenant uuid.UUID
		sql    string
		args   func(uuid.UUID) []any
	}
	suspend := func(id uuid.UUID) []any { return []any{hash, id, "non_payment"} }
	reinstate := func(id uuid.UUID) []any { return []any{hash, id} }
	cases := []kase{
		{"suspend: an unknown tenant", unknown, suspendTenantSQL, suspend},
		{"suspend: not suspended", notSuspended.id, suspendTenantSQL, suspend},
		{"suspend: suspended for the same reason", sameReason.id, suspendTenantSQL, suspend},
		{"suspend: suspended for another reason", otherReason.id, suspendTenantSQL, suspend},
		{"reinstate: an unknown tenant", unknown, reinstateTenantSQL, reinstate},
		{"reinstate: not suspended", notSuspended.id, reinstateTenantSQL, reinstate},
		{"reinstate: suspended", sameReason.id, reinstateTenantSQL, reinstate},
	}
	call := func(c kase) map[string]opStatCounters {
		t.Helper()
		before := opStatXact(t, ctx, tx)
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := opExecAs(t, ctx, sp, "tappa_operator", c.sql, c.args(c.tenant)...); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return opStatDelta(before, opStatXact(t, ctx, tx))
	}
	for range 6 {
		for _, c := range cases {
			call(c)
		}
	}
	measured := make([]string, len(cases))
	deltas := make([]map[string]opStatCounters, len(cases))
	for i, c := range cases {
		deltas[i] = call(c)
		measured[i] = opStatText(deltas[i])
		if again := opStatText(call(c)); again != measured[i] {
			t.Fatalf("PREMISE: %s moved the counters differently twice (%s, then %s)", c.what, measured[i], again)
		}
		t.Logf("OP15-STATS: %s: %s", c.what, measured[i])
	}
	for _, group := range [][]int{{1, 2, 3}, {5, 6}} {
		for _, i := range group[1:] {
			if measured[i] != measured[group[0]] {
				t.Errorf("THE PREVIOUS STATE LEAKS: %s moved %s, %s moved %s", cases[group[0]].what, measured[group[0]], cases[i].what, measured[i])
			}
		}
	}
	for i, want := range []struct{ fetch, upd, ins int64 }{{0, 0, 0}, {3, 1, 1}, {3, 1, 1}, {3, 1, 1}, {0, 0, 0}, {3, 1, 1}, {3, 1, 1}} {
		got, ins := deltas[i]["public.tenants"], deltas[i]["public.audit_log"].nTupIns
		if got.idxTupFetch != want.fetch || got.nTupUpd != want.upd || ins != want.ins {
			t.Errorf("COUNTED LIMIT moved: after %s rolled back, tenants' idx_tup_fetch moved by %d and n_tup_upd by %d, audit_log's n_tup_ins by %d; "+
				"measured was %d, %d and %d -- update ADR 0021's OP-15 note", cases[i].what, got.idxTupFetch, got.nTupUpd, ins, want.fetch, want.upd, want.ins)
		}
	}
}

// opWALOf runs one statement as tappa_operator under EXPLAIN (ANALYZE, WAL) inside a savepoint of
// tx that is released, and returns the WAL the statement generated (records, full-page images,
// bytes) -- the backend's own counters over the statement, function body included.
func opWALOf(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) (records, fpi, bytes int64) {
	t.Helper()
	var plan string
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, "EXPLAIN (ANALYZE, WAL, FORMAT JSON) "+sql, args...).Scan(&plan)
	}); err != nil {
		t.Fatalf("EXPLAIN (ANALYZE, WAL): %v", err)
	}
	var out []struct {
		Plan struct {
			Records int64 `json:"WAL Records"`
			FPI     int64 `json:"WAL FPI"`
			Bytes   int64 `json:"WAL Bytes"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(plan), &out); err != nil || len(out) != 1 {
		t.Fatalf("read the plan's WAL: %v", err)
	}
	return out[0].Plan.Records, out[0].Plan.FPI, out[0].Plan.Bytes
}

// opHOT is this transaction's n_tup_hot_upd on tenants.
func opHOT(t *testing.T, ctx context.Context, q opQuerier) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT coalesce((SELECT n_tup_hot_upd FROM pg_stat_xact_user_tables
	                                            WHERE relid = 'public.tenants'::regclass), 0)`)
}

// TestOpSuspendTenant_TheWidthChannelsAreMeasured is ADR 0025 limit 4's width half, MEASURED --
// the three channels that can tell a tenant's previous state because a suspended row is wider:
// the HOT counter (n_tup_hot_upd), the table's size (pg_relation_size) and the WAL the call writes
// (EXPLAIN (ANALYZE, WAL): this backend's own counters -- but what the call writes still depends
// on what other sessions left on the pages; the end of the test says how much). For each
// previous state -- not suspended, suspended -- one fixture tenant, and the SAME call (a suspension
// for 'customer_request'), each inside a savepoint that is rolled back:
//   - ROOM: the call on the fixture as inserted; logged;
//   - THE FULL PAGE (the ADR's required case): the fixture's page is first filled with the
//     fixture's own versions (an UPDATE of its name, kept while it stays on the page; the first
//     that would leave the page is rolled back, so the fixture's live row stays there), then the
//     call -- the new version does not fit: PINNED, the call is NOT a HOT update and the row moves
//     to another page, in both states; the size and the WAL are logged;
//   - THE WAL, LOGGED ONLY: records, full-page images and bytes of the measured call (a warm-up
//     call, rolled back, comes first); why nothing about it is compared is at the end of the test.
//
// Also logged: each state's row width (pg_column_size), whose difference is the window in which a
// nearly full page fits the new version in one state and not in the other -- the boundary the ADR
// names; it is not constructed here (a page's free space is not readable without an extension).
// Counted, not closed (ADR 0025 "Karar verilmedi").
func TestOpSuspendTenant_TheWidthChannelsAreMeasured(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	for _, st := range opSuspStates[:2] {
		for _, full := range []bool{false, true} {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			f := opSuspTenant(t, ctx, sp, fmt.Sprintf("op15 width %s full=%v %s", st.name, full, tok), st.at, st.reason)
			width := opInt(t, ctx, sp, `SELECT pg_column_size(t.*) FROM public.tenants t WHERE id = $1`, f.id)
			page, fills := opBlockOf(t, ctx, sp, f.id), 0
			if full {
				for ; fills < 400; fills++ {
					in, err := sp.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := in.Exec(ctx, `UPDATE public.tenants SET name = name WHERE id = $1`, f.id); err != nil {
						t.Fatal(err)
					}
					if opBlockOf(t, ctx, in, f.id) != page {
						if err := in.Rollback(ctx); err != nil {
							t.Fatal(err)
						}
						break
					}
					if err := in.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if fills == 400 || opBlockOf(t, ctx, sp, f.id) != page {
					t.Fatalf("PREMISE (%s): the page was not filled (%d versions)", st.name, fills)
				}
			}
			// A WARM-UP of the same call, rolled back: the first change of a page after a checkpoint
			// writes a full-page image, and without it the measured call's WAL bytes would be the
			// images' (measured: 3-4 FPI, 21-26 kB) rather than the records'. A rolled-back version
			// stays on its page, so the full page is still full after it.
			warm, err := sp.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			opWALOf(t, ctx, warm, suspendTenantSQL, hash, f.id, "customer_request")
			if err := warm.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if full && opBlockOf(t, ctx, sp, f.id) != page {
				t.Fatalf("PREMISE (%s): the fixture left its page before the measured call", st.name)
			}
			hot0, size0 := opHOT(t, ctx, sp), opInt(t, ctx, sp, `SELECT pg_relation_size('public.tenants')`)
			records, fpi, bytes := opWALOf(t, ctx, sp, suspendTenantSQL, hash, f.id, "customer_request")
			hot, moved := opHOT(t, ctx, sp)-hot0, opBlockOf(t, ctx, sp, f.id) != page
			grew := opInt(t, ctx, sp, `SELECT pg_relation_size('public.tenants')`) - size0
			t.Logf("OP15-WIDTH: %s, full page=%v (%d versions to fill): row width %d bytes; the call: HOT %d, moved page %v, "+
				"table grew %d bytes, WAL %d record(s), %d FPI, %d bytes", st.name, full, fills, width, hot, moved, grew, records, fpi, bytes)
			if full && (hot != 0 || !moved) {
				t.Errorf("%s, full page: the call was HOT %d and moved %v; measured was a non-HOT update to another page -- update ADR 0021's OP-15 note", st.name, hot, moved)
			}
			if records == 0 {
				t.Errorf("%s, full page=%v: EXPLAIN (ANALYZE, WAL) read no WAL record for the call; the channel's probe is blind", st.name, full)
			}
			if err := sp.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	// THE WAL IS LOGGED, NOT COMPARED -- measured (2026-10-08, at 00035): alone on the database the
	// call wrote 12 records on a page with room and 15 on a full one for both previous states (at
	// 00063, 2026-10-20: 15 and 14 with room, 16 and 16 full -- the two states differ), but under `go test ./...`
	// the same comparison read 13 against 12 and 15 against 16 (records of pruning and full-page
	// images that depend on what other sessions left on the pages), and every measured call
	// carried one to three full-page images even after two warm-ups. So the record's own
	// prefix/suffix difference -- the channel ADR 0025 limit 4 names -- is below the noise here:
	// counted, not closed (ADR 0021's OP-15 note).
}

// TestOpReadTenantDetail_ReturnsTheNamedTenantsSuspension: the two re-created reads return the
// suspension at the end of each row (00063 section 7) -- through the shipped statements with this
// file's scans AND the product's scans (readTenantDetail, readTenants):
//   - the overview of a tenant suspended for non-payment returns its time and code; of a tenant
//     not suspended, nil and ""; of one on a security hold, its own -- never a neighbour's (the
//     belt: the columns come from the row `t.id = p_tenant_id` names);
//   - a code this build does not know (written by the owner, the reason CHECK dropped inside a
//     savepoint) comes back VERBATIM with Known() false -- never as a known one;
//   - the list returns suspended_at for each row of a search that finds the three.
func TestOpReadTenantDetail_ReturnsTheNamedTenantsSuspension(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tok := opToken(t)
	xact := opCommittedXact(t, ctx)
	fixtures := []opSuspFixture{
		opSuspTenant(t, ctx, tx, "op15 read "+tok+" A", "2025-04-05 06:07:08.123456+00", "non_payment"),
		opSuspTenant(t, ctx, tx, "op15 read "+tok+" B", "", ""),
		opSuspTenant(t, ctx, tx, "op15 read "+tok+" C", "2025-07-08 09:10:11.654321+00", "security_hold"),
	}
	same := func(o TenantOverview, f opSuspFixture) bool {
		return o.ID == f.id && opSameTime(o.SuspendedAt, f.at) && string(o.SuspensionReason) == f.reason
	}
	for _, f := range fixtures {
		rows, err := opReadDetailHead(t, ctx, tx, hash, opForgeDetail(t, ctx, tx, session, a.id, f.id, xact), f.id)
		if err != nil || len(rows) != 1 || !same(rows[0], f) {
			t.Errorf("%s: the overview (this file's scan) read %d row(s), err %v; want its own suspension", f.name, len(rows), err)
		}
		var o TenantOverview
		raw := opForgeDetail(t, ctx, tx, session, a.id, f.id, xact)
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			var e error
			o, e = readTenantDetail(ctx, sp, hash, readTicket{v: &raw}, f.id)
			return e
		}); err != nil || !same(o, f) {
			t.Errorf("%s: the overview (the product's scan) read %+v, err %v; want its own suspension", f.name, o, err)
		}
	}

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`ALTER TABLE tenants DROP CONSTRAINT ` + opSuspensionReasonName,
		`UPDATE tenants SET suspension_reason = 'zz_later_reason' WHERE id = '` + fixtures[0].id.String() + `'`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("plant a later build's reason: %v", err)
		}
	}
	var o TenantOverview
	laterRaw := opForgeDetail(t, ctx, sp, session, a.id, fixtures[0].id, xact)
	if err := opAs(t, ctx, sp, "tappa_operator", func(q pgx.Tx) error {
		var e error
		o, e = readTenantDetail(ctx, q, hash, readTicket{v: &laterRaw}, fixtures[0].id)
		return e
	}); err != nil || o.SuspensionReason != "zz_later_reason" || o.SuspensionReason.Known() {
		t.Errorf("a reason this build does not know reads %q (known %v), err %v; want it verbatim and unknown", o.SuspensionReason, o.SuspensionReason.Known(), err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	query := "op15 read " + tok
	list, err := opReadTenantsHead(t, ctx, tx, hash, opForgeTenants(t, ctx, tx, session, a.id, query, 1, 10, xact), query, 1, 10)
	if err != nil || len(list) != 3 {
		t.Fatalf("the list read %d row(s), err %v; want the three fixtures", len(list), err)
	}
	var product []TenantSummary
	listRaw := opForgeTenants(t, ctx, tx, session, a.id, query, 1, 10, xact)
	if err := opAs(t, ctx, tx, "tappa_operator", func(q pgx.Tx) error {
		var e error
		product, e = readTenants(ctx, q, hash, readTicket{v: &listRaw}, TenantListQuery{Search: query, Number: 1, Size: 10})
		return e
	}); err != nil || len(product) != 3 {
		t.Fatalf("the list (the product's scan) read %d row(s), err %v", len(product), err)
	}
	for _, rows := range [][]TenantSummary{list, product} {
		for _, r := range rows {
			for _, f := range fixtures {
				if r.ID == f.id && !opSameTime(r.SuspendedAt, f.at) {
					t.Errorf("%s: the list's suspended_at is %v, want %v", f.name, r.SuspendedAt, f.at)
				}
			}
		}
	}
}
