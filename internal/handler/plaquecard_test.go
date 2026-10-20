package handler

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// plaqueCardOpenFor reports whether the page carries the plaque card for uid: the card is
// named by its heading (aria-labelledby), and the heading is the uid.
func plaqueCardOpenFor(html, uid string) bool {
	return regexp.MustCompile(`<h2 id="plaque-h"[^>]*>` + regexp.QuoteMeta(uid) + `</h2>`).MatchString(html)
}

var (
	cardTagRE    = regexp.MustCompile(`<[^>]*>`)
	cardActionRE = regexp.MustCompile(`<form[^>]*\baction="([^"]*)"`)
)

// TestPlaquesCard_IsThePanelsOwnCardNotTheOldDocket pins the M12-34 port of the plaque
// card (the M12-49 screenshot tour found it still drawn the old way at every width): a
// docket with a docket inside it, the old quiet buttons, the shared components.Notice
// banners, the uid printed twice, and an "Open" button that opened the card itself.
//
// Every state the card has is rendered through the real router and cut down to the card,
// and on each one: none of the old markup; the uid in the card's TEXT exactly once (the
// hidden inputs that post it are attributes, not text); no link back to the card itself;
// every press target carries a touch-target class; and the forms post exactly where they
// did before the port.
func TestPlaquesCard_IsThePanelsOwnCardNotTheOldDocket(t *testing.T) {
	venues := twoVenues()
	wall := venues.firstVenueID()
	onWall := tenant.Plaque{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true, EncodedAt: &fixtureStamp}
	spare := tenant.Plaque{UID: uidSpare2, Status: tenant.PlaqueUnassigned, Canonical: true, EncodedAt: &fixtureStamp}
	retired := tenant.Plaque{UID: uidRetired, Status: tenant.PlaqueRetired, LocationID: &wall, Canonical: true, EncodedAt: &fixtureStamp}
	frozen := tenant.Plaque{UID: "04ac7e55000607", Status: tenant.PlaqueUnassigned, Canonical: false}
	rusty := tenant.Plaque{UID: uidOnWall, Status: tenant.PlaqueActive, LocationID: &wall, Canonical: true}

	tests := []struct {
		name        string
		plaques     []tenant.Plaque
		uid, query  string
		wantActions []string // the forms the card posts, exactly
		wantSays    []string
	}{
		{name: "in stock: the mount form", plaques: []tenant.Plaque{spare}, uid: uidSpare2,
			wantActions: []string{plaqueMountHref}, wantSays: []string{"Mount this plaque", "In stock"}},
		{name: "on a wall with a spare: the replacement form and the way down",
			plaques: []tenant.Plaque{onWall, spare}, uid: uidOnWall,
			wantActions: []string{plaqueReplaceHref},
			wantSays:    []string{"Replace this plaque", "In service", "On the wrong door?", "Take it off the wall…"}},
		{name: "on a wall with no spare: only the way down",
			plaques: []tenant.Plaque{onWall}, uid: uidOnWall,
			wantSays: []string{"take it off the wall below", "Take it off the wall…"}},
		{name: "the un-mount step: its own confirmed form",
			plaques: []tenant.Plaque{onWall}, uid: uidOnWall, query: "&confirm=unmount",
			wantActions: []string{plaqueUnmountHref},
			wantSays:    []string{"Take this plaque off the wall?", "Leave it up"}},
		{name: "retired: nothing to do, and Close",
			plaques: []tenant.Plaque{retired}, uid: uidRetired,
			wantSays: []string{"out of service", ">Close</a>", "Retired"}},
		{name: "frozen: the panel's warning banner",
			plaques: []tenant.Plaque{frozen}, uid: frozen.UID,
			wantSays: []string{noticeWarnClass, "Heads up", "This one cannot be changed"}},
		{name: "Rusty Bar's shape: the panel's alert banner and the repair",
			plaques: []tenant.Plaque{rusty, spare}, uid: uidOnWall,
			wantActions: []string{plaqueReplaceHref},
			wantSays:    []string{noticeAlertClass, "Not done", "This plaque needs replacing"}},
	}
	oldMarkup := []string{
		`btn--quiet`,      // the old quiet button
		`class="docket"`,  // the slip, and the slip inside it
		`docket-label`,    // its labels
		`<article`,        // the inner slip's element
		`border-l-4`,      // components.Notice's frames and the hand-written frozen banner
		`filter-input`,    // the old form field
		`filter-actions`,  // the old button row
		`aria-label="04A`, // the old name-by-attribute: the heading names the card now
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := plaqueBrowser(t, venues, &fakePlaques{screen: tenant.PlaqueScreen{Zone: time.UTC, Plaques: tc.plaques}})
			rec := b.do(http.MethodGet, locationsHref+"?plaque="+tc.uid+tc.query, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			html := htmlOf(t, rec)
			if !plaqueCardOpenFor(html, tc.uid) {
				t.Fatalf("the card for %s did not open under its heading", tc.uid)
			}
			card := cardOnly(t, html)

			for _, want := range tc.wantSays {
				if !strings.Contains(card, want) {
					t.Errorf("the card does not carry %q", want)
				}
			}
			for _, old := range oldMarkup {
				if strings.Contains(card, old) {
					t.Errorf("the card still carries the old markup %q", old)
				}
			}

			// THE UID ONCE, in the text a reader sees: the heading. The old card printed
			// it in the heading and again at the top of its inner slip.
			text := cardTagRE.ReplaceAllString(card, " ")
			if n := strings.Count(text, tc.uid); n != 1 {
				t.Errorf("the card's text names %s %d times, want exactly once (its heading)", tc.uid, n)
			}

			// NO LINK BACK TO ITSELF. The row's open link leads here; on the card the same
			// link only reloads the card. The un-mount step's link carries &confirm= and
			// leads somewhere else, so it is not matched.
			self := regexp.MustCompile(`href="[^"]*\?plaque=` + regexp.QuoteMeta(tc.uid) + `(#plaque)?"`)
			if m := self.FindString(card); m != "" {
				t.Errorf("the card links to itself: %s", m)
			}

			targets := pressTargetsOf(card)
			if len(targets) == 0 {
				t.Fatal("the card carries no press target at all; this case measures nothing")
			}
			for _, tgt := range targets {
				if !hasTouchTargetClass(tgt.classes) {
					t.Errorf("<%s class=%q> carries none of the touch-target classes", tgt.tag, tgt.classes)
				}
			}

			var actions []string
			for _, m := range cardActionRE.FindAllStringSubmatch(card, -1) {
				actions = append(actions, m[1])
			}
			sort.Strings(actions)
			want := append([]string(nil), tc.wantActions...)
			sort.Strings(want)
			if strings.Join(actions, " ") != strings.Join(want, " ") {
				t.Errorf("the card posts to %v, want %v", actions, want)
			}
		})
	}
}
