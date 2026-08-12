package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The ANOMALIES SECTION — M6-11. Detection, never prevention.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), which the
// Protect chain resolved from a signed session cookie against the database. The one
// thing a client controls here is WHICH WEEK, which is a narrowing predicate inside
// that tenant: a hand-edited date can only move the reader along their own business's
// timeline.
//
// 🔴 §4.3 — READ ONLY, AND THE ABSENT HALF OF THE PANEL PATTERN IS ABSENT ON PURPOSE.
// There is no entry in mountWriting for this section, no ProtectWriting chain, no
// synchronizer token, no MAC'd confirmation and no audit_log row — because nothing here
// writes. It is worth the sentence: five of this repository's sections DO have those,
// and the next reader should not read their absence as an omission. What is NOT skipped
// is a.render's header block (the panel CSP, Cache-Control: no-store, nosniff).
//
// 🔴 AND THERE IS DELIBERATELY NO "DISMISS" ANYWHERE. A signal a manager can silence is
// a signal the next manager never sees, and the record it came from is immutable — the
// honest control for "I looked at this" is the correction M6-08 already provides, which
// changes the underlying records and therefore changes these numbers.
//
// 🔴 §4.7 — NO COORDINATE, NO ADDRESS AND NO SNAPSHOT PASSES THROUGH THIS FILE. The
// queries behind it select no gps_lat, no gps_lng, no source_ip and no policy_context —
// the three signals derived from that snapshot are COUNTED INSIDE THE QUERY, so the
// jsonb never crosses the boundary at all.
//
// ⚠️ AND THE SENTENCE THAT USED TO FOLLOW — "could not carry one if it tried" — WAS
// MEASURABLY FALSE. A security review tried: `t.gps_lat::text AS detail` in a query,
// sqlc emitting `Detail string`, carried through ledger and the view to the template.
// Both structural walls passed it, because a coordinate fits in a string and no name
// list or kind list can tell. What caught it was the behavioural sweep,
// TestPanelAnomaliesDB_NoCoordinateAndNoAddressReachesThePage, which plants real
// coordinates and reads the rendered page — by literal AND by the shape of a decimal
// degree, after a second mutation showed a ROUNDED coordinate walking past a
// literal-only sweep at about 110 m resolution. So: the structural walls refuse open
// containers, the behavioural sweep is what refuses a coordinate wearing a plain name,
// and this comment claims only that.
//
// 🔴 §4.2 — GPS IS READ AT TAP TIME AND NOWHERE ELSE. This screen reads records that
// already exist. It starts no location request, and the fact it reports about a reading
// is whether it fell inside a radius the venue itself registered.
//
// §4.6 — A FAILED READ IS AN ERROR, NEVER A QUIET WEEK. If the database does not
// answer, this renders a problem page. It must not fall through to a page with no
// signals on it: "nothing odd happened this week" is a claim about a business, and a
// timeout is not evidence for it. That is the most damaging sentence this section could
// produce, which is why it is the first thing the handler guards.

// anomaliesHref is the section's own URL, READ FROM THE SECTION TABLE rather than
// written out again. It is what the week picker submits to and what every week link is
// built from, so if the section moves they move with it; if the tab is removed
// altogether this panics at startup rather than silently building links to nowhere.
var anomaliesHref = mustSectionHref(pages.TabAnomalies)

// WHAT ONE VIEW OF THIS SECTION COSTS. The read is EIGHT queries in one transaction
// over one week of one tenant.
//
// ⚠️ THE TIMINGS BELOW ARE ONE MACHINE'S OBSERVATION AND ARE NOT A PROPERTY OF THESE
// QUERIES. Measured 2026-08-12 with EXPLAIN (ANALYZE, BUFFERS) against the development
// database, tenant Kebab Factory (10000000-…-000000000001), the seven local days to
// 2026-08-13, four runs of the whole set:
//
//	query                        execution      shape
//	CountAnomalySignals         5.0 - 6.8 ms   one bitmap heap scan, ten FILTERed counts
//	ListAnomalyPeople           7.3 - 12.1 ms  the same scan, grouped and joined
//	ListAnomalyVenues           3.2 - 4.4 ms   the same scan, grouped
//	ListAnomalyPlaques          5.8 - 7.9 ms   the same scan, grouped
//	ListOpenCheckIns            1.5 - 3.5 ms   index anti-join, stops at the cap
//	CountMaskedOpenCheckIns     3.3 - 5.6 ms   two anti-joins over the week's check-ins
//	ListAnomalyPolicySids       1.9 - 5.8 ms   the same scan, grouped
//	ListTapsTakenTogether      11.0 - 24.7 ms  one sort + a lag() window
//
// 🔴 HOW MUCH THOSE READINGS ARE WORTH, STATED RATHER THAN LEFT TO BE DISCOVERED: an
// independent reviewer measured the LAST of them on a comparable table at 5.7 - 6.3 ms,
// which is a quarter of what this machine saw. What the table is evidence for is the
// SHAPE — eight statements, each bounded by the week rather than by history, none of
// them growing with the tenant's history — and not for any millisecond in it.
//
// ⚠️ AND THE WINDOW'S OWN ROW COUNT IS NOT A CONSTANT EITHER, WHICH IS WHY IT IS A
// QUERY HERE AND NOT A NUMBER. On the day these were taken the same window read 1 063
// directed non-practice rows in the morning and 1 365 hours later, because every
// `make test` run writes into it and §4.3 means nothing can be taken back out. Two
// files in this change once quoted the two readings as if they were one fact. To
// re-derive it:
//
//	SELECT count(*) FILTER (WHERE employee_id IS NOT NULL AND location_id IS NOT NULL
//	                          AND type IS NOT NULL AND NOT practice)
//	  FROM transactions
//	 WHERE tenant_id = $1 AND occurred_at >= $2 AND occurred_at < $3;
//
// 🔴 WHY THE PAIR QUERY IS A WINDOW FUNCTION AND NOT A SELF-JOIN is argued in
// db/queries/anomalies.sql, in terms of the COMPARISON COUNT the plan reports rather
// than in milliseconds. Three separate wall-clock comparisons of those two shapes were
// taken during this work and all three disagreed, so no speed ratio is claimed
// anywhere; the argument that survives is that one shape's work grows with the square
// of the week's rows and the other's does not.
//
// ⚠️ AND NO INDEX WAS ADDED FOR THIS SECTION. Every query above rides
// transactions_tenant_occurred_idx or transactions_tenant_location_idx; the jsonb keys
// are not indexed and are read as filters over an already-narrowed week, which is why
// they cost nothing extra here. A tenant whose week is much larger would want a
// different plan, and that is a migration rather than a change to this file.

// maxAnomalyRows is how many rows this screen LISTS, and it is ledger's constant rather
// than a second copy of it.
//
// 🔴 IT IS A BOUND ON EACH LIST, NEVER ON A TALLY. Every figure in the totals docket
// comes from a query with no LIMIT at all, so a shortened listing never shortens a
// number beside it — which is the failure the reports section measured and named.
//
// WHAT THE PAGE COSTS, MEASURED 2026-08-12 ON THIS HANDLER — the real handler and the
// real template, varying only how many rows the listings hold:
//
//	listings                                   document
//	an empty week                                5 727 B
//	the database fixture's week (22 records)    11 032 B
//	every one of the six listings at the cap   113 000 - 118 000 B
//
// The first and second are exact. THE THIRD IS A BAND ON PURPOSE: three independent
// probes of the saturated page read 113 080, 115 906 and 117 630 B, and the only thing
// that differed between them was how long the fabricated employee and venue NAMES were.
// A saturated page is 300 rows of somebody's names, so its size is a property of the
// business as much as of this template, and a single figure would be a false precision.
//
// The empty and saturated pages are rendered through the panel's own browser with a
// reader the probe controls; the fixture week is a real signed-in session against real
// Postgres (the fixture is anomalies_db_test.go's). So the page is bounded at roughly
// 118 KB whatever the business is — about 92% of the 128 320 B the reports section
// settled at, and 14% of the 867 233 B control M6-03 measured and removed. It is
// Cache-Control: no-store and therefore re-sent on every view, so that is a per-view
// cost rather than a download.
//
// ⚠️ NO TEST PINS THESE NUMBERS, AND THAT IS A DECISION RATHER THAN AN OVERSIGHT. A
// byte assertion on a page is a change detector: it goes red on any honest copy edit,
// and this section's review rounds added three sentences to the page and moved the
// figure twice — a pinned test would have gone red three times without a defect behind
// it, which is how an assertion gets loosened until it measures nothing. What the
// figures are for is the ceiling argument (a bounded page, and the caps that bound it),
// and the caps ARE pinned. The cost is that this table can go stale silently; the
// mitigation is that it carries its date and the probe it came from is described here.
//
// ⚠️ RE-MEASURED TWICE ALREADY. The first draft read 5 115 / 10 361 / 114 060 B, before
// the denominator sentence, the pair listing's occurrence count and a third item in the
// "what this page does not see" block were added. A byte table that is not re-taken
// after the page changes is a byte table about a different page.
//
// ⚠️ THE SATURATED FIGURE IS A CEILING RATHER THAN A TYPICAL PAGE, and the two are far
// apart here: it needs FIFTY people carrying a signal, fifty venues, fifty plaques with
// counter gaps, fifty repeat pairs and fifty unclosed check-ins in ONE week. The
// development database's largest tenant has nine venues. The ceiling is what is
// guaranteed; the fixture line above is closer to what is seen.
const maxAnomalyRows = ledger.AnomalyRowCap

// anomaliesSection renders the whole section.
//
// 🔴 IT LOADS NO SCRIPT, so changing week is an ordinary navigation and the panel's
// widened Content-Security-Policy stays pinned to exactly one url.
func (a *AdminAuth) anomaliesSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f := parseAnomalyFilter(r)

	screen, err := a.ledger.Anomalies(r.Context(), id.TenantID(), f)
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to a page with no signals.
		a.log.Error("panel: could not read the week's anomaly signals", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	v := pages.AnomaliesView{PanelChrome: a.chrome(r, pages.TabAnomalies)}
	fillAnomaliesView(&v, screen)
	a.render(w, r, http.StatusOK, pages.AdminAnomalies(v))
}

// fillAnomaliesView maps the domain read onto the view.
//
// EVERY TIME IS CONVERTED HERE AND ONLY HERE (§6: the database is UTC and the render
// layer converts), against the zone the database supplied for this tenant.
func fillAnomaliesView(v *pages.AnomaliesView, s ledger.AnomalyScreen) {
	zone := s.Zone
	if zone == nil {
		zone = time.UTC
	}
	p := s.Period

	v.Queried = s.Queried
	v.ZoneLabel = zone.String()
	v.ListCap = maxAnomalyRows

	v.WeekValue = p.FirstDay.String()
	days := p.DayLabels(zone)
	last := days[len(days)-1]
	// The same week label the reports section prints, for the same reason: a manager
	// reads this line to check they are on the right week, and a repeated month is
	// noise rather than precision.
	if days[0].Month() == last.Month() {
		v.WeekLabel = days[0].Format("Mon 2") + " – " + last.Format("Mon 2 January 2006")
	} else {
		v.WeekLabel = days[0].Format("Mon 2 Jan") + " – " + last.Format("Mon 2 Jan 2006")
	}

	v.ThisWeekHref = anomaliesHref
	v.PrevHref = anomalyWeekHref(days[0].AddDate(0, 0, -1))
	v.NextHref = anomalyWeekHref(days[0].AddDate(0, 0, 7))

	// 🔴 TWO BASES, NAMED. `judged` is the records the engine gathered address or
	// distance evidence for, and it is the base for the figure read out of those
	// COLUMNS. `answerable` is the records that carry a policy snapshot, and it is the
	// base for the three figures read out of THAT. Dividing the second group by
	// `records` — which the first draft did — puts every record the screen cannot
	// answer for into the base of the questions it cannot answer, which is what
	// "counting them as clean" means when it is written as arithmetic.
	t := s.Totals
	v.Totals = pages.AnomalyTotalsView{
		RecordsLine:      plural(t.Records, "record", "records"),
		JudgedLine:       judgedLine(t.Judged),
		Unanswerable:     t.Unanswerable,
		UnanswerableLine: unanswerableLine(t.Unanswerable),
		BasisLine:        basisLine(t.Judged, t.Answerable),

		DistanceOnly:     t.DistanceOnly,
		DistanceOnlyLine: outOfLine(t.DistanceOnly, t.Judged, "verified by GPS alone"),

		OutsideVenue:              t.OutsideVenue,
		OutsideVenueLine:          outOfLine(t.OutsideVenue, t.Answerable, "read outside the venue"),
		OutsideVenueOnNetworkLine: onNetworkLine(t.OutsideVenueOnNetwork, t.OutsideVenue),

		CounterGaps:     t.CounterGaps,
		CounterGapsLine: outOfLine(t.CounterGaps, t.Answerable, "after a counter gap"),

		OtherVenue:     t.OtherVenue,
		OtherVenueLine: outOfLine(t.OtherVenue, t.Answerable, "at another venue"),

		Training:         t.Training,
		TrainingLine:     plural(t.Training, "training tap", "training taps"),
		ManagerTyped:     t.ManagerTyped,
		ManagerTypedLine: plural(t.ManagerTyped, "typed by a manager", "typed by a manager"),
	}

	v.MorePeople = s.MorePeople
	v.People = make([]pages.AnomalyPersonView, 0, len(s.People))
	for _, p := range s.People {
		v.People = append(v.People, pages.AnomalyPersonView{
			Name:         personName(p.Name),
			Records:      plural(p.Records, "record", "records"),
			DistanceOnly: nonZeroCount(p.DistanceOnly, "verified by GPS alone"),
			OutsideVenue: nonZeroCount(p.OutsideVenue, "read outside the venue"),
			CounterGaps:  nonZeroCount(p.CounterGaps, "after a counter gap"),
			OtherVenue:   nonZeroCount(p.OtherVenue, "at another venue"),
		})
	}

	v.MoreVenues = s.MoreVenues
	v.Venues = make([]pages.AnomalyVenueView, 0, len(s.Venues))
	for _, l := range s.Venues {
		v.Venues = append(v.Venues, pages.AnomalyVenueView{
			Name:    venueName(l.Name),
			Records: plural(l.Records, "record", "records"),
			// THE VENUE ROWS PRINT THEIR ZEROES, unlike the people rows. A branch with
			// none of these is the comparison the other branches are read against, so
			// suppressing it would leave a reader unable to tell "clean" from "not
			// listed".
			DistanceOnly: outOfLine(l.DistanceOnly, l.Judged, "verified by GPS alone"),
			// OUT OF THIS VENUE'S SNAPSHOT-CARRYING RECORDS, not its judged ones —
			// the same split the totals docket makes, for the same reason.
			OutsideVenue: outOfLine(l.OutsideVenue, l.Answerable, "read outside the venue"),
		})
	}

	v.MorePlaques = s.MorePlaques
	v.Plaques = make([]pages.AnomalyPlaqueView, 0, len(s.Plaques))
	for _, p := range s.Plaques {
		v.Plaques = append(v.Plaques, pages.AnomalyPlaqueView{
			TagUID:       plaqueName(p.TagUID),
			LocationName: venueName(p.LocationName),
			Records:      plural(p.Records, "record", "records"),
			CounterGaps:  plural(p.CounterGaps, "gap", "gaps"),
			LargestGap:   largestGapLine(p.LargestGap),
		})
	}

	v.MorePairs = s.MorePairs
	v.TogetherLine = togetherLine(s.TogetherWithinSeconds, s.TogetherMinDays)
	v.Pairs = make([]pages.AnomalyPairView, 0, len(s.Pairs))
	for _, p := range s.Pairs {
		v.Pairs = append(v.Pairs, pages.AnomalyPairView{
			First:  personName(p.First),
			Second: personName(p.Second),
			Venue:  venueName(p.Venue),
			Days:   plural(p.Days, "day", "days"),
			// TOGETHER IS RENDERED, AND IT WAS SELECTED WITHOUT A READER UNTIL A REVIEW
			// FOUND IT. "Twice across two days" and "forty times across two days" are
			// different rows, and the day count alone cannot tell them apart.
			Together: plural(p.Together, "time together", "times together"),
			Closest:  closestLine(p.ClosestSeconds),
		})
	}

	v.MoreOpen = s.MoreOpen
	v.Open = make([]pages.AnomalyOpenView, 0, len(s.Open))
	for _, o := range s.Open {
		v.Open = append(v.Open, pages.AnomalyOpenView{
			Who:          personName(o.EmployeeName),
			Venue:        venueName(o.LocationName),
			At:           o.At.In(zone).Format("Mon 2 Jan 15:04"),
			ManagerTyped: o.ManagerTyped,
			WasMaskable:  o.WasMaskable,
		})
	}

	v.Masked = pages.MaskedOpenView{
		Total:       s.Masked.StillOpen + s.Masked.ClosedLater,
		StillOpen:   plural(s.Masked.StillOpen, "still on the list above", "still on the list above"),
		ClosedLater: plural(s.Masked.ClosedLater, "already off it", "already off it"),
	}

	v.MorePolicies = s.MorePolicies
	v.Policies = make([]pages.AnomalyPolicyView, 0, len(s.Policies))
	for _, s := range s.Policies {
		v.Policies = append(v.Policies, pages.AnomalyPolicyView{
			SID:     ruleName(s.SID),
			Layer:   s.Layer,
			Records: plural(s.Records, "record", "records"),
		})
	}
}

// outOfLine is the one sentence shape this screen repeats: a count, its denominator and
// what was counted.
//
// 🔴 THE DENOMINATOR IS NEVER DROPPED, EVEN WHEN THE COUNT IS ZERO. "0 of 212" and "0"
// are different claims: the first says a measurement was taken, the second reads like
// an absence of one. The percentage is INTEGER arithmetic — this screen has no money
// and no hours in it, but a float here would be the one float in the read path and the
// next reader would have to work out why.
//
// A ZERO DENOMINATOR PRINTS NO RATE rather than dividing. It is reachable: a week with
// no judged records at all is an ordinary week for a business that only types records.
func outOfLine(n, of int, what string) string {
	line := strconv.Itoa(n) + " of " + strconv.Itoa(of) + " " + what
	if of <= 0 || n <= 0 {
		return line
	}
	return line + " · " + strconv.Itoa(n*100/of) + "%"
}

// basisLine is the sentence that makes the two denominators readable.
//
// 🔴 IT IS ON THE PAGE BECAUSE THE FIGURES ABOVE IT DO NOT ALL DIVIDE BY THE SAME
// THING, and a reader who spots two different bases without being told why concludes
// one of them is a bug. Both numbers are printed, so the base beside each figure can be
// matched to the population it names.
//
// IT IS PRINTED EVEN WHEN THE TWO ARE EQUAL. "Both bases are the same this week" is a
// measured fact; going quiet when they agree would leave a reader unable to tell that
// case from a page that never split them.
func basisLine(judged, answerable int) string {
	return "Two bases are in use below. " + strconv.Itoa(judged) +
		" records were judged on evidence the engine gathered (an address match or a " +
		"distance reading); " + strconv.Itoa(answerable) +
		" carry the rule snapshot the counter-gap, outside-the-venue and other-venue " +
		"figures are read from. Each figure names the base it is out of."
}

// judgedLine names the denominator the rates above are out of, and says what is NOT in
// it. A manager-typed record has no address and no reading, so counting it in the
// denominator would quietly dilute every rate on the page.
//
// ⚠️ IT TAKES ONE NUMBER, NOT TWO. The first draft also took the record total and never
// read it -- an unused parameter that looked like a second denominator was being
// computed. The word "these" in the sentence refers to the figure printed directly
// above it, which is the record total, so the second argument had nothing to do.
func judgedLine(judged int) string {
	return strconv.Itoa(judged) + " of these were judged on evidence the engine gathered; " +
		"the rest carry none, which is what a typed record looks like."
}

// unanswerableLine is the §4.6 sentence: what this screen cannot answer for.
func unanswerableLine(n int) string {
	if n == 1 {
		return "1 record carries no policy snapshot, so the counter-gap, outside-the-venue " +
			"and other-venue counts cannot be answered for it. It is not counted as clean."
	}
	return strconv.Itoa(n) + " records carry no policy snapshot, so the counter-gap, " +
		"outside-the-venue and other-venue counts cannot be answered for them. They are " +
		"not counted as clean."
}

// onNetworkLine is the sentence the M6-11 card was reaching for, said in terms of what
// the engine actually records.
//
// 🔴 THE CARD CALLS THIS SIGNAL "the address matched but the position did not", AND
// THAT IS NOT WHAT THE FLAG MEANS. internal/domain/tap's gpsFacts sets it from the
// distance alone — it says nothing about the address at all. So the screen reports the
// flag for what it is, and reports the address match as a SEPARATE number beside it,
// which is the combination the card was after: a reading from outside, answered by
// something inside.
//
// ⚠️ THE TWO BRANCHES WERE ONCE TWO IDENTICAL `return`s DIFFERING ONLY IN `tail`, and
// the zero branch printed the same fact twice — "0 of 39 also matched … None of them
// answered from inside …". One return, one tail.
func onNetworkLine(onNetwork, outside int) string {
	if outside <= 0 {
		return ""
	}
	// A MEASURED ZERO IS A SENTENCE, not silence — but it is only the SECOND sentence,
	// because the count itself already carries the number.
	tail := "That combination is what something left switched on inside the venue would produce."
	if onNetwork == 0 {
		tail = "None of them answered from inside the venue's own range, so none has that shape."
	}
	return strconv.Itoa(onNetwork) + " of " + strconv.Itoa(outside) +
		" also matched the venue's registered address range. " + tail
}

// nonZeroCount is a per-row chip, or "" for nothing — so the template can ask "is there
// any" by testing the string it would print. A zero on a person's row is NOT printed:
// the row is already there because something else on it is non-zero, and a wall of
// zeroes is what makes a reader stop reading.
func nonZeroCount(n int, what string) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n) + " " + what
}

func largestGapLine(n int) string {
	if n == 1 {
		return "1 read missing"
	}
	return strconv.Itoa(n) + " reads missing"
}

// togetherLine prints the pair listing's own filter, because a list nobody can see the
// threshold of is a list they have to take on trust.
func togetherLine(within, minDays int) string {
	return "Listed when two people's taps at one venue land within " + strconv.Itoa(within) +
		" seconds of each other, on " + strconv.Itoa(minDays) + " or more separate days."
}

func closestLine(seconds int) string {
	if seconds == 1 {
		return "closest 1 second apart"
	}
	return "closest " + strconv.Itoa(seconds) + " seconds apart"
}

// plaqueName and ruleName turn an absent value into a WORD, for the reason personName
// and venueName give: a blank cell reads as "none", which is a different claim from "we
// could not resolve it".
func plaqueName(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func ruleName(s string) string {
	if s == "" {
		// §4.6 AGAIN: migration 0008's CHECK allows a record with no policy decision at
		// all — everything written before M3-07, and every typed row. Folding those into
		// another bucket would make this column stop adding up to the record count above
		// it.
		return "No rule recorded"
	}
	return s
}

// anomalyWeekHref is the section's URL for the week containing a given local day.
func anomalyWeekHref(day time.Time) string {
	q := url.Values{}
	q.Set("week", day.Format("2006-01-02"))
	return anomaliesHref + "?" + q.Encode()
}

// parseAnomalyFilter reads the week from the query string.
//
// 🔴 THE VALUE IS VALIDATED HERE, AT THE BOUNDARY (§7). A date that does not parse is
// DROPPED rather than refused, which shows the tenant's current week instead of an error
// page — safe for the reason the other sections give: dropping can only move the reader
// inside their own tenant, never outside it. It is also visible, because the picker
// echoes the week that actually took effect.
//
// IT REUSES ledger.ReportFilter rather than declaring a twin. "Any day inside the week I
// want" is one question with one parser and one week-boundary function behind it; a
// second identical struct would be a copy that drifts silently.
func parseAnomalyFilter(r *http.Request) ledger.ReportFilter {
	var f ledger.ReportFilter
	if d, err := ledger.ParseDate(r.URL.Query().Get("week")); err == nil {
		f.Day = d
	}
	return f
}
