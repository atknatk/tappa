package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE ACCOUNT SECTION'S WRITE SIDE — M7-05. One act: rewrite the three facts a
// business may change about itself.
//
// 🔴 THE ROUTE MOUNTS ProtectWriting AND THE ORDER IS THE POINT.
// floodGate → sameOriginGate → requireAdmin → sessionGate: the Origin check runs
// BEFORE the resolver, so a cross-origin POST costs zero database work. A POST
// registered in mountSections would silently take the READ chain — the defect an audit
// measured on POST /admin/review, where 300 cross-origin posts spent a signed-in
// manager's whole session budget and charged 301 unbudgeted lookups on the way.
//
// 🔴 IT IS OWNER-ONLY, AND EACH OF THE THREE FIELDS EARNS THAT ON ITS OWN.
//
//	name           is what the invoice is made out to and what every export header
//	               says.
//	timezone       is the wall clock every shift, every lateness verdict and every
//	               panel "day" is resolved against, and it moves both ends of every
//	               billing month that has not been frozen yet.
//	business_type  chooses the sentence every employee in the business reads after a
//	               tap that counted.
//
// 🔴 IT IS A ROLE CHECK AND NOT A POLICY ACTION, AND THE CHOICE IS MEASURED RATHER
// THAN ASSUMED — the same measurement M6-12 phase B recorded for `billing:close`, which
// applies here word for word and was re-derived rather than cited:
//
//	(1) THE ACTION VOCABULARY IS CLOSED. internal/policy refuses a statement naming an
//	    action that is not a declared constant, and growing the list requires a new ADR
//	    (ADR 0004 §8).
//	(2) IT WOULD LOCK THE OWNER OUT OF EVERY EXISTING BUSINESS. The gate would read the
//	    STORED rulebook, and internal/policy/baseline.go states that existing tenants
//	    are NOT re-materialised when the baseline changes — so a brand-new action
//	    evaluates to the fail-closed default (effect=deny, sid=default) for every
//	    tenant provisioned before today, i.e. all of them.
//	(3) IT WOULD PUT THE GATE UNDER THE CUSTOMER'S CONTROL. A refusal that comes from
//	    the fail-closed default rather than from a `sys:` guardrail is one a TENANT
//	    statement can positively override, which is how M6-12's audit demonstrated a
//	    manager being granted `billing:close`. Writing a guardrail instead would be a
//	    second ADR putting the settings screen inside the engine that judges taps.
//
// THERE IS NO SECOND ROLE CONSTANT IN THIS FILE. It reads locationactions.go's
// adminRoleOwner, which is the one place 'owner' is spelled on the panel's write side;
// a private copy would be a second representation of a value the schema's own CHECK
// (role IN ('owner','manager'), migration 00006) owns.
//
// 🔴 THE SUCCESS ROW IS NOT WRITTEN HERE AND ITS ABSENCE IS THE DESIGN.
// internal/domain/tenant.Accounts.Save writes `tenant.account_updated` INSIDE the same
// transaction as the UPDATE, so a change nobody can attach to a person rolls back —
// which matters more on this table than on most, because `tenants` has no updated_at
// at all (migration 00001). A second row written from this handler would be a second
// representation of one event on an append-only table.
//
// ✅ A REFUSED ATTEMPT DOES WRITE ONE, following the precedent M6-12 phase B settled
// on a user decision (2026-08-09): a structured log line has no tenant boundary, no
// retention story and no reader outside the operator, and the person who most needs to
// know that a manager has been trying to move the business's timezone is the OWNER.
//
// 🔴 §4.7 — NOTHING SECRET PASSES THROUGH THIS FILE. The body carries a name, a
// business type and a zone name; there is no field a token, a coordinate, a key or an
// employee could be posted into.

// maxAccountBody is the ceiling on this section's POST body.
//
// A BARE ParseForm INHERITS net/http's 10 MB, and a valid session may spend
// adminSessionLimit requests per window — so the unbounded shape is 300 × 10 MB of
// allocation per session per ten minutes from somebody who has merely signed in. The
// real body is three short fields, one of which the domain caps at 120 runes, so this
// sits with review.go's 8 KiB rather than with the venue form's larger limit: there is
// no field here that repeats per venue.
const maxAccountBody = 8 << 10

// mayEditAccount reports whether this session's admin may SAVE these details.
//
// IT GATES THE WRITE ONLY. The READ is open to a manager, and account.go carries that
// argument: nothing on the page is a commercial term, and the timezone is the one fact
// a manager most needs to be able to check.
//
// The role comes from the RESOLVED SESSION, which read it from admin_users during
// session resolution — it is not a cookie claim and not a form field, so it costs no
// extra query and cannot be supplied by the caller.
func mayEditAccount(id httpx.AdminIdentity) bool {
	return id.Live() && id.Admin.Role == adminRoleOwner
}

// accountSave rewrites the three editable facts.
//
// 🔴 A REJECTED FORM RE-RENDERS INSTEAD OF REDIRECTING, which is saveVenue's shape and
// for the same reason: the name is free text somebody typed, and a redirect would
// throw it away to report that a zone name has a typo in it. Nothing is written on that
// path, so a browser re-post is harmless. A SUCCESSFUL save still redirects, which is
// what keeps a refresh from saving twice.
func (a *AdminAuth) accountSave(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxAccountBody)
	if err := r.ParseForm(); err != nil {
		// 🔴 THE PARSE ERROR IS CLASSIFIED, NOT PRINTED. net/url's failure QUOTES THE
		// OFFENDING INPUT — an audit measured three bytes of a manager's note reaching
		// the process log through exactly this branch in M6-04 — so the log line names a
		// category and never the value.
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel account save refused: the form could not be read",
			"limit_bytes", maxAccountBody, "reason", reason)
		a.redirect(w, accountReturn("", "unreadable"))
		return
	}

	form := pages.AccountForm{
		Name:         strings.TrimSpace(r.PostFormValue("name")),
		BusinessType: strings.TrimSpace(r.PostFormValue("business_type")),
		Timezone:     strings.TrimSpace(r.PostFormValue("timezone")),
	}

	// 🔴 THE ROLE IS CHECKED BEFORE THE VALIDATION AND BEFORE THE FIRST DATABASE READ,
	// so a refused request costs no query at all — the ordering locationactions.go's
	// removals use for their role check.
	if !mayEditAccount(id) {
		a.refuseAccountSave(r, id)
		a.redirect(w, accountReturn("", "not-permitted"))
		return
	}

	// 🔴 THE BUSINESS TYPE IS VALIDATED AT THE BOUNDARY (§7) AGAINST THE ONE GO COPY OF
	// THE SCHEMA'S VOCABULARY. internal/domain/signup.BusinessTypes is pinned to
	// migration 00001's CHECK by TestBusinessTypes_AreExactlyTheSchemasVocabulary, and
	// it is the same list the <select> was rendered from — so this refuses a value that
	// did not come from the form we drew. The domain refuses it a second time from the
	// database's own CHECK, which is the authority.
	if field, msg := validateAccountForm(form); msg != "" {
		a.log.Warn("panel account save refused: the form did not validate", "field", field)
		a.renderAccountFormAgain(w, r, id, form, field, msg)
		return
	}

	saved, err := a.accounts.Save(r.Context(), tenant.AccountCommand{
		TenantID: id.TenantID(),
		// 🔴 THE ACTOR COMES FROM THE SESSION. There is no field on this form for it and
		// there could not be: the id below was resolved from a signed cookie against
		// admin_users, which is the only thing audit_log.actor_id may hold.
		ActorID:      id.Admin.AdminUserID,
		Name:         form.Name,
		BusinessType: form.BusinessType,
		Timezone:     form.Timezone,
	})
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrAccountName):
		// The domain's own bound refused a name the boundary let through, which means
		// the two disagree. It is reported as a form failure rather than a 500 — the
		// owner can still fix it — and logged as the inconsistency it is.
		a.log.Error("panel account save: the domain refused a name the boundary accepted")
		a.renderAccountFormAgain(w, r, id, form, "name",
			"That name cannot be stored. Try a shorter one.")
		return
	case errors.Is(err, tenant.ErrBusinessType):
		a.log.Error("panel account save: the database refused a business type the boundary accepted")
		a.renderAccountFormAgain(w, r, id, form, "business_type",
			"That is not one of the kinds of business this product knows. Pick the closest match.")
		return
	case errors.Is(err, tenant.ErrTimezone):
		a.log.Warn("panel account save refused: the timezone is not one this build can load")
		a.renderAccountFormAgain(w, r, id, form, "timezone",
			"That is not a timezone this product can load. It needs an IANA name, such as Europe/Malta.")
		return
	case errors.Is(err, tenant.ErrTimezoneMovesBilling):
		// backlog T37. The zone is valid; adopting it would move the first month this
		// business can be billed for, and billing_periods is append-only, so the
		// change would be permanent. The sentence says what it would do rather than
		// what is forbidden, and it names the way out — an operator can still make
		// the change deliberately.
		a.log.Warn("panel account save refused: the timezone would move the first chargeable month",
			"tenant_id", id.Admin.TenantID)
		a.renderAccountFormAgain(w, r, id, form, "timezone",
			"That timezone would move the first month you can be billed for, so we cannot "+
				"change it from this screen. Your business signed up close to a month boundary, "+
				"which is why the zone decides which month is your first paid one. Ask Tappa and "+
				"we will change it with you.")
		return
	case errors.Is(err, tenant.ErrUnknownTenantAccount):
		// A SIGNED SESSION WHOSE OWN TENANT IS NOT VISIBLE IS OUR PROBLEM, NOT THE
		// OWNER'S. Under RLS this is the same answer for "does not exist" and "belongs
		// to somebody else", so it is logged loudly and reported as ours.
		a.log.Error("panel account save: the session's own tenant row could not be read")
		a.redirect(w, accountReturn("", "unknown-business"))
		return
	default:
		a.log.Error("panel: could not save the business account", "err", err)
		a.redirect(w, accountReturn("", "unavailable"))
		return
	}

	// 🔴 WHERE IT GOES AFTERWARDS IS THE THING ITSELF (M6-04's lesson). The section
	// re-reads the row, so the owner lands on what was STORED rather than on a message
	// claiming it worked — which matters here because the zone is the field most likely
	// to be typed wrongly and the page then shows the zone that actually took effect.
	//
	// The zone is logged and the name is not: internal/domain/tenant.Save draws that
	// line and this side keeps it.
	a.log.Info("panel account saved", "actor_id", id.Admin.AdminUserID, "timezone", saved.Timezone)
	a.redirect(w, accountReturn("saved", ""))
}

// validateAccountForm is the boundary's own check (§7). It returns the FIELD and the
// sentence, so a message renders beside the input it is about.
func validateAccountForm(f pages.AccountForm) (field, msg string) {
	switch {
	case f.Name == "":
		return "name", "A business needs a name. This is what your invoice is made out to."
	case utf8.RuneCountInString(f.Name) > tenant.AccountNameLimit:
		return "name", "That name is too long for the invoice and the exports. Shorten it."
	case !signup.ValidBusinessType(f.BusinessType):
		return "business_type", "Pick the closest match from the list."
	case f.Timezone == "":
		return "timezone", "A timezone is needed: it is the clock every date in this panel is worked out in."
	case !tenant.ValidTimezone(f.Timezone):
		// 🔴 THE BOUNDARY ASKS THE DOMAIN'S OWN QUESTION RATHER THAN CALLING
		// time.LoadLocation ITSELF. A second call here would be a second rule — and the
		// domain's refuses two names LoadLocation happily resolves ("" and "Local"),
		// which a simplified copy would quietly start accepting.
		return "timezone", "That is not a timezone this product can load. It needs an IANA name, such as Europe/Malta."
	}
	return "", ""
}

// renderAccountFormAgain re-renders the section with what was posted and one message.
//
// 🔴 IT RE-READS THE ROW RATHER THAN RECONSTRUCTING THE PAGE FROM THE FORM. The VAT
// verdict and the registration date are facts the form never carried, so a page rebuilt
// from the submission alone would omit them.
//
// 🔴 ONLY `Form` IS OVERWRITTEN, AND THE FIRST DRAFT OVERWROTE THE FACTS WITH IT. That
// draft's comment claimed a page built from the submission "would either omit them or
// invent them" — and then invented them, because the facts docket read `Form`.
// Measured on this exact path (owner, name="NOT THE STORED NAME Ltd",
// timezone="Europe/Maltaa", so the save is refused and nothing is written):
//
//	code=200  saves=0                          nothing was written
//	the typed name and zone                    printed under the heading "On file"
//	the STORED name and zone                   absent from the page entirely
//	the send-off                               the TYPED kind of business, not the one
//	                                           this business's staff actually read
//
// Three false facts about a business, under a heading that says they are on file, one of
// them the clock every date in the panel is worked out in — and the true values nowhere,
// so the owner could not see the difference. `AccountView.OnFile` is the second field
// that closes it; `AccountView.Unsaved` is what says, per field, that the box and the
// file disagree and which one the row holds — and it needs no "was this a refusal" flag,
// because this function is the only place in the package where the two can differ at all.
//
// A FAILED RE-READ IS A PROBLEM PAGE. Nothing was written on this path either way, and
// showing a form with no facts around it would be worse than saying the read failed.
func (a *AdminAuth) renderAccountFormAgain(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, form pages.AccountForm, field, msg string) {
	s, err := a.accounts.Settings(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not re-read the business account after a refused save", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAccountUnreadable)
		return
	}
	v := a.accountView(r, id, s)
	// THE BOXES TAKE THE SUBMISSION; v.OnFile KEEPS THE ROW. Overwriting both is the
	// defect this comment block describes.
	v.Form = form
	v.Field = field
	v.FieldError = msg
	// 200 RATHER THAN 400, which is the answer saveVenue gives to the same question:
	// the response IS the form, and a browser showing a form is not an error condition.
	a.render(w, r, http.StatusOK, pages.AdminAccount(v))
}

// accountReturn is the section's own address with a word on it. Every part is
// server-built from a closed list, so this cannot become an open redirect and there is
// nothing in it to escape (billingReturn's construction).
func accountReturn(done, problem string) string {
	q := url.Values{}
	setIf(q, "done", oneOfWords(done, accountDoneWords...))
	setIf(q, "problem", oneOfWords(problem, accountProblemWords...))
	if len(q) == 0 {
		return accountHref
	}
	return accountHref + "?" + q.Encode()
}

// ActionAccountUpdateRefused records an attempt the role gate refused.
//
// 🔴 THE NAME FOLLOWS THE `<subject>.<verb>_<refusal>` SHAPE THE TRAIL ALREADY USES for
// an act that did NOT happen (location.delete_refused, policy.edit_refused,
// billing.period_close_refused), rather than the `<subject>.<past participle>` shape
// reserved for one that did. That split is what lets a reader scanning `action LIKE
// 'tenant.%'` tell the two apart without looking twice — and here it earns its keep,
// because tenant.account_updated is the SUCCESS row internal/domain/tenant writes and a
// prefix match on it does not match this one.
const ActionAccountUpdateRefused = "tenant.account_update_refused"

// refusedAccountDetail is the audit row's payload for a refused save.
//
// 🔴 EXPLICIT EMPTIES, NO omitempty — refusedRemovalDetail's lesson, kept: a key that is
// absent cannot be told apart from a value that was empty, and this row exists precisely
// so somebody can reconstruct what was attempted.
//
// 🔴 NOTHING THAT WAS POSTED IS RECORDED, AND THAT IS A DECISION RATHER THAN AN
// OVERSIGHT. The refused request carried a name, a type and a zone; writing them here
// would let anybody who can POST this route append text of their choosing to a
// tenant-scoped, append-only table that nobody can clean up. What the row carries is the
// ACT, the role that was insufficient and the role required — all of which are the
// actor's own attributes, already known to the tenant.
type refusedAccountDetail struct {
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason"`
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
}

// refuseAccountSave records an attempt the role gate refused.
//
// 🔴 THE SCREEN NEVER OFFERS THIS FORM TO A MANAGER, SO REACHING HERE MEANS THE UI WAS
// BYPASSED — a stale page, a shared link, or a deliberate POST. Both halves are required
// and neither substitutes for the other: withholding the form is the courtesy, this is
// the guarantee.
//
// 🔴 IT IS ITS OWN TRANSACTION (a.record → audit.Record, not RecordTx) BECAUSE THERE IS
// NO OTHER. Nothing is being written — that is the point — so there is no surrounding
// write for the row to share a fate with. That is the opposite of the SUCCESS path,
// where internal/domain/tenant.Save deliberately shares its transaction so a change
// nobody can attribute rolls back.
//
// 🔴 AND A FAILED AUDIT WRITE DOES NOT CHANGE THE ANSWER. The manager still gets the
// refusal sentence, never a 500. This is a RECORDING path, not an authorising one: the
// refusal already happened in the caller, and turning a trail outage into "the panel is
// unavailable" would report OUR failure as a verdict on THEIR request. a.record logs the
// failure loudly and returns (§7: never swallowed).
//
// Bloat is bounded by adminSessionLimit — 300 requests per session per ten minutes, the
// same ceiling every precedent relies on.
func (a *AdminAuth) refuseAccountSave(r *http.Request, id httpx.AdminIdentity) {
	a.log.Warn("panel account save refused: this admin is not an owner",
		"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)

	a.record(r.Context(), audit.Event{
		// §4.5: the tenant is the SESSION's, never the request's.
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionAccountUpdateRefused,
		// THE TARGET IS THE BUSINESS ITSELF, which is what the success row uses — so the
		// two events about one business are found by the same lookup.
		Target: id.TenantID().String(),
		Detail: refusedAccountDetail{
			Outcome:      "refused",
			Reason:       "changing this business's details is reserved for an owner",
			Role:         id.Admin.Role,
			RequiredRole: adminRoleOwner,
		},
	})
}
