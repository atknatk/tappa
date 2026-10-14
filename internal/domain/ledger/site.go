package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// site.go -- M11 API-1c: the EMPLOYER's readers name a row taken at a PEER's venue (ADR 0027 §9,
// "İşverenin okuyucuları").
//
// A tap a holding member's employee makes on another member's plaque is the EMPLOYER's row: its
// location_id and tag_uid are NULL and site_location_id / site_tag_uid name the peer's venue and
// plaque (K-30). This tenant's locations do not hold the peer's venue, so the ordinary joins find
// no name and no shift for it -- before API-1c those rows fell into a nameless bucket. The one way
// into a peer's venue list is holding_site_locations (the closed list, K-33); siteVenuesFor reads it
// ONCE per transaction, and only when the rows on hand name a site, so a business with no holding
// row runs exactly the statements it ran before.
//
// 🔴 K-39 -- THE PEER'S EVIDENCE IS NOT THIS PANEL'S TO DESCRIBE. The employer's row carries its
// own ip_match / gps_match / trust, computed against the PEER's address ranges and coordinate
// (holding_tap_site, server-side only). Printing them would tell the employer, tap by tap, whether
// the phone stood on the peer's network -- ADR 0027 §13 item 5 names that as the residue the panel
// does NOT show. nameSiteRecords blanks them -- and replaces the engine's note, which says the same
// thing in words (PartnerVenueNote) -- on every record that names a site, and the docket says why
// instead of printing "n/a" (which would claim nothing was assessed).

// PartnerVenueUnknown is the name of a peer's venue the employer can no longer read: the
// membership has ended (holding_site_locations is strict -- ADR 0027 §6), so the row's venue id
// has no name on this side any more. The record stays listed (§4.6).
const PartnerVenueUnknown = "Another business's venue"

// siteVenues is a peer's venues by id, read once.
type siteVenues map[uuid.UUID]db.HoldingSiteLocation

// siteVenuesFor reads the peers' venues when need is true, else answers an empty set without a
// statement.
func siteVenuesFor(ctx context.Context, tx pgx.Tx, need bool) (siteVenues, error) {
	if !need {
		return siteVenues{}, nil
	}
	m, err := db.HoldingOn(tx).SiteLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the peers' venues: %w", err)
	}
	return siteVenues(m), nil
}

// name is the venue's name for a site row, PartnerVenueUnknown when it cannot be read.
func (s siteVenues) name(id uuid.UUID) string {
	if v, ok := s[id]; ok && v.Name != "" {
		return v.Name
	}
	return PartnerVenueUnknown
}

// nameSiteRecords fills, on every record that names a site, the venue's name and the K-39
// blanking (see the file comment). Records that name none are left byte for byte as they were.
func nameSiteRecords(ctx context.Context, tx pgx.Tx, recs []Record) error {
	need := false
	for _, r := range recs {
		if r.SiteLocationID != nil || r.AtPartnerVenue {
			need = true
			break
		}
	}
	venues, err := siteVenuesFor(ctx, tx, need)
	if err != nil {
		return err
	}
	for i := range recs {
		if !recs[i].AtPartnerVenue {
			continue
		}
		recs[i].LocationName = PartnerVenueUnknown
		if recs[i].SiteLocationID != nil {
			recs[i].LocationName = venues.name(*recs[i].SiteLocationID)
		}
		recs[i].IPMatch, recs[i].GPSMatch, recs[i].Trust = nil, nil, nil
		if recs[i].Note != "" {
			recs[i].Note = PartnerVenueNote
		}
	}
	return nil
}

// PartnerVenueNote stands in for the engine's sentence on a record taken at a partner venue
// (K-39; the API-1c review's G-ORTA-1). The engine's sentences describe the very evidence the
// docket hides -- "verified via GPS only — no network proof of place" says the phone was not on
// the peer's network, "network proof of place: the source IP matches the location" that it was,
// and the cross-tenant QR guardrail's names the peer's network outright. A note from the
// business's OWN policy is no safer: a tenant statement is a copy of a shipped one, its reason
// included (tenant.copyOfShipped), so the same words arrive with policy_layer = 'tenant'. So
// every note on such a record is replaced; NoteIsTenants still says whose rule decided.
const PartnerVenueNote = "Decided by your rules at a partner venue — evidence is not shown."

// PartnerTapCap is how many of a holding peer's staff's records the reports section LISTS for a
// week -- a bound on the page, never on anything summed (nothing here is summed: see PartnerTaps).
const PartnerTapCap = 100

// PartnerTap is one record a holding PEER's employee made at THIS business's venue or plaque, as
// this business's projection carries it (M11 API-1c, ADR 0027 §9; K-1d/K-1f/K-1h): who, which
// way, when, where and the OUTCOME -- never the reason, never evidence. The record is the
// employer's; its hours are the employer's to compute.
type PartnerTap struct {
	At        time.Time
	Direction string
	// Employer is the employer's code (its API code, a snapshot at the tap), "" when it has none.
	Employer     string
	EmployeeName string
	EmployeeRef  string
	EmployeeCode string
	Venue        string
	TagUID       string
	// Outcome is the projection's closed set, folded to the LAST row of the record (an approval
	// or rejection supersedes the pending_review it decides): counted, pending_review, approved,
	// rejected, refused_tag_not_active, refused, employer_manual.
	Outcome string
}

// PartnerTaps is the reports section's listing of a week's partner-staff records.
//
// 🔴 IT HAS NO HOURS FIELD, AND THAT IS THE RULE RATHER THAN A GAP (ADR 0027 §9, 10th round: the
// projection does not see the employer's chain between two taps -- pairing "the latest in with
// the earliest out" here counted 9 h and 32 h intervals nobody worked). The screen says the hours
// are the employer's. This business's own staff's hours are not touched: the ledger reads this
// business's transactions alone.
//
// 🔴 MANUAL IS APART (K-1a): the employer's manager typed those records for this venue; they carry
// no physical evidence and this business cannot dispute them, so they are listed separately and
// counted in nothing.
type PartnerTaps struct {
	Queried bool
	Taps    []PartnerTap
	Manual  []PartnerTap
	// More is true when the week held more records than PartnerTapCap lists.
	More bool
}

// partnerTaps reads the week's partner-staff records from the projection.
func partnerTaps(ctx context.Context, q *store.Queries, tenantID uuid.UUID, p Period) (PartnerTaps, error) {
	rows, err := q.ListSitePunchesForPeriod(ctx, store.ListSitePunchesForPeriodParams{
		TenantID: tenantID,
		FromAt:   p.From,
		UntilAt:  p.To,
		RowLimit: readLimit(PartnerTapCap),
	})
	if err != nil {
		return PartnerTaps{}, fmt.Errorf("list the partner staff's taps: %w", err)
	}
	out := PartnerTaps{Queried: true} // the rows arrive in time order (the query's ORDER BY)
	if len(rows) > PartnerTapCap {
		rows, out.More = rows[:PartnerTapCap], true
	}
	for _, r := range rows {
		t := PartnerTap{
			At:           r.OccurredAt,
			Direction:    deref(r.Type),
			Employer:     deref(r.EmployerApiCode),
			EmployeeName: deref(r.EmployeeName),
			EmployeeRef:  deref(r.EmployeeRef),
			EmployeeCode: deref(r.EmployeeCode),
			Venue:        deref(r.LocationName),
			TagUID:       deref(r.SiteTagUid),
			Outcome:      r.Outcome,
		}
		if r.Channel == "manual" {
			out.Manual = append(out.Manual, t)
			continue
		}
		out.Taps = append(out.Taps, t)
	}
	return out, nil
}
