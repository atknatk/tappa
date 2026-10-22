package handler

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
	"github.com/google/uuid"
)

// THE PANEL SECTION (LP-05): three drawings of the manager's screens on the landing page,
// each beside a line about it.
//
// Two different things are held here, and they are held differently:
//
//   - THE SENTENCES are Lines in pages and join the Fact regime like every other sentence
//     on the page (landingLines, claimedFacts). Three facts are new; their derivations are
//     the three *Violation functions below, wired into factDerivations, and each has its
//     negative control in TestFactMechanisms_SayNoWhenTheFactIsAbsent.
//   - THE DRAWINGS are invented sample data, labelled as such on the page, and still not
//     free: a drawing of the panel that stamped a record the engine would not stamp, or
//     printed a score §5 cannot produce, is a claim in a picture. So every sample record
//     is run through tap.Decide and the panel's own words, and the venues are the seed's.

// --- the three facts' derivations --------------------------------------------------

// panelFactSources is what the three panel facts read, as text, so a negative control
// can take one piece away.
type panelFactSources struct {
	Queries      string // db/queries/transactions.sql
	Ledger       string // internal/domain/ledger/ledger.go
	Transactions string // web/templates/pages/transactions.templ
	Docket       string // web/templates/components/docket.templ
	Mig05        string // db/migrations/00005_create_transactions_audit_reviews.sql
	Review       string // web/templates/pages/review.templ
	Report       string // internal/domain/ledger/report.go
	Reports      string // web/templates/pages/reports.templ
}

func loadPanelFactSources(t *testing.T) panelFactSources {
	t.Helper()
	return panelFactSources{
		Queries:      repoFile(t, "db", "queries", "transactions.sql"),
		Ledger:       repoFile(t, "internal", "domain", "ledger", "ledger.go"),
		Transactions: repoFile(t, "web", "templates", "pages", "transactions.templ"),
		Docket:       repoFile(t, "web", "templates", "components", "docket.templ"),
		Mig05:        repoFile(t, "db", "migrations", "00005_create_transactions_audit_reviews.sql"),
		Review:       repoFile(t, "web", "templates", "pages", "review.templ"),
		Report:       repoFile(t, "internal", "domain", "ledger", "report.go"),
		Reports:      repoFile(t, "web", "templates", "pages", "reports.templ"),
	}
}

// templBody is the source of one templ component, from its declaration to the next
// top-level declaration, or "" when there is no such component.
func templBody(src, name string) string {
	at := strings.Index(src, "\ntempl "+name+"(")
	if at < 0 {
		return ""
	}
	rest := src[at+1:]
	if end := regexp.MustCompile(`\n(?:templ|func|type|var|const) `).FindStringIndex(rest[1:]); end != nil {
		return rest[:end[0]+1]
	}
	return rest
}

// queryBlock is one sqlc query's text, from its "-- name:" line to the next one.
func queryBlock(src, name string) string {
	at := strings.Index(src, "-- name: "+name+" ")
	if at < 0 {
		return ""
	}
	rest := src[at:]
	if end := strings.Index(rest[1:], "-- name: "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

var lastOrderByRE = regexp.MustCompile(`(?i)ORDER BY\s+([^\n;]+)`)

// passRailViolation is FactPassShowsTheDaysDocketsNewestFirst: the panel's record query
// is newest first, the ledger reads it, The Pass hangs those records on its rail as
// RailDockets, and a RailDocket prints the verdict's stamp.
func passRailViolation(s panelFactSources) string {
	q := queryBlock(s.Queries, "ListPanelTransactions")
	if q == "" {
		return "db/queries/transactions.sql no longer declares ListPanelTransactions"
	}
	orders := lastOrderByRE.FindAllStringSubmatch(q, -1)
	if len(orders) == 0 || !strings.HasPrefix(strings.TrimSpace(orders[len(orders)-1][1]), "t.occurred_at DESC") {
		return "ListPanelTransactions no longer ends ORDER BY t.occurred_at DESC, so the day's records are not newest first"
	}
	if !strings.Contains(s.Ledger, ".ListPanelTransactions(") {
		return "internal/domain/ledger no longer reads ListPanelTransactions, so The Pass's records come from elsewhere"
	}
	if !strings.Contains(templBody(s.Transactions, "passBody"), "@rail(v)") {
		return "pages/transactions.templ's passBody no longer hangs the rail"
	}
	rail := templBody(s.Transactions, "rail")
	if !strings.Contains(rail, "range v.Dockets") || !strings.Contains(rail, "@components.RailDocket(d)") {
		return "The Pass's rail no longer hangs a RailDocket for each of the page's records"
	}
	if !strings.Contains(templBody(s.Docket, "RailDocket"), "@VerdictStamp(d.Verdict)") {
		return "components.RailDocket no longer prints the verdict's stamp"
	}
	stamp := templBody(s.Docket, "VerdictStamp")
	for _, w := range []string{`stamp--approved">APPROVED<`, `stamp--flagged">FLAGGED<`, `stamp--rejected">REJECTED<`, `stamp--ignored">IGNORED<`} {
		if !strings.Contains(stamp, w) {
			return "components.VerdictStamp no longer prints " + strconv.Quote(w)
		}
	}
	return ""
}

var reviewOutcomeCheckRE = regexp.MustCompile(`outcome\s+text\s+NOT NULL\s+CHECK\s*\(\s*outcome\s+IN\s*\(\s*'approved'\s*,\s*'rejected'\s*\)\s*\)`)

// reviewOutcomeViolation is FactReviewIsApproveOrReject: a decision is one of exactly two
// outcomes, and The Spike's form offers both.
func reviewOutcomeViolation(s panelFactSources) string {
	at := strings.Index(s.Mig05, "CREATE TABLE transaction_reviews")
	if at < 0 || !reviewOutcomeCheckRE.MatchString(s.Mig05[at:]) {
		return "migration 0005's transaction_reviews.outcome is no longer the closed set approved|rejected"
	}
	for _, o := range []string{"approved", "rejected"} {
		if !strings.Contains(s.Review, `name="outcome" value="`+o+`"`) {
			return "pages/review.templ's decision form no longer sends outcome=" + o
		}
	}
	return ""
}

var reportDaysRE = regexp.MustCompile(`(?m)^const ReportDays = 7\b`)

// rollWeekViolation is the source half of FactRollIsTheWeekPerPerson: the report's period
// is seven days and The Roll prints one row per person with a cell per day. (The other
// half -- WeekOf starts on a Monday -- is run, not read: see factDerivations.)
func rollWeekViolation(s panelFactSources) string {
	if !reportDaysRE.MatchString(s.Report) {
		return "internal/domain/ledger's ReportDays is no longer 7, so a report is not a week"
	}
	body := templBody(s.Reports, "reportPeople")
	if !strings.Contains(body, "range v.People") || !strings.Contains(body, "range v.DayHeadings") {
		return "pages/reports.templ's reportPeople no longer prints a row per person with a cell per day"
	}
	return ""
}

// rollWeekStartsOnMonday runs ledger.WeekOf on a Wednesday.
func rollWeekStartsOnMonday() string {
	p := ledger.WeekOf(ledger.Date{Year: 2026, Month: time.October, Day: 14}, time.UTC)
	if p.From.Weekday() != time.Monday || p.To.Sub(p.From) != 7*24*time.Hour {
		return fmt.Sprintf("ledger.WeekOf(Wed 14 Oct 2026) is %s to %s, not Monday to Monday", p.From, p.To)
	}
	return ""
}

// --- the drawings: what the product would draw for each sample record --------------

// panelEngineInput is a tap that leaves exactly the sample record's evidence: the plaque
// is active and its signature good (a QR scan has none), the person is signed in and at
// their home venue, the address is on the venue's network or not, and the position is
// at the venue, far from it, or absent.
func panelEngineInput(d pages.PanelDocket, far bool) tap.Input {
	now := time.Date(2026, 10, 20, 9, 59, 0, 0, time.UTC)
	loc := uuid.NewSHA1(uuid.Nil, []byte(d.Venue))
	here := geo.Point{Lat: 35.8989, Lng: 14.5146}
	in := tap.Input{
		Now:         now,
		Channel:     tap.Channel(d.Channel),
		SUN:         tap.SUNResult{Valid: d.Channel == string(tap.ChannelNFC)},
		Tag:         &tap.Tag{Status: tap.TagActive, Location: loc},
		Employee:    &tap.Employee{Status: tap.EmployeeActive, Location: &loc, ActivatedAt: now.Add(-48 * time.Hour)},
		SourceIP:    netip.MustParseAddr("198.51.100.7"),
		LocationIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")},
		LocationGPS: &here,
		GPSRadiusM:  150,
		Debounce:    60 * time.Second,
		PolicySet: policy.Set{
			Guardrails: policy.DefaultGuardrails(),
			Baseline:   policy.BaselinePolicies(func(name string) uuid.UUID { return uuid.NewSHA1(uuid.Nil, []byte(name)) }),
		},
	}
	if d.IP == "yes" {
		in.SourceIP = netip.MustParseAddr("203.0.113.7")
	}
	switch {
	case d.GPS == "yes":
		in.GPS = &geo.Point{Lat: here.Lat, Lng: here.Lng}
	case far:
		in.GPS = &geo.Point{Lat: 35.9100, Lng: 14.5300} // about 2 km away
	}
	// An OUT is drawn as the close of the person's open check-in (Luke Borg's 02:04 ends
	// Rusty Bar's overnight shift), so the input carries one: the engine then answers
	// "out" by §5's toggle against the last open entry, not by the clock.
	if d.Direction == "OUT" {
		in.LastOpenIn = &tap.Transaction{ID: uuid.New(), OccurredAt: now.Add(-8 * time.Hour), Direction: tap.TypeIn}
	}
	return in
}

// panelDocketProblems is every way a sample docket differs from what the engine decides
// for its evidence and the docket would print: the stamp, the score, the two signs and
// the direction. rule is the rule the record must have been decided by ("" for any).
func panelDocketProblems(d pages.PanelDocket, rule string) []string {
	far := rule == policy.SidGPSConflictReview
	dec := tap.Decide(panelEngineInput(d, far))
	var out []string
	say := func(f string, a ...any) { out = append(out, d.Who+" ("+d.At+"): "+fmt.Sprintf(f, a...)) }
	if string(dec.Verdict) != d.Verdict {
		say("the drawing stamps %q and the engine decides %q (rule %s)", d.Verdict, dec.Verdict, dec.MatchedSid)
	}
	if strconv.Itoa(dec.Trust) != d.Trust {
		say("the drawing prints trust %s and §5 scores this evidence %d", d.Trust, dec.Trust)
	}
	ip, gps := dec.IPMatch, dec.GPSMatch
	if sign(&ip) != d.IP || sign(&gps) != d.GPS {
		say("the drawing prints IP %s · GPS %s and a tap's docket prints IP %s · GPS %s", d.IP, d.GPS, sign(&ip), sign(&gps))
	}
	if dec.Type == nil || strings.ToUpper(string(*dec.Type)) != d.Direction {
		say("the drawing says %s and the engine's direction is %v", d.Direction, dec.Type)
	}
	if rule != "" && dec.MatchedSid != rule {
		say("the drawing names the rule %s and the engine decided by %s", rule, dec.MatchedSid)
	}
	return out
}

var hhmmRE = regexp.MustCompile(`^\d\d:\d\d$`)

// panelSampleProblems is every way the panel drawings differ from what the panel would
// draw for the same records.
func panelSampleProblems(pass []pages.PanelDocket, queue []pages.PanelQueueItem, focus pages.PanelFocus, day string, venues []string) []string {
	var out []string
	if len(pass) != 4 {
		out = append(out, fmt.Sprintf("The Pass draws %d docket(s); the boards draw four (two on the phone)", len(pass)))
	}
	all := slices.Clone(pass)
	for _, q := range queue {
		all = append(all, q.Docket)
	}
	for _, d := range all {
		if !slices.Contains(venues, d.Venue) {
			out = append(out, fmt.Sprintf("%s's record is at %q, which the seed does not have", d.Who, d.Venue))
		}
		if d.Verdict != string(tap.VerdictOK) && d.Verdict != string(tap.VerdictFlag) {
			out = append(out, fmt.Sprintf("%s's record carries the verdict %q, which the drawing has no stamp for", d.Who, d.Verdict))
		}
		if !hhmmRE.MatchString(d.At) {
			out = append(out, fmt.Sprintf("%s's record is at %q, not a time", d.Who, d.At))
		}
	}
	// The rail is newest on the left, and so is the queue (newest first).
	for i := 1; i < len(pass); i++ {
		if pass[i].At >= pass[i-1].At {
			out = append(out, fmt.Sprintf("The Pass hangs %s (%s) after %s (%s); its rail is newest on the left",
				pass[i].Who, pass[i].At, pass[i-1].Who, pass[i-1].At))
		}
	}
	for i := 1; i < len(queue); i++ {
		if queue[i].Docket.At >= queue[i-1].Docket.At {
			out = append(out, fmt.Sprintf("The Spike queues %s after %s; its queue is newest first", queue[i].Docket.Who, queue[i-1].Docket.Who))
		}
	}
	for _, d := range pass {
		out = append(out, panelDocketProblems(d, "")...)
	}
	for _, q := range queue {
		out = append(out, panelDocketProblems(q.Docket, q.Rule)...)
		if q.Docket.Verdict != string(tap.VerdictFlag) {
			out = append(out, q.Docket.Who+" is on The Spike with a record that is not flagged")
		}
		if why := components.SpikeWhy(components.DocketView{RuleSID: q.Rule}); why != q.Why {
			out = append(out, fmt.Sprintf("%s's queue line says %q; The Spike says %q for %s", q.Docket.Who, q.Why, why, q.Rule))
		}
		if q.When != "today "+q.Docket.At {
			out = append(out, fmt.Sprintf("%s's queue time %q is not the record's (%s) as The Spike prints today's", q.Docket.Who, q.When, q.Docket.At))
		}
	}
	if len(queue) == 0 {
		return append(out, "The Spike draws no queue")
	}
	// The record in focus is the queue's first, and it is The Pass's flagged docket.
	first := queue[0]
	if !slices.Contains(pass, first.Docket) {
		out = append(out, "The Spike's record in focus is not a docket on The Pass")
	}
	if !regexp.MustCompile(`^` + regexp.QuoteMeta(first.Docket.Venue) + ` · plaque [0-9A-F]{4}$`).MatchString(focus.Label) {
		out = append(out, fmt.Sprintf("the focus label %q is not the venue and the plaque's last four characters", focus.Label))
	}
	parts := strings.Split(focus.Line, " · ")
	if len(parts) != 3 || parts[0] != first.Docket.Direction || parts[2] != strings.ToUpper(first.Docket.Channel) {
		out = append(out, fmt.Sprintf("the focus line %q is not direction · time · channel", focus.Line))
	} else if at, err := time.Parse("2 Jan 15:04:05", parts[1]); err != nil || at.Format("15:04") != first.Docket.At || !strings.HasSuffix(day, at.Format(" 2 Jan")) {
		out = append(out, fmt.Sprintf("the focus line's time %q is not the record's (%s on %s)", parts[1], first.Docket.At, day))
	}
	var want []components.EvidenceTile
	for _, tile := range components.SpikeFocus(components.DocketView{
		Who: first.Docket.Who, Channel: first.Docket.Channel, IPSign: first.Docket.IP, GPSSign: first.Docket.GPS, RuleSID: first.Rule,
	}).Tiles {
		if tile.Label != "Who" { // the drawing leaves the person's tile out; the name is above
			want = append(want, tile)
		}
	}
	if len(want) != len(focus.Tiles) {
		out = append(out, fmt.Sprintf("the focus draws %d evidence tile(s); The Spike draws %d besides the person's", len(focus.Tiles), len(want)))
	} else {
		for i, tile := range focus.Tiles {
			if tile.Label != want[i].Label || tile.Value != want[i].Value || tile.OK != (want[i].Tone == "ok") {
				out = append(out, fmt.Sprintf("focus tile %d is %s %q ok=%v; The Spike draws %s %q tone %s",
					i, tile.Label, tile.Value, tile.OK, want[i].Label, want[i].Value, want[i].Tone))
			}
		}
	}
	return out
}

// rollSampleProblems is every way The Roll's drawing differs from what The Roll prints:
// the week's day headings, the hours in the report's format, the totals and the venues.
func rollSampleProblems(rows []pages.PanelRollRow, week int, days []string, venues []string) []string {
	var out []string
	p := ledger.WeekOf(ledger.Date{Year: 2026, Month: time.October, Day: 14}, time.UTC)
	for p.From.Year() == 2026 {
		if _, w := p.From.ISOWeek(); w == week {
			break
		}
		next := p.From.AddDate(0, 0, 7)
		p = ledger.WeekOf(ledger.Date{Year: next.Year(), Month: next.Month(), Day: next.Day()}, time.UTC)
	}
	if _, w := p.From.ISOWeek(); w != week {
		return append(out, fmt.Sprintf("the drawing names week %d, which is not a week of 2026 from October", week))
	}
	if len(days) != ledger.ReportDays {
		out = append(out, fmt.Sprintf("The Roll's drawing has %d day heading(s); a report is %d days", len(days), ledger.ReportDays))
	}
	for i, h := range days {
		if want := p.From.AddDate(0, 0, i).Format("Mon 2"); h != want {
			out = append(out, fmt.Sprintf("day heading %d is %q; week %d's is %q", i, h, week, want))
		}
	}
	for _, r := range rows {
		if !slices.Contains(venues, r.HomeVenue) {
			out = append(out, fmt.Sprintf("%s's home venue %q is not the seed's", r.Who, r.HomeVenue))
		}
		sum := 0
		for i, d := range r.Days {
			switch {
			case d == pages.RollOpen:
				if d.Cell() != "open" {
					out = append(out, r.Who+"'s open day does not say open")
				}
				if r.Away[i] {
					out = append(out, r.Who+"'s open day is marked as worked at another venue")
				}
			case d > pages.RollNone:
				sum += int(d)
				if want := hoursMinutes(time.Duration(d) * time.Minute); d.Cell() != want {
					out = append(out, fmt.Sprintf("%s's day %d prints %q; The Roll prints %q", r.Who, i, d.Cell(), want))
				}
			case d == pages.RollNone:
				if r.Away[i] {
					out = append(out, r.Who+"'s day with no hours is marked as worked at another venue")
				}
			default:
				out = append(out, fmt.Sprintf("%s's day %d holds %d, which is neither minutes nor a marker", r.Who, i, d))
			}
		}
		if want := hoursMinutes(time.Duration(sum) * time.Minute); r.Total() != want {
			out = append(out, fmt.Sprintf("%s's total prints %q; the counted days add up to %q (an open day is not counted)", r.Who, r.Total(), want))
		}
	}
	return out
}

// TestLandingPanel_TheSampleIsWhatTheProductWouldDraw: every sample record on the three
// drawings is stamped, scored and signed the way the engine decides its evidence and the
// docket prints it; The Spike's reasons and tiles are The Spike's own; The Roll's hours
// are in the report's format and its totals add up; every venue is the seed's.
func TestLandingPanel_TheSampleIsWhatTheProductWouldDraw(t *testing.T) {
	t.Parallel()
	venues := seedVenues(t)
	for _, p := range panelSampleProblems(pages.LandingPanelPassDockets, pages.LandingPanelSpikeQueue,
		pages.LandingPanelSpikeFocus, pages.LandingPanelPassDay, venues) {
		t.Error(p)
	}
	for _, p := range rollSampleProblems(pages.LandingPanelRollRows, pages.LandingPanelRollWeek, pages.LandingPanelRollDays, venues) {
		t.Error(p)
	}
	// The Pass's day is a real date of the week after The Roll's, said the panel's way.
	if d, err := time.Parse("Mon 2 Jan 2006", pages.LandingPanelPassDay+" 2026"); err != nil || d.Format("Mon 2 Jan") != pages.LandingPanelPassDay {
		t.Errorf("The Pass's day %q is not a date of 2026 as the panel prints one (%v)", pages.LandingPanelPassDay, err)
	}
}

// TestLandingPanel_SampleCheckRefusesADrawingTheProductWouldNotDraw is the control: each
// mutation of the sample is one the product would not draw, and each is reported.
func TestLandingPanel_SampleCheckRefusesADrawingTheProductWouldNotDraw(t *testing.T) {
	t.Parallel()
	venues := seedVenues(t)
	pass := pages.LandingPanelPassDockets
	queue := pages.LandingPanelSpikeQueue
	focus := pages.LandingPanelSpikeFocus
	day := pages.LandingPanelPassDay
	if got := panelSampleProblems(pass, queue, focus, day, venues); len(got) != 0 {
		t.Fatalf("PREMISE: the shipped sample already draws problems: %q", got)
	}
	clonePass := func(i int, f func(*pages.PanelDocket)) []pages.PanelDocket {
		out := slices.Clone(pass)
		f(&out[i])
		return out
	}
	cloneQueue := func(i int, f func(*pages.PanelQueueItem)) []pages.PanelQueueItem {
		out := slices.Clone(queue)
		f(&out[i])
		return out
	}
	for _, tc := range []struct {
		name  string
		pass  []pages.PanelDocket
		queue []pages.PanelQueueItem
		focus pages.PanelFocus
	}{
		{"a score §5 does not give that evidence", clonePass(0, func(d *pages.PanelDocket) { d.Trust = "50" }), queue, focus},
		{"a record with no evidence stamped APPROVED", clonePass(2, func(d *pages.PanelDocket) { d.Verdict = "ok" }), queue, focus},
		{"a GPS-only record stamped FLAGGED", clonePass(1, func(d *pages.PanelDocket) { d.Verdict = "flag" }), queue, focus},
		{"a tap's sign printed n/a", clonePass(0, func(d *pages.PanelDocket) { d.GPS = "n/a" }), queue, focus},
		{"a venue the seed does not have", clonePass(0, func(d *pages.PanelDocket) { d.Venue = "KM Facility" }), queue, focus},
		{"a rejected record on the rail", clonePass(0, func(d *pages.PanelDocket) { d.Verdict = "reject" }), queue, focus},
		{"the rail out of order", []pages.PanelDocket{pass[1], pass[0], pass[2], pass[3]}, queue, focus},
		{"a queue reason The Spike does not print", pass, cloneQueue(1, func(q *pages.PanelQueueItem) { q.Why = "suspicious location" }), focus},
		{"a rule that did not decide the record", pass, cloneQueue(0, func(q *pages.PanelQueueItem) { q.Rule = "base:qr-requires-ip"; q.Why = "QR without network" }), focus},
		{"a QR scan with a network match on The Spike", pass, cloneQueue(1, func(q *pages.PanelQueueItem) { q.Docket.IP = "yes" }), focus},
		{"a focus tile The Spike would not draw", pass, queue, pages.PanelFocus{Label: focus.Label, Line: focus.Line,
			Tiles: []pages.PanelTile{focus.Tiles[0], focus.Tiles[1], {Label: "Location", Value: "yes", OK: true}}}},
		{"a focus on another day", pass, queue, pages.PanelFocus{Label: focus.Label, Line: "IN · 19 Oct 09:41:12 · NFC", Tiles: focus.Tiles}},
		{"a focus plaque printed whole", pass, queue, pages.PanelFocus{Label: "KF Msida · plaque 04A1C33C2B7E10", Line: focus.Line, Tiles: focus.Tiles}},
	} {
		if got := panelSampleProblems(tc.pass, tc.queue, tc.focus, day, venues); len(got) == 0 {
			t.Errorf("CONTROL %s: the check reports nothing", tc.name)
		}
	}

	rows := pages.LandingPanelRollRows
	week, days := pages.LandingPanelRollWeek, pages.LandingPanelRollDays
	if got := rollSampleProblems(rows, week, days, venues); len(got) != 0 {
		t.Fatalf("PREMISE: the shipped Roll already draws problems: %q", got)
	}
	shifted := slices.Clone(days)
	shifted[0] = "Mon 19"
	away := slices.Clone(rows)
	away[1].Away[1] = true // Kevin's open Tuesday
	foreign := slices.Clone(rows)
	foreign[0].HomeVenue = "KF Sliema"
	for name, got := range map[string][]string{
		"a heading from another week": rollSampleProblems(rows, week, shifted, venues),
		"another week's number":       rollSampleProblems(rows, 43, days, venues),
		"six days":                    rollSampleProblems(rows, week, days[:6], venues),
		"an open day drawn as away":   rollSampleProblems(away, week, days, venues),
		"a home venue not the seed's": rollSampleProblems(foreign, week, days, venues),
	} {
		if len(got) == 0 {
			t.Errorf("CONTROL %s: the check reports nothing", name)
		}
	}
}

// panelSectionRE cuts the panel section out of the rendered landing page.
var panelSectionRE = regexp.MustCompile(`(?s)<section id="panel".*?</section>`)

// liveWordRE is "live" as a word: the drawings are samples, and the panel shows what was
// recorded, so nothing on them is "live" (the FAQ's banned "live headcount", "live
// dashboard" are the shape this refuses).
var liveWordRE = regexp.MustCompile(`(?i)\blive\b`)

// TestLandingPanel_DrawsThreeSamplePicturesAndNoControl: the section sits where the boards
// put it (after the proofs, before the two fits), draws each screen as one picture whose
// name says it is a sample, prints the label under them, stamps each docket with the
// product's stamp class and word, and carries no link, button or form -- "Take the product
// tour" arrives with /product (README O8).
func TestLandingPanel_DrawsThreeSamplePicturesAndNoControl(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	security, panel, fits := strings.Index(body, `id="security"`), strings.Index(body, `id="panel"`),
		strings.Index(body, "Built for both shapes of business")
	if security < 0 || panel < 0 || fits < 0 || !(security < panel && panel < fits) {
		t.Errorf("the panel section is at %d; the boards put it after the proofs (%d) and before the fits (%d)", panel, security, fits)
	}
	section := panelSectionRE.FindString(body)
	if section == "" {
		t.Fatal("the landing page has no <section id=\"panel\">")
	}
	for _, s := range []pages.PanelScreen{pages.LandingPanelPass, pages.LandingPanelSpike, pages.LandingPanelRoll} {
		if n := strings.Count(section, `role="img" aria-label="Sample screen: `+s.Title+`"`); n != 1 {
			t.Errorf("the section draws %d picture(s) named \"Sample screen: %s\"; want one", n, s.Title)
		}
	}
	if n := strings.Count(section, `role="img"`); n != 3 {
		t.Errorf("the section carries %d picture(s); the boards draw three", n)
	}
	text := screenText(t, "<body>"+section+"</body>")
	if !strings.Contains(text, asRendered(pages.LandingPanelSampleNote)) {
		t.Errorf("the section does not say its drawings are samples (%q)", pages.LandingPanelSampleNote)
	}
	if hit := liveWordRE.FindString(text); hit != "" {
		t.Errorf("the panel section says %q; its drawings are samples and the panel shows what was recorded", hit)
	}
	for _, control := range []string{"<a ", "<button", "<form", "<input", "href=", "tabindex"} {
		if strings.Contains(section, control) {
			t.Errorf("the panel section carries %q; its drawings are pictures and its link arrives with /product", control)
		}
	}
	ok, flag := 0, 1 // the record in focus is flagged
	for _, d := range pages.LandingPanelPassDockets {
		if d.Verdict == "ok" {
			ok++
		} else {
			flag++
		}
	}
	if n := strings.Count(section, `<span class="stamp stamp--approved lp-stamp-sm lp-panelStamp">APPROVED</span>`); n != ok {
		t.Errorf("the section draws %d APPROVED stamp(s) in the product's class; the sample has %d", n, ok)
	}
	if n := strings.Count(section, `<span class="stamp stamp--flagged lp-stamp-sm lp-panelStamp">FLAGGED</span>`); n != flag {
		t.Errorf("the section draws %d FLAGGED stamp(s) in the product's class; the sample has %d", n, flag)
	}
	// The rail in its order, and the queue's count in The Spike's own words.
	var whos []string
	for _, d := range pages.LandingPanelPassDockets {
		whos = append(whos, d.Who)
	}
	renderedInOrder(t, text, "The Pass's rail", whos)
	if want := fmt.Sprintf("Waiting for review: %d", len(pages.LandingPanelSpikeQueue)); !strings.Contains(text, want) {
		t.Errorf("The Spike's drawing does not say %q", want)
	}
}
