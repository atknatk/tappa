package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/pages"
)

// Tap serves GET /t — the screen a wall plaque opens (M5-04).
//
// IT IS THE ONE SCREEN THE PRODUCT IS ABOUT, and CLAUDE.md §9 calls it sacred:
// one screen, one button, zero learning. No menu, no tabs, no settings, nothing
// to read but a name and a venue. Anything anyone wants to add here is a
// question for the product owner first.
//
// WHAT THIS ENDPOINT DOES NOT DO, and both absences are load-bearing:
//
//   - IT WRITES NO `transactions` ROW. Not for a dead tag, not for a bad CMAC,
//     not for a session-less visitor. §5 row 3 forbids a record for the
//     session-less case specifically, and for the rest the reason is simpler:
//     nothing has been decided yet. A person who opens this page has not
//     clocked in — they have looked at a button.
//   - IT DOES NOT ADVANCE THE CHIP'S COUNTER. That is the M5-04 card's central
//     requirement and the reason internal/sun grew a second entry point: an
//     employee who opens the page and walks away must not spend a counter value
//     that their real tap needs. See sun.PreviewWithoutReplayProtection, whose
//     name is as unfriendly as it is on purpose.
//
// 🔴 WHICH MEANS THE REPLAY GUARD IS ENTIRELY M5-05'S TO RUN. A page that has
// pre-checked the CMAC has established half of SUN validity (tapcontext.go
// states the contract in full); the other half — the strict, atomic advance of
// tags.last_ctr — happens on POST /api/checkin or it does not happen at all.
//
// ORDER OF CHECKS, and why identity comes before the tag:
//
//	· parse the SUN URL     pure, no state, and a malformed URL is not a tap.
//	· identity              §5 row 3: no session -> activation page, no record.
//	· SUN pre-check         resolve the tag, verify the CMAC, advance NOTHING.
//	· display facts         name + venue, inside the SESSION's tenant.
//	· mint the context      what the POST will be allowed to act on.
//
// Identity before the tag is deliberate. It costs nothing (the middleware
// already resolved it) and it closes an oracle: with the tag looked up first,
// an anonymous caller could tell "unknown plaque" apart from "activation
// page" and enumerate which uids exist. Since a session-less visitor can do
// nothing with any answer about a tag, there is no reason to give them one.
type Tap struct {
	sun       tapPreviewer
	directory tapDirectory
	sessions  sessionVerifier
	checkins  checkinRecorder
	cookies   session.Cookies
	contexts  tapContexts
	limiter   *httpx.TapLimiter
	// baseURL is this deployment's own origin, for the Origin check on the POST
	// (checkin.go). Reduced to scheme://host at construction, the way an Origin
	// header is written, so a BaseURL with a path still compares.
	baseURL string
	log     *slog.Logger
}

// ⚠️ THE AUDIT RECORDER IS NOT A FIELD ON Tap, and that is not an oversight:
// nothing this handler does writes to audit_log. The ONE thing on this path
// that must leave a trail is a REFUSED tap, and the refusal happens in the
// middleware, above the handler — so the recorder is handed to TapLimiter at
// construction and lives there. See NewTap.

// Consumer-side interfaces (§7): the narrow slice of each dependency this
// screen needs, declared here so a producer cannot widen what the HTTP layer
// may do and so the handler is testable against fakes.
type (
	// tapPreviewer is the NON-ADVANCING half of internal/sun. The interface
	// mentions only that method, so a Tap handler CANNOT reach sun.Verify even
	// by accident — advancing the counter is not in its vocabulary.
	tapPreviewer interface {
		PreviewWithoutReplayProtection(ctx context.Context, p sun.Params) (sun.Preview, error)
	}
	tapDirectory interface {
		TapPage(ctx context.Context, tenantID, employeeID, locationID uuid.UUID) (tenant.TapPageFacts, error)
	}
	sessionVerifier interface {
		Verify(ctx context.Context, t session.Token) (session.Resolved, error)
	}
)

// NewTap wires the screen. Every dependency is required: a nil one cannot fail
// safely on a path whose failure mode is a missing hour on a payslip.
func NewTap(preview tapPreviewer, dir tapDirectory, sess sessionVerifier, checkins checkinRecorder, rec auditRecorder, cfg *config.Config, log *slog.Logger) (*Tap, error) {
	switch {
	case preview == nil:
		return nil, errors.New("handler: nil sun previewer")
	case dir == nil:
		return nil, errors.New("handler: nil directory")
	case sess == nil:
		return nil, errors.New("handler: nil session manager")
	case isNil(checkins):
		// REQUIRED for the same reason the recorder is: this is the production
		// path, and the ONE thing POST /api/checkin exists to do is write an
		// attendance record. A nil here would not degrade the endpoint, it would
		// delete it — and it would do so at the first tap, as a panic, rather than
		// at boot. isNil rather than == nil because a TYPED nil in an interface
		// parameter is not nil (the same trap the recorder check documents).
		return nil, errors.New("handler: nil checkin service")
	case isNil(rec):
		// REQUIRED, not optional. httpx.TapLimiter accepts a nil recorder
		// because it must be constructible before a database is (tests, a future
		// read-only surface) — but THIS constructor is the production path, and
		// a nil here would silently delete half of an ACCEPTED M5-03 criterion:
		// "a refused tap whose session resolved leaves one audit_log row per
		// window". An audit measured the first version of this file doing
		// exactly that (285 served, 15 refused, audit_log unchanged at 4145),
		// so the omission is now a startup error rather than a quiet gap.
		//
		// isNil, not `rec == nil`: a TYPED nil — (*audit.Recorder)(nil) handed to
		// an interface parameter — is a non-nil interface holding a nil pointer,
		// so the plain comparison lets it through and the failure resurfaces at
		// the first refusal as a panic instead of at boot. Unreachable in
		// production today (audit.New never returns a nil recorder without an
		// error, and main stops on the error), which is exactly why the earlier
		// version of this check survived review while claiming more than it did.
		return nil, errors.New("handler: nil audit recorder")
	case cfg == nil:
		return nil, errors.New("handler: nil config")
	}
	if log == nil {
		log = slog.Default()
	}
	ctxs, err := newTapContexts(cfg)
	if err != nil {
		return nil, err
	}
	t := &Tap{
		sun:       preview,
		directory: dir,
		sessions:  sess,
		checkins:  checkins,
		cookies:   session.NewCookies(cfg),
		contexts:  ctxs,
		baseURL:   originOf(cfg.BaseURL),
		log:       log,
	}
	t.limiter = httpx.NewTapLimiter(httpx.TapLimitParams{
		Log: log,
		// 🔴 THE HALF THAT WAS MISSING. TapLimiter's own documentation states the
		// mounting contract as NewTapLimiter(TapLimitParams{Audit: rec, Log: log})
		// and promises that a refusal with a resolved session writes one
		// audit_log row per window; the M5-03 card carries that as an accepted
		// criterion. M5-03 shipped the CAPABILITY and handed the MOUNTING here
		// (state.md hand-off 1), and the first version of this file mounted it
		// without the recorder — so the criterion was true of the middleware and
		// false of the product. Measured after wiring: N refusals against one
		// resolved session produce exactly ONE row (FirstOverLimit), and a
		// refusal with no session produces none, because audit_log.tenant_id is
		// NOT NULL and no "system tenant" is invented (db/queries/audit.sql).
		Audit: rec,
		// The abuse shield answers with the brand's own page rather than the
		// plain text httpx falls back to. state.md carried this as a hand-off
		// from M5-03: "the 429 body is plain text — a branded page is
		// tappa-brand's work and belongs with the screen it sits beside", and
		// this is that screen. The renderer is INJECTED rather than imported by
		// internal/httpx, so the middleware layer still does not depend on
		// templates.
		Refused: t.renderTooManyRequests,
	})
	return t, nil
}

// Mount registers GET /t behind the tap middleware, IN THE ORDER TapLimiter's
// documentation fixes as a contract (state.md hand-off 1 to M5-04/M5-05):
//
//	ByAddress  -> Identify -> BySession
//
// The order is not cosmetic. ByAddress must run BEFORE any database work, since
// a shield that has already made a query has shed nothing; BySession must run
// AFTER Identify, because there is no session to meter until one is resolved —
// and mounting it before Identify is now a LOUD failure (500 + an error log)
// rather than a silent unmetered pass, which is what an audit measured the last
// version doing 100 times out of 100.
//
// ⚠️ RESIDUAL, INHERITED AND NOT FIXED HERE (state.md hand-off 2). If a hostile
// device spends a venue's shared address budget, that venue's LEGITIMATE taps
// get 429 — and a 429 from the shield produces neither a `transactions` row nor
// a `flag`, because the request never reaches the decision engine. That is a
// real tension with §4.6, it was bounded rather than resolved in M5-03, and
// mounting the shield here does not change it either way. What would resolve it
// is a shared store plus per-venue keying (M8).
//
// POST /api/checkin IS MOUNTED HERE, in the same group and behind the same
// limiter INSTANCE — which is the point rather than a convenience. A tap costs
// two requests (the page, then the button), and metering them against separate
// budgets would mean neither budget describes a tap. It also means the shield the
// M5-03 audit measured is the one both endpoints get, rather than a second one
// somebody wired up later with different arguments.
func (t *Tap) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(t.limiter.ByAddress)
		r.Use(httpx.Identify(t.cookies, t.sessions))
		r.Use(t.limiter.BySession)
		r.Get("/t", t.Page)
		r.Post("/api/checkin", t.Checkin)
	})
}

// The failure screens for this endpoint. Package-level constants for the same
// reason the activation flow's are (activate.go): a screen assembled at the
// call site drifts, and two screens that must be indistinguishable stop being
// so the moment somebody adds a helpful detail to one of them.
var (
	tapProblemBadURL = pages.ProblemView{
		Title:   "That link didn't work",
		Message: "This page opens when you hold your phone to the plaque at the door.",
		Hint:    "Try tapping the plaque again.",
	}
	tapProblemUnknownTag = pages.ProblemView{
		Title:   "We don't know that plaque",
		Message: "This plaque isn't set up with Tappa, or it has been replaced.",
		Hint:    "Tell your manager which door you tapped.",
	}
	tapProblemServer = pages.ProblemView{
		Title:   "Something went wrong on our side",
		Message: "Nothing was recorded. This is not your fault and it is not your time.",
		Hint:    "Tap the plaque again in a moment.",
	}
	tapProblemTooMany = pages.ProblemView{
		Title:   "Too many taps from here",
		Message: "We have paused requests from this network for a few minutes.",
		Hint:    "Wait a moment and tap the plaque again. Tell your manager if it keeps happening.",
	}
	// tapProblemStale answers a tap whose signed context did not verify — expired,
	// tampered with, or minted for another phone. The three are DELIBERATELY
	// indistinguishable to the visitor (§4.7: no oracle); the distinction is in
	// the log. The advice is the same in all three cases and it works: touch the
	// plaque again and a fresh context is minted.
	tapProblemStale = pages.ProblemView{
		Title:   "That tap has expired",
		Message: "Nothing was recorded. Tap pages are only good for a few minutes.",
		Hint:    "Hold your phone to the plaque again.",
	}
	// tapProblemForeignTenant answers sys:tenant-mismatch: a real session for one
	// organisation, in front of another organisation's plaque. It names NEITHER
	// organisation — one tenant's venue is not disclosed to another tenant's
	// employee (§4.5) — and it does not send them to activation, because their
	// session is perfectly good.
	tapProblemForeignTenant = pages.ProblemView{
		Title:   "That plaque isn't yours",
		Message: "This plaque belongs to a different employer, so nothing was recorded.",
		Hint:    "Use your own workplace's plaque, or tell your manager.",
	}
)

// Page serves GET /t.
func (t *Tap) Page(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see internal/handler/checkin.go: since round 4 this buys R7/R7b/R7c one extra level of paren depth, not visibility itself.
	// The SUN URL is validated at the HTTP boundary, so the domain layer below
	// only ever sees valid data (§7). sun.Parse never panics on hostile input
	// (FuzzParse) and its error names a parameter and a failure KIND, never a
	// value — safe to log (§4.7).
	p, err := sun.Parse(r.URL.Query())
	if err != nil {
		t.log.InfoContext(ctx, "tap page: unusable url", "err", err)
		t.renderProblem(w, r, http.StatusBadRequest, tapProblemBadURL)
		return
	}

	id := httpx.IdentityOf(r)
	switch id.State {
	case httpx.SessionUnresolved:
		// 🔴 THIS IS NOT "NO SESSION", and conflating the two is the trap
		// state.md hand-off 3 names: Identity's ZERO VALUE has Err == nil AND
		// Live() == false, so `if id.Err != nil {500} else if !id.Live()
		// {activate}` would read a middleware that was never mounted — or a
		// database outage — as a genuine session-less tap, and answer an outage
		// with a stream of activation redirects. Unresolved means "nobody
		// looked, or looking failed"; both are conditions under which we do not
		// know who this is, so we say so loudly instead of guessing.
		t.log.ErrorContext(ctx, "tap page: no resolved identity on the request",
			"hint", "mount httpx.Identify in front of GET /t",
			"err", id.Err, "path", r.URL.Path)
		// Retryable: one of the two things this state means is "looking FAILED",
		// which is a database that may be back in a second.
		t.renderRetryableProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return

	case httpx.SessionAbsent, httpx.SessionRevoked:
		// §5 ROW 3: no session, or an invalid one -> the activation page, and NO
		// RECORD. This redirect is the wiring M5-02 could not do (it built the
		// destination and said so); it is done here.
		//
		// A REVOKED session belongs on this branch and a DEACTIVATED EMPLOYEE
		// does NOT — the distinction session.Verify's API trap is about. A
		// revoked session means this phone was signed out (a stolen handset, a
		// second device), so there is genuinely no session and row 3 applies. A
		// deactivated employee still holds a LIVE session on purpose (M5-01:
		// deactivation deliberately does not revoke, so that the attempt reaches
		// row 4 and gets RECORDED) — they fall through to the page, press the
		// button, and sys:employee-deactivated writes the rejected attempt.
		// Turning either of those into the other would lose a record (§4.6).
		t.redirectToActivation(w, r)
		return

	case httpx.SessionLive:
		// fall through

	default:
		// A SessionState this file does not know cannot be reasoned about, so it
		// is not guessed at. Unreachable today; the branch exists so that adding
		// a state does not silently pick a behaviour.
		t.log.ErrorContext(ctx, "tap page: unknown session state", "state", int(id.State))
		// NO retry: this is a code path that does not exist yet being taken, which
		// re-fetching reproduces exactly. Offering a button here would be the fake
		// affordance renderRetryableProblem's comment refuses.
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}

	// SUN PRE-CHECK. This resolves the tag, checks the chip's MAC against the
	// tag's key, and ADVANCES NOTHING (M5-04's central requirement). It cannot
	// tell a fresh touch from a replay and does not pretend to; POST /api/checkin
	// runs the atomic advance.
	pv, err := t.sun.PreviewWithoutReplayProtection(r.Context(), p)
	if err != nil {
		if errors.Is(err, sun.ErrUnknownTag) {
			// A uid that matches no tag carries no tenant and no location, so
			// there is nothing to render a page around and nothing to record
			// against (internal/sun states the same reasoning where the sentinel
			// is defined). It is also a security-relevant event — somebody is
			// holding a URL that names a plaque we do not have — so it is
			// logged, with the uid, which is not a secret: the chip prints it in
			// the address bar.
			t.log.WarnContext(ctx, "tap page: unknown plaque", "tag_uid", p.UID, "employee_id", id.EmployeeID())
			t.renderProblem(w, r, http.StatusNotFound, tapProblemUnknownTag)
			return
		}
		t.log.ErrorContext(ctx, "tap page: sun pre-check failed", "tag_uid", p.UID, "err", err)
		// Retry offered on a MIXED error class — an outage and a corrupt key ref
		// arrive here identically (internal/sun/preview.go). Stated at
		// renderRetryableProblem rather than claimed away.
		t.renderRetryableProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}

	// THE PAGE RENDERS EVEN WHEN THE PRE-CHECK SAYS NO — for a retired or lost
	// plaque (§5 row 1) and for a CMAC that did not verify (§5 row 2). That
	// looks wrong for about ten seconds and then it is the only thing §4.6
	// allows: every one of those outcomes is a `reject` that MUST BE RECORDED,
	// the record is written by POST /api/checkin, and a page that refused to
	// render would mean the button is never pressed and the rejected attempt
	// leaves no trace at all. So the outcome travels in the signed context, the
	// person presses one button, and the decision engine says no in writing.
	// (The only case that cannot work this way is an unknown uid above, which
	// has no tenant to record against.)

	// 🔴 THE PLAQUE'S WALL IS NULLABLE AND IS FLATTENED HERE, ONCE, WITH A NAME.
	// sun.Preview.Location is a *uuid.UUID because migration 00013 made tags.location_id
	// nullable and the driver reports a SQL NULL as uuid.Nil without an error
	// (db.ResolvedTag.LocationID carries the measurement). uuid.Nil is exactly what
	// TapPage already treats as ErrForeignLocation — "render without a venue name" —
	// so a plaque still in its box takes the path a plaque belonging to another
	// tenant takes: the page renders, the button is pressed, and the POST RECORDS the
	// refusal (§4.6) instead of the page silently dropping it.
	wall := tappedWallOf(pv)
	facts, err := t.directory.TapPage(r.Context(), id.Session.TenantID, id.Session.EmployeeID, wall)
	switch {
	case errors.Is(err, tenant.ErrForeignLocation):
		// The plaque belongs to another tenant. The page renders WITHOUT a venue
		// name — one tenant's venue is not shown to another tenant's employee —
		// and the tap proceeds to the POST, where sys:tenant-mismatch is the
		// authority once M5-05 feeds it both tenants (hand-off N5). Refusing
		// here would decide it, and would decide it without a record.
		t.log.WarnContext(ctx, "tap page: plaque belongs to another tenant",
			"tag_uid", p.UID, "employee_id", id.EmployeeID(),
			"session_tenant_id", id.TenantID(), "tag_tenant_id", pv.TenantID)
	case err != nil:
		t.log.ErrorContext(ctx, "tap page: loading the employee failed", "employee_id", id.EmployeeID(), "err", err)
		t.renderRetryableProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}

	signed, err := t.contexts.mint(tapContext{
		UID:     p.UID,
		Ctr:     p.Ctr,
		Channel: p.Channel,
		// Half of SUN validity; the atomic advance at POST time is the other
		// half (tapcontext.go carries the full contract).
		CMACVerified: pv.CMACValid,
		// Carried, never compared: the tag half of the N5 hand-off.
		TagTenantID: pv.TenantID,
		LocationID:  wall,
	}, id.Session.ID)
	if err != nil {
		t.log.ErrorContext(ctx, "tap page: minting the tap context failed", "tag_uid", p.UID, "err", err)
		// NO retry: mint fails on exactly two DETERMINISTIC conditions (an
		// unconfigured signing key, a nil session id — tapcontext.go), and a second
		// fetch changes neither. Same reasoning as the unknown-session-state branch.
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}

	t.render(w, r, http.StatusOK, pages.Tap(pages.TapView{
		EmployeeName: facts.EmployeeName,
		LocationName: facts.LocationName,
		TapContext:   signed,
	}))
}

// tappedWallOf is where the plaque's nullable wall becomes a plain id for this
// page, and it is a named function rather than two dereferences so the answer to
// "what does the page do with a plaque that has no wall" lives in one place.
//
// 🔴 uuid.Nil IS THE ANSWER ON PURPOSE, AND IT IS NOT A HOLE. tenant.TapPage
// already returns ErrForeignLocation for it — a case that has its own test
// (TestTapPage_NilLocationIsForeignNotAnError) — so the page renders WITHOUT a
// venue name and the tap proceeds to the POST, where the decision engine refuses
// it at §5 row 1 and WRITES the refusal. Refusing to render here would decide the
// tap on the page, and decide it without a record (§4.6).
func tappedWallOf(pv sun.Preview) uuid.UUID {
	if pv.Location == nil {
		return uuid.Nil
	}
	return *pv.Location
}

// tapCSP is the content policy for this screen.
//
// DEFENCE IN DEPTH, NOT A FIX. A security audit went looking for an injection
// vector here and did not find one — templ escapes every interpolated value and
// a live XSS attempt came back escaped — so this closes nothing that is
// currently open. It is cheap because the page already has the shape a strict
// policy wants: one external stylesheet, one external script, self-hosted
// fonts, no inline anything.
//
//	default-src 'none'     nothing loads unless named below
//	script-src / style-src / font-src 'self'
//	                       our own origin only; there is no CDN to allow
//	form-action 'self'     a browser that honours the policy will not let the
//	                       one button be repointed at another host
//	base-uri 'none'        a <base> tag cannot re-root the relative asset paths
//	frame-ancestors 'none' THE ONE WITH A CONCRETE THREAT BEHIND IT: this page's
//	                       whole content is a single large button that records
//	                       attendance, which is the ideal thing to frame
//	                       invisibly under someone else's page and have them
//	                       click. Refusing to be framed removes that outright.
//
// SCOPE: this header is set on THIS package's tap responses. The activation
// screens (M5-02) do not carry it — extending it there is a deliberate change to
// a flow that took four audit rounds to settle, and it belongs in its own task
// rather than as a side effect of this one.
const tapCSP = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; " +
	"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// isNil reports whether v is nil OR a nil pointer wrapped in a non-nil
// interface. See the audit recorder check in NewTap for why the distinction
// matters at a constructor boundary.
func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

// redirectToActivation sends a session-less tap to the activation flow (§5 row
// 3). It writes NO record, which is what row 3 requires, and it is a 303 so the
// browser follows with GET.
//
// ⚠️ WHAT THIS REDIRECT WALKS INTO, evaluated rather than waved past
// (state.md hand-off 4, and internal/handler/cookies.go names M5-04 by name).
// M5-02's third measure is CONDITIONAL: on a phone with NO session, a
// cross-site GET can still plant a `tappa_activation` cookie, and /activate
// renders whatever that cookie holds. So a phone carrying a planted cookie that
// taps a plaque lands on a STRANGER'S activation form.
//
// MEASURED, not assumed: that is not a new capability. Planting the cookie
// already requires the victim to follow the attacker's link, and that same
// navigation ALREADY answers 303 to /activate and renders the form
// immediately — so the attacker never needed a plaque to show it. What this
// redirect changes is WHEN the form can appear (later, in a context where the
// person expects a Tappa screen), not WHETHER it can. The mitigation is the one
// M5-02 shipped: the form names the employer and the employee directly above
// the button, so the only thing between a planted cookie and a confused tap is
// a person reading a name they do not recognise.
//
// Deliberately NOT done here: giving the redirect its own one-shot marker so
// /activate could refuse a planted cookie arriving this way. It would mean
// changing how the activation flow decides which cookie to believe — that flow
// is not this task's, its cookie handling took four audit rounds, and a change
// made from the outside is how a defence that was measured stops being the one
// that was measured. It is written down instead, here and on the card.
func (t *Tap) redirectToActivation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/activate", http.StatusSeeOther)
}

// renderTooManyRequests is the branded 429 body, handed to httpx.TapLimiter at
// construction. The limiter has already set Retry-After, Cache-Control and
// X-Content-Type-Options; this writes the status and the page.
func (t *Tap) renderTooManyRequests(w http.ResponseWriter, r *http.Request) {
	t.renderProblem(w, r, http.StatusTooManyRequests, tapProblemTooMany)
}

func (t *Tap) renderProblem(w http.ResponseWriter, r *http.Request, status int, v pages.ProblemView) {
	t.render(w, r, status, pages.Problem(v))
}

// renderRetryableProblem is renderProblem plus the M5-06 card's "Try again", and
// it exists as a SEPARATE entry point so that offering a retry is a decision
// somebody made at a call site rather than a default every screen inherits.
//
// 🔴 THE RULE IS NOT "THE FAILURE IS TRANSIENT", BECAUSE THIS HANDLER CANNOT
// KNOW THAT. An earlier version of this comment said it was, and an audit showed
// the classification is not one the code can make: the branch below that answers
// a failed SUN pre-check catches EVERY non-ErrUnknownTag error from
// PreviewWithoutReplayProtection, and that function's own contract
// (internal/sun/preview.go) lists a database outage and a CORRUPT aes_key_ref or
// wrong KEK in the same class. The permanent half is not hypothetical — the
// seeded plaques in this repo carry a key ref that is not KEK-wrapped and answer
// 500 on the NFC path every time (state.md hand-off from M5-05). So the honest
// rule is narrower and is about COST, not about diagnosis:
//
//	OFFER A RETRY ONLY WHERE (a) the address is a GET that writes no record and
//	advances no counter, and (b) the failure is not one this code can PROVE is
//	deterministic. Where a retry can help it costs one request; where it cannot,
//	it costs one wasted press and the instruction underneath still stands.
//
// Which is why two of the FIVE 500 branches on this page do NOT get a link. Five,
// not four: an earlier version of this list counted the branches it had opinions
// about and silently dropped the unknown-session-state one, which is the same
// class of error as a field-count comment that stopped matching its struct — so
// the enumeration below is exhaustive against the five CALL SITES in Page that
// render tapProblemServer. (Stated against call sites rather than "grep returns
// five": an earlier version said that, and then a later edit quoted the pattern
// inside this very comment and made its own count wrong. A sentence that measures
// the file it lives in is a sentence that breaks itself.) TestTapPage_EveryGetServerErrorBranchIsAccountedFor drives FOUR of
// the five; the unknown-session-state branch is not reachable from a test, because
// httpx.Identity's State is written by the middleware from a closed set and the
// context key is unexported, so nothing outside internal/httpx can hand this
// handler a value the switch does not know. That branch is verified by reading.
//
//	tapProblemServer on GET /t        FIVE branches; THREE carry a link. The
//	                                  plaque's own URL, re-fetched. GET /t writes
//	                                  no `transactions` row and advances no
//	                                  `tags.last_ctr` (this file's header states
//	                                  both, and the domain never runs), so the
//	                                  press is free; a database that was down a
//	                                  second ago may well be up. On a corrupt key
//	                                  ref it reproduces the 500 — a wasted press,
//	                                  said here rather than denied.
//	  · identity unresolved           RETRY. Through Mount, Identify is always in
//	                                  front, so in production this state means the
//	                                  lookup FAILED. (The other half — no
//	                                  middleware at all — is a wiring bug only a
//	                                  direct call can produce.)
//	  · unknown session state         NO. A SessionState this file does not know
//	                                  is a code path that does not exist yet being
//	                                  taken; re-fetching reproduces it exactly.
//	  · sun pre-check failed          RETRY, and this is the mixed one above.
//	  · directory read failed         RETRY. Its errors are query errors; the
//	                                  foreign-location case is handled separately
//	                                  and never reaches here.
//	  · mint failed                   NO — provably deterministic: tapcontext.go
//	                                  raises it on exactly two conditions (an
//	                                  unconfigured key, a nil session id), and a
//	                                  second fetch changes neither.
//
// Splitting the sun error class properly is internal/sun's job and a real task;
// it is not something a comment here can do, and pretending otherwise is what
// made an earlier version of this text false.
//
//	tapProblemServer on POST          NO. There is no address to offer: the tap
//	                                  context is single-use and the chip's SUN
//	                                  URL — the only thing that could mint a new
//	                                  one — is not in a POST request. The hint
//	                                  already says the true instruction.
//	tapProblemBadURL                  NO. The URL did not parse; re-fetching it
//	                                  reproduces the same failure exactly.
//	tapProblemUnknownTag              NO. A uid we do not have stays a uid we do
//	                                  not have. The screen sends them to a
//	                                  manager, which is the actionable answer.
//	tapProblemTooMany                 NO, AND THIS ONE IS DELIBERATE RATHER THAN
//	                                  MERELY USELESS: a retry button on a rate
//	                                  limit invites spending the budget the page
//	                                  is asking them to stop spending.
//	tapProblemStale                   NO. A fresh context comes from touching the
//	                                  plaque, not from re-fetching anything we
//	                                  could name.
//	tapProblemForeignTenant           NO. Their session is fine and the plaque is
//	                                  somebody else's; no number of retries makes
//	                                  that a tap.
//
// THE GET GUARD IS STRUCTURAL, not a comment. r.URL.RequestURI() on a POST is
// /api/checkin, and a link to it would be a 405 dressed as help — so a non-GET
// request silently gets the plain page instead. Fail-closed: the worst case is a
// screen that says what to do without a button, which is what the other six do.
func (t *Tap) renderRetryableProblem(w http.ResponseWriter, r *http.Request, status int, v pages.ProblemView) {
	if r.Method == http.MethodGet {
		v.RetryURL = r.URL.RequestURI()
	}
	t.renderProblem(w, r, status, v)
}

// render writes one component with the headers every screen on this path needs.
//
// Cache-Control: no-store on ALL of them, including the failure pages. The tap
// page greets somebody by name and carries a signed context; a shared phone's
// back button must not resurrect either from a cache, and no intermediary
// should keep a copy.
func (t *Tap) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	ctx := r.Context() // see internal/handler/checkin.go: since round 4 this buys R7/R7b/R7c one extra level of paren depth, not visibility itself.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", tapCSP)
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		// The status line is already on the wire, so there is nothing to send
		// but a log line. Never swallowed (§7).
		t.log.ErrorContext(ctx, "rendering the tap page failed", "err", err)
	}
}
