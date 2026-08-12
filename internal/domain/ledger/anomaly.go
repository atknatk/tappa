package ledger

// anomaly.go -- the READ side of the ANOMALIES section (M6-11).
//
// 🔴 IT WRITES NOTHING, AND THAT IS THE PROPERTY THIS FILE IS FOR. Every one of the
// eight store calls below is a SELECT over `transactions`, which §4.3 makes
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
// What asserts the second half over these eight queries is this package's existing
// TestPanelQueries_CarryAnExplicitTenantPredicate (query_test.go), which DERIVES its
// subject list from the store calls this package makes -- so a query added here is
// covered because it is CALLED, with no list to forget to update. Verified:
// `go test ./internal/domain/ledger -run TestPanelQueries_CarryAnExplicitTenantPredicate -v`
// lists all eight by name.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
}

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
}

// AnomalyOpen is a check-in with no later checkout.
type AnomalyOpen struct {
	EmployeeName string
	LocationName string
	At           time.Time
	// ManagerTyped marks an entry somebody typed rather than tapped.
	ManagerTyped bool
	// WasMaskable means a practice row of this person's sits AFTER this check-in --
	// the shape ADR 0008 fixed, and one an ordinary new starter also produces. It is
	// a hint about where to look, and the screen says exactly that.
	WasMaskable bool
}

// MaskedOpenTally is the retrospective half of the ADR 0008 devir note.
//
// 🔴 THE SECOND NUMBER IS THE ONE THE OPEN LIST CANNOT PRODUCE. "Open" is `NOT EXISTS
// (an out after it)`, and one `out` satisfies that for every older unclosed check-in at
// once -- so a masked entry leaves the list silently on the person's next checkout. It
// counts as closed while the `out` that closed it belonged to a different check-in.
type MaskedOpenTally struct {
	// StillOpen is the ones the open list above can still show.
	StillOpen int
	// ClosedLater is the ones a later checkout has already taken off it.
	ClosedLater int
}

// AnomalyPolicyRule is how many records one rule decided.
type AnomalyPolicyRule struct {
	// SID is the rule's stable id (M3-07), or "" for a record with no policy
	// decision at all -- everything written before M3-07, and every manager-typed row.
	SID string
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

	Totals   AnomalyTotals
	People   []AnomalyPerson
	Venues   []AnomalyVenue
	Plaques  []AnomalyPlaque
	Pairs    []AnomalyPair
	Open     []AnomalyOpen
	Masked   MaskedOpenTally
	Policies []AnomalyPolicyRule

	// The five "…and there are more" flags. Each is true when its listing hit
	// AnomalyRowCap, which is detected by asking for one row more than can be used --
	// the same shape ledger.readLimit uses for the report's truncation verdict.
	MorePeople  bool
	MoreVenues  bool
	MorePlaques bool
	MorePairs   bool
	MoreOpen    bool
	// MorePolicies is its twin for the rule breakdown.
	MorePolicies bool

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
// ONE TRANSACTION, EIGHT QUERIES -- the shape Hours and Screen use. The clock has to
// be read before the week's boundaries can be computed, and running the rest in the
// same transaction means the eight answers cannot disagree about which records existed.
//
// ⚠️ WHAT EIGHT QUERIES IN ONE TRANSACTION COST is measured rather than assumed; the
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
			DistanceOnly:          int(signals.DistanceOnly),
			OutsideVenue:          int(signals.OutsideVenue),
			OutsideVenueOnNetwork: int(signals.OutsideVenueOnNetwork),
			CounterGaps:           int(signals.CounterGaps),
			OtherVenue:            int(signals.OtherVenue),
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
			row := AnomalyVenue{
				Name:         deref(v.LocationName),
				Records:      int(v.Records),
				Judged:       int(v.Judged),
				Answerable:   int(v.Answerable),
				DistanceOnly: int(v.DistanceOnly),
				OutsideVenue: int(v.OutsideVenue),
			}
			if v.LocationID != nil {
				row.LocationID = *v.LocationID
			}
			body.Venues = append(body.Venues, row)
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
		for _, p := range pairs {
			body.Pairs = append(body.Pairs, AnomalyPair{
				First:          deref(p.FirstEmployeeName),
				Second:         deref(p.SecondEmployeeName),
				Venue:          deref(p.LocationName),
				Together:       int(p.Together),
				Days:           int(p.Days),
				ClosestSeconds: int(p.ClosestSeconds),
			})
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
				EmployeeName: deref(o.EmployeeName),
				LocationName: deref(o.LocationName),
				At:           o.OccurredAt,
				ManagerTyped: o.ManagerTyped,
				WasMaskable:  o.WasMaskable,
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
			StillOpen:   int(masked.StillOpen),
			ClosedLater: int(masked.ClosedLater),
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
		body.Policies = make([]AnomalyPolicyRule, 0, len(rules))
		for _, s := range rules {
			body.Policies = append(body.Policies, AnomalyPolicyRule{
				SID:     deref(s.MatchedSid),
				Layer:   deref(s.PolicyLayer),
				Records: int(s.Records),
			})
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

func trimAnomalyRows[T any](rows []T) (kept []T, more bool) {
	if len(rows) > AnomalyRowCap {
		return rows[:AnomalyRowCap], true
	}
	return rows, false
}
