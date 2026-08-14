package handler

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/web/templates/pages"
)

// AdminReset serves panel PASSWORD RECOVERY: the "I cannot get in" form, and the
// page a reset link opens.
//
// ROUTES, AND WHY EACH EXISTS:
//
//	GET  /admin/reset       the request form. Mints the synchronizer token.
//	POST /admin/reset       resolves the address, mints a link per identity inside
//	                        adminauth.ResetWindow, hands each to the delivery
//	                        channel, and answers ONE page whatever it found.
//	GET  /admin/reset/new   what the link opens. Moves the token out of the URL into
//	                        an HttpOnly cookie and redirects to a clean address.
//	POST /admin/reset/new   spends the token, writes the new digest, revokes every
//	                        session, and sends the person to the sign-in form.
//
// 🔴 IT IS A SEPARATE TYPE FROM AdminAuth, AND THE REASON IS THE OPPOSITE OF
// legaladmin.go's. Every panel SECTION lives on AdminAuth because it needs
// a.chrome() and the identity the Protect chain resolved. This flow has neither: it
// is reachable with no credential at all, renders no panel shell, and must not touch
// the session resolver. What it shares with AdminAuth is one constant (adminCSP) and
// two route strings, which is what internal/handler/signup.go — the other public,
// unauthenticated, password-carrying surface in this product — also shares, as its
// own type. Bolting it onto AdminAuth would add two more constructor parameters to a
// function that already takes sixteen, and would put an anonymous route inside a type
// whose every other method assumes a signed-in operator.
//
// EVERYTHING IS UNDER /admin FOR ONE MECHANICAL REASON: the short-lived cookies this
// flow sets are scoped to adminauth.CookiePath, exactly like the sign-in flow's, so
// they are never sent to the tap surface. A route outside that prefix would arrive
// without them.
//
// 🔴 THREE OF THIS FEATURE'S FIVE ACCEPTANCE CRITERIA — AND TWO OF ITS DEFENCES —
// ARE UNREACHABLE IN THE SHIPPED
// CONFIGURATION, AND THAT SENTENCE BELONGS HERE RATHER THAN SPREAD ACROSS FOUR FILES.
// With TAPPA_RESET_DELIVERY=none — today's only legal value, because Q02 is
// unanswered — no link is ever issued, so:
//
//	criterion 1, harm (a)   the recovery-denial the request budget bounds cannot
//	                        happen: nothing is minted, so nothing is retired.
//	criterion 2, audit half  no link exists to be replayed or refused, so the
//	                        attributable audit_log rows are never written.
//	                        ⚠️ THE PROCESS-LOG HALF IS LIVE, AND IT IS LIVE BECAUSE
//	                        IT WAS FIXED — this line claimed it while the two 404s
//	                        below returned before either writer. Measured on the real
//	                        binary at debug level: four requests, zero lines. It is
//	                        true now because logUndeliverableAttempt writes one,
//	                        bounded, before each 404 — not because this sentence says
//	                        so.
//	criterion 3             entirely dead: there is no delivery, so "the link goes
//	                        only to the address on the administrator's own row" is
//	                        vacuously true in production.
//
// Their evidence is therefore tests — fakes in adminreset_test.go, and the
// panelHarness's recordingChannel against real Postgres in adminreset_db_test.go —
// and NOT observation of the running product. Criteria 4 and 5 are live either way:
// the *Params rule is a scan over source, and the identical-answer rule is exercised
// by the short-circuited POST that every visitor gets today.
//
// ⚠️ TWO MORE THINGS ARE DEAD IN THE SHIPPED CONFIGURATION AND THIS LEDGER MISSED
// THEM, WHICH IS WORTH ADDING BECAUSE THE LEDGER'S ONLY JOB IS TO BE COMPLETE. Both
// live in Submit, BEHIND the fail-closed 404 that now stands at its first statement:
//
//	adminResetSubmitLimit's gate  Measured: 25 x POST /admin/reset/new in the shipped
//	                              configuration gave codes {404: 25}, Consume 0, and
//	                              ZERO scope=submit rate-limit lines. The budget never
//	                              charges because nothing reaches it.
//	cookieSafe's refusal          Measured: ?t=aaa%3Bbbb answers 404, and it is the
//	                              canDeliver short-circuit answering, not the byte
//	                              check.
//
// NEITHER IS A HOLE, and the reason is the direction: a 404 at the first statement is
// a STRICTLY NARROWER bound than either of them — it refuses everything they would
// have refused, plus everything they would have allowed, before any bcrypt is paid or
// any cookie is written. They come alive together with the three criteria above, on
// the day a channel is configured, and they are tested against fakes today for
// exactly that day.
//
// THIS IS COUNTED, NOT CLAIMED CLOSED. It is the second stopping rule's shape: a
// measured gap is safer than a closure nobody verified. Whoever answers Q02 turns all
// three on with one case in main.go's switch, and inherits the obligation to re-run
// the audits that were done against fakes.
//
// WHAT THIS FLOW DELIBERATELY DOES NOT DO:
//
//   - It does not tell anyone whether an address is registered. See Request.
//   - It does not sign anybody in. A reset token permits exactly one state
//     transition and grants no session (ADR 0015); the successful path ends on the
//     sign-in form, which is also what Consume's own revocation makes necessary —
//     it signs the resetter out everywhere, including the browser they are using.
//   - It does not show the link to whoever asked for it. That is not a UX choice: a
//     visible reset link is an account takeover with a button on it, and it is the
//     one thing ADR 0015 says stands between minting and takeover.
type AdminReset struct {
	resets panelResets
	// mail is the delivery channel, and it MAY BE NIL.
	//
	// 🔴 NIL IS A REAL, NAMED STATE AND NOT AN OVERSIGHT — this is the one dependency
	// in internal/handler whose constructor does not refuse it, so it is argued
	// rather than assumed. Nil means "this deployment has no way to send the link",
	// which is TODAY'S SHIPPED TRUTH (config.ResetDelivery, Q02).
	//
	// WHY IT IS A NIL FIELD RATHER THAN A SECOND BOOLEAN OR A SECOND METHOD ON THE
	// INTERFACE: the fact "can this deployment deliver?" has to be readable BEFORE
	// any address is resolved — see Request — and a one-method interface (§7, the
	// shape internal/invite.Channel sets) cannot answer it. A parallel bool would be
	// a second representation of the same fact, which is the drift this repository
	// has paid for repeatedly.
	mail  ResetChannel
	audit auditRecorder
	// cookies writes the two short-lived recovery cookies (the synchronizer token,
	// and the one that carries the link's token out of the URL).
	cookies adminCookies
	// baseURL is this deployment's own origin, for the Origin header check.
	baseURL string
	// linkBase is the absolute address a reset link points at:
	// cfg.BaseURL + adminResetNewPath. adminauth.IssuedReset.Link appends "?t=<token>"
	// to it, so this must be the FULL path and not just the origin.
	linkBase string

	// See adminresetlimits.go for what each budget bounds and which of them may
	// refuse a request.
	requestLimiter *limiter
	submitLimiter  *limiter
	accountLimiter *limiter
	// linkLimiter bounds refusal rows for ONE recovery link. It is a SEPARATE counter
	// from accountLimiter and the separation is a security fix, not tidiness — see
	// recordForLink and adminResetLinkLimit.
	linkLimiter    *limiter
	unknownLimiter *limiter

	// sleep is the constant-time floor's clock, injectable so a test can assert the
	// floor is APPLIED without paying it on every case. Production leaves it nil.
	sleep func(time.Duration)

	log *slog.Logger
}

// panelResets is the slice of adminauth.Resets this package needs, declared HERE at
// the consumer (§7).
type panelResets interface {
	IssueForEmail(ctx context.Context, email string) ([]adminauth.ResetGrant, error)
	Consume(ctx context.Context, t adminauth.ResetToken, newPassword string) (adminauth.ConsumedReset, db.ResolvedPasswordReset, error)
}

// ResetDelivery is what a ResetChannel receives: one reset link and the address it
// must reach.
//
// ⚠️ Link CONTAINS THE RAW TOKEN. It is the single egress of a §4.7 secret from this
// flow, and internal/adminauth.IssuedReset.Link states the three obligations every
// implementation inherits and no type here can enforce — never log it, never persist
// it, hand it to ONE recipient. They are not repeated; they are one file away and
// this comment exists so a future implementer goes and reads them.
//
// Recipient IS READ FROM THE ADMINISTRATOR'S OWN ROW (adminauth.ResetGrant), never
// from the submitted form, which is the acceptance criterion "link yalnızca
// yöneticinin KENDİ satırındaki adrese gider" made structural.
type ResetDelivery struct {
	Recipient string
	Link      string
	ExpiresAt time.Time
}

// ResetChannel delivers a reset link to the administrator it belongs to.
//
// ONE METHOD, DECLARED AT THE CONSUMER (§7), for the reason internal/invite.Channel
// gives: the producer of a delivery mechanism does not get to define what this flow
// needs from it. And, unlike invites, there is no interim implementation at all —
// see config.ResetDelivery for why showing the link to the requester is not one.
type ResetChannel interface {
	DeliverReset(ctx context.Context, d ResetDelivery) error
}

// Audit actions written by the recovery flow. The vocabulary is free text by schema
// decision (00005), so these constants are the vocabulary.
const (
	ActionAdminResetRequested = "admin.recovery.requested"
	ActionAdminResetCompleted = "admin.recovery.completed"
	ActionAdminResetRefused   = "admin.recovery.refused"
	ActionAdminResetLimited   = "admin.recovery.rate_limited"
	// ActionAdminResetUndelivered marks a link that was minted and could NOT be
	// handed to its recipient. The row exists because the alternative is silence:
	// the requester must not be told (it would answer "is this address registered"),
	// so the trail is the only place the fact can live (§4.6).
	ActionAdminResetUndelivered = "admin.recovery.undelivered"
)

// adminResetPath is the recovery request form, and adminResetNewPath is what a reset
// link opens.
//
// THEY ARE CONSTANTS FOR adminLoginPath's REASON: a second surface links to the
// first (the sign-in form's "I cannot get in" link, via pages.AdminLoginView), and a
// link pointing at a route somebody renamed is a dead button on the page an operator
// reaches when they are already stuck. The second is worse than a dead button: it is
// the base of every reset URL this deployment has ever emailed, so renaming it
// invalidates links that are already in inboxes — which is why the two live beside
// each other where that is visible.
const (
	adminResetPath    = "/admin/reset"
	adminResetNewPath = "/admin/reset/new"
)

const (
	// adminResetCookieName carries the synchronizer token for the REQUEST form.
	adminResetCookieName = "tappa_admin_reset"

	// adminResetLinkCookieName carries "<csrf>.<token>" for the SET-A-NEW-ONE form.
	//
	// 🔴 THE TOKEN TRAVELS IN AN HttpOnly COOKIE AND NOT IN A HIDDEN FIELD, which is
	// internal/handler/cookies.go's decision for the activation code, taken again for
	// the same three gains and one more that is specific here:
	//
	//  1. The response BODY carries no token, which is testable and is tested.
	//  2. The ADDRESS BAR carries none after the first hop: GET /admin/reset/new?t=…
	//     sets the cookie and answers 303 to a bare /admin/reset/new, so the URL in
	//     history, in the tab title and in any Referer is clean.
	//  3. HttpOnly removes page script from the picture.
	//  4. AND THE FOURTH IS WHY internal/adminauth/reset.go ASKS FOR Referrer-Policy
	//     HERE: a reset link is opened from a mail client, so the first request is a
	//     cross-site top-level navigation and the browser may keep the full URL. The
	//     redirect is what takes it out of the browser's own record; the header is
	//     what stops it leaving ours. Both are cheap, neither is sufficient alone.
	//
	// ⚠️ KNOWN LIMIT, INHERITED FROM THE SAME PLACE AND MEASURED THERE: SameSite
	// restricts SENDING, not WRITING, so a cross-site GET navigation can PLANT this
	// cookie. What that buys an attacker here is smaller than in the activation flow
	// and is stated rather than waved away — the victim would set a new password on
	// the ATTACKER'S account, which tells the attacker nothing and costs them their
	// own token; the sharper version is denial, because planting overwrites a reset
	// link the victim was in the middle of using. Both need the victim to follow a
	// link, and both leave the victim's own recovery available by asking again.
	adminResetLinkCookieName = "tappa_admin_reset_link"

	// adminResetLinkCookiePath is the NARROWEST path that still reaches every read
	// and every clear of the cookie above. It is deliberately not adminauth.CookiePath
	// — the reasoning, the measurement and the deletion trap are at
	// adminCookies.setResetLink.
	adminResetLinkCookiePath = adminResetNewPath

	// adminResetCookieMaxAge is 15 minutes: enough to read a form and type an
	// address, short enough that a synchronizer token left on a shared machine goes
	// stale on its own. It is adminLoginCookieMaxAge's number and its reasoning.
	adminResetCookieMaxAge = 15 * 60

	// adminResetLinkCookieMaxAge is DERIVED FROM adminauth.ResetTTL rather than
	// written again: a browser must not hold a value that looks usable after the
	// server would refuse it, and two independent numbers held together by prose is
	// the shape adminChoiceCookieMaxAge records having been caught in.
	adminResetLinkCookieMaxAge = int(adminauth.ResetTTL / time.Second)

	// adminResetMaxFormBytes bounds a recovery submission. ParseForm reads the whole
	// body into memory and both of these POSTs are unauthenticated, so without a
	// limit one request decides how much this process allocates. 8 KB is far above
	// the largest legitimate submission (an address, or two password boxes and a
	// synchronizer token).
	adminResetMaxFormBytes = 8 << 10
)

// NewAdminReset wires the recovery flow.
//
// EVERY DEPENDENCY IS REQUIRED EXCEPT THE CHANNEL. A nil recorder would silently drop
// the §4.6 trail this whole flow is judged on, and a nil Resets would put a form on
// screen whose submit panics — the M5-04 shape (a capability delivered, tested and
// DEAD in the wired product). The channel is the one thing this deployment genuinely
// does not have; see the field.
func NewAdminReset(resets panelResets, mail ResetChannel, rec auditRecorder, cfg *config.Config, log *slog.Logger) (*AdminReset, error) {
	switch {
	case resets == nil:
		return nil, errors.New("handler: nil password resets")
	case rec == nil:
		return nil, errors.New("handler: nil audit recorder")
	case cfg == nil:
		return nil, errors.New("handler: nil config")
	}
	if log == nil {
		log = slog.Default()
	}
	return &AdminReset{
		resets:         resets,
		mail:           mail,
		audit:          rec,
		cookies:        newAdminCookies(cfg),
		baseURL:        originOf(cfg.BaseURL),
		linkBase:       strings.TrimRight(cfg.BaseURL, "/") + adminResetNewPath,
		requestLimiter: newLimiter(adminResetRequestLimit, adminResetRequestPeriod),
		submitLimiter:  newLimiter(adminResetSubmitLimit, adminResetSubmitPeriod),
		accountLimiter: newLimiter(adminResetAccountLimit, adminResetAccountPeriod),
		linkLimiter:    newLimiter(adminResetLinkLimit, adminResetLinkPeriod),
		unknownLimiter: newLimiter(adminResetUnknownLimit, adminResetUnknownPeriod),
		log:            log,
	}, nil
}

// Mount registers the routes.
//
// NONE OF THEM IS BEHIND AdminAuth.Protect, AND THAT IS THE POINT: a person who
// cannot sign in is exactly who these pages are for. What stands in for the gate is
// the pair of ceilings in adminresetlimits.go, the strict Origin check on both POSTs
// and the synchronizer token — the same three the sign-up wizard leans on, which is
// the other public form in this product.
func (h *AdminReset) Mount(r chi.Router) {
	r.Get(adminResetPath, h.RequestPage)
	r.Post(adminResetPath, h.Request)
	r.Get(adminResetNewPath, h.NewPage)
	r.Post(adminResetNewPath, h.Submit)
}

// The recovery screens' failure pages.
var (
	problemResetNoCookie = pages.ProblemView{
		Title:   "Your browser didn't keep this step",
		Message: "Tappa needs a cookie to carry this form from one page to the next, and this browser is not storing one.",
		Hint:    "Allow cookies for this site, or turn off private browsing, then start again.",
	}
	problemResetRestart = pages.ProblemView{
		Title:    "That step expired",
		Message:  "This form is only valid for a few minutes.",
		Hint:     "Start again from the sign-in page.",
		RetryURL: adminResetPath,
	}
	problemResetTooMany = pages.ProblemView{
		Title:   "Too many attempts",
		Message: "We have stopped accepting recovery attempts from here for a few minutes.",
		Hint:    "Wait a little and try again.",
	}
	problemResetServer = pages.ProblemView{
		Title:   "Something went wrong on our side",
		Message: "Nothing was changed and no password was set.",
		Hint:    "Try again in a minute.",
	}
	// 🔴 THE SCREEN FOR A DEPLOYMENT THAT CANNOT SEND A LINK AT ALL. It is a
	// SEPARATE view from problemResetLinkUnusable because that one names three
	// causes ("used, expired, or replaced by a newer one") and all three would be
	// FALSE here: no link was ever sent, so the one in the visitor's hand cannot
	// have been any of those things. A screen that guesses wrong about why is a
	// screen that sends somebody to ask for a replacement that will not arrive
	// either.
	problemResetNoDelivery = pages.ProblemView{
		Title:   "This Tappa cannot send recovery links",
		Message: "Sending is not switched on for this server, so no recovery link was ever issued and nothing here can set a password.",
		Hint:    "Ask another owner of your business to set a new password for you from the panel.",
	}
	// 🔴 ONE SCREEN FOR EVERY WAY A LINK CAN FAIL — unknown, malformed, already
	// used, expired, superseded by a newer one, or belonging to an account that has
	// since been switched off. internal/adminauth.ErrResetUnusable collapses all six
	// on purpose and says why in one line: this page is reachable without
	// credentials, so any distinction the caller can observe is a distinction an
	// attacker can observe. The trail keeps what this screen throws away.
	problemResetLinkUnusable = pages.ProblemView{
		Title:    "That link no longer works",
		Message:  "A recovery link can be used once, and only for an hour. This one has been used, has expired, or has been replaced by a newer one.",
		Hint:     "Ask for a new link and use the newest email you receive.",
		RetryURL: adminResetPath,
	}
)

// RequestPage serves GET /admin/reset.
func (h *AdminReset) RequestPage(w http.ResponseWriter, r *http.Request) {
	csrf, err := newCSRFToken()
	if err != nil {
		// "csrf" rather than the word redline R7 matches inside a log call: an
		// innocent MESSAGE reddening CI is how a net gets loosened under pressure
		// (activate.go and adminlogin.go state the same reasoning).
		h.log.Error("panel recovery: minting the csrf value failed", "err", err)
		h.problem(w, r, http.StatusInternalServerError, problemResetServer)
		return
	}
	h.cookies.set(w, adminResetCookieName, csrf, adminResetCookieMaxAge)
	h.render(w, r, http.StatusOK, pages.AdminResetRequest(pages.AdminResetRequestView{
		CSRFToken:  csrf,
		CanDeliver: h.canDeliver(),
		SignInHref: adminLoginPath,
	}))
}

// Request serves POST /admin/reset.
//
// 🔴 THE ANSWER IS THE SAME PAGE FOR EVERY ADDRESS — the fifth acceptance criterion,
// and the reason it is worth a paragraph is that "same page" has four parts and only
// three of them are free. The STATUS is one constant, the BODY is one component with
// no field that could carry a per-address fact, the WORDING says "if that address
// belongs to…" rather than asserting anything, and the TIMING is held to
// resetRequestFloor because the work behind the two answers genuinely differs.
//
// 🔴 AND THE UNDELIVERABLE CASE IS DECIDED BEFORE THE ADDRESS IS TOUCHED, which is
// the whole reason the channel is a nil-able field. The M7-04 card asks for two
// things that pull against each other: "gönderim başarısızlığı kullanıcıya dürüstçe
// bildiriliyor" and "kayıtlı ve kayıtsız adres için yanıt aynı". A per-recipient
// failure cannot satisfy both — an unregistered address has nothing to send, so it
// can never fail, so a "we could not send it" screen would answer the enumeration
// question exactly. The split that satisfies both:
//
//	DEPLOYMENT-LEVEL   "this deployment cannot send the link at all" is a fact about
//	                   the SERVER, identical for every address, knowable before any
//	                   resolution. It is told to the visitor, plainly, and nothing is
//	                   resolved, minted or retired — so an undeliverable deployment
//	                   also writes no rows and kills nobody's pending link.
//	PER-RECIPIENT      a send that fails for one address is a fact about THAT
//	                   address. It goes to audit_log (ActionAdminResetUndelivered)
//	                   and to the process log, never to the response.
//
// ORDER OF CHECKS, and why each is where it is:
//
//	Origin            strict. A cross-origin POST here is CSRF whose result is a
//	                  stranger extinguishing somebody's pending recovery link.
//	cookie + form + synchronizer token
//	request budget    BEFORE any database work, which is the only position a limiter
//	                  can shed load from.
//	delivery check    before resolution, so the undeliverable answer cannot depend on
//	                  the address.
//	resolve + issue + deliver
//	floor             applied on EVERY exit below the budget check, including the
//	                  ones that did nothing.
func (h *AdminReset) Request(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !h.beginPost(w, r, ip, "admin_reset_request") {
		return
	}
	if !h.requestLimiter.Allowed(ip) {
		// The refusal itself is charged (ratelimit.go's rule: a limiter whose own
		// refusals are free is a cheaper way to reach the thing it protects).
		if n := h.requestLimiter.Charge(ip); h.requestLimiter.FirstOverLimit(n) {
			h.log.Warn("panel recovery rate limited", "scope", "address", "ip", ip,
				"limit", adminResetRequestLimit, "period", adminResetRequestPeriod.String())
		}
		h.problem(w, r, http.StatusTooManyRequests, problemResetTooMany)
		return
	}
	h.requestLimiter.Charge(ip)

	// FROM HERE EVERY EXIT PAYS THE FLOOR. It is armed after the budget check on
	// purpose: a refused request must not be slower than a served one, and holding a
	// goroutine for a request we already decided not to do is the opposite of what a
	// limiter is for.
	done := h.floor()

	if !h.canDeliver() {
		// Address-independent, and NOTHING has been resolved: no resolver read, no
		// token, no retirement. The visitor is told the truth (§4.6, no silent
		// swallowing) and the sentence is the same one an unregistered address would
		// get here, because the branch never looked at the address.
		done()
		h.render(w, r, http.StatusOK, pages.AdminResetSent(pages.AdminResetSentView{
			CanDeliver: false,
			SignInHref: adminLoginPath,
		}))
		return
	}

	// The address is normalised only by trimming surrounding whitespace. CASE IS NOT
	// touched: admin_users.email is citext and 00011's resolver compares with
	// OPERATOR(public.=), so the DATABASE is the single authority on what "the same
	// address" means — adminlogin.go's rule, and a second canonicalisation here would
	// be the M5-01/02/03 failure class.
	email := strings.TrimSpace(r.PostFormValue("email"))

	grants, err := h.resets.IssueForEmail(r.Context(), email)
	if err != nil {
		// A database failure is NOT "unknown address", and it must not be reported as
		// one: that would hide an outage behind a screen saying a link is on its way.
		// The address is NOT in the line (§4.7 spirit: an address is personal data and
		// this path is unauthenticated).
		h.log.Error("panel recovery: issuing failed", "ip", ip, "err", err)
		done()
		h.problem(w, r, http.StatusInternalServerError, problemResetServer)
		return
	}
	for _, g := range grants {
		h.deliver(r.Context(), ip, g)
	}
	if len(grants) == 0 {
		// Unattributable: nothing resolved, so there is no tenant and audit_log's
		// tenant_id is NOT NULL with an FK (00005). Logged WITHOUT the address, and
		// bounded, so a flood of guesses cannot write a list of guessed addresses into
		// the process log or fill a disk.
		if h.unknownLimiter.Allowed(ip) {
			h.unknownLimiter.Charge(ip)
			h.log.Info("panel recovery refused", "reason", "no active admin for that address", "ip", ip)
		}
	}
	done()
	h.render(w, r, http.StatusOK, pages.AdminResetSent(pages.AdminResetSentView{
		CanDeliver: true,
		SignInHref: adminLoginPath,
	}))
}

// deliver hands ONE link to the channel and records what happened.
//
// ORDER: deliver FIRST, then record. It is the OPPOSITE of
// internal/invite.ManagerVisibleChannel's order, and the difference is what the row
// means. There, the row IS the disclosure ("a manager read this code"), so it must
// exist even if the screen fails. Here the row says what happened to a link, and
// there is exactly one outcome per attempt: writing "requested" before the send would
// mean a failed send needs a SECOND row correcting the first, in a table nothing can
// delete from.
func (h *AdminReset) deliver(ctx context.Context, ip string, g adminauth.ResetGrant) {
	action := ActionAdminResetRequested
	outcome := "ok"
	reason := ""
	if err := h.mail.DeliverReset(ctx, ResetDelivery{
		Recipient: g.Recipient,
		// Link IS the §4.7 secret. It is built here and passed straight on; it is
		// never assigned to a named variable that a later edit might log, and it is
		// never returned to the caller of this request.
		Link:      g.Issued.Link(h.linkBase),
		ExpiresAt: g.Issued.Reset.ExpiresAt,
	}); err != nil {
		action = ActionAdminResetUndelivered
		outcome = "undelivered"
		reason = "the recovery link could not be handed to the delivery channel"
		// 🔴 THE CHANNEL'S ERROR TEXT IS NOT LOGGED, AND THE FIRST VERSION OF THIS LINE
		// PASSED IT AS "err". The comment above it promised "NOT the address and NOT
		// the link" and the promise did not survive that argument: a security audit
		// drove a channel returning an ordinary SMTP failure and lifted BOTH secrets
		// out of the real log line —
		//
		//	msg="panel recovery: delivery failed" … err="smtp: 550 5.1.1
		//	<leak-probe@probe.example> user unknown while sending
		//	https://tappa.test/admin/reset/new?t=qCq0TQ…YyQvCJ8"
		//
		// THREE OPTIONS WERE WEIGHED AND THE DENYLIST WAS ELIMINATED BY MEASUREMENT.
		// Scrubbing the two known secrets out of the text at this boundary handles the
		// easy case and fails on things ORDINARY mail clients do — measured over six
		// shapes, none adversarial:
		//
		//	verbatim url + address        scrub is clean
		//	percent-encoded url           scrub is clean
		//	json-escaped url              scrub is clean
		//	base64 body echoed back       TOKEN SURVIVES
		//	address upper-cased           ADDRESS SURVIVES
		//	soft-wrapped mid-token        TOKEN SURVIVES
		//
		// A denylist that two of six ordinary shapes walk past is not a §4.7 guarantee,
		// and this file has already had to withdraw one promise it could not keep.
		//
		// WHAT IS LOGGED INSTEAD is a CLASSIFICATION: the failure happened, to which
		// administrator, from which address, and the concrete Go type of the error. A
		// type name cannot contain a token, so the leak is structurally impossible
		// rather than filtered.
		//
		// ⚠️ THE RESIDUAL, NAMED: the provider's own message — the thing an operator
		// wants at the exact moment a customer says "the link never arrived" — is NOT
		// in our log. That is the price, and it is paid to the right party: the CHANNEL
		// is the only code that knows which parts of its own text are safe, and it
		// already inherits the three obligations at adminauth.IssuedReset.Link. Whoever
		// implements one for Q02 logs its own diagnostics under those obligations. The
		// occurrence is never swallowed (§7): it is here, and it is an audit row.
		h.log.Error("panel recovery: delivery failed", "ip", ip,
			"admin_user_id", g.Issued.Reset.AdminUserID, "err_type", fmt.Sprintf("%T", err))
	}
	h.recordForAdmin(ctx, g.Issued.Reset.TenantID, g.Issued.Reset.AdminUserID, audit.Event{
		TenantID: g.Issued.Reset.TenantID,
		ActorID:  ptr(g.Issued.Reset.AdminUserID),
		Action:   action,
		Target:   g.Issued.Reset.AdminUserID.String(),
		Detail: adminResetDetail{
			Outcome: outcome,
			Reason:  reason,
			ResetID: g.Issued.Reset.ID.String(),
			// 🔴 THE RETIREMENT COUNT IS IN THE ROW BECAUSE IT IS THE ONLY PLACE THE
			// ACCEPTED RISK BECOMES VISIBLE. adminauth.Reset.RetiredCount is "how many
			// previously live links this request killed", and ADR 0015's harm (a) is
			// precisely somebody else's request killing yours. A takeover investigation
			// reading "requested, retired 1" one minute after "requested, retired 0"
			// is reading the attack; without the number it reads two ordinary requests.
			RetiredCount: g.Issued.Reset.RetiredCount,
			ExpiresAt:    g.Issued.Reset.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
}

// NewPage serves GET /admin/reset/new — what a reset link opens.
//
// IT MAKES NO DATABASE QUERY AND THAT IS DELIBERATE. Checking here whether the token
// is live would answer "does this token exist" on a page reachable by anybody, in
// microseconds, for free — the oracle internal/adminauth/reset.go refuses to open by
// keeping the digest computation ahead of the lookup. So the form is rendered for any
// well-shaped value and the single authority stays the atomic statement behind POST.
func (h *AdminReset) NewPage(w http.ResponseWriter, r *http.Request) {
	// no-referrer on the ONE surface in this product whose URL can carry a §4.7
	// secret. layout.documentHead already emits the meta equivalent for navigations;
	// the header is what a fetch, a prefetch and a non-HTML response honour.
	w.Header().Set("Referrer-Policy", "no-referrer")

	if !h.canDeliver() {
		// Nothing was ever sent, so no link that reaches here can be one of ours, and
		// a form that cannot succeed is not offered. It also stops this page planting
		// a cookie carrying an attacker-supplied value on a flow that is inert.
		h.logUndeliverableAttempt(r, "admin_reset_open", r.URL.Query().Get("t") != "" || h.hasLinkCookie(r))
		h.problem(w, r, http.StatusNotFound, problemResetNoDelivery)
		return
	}

	if t := r.URL.Query().Get("t"); t != "" {
		csrf, err := newCSRFToken()
		if err != nil {
			h.log.Error("panel recovery: minting the csrf value failed", "err", err)
			h.problem(w, r, http.StatusInternalServerError, problemResetServer)
			return
		}
		// 🔴 THREE REFUSALS, AND NONE OF THEM IS THE SHAPE GATE. Deciding what a
		// well-formed token looks like happens in exactly one place — adminauth's hash
		// — and every value accepted here is still refused there if it cannot be one.
		// What these three decide is whether the value may be put in a COOKIE HEADER
		// at all, which is a transport question this layer owns:
		//
		//  1. '.' is this cookie's own field separator and cannot occur in base64url,
		//     so a value containing one was never a token we issued.
		//  2. The length bound keeps a hostile URL from deciding how big a cookie we
		//     ask a browser to store.
		//  3. 🔴 A BYTE net/http WOULD NOT PUT IN A COOKIE. This one is a real defect
		//     rather than hygiene, and it was MEASURED: http.SetCookie hands the value
		//     to sanitizeOrWarn, which (a) writes a line to the STANDARD LOGGER —
		//     bypassing our slog handler entirely, so TAPPA_LOG_LEVEL cannot quiet it
		//     and no budget bounds it — and (b) SILENTLY STRIPS the offending bytes
		//     and stores what is left. Measured on go1.26's own source and by driving
		//     it: ONE line per call (the loop breaks at the first bad byte, so it is
		//     per REQUEST and not per byte, which is what an audit reported), and
		//     "aaa;bbb;ccc" is stored as "aaabbbccc". Both halves are wrong here: an
		//     unauthenticated caller must not be able to write unbounded lines to
		//     stderr, and a credential-shaped value must be REFUSED rather than
		//     quietly rewritten into a different one.
		if strings.ContainsAny(t, ".") || len(t) > 200 || !cookieSafe(t) {
			h.problem(w, r, http.StatusBadRequest, problemResetLinkUnusable)
			return
		}
		h.cookies.setResetLink(w, csrf+"."+t)
		// 303 to the CLEAN url: the token leaves the address bar, history and the tab
		// title after exactly one hop.
		h.redirect(w, adminResetNewPath)
		return
	}

	csrf, _, ok := h.readLink(r)
	if !ok {
		h.problem(w, r, http.StatusBadRequest, problemResetNoCookie)
		return
	}
	h.render(w, r, http.StatusOK, pages.AdminResetNew(pages.AdminResetNewView{
		CSRFToken:        csrf,
		MinPasswordChars: signup.MinPasswordRunes,
	}))
}

// Submit serves POST /admin/reset/new: spend the link, write the new digest, sign
// every session out, send the person to the sign-in form.
//
// 🔴 THE FORM IS VALIDATED BEFORE Consume IS CALLED, AND THAT IS A CPU DECISION AS
// WELL AS A UX ONE. Consume computes a full cost-12 digest BEFORE it looks the token
// up — deliberately, because looking up first would separate a real token from an
// invented one by ~213 ms on a page anybody can reach — so every call costs the same
// whatever it finds. Checking "both boxes filled, both equal, length inside the
// bounds" here means a mistyped confirmation costs no bcrypt at all.
func (h *AdminReset) Submit(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	// 🔴 FAIL CLOSED BEFORE ANYTHING ELSE WHEN THIS DEPLOYMENT CANNOT DELIVER, and it
	// is a CPU decision rather than a tidiness one. Request already short-circuits on
	// the same fact, so no link can exist — yet this endpoint went on paying a full
	// cost-12 digest (~213 ms, measured in internal/adminauth/reset.go) for any
	// well-shaped 43-character string, with no credential required, for a flow that
	// could not succeed for anybody. That is an unauthenticated CPU surface bought for
	// nothing, on the pool the tap surface shares (§4.6, adminLoginWorkLimit's
	// argument from its side).
	//
	// IT REVEALS NOTHING AN ANONYMOUS CALLER CANNOT ALREADY SEE: GET /admin/reset says
	// the same thing on its face, before an address is typed. The branch is decided by
	// this deployment's configuration and never by the submitted value.
	if !h.canDeliver() {
		h.logUndeliverableAttempt(r, "admin_reset_submit", h.hasLinkCookie(r))
		h.problem(w, r, http.StatusNotFound, problemResetNoDelivery)
		return
	}
	if !h.sameOrigin(r) {
		h.log.Warn("panel recovery refused: not same-origin", "at", "admin_reset_submit", "ip", ip)
		h.problem(w, r, http.StatusBadRequest, problemResetRestart)
		return
	}
	csrf, raw, ok := h.readLink(r)
	if !ok {
		h.problem(w, r, http.StatusBadRequest, problemResetNoCookie)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, adminResetMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.problem(w, r, http.StatusBadRequest, problemResetServer)
		return
	}
	if !constantTimeMatch(csrf, r.PostFormValue("csrf")) {
		h.log.Warn("panel recovery refused: csrf mismatch", "at", "admin_reset_submit", "ip", ip)
		h.problem(w, r, http.StatusBadRequest, problemResetRestart)
		return
	}

	chosen := r.PostFormValue("password")
	if msg := checkNewPassword(chosen, r.PostFormValue("password_confirm")); msg != "" {
		// Re-render with the message and WITHOUT either box refilled. Nothing is
		// charged: no digest was computed and no lookup was made.
		h.render(w, r, http.StatusUnprocessableEntity, pages.AdminResetNew(pages.AdminResetNewView{
			CSRFToken:        csrf,
			MinPasswordChars: signup.MinPasswordRunes,
			Error:            msg,
		}))
		return
	}

	if !h.submitLimiter.Allowed(ip) {
		if n := h.submitLimiter.Charge(ip); h.submitLimiter.FirstOverLimit(n) {
			h.log.Warn("panel recovery rate limited", "scope", "submit", "ip", ip,
				"limit", adminResetSubmitLimit, "period", adminResetSubmitPeriod.String())
		}
		h.problem(w, r, http.StatusTooManyRequests, problemResetTooMany)
		return
	}
	h.submitLimiter.Charge(ip)

	consumed, resolved, err := h.resets.Consume(r.Context(), adminauth.ParseResetToken(raw), chosen)
	switch {
	case err == nil:
		h.clearLink(w)
		// 🔴 UNBUDGETED, AND THE DIRECT CALL IS THE FIX FOR A MEASURED §4.6 DEFECT.
		// This row used to go through recordForAdmin, i.e. through the per-account
		// audit budget — so eleven anonymous POSTs to the PUBLIC request form burnt
		// that budget and the next completed reset wrote NOTHING: a password changed,
		// every session revoked, and no trail. A security audit exploited exactly that
		// on the real router (the counts are in adminresetlimits.go).
		//
		// WHY IT IS SAFE TO LEAVE UNBOUNDED, which is the question the budget was
		// answering for the other rows: spending a link kills it, so there is AT MOST
		// ONE of these per link. The volume is therefore bounded by counters that are
		// already charged, not by trust — and THE BINDING ONE IS ON THIS ENDPOINT, not
		// the previous one:
		//
		//	adminResetSubmitLimit    10 per window per address, and every row here
		//	                         costs one unit, because reaching this branch means
		//	                         a Consume call ran.   <- THE TIGHT BOUND
		//	adminResetRequestLimit   20 per window per address, bounding how many links
		//	                         can be minted in the first place.
		//
		// ⚠️ THIS PARAGRAPH NAMED ONLY THE SECOND, AND CALLED IT "one endpoint
		// earlier". Not wrong — a row cannot exist without a link — but it is the
		// LOOSER of the two, and the whole case for leaving this row uncounted rests
		// on the tighter one. An audit named the substitution; the numbers are here so
		// nobody has to take either on trust.
		//
		// AND THE PRECEDENT IS TWENTY LINES AWAY IN THIS PACKAGE, pointing the same
		// way: adminlogin.go writes ActionAdminLoginSucceeded with a direct a.record
		// and applies its account budget only inside failLogin's FAILURE loop. "A
		// successful state transition can never be silenced" was already this
		// repository's rule.
		h.record(r.Context(), audit.Event{
			TenantID: consumed.TenantID,
			ActorID:  ptr(consumed.AdminUserID),
			Action:   ActionAdminResetCompleted,
			Target:   consumed.AdminUserID.String(),
			Detail: adminResetDetail{
				Outcome:         "ok",
				ResetID:         consumed.ResetID.String(),
				RetiredCount:    consumed.RetiredCount,
				RevokedSessions: consumed.RevokedSessions,
			},
		})
		h.log.Info("panel recovery completed", "admin_user_id", consumed.AdminUserID,
			"ip", ip, "revoked_sessions", consumed.RevokedSessions)
		// 🔴 TO THE SIGN-IN FORM, NEVER INTO THE PANEL. adminauth.Consume revokes EVERY
		// live session for this administrator inside the same transaction as the write
		// — including the browser doing the reset, because nothing here can tell "the
		// browser holding this cookie" from "the browser that stole it". Sending them
		// anywhere else would be sending them to a page that is about to answer 303.
		h.redirect(w, adminLoginPath+"?"+adminResetDoneQuery)
	case errors.Is(err, adminauth.ErrResetUnusable):
		h.refused(w, r, ip, resolved)
	default:
		h.log.Error("panel recovery: spending the link failed", "ip", ip, "err", err)
		h.problem(w, r, http.StatusInternalServerError, problemResetServer)
	}
}

// refused is the SINGLE exit for every unusable link, and the one place §4.6's
// coverage of this flow lives.
//
// internal/adminauth/reset.go states the division of labour and this function is its
// other half:
//
//	THE TOKEN RESOLVED       Consume returns the resolved row ALONGSIDE the error, so
//	                         there is a tenant, an admin id and the three state facts.
//	                         That is an audit_log row, attributable, in the tenant
//	                         whose administrator the link belonged to.
//	IT DID NOT RESOLVE       there is no tenant, and audit_log.tenant_id is NOT NULL
//	                         with an FK (00005), so a row is structurally impossible.
//	                         A bounded process log line instead, carrying neither the
//	                         value nor its hash.
//
// THE VISITOR GETS THE SAME SCREEN EITHER WAY.
func (h *AdminReset) refused(w http.ResponseWriter, r *http.Request, ip string, resolved db.ResolvedPasswordReset) {
	if resolved.TenantID == uuid.Nil {
		if h.unknownLimiter.Allowed(ip) {
			h.unknownLimiter.Charge(ip)
			// Neither the submitted value nor its hash (§4.7). "link" rather than the
			// word redline R7 matches inside a log call — activate.go's precedent.
			h.log.Info("panel recovery refused", "reason", "link did not resolve", "ip", ip)
		}
		h.clearLink(w)
		h.problem(w, r, http.StatusBadRequest, problemResetLinkUnusable)
		return
	}
	// The three state facts, carried by the resolver as FACTS and not applied by it
	// (internal/db's ResolvedPasswordReset). They are what tells a replay from a
	// typo, which is exactly the difference §4.6 wants visible.
	//
	// 🔴 BUDGETED ON THE LINK, NEVER ON THE ACCOUNT. Minting one of these needs a
	// value whose HMAC matches a stored row, which an anonymous caller cannot produce
	// — but REPLAYING a dead link is free for whoever holds it, so the volume is
	// attacker-controllable and the row has to be bounded. Putting it on the account
	// budget (where it started) would have let a stranger flooding the PUBLIC request
	// form silence this exact signal — "somebody is replaying a link against this
	// account", i.e. the takeover signal — for an account they hold no link for.
	// Keyed on the reset row's id, the only person who can burn it is the one holding
	// that link, and burning it silences nothing else.
	h.recordForLink(r.Context(), resolved, audit.Event{
		TenantID: resolved.TenantID,
		ActorID:  ptr(resolved.AdminUserID),
		Action:   ActionAdminResetRefused,
		Target:   resolved.AdminUserID.String(),
		Detail: adminResetDetail{
			Outcome:     "refused",
			Reason:      "the recovery link resolved but could not be spent",
			ResetID:     resolved.ID.String(),
			AlreadyUsed: resolved.UsedAt != nil,
			Superseded:  resolved.CancelledAt != nil,
			Expired:     !resolved.ExpiresAt.IsZero() && time.Now().After(resolved.ExpiresAt),
			ExpiresAt:   resolved.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
	h.log.Warn("panel recovery refused", "reason", "link resolved but could not be spent",
		"ip", ip, "admin_user_id", resolved.AdminUserID)
	h.clearLink(w)
	h.problem(w, r, http.StatusBadRequest, problemResetLinkUnusable)
}

// adminResetDoneQuery is what the sign-in form is told after a completed recovery.
//
// IT CARRIES NO SECRET AND DECIDES NOTHING. Anybody can type it; all it does is put
// one sentence on a page. It exists because the alternative — landing on a bare
// sign-in form — leaves somebody who just changed their password unsure whether it
// worked, on the screen where guessing wrong costs them an attempt budget.
const adminResetDoneQuery = "recovered=1"

// adminResetDetail is the audit payload for this flow.
//
// IT IS A PURPOSE-BUILT STRUCT, NOT A MAP (internal/audit's package doc explains
// why): there is no field here through which a token, a hash, a digest or an email
// address could travel, so no later edit adds one by accident and the §4.7 promise is
// a property of the TYPE rather than of the author's memory. Everything in it is an
// opaque id, a count, a timestamp or a fixed string written in this file.
type adminResetDetail struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
	// ResetID is a row id, never a token and never its hash — neither has a field
	// here. internal/db's ResolvedPasswordReset says the same about the same value:
	// it is for logging and joins, never for consuming.
	ResetID string `json:"reset_id,omitempty"`
	// RetiredCount is how many other live links this act killed. See deliver.
	RetiredCount int64 `json:"retired_count,omitempty"`
	// RevokedSessions is how many live panel sessions the completed reset signed
	// out, in the same transaction as the write.
	RevokedSessions int `json:"revoked_sessions,omitempty"`
	// The three state facts a refused link resolved to. They are the whole reason
	// the resolver carries used_at, cancelled_at and expires_at instead of filtering
	// on them.
	AlreadyUsed bool   `json:"already_used,omitempty"`
	Superseded  bool   `json:"superseded,omitempty"`
	Expired     bool   `json:"expired,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	// SuppressedFrom is the attempt ordinal at which an audit budget tripped, i.e.
	// the first event that was NOT written. Present only on
	// ActionAdminResetLimited, and it is a START rather than a total — see the limit
	// documented at adminResetAccountLimit.
	SuppressedFrom int `json:"suppressed_from,omitempty"`
	// Scope names WHICH counter tripped, because there are two and they silence
	// different things: "account" suppresses request-side rows for one administrator,
	// "link" suppresses refusals for one recovery link. An investigator reading a
	// rate_limited row needs to know which signal went quiet; without this the two are
	// indistinguishable and the row says less than it looks like it says.
	Scope string `json:"scope,omitempty"`
}

// recordForAdmin writes one REQUEST-SIDE attributable row (`requested`,
// `undelivered`), bounded by the per-account audit budget.
//
// 🔴 IT IS NOT THE WRITER FOR EVERY ROW IN THIS FLOW ANY MORE, AND THAT NARROWING IS
// THE FIX FOR A MEASURED §4.6 DEFECT. It used to carry `completed` and `refused` too,
// which meant eleven anonymous POSTs to the public request form could silence a
// successful password change and a link-replay signal for an account the caller held
// nothing for. The dividing test and the exploit's own output are in
// adminresetlimits.go at adminResetAccountLimit.
//
// PAST THE BUDGET it writes ONE "rate_limited" row and then nothing, which is
// adminAccountLimit's design and is needed here for a sharper version of the same
// reason: this surface needs no credential at all, so an unbounded row per request
// would be a write primitive into a named tenant's append-only table.
func (h *AdminReset) recordForAdmin(ctx context.Context, tenantID, adminUserID uuid.UUID, e audit.Event) {
	key := adminUserID.String()
	if !h.accountLimiter.Allowed(key) {
		if n := h.accountLimiter.Charge(key); h.accountLimiter.FirstOverLimit(n) {
			h.record(ctx, audit.Event{
				TenantID: tenantID,
				ActorID:  ptr(adminUserID),
				Action:   ActionAdminResetLimited,
				Target:   key,
				Detail: adminResetDetail{
					Outcome:        "rate_limited",
					Reason:         "too many recovery requests against this account",
					Scope:          "account",
					SuppressedFrom: n,
				},
			})
			h.log.Warn("panel recovery rate limited", "scope", "account",
				"admin_user_id", adminUserID)
		}
		return
	}
	h.accountLimiter.Charge(key)
	h.record(ctx, e)
}

// recordForLink writes the refusal row for ONE recovery link, bounded by a budget
// keyed on that link's own row id.
//
// 🔴 THE KEY IS THE RESET ROW'S UUID, NEVER THE TOKEN AND NEVER ITS HASH.
// internal/handler/ratelimit.go states the rule for the invitation budget and it
// holds verbatim here: an in-memory map key ends up in heap dumps, and a token hash
// is a bearer credential in this design (hash -> resolver -> tenant -> consumption,
// db/queries/passwordresets.sql). The row id is not a credential — internal/db's
// ResolvedPasswordReset says so from its side: "ID IS FOR LOGGING AND JOINS, NOT FOR
// CONSUMING".
//
// WHY A SECOND COUNTER RATHER THAN THE ACCOUNT'S: see the block at the call site and
// the dividing test at adminResetAccountLimit. In one line — the account counter is
// spendable by any anonymous caller who knows an address, and this signal must not be
// silenceable by somebody who holds no link.
//
// IT GATES NOTHING. Past the ceiling the visitor gets the same refusal screen; only
// the writing stops.
func (h *AdminReset) recordForLink(ctx context.Context, resolved db.ResolvedPasswordReset, e audit.Event) {
	key := resolved.ID.String()
	if !h.linkLimiter.Allowed(key) {
		if n := h.linkLimiter.Charge(key); h.linkLimiter.FirstOverLimit(n) {
			h.record(ctx, audit.Event{
				TenantID: resolved.TenantID,
				ActorID:  ptr(resolved.AdminUserID),
				Action:   ActionAdminResetLimited,
				Target:   resolved.AdminUserID.String(),
				Detail: adminResetDetail{
					Outcome:        "rate_limited",
					Reason:         "too many refused attempts against one recovery link",
					Scope:          "link",
					ResetID:        key,
					SuppressedFrom: n,
				},
			})
			h.log.Warn("panel recovery rate limited", "scope", "link",
				"admin_user_id", resolved.AdminUserID, "reset_id", key)
		}
		return
	}
	h.linkLimiter.Charge(key)
	h.record(ctx, e)
}

// record writes an audit event and never lets its failure change the outcome of the
// request — but never swallows it either (§7): a trail that cannot be written is an
// ERROR, because it means §4.6 is not being kept.
//
// ⚠️ THE ROW IS NOT IN THE SAME TRANSACTION AS THE PASSWORD WRITE, AND THAT IS AN
// INHERITED BOUNDARY RATHER THAN A DEFECT OF THIS FLOW. It is written here with its
// verdict so the next reader does not re-open it:
//
//   - THE WINDOW IS REAL AND WAS MEASURED: when the audit write fails, the response is
//     still 303 and the new password is still committed, so a completed recovery can
//     exist with no `completed` row.
//   - IT IS NOT §4.3. That red line is the immutability of `transactions`, and this
//     flow touches that table nowhere. It is an edge of §4.6.
//   - IT IS THE PACKAGE-WIDE SHAPE, not this file's choice. All THREE audit writers in
//     internal/handler use Record (adminlogin.go:1499, activate.go:925 and this one),
//     and EVERY non-comment mention of RecordTx outside internal/audit is under
//     internal/domain/* — 27 of them (tenant 17, billing/legal/manual/review/signup 2
//     each), and ZERO anywhere else — because that is where the transaction is owned.
//     (An audit reported this as "25 call sites"; re-counted here rather than quoted,
//     because a number nobody re-derives is a number that drifts.) A handler has no tx
//     to join here:
//     adminauth.Resets.Consume opens and commits its own, and internal/adminauth's
//     reset.go REFUSES to take an audit.Recorder, with its reason written out ("a
//     data-layer function that logs on its own behalf writes a trail nobody can
//     correlate with a request").
//   - CLOSING IT MEANS CHANGING PHASE A's INTERFACE — Consume would have to accept a
//     recorder or expose its transaction — which is a larger change than this phase
//     owns and would undo an argument phase A made deliberately.
//   - THE TRIGGER IS A DATABASE FAILURE, not anything a caller controls: there is no
//     input to this flow that makes the audit INSERT fail while the password INSERT
//     succeeds, because the audit write happens after the other transaction has
//     already committed.
//
// Counted and handed on, in the shape the second stopping rule asks for: a measured
// limit is safer than a closure nobody verified.
func (h *AdminReset) record(ctx context.Context, e audit.Event) {
	if _, err := h.audit.Record(ctx, e); err != nil {
		h.log.Error("audit write failed", "action", e.Action, "err", err)
	}
}

// cookieSafe reports whether every byte of v may appear in a Set-Cookie value.
//
// IT IS net/http's OWN RULE, RESTATED RATHER THAN INVENTED (validCookieValueByte in
// net/http/cookie.go): printable US-ASCII, 0x20 to 0x7e INCLUSIVE, except double
// quote, semicolon and backslash.
//
// ⚠️ THIS SENTENCE USED TO SAY "except space" AND THE CODE HAS ALWAYS ACCEPTED ONE —
// an audit compared all 256 bytes against go1.26's validCookieValueByte and found
// ZERO divergence, then found the comment describing a 257th rule that is not there.
// Harmless (net/http quotes a value containing a space, and adminauth refuses
// anything that is not 43 bytes of base64url anyway) and corrected rather than left,
// because a comment that misdescribes the code is how the next person "fixes" the
// code to match it. Restating it is deliberate — the alternative is to let net/http apply it
// FOR us, which it does by logging and by silently deleting the offending bytes. See
// the three-refusal block in NewPage for the measurement.
//
// ⚠️ IT IS NOT A TOKEN CHECK AND MUST NOT GROW INTO ONE. adminauth decides what can
// be a token; this decides what can be a header value, and it is deliberately far
// wider than base64url so the two do not become two answers to one question.
func cookieSafe(v string) bool {
	for i := 0; i < len(v); i++ {
		b := v[i]
		if b < 0x20 || b >= 0x7f || b == '"' || b == ';' || b == '\\' {
			return false
		}
	}
	return true
}

// logUndeliverableAttempt is §4.6's coverage of the two routes that answer 404 on a
// deployment with no delivery channel.
//
// 🔴 IT EXISTS BECAUSE THE LEDGER SENTENCE WAS MEASURED FALSE. This type's comment
// counted criterion 2 as "the audit_log arm is dead, the process-log arm IS live" —
// and the short-circuit that made Submit fail closed had killed the process-log arm
// too, by returning before the only two call sites that write one. Measured on the
// real binary with TAPPA_LOG_LEVEL=debug: a GET carrying a 43-character value, a POST
// carrying one, and two POSTs to the request form produced FOUR responses and ZERO
// log lines. Declaring a guarantee the product does not provide is this repository's
// signature defect, and doing it inside the counted-gaps ledger — whose only job is
// to be accurate — empties the ledger of its value.
//
// THE FIX IS THE BEHAVIOUR, NOT THE SENTENCE. A 404 here does not mean "no such
// feature": the route is mounted, the value's shape is inspected, and anybody can
// call it without a credential. An attempt against that is exactly what §4.6 wants
// visible somewhere.
//
// 🔴 IT SHARES unknownLimiter WITH THE OTHER TWO PROCESS-LOG WRITERS, AND THE FIRST
// VERSION OF THIS PARAGRAPH GOT THE REASON WRONG. It said the THREE consumers are
// "mutually exclusive per process". Two auditors refuted it independently and they
// are right: the split is {1,2} vs {3}, not three ways. Request's no-candidate branch
// and refused's unresolved branch are BOTH live, simultaneously, whenever
// canDeliver() is true, and they have shared this bucket since before this consumer
// existed. What is genuinely exclusive is only the NEW one — it needs canDeliver()
// false, which is fixed for the lifetime of the process — so the half of the claim
// that mattered (this consumer cannot starve the other two) stands, and the half that
// was decorative was wrong.
//
// 🔴 SO WHAT ACTUALLY KEEPS THE TWO CO-LIVE CONSUMERS OUT OF EACH OTHER'S WAY IS
// ARITHMETIC, AND IT IS NOW PINNED BY A TEST rather than by this sentence. Per
// address per window, each consumer's demand is bounded by the gate in front of it:
//
//	Request's no-candidate branch    <= adminResetRequestLimit  (20)
//	refused's unresolved branch      <= adminResetSubmitLimit   (10)
//	                                    -----------------------------
//	worst-case demand                   30   against a budget of 60
//
// A security audit measured exactly that shape: 40 requests from one address gave 20
// served and 20 "no active admin for that address" lines; 20 submissions gave 10
// reaching refused and 10 "link did not resolve" lines. Thirty lines, budget sixty.
// The fixed-window boundary doubles both sides, so the inequality survives it.
//
// ⚠️ AND IT IS ONE CONSTANT CHANGE AWAY FROM BEING FALSE. The day
// adminResetRequestLimit + adminResetSubmitLimit exceeds adminResetUnknownLimit, one
// consumer can silence the other — a §4.6 gap opened by a rate limit, which is
// precisely the class of the blocking finding this flow already shipped once. An
// audit mutated the three consumers into simultaneous reachability and the WHOLE
// internal/handler package stayed green, so nothing was holding it.
// TestAdminResetBudgets_TheProcessLogBudgetCoversItsTwoCoLiveConsumers holds it now,
// and re-derives the arithmetic in its failure message.
//
// IT CARRIES NEITHER THE VALUE NOR ITS HASH (§4.7), and no address: this path is
// unauthenticated, so a flood would otherwise write a list of guesses into the log.
func (h *AdminReset) logUndeliverableAttempt(r *http.Request, where string, presented bool) {
	// 🔴 ONLY WHEN SOMETHING WAS ACTUALLY PRESENTED, AND THE FIRST VERSION HAD NO SUCH
	// TEST. Measured: `curl /admin/reset/new` with NO ?t= and no cookie answered 404
	// and wrote reason="a recovery link was presented and this deployment issues
	// none". Nothing had been presented — the line asserted an event that did not
	// happen. That is the same class of defect as the ledger entry this function
	// exists to repair, one layer down: a record is worth what its claim is worth, and
	// a trail that invents attempts is worse than one that misses them, because an
	// investigator cannot tell the invented ones from the real.
	//
	// A BROWSE WITH NO LINK IS NOT AN ATTEMPT AGAINST A CREDENTIAL, so it gets no
	// line and no budget unit. §4.6 asks for a failed ATTEMPT to land somewhere; there
	// is no attempt here to land.
	if !presented {
		return
	}
	ip := clientIP(r)
	if !h.unknownLimiter.Allowed(ip) {
		return
	}
	h.unknownLimiter.Charge(ip)
	h.log.Info("panel recovery refused",
		"reason", "a recovery link was presented and this deployment issues none",
		"at", where, "ip", ip)
}

// hasLinkCookie reports whether this request carries a recovery-link cookie at all.
// It deliberately does NOT read the value: the question is "was a link presented",
// and the value is a §4.7 secret this function has no reason to touch.
func (h *AdminReset) hasLinkCookie(r *http.Request) bool {
	ck, err := r.Cookie(adminResetLinkCookieName)
	return err == nil && ck.Value != ""
}

// canDeliver reports whether this deployment has any way to hand a recovery link to
// its recipient.
//
// IT IS THE ONE PLACE THE NIL CHANNEL IS INTERPRETED, so "no channel" means the same
// thing on all four routes rather than three times over. See the mail field for why
// the fact is a nil dependency and not a boolean beside it.
func (h *AdminReset) canDeliver() bool { return h.mail != nil }

// beginPost runs the checks the recovery request POST shares with every other
// state-changing form in this product: Origin, the synchronizer cookie, the bounded
// body, and the token. It returns false having ALREADY answered.
func (h *AdminReset) beginPost(w http.ResponseWriter, r *http.Request, ip, where string) bool {
	if !h.sameOrigin(r) {
		h.log.Warn("panel recovery refused: not same-origin", "at", where, "ip", ip)
		h.problem(w, r, http.StatusBadRequest, problemResetRestart)
		return false
	}
	ck, err := r.Cookie(adminResetCookieName)
	if err != nil || ck.Value == "" {
		h.problem(w, r, http.StatusBadRequest, problemResetNoCookie)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, adminResetMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.problem(w, r, http.StatusBadRequest, problemResetServer)
		return false
	}
	if !constantTimeMatch(ck.Value, r.PostFormValue("csrf")) {
		h.log.Warn("panel recovery refused: csrf mismatch", "at", where, "ip", ip)
		h.problem(w, r, http.StatusBadRequest, problemResetRestart)
		return false
	}
	return true
}

// readLink lifts the synchronizer token and the raw link value off the request. The
// value is "<csrf>.<token>"; both halves are base64url, and '.' is outside that
// alphabet, so the split is unambiguous.
//
// A cookie that does not split into exactly two non-empty parts is treated as ABSENT
// rather than repaired, which is adminCookies.readLogin's rule: a value in that shape
// was not written by this flow, and guessing at what half of it means is how a parser
// becomes an attack surface.
func (h *AdminReset) readLink(r *http.Request) (csrf, token string, ok bool) {
	ck, err := r.Cookie(adminResetLinkCookieName)
	if err != nil || ck.Value == "" {
		return "", "", false
	}
	csrf, token, found := strings.Cut(ck.Value, ".")
	if !found || csrf == "" || token == "" {
		return "", "", false
	}
	return csrf, token, true
}

func (h *AdminReset) clearLink(w http.ResponseWriter) {
	h.cookies.clearResetLink(w)
}

// constantTimeMatch compares a synchronizer token from a cookie with the one a form
// echoed, in constant time (redline R7: a comparison that decides an authentication
// outcome is never == or bytes.Equal). ConstantTimeCompare returns 0 for differing
// lengths, so an absent form field can never match.
//
// It is adminLoginState.csrfMatches without the struct: this flow's two forms carry
// only a synchronizer value, never a binding half, so there is nothing for that type
// to hold here.
func constantTimeMatch(held, sent string) bool {
	if held == "" || sent == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(held), []byte(sent)) == 1
}

// checkNewPassword returns "" when the two boxes are an acceptable new password, or
// the sentence to show.
//
// THE BOUNDS ARE internal/domain/signup's, NOT NEW ONES. That package already decided
// what a Tappa panel password may be (a twelve-character floor with no composition
// rule, and bcrypt's 72-BYTE ceiling), and a second set of numbers here would be a
// second representation of one product rule — the shape this repository has paid for
// more than once. A person who registers and a person who recovers must not be able
// to end up with passwords the other could not have chosen.
func checkNewPassword(chosen, confirm string) string {
	switch {
	case chosen == "":
		return "Choose a password."
	case utf8.RuneCountInString(chosen) < signup.MinPasswordRunes:
		return "Use at least " + strconv.Itoa(signup.MinPasswordRunes) + " characters. A few words you will remember beats a short one you will not."
	case len(chosen) > signup.MaxPasswordBytes:
		// BYTES, not runes: bcrypt's limit is a byte limit and its comparer silently
		// truncates past it, so a 100-byte password would authenticate an account
		// whose password is its first 72 bytes.
		return "That password is too long — keep it under 72 bytes."
	case confirm == "":
		return "Type the password again to confirm it."
	case chosen != confirm:
		return "The two passwords are not the same."
	}
	return ""
}

// floor returns the function that holds this response until resetRequestFloor has
// elapsed. See that constant for what the floor buys and what it does not.
func (h *AdminReset) floor() func() {
	started := time.Now()
	return func() {
		rest := resetRequestFloor - time.Since(started)
		if rest <= 0 {
			return
		}
		if h.sleep != nil {
			h.sleep(rest)
			return
		}
		time.Sleep(rest)
	}
}

// sameOrigin checks the Origin header against this deployment's own, STRICTLY: an
// absent Origin falls back to the fetch metadata and is refused if that is missing
// too.
//
// IT IS A FOURTH COPY OF THIS FUNCTION AND THAT IS DELIBERATE, for the reason
// adminlogin.go gives about the second and signup.go about the third: the activation
// version carries a `strict` flag whose two settings were argued over four audit
// rounds, and re-shaping it as a side effect of adding a recovery form is how a
// settled flow acquires an unreviewed change. The duplication is named so it is a
// decision rather than an accident.
func (h *AdminReset) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
		case "same-origin", "same-site":
			return true
		default:
			return false
		}
	}
	return strings.EqualFold(strings.TrimRight(origin, "/"), strings.TrimRight(h.baseURL, "/"))
}

// render writes one component with the headers every recovery screen needs.
//
// THE POLICY IS adminCSP, BY REFERENCE RATHER THAN BY COPY, and that is the one thing
// this type shares with AdminAuth. These pages are panel screens that load no script,
// which is exactly what that constant describes — including form-action 'self', the
// directive that matters most here because two of these four pages carry a password
// form. Deriving a fifth identical constant would be the "two policies to keep in
// step" shape adminlogin.go argues against; copying the HEADER BLOCK is unavoidable
// and cheap (four Set calls), and it is the same four every other surface sets.
func (h *AdminReset) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", adminCSP)
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		if errors.Is(err, context.Canceled) {
			// A visitor who left mid-render is not a fault — handler.Marketing
			// measured what logging it at ERROR costs on a public URL.
			h.log.Debug("a visitor left before the page finished", "path", r.URL.Path)
			return
		}
		// The status line is already on the wire, so there is nothing to send but a
		// log line. Never swallowed (§7).
		h.log.Error("rendering a recovery page failed", "err", err)
	}
}

func (h *AdminReset) problem(w http.ResponseWriter, r *http.Request, status int, v pages.ProblemView) {
	h.render(w, r, status, pages.Problem(v))
}

// redirect answers 303 See Other: the browser follows with GET, which turns a POST
// into a plain page load and makes a refresh harmless.
func (h *AdminReset) redirect(w http.ResponseWriter, to string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Location", to)
	w.WriteHeader(http.StatusSeeOther)
}
