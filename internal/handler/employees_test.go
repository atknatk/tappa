package handler

// employees_test.go -- M6-05 phase A's unit-level nets.
//
// WHAT IS HERE AND WHAT IS IN employees_db_test.go. §4.5, §4.6 and §4.7 are
// measured against real Postgres, because a fake reader can only ever confirm what
// the test told it. What is here is what a fake CAN prove: the boundary between
// what the URL asks for and what the domain is handed, the sentence a device count
// turns into, and the property that this screen offers NO WRITE.

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/web/templates/pages"
)

// panelBrowserWithRoster is panelBrowser wired to a reader the caller controls.
func panelBrowserWithRoster(t *testing.T, records *fakeLedger) *browser {
	t.Helper()
	return panelBrowserWithReviewer(t, records, &fakeReviewer{})
}

// 🔴 THE PHASE BOUNDARY TEST THAT USED TO LIVE HERE IS GONE, AND ITS PROPERTY IS
// NOT. TestEmployeesSection_OffersNoWriteAtAll asserted that the four actions the
// card names — invite, re-invite, deactivate, move — were ABSENT, because phase A
// shipped none of them and a control the server does not implement is this
// repository's most expensive class of mistake. Phase B shipped all four, so the
// assertion is retired rather than weakened: what replaces it is
// TestEmployeesSection_EveryControlLeadsSomewhereThatExists
// (employeeactions_test.go), which reads every posting form and every link OFF THE
// RENDERED PAGE and drives each one through the SAME router the page came from. The
// old test could only say "none of these exist"; the new one says "every control
// this page offers is served", which is the property that was wanted all along.
//
// ⚠️ THE FIXED VERB LIST (phaseBVerb) WENT WITH IT, and that is a deliberate
// deletion rather than an oversight. It matched `invite|deactivate|move|…` as a
// cheap extra check, and every one of those words is now a legitimate label on this
// screen — a pattern that flags the correct page is a pattern the next person
// deletes. Its own negative control lives on below, narrowed to the two scanners
// that are still load-bearing.

// sameOriginHrefs reads every in-product link off a rendered page.
//
// It skips fragments and anything absolute: an absolute URL is a separate defect
// that TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty already refuses,
// and driving one through this router would prove nothing.
func sameOriginHrefs(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range hrefRE.FindAllStringSubmatch(body, -1) {
		href := htmlUnescaper.Replace(m[1])
		if href == "" || strings.HasPrefix(href, "#") ||
			strings.HasPrefix(href, "//") || strings.Contains(href, "://") {
			continue
		}
		if !strings.HasPrefix(href, "/") || seen[href] {
			continue
		}
		seen[href] = true
		out = append(out, href)
	}
	return out
}

var (
	hrefRE           = regexp.MustCompile(`(?is)<a\b[^>]*\bhref="([^"]*)"`)
	singleQuotedAttr = regexp.MustCompile(`(?is)<[a-z]+\b[^>]*\s[a-z-]+='[^']*'`)
)

// TestEmployeesScanners_RejectTheThingsTheyExistToReject is the negative control for
// the two scanners the phase-B test depends on. Without it, a blinded regexp would
// make that test pass over a page full of forms it never saw.
func TestEmployeesScanners_RejectTheThingsTheyExistToReject(t *testing.T) {
	const hostile = `<form method="post" action="/admin/employees/deactivate">` +
		`<button type="submit">Deactivate</button></form>` +
		`<a href="/admin/employees/invite">Invite somebody</a>` +
		`<button type="button">Re-invite</button>`

	if n := len(postFormRE.FindAllString(hostile, -1)); n != 1 {
		t.Errorf("the posting-form scanner found %d form(s) in a page that has one", n)
	}
	if got := postFormRE.FindAllStringSubmatch(hostile, -1); len(got) == 1 {
		if c := attrValueRE("action").FindStringSubmatch(got[0][1]); len(c) != 2 ||
			c[1] != "/admin/employees/deactivate" {
			t.Errorf("the form scanner read action %q, want /admin/employees/deactivate", c)
		}
	}
	if hrefs := sameOriginHrefs(hostile); len(hrefs) != 1 || hrefs[0] != "/admin/employees/invite" {
		t.Errorf("the link scanner read %q from a page with one same-origin link", hrefs)
	}
	// AND IT MUST NOT INVENT LINKS OUT OF ABSOLUTE OR FRAGMENT HREFS, or the "every
	// control resolves" test would drive requests at other people's hosts.
	if hrefs := sameOriginHrefs(`<a href="https://evil.example/x">x</a><a href="#top">t</a>`); len(hrefs) != 0 {
		t.Errorf("the link scanner returned %q for a page with no in-product links", hrefs)
	}
	// THE FORM SCAN MATCHES DOUBLE QUOTES ONLY, which is safe because templ
	// normalises single-quoted attributes -- measured in phase A, where writing
	// `<form method='post'>` into the template still rendered as method="post". This
	// asserts the scanner's blind spot is real so the tripwire below has a reason.
	if postFormRE.MatchString(`<form method='post' action='/admin/x'>`) {
		t.Error("the posting-form scanner matched a single-quoted form; the tripwire " +
			"in the phase-B test would then be redundant rather than a tripwire")
	}
	if !singleQuotedAttr.MatchString(`<form method='post'>`) {
		t.Error("the single-quote tripwire does not fire on a single-quoted attribute")
	}
	if singleQuotedAttr.MatchString(`<form method="post">`) {
		t.Error("the single-quote tripwire fires on ordinary markup")
	}
	// THE LABEL SCANNER READS CONTROLS AND NOT CHIPS. "DEACTIVATED" is a <span>
	// inside a card, and a scanner that read spans would make the anti-vacuity check
	// that uses this pass over a page with no controls on it.
	labels := pressTargetLabels(hostile)
	if len(labels) != 3 {
		t.Errorf("the press-target scanner read %d label(s) from a page with three: %q",
			len(labels), labels)
	}
	if got := pressTargetLabels(`<span class="tally tally--deactivated">DEACTIVATED</span>`); len(got) != 0 {
		t.Errorf("the press-target scanner read %q from a page with no press target", got)
	}
}

// postFormRE matches a form that POSTs. It is deliberately not anchored on having a
// submit button: a form with none is still submittable.
var postFormRE = regexp.MustCompile(`(?is)<form\b([^>]*method="post"[^>]*)>`)

// pressTargetLabelRE reads the visible text of an anchor or a button, and
// pressTargetLabels turns a page into that list. It survived the deletion of the
// phase-A verb list because a second test uses it as an ANTI-VACUITY check: "is the
// control I am about to measure actually on this page?".
var pressTargetLabelRE = regexp.MustCompile(`(?is)<(a|button)\b[^>]*>(.*?)</(?:a|button)>`)

// tagStripRE removes any nested markup from a press target's text.
var tagStripRE = regexp.MustCompile(`(?s)<[^>]*>`)

func pressTargetLabels(body string) []string {
	var out []string
	for _, m := range pressTargetLabelRE.FindAllStringSubmatch(body, -1) {
		label := strings.Join(strings.Fields(tagStripRE.ReplaceAllString(m[2], " ")), " ")
		if label != "" {
			out = append(out, label)
		}
	}
	return out
}

// TestEmployeesSection_EchoesTheFiltersThatTookEffect. A filter bar that shows a
// value the query never received tells the manager they are reading a narrowed list
// when they are reading the whole one.
func TestEmployeesSection_EchoesTheFiltersThatTookEffect(t *testing.T) {
	records := newFakeLedger()
	loc, dep := uuid.New(), uuid.New()
	// THE DROPDOWNS HAVE TO CONTAIN THE IDS FOR THE ECHO TO BE POSSIBLE, and that is
	// the control's own rule rather than a fixture convenience: a <select> can only
	// mark an option selected if the option exists, so an id that belongs to no venue
	// of this tenant is silently not echoed. That is the right behaviour — it is also
	// why the cross-tenant half of this is measured against real Postgres, where a
	// foreign id genuinely is absent from the list.
	records.roster.Options = ledger.Options{
		Locations:   []ledger.Option{{ID: loc, Label: "St Julians"}},
		Departments: []ledger.Option{{ID: dep, Label: "Kitchen", Group: "St Julians"}},
	}
	b := panelBrowserWithRoster(t, records)

	q := url.Values{
		"name":       {"Borg"},
		"status":     {"deactivated"},
		"location":   {loc.String()},
		"department": {dep.String()},
	}
	rec := b.do(http.MethodGet, employeesHref+"?"+q.Encode(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d, want 200", rec.Code)
	}
	body := htmlOf(t, rec)

	f := records.lastRosterFilter(t)
	if f.Name != "Borg" || f.Status != "deactivated" {
		t.Errorf("the reader was asked for name=%q status=%q, want Borg / deactivated", f.Name, f.Status)
	}
	if f.LocationID == nil || *f.LocationID != loc {
		t.Errorf("the location filter did not reach the reader: %v", f.LocationID)
	}
	if f.DepartmentID == nil || *f.DepartmentID != dep {
		t.Errorf("the department filter did not reach the reader: %v", f.DepartmentID)
	}
	for _, want := range []string{`value="Borg"`, `value="deactivated" selected`, loc.String(), dep.String()} {
		if !strings.Contains(body, want) {
			t.Errorf("the filter bar does not echo %q, so a manager cannot see which "+
				"filters actually took effect", want)
		}
	}
}

// TestEmployeesSection_DropsWhatItCannotUnderstand. Anything unrecognised WIDENS
// rather than errors — the same rule the transactions filter follows, and the same
// reason: dropping can only ever show more of the tenant's own data, and an error
// page for a stale bookmark is a worse answer than the unfiltered list.
func TestEmployeesSection_DropsWhatItCannotUnderstand(t *testing.T) {
	records := newFakeLedger()
	b := panelBrowserWithRoster(t, records)

	q := url.Values{
		"status":     {"fired"},      // not in the vocabulary
		"location":   {"not-a-uuid"}, // unparseable
		"department": {""},           // empty
		"after_name": {"Borg"},       // half a cursor
	}
	if rec := b.do(http.MethodGet, employeesHref+"?"+q.Encode(), nil); rec.Code != http.StatusOK {
		t.Fatalf("GET: %d, want 200 -- an unreadable filter is dropped, not refused", rec.Code)
	}
	f := records.lastRosterFilter(t)
	switch {
	case f.Status != "":
		t.Errorf("a status outside the vocabulary reached the reader as %q", f.Status)
	case f.LocationID != nil:
		t.Errorf("an unparseable location id reached the reader as %v", f.LocationID)
	case f.DepartmentID != nil:
		t.Errorf("an empty department id reached the reader as %v", f.DepartmentID)
	case f.AfterID != nil:
		t.Errorf("an unparseable cursor reached the reader (%v); it must be dropped",
			f.AfterID)
	}
}

// TestEmployeesSection_TheCursorIsAnIdAndNothingElse.
//
// 🔴 IT REPLACES A TEST THAT PINNED A BOUND THAT SHOULD NOT HAVE EXISTED. The old
// one asserted that a cursor needs BOTH a name and an id, and that a name past
// maxRosterCursorName is dropped — it passed, it was thorough, and it was pinning
// the §4.6 defect in place: a dropped cursor repeats a page, so "we drop long names"
// was the bug written as an expectation. The bound is gone; the anchor is an id and
// the server reads the name (see ledger.RosterFilter.AfterID).
//
// A LEFTOVER after_name IS IGNORED RATHER THAN REFUSED, which matters for the links
// already in somebody's browser history: they carry both parameters, and the id half
// is still exactly right.
func TestEmployeesSection_TheCursorIsAnIdAndNothingElse(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name  string
		query url.Values
		want  *uuid.UUID
	}{
		{"an id alone is a cursor", url.Values{"after_id": {id.String()}}, &id},
		{"a name alone is not", url.Values{"after_name": {"Borg"}}, nil},
		{"an unparseable id is dropped", url.Values{"after_id": {"nope"}}, nil},
		{"no cursor at all", url.Values{}, nil},
		{
			// AN OLD-STYLE LINK from before the cursor changed shape.
			"a stale link carrying both still pages",
			url.Values{"after_id": {id.String()}, "after_name": {strings.Repeat("z", 4000)}},
			&id,
		},
		{
			// AND THE SHAPE THAT USED TO BREAK EVERYTHING: an absurd name beside a
			// good id. There is no length to exceed any more.
			"an absurd name cannot cost a good id its cursor",
			url.Values{"after_id": {id.String()}, "after_name": {strings.Repeat("x", 100000)}},
			&id,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			records := newFakeLedger()
			b := panelBrowserWithRoster(t, records)
			if rec := b.do(http.MethodGet, employeesHref+"?"+tc.query.Encode(), nil); rec.Code != http.StatusOK {
				t.Fatalf("GET: %d, want 200", rec.Code)
			}
			got := records.lastRosterFilter(t).AfterID
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("cursor %v reached the reader; it should have been dropped", got)
			case tc.want != nil && got == nil:
				t.Errorf("the cursor was dropped; want %v", tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Errorf("cursor = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEmployeesSection_NoEmployeeNameTravelsInAPagingLink is the privacy half of the
// same change, and it is asserted separately because it is a separate promise.
//
// The Referrer-Policy note in adminlogin.go recorded that a panel URL had begun to
// carry a value the SERVER read out of the database — a real person's full name, in
// browser history and on any shared screen. Moving the cursor to an id closed that as
// a side effect; this is what stops it reopening.
func TestEmployeesSection_NoEmployeeNameTravelsInAPagingLink(t *testing.T) {
	records := newFakeLedger()
	const who = "Ġużeppi Żammit-Borġ"
	next := uuid.New()
	records.roster = ledger.RosterScreen{
		RosterPage: ledger.RosterPage{
			Queried: true, Zone: time.UTC,
			People: []ledger.Person{{ID: next, Name: who, Status: ledger.StatusActive}},
			NextID: &next,
		},
	}
	b := panelBrowserWithRoster(t, records)
	rec := b.do(http.MethodGet, employeesHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d, want 200", rec.Code)
	}
	body := htmlOf(t, rec)

	hrefs := sameOriginHrefs(body)
	var paging string
	for _, h := range hrefs {
		if strings.Contains(h, "after_id=") {
			paging = h
		}
	}
	if paging == "" {
		t.Fatalf("no paging link on the page, so this test measures nothing. hrefs: %q", hrefs)
	}
	// POSITIVE CONTROL: the name really is on the page, just not in the link.
	if !strings.Contains(body, htmlText(who)) {
		t.Fatalf("the roster does not render %q at all", who)
	}
	for _, form := range []string{who, url.QueryEscape(who), "after_name"} {
		if strings.Contains(paging, form) {
			t.Errorf("the paging link %q carries %q. A URL is browser history and a "+
				"shared screen; the anchor is an id precisely so a person's name does "+
				"not travel in one.", paging, form)
		}
	}
}

// TestDeviceLine_SaysTheFourThingsThatCanBeTrue.
//
// 🔴 THE MIDDLE TWO ARE THE POINT. "No device" and "a device that has never been
// used" both have a nil timestamp, so a template joining a count field to a date
// field renders them identically — and "never used" is the ORDINARY state on the day
// somebody activates, which is the day a manager is most likely to be reading this
// screen.
func TestDeviceLine_SaysTheFourThingsThatCanBeTrue(t *testing.T) {
	zone := time.FixedZone("CET", 3600)
	used := time.Date(2026, 8, 5, 12, 3, 0, 0, time.UTC)

	tests := []struct {
		name     string
		n        int
		lastUsed *time.Time
		want     string
	}{
		{"nobody signed in", 0, nil, "None signed in"},
		{"nobody signed in, and a stale timestamp cannot resurrect one", 0, &used, "None signed in"},
		{"one device, never used", 1, nil, "1 signed in · not used yet"},
		{"one device, used", 1, &used, "1 signed in · last used 5 Aug 13:03"},
		{"several devices", 3, &used, "3 signed in · last used 5 Aug 13:03"},
		{"a negative count is still nobody", -1, nil, "None signed in"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := deviceLine(tc.n, tc.lastUsed, zone); got != tc.want {
				t.Errorf("deviceLine(%d, %v) = %q, want %q", tc.n, tc.lastUsed, got, tc.want)
			}
		})
	}
}

// TestPanelStatuses_AreTheDomainVocabularyAndNothingElse. The dropdown and the
// validator must read one list: two lists is the shape where a filter is offered
// that the validator silently drops, and the manager reads an unfiltered page as a
// filtered one.
func TestPanelStatuses_AreTheDomainVocabularyAndNothingElse(t *testing.T) {
	if len(panelStatuses) != len(ledger.EmployeeStatuses) {
		t.Fatalf("the dropdown offers %d status(es), the domain names %d",
			len(panelStatuses), len(ledger.EmployeeStatuses))
	}
	for i, s := range ledger.EmployeeStatuses {
		if panelStatuses[i].Value != s {
			t.Errorf("option %d is %q, the domain's is %q", i, panelStatuses[i].Value, s)
		}
		if panelStatuses[i].Label == "" {
			t.Errorf("option %d (%q) has no label", i, s)
		}
		// EVERY OFFERED VALUE IS ACCEPTED BY THE VALIDATOR, which is the half that
		// makes "one list" mean something.
		if got := oneOf(s, panelStatuses); got != s {
			t.Errorf("the validator drops %q, which the dropdown offers", s)
		}
	}
	// AND "deactivated" IS ONE OF THEM (§4.6). A vocabulary that lost it would make
	// the people who have left unfilterable, which is the second half of the rule
	// TestPanelEmployeesDB_ListsPeopleWhoHaveLEFT holds.
	if got := oneOf(ledger.StatusDeactivated, panelStatuses); got == "" {
		t.Error("the status filter does not offer 'deactivated'; somebody who has left " +
			"must be findable, not merely un-hidden")
	}
}

// TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget is the NET UNDER THE
// PAGE-SIZE DECISION (user decision, 2026-08-07).
//
// 🔴 IT EXISTS BECAUSE THE CONSTANT SHIPPED AT 25 WITH NOTHING HOLDING IT THERE, and
// the shape of that failure is the one this repository keeps paying for: a number
// chosen on a measurement, written into a comment, and then free to drift back
// because no test could tell. Changing ledger.RosterPageSize from 50 to 25 broke NO
// test at all when it was measured — the paging test seeds 63 people and passes at
// both sizes.
//
// WHAT IS ASSERTED IS THE PROPERTY THE DECISION WAS TAKEN ON, not the number:
// walking a roster costs ceil(E / RosterPageSize) charged requests, adminSessionLimit
// bounds what a session may spend, so RosterPageSize x adminSessionLimit is the
// largest roster one session can walk end to end. It must reach rosterDesignCeiling.
//
// ⚠️ IT PINS ONE DIRECTION ONLY. Any page size ABOVE 34 satisfies it, so this says
// nothing about 100 or 500 — bytes are what bound those, and
// TestPanelEmployeesDB_APageStaysFarUnderTheControlM603Removed is that half. Neither
// distinguishes 50 from 100; what does is the arithmetic in employees.go and a human
// reading it.
func TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget(t *testing.T) {
	walkable := ledger.RosterPageSize * adminSessionLimit
	if walkable < rosterDesignCeiling {
		t.Errorf("one session can walk %d people (RosterPageSize %d x adminSessionLimit %d), "+
			"and this panel promises %d.\n"+
			"A manager of a business between those two figures is refused partway down "+
			"their own roster, with no way to reach the rest by browsing: at %d per page "+
			"the promise runs out after %d people.\n"+
			"Either raise RosterPageSize or lower the promise on purpose; do not do "+
			"either by accident. The measured figures for the development database, "+
			"with the commands that produce them, are in adminratelimit.go -- they are "+
			"NOT repeated here, because that roster grows on every `make test` run and "+
			"a number quoted in a failure message would be stale before it fired.",
			walkable, ledger.RosterPageSize, adminSessionLimit, rosterDesignCeiling,
			ledger.RosterPageSize, walkable)
	}
	// ANTI-VACUITY. A ceiling of zero, or a page size of zero, would make the
	// comparison above pass while meaning nothing -- and a zero page size is not
	// hypothetical, it is what a botched refactor of a constant leaves behind.
	if ledger.RosterPageSize <= 0 || rosterDesignCeiling <= 0 || adminSessionLimit <= 0 {
		t.Fatalf("one of the three quantities is non-positive (page %d, ceiling %d, "+
			"budget %d); the inequality above is meaningless",
			ledger.RosterPageSize, rosterDesignCeiling, adminSessionLimit)
	}
	// AND THE TWO PAGE SIZES ARE SEPARATE CONSTANTS (user decision, same day). Tying
	// the roster's to the transactions' would make a change to one silently move the
	// other's budget arithmetic, and the two were sized against different loads: a
	// day of records is bounded by traffic, a roster by the payroll.
	if ledger.RosterPageSize == ledger.PageSize {
		t.Errorf("RosterPageSize and PageSize are both %d. They were separated on "+
			"purpose (user decision, 2026-08-07): a day of records is bounded by "+
			"TRAFFIC and a roster by the PAYROLL, so at %d per page the roster "+
			"promise falls to %d people against a design ceiling of %d.",
			ledger.PageSize, ledger.PageSize, ledger.PageSize*adminSessionLimit,
			rosterDesignCeiling)
	}
}

// rosterSizeQuote matches the SHAPES a drifting roster count is actually written in.
//
// 🔴 THIS EXISTS BECAUSE ONE DEFECT CLASS PRODUCED A BLOCKING FINDING IN THREE
// CONSECUTIVE AUDIT ROUNDS, and each round I fixed the instances rather than the
// class. The quantity is "how many people the development database's largest tenant
// has", and it is not a fact — it is a clock: the simulated-day fixture hires into
// that tenant on every `make test` and migration 00003 REVOKEs DELETE on employees,
// so it only goes up. Observed while this task was being reviewed: 8 718, 8 818,
// 8 878, 8 918, 8 958, 8 978 — the last two DURING a single audit.
//
// Every time it was quoted in prose, the prose was stale before the round ended, and
// at one point four files disagreed with each other and with the card. So the rule is
// not "keep the number fresh", which is impossible; it is DO NOT QUOTE IT. The
// argument it was being used for does not need it — see
// TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget, which is an inequality
// between constants.
//
// ⚠️ IT MATCHES SHAPES, NOT MEANING, AND SIX WAYS PAST IT ARE MEASURED. The
// vocabulary was widened after an audit walked straight through the first version
// with `rows`, `tenants` and `istek`; widening it to every noun in sight was tried
// and REVERTED, because it then flagged thirty legitimate measurements of other
// magnitudes (records in a day, requests per window, tenant-days) and a tripwire
// that fires on correct prose is one the next person deletes. What it names now are
// the nouns for the magnitude that actually drifts: a headcount.
//
// THE ESCAPES, each verified rather than imagined:
//
//	"~645 çalışana kadar"        Turkish case suffix. Go's \b is ASCII, so a suffix
//	                             beginning with an ASCII vowel breaks the boundary
//	                             while "çalışanın" (dotless ı) does not. THREE of
//	                             M6-03's card lines hide here.
//	"9 138 rows" / "records"     the same magnitude wearing a noun that also names
//	                             other things. Dropped deliberately; see above.
//	"nine thousand employees"    spelled out
//	"employees: 9 138"           noun BEFORE the count
//	"the largest tenant: 9138"   no noun adjacent at all
//	"9 138" / "employees"        split across two comment lines
//
// It is a tripwire on the cheapest route, not a proof — and the route it covers is
// the one this repository actually took, four rounds running.
var rosterSizeQuote = regexp.MustCompile(
	`(?i)(` +
		// A count followed by the thing it counts. The noun list was three words and
		// an audit walked past it with `rows`, `tenants` and `istek` on the first
		// try, so it is now every noun this repository has actually used for one of
		// these magnitudes, in both languages.
		`\d[ ,.]?\d{3}\s*(employees|people|persons|staff|workers|headcount|payroll|` +
		`rosters?|çalışan|kişi|kadro|bordro)\b` +
		`|` +
		// "the measured N" / "measured at N", the shape a justification takes.
		`measured\s+(at\s+)?\d[ ,.]?\d{3}[^0-9x×-]` +
		`|` +
		// "roster (N", "kadro (N", "tenant (N" -- a count in parentheses after the
		// noun rather than before it.
		//
		// ⚠️ THE FIRST DIGIT MAY NOT BE ZERO, AND THAT IS A MEASURED FALSE POSITIVE
		// RATHER THAN A LOOSENING. `\d` matched `tenant (00002)` -- a MIGRATION
		// REFERENCE, which is ordinary prose in this repository and which M6-05 phase B
		// produced on its first run. A headcount never begins with a zero, so requiring
		// [1-9] removes the whole class without letting a single real quote through;
		// both directions are controlled in
		// TestRosterSizeScanner_RejectsTheThingsItExistsToReject. A tel that fires on
		// legitimate prose is the tel the next person deletes.
		`(roster|kadro|tenant|payroll)\W{0,3}\(\s*[1-9][ ,.]?\d{3}` +
		`)`)

// TestComments_DoNotQuoteTheDriftingRosterSize.
//
// WHERE IT LOOKS: the Go and SQL this task owns, plus its own card. Generated files
// are skipped — internal/store is sqlc's copy of the SQL, so fixing the source fixes
// both, and asserting over the copy would report every defect twice.
func TestComments_DoNotQuoteTheDriftingRosterSize(t *testing.T) {
	roots := []string{
		filepath.Join("..", ".."),
	}
	var scanned int
	var bad []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", ".tools", "node_modules", "store":
					return fs.SkipDir
				}
				return nil
			}
			switch {
			case strings.HasSuffix(path, "_templ.go"):
				return nil
			case strings.HasSuffix(path, ".go"),
				strings.HasSuffix(path, ".sql"),
				strings.HasSuffix(path, "m6-dashboard.md"):
			default:
				return nil
			}
			// This file NAMES the shapes it forbids, so it would report itself.
			if strings.HasSuffix(path, "employees_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			for i, line := range strings.Split(string(raw), "\n") {
				if m := rosterSizeQuote.FindString(line); m != "" {
					bad = append(bad, fmt.Sprintf("%s:%d  %q\n      %s",
						path, i+1, m, strings.TrimSpace(line)))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	// ANTI-VACUITY: a walk that read nothing would pass forever.
	if scanned < 50 {
		t.Fatalf("scanned %d file(s); the repository has more, so this walk is reading "+
			"the wrong tree and would pass over anything", scanned)
	}
	// 🔴 THE BUDGET IS INHERITED DEBT AND MAY ONLY GO DOWN.
	//
	// M6-05 wrote six of these across four rounds and removed all six. What remains
	// was written by M6-01 and M6-03 -- "7 769 employees", "7 669 employees",
	// "111 167 employees across 64 838 tenants" and one figure in the transactions
	// card -- all quoted when that roster was smaller, all stale now. Verified
	// pre-existing rather than assumed: each is present in `git show HEAD:<file>`,
	// and the card's sits under the "## M6-03 — Transactions sekmesi" heading.
	//
	// 🔴 THE BUDGET FELL FROM 9 TO 6 AND NOTHING WAS FIXED TO MAKE THAT HAPPEN. The
	// vocabulary changed shape in the same edit, and three of M6-03's card lines now
	// escape it through Turkish case suffixes ("çalışana", "çalışanı") -- see the
	// escape list at rosterSizeQuote. The debt did not shrink; the net's view of it
	// did. Saying so is the point: a budget that falls for a reason nobody records is
	// how a counted hole quietly becomes an uncounted one.
	//
	// They are NOT fixed here, and that is a scope decision rather than an oversight:
	// they are another task's prose and correcting them means re-measuring another
	// task's claims. What this budget buys is that the class cannot GROW -- adding a
	// seventh turns this red, removing any of the six is always allowed.
	const inheritedRosterSizeQuotes = 6
	if len(bad) > inheritedRosterSizeQuotes {
		t.Errorf("%d comment(s) quote a roster count that GROWS ON EVERY `make test` RUN, "+
			"and only %d are inherited debt:\n\n%s\n\n"+
			"Do not refresh the number -- it will be wrong again before the round ends "+
			"(it moved twice inside one audit and six times across this task's review). "+
			"Argue from the inequality instead: RosterPageSize x adminSessionLimit vs "+
			"rosterDesignCeiling, which are constants. If a live figure is genuinely "+
			"needed, print the QUERY and let the reader run it; adminratelimit.go does "+
			"exactly that.",
			len(bad), inheritedRosterSizeQuotes, strings.Join(bad, "\n"))
	}
	// AND THE DEBT ITSELF IS VISIBLE ON EVERY RUN, so it is counted rather than
	// forgotten -- a budget nobody ever sees is a budget that only ever rises.
	if len(bad) > 0 {
		t.Logf("%d inherited roster-size quote(s) still in the tree (budget %d):\n%s",
			len(bad), inheritedRosterSizeQuotes, strings.Join(bad, "\n"))
	}
}

// TestRosterSizeScanner_RejectsTheThingsItExistsToReject is the positive control. An
// assertion that nothing matches is satisfied by a blinded pattern.
func TestRosterSizeScanner_RejectsTheThingsItExistsToReject(t *testing.T) {
	mustMatch := []string{
		"//	largest roster (8 918 employees)         357            179",
		"// WHY 10 000 AND NOT THE MEASURED 8 818. The measured figure is the",
		"// largest tenant (8 718 employees) needs 349 requests",
		"> **en büyük tenant'ı (8.718 çalışan)**:",
		// The parenthesised shape must still fire when the number really is a count.
		"// the tenant (9 138 on the day this was written) walks in 183 requests",
		"> | en büyük kadro (8.818) | **353 istek** | **177 istek** |",
		"-- the difference was measured on the largest tenant (8,718 employees)",
		"// a payroll of 9 138 staff",
		"// the tenant has 9 138 workers",
		"// headcount measured at 9 138 ",
	}
	for _, s := range mustMatch {
		if !rosterSizeQuote.MatchString(s) {
			t.Errorf("the scanner ACCEPTED a quoted roster size: %q", s)
		}
	}
	// AND IT MUST NOT REJECT THE NUMBERS THAT ARE ALLOWED TO BE THERE -- byte counts,
	// constants, RFC numbers, luminances, RATIOS and DATES. A tripwire that fires on
	// those gets deleted by the next person, which is worse than not having one. The
	// last two entries were real false positives this scanner produced on its first
	// run and are kept as the controls for the exclusions that fixed them.
	mustNotMatch := []string{
		"//	                        a measured 281957x difference. No timing gate needs",
		"// (user decision, measured 2026-08-06) the note is rendered",
		"//	       50    40 103 B   36 111 B    3 992 B   <- SHIPPED",
		"// it is 7.3x the 867 233 B control M6-03 measured and removed",
		"// RFC 9110 §7.6.3 names intermediaries, not the client",
		"// green-lite (relative luminance 0.8229 against porcelain's 0.8627)",
		"// a finite-yet-absurd year 9999 is still writable",
		"// PendingCap cannot tell a queue of exactly 100 from a queue of 9 000.",
		// The nouns deliberately NOT in the vocabulary -- other magnitudes that also
		// drift but are other tasks' prose, and that a wider net flagged thirty of.
		// Quoted from db/queries/transactions.sql, which M8-04 FAZ B3 dated after an
		// audit noted the figure carried neither a date nor an anchor. It is a
		// RECORD count on a seeded day, not a roster size, so it stays out of this
		// vocabulary -- and the sample is refreshed with the line rather than left
		// quoting text the tree no longer holds.
		"-- tenant, on a day that held 1 628 records AS SEEDED THEN, warm cache, after ANALYZE,",
		"// 3000 istek/pencere/adres",
		"// 40 850 tenant-days measured",
		"// rosterDesignCeiling = 10000, and RosterPageSize x adminSessionLimit = 15 000",
		"// dev over http -> NOT Secure (so http://localhost:8080 works)",
		// A MIGRATION REFERENCE AFTER THE WORD "tenant" -- the false positive that
		// narrowed the parenthesised alternative to [1-9].
		"// A department belongs to a location as well as to a tenant (00002), and it",
		"-- every row of this tenant (00003 makes the column NOT NULL) has one",
	}
	for _, s := range mustNotMatch {
		if m := rosterSizeQuote.FindString(s); m != "" {
			t.Errorf("the scanner REJECTED a legitimate number: %q matched %q", s, m)
		}
	}
}

// TestEmployeesSection_ConditionalControlsCarryATouchTargetClass.
//
// 🔴 THE PANEL-WIDE SCAN CANNOT SEE THESE TWO CONTROLS, AND THAT WAS MEASURED.
// TestPanelScreens_EveryPressTargetCarriesATouchTargetClass builds its corpus from
// sectionBodies, which fetches each section's bare href — so it only ever sees a
// roster with no filters and no second page. "Clear" renders only when something is
// narrowed and "Next page" only when a further page exists, so neither was ever in
// the corpus: an audit deleted `btn btn--quiet` from Clear and that test stayed
// GREEN.
//
// Both controls do carry the right class today, so this closes a hole in the NET
// rather than a defect in the product. It lives here rather than in dashboard_test.go
// because the conditions that reveal them are this section's.
func TestEmployeesSection_ConditionalControlsCarryATouchTargetClass(t *testing.T) {
	records := newFakeLedger()
	// A roster with a next page and a filter applied, so BOTH conditional controls
	// render in one body.
	next := uuid.New()
	records.roster = ledger.RosterScreen{
		RosterPage: ledger.RosterPage{
			Queried: true,
			Zone:    time.UTC,
			People: []ledger.Person{
				{ID: next, Name: "Borg", Status: ledger.StatusActive},
			},
			NextID: &next,
		},
	}
	b := panelBrowserWithRoster(t, records)
	rec := b.do(http.MethodGet, employeesHref+"?name=Borg", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d, want 200", rec.Code)
	}
	body := htmlOf(t, rec)

	// ANTI-VACUITY FIRST: this test is worthless unless both controls are on the page.
	labels := pressTargetLabels(body)
	for _, want := range []string{"Clear", "Next page"} {
		found := false
		for _, l := range labels {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q is not on the page, so this test measures nothing. Labels: %q\n"+
				"It needs a NARROWED filter (for Clear) and a further page (for Next).",
				want, labels)
		}
	}

	targets := pressTargetsOf(body)
	if len(targets) == 0 {
		t.Fatal("no press targets found at all; the scan is reading the wrong thing")
	}
	for _, tgt := range targets {
		if !hasTouchTargetClass(tgt.classes) {
			t.Errorf("<%s class=%q> is a press target carrying none of %v.\n"+
				"A manager checks this at the pass, on a phone, with one thumb.",
				tgt.tag, tgt.classes, panelTouchTargets)
		}
	}
}

// TestEmployeesSection_LoadsNoScript. The panel-wide cardinality test
// (TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty) already pins that
// EXACTLY ONE url sends the widened policy and that it is the transactions section.
// This says the same thing from the employees section's side, so the reason is
// findable from the file it belongs to: paging here is a whole-document link, which
// is what keeps the widening at one.
func TestEmployeesSection_LoadsNoScript(t *testing.T) {
	b := panelBrowser(t)
	rec := b.do(http.MethodGet, employeesHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", employeesHref, rec.Code)
	}
	if body := htmlOf(t, rec); strings.Contains(strings.ToLower(body), "<script") {
		t.Errorf("the employees section loads a script. Paging here is an ordinary "+
			"link, and the panel's widened CSP is pinned to one url -- adding a second "+
			"is allowed but must be argued for in %s.",
			"TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "script-src") {
		t.Errorf("the employees section sends a policy naming script-src (%q) while "+
			"loading nothing; a page that permits what it does not load widens the "+
			"policy for nothing", csp)
	}
}

// TestEmployeesSection_SaysTheReadFailedRatherThanShowingAnEmptyRoster is §4.6.
// "Nobody works here" is a claim about a business, and a database that did not answer
// is not evidence for it.
func TestEmployeesSection_SaysTheReadFailedRatherThanShowingAnEmptyRoster(t *testing.T) {
	records := newFakeLedger()
	records.rosterErr = errRosterUnavailable
	b := panelBrowserWithRoster(t, records)

	rec := b.do(http.MethodGet, employeesHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed read answered %d; want 500 -- a roster that cannot be read "+
			"must not render as a business with nobody in it", rec.Code)
	}
	body := htmlOf(t, rec)
	for _, never := range []string{"Nobody on the books yet", "Nobody matches these filters"} {
		if strings.Contains(body, never) {
			t.Errorf("a failed read rendered %q, which is a claim about the world", never)
		}
	}
}

// TestEmployeesSection_IsTheOnlySectionRenderingTheRoster keeps the shell honest:
// the section table drives the router, and a body rendered on the wrong href would
// mean the switch in mountSections and the table have drifted.
func TestEmployeesSection_IsTheOnlySectionRenderingTheRoster(t *testing.T) {
	bodies := sectionBodies(t)
	const marker = `aria-label="Filter employees"`
	for _, s := range pages.PanelSections {
		has := strings.Contains(bodies[s.Href], marker)
		if s.Tab == pages.TabEmployees && !has {
			t.Errorf("GET %s does not render the roster's filter bar", s.Href)
		}
		if s.Tab != pages.TabEmployees && has {
			t.Errorf("GET %s renders the EMPLOYEES filter bar; the shell is not "+
				"switching sections", s.Href)
		}
	}
}

// errRosterUnavailable is what a database that did not answer looks like to the
// handler.
var errRosterUnavailable = errFake("roster read failed")

type errFake string

func (e errFake) Error() string { return string(e) }
