package operator_test

// opsignin_test.go -- M13-04: the sign-in and the code step in the redesign, and the signed-out
// column they share with the signed-out problem page. docs/plan/m13-operator-redesign.md, card
// M13-04; docs/design/operator-redesign README §6 "Signed-out card", screens.md §1.

import (
	"bytes"
	"context"
	"html"
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// signedOutMain is the signed-out column's opening tag (chrome.templ, screen(…, false, …)): the
// boards' padding per width, centred, 560 px wide counting the content alone.
const signedOutMain = `<main class="op-main mx-auto box-content flex max-w-[560px] flex-col gap-6 px-4 pt-6 pb-10 ` +
	`md:px-8 md:pt-16 md:pb-20 desk:px-6 desk:pt-[72px] desk:pb-24">`

// signInCard is the sign-in's and the code step's card: at most 480 px, centred in the column,
// its blocks spaced by the card (README §6 "Card": gap-3.5 on the markup, no mt-* in the bodies).
const signInCard = `<section class="op-card mx-auto flex w-full max-w-[480px] flex-col gap-3.5">`

// classAttr is every class attribute of a fragment; topMargin a top-margin utility at any width.
var (
	classAttr = regexp.MustCompile(`class="([^"]*)"`)
	topMargin = regexp.MustCompile(`^(?:[a-z]+:)*-?(?:mt|my|m)-`)
	inputTag  = regexp.MustCompile(`<input [^>]*>`)
	attribute = regexp.MustCompile(`\s([a-z-]+)(?:="([^"]*)")?`)
	anchorTag = regexp.MustCompile(`<a [^>]*>[^<]*</a>`)
)

// TestSignInScreens_AreOneCardInTheSignedOutColumn (M13-04; screens.md §1, README §6 "Signed-out
// card"): each of the six sign-in renders of screens() -- sign-in, refused, expired; code,
// rejected, locked -- is the signed-out column (signedOutMain) holding exactly one card
// (signInCard) and nothing else; inside the card, outside its notice (whose own heading and body
// are opNotice's, M13-03), no element carries a top margin (the card spaces its blocks); the
// card's words, tags dropped, are exactly the step's own (screens.md §1: the h1, the line under it,
// the state's notice heading and body, the field labels, the button, "Start again") in that order;
// the page has one h1, in the card, one form, posting to the step's route, one button, and its
// labels and button are the whole tags screens.md §1 names, in order; each input's attribute set,
// its class aside, is exactly the one screens.md §1 gives (id, name, type, autocomplete, inputmode,
// required, autofocus -- nothing more, so no value: the typed address is not written back, §1
// "Binding", and no hidden field exists); the sign-in has no link in the column and the code step
// exactly one, "Start again" to /operator/login; no render says "forgot", "reset", "remember",
// "sign up", "sign-up" or "register" (screens.md §1 "Not shown"). Every problem constant
// (problemPages()) signed out sits in the same column, its card (div.op-problem) the column's
// only child; signed in, the column is the frame's, the very opening tag of the console's main
// (the signed-in problem keeps its 640 px, M13-03).
//
// PART II -- red on: a card or column width or padding changed, a wrapper back around a notice
// or a top margin back in a body, a word of the card changed, added or dropped, a label or button
// changed, a second form, an input added, removed, given a value or an attribute added, dropped or
// changed (type, autocomplete, inputmode, required, autofocus), a
// recovery, sign-up or remember-me word, a link added, the signed-out column on a signed-in
// problem or a signed-in one on a signed-out problem. PART III -- the look at 320 / 390 / 834 /
// 1200 / 1440 px is the screenshot round's; the refused answers' byte identity is
// TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike's and
// TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes's -- no completeness claim.
func TestSignInScreens_AreOneCardInTheSignedOutColumn(t *testing.T) {
	all := screens(t)
	// fields is one input's whole attribute set (screens.md §1 "Form"), its class aside (the look
	// is the column and card checks' and the screenshot's): a bare attribute maps to "".
	type fields = map[string]string
	type step struct {
		action string
		inputs []fields
		// controls is the form's labels and button, each the whole tag with its words, in order.
		controls []string
		links    []string
		// head and tail are the card's words before and after its notice, tags dropped.
		head, tail string
	}
	signIn := step{action: "/operator/login",
		inputs: []fields{
			{"id": "op-email", "name": "email", "type": "email", "autocomplete": "username", "required": "", "autofocus": ""},
			{"id": "op-password", "name": "password", "type": "password", "autocomplete": "current-password", "required": ""}},
		controls: []string{`<label for="op-email" class="op-label">Email</label>`, `<label for="op-password" class="op-label">Password</label>`,
			`<button type="submit" class="btn btn--primary">Continue</button>`},
		head: "Operator sign-in Taptime staff only. This is not the business panel.",
		tail: "Email Password Continue"}
	code := step{action: "/operator/login/totp",
		inputs: []fields{{"id": "op-code", "name": "code", "type": "text", "inputmode": "numeric", "autocomplete": "one-time-code",
			"required": "", "autofocus": ""}},
		controls: []string{`<label for="op-code" class="op-label">Code</label>`, `<button type="submit" class="btn btn--primary">Sign in</button>`},
		links:    []string{`<a href="/operator/login" class="op-link">Start again</a>`},
		head:     "Enter your code Type the six-digit code your authenticator app shows for Taptime operator.",
		tail:     "Code Sign in Start again"}
	// notice is each state's notice words (screens.md §1), heading then body; the default states have none.
	notice := map[string]string{
		"sign-in, refused": "We could not sign you in Check the email address and the password, then try again.",
		"sign-in, expired": "That sign-in step expired Sign in again with your email address and password.",
		"code, rejected":   "That code was not accepted Check that your phone's clock is right, then type the next code.",
		"code, locked":     "Sign-in is locked for a while Too many wrong codes. Wait a quarter of an hour, then try again.",
	}
	for name, s := range map[string]step{"sign-in": signIn, "sign-in, refused": signIn, "sign-in, expired": signIn,
		"code": code, "code, rejected": code, "code, locked": code} {
		body, ok := all[name]
		if !ok {
			t.Fatalf("PREMISE: screens() has no %q", name)
		}
		column := columnOf(t, name, body)
		if !strings.HasPrefix(column, signedOutMain) {
			t.Errorf("%s: the page is not the signed-out column %q", name, signedOutMain)
			continue
		}
		inner := strings.TrimSpace(column[len(signedOutMain) : len(column)-len("</main>")])
		if !strings.HasPrefix(inner, signInCard) || !strings.HasSuffix(inner, "</section>") || strings.Count(inner, "<section class=\"op-card") != 1 {
			t.Errorf("%s: the column does not hold exactly the one card %q", name, signInCard)
			continue
		}
		for _, m := range classAttr.FindAllStringSubmatch(opNoticeBlock.ReplaceAllString(inner, ""), -1) {
			for _, c := range strings.Fields(m[1]) {
				if topMargin.MatchString(c) {
					t.Errorf("%s: %q in the card carries a top margin; the card spaces its blocks", name, m[1])
				}
			}
		}
		words := strings.TrimSpace(s.head + " " + notice[name] + " " + s.tail)
		if got := spaced(html.UnescapeString(noticeTag.ReplaceAllString(inner, " "))); got != spaced(words) {
			t.Errorf("%s: the card says %q, want %q", name, got, spaced(words))
		}
		if strings.Count(body, "<h1") != 1 || !strings.Contains(inner, "<h1 ") {
			t.Errorf("%s: %d h1, want one, in the card", name, strings.Count(body, "<h1"))
		}
		if strings.Count(body, "<form") != 1 || !strings.Contains(inner, `<form method="post" action="`+s.action+`"`) {
			t.Errorf("%s: %d form(s); want one, posting to %s, in the card", name, strings.Count(body, "<form"), s.action)
		}
		inputs := inputTag.FindAllString(body, -1)
		if len(inputs) != len(s.inputs) {
			t.Errorf("%s: %d input(s), want %d", name, len(inputs), len(s.inputs))
		}
		for i, in := range inputs {
			if i < len(s.inputs) && !maps.Equal(attributesOf(in), s.inputs[i]) {
				t.Errorf("%s: input %d has the attributes %v, want %v", name, i, attributesOf(in), s.inputs[i])
			}
		}
		at := 0
		for _, c := range s.controls {
			i := strings.Index(inner[at:], c)
			if i < 0 {
				t.Errorf("%s: %s is not in the form, in order", name, c)
				break
			}
			at += i + len(c)
		}
		if n, want := strings.Count(body, "<label"), len(s.controls)-1; n != want || strings.Count(body, "<button") != 1 {
			t.Errorf("%s: %d label(s) and %d button(s), want %d and one", name, n, strings.Count(body, "<button"), want)
		}
		if links := anchorTag.FindAllString(column, -1); strings.Join(links, "|") != strings.Join(s.links, "|") {
			t.Errorf("%s: the column's links are %q, want %q", name, links, s.links)
		}
		lower := strings.ToLower(body)
		for _, banned := range []string{"forgot", "reset", "remember", "sign up", "sign-up", "signup", "register"} {
			if strings.Contains(lower, banned) {
				t.Errorf("%s: says %q", name, banned)
			}
		}
	}
	homeMain := regexp.MustCompile(`<main [^>]*>`).FindString(all["home"])
	if homeMain == "" || homeMain == signedOutMain {
		t.Fatalf("PREMISE: the console's main is %q", homeMain)
	}
	var in, out int
	for i, v := range operator.ProblemPagesForTest() {
		var b bytes.Buffer
		if err := operatorpages.Problem(v).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		column := columnOf(t, v.Title, b.String())
		if v.SignedIn {
			in++
			if !strings.HasPrefix(column, homeMain) {
				t.Errorf("problem %d (%q): signed in but not in the frame's column %q", i, v.Title, homeMain)
			}
			continue
		}
		out++
		if !strings.HasPrefix(column, signedOutMain+`<div class="op-problem">`) || !strings.HasSuffix(column, "</div></main>") ||
			strings.Count(column, `<div class="op-problem">`) != 1 {
			t.Errorf("problem %d (%q): signed out but its card is not the signed-out column's only child", i, v.Title)
		}
	}
	if in == 0 || out == 0 {
		t.Fatalf("PREMISE: %d signed-in and %d signed-out problem page(s)", in, out)
	}
}

// attributesOf is an input tag's attributes but its class, a bare one mapped to "".
func attributesOf(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attribute.FindAllStringSubmatch(strings.TrimSuffix(strings.TrimPrefix(tag, "<input"), ">"), -1) {
		if m[1] != "class" {
			out[m[1]] = html.UnescapeString(m[2])
		}
	}
	return out
}

// columnOf is a render's <main> element, from its opening tag to its close; the page has one.
func columnOf(t *testing.T, name, body string) string {
	t.Helper()
	start, end := strings.Index(body, "<main"), strings.Index(body, "</main>")
	if start < 0 || end < start || strings.Count(body, "<main") != 1 {
		t.Fatalf("PREMISE: %s has no single <main>", name)
	}
	return body[start : end+len("</main>")]
}
