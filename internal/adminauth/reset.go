package adminauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// The admin password-reset token: a ONE-SHOT bearer credential that permits exactly
// one state transition -- write this administrator's password_hash, once -- and then
// dies. It grants NO session.
//
// 🔴 THAT SENTENCE IS THE SCOPE DECISION OF M7-04 PHASE A, not a description. The
// task card asks for "magic link and/or password" login. The password half shipped in
// M6-01 (Authenticate, below in this package), so the disjunction is already
// satisfied, and the magic-link half is a SECOND authentication path -- a second thing
// that can be wrong, on the surface where being wrong means somebody else's payroll.
// The difference that decided it is visible to the account owner: a stolen reset link
// costs them a password they can SEE was changed (their own stops working, their panel
// sessions are revoked, an audit row exists), while a stolen magic link is a silent
// sign-in nothing tells them about -- the shape §4.6 exists to refuse. If magic-link
// login is ever wanted it gets its OWN table and its own ADR; putting it on
// password_resets would upgrade every reset token already issued into a session token.
//
// THE DECISION RECORD IS docs/adr/0015-sifirlama-tokeni-tek-gecislik-yetkidir.md.
//
// KIRMIZI CIZGI §4.7. The raw value must not reach a log line, an error string or a
// serialised payload, and the two mechanisms are internal/session's, verbatim, for
// the reasons measured there:
//
//  1. REDACTING METHODS (fmt.Formatter for every verb, Stringer, GoStringer,
//     slog.LogValuer, encoding.TextMarshaler) cover every path where a printer
//     reaches the value as an interface.
//  2. INDIRECTION (a *string field) covers the path the methods CANNOT: fmt skips
//     Formatter/Stringer for a value read out of an UNEXPORTED struct field
//     (reflect CanInterface() == false) and falls through to printing the field's
//     contents. A handler writing `struct{ tok ResetToken }` and logging it with
//     %+v is exactly that shape, and it is the shape an audit measured on
//     session.Token in M5-01.
//
// KNOWN, ACCEPTED LIMIT -- not "impossible". Deliberate reflection, a debugger and a
// core dump all still read it, and reveal does it in one line inside this package.
// The guarantee is about ACCIDENTAL rendering and is tested from an EXTERNAL test
// package (resetleak_external_test.go), which is where the equivalent claim on
// session.Token broke.

const (
	// resetTokenBytes is the entropy behind one reset token: 32 bytes = 256 bits from
	// crypto/rand.
	//
	// THIS CONSTANT IS AN ASSUMPTION MIGRATION 00019 MAKES AND CANNOT CHECK. That
	// migration carries NO brute-force lockout state for password_resets -- no attempt
	// counter, no locked_until -- and says so: the guessing defence is the token
	// itself, not database state, and that is sound only while the token is >= 128
	// bits of cryptographic randomness. Shrinking this, or swapping the generator for
	// a human-readable alphabet, breaks the schema's stated assumption and requires
	// the lockout migration FIRST. Nothing mechanical enforces it: the CHECK on
	// token_hash tests the HASH's shape, and the hash of a six-digit code is 64 hex
	// characters exactly like this one's.
	resetTokenBytes = 32

	// resetTokenLen is the exact character length of resetTokenBytes in unpadded
	// base64url: ceil(32*8/6) = 43. A cheap shape gate before any HMAC work runs on an
	// untrusted URL parameter.
	resetTokenLen = 43

	// resetRedacted is what every printing path emits. It contains neither the value,
	// nor a prefix of it, nor its length.
	//
	// IT IS DELIBERATELY DIFFERENT FROM Token's placeholder AND FROM session.Token's.
	// A leak test that greps for "adminauth.Token(redacted)" would pass over a reset
	// token that leaked, and vice versa; distinct strings keep the suites from
	// vouching for each other.
	resetRedacted = "adminauth.ResetToken(redacted)"

	// resetTokenKeyLabel derives THIS credential's HMAC key from the session HMAC key,
	// the same way adminTokenKeyLabel does for the panel cookie and for the same
	// reason (a new required env var means every deployment, and CI, refusing to boot
	// until an operator sets it; HMAC is one-way, so this is not key sharing).
	//
	// WHAT THE SEPARATE LABEL BUYS, concretely and testably: the SAME 43-character
	// string hashes to a DIFFERENT value for a reset link than for a panel cookie, so
	// a value that is a valid admin_sessions.token_hash can never be a row in
	// password_resets and vice versa (TestResetAndSessionHashes_Differ). Without it
	// the two hashing functions would be identical and the separation of the two
	// credentials would rest entirely on the right function being called on the right
	// table -- discipline, where this repository wants structure.
	//
	// LIMIT, in the honest direction: whoever holds TAPPA_SESSION_HMAC_KEY can derive
	// this one. A leaked session key is already a total compromise of panel identity,
	// so a derivable reset key is not the first thing to worry about -- but the two
	// are NOT independent.
	resetTokenKeyLabel = "tappa/admin-password-reset/v1/key-derivation"
)

// ResetTTL is how long a reset link stays spendable.
//
// ONE HOUR, and the number is a product choice while its CEILING is a schema one.
// Migration 00019 refuses any row with expires_at > created_at + 24 h, so an absurd
// or infinite link is unrepresentable no matter what this constant says; changing
// THIS line is an ordinary edit, and going past the ceiling costs a migration. That
// split is deliberate: the database refuses what must never happen, the constant
// chooses inside what may.
const ResetTTL = time.Hour

// errResetMalformed is the internal shape error for a URL value that cannot be a
// token we issued. Callers never see it: Consume maps it to ErrResetUnusable, so a
// junk token and an unknown token are the same thing to the reset page -- and so no
// error string can ever quote the value.
var errResetMalformed = errors.New("adminauth: reset token has the wrong shape")

var (
	// ErrResetUnusable is every way a reset can fail to apply: unknown token,
	// malformed token, spent token, expired token, superseded token, disabled
	// administrator.
	//
	// 🔴 THEY ARE ONE ERROR ON PURPOSE. The page that consumes a reset link is
	// reachable without credentials, so any distinction the caller can observe is a
	// distinction an attacker can observe. The trail keeps what the response throws
	// away: Consume returns the resolved facts alongside the error whenever the token
	// resolved at all, so phase B can write an attributable audit row (§4.6) while
	// showing one sentence.
	ErrResetUnusable = errors.New("adminauth: reset token cannot be used")

	// ErrNoSuchAdmin is Issue's refusal: no active administrator with that id in that
	// tenant. It is NOT reachable from an unauthenticated request with an
	// attacker-chosen id -- Issue takes uuids that phase B must have resolved first --
	// so it is safe for it to be distinguishable HERE. Whether the RESPONSE
	// distinguishes it is phase B's decision and the answer is no (00011's
	// OBLIGATION 1).
	ErrNoSuchAdmin = errors.New("adminauth: no active admin for that id")
)

// ResetToken is a raw password-reset token in transit: between newResetToken and the
// delivery channel, or between reading the request parameter and hashing it. Never
// longer, and never in the database.
//
// ⚠️ COMPARISON IS IDENTITY, NOT VALUE. The type still compiles as comparable, but
// the POINTER is compared, so two ResetTokens wrapping the same string are `!=`.
// Consequences are fail-CLOSED (a false non-match, never a false match) and nothing
// in this package compares one: equality is decided by the DATABASE, on token_hash.
//
// The zero ResetToken is safe to hold and to print: every method tolerates a nil
// field and hash rejects it as errResetMalformed.
type ResetToken struct{ v *string }

// Compile-time proof that each redaction interface is implemented -- all FIVE. If a
// Go upgrade or a refactor drops one, the build breaks here rather than a token
// silently appearing in a log line.
var (
	_ fmt.Formatter          = ResetToken{}
	_ fmt.Stringer           = ResetToken{}
	_ fmt.GoStringer         = ResetToken{}
	_ slog.LogValuer         = ResetToken{}
	_ encoding.TextMarshaler = ResetToken{}
)

// ParseResetToken wraps an untrusted string (the value out of a reset link) in a
// ResetToken. It does NOT validate: the shape gate lives in hash, so exactly one
// place decides what a well-formed token looks like. The parameter is taken by value
// and the ResetToken points at that copy, so the caller's storage is never aliased.
//
// Exported because the HTTP layer must be able to build one from a request without
// this package importing net/http.
func ParseResetToken(v string) ResetToken { return ResetToken{v: &v} }

// reveal returns the raw value, or "" for the zero ResetToken. Deliberately
// UNEXPORTED and deliberately narrow: its call sites are hash, Link (the single
// egress, into the URL a delivery channel sends) and this package's own tests.
func (t ResetToken) reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

// Format implements fmt.Formatter, which fmt consults BEFORE Stringer and for EVERY
// verb -- %x, %q, %d and any future verb included.
func (ResetToken) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(resetRedacted)) }

// String implements fmt.Stringer for direct .String() calls.
func (ResetToken) String() string { return resetRedacted }

// GoString covers %#v, which uses GoStringer rather than Stringer.
func (ResetToken) GoString() string { return resetRedacted }

// LogValue implements slog.LogValuer so log/slog renders the placeholder even when
// the value is an attribute or nested in a logged struct.
func (ResetToken) LogValue() slog.Value { return slog.StringValue(resetRedacted) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers for a
// value type.
func (ResetToken) MarshalText() ([]byte, error) { return []byte(resetRedacted), nil }

// newResetToken draws resetTokenBytes of cryptographic randomness and encodes it as
// unpadded base64url: URL-safe (it travels in a link), fixed length, no '=' to
// re-encode. crypto/rand is the only source; math/rand would make every outstanding
// reset link predictable from one observed token.
//
// A crypto/rand failure is returned, never ignored (§7).
func newResetToken() (ResetToken, error) {
	b := make([]byte, resetTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return ResetToken{}, fmt.Errorf("adminauth: read randomness: %w", err)
	}
	return ParseResetToken(base64.RawURLEncoding.EncodeToString(b)), nil
}

// deriveResetKey produces the reset credential's HMAC key from the session HMAC key.
func deriveResetKey(sessionKey []byte) ([]byte, error) {
	if len(sessionKey) != hmacKeyLen {
		return nil, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(resetTokenKeyLabel)) // hash writes never fail (documented)
	return m.Sum(nil), nil
}

// hash returns the hex-encoded HMAC-SHA256 of the token under key -- the value stored
// in password_resets.token_hash, whose CHECK requires exactly ^[0-9a-f]{64}$ (00019).
//
// HMAC, not a bare SHA-256: a bare digest in a stolen dump could be attacked offline
// by anyone, while an HMAC is unforgeable and unsearchable without the key, which
// lives only in the process environment. That matters more here than for a session
// cookie -- a session hash buys a session that can be revoked, a reset hash buys a
// password.
//
// The value is hashed as its STRING bytes, not its decoded bytes, so the mapping
// token -> token_hash is injective (base64 permits non-canonical trailing bits, and
// decoding first would let two distinct strings share one hash). DecodeString is still
// used as a charset gate.
//
// SHAPE GATE FIRST: an untrusted URL value is length- and charset-checked before any
// HMAC work, so a huge or junk parameter costs nothing and never reaches the database.
// Errors carry the key length at most, never key or token bytes (§4.7).
func (t ResetToken) hash(key []byte) (string, error) {
	if len(key) != hmacKeyLen {
		return "", fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(key))
	}
	v := t.reveal()
	if len(v) != resetTokenLen {
		return "", errResetMalformed
	}
	if _, err := base64.RawURLEncoding.DecodeString(v); err != nil {
		return "", errResetMalformed
	}
	mac := hmac.New(sha256.New, key)
	// hash.Hash.Write never returns an error (documented); the value is written as-is
	// with no formatting call, so it cannot reach a log through here.
	_, _ = mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// ResetDatabase is the narrow slice of *db.DB the reset flow needs, declared HERE at
// the consumer (§7).
//
// IT IS A SEPARATE INTERFACE FROM Database ON PURPOSE, and not to avoid touching test
// fakes: the reset flow needs exactly two capabilities and Manager's three are a
// different two plus one. Widening Manager's interface would force every consumer of
// panel LOGIN to supply a method that only password RESET calls, which is the
// producer-side interface §7 forbids.
type ResetDatabase interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	// GetPasswordResetByTokenHash is the CONTEXT-LESS lookup (ADR 0002 madde 7,
	// migration 00019): a reset link carries only a token, so the tenant is the
	// RESULT. It reaches its row through a SECURITY DEFINER function owned by
	// tappa_resolver whose blast radius is a fixed column list on one table.
	GetPasswordResetByTokenHash(ctx context.Context, tokenHash string) (db.ResolvedPasswordReset, error)
}

// The one implementation, bound AT COMPILE TIME rather than in a test. An audit found
// this missing and the gap was real: nothing anywhere required *db.DB to satisfy this
// interface, so a rename or a signature change in internal/db would have broken the
// wiring at the first phase-B call site rather than at the build.
var _ ResetDatabase = (*db.DB)(nil)

// Resets issues and spends admin password-reset tokens. Dependencies are injected as
// struct fields (§7); there is no package-level state.
type Resets struct {
	data ResetDatabase
	// hmacKey is this flow's DERIVED key, held privately so a later mutation of the
	// caller's config slice cannot change how tokens hash.
	hmacKey []byte
	// ttl is how long an issued token lives. A field rather than the constant so a
	// test can build an already-expired token without sleeping; production always
	// gets ResetTTL from NewResets.
	ttl time.Duration
	// now is the clock, injectable for the same reason.
	now func() time.Time
	// digest turns the submitted password into the value stored in
	// admin_users.password_hash. It is a FIELD rather than a direct call to Hash for
	// exactly the reason Manager.dummy is one, and the number is the same: `make test`
	// runs with -race, and one cost-12 bcrypt measured 11.34 s under it against 0.60 s
	// without (manager_db_test.go's fixtureDigest records the measurement and the
	// package timeout it caused). The end-to-end reset tests call Consume several
	// times; at the shipped cost that is a minute of -race time for a property none of
	// them is about.
	//
	// PRODUCTION NEVER SETS IT -- NewResets installs Hash, which is the only producer
	// of a digest migration 00018's CHECK accepts, and refuses an empty password and
	// anything over MaxPasswordBytes. The test that IS about the stored work factor
	// (TestConsume_StoresADigestAtTheShippedCost) deliberately does not substitute it.
	digest func(password string) (string, error)
}

// NewResets builds a Resets. It refuses a wrong-sized HMAC key rather than degrading:
// a short or empty key still produces plausible-looking hex, so the failure would stay
// invisible until somebody noticed reset links were forgeable.
func NewResets(data ResetDatabase, cfg *config.Config) (*Resets, error) {
	if data == nil {
		return nil, errors.New("adminauth: nil database")
	}
	if cfg == nil {
		return nil, errors.New("adminauth: nil config")
	}
	key, err := deriveResetKey(cfg.SessionHMACKey)
	if err != nil {
		return nil, err
	}
	return &Resets{data: data, hmacKey: key, ttl: ResetTTL, now: time.Now, digest: Hash}, nil
}

// Reset is the stored record of one reset request. It carries no token and no hash --
// there is no field here either could travel in, so no later edit adds one by accident
// and no %+v of this type can leak one.
type Reset struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	AdminUserID uuid.UUID
	CreatedAt   time.Time
	ExpiresAt   time.Time
	// RetiredCount is how many previously live reset links this request killed.
	// It is the observable half of CreatePasswordReset's fused retirement, and phase B
	// should put it in the audit row: "issuing this link invalidated N earlier ones" is
	// exactly the fact a takeover investigation wants and the fact M6-05 discovered was
	// missing for invites.
	RetiredCount int64
}

// IssuedReset is the result of Issue: the stored record plus the raw token, which the
// caller must hand straight to a delivery channel and then drop. ResetToken cannot
// print itself, so logging an IssuedReset value is safe.
type IssuedReset struct {
	Reset Reset
	Token ResetToken
}

// Link renders the reset URL for a delivery channel.
//
// ⚠️ IT IS THE SINGLE EGRESS OF A §4.7 SECRET FROM THIS PACKAGE, and every caller
// inherits three obligations no type here can enforce (internal/invite.Delivery states
// the same three for the same reason):
//
//  1. NEVER log it, and never log anything derived from it. Not at debug level, not
//     in an error string, not in a retry message. scripts/redline-check.sh R7 catches
//     a `token`-shaped argument to a log call; a variable called `link` sails past it.
//  2. NEVER persist it. The database stores the HASH and nothing else; a delivery
//     queue that keeps the URL for retries has re-created the plain credential the
//     schema goes to some trouble to avoid.
//  3. Hand it to ONE recipient -- the address on the administrator's own row.
//
// base is the absolute origin plus path, e.g. "https://app.tappa.example/reset". The
// token travels in the query string because a reset link is opened by clicking, and a
// path segment would put it in more log formats by default. Neither placement is
// private: phase B must serve this page with Referrer-Policy: no-referrer.
func (i IssuedReset) Link(base string) string {
	return base + "?t=" + i.Token.reveal()
}

// Issue mints a reset token for ONE administrator and retires that administrator's
// previously live links in the SAME statement (db/queries/passwordresets.sql).
//
// 🔴 WHAT IT DELIBERATELY DOES NOT DO: touch the administrator's password. A reset
// REQUEST leaves the existing credential working. That is not politeness, it is the
// anti-lockout property -- a flow that disabled the old password on request would let
// anybody lock any administrator out of their own panel by typing their address into
// a public form, which is the class of harm M7-02 shipped once already (a cap that
// converted a CPU bound into an account lockout). Nothing an unauthenticated caller
// can do here changes the owner's PASSWORD.
//
// 🔴 THAT SENTENCE USED TO READ "changes what the real owner can do", AND A SECURITY
// AUDIT MEASURED IT FALSE FOR THE PENDING LINK. The retirement this function performs
// is the mechanism: the victim requests a reset (T1, retired_count = 0), an attacker
// types the same address into the same public form (T2, retired_count = 1), and T1 is
// dead before the victim reads their mail -- `Consume(T1)` answers 0 rows. Repeated,
// the account owner can never COMPLETE a recovery, which is the WEAK form of exactly
// the harm ADR 0015 lists as rejected alternative 4. The test that pins the behaviour
// already exists and reads innocently:
// TestConsume_EveryFailureIsOneError/a_superseded_token.
//
// IT IS A COUNTED TRADE, NOT AN OVERSIGHT, and both sides are named because only one
// of them was: retiring siblings closes the M6-05 defect (a stale link staying live in
// somebody's inbox), and NOT retiring them leaves two spendable credentials. What
// bounds the attack is a PER-ADDRESS RATE LIMIT on the request endpoint, which is a
// PHASE B OBLIGATION and is written into the M7-04 card's acceptance criteria rather
// than left here. Nothing in this package can impose it: this function is given uuids,
// not an address.
//
// IT TAKES uuids, NOT AN EMAIL, and that boundary is the point. Mapping a typed-in
// address to candidate administrators is 00011's OBLIGATION 1 territory -- the
// question a dummy bcrypt exists to refuse -- so it stays outside this function and
// phase B must answer it with a response that is identical for a registered and an
// unregistered address.
//
// A disabled or unknown administrator yields pgx.ErrNoRows from the query and
// ErrNoSuchAdmin here; nothing is retired in that case either (the statement's EXISTS
// guard, 00009's measured lesson about unconditional data-modifying CTEs).
func (r *Resets) Issue(ctx context.Context, tenantID, adminUserID uuid.UUID) (IssuedReset, error) {
	if tenantID == uuid.Nil || adminUserID == uuid.Nil {
		return IssuedReset{}, errors.New("adminauth: issue reset: tenant and admin are required")
	}
	t, err := newResetToken()
	if err != nil {
		return IssuedReset{}, fmt.Errorf("adminauth: issue reset: %w", err)
	}
	h, err := t.hash(r.hmacKey)
	if err != nil {
		return IssuedReset{}, fmt.Errorf("adminauth: issue reset: %w", err)
	}

	var row store.CreatePasswordResetRow
	err = r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreatePasswordReset(ctx, store.CreatePasswordResetParams{
			TenantID:    tenantID,
			AdminUserID: adminUserID,
			TokenHash:   h,
			ExpiresAt:   r.now().Add(r.ttl),
		})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return IssuedReset{}, ErrNoSuchAdmin
	}
	if err != nil {
		// The message names neither the token nor its hash: neither is interpolated
		// and %w wraps only the database error (§4.7).
		return IssuedReset{}, fmt.Errorf("adminauth: issue reset: %w", err)
	}
	return IssuedReset{
		Reset: Reset{
			ID:           row.ID,
			TenantID:     row.TenantID,
			AdminUserID:  row.AdminUserID,
			CreatedAt:    row.CreatedAt,
			ExpiresAt:    row.ExpiresAt,
			RetiredCount: row.RetiredCount,
		},
		Token: t,
	}, nil
}

// ConsumedReset is what a successful reset did, in the shape an audit row needs.
type ConsumedReset struct {
	ResetID     uuid.UUID
	TenantID    uuid.UUID
	AdminUserID uuid.UUID
	// RetiredCount is how many OTHER live links this consumption killed.
	RetiredCount int64
	// RevokedSessions is how many live panel sessions were signed out. It is part of
	// the same transaction as the password write: a reset that could not revoke does
	// not happen at all.
	RevokedSessions int
}

// Consume spends a reset token and writes the new password, then signs the
// administrator out everywhere -- all in ONE transaction.
//
// 🔴 REVOCATION IS NOT OPTIONAL AND NOT A SEPARATE STEP. db/queries/admins.sql
// describes RevokeAdminSessionsForAdmin as "the password was changed or reset
// (M7-04)", and the reason it must share this transaction is the failure mode: a reset
// that changed the password but left the old cookies alive is a takeover that SURVIVES
// the victim's remedy. Whoever stole the session keeps it; the owner has changed a
// password that was not the thing being used against them.
//
// ⚠️ IT REVOKES THE RESETTER'S OWN SESSIONS TOO, which is deliberate and is the reason
// phase B must send the user to the login page afterwards rather than into the panel.
// There is no way to tell "the browser holding this cookie" from "the browser that
// stole it" at this point, and signing everyone out is the fail-closed answer.
//
// ⚠️ THE ORDER IS consume-and-set FIRST, revoke SECOND — AND THIS COMMENT USED TO CALL
// THAT "LOAD-BEARING", WHICH A MUTATION REFUTED. Reversing the two INSIDE the
// transaction leaves every test in this package green (measured), because when the
// consume matches nothing the callback returns the error and db.WithTenant rolls the
// whole transaction back — the revocation goes with it. So what stops a replayed link
// from being a free "sign this administrator out of everything" button TODAY is the
// TRANSACTION, not the order.
//
// THE ORDER IS THE SECOND BELT, AND IT IS THE ONE THAT MATTERS THE DAY THE FIRST IS
// BROKEN — also measured, because a belt nobody can see the need for is a belt somebody
// removes. Splitting the two writes into separate transactions AND revoking first turns
// exactly this function into that button:
// TestConsume_ReplayIsNotAFreeSignOutButton reports "a replayed link revoked 2 of the 2
// live sessions". With the order kept, the same split fails the atomicity test instead
// and nobody's session is touched. Keep both properties; neither is decoration.
//
// EVERY FAILURE MODE COLLAPSES INTO ErrResetUnusable (see its comment). The resolved
// facts are returned ALONGSIDE the error whenever the token resolved at all, so phase
// B can write an attributable audit row while showing one sentence.
//
// 🔴 NOTHING HERE WRITES A TRAIL, AND THIS PARAGRAPH USED TO SAY IT DID. It read "an
// unknown token produces a process log line -- never carrying the token -- and no audit
// row", in the PRESENT TENSE, about a log line that does not exist: this file makes no
// slog call at all (grep), so today a failed reset attempt lands NOWHERE. An audit
// caught the tense, and the sentence mattered because §4.6 is the red line it was
// claiming to satisfy.
//
// WHAT IS TRUE, AND WHOSE JOB EACH HALF IS:
//
//   - THE ATTRIBUTABLE CASE IS ALREADY POSSIBLE HERE AND IS PHASE B'S TO WRITE. When
//     the token resolves, this function hands back the resolved row ALONGSIDE the
//     error, so the caller holds a tenant, an admin id and the three state facts --
//     everything an audit_log row needs. It is deliberately not written here: this
//     package has no audit.Recorder and no request context, and a data-layer function
//     that logs on its own behalf writes a trail nobody can correlate with a request.
//   - THE UNATTRIBUTABLE CASE CANNOT BE AN AUDIT ROW AT ALL, and that is structural:
//     a token that resolves to nothing has no tenant, and audit_log.tenant_id is NOT
//     NULL with an FK (00005). internal/audit states the same limit from its side
//     ("callers that may hold an unattributable event must log it instead and say so
//     out loud"). Phase B's endpoint answers it with a process log line that names
//     neither the token nor its hash, plus its rate-limit counters -- the shape
//     internal/handler already uses for the activation endpoint.
//
// So §4.6's coverage of this flow is a PHASE B OBLIGATION, written down here rather
// than assumed, and it is not satisfied by anything in this file.
func (r *Resets) Consume(ctx context.Context, t ResetToken, newPassword string) (ConsumedReset, db.ResolvedPasswordReset, error) {
	h, err := t.hash(r.hmacKey)
	if err != nil {
		// A malformed value and an unknown one are the same answer (§4.7: the error
		// must not quote the value, and errResetMalformed does not).
		return ConsumedReset{}, db.ResolvedPasswordReset{}, ErrResetUnusable
	}

	// Hash the NEW password before touching the database: Hash refuses an empty
	// password and anything over MaxPasswordBytes, and it is the only producer of a
	// digest 00018's CHECK will accept. Doing it first means a bad password costs no
	// transaction and, more importantly, cannot leave a spent token behind.
	//
	// 🔴 THE ORDER HAS A SECOND LEG AND ONLY THE FIRST WAS WRITTEN DOWN. Hashing before
	// resolving means an UNKNOWN but well-formed token pays a full cost-12 bcrypt --
	// measured at 212.8 ms per request on this machine, on a page reachable without
	// credentials, so a hundred concurrent POSTs saturate the box. That is the harm
	// class M7-02 shipped once.
	//
	// THE FIX IS NOT TO SWAP THE TWO LINES. Resolving first would answer in microseconds
	// for an unknown token and in ~213 ms for a real one -- a TIMING ORACLE that tells an
	// unauthenticated caller whether a token exists, on the one page that must not answer
	// it. So the order is kept and the cost is accepted, bounded by a PER-ADDRESS RATE
	// LIMIT on the endpoint: a PHASE B OBLIGATION, written into the M7-04 card's
	// acceptance criteria and into ADR 0015's accepted risks rather than only here.
	digest, err := r.digest(newPassword)
	if err != nil {
		return ConsumedReset{}, db.ResolvedPasswordReset{}, fmt.Errorf("adminauth: consume reset: %w", err)
	}

	resolved, err := r.data.GetPasswordResetByTokenHash(ctx, h)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConsumedReset{}, db.ResolvedPasswordReset{}, ErrResetUnusable
	}
	if err != nil {
		return ConsumedReset{}, db.ResolvedPasswordReset{}, fmt.Errorf("adminauth: consume reset: %w", err)
	}

	// 🔴 THE RESOLVED STATE IS NOT CONSULTED HERE. resolved.UsedAt / ExpiresAt /
	// CancelledAt are carried for the AUDIT ROW and nothing else: deciding on them and
	// then writing is the read-then-write race §4.4 forbids (two requests carrying one
	// token would both read "unspent"). The authority is the single atomic statement
	// below, whose WHERE carries the state test.
	var (
		row     store.ConsumePasswordResetAndSetPasswordRow
		revoked int
	)
	err = r.data.WithTenant(ctx, resolved.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		var e error
		row, e = q.ConsumePasswordResetAndSetPassword(ctx, store.ConsumePasswordResetAndSetPasswordParams{
			TokenHash:    h,
			TenantID:     resolved.TenantID,
			PasswordHash: digest,
		})
		if e != nil {
			return e
		}
		ids, e := q.RevokeAdminSessionsForAdmin(ctx, store.RevokeAdminSessionsForAdminParams{
			TenantID: resolved.TenantID, AdminUserID: row.AdminUserID,
		})
		if e != nil {
			return e
		}
		revoked = len(ids)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ConsumedReset{}, resolved, ErrResetUnusable
	}
	if err != nil {
		return ConsumedReset{}, resolved, fmt.Errorf("adminauth: consume reset: %w", err)
	}
	return ConsumedReset{
		ResetID:         row.ResetID,
		TenantID:        row.TenantID,
		AdminUserID:     row.AdminUserID,
		RetiredCount:    row.RetiredCount,
		RevokedSessions: revoked,
	}, resolved, nil
}
