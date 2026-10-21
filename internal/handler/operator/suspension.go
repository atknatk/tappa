package operator

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// SUSPENDING A TENANT AND LIFTING THE SUSPENSION (M10 OP-15, phase C; ADR 0025 §5; ADR 0020 §4;
// ADR 0021, OP-15 C annex). The overview (tenants.go) shows whether the tenant is suspended and
// carries ONE form, by its state: POST /operator/tenants/{id}/suspend with a reason of the
// closed set and a ticked box, or POST /operator/tenants/{id}/reinstate with a ticked box. Both
// sit in the console's group (routes.go, mount): host gate, security headers, flood gate,
// same-origin gate (a POST from another origin is 403 before any store call), requireOperator,
// sessionGate.
//
// 🔴 THE ANSWER IS THE SAME WHATEVER THE TENANT HELD (ADR 0021 §2 v 7; ADR 0025 §2 md. 7). The
// two writes return nothing: a tenant that was not suspended, one that was, and an id no tenant
// has are each a 303 to the overview's path, and the overview -- a fresh, audited read -- says
// what the database holds (for an unknown id, its 404). There is no "already suspended" and no
// "nothing changed": this file never reads the state it is about to write.
//
// 🔴 THE TENANT IS THE PATH'S, AND THE REASON IS A CODE OF THE CLOSED SET. The id is read from
// the path (tenantID: the 36-character hyphenated form) and from nowhere else -- an id posted in
// the body or the query is never read. The reason is one of db.SuspensionReasons, exactly as
// written (db.SuspensionReason.Known: no trimming, no case folding); anything else is a 400
// before any store call. There is no free text (ADR 0025 §1): the tenant reads the reason's
// sentence and its code enters the tenant's append-only audit_log.
//
// 🔴 THE BOX IS NOT A CSRF DEFENCE (the orchestrator's K15-10). It is against a wrong click:
// a form whose confirm field is not exactly suspensionConfirmed is a 400 before any store call.
// The forgery defence is sameOriginGate(true) and the session cookie's SameSite=Strict.
//
// 🔴 A WRITE RUNS TO ITS END WHATEVER THE CLIENT DOES -- AND IS BOUNDED (OP-14 E's answered; the
// VAT re-check's precedent, vat.go). Past the id check each POST runs on answered(r): the form's
// refusals and the fault page are rendered whatever the client did, and the write's context is
// context.WithoutCancel's, spelled out again where it is made, with suspensionWriteTimeout of its
// own -- the one bound of the request past its session gate (SuspensionWriteWorstCase), which
// fits inside the HTTP drain on shutdown (cmd/tappa's
// TestShutdownBudget_TheSuspensionWriteNestsInsideTheHTTPGrace). A browser that closes the tab
// after pressing the button still has the suspension written. The id's 404 follows the client,
// as on every console route.
//
// 🔴 A FAULT IS "NOT CONFIRMED", NEVER "NOT SAVED" (ADR 0025 §5). The write is one autocommit
// statement: an error can arrive after the COMMIT went through (a connection lost on the way
// back, the bound running out in the reply). So the 503 page does not say nothing changed -- it
// sends the operator to the overview, which says what the database holds.
//
// WHAT REACHES THE PROCESS LOG: the tenant's id, the operator's id, the reason's code (a member
// of the closed set) on success; the tenant's id and internal/db's error -- the call and a
// SQLSTATE (operatorErr) -- on a fault. No posted value, no session hash; a refused form writes
// no line. Measured: TestSuspension_NoPostedValueReachesALogLine.

// SuspensionStore is the operator database's slice the overview's two writes need, declared at
// the consumer (CLAUDE.md §7). *db.OperatorDB implements it by delegating to internal/db's
// SuspendTenant and ReinstateTenant (TestOperatorDB_EveryMethodDelegatesVerbatim), and that
// type's method set is derived from this interface and the other stores
// (TestOperatorDB_IsTheStoreAndNothingMore). sessionHash is operatorauth's SessionHash of the
// request's token -- on ADR 0020 §5's never-log list. Neither method answers with a state.
type SuspensionStore interface {
	// SuspendTenant is op_suspend_tenant: for a tenant that exists, its suspension (the first
	// suspension's time kept, the reason replaced) and its own audit_log row; on every accepted
	// call the operator's 'tenant_suspended' row. nil whatever the tenant held -- an unknown id
	// included; a dead session is db.ErrOperatorRefused; a reason outside the closed set is
	// db.ErrSuspensionReasonRefused before a round trip.
	SuspendTenant(ctx context.Context, sessionHash string, tenantID uuid.UUID, reason db.SuspensionReason) error
	// ReinstateTenant is op_reinstate_tenant: for a tenant that exists, both columns cleared and
	// its own audit_log row; on every accepted call the operator's 'tenant_reinstated' row. nil
	// whatever the tenant held; a dead session is db.ErrOperatorRefused.
	ReinstateTenant(ctx context.Context, sessionHash string, tenantID uuid.UUID) error
}

// suspensionWriteTimeout bounds either write: one statement through op_suspend_tenant or
// op_reinstate_tenant -- legalWriteTimeout's reasoning and number. What it bounds on a reachable
// database is a wait for the tenant's row lock (ADR 0025 limit 5: the body locks the row first,
// and the repository sets no lock or statement timeout for this role) or a stalled connection.
const suspensionWriteTimeout = 10 * time.Second

// SuspensionWriteWorstCase is the longest a POST /operator/tenants/{id}/suspend or …/reinstate
// runs past its session gate: the form is read from a body bounded by maxFormBytes, and the one
// write runs on suspensionWriteTimeout, detached from the client. cmd/tappa holds it inside the
// HTTP drain on shutdown (TestShutdownBudget_TheSuspensionWriteNestsInsideTheHTTPGrace), so a
// write in flight at a deploy finishes before the process stops serving.
const SuspensionWriteWorstCase = suspensionWriteTimeout

// suspensionConfirmed is the confirm box's value: the form's checkbox posts it when ticked, and a
// form without exactly this value writes nothing.
const suspensionConfirmed = "yes"

// suspensionReasonWords is the operator's label for each reason -- the fifth copy of the closed set
// (ADR 0025 §1, "Kopyalar"): one entry for each of db.SuspensionReasons, no other.
// TestSuspensionWords_NameEveryReasonAndNothingElse holds the keys equal to that list, and internal/db's
// TestOperator00063_TheReasonSetIsOneSetInEveryCopy reads them from this file's source beside
// the CHECK, the definer's refusal and the Go list. The labels name the reason plainly: the
// operator chose it. (The tenant's banner says a neutral sentence for security_hold and
// customer_request -- the panel's map, phase B; this screen tells the operator so.)
var suspensionReasonWords = map[db.SuspensionReason]string{
	db.SuspensionNonPayment:      "Non-payment",
	db.SuspensionTermsBreach:     "Breach of the terms",
	db.SuspensionSecurityHold:    "Security hold",
	db.SuspensionCustomerRequest: "At the business's request",
}

// suspensionReasonWord is a stored reason as the overview says it: its label, or -- for a code
// this build does not name (a later migration's, or an empty one) -- an unrecognised value whose
// raw text is printed only when it has the shape of a closed set's member (unknownWord; the audit
// log's rule).
func suspensionReasonWord(r db.SuspensionReason) operatorpages.AuditWord {
	if w, ok := suspensionReasonWords[r]; ok {
		return operatorpages.AuditWord{Text: w}
	}
	return unknownWord(string(r))
}

// suspensionView is the overview's suspension block: the state, read from the overview's own row,
// and the one form that state calls for.
func suspensionView(o db.TenantOverview) operatorpages.TenantSuspensionView {
	v := operatorpages.TenantSuspensionView{
		SuspendPath:   suspendPath(o.ID),
		ReinstatePath: reinstatePath(o.ID),
	}
	if o.SuspendedAt != nil {
		v.Suspended = true
		v.Since = utcStamp(*o.SuspendedAt)
		v.Reason = suspensionReasonWord(o.SuspensionReason)
	}
	for _, r := range db.SuspensionReasons() {
		v.Reasons = append(v.Reasons, operatorpages.SuspensionReasonOption{
			Value: string(r), Label: suspensionReasonWords[r], Hold: r == db.SuspensionSecurityHold,
		})
	}
	return v
}

// suspendTenant is POST /operator/tenants/{id}/suspend.
//
// The order, and what each refusal costs (none writes anything):
//
//	the session (storeSession)                       -> the sign-in
//	an id that is not the hyphenated form            -> 404, no store call
//	--- from here on the request runs on answered(r) ---
//	a body over maxFormBytes, or one that does not parse -> 413 / 400, no store call
//	the box not ticked (confirm != suspensionConfirmed) -> 400, no store call
//	a reason outside the closed set                  -> 400, no store call
//	the write: a dead session                        -> the sign-in
//	the write: any other error                       -> 503, "not confirmed"
//	the write accepted                               -> 303 to the overview (POST -> 303 -> GET)
//
// The request is one unit of the session's request budget (sessionGate) and no unit of its read
// budget: it reads nothing. The overview the 303 lands on is a read (surface.go, readLimit).
func (s *Surface) suspendTenant(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	r = answered(r)
	if !s.readForm(w, r, maxFormBytes, problemSuspensionFormTooLarge) {
		return
	}
	if r.PostForm.Get("confirm") != suspensionConfirmed {
		s.problem(w, r, http.StatusBadRequest, problemSuspensionUnconfirmed)
		return
	}
	reason := db.SuspensionReason(r.PostForm.Get("reason"))
	if !reason.Known() {
		s.problem(w, r, http.StatusBadRequest, problemSuspensionReasonRefused)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), suspensionWriteTimeout)
	defer cancel()
	err := s.suspensionStore.SuspendTenant(ctx, hash, tenant, reason)
	if !s.suspensionWritten(w, r, tenant, err, "operator: a tenant's suspension could not be confirmed", problemSuspendNotConfirmed) {
		return
	}
	s.log.InfoContext(r.Context(), "operator suspended a tenant",
		"tenant_id", tenant.String(), "admin_id", id.AdminID.String(), "reason", string(reason))
	s.redirect(w, overviewPath(tenant))
}

// reinstateTenant is POST /operator/tenants/{id}/reinstate: suspendTenant's order with no reason.
func (s *Surface) reinstateTenant(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	r = answered(r)
	if !s.readForm(w, r, maxFormBytes, problemSuspensionFormTooLarge) {
		return
	}
	if r.PostForm.Get("confirm") != suspensionConfirmed {
		s.problem(w, r, http.StatusBadRequest, problemReinstateUnconfirmed)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), suspensionWriteTimeout)
	defer cancel()
	err := s.suspensionStore.ReinstateTenant(ctx, hash, tenant)
	if !s.suspensionWritten(w, r, tenant, err, "operator: a tenant's reinstatement could not be confirmed", problemReinstateNotConfirmed) {
		return
	}
	s.log.InfoContext(r.Context(), "operator reinstated a tenant",
		"tenant_id", tenant.String(), "admin_id", id.AdminID.String())
	s.redirect(w, overviewPath(tenant))
}

// suspensionWritten answers a write's error and reports whether the write was accepted. A dead
// session is the sign-in, as the gate would answer (the session ended between the gate and the
// write). Any other error -- 22023 among them (a refusal the form's checks make unreachable),
// a deadline, a lost connection -- is a 503 whose page does not claim nothing changed, and an
// ERROR line naming the tenant and internal/db's error (the call and a SQLSTATE); the session
// hash is not an argument of it.
func (s *Surface) suspensionWritten(w http.ResponseWriter, r *http.Request, tenant uuid.UUID, err error, line string,
	page operatorpages.ProblemView) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
	default:
		s.log.ErrorContext(r.Context(), line, "tenant_id", tenant.String(), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, page)
	}
	return false
}

// overviewPath is a tenant's overview, /operator/tenants/<id> -- where both writes send the
// browser (POST -> 303 -> GET).
func overviewPath(tenant uuid.UUID) string { return pathTenants + "/" + tenant.String() }

// suspendPath and reinstatePath are the overview's two forms' actions.
func suspendPath(tenant uuid.UUID) string   { return overviewPath(tenant) + "/suspend" }
func reinstatePath(tenant uuid.UUID) string { return overviewPath(tenant) + "/reinstate" }
