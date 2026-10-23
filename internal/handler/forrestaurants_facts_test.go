package handler

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
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
	// LedgerTest is internal/domain/ledger/report_test.go: the page's week, booked by the
	// ledger itself (TestAccumulate_TheRestaurantsPagesWeek). A source scan of report.go
	// is blind to a later line that overwrites what it read; the ledger's own case is not.
	LedgerTest string
	// CheckinDBTest is this package's forrestaurants_db_test.go: the cover claim run
	// through the real tap path against Postgres.
	CheckinDBTest string
	Activate      string // internal/handler/activate.go, comments removed
	SunParams     string // internal/sun/params.go, comments removed
	// GoFiles is every hand-written, non-test Go file under internal/ and cmd/ (comments
	// removed), by path: what could issue a session.
	GoFiles map[string]string
	// StaleHours is the page's mirror of tap.StaleOpenIn.
	StaleHours int
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

		LedgerTest:    repoFile(t, "internal", "domain", "ledger", "report_test.go"),
		CheckinDBTest: repoFile(t, "internal", "handler", "forrestaurants_db_test.go"),
		Activate:      strip(repoFile(t, "internal", "handler", "activate.go")),
		SunParams:     strip(repoFile(t, "internal", "sun", "params.go")),
		GoFiles:       productGoFiles(t),
		StaleHours:    pages.RestaurantsStaleHours,
	}
}

// productGoFiles is every hand-written, non-test Go file under internal/ and cmd/, comments
// removed, by its path from the repository root. Generated sqlc code (internal/store) is
// left out: it declares the queries, it calls nothing.
func productGoFiles(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]string{}
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
				strings.Contains(filepath.ToSlash(path), "internal/store/") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			out[filepath.ToSlash(rel)] = goLineCommentRE.ReplaceAllString(string(b), "")
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", top, err)
		}
	}
	if len(out) < 100 || out["internal/handler/activate.go"] == "" || out["internal/session/manager.go"] == "" {
		t.Fatalf("PREMISE: read %d Go file(s) and not activate.go and the session manager; the walk is reading the wrong tree", len(out))
	}
	return out
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

// ledgerPageConstRE reads one of the page's times out of the ledger's test file.
var ledgerPageConstRE = regexp.MustCompile(`(?m)^\s*restaurantsPage(\w+)\s*=\s*"(\d\d:\d\d)"`)

// ledgerWeekProblem says how internal/domain/ledger's TestAccumulate_TheRestaurantsPagesWeek
// departs from the page: a time the page draws that the case does not book, or a case that
// no longer asserts what the fact says (want, fragments of the function's body).
func ledgerWeekProblem(src string, want ...string) string {
	mins := func(hhmm string) int {
		at, err := time.Parse("15:04", hhmm)
		if err != nil {
			return -1
		}
		return at.Hour()*60 + at.Minute()
	}
	body := goFuncBody(src, "func TestAccumulate_TheRestaurantsPagesWeek(")
	if body == "" {
		return "internal/domain/ledger no longer has TestAccumulate_TheRestaurantsPagesWeek, the page's week"
	}
	times := map[string]string{}
	for _, m := range ledgerPageConstRE.FindAllStringSubmatch(src, -1) {
		times[m[1]] = m[2]
	}
	steps := pages.RestaurantsForgotSteps
	cover := mins(times["CoverOut"]) - mins(times["CoverIn"])
	for name, page := range map[string]string{
		"OvernightIn":  pages.RestaurantsOvernightIn.At,
		"OvernightOut": pages.RestaurantsOvernightOut.At,
		"CoverIn":      pages.RestaurantsCoverDocket.At,
	} {
		if times[name] != page {
			return fmt.Sprintf("the ledger's case books %s at %q and the page draws %q", name, times[name], page)
		}
	}
	if len(steps) != 3 || !strings.Contains(steps[0].Detail, "IN "+times["ForgotIn"]) || !strings.Contains(steps[2].Detail, "OUT "+times["ForgotOut"]) {
		return fmt.Sprintf("the forgotten check-out the page draws is not the ledger's %s / %s", times["ForgotIn"], times["ForgotOut"])
	}
	if cover != int(pages.LandingPanelRollRows[0].Days[3]) {
		return fmt.Sprintf("the ledger's cover is %d minutes and The Roll's Thursday draws %d", cover, pages.LandingPanelRollRows[0].Days[3])
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			return "the ledger's case no longer asserts " + strconv.Quote(w)
		}
	}
	return ""
}

// shiftDayViolation is FactShiftCountsOnItsStartDayAndVenue: the ledger books the page's
// overnight shift on Monday, all of it, at Rusty Bar (the case), and accumulate still reads
// the check-in for both (the source).
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
	return ledgerWeekProblem(s.LedgerTest, "l.Daily[0] != span || l.Daily[1] != 0",
		"rep.Venues[0].LocationID != rusty || rep.Venues[0].Worked != span",
		"moved.Venues[0].LocationID != rusty || moved.Venues[0].Worked != span")
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
	body := goFuncBody(s.CheckinDBTest, "func TestCheckinDB_ATapAtAnotherVenueIsJudgedOnThatVenuesEvidence(")
	for _, w := range []string{`"198.51.100.7:41234"`, `"203.0.113.5:41234"`, "*here.Trust != 100", `home.Verdict != "flag"`,
		"*here.LocationID != h.locationID"} {
		if !strings.Contains(body, w) {
			return "TestCheckinDB_ATapAtAnotherVenueIsJudgedOnThatVenuesEvidence no longer asserts " + strconv.Quote(w)
		}
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
	return ledgerWeekProblem(s.LedgerTest, "!a.Marks[3].Other || away.Totals.OtherVenue != 1",
		"a.Daily[3] != worked || a.Worked != h.Worked || h.Marks[3].Other")
}

// setupViolation is FactSetupTakesARealTap: the only way to a session is activation, and
// activation ends only on a tap that carries the plaque's signature (NFC).
func setupViolation(s restaurantsFactSources) string {
	if !strings.Contains(goFuncBody(s.SunParams, "func (p Params) HasSUN("), "return p.Channel == ChannelNFC") {
		return "sun.Params.HasSUN no longer means an NFC read"
	}
	body := goFuncBody(s.Activate, "func (a *Activation) CompleteByTap(")
	guard := strings.Index(body, "if !p.HasSUN() {")
	finish := strings.Index(body, "a.finishActivation(")
	if guard < 0 || finish < 0 || guard > finish {
		return "Activation.CompleteByTap no longer refuses a tap without the plaque's signature before it finishes"
	}
	if block := body[guard:]; !regexp.MustCompile(`^if !p\.HasSUN\(\) \{[^}]*problemActivationNeedsTouch\)\s*return\s*\}`).MatchString(block) {
		return "the refusal of a tap without the plaque's signature no longer ends the activation there"
	}
	issueRE := regexp.MustCompile(`\.sessions\.Issue\(|session\.IssueParams\{|\.CreateSession\(`)
	for _, path := range slices.Sorted(maps.Keys(s.GoFiles)) {
		src := s.GoFiles[path]
		for _, at := range issueRE.FindAllStringIndex(src, -1) {
			switch path {
			case "internal/session/manager.go":
				continue // the issue itself
			case "internal/handler/activate.go":
				if fin := goFuncBody(src, "func (a *Activation) finishActivation("); fin != "" && strings.Contains(fin, src[at[0]:at[1]]) {
					continue
				}
			}
			return path + " issues a session outside activation (" + src[at[0]:at[1]] + ")"
		}
		if n := strings.Count(src, "a.finishActivation("); n > 0 && (path != "internal/handler/activate.go" || n != 1) {
			return path + " calls finishActivation outside CompleteByTap"
		}
	}
	if !strings.Contains(s.GoFiles["internal/session/manager.go"], ".CreateSession(") {
		return "internal/session no longer creates the session; this scan reads the wrong place"
	}
	return ""
}

// staleProbe is a tap at KF Hamrun the morning after a check-in nobody closed: 24 hours
// and 28 minutes later.
func staleProbe() tap.Input {
	in := panelEngineInput(pages.PanelDocket{Venue: "KF Hamrun", Who: "Sample Person", Direction: "OUT", At: "10:30",
		Channel: "nfc", IP: "yes", GPS: "no", Verdict: "ok", Trust: "70"}, false)
	in.Now = time.Date(2026, 10, 13, 10, 30, 0, 0, time.UTC)
	in.Employee.ActivatedAt = in.Now.Add(-30 * 24 * time.Hour)
	in.LastOpenIn = &tap.Transaction{ID: uuid.New(), OccurredAt: time.Date(2026, 10, 12, 10, 2, 0, 0, time.UTC), Direction: tap.TypeIn}
	return in
}

// forgotViolation is FactForgottenCheckOutIsNotPaid: the engine closes a check-in older
// than its stale threshold with a check-out and the note, the page's figure is that
// threshold, and the ledger does not pay the interval, lists the check-in as open and pays
// it once a manager's check-out closes it (the case).
func forgotViolation(s restaurantsFactSources) string {
	if time.Duration(s.StaleHours)*time.Hour != tap.StaleOpenIn {
		return fmt.Sprintf("the page says %d hours and the engine's threshold is %v", s.StaleHours, tap.StaleOpenIn)
	}
	d := s.Decide(staleProbe())
	if d.Type == nil || *d.Type != tap.TypeOut || !strings.Contains(d.Note, tap.StaleOpenInNote) {
		return fmt.Sprintf("a tap a day after a check-in nobody closed is %v with the note %q, not a check-out noted as a forgotten one", d.Type, d.Note)
	}
	return ledgerWeekProblem(s.LedgerTest, "gap <= tap.StaleOpenIn",
		"f.Worked != 0 || left.Totals.TooLong != 1 || len(left.Open) != 1", "manual())",
		"find(t, early, maria).Daily[0] != want || len(early.Open) != 0",
		"c.Daily[0] != want || len(closed.Open) != 0")
}

var restaurantsDerivationFuncs = map[pages.Fact]func(restaurantsFactSources) string{
	pages.FactDirectionFollowsLastOpenCheckIn:     directionViolation,
	pages.FactShiftCountsOnItsStartDayAndVenue:    shiftDayViolation,
	pages.FactTapIsJudgedAtTheTappedVenue:         tappedVenueViolation,
	pages.FactCrossLocationTapIsNotedNotPenalised: crossLocationViolation,
	pages.FactRollMarksWorkAtAnotherVenue:         rollOtherViolation,
	pages.FactSetupTakesARealTap:                  setupViolation,
	pages.FactForgottenCheckOutIsNotPaid:          forgotViolation,
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
	ledger := func(s *restaurantsFactSources) *string { return &s.LedgerTest }
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
		{pages.FactRollMarksWorkAtAnotherVenue, "the ledger's case stops asserting the mark", replace(ledger,
			"!a.Marks[3].Other || away.Totals.OtherVenue != 1", "away.Totals.OtherVenue != 1")},
		{pages.FactShiftCountsOnItsStartDayAndVenue, "the ledger's case books another night", replace(ledger,
			`restaurantsPageOvernightIn  = "17:58"`, `restaurantsPageOvernightIn  = "18:30"`)},
		{pages.FactShiftCountsOnItsStartDayAndVenue, "the ledger's case is gone", replace(ledger,
			"func TestAccumulate_TheRestaurantsPagesWeek(", "func renamedCase(")},
		{pages.FactTapIsJudgedAtTheTappedVenue, "the DB test stops checking the home network's tap", replace(func(s *restaurantsFactSources) *string { return &s.CheckinDBTest },
			`home.Verdict != "flag"`, `home.Verdict == ""`)},
		{pages.FactSetupTakesARealTap, "a QR read counts as the plaque's signature", replace(func(s *restaurantsFactSources) *string { return &s.SunParams },
			"return p.Channel == ChannelNFC", "return p.Channel != \"\"")},
		{pages.FactSetupTakesARealTap, "activation stops refusing a tap without the signature", replace(func(s *restaurantsFactSources) *string { return &s.Activate },
			"if !p.HasSUN() {", "if false {")},
		{pages.FactSetupTakesARealTap, "another file issues a session", func(s *restaurantsFactSources) {
			m := maps.Clone(s.GoFiles)
			m["internal/handler/qr.go"] += "\nfunc qrSetup() { _, _ = a.sessions.Issue(ctx, session.IssueParams{}) }\n"
			s.GoFiles = m
		}},
		{pages.FactForgottenCheckOutIsNotPaid, "the page's hours drift from the engine's", func(s *restaurantsFactSources) { s.StaleHours = 12 }},
		{pages.FactForgottenCheckOutIsNotPaid, "the engine writes no note on a forgotten check-out", engine(func(_ tap.Input, d tap.Decision) tap.Decision {
			d.Note = strings.ReplaceAll(d.Note, tap.StaleOpenInNote, "")
			return d
		})},
		{pages.FactForgottenCheckOutIsNotPaid, "the ledger's case stops asserting the unpaid interval", replace(ledger,
			"f.Worked != 0 || left.Totals.TooLong != 1 || len(left.Open) != 1", "f.Worked != 0")},
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
	// The steps are words: an ordered list a screen reader walks, not a picture.
	page := mustFetchMarketing(t, marketingRouter(t), pages.ForRestaurantsPath)
	list := regexp.MustCompile(`(?s)<ol class="lp-forSteps[^"]*"[^>]*>.*?</ol>`).FindString(page)
	if strings.Count(list, `<li class="card lp-forStep">`) != len(steps) || strings.Contains(list, `role="img"`) {
		t.Errorf("the forgotten check-out's steps are not an ordered list of %d items", len(steps))
	}
	// The overnight drawing's name says how the two dockets are stamped.
	if !strings.Contains(text, "paired into one shift of "+pages.RollHours(minutes)+"; both approved") {
		t.Error("the overnight drawing's name does not say both taps were approved")
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

	// Every drawing that names a sample person says, in a label the eye reads, that it
	// is a sample.
	body := mustFetchMarketing(t, marketingRouter(t), pages.ForRestaurantsPath)
	who := []string{in.Who, cover.Who}
	for _, p := range sampleLabelProblems(body, who) {
		t.Error(p)
	}
	for _, lbl := range []string{"An overnight shift at Rusty Bar · sample", "The Roll · sample"} {
		if len(sampleLabelProblems(strings.Replace(body, lbl, strings.TrimSuffix(lbl, " · sample"), 1), who)) == 0 {
			t.Errorf("CONTROL: %q without its \"sample\" passed", lbl)
		}
	}
}

// sampleDrawingStartRE opens one drawing of the page that can name a sample person.
var sampleDrawingStartRE = regexp.MustCompile(`<div class="card lp-forCard"|<div class="lp-forRoll">|</section>`)

// sampleLabelProblems names every drawing of body that draws one of who and carries no
// visible label (.lp-lbl) saying it is a sample.
func sampleLabelProblems(body string, who []string) []string {
	var out []string
	starts := sampleDrawingStartRE.FindAllStringIndex(body, -1)
	drawn := 0
	for i, st := range starts {
		if body[st[0]:st[1]] == "</section>" {
			continue
		}
		end := len(body)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		block := body[st[0]:end]
		if !slices.ContainsFunc(who, func(w string) bool { return strings.Contains(block, ">"+w+"<") }) {
			continue
		}
		drawn++
		if !regexp.MustCompile(`<span class="lp-lbl">[^<]*· sample</span>`).MatchString(block) {
			out = append(out, fmt.Sprintf("a drawing of a sample person carries no visible sample label: %.80q", block))
		}
	}
	if drawn < 3 {
		out = append(out, fmt.Sprintf("PREMISE: found %d drawing(s) of a sample person; the page draws three", drawn))
	}
	return out
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
	// The five inner hour marks sit on the axis's own labels between its two ends. A
	// whole percentage is written without decimals ("50%").
	for i, h := range axis[1 : len(axis)-1] {
		v := strings.TrimSuffix(strings.TrimRight(strings.TrimSuffix(pct(h, 4), "%"), "0"), ".") + "%"
		want[".lp .lp-forTick"+strconv.Itoa(i+1)] = []string{"left: " + v}
	}
	var probs []string
	// The axis labels are equally spaced, which is what lets the stylesheet spread them
	// with space-between and no position each.
	for i := 1; i < len(axis); i++ {
		if step := (clockMinutes(t, axis[i]) + 24*60 - clockMinutes(t, axis[i-1])) % (24 * 60); step*(len(axis)-1) != span {
			probs = append(probs, fmt.Sprintf("the axis steps %d minutes from %s to %s; its marks are not evenly spaced", step, axis[i-1], axis[i]))
		}
	}
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
// classes put the two taps, the shift, midnight and the five inner hour marks where their
// times fall on its axis, the axis is evenly spaced and printed as declared, and the shift
// is Rusty Bar's in the seed. CONTROL: a tap and an hour mark moved in the stylesheet are
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
	tick := strings.Replace(css, ".lp .lp-forTick3 { left: 50%; }", ".lp .lp-forTick3 { left: 58%; }", 1)
	if tick == css || len(restaurantsTimelineProblems(t, tick)) == 0 {
		t.Error("CONTROL: the 22:00 mark drawn at 58% passed")
	}
	// The axis the page prints is the axis the positions are computed on.
	text := renderedPublicText(t, pages.ForRestaurantsPath)
	if !strings.Contains(text, strings.Join(pages.RestaurantsTimelineAxis, " ")) {
		t.Errorf("/for/restaurants does not print the axis %v", pages.RestaurantsTimelineAxis)
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
		openRE := regexp.MustCompile(`<a class="card fitcard lp-fit" href="` + regexp.QuoteMeta(a.Href) +
			`" aria-labelledby="([^"]*)" aria-describedby="([^"]*)">`)
		m := openRE.FindStringSubmatchIndex(section)
		if m == nil {
			out = append(out, fmt.Sprintf("the %q card is not one link to %s, named and described by its own parts", a.Title, a.Href))
			continue
		}
		card := section[m[1]:]
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
		// The link's name is what its labelledby ids hold, inside the card; its description
		// the paragraph. Not the card's whole text (the LP-09 review measured ~279 characters).
		textOf := func(ids string) string {
			var parts []string
			for _, id := range strings.Fields(ids) {
				el := regexp.MustCompile(`(?s)<(\w+)[^>]*\bid="` + regexp.QuoteMeta(id) + `"[^>]*>(.*?)</(?:\w+)>`).FindStringSubmatch(card)
				if el == nil {
					return "(no element " + id + " inside the card)"
				}
				parts = append(parts, strings.Join(strings.Fields(tagRE.ReplaceAllString(el[2], " ")), " "))
			}
			return strings.Join(parts, " ")
		}
		name, desc := textOf(section[m[2]:m[3]]), textOf(section[m[4]:m[5]])
		if label == "" || a.Link != label || name != templEscape(a.Title)+" "+templEscape(label) {
			out = append(out, fmt.Sprintf("the %q card's link is named %q; want its title and the Product column's %q", a.Title, name, label))
		}
		if desc != a.Paragraph() {
			out = append(out, fmt.Sprintf("the %q card's link is described by %q; want its paragraph", a.Title, desc))
		}
		if !strings.HasPrefix(section[m[2]:m[3]], "fit-"+filepath.Base(a.Href)+"-") {
			out = append(out, fmt.Sprintf("the %q card's ids are not its page's own (%q)", a.Title, section[m[2]:m[3]]))
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
// page is not mounted is no link. The link is NAMED by the card's title and foot and
// DESCRIBED by its paragraph (aria-labelledby / aria-describedby, ids inside the card,
// "fit-<page>-…"), not announced as its whole text. The link IS the card -- the element carrying the card's
// edge and corner -- so the one focus ring of the public site (.lp :focus-visible) is drawn
// around the card, and nothing on .fitcard takes it away. CONTROL: a link nested in the
// card, the card pointed elsewhere, the link named by its paragraph and the ring switched
// off are red.
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

	nested := strings.Replace(landing, `<span class="lp-fitLink" id="fit-restaurants-link">`, `<a href="/pricing">x</a><span class="lp-fitLink" id="fit-restaurants-link">`, 1)
	if len(fitCardProblems(nested)) == 0 {
		t.Error("CONTROL: a link nested in the card passed")
	}
	elsewhere := strings.Replace(landing, `<a class="card fitcard lp-fit" href="`+pages.ForRestaurantsPath+`"`, `<a class="card fitcard lp-fit" href="/pricing"`, 1)
	if len(fitCardProblems(elsewhere)) == 0 {
		t.Error("CONTROL: the card pointed at /pricing passed")
	}
	whole := strings.Replace(landing, ` aria-labelledby="fit-restaurants-title fit-restaurants-link"`, ` aria-labelledby="fit-restaurants-title fit-restaurants-body"`, 1)
	if whole == landing || len(fitCardProblems(whole)) == 0 {
		t.Error("CONTROL: the link named by its paragraph passed")
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
	"is not paid": "a sooner tap closes an open check-in as an ordinary check-out whose hours count, and the product counts hours, it pays nothing",
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
