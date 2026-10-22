package pages

import "strconv"

// THE PRODUCTION PAGE (LP-10, 2026-10-22): /for/production, drawn from the
// ForProduction / T-ForProduction / M-ForProduction boards
// (docs/design/landing-redesign/boards). It joins the public site the way the pricing,
// security and product pages did (pricingview.go records the shape): a row of
// PublicPages, a named route in internal/handler's Mount, every sentence a value here
// with the facts it rests on, and the one number it prints -- the repeat window --
// handed in by internal/handler.
//
// ONE SITE, MANY TEAMS. The page walks the second shape of business the landing's
// "Built for both shapes of business" names: one plaque at the gate, departments
// resolved from each person's profile, each department judged against its own shift
// (CLAUDE.md §5), a queue at the gate at a shift change (the repeat window is the
// PERSON's, never the plaque's), the QR fallback that needs the venue's network, and
// what happens to a check-in nobody closed.
//
// THE TABLES ARE THE DEMO DATA, NOT SAMPLES (README K1). The departments, their shift
// hours and the site's own hours are test/fixtures/seed.sql's, and the page says
// "from our demo data" beside them; internal/handler's tests read the seed and hold
// every row equal. The PEOPLE on the dockets are invented (none is a person of the
// seed), and every drawing that shows them says it is a sample.
//
// WHERE THE BOARD SAID MORE THAN THE PRODUCT DOES, the value here says less, and the
// edit is recorded once, for the reader looking for it:
//
//   - "Start free — 3 months on us" (the opening's button) says "Start free", and the
//     sign-off is the landing's, which counts the free months the code's way (K6).
//   - "One plaque · five departments · sample setup" says "from our demo data": the
//     departments and their hours are the seed's (K1), not a sample.
//   - "someone with no department is measured against the site's own hours" says "the
//     hours of the site they tapped at": the yardstick is the tapped venue's shift,
//     not a home site (FactLatenessFollowsOwnShift).
//   - "Same minute, different yardsticks" says "Different yardsticks at one gate": the
//     two check-ins it compares are 53 minutes apart.
//   - "The tap screen has one button, at least 64 pixels tall." says what the button
//     is built for instead of its pixels, as the landing's and the product tour's
//     sentences on the same rule do.
//   - The QR answer says "by default": base:qr-requires-ip is a baseline rule an owner
//     may change (the security page's wording).
//   - The board's queue draws two people of the seed (Saviour Bonnici, Dragan Nikolic)
//     as sample taps; the drawing is a sample, and sample people are invented, so two
//     invented names stand in their place.
//   - The board's hero card draws no network; the plant's own network is the seed's
//     198.51.100.0/29, which the page does NOT print: the unmeasured-claim scanner reads
//     "100" in it as a figure, so the card says "IP ✓ · GPS ✓" the way the product
//     tour's venue cards do, and the test holds both signs to the seed.
//   - The board says nothing about a check-in nobody closed; the page adds The Roll's
//     own sentence for it (the card asks for the forgotten check-out).

// ForProductionPath is the page's URL.
const ForProductionPath = "/for/production"

// ForProductionView is the production page.
type ForProductionView struct {
	// SignupHref and SignInHref are the chrome's two ways in, as on LandingView.
	SignupHref string
	SignInHref string
	// FreeMonths is the founding offer's count, for the sign-off.
	FreeMonths int
	// Figures are the page's numbers; Copy is every sentence built from them, computed
	// once by internal/handler (ForProductionCopy).
	Figures ForProductionFigures
	Copy    ForProductionText
}

// ForProductionFigures is every number the page prints that the product decides, each
// read from the product by internal/handler.
type ForProductionFigures struct {
	// DebounceSeconds is the deployment's default repeat window for the same person
	// (policy.DefaultParams, internal/config).
	DebounceSeconds int
}

// The facts only this page claims. Each is derived from the product in
// internal/handler/forproduction_facts_test.go; the closure test reads this block like
// the one in landingview.go.
const (
	// A record's department is the person's own: the check-in reads the employee's
	// department and writes it onto the record (internal/domain/checkin), and the record
	// keeps it (AnchorDepartmentOnEveryRecord).
	FactDepartmentComesFromThePerson Fact = "checkin:record's department_id is the employee's"
	// The Pass and People each narrow their list to one department: both handlers read
	// a department filter, both queries apply it, and both screens draw its select.
	// Derived from internal/handler/transactions.go and employees.go, db/queries'
	// ListPanelTransactions and ListPanelEmployees, and the two filter templates.
	FactPassAndPeopleFilterByDepartment Fact = "handler+query:department filter on The Pass and People"
	// The Roll counts a person's late arrivals and adds up the minutes: the ledger counts
	// an arrival late only when it is past the shift's start, sums the minutes, and the
	// report prints "N late arrival(s) · Hh MMm". Derived from internal/domain/ledger and
	// internal/handler/reports.go.
	FactRollCountsLateArrivalsAndMinutes Fact = "ledger:LateShifts+LateBy past the start+Roll's late words"
	// Every tap the engine decides is written -- ok, flag, reject and ignored alike; only
	// a redirect (no session, an activation) writes nothing. Derived from
	// internal/domain/checkin (the one return between Decide and the write is the
	// redirect's) and tap.Decide (a repeat is ignored and is no redirect).
	FactEveryDecidedTapIsWritten Fact = "checkin:Decide then write, unless a redirect"
	// A plaque belongs to a venue and to no department: tags carries a location and no
	// department column, so every department of a site shares its plaques. Derived from
	// migration 00004 (and every later one), and AnchorPlaqueBelongsToVenue.
	FactPlaqueKnowsNoDepartment Fact = "schema:tags carries no department"
)

// The page's section ids, in its order.
const (
	ForProductionShiftsID      = "shifts"
	ForProductionDepartmentsID = "departments"
	ForProductionQueueID       = "queue"
	ForProductionFloorID       = "floor"
)

// --- the demo data (K1) -------------------------------------------------------------------
//
// THE SEED'S, ROW FOR ROW: internal/handler's tests read test/fixtures/seed.sql and hold
// the site, its hours and every department with its hours equal to these, in both
// directions (a department added to or removed from the seed turns the test red).

// ForProductionSite is the seed's one Kebab Manufacturing site.
const ForProductionSite = "KM Plant"

// ForProductionShift is a shift as the seed records it: local wall-clock start and end.
type ForProductionShift struct {
	Name  string
	Start string
	End   string
}

// ForProductionSiteHours is the site's own shift: the yardstick of a person with no
// department.
var ForProductionSiteHours = ForProductionShift{Name: ForProductionSite, Start: "09:00", End: "17:00"}

// ForProductionDepartments are the site's departments and their shifts, earliest start
// first, as the boards order them.
var ForProductionDepartments = []ForProductionShift{
	{Name: "KM Pastry & Bakery", Start: "04:00", End: "12:00"},
	{Name: "KM Meat Production", Start: "05:00", End: "13:00"},
	{Name: "KM Central Kitchen", Start: "06:00", End: "14:00"},
	{Name: "KM Warehouse & Supply", Start: "08:00", End: "16:00"},
	{Name: "KM General", Start: "09:00", End: "17:00"},
}

// The two departments the opening's sentence and the yardstick drawing name.
const (
	ForProductionBakery = "KM Pastry & Bakery"
	ForProductionMeat   = "KM Meat Production"
)

// ForProductionDepartment is the department named name, or a zero shift.
func ForProductionDepartment(name string) ForProductionShift {
	for _, d := range ForProductionDepartments {
		if d.Name == name {
			return d
		}
	}
	return ForProductionShift{}
}

// --- the samples ---------------------------------------------------------------------------
//
// SAMPLE DATA, and the page says so. Every docket is held to the engine by
// internal/handler: tap.Decide decides each one the way it is stamped (a real NFC tap on
// the site's network), the yardstick's lateness is tap.MinutesLate against the
// department's seed shift, and The Roll's words for it are the report's own.

// ForProductionDocket is one sample record at the site, as the panel's docket draws it:
// the department, the person, the direction ("IN", "OUT", or "NONE" for a verdict that
// carries none, the panel's word), the local time, the engine's verdict and a note.
type ForProductionDocket struct {
	Department string
	Who        string
	Direction  string
	At         string
	Verdict    string
	Note       string
}

// ForProductionYardstick is the shifts drawing's two check-ins: the same gate, two
// departments, two yardsticks.
var ForProductionYardstick = []ForProductionDocket{
	{Department: ForProductionBakery, Who: "Anna Farrugia", Direction: "IN", At: "04:02", Verdict: "ok"},
	{Department: ForProductionMeat, Who: "Mark Spiteri", Direction: "IN", At: "04:55", Verdict: "ok"},
}

// ForProductionLateRoll is what The Roll prints for the first yardstick check-in: one
// late arrival and the minutes it adds up to (internal/handler/reports.go's words).
const ForProductionLateRoll = "1 late arrival · 0h 02m"

// ForProductionPassDockets is The Pass filtered to one department: the bakery's morning,
// newest first.
var ForProductionPassDockets = []ForProductionDocket{
	{Department: ForProductionBakery, Who: "Anna Farrugia", Direction: "IN", At: "04:02", Verdict: "ok"},
	{Department: ForProductionBakery, Who: "Joseph Galea", Direction: "IN", At: "03:57", Verdict: "ok"},
	{Department: ForProductionBakery, Who: "Rita Mifsud", Direction: "IN", At: "03:52", Verdict: "ok"},
}

// ForProductionQueue is the shift change at the gate: three different people seconds
// apart, then the first of them again inside the repeat window.
var ForProductionQueue = []ForProductionDocket{
	{Department: ForProductionMeat, Who: "Kurt Bonello", Direction: "IN", At: "04:58:10", Verdict: "ok"},
	{Department: ForProductionMeat, Who: "Marko Petrovic", Direction: "IN", At: "04:58:14", Verdict: "ok"},
	{Department: ForProductionMeat, Who: "Karl Azzopardi", Direction: "IN", At: "04:58:19", Verdict: "ok"},
	{Department: ForProductionMeat, Who: "Kurt Bonello", Direction: "NONE", At: "04:58:40", Verdict: "ignored",
		Note: "same person, 30 s later"},
}

// ForProductionTapSample is the person the floor's phone greets, at the site.
var ForProductionTapSample = ForProductionYardstick[0]

// ForProductionSampleNote is printed beside the drawings of invented people. It is a
// label, not a claim.
const ForProductionSampleNote = "Sample names and times, drawn for this page."

// ForProductionPassFilterLabel is the label The Pass's filter bar gives its department
// select (components/filterbar.templ).
const ForProductionPassFilterLabel = "Department"

// --- the copy ------------------------------------------------------------------------------

// ForProductionCard is one card of "On the floor": its title and its sentence.
type ForProductionCard struct {
	Title Line
	Body  Line
}

// ForProductionText is every sentence of the page, built from the figures and the demo
// data by ForProductionCopy. Every Line inside it, at any depth, is collected by
// internal/handler's tests by reflection, so a Line added here is checked for its pins
// and for being rendered.
type ForProductionText struct {
	Lede     Line
	LedeFoot Line
	SiteNote Line

	ShiftsHeading Line
	ShiftsLede    Line
	Yardstick     Line
	ShiftsBullets []Line

	DepartmentsHeading Line
	DepartmentsLine    Line
	DepartmentsChip    Line

	QueueHeading Line
	QueueLede    Line
	QueueBullets []Line

	FloorHeading Line
	FloorCards   []ForProductionCard
}

// ForProductionCopy is every sentence of the page for the figures f. Each number in a
// sentence is one of f's or a value of the demo data above; no number is written below.
func ForProductionCopy(f ForProductionFigures) ForProductionText {
	l := func(text string, facts ...Fact) Line { return Line{Text: text, Facts: facts} }
	bakery, meat := ForProductionDepartment(ForProductionBakery), ForProductionDepartment(ForProductionMeat)
	n := len(ForProductionDepartments)
	anna, mark := ForProductionYardstick[0], ForProductionYardstick[1]
	return ForProductionText{
		Lede: l("One plaque at the gate; departments resolve automatically from each employee's profile — bakers "+
			"against their "+bakery.Start+" shift, meat production against "+meat.Start+". No extra hardware per floor.",
			FactDepartmentComesFromThePerson, FactLatenessFollowsOwnShift, FactPlaqueKnowsNoDepartment),
		LedeFoot: l("Built for one site with many teams, each starting at its own hour.",
			FactVenuesAndDepartmentsCarryOwnHours),
		SiteNote: l(strconv.Itoa(n)+" departments · per-department shifts · records filtered by department",
			FactVenuesAndDepartmentsCarryOwnHours, FactPassAndPeopleFilterByDepartment),

		ShiftsHeading: l("One gate, "+countWord(n)+" start times.", FactVenuesAndDepartmentsCarryOwnHours),
		ShiftsLede: l("Lateness is measured against the person's department shift; someone with no department is "+
			"measured against the hours of the site they tapped at.", FactLatenessFollowsOwnShift),
		Yardstick: l("Different yardsticks at one gate: against the bakery's "+bakery.Start+", "+firstName(anna.Who)+"'s "+
			anna.At+" is "+ForProductionLateRoll+" on The Roll; against meat production's "+meat.Start+", "+
			firstName(mark.Who)+"'s "+mark.At+" is not late.",
			FactLatenessFollowsOwnShift, FactRollCountsLateArrivalsAndMinutes),
		ShiftsBullets: []Line{
			l("The Roll counts each person's late arrivals and the minutes they add up to.",
				FactRollCountsLateArrivalsAndMinutes),
			l("A check-in with no check-out is listed on The Roll, not counted, and the week's figure says it is short.",
				FactOpenCheckInIsListedNotCounted),
			l("A manager can enter the missing record by hand from the person's card, marked as entered by a manager.",
				FactManualEntryStartsFromThePerson, FactManualEntryNamesAManager),
		},

		DepartmentsHeading: l("Every record knows its department.", FactDepartmentComesFromThePerson),
		DepartmentsLine: l("A person's department travels on each of their records. The Pass and People filter by it, "+
			"so each team's day reads on its own.", FactDepartmentComesFromThePerson, FactPassAndPeopleFilterByDepartment),
		DepartmentsChip: l("Locations & departments, each on its own hours", FactVenuesAndDepartmentsCarryOwnHours),

		QueueHeading: l("A queue at the gate, one tap each.", FactDebounceIsPerPersonOnTheServerClock),
		QueueLede: l("Debounce is per person: people in a line can tap the same plaque one after another, and each tap "+
			"is recorded. Only the same person tapping again within "+strconv.Itoa(f.DebounceSeconds)+
			" seconds (the default window) is ignored.",
			FactDebounceIsPerPersonOnTheServerClock, FactEveryDecidedTapIsWritten),
		QueueBullets: []Line{
			l("The ignored tap is still written down, stamped IGNORED.",
				FactEveryDecidedTapIsWritten, FactRecordedTapEndsOnAStampedDocket),
		},

		FloorHeading: l("Gloves, flour, and no app to install.", FactTapButtonSizedForAWetHand, FactTapPageNeedsNoApp),
		FloorCards: []ForProductionCard{
			{
				Title: l("No extra hardware per floor.", FactPlaqueKnowsNoDepartment),
				Body: l("Every department can share the one plaque at the gate.",
					FactPlaqueKnowsNoDepartment, FactDepartmentComesFromThePerson),
			},
			{
				Title: l("Built for wet or floury hands.", FactTapButtonSizedForAWetHand),
				Body: l("The tap screen has one large button, sized for a wet or gloved finger.",
					FactTapButtonSizedForAWetHand, FactTapScreenIsOneButton),
			},
			{
				Title: l("What if the phone has no NFC?", FactTapPageNeedsNoApp),
				Body: l("The same page opens from a QR code with the camera. A QR tap carries no cryptographic "+
					"signature — no proof of a physical touch — so by default it needs your venue's network address to "+
					"match before it's approved. A position on its own is not enough: without the address it's stored, "+
					"flagged and queued for review.",
					FactTapPageNeedsNoApp, FactCodelessTapNeedsAddress, FactFlaggedQueueDecidedByAManager),
			},
		},
	}
}

// firstName is the first word of a sample person's name.
func firstName(who string) string {
	for i, r := range who {
		if r == ' ' {
			return who[:i]
		}
	}
	return who
}
