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

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/mail"
)

// identityemail.go -- THE SIGN-IN ADDRESS CHANGE (ADR 0044; T173 EC-2). Three acts, each finding the
// person from a SECRET (CLAUDE.md §4.5 (3), ADR 0044 §5.3 -- no new named exception):
//
//	the request   RequestEmailChange: the LINKED session's cookie + the identity's current password
//	              (compared here, re-checked by identity_request_email_change under the identity's lock)
//	              -> one 'change' link for the typed address, 24 hours. The answer never says whether
//	              the new address belongs to anybody (ADR 0044 §5.4): the resolver is read only to
//	              choose the letter (a link, or the linkless "it already has one" note -- K-E5 (a)),
//	              and both arms write one row and one event.
//	the press     ConfirmEmailChange: the mailed link (spent on the POST, never on its GET -- R19) AND
//	              the clicking browser's live session of the same identity (R2) ->
//	              identity_change_email moves the identity and every linked row in one transaction and
//	              writes the undo link whose token this file drew for the old address's letter.
//	the undo      UndoEmailChange: the undo link alone (a reset-class link, named in ADR 0031 §6.1 and
//	              CLAUDE.md §4.5 (3) since EC-1) -> identity_revert_email_change. It opens no session.
//
// 🔴 TWO NEW HASH KEYS, ONE PER PURPOSE (ADR 0044 §5.3, card EC-2 item 10): a change token presented as
// an undo token -- or as a verification or a link link -- hashes to a value no row of the other purpose
// holds. Both are derived from TAPPA_SESSION_HMAC_KEY under labels of their own, deriveVerifyKey's shape:
// no new secret, no new environment variable. TestEmailChangeKeys_NoPurposeHashesToAnother holds the four
// apart.
//
// 🔴 §7 -- the addresses these acts return are the letters' recipients and nothing else: never logged,
// never in an error. The tokens reach the letters only (Link / UndoLink); the database holds hashes.

var (
	// ErrEmailChangeRefused is the change link's ONE refusal (ADR 0044 §5.3): unknown, malformed,
	// cancelled, spent, expired, pressed from a browser without a live session of the link's identity,
	// the address taken since, a same-address row in one of the person's businesses, a precondition
	// that no longer holds. Not told apart.
	ErrEmailChangeRefused = errors.New("adminauth: the address change link was not accepted from this browser")
	// ErrEmailUndoRefused is the undo link's ONE refusal: unknown, malformed, cancelled, spent, expired,
	// or the old address belongs to another sign-in now (or to another row in one of the person's
	// businesses). Not told apart.
	ErrEmailUndoRefused = errors.New("adminauth: the address change could not be undone with that link")
	// ErrEmailNotAddress: the typed address is not one this product can send to (mail.ValidRecipient)
	// -- refused before the password is compared.
	ErrEmailNotAddress = errors.New("adminauth: the new address cannot receive e-mail")
)

// The two purposes' labels: each separates its hash from every other HMAC this package makes.
const (
	changeKeyLabel = "tappa/admin-email-change/v1/key-derivation"
	undoKeyLabel   = "tappa/admin-email-change-undo/v1/key-derivation"
)

// deriveChangeKey is the change link's HMAC key, from the session HMAC key (deriveVerifyKey's shape).
func deriveChangeKey(sessionKey []byte) ([]byte, error) {
	return deriveLabelled(sessionKey, changeKeyLabel)
}

// deriveUndoKey is the undo link's HMAC key, from the session HMAC key under a label of its own.
func deriveUndoKey(sessionKey []byte) ([]byte, error) {
	return deriveLabelled(sessionKey, undoKeyLabel)
}

func deriveLabelled(sessionKey []byte, label string) ([]byte, error) {
	if len(sessionKey) != hmacKeyLen {
		return nil, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(label)) // hash writes never fail (documented)
	return m.Sum(nil), nil
}

// ChangeEmailPath / ChangeEmailParam and UndoEmailPath / UndoEmailParam are where the two links point,
// under the deployment's base. internal/handler mounts the routes at these paths and web/templates/email
// checks each link against its own copy.
const (
	ChangeEmailPath  = "/admin/change-email"
	ChangeEmailParam = "t"
	UndoEmailPath    = "/admin/undo-email-change"
	UndoEmailParam   = "t"
)

// EmailChangeRequested is what a request did. Exactly one of three:
//   - Refusal set: nothing was written (the person's own state, or the shape of what they typed --
//     db.EmailChangeRefusals; ExpiresAt is the live undo link's end for EmailChangeRevertLive);
//   - Taken: the row and the event were written, and the letter is the linkless note (the address
//     already has a sign-in; K-E5 (a)) -- Link is "";
//   - otherwise the letter carries Link.
//
// Recipient is the address the person typed -- the mailbox being proven; personal data, never logged.
type EmailChangeRequested struct {
	ID        uuid.UUID
	ExpiresAt time.Time
	Recipient string
	Taken     bool
	Refusal   db.EmailChangeRefusal
	token     Token
}

// Link is the change letter's one link: base + ChangeEmailPath + "?t=" + the token; "" for a refusal
// and for the linkless note.
func (e EmailChangeRequested) Link(base string) string {
	if e.Refusal != "" || e.Taken || e.token.v == nil {
		return ""
	}
	return base + ChangeEmailPath + "?" + ChangeEmailParam + "=" + e.token.reveal()
}

// RequestEmailChange is the request (ADR 0044 §5.1 step 1; card EC-2 item 2): the work gate, the new
// address's shape, ONE comparison of password with the identity's digest (identity_password_for_session),
// the read-only resolver for the letter's choice, then identity_request_email_change. ErrNotLinked for a
// session whose row is not linked (the handler refuses those before any charge), ErrBadCredentials for a
// wrong password, ErrEmailNotAddress for an address nobody can send to. current is the cookie's token.
//
// THE THREE ARMS ADR 0044 §5.4 HOLDS FLAT -- a new address nobody holds, one an identity holds, one with
// a row in another business -- run the same statements in the same order: one comparison, one resolver
// read, one definer call that writes one row and one event. Only the letter differs, and it is rendered
// and sent off the request path (internal/handler).
func (m *Manager) RequestEmailChange(ctx context.Context, s Resolved, current Token, password, newEmail string) (EmailChangeRequested, error) {
	release, err := m.gate.enter(ctx)
	if err != nil {
		return EmailChangeRequested{}, err
	}
	defer release()
	if !s.Linked || s.switchBucket == ([sha256.Size]byte{}) {
		return EmailChangeRequested{}, ErrNotLinked
	}
	newEmail = strings.TrimSpace(newEmail)
	if !mail.ValidRecipient(newEmail) || !isLookupableEmail(newEmail) {
		return EmailChangeRequested{}, ErrEmailNotAddress
	}
	sh, err := current.hash(m.hmacKey.Reveal())
	if err != nil {
		if errors.Is(err, errMalformed) {
			return EmailChangeRequested{}, ErrNoSession
		}
		return EmailChangeRequested{}, fmt.Errorf("adminauth: request email change: %w", err)
	}
	digest, err := m.data.IdentityPasswordForSession(ctx, sh)
	if errors.Is(err, pgx.ErrNoRows) {
		// The session died or is not linked after all: one comparison anyway, so this exit costs what a
		// wrong password costs.
		m.payOneComparison(password, "")
		return EmailChangeRequested{}, ErrNotLinked
	}
	if err != nil {
		return EmailChangeRequested{}, fmt.Errorf("adminauth: request email change: %w", err)
	}
	if !m.compare(digest.RevealForPasswordComparison(), password) {
		return EmailChangeRequested{}, ErrBadCredentials
	}
	// THE LETTER'S CHOICE, AND NOTHING ELSE: resolve_identity_by_email writes nothing, and its answer
	// never reaches the response (ADR 0044 §5.4's "Veritabanı işi" row).
	_, err = m.data.ResolveIdentityByEmail(ctx, newEmail)
	taken := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return EmailChangeRequested{}, fmt.Errorf("adminauth: request email change: %w", err)
	}
	t, err := newToken()
	if err != nil {
		return EmailChangeRequested{}, fmt.Errorf("adminauth: request email change: %w", err)
	}
	h, err := t.hash(m.changeKey.Reveal())
	if err != nil {
		return EmailChangeRequested{}, fmt.Errorf("adminauth: request email change: %w", err)
	}
	r, err := m.data.RequestEmailChange(ctx, sh, digest, h, newEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		// The session died, or lost its link, since the comparison.
		return EmailChangeRequested{}, ErrNotLinked
	}
	if err != nil {
		return EmailChangeRequested{}, fmt.Errorf("adminauth: request email change: %w", err)
	}
	if r.Refusal != "" {
		return EmailChangeRequested{Refusal: r.Refusal, ExpiresAt: r.ExpiresAt}, nil
	}
	return EmailChangeRequested{ID: r.ChangeID, ExpiresAt: r.ExpiresAt, Recipient: newEmail, Taken: taken, token: t}, nil
}

// EmailChanged is what a confirmed change did: the identity (for the notice's per-person cap and its log
// line), the two addresses (the OLD one receives the notice -- never logged), this business's revoked
// sessions, and the undo link's end (nil: none was written -- the linkless notice).
type EmailChanged struct {
	IdentityID      uuid.UUID
	OldEmail        string
	NewEmail        string
	RevokedSessions int
	UndoExpiresAt   *time.Time
	At              time.Time
	undo            Token
}

// UndoLink is the notice's undo link: base + UndoEmailPath + "?t=" + the token; "" when the change wrote
// no undo link.
func (c EmailChanged) UndoLink(base string) string {
	if c.UndoExpiresAt == nil || c.undo.v == nil {
		return ""
	}
	return base + UndoEmailPath + "?" + UndoEmailParam + "=" + c.undo.reveal()
}

// ConfirmEmailChange spends the change link raw with the cookie's session current (R2, R19). The session
// s must be LINKED and its row's address must name the session's own identity (identityOfSession) --
// read BEFORE the change, which rewrites that address. ErrEmailChangeRefused is its one refusal.
func (m *Manager) ConfirmEmailChange(ctx context.Context, s Resolved, raw string, current Token) (EmailChanged, error) {
	ident, err := m.identityOfSession(ctx, s)
	if errors.Is(err, ErrNotLinked) {
		return EmailChanged{}, ErrEmailChangeRefused
	}
	if err != nil {
		return EmailChanged{}, fmt.Errorf("adminauth: confirm email change: %w", err)
	}
	h, err := wrap(raw).hash(m.changeKey.Reveal())
	if err != nil {
		if errors.Is(err, errMalformed) {
			return EmailChanged{}, ErrEmailChangeRefused
		}
		return EmailChanged{}, fmt.Errorf("adminauth: confirm email change: %w", err)
	}
	sh, err := current.hash(m.hmacKey.Reveal())
	if err != nil {
		if errors.Is(err, errMalformed) {
			return EmailChanged{}, ErrEmailChangeRefused
		}
		return EmailChanged{}, fmt.Errorf("adminauth: confirm email change: %w", err)
	}
	u, err := newToken()
	if err != nil {
		return EmailChanged{}, fmt.Errorf("adminauth: confirm email change: %w", err)
	}
	uh, err := u.hash(m.undoKey.Reveal())
	if err != nil {
		return EmailChanged{}, fmt.Errorf("adminauth: confirm email change: %w", err)
	}
	c, err := m.data.ChangeEmail(ctx, h, sh, uh)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailChanged{}, ErrEmailChangeRefused
	}
	if err != nil {
		return EmailChanged{}, fmt.Errorf("adminauth: confirm email change: %w", err)
	}
	return EmailChanged{
		IdentityID: ident.ID, OldEmail: c.OldEmail, NewEmail: c.NewEmail, RevokedSessions: c.RevokedSessions,
		UndoExpiresAt: c.RevertExpiresAt, At: time.Now(), undo: u,
	}, nil
}

// EmailRestored is what an undo did: the address given back (the K-E11 reset link's recipient --
// never logged) and how many sessions ended, in all the person's businesses together.
type EmailRestored struct {
	Address         string
	RevokedSessions int
}

// UndoEmailChange spends the undo link raw (one secret -- the reset class; ADR 0044 §5.3): the old
// address back, the password a sentinel, every session ended. It opens no session. ErrEmailUndoRefused
// is its one refusal.
func (m *Manager) UndoEmailChange(ctx context.Context, raw string) (EmailRestored, error) {
	h, err := wrap(raw).hash(m.undoKey.Reveal())
	if err != nil {
		if errors.Is(err, errMalformed) {
			return EmailRestored{}, ErrEmailUndoRefused
		}
		return EmailRestored{}, fmt.Errorf("adminauth: undo email change: %w", err)
	}
	r, err := m.data.RevertEmailChange(ctx, h)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailRestored{}, ErrEmailUndoRefused
	}
	if err != nil {
		return EmailRestored{}, fmt.Errorf("adminauth: undo email change: %w", err)
	}
	return EmailRestored{Address: r.RestoredEmail, RevokedSessions: r.RevokedSessions}, nil
}
