package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

// THE EMPLOYEES SECTION'S ADDRESS CHANGE — M10 EM-6 (ADR 0022 §7 and its EM-6 note).
// One act: write, replace or remove the address on file for one person, which retires
// that person's unspent activation links in the same transaction
// (internal/domain/tenant/staffemail.go). Nothing is sent; e-mailing an invitation is
// EM-7's.
//
// 🔴 THE ROUTE MOUNTS ProtectWriting AND THE ORDER IS THE POINT — mountWriting's
// shared chain, floodGate → sameOriginGate → requireAdmin → sessionGate: a
// cross-origin POST is refused before any database work.
//
// 🔴 IT IS OWNER-ONLY, AND THE REASON IS WHERE THE ADDRESS LEADS, NOT WHAT IT IS.
// Every other act on a person (add, invite, deactivate, move) is open to any admin,
// and this one is not, because in e-mail delivery mode (EM-7) the address IS where an
// activation link — a credential that signs a phone in as that person — is sent.
// Letting a manager point an EXISTING employee's address at a mailbox of their own
// would let them receive that person's next link: ADR 0005 Y-D (a manager taking over
// an employee's identity) in its e-mail form. ADR 0022 K3 already reserves the one
// other way to put a link in front of an admin in that mode — the owner-only "show
// the link" fallback — to the owner, and this follows it. Two counted facts:
//
//	a manager can still ADD somebody with any address (M6-13, unchanged) — that is the
//	  "one mailbox, N invented employees" residue ADR 0022's numbered limit 2 already
//	  names, not a takeover of somebody who exists;
//	in panel delivery mode (today's shipped configuration) a manager can re-invite and
//	  read the link on screen anyway, and every admin in the product today is an
//	  owner (ADR 0022 B28), so this gate refuses no request a real customer makes until
//	  EM-12 brings managers — the same position as K3's fallback. The tests build a
//	  manager session with a fixture.
//
// 🔴 A REFUSED ATTEMPT WRITES ONE TRAIL ROW, following the precedent the account and
// removal gates settled on (user decision, 2026-08-09): the person who most needs to
// know that a manager has been trying to change an employee's address is the owner.
//
// 🔴 §4.7 / R7b — THE ADDRESS IS NEVER LOGGED OR TRAILED, here or in the domain. No
// log call in this file names the posted field. The success row's detail is two
// booleans and a count (had_email, has_email, retired_invitations — written by
// internal/domain/tenant); the refusal row's is four fixed text fields (outcome,
// reason, role, required_role) whose values are constants or the actor's own role.
// Neither carries an address or any posted value.

// ActionEmployeeEmailChangeRefused records an attempt the role gate refused. It takes
// the `<subject>.<verb>_<refusal>` shape the trail uses for acts that did NOT happen
// (location.delete_refused, tenant.account_update_refused), so a reader scanning
// `action LIKE 'employee.email%'` tells it from the success row
// (tenant.ActionEmployeeEmailChanged) at a glance.
const ActionEmployeeEmailChangeRefused = "employee.email_change_refused"

// mayChangeEmployeeEmail reports whether this session's admin may change an address.
// The role comes from the RESOLVED SESSION (adminauth.Resolved.Role, read from
// admin_users during resolution) — not a cookie claim, not a form field, no extra
// query. The card uses the same predicate to decide whether to render the form.
func mayChangeEmployeeEmail(id httpx.AdminIdentity) bool {
	return id.Live() && id.Admin.Role == adminRoleOwner
}

// employeeEmail writes, replaces or removes the address on file.
//
// WHERE EACH OUTCOME GOES (EM-6 round 2: this sentence used to say "every outcome is a
// 303 back to the person's card", which the code does not do):
//
//	success, and the refusals bad-email,      303 to the person's card
//	  email-taken, same-email, not-permitted
//	ErrUnknownEmployee                        303 to the roster, no card (problem=unknown)
//	a body ParseForm refuses, or an id that   303 to the roster, no card
//	  is not a uuid (readAction)                (problem=unreadable)
//	any other failure                         500, the WRITER's problem page
//	                                            (problemPanelWriteFailed)
//
// On none of them is the typed address echoed back. That is a cost, stated: a manager
// who mistypes retypes one field. The alternative — carrying the value in the redirect — would put
// a person's address in the address bar, the history and any Referer, which is the
// disclosure the roster's paging cursor was changed to stop (GetRosterCursorAnchor).
// The add form re-renders inline instead because it has five fields to lose; this
// form has one.
func (a *AdminAuth) employeeEmail(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f, employeeID, ok := a.readAction(w, r)
	if !ok {
		return
	}
	// 🔴 THE ROLE IS CHECKED BEFORE ANY OF THIS HANDLER'S DATABASE WORK (the session was
	// resolved by the chain before it), so a refused request costs the trail row and
	// no call on the staff surface — no Person read, no address read, no change; the
	// manager arm of TestEmployeeEmail_OnlyAnOwnerReachesTheDomain counts all three.
	// The order the account and removal gates use.
	if !mayChangeEmployeeEmail(id) {
		a.refuseEmailChange(r, id, employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "not-permitted"))
		return
	}

	change, err := a.staff.ChangeEmail(r.Context(), tenant.EmailCommand{
		TenantID:   id.TenantID(),
		EmployeeID: employeeID,
		// 🔴 THE ACTOR COMES FROM THE SESSION; there is no field for it on the form.
		ActorID: id.Admin.AdminUserID,
		Email:   r.PostFormValue("email"),
	})
	switch {
	case err == nil:
		a.log.Info("panel employee email changed",
			"employee_id", employeeID, "actor_id", id.Admin.AdminUserID,
			"has_email", change.HasEmail, "retired_invitations", change.Retired)
		a.redirect(w, rosterReturn(f, employeeID, "email", ""))
	case errors.Is(err, tenant.ErrEmployeeEmail):
		a.log.Warn("panel employee email refused: the address breaks the recipient rule",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "bad-email"))
	case errors.Is(err, tenant.ErrEmailTaken):
		a.log.Warn("panel employee email refused: that address is already used in this tenant",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "email-taken"))
	case errors.Is(err, tenant.ErrSameEmail):
		// §4.6 in its quietest form, ErrSamePlacement's: nothing was written, no link was
		// retired and no trail row added, so the screen must not say "changed".
		a.log.Info("panel employee email: the address was already the requested one",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "same-email"))
	case errors.Is(err, tenant.ErrUnknownEmployee):
		a.log.Warn("panel employee email refused: no such employee in this tenant")
		a.redirect(w, rosterReturn(f, uuid.Nil, "", "unknown"))
	default:
		// A real failure is a real failure: the screen says the panel failed rather than
		// inventing a refusal, and it says so to somebody who was WRITING (EM-6 round 3 —
		// the reader's page told them "this page is not showing anything", which is not
		// what they had done).
		//
		// ✅ BOTH CLAUSES OF THAT PAGE ARE MEASURED FOR THIS ROUTE, not borrowed from the
		// manual-entry path the page was written for. "Nothing was written":
		// TestEmployeeEmailDB_AFailedChangeWritesNothingAndARetryWritesOnce breaks the
		// trail half of the change — after reading, through the change's own
		// transaction, that the address and the retirement are already in it — and
		// counts the address, the activation link and the trail rows after the 500.
		// "Pressing again will not enter it twice": the same test presses again — the
		// retry writes once — and once more, which ChangeEmail refuses as ErrSameEmail
		// and rolls back, step 1's retirement included (that UPDATE runs before the
		// comparison; the refusal undoes it rather than preceding it).
		//
		// A DEADLOCK (40P01) LANDS HERE TOO and is told the same thing: Postgres aborts
		// the whole transaction, so nothing was written. The handler half of that is a
		// row of TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress; a
		// real deadlock against the database is not measured (ADR 0022, EM-6 limit 2).
		a.log.Error("panel: could not change the employee's address", "err", err, "employee_id", employeeID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// refusedEmailChangeDetail is the trail row's payload for a refused change.
//
// 🔴 NOTHING THAT WAS POSTED IS RECORDED — refusedAccountDetail's rule: the refused
// request carried an address, and writing it here would put a person's address (or
// any text a caller chooses) into an append-only table nobody can clean. What the row
// carries is the act, the role that was insufficient and the role required. Explicit
// values, no omitempty.
type refusedEmailChangeDetail struct {
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason"`
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
}

// refuseEmailChange records an attempt the role gate refused.
//
// 🔴 THE SCREEN NEVER OFFERS THIS FORM TO A MANAGER, so reaching here means the UI
// was bypassed — a stale page, a shared link, or a deliberate POST. Withholding the
// form is the courtesy; this is the guarantee.
//
// IT IS ITS OWN TRANSACTION (a.record → audit.Record) because nothing else is
// written, and a failed trail write does not change the answer: the refusal already
// happened, and turning a trail outage into "the panel is unavailable" would report
// OUR failure as a verdict on THEIR request. a.record logs the failure and returns.
// Volume is bounded by adminSessionLimit, as for every precedent.
func (a *AdminAuth) refuseEmailChange(r *http.Request, id httpx.AdminIdentity, employeeID uuid.UUID) {
	a.log.Warn("panel employee email refused: this admin is not an owner",
		"employee_id", employeeID, "actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
	a.record(r.Context(), audit.Event{
		// §4.5: the tenant is the SESSION's, never the request's.
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionEmployeeEmailChangeRefused,
		// THE TARGET IS THE PERSON, which is what the success row uses — so both
		// events about one person are found by the same lookup. The id is the posted
		// one and is NOT checked against the roster first: checking would be a
		// database read on a path that must cost nothing, and a foreign or invented id
		// in this tenant's own trail names nobody of another tenant.
		Target: employeeID.String(),
		Detail: refusedEmailChangeDetail{
			Outcome:      "refused",
			Reason:       "changing an employee's address is reserved for an owner",
			Role:         id.Admin.Role,
			RequiredRole: adminRoleOwner,
		},
	})
}
