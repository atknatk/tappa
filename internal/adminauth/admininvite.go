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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/internal/store"
)

// THE ADMINISTRATOR INVITATION (M14-15, ADR 0031 §5.11; M7-07's criteria in the identity model).
//
// An owner invites a second administrator -- a MANAGER -- by e-mail. The invitation is single use,
// expiring and stored as a hash (admin_invites, migration 00077; employee_invites' pattern, M5-02);
// the link goes to the invitee's own address and is never shown on the panel; the invitee sets their
// own password (or uses the sign-in they already have) -- the owner never knows it.
//
// 🔴 ISSUING READS NO MEMBERSHIP AND NO IDENTITY (R18). The owner must not learn whether the address
// already has a Taptime sign-in: the answer, its timing and the invitation's state on the list are one
// shape in both cases. Issue therefore writes the invitation (and retires the address's open one) and
// reads NOTHING about the address -- no resolver, no identity function, no admin_users row -- so the
// two cases run the same statements by construction (TestAdminInvites_IssuingTouchesNoIdentity holds
// the source to it; internal/handler measures the response and the clock).
//
// ACCEPTANCE IS THE DATABASE'S (ADR 0031 §5.2): identity_accept_invite (a live session of the
// identity whose address the invitation names -- the forwarded-link rule) and identity_accept_invite_new
// (an address with no identity: the person chooses a password; the identity is born verified). Both
// are keyed by the invitation token's hash; neither gives a session. The GET that shows the page never
// consumes anything (R19): Usable is the md.7 resolver, read-only.

// InviteTTL is how long an invitation link stays spendable. Three days -- long enough for a person
// to find the e-mail, short enough that a forgotten one does not lie in a mailbox for weeks; 00077's
// CHECK refuses anything past seven days whatever this constant says.
const InviteTTL = 72 * time.Hour

// InviteRoleManager is the one role an invitation grants (00077's CHECK; ownership moves by M14-16's
// two-step transfer, never by an e-mail link).
const InviteRoleManager = "manager"

// MaxInviteNameRunes is the longest name an invitation may carry (00077's CHECK; the employee name's
// ceiling, internal/domain/tenant).
const MaxInviteNameRunes = 120

const (
	inviteTokenBytes    = 32
	inviteTokenLen      = 43
	inviteRedacted      = "adminauth.InviteToken(redacted)"
	inviteTokenKeyLabel = "tappa/admin-invite/v1/key-derivation"
)

var (
	// ErrInviteUnusable is every way an invitation link cannot be used: unknown, malformed, spent,
	// cancelled, expired. One error, for ErrResetUnusable's reason: the page is reachable without
	// credentials.
	ErrInviteUnusable = errors.New("adminauth: invitation cannot be used")
	// ErrInviteNotAccepted is an acceptance the database refused although the link was usable a moment
	// before: the session is not the invited address's, the address already has a sign-in (the
	// new-password path) or is already in the business. The page says what the person can do next
	// without telling which.
	ErrInviteNotAccepted = errors.New("adminauth: invitation was not accepted")
	// ErrInviteAddress is a malformed address or name on the owner's form.
	ErrInviteAddress = errors.New("adminauth: invitation address or name is not acceptable")
	// ErrInviteBusy is the partial UNIQUE refusing a second open invitation for the address -- two
	// presses at once; the second is told to try again.
	ErrInviteBusy = errors.New("adminauth: another invitation for that address is being issued")
	// ErrInviteNotOwner: the inviter is not an active owner of the business (the INSERT's belt).
	ErrInviteNotOwner = errors.New("adminauth: the inviter is not an active owner of the business")
)

// InviteToken is a raw invitation token in transit -- ResetToken's twin, with its own placeholder and
// its own derived key, for ResetToken's reasons (a leaked invitation buys a manager's seat). The zero
// value is safe to hold and to print.
type InviteToken struct{ v *string }

var (
	_ fmt.Formatter          = InviteToken{}
	_ fmt.Stringer           = InviteToken{}
	_ fmt.GoStringer         = InviteToken{}
	_ slog.LogValuer         = InviteToken{}
	_ encoding.TextMarshaler = InviteToken{}
)

// ParseInviteToken wraps an untrusted string (the value out of an invitation link). It does not
// validate: hash holds the shape gate.
func ParseInviteToken(v string) InviteToken { return InviteToken{v: &v} }

func (t InviteToken) reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

// Format redacts every verb.
func (InviteToken) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(inviteRedacted)) }

// String redacts.
func (InviteToken) String() string { return inviteRedacted }

// GoString redacts.
func (InviteToken) GoString() string { return inviteRedacted }

// LogValue redacts.
func (InviteToken) LogValue() slog.Value { return slog.StringValue(inviteRedacted) }

// MarshalText redacts.
func (InviteToken) MarshalText() ([]byte, error) { return []byte(inviteRedacted), nil }

func newInviteToken() (InviteToken, error) {
	b := make([]byte, inviteTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return InviteToken{}, fmt.Errorf("adminauth: read randomness: %w", err)
	}
	return ParseInviteToken(base64.RawURLEncoding.EncodeToString(b)), nil
}

func deriveInviteKey(sessionKey []byte) ([]byte, error) {
	if len(sessionKey) != hmacKeyLen {
		return nil, fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(sessionKey))
	}
	m := hmac.New(sha256.New, sessionKey)
	_, _ = m.Write([]byte(inviteTokenKeyLabel)) // hash writes never fail (documented)
	return m.Sum(nil), nil
}

// hash is the 64-hex HMAC admin_invites.token_hash stores (00077's CHECK), after a shape gate so a
// junk parameter costs nothing and never reaches the database.
func (t InviteToken) hash(key []byte) (string, error) {
	if len(key) != hmacKeyLen {
		return "", fmt.Errorf("adminauth: hmac key must be %d bytes, got %d", hmacKeyLen, len(key))
	}
	v := t.reveal()
	if len(v) != inviteTokenLen {
		return "", ErrInviteUnusable
	}
	if _, err := base64.RawURLEncoding.DecodeString(v); err != nil {
		return "", ErrInviteUnusable
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// InvitePath is where an invitation link opens (internal/handler mounts it, flag-gated); the token is
// its one query parameter.
const (
	InvitePath  = "/admin/invite"
	InviteParam = "t"
)

// AdminInvite is one stored invitation as the owner's panel sees it. No token, no hash.
type AdminInvite struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	Email       string
	FullName    string
	Role        string
	InvitedBy   uuid.UUID
	CreatedAt   time.Time
	ExpiresAt   time.Time
	UsedAt      *time.Time
	CancelledAt *time.Time
}

// The four states an invitation is in on the owner's list. "accepted" says the person joined -- the
// same for a new and an existing sign-in (R18).
const (
	InviteStateOpen      = "open"
	InviteStateAccepted  = "accepted"
	InviteStateCancelled = "cancelled"
	InviteStateExpired   = "expired"
)

// State is the invitation's state at now.
func (a AdminInvite) State(now time.Time) string {
	switch {
	case a.UsedAt != nil:
		return InviteStateAccepted
	case a.CancelledAt != nil:
		return InviteStateCancelled
	case !now.Before(a.ExpiresAt):
		return InviteStateExpired
	}
	return InviteStateOpen
}

// IssuedInvite is an invitation just written and its raw token -- to be handed to the mail and then
// dropped. Retired is how many open invitations of the address this one replaced.
type IssuedInvite struct {
	Invite  AdminInvite
	Token   InviteToken
	Retired int64
}

// Link is the invitation's URL under base (TAPPA_BASE_URL). It is the §4.7 secret: built at the
// mail's call site and passed straight on, never logged.
func (i IssuedInvite) Link(base string) string {
	return strings.TrimRight(base, "/") + InvitePath + "?" + InviteParam + "=" + i.Token.reveal()
}

// InviteDatabase is the slice of *db.DB the invitation needs, declared at the consumer (§7).
type InviteDatabase interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	GetAdminInviteByTokenHash(ctx context.Context, tokenHash string) (db.ResolvedAdminInvite, error)
	AcceptAdminInvite(ctx context.Context, inviteHash, sessionHash string) (db.AcceptedAdminInvite, error)
	AcceptAdminInviteNew(ctx context.Context, inviteHash, newDigest string) (db.AcceptedAdminInvite, error)
}

var _ InviteDatabase = (*db.DB)(nil)

// AdminInvites issues, lists, cancels and accepts administrator invitations.
type AdminInvites struct {
	data InviteDatabase
	// inviteKey hashes invitation tokens; sessionKey hashes the panel cookie the session acceptance
	// carries (the Manager's derivation, token.go) -- the identity is found through it alone.
	inviteKey, sessionKey []byte
	ttl                   time.Duration
	now                   func() time.Time
	// digest is the new-password path's bcrypt (Hash in production; Resets.digest's reason for a field).
	digest func(password string) (string, error)
}

// NewAdminInvites builds the service from the session HMAC key (both keys derived from it).
func NewAdminInvites(data InviteDatabase, cfg *config.Config) (*AdminInvites, error) {
	if data == nil {
		return nil, errors.New("adminauth: nil database")
	}
	if cfg == nil {
		return nil, errors.New("adminauth: nil config")
	}
	ik, err := deriveInviteKey(cfg.SessionHMACKey)
	if err != nil {
		return nil, err
	}
	sk, err := deriveKey(cfg.SessionHMACKey)
	if err != nil {
		return nil, err
	}
	return &AdminInvites{data: data, inviteKey: ik, sessionKey: sk, ttl: InviteTTL, now: time.Now, digest: Hash}, nil
}

// Issue writes an invitation from the owner invitedBy of tenantID to email for fullName, retiring the
// address's open one -- one transaction. It reads nothing about the address (R18, this file's header).
// The caller (the panel) has already decided the actor may invite; the INSERT is written from the
// inviter's own row and writes nothing unless it is an active owner of tenantID (ErrInviteNotOwner).
// The trail row is the caller's: it is written once the e-mail's fate is known (sent or undelivered).
func (s *AdminInvites) Issue(ctx context.Context, tenantID, invitedBy uuid.UUID, email, fullName string) (IssuedInvite, error) {
	email = strings.TrimSpace(email)
	fullName = strings.TrimSpace(fullName)
	// THE ASCII ADDRESS RULE (the user's decision of 2026-10-26: "the stored address is an address we can
	// send to"): the address must pass the mail's own recipient rule before a row is stored -- printable
	// ASCII among it, so no homoglyph citext would fold onto somebody's address ('İ', the Kelvin sign)
	// is ever invited. A FORM check, before any read: R18 holds (it says nothing about any identity).
	if tenantID == uuid.Nil || invitedBy == uuid.Nil || !mail.ValidRecipient(email) || !isLookupableEmail(email) || !inviteNameOK(fullName) {
		return IssuedInvite{}, ErrInviteAddress
	}
	tok, err := newInviteToken()
	if err != nil {
		return IssuedInvite{}, err
	}
	h, err := tok.hash(s.inviteKey)
	if err != nil {
		return IssuedInvite{}, fmt.Errorf("adminauth: issue invite: %w", err)
	}
	out := IssuedInvite{Token: tok}
	err = s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		n, e := q.CancelOpenAdminInvitesForAddress(ctx, store.CancelOpenAdminInvitesForAddressParams{TenantID: tenantID, Email: email})
		if e != nil {
			return e
		}
		row, e := q.InsertAdminInvite(ctx, store.InsertAdminInviteParams{
			TenantID: tenantID, Email: email, FullName: fullName, Role: InviteRoleManager,
			TokenHash: h, InvitedBy: invitedBy, ExpiresAt: s.now().Add(s.ttl),
		})
		if e != nil {
			return e
		}
		out.Retired = n
		out.Invite = AdminInvite{ID: row.ID, TenantID: tenantID, Email: email, FullName: fullName, Role: InviteRoleManager,
			InvitedBy: invitedBy, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssuedInvite{}, ErrInviteNotOwner
		}
		if isUniqueViolation(err) {
			return IssuedInvite{}, ErrInviteBusy
		}
		return IssuedInvite{}, fmt.Errorf("adminauth: issue invite: %w", err)
	}
	return out, nil
}

// List is the business's invitations, newest first, at most 50.
func (s *AdminInvites) List(ctx context.Context, tenantID uuid.UUID) ([]AdminInvite, error) {
	var out []AdminInvite
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := store.New(tx).ListAdminInvites(ctx, tenantID)
		if e != nil {
			return e
		}
		out = make([]AdminInvite, 0, len(rows))
		for _, r := range rows {
			out = append(out, AdminInvite{ID: r.ID, TenantID: tenantID, Email: r.Email, FullName: r.FullName, Role: r.Role,
				InvitedBy: r.InvitedBy, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, UsedAt: r.UsedAt, CancelledAt: r.CancelledAt})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("adminauth: list invites: %w", err)
	}
	return out, nil
}

// Cancel retires one open invitation of tenantID. false: unknown here, spent or cancelled already.
func (s *AdminInvites) Cancel(ctx context.Context, tenantID, inviteID uuid.UUID) (bool, error) {
	cancelled := false
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).CancelAdminInvite(ctx, store.CancelAdminInviteParams{TenantID: tenantID, ID: inviteID})
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		cancelled = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("adminauth: cancel invite: %w", err)
	}
	return cancelled, nil
}

// Usable reports whether the invitation link could be accepted now -- the md.7 resolver, which writes
// nothing (the GET's question; R19: it is never consumed by being looked at). A malformed token is
// ErrInviteUnusable without a database call.
func (s *AdminInvites) Usable(ctx context.Context, t InviteToken) error {
	h, err := t.hash(s.inviteKey)
	if err != nil {
		return ErrInviteUnusable
	}
	r, err := s.data.GetAdminInviteByTokenHash(ctx, h)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInviteUnusable
	}
	if err != nil {
		return fmt.Errorf("adminauth: look up invite: %w", err)
	}
	if r.UsedAt != nil || r.CancelledAt != nil || !s.now().Before(r.ExpiresAt) {
		return ErrInviteUnusable
	}
	return nil
}

// AcceptWithSession accepts with the panel session cookie of the person the invitation names
// (identity_accept_invite). ErrInviteUnusable: the link was not usable; ErrInviteNotAccepted: it was,
// and the database refused (another address's session, an unlinked or ended session, or the address
// already in the business).
func (s *AdminInvites) AcceptWithSession(ctx context.Context, t InviteToken, session Token) (db.AcceptedAdminInvite, error) {
	ih, err := t.hash(s.inviteKey)
	if err != nil {
		return db.AcceptedAdminInvite{}, ErrInviteUnusable
	}
	sh, err := session.hash(s.sessionKey)
	if err != nil {
		return db.AcceptedAdminInvite{}, s.refusal(ctx, ih)
	}
	a, err := s.data.AcceptAdminInvite(ctx, ih, sh)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.AcceptedAdminInvite{}, s.refusal(ctx, ih)
	}
	if err != nil {
		return db.AcceptedAdminInvite{}, fmt.Errorf("adminauth: accept invite: %w", err)
	}
	return a, nil
}

// AcceptNew accepts for an address with no sign-in, with the password the invitee chose
// (identity_accept_invite_new). The caller has checked the two boxes (the panel's password rule).
// ErrInviteUnusable / ErrInviteNotAccepted as AcceptWithSession's.
func (s *AdminInvites) AcceptNew(ctx context.Context, t InviteToken, password string) (db.AcceptedAdminInvite, error) {
	ih, err := t.hash(s.inviteKey)
	if err != nil {
		return db.AcceptedAdminInvite{}, ErrInviteUnusable
	}
	// The link first: a dead link must not cost a bcrypt (and the page says the truer thing).
	if err := s.Usable(ctx, t); err != nil {
		return db.AcceptedAdminInvite{}, err
	}
	d, err := s.digest(password)
	if err != nil {
		return db.AcceptedAdminInvite{}, fmt.Errorf("adminauth: accept invite: %w", err)
	}
	a, err := s.data.AcceptAdminInviteNew(ctx, ih, d)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.AcceptedAdminInvite{}, s.refusal(ctx, ih)
	}
	if err != nil {
		return db.AcceptedAdminInvite{}, fmt.Errorf("adminauth: accept invite: %w", err)
	}
	return a, nil
}

// refusal tells a dead link from a live one the database refused, so the page can say "ask for a new
// link" or "sign in with the invited address". Both are facts about the LINK the visitor holds.
func (s *AdminInvites) refusal(ctx context.Context, inviteHash string) error {
	r, err := s.data.GetAdminInviteByTokenHash(ctx, inviteHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInviteUnusable
	}
	if err != nil {
		return fmt.Errorf("adminauth: look up invite: %w", err)
	}
	if r.UsedAt != nil || r.CancelledAt != nil || !s.now().Before(r.ExpiresAt) {
		return ErrInviteUnusable
	}
	return ErrInviteNotAccepted
}

// inviteNameOK: a filled name within 00077's ceiling, no control characters.
func inviteNameOK(s string) bool {
	if s == "" || len([]rune(s)) > MaxInviteNameRunes {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// isUniqueViolation matches SQLSTATE 23505 by code, never by message.
func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
