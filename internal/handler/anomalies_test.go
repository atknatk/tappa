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
	"context"
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
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
	//
	// Since the 2026-10-20 shell the sign-out is drawn twice -- the rail/sidebar's foot
	// and the phone's More sheet, one of them displayed at any width -- so the set is
	// exactly those two, and every POST form on the page is one of them.
	posts := countPostForms(body)
	signOuts := strings.Count(body, `<form method="post" action="/admin/logout">`)
	if posts != 2 || signOuts != 2 {
		t.Errorf("§4.3: the page renders %d form(s) that POST, %d of them the shell's sign-out; "+
			"want exactly 2, both the sign-out. Nothing on this screen may change a record.", posts, signOuts)
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
	// M12-29: THE GPS-ALONE CARD IS A SUBSET OF THE SHORTENED LIST, so it says it may be
	// incomplete -- People is capped and ordered by counter gaps first, so a person past the
	// cap is missing from the subset too.
	if !strings.Contains(body, "which is shortened, so this list may be incomplete") {
		t.Error("the GPS-alone card is taken from a shortened per-person list and does not say " +
			"it may be incomplete")
	}
	records.anomalies = anomaliesWith(func(a *ledger.Anomalies) {
		a.Totals = ledger.AnomalyTotals{Records: 5000, Judged: 5000, DistanceOnly: 900}
		a.People = []ledger.AnomalyPerson{{Name: "Maria Borg", Records: 9, DistanceOnly: 9}}
	})
	if body := b.do(http.MethodGet, anomaliesHref, nil).Body.String(); strings.Contains(body, "may be incomplete") {
		t.Error("CONTROL: a complete per-person list still calls the GPS-alone card incomplete")
	}
}

// oddPage renders the section through the handler's own mapping, drawn at a fixed moment, so
// a test can read what one domain screen becomes on the page (M12-29).
func oddPage(t *testing.T, mutate func(*ledger.Anomalies), now time.Time) string {
	t.Helper()
	var v pages.AnomaliesView
	fillAnomaliesView(&v, anomaliesWith(mutate), now)
	var b strings.Builder
	if err := pages.AdminAnomalies(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestAnomaliesMix_APartnerVenueGetsTheLabelAndNeverABar is K-39 on the evidence mix: a
// partner venue's row carries its name, its record count and the label -- no bar, no base, no
// split, no outside-the-venue figure -- while the business's own venue beside it gets all four.
func TestAnomaliesMix_APartnerVenueGetsTheLabelAndNeverABar(t *testing.T) {
	body := oddPage(t, func(a *ledger.Anomalies) {
		a.Totals = ledger.AnomalyTotals{Records: 30, Judged: 20, Answerable: 30, Partner: 10}
		a.Venues = []ledger.AnomalyVenue{
			{Name: "KF Own Venue", Records: 20, Judged: 20, Answerable: 20, Both: 12, NetworkOnly: 4, DistanceOnly: 3, Neither: 1},
			{Name: "KM Partner Venue", Records: 10, Partner: true},
		}
	}, weekOfUTC().To)

	// POSITIVE CONTROL: the own venue's bar and counts are there.
	for _, want := range []string{
		`aria-label="KF Own Venue, 20 judged: 12 both, 4 network only, 3 distance only, 1 neither"`,
		"12 both · 4 network only · 3 distance only · 1 neither",
		"20 records · 0 of 20 read outside the venue",
		"Partner venue · evidence not shown",
		"10 records",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the mix does not print %q", want)
		}
	}
	// THE PARTNER ROW: from its name to the end of its list item, nothing evidence-shaped.
	at := strings.Index(body, "KM Partner Venue")
	if at < 0 {
		t.Fatal("the partner venue's row is not on the page")
	}
	row := body[at:]
	row = row[:strings.Index(row, "</li>")]
	for _, leak := range []string{`role="img"`, "judged", "both", "network only", "distance only", "neither", "outside the venue", "bg-porcelain"} {
		if strings.Contains(row, leak) {
			t.Errorf("K-39: the partner venue's row carries %q -- a bar or an evidence word about "+
				"a network and a coordinate that are the partner's", leak)
		}
	}
	// One bar on the page: the own venue's.
	if n := strings.Count(body, `role="img" aria-label="KF`); n != 1 {
		t.Errorf("the mix draws %d bars, want 1 (the own venue's)", n)
	}
}

// TestAnomaliesMix_TheBarIsJudgedSplitWholeAndTheRemainderIsNamed: the widths are steps of
// five that add up to 100 (components.SplitPct), every part's count is printed, and the part
// of Judged the four-way split does not place is drawn and named "not split" rather than
// being spread over the other four.
func TestAnomaliesMix_TheBarIsJudgedSplitWholeAndTheRemainderIsNamed(t *testing.T) {
	body := oddPage(t, func(a *ledger.Anomalies) {
		a.Totals = ledger.AnomalyTotals{Records: 70, Judged: 67, Answerable: 70}
		a.Venues = []ledger.AnomalyVenue{{Name: "KF Valletta", Records: 70, Judged: 67, Answerable: 70, Both: 52, NetworkOnly: 9, DistanceOnly: 3, Neither: 1}}
	}, weekOfUTC().To)

	if !strings.Contains(body, "52 both · 9 network only · 3 distance only · 1 neither · 2 not split") {
		t.Error("the remainder Judged leaves after the four parts is not printed as \"not split\"")
	}
	if !strings.Contains(body, ">not split</li>") {
		t.Error("the legend does not name the remainder the bar draws")
	}
	bar := body[strings.Index(body, `role="img" aria-label="KF Valletta`):]
	bar = bar[:strings.Index(bar, "</div>")]
	widths := regexp.MustCompile(`w-\[(\d+)%\]|w-full`).FindAllStringSubmatch(bar, -1)
	sum := 0
	for _, w := range widths {
		if w[1] == "" {
			sum += 100
			continue
		}
		n, _ := strconv.Atoi(w[1])
		if n%5 != 0 {
			t.Errorf("a part is %d%% wide, which is not one of the width steps", n)
		}
		sum += n
	}
	if sum != 100 {
		t.Errorf("the bar's parts add up to %d%%, want exactly 100 (%v)", sum, widths)
	}
	// CONTROL: a venue the split places entirely names no remainder.
	whole := oddPage(t, func(a *ledger.Anomalies) {
		a.Venues = []ledger.AnomalyVenue{{Name: "KF Hamrun", Records: 10, Judged: 10, Answerable: 10, Both: 10}}
	}, weekOfUTC().To)
	if strings.Contains(whole, "not split") {
		t.Error("CONTROL: a fully split venue still names a \"not split\" remainder")
	}
}

// TestAnomaliesPlaques_EachCounterStateSaysOnlyWhatItKnows is M12-24's four states on the
// page: read prints "#from → #to" and when (or that the earlier read is not on record),
// failed says the counters could not be read, elsewhere prints no counter, and not asked
// claims nothing beyond the gap.
func TestAnomaliesPlaques_EachCounterStateSaysOnlyWhatItKnows(t *testing.T) {
	from := weekOfUTC().From
	prev := from.Add(2*time.Hour + 14*time.Minute)
	at := from.Add(33*time.Hour + 30*time.Minute)
	body := oddPage(t, func(a *ledger.Anomalies) {
		a.Plaques = []ledger.AnomalyPlaque{
			{TagUID: "04AAAAAAAAAA01", LocationName: "Read Venue", Records: 30, CounterGaps: 1, LargestGap: 36,
				JumpState: ledger.CounterJumpRead, JumpFrom: 641, JumpTo: 678, JumpAt: at, JumpPreviousAt: &prev},
			{TagUID: "04AAAAAAAAAA02", LocationName: "Orphan Venue", Records: 3, CounterGaps: 1, LargestGap: 4,
				JumpState: ledger.CounterJumpRead, JumpFrom: 10, JumpTo: 15, JumpAt: at},
			{TagUID: "04AAAAAAAAAA03", LocationName: "Failed Venue", Records: 2, CounterGaps: 1, LargestGap: 2,
				JumpState: ledger.CounterJumpFailed},
			{TagUID: "04AAAAAAAAAA04", LocationName: "Elsewhere Venue", Records: 2, CounterGaps: 1, LargestGap: 3,
				JumpState: ledger.CounterJumpElsewhere},
			{TagUID: "04AAAAAAAAAA05", LocationName: "Unasked Venue", Records: 2, CounterGaps: 1, LargestGap: 5},
		}
	}, weekOfUTC().To)
	rowOf := func(name string) string {
		i := strings.Index(body, name)
		if i < 0 {
			t.Fatalf("the plaque at %s is not on the page", name)
		}
		r := body[i:]
		return r[:strings.Index(r, "</li>")]
	}
	for _, c := range []struct {
		venue        string
		want, absent []string
	}{
		{"Read Venue", []string{"#641", "#678", "Counter #641 → #678, 36 reads missing", "between Mon 3 Aug 02:14 and Tue 4 Aug 09:30"}, []string{"not on record", "could not be read"}},
		{"Orphan Venue", []string{"#10", "#15", "read Tue 4 Aug 09:30 · the earlier read is not on record"}, []string{"between"}},
		{"Failed Venue", []string{"2 reads missing", "could not be read"}, []string{"#", "between"}},
		{"Elsewhere Venue", []string{"3 reads missing", "not on your record, so no counters are printed"}, []string{"#", "between"}},
		{"Unasked Venue", []string{"5 reads missing"}, []string{"#", "between", "could not be read", "not on your record"}},
	} {
		row := rowOf(c.venue)
		for _, w := range c.want {
			if !strings.Contains(row, w) {
				t.Errorf("%s: the row does not say %q", c.venue, w)
			}
		}
		for _, a := range c.absent {
			if strings.Contains(row, a) {
				t.Errorf("%s: the row says %q, which its state does not know", c.venue, a)
			}
		}
	}
}

// TestAnomaliesPairs_TheDotsAreTheWeekAndAnUnreadWeekDrawsNone (M12-23 on the page): one
// dot per day, the days named in the dots' text and counted against the week; a pair whose
// days were not read (nil) prints its day count alone, never seven empty dots.
func TestAnomaliesPairs_TheDotsAreTheWeekAndAnUnreadWeekDrawsNone(t *testing.T) {
	body := oddPage(t, func(a *ledger.Anomalies) {
		a.Pairs = []ledger.AnomalyPair{
			{First: "Ann Read", Second: "Bob Read", Venue: "KF Hamrun", Together: 4, Days: 3, ClosestSeconds: 3,
				TogetherOn: []bool{true, true, false, true, false, false, false}},
			{First: "Cat Unread", Second: "Dan Unread", Venue: "KF Hamrun", Together: 2, Days: 2, ClosestSeconds: 5},
		}
	}, weekOfUTC().To)
	if !strings.Contains(body, `aria-label="Together on Mon, Tue, Thu"`) || !strings.Contains(body, "3 of 7 days") {
		t.Error("the read pair's dots do not name its days or count them against the week")
	}
	unread := body[strings.Index(body, "Cat Unread"):]
	unread = unread[:strings.Index(unread, "</li>")]
	if strings.Contains(unread, `role="img"`) || strings.Contains(unread, "of 7 days") || !strings.Contains(unread, "2 days") {
		t.Errorf("a pair whose days were not read draws dots or a week count: %q", unread)
	}
}

// TestAnomaliesOpen_AnEntryCarriesItsAgeAndNothingCloses: the age is whole days from the
// moment the page is drawn (injected), and no row offers to close the entry (D1, §4.3).
func TestAnomaliesOpen_AnEntryCarriesItsAgeAndNothingCloses(t *testing.T) {
	from := weekOfUTC().From
	body := oddPage(t, func(a *ledger.Anomalies) {
		a.Open = []ledger.AnomalyOpen{
			{EmployeeName: "Old Entry", LocationName: "KF Hamrun", At: from},
			{EmployeeName: "New Entry", LocationName: "KF Hamrun", At: from.Add(70 * time.Hour)},
		}
	}, from.Add(3*24*time.Hour+time.Hour))
	for _, want := range []string{"open 3d", "open <1d"} {
		if !strings.Contains(body, strings.ReplaceAll(want, "<", "&lt;")) {
			t.Errorf("the open list does not print %q", want)
		}
	}
	for _, forbidden := range []string{"Close it", "close it", "Close entry"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("an open entry offers %q; this page is read-only by spec (D1, §4.3)", forbidden)
		}
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

// TestAnomaliesNames_ATruncatedNameKeepsItsFullText (M12-48): a venue's or a person's name
// cut to one line by an ellipsis on the evidence-mix and GPS-alone cards still carries the
// whole name in a title, so a long name ("KF St Julians Seafront Terrace") is never only its
// first half.
func TestAnomaliesNames_ATruncatedNameKeepsItsFullText(t *testing.T) {
	const venue, person = "KF St Julians Seafront Terrace and Upper Bar", "Maria Concetta Borg Camilleri Spiteri"
	body := oddPage(t, func(a *ledger.Anomalies) {
		a.Totals = ledger.AnomalyTotals{Records: 10, Judged: 10, Answerable: 10, DistanceOnly: 2}
		a.People = []ledger.AnomalyPerson{{Name: person, Records: 10, DistanceOnly: 2}}
		a.Venues = []ledger.AnomalyVenue{{Name: venue, Records: 10, Judged: 10, Answerable: 10, Both: 8, DistanceOnly: 2}}
	}, weekOfUTC().To)

	cut := regexp.MustCompile(`<(?:p|span) class="truncate[^"]*"([^>]*)>([^<]*)<`)
	got := map[string]bool{}
	for _, m := range cut.FindAllStringSubmatch(body, -1) {
		if want := ` title="` + m[2] + `"`; m[1] != want {
			t.Errorf("the truncated name %q carries %q, want%s", m[2], m[1], want)
		}
		got[m[2]] = true
	}
	// CONTROL: both cards drew their name, so the loop above read something.
	if !got[venue] || !got[person] {
		t.Fatalf("the page has no truncated venue (%v) or person (%v) name to check", got[venue], got[person])
	}
}
