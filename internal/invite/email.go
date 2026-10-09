package invite

// email.go — the invitation's E-MAIL route (M10 EM-7B; normative source ADR 0022 §7
// and §9, as amended by its "EM-7B notu"). With TAPPA_INVITE_DELIVERY=email the
// activation link goes to the employee's OWN address instead of the manager's
// screen, which is the move ManagerVisibleChannel's comment has been waiting for
// since M5-02: it removes the PRECONDITION of ADR 0005 Y-D (the manager reading the
// code) for every employee who has an address — it does not close Y-D (a manager
// still types the address; ADR 0022 counted limit 2).
//
// WHAT THIS FILE OWNS, AND WHAT IT DOES NOT:
//
//   - It owns the RULES of the route: the three issuing limits (§9), the four
//     refusals of an address (§7: none, not plain ASCII, not one the relay takes,
//     an administrator's), whether the e-mail may name anybody (the user's decision
//     of 2026-10-09: only a VIES-verified business), and the four audit rows
//     (invite.code_emailed, invite.undelivered, invite.email_refused, and — through
//     ManagerVisibleChannel, unchanged — the owner's fallback).
//   - It does NOT render and does NOT speak SMTP. The e-mail's words are
//     web/templates/email's and the relay is internal/mail's; internal/handler joins
//     the two behind MailSink, the seam this package declares for it — the same
//     split ManagerVisibleChannel and its LinkSink have.
//   - It does NOT log: it has no logger. Every failure comes back to the caller as
//     a sentinel or as the sink's own error (a *mail.SendError carries a class and a
//     code and no text, ADR 0022 §3), and the caller decides what is written.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
)

// The issuing limits of the e-mail route (ADR 0022 §9 — m10's starting numbers,
// argued in the EM-7B note). They bound how many codes one business can mint, and so
// how many DKIM-signed e-mails it can make Taptime send, from a signup that is open
// and unverified (B17: three new businesses an hour per source address, no lifetime
// ceiling).
//
//   - TenantHourLimit = 50: a busy opening week of a chain (one venue's whole crew)
//     fits; a list of strangers does not.
//   - TenantDayLimit = 300: six full hours of the above, which no honest onboarding
//     reaches in a day.
//   - EmployeeHourLimit = 3: "it did not arrive, send again" twice; a fourth press
//     in the hour for the same PERSON is a manager hammering a broken address.
//
// ⚠️ THE PERSON LIMIT IS PER EMPLOYEE ROW, NOT PER MAILBOX (EM-7B round 4, security
// review): CountRecentInvites counts by employee_id. Several rows can hold one mailbox
// (plus-addressing within a business; the same address in several businesses), so a
// mailbox's ceiling is the BUSINESS limits — 50 an hour, 300 a day per business — and
// across businesses only the process-wide breaker (ADR 0022 §9). Measured by the
// reviewer: 21 messages to one mailbox in under a second from five plus-tagged rows in
// one business and the same address in two others. Counted in the EM-7B note
// ("alıcıya yoğunlaştırma"); a per-recipient ceiling is EM-5B's precondition (b).
//
// A press refused by a limit mints nothing: the count and the mint share one
// transaction and the refusal rolls it back.
const (
	TenantHourLimit   = 50
	TenantDayLimit    = 300
	EmployeeHourLimit = 3
)

// The refusals of the e-mail route. Each is a sentinel that names no value — no
// address, no name, no count — so it can travel in an error string, and the caller
// tells them apart with errors.Is. In each of them NOTHING WAS MINTED: the
// transaction that counted, inserted and read the address is rolled back, so
// employee_invites holds no row for the press and the employee's earlier links were
// not retired.
var (
	// ErrNoAddress: the employee has no address on file.
	ErrNoAddress = errors.New("invite: the employee has no address on file")
	// ErrAddressNotASCII: the address on file holds a byte outside ASCII. The panel's
	// add form stores such an address (its rule is weaker, B14) and the relay refuses
	// it (§4), so it is refused here BEFORE a code exists — with its own sentence,
	// because "an accented or non-Latin letter" is the one fix an owner needs to know.
	ErrAddressNotASCII = errors.New("invite: the address on file is not plain ASCII")
	// ErrAddressRefused: the address on file fails the relay's recipient rule
	// (mail.ValidRecipient — the rule Send itself applies, not a copy).
	ErrAddressRefused = errors.New("invite: the address on file is not one the relay takes")
	// ErrAddressIsAdministrators: the address equals, case-insensitively, the address
	// of an administrator of the same business.
	ErrAddressIsAdministrators = errors.New("invite: the address on file belongs to an administrator of this business")
	// ErrTenantHourLimit, ErrTenantDayLimit, ErrEmployeeHourLimit: §9's limits.
	ErrTenantHourLimit   = errors.New("invite: this business has reached its invitations for the hour")
	ErrTenantDayLimit    = errors.New("invite: this business has reached its invitations for the day")
	ErrEmployeeHourLimit = errors.New("invite: this person has reached their invitations for the hour")
	// ErrHasAddress: the owner's fallback (IssueParams.OnlyWithoutAddress) asked to
	// show a link on screen for somebody who HAS an address on file — the link goes
	// there instead.
	ErrHasAddress = errors.New("invite: the employee has an address on file")
)

// refusalReasons is the closed vocabulary invite.email_refused rows carry, one per
// refusal. A reason is a fixed word, never a value.
var refusalReasons = map[error]string{
	ErrNoAddress:               "no_address",
	ErrAddressNotASCII:         "address_not_ascii",
	ErrAddressRefused:          "address_not_deliverable",
	ErrAddressIsAdministrators: "address_is_an_administrators",
	ErrTenantHourLimit:         "business_hourly_limit",
	ErrTenantDayLimit:          "business_daily_limit",
	ErrEmployeeHourLimit:       "person_hourly_limit",
}

// RefusalReason reports whether err is one of the e-mail route's refusals and, if
// so, the fixed word its audit row carries.
func RefusalReason(err error) (string, bool) {
	for sentinel, reason := range refusalReasons {
		if errors.Is(err, sentinel) {
			return reason, true
		}
	}
	return "", false
}

// Recipient is where an e-mailed invitation goes and what it may say: the
// employee's own address and the two names, read in the transaction that minted the
// invitation, AFTER the row was inserted (GetInviteRecipient says why that order).
//
// ⚠️ Address IS PERSONAL DATA and travels beside the link in Delivery. It is never
// logged and never written to audit_log (ADR 0022 §7, B29: the trail cannot be edited,
// so a GDPR erasure would never reach it); the employees row is its only home.
type Recipient struct {
	Address      string
	EmployeeName string
	TenantName   string
	// TenantVerified is true only when VIES confirmed the business's VAT number
	// (tenants.vat_verified IS TRUE — false and NULL are both "not verified"). The
	// e-mail names the business and the person only then (user decision 2026-10-09);
	// web/templates/email holds the rule, and its zero value names nobody.
	TenantVerified bool
}

// MailSink sends ONE invitation e-mail and returns the relay's message id (empty
// when the relay gave none, or when internal/mail dropped it for echoing something
// it was sent — ADR 0022 §2). internal/handler implements it: it renders the
// e-mail (web/templates/email) and hands it to the relay (internal/mail).
//
// A failed send returns the relay's *mail.SendError as it came — a class and a
// reply code, no text — so EmailChannel can record the two and the caller can log
// them. It inherits Delivery's three obligations for d.ActivationURL, and one more
// for d.Recipient.Address: it is written into the message's To and nowhere else.
type MailSink interface {
	SendInvitation(ctx context.Context, d Delivery) (messageID string, err error)
}

// The audit actions of the e-mail route.
const (
	// ActionCodeEmailed: the relay accepted the invitation e-mail (250). It says
	// "sent", not "delivered" (ADR 0022 counted limit 4).
	ActionCodeEmailed = "invite.code_emailed"
	// ActionUndelivered: the invitation was minted (and the employee's earlier links
	// retired) but the e-mail did not go, or its outcome is unknown — the relay
	// refused it, the connection failed, the send ran out of time (a send that timed
	// out after the end of data MAY have been accepted: ADR 0022 counted limit 15).
	// The name follows admin.recovery.undelivered.
	ActionUndelivered = "invite.undelivered"
	// ActionEmailRefused: a press the route refused before minting anything — an
	// address it cannot use, or a limit. The name is `<subject>.<verb>_<refusal>`,
	// the shape location.delete_refused and employee.email_change_refused take.
	ActionEmailRefused = "invite.email_refused"
)

// Send and record bounds. The e-mail is sent in the request (ADR 0022 §7: the
// manager reads the outcome on screen), so its time is the manager's wait; both run
// on a context detached from the request's cancellation — a manager who closes the
// tab after the code was minted must not leave a minted code half-sent and
// unrecorded — and each has its own bound, so a send that uses all of its time
// cannot take the row's (EM-5's lesson, adminreset.go's deliver).
const (
	// EmailSendGrace bounds the send. internal/mail bounds a Send by its own
	// DefaultTimeout (15 s) too; the earlier of the two wins.
	EmailSendGrace = 10 * time.Second
	// EmailRecordGrace bounds the one audit row written after the send.
	EmailRecordGrace = 5 * time.Second
)

// EmailChannel is the Channel for TAPPA_INVITE_DELIVERY=email: it hands the link to
// MailSink and records what happened. Manager.IssueAndDeliver recognises it and,
// for it alone, counts the limits and reads the address inside the minting
// transaction (see there).
//
// One EmailChannel serves ONE request: the sink a handler gives it keeps what it
// sent (the address the screen names), which is per-request state, and actorID is
// the admin who pressed.
type EmailChannel struct {
	sink  MailSink
	audit auditRecorder
	// actorID is the admin who asked for the invitation (audit_log.actor_id holds
	// admin identities — ManagerVisibleChannel's precedent).
	actorID *uuid.UUID
}

// NewEmailChannel wires the e-mail route for one request. Both dependencies are
// required: a sink is the route, and an unrecorded send would make the trail lose
// the one fact an investigation starts from — that a credential left the process.
func NewEmailChannel(sink MailSink, rec auditRecorder, actorID *uuid.UUID) (*EmailChannel, error) {
	if sink == nil {
		return nil, errors.New("invite: email channel: nil sink")
	}
	if rec == nil {
		return nil, errors.New("invite: email channel: nil audit recorder")
	}
	return &EmailChannel{sink: sink, audit: rec, actorID: actorID}, nil
}

// emailedDetail is the audit payload of invite.code_emailed, and undeliveredDetail of
// invite.undelivered.
//
// THEY ARE PURPOSE-BUILT STRUCTS WITH NO FIELD AN ADDRESS, A NAME OR THE LINK COULD
// TRAVEL IN (codeShownDetail's rule). MessageID is the relay's id after internal/mail's
// echo rule (§2); Class and SMTPCode are internal/mail.SendError's two values.
//
// EXPLICIT EMPTIES, NO omitempty (EM-7B round 3; showRefusedDetail's and the
// deletedDetail lesson): each action has ONE key set whatever happened. A relay that
// gave no id writes "message_id": "", a failure with no reply code (a timeout, the
// breaker) writes "smtp_code": 0 — an absent key could not be told from a field that
// was never written.
type emailedDetail struct {
	InviteID   uuid.UUID `json:"invite_id"`
	EmployeeID uuid.UUID `json:"employee_id"`
	ExpiresAt  string    `json:"expires_at"`
	Channel    string    `json:"channel"`
	MessageID  string    `json:"message_id"`
}

type undeliveredDetail struct {
	InviteID   uuid.UUID `json:"invite_id"`
	EmployeeID uuid.UUID `json:"employee_id"`
	ExpiresAt  string    `json:"expires_at"`
	Channel    string    `json:"channel"`
	Class      string    `json:"class"`
	SMTPCode   int       `json:"smtp_code"`
}

// refusedDetail is the audit payload of invite.email_refused: the fixed reason
// word, and nothing a press typed or the database held.
type refusedDetail struct {
	Outcome    string    `json:"outcome"`
	Reason     string    `json:"reason"`
	EmployeeID uuid.UUID `json:"employee_id"`
	Channel    string    `json:"channel"`
}

// emailChannelName is the detail's channel value — the M6-11 report's way to tell
// this route from "manager_panel".
const emailChannelName = "email"

// DeliverInvite sends the e-mail and writes ONE row about it: invite.code_emailed
// with the relay's message id, or invite.undelivered with the failure's class and
// reply code.
//
// ORDER: send FIRST, then the row — the reverse of ManagerVisibleChannel's, and for
// the row's sake: invite.code_emailed carries the relay's message id, which exists
// only after the send. If the row's write fails after an accepted send, the error is
// returned and the caller tells the manager it could not confirm the e-mail went out;
// a second press mints a fresh code and retires this one, so the e-mail already in
// the inbox carries a dead link (B12's self-healing direction).
//
// A FAILED SEND RETURNS THE SINK'S ERROR AS IT CAME (unwrapped here; the Manager
// wraps it with the invitation id), so a *mail.SendError reaches the caller intact.
func (c *EmailChannel) DeliverInvite(ctx context.Context, d Delivery) error {
	base := context.WithoutCancel(ctx)
	sendCtx, cancelSend := context.WithTimeout(base, EmailSendGrace)
	messageID, sendErr := c.sink.SendInvitation(sendCtx, d)
	cancelSend()

	expires := d.Invite.ExpiresAt.UTC().Format(time.RFC3339)
	action := ActionCodeEmailed
	var detail any = emailedDetail{
		InviteID:   d.Invite.ID,
		EmployeeID: d.Invite.EmployeeID,
		ExpiresAt:  expires,
		Channel:    emailChannelName,
		MessageID:  messageID,
	}
	if sendErr != nil {
		action = ActionUndelivered
		failed := undeliveredDetail{
			InviteID:   d.Invite.ID,
			EmployeeID: d.Invite.EmployeeID,
			ExpiresAt:  expires,
			Channel:    emailChannelName,
		}
		var se *mail.SendError
		if errors.As(sendErr, &se) {
			failed.Class, failed.SMTPCode = string(se.Class), se.SMTPCode
		} else {
			// Not the relay's: the e-mail could not be built (a render sentinel, which
			// quotes no value). The class says so in a word of its own, outside
			// internal/mail's classes.
			failed.Class = "not_built"
		}
		detail = failed
	}
	recordCtx, cancelRecord := context.WithTimeout(base, EmailRecordGrace)
	defer cancelRecord()
	if _, err := c.audit.Record(recordCtx, audit.Event{
		TenantID: d.Invite.TenantID,
		ActorID:  c.actorID,
		Action:   action,
		Target:   d.Invite.EmployeeID.String(),
		Detail:   detail,
	}); err != nil {
		recordErr := fmt.Errorf("%w: %s: %w", ErrNotRecorded, action, err)
		if sendErr != nil {
			return errors.Join(sendErr, recordErr)
		}
		return recordErr
	}
	return sendErr
}

// ErrNotRecorded marks an outcome whose row could not be written — after an accepted
// send (the e-mail went, the trail does not say so), beside a failed one (errors.Join
// keeps the relay's *mail.SendError findable), or beside a refusal (IssueAndDeliver
// joins it to the refusal's sentinel, so the caller still knows why nothing was sent).
// The caller logs it; it names no value.
var ErrNotRecorded = errors.New("invite: the outcome row was not written")

// refused writes the one invite.email_refused row of a press the route refused. It
// runs AFTER the minting transaction rolled back, on its own transaction
// (audit.Record): the act did not happen, which is when a trail row is most needed.
func (c *EmailChannel) refused(ctx context.Context, p IssueParams, reason string) error {
	// Detached from the request's cancellation and bounded, like DeliverInvite's row: a
	// manager who closes the tab must not take the refusal's row with them.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), EmailRecordGrace)
	defer cancel()
	if _, err := c.audit.Record(ctx, audit.Event{
		TenantID: p.TenantID,
		ActorID:  c.actorID,
		Action:   ActionEmailRefused,
		Target:   p.EmployeeID.String(),
		Detail: refusedDetail{
			Outcome:    "refused",
			Reason:     reason,
			EmployeeID: p.EmployeeID,
			Channel:    emailChannelName,
		},
	}); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrNotRecorded, ActionEmailRefused, err)
	}
	return nil
}

// checkLimits takes the business's issuing lock and refuses a press past one of §9's
// limits. It runs FIRST in the minting transaction (LockTenantForInviteLimits says
// why), so the count includes every invitation committed before this transaction
// held the lock, and no concurrent issuer can mint between the count and this
// transaction's own commit.
//
// The order of the checks is the order of what a manager can do about it: a business
// past its day cannot be helped by waiting an hour, and a business past its hour
// cannot be helped by picking somebody else.
func checkLimits(ctx context.Context, q *store.Queries, p IssueParams) error {
	if err := q.LockTenantForInviteLimits(ctx, p.TenantID); err != nil {
		return fmt.Errorf("lock the limits: %w", err)
	}
	n, err := q.CountRecentInvites(ctx, store.CountRecentInvitesParams{
		TenantID:   p.TenantID,
		EmployeeID: p.EmployeeID,
	})
	if err != nil {
		return fmt.Errorf("count recent invitations: %w", err)
	}
	switch {
	case n.TenantLastDay >= TenantDayLimit:
		return ErrTenantDayLimit
	case n.TenantLastHour >= TenantHourLimit:
		return ErrTenantHourLimit
	case n.EmployeeLastHour >= EmployeeHourLimit:
		return ErrEmployeeHourLimit
	}
	return nil
}

// readRecipient reads the address and the names in the minting transaction, after
// the row was inserted, and applies §7's four refusals in their order: no address,
// not plain ASCII, not one the relay takes, an administrator's.
func readRecipient(ctx context.Context, q *store.Queries, p IssueParams) (Recipient, error) {
	row, err := q.GetInviteRecipient(ctx, store.GetInviteRecipientParams{
		TenantID:   p.TenantID,
		EmployeeID: p.EmployeeID,
	})
	if err != nil {
		return Recipient{}, fmt.Errorf("read the recipient: %w", err)
	}
	if row.Email == nil || *row.Email == "" {
		return Recipient{}, ErrNoAddress
	}
	addr := *row.Email
	for i := 0; i < len(addr); i++ {
		if addr[i] >= 0x80 {
			return Recipient{}, ErrAddressNotASCII
		}
	}
	if !mail.ValidRecipient(addr) {
		return Recipient{}, ErrAddressRefused
	}
	if row.IsAdminAddress {
		return Recipient{}, ErrAddressIsAdministrators
	}
	return Recipient{
		Address:        addr,
		EmployeeName:   row.FullName,
		TenantName:     row.TenantName,
		TenantVerified: row.TenantVerified,
	}, nil
}

// requireNoAddress is the owner's fallback's check (IssueParams.OnlyWithoutAddress),
// in the same place and for the same reason as readRecipient: an address written
// after the check but before the mint would otherwise be bypassed.
func requireNoAddress(ctx context.Context, q *store.Queries, p IssueParams) error {
	row, err := q.GetInviteRecipient(ctx, store.GetInviteRecipientParams{
		TenantID:   p.TenantID,
		EmployeeID: p.EmployeeID,
	})
	if err != nil {
		return fmt.Errorf("read the recipient: %w", err)
	}
	if row.Email != nil && *row.Email != "" {
		return ErrHasAddress
	}
	return nil
}
