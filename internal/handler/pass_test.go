package handler

// pass_test.go -- The Pass (M12-26): which requests get it, that every widget claims nothing
// unread and says so when its read failed (§4.6), that the tape draws no future hour, and that
// the view types it adds carry no field a coordinate could travel in (§4.7).

import (
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// ledgerDayHref is a past day's ledger -- the page that draws the FULL docket. Since
// The Pass took /admin alone (M12-26), the docket tests that read the plaque, the counter, the
// notes and the manager's decision read them here: The Pass's rail docket leaves those to the
// ledger by design (components.RailDocket).
var ledgerDayHref = transactionsHref + "?date=2026-08-05"

// TestPass_NoDayIsThePassAndADayIsTheLedger: /admin alone (or with a verdict chip) is The Pass;
// a day, a paging position or any other filter is the ledger, whose filter bar shows what is
// narrowing it.
func TestPass_NoDayIsThePassAndADayIsTheLedger(t *testing.T) {
	cases := []struct {
		path string
		pass bool
	}{
		{transactionsHref, true},
		{transactionsHref + "?verdict=flag", true},
		{transactionsHref + "?date=2026-08-05", false},
		{transactionsHref + "?location=" + uuid.NewString(), false},
		{transactionsHref + "?channel=qr", false},
		{transactionsHref + "?employee=Borg", false},
		{transactionsHref + "?date=not-a-day", false},
	}
	for _, c := range cases {
		b := panelBrowserWith(t, ledgerWithRecords(2, true))
		body := htmlOf(t, b.do(http.MethodGet, c.path, nil))
		isPass := strings.Contains(body, `id="rail-heading"`)
		isLedger := strings.Contains(body, `aria-label="Filter transactions"`)
		if isPass != c.pass || isLedger == c.pass {
			t.Errorf("GET %s: The Pass=%v, ledger=%v; want The Pass=%v", c.path, isPass, isLedger, c.pass)
		}
		if n := strings.Count(body, "<h1"); n != 1 {
			t.Errorf("GET %s: %d <h1>, want exactly one", c.path, n)
		}
		for _, never := range []string{"live", "06:00", "rostered", "not in yet"} {
			if strings.Contains(strings.ToLower(stripTags(body)), never) {
				t.Errorf("GET %s says %q -- not ported (D1; §4.2 for \"live\")", c.path, never)
			}
		}
	}
}

// stripTags is the page's text with the tags dropped, for wording checks that must not trip on
// a class or an attribute.
func stripTags(body string) string {
	return regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(body, " ")
}

// TestPass_EachWidgetClaimsNothingUnreadAndSaysSoWhenItFailed is §4.6 for every figure The Pass
// adds: an unread widget draws nothing, a failed one says it could not be read and prints no
// number, and only a measured one prints its figure -- zero included.
func TestPass_EachWidgetClaimsNothingUnreadAndSaysSoWhenItFailed(t *testing.T) {
	widgets := []struct {
		name     string
		heading  string
		set      func(f *fakeLedger, s ledger.Reading)
		measured string // a figure only the measured state prints
	}{
		{"floor", "On the floor now", func(f *fakeLedger, s ledger.Reading) {
			f.floor = ledger.Floor{State: s}
			if s == ledger.Measured {
				f.floor.People, f.floor.Venues = 7, []ledger.FloorVenue{{Name: "KF Hamrun", People: 7}}
			}
		}, ">7<"},
		{"left open", "Left open since yesterday", func(f *fakeLedger, s ledger.Reading) {
			f.leftOpen = ledger.LeftOpen{State: s, Day: ledger.Date{Year: 2026, Month: 8, Day: 4}}
			if s == ledger.Measured {
				f.leftOpen.N = 3
			}
		}, ">3<"},
		{"tape", "The tape", func(f *fakeLedger, s ledger.Reading) {
			f.tape = ledger.Tape{State: s}
			if s == ledger.Measured {
				f.tape.Hours = []time.Time{time.Now().Truncate(time.Hour)}
				f.tape.Venues = []ledger.TapeVenue{{Name: "KF Msida", People: []int{3}}}
			}
		}, "KF Msida"},
	}
	for _, w := range widgets {
		t.Run(w.name, func(t *testing.T) {
			for _, s := range []ledger.Reading{ledger.NotQueried, ledger.ReadFailed, ledger.Measured} {
				f := ledgerWithRecords(1, true)
				w.set(f, s)
				body := htmlOf(t, panelBrowserWith(t, f).do(http.MethodGet, transactionsHref, nil))
				has := strings.Contains(body, w.heading)
				switch s {
				case ledger.NotQueried:
					if has {
						t.Errorf("an unread %s widget is drawn; it must claim nothing", w.name)
					}
				case ledger.ReadFailed:
					if !has || !strings.Contains(body, "could not be read") {
						t.Errorf("a failed %s widget does not say it could not be read", w.name)
					}
					if strings.Contains(body, w.measured) {
						t.Errorf("a failed %s widget prints %q, a figure it never measured", w.name, w.measured)
					}
				case ledger.Measured:
					if !has || !strings.Contains(body, w.measured) {
						t.Errorf("a measured %s widget does not print %q", w.name, w.measured)
					}
				}
			}
		})
	}
	// THE LEDGER READS NONE OF THEM: a past day is not "now".
	f := ledgerWithRecords(1, true)
	f.floor = ledger.Floor{State: ledger.Measured, People: 7}
	body := htmlOf(t, panelBrowserWith(t, f).do(http.MethodGet, transactionsHref+"?date=2026-08-05", nil))
	for _, h := range []string{"On the floor now", "Left open since yesterday", "The tape", "On the spike"} {
		if strings.Contains(body, h) {
			t.Errorf("the ledger draws The Pass's %q", h)
		}
	}
}

// TestPass_TheTapeDrawsNoFutureHourAndCapsNothingSilently: a bar for every hour that has begun
// and none after it, the current hour in ink, every count in the row's name, and an incomplete
// read said in words.
func TestPass_TheTapeDrawsNoFutureHourAndCapsNothingSilently(t *testing.T) {
	zone := time.FixedZone("Test/Zone", 2*3600)
	now := time.Date(2026, 8, 5, 3, 20, 0, 0, zone)
	midnight := time.Date(2026, 8, 5, 0, 0, 0, 0, zone)
	tape := ledger.Tape{State: ledger.Measured, Incomplete: true,
		Hours:  []time.Time{midnight, midnight.Add(time.Hour), midnight.Add(2 * time.Hour), midnight.Add(3 * time.Hour)},
		Venues: []ledger.TapeVenue{{Name: "KF Hamrun", People: []int{2, 0, 4, 1}}}}
	v := tapeView(tape, now)
	if v.Columns != 24 || len(v.Axis) != 24 || v.Axis[0] != "00" || v.Axis[3] != "03" || v.Axis[1] != "" {
		t.Fatalf("the axis is not today's 24 hours labelled every three: %d %q", v.Columns, v.Axis)
	}
	row := v.Rows[0]
	if len(row.Bars) != 4 {
		t.Fatalf("%d bars for 4 hours that have begun -- a future hour is drawn or a past one lost", len(row.Bars))
	}
	if !row.Bars[3].Current || row.Bars[2].Current || row.Bars[1].Bucket != 0 || row.Bars[2].Bucket != 10 || row.Bars[3].Bucket != 3 {
		t.Errorf("bars %+v: want the last current, the empty hour bucket 0, the busiest 10, 1 of 4 rounded up to 3", row.Bars)
	}
	if row.Label != "KF Hamrun, people on shift by hour: 00:00 2, 02:00 4, 03:00 1" {
		t.Errorf("row label %q does not carry every count", row.Label)
	}
	f := ledgerWithRecords(1, true)
	f.tape = tape
	html := htmlOf(t, panelBrowserWith(t, f).do(http.MethodGet, transactionsHref, nil))
	if strings.Count(html, "tape-bar--") != 3 || !strings.Contains(html, "The tape is incomplete") {
		t.Errorf("the tape draws %d bars (want 3: one hour is empty) or does not say it is incomplete", strings.Count(html, "tape-bar--"))
	}
	// A clock-change day keeps its own number of columns.
	if loc, err := time.LoadLocation("Europe/Malta"); err == nil {
		v := tapeView(ledger.Tape{State: ledger.Measured}, time.Date(2026, 10, 25, 12, 0, 0, 0, loc))
		if v.Columns != 25 {
			t.Errorf("the day the clocks go back has %d columns, want 25", v.Columns)
		}
	}
}

// TestPass_TheFloorTicksAreCappedAndTheNumberIsTheFigure: at most 20 ticks, the number beside
// them exact, and the partner and unplaced rows named in the dockets' words.
func TestPass_TheFloorTicksAreCappedAndTheNumberIsTheFigure(t *testing.T) {
	v := floorView(ledger.Floor{State: ledger.Measured, People: 33, Venues: []ledger.FloorVenue{
		{Name: "KF Valletta", People: 31}, {Name: "KM Plant", Partner: true, People: 1}, {Unplaced: true, People: 1},
	}})
	if v.People != "33" || len(v.Rows) != 3 {
		t.Fatalf("floor %+v", v)
	}
	if v.Rows[0].Ticks != strings.Repeat("|", 20) || v.Rows[0].People != "31" {
		t.Errorf("31 people drew %d ticks and %q; want 20 ticks and 31", len(v.Rows[0].Ticks), v.Rows[0].People)
	}
	if v.Rows[1].Name != "KM Plant · partner venue" || v.Rows[2].Name != "No location recorded" {
		t.Errorf("partner/unplaced rows named %q, %q", v.Rows[1].Name, v.Rows[2].Name)
	}
}

// TestPass_TheSpikeCardIsTheBadgesFigure: "0" only for a queue measured empty, "?" when it was
// not measured, "N+" when capped.
func TestPass_TheSpikeCardIsTheBadgesFigure(t *testing.T) {
	for _, c := range []struct {
		badge pages.PendingBadge
		want  string
		state pages.WidgetState
	}{
		{pages.PendingBadge{}, "?", pages.WidgetFailed},
		{pages.PendingBadge{Known: true}, "0", pages.WidgetRead},
		{pages.PendingBadge{Known: true, N: 100, Capped: true}, "100+", pages.WidgetRead},
	} {
		if got := spikeView(c.badge); got.Count != c.want || got.State != c.state || got.Href != reviewHref {
			t.Errorf("badge %+v -> %+v, want %q", c.badge, got, c.want)
		}
	}
	if got := leftOpenView(ledger.LeftOpen{State: ledger.Measured, N: 50, Capped: true, Day: ledger.Date{Year: 2026, Month: 8, Day: 4}}); got.Count != "50+" || got.OddHref != anomaliesHref+"?week=2026-08-04" {
		t.Errorf("left open %+v", got)
	}
}

// TestPass_ThePlaqueCardIsTheVenuesRead: mounted plaques only, newest seen first and "never"
// last, at most eight, the uid's last four, the key alert in the Venues section's words, and a
// failed read said rather than an empty list.
func TestPass_ThePlaqueCardIsTheVenuesRead(t *testing.T) {
	loc := uuid.New()
	at := func(h int) *time.Time { x := time.Date(2026, 8, 5, h, 0, 0, 0, time.UTC); return &x }
	enc := at(1)
	var plaques []tenant.Plaque
	plaques = append(plaques,
		tenant.Plaque{UID: "04A1B2C3D491AC", Status: tenant.PlaqueActive, LocationID: &loc, EncodedAt: enc},     // never seen
		tenant.Plaque{UID: "04A1B2C3D47E55", Status: tenant.PlaqueActive, LocationID: &loc, LastSeen: at(14)},   // not encoded: alert
		tenant.Plaque{UID: "04A1B2C3D40000", Status: tenant.PlaqueUnassigned, LastSeen: at(15), EncodedAt: enc}, // stock: not listed
		tenant.Plaque{UID: "04A1B2C3D40C21", Status: tenant.PlaqueActive, LocationID: &loc, LastSeen: at(9), EncodedAt: enc},
	)
	fp := &fakePlaques{screen: tenant.PlaqueScreen{Plaques: plaques, Zone: time.UTC}}
	a := &AdminAuth{plaques: fp, log: discardLogger()}
	v := a.passPlaques(t.Context(), panelTestIdentity(), []ledger.Option{{ID: loc, Label: "KF Valletta"}})
	if v.State != pages.WidgetRead || len(v.Rows) != 3 || v.More {
		t.Fatalf("plaque card %+v", v)
	}
	if v.Rows[0].Plaque != "PLAQUE 7E55" || !v.Rows[0].Replace || v.Rows[1].Plaque != "PLAQUE 0C21" || v.Rows[2].Seen != "never" || v.Rows[0].Venue != "KF Valletta" {
		t.Errorf("rows %+v: want 7E55 (alert) then 0C21 then the never-seen one", v.Rows)
	}
	for i := 0; i < 9; i++ {
		fp.screen.Plaques = append(fp.screen.Plaques, tenant.Plaque{UID: "04A1B2C3D4AAA" + string(rune('0'+i)), Status: tenant.PlaqueActive, LocationID: &loc, EncodedAt: enc})
	}
	if v := a.passPlaques(t.Context(), panelTestIdentity(), nil); len(v.Rows) != pages.PassPlaqueRows || !v.More || v.Rows[0].Venue != "Venue could not be read" {
		t.Errorf("12 mounted plaques: %d rows, more=%v, venue %q", len(v.Rows), v.More, v.Rows[0].Venue)
	}
	fp.screenErr = errors.New("database is unreachable")
	if v := a.passPlaques(t.Context(), panelTestIdentity(), nil); v.State != pages.WidgetFailed || len(v.Rows) != 0 {
		t.Errorf("a failed plaque read gave %+v", v)
	}
}

// panelTestIdentity is panelBrowser's signed-in owner, for a method called without a request.
func panelTestIdentity() httpx.AdminIdentity {
	return httpx.AdminIdentity{State: httpx.AdminLive, Admin: adminauth.Resolved{
		SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin, Role: "owner", FullName: "KF Owner",
	}}
}

// TestPassViews_CarryNothingACoordinateCouldTravelIn is §4.7 on the view types The Pass and the
// ledger add: no field whose name a coordinate, an address or a secret would arrive under, and
// every leaf a string, a count, a flag or a state.
func TestPassViews_CarryNothingACoordinateCouldTravelIn(t *testing.T) {
	seen := map[reflect.Type]bool{}
	fields := 0
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] || rt == reflect.TypeOf(components.DocketView{}) {
			return // DocketView is walked by TestTypes_NothingOnTheReadPathCanCarryACoordinate
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			fields++
			if why := bannedFieldName(f.Name); why != "" {
				t.Errorf("%s.%s: %s", rt.Name(), f.Name, why)
			}
			switch k := f.Type.Kind(); k {
			case reflect.String, reflect.Bool, reflect.Int, reflect.Uint8:
			case reflect.Struct, reflect.Slice:
				walk(f.Type)
			default:
				t.Errorf("%s.%s is a %s -- a Pass view holds words, counts, flags and states", rt.Name(), f.Name, f.Type)
			}
		}
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(pages.VerdictChip{}), reflect.TypeOf(pages.FloorView{}),
		reflect.TypeOf(pages.SpikeView{}), reflect.TypeOf(pages.LeftOpenView{}), reflect.TypeOf(pages.TapeView{}),
		reflect.TypeOf(pages.PassPlaquesView{}),
	} {
		walk(typ)
	}
	if fields < 25 {
		t.Fatalf("the walk saw %d fields; it is reading the wrong types", fields)
	}
}
