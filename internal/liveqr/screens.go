package liveqr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// ScreenSessionLifetime is how long a paired screen's session lives, counted from its pairing
// (live_qr_displays.paired_at) -- ABSOLUTE: polling does not extend it (the security lens's ORTA-1,
// SEÇİM 180 days). A copied tappa_screen cookie therefore stops minting codes at the latest half a
// year after the pairing, and the venue pairs the screen again twice a year -- the panel says
// "Pairing ran out" and the screen shows the pairing form. The cookie's own Max-Age is the same
// figure (SessionMaxAge), so a browser forgets it when the server would refuse it.
//
// WHY 180 DAYS AND NOT LESS, weighed rather than measured (no screen has run in production yet,
// so no screen's life has been measured): re-pairing needs a manager at the panel and someone at
// the screen; every month would make a wall TV a chore, while a year is the employee session's
// cookie and would leave a copied cookie working twice as long. last_seen_at (00114) is the other
// half: a screen polling from somewhere it should not shows up in the panel well before the end.
const ScreenSessionLifetime = 180 * 24 * time.Hour

// PairingLifetime is how long a pairing code can be used: long enough to walk from the office to
// the screen and type 13 characters on a TV remote, short enough that a code read over a shoulder
// is soon worthless. 00111 caps it at one hour.
const PairingLifetime = 10 * time.Minute

// The audit actions (`<subject>.<past participle>`, the shape adminlogin.go and the locations
// section use). Every pairing and every revocation is one row (the card); so is the creation, which
// is the admin's act -- the pairing itself has no admin behind it (actor nil).
const (
	ActionScreenCreated        = "live_qr_screen.created"
	ActionScreenPaired         = "live_qr_screen.paired"
	ActionScreenPairingRefused = "live_qr_screen.pairing_refused"
	ActionScreenRevoked        = "live_qr_screen.revoked"
)

// The domain's refusals. Fixed sentences (§7): none names a code, a session or a token.
var (
	// ErrCodeRefused is every refused pairing: unknown, used, expired, revoked. The screen hears one
	// sentence for all of them -- the code's own state is no business of a caller that does not
	// hold a valid one.
	ErrCodeRefused = errors.New("liveqr: the pairing code was refused")
	// ErrNoScreen is a screen session that names no paired screen.
	ErrNoScreen = errors.New("liveqr: no paired screen for this session")
	// ErrRevoked is a screen whose session resolves to a revoked row.
	ErrRevoked = errors.New("liveqr: the screen was removed")
	// ErrLapsed is a screen whose session is older than ScreenSessionLifetime: paired again or nothing.
	ErrLapsed = errors.New("liveqr: the screen's pairing ran out")
	// ErrNoCode is Token's refusal of a screen that may not show a code now: its business is
	// suspended, its venue switched off, or its pairing unknown or run out.
	ErrNoCode = errors.New("liveqr: this screen shows no code now")
	// ErrUnknownLocation is a location that is not this business's.
	ErrUnknownLocation = errors.New("liveqr: no such venue in this business")
	// ErrUnknownScreen is a screen id that is not this business's.
	ErrUnknownScreen = errors.New("liveqr: no such screen in this business")
)

// Database is the slice of *db.DB this package needs, declared at the consumer (CLAUDE.md §7): the
// tenant-scoped transaction, and the two context-less resolutions the screen page needs (ADR 0002
// md.7; 00111) -- a pairing request and a screen session carry no tenant, the tenant is the RESULT.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	GetLiveQRDisplayByPairingCodeHash(ctx context.Context, codeHash string) (db.ResolvedLiveQRPairing, error)
	GetLiveQRDisplayBySessionHash(ctx context.Context, sessionHash string) (db.ResolvedLiveQRSession, error)
}

// Trail is the audit recorder's two writes, at the consumer.
type Trail interface {
	Record(ctx context.Context, e audit.Event) (uuid.UUID, error)
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Screens pairs, resolves, lists and revokes the venue screens, and mints their tokens.
type Screens struct {
	keys  *Keys
	data  Database
	trail Trail
	now   func() time.Time
}

// NewScreens builds the service. Every dependency is required.
func NewScreens(keys *Keys, data Database, trail Trail) (*Screens, error) {
	if keys == nil || data == nil || trail == nil {
		return nil, errors.New("liveqr: NewScreens needs its keys, the database and the trail")
	}
	return &Screens{keys: keys, data: data, trail: trail, now: time.Now}, nil
}

// --- creation (the panel) -----------------------------------------------------------------------

// CreateCommand is the panel's "Add a live QR screen": which business, which admin, which venue.
type CreateCommand struct {
	TenantID   uuid.UUID
	ActorID    uuid.UUID
	LocationID uuid.UUID
}

// Created is a new screen waiting for its code. Code is shown ONCE (PairingCode.ForThePanelOnce)
// and kept nowhere: the database holds its digest.
type Created struct {
	ID        uuid.UUID
	Code      PairingCode
	ExpiresAt time.Time
}

// createAttempts bounds the redraws on a code digest another waiting screen holds (23505). With 65
// bits one redraw is already a 2^-65 event; the bound exists so a broken random source cannot loop.
const createAttempts = 3

// Create records a screen waiting for a fresh pairing code at one of the business's venues, and the
// creation's audit row in the same transaction. The tenant's own expired codes are swept first
// (00111: a dead code must not hold its value in the global UNIQUE).
func (s *Screens) Create(ctx context.Context, c CreateCommand) (Created, error) {
	if c.TenantID == uuid.Nil || c.ActorID == uuid.Nil || c.LocationID == uuid.Nil {
		return Created{}, errors.New("liveqr: create: a business, an admin and a venue are required")
	}
	var out Created
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, err := q.ClearExpiredLiveQRPairingCodes(ctx, c.TenantID); err != nil {
			return fmt.Errorf("sweep expired codes: %w", err)
		}
		for attempt := 1; ; attempt++ {
			code, err := NewPairingCode()
			if err != nil {
				return err
			}
			codeDigest, err := s.keys.CodeDigest(code)
			if err != nil {
				return err
			}
			// A SAVEPOINT per attempt: a 23505 aborts the transaction it happens in, and the
			// redraw must not take the sweep with it.
			sp, err := tx.Begin(ctx)
			if err != nil {
				return err
			}
			row, err := store.New(sp).CreateLiveQRDisplay(ctx, store.CreateLiveQRDisplayParams{
				TenantID:        c.TenantID,
				LocationID:      c.LocationID,
				PairingCodeHash: codeDigest,
				TtlSeconds:      int32(PairingLifetime / time.Second),
			})
			if err == nil {
				if err := sp.Commit(ctx); err != nil {
					return err
				}
				out = Created{ID: row.ID, Code: code, ExpiresAt: row.PairingExpiresAt}
				break
			}
			if rerr := sp.Rollback(ctx); rerr != nil {
				return rerr
			}
			switch pgCode(err) {
			case "23503": // the composite key to locations: not this business's venue
				return ErrUnknownLocation
			case "23505":
				if attempt < createAttempts {
					continue
				}
			}
			return fmt.Errorf("create screen: %w", err)
		}
		_, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionScreenCreated,
			Target:   out.ID.String(),
			Detail: createdDetail{
				LocationID:       c.LocationID.String(),
				PairingExpiresAt: out.ExpiresAt.UTC().Format(time.RFC3339),
			},
		})
		return err
	})
	if err != nil {
		if errors.Is(err, ErrUnknownLocation) {
			return Created{}, ErrUnknownLocation
		}
		return Created{}, fmt.Errorf("liveqr: create: %w", err)
	}
	return out, nil
}

// --- pairing (the screen) -----------------------------------------------------------------------

// Screen is a paired screen as the screen page knows it. No digest, no secret.
type Screen struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	LocationID     uuid.UUID
	LocationName   string
	LocationActive bool
	// Suspended is the operator's suspension of the business (ADR 0025 §1): its screens show no code
	// while it lasts and come back by themselves when it is lifted (the security lens's ORTA-2).
	Suspended bool
	// PairedAt is when the screen was paired; its session ends ScreenSessionLifetime later.
	PairedAt *time.Time
}

// Pair consumes a pairing code: the screen it names is paired with a fresh session, in ONE
// statement (store.ConsumeLiveQRDisplayPairing), and the pairing's audit row is written with it.
// The session is returned for the cookie and kept nowhere but its digest.
//
// A code that names no waiting screen is ErrCodeRefused with NO audit row: no tenant is known, and
// audit_log.tenant_id is NOT NULL (internal/audit's Event). A code that names a screen whose
// pairing cannot happen (expired; or raced by a revocation or a second pairing) is ErrCodeRefused
// WITH a row in that screen's business -- the attempt is the owner's to see -- carrying the reason
// and never the code.
func (s *Screens) Pair(ctx context.Context, code PairingCode) (Screen, ScreenSession, error) {
	codeDigest, err := s.keys.CodeDigest(code)
	if err != nil {
		return Screen{}, ScreenSession{}, ErrCodeRefused
	}
	waiting, err := s.data.GetLiveQRDisplayByPairingCodeHash(ctx, codeDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return Screen{}, ScreenSession{}, ErrCodeRefused
	}
	if err != nil {
		return Screen{}, ScreenSession{}, fmt.Errorf("liveqr: pair: %w", err)
	}
	sess, err := newScreenSession()
	if err != nil {
		return Screen{}, ScreenSession{}, err
	}
	sessDigest, err := s.keys.SessionDigest(sess)
	if err != nil {
		return Screen{}, ScreenSession{}, err
	}
	err = s.data.WithTenant(ctx, waiting.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).ConsumeLiveQRDisplayPairing(ctx, store.ConsumeLiveQRDisplayPairingParams{
			SessionHash:     sessDigest,
			TenantID:        waiting.TenantID,
			ID:              waiting.ID,
			PairingCodeHash: codeDigest,
		})
		if err != nil {
			return err
		}
		_, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: waiting.TenantID,
			Action:   ActionScreenPaired,
			Target:   row.ID.String(),
			Detail:   pairedDetail{LocationID: row.LocationID.String()},
		})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		reason := "raced"
		switch {
		case waiting.RevokedAt != nil:
			reason = "revoked"
		case !waiting.PairingExpiresAt.After(s.now()):
			reason = "expired"
		}
		// Its own transaction (audit.Record): the consumption matched nothing, so there is no
		// write for the row to share a fate with -- the refused attempt is the one to keep.
		rctx, cancel := audit.Detached(ctx)
		defer cancel()
		if _, rerr := s.trail.Record(rctx, audit.Event{
			TenantID: waiting.TenantID,
			Action:   ActionScreenPairingRefused,
			Target:   waiting.ID.String(),
			Detail:   refusedPairingDetail{Outcome: "refused", Reason: reason, LocationID: waiting.LocationID.String()},
		}); rerr != nil {
			return Screen{}, ScreenSession{}, fmt.Errorf("liveqr: pair: refused, and the trail failed: %w", rerr)
		}
		return Screen{}, ScreenSession{}, ErrCodeRefused
	}
	if err != nil {
		return Screen{}, ScreenSession{}, fmt.Errorf("liveqr: pair: %w", err)
	}
	return Screen{ID: waiting.ID, TenantID: waiting.TenantID, LocationID: waiting.LocationID}, sess, nil
}

// Resolve is the screen behind a session cookie, with its venue's name and state and the business's
// suspension. ErrNoScreen for a session that names nothing (or is not a session's shape); ErrRevoked
// for a revoked screen and ErrLapsed for one paired longer than ScreenSessionLifetime ago, each with
// the Screen it was. A live screen's last_seen_at is stamped in the same transaction, at most every
// ten minutes (TouchLiveQRDisplay) -- also while its venue is off or its business suspended: the
// panel shows the screen is still up.
func (s *Screens) Resolve(ctx context.Context, sess ScreenSession) (Screen, error) {
	sessDigest, err := s.keys.SessionDigest(sess)
	if err != nil {
		return Screen{}, ErrNoScreen
	}
	r, err := s.data.GetLiveQRDisplayBySessionHash(ctx, sessDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return Screen{}, ErrNoScreen
	}
	if err != nil {
		return Screen{}, fmt.Errorf("liveqr: resolve: %w", err)
	}
	out := Screen{ID: r.ID, TenantID: r.TenantID, LocationID: r.LocationID}
	if r.RevokedAt != nil {
		return out, ErrRevoked
	}
	var verdict error
	err = s.data.WithTenant(ctx, r.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.GetLiveQRDisplayForScreen(ctx, store.GetLiveQRDisplayForScreenParams{
			TenantID: r.TenantID, ID: r.ID,
		})
		if err != nil {
			return err
		}
		out.LocationName, out.LocationActive, out.Suspended = row.LocationName, row.LocationActive, row.TenantSuspended
		out.PairedAt = row.PairedAt
		switch {
		case row.RevokedAt != nil: // revoked between the two reads
			verdict = ErrRevoked
			return nil
		case row.PairedAt == nil: // a session names a paired row (00111); unreachable
			verdict = ErrNoScreen
			return nil
		case !s.now().Before(row.PairedAt.Add(ScreenSessionLifetime)):
			verdict = ErrLapsed
			return nil
		}
		_, err = q.TouchLiveQRDisplay(ctx, store.TouchLiveQRDisplayParams{TenantID: r.TenantID, ID: r.ID})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Screen{}, ErrNoScreen
	}
	if err != nil {
		return Screen{}, fmt.Errorf("liveqr: resolve: %w", err)
	}
	if verdict != nil {
		return out, verdict
	}
	return out, nil
}

// Token mints the QR's next token for a resolved screen, issued now by the server's clock.
//
// IT CARRIES ITS OWN GATE (defence in depth, the re-audit's DÜŞÜK): the page asks before it calls,
// but a later caller (M16-23's activation, say) may not -- so a suspended business's screen, a
// switched-off venue's, and one whose pairing is unknown or has run out get ErrNoCode, never a token.
func (s *Screens) Token(sc Screen) (string, time.Time, error) {
	at := s.now()
	if sc.Suspended || !sc.LocationActive || sc.PairedAt == nil || !at.Before(sc.PairedAt.Add(ScreenSessionLifetime)) {
		return "", time.Time{}, ErrNoCode
	}
	tok, err := s.keys.Mint(sc.ID, sc.LocationID, at, TokenLifetime)
	if err != nil {
		return "", time.Time{}, err
	}
	return tok, at.Add(TokenLifetime), nil
}

// --- the panel's list and revocation ------------------------------------------------------------

// The four states a listed screen is in. Words, not colours: the panel prints them.
const (
	StateWaiting = "waiting"
	StateExpired = "expired"
	StatePaired  = "paired"
	StateRevoked = "revoked"
	// StateLapsed is a paired screen older than ScreenSessionLifetime: it shows no code any more.
	StateLapsed = "lapsed"
)

// Listed is one screen of a venue as the panel lists it.
type Listed struct {
	ID               uuid.UUID
	State            string
	CreatedAt        time.Time
	PairingExpiresAt time.Time
	PairedAt         *time.Time
	RevokedAt        *time.Time
	// LastSeenAt is when a paired screen last asked for a code, to ten minutes (00114); nil before
	// its first poll.
	LastSeenAt *time.Time
}

// List is one venue's screens, newest first, with each one's state as of now.
func (s *Screens) List(ctx context.Context, tenantID, locationID uuid.UUID) ([]Listed, error) {
	var out []Listed
	now := s.now()
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListLiveQRDisplaysForLocation(ctx, store.ListLiveQRDisplaysForLocationParams{
			TenantID: tenantID, LocationID: locationID,
		})
		if err != nil {
			return err
		}
		out = make([]Listed, 0, len(rows))
		for _, r := range rows {
			out = append(out, Listed{
				ID: r.ID, State: stateOf(r.PairedAt, r.RevokedAt, r.PairingExpiresAt, now),
				CreatedAt: r.CreatedAt, PairingExpiresAt: r.PairingExpiresAt, PairedAt: r.PairedAt, RevokedAt: r.RevokedAt,
				LastSeenAt: r.LastSeenAt,
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("liveqr: list: %w", err)
	}
	return out, nil
}

func stateOf(paired, revoked *time.Time, expires, now time.Time) string {
	switch {
	case revoked != nil:
		return StateRevoked
	case paired != nil && !now.Before(paired.Add(ScreenSessionLifetime)):
		return StateLapsed
	case paired != nil:
		return StatePaired
	case expires.After(now):
		return StateWaiting
	default:
		return StateExpired
	}
}

// RevokeCommand is the panel's "Remove" on one screen.
type RevokeCommand struct {
	TenantID  uuid.UUID
	ActorID   uuid.UUID
	DisplayID uuid.UUID
}

// Revoked says which venue the screen stood at, and whether it was already removed (then nothing
// was written).
type Revoked struct {
	LocationID uuid.UUID
	Already    bool
}

// Revoke stamps the screen revoked (its page stops showing a code at its next refresh, and its
// last token is refused at the tap by the row -- M16-21) and writes the audit row in the same
// transaction. The row is locked first, so a repeat writes nothing and says Already.
func (s *Screens) Revoke(ctx context.Context, c RevokeCommand) (Revoked, error) {
	if c.TenantID == uuid.Nil || c.ActorID == uuid.Nil || c.DisplayID == uuid.Nil {
		return Revoked{}, errors.New("liveqr: revoke: a business, an admin and a screen are required")
	}
	var out Revoked
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.LockLiveQRDisplay(ctx, store.LockLiveQRDisplayParams{TenantID: c.TenantID, ID: c.DisplayID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownScreen
		}
		if err != nil {
			return err
		}
		out.LocationID = row.LocationID
		if row.RevokedAt != nil {
			out.Already = true
			return nil
		}
		was := "waiting"
		if row.PairedAt != nil {
			was = "paired"
		}
		if _, err := q.RevokeLiveQRDisplay(ctx, store.RevokeLiveQRDisplayParams{TenantID: c.TenantID, ID: c.DisplayID}); err != nil {
			return err
		}
		_, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionScreenRevoked,
			Target:   c.DisplayID.String(),
			Detail:   revokedDetail{LocationID: row.LocationID.String(), Was: was},
		})
		return err
	})
	if errors.Is(err, ErrUnknownScreen) {
		return Revoked{}, ErrUnknownScreen
	}
	if err != nil {
		return Revoked{}, fmt.Errorf("liveqr: revoke: %w", err)
	}
	return out, nil
}

// --- the audit details: typed, and no field a code, a session, a token or a key could travel in.

type createdDetail struct {
	LocationID       string `json:"location_id"`
	PairingExpiresAt string `json:"pairing_expires_at"`
}

type pairedDetail struct {
	LocationID string `json:"location_id"`
}

type refusedPairingDetail struct {
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason"`
	LocationID string `json:"location_id"`
}

type revokedDetail struct {
	LocationID string `json:"location_id"`
	Was        string `json:"was"`
}

func pgCode(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}
