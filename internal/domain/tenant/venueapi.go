package tenant

// venueapi.go -- the integration API's venue write (M11 API-5, ADR 0028 §9:
// PUT /v1/locations/{ref}). internal/handler/api is the one caller; it runs this inside the API's
// write runner (internal/apikey.Writes), whose transaction is in the KEY's tenant context -- the
// tenant a request acts for is never one the request names (ADR 0028 §2).
//
// WHAT THE API MAY SET, AND WHAT IT MAY NOT. A venue the integrator PUTs carries a name, its
// external ref (the path) and K-20's `active` label -- and nothing of the evidence a tap is
// judged on. static_ips, the coordinate, the shift and the network name are the panel's
// (SaveVenue): ADR 0028 §9 and plan §2 item 10 say a venue the API opens sends its taps to the
// approval queue until the panel gives it an address range or a coordinate. Concretely: such a
// venue has neither half of CLAUDE.md §5 row 6, so every tap there reaches row 7 and is
// RECORDED as `flag` (§4.6 -- recorded, never refused); TestLocationsPut_AVenueWithoutEvidenceRecordsATapAsFlag
// measures it through the real tap path. For the same reason §5's netx width rule has nothing
// to apply to on this path: no address range enters through it.
//
// THE SAME RULES AS THE PANEL. The name goes through venueName and refuseHiddenName (SaveVenue's:
// CleanName, 1..MaxVenueNameRunes runes -- refused, never cut -- and the character rule for a new
// or changed name) and the ref
// through the pattern ExternalRef checks (migration 00037's CHECK). Unlike the panel's field, the
// ref is NOT trimmed here: it is a URL path segment, and a ref the integrator did not send
// exactly is a ref it does not hold.
//
// K-7: there is no per-venue time zone. A PUT may name one; it must then be the business's own
// (tenants.timezone) or the PUT is refused -- nothing is stored either way, so the check reads
// the tenant row without a lock (a concurrent change of the business's zone cannot leave a venue
// "in" the old one: venues have no zone).
//
// K-18: a venue is found by its ref ONLY, never matched by name. An unknown ref opens a NEW
// venue; the owner binds an existing venue to its ref in the panel (Venues.ChangeExternalRef)
// before the first sync, and the PUT then updates THAT row.

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// VenuePut is one PUT /v1/locations/{ref}, as the handler validated it at its boundary.
type VenuePut struct {
	// TenantID is the API key's tenant -- the ONE authority (ADR 0028 §2).
	TenantID uuid.UUID
	// ActorID is the API key's id: K-23, actor_id = api_keys.id.
	ActorID uuid.UUID
	// Via is detail.via of the trail row -- K-23's "api" (apikey.Via). Required: a trail row
	// that does not say where the act came from is K-23's gap.
	Via string
	// ExternalRef is the path's ref, exactly as sent.
	ExternalRef string
	Name        string
	// Timezone is the zone the request named, "" when it named none.
	Timezone string
	// Active is K-20's label.
	Active bool
}

// APIVenue is a venue as the API answers with it.
type APIVenue struct {
	ID          uuid.UUID
	ExternalRef string
	Name        string
	Active      bool
	// Timezone is the business's zone (K-7: a venue has none of its own).
	Timezone  string
	CreatedAt time.Time
}

// VenuePutResult says what the PUT did.
type VenuePutResult struct {
	Venue APIVenue
	// Created: the ref was new and this PUT opened the venue (201).
	Created bool
	// Changed: an existing venue's name or label was rewritten (200). Neither Created nor
	// Changed is a PUT that matched the venue as it was: nothing written, no trail row (200).
	Changed bool
}

// ErrVenueTimezone is the refusal of K-7: the PUT named a zone that is not the business's.
// TimezoneMismatch carries the business's zone; errors.Is(err, ErrVenueTimezone) holds for it.
var ErrVenueTimezone = errors.New("tenant: a venue's time zone is its business's")

// TimezoneMismatch is ErrVenueTimezone with the business's own zone, which the API names in its
// answer -- the integrator's own setting, not anything of the request.
type TimezoneMismatch struct{ BusinessZone string }

func (e *TimezoneMismatch) Error() string { return ErrVenueTimezone.Error() }

// Is makes errors.Is(err, ErrVenueTimezone) true.
func (e *TimezoneMismatch) Is(target error) bool { return target == ErrVenueTimezone }

// apiVenueDetail is the trail row's payload of an API venue write (K-23). A purpose-built struct
// with known keys (internal/audit's rule): the ref, the name and the label before and after, and
// whether the venue has proof of place after the act -- false for every venue the API opens,
// and the fact an investigator of "why does every tap there flag" needs. No key, no header, no
// request body.
type apiVenueDetail struct {
	Via         string `json:"via"`
	ExternalRef string `json:"external_ref"`

	FromName string `json:"from_name,omitempty"`
	ToName   string `json:"to_name"`

	FromActive *bool `json:"from_active,omitempty"`
	ToActive   bool  `json:"to_active"`

	// LostProofOfPlace is venueDetail's derived field with venueDetail's meaning: after the act
	// the venue has neither static IPs nor a coordinate.
	LostProofOfPlace bool `json:"lost_proof_of_place"`
}

// apiVenueRow is the row shape the four API statements share (sqlc emits one struct per query;
// their fields are identical, so each converts).
type apiVenueRow struct {
	ID          uuid.UUID
	Name        string
	ExternalRef *string
	Active      bool
	CreatedAt   time.Time
	StaticIps   []netip.Prefix
	GpsLat      pgtype.Numeric
	GpsLng      pgtype.Numeric
}

func (r apiVenueRow) venue(zone string) APIVenue {
	return APIVenue{ID: r.ID, ExternalRef: deref(r.ExternalRef), Name: r.Name, Active: r.Active, Timezone: zone, CreatedAt: r.CreatedAt}
}

func (r apiVenueRow) hasProofOfPlace() bool {
	return len(r.StaticIps) > 0 || gpsOf(r.GpsLat, r.GpsLng) != nil
}

// apiExternalRef checks a path's ref: the schema's pattern, exactly as sent.
func apiExternalRef(raw string) (string, error) {
	ref, err := ExternalRef(raw)
	if err != nil || ref == nil || *ref != raw {
		return "", ErrExternalRef
	}
	return raw, nil
}

// ValidAPIExternalRef reports whether raw is a ref the API can carry, exactly as sent -- the
// handler's boundary check (the domain checks again).
func ValidAPIExternalRef(raw string) bool {
	_, err := apiExternalRef(raw)
	return err == nil
}

// PutVenueTx is PUT /v1/locations/{ref} inside tx (the API runner's transaction, in c.TenantID's
// context): open the venue for a new ref (Created), rewrite an existing one's name and label
// (Changed), or answer it as it is. The trail row (K-23) is written in the same transaction --
// SaveVenue's rule: the act and its record are one.
//
// The refusals: ErrVenueName, ErrExternalRef, a *TimezoneMismatch (ErrVenueTimezone).
func (v *Venues) PutVenueTx(ctx context.Context, tx pgx.Tx, c VenuePut) (VenuePutResult, error) {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return VenuePutResult{}, err
	}
	if c.Via == "" {
		return VenuePutResult{}, errors.New("tenant: an API act needs its detail.via (K-23)")
	}
	if tx == nil {
		return VenuePutResult{}, errors.New("tenant: nil transaction")
	}
	ref, err := apiExternalRef(c.ExternalRef)
	if err != nil {
		return VenuePutResult{}, err
	}
	name, err := venueName(c.Name, ErrVenueName)
	if err != nil {
		return VenuePutResult{}, err
	}
	q := store.New(tx)
	clock, err := q.GetTenantClock(ctx, c.TenantID)
	if err != nil {
		return VenuePutResult{}, fmt.Errorf("tenant: put venue: read the business's zone: %w", err)
	}
	if c.Timezone != "" && c.Timezone != clock.Timezone {
		return VenuePutResult{}, &TimezoneMismatch{BusinessZone: clock.Timezone}
	}

	before, found, err := lockAPIVenue(ctx, q, c.TenantID, ref)
	if err != nil {
		return VenuePutResult{}, err
	}
	if !found {
		if err := refuseHiddenName(name, "", ErrVenueName); err != nil {
			return VenuePutResult{}, err
		}
		created, err := q.CreateLocationFromAPI(ctx, store.CreateLocationFromAPIParams{
			Name: name, ExternalRef: &ref, Active: c.Active, TenantID: c.TenantID,
		})
		switch {
		case err == nil:
			row := apiVenueRow(created)
			if err := v.recordAPIVenue(ctx, tx, c, ActionLocationCreated, row, apiVenueDetail{
				Via: c.Via, ExternalRef: ref, ToName: row.Name, ToActive: row.Active, LostProofOfPlace: !row.hasProofOfPlace(),
			}); err != nil {
				return VenuePutResult{}, err
			}
			return VenuePutResult{Venue: row.venue(clock.Timezone), Created: true}, nil
		case errors.Is(err, pgx.ErrNoRows):
			// ON CONFLICT DO NOTHING: a concurrent PUT of the same ref committed the venue while
			// this one waited on its index entry. The row exists now -- lock it and carry on as
			// an update (a repeated PUT opens no row).
			before, found, err = lockAPIVenue(ctx, q, c.TenantID, ref)
			if err != nil {
				return VenuePutResult{}, err
			}
			if !found {
				// No row to lock and none to insert: the tenant row itself is not visible to
				// this transaction (CreateLocation's comment) -- a wiring failure, never input.
				return VenuePutResult{}, errors.New("tenant: put venue: neither a venue for the ref nor the tenant row is visible")
			}
		default:
			if constraintOf(err) == "locations_external_ref_shape" {
				return VenuePutResult{}, ErrExternalRef
			}
			return VenuePutResult{}, fmt.Errorf("tenant: put venue: create: %w", err)
		}
	}

	// The name rule judges what this PUT introduces: the name on file is never refused (a venue
	// named before the rule, or in the panel, stays writable by its integrator).
	if err := refuseHiddenName(name, before.Name, ErrVenueName); err != nil {
		return VenuePutResult{}, err
	}
	if before.Name == name && before.Active == c.Active {
		return VenuePutResult{Venue: before.venue(clock.Timezone)}, nil
	}
	updated, err := q.UpdateLocationFromAPI(ctx, store.UpdateLocationFromAPIParams{
		Name: name, Active: c.Active, TenantID: c.TenantID, ID: before.ID,
	})
	if err != nil {
		return VenuePutResult{}, fmt.Errorf("tenant: put venue: update: %w", err)
	}
	row := apiVenueRow(updated)
	fromActive := before.Active
	if err := v.recordAPIVenue(ctx, tx, c, ActionLocationUpdated, row, apiVenueDetail{
		Via: c.Via, ExternalRef: ref, FromName: before.Name, ToName: row.Name,
		FromActive: &fromActive, ToActive: row.Active, LostProofOfPlace: !row.hasProofOfPlace(),
	}); err != nil {
		return VenuePutResult{}, err
	}
	return VenuePutResult{Venue: row.venue(clock.Timezone), Changed: true}, nil
}

// lockAPIVenue is LockLocationByExternalRef: found=false for no row.
func lockAPIVenue(ctx context.Context, q *store.Queries, tenantID uuid.UUID, ref string) (apiVenueRow, bool, error) {
	r, err := q.LockLocationByExternalRef(ctx, store.LockLocationByExternalRefParams{TenantID: tenantID, ExternalRef: &ref})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return apiVenueRow{}, false, nil
	case err != nil:
		return apiVenueRow{}, false, fmt.Errorf("tenant: put venue: lock by ref: %w", err)
	}
	return apiVenueRow(r), true, nil
}

func (v *Venues) recordAPIVenue(ctx context.Context, tx pgx.Tx, c VenuePut, action string, row apiVenueRow, d apiVenueDetail) error {
	actor := c.ActorID
	if _, err := v.trail.RecordTx(ctx, tx, audit.Event{
		TenantID: c.TenantID, ActorID: &actor, Action: action, Target: row.ID.String(), Detail: d,
	}); err != nil {
		return fmt.Errorf("tenant: put venue: trail: %w", err)
	}
	return nil
}

// APIVenue reads one venue of the business as the API answers with it -- an Idempotency-Key
// replay's answer, which is the venue as it is NOW (the ledger keeps no body). ErrUnknownVenue
// when it is not this business's (or no longer exists).
func (v *Venues) APIVenue(ctx context.Context, tenantID, venueID uuid.UUID) (APIVenue, error) {
	if tenantID == uuid.Nil {
		return APIVenue{}, errors.New("tenant: tenant id is required")
	}
	var out APIVenue
	err := v.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		r, err := q.GetLocationForAPI(ctx, store.GetLocationForAPIParams{TenantID: tenantID, ID: venueID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownVenue
		}
		if err != nil {
			return fmt.Errorf("read venue: %w", err)
		}
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("read the business's zone: %w", err)
		}
		out = apiVenueRow(r).venue(clock.Timezone)
		return nil
	})
	if err != nil {
		return APIVenue{}, wrapVenue("api venue", err)
	}
	return out, nil
}
