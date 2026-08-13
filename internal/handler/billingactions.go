package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE BILLING SECTION'S WRITE SIDE — M6-12 phase B. One act: freeze a month.
//
// 🔴 IT IS THE MOST PERMANENT WRITE IN THIS PRODUCT, and that is a statement about the
// SCHEMA rather than about care. `transactions` is immutable too (§4.3), but §4.3
// also names its remedy — a correction is a new row — and internal/domain/manual
// implements it. billing_periods has no such remedy: migration 0016 revokes UPDATE
// and DELETE from tappa_app and binds the table owner with a trigger, AND
// UNIQUE (tenant_id, period_month) refuses a second row for the same month. So a
// month closed wrongly cannot be edited, removed, or superseded. There is
// deliberately no credit-note shape; 0016 says the first correction that is actually
// needed decides it, with its own migration and its own ADR.
//
// That is why the act is two POSTs rather than one, and why the first writes nothing.
//
// 🔴 THE ROUTES MOUNT ProtectWriting AND THE ORDER IS THE POINT.
// floodGate → sameOriginGate → requireAdmin → sessionGate: the Origin check runs
// BEFORE the resolver, so a cross-origin POST costs zero database work. A POST
// registered in mountSections would silently take the READ chain — the defect an
// audit measured on POST /admin/review, where 300 cross-origin posts spent a
// manager's whole session budget and charged 301 unbudgeted lookups on the way.
//
// 🔴 NO *SUCCESS* AUDIT ROW IS WRITTEN IN THIS FILE, AND ITS ABSENCE IS THE DESIGN
// RATHER THAN AN OMISSION. internal/domain/billing.Close writes `billing.period_closed`
// INSIDE the same transaction that inserts the frozen row — the two share a fate, so a
// close nobody can attach to a person rolls back — and its own comment argues why that
// is the right coupling here and the wrong one elsewhere. A second success row written
// from this handler would be a second representation of one event, on a table that
// takes no UPDATE and no DELETE, i.e. permanently.
//
// ✅ A REFUSED ATTEMPT *DOES* WRITE ONE, AND THE EARLIER REASONING AGAINST IT WAS
// WRONG. It said M6-12's audit vocabulary was "settled in phase A with exactly one
// action in it", so a second was unrequested widening. That conflated two different
// events: phase A's obligation was "Book.Close already writes the SUCCESS row, do not
// write a second one", and it said nothing about a REFUSAL, which is an event that
// never reaches Book.Close at all.
//
// 🔴 THE PRECEDENT'S OWN ARGUMENT APPLIES HERE WORD FOR WORD, and it is a USER
// DECISION (2026-08-09) rather than a builder's preference: a structured log line has
// no tenant boundary, no retention story and no reader outside the operator, and the
// person who most needs to know that a manager has been trying to freeze months is the
// OWNER. Measured in the development database: policy.edit_refused 326 rows,
// location.delete_refused 100, department.delete_refused 100, billing.*_refused 0 —
// this section was the only gated act with no trail.
//
// The two readers are both kept, which is not two representations of one fact: the log
// serves the operator during an incident, the audit row serves the tenant's own owner
// next quarter. refuseBillingClose carries the rest of the argument.
//
// 🔴 §4.7 — NOTHING SECRET PASSES THROUGH THIS FILE. The body carries one ISO
// year-month and a MAC over server-chosen fields, which is rendered into the page on
// purpose, like the login form's synchronizer token. No employee, no coordinate, no
// key, and no amount — there is no parameter an amount could be posted into.

// The two addresses, built from the SECTION's own href rather than written out again
// — the rule every other section follows. If the tab moves, these move with it.
var (
	// billingCloseHref is the confirmation step. It RENDERS rather than redirecting,
	// for the reason policyChangeReview does: re-posting it is safe because it writes
	// nothing, and what travels between the steps must not go in a URL.
	billingCloseHref = billingHref + "/close"
	// billingFreezeHref is the write. A SEPARATE address so the one route in this
	// product that creates a permanent commercial record is the one route named after
	// doing so.
	billingFreezeHref = billingHref + "/close/apply"
)

// maxBillingBody is the ceiling on this section's POST bodies.
//
// A BARE ParseForm INHERITS net/http's 10 MB, and a valid session may spend
// adminSessionLimit requests per window — so the unbounded shape is 300 × 10 MB of
// allocation per session per ten minutes from somebody who has merely signed in. The
// real body is a seven-character month and a MAC, which is why this is the SMALLEST of
// the three panel body limits (review.go's 8 KiB, policyactions.go's 16 KiB): this
// form has no field that repeats per venue and no free text at all.
const maxBillingBody = 4 << 10

// mayManageBilling reports whether this session's admin may use the billing section at
// all — READ its figures and FREEZE a month.
//
// 🔴 ONE PREDICATE FOR BOTH, AND THE TWO REASONS ARE DIFFERENT. It was called
// mayCloseBilling and gated only the write; a security audit measured that a MANAGER
// could GET /admin/billing and /admin/billing.csv, i.e. read what the business owes
// Tappa. That is not a §4 breach — the red lines forbid CROSS-TENANT leakage and that
// side was measured clean — but it is commercial information the panel had never shown
// before, and it is closed on three arguments:
//
//  1. IT IS PHASE A'S OWN POSITION, FINISHED. Migration 0016 took `plan` and
//     `price_per_employee_month` away from tappa_app on INSERT *and* UPDATE, saying
//     the commercial terms are the operator's and not the customer's. Leaving the
//     READ side open to a manager left that stance half-applied.
//  2. THE SECTION WAS INTERNALLY INCONSISTENT. Its only action is owner-only, so a
//     manager was shown a figure and no way to act on it.
//  3. IT COSTS ONE LINE. Both handlers already had the identity in hand.
//
// THERE IS NO SECOND ROLE CONSTANT IN THIS FILE. It reads locationactions.go's
// adminRoleOwner, which is the one place 'owner' is spelled on the panel's write side;
// a private copy here would be a second representation of a value the schema's own
// CHECK (role IN ('owner','manager'), migration 00006) owns.
//
// 🔴 IT IS A ROLE CHECK AND NOT A POLICY ACTION, AND THE CHOICE WAS MEASURED RATHER
// THAN ASSUMED. The alternative — add `billing:close` to internal/policy's closed
// action vocabulary, grant it to the owner in the managed baseline, and gate through
// checkin.Service.StoredSet the way tenant.RuleWriter gates policy:edit — is the
// shape this repository already built once, so it was probed before being rejected.
// Three measurements, all reproducible:
//
//	(1) THE VOCABULARY IS CLOSED AND REFUSES IT. Validating a baseline document naming
//	    the action without adding the constant returns
//	    `unknown action "billing:close"`, and growing the list requires a new ADR
//	    (ADR 0004 §8; internal/policy/document.go's header states it).
//	(2) IT WOULD LOCK THE OWNER OUT OF EVERY EXISTING BUSINESS. The gate would read the
//	    STORED rulebook, not the shipped one — that is the whole point of StoredSet —
//	    and baseline.go states that existing tenants are NOT auto-updated when the
//	    baseline changes. Measured against the development database, the stored
//	    base:authz-owner statement lists exactly the six actions that shipped with
//	    BaselineVersion, and evaluating `billing:close` against a set built from it
//	    returns effect=deny sid=default — the fail-closed default. Re-derive with:
//	      docker exec -i tappa-db psql -U tappa_owner -d tappa -At -c "SELECT jsonb_path_query_first(v.document,'\$.statements[*] ? (@.sid == \"base:authz-owner\")')->'action' FROM policies p JOIN policy_versions v ON v.policy_id=p.id WHERE p.name LIKE '%panel access by role%' LIMIT 1;"
//	    Repairing that needs provisioning to re-materialise every tenant, which is
//	    M7-03 and does not exist.
//	(3) IT WOULD PUT TAPPA'S OWN COMMERCIAL GATE UNDER THE CUSTOMER'S CONTROL, AND THAT
//	    IS DEMONSTRATED RATHER THAN INFERRED. The manager refusal would come from the
//	    fail-closed DEFAULT and not from a guardrail — evaluating a manager against a
//	    widened baseline returns sid=default, not a sys: sid — and an audit then took
//	    the measurement one step further and made the point conclusive: adding a TENANT
//	    statement granting `billing:close` to a manager yields
//	    `effect=allow sid=tenant:let-managers-close`. So it is not that the refusal
//	    merely rests on a soft layer; the customer can positively grant the action to
//	    whoever they like. policy:edit does not have this problem because
//	    sys:policy-edit-owner-only denies non-owners TERMINALLY. There is no such
//	    guardrail for billing, and writing one would be a second ADR to put Tappa's
//	    invoicing inside the engine that judges the customer's taps.
//
// ⚠️ THE M6-09 OBJECTION TO A ROLE CHECK IS REAL AND DOES NOT REACH THIS ACT. There,
// replacing the engine with `role == "owner"` turned tests red because the engine
// gives a DIFFERENT answer — an owner whose stored rulebook cannot be read is refused
// policy:edit, and that is correct: nobody should rewrite a rulebook the engine cannot
// read. Applied to billing the same answer is wrong. A corrupt policy document is not
// a reason a business cannot be invoiced, and the engine has no opinion about
// `billing:close` at all — there is nothing here for a role check to disagree with.
//
// WHAT THIS SHARES WITH THE ONE ROLE CHECK THAT ALREADY SHIPPED: locationactions.go's
// mayRemove gates the product's other irreversible act on exactly this field, with
// exactly this argument, and this reads its constant rather than declaring a second
// one. The role comes from the RESOLVED SESSION, which read it from admin_users during
// session resolution — it is not a cookie claim and not a form field, so it costs no
// extra query and cannot be supplied by the caller.
func mayManageBilling(id httpx.AdminIdentity) bool {
	return id.Live() && id.Admin.Role == adminRoleOwner
}

// billingCloseReview answers the button with exactly what would be frozen, and mints
// the confirmation bound to it.
//
// 🔴 IT WRITES NO BILLING ROW AND NO AUDIT ROW. The only side effect is a Set-Cookie
// carrying the confirmation's twin. It is in mountWriting anyway, for the reason
// manualEntryReview and policyChangeReview are: minting a confirmation and setting a
// cookie IS state, and a POST registered in mountSections would silently take the READ
// chain.
//
// 🔴 IT RE-READS THE MONTH RATHER THAN TRUSTING THE PAGE THAT POSTED. The figures on
// the warning must be the server's own, taken now; a warning built from hidden inputs
// would be a warning the page could be wrong about, which is the defect an audit
// measured on the policy form's rule NAME.
func (a *AdminAuth) billingCloseReview(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	m, ok := a.readBillingMonth(w, r)
	if !ok {
		return
	}
	if !mayManageBilling(id) {
		a.refuseBillingClose(r, id, m, billingStepWarning)
		a.redirect(w, billingReturn("not-permitted"))
		return
	}
	// THE MONTH IS READ THROUGH Period, NOT Preview, so a month that is ALREADY frozen
	// is caught before a warning is served for it. Serving the warning and letting the
	// write fail would show a manager an amount that is not the one on file.
	d, err := a.books.Period(r.Context(), id.TenantID(), m)
	if err != nil {
		a.redirect(w, billingReturn(a.billingRefusal(err, m)))
		return
	}
	switch {
	case d.Frozen:
		a.redirect(w, billingReturn("already-closed"))
		return
	case !d.AfterSignup:
		a.redirect(w, billingReturn("before-signup"))
		return
	case !d.HasEnded:
		a.redirect(w, billingReturn("not-ended"))
		return
	}
	token, err := a.setConfirmation(w, r, confirmActionCloseBillingPeriod, billingConfirmSubject(m))
	if err != nil {
		// §4.6: an owner whose confirmation could not be minted is told the action is
		// unavailable, not shown a button that would fail.
		a.log.Error("panel: could not mint the billing-close confirmation", "err", err)
		a.redirect(w, billingReturn("unavailable"))
		return
	}
	a.render(w, r, http.StatusOK, pages.AdminBillingClose(a.billingCloseView(r, d, token)))
}

// billingCloseApply verifies the confirmation and freezes the month.
//
// 🔴 THE ROLE IS CHECKED AGAIN HERE AND NOT ONLY AT THE WARNING. A gate on the review
// step alone would be a courtesy: this is the route that writes, and it must refuse on
// its own without depending on which page the request came from.
//
// 🔴 THE CONFIRMATION IS CHECKED BEFORE ANYTHING IS WRITTEN, and it is bound to the
// MONTH, so a warning served for July cannot be spent on August.
//
// 🔴 THE COOKIE IS CLEARED BEFORE THE WRITE IS ATTEMPTED, so an ordinary browser
// cannot ride one mint twice. It is a REQUEST to the client rather than a ledger —
// deactivateconfirm.go counts what a client that re-prints it can still do — and what
// bounds the damage here is the strongest predicate any of the eight gates has:
// UNIQUE (tenant_id, period_month) means the second close raises 23505 and writes
// nothing at all, not even an audit row.
//
// 🔴 NO FIGURE IS POSTED AND NONE IS CHECKED AGAINST THE PAGE. Book.Close takes a
// tenant, a month and an author; the statement counts, prices and multiplies inside
// its own transaction. So the amount that freezes is the one the DEFINITION produces
// at that instant, which can differ from the warning if the roster moved in between —
// the warning screen says so in words rather than pretending otherwise.
func (a *AdminAuth) billingCloseApply(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	m, ok := a.readBillingMonth(w, r)
	if !ok {
		return
	}
	if !mayManageBilling(id) {
		a.refuseBillingClose(r, id, m, billingStepWrite)
		a.redirect(w, billingReturn("not-permitted"))
		return
	}
	if why := a.confirmationRefusalFor(r, confirmActionCloseBillingPeriod, billingConfirmSubject(m)); why != "" {
		a.log.Warn("panel billing close refused: the confirmation step was not completed",
			"period", m.String(), "reason", why)
		a.redirect(w, billingReturn(why))
		return
	}
	a.short.clear(w, adminConfirmCookieName)

	d, err := a.books.Close(r.Context(), id.TenantID(), m, id.Admin.AdminUserID)
	if problem := a.billingRefusal(err, m); problem != "" {
		a.redirect(w, billingReturn(problem))
		return
	}
	// 🔴 WHERE IT GOES AFTERWARDS IS THE THING ITSELF, NOT A SENTENCE ABOUT IT (M6-04's
	// lesson, taken the way M6-08 and M6-09 took it). The section re-reads the month
	// from billing_periods, so the owner lands on the FROZEN document — the stamp, the
	// sentence and the absent button — rather than on a message claiming it worked.
	// Nothing is asserted, because nothing needs to be.
	//
	// The figures are logged, not rendered from this value: Book.Close already wrote the
	// audit row that is the record of this act.
	a.log.Info("panel: a billing period was frozen",
		"period", d.Month.String(), "actor_id", id.Admin.AdminUserID)
	a.redirect(w, billingMonthHref(m))
}

// readBillingMonth bounds the body, parses it and validates the one field.
//
// 🔴 THE BODY IS BOUNDED BEFORE IT IS PARSED. MaxBytesReader makes ParseForm fail on
// an oversized body rather than buffering it, so the ceiling is enforced by the READ
// (review.go's shape).
//
// 🔴 THE PARSE ERROR IS CLASSIFIED, NOT PRINTED. net/url's failure QUOTES THE
// OFFENDING INPUT — an audit measured three bytes of a manager's note reaching the
// process log through exactly this branch in M6-04 — so the log line names a category
// and never the value.
//
// 🔴 A MISSING MONTH IS A REFUSAL HERE, WHICH IS THE OPPOSITE OF THE READ PATH'S
// ANSWER, and the asymmetry is the point. parseBillingMonth DROPS an unparseable month
// and shows the last finished one, because guessing wrong on a READ only moves the
// reader inside their own tenant. Guessing wrong on THIS route would freeze a month
// nobody asked for, permanently.
func (a *AdminAuth) readBillingMonth(w http.ResponseWriter, r *http.Request) (billing.Month, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBillingBody)
	if err := r.ParseForm(); err != nil {
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel billing close refused: the form could not be read",
			"limit_bytes", maxBillingBody, "reason", reason)
		a.redirect(w, billingReturn("unreadable"))
		return billing.Month{}, false
	}
	m, err := billing.ParseMonth(strings.TrimSpace(r.PostFormValue("month")))
	if err != nil || m.Zero() {
		a.log.Warn("panel billing close refused: the posted month is not a year-month")
		a.redirect(w, billingReturn("unreadable"))
		return billing.Month{}, false
	}
	return m, true
}

// billingConfirmSubject is what the confirmation MAC is bound to.
//
// IT IS THE MONTH'S ISO FORM, which cannot contain the payload's separator by
// construction (four digits, a hyphen, two digits — Month.String's own shape), so
// unlike the policy section's rule name it needs no digest. A subject that could carry
// "|" is what adminConfirm.mint refuses.
func billingConfirmSubject(m billing.Month) string { return m.String() }

// ActionPeriodCloseRefused records an attempt the role gate refused.
//
// 🔴 THE NAME FOLLOWS THE `<subject>.<verb>_<refusal>` SHAPE THE TRAIL ALREADY USES for
// an act that did NOT happen (location.delete_refused, department.delete_refused,
// policy.edit_refused), rather than the `<subject>.<past participle>` shape reserved
// for one that did. That split exists so a reader scanning `action LIKE 'billing.%'`
// does not have to look twice, and here it earns its keep: billing.period_closed is the
// SUCCESS row internal/domain/billing writes, and a prefix match on it
// (`LIKE 'billing.period_closed%'`) does not match this one.
const ActionPeriodCloseRefused = "billing.period_close_refused"

// refusedCloseDetail is the audit row's payload for a refused close.
//
// 🔴 EXPLICIT EMPTIES, NO omitempty — refusedRemovalDetail's lesson, kept: a key that is
// absent cannot be told apart from a value that was empty, and this row exists precisely
// so somebody can reconstruct what was attempted.
//
// 🔴 §4.7: THERE IS NO FIELD HERE A SECRET COULD TRAVEL IN. No token, no cookie, no
// session id, no employee and no coordinate — and no AMOUNT either, which is this
// section's own version of the rule: a refused attempt produced no figure, so recording
// one would be inventing it. What it carries is the act, the month, the step, and the
// role that was insufficient, all of which the tenant already owns.
type refusedCloseDetail struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	// Month is the period that was aimed at. It duplicates the event's Target on
	// purpose, so the row is readable without knowing which column carries the subject
	// — the same thing billing.Close's own detail does for the success row.
	Month string `json:"month"`
	// Step is "warning" or "write": WHICH of the two POSTs was refused. Without it the
	// two rows an attempt at both steps produces would be indistinguishable, and the
	// second is the more serious one — it is the request that would have created the
	// permanent row.
	Step string `json:"step"`
	// Role is the role the actor actually held, and RequiredRole is what the act needs,
	// so the row is readable without knowing the product's rules. Both are the actor's
	// own attributes, not secrets.
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
}

// The two values Step can take. They are constants rather than free text so a typo at a
// call site is a compile error rather than a row nobody can group by.
const (
	billingStepWarning = "warning"
	billingStepWrite   = "write"
)

// refuseBillingClose records an attempt the role gate refused, and answers it.
//
// 🔴 THE SCREEN NEVER OFFERS THIS CONTROL TO A MANAGER, SO REACHING HERE MEANS THE UI
// WAS BYPASSED — a stale page, a shared link, or a deliberate POST. Both halves are
// required and neither substitutes for the other: hiding the button is the courtesy,
// this is the guarantee.
//
// 🔴 IT IS CALLED FROM BOTH POSTs, AND THAT IS MEASURED AGAINST THE PRECEDENT RATHER
// THAN ASSUMED. The two removal precedents are single-POST acts and settle nothing
// about a two-step gate; the POLICY section is the two-step one, and it records at BOTH
// steps — policyChangeReview calls refusePolicyEdit directly, and policyChangeApply
// reaches it again through policyRefusal's ErrNotPermitted arm. policyactions.go states
// the principle where its trail turns out not to be idempotent: "each attempt really
// happened". A manager refused at the warning step never receives a token, so a
// subsequent POST to the write route is a SECOND, separate deliberate act — and it is
// the one that would have created the permanent row. Recording only the first would
// hide exactly the more serious half.
//
// 🔴 IT IS ITS OWN TRANSACTION (a.record -> audit.Record, not RecordTx) BECAUSE THERE
// IS NO OTHER. Nothing is being frozen — that is the point — so there is no surrounding
// write for the row to share a fate with. That is the opposite of the SUCCESS path,
// where billing.Close deliberately shares its transaction so a freeze nobody can
// attribute rolls back; here there is nothing to roll back.
//
// 🔴 AND A FAILED AUDIT WRITE DOES NOT CHANGE THE ANSWER. The manager still gets the
// refusal sentence, never a 500. This is a RECORDING path, not an authorising one: the
// refusal already happened in the caller, and turning a trail outage into "the panel is
// unavailable" would report OUR failure as a verdict on THEIR request. a.record logs
// the failure loudly and returns (§7: never swallowed).
//
// Bloat is bounded by adminSessionLimit — 300 requests per session per ten minutes,
// the same ceiling every precedent relies on.
func (a *AdminAuth) refuseBillingClose(r *http.Request, id httpx.AdminIdentity, m billing.Month, step string) {
	a.log.Warn("panel billing close refused: this admin is not an owner",
		"period", m.String(), "step", step,
		"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)

	a.record(r.Context(), audit.Event{
		// §4.5: the tenant is the SESSION's, never the request's.
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionPeriodCloseRefused,
		// THE TARGET IS THE MONTH, which is what billing.Close uses for the success row —
		// so the two events about one period are found by the same lookup.
		Target: m.String(),
		Detail: refusedCloseDetail{
			Outcome:      "refused",
			Reason:       "freezing a billing period is reserved for an owner",
			Month:        m.String(),
			Step:         step,
			Role:         id.Admin.Role,
			RequiredRole: adminRoleOwner,
		},
	})
}

// billingCloseView turns a draft into the confirmation screen.
//
// 🔴 EVERY SENTENCE IS FIXED PROSE OR A SERVER-DERIVED FIGURE. The only value that
// travelled from the client is the MONTH, which was parsed into a typed billing.Month
// and is rendered back through Month.String — four digits, a hyphen, two digits. There
// is nothing on this page a caller can put words into.
func (a *AdminAuth) billingCloseView(r *http.Request, d billing.Draft, token string) pages.BillingCloseView {
	v := pages.BillingCloseView{
		PanelChrome:  a.chrome(r, pages.TabBilling),
		Heading:      "Freeze " + monthLabel(d.Month),
		Action:       billingFreezeHref,
		Month:        d.Month.String(),
		ConfirmField: confirmField,
		ConfirmToken: token,
		BackHref:     billingMonthHref(d.Month),
	}
	v.Summary = "This stores what " + monthLabel(d.Month) + " cost as a permanent record for " +
		businessName(d.TenantName) + ". From then on this month reads its stored figures and " +
		"is never recomputed, so a person deactivated or removed later, and a price that " +
		"changes later, leave the amount exactly as it was."
	v.Warning = "A frozen month can never be edited, removed or replaced — the database refuses " +
		"all three, including for us. There is no undo and no credit note in this release, so a " +
		"month frozen by mistake stays as it is until a correction is designed for it."
	v.Detail = []pages.BillingLine{
		{Label: "Business", Value: businessName(d.TenantName)},
		{Label: "Month", Value: d.Month.String()},
		{Label: "Timezone", Value: zoneLabel(d.Zone)},
		{Label: "Period starts (UTC)", Value: isoUTC(d.From)},
		{Label: "Period ends (UTC, exclusive)", Value: isoUTC(d.To)},
		{Label: "Plan", Value: planLabel(d.Plan)},
		{Label: "People counted (live)", Value: strconv.Itoa(d.EmployeeCount)},
		{Label: "Price per person", Value: money(d.UnitPrice)},
		{Label: "Amount (live)", Value: money(d.AmountDue)},
	}
	if d.Free {
		v.Detail = append(v.Detail, pages.BillingLine{
			Label: "Founding offer",
			Value: "inside the free window — this month freezes at zero",
		})
	}
	if d.UnstampedEmployees > 0 {
		// THE FLOOR ADMISSION TRAVELS ONTO THE WARNING TOO. This is the last screen
		// before a permanent row; a caveat that appeared on the section and not here
		// would be one the reader had already scrolled past.
		v.Detail = append(v.Detail, pages.BillingLine{
			Label: "The count is a floor",
			Value: plural(d.UnstampedEmployees, "record", "records") +
				" disagree with their own dates and cannot be counted (an upper bound, not a shortfall)",
		})
	}
	return v
}
