package operatorauth

// detached_db_test.go -- OP-14 phase E against PostgreSQL: the password-less rows, the
// code step and the enrollment's last statement written through the real definers on a
// context DETACHED from the request, so a client that leaves -- before the step, after the
// lookup, during the comparison -- changes neither the rows nor the lock counter. The
// harness's rolled-back transaction (harness_db_test.go's txStore); NOTHING HERE COMMITS.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// kindRows counts the rows of kind whose target is id (uuid.Nil: no target).
func kindRows(t *testing.T, ctx context.Context, q querier, kind string, id uuid.UUID) int64 {
	t.Helper()
	return countInt(t, ctx, q, `SELECT count(*) FROM operator_audit_log WHERE kind = $1
	                              AND target_admin_id IS NOT DISTINCT FROM NULLIF($2::uuid, '00000000-0000-0000-0000-000000000000')`, kind, id)
}

// sessionsOf counts id's sessions.
func sessionsOf(t *testing.T, ctx context.Context, q querier, id uuid.UUID) int64 {
	t.Helper()
	return countInt(t, ctx, q, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, id)
}

// lookupLeaves is a Store whose code-step lookup is followed by its client leaving: leave,
// when set, is called right after OperatorByID answers (the security audit's P4 shape --
// the client hung up between the account budget's charge and the row).
type lookupLeaves struct {
	Store
	leave *context.CancelFunc
}

func (s lookupLeaves) OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	acc, err := s.Store.OperatorByID(ctx, id)
	if l := *s.leave; l != nil {
		l()
	}
	return acc, err
}

// refuseKinds sends the row writes of the kinds it names to a read-only savepoint (the
// server's own 25006) and everything else to the real store.
type refuseKinds struct {
	Store
	ro    Store
	kinds map[db.OperatorAuthEvent]bool
}

func (s refuseKinds) RecordOperatorAuthEvent(ctx context.Context, k db.OperatorAuthEvent, e string, id uuid.UUID) error {
	if s.kinds[k] {
		return s.ro.RecordOperatorAuthEvent(ctx, k, e, id)
	}
	return s.Store.RecordOperatorAuthEvent(ctx, k, e, id)
}

// isSentinel reports whether err is one of the answers a caller renders as a page.
func isSentinel(err error) bool {
	for _, s := range []error{ErrRefused, ErrThrottled, ErrChallenge, ErrCodeRejected, ErrLocked, ErrNoSession, ErrEnrollment, ErrWeakPassword} {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// TestDetachedSignIn_APasswordlessRowOutlivesItsClient is the OP-14 E card's first two
// arms through the real definer: the client hangs up WHILE the comparison runs (the
// request's context is cancelled inside the comparer -- net/http's answer to a half-closed
// connection; the PREMISE is measured). A wrong password still leaves its login_failed row
// naming the operator, an unknown address its unknown_email row naming nobody, and a
// pending account's address its unknown_email row naming that account (00026's D3); each is
// answered ErrRefused, each charges the process-wide cap once, nothing is logged, and no row
// carries the operator's address or the client's. Before the detach (the security audit's
// P8, over TCP) the wrong password left NO row and was answered 500, while a right one got
// its challenge.
func TestDetachedSignIn_APasswordlessRowOutlivesItsClient(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	a, logs := newAuth(t, txStore(t, tx), dbStepTime(t, ctx, tx))
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	pending, _ := newPendingAccount(t, ctx, tx, 30*time.Minute)
	var hangUp context.CancelFunc
	a.compareFn = func(d, p []byte) error {
		hangUp()
		return exactCompare(d, p)
	}
	unknown := "nobody-" + uuid.NewString()[:8] + "@example.test"
	tests := []struct {
		name, email, given, kind string
		target                   uuid.UUID
	}{
		{"a wrong password", acc.email, fixturePassphrase + "!", "login_failed", acc.id},
		{"an unknown address", unknown, fixturePassphrase, "unknown_email", uuid.Nil},
		{"a pending account's address", pending.email, fixturePassphrase, "unknown_email", pending.id},
	}
	var addrs []string
	for i, tc := range tests {
		rctx, cancel := context.WithCancel(ctx)
		hangUp = cancel
		addr := fmt.Sprintf("198.51.100.%d", 60+i)
		addrs = append(addrs, addr)
		before, kindBefore := auditRows(t, ctx, tx), kindRows(t, ctx, tx, tc.kind, tc.target)
		c, err := a.Password(rctx, addr, tc.email, tc.given)
		gone := rctx.Err() != nil
		cancel()
		if !gone {
			t.Fatalf("%s: PREMISE: the request's context was not cancelled during the comparison", tc.name)
		}
		if !errors.Is(err, ErrRefused) || c.reveal() != "" {
			t.Fatalf("%s, its client gone: %v (challenge minted=%v); want ErrRefused and none", tc.name, err, c.reveal() != "")
		}
		if d, dk := auditRows(t, ctx, tx)-before, kindRows(t, ctx, tx, tc.kind, tc.target)-kindBefore; d != 1 || dk != 1 {
			t.Fatalf("%s, its client gone: %+d row(s), %+d of kind %s with its target; want +1 and +1", tc.name, d, dk, tc.kind)
		}
	}
	if spent := spentIn(a.limits.auditCap, ""); spent != len(tests) {
		t.Fatalf("the process-wide cap holds %d charge(s), want %d -- one per arm", spent, len(tests))
	}
	if logs.Len() != 0 {
		t.Fatalf("below the cap the aborted requests logged %d byte(s), want none", logs.Len())
	}
	text := strings.ToLower(rowText(t, ctx, tx, acc.id) + rowText(t, ctx, tx, pending.id))
	for _, s := range append(addrs, strings.ToLower(acc.email), strings.ToLower(pending.email), strings.ToLower(unknown)) {
		if strings.Contains(text, s) {
			t.Fatal("a row carries an operator's address or the client's")
		}
	}
}

// TestDetachedSignIn_ACodeAttemptOutlivesItsClient is the card's "kesilen TOTP denemesi →
// totp_failed + sayaç +1" through the real definers, for a client gone BEFORE the step began
// (the request's context already done when TOTP is called -- a FIN that lands with the
// request) and one gone AFTER the lookup (the security audit's P4 shape). A wrong code
// leaves its totp_failed row and moves the counter by one, answered ErrCodeRejected; the
// right code then opens its session and writes its 'login' row, the counter back at 0 --
// the attempt ran to its end. Before the detach (P4) such attempts left no row, no counter
// and no session, and spent the account's budget.
//
// 2nd round, the third eye's F4 (LE1, measured here): that session's token reached nobody,
// and the person who types the same code again is refused -- the database's replay guard
// (its step is spent): a totp_failed row, the counter up by one, ErrCodeRejected (401 on the
// surface). The orphan session stays until its idle limit.
//
// 3rd round, the security audit's F1 and F2:
//   - a third client shape, a request PAST ITS DEADLINE (its context's error is
//     DeadlineExceeded -- a body that trickled past httpx.RequestTimeout): the detach drops
//     the deadline with the cancellation (LE8), so the attempt still runs to its end. A
//     detach that honoured the deadline (min(grace, time left)) refused every statement --
//     503, no row, the budget spent -- and the package stayed green (the audit's S09);
//   - the replayed right code -- refused by the database on an account that is NOT locked --
//     is sent first in the loop's client shape and only then by a client that stays: both
//     are ErrCodeRejected with their row. Its row on the request's context (the audit's S04)
//     stayed green while only the locked arm's row was driven with the client gone.
func TestDetachedSignIn_ACodeAttemptOutlivesItsClient(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	var leave context.CancelFunc
	a, logs := newAuth(t, lookupLeaves{Store: txStore(t, tx), leave: &leave}, now)
	for _, when := range []string{"before the step", "after the lookup", "past its deadline"} {
		acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
		c := challengeFor(t, a, acc)
		attempt := func(code string) (Issued, error) {
			t.Helper()
			rctx, cancel := context.WithCancel(ctx)
			defer cancel()
			switch when {
			case "before the step":
				cancel()
			case "after the lookup":
				leave = cancel
			case "past its deadline":
				rctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			iss, err := a.TOTP(rctx, c, code)
			leave = nil
			if rctx.Err() == nil || (when == "past its deadline") != errors.Is(rctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("%s: PREMISE: the request's context is not done the way this shape needs (%v)", when, rctx.Err())
			}
			return iss, err
		}
		if _, err := attempt(wrongCodeAt(acc.key, now)); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("a wrong code, its client gone %s: %v, want ErrCodeRejected", when, err)
		}
		if r, f, s := kindRows(t, ctx, tx, "totp_failed", acc.id), failures(t, ctx, tx, acc.id), sessionsOf(t, ctx, tx, acc.id); r != 1 || f != 1 || s != 0 {
			t.Fatalf("a wrong code, its client gone %s: %d totp_failed row(s), counter %d, %d session(s); want 1, 1, 0", when, r, f, s)
		}
		now = resyncAuth(t, ctx, tx, a)
		iss, err := attempt(codeAt(acc.key, now))
		if err != nil || iss.Token.reveal() == "" || iss.AdminID != acc.id {
			t.Fatalf("the right code, its client gone %s: %v (token=%v); want the session issued", when, err, iss.Token.reveal() != "")
		}
		if got, f, s := operatorRows(t, ctx, tx, acc.id), failures(t, ctx, tx, acc.id), sessionsOf(t, ctx, tx, acc.id); strings.Join(got, ",") != "totp_failed,login" || f != 0 || s != 1 {
			t.Fatalf("the right code, its client gone %s: rows %v, counter %d, %d session(s); want [totp_failed login], 0, 1", when, got, f, s)
		}
		if _, err := attempt(codeAt(acc.key, now)); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("the same right code again, its client %s: %v, want ErrCodeRejected (a replay)", when, err)
		}
		stepStillBound(t, ctx, tx, now)
		if got, f, s := operatorRows(t, ctx, tx, acc.id), failures(t, ctx, tx, acc.id), sessionsOf(t, ctx, tx, acc.id); strings.Join(got, ",") != "totp_failed,login,totp_failed" || f != 1 || s != 1 {
			t.Fatalf("the same right code again, its client %s: rows %v, counter %d, %d session(s); want [totp_failed login totp_failed], 1, 1", when, got, f, s)
		}
		if _, err := a.TOTP(ctx, c, codeAt(acc.key, now)); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("the same right code again, its client staying (%s): %v, want ErrCodeRejected (a replay)", when, err)
		}
		stepStillBound(t, ctx, tx, now)
		if got, f, s := operatorRows(t, ctx, tx, acc.id), failures(t, ctx, tx, acc.id), sessionsOf(t, ctx, tx, acc.id); strings.Join(got, ",") != "totp_failed,login,totp_failed,totp_failed" || f != 2 || s != 1 {
			t.Fatalf("the same right code a third time (%s): rows %v, counter %d, %d session(s); want [totp_failed login totp_failed totp_failed], 2, 1", when, got, f, s)
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("the code step logged %d byte(s), want none", logs.Len())
	}
}

// TestDetachedSignIn_AbortedCodesAreCountedAndLock is the card's "N kesilen deneme kilidi
// tetikler (sayaç N)" and the security audit's P4 replayed. One challenge, every attempt's
// client gone before the step: five wrong codes are five totp_failed rows and a counter of
// 5 -- the account is locked (the database's ~900 s). While locked, the RIGHT code is
// refused for the lock and written as 'locked', answered ErrLocked -- the same answer as a
// wrong one, which moves the counter on to 6: an aborted attempt cannot tell a right code
// from a wrong one under the lock. The account budget is then spent to its end the same way:
// every charge is a row (accountLimit charges, accountLimit rows), and the owner's right
// code is ErrThrottled. P4 measured the same sequence before the detach: 0 rows, counter 0,
// no session, the budget spent 10 of 10 -- a lock-out nothing recorded. It is still a
// lock-out ("parolayı bilen biri tasarım gereği kilitleyebilir"), now with its trail.
func TestDetachedSignIn_AbortedCodesAreCountedAndLock(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, logs := newAuth(t, txStore(t, tx), now)
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	c := challengeFor(t, a, acc)
	gone, leave := context.WithCancel(ctx)
	leave()
	lockedFor := func() float64 {
		var s float64
		if err := tx.QueryRow(ctx, `SELECT coalesce(extract(epoch FROM totp_locked_until - clock_timestamp()), 0)::float8
		                              FROM platform_admins WHERE id = $1`, acc.id).Scan(&s); err != nil {
			t.Fatalf("read the lock: %v", err)
		}
		return s
	}
	for i := 1; i <= 5; i++ {
		if _, err := a.TOTP(gone, c, wrongCodeAt(acc.key, now)); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("aborted wrong code %d: %v, want ErrCodeRejected", i, err)
		}
		if r, f := kindRows(t, ctx, tx, "totp_failed", acc.id), failures(t, ctx, tx, acc.id); r != int64(i) || f != int64(i) {
			t.Fatalf("after %d aborted wrong code(s): %d row(s), counter %d; want %d and %d -- N aborted attempts are N counted ones", i, r, f, i, i)
		}
	}
	if w := lockedFor(); w < 14*60+50 || w > 15*60 {
		t.Fatalf("five aborted wrong codes: locked for %.0f s, want ~900 s", w)
	}
	now = resyncAuth(t, ctx, tx, a)
	if _, err := a.TOTP(gone, c, codeAt(acc.key, now)); !errors.Is(err, ErrLocked) {
		t.Fatalf("the aborted RIGHT code while locked: %v, want ErrLocked", err)
	}
	if r, f, s := kindRows(t, ctx, tx, "locked", acc.id), failures(t, ctx, tx, acc.id), sessionsOf(t, ctx, tx, acc.id); r != 1 || f != 5 || s != 0 {
		t.Fatalf("the aborted right code while locked: %d 'locked' row(s), counter %d, %d session(s); want 1, 5, 0", r, f, s)
	}
	if _, err := a.TOTP(gone, c, wrongCodeAt(acc.key, now)); !errors.Is(err, ErrLocked) {
		t.Fatalf("an aborted wrong code while locked: %v, want ErrLocked -- the right code's answer", err)
	}
	if f := failures(t, ctx, tx, acc.id); f != 6 {
		t.Fatalf("an aborted wrong code while locked: counter %d, want 6", f)
	}
	for spentIn(a.limits.account, acc.id.String()) < accountLimit {
		if _, err := a.TOTP(gone, c, wrongCodeAt(acc.key, now)); !errors.Is(err, ErrLocked) {
			t.Fatalf("an aborted wrong code while locked: %v, want ErrLocked", err)
		}
	}
	rows := kindRows(t, ctx, tx, "totp_failed", acc.id) + kindRows(t, ctx, tx, "locked", acc.id)
	if rows != accountLimit || failures(t, ctx, tx, acc.id) != accountLimit-1 || sessionsOf(t, ctx, tx, acc.id) != 0 {
		t.Fatalf("the account budget spent by aborted attempts: %d row(s), counter %d, %d session(s); want %d (one per charge), %d, 0",
			rows, failures(t, ctx, tx, acc.id), sessionsOf(t, ctx, tx, acc.id), accountLimit, accountLimit-1)
	}
	if _, err := a.TOTP(ctx, c, codeAt(acc.key, now)); !errors.Is(err, ErrThrottled) {
		t.Fatalf("the owner's right code after the budget: %v, want ErrThrottled", err)
	}
	if after := kindRows(t, ctx, tx, "totp_failed", acc.id) + kindRows(t, ctx, tx, "locked", acc.id); after != rows {
		t.Fatalf("the throttled attempt wrote %+d row(s)", after-rows)
	}
	if logs.Len() != 0 {
		t.Fatalf("the code step logged %d byte(s), want none", logs.Len())
	}
}

// TestDetachedSignIn_AnAbortedEnrollmentStillLeavesItsRow is the card's enrollment arm
// through the real definers, every request's client gone before the step: a malformed link
// token, a wrong first code and a well-formed token the database refuses (it passes every
// check Go can make and spends both enrollment budgets) each leave ONE enrollment_failed row
// naming the pending account and are answered as a client that stayed is (ErrEnrollment,
// ErrCodeRejected, ErrEnrollment); the account stays pending. Then the valid link completes
// -- op_complete_enrollment runs detached too: the account is active, its 'enrollment' row
// and its first session exist, and the session token is returned (it reaches a client only
// if the client is still reading).
//
// 2nd round, the third eye's F4 (LE1, measured here): the person whose client left opens
// the link again and is refused -- the token is used (ErrEnrollment, the surface's "been
// used already. Ask for a new one.", one more enrollment_failed row) -- while signing in
// with the password the enrollment set already works (a challenge and its password_ok row).
func TestDetachedSignIn_AnAbortedEnrollmentStillLeavesItsRow(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, logs := newAuth(t, txStore(t, tx), now)
	// The digest is not this test's subject (its cost is TestEnrollment_CompletesOnceAndTheStoredEnvelopeOpens's):
	// the shared fixture digest saves two cost-12 runs and is one the definer accepts.
	a.digestFn = func(string) (string, error) { return fixtureDigest() }
	p, raw := newPendingAccount(t, ctx, tx, 30*time.Minute)
	pend, err := a.BeginEnrollment(p.id)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	secret := pend.Secret.reveal()
	foreign := must(NewEnrollmentToken()).RevealForLink()
	// The password the enrollment sets is the fixture passphrase: digestFn stores its digest,
	// so the sign-in after the enrollment compares against what the enrollment wrote.
	newPw := fixturePassphrase
	gone, leave := context.WithCancel(ctx)
	leave()
	tests := []struct {
		name, token, code string
		want              error
	}{
		{"a malformed link token", "short", codeAt(secret, now), ErrEnrollment},
		{"a wrong first code", raw, wrongCodeAt(secret, now), ErrCodeRejected},
		{"a well-formed token the database refuses", foreign, codeAt(secret, now), ErrEnrollment},
	}
	for i, tc := range tests {
		before := kindRows(t, ctx, tx, "enrollment_failed", p.id)
		addr := fmt.Sprintf("192.0.2.%d", 90+i)
		if _, err := a.CompleteEnrollment(gone, addr, p.id, tc.token, pend.Blob, newPw, tc.code); !errors.Is(err, tc.want) {
			t.Fatalf("%s, its client gone: %v, want %v", tc.name, err, tc.want)
		}
		if d := kindRows(t, ctx, tx, "enrollment_failed", p.id) - before; d != 1 {
			t.Fatalf("%s, its client gone: %+d enrollment_failed row(s), want +1", tc.name, d)
		}
	}
	if n := countInt(t, ctx, tx, `SELECT count(*) FROM platform_admins WHERE id = $1 AND status = 'pending'`, p.id); n != 1 {
		t.Fatal("PREMISE: the refusals changed the pending account")
	}
	now = resyncAuth(t, ctx, tx, a)
	iss, err := a.CompleteEnrollment(gone, "192.0.2.99", p.id, raw, pend.Blob, newPw, codeAt(secret, now))
	if err != nil || iss.Token.reveal() == "" || iss.AdminID != p.id {
		t.Fatalf("the valid link, its client gone: %v; want the enrollment completed and its session issued", err)
	}
	active := countInt(t, ctx, tx, `SELECT count(*) FROM platform_admins WHERE id = $1 AND status = 'active'`, p.id)
	enrolled := countInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'enrollment' AND actor_admin_id = $1`, p.id)
	if active != 1 || enrolled != 1 || sessionsOf(t, ctx, tx, p.id) != 1 {
		t.Fatalf("the valid link, its client gone: active=%d, %d 'enrollment' row(s), %d session(s); want 1, 1, 1", active, enrolled, sessionsOf(t, ctx, tx, p.id))
	}
	failedBefore := kindRows(t, ctx, tx, "enrollment_failed", p.id)
	now = resyncAuth(t, ctx, tx, a)
	if _, err := a.CompleteEnrollment(ctx, "192.0.2.98", p.id, raw, pend.Blob, newPw, codeAt(secret, now)); !errors.Is(err, ErrEnrollment) {
		t.Fatalf("the same link again, its client staying: %v, want ErrEnrollment (the token is used)", err)
	}
	if d := kindRows(t, ctx, tx, "enrollment_failed", p.id) - failedBefore; d != 1 {
		t.Fatalf("the same link again: %+d enrollment_failed row(s), want +1", d)
	}
	if c, err := a.Password(ctx, "192.0.2.97", p.email, newPw); err != nil || c.reveal() == "" {
		t.Fatalf("signing in with the enrollment's password after the used link: %v; want a challenge", err)
	}
	if all, _ := passwordOKRows(t, ctx, tx, p.id); all != 1 {
		t.Fatalf("the sign-in after the enrollment: %d 'password_ok' row(s), want 1", all)
	}
	if logs.Len() != 0 {
		t.Fatalf("the enrollment logged %d byte(s), want none", logs.Len())
	}
}

// TestDetachedSignIn_AnUnwrittenRowFailsTheStepAndCountsNothing is the card's md. 4 on the
// real server's refusal (a read-only savepoint: 25006), every client gone before the row
// (the password step's during its comparison, the others before the step): a row that
// cannot be written is the step's ERROR -- none of the sentinels a
// caller renders as an answer about the credential (so the surface answers 503, the same
// for every arm) -- never a success and never the refusal it would have been. Measured per
// arm: a wrong password (no challenge, no row; the shared cap keeps its charge, as before
// the detach); a wrong code (no session, no row, the counter unmoved -- a checked guess that
// was not counted is not answered "wrong"); the right code while locked (no session, no
// row, the counter unmoved); a malformed enrollment link (no row). No error carries the
// operator's address, the client's, the passphrase, the code or the operator's id.
// CONTROL: the same wrong code through the real store is ErrCodeRejected with its row.
func TestDetachedSignIn_AnUnwrittenRowFailsTheStepAndCountsNothing(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	live := txStore(t, tx)
	a, _ := newAuth(t, refuseKinds{Store: live, ro: readOnlyStore(t, tx), kinds: map[db.OperatorAuthEvent]bool{
		db.OperatorLoginFailed: true, db.OperatorTOTPFailed: true, db.OperatorLocked: true, db.OperatorEnrollmentFailed: true,
	}}, now)
	a.compareFn = exactCompare
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	locked := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_failures = 5, totp_locked_until = clock_timestamp() + interval '15 minutes' WHERE id = $1`, locked.id); err != nil {
		t.Fatalf("lock an account: %v", err)
	}
	gone, leave := context.WithCancel(ctx)
	leave()
	const addr = "203.0.113.55"
	check := func(name string, err error, secrets ...string) {
		t.Helper()
		if err == nil || isSentinel(err) || !strings.Contains(err.Error(), "SQLSTATE 25006") {
			t.Fatalf("%s, its row refused: %v; want the server's 25006 as the step's error, no sentinel", name, err)
		}
		for _, s := range append(secrets, addr, fixturePassphrase, acc.email, acc.id.String(), locked.id.String()) {
			if strings.Contains(err.Error(), s) {
				t.Fatalf("%s: the error carries an address, the passphrase, the code or an operator's id", name)
			}
		}
	}
	before := auditRows(t, ctx, tx)
	// The password step's lookup runs on the request's context (Password's comment), so its
	// client leaves during the comparison: the comparer cancels the request's context.
	rctx, cancel := context.WithCancel(ctx)
	a.compareFn = func(d, p []byte) error { cancel(); return exactCompare(d, p) }
	c, err := a.Password(rctx, addr, acc.email, fixturePassphrase+"!")
	cancel()
	a.compareFn = exactCompare
	check("a wrong password", err)
	if c.reveal() != "" || spentIn(a.limits.auditCap, "") != 1 {
		t.Fatalf("a wrong password whose row was refused: challenge minted=%v, the shared cap at %d; want none and 1", c.reveal() != "", spentIn(a.limits.auditCap, ""))
	}

	wrong := wrongCodeAt(acc.key, now)
	iss, err := a.TOTP(gone, challengeFor(t, a, acc), wrong)
	check("a wrong code", err, wrong)
	if iss.Token.reveal() != "" || failures(t, ctx, tx, acc.id) != 0 || sessionsOf(t, ctx, tx, acc.id) != 0 {
		t.Fatalf("a wrong code whose row was refused: session issued=%v, counter %d; want none and 0", iss.Token.reveal() != "", failures(t, ctx, tx, acc.id))
	}

	now = resyncAuth(t, ctx, tx, a)
	right := codeAt(locked.key, now)
	iss, err = a.TOTP(gone, challengeFor(t, a, locked), right)
	check("the right code while locked", err, right)
	if iss.Token.reveal() != "" || failures(t, ctx, tx, locked.id) != 5 || sessionsOf(t, ctx, tx, locked.id) != 0 {
		t.Fatalf("the right code while locked, its row refused: session issued=%v, counter %d; want none and 5", iss.Token.reveal() != "", failures(t, ctx, tx, locked.id))
	}

	p, _ := newPendingAccount(t, ctx, tx, 30*time.Minute)
	pend, perr := a.BeginEnrollment(p.id)
	if perr != nil {
		t.Fatalf("begin: %v", perr)
	}
	_, err = a.CompleteEnrollment(gone, addr, p.id, "short", pend.Blob, strings.Repeat("op14e enroll ", 2), codeAt(pend.Secret.reveal(), now))
	check("a malformed enrollment link", err)

	if d := auditRows(t, ctx, tx) - before; d != 0 {
		t.Fatalf("the refused rows left %d row(s) behind", d)
	}
	// CONTROL: the same refusal through the real store.
	a.store = live
	if _, err := a.TOTP(gone, challengeFor(t, a, acc), wrong); !errors.Is(err, ErrCodeRejected) {
		t.Fatalf("CONTROL: the wrong code through the real store: %v, want ErrCodeRejected", err)
	}
	if r, f := kindRows(t, ctx, tx, "totp_failed", acc.id), failures(t, ctx, tx, acc.id); r != 1 || f != 1 {
		t.Fatalf("CONTROL: %d totp_failed row(s), counter %d; want 1 and 1", r, f)
	}
}
