// Package operatorauth is the platform operator's authentication (M10 OP-6): the
// password, the TOTP second factor, the session token and its cookie, the short-lived
// login challenge between the two steps, the enrollment that sets the credentials,
// and the budgets that bound all of it.
//
// NORMATIVE SOURCES: docs/adr/0020-platform-operatoru-ayri-kimlik.md §1-§3 (identity,
// session, login flow) and docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md §1,
// §2 (the op_* the flow calls). Where this package had to decide something those left
// open -- the challenge's lifetime and key, the budgets' numbers, where the plain TOTP
// secret lives during enrollment -- the decision is written next to the code and in
// docs/plan/m10-platform.md (OP-6 card correction).
//
// IT MIRRORS internal/adminauth, IT DOES NOT EXTEND IT (that package's header gives
// the reason; ADR 0020 §2 makes it a rule): a separate table, a separate cookie name
// on a separate host, a separate HMAC key that is its OWN variable rather than a label
// on the session key, a separate redaction placeholder. Nothing here imports the
// customer panel's authentication.
//
// WHAT LIVES HERE AND WHAT DOES NOT. The comparisons, the tokens, the cookies, the
// challenge, the TOTP arithmetic and the order in which a request spends its budgets
// live here -- because "a request a budget refused wrote nothing and counted nothing"
// is a property of that order, and it has to be proven where the order is decided.
// Reading configuration (TAPPA_OPERATOR_*), opening the operator's pool and the HTTP
// surface are OP-7 and OP-8: this package takes its keys and its Store as arguments.
//
// 🔴 THE LOCK IS THE DATABASE'S. N failed TOTP codes lock an account inside
// op_open_session's own UPDATE (ADR 0020 §3); this package never refuses a code
// because it believes the account is locked. It READS the lock stamp once, to choose
// which failure row to write and which error to return -- a label on a decision the
// database already made.
//
// CRYPTOGRAPHY: the TOTP HMAC-SHA1 and the token HMACs are here (ADR 0020 §1 md.4 --
// the same class as internal/adminauth, internal/session, internal/invite); the
// AES-256-GCM envelope of the TOTP secret is internal/sun's Seal/Open. No AES here.
package operatorauth

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/sun"
)

// Store is the slice of the operator's database this package needs, declared at the
// consumer (CLAUDE.md §7). The statements are internal/db's (operator.go); OP-7's
// db.OperatorDB -- tappa_operator's pool -- implements this by delegating to them.
//
// Every method is ONE statement on the operator's connection. The ones that write do
// so through a SECURITY DEFINER op_* (ADR 0021 §1): tappa_operator cannot write
// platform_admins, platform_sessions or operator_audit_log directly.
type Store interface {
	OperatorByEmail(ctx context.Context, email string) (db.OperatorAccount, error)
	OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error)
	RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, email string, admin uuid.UUID) error
	OpenOperatorSession(ctx context.Context, admin uuid.UUID, sessionHash string, step int64) error
	CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, rawToken, digest string, sealed []byte, step int64, sessionHash string) error
	TouchOperatorSession(ctx context.Context, sessionHash string) (db.OperatorSession, error)
	CloseOperatorSession(ctx context.Context, sessionHash string) error
}

// The outcomes a caller branches on. They are deliberately few, and two pairs are
// deliberately NOT distinguishable:
//
//   - ErrRefused is the password step's one failure: unknown address, wrong
//     password, a pending account, a disabled account -- ADR 0020 §3's "same answer,
//     same time" set. The TIME half is a real bcrypt comparison on every arm
//     (password.go).
//   - ErrThrottled means a budget refused the request BEFORE anything was verified
//     or written (limits.go). It is the only error that says nothing about the
//     credential at all.
var (
	ErrRefused      = errors.New("operatorauth: sign-in refused")
	ErrThrottled    = errors.New("operatorauth: too many attempts, try again later")
	ErrChallenge    = errors.New("operatorauth: the sign-in step has expired or is not valid")
	ErrCodeRejected = errors.New("operatorauth: the code was not accepted")
	ErrLocked       = errors.New("operatorauth: the account is locked for a while")
	ErrNoSession    = errors.New("operatorauth: no operator session")
	ErrEnrollment   = errors.New("operatorauth: enrollment refused")
	ErrWeakPassword = errors.New("operatorauth: the new password does not meet the length rule")
)

// keyLen is the size both keys must have: TAPPA_OPERATOR_TOTP_KEK is an AES-256 KEK
// (sun.KEKLen) and TAPPA_OPERATOR_TOKEN_HMAC_KEY is an HMAC-SHA256 key of the same 32
// bytes every other key in this repository has (internal/config key32).
const keyLen = sun.KEKLen

// Config is what the operator's process hands this package. OP-7 fills it from
// configuration (the two keys through NewKey); this package reads no environment.
//
// The keys are Keys, not []byte (OP-6 verification, 7th round): Config redacts itself
// (below), but fmt reaches no method of a Config VALUE held in a caller's unexported
// struct field, under %p, or under %w -- and there it printed []byte keys by
// reflection (the 6th auditor measured it on each of those paths). A Key prints as an
// address on all of them (key.go says why).
//
// ⚠️ KEY SEPARATION IS NOT CHECKED HERE. "TOTP KEK and token HMAC key differ from
// every other key" is OP-7's start-up refusal (config.keySeparation's precedent),
// where ALL the keys are visible. Re-checking a subset here would be a second copy of
// that rule that silently stops agreeing with it.
type Config struct {
	// TOTPKEK seals and opens the operator's TOTP secret (ADR 0020 §1).
	TOTPKEK Key
	// TokenHMACKey hashes operator session tokens into platform_sessions.token_hash
	// and, through a labelled derivation, signs the login challenge (challenge.go).
	TokenHMACKey Key
	// Now is the wall clock; nil means time.Now. Injected by tests. The DATABASE
	// keeps its own clock for every expiry it enforces (ADR 0021 §2 vii).
	Now func() time.Time
	// Log receives the three WARN lines this package writes, each once per window: the
	// process-wide cap on password-less pre-session rows crossed; one operator's cap on
	// 'password_ok' rows crossed, with that operator's id; and one operator's password
	// step refused fail-closed for want of its row, with that operator's id (limits.go,
	// flow.go).
	// (Until OP-14 phase C a right password also logged an Info line; the durable
	// 'password_ok' row replaced it.) Required -- a nil logger would make those
	// signals silently disappear.
	Log *slog.Logger
}

// Authenticator is the operator's sign-in, enrollment and session check.
//
// It holds COPIES of the two keys for the life of the process, which is the custody
// internal/config already has for every key; nothing here writes them anywhere.
type Authenticator struct {
	store  Store
	keys   authKeys
	now    func() time.Time
	log    *slog.Logger
	limits *limits

	// compareFn is bcrypt.CompareHashAndPassword. It is a field so this package's
	// tests can COUNT comparisons per request; nothing else sets it.
	compareFn func(digest, password []byte) error
	// digestFn is hashPassword -- the enrollment's cost-Cost bcrypt GENERATION. A field
	// for the same reason as compareFn: the enrollment spends the process-wide budget
	// BEFORE it pays for a digest (flow.go), and only a count per request can show that
	// order; nothing else sets it.
	digestFn func(password string) (string, error)
}

// authKeys is the Authenticator's key material, each a Key: fmt reaches no method of
// an Authenticator value held in a caller's unexported struct field and prints its
// fields by reflection, and a Key's bytes sit behind a *string, which prints as an
// address on every verb (key.go). History, measured: the 6th round's []byte fields
// behind one *authKeys pointer printed all four under %s %q %e %f %t %c %U -- fmt's
// badVerb opens a pointer to a struct once (the 6th auditor, 7th round).
type authKeys struct {
	kek          Key
	tokenKey     Key
	challengeKey Key
	// pendingKey seals the enrollment page's blob: derived from the KEK (enrollment.go),
	// never the KEK itself.
	pendingKey Key
	// dummyDigest is a real cost-Cost bcrypt digest of 32 discarded random bytes,
	// made when the Authenticator is built. The comparison against it is what an
	// unknown address pays (password.go).
	dummyDigest Key
}

// The two holders of the keys redact themselves, the credential types' pattern
// (token.go): fmt's verbs through Format, slog through LogValue, encoding/json and
// every encoding.TextMarshaler consumer through MarshalText, and String/GoString for a
// caller that asks for them directly -- each emits the type's own placeholder, which
// names the type and is no other credential's
// (TestSessionToken_PlaceholderIsNotAnotherCredentialsPlaceholder). Value receivers, so
// a copied value (*a) redacts as the pointer does. The paths fmt reaches NO method on
// (a value in a caller's unexported field, %p, %w) print the fields by reflection; the
// keys there are Keys, which print an address -- measured on the leak test's matrix
// (TestLeak_NoSecretOnAnyPrintingPath). Measured before (OP-6 verification,
// 6th round, the 5th auditor): %v and %+v of New's *Authenticator and %+v of a Config
// printed TAPPA_OPERATOR_TOTP_KEK and TAPPA_OPERATOR_TOKEN_HMAC_KEY as decimal byte
// lists, and so did slog's text handler; encoding/json printed nothing only because
// Config's func field makes Marshal fail -- an accident, not a rule.
const (
	authenticatorRedacted = "operatorauth.Authenticator(redacted)"
	configRedacted        = "operatorauth.Config(redacted)"
)

var (
	_ fmt.Formatter          = Authenticator{}
	_ fmt.Stringer           = Authenticator{}
	_ fmt.GoStringer         = Authenticator{}
	_ slog.LogValuer         = Authenticator{}
	_ encoding.TextMarshaler = Authenticator{}
	_ fmt.Formatter          = Config{}
	_ fmt.Stringer           = Config{}
	_ fmt.GoStringer         = Config{}
	_ slog.LogValuer         = Config{}
	_ encoding.TextMarshaler = Config{}
)

func (Authenticator) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(authenticatorRedacted)) }
func (Authenticator) String() string               { return authenticatorRedacted }
func (Authenticator) GoString() string             { return authenticatorRedacted }
func (Authenticator) LogValue() slog.Value         { return slog.StringValue(authenticatorRedacted) }
func (Authenticator) MarshalText() ([]byte, error) { return []byte(authenticatorRedacted), nil }

func (Config) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(configRedacted)) }
func (Config) String() string               { return configRedacted }
func (Config) GoString() string             { return configRedacted }
func (Config) LogValue() slog.Value         { return slog.StringValue(configRedacted) }
func (Config) MarshalText() ([]byte, error) { return []byte(configRedacted), nil }

// New builds an Authenticator. It refuses a missing Store, a missing logger and a
// key of the wrong size rather than degrading; errors name lengths, never bytes.
//
// It costs one bcrypt at Cost (the dummy digest), paid once per process.
func New(store Store, cfg Config) (*Authenticator, error) {
	if err := checkConfig(store, cfg); err != nil {
		return nil, err
	}
	dummy, err := newDummyDigest()
	if err != nil {
		return nil, err
	}
	return build(store, cfg, dummy), nil
}

// checkConfig is New's refusals, before any work is paid for.
func checkConfig(store Store, cfg Config) error {
	if store == nil {
		return errors.New("operatorauth: a Store is required")
	}
	if cfg.Log == nil {
		return errors.New("operatorauth: a logger is required")
	}
	if n := cfg.TOTPKEK.size(); n != keyLen {
		return fmt.Errorf("operatorauth: the TOTP KEK must be %d bytes, got %d", keyLen, n)
	}
	if n := cfg.TokenHMACKey.size(); n != keyLen {
		return fmt.Errorf("operatorauth: the session HMAC key must be %d bytes, got %d", keyLen, n)
	}
	return nil
}

// build assembles an Authenticator around a dummy digest. New is its one production
// caller; this package's tests call it with ONE shared dummy so that each test does
// not pay a cost-12 bcrypt just to be constructed (measured: under -race in the full
// suite this package took 380 s, most of it constructors). The production path --
// New making its own dummy at Cost -- is what TestPassword_TheDigestAndTheDummyAreBothCostTwelve
// and the external tests drive.
func build(store Store, cfg Config, dummy []byte) *Authenticator {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Authenticator{
		store: store,
		keys: authKeys{
			kek:          cfg.TOTPKEK,
			tokenKey:     cfg.TokenHMACKey,
			challengeKey: NewKey(deriveChallengeKey(cfg.TokenHMACKey.bytes())),
			pendingKey:   NewKey(derivePendingKey(cfg.TOTPKEK.bytes())),
			dummyDigest:  NewKey(dummy),
		},
		now:       now,
		log:       cfg.Log,
		limits:    newLimits(now),
		compareFn: bcrypt.CompareHashAndPassword,
		digestFn:  hashPassword,
	}
}

// Issued is a new operator session: the raw token for the cookie (the only place it
// may go -- SetSessionCookie) and the operator it belongs to. The session row itself
// was written by the database in the same statement that advanced the TOTP step.
type Issued struct {
	Token   SessionToken
	AdminID uuid.UUID
}

// Identity is a live operator session as the session predicate resolved it.
type Identity struct {
	SessionID uuid.UUID
	AdminID   uuid.UUID
}
