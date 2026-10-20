package ledger

// floor.go -- The Pass's "on the floor now" (M12-13, D3), and the frame every Pass widget shares:
// the three-way Reading and passWidgets, which runs each widget inside its own savepoint.
//
// 🔴 §4.6 -- NEVER 0 FOR SOMETHING NOT MEASURED. Every widget carries a Reading: NotQueried (the
// zero value: the read was not asked for, so the screen claims nothing), ReadFailed (it was asked
// and the database did not answer: the screen says "could not be read", never "0"), Measured.
// A widget is not a record, so its failure must not cost the manager the day's records: each one
// runs in its own SAVEPOINT (inSavepointTx), exactly the advisories' pattern and for the reason
// advisories' comment measured -- without the savepoint a failed statement aborts the transaction
// and the outer COMMIT turns it into a 500.
//
// 🔴 §4.1 / §4.2 -- "ON THE FLOOR" IS DERIVED FROM RECORDED TAPS. It is the set of people whose
// latest check-in has no checkout after it; nothing is tracked, nothing is read between taps, no
// coordinate or address is selected (db/queries/transactions.sql, CountOnTheFloorByVenue). It is
// a count as of the page's own load, and the copy must not say "live".

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// Reading is how far one widget's figure got.
type Reading uint8

const (
	// NotQueried is the zero value: nobody asked, and the widget claims nothing.
	NotQueried Reading = iota
	// ReadFailed is a read that was asked and did not answer. The figure is unknown -- "?" or
	// "could not be read" on screen, never 0.
	ReadFailed
	// Measured is a figure the database answered.
	Measured
)

// Known reports whether the figure was measured. It is the only state in which a widget may print
// a number, zero included.
func (r Reading) Known() bool { return r == Measured }

// Floor is how many people are checked in right now, and where.
type Floor struct {
	State Reading
	// People is the number of distinct people whose latest check-in, no older than
	// tap.StaleOpenIn, has no checkout after it. It is the sum of Venues' People: one statement
	// answers both (the card's "büyük sayı ve satırlar aynı okumadan").
	People int
	// Venues is the per-venue tally, most people first. A venue with nobody on the floor is not
	// listed: the read groups the people it found, it does not list the business's venues.
	Venues []FloorVenue
	// Since is the oldest check-in the count could include (now - tap.StaleOpenIn), carried so a
	// screen can say what "now" was measured against.
	Since time.Time
}

// FloorVenue is one venue's share of the floor.
type FloorVenue struct {
	// LocationID is this business's venue, or a holding peer's when Partner is set; uuid.Nil when
	// the records named no venue at all (Unplaced).
	LocationID uuid.UUID
	// Name is the venue's name; for a partner venue it comes from holding_site_locations
	// (siteVenuesFor) -- PartnerVenueUnknown when the membership has ended.
	Name string
	// Partner marks a holding peer's venue (ADR 0027 §9). It carries a count and a name, never
	// evidence (K-39).
	Partner bool
	// Unplaced marks the people whose latest check-in names no venue. Kept, not dropped (§4.6).
	Unplaced bool
	People   int
}

// passWidgets reads The Pass's widgets into out, each inside its own savepoint, and never returns
// an error: a widget that fails is ReadFailed and logged (§7), the page stands.
//
// ONE CLOCK READING FOR ALL OF THEM, taken here, so two widgets on one page cannot disagree about
// what "now" was.
func (r *Reader) passWidgets(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, out *Screen) {
	now := r.now()
	out.Floor = widget(ctx, r, tx, tenantID, "floor", Floor{State: ReadFailed}, func(sp pgx.Tx) (Floor, error) {
		return floorAt(ctx, sp, tenantID, now)
	})
}

// widget runs one widget read in its own savepoint. On failure it logs the error with the tenant
// and answers failed -- the widget's ReadFailed value; whatever the read had half-built is
// discarded, because a figure from a read that failed is not a measurement.
func widget[T any](ctx context.Context, r *Reader, tx pgx.Tx, tenantID uuid.UUID, what string, failed T, read func(pgx.Tx) (T, error)) T {
	var got T
	err := inSavepointTx(ctx, tx, func(sp pgx.Tx) error {
		v, err := read(sp)
		if err != nil {
			return err
		}
		got = v
		return nil
	})
	if err != nil {
		r.log.Error("ledger: could not read a Pass widget; it will say so rather than print a number",
			"widget", what, "err", err, "tenant_id", tenantID)
		return failed
	}
	return got
}

// floorAt counts the people on the floor at now.
func floorAt(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, now time.Time) (Floor, error) {
	since := now.Add(-tap.StaleOpenIn)
	rows, err := store.New(tx).CountOnTheFloorByVenue(ctx, store.CountOnTheFloorByVenueParams{
		TenantID: tenantID,
		Since:    since,
	})
	if err != nil {
		return Floor{}, fmt.Errorf("count the floor: %w", err)
	}
	need := false
	for _, row := range rows {
		need = need || row.SiteLocationID != nil
	}
	sites, err := siteVenuesFor(ctx, tx, need)
	if err != nil {
		return Floor{}, err
	}
	out := Floor{State: Measured, Since: since, Venues: make([]FloorVenue, 0, len(rows))}
	for _, row := range rows {
		v := FloorVenue{Name: deref(row.LocationName), People: int(row.People)}
		switch {
		case row.LocationID != nil:
			v.LocationID = *row.LocationID
		case row.SiteLocationID != nil:
			v.LocationID, v.Partner, v.Name = *row.SiteLocationID, true, sites.name(*row.SiteLocationID)
		default:
			v.Unplaced = true
		}
		out.People += v.People
		out.Venues = append(out.Venues, v)
	}
	// The query orders by count and this business's names; a partner's name is only known here,
	// so the order is settled here: most people first, then by name, the unplaced bucket last.
	sort.SliceStable(out.Venues, func(i, j int) bool {
		a, b := out.Venues[i], out.Venues[j]
		if a.Unplaced != b.Unplaced {
			return b.Unplaced
		}
		if a.People != b.People {
			return a.People > b.People
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.LocationID.String() < b.LocationID.String()
	})
	return out, nil
}
