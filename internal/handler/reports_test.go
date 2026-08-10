package handler

// reports_test.go -- M6-07 phase A's unit-level nets.
//
// WHAT IS HERE AND WHAT IS IN reports_db_test.go. §4.5 and the query's own shape are
// measured against real Postgres, because a fake reader can only ever confirm what the
// test told it. What is here is what a fake CAN prove: the boundary between what the
// URL asks for and what the domain is handed, the sentences the screen is obliged to
// print and the ones it is forbidden to, the formatting of a payroll figure, and the
// property that this section offers NO WRITE.

import (
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/web/templates/pages"
)

// reportBrowser is panelBrowser wired to a reader the caller controls.
func reportBrowser(t *testing.T, records *fakeLedger) *browser {
	t.Helper()
	return panelBrowserWithRoster(t, records)
}

// weekOfUTC is the fixture week every case below reports on: Monday 3 to Sunday 9
// August 2026. The fake reader answers in UTC, so the period is built in UTC too --
// the zone arithmetic itself is internal/domain/ledger's to prove, and duplicating it
// here would be a second implementation of the thing under test.
func weekOfUTC() ledger.Period {
	return ledger.WeekOf(ledger.Date{Year: 2026, Month: time.August, Day: 5}, time.UTC)
}

func reportWith(mutate func(*ledger.Report)) ledger.ReportScreen {
	rep := ledger.Report{Queried: true, Zone: time.UTC, Period: weekOfUTC()}
	if mutate != nil {
		mutate(&rep)
	}
	return ledger.ReportScreen{Report: rep, TenantName: "Kebab Factory Ltd"}
}

// TestReportsSection_AsksTheDomainForTheWeekTheURLNAMES is the boundary §7 asks for:
// the handler validates, and the domain sees data that is already good.
func TestReportsSection_AsksTheDomainForTheWeekTheURLNAMES(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  ledger.Date
	}{
		{name: "a valid ISO day", query: "?week=2026-08-05", want: ledger.Date{Year: 2026, Month: time.August, Day: 5}},
		{
			// DROPPED RATHER THAN REFUSED, which shows the tenant's current week instead
			// of an error page. It is safe because it can only move the reader inside
			// their own tenant, and it is visible because the picker echoes the week that
			// took effect.
			name: "a date that does not parse", query: "?week=not-a-date", want: ledger.Date{},
		},
		{name: "no week at all", query: "", want: ledger.Date{}},
		{
			// A hand-edited URL cannot reach another tenant's records by naming a week:
			// the tenant is not a parameter of this request at all.
			name: "a hostile value", query: "?week=2026-08-05%27%20OR%201=1", want: ledger.Date{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			records := newFakeLedger()
			records.hours = reportWith(nil)
			b := reportBrowser(t, records)
			if rec := b.do(http.MethodGet, reportsHref+c.query, nil); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := records.lastHoursFilter(t).Day; got != c.want {
				t.Fatalf("the domain was asked for %v, want %v", got, c.want)
			}
			// §4.5: the tenant comes from the session and from nowhere else.
			if len(records.hoursTenant) == 0 || records.hoursTenant[0] != panelTestTenant {
				t.Fatalf("the read was scoped to %v, want the session's tenant", records.hoursTenant)
			}
		})
	}
}

// TestReportsSection_SaysTheReadFailedRatherThanShowingAnEmptyWeek is §4.6 at the
// handler boundary: a timeout is not evidence that nobody worked.
func TestReportsSection_SaysTheReadFailedRatherThanShowingAnEmptyWeek(t *testing.T) {
	records := newFakeLedger()
	records.hoursErr = errors.New("connection refused")
	b := reportBrowser(t, records)
	rec := b.do(http.MethodGet, reportsHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 -- a failed read must not render as a week of zero hours", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "0h 00m") {
		t.Fatal("the error page printed a payroll figure")
	}
}

// TestReportsSection_PrintsEveryFigureItHoldsBackFromTheTotal is §4.6 on the screen
// rather than in the domain: hours excluded from payroll must still be visible, with
// the sentence that says the total is short.
func TestReportsSection_PrintsEveryFigureItHoldsBackFromTheTotal(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{
			Worked:   40 * time.Hour,
			Shifts:   5,
			Awaiting: 3*time.Hour + 30*time.Minute,
			Refused:  time.Hour,
			Open:     2,
			TooLong:  1,
		}
		r.PracticeTaps = 4
		r.StartedEarlier = 2
		r.Unattributable = 1
		r.Open = []ledger.OpenEntry{
			{EmployeeName: "Maria Borg", LocationName: "KF St Julians", At: weekOfUTC().From.Add(34 * time.Hour), Stale: true},
			{EmployeeName: "Antoine Vella", LocationName: "Rusty Bar", At: weekOfUTC().From.Add(140 * time.Hour)},
		}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()

	// Each of these is a quantity the payroll figure does NOT include. Every one of
	// them has to be somewhere on the page, or the total silently disagrees with the
	// transactions list.
	for _, want := range []string{
		"40h 00m",     // the total itself
		"3h 30m",      // awaiting a decision
		"1h 00m",      // refused by a manager
		"2 check-ins", // open
		"4 training taps, not counted",
		"2 checkouts belong to shifts that began before this week",
		"1 record in this week names no employee",
		"treated as a missed checkout rather than a shift",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page never says %q", want)
		}
	}
	// 🔴 THE SENTENCE Q18 REQUIRES. Counting an unclosed entry as zero hours would
	// under-pay somebody silently; the screen has to say the figure is short.
	if !strings.Contains(body, "SHORT by whatever these add up to") {
		t.Error("the report does not say its total is short, so an unclosed entry reads as zero hours")
	}
}

// TestReportsSection_TheOpenListDoesNotClaimWhyAnEntryIsOpen is the M6-07 card's
// explicit limit: a forgotten checkout and a pre-ADR-0008 masked check-in are the same
// shape on disk, so the screen must not tell them apart.
func TestReportsSection_TheOpenListDoesNotClaimWhyAnEntryIsOpen(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals.Open = 1
		r.Open = []ledger.OpenEntry{{
			EmployeeName: "Maria Borg", LocationName: "KF St Julians",
			At: weekOfUTC().From.Add(30 * time.Hour), Stale: true,
		}}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()

	if !strings.Contains(body, "A row cannot say why it is open") {
		t.Error("the open list does not state its own limit")
	}
	if !strings.Contains(body, "does not invent a checkout") {
		t.Error("the open list does not state the Q18 rule it exists to implement")
	}
	// 🔴 AND THE ROW ITSELF SAYS ONLY WHAT IT CAN MEASURE. Scanning the whole page for
	// forbidden words was the first shape of this assertion and it was WRONG in a way
	// worth recording: it flagged the section's own honest sentence, which contains the
	// phrase "forgot to tap out" precisely in order to say the product cannot tell.
	// Refusing a word rather than a claim would have forced the explanation off the
	// page. So the check is on the ROW's chips, which are the only thing on a row that
	// asserts anything, and the allowed set is closed.
	tail := body[strings.Index(body, "Open — no checkout"):]
	chips := tallyRE.FindAllStringSubmatch(tail, -1)
	if len(chips) == 0 {
		t.Fatal("no chips found in the open list; this scan is reading nothing")
	}
	allowed := map[string]bool{"Needs action": true, "Still open": true, "Entered by a manager": true}
	for _, c := range chips {
		if !allowed[strings.TrimSpace(c[1])] {
			t.Errorf("an open row is labelled %q, which is a claim about WHY it is open; "+
				"the row can only measure how long", strings.TrimSpace(c[1]))
		}
	}
}

// tallyRE reads the text of every chip on a page. It is the open list's negative
// control: without it, "the row claims nothing" would be asserted over no rows.
var tallyRE = regexp.MustCompile(`(?is)<span class="tally[^"]*">([^<]*)</span>`)

// TestReportsSection_RefusesToPresentATruncatedReadAsATotal is the §4.6 rule applied
// to the one failure this section can have that still LOOKS like an answer.
func TestReportsSection_RefusesToPresentATruncatedReadAsATotal(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Truncated = true
		r.Totals = ledger.Totals{Worked: 12 * time.Hour, Shifts: 2}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()
	if !strings.Contains(body, "These totals are incomplete") {
		t.Fatal("a truncated read rendered as an ordinary week")
	}
	if !strings.Contains(body, "a floor and not a total") {
		t.Error("the warning does not say what is wrong with the number below it")
	}

	// POSITIVE CONTROL: the same page without truncation must NOT carry the warning,
	// or the assertion above would pass on every render.
	clean := newFakeLedger()
	clean.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: 12 * time.Hour, Shifts: 2}
	})
	if strings.Contains(reportBrowser(t, clean).do(http.MethodGet, reportsHref, nil).Body.String(), "These totals are incomplete") {
		t.Error("a complete read also carries the incomplete warning, so the warning means nothing")
	}
}

// TestReportsSection_ListsAtMostMaxReportRowsAndSaysSo is the page-size bound, and the
// property that makes it acceptable: the cap is on the LISTING and never on the sum.
func TestReportsSection_ListsAtMostMaxReportRowsAndSaysSo(t *testing.T) {
	const people = maxReportRows + 31
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Duration(people) * time.Hour, Shifts: people}
		for i := 0; i < people; i++ {
			r.People = append(r.People, ledger.PersonHours{
				Name:   "Employee " + pad2(i),
				Worked: time.Hour,
				Shifts: 1,
				Daily:  make([]time.Duration, ledger.ReportDays),
			})
		}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()

	if got := strings.Count(body, `<section class="docket">`); got > maxReportRows+8 {
		t.Fatalf("%d dockets on the page; the listing is not bounded", got)
	}
	if !strings.Contains(body, "Showing 100 of 131 people") {
		t.Error("the page does not say how many rows it left out")
	}
	// 🔴 THE TOTAL IS STILL EVERYBODY'S. This is the assertion that separates a bounded
	// LISTING from a truncated REPORT, and without it the cap would be indistinguishable
	// from the defect ledger.ReportEventCap exists to refuse.
	if !strings.Contains(body, "131h 00m") {
		t.Error("the total does not cover the people the listing left out")
	}
	if !strings.Contains(body, "totals above cover") {
		t.Error("the page does not say the cap is on the listing rather than on the hours")
	}
}

// TestReportsSection_BoundsTheOpenListAndStillCountsAllOfIt is the second cap, and it
// exists because the first probe of this section measured only the first.
//
// 🔴 AN OPEN CHECK-IN LIST GROWS WITH THE BACKLOG, NOT WITH THE PAYROLL, so it has no
// ceiling of its own: measured on the development database, one week of the seed tenant
// held 1 012 of them and they were 68% of a 559 968 B page. The property that makes a
// cap acceptable is the same one the people cap needs -- the TALLY still counts every
// entry -- and it is asserted here rather than assumed.
func TestReportsSection_BoundsTheOpenListAndStillCountsAllOfIt(t *testing.T) {
	const open = maxOpenRows + 57
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals.Open = open
		for i := 0; i < open; i++ {
			r.Open = append(r.Open, ledger.OpenEntry{
				EmployeeName: "Open Worker " + pad2(i),
				LocationName: "KF St Julians",
				At:           weekOfUTC().From.Add(time.Duration(i) * time.Minute),
				Stale:        true,
			})
		}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()

	tail := body[strings.Index(body, "Open — no checkout"):]
	if got := strings.Count(tail, `<section class="docket">`); got != maxOpenRows {
		t.Fatalf("the open list printed %d entries, want %d", got, maxOpenRows)
	}
	if !strings.Contains(body, "Showing the 100 oldest of 157 open check-ins") {
		t.Error("the page does not say how many open entries it left out")
	}
	// 🔴 THE TALLY IS THE WHOLE BACKLOG. Without this the cap would be indistinguishable
	// from losing records, which is the §4.6 line this section is not allowed to cross.
	if !strings.Contains(body, "157 check-ins") {
		t.Error("the open tally counts the page rather than the backlog")
	}
	// AND THE OLDEST ARE THE ONES KEPT, because that is the order a backlog is worked.
	if !strings.Contains(tail, "Open Worker 00") || strings.Contains(tail, "Open Worker 156") {
		t.Error("the shortened list is not the oldest entries")
	}
}

// TestReportsSection_OffersNoWriteAtAll is the §4.3 boundary, and it is the assertion
// this phase is allowed to make: phase A totals the immutable record and changes
// nothing, so a control that posts anywhere would be a control the server does not
// implement.
func TestReportsSection_OffersNoWriteAtAll(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Hour, Shifts: 1, Open: 1}
		r.Open = []ledger.OpenEntry{{EmployeeName: "Maria Borg", At: weekOfUTC().From}}
		r.People = []ledger.PersonHours{{Name: "Maria Borg", Worked: time.Hour, Shifts: 1, Daily: make([]time.Duration, ledger.ReportDays)}}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()

	// The panel shell carries the sign-out form, which posts; nothing this SECTION
	// renders may. Counting is what separates the two, and the number is derived
	// rather than written down: the same page rendered by a section with no data has
	// the shell's forms and no others.
	empty := newFakeLedger()
	empty.hours = reportWith(nil)
	shellForms := strings.Count(reportBrowser(t, empty).do(http.MethodGet, reportsHref, nil).Body.String(), `method="post"`)
	if got := strings.Count(body, `method="post"`); got != shellForms {
		t.Fatalf("%d posting forms on a filled report against %d on the shell alone; "+
			"phase A writes nothing", got, shellForms)
	}
	if strings.Contains(body, "csrf") {
		t.Error("a synchronizer token is rendered on a section that submits nothing")
	}
}

// TestReportsSection_EveryControlLeadsSomewhereThatExists drives every link the page
// offers through the SAME router the page came from.
//
// A CONTROL THAT 404s IS THIS REPOSITORY'S MOST EXPENSIVE CLASS OF MISTAKE, and the
// week arrows are exactly the shape that produces it: they are built from a date the
// handler computed rather than from a literal.
func TestReportsSection_EveryControlLeadsSomewhereThatExists(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(nil)
	b := reportBrowser(t, records)
	body := b.do(http.MethodGet, reportsHref, nil).Body.String()

	links := sameOriginHrefs(body)
	if len(links) < 4 {
		t.Fatalf("only %d in-product links on the page (%v); the week controls are missing, "+
			"so this scan would pass over nothing", len(links), links)
	}
	for _, href := range links {
		if strings.HasPrefix(href, "/static/") {
			continue
		}
		if rec := b.do(http.MethodGet, href, nil); rec.Code != http.StatusOK {
			t.Errorf("%s answered %d", href, rec.Code)
		}
	}
}

// TestReportsView_HasNoFieldACoordinateCouldTravelIn is §4.7 asserted STRUCTURALLY
// rather than by scanning a rendered page.
//
// 🔴 A PAGE SCAN CAN ONLY SEE WHAT THIS TEST'S FIXTURE HAPPENED TO PUT IN IT. The
// property the file comments claim is stronger: there is no field for a coordinate at
// all, so no fixture and no future handler could put one on the page. Reflection is
// where that is visible. It walks the view recursively, so a coordinate smuggled in
// on a nested row type is caught too.
//
// 🔴 THE FIRST VERSION MATCHED NINE NAMES EXACTLY AND AN AUDIT WALKED SIX FIELDS PAST
// IT: Coords, Where, GPS, SourceAddress, SessionToken and InviteCode. The last two are
// §4.7 secrets and both escaped because "token" was an EQUALITY test -- SessionToken is
// not "token". Exact names are a spelling check; what §4.7 needs is a check on the
// idea. Every one of the six is in bannedFieldProbes below and is required to be
// caught.
func TestReportsView_HasNoFieldACoordinateCouldTravelIn(t *testing.T) {
	seen := map[reflect.Type]bool{}
	fields := 0

	var walk func(reflect.Type, string)
	walk = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			fields++
			if why := bannedFieldName(f.Name); why != "" {
				t.Errorf("%s%s.%s is a field a coordinate or a secret could travel in (%s). "+
					"§4.7: the query does not select one, the domain type has no field for one, "+
					"and neither may this.", path, rt.Name(), f.Name, why)
			}
			walk(f.Type, path+rt.Name()+".")
		}
	}
	walk(reflect.TypeOf(pages.ReportsView{}), "")

	// ANTI-VACUITY: a walker that visited nothing would pass forever. 53 fields were
	// counted on the day this was written; the floor is well under it so that deleting
	// a field is not a failure, and well over zero so that reading the wrong type is.
	if fields < 30 {
		t.Fatalf("the walk saw %d field(s); ReportsView and its rows hold more, so it is "+
			"reading the wrong type", fields)
	}
}

// bannedFieldProbes are the names this check MUST refuse, and the names it must NOT.
//
// 🔴 THE SIX THE AUDIT SMUGGLED THROUGH ARE THE FIRST SIX REFUSALS. The allow list
// underneath is the other half and it is not decoration: a matcher that refuses
// everything is a matcher nobody can add a field past, which is how a net gets deleted
// rather than fixed.
var bannedFieldProbes = struct{ refuse, allow []string }{
	refuse: []string{
		// the first audit's six
		"Coords", "Where", "GPS", "SourceAddress", "SessionToken", "InviteCode",
		// 🔴 THE SECOND AUDIT'S SEVEN, of which RemoteAddr is the one that mattered:
		// it is net/http.Request's OWN name for the address §4.7 forbids, and it walked
		// past a list carrying "address" because the fragment is "addr".
		"RemoteAddr", "ClientAddr", "PeerAddr", "Nonce", "Salt", "Bearer",
		// and the shapes they all generalise to
		"Lat", "Lng", "Latitude", "Longitude", "GpsLat", "SourceIP", "IPAddress",
		"PolicyContext", "AesKey", "TokenHash", "Cmac", "PasswordHash", "GeoPoint",
		"Geoloc", "CoordinateList", "ClientSecret", "Credential",
	},
	allow: []string{
		// 🔴 MEASURED, NOT GUESSED, TWICE. Every field name in the ReportsView tree is
		// walked against this matcher on every run: 53 fields, 0 refused. The only
		// collision the first measurement found was Late against "lat", which is why
		// "lat" is word-matched; the second audit found four more classes and they are
		// the next block down.
		"Late", "LateShifts", "Totals", "Incomplete", "PracticeTaps", "NeedsAction",
		"Unattributable", "StartedEarlier", "DayHeadings", "Capped", "Worked",
		// 🔴 THE FOUR CLASSES THE MATCHER USED TO REFUSE. Every one is a name this
		// product can legitimately produce -- `resource` is what internal/policy scopes
		// a rule to, and it is M6-09's own vocabulary -- so each of these is a net that
		// would have been deleted rather than fixed.
		"Resource", "ResourceName", "Resources", "Nowhere", "Elsewhere", "Anywhere",
		"Geometry",
		// ⚠️ AND ONE THAT IS ACCEPTED ON PURPOSE RATHER THAN BY OVERSIGHT: see
		// bannedFieldName's note on why `position` is not banned.
		"Position", "CursorPosition",
		// the shapes short tokens matched as substrings would have refused
		"Description", "Recipient", "Multiple", "Equipment", "Zip", "Encoded", "Monkey",
	},
}

// bannedFieldName reports why a field name is refused, or "" for an acceptable one.
//
// TWO MATCHING MODES, AND WHICH TOKEN GOES IN WHICH IS A MEASUREMENT RATHER THAN A
// STYLE. A token that is distinctive enough to be a substring of nothing innocent
// ("token", "coord", "addr") matches anywhere in a name. One that is a fragment of
// ordinary English is matched as a whole CamelCase WORD instead -- which still catches
// the real shapes, because "SourceIP" and "GpsLat" put the boundary exactly there.
//
// 🔴 THE WORD-MODE LIST GREW BY THREE AFTER AN AUDIT COMPILED THE MATCHER ON ITS OWN
// AND FED IT NAMES THIS TASK HAD NOT THOUGHT OF. It refused three legitimate classes:
//
//	Resource, ResourceName, Resources   re-SOURCE. `resource` is a REAL concept in this
//	                                    product -- internal/policy scopes a rule to one
//	                                    (M6-09's "kapsam bağlama") -- so this name will
//	                                    reach a view, and refusing it is how a net gets
//	                                    deleted instead of fixed.
//	Nowhere, Elsewhere, Anywhere        no-WHERE.
//	Geometry                            GEO-metry.
//
// All three tokens moved to word mode. What that costs is Geoloc, which word mode
// cannot see (it is one word, not "geo"+"loc"), so `geoloc` is carried as its own
// substring. That is whack-a-mole and it is written down as such: the honest general
// form needs a type checker rather than a name list, and this net is a name list.
//
// 🔴 AND ONE TOKEN WAS MISSING OUTRIGHT: `addr`. The same audit walked RemoteAddr,
// ClientAddr and PeerAddr straight past a list that carried "address" -- and
// RemoteAddr is Go's OWN field name for the thing §4.7 forbids
// (net/http.Request.RemoteAddr, which is where internal/handler's clientIP reads the
// source address from). `addr` replaces `address` rather than joining it, since it
// subsumes it.
//
// ⚠️ `position` IS DELIBERATELY NOT BANNED, AND THE REASON IS MEASURED. It is a
// plausible name for a coordinate, but in THIS repository the word already means the
// KEYSET CURSOR position (internal/domain/ledger's Cursor, internal/handler's
// transactions.go and review.go; 82 occurrences under internal/ and web/), so banning
// it would refuse a field this codebase is actively likely to produce. What covers the
// real risk instead is the first wall rather than this one: a coordinate field has
// nothing to be filled FROM, because the query does not select gps_lat or gps_lng.
// Counted, not closed.
func bannedFieldName(name string) string {
	low := strings.ToLower(name)
	for _, tok := range []string{
		"latitude", "longitude", "lng", "gps", "coord", "geoloc", "addr",
		"token", "secret", "credential", "context", "cmac", "password",
		"invite", "hash", "nonce", "bearer",
	} {
		if strings.Contains(low, tok) {
			return "the name contains " + tok
		}
	}
	for _, word := range camelWords(name) {
		switch word {
		case "lat", "ip", "key", "code", "source", "where", "geo", "salt":
			return "the name carries the word " + word
		}
	}
	return ""
}

// camelWords splits a Go field name into lower-cased words, keeping acronyms whole:
// SourceIP -> source, ip; GPSLat -> gps, lat; IPMatch -> ip, match.
func camelWords(name string) []string {
	runes := []rune(name)
	var out []string
	start := 0
	for i := 1; i < len(runes); i++ {
		lowerToUpper := unicode.IsLower(runes[i-1]) && unicode.IsUpper(runes[i])
		acronymEnd := i+1 < len(runes) && unicode.IsUpper(runes[i-1]) &&
			unicode.IsUpper(runes[i]) && unicode.IsLower(runes[i+1])
		if lowerToUpper || acronymEnd {
			out = append(out, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}
	return append(out, strings.ToLower(string(runes[start:])))
}

// TestBannedFieldName_RefusesTheThingsItExistsToRefuse is the matcher's own control,
// in both directions. Without the second half this would be satisfied by a matcher
// that refuses every name, which is a net nobody can work past.
func TestBannedFieldName_RefusesTheThingsItExistsToRefuse(t *testing.T) {
	for _, name := range bannedFieldProbes.refuse {
		if bannedFieldName(name) == "" {
			t.Errorf("the §4.7 matcher ACCEPTS the field name %q, which is a field a "+
				"coordinate or a secret could travel in", name)
		}
	}
	for _, name := range bannedFieldProbes.allow {
		if why := bannedFieldName(name); why != "" {
			t.Errorf("the §4.7 matcher REFUSES the ordinary field name %q (%s); a matcher "+
				"that refuses everything is one somebody deletes rather than fixes", name, why)
		}
	}
	// The splitter itself, since both modes above stand on it.
	for _, c := range []struct {
		in   string
		want []string
	}{
		{in: "SourceIP", want: []string{"source", "ip"}},
		{in: "GPSLat", want: []string{"gps", "lat"}},
		{in: "IPMatch", want: []string{"ip", "match"}},
		{in: "Late", want: []string{"late"}},
	} {
		got := camelWords(c.in)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("camelWords(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestHoursMinutes_TruncatesRatherThanRounds pins the formatting decision, which is
// the one place a payroll figure could stop agreeing with itself.
func TestHoursMinutes_TruncatesRatherThanRounds(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{in: 0, want: "0h 00m"},
		{in: 59 * time.Second, want: "0h 00m"},
		{in: time.Minute, want: "0h 01m"},
		{in: 9*time.Hour + 5*time.Minute, want: "9h 05m"},
		// 🔴 TRUNCATION, NOT ROUNDING. 7h 29m 59s reads as 7h 29m. Rounding up would
		// let seven daily columns exceed the week they add up to by up to seven
		// minutes, and a row whose parts do not sum to its own total is one nobody
		// trusts.
		{in: 7*time.Hour + 29*time.Minute + 59*time.Second, want: "7h 29m"},
		{in: 100 * time.Hour, want: "100h 00m"},
	}
	for _, c := range cases {
		if got := hoursMinutes(c.in); got != c.want {
			t.Errorf("hoursMinutes(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	// A day with no shift is a DASH rather than a zero, which is a different claim:
	// zero is a measured figure and an absent shift is the absence of one.
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		daily := make([]time.Duration, ledger.ReportDays)
		daily[0] = 8 * time.Hour
		r.People = []ledger.PersonHours{{Name: "Maria Borg", Worked: 8 * time.Hour, Shifts: 1, Daily: daily}}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()
	if !strings.Contains(body, "—") {
		t.Error("a day with no shift prints as a figure rather than as an absence")
	}
}

// TestReportsSection_MarksManualEntriesAndUnmeasuredArrivals covers the two M6-07 card
// criteria whose only visible form is a chip on a row.
//
// 🔴 THEY ARE ASSERTED ON THE RENDERED PAGE RATHER THAN ON THE DOMAIN VALUE, because
// the domain half is already proved in internal/domain/ledger and what those two
// criteria actually ask for is that a MANAGER SEES the distinction. A field computed
// correctly and never rendered would satisfy the domain test and fail the card.
func TestReportsSection_MarksManualEntriesAndUnmeasuredArrivals(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		daily := make([]time.Duration, ledger.ReportDays)
		daily[0] = 6 * time.Hour
		r.Totals = ledger.Totals{Worked: 6 * time.Hour, Shifts: 1}
		r.People = []ledger.PersonHours{{
			Name: "Nadia Cassar", Worked: 6 * time.Hour, Shifts: 1, Daily: daily,
			ManualArrivals: 1, Unmeasured: 2, LateShifts: 1, LateBy: 17 * time.Minute,
		}}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()

	for _, want := range []string{
		// §5 + handoff §6: a manager-entered row carries no evidence of a physical
		// touch, so a report that blended it in would present a typed figure as a
		// measured one.
		"1 arrival entered by a manager",
		// M4-05's third answer. "Not measured" is not "on time": a venue with no
		// opening hours has no yardstick, and printing 0 minutes late would invent one.
		"2 arrivals not measured — no shift set",
		"1 late arrival · 0h 17m",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page never says %q", want)
		}
	}

	// POSITIVE CONTROL: a person with none of the three shows none of the chips, so
	// the assertions above are about this fixture rather than about the template
	// always printing them.
	clean := newFakeLedger()
	clean.hours = reportWith(func(r *ledger.Report) {
		daily := make([]time.Duration, ledger.ReportDays)
		daily[0] = 6 * time.Hour
		r.People = []ledger.PersonHours{{Name: "Maria Borg", Worked: 6 * time.Hour, Shifts: 1, Daily: daily}}
	})
	plain := reportBrowser(t, clean).do(http.MethodGet, reportsHref, nil).Body.String()
	for _, forbidden := range []string{"entered by a manager", "not measured", "late arrival"} {
		if strings.Contains(plain, forbidden) {
			t.Errorf("a row with nothing to qualify still prints %q", forbidden)
		}
	}
}

// TestReportsSection_SaysHowMuchOfTheBacklogNeedsAction is the READER for
// Totals.NeedsAction, and it is here because an audit found the field computed and
// rendered nowhere.
//
// 🔴 AN UNREAD FIELD IS NOT A HARMLESS ONE HERE. The handler's open-entry loop refuses
// to stop at maxOpenRows and justifies the cost with this very count -- "or the count
// beside the list would be a count of the page" -- so without a reader the comment was
// defending work nothing used. Removing the line from the template left the whole
// package green (measured); this test is what makes that removal loud.
func TestReportsSection_SaysHowMuchOfTheBacklogNeedsAction(t *testing.T) {
	week := weekOfUTC()
	// Two entries, of which ONE is stale, and both inside the listing cap -- so the
	// difference between "a count of the page" and "a count of the backlog" is not what
	// this case measures. The case below it is.
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals.Open = 2
		r.Open = []ledger.OpenEntry{
			{EmployeeName: "Maria Borg", At: week.From.Add(2 * time.Hour), Stale: true},
			{EmployeeName: "Antoine Vella", At: week.From.Add(100 * time.Hour)},
		}
	})
	body := reportBrowser(t, records).do(http.MethodGet, reportsHref, nil).Body.String()
	if !strings.Contains(body, "1 of these has been open too long to be somebody still on shift") {
		t.Error("the open list never says how many entries need a manager")
	}

	// 🔴 THE COUNT COVERS THE BACKLOG AND NOT THE PAGE. Every entry past the listing cap
	// is stale, so a tally taken inside the shortened loop would report maxOpenRows and
	// this assertion would fail — which is exactly the defect the loop's comment claims
	// to prevent.
	big := newFakeLedger()
	stale := maxOpenRows + 43
	big.hours = reportWith(func(r *ledger.Report) {
		r.Totals.Open = stale
		for i := 0; i < stale; i++ {
			r.Open = append(r.Open, ledger.OpenEntry{
				EmployeeName: "Open Worker " + pad2(i),
				At:           week.From.Add(time.Duration(i) * time.Minute),
				Stale:        true,
			})
		}
	})
	wide := reportBrowser(t, big).do(http.MethodGet, reportsHref, nil).Body.String()
	if !strings.Contains(wide, "143 of these have been open too long") {
		t.Errorf("the needs-action count is a count of the page rather than of the backlog")
	}

	// POSITIVE CONTROL: with nothing stale the sentence changes rather than disappears —
	// "none of these" is a measured claim and silence is not.
	fresh := newFakeLedger()
	fresh.hours = reportWith(func(r *ledger.Report) {
		r.Totals.Open = 1
		r.Open = []ledger.OpenEntry{{EmployeeName: "Maria Borg", At: week.From}}
	})
	calm := reportBrowser(t, fresh).do(http.MethodGet, reportsHref, nil).Body.String()
	if !strings.Contains(calm, "None of these has been open long enough") {
		t.Error("with nothing stale the page says nothing at all, which reads as unmeasured")
	}
}
