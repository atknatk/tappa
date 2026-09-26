package db

// operatorfuncs_test.go -- the BEHAVIOUR proofs for the five op_* functions migration
// 00026 creates (M10 OP-5; ADR 0021 §6 "Davranış"). The catalogue half and the shared
// helpers (opTx, opAs, opWant, ...) are in operatorschema_test.go.
//
// Every test but two runs in a single rolled-back REPEATABLE READ transaction as the
// owner and calls the functions AS tappa_operator (SET LOCAL SESSION AUTHORIZATION),
// the only role that may. The two that need more than one session commit:
//   - the concurrency test: "exactly one winner" needs the winner to COMMIT while the
//     others wait on its row lock, so it leaves one operator account, one session and
//     one audit row behind per run (measured: 7/7/7 -> 8/8/8) -- named by a random
//     uuid, inert, and on an append-only table nothing can clean (the price
//     invites_test.go's race already pays; M3-02's note);
//   - the lock-oracle test: it commits three fixture accounts so a second session can
//     see them, rolls back both probing transactions (so no audit row is committed)
//     and deletes the accounts when it ends (measured: 8/8/8 -> 8/8/8).
//
// Time is never waited for when it can be written: expiry is proven by writing the
// row's own timestamps into the past (ADR 0021 §2 i, OP-6's "clock-injected" DB tests).
// The two places that DO sleep are the ones whose point is that a clock kept running
// inside one transaction or one statement.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ------------------------------------------------------------------ fixtures --

type opAccount struct {
	id    uuid.UUID
	email string
}

// opNewActive inserts an ACTIVE operator as the owner (the only role that may -- ADR
// 0020 §6) with synthetic credentials that satisfy 00026's CHECKs.
func opNewActive(t *testing.T, ctx context.Context, tx pgx.Tx) opAccount {
	t.Helper()
	a := opAccount{id: uuid.New()}
	a.email = "op-" + a.id.String()[:12] + "@example.test"
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed)
		VALUES ($1, $2, 'fixture operator', 'active', $3, $4)`,
		a.id, a.email, opFakeDigest("a"), opFakeSealed(0x5A)); err != nil {
		t.Fatalf("insert active operator: %v", err)
	}
	return a
}

// opNewPending inserts a PENDING operator whose enrollment token was issued issuedAgo
// ago and lives ttl, from ONE clock read. It returns the raw token; the table holds
// only its keyless SHA-256.
func opNewPending(t *testing.T, ctx context.Context, q opQuerier, issuedAgo, ttl time.Duration) (opAccount, string) {
	t.Helper()
	a := opAccount{id: uuid.New()}
	a.email = "op-" + a.id.String()[:12] + "@example.test"
	raw, hash := opRandToken(t)
	if _, err := q.Exec(ctx, `
		WITH c AS (SELECT clock_timestamp() - make_interval(secs => $4) AS issued)
		INSERT INTO platform_admins (id, email, display_name, status, enroll_token_hash,
		                             enroll_issued_at, enroll_expires_at)
		SELECT $1, $2, 'fixture operator', 'pending', $3, c.issued, c.issued + make_interval(secs => $5)
		  FROM c`,
		a.id, a.email, hash, issuedAgo.Seconds(), ttl.Seconds()); err != nil {
		t.Fatalf("insert pending operator: %v", err)
	}
	return a, raw
}

// opNewSession inserts a session row directly as the owner -- the way to reach states
// no op_* produces (no MFA) and to age a session by rewriting its timestamps.
func opNewSession(t *testing.T, ctx context.Context, tx pgx.Tx, admin uuid.UUID, mfa bool) (string, uuid.UUID) {
	t.Helper()
	hash := opRandHex(t)
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO platform_sessions (admin_id, token_hash, mfa_verified_at)
		VALUES ($1, $2, CASE WHEN $3 THEN clock_timestamp() END)
		RETURNING id`, admin, hash, mfa).Scan(&id); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return hash, id
}

// opStep is the DATABASE's current 30-second TOTP step -- the value op_open_session and
// op_complete_enrollment bind to -- read at least three seconds before the next
// boundary, so a test that sends cur-1 cannot be moved to cur-2 by the clock ticking
// between the read and the call.
func opStep(t *testing.T, ctx context.Context, q opQuerier) int64 {
	t.Helper()
	for {
		var step int64
		var left float64
		if err := q.QueryRow(ctx, `
			SELECT floor(extract(epoch FROM clock_timestamp()) / 30)::bigint,
			       (30 - mod(extract(epoch FROM clock_timestamp()), 30))::float8`).Scan(&step, &left); err != nil {
			t.Fatalf("read the database's TOTP step: %v", err)
		}
		if left > 3 {
			return step
		}
		time.Sleep(time.Duration((left + 0.3) * float64(time.Second)))
	}
}

// opAudit counts the audit table in the test's snapshot: under REPEATABLE READ only
// this transaction's writes move it, so a delta of 0 means "this call wrote nothing".
func opAudit(t *testing.T, ctx context.Context, tx pgx.Tx) int64 {
	t.Helper()
	return opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log`)
}

func opOpen(t *testing.T, ctx context.Context, tx pgx.Tx, admin uuid.UUID, sessionHash string, step int64) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_open_session($1, $2, $3)`, admin, sessionHash, step)
}

func opRecord(t *testing.T, ctx context.Context, tx pgx.Tx, kind any, email any, admin any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_record_auth_event($1, $2, $3)`, kind, email, admin)
}

func opEnroll(t *testing.T, ctx context.Context, tx pgx.Tx, admin uuid.UUID, token, digest string, sealed []byte, step int64, sessionHash string) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator",
		`SELECT public.op_complete_enrollment($1, $2, $3, $4, $5, $6)`, admin, token, digest, sealed, step, sessionHash)
}

// ------------------------------------------------------------ op_touch_session --

// TestOpTouchSession_OnlyALiveMFASessionOfAnActiveOperator is ADR 0021 §6 "Davranış —
// oturum": the ONE predicate accepts a live session and advances last_used_at, and
// refuses -- with 28000, writing nothing -- every dead shape. The boundary cases are
// the positive half: a session one minute inside each window must pass, so a mutation
// that widens or narrows a window by more than that turns a case red.
func TestOpTouchSession_OnlyALiveMFASessionOfAnActiveOperator(t *testing.T) {
	ctx, tx := opTx(t)

	touch := func(hash string) (uuid.UUID, uuid.UUID, error) {
		t.Helper()
		var sid, aid uuid.UUID
		err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, `SELECT session_id, admin_id FROM public.op_touch_session($1)`, hash).Scan(&sid, &aid)
		})
		return sid, aid, err
	}
	lastUsed := func(id uuid.UUID) time.Time {
		t.Helper()
		var at time.Time
		if err := tx.QueryRow(ctx, `SELECT last_used_at FROM platform_sessions WHERE id = $1`, id).Scan(&at); err != nil {
			t.Fatalf("read last_used_at: %v", err)
		}
		return at
	}

	// ACCEPTED: fresh, and one minute inside both windows.
	a := opNewActive(t, ctx, tx)
	for _, c := range []struct{ name, age, idle string }{
		{"fresh", "0 seconds", "0 seconds"},
		{"7h59m old, idle 29m", "7 hours 59 minutes", "29 minutes"},
	} {
		hash, id := opNewSession(t, ctx, tx, a.id, true)
		if _, err := tx.Exec(ctx, `UPDATE platform_sessions SET created_at = clock_timestamp() - $2::interval,
		                               last_used_at = clock_timestamp() - $3::interval WHERE id = $1`, id, c.age, c.idle); err != nil {
			t.Fatalf("age the session: %v", err)
		}
		before := lastUsed(id)
		sid, aid, err := touch(hash)
		if err != nil {
			t.Fatalf("%s: a live MFA session of an active operator was refused: %v", c.name, err)
		}
		if sid != id || aid != a.id {
			t.Fatalf("%s: touch returned session=%s admin=%s, want %s / %s", c.name, sid, aid, id, a.id)
		}
		if after := lastUsed(id); !after.After(before) {
			t.Fatalf("%s: last_used_at did not advance (%s -> %s); the idle window would never renew", c.name, before, after)
		}
	}

	// REFUSED: every dead shape, 28000, nothing written.
	for _, c := range opDeadSessions(t, ctx, tx) {
		var before time.Time
		if c.id != uuid.Nil {
			before = lastUsed(c.id)
		}
		auditBefore := opAudit(t, ctx, tx)
		_, _, err := touch(c.hash)
		opWant(t, err, sqlstateInvalidAuthorization, "op_touch_session: "+c.name)
		if c.id != uuid.Nil && !lastUsed(c.id).Equal(before) {
			t.Errorf("%s: a REFUSED touch moved last_used_at", c.name)
		}
		if d := opAudit(t, ctx, tx) - auditBefore; d != 0 {
			t.Errorf("%s: a refused touch left %d audit row(s)", c.name, d)
		}
	}
}

// opDeadSession is one session no op_* may accept.
type opDeadSession struct {
	name string
	hash string
	id   uuid.UUID // uuid.Nil for the unknown hash
}

// opDeadSessions builds ADR 0021 §6's dead-session table -- absolute lifetime over,
// idle over, no MFA stamp, revoked, operator disabled, unknown hash -- as owner-written
// rows (time is written into the past, not waited for). op_touch_session's test and
// op_close_session's test drive the SAME table: close resolves its session through the
// touch predicate, and a close that looked the session up any other way would log out
// (and write a logout row for) a session the predicate calls dead.
func opDeadSessions(t *testing.T, ctx context.Context, tx pgx.Tx) []opDeadSession {
	t.Helper()
	a := opNewActive(t, ctx, tx)
	disabled := opNewActive(t, ctx, tx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	var out []opDeadSession
	h, id := opNewSession(t, ctx, tx, a.id, true)
	exec(`UPDATE platform_sessions SET created_at = clock_timestamp() - interval '8 hours 1 minute',
	          last_used_at = clock_timestamp() - interval '1 minute' WHERE id = $1`, id)
	out = append(out, opDeadSession{"absolute lifetime over (8h01m, used a minute ago)", h, id})
	h, id = opNewSession(t, ctx, tx, a.id, true)
	exec(`UPDATE platform_sessions SET last_used_at = clock_timestamp() - interval '31 minutes' WHERE id = $1`, id)
	out = append(out, opDeadSession{"idle over (31m)", h, id})
	h, id = opNewSession(t, ctx, tx, a.id, false)
	out = append(out, opDeadSession{"no MFA stamp", h, id})
	h, id = opNewSession(t, ctx, tx, a.id, true)
	exec(`UPDATE platform_sessions SET revoked_at = clock_timestamp() WHERE id = $1`, id)
	out = append(out, opDeadSession{"revoked", h, id})
	h, id = opNewSession(t, ctx, tx, disabled.id, true)
	exec(`UPDATE platform_admins SET status = 'disabled' WHERE id = $1`, disabled.id)
	out = append(out, opDeadSession{"operator disabled", h, id})
	out = append(out, opDeadSession{"unknown hash", opRandHex(t), uuid.Nil})
	return out
}

// TestOpTouchSession_IdleWindowIsTheWallClockInsideOneStatement is ADR 0021 §2 (vii) for
// the session predicate: the idle window runs out WHILE one statement (a DO block)
// sleeps, and the touch at the end of that statement must see it. clock_timestamp()
// does; now() (transaction start) and statement_timestamp() (statement start) would
// call the session fresh -- which is why the sleep is INSIDE the DO (ADR 0021's DO
// measurement). The control has the same shape with room to spare and must pass.
func TestOpTouchSession_IdleWindowIsTheWallClockInsideOneStatement(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)

	run := func(margin string) error {
		t.Helper()
		hash, id := opNewSession(t, ctx, tx, a.id, true)
		if _, err := tx.Exec(ctx, `UPDATE platform_sessions
		                              SET last_used_at = clock_timestamp() - interval '30 minutes' + $2::interval
		                            WHERE id = $1`, id, margin); err != nil {
			t.Fatalf("set the idle clock: %v", err)
		}
		// The hash is a random test value, hex only; it is spliced because DO takes no
		// parameters.
		return opExecAs(t, ctx, tx, "tappa_operator", fmt.Sprintf(
			`DO $d$ BEGIN PERFORM pg_sleep(2.5); PERFORM public.op_touch_session('%s'); END $d$`, hash))
	}
	if err := run("20 seconds"); err != nil {
		t.Fatalf("CONTROL: a session with 20 s of idle window left was refused after a 2.5 s sleep: %v", err)
	}
	opWant(t, run("1 second"), sqlstateInvalidAuthorization,
		"a session whose idle window ran out DURING the statement (a frozen clock would call it fresh)")
}

// -------------------------------------------------------- op_record_auth_event --

// TestOpRecordAuthEvent_ClosedSetNoActorNoAddress is ADR 0021 §1's op_record_auth_event
// contract: exactly one row per accepted call, only the five failure kinds, no session,
// no actor, never the address (not even a hash of it) -- the target is the database's
// own lookup, by address (case-insensitive) or by id, and an unknown id is not an
// error. totp_failed moves the account's counter in the same call; nothing else does.
func TestOpRecordAuthEvent_ClosedSetNoActorNoAddress(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)

	failures := func() int64 {
		return opInt(t, ctx, tx, `SELECT totp_failures FROM platform_admins WHERE id = $1`, a.id)
	}
	rowJSON := func(kind string, target any) string {
		t.Helper()
		var s string
		if err := tx.QueryRow(ctx, `
			SELECT row_to_json(l)::text FROM operator_audit_log l
			 WHERE kind = $1 AND target_admin_id IS NOT DISTINCT FROM $2::uuid
			 ORDER BY at DESC LIMIT 1`, kind, target).Scan(&s); err != nil {
			t.Fatalf("read the %s row: %v", kind, err)
		}
		return s
	}
	checkRow := func(kind string, target any, secret string) {
		t.Helper()
		var session, actor, tenant, scope, page, size bool
		var detail string
		if err := tx.QueryRow(ctx, `
			SELECT session_id IS NULL, actor_admin_id IS NULL, target_tenant_id IS NULL,
			       target_scope IS NULL, page_number IS NULL, page_size IS NULL, detail::text
			  FROM operator_audit_log
			 WHERE kind = $1 AND target_admin_id IS NOT DISTINCT FROM $2::uuid
			 ORDER BY at DESC LIMIT 1`, kind, target).Scan(&session, &actor, &tenant, &scope, &page, &size, &detail); err != nil {
			t.Fatalf("read the %s row: %v", kind, err)
		}
		if !session || !actor {
			t.Errorf("%s: the row claims a session or an actor (ADR 0021 §1: aktör iddia etmez)", kind)
		}
		if !tenant || !scope || !page || !size || detail != "{}" {
			t.Errorf("%s: the row carries a target or detail it has no business carrying: tenant-null=%v scope-null=%v page-null=%v size-null=%v detail=%s",
				kind, tenant, scope, page, size, detail)
		}
		if secret != "" && strings.Contains(strings.ToLower(rowJSON(kind, target)), strings.ToLower(strings.SplitN(secret, "@", 2)[0])) {
			t.Errorf("%s: the stored row carries the address it was given", kind)
		}
	}

	// The five kinds, by ADDRESS given in a different case: citext equality through
	// OPERATOR(public.=) under the pinned search_path (ADR 0002 M6-01).
	for _, kind := range []string{"login_failed", "unknown_email", "totp_failed", "locked", "enrollment_failed"} {
		before, fBefore := opAudit(t, ctx, tx), failures()
		if err := opRecord(t, ctx, tx, kind, strings.ToUpper(a.email), nil); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if d := opAudit(t, ctx, tx) - before; d != 1 {
			t.Fatalf("%s wrote %d rows, want exactly 1", kind, d)
		}
		checkRow(kind, a.id, a.email)
		want := fBefore
		if kind == "totp_failed" {
			want++
		}
		if got := failures(); got != want {
			t.Errorf("%s: totp_failures %d -> %d, want %d (only totp_failed counts, and in the same call)", kind, fBefore, got, want)
		}
	}

	// An address nobody has: a row, with nothing about the address.
	stranger := "stranger-" + uuid.NewString()[:8] + "@example.test"
	before := opAudit(t, ctx, tx)
	if err := opRecord(t, ctx, tx, "unknown_email", stranger, nil); err != nil {
		t.Fatalf("unknown address: %v", err)
	}
	if d := opAudit(t, ctx, tx) - before; d != 1 {
		t.Fatalf("unknown address wrote %d rows, want 1", d)
	}
	if s := rowJSON("unknown_email", nil); strings.Contains(strings.ToLower(s), "stranger-") {
		t.Errorf("the unknown-address row carries the address: %s", s)
	}

	// D3 -- THE KIND IS GO'S VIEW, THE TARGET IS THE DATABASE'S. Go's login lookup sees
	// only ACTIVE accounts (the RLS policy), so it reports the address of a pending or a
	// disabled operator as unknown_email; the row still names that account, because the
	// definer's lookup sees every status. "unknown_email + target" = an address of an
	// account that cannot log in; "unknown_email, no target" = no such account.
	pending, _ := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	disabledAcc := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`, disabledAcc.id); err != nil {
		t.Fatalf("disable: %v", err)
	}
	for _, acc := range []opAccount{pending, disabledAcc} {
		var visible int64
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, `SELECT count(*) FROM public.platform_admins WHERE email = $1`, acc.email).Scan(&visible)
		}); err != nil {
			t.Fatalf("login lookup: %v", err)
		}
		if visible != 0 {
			t.Fatalf("the login lookup sees a non-active account (%d row); the premise of this case is gone", visible)
		}
		if err := opRecord(t, ctx, tx, "unknown_email", acc.email, nil); err != nil {
			t.Fatalf("unknown_email for a non-active account: %v", err)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'unknown_email' AND target_admin_id = $1`, acc.id); n != 1 {
			t.Errorf("unknown_email for a non-active account's address: %d row(s) name the account, want 1 (the target is the database's lookup, not Go's)", n)
		}
		checkRow("unknown_email", acc.id, acc.email)
	}
	// ...and a totp_failed against a non-active account does NOT move its counter (D2:
	// only an active account is locked, and only an active one is row-locked by the call).
	for _, acc := range []opAccount{pending, disabledAcc} {
		if err := opRecord(t, ctx, tx, "totp_failed", nil, acc.id); err != nil {
			t.Fatalf("totp_failed for a non-active account: %v", err)
		}
		if f := opInt(t, ctx, tx, `SELECT totp_failures FROM platform_admins WHERE id = $1`, acc.id); f != 0 {
			t.Errorf("totp_failed moved a non-active account's counter to %d; the UPDATE is not filtered by status", f)
		}
	}

	// By ID: a real one is the target; a made-up one is NOT a foreign-key error (that
	// would answer "does this operator exist" -- a rolled-back existence oracle).
	if err := opRecord(t, ctx, tx, "enrollment_failed", nil, a.id); err != nil {
		t.Fatalf("by id: %v", err)
	}
	before = opAudit(t, ctx, tx)
	if err := opRecord(t, ctx, tx, "enrollment_failed", nil, uuid.New()); err != nil {
		t.Fatalf("an unknown id must not be an error (existence oracle): %v", err)
	}
	if d := opAudit(t, ctx, tx) - before; d != 1 {
		t.Fatalf("an unknown id wrote %d rows, want 1", d)
	}

	// OUTSIDE the closed set: refused, nothing written. 'login'/'logout'/'enrollment'
	// are real kinds of the table -- the ones that need a session -- and are exactly
	// what this function must never mint.
	for _, kind := range []any{"login", "logout", "enrollment", "session_refused", "", nil} {
		before := opAudit(t, ctx, tx)
		opWant(t, opRecord(t, ctx, tx, kind, a.email, nil), sqlstateInvalidParameter, fmt.Sprintf("kind %v", kind))
		if d := opAudit(t, ctx, tx) - before; d != 0 {
			t.Errorf("kind %v: a refused call left %d row(s)", kind, d)
		}
	}
}

// TestOpRecordAuthEvent_NoLockOracleOnAnInvisibleAccount is D2 of the round-2 audit.
// A DSN holder keeps one op_record_auth_event('totp_failed', NULL, <id>) open; a second
// session making the same call must not WAIT for an account tappa_operator cannot see
// (pending, disabled): waiting versus returning at once was an existence oracle for
// exactly the accounts the login lookup's policy hides. The counter UPDATE is filtered
// to ACTIVE accounts, so nothing else row-locks them. The positive control is an active
// account, whose counter row IS locked -- proving the probe sees contention when there is
// some; tappa_operator may SELECT active accounts anyway, so that is not an oracle.
//
// It needs two sessions and therefore COMMITTED fixtures: three accounts, removed by
// the owner when the test ends. Both probing transactions are rolled back, so no audit
// row is committed and the removal meets no foreign key.
func TestOpRecordAuthEvent_NoLockOracleOnAnInvisibleAccount(t *testing.T) {
	dsn := opOwnerDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	connect := func() *pgx.Conn {
		t.Helper()
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(func() {
			if err := c.Close(context.Background()); err != nil {
				t.Logf("close: %v", err)
			}
		})
		return c
	}
	owner, holder, prober := connect(), connect(), connect()

	pending, _ := opNewPending(t, ctx, owner, 0, 30*time.Minute)
	ids := map[string]uuid.UUID{"pending": pending.id}
	for _, status := range []string{"disabled", "active"} {
		id := uuid.New()
		if _, err := owner.Exec(ctx, `
			INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed)
			VALUES ($1, $2, 'lock oracle', $3, $4, $5)`,
			id, "lockoracle-"+id.String()[:12]+"@example.test", status, opFakeDigest("l"), opFakeSealed(7)); err != nil {
			t.Fatalf("insert %s account: %v", status, err)
		}
		ids[status] = id
	}
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `DELETE FROM platform_admins WHERE id = ANY($1)`,
			[]uuid.UUID{ids["pending"], ids["disabled"], ids["active"]}); err != nil {
			t.Errorf("remove the lock-oracle fixtures: %v", err)
		}
	})

	probe := func(id uuid.UUID) error {
		t.Helper()
		htx, err := holder.Begin(ctx)
		if err != nil {
			t.Fatalf("holder begin: %v", err)
		}
		defer func() {
			if err := htx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("holder rollback: %v", err)
			}
		}()
		for _, q := range []string{`SET LOCAL SESSION AUTHORIZATION tappa_operator`} {
			if _, err := htx.Exec(ctx, q); err != nil {
				t.Fatalf("holder: %v", err)
			}
		}
		if _, err := htx.Exec(ctx, `SELECT public.op_record_auth_event('totp_failed', NULL, $1)`, id); err != nil {
			t.Fatalf("holder's call: %v", err)
		}
		ptx, err := prober.Begin(ctx)
		if err != nil {
			t.Fatalf("prober begin: %v", err)
		}
		defer func() {
			if err := ptx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("prober rollback: %v", err)
			}
		}()
		for _, q := range []string{`SET LOCAL lock_timeout = '300ms'`, `SET LOCAL SESSION AUTHORIZATION tappa_operator`} {
			if _, err := ptx.Exec(ctx, q); err != nil {
				t.Fatalf("prober: %v", err)
			}
		}
		_, err = ptx.Exec(ctx, `SELECT public.op_record_auth_event('totp_failed', NULL, $1)`, id)
		return err
	}

	if code, _ := opCode(probe(ids["active"])); code != sqlstateLockNotAvailable {
		t.Fatalf("CONTROL: a second call on a held ACTIVE account answered %q, want %s (lock_timeout) -- the probe cannot see contention, so the cases below prove nothing", code, sqlstateLockNotAvailable)
	}
	ids["unknown"] = uuid.New() // never inserted: the "returns at once" baseline
	for _, which := range []string{"pending", "disabled", "unknown"} {
		if err := probe(ids[which]); err != nil {
			code, _ := opCode(err)
			t.Errorf("a second call on a held %s account failed with %q: it waited on a row lock, which tells a DSN holder the account exists although the login lookup hides it", which, code)
		}
	}
}

// ------------------------------------------------------------- op_open_session --

// TestOpOpenSession_StepIsFreshAndBoundToTheWallClock is ADR 0021 §6 "Davranış — TOTP
// adımı ve oturum doğuşu": cur-1, cur and cur+1 open a session; a poisoned step (the
// bigint ceiling, cur±2, a year ahead) is refused AND does not lock the account (the
// next honest step works); a replayed step is refused with no session and no origin row.
func TestOpOpenSession_StepIsFreshAndBoundToTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	cur := opStep(t, ctx, tx)

	sessions := func(id uuid.UUID) int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, id)
	}
	logins := func(id uuid.UUID) int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'login' AND actor_admin_id = $1`, id)
	}
	lastStep := func(id uuid.UUID) int64 {
		return opInt(t, ctx, tx, `SELECT totp_last_step FROM platform_admins WHERE id = $1`, id)
	}

	for _, poison := range []struct {
		name string
		step int64
	}{
		{"bigint ceiling", math.MaxInt64},
		{"cur+2", cur + 2},
		{"cur-2", cur - 2},
		{"cur + one year", cur + 365*24*120},
	} {
		auditBefore := opAudit(t, ctx, tx)
		opWant(t, opOpen(t, ctx, tx, a.id, opRandHex(t), poison.step), sqlstateInvalidAuthorization, "poisoned step "+poison.name)
		if got := lastStep(a.id); got != 0 {
			t.Fatalf("poisoned step %s moved totp_last_step to %d; one call would lock the account for good", poison.name, got)
		}
		if sessions(a.id) != 0 || opAudit(t, ctx, tx) != auditBefore {
			t.Fatalf("poisoned step %s left a session or an audit row", poison.name)
		}
	}

	// The honest step still works: the poison did not lock anything.
	first := opRandHex(t)
	if err := opOpen(t, ctx, tx, a.id, first, cur); err != nil {
		t.Fatalf("the current step was refused after the poisoned attempts: %v", err)
	}
	if lastStep(a.id) != cur || sessions(a.id) != 1 || logins(a.id) != 1 {
		t.Fatalf("after one login: last_step=%d sessions=%d login rows=%d, want %d/1/1", lastStep(a.id), sessions(a.id), logins(a.id), cur)
	}
	var mfa, sameSession bool
	if err := tx.QueryRow(ctx, `
		SELECT s.mfa_verified_at IS NOT NULL,
		       l.session_id = s.id
		  FROM platform_sessions s
		  JOIN operator_audit_log l ON l.kind = 'login' AND l.actor_admin_id = s.admin_id
		 WHERE s.token_hash = $1`, first).Scan(&mfa, &sameSession); err != nil {
		t.Fatalf("read the new session and its origin row: %v", err)
	}
	if !mfa || !sameSession {
		t.Fatalf("the new session: mfa stamped=%v, origin row names it=%v", mfa, sameSession)
	}

	// REPLAY: the same step again -- refused, no second session, no second origin row.
	opWant(t, opOpen(t, ctx, tx, a.id, opRandHex(t), cur), sqlstateInvalidAuthorization, "the same TOTP step twice")
	if sessions(a.id) != 1 || logins(a.id) != 1 {
		t.Fatalf("a replayed step left sessions=%d login rows=%d, want 1/1", sessions(a.id), logins(a.id))
	}

	// THE WINDOW: cur-1, cur, cur+1 each open a session on a fresh account.
	for _, d := range []int64{-1, 0, 1} {
		b := opNewActive(t, ctx, tx)
		if err := opOpen(t, ctx, tx, b.id, opRandHex(t), cur+d); err != nil {
			t.Errorf("step cur%+d was refused; ±1 step is the RFC 6238 window both clocks share: %v", d, err)
		}
	}
}

// TestOpOpenSession_RefusesPendingAndDisabledAccounts: only an ACTIVE operator gets a
// session (ADR 0021 §1), whatever step it brings.
func TestOpOpenSession_RefusesPendingAndDisabledAccounts(t *testing.T) {
	ctx, tx := opTx(t)
	pending, _ := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	disabled := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`, disabled.id); err != nil {
		t.Fatalf("disable: %v", err)
	}
	cur := opStep(t, ctx, tx)
	before := opAudit(t, ctx, tx)
	for _, c := range []struct {
		name string
		id   uuid.UUID
	}{{"pending", pending.id}, {"disabled", disabled.id}, {"no such account", uuid.New()}} {
		opWant(t, opOpen(t, ctx, tx, c.id, opRandHex(t), cur), sqlstateInvalidAuthorization, "op_open_session for a "+c.name+" account")
	}
	if d := opAudit(t, ctx, tx) - before; d != 0 {
		t.Errorf("refused openings left %d audit row(s)", d)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id IN ($1, $2)`, pending.id, disabled.id); n != 0 {
		t.Errorf("refused openings left %d session(s)", n)
	}
}

// TestOpOpenSession_TheLockIsTheAccountsCounterInTheSameUpdate is ADR 0021 §1's lock:
// op_record_auth_event('totp_failed') counts; at N (5) the window (15 min) opens and
// op_open_session refuses inside its own UPDATE; when the window has passed the next
// honest code works and resets the counter; N-1 failures do not lock; and audit rows
// written by any other route do NOT lock (the lock reads the counter, never the log).
// Both constants are pinned here, so a drift between the two functions' copies of them
// turns this red.
func TestOpOpenSession_TheLockIsTheAccountsCounterInTheSameUpdate(t *testing.T) {
	ctx, tx := opTx(t)
	fail := func(a opAccount, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			if err := opRecord(t, ctx, tx, "totp_failed", nil, a.id); err != nil {
				t.Fatalf("record totp_failed: %v", err)
			}
		}
	}
	state := func(a opAccount) (failures int64, lockedFor float64, locked bool) {
		t.Helper()
		var until *time.Time
		if err := tx.QueryRow(ctx, `SELECT totp_failures, totp_locked_until,
		                                   coalesce(extract(epoch FROM totp_locked_until - clock_timestamp())::float8, 0)
		                              FROM platform_admins WHERE id = $1`, a.id).Scan(&failures, &until, &lockedFor); err != nil {
			t.Fatalf("read the lock state: %v", err)
		}
		return failures, lockedFor, until != nil
	}

	// N-1 failures: not locked, and the success resets the counter.
	below := opNewActive(t, ctx, tx)
	fail(below, 4)
	if f, _, locked := state(below); f != 4 || locked {
		t.Fatalf("after 4 failures: counter=%d locked=%v, want 4/false", f, locked)
	}
	if err := opOpen(t, ctx, tx, below.id, opRandHex(t), opStep(t, ctx, tx)); err != nil {
		t.Fatalf("4 failures locked the account; the threshold is 5: %v", err)
	}
	if f, _, locked := state(below); f != 0 || locked {
		t.Fatalf("a successful login left counter=%d locked=%v, want 0/false", f, locked)
	}

	// N failures: locked for the window, and refused in the same UPDATE.
	locked := opNewActive(t, ctx, tx)
	fail(locked, 5)
	f, lockedFor, isLocked := state(locked)
	if f != 5 || !isLocked || lockedFor < 14*60+50 || lockedFor > 15*60 {
		t.Fatalf("after 5 failures: counter=%d locked=%v window=%.0fs, want 5/true/~900s", f, isLocked, lockedFor)
	}
	before := opAudit(t, ctx, tx)
	opWant(t, opOpen(t, ctx, tx, locked.id, opRandHex(t), opStep(t, ctx, tx)), sqlstateInvalidAuthorization, "a locked account with a valid step")
	if d := opAudit(t, ctx, tx) - before; d != 0 {
		t.Fatalf("the refused opening of a locked account left %d audit row(s)", d)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, locked.id); n != 0 {
		t.Fatalf("a locked account got %d session(s)", n)
	}
	// The window passes (written, not waited): the next honest code works and resets.
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_locked_until = clock_timestamp() - interval '1 second' WHERE id = $1`, locked.id); err != nil {
		t.Fatalf("expire the lock window: %v", err)
	}
	if err := opOpen(t, ctx, tx, locked.id, opRandHex(t), opStep(t, ctx, tx)); err != nil {
		t.Fatalf("the lock outlived its window: %v", err)
	}
	if f, _, isLocked := state(locked); f != 0 || isLocked {
		t.Fatalf("after the window and a success: counter=%d locked=%v, want 0/false", f, isLocked)
	}

	// D4 -- THE LOCK SHAPE, deliberate and pinned. The counter resets ONLY on success,
	// so once it has reached N every further failure re-opens the FULL window:
	// (a) after the window has passed, ONE failure locks again;
	relock := opNewActive(t, ctx, tx)
	fail(relock, 5)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_locked_until = clock_timestamp() - interval '1 second' WHERE id = $1`, relock.id); err != nil {
		t.Fatalf("expire the window: %v", err)
	}
	fail(relock, 1)
	if f, lockedFor, isLocked := state(relock); f != 6 || !isLocked || lockedFor < 14*60+50 || lockedFor > 15*60 {
		t.Fatalf("one failure after the window: counter=%d locked=%v window=%.0fs, want 6/true/~900s (counter resets only on success)", f, isLocked, lockedFor)
	}
	opWant(t, opOpen(t, ctx, tx, relock.id, opRandHex(t), opStep(t, ctx, tx)), sqlstateInvalidAuthorization, "the re-locked account")
	// (b) while locked, a failure EXTENDS the lock to a full window from now.
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_locked_until = clock_timestamp() + interval '5 minutes' WHERE id = $1`, relock.id); err != nil {
		t.Fatalf("shorten the window: %v", err)
	}
	fail(relock, 1)
	if _, lockedFor, _ := state(relock); lockedFor < 14*60+50 {
		t.Fatalf("a failure while locked left %.0fs on the lock, want a fresh ~900s (the window slides)", lockedFor)
	}

	// Audit rows are NOT the lock: ten totp_failed rows written around the counter.
	noisy := opNewActive(t, ctx, tx)
	for i := 0; i < 10; i++ {
		if _, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('totp_failed', $1)`, noisy.id); err != nil {
			t.Fatalf("write a bare audit row: %v", err)
		}
	}
	if err := opOpen(t, ctx, tx, noisy.id, opRandHex(t), opStep(t, ctx, tx)); err != nil {
		t.Fatalf("audit rows alone locked the account; the lock must read the account's counter (ADR 0021 §1): %v", err)
	}
}

// ------------------------------------------------------ op_complete_enrollment --

// TestOpCompleteEnrollment_OneRefusalForEveryDeadToken is ADR 0021 §6 "Davranış —
// enrollment": a wrong token, a token for another account, a token expired by the wall
// clock, a used token, an account that is already active and a poisoned first step are
// ALL refused with one SQLSTATE and one message, and none of them changes the account.
// The success path writes exactly what the ADR lists, from one statement.
func TestOpCompleteEnrollment_OneRefusalForEveryDeadToken(t *testing.T) {
	ctx, tx := opTx(t)
	p, rawP := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	q, _ := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	e, rawE := opNewPending(t, ctx, tx, 40*time.Minute, 30*time.Minute) // expired ten minutes ago
	r, rawR := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	// r: already ACTIVE with its token still unused -- only the status says no.
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'active', password_hash = $2, totp_secret_sealed = $3 WHERE id = $1`,
		r.id, opFakeDigest("b"), opFakeSealed(0x33)); err != nil {
		t.Fatalf("activate r: %v", err)
	}
	wrongRaw, _ := opRandToken(t)
	digest, sealed := opFakeDigest("c"), opFakeSealed(0x44)
	cur := opStep(t, ctx, tx)

	unchanged := func(a opAccount) {
		t.Helper()
		var status string
		var used, pw bool
		if err := tx.QueryRow(ctx, `SELECT status, enroll_used_at IS NOT NULL, password_hash IS NOT NULL
		                              FROM platform_admins WHERE id = $1`, a.id).Scan(&status, &used, &pw); err != nil {
			t.Fatalf("read account: %v", err)
		}
		if status != "pending" || used || pw {
			t.Fatalf("a REFUSED enrollment changed the account: status=%s used=%v digest=%v", status, used, pw)
		}
	}

	var messages []string
	refuse := func(what string, err error) {
		t.Helper()
		opWant(t, err, sqlstateInvalidAuthorization, what)
		_, msg := opCode(err)
		messages = append(messages, msg)
	}
	before := opAudit(t, ctx, tx)
	refuse("wrong token", opEnroll(t, ctx, tx, p.id, wrongRaw, digest, sealed, cur, opRandHex(t)))
	refuse("p's token on q's id", opEnroll(t, ctx, tx, q.id, rawP, digest, sealed, cur, opRandHex(t)))
	refuse("token expired by the wall clock", opEnroll(t, ctx, tx, e.id, rawE, digest, sealed, cur, opRandHex(t)))
	refuse("an already active account", opEnroll(t, ctx, tx, r.id, rawR, digest, sealed, cur, opRandHex(t)))
	for _, poison := range []int64{math.MaxInt64, cur + 2, cur - 2} {
		refuse("poisoned first step "+strconv.FormatInt(poison-cur, 10), opEnroll(t, ctx, tx, p.id, rawP, digest, sealed, poison, opRandHex(t)))
	}
	unchanged(p)
	unchanged(q)
	unchanged(e)
	if d := opAudit(t, ctx, tx) - before; d != 0 {
		t.Fatalf("refused enrollments left %d audit row(s)", d)
	}

	// SUCCESS.
	sessionHash := opRandHex(t)
	if err := opEnroll(t, ctx, tx, p.id, rawP, digest, sealed, cur, sessionHash); err != nil {
		t.Fatalf("a valid enrollment was refused: %v", err)
	}
	var status, gotDigest string
	var gotSealed []byte
	var step int64
	var used, login bool
	if err := tx.QueryRow(ctx, `SELECT status, password_hash, totp_secret_sealed, totp_last_step,
	                                   enroll_used_at IS NOT NULL, last_login_at IS NOT NULL
	                              FROM platform_admins WHERE id = $1`, p.id).Scan(&status, &gotDigest, &gotSealed, &step, &used, &login); err != nil {
		t.Fatalf("read the enrolled account: %v", err)
	}
	if status != "active" || gotDigest != digest || string(gotSealed) != string(sealed) || step != cur || !used || !login {
		t.Fatalf("after enrollment: status=%s digest-written=%v sealed-written=%v step=%d(want %d) used=%v login=%v",
			status, gotDigest == digest, string(gotSealed) == string(sealed), step, cur, used, login)
	}
	var mfa, origin bool
	if err := tx.QueryRow(ctx, `
		SELECT s.mfa_verified_at IS NOT NULL,
		       EXISTS (SELECT 1 FROM operator_audit_log l
		                WHERE l.kind = 'enrollment' AND l.session_id = s.id AND l.actor_admin_id = $1)
		  FROM platform_sessions s WHERE s.admin_id = $1 AND s.token_hash = $2`, p.id, sessionHash).Scan(&mfa, &origin); err != nil {
		t.Fatalf("read the first session: %v", err)
	}
	if !mfa || !origin {
		t.Fatalf("the enrollment's session: mfa=%v origin row=%v, want both", mfa, origin)
	}

	// USED: the same token again.
	refuse("a used token", opEnroll(t, ctx, tx, p.id, rawP, digest, sealed, cur, opRandHex(t)))

	// ONE message for every reason (§2 v 7's scope note: the refusal tells the token
	// holder nothing it could not already know, and it tells it the same thing).
	for i, m := range messages {
		if m != messages[0] {
			t.Errorf("refusal %d says %q, refusal 0 says %q: the reasons are distinguishable", i, m, messages[0])
		}
	}
}

// TestOpCompleteEnrollment_TheEnrollmentCodeCannotOpenASession: the first code's step is
// stored as totp_last_step, so the SAME code cannot open a session right after -- and
// the NEXT step can, which shows the refusal is the replay and not a general one.
func TestOpCompleteEnrollment_TheEnrollmentCodeCannotOpenASession(t *testing.T) {
	ctx, tx := opTx(t)
	p, raw := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	cur := opStep(t, ctx, tx)
	if err := opEnroll(t, ctx, tx, p.id, raw, opFakeDigest("d"), opFakeSealed(0x21), cur, opRandHex(t)); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	opWant(t, opOpen(t, ctx, tx, p.id, opRandHex(t), cur), sqlstateInvalidAuthorization, "logging in with the enrollment code's step")
	if err := opOpen(t, ctx, tx, p.id, opRandHex(t), cur+1); err != nil {
		t.Fatalf("CONTROL: the next step was refused too, so the refusal above proves nothing: %v", err)
	}
}

// TestOpCompleteEnrollment_AUsedTokenIsRefusedByTheFunctionItself pins the FUNCTION half
// of single use (B1), independently of the schema half. The CHECK
// platform_admins_pending_token_unused makes "pending AND used" unrepresentable, so to
// see what op_complete_enrollment does on its own the CHECK is dropped INSIDE this
// test's transaction (rolled back with it), the state an owner statement could
// otherwise produce -- a used account put back to pending without a new token -- is
// built, and the OLD token must still be refused. The control proves the state is the
// one that matters: with enroll_used_at cleared, the same call is accepted.
func TestOpCompleteEnrollment_AUsedTokenIsRefusedByTheFunctionItself(t *testing.T) {
	ctx, tx := opTx(t)
	// IF EXISTS: this test pins the FUNCTION layer alone, so it must still run -- and
	// still pass -- against a schema that has lost the CHECK (the other layer's mutant).
	if _, err := tx.Exec(ctx, `ALTER TABLE platform_admins DROP CONSTRAINT IF EXISTS platform_admins_pending_token_unused`); err != nil {
		t.Fatalf("drop the schema half inside the transaction: %v", err)
	}
	p, raw := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	cur := opStep(t, ctx, tx)
	if err := opEnroll(t, ctx, tx, p.id, raw, opFakeDigest("k"), opFakeSealed(0x12), cur, opRandHex(t)); err != nil {
		t.Fatalf("first enrollment: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'pending' WHERE id = $1`, p.id); err != nil {
		t.Fatalf("put the used account back to pending: %v", err)
	}
	var status string
	var used, live bool
	if err := tx.QueryRow(ctx, `SELECT status, enroll_used_at IS NOT NULL, enroll_expires_at > clock_timestamp()
	                              FROM platform_admins WHERE id = $1`, p.id).Scan(&status, &used, &live); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != "pending" || !used || !live {
		t.Fatalf("fixture: status=%s used=%v live=%v, want pending/true/true", status, used, live)
	}
	before := opAudit(t, ctx, tx)
	opWant(t, opEnroll(t, ctx, tx, p.id, raw, opFakeDigest("m"), opFakeSealed(0x13), opStep(t, ctx, tx), opRandHex(t)),
		sqlstateInvalidAuthorization, "the OLD, used token on an account put back to pending")
	if d := opAudit(t, ctx, tx) - before; d != 0 {
		t.Fatalf("the refused re-enrollment left %d audit row(s)", d)
	}
	// CONTROL, in a savepoint: clear enroll_used_at and the same token is accepted, so
	// the refusal above is the used-token predicate and not some other condition.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if _, err := sp.Exec(ctx, `UPDATE platform_admins SET enroll_used_at = NULL WHERE id = $1`, p.id); err != nil {
		t.Fatalf("clear used: %v", err)
	}
	if err := opEnroll(t, ctx, sp, p.id, raw, opFakeDigest("n"), opFakeSealed(0x14), opStep(t, ctx, sp), opRandHex(t)); err != nil {
		t.Fatalf("CONTROL: with enroll_used_at cleared the token was still refused, so the refusal above proves nothing: %v", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback control: %v", err)
	}
}

// TestOpCompleteEnrollment_ConcurrentRaceExactlyOneWinner is the single-use proof in the
// DATABASE (ADR 0021 §1, card D-4; emsal TestConsumeInvite_ConcurrentRaceExactlyOneWinner):
// N goroutines, each on its own pooled connection and transaction, present the SAME
// token at once. Exactly one commits; the other N-1 wait on its row lock, re-check the
// WHERE against the committed row and are refused with 28000. No mutex: that would be a
// single-process fiction.
//
// It COMMITS (see the file header): one account, one session and one enrollment row
// remain per run.
func TestOpCompleteEnrollment_ConcurrentRaceExactlyOneWinner(t *testing.T) {
	const racers = 24
	dsn := opOwnerDSN(t)
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	d, err := New(context.Background(), &config.Config{DatabaseURL: dsn + sep + "pool_max_conns=" + strconv.Itoa(racers+4)})
	if err != nil {
		t.Fatalf("owner race pool: %v", err)
	}
	t.Cleanup(d.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	account, raw := opNewPending(t, ctx, d.pool, 0, 30*time.Minute)
	cur := opStep(t, ctx, d.pool)
	digest, sealed := opFakeDigest("e"), opFakeSealed(0x77)

	var wins, refused, others int64
	var launched, done sync.WaitGroup
	launched.Add(racers)
	done.Add(racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		hash := opRandHex(t)
		go func() {
			defer done.Done()
			launched.Done()
			<-start
			tx, err := d.pool.Begin(ctx)
			if err != nil {
				atomic.AddInt64(&others, 1)
				t.Errorf("begin: %v", err)
				return
			}
			rollback := func() {
				if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
					t.Errorf("rollback: %v", err)
				}
			}
			if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
				atomic.AddInt64(&others, 1)
				t.Errorf("become tappa_operator: %v", err)
				rollback()
				return
			}
			_, callErr := tx.Exec(ctx, `SELECT public.op_complete_enrollment($1, $2, $3, $4, $5, $6)`,
				account.id, raw, digest, sealed, cur, hash)
			if callErr == nil {
				if err := tx.Commit(ctx); err != nil {
					atomic.AddInt64(&others, 1)
					t.Errorf("commit the winner: %v", err)
					return
				}
				atomic.AddInt64(&wins, 1)
				return
			}
			rollback()
			if code, _ := opCode(callErr); code == sqlstateInvalidAuthorization {
				atomic.AddInt64(&refused, 1)
				return
			}
			atomic.AddInt64(&others, 1)
			t.Errorf("a racer failed with something other than the refusal: %v", callErr)
		}()
	}
	launched.Wait()
	close(start)
	done.Wait()

	if wins != 1 || refused != racers-1 || others != 0 {
		t.Fatalf("winners=%d refused=%d other=%d, want 1/%d/0 (single use must be one conditional statement)", wins, refused, others, racers-1)
	}
	if n := opInt(t, ctx, d.pool, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, account.id); n != 1 {
		t.Fatalf("the race produced %d sessions, want 1", n)
	}
	if n := opInt(t, ctx, d.pool, `SELECT count(*) FROM operator_audit_log WHERE kind = 'enrollment' AND actor_admin_id = $1`, account.id); n != 1 {
		t.Fatalf("the race produced %d enrollment rows, want 1", n)
	}
}

// ------------------------------------------------------------ op_close_session --

// TestOpCloseSession_RevokesOnceAndWritesOneLogoutRow: logout revokes and writes its
// row in one statement; a second logout of the same session is refused (the touch sees
// the revocation) and adds no row; the session is dead for every later touch.
func TestOpCloseSession_RevokesOnceAndWritesOneLogoutRow(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash := opRandHex(t)
	if err := opOpen(t, ctx, tx, a.id, hash, opStep(t, ctx, tx)); err != nil {
		t.Fatalf("open: %v", err)
	}
	logouts := func() int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'logout' AND actor_admin_id = $1`, a.id)
	}
	if err := opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_close_session($1)`, hash); err != nil {
		t.Fatalf("close: %v", err)
	}
	var revoked, rowNamesIt bool
	if err := tx.QueryRow(ctx, `
		SELECT s.revoked_at IS NOT NULL,
		       EXISTS (SELECT 1 FROM operator_audit_log l WHERE l.kind = 'logout' AND l.session_id = s.id)
		  FROM platform_sessions s WHERE s.token_hash = $1`, hash).Scan(&revoked, &rowNamesIt); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !revoked || !rowNamesIt || logouts() != 1 {
		t.Fatalf("after logout: revoked=%v row-names-session=%v logout rows=%d, want true/true/1", revoked, rowNamesIt, logouts())
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_close_session($1)`, hash),
		sqlstateInvalidAuthorization, "a second logout of the same session")
	if logouts() != 1 {
		t.Fatalf("a second logout added a row (now %d)", logouts())
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_touch_session($1)`, hash),
		sqlstateInvalidAuthorization, "a touch after logout")
	before := opAudit(t, ctx, tx)
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_close_session($1)`, opRandHex(t)),
		sqlstateInvalidAuthorization, "logout of an unknown session")
	if d := opAudit(t, ctx, tx) - before; d != 0 {
		t.Fatalf("a refused logout left %d row(s)", d)
	}
}

// TestOpCloseSession_RefusesEveryDeadSession is ADR 0021 §6 "Davranış — oturum" for
// logout: the dead-session table op_touch_session is held to, driven through
// op_close_session. Each dead session is refused with 28000, stays exactly as it was
// (revoked_at untouched) and leaves no audit row -- a close that resolved its session
// by any lookup other than the touch predicate would log out an expired, MFA-less or
// disabled operator's session and write a logout row for it (the round-2 audit's
// mutant did exactly that with every other test green).
func TestOpCloseSession_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	for _, c := range opDeadSessions(t, ctx, tx) {
		var before *time.Time
		if c.id != uuid.Nil {
			if err := tx.QueryRow(ctx, `SELECT revoked_at FROM platform_sessions WHERE id = $1`, c.id).Scan(&before); err != nil {
				t.Fatalf("read revoked_at: %v", err)
			}
		}
		auditBefore := opAudit(t, ctx, tx)
		opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_close_session($1)`, c.hash),
			sqlstateInvalidAuthorization, "op_close_session: "+c.name)
		if d := opAudit(t, ctx, tx) - auditBefore; d != 0 {
			t.Errorf("%s: a refused logout left %d audit row(s)", c.name, d)
		}
		if c.id != uuid.Nil {
			var after *time.Time
			if err := tx.QueryRow(ctx, `SELECT revoked_at FROM platform_sessions WHERE id = $1`, c.id).Scan(&after); err != nil {
				t.Fatalf("read revoked_at: %v", err)
			}
			if (before == nil) != (after == nil) || (before != nil && !before.Equal(*after)) {
				t.Errorf("%s: a REFUSED logout changed revoked_at (%v -> %v)", c.name, before, after)
			}
		}
	}
}

// TestOperator00026_ArgumentsNeverComeBackInAnError is O1 of the round-2 audit. A
// constraint that refuses an ARGUMENT answers with a DETAIL line carrying the row --
// measured before the fix: a valid token with a cost-11 digest returned 23514 and
// "Failing row contains (...)" holding the digest, the sealed envelope in hex, the
// address and the enrollment hash; a repeated session hash returned 23505 and its value.
// It was also an oracle: those codes appear only once every WHERE condition held.
//
// Every case below therefore satisfies EVERY other condition (live token, fresh step,
// active account), so that only the argument's shape can refuse it -- and must come back
// as the function's one refusal (28000, the same message as a wrong token), with no
// DETAIL, no HINT and no argument value in any field, having written nothing. Each
// block ends with the same call made valid, which must succeed: the refusals were the
// shape, not something else.
func TestOperator00026_ArgumentsNeverComeBackInAnError(t *testing.T) {
	ctx, tx := opTx(t)

	clean := func(what string, err error, code, msg string, values ...string) {
		t.Helper()
		var pg *pgconn.PgError
		if !errors.As(err, &pg) {
			t.Fatalf("%s: want a PostgreSQL error, got %T %v", what, err, err)
		}
		if pg.Code != code || pg.Message != msg {
			t.Errorf("%s: %s %q, want %s %q", what, pg.Code, pg.Message, code, msg)
		}
		if pg.Detail != "" || pg.Hint != "" {
			// Lengths only: a DETAIL here is exactly the row this test says must not be
			// printed anywhere, test output included.
			t.Errorf("%s: the error carries a DETAIL (%d bytes) / HINT (%d bytes); a refusal must carry neither", what, len(pg.Detail), len(pg.Hint))
		}
		all := strings.Join([]string{pg.Message, pg.Detail, pg.Hint, pg.Where, pg.ConstraintName, pg.ColumnName}, "\n")
		for _, v := range values {
			if v != "" && strings.Contains(all, v) {
				t.Errorf("%s: the error text carries an argument value (%d characters of it)", what, len(v))
			}
		}
	}

	// --- op_complete_enrollment ------------------------------------------------------
	other := opNewActive(t, ctx, tx)
	takenHash, _ := opNewSession(t, ctx, tx, other.id, true)
	p, raw := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	cur := opStep(t, ctx, tx)
	goodDigest, goodSealed := opFakeDigest("v"), opFakeSealed(0x5A)
	enrollRefusal := "op_complete_enrollment: enrollment refused"
	for _, c := range []struct {
		name    string
		digest  any
		sealed  any
		session any
	}{
		{"digest cost 11", "$2a$11$" + strings.Repeat("v", 53), goodSealed, opRandHex(t)},
		{"digest cost 15", "$2a$15$" + strings.Repeat("v", 53), goodSealed, opRandHex(t)},
		{"digest with a 52-character body", "$2a$12$" + strings.Repeat("v", 52), goodSealed, opRandHex(t)},
		{"NULL digest", nil, goodSealed, opRandHex(t)},
		{"43-byte envelope", goodDigest, make([]byte, 43), opRandHex(t)},
		{"NULL envelope", goodDigest, nil, opRandHex(t)},
		{"non-hex session hash", goodDigest, goodSealed, strings.Repeat("z", 64)},
		{"short session hash", goodDigest, goodSealed, "abc123"},
		{"NULL session hash", goodDigest, goodSealed, nil},
		{"a session hash already in use", goodDigest, goodSealed, takenHash},
	} {
		before := opAudit(t, ctx, tx)
		err := opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_complete_enrollment($1, $2, $3, $4, $5, $6)`,
			p.id, raw, c.digest, c.sealed, cur, c.session)
		values := []string{raw, p.email, hex.EncodeToString(goodSealed)}
		for _, v := range []any{c.digest, c.session} {
			if s, ok := v.(string); ok {
				values = append(values, s)
			}
		}
		clean("op_complete_enrollment, "+c.name, err, sqlstateInvalidAuthorization, enrollRefusal, values...)
		if d := opAudit(t, ctx, tx) - before; d != 0 {
			t.Errorf("op_complete_enrollment, %s: %d audit row(s) left", c.name, d)
		}
	}
	var status string
	var used bool
	if err := tx.QueryRow(ctx, `SELECT status, enroll_used_at IS NOT NULL FROM platform_admins WHERE id = $1`, p.id).Scan(&status, &used); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != "pending" || used {
		t.Fatalf("after the refused calls the account is %s used=%v, want pending/false", status, used)
	}
	if err := opEnroll(t, ctx, tx, p.id, raw, goodDigest, goodSealed, cur, opRandHex(t)); err != nil {
		t.Fatalf("CONTROL: the same enrollment with valid arguments was refused: %v", err)
	}

	// --- op_open_session -------------------------------------------------------------
	a := opNewActive(t, ctx, tx)
	openRefusal := "op_open_session: session refused"
	for _, c := range []struct {
		name    string
		session any
	}{
		{"non-hex session hash", strings.Repeat("z", 64)},
		{"short session hash", "abc123"},
		{"NULL session hash", nil},
		{"a session hash already in use", takenHash},
	} {
		before := opAudit(t, ctx, tx)
		err := opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_open_session($1, $2, $3)`, a.id, c.session, cur)
		values := []string{a.email}
		if s, ok := c.session.(string); ok {
			values = append(values, s)
		}
		clean("op_open_session, "+c.name, err, sqlstateInvalidAuthorization, openRefusal, values...)
		if d := opAudit(t, ctx, tx) - before; d != 0 {
			t.Errorf("op_open_session, %s: %d audit row(s) left", c.name, d)
		}
		if s := opInt(t, ctx, tx, `SELECT totp_last_step FROM platform_admins WHERE id = $1`, a.id); s != 0 {
			t.Fatalf("op_open_session, %s: the refused call moved totp_last_step to %d", c.name, s)
		}
	}
	if err := opOpen(t, ctx, tx, a.id, opRandHex(t), cur); err != nil {
		t.Fatalf("CONTROL: the same login with a valid session hash was refused: %v", err)
	}

	// --- the three whose arguments reach no constraint ---------------------------------
	// op_touch_session and op_close_session use the hash only in a WHERE; hostile shapes
	// are an unknown session, nothing more.
	long := strings.Repeat("x", 1<<20)
	touchRefusal := "op_touch_session: operator session refused"
	for _, h := range []any{nil, "", long, strings.Repeat("z", 64)} {
		for _, fn := range []string{"op_touch_session", "op_close_session"} {
			err := opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.`+fn+`($1)`, h)
			v, _ := h.(string)
			clean(fn+" with a hostile hash", err, sqlstateInvalidAuthorization, touchRefusal, v)
		}
	}
	// op_record_auth_event validates the kind before writing; the address and the id are
	// only looked up.
	err := opRecord(t, ctx, tx, "not-a-kind", "someone@example.test", nil)
	clean("op_record_auth_event with a kind outside the set", err, sqlstateInvalidParameter,
		"op_record_auth_event: kind is not a pre-session failure kind", "someone@example.test", "not-a-kind")
	for _, email := range []any{strings.Repeat("y", 10000) + "@example.test", nil} {
		before := opAudit(t, ctx, tx)
		if err := opRecord(t, ctx, tx, "login_failed", email, nil); err != nil {
			t.Errorf("op_record_auth_event with a hostile address failed: %v", err)
		}
		if d := opAudit(t, ctx, tx) - before; d != 1 {
			t.Errorf("op_record_auth_event with a hostile address wrote %d rows, want 1", d)
		}
	}
}

// ---------------------------------------------------------------- the shape --

// TestOperator00026_WrittenTimesAreTheWallClock is ADR 0021 O-4 as behaviour: inside a
// transaction that has already run for a while, the audit row's `at` and the new
// session's created_at/last_used_at are the WALL clock, not the transaction's start.
// A DEFAULT now() (or an INSERT that wrote now()) back-dates them by the sleep.
func TestOperator00026_WrittenTimesAreTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	if err := opRecord(t, ctx, tx, "login_failed", nil, a.id); err != nil {
		t.Fatalf("record: %v", err)
	}
	hash := opRandHex(t)
	if err := opOpen(t, ctx, tx, a.id, hash, opStep(t, ctx, tx)); err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, c := range []struct{ what, sql string }{
		{"audit at (op_record_auth_event)", `SELECT at FROM operator_audit_log WHERE kind = 'login_failed' AND target_admin_id = $1`},
		{"audit at (op_open_session)", `SELECT at FROM operator_audit_log WHERE kind = 'login' AND actor_admin_id = $1`},
		{"session created_at", `SELECT created_at FROM platform_sessions WHERE admin_id = $1`},
		{"session last_used_at", `SELECT last_used_at FROM platform_sessions WHERE admin_id = $1`},
		{"session mfa_verified_at", `SELECT mfa_verified_at FROM platform_sessions WHERE admin_id = $1`},
		{"account last_login_at", `SELECT last_login_at FROM platform_admins WHERE id = $1`},
	} {
		var afterStart, beforeClock float64
		if err := tx.QueryRow(ctx, `SELECT extract(epoch FROM (x.t - transaction_timestamp()))::float8,
		                                   extract(epoch FROM (clock_timestamp() - x.t))::float8
		                              FROM (`+c.sql+`) AS x(t)`, a.id).Scan(&afterStart, &beforeClock); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if afterStart < 1.4 || beforeClock < 0 || beforeClock > 30 {
			t.Errorf("%s is %.3fs after the transaction start and %.3fs before the wall clock; a value written from the WALL clock is >= 1.4s after a start that slept 1.5s",
				c.what, afterStart, beforeClock)
		}
	}
}

// TestOperator00026_TableShapeChecks drives the CHECKs and the column grants of 00026 as
// statements: what ADR 0020/0021 put in the schema must refuse as the schema, whoever
// writes -- tappa_owner included where the rule is a CHECK, tappa_opdefiner where it is
// a grant. Each refusal has a passing neighbour, so a CHECK that refused everything
// would be caught too.
func TestOperator00026_TableShapeChecks(t *testing.T) {
	ctx, tx := opTx(t)
	insertAdmin := func(status, digest string, sealed []byte) error {
		t.Helper()
		id := uuid.New()
		return opTry(t, ctx, tx, `
			INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed)
			VALUES ($1, $2, 'shape', $3, $4, $5)`,
			id, "shape-"+id.String()[:12]+"@example.test", status, nilIfEmpty(digest), sealed)
	}

	// active => digest AND sealed secret (ADR 0020 §1).
	opWant(t, insertAdmin("active", "", opFakeSealed(1)), sqlstateCheckViolation, "active without a digest")
	opWant(t, insertAdmin("active", opFakeDigest("f"), nil), sqlstateCheckViolation, "active without a sealed secret")
	if err := insertAdmin("active", opFakeDigest("f"), opFakeSealed(1)); err != nil {
		t.Fatalf("CONTROL: an active account with both credentials was refused: %v", err)
	}
	// bcrypt cost 12..14, and a digest's shape.
	opWant(t, insertAdmin("active", "$2a$11$"+strings.Repeat("g", 53), opFakeSealed(1)), sqlstateCheckViolation, "cost 11")
	opWant(t, insertAdmin("active", "$2a$15$"+strings.Repeat("g", 53), opFakeSealed(1)), sqlstateCheckViolation, "cost 15")
	opWant(t, insertAdmin("active", "$2a$12$"+strings.Repeat("g", 52), opFakeSealed(1)), sqlstateCheckViolation, "a 52-character body")
	if err := insertAdmin("active", "$2b$14$"+strings.Repeat("g", 53), opFakeSealed(1)); err != nil {
		t.Fatalf("CONTROL: cost 14 was refused: %v", err)
	}
	// the sealed secret's lower bound (nonce 12 + 128-bit secret + tag 16).
	opWant(t, insertAdmin("active", opFakeDigest("f"), make([]byte, 43)), sqlstateCheckViolation, "a 43-byte envelope")
	if err := insertAdmin("active", opFakeDigest("f"), make([]byte, 44)); err != nil {
		t.Fatalf("CONTROL: a 44-byte envelope was refused: %v", err)
	}
	// pending needs a token; totp_last_step is NOT NULL and starts below every real step.
	opWant(t, insertAdmin("pending", "", nil), sqlstateCheckViolation, "pending without a token")
	// A pending account carries an UNUSED token (B1): the schema half of single use,
	// pinned by constraint NAME so that removing exactly this CHECK turns it red.
	_, usedHash := opRandToken(t)
	usedID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed,
		                             enroll_token_hash, enroll_issued_at, enroll_expires_at, enroll_used_at)
		VALUES ($1, $2, 'used', 'active', $3, $4, $5, clock_timestamp(), clock_timestamp() + interval '30 minutes', clock_timestamp())`,
		usedID, "used-"+usedID.String()[:12]+"@example.test", opFakeDigest("u"), opFakeSealed(9), usedHash); err != nil {
		t.Fatalf("an enrolled account with a used token: %v", err)
	}
	opWantConstraint(t, opTry(t, ctx, tx, `UPDATE platform_admins SET status = 'pending' WHERE id = $1`, usedID),
		"platform_admins_pending_token_unused", "back to pending with the OLD, used token")
	_, freshHash := opRandToken(t)
	opWantConstraint(t, opTry(t, ctx, tx, `
		INSERT INTO platform_admins (email, display_name, status, enroll_token_hash, enroll_issued_at, enroll_expires_at, enroll_used_at)
		VALUES ('usedpending-'||gen_random_uuid()||'@example.test', 'x', 'pending', $1, clock_timestamp(), clock_timestamp() + interval '30 minutes', clock_timestamp())`, freshHash),
		"platform_admins_pending_token_unused", "a pending account born with a used token")
	// CONTROL: the reset-mfa shape OP-9 is handed -- a NEW token and enroll_used_at = NULL
	// in the same statement -- is accepted.
	if err := opTry(t, ctx, tx, `
		UPDATE platform_admins
		   SET status = 'pending', totp_secret_sealed = NULL, enroll_token_hash = $2,
		       enroll_issued_at = clock_timestamp(), enroll_expires_at = clock_timestamp() + interval '30 minutes',
		       enroll_used_at = NULL
		 WHERE id = $1`, usedID, freshHash); err != nil {
		t.Fatalf("CONTROL: the reset-mfa shape (new token, used cleared) was refused: %v", err)
	}
	id := uuid.New()
	err := opTry(t, ctx, tx, `INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed, totp_last_step)
	                          VALUES ($1, $2, 'shape', 'active', $3, $4, NULL)`, id, "shape-"+id.String()[:12]+"@example.test", opFakeDigest("h"), opFakeSealed(2))
	opWant(t, err, sqlstateNotNullViolation, "totp_last_step NULL")
	a := opNewActive(t, ctx, tx)
	if s := opInt(t, ctx, tx, `SELECT totp_last_step FROM platform_admins WHERE id = $1`, a.id); s != 0 {
		t.Fatalf("totp_last_step starts at %d, want 0 (below every real step)", s)
	}
	// email is GLOBALLY unique, case-insensitively.
	opWant(t, opTry(t, ctx, tx, `INSERT INTO platform_admins (email, display_name, status, password_hash, totp_secret_sealed)
	                              VALUES ($1, 'dup', 'active', $2, $3)`, strings.ToUpper(a.email), opFakeDigest("i"), opFakeSealed(3)),
		sqlstateUniqueViolation, "the same address in another case")
	// enrollment TTL ceiling (1 h over the issue time) and the issuance triple.
	_, tokHash := opRandToken(t)
	opWant(t, opTry(t, ctx, tx, `INSERT INTO platform_admins (email, display_name, status, enroll_token_hash, enroll_issued_at, enroll_expires_at)
	                              VALUES ('ttl-'||gen_random_uuid()||'@example.test', 'ttl', 'pending', $1, clock_timestamp(), clock_timestamp() + interval '61 minutes')`, tokHash),
		sqlstateCheckViolation, "an enrollment token living 61 minutes")
	opWant(t, opTry(t, ctx, tx, `INSERT INTO platform_admins (email, display_name, status, enroll_token_hash)
	                              VALUES ('triple-'||gen_random_uuid()||'@example.test', 'triple', 'pending', $1)`, tokHash),
		sqlstateCheckViolation, "a token hash without its issue and expiry times")

	// sessions: the hash is a hash.
	opWant(t, opTry(t, ctx, tx, `INSERT INTO platform_sessions (admin_id, token_hash) VALUES ($1, 'not-a-hash')`, a.id),
		sqlstateCheckViolation, "a session hash that is not 64 hex")

	// audit: the actor shape and the closed kind set.
	_, sid := opNewSession(t, ctx, tx, a.id, true)
	opWant(t, opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ('login_failed', $1, $2)`, sid, a.id),
		sqlstateCheckViolation, "a pre-session failure claiming a session and an actor")
	opWant(t, opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind) VALUES ('login')`),
		sqlstateCheckViolation, "a login row without its session")
	opWant(t, opTry(t, ctx, tx, `INSERT INTO operator_audit_log (kind) VALUES ('bogus')`),
		sqlstateCheckViolation, "a kind outside the closed set")
	var auditID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ('login', $1, $2) RETURNING id`,
		sid, a.id).Scan(&auditID); err != nil {
		t.Fatalf("audit fixture: %v", err)
	}

	// tickets: the lifetime ceiling and the real-xid CHECK bind the OWNER too.
	ticket := func(extraCols, extraVals string, args ...any) error {
		all := append([]any{opRandHex(t), sid, auditID}, args...)
		return opTry(t, ctx, tx, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at`+extraCols+`)
		                          VALUES ($1, $2, 'probe', $3, clock_timestamp() + interval '30 seconds'`+extraVals+`)`, all...)
	}
	if err := ticket("", ""); err != nil {
		t.Fatalf("CONTROL: a 30-second ticket was refused: %v", err)
	}
	opWant(t, opTry(t, ctx, tx, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at)
	                              VALUES ($1, $2, 'probe', $3, clock_timestamp() + interval '61 seconds')`, opRandHex(t), sid, auditID),
		sqlstateCheckViolation, "a ticket living 61 seconds")
	opWant(t, ticket(", created_xact", ", '2'::xid8"), sqlstateCheckViolation, "created_xact = 2 (pg_xact_status says committed)")
	opWant(t, ticket(", created_xact", ", '1'::xid8"), sqlstateCheckViolation, "created_xact = 1")

	// tappa_opdefiner's grants, as statements (ADR 0021 §1, O-2, O-4).
	asDefiner := func(sql string, args ...any) error {
		t.Helper()
		return opExecAs(t, ctx, tx, "tappa_opdefiner", sql, args...)
	}
	binding := `INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at%s)
	            VALUES ($1, $2, 'probe', $3, clock_timestamp() + interval '30 seconds'%s)`
	if err := asDefiner(fmt.Sprintf(binding, "", ""), opRandHex(t), sid, auditID); err != nil {
		t.Fatalf("CONTROL: the definer cannot insert a ticket with the binding columns: %v", err)
	}
	for _, c := range []struct{ col, val string }{
		{"created_xact", "pg_current_xact_id()"},
		{"created_at", "clock_timestamp() + interval '1 year'"},
		{"consumed_at", "NULL"},
	} {
		opWant(t, asDefiner(fmt.Sprintf(binding, ", "+c.col, ", "+c.val), opRandHex(t), sid, auditID),
			sqlstateInsufficientPrivi, "the definer writing "+c.col+" on a ticket")
	}
	opWant(t, asDefiner(`UPDATE public.operator_read_tickets SET expires_at = expires_at WHERE session_id = $1`, sid),
		sqlstateInsufficientPrivi, "the definer updating a ticket's expires_at")
	if err := asDefiner(`UPDATE public.operator_read_tickets SET consumed_at = clock_timestamp() WHERE session_id = $1 AND consumed_at IS NULL`, sid); err != nil {
		t.Fatalf("CONTROL: the definer cannot consume a ticket: %v", err)
	}
	opWant(t, asDefiner(`INSERT INTO public.operator_audit_log (at, kind, target_admin_id) VALUES (clock_timestamp(), 'locked', $1)`, a.id),
		sqlstateInsufficientPrivi, "the definer writing the audit clock")
	opWant(t, asDefiner(`INSERT INTO public.platform_admins (email, display_name, status, password_hash, totp_secret_sealed)
	                                        VALUES ('definer@example.test', 'x', 'active', $1, $2)`, opFakeDigest("j"), opFakeSealed(4)),
		sqlstateInsufficientPrivi, "the definer creating an operator")
	opWant(t, asDefiner(`SELECT password_hash FROM public.platform_admins WHERE id = $1`, a.id),
		sqlstateInsufficientPrivi, "the definer reading a password digest")
	opWant(t, asDefiner(`SELECT totp_secret_sealed FROM public.platform_admins WHERE id = $1`, a.id),
		sqlstateInsufficientPrivi, "the definer reading a sealed TOTP secret")
}

// opWantConstraint fails unless err is a CHECK violation (23514) of exactly this
// constraint -- the name, not merely the class, so that the test belongs to one CHECK.
func opWantConstraint(t *testing.T, err error, constraint, what string) {
	t.Helper()
	opWant(t, err, sqlstateCheckViolation, what)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.ConstraintName != constraint {
		t.Fatalf("%s: refused by %q, want %q", what, pg.ConstraintName, constraint)
	}
}

// nilIfEmpty maps "" to a SQL NULL.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
