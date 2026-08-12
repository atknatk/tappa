package handler

// anomalies_test.go -- M6-11's unit-level nets.
//
// WHAT IS HERE AND WHAT IS IN anomalies_db_test.go. §4.5, the queries' own shape and
// "which rows produce which counts" are measured against real Postgres, because a fake
// reader can only ever confirm what the test told it. What is here is what a fake CAN
// prove: the boundary between what the URL asks for and what the domain is handed, the
// sentences the screen is obliged to print and the ones it is forbidden to, the
// arithmetic of a rate, and the property that this section offers NO WRITE.

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/web/templates/pages"
)

func anomalyBrowser(t *testing.T, records *fakeLedger) *browser {
	t.Helper()
	return panelBrowserWithRoster(t, records)
}

// anomaliesWith builds a screen for the fixture week, mutated by the caller.
func anomaliesWith(mutate func(*ledger.Anomalies)) ledger.AnomalyScreen {
	a := ledger.Anomalies{
		Queried:               true,
		Zone:                  time.UTC,
		Period:                weekOfUTC(),
		TogetherWithinSeconds: int(ledger.TogetherWithin / time.Second),
		TogetherMinDays:       ledger.TogetherMinDays,
	}
	if mutate != nil {
		mutate(&a)
	}
	return ledger.AnomalyScreen{Anomalies: a, TenantName: "Kebab Factory Ltd"}
}

// TestAnomaliesSection_AsksTheDomainForTheWeekTheURLNAMES is the boundary §7 asks for:
// the handler validates, and the domain sees data that is already good.
func TestAnomaliesSection_AsksTheDomainForTheWeekTheURLNAMES(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  ledger.Date
	}{
		{name: "a valid ISO day", query: "?week=2026-08-05", want: ledger.Date{Year: 2026, Month: time.August, Day: 5}},
		{
			// DROPPED RATHER THAN REFUSED, for the reason the reports section gives:
			// dropping can only move the reader inside their own tenant, and it is
			// visible because the picker echoes the week that took effect.
			name: "a date that does not parse", query: "?week=not-a-date", want: ledger.Date{},
		},
		{name: "no week at all", query: "", want: ledger.Date{}},
		{name: "a hostile value", query: "?week=2026-08-05%27%20OR%201=1", want: ledger.Date{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			records := newFakeLedger()
			records.anomalies = anomaliesWith(nil)
			b := anomalyBrowser(t, records)
			if rec := b.do(http.MethodGet, anomaliesHref+c.query, nil); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := records.lastAnomalyFilter(t).Day; got != c.want {
				t.Fatalf("the domain was asked for %v, want %v", got, c.want)
			}
		})
	}
}

// TestAnomaliesSection_AFailedReadIsNeverAQuietWeek is §4.6, and on THIS section it is
// the single most important assertion in the file.
//
// 🔴 A ZERO SCREEN AND A CLEAN WEEK RENDER THE SAME WAY unless something separates them.
// "Nothing odd happened this week" is a claim about a business, and a database timeout
// is not evidence for it — a manager who reads that sentence stops looking. So a failed
// read must produce a problem page, and the section must not fall through.
func TestAnomaliesSection_AFailedReadIsNeverAQuietWeek(t *testing.T) {
	records := newFakeLedger()
	records.anomaliesErr = errors.New("connection refused")
	b := anomalyBrowser(t, records)

	rec := b.do(http.MethodGet, anomaliesHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 — a failed read must not render as a clean week", rec.Code)
	}
	body := rec.Body.String()
	for _, forbidden := range []string{
		"Records examined this week",
		"Nobody carries one of these counts",
		"Every check-in this week has a checkout after it",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("a failed read rendered %q, which is a measured claim about a week "+
				"nobody measured", forbidden)
		}
	}
	// POSITIVE CONTROL: the same browser really does render the page when the read
	// works, so the absences above are the failure branch rather than a broken route.
	records.anomaliesErr = nil
	records.anomalies = anomaliesWith(nil)
	if rec := b.do(http.MethodGet, anomaliesHref, nil); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "Records examined this week") {
		t.Fatalf("the section does not render on a successful read (%d); every assertion "+
			"above would be vacuous", rec.Code)
	}
}

// TestAnomaliesSection_OffersNoWriteAtAll is §4.3 asserted on the rendered surface.
//
// 🔴 THE TEMPTING CONTROL ON A SCREEN LIKE THIS IS "DISMISS", and it must not exist. A
// signal a manager can silence is a signal the next manager never sees, and the record
// behind it is immutable. The section renders exactly one form — the week picker — and
// that form is a GET.
func TestAnomaliesSection_OffersNoWriteAtAll(t *testing.T) {
	records := newFakeLedger()
	records.anomalies = anomaliesWith(func(a *ledger.Anomalies) {
		// 🔴 Answerable IS SET, AND LEAVING IT AT ZERO WAS A SHAPE THE PRODUCT CANNOT
		// PRODUCE. Answerable is Records minus Unanswerable, so it can never be less
		// than a count read out of the snapshot; a zero here made outOfLine print
		// "1 of 0 after a counter gap". No assertion rested on it, which is the only
		// reason it was not a defect -- but a fixture that describes an impossible week
		// is a fixture the next assertion cannot be trusted against.
		a.Totals = ledger.AnomalyTotals{Records: 10, Judged: 10, Answerable: 10, DistanceOnly: 3, CounterGaps: 1}
		a.People = []ledger.AnomalyPerson{{Name: "Maria Borg", Records: 5, DistanceOnly: 3}}
		a.Open = []ledger.AnomalyOpen{{EmployeeName: "Maria Borg", At: weekOfUTC().From}}
		a.Masked = ledger.MaskedOpenTally{StillOpen: 1}
	})
	b := anomalyBrowser(t, records)
	rec := b.do(http.MethodGet, anomaliesHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// 🔴 THE SHELL'S SIGN-OUT IS THE ONE POST EVERY PANEL PAGE CARRIES, so the
	// assertion is about the SET of POST forms rather than their absence. Written as
	// "contains no method=post" this test was RED on its first run — against a correct
	// page — which is the shape that gets an assertion loosened until it sees nothing.
	posts := countPostForms(body)
	if posts != 1 {
		t.Errorf("§4.3: the page renders %d form(s) that POST, want exactly 1 (the shell's "+
			"sign-out). Nothing on this screen may change a record.", posts)
	}
	if !strings.Contains(body, `<form method="post" action="/admin/logout">`) {
		t.Error("the one POST form on the page is not the shell's sign-out, so the count " +
			"above is counting something else")
	}
	if strings.Contains(body, "csrf") || strings.Contains(body, "CSRF") {
		t.Error("§4.3: the anomalies section renders a synchronizer token, which only a " +
			"write path needs")
	}
	// AND THE ROUTE ITSELF REFUSES A POST, which is chi's answer for a GET-only route.
	if rec := b.do(http.MethodPost, anomaliesHref, nil); rec.Code == http.StatusOK {
		t.Errorf("POST %s answered 200; this section is registered as a read", anomaliesHref)
	}
	// POSITIVE CONTROL for the form scan: the week picker IS on the page, as a GET.
	if !strings.Contains(body, `method="get"`) {
		t.Error("the week picker is missing, so the POST scan above ran over a page with " +
			"no forms on it at all")
	}
}

// TestAnomaliesSection_SaysWhatItCannotSee is the M6-11 card's two source-less signals,
// asserted as text.
//
// 🔴 A SCREEN THAT SIMPLY OMITTED THEM WOULD READ AS "CHECKED, NOTHING FOUND", which is
// the failure class this repository has paid for most: a page that declares a guarantee
// its mechanism does not provide. Both absences are named on the page, in the words that
// say the product cannot see them rather than that they did not happen.
func TestAnomaliesSection_SaysWhatItCannotSee(t *testing.T) {
	records := newFakeLedger()
	records.anomalies = anomaliesWith(nil)
	b := anomalyBrowser(t, records)
	body := b.do(http.MethodGet, anomaliesHref, nil).Body.String()

	for _, want := range []string{
		"What this page does not see",
		"is not recorded anywhere",
		"only a coarse",
		// 🔴 THE THIRD ITEM IS A LIMIT OF THE PAIR QUERY, and it was written in the SQL
		// and nowhere a reader could reach. A limit only the author knows about is not
		// disclosed.
		"compares each tap with the ONE before it",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not say %q, so a reader would take the two missing "+
				"signals for two clean ones", want)
		}
	}
	// AND IT MUST NOT CLAIM THE OPPOSITE. These are the sentences a well-meaning edit
	// would produce, and each of them is a guarantee the mechanism does not provide.
	for _, forbidden := range []string{
		"no suspicious", "nothing suspicious", "all clear", "fully checked",
		"complete check", "exhaustive",
	} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Errorf("the page says %q, which is a guarantee this section does not provide",
				forbidden)
		}
	}
}

// TestAnomaliesSection_NeverLabelsAPerson is the card's own sentence: the report does not
// comment, it shows data.
func TestAnomaliesSection_NeverLabelsAPerson(t *testing.T) {
	records := newFakeLedger()
	records.anomalies = anomaliesWith(func(a *ledger.Anomalies) {
		a.Totals = ledger.AnomalyTotals{Records: 40, Judged: 40, Answerable: 40, DistanceOnly: 40, CounterGaps: 9}
		a.People = []ledger.AnomalyPerson{{
			Name: "Maria Borg", Records: 40, DistanceOnly: 40, CounterGaps: 9,
			OutsideVenue: 4, OtherVenue: 2,
		}}
		a.Pairs = []ledger.AnomalyPair{{
			First: "Maria Borg", Second: "Joseph Vella", Venue: "KF St Julians",
			Together: 12, Days: 6, ClosestSeconds: 2,
		}}
	})
	b := anomalyBrowser(t, records)
	body := strings.ToLower(b.do(http.MethodGet, anomaliesHref, nil).Body.String())

	for _, forbidden := range []string{
		"suspicious", "fraud", "cheat", "buddy punch", "abuser", "offender",
		"high risk", "severity", "risk score",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the page uses the word %q. The M6-11 card is explicit: the report "+
				"shows data and does not label anybody.", forbidden)
		}
	}
	// POSITIVE CONTROL: the row really is on the page, so the absences are about words
	// rather than about an empty listing.
	if !strings.Contains(body, "maria borg") {
		t.Fatal("the person's row is not on the page; every assertion above is vacuous")
	}
	// AND THE PAIR LIST PRINTS ITS OWN THRESHOLDS, so the filter is something a reader
	// can disagree with rather than trust.
	if !strings.Contains(body, "seconds of each other") || !strings.Contains(body, "separate days") {
		t.Error("the pair list does not print the thresholds it filtered by")
	}
}

// TestAnomalyRates_CarryTheirDenominatorAndUseIntegerArithmetic pins outOfLine, which is
// the one piece of arithmetic on this screen.
//
// 🔴 THE DENOMINATOR IS NEVER DROPPED, EVEN AT ZERO. "0 of 212" says a measurement was
// taken; "0" reads like the absence of one. And a zero denominator must not divide — it
// is reachable by an ordinary business that only types records.
func TestAnomalyRates_CarryTheirDenominatorAndUseIntegerArithmetic(t *testing.T) {
	for _, c := range []struct {
		n, of int
		want  string
	}{
		{0, 0, "0 of 0 x"},
		{0, 100, "0 of 100 x"},
		{1, 100, "1 of 100 x · 1%"},
		{47, 2273, "47 of 2273 x · 2%"},
		{1, 3, "1 of 3 x · 33%"},
		{3, 3, "3 of 3 x · 100%"},
		// A count LARGER than its denominator cannot arise from the queries (every
		// numerator is a FILTER over the same rows), but the formatter must not panic or
		// print nonsense if one ever does.
		{5, 2, "5 of 2 x · 250%"},
	} {
		if got := outOfLine(c.n, c.of, "x"); got != c.want {
			t.Errorf("outOfLine(%d, %d) = %q, want %q", c.n, c.of, got, c.want)
		}
	}
}

// TestOnNetworkLine_DescribesTheFlagTheEngineACTUALLYSETS is the card correction, made
// into a test.
//
// 🔴 THE M6-11 CARD CALLS THIS SIGNAL "the address matched but the position did not".
// internal/domain/tap's gpsFacts sets it from the DISTANCE ALONE. A screen that used the
// card's words would describe a mechanism the product does not have — which is exactly
// the class §7 of this task's brief forbids. So the flag is reported for what it is, and
// the address match is a separate number beside it, INCLUDING when that number is zero.
func TestOnNetworkLine_DescribesTheFlagTheEngineACTUALLYSETS(t *testing.T) {
	if got := onNetworkLine(0, 0); got != "" {
		t.Errorf("with nothing outside the venue the line is %q, want empty", got)
	}
	zero := onNetworkLine(0, 39)
	if !strings.Contains(zero, "0 of 39") {
		t.Errorf("a measured zero must still be a sentence; got %q", zero)
	}
	if !strings.Contains(zero, "None of them") {
		t.Errorf("a measured zero must SAY it is zero rather than going quiet; got %q", zero)
	}
	some := onNetworkLine(4, 39)
	if !strings.Contains(some, "4 of 39") {
		t.Errorf("got %q, want it to carry both numbers", some)
	}
	// AND NEITHER SENTENCE MAY CLAIM THE CARD'S MECHANISM.
	for _, line := range []string{zero, some} {
		if strings.Contains(strings.ToLower(line), "the address matched but") {
			t.Errorf("the sentence %q describes a mechanism the engine does not implement", line)
		}
	}
}

// TestAnomaliesView_CarriesNothingACoordinateCouldHideIn is §4.7 asserted STRUCTURALLY
// on the VIEW, which is the last surface before HTML.
//
// 🔴 IT IS THE TYPE CHECK RATHER THAN ONLY THE NAME CHECK, and the reason is written in
// the reports section's own matcher: "the honest general form needs a type checker
// rather than a name list". A field called `Detail` typed `map[string]any` passes every
// name list ever written and carries a whole policy snapshot onto a page. Here every
// leaf must be a string, an int or a bool — there is no third thing a count can be.
func TestAnomaliesView_CarriesNothingACoordinateCouldHideIn(t *testing.T) {
	seen := map[reflect.Type]bool{}
	fields := 0
	var dump []string

	var walk func(reflect.Type, string)
	walk = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			fields++
			dump = append(dump, path+rt.Name()+"."+f.Name+" "+f.Type.String())
			if why := bannedFieldName(f.Name); why != "" {
				t.Errorf("%s%s.%s is a field a coordinate or a secret could travel in (%s)",
					path, rt.Name(), f.Name, why)
			}
			switch f.Type.Kind() {
			case reflect.String, reflect.Bool, reflect.Int, reflect.Struct, reflect.Slice:
				// A struct or slice is walked below; the leaf kinds are the three a
				// count, a name or a yes/no can be.
				if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Uint8 {
					t.Errorf("%s%s.%s is a byte slice, which carries anything at all",
						path, rt.Name(), f.Name)
				}
			default:
				t.Errorf("%s%s.%s is of type %s. Every value this view carries is a "+
					"formatted string, a count or a flag; anything else is where a "+
					"coordinate or a snapshot would arrive under a harmless name.",
					path, rt.Name(), f.Name, f.Type)
			}
			walk(f.Type, path+rt.Name()+".")
		}
	}
	walk(reflect.TypeOf(pages.AnomaliesView{}), "")

	// ANTI-VACUITY: a walker that visited nothing would pass forever.
	if fields < 30 {
		t.Fatalf("the walk saw %d field(s); AnomaliesView and its rows hold more, so it is "+
			"reading the wrong type", fields)
	}
	t.Logf("§4.7 view type graph, %d fields:\n%s", fields, strings.Join(dump, "\n"))
}

// TestAnomaliesPages_CarryNoDecimalDegreeInEITHEROfTheOtherTwOShapes extends the §4.7
// shape sweep to the page shapes a database fixture cannot produce.
//
// 🔴 THE SWEEP RAN ON ONE PAGE SHAPE AND THE FALSE-ALARM MEASUREMENT RAN ON THREE, and a
// reviewer named the gap: a decimal that only appears on an EMPTY week, or only when a
// listing is long enough to be shortened, had no brake on it at all. The empty page
// carries sentences the fixture page does not (three empty states) and the saturated one
// carries six "this list is shortened" notices; neither is reachable from
// anomalies_db_test.go's fixture.
//
// IT USES THE SAME PATTERN AS THE DATABASE SWEEP rather than a second copy of it —
// decimalDegreeShape is this package's, and two patterns for one rule is the shape where
// one of them stops matching what the other does.
func TestAnomaliesPages_CarryNoDecimalDegreeInEITHEROfTheOtherTwOShapes(t *testing.T) {
	empty := newFakeLedger()
	empty.anomalies = anomaliesWith(nil)

	saturated := newFakeLedger()
	saturated.anomalies = anomaliesWith(func(a *ledger.Anomalies) {
		a.Totals = ledger.AnomalyTotals{
			Records: 12000, Judged: 11000, Unanswerable: 700, Answerable: 11300,
			DistanceOnly: 900, OutsideVenue: 120, OutsideVenueOnNetwork: 12,
			CounterGaps: 60, OtherVenue: 300, Training: 40, ManagerTyped: 90,
		}
		for i := 0; i < ledger.AnomalyRowCap; i++ {
			n := itoaTest(i)
			a.People = append(a.People, ledger.AnomalyPerson{Name: "Employee " + n, Records: 212, DistanceOnly: 12, OutsideVenue: 3, CounterGaps: 2, OtherVenue: 1})
			a.Venues = append(a.Venues, ledger.AnomalyVenue{Name: "Venue " + n, Records: 212, Judged: 200, Answerable: 190, DistanceOnly: 12, OutsideVenue: 3})
			a.Plaques = append(a.Plaques, ledger.AnomalyPlaque{TagUID: "04A1B2C3D4E5F" + n, LocationName: "Venue " + n, Records: 212, CounterGaps: 4, LargestGap: 7})
			a.Pairs = append(a.Pairs, ledger.AnomalyPair{First: "Employee " + n, Second: "Other " + n, Venue: "Venue " + n, Together: 12, Days: 6, ClosestSeconds: 3})
			a.Open = append(a.Open, ledger.AnomalyOpen{EmployeeName: "Employee " + n, LocationName: "Venue " + n, At: weekOfUTC().From, ManagerTyped: true, WasMaskable: true})
			a.Policies = append(a.Policies, ledger.AnomalyPolicyRule{SID: "base:rule-" + n, Layer: "baseline", Records: 212})
		}
		a.MorePeople, a.MoreVenues, a.MorePlaques = true, true, true
		a.MorePairs, a.MoreOpen, a.MorePolicies = true, true, true
	})

	for _, c := range []struct {
		name   string
		reader *fakeLedger
		// anchor is a sentence only THIS page shape renders, so a sweep over an error
		// page or a blank body cannot pass by finding nothing.
		anchor string
	}{
		{"an empty week", empty, "Nobody carries one of these counts this week"},
		{"every listing at the cap", saturated, "This list is shortened"},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := anomalyBrowser(t, c.reader).do(http.MethodGet, anomaliesHref, nil).Body.String()
			if !strings.Contains(body, c.anchor) {
				t.Fatalf("the page did not render %q, so the sweep below runs over the "+
					"wrong page", c.anchor)
			}
			if m := decimalDegreeShape.FindAllString(body, -1); len(m) > 0 {
				t.Errorf("§4.7: %s carries %d string(s) shaped like a decimal degree: %q",
					c.name, len(m), m)
			}
		})
	}
}

// TestAnomaliesSection_ShortenedListsSayTheCapAndSayTheTotalsDoNot is the M6-07 lesson
// applied here: a cap on a LIST is honest, a cap on a TALLY is a wrong number that looks
// right. The page has to say which one it is.
func TestAnomaliesSection_ShortenedListsSayTheCapAndSayTheTotalsDoNot(t *testing.T) {
	records := newFakeLedger()
	records.anomalies = anomaliesWith(func(a *ledger.Anomalies) {
		a.Totals = ledger.AnomalyTotals{Records: 5000, Judged: 5000, DistanceOnly: 900}
		a.People = []ledger.AnomalyPerson{{Name: "Maria Borg", Records: 9, DistanceOnly: 9}}
		a.MorePeople = true
	})
	b := anomalyBrowser(t, records)
	body := b.do(http.MethodGet, anomaliesHref, nil).Body.String()

	if !strings.Contains(body, "This list is shortened") {
		t.Error("a shortened listing does not say so")
	}
	if !strings.Contains(body, "cover every record") {
		t.Error("the shortened-list notice does not say that the totals are NOT shortened, " +
			"which is the whole difference between a bounded page and a wrong figure")
	}
	// THE CAP IS PRINTED FROM THE READ SIDE'S CONSTANT rather than from a copy.
	if !strings.Contains(body, itoaTest(ledger.AnomalyRowCap)) {
		t.Errorf("the notice does not name the cap (%d); a sentence that cannot name its "+
			"own limit is one that drifts from it silently", ledger.AnomalyRowCap)
	}
}

// countPostForms counts the forms on a page whose method is post, case-insensitively —
// the attribute is written lower-case throughout this repository, and an upper-case one
// would be a change worth noticing rather than one to absorb silently.
func countPostForms(body string) int {
	return strings.Count(strings.ToLower(body), `<form method="post"`)
}

// itoaTest keeps the assertion above from depending on the pages package's unexported
// helper.
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
