package adminauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// THE VERIFIED REGISTRATION (M14-17, ADR 0031 §5.10; migration 00078). A registration is a DRAFT until
// the address's mailbox confirms it: POST /signup opens a draft (Open) and the link goes to the address;
// the link's holder reads it back (Resolve -- the GET, which spends nothing) and confirms it (POST) with
// ONE of two proofs, each the only producer of a ClaimProof:
//
//   - A NEW ADDRESS (no identity): the confirmation page asks again for the password the form chose, and
//     ProveWithPassword compares it with the draft's digest. Only a match yields a proof (R2: the person
//     who clicks must be the person who chose the password -- an attacker who typed a victim's not yet
//     registered address cannot wait for the victim's click to birth an identity with the attacker's
//     password). THE COMPARISON IS HERE, IN THE APPLICATION (S12): the resolver returns the digest only
//     to the holder of the claim secret, and identity_claim_signup is never called without this match.
//   - AN EXISTING IDENTITY: the person signs in with their own password and confirms from that browser;
//     ProveWithSession hashes that browser's panel session cookie, and identity_claim_signup accepts it
//     only when the session's identity carries the draft's address (R18).
//   - AN EXISTING IDENTITY WITH NO ACTIVE MEMBERSHIP (M14-17b, the user's decision of 2026-10-27; 00080):
//     it cannot sign in, so the confirmation page asks for the IDENTITY'S password and
//     ProveWithIdentityPassword compares it with the digest resolve_identity_by_email returned (Resolve
//     keeps it only for such an identity). Only a match yields a proof, and the proof carries that very
//     digest: identity_claim_signup claims only while it is still the identity's digest, under the
//     person's lock, and only while the identity has no active membership (identity_open_session's rule).
//
// 🔴 ClaimProof IS THE GO-TYPE-BOUND GUARANTEE, NAMED LIKE identity_open_session's verifiedIdentity: its
// fields are unexported, so no other package can build one; its producers are ProveWithPassword,
// ProveWithSession and ProveWithIdentityPassword alone and its one consumer of db.ClaimSignup is ClaimProof.Claim
// (TestSignupClaimProof_HasItsProducersAndOneConsumer reads the source; TestClaimSignup_HasNoCallerOutsideTheProof
// reads the tree). The zero value claims nothing.
//
// WHAT NEVER LEAVES: the token (a ResetToken -- it cannot print itself), the digest (db.PasswordHash),
// the address and the draft (personal data -- never a log line or an error).

// SignupClaimTTL is how long a registration link lives: one day, 00078's ceiling (00019's for a link). A
// person registers, then opens their mail -- possibly the next morning.
const SignupClaimTTL = 24 * time.Hour

// signupClaimKeyLabel derives this flow's HMAC key from the session key, apart from every other link's.
const signupClaimKeyLabel = "tappa/admin-signup-claim/v1/key-derivation"

// ErrNoSuchClaim is Resolve's refusal: no draft carries the token (never issued, malformed or swept).
var ErrNoSuchClaim = errors.New("adminauth: no such registration link")

// SignupClaimDatabase is the narrow slice of *db.DB this flow needs, declared here at the consumer (§7).
// db.ClaimSignup is not in it: it runs on the registration's own transaction (ClaimProof.Claim).
type SignupClaimDatabase interface {
	// ResolveIdentityByEmail is the one e-mail -> identity path (md.7; it writes nothing): it decides which
	// letter a draft's address receives and which confirmation page its link opens.
	ResolveIdentityByEmail(ctx context.Context, email string) (db.ResolvedIdentity, error)
	OpenSignupClaim(ctx context.Context, tokenHash, email string, draft []byte, digest string, expiresAt time.Time) (uuid.UUID, error)
	GetSignupClaimByTokenHash(ctx context.Context, tokenHash string) (db.ResolvedSignupClaim, error)
	SweepSignupClaims(ctx context.Context) (int, error)
}

var _ SignupClaimDatabase = (*db.DB)(nil)

// SignupClaims opens, resolves and proves registration drafts. Dependencies are struct fields (§7).
type SignupClaims struct {
	data SignupClaimDatabase
	// claimKey hashes a draft's token; sessionKey hashes a panel session cookie (Manager's key), for the
	// existing identity's proof.
	claimKey, sessionKey []byte
	ttl                  time.Duration
	now                  func() time.Time
	// digest and compare are bcrypt (Hash, Compare) in production; fields for the -race tests' sake
	// (Resets.digest's reason, its measurement).
	digest  func(password string) (string, error)
	compare func(digest, password string) bool
}

// NewSignupClaims builds the flow; it refuses a wrong-sized key (NewResets' reason).
func NewSignupClaims(data SignupClaimDatabase, cfg *config.Config) (*SignupClaims, error) {
	if data == nil {
		return nil, errors.New("adminauth: nil database")
	}
	if cfg == nil {
		return nil, errors.New("adminauth: nil config")
	}
	if len(cfg.SessionHMACKey) != hmacKeyLen {
		return nil, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(cfg.SessionHMACKey))
	}
	m := hmac.New(sha256.New, cfg.SessionHMACKey)
	_, _ = m.Write([]byte(signupClaimKeyLabel)) // hash writes never fail (documented)
	sessionKey, err := deriveKey(cfg.SessionHMACKey)
	if err != nil {
		return nil, err
	}
	return &SignupClaims{data: data, claimKey: m.Sum(nil), sessionKey: sessionKey, ttl: SignupClaimTTL,
		now: time.Now, digest: Hash, compare: Compare}, nil
}

// ClaimGrant is a draft just opened: the token for the link (it cannot print itself), the address the
// link goes to (claimRecipient: a known address's REGISTERED spelling) and when it dies. KnownAddress says the address has an identity -- it picks the letter
// ("someone started a registration with this address: sign in and claim it") and NOTHING else: the
// response, its timing and every log line are the same for both (R18).
type ClaimGrant struct {
	ClaimID      uuid.UUID
	Token        ResetToken
	Recipient    string
	ExpiresAt    time.Time
	KnownAddress bool
}

// Link renders the confirmation URL (IssuedReset.Link's three obligations hold: never logged, never
// stored, handed to the one recipient).
func (g ClaimGrant) Link(base string) string {
	return base + "?t=" + g.Token.reveal()
}

// Open writes a draft for email and returns its grant. draft is the closed-key JSON
// internal/domain/signup encodes (validated there); password is the one the form chose, hashed here.
//
// THE SAME WORK FOR EVERY ADDRESS (R18): one bcrypt, one resolver read, one insert -- whether the
// address has an identity or not. pgx.ErrNoRows: the database refused the draft (a value its CHECKs would
// refuse, an address that is not ASCII) -- the caller's validation should have caught it first.
func (s *SignupClaims) Open(ctx context.Context, email, password string, draft []byte) (ClaimGrant, error) {
	digest, err := s.digest(password)
	if err != nil {
		return ClaimGrant{}, fmt.Errorf("adminauth: open signup claim: %w", err)
	}
	t, err := newResetToken()
	if err != nil {
		return ClaimGrant{}, fmt.Errorf("adminauth: open signup claim: %w", err)
	}
	h, err := t.hash(s.claimKey)
	if err != nil {
		return ClaimGrant{}, fmt.Errorf("adminauth: open signup claim: %w", err)
	}
	ident, known, err := s.identityOf(ctx, email)
	if err != nil {
		return ClaimGrant{}, fmt.Errorf("adminauth: open signup claim: %w", err)
	}
	expires := s.now().Add(s.ttl)
	id, err := s.data.OpenSignupClaim(ctx, h, email, draft, digest, expires)
	if err != nil {
		return ClaimGrant{}, fmt.Errorf("adminauth: open signup claim: %w", err)
	}
	return ClaimGrant{ClaimID: id, Token: t, Recipient: signupRecipientOf(email, ident, known).address, ExpiresAt: expires,
		KnownAddress: known}, nil
}

// signupRecipient is the address a draft's letter goes to (M14-17's audit; M14-14's identityRecipient
// rule): for a known address the identity's REGISTERED spelling -- a column of the same resolver row
// that said "known" -- never the visitor's typing, which is equal under citext and not byte for byte;
// for a new address the typed address, which is what the draft stores and the identity will be born
// with. Its one constructor is signupRecipientOf. The response and its timing do not depend on it.
type signupRecipient struct{ address string }

// signupRecipientOf is signupRecipient's only constructor.
func signupRecipientOf(typed string, ident db.ResolvedIdentity, known bool) signupRecipient {
	if known {
		return signupRecipient{address: ident.Email}
	}
	return signupRecipient{address: typed}
}

// identityOf resolves email to its identity, reporting whether there is one.
func (s *SignupClaims) identityOf(ctx context.Context, email string) (db.ResolvedIdentity, bool, error) {
	r, err := s.data.ResolveIdentityByEmail(ctx, email)
	switch {
	case err == nil:
		return r, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return db.ResolvedIdentity{}, false, nil
	}
	return db.ResolvedIdentity{}, false, err
}

// SignupClaim is a draft its link resolved to. Only the link's holder ever sees one.
type SignupClaim struct {
	hash   string
	digest db.PasswordHash
	// identityDigest is the address's identity's digest -- set only when IdentityPassword (M14-17b); the
	// zero value proves nothing (its "" matches no password).
	identityDigest db.PasswordHash

	ID        uuid.UUID
	Email     string
	Draft     []byte
	ExpiresAt time.Time
	UsedAt    *time.Time
	// KnownAddress: an identity carries Email now -- the existing identity's confirmation (sign in, then
	// confirm) rather than the new address's (re-enter the password).
	KnownAddress bool
	// IdentityPassword (M14-17b): KnownAddress, and that identity has NO active membership -- it cannot
	// sign in, so the confirmation asks for its password (ProveWithIdentityPassword) instead of a session.
	IdentityPassword bool
}

// Live reports whether the draft can still be confirmed (identity_claim_signup checks it again).
func (c SignupClaim) Live(now time.Time) bool { return c.UsedAt == nil && now.Before(c.ExpiresAt) }

// Resolve reads the draft a link names. It WRITES NOTHING (the confirmation page's GET, R19).
// ErrNoSuchClaim when no draft carries the token.
func (s *SignupClaims) Resolve(ctx context.Context, t ResetToken) (SignupClaim, error) {
	h, err := t.hash(s.claimKey)
	if err != nil {
		if errors.Is(err, errResetMalformed) {
			return SignupClaim{}, ErrNoSuchClaim
		}
		return SignupClaim{}, fmt.Errorf("adminauth: resolve signup claim: %w", err)
	}
	r, err := s.data.GetSignupClaimByTokenHash(ctx, h)
	if errors.Is(err, pgx.ErrNoRows) {
		return SignupClaim{}, ErrNoSuchClaim
	}
	if err != nil {
		return SignupClaim{}, fmt.Errorf("adminauth: resolve signup claim: %w", err)
	}
	ident, known, err := s.identityOf(ctx, r.Email)
	if err != nil {
		return SignupClaim{}, fmt.Errorf("adminauth: resolve signup claim: %w", err)
	}
	c := SignupClaim{hash: h, digest: r.PasswordHash, ID: r.ID, Email: r.Email, Draft: r.Draft,
		ExpiresAt: r.ExpiresAt, UsedAt: r.UsedAt, KnownAddress: known}
	if known && !ident.HasActiveMembership {
		c.IdentityPassword, c.identityDigest = true, ident.PasswordHash
	}
	return c, nil
}

// Now is the flow's clock (the handler asks Live with it).
func (s *SignupClaims) Now() time.Time { return s.now() }

// ClaimProof is the ONLY way to identity_claim_signup (see this file's header): a draft's hash with
// a matched password of the draft (nothing else), a panel session's hash, or the identity's digest a
// matched password was compared with (verified). Its zero value claims nothing.
type ClaimProof struct {
	hash     string
	session  *string
	verified db.PasswordHash
	draft    []byte
}

// Draft is the proven draft's JSON: the registration the caller creates the business from.
func (p ClaimProof) Draft() []byte { return p.draft }

// errNoProof is Claim's answer for a ClaimProof nobody proved (the zero value).
var errNoProof = errors.New("adminauth: claim signup: no proof")

// Claim consumes the draft inside the registration's own TOP-LEVEL transaction (tx, under tenantID's
// context -- the business this transaction just created) and returns the owner row. pgx.ErrNoRows (wrapped)
// is identity_claim_signup's refusal; the caller rolls back.
func (p ClaimProof) Claim(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (uuid.UUID, error) {
	if p.hash == "" {
		return uuid.Nil, errNoProof
	}
	return db.ClaimSignup(ctx, tx, p.hash, tenantID, p.session, p.verified)
}

// ProveWithPassword is the new address's proof: the re-entered password against the draft's digest
// (one bcrypt comparison, always paid). ok is false on a mismatch -- nothing is proved and nothing may be
// created (R2).
func (s *SignupClaims) ProveWithPassword(c SignupClaim, password string) (ClaimProof, bool) {
	if !s.compare(c.digest.RevealForPasswordComparison(), password) || c.hash == "" {
		return ClaimProof{}, false
	}
	return ClaimProof{hash: c.hash, draft: c.Draft}, true
}

// ProveWithSession is the existing identity's proof: the panel session cookie of the confirming browser,
// hashed as the panel hashes it. identity_claim_signup decides whether that session's person is the
// draft's address (R18); a malformed cookie proves nothing.
func (s *SignupClaims) ProveWithSession(c SignupClaim, session Token) (ClaimProof, bool) {
	h, err := session.hash(s.sessionKey)
	if err != nil || c.hash == "" {
		return ClaimProof{}, false
	}
	return ClaimProof{hash: c.hash, session: &h, draft: c.Draft}, true
}

// ProveWithIdentityPassword is the proof of an existing identity with NO active membership (M14-17b): the
// re-entered password against the IDENTITY's digest -- the one resolve_identity_by_email returned to
// Resolve (one bcrypt comparison, always paid). ok is false on a mismatch and for any other draft (its
// identity digest is the zero value, which matches nothing). The proof carries that digest:
// identity_claim_signup claims only while it is still the identity's, and only while the identity has no
// active membership -- a person who can sign in proves themself with ProveWithSession.
func (s *SignupClaims) ProveWithIdentityPassword(c SignupClaim, password string) (ClaimProof, bool) {
	if !s.compare(c.identityDigest.RevealForPasswordComparison(), password) || c.hash == "" || !c.IdentityPassword {
		return ClaimProof{}, false
	}
	return ClaimProof{hash: c.hash, verified: c.identityDigest, draft: c.Draft}, true
}

// Sweep deletes the expired and the used drafts (identity_sweep_signup_claims, R17).
func (s *SignupClaims) Sweep(ctx context.Context) (int, error) {
	n, err := s.data.SweepSignupClaims(ctx)
	if err != nil {
		return 0, fmt.Errorf("adminauth: sweep signup claims: %w", err)
	}
	return n, nil
}

// ParseSignupClaimToken wraps an untrusted link value (ParseResetToken's contract: no validation here,
// the shape gate is the hash).
func ParseSignupClaimToken(v string) ResetToken { return ParseResetToken(strings.TrimSpace(v)) }
