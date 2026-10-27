package adminauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// identityaccount.go -- THE ACCOUNT PAGE'S IDENTITY ACTS (ADR 0031 §5.2, §5.7, §5.8; M14-18): the
// e-mail verification, the confirmation window (its read, "Keep all", unlink and its Undo) and
// "Link a business" in both directions.
//
// 🔴 THE PERSON IS FOUND THROUGH A SECRET, AND THE SECRET IS THE SESSION. Every call below takes the
// request's VERIFIED session (Resolved -- the tenant, the row, whether it is linked, the opaque
// person bucket) and, where an identity function is keyed by a session, a session token's hash: the
// cookie's own, or a FRESH one this package has just opened after the password was typed again. A
// tenant from the request is only ever a CHOICE the identity functions either accept or refuse (the
// window's unlink / Undo target), never an authority.
//
// 🔴 FRESH SESSIONS, AND THEIR END (ADR 0031 §5.7 (2), §5.8; S6). identity_link_legacy,
// identity_unlink_backfilled and identity_relink_refused want sessions authenticated in the last
// five minutes; the page's session is usually older. So each of those acts asks for the password
// again and opens a fresh session for the call:
//
//	the identity side  proveIdentity (ONE comparison with the identity's digest, a dummy one when
//	                   the address has none) -> openIdentitySession -> identity_open_session
//	                   (authenticated_at = now(), the business's ordinary sign-in row)
//	the legacy side    the row's own digest compared -> openLegacyProofSession (CreateAdminSession's
//	                   digest predicate; authenticated_at's DEFAULT)
//
// and each fresh session is REVOKED before the call returns -- the cookie the person holds is not
// touched, so no answer here writes a Set-Cookie. identity_link_legacy revokes the legacy session
// itself on success; every other fresh session is revoked here (ignoring the request's
// cancellation, revokeProofs' reason). The caller writes the business an ordinary sign-out row for
// each fresh identity session (Temporary): identity_open_session wrote its sign-in row.
//
// 🔴 ONE RESULT FOR "NOTHING TO LINK" AND "WRONG OTHER PASSWORD" (ADR 0031 §5.7 (2)). LinkBusiness
// answers Linked == 0 with a nil error for both, and its work is the same for both: the forward
// direction pays 1 + MaxCandidates comparisons whatever the address holds (the window padded --
// Manager.pad), the reverse 1 + 1 (the identity's digest or the dummy). Only the person's OWN
// current password being wrong is told apart (ErrBadCredentials) -- it is a fact about their own
// credential, not about any other row.

var (
	// ErrNotLinked: the act needs the session's row linked to an identity (a verification, the
	// window) and it is not -- or the identity the row's address names is not the session's.
	ErrNotLinked = errors.New("adminauth: this sign-in is not linked to a Taptime identity")
	// ErrAlreadyVerified: the identity's address is verified already; no link was written.
	ErrAlreadyVerified = errors.New("adminauth: the address is already verified")
	// ErrVerificationRefused is the verification link's ONE refusal: unknown, malformed, spent,
	// expired, or clicked from a browser without a live session of the link's identity (R2). They are
	// not told apart.
	ErrVerificationRefused = errors.New("adminauth: the verification link was not accepted from this browser")
	// ErrWindowRefused is the window's ONE refusal for Keep all, unlink and Undo: the window is closed,
	// the row is not on its list (or not refused in it), or the session died meanwhile.
	ErrWindowRefused = errors.New("adminauth: the confirmation window refused that")
)

// verifyKeyLabel separates the verification link's hash from every other HMAC this package makes.
const verifyKeyLabel = "tappa/admin-email-verification/v1/key-derivation"

// deriveVerifyKey is the verification link's HMAC key, from the session HMAC key (token.go's shape).
func deriveVerifyKey(sessionKey []byte) ([]byte, error) {
	if len(sessionKey) != hmacKeyLen {
		return nil, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(verifyKeyLabel)) // hash writes never fail (documented)
	return m.Sum(nil), nil
}

// VerifyPath and VerifyParam are where a verification link points: VerifyPath + "?" + VerifyParam +
// "=" + the token, under the deployment's base. internal/handler mounts the route at this path and
// web/templates/email checks the link against its own copy.
const (
	VerifyPath  = "/admin/verify-email"
	VerifyParam = "t"
)

// IssuedVerification is a verification link just written. Its token is a credential: it reaches the
// e-mail and nothing else -- never a log, a response or a database row (the database holds its hash).
type IssuedVerification struct {
	ID        uuid.UUID
	ExpiresAt time.Time
	// Recipient is the identity's REGISTERED address (resolve_identity_by_email's email, the same row
	// as the id the session's bucket was matched against) -- personal data, never logged.
	Recipient string
	token     Token
}

// Link is the e-mail's one link: base + VerifyPath + "?t=" + the token.
func (v IssuedVerification) Link(base string) string {
	return base + VerifyPath + "?" + VerifyParam + "=" + v.token.reveal()
}

// rowFacts is what the account acts read of the session row itself, in its own tenant: its address
// (GetAdminByID) and its digest and status (GetAdminPasswordHashByID -- the reverse link's own-password
// comparison). The digest never leaves this package.
type rowFacts struct {
	email, digest, status string
}

// rowFactsOf reads them (rowFn in a test; production never sets it -- compareFn's precedent: the
// comparison-count tests run without a database).
func (m *Manager) rowFactsOf(ctx context.Context, s Resolved) (rowFacts, error) {
	if m.rowFn != nil {
		return m.rowFn(s)
	}
	var f rowFacts
	err := m.data.WithTenant(ctx, s.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, e := q.GetAdminByID(ctx, store.GetAdminByIDParams{ID: s.AdminUserID, TenantID: s.TenantID})
		if e != nil {
			return e
		}
		if row.Email != nil {
			f.email = *row.Email
		}
		d, e := q.GetAdminPasswordHashByID(ctx, store.GetAdminPasswordHashByIDParams{ID: s.AdminUserID, TenantID: s.TenantID})
		if e != nil {
			return e
		}
		f.digest, f.status = d.PasswordHash, d.Status
		return nil
	})
	if err != nil {
		return rowFacts{}, fmt.Errorf("adminauth: read the session's row: %w", err)
	}
	return f, nil
}

// rowEmail is the session row's own address: the key every identity lookup below starts from. ""
// when the row has none.
func (m *Manager) rowEmail(ctx context.Context, s Resolved) (string, error) {
	f, err := m.rowFactsOf(ctx, s)
	return f.email, err
}

// identityOfSession resolves the identity a LINKED session's row names by its address and holds it
// to the session: its bucket must be the session's (the same MAC of the same id). An address that
// names no identity, or another one, is ErrNotLinked.
func (m *Manager) identityOfSession(ctx context.Context, s Resolved) (db.ResolvedIdentity, error) {
	if !s.Linked || s.switchBucket == ([sha256.Size]byte{}) {
		return db.ResolvedIdentity{}, ErrNotLinked
	}
	email, err := m.rowEmail(ctx, s)
	if err != nil {
		return db.ResolvedIdentity{}, err
	}
	if email == "" || !isLookupableEmail(email) {
		return db.ResolvedIdentity{}, ErrNotLinked
	}
	ident, err := m.data.ResolveIdentityByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ResolvedIdentity{}, ErrNotLinked
	}
	if err != nil {
		return db.ResolvedIdentity{}, err
	}
	b := m.switchBucketOf(&ident.ID)
	if !hmac.Equal(b[:], s.switchBucket[:]) {
		return db.ResolvedIdentity{}, ErrNotLinked
	}
	return ident, nil
}

// AccountBucket is the rate bucket of the account page's identity acts (M14-18): the LINKED session's
// own person (Resolved.SwitchBucket); for an unlinked row the row itself ("row:" + its id). An opaque
// string: no id of an identity leaves this package.
//
// 🔴 AN UNLINKED ROW NEVER GETS THE IDENTITY'S BUCKET (M14-18's security audit, YÜKSEK). A row whose
// address names an identity is not that identity -- the pre-seeded attacker of ADR 0031 §5.8 (S3, T1)
// holds exactly such a row. Charging the identity's bucket from it let eleven junk presses an
// unlinked row sent lock the real person out of "Remove" and of the verification for the whole
// window. The reverse link's guesses against the identity are bounded by ReverseBucket instead, a
// key the identity's own acts never use.
func (m *Manager) AccountBucket(_ context.Context, s Resolved) (string, error) {
	if b, ok := s.SwitchBucket(); ok && s.Linked {
		return b, nil
	}
	return m.rowBucket(s), nil
}

// rowBucketLabel is the domain of a row's own bucket under this package's key.
const rowBucketLabel = "tappa/admin-account-row-bucket/v1|"

// rowBucket is an unlinked row's own bucket: an opaque MAC of the row's id (no id in a limiter key).
func (m *Manager) rowBucket(s Resolved) string {
	h := hmac.New(sha256.New, m.hmacKey)
	_, _ = h.Write([]byte(rowBucketLabel + s.TenantID.String() + "|" + s.AdminUserID.String())) // hash writes never fail
	return "row:" + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// ReverseBucket bounds the reverse link's guesses of an identity's password from unlinked rows: the
// identity the row's address names, under a prefix no other act uses ("reverse|" + its bucket), or
// the row itself when the address names none. Guesses from many unlinked rows of one address share
// it; the identity's own acts (verification, unlink, Undo, link) never charge it.
func (m *Manager) ReverseBucket(ctx context.Context, s Resolved) (string, error) {
	email, err := m.rowEmail(ctx, s)
	if err != nil {
		return "", err
	}
	if email != "" && isLookupableEmail(email) {
		ident, err := m.data.ResolveIdentityByEmail(ctx, email)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("adminauth: reverse bucket: %w", err)
		}
		if err == nil {
			b := m.switchBucketOf(&ident.ID)
			return "reverse|" + base64.RawURLEncoding.EncodeToString(b[:]), nil
		}
	}
	return "reverse|" + m.rowBucket(s), nil
}

// ---------------------------------------------------------------- verification --

// RequestVerification writes an e-mail verification link for the identity behind the linked session
// s (identity_request_verification: bound to the IDENTITY, S16). current is the cookie's token. The
// link is the caller's to mail to Recipient; it lives 24 hours (00072).
func (m *Manager) RequestVerification(ctx context.Context, s Resolved, current Token) (IssuedVerification, error) {
	ident, err := m.identityOfSession(ctx, s)
	if err != nil {
		return IssuedVerification{}, err
	}
	if ident.EmailVerifiedAt != nil {
		return IssuedVerification{}, ErrAlreadyVerified
	}
	sh, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return IssuedVerification{}, ErrNoSession
		}
		return IssuedVerification{}, fmt.Errorf("adminauth: request verification: %w", err)
	}
	t, err := newToken()
	if err != nil {
		return IssuedVerification{}, fmt.Errorf("adminauth: request verification: %w", err)
	}
	h, err := t.hash(m.verifyKey)
	if err != nil {
		return IssuedVerification{}, fmt.Errorf("adminauth: request verification: %w", err)
	}
	v, err := m.data.RequestIdentityVerification(ctx, sh, h)
	if errors.Is(err, pgx.ErrNoRows) {
		// The session died, or the address was verified, since the checks above.
		return IssuedVerification{}, ErrAlreadyVerified
	}
	if err != nil {
		return IssuedVerification{}, fmt.Errorf("adminauth: request verification: %w", err)
	}
	return IssuedVerification{ID: v.ID, ExpiresAt: v.ExpiresAt, Recipient: ident.Email, token: t}, nil
}

// VerifyEmail spends the verification link raw with the cookie's session current (R2, R19: the link
// is spent here, on the POST, never on the GET that carried it). It reports whether this was the
// identity's first verification (a first_verify window opened). ErrVerificationRefused is its one
// refusal: a click from a browser without a live session of the link's identity verifies nothing.
func (m *Manager) VerifyEmail(ctx context.Context, raw string, current Token) (bool, error) {
	h, err := wrap(raw).hash(m.verifyKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return false, ErrVerificationRefused
		}
		return false, fmt.Errorf("adminauth: verify e-mail: %w", err)
	}
	sh, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return false, ErrVerificationRefused
		}
		return false, fmt.Errorf("adminauth: verify e-mail: %w", err)
	}
	first, err := m.data.VerifyIdentityEmail(ctx, h, sh)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrVerificationRefused
	}
	if err != nil {
		return false, fmt.Errorf("adminauth: verify e-mail: %w", err)
	}
	return first, nil
}

// ---------------------------------------------------------------- the window --

// WindowRow is one row of the confirmation list: a membership on it, or a row refused in this window
// (Refused: its Undo is open while the window is).
type WindowRow struct {
	TenantID   uuid.UUID
	TenantName string
	Role       string
	Refused    bool
	// LastActiveOwner: the row is its business's last active owner -- the page warns before an unlink
	// (S4); the unlink is still allowed (ONAY notu 4).
	LastActiveOwner bool
}

// Window is the person's confirmation window as identity_confirm_window read it (00079).
type Window struct {
	// Linked is false for a session whose row is not linked (no identity, no window, no verification):
	// every other field is then zero.
	Linked   bool
	Verified bool
	// Open: the address is verified and the window has not ended or been kept.
	Open      bool
	Scope     string
	OpenUntil time.Time
	Rows      []WindowRow
}

// ConfirmWindow reads the window of the person behind the cookie's session. It writes nothing.
func (m *Manager) ConfirmWindow(ctx context.Context, current Token) (Window, error) {
	h, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return Window{}, ErrNoSession
		}
		return Window{}, fmt.Errorf("adminauth: confirm window: %w", err)
	}
	rows, err := m.data.ConfirmWindow(ctx, h)
	if err != nil {
		return Window{}, fmt.Errorf("adminauth: confirm window: %w", err)
	}
	if len(rows) == 0 {
		return Window{}, nil
	}
	head := rows[0]
	if head.TenantID != nil {
		return Window{}, errors.New("adminauth: confirm window: the first row is not the header")
	}
	w := Window{Linked: true, Verified: head.Verified}
	if head.Scope != nil && head.OpenUntil != nil {
		w.Open, w.Scope, w.OpenUntil = true, *head.Scope, *head.OpenUntil
	}
	for _, r := range rows[1:] {
		if r.TenantID == nil {
			return Window{}, errors.New("adminauth: confirm window: a list row without a business")
		}
		row := WindowRow{TenantID: *r.TenantID, Refused: r.Refused, LastActiveOwner: r.LastActiveOwner}
		if r.TenantName != nil {
			row.TenantName = *r.TenantName
		}
		if r.Role != nil {
			row.Role = *r.Role
		}
		w.Rows = append(w.Rows, row)
	}
	return w, nil
}

// KeepAll closes the window of the person behind the cookie's session ("Keep all", T11): one click,
// a LIVE session, no password (the user's decision of 2026-10-27; its accepted risk is ADR 0031
// §5.8's note of that day). ErrWindowRefused when no window is open.
func (m *Manager) KeepAll(ctx context.Context, current Token) error {
	h, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return ErrNoSession
		}
		return fmt.Errorf("adminauth: keep all: %w", err)
	}
	err = m.data.KeepAll(ctx, h)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWindowRefused
	}
	if err != nil {
		return fmt.Errorf("adminauth: keep all: %w", err)
	}
	return nil
}

// FreshAct is what an act that opened fresh sessions leaves the caller: the identity sessions it
// opened (each wrote its business an ordinary sign-in row) -- all revoked again before the act
// returned (Revoked says whether each revocation committed). The caller writes each an ordinary
// sign-out row.
type FreshAct struct {
	Temporary []Session
	Revoked   []bool
}

func (f *FreshAct) ended(m *Manager, ctx context.Context, s Session) {
	err := m.Revoke(context.WithoutCancel(ctx), s.TenantID, s.ID)
	f.Temporary = append(f.Temporary, s)
	f.Revoked = append(f.Revoked, err == nil || errors.Is(err, ErrNoSession))
}

// proveIdentity is the identity half of a fresh session: ONE comparison of password with the digest
// of the identity the address names -- a dummy comparison when it names none, so the two cost the
// same. It returns the proven identity (nil when the password did not verify), the resolver's row,
// and whether the address names an identity at all.
//
// 🔴 IT IS verifiedIdentity's SECOND PRODUCER, and like the first (AuthenticatePerson) it builds the
// type in one place, right after the comparison returned true
// (TestVerifiedIdentity_HasOneProducerAndOneConsumer names both). The guarantee is the same: no
// identity session opens for a password nobody typed.
func (m *Manager) proveIdentity(ctx context.Context, email, password string) (*verifiedIdentity, db.ResolvedIdentity, bool, error) {
	if email == "" || password == "" || !isLookupableEmail(email) {
		m.payOneComparison(password, "")
		return nil, db.ResolvedIdentity{}, false, nil
	}
	ident, err := m.data.ResolveIdentityByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		m.payOneComparison(password, "")
		return nil, db.ResolvedIdentity{}, false, nil
	}
	if err != nil {
		return nil, db.ResolvedIdentity{}, false, fmt.Errorf("adminauth: prove identity: %w", err)
	}
	if !m.compare(ident.PasswordHash.RevealForPasswordComparison(), password) {
		return nil, ident, true, nil
	}
	return &verifiedIdentity{id: ident.ID, digest: ident.PasswordHash}, ident, true, nil
}

// freshIdentitySession re-proves the identity password behind the LINKED session s and opens a
// fresh identity session in s's own business (or, if that membership has gone, wherever
// identity_open_session lands). The identity is the one the row's address names AND the session's
// (its bucket); a wrong password records the identity's failure event (the sign-in's budget) and is
// ErrBadCredentials.
func (m *Manager) freshIdentitySession(ctx context.Context, s Resolved, password string, budget FailureBudget) (Issued, string, error) {
	if !s.Linked || s.switchBucket == ([sha256.Size]byte{}) {
		m.payOneComparison(password, "")
		return Issued{}, "", ErrNotLinked
	}
	email, err := m.rowEmail(ctx, s)
	if err != nil {
		return Issued{}, "", err
	}
	v, ident, has, err := m.proveIdentity(ctx, email, password)
	if err != nil {
		return Issued{}, "", err
	}
	if b := m.switchBucketOf(&ident.ID); !has || !hmac.Equal(b[:], s.switchBucket[:]) {
		return Issued{}, "", ErrNotLinked
	}
	if v == nil {
		if err := m.recordFailure(ctx, true, ident.ID, budget); err != nil {
			return Issued{}, "", err
		}
		return Issued{}, "", ErrBadCredentials
	}
	issued, h, err := m.openIdentitySession(ctx, *v, s.TenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		// identity_open_session refused under the person's lock: a reset since, or no active membership.
		return Issued{}, "", ErrWindowRefused
	}
	if err != nil {
		return Issued{}, "", fmt.Errorf("adminauth: fresh identity session: %w", err)
	}
	return issued, h, nil
}

// Unlink refuses the person's membership in target from the open window's list (ADR 0031 §5.8, S4):
// the identity password typed again, a fresh session, identity_unlink_backfilled. The fresh session is
// revoked before the answer. CurrentEnded: target is s's own business -- the unlink revoked this
// business's sessions, the cookie's among them.
func (m *Manager) Unlink(ctx context.Context, s Resolved, password string, target uuid.UUID, budget FailureBudget) (FreshAct, bool, error) {
	issued, h, err := m.freshIdentitySession(ctx, s, password, budget)
	if err != nil {
		return FreshAct{}, false, err
	}
	var act FreshAct
	_, err = m.data.UnlinkMembership(ctx, h, target)
	act.ended(m, ctx, issued.Session)
	if errors.Is(err, pgx.ErrNoRows) {
		return act, false, ErrWindowRefused
	}
	if err != nil {
		return act, false, fmt.Errorf("adminauth: unlink: %w", err)
	}
	return act, target == s.TenantID, nil
}

// Relink takes back a row refused in THIS window (the Undo, S4/T4): the identity password typed
// again, a fresh session, identity_relink_refused. The fresh session is revoked before the answer.
func (m *Manager) Relink(ctx context.Context, s Resolved, password string, target uuid.UUID, budget FailureBudget) (FreshAct, error) {
	issued, h, err := m.freshIdentitySession(ctx, s, password, budget)
	if err != nil {
		return FreshAct{}, err
	}
	var act FreshAct
	_, err = m.data.RelinkMembership(ctx, h, target)
	act.ended(m, ctx, issued.Session)
	if errors.Is(err, pgx.ErrNoRows) {
		return act, ErrWindowRefused
	}
	if err != nil {
		return act, fmt.Errorf("adminauth: relink: %w", err)
	}
	return act, nil
}

// ---------------------------------------------------------------- link a business --

// LinkResult is what LinkBusiness did. Linked is how many rows it linked -- 0 for "nothing to link"
// and for "wrong other password" alike (the one result).
type LinkResult struct {
	FreshAct
	Linked int
}

// LinkBusiness links business rows of the same address to one sign-in, proven by BOTH credentials
// (ADR 0031 §5.7 (2)). current is the password of the session s's own row (asked again: S6), other
// the other side's:
//
//	s linked     (forward)  current = the identity's; other = an older per-business password -- every
//	                        unlinked row of the address it verifies (at most MaxCandidates, padded) is
//	                        linked;
//	s unlinked   (reverse)  current = this row's; other = the identity's password -- this row is linked
//	                        to the identity its address names (M14-13's Choose paragraph sends an
//	                        unlinked person here).
//
// ErrBadCredentials only when current is wrong. budget meters the identity's failure events.
func (m *Manager) LinkBusiness(ctx context.Context, s Resolved, current, other string, budget FailureBudget) (LinkResult, error) {
	if budget == nil {
		return LinkResult{}, errors.New("adminauth: link a business: no failure budget")
	}
	if s.Linked {
		return m.linkForward(ctx, s, current, other, budget)
	}
	return m.linkReverse(ctx, s, current, other, budget)
}

func (m *Manager) linkForward(ctx context.Context, s Resolved, current, other string, budget FailureBudget) (LinkResult, error) {
	issued, identityHash, err := m.freshIdentitySession(ctx, s, current, budget)
	if err != nil {
		return LinkResult{}, err
	}
	var res LinkResult
	n, err := m.linkCandidates(ctx, s, identityHash, other)
	res.Linked = n
	res.ended(m, ctx, issued.Session)
	return res, err
}

// linkCandidates compares other with the address's UNLINKED rows (T2; a refused row is none -- the
// resolver drops it), at most MaxCandidates, incumbent first, padded to MaxCandidates -- the sign-in's
// legacy arm -- and links every active one it verifies with a fresh proof session.
func (m *Manager) linkCandidates(ctx context.Context, s Resolved, identityHash, other string) (int, error) {
	email, err := m.rowEmail(ctx, s)
	if err != nil {
		return 0, err
	}
	var rows []db.ResolvedAdmin
	if email != "" && isLookupableEmail(email) {
		rows, err = m.data.GetAdminByEmail(ctx, email)
		if err != nil {
			return 0, fmt.Errorf("adminauth: link a business: %w", err)
		}
	}
	legacy := make([]db.ResolvedAdmin, 0, len(rows))
	for _, r := range rows {
		if r.IdentityID == nil {
			legacy = append(legacy, r)
		}
	}
	if len(legacy) > MaxCandidates {
		legacy = legacy[:MaxCandidates]
	}
	ref := ""
	if len(legacy) > 0 {
		ref = legacy[0].PasswordHash.RevealForPasswordComparison()
	}
	type match struct {
		admin, tenant uuid.UUID
		digest        db.PasswordHash
	}
	var matched []match
	for _, c := range legacy {
		// Every row is compared, a disabled one included; the status is read afterwards.
		ok := m.compare(c.PasswordHash.RevealForPasswordComparison(), other)
		if ok && c.Status == "active" {
			matched = append(matched, match{c.ID, c.TenantID, c.PasswordHash})
		}
	}
	m.pad(other, ref, len(legacy))

	linked := 0
	for _, c := range matched {
		p, err := m.openLegacyProofSession(ctx, c.admin, c.tenant, c.digest)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // the row changed since it verified: nothing to link
		}
		if err != nil {
			return linked, fmt.Errorf("adminauth: link a business: %w", err)
		}
		if _, err := m.data.LinkLegacySession(ctx, identityHash, p.hash); err != nil {
			// Refused (or failed): the function leaves the proof session live -- revoked here.
			if rerr := m.revokeProofs(ctx, []proofSession{p}); rerr != nil {
				return linked, errors.Join(err, rerr)
			}
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return linked, fmt.Errorf("adminauth: link a business: %w", err)
		}
		linked++
	}
	return linked, nil
}

func (m *Manager) linkReverse(ctx context.Context, s Resolved, current, other string, budget FailureBudget) (LinkResult, error) {
	// This row's own password, typed again, compared with the row's digest.
	f, err := m.rowFactsOf(ctx, s)
	if errors.Is(err, pgx.ErrNoRows) {
		return LinkResult{}, ErrNoSuchAdmin
	}
	if err != nil {
		return LinkResult{}, fmt.Errorf("adminauth: link a business: %w", err)
	}
	if f.status != "active" {
		return LinkResult{}, ErrNoSuchAdmin
	}
	if !m.compare(f.digest, current) {
		return LinkResult{}, ErrBadCredentials
	}
	// The identity side: ONE comparison whatever the address holds (a dummy when it names no
	// identity), and ONE event step whatever the outcome (recordFailure: the identity's
	// login_failed within its budget, the same call for an id no identity carries otherwise) -- the
	// sign-in's shape, so a present and an absent identity do the same work (M14-18's audit, D3).
	v, ident, has, err := m.proveIdentity(ctx, f.email, other)
	if err != nil {
		return LinkResult{}, err
	}
	if v == nil {
		if err := m.recordFailure(ctx, has, ident.ID, budget); err != nil {
			return LinkResult{}, err
		}
		return LinkResult{}, nil // the one result: nothing linked
	}
	// Both passwords verified: the two fresh sessions, the link.
	proof, err := m.openLegacyProofSession(ctx, s.AdminUserID, s.TenantID, db.NewPasswordHash(f.digest))
	if errors.Is(err, pgx.ErrNoRows) {
		return LinkResult{}, ErrNoSuchAdmin
	}
	if err != nil {
		return LinkResult{}, fmt.Errorf("adminauth: link a business: %w", err)
	}
	var res LinkResult
	n, err := m.linkThisRow(ctx, &res, proof, *v)
	res.Linked = n
	if n == 0 {
		// Not consumed by identity_link_legacy (which revokes it on success): revoked here.
		if rerr := m.revokeProofs(ctx, []proofSession{proof}); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}
	return res, err
}

// linkThisRow opens the proven identity's fresh session and links the proof session's row to it. 0
// with a nil error when there is nothing to link to (a reset since, no active membership, or the
// link refused).
func (m *Manager) linkThisRow(ctx context.Context, res *LinkResult, proof proofSession, v verifiedIdentity) (int, error) {
	issued, identityHash, err := m.openIdentitySession(ctx, v, uuid.Nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("adminauth: link a business: %w", err)
	}
	_, err = m.data.LinkLegacySession(ctx, identityHash, proof.hash)
	res.ended(m, ctx, issued.Session)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("adminauth: link a business: %w", err)
	}
	return 1, nil
}
