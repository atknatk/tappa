package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The REVIEW QUEUE section — M6-04. The panel's first WRITE.
//
// 🔴 §4.3 — WHAT THIS FILE DOES NOT DO. It does not modify a transaction, it does
// not remove one and it does not write a second one. A decision is a row in
// transaction_reviews plus a row in audit_log, both inside one transaction
// (internal/domain/review). The database gives the same answer: tappa_app holds
// neither privilege on that table (measured with has_table_privilege) and a trigger
// refuses even the owner.
//
// ⚠️ THE TWO FORBIDDEN STATEMENTS ARE DESCRIBED RATHER THAN SPELLED, because
// scripts/redline-check.sh scans this file as raw text and its R3 rule matches the
// literal wherever it appears — including in a sentence saying it must not appear.
// The first version of this comment turned `make audit` red for exactly that.
//
// 🔴 THE TENANT AND THE REVIEWER ARE NEVER INPUTS (§4.5). Both come from
// httpx.AdminOf(r), which the Protect chain resolved from a signed session cookie
// against the database. The only thing the client contributes is a transaction id
// and an outcome, and neither can widen scope: the INSERT is an INSERT ... SELECT
// whose SELECT carries the tenant predicate, so a foreign id produces no row rather
// than a foreign write.
//
// 🔴 A DECISION MADE HERE IS PERMANENT, AND NOTHING ON THIS SCREEN SAYS SO. It
// cannot be superseded, changed or deleted — docs/adr/0009 records the three
// mechanisms and the measurement. Today the consequence is presentational (no report
// reads transaction_reviews yet); it becomes monetary with M6-07. Saying it on the
// form before the button is pressed is one of that ADR's three options and is NOT
// done here.
//
// 🔴 THERE IS NO BULK APPROVAL, AND THAT IS A DECISION RATHER THAN AN OMISSION.
// M6-04's card makes it conditional ("if there is a bulk approval, one audit row
// per line"), and it is deliberately not taken here. Each decision is a separate
// legal act about a separate person's pay, and a control that decides twenty at
// once multiplies exactly the two surfaces this task had to get right: the
// concurrency window on UNIQUE (transaction_id), and the audit trail, which would
// have to stay one row per record while the failure of any single line rolls back
// the whole batch — or not, and then a partial batch has to be reported honestly.
// If it is wanted, it is a task with its own audit rounds.

// panelReviewer is the slice of internal/domain/review this handler needs, declared
// here because CLAUDE.md §7 puts an interface on the consumer's side.
type panelReviewer interface {
	Record(ctx context.Context, d review.Decision) error
}

// panelQueue is the slice of the ledger the queue needs. It is SEPARATE FROM
// panelLedger on purpose: the two sections read different things, and one wide
// interface would let a change to the day view break the queue's wiring silently.
type panelQueue interface {
	Queue(ctx context.Context, tenantID uuid.UUID, after *ledger.Cursor) (ledger.QueuePage, error)
	Pending(ctx context.Context, tenantID uuid.UUID) (ledger.Pending, error)
	// Decision is read ONLY to confirm a decision the caller says they just made.
	// See the confirmation branch in reviewSection for why a banner printed from a
	// query string alone was a claim rather than a measurement.
	Decision(ctx context.Context, tenantID, transactionID uuid.UUID) (string, bool, error)
}

// reviewHref is the section's own URL, READ FROM THE SECTION TABLE rather than
// written out again — the same rule transactionsHref follows, and it has teeth
// here too: it is what the decision form posts to.
var reviewHref = mustSectionHref(pages.TabReview)

// maxReviewNote bounds the manager's sentence.
//
// 500 IS THE FORM'S maxlength AND THE SERVER'S LIMIT, stated in both places because
// a maxlength attribute is a courtesy to a browser and not a bound: the column is
// unbounded `text`, in an append-only table nobody can clean up afterwards, on a
// route a valid session can post to 300 times per window. The number itself is a
// sentence or two — long enough to say "Maria was on the terrace, the router was
// down", short enough that the queue stays readable.
//
// 🔴 SINCE 2026-08-06 IT IS ALSO A PAGE-SIZE NUMBER, because the note is rendered
// (user decision). M6-03 shipped a control that was 96% of a page and nobody had
// measured it, so this one was measured before it shipped — real bodies off the
// real handler, ledger.PageSize dockets, every record decided:
//
//	no note at all                31 013 B
//	24-char note on all 25        33 663 B   +2 650   +8.5%
//	500-char note on all 25       45 563 B  +14 550  +46.9%   <- the ceiling
//
// ⚠️ THE MIDDLE ROW USED TO SAY "the longest note in the dev DB" AND THAT LABEL
// ROTTED WITHIN THE HOUR — because the test that proves the clip is visible writes
// a 501-character note on every run, so the longest note in that database is now
// maxReviewNote by construction (measured 2026-08-06: 10 189 reviews, 55 with a
// note, longest 500). The 24 is kept because it is a useful SHORT-NOTE reading, and
// it is now labelled as what it is: 24 characters. Never anchor a number to a
// magnitude this suite itself pollutes.
//
// ⚠️ THE WORST CASE IS BIGGER THAN THE ESTIMATE THAT PRECEDED IT. The prediction
// was 25 x 500 = 12.5 KB (+39%); the measurement is 14 550 B (+46.9%), because each
// note also carries its label and its markup. The estimate was the arithmetic of
// the payload, not of the page — the same denominator mistake this session keeps
// paying for, caught here by measuring instead of reasoning.
//
// IT IS PAID ON A Cache-Control: no-store PAGE, so it is re-sent on every view. The
// worst case needs all 25 records decided AND every note at the limit, which no
// measurement of this database can bound any more — see the warning above: the suite
// itself writes a note at the ceiling on every run, so "the real distribution today"
// is a number about the test residue rather than about customers. (This sentence
// used to quote one anyway, four lines under the paragraph forbidding it.) Unlike the roster M6-03
// removed, this is BOUNDED — it cannot grow with the size of the business, only
// with how much managers write, and this constant is that bound.
const maxReviewNote = 500

// maxReviewBody is the ceiling on the decision POST's body.
//
// 🔴 A BARE ParseForm INHERITS net/http's 10 MB, and that is the wrong number for
// the panel's only write. ParseForm reads the whole body into memory before this
// handler sees a field, the route sits in front of a database write, and a valid
// session may spend adminSessionLimit requests per window — so the unbounded shape
// is 300 x 10 MB of allocation per session per ten minutes, from a caller who has
// merely signed in. The real body is a uuid, one of two words and at most
// maxReviewNote characters; 8 KiB is already generous for the worst multi-byte
// case. The pattern and the constant's size are internal/handler/checkin.go's
// (checkinMaxBody), which bounds the tap endpoint for the same reason.
const maxReviewBody = 8 << 10

// reviewSection renders the queue.
func (a *AdminAuth) reviewSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	after := reviewCursor(r)

	page, err := a.queue.Queue(r.Context(), id.TenantID(), after)
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to an empty queue — "every
		// flagged tap has been decided" is a claim, and a database timeout is not
		// evidence for it.
		a.log.Error("panel: could not read the review queue", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	v := pages.ReviewView{
		PanelChrome: a.chrome(r, pages.TabReview),
		// 🔴 THE FORM'S TARGET COMES FROM THE SECTION TABLE, not from a literal in the
		// template. It used to be written out by hand there while this file claimed
		// otherwise, and an audit measured the consequence: changing the template's
		// action to a dead path left the entire handler package green over real HTTP.
		Action:  reviewHref,
		Queried: page.Queried,
		Done:    a.confirmedDecision(r),
		Problem: oneOfWords(r.URL.Query().Get("problem"), "taken", "gone", "yours", "unreadable"),
		// Was is WHICH decision the caller had already recorded. It only means
		// anything beside problem=yours; the template gates on that rather than on
		// this being non-empty, so a hand-edited URL cannot make the page describe a
		// decision that was never mentioned.
		Was: oneOfWords(r.URL.Query().Get("was"), "approved", "rejected"),
		// Clipped says the note the caller just submitted was longer than the
		// boundary allows and was cut. It is a closed value like the two above, so a
		// hand-edited URL can turn the sentence on but cannot put anything in it.
		Clipped: r.URL.Query().Get("clipped") == "1",
	}
	zone := page.Zone
	if zone == nil {
		zone = time.UTC
	}
	v.ZoneLabel = zone.String()
	v.Items = make([]pages.ReviewItem, 0, len(page.Records))
	for _, rec := range page.Records {
		v.Items = append(v.Items, pages.ReviewItem{
			ID: rec.ID.String(),
			// THE DAY IS PART OF THE TIME HERE AND NOT IN THE TRANSACTIONS SECTION,
			// because the queue spans days: a bare "23:14:02" on a worklist that
			// reaches back weeks tells a manager nothing about which shift it was.
			Docket: queueDocketView(rec, zone),
		})
	}
	if page.Next != nil {
		q := url.Values{}
		q.Set("after_at", page.Next.At.UTC().Format(time.RFC3339Nano))
		q.Set("after_id", page.Next.ID.String())
		v.MoreHref = reviewHref + "?" + q.Encode()
	}

	a.render(w, r, http.StatusOK, pages.AdminReview(v))
}

// confirmedDecision returns the outcome to confirm, having CHECKED IT, or "".
//
// 🔴 THE BANNER USED TO BE PRINTED FROM THE QUERY STRING ALONE, and an audit measured
// what that meant: `GET /admin/review?done=approved&clipped=1` rendered "Recorded as
// approved" and "your note was cut" with ZERO review rows in the database (before 0,
// after 0), and the URL was sendable to somebody else. There was no XSS (the
// vocabulary is closed), no lost record, and the queue on the same page contradicted
// it — but the product was asserting a fact it had not looked up, which is the class
// this task has now paid for four separate times.
//
// 🔴 WHY THIS SHAPE AND NOT A SIGNED FLASH. A one-shot signed cookie is the textbook
// answer and it costs a new secret-bearing value, a new expiry, and a new thing to
// get wrong. This costs ONE INDEXED LOOKUP — measured rather than described, because
// every other cost in this task is. EXPLAIN ANALYZE, seed tenant, warm, 3 repeats:
// 0.367 / 0.081 / 0.095 ms in an audit's run and 0.427 / 0.190 / 0.106 ms reproduced
// here, both on Index Scan using transaction_reviews_tenant_idx. It is paid on GETs
// carrying ?done only — once per decision, never on an ordinary section view — and
// the query already existed for another caller. The figure is repeated in
// adminratelimit.go beside the ceilings it affects. The cheaper mechanism was chosen
// because the defect is a false STATEMENT rather than a forged authorisation: there
// is nothing here to authorise.
//
// ⚠️ WHAT IT STILL DOES NOT PROVE: that THIS operator made the decision, or that they
// made it just now. A manager who reloads a stale confirmation URL for a record that
// was in fact decided sees the banner again. That is a true sentence about the
// record, which is the property being bought; "you, a moment ago" would need the
// flash.
func (a *AdminAuth) confirmedDecision(r *http.Request) string {
	want := oneOfWords(r.URL.Query().Get("done"), "approved", "rejected")
	if want == "" {
		return ""
	}
	txnID, err := uuid.Parse(r.URL.Query().Get("for"))
	if err != nil {
		return ""
	}
	got, ok, err := a.queue.Decision(r.Context(), httpx.AdminOf(r).TenantID(), txnID)
	if err != nil {
		// A failed check is NOT a confirmation. The section still renders — the queue
		// is the page's job and it answered — but nothing is claimed about a decision
		// nobody could read.
		a.log.Error("panel: could not verify the decision being confirmed", "err", err)
		return ""
	}
	if !ok || got != want {
		return ""
	}
	return want
}

// queueDocketView maps one queued record onto a card.
//
// IT REUSES docketView AND CHANGES ONE THING: the time carries its date. Everything
// else — the evidence signs, the stamp, the tallies — is the shape a manager has
// already learned in the Transactions section, and a second layout would be a
// second place for the evidence line to drift.
func queueDocketView(rec ledger.Record, zone *time.Location) components.DocketView {
	d := docketView(rec, zone)
	d.At = rec.OccurredAt.In(zone).Format("2 Jan 15:04:05")
	return d
}

// reviewDecision records one approval or rejection.
//
// EVERY OUTCOME A MANAGER CAN CAUSE ANSWERS 303, so the result is always a plain
// page load: a rendered POST means a refresh re-submits the decision, and this one
// is not idempotent in a way a manager can see — the second submission would be
// refused as "already decided" by somebody looking at their own first one.
//
// ⚠️ "ALWAYS" IS NOT THE WORD, AND THE FUNCTION REFUTES IT 75 LINES DOWN. The
// default branch of the switch below renders a 500 through renderProblem, and it is
// right to: a database failure is not a refusal, and redirecting it would tell a
// manager their decision was rejected when in fact nothing was asked. So the honest
// statement is the one above — every branch the CALLER's input can select is a
// redirect; the one that is not is the branch nobody selected.
//
// (An emphatic absolute that the same function contradicts is this task's most
// expensive class of defect, in its cheapest form: nothing behaves wrongly, and the
// next reader trusts the sentence instead of the switch.)
func (a *AdminAuth) reviewDecision(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)

	// THE BODY IS BOUNDED BEFORE IT IS PARSED. MaxBytesReader makes ParseForm fail
	// on an oversized body rather than buffering it, so the ceiling is enforced by
	// the read rather than by a length check afterwards.
	r.Body = http.MaxBytesReader(w, r.Body, maxReviewBody)
	// 🔴 A MALFORMED REQUEST GETS problem=unreadable, NOT problem=gone, AND THE
	// DIFFERENCE IS §4.6's SUBJECT AGAIN. "gone" renders "that record is not waiting
	// for a decision any more" — a statement about the RECORD, which nothing here
	// has looked up. On an oversized body the record IS still waiting; on an
	// unparseable id there is no record to make a statement about at all. Both
	// sentences were wrong in the same way B4's "somebody else" was: true about the
	// write, invented about the thing it names.
	//
	// IT IS NOT AN EXISTENCE ORACLE. The three-way collapse in
	// internal/domain/review (does not exist / another tenant's / not flagged) is
	// deliberate and stays: those are questions about a RECORD and answering them
	// separately would tell a stranger whether an id exists elsewhere. A request
	// that could not be read is not one of those questions — the caller already
	// knows what they sent, so saying "we could not read that" reveals nothing.
	if err := r.ParseForm(); err != nil {
		// 🔴 THE ERROR IS CLASSIFIED, NOT PRINTED, AND THIS FILE'S OWN RULE IS WHY.
		// reviewNote's block says the note is not logged; this branch was the one
		// hole in it. net/url's parse failure QUOTES THE OFFENDING INPUT — a body
		// with a bad percent-escape produced `invalid URL escape "%ZZ"`, i.e. about
		// three bytes of the manager's note in the process log, on the only path
		// that could leak any of it. Measured by an audit, with a canary.
		//
		// The two causes are told apart because they mean different things
		// operationally (a ceiling doing its job vs a broken client), and neither
		// reason string contains anything the caller sent.
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel review refused: the form could not be read",
			"limit_bytes", maxReviewBody, "reason", reason)
		a.redirect(w, reviewHref+"?problem=unreadable")
		return
	}
	txnID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("id")))
	if err != nil {
		a.log.Warn("panel review refused: the posted record id is not a uuid")
		a.redirect(w, reviewHref+"?problem=unreadable")
		return
	}
	outcome, ok := review.Parse(r.PostFormValue("outcome"))
	if !ok {
		// AN UNRECOGNISED OUTCOME IS REFUSED, NEVER DEFAULTED. Defaulting to
		// "approved" would make a mistyped or truncated form an approval, which is
		// the one direction that silently adds paid hours.
		a.log.Warn("panel review refused: the posted outcome is not one of the two")
		a.redirect(w, reviewHref+"?problem=unreadable")
		return
	}

	note, clipped := reviewNote(r.PostFormValue("note"))
	err = a.reviewer.Record(r.Context(), review.Decision{
		TenantID:      id.TenantID(),
		TransactionID: txnID,
		ReviewerID:    id.Admin.AdminUserID,
		Outcome:       outcome,
		Note:          note,
	})
	var decided *review.DecidedError
	switch {
	case err == nil:
		// 🔴 THE CLIP IS CARRIED INTO THE CONFIRMATION. Without it the screen says
		// "Recorded as approved" and stops, which is true and incomplete: part of
		// what the manager wrote is gone and only the product knows.
		// THE RECORD'S ID TRAVELS WITH THE CONFIRMATION so the next request can VERIFY
		// it instead of believing it. It is not a capability: the id is scoped to the
		// caller's own tenant on the way back, and a foreign or invented one simply
		// confirms nothing.
		to := reviewHref + "?done=" + string(outcome) + "&for=" + txnID.String()
		if clipped {
			to += "&clipped=1"
		}
		a.redirect(w, to)
	case errors.As(err, &decided) && decided.ByYou:
		// 🔴 §4.6 ON THE SUBJECT OF THE SENTENCE, NOT ON ITS PREDICATE. A double click
		// on Approve produces exactly this branch, and the first version of this
		// screen answered it with "somebody else decided that record first" — true
		// about the write, invented about the WHO. The manager's own decision IS
		// recorded; what did not happen is a second one.
		a.log.Info("panel review: the caller had already decided this record",
			"transaction_id", txnID, "outcome", string(decided.Outcome))
		a.redirect(w, reviewHref+"?problem=yours&was="+string(decided.Outcome))
	case isReviewTaken(err):
		// §4.6 — THE LOSER OF A RACE IS TOLD. Two managers can press Approve on the
		// same card; UNIQUE (transaction_id) lets exactly one row exist, and the
		// other must not be shown a clean queue and left to believe their click was
		// what decided it.
		a.log.Info("panel review: the record was already decided by somebody else",
			"transaction_id", txnID)
		a.redirect(w, reviewHref+"?problem=taken")
	case isReviewGone(err):
		a.log.Warn("panel review refused: not a reviewable record", "transaction_id", txnID)
		a.redirect(w, reviewHref+"?problem=gone")
	default:
		// A real failure is a real failure. It is NOT redirected into "gone", which
		// would tell a manager their decision was refused when in fact the database
		// was unreachable — and would hide an outage behind a plausible sentence.
		a.log.Error("panel: could not record the review", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
	}
}

// isReviewTaken / isReviewGone name the two refusals the screen can explain.
//
// THE SELF-REVIEW CASE IS FOLDED INTO "gone" rather than given a sentence of its
// own: it is unreachable today (a reviewer id comes from admin_users and the
// reviewed party from employees, two different id spaces) and inventing a screen
// for it would be describing a state the product cannot produce.
func isReviewTaken(err error) bool { return errors.Is(err, review.ErrAlreadyDecided) }
func isReviewGone(err error) bool {
	return errors.Is(err, review.ErrNotReviewable) || errors.Is(err, review.ErrSelfReview)
}

// reviewNote validates the manager's sentence at the boundary (§7), so the domain
// sees data that is already good, and REPORTS WHETHER IT HAD TO CUT IT.
//
// 🔴 THE SECOND RETURN VALUE IS THE POINT AND IT IS A USER DECISION (2026-08-06).
// The first version cut at maxReviewNote and said nothing, on a field nothing
// rendered — so a manager could type three paragraphs, be told "Recorded as
// approved", and never learn that two of them were dropped. Silence about a cut is
// the same class of defect as a screen claiming something it has not measured, in
// the opposite direction: here the product measured something and did not say it.
//
// REFUSING THE WHOLE DECISION WAS THE OTHER OPTION AND WAS REJECTED: the decision
// is the part that matters and the note is an annotation, so throwing away a
// manager's click because their sentence was long would trade the important thing
// for the unimportant one. What ships instead is: keep the decision, keep the first
// maxReviewNote characters, SAY SO, and render the note back so they can see
// exactly what was kept.
//
// IT IS employeeNameFilter's SAME THREE STEPS AND THAT IS DELIBERATE, because the
// two 500s that shaped that function apply here word for word: a NUL byte cannot be
// stored in a PostgreSQL text column (the driver refuses the whole statement), and
// truncating by BYTE cuts multi-byte characters in half — and this text is written
// by Maltese and Turkish managers about Maltese and Turkish names, so ċ ġ ħ ż and İ
// are the ordinary case rather than an adversarial one.
//
// 🔴 IT DOES NOT ESCAPE, AND THIS PARAGRAPH IS ON ITS THIRD VERSION BECAUSE THE
// FIRST TWO DESCRIBED A DIFFERENT SYSTEM. Version one said "the note is rendered by
// templ, which escapes on output" while NOTHING rendered it. Version two said it
// was write-only and named the consumer as future work. Both were true when
// written and both stopped being true — the second within a day, when the user
// decided (2026-08-06) that the note SHOULD be rendered. What is true now, measured
// rather than reasoned:
//
//	the note is SELECTed by ListPanelTransactions (rv.note), carried on
//	ledger.Record.ReviewNote and components.DocketView.ReviewNote, and rendered on
//	the docket of the record it decided. templ escapes it there — PROVED by
//	TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered, which stores a note
//	containing <script>, a quote and an ampersand and reads the rendered markup.
//
// So escaping HERE would store the escape and show it back, which is the mistake
// M6-03 shipped once on the name filter. The boundary cleans; the renderer encodes.
//
// ⚠️ AND templ's ESCAPING DOES NOT TRAVEL. M6-07's CSV export reads the same column
// and gets none of it; a spreadsheet treats a cell beginning = + - or @ as a
// formula. That rule is still that task's, and rendering the note here does not
// discharge it.
func reviewNote(raw string) (note string, clipped bool) {
	s := strings.ReplaceAll(raw, "\x00", "")
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxReviewNote {
		runes := []rune(s)
		s = string(runes[:maxReviewNote])
		clipped = true
	}
	// The cut can leave a trailing space, and any invalid sequence the client sent
	// for its own reasons is dropped rather than forwarded. Neither of those counts
	// as a clip: they remove nothing the manager meant to write.
	return strings.TrimSpace(strings.ToValidUTF8(s, "")), clipped
}

// reviewCursor reads the keyset position, BOTH HALVES OR NEITHER — the same rule
// parseTransactionFilter states: half a keyset position is not a position.
func reviewCursor(r *http.Request) *ledger.Cursor {
	q := r.URL.Query()
	at, atErr := time.Parse(time.RFC3339Nano, q.Get("after_at"))
	id, idErr := uuid.Parse(q.Get("after_id"))
	if atErr != nil || idErr != nil {
		return nil
	}
	return &ledger.Cursor{At: at, ID: id}
}

// oneOfWords accepts s only if it is one of the given words. It is how the two
// query-string flags this screen echoes are kept to a closed set — a reflected
// parameter is a value somebody can put anything in, and the answer is not to
// escape it more carefully but to have nothing to escape.
func oneOfWords(s string, allowed ...string) string {
	for _, a := range allowed {
		if s == a {
			return s
		}
	}
	return ""
}

// chrome builds the panel's surround for one request, INCLUDING the queue badge.
//
// 🔴 IT IS ONE FUNCTION SO THAT EVERY SECTION PAYS THE SAME COST AND SHOWS THE SAME
// NUMBER. The badge sits in the navigation, so it has to be read on whichever
// section a manager opens; a per-handler copy is how one section quietly stops
// counting. Measured cost of that choice, per panel request (EXPLAIN ANALYZE, seed
// tenant, warm, after ANALYZE): 0.7-7.1 ms with records waiting, 17.8-20.3 ms with
// the queue EMPTY — the empty case is dearer because the cap never fills, and it is
// the steady state of a panel somebody keeps clear. See ledger.PendingCap.
//
// 🔴 A FAILED COUNT DOES NOT FAIL THE PAGE, AND IT DOES NOT SAY ZERO EITHER. The
// section a manager asked for is still worth rendering; what is not acceptable is a
// badge that vanishes and reads as "nothing waiting" (§4.6, the class M5-11
// closed). PendingBadge.Known stays false and the navigation prints "?".
func (a *AdminAuth) chrome(r *http.Request, tab pages.PanelTab) pages.PanelChrome {
	id := httpx.AdminOf(r)
	c := pages.PanelChrome{
		FullName: id.Admin.FullName,
		Role:     id.Admin.Role,
		Tab:      tab,
	}
	p, err := a.queue.Pending(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not count the review queue", "err", err)
		return c
	}
	c.Pending = pages.PendingBadge{Known: true, N: p.N, Capped: p.Capped}
	return c
}
