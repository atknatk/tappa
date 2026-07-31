// Package handler is the HTTP surface: parse the request, call a domain, render
// the answer (CLAUDE.md §3). No business rule and no SQL lives here — the
// activation decision is internal/invite's, the session is internal/session's,
// and the trail is internal/audit's.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/web/templates/pages"
)

// Consumer-side interfaces (§7): this package declares the narrow slice it needs
// of each dependency, so the handlers can be tested against fakes and so a
// producer cannot widen what the HTTP layer may do.
type (
	inviteManager interface {
		Lookup(ctx context.Context, c invite.Code) (invite.Context, error)
		Activate(ctx context.Context, c invite.Code) (invite.Activation, error)
		ActivationContext(ctx context.Context, tenantID, employeeID uuid.UUID) (invite.Context, error)
	}
	sessionManager interface {
		Issue(ctx context.Context, p session.IssueParams) (session.Issued, error)
		Verify(ctx context.Context, t session.Token) (session.Resolved, error)
		RevokeAllForEmployee(ctx context.Context, tenantID, employeeID uuid.UUID) (int, error)
	}
	auditRecorder interface {
		Record(ctx context.Context, e audit.Event) (uuid.UUID, error)
	}
)

// Audit actions written by this flow. The vocabulary is free text by schema
// decision (00005), so the constants are the vocabulary.
const (
	ActionActivationCompleted = "activation.completed"
	ActionActivationFailed    = "activation.failed"
	ActionActivationLimited   = "activation.rate_limited"
	ActionActivationBlocked   = "activation.blocked"
	ActionDeviceReplaced      = "activation.device_replaced"
)

// Activation serves the three screens of the invite flow.
//
// ROUTES AND WHY THERE ARE THREE:
//
//	GET  /activate       the link the employee was sent. Validates the code, moves
//	                     it into an HttpOnly cookie and redirects to itself with a
//	                     clean URL; then renders the notice + Wi-Fi step + consent.
//	POST /api/activate   spends the code, issues the session, sets the cookie.
//	GET  /activate/done  the confirmation, identified by the NEW SESSION COOKIE.
//
// The third exists so the POST can answer 303 instead of rendering: a refresh on
// a rendered POST would re-submit a code that is now spent and show an error to
// someone who did nothing wrong. It also proves, at the only moment it is cheap
// to find out, that the browser actually kept the session cookie.
//
// §5 ROW 3 — SCOPE NOTE, NOT A CLAIM. The tap page (`GET /t`, M5-04) is the thing
// that redirects a session-less visitor here "without writing a record". That
// endpoint does not exist yet, so what M5-02 delivers is the destination: an
// activation page that exists and is reachable. Wiring the redirect is M5-04's
// work and is NOT done here.
//
// ⚠️ "WRITES NOTHING UNTIL A CODE IS SPENT" IS FALSE and an earlier version of
// this comment said it. Measured: GET /activate?code=<consumed> writes an
// activation.failed row without consuming anything. That WRITE IS CORRECT — §4.6
// wants a refused attempt to leave a trace — so it is the sentence that was
// wrong, not the behaviour. What §5 row 3 actually forbids is a TRANSACTION for a
// tap with no session, and this flow writes none: nothing here touches
// `transactions`.
type Activation struct {
	invites  inviteManager
	sessions sessionManager
	audit    auditRecorder
	// cookies writes the SESSION cookie (internal/session owns its attributes).
	cookies session.Cookies
	// codes writes the short-lived ACTIVATION cookie carrying the invite code.
	codes codeCookies
	// retentionYears and showConfigNotice feed the GDPR Art. 13 notice. The
	// number is configuration, never a constant in this repo — see
	// config.Config.RetentionYears.
	retentionYears   int
	showConfigNotice bool
	// baseURL is this deployment's own origin, for the Origin header check.
	baseURL string

	// See ratelimit.go for why there are three and what each one may refuse.
	floodLimiter   *limiter
	unknownLimiter *limiter
	inviteLimiter  *limiter

	log *slog.Logger
}

// NewActivation wires the flow. Every dependency is required: a nil recorder
// would silently drop the §4.6 trail, and a nil manager cannot fail safely.
func NewActivation(inv inviteManager, sess sessionManager, rec auditRecorder, cfg *config.Config, log *slog.Logger) (*Activation, error) {
	switch {
	case inv == nil:
		return nil, errors.New("handler: nil invite manager")
	case sess == nil:
		return nil, errors.New("handler: nil session manager")
	case rec == nil:
		return nil, errors.New("handler: nil audit recorder")
	case cfg == nil:
		return nil, errors.New("handler: nil config")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Activation{
		invites:          inv,
		sessions:         sess,
		audit:            rec,
		cookies:          session.NewCookies(cfg),
		codes:            newCodeCookies(cfg),
		retentionYears:   cfg.RetentionYears,
		showConfigNotice: !cfg.IsProd(),
		baseURL:          originOf(cfg.BaseURL),
		floodLimiter:     newLimiter(floodLimit, floodPeriod),
		unknownLimiter:   newLimiter(unknownLimit, unknownPeriod),
		inviteLimiter:    newLimiter(inviteFailureLimit, inviteFailurePeriod),
		log:              log,
	}, nil
}

// Mount registers the routes on r.
func (a *Activation) Mount(r chi.Router) {
	r.Get("/activate", a.Page)
	r.Post("/activate", a.Continue)
	r.Get("/activate/done", a.Done)
	r.Post("/api/activate", a.Submit)
}

// The failure screens. They are PACKAGE-LEVEL CONSTANTS, and problemBadLink is
// ONE value used for four different internal outcomes (unknown code, expired
// code, consumed code, employee not activatable).
//
// THAT SHARING IS THE FEATURE (§4.7). Telling the visitor which one happened is
// an oracle: "expired" confirms a code existed, "unknown" says it never did, and
// someone holding a truncated or guessed link learns which half of the space
// they are in. Because all four render the same value with the same status, the
// bodies are byte-identical — asserted in activate_test.go rather than asserted
// here in a comment. The distinction is not lost, it is written to audit_log,
// which is where it belongs.
var (
	problemNoLink = pages.ProblemView{
		Title:   "You need your activation link",
		Message: "This page opens from the personal link your workplace sent you.",
		Hint:    "Ask your manager to send it again.",
	}
	problemBadLink = pages.ProblemView{
		Title:   "This link can't be used",
		Message: "It may already have been used, or it may have run out of time.",
		Hint:    "Ask your manager for a new one — it only takes a moment.",
	}
	problemTooMany = pages.ProblemView{
		Title:   "Too many attempts",
		Message: "We have stopped accepting activations from here for a few minutes.",
		Hint:    "Wait a little and open your link again.",
	}
	problemNoSession = pages.ProblemView{
		Title:   "Your browser didn't keep the sign-in",
		Message: "Tappa needs to remember this phone, and this browser is not storing that.",
		Hint:    "Turn off private browsing or allow cookies for this site, then open your link again.",
	}
	problemServer = pages.ProblemView{
		Title:   "Something went wrong on our side",
		Message: "Nothing was activated and nothing was recorded against you.",
		Hint:    "Try your link again in a minute.",
	}
)

// Page serves GET /activate.
//
// TWO SHAPES, ONE ROUTE:
//
//	/activate?code=…   validate, move the code into the cookie, 303 to /activate.
//	/activate          read the cookie and render the form.
//
// The redirect is what keeps the code out of the address bar, out of history and
// out of any Referer (cookies.go explains the rest of the reasoning).
func (a *Activation) Page(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.flooded(w, r, ip, "activate_page") {
		return
	}

	if raw := r.URL.Query().Get("code"); raw != "" {
		ictx, err := a.invites.Lookup(r.Context(), invite.ParseCode(raw))
		if a.overInviteBudget(w, r, ip, ictx) {
			return
		}
		if err != nil {
			a.rejectCode(w, r, ip, ictx, err, "activate_page")
			return
		}
		// MEASURE 3 (cookies.go): a CROSS-SITE navigation that would collide with
		// the session already on this phone gets no cookie. That is the exact
		// shape the audit drove — a victim's browser made to store a stranger's
		// code — and it is the case where refusing costs least, because a phone
		// carrying somebody else's live session is not a phone that is casually
		// activating. Everything else proceeds untouched, which matters because
		// the ORDINARY arrival is usually cross-site too: a link tapped in a chat
		// app or a mail client reports cross-site, while the same URL pasted into
		// the address bar or opened from a bookmark reports "none". Refusing every
		// cross-site arrival would therefore have refused the normal path.
		held, err := a.heldByOther(r, ictx.EmployeeID)
		if err != nil {
			a.log.Error("activation page: could not determine the phone's holder", "err", err)
			a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
			return
		}
		if held != nil && crossSite(r) {
			a.recordConflict(r.Context(), ip, ictx, "cross_site_session_conflict")
			a.log.Warn("cross-site activation blocked", "ip", ip,
				"held_employee_id", held.EmployeeID, "offered_employee_id", ictx.EmployeeID)
			a.render(w, r, http.StatusOK, pages.Confirm(pages.ConfirmView{
				Code:         raw,
				EmployeeName: ictx.FullName,
				EmployerName: ictx.TenantName,
			}))
			return
		}
		a.startActivation(w, r, raw)
		return
	}

	st, ok := a.codes.read(r)
	if !ok {
		a.renderProblem(w, r, http.StatusOK, problemNoLink)
		return
	}
	ictx, err := a.invites.Lookup(r.Context(), st.code)
	if a.overInviteBudget(w, r, ip, ictx) {
		return
	}
	if err != nil {
		a.rejectCode(w, r, ip, ictx, err, "activate_page")
		return
	}
	held, err := a.heldByOther(r, ictx.EmployeeID)
	if err != nil {
		a.log.Error("activation page: could not determine the phone's holder", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}
	a.renderForm(w, r, http.StatusOK, ictx, st, formState{heldBy: held})
}

// Continue is the same-site confirmation step behind the cross-site guard in
// Page. It exists so that a phone which already carries somebody's session does
// not get a stranger's code planted on it by a single cross-site navigation —
// completing the switch takes a deliberate click on OUR page.
//
// ⚠️ IT IS NOT "THE ONLY WAY PAST" THE GUARD, and saying so would repeat the
// mistake this round was called for. The guard fires on `crossSite(r)`, which
// reads Sec-Fetch-Site and treats an ABSENT header as same-site (see crossSite
// for why that direction is the safe default). So a browser that does not send
// the header reaches the ordinary path directly, without this page. That is the
// documented fail-open in measure 3, and it is why measures 1 and 2 — which
// depend on no header at all — are the load-bearing ones.
//
// It is a POST because the ORIGIN HEADER IS SENT ON CROSS-ORIGIN POSTS by every
// browser this product targets, which makes sameOrigin a real check here rather
// than an optimistic one. A GET would have no such header.
func (a *Activation) Continue(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.flooded(w, r, ip, "activate_continue") {
		return
	}
	if !a.sameOrigin(r, true) {
		// STRICT here, unlike Submit: this endpoint exists only to gate a switch
		// that is already suspicious, and every browser that can reach it sends
		// Origin on a cross-origin POST. Refusing an unlabelled request costs an
		// ancient browser a device switch; allowing it would give the attack its
		// step back.
		a.anonFail(ip, "activate_continue", "not same-origin")
		a.renderProblem(w, r, http.StatusBadRequest, problemBadLink)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemServer)
		return
	}
	raw := r.PostFormValue("code")
	if raw == "" {
		a.renderProblem(w, r, http.StatusBadRequest, problemNoLink)
		return
	}
	ictx, err := a.invites.Lookup(r.Context(), invite.ParseCode(raw))
	if a.overInviteBudget(w, r, ip, ictx) {
		return
	}
	if err != nil {
		a.rejectCode(w, r, ip, ictx, err, "activate_continue")
		return
	}
	a.startActivation(w, r, raw)
}

// startActivation mints a synchronizer token, parks it with the code in the
// cookie and redirects to a clean URL.
func (a *Activation) startActivation(w http.ResponseWriter, r *http.Request, raw string) {
	token, err := newCSRFToken()
	if err != nil {
		// The message says "csrf" rather than the word redline R7 watches for:
		// the scanner matches that word anywhere inside a log call, and an innocent
		// MESSAGE tripping CI is how a net gets loosened under pressure (the same
		// reasoning that narrowed the `code` pattern in M5-02 phase A). Nothing but
		// the error is logged here.
		a.log.Error("activation: minting the csrf value failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}
	a.codes.set(w, token, raw)
	a.redirect(w, r, "/activate")
}

// Submit serves POST /api/activate — the endpoint that spends the code.
//
// ORDER OF CHECKS, and why each one is where it is:
//
//	· per-IP window      BEFORE any database work; it is the only position from
//	                     which a limiter can actually shed load.
//	· activation cookie  no cookie means no code (and, thanks to SameSite=Lax,
//	                     also means a cross-site POST) — refused before anything.
//	· lookup             resolves the tenant. Everything after this point can be
//	                     written to audit_log, because a tenant is known.
//	· per-invite window  now attributable, so a trip writes a row rather than
//	                     vanishing (§4.6).
//	· consent            the GDPR gate. Nothing is consumed without it, and the
//	                     refusal is itself recorded.
//	· activate           the single atomic statement (§4.4 shape).
//	· revoke, then issue ORDER IS LOAD-BEARING: RevokeAllForEmployee kills every
//	                     live session of the employee, so issuing first would kill
//	                     the session just issued.
func (a *Activation) Submit(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.flooded(w, r, ip, "activate_submit") {
		return
	}

	st, ok := a.codes.read(r)
	if !ok {
		// No cookie: either the link was never opened on this browser, or the
		// POST came from somewhere else entirely. Same answer for both.
		a.anonFail(ip, "activate_submit", "no activation cookie")
		a.renderProblem(w, r, http.StatusBadRequest, problemNoLink)
		return
	}
	code := st.code

	if !a.sameOrigin(r, false) {
		// A belt over the synchronizer token's braces. Not strict (an absent
		// Origin is allowed) because the token below is the load-bearing check
		// and an over-eager origin rule would break browsers that omit the
		// header; the strict version lives on Continue, where it can afford to be.
		a.anonFail(ip, "activate_submit", "foreign origin")
		a.renderProblem(w, r, http.StatusBadRequest, problemBadLink)
		return
	}

	ictx, lookErr := a.invites.Lookup(r.Context(), code)
	// The per-invite gate runs as soon as the invitation is IDENTIFIED, which is
	// the earliest possible moment — and it runs whether the lookup succeeded or
	// failed, because a loop replaying a dead code is exactly what it is for.
	if a.overInviteBudget(w, r, ip, ictx) {
		return
	}
	if lookErr != nil {
		a.rejectCode(w, r, ip, ictx, lookErr, "activate_submit")
		return
	}

	if err := r.ParseForm(); err != nil {
		a.failAttempt(r.Context(), ip, ictx, "malformed_form", againstInvite)
		a.renderProblem(w, r, http.StatusBadRequest, problemServer)
		return
	}

	// MEASURE 1 (cookies.go): the synchronizer token. An attacker can plant the
	// cookie with a cross-site navigation, but the token the server minted while
	// doing so went into the VICTIM's browser, behind HttpOnly and the same-origin
	// policy — a token the attacker obtained by opening the same link in their own
	// browser belongs to their own cookie and does not match this one. So the
	// forged submission fails; what remains is PHISHING (the victim submitting the
	// form themselves, on a page bearing a stranger's name), which measure 2
	// addresses and which is NOT claimed to be closed here.
	//
	// A mismatch is an ATTACK signal, not a user slip, so it does count against
	// the invitation's budget (see budgetScope).
	if !st.csrfMatches(r.PostFormValue("csrf")) {
		a.failAttempt(r.Context(), ip, ictx, "csrf_mismatch", againstInvite)
		a.log.Warn("activation submit rejected: csrf mismatch", "ip", ip, "invite_id", nonNil(ictx.InviteID))
		a.renderProblem(w, r, http.StatusBadRequest, problemBadLink)
		return
	}

	// MEASURE 2 (cookies.go): never bind this phone to a different employee
	// without being told to. The session already on the phone is EVIDENCE of who
	// it belongs to; overwriting it silently is what turned the audit's fixation
	// into a lost shift for someone who would never have noticed.
	held, err := a.heldByOther(r, ictx.EmployeeID)
	if err != nil {
		// FAIL CLOSED: we cannot tell whether this would replace someone, so we do
		// not do it. Nothing is consumed and the visitor is told to try again.
		a.log.Error("activation submit: could not determine the phone's holder", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}
	if held != nil && r.PostFormValue("switch") == "" {
		a.recordConflict(r.Context(), ip, ictx, "session_conflict_unconfirmed")
		a.log.Warn("activation would replace another employee's session", "ip", ip,
			"held_employee_id", held.EmployeeID, "offered_employee_id", ictx.EmployeeID)
		a.renderForm(w, r, http.StatusConflict, ictx, st, formState{heldBy: held, switchMissing: true})
		return
	}

	if r.PostFormValue("consent") == "" {
		// A USER SLIP, not an attack: forgetting the box must not spend the
		// invitation's budget, or a confused employee locks themselves out of
		// their own link with a 429. It still counts against the IP window (which
		// is 60 and would take real determination to reach) and it is still
		// recorded — §4.6 wants the refusal visible, it does not want it punished.
		a.failAttempt(r.Context(), ip, ictx, "consent_missing", againstIPOnly)
		a.renderForm(w, r, http.StatusBadRequest, ictx, st, formState{
			heldBy: held, consentMissing: true, switchConfirmed: held != nil,
		})
		return
	}

	act, err := a.invites.Activate(r.Context(), code)
	if err != nil {
		a.rejectCode(w, r, ip, act.Context, err, "activate_submit")
		return
	}

	// A second device replaces the first: kill the old sessions BEFORE issuing
	// the new one. See the decision note below.
	if act.SecondDeviceReplaced {
		n, err := a.sessions.RevokeAllForEmployee(r.Context(), act.TenantID, act.EmployeeID)
		if err != nil {
			// The invitation is already spent, so failing here would leave the
			// employee unable to retry with a dead code. The activation stands,
			// the old sessions do not get revoked, and that is LOUD: an error log
			// plus an audit row saying the revocation failed.
			a.log.Error("second activation: revoking previous sessions failed",
				"employee_id", act.EmployeeID, "err", err)
			a.record(r.Context(), audit.Event{
				TenantID: act.TenantID,
				Action:   ActionDeviceReplaced,
				Target:   act.EmployeeID.String(),
				Detail: activationDetail{
					InviteID: act.InviteID.String(),
					Outcome:  "revoke_failed",
					Reason:   "previous sessions could not be revoked",
				},
			})
		} else {
			a.record(r.Context(), audit.Event{
				TenantID: act.TenantID,
				Action:   ActionDeviceReplaced,
				Target:   act.EmployeeID.String(),
				Detail: activationDetail{
					InviteID:        act.InviteID.String(),
					Outcome:         "ok",
					SessionsRevoked: n,
				},
			})
		}
	}

	issued, err := a.sessions.Issue(r.Context(), session.IssueParams{
		TenantID:   act.TenantID,
		EmployeeID: act.EmployeeID,
		DeviceInfo: coarseDevice(r.UserAgent()),
	})
	if err != nil {
		// The code is spent and the employee is active, but they have no cookie.
		// Recovery is a new invitation, so this must be visible: error log AND an
		// audit row. It is NOT presented as "your link is invalid" — that would
		// send the employee to their manager with the wrong story.
		a.log.Error("activation: issuing the session failed", "employee_id", act.EmployeeID, "err", err)
		a.record(r.Context(), audit.Event{
			TenantID: act.TenantID,
			Action:   ActionActivationFailed,
			Target:   act.EmployeeID.String(),
			Detail: activationDetail{
				InviteID: act.InviteID.String(),
				Outcome:  "session_issue_failed",
				Reason:   "the invitation was consumed but no session could be issued",
			},
		})
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}

	if err := a.cookies.Set(w, issued.Token); err != nil {
		// SYMMETRIC WITH THE BRANCH ABOVE, and an audit was right that it was not.
		// Both leave the SAME state — the invitation is spent, the employee is
		// 'active', and nobody holds a usable session — so both must leave the same
		// trail (§4.6). Only one of them wrote a row, which made the trail's shape
		// depend on which of two equivalent failures happened.
		//
		// This branch is unreachable in practice (Cookies.Set fails only on an
		// empty token, and Issue has already refused to return one), and that is
		// exactly why the asymmetry survived review: nobody could trip it. An
		// unreachable branch still teaches the next reader what the rule is.
		a.log.Error("activation: writing the session cookie failed", "employee_id", act.EmployeeID, "err", err)
		a.record(r.Context(), audit.Event{
			TenantID: act.TenantID,
			Action:   ActionActivationFailed,
			Target:   act.EmployeeID.String(),
			Detail: activationDetail{
				InviteID: act.InviteID.String(),
				Outcome:  "session_cookie_failed",
				Reason:   "the invitation was consumed but the session cookie could not be written",
			},
		})
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}
	// The credential is spent; do not leave a dead one in the browser.
	a.codes.clear(w)

	a.record(r.Context(), audit.Event{
		TenantID: act.TenantID,
		Action:   ActionActivationCompleted,
		Target:   act.EmployeeID.String(),
		Detail: activationDetail{
			InviteID:     act.InviteID.String(),
			Outcome:      "ok",
			SecondDevice: act.SecondDeviceReplaced,
			SessionID:    issued.Session.ID.String(),
			Device:       issued.Session.DeviceInfo,
		},
	})

	dest := "/activate/done"
	if act.SecondDeviceReplaced {
		dest += "?replaced=1"
	}
	a.redirect(w, r, dest)
}

// Done serves GET /activate/done — the confirmation, identified by the session
// cookie that was just set.
//
// WHY IT READS THE SESSION RATHER THAN CARRYING STATE FORWARD: it makes the
// confirmation a genuine end-to-end check. If the browser silently dropped the
// cookie (private mode, a locked-down work profile), the employee finds out here,
// with a sentence that says what to change — instead of at the plaque tomorrow.
//
// A REVOKED session lands on the same "no session" screen, and that is correct
// HERE even though the ErrRevoked/ErrNoSession distinction matters enormously on
// the tap path (§5 rows 3 vs 4, session.Verify's API TRAP). The difference is that
// no attendance record is at stake on this page: there is nothing to write, so
// there is nothing to lose. The tap endpoint (M5-05) must branch on it and will.
func (a *Activation) Done(w http.ResponseWriter, r *http.Request) {
	// BEHIND THE SHIELD TOO. This endpoint writes nothing, so it cannot fill the
	// trail — but it does two database round trips per request (verify the
	// session, load the employee and the venue), and ratelimit.go calls the flood
	// budget "the DoS shield" without qualification. An unshielded read path in
	// the same flow made that sentence smaller than it sounded.
	ip := clientIP(r)
	if a.flooded(w, r, ip, "activate_done") {
		return
	}
	tok, err := a.cookies.Read(r)
	if err != nil {
		a.renderProblem(w, r, http.StatusOK, problemNoSession)
		return
	}
	res, err := a.sessions.Verify(r.Context(), tok)
	if err != nil {
		if errors.Is(err, session.ErrNoSession) || errors.Is(err, session.ErrRevoked) {
			a.renderProblem(w, r, http.StatusOK, problemNoSession)
			return
		}
		a.log.Error("activation done: verifying the session failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}

	ictx, err := a.invites.ActivationContext(r.Context(), res.TenantID, res.EmployeeID)
	if err != nil {
		a.log.Error("activation done: loading the employee failed", "employee_id", res.EmployeeID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}

	a.render(w, r, http.StatusOK, pages.Done(pages.DoneView{
		EmployeeName: ictx.FullName,
		LocationName: ictx.LocationName,
		WiFiSSID:     ictx.WiFiSSID,
		SecondDevice: r.URL.Query().Get("replaced") == "1",
	}))
}

// activationDetail is the audit payload for this flow.
//
// IT IS A PURPOSE-BUILT STRUCT, NOT A MAP — internal/audit's package doc explains
// why: there is no field here that could carry the invite code or its hash, so no
// later edit adds one by accident, and the §4.7 promise is a property of the type
// rather than of the author's memory. Everything in it is an opaque id, a fixed
// string from this file, or the coarse device label that sessions.device_info
// already stores.
type activationDetail struct {
	InviteID        string `json:"invite_id,omitempty"`
	Outcome         string `json:"outcome"`
	Reason          string `json:"reason,omitempty"`
	SecondDevice    bool   `json:"second_device,omitempty"`
	SessionsRevoked int    `json:"sessions_revoked,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	// Device is the coarse label ("iPhone Safari"), never a user agent
	// (device.go). It is here because ADR 0005 Y-D's detection signal is "N
	// employees activated from one device", and that report needs the field.
	Device string `json:"device,omitempty"`
}

// rejectCode is the single exit for every unusable-code outcome: it records the
// attempt where it can, counts it against both windows, and renders the ONE
// generic page.
//
// ictx may be the zero Context (ErrUnknownCode), which is exactly the case that
// cannot be audited — audit_log.tenant_id is NOT NULL and there is no tenant to
// attribute the attempt to (db/queries/audit.sql states this limit at length).
// Those attempts are counted and logged instead. That gap is real and is not
// claimed away.
func (a *Activation) rejectCode(w http.ResponseWriter, r *http.Request, ip string, ictx invite.Context, err error, where string) {
	switch {
	case errors.Is(err, invite.ErrUnknownCode):
		// No tenant: nothing can be written to audit_log, so this charges the
		// UNKNOWN budget, which bounds the log instead. Logged WITHOUT the code
		// and without its hash (§4.7).
		a.anonFail(ip, where, "unknown code")
	case errors.Is(err, invite.ErrCodeExpired):
		a.failAttempt(r.Context(), ip, ictx, "expired", againstInvite)
	case errors.Is(err, invite.ErrCodeUsed):
		a.failAttempt(r.Context(), ip, ictx, "already_used", againstInvite)
	case errors.Is(err, invite.ErrNotActivatable):
		a.failAttempt(r.Context(), ip, ictx, "employee_not_activatable", againstInvite)
	default:
		// A database or internal failure is NOT an invalid link: saying so would
		// send the employee to their manager for a new code that would fail the
		// same way, and would hide an outage behind a UX message.
		a.log.Error("activation lookup failed", "at", where, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemServer)
		return
	}
	a.renderProblem(w, r, http.StatusBadRequest, problemBadLink)
}

// budgetScope says which rate-limit windows a failure is charged to.
//
// THE DISTINCTION IS THE POINT (audit finding B4). Charging every failure to the
// invitation's window meant a legitimate employee who forgot the consent box ten
// times got a 429 on THEIR OWN link — the limiter punishing user error instead of
// abuse, and the "a legitimate flow structurally never touches the limit" claim
// was only true for a flawless flow. A slip now costs the (generous, 60-wide) IP
// window and nothing else; an attack signal — a bad form token, a replayed or
// expired code — still costs the invitation.
type budgetScope int

const (
	againstInvite budgetScope = iota // abuse: charge both windows
	againstIPOnly                    // user slip: charge only the wide IP window
)

// failAttempt records one ATTRIBUTABLE failure: audit row, the window the scope
// names, one log line without secrets.
//
// It charges the INVITE budget, not a per-IP one. That is what bounds audit_log:
// the rows this path writes all name a tenant and an invitation, so the
// invitation is the right meter — and metering them per IP was what let an
// anonymous flood starve a legitimate activation (ratelimit.go).
func (a *Activation) failAttempt(ctx context.Context, ip string, ictx invite.Context, reason string, scope budgetScope) {
	if scope == againstInvite && ictx.InviteID != uuid.Nil {
		a.inviteLimiter.charge(ictx.InviteID.String())
	}
	if ictx.TenantID == uuid.Nil {
		return
	}
	a.record(ctx, audit.Event{
		TenantID: ictx.TenantID,
		Action:   ActionActivationFailed,
		Target:   ictx.EmployeeID.String(),
		Detail: activationDetail{
			InviteID: nonNil(ictx.InviteID),
			Outcome:  "rejected",
			Reason:   reason,
		},
	})
	a.log.Info("activation attempt rejected", "reason", reason, "ip", ip,
		"employee_id", ictx.EmployeeID, "invite_id", nonNil(ictx.InviteID))
}

// overInviteBudget is the per-invitation gate. It reports true (and has already
// answered the request) when this invitation has burned through its window.
//
// IT IS CHECKED BEFORE THE FAILURE IS COUNTED, so exactly inviteFailureLimit
// failures are answered normally and the next one is refused — rather than the
// limit-th request being the one that trips, which is the off-by-one this shape
// avoids.
//
// A zero InviteID means the code resolved to nothing: there is no invitation to
// budget, and such attempts are bounded by the per-IP window alone (ratelimit.go
// explains why that is sound only because the code is 256 bits).
func (a *Activation) overInviteBudget(w http.ResponseWriter, r *http.Request, ip string, ictx invite.Context) bool {
	if ictx.InviteID == uuid.Nil {
		return false
	}
	key := ictx.InviteID.String()
	if a.inviteLimiter.allowed(key) {
		return false
	}
	// THE REFUSAL ITSELF IS CHARGED, AND THE ROW IS WRITTEN ONCE. An audit found
	// that this branch wrote a row per request and charged nothing: 400 POSTs
	// against one dead invitation produced 390 x 429 AND 400 audit rows, from a
	// client holding nothing but a spent link. audit_log is append-only at the
	// DATABASE level — measured: even tappa_owner gets "append-only table
	// audit_log: DELETE is forbidden" — so those rows are PERMANENT, and the trail
	// §4.6 exists to protect becomes the thing being filled with noise.
	//
	// firstOverLimit is true exactly once per window, so the trail records THAT
	// this invitation was throttled without recording every request that hit the
	// wall. The count keeps climbing, so the window still expires normally.
	n := a.inviteLimiter.charge(key)
	if a.inviteLimiter.firstOverLimit(n) {
		a.record(r.Context(), audit.Event{
			TenantID: ictx.TenantID,
			Action:   ActionActivationLimited,
			Target:   ictx.EmployeeID.String(),
			Detail: activationDetail{
				InviteID: key,
				Outcome:  "rate_limited",
				Reason:   "too many failed attempts against this invitation",
			},
		})
		a.log.Warn("activation rate limited", "scope", "invite", "invite_id", key, "ip", ip)
	}
	a.renderProblem(w, r, http.StatusTooManyRequests, problemTooMany)
	return true
}

// flooded is the entry gate on every handler: the flood ceiling, charged on every
// request. It reports true (and has already answered) when this address is over.
//
// IT WRITES NO AUDIT ROW, and that is a stated limit rather than an oversight:
// the check runs before the code is resolved, so no tenant is known, and
// resolving one first would defeat the only purpose of a pre-database shield.
// The trail for a flooded caller exists at the process log level, and the log
// line itself is written once per window so the shield cannot be turned into an
// unbounded disk writer.
//
// ⚠️ THIS IS THE ONE BUDGET THAT CAN REFUSE A VALID ACTIVATION. It takes
// floodLimit requests from the same address in the window, and behind a reverse
// proxy "the same address" is everyone (clientIP; M5-03 owns the fix).
func (a *Activation) flooded(w http.ResponseWriter, r *http.Request, ip, where string) bool {
	n := a.floodLimiter.charge(ip)
	if n <= floodLimit {
		return false
	}
	if a.floodLimiter.firstOverLimit(n) {
		a.log.Warn("activation flood ceiling reached", "ip", ip, "at", where,
			"limit", floodLimit, "period", floodPeriod.String())
	}
	a.renderProblem(w, r, http.StatusTooManyRequests, problemTooMany)
	return true
}

// anonFail records an UNATTRIBUTABLE refusal — one with no tenant, so no
// audit_log row is possible (db/queries/audit.sql states why no "system tenant"
// is invented). It charges the unknown budget, which bounds the PROCESS LOG:
// past the limit the branch answers identically and stops writing lines, after
// one final line saying it has.
//
// It gates nothing. That is deliberate and is the fix for the lockout an audit
// measured: an unknown-code flood must not be able to refuse a request that
// carries a real invitation.
func (a *Activation) anonFail(ip, where, reason string) {
	n := a.unknownLimiter.charge(ip)
	switch {
	case n <= unknownLimit:
		a.log.Info("activation attempt refused", "reason", reason, "at", where, "ip", ip)
	case a.unknownLimiter.firstOverLimit(n):
		a.log.Warn("activation: further unattributable refusals from this address will not be logged this window",
			"ip", ip, "at", where, "limit", unknownLimit, "period", unknownPeriod.String())
	}
}

// record writes an audit event and never lets its failure change the outcome of
// the request — but never swallows it either (§7): a trail that cannot be written
// is an ERROR-level event, because it means §4.6 is not being kept.
func (a *Activation) record(ctx context.Context, e audit.Event) {
	if _, err := a.audit.Record(ctx, e); err != nil {
		a.log.Error("audit write failed", "action", e.Action, "err", err)
	}
}

func nonNil(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// formState is the transient, per-render half of the activation form: what went
// wrong last time and whose session is in the way.
type formState struct {
	consentMissing bool
	switchMissing  bool
	// switchConfirmed keeps a confirmed switch checked when the form comes back
	// for an unrelated reason, so the employee does not have to tick it twice.
	switchConfirmed bool
	heldBy          *heldSession
}

func (a *Activation) renderForm(w http.ResponseWriter, r *http.Request, status int, ictx invite.Context, st activationState, fs formState) {
	v := pages.ActivateView{
		EmployeeName:     ictx.FullName,
		EmployerName:     ictx.TenantName,
		LocationName:     ictx.LocationName,
		WiFiSSID:         ictx.WiFiSSID,
		RetentionYears:   a.retentionYears,
		ShowConfigNotice: a.showConfigNotice,
		SecondDevice:     ictx.SecondDevice(),
		ConsentMissing:   fs.consentMissing,
		SwitchMissing:    fs.switchMissing,
		SwitchConfirmed:  fs.switchConfirmed,
		// CSRFToken is rendered ON PURPOSE — it is the synchronizer token, not a
		// secret in the §4.7 sense. The invite code, which IS one, is not here.
		CSRFToken: st.csrf,
	}
	if fs.heldBy != nil {
		v.HeldByName = fs.heldBy.FullName
		v.HeldByEmployer = fs.heldBy.TenantName
	}
	a.render(w, r, status, pages.Activate(v))
}

// heldSession is who this phone currently belongs to, as far as its session
// cookie says.
type heldSession struct {
	EmployeeID uuid.UUID
	FullName   string
	TenantName string
}

// heldBy resolves the session cookie on the request, if any.
//
// A REVOKED session counts as NOT HELD. That is right here and would be wrong on
// the tap path: §5 rows 3 and 4 differ because a tap must leave a record, and
// session.Verify's API TRAP is about exactly that. Nothing is recorded on this
// page, so a dead session is simply not evidence of anybody.
//
// A DATABASE FAILURE IS NOT "not held" — it is "I do not know", and this
// function says so by returning an error. An earlier version collapsed the two
// and an audit measured the consequence: with the session lookup failing, Submit
// skipped the 409 and issued a session, i.e. the takeover protection switched
// itself off exactly when the system was already unwell. Failing CLOSED costs a
// legitimate activation a "try again" during an outage; failing open costs
// somebody their shift.
func (a *Activation) heldBy(r *http.Request) (*heldSession, error) {
	tok, err := a.cookies.Read(r)
	if err != nil {
		return nil, nil // no cookie at all: nobody holds this phone
	}
	res, err := a.sessions.Verify(r.Context(), tok)
	if err != nil {
		if errors.Is(err, session.ErrNoSession) || errors.Is(err, session.ErrRevoked) {
			// An unknown or revoked session is not evidence of anybody. That is
			// right HERE and would be wrong on the tap path, where §5 rows 3 and 4
			// differ because a tap must leave a record; nothing is recorded here.
			return nil, nil
		}
		return nil, fmt.Errorf("handler: reading the phone's current session: %w", err)
	}
	held := &heldSession{EmployeeID: res.EmployeeID, FullName: "another employee"}
	ictx, err := a.invites.ActivationContext(r.Context(), res.TenantID, res.EmployeeID)
	if err != nil {
		// The CONFLICT is already established — we know a different employee holds
		// this phone — so the failure to NAME them is not a reason to let the
		// takeover through. Keep the conflict, use a neutral label, be loud.
		a.log.Error("activation: naming the phone's current holder failed", "err", err)
		return held, nil
	}
	held.FullName = ictx.FullName
	held.TenantName = ictx.TenantName
	return held, nil
}

// heldByOther returns the current session holder only when it is SOMEONE ELSE.
// Re-activating the same employee on the same phone is not a conflict. An error
// means the answer is unknown and the caller must fail closed.
func (a *Activation) heldByOther(r *http.Request, offered uuid.UUID) (*heldSession, error) {
	held, err := a.heldBy(r)
	if err != nil {
		return nil, err
	}
	if held == nil || held.EmployeeID == offered {
		return nil, nil
	}
	return held, nil
}

// recordConflict writes the trail for a refused takeover. The row names only
// ids: the OTHER tenant must not learn who this phone belonged to, so the held
// employee's name and employer stay out of it (they are shown to the person
// holding the phone, who already knows).
//
// IT CHARGES THE INVITE WINDOW, for the reason overInviteBudget spells out: an
// audit measured 500 cross-site GETs producing 500 permanent audit rows and ZERO
// counted failures. A blocked takeover is a failure, and an unbounded writer into
// an append-only table is a denial-of-service against the trail itself. Once the
// window trips, overInviteBudget answers the next request and writes nothing.
//
// The invitation is the meter rather than the address because these rows all name
// one — metering them per IP is what let an anonymous flood starve a legitimate
// activation from the same address (ratelimit.go).
func (a *Activation) recordConflict(ctx context.Context, ip string, ictx invite.Context, reason string) {
	_ = ip // the address is logged by the caller; the METER is the invitation
	if ictx.TenantID == uuid.Nil {
		return
	}
	if ictx.InviteID != uuid.Nil {
		a.inviteLimiter.charge(ictx.InviteID.String())
	}
	a.record(ctx, audit.Event{
		TenantID: ictx.TenantID,
		Action:   ActionActivationBlocked,
		Target:   ictx.EmployeeID.String(),
		Detail: activationDetail{
			InviteID: nonNil(ictx.InviteID),
			Outcome:  "blocked",
			Reason:   reason,
		},
	})
}

// crossSite reports whether the browser said this navigation came from another
// site.
//
// LIMIT, and it is why this is measure 3 of 3 rather than the defence:
// Sec-Fetch-Site is absent on older browsers and on anything that is not a
// browser, and an ABSENT header is read as same-site here — fail-open, because
// reading it as cross-site would break every activation from a browser that does
// not send it. The synchronizer token and the session check do not depend on any
// header.
// The comparison is CASE-INSENSITIVE. The header's grammar is lowercase, but so
// is a URL scheme, and M5-01 already shipped a bug where `HTTPS://` slipped past
// a case-sensitive prefix test and bought a weaker cookie. Measured before the
// fix: "Cross-Site" and "CROSS-SITE" both planted the cookie that "cross-site"
// refused.
func crossSite(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "cross-site", "cross-origin":
		return true
	default:
		return false
	}
}

// sameOrigin checks the Origin header against this deployment's own.
//
// Origin IS sent by every current browser on a cross-origin POST, which makes
// this a real check for form submissions (unlike Sec-Fetch-Site, which older
// browsers omit entirely). The `strict` argument decides what an ABSENT header
// means: Continue refuses it, Submit allows it and leans on the synchronizer
// token instead. Both positions are stated at their call sites.
//
// A malformed configured BaseURL makes this check pass; config.Load does not
// validate the URL's shape and this function is not the place to start (the same
// note session.NewCookies carries).
func (a *Activation) sameOrigin(r *http.Request, strict bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		if !strict {
			return true
		}
		// Fall back to the fetch metadata when there is no Origin at all.
		switch r.Header.Get("Sec-Fetch-Site") {
		case "same-origin", "same-site":
			return true
		default:
			return false
		}
	}
	return strings.EqualFold(strings.TrimRight(origin, "/"), strings.TrimRight(a.baseURL, "/"))
}

func (a *Activation) renderProblem(w http.ResponseWriter, r *http.Request, status int, v pages.ProblemView) {
	a.render(w, r, status, pages.Problem(v))
}

// render writes one component with the headers every screen in this flow needs.
//
// Cache-Control: no-store on ALL of them, including the failure pages. These
// responses are personal (a name, an employer, a venue) and they are reached
// from a credential-bearing URL; a shared phone's back button must not resurrect
// them from a cache, and no intermediary should keep a copy.
func (a *Activation) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		// The status line is already on the wire, so there is nothing to send but
		// a log line. Never swallowed (§7).
		a.log.Error("rendering the activation page failed", "err", err)
	}
}

// redirect answers 303 See Other. 303 rather than 302 on purpose: it tells the
// browser to follow with GET, which is what turns the POST into a plain page load
// and makes a refresh harmless.
func (a *Activation) redirect(w http.ResponseWriter, r *http.Request, to string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// clientIP is the rate-limiter key: the TCP peer address, and NOTHING ELSE.
//
// IT DELIBERATELY IGNORES X-Forwarded-For. Reading that header without knowing
// which hops may set it hands every caller a free choice of rate-limit bucket —
// and, worse on this product, a free choice of "where I am" (§5: an IP match is
// worth 50 trust points). The bounded resolver that consults cfg.TrustedProxies
// is M5-03's job, and internal/httpx/router.go carries the same note about why
// chi's RealIP is not used.
//
// ⚠️ NAMED HAND-OFF TO M5-03, because until it lands this key is not just coarse,
// it is SHARED. The planned deployment is a single VPS behind a reverse proxy, so
// r.RemoteAddr is the PROXY for every request on earth: one key for the whole
// internet. Concretely, until M5-03 resolves the real client address —
//
//   - the flood ceiling (the one budget that can refuse a VALID activation) is
//     global rather than per caller, so a determined outsider can spend it and
//     make activation unavailable for everyone until the window rolls;
//   - the unknown budget's log suppression is global too, so one noisy source
//     silences the logging of everybody else's anonymous refusals.
//
// What the budget split already removed is the CHEAP version of that: sixty bad
// links no longer refuse a real one (ratelimit.go). What remains needs the real
// client IP, and that is M5-03's work, not a number that can be tuned here.
// M5-03 must also revisit floodLimit, which was chosen for per-address traffic.
// originOf reduces a configured base URL to its scheme://host origin, which is
// the form an Origin header takes. A BaseURL with a path ("https://x/app") would
// otherwise never match one.
func originOf(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(base, "/")
	}
	return u.Scheme + "://" + u.Host
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
