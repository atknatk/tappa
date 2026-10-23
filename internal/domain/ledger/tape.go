package ledger

// tape.go -- The Pass's hourly tape (M12-14, D3): for each hour of TODAY that has begun, how many
// people were on shift at each venue.
//
// 🔴 THE PATH IS ListWorkedShiftEvents, REUSED, AND THE CHOICE WAS MEASURED. The card offered it
// or a new aggregation. The reuse reads [local midnight - tap.StaleOpenIn, now) -- one tenant- and
// time-bounded range -- and pairs in Go, the way the reports section already does; a SQL
// aggregation would have to pair the chain (LEAD over each person's rows) and expand hours
// (generate_series) to answer the same question, and would be a SECOND definition of "on shift"
// beside the report's. EXPLAIN (ANALYZE, BUFFERS), 2026-10-20, seed tenant 10000000-...-0001
// after `make seed` and one simulated day (22 rows in the table): Bitmap Index Scan on
// transactions_tenant_occurred_idx (the window) -> Bitmap Heap Scan, Rows Removed by Filter 3
// (practice / no direction) -> left joins to transaction_reviews, employees, locations,
// departments -> Sort -> Limit; 19 rows, shared hit=19, 1.06 ms. One observation on a tiny table,
// not a property -- the query's own comment says why it names no index.
//
// WHAT "ON SHIFT IN AN HOUR" MEANS HERE, exactly. Per person, in order (one forward pass, the
// report's): an `in` opens an entry, a later `in` replaces it (the earlier one is left open -- its
// hours are not guessed, the report's reading), and an `out` is classified by ADR 0032 §A.6's one
// rule (checkout.go -- the report's walker, M15-05): when it CLOSES the open `in` the two are an
// interval [in, out) at the `in`'s venue; a checkout too far from it (a) or of a later business
// day (b) is not a shift and ends the entry with no interval, as a checkout with no `in` before it
// ((c), (d), (e)) adds none. An entry still open at now is [in, now) when it
// is no older than tap.StaleOpenIn -- the floor's rule (CountOnTheFloorByVenue) -- and ignored when
// it is older. A person counts in an hour at a venue when one of their intervals there overlaps
// the hour; each person once per hour per venue. Flagged entries count, as on the floor: the tape
// shows records, the reports section decides pay.
//
// 🔴 NO FUTURE HOUR, AND THE DAY IS A CALENDAR DAY. The hours run from local midnight to the hour
// that holds now, as instants (a DST day has 23 or 25 of them); a shift that began before midnight
// shows from 00:00 (its `in` is inside the read's tail). Nothing is drawn for an hour that has not
// begun -- there is no roster to project from (D1).
//
// 🔴 §4.6: A CAPPED READ SAYS SO. The read asks for TapeEventCap + 1 rows; when the extra one
// arrives the tape is Incomplete and the person the cut fell inside is dropped whole (half a chain
// would invent an open entry and OVER-count), so every number left is at least what it says.

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

// TapeEventCap is how many direction-carrying records one tape read takes.
//
// IT IS ReportEventCap, NOT A NUMBER OF ITS OWN: the same statement, bounded the same way, over a
// window (one day plus the tail) that is never wider than the reports section's week -- so the tape
// is never closer to its cap than that week's report is to the same one.
const TapeEventCap = ReportEventCap

// Tape is today's hourly count of people on shift, per venue.
type Tape struct {
	State Reading
	// Incomplete is true when the read hit TapeEventCap: the counts are floors and the screen says
	// the tape is incomplete.
	Incomplete bool
	// Hours are the starts of today's hours that have begun, local midnight first and the hour
	// holding now last, as instants (render converts to the zone).
	Hours []time.Time
	// Venues are the venues somebody was on shift at today, this business's by name, then the
	// partner venues, then the unplaced bucket.
	Venues []TapeVenue
}

// TapeVenue is one venue's row: People[i] is the number of people on shift there during Hours[i].
type TapeVenue struct {
	// LocationID is this business's venue, or a holding peer's when Partner is set; uuid.Nil for
	// Unplaced.
	LocationID uuid.UUID
	// Name comes from this business's locations or, for a partner venue, from
	// holding_site_locations (siteVenuesFor).
	Name     string
	Partner  bool
	Unplaced bool
	People   []int
}

// tapeEvent is one direction-carrying record, reduced to what the tape needs: no verdict, no
// evidence, no name of a person.
type tapeEvent struct {
	who   uuid.UUID
	at    time.Time
	in    bool
	venue tapeVenueRef
	// createdAt and dayStart are the checkout classification's W and S (checkout.go).
	createdAt time.Time
	dayStart  *time.Time
}

// tapeVenueRef is where an interval is counted.
type tapeVenueRef struct {
	id       uuid.UUID
	partner  bool
	unplaced bool
	name     string
}

// tapeAt reads and builds today's tape as of now, in zone.
func tapeAt(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, now time.Time, zone *time.Location) (Tape, error) {
	if zone == nil {
		zone = time.UTC
	}
	y, m, d := now.In(zone).Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, zone)
	rows, err := store.New(tx).ListWorkedShiftEvents(ctx, store.ListWorkedShiftEventsParams{
		TenantID: tenantID,
		FromAt:   midnight.Add(-tap.StaleOpenIn),
		UntilAt:  now,
		RowLimit: readLimit(TapeEventCap),
	})
	if err != nil {
		return Tape{}, fmt.Errorf("list today's shift events: %w", err)
	}
	incomplete := len(rows) > TapeEventCap
	if incomplete {
		rows = rows[:TapeEventCap]
	}
	need := false
	for _, r := range rows {
		need = need || r.SiteLocationID != nil
	}
	sites, err := siteVenuesFor(ctx, tx, need)
	if err != nil {
		return Tape{}, err
	}
	evs := tapeEvents(rows, sites, incomplete)
	hours, venues := tapeOf(evs, midnight, now)
	return Tape{State: Measured, Incomplete: incomplete, Hours: hours, Venues: venues}, nil
}

// tapeEvents maps the rows onto the arithmetic's input. cut says the read hit its cap: the rows
// arrive by person, so the last person's chain may be cut short, and it is dropped whole -- half a
// chain would leave an `in` whose `out` was not read looking open, and over-count.
func tapeEvents(rows []store.ListWorkedShiftEventsRow, sites siteVenues, cut bool) []tapeEvent {
	evs := make([]tapeEvent, 0, len(rows))
	for _, r := range rows {
		if r.EmployeeID == nil || r.Type == nil {
			continue // no person, no chain: nobody to count (the report counts these apart)
		}
		e := tapeEvent{who: *r.EmployeeID, at: r.OccurredAt, in: *r.Type == "in", createdAt: r.CreatedAt, dayStart: r.BusinessDayStart}
		switch {
		case r.LocationID != nil:
			e.venue = tapeVenueRef{id: *r.LocationID, name: deref(r.LocationName)}
		case r.SiteLocationID != nil:
			e.venue = tapeVenueRef{id: *r.SiteLocationID, partner: true, name: sites.name(*r.SiteLocationID)}
		default:
			e.venue = tapeVenueRef{unplaced: true}
		}
		evs = append(evs, e)
	}
	if cut && len(evs) > 0 {
		last := evs[len(evs)-1].who
		for len(evs) > 0 && evs[len(evs)-1].who == last {
			evs = evs[:len(evs)-1]
		}
	}
	return evs
}

// tapeOf is the arithmetic: evs ordered by (person, time), the day starting at midnight, now the
// instant the page measures at. Pure, so the table tests drive it without a database.
func tapeOf(evs []tapeEvent, midnight, now time.Time) ([]time.Time, []TapeVenue) {
	var hours []time.Time
	for h := midnight; !h.After(now); h = h.Add(time.Hour) {
		hours = append(hours, h)
	}
	type interval struct {
		who      uuid.UUID
		venue    tapeVenueRef
		from, to time.Time
	}
	var intervals []interval
	var open *tapeEvent
	// stillOpenAt is an entry nothing closed, drawn up to now -- the floor's rule
	// (CountOnTheFloorByVenue): no older than tap.StaleOpenIn.
	stillOpenAt := func(to time.Time) {
		if open != nil && to.Sub(open.at) <= tap.StaleOpenIn && open.at.Before(to) {
			intervals = append(intervals, interval{open.who, open.venue, open.at, to})
		}
		open = nil
	}
	var chain checkoutChain
	for i := range evs {
		e := &evs[i]
		if i == 0 || evs[i-1].who != e.who {
			stillOpenAt(now) // the previous person's entry is still open at now
			chain = checkoutChain{}
		}
		if e.in {
			open = e // a second `in` leaves the first open: not guessed
			chain.checkin(e.at, e.createdAt)
			continue
		}
		class := chain.checkout(e.at, e.createdAt, e.dayStart)
		if open == nil || !consumesTheIn(class) {
			continue // (c), (d), (e): no `in` before it, nothing to end
		}
		if class == tap.CheckoutCloses && open.at.Before(e.at) {
			intervals = append(intervals, interval{open.who, open.venue, open.at, e.at})
		}
		open = nil // closes, (a) and (b) all end the entry; only closes draws it
	}
	stillOpenAt(now)

	counted := map[tapeVenueRef][]map[uuid.UUID]bool{}
	for _, iv := range intervals {
		for i, h := range hours {
			end := h.Add(time.Hour)
			if !iv.from.Before(end) || !iv.to.After(h) {
				continue
			}
			sets := counted[iv.venue]
			if sets == nil {
				sets = make([]map[uuid.UUID]bool, len(hours))
				counted[iv.venue] = sets
			}
			if sets[i] == nil {
				sets[i] = map[uuid.UUID]bool{}
			}
			sets[i][iv.who] = true
		}
	}
	venues := make([]TapeVenue, 0, len(counted))
	for ref, sets := range counted {
		v := TapeVenue{LocationID: ref.id, Name: ref.name, Partner: ref.partner, Unplaced: ref.unplaced, People: make([]int, len(hours))}
		for i, set := range sets {
			v.People[i] = len(set)
		}
		venues = append(venues, v)
	}
	sort.Slice(venues, func(i, j int) bool {
		a, b := venues[i], venues[j]
		if a.Unplaced != b.Unplaced {
			return b.Unplaced
		}
		if a.Partner != b.Partner {
			return b.Partner
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.LocationID.String() < b.LocationID.String()
	})
	return hours, venues
}
