package handler

import (
	"bytes"
	"context"
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

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/webhook"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PRODUCT TOUR (LP-08): /product joins the public site the way /pricing and /security
// did (publicpages_test.go). What is held here, once:
//
//   - THE FIFTEEN FACTS ONLY THIS PAGE CLAIMS, each derived from the code that keeps it
//     (productFactDerivations, appended to pageFactDerivations), each with its negative
//     control (TestProductFacts_SayNoWhenTheFactIsAbsent).
//   - THE COPY: every Line of pages.ProductText, at any depth, collected by reflection
//     (productLines), so publicPagesCopy checks each for a pin and for being rendered on
//     /product.
//   - THE DRAWINGS: sample data, and still held to the product -- the tap phones' record
//     through tap.Decide and pages.Result, the people against the seed and the panel's own
//     words, the venues and their hours against the seed, the rules against the engine and
//     the Rules screen, the integration names against the OpenAPI document and the panel.
//   - THE INDEX: every "On this page" entry lands on its section.
//   - WHAT THE PAGE MUST NOT SAY: "guarantee" in any form, anywhere in the served bytes
//     (README O1), and the board's sentences this card withdrew (productview.go lists why).

// --- the facts' sources ---------------------------------------------------------------

// productFactSources is what the product tour's facts read, as text, so a negative control
// can take one piece away.
type productFactSources struct {
	Result        string // web/templates/pages/result.templ
	TapHandler    string // internal/handler/checkin.go
	Tap           string // web/templates/pages/tap.templ
	Transactions  string // web/templates/pages/transactions.templ
	TxHandler     string // internal/handler/transactions.go
	Docket        string // web/templates/components/docket.templ
	Decide        string // internal/domain/tap/decide.go
	Review        string // web/templates/pages/review.templ
	ReviewHandler string // internal/handler/review.go
	Mig05         string // db/migrations/00005_create_transactions_audit_reviews.sql
	Reports       string // web/templates/pages/reports.templ
	TxQueries     string // db/queries/transactions.sql
	Roster        string // web/templates/components/roster.templ
	Employees     string // web/templates/pages/employees.templ
	EmployeesGo   string // internal/handler/employees.go
	InviteEmail   string // internal/handler/inviteemail.go
	Mig03         string // db/migrations/00003_create_employees_sessions.sql
	Config        string // internal/config/config.go
	InviteManager string // internal/invite/manager.go
	Mig12         string // db/migrations/00012_add_cancelled_at_to_employee_invites.sql
	Mig02         string // db/migrations/00002_create_locations_departments.sql
	VenueDetail   string // web/templates/components/venuedetail.templ
	PolicyActions string // internal/handler/policyactions.go
	RuleWriter    string // internal/domain/tenant/rulewriter.go
	Dashboard     string // internal/handler/dashboard.go
	PoliciesGo    string // internal/handler/policies.go
	Policies      string // web/templates/pages/policies.templ
	Mig48         string // db/migrations/00048_create_the_punch_feed_tables.sql
	Endpoints     string // internal/webhook/endpoints/endpoints.go

	Guards []policy.Guardrail
	// Anchors is what each Anchor derivation the facts lean on answers today ("" holds).
	Anchors map[pages.Anchor]string
	// Migs is every migration by file name: a fact read off one migration also reads
	// every later one's Up, which could change what that migration declared.
	Migs map[string]string
	// TapRenders is the tap screen as it is served for each of the three directions (no
	// prediction, Clock in, Clock out), and ResultRenders the result screen for each
	// verdict: a control a sub-template adds is in the render, not in the templates a
	// source scan names.
	TapRenders    map[pages.TapDirection]string
	ResultRenders map[string]string
}

// tapDirections is every direction the tap screen is drawn with (ADR 0032 §B.1).
var tapDirections = []pages.TapDirection{pages.TapUndirected, pages.TapClockIn, pages.TapClockOut}

// renderTapAndResults renders the tap screen for every direction and the result screen
// for every verdict.
func renderTapAndResults(t *testing.T) (map[pages.TapDirection]string, map[string]string) {
	t.Helper()
	var b bytes.Buffer
	taps := map[pages.TapDirection]string{}
	for _, d := range tapDirections {
		b.Reset()
		view := pages.TapView{EmployeeName: "Sample Person", LocationName: "Sample Venue", TapContext: "ctx", Direction: d}
		if err := pages.Tap(view, layout.Brand{}).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		taps[d] = b.String()
	}
	results := map[string]string{}
	for _, v := range []string{"ok", "flag", "reject", "ignored"} {
		b.Reset()
		view := pages.ResultView{Verdict: v, Venue: "Sample Venue", At: "09:41:00", Trust: tap.TrustBase}
		if v == "ok" || v == "flag" {
			view.Direction = "in"
		}
		if err := pages.Result(view, layout.Logo{}).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		results[v] = b.String()
	}
	return taps, results
}

// allMigrations is every migration, by file name.
func allMigrations(t *testing.T) map[string]string {
	t.Helper()
	out := migrationsAfter(t, "00000")
	if _, ok := out["00002_create_locations_departments.sql"]; !ok {
		t.Fatal("PREMISE: the migrations read do not include 00002")
	}
	return out
}

// laterAlterProblem names the first migration after `after` whose Up (comments removed)
// has an ALTER TABLE of table that touches what the fact reads (touches), or drops the
// table, or names one of the constraints in names anywhere. The LP-05/LP-06 shape
// (reviewOutcomeViolation, planWriteProblem): a closed set or a constraint declared by
// one migration holds only while no later one changes it.
//
// ITS LIMITS, STATED: a quoted table name, a change inside a DO $$ … $$ block or through
// dynamic SQL passes unseen; a person still reads a migration that touches the table.
func laterAlterProblem(migs map[string]string, after, table string, touches *regexp.Regexp, names ...string) string {
	alter := regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(?:public\.)?` + regexp.QuoteMeta(table) + `\b`)
	drop := regexp.MustCompile(`(?is)^\s*DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:public\.)?` + regexp.QuoteMeta(table) + `\b`)
	keys := make([]string, 0, len(migs))
	for name := range migs {
		if name > after {
			keys = append(keys, name)
		}
	}
	slices.Sort(keys)
	for _, name := range keys {
		up := sqlLineCommentRE.ReplaceAllString(gooseUp(migs[name]), "")
		for _, stmt := range strings.Split(up, ";") {
			if drop.MatchString(stmt) {
				return "migration " + name + " drops " + table
			}
			if alter.MatchString(stmt) && touches.MatchString(stmt) {
				return "migration " + name + " alters " + table + " where this fact reads it"
			}
			for _, n := range names {
				if strings.Contains(stmt, n) {
					return "migration " + name + " names " + n
				}
			}
		}
	}
	return ""
}

func loadProductFactSources(t *testing.T) productFactSources {
	t.Helper()
	strip := func(s string) string { return goLineCommentRE.ReplaceAllString(s, "") }
	anchors := anchorDerivations(t)
	answers := map[pages.Anchor]string{}
	for _, a := range []pages.Anchor{pages.AnchorLatenessFollowsDepartment, pages.AnchorVenueShiftAndAddress,
		pages.AnchorPlaqueBelongsToVenue} {
		derive, ok := anchors[a]
		if !ok {
			t.Fatalf("PREMISE: anchorDerivations derives no %q", a)
		}
		answers[a] = derive()
	}
	taps, results := renderTapAndResults(t)
	return productFactSources{
		Result:        repoFile(t, "web", "templates", "pages", "result.templ"),
		TapHandler:    strip(repoFile(t, "internal", "handler", "checkin.go")),
		Tap:           repoFile(t, "web", "templates", "pages", "tap.templ"),
		Transactions:  repoFile(t, "web", "templates", "pages", "transactions.templ"),
		TxHandler:     strip(repoFile(t, "internal", "handler", "transactions.go")),
		Docket:        repoFile(t, "web", "templates", "components", "docket.templ"),
		Decide:        strip(repoFile(t, "internal", "domain", "tap", "decide.go")),
		Review:        repoFile(t, "web", "templates", "pages", "review.templ"),
		ReviewHandler: strip(repoFile(t, "internal", "handler", "review.go")),
		Mig05:         repoFile(t, "db", "migrations", "00005_create_transactions_audit_reviews.sql"),
		Reports:       repoFile(t, "web", "templates", "pages", "reports.templ"),
		TxQueries:     repoFile(t, "db", "queries", "transactions.sql"),
		Roster:        repoFile(t, "web", "templates", "components", "roster.templ"),
		Employees:     repoFile(t, "web", "templates", "pages", "employees.templ"),
		EmployeesGo:   strip(repoFile(t, "internal", "handler", "employees.go")),
		InviteEmail:   strip(repoFile(t, "internal", "handler", "inviteemail.go")),
		Mig03:         repoFile(t, "db", "migrations", "00003_create_employees_sessions.sql"),
		Config:        strip(repoFile(t, "internal", "config", "config.go")),
		InviteManager: strip(repoFile(t, "internal", "invite", "manager.go")),
		Mig12:         repoFile(t, "db", "migrations", "00012_add_cancelled_at_to_employee_invites.sql"),
		Mig02:         repoFile(t, "db", "migrations", "00002_create_locations_departments.sql"),
		VenueDetail:   repoFile(t, "web", "templates", "components", "venuedetail.templ"),
		PolicyActions: strip(repoFile(t, "internal", "handler", "policyactions.go")),
		RuleWriter:    strip(repoFile(t, "internal", "domain", "tenant", "rulewriter.go")),
		Dashboard:     strip(repoFile(t, "internal", "handler", "dashboard.go")),
		PoliciesGo:    strip(repoFile(t, "internal", "handler", "policies.go")),
		Policies:      repoFile(t, "web", "templates", "pages", "policies.templ"),
		Mig48:         repoFile(t, "db", "migrations", "00048_create_the_punch_feed_tables.sql"),
		Endpoints:     strip(repoFile(t, "internal", "webhook", "endpoints", "endpoints.go")),
		Guards:        policy.Guardrails(policy.DefaultParams()),
		Migs:          allMigrations(t),
		TapRenders:    taps,
		ResultRenders: results,
		Anchors:       answers,
	}
}

// noControl says which of a, button, form or input the templ source carries, or "".
func noControl(src string) string {
	for _, c := range []string{"<a ", "<button", "<form", "<input", "<nav"} {
		if strings.Contains(src, c) {
			return c
		}
	}
	return ""
}

// --- the derivations -----------------------------------------------------------------

// recordedDocketViolation is FactRecordedTapEndsOnAStampedDocket: the result screen is a
// docket with the verdict's stamp, the stamp prints the four words, and the tap handler
// renders it for a recorded tap.
func recordedDocketViolation(s productFactSources) string {
	body := templBody(s.Result, "Result")
	if !strings.Contains(body, `<section class="docket">`) || !strings.Contains(body, "@stamp(v)") {
		return "pages/result.templ's Result is no longer a docket carrying the verdict's stamp"
	}
	stamp := templBody(s.Result, "stamp")
	for _, w := range [][2]string{{"approved", "APPROVED"}, {"flagged", "FLAGGED"}, {"rejected", "REJECTED"}, {"ignored", "IGNORED"}} {
		if !strings.Contains(stamp, `stamp--`+w[0]+`">`+w[1]+`<`) {
			return "the result screen's stamp no longer prints " + w[1]
		}
	}
	if !strings.Contains(s.TapHandler, "pages.Result(resultView(res)") {
		return "the tap handler no longer renders pages.Result for a recorded tap"
	}
	return ""
}

// resultNoButtonViolation is FactResultScreenEndsWithoutAButton: a counted tap's closing is
// "All done — you can close this page.", and the result screen holds no control.
func resultNoButtonViolation(s productFactSources) string {
	closing := withoutTemplComments(templBody(s.Result, "closing"))
	ok, done, flag := strings.Index(closing, `case "ok":`), strings.Index(closing, "All done — you can close this page."),
		strings.Index(closing, `case "flag":`)
	if ok < 0 || done < 0 || flag < 0 || !(ok < done && done < flag) {
		return "the result screen's counted tap no longer ends on \"All done — you can close this page.\""
	}
	for _, name := range []string{"Result", "closing", "stamp", "brandMessage"} {
		if c := noControl(withoutTemplComments(templBody(s.Result, name))); c != "" {
			return "pages/result.templ's " + name + " now carries " + strconv.Quote(c)
		}
	}
	// The served screen, every verdict: a control a sub-template adds is here.
	if len(s.ResultRenders) != 4 {
		return fmt.Sprintf("the result screen was rendered for %d verdict(s), not the four", len(s.ResultRenders))
	}
	for _, v := range slices.Sorted(maps.Keys(s.ResultRenders)) {
		if c := renderedControl(s.ResultRenders[v]); c != "" {
			return "the result screen for " + v + " renders " + strconv.Quote(c)
		}
	}
	return ""
}

// renderedControlRE is a control in served HTML.
var renderedControlRE = regexp.MustCompile(`(?i)<(a|button|form|input|select|textarea|nav|summary)\b[^>]*>`)

// renderedControl is the first control in html, or "".
func renderedControl(html string) string { return renderedControlRE.FindString(html) }

// tapCorrectionCallRE is tapForm's one call of the correction, and the condition it sits
// under: the "Not right? …" button is drawn only when the predicted direction has an
// other -- under "Clock in" or "Clock out", never under "Tap" (ADR 0032 §B.1).
var tapCorrectionCallRE = regexp.MustCompile(`if d\.Other\(\)\.Want\(\) != "" \{\s*@tapCorrection\(d\.Other\(\)\)\s*\}`)

// buttonTagRE is a <button …> start tag; tapButtonRE a whole button with its words.
// (classAttrRE, which reads a tag's classes, is landingcss_test.go's.)
var (
	buttonTagRE = regexp.MustCompile(`<button\b[^>]*>`)
	tapButtonRE = regexp.MustCompile(`(<button\b[^>]*>)([^<]*)</button>`)
)

// correctionAccentProblem says how a correction button's start tag could take the tenant
// accent, or "". The accent reaches a page through three variables, and the compiled CSS
// reads them only in .tap-button and in the Tailwind tokens named for the brand (bg-brand,
// text-on-brand, the brand-edge shadow) -- TestCompiledCSS_BrandVariablesOnlyInTheirSlots
// holds that. So a button whose classes are neither tap-button nor brand- or accent-named,
// and that carries no style of its own, cannot draw in the accent (ADR 0023 §2).
func correctionAccentProblem(tag string) string {
	if strings.Contains(tag, "style=") {
		return "the correction carries a style of its own: " + tag
	}
	m := classAttrRE.FindStringSubmatch(tag)
	if m == nil {
		return "the correction has no class to read: " + tag
	}
	for _, c := range strings.Fields(m[1]) {
		if c == "tap-button" || strings.Contains(c, "brand") || strings.Contains(c, "accent") {
			return "the correction's class " + strconv.Quote(c) + " can draw in the tenant accent (ADR 0023 §2: the accent fills the primary button only)"
		}
	}
	return ""
}

// tapScreenViolation is FactTapScreenIsOneButtonAndACorrection: the tap screen is its
// greeting and one form; the form holds one primary button face and, only under a
// predicted direction, the "Not right? …" correction after it -- both inside the form;
// the correction takes no accent; nothing on the screen is a link or a menu. Read from
// the templates and from the screen rendered for each of the three directions.
func tapScreenViolation(s productFactSources) string {
	page := withoutTemplComments(templBody(s.Tap, "Tap"))
	if !strings.Contains(page, "TapHeading(") || !strings.Contains(page, "tapForm(") || noControl(page) != "" {
		return "the tap screen is no longer the greeting and the one form"
	}
	if c := noControl(withoutTemplComments(templBody(s.Tap, "TapHeading"))); c != "" {
		return "the tap screen's greeting now carries " + strconv.Quote(c)
	}
	form := withoutTemplComments(templBody(s.Tap, "tapForm"))
	faces, fixes := strings.Count(form, "@TapButtonFace("), strings.Count(form, "@tapCorrection(")
	if faces != 1 || fixes != 1 || strings.Contains(form, "<a ") || strings.Contains(form, "<button") {
		return fmt.Sprintf("the tap form draws %d button face(s) and %d correction(s), or a link or button of its own", faces, fixes)
	}
	open, face, fix, end := strings.Index(form, "<form"), strings.Index(form, "@TapButtonFace("), strings.Index(form, "@tapCorrection("), strings.LastIndex(form, "</form>")
	if open < 0 || open >= face || face >= fix || fix >= end {
		return "the primary button and, after it, its correction are no longer inside the one form"
	}
	if !tapCorrectionCallRE.MatchString(form) {
		return "the correction is no longer drawn only under a predicted direction"
	}
	faceTags := buttonTagRE.FindAllString(withoutTemplComments(templBody(s.Tap, "TapButtonFace")), -1)
	if len(faceTags) == 0 {
		return "the tap button face draws no button"
	}
	for _, tag := range faceTags {
		if !strings.Contains(tag, `class="tap-button"`) {
			return "the tap button face draws a button that is not the primary one: " + tag
		}
	}
	fixTags := buttonTagRE.FindAllString(withoutTemplComments(templBody(s.Tap, "tapCorrection")), -1)
	if len(fixTags) != 1 {
		return fmt.Sprintf("the correction draws %d button(s), not one", len(fixTags))
	}
	if why := correctionAccentProblem(fixTags[0]); why != "" {
		return why
	}
	for _, d := range tapDirections {
		if why := tapRenderProblem(s.TapRenders[d], d); why != "" {
			return fmt.Sprintf("the tap screen drawn for %q: %s", d.Words(), why)
		}
	}
	return ""
}

// tapRenderProblem is the served screen for direction d: one form; every control a hidden
// field or a button inside it; one primary button, in d's words; and under a prediction
// exactly one more -- the correction after it, sending and saying the other direction,
// with no accent -- or under none, nothing more.
func tapRenderProblem(html string, d pages.TapDirection) string {
	open, end := strings.Index(html, "<form"), strings.LastIndex(html, "</form>")
	var forms, buttons int
	for _, at := range renderedControlRE.FindAllStringSubmatchIndex(html, -1) {
		tag, kind := html[at[0]:at[1]], strings.ToLower(html[at[2]:at[3]])
		switch kind {
		case "form":
			forms++
			continue
		case "button":
			buttons++
		case "input":
			if !strings.Contains(tag, `type="hidden"`) {
				return "it renders a visible field: " + tag
			}
		default:
			return "it renders " + strconv.Quote(tag)
		}
		if at[0] < open || at[0] > end {
			return "a control sits outside the form: " + tag
		}
	}
	if forms != 1 {
		return fmt.Sprintf("it renders %d form(s), not one", forms)
	}
	var primary, correction []string
	primaryAt, correctionAt := -1, -1
	for _, at := range tapButtonRE.FindAllStringSubmatchIndex(html, -1) {
		m := []string{html[at[0]:at[1]], html[at[2]:at[3]], html[at[4]:at[5]]}
		switch {
		case strings.Contains(m[1], `class="tap-button"`):
			primary, primaryAt = m, at[0]
		case strings.Contains(m[1], "data-tap-correction"):
			correction, correctionAt = m, at[0]
		}
	}
	if n := strings.Count(html, `class="tap-button"`); n != 1 || primary == nil {
		return fmt.Sprintf("it renders %d primary button(s), not one", n)
	}
	if primary[2] != d.Words() {
		return fmt.Sprintf("the primary button says %q, not %q", primary[2], d.Words())
	}
	other := d.Other()
	if other.Want() == "" {
		if buttons != 1 || correction != nil {
			return fmt.Sprintf("with no prediction it renders %d button(s); only the primary one may be there", buttons)
		}
		return ""
	}
	if buttons != 2 || correction == nil || strings.Count(html, "data-tap-correction") != 1 {
		return fmt.Sprintf("under a prediction it renders %d button(s); the primary one and one correction may be there", buttons)
	}
	if correctionAt < primaryAt {
		return "the correction comes before the primary button"
	}
	if want := "Not right? " + other.Words(); correction[2] != want {
		return fmt.Sprintf("the correction says %q, not %q", correction[2], want)
	}
	if !strings.Contains(correction[1], `type="submit"`) || !strings.Contains(correction[1], `name="want" value="`+other.Want()+`"`) {
		return "the correction does not submit the other direction: " + correction[1]
	}
	return correctionAccentProblem(correction[1])
}

// dateFilterRE is the records list's day, read from the query.
var dateFilterRE = regexp.MustCompile(`ledger\.ParseDate\(q\.Get\("date"\)\)`)

// earlierDayViolation is FactRecordsReadForAnyEarlierDay.
func earlierDayViolation(s productFactSources) string {
	step := templBody(s.Transactions, "dayStepper")
	if !strings.Contains(step, `aria-label="Previous day"`) || !strings.Contains(step, "v.PrevDayHref") {
		return "the records list no longer steps back a day (dayStepper's Previous day)"
	}
	if !dateFilterRE.MatchString(s.TxHandler) {
		return "the records list no longer reads its day from the query"
	}
	return ""
}

// decideCopiesTheReasonRE is tap.Decide putting the deciding rule's sentence on the record
// (its own line: EmployeeNote's assignment does not count).
var decideCopiesTheReasonRE = regexp.MustCompile(`(?m)^\s*Note:\s+pd\.Reason,`)

// spikeEvidenceViolation is FactSpikeShowsEvidenceAndTheRulesSentence.
func spikeEvidenceViolation(s productFactSources) string {
	body := templBody(s.Docket, "SpikeDocket")
	for _, want := range []string{"@evidenceTile(t)", "Why it is here", "f.Docket.Note"} {
		if !strings.Contains(body, want) {
			return "The Spike's docket no longer carries " + strconv.Quote(want)
		}
	}
	if !decideCopiesTheReasonRE.MatchString(s.Decide) {
		return "tap.Decide no longer copies the deciding rule's sentence onto the record"
	}
	return ""
}

// reviewNoteViolation is FactReviewCarriesANote (README §10 U3).
func reviewNoteViolation(s productFactSources) string {
	if !strings.Contains(s.Review, `name="note"`) {
		return "The Spike's decision form has no note field"
	}
	if !strings.Contains(s.ReviewHandler, `r.PostFormValue("note")`) {
		return "the review handler no longer reads the note"
	}
	if !hasColumn(sqlTableBlock(s.Mig05, "transaction_reviews"), "note", "text") {
		return "transaction_reviews no longer keeps a note"
	}
	if why := laterAlterProblem(s.Migs, "00005_create_transactions_audit_reviews.sql", "transaction_reviews", noteColumnRE); why != "" {
		return why
	}
	return ""
}

var (
	noteColumnRE   = regexp.MustCompile(`(?i)\bnote\b`)
	statusColumnRE = regexp.MustCompile(`(?i)\bstatus\b|employees_status_check`)
	gpsColumnRE    = regexp.MustCompile(`(?i)\bgps_(?:lat|lng)\b|locations_gps_pair`)
	endpointKeyRE  = regexp.MustCompile(`(?i)\bUNIQUE\b|\bPRIMARY\s+KEY\b|\btenant_id\b`)
	pillCaseRE     = regexp.MustCompile(`(?m)^\s*case\s+"`)
)

// openCheckInViolation is FactOpenCheckInIsListedNotCounted.
func openCheckInViolation(s productFactSources) string {
	for _, want := range []string{"Open check-ins, not counted", "The figure above is SHORT by whatever these add up to.",
		"templ reportOpen("} {
		if !strings.Contains(s.Reports, want) {
			return "The Roll no longer carries " + strconv.Quote(want)
		}
	}
	return ""
}

// recordVenueJoinRE is the worked-shift query's venue: the record's own location.
var recordVenueJoinRE = regexp.MustCompile(`LEFT JOIN locations\s+l ON l\.tenant_id = t\.tenant_id AND l\.id = t\.location_id`)

// latenessViolation is FactLatenessFollowsOwnShift.
func latenessViolation(s productFactSources) string {
	if why := s.Anchors[pages.AnchorLatenessFollowsDepartment]; why != "" {
		return why
	}
	if !recordVenueJoinRE.MatchString(queryBlock(s.TxQueries, "ListWorkedShiftEvents")) {
		return "the worked-shift query no longer takes the venue's shift from the record's own location"
	}
	if !strings.Contains(s.Reports, "p.Late") {
		return "The Roll no longer shows lateness"
	}
	return ""
}

// statusViolation is FactPeopleShowOneStatus.
func statusViolation(s productFactSources) string {
	pill := templBody(s.Roster, "lifecyclePill")
	for _, st := range []string{"invited", "active", "deactivated"} {
		if !strings.Contains(pill, `pill--`+st+` flex-none">`+pages.ProductStatusWord(st)+`<`) {
			return "People no longer prints " + pages.ProductStatusWord(st)
		}
	}
	if n := len(pillCaseRE.FindAllString(withoutTemplComments(pill), -1)); n != 3 {
		return fmt.Sprintf("People's status pill has %d case(s); the page names three statuses", n)
	}
	if !strings.Contains(s.Mig03, "CHECK (status IN ('invited', 'active', 'deactivated'))") {
		return "the employees table's status is no longer the three"
	}
	if why := laterAlterProblem(s.Migs, "00003_create_employees_sessions.sql", "employees", statusColumnRE, "employees_status_check"); why != "" {
		return why
	}
	return ""
}

var inviteEmailModeRE = regexp.MustCompile(`InviteDeliveryEmail\s*=\s*"email"`)

// inviteViolation is FactInviteLinkShownOnceOrMailed (README §10 U5).
func inviteViolation(s productFactSources) string {
	if !strings.Contains(templBody(s.Employees, "AdminInviteIssued"), "Activation link — shown once") {
		return "the screen that mints an invitation no longer shows its link once"
	}
	if !inviteEmailModeRE.MatchString(s.Config) || !strings.Contains(s.InviteEmail, "a.invites.IssueAndDeliver(") {
		return "the e-mail delivery mode is gone"
	}
	if !strings.Contains(s.InviteManager, "code.hash(m.hmacKey)") || !strings.Contains(s.Mig12, "WHERE i.code_hash = p_code_hash") {
		return "an invitation is no longer kept as a keyed hash"
	}
	return ""
}

var recordHrefRE = regexp.MustCompile(`RecordHref:\s+manualEntryHref \+ "\?id="`)

// personEntryViolation is FactManualEntryStartsFromThePerson.
func personEntryViolation(s productFactSources) string {
	if !recordHrefRE.MatchString(s.EmployeesGo) {
		return "the person's card no longer links to the manual entry for that person"
	}
	if !strings.Contains(s.Roster, `href={ templ.URL(v.RecordHref) }>Enter a record by hand</a>`) {
		return "the person's card no longer offers \"Enter a record by hand\""
	}
	return ""
}

// venueViolation is FactVenueHoldsItsEvidenceAndHours.
func venueViolation(s productFactSources) string {
	for _, a := range []pages.Anchor{pages.AnchorVenueShiftAndAddress, pages.AnchorPlaqueBelongsToVenue} {
		if why := s.Anchors[a]; why != "" {
			return why
		}
	}
	locations := sqlTableBlock(s.Mig02, "locations")
	if !hasColumn(locations, "gps_lat", "numeric") || !hasColumn(locations, "gps_lng", "numeric") {
		return "a venue no longer keeps a GPS point"
	}
	if why := laterAlterProblem(s.Migs, "00002_create_locations_departments.sql", "locations", gpsColumnRE); why != "" {
		return why
	}
	for _, label := range []string{">Venue network<", ">GPS point<", ">Shift<", ">Plaques at this venue<"} {
		if !strings.Contains(s.VenueDetail, label) {
			return "Venues & plaques no longer prints " + label
		}
	}
	return ""
}

// ruleChangeViolation is FactRuleChangeIsConfirmedThenRecorded.
func ruleChangeViolation(s productFactSources) string {
	if !registered(s.Dashboard, "Post", "policyChangeHref") || !registered(s.Dashboard, "Post", "policyApplyHref") {
		return "the rule change's review and apply routes are gone"
	}
	review := goFuncBody(s.PolicyActions, "func (a *AdminAuth) policyChangeReview(")
	if !strings.Contains(review, "a.setConfirmation(") || !strings.Contains(review, "pages.AdminPolicyChange(") {
		return "a rule change is no longer shown on a confirmation page before it is written"
	}
	apply := goFuncBody(s.PolicyActions, "func (a *AdminAuth) policyChangeApply(")
	check, write := strings.Index(apply, "a.confirmationRefusalFor("), strings.Index(apply, "a.scribe.")
	if check < 0 || write < 0 || check > write {
		return "the rule change's write no longer checks its confirmation first"
	}
	if strings.Count(apply, "Actor: actor") < 4 || !strings.Contains(apply, "Venues: c.venues") {
		return "the rule change no longer names its actor, or a rule of your own no longer takes its venues"
	}
	if !strings.Contains(s.RuleWriter, "ActorID:") || !strings.Contains(s.RuleWriter, `ActionPolicyDisabled = "policy.disabled"`) {
		return "the rule writer no longer records the change against its actor"
	}
	return ""
}

// registered says src registers method href (a name, not a literal).
func registered(src, method, href string) bool {
	return regexp.MustCompile(`r\.` + method + `\(\s*` + regexp.QuoteMeta(href) + `\s*,`).MatchString(src)
}

// ownerOnlyViolation is FactOnlyAnOwnerChangesARule.
func ownerOnlyViolation(s productFactSources) string {
	g := guardBySid(s.Guards, "sys:policy-edit-owner-only")
	if g == nil || g.Effect != policy.EffectDeny || g.Match == nil {
		return "sys:policy-edit-owner-only is no longer a guardrail that denies"
	}
	edit := func(role string) policy.Context {
		return policy.Context{Action: policy.ActionPolicyEdit, Keys: map[policy.ContextKey]any{policy.CtxActorRole: role}}
	}
	if !g.Match(edit("manager")) {
		return "sys:policy-edit-owner-only no longer refuses a manager's rule change"
	}
	if g.Match(edit("owner")) {
		return "sys:policy-edit-owner-only refuses the owner too, so \"only an owner\" is not what it does"
	}
	if !strings.Contains(s.PoliciesGo, "v.MayEdit = a.scribe.May(") {
		return "the Rules screen no longer asks the engine before it draws a control"
	}
	if !strings.Contains(s.Policies, "if v.MayEdit {") || !strings.Contains(s.Policies, "Only an organisation owner is ever granted it.") {
		return "the Rules screen no longer reads read-only for anybody but an owner"
	}
	return ""
}

var requireOpenRE = regexp.MustCompile(`if !open \{\s*return ErrAPIClosed\s*\}`)

// webhookEndpointViolation is FactWebhookIsOneEndpointBehindTheAPI.
func webhookEndpointViolation(s productFactSources) string {
	if !strings.Contains(s.Mig48, "CONSTRAINT webhook_endpoints_one_per_tenant UNIQUE (tenant_id)") {
		return "a business may now keep more than one webhook endpoint"
	}
	if why := laterAlterProblem(s.Migs, "00048_create_the_punch_feed_tables.sql", "webhook_endpoints", endpointKeyRE, "webhook_endpoints_one_per_tenant"); why != "" {
		return why
	}
	create := goFuncBody(s.Endpoints, "func (s *Service) Create(")
	if !strings.Contains(create, "requireOpen(ctx, q, c.TenantID)") || !requireOpenRE.MatchString(s.Endpoints) {
		return "a webhook endpoint can now be set up while the business's API is closed"
	}
	return ""
}

// productDerivationFuncs is each fact's derivation over the sources.
var productDerivationFuncs = map[pages.Fact]func(productFactSources) string{
	pages.FactRecordedTapEndsOnAStampedDocket:       recordedDocketViolation,
	pages.FactResultScreenEndsWithoutAButton:        resultNoButtonViolation,
	pages.FactTapScreenIsOneButtonAndACorrection:    tapScreenViolation,
	pages.FactRecordsReadForAnyEarlierDay:           earlierDayViolation,
	pages.FactSpikeShowsEvidenceAndTheRulesSentence: spikeEvidenceViolation,
	pages.FactReviewCarriesANote:                    reviewNoteViolation,
	pages.FactOpenCheckInIsListedNotCounted:         openCheckInViolation,
	pages.FactLatenessFollowsOwnShift:               latenessViolation,
	pages.FactPeopleShowOneStatus:                   statusViolation,
	pages.FactInviteLinkShownOnceOrMailed:           inviteViolation,
	pages.FactManualEntryStartsFromThePerson:        personEntryViolation,
	pages.FactVenueHoldsItsEvidenceAndHours:         venueViolation,
	pages.FactRuleChangeIsConfirmedThenRecorded:     ruleChangeViolation,
	pages.FactOnlyAnOwnerChangesARule:               ownerOnlyViolation,
	pages.FactWebhookIsOneEndpointBehindTheAPI:      webhookEndpointViolation,
}

// productFactDerivations is the product tour's derivation table (pageFactDerivations).
func productFactDerivations(t *testing.T) map[pages.Fact]func() string {
	t.Helper()
	s := loadProductFactSources(t)
	out := map[pages.Fact]func() string{}
	for fact, derive := range productDerivationFuncs {
		out[fact] = func() string { return derive(s) }
	}
	return out
}

// TestProductFacts_SayNoWhenTheFactIsAbsent is the negative control for every derivation
// above: each holds over the real sources, and each says no once one piece of what it
// reads is taken away or changed. Every fact of the table has at least one control.
func TestProductFacts_SayNoWhenTheFactIsAbsent(t *testing.T) {
	t.Parallel()
	real := loadProductFactSources(t)
	anchor := func(a pages.Anchor) func(*productFactSources) {
		return func(s *productFactSources) {
			m := maps.Clone(s.Anchors)
			m[a] = "taken away"
			s.Anchors = m
		}
	}
	replace := func(get func(*productFactSources) *string, old, new string) func(*productFactSources) {
		return func(s *productFactSources) {
			p := get(s)
			if !strings.Contains(*p, old) {
				t.Fatalf("PREMISE: %q is not in the source this mutation edits", old)
			}
			*p = strings.Replace(*p, old, new, 1)
		}
	}
	later := func(sql string) func(*productFactSources) {
		return func(s *productFactSources) {
			m := maps.Clone(s.Migs)
			m["00064_lp08_probe.sql"] = "-- +goose Up\n" + sql + "\n-- +goose Down\nSELECT 1;\n"
			s.Migs = m
		}
	}
	result := func(s *productFactSources) *string { return &s.Result }
	tapT := func(s *productFactSources) *string { return &s.Tap }
	// tapRender edits the screen served for one direction, on a copy of the map.
	tapRender := func(d pages.TapDirection, old, new string) func(*productFactSources) {
		return func(s *productFactSources) {
			m := maps.Clone(s.TapRenders)
			if !strings.Contains(m[d], old) {
				t.Fatalf("PREMISE: %q is not in the tap screen drawn for %q", old, d.Words())
			}
			m[d] = strings.Replace(m[d], old, new, 1)
			s.TapRenders = m
		}
	}
	tests := []struct {
		fact pages.Fact
		name string
		edit func(*productFactSources)
	}{
		{pages.FactRecordedTapEndsOnAStampedDocket, "the result screen is no docket", replace(result, `<section class="docket">`, `<section class="slip">`)},
		{pages.FactRecordedTapEndsOnAStampedDocket, "the stamp loses IGNORED", replace(result, `stamp--ignored">IGNORED<`, `stamp--ignored">DONE<`)},
		{pages.FactRecordedTapEndsOnAStampedDocket, "the handler renders something else", replace(func(s *productFactSources) *string { return &s.TapHandler }, "pages.Result(resultView(res)", "pages.Other(resultView(res)")},
		{pages.FactResultScreenEndsWithoutAButton, "a counted tap's closing changes", replace(result, `<p class="mt-4 text-ink/70">All done — you can close this page.</p>`, `<p class="mt-4 text-ink/70">Done.</p>`)},
		{pages.FactResultScreenEndsWithoutAButton, "the result screen gains a button", replace(result, `<p class="mt-4 text-ink/70">All done`, `<button>Again</button><p class="mt-4 text-ink/70">All done`)},
		{pages.FactResultScreenEndsWithoutAButton, "a sub-template renders a link on the flagged result", func(s *productFactSources) {
			r := maps.Clone(s.ResultRenders)
			r["flag"] = strings.Replace(r["flag"], "</section>", `</section><a href="/again">Tap again</a>`, 1)
			s.ResultRenders = r
		}},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the tap form draws a second face", replace(tapT, "@TapButtonFace(TapButtonSubmits, d)", "@TapButtonFace(TapButtonSubmits, d)\n\t\t@TapButtonFace(TapButtonSubmits, d)")},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the greeting gains a link", replace(tapT, `<p class="docket-label">Tapping</p>`, `<p class="docket-label">Tapping</p><a href="/menu">Menu</a>`)},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the correction loses its condition", replace(tapT, "if d.Other().Want() != \"\" {\n\t\t\t@tapCorrection(d.Other())\n\t\t}", "@tapCorrection(d.Other())")},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the correction moves above the primary button", replace(tapT, "@TapButtonFace(TapButtonSubmits, d)\n\t\tif d.Other().Want() != \"\" {\n\t\t\t@tapCorrection(d.Other())\n\t\t}", "if d.Other().Want() != \"\" {\n\t\t\t@tapCorrection(d.Other())\n\t\t}\n\t\t@TapButtonFace(TapButtonSubmits, d)")},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the correction takes the accent", replace(tapT, `items-center self-center px-3 text-base text-ink underline`, `items-center self-center px-3 text-base bg-brand text-ink underline`)},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the correction becomes a second primary button", replace(tapT, `class="mt-2 inline-flex min-h-11`, `class="tap-button mt-2 inline-flex min-h-11`)},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the face draws a button that is not the primary one", replace(tapT, `<button type="button" class="tap-button">`, `<button type="button" class="tap-link">`)},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the served tap screen carries a second button", tapRender(pages.TapUndirected, "</form>", `</form><button type="button">History</button>`)},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the undirected screen draws a correction", tapRender(pages.TapUndirected, "</button>", `</button><button type="submit" name="want" value="out" class="text-ink" data-tap-correction>Not right? Clock out</button>`)},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the served correction leaves the form", func(s *productFactSources) {
			tapRender(pages.TapClockIn, "</form>", "")(s)
			tapRender(pages.TapClockIn, `<button type="submit" name="want" value="out"`, `</form><button type="submit" name="want" value="out"`)(s)
		}},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the served correction sends the predicted direction", tapRender(pages.TapClockIn, `name="want" value="out" class="mt-2`, `name="want" value="in" class="mt-2`)},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the served correction says the wrong words", tapRender(pages.TapClockOut, ">Not right? Clock in<", ">Not right? Clock out<")},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the served correction is drawn in the accent", tapRender(pages.TapClockOut, `data-tap-correction`, `style="background:rgb(var(--brand-accent))" data-tap-correction`)},
		{pages.FactTapScreenIsOneButtonAndACorrection, "the served primary button says Tap under a prediction", tapRender(pages.TapClockIn, `data-tap-button>Clock in<`, `data-tap-button>Tap<`)},
		{pages.FactRecordsReadForAnyEarlierDay, "the day stepper loses Previous day", replace(func(s *productFactSources) *string { return &s.Transactions }, `aria-label="Previous day"`, `aria-label="Back"`)},
		{pages.FactRecordsReadForAnyEarlierDay, "the day is not read", replace(func(s *productFactSources) *string { return &s.TxHandler }, `ledger.ParseDate(q.Get("date"))`, `ledger.ParseDate("")`)},
		{pages.FactSpikeShowsEvidenceAndTheRulesSentence, "The Spike loses its reason", replace(func(s *productFactSources) *string { return &s.Docket }, "Why it is here", "Details")},
		{pages.FactSpikeShowsEvidenceAndTheRulesSentence, "Decide stops copying the reason", replace(func(s *productFactSources) *string { return &s.Decide }, "Note: pd.Reason,", "Note: \"\",")},
		{pages.FactReviewCarriesANote, "the note field goes", replace(func(s *productFactSources) *string { return &s.Review }, `name="note"`, `name="comment"`)},
		{pages.FactReviewCarriesANote, "the note column goes", replace(func(s *productFactSources) *string { return &s.Mig05 }, "    note           text,", "    remark         text,")},
		{pages.FactReviewCarriesANote, "a later migration drops the note", later("ALTER TABLE transaction_reviews DROP COLUMN note;")},
		{pages.FactOpenCheckInIsListedNotCounted, "The Roll counts open check-ins", replace(func(s *productFactSources) *string { return &s.Reports }, "Open check-ins, not counted", "Open check-ins")},
		{pages.FactLatenessFollowsOwnShift, "the department no longer wins", anchor(pages.AnchorLatenessFollowsDepartment)},
		{pages.FactLatenessFollowsOwnShift, "the venue is the home venue", func(s *productFactSources) {
			block := queryBlock(s.TxQueries, "ListWorkedShiftEvents")
			s.TxQueries = strings.Replace(s.TxQueries, block, strings.Replace(block, "AND l.id = t.location_id", "AND l.id = e.location_id", 1), 1)
		}},
		{pages.FactPeopleShowOneStatus, "People loses DEACTIVATED", replace(func(s *productFactSources) *string { return &s.Roster }, `pill--deactivated flex-none">DEACTIVATED<`, `pill--deactivated flex-none">GONE<`)},
		{pages.FactPeopleShowOneStatus, "a fourth status, SUSPENDED, joins the pill", replace(func(s *productFactSources) *string { return &s.Roster },
			`			<span class="pill pill--deactivated flex-none">DEACTIVATED</span>`,
			"			<span class=\"pill pill--deactivated flex-none\">DEACTIVATED</span>\n\t\tcase \"suspended\":\n\t\t\t<span class=\"pill pill--deactivated flex-none\">SUSPENDED</span>")},
		{pages.FactPeopleShowOneStatus, "a later migration widens the status CHECK", later("ALTER TABLE employees DROP CONSTRAINT employees_status_check;\n" +
			"ALTER TABLE employees ADD CONSTRAINT employees_status_check CHECK (status IN ('invited', 'active', 'deactivated', 'suspended'));")},
		{pages.FactInviteLinkShownOnceOrMailed, "the link is no longer shown once", replace(func(s *productFactSources) *string { return &s.Employees }, "Activation link — shown once", "Activation link")},
		{pages.FactInviteLinkShownOnceOrMailed, "the e-mail mode goes", replace(func(s *productFactSources) *string { return &s.Config }, `InviteDeliveryEmail = "email"`, `InviteDeliveryEmail = "panel2"`)},
		{pages.FactInviteLinkShownOnceOrMailed, "the code is stored plain", replace(func(s *productFactSources) *string { return &s.InviteManager }, "code.hash(m.hmacKey)", "code.plain()")},
		{pages.FactManualEntryStartsFromThePerson, "the card no longer links to the entry", replace(func(s *productFactSources) *string { return &s.Roster }, ">Enter a record by hand</a>", ">Record</a>")},
		{pages.FactVenueHoldsItsEvidenceAndHours, "a venue loses its shift", anchor(pages.AnchorVenueShiftAndAddress)},
		{pages.FactVenueHoldsItsEvidenceAndHours, "a venue loses its GPS point", replace(func(s *productFactSources) *string { return &s.Mig02 }, "gps_lat     numeric", "gps_x       numeric")},
		{pages.FactVenueHoldsItsEvidenceAndHours, "a later migration drops the GPS point", later("ALTER TABLE public.locations DROP COLUMN gps_lat, DROP COLUMN gps_lng;")},
		{pages.FactRuleChangeIsConfirmedThenRecorded, "the write stops checking the confirmation", replace(func(s *productFactSources) *string { return &s.PolicyActions }, "a.confirmationRefusalFor(", "a.ignore(")},
		{pages.FactRuleChangeIsConfirmedThenRecorded, "the review mints no confirmation", replace(func(s *productFactSources) *string { return &s.PolicyActions }, "a.setConfirmation(", "a.skip(")},
		{pages.FactOnlyAnOwnerChangesARule, "the owner gate goes", func(s *productFactSources) {
			s.Guards = slices.DeleteFunc(slices.Clone(s.Guards), func(g policy.Guardrail) bool { return g.Sid == "sys:policy-edit-owner-only" })
		}},
		{pages.FactOnlyAnOwnerChangesARule, "the guardrail stops refusing a manager", func(s *productFactSources) {
			s.Guards = slices.Clone(s.Guards)
			for i := range s.Guards {
				if s.Guards[i].Sid == "sys:policy-edit-owner-only" {
					s.Guards[i].Match = func(policy.Context) bool { return false }
				}
			}
		}},
		{pages.FactOnlyAnOwnerChangesARule, "the screen draws controls for anybody", replace(func(s *productFactSources) *string { return &s.PoliciesGo }, "v.MayEdit = a.scribe.May(", "v.MayEdit = true || a.scribe.May(")},
		{pages.FactWebhookIsOneEndpointBehindTheAPI, "a second endpoint is allowed", replace(func(s *productFactSources) *string { return &s.Mig48 }, "webhook_endpoints_one_per_tenant UNIQUE (tenant_id)", "webhook_endpoints_one_per_tenant UNIQUE (id)")},
		{pages.FactWebhookIsOneEndpointBehindTheAPI, "a later migration drops the one-per-business key", later("ALTER TABLE webhook_endpoints DROP CONSTRAINT webhook_endpoints_one_per_tenant;")},
		{pages.FactWebhookIsOneEndpointBehindTheAPI, "the endpoint skips the API gate", replace(func(s *productFactSources) *string { return &s.Endpoints }, "requireOpen(ctx, q, c.TenantID)", "nil")},
	}
	controlled := map[pages.Fact]bool{}
	for fact, derive := range productDerivationFuncs {
		if why := derive(real); why != "" {
			t.Errorf("PREMISE %s: the derivation fails on the real sources: %s", fact, why)
		}
	}
	for _, tc := range tests {
		derive, ok := productDerivationFuncs[tc.fact]
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
	for fact := range productDerivationFuncs {
		if !controlled[fact] {
			t.Errorf("%s has no negative control", fact)
		}
	}
}

// --- the copy ------------------------------------------------------------------------

// productFigures is what the handler hands the page.
func productFiguresForTest() pages.ProductFigures { return productFigures(securityFigures()) }

// productLines is every sentence /product renders from a pages.Line: every Line of
// ProductText at any depth, the landing's third step it repeats, and the sign-off's free
// months.
func productLines() map[string]pages.Line {
	out := map[string]pages.Line{
		"LandingFreeMonthsLine": pages.LandingFreeMonthsLine(foundingFreeMonths),
	}
	step := pages.LandingSteps[2]
	for i, run := range step.Body {
		out["LandingSteps[2].Body["+strconv.Itoa(i)+"]"] = pages.Line{Text: run.Text, Facts: step.Facts}
	}
	collectLines(reflect.ValueOf(pages.ProductCopy(productFiguresForTest())), "ProductText", out)
	return out
}

// TestProductLines_EveryLineIsCollected: the walk reaches every Line of the page -- a
// section's lines and bullets, the points' titles and bodies, the steps, the integrations
// -- no field could hide one from it, and none is empty.
func TestProductLines_EveryLineIsCollected(t *testing.T) {
	t.Parallel()
	for _, p := range unreachableLines(reflect.TypeOf(pages.ProductText{}), "ProductText") {
		t.Error(p)
	}
	lines := productLines()
	if len(lines) < 45 {
		t.Fatalf("productLines collects %d line(s); the page carries more", len(lines))
	}
	for _, want := range []string{"ProductText.Tap.Bullets[2]", "ProductText.TeamPoints[3].Body", "ProductText.Rules.Lines[0]",
		"ProductText.ShiftSteps[5].Text", "ProductText.Integrations[2].Text", "ProductText.IntegNote"} {
		if _, ok := lines[want]; !ok {
			t.Errorf("productLines does not reach %s", want)
		}
	}
	for _, name := range emptyLines(lines) {
		t.Errorf("%s is a Line with no words", name)
	}
}

// TestProductFigures_AreTheProductsAndThePagePrintsThem: the two figures the handler hands
// the page are the engine's window and the webhook's header, and the page prints both.
// CONTROL: a figure changed at the source changes the sentence.
func TestProductFigures_AreTheProductsAndThePagePrintsThem(t *testing.T) {
	t.Parallel()
	f := productFiguresForTest()
	if want := int(policy.DefaultParams().DebounceWindow / time.Second); f.DebounceSeconds != want {
		t.Errorf("the page's window is %d s and the engine's default is %d s", f.DebounceSeconds, want)
	}
	if f.WebhookHeader != webhook.SignatureHeader {
		t.Errorf("the page's webhook header is %q and deliveries are signed in %q", f.WebhookHeader, webhook.SignatureHeader)
	}
	text := renderedPublicText(t, pages.ProductPath)
	// "(the default window)": the window is a deployment setting (LP-15 fact review), as
	// /for/production says it.
	for _, want := range []string{"within " + strconv.Itoa(f.DebounceSeconds) + " seconds (the default window)", f.WebhookHeader + ": sha256=…"} {
		if !strings.Contains(text, want) {
			t.Errorf("/product does not print %q", want)
		}
	}
	other := f
	other.DebounceSeconds++
	if pages.ProductCopy(other).Tap.Bullets[1].Text == pages.ProductCopy(f).Tap.Bullets[1].Text {
		t.Error("CONTROL: the window changed and the sentence did not")
	}
}

// --- the index ---------------------------------------------------------------------------

// productIndexProblems says which "On this page" entry does not land on its section in
// body, or lands out of order.
func productIndexProblems(body string) []string {
	var out []string
	last := -1
	for _, e := range pages.ProductIndex {
		if !strings.Contains(body, `href="#`+e.ID+`"`) {
			out = append(out, "the index carries no link to #"+e.ID)
		}
		at := strings.Index(body, `<section id="`+e.ID+`"`)
		if at < 0 {
			out = append(out, fmt.Sprintf("the index links #%s and no section carries that id", e.ID))
			continue
		}
		if at < last {
			out = append(out, "#"+e.ID+" comes before the section the index lists ahead of it")
		}
		last = at
		title := regexp.MustCompile(`<h2 id="` + regexp.QuoteMeta(e.ID) + `-title"[^>]*>([^<]*)</h2>`).FindStringSubmatch(body)
		if title == nil || title[1] != templEscape(e.Title) {
			out = append(out, fmt.Sprintf("#%s's heading is not %q", e.ID, e.Title))
		}
	}
	return out
}

// templEscape is s as templ writes text.
func templEscape(s string) string { return strings.ReplaceAll(s, "&", "&amp;") }

// TestProductPage_EveryIndexEntryLandsOnItsSection: "On this page" lists the seven sections
// in order, each a link to an id the page renders, on the section that carries the entry's
// own name. CONTROL: a section's id removed, and an entry pointing nowhere, are red.
func TestProductPage_EveryIndexEntryLandsOnItsSection(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), pages.ProductPath)
	if len(pages.ProductIndex) != 7 {
		t.Errorf("the index lists %d section(s); the boards list seven", len(pages.ProductIndex))
	}
	for _, p := range productIndexProblems(body) {
		t.Error(p)
	}
	gone := strings.Replace(body, `<section id="`+pages.ProductRollID+`"`, `<section id="x"`, 1)
	if len(productIndexProblems(gone)) == 0 {
		t.Error("CONTROL: the page with The Roll's id removed passed")
	}
	nowhere := strings.Replace(body, `href="#`+pages.ProductRulesID+`"`, `href="#nowhere"`, 1)
	if len(productIndexProblems(nowhere)) == 0 {
		t.Error("CONTROL: the index with Rules pointing nowhere passed")
	}
}

// --- the drawings --------------------------------------------------------------------------

// productTapProblems is every way the two phones depart from what the product draws for
// their record: the engine's verdict, score and sentence, the tap screen's greeting, and
// the result screen's words.
func productTapProblems(t *testing.T, d pages.PanelDocket, note string, closing []string, plaque string) []string {
	t.Helper()
	var out []string
	dec := tap.Decide(panelEngineInput(d, false))
	if string(dec.Verdict) != "flag" || d.Verdict != "flag" {
		out = append(out, fmt.Sprintf("the phones draw a flagged result and the engine decides %q", dec.Verdict))
	}
	if strconv.Itoa(dec.Trust) != d.Trust {
		out = append(out, fmt.Sprintf("the result prints trust %s and the engine scores %d", d.Trust, dec.Trust))
	}
	if dec.EmployeeNote != note {
		out = append(out, fmt.Sprintf("the result prints %q and the engine's sentence for the employee is %q", note, dec.EmployeeNote))
	}
	var b bytes.Buffer
	view := pages.ResultView{Verdict: string(dec.Verdict), Venue: d.Venue, At: d.At, Trust: dec.Trust, Note: dec.EmployeeNote}
	if dec.Type != nil {
		view.Direction = string(*dec.Type)
	}
	if err := pages.Result(view, layout.Logo{}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	result := screenText(t, b.String())
	for _, want := range append([]string{"Clocked in", d.Venue, "FLAGGED", "Trust " + d.Trust, note}, closing...) {
		if !strings.Contains(result, asRendered(want)) {
			out = append(out, fmt.Sprintf("the result screen does not say %q for this tap", want))
		}
	}
	b.Reset()
	if err := pages.TapHeading(d.Who, d.Venue).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	heading := screenText(t, "<body>"+b.String()+"</body>")
	if want := "Tapping Hello " + d.Who + " " + d.Venue; heading != want {
		out = append(out, fmt.Sprintf("the tap screen reads %q, the drawing %q", heading, want))
	}
	if want := strings.ToUpper(pages.LandingPanelSpikeFocus.Label); plaque != want {
		out = append(out, fmt.Sprintf("the plaque reads %q and The Spike names the record's plaque %q", plaque, want))
	}
	if !strings.HasPrefix(strings.ToUpper(pages.LandingPanelSpikeFocus.Label), strings.ToUpper(d.Venue)+" ") {
		out = append(out, "the plaque is not at the record's venue")
	}
	return out
}

// seedVenueRowRE reads one location row of the seed: name, ranges, the GPS pair, the shift.
var seedVenueRowRE = regexp.MustCompile(`'([^']+)',\s*ARRAY\[([^\]]*)\]::cidr\[\],\s*([0-9.]+|NULL),\s*([0-9.]+|NULL),\s*TIME '(\d\d:\d\d)',\s*TIME '(\d\d:\d\d)'`)

// seedVenueRow is one venue as the seed records it.
type seedVenueRow struct {
	Ranges     string
	GPS        bool
	Start, End string
}

func seedVenueRows(t *testing.T) map[string]seedVenueRow {
	t.Helper()
	out := map[string]seedVenueRow{}
	for _, m := range seedVenueRowRE.FindAllStringSubmatch(repoFile(t, "test", "fixtures", "seed.sql"), -1) {
		out[m[1]] = seedVenueRow{Ranges: strings.ReplaceAll(m[2], "'", ""), GPS: m[3] != "NULL", Start: m[5], End: m[6]}
	}
	if len(out) < 10 {
		t.Fatalf("PREMISE: read %d venue row(s) from the seed; the pattern lost the file", len(out))
	}
	return out
}

// productPeopleProblems is every way People's drawing departs from the panel and the other
// drawings: an invented name the seed has, a venue the seed lacks, a status word or signed-in
// words the panel would not print, an invited person with a phone, a person whose venue the
// other drawings name differently.
func productPeopleProblems(people []pages.ProductPerson, seed, venues []string, roster string) []string {
	var out []string
	var names []string
	elsewhere := map[string]string{}
	for _, d := range pages.LandingPanelPassDockets {
		elsewhere[d.Who] = d.Venue
	}
	for _, q := range pages.LandingPanelSpikeQueue {
		elsewhere[q.Docket.Who] = q.Docket.Venue
	}
	for _, r := range pages.LandingPanelRollRows {
		elsewhere[r.Who] = r.HomeVenue
	}
	pill := templBody(roster, "lifecyclePill")
	for _, row := range people {
		names = append(names, row.Name)
		if !slices.Contains(venues, row.Venue) {
			out = append(out, fmt.Sprintf("%s's venue %q is not a venue of the seed", row.Name, row.Venue))
		}
		if v, ok := elsewhere[row.Name]; ok && v != row.Venue {
			out = append(out, fmt.Sprintf("%s is at %q here and at %q in the panel's drawings", row.Name, row.Venue, v))
		}
		word := pages.ProductStatusWord(row.Status)
		if word == "" || !strings.Contains(pill, `pill--`+row.Status+` flex-none">`+word+`<`) {
			out = append(out, fmt.Sprintf("%s's status %q is not one People prints", row.Name, row.Status))
		}
		if got := deviceCount(row.Devices); got != row.Signed {
			out = append(out, fmt.Sprintf("%s's row says %q and People says %q for %d phone(s)", row.Name, row.Signed, got, row.Devices))
		}
		if row.Status == "invited" && row.Devices != 0 {
			out = append(out, row.Name+" is invited and has a signed-in phone; only activation signs a phone in")
		}
		if row.Status != "invited" {
			if _, ok := elsewhere[row.Name]; !ok && row.Status == "active" {
				out = append(out, row.Name+" is active and in none of the other drawings")
			}
		}
		parts := strings.Fields(row.Name)
		if len(parts) < 2 || row.Initials != parts[0][:1]+parts[len(parts)-1][:1] {
			out = append(out, fmt.Sprintf("%s's initials are not %q", row.Name, row.Initials))
		}
	}
	return append(out, samplePeopleProblems(names, seed)...)
}

// productVenueProblems is every way the Venues drawing departs from the seed and the panel.
func productVenueProblems(venues []pages.ProductVenue, network string, plaques []pages.ProductPlaque, rows map[string]seedVenueRow, plaquesGo string) []string {
	var out []string
	for _, v := range venues {
		r, ok := rows[v.Name]
		if !ok {
			out = append(out, fmt.Sprintf("%q is not a venue of the seed", v.Name))
			continue
		}
		if want := r.Start + " – " + r.End; v.Shift != want {
			out = append(out, fmt.Sprintf("%s's card prints %q and the seed's shift is %q", v.Name, v.Shift, want))
		}
		if v.IP != (r.Ranges != "") || v.GPS != r.GPS {
			out = append(out, fmt.Sprintf("%s's card signs IP %v · GPS %v and the seed has ranges %q, a point %v", v.Name, v.IP, v.GPS, r.Ranges, r.GPS))
		}
	}
	if len(venues) > 0 && rows[venues[0].Name].Ranges != network {
		out = append(out, fmt.Sprintf("the open venue's network is %q and the seed's is %q", network, rows[venues[0].Name].Ranges))
	}
	for _, p := range plaques {
		if !strings.Contains(plaquesGo, `return "`+p.Status+`"`) {
			out = append(out, fmt.Sprintf("the plaque table prints no %q", p.Status))
		}
		if !regexp.MustCompile(`^[0-9A-F]{14}$`).MatchString(p.UID) {
			out = append(out, fmt.Sprintf("%q is not a plaque id as the table prints one", p.UID))
		}
	}
	return out
}

// guardrailWhenRE reads one entry of the Rules screen's words for a guardrail.
func guardrailWords(rulebook, sid string) string {
	block := regexp.MustCompile(`(?s)var guardrailWhen = map\[string\]string\{(.*?)\n\}`).FindStringSubmatch(rulebook)
	if block == nil {
		return ""
	}
	m := regexp.MustCompile(`"` + regexp.QuoteMeta(sid) + `":\s*"([^"]*)"`).FindStringSubmatch(block[1])
	if m == nil {
		return ""
	}
	return m[1]
}

// productRuleProblems is every way the Rules drawing departs from the engine and the Rules
// screen: a guardrail's number, words or effect, a baseline rule's name or effect.
func productRuleProblems(guards, base []pages.ProductRuleRow, gs []policy.Guardrail, docs []policy.BaselineDoc, rulebook string) []string {
	var out []string
	for _, r := range guards {
		at := slices.IndexFunc(gs, func(g policy.Guardrail) bool { return g.Sid == r.Sid })
		if at < 0 {
			out = append(out, r.Sid+" is not a guardrail")
			continue
		}
		if want := fmt.Sprintf("%02d", at+1); r.No != want {
			out = append(out, fmt.Sprintf("%s is numbered %s and the Rules screen numbers it %s", r.Sid, r.No, want))
		}
		if want := guardrailWords(rulebook, r.Sid); r.Words != want {
			out = append(out, fmt.Sprintf("%s reads %q and the Rules screen says %q", r.Sid, r.Words, want))
		}
		if want := effectLabel(string(gs[at].Effect)); r.Effect != want {
			out = append(out, fmt.Sprintf("%s ends in %q and the Rules screen says %q", r.Sid, r.Effect, want))
		}
	}
	for _, r := range base {
		found := false
		for _, d := range docs {
			for _, st := range d.Document.Statements {
				if st.Sid != r.Sid {
					continue
				}
				found = true
				if want := strings.TrimPrefix(d.Name, baselineNamePrefix); r.Words != want {
					out = append(out, fmt.Sprintf("%s is named %q and the Rules screen names it %q", r.Sid, r.Words, want))
				}
				if want := effectLabel(string(st.Effect)); r.Effect != want {
					out = append(out, fmt.Sprintf("%s ends in %q and the Rules screen says %q", r.Sid, r.Effect, want))
				}
			}
		}
		if !found {
			out = append(out, r.Sid+" is not a baseline rule")
		}
	}
	return out
}

// openAPIRouteRE reads a path of the OpenAPI document and the block under it.
func openAPIHasRoute(doc, route string) bool {
	method, path, ok := strings.Cut(route, " ")
	if !ok {
		return false
	}
	at := strings.Index(doc, "\n  "+path+":\n")
	if at < 0 {
		return false
	}
	rest := doc[at+1:]
	if end := regexp.MustCompile(`\n  /`).FindStringIndex(rest[1:]); end != nil {
		rest = rest[:end[0]+1]
	}
	return strings.Contains(rest, "\n    "+strings.ToLower(method)+":")
}

// TestProductPage_TheDrawingsAreWhatTheProductWouldDraw: every sample on the page is held to
// the product -- the phones' record to the engine and the two screens, People to the seed
// and the panel's words, the venues to the seed, the rules to the engine and the Rules
// screen, and the integrations' names to the OpenAPI document and the panel -- and the page
// draws each where the boards put it. CONTROL: one mutation per drawing is red.
func TestProductPage_TheDrawingsAreWhatTheProductWouldDraw(t *testing.T) {
	t.Parallel()
	roster := repoFile(t, "web", "templates", "components", "roster.templ")
	rulebook := repoFile(t, "internal", "domain", "tenant", "rulebook.go")
	plaquesGo := repoFile(t, "internal", "handler", "plaques.go")
	openapi := repoFile(t, "internal", "handler", "api", "openapi.yaml")
	gs := policy.Guardrails(policy.DefaultParams())
	docs := policy.Baseline()
	seed, venues, rows := seedPeople(t), seedVenues(t), seedVenueRows(t)

	for _, p := range productTapProblems(t, pages.ProductTapSample, pages.ProductTapNote, pages.ProductTapClosing, pages.ProductTapPlaque) {
		t.Error(p)
	}
	for _, p := range productPeopleProblems(pages.ProductPeople, seed, venues, roster) {
		t.Error(p)
	}
	for _, p := range productVenueProblems(pages.ProductVenues, pages.ProductVenueNetwork, pages.ProductPlaques, rows, plaquesGo) {
		t.Error(p)
	}
	for _, p := range productRuleProblems(pages.ProductGuardRows, pages.ProductBaselineRows, gs, docs, rulebook) {
		t.Error(p)
	}
	// The drawings' labels are the panel's own.
	labels := map[string][]string{
		"components/staffform.templ": {pages.ProductPeopleAdd},
		"pages/reports.templ":        {pages.ProductRollCSVLabel},
		"pages/billing.templ":        {pages.ProductBillingCSVLabel},
		"pages/policies.templ":       {"Cannot be switched off", "Taptime baseline", "checked in this order"},
		"components/venuedetail.templ": {"Venue network", "Plaques at this venue", `"IP"`, "GPS<span",
			`{ " ✓" }`},
	}
	for file, words := range labels {
		dir, name, _ := strings.Cut(file, "/")
		src := repoFile(t, "web", "templates", dir, name)
		for _, w := range words {
			if !strings.Contains(src, w) && !strings.Contains(src, strings.Trim(w, `"`)) {
				t.Errorf("%s does not print %q, which the drawing does", file, w)
			}
		}
	}
	policiesGo := repoFile(t, "internal", "handler", "policies.go")
	for _, w := range []string{`return "In force"`, `Label: "Switch this rule off"`} {
		if !strings.Contains(policiesGo, w) {
			t.Errorf("the Rules screen no longer says %s", w)
		}
	}
	chips := []string{"Everyone"}
	for _, s := range panelStatuses {
		chips = append(chips, s.Label)
	}
	if !slices.Equal(pages.ProductPeopleChips, chips) {
		t.Errorf("People's drawn filter is %q and the list's is %q", pages.ProductPeopleChips, chips)
	}
	for _, r := range pages.ProductAPIRoutes {
		if !openAPIHasRoute(openapi, r) {
			t.Errorf("the integration API's OpenAPI document names no %s", r)
		}
	}

	// The page draws each sample, whole and in order.
	text := renderedPublicText(t, pages.ProductPath)
	d := pages.ProductTapSample
	want := []string{
		pages.ProductTapPlaque,
		"Sample screen: the tap screen — Hello " + d.Who + ", " + d.Venue + ", one Tap button",
		"Tapping Hello " + d.Who + " " + d.Venue + " Tap",
		"Sample screen: the result of a flagged tap — Clocked in " + d.At + ", FLAGGED",
		d.Venue + " Clocked in " + d.At + " FLAGGED Trust " + d.Trust + " " + pages.ProductTapNote + " " + strings.Join(pages.ProductTapClosing, " "),
	}
	want = append(want, panelDrawingWant()...)
	people := []string{"Sample screen: People", "Kebab Factory People " + pages.ProductPeopleAdd + " " + strings.Join(pages.ProductPeopleChips, " ")}
	for _, p := range pages.ProductPeople {
		people = append(people, p.Initials+" "+p.Name+" "+p.Venue+" "+pages.ProductStatusWord(p.Status)+" "+p.Signed)
	}
	want = append(want, people...)
	want = append(want, "Sample screen: Venues & plaques")
	for _, v := range pages.ProductVenues {
		want = append(want, v.Name+" "+v.Shift+" IP ✓ GPS ✓")
	}
	want = append(want, "Venue "+pages.ProductVenues[0].Name+" Venue network "+pages.ProductVenueNetwork+" Shift "+pages.ProductVenues[0].Shift)
	for _, p := range pages.ProductPlaques {
		want = append(want, p.UID+" "+p.Status)
	}
	want = append(want, "Sample screen: Rules", "Taptime's guardrails")
	for _, g := range pages.ProductGuardRows {
		want = append(want, g.No+" "+g.Words+" Cannot be switched off "+g.Effect)
	}
	want = append(want, "Taptime baseline")
	for _, b := range pages.ProductBaselineRows {
		want = append(want, b.Words+" "+b.Effect+" In force")
	}
	want = append(want, "Switch this rule off", pages.ProductSampleNote)
	renderedInOrder(t, text, "the product tour's drawings", want)

	// CONTROLS, one per drawing.
	far := pages.ProductTapSample
	far.Trust = "50"
	if len(productTapProblems(t, far, pages.ProductTapNote, pages.ProductTapClosing, pages.ProductTapPlaque)) == 0 {
		t.Error("CONTROL: a flagged tap drawn with a score of 50 passed")
	}
	if len(productTapProblems(t, pages.ProductTapSample, pages.ProductTapNote, []string{"All done — you can close this page."}, pages.ProductTapPlaque)) == 0 {
		t.Error("CONTROL: a flagged result drawn with the counted tap's closing passed")
	}
	people0 := slices.Clone(pages.ProductPeople)
	people0[3].Name = "Ahmed Hassan"
	if len(productPeopleProblems(people0, seed, venues, roster)) == 0 {
		t.Error("CONTROL: a seed person drawn as a sample passed")
	}
	people1 := slices.Clone(pages.ProductPeople)
	people1[3].Devices, people1[3].Signed = 1, "1 signed in"
	if len(productPeopleProblems(people1, seed, venues, roster)) == 0 {
		t.Error("CONTROL: an invited person with a signed-in phone passed")
	}
	venues0 := slices.Clone(pages.ProductVenues)
	venues0[3].Shift = "10:00 – 22:00"
	if len(productVenueProblems(venues0, pages.ProductVenueNetwork, pages.ProductPlaques, rows, plaquesGo)) == 0 {
		t.Error("CONTROL: Rusty Bar drawn on a day shift passed")
	}
	guards0 := slices.Clone(pages.ProductGuardRows)
	guards0[0].No = "02"
	if len(productRuleProblems(guards0, pages.ProductBaselineRows, gs, docs, rulebook)) == 0 {
		t.Error("CONTROL: a guardrail drawn with another number passed")
	}
	base0 := slices.Clone(pages.ProductBaselineRows)
	base0[2].Effect = "Approve"
	if len(productRuleProblems(pages.ProductGuardRows, base0, gs, docs, rulebook)) == 0 {
		t.Error("CONTROL: the no-evidence rule drawn as approving passed")
	}
	if openAPIHasRoute(openapi, "DELETE /v1/punches") || openAPIHasRoute(openapi, "GET /v1/payroll") {
		t.Error("CONTROL: a route the API does not have passed")
	}
}

// --- what the page must not say ------------------------------------------------------------

// guaranteeRE is the word in any form and case -- guarantee, guarantees, guaranteed,
// Guaranty -- wherever it stands in the served bytes, an attribute or a comment included.
var guaranteeRE = regexp.MustCompile(`(?i)guarant`)

// productRefusedPhrasings are the board's sentences this card withdrew, each with its
// reason (productview.go). None may come back, on this page or any other public page.
var productRefusedPhrasings = map[string]string{
	"Nobody can decide":               "the self-review guard never meets a real review today (U10, backlog T148)",
	"Taptime keeps no copy":           "an invitation is kept as a keyed hash, and with e-mail delivery it is mailed, not shown (U5)",
	"moments later":                   "the window is a deployment setting with a default the page names",
	"Pick any earlier day":            "the records list steps one day at a time (U2)",
	"never approved by default":       "the default sends an undecided tap to review; \"never\" was the security review's refused word",
	"hand over the invite":            "with e-mail delivery nobody hands the link over",
	"The answer is a stamped docket":  "a refused link ends on the problem screen, not a docket",
	"recorded with your name against": "the trail records the account; the page does not promise how a name is shown",
	// Withdrawn at the main merge (2026-10-25, user decision "write it as M15 draws it"):
	// under a predicted direction the tap screen also draws the "Not right? …" correction.
	"One screen, one button.":    "a prediction draws the correction under the one large button (ADR 0032 §B.1)",
	"was tapped and one button.": "a prediction draws the correction under the one large button (ADR 0032 §B.1)",
	"one button, done":           "a prediction draws the correction under the one large button (ADR 0032 §B.1)",
}

// TestProductPage_SaysNoGuaranteeAndNoneOfTheWithdrawnSentences: the served bytes of every
// public page carry no form of "guarantee" (README O1: the unmeasured-claim scanner reads
// the screen text; this reads the bytes, attributes included), and no page says one of the
// board's sentences this card withdrew. CONTROL: each planted is red.
func TestProductPage_SaysNoGuaranteeAndNoneOfTheWithdrawnSentences(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	refused := func(body string) []string {
		var out []string
		if hit := guaranteeRE.FindString(body); hit != "" {
			out = append(out, strconv.Quote(hit)+": README O1")
		}
		text := strings.ReplaceAll(body, "&#39;", "'")
		for phrase, why := range productRefusedPhrasings {
			if strings.Contains(text, phrase) {
				out = append(out, strconv.Quote(phrase)+": "+why)
			}
		}
		sort.Strings(out)
		return out
	}
	for _, url := range marketingURLs() {
		for _, p := range refused(mustFetchMarketing(t, r, url)) {
			t.Errorf("%s says %s", url, p)
		}
	}
	body := mustFetchMarketing(t, r, pages.ProductPath)
	for _, plant := range []string{`<span aria-label="Taptime guarantees">x</span>`, "GUARANTEED"} {
		if len(refused(body+plant)) == 0 {
			t.Errorf("CONTROL: %q planted on the page passed", plant)
		}
	}
	for phrase := range productRefusedPhrasings {
		if len(refused(body+" "+phrase)) == 0 {
			t.Errorf("CONTROL: %q planted on the page passed", phrase)
		}
	}
}

// TestProductPage_TheLandingLinksToTheTour: the landing's panel section ends with "Take
// the product tour", a link to /product -- the one link in that section (O8: it arrived
// with the route). CONTROL: the link pointed elsewhere is red.
func TestProductPage_TheLandingLinksToTheTour(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	check := func(body string) string {
		section := panelSectionRE.FindString(body)
		m := regexp.MustCompile(`(?s)<a class="more lp-moreAfter" href="([^"]*)">\s*Take the product tour`).FindStringSubmatch(section)
		switch {
		case m == nil:
			return "the panel section has no \"Take the product tour\" link"
		case m[1] != pages.ProductPath:
			return "\"Take the product tour\" goes to " + m[1]
		case strings.Count(section, "<a ") != 1:
			return fmt.Sprintf("the panel section carries %d links", strings.Count(section, "<a "))
		}
		return ""
	}
	if why := check(body); why != "" {
		t.Error(why)
	}
	if check(strings.Replace(body, `<a class="more lp-moreAfter" href="`+pages.ProductPath+`">`, `<a class="more lp-moreAfter" href="/pricing">`, 1)) == "" {
		t.Error("CONTROL: the tour link pointed at /pricing passed")
	}
}

// everyPunchRE is the wording the LP-08 review refused: the feed and the webhook carry
// counted taps, never a manager's typed entry.
var everyPunchRE = regexp.MustCompile(`(?i)\b(?:every|each|all)\s+punch(?:es)?\b`)

// feedSentenceProblems is every sentence resting on FactAPIFeedsEveryCountedTap that
// talks about the feed or the webhook without saying "counted tap", or says "every
// punch"; and every page whose sentence names the feed without saying where a manager's
// entries go instead.
func feedSentenceProblems(copies map[string]publicCopy) []string {
	var out []string
	check := func(url, text string, facts []pages.Fact) (feed bool) {
		if !slices.Contains(facts, pages.FactAPIFeedsEveryCountedTap) {
			return false
		}
		if everyPunchRE.MatchString(text) {
			out = append(out, fmt.Sprintf("%s says %q: a manager's typed entry is not in the feed", url, everyPunchRE.FindString(text)))
		}
		lower := strings.ToLower(text)
		if (strings.Contains(lower, "feed") || strings.Contains(lower, "webhook")) && !strings.Contains(lower, "counted tap") {
			out = append(out, fmt.Sprintf("%s's %q does not say the feed carries counted taps", url, text))
		}
		return strings.Contains(lower, "from a feed")
	}
	for _, url := range slices.Sorted(maps.Keys(copies)) {
		c := copies[url]
		named, told := false, false
		for _, l := range c.lines {
			named = check(url, l.Text, l.Facts) || named
			told = told || strings.Contains(l.Text, "typed by a manager stay in the CSV")
		}
		for _, q := range c.questions {
			named = check(url, q.A, q.Facts) || named
			told = told || strings.Contains(q.A, "typed by a manager stay in the CSV")
		}
		if named && !told {
			out = append(out, url+" names the feed and does not say a manager's entries stay in the CSV")
		}
	}
	return out
}

// TestAPIFeedSentences_SayCountedTapsAndWhereAManagersEntryGoes: on every public page, a
// sentence about the feed or the webhook says "counted tap" (00049's punch_feed admits an
// ok NFC or QR tap and an approved flagged one, nothing on the manual channel), none says
// "every punch", and a page that names the feed says a manager's entries stay in the CSV.
// CONTROL: the landing FAQ's old sentence put back is red twice.
func TestAPIFeedSentences_SayCountedTapsAndWhereAManagersEntryGoes(t *testing.T) {
	t.Parallel()
	copies := publicPagesCopy()
	for _, p := range feedSentenceProblems(copies) {
		t.Error(p)
	}
	for _, url := range []string{"/", pages.SecurityPath, pages.ProductPath} {
		found := false
		for _, l := range copies[url].lines {
			found = found || slices.Contains(l.Facts, pages.FactAPIFeedsEveryCountedTap)
		}
		for _, q := range copies[url].questions {
			found = found || slices.Contains(q.Facts, pages.FactAPIFeedsEveryCountedTap)
		}
		if !found {
			t.Errorf("PREMISE: %s carries no sentence on the feed fact; this test reads nothing there", url)
		}
	}
	old := maps.Clone(copies)
	landing := old["/"]
	qs := slices.Clone(landing.questions)
	for i := range qs {
		qs[i].A = strings.Replace(qs[i].A, "every counted tap from a feed or receive it by webhook; entries typed by a manager stay in the CSV",
			"every punch from a feed or receive it by webhook", 1)
	}
	landing.questions = qs
	old["/"] = landing
	if got := feedSentenceProblems(old); len(got) < 2 {
		t.Errorf("CONTROL: the landing's old \"every punch\" sentence put back gave %d problem(s), want two: %v", len(got), got)
	}
}

// productSampleProblems names every tour section with a drawing whose own markup does
// not carry the sample note.
func productSampleProblems(body string) []string {
	var out []string
	for _, id := range []string{pages.ProductTapID, pages.ProductPassID, pages.ProductSpikeID, pages.ProductRollID,
		pages.ProductPeopleID, pages.ProductVenuesID, pages.ProductRulesID} {
		at := strings.Index(body, `<section id="`+id+`"`)
		if at < 0 {
			out = append(out, "PREMISE: the section #"+id+" is not drawn")
			continue
		}
		section := body[at:]
		if end := strings.Index(section, "</section>"); end >= 0 {
			section = section[:end]
		}
		if !strings.Contains(section, pages.ProductSampleNote) {
			out = append(out, "#"+id+" draws invented people and does not say, where its drawing stands, that it is a sample")
		}
	}
	return out
}

// TestProductPage_EveryDrawingSaysItIsASample: each tour section with a drawing carries
// the sample note in its own markup -- not only the first and the last, as before the
// LP-15 fact review. CONTROL: the note taken out of The Spike's section is red.
func TestProductPage_EveryDrawingSaysItIsASample(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), pages.ProductPath)
	for _, p := range productSampleProblems(body) {
		t.Error(p)
	}
	at := strings.Index(body, `<section id="`+pages.ProductSpikeID+`"`)
	if at < 0 {
		t.Fatal("PREMISE: The Spike's section is not drawn")
	}
	cut := body[:at] + strings.Replace(body[at:], pages.ProductSampleNote, "", 1)
	if len(productSampleProblems(cut)) == 0 {
		t.Error("CONTROL: The Spike's drawing without its note passed")
	}
}
