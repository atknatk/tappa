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
//   - THE DRAWINGS: sample people, invented and labelled as samples where they stand, each
//     record decided by tap.Decide the way it is stamped, the yardstick's lateness by the
//     ledger's own test case and The Roll's own words, the
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
	LedgerTest string // internal/domain/ledger/report_test.go (the yardstick's cases)

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
		LedgerTest:     repoFile(t, "internal", "domain", "ledger", "report_test.go"),
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
	// The one assignment each, so nothing between the person and the record rewrites it.
	if strings.Count(s.Checkin, "f.departmentID =") != 1 || !strings.Contains(s.Checkin, "f.departmentID = emp.DepartmentID") {
		return "the check-in no longer takes the department off the employee, and only there"
	}
	if strings.Count(s.Checkin, "p.DepartmentID =") != 1 || !strings.Contains(s.Checkin, "p.DepartmentID = f.departmentID") {
		return "the check-in no longer writes the employee's department onto the record, and only that"
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
	cases := ledgerLateCases(s.LedgerTest)
	if !slices.ContainsFunc(cases, func(c ledgerLateCase) bool { return c.Shifts == 1 }) {
		return "the ledger's test of the smallest lateness is gone, so nothing runs the report over the page's arrivals"
	}
	return ""
}

// ledgerLateCase is one case of the ledger's test of the smallest lateness
// (TestAccumulate_AFewMinutesPastTheStartIsOneLateArrival): a shift, an arrival, and what
// the report counts for it.
type ledgerLateCase struct {
	Start, End, Arrive string
	Shifts, Minutes    int
}

var ledgerLateCaseRE = regexp.MustCompile(`\{shift: "(\d\d:\d\d)-(\d\d:\d\d)", arrive: "(\d\d:\d\d)", lateShifts: (\d+), lateMinutes: (\d+)\}`)

// ledgerLateCases reads that test's cases out of its source, inside its own body only.
func ledgerLateCases(src string) []ledgerLateCase {
	body := goFuncBody(src, "func TestAccumulate_AFewMinutesPastTheStartIsOneLateArrival(")
	var out []ledgerLateCase
	for _, m := range ledgerLateCaseRE.FindAllStringSubmatch(body, -1) {
		n, _ := strconv.Atoi(m[4])
		mins, _ := strconv.Atoi(m[5])
		out = append(out, ledgerLateCase{Start: m[1], End: m[2], Arrive: m[3], Shifts: n, Minutes: mins})
	}
	return out
}

// rollLateWords is what The Roll prints for an arrival at at against s, as the ledger's
// tested case for it counts it: its words if it is late, "" if it is not, and false when
// the ledger has no case for it.
func rollLateWords(cases []ledgerLateCase, at string, s pages.ForProductionShift) (string, bool) {
	for _, c := range cases {
		if c.Start == s.Start && c.End == s.End && c.Arrive == at {
			if c.Shifts == 0 {
				return "", true
			}
			return plural(c.Shifts, "late arrival", "late arrivals") + " · " + hoursMinutes(time.Duration(c.Minutes)*time.Minute), true
		}
	}
	return "", false
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
	// And the write itself writes whatever it is handed: before its INSERT the one way out
	// is a parameter error, never a verdict.
	write := goFuncBody(s.Checkin, "func (s *Service) write(")
	pre, _, ok := strings.Cut(write, "row, err := s.insert(ctx, tx, params)")
	if !ok || strings.Count(pre, "return") != 1 || !strings.Contains(pre, "if err != nil {\n\t\treturn uuid.Nil, err\n\t}") || strings.Contains(write, "Verdict") {
		return "the write now decides on its own whether to write"
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
		{pages.FactDepartmentComesFromThePerson, "the record's department is cleared after it is set (M1b)", replace(checkin, "p.DepartmentID = f.departmentID", "p.DepartmentID = f.departmentID\n\tp.DepartmentID = nil")},
		{pages.FactDepartmentComesFromThePerson, "the gathered department is cleared after it is read (M1c)", replace(checkin, "f.departmentID = emp.DepartmentID", "f.departmentID = emp.DepartmentID\n\t\tf.departmentID = nil")},
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
		{pages.FactEveryDecidedTapIsWritten, "the write skips an ignored tap (M3b)", replace(checkin, "row, err := s.insert(ctx, tx, params)", "if dec.Verdict == tap.VerdictIgnored {\n\t\treturn uuid.Nil, nil\n\t}\n\trow, err := s.insert(ctx, tx, params)")},
		{pages.FactRollCountsLateArrivalsAndMinutes, "the ledger's smallest-lateness case is gone", replace(func(s *forProductionFactSources) *string { return &s.LedgerTest }, `{shift: "04:00-12:00", arrive: "04:02", lateShifts: 1, lateMinutes: 2},`, ``)},
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
	if len(rows) == 0 || len(out) == 0 {
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

// forProductionLateWords is what The Roll prints for an arrival at at against shift, as the
// ledger's own test of the smallest lateness counts it (accumulate, not a second
// computation here): its words if it is late, "" if it is not. A drawn arrival the ledger
// has no case for is a test failure, not a guess.
func forProductionLateWords(t *testing.T, at string, s pages.ForProductionShift) string {
	t.Helper()
	words, ok := rollLateWords(ledgerLateCases(repoFile(t, "internal", "domain", "ledger", "report_test.go")), at, s)
	if !ok {
		t.Fatalf("the ledger's TestAccumulate_AFewMinutesPastTheStartIsOneLateArrival has no case for %s against %s–%s; "+
			"the page may not draw an arrival the report was not run over", at, s.Start, s.End)
	}
	return words
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
	// Each place is the time the drawing writes beside it: a bar starts and ends where its
	// own hours say, the mark sits at its tag's time, an axis label at its hour.
	place := func(hhmm string) string { return strings.Replace(hhmm, ":", "", 1) }
	rowRE := regexp.MustCompile(`(?s)<li class="lp-fpRow[^"]*">(.*?)</li>`)
	barRE := regexp.MustCompile(`class="lp-fpBar ([^"]*)"`)
	hoursRE := regexp.MustCompile(`class="lp-fpBarHours (lp-fpAt\d{4})">(\d\d:\d\d)–(\d\d:\d\d)<`)
	markRE := regexp.MustCompile(`class="lp-fpMark (lp-fpAt\d{4})"`)
	tag := regexp.MustCompile(`class="lp-fpMarkTag (lp-fpAt\d{4})">(\d\d:\d\d)<`).FindStringSubmatch(body)
	if tag == nil || tag[1] != "lp-fpAt"+place(tag[2]) {
		out = append(out, fmt.Sprintf("the mark's tag %v is not placed at its own time", tag))
	}
	rows := rowRE.FindAllStringSubmatch(body, -1)
	if len(rows) == 0 {
		out = append(out, "the timeline has no rows")
	}
	for _, r := range rows {
		h := hoursRE.FindStringSubmatch(r[1])
		bar := barRE.FindStringSubmatch(r[1])
		if h == nil || bar == nil {
			out = append(out, "a timeline row has no bar or no hours")
			continue
		}
		classes := strings.Fields(bar[1])
		if h[1] != "lp-fpAt"+place(h[2]) || !slices.Contains(classes, "lp-fpAt"+place(h[2])) || !slices.Contains(classes, "lp-fpTo"+place(h[3])) {
			out = append(out, fmt.Sprintf("the bar %q is not drawn from %s to %s, the hours it prints", bar[1], h[2], h[3]))
		}
		for _, m := range markRE.FindAllStringSubmatch(r[1], -1) {
			if tag != nil && m[1] != tag[1] {
				out = append(out, fmt.Sprintf("a row's mark sits at %s and the tag says %s", m[1], tag[2]))
			}
		}
	}
	for _, m := range regexp.MustCompile(`class="lp-fpAxisHour (lp-fpAt\d{4})">(\d\d:\d\d)<`).FindAllStringSubmatch(body, -1) {
		if m[1] != "lp-fpAt"+place(m[2]) {
			out = append(out, fmt.Sprintf("the axis label %s sits at %s", m[2], m[1]))
		}
	}
	return out
}

// TestForProductionPage_TheDrawingsAreWhatTheProductWouldDraw: every sample on the page is
// held to the product -- each record to the engine, the yardstick to the ledger's own
// lateness case and The Roll's words, the queue's repeat to the person's window, the Pass to its own filter
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
	meat := regexp.MustCompile(`class="lp-fpBar lp-fpAt0500`).FindStringIndex(body)
	if meat == nil {
		t.Fatal("PREMISE: no bar starts at 05:00")
	}
	if len(forProductionPlaceProblems(body[:meat[0]]+`class="lp-fpBar lp-fpAt0600`+body[meat[1]:], css)) == 0 {
		t.Error("CONTROL: meat production's bar drawn from 06:00 under its printed 05:00 passed")
	}
	if len(forProductionPlaceProblems(strings.Replace(body, `class="lp-fpAxisHour lp-fpAt0900">`, `class="lp-fpAxisHour lp-fpAt0800">`, 1), css)) == 0 {
		t.Error("CONTROL: the 09:00 axis label at 08:00 passed")
	}
	if len(forProductionPlaceProblems(body, strings.Replace(css, ".lp .lp-fpTo1300 { right: 33.3333%; }", ".lp .lp-fpTo1300 { right: 30%; }", 1))) == 0 {
		t.Error("CONTROL: the 13:00 end placed at 70% passed")
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
		title := `>` + templEscape(a.Title) + `</h3>`
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
		m := regexp.MustCompile(`(?s)<a class="card fitcard lp-fit" href="` + regexp.QuoteMeta(a.Href) + `"([^>]*)>(.*?)</a>`).FindStringSubmatch(section)
		if m == nil || linked < plain || !strings.Contains(m[2], title) {
			out = append(out, a.Title+"'s card is not one link to "+a.Href)
			continue
		}
		// Its name is the heading and the page's name, its description the paragraph: not
		// the card's whole text.
		named := map[string]string{}
		for _, part := range []string{"labelledby", "describedby"} {
			if r := regexp.MustCompile(`aria-` + part + `="([^"]*)"`).FindStringSubmatch(m[1]); r != nil {
				for _, id := range strings.Fields(r[1]) {
					el := regexp.MustCompile(`(?s)id="` + regexp.QuoteMeta(id) + `"[^>]*>(.*?)</(?:h3|p|span)>`).FindStringSubmatch(m[2])
					if el == nil {
						out = append(out, fmt.Sprintf("%s's link names #%s, which is not inside it", a.Title, id))
						continue
					}
					named[part] += " " + fitsText(el[1])
				}
			}
		}
		if asRendered(named["labelledby"]) != asRendered(a.Title+" "+a.Link) {
			out = append(out, fmt.Sprintf("%s's link is named %q; it is the heading and the page's name", a.Title, named["labelledby"]))
		}
		if asRendered(named["describedby"]) != asRendered(a.Paragraph()) {
			out = append(out, fmt.Sprintf("%s's link is described by %q; it is the paragraph", a.Title, named["describedby"]))
		}
		inner := fitsText(m[2])
		for _, w := range []string{a.Kicker, a.Title, a.Paragraph(), a.Example.Text, a.Link} {
			if w == "" || !strings.Contains(inner, asRendered(w)) {
				out = append(out, fmt.Sprintf("%s's link does not hold %q", a.Title, w))
			}
		}
		for _, c := range []string{"<a ", "<button", "<input", "<select", "<textarea", "<details", "<summary", "tabindex", "href="} {
			if strings.Contains(m[2], c) {
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
	link := regexp.MustCompile(`<a class="card fitcard lp-fit" href="` + regexp.QuoteMeta(pages.ForProductionPath) + `"[^>]*>`).FindString(landing)
	if link == "" {
		t.Fatal("PREMISE: the landing has no production card link")
	}
	if len(fitsLinkProblems(strings.Replace(landing, link, link+`<a href="/x">x</a>`, 1))) == 0 {
		t.Error("CONTROL: a link nested in the card passed")
	}
	if len(fitsLinkProblems(strings.Replace(landing, link, strings.Replace(link, pages.ForProductionPath, "/pricing", 1), 1))) == 0 {
		t.Error("CONTROL: the card pointed at /pricing passed")
	}
	if len(fitsLinkProblems(regexp.MustCompile(` aria-labelledby="[^"]*"`).ReplaceAllString(landing, ""))) == 0 {
		t.Error("CONTROL: the card named by its whole text passed")
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
	"Gloves, flour":         "nobody has pressed the tap button in gloves (LP-10 review)",
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
	// On this page no glove at all (the product tour's "wet or gloved finger" is its own
	// sentence, on its own page).
	if gloveRE.MatchString(text) {
		t.Errorf("/for/production says %q; nobody has pressed the tap button in gloves", gloveRE.FindString(text))
	}
	if !gloveRE.MatchString(text + " sized for a wet or gloved finger") {
		t.Error("CONTROL: \"gloved\" planted in the page passed")
	}
}

// gloveRE is a glove in any form.
var gloveRE = regexp.MustCompile(`(?i)\bglove`)

// forProductionSampleRegions are the drawings of invented people, each from its first
// mark to where it ends, and what visibly says they are samples there.
var forProductionSampleRegions = []struct{ name, from, to string }{
	{"the yardstick", `class="card lp-fpTimeline"`, `class="lp-prodBullets"`},
	{"The Pass", `class="lp-fpSplit"`, `</section>`},
	{"the queue", `class="card lp-fpQueue"`, `class="lp-prodBullets"`},
	{"the floor's phone", `class="lp-fpPhoneWrap"`, `</section>`},
}

// forProductionSampleProblems is every drawing of an invented person whose visible text
// (attributes left out: an aria-label is not seen) does not say it is a sample.
func forProductionSampleProblems(body string) []string {
	var out []string
	var people []string
	for _, list := range [][]pages.ForProductionDocket{pages.ForProductionYardstick, pages.ForProductionPassDockets, pages.ForProductionQueue} {
		for _, d := range list {
			people = append(people, d.Who)
		}
	}
	for _, r := range forProductionSampleRegions {
		at := strings.Index(body, r.from)
		if at < 0 {
			out = append(out, r.name+" is not drawn")
			continue
		}
		region := body[at:]
		if end := strings.Index(region, r.to); end >= 0 {
			region = region[:end]
		}
		seen := fitsText(region)
		if !slices.ContainsFunc(people, func(p string) bool { return strings.Contains(seen, p) }) {
			out = append(out, "PREMISE: "+r.name+" draws none of the sample people")
		}
		if !strings.Contains(seen, pages.ForProductionSampleNote) && !strings.Contains(seen, "· sample") {
			out = append(out, r.name+" draws invented people and does not say, where they stand, that they are samples")
		}
	}
	return out
}

// TestForProductionPage_EveryDrawingOfInventedPeopleSaysItIsASample: the yardstick sits
// under "from our demo data" and the phone greets an invented person, so each drawing of
// one carries a visible sample note (README §7 step 5). CONTROL: the yardstick's and the
// phone's notes taken away are each red.
func TestForProductionPage_EveryDrawingOfInventedPeopleSaysItIsASample(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), pages.ForProductionPath)
	for _, p := range forProductionSampleProblems(body) {
		t.Error(p)
	}
	for _, note := range []string{`<p class="lp-fpSampleNote">`, `<p class="lp-prodNote lp-fpPhoneNote">`} {
		at := strings.Index(body, note)
		if at < 0 {
			t.Fatalf("PREMISE: %s is not on the page", note)
		}
		cut := body[:at] + strings.Replace(body[at:], pages.ForProductionSampleNote, "", 1)
		if len(forProductionSampleProblems(cut)) == 0 {
			t.Errorf("CONTROL: the drawing without %s passed", note)
		}
	}
}

// forProductionReadableProblems is every way the demo-data table and the yardstick's
// words would be hidden from a screen reader: inside an element whose role is img, the
// browser reads the element's label and none of its text.
func forProductionReadableProblems(body string) []string {
	var out []string
	for _, r := range []struct{ name, from, to string }{
		{"the gate", `class="card lp-fpGate"`, `class="lp-strip"`},
		{"the shifts timeline", `class="card lp-fpTimeline"`, `class="lp-prodBullets"`},
	} {
		at := strings.Index(body, r.from)
		end := strings.Index(body[max(at, 0):], r.to)
		if at < 0 || end < 0 {
			out = append(out, r.name+" is not drawn")
			continue
		}
		region := body[at : at+end]
		roles := regexp.MustCompile(`<[a-z]+ class="([^"]*)"[^>]*role="img"`).FindAllStringSubmatch(region, -1)
		switch {
		case r.name == "the gate" && (len(roles) != 1 || roles[0][1] != "lp-fpPlaque"):
			out = append(out, fmt.Sprintf("the gate's pictures are %v; only the plaque is one, the departments are a list", roles))
		case r.name != "the gate" && len(roles) != 0:
			out = append(out, fmt.Sprintf("the shifts timeline holds the pictures %v; its rows and caption are text", roles))
		}
		if r.name == "the gate" && !regexp.MustCompile(`(?s)role="img".*?</div>\s*<ol class="lp-fpDepts"`).MatchString(region) {
			out = append(out, "the gate's department list is not after the plaque's picture closes")
		}
		if strings.Contains(region, `class="card lp-fp`) && regexp.MustCompile(`^class="card lp-fp[A-Za-z]+"[^>]*role=`).MatchString(region) {
			out = append(out, r.name+"'s card is itself a picture")
		}
	}
	return out
}

// TestForProductionPage_TheDemoDataIsReadAloud: the gate's departments and their starts, the
// timeline's rows and the yardstick's caption are text a screen reader reaches (K1's table
// is the page's content, not a picture of it). CONTROL: the timeline card made a picture,
// and the gate's list moved inside the plaque's picture, are each red.
func TestForProductionPage_TheDemoDataIsReadAloud(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), pages.ForProductionPath)
	for _, p := range forProductionReadableProblems(body) {
		t.Error(p)
	}
	if len(forProductionReadableProblems(strings.Replace(body, `class="card lp-fpTimeline"`, `class="card lp-fpTimeline" role="img" aria-label="x"`, 1))) == 0 {
		t.Error("CONTROL: the timeline as a picture passed")
	}
	if len(forProductionReadableProblems(strings.Replace(body, `<div class="lp-fpGateRow">`, `<div class="lp-fpGateRow" role="img" aria-label="x">`, 1))) == 0 {
		t.Error("CONTROL: the gate's row as a picture passed")
	}
}
