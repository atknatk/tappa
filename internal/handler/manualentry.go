package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// MANUAL RECORD ENTRY — M6-08. The panel's second write to `transactions`, and the
// product's second writer of that table altogether.
//
// 🔴 WHAT IT IS FOR, IN THE PRODUCT'S OWN WORDS. Q18 decided that the system produces
// NO checkout of its own: "a record always rests on a human's declaration", because an
// hour the software invented is an hour that stops being usable as evidence. The cost
// of that decision is this screen — somebody has to type the checkout nobody tapped,
// and the hours of the employee whose phone cannot open the tap page at all.
//
// 🔴 THE TENANT AND THE AUTHOR ARE NEVER INPUTS (§4.5, §4.7). Both come from
// httpx.AdminOf(r), which the Protect chain resolved from a signed session cookie
// against the database. What a client contributes is ONE employee id, a direction from
// a two-word vocabulary, a wall clock and a sentence. The venue and the department are
// NOT posted: db/queries/transactions.sql reads them off the employee row inside the
// INSERT, so the two composite foreign keys are satisfied by construction and there is
// no third id for a cross-tenant probe to aim at.
//
// 🔴 THE TWO WRITING ROUTES MOUNT ProtectWriting AND THE ORDER IS THE POINT.
// floodGate → sameOriginGate → requireAdmin → sessionGate: the Origin check runs
// BEFORE the resolver, so a cross-origin POST costs zero database work. A POST
// registered in mountSections would silently take the READ chain instead — the defect
// an audit measured on POST /admin/review, where 300 cross-origin posts spent a
// signed-in manager's whole session budget and charged 301 unbudgeted lookups on the
// way. Four stages, and the third is the one that must not be first.
//
// 🔴 THREE ROUTES FOR ONE ACT, AND THE MIDDLE ONE WRITES NOTHING. The manager fills a
// form, is shown exactly what will be written, and confirms. The middle step is a POST
// that RENDERS rather than redirects, which breaks this panel's own pattern for the
// second time — employeeInvite is the first — and the reason is the same shape: what
// travels between the steps must not go in a URL. Here it is a manager's free sentence
// about a named employee, which in a query string would reach the address bar, the
// history, any shared screen and any Referer a later navigation sends. It is safe to
// re-post because it writes nothing: a refresh re-renders the same warning and mints a
// fresh confirmation.
//
// 🔴 AND THE CONFIRMATION GATE IS HERE FOR A MEASURED REASON RATHER THAN BY COPYING
// THE DEACTIVATION FLOW. Copying half a pattern is this repository's most expensive
// habit; copying an unnecessary half is the same fault in the other direction, so the
// gate had to earn its place. It does, and the measurement is in
// manual.CorrectionsOnlyShorten: appending a correcting record makes an interval
// SHORTER in the report and can never make it longer, so a checkout typed too EARLY
// under-pays somebody with no one-row remedy — the shape docs/adr/0009 records for
// reviews, arriving here in monetary form. This gate is that ADR's third option, the
// one M6-04 was faulted for not taking: say it on the screen BEFORE the button.
//
// WHAT THE GATE BUYS AND DOES NOT BUY is deactivateconfirm.go's paragraph unchanged:
// it guarantees the warning was SERVED, for this person, this direction, this instant
// and this session, within ten minutes. It does not prove the warning was READ, it is
// not the panel's CSRF defence (ProtectWriting is), and it is not single-use on the
// server.
//
// 🔴 §4.7 — NOTHING ON THESE SCREENS SHOWS OR STORES A COORDINATE, AN ADDRESS OR A
// TOKEN. The write statement has no column for the first two, manual.Recorded has no
// field for any of them, and the confirmation value is a MAC over server-chosen fields
// that is deliberately rendered into the page (like the login form's synchronizer
// token) rather than being a secret.

// panelRecorder is the slice of internal/domain/manual this handler needs, declared
// HERE at the consumer (§7).
//
// IT IS ITS OWN FIELD ON AdminAuth for the rule M6-04 set and M6-06 twice restated: a
// different PACKAGE gets a different field, so a change to one section's domain cannot
// break another's wiring with no compile error at the call site.
type panelRecorder interface {
	Subject(ctx context.Context, tenantID, employeeID uuid.UUID) (manual.Subject, error)
	Bounds(at time.Time) error
	Backdated(at time.Time) bool
	Record(ctx context.Context, e manual.Entry) (manual.Recorded, error)
}

// The three URLs, built from the TRANSACTIONS section's own address rather than
// written out again — the rule employeesHref and reviewHref already follow. A record
// entered here is a row in the list that section renders, so if the section moves,
// the way to add to it moves with it.
var (
	// manualEntryHref is the form (GET) and the review step (POST). One address, two
	// methods: the review step is the same page having been shown its own answer, and
	// giving it a second URL would make "go back and change the time" a different
	// screen from the one the manager was already on.
	manualEntryHref = transactionsHref + "/records/new"
	// manualRecordHref is the write. It is a SEPARATE address from the two steps
	// above so that the one route in this file that appends to an immutable table is
	// the one route named after doing so.
	manualRecordHref = transactionsHref + "/records"
)

// maxManualEntryBody is the ceiling on this section's POST bodies.
//
// 🔴 A BARE ParseForm INHERITS net/http's 10 MB. These routes sit in front of a
// database write and a valid session may spend adminSessionLimit requests per window,
// so the unbounded shape is 300 × 10 MB of allocation per session per ten minutes from
// somebody who has merely signed in. The real body is a uuid, two short words, a date,
// a time, a MAC and at most maxManualNote characters. The constant and the pattern are
// review.go's maxReviewBody and employeeactions.go's maxEmployeeActionBody.
const maxManualEntryBody = 8 << 10

// maxManualNote bounds the manager's sentence.
//
// 🔴 IT IS maxReviewNote RATHER THAN A SECOND 500, and the reason is not tidiness: the
// argument that produced that number transfers WORD FOR WORD, because the note lands on
// the SAME docket component in the SAME ledger.PageSize list on a Cache-Control:
// no-store page. M6-04 measured that page growing 46.9% when all 25 records carried a
// note at the ceiling; a manual note is rendered through components.DocketView.Note on
// exactly those cards. Two constants holding one measured bound is the second
// representation this repository keeps paying for.
const maxManualNote = maxReviewNote

// The closed vocabularies this section's query string may echo. A reflected parameter
// is a value somebody can put anything into, and the answer is not to escape it more
// carefully but to have nothing to escape (oneOfWords, review.go).
var manualProblemWords = []string{
	"unreadable", "unknown", "direction", "when", "future", "too-old",
	"confirm-required", "confirm-stale", "unavailable",
}

// manualDirections is the direction control's options AND the validator's list, and it
// is the ONLY copy.
//
// 🔴 THE CONTROL AND THE VALIDATOR READ THE SAME SLICE, which is M6-03's rule applied
// to a write instead of a filter, and it matters more here: a direction the form offers
// and the server drops would not show an unfiltered page, it would refuse a record a
// manager believes they entered. The values are migration 00005's
// transactions_type_check vocabulary; the labels are the words §5 uses.
var manualDirections = []pages.ManualDirectionView{
	{Value: string(manual.In), Label: "Check-in", Sentence: "started work"},
	{Value: string(manual.Out), Label: "Checkout", Sentence: "stopped work"},
}

// manualEntryForm renders the empty form for one person.
//
// IT IS A GET AND IT CHANGES NOTHING. The roster links here; a crawler following that
// link mints nothing and writes nothing, which is why the confirmation is minted by
// the POST below rather than by this.
func (a *AdminAuth) manualEntryForm(w http.ResponseWriter, r *http.Request) {
	employeeID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("id")))
	if err != nil {
		// NO PERSON, NO FORM. There is no "pick somebody" state on this screen: the
		// roster is the place a manager chooses a person, and a picker here would be a
		// second roster with its own paging, its own filters and its own §4.7 surface.
		a.log.Warn("panel manual entry: the requested employee id is not a uuid")
		a.redirect(w, employeesHref+"?problem=unreadable")
		return
	}
	subject, ok := a.manualSubject(w, r, employeeID, problemPanelUnavailable)
	if !ok {
		return
	}

	v := a.manualView(r, subject, employeeID)
	// THE DEFAULTS ARE THE TENANT'S TODAY AND NOTHING ELSE. A pre-filled TIME would be
	// a guess about when somebody worked, and a guess a manager confirms without
	// reading is the failure this whole flow is arranged against; a pre-filled DATE is
	// the day they are almost certainly fixing and is visible in the field.
	now := time.Now().In(manualZone(subject.Zone))
	v.Date = now.Format("2006-01-02")
	v.Problem = oneOfWords(r.URL.Query().Get("problem"), manualProblemWords...)
	a.render(w, r, http.StatusOK, pages.AdminManualEntry(v))
}

// manualEntryReview answers the form with exactly what would be written, and mints the
// confirmation bound to it.
//
// 🔴 IT WRITES NO TRANSACTION ROW AND NO AUDIT ROW. The only side effect is a
// Set-Cookie carrying the confirmation's twin, which is what makes the value worth
// anything for an ordinary browser (deactivateconfirm.go: the MAC is what makes it
// trustworthy; the cookie is what makes it single-use for a client that honours it).
//
// 🔴 THE BOUNDS ARE CHECKED HERE AS WELL AS AT THE WRITE, and that is two calls on
// purpose rather than a duplicate: a manager must be refused BEFORE being shown a
// warning about a record that could never be written, and the bound is measured against
// the server's clock, which moves during the ten minutes a confirmation lasts.
func (a *AdminAuth) manualEntryReview(w http.ResponseWriter, r *http.Request) {
	f, ok := a.readManualForm(w, r)
	if !ok {
		return
	}
	subject, ok := a.manualSubject(w, r, f.employeeID, problemPanelWriteFailed)
	if !ok {
		return
	}
	at, problem := manualInstant(f, subject.Zone)
	if problem == "" {
		problem = manualBoundsProblem(a.entries.Bounds(at))
	}
	if problem != "" {
		a.log.Warn("panel manual entry refused: the submitted moment cannot be recorded",
			"employee_id", f.employeeID, "reason", problem)
		a.renderManualForm(w, r, subject, f, problem)
		return
	}

	token, err := a.setConfirmation(w, r, confirmActionRecordManual, manualConfirmSubject(f.employeeID, f.direction, at))
	if err != nil {
		// §4.6: a manager whose confirmation could not be minted is told the action is
		// unavailable, not shown a button that would fail.
		a.log.Error("panel: could not mint the manual-entry confirmation", "err", err)
		a.renderManualForm(w, r, subject, f, "unavailable")
		return
	}

	v := a.manualView(r, subject, f.employeeID)
	v.Date, v.Time, v.Note = f.date, f.clock, f.note
	v.Direction = string(f.direction)
	v.Confirming = true
	v.ConfirmToken = token
	v.RecordAction = manualRecordHref
	zone := manualZone(subject.Zone)
	v.WhenLocal = at.In(zone).Format("Monday 2 January 2006, 15:04")
	v.WhenUTC = at.UTC().Format(time.RFC3339)
	v.Backdated = a.entries.Backdated(at)
	a.render(w, r, http.StatusOK, pages.AdminManualEntry(v))
}

// manualEntryRecord appends the record.
//
// 🔴 SUCCESS ANSWERS 303 AND EVERY REFUSAL ANSWERS 200, AND WHAT MAKES A REFRESH SAFE
// IS NOT THE STATUS CODE. This block said "EVERY OUTCOME A MANAGER CAN CAUSE ANSWERS
// 303" until the same round's own change made five branches render instead (an
// unreadable moment, a refused confirmation, and the domain's three: ErrFuture,
// ErrTooOld, ErrDirection). The sentence was kept honest by re-measuring the property
// it was really about — "a refresh cannot write a second permanent row" — which turns
// out to hold for a reason the status code never carried:
//
//	the 303 on SUCCESS         the receipt is a redirect, so F5 re-loads the
//	                           transactions list rather than re-posting anything.
//	a REFUSED CONFIRMATION     returns BEFORE Record and WITHOUT clearing the cookie,
//	                           so re-posting meets confirmationRefusalFor again and is
//	                           refused again. Nothing was written the first time and
//	                           nothing is written the second.
//	a DOMAIN REFUSAL           happens AFTER the cookie was cleared, so re-posting is
//	                           refused as `confirm-required` — the confirmation is
//	                           spent even though no record exists, which is the safe
//	                           direction: the manager walks the warning again.
//
// Both refusal readings are measured end to end by
// TestManualEntryRecord_ARefreshOfARefusedWriteCannotAppendASecondRecord.
//
// ⚠️ WHAT IS STILL NOT CLOSED, and it is deactivateconfirm.go's counted limit rather
// than this handler's: a client that RE-PRINTS the cleared cookie can spend one mint
// repeatedly, so two SUCCESSFUL writes are reachable by a non-browser. That is the
// second-identical-record shape written up beside a.short.clear below.
//
// (The default branch renders a 500 and is right to: a database failure is not a
// refusal, and answering it like one would tell a manager their record was rejected
// when in fact nothing was asked. That branch is not one a caller's input can select.)
//
// 🔴 WHERE IT GOES AFTERWARDS IS A MEASUREMENT RATHER THAN A CLAIM. M6-04 shipped a
// banner printed from a query string alone and an audit produced the URL with zero rows
// behind it. This redirects to the TRANSACTIONS section, filtered to the record's own
// local day and to channel='manual' — both closed, already-validated filters — so what
// the manager sees is the immutable row itself, with its MANUAL marker and its stamp.
// Nothing is asserted, because nothing needs to be.
func (a *AdminAuth) manualEntryRecord(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f, ok := a.readManualForm(w, r)
	if !ok {
		return
	}
	// ⚠️ THE SUBJECT IS READ BEFORE THE CONFIRMATION IS CHECKED, AND THE ORDER IS
	// FORCED RATHER THAN CHOSEN. The confirmation is bound to the INSTANT, and the
	// instant only exists once the manager's wall clock has been read in the tenant's
	// zone — which is what this read fetches. So an unconfirmed POST costs one
	// tenant-scoped read before it is refused. It is budgeted (ProtectWriting's
	// sessionGate charges it) and it writes nothing; the cheaper order would be to
	// bind the confirmation to the person alone, which is exactly the weakening
	// manualConfirmSubject exists to refuse.
	subject, ok := a.manualSubject(w, r, f.employeeID, problemPanelWriteFailed)
	if !ok {
		return
	}
	at, problem := manualInstant(f, subject.Zone)
	if problem != "" {
		a.log.Warn("panel manual entry refused: the submitted moment could not be read",
			"employee_id", f.employeeID, "reason", problem)
		a.renderManualForm(w, r, subject, f, problem)
		return
	}
	// 🔴 THE CONFIRMATION IS CHECKED BEFORE ANYTHING IS WRITTEN, and a refusal writes
	// NOTHING: no record, no audit row. It is bound to the exact record — this person,
	// this direction, this instant — so changing any of the three in the posted form
	// after the warning was served produces a value that does not verify.
	if why := a.confirmationRefusalFor(r, confirmActionRecordManual,
		manualConfirmSubject(f.employeeID, f.direction, at)); why != "" {
		// 🔴 IT RE-RENDERS RATHER THAN REDIRECTING, AND A MEASURED SCENARIO IS WHY. The
		// confirmation lasts ten minutes; a manager who fills the form, reads the two
		// warnings, is interrupted, and then presses "Record it" lands exactly here — and
		// a 303 to manualEntryReturn would hand them a BLANK form, because the note, the
		// date and the time deliberately do not travel in a query string (§4.7: a
		// manager's sentence about a named employee must not reach the address bar, the
		// history or a Referer). Losing all three to punish a slow reader is the wrong
		// trade, and the fix costs nothing: this handler already holds the parsed form
		// and the subject, and renderManualForm is the review step's own function. No
		// second render path, no second vocabulary.
		//
		// A REFRESH IS SAFE. Nothing was written, and the confirmation cookie is NOT
		// cleared on this branch (the clear below happens only after the gate passes), so
		// re-posting is refused again rather than half-spending anything.
		a.log.Warn("panel manual entry refused: the confirmation step was not completed",
			"employee_id", f.employeeID, "reason", why)
		a.renderManualForm(w, r, subject, f, why)
		return
	}
	// THE COOKIE IS CLEARED BEFORE THE WRITE IS ATTEMPTED, so an ordinary browser
	// cannot ride the same confirmation twice, and a failed write leaves no reusable
	// value behind either. It is a REQUEST to the client rather than a ledger — a
	// client that re-prints it spends the same mint until it expires, which is measured
	// and counted at deactivateconfirm.go. What bounds the damage here is different
	// from the deactivation's `status <> 'deactivated'` predicate and weaker: a second
	// spend writes a SECOND identical record. It is visible (both rows are in the day's
	// manual list, both name the author) and it is the shape the report reads as one
	// interval plus one stranded row rather than as doubled hours — see
	// manual.CorrectionsOnlyShorten.
	a.short.clear(w, adminConfirmCookieName)

	rec, err := a.entries.Record(r.Context(), manual.Entry{
		TenantID:   id.TenantID(),
		EmployeeID: f.employeeID,
		// 🔴 THE AUTHOR COMES FROM THE SESSION. There is no field on this form for it
		// and there could not be: the id below was resolved from a signed cookie against
		// admin_users, which is the only thing transactions.entered_by may hold.
		EnteredBy: id.Admin.AdminUserID,
		Direction: f.direction,
		At:        at,
		Note:      f.note,
	})
	switch {
	case err == nil:
		a.redirect(w, manualRecordReturn(rec, subject.Zone))
	case errors.Is(err, manual.ErrUnknownEmployee):
		a.log.Warn("panel manual entry refused: no such employee in this tenant")
		a.redirect(w, employeesHref+"?problem=unknown")
	// THE THREE DOMAIN REFUSALS RE-RENDER FOR THE REASON THE CONFIRMATION ONE DOES:
	// the manager's three inputs are in hand and a redirect would throw them away.
	case errors.Is(err, manual.ErrFuture):
		a.renderManualForm(w, r, subject, f, "future")
	case errors.Is(err, manual.ErrTooOld):
		a.renderManualForm(w, r, subject, f, "too-old")
	case errors.Is(err, manual.ErrDirection):
		a.renderManualForm(w, r, subject, f, "direction")
	default:
		// A real failure is a real failure. Redirecting it into one of the refusals
		// above would tell a manager their record was rejected when nothing was asked,
		// and would hide an outage behind a plausible sentence.
		a.log.Error("panel: could not record the manual entry", "err", err,
			"employee_id", f.employeeID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// manualForm is the posted body after the boundary has validated it (§7), so the
// domain sees data that is already good.
type manualForm struct {
	employeeID uuid.UUID
	direction  manual.Direction
	// date and clock are the manager's WALL CLOCK, echoed back into the form
	// unchanged so a refused submission does not lose what they typed.
	date  string
	clock string
	note  string
}

// readManualForm bounds the body, parses it and validates every field.
//
// 🔴 THE BODY IS BOUNDED BEFORE IT IS PARSED. MaxBytesReader makes ParseForm fail on
// an oversized body rather than buffering it, so the ceiling is enforced by the READ
// rather than by a length check afterwards (review.go's shape).
//
// 🔴 THE PARSE ERROR IS CLASSIFIED, NOT PRINTED. net/url's failure QUOTES THE
// OFFENDING INPUT — an audit measured three bytes of a manager's note reaching the
// process log through exactly this branch in M6-04, and this form carries a note too.
// Nothing a caller sent appears in the log line below.
func (a *AdminAuth) readManualForm(w http.ResponseWriter, r *http.Request) (manualForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxManualEntryBody)
	if err := r.ParseForm(); err != nil {
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel manual entry refused: the form could not be read",
			"limit_bytes", maxManualEntryBody, "reason", reason)
		a.redirect(w, employeesHref+"?problem=unreadable")
		return manualForm{}, false
	}
	employeeID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("id")))
	if err != nil {
		a.log.Warn("panel manual entry refused: the posted employee id is not a uuid")
		a.redirect(w, employeesHref+"?problem=unreadable")
		return manualForm{}, false
	}
	direction, ok := manual.ParseDirection(strings.TrimSpace(r.PostFormValue("direction")))
	if !ok {
		// AN UNRECOGNISED DIRECTION IS REFUSED, NEVER DEFAULTED. Defaulting to `in`
		// would turn a mistyped or truncated form into an entry nothing ever closes,
		// and defaulting to `out` would close somebody else's shift at a time nobody
		// chose. Both are the direction that silently changes payroll.
		a.log.Warn("panel manual entry refused: the posted direction is neither in nor out")
		a.redirect(w, manualEntryReturn(employeeID, "direction"))
		return manualForm{}, false
	}
	// 🔴 THE CLIP FLAG IS DISCARDED HERE AND THAT IS NOT CARELESSNESS — THE
	// CONFIRMATION SCREEN IS A BETTER ANSWER THAN A FLAG. M6-04's rule is that silence
	// about a truncation is a defect: a manager typed three paragraphs, was told
	// "recorded", and never learned two were dropped. That flow had to signal it
	// because the note went straight into the database. This one RENDERS THE CLEANED
	// NOTE BACK on the very next screen, before anything is written, so the manager
	// reads exactly what will be stored and can shorten it themselves. A "we cut your
	// note" banner over a page that is already showing the cut text would be telling
	// somebody something they are looking at.
	note, _ := cleanBoundedText(r.PostFormValue("note"), maxManualNote)
	return manualForm{
		employeeID: employeeID,
		direction:  direction,
		date:       strings.TrimSpace(r.PostFormValue("date")),
		clock:      strings.TrimSpace(r.PostFormValue("time")),
		note:       note,
	}, true
}

// manualInstant turns the manager's wall clock into the UTC instant the record claims,
// or names the problem.
//
// 🔴 THE ZONE IS THE TENANT'S AND NEITHER THE SERVER'S NOR THE BROWSER'S (§6, and
// db/queries/tenants.sql's argument for GetTenantClock). Using the server's would make
// the same business record a different instant depending on where the VPS runs; using
// the browser's would let a client decide what a record means. The screen prints the
// zone beside the fields so nobody types 17:00 meaning one clock into a field the
// server reads as another.
//
// ⚠️ TWO LOCAL WALL CLOCKS A YEAR ARE NOT INSTANTS, AND THIS IS COUNTED RATHER THAN
// SOLVED. Malta and Turkey both had DST transitions in the period this product's
// records span (Malta still does, twice a year): the hour that is SKIPPED forward has
// no instant at all, and the hour that REPEATS has two. time.ParseInLocation answers
// both with one plausible instant rather than an error, so a record typed inside those
// two hours may land an hour from where the manager meant. The alternative — refusing
// them — would refuse a real shift on the two nights a year the overnight venues §5
// keeps naming are actually working. Whichever instant is chosen, it is written down,
// visible on the confirmation screen in local time, and correctable by the same route
// as any other mistake.
func manualInstant(f manualForm, zoneName string) (time.Time, string) {
	if f.date == "" || f.clock == "" {
		return time.Time{}, "when"
	}
	at, err := time.ParseInLocation("2006-01-02 15:04", f.date+" "+f.clock, manualZone(zoneName))
	if err != nil {
		// THE ERROR IS NOT PRINTED. time's parse failure quotes the offending input,
		// and while a date field is not a note, the rule is the same one review.go
		// learned: a classified reason is what the log needs and the caller's bytes are
		// not.
		return time.Time{}, "when"
	}
	return at.UTC(), ""
}

// manualBoundsProblem names the refusal the domain returned, or "".
func manualBoundsProblem(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, manual.ErrFuture):
		return "future"
	case errors.Is(err, manual.ErrTooOld):
		return "too-old"
	default:
		return "when"
	}
}

// manualZone loads the tenant's location, falling back to UTC.
//
// A ZONE THAT WILL NOT LOAD IS UTC AND IS SAID OUT LOUD by the screen, which prints
// whatever zone it is actually using. Guessing a different one would move every
// instant on the page silently; refusing the screen would take away the only way to
// close an open entry.
func manualZone(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// manualConfirmSubject is what the confirmation MAC is bound to: this person, this
// direction, this instant.
//
// 🔴 ALL THREE, NOT JUST THE PERSON, and that is the difference from every other gate
// in this panel. Deactivating somebody is one act with one subject; a record is a
// STATEMENT, and a confirmation bound only to the employee would let the warning be
// served for "checkout at 17:00" and spent on "check-in at 04:00". The parts are joined
// with "/" because adminConfirm.mint refuses a subject containing the payload's own
// separator, and none of the three can contain a slash: a uuid, a two-word vocabulary
// and an RFC3339 instant.
func manualConfirmSubject(employeeID uuid.UUID, d manual.Direction, at time.Time) string {
	return employeeID.String() + "/" + string(d) + "/" + at.UTC().Format(time.RFC3339)
}

// manualEntryReturn is the 303 target for the ONE refusal that cannot re-render: a
// submission whose DIRECTION could not be read.
//
// 🔴 EVERY OTHER REFUSAL RE-RENDERS THE FORM WITH THE MANAGER'S INPUT INTACT, and this
// one cannot, because there is no valid direction to echo back into the control — the
// screen would have to guess one, and guessing `in` is how a mistyped form becomes an
// entry nothing ever closes. So this path deliberately starts again, and the date, the
// time and the note are lost with it: they do NOT travel in a query string (§4.7 — a
// manager's sentence about a named employee must not reach the address bar, the
// history or a Referer), and carrying two of the three would make the form
// half-remembered in a way a manager could not predict.
//
// ⚠️ COUNTED, NOT CLOSED: reaching it needs a submission with no usable direction,
// which the rendered form cannot produce (the control offers exactly two values and
// pre-selects the chosen one). Closing it properly would mean rendering the form with
// no direction selected, which is a fourth state for a two-value control.
//
// EVERY PART IS SERVER-BUILT FROM VALIDATED VALUES — a uuid and a member of
// manualProblemWords — so this cannot become an open redirect and there is nothing in
// it to escape.
func manualEntryReturn(employeeID uuid.UUID, problem string) string {
	q := url.Values{}
	q.Set("id", employeeID.String())
	setIf(q, "problem", oneOfWords(problem, manualProblemWords...))
	return manualEntryHref + "?" + q.Encode()
}

// manualRecordReturn is where a WRITTEN record sends the manager: the transactions
// section, showing the record's own local day and the manual channel alone.
//
// 🔴 IT IS THE RECORD ITSELF RATHER THAN A SENTENCE ABOUT IT. Both parameters are
// filters that section already validates against its own closed lists, so a
// hand-edited URL narrows a list rather than claiming anything, and the row the manager
// just wrote is on the page with its MANUAL marker, its APPROVED stamp and its author's
// timestamp. This is M6-04's lesson taken to its end: the cheapest verified
// confirmation is the thing itself.
// ⚠️ THE CHANNEL VALUE IS THE DOMAIN'S CONSTANT, NOT A LITERAL, AND THE AGREEMENT
// BETWEEN IT AND THE FILTER'S DROPDOWN IS PINNED RATHER THAN ASSUMED. This redirect
// only works if panelChannels offers exactly this value: `oneOf` drops anything it
// does not, silently, which would land the manager on an unfiltered day and look like
// the record went somewhere else. TestManualEntry_LandsOnAFilterTheSectionACCEPTS
// holds the two together.
func manualRecordReturn(rec manual.Recorded, zoneName string) string {
	q := url.Values{}
	q.Set("date", rec.At.In(manualZone(zoneName)).Format("2006-01-02"))
	q.Set("channel", string(tap.ChannelManual))
	return transactionsHref + "?" + q.Encode()
}

// manualSubject loads the person the record is about, answering false having written a
// response.
// onFailure IS A PARAMETER BECAUSE THIS HELPER SERVES BOTH A GET AND TWO POSTs, and
// the two need different sentences: a reader is told the page is empty, somebody who
// pressed a button is told nothing was written. Sharing one message is how a manager
// comes to read a claim about a screen they were not looking at.
func (a *AdminAuth) manualSubject(w http.ResponseWriter, r *http.Request, employeeID uuid.UUID, onFailure pages.ProblemView) (manual.Subject, bool) {
	id := httpx.AdminOf(r)
	subject, err := a.entries.Subject(r.Context(), id.TenantID(), employeeID)
	switch {
	case err == nil:
		return subject, true
	case errors.Is(err, manual.ErrUnknownEmployee):
		a.log.Warn("panel manual entry refused: no such employee in this tenant")
		a.redirect(w, employeesHref+"?problem=unknown")
		return manual.Subject{}, false
	default:
		a.log.Error("panel: could not read the employee a record was being entered for", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, onFailure)
		return manual.Subject{}, false
	}
}

// manualView is the parts of the screen that are the same on both steps.
func (a *AdminAuth) manualView(r *http.Request, s manual.Subject, employeeID uuid.UUID) pages.ManualEntryView {
	return pages.ManualEntryView{
		PanelChrome:  a.chrome(r, pages.TabTransactions),
		Action:       manualEntryHref,
		ConfirmField: confirmField,
		EmployeeID:   employeeID.String(),
		Name:         s.Name,
		Venue:        s.LocationName,
		Department:   s.DepartmentName,
		Deactivated:  s.Deactivated(),
		Zone:         manualZone(s.Zone).String(),
		Directions:   manualDirections,
		BackHref:     employeesHref + "?manage=" + employeeID.String(),
		MaxNote:      maxManualNote,
	}
}

// renderManualForm re-renders the form after a refusal, keeping what the manager
// typed so they can fix one field rather than start again.
func (a *AdminAuth) renderManualForm(w http.ResponseWriter, r *http.Request, s manual.Subject, f manualForm, problem string) {
	v := a.manualView(r, s, f.employeeID)
	v.Date, v.Time, v.Note = f.date, f.clock, f.note
	v.Direction = string(f.direction)
	v.Problem = problem
	a.render(w, r, http.StatusOK, pages.AdminManualEntry(v))
}
