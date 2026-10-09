package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/web/templates/email"
	"github.com/atknatk/tappa/web/templates/pages"
)

// inviteemail.go — the invitation's E-MAIL mode on the panel (M10 EM-7B; normative
// source ADR 0022 §7, §9, §10 and its "EM-7B notu"). With TAPPA_INVITE_DELIVERY=email
// the Employees section's invite button no longer shows a link: it mails one to the
// employee's own address, synchronously, and the screen says where it went.
//
// THE RULES ARE NOT HERE. The limits, the four address refusals, the order of the
// minting transaction and the audit rows are internal/invite's (email.go there); the
// e-mail's words, and whether they may name anybody, are web/templates/email's. This
// file joins them: it builds the per-request sink that renders and sends, picks the
// route (e-mail, or the owner's on-screen fallback), and turns each outcome into a
// sentence and a log line.
//
// THE CLAIM, IN THREE PARTS. Threat model: these pins hold against ACCIDENTAL drift in
// this file, its callers and internal/invite's e-mail route; code written on purpose
// to get past a pin is the subject of code review.
//
// PART I — TODAY'S CODE, MEASURED (inviteemail_db_test.go over real HTTP, real
// Postgres and the real mail.SMTP talking to the in-test relay; inviteemail_test.go
// with fakes; internal/invite's email_db_test.go for the transaction):
//   - one press, one message, to the address on the ROW, carrying the link
//     IssueAndDeliver minted (the activation link in both parts activates a phone over
//     HTTP), one invite.code_emailed row with exactly invite_id, employee_id,
//     expires_at, channel and message_id and no address, zero
//     invite.code_shown_to_manager rows, and no "/activate?code=" in the response —
//     TestInviteEmailDB_OnePressOneMessageCarryingTheMintedLink;
//   - names only for a VIES-verified business: verified → both names in the e-mail;
//     vat_verified false or NULL → neither, with hostile prose as the fixture, and the
//     neutral words present — TestInviteEmailDB_NamesOnlyForAVerifiedBusiness;
//   - the four address refusals each leave employee_invites unchanged, open no relay
//     connection, write one invite.email_refused row without the address, and show
//     their sentence — TestInviteEmailDB_EachAddressRefusalMintsNothing;
//   - a relay failure records invite.undelivered with class and smtp_code, and the
//     log holds the ids, the class and the code and not the address (any case), the
//     link, the code, the body or the relay's words —
//     TestInviteEmailDB_AFailedSendIsRecordedAndNeverLogsTheMessage;
//   - the owner's fallback shows the link once and records manager_panel; a manager
//     is refused with a trail row and no link — TestInviteEmailDB_OnlyTheOwnerSeesALinkOnScreen;
//     a manager, an empty and an undefined role are refused with the SESSION's role in
//     the row — TestInviteEmail_TheFallbackIsTheOwnersAlone; the card offers the e-mail
//     button only with an address and the fallback only to an owner —
//     TestInviteEmail_TheCardOffersOnlyWhatTheServerWouldDo;
//   - every outcome is its sentence and status and none carries the link —
//     TestInviteEmail_EveryOutcomeIsASentenceAndNoneCarriesALink; a failed send's log
//     line holds exactly the ids, err_type, class and smtp_code —
//     TestInviteEmail_AFailedSendLogsOnlyIdsClassAndCode; the process-wide breaker's
//     refusal (EM-7A) is the same "may not have gone out" page and an
//     invite.undelivered row with class "breaker" —
//     TestInviteEmailDB_ABreakerRefusalIsUndeliveredWithItsClass; a refusal whose own row
//     cannot be written still answers its sentence and logs the missing row —
//     TestInviteEmailDB_ARefusalWhoseRowFailsStillSaysWhy; a refused on-screen fallback
//     keeps its row when the visitor has left — TestInviteEmail_AShowRefusalIsRecordedWhenTheVisitorLeaves;
//     an outcome row that cannot
//     be written is logged, not swallowed — TestInviteEmail_AnUnrecordedOutcomeIsLoggedNotSwallowed;
//   - the panel mode takes the panel channel whatever the form says, and its card is
//     byte for byte the card before EM-7B — TestInviteEmail_ThePanelModeNeverMails,
//     TestRosterActions_ThePanelModeCardIsTheCardBeforeEM7B;
//   - the construction matches the mode both ways —
//     TestNewAdminAuth_TheInvitationModeMatchesTheConfiguration; a base URL the e-mail
//     cannot carry, and a nil sender, are refused at boot —
//     TestNewEmailInvitations_RefusesWhatItCannotBuild;
//   - THE PER-MAILBOX CAP (M10 EM-7C): with the employee's mailbox full (filled in
//     other spellings, through the real mail.RecipientCap the panel is given), a press
//     answers 429 with its sentence, mints nothing (the earlier link still works),
//     dials no relay, writes one invite.email_refused row with reason recipient_limit
//     and no address, logs no address, and carries no "/activate?code="; an hour later
//     the same press is sent — TestInviteEmailDB_ACappedMailboxIsRefusedBeforeAnythingIsMinted;
//     the sink asks the cap about the address and the press's business and counts
//     the send under that business — TestInviteEmail_TheSinkAsksTheCapAboutTheAddressOnly;
//     one business's invitations never decide another's, both ways: business A's five
//     invitations to an inbox leave business B's press to it SENT, and B's five leave
//     A's next one SENT once A's own count frees —
//     TestInviteEmailDB_OneBusinessesInvitationsNeverDecideAnothers (round 2 counted
//     every business together, and A's presses told B that some other business invites
//     this person; ADR 0022's EM-7C note); the cap met AT THE SEND (minted, then the
//     last slot taken) answers its own sentence, not "a few minutes" —
//     TestInviteEmail_EveryOutcomeIsASentenceAndNoneCarriesALink. Recovery links are
//     not counted here at all: their ceiling is one live link per account
//     (internal/adminauth).
//
// PART II — the named pins and what each catches are the M10 EM-7B card's mutation
// table (ADR 0022's "EM-7B notu" lists them) and, for the per-mailbox cap, the M10
// EM-7C card's (its "EM-7C notu"): the sink not asking the cap, the cap's refusal
// answered 200, the question removed from the minting transaction, asked after its
// commit, or asked about another value — each red.
//
// PART III — Any form not listed above is the subject of code review — no
// completeness claim.

// inviteSender is what the invitation route needs from the ceilings around the
// transport (ADR 0022 §9): Send — refusable by the per-mailbox cap and by the breaker,
// counted under the business that pressed (scope) — and the cap's question for that
// business and mailbox, asked inside the minting transaction (M10 EM-7C, K7C-5).
// Declared here, at the consumer; *mail.RecipientCap has both, the breaker and the
// bare transport do not, so the route cannot be built around the cap.
//
// 🔴 THE SCOPE IS THE BUSINESS (EM-7C round 3): another business's invitations to the
// same inbox are not counted, so one business's presses can neither refuse another's
// nor tell it that someone else invites this person. And recovery links are not
// counted here at all — the reset channel is given the breaker, not this cap.
type inviteSender interface {
	Send(ctx context.Context, scope string, m mail.Message) (mail.Receipt, error)
	Capped(scope, to string) bool
}

var _ inviteSender = (*mail.RecipientCap)(nil)

// EmailInvitations is the invitation e-mail transport as the panel holds it: the
// relay and the configured base the e-mail's one link must sit under. It holds no
// per-request state; each press builds its own emailLinkSink around it.
type EmailInvitations struct {
	sender  inviteSender
	baseURL string
}

// inviteProbeValue is a well-shaped code for the constructor's render check. It
// grants nothing and never leaves the process (resetProbeValue's twin).
const inviteProbeValue = "probe"

// NewEmailInvitations builds the transport for TAPPA_INVITE_DELIVERY=email.
//
// IT REFUSES A BASE URL THE E-MAIL CANNOT CARRY, AT BOOT (NewEmailResetChannel's
// reason): one render with a probe link, so a deployment whose TAPPA_BASE_URL fails
// the e-mail's base rule does not start and mint invitations it can never send.
//
// sender is the consumer-side interface, not a concrete type: cmd/tappa gives it the
// ONE mail.RecipientCap (EM-7C) around the ONE mail.Breaker (EM-7A) around the ONE
// transport — the reset channel gets that breaker directly, not the cap — and this
// route calls its Send under the pressing business — an invitation is counted by both
// ceilings and refused by either — and its Capped, before anything is minted.
func NewEmailInvitations(sender inviteSender, baseURL string) (*EmailInvitations, error) {
	if isNil(sender) {
		return nil, errors.New("handler: nil invitation e-mail sender")
	}
	probe := strings.TrimRight(baseURL, "/") + "/activate?code=" + inviteProbeValue
	if _, err := email.RenderInvitation(context.Background(), probe, email.InvitationView{BaseURL: baseURL, ValidFor: time.Hour}); err != nil {
		return nil, fmt.Errorf("handler: the invitation e-mail cannot be built from TAPPA_BASE_URL: %w", err)
	}
	return &EmailInvitations{sender: sender, baseURL: baseURL}, nil
}

// AdminAuthOption configures what only one deployment mode of the panel has.
type AdminAuthOption func(*AdminAuth)

// InvitationsByEmail switches the Employees section's invitations to the e-mail
// mode. cmd/tappa passes it exactly when TAPPA_INVITE_DELIVERY=email; NewAdminAuth
// refuses the option without that mode and the mode without the option.
func InvitationsByEmail(e *EmailInvitations) AdminAuthOption {
	return func(a *AdminAuth) { a.inviteMail = e }
}

// invitationMode checks the panel's invitation route against the configuration, both
// ways, at construction.
//
// 🔴 BOTH DIRECTIONS FAIL CLOSED, and each for its own reason. "email" without the
// transport would fall back to showing every link on the manager's screen while the
// operator believed they were being mailed (the shape cmd/tappa's old unbuiltDelivery
// refusal existed for). A transport in the panel mode would mail links a deployment
// never switched on — one that says "panel" mails nothing (the shipped ConfigMap did
// until 2026-10-09 and says "email" since), and switching is a deploy decision (ADR
// 0022 §12). "" is the panel mode, as config.Load reads an empty value.
func invitationMode(mode string, transport *EmailInvitations) error {
	switch mode {
	case "", config.InviteDeliveryPanel:
		if transport != nil {
			return errors.New("handler: an invitation e-mail transport was given, but TAPPA_INVITE_DELIVERY is panel")
		}
	case config.InviteDeliveryEmail:
		if transport == nil {
			return errors.New("handler: TAPPA_INVITE_DELIVERY is email, but no invitation e-mail transport was given")
		}
	default:
		return fmt.Errorf("handler: TAPPA_INVITE_DELIVERY=%q is not a mode this panel implements", mode)
	}
	return nil
}

// emailLinkSink is invite.MailSink for ONE press — panelLinkSink's twin. It renders
// the invitation (web/templates/email), sends it (the relay), and keeps the address
// the relay accepted so the screen can say where the link went.
//
// 🔴 IT IS THE WHOLE OF THE PANEL'S ACCESS TO THE LINK IN THIS MODE, AND IT NEVER
// SHOWS IT: the link goes into the message and the message goes to Send and nowhere
// else — not to a log, not to a field, not back to the handler. The address is kept
// (to) because the screen names it; the message is not.
type emailLinkSink struct {
	mail *EmailInvitations
	log  *slog.Logger
	// to is the address the relay accepted; sent says it did. Both stay zero on a
	// failed send.
	to   string
	sent bool
}

// SendInvitation renders and sends one invitation.
//
// THE LIFETIME IT STATES IS THE ROW'S OWN: expires_at − created_at (the M10 EM-4
// card's hand-off), the same two timestamps the screen's expiryPhrase reads.
// created_at is the database's clock and expires_at the process's, so the difference
// carries the two clocks' skew; it is rounded to the minute so a skew of milliseconds
// cannot turn the e-mail's "7 days" into "6 days" beside a screen that says 7.
//
// A FAILED SEND RETURNS THE RELAY'S ERROR UNWRAPPED (a *mail.SendError: a class and a
// code, no text); a render failure returns one of the email package's sentinels,
// which quote no value. An accepted send writes ONE line: the ids and the relay's
// message id — never the address, the names, the link or the message.
func (s *emailLinkSink) SendInvitation(ctx context.Context, d invite.Delivery) (string, error) {
	m, err := email.RenderInvitation(ctx, d.ActivationURL, email.InvitationView{
		BaseURL:        s.mail.baseURL,
		EmployeeName:   d.Recipient.EmployeeName,
		TenantName:     d.Recipient.TenantName,
		TenantVerified: d.Recipient.TenantVerified,
		ValidFor:       d.Invite.ExpiresAt.Sub(d.Invite.CreatedAt).Round(time.Minute),
	})
	if err != nil {
		return "", fmt.Errorf("handler: rendering the invitation e-mail: %w", err)
	}
	m.To = d.Recipient.Address
	m.Ref = d.Invite.ID.String()
	// Counted under the business that minted it — the scope RecipientCapped was asked
	// with inside the minting transaction (invite.MailSink).
	receipt, err := s.mail.sender.Send(ctx, d.Invite.TenantID.String(), m)
	if err != nil {
		return "", err
	}
	s.log.InfoContext(ctx, "panel invitation: the relay accepted the e-mail",
		"tenant_id", d.Invite.TenantID, "employee_id", d.Invite.EmployeeID,
		"invite_id", d.Invite.ID, "message_id", receipt.MessageID)
	s.to, s.sent = d.Recipient.Address, true
	return receipt.MessageID, nil
}

// RecipientCapped asks the per-mailbox cap whether an invitation from business
// tenantID to address would be refused now — that business's count only
// (invite.MailSink's question, M10 EM-7C). The address goes to the cap and nowhere
// else.
func (s *emailLinkSink) RecipientCapped(tenantID uuid.UUID, address string) bool {
	return s.mail.sender.Capped(tenantID.String(), address)
}

// The owner's fallback travels as ONE extra field on the invite form (ADR 0022 §7,
// K3). It is read only in the e-mail mode: the panel mode shows every link anyway and
// never looks at it.
const (
	inviteDeliverField  = "deliver"
	inviteDeliverScreen = "screen"
)

// showRefusalReasons maps every refusal internal/invite can return to the on-screen
// fallback onto the word its invite.show_refused row stores (the role refusal,
// "not_permitted", is this file's own and names no sentinel).
//
// IT IS A TABLE FOR THE REASON activate.go's inviteFailureReasons is one: a test has to
// be able to read it. TestActivationReasons_CoverEverySentinel derives internal/invite's
// sentinels and requires each to have its audited word in exactly one of the tables —
// activation's, the e-mail route's (internal/invite's refusalReasons) or this one.
var showRefusalReasons = map[error]string{
	invite.ErrHasAddress: "has_address",
}

// ActionInviteShowRefused is the audit action of a refused on-screen fallback: a
// non-owner asked for it, or the person has an address after all. The name is the
// `<subject>.<verb>_<refusal>` shape employee.email_change_refused takes.
const ActionInviteShowRefused = "invite.show_refused"

// showRefusedDetail is that row's payload: who was refused and why, and nothing a
// request typed. EXPLICIT EMPTIES, the deletedDetail lesson.
type showRefusedDetail struct {
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason"`
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
}

// mayShowInviteLink reports whether this session may see an activation link on
// screen in the e-mail mode: an OWNER only (ADR 0022 §7, K3) — EM-6's
// mayChangeEmployeeEmail predicate, since the address and the fallback are the two
// ways a link reaches somebody other than its employee. The role comes from the
// resolved session, never from the form. ⚠️ Until EM-12 every administrator is an
// owner (B28), so in the product today the gate separates no real request; the tests
// build a manager session by hand.
func mayShowInviteLink(id httpx.AdminIdentity) bool {
	return id.Live() && id.Admin.Role == adminRoleOwner
}

// employeeInviteByEmail is employeeInvite in the e-mail mode, for a person the caller
// has already read and found invitable.
//
// WHERE EACH OUTCOME GOES — every one is rendered by this POST (pages.AdminInviteEmailed
// says why no redirect carries them):
//
//	sent                               200, "sent to <address>, works for N days"
//	an address refusal (four)          200, its sentence; nothing minted
//	a limit (three) or the mailbox's   429, its sentence; nothing minted
//	  cap (M10 EM-7C)
//	minted, not confirmed sent         503, "may not have gone out — try again in a
//	                                     few minutes"
//	minted, then the mailbox's cap     503, "created but not emailed — the limit
//	  met at the send (EM-7C round 2)    frees within an hour, or a day"
//	the transaction failed             500, the panel's problem page
//
// 503 FOR "MAY NOT HAVE GONE OUT" (EM-7B round 3), the panel's precedent for an outside
// service that did not answer or cannot serve now and a retry later is the remedy: the
// operator's VAT re-check answers 503 when VIES gives no answer (and when its answer
// could not be recorded), the logo upload 503 when its admission is full. The relay
// refusing or timing out, and the breaker (EM-7A) refusing, are that shape; 500 stays
// for OUR failure before anything was minted.
//
// 🔴 THE LOG NEVER CARRIES THE ERROR'S TEXT ON A FAILED SEND (ADR 0022 §10, K7B-8):
// the ids, then the relay's class and reply code when it is a *mail.SendError, its Go
// type otherwise. The relay's words can quote the address and the link (S6, B6).
func (a *AdminAuth) employeeInviteByEmail(w http.ResponseWriter, r *http.Request, f ledger.RosterFilter, person tenant.Person) {
	if r.PostFormValue(inviteDeliverField) == inviteDeliverScreen {
		a.employeeInviteOnScreen(w, r, f, person)
		return
	}
	id := httpx.AdminOf(r)
	view := pages.InviteEmailedView{
		PanelChrome:        a.chrome(r, pages.TabEmployees),
		Name:               person.Name,
		PersonLimit:        invite.EmployeeHourLimit,
		BusinessHourLimit:  invite.TenantHourLimit,
		BusinessDayLimit:   invite.TenantDayLimit,
		RecipientHourLimit: mail.RecipientHourLimit,
		RecipientDayLimit:  mail.RecipientDayLimit,
		BackHref:           rosterReturn(f, person.ID, "", ""),
	}
	// PER-REQUEST, ON THE STACK — panelLinkSink's rule: the sink holds the address it
	// sent to, and a shared one would name one manager's recipient on another's screen.
	sink := &emailLinkSink{mail: a.inviteMail, log: a.log}
	actor := id.Admin.AdminUserID
	ch, err := invite.NewEmailChannel(sink, a.audit, &actor)
	if err != nil {
		a.log.Error("panel: could not build the invitation e-mail channel", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	inv, err := a.invites.IssueAndDeliver(r.Context(), invite.IssueParams{
		TenantID:   id.TenantID(),
		EmployeeID: person.ID,
	}, ch)
	if reason, refused := invite.RefusalReason(err); refused {
		a.log.Warn("panel invitation e-mail refused: nothing was minted",
			"tenant_id", id.TenantID(), "employee_id", person.ID, "reason", reason)
		// §4.6: the refusal stands (nothing was minted, the sentence below is true), but
		// the trail is missing its row — said in a line of its own, ids and the fixed
		// reason only.
		if errors.Is(err, invite.ErrNotRecorded) {
			a.log.Error("panel invitation e-mail refused, and its refusal row was not written",
				"tenant_id", id.TenantID(), "employee_id", person.ID, "reason", reason)
		}
		view.Outcome = reason
		status := http.StatusOK
		if errors.Is(err, invite.ErrEmployeeHourLimit) || errors.Is(err, invite.ErrTenantHourLimit) ||
			errors.Is(err, invite.ErrTenantDayLimit) || errors.Is(err, invite.ErrRecipientCapped) {
			status = http.StatusTooManyRequests
		}
		a.renderPanel(w, r, status, view.PanelChrome, pages.AdminInviteEmailed(view))
		return
	}
	if err != nil && inv.ID == uuid.Nil {
		// Nothing was minted and it was not a refusal: the transaction failed. The
		// chain is this repository's id-carrying wraps around a database error — no
		// address, no link (ADR 0022 §10's `err` key, B11).
		a.log.Error("panel: could not issue the invitation", "employee_id", person.ID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	if err != nil {
		// MINTED, NOT CONFIRMED SENT (B12, ADR 0022 counted limit 11). The row is
		// committed and the person's earlier links are retired; the e-mail failed, or
		// its outcome is unknown, or its trail row could not be written. The next
		// press mints a fresh link and retires this one, which is what the screen asks
		// for — without blaming the address or the manager.
		a.logUnsentInvitation(r.Context(), id.TenantID(), person.ID, inv.ID, sink.sent, err)
		view.Outcome = "unsent"
		// THE MAILBOX FILLED BETWEEN THE QUESTION AND THE SEND (M10 EM-7C round 2): the
		// minting transaction found room, another of this business's invitations to the
		// same inbox took the last slot before this one went. Its own sentence, because "try again in a few
		// minutes" would be wrong — the slot frees within the hour, or the day at the
		// daily line — and, like every sentence here, it blames neither the address nor
		// the manager.
		if class, _, ok := sendErrorClass(err); ok && !sink.sent && class == string(mail.ClassRecipientCap) {
			view.Outcome = "unsent-recipient-limit"
		}
		a.renderPanel(w, r, http.StatusServiceUnavailable, view.PanelChrome, pages.AdminInviteEmailed(view))
		return
	}
	if !sink.sent {
		// The channel reported success without the sink ever sending. That cannot
		// happen through invite.EmailChannel; if it does, the screen must not say
		// "sent" (§4.6).
		a.log.Error("panel: the invitation was issued but no e-mail was sent", "invite_id", inv.ID)
		view.Outcome = "unsent"
		a.renderPanel(w, r, http.StatusServiceUnavailable, view.PanelChrome, pages.AdminInviteEmailed(view))
		return
	}
	view.Outcome = "sent"
	view.Address = sink.to
	view.Expires = expiryPhrase(inv.CreatedAt, inv.ExpiresAt)
	view.Retired = inv.RetiredSiblings
	a.renderPanel(w, r, http.StatusOK, view.PanelChrome, pages.AdminInviteEmailed(view))
}

// logUnsentInvitation writes the failed send's ONE line: the ids, and the relay's
// class and reply code (a *mail.SendError) or the error's Go type — never its text.
// sent distinguishes the one case where the relay DID accept the e-mail and the trail
// row is what failed.
func (a *AdminAuth) logUnsentInvitation(ctx context.Context, tenantID, employeeID, inviteID uuid.UUID, sent bool, err error) {
	if sent {
		a.log.ErrorContext(ctx, "panel invitation: the relay accepted the e-mail but its trail row was not written",
			"tenant_id", tenantID, "employee_id", employeeID, "invite_id", inviteID, "err_type", fmt.Sprintf("%T", err))
		return
	}
	if class, code, ok := sendErrorClass(err); ok {
		a.log.ErrorContext(ctx, "panel invitation: the e-mail was not sent",
			"tenant_id", tenantID, "employee_id", employeeID, "invite_id", inviteID,
			"err_type", fmt.Sprintf("%T", err), "class", class, "smtp_code", code)
	} else {
		a.log.ErrorContext(ctx, "panel invitation: the e-mail was not sent",
			"tenant_id", tenantID, "employee_id", employeeID, "invite_id", inviteID, "err_type", fmt.Sprintf("%T", err))
	}
	// §4.6: a failed send whose invite.undelivered row ALSO failed leaves the trail
	// silent about this press, so the log says so in a line of its own — ids only.
	if errors.Is(err, invite.ErrNotRecorded) {
		a.log.ErrorContext(ctx, "panel invitation: the undelivered row was not written either",
			"tenant_id", tenantID, "employee_id", employeeID, "invite_id", inviteID)
	}
}

// employeeInviteOnScreen is the e-mail mode's owner-only fallback (ADR 0022 §7, K3):
// for somebody with NO address on file, the link is shown on the panel exactly as the
// panel mode shows it — through invite.ManagerVisibleChannel, whose row
// (invite.code_shown_to_manager, channel "manager_panel") records that an owner saw
// it. M6-11 reports the two channels apart (EM-8).
//
// TWO REFUSALS, EACH WITH A TRAIL ROW AND NOTHING MINTED: a session that is not an
// owner's (checked BEFORE anything is read or written), and a person who has an
// address after all — checked by internal/invite inside the minting transaction, after
// the row is inserted (IssueParams.OnlyWithoutAddress), so an address saved a moment
// earlier cannot be bypassed.
func (a *AdminAuth) employeeInviteOnScreen(w http.ResponseWriter, r *http.Request, f ledger.RosterFilter, person tenant.Person) {
	id := httpx.AdminOf(r)
	refused := func(reason, outcome string) {
		a.recordShowRefusal(r.Context(), id, person.ID, reason)
		view := pages.InviteEmailedView{
			PanelChrome: a.chrome(r, pages.TabEmployees),
			Name:        person.Name,
			Outcome:     outcome,
			BackHref:    rosterReturn(f, person.ID, "", ""),
		}
		a.renderPanel(w, r, http.StatusOK, view.PanelChrome, pages.AdminInviteEmailed(view))
	}
	if !mayShowInviteLink(id) {
		a.log.Warn("panel invitation link refused: only the owner may see one on screen",
			"tenant_id", id.TenantID(), "employee_id", person.ID, "role", id.Admin.Role)
		refused("not_permitted", "show-not-permitted")
		return
	}
	sink := &panelLinkSink{}
	actor := id.Admin.AdminUserID
	ch, err := invite.NewManagerVisibleChannel(sink, a.audit, &actor)
	if err != nil {
		a.log.Error("panel: could not build the invitation channel", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	inv, err := a.invites.IssueAndDeliver(r.Context(), invite.IssueParams{
		TenantID:           id.TenantID(),
		EmployeeID:         person.ID,
		OnlyWithoutAddress: true,
	}, ch)
	switch {
	case errors.Is(err, invite.ErrHasAddress):
		a.log.Warn("panel invitation link refused: the person has an address on file",
			"tenant_id", id.TenantID(), "employee_id", person.ID)
		refused(showRefusalReasons[invite.ErrHasAddress], "show-has-address")
		return
	case err != nil:
		a.log.Error("panel: could not issue the invitation", "employee_id", person.ID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	case !sink.shown:
		a.log.Error("panel: the invitation was issued but no link reached the screen", "invite_id", inv.ID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	issued := pages.InviteIssuedView{
		PanelChrome:   a.chrome(r, pages.TabEmployees),
		Name:          person.Name,
		ActivationURL: sink.link,
		Expires:       expiryPhrase(inv.CreatedAt, inv.ExpiresAt),
		Retired:       inv.RetiredSiblings,
		BackHref:      rosterReturn(f, person.ID, "", ""),
	}
	a.renderPanel(w, r, http.StatusOK, issued.PanelChrome, pages.AdminInviteIssued(issued))
}

// recordShowRefusal writes the one invite.show_refused row. A failure to write it is
// logged, not swallowed (§7), and does not change the answer: nothing was minted
// either way.
//
// ON ITS OWN BOUNDED CONTEXT, DETACHED FROM THE REQUEST'S CANCELLATION (EM-7B round 3),
// like every other row of the e-mail route (invite.EmailRecordGrace): a manager who
// closes the tab must not take the refused attempt's row with them.
func (a *AdminAuth) recordShowRefusal(ctx context.Context, id httpx.AdminIdentity, employeeID uuid.UUID, reason string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), invite.EmailRecordGrace)
	defer cancel()
	actor := id.Admin.AdminUserID
	if _, err := a.audit.Record(ctx, audit.Event{
		TenantID: id.TenantID(),
		ActorID:  &actor,
		Action:   ActionInviteShowRefused,
		Target:   employeeID.String(),
		Detail: showRefusedDetail{
			Outcome:      "refused",
			Reason:       reason,
			Role:         id.Admin.Role,
			RequiredRole: adminRoleOwner,
		},
	}); err != nil {
		a.log.Error("panel: could not record the refused on-screen invitation", "employee_id", employeeID, "err", err)
	}
}
