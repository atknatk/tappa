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
// e-mail verification, the confirmation window (its read, "Keep all", unlink and its Undo) and -- M14-22,
// in place of the two-password "Link a business" -- the link of the person's other businesses by e-mail
// proof.
//
// 🔴 THE PERSON IS FOUND THROUGH A SECRET, AND THE SECRET IS THE SESSION. Every call below takes the
// request's VERIFIED session (Resolved -- the tenant, the row, whether it is linked, the opaque
// person bucket) and, where an identity function is keyed by a session, a session token's hash: the
// cookie's own, or a FRESH one this package has just opened after the password was typed again. A
// tenant from the request is only ever a CHOICE the identity functions either accept or refuse (the
// window's unlink / Undo target), never an authority.
//
// 🔴 FRESH SESSIONS, AND THEIR END (ADR 0031 §5.8; S4). identity_unlink_backfilled and
// identity_relink_refused want sessions authenticated in the last five minutes; the page's session is
// usually older. So each of those acts asks for the identity's password again and opens a fresh
// session for the call (proveIdentity -- ONE comparison, a dummy one when the address has none ->
// openIdentitySession -> identity_open_session: authenticated_at = now(), the business's ordinary
// sign-in row), and that fresh session is REVOKED before the call returns -- the cookie the person
// holds is not touched, so no answer here writes a Set-Cookie. The caller writes the business an
// ordinary sign-out row for each fresh identity session (Temporary). (M14-18's two-password "Link a
// business", identity_link_legacy's account path, was removed by M14-22; the sign-in's automatic link
// with the same password -- identity.go -- still calls it.)

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

// rowFacts is what a test's rowFn hands the account acts for the session row (production reads the
// address alone -- rowEmail). digest and status are the shape the comparison-count tests were written
// in; nothing reads them since M14-22 removed the reverse link.
type rowFacts struct {
	email, digest, status string
}

// rowEmail is the session row's own address: the key every identity lookup below starts from. ""
// when the row has none. It reads the address alone (GetAdminByID), never the digest: the banner asks
// it on every panel section of a linked sign-in (M14-22).
func (m *Manager) rowEmail(ctx context.Context, s Resolved) (string, error) {
	if m.rowFn != nil {
		f, err := m.rowFn(s)
		return f.email, err
	}
	var email string
	err := m.data.WithTenant(ctx, s.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetAdminByID(ctx, store.GetAdminByIDParams{ID: s.AdminUserID, TenantID: s.TenantID})
		if e != nil {
			return e
		}
		if row.Email != nil {
			email = *row.Email
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("adminauth: read the session's row: %w", err)
	}
	return email, nil
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
// window. (M14-18's reverse link, which guessed the identity's password from such a row under a key of
// its own, was removed by M14-22.)
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

// ---------------------------------------------------------------- link by e-mail --

// THE LINK BY E-MAIL (ADR 0031 §5.2's and §5.7's M14-22 notes; migration 00084). The user's decisions
// of 2026-10-28/29: the proof that another business's row is the person's is the MAILBOX -- the
// mainstream pattern (Slack's "find your workspaces" confirmation; one account and a workspace switcher
// in Notion, Linear) -- not two passwords; and nothing names a business before that proof.
//
//	the banner     HasBusinessToAdd: a LINKED session whose address holds an active, unlinked row in
//	               another business (resolve_admin_by_email, md.7, read-only) -- a yes or a no, never a
//	               name or a count
//	"Add it"       RequestLink: ONE mailed link (identity_request_verification, purpose 'link', 24 h),
//	               bound to the IDENTITY, sent to its registered address
//	the link       its GET spends nothing (R19); LinkByEmail spends it on the POST with THIS browser's
//	               live session of the same identity (R2) -- two secrets, the identity found from them
//	               (CLAUDE.md §4.5 (3): no named exception) -- and every active, unrefused, unlinked row
//	               of the identity's address is linked in one transaction (identity_link_by_email)

var (
	// ErrNothingToAdd: the session's address holds no business to add -- no link was written.
	ErrNothingToAdd = errors.New("adminauth: no other business uses this address")
	// ErrLinkRefused is the link link's ONE refusal: unknown, malformed, spent, expired, a verification
	// link, or pressed from a browser without a live session of the link's identity. Not told apart.
	ErrLinkRefused = errors.New("adminauth: the link was not accepted from this browser")
)

// linkKeyLabel separates the link link's hash from every other HMAC this package makes -- the
// verification link's included: a verification token presented as a link (or the reverse) hashes to a
// value no row holds. The database refuses both directions besides: identity_link_by_email spends
// 'link' rows alone and identity_verify_email (redefined by 00084) 'verify' rows alone.
const linkKeyLabel = "tappa/admin-email-link/v1/key-derivation"

// deriveLinkKey is the link link's HMAC key, from the session HMAC key (deriveVerifyKey's shape).
func deriveLinkKey(sessionKey []byte) ([]byte, error) {
	if len(sessionKey) != hmacKeyLen {
		return nil, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(linkKeyLabel)) // hash writes never fail (documented)
	return m.Sum(nil), nil
}

// LinkPath and LinkParam are where a link link points: LinkPath + "?" + LinkParam + "=" + the token,
// under the deployment's base. internal/handler mounts the route at this path and web/templates/email
// checks the link against its own copy.
const (
	LinkPath  = "/admin/link-businesses"
	LinkParam = "t"
)

// IssuedLink is a link link just written. Its token is a credential: it reaches the e-mail and nothing
// else -- never a log, a response or a database row (the database holds its hash).
type IssuedLink struct {
	ID        uuid.UUID
	ExpiresAt time.Time
	// Recipient is the identity's REGISTERED address (resolve_identity_by_email's email, the same row as
	// the id the session's bucket was matched against) -- personal data, never logged.
	Recipient string
	token     Token
}

// Link is the e-mail's one link: base + LinkPath + "?t=" + the token.
func (l IssuedLink) Link(base string) string {
	return base + LinkPath + "?" + LinkParam + "=" + l.token.reveal()
}

// linkBucketPrefix keeps the link's mail budget apart from the verification's -- for a VERIFIED address
// only (LinkBucket).
const linkBucketPrefix = "link|"

// LinkBucket is the rate bucket of the link by e-mail's request ("Add it"): the person's own
// (AccountBucket), under a prefix of its own while the identity's address is verified. While it is NOT
// verified the bucket IS the verification's, unprefixed: both requests mail the same unproven address, and
// together they may send it no more than the one budget (the audits' DÜŞÜK, 2026-10-29) -- otherwise an
// unverified address could be sent twice the verification's mails an hour. ErrNotLinked as
// identityOfSession says it.
func (m *Manager) LinkBucket(ctx context.Context, s Resolved) (string, error) {
	b, err := m.AccountBucket(ctx, s)
	if err != nil {
		return "", err
	}
	ident, err := m.identityOfSession(ctx, s)
	if err != nil {
		return "", err
	}
	if ident.EmailVerifiedAt == nil {
		return b, nil
	}
	return linkBucketPrefix + b, nil
}

// unlinkedAtAddress reports whether address holds an active row that is linked to no identity -- a
// business the person may add. The resolver drops refused rows (unlinked_at, 00072).
func (m *Manager) unlinkedAtAddress(ctx context.Context, address string) (bool, error) {
	if address == "" || !isLookupableEmail(address) {
		return false, nil
	}
	rows, err := m.data.GetAdminByEmail(ctx, address)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if r.IdentityID == nil && r.Status == "active" {
			return true, nil
		}
	}
	return false, nil
}

// HasBusinessToAdd reports whether the panel's banner is drawn for the session s: its row is LINKED and
// its address holds an active row of another business that is linked to nobody. An unlinked session --
// an older per-business sign-in -- never (it is not the identity, and the identity's mailbox proof is
// the identity's to give). It is a yes or a no: the business is not named before the mailbox proof (the
// user's decision of 2026-10-29). It writes nothing.
//
// ⚠️ A ROW citext FOLDS ONTO THE ADDRESS (U+0130, U+212A) COUNTS HERE, and identity_link_by_email never
// links it (00076's address rule): such a person would see the banner after the link too. Named, not
// closed: no such row exists (deploy/README.md's M14-08a counts), and 00076 stops new ones being born.
func (m *Manager) HasBusinessToAdd(ctx context.Context, s Resolved) (bool, error) {
	if !s.Linked || s.switchBucket == ([sha256.Size]byte{}) {
		return false, nil
	}
	address, err := m.rowEmail(ctx, s)
	if err != nil {
		return false, err
	}
	ok, err := m.unlinkedAtAddress(ctx, address)
	if err != nil {
		return false, fmt.Errorf("adminauth: businesses to add: %w", err)
	}
	return ok, nil
}

// RequestLink writes ONE link link for the identity behind the linked session s and returns it for the
// caller to mail to Recipient; it lives 24 hours (00084). ErrNotLinked for an unlinked session (or one
// whose address names another identity), ErrNothingToAdd when the address holds no business to add --
// then nothing is written and nothing is to be mailed. current is the cookie's token.
func (m *Manager) RequestLink(ctx context.Context, s Resolved, current Token) (IssuedLink, error) {
	ident, err := m.identityOfSession(ctx, s)
	if err != nil {
		return IssuedLink{}, err
	}
	add, err := m.unlinkedAtAddress(ctx, ident.Email)
	if err != nil {
		return IssuedLink{}, fmt.Errorf("adminauth: request link: %w", err)
	}
	if !add {
		return IssuedLink{}, ErrNothingToAdd
	}
	sh, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return IssuedLink{}, ErrNoSession
		}
		return IssuedLink{}, fmt.Errorf("adminauth: request link: %w", err)
	}
	t, err := newToken()
	if err != nil {
		return IssuedLink{}, fmt.Errorf("adminauth: request link: %w", err)
	}
	h, err := t.hash(m.linkKey)
	if err != nil {
		return IssuedLink{}, fmt.Errorf("adminauth: request link: %w", err)
	}
	v, err := m.data.RequestIdentityLink(ctx, sh, h)
	if errors.Is(err, pgx.ErrNoRows) {
		// The session died, or lost its link, since the checks above.
		return IssuedLink{}, ErrNotLinked
	}
	if err != nil {
		return IssuedLink{}, fmt.Errorf("adminauth: request link: %w", err)
	}
	return IssuedLink{ID: v.ID, ExpiresAt: v.ExpiresAt, Recipient: ident.Email, token: t}, nil
}

// LinkedBusinesses is what a spent link did.
type LinkedBusinesses struct {
	// Linked is how many businesses were added to the sign-in (0 when none was left to add).
	Linked int
	// FirstVerification: the click was the address's first mailbox proof (a first_verify window opened).
	FirstVerification bool
	// Recipient is the identity's REGISTERED address -- the one notice's recipient; never logged.
	Recipient string
}

// LinkByEmail spends the link link raw with the cookie's session current (R2, R19: on the POST, never
// on the GET that carried it) and links every business of the identity's address (00084). ErrLinkRefused
// is its one refusal.
func (m *Manager) LinkByEmail(ctx context.Context, raw string, current Token) (LinkedBusinesses, error) {
	h, err := wrap(raw).hash(m.linkKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return LinkedBusinesses{}, ErrLinkRefused
		}
		return LinkedBusinesses{}, fmt.Errorf("adminauth: link by e-mail: %w", err)
	}
	sh, err := current.hash(m.hmacKey)
	if err != nil {
		if errors.Is(err, errMalformed) {
			return LinkedBusinesses{}, ErrLinkRefused
		}
		return LinkedBusinesses{}, fmt.Errorf("adminauth: link by e-mail: %w", err)
	}
	l, err := m.data.LinkByEmail(ctx, h, sh)
	if errors.Is(err, pgx.ErrNoRows) {
		return LinkedBusinesses{}, ErrLinkRefused
	}
	if err != nil {
		return LinkedBusinesses{}, fmt.Errorf("adminauth: link by e-mail: %w", err)
	}
	return LinkedBusinesses{Linked: l.Linked, FirstVerification: l.FirstVerification, Recipient: l.Email}, nil
}
