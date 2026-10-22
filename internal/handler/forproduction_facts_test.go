package handler

import (
	"bytes"
	"context"
	"fmt"
	"html"
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

// THE PRODUCTION PAGE (LP-10): /for/production joins the public site the way /pricing,
// /security and /product did (publicpages_test.go). What is held here, once:
//
//   - THE FIVE FACTS ONLY THIS PAGE CLAIMS, each derived from the code that keeps it
//     (forProductionFactDerivations, appended to pageFactDerivations), each with its
//     negative control (TestForProductionFacts_SayNoWhenTheFactIsAbsent).
//   - THE COPY: every Line of pages.ForProductionText, at any depth, collected by
//     reflection (forProductionLines), so publicPagesCopy checks each for a pin and for
//     being rendered on /for/production; and the one figure, the repeat window, read from
//     the engine and the config default.
//   - THE DEMO DATA (README K1): the site, its hours and every department with its hours
//     are test/fixtures/seed.sql's, row for row, in both directions.
//   - THE DRAWINGS: sample people, invented, each record decided by tap.Decide the way it
//     is stamped, the yardstick's lateness by tap.MinutesLate and The Roll's own words, the
//     queue's repeat by the person's own window, and the timeline's places by the minutes
//     they stand for.
//   - THE LINKS: the landing's fits card is one link to the page, and the menu and the
//     footer mark the page as the current one.
//   - WHAT THE PAGE MUST NOT SAY: the board's sentences this card withdrew
//     (forproductionview.go lists why).

// --- the facts' sources ---------------------------------------------------------------

// forProductionFactSources is what the production page's facts read, as text, so a
// negative control can take one piece away.
type forProductionFactSources struct {
	Checkin    string // internal/domain/checkin/checkin.go
	TxHandler  string // internal/handler/transactions.go
	EmpHandler string // internal/handler/employees.go
	TxQueries  string // db/queries/transactions.sql
	EmpQueries string // db/queries/employees.sql
	FilterBar  string // web/templates/components/filterbar.templ
	Roster     string // web/templates/components/roster.templ
	Report     string // internal/domain/ledger/report.go
	ReportsGo  string // internal/handler/reports.go
	Reports    string // web/templates/pages/reports.templ
	Mig04      string // db/migrations/00004_create_tags.sql

	// Migs is every migration by file name: a fact read off one migration also reads
	// every later one's Up.
	Migs map[string]string
	// Anchors is what each Anchor derivation the facts lean on answers today ("" holds).
	Anchors map[pages.Anchor]string
	// Repeat is the engine's decision for the same person tapping again inside the
	// window: its verdict, and whether it redirects (writes nothing).
	RepeatVerdict  string
	RepeatRedirect bool
}

// forProductionRepeat decides a second tap by the same person ten seconds after the
// first, on the site's network: the shape the queue draws.
func forProductionRepeat() tap.Decision {
	in := panelEngineInput(pages.PanelDocket{Venue: pages.ForProductionSite, Who: "Sample Person", Direction: "IN",
		At: "04:58", Channel: "nfc", IP: "yes", GPS: "no"}, false)
	ten := 10.0
	in.LastForPerson = &tap.Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-10 * time.Second), Direction: tap.TypeIn}
	in.SecondsSinceLastRecordedTap = &ten
	return tap.Decide(in)
}

func loadForProductionFactSources(t *testing.T) forProductionFactSources {
	t.Helper()
	strip := func(s string) string { return goLineCommentRE.ReplaceAllString(s, "") }
	anchors := anchorDerivations(t)
	answers := map[pages.Anchor]string{}
	for _, a := range []pages.Anchor{pages.AnchorDepartmentOnEveryRecord, pages.AnchorPlaqueBelongsToVenue} {
		derive, ok := anchors[a]
		if !ok {
			t.Fatalf("PREMISE: anchorDerivations derives no %q", a)
		}
		answers[a] = derive()
	}
	repeat := forProductionRepeat()
	return forProductionFactSources{
		Checkin:        strip(repoFile(t, "internal", "domain", "checkin", "checkin.go")),
		TxHandler:      strip(repoFile(t, "internal", "handler", "transactions.go")),
		EmpHandler:     strip(repoFile(t, "internal", "handler", "employees.go")),
		TxQueries:      repoFile(t, "db", "queries", "transactions.sql"),
		EmpQueries:     repoFile(t, "db", "queries", "employees.sql"),
		FilterBar:      repoFile(t, "web", "templates", "components", "filterbar.templ"),
		Roster:         repoFile(t, "web", "templates", "components", "roster.templ"),
		Report:         strip(repoFile(t, "internal", "domain", "ledger", "report.go")),
		ReportsGo:      strip(repoFile(t, "internal", "handler", "reports.go")),
		Reports:        repoFile(t, "web", "templates", "pages", "reports.templ"),
		Mig04:          repoFile(t, "db", "migrations", "00004_create_tags.sql"),
		Migs:           allMigrations(t),
		Anchors:        answers,
		RepeatVerdict:  string(repeat.Verdict),
		RepeatRedirect: repeat.Redirect != tap.RedirectNone,
	}
}

// --- the derivations -----------------------------------------------------------------

// departmentFromPersonViolation is FactDepartmentComesFromThePerson: the record keeps a
// department (the anchor), and the check-in fills it from the employee's own.
func departmentFromPersonViolation(s forProductionFactSources) string {
	if why := s.Anchors[pages.AnchorDepartmentOnEveryRecord]; why != "" {
		return why
	}
	if !strings.Contains(s.Checkin, "f.departmentID = emp.DepartmentID") {
		return "the check-in no longer reads the department off the employee"
	}
	if !strings.Contains(s.Checkin, "p.DepartmentID = f.departmentID") {
		return "the check-in no longer writes the employee's department onto the record"
	}
	return ""
}

// departmentFilterViolation is FactPassAndPeopleFilterByDepartment: each list reads the
// filter, hands it to its query, the query applies it, and the screen draws its select.
func departmentFilterViolation(s forProductionFactSources) string {
	for _, h := range []struct{ name, src string }{{"The Pass", s.TxHandler}, {"People", s.EmpHandler}} {
		if !strings.Contains(h.src, `f.DepartmentID = optionalUUID(q.Get("department"))`) {
			return h.name + " no longer reads a department filter"
		}
		if !strings.Contains(h.src, "DepartmentID: uuidParam(f.DepartmentID)") {
			return h.name + " no longer hands its department filter on"
		}
	}
	if !strings.Contains(queryBlock(s.TxQueries, "ListPanelTransactions"), "OR t.department_id = sqlc.narg('department_id')::uuid") {
		return "The Pass's query no longer narrows to a department"
	}
	if !strings.Contains(queryBlock(s.EmpQueries, "ListPanelEmployees"), "OR e.department_id = sqlc.narg('department_id')::uuid") {
		return "People's query no longer narrows to a department"
	}
	if !strings.Contains(s.FilterBar, `@filterSelect("filter-department", "department", "`+pages.ForProductionPassFilterLabel+`",`) {
		return "The Pass's filter bar no longer draws its department select"
	}
	if !strings.Contains(s.Roster, `@rosterSelect("filter-department", "department", "Department",`) {
		return "People's filters no longer draw a department select"
	}
	return ""
}

// lateCountRE is the ledger's rule: an arrival is late only past the shift's start, and
// its minutes add up.
var lateCountRE = regexp.MustCompile(`if \*late > 0 \{\s*ph\.LateShifts\+\+\s*ph\.LateBy \+= time\.Duration\(\*late\) \* time\.Minute`)

// lateWordsViolation is FactRollCountsLateArrivalsAndMinutes.
func lateWordsViolation(s forProductionFactSources) string {
	if !lateCountRE.MatchString(s.Report) {
		return "the ledger no longer counts late arrivals past the start and adds up their minutes"
	}
	if !strings.Contains(s.ReportsGo, `row.Late = plural(p.LateShifts, "late arrival", "late arrivals") + " · " + hoursMinutes(p.LateBy)`) {
		return "The Roll no longer words a person's late arrivals and their minutes"
	}
	if !strings.Contains(s.Reports, "{ p.Late }") {
		return "The Roll no longer prints them"
	}
	return ""
}

// decideToWriteRE cuts the check-in from the decision to the write.
var decideToWriteRE = regexp.MustCompile(`(?s)dec = tap\.Decide\(in\)(.*?)id, err = s\.write\(ctx, tx, req, tagRow, facts, dec, occurredAt\)`)

// redirectOnlyRE is the one way out between the two: the redirect, which writes nothing.
var redirectOnlyRE = regexp.MustCompile(`(?s)^\s*if dec\.Redirect != tap\.RedirectNone \{\s*return nil\s*\}`)

// writtenViolation is FactEveryDecidedTapIsWritten.
func writtenViolation(s forProductionFactSources) string {
	m := decideToWriteRE.FindStringSubmatch(s.Checkin)
	if m == nil {
		return "the check-in no longer writes after the decision"
	}
	if !redirectOnlyRE.MatchString(m[1]) || strings.Count(m[1], "return") != 1 {
		return "something other than a redirect now leaves the check-in between the decision and the write"
	}
	if s.RepeatVerdict != string(tap.VerdictIgnored) || s.RepeatRedirect {
		return fmt.Sprintf("a repeat inside the window is decided %q (redirect %v), not an ignored tap that is written",
			s.RepeatVerdict, s.RepeatRedirect)
	}
	return ""
}

// departmentWordRE is any mention of a department in a statement about plaques.
var departmentWordRE = regexp.MustCompile(`(?i)department`)

// plaqueNoDepartmentViolation is FactPlaqueKnowsNoDepartment.
func plaqueNoDepartmentViolation(s forProductionFactSources) string {
	if why := s.Anchors[pages.AnchorPlaqueBelongsToVenue]; why != "" {
		return why
	}
	tags := sqlTableBlock(s.Mig04, "tags")
	if tags == "" {
		return "migration 00004 no longer creates the tags table"
	}
	if departmentWordRE.MatchString(tags) {
		return "a plaque now carries a department"
	}
	if why := laterAlterProblem(s.Migs, "00004_create_tags.sql", "tags", departmentWordRE); why != "" {
		return why
	}
	return ""
}

// forProductionDerivationFuncs is each fact's derivation over the sources.
var forProductionDerivationFuncs = map[pages.Fact]func(forProductionFactSources) string{
	pages.FactDepartmentComesFromThePerson:     departmentFromPersonViolation,
	pages.FactPassAndPeopleFilterByDepartment:  departmentFilterViolation,
	pages.FactRollCountsLateArrivalsAndMinutes: lateWordsViolation,
	pages.FactEveryDecidedTapIsWritten:         writtenViolation,
	pages.FactPlaqueKnowsNoDepartment:          plaqueNoDepartmentViolation,
}

// forProductionFactDerivations is the production page's derivation table
// (pageFactDerivations).
func forProductionFactDerivations(t *testing.T) map[pages.Fact]func() string {
	t.Helper()
	s := loadForProductionFactSources(t)
	out := map[pages.Fact]func() string{}
	for fact, derive := range forProductionDerivationFuncs {
		out[fact] = func() string { return derive(s) }
	}
	return out
}

// TestForProductionFacts_SayNoWhenTheFactIsAbsent is the negative control for every
// derivation above: each holds over the real sources, and each says no once one piece of
// what it reads is taken away or changed. Every fact of the table has at least one control.
func TestForProductionFacts_SayNoWhenTheFactIsAbsent(t *testing.T) {
	t.Parallel()
	real := loadForProductionFactSources(t)
	anchor := func(a pages.Anchor) func(*forProductionFactSources) {
		return func(s *forProductionFactSources) {
			m := maps.Clone(s.Anchors)
			m[a] = "taken away"
			s.Anchors = m
		}
	}
	replace := func(get func(*forProductionFactSources) *string, old, new string) func(*forProductionFactSources) {
		return func(s *forProductionFactSources) {
			p := get(s)
			if !strings.Contains(*p, old) {
				t.Fatalf("PREMISE: %q is not in the source this mutation edits", old)
			}
			*p = strings.Replace(*p, old, new, 1)
		}
	}
	later := func(sql string) func(*forProductionFactSources) {
		return func(s *forProductionFactSources) {
			m := maps.Clone(s.Migs)
			m["00099_lp10_probe.sql"] = "-- +goose Up\n" + sql + "\n-- +goose Down\nSELECT 1;\n"
			s.Migs = m
		}
	}
	checkin := func(s *forProductionFactSources) *string { return &s.Checkin }
	tests := []struct {
		fact pages.Fact
		name string
		edit func(*forProductionFactSources)
	}{
		{pages.FactDepartmentComesFromThePerson, "the record loses its department", anchor(pages.AnchorDepartmentOnEveryRecord)},
		{pages.FactDepartmentComesFromThePerson, "the check-in stops reading the person's department", replace(checkin, "f.departmentID = emp.DepartmentID", "f.departmentID = nil")},
		{pages.FactDepartmentComesFromThePerson, "the record's department comes from the plaque", replace(checkin, "p.DepartmentID = f.departmentID", "p.DepartmentID = tagRow.DepartmentID")},
		{pages.FactPassAndPeopleFilterByDepartment, "The Pass stops reading the filter", replace(func(s *forProductionFactSources) *string { return &s.TxHandler }, `f.DepartmentID = optionalUUID(q.Get("department"))`, `f.DepartmentID = nil`)},
		{pages.FactPassAndPeopleFilterByDepartment, "People's query ignores the department", func(s *forProductionFactSources) {
			block := queryBlock(s.EmpQueries, "ListPanelEmployees")
			s.EmpQueries = strings.Replace(s.EmpQueries, block, strings.Replace(block, "OR e.department_id = sqlc.narg('department_id')::uuid", "OR true", 1), 1)
		}},
		{pages.FactPassAndPeopleFilterByDepartment, "The Pass's filter bar loses its select", replace(func(s *forProductionFactSources) *string { return &s.FilterBar }, `"filter-department", "department", "Department",`, `"filter-team", "team", "Team",`)},
		{pages.FactRollCountsLateArrivalsAndMinutes, "the ledger counts an on-time arrival late", replace(func(s *forProductionFactSources) *string { return &s.Report }, "if *late > 0 {", "if *late >= 0 {")},
		{pages.FactRollCountsLateArrivalsAndMinutes, "The Roll stops adding up the minutes", replace(func(s *forProductionFactSources) *string { return &s.ReportsGo }, `+ " · " + hoursMinutes(p.LateBy)`, ``)},
		{pages.FactEveryDecidedTapIsWritten, "an ignored tap returns before the write", replace(checkin, "if dec.Redirect != tap.RedirectNone {", "if dec.Verdict == tap.VerdictIgnored {\n\t\t\treturn nil\n\t\t}\n\t\tif dec.Redirect != tap.RedirectNone {")},
		{pages.FactEveryDecidedTapIsWritten, "the engine redirects a repeat", func(s *forProductionFactSources) { s.RepeatRedirect = true }},
		{pages.FactPlaqueKnowsNoDepartment, "a plaque is no longer a venue's", anchor(pages.AnchorPlaqueBelongsToVenue)},
		{pages.FactPlaqueKnowsNoDepartment, "a later migration gives plaques a department", later("ALTER TABLE tags ADD COLUMN department_id uuid;")},
		{pages.FactPlaqueKnowsNoDepartment, "migration 00004 gives plaques a department", replace(func(s *forProductionFactSources) *string { return &s.Mig04 }, "location_id", "department_id uuid,\n    location_id")},
	}
	controlled := map[pages.Fact]bool{}
	for fact, derive := range forProductionDerivationFuncs {
		if why := derive(real); why != "" {
			t.Errorf("PREMISE %s: the derivation fails on the real sources: %s", fact, why)
		}
	}
	for _, tc := range tests {
		derive, ok := forProductionDerivationFuncs[tc.fact]
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
	for fact := range forProductionDerivationFuncs {
		if !controlled[fact] {
			t.Errorf("%s has no negative control", fact)
		}
	}
}

// --- the copy ------------------------------------------------------------------------

// forProductionFiguresForTest is what the handler hands the page.
func forProductionFiguresForTest() pages.ForProductionFigures {
	return forProductionFigures(securityFigures())
}

// forProductionLines is every sentence /for/production renders from a pages.Line: every
// Line of ForProductionText at any depth, and the sign-off's free months.
func forProductionLines() map[string]pages.Line {
	out := map[string]pages.Line{
		"LandingFreeMonthsLine": pages.LandingFreeMonthsLine(foundingFreeMonths),
	}
	collectLines(reflect.ValueOf(pages.ForProductionCopy(forProductionFiguresForTest())), "ForProductionText", out)
	return out
}

// TestForProductionLines_EveryLineIsCollected: the walk reaches every Line of the page --
// the opening, each section's lines and bullets, the floor's cards -- no field could hide
// one from it, and none is empty.
func TestForProductionLines_EveryLineIsCollected(t *testing.T) {
	t.Parallel()
	for _, p := range unreachableLines(reflect.TypeOf(pages.ForProductionText{}), "ForProductionText") {
		t.Error(p)
	}
	lines := forProductionLines()
	if len(lines) < 20 {
		t.Fatalf("forProductionLines collects %d line(s); the page carries more", len(lines))
	}
	for _, want := range []string{"ForProductionText.Lede", "ForProductionText.Yardstick", "ForProductionText.ShiftsBullets[2]",
		"ForProductionText.QueueLede", "ForProductionText.FloorCards[2].Body"} {
		if _, ok := lines[want]; !ok {
			t.Errorf("forProductionLines does not reach %s", want)
		}
	}
	for _, name := range emptyLines(lines) {
		t.Errorf("%s is a Line with no words", name)
	}
}

// configDebounceDefaultRE is internal/config's default for the repeat window.
var configDebounceDefaultRE = regexp.MustCompile(`floatEnvRange\(\s*"TAPPA_DEBOUNCE_SECONDS"\s*,\s*(\d+)`)

// TestForProductionFigures_TheWindowIsTheDefaultAndThePagePrintsIt: the window the page
// prints is the engine's default and internal/config's, and the queue's sentence says it
// as the default. CONTROL: a window changed at the source changes the sentence.
func TestForProductionFigures_TheWindowIsTheDefaultAndThePagePrintsIt(t *testing.T) {
	t.Parallel()
	f := forProductionFiguresForTest()
	if want := int(policy.DefaultParams().DebounceWindow / time.Second); f.DebounceSeconds != want {
		t.Errorf("the page's window is %d s and the engine's default is %d s", f.DebounceSeconds, want)
	}
	m := configDebounceDefaultRE.FindStringSubmatch(repoFile(t, "internal", "config", "config.go"))
	if m == nil || m[1] != strconv.Itoa(f.DebounceSeconds) {
		t.Errorf("internal/config's default window is %v and the page's is %d s", m, f.DebounceSeconds)
	}
	want := "within " + strconv.Itoa(f.DebounceSeconds) + " seconds (the default window)"
	if !strings.Contains(renderedPublicText(t, pages.ForProductionPath), want) {
		t.Errorf("/for/production does not say %q", want)
	}
	other := f
	other.DebounceSeconds++
	if pages.ForProductionCopy(other).QueueLede.Text == pages.ForProductionCopy(f).QueueLede.Text {
		t.Error("CONTROL: the window changed and the sentence did not")
	}
}

// --- the demo data (K1) ---------------------------------------------------------------

// seedDepartmentRowRE reads one department row of the seed: its id, its tenant's, its
// venue's, its name, its shift and whether it runs overnight. A location row cannot
// match: its name is followed by its address ranges, not a time.
var seedDepartmentRowRE = regexp.MustCompile(`'([0-9a-f-]{36})',\s*'([0-9a-f-]{36})',\s*'([0-9a-f-]{36})',\s*'([^']+)',\s*TIME '(\d\d:\d\d)',\s*TIME '(\d\d:\d\d)',\s*(true|false)`)

// seedVenueIDRE reads a location row's id and name.
var seedVenueIDRE = regexp.MustCompile(`'([0-9a-f-]{36})',\s*'[0-9a-f-]{36}',\s*'([^']+)',\s*ARRAY\[`)

// seedDepartments is every department of the seed's site, by name.
type seedDepartment struct {
	Start, End string
	Overnight  bool
}

func seedSiteDepartments(t *testing.T, site string) map[string]seedDepartment {
	t.Helper()
	seed := repoFile(t, "test", "fixtures", "seed.sql")
	siteID := ""
	for _, m := range seedVenueIDRE.FindAllStringSubmatch(seed, -1) {
		if m[2] == site {
			siteID = m[1]
		}
	}
	if siteID == "" {
		t.Fatalf("PREMISE: the seed has no venue %q", site)
	}
	out := map[string]seedDepartment{}
	rows := seedDepartmentRowRE.FindAllStringSubmatch(seed, -1)
	for _, m := range rows {
		if m[3] == siteID {
			out[m[4]] = seedDepartment{Start: m[5], End: m[6], Overnight: m[7] == "true"}
		}
	}
	if len(rows) < 5 || len(out) == 0 {
		t.Fatalf("PREMISE: read %d department row(s), %d at %s; the pattern lost the file", len(rows), len(out), site)
	}
	return out
}

// forProductionSeedProblems is every way the page's tables depart from the seed: a
// department the seed lacks or has, another shift, an overnight shift drawn as a day bar,
// the site's own hours, its two evidence signs, and an order that is not earliest first.
func forProductionSeedProblems(depts []pages.ForProductionShift, site pages.ForProductionShift, seed map[string]seedDepartment, venues map[string]seedVenueRow) []string {
	var out []string
	drawn := map[string]bool{}
	for i, d := range depts {
		drawn[d.Name] = true
		s, ok := seed[d.Name]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%q is not a department of %s in the seed", d.Name, pages.ForProductionSite))
		case s.Start != d.Start || s.End != d.End:
			out = append(out, fmt.Sprintf("%s is drawn %s–%s and the seed's shift is %s–%s", d.Name, d.Start, d.End, s.Start, s.End))
		case s.Overnight:
			out = append(out, d.Name+" runs overnight in the seed and the timeline draws a day shift")
		}
		if i > 0 && depts[i-1].Start >= d.Start {
			out = append(out, d.Name+" is not drawn after an earlier start; the boards order them earliest first")
		}
	}
	for name := range seed {
		if !drawn[name] {
			out = append(out, fmt.Sprintf("the seed's department %q is not drawn", name))
		}
	}
	v, ok := venues[pages.ForProductionSite]
	switch {
	case !ok:
		out = append(out, pages.ForProductionSite+" is not a venue of the seed")
	case site.Name != pages.ForProductionSite || site.Start != v.Start || site.End != v.End:
		out = append(out, fmt.Sprintf("the site's hours are drawn %s–%s and the seed's are %s–%s", site.Start, site.End, v.Start, v.End))
	case v.Ranges == "" || !v.GPS:
		out = append(out, fmt.Sprintf("the gate signs IP ✓ · GPS ✓ and the seed has ranges %q, a point %v", v.Ranges, v.GPS))
	}
	return out
}

// TestForProductionPage_TheTablesAreTheSeed: the departments, their hours and the site's
// own hours are the seed's (K1), and the page draws each, in order, in the gate and on the
// timeline, labelled as demo data; the opening's sentence and chip say what the landing's
// production card says. CONTROLS: an hour changed, a department dropped, one added, and the
// site's hours changed are each red.
func TestForProductionPage_TheTablesAreTheSeed(t *testing.T) {
	t.Parallel()
	seed := seedSiteDepartments(t, pages.ForProductionSite)
	venues := seedVenueRows(t)
	for _, p := range forProductionSeedProblems(pages.ForProductionDepartments, pages.ForProductionSiteHours, seed, venues) {
		t.Error(p)
	}
	text := renderedPublicText(t, pages.ForProductionPath)
	gate := []string{"One plaque · five departments · from our demo data", "TAPTIME The gate " + pages.ForProductionSite + " IP ✓ GPS ✓"}
	timeline := []string{"Department shifts at " + pages.ForProductionSite + " · from our demo data", pages.ForProductionYardstick[0].At}
	for _, d := range pages.ForProductionDepartments {
		gate = append(gate, d.Name+" "+d.Start)
		timeline = append(timeline, d.Name+" "+d.Start+"–"+d.End)
	}
	s := pages.ForProductionSiteHours
	timeline = append(timeline, "No department · site hours "+s.Start+"–"+s.End, "03:00 06:00 09:00 12:00 15:00 18:00")
	renderedInOrder(t, text, "the gate", gate)
	renderedInOrder(t, text, "the shifts timeline", timeline)
	if got := countWordFor(len(pages.ForProductionDepartments)); !strings.Contains(text, "One gate, "+got+" start times.") {
		t.Errorf("the shifts heading does not count %d departments", len(pages.ForProductionDepartments))
	}
	// The opening is the landing's production card, said the same way.
	audience := pages.LandingAudiences[1]
	c := pages.ForProductionCopy(forProductionFiguresForTest())
	if c.Lede.Text != audience.Paragraph() {
		t.Errorf("the opening says %q and the landing's card %q", c.Lede.Text, audience.Paragraph())
	}
	if c.SiteNote.Text != audience.Example.Text {
		t.Errorf("the gate's chip says %q and the landing's card %q", c.SiteNote.Text, audience.Example.Text)
	}

	// CONTROLS.
	moved := slices.Clone(pages.ForProductionDepartments)
	moved[0].Start = "04:30"
	if len(forProductionSeedProblems(moved, pages.ForProductionSiteHours, seed, venues)) == 0 {
		t.Error("CONTROL: the bakery drawn at 04:30 passed")
	}
	if len(forProductionSeedProblems(pages.ForProductionDepartments[1:], pages.ForProductionSiteHours, seed, venues)) == 0 {
		t.Error("CONTROL: a table without the bakery passed")
	}
	extra := append(slices.Clone(pages.ForProductionDepartments), pages.ForProductionShift{Name: "KM Night Cleaning", Start: "22:00", End: "06:00"})
	if len(forProductionSeedProblems(extra, pages.ForProductionSiteHours, seed, venues)) == 0 {
		t.Error("CONTROL: a department the seed lacks passed")
	}
	site := pages.ForProductionSiteHours
	site.Start = "08:00"
	if len(forProductionSeedProblems(pages.ForProductionDepartments, site, seed, venues)) == 0 {
		t.Error("CONTROL: the site's hours drawn from 08:00 passed")
	}
}

// countWordFor spells a count as the page does.
func countWordFor(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}

// --- the drawings ------------------------------------------------------------------------

// forProductionMalta is the seed's zone: the shifts are Malta wall-clock times.
func forProductionMalta(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatalf("PREMISE: loading Europe/Malta: %v", err)
	}
	return loc
}

// forProductionClock is a drawn "HH:MM" or "HH:MM:SS" as a duration since midnight.
func forProductionClock(at string) (time.Duration, bool) {
	parts := strings.Split(at, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var d time.Duration
	for i, unit := range []time.Duration{time.Hour, time.Minute, time.Second}[:len(parts)] {
		n, err := strconv.Atoi(parts[i])
		if err != nil || len(parts[i]) != 2 {
			return 0, false
		}
		d += time.Duration(n) * unit
	}
	return d, true
}

// forProductionDocketProblems is every way the drawn records depart from what the engine
// decides for them: each is a real NFC tap on the site's network, by a person whose earlier
// tap in the same list (if any) is their last one; the verdict, the direction word and a
// repeat's note must be the engine's, and the department one of the seed's.
func forProductionDocketProblems(ds []pages.ForProductionDocket, seed map[string]seedDepartment, window time.Duration) []string {
	var out []string
	last := map[string]time.Duration{}
	for _, d := range ds {
		if _, ok := seed[d.Department]; !ok {
			out = append(out, fmt.Sprintf("%s's record is at %q, which is not a department of the seed", d.Who, d.Department))
		}
		at, ok := forProductionClock(d.At)
		if !ok {
			out = append(out, fmt.Sprintf("%s's record is at %q, which is not a clock time", d.Who, d.At))
			continue
		}
		in := panelEngineInput(pages.PanelDocket{Venue: pages.ForProductionSite, Who: d.Who, Direction: "IN", At: d.At,
			Channel: "nfc", IP: "yes", GPS: "no"}, false)
		note := ""
		if prev, ok := last[d.Who]; ok {
			gap := at - prev
			secs := gap.Seconds()
			in.LastForPerson = &tap.Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-gap), Direction: tap.TypeIn}
			in.SecondsSinceLastRecordedTap = &secs
			note = "same person, " + strconv.Itoa(int(secs)) + " s later"
			if gap >= window {
				note = ""
			}
		}
		last[d.Who] = at
		dec := tap.Decide(in)
		if string(dec.Verdict) != d.Verdict {
			out = append(out, fmt.Sprintf("%s at %s is stamped %q and the engine decides %q", d.Who, d.At, d.Verdict, dec.Verdict))
		}
		dir := "NONE"
		if dec.Type != nil {
			dir = strings.ToUpper(string(*dec.Type))
		}
		if d.Direction != dir {
			out = append(out, fmt.Sprintf("%s at %s reads %q and the panel would print %q", d.Who, d.At, d.Direction, dir))
		}
		if d.Note != note {
			out = append(out, fmt.Sprintf("%s at %s carries the note %q and the record's own is %q", d.Who, d.At, d.Note, note))
		}
	}
	return out
}

// forProductionLateWords is what The Roll prints for an arrival at at against shift: its
// words if it is late, "" if it is not.
func forProductionLateWords(t *testing.T, at string, s pages.ForProductionShift) string {
	t.Helper()
	malta := forProductionMalta(t)
	clock, ok1 := forProductionClock(at)
	start, ok2 := forProductionClock(s.Start)
	end, ok3 := forProductionClock(s.End)
	if !ok1 || !ok2 || !ok3 {
		t.Fatalf("PREMISE: %q, %q or %q is not a clock time", at, s.Start, s.End)
	}
	when := time.Date(2026, 10, 20, 0, 0, 0, 0, malta).Add(clock)
	late := tap.MinutesLate(when, tap.Shift{Start: start, End: end, Timezone: "Europe/Malta"})
	if late == nil {
		t.Fatal("PREMISE: tap.MinutesLate measured nothing")
	}
	if *late <= 0 {
		return ""
	}
	return plural(1, "late arrival", "late arrivals") + " · " + hoursMinutes(time.Duration(*late)*time.Minute)
}

// forProductionPlaceRE reads one of the timeline's places from the stylesheet: the class's
// time and its edge.
var forProductionPlaceRE = regexp.MustCompile(`\.lp \.lp-fp(At|To)(\d{2})(\d{2}) \{ (left|right): ([0-9.]+)%?; \}`)

// forProductionPlaceProblems is every timeline place the page uses that the stylesheet
// does not put at the minute it names (the timeline runs 03:00 to 18:00), and every bar,
// mark or label drawn without one.
func forProductionPlaceProblems(body, css string) []string {
	var out []string
	places := map[string]float64{}
	for _, m := range forProductionPlaceRE.FindAllStringSubmatch(css, -1) {
		h, _ := strconv.Atoi(m[2])
		mi, _ := strconv.Atoi(m[3])
		pct, _ := strconv.ParseFloat(m[5], 64)
		want := float64(h*60+mi-180) / 900 * 100
		if m[1] == "To" {
			want = 100 - want
		}
		if (m[1] == "At") != (m[4] == "left") || pct < want-0.001 || pct > want+0.001 {
			out = append(out, fmt.Sprintf(".lp-fp%s%s%s puts its edge at %s %s%%, and %s:%s is at %.4f%%", m[1], m[2], m[3], m[4], m[5], m[2], m[3], want))
		}
		places["lp-fp"+m[1]+m[2]+m[3]] = pct
	}
	classRE := regexp.MustCompile(`class="(lp-fp(?:Bar|BarHours|Mark|MarkTag|AxisHour)\b[^"]*)"`)
	used := 0
	for _, m := range classRE.FindAllStringSubmatch(body, -1) {
		used++
		classes := strings.Fields(m[1])
		has := func(prefix string) bool {
			for _, c := range classes {
				if _, ok := places[c]; ok && strings.HasPrefix(c, prefix) {
					return true
				}
			}
			return false
		}
		if !has("lp-fpAt") {
			out = append(out, fmt.Sprintf("%q is drawn with no place on the timeline", m[1]))
		}
		if classes[0] == "lp-fpBar" && !has("lp-fpTo") {
			out = append(out, fmt.Sprintf("%q is drawn with no end on the timeline", m[1]))
		}
	}
	if used == 0 {
		out = append(out, "the page draws no timeline")
	}
	return out
}

// TestForProductionPage_TheDrawingsAreWhatTheProductWouldDraw: every sample on the page is
// held to the product -- each record to the engine, the yardstick to tap.MinutesLate and
// The Roll's words, the queue's repeat to the person's window, the Pass to its own filter
// and order, the phone to the tap screen, the timeline's places to the minutes they stand
// for -- the people are invented, and the page draws each sample whole and in order.
// CONTROL: one mutation per drawing is red.
func TestForProductionPage_TheDrawingsAreWhatTheProductWouldDraw(t *testing.T) {
	t.Parallel()
	seed := seedSiteDepartments(t, pages.ForProductionSite)
	window := policy.DefaultParams().DebounceWindow
	var all []pages.ForProductionDocket
	for _, list := range [][]pages.ForProductionDocket{pages.ForProductionYardstick, pages.ForProductionPassDockets, pages.ForProductionQueue} {
		for _, p := range forProductionDocketProblems(list, seed, window) {
			t.Error(p)
		}
		all = append(all, list...)
	}
	// The queue is three people and a repeat: the repeat is ignored and the others are not.
	q := pages.ForProductionQueue
	if len(q) != 4 || q[3].Who != q[0].Who || q[3].Verdict != "ignored" || q[1].Who == q[0].Who || q[2].Who == q[0].Who || q[1].Who == q[2].Who {
		t.Errorf("the queue is %+v; it draws three different people, then the first again", q)
	}
	// The sample people are invented.
	var names []string
	for _, d := range all {
		names = append(names, d.Who)
	}
	for _, p := range samplePeopleProblems(names, seedPeople(t)) {
		t.Error(p)
	}
	// The yardstick: the bakery's check-in is late by The Roll's words, meat production's
	// is not, and the caption says so.
	y := pages.ForProductionYardstick
	if got := forProductionLateWords(t, y[0].At, pages.ForProductionDepartment(y[0].Department)); got != pages.ForProductionLateRoll || got == "" {
		t.Errorf("against %s, %s is %q on The Roll and the page says %q", y[0].Department, y[0].At, got, pages.ForProductionLateRoll)
	}
	if got := forProductionLateWords(t, y[1].At, pages.ForProductionDepartment(y[1].Department)); got != "" {
		t.Errorf("against %s, %s is %q on The Roll and the page says it is not late", y[1].Department, y[1].At, got)
	}
	if y[0].Department != pages.ForProductionBakery || y[1].Department != pages.ForProductionMeat {
		t.Error("the yardstick's two records are not the bakery's and meat production's, which its caption names")
	}
	// The Pass: one department, the filter's, newest first.
	pass := pages.ForProductionPassDockets
	for i, d := range pass {
		if d.Department != pass[0].Department {
			t.Errorf("The Pass is filtered to %s and draws a record of %s", pass[0].Department, d.Department)
		}
		if i > 0 && pass[i-1].At <= d.At {
			t.Errorf("The Pass draws %s after %s; it is newest first", d.At, pass[i-1].At)
		}
	}
	if !strings.Contains(repoFile(t, "web", "templates", "components", "filterbar.templ"), `"department", "`+pages.ForProductionPassFilterLabel+`"`) {
		t.Errorf("The Pass's filter is not labelled %q", pages.ForProductionPassFilterLabel)
	}
	// The phone is the tap screen's greeting.
	var b bytes.Buffer
	ts := pages.ForProductionTapSample
	if err := pages.TapHeading(ts.Who, pages.ForProductionSite).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	heading := screenText(t, "<body>"+b.String()+"</body>")
	if want := "Tapping Hello " + ts.Who + " " + pages.ForProductionSite; heading != want {
		t.Errorf("the tap screen reads %q, the drawing %q", heading, want)
	}
	// The timeline's places.
	body := mustFetchMarketing(t, marketingRouter(t), pages.ForProductionPath)
	css := repoFile(t, "web", "static", "css", "input.css")
	for _, p := range forProductionPlaceProblems(body, css) {
		t.Error(p)
	}

	// The page draws each sample, whole and in order.
	text := screenText(t, body)
	docket := func(d pages.ForProductionDocket) string {
		s := pages.ForProductionSite + " · " + d.Department + " " + stampWord(d.Verdict) + " " + d.Who + " " + d.Direction + " " + d.At
		if d.Note != "" {
			s += " " + d.Note
		}
		return s
	}
	want := []string{}
	for _, d := range y {
		want = append(want, docket(d))
	}
	want = append(want, pages.ForProductionCopy(forProductionFiguresForTest()).Yardstick.Text,
		"Sample screen: The Pass", "Today · filtered by department The Pass",
		pages.ForProductionPassFilterLabel+" "+pass[0].Department+" Today")
	for _, d := range pass {
		want = append(want, docket(d))
	}
	want = append(want, "Shift change at the gate · sample")
	for _, d := range q {
		want = append(want, docket(d))
	}
	want = append(want, "Sample screen: the person's own phone on the tap screen — Hello "+ts.Who+", "+pages.ForProductionSite+", one Tap button",
		heading+" Tap")
	renderedInOrder(t, text, "the production page's drawings", want)
	if !strings.Contains(text, pages.ForProductionSampleNote) {
		t.Errorf("/for/production does not say %q beside its samples", pages.ForProductionSampleNote)
	}

	// CONTROLS, one per drawing.
	wrong := slices.Clone(q)
	wrong[3].Verdict = "ok"
	if len(forProductionDocketProblems(wrong, seed, window)) == 0 {
		t.Error("CONTROL: the repeat drawn as approved passed")
	}
	late := slices.Clone(q)
	late[3].At = "04:59:20"
	if len(forProductionDocketProblems(late, seed, window)) == 0 {
		t.Error("CONTROL: a repeat outside the window drawn as ignored passed")
	}
	noted := slices.Clone(q)
	noted[3].Note = "same person, 10 s later"
	if len(forProductionDocketProblems(noted, seed, window)) == 0 {
		t.Error("CONTROL: a repeat with another gap in its note passed")
	}
	foreign := slices.Clone(pass)
	foreign[0].Department = "KM Night Shift"
	if len(forProductionDocketProblems(foreign, seed, window)) == 0 {
		t.Error("CONTROL: a record at a department the seed lacks passed")
	}
	if got := forProductionLateWords(t, "04:00", pages.ForProductionDepartment(pages.ForProductionBakery)); got != "" {
		t.Errorf("CONTROL: an arrival at the shift's start is %q on The Roll", got)
	}
	if len(samplePeopleProblems([]string{"Saviour Bonnici"}, seedPeople(t))) == 0 {
		t.Error("CONTROL: the board's seed person drawn as a sample passed")
	}
	if len(forProductionPlaceProblems(body, strings.Replace(css, ".lp .lp-fpAt0402 { left: 6.8889%; }", ".lp .lp-fpAt0402 { left: 7.5%; }", 1))) == 0 {
		t.Error("CONTROL: the 04:02 mark placed at 7.5% passed")
	}
	if len(forProductionPlaceProblems(strings.Replace(body, `class="lp-fpBar lp-fpAt0400`, `class="lp-fpBar lp-fpAtNone`, 1), css)) == 0 {
		t.Error("CONTROL: a bar drawn with no place passed")
	}
}

// --- the links ------------------------------------------------------------------------------

// fitsSectionRE is the landing's "Built for both shapes of business" section.
var fitsSectionRE = regexp.MustCompile(`(?s)<div class="lp-fits">.*?</section>`)

// fitsText is a piece of markup as words: tags dropped, entities read, runs of space one.
func fitsText(markup string) string {
	return asRendered(html.UnescapeString(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(markup, " ")))
}

// fitsLinkProblems is every way the landing's fits cards depart from their pages: a card
// whose shape has a page is one link to it, holding the card's words and the page's name
// and nothing else that can be pressed; a card without one is no link.
func fitsLinkProblems(body string) []string {
	var out []string
	section := fitsSectionRE.FindString(body)
	if section == "" {
		return []string{"the landing has no fits section"}
	}
	for _, a := range pages.LandingAudiences {
		title := `<h3 class="lp-fitTitle">` + templEscape(a.Title) + `</h3>`
		at := strings.Index(section, title)
		if at < 0 {
			out = append(out, a.Title+"'s card is not drawn")
			continue
		}
		plain := strings.LastIndex(section[:at], `<div class="card lp-fit">`)
		linked := strings.LastIndex(section[:at], `<a class="card fitcard lp-fit" href="`)
		if a.Href == "" {
			if linked > plain {
				out = append(out, a.Title+" has no page and its card is a link")
			}
			continue
		}
		m := regexp.MustCompile(`(?s)<a class="card fitcard lp-fit" href="` + regexp.QuoteMeta(a.Href) + `">(.*?)</a>`).FindStringSubmatch(section)
		if m == nil || linked < plain || !strings.Contains(m[1], title) {
			out = append(out, a.Title+"'s card is not one link to "+a.Href)
			continue
		}
		inner := fitsText(m[1])
		for _, w := range []string{a.Kicker, a.Title, a.Paragraph(), a.Example.Text, a.Link} {
			if w == "" || !strings.Contains(inner, asRendered(w)) {
				out = append(out, fmt.Sprintf("%s's link does not hold %q", a.Title, w))
			}
		}
		for _, c := range []string{"<a ", "<button", "<input", "<select", "<textarea", "<details", "<summary", "tabindex", "href="} {
			if strings.Contains(m[1], c) {
				out = append(out, fmt.Sprintf("%s's link holds %q; the card is one control", a.Title, c))
			}
		}
		if n := strings.Count(section, `href="`+a.Href+`"`); n != 1 {
			out = append(out, fmt.Sprintf("the fits section links %s %d times", a.Href, n))
		}
	}
	return out
}

// TestForProductionPage_TheLandingCardAndTheChromeLinkToIt: the landing's production card
// is one link to /for/production (README O8: it arrived with the route), and on the page
// itself the menu and the footer mark its entry as the current one while the header marks
// none; the page has one h1. CONTROLS: a link inside the card, the card pointed elsewhere,
// and the footer's mark taken away are each red.
func TestForProductionPage_TheLandingCardAndTheChromeLinkToIt(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	landing := mustFetchMarketing(t, r, "/")
	if pages.LandingAudiences[1].Href != pages.ForProductionPath {
		t.Errorf("the landing's production card links %q", pages.LandingAudiences[1].Href)
	}
	for _, p := range fitsLinkProblems(landing) {
		t.Error(p)
	}
	page := mustFetchMarketing(t, r, pages.ForProductionPath)
	marked := func(body string) []string {
		var out []string
		current := regexp.MustCompile(`<a [^>]*href="` + regexp.QuoteMeta(pages.ForProductionPath) + `"[^>]*aria-current="page"`)
		for _, label := range []string{"Menu", "Product"} {
			if !current.MatchString(chromeSegment(body, label)) {
				out = append(out, "the "+label+" nav does not mark "+pages.ForProductionPath+" as the current page")
			}
		}
		if strings.Contains(chromeSegment(body, "Primary"), `aria-current="page"`) {
			out = append(out, "the header marks an entry current; the production page has none there")
		}
		return out
	}
	for _, p := range marked(page) {
		t.Error(p)
	}
	if n := strings.Count(page, "<h1"); n != 1 {
		t.Errorf("/for/production carries %d h1, want one", n)
	}
	if strings.Contains(landing, `href="`+pages.ForProductionPath+`" aria-current`) {
		t.Error("the landing marks the production page as the current one")
	}

	// CONTROLS.
	link := `<a class="card fitcard lp-fit" href="` + pages.ForProductionPath + `">`
	if !strings.Contains(landing, link) {
		t.Fatal("PREMISE: the landing has no production card link")
	}
	if len(fitsLinkProblems(strings.Replace(landing, link, link+`<a href="/x">x</a>`, 1))) == 0 {
		t.Error("CONTROL: a link nested in the card passed")
	}
	if len(fitsLinkProblems(strings.Replace(landing, link, `<a class="card fitcard lp-fit" href="/pricing">`, 1))) == 0 {
		t.Error("CONTROL: the card pointed at /pricing passed")
	}
	footer := chromeSegment(page, "Product")
	if len(marked(strings.Replace(page, footer, strings.ReplaceAll(footer, ` aria-current="page"`, ""), 1))) == 0 {
		t.Error("CONTROL: the footer without its mark passed")
	}
}

// --- what the page must not say ------------------------------------------------------------

// forProductionRefusedPhrasings are the board's sentences this card withdrew, and the
// wordings the card's traps name, each with its reason (forproductionview.go). None may
// come back, on this page or any other public page.
var forProductionRefusedPhrasings = map[string]string{
	"KM Facility":           "the seed's site is KM Plant (README U11)",
	"sample setup":          "the departments and their hours are the demo data, not a sample (K1)",
	"Same minute":           "the two check-ins the yardstick compares are 53 minutes apart",
	"at least 64 pixels":    "the page says what the button is built for, as the landing and the tour do",
	"per plaque":            "the repeat window is the person's, never the plaque's (CLAUDE.md §5)",
	"per tag":               "the repeat window is the person's, never the plaque's (CLAUDE.md §5)",
	"site's own hours":      "a person with no department is measured against the site they tapped at",
	"per-department report": "The Roll has no department breakdown (TestLandingAudiences_SayNothingAboutTheTwoBreakdownsThatDoNotExist)",
	"hours per department":  "The Roll has no department breakdown",
}

// TestForProductionPage_SaysNoneOfTheWithdrawnSentences: no public page says one of the
// board's sentences this card withdrew. CONTROL: each planted in the page's text is red.
func TestForProductionPage_SaysNoneOfTheWithdrawnSentences(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	refused := func(text string) []string {
		var out []string
		for phrase, why := range forProductionRefusedPhrasings {
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
	text := renderedPublicText(t, pages.ForProductionPath)
	for phrase := range forProductionRefusedPhrasings {
		if len(refused(text+" "+phrase)) == 0 {
			t.Errorf("CONTROL: %q planted in the page passed", phrase)
		}
	}
}
