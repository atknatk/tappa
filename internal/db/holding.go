package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// holding.go -- the Go side of ADR 0027 §2's callable holding functions: the tap path's three
// (M11 API-1b, migration 00043), holding_tap_site, holding_advance_tag_counter and holding_logo,
// the employer's readers' one (M11 API-1c, migration 00047), holding_site_locations, and the
// integration API's directory (M11 API-8, migration 00055), holding_directory, and the panel's Group
// section's (GRP-1, migration 00062), holding_group.
//
// HAND-WRITTEN AND TYPED, for resolve.go's reason: sqlc v1.28 cannot type a call to a
// `RETURNS TABLE(...)` function (ADR 0002 md.7, measured three times). The SQL strings below
// name each function's result columns exactly as the signature table writes them, and
// TestHoldingCatalog_TheFunctionsAndTheirExactSignatures pins that table in the catalogue.
//
// 🔴 THEY RUN ON THE CALLER'S TRANSACTION, WHICH IS THE SESSION TENANT'S. Each function takes
// the caller's tenant from app.tenant_id -- the WithTenant the caller is inside -- and never as an
// argument; the only argument is the key the server resolved (a plaque's uid, a counter, a logo
// digest). There is no WithTenant(site) anywhere on this path (ADR 0016 §2 holds): a peer's row
// is reached through these functions and through nothing else. A non-peer, an unknown key and a
// missing context are one answer.

// HoldingTx is the slice of a transaction the holding calls need: one statement at a time. A
// pgx.Tx is one.
type HoldingTx interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Holding is the three tap functions on one transaction.
type Holding struct{ tx HoldingTx }

// HoldingOn wraps the caller's transaction (the session tenant's).
func HoldingOn(tx HoldingTx) Holding { return Holding{tx: tx} }

// HoldingSite is holding_tap_site's row: a peer's plaque, its venue and the site's brand meta.
//
// 🔴 K-39: the venue's address ranges and its coordinate are SERVER-SIDE DECISION INPUTS ONLY.
// They feed tap.Decide's IP and GPS facts and are shown on no screen, written to no log and
// carried in no response (ADR 0027 §2, K-39). So they are not fields: they sit inside an
// unexported CLOSURE, read through StaticIPs and GPS. Every way out redacts them, each for its
// own reason (the security review's DÜŞÜK-2 measured the JSON ones leaking when they were fields):
//
//	slog attribute      LogValue: the site tenant and the plaque's status.
//	fmt, any verb       Format: the same two -- an error's %v and a test failure's %+v included,
//	                    and the struct nested in an exported field, a slice or a map.
//	encoding/json       MarshalJSON: the same two -- json.Marshal, and slog's JSONHandler, which
//	                    marshals a nested struct with encoding/json and never asks LogValue.
//	reflection          a HoldingSite reached through an UNEXPORTED field cannot have its
//	                    methods called, and fmt prints such a struct field by field -- a
//	                    pointer it FOLLOWS under a malformed verb (%s, %q: measured, the
//	                    address's integer in the output). A func is the one value reflection
//	                    cannot open: fmt prints it as an address under every verb.
type HoldingSite struct {
	// SiteTenantID is the plaque's tenant -- the site.
	SiteTenantID uuid.UUID
	// TagStatus is the plaque's status as this read saw it.
	TagStatus string
	// LocationID is the plaque's wall, nil for a plaque on none (unassigned): the venue fields
	// below are then zero.
	LocationID          *uuid.UUID
	LocationName        string
	LocationExternalRef *string
	// evidence is K-39's, nil when the plaque is on no wall.
	evidence   func() holdingEvidence
	ShiftStart pgtype.Time
	ShiftEnd   pgtype.Time
	Overnight  bool
	// Timezone is the site tenant's zone (K-36: a holding's members share one).
	Timezone string
	APICode  *string
	// Accent is the site's stored accent (six upper-case hex digits), or nil.
	Accent *string
	// The logo half: present only for a business VIES verified (ADR 0027 §10) -- all five set
	// or all five nil; BusinessName is the logo's alt.
	LogoSHA256   *string
	LogoMIME     *string
	LogoWidth    *int32
	LogoHeight   *int32
	BusinessName *string
}

// holdingEvidence is the venue's place evidence (K-39), inside HoldingSite's unexported closure.
type holdingEvidence struct {
	staticIPs      []netip.Prefix
	gpsLat, gpsLng pgtype.Numeric
}

// StaticIPs is the venue's address ranges -- for tap.Decide's IP fact, and nothing else.
func (s HoldingSite) StaticIPs() []netip.Prefix {
	if s.evidence == nil {
		return nil
	}
	return s.evidence().staticIPs
}

// GPS is the venue's coordinate (invalid numerics when it has none) -- for tap.Decide's GPS
// fact, and nothing else.
func (s HoldingSite) GPS() (lat, lng pgtype.Numeric) {
	if s.evidence == nil {
		return pgtype.Numeric{}, pgtype.Numeric{}
	}
	ev := s.evidence()
	return ev.gpsLat, ev.gpsLng
}

// LogValue keeps K-39's evidence out of every log line: the site and the plaque's status only.
func (s HoldingSite) LogValue() slog.Value {
	return slog.GroupValue(slog.String("site_tenant_id", s.SiteTenantID.String()), slog.String("tag_status", s.TagStatus))
}

// Format is LogValue's rule for fmt: every verb prints the site and the status, never the
// evidence -- a %v in an error or a %+v in a test failure included.
func (s HoldingSite) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "HoldingSite{site_tenant_id:%s tag_status:%s}", s.SiteTenantID, s.TagStatus)
}

// MarshalJSON is LogValue's rule for encoding/json: the site and the status, never the evidence
// -- json.Marshal's, and slog's JSONHandler's for a HoldingSite nested in another value.
func (s HoldingSite) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		SiteTenantID uuid.UUID `json:"site_tenant_id"`
		TagStatus    string    `json:"tag_status"`
	}{s.SiteTenantID, s.TagStatus})
}

// holdingTapSiteSQL names holding_tap_site's nineteen result columns in the signature table's
// order (migration 00043).
const holdingTapSiteSQL = `SELECT site_tenant_id, tag_status, location_id, location_name, location_external_ref,
       static_ips, gps_lat, gps_lng, shift_start, shift_end, overnight, timezone, api_code, accent,
       logo_sha256, logo_mime, logo_width, logo_height, business_name
  FROM public.holding_tap_site($1)`

// TapSite reads uid's plaque when its tenant is a peer of the caller's: (site, true, nil). Not a
// peer, an unknown uid and no tenant context are (zero, false, nil) -- one answer.
func (h Holding) TapSite(ctx context.Context, uid string) (HoldingSite, bool, error) {
	var (
		s        HoldingSite
		ev       holdingEvidence
		name     *string
		overnite *bool
		zone     *string
	)
	err := h.tx.QueryRow(ctx, holdingTapSiteSQL, uid).Scan(
		&s.SiteTenantID, &s.TagStatus, &s.LocationID, &name, &s.LocationExternalRef,
		&ev.staticIPs, &ev.gpsLat, &ev.gpsLng, &s.ShiftStart, &s.ShiftEnd, &overnite, &zone, &s.APICode, &s.Accent,
		&s.LogoSHA256, &s.LogoMIME, &s.LogoWidth, &s.LogoHeight, &s.BusinessName)
	if errors.Is(err, pgx.ErrNoRows) {
		return HoldingSite{}, false, nil
	}
	if err != nil {
		return HoldingSite{}, false, fmt.Errorf("db: holding_tap_site: %w", err)
	}
	s.evidence = func() holdingEvidence { return ev }
	if name != nil {
		s.LocationName = *name
	}
	if overnite != nil {
		s.Overnight = *overnite
	}
	if zone != nil {
		s.Timezone = *zone
	}
	return s, true, nil
}

// HoldingAdvanceOutcome is holding_advance_tag_counter's verdict on one counter (ADR 0027 §2, §5,
// K-29) -- a closed set of four.
type HoldingAdvanceOutcome string

const (
	// HoldingAdvanced: a peer's active plaque, and last_ctr moved to the presented counter.
	HoldingAdvanced HoldingAdvanceOutcome = "advanced"
	// HoldingNotPeer: not a peer (or no such plaque, or no tenant context); nothing moved.
	HoldingNotPeer HoldingAdvanceOutcome = "not_peer"
	// HoldingReplay: a peer's active plaque whose counter did not go up; nothing moved.
	HoldingReplay HoldingAdvanceOutcome = "replay"
	// HoldingPeerInactive: a peer's plaque that is not active at the lock; nothing moved.
	HoldingPeerInactive HoldingAdvanceOutcome = "peer_inactive"
)

// HoldingAdvance is holding_advance_tag_counter's row: the outcome, the plaque's status AT
// THE LOCK ("" for not_peer) and, for an advance, the reads skipped since the last one.
type HoldingAdvance struct {
	Outcome   HoldingAdvanceOutcome
	TagStatus string
	// CtrGap is the reads the plaque made that never reached the server (p_ctr - old - 1),
	// computed in the advancing statement exactly as store.AdvanceTagCounter computes it for
	// the session's own plaque (migration 00045; the user's decision K-1i). 0 for every
	// outcome but HoldingAdvanced, where the function answers NULL.
	CtrGap int32
}

const holdingAdvanceSQL = `SELECT outcome, tag_status, ctr_gap FROM public.holding_advance_tag_counter($1, $2)`

// HoldingAdvanceTagCounter runs §4.4's replay statement on a peer's plaque. A replay is an
// OUTCOME, not an error; an error means the database could not say whether the counter moved.
func (h Holding) HoldingAdvanceTagCounter(ctx context.Context, uid string, ctr uint32) (HoldingAdvance, error) {
	var (
		outcome string
		status  *string
		gap     *int32
	)
	if err := h.tx.QueryRow(ctx, holdingAdvanceSQL, uid, int32(ctr)).Scan(&outcome, &status, &gap); err != nil { //nolint:gosec // a 24-bit chip counter (sun.Params), always inside int32
		return HoldingAdvance{}, fmt.Errorf("db: holding_advance_tag_counter: %w", err)
	}
	out := HoldingAdvance{Outcome: HoldingAdvanceOutcome(outcome)}
	switch out.Outcome {
	case HoldingAdvanced, HoldingNotPeer, HoldingReplay, HoldingPeerInactive:
	default:
		return HoldingAdvance{}, fmt.Errorf("db: holding_advance_tag_counter: unknown outcome %q", outcome)
	}
	if status != nil {
		out.TagStatus = *status
	}
	if (gap != nil) != (out.Outcome == HoldingAdvanced) {
		// The function sets the gap exactly when it advanced; anything else is not 00045's.
		return HoldingAdvance{}, fmt.Errorf("db: holding_advance_tag_counter: outcome %s with a gap present=%v", outcome, gap != nil)
	}
	if gap != nil {
		out.CtrGap = *gap
	}
	return out, nil
}

// HoldingLogo is holding_logo's row.
type HoldingLogo struct {
	Data []byte
	MIME string
}

const holdingLogoSQL = `SELECT logo, logo_mime FROM public.holding_logo($1, $2)`

// Logo reads the CURRENT logo of uid's business when it is a peer of the caller's, VIES verified
// and holding exactly digest: (logo, true, nil). Every other case is (zero, false, nil).
func (h Holding) Logo(ctx context.Context, uid, digest string) (HoldingLogo, bool, error) {
	var (
		l    HoldingLogo
		mime *string
	)
	err := h.tx.QueryRow(ctx, holdingLogoSQL, uid, digest).Scan(&l.Data, &mime)
	if errors.Is(err, pgx.ErrNoRows) {
		return HoldingLogo{}, false, nil
	}
	if err != nil {
		return HoldingLogo{}, false, fmt.Errorf("db: holding_logo: %w", err)
	}
	if mime != nil {
		l.MIME = *mime
	}
	return l, true, nil
}

// HoldingSiteLocation is holding_site_locations' row: one venue of a peer of the caller's tenant
// (ADR 0027 §9) -- the name the employer's readers print for a row taken there, its shift, and
// the tenant a manual record at it names. 🔴 No evidence: the function returns no address range
// and no coordinate (K-39), so there is nothing to keep out of a log here.
type HoldingSiteLocation struct {
	SiteTenantID uuid.UUID
	LocationID   uuid.UUID
	Name         string
	ExternalRef  *string
	ShiftStart   pgtype.Time
	ShiftEnd     pgtype.Time
	Overnight    bool
}

// holdingSiteLocationsPage is the function's page size (ADR 0027 §2: "en çok 1000").
const holdingSiteLocationsPage = 1000

// HoldingSiteLocationsMaxPages bounds how many pages SiteLocations reads -- 20 000 venues across a
// tenant's peers, far past any holding this product serves (v1's is two tenants, nine venues). A
// read that reaches it says so (ErrHoldingSiteLocationsTruncated) rather than naming some rows
// and not others without a word.
const HoldingSiteLocationsMaxPages = 20

// ErrHoldingSiteLocationsTruncated: the peers hold more venues than SiteLocations reads.
var ErrHoldingSiteLocationsTruncated = errors.New("db: holding_site_locations: more venues than one read takes")

const holdingSiteLocationsSQL = `SELECT site_tenant_id, location_id, location_name, location_external_ref, shift_start, shift_end, overnight
  FROM public.holding_site_locations($1)`

// SiteLocations reads every venue of every peer of the caller's tenant, page by page, keyed by
// the venue's id. No peer, no context and a membership past its end are one answer: an empty map
// (the function's 0 rows -- ADR 0027 §6, the reads are strict).
func (h Holding) SiteLocations(ctx context.Context) (map[uuid.UUID]HoldingSiteLocation, error) {
	out := map[uuid.UUID]HoldingSiteLocation{}
	var cursor *uuid.UUID
	for range HoldingSiteLocationsMaxPages {
		rows, err := h.tx.Query(ctx, holdingSiteLocationsSQL, cursor)
		if err != nil {
			return nil, fmt.Errorf("db: holding_site_locations: %w", err)
		}
		n := 0
		for rows.Next() {
			var (
				l        HoldingSiteLocation
				name     *string
				overnite *bool
			)
			if err := rows.Scan(&l.SiteTenantID, &l.LocationID, &name, &l.ExternalRef, &l.ShiftStart, &l.ShiftEnd, &overnite); err != nil {
				rows.Close()
				return nil, fmt.Errorf("db: holding_site_locations: %w", err)
			}
			if name != nil {
				l.Name = *name
			}
			if overnite != nil {
				l.Overnight = *overnite
			}
			out[l.LocationID] = l
			id := l.LocationID
			cursor = &id
			n++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("db: holding_site_locations: %w", err)
		}
		if n < holdingSiteLocationsPage {
			return out, nil
		}
	}
	return nil, ErrHoldingSiteLocationsTruncated
}

// HoldingDirectoryEntry is holding_directory's row: one employee of a PEER of the caller's tenant
// (ADR 0027 §2; ADR 0028 §2, §9) -- never one of the caller's own. 🔴 No e-mail: the function
// returns none (K-1f, K-24; employees.email is on K-34's "never SELECT" list).
type HoldingDirectoryEntry struct {
	EmployeeID uuid.UUID
	// EmployerTenantID is the peer the employee works for, and APICode its company code (nil when
	// the peer's API was never given one -- K-22).
	EmployerTenantID uuid.UUID
	APICode          *string
	ExternalRef      *string
	EmployeeCode     *string
	FirstName        *string
	LastName         *string
	// FullName is the name the panel shows -- the one name a person the panel created has (the user's
	// decision, 2026-10-15).
	FullName string
	// Active is status <> 'deactivated' (K-1h).
	Active    bool
	UpdatedAt time.Time
}

// HoldingDirectoryPage is the function's page size (ADR 0027 §2: "en çok 1000"). A page this full
// may have more behind it; a shorter one is the end.
const HoldingDirectoryPage = 1000

const holdingDirectorySQL = `SELECT employee_id, employer_tenant_id, api_code, external_ref, employee_code, first_name, last_name, full_name, active, updated_at
  FROM public.holding_directory($1, $2)`

// Directory reads one page of the peers' employees in (updated_at, id) order: with cursor nil, those
// updated at or after since (nil: all of them); with a cursor, those after the position (since,
// cursor) -- the last row a previous page handed out. No peer, no context and a membership past its
// end are one answer: no row (ADR 0027 §6, the reads are strict).
func (h Holding) Directory(ctx context.Context, since *time.Time, cursor *uuid.UUID) ([]HoldingDirectoryEntry, error) {
	rows, err := h.tx.Query(ctx, holdingDirectorySQL, since, cursor)
	if err != nil {
		return nil, fmt.Errorf("db: holding_directory: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (HoldingDirectoryEntry, error) {
		var e HoldingDirectoryEntry
		var full *string
		err := r.Scan(&e.EmployeeID, &e.EmployerTenantID, &e.APICode, &e.ExternalRef, &e.EmployeeCode, &e.FirstName, &e.LastName,
			&full, &e.Active, &e.UpdatedAt)
		if full != nil {
			e.FullName = *full
		}
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("db: holding_directory: %w", err)
	}
	return out, nil
}

// HoldingGroupMember is holding_group's row: one LIVE member of a holding the caller is a live member
// of (ADR 0027 §2, its GRP-1 note) -- the caller's own row included (IsSelf). 🔴 A name, a company code
// and a date, nothing else of a member (K-1d): the function returns no VAT number, address, e-mail,
// headcount, plan or API switch, and no member that has left.
type HoldingGroupMember struct {
	// HoldingID keys the holding; the panel groups by it and prints none of it.
	HoldingID   uuid.UUID
	HoldingName string
	MemberName  string
	// APICode is the member's company code, nil when it never had one (K-22).
	APICode  *string
	JoinedAt time.Time
	IsSelf   bool
}

// HoldingGroupMax is the function's LIMIT (ADR 0027 §2: "en çok 1000") -- a holding is a handful of
// businesses; a read that reaches it says so (ErrHoldingGroupTruncated) rather than list some members.
const HoldingGroupMax = 1000

// ErrHoldingGroupTruncated: the caller's holding holds more members than one read takes.
var ErrHoldingGroupTruncated = errors.New("db: holding_group: more members than one read takes")

const holdingGroupSQL = `SELECT holding_id, holding_name, member_name, member_api_code, joined_at, is_self
  FROM public.holding_group()`

// Group reads the caller's live holding(s) and their live members, the caller's own row first in each
// holding. A business in no holding, a membership that has ended and no context are one answer: no row
// (ADR 0027 §6, the reads are strict).
func (h Holding) Group(ctx context.Context) ([]HoldingGroupMember, error) {
	rows, err := h.tx.Query(ctx, holdingGroupSQL)
	if err != nil {
		return nil, fmt.Errorf("db: holding_group: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (HoldingGroupMember, error) {
		var m HoldingGroupMember
		var hname, mname *string
		err := r.Scan(&m.HoldingID, &hname, &mname, &m.APICode, &m.JoinedAt, &m.IsSelf)
		if hname != nil {
			m.HoldingName = *hname
		}
		if mname != nil {
			m.MemberName = *mname
		}
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("db: holding_group: %w", err)
	}
	if len(out) >= HoldingGroupMax {
		return nil, ErrHoldingGroupTruncated
	}
	return out, nil
}
