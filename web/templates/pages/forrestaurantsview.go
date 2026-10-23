package pages

// THE RESTAURANTS PAGE (LP-09, 2026-10-22): /for/restaurants, drawn from the
// ForRestaurants / T-ForRestaurants / M-ForRestaurants boards
// (docs/design/landing-redesign/boards). It joins the public site the way the pricing,
// security and product pages did (pricingview.go records the shape): a row of
// PublicPages, a named route in internal/handler's Mount, every sentence a value here
// with the facts it rests on.
//
// THE PAGE IS A CHAIN'S WEEK: the late night that ends after midnight (Rusty Bar's
// 18:00–02:00, CLAUDE.md §5's own example), the venues each on their own hours and
// network, somebody covering another venue, a check-out somebody forgot, the day-to-day
// chores, and the hours that come out of it. The landing's "Multi-location" card is the
// way in: its sentences open this page word for word.
//
// THE VENUES ARE REAL (decision K1, 2026-10-21): RestaurantsVenues is the demo seed's
// Kebab Factory chain, name for name, hours for hours and range for range, and the page
// says "from our demo data" beside it. internal/handler reads test/fixtures/seed.sql and
// holds the table equal to it; the seed does not move to fit the page.
//
// THE PEOPLE AND THE TIMES ARE SAMPLES, and they are the landing panel's own: Luke Borg's
// 02:04 check-out at Rusty Bar is The Pass's docket (whose rail hangs on a Tuesday, 20
// Oct), and the shift it closes here is as long as his Monday on The Roll (week 42,
// Mon 12 Oct): 8h 06m. The page prints days of the week, not dates, so the two drawings
// share the times and not a date. Maria Vella's Thursday at KF Msida is the day The Roll
// marks as worked at another venue. internal/handler runs each docket through tap.Decide,
// and internal/domain/ledger's TestAccumulate_TheRestaurantsPagesWeek books the same
// times the way the page says they are booked.
//
// WHERE THE BOARD SAID MORE THAN THE PRODUCT DOES, the value here says less, and the edit
// is recorded once:
//
//   - "Start free — 3 months on us" (the hero's button) is "Start free": the free months
//     are said the code's way in the sign-off (K6), and the board's wording is refused on
//     every public page.
//   - "Taptime never invents a check-out." (the heading over the forgotten check-out) is
//     "A check-in left open for more than 18 hours is not counted until a manager closes
//     it." (the hours from RestaurantsStaleHours): no absence is derived here, the
//     person's own next tap DOES close an entry older than the engine's stale threshold
//     (tap.StaleOpenIn) -- as a check-out with a note, which the report does not count
//     and lists as open -- and a sooner tap closes it as an ordinary check-out whose hours
//     count. The bullet says both, in the product's word ("count", not "pay").
//   - "A venue's network only counts if the range is narrow enough to describe one
//     place" is said in the security page's words ("stand for a single network"): the
//     security review refused "describe one place" on every page.
//   - "Lateness is measured against the hours of the venue where the person tapped"
//     adds the department: a department's own shift comes first (§5).
//   - "The record carries a note that the tap was away from home" says "by default":
//     the note is a baseline rule (base:cross-location-note) a business can switch off.
//   - "The Roll counts those shifts as at another venue, so cover is visible without
//     being treated as a problem" says what the Roll does: it marks the day and counts
//     the hours like any other.
//   - The venues section carries a third bullet the board does not draw: the QR fallback
//     and what it needs (§5: a QR check-in has no signature, so the venue's network is
//     required and a position alone ends flagged). It is for a phone whose NFC is off or
//     fails on the day: a phone is set up only by a real tap on a plaque (ADR 0026), so
//     a phone that cannot tap at all never gets a session, and the bullet says so.
//   - "A tap at another venue is judged on that venue's evidence and that venue's hours,
//     exactly as a tap at home would be" says "by default": a business can write a rule
//     on employee:crossLocation. "Hours" says what the product does with them: the
//     report measures lateness against the tapped venue's shift (the record's own
//     location), unless the person's department has one.
//   - The venue table draws one layout at every width (a three-column table with its
//     header row): the phone board's two-line rows without a header would need a second
//     DOM or a table that stops being one.
//   - The hours drawing is the landing's own Roll (LandingPanelRollRows: four people,
//     Claire Mifsud included, and no "1 open check-in" line under Kevin Grech): the board
//     redraws a three-person Roll, and changing the shared drawing would change the
//     landing. Its legend is the panel's, under it, and a visible "The Roll · sample"
//     label sits over it.
//   - The forgotten check-out's three steps are an ordered list of cards, not a picture:
//     they are words.

// ForRestaurantsPath is the page's URL.
const ForRestaurantsPath = "/for/restaurants"

// ForRestaurantsView is the restaurants page.
type ForRestaurantsView struct {
	// SignupHref and SignInHref are the chrome's two ways in, as on LandingView.
	SignupHref string
	SignInHref string
	// FreeMonths is the founding offer's count, for the sign-off.
	FreeMonths int
}

// The facts only this page claims. Each is derived from the product in
// internal/handler/forrestaurants_facts_test.go; the closure test reads this block like
// the one in landingview.go.
const (
	// In or out is decided against the person's last OPEN check-in and never the calendar
	// day: a check-out after midnight closes the evening's check-in. Derived from
	// AnchorDirectionFollowsLastOpenEntry and by asking tap.Decide about a tap at 02:04
	// with a check-in open since 17:58 the evening before.
	FactDirectionFollowsLastOpenCheckIn Fact = "tap:LastOpenIn closes across midnight"
	// A worked shift's hours are booked on the day its check-in falls on and at the venue
	// of its check-in. Derived from internal/domain/ledger's accumulate.
	FactShiftCountsOnItsStartDayAndVenue Fact = "ledger:shift booked on its check-in's day and venue"
	// A tap is judged on the TAPPED venue's address ranges and GPS point, never the
	// person's home venue's (the gather hands the engine that venue's shift too, though
	// lateness is the report's: FactLatenessFollowsOwnShift). Derived from
	// internal/domain/checkin's gather and pinned by internal/handler's
	// TestCheckinDB_ATapAtAnotherVenueIsJudgedOnThatVenuesEvidence.
	FactTapIsJudgedAtTheTappedVenue Fact = "checkin:tapped venue's ranges, point and shift"
	// A tap at a venue other than the person's home venue is decided as it would be at
	// home -- the same verdict, the same trust --, the record's note says it was at
	// another location (base:cross-location-note, a note rule a business can switch off)
	// and the person's own screen does not. Derived by asking tap.Decide, and from the
	// baseline.
	FactCrossLocationTapIsNotedNotPenalised Fact = "tap:CrossLocation noted on the record, not on the screen, verdict unchanged"
	// The Roll marks a day whose shift started at a venue other than the person's home
	// venue, counts its hours like any other, and says so in its legend and receipt.
	// Derived from internal/domain/ledger (DayMarks.Other) and pages/reports.templ.
	FactRollMarksWorkAtAnotherVenue Fact = "ledger:DayMarks.Other+templ:roll legend and receipt"
	// A session comes only from activation, and activation ends only on a real NFC tap:
	// a QR scan (no SUN) is refused there, finishActivation is its one caller of the
	// session issue, and nothing else in the product creates a session. So a phone that
	// cannot tap a plaque is never set up, and QR is a fallback for a phone already set
	// up. Derived from internal/handler/activate.go, internal/sun and every Go file that
	// could issue a session.
	FactSetupTakesARealTap Fact = "handler:session only via CompleteByTap(HasSUN) → finishActivation"
	// A check-in left open past the engine's stale threshold is closed by the person's
	// next tap as a check-out WITH a note, the report does not pay that interval and
	// lists the check-in as open, and a check-out a manager types closes it for pay.
	// Derived by asking tap.Decide, and from internal/domain/ledger's
	// TestAccumulate_TheRestaurantsPagesWeek.
	FactForgottenCheckOutIsNotPaid Fact = "tap:stale open → out+note; ledger:too long → open, manual out pays"
)

// The page's sections' ids, in the page's order.
const (
	RestaurantsOvernightID = "overnight"
	RestaurantsVenuesID    = "venues"
	RestaurantsCoverID     = "cover"
	RestaurantsForgotID    = "forgot"
	RestaurantsDayID       = "day-to-day"
	RestaurantsHoursID     = "hours"
)

// RestaurantsVenue is one venue of the chain as the demo seed records it: its name, its
// shift (local wall-clock, as the venue's page sets it), whether the shift runs past
// midnight, and its address range on file.
type RestaurantsVenue struct {
	Name      string
	Start     string
	End       string
	Overnight bool
	Network   string
}

// Hours is the venue's shift as the page prints it: "18:00–02:00".
func (v RestaurantsVenue) Hours() string { return v.Start + "–" + v.End }

// RestaurantsVenues is the Kebab Factory chain of the demo seed (test/fixtures/seed.sql),
// in the seed's order. internal/handler holds every field to the seed.
var RestaurantsVenues = []RestaurantsVenue{
	{Name: "KF Hamrun", Start: "10:00", End: "22:00", Network: "203.0.113.10/32"},
	{Name: "KF Mellieha", Start: "10:00", End: "22:00", Network: "203.0.113.11/32"},
	{Name: "KF Msida", Start: "10:00", End: "22:00", Network: "203.0.113.12/32"},
	{Name: "KF Valletta", Start: "10:00", End: "22:00", Network: "203.0.113.13/32"},
	{Name: "KF San Gwann", Start: "10:00", End: "22:00", Network: "203.0.113.14/32"},
	{Name: "KF St Julians", Start: "10:00", End: "22:00", Network: "203.0.113.15/32"},
	{Name: "KF Paceville", Start: "11:00", End: "23:00", Network: "203.0.113.16/32"},
	{Name: "Rusty Bar", Start: "18:00", End: "02:00", Overnight: true, Network: "203.0.113.17/32"},
	{Name: "KF Headquarter", Start: "09:00", End: "17:00", Network: "203.0.113.18/32"},
}

// RestaurantsVenueNamed is the venue of RestaurantsVenues called name, or the zero venue.
func RestaurantsVenueNamed(name string) RestaurantsVenue {
	for _, v := range RestaurantsVenues {
		if v.Name == name {
			return v
		}
	}
	return RestaurantsVenue{}
}

// RestaurantsDemoLabel is the label beside the real table (K1). It is a label, not a claim.
const RestaurantsDemoLabel = "from our demo data"

// --- the samples ------------------------------------------------------------------------
//
// SAMPLE DATA, and the page says so. Every docket is run through tap.Decide by
// internal/handler and must come out stamped as drawn.

// RestaurantsOvernightIn is Luke Borg's check-in on the Monday evening at Rusty Bar; the
// check-out that closes it is The Pass's 02:04 docket (RestaurantsOvernightOut).
var RestaurantsOvernightIn = PanelDocket{Venue: "Rusty Bar", Who: "Luke Borg", Direction: "IN", At: "17:58",
	Channel: "nfc", IP: "yes", GPS: "no", Verdict: "ok", Trust: "70"}

// RestaurantsOvernightOut is the landing panel's own Rusty Bar check-out.
var RestaurantsOvernightOut = LandingPanelPassDockets[3]

// RestaurantsOvernightDays are the two dockets' days, as the docket's label prints them.
var RestaurantsOvernightDays = [2]string{"MON", "TUE"}

// RestaurantsOvernightMinutes is the shift the two dockets make: Luke Borg's Monday on The
// Roll (LandingPanelRollRows), the hours booked on the day the shift started.
var RestaurantsOvernightMinutes = int(LandingPanelRollRows[3].Days[0])

// RestaurantsTimelineAxis is the overnight drawing's hour marks, every two hours from
// 16:00 to 04:00. The stylesheet places the shift and the two taps on the same twelve
// hours, and internal/handler holds its positions to these times.
var RestaurantsTimelineAxis = []string{"16:00", "18:00", "20:00", "22:00", "00:00", "02:00", "04:00"}

// RestaurantsCoverHome is Maria Vella's home venue, as The Roll's sample names it.
var RestaurantsCoverHome = LandingPanelRollRows[0].HomeVenue

// RestaurantsCoverDocket is Maria Vella's check-in on the Thursday she covered KF Msida.
var RestaurantsCoverDocket = PanelDocket{Venue: "KF Msida", Who: "Maria Vella", Direction: "IN", At: "10:04",
	Channel: "nfc", IP: "yes", GPS: "no", Verdict: "ok", Trust: "70"}

// RestaurantsCoverDay is the day of the cover, the Thursday The Roll marks as worked at
// another venue; RestaurantsCoverDayShort is how the docket's label prints it.
const (
	RestaurantsCoverDay      = "Thursday"
	RestaurantsCoverDayShort = "THU"
	// RestaurantsCoverPlaque is an invented plaque id, written as the plaque table
	// prints one.
	RestaurantsCoverPlaque = "04A1C33C2B7E10"
)

// RestaurantsCoverNote is the sentence the cross-location rule writes on the record
// (base:cross-location-note); internal/handler holds it to the engine's note.
const RestaurantsCoverNote = "tapped at a location other than the employee's home location (recorded as cross-location)"

// RestaurantsStaleHours is how long a check-in may stay open before the next tap that
// closes it is noted as a probable forgotten check-out, and the report stops paying the
// interval: tap.StaleOpenIn, in hours. A MIRROR OF THAT CONSTANT (pages does not import
// the engine), held equal to it by internal/handler's tests.
const RestaurantsStaleHours = 18

// RestaurantsForgotStep is one card of the forgotten check-out's drawing.
type RestaurantsForgotStep struct {
	No     string
	Title  string
	Detail string
}

// RestaurantsForgotSteps is an open check-in, the list a manager reads it from and the
// check-out a manager types: the details are the panel's own words ("Open check-ins, not
// counted" is The Roll's receipt line, "Entered by a manager" the docket's mark).
var RestaurantsForgotSteps = []RestaurantsForgotStep{
	{No: "01", Title: "A check-in nobody closed", Detail: "KF Hamrun · IN 10:02 · no check-out"},
	{No: "02", Title: "Listed for a manager", Detail: "Open check-ins, not counted"},
	{No: "03", Title: "Closed by hand", Detail: "Entered by a manager · OUT 22:05"},
}

// RestaurantsRollLegend is The Roll's legend, in the panel's own words
// (pages/reports.templ rollLegend), for the drawing of it.
var RestaurantsRollLegend = []string{
	"worked",
	"at a venue other than their home venue",
	"open check-in, not counted",
	"no hours counted",
}

// --- the copy ---------------------------------------------------------------------------

// RestaurantsCase is one card of "Day to day": its label and its sentence.
type RestaurantsCase struct {
	Label string
	Text  Line
}

// RestaurantsText is every sentence of the page. Every Line inside it, at any depth, is
// collected by internal/handler's tests by reflection, so a Line added here is checked
// for its pins and for being rendered.
type RestaurantsText struct {
	Lede  Line
	Built Line

	OvernightHeading Line
	OvernightLede    Line
	OvernightBullets []Line

	VenuesHeading Line
	VenuesLede    Line
	VenuesBullets []Line
	VenuesChip    Line

	CoverHeading Line
	CoverLede    Line
	CoverBullets []Line

	ForgotHeading Line
	ForgotBullets []Line

	CasesHeading Line
	Cases        []RestaurantsCase

	HoursHeading Line
	HoursBullets []Line
}

// RestaurantsCopy is every sentence of the page. The landing's "Multi-location" card
// opens it word for word, the times are the samples' and the hours the seed's, so no
// number is typed below.
var RestaurantsCopy = restaurantsCopy()

func restaurantsCopy() RestaurantsText {
	l := func(text string, facts ...Fact) Line { return Line{Text: text, Facts: facts} }
	fits := LandingAudiences[0]
	rusty, hamrun := RestaurantsVenueNamed("Rusty Bar"), RestaurantsVenueNamed("KF Hamrun")
	return RestaurantsText{
		Lede: l(fits.Paragraph(),
			FactVenueHoldsItsEvidenceAndHours, FactPlaqueIdentifiesThePlace, FactDirectionFollowsLastOpenCheckIn),
		Built: l("Built for a chain where staff cover each other's venues and the bar closes after midnight.",
			FactCrossLocationTapIsNotedNotPenalised, FactDirectionFollowsLastOpenCheckIn),

		OvernightHeading: l("A shift that ends after midnight is still one shift.",
			FactDirectionFollowsLastOpenCheckIn, FactShiftCountsOnItsStartDayAndVenue),
		OvernightLede: l(fits.Points[1].Text, FactDirectionFollowsLastOpenCheckIn),
		OvernightBullets: []Line{
			l("In or out is decided by the person's last open check-in, so the "+RestaurantsOvernightOut.At+
				" tap closes the "+RestaurantsOvernightIn.At+" one.", FactDirectionFollowsLastOpenCheckIn),
			l("The hours land on the day the shift started, at the venue where it started.",
				FactShiftCountsOnItsStartDayAndVenue),
			l("Lateness is measured against the shift of the venue where the person tapped — "+rusty.Start+
				" at "+rusty.Name+", "+hamrun.Start+" at "+hamrun.Name+" — unless their department has a shift of its own.",
				FactLatenessFollowsOwnShift, FactTapIsJudgedAtTheTappedVenue),
		},

		VenuesHeading: l("Each venue keeps its own hours and its own network.", FactVenueHoldsItsEvidenceAndHours),
		VenuesLede:    l(fits.Points[0].Text, FactPlaqueIdentifiesThePlace, FactVenueHoldsItsEvidenceAndHours),
		VenuesBullets: []Line{
			l("Plaques are bound to a venue, so the venue of a tap comes from the plaque, not from the phone.",
				FactPlaqueIdentifiesThePlace, FactTapIsJudgedAtTheTappedVenue),
			l("A venue's address list counts only when it is narrow enough to stand for a single network; "+
				"a list that is too wide is refused when you save it.", FactWideAddressListIsNoEvidence),
			l("If a phone's NFC is off or fails on the day, the same page opens from the venue's QR code; by default "+
				"a QR check-in needs the venue's network, and on a position alone it is flagged for a manager. "+
				"Setting a phone up still takes one real tap on a plaque.",
				FactTapPageNeedsNoApp, FactCodelessTapNeedsAddress, FactFlaggedQueueDecidedByAManager,
				FactActivationEndsOnARealTap, FactSetupTakesARealTap),
		},
		VenuesChip: LandingPricingIncludes[0],

		CoverHeading: l("Staff can cover any venue in the chain.",
			FactTapIsJudgedAtTheTappedVenue, FactCrossLocationTapIsNotedNotPenalised),
		CoverLede: l("By default, a tap at another venue is judged on that venue's network and position, exactly as a "+
			"tap at home would be, and its lateness is measured against that venue's shift unless the person's "+
			"department has one.",
			FactTapIsJudgedAtTheTappedVenue, FactCrossLocationTapIsNotedNotPenalised, FactLatenessFollowsOwnShift),
		CoverBullets: []Line{
			l("By default the record carries a note that the tap was at another location; the employee's "+
				"screen does not show it.", FactCrossLocationTapIsNotedNotPenalised),
			l("The Roll marks those days as worked at another venue, and their hours count like any others.",
				FactRollMarksWorkAtAnotherVenue),
		},

		ForgotHeading: l("A check-in left open for more than "+itoa(RestaurantsStaleHours)+" hours is not counted "+
			"until a manager closes it.",
			FactForgottenCheckOutIsNotPaid, FactOpenCheckInIsListedNotCounted, FactManualEntryNamesAManager),
		ForgotBullets: []Line{
			l("A tap more than "+itoa(RestaurantsStaleHours)+" hours after a check-in closes it with a note; The Roll "+
				"lists the check-in as open and does not count it, and a manager enters the real check-out by hand. "+
				"A tap sooner than that closes it as an ordinary check-out, and its hours count.",
				FactForgottenCheckOutIsNotPaid, FactOpenCheckInIsListedNotCounted, FactManualEntryNamesAManager,
				FactDirectionFollowsLastOpenCheckIn),
			l("The Roll says how many check-ins are open, and its week's figure says it is short by them.",
				FactOpenCheckInIsListedNotCounted),
		},

		CasesHeading: l("Hiring, leaving, broken plaques, dead Wi-Fi.",
			FactPeopleAreInvitedFromTheDashboard, FactDeactivatedPersonIsRefused, FactPlaqueCanBeReplaced,
			FactGPSAloneApprovesATap),
		Cases: []RestaurantsCase{
			{Label: "New-hire setup", Text: l("Invite link, activated on their own phone",
				FactPeopleAreInvitedFromTheDashboard, FactActivationEndsOnARealTap)},
			{Label: "When someone leaves", Text: l("Switch off in the dashboard — the next tap is refused",
				FactDeactivatedPersonIsRefused)},
			{Label: "If it breaks", Text: l("Stick a spare plaque and swap it in the dashboard",
				FactPlaqueCanBeReplaced)},
			{Label: "Internet down on site", Text: l("Phones fall back to mobile data + GPS",
				FactGPSAloneApprovesATap)},
		},

		HoursHeading: l("Hours per person and per venue, ready for payroll.",
			FactReportPerPersonAndVenue, FactHoursExportAsCSV),
		HoursBullets: []Line{LandingPricingIncludes[3], LandingPricingIncludes[4]},
	}
}

// --- the drawings' names ------------------------------------------------------------------
//
// Each drawing is a picture (role img) and its accessible name is built here from the
// same values it draws, so the name cannot say another time or another venue.

// RestaurantsChainCount is how many venues the chain card draws, in words ("nine").
func RestaurantsChainCount() string { return countWord(len(RestaurantsVenues)) }

// RestaurantsChainLabel is the chain card's accessible name.
func RestaurantsChainLabel() string {
	return "Sample setup from our demo data: " + RestaurantsChainCount() +
		" venues, each with its own plaque and hours, and one dashboard over them"
}

// RestaurantsTimelineLabel is the overnight drawing's accessible name: the two taps, the
// shift they make, and the two dockets' stamp.
func RestaurantsTimelineLabel() string {
	in, out := RestaurantsOvernightIn, RestaurantsOvernightOut
	label := "Sample: at " + in.Venue + ", a check-in at " + in.At + " on Monday and a check-out at " + out.At +
		" on Tuesday are paired into one shift of " + RollHours(RestaurantsOvernightMinutes)
	if in.Verdict == "ok" && out.Verdict == "ok" {
		label += "; both approved"
	}
	return label
}

// RestaurantsCoverLabel is the cover drawing's accessible name.
func RestaurantsCoverLabel() string {
	d := RestaurantsCoverDocket
	return "Sample: " + d.Who + "'s home venue is " + RestaurantsCoverHome + "; she taps in at " + d.Venue +
		" on " + RestaurantsCoverDay + " and the approved record carries the cross-location note"
}

// RestaurantsForgotLabel is the name of the forgotten check-out's list of steps.
const RestaurantsForgotLabel = "A forgotten check-out, step by step · sample"
