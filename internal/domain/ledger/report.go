package ledger

// report.go -- the READ side of the REPORTS section (M6-07 phase A): worked hours.
//
// 🔴 NOTHING HERE IS STORED, AND THAT IS §4.3 RATHER THAN A PREFERENCE. There is no
// hours column and no minutes_late column, and there must not be one: a stored total
// is a second representation of a sum the immutable records already determine, and
// keeping it true would mean UPDATEs on a table that takes none. Every figure below
// is recomputed from `transactions` on every view, which is also what makes a
// corrected record (M6-08 writes a NEW row) change the report without anything being
// rewritten.
//
// 🔴 NO FLOAT TOUCHES AN ATTENDANCE FIGURE (§6). Durations are time.Duration --
// int64 nanoseconds -- and lateness is whole minutes as an int. There is no float64
// hour, no EXTRACT(EPOCH ...)::float and no AVG() anywhere in this path. The
// separation is asserted rather than asserted-about:
// TestAccumulate_TotalsAreExactUnderRepeatedOddMinutes adds a quantity that a float
// accumulator gets wrong and is measured going red when one is used.
//
// 🔴 §4.7 -- WHAT A REPORT ROW CANNOT CARRY. The query it is built from selects no
// gps_lat, no gps_lng, no source_ip, no policy_context and no entered_by; no type in
// this file has a field for any of them. Two independent walls, the same shape
// Record and Person use: a CSV cannot serialise a coordinate the struct has no field
// for, and a field added later would have nothing to fill it from. That matters more
// here than on a screen, because phase B of this task turns these values into a file.
//
// TENANT SCOPE (§4.5, belt + braces) is the package's, unchanged: every read runs
// inside db.(*DB).WithTenant so RLS applies, AND passes the same tenant id as an
// explicit predicate. The id is the caller's, taken from the signed panel session.

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

// ReportDays is how many local days one report covers: a week.
//
// THE WEEK STARTS ON MONDAY, and that is a decision rather than a default. Both
// markets this ships to (Malta, Turkey) run a Monday-to-Sunday week, ISO 8601 says
// Monday, and the M6-07 card already fixes ISO 8601 as the date format for the
// export. Go's time.Weekday starts at Sunday, so the arithmetic below has to shift
// it explicitly -- which is exactly the kind of off-by-one that is invisible until
// somebody's Sunday night lands in the wrong week, so it is pinned by
// TestWeekOf_StartsOnMondayInTheTenantsOwnZone.
const ReportDays = 7

// ReportEventCap is the largest number of direction-carrying records one report will
// read.
//
// 🔴 IT IS A REFUSAL TO ANSWER, NOT A PAGE, and the difference is the whole reason it
// is stated. A list can be paged: half a list is still an honest half. A TOTAL cannot
// -- half a person's chain is not a smaller number, it is a WRONG one, and a wrong
// payroll figure that looks like a right one is the worst thing this section could
// produce. So the query asks for one row more than this, and if that row arrives the
// report is marked incomplete.
//
// ⚠️ WHAT THE SCREEN THEN DOES, EXACTLY, BECAUSE THIS LINE USED TO OVERSTATE IT. It
// said the report "says it is incomplete INSTEAD OF printing a sum". It prints both:
// a red-line notice above the figures saying the hours below are "a floor and not a
// total", and then the figures. That is the honest shape -- a floor is useful and a
// blank page is not -- but the notice is the whole of the protection, so it is the
// thing that must never be softened. It is pinned by
// TestReportsSection_RefusesToPresentATruncatedReadAsATotal (internal/handler), with a
// positive control that an untruncated read does NOT carry it.
//
// ⚠️ THAT NAME USED TO BE WRAPPED ACROSS TWO LINES, which made it unfindable by the
// grep a reader runs to check the guarantee. Same rule as the one M6-11 adopted after
// citing two tests that did not exist: a name a grep cannot find is a citation that
// does not work.
//
// WHY 20 000, AND THE ANSWER IS STATED AS AN INEQUALITY BECAUSE THE TWO CLAIMS THAT
// USED TO SIT HERE WERE HEADCOUNTS AND BOTH WERE WRONG.
//
// A week of records is bounded by the PAYROLL rather than by history: people tap in and
// out, so a week costs roughly 2 x shifts-per-week x staff. The two withdrawn claims:
//
//	"well past rosterDesignCeiling-sized businesses"   FALSE, and by a wide margin. The
//	    reach of this cap is ReportEventCap / (2 x shifts per week), and that quantity
//	    is a FRACTION of internal/handler's rosterDesignCeiling -- the payroll the panel
//	    PROMISES a manager can browse end to end. A business inside that promise can
//	    exceed this cap. The two constants are the comparison; neither is a measurement.
//	"an order of magnitude past the largest tenant
//	 in the development database"                      FALSE. That tenant's busiest week
//	    read 12 087 rows on 2026-08-10 (the figure and its conditions are in
//	    db/queries/transactions.sql). This cap is under twice it, not ten times.
//
// SO THE HONEST STATEMENT IS: it bounds ONE REQUEST's cost, it is under twice the
// largest week this repository has ever measured, and a business whose week produces
// more rows than this will be told its totals are a floor. That is a counted limit
// rather than a comfortable margin, and the answer for such a business is the export
// (phase B) rather than a bigger number here -- raising it raises what a single panel
// request may cost the tap path's connection pool.
//
// 🔴 HOW CLOSE THE CAP IS: THE ANSWER DEPENDS ENTIRELY ON WHICH POPULATION IS COUNTED,
// AND TWO INDEPENDENT REVIEWS OF PHASE B ANSWERED IT DIFFERENTLY BY A FACTOR OF ONE AND
// A HALF. The disagreement is settled by counting what ListWorkedShiftEvents' own WHERE
// counts -- tenant, `type IS NOT NULL`, `NOT practice`, over [local Monday 00:00,
// +7 local days + closingTail) -- rather than raw directed rows bucketed by calendar
// week. Measured on the development database's busiest week, one tenant, one week,
// after ANALYZE (2026-08-10; the four variants are in the M6-07 phase B delivery notes):
//
//	predicate                                        rows      of the cap
//	no practice filter, no tail   (the naive shape)  19 134         96%
//	no practice filter, with tail                    22 144        111%
//	NOT practice, no tail                            10 839         54%
//	NOT practice, with tail       (THIS QUERY)       12 562         63%
//
// THE PRACTICE FILTER IS THE WHOLE OF THE GAP and it is not a rounding difference:
// across the table, `type IS NOT NULL` holds 152 519 rows and `+ NOT practice` holds
// 104 353, so a count that omits it inflates a week by about half. The tail pushes the
// other way and is much smaller. The naive figure is therefore a MISLABEL of exactly
// the kind this file already records twice -- a population quoted under another
// population's name -- and the honest reading is that this cap sits at roughly two
// thirds of its budget on the busiest week measured, not at the brink.
//
// ⚠️ AND THE ABSOLUTE ROW COUNTS ABOVE DRIFT UPWARD ON EVERY `make test` RUN. The
// PROPORTIONS and the ORDERING of the four variants are what to re-derive; the query
// that does it is the one in this block.
const ReportEventCap = 20000

// closingTail is how far PAST the reporting period a closing tap is looked for.
//
// 🔴 IT IS tap.StaleOpenIn AND NOT A NUMBER OF ITS OWN, which is the single point of
// exporting that constant. A private 18h here would be the "second representation"
// shape this repository keeps paying for, with the drift invisible because each copy
// is self-consistent.
//
// THE CONSTANT IS SHARED AND, SINCE ADR 0032, SO IS THE QUANTITY (M15-03, M15-04). Until then
// the engine measured in.Now - open.OccurredAt -- the SERVER's clock at the moment the closing
// tap was judged -- while this report measures out.At - in.At, the two records' own occurred_at,
// and a security audit built the ordinary case where they parted company: a checkout for a shift
// three days ago, declared at its real instant, was stamped "stale open check-in" by the engine
// (its clock was three days past the entry) and paid here as a clean eight-hour pair. The engine
// now measures at - open.OccurredAt, where at is the tap's OWN instant (tap.tapInstant: the
// declared occurred_at when the client sent one, the server's now otherwise -- the instant the
// record carries), so for a pair the engine closed the two read the same number against the
// same threshold.
//
// WHAT STILL DIFFERS, STATED: with the business-day switch on (TAPPA_BUSINESS_DAY_FROM) the engine
// also refuses to close an entry of an earlier business day (ADR 0032 §A.1) and freezes the
// tap's business day on the row (business_day_start); this report does not read that column yet
// -- the one checkout classification of §A.6 reaches it in M15-05. Measuring the report against
// a server clock is still not an option: last month's hours must not depend on when the report
// was opened.
//
// WHAT IT BUYS: a shift that starts at 18:00 on the last night of the period and ends
// at 02:00 is a CLOSED interval rather than an open one. Without the tail, every
// overnight venue would show its last night of every period as an anomaly -- which is
// precisely the Rusty Bar case §5 names.
//
// WHAT IT COSTS, STATED: a check-in closed by a tap MORE than this long afterwards is
// reported as open rather than as a very long shift. That is the answer this product
// wants — see tooLong below — so the horizon and the "this is not a work interval"
// rule are the same quantity used twice, deliberately.
const closingTail = tap.StaleOpenIn

// Period is the span a report covers.
type Period struct {
	// From and To are UTC instants; To is EXCLUSIVE. They are derived from local
	// midnights in the tenant's zone, so the period is a run of LOCAL DAYS rather
	// than a fixed number of hours -- a DST week is 167 or 169 hours long and a
	// 7x24h window would put an hour of Monday into the previous Sunday.
	From, To time.Time
	// FirstDay is the local calendar day From begins.
	FirstDay Date
}

// Days is how many local days the period holds. It is a constant today; the method
// exists so callers ask the period rather than repeat the number.
func (p Period) Days() int { return ReportDays }

// HoursState says whether an interval's hours are in the total, and if not, why.
//
// 🔴 THERE IS NO "DROPPED" STATE, AND THAT IS §4.6. A flagged record is a record; the
// report may exclude its hours from the total but may not make them disappear. Every
// interval this engine builds lands in exactly one of these, and every one of them is
// printed with its own number and its own sentence.
type HoursState string

const (
	// HoursCounted is in the payroll total: both ends were decided `ok` by the
	// engine, or flagged and then APPROVED by a manager.
	HoursCounted HoursState = "counted"
	// HoursAwaiting is a flagged record nobody has decided yet. The hours are
	// computed and reported SEPARATELY -- never in the total.
	HoursAwaiting HoursState = "awaiting"
	// HoursRefused is a flagged record a manager REJECTED. Excluded and named.
	HoursRefused HoursState = "refused"
)

// ⚠️ THERE IS NO "TOO LONG" STATE HERE, AND ITS ABSENCE IS DELIBERATE. An interval
// longer than closingTail is not an interval with a bad verdict, it is not an
// interval at all -- the engine calls the closing tap a probable forgotten checkout,
// so what the report has is an UNCLOSED check-in. It becomes an OpenEntry and is
// counted by Totals.TooLong; giving it a fourth HoursState would put a duration
// beside it that nobody worked.

// PersonHours is one employee's week.
type PersonHours struct {
	EmployeeID uuid.UUID
	// Name is "" when the record carries an employee id the roster no longer names.
	// The screen says "unknown" rather than printing nothing, because a blank cell
	// reads as "nobody".
	Name string
	// Role, HomeLocationID and HomeVenue are the person's job title and HOME venue as the
	// roster holds them NOW (M12-21; ListRollEmployees) -- not as they were on the days of
	// the week, which nothing records. Role is "" when none is set and the screen omits it.
	// HomeLocationID is nil when the person has no home venue (K-14) OR when the read did
	// not answer (Report.Homes says which); either way no other-venue mark is claimed.
	Role           string
	HomeLocationID *uuid.UUID
	HomeVenue      string
	// Worked is the payroll figure: the sum of HoursCounted intervals only.
	Worked time.Duration
	// Daily is Worked split over the period's local days, FirstDay first. It is a
	// slice rather than a [7]time.Duration so the template ranges over it without
	// knowing how long a period is.
	Daily []time.Duration
	// Marks runs beside Daily, one per local day: what else is true of that day besides
	// its counted hours (M12-21). See DayMarks for exactly what each mark counts.
	Marks []DayMarks
	// Awaiting and Refused are the hours held out of Worked, kept apart because they
	// are different instructions to a manager: one needs a decision, one has had one.
	Awaiting time.Duration
	Refused  time.Duration
	// Shifts counts the intervals behind Worked.
	Shifts int
	// ManualArrivals counts how many of this person's attributed CHECK-INS a manager
	// typed (channel='manual'). §5 and the M6-07 card both ask for those to be visible
	// rather than blended in: a manual row carries no evidence of a physical touch.
	//
	// 🔴 IT WAS CALLED Manual AND EVERY SURFACE RENDERED IT AS "shifts", WHICH WAS
	// WRONG IN BOTH DIRECTIONS — measured (2026-08-10, M6-08's audit round), on the
	// real arithmetic:
	//
	//	one typed pair                        Manual 1  Shifts 1   correct by luck
	//	a typed check-in CORRECTED by another Manual 2  Shifts 1   OVER-counts a payroll column
	//	a tapped check-in, TYPED checkout     Manual 0  Shifts 1   UNDER-counts, and it is
	//	                                                            Q18's own scenario
	//	a typed check-in nobody closed        Manual 1  Shifts 0   not a shift at all
	//
	// The arithmetic is NOT changed — it lives in arrival(), which runs for every
	// attributed check-in, closed or not, and that is the right place for it (lateness
	// is a property of turning up). What changes is the NAME, on this field and on
	// every surface, so the number says what it counts. All four rows above are true
	// statements about ARRIVALS.
	//
	// ⚠️ WHAT IT STILL DOES NOT COUNT, said out loud rather than left to be discovered
	// a third time: a typed CHECKOUT. "How much of this week was typed rather than
	// tapped" is a different question and would need both ends; this answers "how many
	// times did somebody's arrival get typed for them".
	ManualArrivals int
	// LateShifts and LateBy describe ARRIVALS, not intervals: lateness is a property
	// of turning up. LateBy is the sum of the positive lateness only, so somebody who
	// is ten minutes early one day and ten late the next is not "on time on average".
	LateShifts int
	LateBy     time.Duration
	// Unmeasured counts check-ins whose shift could not be resolved, which is the
	// honest third answer beside "late" and "on time" (M4-05: no shift means lateness
	// is NOT COMPUTED). The screen says so in those words.
	Unmeasured int
	// Open counts this person's unclosed check-ins inside the period. The hours are
	// not estimated and never enter any of the totals above (Q18).
	Open int
}

// DayMarks is what one cell of the Roll says besides its hours (M12-21). Each mark is a
// CLAIM, so each is false unless measured: a false mark is "not seen", never "seen not to be".
type DayMarks struct {
	// Other: a COUNTED shift that started this day began at a venue other than the person's
	// home venue -- a holding peer's venue included (ADR 0027 §9: it is not theirs). Never set
	// for a person with no home venue, for a shift whose record names no venue, or when the
	// roster read did not answer: those shifts are Totals.OtherVenueUnknown, not "at home".
	// Durations and this mark only -- no evidence of the peer's is behind it (K-39).
	Other bool
	// Manual: a manager TYPED one of this person's check-ins this day (channel='manual') --
	// the same population as PersonHours.ManualArrivals, so the marks and the tally can be
	// reconciled (two typed arrivals on one day are one mark and a tally of two). A typed
	// CHECKOUT does not set it, for the reason ManualArrivals gives.
	Manual bool
	// Open: one of this person's check-ins this day has no checkout (an OpenEntry, Q18) -- on
	// a day with hours too, which is a different cell from a day with none.
	Open bool
}

// VenueHours is one location's week. The key is the location of the CHECK-IN: a
// person who taps in at one venue and out at another worked the shift they started,
// and §5 already treats moving down the chain as normal.
type VenueHours struct {
	LocationID uuid.UUID
	Name       string
	Worked     time.Duration
	Shifts     int
}

// OpenEntry is a check-in with no checkout (Q18).
//
// 🔴 THE SYSTEM PRODUCES NO CHECKOUT FOR IT, and this type is what that decision looks
// like from the report's side: it carries the opening tap and NOTHING resembling a
// closing one -- no estimated end, no assumed shift length, no hours. A manager enters
// the correction (M6-08); until they do, the total is short and the screen says so.
type OpenEntry struct {
	// EmployeeID is whose entry it is (M12-21), so the Roll can mark the day on that
	// person's row; the name alone is not unique.
	EmployeeID   uuid.UUID
	EmployeeName string
	LocationName string
	At           time.Time
	// Stale is true when the entry has been open longer than the engine's own
	// forgotten-checkout threshold at the moment the report was built. It separates
	// "somebody is on shift right now", which is the ordinary state of a live day,
	// from "somebody forgot", which needs a manager.
	//
	// ⚠️ IT DOES NOT SAY WHY THE ENTRY IS OPEN, and the M6-07 card is explicit that
	// nothing can: a forgotten checkout and a pre-ADR-0008 masked check-in are the
	// same shape on disk. The screen must not claim to tell them apart.
	Stale bool
	// Manual marks an entry a manager typed rather than a tap.
	Manual bool
}

// Totals is the whole tenant's week, and it is summed from the SAME intervals the
// per-person rows are, rather than recomputed. One arithmetic, two presentations.
type Totals struct {
	Worked   time.Duration
	Awaiting time.Duration
	Refused  time.Duration
	Shifts   int
	Open     int
	TooLong  int
	// OtherVenue counts the COUNTED shifts (a subset of Shifts) that began at a venue other
	// than the person's home venue (M12-21) -- one per DayMarks.Other source, so a day with
	// two such shifts is one mark and two here. OtherVenueUnknown counts the counted shifts
	// that could not be judged: the person has no home venue, the record names no venue, or
	// the roster read did not answer (Report.Homes). The screen prints the second beside the
	// first, so an unjudged week never reads as "nobody worked elsewhere".
	OtherVenue        int
	OtherVenueUnknown int
	// ManualArrivals is the sum of PersonHours.ManualArrivals: check-ins a manager typed.
	ManualArrivals int
	// Approved counts the COUNTED shifts (a subset of Shifts) that are in the total only
	// because a manager APPROVED a flag: at least one end has verdict 'flag' and review
	// 'approved' (endpointState's second case).
	Approved int
}

// HomesRead is the tri-state of the roster read behind PersonHours.Role/HomeLocationID/
// HomeVenue, DayMarks.Other and Totals.OtherVenue (M12-21). The zero value is NOT QUERIED
// (no claim); Failed is a read that did not answer -- the report itself still renders, the
// other-venue figure says "could not be read" rather than 0; Queried is measured.
type HomesRead struct {
	Queried bool
	Failed  bool
}

// Report is one week of worked hours.
type Report struct {
	// Queried is set ONLY after the database has answered.
	//
	// 🔴 IT IS THE ANTI-FABRICATION FLAG, the same one Page and RosterPage carry. "No
	// hours were worked this week" is a claim about a business, and a zero Report
	// renders identically to a real empty week unless something separates them.
	Queried bool
	Period  Period
	Zone    *time.Location
	People  []PersonHours
	Venues  []VenueHours
	Open    []OpenEntry
	Totals  Totals
	// Homes is whether the roster read answered; see HomesRead.
	Homes HomesRead
	// PracticeTaps is how many TRAINING taps fall in the period. They are excluded
	// from every figure above; the number is here so "excluded" is not the same as
	// "invisible" (§5, M6-07 card).
	PracticeTaps int
	// Truncated means the read hit ReportEventCap, so every total above is a FLOOR
	// rather than a week's hours.
	//
	// ⚠️ THE SCREEN STILL PRINTS THE FIGURES, and this comment used to say it "refuses
	// to present them". It does not: it prints a notice above them saying they are a
	// floor and not a total, and then prints them. Withholding them was weighed and
	// rejected -- a floor is something a manager can act on and a blank page is not --
	// so the notice IS the protection, which is why it is asserted separately.
	Truncated bool
	// StartedEarlier counts checkouts whose check-in falls before the period. Their
	// hours belong to the previous report and are not counted here; the number exists
	// so a manager comparing two weeks can see the seam rather than wonder about it.
	StartedEarlier int
	// Unattributable counts direction-carrying records with no employee on them.
	// Migration 0005 permits that shape (§4.6: a record is written even with no
	// context) and no chain can hold it, so it is counted rather than dropped.
	Unattributable int
}

// ReportScreen is a Report plus what only the full document needs.
type ReportScreen struct {
	Report
	TenantName string
	// Partners are the week's records of holding PEERS' staff at this business's venues (M11
	// API-1c) -- listed, never summed; empty for a business in no holding.
	Partners PartnerTaps
}

// ReportFilter is what the manager asks for: any day inside the week they want.
//
// IT IS A DAY RATHER THAN A RANGE, deliberately. Two free dates make four illegal
// combinations (reversed, overlapping a DST seam, spanning a year, unbounded) that
// each need a rule, and none of them is what a manager asks for -- they ask for a
// week. The zero Date means the week the tenant is in now.
type ReportFilter struct {
	Day Date
}

// Hours loads one week of worked hours.
//
// ONE TRANSACTION, THREE QUERIES -- the same shape Screen and Roster use. The clock
// has to be read before the week's boundaries can be computed, and reading the events
// and the training-tap count in the same transaction means the two cannot disagree
// about which records existed. (Since M12-21 the roster read behind the titles and the
// other-venue marks rides in the same transaction too, inside a savepoint -- see rollHomes.)
func (r *Reader) Hours(ctx context.Context, tenantID uuid.UUID, f ReportFilter) (ReportScreen, error) {
	var out ReportScreen
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		out.TenantName = clock.Name
		zone := r.loadZone(clock.Timezone)
		out.Zone = zone

		day := f.Day
		if day.Zero() {
			now := time.Now().In(zone)
			day = Date{Year: now.Year(), Month: now.Month(), Day: now.Day()}
		}
		period := WeekOf(day, zone)
		out.Period = period

		rows, err := q.ListWorkedShiftEvents(ctx, store.ListWorkedShiftEventsParams{
			TenantID: tenantID,
			FromAt:   period.From,
			// THE READ RUNS PAST THE PERIOD so an overnight shift that starts inside
			// it can be closed. See closingTail.
			UntilAt: period.To.Add(closingTail),
			// ONE ROW MORE THAN CAN BE USED, which is how "was this complete" is
			// answered without a COUNT over the week -- the same trick the two paged
			// sections use, applied to a total rather than to a page.
			RowLimit: readLimit(ReportEventCap),
		})
		if err != nil {
			return fmt.Errorf("list worked shift events: %w", err)
		}

		practice, err := q.CountPracticeTaps(ctx, store.CountPracticeTapsParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
		})
		if err != nil {
			return fmt.Errorf("count practice taps: %w", err)
		}

		// M11 API-1c: a row taken at a holding peer's venue is filed under THAT venue and judged
		// against ITS shift (ADR 0027 §9, CLAUDE.md §5: the tapped location) -- read once, and only
		// when such a row is on hand.
		need := false
		for _, row := range rows {
			need = need || row.SiteLocationID != nil
		}
		sites, err := siteVenuesFor(ctx, tx, need)
		if err != nil {
			return err
		}
		evs, unattributable := events(rows, clock.Timezone, sites)
		if out.Partners, err = partnerTaps(ctx, q, tenantID, period, false); err != nil {
			return err
		}
		homes, homesRead := r.rollHomes(ctx, tx, tenantID, evs)
		for i := range evs {
			if h, ok := homes[evs[i].EmployeeID]; ok {
				evs[i].Home = h.HomeLocationID
			}
		}
		body := accumulate(evs, period, zone, time.Now())
		body.Homes = homesRead
		for i := range body.People {
			if h, ok := homes[body.People[i].EmployeeID]; ok {
				body.People[i].Role = deref(h.Role)
				body.People[i].HomeLocationID = h.HomeLocationID
				body.People[i].HomeVenue = deref(h.HomeVenue)
			}
		}
		body.Unattributable = unattributable
		body.Truncated = truncatedBy(len(rows))
		body.PracticeTaps = int(practice)
		body.Queried = true
		body.Period = period
		body.Zone = zone
		out.Report = body
		return nil
	})
	if err != nil {
		return ReportScreen{}, fmt.Errorf("ledger: hours: %w", err)
	}
	return out, nil
}

// rollHomes reads the job title and home venue of everybody the week's events name
// (M12-21, ListRollEmployees), inside a SAVEPOINT: a failure costs the other-venue marks
// and the titles -- reported as HomesRead.Failed -- and never the week's hours.
//
// AN EMPTY WEEK ASKS NOTHING AND IS MEASURED: with no person there is no title to read and
// no shift to judge, so "queried, nothing" is the true answer rather than "not queried".
func (r *Reader) rollHomes(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, evs []reportEvent) (map[uuid.UUID]store.ListRollEmployeesRow, HomesRead) {
	seen := map[uuid.UUID]bool{}
	ids := make([]uuid.UUID, 0)
	for _, e := range evs {
		if !seen[e.EmployeeID] {
			seen[e.EmployeeID] = true
			ids = append(ids, e.EmployeeID)
		}
	}
	if len(ids) == 0 {
		return nil, HomesRead{Queried: true}
	}
	homes := map[uuid.UUID]store.ListRollEmployeesRow{}
	if err := inSavepoint(ctx, tx, func(q *store.Queries) error {
		rows, err := q.ListRollEmployees(ctx, store.ListRollEmployeesParams{TenantID: tenantID, EmployeeIds: ids})
		if err != nil {
			return err
		}
		for _, row := range rows {
			homes[row.ID] = row
		}
		return nil
	}); err != nil {
		r.log.Error("ledger: could not read the roll's titles and home venues; the other-venue "+
			"figure will say it could not be read", "err", err, "tenant_id", tenantID)
		return nil, HomesRead{Failed: true}
	}
	return homes, HomesRead{Queried: true}
}

// readLimit and truncatedBy are the two halves of ONE invariant, and they are named
// functions rather than two expressions inside Hours because the invariant is between
// them rather than in either.
//
// 🔴 THE LIMIT MUST EXCEED THE CAP OR THE VERDICT IS DEAD CODE. If the query were asked
// for exactly ReportEventCap rows, len(rows) could never be greater than it, Truncated
// would be false forever, and a report that had silently lost half a payroll would
// present itself as a week's hours -- the precise failure ReportEventCap exists to
// refuse, arrived at by a one-character edit. An audit found this pair written inline
// with NO test of its own: the handler test sets Truncated by hand on a fake reader,
// and the database tests do not come near the cap (the busiest week measured reads
// 12 087 rows).
//
// THE COMPARISON IS STRICT. Exactly ReportEventCap rows is a complete read that happens
// to fill the budget; one more is the first row that could not fit. Both are pinned by
// TestReadLimit_MakesTruncationDETECTABLE, which is measured going red on `>=` and on a
// limit equal to the cap.
// ⚠️ IT TAKES THE CAP NOW. M6-11 shipped its own `AnomalyRowCap + 1` in anomaly.go --
// a second copy of one rule, each self-consistent and drifting invisibly, which is the
// class this file's own comments name three times. One function, two callers.
func readLimit(cap int32) int32 { return cap + 1 }

func truncatedBy(rowsRead int) bool { return rowsRead > ReportEventCap }

// WeekOf returns the Monday-to-Sunday week containing d, in the tenant's zone.
//
// THE BOUNDARIES ARE LOCAL MIDNIGHTS, WHICH IS THE WHOLE POINT (§6). Everything on
// disk is UTC, and a week bounded by UTC midnights would cut Malta's Monday an hour
// or two early -- so the Rusty Bar's 18:00-02:00 Sunday shift would land its checkout
// in the wrong week, and a "daily hours" column would move every night shift's tail
// onto the following day. AddDate is used rather than +168h because a DST week is not
// 168 hours long.
func WeekOf(d Date, zone *time.Location) Period {
	if zone == nil {
		zone = time.UTC
	}
	local := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, zone)
	// Go's Weekday is Sunday=0; the week here starts on Monday, so Sunday is the
	// SEVENTH day rather than the first. (int(Sunday)+6)%7 == 6 does that shift.
	back := (int(local.Weekday()) + 6) % 7
	from := local.AddDate(0, 0, -back)
	y, m, day := from.Date()
	return Period{
		From:     from,
		To:       from.AddDate(0, 0, ReportDays),
		FirstDay: Date{Year: y, Month: m, Day: day},
	}
}

// DayLabels are the period's local days, for the column headings.
func (p Period) DayLabels(zone *time.Location) []time.Time {
	if zone == nil {
		zone = time.UTC
	}
	start := time.Date(p.FirstDay.Year, p.FirstDay.Month, p.FirstDay.Day, 0, 0, 0, 0, zone)
	out := make([]time.Time, 0, p.Days())
	for i := 0; i < p.Days(); i++ {
		out = append(out, start.AddDate(0, 0, i))
	}
	return out
}

// reportEvent is one direction-carrying record, reduced to what the arithmetic needs.
//
// 🔴 THE FIELDS THAT ARE NOT HERE ARE THE POINT -- see the file comment. No Latitude,
// no Longitude, no SourceIP, no PolicyContext, no EnteredBy.
type reportEvent struct {
	EmployeeID   uuid.UUID
	Name         string
	At           time.Time
	Direction    string
	Verdict      string
	Review       string
	Manual       bool
	LocationID   uuid.UUID
	LocationName string
	// Home is the person's home venue (M12-21), nil when they have none or it was not
	// read. It decides DayMarks.Other and nothing else -- resolveShift never sees it (§5/Q17:
	// lateness follows the TAPPED venue), which is why it is attached after the row mapping.
	Home *uuid.UUID
	// Shift is the shift this tap is judged against, already resolved: the
	// DEPARTMENT's when it has one, otherwise the TAPPED location's (§5, Q17). nil
	// means lateness is not computed here, which is a different answer from "on time".
	Shift *tap.Shift
}

// events maps store rows onto the arithmetic's input.
//
// IT IS A NARROWING, NOT A COPY, exactly like record() and person(): the query is
// already free of coordinates, and this step additionally resolves the shift and
// drops the rows no chain can hold.
func events(rows []store.ListWorkedShiftEventsRow, zoneName string, sites siteVenues) (out []reportEvent, unattributable int) {
	out = make([]reportEvent, 0, len(rows))
	for _, row := range rows {
		if row.EmployeeID == nil || row.Type == nil {
			// 🔴 COUNTED, NOT DROPPED (§4.6). Migration 0005 lets a record exist with
			// no employee on it -- that is the shape a stolen plaque touched with no
			// cookie writes -- and no chain can hold one, because there is no "who" to
			// attribute the hours to. The number reaches the screen so the report never
			// quietly differs from the transactions list. (The NULL type branch is a
			// guard rather than a filter: the query's WHERE already excludes those.)
			unattributable++
			continue
		}
		e := reportEvent{
			EmployeeID:   *row.EmployeeID,
			Name:         deref(row.EmployeeName),
			At:           row.OccurredAt,
			Direction:    *row.Type,
			Verdict:      row.Verdict,
			Review:       deref(row.ReviewOutcome),
			Manual:       row.Channel == "manual",
			LocationName: deref(row.LocationName),
			Shift:        resolveShift(row, zoneName, sites),
		}
		switch {
		case row.LocationID != nil:
			e.LocationID = *row.LocationID
		case row.SiteLocationID != nil:
			// A holding peer's venue (K-30: location_id NULL): its own bucket, named from
			// holding_site_locations -- before API-1c it fell into the nameless uuid.Nil one.
			e.LocationID = *row.SiteLocationID
			e.LocationName = sites.name(*row.SiteLocationID)
		}
		out = append(out, e)
	}
	return out, unattributable
}

// resolveShift picks the shift a tap is judged against: the DEPARTMENT's when it has
// one, otherwise the TAPPED location's.
//
// 🔴 THE TAPPED LOCATION, NEVER THE PERSON'S HOME ONE (§5, Q17). Moving down the chain
// is normal -- somebody covering a shift at another venue works THAT venue's hours --
// and measuring them against their usual branch's opening time would invent lateness
// out of a rota change. The query joins the location on transactions.location_id,
// which is where the plaque is, so this function has no way to reach the other one.
//
// A HALF-FILLED SHIFT IS NO SHIFT. Migration 0002's shift_pair CHECK refuses a start
// without an end, so this cannot happen through the schema; guessing an end would
// invent a wall clock, which is the same reasoning internal/domain/checkin's shiftOf
// gives for the identical branch.
//
// M11 API-1c: a row at a holding PEER's venue has no location_id (K-30); its tapped location is
// site_location_id, and the shift is that venue's, read from holding_site_locations (sites). A
// peer the employer can no longer read (the membership ended) gives no shift -- no lateness is
// computed, which is the answer for "unknown", not "on time".
func resolveShift(row store.ListWorkedShiftEventsRow, zoneName string, sites siteVenues) *tap.Shift {
	if row.DepartmentShiftStart.Valid && row.DepartmentShiftEnd.Valid {
		return &tap.Shift{
			Start:     time.Duration(row.DepartmentShiftStart.Microseconds) * time.Microsecond,
			End:       time.Duration(row.DepartmentShiftEnd.Microseconds) * time.Microsecond,
			Overnight: row.DepartmentOvernight != nil && *row.DepartmentOvernight,
			Timezone:  zoneName,
		}
	}
	if row.SiteLocationID != nil {
		v, ok := sites[*row.SiteLocationID]
		if !ok || !v.ShiftStart.Valid || !v.ShiftEnd.Valid {
			return nil
		}
		return &tap.Shift{
			Start:     time.Duration(v.ShiftStart.Microseconds) * time.Microsecond,
			End:       time.Duration(v.ShiftEnd.Microseconds) * time.Microsecond,
			Overnight: v.Overnight,
			Timezone:  zoneName,
		}
	}
	if row.LocationShiftStart.Valid && row.LocationShiftEnd.Valid {
		return &tap.Shift{
			Start:     time.Duration(row.LocationShiftStart.Microseconds) * time.Microsecond,
			End:       time.Duration(row.LocationShiftEnd.Microseconds) * time.Microsecond,
			Overnight: row.LocationOvernight != nil && *row.LocationOvernight,
			Timezone:  zoneName,
		}
	}
	return nil
}

// endpointState is what ONE record contributes to an interval's fate.
//
// 🔴 A PENDING FLAG DOES NOT COUNT, AND THAT IS A DECISION WITH A MEASUREMENT BEHIND
// IT. §5 says `ok` counts toward worked hours and a flagged tap is "recorded AND
// queued for approval"; counting it anyway would make the approval step decorative
// for payroll, because the hours would already have been paid by the time a manager
// rejected it. Counting it only once APPROVED is what makes Q20's separation mean
// something: the engine's verdict is permanent (§4.3) and the human's decision is a
// different fact read through a join.
//
// 🔴 AND IT IS NOT DROPPED EITHER (§4.6). The alternative readings were measured on
// the development database rather than argued, as PROPORTIONS because the absolute
// counts grow on every `make test` run: of the direction-carrying, non-practice
// flagged records, 91.0% are undecided, 7.3% approved and 1.7% refused (measured
// 2026-08-10 over 28 084 such records; the query is in this file's test). Folding
// pending flags into the total would put nine tenths of the flagged hours into
// payroll with nobody having looked at them; hiding them silently would lose about a
// third of every check-in. So they are their own number with their own sentence, and
// the screen prints it beside the total rather than instead of it.
//
// 🔴 THE PAYABLE VERDICTS ARE NAMED, AND THE DEFAULT IS THE SAFE ONE. This function
// used to end `default: return HoursCounted` with a comment arguing that `ok` was the
// only verdict that could reach it — reject and ignored carry no direction, so the
// query's `type IS NOT NULL` excludes them. A security audit measured what that
// argument is worth: endpointState("reject",""), ("ignored","") and ("void","") all
// answered "counted", and a hand-inserted refused pair produced Totals.Worked = 8h,
// Shifts = 1.
//
// THE ARGUMENT WAS TRUE AND THE BARRIER WAS IN THE WRONG PLACE. What stops a
// directed reject today is a CODE INVARIANT — internal/domain/tap only assigns a
// direction on ok/flag (decide.go, the `if dec.Verdict == VerdictOK || ... VerdictFlag`
// gate) — and NOT a schema constraint: there is no CHECK saying "reject/ignored implies
// no direction"; migration 0005's transactions_ok_has_direction runs the other way
// only. M6-08 will write new rows to this table, and after that the `if` above is the
// whole of the guarantee.
//
// SO THE UNKNOWN CASE FAILS TO "awaiting" RATHER THAN TO "payable", which is exactly
// what §4.6 asks for in both directions at once: the record is not lost (its hours are
// reported, under "waiting for a decision") and it is not silently approved either.
// The word is imperfect for a verdict the ENGINE already refused — nobody is waiting
// on a reject — but the two available states are "in payroll" and "visible, not in
// payroll", and only the second is safe. Pinned by
// TestEndpointState_NeverPaysAVerdictItDoesNotKNOW, measured going red when the
// default is put back.
func endpointState(verdict, review string) HoursState {
	switch {
	case verdict == "flag" && review == "rejected":
		return HoursRefused
	case verdict == "flag" && review == "approved":
		return HoursCounted
	case verdict == "flag":
		return HoursAwaiting
	case verdict == "ok":
		return HoursCounted
	default:
		return HoursAwaiting
	}
}

// approvedFlag is an end that is payable only because a manager approved it: the
// engine flagged it and the review said yes (endpointState's second case).
func approvedFlag(e reportEvent) bool { return e.Verdict == "flag" && e.Review == "approved" }

// worse picks the state that keeps an interval OUT of the total. An interval has two
// ends and it is only payable if BOTH are.
func worse(a, b HoursState) HoursState {
	rank := func(s HoursState) int {
		switch s {
		case HoursRefused:
			return 3
		case HoursAwaiting:
			return 2
		default:
			return 1
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// accumulate walks the ordered event stream once and builds the whole report.
//
// 🔴 THE PAIRING IS SEQUENTIAL AND PER PERSON, NEVER PER CALENDAR DAY (§5). A day
// filter is the classic overnight-shift bug: it refuses to pair the Rusty Bar 02:10
// exit with the previous evening's 18:05 entry because they fall on different dates.
// The stream arrives ordered by (employee, occurred_at, id), so one forward pass with
// a single open slot per person is the whole algorithm.
//
// 🔴 TWO CHECK-INS IN A ROW LEAVE THE FIRST OPEN, WHICH IS THE HONEST READING RATHER
// THAN THE GENEROUS ONE. That shape exists on disk: before ADR 0008 a practice row
// could mask a real open check-in, so the next tap was written as `in` instead of
// `out`. Three readings were possible and two of them are wrong:
//
//	pair in1 with the out   the LONGEST interval, so it OVER-counts -- it charges
//	                        payroll for the gap nobody worked
//	pair both with the out  counts the same worked span twice
//	leave in1 open          this one. in2..out is the interval; in1 is listed as an
//	                        entry nobody closed, its hours are not guessed, and a
//	                        manager corrects it (Q18, M6-08)
//
// 🔴 AND THE CONSEQUENCE OF THAT CHOICE FOR M6-08, WHICH NOTHING SAID UNTIL THE WRITE
// PATH EXISTED. Keeping the LATEST `in` and (below) the EARLIEST `out` means the engine
// always builds the SHORTEST interval the records admit -- so a record APPENDED as a
// correction can make a shift shorter and can never make it longer. Measured, one
// person, truth 09:00-17:00 = 8h (internal/domain/ledger's correction_test.go):
//
//	a checkout typed too EARLY, corrected by a later one   3h  -> 3h   NOT applied
//	a checkout typed too LATE,  corrected by an earlier    11h -> 8h   applied
//	a check-in  typed too LATE, corrected by an earlier    7h  -> 7h   NOT applied
//	a check-in  typed too EARLY, corrected by a later      9h  -> 8h   applied
//
// The two that do not apply are the two that would RESTORE pay, and the stranded row
// is counted rather than lost (StartedEarlier or an OpenEntry) but is not labelled as
// a correction. Nothing changes here -- each reading above is defended on its own
// terms -- but internal/domain/manual.CorrectionsOnlyShorten and the confirmation
// screen both quote this measurement, so a change to the pairing has to move them too.
//
// A CHECKOUT WITH NO OPEN CHECK-IN is not an anomaly, it is the seam: the shift began
// before the period. It is counted (StartedEarlier) and its hours belong to the
// previous report, which is what "attribute an interval to the day it STARTED" means
// at the boundary.
//
// `now` is passed in rather than read, so the split between "on shift" and "forgot to
// tap out" is deterministic under test -- the same purity argument internal/domain/tap
// makes for Input.Now.
func accumulate(evs []reportEvent, p Period, zone *time.Location, now time.Time) Report {
	var out Report
	people := map[uuid.UUID]*PersonHours{}
	venues := map[uuid.UUID]*VenueHours{}

	// person returns the accumulator for an employee, creating it on first sight so
	// that somebody whose only record is an unclosed check-in still gets a row.
	person := func(e reportEvent) *PersonHours {
		ph, ok := people[e.EmployeeID]
		if !ok {
			ph = &PersonHours{
				EmployeeID: e.EmployeeID,
				Name:       e.Name,
				Daily:      make([]time.Duration, p.Days()),
				Marks:      make([]DayMarks, p.Days()),
			}
			people[e.EmployeeID] = ph
		}
		return ph
	}

	// openEntry records a check-in that nothing closed. It is only reported when the
	// check-in itself falls inside the period: one that opened earlier belongs to the
	// report that covers it, and repeating it here would show the same anomaly in
	// every week until somebody fixed it.
	openEntry := func(e reportEvent) {
		if e.At.Before(p.From) || !e.At.Before(p.To) {
			return
		}
		ph := person(e)
		ph.Open++
		out.Totals.Open++
		if d := dayIndex(e.At, p, zone); d >= 0 && d < len(ph.Marks) {
			ph.Marks[d].Open = true
		}
		out.Open = append(out.Open, OpenEntry{
			EmployeeID:   e.EmployeeID,
			EmployeeName: e.Name,
			LocationName: e.LocationName,
			At:           e.At,
			Stale:        now.Sub(e.At) > tap.StaleOpenIn,
			Manual:       e.Manual,
		})
	}

	// arrival records what a check-in says about turning up on time. It runs for every
	// attributed check-in, closed or not: lateness is a property of ARRIVING, and a
	// person who came in late and forgot to tap out was still late.
	arrival := func(e reportEvent) {
		if e.At.Before(p.From) || !e.At.Before(p.To) {
			return
		}
		ph := person(e)
		if e.Manual {
			ph.ManualArrivals++
			out.Totals.ManualArrivals++
			if d := dayIndex(e.At, p, zone); d >= 0 && d < len(ph.Marks) {
				ph.Marks[d].Manual = true
			}
		}
		if e.Shift == nil {
			ph.Unmeasured++
			return
		}
		late := tap.MinutesLate(e.At, *e.Shift)
		if late == nil {
			// The zone would not load. M4-05's rule is to decline rather than measure
			// against the wrong wall clock, and the screen says "not measured here".
			ph.Unmeasured++
			return
		}
		if *late > 0 {
			ph.LateShifts++
			ph.LateBy += time.Duration(*late) * time.Minute
		}
	}

	var open *reportEvent
	var openOwner uuid.UUID
	flush := func() {
		if open != nil {
			openEntry(*open)
			open = nil
		}
	}

	for i := range evs {
		e := evs[i]
		if open != nil && openOwner != e.EmployeeID {
			flush()
		}
		openOwner = e.EmployeeID
		if e.Direction == "in" {
			// A second `in` on top of an open one: the first was never closed.
			flush()
			arrival(e)
			open = &evs[i]
			continue
		}
		// A checkout.
		if open == nil {
			if !e.At.Before(p.From) && e.At.Before(p.To) {
				out.StartedEarlier++
			}
			continue
		}
		in := *open
		open = nil
		if in.At.Before(p.From) || !in.At.Before(p.To) {
			// The interval belongs to another period; its closing tap merely ends the
			// chain here.
			continue
		}
		worked := e.At.Sub(in.At)
		ph := person(in)
		if worked > closingTail {
			// 🔴 NOT A SHIFT. The engine annotates a closing tap this far from its
			// check-in as a probable forgotten checkout, and paying it would charge
			// payroll for the hours nobody worked between them. The opening tap is
			// reported as open instead, which is the state a manager has to correct.
			out.Totals.TooLong++
			openEntry(in)
			continue
		}
		state := worse(endpointState(in.Verdict, in.Review), endpointState(e.Verdict, e.Review))
		switch state {
		case HoursAwaiting:
			ph.Awaiting += worked
			out.Totals.Awaiting += worked
		case HoursRefused:
			ph.Refused += worked
			out.Totals.Refused += worked
		default:
			ph.Worked += worked
			ph.Shifts++
			out.Totals.Worked += worked
			out.Totals.Shifts++
			d := dayIndex(in.At, p, zone)
			if d >= 0 && d < len(ph.Daily) {
				ph.Daily[d] += worked
			}
			// THE SHIFT'S VENUE IS ITS CHECK-IN'S, the same key VenueHours uses.
			switch {
			case in.Home == nil || in.LocationID == uuid.Nil:
				out.Totals.OtherVenueUnknown++
			case in.LocationID != *in.Home:
				out.Totals.OtherVenue++
				if d >= 0 && d < len(ph.Marks) {
					ph.Marks[d].Other = true
				}
			}
			if approvedFlag(in) || approvedFlag(e) {
				out.Totals.Approved++
			}
			v, ok := venues[in.LocationID]
			if !ok {
				v = &VenueHours{LocationID: in.LocationID, Name: in.LocationName}
				venues[in.LocationID] = v
			}
			v.Worked += worked
			v.Shifts++
		}
	}
	flush()

	out.People = make([]PersonHours, 0, len(people))
	for _, ph := range people {
		out.People = append(out.People, *ph)
	}
	// BY NAME, THEN BY ID. A stable order is what makes two views of the same week
	// comparable, and a name is not unique -- two people called Maria Borg would
	// otherwise swap places between requests, because Go randomises map iteration.
	sort.Slice(out.People, func(i, j int) bool {
		if out.People[i].Name != out.People[j].Name {
			return out.People[i].Name < out.People[j].Name
		}
		return out.People[i].EmployeeID.String() < out.People[j].EmployeeID.String()
	})
	out.Venues = make([]VenueHours, 0, len(venues))
	for _, v := range venues {
		out.Venues = append(out.Venues, *v)
	}
	sort.Slice(out.Venues, func(i, j int) bool {
		if out.Venues[i].Name != out.Venues[j].Name {
			return out.Venues[i].Name < out.Venues[j].Name
		}
		return out.Venues[i].LocationID.String() < out.Venues[j].LocationID.String()
	})
	sort.Slice(out.Open, func(i, j int) bool {
		if !out.Open[i].At.Equal(out.Open[j].At) {
			return out.Open[i].At.Before(out.Open[j].At)
		}
		return out.Open[i].EmployeeName < out.Open[j].EmployeeName
	})
	return out
}

// dayIndex is which local day of the period an instant falls on, or -1 for outside.
//
// IT COMPARES CIVIL DATES RATHER THAN SUBTRACTING INSTANTS. A DST day is 23 or 25
// hours long, so dividing a difference by 24h puts the day either side of a transition
// into the wrong column -- and Malta transitions in March and October, i.e. inside two
// ordinary payroll weeks a year.
func dayIndex(at time.Time, p Period, zone *time.Location) int {
	if zone == nil {
		zone = time.UTC
	}
	y, m, d := at.In(zone).Date()
	// Both sides are rebuilt as UTC midnights purely as a day COUNTER: UTC has no
	// transitions, so the subtraction is exact whole days.
	a := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	b := time.Date(p.FirstDay.Year, p.FirstDay.Month, p.FirstDay.Day, 0, 0, 0, 0, time.UTC)
	return int(a.Sub(b) / (24 * time.Hour))
}
