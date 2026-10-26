package operator_test

// optenant_test.go -- M13-08: the tenant overview and its two write cards in the redesign.
// docs/plan/m13-operator-redesign.md, card M13-08; docs/design/operator-redesign screens.md §6
// (with "Panel writes card"), README §2 annex (c), §6 "Card", "Fact grid", "Suspension card".

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// The overview's blocks and the pieces of them screens.md §6 names, each a whole tag. Each card
// spaces its blocks (README §6 "Card": gap-3.5 in .op-card's rule, no mt-* on a card's or a form's
// children); the docket keeps its pinned class attribute and spaces its own children.
const (
	overviewCard     = `<section class="op-card">`
	overviewH1       = `<h1 class="docket-label">Tenant overview</h1>`
	overviewFacts    = `<dl class="grid grid-cols-2 gap-x-4 gap-y-3.5 md:gap-x-5 md:gap-y-4 desk:grid-cols-[320px_200px_minmax(0,1fr)_minmax(0,1fr)]">`
	overviewWideFact = `<div class="col-span-2 md:col-span-1">`
	overviewDocket   = `<section class="docket" aria-labelledby="tenant-live">`
	overviewLive     = `<h2 id="tenant-live" class="docket-label">Live now</h2>`
	overviewCounts   = `<dl class="mt-3 grid grid-cols-2 items-end gap-4 md:grid-cols-4 md:gap-x-5">`
	overviewNote     = `<p class="mt-3 text-xs leading-[1.45] text-ink/70">`
	overviewSee      = `<p class="mt-2 flex flex-col items-start text-sm md:flex-row md:flex-wrap md:gap-x-6">`
	overviewBack     = `<p class="flex flex-col items-start text-sm md:flex-row md:flex-wrap md:gap-x-6">` +
		`<a href="/operator/tenants" class="op-link">Back to the tenants</a> <a href="/operator" class="op-link">Back to the console</a></p>`

	apiCard    = `<section class="op-card" aria-labelledby="tenant-api">`
	apiHeading = `<h2 id="tenant-api" class="docket-label">Integration API</h2>`
	apiState   = `<p class="flex flex-wrap items-center gap-x-3.5 gap-y-2">`
	apiTextP   = `<p class="text-sm leading-normal">`
	// The open form: its children in the phone's reading order (label, box, help, button); from the
	// tablet the button beside the box and the help under both, by placement (the boards' row).
	apiFormClass = ` class="flex flex-col gap-2 border-t border-dashed border-line pt-3.5 md:grid md:grid-cols-[minmax(0,220px)_auto] md:justify-start md:gap-x-2.5">`
	apiLabel     = `<label for="tenant-api-code" class="op-label md:col-span-2">API code</label>`
	apiHelp      = `<p class="text-xs leading-[1.45] text-ink/70 md:col-span-2 md:row-start-3">A capital letter followed by 1 to 15 ` +
		`capital letters or digits, such as KF. No other business in its holding may have it.</p>`
	apiButton = `<button type="submit" class="btn btn--primary md:col-start-2 md:row-start-2 md:justify-self-start">`
	// The close form: its button the phone's full width, its own width from the tablet.
	apiCloseRest = ` class="flex flex-col md:block"><button type="submit" class="btn btn--quiet">Close the API</button></form>`

	suspensionCard      = `<section class="op-card" aria-labelledby="tenant-suspension">`
	suspensionHeading   = `<h2 id="tenant-suspension" class="docket-label">Panel writes</h2>`
	suspensionFacts     = `<dl class="grid grid-cols-1 gap-4 md:grid-cols-2">`
	suspensionFormClass = ` class="flex flex-col gap-3 border-t border-dashed border-line pt-3.5">`
	suspendH3           = `<h3 class="font-display text-lg font-bold tracking-tight">Suspend this tenant's writes</h3>`
	reinstateH3         = `<h3 class="font-display text-lg font-bold tracking-tight">Lift the suspension</h3>`
	suspendButton       = `<button type="submit" class="btn btn--quiet self-start">Suspend writes</button>`
	reinstateButton     = `<button type="submit" class="btn btn--primary self-start">Lift the suspension</button>`
	confirmBox          = `<label class="inline-flex min-h-11 items-center gap-2 text-sm"><input type="checkbox" name="confirm" ` +
		`value="yes" required class="h-5 w-5 shrink-0 accent-tappa-green"> I have checked that this is the right tenant.</label>`
)

// The overview's sentences, word for word (screens.md §6).
const (
	overviewNoteWords = "Counted when this page was opened. Locations have no status; the other three count the active ones only."
	apiOpenWords      = "The business's integration keys work and its webhooks are sent. Every punch names it by this code."
	apiClosedWords    = "The business's integration keys are refused and no webhook is sent. Punches are still recorded, and keep this code."
	apiNeverWords     = "The API has never been opened for this business. Its owner can create keys in the panel; they work once it is open."
	apiHelpWords      = "A capital letter followed by 1 to 15 capital letters or digits, such as KF. No other business in its holding may have it."
	activeWords       = "The business's panel accepts its writes."
	suspendedWords    = "The business's panel refuses its writes. Taps, signing in, reading every section, the CSV exports and the four " +
		"paths of the time record stay open."
	suspendP1 = "Every write of the business's panel is refused until the suspension is lifted — except signing in and out, a " +
		"panel account's own password, and the four paths of the time record: adding a person to the staff, a new invitation " +
		"and both steps of a manual entry. Taps, reading every section and the CSV exports go on, and the months of a " +
		"suspension are billed as usual."
	suspendP2 = "Every section shows the business a banner with a sentence for the reason: non-payment and a breach of the " +
		"terms are named; a security hold and the business's own request share one neutral sentence."
	reinstateP = "The business's panel accepts its writes again from its next request. The suspension and its lifting stay on " +
		"record in the business's own audit log and in the operator audit log."
	boxWords = "I have checked that this is the right tenant."
)

// TestTenantOverviewScreen_IsTheFactsTheCountsAndTheTwoWriteCards (M13-08; screens.md §6 and its
// "Panel writes card", README §2 annex (c), §6 "Card", "Fact grid", "Suspension card"): each
// overview state -- screens()'s four "tenant overview" renders (active with the API open; unnamed,
// the API closed under a code; suspended for a known reason; suspended for an unrecognised one)
// and three rendered here (the API never opened; a refused open's warn notice with its rules; a
// close's ok notice) -- is the frame's column holding exactly, in this order: the facts card
// (overviewCard), the "Live now" docket, the API's notice when there is one, the Integration API
// card, the Panel writes card and the back links' row -- nothing else. No child of a card, and no
// child of a form in one, carries a top margin (the card spaces its blocks). The column's words,
// tags dropped, are exactly screens.md §6's in order for the state -- the h1, the four facts, the
// four counts and the note, the three links, the notice, the API card's state line, sentence,
// field, help and buttons, the Panel writes card's chip, sentence, facts and one form's words (the
// hold warning's heading and every sentence included), the back links. The h1, the two lists, the
// note's and the links' tags, the API card's state line, its form's tag and children (label, box,
// help, button in that order), the close form, both suspension forms' tags, h3s, boxes and
// buttons are the whole tags above; the page has one h1, the facts card's. Each input has exactly
// its attribute set: the API code box id, type, name, value (the code), maxlength 16, required,
// autocomplete off, spellcheck false; each reason radio id, type radio, name reason, value,
// required -- and the security hold's alone aria-describedby the warning; the box type checkbox,
// name confirm, value yes, required. The suspend form carries the four radios, the box and one
// button; the reinstate form the box and one button. Forms: the open form once, the close form
// exactly when the API is open, one suspension form by state; no other. The column's anchors are
// exactly the three "See this tenant's" links and the two back links. Nowhere in the column:
// hidden content (a hidden attribute, class or field, sr-only, aria-hidden), a preselected radio
// (checked), a formaction or formmethod, a select, a textarea, a drawing, an image, a script,
// an e-mail address, a VAT number.
//
// PART II -- red on: a block added to, dropped from or moved in the column; a top margin back on
// a card's or a form's child; a word changed, added or dropped; any whole tag above changed; the
// h1 back to a paragraph or a second h1; an attribute added to, dropped from or changed on any
// input; a radio preselected; a form or an anchor added or dropped; hidden content.
// PART III -- the layout per width is the screenshot round's (320 / 390 / 834 / 1200 / 1440 px)
// and the count figures' sizes TestTenantOverviewCounts_TheScopedRuleKeepsTheBoardsSizes's; the
// warning's show-on-hold rule and its place after the radios are
// TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice's; the routes
// TestOperatorScreens_EveryActionAndLinkIsAMountedRoute's; the escaping of the tenant's values
// TestTenantScreens_EscapeWhatATenantAndAnOperatorTyped's -- no completeness claim.
func TestTenantOverviewScreen_IsTheFactsTheCountsAndTheTwoWriteCards(t *testing.T) {
	all := screens(t)
	const id = "10000000-0000-4000-8000-000000000001"
	path := "/operator/tenants/" + id
	name, err := operatorpages.NewTenantName("Kebab Factory Ltd")
	if err != nil {
		t.Fatal(err)
	}
	base := operatorpages.TenantOverviewView{Name: name, ID: id, CreatedAt: "2026-07-05 09:30 UTC", Plan: "founding",
		BusinessType: "restaurant", Locations: "9", ActiveEmployees: "41", ActivePlaques: "9", ActiveAdmins: "2",
		PlaquesPath: path + "/plaques", BillingPath: path + "/billing", VATPath: path + "/vat", Tabs: fixtureTabs,
		API: operatorpages.TenantAPIView{OpenPath: path + "/integration", ClosePath: path + "/integration/close"},
		Suspension: operatorpages.TenantSuspensionView{SuspendPath: path + "/suspend", ReinstatePath: path + "/reinstate",
			Reasons: []operatorpages.SuspensionReasonOption{{Value: "non_payment", Label: "Non-payment"},
				{Value: "terms_breach", Label: "Breach of the terms"}, {Value: "security_hold", Label: "Security hold", Hold: true},
				{Value: "customer_request", Label: "At the business's request"}}}}
	refused, closed := base, base
	refused.API.Code = "KF"
	refused.API.NoticeTone, refused.API.NoticeHeading = operatorpages.HoldingNoticeWarn, "Not opened — nothing changed."
	refused.API.Notice = "The database opens the API under a code only when every rule below holds. The API section shows the business as it is now."
	refused.API.NoticeRules = []string{"The code is a capital letter followed by 1 to 15 capital letters or digits.", "The business exists."}
	closed.API.Code = "KF"
	closed.API.NoticeTone, closed.API.NoticeHeading = operatorpages.HoldingNoticeOK, "Closed."
	closed.API.Notice = "The integration API is closed for this business: its keys are refused from the next request and no webhook is sent. The code is kept."
	for k, v := range map[string]operatorpages.TenantOverviewView{"tenant overview, never opened": base,
		"tenant overview, a refused open": refused, "tenant overview, closed": closed} {
		var b bytes.Buffer
		if err := operatorpages.TenantOverview(v).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		all[k] = b.String()
	}

	type state struct {
		api       string // open, closed, never
		notice    string // the notice's words, "" for none
		noticeTag string
		suspended bool
		reason    string // the Reason fact's words
	}
	const warnTag, okTag = `<section role="status" class="op-notice op-notice--warn">`, `<section role="status" class="op-notice op-notice--ok">`
	states := map[string]state{
		"tenant overview":                         {api: "open"},
		"tenant overview, unnamed":                {api: "closed"},
		"tenant overview, suspended":              {api: "open", suspended: true, reason: "Security hold"},
		"tenant overview, an unrecognised reason": {api: "open", suspended: true, reason: "zz_later_reason Unrecognised"},
		"tenant overview, never opened":           {api: "never"},
		"tenant overview, a refused open": {api: "closed", noticeTag: warnTag, notice: "Not opened — nothing changed. The database opens " +
			"the API under a code only when every rule below holds. The API section shows the business as it is now. The code is a " +
			"capital letter followed by 1 to 15 capital letters or digits. The business exists."},
		"tenant overview, closed": {api: "closed", noticeTag: okTag, notice: "Closed. The integration API is closed for this business: " +
			"its keys are refused from the next request and no webhook is sent. The code is kept."},
	}
	for rn, s := range states {
		body, ok := all[rn]
		if !ok {
			t.Fatalf("PREMISE: no render %q", rn)
		}
		column := columnOf(t, rn, body)
		open := regexp.MustCompile(`^<main [^>]*>`).FindString(column)
		inner := column[len(open) : len(column)-len("</main>")]
		kids := childElements(inner)

		// The column: its blocks, in order, and nothing else.
		want := []string{overviewCard, overviewDocket}
		if s.noticeTag != "" {
			want = append(want, s.noticeTag)
		}
		want = append(want, apiCard, suspensionCard, overviewBack[:strings.Index(overviewBack, "<a ")])
		var got []string
		for _, k := range kids {
			got = append(got, openingTag(k))
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s: the column's blocks are %q, want %q", rn, got, want)
		}
		if kids[len(kids)-1] != overviewBack {
			t.Errorf("%s: the back links are %s, want %s", rn, kids[len(kids)-1], overviewBack)
		}
		facts, docket, api, sus := kids[0], kids[1], kids[len(kids)-3], kids[len(kids)-2]

		// No top margin on a card's or a form's children.
		for _, card := range []string{facts, api, sus} {
			for _, child := range childElements(innerOf(card)) {
				checkNoTopMargin(t, rn, child)
				if strings.HasPrefix(child, "<form") {
					for _, fc := range childElements(innerOf(child)) {
						checkNoTopMargin(t, rn, fc)
					}
				}
			}
		}

		// The words, in order.
		words := []string{"Tenant overview", "Tenant id", id, "Signed up 2026-07-05 09:30 UTC", "Plan founding", "Business type restaurant",
			"Live now", "Locations 9", "Active employees 41", "Active plaques 9", "Active panel accounts 2", overviewNoteWords,
			"See this tenant's plaques", "See this tenant's billing", "See this tenant's VAT number"}
		if s.notice != "" {
			words = append(words, s.notice)
		}
		words = append(words, "Integration API")
		switch s.api {
		case "open":
			words = append(words, "Open Since 2026-10-16 09:30 UTC API code KF", apiOpenWords, "API code", apiHelpWords, "Change the code", "Close the API")
		case "closed":
			words = append(words, "Closed API code KF", apiClosedWords, "API code", apiHelpWords, "Open the API")
		default:
			words = append(words, "Closed No API code", apiNeverWords, "API code", apiHelpWords, "Open the API")
		}
		words = append(words, "Panel writes")
		if s.suspended {
			words = append(words, "Suspended", suspendedWords, "Suspended since 2026-10-08 09:30 UTC", "Reason "+s.reason,
				"Lift the suspension", reinstateP, boxWords, "Lift the suspension")
		} else {
			words = append(words, "Active", activeWords, "Suspend this tenant's writes", suspendP1, suspendP2, "Reason",
				"Non-payment Breach of the terms Security hold At the business's request", holdHeading)
			words = append(words, holdSentences...)
			words = append(words, boxWords, "Suspend writes")
		}
		words = append(words, "Back to the tenants Back to the console")
		if got, want := spaced(html.UnescapeString(noticeTag.ReplaceAllString(inner, " "))), spaced(strings.Join(words, " ")); got != want {
			t.Errorf("%s: the column says\n%q\nwant\n%q", rn, got, want)
		}

		// The facts card: its h1 and four facts, the id and the time wide.
		if !strings.HasPrefix(facts, overviewCard+overviewH1+overviewFacts+overviewWideFact) || strings.Count(facts, overviewWideFact) != 2 ||
			!strings.Contains(facts, overviewWideFact+`<dt class="docket-label">Signed up</dt>`) || strings.Count(facts, "<div") != 4 {
			t.Errorf("%s: the facts card is not the h1 over the facts, the id and the time wide: %s", rn, facts)
		}
		if strings.Count(body, "<h1") != 1 {
			t.Errorf("%s: %d h1, want the facts card's one", rn, strings.Count(body, "<h1"))
		}
		// The docket: its heading, the counts, the note, the links.
		if !strings.HasPrefix(docket, overviewDocket+overviewLive+overviewCounts) || !strings.Contains(docket, "</dl>"+overviewNote) ||
			!strings.Contains(docket, "</p>"+overviewSee+`<a href="`+path+`/plaques" class="op-link">`) {
			t.Errorf("%s: the docket is not heading, counts, note, links: %s", rn, docket)
		}

		// The API card.
		openForm := `<form method="post" action="` + path + `/integration"` + apiFormClass
		closeForm := `<form method="post" action="` + path + `/integration/close"` + apiCloseRest
		if !strings.HasPrefix(api, apiCard+apiHeading+apiState) || strings.Count(api, apiTextP) != 1 || strings.Count(api, openForm) != 1 {
			t.Errorf("%s: the API card's heading, state line, sentence or open form's tag is not as pinned: %s", rn, api)
		}
		button := "Open the API"
		if s.api == "open" {
			button = "Change the code"
		}
		of := api[strings.Index(api, openForm):]
		of = of[:strings.Index(of, "</form>")+len("</form>")]
		fkids := childElements(innerOf(of))
		if len(fkids) != 4 || fkids[0] != apiLabel || !strings.HasPrefix(fkids[1], "<input ") || fkids[2] != apiHelp ||
			fkids[3] != apiButton+button+"</button>" {
			t.Errorf("%s: the open form's children are %q, want the label, the box, the help and %s%s</button>", rn, fkids, apiButton, button)
		}
		if (s.api == "open") != strings.Contains(api, closeForm) || strings.Count(api, "<form") != 1+btoi(s.api == "open") {
			t.Errorf("%s: the close form %s is not there exactly when the API is open", rn, closeForm)
		}

		// The Panel writes card.
		action, h3, btn := "/suspend", suspendH3, suspendButton
		chip := chipActive
		if s.suspended {
			action, h3, btn, chip = "/reinstate", reinstateH3, reinstateButton, chipSuspended
		}
		sform := `<form method="post" action="` + path + action + `"` + suspensionFormClass
		if !strings.HasPrefix(sus, suspensionCard+suspensionHeading+"<p>"+chip+"</p>"+apiTextP) || strings.Count(sus, "<form") != 1 ||
			!strings.Contains(sus, sform+h3) || strings.Count(sus, confirmBox) != 1 || !strings.HasSuffix(sus, btn+"</form></section>") ||
			strings.Count(sus, "<button") != 1 {
			t.Errorf("%s: the Panel writes card is not heading, chip, sentence, [facts,] one form %s%s … %s: %s", rn, sform, h3, btn, sus)
		}
		if s.suspended != strings.Contains(sus, suspensionFacts) || strings.Count(sus, "<dl") != btoi(s.suspended) {
			t.Errorf("%s: the suspension's facts list %s is not there exactly when suspended", rn, suspensionFacts)
		}

		// Every input's attribute set, and the forms of the page.
		wantInputs := []map[string]string{{"id": "tenant-api-code", "type": "text", "name": "api_code", "value": map[bool]string{true: "", false: "KF"}[s.api == "never"],
			"maxlength": "16", "required": "", "autocomplete": "off", "spellcheck": "false"}}
		if !s.suspended {
			for _, r := range []string{"non_payment", "terms_breach", "security_hold", "customer_request"} {
				in := map[string]string{"id": "suspend-reason-" + r, "type": "radio", "name": "reason", "value": r, "required": ""}
				if r == "security_hold" {
					in["aria-describedby"] = "suspend-hold-warning"
				}
				wantInputs = append(wantInputs, in)
			}
		}
		wantInputs = append(wantInputs, map[string]string{"type": "checkbox", "name": "confirm", "value": "yes", "required": ""})
		inputs := inputTag.FindAllString(column, -1)
		if len(inputs) != len(wantInputs) {
			t.Fatalf("%s: %d input(s) in the column, want %d", rn, len(inputs), len(wantInputs))
		}
		for i, in := range inputs {
			if got := attributesOf(in); !maps.Equal(got, wantInputs[i]) {
				t.Errorf("%s: input %d's attributes are %v, want exactly %v", rn, i, got, wantInputs[i])
			}
		}
		if !strings.Contains(inputs[0], ` class="op-input font-mono"`) {
			t.Errorf("%s: the API code box is not a mono op-input: %s", rn, inputs[0])
		}
		if n := strings.Count(column, "<form"); n != 2+btoi(s.api == "open") {
			t.Errorf("%s: %d form(s) in the column, want the open form, [the close form,] one suspension form", rn, n)
		}

		// The anchors.
		links := []string{`<a href="` + path + `/plaques" class="op-link">See this tenant's plaques</a>`,
			`<a href="` + path + `/billing" class="op-link">See this tenant's billing</a>`,
			`<a href="` + path + `/vat" class="op-link">See this tenant's VAT number</a>`,
			`<a href="/operator/tenants" class="op-link">Back to the tenants</a>`, `<a href="/operator" class="op-link">Back to the console</a>`}
		if got := anchorTag.FindAllString(column, -1); !slices.Equal(got, links) {
			t.Errorf("%s: the column's anchors are %q, want %q", rn, got, links)
		}

		// Nothing hidden, nothing preselected, nothing the overview does not have.
		for _, banned := range []string{"hidden", "sr-only", "formaction", "formmethod", "<select", "<textarea", "<svg", "<img",
			"<script", "@", "MT1"} {
			if strings.Contains(column, banned) {
				t.Errorf("%s: the column carries %q", rn, banned)
			}
		}
		if m := checkedAttr.FindString(column); m != "" {
			t.Errorf("%s: %s is preselected; no reason is chosen for the operator", rn, m)
		}
	}
}

// btoi is 1 for true.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// voidElement is an HTML element with no closing tag.
var voidElement = map[string]bool{"input": true, "br": true, "img": true, "hr": true, "meta": true, "link": true, "source": true, "wbr": true}

// checkedAttr is a tag carrying the checked attribute.
var checkedAttr = regexp.MustCompile(`<[^>]*\schecked\b[^>]*>`)

var anyTag = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)\b[^>]*>`)

// childElements is a fragment's top-level elements, each from its opening tag to its close (a
// void element is its tag alone); the text between them is dropped.
func childElements(fragment string) []string {
	var out []string
	depth, start := 0, -1
	for _, m := range anyTag.FindAllStringSubmatchIndex(fragment, -1) {
		closing, name := fragment[m[2]:m[3]] == "/", strings.ToLower(fragment[m[4]:m[5]])
		switch {
		case closing:
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, fragment[start:m[1]])
				start = -1
			}
		case voidElement[name]:
			if depth == 0 {
				out = append(out, fragment[m[0]:m[1]])
			}
		default:
			if depth == 0 {
				start = m[0]
			}
			depth++
		}
	}
	return out
}

// openingTag is an element's opening tag; innerOf its content between that tag and its close.
func openingTag(el string) string { return el[:strings.Index(el, ">")+1] }

func innerOf(el string) string {
	return el[len(openingTag(el)):strings.LastIndex(el, "</")]
}

// checkNoTopMargin fails when an element's opening tag carries a top-margin utility at any width.
func checkNoTopMargin(t *testing.T, render, el string) {
	t.Helper()
	if m := classAttr.FindStringSubmatch(openingTag(el)); m != nil {
		for _, c := range strings.Fields(m[1]) {
			if topMargin.MatchString(c) {
				t.Errorf("%s: %q carries a top margin; the card spaces its blocks", render, openingTag(el))
			}
		}
	}
}

// oneTenantH1Findings is the one-tenant screens' heading rule (M13-08, orchestrator decision (c)):
// a one-tenant page carries exactly one h1, inside <main>, and not in the tenant banner -- the
// banner's name is the shell's, not the page's heading.
func oneTenantH1Findings(render, body string) []string {
	var out []string
	main := strings.Index(body, "<main")
	h1 := strings.Index(body, "<h1")
	banner := strings.Index(body, `<section class="op-tenant"`)
	bannerEnd := -1
	if banner >= 0 {
		if e := strings.Index(body[banner:], "</section>"); e >= 0 {
			bannerEnd = banner + e
		}
	}
	switch {
	case strings.Count(body, "<h1") != 1:
		out = append(out, fmt.Sprintf("%s: %d h1, want one", render, strings.Count(body, "<h1")))
	case main < 0 || h1 < main || h1 > strings.Index(body, "</main>"):
		out = append(out, render+": the h1 is not inside <main>")
	case banner >= 0 && h1 > banner && h1 < bannerEnd:
		out = append(out, render+": the h1 is in the tenant banner")
	}
	if banner < 0 || bannerEnd < 0 || banner > main {
		out = append(out, render+": PREMISE: no tenant banner before <main>")
	}
	return out
}

// TestTenantScreens_EachHasOneH1InTheMainAndNoneInTheBanner (M13-08; README §2 annex (c),
// screens.md §6 "The overview's <h1>"): every render of the four one-tenant screens in screens()
// -- the overview's, the plaques', the billing's and the VAT screen's fixtures, eighteen -- carries
// exactly one h1, inside <main>, outside the tenant banner (<section class="op-tenant" … </section>);
// the overview's is <h1 class="docket-label">Tenant overview</h1>. CONTROLS, each read by the same
// function and each a finding: the overview as it was before M13-08 (its label a paragraph, no
// h1); the banner's name made an h1 (two, one in the banner); the page's h1 moved into the banner.
//
// PART II -- red on any of the above. PART III -- the bare "tenant screen" fixture (TenantScreen
// with no page in it) is the shell's, not a screen, and is not read; the h1's look is each
// screen's own test's -- no completeness claim.
func TestTenantScreens_EachHasOneH1InTheMainAndNoneInTheBanner(t *testing.T) {
	all := screens(t)
	n := 0
	for render, body := range all {
		if !strings.HasPrefix(render, "tenant overview") && !strings.HasPrefix(render, "tenant plaques") &&
			!strings.HasPrefix(render, "tenant billing") && !strings.HasPrefix(render, "tenant VAT") {
			continue
		}
		n++
		for _, f := range oneTenantH1Findings(render, body) {
			t.Error(f)
		}
		if strings.HasPrefix(render, "tenant overview") && strings.Count(body, overviewH1) != 1 {
			t.Errorf("%s: the h1 is not %s", render, overviewH1)
		}
	}
	if n != 18 {
		t.Fatalf("PREMISE: %d one-tenant render(s), the comment says 18", n)
	}
	ov := all["tenant overview"]
	const bannerName = `<p class="font-display text-xl font-bold tracking-tight"><bdi>Kebab Factory Ltd</bdi></p>`
	if !strings.Contains(ov, bannerName) {
		t.Fatalf("PREMISE: the overview's banner does not name the tenant as %s", bannerName)
	}
	for _, c := range []struct{ what, from, to string }{
		{"the overview before M13-08", overviewH1, `<p class="docket-label">Tenant overview</p>`},
		{"the banner's name an h1", bannerName, `<h1 class="font-display text-xl font-bold tracking-tight"><bdi>Kebab Factory Ltd</bdi></h1>`},
		{"the h1 moved into the banner", overviewH1, ""},
	} {
		mutated := strings.Replace(ov, c.from, c.to, 1)
		if c.what == "the h1 moved into the banner" {
			mutated = strings.Replace(mutated, bannerName, bannerName+overviewH1, 1)
		}
		if mutated == ov {
			t.Fatalf("CONTROL %s: nothing changed", c.what)
		}
		if len(oneTenantH1Findings("control", mutated)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// overviewCountRules is the overview's one scoped rule in web/static/css/input.css (M13-08): the
// "Live now" figures at the boards' 30 / 32 / 34 px with a line height of 1.1, set from the
// docket's heading id because the figure's markup is pinned.
var overviewCountRules = map[string][]string{
	`[aria-labelledby="tenant-live"] dd`: {"leading-[1.1]", "md:text-[32px]", "desk:text-[34px]"},
}

// TestTenantOverviewCounts_TheScopedRuleKeepsTheBoardsSizes (M13-08; screens.md §6, boards
// OpTenant, T-OpTenant, M-OpTenant): in web/static/css/input.css (comments stripped, read as flat
// `selectors { @apply …; }` rules), every rule whose selector names the overview's "Live now" id
// is overviewCountRules' one, exactly once, one @apply of exactly its utilities. CONTROLS, each
// read by the same function and each a finding: the rule deleted; the desktop's 34 px dropped; the
// tablet's 32 px changed; a second rule on the docket, in the double-quoted, the single-quoted and
// the unquoted spelling.
//
// PART II -- red on any of the above. PART III -- that the scoped selector outranks the figure's
// own size class is the cascade's (a class and a type selector against one class), measured by
// the screenshot round; a utility added to the figure in the template is
// TestTenantOverview_NamesTheTenantInTheBannerAndRefusesABadPath's (its count regex) -- no
// completeness claim.
func TestTenantOverviewCounts_TheScopedRuleKeepsTheBoardsSizes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, f := range readScopedRules(css, "tenant-live", overviewCountRules) {
		t.Error(f)
	}
	const rule = `[aria-labelledby="tenant-live"] dd {`
	for _, c := range []struct{ what, from, to string }{
		{"the rule deleted", rule, `.m1308-control-deleted {`},
		{"the desktop's size dropped", "md:text-[32px] desk:text-[34px]", "md:text-[32px]"},
		{"the tablet's size changed", "md:text-[32px]", "md:text-[30px]"},
		{"a second rule", rule, `[aria-labelledby="tenant-live"] dt { @apply text-sm; }` + "\n  " + rule},
		{"a second rule, single-quoted", rule, `[aria-labelledby='tenant-live'] dd { @apply text-xl; }` + "\n  " + rule},
		{"a second rule, unquoted", rule, `[aria-labelledby=tenant-live] dd { @apply leading-none; }` + "\n  " + rule},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readScopedRules(mutated, "tenant-live", overviewCountRules)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// readScopedRules reads input.css's rules whose selector names id: one finding per way the source
// departs from want (each selector exactly once, one @apply of exactly its utilities, no other
// rule naming id in any spelling).
func readScopedRules(css, id string, want map[string][]string) []string {
	src := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	rule := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	apply := regexp.MustCompile(`^@apply ([^;]+);$`)
	found := map[string][][]string{}
	var findings []string
	for _, m := range rule.FindAllStringSubmatch(src, -1) {
		sel := strings.Join(strings.Fields(m[1]), " ")
		if !strings.Contains(sel, id) {
			continue
		}
		if _, known := want[sel]; !known {
			findings = append(findings, "a rule naming "+id+" that is not one of the pinned: "+sel)
			continue
		}
		a := apply.FindStringSubmatch(strings.TrimSpace(m[2]))
		if a == nil {
			findings = append(findings, "`"+sel+"` is not one @apply: "+strings.TrimSpace(m[2]))
			continue
		}
		found[sel] = append(found[sel], strings.Fields(a[1]))
	}
	for sel, w := range want {
		if len(found[sel]) != 1 {
			findings = append(findings, fmt.Sprintf("%d `%s { @apply … }` rule(s) in input.css, want 1", len(found[sel]), sel))
			continue
		}
		got, ww := slices.Clone(found[sel][0]), slices.Clone(w)
		slices.Sort(got)
		slices.Sort(ww)
		if !slices.Equal(got, ww) {
			findings = append(findings, fmt.Sprintf("`%s` applies %v, want exactly %v", sel, found[sel][0], w))
		}
	}
	return findings
}
