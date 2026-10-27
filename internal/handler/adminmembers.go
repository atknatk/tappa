package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// MEMBERSHIP MANAGEMENT (M14-16, ADR 0031 §5.12; K-6, K-11): the people who run this business in the
// panel, listed on the owner's Managers page beside the invitations, and the four presses on them --
// disable, enable, make owner, make manager. An ownership transfer is the last two, in two steps
// (adminauth.Memberships carries the argument).
//
// 🔴 FLAG-GATED, EVERY ROUTE (adminauth.SurfaceMembershipManagement, ADR 0031 §8): closed, the list is
// not drawn and the four POSTs are not mounted -- a path the panel never had, for every method.
//
// 🔴 THE OWNER GATE IS M14-15's, READ RATHER THAN RESTATED: a.mayInvite -- the policy engine's
// policy:edit (sys:policy-edit-owner-only refuses anybody else terminally), with the shipped baseline
// for a business whose rulebook was never materialised (tenant.RuleWriter.MayInviteManager, the
// user's decision of 2026-10-27). Deciding who holds panel authority IS changing the business's
// authorisation, and a new action would meet "deny sid=default" in every materialised rulebook (M7-07's
// trap). The belt is the service's: the actor's row must be an active owner under the owners' lock.
//
// ⚠️ TWO CHAINS, BY WHAT THE PRESS DOES (ADR 0025 §3, its M11 note's K-26 and its M14 note). The two
// presses that NARROW who can act for the business -- disabling a membership and making an owner a
// manager -- are defences: a suspended business that could not shut out a compromised manager is the
// opposite of what a suspension is for, so they hang off protectWritingThroughSuspension beside K-26's
// key revocations. The two that WIDEN it -- enabling and making an owner -- are ordinary writes and a
// suspension stops them, as it stops sending an invitation (M14-15, the user's decision of 2026-10-27).
// Both chains carry the tenant binding (formBindingGate), so a page drawn for another business cannot
// press here.
//
// LOGS: ids and fixed words only -- never the address or the name (CLAUDE.md §7, ADR 0022 §10).

var (
	membersDisableHref     = managersHref + "/disable"
	membersEnableHref      = managersHref + "/enable"
	membersMakeOwnerHref   = managersHref + "/make-owner"
	membersMakeManagerHref = managersHref + "/make-manager"
)

// panelMemberships is what the panel needs of adminauth.Memberships, declared at the consumer.
type panelMemberships interface {
	List(ctx context.Context, tenantID uuid.UUID) ([]adminauth.Member, error)
	Change(ctx context.Context, tenantID, actorID, memberID uuid.UUID, act adminauth.MembershipAct) (adminauth.MembershipChange, error)
}

var _ panelMemberships = (*adminauth.Memberships)(nil)

// AdminMemberships gives the panel the membership service (cmd/tappa always does). Without it the
// gated routes answer 503 and the page draws no list.
func AdminMemberships(s panelMemberships) AdminAuthOption {
	return func(a *AdminAuth) { a.memberships = s }
}

var problemMembersUnavailable = pages.ProblemView{
	Title:   "Managing people is not available",
	Message: "This Taptime was started without its membership service, so nothing here can be changed.",
}

// The redirect's words for the membership presses (joined to managersDone / managersProblem).
var membersDone = map[string]string{
	"disabled":   "That person can no longer sign in to this business. Their other businesses on Taptime are not affected.",
	"enabled":    "That person can sign in to this business again.",
	"made-owner": "That person is now an owner of this business. To hand the business over, step down to manager below.",
	"made-mgr":   "That person is now a manager of this business.",
}

var membersProblem = map[string]string{
	"last-owner":    "A business always keeps at least one active owner, so that was not changed. Make someone else an owner first.",
	"self":          "You cannot disable or enable yourself. To stop being an owner, step down to manager once someone else is an owner.",
	"unchanged":     "That person already was that, so nothing was changed. Reload the page to see the current list.",
	"unknown":       "That person is not part of this business, so nothing was changed.",
	"confirm":       "Tick the box to confirm you are stepping down, then press the button again.",
	"confirm-owner": "Tick the box to confirm you are making that person an owner, then press the button again.",
}

// membersRedirect is the managers page with one of the two closed vocabularies' words.
func membersRedirect(kind, word string) string {
	return managersHref + "?" + kind + "=" + word
}

// membersChange serves the four POSTs.
func (a *AdminAuth) membersChange(act adminauth.MembershipAct) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := httpx.AdminOf(r)
		if a.memberships == nil {
			a.renderProblem(w, r, http.StatusServiceUnavailable, problemMembersUnavailable)
			return
		}
		if !a.readManagersForm(w, r) {
			return
		}
		if !a.mayInvite(r.Context(), id) {
			a.refuseManagers(w, r, id, "membership-"+string(act))
			return
		}
		memberID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("member_id")))
		if err != nil {
			a.redirect(w, managersHref+"?problem=unreadable")
			return
		}
		actor := id.Admin.AdminUserID
		self := memberID == actor
		// Stepping down is the one press that takes the presser's own authority away: it asks for a
		// tick, so a stray tap does not end someone's ownership.
		if act == adminauth.MemberMakeManager && self && r.PostFormValue("confirm") != "yes" {
			a.redirect(w, membersRedirect("problem", "confirm"))
			return
		}
		// Making someone an owner hands them the whole business in one press -- the same tick (the
		// third eye's DÜŞÜK, 2026-10-27).
		if act == adminauth.MemberMakeOwner && r.PostFormValue("confirm") != "yes" {
			a.redirect(w, membersRedirect("problem", "confirm-owner"))
			return
		}
		ch, err := a.memberships.Change(r.Context(), id.TenantID(), actor, memberID, act)
		switch {
		case err == nil:
			a.log.Info("panel membership changed", "act", string(act), "tenant_id", id.TenantID(),
				"actor_id", actor, "member_id", memberID, "revoked_sessions", ch.RevokedSessions)
			if ch.Self {
				// A step-down: the presser is a manager now, and the Managers page is an owner's.
				a.redirect(w, accountReturn("stepped-down", ""))
				return
			}
			a.redirect(w, membersRedirect("done", membersDoneWord(act)))
		case errors.Is(err, adminauth.ErrLastOwner):
			a.log.Warn("panel membership refused: the last active owner", "act", string(act), "actor_id", actor, "member_id", memberID)
			a.redirect(w, membersRedirect("problem", "last-owner"))
		case errors.Is(err, adminauth.ErrMemberSelf):
			a.redirect(w, membersRedirect("problem", "self"))
		case errors.Is(err, adminauth.ErrMemberUnchanged):
			a.redirect(w, membersRedirect("problem", "unchanged"))
		case errors.Is(err, adminauth.ErrMemberUnknown):
			a.log.Warn("panel membership refused: not a membership of this business", "act", string(act), "actor_id", actor, "member_id", memberID)
			a.redirect(w, membersRedirect("problem", "unknown"))
		case errors.Is(err, adminauth.ErrMemberNotOwner):
			a.log.Warn("panel membership refused: the actor's row is not an active owner", "act", string(act), "actor_id", actor)
			a.renderProblem(w, r, http.StatusForbidden, problemManagersOwnerOnly)
		default:
			a.log.Error("panel: could not change a membership", "act", string(act), "err", err)
			a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		}
	}
}

func membersDoneWord(act adminauth.MembershipAct) string {
	switch act {
	case adminauth.MemberDisable:
		return "disabled"
	case adminauth.MemberEnable:
		return "enabled"
	case adminauth.MemberMakeOwner:
		return "made-owner"
	}
	return "made-mgr"
}

// membersView fills the page's membership section: the rows and the presses each one offers. The
// buttons follow the service's rules (no button the service would refuse is drawn), but the service
// decides -- a stale page's press is refused there.
func (a *AdminAuth) membersView(r *http.Request, id httpx.AdminIdentity, v *pages.ManagersView) {
	if !a.identityGate.Open(adminauth.SurfaceMembershipManagement) {
		return
	}
	v.MembersShown = true
	v.DisableHref, v.EnableHref = membersDisableHref, membersEnableHref
	v.MakeOwnerHref, v.MakeManagerHref = membersMakeOwnerHref, membersMakeManagerHref
	v.APIKeysHref = apiKeysHref
	if a.memberships == nil {
		v.MembersFailed = true
		return
	}
	list, err := a.memberships.List(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not read the memberships", "err", err)
		v.MembersFailed = true
		return
	}
	owners := 0
	for _, m := range list {
		if m.Owner() && m.Active() {
			owners++
		}
	}
	actor := id.Admin.AdminUserID
	for _, m := range list {
		row := pages.ManagerMemberRow{
			ID: m.ID.String(), Name: m.FullName, Email: m.Email, Self: m.ID == actor,
			Role: "Manager", Status: "Active",
		}
		if m.Owner() {
			row.Role = "Owner"
		}
		if !m.Active() {
			row.Status = "Disabled"
		}
		switch {
		case row.Self:
			row.CanStepDown = m.Owner() && m.Active() && owners >= 2
			v.OnlyOwner = m.Owner() && m.Active() && owners < 2
		case m.Active():
			row.CanDisable = true
			row.CanMakeOwner = !m.Owner()
			row.CanMakeManager = m.Owner()
		default:
			row.CanEnable = true
		}
		// An owner's integration keys outlive their ownership (backlog T174, ADR 0031 §5.13): until T174
		// is decided, every press that disables or demotes an owner says so beside it.
		row.KeysNote = m.Owner() && (row.CanDisable || row.CanMakeManager || row.CanStepDown)
		v.Members = append(v.Members, row)
	}
}
