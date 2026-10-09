package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
)

// THE "YOUR PASSWORD WAS CHANGED" NOTICE (M10 EM-9; ADR 0022 §8 and its "EM-9 note").
//
// WHEN IT GOES: after a password change has COMMITTED, on both paths that change one —
// a recovery link spent (AdminReset.Submit) and the Account section's change-my-own-
// password (AdminAuth.accountPasswordSave). It goes to the administrator's OWN row's
// address, read after the change (adminauth.Resets.NoticeRecipient), never to anything
// a request carried; its one link is the sign-in page, built from TAPPA_BASE_URL by the
// e-mail channel and never from a request header (resetmail.go).
//
// WHAT TURNS IT ON: the recovery flow's channel — TAPPA_RESET_DELIVERY. With "none"
// (the shipped ConfigMap) nothing is sent and the change still gets its notice row,
// saying so; with "email" the notice is rendered and sent through the same transport
// as the reset link. One switch, because both e-mails go to the same address on the
// same row through the same relay, and switching administrator e-mail on is ONE
// deploy decision (ADR 0022 §12).
//
// 🔴 IT IS SENT IN THE REQUEST, NOT THROUGH THE RESET OUTBOX — the decision, and why.
// EM-5A's outbox exists to hide a registered address from an anonymous requester
// (ADR 0022 §6): there, the send's time would answer "is this address registered".
// The notice has no such question to hide — the person who just changed a password
// knows their account exists — so the outbox buys it nothing, and it would cost it
// the thing a notice is for. The outbox is ONE FIFO of 32 shared with every recovery
// request, and anybody can fill it from the public form; a full outbox records its
// overflow undelivered and sends nothing. A notice queued there could be kept from
// the account holder by exactly the anonymous traffic it should not depend on —
// measured: with the outbox full, the notice still reaches the channel
// (TestPasswordNotice_AFullResetOutboxDoesNotStopIt). The price is the response's
// time: the change's answer waits for the relay, bounded by PasswordNoticeSendGrace.
// ⚠️ THAT ARGUMENT IS ABOUT THE QUEUE, AND THE §9 BREAKER (M10 EM-7A, mail.Breaker) IS
// THE OTHER SHARED RESOURCE the same anonymous traffic can exhaust. So the breaker
// COUNTS the notice and never REFUSES it (ADR 0022, EM-9 note limit 11; EM-7A note
// K7A-3): the e-mail channel sends the notice through Breaker.SendExempt and the reset
// link through Breaker.Send (resetmail.go). Measured: with the breaker refusing every
// reset link, a change still sends its notice and writes a sent row —
// TestPasswordNotice_ATrippedBreakerDoesNotStopIt.
//
// §4.6 — THE CHANGE IS NEVER UNDONE AND EVERY CHANGE ENDS IN ONE NOTICE ROW. The
// notice runs after the change committed and decides nothing about it: whatever the
// relay does, the answer is the one the change earned (303), and the trail gets
// exactly one admin.password_notice.* row per change — sent, or undelivered with a
// fixed reason. A panic inside the notice is contained HERE: under the router's
// Recoverer it would turn a committed change into a 500 that says it failed.
//
// THE CLAIM, IN THREE PARTS (agent-brief, M10 OP-6/OP-7). Threat model: these pins hold
// against ACCIDENTAL drift in this file, its two callers and the e-mail channel's notice
// method; code written on purpose to get past a pin is the subject of code review.
//
// PART I — TODAY'S CODE, MEASURED (passwordnotice_test.go unless named otherwise):
//   - every change ends in exactly ONE notice row on each outcome — sent; the relay
//     refusing (class and code in the row, no text); a plain channel error; "none"
//     (nothing sent, the address never read, the row says why); no address on the
//     row; the address read failing; the per-account cap spent; the channel panicking
//     with the address in the panic value — and nothing escapes to the caller; a
//     recorder that panics is asked for ONE write, not a second (fault) row —
//     TestPasswordNotice_EveryChangeEndsInOneRowAndTheChangeStands;
//   - the Account section tells the notifier after a committed change only (a wrong
//     or unchanged current password, a failure on our side and a password refused
//     before the database tell it nothing), with the session's own tenant and
//     administrator; with the real notifier failing or panicking it still answers 303
//     with the done flash, and the change's row comes before the notice row —
//     TestAccountPasswordSave_NotifiesAfterACommittedChangeOnly; a spent recovery link
//     sends it for the link's administrator to the row's address, after the completed
//     row, still landing on the sign-in form, and a refused link or a spend that
//     failed on our side (500) sends nothing —
//     TestAdminReset_ACompletedRecoveryNotifiesAndStillSignsIn;
//   - against the real transport and an in-test SMTP relay, both paths, every request
//     carrying a hostile Host, X-Forwarded-Host and Forwarded: the message goes to the
//     address ON THE ROW (a spelling no request carried), its one URL in each part is
//     TAPPA_BASE_URL + "/admin/login", and it carries no reset token, no recovery or
//     activation path, nothing of the hostile host and not the recipient's address —
//     TestPasswordNotice_GoesToTheRowsAddressWithTheConfiguredSignInLinkOnly; over
//     seven relay behaviours (accepted; 550 and 554 quoting the address, the sign-in
//     link and the address in base64; 451 twice; 535; no STARTTLS; no greeting) every
//     log line's keys are in ADR 0022 §10's set and no line carries the address (any
//     case), the SMTP credentials (or any 8-character run of either), the base64
//     address or the relay's words — TestPasswordNotice_LogsOnlyIdsClassAndCode; a
//     stored address the sending rule refuses (non-ASCII, a CR LF with a header after
//     it, two addresses) ends undelivered with class invalid_address, the relay never
//     dialled — TestPasswordNotice_AnAddressTheRelayCannotTakeIsNeverDialled;
//   - with the reset outbox full (one grant in a send, resetOutboxSize waiting, one
//     more refused — the control), the notice still reaches the channel —
//     TestPasswordNotice_AFullResetOutboxDoesNotStopIt; with the process-wide breaker
//     refusing (300 sends in the hour; the control: a reset link is refused), the
//     notice goes through the real e-mail channel to the sender behind the breaker,
//     the row says sent, and the notice was counted —
//     TestPasswordNotice_ATrippedBreakerDoesNotStopIt (M10 EM-7A);
//   - per account, exactly passwordNoticeLimit notices are SENT for 40 concurrent
//     changes (200 runs, every call started from one line); the rest end undelivered
//     with the cap's reason, one rate-limited line is logged, and another account's
//     notice still goes — TestPasswordNotice_ThePerAccountCapIsExactUnderConcurrentChanges;
//   - a relay that never answers holds the notice no longer than its own send grace
//     (the transport's own timeout set to 30 s), and the undelivered row is still
//     written on a trail that refuses an ended context —
//     TestPasswordNotice_AHungRelayHoldsTheAnswerOnlyForTheSendGrace; a request context
//     already cancelled still sends the notice and writes its row —
//     TestPasswordNotice_AVisitorWhoLeavesStillGetsTheNotice; an address read that never
//     answers gives up at the send grace (the row says the address could not be read,
//     nothing reaches the channel), and a row write that never answers is abandoned at
//     PasswordNoticeRecordGrace (the shipped 5 s, one write attempted) —
//     TestPasswordNotice_EachStepHasItsOwnBound;
//   - against REAL Postgres: the address is the row's own spelling, read in the row's
//     own tenant, and "" for a disabled row, a row with no address, the right id under
//     another tenant and an unknown id — internal/adminauth's
//     TestNoticeRecipient_IsTheAddressOnTheRowAndNothingElse; the whole recovery loop
//     ends with one notice to the row's address and one sent row in the tenant, and a
//     refused replay adds none (TestPanelRecoveryDB_EndToEnd); through the real
//     transport the notice goes to the row's address, its one URL in each part is the
//     configured sign-in page and it carries none of the e-mailed link
//     (TestPanelRecoveryDB_EndToEndThroughTheSMTPTransport);
//   - the row's detail, as JSON, is a closed set of keys and holds no address and no
//     '@' — TestPasswordNotice_TheRowCarriesNoAddress; the cap is 5 an hour and the
//     clocks 10 s and 5 s, each equal to its literal, stated in ADR 0022's EM-9 note in
//     the same numbers (read from the ADR) and wired into every flow —
//     TestPasswordNotice_TheShippedClocksAndCapAreWired;
//     NewAdminAuth refuses a nil notifier, untyped and typed —
//     TestPasswordNotice_TheNotifierIsRequired; the notice's send and row nest inside
//     the HTTP drain with room for the change itself — cmd/tappa's
//     TestShutdownBudget_ThePasswordNoticeNestsInsideTheHTTPGrace; run() gives the panel,
//     as its notifier, the one recovery flow it builds, whose channel is the one run()
//     fills from handler.NewEmailResetChannel and which it mounts and drains — cmd/tappa's
//     TestPasswordNoticeWiring_ThePanelIsGivenTheMountedRecoveryFlow (a source pin).
//
// PART II — NAMED PINS AND EXACTLY WHAT EACH CATCHES (each mutation was run on a copy;
// the M10 EM-9 card's table has them all):
//   - the row test: "none" no longer short-circuiting, the cap not consulted, a panic
//     re-raised, "decided" set after the row's write, no fault row after a panic, a
//     failed address read that does not stop the notice, a recipient that is not the
//     row's, the row naming another tenant;
//   - the Account test: the notice dropped, sent before the change whatever its
//     outcome, sent after a refused change, sent before the change's row, naming
//     another tenant, a panic re-raised, no fault row after a panic;
//   - the recovery test: the notice dropped from Submit, said to come from the Account
//     section, sent before the completed row, sent from Submit's failure branch; the
//     SMTP DB test: the notice dropped from
//     Submit; the recovery DB test: the address read with the ids swapped;
//   - the relay test: the recipient not the row's, the link not from the configured
//     base, the notice dropped from either path, the e-mail package's sign-in path
//     changed (with the channel's constructor test);
//   - the log test: the recipient on the accepted line, the error itself on the
//     failure line;
//   - the full-outbox test: the notice refused while the reset outbox is full; the
//     breaker test: the notice sent through the breaker's refusable Send;
//   - the cap test: "Allowed, then Charge" in place of TryCharge, the cap keyed on the
//     tenant, the cap not consulted;
//   - the hung-relay test: the send's context without its grace, the send-failed row
//     written on the spent send context; the visitor test: the request's cancellation
//     kept;
//   - the step-bound test: the address read moved outside the send grace, the row
//     written without its own grace;
//   - the row-keys test: an address field in the detail; the shipped-values test:
//     another send grace wired into the flow, the cap's window or size changed; the
//     required-notifier test: a nil notifier accepted; cmd/tappa's budget test: the send
//     grace raised to 16 s; cmd/tappa's wiring pin: the panel given a second recovery
//     flow built without the channel.
//   WHAT THEY DO NOT CATCH: the e-mail channel's constructor no longer rendering the
//   notice once at boot (the mutation stayed green: it matters only when the e-mail
//   package's copy of the sign-in path and adminLoginPath disagree, and then the relay
//   test is red at send time instead of the boot); a send the relay accepted after the
//   grace cut the conversation (recorded undelivered — ADR 0022 counted limit 15's
//   shape); a process killed between the change and the row (no row); a new caller
//   that changes a password and does not call passwordChanged.
//
// PART III — Any form not listed above is the subject of code review — no completeness
// claim.
//
// COUNTED LIMITS (ADR 0022's EM-9 note lists them with the card): the cap is per
// account and per process, in a fixed window (a burst at the boundary reaches twice
// the limit); one attacker with several accounts carrying a victim's address (signup
// is open and unverified) multiplies it; the §9 breaker counts the notice and never
// refuses it (see the queue paragraph above), so it does not bound the notices
// either — only this cap does.

// The notice's clocks, exported for cmd/tappa's shutdown budget test.
const (
	// PasswordNoticeSendGrace bounds getting the notice out: the address read and the
	// whole relay conversation, both attempts included. It runs INSIDE the request that
	// changed the password, so it must leave room in the HTTP drain for the change
	// itself (three cost-12 bcrypt runs on the Account path) and for the row below —
	// TestShutdownBudget_ThePasswordNoticeNestsInsideTheHTTPGrace holds that.
	PasswordNoticeSendGrace = 10 * time.Second
	// PasswordNoticeRecordGrace bounds the notice row's write, SEPARATELY from the send
	// (ADR 0022 §6.3's lesson): a send that spends its whole grace must not take the row
	// down with it.
	PasswordNoticeRecordGrace = 5 * time.Second
)

// passwordNoticeLimit / passwordNoticePeriod: how many notices ONE account may SEND
// per window. Past it the change still stands and still gets its row (undelivered,
// the cap's reason) — only the e-mail is withheld.
//
// WHY IT EXISTS: signup is open and unverified, so anybody can own an account whose
// row carries somebody else's address, and every change of that account's password
// sends that somebody a message. The Account section's write chain allows hundreds of
// changes a window per session (adminSessionLimit), each costing three cost-12 bcrypt
// runs: without this cap one account is a mail bomb aimed at a stranger.
//
// WHY PER ACCOUNT AND NOT PER ADDRESS — the cap's own cost, weighed. Keyed on the
// address, an attacker who owns an account with the victim's address could spend the
// address's budget first and SILENCE the victim's genuine notice when the victim's
// real account is taken over — the one message this exists for. Keyed on the account,
// spending the budget needs that account's password changed, and every change before
// the cap sends a notice: the takeover is announced before anything can be silenced.
// The price is counted, not hidden: several accounts multiply the cap.
//
// FIVE AN HOUR: a person who forgot a password, recovered it, then chose a better one
// from the Account section has sent two; five leaves room for a slip without letting
// one account be a bomb.
const (
	passwordNoticeLimit  = 5
	passwordNoticePeriod = time.Hour
)

// Audit actions for the notice. One per change, always one of the two.
const (
	ActionAdminPasswordNoticeSent        = "admin.password_notice.sent"
	ActionAdminPasswordNoticeUndelivered = "admin.password_notice.undelivered"
)

// Which path changed the password. A fixed token, never a request value.
const (
	noticeViaRecovery = "recovery"
	noticeViaAccount  = "account"
)

// The fixed reasons an undelivered notice row carries.
const (
	noticeReasonOff        = "this deployment sends no e-mail (TAPPA_RESET_DELIVERY is none), so no notice was sent"
	noticeReasonLimited    = "too many password changes on this account in the window, so the notice was not sent"
	noticeReasonNoAddress  = "the account has no active address to send the notice to"
	noticeReasonReadFailed = "the account's address could not be read, so the notice was not sent"
	noticeReasonSendFailed = "the notice could not be handed to the delivery channel"
	noticeReasonFault      = "the notice stopped on an internal fault; it may not have been sent"
)

// PasswordNotice is what a ResetChannel receives for the notice: where it goes and
// whose change it is about. It carries no link — the channel builds the one link the
// notice has, the sign-in page, from its own configured base.
//
// Recipient IS READ FROM THE ADMINISTRATOR'S OWN ROW (adminauth.Resets.NoticeRecipient)
// after the change committed. It is personal data and is never logged.
type PasswordNotice struct {
	Recipient   string
	TenantID    uuid.UUID
	AdminUserID uuid.UUID
}

// passwordChange is one committed password change the notice is about.
type passwordChange struct {
	tenantID    uuid.UUID
	adminUserID uuid.UUID
	// via is noticeViaRecovery or noticeViaAccount.
	via string
}

// passwordNotifier is what the Account section needs from the notice, declared at the
// consumer (§7). *AdminReset is the implementation: it holds the channel, the address
// reader and the recorder the notice needs, and the mode ("none" or "email") is its.
type passwordNotifier interface {
	passwordChanged(ctx context.Context, c passwordChange)
}

// passwordNoticeDetail is the audit payload of a notice row.
//
// IT IS A PURPOSE-BUILT STRUCT WITH NO FIELD AN ADDRESS COULD TRAVEL IN (ADR 0022 §7's
// rule for invitations, B29's reason: audit_log cannot be edited, so a GDPR erasure
// never reaches it). Class and SMTPCode are internal/mail.SendError's two values — a
// class from a closed set and a reply code — and nothing else of the relay's.
type passwordNoticeDetail struct {
	Outcome  string `json:"outcome"`
	Via      string `json:"via"`
	Reason   string `json:"reason,omitempty"`
	Class    string `json:"class,omitempty"`
	SMTPCode int    `json:"smtp_code,omitempty"`
}

// passwordChanged sends the notice for one committed change and writes its one row.
// It never returns an error and never panics: the change it is about has already
// happened, and nothing here may make its answer say otherwise.
func (h *AdminReset) passwordChanged(ctx context.Context, c passwordChange) {
	// The request's values (its id) without its cancellation: a visitor closing the tab
	// after a committed change must not stop the account holder being told.
	base := context.WithoutCancel(ctx)
	var decided bool
	if !h.containNotice(base, c, func() { h.notice(base, c, &decided) }) || decided {
		return
	}
	// The fault row is itself contained: a recorder that panics too is logged and the
	// request goes on to its answer.
	h.containNotice(base, c, func() {
		h.recordNotice(base, c, ActionAdminPasswordNoticeUndelivered,
			passwordNoticeDetail{Outcome: "undelivered", Via: c.via, Reason: noticeReasonFault})
	})
}

// notice is one change's notice. decided is set the moment the outcome row's write
// BEGINS: from there a panic must not produce a second row (passwordChanged).
func (h *AdminReset) notice(base context.Context, c passwordChange, decided *bool) {
	undelivered := func(reason string) {
		*decided = true
		h.recordNotice(base, c, ActionAdminPasswordNoticeUndelivered,
			passwordNoticeDetail{Outcome: "undelivered", Via: c.via, Reason: reason})
	}
	if !h.canDeliver() {
		undelivered(noticeReasonOff)
		return
	}
	// ONE LOCKED STEP (httpx.Limiter.TryCharge): concurrent changes of one account must
	// not all read "allowed" before any of them charges.
	if within, n := h.noticeLimiter.TryCharge(c.adminUserID.String()); !within {
		if h.noticeLimiter.FirstOverLimit(n) {
			h.log.WarnContext(base, "panel credential notice rate limited",
				"admin_user_id", c.adminUserID, "tenant_id", c.tenantID)
		}
		undelivered(noticeReasonLimited)
		return
	}

	sendCtx, cancelSend := context.WithTimeout(base, h.noticeSendGrace)
	defer cancelSend()
	to, err := h.resets.NoticeRecipient(sendCtx, c.tenantID, c.adminUserID)
	if err != nil {
		// The error's TYPE only, as deliver logs a failed send: the line is about a
		// person's address and its text is not ours to vouch for.
		h.log.ErrorContext(base, "panel credential notice: reading the address failed",
			"admin_user_id", c.adminUserID, "tenant_id", c.tenantID, "err_type", fmt.Sprintf("%T", err))
		undelivered(noticeReasonReadFailed)
		return
	}
	if to == "" {
		undelivered(noticeReasonNoAddress)
		return
	}
	err = h.mail.DeliverPasswordNotice(sendCtx, PasswordNotice{
		Recipient: to, TenantID: c.tenantID, AdminUserID: c.adminUserID,
	})
	// Released as soon as the send returns: the row below never runs on its context.
	cancelSend()
	if err != nil {
		// THE CHANNEL'S TEXT IS NOT LOGGED (deliver's measured reason): its class and
		// reply code when it is a *mail.SendError, its Go type otherwise.
		class, code, ok := sendErrorClass(err)
		if ok {
			h.log.ErrorContext(base, "panel credential notice: delivery failed",
				"admin_user_id", c.adminUserID, "tenant_id", c.tenantID,
				"err_type", fmt.Sprintf("%T", err), "class", class, "smtp_code", code)
		} else {
			h.log.ErrorContext(base, "panel credential notice: delivery failed",
				"admin_user_id", c.adminUserID, "tenant_id", c.tenantID, "err_type", fmt.Sprintf("%T", err))
		}
		*decided = true
		h.recordNotice(base, c, ActionAdminPasswordNoticeUndelivered, passwordNoticeDetail{
			Outcome: "undelivered", Via: c.via, Reason: noticeReasonSendFailed, Class: class, SMTPCode: code,
		})
		return
	}
	*decided = true
	h.recordNotice(base, c, ActionAdminPasswordNoticeSent, passwordNoticeDetail{Outcome: "sent", Via: c.via})
}

// recordNotice writes the notice's row on its OWN bounded context (never the send's).
// It is not budgeted: there is exactly one per committed change, so its volume is the
// changes' own, which their rows (admin.password_changed, admin.recovery.completed)
// already have unbudgeted — a success is never silenced in this package.
func (h *AdminReset) recordNotice(base context.Context, c passwordChange, action string, d passwordNoticeDetail) {
	ctx, cancel := context.WithTimeout(base, PasswordNoticeRecordGrace)
	defer cancel()
	h.record(ctx, audit.Event{
		TenantID: c.tenantID,
		ActorID:  ptr(c.adminUserID),
		Action:   action,
		Target:   c.adminUserID.String(),
		Detail:   d,
	})
}

// containNotice runs f and reports whether it PANICKED. The panic is logged as a fixed
// sentence with the change's ids — its value is not (it may carry the address or a
// part of the message) — and swallowed: re-raising would hand a committed change to
// the router's Recoverer, which answers 500.
func (h *AdminReset) containNotice(base context.Context, c passwordChange, f func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
			h.log.ErrorContext(base, "panel credential notice: the notice panicked; the change stands and the request went on",
				"admin_user_id", c.adminUserID, "tenant_id", c.tenantID)
		}
	}()
	f()
	return false
}
