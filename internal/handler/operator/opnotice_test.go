package operator_test

// opnotice_test.go -- M13-03: the operator surface's own notice (operatorpages' opNotice) and
// its problem card. docs/plan/m13-operator-redesign.md, card M13-03; docs/design/operator-redesign
// README §6 "Operator notice", "Problem page"; screens.md §4.

import (
	"bytes"
	"context"
	"html"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// opNoticeBlock is one operator notice: its role, its tone and its contents up to the box's end.
// The contents hold no section of their own (no caller puts one there), so the first close is
// the box's.
var opNoticeBlock = regexp.MustCompile(`(?s)<section role="([a-z]+)" class="op-notice op-notice--([a-z]+)">(.*?)</section>`)

// opNoticeHeading is a notice's heading, when it has one: the paragraph that opens the box.
var opNoticeHeading = regexp.MustCompile(`^\s*<p class="font-display font-bold tracking-tight">([^<]*)</p>`)

// sharedNoticeFrame is components.Notice's frame (the tap and activation screens' box): the
// operator surface no longer draws it.
var sharedNoticeFrame = regexp.MustCompile(`class="border-l-4 border-(tappa-green|saffron|tomato) `)

// noticeOf is one notice as a reader meets it: the tone its frame draws, the heading it says and
// the text under the heading (tags dropped, entities decoded, white space collapsed). In a table of
// wanted notices, body is a part of that text the notice must say.
type noticeOf struct{ tone, heading, body string }

// noticeTag is any tag of a notice's body, read as a space so two paragraphs do not run together;
// spaceBeforeMark undoes the space a closing inline tag leaves before punctuation.
var (
	noticeTag       = regexp.MustCompile(`<[^>]*>`)
	spaceBeforeMark = regexp.MustCompile(` ([,.;:])`)
)

// noticesOn parses every operator notice of a render, checking its role against its tone.
func noticesOn(t *testing.T, name, body string) []noticeOf {
	t.Helper()
	var out []noticeOf
	for _, m := range opNoticeBlock.FindAllStringSubmatch(body, -1) {
		role, tone := m[1], m[2]
		if want := map[string]string{"ok": "status", "warn": "status", "alert": "alert"}[tone]; want == "" || role != want {
			t.Errorf("%s: a notice of tone %q has role %q, want %q", name, tone, role, want)
		}
		heading, rest := "", m[3]
		if h := opNoticeHeading.FindStringSubmatch(rest); h != nil {
			heading, rest = html.UnescapeString(h[1]), rest[len(h[0]):]
		}
		out = append(out, noticeOf{tone, heading, spaceBeforeMark.ReplaceAllString(spaced(html.UnescapeString(noticeTag.ReplaceAllString(rest, " "))), "$1")})
	}
	return out
}

// TestOperatorNotice_EveryCallerKeepsItsToneAndHeadingInTheOperatorsOwnBox (M13-03): every
// operator screen that says something in a notice says it in opNotice -- the operator's box, a
// section with the tone's role and class -- with the tone and the heading it had in
// components.Notice before the port, and no operator render draws components.Notice's frame.
//
// PART I -- the 49 renders of screens() and eight more (the API card's two answers, the legal
// screen behind its page, the enrollment's three refusals one at a time, the code step's two):
// each render carries exactly the notices the table below names, in order, tone by tone and
// heading by heading, each saying the part of its body sentence the table names -- the whole of a
// fixed sentence the template writes, the fixture's words where the view supplies them
// (screens.md §1, §2, §4, §6, §9, §11, §12 for the words); every render not
// in the table carries none; an ok or warn notice is role status and an alert role alert; the
// count of "op-notice" in a render is the count of parsed notices (no notice in another shape);
// the alerts of a render are its alert-toned notices, at most one -- except the screens()
// fixture that turns the enrollment's three exclusive refusals on at once ("enroll, re-render";
// enroll.go picks one, as the single-flag renders show); no render carries components.Notice's
// frame. CONTROL: the frame pattern finds components.Notice's own render (it is not blind), and
// the parser finds a notice in a render whose tone and heading the table names.
//
// PART II -- red on: a caller back on components.Notice, a tone changed, a heading changed, a
// fixed body sentence changed, a role on the wrong tone, a notice added to or dropped from a
// screen, a second alert on a page.
// PART III -- the sentences a handler puts in a view (holdings, the API card, VAT) are the
// handlers' tests' (headingsOn, apiHeadingsOn and the VAT screen's); the problem card's body is
// TestProblemPage_IsOneAlertCardWhoseHeadingIsThePagesOnlyH1's; the hold warning's every sentence is
// TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice's (here its first); whether a screen
// reader announces the box is a browser's, not measured here -- no completeness claim.
func TestOperatorNotice_EveryCallerKeepsItsToneAndHeadingInTheOperatorsOwnBox(t *testing.T) {
	renders := screens(t)
	name, err := operatorpages.NewTenantName("Kebab Factory Ltd")
	if err != nil {
		t.Fatal(err)
	}
	path := "/operator/tenants/10000000-0000-4000-8000-000000000001"
	overviewWith := func(api operatorpages.TenantAPIView) templ.Component {
		api.OpenPath, api.ClosePath = path+"/integration", path+"/integration/close"
		return operatorpages.TenantOverview(operatorpages.TenantOverviewView{Name: name, ID: path[len("/operator/tenants/"):],
			CreatedAt: "2026-07-05 09:30 UTC", Plan: "founding", PlaquesPath: path + "/plaques", BillingPath: path + "/billing",
			VATPath: path + "/vat", Tabs: fixtureTabs, API: api,
			Suspension: operatorpages.TenantSuspensionView{SuspendPath: path + "/suspend", ReinstatePath: path + "/reinstate",
				Reasons: []operatorpages.SuspensionReasonOption{{Value: "security_hold", Label: "Security hold", Hold: true}}}})
	}
	for k, c := range map[string]templ.Component{
		"overview, API opened": overviewWith(operatorpages.TenantAPIView{Open: true, OpenedAt: "2026-10-16 09:30 UTC", Code: "KF",
			NoticeTone: operatorpages.HoldingNoticeOK, NoticeHeading: "Opened.", Notice: "N"}),
		"overview, API code refused": overviewWith(operatorpages.TenantAPIView{NoticeTone: operatorpages.HoldingNoticeWarn,
			NoticeHeading: "That code cannot be used", Notice: "N", NoticeRules: []string{"R1", "R2"}}),
		"legal, behind": operatorpages.Legal(operatorpages.LegalView{Limit: 1, Docs: []operatorpages.LegalDoc{{Slug: "privacy",
			Path: "/legal/privacy", Published: true, PublishedAt: "2026-10-03 09:30 UTC", Body: "T", Behind: true,
			LiveAt: "2026-10-03 09:31 UTC"}}}),
		"enroll, mismatch": operatorpages.Enroll(operatorpages.EnrollView{AccountID: "x", Key: "ABCD", URI: "otpauth://totp/x", Blob: "b",
			Mismatch: true}),
		"enroll, weak password": operatorpages.Enroll(operatorpages.EnrollView{AccountID: "x", Key: "ABCD", URI: "otpauth://totp/x",
			Blob: "b", WeakPassword: true}),
		"enroll, code refused": operatorpages.Enroll(operatorpages.EnrollView{AccountID: "x", Key: "ABCD", URI: "otpauth://totp/x",
			Blob: "b", CodeRejected: true}),
	} {
		var b bytes.Buffer
		if err := c.Render(context.Background(), &b); err != nil {
			t.Fatalf("render %s: %v", k, err)
		}
		renders[k] = b.String()
	}
	const hold = "A security hold freezes writes. It does not contain an account."
	const holdFirst = "Whoever holds a session of this business keeps it. While the business is suspended, that session still:"
	const enrollUseTheKey = "Type a code from the authenticator entry this page gave you. If you did not add it, open your link again for a new key."
	const enrollMismatch = "Type the same new password in both fields."
	const enrollWeak = "Use at least 14 characters and at most 72 bytes. A letter with an accent, or from another alphabet, takes two bytes or more."
	const clock = "Check that your phone's clock is right, then type the next code."
	want := map[string][]noticeOf{
		"sign-in, refused": {{"alert", "We could not sign you in", "Check the email address and the password, then try again."}},
		"sign-in, expired": {{"warn", "That sign-in step expired", "Sign in again with your email address and password."}},
		"code, rejected":   {{"alert", "That code was not accepted", clock}},
		"code, locked":     {{"alert", "Sign-in is locked for a while", "Too many wrong codes. Wait a quarter of an hour, then try again."}},
		"enroll, re-render": {{"warn", "Use the key you already added", enrollUseTheKey},
			{"alert", "The two passwords differ", enrollMismatch}, {"alert", "Choose another password", enrollWeak},
			{"alert", "That code was not accepted", clock}},
		"enroll, mismatch":           {{"alert", "The two passwords differ", enrollMismatch}},
		"enroll, weak password":      {{"alert", "Choose another password", enrollWeak}},
		"enroll, code refused":       {{"alert", "That code was not accepted", clock}},
		"problem":                    {{"alert", "", "T M"}},
		"problem, signed in":         {{"alert", "", "T M"}},
		"tenant overview":            {{"warn", hold, holdFirst}},
		"tenant overview, unnamed":   {{"warn", hold, holdFirst}},
		"overview, API opened":       {{"ok", "Opened.", "N"}, {"warn", hold, holdFirst}},
		"overview, API code refused": {{"warn", "That code cannot be used", "N R1 R2"}, {"warn", hold, holdFirst}},
		"legal, behind": {{"warn", "The public page is behind", "The newest version, published 2026-10-03 09:31 UTC, has not " +
			"reached the public page yet. This editor opened on the text the page shows, so publishing it replaces the newest " +
			"version. Reload this screen to try again. The page also catches up after the next publication or a restart."}},
		"tenant VAT, a notice": {{"warn", "VIES did not answer", "N"}},
		"holdings, added":      {{"ok", "Added.", "N"}},
		"holdings, not added":  {{"warn", "Not added — nothing changed.", "N R1 R2"}},
	}
	if len(renders) != 49+6 {
		t.Fatalf("PREMISE: %d renders, want 55", len(renders))
	}
	for k := range want {
		if _, ok := renders[k]; !ok {
			t.Fatalf("PREMISE: the table names %q, which is not rendered", k)
		}
	}
	tones := map[string]int{}
	for k, body := range renders {
		got := noticesOn(t, k, body)
		if !equalNotices(got, want[k]) {
			t.Errorf("%s: notices %v, want %v", k, got, want[k])
		}
		if n := strings.Count(body, "op-notice "); n != len(got) {
			t.Errorf("%s: %d op-notice boxes, %d of them in the notice's shape", k, n, len(got))
		}
		alerts := 0
		for _, n := range got {
			tones[n.tone]++
			if n.tone == "alert" {
				alerts++
			}
		}
		if n := strings.Count(body, `role="alert"`); n != alerts || (n > 1 && k != "enroll, re-render") {
			t.Errorf("%s: %d alert(s) on the page, %d of them notices; want at most one, a notice", k, n, alerts)
		}
		if sharedNoticeFrame.MatchString(body) {
			t.Errorf("%s: carries components.Notice's frame", k)
		}
	}
	if tones["ok"] == 0 || tones["warn"] == 0 || tones["alert"] == 0 {
		t.Fatalf("PREMISE: the renders carried tones %v; each of the three must be met", tones)
	}
	var shared bytes.Buffer
	ctx := templ.WithChildren(context.Background(), templ.Raw("<p>x</p>"))
	if err := components.Notice(components.ToneWarn, "x").Render(ctx, &shared); err != nil {
		t.Fatal(err)
	}
	if !sharedNoticeFrame.MatchString(shared.String()) {
		t.Errorf("CONTROL: the frame pattern does not find components.Notice's own render %q; the check above is blind", shared.String())
	}
}

// equalNotices says whether the notices got are the notices want names: the same tones and
// headings in the same order, each body saying its wanted part.
func equalNotices(got, want []noticeOf) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].tone != want[i].tone || got[i].heading != want[i].heading || !strings.Contains(got[i].body, want[i].body) {
			return false
		}
	}
	return true
}

// TestProblemPage_IsOneAlertCardWhoseHeadingIsThePagesOnlyH1 (M13-03; screens.md §4): every
// problem constant (problemPages(), the six of the suspension included) renders as one card -- an
// op-problem wrapper holding the alert-toned operator notice, whose body is the page's one h1,
// the constant's title, then its message, word for word -- and under it, inside the wrapper, the
// constant's way on when it has one (an op-link to Back saying BackLabel) and no op-link when it
// has none. Signed in, the frame's nav is there and marks nothing (no aria-current anywhere);
// signed out there is no nav. No page carries a tenant banner, a sub-nav or a chip (the view has
// no state to show -- D1), and none carries a second alert.
//
// PART II -- red on: the title out of the alert card or not the h1, a second h1, the card not an
// alert, a changed title, message or link, a nav current item on a problem page, a nav on a
// signed-out one, a chip or a banner. PART III -- the constants' words and links are also
// TestProblemPages_LinkOnlyToMountedRoutes's and TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo's;
// the look is the screenshot's -- no completeness claim.
func TestProblemPage_IsOneAlertCardWhoseHeadingIsThePagesOnlyH1(t *testing.T) {
	pages := operator.ProblemPagesForTest()
	var signedIn, signedOut, withBack, withoutBack int
	for i, v := range pages {
		var b bytes.Buffer
		if err := operatorpages.Problem(v).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		body := b.String()
		card := regexp.MustCompile(`(?s)<div class="op-problem"><section role="alert" class="op-notice op-notice--alert"><div class="text-sm text-ink/85">` +
			`<h1 class="op-problem-title">` + regexp.QuoteMeta(html.EscapeString(v.Title)) + `</h1>` +
			`<p class="mt-2.5 text-\[15px\] leading-normal">` + regexp.QuoteMeta(html.EscapeString(v.Message)) + `</p></div></section>(.*?)</div></main>`)
		m := card.FindStringSubmatch(body)
		if m == nil {
			t.Errorf("problem %d (%q): not one alert card holding the title as its h1 and then the message", i, v.Title)
			continue
		}
		back := `<p class="text-[15px]"><a href="` + html.EscapeString(v.Back) + `" class="op-link">` + html.EscapeString(v.BackLabel) + `</a></p>`
		if v.Back != "" && m[1] != back {
			t.Errorf("problem %d (%q): the way on is %q, want %q", i, v.Title, m[1], back)
		}
		if v.Back == "" && m[1] != "" {
			t.Errorf("problem %d (%q): has no way on but the card is followed by %q", i, v.Title, m[1])
		}
		if v.Back == "" && strings.Contains(body, "op-link") {
			t.Errorf("problem %d (%q): has no way on but carries a link", i, v.Title)
		}
		if strings.Count(body, "<h1") != 1 || strings.Count(body, `role="alert"`) != 1 {
			t.Errorf("problem %d (%q): %d h1, %d alert(s); want one each", i, v.Title, strings.Count(body, "<h1"), strings.Count(body, `role="alert"`))
		}
		for _, banned := range []string{"aria-current", "op-tenant", "op-subnav", `class="tally`} {
			if strings.Contains(body, banned) {
				t.Errorf("problem %d (%q): carries %s", i, v.Title, banned)
			}
		}
		if nav := strings.Count(body, `<nav class="op-nav"`); (v.SignedIn && nav != 1) || (!v.SignedIn && nav != 0) {
			t.Errorf("problem %d (%q): signed in %v with %d nav(s)", i, v.Title, v.SignedIn, nav)
		}
		if v.SignedIn {
			signedIn++
		} else {
			signedOut++
		}
		if v.Back != "" {
			withBack++
		} else {
			withoutBack++
		}
	}
	if len(pages) < 40 || signedIn == 0 || signedOut == 0 || withBack == 0 || withoutBack == 0 {
		t.Fatalf("PREMISE: %d pages, %d signed in, %d signed out, %d with a way on, %d without", len(pages), signedIn, signedOut,
			withBack, withoutBack)
	}
}

// opNoticeRule is one tone rule of the operator notice in input.css: its @apply list.
var opNoticeRule = regexp.MustCompile(`(?m)^\s*\.op-notice--(ok|warn|alert) \{ @apply ([^;]*); \}$`)

// noticeGroundsBelowAA is the AA failures of the notice's words on the tone rules' grounds: the
// heading ink and the body at alpha (the template's ink tone) on each rule's ground, a tint
// composited over paper and over porcelain (the two grounds a notice sits on). A rule without a
// ground of the palette is a failure too: it cannot be computed.
func noticeGroundsBelowAA(t *testing.T, rules map[string]string, body float64) []string {
	t.Helper()
	ink, paper, porcelain := rgbOf(t, paletteHex(t, "ink")), rgbOf(t, paletteHex(t, "paper")), rgbOf(t, paletteHex(t, "porcelain"))
	over := func(fg [3]float64, alpha float64, bg [3]float64) [3]float64 {
		var c [3]float64
		for i := range c {
			c[i] = alpha*fg[i] + (1-alpha)*bg[i]
		}
		return c
	}
	var bad []string
	for tone, apply := range rules {
		g := regexp.MustCompile(`(?:^| )bg-([a-z-]+)(?:/(\d+))?(?: |$)`).FindStringSubmatch(apply)
		if g == nil {
			bad = append(bad, tone+": no ground")
			continue
		}
		alpha, label := 1.0, "bg-"+g[1]
		if g[2] != "" {
			label += "/" + g[2]
			n, err := strconv.Atoi(g[2])
			if err != nil {
				t.Fatal(err)
			}
			alpha = float64(n) / 100
		}
		for under, page := range map[string][3]float64{"paper": paper, "porcelain": porcelain} {
			ground := over(rgbOf(t, paletteHex(t, g[1])), alpha, page)
			for what, fg := range map[string][3]float64{"heading ink": ink, "body ink": over(ink, body, ground)} {
				if c := contrast(fg, ground); c < 4.5 {
					bad = append(bad, tone+": "+what+" over "+under+" = "+strconv.FormatFloat(c, 'f', 2, 64)+":1")
				} else {
					t.Logf("%s: %s on %s over %s = %.2f:1", tone, what, label, under, c)
				}
			}
		}
	}
	return bad
}

// TestOperatorNotice_TheWordsClearAAOnEveryTone (M13-03): the notice's heading (ink) and its body
// (the ink tone notice.templ writes on the body) clear AA's 4.5:1 on the ground of each of
// input.css's three tone rules -- read from the stylesheet, not restated: the ok rule's ground is a
// tint (tappa-green at 10 %), so it is composited over paper and over porcelain, the two grounds a
// notice sits on (the holdings' and the API card's answers sit on the page; the in-card ones and
// the problem card on paper). CONTROL: the same computation on a rule whose ground is the green
// itself, opaque, is a failure -- the check can fail.
//
// PART II -- red on: a tone rule whose ground fails AA under the words, a rule missing or without a
// palette ground, the body's tone not ink/NN. PART III -- the frames are no text and are not
// computed; the problem title is ink on paper, as the heading -- no completeness claim.
func TestOperatorNotice_TheWordsClearAAOnEveryTone(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "templates", "operatorpages", "notice.templ"))
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]string{}
	for _, m := range opNoticeRule.FindAllStringSubmatch(string(css), -1) {
		if _, dup := rules[m[1]]; dup {
			t.Errorf("input.css has two .op-notice--%s rules", m[1])
		}
		rules[m[1]] = m[2]
	}
	if len(rules) != 3 {
		t.Fatalf("input.css has the tone rules %v, want ok, warn and alert", rules)
	}
	bodies := regexp.MustCompile(`<div class="(?:mt-1 )?text-sm text-ink/(\d+)">`).FindAllStringSubmatch(string(src), -1)
	if len(bodies) != 2 || bodies[0][1] != bodies[1][1] {
		t.Fatalf("notice.templ's body tones %v, want the same ink tone on its two bodies", bodies)
	}
	n, err := strconv.Atoi(bodies[0][1])
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range noticeGroundsBelowAA(t, rules, float64(n)/100) {
		t.Error(f)
	}
	if bad := noticeGroundsBelowAA(t, map[string]string{"control": "border-tappa-green bg-tappa-green"}, float64(n)/100); len(bad) == 0 {
		t.Error("CONTROL: ink on opaque tappa-green passed; the computation cannot fail")
	}
}

// noticeColour is a palette colour a utility of a notice's tone rule names: border-x, bg-x or
// text-x with x a token of tailwind.config.js's palette (a width such as border-2 names none).
var noticeColour = regexp.MustCompile(`(?:^| )(border|bg|text)-(ink|porcelain|paper|tappa-green|green-lite|saffron-lite|saffron|tomato|line)(?:/\d+)?(?: |$)`)

// noticeToneColourFindings is what is wrong with the colours of the notice's tone rules against
// the brand's status mapping (skill tappa-brand: ok/APPROVED tappa-green, flag/FLAGGED saffron,
// reject/REJECTED tomato, never inverted): each rule has exactly one frame colour and one ground,
// the frame is its tone's colour and the ground is its tone's colour or tint -- paper, and only
// paper, for the alert, as README §6 draws it (orchestrator decision, 2026-10-26) --, and nothing
// colours the words (they stay ink, from .op-notice).
func noticeToneColourFindings(rules map[string]string) []string {
	frame := map[string]string{"ok": "tappa-green", "warn": "saffron", "alert": "tomato"}
	ground := map[string][]string{"ok": {"tappa-green", "green-lite"}, "warn": {"saffron", "saffron-lite"}, "alert": {"paper"}}
	var bad []string
	for _, tone := range []string{"ok", "warn", "alert"} {
		apply, ok := rules[tone]
		if !ok {
			bad = append(bad, tone+": no rule")
			continue
		}
		var frames, grounds []string
		for _, m := range noticeColour.FindAllStringSubmatch(strings.ReplaceAll(apply, " ", "  "), -1) {
			switch m[1] {
			case "border":
				frames = append(frames, m[2])
			case "bg":
				grounds = append(grounds, m[2])
			default:
				bad = append(bad, tone+": colours the words "+m[2])
			}
		}
		if len(frames) != 1 || frames[0] != frame[tone] {
			bad = append(bad, tone+": frame "+strings.Join(frames, ",")+", want "+frame[tone])
		}
		if len(grounds) != 1 || !slices.Contains(ground[tone], grounds[0]) {
			bad = append(bad, tone+": ground "+strings.Join(grounds, ",")+", want one of "+strings.Join(ground[tone], ","))
		}
	}
	return bad
}

// opNoticeSelectors is every selector of input.css that names the operator notice, as written
// there: the base frame, the three tone rules and the problem card's padding. A new one is a
// finding, because a selector more specific than a tone rule can re-colour it (a combined
// .op-notice.op-notice--alert, or a colour on the problem card's rule, measured in the audit).
var opNoticeSelectors = []string{".op-notice", ".op-notice--ok", ".op-notice--warn", ".op-notice--alert", ".op-problem > .op-notice"}

// opNoticeCompiled is the selectors of the compiled app.css that name the notice: Tailwind
// writes the tone rules' shared opacity variables under one grouped selector, and the problem
// card's padding twice (base and from the tablet) under the same selector.
var opNoticeCompiled = []string{".op-notice", ".op-notice--ok", ".op-notice--warn", ".op-notice--alert",
	".op-notice--alert,.op-notice--warn", ".op-problem>.op-notice"}

// cssRule is one innermost rule of a stylesheet: its selector and its body.
var cssRule = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)

// cssComment is a stylesheet comment, dropped before the rules are read.
var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// noticeRulesOf is the innermost rules of a stylesheet whose selector names op-notice, selector to
// bodies (a selector may occur more than once, as in the compiled sheet's media blocks).
func noticeRulesOf(css string) map[string][]string {
	out := map[string][]string{}
	for _, m := range cssRule.FindAllStringSubmatch(cssComment.ReplaceAllString(css, ""), -1) {
		sel := strings.Join(strings.Fields(m[1]), " ")
		if strings.Contains(sel, "op-notice") {
			out[sel] = append(out[sel], strings.TrimSpace(m[2]))
		}
	}
	return out
}

// anyColour is a utility that colours something with a palette token or any other colour:
// border-, bg-, text-, ring-, outline-, divide-, decoration-, shadow-, fill-, stroke-, accent-,
// caret- followed by a colour name (a width or a size such as border-2 or text-sm is not one).
var anyColour = regexp.MustCompile(`(?:^|\s)(?:[a-z-]+:)*(?:border|bg|text|ring|outline|divide|decoration|shadow|fill|stroke|accent|caret)-` +
	`(?:ink|porcelain|paper|tappa-green|green-lite|saffron-lite|saffron|tomato|line|white|black|transparent|current|inherit|\[#)`)

// noticeSelectorFindings is what is wrong with input.css's notice rules beyond the tone rules'
// own colours: a selector naming op-notice that is not one of opNoticeSelectors, one of them
// missing or written twice, and a colour on a rule that is not a tone rule -- the base frame may
// set the words' ink (text-ink) and nothing else, the problem card's rule nothing.
func noticeSelectorFindings(rules map[string][]string) []string {
	var bad []string
	for sel, bodies := range rules {
		if !slices.Contains(opNoticeSelectors, sel) {
			bad = append(bad, "a selector naming the notice that is not on the list: "+sel)
			continue
		}
		if len(bodies) != 1 {
			bad = append(bad, sel+": written "+strconv.Itoa(len(bodies))+" times")
		}
		if strings.HasPrefix(sel, ".op-notice--") {
			continue
		}
		for _, b := range bodies {
			b = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(b, "@apply")), ";"))
			body := "  " + strings.ReplaceAll(b, " ", "  ") + "  "
			if sel == ".op-notice" {
				body = strings.Replace(body, "  text-ink  ", "  ", 1)
			}
			if m := anyColour.FindString(body); m != "" {
				bad = append(bad, sel+": a colour on a rule that is not a tone rule:"+m)
			}
		}
	}
	for _, sel := range opNoticeSelectors {
		if _, ok := rules[sel]; !ok {
			bad = append(bad, "no rule "+sel)
		}
	}
	return bad
}

// TestOperatorNotice_EachToneRuleKeepsItsStatusColour (M13-03, audit): input.css's three tone
// rules of the operator notice keep the brand's status mapping -- ok framed in tappa-green on a
// green ground, warn in saffron on a saffron ground, alert in tomato on paper --, one frame colour
// and one ground each, nothing colouring the words; and nothing else can re-colour them: every
// selector of input.css naming op-notice is one of opNoticeSelectors, written once, and the two
// that are not tone rules carry no colour (the base its words' ink alone); when the compiled
// app.css is present (make css), its selectors naming op-notice are opNoticeCompiled exactly.
// CONTROL: the same readers find the ok rule painted saffron, the alert rule painted saffron, a
// combined .op-notice.op-notice--alert rule, and a saffron frame on the problem card's rule.
//
// PART II -- red on: a tone rule re-toned (the mapping inverted or crossed), a second frame or
// ground colour, a text colour, a rule missing, a new selector naming the notice in the source or
// the compiled sheet, a colour on the base or the problem card's rule. PART III -- the frame widths and the corners are
// the screenshot's; the words' contrast is TestOperatorNotice_TheWordsClearAAOnEveryTone's -- no
// completeness claim.
func TestOperatorNotice_EachToneRuleKeepsItsStatusColour(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]string{}
	for _, m := range opNoticeRule.FindAllStringSubmatch(string(css), -1) {
		if _, dup := rules[m[1]]; dup {
			t.Errorf("input.css has two .op-notice--%s rules", m[1])
		}
		rules[m[1]] = m[2]
	}
	for _, f := range noticeToneColourFindings(rules) {
		t.Error(f)
	}
	for what, mutate := range map[string][2]string{
		"ok painted saffron":    {"ok", "border-saffron bg-saffron-lite"},
		"alert painted saffron": {"alert", "border-2 border-saffron bg-paper"},
	} {
		control := maps.Clone(rules)
		control[mutate[0]] = mutate[1]
		if len(noticeToneColourFindings(control)) == 0 {
			t.Errorf("CONTROL: the rules with %s raise no finding; the reader is blind", what)
		}
	}
	source := noticeRulesOf(string(css))
	for _, f := range noticeSelectorFindings(source) {
		t.Error(f)
	}
	for what, extra := range map[string]string{
		"a combined alert rule":           "\n  .op-notice.op-notice--alert { @apply border-saffron; }\n",
		"a saffron problem card frame":    "\n  .op-problem > .op-notice { @apply border-saffron; }\n",
		"a second notice selector inside": "\n  .op-main .op-notice--ok { @apply border-tomato; }\n",
	} {
		control := noticeRulesOf(string(css) + extra)
		if what == "a saffron problem card frame" {
			control = noticeRulesOf(strings.Replace(string(css), ".op-problem > .op-notice { @apply ", ".op-problem > .op-notice { @apply border-saffron ", 1))
		}
		if len(noticeSelectorFindings(control)) == 0 {
			t.Errorf("CONTROL: %s raises no finding; the selector reader is blind", what)
		}
	}
	compiled, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "app.css"))
	if err != nil {
		t.Log("app.css not built (make css): the compiled selectors are not measured on this run")
		return
	}
	var got []string
	for sel := range noticeRulesOf(string(compiled)) {
		got = append(got, sel)
	}
	slices.Sort(got)
	want := slices.Clone(opNoticeCompiled)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("the compiled app.css names the notice in %q, want %q", got, want)
	}
}
