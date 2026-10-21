package components

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderPanelState(t *testing.T, c templ.Component, body string) string {
	t.Helper()
	var sb strings.Builder
	ctx := templ.WithChildren(context.Background(), templ.Raw(body))
	if err := c.Render(ctx, &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// TestPanelNotice_TheWordComesFirstAndTheRoleFollowsTheTone: README §5 "Notice" -- each tone
// opens with its label before the heading, an alert is role=alert and the other two role=status,
// and an unknown tone reads as the alert rather than as a success.
func TestPanelNotice_TheWordComesFirstAndTheRoleFollowsTheTone(t *testing.T) {
	tests := []struct {
		name      string
		tone      Tone
		wantRole  string
		wantLabel string
	}{
		{"ok", ToneOK, "status", "Done"},
		{"warn", ToneWarn, "status", "Heads up"},
		{"alert", ToneAlert, "alert", "Not done"},
		{"an unknown tone is the alert", Tone("bogus"), "alert", "Not done"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderPanelState(t, PanelNotice(tc.tone, "Colour saved"), "<p>BODY</p>")
			if !strings.HasPrefix(got, `<div role="`+tc.wantRole+`"`) {
				t.Errorf("role: got %.60q, want role=%q on the outer element", got, tc.wantRole)
			}
			label := strings.Index(got, ">"+tc.wantLabel+"</p>")
			heading := strings.Index(got, ">Colour saved</p>")
			body := strings.Index(got, "BODY")
			if label < 0 || heading < 0 || body < 0 || !(label < heading && heading < body) {
				t.Errorf("want label %q, then heading, then body; got indexes %d, %d, %d in %s",
					tc.wantLabel, label, heading, body, got)
			}
			if strings.Count(got, `aria-hidden="true"`) != 1 || !strings.Contains(got, `<span class="inline-flex size-8`) {
				t.Errorf("the mark is not the one hidden element: %s", got)
			}
		})
	}
	if got := renderPanelState(t, PanelNotice(ToneWarn, ""), "x"); strings.Contains(got, "font-display") {
		t.Errorf("an empty heading still drew a heading line: %s", got)
	}
}

var panelEmptyShape = regexp.MustCompile(`^<section class="[^"]*border-dashed[^"]*"><p class="lbl">The Roll</p><h2 [^>]*>No hours were recorded this week</h2><div [^>]*><p>BODY</p></div>`)

// TestPanelEmpty_NamesItsSectionAndDrawsALinkOnlyWhenGivenOne: the eyebrow is the section name the
// caller passes (never "Nothing here yet"), the heading and the caller's sentences follow, and the
// zero PanelEmptyCTA draws no link while a half-filled one draws none either.
func TestPanelEmpty_NamesItsSectionAndDrawsALinkOnlyWhenGivenOne(t *testing.T) {
	tests := []struct {
		name     string
		cta      PanelEmptyCTA
		wantLink bool
	}{
		{"no link", PanelEmptyCTA{}, false},
		{"a label without an address is no link", PanelEmptyCTA{Label: "Add a venue"}, false},
		{"an address without a label is no link", PanelEmptyCTA{Href: "/admin/locations?manage=new"}, false},
		{"a link", PanelEmptyCTA{Label: "Add a venue", Href: "/admin/locations?manage=new"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderPanelState(t, PanelEmpty("The Roll", "No hours were recorded this week", tc.cta), "<p>BODY</p>")
			if !panelEmptyShape.MatchString(got) {
				t.Errorf("shape: %s", got)
			}
			if strings.Contains(got, "Nothing here yet") {
				t.Errorf("the label still promises an arrival: %s", got)
			}
			link := strings.Contains(got, `<a class="btn btn--primary`)
			if link != tc.wantLink {
				t.Errorf("link drawn = %v, want %v: %s", link, tc.wantLink, got)
			}
			if tc.wantLink && !strings.Contains(got, `href="/admin/locations?manage=new">Add a venue</a>`) {
				t.Errorf("the link does not carry its address and words: %s", got)
			}
		})
	}
}
