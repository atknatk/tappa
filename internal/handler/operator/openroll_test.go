package operator_test

// openroll_test.go -- M13-05: the enrollment page in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-05; docs/design/operator-redesign README §6 "Signed-out card", "Step / card heading",
// "QR + key", screens.md §2.

import (
	"bytes"
	"context"
	"html"
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/internal/qrcode"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// enrollCard is the enrollment page's card: the whole of the column, its blocks spaced by the
// card (README §6 "Card": gap-3.5 in .op-card's rule, no mt-* in the bodies).
const enrollCard = `<section class="op-card w-full">`

// tokenField is the link code's field whole (screens.md §2 "Form"): the wrapper enroll.js hides by
// its id, the label, the input enroll.js fills by its id, the help, and nothing else.
var tokenField = regexp.MustCompile(`<div id="enroll-token-wrap" class="op-token-field"><label for="enroll-token" class="op-label">Link code</label>\s*` +
	`<input id="enroll-token" [^>]*><p class="text-xs text-ink/70">The part of your link after the # sign\.</p></div>`)

// TestEnrollScreen_IsOneCardInTheSignedOutColumn (M13-05; screens.md §2, README §6 "Signed-out
// card"): each enrollment render -- the first load with its QR, the first load whose QR could not
// be drawn, the re-render with each of its three alerts and with all three, and screens()'s two --
// is the signed-out column, the very opening tag of the sign-in's (signedOutMain), holding exactly
// one card (enrollCard) and nothing else; inside the card, outside its notices (opNotice's, M13-03),
// no element carries a top margin and no docket label (the steps are .op-step headings, the field
// labels .op-label); the card's words, tags dropped, are exactly screens.md §2's in order -- the h1,
// the line under it, on a first load step 1's heading, the QR's title, "Scan the code ..." (with the
// QR only), the key, the URI's sentence and the URI, on a re-render the warn notice then the
// state's alerts, then step 2's heading, the four field labels, the link code's help and the button;
// the page has one h1, in the card, and its step headings are exactly the whole tags
// <h2 class="op-step">Step 1 · …</h2> (first load only, before the QR) and Step 2's (inside the
// form, before its first input); one form, posting to /operator/enroll; one button, one script
// (enroll.js, by its src); four labels, the whole tags in order; the inputs are the six of §2 "Form"
// in order, each with exactly its attribute set, its class aside -- the hidden id and blob and the
// token's value are checked for presence and never by value (they are the account, the sealed key
// and the link's token); the token field is the one div#enroll-token-wrap.op-token-field, holding
// the label, the input#enroll-token and its help, and no other element names enroll-token; the
// key comes before the URI; the column holds no link.
//
// PART II -- red on: the column's or the card's class changed, a wrapper back around a notice or a
// top margin back in a body, a step back to a docket label or out of its heading, a word of the
// card changed, added or dropped, a label or the button changed, a second form, button or script,
// an input added, removed or an attribute of one added, dropped or changed (type, autocomplete,
// inputmode, minlength, required), a hidden field or the token's value dropped, the token field's
// ids changed, a link added. PART III -- the QR's drawing and its svg-key-URI order on the first
// load are TestEnrollQR_IsTheKeysURIInkOnPaper's, the one svg TestEnrollQR_OnlyOnTheFirstLoad's,
// the script's body TestEnrollScript_IsTheReviewedBody's, the notices' tones and sentences
// TestOperatorNotice_EveryCallerKeepsItsToneAndHeadingInTheOperatorsOwnBox's; the look at 320 /
// 390 / 834 / 1200 / 1440 px is the screenshot round's.
func TestEnrollScreen_IsOneCardInTheSignedOutColumn(t *testing.T) {
	sym, err := qrcode.Encode([]byte("otpauth://totp/x"), qrcode.M)
	if err != nil {
		t.Fatal(err)
	}
	first := operatorpages.EnrollView{AccountID: "x", Key: "ABCD EFGH", URI: "otpauth://totp/x", QR: sym.Bitmap(qrcode.QuietZone), Blob: "b"}
	noQR := first
	noQR.QR = nil
	again := operatorpages.EnrollView{AccountID: "x", Blob: "b", Token: "t"}
	mismatch, weak, rejected, all3 := again, again, again, again
	mismatch.Mismatch, weak.WeakPassword, rejected.CodeRejected = true, true, true
	all3.Mismatch, all3.WeakPassword, all3.CodeRejected = true, true, true

	const (
		head  = "Set up your operator account This link works once, for 30 minutes from when it was made."
		step1 = "Step 1 · Add this key to your authenticator app"
		scan  = "Scan with your authenticator app Scan the code with your app, or type this key:"
		key   = "ABCD EFGH If your app takes a setup link instead, use this one: otpauth://totp/x"
		warn  = "Use the key you already added Type a code from the authenticator entry this page gave you. " +
			"If you did not add it, open your link again for a new key."
		differ = "The two passwords differ Type the same new password in both fields."
		weakW  = "Choose another password Use at least 14 characters and at most 72 bytes. A letter with an accent, " +
			"or from another alphabet, takes two bytes or more."
		clockW = "That code was not accepted Check that your phone's clock is right, then type the next code."
		step2  = "Step 2 · Choose a password and confirm the code"
		tail   = step2 + " Link code The part of your link after the # sign. New password New password again Code from the app Finish setup"
	)
	type render struct {
		v     operatorpages.EnrollView
		words string
	}
	renders := map[string]render{
		"first load":           {first, head + " " + step1 + " " + scan + " " + key + " " + tail},
		"first load, no QR":    {noQR, head + " " + step1 + " " + key + " " + tail},
		"re-render, mismatch":  {mismatch, head + " " + warn + " " + differ + " " + tail},
		"re-render, weak":      {weak, head + " " + warn + " " + weakW + " " + tail},
		"re-render, code":      {rejected, head + " " + warn + " " + clockW + " " + tail},
		"re-render, all three": {all3, head + " " + warn + " " + differ + " " + weakW + " " + clockW + " " + tail},
	}
	bodies := map[string]string{}
	for name, r := range renders {
		bodies[name] = renderComponent(t, operatorpages.Enroll(r.v))
	}
	scr := screens(t)
	if scr["enroll"] == "" || scr["enroll, re-render"] == "" {
		t.Fatal("PREMISE: screens() has no enrollment render")
	}
	bodies["screens(): enroll"], bodies["screens(): re-render"] = scr["enroll"], scr["enroll, re-render"]
	renders["screens(): enroll"] = renders["first load"]
	renders["screens(): re-render"] = renders["re-render, all three"]

	// inputs is §2 "Form" in order, each input's whole attribute set but its class; "value" maps to
	// presence ("present"), never to the value itself.
	const present = "present"
	inputs := []map[string]string{
		{"type": "hidden", "name": "id", "value": present},
		{"type": "hidden", "name": "blob", "value": present},
		{"id": "enroll-token", "name": "token", "type": "password", "autocomplete": "off", "required": "", "value": present},
		{"id": "enroll-password", "name": "password", "type": "password", "autocomplete": "new-password", "minlength": "14", "required": ""},
		{"id": "enroll-password-again", "name": "password_again", "type": "password", "autocomplete": "new-password", "minlength": "14", "required": ""},
		{"id": "enroll-code", "name": "code", "type": "text", "inputmode": "numeric", "autocomplete": "one-time-code", "required": ""},
	}
	labels := []string{
		`<label for="enroll-token" class="op-label">Link code</label>`,
		`<label for="enroll-password" class="op-label">New password</label>`,
		`<label for="enroll-password-again" class="op-label">New password again</label>`,
		`<label for="enroll-code" class="op-label">Code from the app</label>`,
		`<button type="submit" class="btn btn--primary">Finish setup</button>`,
	}
	const (
		step1Tag = `<h2 class="op-step">` + step1 + `</h2>`
		step2Tag = `<h2 class="op-step">` + step2 + `</h2>`
		formTag  = `<form method="post" action="/operator/enroll" class="`
	)
	for name, body := range bodies {
		r := renders[name]
		column := columnOf(t, name, body)
		if !strings.HasPrefix(column, signedOutMain) {
			t.Errorf("%s: the page is not the signed-out column %q", name, signedOutMain)
			continue
		}
		inner := strings.TrimSpace(column[len(signedOutMain) : len(column)-len("</main>")])
		if !strings.HasPrefix(inner, enrollCard) || !strings.HasSuffix(inner, "</section>") || strings.Count(inner, `<section class="op-card`) != 1 {
			t.Errorf("%s: the column does not hold exactly the one card %q", name, enrollCard)
			continue
		}
		outside := opNoticeBlock.ReplaceAllString(inner, "")
		for _, m := range classAttr.FindAllStringSubmatch(outside, -1) {
			for _, c := range strings.Fields(m[1]) {
				if topMargin.MatchString(c) {
					t.Errorf("%s: %q in the card carries a top margin; the card spaces its blocks", name, m[1])
				}
			}
		}
		if strings.Contains(inner, "docket-label") {
			t.Errorf("%s: the card still carries a docket label", name)
		}
		if got := spaced(html.UnescapeString(noticeTag.ReplaceAllString(inner, " "))); got != spaced(r.words) {
			t.Errorf("%s: the card says %q, want %q", name, got, spaced(r.words))
		}
		if strings.Count(body, "<h1") != 1 || !strings.Contains(inner, "<h1 ") {
			t.Errorf("%s: %d h1, want one, in the card", name, strings.Count(body, "<h1"))
		}
		form := strings.Index(inner, formTag)
		if strings.Count(body, "<form") != 1 || form < 0 {
			t.Errorf("%s: %d form(s); want one, posting to /operator/enroll, in the card", name, strings.Count(body, "<form"))
			continue
		}
		wantH2 := 1
		if r.v.Key != "" {
			wantH2 = 2
			at := strings.Index(inner, step1Tag)
			if at < 0 || at > strings.Index(inner, `class="op-key"`) || (len(r.v.QR) > 0 && at > strings.Index(inner, "<svg")) {
				t.Errorf("%s: step 1 is not the heading %s before the QR and the key", name, step1Tag)
			}
		}
		if n := strings.Count(body, "<h2"); n != wantH2 || strings.Count(body, `<h2 class="op-step">`) != wantH2 {
			t.Errorf("%s: %d h2 element(s), want %d, each an op-step", name, n, wantH2)
		}
		if at := strings.Index(inner, step2Tag); at < form || at > strings.Index(inner, "<input") {
			t.Errorf("%s: step 2 is not the heading %s opening the form", name, step2Tag)
		}
		got := inputTag.FindAllString(body, -1)
		if len(got) != len(inputs) {
			t.Errorf("%s: %d input(s), want %d", name, len(got), len(inputs))
		}
		for i, in := range got {
			a := attributesOf(in)
			if _, ok := a["value"]; ok {
				a["value"] = present
			}
			if i < len(inputs) && !maps.Equal(a, inputs[i]) {
				t.Errorf("%s: input %d has the attributes %v, want %v", name, i, a, inputs[i])
			}
		}
		at := form
		for _, c := range labels {
			i := strings.Index(inner[at:], c)
			if i < 0 {
				t.Errorf("%s: %s is not in the form, in order", name, c)
				break
			}
			at += i + len(c)
		}
		if n := strings.Count(body, "<label"); n != len(labels)-1 || strings.Count(body, "<button") != 1 {
			t.Errorf("%s: %d label(s) and %d button(s), want %d and one", name, n, strings.Count(body, "<button"), len(labels)-1)
		}
		if strings.Count(body, "<script") != 1 || !strings.Contains(body, `<script src="`+operatorpages.EnrollScript()+`"`) {
			t.Errorf("%s: the page does not load exactly the one script, %s", name, operatorpages.EnrollScript())
		}
		if w := tokenField.FindAllStringIndex(body, -1); len(w) != 1 || w[0][0] < strings.Index(body, formTag) ||
			strings.Count(body, "enroll-token-wrap") != 1 || strings.Count(body, `"enroll-token"`) != 2 {
			t.Errorf("%s: the token field is not the one div#enroll-token-wrap.op-token-field holding its label, input and help", name)
		}
		if r.v.Key != "" && strings.Index(inner, `class="op-key"`) > strings.Index(inner, `class="op-uri"`) {
			t.Errorf("%s: the URI comes before the key", name)
		}
		if links := anchorTag.FindAllString(column, -1); len(links) != 0 {
			t.Errorf("%s: the column holds the link(s) %q", name, links)
		}
	}
	if len(bodies) != 8 {
		t.Fatalf("PREMISE: %d render(s), want 8", len(bodies))
	}
}

// renderComponent is c's HTML.
func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
