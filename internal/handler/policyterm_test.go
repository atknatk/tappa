package handler

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// policyTermWord is the word the panel no longer uses for sys:* (backlog T150), in any form and case.
var policyTermWord = regexp.MustCompile(`(?i)guarant`)

// policyTermClassAttr is the one place the word may stay: the guardrail block's CSS class
// (policy-guarantees), which no reader sees and which the layout tests anchor on.
var policyTermClassAttr = regexp.MustCompile(`class="[^"]*"`)

// TestPolicyScreens_SayGuardrailsNotGuarantees (backlog T150): the Rules screen in every state the
// template tests drive (policyPageStates: read-only and editable, the degraded "guardrails alone"
// rulebook, a business with no baseline in force, the refusal words) and the change confirmation
// carry no form of "guarantee" outside a class attribute -- the public site calls the same layer
// "Taptime's guardrails" and may not print that word, and the panel now says what the site says.
//
// CONTROL: the heading is on the page, and the scan finds the word when it is there.
// MUTANT (measured): the heading back to "Taptime guarantees" -> red.
func TestPolicyScreens_SayGuardrailsNotGuarantees(t *testing.T) {
	// scan returns the text around the first use of the word outside a class attribute, or "".
	scan := func(page string) string {
		text := policyTermClassAttr.ReplaceAllString(page, "")
		loc := policyTermWord.FindStringIndex(text)
		if loc == nil {
			return ""
		}
		return text[max(0, loc[0]-60):min(len(text), loc[1]+60)]
	}
	pages := map[string]string{}
	for name, st := range policyPageStates(t) {
		pages["rules screen, "+name] = policyStatePage(t, st)
	}
	rec := policyWriteBrowser(t, newFakeScribe()).do(http.MethodPost, policyChangeHref,
		url.Values{"op": {opDisable}, "policy": {uuid.NewString()}})
	if rec.Code != http.StatusOK {
		t.Fatalf("the confirmation step answered %d, want 200", rec.Code)
	}
	pages["change confirmation"] = htmlOf(t, rec)
	if len(pages) < 5 {
		t.Fatalf("CONTROL FAILED: %d pages to scan", len(pages))
	}
	heading := 0
	for name, page := range pages {
		if around := scan(page); around != "" {
			t.Errorf("%s uses the word: %q", name, around)
		}
		// templ writes a template's static text as written, so the apostrophe is not escaped.
		if strings.Contains(page, "Taptime's guardrails") || strings.Contains(page, htmlText("Taptime's guardrails")) {
			heading++
		}
	}
	if heading == 0 {
		t.Error("CONTROL FAILED: no page carries the heading \"Taptime's guardrails\"")
	}
	if scan(`<h2 class="policy-guarantees">Taptime GUARANTEED</h2>`) == "" ||
		scan(`<div class="policy-guarantees"></div>`) != "" {
		t.Fatal("CONTROL FAILED: the scan does not tell the class attribute from the text")
	}
}
