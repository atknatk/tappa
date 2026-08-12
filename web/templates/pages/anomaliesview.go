package pages

// The ANOMALIES section's view model (M6-11).
//
// 🔴 IT IS A READ-ONLY VIEW AND THE ABSENCE OF EVERY WRITE FIELD IS THE SPEC. There is
// no CSRFToken here, no form target and no per-row action href. This section shows what
// the immutable record already says and changes nothing — there is no "acknowledge", no
// "dismiss" and no "mark as reviewed", because every one of those is a write to a table
// that takes none (§4.3), and because a dismissed signal is a signal the next reader
// never sees. A template cannot render a control whose destination the view has no
// field to carry.
//
// 🔴 §4.7 — EVERY FIELD BELOW IS A STRING, AN INT OR A BOOL: no interface, no map, no
// []byte and no nested payload of any kind.
// TestAnomaliesView_CarriesNothingACoordinateCouldHideIn walks the whole tree and
// refuses both the §4.7 name shapes and any other kind.
//
// ⚠️ WHAT THAT DOES NOT BUY, because this paragraph claimed it and was wrong. It used
// to say there was "nowhere for one to hide even under a neutral name like `Detail`" —
// a non-sequitur, since a string is exactly where a coordinate hides. A security review
// carried `t.gps_lat::text` through a `Detail string` field on this very type and both
// structural walls passed it. The wall that refused it is the behavioural one:
// TestPanelAnomaliesDB_NoCoordinateAndNoAddressReachesThePage (internal/handler) plants
// real coordinates and sweeps the rendered page, by literal and by the SHAPE of a
// decimal degree. What THIS type buys is that an open container cannot be added
// silently; a plainly-named string still can, and the page sweep is what catches it.
//
// 🔴 THE REPORT DOES NOT COMMENT, IT SHOWS DATA. There is no Severity, no Score, no
// Suspicious flag and no Rank. Every listing is a count beside the denominator it came
// out of, and every threshold the screen filtered by is a FIELD so the page can print
// it — a list filtered by a constant the reader cannot see is one they have to take on
// trust.
//
// EVERY NUMBER IS ALREADY A STRING WHERE IT IS PROSE and an int where the template has
// to compare it (an "is there any" test). The handler formats once, against the zone the
// database gave for this tenant.
type AnomaliesView struct {
	PanelChrome

	// Queried is set ONLY after the database has answered, and it gates every listing
	// below.
	//
	// 🔴 IT IS THE ANTI-FABRICATION FLAG, and it carries more weight on this screen
	// than on any other in the panel. A zero AnomaliesView renders as a page with no
	// signals on it, which reads as "nothing odd happened this week" — the single most
	// damaging sentence this product could say without having measured it. A failed
	// read renders a problem page in the handler and never reaches this template.
	Queried bool

	// ZoneLabel names the timezone the days below are counted in. It decides where a
	// week begins and which local day a pair of taps falls on.
	ZoneLabel string

	// WeekLabel is the period in words; WeekValue is the ISO date the picker echoes.
	WeekLabel string
	WeekValue string

	// The three navigation targets, the same shape the reports section uses.
	PrevHref     string
	NextHref     string
	ThisWeekHref string

	Totals AnomalyTotalsView

	People  []AnomalyPersonView
	Venues  []AnomalyVenueView
	Plaques []AnomalyPlaqueView
	Pairs   []AnomalyPairView
	Open    []AnomalyOpenView

	// Masked is the retrospective ADR 0008 tally: check-ins a practice row could have
	// hidden, split by whether the list above can still show them.
	Masked MaskedOpenView

	Policies []AnomalyPolicyView

	// The six "…and there are more" flags. Each is set when its listing hit the cap.
	MorePeople   bool
	MoreVenues   bool
	MorePlaques  bool
	MorePairs    bool
	MoreOpen     bool
	MorePolicies bool

	// TogetherLine states the pair listing's thresholds in words, because a reader who
	// cannot see them cannot judge the list.
	TogetherLine string

	// ListCap is how many rows one listing prints.
	//
	// 🔴 IT IS A FIELD RATHER THAN A CONSTANT IN THIS PACKAGE, because the number is
	// the READ side's (ledger.AnomalyRowCap) and a copy here would be a second
	// representation of it — one that stays self-consistent while drifting, so the
	// sentence "this list stops at 50" would keep reading correctly while the list
	// stopped at some other number.
	ListCap int
}

// AnomalyTotalsView is the window in numbers, already formatted.
//
// EACH PROSE FIELD CARRIES ITS OWN DENOMINATOR ("47 of 2 273 judged records · 2%"),
// which is what makes this a report rather than an assertion. The plain ints beside
// them exist so the template can ask "is there any" without parsing a sentence.
type AnomalyTotalsView struct {
	// RecordsLine and JudgedLine are the two denominators every other line is out of.
	RecordsLine string
	JudgedLine  string

	// UnanswerableLine is the honest gap: records with no frozen policy context, which
	// three of the signals below cannot be answered for at all.
	UnanswerableLine string
	Unanswerable     int

	// BasisLine names BOTH denominators in words.
	//
	// 🔴 THE FIGURES BELOW DO NOT ALL DIVIDE BY THE SAME THING, and until this field
	// existed the page did not say so — worse, three of them divided by a base that
	// INCLUDED the records they cannot be computed for, which is what counting a record
	// as clean looks like written as arithmetic.
	BasisLine string

	DistanceOnlyLine string
	DistanceOnly     int

	OutsideVenueLine string
	OutsideVenue     int
	// OutsideVenueOnNetworkLine is the subset that also answered from inside the
	// venue's registered address range.
	OutsideVenueOnNetworkLine string

	CounterGapsLine string
	CounterGaps     int

	OtherVenueLine string
	OtherVenue     int

	TrainingLine     string
	Training         int
	ManagerTypedLine string
	ManagerTyped     int
}

// AnomalyPersonView is one employee's row. Only people with a non-zero signal reach it.
type AnomalyPersonView struct {
	Name string
	// Records is prose ("212 records"): the size of this person's week, printed as
	// context for the counts beside it.
	//
	// 🔴 IT IS NOT A DENOMINATOR AND THIS COMMENT USED TO CALL IT ONE. Three of the four
	// counters on a person's row are read out of the policy snapshot, so their base
	// would be this person's ANSWERABLE record count -- a column ListAnomalyPeople does
	// not return. The row therefore prints COUNTS and no rate at all, which is why
	// nothing wrong is visible on the screen; what was wrong was the sentence. A
	// per-person rate would need that column first, and it is the same defect class the
	// totals docket already paid for once.
	Records string
	// The four counters, as prose or "" for nothing. The template tests the string it
	// would print, so a zero never renders as a chip.
	DistanceOnly string
	OutsideVenue string
	CounterGaps  string
	OtherVenue   string
}

// AnomalyVenueView is one location's row. EVERY venue with taps is here, including the
// clean ones — the comparison between branches is the point.
// ⚠️ THE TWO FIGURES ON A VENUE ROW COME OUT OF DIFFERENT POPULATIONS — DistanceOnly
// out of the records the engine judged on evidence, OutsideVenue out of the records
// carrying a rule snapshot — and each string prints the base it used, so the row is
// readable without knowing that.
type AnomalyVenueView struct {
	Name         string
	Records      string
	DistanceOnly string
	OutsideVenue string
}

// AnomalyPlaqueView is one wall tag's counter gaps.
type AnomalyPlaqueView struct {
	// TagUID is the plaque's own identifier. It is not a secret: it travels in the tap
	// URL and the last four characters are printed on the plaque itself (skill
	// tappa-brand), and it is the one thing here a manager can act on — they walk to
	// that door.
	TagUID       string
	LocationName string
	Records      string
	CounterGaps  string
	LargestGap   string
}

// AnomalyPairView is two people whose taps land together, repeatedly.
type AnomalyPairView struct {
	First  string
	Second string
	Venue  string
	Days   string
	// Together is how many times the two were adjacent inside the window. It is
	// rendered: "twice across two days" and "forty times across two days" are different
	// rows and the day count cannot tell them apart.
	Together string
	Closest  string
}

// AnomalyOpenView is a check-in nothing closed.
type AnomalyOpenView struct {
	Who   string
	Venue string
	At    string
	// ManagerTyped marks an entry somebody typed rather than tapped.
	ManagerTyped bool
	// WasMaskable means a practice row of this person's sits after this check-in. It
	// is a hint about where to look and the screen says exactly that — an ordinary new
	// starter produces the same shape.
	WasMaskable bool
}

// MaskedOpenView is the ADR 0008 retrospective tally.
type MaskedOpenView struct {
	// Total is the two counts added up, so the template can ask whether to render the
	// block at all without re-adding them.
	Total       int
	StillOpen   string
	ClosedLater string
}

// AnomalyPolicyView is how many records one rule decided.
type AnomalyPolicyView struct {
	// SID is the rule's stable id, or the words for "no rule recorded" — the handler
	// resolves that, so the template never has to render an empty cell.
	SID string
	// Layer is guardrail / baseline / tenant, or "" when there is no rule.
	Layer   string
	Records string
}
