package handler

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"regexp"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/httpx"
)

// The REPORTS SECTION's CSV EXPORT — M6-07 phase B.
//
// 🔴 IT ADDS NO ARITHMETIC, AND THAT IS THE FIRST OBLIGATION PHASE A HANDED OVER.
// Everything below reads a ledger.ReportScreen that reportsSection's own reader
// produced and turns it into bytes. There is no second accumulator, no second
// pairing rule and no second definition of "worked": a figure that appeared on the
// screen and a figure in the file are the SAME field of the SAME value, formatted by
// the SAME functions (hoursMinutes, and wholeMinutes beside it). A second summation
// here would be the failure class this repository has paid for repeatedly — two
// implementations that are each self-consistent and disagree about payroll, with the
// disagreement invisible because nobody diffs a spreadsheet against a web page.
// TestReportCSV_AgreesWithTheScreenItWasExportedFrom pins it, and it is measured
// going red when the CSV totals its own rows instead.
//
// 🔴 §4.7 — THIS FILE ADDS NO COLUMN TO ANY QUERY, and phase A's wall is exactly
// that: db/queries/transactions.sql's ListWorkedShiftEvents selects no gps_lat, no
// gps_lng, no source_ip, no policy_context and no entered_by, and the ledger types it
// fills have no field for them. The export is the place where "enrich the report a
// little" is most tempting, because a file feels private in a way a screen does not —
// it is the opposite. A screen is looked at once by somebody signed in; a file is
// emailed to an accountant, kept on a laptop and forwarded. Nothing here reaches past
// ledger.Report, so a coordinate has nothing to travel in.
//
// ⚠️ ONE COLUMN IS NAMED manager_entered AND IT IS NOT transactions.entered_by. It is
// ledger.PersonHours.Manual / ledger.OpenEntry.Manual, a BOOLEAN meaning "the record
// was typed rather than tapped" (channel='manual'). WHO typed it is the entered_by
// column §4.7 keeps out of this path, and it is neither selected nor carried.
//
// 🔴 §4.6 — EVERY QUANTITY THE PAYROLL FIGURE EXCLUDES IS WRITTEN INTO THE FILE, in
// its own block, before the figures. The screen already does this; the file needs it
// MORE, because its reader is a payroll process rather than a person. A short screen
// is noticed by a manager who knows the business; a short CSV is pasted into a
// spreadsheet and paid.
//
// WHAT IS DELIBERATELY NOT HERE: no policy.Evaluate call. See reportExportAuthority.

// reportsCSVHref is the export's URL, DERIVED FROM THE SECTION'S so the two cannot
// drift apart. If the section moves, the export moves with it.
var reportsCSVHref = reportsHref + ".csv"

// ActionReportExported is the audit vocabulary entry for a bulk hours export. The
// action name is free text by schema decision (00005), so the constant IS the
// vocabulary — the same rule the activation flow's actions follow.
const ActionReportExported = "report.exported"

// reportExportAuthority records what does NOT gate this route, because a reader
// looking for "where is the authorisation?" must find the answer in the code rather
// than in a delivery note.
//
// 🔴 policy.ActionReportExport IS DEFINED AND IS NOT EVALUATED HERE. The engine's only
// integration today is the tap path; NO panel handler calls policy.Evaluate. Wiring
// one here was weighed and refused for a measured reason rather than a stylistic one:
// the managed baseline grants this action to BOTH panel roles — internal/policy's
// base:authz-owner and base:authz-manager each list it — so a role gate on this route
// would refuse nobody who can reach it, while looking in the source like a gate that
// does something. A door that is measured to be always open, drawn as a door, is worse
// than no door: the next reader stops looking.
//
// WHAT ACTUALLY STANDS IN FRONT OF THIS ROUTE TODAY is the Protect chain (a signed
// panel session, resolved against admin_users, inside the address and session
// budgets) and the tenant scope that session carries. The real per-role gate is
// M6-09's, which is the task that builds the screen where these statements are
// managed; when it lands, this constant is where it attaches.
//
// 🔴 THE CLAIM ABOVE IS MEASURED RATHER THAN ASSERTED, and it has to be, because it
// is the whole justification for not wiring a gate. Two properties, two tests, both in
// reportscsv_test.go: TestReportExportAuthority_IsGrantedToBothPanelRolesByTheBaseline
// reads internal/policy's own baseline and requires the action to be allowed for BOTH
// roles (so a gate would refuse nobody), and TestPanel_CallsThePolicyEngineNowhere
// parses this package and requires zero policy.Evaluate calls (so this route is not
// the odd one out). When either stops being true, the comment above stops being true
// and a test says so.

// reportCSVBOM is the UTF-8 byte order mark, and whether to emit it is the one
// question in this file with no answer that is right for every reader.
//
// MEASURED, BOTH WAYS (2026-08-10, on this machine, with the files in the delivery
// notes):
//
//	consumer                       without BOM        with BOM
//	Go encoding/csv                clean              first header cell carries
//	python csv, encoding=utf-8     clean              first header cell carries
//	python csv, utf-8-sig          clean              clean
//	cut -d, -f1 | od -c            clean              leading ef bb bf on field 1
//	Excel (Windows, default)       NOT MEASURED HERE — the documented behaviour is that
//	                               a BOM-less UTF-8 CSV is opened in the system code
//	                               page, which destroys ħ ġ ċ ż and ı ş ğ ç
//
// 🔴 IT IS EMITTED, AND THE DECIDING ARGUMENT IS WHOSE NAME BREAKS. Both markets this
// product ships to are exactly the ones a missing BOM damages: Maltese employee names
// carry ħ ġ ċ ż and Turkish ones ı ş ğ ç. A mangled NAME in a payroll file is a wrong
// statement about a person, and it is silent — nobody diffs it. What the BOM costs, by
// contrast, is three bytes on the FIRST cell of the FIRST line, which this document
// deliberately makes a human-readable title rather than a column heading: every
// machine-readable header row in this file is preceded by a blank line and is
// therefore untouched. TestReportCSV_PutsTheBOMWhereNoColumnHeadingIs pins that
// placement, because it is the whole of the mitigation.
//
// ⚠️ AND IT IS A COUNTED LIMIT RATHER THAN A CLOSED ONE. A consumer that reads THIS
// FILE's first cell byte-for-byte sees the mark. The escape hatch is one line in every
// mainstream reader (utf-8-sig in Python, a TrimPrefix in Go), and the alternative —
// silently corrupting the names of Maltese and Turkish employees — has no escape hatch
// at all.
// (Written as an escape rather than as the character itself: the Go compiler refuses
// a literal BOM anywhere but the first byte of a source file — measured, not assumed.)
const reportCSVBOM = "\uFEFF"

// reportsExport answers the whole week as a file.
//
// 🔴 IT IS A GET, AND THE TWO CHAINS WERE MEASURED BEFORE CHOOSING (M6-06's rule: do
// not copy half a pattern).
//
//	Protect()         floodGate -> requireAdmin -> sessionGate
//	ProtectWriting()  floodGate -> sameOriginGate -> requireAdmin -> sessionGate
//
// So the address shield DOES cover reads: this route inherits the same per-address
// ceiling and the same per-session budget the HTML report has, and an anonymous caller
// pays the shield before the resolver. What ProtectWriting adds is the Origin check.
//
// WHY NOT ADD IT ANYWAY. sameOriginGate is strict: with no Origin header it falls back
// to Sec-Fetch-Site and REFUSES when that is missing too. A download is reached by a
// link, and a browser sends no Origin on a plain navigation — so the gate would rest
// entirely on Sec-Fetch-Site, which browsers older than the panel's own floor do not
// send. That trades a bounded nuisance for a manager who cannot download their payroll
// at all, on a route that changes no product state.
//
// 🔴 WHAT THAT LEAVES OPEN, COUNTED (backlog T14's class). This route has ONE side
// effect: an audit_log row. A cross-origin page can drive a signed-in manager's
// browser through a TOP-LEVEL navigation here and cause one — inflating the trail the
// export is audited by. What bounds it, measured rather than hoped:
//
//   - the panel cookie is SameSite=Lax (internal/adminauth/cookie.go), so a
//     SUBRESOURCE fetch — img, iframe, fetch, XHR — carries no cookie and reaches
//     requireLogin instead. Only a top-level navigation sends it, which navigates the
//     victim's own tab and downloads a file to their machine: loud, and one at a time.
//   - floodGate and sessionGate charge it like every other panel request, so the trail
//     cannot be inflated faster than the panel can be used at all.
//   - the row names the actor, so a burst is attributable rather than anonymous.
//
// The alternative — making the download a POST — was weighed: it buys the Origin check
// and costs the property that a week is an ADDRESS. Every other week control on this
// screen is a link, the report is bookmarkable and shareable inside the tenant, and a
// POST download cannot be re-run from history. The exposure above did not justify it.
//
// 🔴 §4.6 — A FAILED READ IS AN ERROR PAGE, NEVER AN EMPTY FILE. This is sharper than
// on the screen: a CSV of zero hours downloads without complaint and gets paid. So a
// read that errors, and a report that was never queried at all, both render the panel's
// problem page and send no file.
func (a *AdminAuth) reportsExport(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f := parseReportFilter(r)

	screen, err := a.ledger.Hours(r.Context(), id.TenantID(), f)
	if err != nil {
		a.log.Error("panel: could not read the week's hours for export", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	if !screen.Queried {
		// 🔴 THE ANTI-FABRICATION FLAG, ENFORCED RATHER THAN PRINTED. "Nobody worked
		// this week" is a claim about a business; a zero Report renders as a valid
		// CSV of zeroes and is indistinguishable from a real quiet week once it is in
		// a spreadsheet. The separation this file uses is that the FILE'S EXISTENCE is
		// the claim that the database answered — an unqueried report produces no file
		// at all, so there is nothing to misread.
		a.log.Error("panel: refusing to export a report the database never answered")
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	doc, err := reportCSV(screen, time.Now())
	if err != nil {
		a.log.Error("panel: could not render the hours export", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	// 🔴 THE TRAIL IS WRITTEN BEFORE THE BYTES GO OUT, AND IN ITS OWN TRANSACTION.
	//
	// WHY NOT audit.RecordTx, WHICH IS WHAT M6-06's WRITES USE. RecordTx needs a
	// pgx.Tx the caller owns so the row shares the fate of the surrounding change.
	// There is no such transaction here: ledger.Reader.Hours opens and closes its own
	// read, and threading a transaction out of a READER so a handler can write inside
	// it would make the read path a write path — for a row that is not conditional on
	// the read in the first place.
	//
	// WHY A FAILED AUDIT WRITE DOES NOT REFUSE THE DOWNLOAD, which is the sharper
	// question — and the answer has to be stated NARROWLY, because the first version of
	// it claimed more than is true. It said "the same numbers are already reachable
	// without any audit row on GET reportsHref". That holds for the TOTALS and NOT for
	// the rows: maxReportRows and maxOpenRows bound the screen's listings at a hundred
	// each, the reports screen has no cursor at all (reports.go says so: "this page
	// cannot page"), so the hundred-and-first person's row is reachable HERE and
	// nowhere else in this section.
	//
	// SO THE ACCURATE VERSION, WITH THE NUANCE THAT SURVIVES IT: what the export adds
	// over the screen is a bulk artefact and an ACCOUNTABILITY question, not a
	// confidentiality one. The underlying records are readable row by row from the
	// paginated Transactions section — which writes no audit row either — so refusing
	// this download on a database blip would not withhold anything from anybody
	// determined; it would only cost a manager their payroll file. And nothing is LOST
	// when the trail write fails: §4.6 is about the attendance record, and this is an
	// access trail beside it. a.record logs the failure loudly (§7: never swallowed).
	a.record(r.Context(), audit.Event{
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionReportExported,
		Target:   screen.Period.FirstDay.String(),
		Detail:   exportDetail(screen, len(doc)),
	})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	// 🔴 nosniff MATTERS MORE HERE THAN ON THE PANEL'S HTML, which is why it is set
	// rather than inherited: this response is a FILE a browser may be talked into
	// rendering. Content sniffing on a document whose first bytes are attacker-
	// influenced text is the classic way an "attachment" becomes a page.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 🔴 no-store BECAUSE THIS IS PAYROLL. A cached copy in an intermediary or in a
	// browser's back/forward store is one shared machine away from being somebody
	// else's. The panel's HTML sets it for the same reason (a.render); this route
	// cannot use a.render — that helper writes text/html and the panel CSP — so the
	// header is set here rather than acquired.
	w.Header().Set("Content-Disposition", `attachment; filename="`+reportCSVFilename(screen.Period.FirstDay.String())+`"`)
	w.Header().Set("Cache-Control", "no-store")
	// Content-Length is the reason the document is BUFFERED rather than streamed: with
	// it, a download cut short by a dropped connection is a BROKEN download the browser
	// reports, instead of a silently short payroll file with a 200 on it. See
	// reportCSV for what that costs in memory.
	w.Header().Set("Content-Length", strconv.Itoa(len(doc)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(doc); err != nil {
		a.log.Error("panel: writing the hours export failed", "err", err)
	}
}

// exportDetail is the audit row's typed payload.
//
// 🔴 IT IS A PURPOSE-BUILT STRUCT AND EVERY FIELD IS A COUNT (internal/audit's own
// rule: the package cannot scrub, so the caller must not hand it anything to scrub).
// There is no employee name, no employee id, no venue and nothing from a record —
// what a bulk export needs to be answerable for is WHO exported WHICH week and HOW
// MUCH, not a second copy of the data inside the trail.
type reportExportDetail struct {
	Week          string `json:"week"`
	Zone          string `json:"zone"`
	FromUTC       string `json:"from_utc"`
	ToUTC         string `json:"to_utc"`
	People        int    `json:"people"`
	Venues        int    `json:"venues"`
	OpenEntries   int    `json:"open_entries"`
	Shifts        int    `json:"shifts"`
	WorkedMinutes int    `json:"worked_minutes"`
	// Truncated travels with the row because an export of a FLOOR is a different
	// event from an export of a week, and the difference matters most when somebody
	// is reading the trail months later asking why two figures disagree.
	Truncated bool `json:"truncated"`
	Bytes     int  `json:"bytes"`
}

func exportDetail(s ledger.ReportScreen, size int) reportExportDetail {
	return reportExportDetail{
		Week:          s.Period.FirstDay.String(),
		Zone:          zoneOf(s).String(),
		FromUTC:       isoUTC(s.Period.From),
		ToUTC:         isoUTC(s.Period.To),
		People:        len(s.People),
		Venues:        len(s.Venues),
		OpenEntries:   len(s.Open),
		Shifts:        s.Totals.Shifts,
		WorkedMinutes: wholeMinutes(s.Totals.Worked),
		Truncated:     s.Truncated,
		Bytes:         size,
	}
}

// reportCSVName is the shape a filename may have, and the guard is the whole of the
// header-injection defence on this route.
//
// 🔴 THE TENANT'S NAME IS NOT IN THE FILENAME, DELIBERATELY. Content-Disposition is a
// header whose value is quoted, and a tenant name is free text a customer types: a
// business called `x" ; filename="payroll.html` would break out of the quotes.
// (Newlines cannot: net/http replaces CR and LF in header values on the way out — that
// half is Go's, measured, and it is not the half that matters here.) So the filename
// is built from ONE server-derived value, the period's first local day, and this
// pattern is what makes "server-derived" checkable rather than believed: anything that
// is not an ISO date falls back to a constant name.
var reportCSVName = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

func reportCSVFilename(day string) string {
	if !reportCSVName.MatchString(day) {
		return "tappa-hours.csv"
	}
	return "tappa-hours-" + day + ".csv"
}

// spreadsheetSafe neutralises a cell a spreadsheet would treat as a FORMULA.
//
// 🔴 templ's ESCAPING DOES NOT REACH THIS FILE, WHICH IS THE ENTIRE REASON THIS
// FUNCTION EXISTS. Every string on the reports SCREEN is escaped by the template
// engine on its way into HTML; a CSV is written by encoding/csv, which correctly
// escapes CSV syntax (commas, quotes, newlines) and knows nothing about Excel. A cell
// beginning `=`, `+`, `-` or `@` is evaluated as a formula when the file is opened,
// and `=cmd|'/c calc'!A0` is the shape that makes that a code-execution bug rather
// than a display bug.
//
// 🔴 THE RULE IS "STRIP, THEN LOOK", AND THE FIRST VERSION OF THIS FUNCTION GOT IT
// WRONG IN A WAY ONLY A MUTATION FOUND. It listed SIX triggers — the four above plus
// TAB (0x09) and CARRIAGE RETURN (0x0D), which is OWASP's list — and a comment here
// claimed the two extras were "the way past a four-character check". Measured, that
// claim was false in both directions:
//
//	tab and CR are DEAD in the trigger list.   unicode.IsSpace is true for both, so the
//	    loop below steps over them and finds the `=` anyway. Deleting them from the
//	    switch changed NO answer, which is why the mutation that deleted them left the
//	    escaping test green — a branch defended by a comment and reachable by nothing.
//	a leading NON-SPACE CONTROL character was a real hole.   "\x01=1+1" is not a space
//	    (unicode.IsSpace(0x01) is false), so the old loop hit `default` and returned it
//	    UNCHANGED. A spreadsheet that discards control characters before parsing then
//	    sees `=1+1`. That gap was in the shipped code and in the test's blind spot at
//	    the same time, because the test's own sweep only looked at the FIRST rune.
//
// A SECOND REVIEW THEN FOUND THE SAME MISTAKE ONE CATEGORY WIDER, and that is the
// finding worth carrying: the fix above wrote the step-over as
// unicode.IsSpace || unicode.IsControl, and Go's IsControl is category Cc ALONE, so
// every FORMAT character (Cf) still walked through in front of a live formula. The two
// halves are now named classes with their own predicates — see startsAFormula and
// hidesTheStartOfACell, which carry the measurements — and
// TestSpreadsheetSafe_RefusesEveryTriggerAndLeavesOrdinaryCellsAlone has a case for
// each class, each one measured going red on its own.
//
// 🔴 IT IS APPLIED TO EVERY CELL, INCLUDING THE ONES THIS PRODUCT WRITES ITSELF, and
// that is structural rather than diligent: reportDoc.row is the only way a byte
// reaches the file, and it escapes unconditionally. A review that decided which cells
// are "ours" and therefore safe would be a review that has to be repeated every time a
// column is added — and "Unknown employee" is ours.
//
// 🔴 WHAT THE FILE COSTS, AND THERE ARE **TWO** LOSSY STEPS RATHER THAN THE ONE THIS
// PARAGRAPH USED TO ADMIT. It said the apostrophe was the only change and that a
// byte-exact reader "can strip it" — true of the apostrophe, and not the whole story.
// Measured, round-tripping a cell through this writer and back through encoding/csv:
//
// ⚠️ THE "in the file" COLUMN IS BYTES ON THE WIRE, AND THE THIRD ROW USED TO SAY
// "quoted, unchanged" — which is wrong: the writer turns a lone LF into CRLF inside
// the quoted field, so the BYTES change and only the ROUND TRIP is faithful.
// "Preserved" and "unchanged" are different claims, and this table is where the
// difference lives.
//
//		value as stored        in the file          read back        verdict
//		"Maria\rBorg"          "MariaBorg"          "MariaBorg"      the CR is DELETED
//		"Maria\r\nBorg"        "Maria\r\nBorg"      "Maria\nBorg"    the CR is folded away
//		"Maria\nBorg"          "Maria\r\nBorg"       "Maria\nBorg"    round-trips
//		"=1+1"                 "'=1+1"              "'=1+1"          the apostrophe, recoverable
//
//	 1. THE APOSTROPHE. Recoverable exactly: one TrimPrefix, and it is applied only to a
//	    cell that begins with a trigger, so an ordinary name, an ISO date and every figure
//	    in this document pass through untouched. That half is measured by the
//	    ordinary-cell list in TestSpreadsheetSafe_RefusesEveryTriggerAndLeavesOrdinaryCellsAlone,
//	    and it is what keeps this from being a corrupting escape.
//	 2. A BARE CARRIAGE RETURN INSIDE A VALUE IS LOST, and NOT recoverable. csv.Writer
//	    with UseCRLF drops a CR that is not part of a CRLF; the reader folds CRLF to LF.
//	    `employees.full_name` is unbounded `text` with no CHECK, so a name really can hold
//	    one — a paste from a Windows field, or somebody trying to break a record in two.
//
// WHY THAT IS NOT FIXED HERE, weighed rather than shrugged at. Turning UseCRLF off
// preserves the CR (measured: "Maria\rBorg" survives intact) at the price of LF line
// endings — and it would put a bare CR inside a quoted field, which is precisely the
// byte a lenient parser splits a record on. So the choice is between losing a character
// nobody legitimately puts in a name and shipping a file that some readers cut in half;
// RFC 4180 line endings win. What makes it acceptable is that this file is a RENDERING
// and not the record: the CR is still in Postgres, verbatim, and §4.3's immutable row is
// untouched. TestReportCSV_LosesABareCarriageReturnAndSaysSo pins the behaviour so the
// next reader finds it measured rather than discovers it.
func spreadsheetSafe(s string) string {
	for _, r := range s {
		switch {
		case startsAFormula(r):
			return "'" + s
		case hidesTheStartOfACell(r):
			continue
		default:
			return s
		}
	}
	return s
}

// startsAFormula is the set a spreadsheet reads as "this cell is code".
//
// THE FIRST FOUR ARE THE M6-07 CARD's AND THEY ARE THE MEASURED ONES. The rest are
// their FULL-WIDTH twins plus U+2212 MINUS SIGN, and they are here as a DECISION under
// uncertainty rather than as a measurement — stated as such because this repository
// does not let an unmeasured thing pass for a measured one.
//
// ⚠️ WHETHER A SPREADSHEET EVALUATES `＝1+1` COULD NOT BE MEASURED HERE: there is no
// spreadsheet engine on this machine, and two independent reviewers reached the same
// dead end. So the question was decided on the COST OF BEING WRONG, which is lopsided
// in a way that settles it:
//
//	escape and be wrong    an apostrophe on a cell that is already text. Recoverable by
//	                       one TrimPrefix, and a name beginning with a full-width
//	                       arithmetic sign is a shape no roster in either market
//	                       produces.
//	skip and be wrong      a spreadsheet that normalises full-width forms to ASCII
//	                       before parsing — which is exactly what a CJK input layer does
//	                       elsewhere in the same product — evaluates the cell.
//
// WHAT IS DELIBERATELY NOT HERE: the en dash, the em dash and the other look-alike
// dashes. They are not arithmetic in any spreadsheet, and `—` is a character this
// product PRINTS itself (the screen's empty-day cell), so refusing it would be a net
// that fires on the product's own vocabulary.
func startsAFormula(r rune) bool {
	switch r {
	case '=', '+', '-', '@':
		return true
	// 🔴 THE COMPATIBILITY VARIANTS OF THOSE FOUR, WHICH IS A CLOSED SET RATHER THAN A
	// LIST SOMEBODY KEPT EXTENDING. Unicode gives each of `=`, `+`, `-` and `@` a
	// full-width form, a small form and (for the arithmetic three) a superscript and a
	// subscript form, and NFKC folds every one of them onto the ASCII character. This
	// is that fold, written out — the fold itself lives in golang.org/x/text, which is
	// not a dependency of this repository and is not worth becoming one for fourteen
	// runes. U+2212 MINUS SIGN is the one addition that is not a compatibility variant:
	// it is the typographic minus a word processor substitutes for a hyphen.
	case '\uFF1D', '\uFE66', '\u207C', '\u208C', // = full-width, small, super, sub
		'\uFF0B', '\uFE62', '\u207A', '\u208A', // + full-width, small, super, sub
		'\uFF0D', '\uFE63', '\u207B', '\u208B', // - full-width, small, super, sub
		'\uFF20', '\uFE6B', // @ full-width, small
		'\u2212': // MINUS SIGN
		return true
	}
	return false
}

// hidesTheStartOfACell reports whether r is something a READER cannot see as the
// beginning of a value, and which a spreadsheet may therefore discard before it
// decides what the cell is.
//
// 🔴 THE FIRST VERSION OF THIS TEST WAS unicode.IsSpace(r) || unicode.IsControl(r),
// AND IT HAD A HOLE THAT THIS REPOSITORY HAD ALREADY CLOSED ONCE. Go's IsControl is
// category Cc ONLY — the stdlib says so in one line, "all control characters are <
// MaxLatin1" — so the whole FORMAT category (Cf) walked straight past it. Measured end
// to end, seeded as an employee name and downloaded over real HTTP, thirteen Cf runes
// reached the file UNESCAPED in front of a live formula: U+FEFF, U+200B/C/D, U+00AD,
// U+2060, U+202D/E, U+200F, U+2066, U+2069, U+0600, U+180E.
//
// 🔴 AND THE PATTERN WAS SITTING IN THIS REPOSITORY, IN A FILE THAT NAMES THIS TASK.
// internal/session's device-label validator already refuses Cc and Cf as separate
// cases, and its own doc comment hands formula neutralising to "the CSV writer's job
// (M6-07)". This function inherited half of that classification — the fifth time this
// project has paid for copying half a pattern, and the first where the other half was
// one file away with a comment pointing here.
//
// Mn AND Me ARE HERE FOR THE SAME REASON, AND THEIR COST WAS MEASURED RATHER THAN
// ASSUMED: a non-spacing or enclosing mark renders with zero advance width, so it hides
// a trigger exactly as a Cf rune does (U+034F, U+0301 and U+17B4 all reached the file
// raw before this). Adding them to the SKIP set cannot produce a false escape — skipping
// never escapes on its own, it only keeps looking — which is why the ordinary-cell
// control list in the tests is unchanged by it. Mc is NOT here: a spacing combining mark
// occupies width, so it is not a hiding place.
func hidesTheStartOfACell(r rune) bool {
	return unicode.IsSpace(r) || // Zs, Zl, Zp and the ASCII whitespace controls
		unicode.Is(unicode.Cc, r) || // C0, C1, DEL — what IsControl covers, named
		unicode.Is(unicode.Cf, r) || // zero-width, BiDi overrides, soft hyphen
		unicode.Is(defaultIgnorable, r) || // the Hangul fillers and friends — see below
		unicode.Is(unicode.M, r) || // EVERY mark: Mn, Mc, Me
		r == utf8.RuneError || // what a range loop yields for invalid UTF-8
		r == brailleBlank // So, and it renders as nothing
}

// defaultIgnorable is the limb of Unicode's Default_Ignorable_Code_Point that Cf and
// the mark categories do not already cover.
//
// 🔴 IT IS THE PROPERTY THE BOUNDARY WAS MISSING. Default_Ignorable is Unicode's own
// name for "a renderer is permitted to display nothing here", which is precisely the
// question this function asks — and the CATEGORIES alone do not answer it: U+3164
// HANGUL FILLER is category **Lo**, an ordinary LETTER, and renders as nothing in most
// fonts. A review found it and three siblings (U+115F, U+1160, U+FFA0) walking through
// a classification built out of Cc/Cf/M, and its verdict was the right one: the
// boundary was not a principle, it was where the search had stopped. Go ships the
// property, so a principle replaces the list.
//
// ⚠️ Variation_Selector IS THE THIRD LIMB OF THAT PROPERTY AND IT IS DELIBERATELY NOT
// HERE, because adding it was tried and MEASURED DEAD: over the whole code space, every
// rune unicode.Properties["Variation_Selector"] matches is already matched by
// unicode.M or by this table — exclusive-rune count ZERO. Every other limb of
// hidesTheStartOfACell has exclusive runes (IsSpace 19, Cc 59, Cf 170, this one 3 773,
// M 2 187, and one apiece for the two constants). A branch no rune can reach is the
// shape this task already shipped once, with tab and CR, and had to withdraw.
var defaultIgnorable = unicode.Properties["Other_Default_Ignorable_Code_Point"]

// brailleBlank is U+2800 BRAILLE PATTERN BLANK: category So, not default-ignorable, and
// it renders as blank space in every braille font.
//
// 🔴 IT IS A NAMED EXCEPTION AND NOT A CLASS, WHICH IS THE HONEST SHAPE OF WHAT IS LEFT.
// The three tables above are principles; this is one rune somebody found. Writing it as
// a constant with this sentence beside it is the difference between a boundary that is
// argued and one that is enumerated.
const brailleBlank = '\u2800'

// 🔴 WHAT IS STILL OPEN, ENUMERATED RATHER THAN IMPLIED — because the honest summary of
// this function is that its boundary is a MEASURED LIST with three principles inside it,
// not a proof. A review put it exactly right: a boundary can be "where the search
// stopped" while looking like a rule. These are the classes known to be outside it:
//
//   - RUNES THAT RENDER INVISIBLY IN A PARTICULAR FONT without being default-ignorable,
//     a mark, or whitespace. U+2800 is the one that was found; there is no property that
//     enumerates the rest, because "renders as nothing" is a font question and Unicode
//     does not answer it. Any future one is a one-line addition beside brailleBlank.
//   - HOMOGLYPHS THAT ARE NOT COMPATIBILITY-EQUIVALENT to the four triggers. The
//     compatibility fold is closed and startsAFormula carries all of it; a look-alike
//     Unicode does NOT relate to `=` (a mathematical or fullwidth cousin outside NFKC)
//     would pass. Nothing in the roster measured produces one.
//   - EAST ASIAN WIDTH is not consulted at all; Go's stdlib does not ship the table and
//     golang.org/x/text is not a dependency of this repository.
//
// ⚠️ AND THE HALF THAT CANNOT BE MEASURED HERE AT ALL: whether a spreadsheet actually
// EVALUATES any of the non-ASCII triggers, or actually DISCARDS any of the skipped
// classes, was not measured — there is no spreadsheet engine on this machine, and three
// independent reviews reached the same wall. Everything above is reasoning about what a
// spreadsheet MAY do, applied in the direction where being wrong is cheap
// (startsAFormula states that asymmetry). It is not a claim that any of these is
// exploitable, and it must not be read as one.
//
// WHAT *IS* MEASURED is the cost of being this wide: over 662 873 free-text values in
// the development database (employee, location, department and tenant names) the widened
// sets escape EXACTLY the same 74 values the narrower ones did — delta zero — and a
// hand-built control of 30 names across Latin, Maltese, Turkish, CJK, Cyrillic, Greek,
// Hebrew, Arabic, Devanagari and Thai scripts escapes none. Widening a SKIP set cannot
// produce a false escape by construction; widening the TRIGGER set can, and this is the
// measurement that says it did not.

// reportDoc is the document being built, and it exists so there is exactly ONE way a
// cell reaches the file.
//
// 🔴 THE ESCAPING LIVES IN row() RATHER THAN AT THE CALL SITES. A file where twenty
// call sites each remember to escape is a file where the twenty-first does not, and
// the twenty-first is the one that carries an employee's name. Here a column added
// later is escaped because there is no other Write.
//
// The error is kept rather than returned per row: a csv.Writer over a bytes.Buffer
// cannot fail in practice, and threading an error through every row would bury the
// document's shape in error handling. It is checked once, at the end, and never
// swallowed (§7).
type reportDoc struct {
	buf bytes.Buffer
	w   *csv.Writer
	err error
}

func newReportDoc() *reportDoc {
	d := &reportDoc{}
	d.buf.WriteString(reportCSVBOM)
	d.w = csv.NewWriter(&d.buf)
	// CRLF is RFC 4180's line ending and what every spreadsheet expects; Go's and
	// Python's readers take it either way.
	d.w.UseCRLF = true
	return d
}

func (d *reportDoc) row(cells ...string) {
	safe := make([]string, len(cells))
	for i, c := range cells {
		safe[i] = spreadsheetSafe(c)
	}
	if err := d.w.Write(safe); err != nil && d.err == nil {
		d.err = err
	}
}

// blank separates two blocks. It is what lets one file hold six differently shaped
// tables and still be read by a spreadsheet as one sheet.
func (d *reportDoc) blank() { d.row() }

func (d *reportDoc) bytes() ([]byte, error) {
	d.w.Flush()
	if err := d.w.Error(); err != nil {
		return nil, err
	}
	if d.err != nil {
		return nil, d.err
	}
	return d.buf.Bytes(), nil
}

// isoLocal and isoUTC are the two forms every instant in this file appears in.
//
// 🔴 BOTH, ALWAYS, WITH THE COLUMN SAYING WHICH — the M6-07 card's own criterion and
// phase A's sixth handover. The screen prints local only and names the zone once,
// which is right for somebody reading a page about their own business; a file is read
// by a payroll process that may be in another country, and a bare wall clock with no
// offset is the bug that shows up twice a year.
//
// ISO 8601 IS ALSO WHAT KEEPS A SPREADSHEET FROM MANGLING THE DATE (the card says so
// in those words): a locale that reads 03/08/2026 as March is a whole month of
// somebody's hours in the wrong place.
func isoLocal(t time.Time, zone *time.Location) string {
	return t.In(zone).Format(time.RFC3339)
}

func isoUTC(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// wholeMinutes is the arithmetic column beside every hoursMinutes one.
//
// 🔴 INTEGER DIVISION ON time.Duration, NO FLOAT (§6). d.Minutes() returns a float64
// and is deliberately not used: this is the number an accountant SUMS, and a week of
// odd minutes accumulated through float hours is off in a way nobody can see until the
// column does not add up.
//
// IT TRUNCATES, exactly as hoursMinutes does, so the two columns of the same cell
// never disagree — 7h29m59s is "7h 29m" and 449.
func wholeMinutes(d time.Duration) int { return int(d / time.Minute) }

func zoneOf(s ledger.ReportScreen) *time.Location {
	if s.Zone == nil {
		return time.UTC
	}
	return s.Zone
}

// reportCSV renders one week as a file.
//
// THE DOCUMENT IS SIX BLOCKS separated by blank lines: what this file is, what is NOT
// in the hours, the totals, then one row per person, per venue and per unclosed
// check-in. A spreadsheet opens it as one sheet; encoding/csv and python's csv read it
// with a ragged record length, which both do.
//
// 🔴 THE THREE ROW BLOCKS CARRY EVERY ROW, WITH NO CAP (phase A's eighth handover).
// internal/handler's maxReportRows and maxOpenRows bound the SCREEN's listings and are
// not consulted here — the reason the export exists at all is the business whose
// payroll is larger than a page, and a capped export would be the exact defect the
// screen's cap is justified by pointing AT the export.
// TestReportCSV_CarriesEveryRowPastTheScreensCaps pins it.
//
// WHAT IT COSTS IN MEMORY, AND WHY IT IS BUFFERED. The Report is already entirely in
// memory before this runs (ledger.Hours reads its rows, pairs them and returns
// slices), so buffering the rendered file adds ONE more copy of a quantity that is
// already bounded by ledger.ReportEventCap. What that copy buys is Content-Length: a
// download that is cut short is then a broken download rather than a short payroll
// file that looks complete.
//
// MEASURED on 2026-08-10 by rendering this function directly, AND THE FIXTURE IS NAMED
// BECAUSE THE FIGURES DEPEND ON IT. A row's width is dominated by the employee NAME,
// so a byte total without the name length beside it is unreproducible: a reviewer
// re-ran the ceiling row with a ten-character name and got 1 102 537 B against the
// 1 252 537 B below — a difference of exactly the name-length delta, and a fair
// correction. So the per-row cost is stated as a FORMULA and the table names its
// fixture.
//
//	fixture: names of the form "Employee Number %06d" — 22 bytes, not the 21 an
//	         earlier version of this line said. The figure matters because it is what
//	         makes the table reconcile: 103 + 22 = 125, the per-row column below;
//	         103 + 21 = 124 does not. A uuid per row, one non-zero daily cell,
//	         ledger.ReportDays day columns.
//
//	person rows   bytes         per row
//	          1     2 646        (the fixed blocks dominate a tiny week)
//	        100    15 029          150
//	     10 000 1 252 537          125
//
//	open rows     bytes         per row
//	        100    10 773          107
//	     10 000   822 577           82
//
// A PERSON ROW IS THEREFORE 103 bytes of fixed structure (uuid, the two duration
// columns, the day columns, the qualifier counts) PLUS the name. TestReportCSVSize_
// IsAboutTheNameLengthPlusAFixedRow measures that decomposition rather than restating
// this table, so the claim is re-derived on every run instead of ageing.
//
// 🔴 THE CEILING IS NOT A GUESS: it is ledger.ReportEventCap / 2, because a week costs
// two records per shift, so no report can hold more people-with-hours than half the
// cap. That worst case renders in tens of milliseconds (31–36 ms measured) and is a
// file of about a megabyte and a quarter — which is why buffering it is affordable and
// why the cap upstream is the thing that actually bounds this route.
//
// ⚠️ AND WHAT THE DEVELOPMENT DATABASE'S OWN BUSIEST WEEK COSTS IS A FORMULA HERE,
// NOT A FIGURE — because the first two attempts at this paragraph both carried drifting
// figures, and the SECOND one was the paragraph warning about drifting figures. It said
// "754 person rows", which was 814 within days; a three-digit magnitude slips under this
// repository's own drifting-quantity tripwire, so nothing catches it going stale. Its
// replacement then quoted a byte size and a request time, which are linear in that same
// moving number, and a reviewer measured the week as having drifted past both.
//
// SO THE DURABLE FORM IS THE ONE THE TABLE ABOVE ALREADY GIVES:
//
//	file bytes ≈ people × (103 + average name length) + open entries × 82 + a preamble
//
// Everything a reader needs in order to size a real week is on the left of that, and
// both inputs are properties of the BUSINESS rather than of this repository's test
// residue. The one claim worth keeping is a RATIO rather than a magnitude, and it is
// what the export exists for: on that week the screen listed maxReportRows people and
// the file carried every one of them, at a per-request cost that did not separate from
// the screen's when the two were timed together, three runs each.
func reportCSV(s ledger.ReportScreen, now time.Time) ([]byte, error) {
	d := newReportDoc()
	zone := zoneOf(s)

	reportCSVPreamble(d, s, zone, now)
	d.blank()
	reportCSVExclusions(d, s)
	d.blank()
	reportCSVTotals(d, s)
	d.blank()
	reportCSVPeople(d, s, zone)
	d.blank()
	reportCSVVenues(d, s)
	d.blank()
	reportCSVOpen(d, s, zone)

	return d.bytes()
}

// reportCSVPreamble says what the file is, which week it covers and in which clock.
//
// 🔴 ITS FIRST CELL IS A TITLE RATHER THAN A COLUMN HEADING, and that is load-bearing
// rather than decorative: it is the cell the byte order mark lands on. Every heading
// row further down — the ones a machine keys on — begins a block of its own after a
// blank line and is untouched. See reportCSVBOM.
func reportCSVPreamble(d *reportDoc, s ledger.ReportScreen, zone *time.Location, now time.Time) {
	p := s.Period
	days := p.DayLabels(zone)
	last := days[len(days)-1]

	d.row("Tappa — hours worked")
	d.row("Business", s.TenantName)
	d.row("Week (local days)", p.FirstDay.String()+" to "+last.Format("2006-01-02"))
	d.row("Timezone", zone.String())
	// The period's own boundaries in UTC, because "which week is this" is the question
	// a payroll process asks and a local date alone does not answer it across a border.
	// The end is EXCLUSIVE and the heading says so; a reader who assumes otherwise
	// double-counts one instant.
	d.row("Period starts (UTC)", isoUTC(p.From))
	d.row("Period ends (UTC, exclusive)", isoUTC(p.To))
	d.row("Exported at (local)", isoLocal(now, zone))
	d.row("Exported at (UTC)", isoUTC(now))
	d.row("Times", "Every instant appears twice: local in the timezone above, and UTC. Both are ISO 8601.")
	d.row("Durations", "Two columns each: h/m to read, whole minutes to add up. Seconds are dropped, never rounded up.")
	d.row("Attribution", "A shift counts on the day it STARTED, so an overnight shift belongs to the evening it began.")
}

// reportCSVExclusions is §4.6 in a file: every quantity the hours below leave out.
//
// 🔴 IT COMES BEFORE THE FIGURES, NOT AFTER THEM. A note under a table is a note
// nobody scrolls to, and this block is the difference between a total and a floor.
// The screen makes the same promise in prose; a payroll process reads rows.
func reportCSVExclusions(d *reportDoc, s ledger.ReportScreen) {
	d.row("What is NOT in the hours below")
	d.row("what", "value", "minutes", "note")

	if s.Truncated {
		// 🔴 THE ONE LINE THIS FILE MUST NEVER SOFTEN, and it is FIRST in the block.
		// A truncated read makes every figure below a floor; a floor pasted into a
		// spreadsheet and paid is a payroll that comes up short with nothing to show
		// for it. The cap is interpolated from the constant rather than written out,
		// so the sentence cannot drift from the code that enforces it.
		d.row("Read complete", "NO", "",
			"This week holds more records than one report reads at once (limit "+
				strconv.Itoa(ledger.ReportEventCap)+" records). EVERY FIGURE IN THIS FILE IS A "+
				"FLOOR AND NOT A TOTAL. Nothing has been lost — every record is still in the "+
				"Transactions section.")
	} else {
		// A MEASURED "yes" RATHER THAN SILENCE. The absence of a warning and the
		// absence of a check look identical in a file.
		d.row("Read complete", "yes", "", "The whole week was read.")
	}

	d.row("Hours awaiting a decision", hoursMinutes(s.Totals.Awaiting), strconv.Itoa(wholeMinutes(s.Totals.Awaiting)),
		"Flagged records nobody has decided yet. Not in worked hours; they enter it if a manager approves them.")
	d.row("Hours a manager refused", hoursMinutes(s.Totals.Refused), strconv.Itoa(wholeMinutes(s.Totals.Refused)),
		"Flagged records a manager rejected. Not in worked hours, and shown so the total can be checked against the records.")
	d.row("Check-ins with no checkout", strconv.Itoa(s.Totals.Open), "",
		"Tappa does not invent a checkout. These are worth NO hours here — not zero hours — and a manager enters the missing record. Listed in full below.")
	// 🔴 THE SCREEN PRINTS THIS TALLY AND THE FILE DID NOT, WHICH IS AN ASYMMETRY
	// RATHER THAN A SAVING. pages.needsActionLine tells a manager how many of the whole
	// backlog have been open too long to be somebody still on shift; the open block
	// below carries the same fact per row, so a reader COULD derive it — but the whole
	// point of the block above is that a payroll process does not derive, it reads. It
	// is counted from the same OpenEntry values the rows are built from, so the tally
	// and the list cannot disagree.
	d.row("Open too long to be somebody on shift", strconv.Itoa(needsActionTotal(s)), "",
		"Open check-ins older than the engine's own forgotten-checkout threshold. A row cannot say WHY it is open — somebody still on shift and somebody who forgot look the same — so this measures how long only.")
	d.row("Closed too late to be a shift", strconv.Itoa(s.Totals.TooLong), "",
		"Check-ins closed so much later that the engine calls them probable missed checkouts. Reported as open rather than paid as very long shifts.")
	d.row("Training taps", strconv.Itoa(s.PracticeTaps), "",
		"Practice taps from the activation tour. In no figure in this file, and never in worked hours.")
	d.row("Checkouts that began earlier", strconv.Itoa(s.StartedEarlier), "",
		"Their shift started before this week, so their hours are in the previous report.")
	d.row("Records naming no employee", strconv.Itoa(s.Unattributable), "",
		"Direction-carrying records with nobody on them, so no hours could be attributed. Still in the Transactions section.")

	if len(s.People) == 0 {
		// The file's twin of the screen's empty state, and it says the same careful
		// thing: nothing PAIRED into a shift, which is not the same claim as "nobody
		// worked" (§5 row 3 writes no record at all for somebody with no session).
		d.row("Nothing in this week pairs into a worked shift.")
	}
}

// reportCSVTotals is the whole tenant's week, from ledger.Totals — the same value the
// screen's headline docket prints, not a sum of the rows below.
func reportCSVTotals(d *reportDoc, s ledger.ReportScreen) {
	d.row("Totals")
	d.row("measure", "value", "minutes")
	d.row("Hours worked", hoursMinutes(s.Totals.Worked), strconv.Itoa(wholeMinutes(s.Totals.Worked)))
	d.row("Shifts", strconv.Itoa(s.Totals.Shifts), "")
	d.row("People with hours", strconv.Itoa(len(s.People)), "")
	d.row("Venues with hours", strconv.Itoa(len(s.Venues)), "")
	d.row("Hours awaiting a decision", hoursMinutes(s.Totals.Awaiting), strconv.Itoa(wholeMinutes(s.Totals.Awaiting)))
	d.row("Hours a manager refused", hoursMinutes(s.Totals.Refused), strconv.Itoa(wholeMinutes(s.Totals.Refused)))
	d.row("Check-ins with no checkout", strconv.Itoa(s.Totals.Open), "")
}

// reportCSVPeople is one row per person, with the week split over its local days.
//
// THE DAILY COLUMNS ARE MINUTES ONLY, which is a departure from the two-column rule
// above and is deliberate: seven days times two columns is fourteen, and the daily
// split exists to be CHECKED and summed rather than read aloud. The weekly figure
// beside them carries both forms, and the heading names the date and the weekday so a
// column cannot be misread.
//
// employee_id IS PRESENT AND IT IS NOT A DISCLOSURE THIS TASK INVENTED. A payroll file
// keyed on names alone cannot tell two people called Maria Borg apart — which is the
// exact case ledger's own People sort documents ("a name is not unique") — so a name
// key would silently merge two employees' weeks in whatever the accountant pastes it
// into. The id is a tenant-internal opaque uuid, it is not in §4.7's list, it is
// already carried by ledger.PersonHours, and this repository already treats it as LESS
// disclosing than a name: M6-05 replaced an employee NAME in the roster's paging link
// with exactly this id, on purpose.
func reportCSVPeople(d *reportDoc, s ledger.ReportScreen, zone *time.Location) {
	d.row("Per person")

	head := []string{"employee_id", "employee", "worked", "worked_minutes", "shifts"}
	for _, day := range s.Period.DayLabels(zone) {
		head = append(head, day.Format("2006-01-02")+" ("+day.Format("Mon")+") minutes")
	}
	head = append(head,
		"awaiting", "awaiting_minutes", "refused", "refused_minutes",
		"late_arrivals", "late_by", "late_by_minutes",
		"arrivals_not_measured", "manager_entered_shifts", "open_check_ins")
	d.row(head...)

	for _, p := range s.People {
		row := []string{
			p.EmployeeID.String(),
			// The same word the screen prints, for the same reason: a blank cell reads
			// as "nobody", which is a different claim from "the roster no longer names
			// this record's employee".
			personName(p.Name),
			hoursMinutes(p.Worked),
			strconv.Itoa(wholeMinutes(p.Worked)),
			strconv.Itoa(p.Shifts),
		}
		for i := range s.Period.DayLabels(zone) {
			var day time.Duration
			if i < len(p.Daily) {
				day = p.Daily[i]
			}
			row = append(row, strconv.Itoa(wholeMinutes(day)))
		}
		row = append(row,
			hoursMinutes(p.Awaiting), strconv.Itoa(wholeMinutes(p.Awaiting)),
			hoursMinutes(p.Refused), strconv.Itoa(wholeMinutes(p.Refused)),
			strconv.Itoa(p.LateShifts), hoursMinutes(p.LateBy), strconv.Itoa(wholeMinutes(p.LateBy)),
			strconv.Itoa(p.Unmeasured), strconv.Itoa(p.Manual), strconv.Itoa(p.Open))
		d.row(row...)
	}
}

// reportCSVVenues is the same week split by where each shift STARTED. Somebody who
// taps in at one venue and out at another worked the shift they began, and §5 treats
// moving down the chain as ordinary — so the heading says which end it keys on.
func reportCSVVenues(d *reportDoc, s ledger.ReportScreen) {
	d.row("Per venue (where the shift STARTED)")
	d.row("venue", "worked", "worked_minutes", "shifts")
	for _, v := range s.Venues {
		d.row(venueName(v.Name), hoursMinutes(v.Worked), strconv.Itoa(wholeMinutes(v.Worked)), strconv.Itoa(v.Shifts))
	}
}

// reportCSVOpen is Q18's list, in full.
//
// 🔴 IT CARRIES NO END AND NO DURATION, which is the whole decision rather than a
// missing column: the system produces no checkout, so an "out" or an "hours" column
// here would be the product inventing a record. needs_action says how LONG it has been
// open and never WHY — a forgotten checkout and a pre-ADR-0008 masked check-in are the
// same shape on disk, and a file must not claim to tell them apart any more than the
// screen may.
func reportCSVOpen(d *reportDoc, s ledger.ReportScreen, zone *time.Location) {
	d.row("Open check-ins — no checkout, no hours")
	d.row("employee", "venue", "opened_local", "opened_utc", "needs_action", "manager_entered")
	for _, o := range s.Open {
		d.row(personName(o.EmployeeName), venueName(o.LocationName),
			isoLocal(o.At, zone), isoUTC(o.At), yesNo(o.Stale), yesNo(o.Manual))
	}
}

// yesNo writes a boolean as a word rather than as TRUE/FALSE, because a spreadsheet
// coerces the second into a native boolean and then a locale renders it in another
// language.
// needsActionTotal counts the open entries that have been open too long to be somebody
// still on shift.
//
// 🔴 IT IS THE ONE CALCULATION POINT FOR THIS QUANTITY, AND IT DID NOT START THAT WAY.
// This function and fillReportsView's open-entry loop accumulated the identical number
// over the identical slice — a second representation, in a file whose own header calls
// that "the failure class this repository has paid for repeatedly". A review measured
// what it cost: inverting the condition here left
// TestReportCSV_AgreesWithTheScreenItWasExportedFrom GREEN, because no test compared
// this quantity across the two surfaces. reports.go now calls this, so the screen's
// "N of these have been open too long" and the file's tally are one arithmetic.
//
// IT COUNTS THE WHOLE BACKLOG AND NOT A PAGE. Stale is ledger's own field — this adds
// no threshold of its own, which would be a second representation of
// internal/domain/tap's StaleOpenIn.
func needsActionTotal(s ledger.ReportScreen) int {
	n := 0
	for _, o := range s.Open {
		if o.Stale {
			n++
		}
	}
	return n
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
