package handler

import (
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE RESTAURANTS PAGE (LP-09): /for/restaurants joins the public site the way the
// pricing, security and product pages did (publicpages_test.go). What is held here, once:
//
//   - THE FIVE FACTS ONLY THIS PAGE CLAIMS, each derived from the code that keeps it
//     (restaurantsFactDerivations, appended to pageFactDerivations), each with its
//     negative control (TestRestaurantsFacts_SayNoWhenTheFactIsAbsent).
//   - THE COPY: every Line of pages.RestaurantsText, at any depth, collected by
//     reflection (restaurantsLines), so publicPagesCopy checks each for a pin and for
//     being rendered on /for/restaurants; the lines this page shares with the landing's
//     "Multi-location" card are held to that card word for word.
//   - THE TABLE: the chain is the demo seed's (K1), row for row, and the page renders it.
//   - THE SAMPLES: every docket through tap.Decide, the overnight shift to The Roll's
//     Monday, the cover to The Roll's Thursday and the engine's own note, the drawing's
//     words to the panel's, and the timeline's positions to the sample's times.
//   - THE LANDING'S FIT CARD: one link to the page, with nothing else pressed inside.
//   - WHAT THE PAGE MUST NOT SAY: the board's sentences this card withdrew
//     (forrestaurantsview.go lists why).

// --- the facts' sources ---------------------------------------------------------------

// restaurantsFactSources is what the restaurants page's facts read, so a negative control
// can take one piece away.
type restaurantsFactSources struct {
	Report   string // internal/domain/ledger/report.go, comments removed
	Checkin  string // internal/domain/checkin/checkin.go, comments removed
	Reports  string // web/templates/pages/reports.templ
	Baseline []policy.BaselineDoc
	// Decide is the engine the probes ask; a control swaps in one that behaves otherwise.
	Decide func(tap.Input) tap.Decision
	// Anchors is what each Anchor derivation the facts lean on answers today ("" holds).
	Anchors map[pages.Anchor]string
}

func loadRestaurantsFactSources(t *testing.T) restaurantsFactSources {
	t.Helper()
	strip := func(s string) string { return goLineCommentRE.ReplaceAllString(s, "") }
	anchors := anchorDerivations(t)
	answers := map[pages.Anchor]string{}
	for _, a := range []pages.Anchor{pages.AnchorDirectionFollowsLastOpenEntry} {
		derive, ok := anchors[a]
		if !ok {
			t.Fatalf("PREMISE: anchorDerivations derives no %q", a)
		}
		answers[a] = derive()
	}
	return restaurantsFactSources{
		Report:   strip(repoFile(t, "internal", "domain", "ledger", "report.go")),
		Checkin:  strip(repoFile(t, "internal", "domain", "checkin", "checkin.go")),
		Reports:  repoFile(t, "web", "templates", "pages", "reports.templ"),
		Baseline: policy.Baseline(),
		Decide:   tap.Decide,
		Anchors:  answers,
	}
}

// overnightProbe is a tap at Rusty Bar at 02:04 on the Tuesday, the person's check-in open
// since 17:58 on the Monday (open = false leaves nothing open).
func overnightProbe(open bool) tap.Input {
	in := panelEngineInput(pages.RestaurantsOvernightOut, false)
	in.Now = time.Date(2026, 10, 13, 2, 4, 0, 0, time.UTC)
	in.Employee.ActivatedAt = in.Now.Add(-30 * 24 * time.Hour)
	in.LastOpenIn = nil
	if open {
		in.LastOpenIn = &tap.Transaction{ID: uuid.New(), OccurredAt: time.Date(2026, 10, 12, 17, 58, 0, 0, time.UTC), Direction: tap.TypeIn}
	}
	return in
}

// coverProbe is d's tap made by somebody whose home venue is home (home == d.Venue is a
// tap at home).
func coverProbe(d pages.PanelDocket, home string) tap.Input {
	in := panelEngineInput(d, false)
	h := uuid.NewSHA1(uuid.Nil, []byte(home))
	in.Employee.Location = &h
	return in
}

// directionViolation is FactDirectionFollowsLastOpenCheckIn.
func directionViolation(s restaurantsFactSources) string {
	if why := s.Anchors[pages.AnchorDirectionFollowsLastOpenEntry]; why != "" {
		return why
	}
	closing := s.Decide(overnightProbe(true))
	if closing.Type == nil || *closing.Type != tap.TypeOut {
		return fmt.Sprintf("a 02:04 tap with a check-in open since 17:58 the evening before is %v, not a check-out", closing.Type)
	}
	if fresh := s.Decide(overnightProbe(false)); fresh.Type == nil || *fresh.Type != tap.TypeIn {
		return fmt.Sprintf("the same tap with nothing open is %v, not a check-in", fresh.Type)
	}
	return ""
}

// accumulateBody is ledger's accumulate, the one pass that books the week.
func accumulateBody(report string) string {
	at := strings.Index(report, "\nfunc accumulate(")
	if at < 0 {
		return ""
	}
	rest := report[at+1:]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

var (
	// shiftDayRE: a worked shift's hours go to the day its CHECK-IN falls on.
	shiftDayRE = regexp.MustCompile(`d := dayIndex\(in\.At, p, zone\)\s*if d >= 0 && d < len\(ph\.Daily\) \{\s*ph\.Daily\[d\] \+= worked`)
	// shiftVenueRE: and to the venue of its check-in.
	shiftVenueRE = regexp.MustCompile(`v, ok := venues\[in\.LocationID\]`)
	// otherVenueRE: a shift that started away from home marks its day, after its hours are booked.
	otherVenueRE = regexp.MustCompile(`case in\.LocationID != \*in\.Home:\s*out\.Totals\.OtherVenue\+\+\s*if d >= 0 && d < len\(ph\.Marks\) \{\s*ph\.Marks\[d\]\.Other = true`)
)

// shiftDayViolation is FactShiftCountsOnItsStartDayAndVenue.
func shiftDayViolation(s restaurantsFactSources) string {
	body := accumulateBody(s.Report)
	switch {
	case body == "":
		return "internal/domain/ledger has no accumulate"
	case !shiftDayRE.MatchString(body):
		return "accumulate no longer books a shift's hours on the day of its check-in"
	case !shiftVenueRE.MatchString(body):
		return "accumulate no longer books a shift at the venue of its check-in"
	}
	return ""
}

var (
	// tappedWallRE: the facts a tap is judged on are gathered for the PLAQUE's venue.
	tappedWallRE = regexp.MustCompile(`s\.gatherWith\(ctx, tx, req, tappedWall\(tagRow\.LocationID\)`)
	// tappedVenueRE: and that venue's ranges, point and shift are what the engine gets.
	tappedVenueRE = regexp.MustCompile(`GetLocationForTap\(ctx, store\.GetLocationForTapParams\{\s*TenantID: req\.SessionTenantID, ID: tappedLocationID,\s*\}\)[\s\S]*?f\.locationIPs = loc\.StaticIps[\s\S]*?f\.locationGPS = point\(loc\.GpsLat, loc\.GpsLng\)\s*f\.shift = shiftOf\(loc\.ShiftStart, loc\.ShiftEnd, loc\.Overnight`)
	// departmentShiftRE: a department's shift replaces the venue's.
	departmentShiftRE = regexp.MustCompile(`if emp\.DepartmentID != nil \{[\s\S]*?f\.shift = sh`)
)

// tappedVenueViolation is FactTapIsJudgedAtTheTappedVenue.
func tappedVenueViolation(s restaurantsFactSources) string {
	switch {
	case !tappedWallRE.MatchString(s.Checkin):
		return "checkin no longer gathers a tap's facts for the plaque's own venue"
	case !tappedVenueRE.MatchString(s.Checkin):
		return "checkin no longer judges a tap on the tapped venue's ranges, point and shift"
	case !departmentShiftRE.MatchString(s.Checkin):
		return "checkin no longer puts a department's shift before the venue's"
	}
	return ""
}

// crossLocationViolation is FactCrossLocationTapIsNotedNotPenalised.
func crossLocationViolation(s restaurantsFactSources) string {
	var reason string
	for _, d := range s.Baseline {
		for _, st := range d.Document.Statements {
			if st.Sid == policy.SidCrossLocationNote && st.Effect == policy.EffectNote {
				reason = st.Reason
			}
		}
	}
	if reason == "" {
		return "the baseline carries no " + policy.SidCrossLocationNote + " note"
	}
	d := pages.RestaurantsCoverDocket
	home := s.Decide(coverProbe(d, d.Venue))
	away := s.Decide(coverProbe(d, pages.RestaurantsCoverHome))
	switch {
	case !away.CrossLocation || home.CrossLocation:
		return "the engine does not tell a tap at another venue from one at home"
	case away.Verdict != home.Verdict || away.Trust != home.Trust:
		return fmt.Sprintf("a tap at another venue is decided %s/%d and the same tap at home %s/%d", away.Verdict, away.Trust, home.Verdict, home.Trust)
	case !strings.Contains(away.Note, reason):
		return "the record of a tap at another venue does not carry the note"
	case strings.Contains(away.EmployeeNote, reason):
		return "the person's own screen shows the cross-location note"
	}
	return ""
}

// rollOtherViolation is FactRollMarksWorkAtAnotherVenue.
func rollOtherViolation(s restaurantsFactSources) string {
	body := accumulateBody(s.Report)
	if !otherVenueRE.MatchString(body) {
		return "accumulate no longer marks the day of a shift started away from home"
	}
	// The hours are booked before the mark, so a marked day counts like any other.
	if i, j := strings.Index(body, "ph.Daily[d] += worked"), strings.Index(body, "ph.Marks[d].Other = true"); i < 0 || j < 0 || i > j {
		return "a marked day's hours are no longer booked like any other's"
	}
	for _, words := range []string{">at a venue other than their home venue</span>", `@receiptLine("At another venue", itoa(v.Totals.OtherVenue))`} {
		if !strings.Contains(s.Reports, words) {
			return "The Roll no longer says " + strconv.Quote(words)
		}
	}
	return ""
}

var restaurantsDerivationFuncs = map[pages.Fact]func(restaurantsFactSources) string{
	pages.FactDirectionFollowsLastOpenCheckIn:     directionViolation,
	pages.FactShiftCountsOnItsStartDayAndVenue:    shiftDayViolation,
	pages.FactTapIsJudgedAtTheTappedVenue:         tappedVenueViolation,
	pages.FactCrossLocationTapIsNotedNotPenalised: crossLocationViolation,
	pages.FactRollMarksWorkAtAnotherVenue:         rollOtherViolation,
}

// restaurantsFactDerivations is the restaurants page's derivation table (pageFactDerivations).
func restaurantsFactDerivations(t *testing.T) map[pages.Fact]func() string {
	t.Helper()
	s := loadRestaurantsFactSources(t)
	out := map[pages.Fact]func() string{}
	for fact, derive := range restaurantsDerivationFuncs {
		out[fact] = func() string { return derive(s) }
	}
	return out
}

// TestRestaurantsFacts_SayNoWhenTheFactIsAbsent is the negative control for every
// derivation above: each holds over the real sources, and each says no once one piece of
// what it reads is taken away or changed. Every fact of the table has at least one control.
func TestRestaurantsFacts_SayNoWhenTheFactIsAbsent(t *testing.T) {
	t.Parallel()
	real := loadRestaurantsFactSources(t)
	replace := func(get func(*restaurantsFactSources) *string, old, new string) func(*restaurantsFactSources) {
		return func(s *restaurantsFactSources) {
			p := get(s)
			if !strings.Contains(*p, old) {
				t.Fatalf("PREMISE: %q is not in the source this mutation edits", old)
			}
			*p = strings.Replace(*p, old, new, 1)
		}
	}
	engine := func(edit func(tap.Input, tap.Decision) tap.Decision) func(*restaurantsFactSources) {
		return func(s *restaurantsFactSources) {
			s.Decide = func(in tap.Input) tap.Decision { return edit(in, tap.Decide(in)) }
		}
	}
	report := func(s *restaurantsFactSources) *string { return &s.Report }
	checkin := func(s *restaurantsFactSources) *string { return &s.Checkin }
	tests := []struct {
		fact pages.Fact
		name string
		edit func(*restaurantsFactSources)
	}{
		{pages.FactDirectionFollowsLastOpenCheckIn, "the anchor's derivation fails", func(s *restaurantsFactSources) {
			m := maps.Clone(s.Anchors)
			m[pages.AnchorDirectionFollowsLastOpenEntry] = "taken away"
			s.Anchors = m
		}},
		{pages.FactDirectionFollowsLastOpenCheckIn, "the engine decides by the calendar day", engine(func(in tap.Input, d tap.Decision) tap.Decision {
			if in.LastOpenIn != nil && in.LastOpenIn.OccurredAt.YearDay() != in.Now.YearDay() {
				ty := tap.TypeIn
				d.Type = &ty
			}
			return d
		})},
		{pages.FactShiftCountsOnItsStartDayAndVenue, "the hours go to the check-out's day", replace(report, "d := dayIndex(in.At, p, zone)\n\t\t\tif d >= 0 && d < len(ph.Daily)", "d := dayIndex(e.At, p, zone)\n\t\t\tif d >= 0 && d < len(ph.Daily)")},
		{pages.FactShiftCountsOnItsStartDayAndVenue, "the shift goes to the check-out's venue", replace(report, "v, ok := venues[in.LocationID]", "v, ok := venues[e.LocationID]")},
		{pages.FactTapIsJudgedAtTheTappedVenue, "the gather reads the home venue", replace(checkin, "TenantID: req.SessionTenantID, ID: tappedLocationID,", "TenantID: req.SessionTenantID, ID: homeLocationID,")},
		{pages.FactTapIsJudgedAtTheTappedVenue, "the plaque's venue is not what is gathered", replace(checkin, "tappedWall(tagRow.LocationID)", "tappedWall(emp.LocationID)")},
		{pages.FactTapIsJudgedAtTheTappedVenue, "the venue's shift is not read", replace(checkin, "f.shift = shiftOf(loc.ShiftStart, loc.ShiftEnd, loc.Overnight", "f.shift = nil; _ = shiftOf(loc.ShiftStart, loc.ShiftEnd, loc.Overnight")},
		{pages.FactCrossLocationTapIsNotedNotPenalised, "the note goes from the baseline", func(s *restaurantsFactSources) {
			s.Baseline = slices.DeleteFunc(slices.Clone(s.Baseline), func(d policy.BaselineDoc) bool {
				for _, st := range d.Document.Statements {
					if st.Sid == policy.SidCrossLocationNote {
						return true
					}
				}
				return false
			})
		}},
		{pages.FactCrossLocationTapIsNotedNotPenalised, "a tap away from home is flagged", engine(func(_ tap.Input, d tap.Decision) tap.Decision {
			if d.CrossLocation {
				d.Verdict = tap.VerdictFlag
			}
			return d
		})},
		{pages.FactCrossLocationTapIsNotedNotPenalised, "the person's screen shows the note", engine(func(_ tap.Input, d tap.Decision) tap.Decision {
			d.EmployeeNote = d.Note
			return d
		})},
		{pages.FactRollMarksWorkAtAnotherVenue, "the day is not marked", replace(report, "ph.Marks[d].Other = true", "_ = d")},
		{pages.FactRollMarksWorkAtAnotherVenue, "the legend loses its words", replace(func(s *restaurantsFactSources) *string { return &s.Reports },
			">at a venue other than their home venue</span>", ">elsewhere</span>")},
	}
	for fact, derive := range restaurantsDerivationFuncs {
		if why := derive(real); why != "" {
			t.Errorf("PREMISE %s: the derivation fails on the real sources: %s", fact, why)
		}
	}
	controlled := map[pages.Fact]bool{}
	for _, tc := range tests {
		derive, ok := restaurantsDerivationFuncs[tc.fact]
		if !ok {
			t.Fatalf("%s has a control and no derivation", tc.fact)
		}
		controlled[tc.fact] = true
		s := real
		tc.edit(&s)
		if derive(s) == "" {
			t.Errorf("CONTROL %s: %s and the derivation still holds", tc.fact, tc.name)
		}
	}
	for fact := range restaurantsDerivationFuncs {
		if !controlled[fact] {
			t.Errorf("%s has no negative control", fact)
		}
	}
}

// --- the copy ------------------------------------------------------------------------

// restaurantsLines is every sentence /for/restaurants renders from a pages.Line: every
// Line of RestaurantsText at any depth, and the sign-off's free months.
func restaurantsLines() map[string]pages.Line {
	out := map[string]pages.Line{
		"LandingFreeMonthsLine": pages.LandingFreeMonthsLine(foundingFreeMonths),
	}
	collectLines(reflect.ValueOf(pages.RestaurantsCopy), "RestaurantsText", out)
	return out
}

// TestRestaurantsLines_EveryLineIsCollected: the walk reaches every Line of the page --
// the bullets, the chores' sentences, the lines the landing card lends -- no field could
// hide one from it, none is empty, and the three sentences this page shares with the
// landing's "Multi-location" card are that card's, word for word.
func TestRestaurantsLines_EveryLineIsCollected(t *testing.T) {
	t.Parallel()
	for _, p := range unreachableLines(reflect.TypeOf(pages.RestaurantsText{}), "RestaurantsText") {
		t.Error(p)
	}
	lines := restaurantsLines()
	if len(lines) < 25 {
		t.Fatalf("restaurantsLines collects %d line(s); the page carries more", len(lines))
	}
	for _, want := range []string{"RestaurantsText.OvernightBullets[2]", "RestaurantsText.Cases[3].Text",
		"RestaurantsText.HoursBullets[1]", "RestaurantsText.VenuesChip", "RestaurantsText.ForgotBullets[1]"} {
		if _, ok := lines[want]; !ok {
			t.Errorf("restaurantsLines does not reach %s", want)
		}
	}
	for _, name := range emptyLines(lines) {
		t.Errorf("%s is a Line with no words", name)
	}
	c, fits := pages.RestaurantsCopy, pages.LandingAudiences[0]
	for got, want := range map[string]string{
		c.Lede.Text:          fits.Paragraph(),
		c.OvernightLede.Text: fits.Points[1].Text,
		c.VenuesLede.Text:    fits.Points[0].Text,
	} {
		if got != want {
			t.Errorf("the page says %q where the landing's card says %q", got, want)
		}
	}
}

// --- the venue table -------------------------------------------------------------------

// seedChainRowRE reads one location row of the seed with its tenant and its overnight flag:
// the tenant id, the name, the ranges, the shift and whether it runs past midnight.
var seedChainRowRE = regexp.MustCompile(`'[0-9a-f-]+',\s*'([0-9a-f-]+)',\s*'([^']+)',\s*ARRAY\[([^\]]*)\]::cidr\[\],\s*[0-9.]+,\s*[0-9.]+,\s*TIME '(\d\d:\d\d)',\s*TIME '(\d\d:\d\d)',\s*(true|false)`)

// seedTenantRE reads the tenant called "Kebab Factory Ltd.", the chain the page draws.
var seedTenantRE = regexp.MustCompile(`\('([0-9a-f-]+)',\s*'Kebab Factory Ltd\.'`)

// seedChain is the chain's venues as the seed records them, in its order.
func seedChain(t *testing.T) []pages.RestaurantsVenue {
	t.Helper()
	seed := repoFile(t, "test", "fixtures", "seed.sql")
	tenant := seedTenantRE.FindStringSubmatch(seed)
	if tenant == nil {
		t.Fatal("PREMISE: the seed has no tenant called Kebab Factory Ltd.")
	}
	var out []pages.RestaurantsVenue
	rows := seedChainRowRE.FindAllStringSubmatch(seed, -1)
	for _, m := range rows {
		if m[1] != tenant[1] {
			continue
		}
		out = append(out, pages.RestaurantsVenue{Name: m[2], Start: m[4], End: m[5], Overnight: m[6] == "true",
			Network: strings.ReplaceAll(m[3], "'", "")})
	}
	// Every location of the seed is read (ten today: nine of the chain, one plant).
	if len(rows) != len(seedVenues(t)) || len(out) < 2 {
		t.Fatalf("PREMISE: read %d location row(s) and %d of the chain; the seed has %d", len(rows), len(out), len(seedVenues(t)))
	}
	return out
}

// restaurantsTableProblems says how the page's chain departs from the seed's, and how the
// rendered page departs from the chain.
func restaurantsTableProblems(chain, seed []pages.RestaurantsVenue, body string) []string {
	var out []string
	if len(chain) != len(seed) {
		out = append(out, fmt.Sprintf("the page draws %d venue(s) and the seed's chain has %d", len(chain), len(seed)))
	}
	for i := range min(len(chain), len(seed)) {
		if chain[i] != seed[i] {
			out = append(out, fmt.Sprintf("row %d is %+v and the seed's is %+v", i+1, chain[i], seed[i]))
		}
	}
	table := regexp.MustCompile(`(?s)<table class="lp-forTable">.*?</table>`).FindString(body)
	if !strings.Contains(table, pages.RestaurantsDemoLabel) {
		out = append(out, "the table does not say it is our demo data")
	}
	rows := regexp.MustCompile(`(?s)<tr><th scope="row">(.*?)</th><td class="lp-forHours">(.*?)</td><td class="lp-forNet">(.*?)</td></tr>`).FindAllStringSubmatch(table, -1)
	if len(rows) != len(chain) {
		out = append(out, fmt.Sprintf("the page renders %d table row(s) for %d venue(s)", len(rows), len(chain)))
	}
	for i := range min(len(rows), len(chain)) {
		v, r := chain[i], rows[i]
		night := strings.Contains(r[2], "overnight")
		if r[1] != templEscape(v.Name) || !strings.HasPrefix(r[2], v.Hours()) || r[3] != v.Network || night != v.Overnight {
			out = append(out, fmt.Sprintf("row %d renders %q · %q · %q", i+1, r[1], r[2], r[3]))
		}
	}
	return out
}

// TestRestaurantsPage_TheVenueTableIsTheSeed: the chain the page draws -- the table and the
// chain card -- is the demo seed's Kebab Factory chain, name for name, hours for hours,
// range for range and overnight for overnight, in the seed's order (K1); the table says
// it is our demo data; the counts printed beside it are its length. CONTROL: a name, an
// hour, a range or an overnight flag changed, and a row left out, are red.
func TestRestaurantsPage_TheVenueTableIsTheSeed(t *testing.T) {
	t.Parallel()
	seed := seedChain(t)
	body := mustFetchMarketing(t, marketingRouter(t), pages.ForRestaurantsPath)
	for _, p := range restaurantsTableProblems(pages.RestaurantsVenues, seed, body) {
		t.Error(p)
	}
	n := strconv.Itoa(len(seed))
	text := screenText(t, body)
	for _, want := range []string{
		"KEBAB FACTORY LTD. — " + n + " locations",
		pages.LandingAudiences[0].Example.Text,
		"A chain of " + pages.RestaurantsChainCount() + " venues · " + pages.RestaurantsDemoLabel,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("/for/restaurants does not print %q", want)
		}
	}
	if !strings.HasPrefix(pages.LandingAudiences[0].Example.Text, n+" sites ") {
		t.Errorf("the card's chip says %q and the chain has %s venues", pages.LandingAudiences[0].Example.Text, n)
	}
	chip := regexp.MustCompile(`(?s)<ul class="lp-forTiles">.*?</ul>`).FindString(body)
	for _, v := range seed {
		if !strings.Contains(chip, ">"+templEscape(v.Name)+"<") || !strings.Contains(chip, ">"+v.Hours()+"<") {
			t.Errorf("the chain card does not draw %s at %s", v.Name, v.Hours())
		}
	}
	if got := strings.Count(chip, "lp-forTile--night"); got != 1 {
		t.Errorf("the chain card marks %d venue(s) overnight; the seed has one", got)
	}

	mutate := func(edit func([]pages.RestaurantsVenue) []pages.RestaurantsVenue) []pages.RestaurantsVenue {
		return edit(slices.Clone(pages.RestaurantsVenues))
	}
	controls := map[string][]pages.RestaurantsVenue{
		"a venue renamed": mutate(func(c []pages.RestaurantsVenue) []pages.RestaurantsVenue { c[0].Name = "KF Hamrun Square"; return c }),
		"an hour moved":   mutate(func(c []pages.RestaurantsVenue) []pages.RestaurantsVenue { c[6].Start = "10:00"; return c }),
		"a range changed": mutate(func(c []pages.RestaurantsVenue) []pages.RestaurantsVenue { c[2].Network = "203.0.113.0/24"; return c }),
		"the bar not overnight": mutate(func(c []pages.RestaurantsVenue) []pages.RestaurantsVenue {
			c[7].Overnight = false
			return c
		}),
		"a row left out": mutate(func(c []pages.RestaurantsVenue) []pages.RestaurantsVenue { return c[:8] }),
	}
	for name, chain := range controls {
		if len(restaurantsTableProblems(chain, seed, body)) == 0 {
			t.Errorf("CONTROL: %s passed", name)
		}
	}
	if len(restaurantsTableProblems(pages.RestaurantsVenues, seed, strings.Replace(body, ">203.0.113.17/32<", ">203.0.113.71/32<", 1))) == 0 {
		t.Error("CONTROL: a range rendered wrong passed")
	}
	// The other side: the seed moves and the page does not.
	moved := slices.Clone(seed)
	moved[7].End = "03:00"
	if len(restaurantsTableProblems(pages.RestaurantsVenues, moved, body)) == 0 {
		t.Error("CONTROL: the seed's bar closing at 03:00 passed against the page's 02:00")
	}
}

// --- the samples ----------------------------------------------------------------------------

// clockMinutes is "HH:MM" as minutes after midnight.
func clockMinutes(t *testing.T, hhmm string) int {
	t.Helper()
	at, err := time.Parse("15:04", hhmm)
	if err != nil {
		t.Fatalf("%q is not a time: %v", hhmm, err)
	}
	return at.Hour()*60 + at.Minute()
}

// restaurantsSampleProblems is every way the page's samples depart from the engine, the
// landing panel's samples and the panel's own words.
func restaurantsSampleProblems(t *testing.T, in, out, cover pages.PanelDocket, minutes int, note string, steps []pages.RestaurantsForgotStep, legend []string) []string {
	t.Helper()
	var probs []string
	for _, d := range []pages.PanelDocket{in, out} {
		probs = append(probs, panelDocketProblems(d, "")...)
	}
	if in.Who != out.Who || in.Venue != out.Venue || in.Direction != "IN" || out.Direction != "OUT" {
		probs = append(probs, "the overnight dockets are not one person's check-in and check-out at one venue")
	}
	if span := (clockMinutes(t, out.At) + 24*60 - clockMinutes(t, in.At)) % (24 * 60); span != minutes {
		probs = append(probs, fmt.Sprintf("%s to %s is %d minutes and the drawing says %d", in.At, out.At, span, minutes))
	}
	if !slices.ContainsFunc(pages.LandingPanelRollRows, func(r pages.PanelRollRow) bool {
		return r.Who == in.Who && r.HomeVenue == in.Venue && int(r.Days[0]) == minutes
	}) {
		probs = append(probs, fmt.Sprintf("The Roll's sample has no %s at %s with %d minutes on the Monday", in.Who, in.Venue, minutes))
	}
	bar := pages.RestaurantsVenueNamed(in.Venue)
	if !bar.Overnight || clockMinutes(t, in.At) > clockMinutes(t, bar.Start)+30 || clockMinutes(t, out.At) > clockMinutes(t, bar.End)+30 {
		probs = append(probs, fmt.Sprintf("%s's shift %s is not the overnight one the two taps sit on", in.Venue, bar.Hours()))
	}

	// The cover: the engine decides the tap away from home as drawn, and writes the note.
	probs = append(probs, panelDocketProblems(cover, "")...)
	dec := tap.Decide(coverProbe(cover, pages.RestaurantsCoverHome))
	if !dec.CrossLocation || !strings.Contains(dec.Note, note) || strings.Contains(dec.EmployeeNote, note) {
		probs = append(probs, fmt.Sprintf("the engine notes %q on the cover's record (screen: %q); the drawing prints %q", dec.Note, dec.EmployeeNote, note))
	}
	if cover.Venue == pages.RestaurantsCoverHome {
		probs = append(probs, "the cover is at the person's home venue")
	}
	if !slices.ContainsFunc(pages.LandingPanelRollRows, func(r pages.PanelRollRow) bool {
		return r.Who == cover.Who && r.HomeVenue == pages.RestaurantsCoverHome && r.Away[3] && r.Days[3] > pages.RollNone
	}) {
		probs = append(probs, fmt.Sprintf("The Roll's sample does not mark %s's Thursday as worked at another venue", cover.Who))
	}
	if !regexp.MustCompile(`^[0-9A-F]{14}$`).MatchString(pages.RestaurantsCoverPlaque) {
		probs = append(probs, fmt.Sprintf("%q is not a plaque id as the table prints one", pages.RestaurantsCoverPlaque))
	}
	for _, v := range []string{in.Venue, cover.Venue, pages.RestaurantsCoverHome} {
		if pages.RestaurantsVenueNamed(v).Name == "" {
			probs = append(probs, v+" is not a venue of the chain")
		}
	}

	// The forgotten check-out: an open check-in at a venue of the chain, and the panel's words.
	reports := repoFile(t, "web", "templates", "pages", "reports.templ")
	docket := repoFile(t, "web", "templates", "components", "docket.templ")
	if len(steps) != 3 || !strings.HasPrefix(steps[0].Detail, "KF Hamrun · IN ") || pages.RestaurantsVenueNamed("KF Hamrun").Name == "" {
		probs = append(probs, "the forgotten check-out does not open on a check-in at a venue of the chain")
	}
	if len(steps) == 3 {
		if !strings.Contains(reports, `"`+steps[1].Detail+`"`) {
			probs = append(probs, fmt.Sprintf("The Roll does not say %q", steps[1].Detail))
		}
		mark, _, _ := strings.Cut(steps[2].Detail, " · ")
		if !strings.Contains(docket, mark) {
			probs = append(probs, fmt.Sprintf("the docket does not mark a typed record %q", mark))
		}
	}
	for _, w := range legend {
		if !strings.Contains(reports, "</span>"+w+"</span>") {
			probs = append(probs, fmt.Sprintf("The Roll's legend does not say %q", w))
		}
	}
	return probs
}

// TestRestaurantsPage_TheSamplesAreWhatTheProductWouldDraw: the two overnight dockets are
// stamped as the engine decides them, one person's check-in and check-out at the overnight
// venue, the 8h 06m The Roll books on that Monday; the cover is stamped as the engine
// decides a tap away from home and carries the engine's own note, on the Thursday The Roll
// marks; the forgotten check-out's cards and The Roll's legend are the panel's words; no
// sample is a person of the seed; and the page draws each. CONTROL: one mutation per
// sample is red.
func TestRestaurantsPage_TheSamplesAreWhatTheProductWouldDraw(t *testing.T) {
	t.Parallel()
	in, out, cover := pages.RestaurantsOvernightIn, pages.RestaurantsOvernightOut, pages.RestaurantsCoverDocket
	minutes, note := pages.RestaurantsOvernightMinutes, pages.RestaurantsCoverNote
	steps, legend := pages.RestaurantsForgotSteps, pages.RestaurantsRollLegend
	for _, p := range restaurantsSampleProblems(t, in, out, cover, minutes, note, steps, legend) {
		t.Error(p)
	}
	for _, p := range samplePeopleProblems([]string{in.Who, out.Who, cover.Who}, seedPeople(t)) {
		t.Error(p)
	}
	text := renderedPublicText(t, pages.ForRestaurantsPath)
	for _, want := range []string{
		in.Venue + " · MON " + stampWord(in.Verdict) + " " + in.Who + " IN " + in.At + " Trust " + in.Trust,
		out.Venue + " · TUE " + stampWord(out.Verdict) + " " + out.Who + " OUT " + out.At + " Trust " + out.Trust,
		cover.Venue + " · " + pages.RestaurantsCoverDayShort + " · PLAQUE " + pages.RestaurantsCoverPlaque + " " + stampWord(cover.Verdict) +
			" " + cover.Who + " IN " + cover.At + " Trust " + cover.Trust + " IP " + cover.IP + " · GPS " + cover.GPS + " " + note,
		pages.RollHours(minutes) + " · one shift",
		"Home venue " + pages.RestaurantsCoverHome,
		pages.LandingPanelSampleNote,
	} {
		if !strings.Contains(text, asRendered(want)) {
			t.Errorf("/for/restaurants does not draw %q", want)
		}
	}
	for _, s := range steps {
		if !strings.Contains(text, s.No+" "+s.Title+" "+s.Detail) {
			t.Errorf("/for/restaurants does not draw the step %q", s.Title)
		}
	}
	for _, w := range panelDrawingWant() {
		if strings.HasPrefix(w, pages.LandingPanelRollRows[0].Who) && !strings.Contains(text, w) {
			t.Errorf("/for/restaurants does not draw The Roll's row %q", w)
		}
	}

	flip := func(d pages.PanelDocket, edit func(*pages.PanelDocket)) pages.PanelDocket { edit(&d); return d }
	controls := []struct {
		name string
		p    []string
	}{
		{"the check-out stamped FLAGGED", restaurantsSampleProblems(t, in, flip(out, func(d *pages.PanelDocket) { d.Verdict = "flag" }), cover, minutes, note, steps, legend)},
		{"the check-in at 18:30", restaurantsSampleProblems(t, flip(in, func(d *pages.PanelDocket) { d.At = "18:30" }), out, cover, minutes, note, steps, legend)},
		{"the shift a minute longer", restaurantsSampleProblems(t, in, out, cover, minutes+1, note, steps, legend)},
		{"the cover with trust 50", restaurantsSampleProblems(t, in, out, flip(cover, func(d *pages.PanelDocket) { d.Trust = "50" }), minutes, note, steps, legend)},
		{"the cover at home", restaurantsSampleProblems(t, in, out, flip(cover, func(d *pages.PanelDocket) { d.Venue = pages.RestaurantsCoverHome }), minutes, note, steps, legend)},
		{"a note the engine does not write", restaurantsSampleProblems(t, in, out, cover, minutes, "covered for a colleague", steps, legend)},
		{"a step in words the panel does not use", restaurantsSampleProblems(t, in, out, cover, minutes, note, []pages.RestaurantsForgotStep{steps[0], {No: "02", Title: "x", Detail: "Missing check-outs"}, steps[2]}, legend)},
		{"a legend in words the panel does not use", restaurantsSampleProblems(t, in, out, cover, minutes, note, steps, []string{"worked elsewhere"})},
	}
	for _, c := range controls {
		if len(c.p) == 0 {
			t.Errorf("CONTROL: %s passed", c.name)
		}
	}
	if len(samplePeopleProblems([]string{"Grace Attard"}, seedPeople(t))) == 0 {
		t.Error("CONTROL: a seed person drawn as a sample passed")
	}
}

// restaurantsTimelineProblems is every position of the overnight drawing the stylesheet
// puts somewhere other than the sample's time on the axis: the two taps, the shift's two
// ends, midnight, and the brace from tap to tap.
func restaurantsTimelineProblems(t *testing.T, css string) []string {
	t.Helper()
	axis := pages.RestaurantsTimelineAxis
	from := clockMinutes(t, axis[0])
	span := (clockMinutes(t, axis[len(axis)-1]) + 24*60 - from) % (24 * 60)
	pct := func(hhmm string, digits int) string {
		m := (clockMinutes(t, hhmm) + 24*60 - from) % (24 * 60)
		return strconv.FormatFloat(float64(m)*100/float64(span), 'f', digits, 64) + "%"
	}
	rest := func(hhmm string, digits int) string {
		m := (clockMinutes(t, hhmm) + 24*60 - from) % (24 * 60)
		return strconv.FormatFloat(100-float64(m)*100/float64(span), 'f', digits, 64) + "%"
	}
	bar := pages.RestaurantsVenueNamed(pages.RestaurantsOvernightIn.Venue)
	in, out := pages.RestaurantsOvernightIn.At, pages.RestaurantsOvernightOut.At
	want := map[string][]string{
		".lp .lp-forTlIn":       {"left: " + pct(in, 2)},
		".lp .lp-forTlOut":      {"left: " + pct(out, 2)},
		".lp .lp-forTlShift":    {"left: " + pct(bar.Start, 4), "right: " + rest(bar.End, 4)},
		".lp .lp-forTlSpan":     {"left: " + pct(in, 2), "right: " + rest(out, 2)},
		".lp .lp-forTlBrace":    {"left: " + pct(in, 2), "right: " + rest(out, 2)},
		".lp .lp-forTlMidnight": {"left: " + pct("00:00", 4)},
		".lp .lp-forTlNextDay":  {"left: " + pct("00:00", 4)},
	}
	var probs []string
	for _, sel := range slices.Sorted(maps.Keys(want)) {
		body := cssRuleBody(css, sel)
		for _, decl := range want[sel] {
			if !strings.Contains(body, decl+";") {
				probs = append(probs, fmt.Sprintf("%s does not set %q (it reads %q)", sel, decl, strings.TrimSpace(body)))
			}
		}
	}
	return probs
}

// TestRestaurantsPage_TheTimelineIsDrawnAtTheSampleTimes: the overnight drawing's fixed
// classes put the two taps, the shift and midnight where the sample's times fall on its
// axis, and the shift is Rusty Bar's in the seed. CONTROL: a tap moved in the stylesheet is
// red.
func TestRestaurantsPage_TheTimelineIsDrawnAtTheSampleTimes(t *testing.T) {
	t.Parallel()
	css := repoFile(t, "web", "static", "css", "input.css")
	for _, p := range restaurantsTimelineProblems(t, css) {
		t.Error(p)
	}
	moved := strings.Replace(css, ".lp .lp-forTlOut { left: 83.89%; }", ".lp .lp-forTlOut { left: 83.33%; }", 1)
	if moved == css || len(restaurantsTimelineProblems(t, moved)) == 0 {
		t.Error("CONTROL: the check-out drawn at 02:00 passed")
	}
}

// --- the landing's fit card ----------------------------------------------------------

// fitsSectionRE is the landing's "Built for both shapes of business" section.
var fitsSectionRE = regexp.MustCompile(`(?s)<div class="lp-fits">.*?</section>`)

// fitCardProblems says how the landing's fit cards depart from the rule: a card whose page
// is mounted is ONE link to it -- the card itself, its foot the page's label in the Product
// column, nothing else pressed inside -- and a card whose page is not is no link at all.
func fitCardProblems(landing string) []string {
	section := fitsSectionRE.FindString(landing)
	if section == "" {
		return []string{"the landing has no fits section"}
	}
	var out []string
	links := 0
	for _, a := range pages.LandingAudiences {
		if a.Href == "" {
			continue
		}
		links++
		open := `<a class="card fitcard lp-fit" href="` + a.Href + `">`
		at := strings.Index(section, open)
		if at < 0 {
			out = append(out, fmt.Sprintf("the %q card is not one link to %s", a.Title, a.Href))
			continue
		}
		card := section[at+len(open):]
		card = card[:strings.Index(card, "</a>")]
		for _, control := range []string{"<a ", "<button", "<input", "<select", "<textarea", "<form", "tabindex", "<details"} {
			if strings.Contains(card, control) {
				out = append(out, fmt.Sprintf("the %q card holds %q inside its link", a.Title, control))
			}
		}
		label := ""
		for _, l := range pages.PublicProductLinks {
			if l.Href == a.Href {
				label = l.Label
			}
		}
		if label == "" || a.Link != label || !strings.Contains(card, `<span class="lp-fitLink">`+templEscape(label)) {
			out = append(out, fmt.Sprintf("the %q card's foot is not the Product column's %q", a.Title, label))
		}
		if !strings.Contains(card, ">"+templEscape(a.Title)+"</h3>") {
			out = append(out, fmt.Sprintf("the %q card's link does not hold the card", a.Title))
		}
	}
	if got := strings.Count(section, "<a "); got != links {
		out = append(out, fmt.Sprintf("the fits section carries %d link(s) for %d mounted page(s)", got, links))
	}
	return out
}

// TestRestaurantsPage_TheLandingFitCardIsOneLinkToThePage: the landing's "Multi-location"
// card is the whole-card link to /for/restaurants (README O8: it arrived with the route),
// with nothing else pressed inside it and its foot the Product column's label; a card whose
// page is not mounted is no link. The link IS the card -- the element carrying the card's
// edge and corner -- so the one focus ring of the public site (.lp :focus-visible) is drawn
// around the card, and nothing on .fitcard takes it away. CONTROL: a link nested in the
// card, the card pointed elsewhere and the ring switched off are red.
func TestRestaurantsPage_TheLandingFitCardIsOneLinkToThePage(t *testing.T) {
	t.Parallel()
	if pages.LandingAudiences[0].Href != pages.ForRestaurantsPath {
		t.Fatalf("the Multi-location card links to %q", pages.LandingAudiences[0].Href)
	}
	landing := mustFetchMarketing(t, marketingRouter(t), "/")
	for _, p := range fitCardProblems(landing) {
		t.Error(p)
	}
	css := repoFile(t, "web", "static", "css", "input.css")
	ring := func(css string) string {
		if !strings.Contains(cssRuleBody(css, ".lp :focus-visible"), "outline: 2px solid var(--ring)") {
			return "the public site's focus ring is gone"
		}
		for _, sel := range []string{".lp .fitcard:focus-visible", ".lp .fitcard:focus", ".lp .lp-fit:focus-visible", ".lp .card:focus-visible"} {
			if strings.Contains(cssRuleBody(css, sel), "outline") {
				return sel + " restyles the ring"
			}
		}
		return ""
	}
	if why := ring(css); why != "" {
		t.Error(why)
	}

	nested := strings.Replace(landing, `<span class="lp-fitLink">`, `<a href="/pricing">x</a><span class="lp-fitLink">`, 1)
	if len(fitCardProblems(nested)) == 0 {
		t.Error("CONTROL: a link nested in the card passed")
	}
	elsewhere := strings.Replace(landing, `<a class="card fitcard lp-fit" href="`+pages.ForRestaurantsPath+`">`, `<a class="card fitcard lp-fit" href="/pricing">`, 1)
	if len(fitCardProblems(elsewhere)) == 0 {
		t.Error("CONTROL: the card pointed at /pricing passed")
	}
	if ring(css+"\n.lp .fitcard:focus-visible { outline: none; }\n") == "" {
		t.Error("CONTROL: the ring switched off on the card passed")
	}
}

// TestRestaurantsPage_IsInTheMenuAndTheFooterAndMarksItself: the phone menu and the
// footer's Product column link to /for/restaurants from every page of the site, and mark
// it as the current page on the page itself and nowhere else; no header entry is marked
// there (the header's three are other pages). CONTROL: the entry's Href emptied leaves
// both lists without it.
func TestRestaurantsPage_IsInTheMenuAndTheFooterAndMarksItself(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	href := `href="` + pages.ForRestaurantsPath + `"`
	for _, url := range siteURLs() {
		body := mustFetchMarketing(t, r, url)
		menu := regexp.MustCompile(`(?s)<nav aria-label="Menu">.*?</nav>`).FindString(body)
		foot := regexp.MustCompile(`(?s)<nav class="fcol" aria-label="Product">.*?</nav>`).FindString(body)
		for name, list := range map[string]string{"menu": menu, "footer": foot} {
			if !strings.Contains(list, href) {
				t.Errorf("%s: the %s does not link to the restaurants page", url, name)
			}
			marked := strings.Contains(list, href+` aria-current="page"`) || regexp.MustCompile(href+`\s+aria-current="page"`).MatchString(list)
			if marked != (url == pages.ForRestaurantsPath) {
				t.Errorf("%s: the %s marks the restaurants page current = %v", url, name, marked)
			}
		}
		if url == pages.ForRestaurantsPath {
			bar := regexp.MustCompile(`(?s)<nav class="lp-headNav".*?</nav>`).FindString(body)
			if strings.Contains(bar, `aria-current`) {
				t.Error("/for/restaurants marks a header entry as the current page")
			}
		}
	}
	unmounted := slices.Clone(pages.PublicProductLinks)
	for i := range unmounted {
		if unmounted[i].Href == pages.ForRestaurantsPath {
			unmounted[i].Href = ""
		}
	}
	if slices.ContainsFunc(pages.MountedPublicLinks(unmounted), func(l pages.PublicLink) bool { return l.Label == "For restaurants & bars" }) {
		t.Error("CONTROL: an entry with no Href is still drawn")
	}
}

// --- what the page must not say ----------------------------------------------------------

// restaurantsRefusedPhrasings are the board's sentences this card withdrew because the
// product does not keep them as written (forrestaurantsview.go), each with its reason.
// None may come back, on this page or any other page of the public site.
var restaurantsRefusedPhrasings = map[string]string{
	"never invents a check-out":                   "no absence is derived, and a late next tap closes an entry the report then reads as open",
	"without being treated as a problem":          "The Roll marks the day and counts its hours; it judges nothing",
	"hours of the venue where the person tapped:": "a department's own shift comes first (§5)",
}

// TestRestaurantsPage_SaysNoneOfTheWithdrawnSentences: none of the board's withdrawn
// sentences is on a public page. CONTROL: each planted in the page's text is red.
func TestRestaurantsPage_SaysNoneOfTheWithdrawnSentences(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	refused := func(text string) []string {
		var out []string
		for phrase, why := range restaurantsRefusedPhrasings {
			if strings.Contains(text, phrase) {
				out = append(out, strconv.Quote(phrase)+": "+why)
			}
		}
		sort.Strings(out)
		return out
	}
	for _, url := range marketingURLs() {
		for _, p := range refused(screenText(t, mustFetchMarketing(t, r, url))) {
			t.Errorf("%s says %s", url, p)
		}
	}
	text := renderedPublicText(t, pages.ForRestaurantsPath)
	for phrase := range restaurantsRefusedPhrasings {
		if len(refused(text+" "+phrase)) == 0 {
			t.Errorf("CONTROL: %q planted in the page passed", phrase)
		}
	}
}
