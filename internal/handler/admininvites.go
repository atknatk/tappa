package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/email"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE MANAGER INVITATION (M14-15, ADR 0031 §5.11; M7-07's six criteria in the identity model).
//
// 🔴 FLAG-GATED, EVERY ROUTE. The owner's page and its two writes, and the invitee's page and its
// press, exist only while TAPPA_ADMIN_IDENTITY is on (mountGated, adminauth.SurfaceAdminInvites --
// closed, each is a path the panel never had: 404 for every method). With the flag off an invitee
// would sign in through the legacy arm and M7-07's "resolve_admin_by_email window" trap would be back
// (ADR 0031 §5.11, R12).
//
// THE OWNER'S SIDE (/admin/account/managers): the page and its two writes are the owner's alone, and
// the gate is the POLICY ENGINE's -- a.scribe.May, the very question the policies section asks before
// a rulebook is changed (policy:edit: sys:policy-edit-owner-only refuses anybody else TERMINALLY, the
// stored base:authz-owner grants the owner). Granting panel authority to a new person IS changing the
// business's authorisation, and the action already in the closed vocabulary avoids M7-07's trap: a NEW
// action would meet "deny sid=default" in every business whose baseline was materialised before it
// shipped (billingactions.go measured that shape) -- the owner locked out. The cost, stated: an owner
// whose business's rulebook has never been set up (no tap yet) or cannot be read is refused, as on
// the policies screen (checkin.Service.StoredSet's header), and the page says so. The two writes are
// in mountWriting's MAIN group (ProtectWriting): ADR 0025's closed list of suspension exceptions names
// no invitation, so a suspended business invites nobody. Each form carries the tenant binding (§5.5).
//
// 🔴 R18 -- THE OWNER LEARNS NOTHING ABOUT THE ADDRESS. The send path reads no identity and no
// membership (adminauth.AdminInvites.Issue): one redirect, one row state, one e-mail for every
// address. Every refusal on it is a fact about the SERVER (no mail), the OWNER (the policy, the
// per-mailbox cap of THIS business -- EM-7C's (tenant_id, mailbox) bucket) or the form, never about
// whether the address has a Taptime sign-in.
//
// THE INVITEE'S SIDE (/admin/invite?t=…, under the panel cookie's path so a session reaches the
// press): GET shows the page and consumes nothing (R19); POST accepts -- with the session the browser
// holds (identity_accept_invite: the identity is found through that cookie and nothing else, and its
// address must be the invitation's) or with a password the person chooses now (identity_accept_invite_
// new: only for an address no identity carries). Neither gives a session. Origin, a synchronizer cookie
// and a per-address budget stand before the bcrypt.
//
// LOGS: ids and fixed words only -- never the address, the name, the token or the link (CLAUDE.md §7,
// ADR 0022 §10).

var (
	managersHref       = accountHref + "/managers"
	managersInviteHref = managersHref + "/invite"
	managersCancelHref = managersHref + "/cancel"
)

// adminInvitePath is the invitee's page: adminauth.InvitePath, the link's own path, written out so the
// route scans can read it (TestAdminInvite_ThePathIsTheLinksPath holds the two together).
const adminInvitePath = "/admin/invite"

const (
	adminInviteCookieName   = "tappa_admin_invite"
	adminInviteCookieMaxAge = 30 * 60
	adminInviteMaxFormBytes = 8 << 10
	// adminInviteAcceptLimit bounds the acceptance presses per address: each "new" press may cost a
	// cost-12 bcrypt. Twenty in fifteen minutes is generous for a person and cheap for the server.
	adminInviteAcceptLimit  = 20
	adminInviteAcceptPeriod = 15 * time.Minute
)

// The audit vocabulary the handler writes -- every issued invitation gets exactly one of the first two,
// once the e-mail's fate is known (the acceptance's row is the definer's, admin.invite.accepted).
const (
	ActionAdminInviteSent        = "admin.invite.sent"
	ActionAdminInviteUndelivered = "admin.invite.undelivered"
	ActionAdminInviteCancelled   = "admin.invite.cancelled"
)

// panelAdminInvites is what the panel needs of adminauth.AdminInvites, declared at the consumer.
type panelAdminInvites interface {
	Issue(ctx context.Context, tenantID, invitedBy uuid.UUID, email, fullName string) (adminauth.IssuedInvite, error)
	List(ctx context.Context, tenantID uuid.UUID) ([]adminauth.AdminInvite, error)
	Cancel(ctx context.Context, tenantID, inviteID uuid.UUID) (bool, error)
	Usable(ctx context.Context, t adminauth.InviteToken) error
	AcceptWithSession(ctx context.Context, t adminauth.InviteToken, session adminauth.Token) (db.AcceptedAdminInvite, error)
	AcceptNew(ctx context.Context, t adminauth.InviteToken, password string) (db.AcceptedAdminInvite, error)
}

var _ panelAdminInvites = (*adminauth.AdminInvites)(nil)

// AdminInvitations gives the panel the invitation service (cmd/tappa always does). Without it the
// gated routes answer 503 rather than pretend.
func AdminInvitations(s panelAdminInvites) AdminAuthOption {
	return func(a *AdminAuth) { a.adminInvites = s }
}

var (
	problemManagersOwnerOnly = pages.ProblemView{
		Title:   "Managers are invited by an owner",
		Message: "Only an owner of this business can invite a manager or see the invitations.",
		Hint:    "Nothing is wrong with your account. Ask an owner of the business if someone needs access.",
	}
	problemManagersNotSetUp = pages.ProblemView{
		Title:   "Invitations are not available for this business yet",
		Message: "Who may invite a manager is decided by the business's rules, and they are not set up yet or cannot be read right now.",
		Hint:    "The rules are set up with the first check-in at one of your plaques; the Policies section shows their state.",
	}
	problemInvitesUnavailable = pages.ProblemView{
		Title:   "Invitations are not available",
		Message: "This Taptime was started without its invitation service, so nothing here can be sent or accepted.",
	}
	problemInviteServer = pages.ProblemView{
		Title:   "Something went wrong on our side",
		Message: "Nothing was accepted and nothing was changed.",
		Hint:    "Try again in a minute.",
	}
	problemInviteRestart = pages.ProblemView{
		Title:   "That step expired",
		Message: "This page is only valid for a little while.",
		Hint:    "Open the link from your email again.",
	}
	problemInviteTooMany = pages.ProblemView{
		Title:   "Too many attempts",
		Message: "We have stopped accepting invitation presses from here for a few minutes.",
		Hint:    "Wait a little and try again.",
	}
)

// managersDone / managersProblem: the redirect's word -> the page's sentence. A word not in the table
// is ignored (oneOfWords), so a crafted URL prints nothing of its own.
var managersDone = map[string]string{
	"sent":      "The invitation is on its way. The link in it works once, for three days.",
	"cancelled": "The invitation was cancelled. Its link no longer works.",
}

var managersProblem = map[string]string{
	"unreadable":  "The form could not be read. Please try again.",
	"missing":     "Enter the manager's name and email address.",
	"address":     "That does not look like an email address and a name we can use. Check both and try again.",
	"no-mail":     "This Taptime does not send invitation emails, so nothing was sent.",
	"capped":      "This business has sent that address several emails in the last hour. Wait an hour and try again.",
	"mail-down":   "Our email service is not accepting messages right now, so nothing was sent. Try again in a few minutes.",
	"undelivered": "The invitation email could not be sent, so the invitation was cancelled. Try again in a few minutes.",
	"busy":        "Another invitation to that address was being sent at the same moment. Reload the page to see it.",
	"not-open":    "That invitation is no longer open, so there was nothing to cancel.",
}

func managersWords(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// mayInvite is the owner gate: the policy engine's policy:edit, asked of the stored rulebook without
// materialising it (tenant.RuleWriter.May).
func (a *AdminAuth) mayInvite(ctx context.Context, id httpx.AdminIdentity) bool {
	return id.Live() && a.scribe.May(ctx, id.TenantID(), tenant.Actor{ID: id.Admin.AdminUserID, Role: id.Admin.Role})
}

// refuseManagers answers a refused gate: an owner the engine refused (no usable rulebook) gets the
// sentence that says so; anybody else the owner-only page.
func (a *AdminAuth) refuseManagers(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, act string) {
	a.log.Warn("panel managers refused by the policy engine", "act", act, "actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
	if id.Live() && id.Admin.Role == adminRoleOwner {
		a.renderProblem(w, r, http.StatusForbidden, problemManagersNotSetUp)
		return
	}
	a.renderProblem(w, r, http.StatusForbidden, problemManagersOwnerOnly)
}

// managersPage serves GET managersHref.
func (a *AdminAuth) managersPage(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if a.adminInvites == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemInvitesUnavailable)
		return
	}
	if !a.mayInvite(r.Context(), id) {
		a.refuseManagers(w, r, id, "list")
		return
	}
	v := pages.ManagersView{
		PanelChrome:   a.chrome(r, pages.TabAccount),
		CanSend:       a.inviteMail != nil,
		InviteHref:    managersInviteHref,
		CancelHref:    managersCancelHref,
		NameMaxLength: adminauth.MaxInviteNameRunes,
	}
	v.Kicker = "Owner only · who else runs this business in Taptime"
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("done")), managersWords(managersDone)...); word != "" {
		v.Done = managersDone[word]
	}
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("problem")), managersWords(managersProblem)...); word != "" {
		v.Problem = managersProblem[word]
	}
	list, err := a.adminInvites.List(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not read the invitations", "err", err)
		v.ListFailed = true
	}
	zone := time.UTC
	if s, err := a.accounts.Settings(r.Context(), id.TenantID()); err == nil {
		zone = a.accountZone(s.Timezone)
	}
	now := time.Now()
	for _, inv := range list {
		v.Invites = append(v.Invites, pages.ManagerInviteRow{
			ID: inv.ID.String(), Email: inv.Email, Name: inv.FullName,
			State:  inviteStateWord(inv.State(now)),
			Open:   inv.State(now) == adminauth.InviteStateOpen,
			SentOn: inv.CreatedAt.In(zone).Format("2 Jan 2006 15:04"),
			EndsOn: inv.ExpiresAt.In(zone).Format("2 Jan 2006 15:04"),
		})
	}
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminManagers(v))
}

func inviteStateWord(s string) string {
	switch s {
	case adminauth.InviteStateAccepted:
		return "Accepted"
	case adminauth.InviteStateCancelled:
		return "Cancelled"
	case adminauth.InviteStateExpired:
		return "Expired"
	}
	return "Open"
}

// readManagersForm bounds and parses the body; false when it has answered.
func (a *AdminAuth) readManagersForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, adminInviteMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		a.log.Warn("panel managers write refused: the form could not be read")
		a.redirect(w, managersHref+"?problem=unreadable")
		return false
	}
	return true
}

// managersInvite serves POST managersInviteHref: issue, mail, answer -- the same redirect for every
// address that reaches the send (R18).
func (a *AdminAuth) managersInvite(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if a.adminInvites == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemInvitesUnavailable)
		return
	}
	if !a.readManagersForm(w, r) {
		return
	}
	if !a.mayInvite(r.Context(), id) {
		a.refuseManagers(w, r, id, "invite")
		return
	}
	if a.inviteMail == nil {
		a.redirect(w, managersHref+"?problem=no-mail")
		return
	}
	address := strings.TrimSpace(r.PostFormValue("email"))
	name := strings.TrimSpace(r.PostFormValue("full_name"))
	if address == "" || name == "" {
		a.redirect(w, managersHref+"?problem=missing")
		return
	}
	tenantID := id.TenantID()
	// Both ceilings asked BEFORE anything is minted (EM-7C): THIS business's count for the mailbox,
	// and the one breaker.
	if a.inviteMail.sender.Capped(tenantID.String(), address) {
		a.log.Warn("panel manager invitation refused: the mailbox cap for this business", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, managersHref+"?problem=capped")
		return
	}
	if a.inviteMail.sender.Refusing() {
		a.log.Warn("panel manager invitation refused: the mail breaker is open", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, managersHref+"?problem=mail-down")
		return
	}
	issued, err := a.adminInvites.Issue(r.Context(), tenantID, id.Admin.AdminUserID, address, name)
	switch {
	case errors.Is(err, adminauth.ErrInviteAddress):
		a.redirect(w, managersHref+"?problem=address")
		return
	case errors.Is(err, adminauth.ErrInviteBusy):
		a.redirect(w, managersHref+"?problem=busy")
		return
	case errors.Is(err, adminauth.ErrInviteNotOwner):
		// The INSERT's belt: the session's row is not an active owner here although the engine said yes.
		a.log.Warn("panel manager invitation refused: the inviter's row is not an active owner", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemManagersOwnerOnly)
		return
	case err != nil:
		a.log.Error("panel: could not issue a manager invitation", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return
	}
	view := email.AdminInvitationView{BaseURL: a.inviteMail.baseURL, ValidFor: issued.Invite.ExpiresAt.Sub(issued.Invite.CreatedAt).Round(time.Minute)}
	if s, err := a.accounts.Settings(r.Context(), tenantID); err == nil {
		view.TenantName, view.TenantVerified = s.Name, s.VAT.Verified != nil && *s.VAT.Verified
	} else {
		// The e-mail goes out without the name (the neutral words) rather than not at all.
		a.log.Warn("panel manager invitation: the business's name could not be read; the e-mail names nobody", "err_type", fmt.Sprintf("%T", err))
	}
	m, err := email.RenderAdminInvitation(r.Context(), issued.Link(a.inviteMail.baseURL), view)
	if err == nil {
		m.To = issued.Invite.Email
		m.Ref = issued.Invite.ID.String()
		rc, serr := a.inviteMail.sender.Send(r.Context(), tenantID.String(), m)
		if serr == nil {
			a.log.Info("panel manager invitation: the relay accepted the e-mail",
				"tenant_id", tenantID, "invite_id", issued.Invite.ID, "message_id", rc.MessageID)
			a.recordInvite(r, id, ActionAdminInviteSent, issued.Invite.ID, map[string]any{"invite_id": issued.Invite.ID.String()})
			a.redirect(w, managersHref+"?done=sent")
			return
		}
		err = serr
	}
	// Undelivered: the invitation is withdrawn (a live link no inbox holds would block the next one --
	// one open invitation per address), the trail says so with the class, the log with the class.
	attrs := []any{"tenant_id", tenantID, "invite_id", issued.Invite.ID, "err_type", fmt.Sprintf("%T", err)}
	detail := map[string]any{"invite_id": issued.Invite.ID.String()}
	if class, code, ok := sendErrorClass(err); ok {
		attrs = append(attrs, "class", class, "smtp_code", code)
		detail["class"], detail["smtp_code"] = class, code
	}
	a.log.Error("panel manager invitation: delivery failed", attrs...)
	wctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if _, cerr := a.adminInvites.Cancel(wctx, tenantID, issued.Invite.ID); cerr != nil {
		a.log.Error("panel manager invitation: withdrawing the undelivered invitation failed", "invite_id", issued.Invite.ID, "err_type", fmt.Sprintf("%T", cerr))
	}
	a.recordInvite(r, id, ActionAdminInviteUndelivered, issued.Invite.ID, detail)
	a.redirect(w, managersHref+"?problem=undelivered")
}

// recordInvite writes one trail row for the owner's act, on its own transaction (audit.Recorder), the
// request's cancellation removed and bounded -- a failure is logged, never swallowed.
func (a *AdminAuth) recordInvite(r *http.Request, id httpx.AdminIdentity, action string, inviteID uuid.UUID, detail map[string]any) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	actor := id.Admin.AdminUserID
	if _, err := a.audit.Record(ctx, audit.Event{TenantID: id.TenantID(), ActorID: &actor, Action: action, Target: inviteID.String(), Detail: detail}); err != nil {
		a.log.Error("panel manager invitation: the trail row could not be written", "action", action, "invite_id", inviteID, "err", err)
	}
}

// managersCancel serves POST managersCancelHref.
func (a *AdminAuth) managersCancel(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if a.adminInvites == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemInvitesUnavailable)
		return
	}
	if !a.readManagersForm(w, r) {
		return
	}
	if !a.mayInvite(r.Context(), id) {
		a.refuseManagers(w, r, id, "cancel")
		return
	}
	inviteID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("invite_id")))
	if err != nil {
		a.redirect(w, managersHref+"?problem=unreadable")
		return
	}
	ok, err := a.adminInvites.Cancel(r.Context(), id.TenantID(), inviteID)
	switch {
	case err != nil:
		a.log.Error("panel: could not cancel a manager invitation", "err", err, "invite_id", inviteID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	case !ok:
		a.redirect(w, managersHref+"?problem=not-open")
	default:
		a.recordInvite(r, id, ActionAdminInviteCancelled, inviteID, map[string]any{"invite_id": inviteID.String()})
		a.redirect(w, managersHref+"?done=cancelled")
	}
}

// ------------------------------------------------------------- the invitee --

// inviteAcceptPage serves GET adminInvitePath. It writes nothing: the resolver's read decides between
// the forms and the one "no longer works" screen (R19).
func (a *AdminAuth) inviteAcceptPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if a.adminInvites == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemInvitesUnavailable)
		return
	}
	raw := r.URL.Query().Get(adminauth.InviteParam)
	err := a.adminInvites.Usable(r.Context(), adminauth.ParseInviteToken(raw))
	if errors.Is(err, adminauth.ErrInviteUnusable) {
		a.render(w, r, http.StatusOK, pages.AdminInviteAccept(pages.InviteAcceptView{Dead: true, SignInHref: adminLoginPath}))
		return
	}
	if err != nil {
		a.log.Error("panel invitation page: the link could not be looked up", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemInviteServer)
		return
	}
	csrf, err := newCSRFToken()
	if err != nil {
		a.log.Error("panel invitation page: minting the csrf value failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemInviteServer)
		return
	}
	a.short.set(w, adminInviteCookieName, csrf, adminInviteCookieMaxAge)
	a.render(w, r, http.StatusOK, pages.AdminInviteAccept(a.inviteAcceptView(csrf, raw)))
}

func (a *AdminAuth) inviteAcceptView(csrf, raw string) pages.InviteAcceptView {
	return pages.InviteAcceptView{CSRFToken: csrf, Token: raw, SignInHref: adminLoginPath, MinPasswordChars: signup.MinPasswordRunes}
}

// inviteAccept serves POST adminInvitePath: Origin, the synchronizer cookie, the per-address budget,
// then one of the two acceptances.
func (a *AdminAuth) inviteAccept(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	ip := clientIP(r)
	if a.adminInvites == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemInvitesUnavailable)
		return
	}
	if !a.sameOrigin(r) {
		a.log.Warn("panel invitation refused: not same-origin", "ip", ip)
		a.renderProblem(w, r, http.StatusBadRequest, problemInviteRestart)
		return
	}
	ck, err := r.Cookie(adminInviteCookieName)
	if err != nil || ck.Value == "" {
		a.renderProblem(w, r, http.StatusBadRequest, problemInviteRestart)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, adminInviteMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemInviteRestart)
		return
	}
	if !constantTimeMatch(ck.Value, r.PostFormValue("csrf")) {
		a.log.Warn("panel invitation refused: csrf mismatch", "ip", ip)
		a.renderProblem(w, r, http.StatusBadRequest, problemInviteRestart)
		return
	}
	if within, n := a.inviteAcceptLimiter.TryCharge(ip); !within {
		if a.inviteAcceptLimiter.FirstOverLimit(n) {
			a.log.Warn("panel invitation rate limited", "scope", "address", "ip", ip)
		}
		a.renderProblem(w, r, http.StatusTooManyRequests, problemInviteTooMany)
		return
	}
	raw := r.PostFormValue(adminauth.InviteParam)
	tok := adminauth.ParseInviteToken(raw)
	mode := r.PostFormValue("mode")
	var (
		got db.AcceptedAdminInvite
	)
	switch mode {
	case "session":
		// The browser's panel cookie, whatever business it is in; an absent or unreadable one is the
		// zero Token, which no session hashes to -- the database refuses it like another person's.
		session, rerr := a.cookies.Read(r)
		if rerr != nil {
			session = adminauth.Token{}
		}
		got, err = a.adminInvites.AcceptWithSession(r.Context(), tok, session)
	case "new":
		if msg := checkNewPassword(r.PostFormValue("password"), r.PostFormValue("password_confirm")); msg != "" {
			v := a.inviteAcceptView(ck.Value, raw)
			v.Error, v.ErrorFor = msg, "new"
			a.render(w, r, http.StatusUnprocessableEntity, pages.AdminInviteAccept(v))
			return
		}
		got, err = a.adminInvites.AcceptNew(r.Context(), tok, r.PostFormValue("password"))
	default:
		a.renderProblem(w, r, http.StatusBadRequest, problemInviteRestart)
		return
	}
	switch {
	case err == nil:
		a.log.Info("panel invitation accepted", "tenant_id", got.TenantID, "admin_user_id", got.AdminUserID, "mode", mode)
		a.short.set(w, adminInviteCookieName, "", -1)
		a.render(w, r, http.StatusOK, pages.AdminInviteAccepted(pages.InviteAcceptedView{
			SignInHref: adminLoginPath, WithSession: mode == "session", SwitchHref: adminSwitchHref,
		}))
	case errors.Is(err, adminauth.ErrInviteUnusable):
		a.render(w, r, http.StatusOK, pages.AdminInviteAccept(pages.InviteAcceptView{Dead: true, SignInHref: adminLoginPath}))
	case errors.Is(err, adminauth.ErrInviteNotAccepted):
		v := a.inviteAcceptView(ck.Value, raw)
		v.ErrorFor = mode
		if mode == "session" {
			v.Error = "This browser is not signed in with the email address the invitation was sent to, or that address is already part of this business. Sign in with the invited address and open the link again."
		} else {
			v.Error = "This invitation could not be accepted with a new password. If this email address already has a Taptime sign-in, sign in with it and accept with your sign-in instead."
		}
		a.render(w, r, http.StatusOK, pages.AdminInviteAccept(v))
	default:
		a.log.Error("panel invitation: the acceptance failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemInviteServer)
	}
}
