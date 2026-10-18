package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/sun"
)

// THE VIRTUAL PLAQUE (ADR 0028 §11: "sanal plaket izin listesi"). Every venue of a sandbox business
// has at most one, and its UID is DERIVED from the venue's id: db.VirtualTagUIDPrefix followed by
// the venue id's first five bytes. Derived, so that the simulation can find a holding PEER's plaque
// at a peer venue without reading the peer's tags -- it computes the UID and asks the context-less
// resolver every tap asks (the phone in front of the plaque learns the UID the same way: the chip
// says it). A UID is public (ADR 0003 md. 1); knowing it forges nothing, the CMAC needs the key.
//
// A collision of five random bytes with another venue's plaque is an INSERT refused by the primary
// key, and the venue then has no plaque of its own: the resolver's answer names another business or
// another venue, and the simulation answers 404 rather than tap a plaque that is not the venue's.
//
// THE KEY is 16 bytes of crypto/rand, wrapped under the sandbox's own TAPPA_TAG_KEK by sun.Wrap
// before it touches the database, and zeroed as soon as it is wrapped (CLAUDE.md §4.7): the row
// holds the 44-byte envelope like every plaque's, and no plain key is ever stored, logged or
// returned. There is no application master key (key 0): a virtual plaque has no chip to rekey.
//
// PROVISIONING is the encode flow's three statements -- InsertUnassigned, MarkTagEncoded,
// AssignTagToLocation -- in ONE transaction in the key's business, each with the trail row the
// panel already reads (plaque.loaded, plaque.encoded, plaque.mounted), attributed to the key (K-23).
// It runs only at the key's OWN venue (sandbox.go, AUTHORITY), and it commits before the tap is
// recorded: the tap resolves the plaque on another connection.

// VirtualUID is the venue's virtual plaque UID.
func VirtualUID(venue uuid.UUID) string {
	return db.VirtualTagUIDPrefix + strings.ToUpper(hex.EncodeToString(venue[:5]))
}

// virtualKeyLen is the plaque key's size: AES-128, the chip's (sun's tagKeyLen).
const virtualKeyLen = 16

// The provisioning trail's details: the panel's own shapes (internal/encode's loadedDetail and
// encodedDetail, internal/domain/tenant's mountedDetail) with K-23's via beside them.
type (
	loadedDetail struct {
		apikey.Attribution
		ClaimedBy string `json:"claimed_by"`
		KeyBytes  int    `json:"key_bytes"`
	}
	encodedDetail struct {
		apikey.Attribution
		ClaimedBy string `json:"claimed_by"`
	}
	mountedDetail struct {
		apikey.Attribution
		Name       string `json:"name"`
		LocationID string `json:"location_id"`
		Replaces   string `json:"replaces"`
		FromStatus string `json:"from_status"`
	}
)

// claimedBy names the loader in the trail: the sandbox's minter, not a person.
const claimedBy = "sandbox"

// provision gives the key's own venue its virtual plaque, unless it has one. It returns whether it
// wrote one. A plaque already at the UID -- this venue's, from an earlier call, or a concurrent
// call's that committed first -- is not an error; one of another business (the collision above) is
// found out by plaqueAt, which holds the resolved row to the venue.
func (s *Simulation) provision(ctx context.Context, p apikey.Principal, venue uuid.UUID, venueName string) (bool, error) {
	uid := VirtualUID(venue)
	if !db.IsVirtualTagUID(uid) {
		return false, fmt.Errorf("sandbox: derived a UID outside the virtual range")
	}
	// The insert door, before anything (internal/db/virtualtags.go).
	if err := s.data.AdmitTagUID(uid); err != nil {
		return false, fmt.Errorf("sandbox: admit plaque: %w", err)
	}
	uidBytes, err := hex.DecodeString(uid)
	if err != nil {
		return false, fmt.Errorf("sandbox: plaque uid: %w", err)
	}
	created := false
	err = s.data.WithTenant(ctx, p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, err := q.GetTagForTenant(ctx, store.GetTagForTenantParams{TenantID: p.TenantID, Uid: uid}); err == nil {
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read the venue's plaque: %w", err)
		}
		wrapped, err := s.wrapFreshKey(uidBytes)
		if err != nil {
			return err
		}
		if _, err := q.InsertUnassigned(ctx, store.InsertUnassignedParams{
			Uid: uid, TenantID: p.TenantID, AesKeyRef: wrapped, AppKeyRef: nil,
		}); err != nil {
			return fmt.Errorf("insert the virtual plaque: %w", err)
		}
		if err := s.record(ctx, tx, p, tenant.ActionPlaqueLoaded, uid,
			loadedDetail{Attribution: p.Attribution(), ClaimedBy: claimedBy, KeyBytes: len(wrapped)}); err != nil {
			return err
		}
		if _, err := q.MarkTagEncoded(ctx, store.MarkTagEncodedParams{Uid: uid, TenantID: p.TenantID}); err != nil {
			return fmt.Errorf("mark the virtual plaque encoded: %w", err)
		}
		if err := s.record(ctx, tx, p, tenant.ActionPlaqueEncoded, uid,
			encodedDetail{Attribution: p.Attribution(), ClaimedBy: claimedBy}); err != nil {
			return err
		}
		if _, err := q.AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: venue, TenantID: p.TenantID, Uid: uid,
		}); err != nil {
			return fmt.Errorf("mount the virtual plaque: %w", err)
		}
		if err := s.record(ctx, tx, p, tenant.ActionPlaqueMounted, uid, mountedDetail{
			Attribution: p.Attribution(), Name: venueName, LocationID: venue.String(), FromStatus: tenant.PlaqueUnassigned,
		}); err != nil {
			return err
		}
		created = true
		return nil
	})
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return created, nil
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		// A concurrent call -- or a colliding plaque of another business -- holds the UID; plaqueAt
		// decides which.
		return false, nil
	default:
		return false, fmt.Errorf("sandbox: provision the virtual plaque: %w", err)
	}
}

func (s *Simulation) record(ctx context.Context, tx pgx.Tx, p apikey.Principal, action, uid string, detail any) error {
	if _, err := s.trail.RecordTx(ctx, tx, audit.Event{TenantID: p.TenantID, ActorID: p.Actor(), Action: action, Target: uid, Detail: detail}); err != nil {
		return fmt.Errorf("record %s: %w", action, err)
	}
	return nil
}

// wrapFreshKey draws a plaque key, wraps it under the sandbox's KEK and zeroes it.
func (s *Simulation) wrapFreshKey(uid []byte) ([]byte, error) {
	key := make([]byte, virtualKeyLen)
	defer sun.Zero(key)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("draw the plaque key: %w", err)
	}
	wrapped, err := sun.Wrap(s.kek, uid, key)
	if err != nil {
		return nil, fmt.Errorf("wrap the plaque key: %w", err)
	}
	return wrapped, nil
}

// plaqueAt resolves the venue's virtual plaque through the context-less resolver and holds it to
// the venue: the resolved row must be site's plaque on that venue's wall, or there is no plaque here
// (pgx.ErrNoRows -- the caller's 404). Any status is a plaque: a lost or retired one is tapped and
// refused by §5 row 1, in writing.
func (s *Simulation) plaqueAt(ctx context.Context, site, venue uuid.UUID) (db.ResolvedTag, error) {
	tag, err := s.data.GetTagByUID(ctx, VirtualUID(venue))
	if err != nil {
		return db.ResolvedTag{}, err
	}
	if tag.TenantID != site || tag.LocationID == nil || *tag.LocationID != venue {
		return db.ResolvedTag{}, pgx.ErrNoRows
	}
	return tag, nil
}

// tapPath is the URL a touch of the plaque would open: for nfc, the chip's next read -- last_ctr+1
// under the plaque's own key, minted by sun.MintTapPath; for qr, the printed code (no counter, no
// MAC). A plaque not in service is not unwrapped (internal/sun: "a plaque that is not in service
// must not touch cryptography"): its nfc URL carries the next counter and a zero MAC, which the
// pre-check never reads -- §5 row 1 refuses the tap first.
//
// THE COUNTER IS NOT JUDGED HERE: last_ctr+1 is only what the chip would say next. Whether it is
// fresh is the atomic advance's question, in SQL, inside Record (CLAUDE.md §4.4); a counter at the
// chip's 24-bit end fails to mint (sun.MintTapPath refuses it) or to parse, and the tap is an outage
// answer, never a recorded success.
func (s *Simulation) tapPath(tag db.ResolvedTag, nfc bool) (string, error) {
	q := url.Values{}
	q.Set("tag", tag.UID)
	if !nfc {
		return "/t?" + q.Encode(), nil
	}
	next := uint32(tag.LastCtr) + 1
	if tag.Status != tenant.PlaqueActive {
		q.Set("ctr", strings.ToUpper(hex.EncodeToString([]byte{byte(next >> 16), byte(next >> 8), byte(next)})))
		q.Set(sunMACParam, strings.Repeat("0", 16))
		return "/t?" + q.Encode(), nil
	}
	uid, err := hex.DecodeString(tag.UID)
	if err != nil {
		return "", fmt.Errorf("sandbox: plaque uid: %w", err)
	}
	key, err := sun.UnwrapAny(s.keks, uid, tag.AESKeyRef)
	if err != nil {
		return "", fmt.Errorf("sandbox: open the plaque key: %w", err)
	}
	defer sun.Zero(key)
	return sun.MintTapPath(key, uid, next)
}

// sunMACParam is the SUN URL's MAC parameter (internal/sun's paramCMAC).
const sunMACParam = "cmac"
