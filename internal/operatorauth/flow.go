package operatorauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/sun"
)

// The sign-in, the session check, the logout and the enrollment's last step (ADR 0020
// §3). Every entry point spends its budgets FIRST (limits.go) and then does its work;
// the order below is the property the OP-6 acceptance names.

// Password is the first step: an address and a password in, a login challenge out.
//
// Every arm pays ONE bcrypt comparison at Cost and, under the audit cap, writes ONE
// row: an active operator with a wrong password (login_failed), and an address the
// lookup does not return -- unknown, pending or disabled, which the database's policy
// makes one answer (00026) -- compared against the dummy digest (unknown_email; the
// definer still names the pending or disabled account as the target, ADR 0021 §1's
// D3). The caller receives ErrRefused for all of them: ADR 0020 §3's "same answer,
// same time".
//
// A RIGHT password writes ONE 'password_ok' row under its operator's own cap
// (recordPasswordOK) BEFORE the challenge exists; a row that cannot be written is this
// step's error, and no challenge is returned.
//
// addr is the client's rate key (OP-8 resolves it). It is used for the work budget and
// is written nowhere: no row carries an address (ADR 0021 §1).
//
// WHICH STATEMENTS FOLLOW THE CLIENT (OP-14 phase E): only the lookup. A request whose
// client has gone before the lookup answers compares nothing and writes nothing -- no
// guess was checked, and the budget it spent is its own address's. Every row written
// after a comparison is written detached (recordPasswordless, recordPasswordOK): a
// comparison that ran is a fact whether or not its client is still listening.
func (a *Authenticator) Password(ctx context.Context, addr, email, password string) (Challenge, error) {
	if !a.limits.work.spend(addr) {
		return Challenge{}, ErrThrottled
	}
	acc, err := a.store.OperatorByEmail(ctx, email)
	switch {
	case errors.Is(err, db.ErrNoOperator):
		a.compareDummy(password)
		if rerr := a.recordPasswordless(ctx, db.OperatorUnknownEmail, email, uuid.Nil); rerr != nil {
			return Challenge{}, rerr
		}
		return Challenge{}, ErrRefused
	case err != nil:
		return Challenge{}, fmt.Errorf("operatorauth: sign-in lookup: %w", err)
	}
	if !a.compare(acc.Digest.RevealForPasswordComparison(), password) {
		if rerr := a.recordPasswordless(ctx, db.OperatorLoginFailed, email, uuid.Nil); rerr != nil {
			return Challenge{}, rerr
		}
		return Challenge{}, ErrRefused
	}
	// 🔴 THE TRAIL COMES BEFORE THE CHALLENGE (OP-14 phase C; ADR 0021, "OP-14 C fazı
	// eki"). A right password whose second factor never completes is the "password known,
	// device missing" signal; until OP-14 it was one process-log line (OP-6 12c), since
	// 00031 it is a durable row. It is written HERE, after the comparison and before a
	// challenge exists, because the challenge is what the caller turns into the cookie: a
	// row written later -- at the code step, or after the cookie was set -- is missing
	// exactly when the code step never comes, and a write that failed after the cookie
	// would leave the code step without its trail. A row that cannot be written fails
	// this step (fail-closed): no challenge, so no cookie, and the error carries nothing
	// the request did. The price is that a database that refuses this write stops the
	// sign-in here -- the next step's op_open_session writes too, so a database that
	// cannot be written already stopped it there.
	if err := a.recordPasswordOK(ctx, acc.ID); err != nil {
		return Challenge{}, err
	}
	c, err := a.mintChallenge(acc.ID)
	if err != nil {
		return Challenge{}, err
	}
	return c, nil
}

// TOTP is the second step: the challenge the password step left, and a six-digit code.
// On success the database has advanced the account's step, opened an MFA-stamped
// session and written its 'login' row in ONE statement (op_open_session); the raw
// token is returned for the cookie.
//
// ORDER, AND WHY EACH STEP IS WHERE IT IS:
//  1. the challenge is verified (a MAC and a clock -- no database);
//  2. the ACCOUNT BUDGET is spent, before anything else touches the account: a refused
//     attempt is never evaluated, so it can neither write a totp_failed row nor move
//     the lock counter, and no guess escapes the counter either (limits.go);
//  3. the account is read -- ACTIVE only (a challenge outlives nothing: an account
//     disabled or reset since the password step is refused here);
//  4. the envelope is opened with AAD = the account's id: an envelope moved onto
//     another row does not open (ADR 0020 §1 md.2), and a wrong KEK does not open
//     either -- both are server faults, answered with an error and NO row, because
//     neither is the operator's failure to count;
//  5. the code is checked over the whole window in constant time (totp.go);
//  6. a wrong code writes ONE totp_failed row, which moves the database's counter in
//     the same statement (00026); a right code goes to op_open_session, where the
//     database decides replay, lock and clock (ADR 0020 §3: "Go kilit kararı vermez").
//     If it refuses a code Go accepted, that is recorded too -- as 'locked' when the
//     account was locked as read in step 3, otherwise as 'totp_failed' (a replayed or
//     out-of-clock code IS a failed attempt, and counting it keeps a replay from being
//     free).
//
// NO ADDRESS PARAMETER, deliberately: an address budget here would add nothing the two
// gates in front of it do not already give -- the flood budget (OP-8's gate, per
// address) prices every request, and a challenge that verifies was minted by a correct
// password, so the budget that matters is the account's.
//
// 🔴 FROM THE ACCOUNT BUDGET'S CHARGE ON, THE ATTEMPT RUNS TO ITS END WHATEVER THE CLIENT
// DOES (OP-14 phase E; ADR 0021, "OP-14 E uygulama notu"). Every statement after step 2
// -- the lookup, the failure row, op_open_session and the refused code's row -- runs on
// detach's context. Measured before (OP-14 phase C's security audit, P4, real Postgres):
// with the request's context, ten code attempts whose client hung up left ZERO rows, a
// lock counter of 0 and no session, and spent the account's budget ten of ten -- the
// owner's right code was then ErrThrottled, and nothing anywhere said why. Detaching only
// the failure row would not close it: a client that hangs up at once has its context
// cancelled before the lookup, and a lookup that fails writes nothing. So N attempts whose
// client left are N attempts the database counted (a wrong code moves the counter, a right
// one opens its session or, while locked, writes 'locked'), and a client that half-closes
// its connection gets the answer an attempt that stayed gets.
func (a *Authenticator) TOTP(ctx context.Context, c Challenge, code string) (Issued, error) {
	id, err := a.verifyChallenge(c)
	if err != nil {
		return Issued{}, ErrChallenge
	}
	if !a.limits.account.spend(id.String()) {
		return Issued{}, ErrThrottled
	}
	lctx, cancel := detach(ctx)
	acc, err := a.store.OperatorByID(lctx, id)
	cancel()
	switch {
	case errors.Is(err, db.ErrNoOperator):
		return Issued{}, ErrRefused
	case err != nil:
		return Issued{}, fmt.Errorf("operatorauth: second-step lookup: %w", err)
	}
	now := a.now()
	// A LABEL, NOT A DECISION: which row a refusal writes and which error it returns.
	// The refusal itself is the database's (op_open_session's own UPDATE).
	lockedNow := acc.LockedUntil != nil && acc.LockedUntil.After(now)

	key, err := sun.Open(a.keys.kek.bytes(), id[:], acc.Sealed.RevealForOpen())
	if err != nil {
		return Issued{}, errors.New("operatorauth: the stored TOTP envelope does not open under this KEK and account")
	}
	defer sun.Zero(key)

	step, ok := verifyCode(key, code, now)
	if !ok {
		// A failure row that cannot be written is the step's ERROR, never ErrCodeRejected:
		// the guess was checked and not counted, and answering it as a refusal would tell
		// its sender the code was wrong without the counter knowing (ADR 0021, "OP-14 E
		// uygulama notu", md. 4).
		rctx, cancel := detach(ctx)
		defer cancel()
		if rerr := a.store.RecordOperatorAuthEvent(rctx, db.OperatorTOTPFailed, "", id); rerr != nil {
			return Issued{}, fmt.Errorf("operatorauth: record a failed code: %w", rerr)
		}
		if lockedNow {
			return Issued{}, ErrLocked
		}
		return Issued{}, ErrCodeRejected
	}

	tok, err := newSessionToken()
	if err != nil {
		return Issued{}, err
	}
	h, err := tok.hash(a.keys.tokenKey.bytes())
	if err != nil {
		return Issued{}, err
	}
	// Detached too: under the request's context a right code whose client left would get
	// an error where a wrong one gets its refusal -- and while the account is locked that
	// difference alone would say which code was right.
	octx, cancel := detach(ctx)
	err = a.store.OpenOperatorSession(octx, id, h, step)
	cancel()
	switch {
	case err == nil:
		return Issued{Token: tok, AdminID: id}, nil
	case errors.Is(err, db.ErrOperatorRefused):
		kind, out := db.OperatorTOTPFailed, ErrCodeRejected
		if lockedNow {
			kind, out = db.OperatorLocked, ErrLocked
		}
		rctx, cancel := detach(ctx)
		defer cancel()
		if rerr := a.store.RecordOperatorAuthEvent(rctx, kind, "", id); rerr != nil {
			return Issued{}, fmt.Errorf("operatorauth: record a refused code: %w", rerr)
		}
		return Issued{}, out
	default:
		return Issued{}, fmt.Errorf("operatorauth: open the session: %w", err)
	}
}

// Verify is the session gate's check: the cookie's token, hashed under the operator's
// key, through THE session predicate (op_touch_session -- 8 h absolute, 30 min idle,
// MFA stamped, not revoked, operator active, all by the database's wall clock, and
// last_used_at advanced in the same statement). Every refusal is ErrNoSession, a junk
// cookie included; nothing is written for a refusal (00026's decision: a refused
// session probe is not an audit row -- m10-platform.md, OP-5 madde 9).
func (a *Authenticator) Verify(ctx context.Context, t SessionToken) (Identity, error) {
	h, err := t.hash(a.keys.tokenKey.bytes())
	if err != nil {
		return Identity{}, ErrNoSession
	}
	s, err := a.store.TouchOperatorSession(ctx, h)
	switch {
	case err == nil:
		return Identity{SessionID: s.SessionID, AdminID: s.AdminID}, nil
	case errors.Is(err, db.ErrOperatorRefused):
		return Identity{}, ErrNoSession
	default:
		return Identity{}, fmt.Errorf("operatorauth: session check: %w", err)
	}
}

// SessionHash is the value the operator's session-carrying op_* functions take as their
// session argument: the token hashed under the operator's key -- the one Verify hands
// op_touch_session. The definer resolves it through the same predicate on every call
// (ADR 0021 §2 i), so holding it is not a session check; the HTTP surface asks for it
// behind its session gate, for the calls that act on the session (OP-10: the legal
// screen's version list and publication). A token of the wrong shape is ErrNoSession,
// and the error names neither the token nor the hash.
//
// The hash is on ADR 0020 §5's never-log list with the token itself: the caller's job is
// to hand it to its store (internal/handler/operator's leak test searches it, G8, on the
// legal screen's arms).
func (a *Authenticator) SessionHash(t SessionToken) (string, error) {
	h, err := t.hash(a.keys.tokenKey.bytes())
	if err != nil {
		return "", ErrNoSession
	}
	return h, nil
}

// Logout revokes the session through the same predicate and writes its 'logout' row in
// one statement (op_close_session). A dead session is ErrNoSession.
func (a *Authenticator) Logout(ctx context.Context, t SessionToken) error {
	h, err := t.hash(a.keys.tokenKey.bytes())
	if err != nil {
		return ErrNoSession
	}
	err = a.store.CloseOperatorSession(ctx, h)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, db.ErrOperatorRefused):
		return ErrNoSession
	default:
		return fmt.Errorf("operatorauth: sign-out: %w", err)
	}
}

// CompleteEnrollment is the enrollment's last step: the account id and the raw token
// from the link, the pending blob from the form, the new password and the first code.
// On success the account is active and its first session is open (op_complete_
// enrollment, one statement); the raw session token is returned for the cookie.
//
// ORDER: the per-address work budget; the password rule (a person's typing error --
// answered before anything is spent or written); the token's shape; the blob (expiry,
// account binding, integrity); the first code. Only a request that passed all of those
// spends its address's share of the enrollment budget (enrollAddrLimit) and then the
// PROCESS-WIDE enrollment budget, and pays for the digest and the seal --
// the cost ADR 0021 sınır 12 names, which the database cannot pre-check because
// tappa_operator cannot read a token hash. Every refusal after the password rule writes
// ONE enrollment_failed row under the audit cap, detached from the request
// (recordPasswordless); the definer names the account if the id exists.
//
// The token's validity -- right account, pending, unused, unexpired by the database's
// clock -- is decided ONLY by op_complete_enrollment; Go never sees a token hash.
func (a *Authenticator) CompleteEnrollment(ctx context.Context, addr string, id uuid.UUID, rawToken string,
	b PendingBlob, password, code string) (Issued, error) {
	if !a.limits.work.spend(addr) {
		return Issued{}, ErrThrottled
	}
	if err := checkPasswordPolicy(password); err != nil {
		return Issued{}, err
	}
	if id == uuid.Nil || !wellFormed(rawToken) {
		return Issued{}, a.enrollmentRefused(ctx, id, ErrEnrollment)
	}
	key, err := a.openPending(id, b)
	if err != nil {
		return Issued{}, a.enrollmentRefused(ctx, id, ErrEnrollment)
	}
	defer sun.Zero(key)
	step, ok := verifyCode(key, code, a.now())
	if !ok {
		return Issued{}, a.enrollmentRefused(ctx, id, ErrCodeRejected)
	}
	if !a.limits.enrollAddr.spend(addr) {
		return Issued{}, ErrThrottled
	}
	if !a.limits.enroll.spend("") {
		return Issued{}, ErrThrottled
	}
	digest, err := a.digestFn(password)
	if err != nil {
		return Issued{}, err
	}
	sealed, err := sun.Seal(a.keys.kek.bytes(), id[:], key)
	if err != nil {
		return Issued{}, errors.New("operatorauth: sealing the TOTP key failed")
	}
	tok, err := newSessionToken()
	if err != nil {
		return Issued{}, err
	}
	h, err := tok.hash(a.keys.tokenKey.bytes())
	if err != nil {
		return Issued{}, err
	}
	// Detached (OP-14 phase E): the two enrollment budgets are spent, and the process-wide
	// one is everybody's. Under the request's context a client that hangs up during the
	// digest spends a share of it with no enrollment_failed row and is answered with an
	// error instead of the refusal; detached, the database decides -- a token it refuses is
	// a row and ErrEnrollment, a valid one completes (its cookie reaches the client only if
	// the client is still reading).
	cctx, cancel := detach(ctx)
	err = a.store.CompleteOperatorEnrollment(cctx, id, rawToken, digest, sealed, step, h)
	cancel()
	switch {
	case err == nil:
		return Issued{Token: tok, AdminID: id}, nil
	case errors.Is(err, db.ErrOperatorRefused):
		return Issued{}, a.enrollmentRefused(ctx, id, ErrEnrollment)
	default:
		return Issued{}, fmt.Errorf("operatorauth: complete the enrollment: %w", err)
	}
}

func (a *Authenticator) enrollmentRefused(ctx context.Context, id uuid.UUID, out error) error {
	if err := a.recordPasswordless(ctx, db.OperatorEnrollmentFailed, "", id); err != nil {
		return err
	}
	return out
}

// recordPasswordless writes one of the pre-session rows that need NO password, under
// the process-wide audit cap (limits.go). Past the cap it writes nothing and returns
// nil -- the request is still answered -- and the crossing is logged ONCE per window,
// with the kind and the numbers and nothing that came from the request.
//
// A database error writing the row is returned: "her giriş operator_audit_log'da"
// (OP-8's acceptance) is not something to drop silently when the table is reachable
// and refuses.
//
// THE WRITE IS DETACHED from the request (detach; OP-14 phase E). Measured before (OP-14
// phase C's security audit, P8, the real router over TCP): a client that half-closes its
// connection -- FIN after the request, still reading -- has its request's context
// cancelled by net/http while the comparison runs, and still receives the answer. With
// the request's context a wrong password then wrote NO login_failed row and was answered
// 500, while a right one got 303 and its challenge: a password guess, one per work-budget
// charge, that left no trail and read its result. The cap's charge above is unchanged:
// what changed is that a charge under the cap is now always followed by its write.
func (a *Authenticator) recordPasswordless(ctx context.Context, kind db.OperatorAuthEvent, email string, id uuid.UUID) error {
	if n := a.limits.auditCap.charge(""); n > auditCapLimit {
		if n == auditCapLimit+1 {
			a.log.Warn("operator pre-session audit rows suppressed for the rest of the window",
				"kind", string(kind), "cap", auditCapLimit, "window", auditCapPeriod.String())
		}
		return nil
	}
	wctx, cancel := detach(ctx)
	defer cancel()
	if err := a.store.RecordOperatorAuthEvent(wctx, kind, email, id); err != nil {
		return fmt.Errorf("operatorauth: record a pre-session failure: %w", err)
	}
	return nil
}

// SignInStatementGrace bounds EACH statement the sign-in runs detached from the request
// (detach; OP-14 phase E): a password-less row (recordPasswordless), the second step's
// statements after its account budget is charged (the lookup, op_open_session, the
// totp_failed or locked row), and the enrollment's op_complete_enrollment. The
// 'password_ok' row keeps its own FirstFactorRecordGrace (OP-14 phase C).
//
// 🔴 FIVE SECONDS, FirstFactorRecordGrace's number for its reasons -- one statement is
// milliseconds, the rest is a wait for a pooled connection; >= 1 s so the bound is not
// decorative -- and ONE BOUND PER STATEMENT, not per step: a slow lookup must not eat the
// time of the row after it, because a row starved of time is a checked guess without its
// count. The worst path in sequence is the second step's, THREE statements (the lookup,
// op_open_session refused, the refused code's row):
//
//	3 x 5 s = 15 s  <=  httpShutdownGrace (20 s), which Shutdown drains
//	enrollment: op_complete_enrollment + its refusal row = 2 x 5 s; password step: one row
//
// WithoutCancel drops the request's deadline (httpx.RequestTimeout) with its cancellation,
// so these bounds are the only ones: a step that starts in the request's last second can
// answer up to 15 s after that deadline. Exported only so cmd/tappa's shutdown-budget gate
// can hold the nesting (TestShutdownBudget_TheSignInStatementsNestInsideTheHTTPGrace).
const SignInStatementGrace = 5 * time.Second

// detach is the context of ONE sign-in statement that must run whether or not the client
// is still there: ctx's values, none of its cancellation or deadline, and
// SignInStatementGrace as its only bound.
func detach(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), SignInStatementGrace)
}

// FirstFactorRecordGrace bounds the 'password_ok' write, which runs DETACHED from the
// request (recordPasswordOK; OP-14 phase C, 2nd round).
//
// WHY DETACHED (the third eye's B1, measured through the real router over TCP and on
// the real database): with the request's own context, a client that hangs up while the
// comparison runs cancels r.Context(); the cap was charged, but pgxpool's Acquire
// answered the done context at once and nothing was sent -- ten such requests spent an
// operator's cap with ZERO rows, and the eleventh right password got 303, a challenge
// and no row. The holder of the password could empty the window's trail. A right
// password that was compared is a fact whether or not its client is still listening, so
// its row is written on its own clock.
//
// 🔴 FIVE SECONDS, THE SAME AS encode.DefaultRepairGrace AND tenant.RefusalRecordGrace,
// FOR THE SAME REASONS:
//
//	one INSERT through op_record_auth_event        milliseconds (dev, measured)
//	+ a wait for a pooled connection under load     the rest
//	floor: one round trip to Postgres with room     >= 1 s (a positive control)
//	ceiling: it runs inside an in-flight request,   <= httpShutdownGrace (20 s), so
//	  which Shutdown drains                          SIGTERM never cuts a trail short
//
// WithoutCancel drops the request's deadline (httpx.RequestTimeout, 30 s) with its
// cancellation, so this is the ONLY bound: the password step lasts at most the lookup,
// one cost-12 comparison and this. Exported only so cmd/tappa's shutdown-budget gate can
// hold the nesting (TestShutdownBudget_TheFirstFactorRecordNestsInsideTheHTTPGrace).
const FirstFactorRecordGrace = 5 * time.Second

// errFirstFactorPending is the password step's refusal when the operator's cap is full
// but its rows are not all WRITTEN -- writes still in flight, or about to fail and be given
// back (OP-14 phase C, 3rd round, F1). It is no sentinel: the caller answers it as a
// server failure (OP-8's handler: 503), and a person retries.
var errFirstFactorPending = errors.New("operatorauth: the operator's first-factor rows are not written yet")

// recordPasswordOK writes the 'password_ok' row of the operator whose right password
// was just compared, under THAT OPERATOR's cap (limits.go, firstFactorLimit) -- never
// under the process-wide audit cap, which password-less junk can fill. The account is
// named by its id alone: the definer refuses an address for this kind (00031, 22023),
// and no row carries one (ADR 0021 §1).
//
// THE CAP: the decision is what ONE take returns (one locked step). Past the cap the
// request is served WITHOUT a row only if the window's rows WRITTEN have reached the cap
// -- then the window's first such request is logged once (the kind, the operator's id
// and the numbers, nothing from the request). If the count is past the cap but its rows
// are NOT all written -- writes in flight, or writes that will fail and be given back
// (3rd round, F1: ten in-flight writes that then failed left a challenge and zero rows)
// -- the step FAILS CLOSED: the charge is given back and errFirstFactorPending returned.
// So "a challenge without its row" exists only in a window that holds firstFactorLimit
// written rows. Waiting for the writes in flight instead was weighed and not chosen: it
// needs a condition per window and a bound on the wait, for a case a person meets only by
// signing in more than firstFactorLimit times at once; a 503 costs them one retry.
//
// THE WRITE runs on a context detached from the request's cancellation, with its own
// bound (FirstFactorRecordGrace): an abandoned request still leaves its row. A row
// written is counted (wrote). If the write FAILS, the charge is GIVEN BACK (refund: the
// same lock as every charge, this take's own window only, never below zero) and the error
// is returned, never logged and dropped: the caller fails the step.
//
// EVERY FAIL-CLOSED REFUSAL names the operator once per window (refused; the 3rd round's
// F4): the handler's ERROR line for the 503 names no operator, so without this line
// nothing in the process log says whose sign-in a write outage stopped. An outage does
// not by itself tell a right password from a wrong one (5th round, D5, narrowed from the
// 3rd's wording; the closing audit's Q4 measured it): below the process-wide password-less
// cap (auditCapLimit) a wrong one's login_failed write fails too and it gets the same
// 503; only once that cap is spent is a wrong one answered 401 without its row while a
// right one still gets the 503.
func (a *Authenticator) recordPasswordOK(ctx context.Context, id uuid.UUID) error {
	kind, key := db.OperatorPasswordOK, id.String()
	n, written, window, firstOver := a.limits.firstFactor.take(key)
	if n > firstFactorLimit {
		if written >= firstFactorLimit {
			if firstOver {
				a.log.Warn("operator first-factor audit rows suppressed for this operator for the rest of the window",
					"kind", string(kind), "operator_id", key, "cap", firstFactorLimit, "window", firstFactorPeriod.String())
			}
			return nil
		}
		a.limits.firstFactor.refund(key, window)
		a.refused(kind, key, window)
		return errFirstFactorPending
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), FirstFactorRecordGrace)
	defer cancel()
	if err := a.store.RecordOperatorAuthEvent(wctx, kind, "", id); err != nil {
		a.limits.firstFactor.refund(key, window)
		a.refused(kind, key, window)
		return fmt.Errorf("operatorauth: record an accepted first factor: %w", err)
	}
	a.limits.firstFactor.wrote(window)
	return nil
}

// refused is a fail-closed refusal's one line per window (budget.firstRefusal): the kind,
// the operator's id and the numbers -- the suppression line's attributes. Never the
// error's text, the address or anything the request carried.
func (a *Authenticator) refused(kind db.OperatorAuthEvent, key string, window *budgetWindow) {
	if a.limits.firstFactor.firstRefusal(window) {
		a.log.Warn("operator first-factor audit row not written; this operator's sign-in step was refused",
			"kind", string(kind), "operator_id", key, "cap", firstFactorLimit, "window", firstFactorPeriod.String())
	}
}
