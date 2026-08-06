package handler

// transactions_test.go -- the nets for M6-03, the transactions section.
//
// SAME DISCIPLINE AS dashboard_test.go, and the two shapes it refuses are the same
// two: a TAUTOLOGY (comparing an expectation against the thing it is checking) and
// a FIXED LIST (a change detector, whose natural repair is to add the newcomer to
// the list). Every test below either asserts a property over shipped data, or
// derives its expectation from a source the product does not read.
//
// WHAT IS NOT HERE, ON PURPOSE. The two red lines this task actually touches --
// §4.5 tenant isolation and §4.7 "no coordinate reaches a screen" -- CANNOT be
// proved with a fake ledger, because a fake produces domain values that have no
// coordinate field to leak in the first place, which is exactly the tautology this
// file exists to avoid. Both are proved in transactions_db_test.go against real
// Postgres, over real HTTP, with rows that DO carry coordinates.

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/components"
)

// panelBrowserWith is panelBrowser over a ledger the caller controls.
func panelBrowserWith(t *testing.T, records panelLedger) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithLedger(t, admins, &fakeTrail{}, records))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// --- §4.7, the type-level half -------------------------------------------

// coordinateField matches the name of anything that could carry a location or an
// address, in any of the spellings the schema and the domain use.
var coordinateField = regexp.MustCompile(`(?i)^(gps|lat|lng|long|longitude|latitude|coord|sourceip|source_ip|ip)`)

// TestTypes_NothingOnTheReadPathCanCarryACoordinate is §4.7 asserted where it is
// cheapest to keep: in the types.
//
// 🔴 IT IS A PROPERTY OVER THE SHIPPED STRUCTS, NOT A LIST OF FIELD NAMES. The
// query, the domain record and the view model each have a field set; this walks
// all three and fails on any field whose NAME says it holds a coordinate or an
// address. A future edit that widens the SELECT to "just add gps_lat for a map"
// turns this red at the first of the three walls, before any template exists to
// render it.
//
// 🔴 THE SIGNS ARE THE EXEMPTION AND THEY ARE NAMED, so the rule cannot be
// satisfied by deleting the evidence entirely: IpMatch/GpsMatch are BOOLEANS and
// are required to be present. That is the positive control -- it is what makes
// this test fail if somebody "fixes" it by removing everything that matches.
func TestTypes_NothingOnTheReadPathCanCarryACoordinate(t *testing.T) {
	// booleans that ARE allowed: they carry the sign, never the value.
	allowed := map[string]bool{"IpMatch": true, "GpsMatch": true, "IPMatch": true, "GPSMatch": true, "IPSign": true, "GPSSign": true}

	walls := []struct {
		what string
		typ  reflect.Type
	}{
		{"the sqlc row (db/queries/transactions.sql, ListPanelTransactions)", reflect.TypeOf(store.ListPanelTransactionsRow{})},
		{"the domain record (internal/domain/ledger)", reflect.TypeOf(ledger.Record{})},
		{"the view model (web/templates/components)", reflect.TypeOf(components.DocketView{})},
	}
	for _, wall := range walls {
		fields := 0
		signs := 0
		for i := 0; i < wall.typ.NumField(); i++ {
			f := wall.typ.Field(i)
			fields++
			if allowed[f.Name] {
				signs++
				// A sign must be a BOOL or a word -- never a number, which is what a
				// coordinate would arrive as if somebody reused the name.
				k := f.Type.Kind()
				if k == reflect.Ptr {
					k = f.Type.Elem().Kind()
				}
				if k != reflect.Bool && k != reflect.String {
					t.Errorf("%s: %s is meant to be a SIGN but is a %s -- a sign says "+
						"whether the evidence matched, it never carries the value",
						wall.what, f.Name, f.Type)
				}
				continue
			}
			if coordinateField.MatchString(f.Name) {
				t.Errorf("%s carries a field named %q (%s).\n"+
					"§4.7: a raw coordinate never reaches a screen, a log or an "+
					"attribute. The panel shows the SIGNS (ip_match, gps_match), which "+
					"is what §5 rows 6-7 decided on. If this field is genuinely a sign, "+
					"name it so and make it a bool.", wall.what, f.Name, f.Type)
			}
		}
		if fields == 0 {
			t.Fatalf("%s has no fields at all -- this scan is reading the wrong type", wall.what)
		}
		// ANTI-VACUITY, and the one that matters: the rule above is satisfiable by
		// carrying no evidence at all, which would be a screen that cannot explain a
		// verdict. Both signs must be present on every wall.
		if signs < 2 {
			t.Errorf("%s carries %d of the two evidence signs. §4.7 is satisfied by "+
				"showing the signs INSTEAD of the values, not by showing neither -- a "+
				"docket with no evidence cannot explain its own verdict.", wall.what, signs)
		}
	}
}

// --- the HTMX contract ----------------------------------------------------

// htmxDynamicCodeSyntax matches the three htmx syntaxes that reach `new Function`
// or `eval` inside the library, and therefore the three that would force
// 'unsafe-inline' or 'unsafe-eval' into the panel's policy.
var htmxDynamicCodeSyntax = []struct {
	name string
	re   *regexp.Regexp
}{
	{"hx-on (an inline event handler; reaches new Function)", regexp.MustCompile(`(?i)\bhx-on`)},
	{"a js: prefix on hx-vals/hx-headers (reaches eval)", regexp.MustCompile(`(?i)hx-(vals|headers)\s*=\s*["']\s*js:`)},
	{"a bracketed event filter on hx-trigger (reaches eval)", regexp.MustCompile(`(?i)hx-trigger\s*=\s*["'][^"']*\[`)},
}

// TestPanelMarkup_UsesNoHtmxSyntaxThatNeedsUnsafeEval is what lets
// adminScriptedCSP stay free of 'unsafe-inline' and 'unsafe-eval'.
//
// 🔴 THE POLICY IS A PROMISE ABOUT MARKUP THAT DOES NOT EXIST YET. htmx.min.js
// contains exactly one `new Function` and one `eval(` (counted; see
// web/static/vendor/README.md), and both are unreachable ONLY because this product
// avoids the three attribute syntaxes below. Nothing enforces that but this test:
// a future hx-on:click would load fine in development, be blocked in a browser
// that honours the policy, and the obvious "fix" would be to widen the policy.
func TestPanelMarkup_UsesNoHtmxSyntaxThatNeedsUnsafeEval(t *testing.T) {
	bodies := sectionBodies(t)

	// The fragment endpoint too -- it is markup this product serves, and the swap
	// puts it into a document governed by the same policy. It is driven with a page
	// that HAS a continuation, because a last page renders no paging control at all
	// and would leave the scan below with no htmx attribute to judge.
	paging := ledgerWithRecords(3, true)
	paging.page.Next = &ledger.Cursor{At: time.Now().UTC(), ID: uuid.New()}
	b := panelBrowserWith(t, paging)
	for _, path := range []string{transactionsHref, docketFragmentPath} {
		rec := b.do(http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", path, rec.Code)
		}
		bodies["paging "+path] = htmlOf(t, rec)
	}

	// ANTI-VACUITY: this scan is only worth anything over markup that USES htmx.
	// Without this, deleting every hx-* attribute would leave the test green while
	// the pagination it guards had silently stopped working.
	hx := 0
	for _, body := range bodies {
		hx += strings.Count(body, "hx-get")
	}
	if hx == 0 {
		t.Fatal("no hx-get anywhere in the panel or the fragment -- either the paging " +
			"control regressed, or this scan reads the wrong bodies and would accept " +
			"any syntax at all")
	}

	for where, body := range bodies {
		for _, bad := range htmxDynamicCodeSyntax {
			if bad.re.MatchString(body) {
				t.Errorf("%s uses %s.\n"+
					"That syntax reaches htmx's dynamic-code path, which the panel's "+
					"Content-Security-Policy forbids (no 'unsafe-inline', no "+
					"'unsafe-eval'). The page would break in the browser and the "+
					"tempting repair is to widen the policy. Use a server-rendered "+
					"attribute instead.", where, bad.name)
			}
		}
	}
}

// TestPanelScript_IsVendoredAndServedFromOurOwnOrigin pins the no-CDN rule at the
// place it can actually be broken: the file has to EXIST in the embedded tree.
//
// A src under /static is necessary and not sufficient -- a path pointing at
// nothing renders identically and fails only in a browser.
func TestPanelScript_IsVendoredAndServedFromOurOwnOrigin(t *testing.T) {
	bodies := sectionBodies(t)
	srcRE := regexp.MustCompile(`(?is)<script\b[^>]*\bsrc="([^"]+)"`)

	found := 0
	for where, body := range bodies {
		for _, m := range srcRE.FindAllStringSubmatch(body, -1) {
			found++
			src := m[1]
			if !strings.HasPrefix(src, "/static/") {
				t.Errorf("%s loads %s, which is not served from our own origin", where, src)
				continue
			}
			// The embedded tree is what the binary actually ships.
			if _, err := web.Static().Open(strings.TrimPrefix(src, "/static/")); err != nil {
				t.Errorf("%s loads %s but that file is not in the embedded static tree: %v.\n"+
					"A script tag pointing at nothing looks correct in the markup and "+
					"fails only in a browser.", where, src, err)
			}
		}
	}
	if found == 0 {
		t.Fatal("no <script src> on any panel section -- M6-03 vendored HTMX for the " +
			"transactions section, so this scan is reading the wrong bodies")
	}
}

// --- §4.6: a screen may not claim what it has not measured -----------------

// TestTransactions_AFailedReadIsAnErrorAndNeverAnEmptyDay is the §4.6 net, and the
// class it closes is the one M5-11 closed: stating something the system has not
// measured.
//
// 🔴 THE FAILURE MODE IS SILENT AND PLAUSIBLE. A handler that logged the error and
// carried on would render a page saying "Nobody tapped a plaque on this day" --
// which is not a missing feature, it is the product telling a manager their staff
// did not turn up, on the strength of a database timeout.
func TestTransactions_AFailedReadIsAnErrorAndNeverAnEmptyDay(t *testing.T) {
	broken := newFakeLedger()
	broken.err = errors.New("database is unreachable")
	b := panelBrowserWith(t, broken)

	for _, path := range []string{transactionsHref, docketFragmentPath} {
		t.Run(path, func(t *testing.T) {
			rec := b.do(http.MethodGet, path, nil)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("GET %s answered %d when the read failed, want 500", path, rec.Code)
			}
			body := htmlOf(t, rec)
			for _, claim := range []string{"No taps recorded", "Nobody tapped", "No records match"} {
				if strings.Contains(body, claim) {
					t.Errorf("GET %s could not read the database and told the manager %q.\n"+
						"That is a claim about the world made without measuring it.", path, claim)
				}
			}
		})
	}
}

// TestTransactions_AnUnqueriedViewClaimsNothing is the same rule one layer down,
// at the view model, where the flag can be mutated on its own.
//
// It renders the page from a ZERO ledger.Page -- Queried false -- which is what a
// wiring bug would produce, and requires the template to say nothing at all rather
// than to say the day was empty.
func TestTransactions_AnUnqueriedViewClaimsNothing(t *testing.T) {
	silent := newFakeLedger()
	silent.page = ledger.Page{} // never queried: no Queried, no Zone, no records
	b := panelBrowserWith(t, silent)

	rec := b.do(http.MethodGet, transactionsHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", transactionsHref, rec.Code)
	}
	body := htmlOf(t, rec)
	for _, claim := range []string{"No taps recorded", "Nobody tapped", "No records match"} {
		if strings.Contains(body, claim) {
			t.Errorf("a view that was never queried rendered %q. The empty state must be "+
				"gated on having asked the database -- otherwise a page that failed to "+
				"query is indistinguishable from a quiet day.", claim)
		}
	}
	// Positive control: the SAME page with Queried set does say it, so the check
	// above is not passing merely because the words changed.
	asked := newFakeLedger() // Queried true, no records
	b2 := panelBrowserWith(t, asked)
	if got := htmlOf(t, b2.do(http.MethodGet, transactionsHref, nil)); !strings.Contains(got, "No taps recorded") {
		t.Error("a queried, genuinely empty day does NOT say so either -- the test above " +
			"is passing because the sentence does not exist, not because it is gated")
	}
}

// --- the filters ----------------------------------------------------------

// TestTransactionFilters_ReachTheQuery is the property the filter bar exists for:
// what a manager picks is what the database is asked.
//
// 🔴 IT ASSERTS THE VALUE ARRIVED, NOT THAT A PARAMETER EXISTS. The failure this
// catches is a filter rendered in the bar, submitted in the URL, and dropped on
// the way to the query -- which shows an unfiltered page that LOOKS filtered,
// because the bar echoes the selection back.
func TestTransactionFilters_ReachTheQuery(t *testing.T) {
	loc, dep := uuid.New(), uuid.New()
	fake := ledgerWithRecords(1, true)
	b := panelBrowserWith(t, fake)

	q := url.Values{
		"date":       {"2026-08-05"},
		"location":   {loc.String()},
		"department": {dep.String()},
		"employee":   {"Borg"},
		"verdict":    {"flag"},
		"channel":    {"qr"},
	}
	rec := b.do(http.MethodGet, transactionsHref+"?"+q.Encode(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	got := fake.lastFilter(t)
	checks := []struct {
		name string
		ok   bool
		got  any
	}{
		{"date", got.Date == ledger.Date{Year: 2026, Month: time.August, Day: 5}, got.Date},
		{"location", got.LocationID != nil && *got.LocationID == loc, got.LocationID},
		{"department", got.DepartmentID != nil && *got.DepartmentID == dep, got.DepartmentID},
		{"employee", got.EmployeeName == "Borg", got.EmployeeName},
		{"verdict", got.Verdict == "flag", got.Verdict},
		{"channel", got.Channel == "qr", got.Channel},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("the %s filter did not reach the query (got %v). The bar would still "+
				"show it as selected, so the manager sees a page that looks filtered "+
				"and is not.", c.name, c.got)
		}
	}
	if len(checks) != 6 {
		t.Fatalf("this test covers %d filters; M6-03's card names six", len(checks))
	}
}

// TestTransactionFilters_RefuseValuesTheDropdownNeverOffered is the boundary
// validation (§7), stated as the property that makes it safe: anything the form
// could not have produced is DROPPED, so the query never sees it.
//
// DROPPING RATHER THAN REFUSING IS A CHOICE WITH A COST, and the cost is asserted
// too: the bar must not echo a value that was dropped, or the manager would be
// shown a filter that is not in force.
func TestTransactionFilters_RefuseValuesTheDropdownNeverOffered(t *testing.T) {
	fake := ledgerWithRecords(1, true)
	b := panelBrowserWith(t, fake)

	q := url.Values{
		"location": {"not-a-uuid"},
		"verdict":  {"approved"}, // the COLUMN value is 'ok'; this is the label
		"channel":  {"sms"},
		"date":     {"the fifth of August"},
	}
	rec := b.do(http.MethodGet, transactionsHref+"?"+q.Encode(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d -- a hand-edited URL must not be an error page", rec.Code)
	}

	got := fake.lastFilter(t)
	if got.LocationID != nil {
		t.Errorf("an unparseable id reached the query: location=%v", got.LocationID)
	}
	if got.Verdict != "" {
		t.Errorf("verdict %q reached the query; the closed set is what the dropdown offers", got.Verdict)
	}
	if got.Channel != "" {
		t.Errorf("channel %q reached the query", got.Channel)
	}
	if !got.Date.Zero() {
		t.Errorf("an unparseable date reached the query as %v", got.Date)
	}

	body := htmlOf(t, rec)
	for _, dropped := range []string{"not-a-uuid", "the fifth of August", `value="sms"`} {
		if strings.Contains(body, dropped) {
			t.Errorf("the filter bar echoes %q, which was DROPPED. A control showing a "+
				"filter that is not in force is worse than one showing none.", dropped)
		}
	}
}

// TestTransactionFilters_EveryOfferedOptionIsAccepted closes the other direction,
// and it is the one that makes "one list" more than a comment.
//
// A dropdown that offers a value the validator rejects is a control that silently
// does nothing. This walks the options the product RENDERS and drives each one
// through the real handler.
func TestTransactionFilters_EveryOfferedOptionIsAccepted(t *testing.T) {
	fake := ledgerWithRecords(1, true)
	b := panelBrowserWith(t, fake)

	cases := []struct {
		param   string
		options []components.OptionView
		read    func(ledger.Filter) string
	}{
		{"verdict", panelVerdicts, func(f ledger.Filter) string { return f.Verdict }},
		{"channel", panelChannels, func(f ledger.Filter) string { return f.Channel }},
	}
	total := 0
	for _, c := range cases {
		if len(c.options) == 0 {
			t.Fatalf("%s offers no options at all; this test would pass over an empty set", c.param)
		}
		for _, o := range c.options {
			total++
			rec := b.do(http.MethodGet, transactionsHref+"?"+c.param+"="+url.QueryEscape(o.Value), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET with %s=%s: %d", c.param, o.Value, rec.Code)
			}
			if got := c.read(fake.lastFilter(t)); got != o.Value {
				t.Errorf("the %s dropdown offers %q but the handler read %q -- the "+
					"dropdown and the validator disagree, so a manager can pick a "+
					"filter that does nothing", c.param, o.Value, got)
			}
			if body := htmlOf(t, rec); !strings.Contains(body, `value="`+o.Value+`"`) {
				t.Errorf("the %s dropdown does not render an option with value %q", c.param, o.Value)
			}
		}
	}
	// The vocabularies are migration 0005's CHECK lists: four verdicts, three
	// channels. A shrunken list would make this pass over less than it should.
	if total != 7 {
		t.Errorf("walked %d options; migration 0005 fixes four verdicts and three "+
			"channels, so this should be 7. If the schema changed, the dropdown and "+
			"this number change together.", total)
	}
}

// --- paging ---------------------------------------------------------------

// TestTransactionPaging_CarriesTheFiltersAndTheCursor is what keeps "show more"
// from quietly widening the result.
//
// 🔴 THE FAILURE IS INVISIBLE TO A READER. A next-page link that forgot the
// verdict filter appends records that do not match what the manager asked for,
// into a list that still says it is filtered.
func TestTransactionPaging_CarriesTheFiltersAndTheCursor(t *testing.T) {
	fake := ledgerWithRecords(2, true)
	cursorAt := time.Date(2026, 8, 5, 9, 30, 0, 123456789, time.UTC)
	cursorID := uuid.New()
	fake.page.Next = &ledger.Cursor{At: cursorAt, ID: cursorID}

	b := panelBrowserWith(t, fake)
	rec := b.do(http.MethodGet, transactionsHref+"?date=2026-08-05&verdict=flag&channel=qr&employee=Borg", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := htmlOf(t, rec)

	hrefs := regexp.MustCompile(`(?is)<a\b[^>]*\bhx-get="([^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(hrefs) != 1 {
		t.Fatalf("found %d paging controls, want exactly 1", len(hrefs))
	}
	frag, err := url.Parse(strings.ReplaceAll(hrefs[0][1], "&amp;", "&"))
	if err != nil {
		t.Fatalf("parsing the paging URL: %v", err)
	}
	q := frag.Query()

	for _, want := range []struct{ key, value string }{
		{"date", "2026-08-05"},
		{"verdict", "flag"},
		{"channel", "qr"},
		{"employee", "Borg"},
		{"after_id", cursorID.String()},
	} {
		if got := q.Get(want.key); got != want.value {
			t.Errorf("the next-page link carries %s=%q, want %q. A page that drops a "+
				"filter appends records the manager did not ask for.", want.key, got, want.value)
		}
	}
	// THE CURSOR KEEPS ITS SUB-SECOND PRECISION. Truncating it would put every tap
	// sharing that second either back on the page just read or past the start of the
	// next -- a duplicate or a hole, both silent.
	at, err := time.Parse(time.RFC3339Nano, q.Get("after_at"))
	if err != nil {
		t.Fatalf("after_at %q does not parse as RFC3339Nano: %v", q.Get("after_at"), err)
	}
	if !at.Equal(cursorAt) {
		t.Errorf("after_at round-tripped to %s, want %s -- precision was lost and rows "+
			"will be skipped or repeated", at.Format(time.RFC3339Nano), cursorAt.Format(time.RFC3339Nano))
	}

	// AND IT DEGRADES: the same control is an ordinary link to a whole page, so a
	// browser with no JavaScript can still reach the next records.
	plain := regexp.MustCompile(`(?is)<a\b[^>]*\bhref="([^"]+)"[^>]*\bhx-get=`).FindStringSubmatch(body)
	if plain == nil {
		t.Fatal("the paging control has hx-get but no href. Without a script it would " +
			"do nothing at all, and a panel that stops paginating hides records.")
	}
	if !strings.HasPrefix(plain[1], transactionsHref+"?") {
		t.Errorf("the paging control's plain href is %q, which is not the section's own "+
			"URL -- without a script it must load the next page as a document", plain[1])
	}
}

// TestTransactionPaging_LastPageOffersNothingToPress. A "show more" that fetches
// an empty list is a control that lies about there being more.
func TestTransactionPaging_LastPageOffersNothingToPress(t *testing.T) {
	fake := ledgerWithRecords(3, true)
	fake.page.Next = nil
	b := panelBrowserWith(t, fake)

	body := htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))
	if strings.Contains(body, "hx-get") {
		t.Error("the last page still renders a paging control. There is nothing behind " +
			"it, so pressing it appends nothing and looks broken.")
	}
	// Positive control: with a cursor there IS one, so the check above is not
	// passing because the control never renders.
	fake2 := ledgerWithRecords(3, true)
	fake2.page.Next = &ledger.Cursor{At: time.Now().UTC(), ID: uuid.New()}
	b2 := panelBrowserWith(t, fake2)
	if !strings.Contains(htmlOf(t, b2.do(http.MethodGet, transactionsHref, nil)), "hx-get") {
		t.Error("a page WITH a next cursor renders no paging control either -- the test " +
			"above is vacuous")
	}
}

// --- the brand ------------------------------------------------------------

// TestTransactionDocket_IsADocketAndCarriesEveryFieldTheCardAsksFor.
//
// M6-03's card lists what each card must show: location, name, in/out, time,
// trust, the IP/GPS signs, the tag uid + ctr, and the stamp. This asserts all
// eight are rendered, and that the record is a DOCKET rather than a table row --
// the substitution skill tappa-brand's "Yapma" list ends with.
func TestTransactionDocket_IsADocketAndCarriesEveryFieldTheCardAsksFor(t *testing.T) {
	fake := newFakeLedger()
	fake.page.Records = []ledger.Record{{
		ID:           uuid.New(),
		OccurredAt:   time.Date(2026, 8, 5, 14, 3, 22, 0, time.UTC),
		Direction:    "in",
		Trust:        ptrInt16(100),
		Verdict:      "ok",
		Channel:      "nfc",
		TagUID:       "91AC7E5504A001",
		Ctr:          ptrInt32(641),
		IPMatch:      ptrBool(true),
		GPSMatch:     ptrBool(true),
		LocationName: "KF St Julians",
		EmployeeName: "Maria Borg",
	}}
	b := panelBrowserWith(t, fake)
	body := htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))

	for _, want := range []struct{ what, text string }{
		{"the location", "KF St Julians"},
		{"the person", "Maria Borg"},
		{"the direction", "in"},
		{"the time in the tenant's zone", "14:03:22"},
		{"the trust score", "100"},
		{"the tag uid", "91AC7E5504A001"},
		{"the counter", "641"},
		{"the verdict stamp", "APPROVED"},
	} {
		if !strings.Contains(body, want.text) {
			t.Errorf("the docket does not show %s (%q). M6-03's card lists it.", want.what, want.text)
		}
	}
	// The evidence is shown as a SIGN. Both signs, both readable as words.
	if !strings.Contains(body, "IP yes") || !strings.Contains(body, "GPS yes") {
		t.Error("the docket does not show the IP/GPS signs as words")
	}
	// It is a docket: the motif's class, and the stamp inside it.
	if !strings.Contains(body, `class="docket"`) {
		t.Error("the record is not rendered as a .docket. skill tappa-brand: a record is " +
			"a kitchen docket, never a plain table row.")
	}
	if strings.Contains(body, "<table") {
		t.Error("the transactions section renders a <table>. The brand's one explicit " +
			"prohibition is a table row in place of a docket.")
	}
}

// TestTransactionDocket_DistinguishesManualAndPractice -- M6-03's card asks for
// both to be visually distinguishable, and §5 requires it of `manual` (it appears
// separately in reports) and of `practice` (it never counts toward hours).
//
// ⚠️ AND IT ASSERTS WHAT THE PANEL MUST NOT SAY. Totalling hours is M6-07 and does
// not exist in this repo, so a sentence here about what a practice tap does not
// add up to would describe arithmetic the product has not done -- the failure
// M5-07 recorded as a finding.
func TestTransactionDocket_DistinguishesManualAndPractice(t *testing.T) {
	fake := newFakeLedger()
	fake.page.Records = []ledger.Record{
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "ok", Channel: "manual", Manual: true, EmployeeName: "Manual Person"},
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "ok", Channel: "nfc", Practice: true, EmployeeName: "Practice Person"},
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "ok", Channel: "nfc", EmployeeName: "Ordinary Person"},
	}
	b := panelBrowserWith(t, fake)
	body := htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))

	if !strings.Contains(body, "TRAINING") {
		t.Error("a practice record carries no TRAINING stamp")
	}
	if !strings.Contains(body, "Entered by a manager") {
		t.Error("a manual record is not marked as entered by a manager")
	}
	// Neither mark may appear on the ordinary record: exactly one TRAINING and
	// exactly one manual tally across three cards.
	if n := strings.Count(body, "TRAINING"); n != 1 {
		t.Errorf("TRAINING appears %d times across three records, want 1", n)
	}
	if n := strings.Count(body, "Entered by a manager</span>"); n != 1 {
		t.Errorf("the manual tally appears %d times across three records, want 1", n)
	}
	for _, forbidden := range []string{"toward your hours", "toward hours", "does not count", "not counted"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Errorf("the panel says %q about a practice tap. Hours are M6-07 and do not "+
				"exist yet; a screen must not describe arithmetic the product has not "+
				"done.", forbidden)
		}
	}
}

// TestTransactionDocket_EvidenceSignHasThreeStates is the §5/§4.7 detail that no
// other test here would catch.
//
// 🔴 COLLAPSING nil INTO "no" IS THE PLAUSIBLE BUG, and it is a lie about a
// record rather than a cosmetic one. migration 0005 keeps ip_match and gps_match
// NULLABLE to mean "this channel was never assessed on this" -- a manual entry has
// no network evidence, so reporting it as "no" accuses it of failing a check
// nobody ran, and a manager reading the queue would treat it as weak evidence.
//
// The three states are driven through the REAL template, so this measures what is
// rendered rather than what sign() returns.
func TestTransactionDocket_EvidenceSignHasThreeStates(t *testing.T) {
	fake := newFakeLedger()
	fake.page.Records = []ledger.Record{
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "ok", Channel: "nfc",
			EmployeeName: "Yes Person", IPMatch: ptrBool(true), GPSMatch: ptrBool(true)},
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "flag", Channel: "nfc",
			EmployeeName: "No Person", IPMatch: ptrBool(false), GPSMatch: ptrBool(false)},
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "ok", Channel: "manual",
			EmployeeName: "Manual Person", Manual: true}, // both nil: never assessed
	}
	b := panelBrowserWith(t, fake)
	body := htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))

	for _, want := range []struct{ state, text string }{
		{"matched", "IP yes"},
		{"did not match", "IP no"},
		{"was never assessed", "IP n/a"},
		{"matched", "GPS yes"},
		{"did not match", "GPS no"},
		{"was never assessed", "GPS n/a"},
	} {
		if !strings.Contains(body, want.text) {
			t.Errorf("no docket renders %q (evidence that %s).\n"+
				"The three states are not decoration: NULL means the channel was "+
				"never assessed, and rendering it as a failure accuses a manual "+
				"entry of failing a check nobody ran.", want.text, want.state)
		}
	}
	// The nil case must not be reported as a failure: exactly one card is manual,
	// and it must carry neither "IP no" nor "GPS no".
	// Go's regexp is RE2 and has no negative lookahead, so the card is isolated by
	// splitting on the article boundary rather than by a non-greedy exclusion.
	manual := ""
	for _, card := range strings.Split(body, "<article") {
		if strings.Contains(card, "Manual Person") {
			manual = card
			break
		}
	}
	if manual == "" {
		t.Fatal("could not isolate the manual record's docket")
	}
	for _, wrong := range []string{"IP no", "GPS no"} {
		if strings.Contains(manual, wrong) {
			t.Errorf("the manual record's docket says %q, but its evidence columns are "+
				"NULL -- that is 'not assessed', not 'failed'", wrong)
		}
	}
}

// --- helpers --------------------------------------------------------------

// ledgerWithRecords builds a fake holding n plain records.
func ledgerWithRecords(n int, queried bool) *fakeLedger {
	f := newFakeLedger()
	f.page.Queried = queried
	for i := 0; i < n; i++ {
		f.page.Records = append(f.page.Records, ledger.Record{
			ID:           uuid.New(),
			OccurredAt:   time.Date(2026, 8, 5, 9, i, 0, 0, time.UTC),
			Direction:    "in",
			Verdict:      "ok",
			Channel:      "nfc",
			EmployeeName: "Someone",
			LocationName: "Somewhere",
		})
	}
	return f
}

func ptrInt16(v int16) *int16 { return &v }
func ptrInt32(v int32) *int32 { return &v }
func ptrBool(v bool) *bool    { return &v }

// --- the gaps an audit found, closed ---------------------------------------

// TestPanelScreens_FormControlsCarryATouchTargetClass is the MARKUP half for form
// controls, and it exists because the existing pair had a hole exactly the shape
// of M6-03.
//
// 🔴 pressTargetsOf READS THE <a>/<button> FAMILY ONLY. That was complete while the
// panel's only controls were links and buttons. M6-03 added a date box and five
// selects -- six controls a manager actually presses, none of which any markup test
// could see. The stylesheet half can only check classes it is told about, so a
// control rendered with no touch-target class at all was invisible to both halves.
//
// CLAUDE.md §9 and skill tappa-brand ask for 44px on every control, not on every
// anchor.
func TestPanelScreens_FormControlsCarryATouchTargetClass(t *testing.T) {
	bodies := sectionBodies(t)
	// The sign-in form is the same brand surface and is held to the same rule.
	anon := newBrowser(t, newAdminRouter(t, &fakeAdmins{}, &fakeTrail{}))
	bodies["GET /admin/login"] = htmlOf(t, anon.do(http.MethodGet, "/admin/login", nil))

	// input/select/textarea. type="hidden" is excluded: it renders nothing and is
	// not a target -- that is the only exemption, and it is by TYPE rather than by
	// a list of element names somebody has to keep.
	controlRE := regexp.MustCompile(`(?is)<(input|select|textarea)\b([^>]*)>`)
	hiddenRE := regexp.MustCompile(`(?i)type\s*=\s*["']?hidden`)

	total := 0
	for where, body := range bodies {
		for _, m := range controlRE.FindAllStringSubmatch(body, -1) {
			attrs := m[2]
			if hiddenRE.MatchString(attrs) {
				continue
			}
			total++
			classes := ""
			if c := attrValueRE("class").FindStringSubmatch(attrs); len(c) == 2 {
				classes = c[1]
			}
			if !hasTouchTargetClass(classes) {
				t.Errorf("%s: <%s class=%q> is a form control carrying none of %v.\n"+
					"A control smaller than 44px is missed by a thumb, and a manager "+
					"checks this at the pass on a phone.", where, m[1], classes, panelTouchTargets)
			}
		}
	}
	if total == 0 {
		t.Fatal("found no visible form control on any panel screen -- M6-03 added six " +
			"to the transactions section and the sign-in form has two, so this scan " +
			"is reading the wrong bodies and would pass over anything")
	}
}

// TestDocketFragment_IsBehindTheSessionGate.
//
// 🔴 IT IS THE ONE ROUTE M6-03 ADDED THAT IS NOT IN pages.PanelSections, so
// TestPanelSections_EveryOneIsRoutedAndBehindTheGate -- which ranges over that
// table -- cannot see it. The fragment reads the same records as the section, so
// "behind the gate" has to be asserted for it explicitly rather than left to the
// route being registered next to ones that are.
func TestDocketFragment_IsBehindTheSessionGate(t *testing.T) {
	// Signed in: it answers.
	b := panelBrowserWith(t, ledgerWithRecords(1, true))
	if rec := b.do(http.MethodGet, docketFragmentPath, nil); rec.Code != http.StatusOK {
		t.Fatalf("signed in, GET %s: %d, want 200 (the negative below would be "+
			"vacuous over a route that answers nothing to anyone)", docketFragmentPath, rec.Code)
	}
	// Not signed in: it does NOT.
	anon := newBrowser(t, newAdminRouterWithLedger(t, &fakeAdmins{}, &fakeTrail{}, newFakeLedger()))
	rec := anon.do(http.MethodGet, docketFragmentPath, nil)
	if rec.Code == http.StatusOK {
		t.Errorf("GET %s answered 200 with no session. It returns the same records as "+
			"the section, so it must be behind the same gate.", docketFragmentPath)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("an unauthenticated GET %s went to %q, want /admin/login", docketFragmentPath, loc)
	}
}

// TestCoordinateScanner_RejectsTheThingsItExistsToReject is the negative control
// for the §4.7 type walls, and it exists because an audit blinded the scanner.
//
// 🔴 NEUTERING coordinateField LEFT TestTypes_... GREEN. Its anti-vacuity check
// required the two SIGNS to be present, which says the types are readable -- it
// never asked whether the scanner could still recognise a coordinate. A regexp that
// matches nothing is the exact failure mode that looks like success, and this file
// already carries the same control for the brand scanners.
func TestCoordinateScanner_RejectsTheThingsItExistsToReject(t *testing.T) {
	mustMatch := []string{
		"GpsLat", "GpsLng", "Latitude", "Longitude", "Lat", "Lng",
		"SourceIP", "SourceIp", "Coordinates", "IPAddress",
	}
	for _, name := range mustMatch {
		if !coordinateField.MatchString(name) {
			t.Errorf("coordinateField does not match %q, so a field with that name "+
				"could be added to the read path and TestTypes_... would stay green", name)
		}
	}
	mustNotMatch := []string{"Verdict", "Practice", "Trust", "Note", "TagUid", "OccurredAt", "Channel"}
	for _, name := range mustNotMatch {
		if coordinateField.MatchString(name) {
			t.Errorf("coordinateField matches %q, which carries no location -- an "+
				"over-broad scanner gets relaxed and stops guarding anything", name)
		}
	}
}

// --- the person filter, after it stopped being a list (user decision, 2026-08-06)

// TestEmployeeFilter_IsTypedAndRoundTripsExactly.
//
// 🔴 THE ROUND TRIP IS THE PROPERTY, and it is the bug the first version shipped.
// ILIKE reads % and _ as wildcards, so the typed name has to be escaped before it
// reaches Postgres -- and the first attempt escaped it in the HANDLER, which meant
// the escaped string was what the box echoed and what every paging link carried.
// Typing "100%" gave back "100\%", and submitting that again escaped the escape.
// The escaping belongs to the query (internal/domain/ledger); what travels through
// the handler, the view and the URL is exactly what was typed.
func TestEmployeeFilter_IsTypedAndRoundTripsExactly(t *testing.T) {
	for _, typed := range []string{"Borg", "Maria Borg", "100%", "a_b", `back\slash`, "  padded  "} {
		t.Run(typed, func(t *testing.T) {
			fake := ledgerWithRecords(1, true)
			fake.page.Next = &ledger.Cursor{At: time.Now().UTC(), ID: uuid.New()}
			b := panelBrowserWith(t, fake)

			rec := b.do(http.MethodGet, transactionsHref+"?employee="+url.QueryEscape(typed), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d", rec.Code)
			}
			want := strings.TrimSpace(typed)

			if got := fake.lastFilter(t).EmployeeName; got != want {
				t.Errorf("the handler passed %q to the query; the manager typed %q.\n"+
					"Escaping is the query layer's job precisely so this stays untouched.", got, want)
			}

			body := htmlOf(t, rec)
			// The box shows what was typed, HTML-escaped for the attribute and
			// nothing else. A backslash appearing here means the LIKE escaping
			// leaked into the view.
			if !strings.Contains(body, `value="`+htmlText(want)+`"`) {
				t.Errorf("the filter box does not echo %q back. A control that forgets "+
					"what was typed is unusable, and one that shows a MANGLED version "+
					"of it re-submits the mangling.", want)
			}
			// The paging link carries the same untouched value.
			if m := regexp.MustCompile(`(?is)<a\b[^>]*\bhx-get="([^"]+)"`).FindStringSubmatch(body); m != nil {
				u, err := url.Parse(strings.ReplaceAll(m[1], "&amp;", "&"))
				if err != nil {
					t.Fatalf("parsing the paging URL: %v", err)
				}
				if got := u.Query().Get("employee"); got != want {
					t.Errorf("the next-page link carries employee=%q, want %q -- paging "+
						"would silently change who is being filtered for", got, want)
				}
			}
		})
	}
}

// TestEmployeeFilter_BlankInputDoesNotNarrow. A box containing only spaces is "no
// filter", not "everyone whose name contains a space" -- which is what a bare trim
// would have produced, and it would have looked like a broken filter.
func TestEmployeeFilter_BlankInputDoesNotNarrow(t *testing.T) {
	for _, blank := range []string{"", "   ", "\t"} {
		fake := ledgerWithRecords(1, true)
		b := panelBrowserWith(t, fake)
		rec := b.do(http.MethodGet, transactionsHref+"?employee="+url.QueryEscape(blank), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		if got := fake.lastFilter(t).EmployeeName; got != "" {
			t.Errorf("employee=%q reached the query as %q, want no filter", blank, got)
		}
		if strings.Contains(htmlOf(t, rec), "No records match these filters") {
			t.Errorf("employee=%q made the page claim it was filtered", blank)
		}
	}
}

// TestFilterBar_RendersNoEmployeeRoster is the SAVING, asserted so it cannot come
// back by accident.
//
// 🔴 THE ROSTER WAS 96% OF THE PAGE. Measured on a real tenant: 835 319 of 867 233
// bytes were one <select> of 7 490 <option> elements, on a page that is
// Cache-Control: no-store and therefore re-sent on every view and every filter
// change. Venues and departments stay pickable because their count is bounded by
// how many places a business has; a payroll is not.
func TestFilterBar_RendersNoEmployeeRoster(t *testing.T) {
	fake := ledgerWithRecords(1, true)
	// A tenant with venues and departments, so the bounded lists ARE present and
	// this test cannot pass merely because no options render at all.
	for i := 0; i < 3; i++ {
		fake.opts.Locations = append(fake.opts.Locations, ledger.Option{ID: uuid.New(), Label: "Venue"})
		fake.opts.Departments = append(fake.opts.Departments, ledger.Option{ID: uuid.New(), Label: "Kitchen", Group: "Venue"})
	}
	b := panelBrowserWith(t, fake)
	body := htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))

	if !strings.Contains(body, `id="filter-employee"`) {
		t.Fatal("there is no employee filter at all -- the card names six filters")
	}
	// It is an <input>, not a <select>.
	sel := regexp.MustCompile(`(?is)<select\b[^>]*id="filter-employee"`)
	if sel.MatchString(body) {
		t.Error("the employee filter is a <select> again. A roster grows without bound " +
			"and was measured at 96% of the page; the control is a text box.")
	}
	if !regexp.MustCompile(`(?is)<input\b[^>]*id="filter-employee"`).MatchString(body) {
		t.Error("the employee filter is neither a select nor an input")
	}
	// POSITIVE CONTROL: the bounded lists really are rendering options, so "no
	// employee options" is a fact about the employee filter and not about the page.
	if n := strings.Count(body, "<option"); n < 6 {
		t.Fatalf("only %d <option> on the page; the venue and department lists should "+
			"be rendering, so this test cannot tell a missing roster from a missing "+
			"filter bar", n)
	}
}

// TestDocketFragment_UsesTheUnwidenedPolicy closes the hole the cardinality check
// structurally cannot see.
//
// 🔴 THE CARDINALITY CHECK WALKS pages.PanelSections, AND THE FRAGMENT IS NOT IN IT.
// That is deliberate -- it has no tab and appears in no navigation -- but it means
// the panel now has a protected URL outside the set that counts widened policies.
// An audit switched the fragment to renderScripted and the whole handler package
// stayed green.
//
// THE FRAGMENT MUST NOT BE SCRIPTED, and for a concrete reason rather than tidiness:
// it is inserted into a document whose policy already governs it, so widening its
// own adds nothing; and if somebody opens the URL directly it becomes a document
// that loads no script, which must therefore not permit one.
func TestDocketFragment_UsesTheUnwidenedPolicy(t *testing.T) {
	b := panelBrowserWith(t, ledgerWithRecords(1, true))
	rec := b.do(http.MethodGet, docketFragmentPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", docketFragmentPath, rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("the fragment answers with no Content-Security-Policy")
	}
	body := htmlOf(t, rec)
	if strings.Contains(strings.ToLower(body), "<script") {
		t.Error("the fragment carries a <script> tag; it is swapped into a document " +
			"that already loaded the library")
	}
	for _, widened := range []string{"script-src", "connect-src"} {
		if strings.Contains(csp, widened) {
			t.Errorf("the fragment sends %s in its policy (%q) while loading no script.\n"+
				"It is not in pages.PanelSections, so the cardinality check cannot see "+
				"it -- this is the only thing standing between the fragment and a "+
				"silently widened policy.", widened, csp)
		}
	}
	// Positive control: the SECTION does send the widened policy, so this test is
	// distinguishing two responses rather than reading a product with no policy.
	sec := b.do(http.MethodGet, transactionsHref, nil)
	if !strings.Contains(sec.Header().Get("Content-Security-Policy"), "script-src") {
		t.Error("the transactions section does not send the widened policy either -- " +
			"this test cannot tell the fragment apart from anything")
	}
}

// TestEmployeeFilter_TruncatesByCharacterNotByByte is the half of N1 that had NO
// net, and an audit proved it by putting the defect back and watching the suite
// stay green.
//
// 🔴 WHY THE 500 TEST CANNOT SEE THIS. The N1 repair did two things: it counted
// RUNES instead of bytes, and it added ToValidUTF8 as a backstop. The backstop
// SCRUBS the half-character a byte cut leaves behind, so with byte truncation
// restored the input is still valid UTF-8, Postgres accepts it, and
// TestPanelTransactionsDB_NameFilterSurvivesHostileInput -- which asserts "not a
// 500" -- passes over it. Only the NUL half was actually pinned.
//
// 🔴 IT IS NOT A NO-OP, MEASURED: for a 108-rune name of two-byte characters the
// shipped code keeps 100 runes and byte truncation keeps 50. Every Maltese name
// (ċ ġ ħ ż) and every Turkish one is two bytes per accented character, so the
// filter would silently use HALF of what the manager typed -- and still return a
// plausible page, which is the worst kind of wrong.
func TestEmployeeFilter_TruncatesByCharacterNotByByte(t *testing.T) {
	for _, tc := range []struct {
		name string
		char string
	}{
		{"Maltese z-dot (2 bytes)", "Ż"},
		{"Maltese h-bar (2 bytes)", "ħ"},
		{"Turkish dotted I (2 bytes)", "İ"},
		{"ASCII control arm (1 byte)", "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Longer than the limit, with a distinctive tail that must be cut off.
			in := strings.Repeat(tc.char, maxEmployeeNameFilter+8) + "TAILMARK"
			got := employeeNameFilter(in)

			if n := utf8.RuneCountInString(got); n != maxEmployeeNameFilter {
				t.Errorf("a %d-rune name came back as %d runes, want %d.\n"+
					"The limit counts CHARACTERS. Cutting at %d BYTES keeps only "+
					"%d of a two-byte character's worth, so the filter would use a "+
					"fraction of what was typed and still return a plausible page.",
					utf8.RuneCountInString(in), n, maxEmployeeNameFilter,
					maxEmployeeNameFilter, maxEmployeeNameFilter/len(tc.char))
			}
			if !utf8.ValidString(got) {
				t.Errorf("the truncated name is not valid UTF-8 (%q) -- Postgres refuses "+
					"it and the manager gets a 500", got)
			}
			if strings.Contains(got, "TAILMARK") {
				t.Errorf("the tail past the limit survived: %q", got)
			}
		})
	}

	// ANTI-VACUITY: a name INSIDE the limit must come back untouched, so the checks
	// above are measuring truncation rather than a function that mangles everything.
	short := "Ħaddiem Żammit"
	if got := employeeNameFilter(short); got != short {
		t.Errorf("a short name was altered: %q -> %q", short, got)
	}
}

// TestVendoredScript_MatchesTheRecordedDigest turns a written-down hash into a
// mechanism.
//
// 🔴 THE DIGEST WAS RECORDED AND NOTHING READ IT. web/static/vendor/README.md
// carries htmx's version, source URL and sha256, and an audit asked the obvious
// question: who checks it at the next upgrade? The answer was "discipline". No
// test hashed the file and `make audit` does not either, so a single changed byte
// -- a truncated download, a tampered mirror, a careless bump -- would ship a file
// that runs on the panel's one scripted page under script-src 'self', with every
// gate green.
//
// 🔴 IT READS THE DIGEST OUT OF THE README RATHER THAN HOLDING ITS OWN COPY. A
// constant here would be a SECOND representation of the same fact, and the two
// would drift the way every other pair in this task did. The README is the record;
// this recomputes it against what the binary actually embeds.
//
// WHAT IT DOES NOT CLAIM: this is not supply-chain provenance. It proves the file
// on disk is the file somebody wrote a hash for -- so an upgrade must update BOTH
// the bytes and the record, deliberately, in one edit. Whether that hash was ever
// the right one is a human check at vendoring time.
func TestVendoredScript_MatchesTheRecordedDigest(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "vendor", "README.md"))
	if err != nil {
		t.Fatalf("reading the vendor README: %v -- it is the record this test checks against", err)
	}

	// Every sha256 the record names, paired with the file it names beside it.
	fileRE := regexp.MustCompile("(?i)\\*\\*sha256\\*\\*\\s*\\|\\s*`?([0-9a-f]{64})`?")
	digests := fileRE.FindAllStringSubmatch(string(readme), -1)
	if len(digests) == 0 {
		t.Fatal("the vendor README records no sha256 at all. Either nothing is vendored " +
			"any more, or the record this test exists to enforce has been removed -- " +
			"which is not a way to make it pass.")
	}

	// The vendored files actually embedded in the binary.
	entries, err := fs.ReadDir(web.Static(), "vendor")
	if err != nil {
		t.Fatalf("reading the embedded vendor tree: %v", err)
	}
	checked := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		raw, err := fs.ReadFile(web.Static(), "vendor/"+e.Name())
		if err != nil {
			t.Fatalf("reading embedded vendor/%s: %v", e.Name(), err)
		}
		got := fmt.Sprintf("%x", sha256.Sum256(raw))
		found := false
		for _, d := range digests {
			if d[1] == got {
				found = true
				break
			}
		}
		if !found {
			var recorded []string
			for _, d := range digests {
				recorded = append(recorded, d[1][:16]+"...")
			}
			t.Errorf("vendor/%s hashes to %s, which the README does not record (it names %s).\n"+
				"A vendored file and its recorded digest must change in the SAME edit. "+
				"If this is an upgrade, update the version, size and sha256 in "+
				"web/static/vendor/README.md; if it is not, the file changed under you.",
				e.Name(), got[:16]+"...", strings.Join(recorded, ", "))
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no vendored .js in the embedded tree -- this test is guarding nothing")
	}
}
