package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/httpx"
)

// The BILLING SECTION's CSV EXPORT — M6-12 phase B.
//
// 🔴 IT SHARES reportscsv.go's WRITER RATHER THAN GROWING A SECOND ONE, AND THAT IS
// THE FIRST DECISION IN THIS FILE. This repository has paid seven times for copying
// HALF of a pattern, and the CSV escaping boundary is the worst candidate for an
// eighth: it is not one rule but four, three of them found by successive audits —
// startsAFormula's eighteen triggers, hidesTheStartOfACell's five named classes
// (IsSpace, Cc, Cf, Other_Default_Ignorable, every mark) plus two named runes, the
// apostrophe escape, and the BOM placement decision. A second writer would inherit
// whichever of those the author happened to remember. So there is NO new escaping
// code below: reportDoc, spreadsheetSafe and reportCSVBOM are used as they are, in
// the same package, and TestBillingCSV_UsesTheSameEscapingBoundaryAsTheHoursExport
// measures that a formula, a zero-width prefix and a Hangul filler are neutralised
// here exactly as they are there.
//
// ⚠️ WHAT IS *NOT* SHARED, AND WHY IT IS A DEPARTURE RATHER THAN A COPY: the
// FILENAME pattern. reportCSVFilename validates an ISO DAY (`2026-08-03`); a billing
// document is keyed on a MONTH (`2026-08`), so a second regexp is unavoidable. It is
// written with the same construction and the same fallback — anything that is not the
// exact shape yields a constant name — because the property being defended is
// identical: Content-Disposition's value is quoted, a tenant name is free text a
// customer types, and the answer is that the filename is built from ONE server-derived
// value whose shape is checkable rather than believed. reportCSVName's own comment
// carries the argument in full.
//
// 🔴 §4.7 — THIS FILE ADDS NO COLUMN TO ANY QUERY, and the wall is the domain's:
// db/queries/billing.sql touches `employees` as a COUNT and never as a row, and
// billing.Draft has no field a person could travel in. The export is where "enrich it
// a little" is most tempting, because a file feels private in a way a screen does not
// — it is the opposite. A screen is looked at once by somebody signed in; a file is
// emailed to an accountant, kept on a laptop and forwarded. An invoice for this
// product names ONE party: the business being billed.
//
// 🔴 AND IT OFFERS NO ITEMISATION, WHICH IS THE ONE THING A SPREADSHEET READER WILL
// LOOK FOR. A frozen period stores the NUMBER of people and not which people
// (migration 0016 states why storing the ids was rejected rather than forgotten), so
// the file says so in its own preamble instead of leaving a reader to conclude the
// list was merely left out.

// billingCSVName is the shape a billing filename may have: an ISO year-month, and
// nothing else. See the file header for why this is a second pattern rather than a
// reuse.
var billingCSVName = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}$`)

// ActionBillingExported is the audit vocabulary entry for a billing export.
//
// THE NAME FOLLOWS ActionReportExported's PATTERN — `<subject>.<past participle>` for
// something that happened — read from reportscsv.go rather than invented. The action
// name is free text by schema decision (00005), so the constant IS the vocabulary.
const ActionBillingExported = "billing.exported"

// billingExportDetail is the audit row's typed payload.
//
// 🔴 IT IS A PURPOSE-BUILT STRUCT AND EVERY FIELD IS A COUNT, A MONTH OR A FLAG
// (internal/audit's own rule: the package cannot scrub, so the caller must not hand it
// anything to scrub). reportExportDetail's shape, with this document's own quantities.
//
// ⚠️ THE AMOUNT IS CARRIED AS AN EXACT DECIMAL STRING, NOT AS A JSON NUMBER, and that
// is billing.Close's rule applied to the second place money reaches audit_log:
// audit_log.detail is jsonb, and a JSON number is a float64 in every reader that will
// ever parse it — storing 2019.00 as one would be the single place this section let a
// float touch money (§6).
type billingExportRow struct {
	Month         string `json:"month"`
	Zone          string `json:"zone"`
	FromUTC       string `json:"from_utc"`
	ToUTC         string `json:"to_utc"`
	Frozen        bool   `json:"frozen"`
	EmployeeCount int    `json:"employee_count"`
	// Unstamped travels because an export of a FLOOR is a different event from an
	// export of a total — reportExportDetail carries Truncated for the same reason.
	Unstamped int    `json:"unstamped_employees"`
	AmountDue string `json:"amount_due"`
	Currency  string `json:"currency"`
	Free      bool   `json:"free_period"`
	// HistoryRows and HistoryTruncated describe the second table in the document.
	HistoryRows      int  `json:"history_rows"`
	HistoryTruncated bool `json:"history_truncated"`
	Bytes            int  `json:"bytes"`
}

func billingExportDetail(d billing.Draft, history []billing.Draft, capped bool, size int) billingExportRow {
	return billingExportRow{
		Month:            d.Month.String(),
		Zone:             d.Zone,
		FromUTC:          isoUTC(d.From),
		ToUTC:            isoUTC(d.To),
		Frozen:           d.Frozen,
		EmployeeCount:    d.EmployeeCount,
		Unstamped:        d.UnstampedEmployees,
		AmountDue:        d.AmountDue.String(),
		Currency:         d.AmountDue.Currency(),
		Free:             d.Free,
		HistoryRows:      len(history),
		HistoryTruncated: capped,
		Bytes:            size,
	}
}

func billingCSVFilename(month string) string {
	if !billingCSVName.MatchString(month) {
		return "tappa-billing.csv"
	}
	return "tappa-billing-" + month + ".csv"
}

// billingExport answers one month as a file.
//
// 🔴 IT IS A GET WITH ONE SIDE EFFECT: AN audit_log ROW, exactly as reportsExport has.
// A security audit measured the asymmetry — `audit rows: before=743,
// after_billing_csv=743, after_reports_csv=744` — and the earlier version of this
// comment argued the difference was deliberate. That argument is withdrawn.
//
// ⚠️ WHAT IT GOT RIGHT AND WHY IT STILL LOSES. The confidentiality half was sound:
// this file names nobody (measured — 2 213 employee names scanned against the rendered
// document, zero matches), and M6-07's own justification leans on something absent
// here ("the hundred-and-first person's row is reachable HERE and nowhere else in this
// section" — a billing document carries no row the screen does not). The deciding
// argument is not confidentiality, it is CONSISTENCY: two sibling CSV routes in one
// repository answering "does a bulk export leave a trail?" differently is how the
// precedent sentence gets bent by the section after next. And this document is emailed
// to an accountant and carries the business's commercial position — "who downloaded
// what" is not worth LESS here than in M6-07.
//
// The alternative — narrowing M6-07's justification so it stops covering this route —
// would mean editing shipped code to fit a comment. More expensive and more brittle.
//
// 🔴 THE TRAIL IS WRITTEN BEFORE THE BYTES GO OUT, AND IN ITS OWN TRANSACTION, for
// reportsExport's reason: there is no surrounding transaction to share a fate with
// (both reads open and close their own), and threading one out of a READER so a handler
// could write inside it would make the read path a write path for a row that is not
// conditional on the read.
//
// 🔴 AND A FAILED AUDIT WRITE DOES NOT REFUSE THE DOWNLOAD. Nothing is LOST when it
// fails: §4.6 is about the attendance record, and this is an access trail beside it.
// a.record logs the failure loudly (§7: never swallowed). The figures in this file are
// the same ones the section renders, which writes no trail row either, so refusing the
// download on a database blip would withhold nothing from anybody determined — it would
// only cost an owner their invoice.
//
// ✅ THE READ-PATH PROPERTY IS UNTOUCHED BY THIS, and it is worth saying which property
// that is: TestBillingRead_WritesNothing pins that no GET reaches billing.Book.Close,
// i.e. that reading can never FREEZE a period. An access-trail row in audit_log is a
// different table and a different claim; TestBillingDB_ReadingTheScreenFreezesNothing
// counts billing_periods for the same reason.
//
// 🔴 §4.6 — A FAILED READ IS AN ERROR PAGE, NEVER AN EMPTY FILE, and it is sharper
// here than on the hours export. A CSV of zero hours at least looks like a quiet week;
// a billing document of zero is a PLAUSIBLE and correct answer — a founding month
// really does cost nothing — so a failure that fell through would be indistinguishable
// from the truth and would be filed as an invoice.
func (a *AdminAuth) billingExport(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	// OWNER-ONLY, LIKE THE SECTION — and the gate runs before any read, so a refused
	// request costs no query. mayManageBilling carries the argument.
	if !mayManageBilling(id) {
		a.refuseBillingRead(id, billingCSVHref)
		a.renderProblem(w, r, http.StatusForbidden, problemBillingOwnerOnly)
		return
	}

	d, err := a.billingMonth(r.Context(), id.TenantID(), parseBillingMonth(r))
	if err != nil {
		a.log.Error("panel: could not read the billing period for export", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	history, capped, err := a.books.History(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not read the frozen billing history for export", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	doc, err := billingCSV(d, history, capped, time.Now())
	if err != nil {
		a.log.Error("panel: could not render the billing export", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	a.record(r.Context(), audit.Event{
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionBillingExported,
		Target:   d.Month.String(),
		Detail:   billingExportDetail(d, history, capped, len(doc)),
	})

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	// nosniff MATTERS MORE ON A FILE THAN ON THE PANEL'S HTML: this response is a
	// document a browser may be talked into rendering. reportsExport's reasoning.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="`+billingCSVFilename(d.Month.String())+`"`)
	// no-store BECAUSE THIS IS A COMMERCIAL DOCUMENT. A cached copy in an intermediary
	// or in a browser's back/forward store is one shared machine away from being
	// somebody else's.
	w.Header().Set("Cache-Control", "no-store")
	// Content-Length is why the document is BUFFERED rather than streamed: with it, a
	// download cut short by a dropped connection is a BROKEN download the browser
	// reports, instead of a silently truncated invoice with a 200 on it.
	w.Header().Set("Content-Length", strconv.Itoa(len(doc)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(doc); err != nil {
		a.log.Error("panel: writing the billing export failed", "err", err)
	}
}

// billingCSV renders one month as a file.
//
// FOUR BLOCKS separated by blank lines: what this document is, what it does NOT say,
// the figures, and the frozen history. A spreadsheet opens it as one sheet;
// encoding/csv and python's csv read it with a ragged record length, which both do.
//
// 🔴 IT ADDS NO ARITHMETIC. Every figure is a field of the billing.Draft the section's
// own reader produced, formatted by the same functions the screen uses. There is no
// second multiplication anywhere: unit_price × employee_count happens in Postgres
// (migration 0016's tappa_billing_amount_due, which the frozen column is generated
// from and the live preview calls), so one multiplication exists in the whole system
// rather than two that must agree.
//
// 🔴 EVERY AMOUNT IS billing.Money.String — SYMBOL-FREE AND GROUPING-FREE — WITH THE
// CURRENCY IN ITS OWN COLUMN. The screen's money() adds a symbol because a person
// reads it; a file is read by a payroll or accounting process, and a symbol glued to
// the digits is one more thing that has to be stripped before the value can be parsed.
// Grouping separators would be worse: they are locale-dependent and would have to be
// undone. What this guarantees is that the digits are the exact ones Postgres sent.
func billingCSV(d billing.Draft, history []billing.Draft, capped bool, now time.Time) ([]byte, error) {
	doc := newReportDoc()

	billingCSVPreamble(doc, d, now)
	doc.blank()
	billingCSVCaveats(doc, d, capped)
	doc.blank()
	billingCSVFigures(doc, d)
	doc.blank()
	billingCSVHistory(doc, history)

	return doc.bytes()
}

// billingCSVPreamble says what the file is, which month it covers, in which clock, and
// — first — WHETHER IT IS FINAL.
//
// 🔴 ITS FIRST CELL IS A TITLE RATHER THAN A COLUMN HEADING, and that is load-bearing
// rather than decorative: it is the cell the byte order mark lands on. Every heading
// row further down — the ones a machine keys on — begins a block of its own after a
// blank line and is untouched. reportCSVBOM carries the measurement and the reason the
// mark is emitted at all (Maltese ħ ġ ċ ż and Turkish ı ş ğ ç in a business name).
func billingCSVPreamble(doc *reportDoc, d billing.Draft, now time.Time) {
	doc.row("Tappa — monthly billing")
	// 🔴 THE FIRST FACT IN THE FILE IS WHICH OF TWO DOCUMENTS THIS IS. A frozen figure
	// and a live draft carry the same five numbers, and in a spreadsheet they are
	// indistinguishable once the header has scrolled away — which is exactly how a
	// provisional figure gets invoiced. Both the WORD and a whole sentence, because a
	// payroll process reads a cell and a person reads a line.
	if d.Frozen {
		doc.row("Status", "FROZEN",
			"These figures were stored when this month was closed and are never recomputed. "+
				"A person removed since, or a price changed since, cannot move this amount.")
		doc.row("Frozen at (UTC)", isoUTC(d.ClosedAt))
	} else {
		// 🔴 THE SENTENCE IS THE SCREEN'S, NOT A SECOND COPY OF IT. draftStateLine is one
		// function with one measurement behind it (it distinguishes a month that is still
		// RUNNING from one that has ENDED, because hiring somebody cannot enter a period
		// that has already closed), and a file that restated the claim in its own words is
		// exactly where the two would drift. Only the "NOT A BILL" prefix is added, because
		// a spreadsheet reader has no stamp to look at.
		doc.row("Status", "DRAFT", "NOT A BILL. "+draftStateLine(d)+
			" Only a month that has been frozen in the panel is final.")
	}
	doc.row("Business", d.TenantName)
	doc.row("Month", d.Month.String())
	doc.row("Plan", d.Plan)
	doc.row("Timezone", d.Zone)
	// The period's own boundaries in UTC, because "which month is this" is the question
	// an accounting process asks and a local month name alone does not answer it across
	// a border. The end is EXCLUSIVE and the heading says so; a reader who assumes
	// otherwise double-counts one instant.
	doc.row("Period starts (UTC)", isoUTC(d.From))
	doc.row("Period ends (UTC, exclusive)", isoUTC(d.To))
	doc.row("Exported at (UTC)", isoUTC(now))
	doc.row("Counted",
		"Everybody whose employment overlapped this period — anybody on the books for even "+
			"one day of it — provided their status agrees with their own joining and leaving dates.")
	doc.row("Amounts",
		"Plain decimal, no symbol and no thousands separator. The currency is its own column.")
}

// billingCSVCaveats is §4.6 in a file: everything this document does NOT say.
//
// 🔴 IT COMES BEFORE THE FIGURES, NOT AFTER THEM (reportCSVExclusions' rule). A note
// under a table is a note nobody scrolls to, and two of these lines are the difference
// between a total and a floor.
func billingCSVCaveats(doc *reportDoc, d billing.Draft, capped bool) {
	doc.row("What this document does not say")
	doc.row("what", "value", "note")

	if d.UnstampedEmployees > 0 {
		// 🔴 THE LINE THIS FILE MUST NEVER SOFTEN, AND IT IS FIRST IN THE BLOCK. A
		// non-zero honesty column makes the headcount below a FLOOR, and a floor pasted
		// into a spreadsheet and paid is a business under-billed with nothing to show for
		// it. The second sentence bounds the admission in the other direction, because
		// the column is TENANT-WIDE and POINT-IN-TIME: it counts every disagreeing record
		// this business has, including people who left long before this period and were
		// never candidates for it. Migration 0016 states why it cannot be narrowed.
		doc.row("Headcount complete", "NO", strconv.Itoa(d.UnstampedEmployees)+
			" employee records in this business have a status that disagrees with their own "+
			"joining and leaving dates, so they cannot be counted for any period. THE HEADCOUNT "+
			"BELOW IS A FLOOR AND NOT A TOTAL. That figure is an UPPER BOUND on the effect and "+
			"NOT the number missing from this month: it is tenant-wide and includes people who "+
			"left long before this period.")
	} else {
		// A MEASURED "yes" RATHER THAN SILENCE. The absence of a warning and the absence
		// of a check look identical in a file.
		doc.row("Headcount complete", "yes",
			"Every employee record in this business agrees with its own dates.")
	}

	// 🔴 THE PROMISE THIS DATA CANNOT KEEP, WRITTEN DOWN BEFORE ANYBODY ASKS FOR IT. A
	// spreadsheet reader's first instinct on seeing a headcount is to look for the list
	// behind it. There is none, and there can never be one for an old period: a frozen
	// row stores the NUMBER and not the identities, and anybody anonymised under GDPR
	// since would no longer resolve even if it did. Saying so is the difference between
	// a documented limit and an apparent omission.
	doc.row("Named people", "none",
		"This document names no employee, by design. What is billed is a COUNT; nothing "+
			"stored can turn a frozen count back into a list of people, so no itemisation of a "+
			"past month exists or can be produced.")

	if capped {
		doc.row("History complete", "NO",
			"This business has more frozen months than one read carries (limit "+
				strconv.Itoa(billing.HistoryCap)+"). The history block below is shortened. "+
				"Nothing has been lost — an older month is still readable by asking for it by name.")
	} else {
		doc.row("History complete", "yes", "Every frozen month is in the history block below.")
	}

	if !d.Frozen {
		doc.row("This month is final", "NO",
			"The figures below are a draft and will change with the roster or the price.")
		if !d.HasEnded {
			doc.row("This month has ended", "NO",
				"The period is still running, so the headcount is still moving. A month can only "+
					"be frozen once it is over.")
		}
	}
}

// billingCSVFigures is the invoice itself: the figures, and the currency beside them.
//
// 🔴 EVERY ROW IS AS WIDE AS THE HEADING ROW, AND THAT WAS A REAL DEFECT BEFORE IT WAS
// A RULE. Two rows in this block carried a fourth cell against a three-column heading,
// so their explanatory note landed in a column the heading called `currency` — measured
// by dumping the rendered document and reading it. A ragged FILE is fine and deliberate
// (four differently shaped tables, one sheet); a ragged BLOCK is a mislabelled column,
// which is worse than no label. TestBillingCSV_EveryRowIsAsWideAsItsOwnHeading pins it
// over the whole document rather than over these two rows.
func billingCSVFigures(doc *reportDoc, d billing.Draft) {
	doc.row("This month")
	doc.row("measure", "value", "currency", "note")
	doc.row("People counted", strconv.Itoa(d.EmployeeCount), "", "")
	doc.row("Price per person per month", d.UnitPrice.String(), d.UnitPrice.Currency(), "")
	doc.row("Amount due", d.AmountDue.String(), d.AmountDue.Currency(), "")
	doc.row("Founding offer free period", yesNo(d.Free), "", "")
	if d.Free {
		doc.row("Why the amount is zero", "founding offer", "",
			"This month falls inside the founding offer's free window, so the amount is zero "+
				"because of the offer and not because nobody was employed.")
	}
	if !d.FirstChargeableMonth.Zero() {
		// ONLY A DRAFT CARRIES THE RULE. A frozen row records the DECISION (free or not)
		// rather than the rule that produced it, because the rule is allowed to change and
		// the decision is not — billing.Draft says so where the field is declared.
		doc.row("First month that is charged for", d.FirstChargeableMonth.String(), "",
			"The founding offer covers this business's first three billing months. From this "+
				"month onwards each one is charged.")
	}
}

// billingCSVHistory is every frozen month, newest first — the same list the screen
// shows and bounded by the same cap, which the block above declares.
func billingCSVHistory(doc *reportDoc, history []billing.Draft) {
	doc.row("Frozen months")
	doc.row("month", "people", "unit_price", "amount_due", "currency", "free_period",
		"headcount_is_a_floor", "frozen_at_utc")
	if len(history) == 0 {
		doc.row("No month has been frozen for this business yet.")
		return
	}
	for _, h := range history {
		doc.row(
			h.Month.String(),
			strconv.Itoa(h.EmployeeCount),
			h.UnitPrice.String(),
			h.AmountDue.String(),
			h.AmountDue.Currency(),
			yesNo(h.Free),
			// THE PER-ROW TWIN OF THE CAVEAT BLOCK'S ADMISSION. A frozen row keeps its own
			// honesty column, so a history line that stayed silent would present a floor as
			// a total — and this is the block somebody sums.
			yesNo(h.UnstampedEmployees > 0),
			isoUTC(h.ClosedAt),
		)
	}
}
