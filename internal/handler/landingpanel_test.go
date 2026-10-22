package handler

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
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
	// Later is every migration after 00005, by file name: one of them could change the
	// outcome constraint 00005 declares.
	Later   map[string]string
	Review  string // web/templates/pages/review.templ
	Report  string // internal/domain/ledger/report.go
	Reports string // web/templates/pages/reports.templ
}

func loadPanelFactSources(t *testing.T) panelFactSources {
	t.Helper()
	return panelFactSources{
		Queries:      repoFile(t, "db", "queries", "transactions.sql"),
		Ledger:       repoFile(t, "internal", "domain", "ledger", "ledger.go"),
		Transactions: repoFile(t, "web", "templates", "pages", "transactions.templ"),
		Docket:       repoFile(t, "web", "templates", "components", "docket.templ"),
		Mig05:        repoFile(t, "db", "migrations", "00005_create_transactions_audit_reviews.sql"),
		Later:        migrationsAfter(t, "00005_create_transactions_audit_reviews.sql"),
		Review:       repoFile(t, "web", "templates", "pages", "review.templ"),
		Report:       repoFile(t, "internal", "domain", "ledger", "report.go"),
		Reports:      repoFile(t, "web", "templates", "pages", "reports.templ"),
	}
}

// migrationsAfter reads every migration whose file name sorts after name.
func migrationsAfter(t *testing.T, name string) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "..", "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") || e.Name() <= name {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(b)
	}
	if len(out) < 50 {
		t.Fatalf("PREMISE: read %d migration(s) after %s; the directory holds more", len(out), name)
	}
	return out
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

var (
	reviewOutcomeCheckRE = regexp.MustCompile(`outcome\s+text\s+NOT NULL\s+CHECK\s*\(\s*outcome\s+IN\s*\(\s*'approved'\s*,\s*'rejected'\s*\)\s*\)`)
	// alterReviewsRE is an ALTER TABLE of transaction_reviews, one statement at a time.
	alterReviewsRE = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(?:public\.)?transaction_reviews\b`)
	// touchesOutcomeRE is what such a statement would carry to change the closed set.
	touchesOutcomeRE = regexp.MustCompile(`(?i)\bCHECK\b|\bCONSTRAINT\b|\boutcome\b`)
	sqlLineCommentRE = regexp.MustCompile(`--[^\n]*`)
	outcomeButtonRE  = regexp.MustCompile(`name="outcome"\s+value="([^"]*)"`)
)

// reviewOutcomeViolation is FactReviewIsApproveOrReject: a decision is one of exactly two
// outcomes -- migration 0005 declares the closed set and no later migration's Up alters
// transaction_reviews' constraints or its outcome column -- and The Spike's form offers
// exactly those two, no third.
func reviewOutcomeViolation(s panelFactSources) string {
	at := strings.Index(s.Mig05, "CREATE TABLE transaction_reviews")
	if at < 0 || !reviewOutcomeCheckRE.MatchString(s.Mig05[at:]) {
		return "migration 0005's transaction_reviews.outcome is no longer the closed set approved|rejected"
	}
	names := make([]string, 0, len(s.Later))
	for name := range s.Later {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, stmt := range strings.Split(sqlLineCommentRE.ReplaceAllString(gooseUp(s.Later[name]), ""), ";") {
			if alterReviewsRE.MatchString(stmt) && touchesOutcomeRE.MatchString(stmt) {
				return "migration " + name + " alters transaction_reviews' constraints or outcome, so 0005's closed set may no longer hold"
			}
		}
	}
	var sent []string
	for _, m := range outcomeButtonRE.FindAllStringSubmatch(s.Review, -1) {
		sent = append(sent, m[1])
	}
	slices.Sort(sent)
	if !slices.Equal(sent, []string{"approved", "rejected"}) {
		return fmt.Sprintf("pages/review.templ's decision form sends outcome %q, not exactly approved and rejected", sent)
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
	// The day cells must be INSIDE the per-person loop: the header row ranges over the day
	// headings too, so finding the words anywhere in the component would prove nothing.
	body := templBody(s.Reports, "reportPeople")
	at := strings.Index(body, "range v.People")
	if at < 0 {
		return "pages/reports.templ's reportPeople no longer prints a row per person"
	}
	if !strings.Contains(body[at:], "range v.DayHeadings") {
		return "pages/reports.templ's reportPeople no longer prints a cell per day in each person's row"
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
	// THE DIRECTION IS AN INPUT HERE, NOT A FINDING. The drawing shows no earlier record
	// of anybody who checks out (Luke Borg's IN was the evening before, off the rail), so
	// an OUT is given its open check-in from the drawing's own word. What the check below
	// then proves is narrow: the engine answers "out" by §5's toggle against an open
	// entry (not by the clock), and a word other than IN or OUT is refused. It does not
	// prove that a drawn OUT had an IN before it.
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

// seedPersonRE reads one employee row of test/fixtures/seed.sql: the name, then the role,
// the e-mail (or NULL) and the status.
var seedPersonRE = regexp.MustCompile(`(?m)^\s*'((?:[^']|'')+)',\s+'[^']*',\s+(?:'[^']*'(?:::citext)?|NULL),\s+'(?:active|invited|deactivated)'\)`)

// seedPeople is every employee name the seed carries.
func seedPeople(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range seedPersonRE.FindAllStringSubmatch(repoFile(t, "test", "fixtures", "seed.sql"), -1) {
		out = append(out, strings.ReplaceAll(m[1], "''", "'"))
	}
	// The seed is 25 KF people plus the KM crew (skill tappa-seed). Fewer means the
	// pattern stopped reading the file, and every sample name would look invented.
	if len(out) < 25 || !slices.Contains(out, "Grace Attard") {
		t.Fatalf("PREMISE: read %d employee(s) from seed.sql; the pattern lost the file", len(out))
	}
	return out
}

// samplePeopleProblems is every sample person who is one of the seed's people. The seed's
// people have a status and a role of their own (Grace Attard is invited and has never
// tapped), and a drawing that showed one of them with hours or a flagged tap would
// contradict the demo data the page names; the sample's people are invented instead.
func samplePeopleProblems(names, seed []string) []string {
	var out []string
	for _, n := range names {
		if slices.Contains(seed, n) {
			out = append(out, fmt.Sprintf("the sample draws %q, who is a person in the seed; sample people are invented", n))
		}
	}
	return out
}

// panelSamplePeople is every person the three drawings name.
func panelSamplePeople() []string {
	var out []string
	for _, d := range pages.LandingPanelPassDockets {
		out = append(out, d.Who)
	}
	for _, q := range pages.LandingPanelSpikeQueue {
		out = append(out, q.Docket.Who)
	}
	for _, r := range pages.LandingPanelRollRows {
		out = append(out, r.Who)
	}
	return out
}

// stampWord is the word the product's stamp prints for a verdict (components.VerdictStamp).
func stampWord(verdict string) string {
	switch verdict {
	case "ok":
		return "APPROVED"
	case "flag":
		return "FLAGGED"
	case "reject":
		return "REJECTED"
	case "ignored":
		return "IGNORED"
	}
	return "?" + verdict
}

// panelDrawingWant is what each drawn record must read as on the page, as screenText
// flattens it, one string per docket, queue item, record in focus and Roll row, in the
// page's order. Each is written out from the sample data, field by field, so a template
// that put a value in another value's place (IP where GPS goes) reads differently.
func panelDrawingWant() []string {
	var out []string
	for _, d := range pages.LandingPanelPassDockets {
		out = append(out, strings.Join([]string{d.Venue, stampWord(d.Verdict), d.Who, d.Direction, d.At,
			"Trust " + d.Trust, "IP " + d.IP + " · GPS " + d.GPS}, " "))
	}
	for _, q := range pages.LandingPanelSpikeQueue {
		out = append(out, strings.Join([]string{q.Docket.Who, q.When, q.Docket.Venue + " · " + q.Docket.Direction + " · " + q.Why}, " "))
	}
	f, first := pages.LandingPanelSpikeFocus, pages.LandingPanelSpikeQueue[0].Docket
	focus := []string{f.Label, first.Who, f.Line, stampWord(first.Verdict)}
	for _, tile := range f.Tiles {
		mark := "✗"
		if tile.OK {
			mark = "✓"
		}
		focus = append(focus, tile.Label, mark, tile.Value)
	}
	out = append(out, strings.Join(append(focus, "Approve", "Reject"), " "))
	for _, r := range pages.LandingPanelRollRows {
		row := []string{r.Who, r.Sub()}
		for _, d := range r.Days {
			row = append(row, d.Cell())
		}
		out = append(out, strings.Join(append(row, r.Shifts(), r.Total()), " "))
	}
	return out
}

// drawingMissing is every wanted string text does not carry whole.
func drawingMissing(text string, want []string) []string {
	var out []string
	for _, w := range want {
		if !strings.Contains(text, asRendered(w)) {
			out = append(out, w)
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
	for _, p := range samplePeopleProblems(panelSamplePeople(), seedPeople(t)) {
		t.Error(p)
	}
	// The Pass's day is a real date of 2026, said the panel's way, in the ISO week after
	// The Roll's.
	if d, err := time.Parse("Mon 2 Jan 2006", pages.LandingPanelPassDay+" 2026"); err != nil || d.Format("Mon 2 Jan") != pages.LandingPanelPassDay {
		t.Errorf("The Pass's day %q is not a date of 2026 as the panel prints one (%v)", pages.LandingPanelPassDay, err)
	} else if _, w := d.ISOWeek(); w != pages.LandingPanelRollWeek+1 {
		t.Errorf("The Pass's day %q is in ISO week %d; The Roll shows week %d, the week before", pages.LandingPanelPassDay, w, pages.LandingPanelRollWeek)
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
		{"a direction that is neither IN nor OUT", clonePass(0, func(d *pages.PanelDocket) { d.Direction = "BREAK" }), queue, focus},
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
		"a seed person in the sample": samplePeopleProblems([]string{"Maria Vella", "Grace Attard"}, seedPeople(t)),
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
	// What each drawn record reads as, whole and in the page's order: the docket's venue,
	// stamp, person, direction and time, score and the two signs in their own places; The
	// Spike's queue lines, the record in focus with its tiles' marks and words; each Roll
	// row's cells, shift count and total. The data is held to the engine above; this holds
	// the picture to the data.
	want := panelDrawingWant()
	renderedInOrder(t, text, "the panel's drawings", want)
	// CONTROL: the auditor's mutation -- the first docket's IP and GPS signs in each
	// other's places -- is a drawing this check refuses.
	first := pages.LandingPanelPassDockets[0]
	foot := "IP " + first.IP + " · GPS " + first.GPS
	swapped := strings.Replace(text, foot, "IP "+first.GPS+" · GPS "+first.IP, 1)
	if first.IP == first.GPS || swapped == text {
		t.Fatalf("PREMISE: the first docket's signs (%q) cannot be swapped into a different drawing", foot)
	}
	if got := drawingMissing(swapped, want); len(got) == 0 {
		t.Error("CONTROL: a drawing with the first docket's IP and GPS swapped reads as the sample")
	}
	if want := fmt.Sprintf("Waiting for review: %d", len(pages.LandingPanelSpikeQueue)); !strings.Contains(text, want) {
		t.Errorf("The Spike's drawing does not say %q", want)
	}
}
