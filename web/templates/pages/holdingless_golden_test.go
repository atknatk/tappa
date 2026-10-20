package pages

// holdingless_golden_test.go -- M11 API-1c (the second review's D-3): the reports and anomalies
// pages of a business in NO holding are byte for byte what they were before API-1c. The goldens
// in testdata/holdingless/ were rendered by THIS test at 85b4b8d (the commit before API-1c's
// panels) and are never rewritten from later code: a change to these pages that is meant has to
// say so by replacing a golden in its own commit. MUTANT (measured): one space added between two
// sections of either page -> red.
//
// RE-RENDERED ON 2026-10-20 FOR THE PANEL SHELL OF DESIGN "THE PASS" (user-approved), which
// changes the chrome around every panel page. Checked before replacing: with the chrome cut off
// (everything outside the content well; before, everything outside the tab bar and </main>), each
// page's own markup is byte for byte the 85b4b8d golden's. The holding claim is unchanged.
// The anomalies golden was replaced once more in the same day's fix round: its one change is the
// sentence naming the reports section by its new label ("The Reports section" -> "The Roll").

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// holdinglessRenders are the two pages, populated with every list that renders for a business with
// no partner (the views' API-1c fields left zero, which is what such a business gets).
func holdinglessRenders(t *testing.T) map[string][]byte {
	t.Helper()
	reports := ReportsView{
		Queried: true, ZoneLabel: "Europe/Malta", WeekLabel: "Mon 7 – Sun 13 September 2026",
		WeekValue: "2026-09-07", PrevHref: "/admin/reports?week=2026-08-31", NextHref: "/admin/reports?week=2026-09-14",
		ThisWeekHref: "/admin/reports", ExportHref: "/admin/reports.csv?week=2026-09-07",
		DayHeadings: []string{"Mon 7", "Tue 8", "Wed 9", "Thu 10", "Fri 11", "Sat 12", "Sun 13"},
		Totals:      ReportTotalsView{Worked: "16h 00m", Shifts: 2, Awaiting: "1h 00m", Open: 1, NeedsAction: 1},
		People:      []ReportRowView{{Name: "Maria Borg", Worked: "16h 00m", Daily: []string{"8h 00m", "", "8h 00m", "", "", "", ""}, Shifts: 2}},
		Venues:      []ReportVenueView{{Name: "KF St Julians", Worked: "16h 00m", Shifts: 2}},
		Open:        []ReportOpenView{{Who: "Joe Camilleri", Venue: "KF St Julians", At: "Fri 11 Sep 09:00", NeedsAction: true}},
		PeopleShown: 1, PeopleTotal: 1, OpenShown: 1, OpenTotal: 1, PracticeTaps: 1,
	}
	anomalies := AnomaliesView{
		Queried: true, ZoneLabel: "Europe/Malta", WeekLabel: "Mon 7 – Sun 13 September 2026",
		Totals: AnomalyTotalsView{RecordsLine: "12 records", JudgedLine: "10 of these were judged", BasisLine: "Two bases",
			DistanceOnly: 1, DistanceOnlyLine: "1 of 10 verified by GPS alone", OutsideVenue: 1, OutsideVenueLine: "1 of 11 read outside the venue",
			OutsideVenueOnNetworkLine: "none on the network", Unanswerable: 1, UnanswerableLine: "1 record carries no policy snapshot"},
		People:   []AnomalyPersonView{{Name: "Maria Borg", Records: "4 records", DistanceOnly: "1 verified by GPS alone"}},
		Venues:   []AnomalyVenueView{{Name: "KF St Julians", Records: "12 records", DistanceOnly: "1 of 10", OutsideVenue: "1 of 11"}},
		Policies: []AnomalyPolicyView{{SID: "base:ip-or-gps-ok", Layer: "baseline", Records: "10 records"}},
		ListCap:  50,
	}
	out := map[string][]byte{}
	var b bytes.Buffer
	if err := AdminReports(reports).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out["reports"] = bytes.Clone(b.Bytes())
	b.Reset()
	if err := AdminAnomalies(anomalies).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out["anomalies"] = bytes.Clone(b.Bytes())
	return out
}

func TestHoldinglessPanels_AreByteIdenticalToTheGolden(t *testing.T) {
	dir := filepath.Join("testdata", "holdingless")
	for name, got := range holdinglessRenders(t) {
		path := filepath.Join(dir, name+".golden")
		if os.Getenv("TAPPA_WRITE_HOLDINGLESS_GOLDEN") == "1" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: no golden (%v) -- the golden is written from the code BEFORE a change, never after", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its golden (%d bytes vs %d): a business in no holding no longer gets the page it got", name, len(got), len(want))
		}
	}
}
