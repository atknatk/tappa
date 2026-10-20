package handler

// holdingless_view_golden_test.go -- M11 API-1c (the third review's D-4): the reports and anomalies
// pages a business in NO holding gets, through the HANDLER's mapping (fillReportsView,
// fillAnomaliesView) from a domain screen, are byte for byte what they were before API-1c. The
// goldens in testdata/holdingless/ were rendered by this test at 85b4b8d (before API-1c's panels)
// and are never rewritten from later code. web/templates/pages' golden pins the templates; this one
// pins the mapping too -- MUTANT (measured): partnerLine answering a sentence for 0 partner records
// -> red.
//
// RE-RENDERED ON 2026-10-20 FOR THE PANEL SHELL OF DESIGN "THE PASS" (user-approved), which
// changes the chrome around every panel page. Checked before replacing: with the chrome cut off
// (everything outside the content well; before, everything outside the tab bar and </main>), each
// page's own markup is byte for byte the 85b4b8d golden's. The holding claim is unchanged.
// The anomalies golden was replaced once more in the same day's fix round: its one change is the
// sentence naming the reports section by its new label ("The Reports section" -> "The Roll").
// M12-29 (Odd shapes ported to the redesign) replaced the anomalies-view golden: the bytes differ
// only inside the content well, and the holding claim was re-measured on the new page -- the
// same screen with three partner records added renders these bytes plus exactly three
// insertions (the partner sentence, the partner venue's row with its label and no bar, the
// anonymous rule bucket last), and the absent lost-plaque block still adds 0 bytes.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/web/templates/pages"
)

func holdinglessViewRenders(t *testing.T) map[string][]byte {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	period := ledger.WeekOf(ledger.Date{Year: 2026, Month: time.September, Day: 9}, zone)
	at := time.Date(2026, 9, 9, 9, 0, 0, 0, zone)
	emp := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	venue := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	var r ledger.ReportScreen
	r.Queried, r.Period, r.Zone, r.TenantName = true, period, zone, "Holdingless Ltd"
	r.Totals.Worked, r.Totals.Shifts = 8*time.Hour, 1
	r.People = []ledger.PersonHours{{EmployeeID: emp, Name: "Maria Borg", Worked: 8 * time.Hour, Shifts: 1, Daily: make([]time.Duration, ledger.ReportDays)}}
	r.Venues = []ledger.VenueHours{{LocationID: venue, Name: "St Julians", Worked: 8 * time.Hour, Shifts: 1}}

	var a ledger.AnomalyScreen
	a.Queried, a.Period, a.Zone, a.TenantName = true, period, zone, "Holdingless Ltd"
	a.Totals = ledger.AnomalyTotals{Records: 12, Judged: 10, Unanswerable: 1, Answerable: 11, DistanceOnly: 1, OutsideVenue: 1, CounterGaps: 1}
	a.People = []ledger.AnomalyPerson{{EmployeeID: emp, Name: "Maria Borg", Records: 4, DistanceOnly: 1}}
	a.Venues = []ledger.AnomalyVenue{{LocationID: venue, Name: "St Julians", Records: 12, Judged: 10, Answerable: 11, DistanceOnly: 1, OutsideVenue: 1}}
	a.Open = []ledger.AnomalyOpen{{EmployeeName: "Joe", LocationName: "St Julians", At: at}}
	a.Policies = []ledger.AnomalyPolicyRule{{SID: "base:ip-or-gps-ok", Layer: "baseline", Records: 10}}
	a.TogetherWithinSeconds, a.TogetherMinDays = 20, 2
	// 85b4b8d had no AnswerableOwn; with no partner record it IS Answerable (API-1c's base).
	a.Totals.AnswerableOwn = a.Totals.Answerable

	out := map[string][]byte{}
	var b bytes.Buffer
	var rv pages.ReportsView
	fillReportsView(&rv, r)
	if err := pages.AdminReports(rv).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out["reports-view"] = bytes.Clone(b.Bytes())
	b.Reset()
	var av pages.AnomaliesView
	// The page is drawn "now" = the Monday after the week, so the open entry's age is fixed.
	fillAnomaliesView(&av, a, time.Date(2026, 9, 14, 12, 0, 0, 0, zone))
	if err := pages.AdminAnomalies(av).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out["anomalies-view"] = bytes.Clone(b.Bytes())
	return out
}

func TestHoldinglessViews_AreByteIdenticalToTheGolden(t *testing.T) {
	dir := filepath.Join("testdata", "holdingless")
	for name, got := range holdinglessViewRenders(t) {
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
