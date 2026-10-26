package ledger

// anomaly.go -- the READ side of the ANOMALIES section (M6-11).
//
// 🔴 IT WRITES NOTHING, AND THAT IS THE PROPERTY THIS FILE IS FOR. Every one of the
// store calls below is a SELECT over `transactions`, which §4.3 makes
// immutable; there is no correction here, no acknowledgement, no "mark as reviewed"
// and no counter that a page view increments. Two independent proofs rather than a
// sentence: TestAnomalies_IssuesOnlySelects (internal/domain/ledger) parses THIS file,
// collects the sqlc queries it names and reads each one's first SQL keyword out of
// db/queries, and TestPanelAnomaliesDB_ReadingTheScreenWritesNothing
// (internal/handler) counts the rows of every table this path touches before and after
// a real read, with a positive control for the counter.
//
// ⚠️ EVERY TEST NAME IN THIS FILE IS grep-VERIFIED AGAINST THE REPOSITORY, AND THAT
// RULE EXISTS BECAUSE TWO OF THEM WERE WRONG. One citation dropped the `Panel` prefix
// from the handler test named above; the other named a §4.5 test that WAS NEVER
// WRITTEN, complete with an invented reason for its existence. A reviewer grepping
// either one for the guarantee behind it found nothing. The properties were real; the
// citations were not, and a wrong citation is worse than none — it sends the next
// reader looking for a net that is not there and stops them checking.
//
// THE TWO WITHDRAWN NAMES ARE DELIBERATELY NOT REPEATED HERE. Writing them again would
// put them back into every grep that goes looking for a test that exists.
//
// 🔴 §4.7 -- WHAT CROSSES THE DATABASE BOUNDARY IS AN INTEGER, A NAME, A BOOLEAN, ONE
// TIMESTAMP OR A ROW ID. This sentence used to stop at "integers, names and one
// timestamp", which left out the two booleans on the open-check-in row and the
// nullable ids on the two grouped listings -- the property was unchanged, but the
// inventory a reader would check it against was short by three kinds.
// Three of the signals below are derived from `policy_context`, which
// is §4.7-safe by construction (migration 0008 freezes a DISTANCE in metres, never a
// coordinate) -- and the jsonb value is still never selected: the aggregation happens
// inside the query, so what arrives here is a count.
//
// 🔴 WHAT THE TYPE WALL DOES AND DOES NOT CATCH, MEASURED RATHER THAN ASSERTED.
// TestAnomalyTypes_CarryNothingAFigureCouldHide dumps the whole type graph and refuses
// both the §4.7 name shapes and any field of an OPEN type (interface, map, []byte,
// float, a struct this section does not own). A security review then measured what
// that leaves: adding `t.gps_lat::text AS detail` to a query, letting sqlc emit
// `Detail string`, and carrying it through to the template DEFEATED it -- a string is
// exactly where a coordinate hides, and neither a name list nor a kind list can see
// one. The first draft of this paragraph said there was "nowhere for one to hide"; it
// was tried, and it hid.
//
// SO THE HONEST STATEMENT IS: the type wall refuses open containers, and it does NOT
// refuse a neutrally-named string. What stops that case is the BEHAVIOURAL test --
// TestPanelAnomaliesDB_NoCoordinateAndNoAddressReachesThePage (internal/handler) --
// which plants a real coordinate on every fixture row and sweeps the rendered page by
// literal AND by the SHAPE of a decimal degree, so a rounded or reformatted coordinate
// is caught too. That sweep is the load-bearing net for this class; the type graph is
// the cheap one that runs without a database.
//
// 🔴 THE REPORT DOES NOT COMMENT, IT SHOWS DATA (the M6-11 card's own sentence). No
// type below carries a severity, a score, a "suspicious" boolean or a rank. What it
// carries is a count and the denominator that count is out of, so every rate on the
// screen is one the reader can check. The one place a judgement could have crept in
// -- deciding which people are worth listing -- is a FILTER on whether a row has a
// non-zero signal at all, which is a property of the data rather than of the person.
//
// TENANT SCOPE (§4.5, belt + braces) is the package's, unchanged: every read runs
// inside db.(*DB).WithTenant so RLS applies, AND passes the same tenant id as an
// explicit predicate. The id is the caller's, taken from the signed panel session.
// What asserts the second half over these queries is this package's existing
// TestPanelQueries_CarryAnExplicitTenantPredicate (query_test.go), which DERIVES its
// subject list from the store calls this package makes -- so a query added here is
// covered because it is CALLED, with no list to forget to update. Verified:
// `go test ./internal/domain/ledger -run TestPanelQueries_CarryAnExplicitTenantPredicate -v`
// lists every one of them by name. (No count is written here: it was "eight" for a long time
// after it stopped being true -- M15-06's review counted thirteen calls -- and a number in a
// comment is a second representation of the code below it.)

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/store"
)

// AnomalyRowCap is how many rows each of this screen's listings returns.
//
// 🔴 IT IS A BOUND ON EACH LIST, AND EVERY TALLY ON THE PAGE IS COMPUTED WITHOUT IT.
// The window-wide counts come from CountAnomalySignals, which has no LIMIT at all, so
// a shortened list never shortens a number beside it -- the failure the reports
// section measured and named (a cap on a SUM is a wrong figure that looks like a right
// one).
//
// WHY 50 RATHER THAN THE REPORTS SECTION'S 100. This page holds SIX listings where
// that one holds three, and M6-03 measured what an unbounded listing costs: a filter
// bar once ate 96% of a page, and M6-07's own probe missed the biggest list on its
// page and shipped 560 KB. The page byte measurement for this section is in
// internal/handler/anomalies.go.
//
// ⚠️ A LIST THAT STOPS HERE SAYS SO. Each listing carries its own "…and more" flag,
// set by reading one row past the cap -- the same trick the paged sections use.
const AnomalyRowCap = 50

// TogetherWithin is how close two people's taps have to be to count as "together".
//
// IT IS A DECISION WITH §5 BEHIND IT RATHER THAN A ROUND NUMBER. §5 fixes the
// per-person debounce at 60 s and says explicitly that DIFFERENT people queueing at
// one plaque is normal; so the interesting quantity is not "close" but "close, every
// day". 20 s is roughly how long a queue of two takes at one plaque (the simulated day
// puts consecutive taps 1-3 s apart), and it is short enough that two people arriving
// in separate cars do not pair.
//
// ⚠️ IT IS NOT A THRESHOLD FOR AN ACCUSATION AND THE SCREEN PRINTS IT. A reader who
// knows the number can judge the list; one who does not is being asked to trust a
// constant they cannot see.
const TogetherWithin = 20 * time.Second

// TogetherMinDays is how many separate local days a pair has to appear on.
//
// 🔴 THIS IS THE WHOLE DISCRIMINATOR AND WITHOUT IT THE LIST IS NOISE. One burst is a
// queue, which §5 calls ordinary; the M6-11 card asks for people who tap together
// "every day". Two is the smallest number that can distinguish a habit from an
// occasion, and it is deliberately the low end: this screen exists to point at places
// to LOOK, so a threshold that hides a pair until the third day hides it for two days.
const TogetherMinDays = 2

// AnomalyTotals is the window in numbers. Every field is a count of RECORDS.
//
// 🔴 THE DENOMINATORS ARE FIELDS, NOT PROSE. Records and Judged are here so the screen
// can print "47 of 2 273" rather than "47", and Unanswerable is here because three of
// the signals below cannot be answered for a record that has no frozen policy context
// -- and "we cannot answer for these" is a different fact from "these are clean".
type AnomalyTotals struct {
	// Records is every record in the window, whatever its verdict or channel.
	Records int
	// Judged is the records for which the engine gathered address or distance
	// evidence at all. A manager-typed record has neither (§5: channel='manual'
	// carries no evidence of a physical touch), so it is not in this denominator.
	Judged int
	// Unanswerable is records with no frozen policy context. CounterGaps,
	// OutsideVenue and OtherVenue are UNANSWERABLE for those, and the screen says so
	// rather than counting them as clean.
	Unanswerable int
	// Answerable is Records minus Unanswerable: the base the three snapshot-derived
	// figures are out of.
	//
	// 🔴 IT IS A FIELD BECAUSE IT IS A DENOMINATOR, AND THE FIRST DRAFT USED THE WRONG
	// ONE. CounterGaps and OtherVenue were divided by Records and OutsideVenue by
	// Judged, which put every record the screen CANNOT ANSWER FOR into the base of the
	// three figures it cannot answer -- arithmetically, counting them as clean, in the
	// same docket as a sentence saying they are not. Measured on this section's own
	// fixture: 22 records, 4 without a snapshot, printing "1 of 22 after a counter gap"
	// where only 18 records could be asked the question at all.
	Answerable int
	// DistanceOnly is records the engine matched on distance alone: inside the
	// venue's radius, with no address match to corroborate it.
	//
	// THE NAME IS THE RECORD'S SHAPE RATHER THAN THE SENSOR'S, deliberately. What
	// the row holds is a boolean saying a reading fell inside a radius; the raw
	// position is never read here (§4.7), and a field whose name promised one would
	// be a field somebody later fills.
	DistanceOnly int
	// OutsideVenue is records where a reading WAS taken and it fell outside the
	// venue's radius.
	//
	// ⚠️ IT IS NOT "THE ADDRESS MATCHED BUT THE POSITION DID NOT", which is how the
	// M6-11 card describes this signal. internal/domain/tap's gpsFacts sets the flag
	// from the distance ALONE -- it says nothing about the address -- so a screen
	// that used the card's words would be describing a mechanism the product does not
	// have. The card is corrected; the number beside it is what the card meant.
	OutsideVenue int
	// OutsideVenueOnNetwork is the subset of OutsideVenue that ALSO matched the
	// venue's registered address range: a reading from outside, taken by something
	// answering from inside. That combination is the "device left on the premises"
	// shape, and it is the one the card was reaching for.
	OutsideVenueOnNetwork int
	// CounterGaps is records whose plaque counter jumped by more than one -- the only
	// live trace of somebody collecting tap URLs (A1 / Q21).
	CounterGaps int
	// Partner is records taken at a holding PEER's venue (M11 API-1c). They are in Records and
	// in the non-evidence figures (CounterGaps -- K-1i --, OtherVenue, Unanswerable), and OUT of
	// every evidence figure (Judged, DistanceOnly, OutsideVenue, OutsideVenueOnNetwork): those
	// would describe the peer's network and coordinate (K-39; the second review's ORTA-A).
	Partner int
	// AnswerableOwn is Answerable less the partner records that carry a snapshot: the base
	// OutsideVenue is out of, now that it counts the business's own venues only.
	AnswerableOwn int
	// OtherVenue is records taken at a venue other than the person's own. §5 treats
	// moving down the chain as ordinary; the number is here because the ABSENCE of it
	// is half of the card's Y-D signal, and because a change in it is worth seeing.
	OtherVenue int
	// Training is practice taps. They are counted in every signal above, because a
	// training tap is a real physical touch; they are excluded from ONE thing, the
	// open check-in list.
	Training int
	// ManagerTyped is records a manager entered by hand (M6-08). They carry no tap
	// evidence at all, which is why they are outside Judged.
	ManagerTyped int
}

// AnomalyPerson is one employee's signals in the window.
type AnomalyPerson struct {
	EmployeeID uuid.UUID
	// Name is "" when the record names an employee the roster no longer holds. The
	// screen prints a word rather than a blank cell, for the reason ledger.Person
	// gives: an empty cell reads as "nobody".
	Name         string
	Records      int
	DistanceOnly int
	OutsideVenue int
	CounterGaps  int
	OtherVenue   int
}

// AnomalyVenue is one location's signals.
//
// EVERY VENUE WITH TAPS IS HERE, INCLUDING THE CLEAN ONES, and that is the difference
// from AnomalyPerson. The question this breakdown answers is "is one branch different
// from the others", which needs the others.
type AnomalyVenue struct {
	LocationID uuid.UUID
	Name       string
	Records    int
	Judged     int
	// Answerable is this venue's second base: how many of its records carry a policy
	// snapshot. OutsideVenue is out of THIS, DistanceOnly is out of Judged -- the two
	// figures on one venue's row come from different populations and the row says so.
	Answerable   int
	DistanceOnly int
	OutsideVenue int
	// Both, NetworkOnly and Neither are, with DistanceOnly, the four-way split of Judged the
	// Odd shapes "evidence mix" bar draws (M12-22). Each is a count of records, out of Judged:
	//
	//	Both          the address range matched and the reading fell inside the radius
	//	NetworkOnly   the address range matched; the reading did not, or none was taken
	//	DistanceOnly  the reading fell inside the radius; the address range did not match
	//	Neither       one of the two was assessed and neither matched
	//
	// THE NAMES ARE THE RECORD'S SHAPE, NOT THE SENSOR'S (DistanceOnly's reason): what the row
	// holds is two booleans; no address and no distance reaches this type (§4.7). The exact
	// predicates, and the one shape none of the four counts, are in db/queries/anomalies.sql
	// (ListAnomalyVenues) -- Unsplit is that shape's count.
	Both        int
	NetworkOnly int
	Neither     int
	// Partner marks ANOTHER holding member's venue where this business's staff tapped (M11
	// API-1c, ADR 0027 §9): Name is the peer's venue, and DistanceOnly / OutsideVenue / Both /
	// NetworkOnly / Neither are zero by construction -- the query counts the evidence figures at
	// this business's own venues only (K-39: the peer's network and coordinate are not this
	// panel's to describe). Such a row gets the label, never a bar.
	Partner bool
}

// Unsplit is the part of Judged the four-way split does not place: a record whose address
// range was not assessed while its reading fell inside the radius (ip_match NULL, gps_match
// true). No writer produces it today -- the tap path sets both booleans, the manual path
// neither -- so it is 0 in every measured week; it is a method rather than an assumption so a
// bar drawn out of Judged can show the remainder as "not split" instead of stretching a
// segment over it. A partner venue's row has no split at all and answers 0.
func (v AnomalyVenue) Unsplit() int {
	if v.Partner {
		return 0
	}
	return v.Judged - v.Both - v.NetworkOnly - v.DistanceOnly - v.Neither
}

// AnomalyLostPlaque is one of THIS business's lost or retired plaques that a holding peer's
// employee touched (M11 API-1c, ADR 0027 §5 and §9): the peer refused and recorded the tap in its
// own books, and this row -- from this business's projection -- is the only trace here. Refusals
// counts what reached this business (repeats of one URL within an hour are one); Last is the
// SERVER's clock (site_punches.created_at), never a phone's claim.
type AnomalyLostPlaque struct {
	TagUID       string
	LocationName string
	Refusals     int
	Last         time.Time
}

// AnomalyPlaque is one wall tag's counter gaps.
type AnomalyPlaque struct {
	// TagUID is the plaque's own identifier. It is NOT a secret -- it travels in the
	// tap URL and is printed on the plaque itself (skill tappa-brand) -- and it is the
	// one thing a manager can act on: they walk to that door.
	TagUID       string
	LocationName string
	Records      int
	CounterGaps  int
	// LargestGap is the biggest single jump rather than their sum. Ten taps each one
	// short is a busy plaque; one tap forty short is a different event.
	LargestGap int

	// The counters around LargestGap (M12-24, "#from -> #to · between … and …"). JumpState says
	// which of the four states the five fields below are in; outside JumpRead they are zero and
	// make no claim.
	JumpState CounterJumpState
	// JumpTo is the counter of the read that showed the gap; JumpFrom = JumpTo - LargestGap - 1,
	// the counter the atomic advance compared it against (db/queries/tags.sql).
	JumpFrom, JumpTo int
	// JumpAt is the declared occurred_at of the JumpTo read.
	JumpAt time.Time
	// JumpPreviousAt is the declared occurred_at of the read whose counter is JumpFrom, or nil
	// when that read is not on this business's record within CounterJumpLookback (an activation
	// tap writes no row, a holding peer's read is in the peer's books) -- never another read's
	// time beside JumpFrom.
	JumpPreviousAt *time.Time
}

// CounterJumpState is the state of a plaque's from -> to counters (M12-24): the P3 tri-state
// (not read / failed / read) plus the one a holding adds.
type CounterJumpState string

const (
	// CounterJumpNotRead: not asked (the zero value) -- the screen claims nothing.
	CounterJumpNotRead CounterJumpState = ""
	// CounterJumpFailed: the read failed; the rest of the page stands ("could not be read").
	CounterJumpFailed CounterJumpState = "failed"
	// CounterJumpRead: JumpFrom, JumpTo and JumpAt are measured (JumpPreviousAt may be nil).
	CounterJumpRead CounterJumpState = "read"
	// CounterJumpElsewhere: the largest gap on the row was revealed by a holding peer's staff on
	// this business's plaque (the projection, K-1i) -- its counters are not in this business's
	// records, so none are printed.
	CounterJumpElsewhere CounterJumpState = "elsewhere"
)

// CounterJumpLookback is how far before the week the read just before a gap is looked for.
//
// A BOUND, BECAUSE THE LOOKUP OTHERWISE HAS NONE: a plaque whose gap is on its first recorded
// read (an installer's test reads, then the first real tap) has no earlier read, and the
// search for one would walk the tenant's whole history -- once per listed plaque. 31 days keeps
// a plaque used weekly well inside it; past it the screen says the earlier read is not on
// record rather than guessing. The bound also caps the join's hash side (db/queries/anomalies.sql,
// ListAnomalyPlaqueJumps, measured with EXPLAIN ANALYZE).
const CounterJumpLookback = 31 * 24 * time.Hour

// AnomalyPair is two people whose taps land together, repeatedly.
//
// 🔴 IT CARRIES NO WORD FOR WHAT IT MIGHT MEAN. The card is explicit: "not an
// accusation, a place to look". Two colleagues who share a lift to work produce this
// row, and so does one person carrying two phones; nothing in the data tells them
// apart, so nothing here pretends to.
type AnomalyPair struct {
	First  string
	Second string
	Venue  string
	// Together is how many times the two were adjacent within TogetherWithin.
	Together int
	// Days is how many separate local days that happened on.
	Days int
	// ClosestSeconds is the smallest gap observed between the two.
	ClosestSeconds int
	// TogetherOn says, for each local day of the week -- index i is Period.DayLabels' i-th --,
	// whether the two were together that day (M12-23, the week's dots). It comes from the same
	// query and the same zone as Days, so its true count is Days; a nil slice is a pair read
	// before this field existed (a fake), never "on no day".
	TogetherOn []bool
}

// AnomalyOpen is a check-in with no later checkout.
type AnomalyOpen struct {
	// ID is the check-in's own row id (transactions.id, M15-18): what the "claim pending" mark is
	// bound by -- an instant alone does not pick one row.
	ID           uuid.UUID
	EmployeeName string
	LocationName string
	At           time.Time
	// ManagerTyped marks an entry somebody typed rather than tapped.
	ManagerTyped bool
	// WasMaskable means a practice row of this person's sits AFTER this check-in --
	// the shape ADR 0008 fixed, and one an ordinary new starter also produces. It is
	// a hint about where to look, and the screen says exactly that.
	WasMaskable bool
	// ClaimPending: the person sent a claim of this check-in's missing checkout and nobody has
	// decided it yet (M15-18; ADR 0032 §C.4) -- bound by ID to punch_claims.anchor_transaction_id.
	ClaimPending bool
}

// AnomalyInMissing is a checkout with no open check-in for it to close -- ADR 0032 §A.6's (b) and
// (c), the "check-in missing" list (M15-06; ListCheckoutsWithoutCheckIn). Like an
// open check-in it is a record a manager completes, and nothing here completes it.
type AnomalyInMissing struct {
	// ID is the checkout's own row id (M15-18), AnomalyOpen.ID's reason.
	ID           uuid.UUID
	EmployeeName string
	LocationName string
	At           time.Time
	// AfterEarlierDay is (b): the record before it is a check-in of an EARLIER business day, which
	// is itself on the open list. False is (c): nothing, or a checkout tapped before it, is before
	// it.
	AfterEarlierDay bool
	// ClaimPending is AnomalyOpen.ClaimPending's twin: an undecided claim of the missing check-in.
	ClaimPending bool
}

// MaskedOpenTally is the retrospective half of the ADR 0008 devir note: the week's check-ins a
// practice row of the same person sits after.
//
// 🔴 UNTIL M15-05 ITS SECOND NUMBER WAS THE ONE THE OPEN LIST COULD NOT PRODUCE: "open" was
// `NOT EXISTS (an out after it)`, which one `out` satisfied for every older unclosed check-in at
// once, so a masked entry left the list silently on the person's next checkout. The list now
// reads ADR 0032 §A.6's definition -- an entry is open unless its OWN next record closes it --
// and the record a mask produced is an `in`, which closes nothing; so a masked entry stays on
// the list, and the two numbers split by that same test (CountMaskedOpenCheckIns).
type MaskedOpenTally struct {
	// StillOpen is the ones nothing closes: all of them are on the open list above.
	StillOpen int
	// ClosedByItsCheckout is the ones whose own next record is a checkout that closes them --
	// an ordinary pair, which the training tap after it did not hide.
	ClosedByItsCheckout int
}

// AnomalyPolicyRule is how many records one rule decided.
type AnomalyPolicyRule struct {
	// SID is the rule's stable id (M3-07), or "" for a record with no policy
	// decision at all -- everything written before M3-07, and every manager-typed row.
	// "" too for the Partner bucket.
	SID string
	// Partner is the one bucket of every record at a holding peer's venue, whatever rule
	// decided it (M11 API-1c, K-39: the rule's name says what the peer's evidence showed).
	Partner bool
	// Layer is guardrail / baseline / tenant, or "" with SID.
	Layer   string
	Records int
}

// Anomalies is one window of signals.
type Anomalies struct {
	// Queried is set ONLY after the database has answered.
	//
	// 🔴 IT IS THE ANTI-FABRICATION FLAG the other screens carry, and it matters more
	// here than anywhere else in the panel: a zero Anomalies renders as "nothing odd
	// this week", which is the single most damaging sentence this product could say
	// without having measured it.
	Queried bool
	Period  Period
	Zone    *time.Location

	Totals  AnomalyTotals
	People  []AnomalyPerson
	Venues  []AnomalyVenue
	Plaques []AnomalyPlaque
	Pairs   []AnomalyPair
	Open    []AnomalyOpen
	// InMissing is the "check-in missing" list (M15-06), beside Open: the week's checkouts with no
	// open check-in for them to close.
	InMissing []AnomalyInMissing
	Masked    MaskedOpenTally
	Policies  []AnomalyPolicyRule
	// LostPlaques are this business's lost plaques a holding peer's staff touched this week (M11
	// API-1c). Empty for a business in no holding.
	LostPlaques []AnomalyLostPlaque

	// The five "…and there are more" flags. Each is true when its listing hit
	// AnomalyRowCap, which is detected by asking for one row more than can be used --
	// the same shape ledger.readLimit uses for the report's truncation verdict.
	MorePeople  bool
	MoreVenues  bool
	MorePlaques bool
	MorePairs   bool
	MoreOpen    bool
	// MoreInMissing is its twin for the "check-in missing" list.
	MoreInMissing bool
	// MorePolicies is its twin for the rule breakdown.
	MorePolicies bool
	MoreLost     bool

	// TogetherWithinSeconds and TogetherMinDays are the pair listing's own thresholds,
	// carried to the screen so it can print them. A list filtered by a constant the
	// reader cannot see is a list they have to take on trust.
	TogetherWithinSeconds int
	TogetherMinDays       int
}

// AnomalyScreen is Anomalies plus what only the full document needs.
type AnomalyScreen struct {
	Anomalies
	TenantName string
}

// Anomalies loads one week of signals.
//
// 🔴 THE FILTER TYPE IS ReportFilter, DELIBERATELY REUSED RATHER THAN CLONED. "Any day
// inside the week I want" is one question, and it already has a type, a parser and a
// week-boundary function with a DST argument behind it (WeekOf). A second identical
// struct here would be the "second representation" shape this repository keeps paying
// for, with the drift invisible because each copy is self-consistent.
//
// ONE TRANSACTION, EVERY QUERY IN IT -- the shape Hours and Screen use. The clock has to
// be read before the week's boundaries can be computed, and running the rest in the
// same transaction means their answers cannot disagree about which records existed.
//
// ⚠️ WHAT THOSE QUERIES IN ONE TRANSACTION COST is measured rather than assumed; the
// figures, their tenant and their row counts are in internal/handler/anomalies.go.
func (r *Reader) Anomalies(ctx context.Context, tenantID uuid.UUID, f ReportFilter) (AnomalyScreen, error) {
	var out AnomalyScreen
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		out.TenantName = clock.Name
		zone := r.loadZone(clock.Timezone)

		day := f.Day
		if day.Zero() {
			now := time.Now().In(zone)
			day = Date{Year: now.Year(), Month: now.Month(), Day: now.Day()}
		}
		period := WeekOf(day, zone)

		body := Anomalies{
			Period:                period,
			Zone:                  zone,
			TogetherWithinSeconds: int(TogetherWithin / time.Second),
			TogetherMinDays:       TogetherMinDays,
		}

		signals, err := q.CountAnomalySignals(ctx, store.CountAnomalySignalsParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
		})
		if err != nil {
			return fmt.Errorf("count anomaly signals: %w", err)
		}
		body.Totals = AnomalyTotals{
			Records:      int(signals.Records),
			Judged:       int(signals.Judged),
			Unanswerable: int(signals.Unanswerable),
			// DERIVED HERE AND NOWHERE ELSE, so the two numbers cannot disagree: a
			// second `count(*) FILTER (WHERE policy_context IS NOT NULL)` beside the
			// one that produces Unanswerable would be two representations of one split.
			Answerable:            int(signals.Records) - int(signals.Unanswerable),
			AnswerableOwn:         int(signals.Records) - int(signals.Unanswerable) - int(signals.PartnerAnswerable),
			DistanceOnly:          int(signals.DistanceOnly),
			OutsideVenue:          int(signals.OutsideVenue),
			OutsideVenueOnNetwork: int(signals.OutsideVenueOnNetwork),
			CounterGaps:           int(signals.CounterGaps),
			OtherVenue:            int(signals.OtherVenue),
			Partner:               int(signals.PartnerRecords),
			Training:              int(signals.Training),
			ManagerTyped:          int(signals.ManagerTyped),
		}

		people, err := q.ListAnomalyPeople(ctx, store.ListAnomalyPeopleParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			RowLimit: anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list anomaly people: %w", err)
		}
		people, body.MorePeople = trimAnomalyRows(people)
		body.People = make([]AnomalyPerson, 0, len(people))
		for _, p := range people {
			row := AnomalyPerson{
				Name:         deref(p.EmployeeName),
				Records:      int(p.Records),
				DistanceOnly: int(p.DistanceOnly),
				OutsideVenue: int(p.OutsideVenue),
				CounterGaps:  int(p.CounterGaps),
				OtherVenue:   int(p.OtherVenue),
			}
			if p.EmployeeID != nil {
				row.EmployeeID = *p.EmployeeID
			}
			body.People = append(body.People, row)
		}

		venues, err := q.ListAnomalyVenues(ctx, store.ListAnomalyVenuesParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			RowLimit: anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list anomaly venues: %w", err)
		}
		venues, body.MoreVenues = trimAnomalyRows(venues)
		body.Venues = make([]AnomalyVenue, 0, len(venues))
		for _, v := range venues {
			body.Venues = append(body.Venues, AnomalyVenue{
				LocationID:   v.LocationID,
				Name:         deref(v.LocationName),
				Records:      int(v.Records),
				Judged:       int(v.Judged),
				Answerable:   int(v.Answerable),
				DistanceOnly: int(v.DistanceOnly),
				OutsideVenue: int(v.OutsideVenue),
				Both:         int(v.BothMatched),
				NetworkOnly:  int(v.NetworkOnly),
				Neither:      int(v.NeitherMatched),
				// A venue of this business always has a name (the join's key is a foreign key);
				// one with none is a peer's -- the group the query made from site_location_id.
				Partner: v.LocationName == nil,
			})
		}

		plaques, err := q.ListAnomalyPlaques(ctx, store.ListAnomalyPlaquesParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			RowLimit: anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list anomaly plaques: %w", err)
		}
		plaques, body.MorePlaques = trimAnomalyRows(plaques)
		body.Plaques = make([]AnomalyPlaque, 0, len(plaques))
		for _, p := range plaques {
			body.Plaques = append(body.Plaques, AnomalyPlaque{
				TagUID:       deref(p.TagUid),
				LocationName: deref(p.LocationName),
				Records:      int(p.Records),
				CounterGaps:  int(p.CounterGaps),
				LargestGap:   int(p.LargestGap),
			})
		}
		// M11 API-1c: the gaps a holding peer's staff revealed on THIS business's plaques (K-1i,
		// from the projection -- ADR 0027 §9). Added to the plaque's own row; a business in no
		// holding reads an empty list and the listing above is unchanged.
		sitePlaques, err := q.ListSitePlaqueGaps(ctx, store.ListSitePlaqueGapsParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			RowLimit: anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list the site's plaque gaps: %w", err)
		}
		var moreSite bool
		sitePlaques, moreSite = trimAnomalyRows(sitePlaques)
		if len(sitePlaques) > 0 {
			body.Plaques, body.MorePlaques = mergeSitePlaques(body.Plaques, sitePlaques, body.MorePlaques || moreSite)
		}

		// M12-24: the counters around each listed plaque's largest gap, in a savepoint -- a failed
		// read leaves the gap list standing and says "could not be read" for the counters.
		if len(body.Plaques) > 0 {
			if err := inSavepoint(ctx, tx, func(q *store.Queries) error {
				return readPlaqueJumps(ctx, q, tenantID, period, body.Plaques)
			}); err != nil {
				for i := range body.Plaques {
					body.Plaques[i].JumpState, body.Plaques[i].JumpFrom, body.Plaques[i].JumpTo = CounterJumpFailed, 0, 0
					body.Plaques[i].JumpAt, body.Plaques[i].JumpPreviousAt = time.Time{}, nil
				}
				r.log.Error("ledger: could not read the plaque counters around the largest gaps; the gap list "+
					"stands without them", "err", err, "tenant_id", tenantID)
			}
		}

		pairs, err := q.ListTapsTakenTogether(ctx, store.ListTapsTakenTogetherParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			// THE RESOLVED ZONE'S NAME, NOT THE STORED ONE. loadZone has already
			// fallen back to UTC if the tenant's string does not load, and passing the
			// unresolved string would let Postgres bucket days in a zone Go refused --
			// two different day boundaries on one page.
			Zone:          zone.String(),
			WithinSeconds: TogetherWithin.Seconds(),
			MinDays:       TogetherMinDays,
			RowLimit:      anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list taps taken together: %w", err)
		}
		pairs, body.MorePairs = trimAnomalyRows(pairs)
		body.Pairs = make([]AnomalyPair, 0, len(pairs))
		pairSites := make([]*uuid.UUID, 0, len(pairs))
		for _, p := range pairs {
			body.Pairs = append(body.Pairs, AnomalyPair{
				First:          deref(p.FirstEmployeeName),
				Second:         deref(p.SecondEmployeeName),
				Venue:          deref(p.LocationName),
				Together:       int(p.Together),
				Days:           int(p.Days),
				ClosestSeconds: int(p.ClosestSeconds),
				TogetherOn:     togetherOn(p.TogetherDays, period, zone),
			})
			var site *uuid.UUID
			if p.LocationName == nil {
				id := p.LocationID
				site = &id
			}
			pairSites = append(pairSites, site)
		}

		open, err := q.ListOpenCheckIns(ctx, store.ListOpenCheckInsParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			RowLimit: anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list open check-ins: %w", err)
		}
		open, body.MoreOpen = trimAnomalyRows(open)
		body.Open = make([]AnomalyOpen, 0, len(open))
		for _, o := range open {
			body.Open = append(body.Open, AnomalyOpen{
				ID:           o.ID,
				EmployeeName: deref(o.EmployeeName),
				LocationName: deref(o.LocationName),
				At:           o.OccurredAt,
				ManagerTyped: o.ManagerTyped,
				WasMaskable:  o.WasMaskable,
			})
		}

		// M15-06: the "check-in missing" list -- the checkouts of the week with no open check-in
		// for them to close (ADR 0032 §A.6, (b) and (c); never (d) or (e)). The open list's
		// shape: the window, the cap read one past, oldest first.
		inMissing, err := q.ListCheckoutsWithoutCheckIn(ctx, store.ListCheckoutsWithoutCheckInParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			RowLimit: anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list checkouts without a check-in: %w", err)
		}
		inMissing, body.MoreInMissing = trimAnomalyRows(inMissing)
		body.InMissing = make([]AnomalyInMissing, 0, len(inMissing))
		for _, o := range inMissing {
			body.InMissing = append(body.InMissing, AnomalyInMissing{
				ID:              o.ID,
				EmployeeName:    deref(o.EmployeeName),
				LocationName:    deref(o.LocationName),
				At:              o.OccurredAt,
				AfterEarlierDay: o.AfterEarlierDay,
			})
		}

		// M15-18: the "claim pending" mark on the two lists above -- one read over the listed rows'
		// ids, and none when both lists are empty.
		if err := markPendingClaims(ctx, q, tenantID, body.Open, body.InMissing); err != nil {
			return err
		}

		// M11 API-1c: name the peers' venues the listings above reached (ADR 0027 §9) -- one
		// holding_site_locations read, and none when no row names a peer's venue.
		need := false
		for _, v := range body.Venues {
			need = need || v.Partner
		}
		for _, id := range pairSites {
			need = need || id != nil
		}
		for _, o := range open {
			need = need || o.SiteLocationID != nil
		}
		for _, o := range inMissing {
			need = need || o.SiteLocationID != nil
		}
		venuesOfPeers, err := siteVenuesFor(ctx, tx, need)
		if err != nil {
			return err
		}
		for i := range body.Venues {
			if body.Venues[i].Partner {
				body.Venues[i].Name = venuesOfPeers.name(body.Venues[i].LocationID)
			}
		}
		for i, id := range pairSites {
			if id != nil {
				body.Pairs[i].Venue = venuesOfPeers.name(*id)
			}
		}
		for i, o := range open {
			if o.SiteLocationID != nil {
				body.Open[i].LocationName = venuesOfPeers.name(*o.SiteLocationID)
			}
		}
		for i, o := range inMissing {
			if o.SiteLocationID != nil {
				body.InMissing[i].LocationName = venuesOfPeers.name(*o.SiteLocationID)
			}
		}

		// M11 API-1c: this business's lost plaques a holding peer's staff touched (ADR 0027 §9),
		// by the server's clock. A business in no holding reads an empty list.
		lost, err := q.ListSiteLostPlaqueAlerts(ctx, store.ListSiteLostPlaqueAlertsParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			RowLimit: anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list the site's lost plaque alerts: %w", err)
		}
		lost, body.MoreLost = trimAnomalyRows(lost)
		body.LostPlaques = make([]AnomalyLostPlaque, 0, len(lost))
		for _, l := range lost {
			body.LostPlaques = append(body.LostPlaques, AnomalyLostPlaque{
				TagUID:       l.TagUid,
				LocationName: deref(l.LocationName),
				Refusals:     int(l.Refusals),
				Last:         l.LastAt,
			})
		}

		masked, err := q.CountMaskedOpenCheckIns(ctx, store.CountMaskedOpenCheckInsParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
		})
		if err != nil {
			return fmt.Errorf("count masked open check-ins: %w", err)
		}
		body.Masked = MaskedOpenTally{
			StillOpen:           int(masked.StillOpen),
			ClosedByItsCheckout: int(masked.ClosedByItsCheckout),
		}

		rules, err := q.ListAnomalyPolicySids(ctx, store.ListAnomalyPolicySidsParams{
			TenantID: tenantID,
			FromAt:   period.From,
			ToAt:     period.To,
			RowLimit: anomalyReadLimit(),
		})
		if err != nil {
			return fmt.Errorf("list anomaly policy sids: %w", err)
		}
		rules, body.MorePolicies = trimAnomalyRows(rules)
		body.Policies = make([]AnomalyPolicyRule, 0, len(rules)+1)
		for _, s := range rules {
			body.Policies = append(body.Policies, AnomalyPolicyRule{
				SID:     deref(s.MatchedSid),
				Layer:   deref(s.PolicyLayer),
				Records: int(s.Records),
			})
		}
		// M11 API-1c (K-39): every row at a peer's venue is one bucket with no name, ALWAYS LAST,
		// counted from the totals -- the query leaves those rows out, so neither a rule's name nor
		// the bucket's place in an order by count and sid can say what the peer's evidence showed.
		if body.Totals.Partner > 0 {
			body.Policies = append(body.Policies, AnomalyPolicyRule{Partner: true, Records: body.Totals.Partner})
		}

		body.Queried = true
		out.Anomalies = body
		return nil
	})
	if err != nil {
		return AnomalyScreen{}, fmt.Errorf("ledger: anomalies: %w", err)
	}
	return out, nil
}

// readPlaqueJumps fills the counters around each plaque's largest gap (M12-24) from
// ListAnomalyPlaqueJumps. A row of this business's own records whose gap is the row's
// LargestGap is CounterJumpRead; a row with none -- or with a smaller one, because the
// projection's gap (a holding peer's tap on this plaque) is the larger -- is
// CounterJumpElsewhere. The previous read's time is kept only when its counter IS JumpFrom.
func readPlaqueJumps(ctx context.Context, q *store.Queries, tenantID uuid.UUID, period Period, plaques []AnomalyPlaque) error {
	uids := make([]string, 0, len(plaques))
	for _, p := range plaques {
		uids = append(uids, p.TagUID)
	}
	rows, err := q.ListAnomalyPlaqueJumps(ctx, store.ListAnomalyPlaqueJumpsParams{
		TenantID:     tenantID,
		LookbackFrom: period.From.Add(-CounterJumpLookback),
		FromAt:       period.From,
		ToAt:         period.To,
		TagUids:      uids,
	})
	if err != nil {
		return fmt.Errorf("list the plaque counters around the largest gaps: %w", err)
	}
	type key struct{ uid, venue string }
	at := make(map[key]store.ListAnomalyPlaqueJumpsRow, len(rows))
	for _, r := range rows {
		at[key{r.TagUid, deref(r.LocationName)}] = r
	}
	for i := range plaques {
		p := &plaques[i]
		r, ok := at[key{p.TagUID, p.LocationName}]
		if !ok || r.RevealedCtr == nil || int(r.Gap) != p.LargestGap {
			p.JumpState = CounterJumpElsewhere
			continue
		}
		p.JumpState = CounterJumpRead
		p.JumpTo = int(*r.RevealedCtr)
		p.JumpFrom = p.JumpTo - p.LargestGap - 1
		p.JumpAt = r.RevealedAt
		if r.PreviousCtr != nil && int(*r.PreviousCtr) == p.JumpFrom && r.PreviousAt != nil {
			when := *r.PreviousAt
			p.JumpPreviousAt = &when
		}
	}
	return nil
}

// togetherOn maps a pair's local days onto the week: one boolean per Period.DayLabels entry.
//
// THE DAYS ARE MATCHED BY CALENDAR DATE, NOT BY INSTANT. Postgres bucketed each tap into a
// local date in the resolved zone (`occurred_at AT TIME ZONE zone`) and returns a `date`, which
// pgx reads as midnight UTC of that date; DayLabels are midnights in the zone. Comparing the
// instants would shift every day by the zone's offset -- comparing year, month and day does not.
// A date outside the week (none can be: the window is the week's own bounds) sets no day.
func togetherOn(days []pgtype.Date, period Period, zone *time.Location) []bool {
	labels := period.DayLabels(zone)
	out := make([]bool, len(labels))
	for _, d := range days {
		if !d.Valid {
			continue
		}
		y, m, dd := d.Time.Date()
		for i, l := range labels {
			if ly, lm, ld := l.Date(); ly == y && lm == m && ld == dd {
				out[i] = true
			}
		}
	}
	return out
}

// anomalyReadLimit and trimAnomalyRows are the two halves of ONE invariant, written as
// named functions because the invariant lives BETWEEN them rather than in either.
//
// 🔴 THE +1 IS SHARED WITH report.go NOW, WHICH IT WAS NOT. This file shipped its own
// `AnomalyRowCap + 1` beside report.go's `ReportEventCap + 1` -- two copies of one
// rule, each self-consistent, drifting invisibly, which is the class report.go's own
// comments name three times. readLimit takes the cap.
//
// 🔴 THE LIMIT MUST EXCEED THE CAP OR THE "…and more" FLAG IS DEAD. Asked for exactly
// AnomalyRowCap rows, a full page and a truncated one are the same length, every flag
// is false forever, and a listing that stopped halfway presents itself as the whole
// answer. Pinned by TestAnomalyReadLimit_MakesTheCapVISIBLE, measured going red when
// the limit equals the cap.
//
// trimAnomalyRows IS GENERIC because six listings share this exact step, and six copies
// of "if len(rows) > cap { rows = rows[:cap]; more = true }" is six places for one of
// them to lose the second statement -- silently, because a page that is one row short
// looks exactly like a page that is complete.
func anomalyReadLimit() int32 { return readLimit(AnomalyRowCap) }

// markPendingClaims sets ClaimPending on the listed check-ins and checkouts that are the anchor of
// an undecided punch claim (M15-18). THE BINDING IS THE ROW'S ID, NEVER ITS INSTANT: two records of
// one person can share an occurred_at, and a mark matched by time would land on both. A failed read
// fails the screen with the rest of its reads -- the mark is not dropped silently, which would say
// "no claim" about rows nobody asked about (§4.6).
func markPendingClaims(ctx context.Context, q *store.Queries, tenantID uuid.UUID, open []AnomalyOpen, inMissing []AnomalyInMissing) error {
	ids := make([]uuid.UUID, 0, len(open)+len(inMissing))
	for _, o := range open {
		ids = append(ids, o.ID)
	}
	for _, o := range inMissing {
		ids = append(ids, o.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.ListPendingPunchClaimAnchors(ctx, store.ListPendingPunchClaimAnchorsParams{TenantID: tenantID, AnchorIds: ids})
	if err != nil {
		return fmt.Errorf("list the pending claims of the listed records: %w", err)
	}
	pending := make(map[uuid.UUID]bool, len(rows))
	for _, id := range rows {
		pending[id] = true
	}
	for i := range open {
		open[i].ClaimPending = pending[open[i].ID]
	}
	for i := range inMissing {
		inMissing[i].ClaimPending = pending[inMissing[i].ID]
	}
	return nil
}

func trimAnomalyRows[T any](rows []T) (kept []T, more bool) {
	if len(rows) > AnomalyRowCap {
		return rows[:AnomalyRowCap], true
	}
	return rows, false
}

// mergeSitePlaques adds the projection's gaps to the plaques' own rows (one row per plaque: the
// counts summed, the largest gap the larger), keeps the listing's order -- largest gap, then
// most gaps, then the uid -- and its cap, and reports whether anything was left out.
func mergeSitePlaques(own []AnomalyPlaque, site []store.ListSitePlaqueGapsRow, more bool) ([]AnomalyPlaque, bool) {
	at := make(map[string]int, len(own))
	for i, p := range own {
		at[p.TagUID] = i
	}
	for _, sp := range site {
		if i, ok := at[sp.TagUid]; ok {
			own[i].Records += int(sp.Records)
			own[i].CounterGaps += int(sp.CounterGaps)
			own[i].LargestGap = max(own[i].LargestGap, int(sp.LargestGap))
			continue
		}
		at[sp.TagUid] = len(own)
		own = append(own, AnomalyPlaque{
			TagUID:       sp.TagUid,
			LocationName: deref(sp.LocationName),
			Records:      int(sp.Records),
			CounterGaps:  int(sp.CounterGaps),
			LargestGap:   int(sp.LargestGap),
		})
	}
	sort.SliceStable(own, func(i, j int) bool {
		if own[i].LargestGap != own[j].LargestGap {
			return own[i].LargestGap > own[j].LargestGap
		}
		if own[i].CounterGaps != own[j].CounterGaps {
			return own[i].CounterGaps > own[j].CounterGaps
		}
		return own[i].TagUID < own[j].TagUID
	})
	if len(own) > AnomalyRowCap {
		own, more = own[:AnomalyRowCap], true
	}
	return own, more
}

// LeftOpen is The Pass's "left open since yesterday" (M12-15, D3): how many of YESTERDAY's
// check-ins no checkout has closed.
//
// IT IS THE ODD SHAPES DEFINITION, NOT A NEW ONE: ListOpenCheckIns (a non-practice `in` whose own
// next record does not close it -- ADR 0032 §A.6's one classification, the reports section's too,
// since M15-05), over yesterday's calendar day in the tenant's zone. No new SQL --
// the card's choice -- so the Pass's number and the Odd shapes list cannot disagree about what an
// open check-in is, and Day is what the Pass links that list with (its `week` parameter).
//
// 🔴 §4.6: CAPPED AT AnomalyRowCap, THE SAME CAP THE LIST IT LINKS TO USES. The read asks for one
// row more; when it arrives N is the cap and Capped says "N+". A partner-venue entry counts (the
// record is this business's) and adds nothing but one to the number (K-39).
//
// EXPLAIN (ANALYZE, BUFFERS) of yesterday's window, 2026-10-20, seed tenant after `make seed` and
// one simulated day: Index Scan Backward on transactions_tenant_occurred_idx (the day) -> Nested
// Loop Anti Join probing transactions_tenant_employee_occurred_idx for a later `out` -> the name
// joins -> Incremental Sort -> Limit 51; 0 rows (the day's one entry was closed), 0.49 ms. One
// observation on a 22-row table, of the statement before M15-05 replaced the anti join with the
// next-record lookup (its header carries that measurement).
type LeftOpen struct {
	State Reading
	// N is the number of yesterday's check-ins still open, exact unless Capped.
	N int
	// Capped means there are more than N: the screen prints "N+".
	Capped bool
	// Day is yesterday in the tenant's zone -- the day counted and the Odd shapes link's week.
	Day Date
}

// leftOpenAt counts yesterday's open check-ins as of now, in zone.
func leftOpenAt(ctx context.Context, q *store.Queries, tenantID uuid.UUID, now time.Time, zone *time.Location) (LeftOpen, error) {
	if zone == nil {
		zone = time.UTC
	}
	y, m, d := now.In(zone).Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, zone)
	// AddDate, not -24h: a DST day is 23 or 25 hours long.
	yesterday := today.AddDate(0, 0, -1)
	rows, err := q.ListOpenCheckIns(ctx, store.ListOpenCheckInsParams{
		TenantID: tenantID,
		FromAt:   yesterday,
		ToAt:     today,
		RowLimit: anomalyReadLimit(),
	})
	if err != nil {
		return LeftOpen{}, fmt.Errorf("list yesterday's open check-ins: %w", err)
	}
	rows, capped := trimAnomalyRows(rows)
	return LeftOpen{
		State:  Measured,
		N:      len(rows),
		Capped: capped,
		Day:    Date{Year: yesterday.Year(), Month: yesterday.Month(), Day: yesterday.Day()},
	}, nil
}
