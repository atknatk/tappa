package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/web/templates/email"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE SIGN-IN ADDRESS CHANGE (T173 EC-2; ADR 0044, every K-E decision as the user accepted it on
// 2026-11-12). The mainstream pattern (Google, Microsoft, GitHub): the current password, a link to the
// NEW address that must be opened to finish, and a "this wasn't you" letter to the OLD address that can
// undo it.
//
//	the card        "Change your sign-in email" on the account page, LINKED sessions only (an older
//	                per-business sign-in is not the person -- ADR 0044 §3); new address + current
//	                password + the warning that the new address is visible to the admins of every
//	                business (K-E10 (ii), K-E17 (a), ADR 0044 §8 risk 10)
//	the request     POST accountEmailHref -- ProtectWriting's MAIN group, the suspension ring included
//	                (K-E15 (a)). The work gate -> the person's bucket (charged before anything is
//	                compared or resolved) -> the address's shape -> the password -> the resolver (the
//	                letter's choice) -> identity_request_email_change -> the letter handed to ITS OWN
//	                queue -> the FLOOR -> 303 with one flash word that names no address (K-E10 (i) (a)).
//	the link's GET  emailChangeLanding: the token into a short-lived cookie, a redirect -- it spends
//	                NOTHING (R19)
//	the press       POST accountEmailConfirmHref (main group too, K-E15 (a)): identity_change_email with
//	                the link AND this browser's live session of the same identity (R2), then ONE notice
//	                to the old address with the undo link, in the request (emailchangenotice.go)
//	the undo        GET/POST emailUndoPath -- SESSIONLESS, outside every panel chain, mounted only while
//	                TAPPA_ADMIN_IDENTITY is on (the invitation page's shape: Origin, a synchronizer cookie,
//	                a per-address budget): identity_revert_email_change, then ONE recovery link to the
//	                address it gave back (K-E11 (a))
//
// 🔴 THE REQUEST'S ANSWER IS THE SAME FOR EVERY NEW ADDRESS (ADR 0044 §5.4, K-15's fourth requirement):
// nobody's, an identity's, or the address of a row in another business -- the same 303, the same flash,
// the same floor; the three arms run the same statements (adminauth.RequestEmailChange) and the letter
// leaves the request path. TestEmailChange_TheThreeArmsAnswerAlikeOverHTTP holds the bytes and
// TestEmailChange_TheThreeArmsAreFlatOverHTTP the time, on real Postgres.
//
// 🔴 §7 -- no address, no token, no password in a log line, a URL or an audit row. The flash is a word
// from a closed list; the refusals are sentences about the person's own state. This file writes no audit
// row: the definer writes each business its admin.email.changed / admin.email.reverted row.

var (
	// accountEmailHref is the request (POST); accountEmailConfirmHref the press that spends the link.
	accountEmailHref        = accountHref + "/email"
	accountEmailConfirmHref = accountHref + "/email/confirm"
)

// emailChangeLandingPath is the change link's landing; emailUndoPath the undo link's page (GET) and press
// (POST). Both are adminauth's paths -- under the panel cookie's path, so the landing's redirect reaches
// the account page with the session. emailUndoPath is written as a literal for the route scans that
// resolve a route by its declaration (admincookiepath_test.go); TestEmailUndo_IsAdminauthsPath holds it
// to adminauth.UndoEmailPath.
const (
	emailChangeLandingPath = adminauth.ChangeEmailPath
	emailUndoPath          = "/admin/undo-email-change"
)

const (
	// accountEmailCookieName holds the change link's raw token between its GET and the press (the
	// verification link's pattern): HttpOnly, the panel path, half an hour.
	accountEmailCookieName   = "tappa_admin_email_change"
	accountEmailCookieMaxAge = 30 * 60
	// emailUndoCookieName holds "<csrf>.<token>" between the undo link's GET and its press (the
	// invitation page's pattern): HttpOnly, the page's own path, half an hour.
	emailUndoCookieName   = "tappa_admin_email_undo"
	emailUndoCookieMaxAge = 30 * 60
	// emailUndoMaxFormBytes bounds the undo press's form: one synchronizer value.
	emailUndoMaxFormBytes = 4 << 10
)

// emailChangeRequestFloor is how long POST accountEmailHref takes, whatever happened: EVERY exit pays it
// (atFloor holds the whole response). The work under it is one cost-12 bcrypt comparison -- the bulk,
// ~0.38 s in production (adminAttemptLimit's figure) -- and three database round trips; 600 ms is
// signupClaimFloor's number for the same work. It equalises the three arms ADR 0044 §5.4 holds flat; work
// that overruns it leaks again, proportionally (resetRequestFloor's caveat).
const emailChangeRequestFloor = 600 * time.Millisecond

const (
	// emailLetterQueueSize is the change letters' own queue (card EC-2 item 5): bounded, in-process, and
	// NOT the reset outbox -- anonymous traffic can fill that one from the public form. Full: the letter
	// is not sent, one log line says so, and the answer does not change.
	emailLetterQueueSize = 16
	// emailLetterSendGrace bounds one letter's send on the worker (resetSendGrace's number).
	emailLetterSendGrace = 15 * time.Second
)

// The account page's words for this card (accountIdentityDoneWords / ProblemWords take them).
var (
	accountEmailDoneWords    = []string{"email-sent", "email-changed"}
	accountEmailProblemWords = []string{"email-refused", "email-no-mail", "email-mail-down"}
)

// The card's sentences (K-E10; the user approves them on screen -- the report lists every one).
const (
	emailSentSentence     = "Check your inbox at the new address. The change happens when you open the link there."
	emailChangedSentence  = "Your sign-in email is changed. Sign in with the new address from now on."
	emailRefusedSentence  = "This link can't be used. Ask for a new one from your account page."
	emailNoMailSentence   = "This Taptime does not send emails, so your sign-in email cannot be changed here."
	emailMailDownSentence = "Our email service is not accepting messages right now, so nothing was sent. Try again in a few minutes."
	emailTooManySentence  = "You asked to change your email several times in the last hour. Use the newest email, or wait an hour and ask again."
	emailWrongPassword    = "Your current password is not right."
	emailRefusedGeneric   = "That could not be done just now. Nothing was changed; please try again."
)

// emailRefusalSentences maps identity_request_email_change's closed set of refusals to K-E10 (v)'s
// sentences. Each speaks of the person's own state or of the shape of what they typed -- never of the new
// address's owner. revert_live is emailRevertLiveSentence (it carries the undo link's end, K-E18 (a)).
var emailRefusalSentences = map[db.EmailChangeRefusal]string{
	db.EmailChangeUnverified:     "Confirm your current email first.",
	db.EmailChangeWindowOpen:     "Finish confirming your businesses first.",
	db.EmailChangeUnlinkedRows:   "Add your other businesses first.",
	db.EmailChangeSameAddress:    "That's already your address.",
	db.EmailChangeInvalidAddress: "That doesn't look like an email address we can send to.",
	db.EmailChangeStale:          "Please sign in again.",
}

// emailRevertLiveSentence is revert_live's sentence: the person's OWN undo link's end, in the business's
// zone (CLAUDE.md §6: local time only here).
func emailRevertLiveSentence(until time.Time, zone *time.Location) string {
	if until.IsZero() {
		return "You changed your email recently. You can change it again 7 days after that change."
	}
	return "You changed your email recently. You can change it again after " + until.In(zone).Format("2 January 2006, 15:04") + "."
}

// emailRefusalSentence is the sentence for a refusal code; an unknown one is the generic refusal, never a
// 500 (card EC-2 item 3).
func emailRefusalSentence(code db.EmailChangeRefusal, until time.Time, zone *time.Location) string {
	if code == db.EmailChangeRevertLive {
		return emailRevertLiveSentence(until, zone)
	}
	if s, ok := emailRefusalSentences[code]; ok {
		return s
	}
	return emailRefusedGeneric
}

// accountEmailNotice places a done or problem word in the card.
func accountEmailNotice(v *pages.AccountIdentityView, done, problem string) {
	switch done {
	case "email-sent":
		v.EmailNotice = emailSentSentence
	case "email-changed":
		v.EmailNotice = emailChangedSentence
	}
	switch problem {
	case "email-refused":
		v.EmailProblem = emailRefusedSentence
	case "email-no-mail":
		v.EmailProblem = emailNoMailSentence
	case "email-mail-down":
		v.EmailProblem = emailMailDownSentence
	}
}

// ------------------------------------------------------------------ the floor --

// heldResponse is a response kept in memory until the floor has passed: the status, the headers and the
// body. Nothing reaches the client before atFloor writes it.
type heldResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (h *heldResponse) Header() http.Header { return h.header }

func (h *heldResponse) WriteHeader(status int) {
	if h.status == 0 {
		h.status = status
	}
}

func (h *heldResponse) Write(b []byte) (int, error) {
	if h.status == 0 {
		h.status = http.StatusOK
	}
	return h.body.Write(b)
}

// atFloor runs next into a held response and writes it once floor has passed since the request reached
// here -- on EVERY exit next takes, so no branch can answer early (card EC-2 item 4's structural
// guarantee; TestEmailChange_EveryExitPaysTheFloor).
func (a *AdminAuth) atFloor(w http.ResponseWriter, r *http.Request, floor time.Duration, next http.HandlerFunc) {
	start := time.Now()
	held := &heldResponse{header: http.Header{}}
	next(held, r)
	if rest := floor - time.Since(start); rest > 0 {
		time.Sleep(rest)
	}
	for k, vs := range held.header {
		w.Header()[k] = vs
	}
	status := held.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if _, err := w.Write(held.body.Bytes()); err != nil {
		a.log.Debug("panel address change: the client left before the answer was written")
	}
}

// ---------------------------------------------------------------- the request --

// accountEmailRequest serves POST accountEmailHref: every exit at the floor.
func (a *AdminAuth) accountEmailRequest(w http.ResponseWriter, r *http.Request) {
	a.atFloor(w, r, a.emailFloor, a.emailRequest)
}

func (a *AdminAuth) emailRequest(w http.ResponseWriter, r *http.Request) {
	id, token, ok := a.accountIdentityBegin(w, r, "email_request")
	if !ok {
		return
	}
	// AN UNLINKED ROW IS REFUSED BEFORE ANY CHARGE (M14-18's rule): it is not the person, and its bucket
	// is its own -- it never spends the identity's.
	if !id.Admin.Linked {
		a.redirect(w, accountReturn("", "unavailable"))
		return
	}
	// A fact about the deployment, never about the address: said before anything is charged.
	if a.inviteMail == nil || a.emailLetters == nil {
		a.redirect(w, accountReturn("", "email-no-mail"))
		return
	}
	bucket, err := a.admins.AccountBucket(r.Context(), id.Admin)
	if err != nil {
		a.log.Error("panel: the address change's bucket could not be read", "admin_user_id", id.Admin.AdminUserID, "tenant_id", id.TenantID(), "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return
	}
	// THE PERSON'S BUDGET, BEFORE THE COMPARISON AND THE RESOLVER (ADR 0044 §5.4): every arm spends the
	// same unit. One locked step; the work gate's refusal gives it back (it compared nothing).
	charge, within, first, _ := a.emailChangeLimiter.Reserve(bucket)
	if !within {
		if first {
			a.log.Warn("panel address change rate limited", "admin_user_id", id.Admin.AdminUserID, "tenant_id", id.TenantID())
		}
		a.accountEmailAgain(w, r, http.StatusTooManyRequests, func(v *pages.AccountIdentityView, _ *time.Location) {
			v.EmailProblem = emailTooManySentence
		})
		return
	}
	defer charge.Keep()
	if a.inviteMail.sender.Refusing() {
		a.log.Warn("panel address change refused: the mail breaker is open", "admin_user_id", id.Admin.AdminUserID, "tenant_id", id.TenantID())
		a.redirect(w, accountReturn("", "email-mail-down"))
		return
	}
	req, err := a.admins.RequestEmailChange(r.Context(), id.Admin, token, r.PostFormValue("current_password"), r.PostFormValue("new_email"))
	switch {
	case errors.Is(err, adminauth.ErrBusy):
		charge.Refund()
		a.workBusy(w, r)
		return
	case errors.Is(err, adminauth.ErrNotLinked), errors.Is(err, adminauth.ErrNoSession):
		a.redirect(w, accountReturn("", "unavailable"))
		return
	case errors.Is(err, adminauth.ErrEmailNotAddress):
		a.accountEmailAgain(w, r, http.StatusOK, func(v *pages.AccountIdentityView, _ *time.Location) {
			v.EmailProblem = emailRefusalSentences[db.EmailChangeInvalidAddress]
		})
		return
	case errors.Is(err, adminauth.ErrBadCredentials):
		a.accountEmailAgain(w, r, http.StatusOK, func(v *pages.AccountIdentityView, _ *time.Location) {
			v.EmailProblem = emailWrongPassword
		})
		return
	case err != nil:
		a.log.Error("panel: could not write the address change link", "admin_user_id", id.Admin.AdminUserID, "tenant_id", id.TenantID(), "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return
	}
	if req.Refusal != "" {
		a.log.Info("panel address change refused", "admin_user_id", id.Admin.AdminUserID, "tenant_id", id.TenantID(), "reason", string(req.Refusal))
		a.accountEmailAgain(w, r, http.StatusOK, func(v *pages.AccountIdentityView, zone *time.Location) {
			v.EmailProblem = emailRefusalSentence(req.Refusal, req.ExpiresAt, zone)
		})
		return
	}
	a.emailLetters.hand(r, a.log, emailLetterJob{
		scope:       emailChangeScope(bucket),
		recipient:   req.Recipient,
		link:        req.Link(strings.TrimRight(a.inviteMail.baseURL, "/")),
		expiresAt:   req.ExpiresAt,
		adminUserID: id.Admin.AdminUserID,
		tenantID:    id.TenantID(),
		ref:         req.ID,
	})
	a.redirect(w, accountReturn("email-sent", ""))
}

// emailChangeScope is the per-mailbox cap's partition for the change letters: the PERSON (ADR 0044 §5.8,
// EM-7C's partitioned count) -- another sender's letters to the same inbox are not counted with these.
func emailChangeScope(bucket string) string { return "email-change|" + bucket }

// accountEmailAgain renders the account page again (status) with the card's sentence placed by place,
// which also gets the business's zone -- a refused request's answer, never in the address bar.
func (a *AdminAuth) accountEmailAgain(w http.ResponseWriter, r *http.Request, status int, place func(*pages.AccountIdentityView, *time.Location)) {
	id := httpx.AdminOf(r)
	s, err := a.accounts.Settings(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not re-read the business account after a refused address change", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAccountUnreadable)
		return
	}
	v := a.accountView(r, id, s)
	if v.Identity != nil {
		place(v.Identity, a.accountZone(s.Timezone))
	}
	a.renderPanel(w, r, status, v.PanelChrome, pages.AdminAccount(v))
}

// ------------------------------------------------------------- the letter queue --

// emailLetterJob is one change letter waiting for the worker: the link (or "" for the linkless note to an
// address that already has a sign-in -- K-E5 (a)), the recipient, the mailbox cap's scope, and the
// requester's ids for the log line. The link carries the raw token: never logged, never persisted.
type emailLetterJob struct {
	base        context.Context
	scope       string
	recipient   string
	link        string
	expiresAt   time.Time
	adminUserID uuid.UUID
	tenantID    uuid.UUID
	ref         uuid.UUID
}

// emailLetterSender is what the worker needs from the ceilings around the transport: the per-mailbox
// cap's Send under a scope -- refusable by the cap and by the breaker (EM-7C, EM-7A). *mail.RecipientCap
// is the implementation (inviteSender has it).
type emailLetterSender = inviteSender

// emailLetterQueue is the change letters' bounded queue and its one worker.
type emailLetterQueue struct {
	jobs    chan emailLetterJob
	mu      sync.Mutex
	closed  bool
	done    chan struct{}
	stop    context.Context
	cancel  context.CancelFunc
	sender  emailLetterSender
	baseURL string
	log     emailLetterLog
}

func newEmailLetterQueue(size int, e *EmailInvitations, log emailLetterLog) *emailLetterQueue {
	q := &emailLetterQueue{jobs: make(chan emailLetterJob, size), done: make(chan struct{}), sender: e.sender, baseURL: e.baseURL, log: log}
	q.stop, q.cancel = context.WithCancel(context.Background())
	go q.work()
	return q
}

// emailLetterLog is the logger the queue writes through (a *slog.Logger in production).
type emailLetterLog interface {
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// hand offers j without blocking. A full or closing queue drops the letter with one line; the request's
// answer does not change (ADR 0044 §5.4: the time of the send must not reach the response).
func (q *emailLetterQueue) hand(r *http.Request, log emailLetterLog, j emailLetterJob) {
	j.base = context.WithoutCancel(r.Context())
	q.mu.Lock()
	defer q.mu.Unlock()
	state := "closing"
	if !q.closed {
		select {
		case q.jobs <- j:
			return
		default:
			state = "full"
		}
	}
	log.WarnContext(r.Context(), "panel address change: the letter queue did not take the letter, so it was not sent",
		"admin_user_id", j.adminUserID, "tenant_id", j.tenantID, "queue", state)
}

func (q *emailLetterQueue) work() {
	defer close(q.done)
	for j := range q.jobs {
		q.send(j)
	}
}

// send is one letter on the worker: rendered here, sent through the per-mailbox cap (scope: the person)
// and the breaker. A panic is caught and logged WITHOUT its value (it may carry the address or the link).
func (q *emailLetterQueue) send(j emailLetterJob) {
	defer func() {
		if recover() != nil {
			q.log.ErrorContext(j.base, "panel address change: a letter panicked; the worker recovered",
				"admin_user_id", j.adminUserID, "tenant_id", j.tenantID)
		}
	}()
	if q.stop.Err() != nil {
		q.log.WarnContext(j.base, "panel address change: the process stopped before the letter was sent",
			"admin_user_id", j.adminUserID, "tenant_id", j.tenantID)
		return
	}
	ctx, cancel := boundedBy(j.base, emailLetterSendGrace, q.stop)
	defer cancel()
	m, err := q.render(ctx, j)
	if err == nil {
		m.To = j.recipient
		m.Ref = j.ref.String()
		var receipt mail.Receipt
		if receipt, err = q.sender.Send(ctx, j.scope, m); err == nil {
			q.log.InfoContext(j.base, "panel address change: the relay accepted the letter",
				"admin_user_id", j.adminUserID, "tenant_id", j.tenantID, "message_id", receipt.MessageID)
			return
		}
	}
	extra := []any{"admin_user_id", j.adminUserID, "tenant_id", j.tenantID, "err_type", fmt.Sprintf("%T", err)}
	if class, code, ok := sendErrorClass(err); ok {
		extra = append(extra, "class", class, "smtp_code", code)
	}
	q.log.WarnContext(j.base, "panel address change: the letter was not sent", extra...)
}

// render builds the letter: the change link, or -- for an address that already has a sign-in -- the
// linkless note whose one link is the sign-in page (K-E5 (a)). Both from TAPPA_BASE_URL.
func (q *emailLetterQueue) render(ctx context.Context, j emailLetterJob) (mail.Message, error) {
	if j.link == "" {
		return email.RenderEmailChangeTaken(ctx, signInLink(q.baseURL), email.PasswordChangedView{BaseURL: q.baseURL})
	}
	return email.RenderEmailChange(ctx, j.link, email.EmailChangeView{BaseURL: q.baseURL, ValidFor: time.Until(j.expiresAt)})
}

// Drain stops the queue taking letters and waits for the worker to finish what it holds, inside ctx's
// deadline (cmd/tappa's shutdown runs it beside the HTTP server's, with the other drains).
func (q *emailLetterQueue) Drain(ctx context.Context) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.jobs)
	}
	q.mu.Unlock()
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		q.cancel()
		return fmt.Errorf("handler: the address change letters did not drain within their budget: %w", ctx.Err())
	}
}

// Drain is the panel's shutdown drain: the address change letters' queue (nil when this deployment
// sends no e-mail).
func (a *AdminAuth) Drain(ctx context.Context) error {
	if a.emailLetters == nil {
		return nil
	}
	return a.emailLetters.Drain(ctx)
}

// ------------------------------------------------------------------ the link --

// emailChangeLanding serves GET emailChangeLandingPath?t=…: the mailed link. It changes NOTHING (R19):
// the value goes into a short-lived cookie and the browser goes to the account page, which offers the
// press when a session is there (without one the panel sends it to the sign-in first; the cookie waits
// half an hour).
func (a *AdminAuth) emailChangeLanding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	t := r.URL.Query().Get(adminauth.ChangeEmailParam)
	if t == "" || strings.ContainsAny(t, ".") || len(t) > 200 || !cookieSafe(t) {
		a.redirect(w, accountReturn("", "email-refused"))
		return
	}
	a.short.set(w, accountEmailCookieName, t, accountEmailCookieMaxAge)
	a.redirect(w, accountHref)
}

// accountEmailConfirm serves POST accountEmailConfirmHref: the link this browser carries, spent with THIS
// browser's session (identity_change_email: the link's identity's live session -- R2). The cookie is
// cleared whatever the answer; the refusal is one sentence that does not tell its causes apart.
func (a *AdminAuth) accountEmailConfirm(w http.ResponseWriter, r *http.Request) {
	id, token, ok := a.accountIdentityBegin(w, r, "email_confirm")
	if !ok {
		return
	}
	ck, err := r.Cookie(accountEmailCookieName)
	a.short.clear(w, accountEmailCookieName)
	if err != nil || ck.Value == "" || !id.Admin.Linked {
		a.redirect(w, accountReturn("", "email-refused"))
		return
	}
	changed, err := a.admins.ConfirmEmailChange(r.Context(), id.Admin, ck.Value, token)
	if errors.Is(err, adminauth.ErrEmailChangeRefused) {
		a.log.Info("panel address change link refused", "admin_user_id", id.Admin.AdminUserID, "tenant_id", id.TenantID())
		a.redirect(w, accountReturn("", "email-refused"))
		return
	}
	if err != nil {
		a.log.Error("panel: could not change the sign-in address", "admin_user_id", id.Admin.AdminUserID, "tenant_id", id.TenantID(), "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return
	}
	a.log.Info("panel sign-in address changed", "admin_user_id", id.Admin.AdminUserID, "tenant_id", id.TenantID())
	// THE OLD ADDRESS'S NOTICE, after the commit, in the request (emailchangenotice.go). It decides
	// nothing about the change (§4.6).
	a.notices.emailChanged(context.WithoutCancel(r.Context()), changed)
	a.redirect(w, accountReturn("email-changed", ""))
}

// ------------------------------------------------------------------ the undo --

// The undo page's failure screens.
var (
	problemEmailUndoNoLink = pages.ProblemView{
		Title:   "Open the link from the email again",
		Message: "This page needs the link from the email in the same browser, and this browser does not have it.",
		Hint:    "Allow cookies for this site, then open the link from the email again.",
	}
	problemEmailUndoRestart = pages.ProblemView{
		Title:   "That step expired",
		Message: "Nothing was changed.",
		Hint:    "Open the link from the email again.",
	}
	problemEmailUndoTooMany = pages.ProblemView{
		Title:   "Too many attempts",
		Message: "We have stopped accepting these from here for a few minutes. Nothing was changed.",
		Hint:    "Wait a little and open the link from the email again.",
	}
	problemEmailUndoServer = pages.ProblemView{
		Title:   "Something went wrong on our side",
		Message: "Nothing was changed.",
		Hint:    "Try again in a minute.",
	}
)

func (c adminCookies) setEmailUndo(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     emailUndoCookieName,
		Value:    value,
		Path:     emailUndoPath,
		MaxAge:   emailUndoCookieMaxAge,
		HttpOnly: true,
		Secure:   c.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (c adminCookies) clearEmailUndo(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     emailUndoCookieName,
		Value:    "",
		Path:     emailUndoPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// readEmailUndo lifts "<csrf>.<token>" off the request; anything else is absent.
func readEmailUndo(r *http.Request) (csrf, token string, ok bool) {
	ck, err := r.Cookie(emailUndoCookieName)
	if err != nil || ck.Value == "" {
		return "", "", false
	}
	csrf, token, found := strings.Cut(ck.Value, ".")
	if !found || csrf == "" || token == "" {
		return "", "", false
	}
	return csrf, token, true
}

// MountEmailUndo mounts the undo link's page and press -- GET and POST emailUndoPath -- on r, outside the
// panel's chains and by its own call, as the invitation page is (MountInviteAcceptance): the undo is
// SESSIONLESS (ADR 0044 §5.3) -- neither the panel's protect chain nor its writing chain is its gate, so
// neither is the suspension ring -- and it is no panel write of any one business (ADR 0025's census walks
// the panel). Its own gates: the flood ceiling, Origin, the synchronizer cookie, the per-address budget --
// and, only while TAPPA_ADMIN_IDENTITY is on, the routes exist at all (mountGated).
func (a *AdminAuth) MountEmailUndo(r chi.Router) {
	a.mountGated(r, adminauth.SurfaceAccountIdentity, func(r chi.Router) {
		r.Get(emailUndoPath, a.emailUndoPage)
		r.Post(emailUndoPath, a.emailUndo)
	})
}

// emailUndoMount is the undo page as an httpx.Mounter of its own (cmd/tappa's router mounts it beside
// the panel).
type emailUndoMount struct{ a *AdminAuth }

// Mount mounts the undo page.
func (m emailUndoMount) Mount(r chi.Router) { m.a.MountEmailUndo(r) }

// EmailUndo is the undo page, for the application router.
func (a *AdminAuth) EmailUndo() httpx.Mounter { return emailUndoMount{a: a} }

// emailUndoPage serves GET emailUndoPath. With ?t= it moves the token into the cookie and answers 303 to
// the bare path; bare, it draws the page and its one press. It reads and writes nothing in the database
// (R19: a mail client's prefetch must not spend the link, nor learn whether it is live).
func (a *AdminAuth) emailUndoPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if a.flooded(w, r, clientIP(r), "admin_email_undo_page") {
		return
	}
	if t := r.URL.Query().Get(adminauth.UndoEmailParam); t != "" {
		if strings.Contains(t, ".") || len(t) > 200 || !cookieSafe(t) {
			a.render(w, r, http.StatusOK, pages.AdminEmailUndo(pages.EmailUndoView{Dead: true, SignInHref: adminLoginPath}))
			return
		}
		csrf, err := newCSRFToken()
		if err != nil {
			a.log.Error("panel address change undo: minting the csrf value failed", "err", err)
			a.renderProblem(w, r, http.StatusInternalServerError, problemEmailUndoServer)
			return
		}
		a.short.setEmailUndo(w, csrf+"."+t)
		a.redirect(w, emailUndoPath)
		return
	}
	csrf, _, ok := readEmailUndo(r)
	if !ok {
		a.renderProblem(w, r, http.StatusBadRequest, problemEmailUndoNoLink)
		return
	}
	a.render(w, r, http.StatusOK, pages.AdminEmailUndo(pages.EmailUndoView{CSRFToken: csrf, SignInHref: adminLoginPath}))
}

// emailUndo serves POST emailUndoPath: Origin, the synchronizer half of the cookie, the per-address
// budget, then identity_revert_email_change with the token half. It looks at no session -- a signed-in
// browser is treated like any other (the undo is the old mailbox's, ADR 0044 §5.3). On success the address
// it gave back gets one recovery link (K-E11 (a)).
func (a *AdminAuth) emailUndo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	ip := clientIP(r)
	if a.flooded(w, r, ip, "admin_email_undo") {
		return
	}
	if !a.sameOrigin(r) {
		a.log.Warn("panel address change undo refused: not same-origin", "ip", ip)
		a.renderProblem(w, r, http.StatusBadRequest, problemEmailUndoRestart)
		return
	}
	csrf, raw, ok := readEmailUndo(r)
	if !ok {
		a.renderProblem(w, r, http.StatusBadRequest, problemEmailUndoNoLink)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, emailUndoMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemEmailUndoRestart)
		return
	}
	if !constantTimeMatch(csrf, r.PostFormValue("csrf")) {
		a.log.Warn("panel address change undo refused: csrf mismatch", "ip", ip)
		a.renderProblem(w, r, http.StatusBadRequest, problemEmailUndoRestart)
		return
	}
	if within, n := a.emailUndoLimiter.TryCharge(ip); !within {
		if a.emailUndoLimiter.FirstOverLimit(n) {
			a.log.Warn("panel address change undo rate limited", "scope", "address", "ip", ip)
		}
		a.renderProblem(w, r, http.StatusTooManyRequests, problemEmailUndoTooMany)
		return
	}
	restored, err := a.admins.UndoEmailChange(r.Context(), raw)
	if errors.Is(err, adminauth.ErrEmailUndoRefused) {
		a.short.clearEmailUndo(w)
		a.log.Info("panel address change undo refused", "ip", ip)
		a.render(w, r, http.StatusOK, pages.AdminEmailUndo(pages.EmailUndoView{Dead: true, SignInHref: adminLoginPath}))
		return
	}
	if err != nil {
		a.log.Error("panel: could not undo the address change", "ip", ip, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemEmailUndoServer)
		return
	}
	a.short.clearEmailUndo(w)
	a.log.Info("panel address change undone", "ip", ip)
	a.notices.addressRestored(r, restored.Address)
	a.render(w, r, http.StatusOK, pages.AdminEmailUndo(pages.EmailUndoView{Done: true, SignInHref: adminLoginPath}))
}
