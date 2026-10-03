package operatorauth

import (
	"context"
	"errors"
	"fmt"

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
// addr is the client's rate key (OP-8 resolves it). It is used for the work budget and
// is written nowhere: no row carries an address (ADR 0021 §1).
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
	c, err := a.mintChallenge(acc.ID)
	if err != nil {
		return Challenge{}, err
	}
	// A right password whose second factor never completes left no trace (OP-6 12c, the
	// security audit's LOW finding). One line, the operator's ID and nothing else -- an
	// operator is named by id (ADR 0020 §5): no address, no email, nothing from the
	// request. A durable 'password_ok' audit kind is a migration: OP-8 added none (the
	// orchestrator's decision), so it is OP-14's.
	a.log.Info("operator first factor verified; second factor pending", "operator_id", acc.ID.String())
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
func (a *Authenticator) TOTP(ctx context.Context, c Challenge, code string) (Issued, error) {
	id, err := a.verifyChallenge(c)
	if err != nil {
		return Issued{}, ErrChallenge
	}
	if !a.limits.account.spend(id.String()) {
		return Issued{}, ErrThrottled
	}
	acc, err := a.store.OperatorByID(ctx, id)
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
		if rerr := a.store.RecordOperatorAuthEvent(ctx, db.OperatorTOTPFailed, "", id); rerr != nil {
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
	err = a.store.OpenOperatorSession(ctx, id, h, step)
	switch {
	case err == nil:
		return Issued{Token: tok, AdminID: id}, nil
	case errors.Is(err, db.ErrOperatorRefused):
		kind, out := db.OperatorTOTPFailed, ErrCodeRejected
		if lockedNow {
			kind, out = db.OperatorLocked, ErrLocked
		}
		if rerr := a.store.RecordOperatorAuthEvent(ctx, kind, "", id); rerr != nil {
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
// ONE enrollment_failed row under the audit cap; the definer names the account if the
// id exists.
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
	err = a.store.CompleteOperatorEnrollment(ctx, id, rawToken, digest, sealed, step, h)
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
func (a *Authenticator) recordPasswordless(ctx context.Context, kind db.OperatorAuthEvent, email string, id uuid.UUID) error {
	if n := a.limits.auditCap.charge(""); n > auditCapLimit {
		if n == auditCapLimit+1 {
			a.log.Warn("operator pre-session audit rows suppressed for the rest of the window",
				"kind", string(kind), "cap", auditCapLimit, "window", auditCapPeriod.String())
		}
		return nil
	}
	if err := a.store.RecordOperatorAuthEvent(ctx, kind, email, id); err != nil {
		return fmt.Errorf("operatorauth: record a pre-session failure: %w", err)
	}
	return nil
}
