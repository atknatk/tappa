package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// THE RECOVERY FLOW'S TWO PARTS OF THE SIGN-IN ADDRESS CHANGE (T173 EC-2; ADR 0044 §5.1 step 3, K-E11).
//
//	emailChanged     the OLD address's notice after a committed change: in the request, like the
//	                 "your password was changed" notice (passwordnotice.go gives the reasons: not the
//	                 reset outbox, which anonymous traffic can fill; the breaker counts it and never
//	                 refuses it -- SendExempt), its own send grace, its own per-person cap. With the undo
//	                 link the change wrote (K-E2 (a)); without one -- only when none was written (K-E9 (d)
//	                 makes that unreachable) -- the linkless shape (K-E10 (vi)).
//	addressRestored  after an undo: ONE recovery request for the address given back, on the per-identity
//	                 path (IssueForPerson -- identity_open_reset bound to that address, S-1), handed to the
//	                 reset outbox like any request's grants (K-E11 (a)).
//
// 🔴 BOTH ARE THE RECOVERY FLOW'S BECAUSE THE CHANNEL AND TAPPA_RESET_DELIVERY ARE (M10 EM-9's argument):
// one switch for every letter that goes to a sign-in's own address about its credentials.
//
// 🔴 THEIR OUTCOMES ARE LOG LINES, NOT AUDIT ROWS (ADR 0044 §5.7, ADR 0031 §5.15): the notice belongs to
// no business (every business already holds the definer's admin.email.changed row). The lines carry the
// identity's id and fixed words -- never an address, a link or a token (CLAUDE.md §7).

// EmailChangeNotice is what a ResetChannel receives for the old address's notice: where it goes, whose
// change it is, when it happened and -- when the change wrote one -- the undo link and its end.
//
// ⚠️ UndoLink CONTAINS THE RAW UNDO TOKEN (a reset-class credential: one secret writes the address AND
// the password -- ADR 0044 §5.3). It is built from TAPPA_BASE_URL by emailChanged and handed to the
// channel and to nothing else; never logged, never persisted. Recipient is personal data, never logged.
type EmailChangeNotice struct {
	Recipient     string
	IdentityID    uuid.UUID
	ChangedAt     time.Time
	UndoLink      string
	UndoExpiresAt time.Time
}

// emailChangeNoticeReasons are the fixed words of an undelivered notice's line.
const (
	emailNoticeReasonOff       = "this deployment sends no e-mail (TAPPA_RESET_DELIVERY is none), so no notice was sent"
	emailNoticeReasonLimited   = "too many address changes for this sign-in in the window, so the notice was not sent"
	emailNoticeReasonNoAddress = "the change returned no old address to send the notice to"
	emailNoticeReasonSendFail  = "the notice could not be handed to the delivery channel"
)

// emailChangeBudgetKey is the notice cap's key: the identity, under a prefix of its own (the password
// notice's identity key is another key -- the two caps never spend each other).
func emailChangeBudgetKey(id uuid.UUID) string { return "email-change:" + id.String() }

// emailChanged sends the old address's notice for one committed change and writes its one log line. It
// never returns an error and never panics: the change has happened, and nothing here may make its answer
// say otherwise (passwordChanged's rule; §4.6 -- the change is not undone when the notice fails).
func (h *AdminReset) emailChanged(ctx context.Context, c adminauth.EmailChanged) {
	base := context.WithoutCancel(ctx)
	outcome := func(o, reason string, extra ...any) {
		attrs := append([]any{"identity_id", c.IdentityID, "outcome", o}, extra...)
		if reason != "" {
			attrs = append(attrs, "reason", reason)
		}
		if o == "sent" {
			h.log.InfoContext(base, "panel address change notice", attrs...)
			return
		}
		h.log.WarnContext(base, "panel address change notice", attrs...)
	}
	defer func() {
		if recover() != nil {
			// The value is not logged: it may carry the address or a part of the message.
			h.log.ErrorContext(base, "panel address change notice: the notice panicked; the change stands and the request went on",
				"identity_id", c.IdentityID)
		}
	}()
	if !h.canDeliver() {
		outcome("undelivered", emailNoticeReasonOff)
		return
	}
	// ONE LOCKED STEP (TryCharge): concurrent changes of one person must not all read "allowed".
	if within, n := h.emailNoticeLimiter.TryCharge(emailChangeBudgetKey(c.IdentityID)); !within {
		if h.emailNoticeLimiter.FirstOverLimit(n) {
			h.log.WarnContext(base, "panel address change notice rate limited", "identity_id", c.IdentityID)
		}
		outcome("undelivered", emailNoticeReasonLimited)
		return
	}
	if c.OldEmail == "" {
		outcome("undelivered", emailNoticeReasonNoAddress)
		return
	}
	n := EmailChangeNotice{Recipient: c.OldEmail, IdentityID: c.IdentityID, ChangedAt: c.At}
	// The undo link from the CONFIGURED base, never a request header (resetmail.go's rule).
	if link := c.UndoLink(h.undoBase); link != "" {
		n.UndoLink, n.UndoExpiresAt = link, *c.UndoExpiresAt
	}
	sendCtx, cancelSend := context.WithTimeout(base, h.noticeSendGrace)
	defer cancelSend()
	err := h.mail.DeliverEmailChangeNotice(sendCtx, n)
	cancelSend()
	if err != nil {
		extra := []any{"err_type", fmt.Sprintf("%T", err)}
		if class, code, ok := sendErrorClass(err); ok {
			extra = append(extra, "class", class, "smtp_code", code)
		}
		outcome("undelivered", emailNoticeReasonSendFail, extra...)
		return
	}
	outcome("sent", "")
}

// addressRestored asks for ONE recovery link for the address an undo just gave back (K-E11 (a)): the
// per-identity request (IssueForPerson, M14-14's path -- identity_open_reset with p_recipient = that
// address, S-1), its grants handed to the reset outbox like a form request's. The undo already cancelled
// every live link of the identity, so the identity's link is minted now; what the outbox cannot take is
// recorded and withdrawn as for any request. A deployment that sends no e-mail logs that nothing was sent:
// the page then still tells the person to use "Forgot password" (K-E11 (b) is the fallback that exists
// anyway). Nothing here changes the undo's answer.
func (h *AdminReset) addressRestored(r *http.Request, address string) {
	ip := clientIP(r)
	if !h.canDeliver() {
		h.log.WarnContext(r.Context(), "panel address change undone: this deployment sends no e-mail, so no recovery link was sent", "ip", ip)
		return
	}
	if address == "" {
		h.log.WarnContext(r.Context(), "panel address change undone: no address came back, so no recovery link was sent", "ip", ip)
		return
	}
	p, err := h.resets.IssueForPerson(r.Context(), address)
	if err != nil {
		h.log.Error(EventIssueFailed, "ip", ip, "err", err)
		h.oweFailedRequest(context.WithoutCancel(r.Context()), err)
		return
	}
	fallbackUntil := time.Now().Add(h.clocks.fallbackBudget)
	for _, g := range p.Grants {
		h.dispatch(r, fallbackUntil, ip, g)
	}
	for _, k := range p.Kept {
		h.recordKept(context.WithoutCancel(r.Context()), fallbackUntil, k)
	}
	if p.IdentityKept != nil {
		h.recordIdentityKept(r, *p.IdentityKept)
	}
	for _, st := range p.Stalled {
		h.logStalledMint(r.Context(), ip, st)
	}
}
