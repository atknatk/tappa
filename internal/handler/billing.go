package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The BILLING SECTION — M6-12 phase B, the READ side. The one write is in
// billingactions.go.
//
// 🔴 NO READ PATH CAN FREEZE A PERIOD, AND THAT IS THE PROPERTY THE PANEL HAS NOW PAID
// FOR FOUR TIMES (M6-07, M6-09, M6-11, and here). Every register call below is
// Book.Period or Book.History, both SELECTs; Book.Close is unreachable from this file
// and is called from exactly one place, a POST behind ProtectWriting. That is not a
// claim about care taken: it is measured by TestBillingRead_WritesNothing, which parses
// this package's static call graph AND counts live calls, and by its database twin,
// which counts billing_periods across a GET of every address this section owns.
//
// ⚠️ SAID AT THAT EXACT WIDTH BECAUSE ONE GET DOES WRITE. /admin/billing.csv appends an
// audit_log ACCESS row, exactly as the hours export does — billingcsv.go carries the
// argument, and it is a different table answering a different question ("who downloaded
// the invoice") from the one this property is about ("can a page view create a permanent
// commercial record"). Stating it as "this file writes nothing" would have been the
// wider, wrong claim.
//
// It matters more here than on the other three sections. Freezing a month is
// PERMANENT — 0016 revokes UPDATE and DELETE on billing_periods from tappa_app and
// binds the owner with a trigger — so a read path that froze anything would hand an
// irreversible commercial act to whoever opened a page first, including a crawler, a
// prefetch or a retried request. db/queries/billing.sql's PreviewBillingPeriod header
// states the same refusal from the SQL side.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), resolved by
// the Protect chain from a signed session cookie against admin_users. The one thing a
// client controls here is WHICH MONTH, which is a narrowing predicate inside that
// tenant: a hand-edited month can only move the reader along their own business's
// timeline.
//
// 🔴 §4.7 — NO PERSON REACHES THIS SCREEN. internal/domain/billing's queries touch
// `employees` as a COUNT and never as a row, billing.Draft has no field a name could
// travel in, and pages.BillingView has none either. A billing document names one
// party: the BUSINESS being invoiced.
//
// 🔴 §4.6 — A FAILED READ IS A PROBLEM PAGE, NEVER A ZERO INVOICE. "This month costs
// nothing" is a claim about a business and a timeout is not evidence for it. The same
// rule reports.go states, and it is sharper here: a zero amount is a plausible figure
// (a founding month really is free), so a failure that fell through would be
// indistinguishable from the truth.

// billingHref is the section's own URL, READ FROM THE SECTION TABLE rather than
// written out again — the rule every other section follows. If the tab moves, the
// picker, the month links, the export and both POSTs move with it; if the tab is
// removed altogether this panics at startup rather than silently building links to
// nowhere.
var billingHref = mustSectionHref(pages.TabBilling)

// billingCSVHref is the export's URL, DERIVED FROM THE SECTION'S so the two cannot
// drift apart. reportsCSVHref's construction, deliberately.
var billingCSVHref = billingHref + ".csv"

// billingProblemWords is the closed vocabulary the section's query string may echo
// after a refused close. A reflected parameter is a value somebody can put anything
// into, and the answer is to have nothing to escape (oneOfWords).
var billingProblemWords = []string{
	"unreadable", "not-permitted", "confirm-required", "confirm-stale",
	"not-ended", "before-signup", "already-closed", "unavailable",
}

// billingProblemSentence turns one of those words into the sentence a manager reads.
//
// EVERY BRANCH IS A SENTENCE ABOUT WHAT DID NOT HAPPEN, and the default is the honest
// one: an unrecognised word cannot reach here (oneOfWords filters it) and, if it did,
// saying "that did not happen" is better than saying nothing.
func billingProblemSentence(word string) string {
	switch word {
	case "unreadable":
		return "That form could not be read, so nothing was frozen."
	case "not-permitted":
		return "Only an owner of this business can freeze a month. Nothing was frozen."
	case "confirm-required":
		return "The warning screen was not completed, so nothing was frozen. Press the button again and read it through."
	case "confirm-stale":
		return "That confirmation was for a different month — most likely another tab. Nothing was frozen; start again from the month you meant."
	case "not-ended":
		return "That month has not ended yet, so it cannot be frozen. The headcount is still moving."
	case "before-signup":
		return "That month is before this business signed up, so there is nothing to bill for it."
	case "already-closed":
		return "That month is already frozen. A month has exactly one answer and it can never be replaced."
	case "unavailable":
		return "That could not be done just now. Nothing was frozen; please try again."
	default:
		return "That did not happen."
	}
}

// panelBooks is the billing register this package needs, declared HERE at the
// consumer (§7).
//
// 🔴 THREE METHODS, AND THE READS AND THE WRITE TRAVEL TOGETHER ON PURPOSE — which is
// the opposite of what `rules`/`scribe` did for the policies section, so it is stated
// rather than assumed. There the split bought something the compiler could hold: the
// OTHER assembler of a policy set materialises, so a narrow read interface made "a
// page view cannot write a policy row" a fact rather than a promise. Here the concrete
// type is one small register over one table, splitting it would put two fields on
// AdminAuth pointing at the same *billing.Book, and the property that actually needs
// proving — that the read path never freezes — is proved by a static call-graph test
// rather than by a type. A second field would look like a guarantee and be a naming
// convention.
//
// ⚠️ Preview IS DELIBERATELY ABSENT, AND IT WAS HERE UNTIL A SECURITY AUDIT COUNTED
// ITS CALLERS: zero in production (`rg 'books\.Preview' internal/handler --glob
// '!*_test.go'`). §7 puts an interface on the CONSUMER's side precisely so it names
// what the consumer uses, and a method with no caller is not a narrower surface than
// the concrete type — it is the same surface with a comment on it. This section reads
// through Period ALONE, which is the right method anyway: Period answers with the
// FROZEN row when a month has been closed and falls back to a live preview when it has
// not, so a handler that reached for Preview directly would be one that could show a
// live recount of a month that is already invoiced. Removing it makes that mistake
// unavailable rather than merely unmade.
type panelBooks interface {
	Period(ctx context.Context, tenantID uuid.UUID, m billing.Month) (billing.Draft, error)
	History(ctx context.Context, tenantID uuid.UUID) ([]billing.Draft, bool, error)
	Close(ctx context.Context, tenantID uuid.UUID, m billing.Month, actor uuid.UUID) (billing.Draft, error)
}

// problemBillingOwnerOnly is what a manager sees at either of this section's two
// addresses.
//
// 🔴 §4.6 — THE REFUSAL IS NOT SILENT. A manager who follows a shared link, or a
// bookmark from before their role changed, is told plainly that the section exists and
// belongs to the owner. A 404 would be a lie about the product, and a blank page would
// look like a fault.
//
// ⚠️ IT NAMES NO FIGURE, which is the whole point of the gate. The sentence describes
// WHO may look, never WHAT they would have seen.
var problemBillingOwnerOnly = pages.ProblemView{
	Title:   "Billing is the owner's",
	Message: "This section shows what this business is charged for Tappa, and it is visible to an owner of the business rather than to a manager. Nothing is wrong with your account.",
	Hint:    "If you need these figures, ask an owner of the business.",
}

// billingSection renders the whole section: the month picker, the draft or the frozen
// row, the honesty block, the one control and the history.
//
// 🔴 IT IS OWNER-ONLY, READ INCLUDED (M6-12 phase B, round 3). mayManageBilling carries
// the three arguments; what this handler adds is that the gate runs BEFORE the first
// database read, so a refused request costs no query at all — the same ordering
// locationactions.go's removals use for their role check.
func (a *AdminAuth) billingSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayManageBilling(id) {
		a.refuseBillingRead(id, billingHref)
		a.renderProblem(w, r, http.StatusForbidden, problemBillingOwnerOnly)
		return
	}

	d, err := a.billingMonth(r.Context(), id.TenantID(), parseBillingMonth(r))
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to a month of zero.
		a.log.Error("panel: could not read the billing period", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	history, capped, err := a.books.History(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not read the frozen billing history", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	v := pages.BillingView{PanelChrome: a.chrome(r, pages.TabBilling)}
	fillBillingView(&v, d, history, capped, time.Now())
	// THE ECHOED WORD IS FILTERED THROUGH THE CLOSED LIST BEFORE IT IS TURNED INTO A
	// SENTENCE, so a query string carrying anything at all produces no notice rather
	// than a reflected value. An absent or unrecognised word leaves Problem empty and
	// the template renders no block.
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("problem")), billingProblemWords...); word != "" {
		v.Problem = billingProblemSentence(word)
	}
	a.render(w, r, http.StatusOK, pages.AdminBilling(v))
}

// refuseBillingRead records that a non-owner was turned away from a READ.
//
// 🔴 IT LOGS AND WRITES NO audit_log ROW, AND THAT IS A DECISION RATHER THAN THE OLD
// OMISSION. The write path's refusal DOES write one (refuseBillingClose) because an
// attempt to freeze a month is an attempt to create a permanent commercial record, and
// the owner needs to see it. A refused PAGE VIEW is not that: nothing was attempted
// against the data, the reading path of this panel writes nothing by construction (see
// this file's header and TestBillingRead_WritesNothing), and giving any signed-in
// manager a way to append rows by reloading a URL would be a bloat surface the write
// gate does not have — that one is reached only by a POST that already passed the
// Origin check.
func (a *AdminAuth) refuseBillingRead(id httpx.AdminIdentity, path string) {
	a.log.Warn("panel billing read refused: this admin is not an owner",
		"path", path, "actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
}

// billingMonth answers "which month, and is it frozen?" in at most two reads.
//
// 🔴 THE EMPTY REQUEST IS THE INTERESTING CASE, AND billing.Month{} IS NOT AN ANSWER
// TO IT. ParseMonth returns the zero Month for an absent parameter on purpose ("no
// month given" is legitimate), but the zero Month rendered as a DATE is
// -0001-12-01 — Go normalises month 0 to December of the year before — and the
// queries would happily compute a period for it and return a plausible-looking row
// about a month two thousand years ago. So a zero Month never reaches the store; this
// function resolves it first.
//
// 🔴 AND "THE LAST FINISHED MONTH" IS A QUESTION ABOUT A ZONE, WHICH IS THE §6 SEAM.
// The tenant's zone lives in the database and cannot be known before a read, so the
// resolution is: guess in UTC, read, and re-read IF the tenant's own zone disagrees
// about which month it is. The two disagree for the one-to-two hours per month
// between local midnight on the 1st and UTC midnight on the 1st — at 00:30 in Malta
// on 1 September the local answer is August and the UTC answer is July — so the
// second read costs roughly two hours a month and buys the right default the rest of
// the time. It is pinned by TestBillingMonth_ResolvesTheLastFinishedMonthInTheTenantsZone.
//
// A NAMED MONTH TAKES ONE READ AND NO GUESS, which is every request that came from
// the picker or from a history link.
func (a *AdminAuth) billingMonth(ctx context.Context, tenantID uuid.UUID, m billing.Month) (billing.Draft, error) {
	if !m.Zero() {
		return a.books.Period(ctx, tenantID, m)
	}
	guess := billing.MonthOf(time.Now(), time.UTC).Add(-1)
	d, err := a.books.Period(ctx, tenantID, guess)
	if err != nil {
		return billing.Draft{}, err
	}
	zone, zerr := time.LoadLocation(d.Zone)
	if zerr != nil {
		// A ZONE THIS BINARY CANNOT LOAD IS NOT A REASON TO REFUSE THE PAGE. The row
		// carries the name the database resolved the boundaries in, so the figures are
		// already right; only the DEFAULT month would be chosen in UTC instead. Said out
		// loud rather than swallowed (§7).
		a.log.Warn("panel: the tenant's timezone could not be loaded; defaulting the billing month in UTC",
			"zone", d.Zone, "err", zerr)
		return d, nil
	}
	want := billing.MonthOf(time.Now(), zone).Add(-1)
	if want == guess {
		return d, nil
	}
	return a.books.Period(ctx, tenantID, want)
}

// fillBillingView maps a domain draft onto the view.
//
// EVERY RENDERING DECISION IS HERE AND ONLY HERE: the currency symbol, the month's
// words, the sentences that carry Frozen and the four reasons a month may not be
// closed. `now` is a parameter rather than a call so the founding warning is testable
// without a clock (§7's habit and adminConfirm's).
func fillBillingView(v *pages.BillingView, d billing.Draft, history []billing.Draft, capped bool, now time.Time) {
	v.Queried = true
	v.TenantName = businessName(d.TenantName)

	v.MonthValue = d.Month.String()
	v.MonthLabel = monthLabel(d.Month)
	v.ZoneLabel = zoneLabel(d.Zone)
	v.FromUTC = isoUTC(d.From)
	v.ToUTC = isoUTC(d.To)

	v.SectionHref = billingHref
	v.PrevHref = billingMonthHref(d.Month.Add(-1))
	v.NextHref = billingMonthHref(d.Month.Add(1))
	v.LatestHref = billingHref
	v.ExportHref = billingCSVMonthHref(d.Month)

	// 🔴 THE THREE PLACES Frozen IS SAID. A stamp, a sentence, and the presence of the
	// control below. billing.Draft's own comment: rendering a frozen figure and a live
	// preview identically is the failure this task exists to prevent.
	v.Frozen = d.Frozen
	if d.Frozen {
		v.Stamp = "FROZEN"
		// skill tappa-brand: the WORD carries the status and the token only tints the
		// frame. tappa-green is the settled tone and this reuses the shipped modifier
		// rather than adding a hex or a class — the palette is closed.
		v.StampClass = "stamp--approved"
		v.StateLine = "These figures were frozen when this month was closed. Nothing recomputes " +
			"them: a person deactivated or removed since, and a price that has changed since, " +
			"cannot move this amount."
		v.ClosedLine = d.ClosedAt.UTC().Format(time.RFC3339)
		v.Itemisation = "What was frozen is the NUMBER of people, not which people. Nothing " +
			"stored can reconstruct that list, so this figure is an invoice and never an " +
			"itemised one."
	} else {
		v.Stamp = "DRAFT"
		// saffron is the brand's warning tone, which is exactly what a figure that can
		// still move is.
		v.StampClass = "stamp--flagged"
		v.StateLine = draftStateLine(d)
		v.Itemisation = "Freezing a month stores the NUMBER of people and not which people, so a " +
			"frozen figure can never be itemised afterwards."
	}

	v.PlanLabel = planLabel(d.Plan)
	v.EmployeeCount = strconv.Itoa(d.EmployeeCount)
	v.UnitPrice = money(d.UnitPrice)
	v.Amount = money(d.AmountDue)
	// THE DEFINITION, ON THE SCREEN, which is the card's first criterion. It is the
	// applied definition and not the card's parenthesis: the card said "active on any
	// day of the month" and the shipped rule adds a conjunct.
	v.CountLine = "Counted: everybody whose employment overlapped this month — anybody who " +
		"was on the books for even one day of it — provided their status agrees with their " +
		"own joining and leaving dates."
	if d.Free {
		v.FreeLine = "This month is inside the founding offer's free window, so the amount is " +
			"zero because of the offer and not because nobody was employed."
	}

	fillBillingUnstamped(v, d)
	fillBillingFounding(v, d, now)
	fillBillingControl(v, d)
	fillBillingHistory(v, history, capped)
}

// draftStateLine says what a DRAFT figure can still do, and it says two different
// things because a draft is two different states.
//
// 🔴 THE FIRST VERSION SAID ONE THING AND OVERSTATED IT. It read "a live count of
// TODAY'S ROSTER … it will change if somebody is HIRED", which is true of a month that
// is still running and FALSE of a month that has ended — and an audit was right to
// call it loose. The count is not over today's roster: it is over employment intervals
// that OVERLAP the month's own window (tappa_employee_is_billable, migration 0016), so
// somebody hired today cannot enter a period that has already closed.
//
// MEASURED against the live schema rather than reasoned, over an ended month's window:
//
//	hired today (activated_at = now())                 -> NOT billable in that month
//	employed for years, still active                   -> billable
//	the same person DEACTIVATED TODAY                  -> STILL billable in that month
//	status disagrees with its own stamps               -> not billable (the floor case)
//
// Re-derive it with tappa_employee_is_billable directly; the third row is the one that
// surprises, and it is why "it will change if somebody is deactivated" would have been
// wrong in the same direction.
//
// SO WHAT CAN STILL MOVE AN ENDED MONTH, also measured, from the column privileges the
// application role actually holds:
//
//	employees.activated_at / deactivated_at   UPDATE — a correction to somebody's dates
//	                                          can move them across the window's edge
//	tenants.timezone                          UPDATE — moves period_from/period_to
//	                                          themselves, which is a §6 boundary shift
//	tenants.price_per_employee_month          SELECT ONLY — the app cannot change a
//	                                          price at all; that is an operator's act
//
// So the ended-month sentence names corrections and the zone, and does NOT promise that
// hiring or dismissing somebody will move it. The running-month sentence keeps the
// original claim, where it is true.
func draftStateLine(d billing.Draft) string {
	if !d.HasEnded {
		return "This month is still running, so this is a live count: anybody hired or " +
			"leaving before it ends still changes it. A month can only be frozen once it is over."
	}
	return "This month is over, so hiring or dismissing somebody now does NOT change it — the " +
		"count is over the month's own dates, not over today's roster. It is still a draft " +
		"rather than a bill, because it is worked out afresh every time this page is opened: " +
		"correcting somebody's joining or leaving dates, or changing this business's timezone, " +
		"would move it, and so would a change to the price. Freezing stores the figures and " +
		"stops all of that."
}

// fillBillingUnstamped is §4.6 applied to money, and the WORDING is the obligation
// rather than the number.
//
// 🔴 THE COLUMN IS AN UPPER BOUND, NOT "HOW MANY THIS MONTH LOST", and a screen that
// implied the second would be worse than one that said nothing. migration 0016 states
// the measurement: the count is TENANT-WIDE and POINT-IN-TIME, so it includes people
// who left years before this period and were never candidates for it, and it cannot
// be narrowed to the period because a disagreeing row is precisely one whose
// employment interval is not known. So the sentence says the direction the figure
// moves the answer in — the headcount is a FLOOR — and then bounds its own size.
func fillBillingUnstamped(v *pages.BillingView, d billing.Draft) {
	if d.UnstampedEmployees <= 0 {
		return
	}
	v.Unstamped = "Some employee records in this business have a status that disagrees with " +
		"their own joining and leaving dates, so they cannot be counted for any month — the " +
		"figure above is a FLOOR and the real one may be higher. How much higher is not known " +
		"from here: the number beside this is every disagreeing record this business has, " +
		"including people who left long before this month and were never part of it, so it is " +
		"the most it could possibly be and not the amount missing."
	v.UnstampedCount = plural(d.UnstampedEmployees, "record disagrees with its own dates",
		"records disagree with their own dates") + " — an upper bound, not a shortfall"
}

// fillBillingFounding is the M6-12 card's third criterion: a visible warning when the
// founding offer's free window ends, "or the free period quietly extends itself".
//
// 🔴 NO DATE IS WRITTEN DOWN ANYWHERE, and that is a measured rule rather than
// caution. test/fixtures/seed.sql writes the design partners' created_at as
// `now() - interval '90 days'`, so their signup date — and therefore their free
// months and their first invoice — is a property of THE DAY THE DATABASE WAS SEEDED
// rather than of this repository. Every date on this screen comes from the row.
//
// THE CONDITION IS "THE MONTH WE ARE LIVING IN HAS REACHED THE FIRST CHARGEABLE ONE",
// not "the month being looked at has". A manager browsing last March must still be
// told that the offer has run out NOW; tying the warning to the month on screen would
// hide it from everybody who was not looking at the right one.
//
// ⚠️ IT CAN ONLY FIRE ON A PREVIEW, AND THAT IS THE DATA RATHER THAN A CHOICE. A
// frozen row records the DECISION (Free) and not the rule, so FirstChargeableMonth is
// zero on one — billing.Draft says so where the field is declared. A tenant looking
// at an old frozen month sees no warning; the same tenant's current month, which is
// the default this section opens on, does show it.
func fillBillingFounding(v *pages.BillingView, d billing.Draft, now time.Time) {
	if d.FirstChargeableMonth.Zero() {
		return
	}
	zone, err := time.LoadLocation(d.Zone)
	if err != nil {
		zone = time.UTC
	}
	current := billing.MonthOf(now, zone)
	if current.Before(d.FirstChargeableMonth) {
		// STILL INSIDE THE WINDOW. Saying WHICH month starts charging is the half of
		// this that stops the end being a surprise, and it is read from the row.
		v.FoundingHeading = "Your founding months are free until " + monthLabel(d.FirstChargeableMonth)
		v.Founding = "The founding offer covers this business's first three billing months. " +
			monthLabel(d.FirstChargeableMonth) + " is the first month that is charged for, at " +
			money(d.UnitPrice) + " per person. Nothing is invoiced before then."
		return
	}
	v.FoundingHeading = "The founding offer's free months have ended"
	v.Founding = "This business's free window ran to the end of the month before " +
		monthLabel(d.FirstChargeableMonth) + ". From " + monthLabel(d.FirstChargeableMonth) +
		" onwards each month is charged at " + money(d.UnitPrice) + " per person. This notice " +
		"is here so the free period cannot end quietly."
}

// fillBillingControl decides whether the one irreversible button is offered, and says
// why when it is not.
//
// 🔴 THE THREE CONDITIONS ARE THE SCREEN'S COPY AND NEVER THE AUTHORITY. All three are
// enforced INSIDE db/queries/billing.sql's CloseBillingPeriod — the month must have
// ended, it must not be before signup, and the UNIQUE constraint refuses a second
// close. This function decides what is SHOWN; a stale page that posts anyway meets the
// same refusals and gets a sentence for each.
//
// A CONTROL THAT SIMPLY DISAPPEARS TELLS NOBODY ANYTHING, so every branch that
// withholds it writes the reason.
//
// ⚠️ THERE WAS A FOURTH BRANCH — "only an owner can do this" — AND IT WAS DELETED
// RATHER THAN LEFT, because O1 made it unreachable. Once billingSection refuses a
// non-owner before it renders anything, every reader of this page is an owner and the
// branch is a door measured to be always open, drawn as a door. reportscsv.go names
// exactly that shape and why it is worse than no door: the next reader stops looking.
// The sentence a manager needs now lives on the refusal page
// (problemBillingOwnerOnly), which is the screen they actually reach.
func fillBillingControl(v *pages.BillingView, d billing.Draft) {
	v.CloseHref = billingCloseHref
	v.CloseLabel = "Freeze " + monthLabel(d.Month)
	switch {
	case d.Frozen:
		// No sentence: the stamp and StateLine already say it, and a third copy under
		// the docket would read as a refusal rather than as a settled fact.
	case !d.AfterSignup:
		v.WhyNotClose = "This month is before this business signed up, so there is nothing to bill for it."
	case !d.HasEnded:
		v.WhyNotClose = "This month has not ended yet. A month can only be frozen once it is over — " +
			"the headcount is still moving, and a frozen figure can never be corrected."
	default:
		v.CanClose = true
	}
}

// fillBillingHistory lists the frozen months and DECLARES THE CAP.
//
// billing.HistoryCap is a PAGE rather than a refusal — the constant's own comment
// draws that distinction against ledger.ReportEventCap, which is the other kind — so
// the honest thing is to say the list is bounded and where the rest is. The number is
// interpolated from the constant rather than written out, so the sentence cannot drift
// from the code that enforces it (reportCSVExclusions' rule).
func fillBillingHistory(v *pages.BillingView, history []billing.Draft, capped bool) {
	v.HistoryCapped = capped
	v.HistoryCapLabel = "This business has more frozen months than one page carries (limit " +
		strconv.Itoa(billing.HistoryCap) + "). Nothing has been lost — an older month is still " +
		"readable by asking for it in the month picker above."
	v.HistoryEmpty = "No month has been frozen yet. A month that has ended can be frozen from " +
		"the card above, and it then appears here."
	v.History = make([]pages.BillingHistoryRow, 0, len(history))
	for _, h := range history {
		row := pages.BillingHistoryRow{
			MonthValue: h.Month.String(),
			MonthLabel: monthLabel(h.Month),
			Employees:  plural(h.EmployeeCount, "person", "people"),
			UnitPrice:  money(h.UnitPrice) + " each",
			Amount:     money(h.AmountDue),
			ClosedAt:   h.ClosedAt.UTC().Format(time.RFC3339),
			Free:       h.Free,
		}
		if h.UnstampedEmployees > 0 {
			// THE SAME ADMISSION THE HEADLINE MAKES, ON THE ROW THAT CARRIES IT. A frozen
			// row keeps its own honesty column, so a history line that stayed silent would
			// present a floor as a total.
			row.Unstamped = "The count in this frozen month is a floor: " +
				plural(h.UnstampedEmployees, "record", "records") +
				" disagreed with their own dates when it was closed (an upper bound on the effect, not a shortfall)."
		}
		v.History = append(v.History, row)
	}
}

// money renders an exact amount with a symbol.
//
// 🔴 THE SYMBOL IS THE RENDER LAYER'S BUSINESS AND THIS IS THE RENDER LAYER.
// billing.Money.String is deliberately symbol-free and grouping-free — its own comment
// says a template knows the reader's locale and the package does not — so the symbol
// is chosen HERE, from the currency the row carries. A currency with no symbol this
// product knows prints the ISO code, which is always right and never a guess: an
// amount labelled with the WRONG symbol is a wrong statement about money.
//
// An invalid Money (the zero value: no currency, so not an amount) prints "—" rather
// than "0.00", which is the distinction Money.Valid exists to draw.
func money(m billing.Money) string {
	if !m.Valid() {
		return "—"
	}
	if m.Currency() == "EUR" {
		return "€" + m.String()
	}
	return m.String() + " " + m.Currency()
}

// monthLabel renders a billing month in words: "July 2026". The empty Month renders
// as a WORD rather than as a plausible-looking wrong month — billing.Month.String
// makes the same choice for the same reason.
func monthLabel(m billing.Month) string {
	if m.Zero() {
		return "no month"
	}
	return time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, time.UTC).Format("January 2006")
}

// planLabel puts the commercial plan in words. An unknown value is printed as itself
// rather than mapped to a default: `plan` carries a CHECK (0016) so a third value is a
// schema change, and printing it is how somebody would find out.
func planLabel(plan string) string {
	switch plan {
	case "founding":
		return "founding customer"
	case "standard":
		return "standard plan"
	default:
		return plan
	}
}

// zoneLabel and businessName turn an absent value into a WORD, for personName's
// reason: a blank cell reads as "none", which is a different claim from "we could not
// resolve it".
func zoneLabel(s string) string {
	if s == "" {
		return "unknown timezone"
	}
	return s
}

func businessName(s string) string {
	if s == "" {
		return "This business"
	}
	return s
}

// billingMonthHref and billingCSVMonthHref are the section's and the export's URLs for
// one month. Both are built from the SAME Month value the figures came from, so a link
// and the page it came from can never be one month apart.
func billingMonthHref(m billing.Month) string {
	q := url.Values{}
	setIf(q, "month", m.String())
	if len(q) == 0 {
		return billingHref
	}
	return billingHref + "?" + q.Encode()
}

func billingCSVMonthHref(m billing.Month) string {
	q := url.Values{}
	setIf(q, "month", m.String())
	if len(q) == 0 {
		return billingCSVHref
	}
	return billingCSVHref + "?" + q.Encode()
}

// billingReturn is the section's own address with a problem word on it. Every part is
// server-built from a closed list, so this cannot become an open redirect and there is
// nothing in it to escape (policiesReturn's construction).
func billingReturn(problem string) string {
	q := url.Values{}
	setIf(q, "problem", oneOfWords(problem, billingProblemWords...))
	if len(q) == 0 {
		return billingHref
	}
	return billingHref + "?" + q.Encode()
}

// parseBillingMonth reads the month from the query string.
//
// 🔴 THE VALUE IS VALIDATED AT THE BOUNDARY (§7), so the domain sees data that is
// already good. A month that does not parse is DROPPED rather than refused, which
// shows the tenant's last finished month instead of an error page — safe for
// parseReportFilter's reason: dropping can only move the reader inside their own
// tenant, never outside it, and it is visible because the picker echoes the month that
// actually took effect.
func parseBillingMonth(r *http.Request) billing.Month {
	m, err := billing.ParseMonth(strings.TrimSpace(r.URL.Query().Get("month")))
	if err != nil {
		return billing.Month{}
	}
	return m
}

// billingRefusal turns a domain error into one of the closed problem words, or "" when
// there was none.
//
// EVERY REFUSAL IS A WORD FROM billingProblemWords and every unexpected error is a
// 500-shaped redirect with a loud log line (policyRefusal's rule): a database outage
// must not be dressed up as "you are not allowed to do that". One is our failure and
// the other is a verdict on their request.
func (a *AdminAuth) billingRefusal(err error, m billing.Month) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, billing.ErrPeriodNotEnded):
		return "not-ended"
	case errors.Is(err, billing.ErrBeforeSignup):
		return "before-signup"
	case errors.Is(err, billing.ErrAlreadyClosed):
		return "already-closed"
	case errors.Is(err, billing.ErrUnknownTenant):
		// A SIGNED SESSION WHOSE TENANT IS NOT VISIBLE IS OUR PROBLEM, NOT THE
		// OPERATOR'S. Under RLS this is the same answer for "does not exist" and
		// "belongs to somebody else", so it is logged loudly and reported as
		// unavailable rather than as a verdict.
		a.log.Error("panel: the billing register could not see the session's own tenant",
			"period", m.String())
		return "unavailable"
	default:
		a.log.Error("panel: could not freeze the billing period", "err", err, "period", m.String())
		return "unavailable"
	}
}
