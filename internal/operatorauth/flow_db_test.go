package operatorauth

// flow_db_test.go -- the OP-6 acceptances that are the DATABASE's, driven through this
// package's real entry points against the real op_* functions (harness_db_test.go).
// Everything but TestTOTP_SameCodeFromNGoroutinesOpensExactlyOneSession runs in one
// rolled-back transaction; that one commits and says what it leaves.
//
// "SAAT ENJEKTE" MEANS TWO CLOCKS, DELIBERATELY (ADR 0021 §2 i): Go's is injected (the
// Authenticator's Now, pinned to the middle of the database's current TOTP step); the
// database's is its wall clock, and every expiry it enforces -- 8 h, 30 min, the lock
// window, the enrollment token -- is proven by WRITING THE ROW'S OWN TIMESTAMPS INTO
// THE PAST, not by waiting.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/sun"
)

const testAddr = "192.0.2.10"

// TestEnrollmentTokenHash_IsTheDatabasesHash pins the enrollment-token hash contract
// (00026 / OP-5 madde 12) to the live server: EnrollmentTokenHash(raw) equals
// `encode(sha256(convert_to(raw, 'UTF8')), 'hex')` -- for a minted token, and for text
// a link could never carry but a careless implementation would hash differently
// (non-ASCII, a space). OP-9's cmd/opadmin writes this value; op_complete_enrollment
// computes the other.
func TestEnrollmentTokenHash_IsTheDatabasesHash(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	tok, err := NewEnrollmentToken()
	if err != nil {
		t.Fatalf("NewEnrollmentToken: %v", err)
	}
	if len(tok.RevealForLink()) != tokenLen || !wellFormed(tok.RevealForLink()) || tok.Hash() != EnrollmentTokenHash(tok.RevealForLink()) {
		t.Fatalf("a minted token is not a 43-character base64url value hashing through EnrollmentTokenHash")
	}
	for _, raw := range []string{tok.RevealForLink(), "ünïcödé-ş-İ", "a b", "x", strings.Repeat("z", 200)} {
		var want string
		if err := tx.QueryRow(ctx, `SELECT encode(sha256(convert_to($1, 'UTF8')), 'hex')`, raw).Scan(&want); err != nil {
			t.Fatalf("hash in the database: %v", err)
		}
		if got := EnrollmentTokenHash(raw); got != want {
			t.Errorf("a %d-byte token: Go %s..., database %s...", len(raw), got[:8], want[:8])
		}
	}
}

// TestPassword_EveryArmPaysOneComparisonAtTheSameCost is ADR 0020 §3's "same answer,
// same time" on the real lookup: an unknown address, a wrong password, a PENDING
// account, a DISABLED account, an over-long password against a 72-byte one, an
// over-long address and an address PostgreSQL cannot store as text (a NUL byte,
// invalid UTF-8) each return the SAME error, pay exactly ONE bcrypt comparison
// (counted), and write exactly ONE row -- the kind is Go's view, the target the
// database's own lookup (so a pending or disabled address is unknown_email with that
// account as target, ADR 0021 §1's D3). The success arm pays one comparison and writes
// nothing. The cost half (dummy and stored digest both Cost) is
// TestPassword_TheDigestAndTheDummyAreBothCostTwelve.
func TestPassword_EveryArmPaysOneComparisonAtTheSameCost(t *testing.T) {
	warmDigests(t)
	pw72 := strings.Repeat("7", 72)
	d72, err := hashPassword(pw72) // before the transaction: see warmDigests
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ctx, tx := ownerTx(t)
	a, _ := newAuth(t, txStore(t, tx), dbStepTime(t, ctx, tx))
	active := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	pending, _ := newPendingAccount(t, ctx, tx, 30*time.Minute)
	disabled := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`, disabled.id); err != nil {
		t.Fatalf("disable: %v", err)
	}
	long := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET password_hash = $2 WHERE id = $1`, long.id, d72); err != nil {
		t.Fatalf("set the 72-byte digest: %v", err)
	}

	var calls int64
	a.compareFn = func(dg, pw []byte) error { calls++; return bcrypt.CompareHashAndPassword(dg, pw) }
	lastRow := func() (string, *uuid.UUID) {
		t.Helper()
		var kind string
		var target *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT kind, target_admin_id FROM operator_audit_log ORDER BY at DESC LIMIT 1`).Scan(&kind, &target); err != nil {
			t.Fatalf("read the last row: %v", err)
		}
		return kind, target
	}

	wrong := fixturePassphrase + "!"
	for _, c := range []struct {
		name, email, pw, kind string
		target                uuid.UUID
	}{
		{"unknown address", "nobody-" + uuid.NewString()[:8] + "@example.test", fixturePassphrase, "unknown_email", uuid.Nil},
		{"active, wrong password", active.email, wrong, "login_failed", active.id},
		{"active, wrong password, address in another case", strings.ToUpper(active.email), wrong, "login_failed", active.id},
		{"pending account, its would-be password", pending.email, fixturePassphrase, "unknown_email", pending.id},
		{"disabled account, its right password", disabled.email, fixturePassphrase, "unknown_email", disabled.id},
		{"72-byte password + 28 bytes (bcrypt would match)", long.email, pw72 + strings.Repeat("q", 28), "login_failed", long.id},
		{"over-long address", strings.Repeat("x", 300) + "@example.test", fixturePassphrase, "unknown_email", uuid.Nil},
		// Addresses PostgreSQL cannot store as text. Before the OP-6 verification
		// (2026-09-30) both came back as a database error (SQLSTATE 22021) with no bcrypt
		// and no row -- an arm outside "same answer, same time".
		{"address with a NUL byte", "a\x00b@example.test", fixturePassphrase, "unknown_email", uuid.Nil},
		{"address that is not UTF-8", "a\xffb@example.test", fixturePassphrase, "unknown_email", uuid.Nil},
	} {
		calls = 0
		before := auditRows(t, ctx, tx)
		c1, err := a.Password(ctx, testAddr, c.email, c.pw)
		if err != ErrRefused || c1.reveal() != "" {
			t.Errorf("%s: %v (challenge minted=%v), want exactly ErrRefused", c.name, err, c1.reveal() != "")
			continue
		}
		if calls != 1 {
			t.Errorf("%s: %d bcrypt comparison(s), want 1 -- the arms must cost the same", c.name, calls)
		}
		if d := auditRows(t, ctx, tx) - before; d != 1 {
			t.Errorf("%s: %d row(s), want 1", c.name, d)
			continue
		}
		kind, target := lastRow()
		gotTarget := uuid.Nil
		if target != nil {
			gotTarget = *target
		}
		if kind != c.kind || gotTarget != c.target {
			t.Errorf("%s: row kind=%s target=%s, want %s / %s", c.name, kind, gotTarget, c.kind, c.target)
		}
	}
	calls = 0
	before := auditRows(t, ctx, tx)
	if c, err := a.Password(ctx, testAddr, strings.ToUpper(active.email), fixturePassphrase); err != nil || c.reveal() == "" {
		t.Fatalf("the right password was refused: %v", err)
	}
	if calls != 1 || auditRows(t, ctx, tx) != before {
		t.Fatalf("a successful password step: %d comparison(s), %d row(s), want 1 and 0", calls, auditRows(t, ctx, tx)-before)
	}
}

// TestTOTP_SameCodeFromNGoroutinesOpensExactlyOneSession is the OP-6 acceptance "aynı
// kod N goroutine → tam 1 başarı (-race)": one challenge, one valid code, N racers on
// N pooled connections, each through the whole TOTP step in its own committed
// transactions -- the production shape. Every racer's code is right (Go accepts all N);
// the database's single conditional UPDATE (`totp_last_step < step`, 00026) lets
// exactly one open a session. The N-1 losers are refused as a failed attempt and each
// leaves a row (a replayed code is not free -- flow.go, step 6).
//
// THE LOSERS' LABEL DEPENDS ON TIMING, AND THE TEST SAYS SO (OP-6 verification, 4th
// round, measured): N-1 = 7 refusals are more than the lock threshold (5), so a loser
// that READS the account after five refusals have committed finds it locked and is
// answered ErrLocked with a 'locked' row instead of ErrCodeRejected with 'totp_failed'.
// The first version accepted only the second and failed about once in forty package
// runs ("a racer ended with … locked"). What does not depend on timing is asserted
// exactly: one winner, every loser refused with one row of either kind, and -- in the
// order the rows were WRITTEN (their `at`) -- no 'locked' row before five totp_failed
// rows. The order, not the final count (5th round, measured: labelling the FIRST
// refusal 'locked' stayed green while only the counts were compared).
//
// 🔴 IT COMMITS. Per run it leaves in the development database: one operator account
// (random uuid, @example.test), one session, one 'login' row and N-1 failure rows --
// 'totp_failed', or 'locked' for a loser that read the account after the lock engaged
// -- on an append-only table nothing can clean (the price OP-5's own race test pays,
// m10-platform.md OP-5 madde 17).
func TestTOTP_SameCodeFromNGoroutinesOpensExactlyOneSession(t *testing.T) {
	warmDigests(t)
	const racers = 8 // under accountLimit (10), so the budget refuses nobody here
	pool := ownerPool(t, racers+4)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := dbStepTime(t, ctx, pool)
	a, _ := newAuth(t, poolStore(pool), now)
	acc := newActiveAccount(t, ctx, pool, a.keys.kek.bytes())
	ch := challengeFor(t, a, acc)
	code := codeAt(acc.key, now)

	var wins, rejected, lockedOut, other int64
	var ready, done sync.WaitGroup
	ready.Add(racers)
	done.Add(racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			iss, err := a.TOTP(ctx, ch, code)
			switch {
			case err == nil && iss.AdminID == acc.id:
				atomic.AddInt64(&wins, 1)
			case errors.Is(err, ErrCodeRejected):
				atomic.AddInt64(&rejected, 1)
			case errors.Is(err, ErrLocked):
				atomic.AddInt64(&lockedOut, 1)
			default:
				atomic.AddInt64(&other, 1)
				t.Errorf("a racer ended with %v", err)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	// The losers are replays of the winner's step: refused for that alone only while
	// the database still binds it.
	stepStillBound(t, ctx, pool, now)

	sessions := countInt(t, ctx, pool, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, acc.id)
	logins := countInt(t, ctx, pool, `SELECT count(*) FROM operator_audit_log WHERE kind = 'login' AND actor_admin_id = $1`, acc.id)
	failed := countInt(t, ctx, pool, `SELECT count(*) FROM operator_audit_log WHERE kind = 'totp_failed' AND target_admin_id = $1`, acc.id)
	lockedRows := countInt(t, ctx, pool, `SELECT count(*) FROM operator_audit_log WHERE kind = 'locked' AND target_admin_id = $1`, acc.id)
	last := countInt(t, ctx, pool, `SELECT totp_last_step FROM platform_admins WHERE id = $1`, acc.id)
	var kinds []string
	rows, err := pool.Query(ctx, `SELECT kind FROM operator_audit_log WHERE target_admin_id = $1 AND kind IN ('totp_failed', 'locked') ORDER BY at, id`, acc.id)
	if err != nil {
		t.Fatalf("read the failure rows in order: %v", err)
	}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("scan: %v", err)
		}
		kinds = append(kinds, k)
	}
	rows.Close()
	for i, k := range kinds {
		if k == "locked" && i < 5 {
			t.Errorf("a 'locked' row was written after only %d totp_failed row(s); the lock engages at five", i)
			break
		}
	}
	if wins != 1 || rejected+lockedOut != racers-1 || other != 0 || sessions != 1 || logins != 1 ||
		failed+lockedRows != racers-1 || lockedRows != lockedOut || last != step(now) {
		t.Fatalf("racers=%d: wins=%d rejected=%d locked=%d other=%d; sessions=%d login rows=%d totp_failed rows=%d locked rows=%d last step=%d (want %d)",
			racers, wins, rejected, lockedOut, other, sessions, logins, failed, lockedRows, last, step(now))
	}
	t.Logf("left in the development database: operator %s, 1 session, 1 login row, %d totp_failed and %d locked rows", acc.id, failed, lockedRows)
}

// TestVerify_EveryDeadSessionIsRefused is the OP-6 acceptance "8 s / 30 dk / MFA'sız /
// revoked / disabled hepsi red": a session born through the real sign-in verifies;
// then each dead shape is WRITTEN into its row (the predicate runs on the database's
// wall clock, ADR 0021 §2 i) and Verify refuses it with ErrNoSession, writing nothing;
// the row is restored between cases. One minute inside each window is the positive
// half, so a window moved by more than that turns a case red. Logout ends it for good.
func TestVerify_EveryDeadSessionIsRefused(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, _ := newAuth(t, txStore(t, tx), now)
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	iss, err := a.TOTP(ctx, challengeFor(t, a, acc), codeAt(acc.key, now))
	if err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	id, err := a.Verify(ctx, iss.Token)
	if err != nil || id.AdminID != acc.id {
		t.Fatalf("a fresh session did not verify: %v", err)
	}
	if !storedUnderTokenKey(t, ctx, tx, a.keys.tokenKey.bytes(), iss.Token, acc.id) {
		t.Fatal("the sign-in's session row is not HMAC-SHA256(TokenHMACKey, token)")
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	reset := func() {
		exec(`UPDATE platform_sessions SET created_at = clock_timestamp(), last_used_at = clock_timestamp(),
		          revoked_at = NULL, mfa_verified_at = clock_timestamp() WHERE id = $1`, id.SessionID)
		exec(`UPDATE platform_admins SET status = 'active' WHERE id = $1`, acc.id)
	}
	for _, c := range []struct {
		name  string
		sql   string
		alive bool
	}{
		{"7h59m old, used 1 minute ago", `UPDATE platform_sessions SET created_at = clock_timestamp() - interval '7 hours 59 minutes', last_used_at = clock_timestamp() - interval '1 minute' WHERE id = $1`, true},
		{"8h01m old, used 1 minute ago", `UPDATE platform_sessions SET created_at = clock_timestamp() - interval '8 hours 1 minute', last_used_at = clock_timestamp() - interval '1 minute' WHERE id = $1`, false},
		{"idle 29 minutes", `UPDATE platform_sessions SET last_used_at = clock_timestamp() - interval '29 minutes' WHERE id = $1`, true},
		{"idle 31 minutes", `UPDATE platform_sessions SET last_used_at = clock_timestamp() - interval '31 minutes' WHERE id = $1`, false},
		{"no MFA stamp", `UPDATE platform_sessions SET mfa_verified_at = NULL WHERE id = $1`, false},
		{"revoked", `UPDATE platform_sessions SET revoked_at = clock_timestamp() WHERE id = $1`, false},
		{"operator disabled", `UPDATE platform_admins SET status = 'disabled' WHERE id = (SELECT admin_id FROM platform_sessions WHERE id = $1)`, false},
	} {
		reset()
		exec(c.sql, id.SessionID)
		before := auditRows(t, ctx, tx)
		got, err := a.Verify(ctx, iss.Token)
		switch {
		case c.alive && (err != nil || got.SessionID != id.SessionID):
			t.Errorf("%s: %v, want the session alive", c.name, err)
		case !c.alive && !errors.Is(err, ErrNoSession):
			t.Errorf("%s: %v, want ErrNoSession", c.name, err)
		}
		if d := auditRows(t, ctx, tx) - before; d != 0 {
			t.Errorf("%s: the session check wrote %d row(s)", c.name, d)
		}
	}
	reset()
	if _, err := a.Verify(ctx, wrapSessionToken("not-a-token")); !errors.Is(err, ErrNoSession) {
		t.Errorf("a junk value: %v, want ErrNoSession", err)
	}
	if err := a.Logout(ctx, wrapSessionToken("not-a-token")); !errors.Is(err, ErrNoSession) {
		t.Errorf("a logout with a junk value: %v, want ErrNoSession", err)
	}
	if err := a.Logout(ctx, iss.Token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if err := a.Logout(ctx, iss.Token); !errors.Is(err, ErrNoSession) {
		t.Errorf("a second logout: %v, want ErrNoSession", err)
	}
	if _, err := a.Verify(ctx, iss.Token); !errors.Is(err, ErrNoSession) {
		t.Errorf("a session after logout: %v, want ErrNoSession", err)
	}
	if n := countInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'logout' AND session_id = $1`, id.SessionID); n != 1 {
		t.Errorf("%d logout row(s), want 1", n)
	}
}

// TestLock_ThresholdAndWindowThroughTheSignIn is the OP-6 acceptance "kilit eşiği ve
// penceresi", through Go: N-1 = 4 wrong codes do not lock and the right code then
// resets the counter; N = 5 lock the account for the window (~15 min, 00026's numbers,
// kept -- see the OP-6 card correction); the right code while locked is refused BY THE
// DATABASE and recorded as 'locked', which does not count; a wrong code while locked
// counts and extends; once the window has passed (written, not waited) the account is
// no longer LABELLED locked even though the counter still stands above the threshold
// (5th round, measured: a label that ignored the window's end stayed green, because
// the success path does not read it) -- a code the database refuses there is a
// 'totp_failed' that counts, not a 'locked'; and the right code opens a session and
// the counter returns to zero.
func TestLock_ThresholdAndWindowThroughTheSignIn(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, _ := newAuth(t, txStore(t, tx), now)
	rows := func(kind string, id uuid.UUID) int64 {
		return countInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = $1 AND target_admin_id = $2`, kind, id)
	}
	lockedFor := func(id uuid.UUID) float64 {
		var s float64
		if err := tx.QueryRow(ctx, `SELECT coalesce(extract(epoch FROM totp_locked_until - clock_timestamp()), 0)::float8
		                              FROM platform_admins WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatalf("read the lock: %v", err)
		}
		return s
	}

	below := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	ch := challengeFor(t, a, below)
	for i := 0; i < 4; i++ {
		if _, err := a.TOTP(ctx, ch, wrongCodeAt(below.key, now)); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("wrong code %d: %v", i+1, err)
		}
	}
	if f := failures(t, ctx, tx, below.id); f != 4 || rows("totp_failed", below.id) != 4 {
		t.Fatalf("after 4 wrong codes: counter=%d rows=%d, want 4/4", f, rows("totp_failed", below.id))
	}
	if _, err := a.TOTP(ctx, ch, codeAt(below.key, now)); err != nil {
		t.Fatalf("4 failures locked the account; the threshold is 5: %v", err)
	}
	if f := failures(t, ctx, tx, below.id); f != 0 {
		t.Fatalf("a successful sign-in left the counter at %d", f)
	}

	locked := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	ch = challengeFor(t, a, locked)
	for i := 0; i < 5; i++ {
		if _, err := a.TOTP(ctx, ch, wrongCodeAt(locked.key, now)); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("wrong code %d: %v", i+1, err)
		}
	}
	if f, w := failures(t, ctx, tx, locked.id), lockedFor(locked.id); f != 5 || w < 14*60+50 || w > 15*60 {
		t.Fatalf("after 5 wrong codes: counter=%d window=%.0fs, want 5 / ~900 s", f, w)
	}
	sessions := func() int64 {
		return countInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, locked.id)
	}
	// The database refuses this code for the LOCK; on a resynced clock (and now with
	// it, which the window arithmetic below reads) that is its only reason.
	now = resyncAuth(t, ctx, tx, a)
	if _, err := a.TOTP(ctx, ch, codeAt(locked.key, now)); !errors.Is(err, ErrLocked) {
		t.Fatalf("the right code on a locked account: %v, want ErrLocked", err)
	}
	if sessions() != 0 || rows("locked", locked.id) != 1 || failures(t, ctx, tx, locked.id) != 5 {
		t.Fatalf("locked, right code: sessions=%d locked rows=%d counter=%d, want 0/1/5", sessions(), rows("locked", locked.id), failures(t, ctx, tx, locked.id))
	}
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_locked_until = clock_timestamp() + interval '5 minutes' WHERE id = $1`, locked.id); err != nil {
		t.Fatalf("shorten the window: %v", err)
	}
	if _, err := a.TOTP(ctx, ch, wrongCodeAt(locked.key, now)); !errors.Is(err, ErrLocked) {
		t.Fatalf("a wrong code while locked: %v, want ErrLocked", err)
	}
	if f, w := failures(t, ctx, tx, locked.id), lockedFor(locked.id); f != 6 || w < 14*60+50 {
		t.Fatalf("a wrong code while locked: counter=%d window=%.0fs, want 6 and a fresh ~900 s", f, w)
	}
	// Past the window, before any success: the counter is still 6, the lock has
	// expired. The right code is made one the database refuses (its step already
	// spent), so Go's check passes and op_open_session answers no. The window ends
	// before BOTH clocks -- the database's and the injected one (mid-step, so up to
	// 15 s either side of the database's): an end between them would be "over" for one
	// and "not yet" for the other (measured: with the database's clock alone, 95 of 300
	// repeated runs failed here; with both, 0 of 300).
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_locked_until = least(clock_timestamp(), $3) - interval '1 second', totp_last_step = $2 WHERE id = $1`, locked.id, step(now), now); err != nil {
		t.Fatalf("expire the window and spend the step: %v", err)
	}
	failedBefore, lockedBefore := rows("totp_failed", locked.id), rows("locked", locked.id)
	if _, err := a.TOTP(ctx, ch, codeAt(locked.key, now)); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("a refused code after the window: %v, want ErrCodeRejected (the lock has ended)", err)
	}
	stepStillBound(t, ctx, tx, now)
	if df, dl, f := rows("totp_failed", locked.id)-failedBefore, rows("locked", locked.id)-lockedBefore, failures(t, ctx, tx, locked.id); df != 1 || dl != 0 || f != 7 || sessions() != 0 {
		t.Fatalf("a refused code after the window: totp_failed %+d, locked %+d, counter %d, sessions %d; want +1, +0, 7, 0", df, dl, f, sessions())
	}
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_locked_until = clock_timestamp() - interval '1 second', totp_last_step = 0 WHERE id = $1`, locked.id); err != nil {
		t.Fatalf("expire the window again: %v", err)
	}
	if _, err := a.TOTP(ctx, ch, codeAt(locked.key, now)); err != nil {
		t.Fatalf("the lock outlived its window: %v", err)
	}
	if failures(t, ctx, tx, locked.id) != 0 || sessions() != 1 {
		t.Fatalf("after the window and the right code: counter=%d sessions=%d, want 0/1", failures(t, ctx, tx, locked.id), sessions())
	}
}

// TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter is the OP-6 acceptance moved
// from OP-5 B3 (m10-platform.md): a request a budget refuses writes no row and moves no
// counter -- driven through the budgets build makes (the constructor path New takes),
// with their shipped numbers, spent
// up to the limit and then asked once more. Each budget has a positive control that
// shows the same request DOES write when the budget has room.
func TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	cs := &countingStore{Store: txStore(t, tx)}
	a, logs := newAuth(t, cs, now)
	var calls int64
	a.compareFn = func(dg, pw []byte) error { calls++; return bcrypt.CompareHashAndPassword(dg, pw) }
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	ch := challengeFor(t, a, acc)

	// ACCOUNT budget (TOTP): control, then spend the rest, then one more attempt.
	before := auditRows(t, ctx, tx)
	if _, err := a.TOTP(ctx, ch, wrongCodeAt(acc.key, now)); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("CONTROL: %v", err)
	}
	if auditRows(t, ctx, tx)-before != 1 || failures(t, ctx, tx, acc.id) != 1 {
		t.Fatalf("CONTROL: a wrong code under budget did not write its row and move the counter")
	}
	for a.limits.account.charge(acc.id.String()) < accountLimit {
	}
	// "Before anything else touches the account" is counted, not argued: a refused
	// attempt does not even READ the account. Measured (OP-6 verification, 3rd round):
	// charging the budget after the lookup stayed green while only rows and the counter
	// were read.
	before, byID0 := auditRows(t, ctx, tx), cs.byID
	for _, code := range []string{wrongCodeAt(acc.key, now), codeAt(acc.key, now)} {
		if _, err := a.TOTP(ctx, ch, code); !errors.Is(err, ErrThrottled) {
			t.Fatalf("an attempt past the account budget: %v, want ErrThrottled", err)
		}
	}
	if cs.byID != byID0 {
		t.Fatalf("past the account budget the account was read %d time(s), want 0", cs.byID-byID0)
	}
	if d := auditRows(t, ctx, tx) - before; d != 0 || failures(t, ctx, tx, acc.id) != 1 ||
		countInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, acc.id) != 0 {
		t.Fatalf("past the account budget: rows %+d, counter %d, want 0 and 1 and no session", d, failures(t, ctx, tx, acc.id))
	}

	// WORK budget (password step): no comparison, no lookup's row.
	for a.limits.work.charge("198.51.100.7") < workLimit {
	}
	calls, before = 0, auditRows(t, ctx, tx)
	for _, email := range []string{acc.email, "nobody@example.test"} {
		if _, err := a.Password(ctx, "198.51.100.7", email, "wrong wrong wrong"); !errors.Is(err, ErrThrottled) {
			t.Fatalf("a password step past the work budget: %v", err)
		}
	}
	if calls != 0 || auditRows(t, ctx, tx) != before {
		t.Fatalf("past the work budget: %d comparison(s), %+d row(s), want 0 and 0", calls, auditRows(t, ctx, tx)-before)
	}
	// The enrollment's last step spends the SAME per-address work budget (flow.go). A
	// malformed token would write an enrollment_failed row if it got that far; past the
	// budget it must not. Measured (OP-6 verification, 2026-09-30): removing the
	// enrollment's work charge stayed green until this case existed.
	if _, err := a.CompleteEnrollment(ctx, "198.51.100.7", uuid.New(), "not-a-token", PendingBlob{}, strings.Repeat("new pass ", 2), "000000"); !errors.Is(err, ErrThrottled) {
		t.Fatalf("an enrollment step past the work budget: %v, want ErrThrottled", err)
	}
	if auditRows(t, ctx, tx) != before {
		t.Fatalf("an enrollment step past the work budget wrote %+d row(s)", auditRows(t, ctx, tx)-before)
	}
	calls = 0
	if _, err := a.Password(ctx, "198.51.100.8", acc.email, "wrong wrong wrong"); !errors.Is(err, ErrRefused) || calls != 1 || auditRows(t, ctx, tx) != before+1 {
		t.Fatalf("CONTROL, another address: %v, %d comparison(s), %+d row(s)", err, calls, auditRows(t, ctx, tx)-before)
	}

	// AUDIT CAP (process-wide, password-less rows): the request is still SERVED --
	// one comparison, the same refusal -- but no row is written, and the crossing is
	// logged once, without the address. ALL THREE password-less kinds are driven, each
	// with its control under the cap: measured (OP-6 verification, 2026-09-30), writing
	// login_failed or enrollment_failed straight to the store, around the cap, stayed
	// green while only unknown_email was driven here.
	//
	// Every enrollment below that the DATABASE answers carries a code of a resynced
	// clock (freshCode): the arms run up to 33 s after the pin, past which a pinned
	// step can leave op_complete_enrollment's cur ± 1, and the database's refusal of a
	// wrong token or an unknown account would then be the step's.
	freshCode := func(key []byte) string {
		t.Helper()
		now = resyncAuth(t, ctx, tx, a)
		return codeAt(key, now)
	}
	passwordless := []struct {
		kind string
		want error
		run  func(addr string) error
	}{
		{"unknown_email", ErrRefused, func(addr string) error {
			_, err := a.Password(ctx, addr, "nobody-"+uuid.NewString()[:6]+"@example.test", "wrong wrong wrong")
			return err
		}},
		{"login_failed", ErrRefused, func(addr string) error {
			_, err := a.Password(ctx, addr, acc.email, "wrong wrong wrong")
			return err
		}},
		// enrollment_failed, ARM BY ARM -- every arm reachable without an account: the
		// malformed token (E3), a foreign page (E4), a wrong first code (E5) and the
		// database refusing a well-formed link (E8). Measured (OP-6 verification, 4th
		// round): with the malformed-token arm alone, the other three arms' rows written
		// around the cap stayed green. TestAuditRows_EveryPasswordlessKindGoesThroughTheCap
		// reads every arm in the source; these drive the ones a stranger can reach.
		{"enrollment_failed", ErrEnrollment, func(addr string) error {
			_, err := a.CompleteEnrollment(ctx, addr, uuid.New(), "not-a-token", PendingBlob{}, strings.Repeat("new pass ", 2), "000000")
			return err
		}},
		{"enrollment_failed", ErrEnrollment, func(addr string) error {
			tok, _ := NewEnrollmentToken()
			other, _ := a.BeginEnrollment(uuid.New())
			_, err := a.CompleteEnrollment(ctx, addr, uuid.New(), tok.RevealForLink(), other.Blob, strings.Repeat("new pass ", 2), "000000")
			return err
		}},
		{"enrollment_failed", ErrCodeRejected, func(addr string) error {
			id, tok := uuid.New(), must(NewEnrollmentToken())
			p, _ := a.BeginEnrollment(id)
			_, err := a.CompleteEnrollment(ctx, addr, id, tok.RevealForLink(), p.Blob, strings.Repeat("new pass ", 2), wrongCodeAt(p.Secret.reveal(), now))
			return err
		}},
		{"enrollment_failed", ErrEnrollment, func(addr string) error {
			id, tok := uuid.New(), must(NewEnrollmentToken())
			p, _ := a.BeginEnrollment(id)
			_, err := a.CompleteEnrollment(ctx, addr, id, tok.RevealForLink(), p.Blob, strings.Repeat("new pass ", 2), freshCode(p.Secret.reveal()))
			return err
		}},
	}
	kindRows := func(kind string) int64 {
		return countInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = $1`, kind)
	}
	for _, c := range passwordless {
		k0 := kindRows(c.kind)
		if err := c.run("203.0.113.10"); !errors.Is(err, c.want) || kindRows(c.kind) != k0+1 {
			t.Fatalf("CONTROL, under the audit cap: %s: %v, %+d row(s) of its kind, want %v and 1", c.kind, err, kindRows(c.kind)-k0, c.want)
		}
	}
	for a.limits.auditCap.charge("") < auditCapLimit {
	}
	calls, before = 0, auditRows(t, ctx, tx)
	for _, c := range passwordless {
		if err := c.run("203.0.113.9"); !errors.Is(err, c.want) {
			t.Fatalf("past the audit cap: %s: %v, want the same %v", c.kind, err, c.want)
		}
		if d := auditRows(t, ctx, tx) - before; d != 0 {
			t.Fatalf("past the audit cap: a %s request wrote %d row(s)", c.kind, d)
		}
	}
	if _, err := a.Password(ctx, "203.0.113.9", "nobody-"+uuid.NewString()[:6]+"@example.test", "wrong wrong wrong"); !errors.Is(err, ErrRefused) {
		t.Fatalf("past the audit cap: %v, want the same ErrRefused", err)
	}
	if calls != 3 || auditRows(t, ctx, tx) != before {
		t.Fatalf("past the audit cap: %d comparison(s), %+d row(s), want 3 (every password step still pays one) and 0", calls, auditRows(t, ctx, tx)-before)
	}
	if n := strings.Count(logs.String(), "operator pre-session audit rows suppressed"); n != 1 || strings.Contains(logs.String(), "203.0.113.9") || strings.Contains(logs.String(), "@example.test") {
		t.Fatalf("the audit cap's log: %d line(s), carries the address=%v, carries the email=%v; want 1, false, false",
			n, strings.Contains(logs.String(), "203.0.113.9"), strings.Contains(logs.String(), "@example.test"))
	}

	// A FRESH audit cap for the enrollment arms below (12d, the closing auditor): the
	// cap above was spent to its limit, and past it no row is written whatever an arm
	// does -- so the "+0 row(s)" checks below could never turn red. With the cap fresh, a
	// refusal that wrote an enrollment_failed row shows as +1.
	a.limits.auditCap = newBudget(auditCapLimit, auditCapPeriod, a.now)

	// ENROLLMENT budget (process-wide): a request that would pay bcrypt + Seal is
	// refused BEFORE it does -- counted: no digest is made and the database is not
	// called -- writes no row and leaves the account pending. Measured (OP-6
	// verification, 2026-09-30): paying the digest first and the budget second stayed
	// green while only the outcome was read. The control, under the budget, is a
	// request that passes every check Go can make and carries a well-formed WRONG
	// token: it pays exactly one digest and one database call, which refuses it.
	var digests int
	// Wrap the PRODUCTION digestFn, never a stand-in: a test that swaps in its own
	// generator measures that generator's cost, not the one enrollment ships with --
	// measured (OP-6 verification, 3rd round): with hashPassword here, a production
	// digestFn at cost 13 or 14 left the package green.
	orig := a.digestFn
	a.digestFn = func(pw string) (string, error) { digests++; return orig(pw) }
	p, raw := newPendingAccount(t, ctx, tx, 30*time.Minute)
	pend, err := a.BeginEnrollment(p.id)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	wrongTok, err := NewEnrollmentToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	c0 := cs.completes
	if _, err := a.CompleteEnrollment(ctx, "198.51.100.10", p.id, wrongTok.RevealForLink(), pend.Blob, strings.Repeat("new pass ", 2), freshCode(pend.Secret.reveal())); !errors.Is(err, ErrEnrollment) || digests != 1 || cs.completes != c0+1 {
		t.Fatalf("CONTROL, under the enrollment budget: %v, %d digest(s), %d database call(s), want ErrEnrollment, 1 and 1", err, digests, cs.completes-c0)
	}
	// ENROLLMENT, the per-address SHARE (OP-6 12c): one address gets enrollAddrLimit
	// requests that reach the process-wide budget per window, the next is refused BEFORE
	// it -- no digest, no database call, no row, and the PROCESS-WIDE counter unmoved --
	// and another address still spends what the process budget has left. The first of
	// the address's requests is the control (under the share: one digest, one database
	// call). A stand-in digest generator here, unlike the control above: this arm counts
	// digests, it does not time them, and enrollAddrLimit+1 production digests would cost
	// ~4 s each under -race.
	fixture, err := fixtureDigest()
	if err != nil {
		t.Fatalf("fixture digest: %v", err)
	}
	a.digestFn = func(string) (string, error) { digests++; return fixture, nil }
	processCount := func() int {
		if w := a.limits.enroll.windows[""]; w != nil {
			return w.count
		}
		return 0
	}
	for i := 0; i < enrollAddrLimit; i++ {
		digests, c0 = 0, cs.completes
		if _, err := a.CompleteEnrollment(ctx, "198.51.100.20", p.id, wrongTok.RevealForLink(), pend.Blob, strings.Repeat("new pass ", 2), freshCode(pend.Secret.reveal())); !errors.Is(err, ErrEnrollment) || digests != 1 || cs.completes != c0+1 {
			t.Fatalf("CONTROL, request %d of one address under its enrollment share: %v, %d digest(s), %d database call(s), want ErrEnrollment, 1 and 1", i+1, err, digests, cs.completes-c0)
		}
	}
	digests, c0, before = 0, cs.completes, auditRows(t, ctx, tx)
	p0 := processCount()
	if _, err := a.CompleteEnrollment(ctx, "198.51.100.20", p.id, wrongTok.RevealForLink(), pend.Blob, strings.Repeat("new pass ", 2), codeAt(pend.Secret.reveal(), now)); !errors.Is(err, ErrThrottled) {
		t.Fatalf("request %d of one address: %v, want ErrThrottled from its enrollment share", enrollAddrLimit+1, err)
	}
	if digests != 0 || cs.completes != c0 || auditRows(t, ctx, tx) != before || processCount() != p0 {
		t.Fatalf("past one address's enrollment share: %d digest(s), %d database call(s), %+d row(s), process counter %d -> %d; want 0, 0, 0 and unmoved",
			digests, cs.completes-c0, auditRows(t, ctx, tx)-before, p0, processCount())
	}
	digests, c0 = 0, cs.completes
	if _, err := a.CompleteEnrollment(ctx, "198.51.100.21", p.id, wrongTok.RevealForLink(), pend.Blob, strings.Repeat("new pass ", 2), freshCode(pend.Secret.reveal())); !errors.Is(err, ErrEnrollment) || digests != 1 || cs.completes != c0+1 {
		t.Fatalf("another address after one exhausted its share: %v, %d digest(s), %d database call(s), want ErrEnrollment, 1 and 1 -- the process budget's remainder is its", err, digests, cs.completes-c0)
	}
	a.digestFn = func(pw string) (string, error) { digests++; return orig(pw) }

	for a.limits.enroll.charge("") < enrollLimit {
	}
	digests, c0, before = 0, cs.completes, auditRows(t, ctx, tx)
	if _, err := a.CompleteEnrollment(ctx, "198.51.100.9", p.id, raw, pend.Blob, strings.Repeat("new pass ", 2), codeAt(pend.Secret.reveal(), now)); !errors.Is(err, ErrThrottled) {
		t.Fatalf("an enrollment past the process budget: %v, want ErrThrottled", err)
	}
	if digests != 0 || cs.completes != c0 {
		t.Fatalf("past the enrollment budget: %d digest(s), %d database call(s), want 0 and 0", digests, cs.completes-c0)
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM platform_admins WHERE id = $1`, p.id).Scan(&status); err != nil || status != "pending" || auditRows(t, ctx, tx) != before {
		t.Fatalf("past the enrollment budget: status=%s rows %+d, want pending and 0", status, auditRows(t, ctx, tx)-before)
	}
}

// TestLimits_TheAuditCapCannotSwitchTheLockOff: with the process-wide cap on
// password-less rows exhausted (a flood of unknown addresses needs no password), five
// wrong codes STILL write their five totp_failed rows and lock the account -- the
// failure OP-5 measured against a database-side cap (m10-platform.md OP-5 madde 10)
// cannot be reproduced one layer up.
//
// The same holds for the TOTP step's OTHER two rows, the ones written when the DATABASE
// refuses a code Go accepted (flow.go, step 6): a replayed code still writes its
// totp_failed row and moves the counter, and the right code on a locked account still
// writes its 'locked' row. Both were measured unpinned (OP-6 verification, 2026-09-30):
// routing that arm through the cap left this test green until these two halves existed.
func TestLimits_TheAuditCapCannotSwitchTheLockOff(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, _ := newAuth(t, txStore(t, tx), now)
	for a.limits.auditCap.charge("") <= auditCapLimit {
	}
	rows := func(kind string, id uuid.UUID) int64 {
		return countInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = $1 AND target_admin_id = $2`, kind, id)
	}

	// A REPLAYED code (the database refuses it: its step is no longer greater than the
	// stored one) is a failed attempt whatever the cap says.
	replayed := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	rch := challengeFor(t, a, replayed)
	if _, err := a.TOTP(ctx, rch, codeAt(replayed.key, now)); err != nil {
		t.Fatalf("CONTROL: the right code, first use: %v", err)
	}
	if _, err := a.TOTP(ctx, rch, codeAt(replayed.key, now)); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the same code again: %v, want ErrCodeRejected", err)
	}
	stepStillBound(t, ctx, tx, now)
	if r, f := rows("totp_failed", replayed.id), failures(t, ctx, tx, replayed.id); r != 1 || f != 1 {
		t.Fatalf("with the audit cap exhausted, a replayed code: %d totp_failed row(s), counter %d, want 1 and 1", r, f)
	}

	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	ch := challengeFor(t, a, acc)
	for i := 0; i < 5; i++ {
		if _, err := a.TOTP(ctx, ch, wrongCodeAt(acc.key, now)); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("wrong code %d: %v", i+1, err)
		}
	}
	if r := rows("totp_failed", acc.id); r != 5 || failures(t, ctx, tx, acc.id) != 5 {
		t.Fatalf("with the audit cap exhausted: %d totp_failed row(s), counter %d, want 5 and 5", r, failures(t, ctx, tx, acc.id))
	}
	// Refused by the database for the LOCK: on a resynced clock, for nothing else.
	now = resyncAuth(t, ctx, tx, a)
	if _, err := a.TOTP(ctx, ch, codeAt(acc.key, now)); !errors.Is(err, ErrLocked) {
		t.Fatalf("the right code after five failures: %v, want ErrLocked", err)
	}
	if r := rows("locked", acc.id); r != 1 {
		t.Fatalf("with the audit cap exhausted, the right code on a locked account: %d 'locked' row(s), want 1", r)
	}
}

// TestTOTP_TheEnvelopeIsBoundToItsAccountAndItsKEK is "yanlış KEK açamaz" and the AAD
// binding through the sign-in: an envelope copied onto another account's row does not
// open for that account (whatever code is typed), and an Authenticator holding another
// KEK cannot open the right envelope. Both are server faults, not the operator's
// failure: an error, NO session, NO row, NO counter. The control (same account, right
// KEK) opens a session, so each refusal is the binding and nothing else.
func TestTOTP_TheEnvelopeIsBoundToItsAccountAndItsKEK(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	store := txStore(t, tx)
	a, _ := newAuth(t, store, now)
	accA := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	accB := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_secret_sealed = (SELECT totp_secret_sealed FROM platform_admins WHERE id = $1) WHERE id = $2`, accA.id, accB.id); err != nil {
		t.Fatalf("move A's envelope onto B: %v", err)
	}
	quiet := func(what string, err error, id uuid.UUID, before int64) {
		t.Helper()
		if err == nil || errors.Is(err, ErrCodeRejected) || errors.Is(err, ErrLocked) {
			t.Errorf("%s: %v, want the envelope refused as a server fault", what, err)
		}
		if d := auditRows(t, ctx, tx) - before; d != 0 || failures(t, ctx, tx, id) != 0 ||
			countInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, id) != 0 {
			t.Errorf("%s: rows %+d, counter %d -- a server fault must not count against the operator", what, d, failures(t, ctx, tx, id))
		}
	}
	chB := challengeFor(t, a, accB)
	for _, code := range []string{codeAt(accA.key, now), codeAt(accB.key, now)} {
		before := auditRows(t, ctx, tx)
		_, err := a.TOTP(ctx, chB, code)
		quiet("A's envelope on B's row", err, accB.id, before)
	}

	other, _ := newAuth(t, store, now) // another KEK, same database
	accC := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	chC := challengeFor(t, other, accC)
	before := auditRows(t, ctx, tx)
	_, err := other.TOTP(ctx, chC, codeAt(accC.key, now))
	quiet("another KEK", err, accC.id, before)

	if _, err := a.TOTP(ctx, challengeFor(t, a, accC), codeAt(accC.key, now)); err != nil {
		t.Fatalf("CONTROL: the right KEK and the account's own envelope: %v", err)
	}
}

// TestTOTP_AChallengeDoesNotOutliveTheAccount: the login challenge is minted by a
// correct password, but it vouches for nothing past the account's status -- an account
// disabled between the two steps is refused at the TOTP step with the password step's
// own answer (ErrRefused), and the refusal opens no session and writes no row. The
// control re-activates the same account and the same challenge with the same code opens
// a session, so the refusal was the status and nothing else. Measured (OP-6
// verification, 2026-09-30): this arm had no test, and a flow that carried on to the
// code check for an inactive account stayed green.
func TestTOTP_AChallengeDoesNotOutliveTheAccount(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, _ := newAuth(t, txStore(t, tx), now)
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	ch := challengeFor(t, a, acc)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`, acc.id); err != nil {
		t.Fatalf("disable: %v", err)
	}
	before := auditRows(t, ctx, tx)
	if _, err := a.TOTP(ctx, ch, codeAt(acc.key, now)); err != ErrRefused {
		t.Fatalf("the right code on an account disabled after the password step: %v, want exactly ErrRefused", err)
	}
	if d := auditRows(t, ctx, tx) - before; d != 0 || failures(t, ctx, tx, acc.id) != 0 ||
		countInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, acc.id) != 0 {
		t.Fatalf("a disabled account's TOTP step: rows %+d, counter %d, want nothing written", d, failures(t, ctx, tx, acc.id))
	}
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'active' WHERE id = $1`, acc.id); err != nil {
		t.Fatalf("re-activate: %v", err)
	}
	if _, err := a.TOTP(ctx, ch, codeAt(acc.key, now)); err != nil {
		t.Fatalf("CONTROL: the same challenge and code on the re-activated account: %v", err)
	}
}

// TestTOTP_ANextStepCodeIsRetiredByItsOwnStep is the replay guard's WIRING: the step
// op_open_session receives is the step the code MATCHED, not the step Go's clock is in.
// Every other flow test signs in with the current step's code, where the two are the
// same number -- measured (OP-6 verification, 2026-09-30): passing Go's current step
// instead stayed green across the package, and the auditor's probe opened TWO sessions
// with ONE six-digit code. So the code here is the NEXT step's (inside ±1 of both
// clocks, and different from the current one): the first use opens a session and
// stores that next step; Go's clock then moves into the next step and the same code is
// presented again -- the database must refuse it (ErrCodeRejected), one session only.
func TestTOTP_ANextStepCodeIsRetiredByItsOwnStep(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, _ := newAuth(t, txStore(t, tx), now)
	next := now.Add(PeriodSeconds * time.Second)
	// A key whose next-step code differs from its current one (they coincide about
	// once in a million keys; the property needs two different codes).
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	for codeAt(acc.key, next) == codeAt(acc.key, now) {
		acc = newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	}
	code := codeAt(acc.key, next)
	if _, err := a.TOTP(ctx, challengeFor(t, a, acc), code); err != nil {
		t.Fatalf("the next step's code, first use: %v", err)
	}
	if last := countInt(t, ctx, tx, `SELECT totp_last_step FROM platform_admins WHERE id = $1`, acc.id); last != step(next) {
		t.Fatalf("the stored step is %d, want the step the code matched (%d), not the current one (%d)", last, step(next), step(now))
	}
	a.now = func() time.Time { return next }
	if _, err := a.TOTP(ctx, challengeFor(t, a, acc), code); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the same code again, one step later by Go's clock: %v, want ErrCodeRejected", err)
	}
	stepStillBound(t, ctx, tx, next)
	if n := countInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, acc.id); n != 1 {
		t.Fatalf("one six-digit code opened %d session(s), want 1", n)
	}
}

// TestEnrollment_ANextStepFirstCodeCannotSignInAgain is the same wiring for the
// enrollment (op_complete_enrollment stores the first code's MATCHED step as
// totp_last_step): the first code is taken from the next step, Go's clock then moves
// there, and the same code at sign-in must be refused -- the enrollment's own session
// stays the only one. Measured (OP-6 verification, 2026-09-30): passing Go's current
// step instead stayed green, and the auditor's probe signed in again with the
// enrollment's code.
func TestEnrollment_ANextStepFirstCodeCannotSignInAgain(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, _ := newAuth(t, txStore(t, tx), now)
	p, raw := newPendingAccount(t, ctx, tx, 30*time.Minute)
	next := now.Add(PeriodSeconds * time.Second)
	// A page whose secret's next-step code differs from its current one (see above).
	var pend Pending
	for {
		var err error
		if pend, err = a.BeginEnrollment(p.id); err != nil {
			t.Fatalf("begin: %v", err)
		}
		if codeAt(pend.Secret.reveal(), next) != codeAt(pend.Secret.reveal(), now) {
			break
		}
	}
	code := codeAt(pend.Secret.reveal(), next)
	pw := strings.Repeat("next step pass ", 2)
	if _, err := a.CompleteEnrollment(ctx, testAddr, p.id, raw, pend.Blob, pw, code); err != nil {
		t.Fatalf("an enrollment with the next step's code: %v", err)
	}
	if last := countInt(t, ctx, tx, `SELECT totp_last_step FROM platform_admins WHERE id = $1`, p.id); last != step(next) {
		t.Fatalf("the stored step is %d, want the step the first code matched (%d), not the current one (%d)", last, step(next), step(now))
	}
	a.now = func() time.Time { return next }
	ch, err := a.Password(ctx, testAddr, p.email, pw)
	if err != nil {
		t.Fatalf("the password step: %v", err)
	}
	if _, err := a.TOTP(ctx, ch, code); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the enrollment's first code at sign-in, one step later by Go's clock: %v, want ErrCodeRejected", err)
	}
	stepStillBound(t, ctx, tx, next)
	if n := countInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, p.id); n != 1 {
		t.Fatalf("the enrollment's code opened %d session(s), want 1 (the enrollment's own)", n)
	}
}

// TestEnrollment_CompletesOnceAndTheStoredEnvelopeOpens drives the enrollment end to
// end: a pending account (token stored as EnrollmentTokenHash), BeginEnrollment's
// secret, the first code, a new password. After it: the account is active, the stored
// envelope opens under the KEK with the account id as AAD and holds the secret the page
// showed, the digest is Cost and matches, totp_last_step is the first code's step (so
// the enrollment code cannot sign in again), the returned session verifies, and one
// 'enrollment' row exists. Then every refusal: one row each (under the cap), the account
// untouched, and the weak password refused before anything is spent or written.
func TestEnrollment_CompletesOnceAndTheStoredEnvelopeOpens(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	cs := &countingStore{Store: txStore(t, tx)}
	a, _ := newAuth(t, cs, now)
	var digests int
	// Wrap the PRODUCTION digestFn, never a stand-in: a test that swaps in its own
	// generator measures that generator's cost, not the one enrollment ships with --
	// measured (OP-6 verification, 3rd round): with hashPassword here, a production
	// digestFn at cost 13 or 14 left the package green.
	orig := a.digestFn
	a.digestFn = func(pw string) (string, error) { digests++; return orig(pw) }
	newPw := strings.Repeat("new op6 pass ", 2)

	p, raw := newPendingAccount(t, ctx, tx, 30*time.Minute)
	pend, err := a.BeginEnrollment(p.id)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	shown := append([]byte(nil), pend.Secret.reveal()...)
	code := codeAt(shown, now)
	iss, err := a.CompleteEnrollment(ctx, testAddr, p.id, raw, pend.Blob, newPw, code)
	if err != nil {
		t.Fatalf("a valid enrollment was refused: %v", err)
	}
	var (
		status, digest string
		sealed         []byte
		lastStep       int64
		used           bool
	)
	if err := tx.QueryRow(ctx, `SELECT status, password_hash, totp_secret_sealed, totp_last_step, enroll_used_at IS NOT NULL
	                              FROM platform_admins WHERE id = $1`, p.id).Scan(&status, &digest, &sealed, &lastStep, &used); err != nil {
		t.Fatalf("read the enrolled account: %v", err)
	}
	opened, err := sun.Open(a.keys.kek.bytes(), p.id[:], sealed)
	cost, _ := bcrypt.Cost([]byte(digest))
	if status != "active" || !used || err != nil || string(opened) != string(shown) || len(sealed) != sun.SealOverhead+SecretBytes ||
		cost != Cost || bcrypt.CompareHashAndPassword([]byte(digest), []byte(newPw)) != nil || lastStep != step(now) {
		t.Fatalf("after enrollment: status=%s used=%v envelope-opens=%v (%v) holds-the-shown-key=%v size=%d cost=%d digest-matches=%v last-step=%d/%d",
			status, used, err == nil, err, string(opened) == string(shown), len(sealed), cost,
			bcrypt.CompareHashAndPassword([]byte(digest), []byte(newPw)) == nil, lastStep, step(now))
	}
	if id, err := a.Verify(ctx, iss.Token); err != nil || id.AdminID != p.id {
		t.Fatalf("the enrollment's session does not verify: %v", err)
	}
	if !storedUnderTokenKey(t, ctx, tx, a.keys.tokenKey.bytes(), iss.Token, p.id) {
		t.Fatal("the enrollment's session row is not HMAC-SHA256(TokenHMACKey, token)")
	}
	if n := countInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'enrollment' AND actor_admin_id = $1`, p.id); n != 1 {
		t.Fatalf("%d enrollment row(s), want 1", n)
	}
	// The enrollment code cannot sign in (its step is stored); a right code at the
	// next step could -- that is the database's replay guard, reached through Go.
	ch, err := a.Password(ctx, testAddr, p.email, newPw)
	if err != nil {
		t.Fatalf("signing in with the new password: %v", err)
	}
	if _, err := a.TOTP(ctx, ch, code); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("the enrollment's own code at sign-in: %v, want ErrCodeRejected (a replay)", err)
	}
	stepStillBound(t, ctx, tx, now)
	// The same link again -- NOT a replay (op_complete_enrollment does not compare with
	// totp_last_step), so it is sent on a resynced clock with that clock's code, and the
	// database refuses the USED TOKEN and nothing else. Measured: with the token made
	// acceptable again (pending, unused) and the database's step held two ahead, the
	// enrollment's own code on the pinned clock stayed green.
	now = resyncAuth(t, ctx, tx, a)
	before := auditRows(t, ctx, tx)
	if _, err := a.CompleteEnrollment(ctx, testAddr, p.id, raw, pend.Blob, newPw, codeAt(shown, now)); !errors.Is(err, ErrEnrollment) {
		t.Fatalf("a used link: %v, want ErrEnrollment", err)
	}
	if auditRows(t, ctx, tx)-before != 1 {
		t.Fatal("a refused enrollment did not write its enrollment_failed row")
	}

	// Refusals on fresh pending accounts. Each: the error, one row naming the account,
	// the account still pending -- and what the refusal PAID: a refusal Go can make on
	// its own (the password rule, the token's shape, the page, the first code) comes
	// before the digest and the database call, so it pays neither; only a request that
	// passed every one of those pays one digest and one call, which the database then
	// refuses. Measured (OP-6 verification, 2026-09-30): removing the token's shape gate
	// stayed green -- the database refused the malformed token too, one digest later.
	refuse := func(name string, want error, rows int64, paid int, run func(p fixtureAccount, raw string, pend Pending) error) {
		t.Helper()
		q, qraw := newPendingAccount(t, ctx, tx, 30*time.Minute)
		qpend, err := a.BeginEnrollment(q.id)
		if err != nil {
			t.Fatalf("%s: begin: %v", name, err)
		}
		// The two arms the database answers ran 18.6 and 23.1 s after the pin (-race, a
		// developer machine); a slower run passes the 33 s after which the pinned step
		// can fall out of the database's cur ± 1, and the database then refuses them for
		// the step. Measured: with the last arm's expiry removed and the database's step
		// held two ahead, the arm stayed green on the pinned clock and turned red on a
		// resynced one. So each arm sends its code on a resynced clock (good reads now).
		now = resyncAuth(t, ctx, tx, a)
		before, d0, c0 := auditRows(t, ctx, tx), digests, cs.completes
		if err := run(q, qraw, qpend); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", name, err, want)
		}
		if digests-d0 != paid || cs.completes-c0 != paid {
			t.Errorf("%s: %d digest(s) and %d database call(s), want %d and %d", name, digests-d0, cs.completes-c0, paid, paid)
		}
		if d := auditRows(t, ctx, tx) - before; d != rows {
			t.Errorf("%s: %d row(s), want %d", name, d, rows)
		}
		if rows == 1 {
			if n := countInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'enrollment_failed' AND target_admin_id = $1`, q.id); n != 1 {
				t.Errorf("%s: the row does not name the account", name)
			}
		}
		var st string
		if err := tx.QueryRow(ctx, `SELECT status FROM platform_admins WHERE id = $1`, q.id).Scan(&st); err != nil || st != "pending" {
			t.Errorf("%s: the account is %s after a refusal", name, st)
		}
	}
	// The refusals come from their own address: the steps above already spent part of
	// testAddr's share of the enrollment budget (enrollAddrLimit, 12c), and this test's
	// subject is not that budget.
	const refuseAddr = "192.0.2.11"
	good := func(q fixtureAccount, qraw string, qp Pending) (string, PendingBlob, string) {
		return qraw, qp.Blob, codeAt(qp.Secret.reveal(), now)
	}
	// The password rule is a person's typing error, answered BEFORE the process-wide
	// enrollment budget is spent (flow.go's order). hashPassword re-applies the rule, so
	// the error alone cannot tell the two orders apart -- measured (OP-6 verification,
	// 2026-09-30): without the early check this case stayed green -- the budget can.
	refuse("a 13-rune password", ErrWeakPassword, 0, 0, func(q fixtureAccount, qraw string, qp Pending) error {
		r, b, c := good(q, qraw, qp)
		before := spentIn(a.limits.enroll, "")
		_, err := a.CompleteEnrollment(ctx, refuseAddr, q.id, r, b, strings.Repeat("s", 13), c)
		if after := spentIn(a.limits.enroll, ""); after != before {
			t.Errorf("a 13-rune password spent the process-wide enrollment budget (%d -> %d); the rule is checked before it", before, after)
		}
		return err
	})
	refuse("another account's token", ErrEnrollment, 1, 1, func(q fixtureAccount, qraw string, qp Pending) error {
		_, b, c := good(q, qraw, qp)
		_, err := a.CompleteEnrollment(ctx, refuseAddr, q.id, raw, b, newPw, c)
		return err
	})
	refuse("a token of the wrong shape", ErrEnrollment, 1, 0, func(q fixtureAccount, qraw string, qp Pending) error {
		_, b, c := good(q, qraw, qp)
		_, err := a.CompleteEnrollment(ctx, refuseAddr, q.id, qraw[:20], b, newPw, c)
		return err
	})
	refuse("another account's page", ErrEnrollment, 1, 0, func(q fixtureAccount, qraw string, qp Pending) error {
		_, _, c := good(q, qraw, qp)
		_, err := a.CompleteEnrollment(ctx, refuseAddr, q.id, qraw, pend.Blob, newPw, c)
		return err
	})
	refuse("a wrong first code", ErrCodeRejected, 1, 0, func(q fixtureAccount, qraw string, qp Pending) error {
		r, b, _ := good(q, qraw, qp)
		_, err := a.CompleteEnrollment(ctx, refuseAddr, q.id, r, b, newPw, wrongCodeAt(qp.Secret.reveal(), now))
		return err
	})
	refuse("a token expired by the database's clock", ErrEnrollment, 1, 1, func(q fixtureAccount, qraw string, qp Pending) error {
		if _, err := tx.Exec(ctx, `UPDATE platform_admins SET enroll_issued_at = clock_timestamp() - interval '40 minutes',
		                              enroll_expires_at = clock_timestamp() - interval '10 minutes' WHERE id = $1`, q.id); err != nil {
			t.Fatalf("expire the token: %v", err)
		}
		r, b, c := good(q, qraw, qp)
		_, err := a.CompleteEnrollment(ctx, refuseAddr, q.id, r, b, newPw, c)
		return err
	})
}

// TestPendingBlob_IsNotTheStoredEnvelope: the page's sealed secret and the stored
// envelope are different AADs, so neither can stand in for the other (enrollment.go's
// decision) -- and the blob's own guarantees: it opens for its account until its expiry
// (one second before: yes; at the expiry: no), and not for another account, another
// KEK, or a changed byte. And the two are sealed under DIFFERENT KEYS (OP-6 12c, the
// security audit's LOW finding): the blob under HMAC(KEK, pendingKeyLabel) -- the label
// pinned as a literal, which the external leak test restates to search for the key --
// never under the KEK itself, which only the stored envelope uses.
func TestPendingBlob_IsNotTheStoredEnvelope(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	clock := start
	a, _ := newAuth(t, nopStore{t}, start)
	a.now = func() time.Time { return clock }
	id := uuid.New()
	pend, err := a.BeginEnrollment(id)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	sec := append([]byte(nil), pend.Secret.reveal()...)
	key, err := a.openPending(id, pend.Blob)
	if err != nil || string(key) != string(sec) {
		t.Fatalf("a fresh blob does not open to its secret: %v", err)
	}
	raw, err := strictB64.DecodeString(pend.Blob.RevealForForm())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := sun.Open(a.keys.kek.bytes(), id[:], raw[pendingExpOff:]); err == nil {
		t.Fatal("the page's blob opened as a STORED envelope (AAD = the id alone)")
	}
	exp := int64(binary.BigEndian.Uint64(raw[:pendingExpOff]))
	if _, err := sun.Open(a.keys.kek.bytes(), pendingAAD(id, exp), raw[pendingExpOff:]); err == nil {
		t.Fatal("the page's blob opened under the KEK ITSELF: BeginEnrollment, reachable without a credential, seals under the stored envelopes' key")
	}
	lm := hmac.New(sha256.New, a.keys.kek.bytes())
	_, _ = lm.Write([]byte("taptime/operator/enrollment-pending/v1/key-derivation"))
	if got, err := sun.Open(lm.Sum(nil), pendingAAD(id, exp), raw[pendingExpOff:]); err != nil || string(got) != string(sec) {
		t.Fatalf("the page's blob does not open under HMAC(KEK, the pinned label): %v", err)
	}
	stored, err := sun.Seal(a.keys.kek.bytes(), id[:], sec)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	asBlob := strictB64.EncodeToString(append(append([]byte(nil), raw[:pendingExpOff]...), stored...))
	if _, err := a.openPending(id, PendingBlobFromForm(asBlob)); err == nil {
		t.Fatal("a stored envelope opened as the page's blob")
	}
	clock = start.Add(pendingTTL - time.Second)
	if _, err := a.openPending(id, pend.Blob); err != nil {
		t.Fatalf("one second before its expiry the blob was refused: %v", err)
	}
	clock = start.Add(pendingTTL)
	if _, err := a.openPending(id, pend.Blob); err == nil {
		t.Fatal("the blob opened at its expiry")
	}
	clock = start
	if _, err := a.openPending(uuid.New(), pend.Blob); err == nil {
		t.Fatal("the blob opened for another account")
	}
	other, _ := newAuth(t, nopStore{t}, start)
	if _, err := other.openPending(id, pend.Blob); err == nil {
		t.Fatal("the blob opened under another KEK")
	}
	v := pend.Blob.RevealForForm()
	for i := range v {
		b := []byte(v)
		b[i] ^= 0x01
		if _, err := a.openPending(id, PendingBlobFromForm(string(b))); err == nil {
			t.Fatalf("a blob with character %d changed opened", i)
		}
	}
	for _, bad := range []string{"", "!!", strings.Repeat("A", 600)} {
		if _, err := a.openPending(id, PendingBlobFromForm(bad)); err == nil {
			t.Errorf("a malformed blob (%d characters) opened", len(bad))
		}
	}
	if _, err := a.BeginEnrollment(uuid.Nil); err == nil {
		t.Error("a page was sealed for the nil account id")
	}
}
