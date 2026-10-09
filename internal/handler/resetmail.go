package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/web/templates/email"
)

// mailSender is the one thing an e-mail channel needs from internal/mail to send,
// declared at the consumer (§7). *mail.SMTP and *mail.Breaker both have it.
type mailSender interface {
	Send(ctx context.Context, m mail.Message) (mail.Receipt, error)
}

// resetSender is what THIS channel needs from the process-wide breaker (M10 EM-7A,
// ADR 0022 §9): mailSender for the reset link (refusable), SendExempt for the notice
// (counted, never refused) and Refusing for the request's question before it queues
// a grant. *mail.Breaker is the only type that has all three, and NewEmailResetChannel
// takes that type, so the channel cannot be wired around the breaker.
type resetSender interface {
	mailSender
	SendExempt(ctx context.Context, m mail.Message) (mail.Receipt, error)
	Refusing() bool
}

var _ resetSender = (*mail.Breaker)(nil)

// emailResetChannel is the ResetChannel for TAPPA_RESET_DELIVERY=email (M10 EM-5,
// ADR 0022 §6): it renders the reset e-mail (web/templates/email) and hands it to the
// SMTP relay (internal/mail) THROUGH THE PROCESS-WIDE BREAKER (mail.Breaker, M10
// EM-7A). DeliverReset runs on the outbox's worker, never on a request;
// DeliverPasswordNotice (M10 EM-9) runs in the request that changed the password —
// passwordnotice.go carries the claim for that half.
//
// 🔴 THIS CHANNEL IS WHERE THE BREAKER TELLS THE NOTICE FROM THE LINK (ADR 0022 EM-7A
// note, K7A-3), because it is the one place that knows which message it is rendering:
// the reset link goes through Send (refused past the limit), the notice through
// SendExempt (counted like every send, never refused — the same anonymous traffic that
// can fill the window must not silence the message that announces a takeover, EM-9
// note limit 11). internal/mail only offers the two methods and knows nothing of
// notices; the outbox and the request path never see the breaker, except through
// RefusingResets.
//
// WHAT IT TELLS ANYBODY, AND NOTHING ELSE (ADR 0022 §10):
//   - a failed send is returned as the relay's *mail.SendError unwrapped — a class
//     and a reply code, no text by construction — so deliver can log those two
//     fields; a render failure is returned wrapped around one of the email package's
//     sentinels, which quote no value;
//   - an accepted send writes ONE line: the reset row's id and the relay's message
//     id (which internal/mail has already dropped if it echoed anything it sent,
//     §2's window rule). Never the address, the link, the message or the relay's
//     words — the mail.Message built here is passed to Send and to nothing else.
//
// THE E-MAIL NAMES NOBODY (ADR 0022 §8, EM-4's ResetView): anyone can trigger it for
// any address, so it carries no signup-chosen text. The cost is accepted here: one
// address that belongs to several administrator accounts receives one e-mail per
// account (at most adminauth.ResetWindow), alike except for their links.
//
// THE CLAIM, IN THREE PARTS. Threat model: these pins hold against ACCIDENTAL drift
// in this file and in deliver's log lines; code written on purpose to get past a pin
// is the subject of code review.
//
// PART I — TODAY'S CODE, MEASURED (resetmail_test.go, the real mail.SMTP over TCP and
// STARTTLS to an in-test relay):
//   - three accounts behind one address get three messages, each to the address on
//     the ROW (the form typed it upper-cased), each with its own link intact in both
//     parts, the fixed subject, no reset id on the wire and no name —
//     TestEmailResetChannel_SendsEachLinkToTheRowsAddressAndNamesNobody;
//   - over nine relay behaviours — a clean 250, a 250 whose id echoes the token, 550
//     at RCPT and 554 at the end of data quoting the upper-cased address, the link
//     and the link in base64, 451 twice, 535 to AUTH, no STARTTLS, a relay that
//     accepts and never greets, one that stalls in the TLS handshake — each grant
//     ends in one row, a failure line names class, smtp_code and
//     err_type=*mail.SendError, an accepted line names the message id (empty when it
//     echoed), every key of every line is in ADR 0022 §10's closed set, and the log
//     holds neither the address (any case), the link, the token, the base64 link,
//     the SMTP username or password (nor any 8-character run of either) nor the
//     relay's words — TestEmailResetChannel_LogsOnlyIdsClassAndCode; the same key and
//     value scan over the three lines that test never reaches — the outbox refusing
//     a grant because it is full, because it is closing, and the account budget's
//     rate-limited line (its own key "scope" admitted) —
//     TestEmailResetChannel_QueueAndBudgetLinesLogOnlyIds;
//   - the e-mail states the time left when sent, and a link with under a minute left
//     is never sent — TestEmailResetChannel_StatesTheTimeLeftWhenSent,
//     TestEmailResetChannel_ALinkWithUnderAMinuteLeftIsNotSent;
//   - a base URL the e-mail cannot carry, and a nil sender, are refused at boot —
//     TestEmailResetChannel_RefusesWhatItCannotBuild;
//   - a stored non-ASCII address ends undelivered with class invalid_address, the
//     relay never dialled and the address not logged —
//     TestEmailResetChannel_AStoredAddressTheRelayCannotTakeIsNeverDialled;
//   - THE BREAKER (M10 EM-7A), over a recording sender with an injected clock: 300
//     notices pass, are counted (a link after them is refused as breaker and
//     RefusingResets says true) and a notice past the limit still passes; 300 links
//     pass and the 301st is refused —
//     TestEmailResetChannel_TheBreakerCountsTheNoticeAndRefusesTheLink.
//
// PART II — NAMED PINS AND EXACTLY WHAT EACH CATCHES (mutations run; the M10 EM-5
// card): the log test — the error itself on the failure line (M17, through the key
// set), class and code dropped (M18), the recipient on the accepted line (M19), the
// SendError wrapped so err_type no longer names it (M20); the queue-and-budget test —
// the recipient in the queue line's value (X19), a new "to" key on it (X19b), the
// event's detail on the budget line (X20); the lifetime tests — the
// full TTL instead of the time left (M21); the constructor test — no base-URL check
// (M22), a nil sender accepted (M23); the delivery test — the recipient not written
// into the message (M35); the breaker test (the M10 EM-7A card) — the notice sent
// through Send (refused when full), the notice sent around the breaker (not counted),
// the link sent through SendExempt (never refused). WHAT THEY DO NOT CATCH: a key inside the closed
// set carrying a value it should not (the set is about names; the value scan covers
// the listed secrets only); a relay behaviour not in the table; the real relay (SES),
// which is EM-5B's.
//
// PART III — Any form not listed above is the subject of code review — no
// completeness claim.
type emailResetChannel struct {
	sender  resetSender
	baseURL string
	log     *slog.Logger
}

// resetProbeValue is a well-shaped link value for the constructor's render check. It
// is not a token and grants nothing: it never leaves this process.
const resetProbeValue = "probe"

// NewEmailResetChannel builds the reset e-mail channel around the process-wide
// breaker. It takes *mail.Breaker and not the transport: cmd/tappa builds ONE breaker
// around the ONE transport and every e-mail channel sends through it (ADR 0022 §9 —
// it counts every send), so a channel handed the bare transport would be a hole in
// that count the type system can close.
//
// IT REFUSES A BASE URL THE E-MAIL CANNOT CARRY, AT BOOT. The email package accepts
// only an https base (plain http only on loopback, where development runs), and the
// rule is asked rather than restated: one render with a probe link. Without this a
// deployment whose TAPPA_BASE_URL fails the rule would boot, mint links, and record
// every one of them undelivered.
func NewEmailResetChannel(sender *mail.Breaker, baseURL string, log *slog.Logger) (ResetChannel, error) {
	if sender == nil {
		return nil, errors.New("handler: nil reset e-mail sender")
	}
	if log == nil {
		log = slog.Default()
	}
	probe := strings.TrimRight(baseURL, "/") + adminResetNewPath + "?t=" + resetProbeValue
	if _, err := email.RenderPasswordReset(context.Background(), probe, email.ResetView{BaseURL: baseURL, ValidFor: time.Hour}); err != nil {
		return nil, fmt.Errorf("handler: the reset e-mail cannot be built from TAPPA_BASE_URL: %w", err)
	}
	// THE NOTICE TOO (M10 EM-9), and this probe also holds the two copies of the
	// sign-in path together: the link is built from adminLoginPath here and checked
	// against the email package's own copy there, so a route renamed on one side
	// refuses to boot instead of mailing a dead link.
	if _, err := email.RenderPasswordChanged(context.Background(), signInLink(baseURL), email.PasswordChangedView{BaseURL: baseURL}); err != nil {
		return nil, fmt.Errorf("handler: the change notice cannot be built from TAPPA_BASE_URL: %w", err)
	}
	return &emailResetChannel{sender: sender, baseURL: baseURL, log: log}, nil
}

// signInLink is the notice's one link: the configured base's sign-in page. It is
// built from the CONFIGURED base (cfg.BaseURL, handed to NewEmailResetChannel at boot)
// and from nothing a request carries — a Host or X-Forwarded-Host header never reaches
// the channel, so it cannot reach the link.
func signInLink(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + adminLoginPath
}

// DeliverPasswordNotice renders the "your password was changed" notice (M10 EM-9) and
// sends it to n.Recipient — the address on the administrator's own row. Like
// DeliverReset it returns the relay's *mail.SendError unwrapped on a failed send, a
// wrapped sentinel on a render failure, and writes ONE line on an accepted send: the
// administrator's id and the relay's message id, never the address or the message.
func (c *emailResetChannel) DeliverPasswordNotice(ctx context.Context, n PasswordNotice) error {
	m, err := email.RenderPasswordChanged(ctx, signInLink(c.baseURL), email.PasswordChangedView{BaseURL: c.baseURL})
	if err != nil {
		return fmt.Errorf("handler: rendering the change notice: %w", err)
	}
	m.To = n.Recipient
	// SendExempt: counted by the breaker like every send, NEVER refused by it (the
	// type comment says why). Any other error is the transport's, returned as-is.
	receipt, err := c.sender.SendExempt(ctx, m)
	if err != nil {
		return err
	}
	c.log.InfoContext(ctx, "panel credential notice: the relay accepted the e-mail",
		"admin_user_id", n.AdminUserID, "tenant_id", n.TenantID, "message_id", receipt.MessageID)
	return nil
}

// DeliverReset renders the e-mail for d and sends it.
//
// THE LIFETIME IT STATES IS THE TIME LEFT WHEN IT IS SENT (d.ExpiresAt - now),
// floored by the email package — never ResetTTL. The outbox may hold a grant for a
// while, and the e-mail must not promise more time than the link has; a link with
// under a minute left is refused by the renderer and the grant is recorded
// undelivered.
func (c *emailResetChannel) DeliverReset(ctx context.Context, d ResetDelivery) error {
	m, err := email.RenderPasswordReset(ctx, d.Link, email.ResetView{BaseURL: c.baseURL, ValidFor: time.Until(d.ExpiresAt)})
	if err != nil {
		return fmt.Errorf("handler: rendering the reset e-mail: %w", err)
	}
	m.To = d.Recipient
	m.Ref = d.ResetID.String()
	// Send: refusable. A grant the request already saw refused never gets here
	// (RefusingResets); one that passed that question and found the window full by
	// the time the worker reached it is refused here, as an ordinary *mail.SendError
	// of class breaker — deliver records it undelivered like any failed send.
	receipt, err := c.sender.Send(ctx, m)
	if err != nil {
		// Unwrapped: a *mail.SendError carries a class and a code and no text, and
		// its Go type is what deliver's err_type names.
		return err
	}
	c.log.InfoContext(ctx, "panel recovery: the relay accepted the reset e-mail",
		"reset_id", d.ResetID, "message_id", receipt.MessageID)
	return nil
}

// RefusingResets asks the breaker whether a reset link handed over now would be
// refused (ADR 0022 EM-7A note, K7A-4). The request path asks it before queueing each
// grant; a true answer is a refusal the breaker counts.
func (c *emailResetChannel) RefusingResets() bool {
	return c.sender.Refusing()
}
