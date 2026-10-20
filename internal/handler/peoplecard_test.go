package handler

// peoplecard_test.go -- the People screen of the panel redesign (M12-30): what the list and
// the person's card print from the D3 reads, and what they never print.
//
// THE RULES HELD HERE are the card's own, each one a sentence a manager acts on:
//   - the tenant gate comes first: Staff.Person answers "on this roster" before RecentTaps is
//     asked anything, because RecentTaps answers an unknown id with a measured empty strip;
//   - unknown is never 0 or "none" (§4.6): a failed read says so, a read nobody asked for draws
//     nothing, and only a measured one prints a count, a "—" or "No record yet";
//   - no date is invented: an invited person added in the panel has no invited_at, so the card
//     prints the pending link's own date or nothing;
//   - deactivating keeps browsers signed in, and the card prints the real count;
//   - the strip is four words per record and nothing else (K-39);
//   - nothing on these views is a field an address, a code or a device label could travel in.

import (
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/components"
)

// peopleCardBrowser drives the section against a caller's ledger and staff fakes.
func peopleCardBrowser(t *testing.T, records *fakeLedger, staff *fakeStaff) *browser {
	t.Helper()
	return panelBrowserWithLedger(t, records, staff, &fakeInviter{})
}

// cardOf isolates the person's card, so an assertion about the card is not satisfied by a
// row of the list or a notice.
var cardRE = regexp.MustCompile(`(?s)<section class="[^"]*" aria-label="Actions for [^"]*">.*?</section>`)

func cardOf(t *testing.T, body string) string {
	t.Helper()
	cards := cardRE.FindAllString(body, -1)
	if len(cards) != 1 {
		t.Fatalf("the page carries %d person card(s), want exactly 1", len(cards))
	}
	return cards[0]
}

func TestPeopleCard_AsksTheTenantGateBeforeTheStrip(t *testing.T) {
	t.Run("an unknown person: no summary and no strip are asked for", func(t *testing.T) {
		records := newFakeLedger()
		staff := &fakeStaff{personErr: tenant.ErrUnknownEmployee}
		b := peopleCardBrowser(t, records, staff)
		body := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New()), nil))
		if !strings.Contains(body, "That person is not on this business&#39;s roster") &&
			!strings.Contains(body, "That person is not on this business's roster") {
			t.Fatalf("an unknown person did not get the 'unknown' sentence; this test reads the wrong page")
		}
		if len(records.recentAsked) != 0 || len(staff.summaryAsked) != 0 {
			t.Errorf("a person the gate refused was still read: strip %v, summary %v",
				records.recentAsked, staff.summaryAsked)
		}
	})
	t.Run("a person on the roster: both are asked, for that person, in this tenant", func(t *testing.T) {
		records := newFakeLedger()
		staff := &fakeStaff{}
		b := peopleCardBrowser(t, records, staff)
		id := uuid.New()
		htmlOf(t, b.do(http.MethodGet, managedRosterHref(id), nil))
		if len(staff.lookups) == 0 || staff.lookups[0] != id {
			t.Fatalf("the gate was not asked about %s first: %v", id, staff.lookups)
		}
		if len(records.recentAsked) != 2 || records.recentAsked[0] != panelTestTenant || records.recentAsked[1] != id {
			t.Errorf("the strip was asked %v, want [tenant %s, employee %s]", records.recentAsked, panelTestTenant, id)
		}
		if len(staff.summaryAsked) != 1 || staff.summaryAsked[0] != id {
			t.Errorf("the summary was asked about %v, want [%s]", staff.summaryAsked, id)
		}
	})
}

func TestPeopleCard_UnknownIsNeverNoneOrZero(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary tenant.PersonSummary
		recent  ledger.RecentTaps
		want    []string
		never   []string
	}{
		{
			name:  "nothing was read",
			want:  []string{"Managing"},
			never: []string{"No record yet", "could not be read", "signed in", "No working link"},
		},
		{
			name:    "both reads failed",
			summary: tenant.PersonSummary{State: tenant.ReadFailed},
			recent:  ledger.RecentTaps{State: ledger.ReadFailed},
			want:    []string{"Their dates and signed-in browsers could not be read", "Their recent records could not be read"},
			never:   []string{"No record yet", "None signed in", "No working link"},
		},
		{
			name:    "both measured, nothing to show",
			summary: tenant.PersonSummary{State: tenant.Measured, Status: ledger.StatusActive},
			recent:  ledger.RecentTaps{State: ledger.Measured},
			want:    []string{"No record yet", "None signed in"},
			never:   []string{"could not be read"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := newFakeLedger()
			records.recent = tc.recent
			b := peopleCardBrowser(t, records, &fakeStaff{summary: tc.summary})
			card := cardOf(t, htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New()), nil)))
			for _, w := range tc.want {
				if !strings.Contains(card, w) {
					t.Errorf("the card does not say %q.\ncard:\n%s", w, card)
				}
			}
			for _, n := range tc.never {
				if strings.Contains(card, n) {
					t.Errorf("the card says %q, which nothing measured.\ncard:\n%s", n, card)
				}
			}
		})
	}
}

func TestPeopleCard_AnInvitedPersonGetsNoInventedDate(t *testing.T) {
	invited := &tenant.Person{Name: "Ahmed Hassan", Status: ledger.StatusInvited, LocationID: &panelTestLocation, LocationName: "KF Hamrun"}
	created := time.Now().Add(-49 * time.Hour)
	expires := created.Add(7 * 24 * time.Hour)
	for _, tc := range []struct {
		name string
		link tenant.ActivationLink
		want string
	}{
		{"no link outstanding", tenant.ActivationLink{State: tenant.Measured}, "No working link yet."},
		{"a link outstanding", tenant.ActivationLink{State: tenant.Measured, Pending: 1, Created: &created, Expires: &expires},
			"A link was created on " + created.UTC().Format("2 Jan") + " and works until " + expires.UTC().Format("2 Jan 15:04") + "."},
		{"the link read failed", tenant.ActivationLink{State: tenant.ReadFailed}, "Whether a link is outstanding could not be read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			staff := &fakeStaff{person: invited, summary: tenant.PersonSummary{
				State: tenant.Measured, Status: ledger.StatusInvited, Link: tc.link,
			}}
			card := cardOf(t, htmlOf(t, peopleCardBrowser(t, newFakeLedger(), staff).
				do(http.MethodGet, managedRosterHref(uuid.New()), nil)))
			if !strings.Contains(card, tc.want) {
				t.Errorf("the activation box does not say %q.\ncard:\n%s", tc.want, card)
			}
			for _, invented := range []string{"since", "Invited on", "Invited since"} {
				if strings.Contains(card, invented) {
					t.Errorf("an invited person's card prints %q, a date no statement of this product wrote.\ncard:\n%s",
						invented, card)
				}
			}
			if strings.Contains(card, " sent") {
				t.Errorf("the card says a link was sent; the product knows only that one was created.\ncard:\n%s", card)
			}
		})
	}
}

func TestPeopleCard_ADeactivatedPersonKeepsTheirSignedInBrowsers(t *testing.T) {
	gone := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	first := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	staff := &fakeStaff{
		person: &tenant.Person{Name: "Dragan Nikolic", Status: ledger.StatusDeactivated, LocationID: &panelTestLocation, LocationName: "KF St Julians"},
		summary: tenant.PersonSummary{
			State: tenant.Measured, Status: ledger.StatusDeactivated, Deactivated: &gone,
			LiveDevices: 2, FirstSignedIn: &first,
		},
	}
	card := cardOf(t, htmlOf(t, peopleCardBrowser(t, newFakeLedger(), staff).
		do(http.MethodGet, managedRosterHref(uuid.New()), nil)))
	for _, want := range []string{
		"2 signed in · since 3 Sep 2026 · not used yet",
		"stays signed in; it simply cannot record time any more",
		"Deactivated on 2 Oct 2026. Their taps are refused, and every refusal is still recorded.",
		"Their taps are already being refused. Deactivating again would change nothing.",
		"No invitation can be sent to somebody who has been deactivated.",
	} {
		if !strings.Contains(strings.Join(strings.Fields(card), " "), want) {
			t.Errorf("the card does not say %q.\ncard:\n%s", want, card)
		}
	}
	for _, never := range []string{"Unlink", "Unlinked when stopped", "can no longer tap", "Stopped"} {
		if strings.Contains(card, never) {
			t.Errorf("the card says %q -- deactivation keeps sessions and records every refusal", never)
		}
	}
}

func TestPeopleCard_TheStripIsFourWordsPerRecord(t *testing.T) {
	want := []string{"Day", "Direction", "Time", "Verdict", "Word"}
	rt := reflect.TypeOf(components.MiniTapView{})
	var got []string
	for i := 0; i < rt.NumField(); i++ {
		got = append(got, rt.Field(i).Name)
		if rt.Field(i).Type.Kind() != reflect.String {
			t.Errorf("MiniTapView.%s is a %s; a strip cell is words", rt.Field(i).Name, rt.Field(i).Type)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("MiniTapView carries %v, want exactly %v (K-39: day, direction, time and the "+
			"engine's word -- a partner venue's record must read like any other)", got, want)
	}

	at := time.Date(2026, 10, 20, 14, 26, 0, 0, time.UTC)
	records := newFakeLedger()
	records.recent = ledger.RecentTaps{State: ledger.Measured, Taps: []ledger.RecentTap{
		{At: at, Direction: "in", Verdict: "flag", Channel: "nfc"},
		{At: at.Add(-time.Hour), Direction: "", Verdict: "reject", Channel: "nfc"},
	}}
	card := cardOf(t, htmlOf(t, peopleCardBrowser(t, records, &fakeStaff{}).
		do(http.MethodGet, managedRosterHref(uuid.New()), nil)))
	for _, w := range []string{"Last 2 records, newest first", "Tue 20", ">IN<", "14:26", "FLAGGED", ">—<", "REJECTED"} {
		if !strings.Contains(card, w) {
			t.Errorf("the strip does not carry %q.\ncard:\n%s", w, card)
		}
	}
}

func TestPeopleList_CountsOnlyWhatWasMeasured(t *testing.T) {
	people := []ledger.Person{{ID: uuid.New(), Name: "Maria Borg", Status: ledger.StatusActive, HasHome: true, LocationName: "KF Valletta"}}
	t.Run("measured", func(t *testing.T) {
		records := newFakeLedger()
		var tally ledger.StatusTally
		tally.State = ledger.Measured
		records.roster = ledger.RosterScreen{RosterPage: ledger.RosterPage{Queried: true, People: people, LastTaps: ledger.Measured}, Tally: tally}
		body := htmlOf(t, panelBrowserWithRoster(t, records).do(http.MethodGet, employeesHref, nil))
		if !strings.Contains(body, "Who taps · 0 on the books") {
			t.Error("a measured tally of nobody does not print its 0 in the kicker")
		}
		if n := strings.Count(body, `<span class="chip-count">0</span>`); n != 4 {
			t.Errorf("a measured empty tally prints %d chip counts, want 4", n)
		}
		if !strings.Contains(rosterRowsOn(body), "—") {
			t.Error("a measured page with no tap for the person does not print the dash")
		}
	})
	t.Run("failed", func(t *testing.T) {
		records := newFakeLedger()
		records.roster = ledger.RosterScreen{
			RosterPage: ledger.RosterPage{Queried: true, People: people, LastTaps: ledger.ReadFailed},
			Tally:      ledger.StatusTally{State: ledger.ReadFailed},
		}
		body := htmlOf(t, panelBrowserWithRoster(t, records).do(http.MethodGet, employeesHref, nil))
		if strings.Contains(body, "on the books") || strings.Contains(body, "chip-count") {
			t.Error("a failed tally printed a number")
		}
		if !strings.Contains(body, "<p class=\"lbl\">Who taps</p>") {
			t.Error("a failed tally lost the kicker altogether; it should say what the page is")
		}
		rows := rosterRowsOn(body)
		if !strings.Contains(rows, "could not be read") || strings.Contains(rows, "—") {
			t.Errorf("a failed last-tap read must say so on the row, never print the dash.\nrows:\n%s", rows)
		}
	})
}

// TestPeopleViews_TheNewFieldsCarryNoSecret is §4.7 on the view types the redesign added
// (M12-30): every new field passes the coordinate-and-secret name walker and is a word, a
// count or a flag, and the row still has no field for an address, a device or a code.
func TestPeopleViews_TheNewFieldsCarryNoSecret(t *testing.T) {
	fields := map[reflect.Type][]string{
		reflect.TypeOf(components.RosterRowView{}): {"Initials", "Role", "LastTap", "LastTapNote", "Devices", "Selected"},
		reflect.TypeOf(components.RosterActionsView{}): {"Initials", "Role", "SinceLine", "DeactivatedOn",
			"SignedInLine", "LiveDevices", "LinkLine", "SummaryFailed", "Recent", "RecentState"},
		reflect.TypeOf(components.RosterFilterView{}): {"Chips"},
	}
	for rt, names := range fields {
		for _, name := range names {
			f, ok := rt.FieldByName(name)
			if !ok {
				t.Errorf("%s has no field %s", rt, name)
				continue
			}
			if why := bannedFieldName(f.Name); why != "" {
				t.Errorf("%s.%s: %s", rt, f.Name, why)
			}
		}
	}
	for _, rt := range []reflect.Type{reflect.TypeOf(components.MiniTapView{}), reflect.TypeOf(components.RosterChipView{})} {
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if why := bannedFieldName(f.Name); why != "" {
				t.Errorf("%s.%s: %s", rt, f.Name, why)
			}
			switch f.Type.Kind() {
			case reflect.String, reflect.Int, reflect.Bool:
			default:
				t.Errorf("%s.%s is a %s; a People view leaf is a word, a count or a flag", rt, f.Name, f.Type)
			}
		}
	}
	row := reflect.TypeOf(components.RosterRowView{})
	for i := 0; i < row.NumField(); i++ {
		low := strings.ToLower(row.Field(i).Name)
		for _, never := range []string{"email", "mail", "device", "code", "token", "session"} {
			if strings.Contains(low, never) && row.Field(i).Name != "Devices" {
				t.Errorf("RosterRowView.%s: a list row carries no %s", row.Field(i).Name, never)
			}
		}
	}
}
