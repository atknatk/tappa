package handler

import (
	"encoding/csv"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/web/templates/pages"
	"github.com/go-chi/chi/v5"
)

// The BILLING section's tests — M6-12 phase B.
//
// WHAT EACH BLOCK BELOW IS FOR, because this file answers eleven separate obligations
// phase A handed over and a reader should be able to find the one they came for:
//
//	the floor sentence          TestBilling_SaysTheCountIsAFloor{OnTheScreen,InTheCSV}
//	frozen vs draft             TestBilling_AFrozenMonthAndADraftDoNotRenderAlike
//	the founding warning        TestBilling_FoundingWarning*
//	the close route             TestBillingClose_*
//	no second audit row         TestBillingClose_WritesNoAuditRowFromTheHandler
//	the history cap             TestBilling_DeclaresTheHistoryCap
//	Money.String in the file    TestBillingCSV_WritesMoneyWithNoSymbolAndNoGrouping
//	the currency symbol         TestBillingMoney_PutsTheSymbolInTheRenderLayer
//	the empty month             TestBillingMonth_*
//	no itemisation offered      TestBilling_OffersNoItemisationAnywhere
//	the read path writes nothing TestBillingRead_WritesNothing

const billingTestOwnerRole = "owner"

// billingBrowser wires the panel with a billing register the test controls.
func billingBrowser(t *testing.T, books panelBooks, role string) *browser {
	t.Helper()
	return billingBrowserWithTrail(t, books, &fakeTrail{}, role)
}

// billingBrowserWithTrail is the same with the AUDIT SINK under the test's control, so
// obligation 5 — that the handler writes no second audit row — can be COUNTED rather
// than argued.
func billingBrowserWithTrail(t *testing.T, books panelBooks, trail *fakeTrail, role string) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "Owner Person",
		}, nil
	}}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), books, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// billingPage renders the section and fails loudly on anything but a 200.
func billingPage(t *testing.T, books panelBooks) string {
	t.Helper()
	rec := billingBrowser(t, books, billingTestOwnerRole).do(http.MethodGet, billingHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", billingHref, rec.Code)
	}
	return htmlOf(t, rec)
}

// billingFile downloads the export and returns the raw bytes.
func billingFile(t *testing.T, books panelBooks) string {
	t.Helper()
	rec := billingBrowser(t, books, billingTestOwnerRole).do(http.MethodGet, billingCSVHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", billingCSVHref, rec.Code)
	}
	return htmlOf(t, rec)
}

// --- obligation 1: the count is a FLOOR, and the figure is an UPPER BOUND ---------

// TestBilling_SaysTheCountIsAFloorOnTheScreen is phase A's first handover.
//
// 🔴 THE TWO HALVES ARE ASSERTED SEPARATELY BECAUSE THEY ARE DIFFERENT CLAIMS AND
// ONLY ONE OF THEM IS OBVIOUS. "The headcount is a floor" is the direction the figure
// moves the answer in; "the number beside it is an upper bound and not the shortfall"
// is the SIZE, and getting that one wrong turns an honesty column into a false
// adjustment a customer could try to reconcile against. Migration 0016 states the
// measurement: the column is tenant-wide and point-in-time.
func TestBilling_SaysTheCountIsAFloorOnTheScreen(t *testing.T) {
	quiet := newFakeBooks()
	if html := billingPage(t, quiet); strings.Contains(html, "floor") {
		t.Errorf("a month with no disagreeing records still says the count is a floor.\n" +
			"The sentence is an admission and must not be printed when there is nothing to admit.")
	}

	loud := newFakeBooks()
	loud.draft.UnstampedEmployees = 3
	html := billingPage(t, loud)
	for _, want := range []string{
		"FLOOR",                  // the direction, shouted
		"upper bound",            // the size
		"not the amount missing", // the shape it is NOT
		"3 records",              // the figure itself
	} {
		if !strings.Contains(html, htmlText(want)) {
			t.Errorf("the screen does not carry %q when unstamped_employees is 3.\n"+
				"Phase A produced the number and deliberately did not say it; saying it is this "+
				"phase's obligation, and an excluded row that is invisible is exactly what §4.6 forbids.", want)
		}
	}
}

func TestBilling_SaysTheCountIsAFloorInTheCSV(t *testing.T) {
	quiet := newFakeBooks()
	if file := billingFile(t, quiet); !strings.Contains(file, "Headcount complete,yes") {
		t.Errorf("with nothing to admit the file does not state a MEASURED yes.\n" +
			"The absence of a warning and the absence of a check look identical in a file.")
	}

	loud := newFakeBooks()
	loud.draft.UnstampedEmployees = 3
	file := billingFile(t, loud)
	if !strings.Contains(file, "Headcount complete") || !strings.Contains(file, "NO") {
		t.Fatalf("the file does not carry the floor admission at all")
	}
	for _, want := range []string{"FLOOR AND NOT A TOTAL", "UPPER BOUND", "NOT the number missing"} {
		if !strings.Contains(file, want) {
			t.Errorf("the export does not carry %q.\n"+
				"A short screen is noticed by a manager who knows the business; a short CSV is "+
				"pasted into a spreadsheet and paid.", want)
		}
	}
	// AND IT COMES BEFORE THE FIGURES. A note under a table is a note nobody scrolls to.
	if i, j := strings.Index(file, "Headcount complete"), strings.Index(file, "Amount due"); i < 0 || j < 0 || i > j {
		t.Errorf("the caveat block is not ahead of the figures (caveat at %d, amount at %d)", i, j)
	}
}

// --- obligation 2: a frozen figure and a live draft must not render alike ---------

// TestBilling_AFrozenMonthAndADraftDoNotRenderAlike is the obligation the whole task
// exists for, and it is asserted as a DIFFERENCE rather than as two expected strings.
//
// 🔴 THE ANTI-VACUITY HALF IS THE POINT. Two pages that happen to differ somewhere
// prove nothing; what has to be true is that the difference is about FINALITY and
// that a reader meets it in more than one place. So the test requires the word, the
// sentence and the control to differ, and it is measured going red when any one of
// the three is made identical.
func TestBilling_AFrozenMonthAndADraftDoNotRenderAlike(t *testing.T) {
	july := billing.Month{Year: 2026, Month: time.July}

	draftBooks := newFakeBooks()
	draftHTML := billingPage(t, draftBooks)

	frozenBooks := newFakeBooks()
	frozenBooks.frozen[july.String()] = frozenDraftFor(july, 24, 3600)
	frozenHTML := billingPage(t, frozenBooks)

	if draftHTML == frozenHTML {
		t.Fatal("a frozen month and a live draft render the identical page.\n" +
			"billing.Draft.Frozen: \"Rendering them identically is the failure this whole task " +
			"exists to prevent.\"")
	}
	// 1. THE STAMP — a word, not a colour (skill tappa-brand: status is never told by
	//    colour alone).
	if !strings.Contains(draftHTML, ">DRAFT<") {
		t.Error("an unfrozen month carries no DRAFT stamp")
	}
	if !strings.Contains(frozenHTML, ">FROZEN<") {
		t.Error("a frozen month carries no FROZEN stamp")
	}
	if strings.Contains(frozenHTML, ">DRAFT<") {
		t.Error("a FROZEN month also carries the DRAFT stamp")
	}
	// 2. THE SENTENCE. A stamp is a label; this is the claim about whether the number
	//    can still change.
	// ⚠️ THE PHRASE THIS ASSERTED ON WAS THE OVERSTATED ONE ("it will change if somebody
	// is hired"), which K3 replaced. What the draft must say is that it is NOT settled —
	// the exact reason differs by whether the month has ended, and
	// TestBillingDraft_DoesNotClaimMoreVolatilityThanTheDefinitionHas owns that split.
	// Here the claim is only that a draft says it is a draft.
	if !strings.Contains(draftHTML, htmlText("still a draft rather than a bill")) {
		t.Error("the draft does not say in words that its figure is not settled")
	}
	if !strings.Contains(frozenHTML, htmlText("Nothing recomputes")) {
		t.Error("the frozen month does not say in words that nothing recomputes it")
	}
	// 3. THE CONTROL. A month that is already frozen must not offer to freeze again.
	if !strings.Contains(draftHTML, billingCloseHref) {
		t.Error("a closable draft does not offer the freeze control")
	}
	if strings.Contains(frozenHTML, `action="`+billingCloseHref+`"`) {
		t.Error("a FROZEN month still offers a form posting to the close route")
	}
}

// TestBillingDraft_DoesNotClaimMoreVolatilityThanTheDefinitionHas is K3.
//
// 🔴 THE FIRST VERSION OF THE DRAFT SENTENCE OVERSTATED WHAT CAN CHANGE, and an audit
// caught it. It said "a live count of TODAY'S ROSTER … it will change if somebody is
// HIRED" — true of a month still running, FALSE of one that has ended. The count is
// over employment intervals that OVERLAP the month's own window
// (tappa_employee_is_billable, migration 0016), so a hire today cannot enter a closed
// period, and a deactivation today leaves that person counted for it.
//
// The direction was safe (it promised more movement than exists) and the CountLine
// underneath always carried the exact definition — but a loose sentence on the one
// screen that says what a customer owes is a sentence somebody plans around.
//
// THE TWO STATES ARE ASSERTED AS DIFFERENT, and the ended one is asserted NOT to make
// the hiring claim, which is the specific correction.
func TestBillingDraft_DoesNotClaimMoreVolatilityThanTheDefinitionHas(t *testing.T) {
	running := newFakeBooks()
	running.draft.HasEnded = false
	runningHTML := billingPage(t, running)

	ended := newFakeBooks() // HasEnded is true in the fixture
	endedHTML := billingPage(t, ended)

	if runningHTML == endedHTML {
		t.Fatal("a month that is still running and one that has ended render identically; " +
			"the two are different claims about what can still change")
	}
	// A RUNNING MONTH: the hiring claim is TRUE and must be made.
	if !strings.Contains(runningHTML, htmlText("still running")) ||
		!strings.Contains(runningHTML, htmlText("hired or leaving before it ends")) {
		t.Error("a month that is still running does not say that hiring still moves it")
	}
	// AN ENDED MONTH: the hiring claim is FALSE and must NOT be made.
	if strings.Contains(endedHTML, htmlText("hired or leaving before it ends")) {
		t.Error("an ENDED month claims that hiring somebody still changes it.\n" +
			"Measured against tappa_employee_is_billable: somebody activated today is NOT " +
			"billable for a period that has already closed.")
	}
	if !strings.Contains(endedHTML, htmlText("does NOT change it")) {
		t.Error("an ended month does not say that hiring or dismissing somebody no longer moves it")
	}
	// AND IT STILL SAYS WHY IT IS A DRAFT RATHER THAN A BILL — the honest residue, which
	// is the set of things that CAN still move it. Dropping the sentence altogether would
	// be the opposite error: a figure that looks settled and is recomputed on every view.
	for _, want := range []string{"joining or leaving dates", "timezone", "price", "Freezing stores the figures"} {
		if !strings.Contains(endedHTML, htmlText(want)) {
			t.Errorf("the ended-month sentence does not name %q as something that can still move it", want)
		}
	}
	// THE FILE CARRIES THE SAME SENTENCE, from the same function — a CSV that restated
	// the claim in its own words is exactly where the two would drift apart.
	file := billingFile(t, ended)
	if !strings.Contains(file, "does NOT change it") {
		t.Error("the export does not carry the corrected sentence")
	}
	if strings.Contains(file, "today's roster at today's price") {
		t.Error("the export still carries the overstated sentence")
	}
}

// TestBillingCSV_SaysWhichDocumentItIsInItsFirstFacts is the same obligation in the
// file, and it matters MORE there: in a spreadsheet the header scrolls away, and a
// provisional figure that looks final is how a draft gets invoiced.
func TestBillingCSV_SaysWhichDocumentItIsInItsFirstFacts(t *testing.T) {
	july := billing.Month{Year: 2026, Month: time.July}

	draft := billingFile(t, newFakeBooks())
	if !strings.Contains(draft, "Status,DRAFT") {
		t.Error("the exported draft does not carry Status,DRAFT")
	}
	if !strings.Contains(draft, "NOT A BILL") {
		t.Error("the exported draft does not say in a sentence that it is not a bill")
	}

	frozenBooks := newFakeBooks()
	frozenBooks.frozen[july.String()] = frozenDraftFor(july, 24, 3600)
	frozen := billingFile(t, frozenBooks)
	if !strings.Contains(frozen, "Status,FROZEN") {
		t.Error("the exported frozen month does not carry Status,FROZEN")
	}
	if strings.Contains(frozen, "NOT A BILL") {
		t.Error("a FROZEN export still carries the draft's disclaimer")
	}
	// THE STATUS IS THE SECOND ROW, i.e. before anything a reader could act on. The
	// first is the title the BOM lands on.
	if i, j := strings.Index(frozen, "Status,"), strings.Index(frozen, "Amount due"); i < 0 || j < 0 || i > j {
		t.Errorf("Status is not ahead of the figures (status at %d, amount at %d)", i, j)
	}
}

// --- obligation 3: the founding warning -------------------------------------------

// TestBilling_FoundingWarningFiresWhenTheFreeWindowHasRun is the card's third
// criterion: "a visible warning in the panel and in the report at the end of the third
// month — otherwise the free period quietly extends itself".
//
// ⚠️ NO DATE IS WRITTEN INTO THIS TEST AS A FACT ABOUT ANY REAL TENANT. Both cases
// build their own Draft, and the clock is a parameter — fillBillingView takes `now`
// precisely so this is provable without one. test/fixtures/seed.sql writes the design
// partners' created_at as `now() - interval '90 days'`, so a real tenant's window is a
// property of the day the database was seeded and cannot be asserted from a
// repository.
func TestBilling_FoundingWarningFiresWhenTheFreeWindowHasRun(t *testing.T) {
	sept := billing.Month{Year: 2026, Month: time.September}
	base := newFakeBooks().draft
	base.FirstChargeableMonth = sept

	cases := []struct {
		name    string
		now     time.Time
		wantEnd bool
	}{
		{"a month before the window ends", time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC), false},
		{"the first chargeable month itself", time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), true},
		{"long after", time.Date(2027, 2, 2, 12, 0, 0, 0, time.UTC), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v pages.BillingView
			fillBillingFounding(&v, base, tc.now)
			if v.FoundingHeading == "" || v.Founding == "" {
				t.Fatalf("no founding notice at all; the data is present so silence is the one wrong answer")
			}
			ended := strings.Contains(v.FoundingHeading, "have ended")
			if ended != tc.wantEnd {
				t.Errorf("heading %q: reports the window as ended=%v, want %v", v.FoundingHeading, ended, tc.wantEnd)
			}
			// EITHER WAY IT NAMES THE MONTH, which is the half that stops the end being a
			// surprise, and it is read from the row rather than computed here.
			if !strings.Contains(v.Founding, "September 2026") {
				t.Errorf("the notice does not name the first chargeable month: %q", v.Founding)
			}
		})
	}
}

// TestBilling_NoFoundingWarningWithoutTheRule is the negative half. A frozen row
// records the DECISION and not the rule, so FirstChargeableMonth is zero on one —
// inventing a warning from a zero month would print "no month" at a customer.
func TestBilling_NoFoundingWarningWithoutTheRule(t *testing.T) {
	base := newFakeBooks().draft
	base.FirstChargeableMonth = billing.Month{}
	var v pages.BillingView
	fillBillingFounding(&v, base, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if v.FoundingHeading != "" || v.Founding != "" {
		t.Errorf("a draft with no first-chargeable month still produced a founding notice: %q / %q",
			v.FoundingHeading, v.Founding)
	}
}

// TestBilling_TheFoundingWarningReachesTheScreen closes the gap between the unit above
// and the product: a sentence that is only ever produced by a helper is a sentence a
// customer never sees. This is the M5-04 shape — delivered, tested and dead.
func TestBilling_TheFoundingWarningReachesTheScreen(t *testing.T) {
	books := newFakeBooks()
	// A window that ended a long time ago, so the assertion does not depend on when the
	// suite runs.
	books.draft.FirstChargeableMonth = billing.Month{Year: 2000, Month: time.January}
	html := billingPage(t, books)
	if !strings.Contains(html, htmlText("free months have ended")) {
		t.Error("the founding warning is produced by fillBillingFounding and never reaches the page")
	}
	// AND IT IS THE FIRST THING ON THE PAGE, ahead of the month picker. A warning under
	// the figures is a warning nobody scrolls to, which is the "quietly" the card names.
	if i, j := strings.Index(html, htmlText("free months have ended")), strings.Index(html, `id="billing-month"`); i < 0 || j < 0 || i > j {
		t.Errorf("the founding warning is not ahead of the month picker (warning %d, picker %d)", i, j)
	}
}

// --- obligation 6: the history cap is DECLARED ------------------------------------

func TestBilling_DeclaresTheHistoryCap(t *testing.T) {
	quiet := newFakeBooks()
	quiet.history = []billing.Draft{frozenDraftFor(billing.Month{Year: 2026, Month: time.June}, 2, 300)}
	if html := billingPage(t, quiet); strings.Contains(html, htmlText("more frozen months than one page carries")) {
		t.Error("an un-truncated history still announces that it is shortened")
	}

	capped := newFakeBooks()
	capped.capped = true
	capped.history = []billing.Draft{frozenDraftFor(billing.Month{Year: 2026, Month: time.June}, 2, 300)}
	html := billingPage(t, capped)
	if !strings.Contains(html, htmlText("more frozen months than one page carries")) {
		t.Error("a truncated history does not say it is truncated (M6-07 and M6-11's rule)")
	}
	// THE NUMBER IS INTERPOLATED FROM THE CONSTANT, not written out, so the sentence
	// cannot drift from the code that enforces it. Asserting on the constant is what
	// makes that checkable.
	if !strings.Contains(html, "limit 60") {
		t.Errorf("the cap sentence does not carry billing.HistoryCap (%d)", billing.HistoryCap)
	}
	if !strings.Contains(html, htmlText("Nothing has been lost")) {
		t.Error("the cap is announced as a loss rather than as a page")
	}
	// AND THE FILE SAYS IT TOO.
	if file := billingFile(t, capped); !strings.Contains(file, "History complete") || !strings.Contains(file, "limit 60") {
		t.Error("the export does not declare the history cap")
	}
}

// --- obligations 7 and 9: money in the file vs money on the screen -----------------

// TestBillingCSV_WritesMoneyWithNoSymbolAndNoGrouping is phase A's seventh handover.
//
// 🔴 IT PARSES THE FILE RATHER THAN GREPPING IT. A substring search would pass on a
// document that also carried the symbol somewhere else; reading the cell back through
// encoding/csv asserts on the VALUE a consumer would get.
func TestBillingCSV_WritesMoneyWithNoSymbolAndNoGrouping(t *testing.T) {
	books := newFakeBooks()
	// A figure large enough that a grouping separator would show up, and a price with a
	// fractional part.
	books.draft.EmployeeCount = 1346
	books.draft.Free = false
	books.draft.AmountDue = billing.NewMoney(201900, "EUR")
	books.draft.UnitPrice = billing.NewMoney(150, "EUR")

	rows := parseBillingCSV(t, billingFile(t, books))
	amount := billingCell(t, rows, "Amount due")
	if amount != "2019.00" {
		t.Errorf("Amount due is %q, want %q — Money.String is symbol-free and grouping-free "+
			"on purpose: the symbol is the render layer's business and a separator would have to "+
			"be undone before the value could be parsed back", amount, "2019.00")
	}
	if price := billingCell(t, rows, "Price per person per month"); price != "1.50" {
		t.Errorf("unit price is %q, want %q", price, "1.50")
	}
	// THE CURRENCY IS ITS OWN COLUMN rather than glued to the digits.
	if cur := billingCellAt(t, rows, "Amount due", 2); cur != "EUR" {
		t.Errorf("the amount's currency column is %q, want EUR", cur)
	}
}

// TestBillingMoney_PutsTheSymbolInTheRenderLayer is obligation 9 from the other end:
// the SCREEN does add a symbol, and it picks it from the currency the row carries
// rather than assuming one.
func TestBillingMoney_PutsTheSymbolInTheRenderLayer(t *testing.T) {
	cases := []struct {
		name string
		in   billing.Money
		want string
	}{
		{"euro gets its symbol", billing.NewMoney(201900, "EUR"), "€2019.00"},
		{"zero is still an amount", billing.NewMoney(0, "EUR"), "€0.00"},
		// A CURRENCY WITH NO SYMBOL THIS PRODUCT KNOWS PRINTS THE CODE. An amount
		// labelled with the WRONG symbol is a wrong statement about money; a code is
		// always right.
		{"an unknown currency prints its code", billing.NewMoney(500, "GBP"), "5.00 GBP"},
		// THE ZERO Money IS NOT AN AMOUNT — Money.Valid's whole reason — so it must not
		// render as a plausible 0.00.
		{"no currency is not an amount", billing.Money{}, "—"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := money(tc.in); got != tc.want {
				t.Errorf("money(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	// AND billing.Money ITSELF STILL CARRIES NO SYMBOL, which is what makes the split
	// real rather than a convention.
	if s := billing.NewMoney(201900, "EUR").String(); strings.ContainsAny(s, "€$£,") {
		t.Errorf("billing.Money.String has acquired a symbol or a separator: %q", s)
	}
}

// TestBillingCSV_EveryRowIsAsWideAsItsOwnHeading is the structural net for a defect
// this file shipped and a DUMP found rather than a test.
//
// 🔴 A RAGGED FILE IS FINE; A RAGGED BLOCK IS A MISLABELLED COLUMN. The document holds
// four differently shaped tables on purpose — a spreadsheet opens it as one sheet, and
// parseBillingCSV sets FieldsPerRecord = -1 for exactly that reason. But WITHIN a
// block, a row wider than its heading puts a value under a name that does not describe
// it: two rows here carried a note in a column the heading called `currency`, and every
// existing assertion stayed green because they all keyed on column 0 or 1.
//
// ⚠️ A HEADING IS RECOGNISED BY ITS CELLS, NOT BY ITS POSITION, and the first draft of
// this scan got that wrong: it took "the row after a single-cell title" and therefore
// read the PREAMBLE's `Status,DRAFT,"…"` line as a heading, then reported all eight
// label/value rows under it as too narrow. The preamble is deliberately NOT a table —
// it is the label/value block reportCSVPreamble also writes, and the byte order mark's
// whole mitigation depends on its first cell being a human title rather than a column
// name. So the test looks for what reportCSVBOM calls a "machine-readable header row":
// every cell a lowercase identifier.
func TestBillingCSV_EveryRowIsAsWideAsItsOwnHeading(t *testing.T) {
	books := newFakeBooks()
	// A saturated fixture, so every conditional row is present: a floor admission, a
	// capped history, a free month, a founding rule and a frozen row.
	books.draft.UnstampedEmployees = 3
	books.draft.Free = true
	books.draft.FirstChargeableMonth = billing.Month{Year: 2026, Month: time.September}
	books.capped = true
	books.history = []billing.Draft{frozenDraftFor(billing.Month{Year: 2026, Month: time.June}, 20, 3000)}

	machineHeading := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	isHeading := func(row []string) bool {
		if len(row) < 2 {
			return false
		}
		for _, c := range row {
			if !machineHeading.MatchString(c) {
				return false
			}
		}
		return true
	}

	rows := parseBillingCSV(t, billingFile(t, books))
	blocks, width := 0, 0
	var heading []string
	for _, row := range rows {
		if len(row) == 0 || (len(row) == 1 && row[0] == "") {
			width = 0 // a blank line ends a block
			continue
		}
		if isHeading(row) {
			heading, width = row, len(row)
			blocks++
			continue
		}
		if width == 0 || len(row) == 1 {
			// Outside a table, or a one-cell sentence (the empty-history line).
			continue
		}
		if len(row) != width {
			t.Errorf("under the heading %v the row starting %q has %d cells, the heading has %d.\n"+
				"A value under a column name that does not describe it is worse than no name.",
				heading, row[0], len(row), width)
		}
	}
	// ANTI-VACUITY: the walk really found the tables, or every check above ran over
	// nothing. The document has three (the caveats, the figures, the history); the
	// preamble is a label/value block with no heading, on purpose.
	if blocks != 3 {
		t.Fatalf("the block walk found %d machine-readable heading(s), want 3 — the scan is "+
			"not seeing the document's shape, so the assertions above proved nothing", blocks)
	}
}

// --- obligation 11: no itemisation is offered anywhere ----------------------------

// TestBilling_OffersNoItemisationAnywhere is the obligation that is a NEGATIVE, and it
// is asserted as one: nothing on the screen or in the file may promise the people
// behind a figure.
//
// 🔴 THE PROMISE IS UNKEEPABLE RATHER THAN MERELY UNBUILT, which is why this is a test
// and not a backlog item. A frozen period stores the NUMBER and not the identities
// (migration 0016 says why the ids were rejected rather than forgotten), and anybody
// anonymised under GDPR since would not resolve even if they had been stored. A
// control that offered it would be making a promise the data cannot keep.
func TestBilling_OffersNoItemisationAnywhere(t *testing.T) {
	july := billing.Month{Year: 2026, Month: time.July}
	books := newFakeBooks()
	books.frozen[july.String()] = frozenDraftFor(july, 24, 3600)
	books.history = []billing.Draft{frozenDraftFor(billing.Month{Year: 2026, Month: time.June}, 20, 3000)}

	html := billingPage(t, books)
	file := billingFile(t, books)

	// 1. STRUCTURALLY THERE IS NOWHERE FOR ONE TO LIVE. Anything offering to break the
	//    count down would have to be a link or a form, and the section owns exactly four
	//    addresses. So every target in the page that points INTO the section must be one
	//    of them — a fifth would be the itemisation route, whatever it was called.
	//
	//    ⚠️ THIS IS ASSERTED OVER TARGETS RATHER THAN OVER WORDS, and the first draft of
	//    this test did the second. It scanned the whole page for "employee" and went red
	//    on the NAVIGATION's own Employees tab — a section that has nothing to do with
	//    billing and legitimately appears on every panel page. A word scan over a page
	//    that carries shared chrome measures the chrome.
	allowed := map[string]bool{
		billingHref: true, billingCSVHref: true,
		billingCloseHref: true, billingFreezeHref: true,
	}
	for _, target := range panelTargets(html) {
		if !strings.HasPrefix(target, billingHref) {
			continue
		}
		// A month parameter narrows the SAME document; anything else is a new capability.
		base, query, _ := strings.Cut(target, "?")
		if !allowed[base] {
			t.Errorf("the billing section links to %q, which is not one of its four addresses.\n"+
				"A frozen month stores the NUMBER of people and not which people, so a fifth "+
				"address is a promise this data cannot keep.", target)
			continue
		}
		if query != "" && !strings.HasPrefix(query, "month=") && !strings.HasPrefix(query, "problem=") {
			t.Errorf("the billing section links to %q, whose query is neither a month nor a "+
				"problem word", target)
		}
	}
	// 2. AND IT SAYS SO, which is the other half: a limit that is merely absent looks
	//    like a feature nobody got round to.
	if !strings.Contains(html, htmlText("NUMBER of people, not which people")) {
		t.Error("the screen does not state that only the count is kept")
	}
	if !strings.Contains(file, "no itemisation of a") {
		t.Error("the export does not state that no itemisation exists or can be produced")
	}
	// 3. AND NO PERSON'S NAME COULD TRAVEL HERE EVEN IF ONE WERE ADDED UPSTREAM. §4.7's
	//    wall is the domain's — billing.Draft carries no employee field — and this is the
	//    render side of it: the only NAME on the page is the business's.
	if !strings.Contains(html, htmlText(newFakeBooks().draft.TenantName)) {
		t.Error("the page does not even name the business it invoices")
	}
}

// panelTargets is every href and action in a rendered page.
func panelTargets(html string) []string {
	var out []string
	for _, attr := range []string{`href="`, `action="`} {
		rest := html
		for {
			i := strings.Index(rest, attr)
			if i < 0 {
				break
			}
			rest = rest[i+len(attr):]
			j := strings.Index(rest, `"`)
			if j < 0 {
				break
			}
			out = append(out, htmlUnescaper.Replace(rest[:j]))
			rest = rest[j:]
		}
	}
	return out
}

// --- obligation 10: the empty month is resolved, never passed through --------------

// TestBillingMonth_NeverAsksTheStoreForTheZeroMonth is the sharp half of obligation
// 10. billing.Month{} is a legitimate REQUEST ("no month given") and a nonsense
// QUERY: Month.date renders it through time.Date, which normalises month 0 to
// December of the year before, so the store would happily compute a period for
// -0001-12 and return a plausible-looking row.
func TestBillingMonth_NeverAsksTheStoreForTheZeroMonth(t *testing.T) {
	books := newFakeBooks()
	billingPage(t, books)
	for _, m := range books.closedMonths {
		t.Fatalf("a GET closed %q", m)
	}
	// The double records every month it was asked for through the drafts it returns;
	// asking it directly is simpler and is what the production code does.
	d, err := books.Period(t.Context(), panelTestTenant, billing.Month{})
	if err != nil {
		t.Fatalf("Period: %v", err)
	}
	if d.Month.Zero() {
		// This is a statement about the DOUBLE, not the handler: it echoes what it was
		// asked. If it echoes the zero month the handler's guard is the only thing
		// between billing.Month{} and the SQL, which is exactly what the next case
		// measures.
		t.Log("the double echoes the zero month, as expected")
	}
	// THE HANDLER'S GUARD. billingMonth must resolve before it reads, so no read is
	// ever made for the zero month.
	h := billingHandlerFor(t, books)
	got, err := h.billingMonth(t.Context(), panelTestTenant, billing.Month{})
	if err != nil {
		t.Fatalf("billingMonth: %v", err)
	}
	if got.Month.Zero() {
		t.Fatal("billingMonth resolved the empty request to the ZERO month.\n" +
			"Month.date would render that as -0001-12-01 and the query would answer for it.")
	}
}

// TestBillingMonth_ResolvesTheLastFinishedMonthInTheTenantsZone is the §6 half.
//
// 🔴 "THE LAST FINISHED MONTH" IS A QUESTION ABOUT A ZONE. Malta runs UTC+1/+2, so for
// the one-to-two hours between local midnight on the 1st and UTC midnight on the 1st
// the two clocks disagree about which month it is — at 00:30 in Malta on 1 September
// the local answer is August and the UTC answer is July. The zone lives in the
// database and cannot be known before a read, so the resolution guesses in UTC, reads,
// and re-reads only when the tenant's own zone disagrees.
func TestBillingMonth_ResolvesTheLastFinishedMonthInTheTenantsZone(t *testing.T) {
	books := newFakeBooks()
	h := billingHandlerFor(t, books)

	// A NAMED MONTH COSTS ONE READ AND NO GUESS.
	before := countPeriods(books)
	if _, err := h.billingMonth(t.Context(), panelTestTenant, billing.Month{Year: 2026, Month: time.March}); err != nil {
		t.Fatalf("billingMonth: %v", err)
	}
	if n := countPeriods(books) - before; n != 1 {
		t.Errorf("a NAMED month cost %d reads, want 1 — the guess is only for an empty request", n)
	}

	// AND THE RESOLVED MONTH IS THE ONE BEFORE THE CURRENT ONE IN THE TENANT'S ZONE.
	// The assertion is derived from the same clock the handler uses rather than from a
	// written-down date, so it cannot go stale.
	zone, err := time.LoadLocation(books.draft.Zone)
	if err != nil {
		t.Skipf("this machine has no tzdata for %s", books.draft.Zone)
	}
	want := billing.MonthOf(time.Now(), zone).Add(-1)
	got, err := h.billingMonth(t.Context(), panelTestTenant, billing.Month{})
	if err != nil {
		t.Fatalf("billingMonth: %v", err)
	}
	if got.Month != want {
		t.Errorf("the empty request resolved to %s, want %s (the last finished month in %s)",
			got.Month, want, books.draft.Zone)
	}
}

// billingHandlerFor builds the handler alone, for the two unit cases above that need
// to call an unexported method rather than drive HTTP.
func billingHandlerFor(t *testing.T, books panelBooks) *AdminAuth {
	t.Helper()
	h, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), books, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	return h
}

func countPeriods(b *fakeBooks) int {
	_, periods, _ := b.readCounts()
	return periods
}

// --- the read path writes nothing --------------------------------------------------

// TestBillingRead_WritesNothing is the property M6-07, M6-09 and M6-11 each measured
// for their own section, and it is sharpest here: freezing a month is PERMANENT, so a
// read path that froze anything would hand an irreversible commercial act to whoever
// opened a page first — a crawler, a prefetch, a retried request.
//
// 🔴 WHAT "WRITES NOTHING" MEANS HERE, STATED NARROWLY BECAUSE ONE THING CHANGED. The
// claim is that no GET can FREEZE a period — i.e. reach billing.Book.Close and append
// to billing_periods, the table that takes no UPDATE and no DELETE. It is NOT "no GET
// touches any table": /admin/billing.csv deliberately writes one audit_log access row,
// exactly as the hours export does (billingcsv.go carries that argument). Those are
// different tables and different claims, and TestBillingDB_ReadingTheScreenFreezesNothing
// counts billing_periods for the same reason.
//
// 🔴 TWO HALVES, AND NEITHER IS SUFFICIENT ALONE. The STATIC half parses this package
// and requires that no function reachable from a GET names Close; the LIVE half drives
// every address the section owns over real HTTP and counts. The static half catches a
// call added tomorrow on a branch no test exercises; the live half catches a write
// that arrives some other way than a call this scan can see.
//
// ⚠️ THE STATIC HALF'S LIMIT, COUNTED RATHER THAN CLOSED. It matches the SYNTAX
// `<x>.books.Close(...)`, so a call reached through an intermediate value —
// `c := a.books; c.Close(...)`, or the method passed as a func value — is invisible to
// it. Closing that properly needs type resolution (go/types or golang.org/x/tools/
// go/ssa), which is a dependency this repository does not carry for one test. What
// keeps the property standing meanwhile is the LIVE half, which counts actual calls
// through the real router and does not care how the call was spelled. This is written
// down rather than papered over: a boundary that is "where the search stopped" must say
// so, which is the rule reportscsv.go's escaping boundary already follows.
func TestBillingRead_WritesNothing(t *testing.T) {
	// --- static: which functions call books.Close, and are any of them GET handlers?
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	callers := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				ast.Inspect(fn, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Close" {
						return true
					}
					// a.books.Close(...)
					inner, ok := sel.X.(*ast.SelectorExpr)
					if ok && inner.Sel.Name == "books" {
						callers[fn.Name.Name] = true
					}
					return true
				})
			}
		}
	}
	if len(callers) == 0 {
		t.Fatal("no function in this package calls books.Close.\n" +
			"That is not a pass: the freeze route would be dead, and this scan would be " +
			"asserting over an empty set.")
	}
	// EXACTLY ONE CALLER, AND IT IS THE APPLY ROUTE. A second one is not automatically
	// wrong, but it is a decision somebody has to make on purpose rather than acquire.
	if len(callers) != 1 || !callers["billingCloseApply"] {
		t.Errorf("books.Close is called from %v, want exactly {billingCloseApply}", callers)
	}

	// --- live: drive every GET this section owns and count the writes.
	july := billing.Month{Year: 2026, Month: time.July}
	books := newFakeBooks()
	books.history = []billing.Draft{frozenDraftFor(billing.Month{Year: 2026, Month: time.June}, 20, 3000)}
	b := billingBrowser(t, books, billingTestOwnerRole)
	for _, path := range []string{
		billingHref,
		billingHref + "?month=" + july.String(),
		billingHref + "?month=not-a-month",
		billingHref + "?problem=already-closed",
		billingCSVHref,
		billingCSVHref + "?month=" + july.String(),
	} {
		if rec := b.do(http.MethodGet, path, nil); rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d, want 200", path, rec.Code)
		}
	}
	if n := books.closeCount(); n != 0 {
		t.Errorf("reading the billing screens froze %d period(s).\n"+
			"A GET that freezes a month hands a permanent, uncorrectable commercial act to "+
			"whoever opens a page first.", n)
	}
}

// --- helpers ------------------------------------------------------------------------

// parseBillingCSV reads the document back the way a consumer would.
//
// FieldsPerRecord = -1 BECAUSE THE DOCUMENT IS DELIBERATELY RAGGED: one file holds
// four differently shaped tables separated by blank lines, which is the shape
// reportCSV already ships and which a spreadsheet opens as one sheet. Go's reader
// enforces a uniform record width unless told otherwise, and python's csv does not —
// so a test that did not set this would be measuring Go's default rather than the
// file.
func parseBillingCSV(t *testing.T, doc string) [][]string {
	t.Helper()
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(doc, reportCSVBOM)))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("the billing export is not readable as CSV: %v", err)
	}
	return rows
}

func billingCell(t *testing.T, rows [][]string, label string) string {
	t.Helper()
	return billingCellAt(t, rows, label, 1)
}

func billingCellAt(t *testing.T, rows [][]string, label string, col int) string {
	t.Helper()
	for _, row := range rows {
		if len(row) > col && row[0] == label {
			return row[col]
		}
	}
	t.Fatalf("no row labelled %q in the export", label)
	return ""
}

// billingForm is a POST body for the close routes.
func billingForm(month, token string) url.Values {
	v := url.Values{}
	if month != "" {
		v.Set("month", month)
	}
	if token != "" {
		v.Set(confirmField, token)
	}
	return v
}
