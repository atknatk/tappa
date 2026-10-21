package handler

// pass.go -- the redesign's two layouts of the transactions section (M12-26 The Pass, M12-27 the
// past day): which one a request gets, and the view pieces only they draw -- the verdict chips,
// the day's summary line, the hour groups, the day stepper and The Pass's widgets.
//
// 🔴 EVERY FIGURE KEEPS ITS STATE (§4.6). A widget the ledger did not read claims nothing, one
// whose read failed says "could not be read" ("?" in place of its number), and only a measured
// one prints a number -- zero included. The mapping below never turns a missing figure into 0.
//
// 🔴 NOTHING HERE READS EVIDENCE. The widgets carry counts and venue names (the domain's own
// walker, TestPassReads_CarryNothingACoordinateCouldTravelIn, holds that), the dockets are the
// ones docketView already builds, and a partner venue's row says only that it is one (K-39 is
// applied in the ledger, never here).

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// floorTickCap is the most ticks one venue's row draws; the number beside it carries the count.
const floorTickCap = 20

// isPass reports whether a request is for The Pass: /admin with no day, no paging position, and
// nothing narrowing the list but a verdict chip. Anything else is a ledger page -- a bookmark that
// names a venue keeps its filter bar, so a narrowed list never shows without the controls that
// narrowed it.
func isPass(r *http.Request, f ledger.Filter) bool {
	return r.URL.Query().Get("date") == "" && f.Date.Zero() && f.After == nil &&
		f.LocationID == nil && f.DepartmentID == nil && f.EmployeeName == "" && f.Channel == ""
}

// fillPass puts The Pass's heading and widgets on the view.
func (a *AdminAuth) fillPass(ctx context.Context, v *pages.TransactionsView, screen ledger.Screen, id httpx.AdminIdentity) {
	zone := screen.Zone
	if zone == nil {
		zone = time.UTC
	}
	now := screen.AsOf
	if now.IsZero() {
		now = time.Now()
	}
	local := now.In(zone)
	// THE KICKER IS THE MOMENT THE PAGE MEASURED, AND THE FULL ZONE (pass-ledger.md P1): the page
	// is no-store and nothing on it refreshes itself (§4.2 -- no "live"), so the time says how old
	// the figures are.
	v.Kicker = local.Format("Mon 2 Jan · 15:04") + " · " + zone.String()
	v.Today = true
	v.Floor = floorView(screen.Floor)
	v.Spike = spikeView(v.PanelChrome.Pending)
	v.LeftOpen = leftOpenView(screen.LeftOpen)
	v.Tape = tapeView(screen.Tape, local)
	v.PassPlaques = a.passPlaques(ctx, id, screen.Options.Locations)
	v.WholeDayHref = transactionsHref + "?date=" + screen.Page.Day.String()
}

// fillLedger puts the past day's heading on the view: the title with the day, the kicker, and
// the stepper.
func fillLedger(v *pages.TransactionsView, page ledger.Page, f ledger.Filter, today time.Time) {
	zone := page.Zone
	if zone == nil {
		zone = time.UTC
	}
	day := page.Day
	start := time.Date(day.Year, day.Month, day.Day, 0, 0, 0, 0, zone)
	v.DayShort = start.Format("Mon 2 Jan")
	v.Title = pages.SectionLabel(pages.TabTransactions) + " · " + v.DayShort
	// "records are never edited" is §4.3 stated where the reader looks at them.
	v.Kicker = v.DayLabel + " · " + v.ZoneLabel + " · records are never edited"

	prev := start.AddDate(0, 0, -1)
	v.PrevDayHref = transactionsHref + "?" + filterQuery(f, dateOf(prev)).Encode()
	// A day that has not begun has nothing to show yet, so the stepper stops at today.
	t := today.In(zone)
	todayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, zone)
	if next := start.AddDate(0, 0, 1); !next.After(todayStart) {
		v.NextDayHref = transactionsHref + "?" + filterQuery(f, dateOf(next)).Encode()
	}
	v.PassHref = transactionsHref
}

func dateOf(t time.Time) ledger.Date {
	return ledger.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

// verdictChips are the five verdict links: All, then panelVerdicts' own four, in its order and
// with its labels -- the slice the validator reads, so a chip cannot offer a value the handler
// would drop. On The Pass a chip is /admin?verdict=…; on the ledger it keeps the day and every
// other filter. The cursor is never carried: a new filter starts at the newest record.
func verdictChips(f ledger.Filter, day ledger.Date, pass bool, tally ledger.VerdictTally) []pages.VerdictChip {
	href := func(verdict string) string {
		q := filterQuery(f, day)
		if pass {
			q = url.Values{}
		}
		q.Del("verdict")
		if verdict != "" {
			q.Set("verdict", verdict)
		}
		if len(q) == 0 {
			return transactionsHref
		}
		return transactionsHref + "?" + q.Encode()
	}
	count := func(n int) string {
		if !tally.State.Known() {
			return ""
		}
		return strconv.Itoa(n)
	}
	out := []pages.VerdictChip{{Label: "All", Count: count(tally.Records), Href: href(""), Current: f.Verdict == ""}}
	for _, o := range panelVerdicts {
		n := 0
		switch o.Value {
		case "ok":
			n = tally.Approved
		case "flag":
			n = tally.Flagged
		case "reject":
			n = tally.Rejected
		case "ignored":
			n = tally.Ignored
		}
		out = append(out, pages.VerdictChip{Label: o.Label, Count: count(n), Href: href(o.Value), Current: f.Verdict == o.Value})
	}
	return out
}

// tallySummary is the ledger's count line: "10 records · 6 approved · 2 flagged · 1 rejected ·
// 0 ignored · 1 manual entry". "" when the tally was not measured -- the line is then not drawn.
func tallySummary(t ledger.VerdictTally) string {
	if !t.State.Known() {
		return ""
	}
	return strings.Join([]string{
		plural(t.Records, "record", "records"),
		strconv.Itoa(t.Approved) + " approved",
		strconv.Itoa(t.Flagged) + " flagged",
		strconv.Itoa(t.Rejected) + " rejected",
		strconv.Itoa(t.Ignored) + " ignored",
		plural(t.Manual, "manual entry", "manual entries"),
	}, " · ")
}

// hourGroups groups a page's records by the hour they happened in, in the tenant's zone, newest
// hour first (the page's own order). after is the cursor the page started from: when the first
// record falls in the same hour as the last one the reader was already shown, the group says it
// is continued rather than pretending the hour starts here.
//
// AN HOUR IS KEYED BY ITS LOCAL DATE, HOUR AND OFFSET, so the hour a clock change repeats is two
// groups, not one group holding both.
func hourGroups(page ledger.Page, after *ledger.Cursor, dockets []components.DocketView) []pages.HourGroup {
	zone := page.Zone
	if zone == nil {
		zone = time.UTC
	}
	type hourKey struct {
		y, d, h, off int
		m            time.Month
	}
	keyOf := func(t time.Time) hourKey {
		l := t.In(zone)
		_, off := l.Zone()
		return hourKey{y: l.Year(), m: l.Month(), d: l.Day(), h: l.Hour(), off: off}
	}
	var out []pages.HourGroup
	var last hourKey
	for i, rec := range page.Records {
		k := keyOf(rec.OccurredAt)
		if len(out) == 0 || k != last {
			l := rec.OccurredAt.In(zone)
			g := pages.HourGroup{
				Hour:    fmt.Sprintf("%02d:00", l.Hour()),
				Weekday: l.Format("Mon"),
				Anchor:  "hour-" + l.Format("20060102-1504") + "-" + strconv.Itoa(i),
			}
			if len(out) == 0 && after != nil && keyOf(after.At) == k {
				g.Continued = true
				g.Anchor += "-" + strconv.FormatInt(after.At.UnixNano(), 36)
			}
			out = append(out, g)
			last = k
		}
		out[len(out)-1].Dockets = append(out[len(out)-1].Dockets, dockets[i])
	}
	return out
}

// floorView maps "on the floor now".
func floorView(f ledger.Floor) pages.FloorView {
	switch f.State {
	case ledger.Measured:
	case ledger.ReadFailed:
		return pages.FloorView{State: pages.WidgetFailed, People: "?"}
	default:
		return pages.FloorView{}
	}
	out := pages.FloorView{State: pages.WidgetRead, People: strconv.Itoa(f.People)}
	for _, fv := range f.Venues {
		ticks := fv.People
		if ticks > floorTickCap {
			ticks = floorTickCap
		}
		if ticks < 0 {
			ticks = 0
		}
		out.Rows = append(out.Rows, pages.FloorRow{
			Name:   passVenueName(fv.Name, fv.Partner, fv.Unplaced),
			Ticks:  strings.Repeat("|", ticks),
			People: strconv.Itoa(fv.People),
		})
	}
	return out
}

// passVenueName is a widget row's venue, in the words the dockets use for the same cases.
func passVenueName(name string, partner, unplaced bool) string {
	switch {
	case unplaced || name == "":
		return "No location recorded"
	case partner:
		return name + " · partner venue"
	default:
		return name
	}
}

// spikeView reads the queue's size from the chrome's badge, so the card and the navigation say
// the same number. "0" is printed only for a queue measured empty.
func spikeView(p pages.PendingBadge) pages.SpikeView {
	if !p.Known {
		return pages.SpikeView{State: pages.WidgetFailed, Count: "?", Href: reviewHref}
	}
	n := p.N
	if n < 0 {
		n = 0
	}
	c := strconv.Itoa(n)
	if p.Capped {
		c += "+"
	}
	return pages.SpikeView{State: pages.WidgetRead, Count: c, Href: reviewHref}
}

// leftOpenView maps "left open since yesterday", with its link to yesterday's week on Odd shapes.
func leftOpenView(l ledger.LeftOpen) pages.LeftOpenView {
	odd := anomaliesHref
	if !l.Day.Zero() {
		odd = anomaliesHref + "?week=" + l.Day.String()
	}
	switch l.State {
	case ledger.Measured:
		c := strconv.Itoa(l.N)
		if l.Capped {
			c += "+"
		}
		return pages.LeftOpenView{State: pages.WidgetRead, Count: c, OddHref: odd}
	case ledger.ReadFailed:
		return pages.LeftOpenView{State: pages.WidgetFailed, Count: "?", OddHref: odd}
	default:
		return pages.LeftOpenView{}
	}
}

// tapeView maps the hourly tape onto today's grid: one column per hour of the day in the zone
// (23, 24 or 25), a bar for every hour that has begun, none after. Heights are ten buckets of
// the tape's busiest hour; the row's label carries every count.
func tapeView(t ledger.Tape, now time.Time) pages.TapeView {
	switch t.State {
	case ledger.Measured:
	case ledger.ReadFailed:
		return pages.TapeView{State: pages.WidgetFailed}
	default:
		return pages.TapeView{}
	}
	zone := now.Location()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, zone)
	out := pages.TapeView{State: pages.WidgetRead, Incomplete: t.Incomplete, Columns: int(next.Sub(midnight) / time.Hour)}
	for i := 0; i < out.Columns; i++ {
		h := midnight.Add(time.Duration(i) * time.Hour).In(zone).Hour()
		label := ""
		if h%3 == 0 {
			label = fmt.Sprintf("%02d", h)
		}
		out.Axis = append(out.Axis, label)
	}
	most := 0
	for _, tv := range t.Venues {
		for _, n := range tv.People {
			if n > most {
				most = n
			}
		}
	}
	for _, tv := range t.Venues {
		name := passVenueName(tv.Name, tv.Partner, tv.Unplaced)
		row := pages.TapeRow{Name: name}
		var said []string
		for i, n := range tv.People {
			if i >= out.Columns {
				break
			}
			bar := pages.TapeBar{Bucket: tapeBucket(n, most), Current: i == len(t.Hours)-1}
			row.Bars = append(row.Bars, bar)
			if n > 0 && i < len(t.Hours) {
				said = append(said, t.Hours[i].In(zone).Format("15:04")+" "+strconv.Itoa(n))
			}
		}
		if len(said) == 0 {
			row.Label = name + ": nobody on shift today so far"
		} else {
			row.Label = name + ", people on shift by hour: " + strings.Join(said, ", ")
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}

// tapeBucket is n's bar height in ten steps of the busiest hour: 0 draws no bar, any person at all
// draws at least the shortest one. Integer arithmetic, rounded up.
func tapeBucket(n, most int) int {
	if n <= 0 || most <= 0 {
		return 0
	}
	b := (n*10 + most - 1) / most
	if b > 10 {
		b = 10
	}
	return b
}

// passPlaques is The Pass's plaque card: the plaques on a wall, newest last-seen first and the
// never-seen last, at most pages.PassPlaqueRows. It reuses the Venues section's own read
// (tenant.Plaques.Screen) in its own transaction; a failure costs the card, never the page.
func (a *AdminAuth) passPlaques(ctx context.Context, id httpx.AdminIdentity, venues []ledger.Option) pages.PassPlaquesView {
	out := pages.PassPlaquesView{
		Href:  pages.SectionHref(pages.TabLocations),
		Label: pages.SectionLabel(pages.TabLocations),
	}
	screen, err := a.plaques.Screen(ctx, id.TenantID())
	if err != nil {
		a.log.Warn("panel: could not read the plaques for The Pass; the card says so", "err", err)
		out.State = pages.WidgetFailed
		return out
	}
	out.State = pages.WidgetRead
	names := make(map[string]string, len(venues))
	for _, o := range venues {
		names[o.ID.String()] = o.Label
	}
	var walls []tenant.Plaque
	for _, p := range screen.Plaques {
		if p.OnAWall() {
			walls = append(walls, p)
		}
	}
	sort.SliceStable(walls, func(i, j int) bool {
		a, b := walls[i].LastSeen, walls[j].LastSeen
		switch {
		case a == nil:
			return false
		case b == nil:
			return true
		default:
			return a.After(*b)
		}
	})
	if len(walls) > pages.PassPlaqueRows {
		walls, out.More = walls[:pages.PassPlaqueRows], true
	}
	for _, p := range walls {
		venue := "Venue could not be read"
		if p.LocationID != nil {
			if n := names[p.LocationID.String()]; n != "" {
				venue = n
			}
		}
		seen := plaqueStamp(p.LastSeen, screen.Zone)
		if seen == "" {
			seen = "never"
		}
		tail := p.UID
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		out.Rows = append(out.Rows, pages.PassPlaqueRow{
			Venue:   venue,
			Plaque:  "PLAQUE " + strings.ToUpper(tail),
			Seen:    seen,
			Replace: keyAlertOf(p) == "wall",
		})
	}
	return out
}
