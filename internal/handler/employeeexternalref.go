package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

// THE EMPLOYEES SECTION'S EXTERNAL-REF CHANGE — M11 API-1 (ADR 0028 §9, K-18). One act:
// bind one person to the identifier the customer's HR/ERP system uses for them, or
// unbind them. Before the integration API is switched on, the owner binds the people
// who already exist, so the first sync updates them rather than adding them again.
//
// It is employeeEmail's shape, route for route: ProtectWriting (mountWriting's chain —
// the Origin check runs before the resolver), OWNER-ONLY with a trail row for a refused
// attempt, every outcome a 303 to the person's card with a closed word. The reason it is
// owner-only is the email's: once the API is on, the integrator's writes for a ref land
// on whoever holds it — a rename, a new address and with it the next activation link, a
// deactivation — so binding a ref to the wrong person is ADR 0005 Y-D in another form.
//
// The ref is not personal data in the §7 sense (it is the customer's opaque record key)
// and the success trail row carries it before and after; the process log carries only
// whether one is on file now.

// ActionEmployeeExternalRefChangeRefused records an attempt the role gate refused, in the
// `<subject>.<verb>_<refusal>` shape of ActionEmployeeEmailChangeRefused.
const ActionEmployeeExternalRefChangeRefused = "employee.external_ref_change_refused"

// mayChangeExternalRef reports whether this session's admin may bind or unbind an
// external ref. It is mayChangeEmployeeEmail's predicate on purpose: the two routes guard
// the same kind of takeover. The card uses it to decide whether to render the form.
func mayChangeExternalRef(id httpx.AdminIdentity) bool {
	return mayChangeEmployeeEmail(id)
}

// employeeExternalRef binds, replaces or removes the external ref on file.
//
//	success                                   303 to the person's card (done=external-ref)
//	ErrExternalRef / ErrExternalRefTaken /    303 to the person's card with the refusal
//	  ErrSameExternalRef /                      word
//	  ErrExternalRefRequired / not an owner
//	ErrUnknownEmployee                        303 to the roster, no card (problem=unknown)
//	an unreadable body or id                  303 to the roster (problem=unreadable)
//	any other failure                         500, the writer's problem page
func (a *AdminAuth) employeeExternalRef(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f, employeeID, ok := a.readAction(w, r)
	if !ok {
		return
	}
	// THE ROLE IS CHECKED BEFORE ANY OF THIS HANDLER'S DATABASE WORK, the email gate's
	// order: a refused request costs the trail row and nothing on the staff surface.
	if !mayChangeExternalRef(id) {
		a.refuseExternalRefChange(r, id, employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "external-ref-not-permitted"))
		return
	}

	change, err := a.staff.ChangeExternalRef(r.Context(), tenant.ExternalRefCommand{
		TenantID:  id.TenantID(),
		SubjectID: employeeID,
		// THE ACTOR COMES FROM THE SESSION; there is no field for it on the form.
		ActorID:     id.Admin.AdminUserID,
		ExternalRef: r.PostFormValue("external_ref"),
	})
	switch {
	case err == nil:
		a.log.Info("panel employee external ref changed",
			"employee_id", employeeID, "actor_id", id.Admin.AdminUserID,
			"has_external_ref", change.To != "")
		a.redirect(w, rosterReturn(f, employeeID, "external-ref", ""))
	case errors.Is(err, tenant.ErrExternalRef):
		a.log.Warn("panel employee external ref refused: not the shape the integration carries",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "bad-external-ref"))
	case errors.Is(err, tenant.ErrExternalRefTaken):
		a.log.Warn("panel employee external ref refused: another live person holds it",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "external-ref-taken"))
	case errors.Is(err, tenant.ErrSameExternalRef):
		a.log.Info("panel employee external ref: already the one on file",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "same-external-ref"))
	case errors.Is(err, tenant.ErrExternalRefRequired):
		a.log.Warn("panel employee external ref refused: the person has no home venue",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "external-ref-required"))
	case errors.Is(err, tenant.ErrUnknownEmployee):
		a.log.Warn("panel employee external ref refused: no such employee in this tenant")
		a.redirect(w, rosterReturn(f, uuid.Nil, "", "unknown"))
	default:
		a.log.Error("panel: could not change the employee's external ref", "err", err,
			"employee_id", employeeID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// refusedExternalRefChangeDetail is refusedEmailChangeDetail's shape: the act, the role
// that was insufficient and the role required — nothing that was posted.
type refusedExternalRefChangeDetail struct {
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason"`
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
}

// refuseExternalRefChange records an attempt the role gate refused, in its own
// transaction (refuseEmailChange's reasoning: nothing else is written, and a trail
// outage does not change the answer).
func (a *AdminAuth) refuseExternalRefChange(r *http.Request, id httpx.AdminIdentity, employeeID uuid.UUID) {
	a.log.Warn("panel employee external ref refused: this admin is not an owner",
		"employee_id", employeeID, "actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
	a.record(r.Context(), audit.Event{
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionEmployeeExternalRefChangeRefused,
		Target:   employeeID.String(),
		Detail: refusedExternalRefChangeDetail{
			Outcome:      "refused",
			Reason:       "changing an employee's external ref is reserved for an owner",
			Role:         id.Admin.Role,
			RequiredRole: adminRoleOwner,
		},
	})
}
