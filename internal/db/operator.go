package db

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The platform operator's database accessors (M10; ADR 0021 §2 vi, §4). Hand-written
// for the reason resolve.go gives -- sqlc v1.28 cannot type these calls -- and
// mirrored verbatim in db/queries/operator.sql, which is the canonical, `-- name:`-less
// document of every statement below.
//
// 🔴 WHERE THEY RUN: ON A tappa_operator CONNECTION, NEVER ON *DB. These are free
// functions over an OperatorConn rather than methods on the customer pool, and that
// is the structure, not a style: *DB is tappa_app's pool, which holds no EXECUTE on
// any op_* and no privilege on the four operator tables (00026), so a call through it
// fails loudly (42501) -- ADR 0021 §4's "iki DSN'in yer değiştirmesi iki yönde de
// gürültülü". The operator's own pool (db.OperatorDB) is OP-7's; it satisfies
// OperatorConn and delegates here. In tests the conn is a transaction that
// impersonates tappa_operator (SET LOCAL SESSION AUTHORIZATION), the identity
// PostgreSQL actually checks.
//
// NOTHING HERE CALLS set_config: no tenant context is produced or consumed (ADR 0021
// §3.6), and none of the five op_* born in 00026 touches a tenant table.
//
// THREE RULES THE OP-7 CARD NAMED, KEPT HERE BECAUSE THE SQL NOW LIVES HERE
// (m10-platform.md, "Kabullere bağlananlar — OP-7" (b), (c), (d)):
//
//	(b) every value is a BOUND PARAMETER. The statements are package constants with
//	    $n placeholders and no quoted literal (TestOperatorSQL_OnlyBoundParameters reads
//	    them); a value spliced into SQL text would reach the server log through
//	    log_statement / the error STATEMENT whatever log_parameter_max_length says
//	    (ADR 0021 "Karar verilmedi", condition 2).
//	(c) a *pgconn.PgError is NEVER returned or wrapped. Its Detail carries row values
//	    (00026's own history: "Failing row contains (...)" held a digest and an
//	    envelope), so operatorErr keeps the SQLSTATE and drops the rest -- errors.As
//	    cannot reach a PgError through anything this file returns.
//	(d) the statements themselves, which the OP-7 card had and OP-6 needed first
//	    (the OP-6 card correction says why they moved).

// OperatorConn is the connection an operator statement runs on: tappa_operator's
// pool (OP-7) or a transaction impersonating it. *pgxpool.Pool, *pgx.Conn and pgx.Tx
// all satisfy it.
type OperatorConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ErrOperatorRefused is every op_* refusal: 00026 raises SQLSTATE 28000 for a dead
// session, a used or wrong token, a stale or poisoned TOTP step, a lock and a wrong
// status alike, with one message per function. Nothing else on these paths produces
// 28000, so a missing grant (42501) can never be mistaken for it.
var ErrOperatorRefused = errors.New("db: operator call refused")

// ErrNoOperator is the login lookup finding no ACTIVE operator. Pending, disabled and
// unknown are the same answer by construction: tappa_operator's only policy on
// platform_admins shows active rows (00026), and the statement filters on status as
// well (the belt; RLS is the braces).
var ErrNoOperator = errors.New("db: no active operator")

// OperatorAuthEvent is op_record_auth_event's CLOSED kind set (00026, ADR 0021 §1):
// pre-session FAILURES only. The database refuses any other value with 22023.
type OperatorAuthEvent string

const (
	OperatorLoginFailed      OperatorAuthEvent = "login_failed"
	OperatorUnknownEmail     OperatorAuthEvent = "unknown_email"
	OperatorTOTPFailed       OperatorAuthEvent = "totp_failed"
	OperatorLocked           OperatorAuthEvent = "locked"
	OperatorEnrollmentFailed OperatorAuthEvent = "enrollment_failed"
)

// OperatorAccount is what the login needs of an ACTIVE operator: the digest to
// compare, the sealed TOTP secret to open, and the lock stamp to REPORT (the lock is
// DECIDED inside op_open_session's UPDATE -- ADR 0020 §3; nothing in Go reads this
// field to refuse anything).
//
// Both credentials travel wrapped: a bcrypt digest in a log is an offline cracking
// target (PasswordHash's own argument), and the envelope, though useless without
// TAPPA_OPERATOR_TOTP_KEK, is on ADR 0020 §5's never-log list.
type OperatorAccount struct {
	ID          uuid.UUID
	Digest      PasswordHash
	Sealed      SealedSecret
	LockedUntil *time.Time
}

// OperatorSession is op_touch_session's answer: the session and the operator it
// resolves to. Never the hash (ADR 0021 §1: the session's hash is seen by the definer
// and returned by nothing).
type OperatorSession struct {
	SessionID uuid.UUID
	AdminID   uuid.UUID
}

// The statements, mirrored in db/queries/operator.sql.
//
// The two lookups read the table directly: tappa_operator holds a column SELECT on
// platform_admins for exactly this (ADR 0021 §1, sınır 11), under one policy that
// shows active rows. `status = 'active'` is written AGAIN in the statement -- the
// belt beside RLS's braces (CLAUDE.md §4.5's shape, applied to a table no tenant
// owns) -- and the citext equality is schema-qualified so a pinned search_path cannot
// turn it case-sensitive (ADR 0002's M6-01 trap).
//
// ⚠️ The literal 'active' is the one quoted value in this file, and it is a constant
// of the schema, not a caller's value; TestOperatorSQL_OnlyBoundParameters names it.
const (
	operatorByEmailSQL = `SELECT id, password_hash, totp_secret_sealed, totp_locked_until
FROM public.platform_admins
WHERE email OPERATOR(public.=) $1::public.citext AND status = 'active'`

	operatorByIDSQL = `SELECT id, password_hash, totp_secret_sealed, totp_locked_until
FROM public.platform_admins
WHERE id = $1 AND status = 'active'`

	recordOperatorAuthEventSQL = `SELECT public.op_record_auth_event($1, $2, $3)`

	openOperatorSessionSQL = `SELECT public.op_open_session($1, $2, $3)`

	completeOperatorEnrollmentSQL = `SELECT public.op_complete_enrollment($1, $2, $3, $4, $5, $6)`

	touchOperatorSessionSQL = `SELECT session_id, admin_id FROM public.op_touch_session($1)`

	closeOperatorSessionSQL = `SELECT public.op_close_session($1)`
)

// maxOperatorEmailBytes is 00026's CHECK on platform_admins.email (254). A longer
// address cannot be an operator's, so it is answered without a round trip -- and
// without handing the server a megabyte to cast to citext.
const maxOperatorEmailBytes = 254

// storableAddress reports whether PostgreSQL can hold email as text at all: valid
// UTF-8 (the server encoding) and no NUL byte. Anything else cannot be an operator's
// address, and sending it is not a lookup but an error -- measured (OP-6 verification,
// 2026-09-30): "a\x00b@…" and "a\xffb@…" came back as SQLSTATE 22021, and the password
// step answered a database error with NO bcrypt and NO row, outside ADR 0020 §3's
// "same answer, same time" set. So such an address takes the over-long address's path:
// no round trip, ErrNoOperator, and the caller pays the dummy comparison and writes its
// unknown_email row like for any other unknown address.
func storableAddress(email string) bool {
	return utf8.ValidString(email) && !strings.ContainsRune(email, 0)
}

// OperatorByEmail is the login lookup: the ACTIVE operator with this address
// (case-insensitive), or ErrNoOperator.
func OperatorByEmail(ctx context.Context, c OperatorConn, email string) (OperatorAccount, error) {
	if email == "" || len(email) > maxOperatorEmailBytes || !storableAddress(email) {
		return OperatorAccount{}, ErrNoOperator
	}
	return scanOperator(c.QueryRow(ctx, operatorByEmailSQL, email), "operator by email")
}

// OperatorByID is the TOTP step's lookup: the account the signed login challenge
// names, if it is still ACTIVE.
func OperatorByID(ctx context.Context, c OperatorConn, id uuid.UUID) (OperatorAccount, error) {
	return scanOperator(c.QueryRow(ctx, operatorByIDSQL, id), "operator by id")
}

func scanOperator(row pgx.Row, what string) (OperatorAccount, error) {
	var (
		a      OperatorAccount
		digest *string
		sealed []byte
	)
	if err := row.Scan(&a.ID, &digest, &sealed, &a.LockedUntil); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OperatorAccount{}, ErrNoOperator
		}
		return OperatorAccount{}, operatorErr(what, err)
	}
	// 00026's CHECK makes an active row without both credentials unrepresentable;
	// should one ever appear it is treated as absent, so the caller takes the same
	// dummy-comparison path as for an unknown address.
	if digest == nil || len(sealed) == 0 {
		return OperatorAccount{}, ErrNoOperator
	}
	a.Digest = NewPasswordHash(*digest)
	a.Sealed = NewSealedSecret(sealed)
	clear(sealed)
	return a, nil
}

// RecordOperatorAuthEvent writes ONE pre-session failure row through
// op_record_auth_event. The address (if any) and the account id (if any) are only
// looked up by the definer; neither is stored (ADR 0021 §1). An empty email, one no
// operator can have (over-long or not storable as text) and uuid.Nil are sent as NULL.
func RecordOperatorAuthEvent(ctx context.Context, c OperatorConn, kind OperatorAuthEvent, email string, admin uuid.UUID) error {
	var e, a any
	if email != "" && len(email) <= maxOperatorEmailBytes && storableAddress(email) {
		e = email
	}
	if admin != uuid.Nil {
		a = admin
	}
	_, err := c.Exec(ctx, recordOperatorAuthEventSQL, string(kind), e, a)
	return operatorErr("record operator auth event", err)
}

// OpenOperatorSession is op_open_session: advance the TOTP step, check the lock, and
// give birth to an MFA-stamped session and its 'login' row -- one statement. A
// refusal (replayed or stale step, lock, not active) is ErrOperatorRefused.
func OpenOperatorSession(ctx context.Context, c OperatorConn, admin uuid.UUID, sessionHash string, step int64) error {
	_, err := c.Exec(ctx, openOperatorSessionSQL, admin, sessionHash, step)
	return operatorErr("open operator session", err)
}

// CompleteOperatorEnrollment is op_complete_enrollment. rawToken is the enrollment
// token AS IT TRAVELLED in the link; the definer hashes it (keyless SHA-256 of its
// UTF-8 text, 00026) and consumes it in the same statement that writes the
// credentials and opens the first session. One refusal for every reason.
func CompleteOperatorEnrollment(ctx context.Context, c OperatorConn, admin uuid.UUID, rawToken, digest string,
	sealed []byte, step int64, sessionHash string) error {
	_, err := c.Exec(ctx, completeOperatorEnrollmentSQL, admin, rawToken, digest, sealed, step, sessionHash)
	return operatorErr("complete operator enrollment", err)
}

// TouchOperatorSession is THE session predicate (ADR 0021 §2 i): alive (8 h absolute,
// 30 min idle, both by the database's wall clock), MFA-stamped, not revoked, operator
// active -- and last_used_at advanced in the same statement. Anything else is
// ErrOperatorRefused.
func TouchOperatorSession(ctx context.Context, c OperatorConn, sessionHash string) (OperatorSession, error) {
	var s OperatorSession
	if err := c.QueryRow(ctx, touchOperatorSessionSQL, sessionHash).Scan(&s.SessionID, &s.AdminID); err != nil {
		return OperatorSession{}, operatorErr("touch operator session", err)
	}
	return s, nil
}

// CloseOperatorSession is op_close_session: revoke through the touch predicate and
// write the 'logout' row in one statement. A dead session is ErrOperatorRefused.
func CloseOperatorSession(ctx context.Context, c OperatorConn, sessionHash string) error {
	_, err := c.Exec(ctx, closeOperatorSessionSQL, sessionHash)
	return operatorErr("close operator session", err)
}

// operatorErr is rule (c): a PostgreSQL error leaves this file as ITS SQLSTATE ONLY.
// 28000 becomes ErrOperatorRefused; any other SQLSTATE becomes a fresh error naming
// the call and the code -- NOT wrapping the *pgconn.PgError, whose Detail, Where and
// ConstraintName are exactly what 00026 took pains to keep off the wire. Errors that
// are not PostgreSQL's (a cancelled context, a broken connection) carry no row value
// and are wrapped so errors.Is(context.Canceled) still works.
func operatorErr(what string, err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.Code == "28000" {
			return ErrOperatorRefused
		}
		return fmt.Errorf("db: %s: database error (SQLSTATE %s)", what, pg.Code)
	}
	return fmt.Errorf("db: %s: %w", what, err)
}

// SealedSecret is platform_admins.totp_secret_sealed in transit: the AES-256-GCM
// envelope of an operator's TOTP secret (internal/sun.Seal, AAD = the account id).
// It is PasswordHash's pattern -- the five redacting methods plus the bytes behind a
// *string -- because the envelope is on ADR 0020 §5's never-log list: useless
// without TAPPA_OPERATOR_TOTP_KEK, and a RCE that has the process has the KEK too
// (ADR 0020 risk 3), so a logged envelope is one step from a logged secret.
//
// WHY *string AND NOT *[]byte (OP-6 verification, 7th round, the 6th auditor
// measured): fmt reaches no method of a value held in a caller's UNEXPORTED field and
// prints it by reflection; for a verb a pointer does not take (%s %q %e %f %t %c %U)
// fmt's badVerb opens the pointer ONCE and a *[]byte opens to the bytes -- the
// envelope printed. A *string is not opened (fmt opens a pointer to an array, a slice,
// a struct or a map only), so it prints an address. The first version held *[]byte.
//
// The placeholder differs from PasswordHash's so the two cannot vouch for each other
// in a leak test that greps for one of them.
type SealedSecret struct{ v *string }

const envelopeRedacted = "db.SealedSecret(redacted)"

var (
	_ fmt.Formatter          = SealedSecret{}
	_ fmt.Stringer           = SealedSecret{}
	_ fmt.GoStringer         = SealedSecret{}
	_ slog.LogValuer         = SealedSecret{}
	_ encoding.TextMarshaler = SealedSecret{}
)

// NewSealedSecret wraps a COPY of b; the caller's slice is not aliased.
func NewSealedSecret(b []byte) SealedSecret {
	c := string(b)
	return SealedSecret{v: &c}
}

// RevealForOpen returns a copy of the envelope for internal/sun.Open, or nil for the
// zero value (which then fails to open -- the fail-closed direction). The one reader.
func (s SealedSecret) RevealForOpen() []byte {
	if s.v == nil {
		return nil
	}
	return []byte(*s.v)
}

// Format, String, GoString, LogValue, MarshalText: every path that reaches a method
// emits the placeholder (PasswordHash's comments give the path each one covers); the
// paths that reach none print the *string's address (operatorauth's leak test drives
// the matrix).
func (SealedSecret) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(envelopeRedacted)) }
func (SealedSecret) String() string               { return envelopeRedacted }
func (SealedSecret) GoString() string             { return envelopeRedacted }
func (SealedSecret) LogValue() slog.Value         { return slog.StringValue(envelopeRedacted) }
func (SealedSecret) MarshalText() ([]byte, error) { return []byte(envelopeRedacted), nil }
