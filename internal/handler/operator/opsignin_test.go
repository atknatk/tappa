package operator_test

// opsignin_test.go -- M13-04: the sign-in and the code step in the redesign, and the signed-out
// column they share with the signed-out problem page. docs/plan/m13-operator-redesign.md, card
// M13-04; docs/design/operator-redesign README §6 "Signed-out card", screens.md §1.

import (
	"bytes"
	"context"
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
	anchorTag = regexp.MustCompile(`<a [^>]*>[^<]*</a>`)
)

// TestSignInScreens_AreOneCardInTheSignedOutColumn (M13-04; screens.md §1, README §6 "Signed-out
// card"): each of the six sign-in renders of screens() -- sign-in, refused, expired; code,
// rejected, locked -- is the signed-out column (signedOutMain) holding exactly one card
// (signInCard) and nothing else; inside the card, outside its notice (whose own heading and body
// are opNotice's, M13-03), no element carries a top margin (the card spaces its blocks); the page has one h1, the step's ("Operator sign-in" / "Enter your code"), one form,
// posting to the step's route, whose inputs are exactly the step's named fields (email and
// password; code), none carrying a value -- the typed address is not written back (screens.md §1
// "Binding") and no hidden field exists; the sign-in has no link in the column and the code step
// exactly one, "Start again" to /operator/login; no render says "forgot", "reset", "remember",
// "sign up", "sign-up" or "register" (screens.md §1 "Not shown"). Every problem constant
// (problemPages()) signed out sits in the same column, its card (div.op-problem) the column's
// only child; signed in, the column is the frame's, the very opening tag of the console's main
// (the signed-in problem keeps its 640 px, M13-03).
//
// PART II -- red on: a card or column width or padding changed, a wrapper back around a notice
// or a top margin back in a body, a second form, an input added, removed or given a value, a
// recovery, sign-up or remember-me word, a link added, the signed-out column on a signed-in
// problem or a signed-in one on a signed-out problem. PART III -- the look at 320 / 390 / 834 /
// 1200 / 1440 px is the screenshot round's; the refused answers' byte identity is
// TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike's and
// TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes's -- no completeness claim.
func TestSignInScreens_AreOneCardInTheSignedOutColumn(t *testing.T) {
	all := screens(t)
	type step struct {
		h1, action string
		inputs     []string
		links      []string
	}
	signIn := step{"Operator sign-in", "/operator/login", []string{`name="email"`, `name="password"`}, nil}
	code := step{"Enter your code", "/operator/login/totp", []string{`name="code"`}, []string{`<a href="/operator/login" class="op-link">Start again</a>`}}
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
		if strings.Count(body, "<h1") != 1 || !strings.Contains(body, ">"+s.h1+"</h1>") {
			t.Errorf("%s: %d h1, want one saying %q", name, strings.Count(body, "<h1"), s.h1)
		}
		if strings.Count(body, "<form") != 1 || !strings.Contains(inner, `<form method="post" action="`+s.action+`"`) {
			t.Errorf("%s: %d form(s); want one, posting to %s, in the card", name, strings.Count(body, "<form"), s.action)
		}
		inputs := inputTag.FindAllString(body, -1)
		if len(inputs) != len(s.inputs) {
			t.Errorf("%s: %d input(s), want %d", name, len(inputs), len(s.inputs))
		}
		for i, in := range inputs {
			if i < len(s.inputs) && !strings.Contains(in, s.inputs[i]) {
				t.Errorf("%s: input %d is %q, want %s", name, i, in, s.inputs[i])
			}
			if strings.Contains(in, "value=") || strings.Contains(in, `type="hidden"`) {
				t.Errorf("%s: input %q carries a value or is hidden", name, in)
			}
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

// columnOf is a render's <main> element, from its opening tag to its close; the page has one.
func columnOf(t *testing.T, name, body string) string {
	t.Helper()
	start, end := strings.Index(body, "<main"), strings.Index(body, "</main>")
	if start < 0 || end < start || strings.Count(body, "<main") != 1 {
		t.Fatalf("PREMISE: %s has no single <main>", name)
	}
	return body[start : end+len("</main>")]
}
