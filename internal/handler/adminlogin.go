package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// adminAuthenticator is the slice of adminauth.Manager this package needs,
// declared HERE at the consumer (section 7).
type adminAuthenticator interface {
	Authenticate(ctx context.Context, email, password string) (adminauth.Authentication, error)
	TenantChoices(ctx context.Context, verified []adminauth.Verified) ([]adminauth.Choice, error)
	Issue(ctx context.Context, v adminauth.Verified) (adminauth.Issued, error)
	Verify(ctx context.Context, t adminauth.Token) (adminauth.Resolved, error)
	Revoke(ctx context.Context, tenantID, sessionID uuid.UUID) error
}

// Audit actions written by the panel auth flow. The vocabulary is free text by
// schema decision (00005), so these constants are the vocabulary.
const (
	ActionAdminLoginSucceeded = "admin.login.succeeded"
	ActionAdminLoginFailed    = "admin.login.failed"
	ActionAdminLoginLimited   = "admin.login.rate_limited"
	ActionAdminLoginRefused   = "admin.login.tenant_refused"
	ActionAdminLoggedOut      = "admin.logout"
)

// AdminAuth serves panel authentication: the login screen, the "which business?"
// picker, sign-out, and — since M6-02 — the protected panel shell those sign a
// person into (dashboard.go).
//
// ROUTES, AND WHY EACH EXISTS:
//
//	GET  /admin/login         the form. Mints the synchronizer token and the value
//	                          the choice blob is bound to.
//	POST /admin/login         resolves the email GLOBALLY, verifies the password
//	                          against every candidate it is willing to compare, and
//	                          either issues a session (one match) or hands off to
//	                          the picker (several).
//	GET  /admin/login/choose  the picker. Reads the SIGNED verified set from an
//	                          HttpOnly cookie and shows one row per business.
//	POST /admin/login/choose  re-proves membership of the signed set, then issues.
//	POST /admin/logout        revokes the session and clears the cookie.
//	GET  /admin               the panel. Transactions is the section sign-in lands
//	                          on; every other section is mounted beside it from
//	                          pages.PanelSections (dashboard.go).
//	POST /admin/review        the FLAGGED approval queue's decision (M6-04) — the
//	                          panel's one mutating route, and the only one besides
//	                          sign-out that carries sameOriginGate.
//
// EVERYTHING IS UNDER /admin, and that is a requirement rather than tidiness: the
// panel cookie is Path=/admin (adminauth.CookiePath), so a route outside that
// prefix would arrive with no cookie and look permanently signed out.
//
// WHY THE PICKER IS A SEPARATE GET RATHER THAN THE POST'S RESPONSE. A rendered POST
// means a refresh re-submits the PASSWORD. With the 303 the picker is a plain page
// load, refreshable, and the password exists for exactly one request.
//
// WHAT THIS FLOW DELIBERATELY DOES NOT DO:
//
//   - It does not tell anyone whether an email exists. See renderLoginFailure.
//   - It does not lock an account out. See adminratelimit.go for the two readings
//     and why the address is metered instead.
//   - It does not read the panel cookie on GET /admin/login to "helpfully" skip a
//     login somebody is already signed in for. That would put a resolver read plus
//     an UPDATE on an unauthenticated endpoint for a convenience, and the cost of
//     NOT doing it is one extra click.
type AdminAuth struct {
	admins adminAuthenticator
	audit  auditRecorder
	// cookies writes the PANEL SESSION cookie (internal/adminauth owns its
	// attributes).
	cookies adminauth.Cookies
	// short writes the two SHORT-LIVED auth cookies: the synchronizer token and
	// the signed verified-candidate set (logincontext.go).
	short   adminCookies
	choices adminChoices
	// confirm mints and verifies the deactivation confirmation (M6-05 phase B, user
	// decision 2026-08-08). It is a SEPARATE keyed value from choices — its own
	// label, its own TTL, its own bindings — so a blob minted for one purpose can
	// never be spent on the other.
	confirm adminConfirm
	// baseURL is this deployment's own origin, for the Origin header check.
	baseURL string

	// ledger is the panel's READ side (M6-03, internal/domain/ledger). It is on
	// this type rather than on one of its own for the reason dashboard.go gives:
	// the section handlers need a.render (which sets the policy, the cache header
	// and nosniff in ONE place) and the identity the Protect chain resolved, and
	// AdminAuth already owns both. A second type would either duplicate the header
	// block — two policies to keep in step, the failure class this repo has paid
	// for three times — or need render exported, which makes those headers
	// optional for whoever calls it next.
	ledger panelLedger

	// queue is the review queue's READ side and reviewer is its WRITE side (M6-04).
	// They are two fields rather than one because they are two packages: reading the
	// immutable record is internal/domain/ledger, and the one INSERT that records a
	// decision is internal/domain/review, which keeps "no store call in ledger is
	// not a SELECT" a fact grep can check.
	queue    panelQueue
	reviewer panelReviewer

	// staff is the employees section's WRITE side and invites mints activation
	// links (M6-05 phase B). Two more fields for the same reason queue and reviewer
	// are two: they are two PACKAGES (internal/domain/tenant writes employees,
	// internal/invite owns the code), and one wide field would let a change to one
	// break the other's wiring silently.
	staff   panelStaff
	invites panelInviter

	// venues is the locations section's venue and department side: its reads and its
	// writes (M6-06 phase A).
	//
	// ⚠️ IT IS A THIRD FIELD ON THE SAME PACKAGE AS staff, WHICH BENDS THE RULE
	// M6-04 SET ("a different package gets a different field") AND IS THEREFORE
	// STATED RATHER THAN SMUGGLED. That rule exists so "no store call in ledger is
	// not a SELECT" stays a fact grep can check — it separates READS from WRITES,
	// and both of these are internal/domain/tenant writes. What decides it here is
	// blast radius: one wide interface over both sides would let a change to
	// the venue side break the employee side's wiring with no compile error at the
	// call site. locationactions.go's panelVenues says the same from the consumer's
	// end.
	venues panelVenues

	// plaques is the wall-plaque half of the same section (M6-06 phase B): the
	// list, the mount and the replace.
	//
	// ⚠️ A FOURTH FIELD ON internal/domain/tenant, AND IT IS STATED FOR THE REASON
	// venues states the third. One wide interface carrying thirteen methods would
	// let a change to the plaque side break the employee or venue wiring with no
	// compile error at the call site. plaqueactions.go's panelPlaques says the same
	// from the consumer's end.
	plaques panelPlaques

	// entries is the manual record writer (M6-08, internal/domain/manual). It is a
	// FIFTH field rather than a method on one of the four above because it is a fifth
	// PACKAGE, and this one more than any of them: it is the second writer of
	// `transactions` in the product, and keeping it behind its own narrow interface is
	// what stops a change to the roster or the venue side reaching the one table
	// nobody can clean up.
	entries panelRecorder

	// rules is the policies section's READ side (M6-09 phase A,
	// internal/domain/tenant). It is its own field rather than a method on `venues`
	// or `staff` -- which are the same package -- for the blast-radius reason those
	// two already give, and for one more that is specific to this one: the OTHER
	// assembler of a policy set (internal/domain/checkin) WRITES, and keeping the
	// panel's reader behind a one-method interface is what makes "the policies
	// screen cannot materialise anything" a fact the compiler helps with rather
	// than a promise. See policies.go's panelRules.
	rules panelRules

	// scribe is the policies section's WRITE side (M6-09 phase B). It is a SECOND
	// field beside `rules` rather than a widening of it, and policies.go's panelRules
	// predicted exactly this: reading the rulebook and rewriting it are different
	// authorities and should not travel behind one value. The narrow read interface is
	// what keeps "a page view cannot write a policy row" a fact the compiler helps
	// with; folding the two together would give every reader a Save method.
	scribe panelScribe

	// books is the billing register (M6-12 phase B, internal/domain/billing). It is a
	// field of its own because it is a seventh PACKAGE, and this one carries the
	// heaviest write in the product: internal/domain/manual appends to a table §4.3
	// gives a correction shape for, while billing_periods has none — 0016 revokes
	// UPDATE and DELETE and refuses a second row per month, so a wrong figure is
	// permanent and uncorrectable.
	//
	// ⚠️ UNLIKE rules/scribe IT IS ONE FIELD OVER BOTH THE READS AND THE WRITE, AND
	// THAT IS STATED RATHER THAN SMUGGLED. The policies section split its two because
	// the split bought a compiler-checkable fact: the OTHER assembler of a policy set
	// materialises rows, so a narrow read interface made "a page view cannot write a
	// policy row" structural. There is no such second assembler here — one small
	// register, four methods, one table — so a split would put two fields on this
	// struct pointing at the same *billing.Book. billing.go's panelBooks says the same
	// from the consumer's end, and the property that matters (the read path freezes
	// nothing) is measured by a static call-graph test rather than implied by a type.
	books panelBooks

	// See adminratelimit.go for why there are three and what each may refuse.
	floodLimiter   *limiter
	attemptLimiter *limiter
	accountLimiter *limiter
	sessionLimiter *limiter
	logoutLimiter  *limiter

	log *slog.Logger
}

// NewAdminAuth wires the flow. Every dependency is required: a nil recorder would
// silently drop the section 4.6 trail and a nil manager cannot fail safely.
func NewAdminAuth(admins adminAuthenticator, rec auditRecorder, records panelLedger, queue panelQueue, reviewer panelReviewer, staff panelStaff, invites panelInviter, venues panelVenues, plaques panelPlaques, entries panelRecorder, rules panelRules, scribe panelScribe, books panelBooks, cfg *config.Config, log *slog.Logger) (*AdminAuth, error) {
	switch {
	case admins == nil:
		return nil, errors.New("handler: nil admin authenticator")
	case rec == nil:
		return nil, errors.New("handler: nil audit recorder")
	// A NIL LEDGER IS REFUSED RATHER THAN TOLERATED, and the reason is section
	// 4.6 rather than tidiness: a panel that cannot read would have to render
	// SOMETHING for the transactions section, and the only thing it could render
	// is a page with no records on it — which is indistinguishable from a quiet
	// day. The wiring bug has to be impossible to construct, not merely unlikely.
	case records == nil:
		return nil, errors.New("handler: nil transaction ledger")
	// THE SAME ARGUMENT, TWICE MORE (M6-04). A nil queue would render "nothing is
	// waiting for review" — a claim — and a nil reviewer would put an Approve
	// button on a screen where pressing it panics. The M5-04 lesson is that a
	// capability can be delivered, tested and DEAD in the wired product because two
	// halves were never assembled; a constructor that refuses is the only check
	// that runs before a customer finds out.
	case queue == nil:
		return nil, errors.New("handler: nil review queue")
	case reviewer == nil:
		return nil, errors.New("handler: nil reviewer")
	// THE SAME ARGUMENT, TWICE MORE (M6-05 phase B). A nil staff would put Deactivate
	// and Move buttons on a screen where pressing them panics, and a nil inviter would
	// offer an invitation the server cannot mint. Both are the M5-04 shape — a
	// capability delivered, tested and DEAD in the wired product because two halves
	// were never assembled — and a constructor that refuses is the only check that
	// runs before a customer finds out.
	case staff == nil:
		return nil, errors.New("handler: nil employee staff")
	case invites == nil:
		return nil, errors.New("handler: nil inviter")
	// THE SAME ARGUMENT, ONCE MORE (M6-06 phase A). A nil venues would put Add and
	// Save buttons on a screen where pressing them panics, and would leave the
	// locations section unable to say anything true about where a business works.
	// The M5-04 lesson is that a capability can be delivered, tested and DEAD in the
	// wired product because two halves were never assembled; a constructor that
	// refuses is the only check that runs before a customer finds out.
	case venues == nil:
		return nil, errors.New("handler: nil venues")
	// THE SAME ARGUMENT, ONCE MORE (M6-06 phase B). A nil plaques would put Mount
	// and Replace buttons on a screen where pressing them panics, and would leave
	// the section unable to say anything true about which plaque is on which wall —
	// which is the one thing every tap is authenticated by. The M5-04 lesson is that
	// a capability can be delivered, tested and DEAD in the wired product because
	// two halves were never assembled.
	case plaques == nil:
		return nil, errors.New("handler: nil plaques")
	// THE SAME ARGUMENT, ONCE MORE (M6-08) — and here the dead-capability cost is the
	// highest it has been. A nil entries would put "Enter a record by hand" on the
	// roster and a Record it button under a warning screen, and pressing either would
	// panic; the manager whose employee has no phone, and the one closing the
	// forgotten checkout Q18 says the system will never write for itself, would both
	// find the panel's answer to their problem is a crash. The M5-04 lesson is that a
	// capability can be delivered, tested and DEAD in the wired product because two
	// halves were never assembled.
	case entries == nil:
		return nil, errors.New("handler: nil manual recorder")
	// THE SAME ARGUMENT, ONCE MORE (M6-09 phase A), and here the dead capability
	// would be the rulebook itself. A nil rules would put a Policies tab in the
	// navigation whose page panics -- on the ONE screen whose job is to tell a
	// customer which rules their attendance is judged by. The M5-04 lesson is that a
	// capability can be delivered, tested and DEAD in the wired product because two
	// halves were never assembled.
	case rules == nil:
		return nil, errors.New("handler: nil policy rulebook")
	// THE SAME ARGUMENT, ONCE MORE (M6-09 phase B), and this one refuses the gate as
	// well as the buttons. A nil scribe would put an on/off switch and a Save on the
	// policies screen whose handler panics -- and, worse, it would remove the ONLY
	// place `policy:edit` is enforced, because the gate lives inside the writer
	// (internal/domain/tenant.RuleWriter.authorise) rather than in this package.
	case scribe == nil:
		return nil, errors.New("handler: nil policy scribe")
	// THE SAME ARGUMENT, ONCE MORE (M6-12 phase B), and here the dead capability
	// would be the invoice. A nil books would put a Billing tab in the navigation
	// whose page panics, on the ONE screen that says what this product costs — and it
	// would put a "Freeze this month" button on it whose handler crashes. The M5-04
	// lesson is that a capability can be delivered, tested and DEAD in the wired
	// product because two halves were never assembled.
	case books == nil:
		return nil, errors.New("handler: nil billing register")
	case cfg == nil:
		return nil, errors.New("handler: nil config")
	}
	choices, err := newAdminChoices(cfg)
	if err != nil {
		return nil, err
	}
	confirm, err := newAdminConfirm(cfg)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	return &AdminAuth{
		admins:         admins,
		audit:          rec,
		ledger:         records,
		queue:          queue,
		reviewer:       reviewer,
		staff:          staff,
		invites:        invites,
		venues:         venues,
		plaques:        plaques,
		entries:        entries,
		rules:          rules,
		scribe:         scribe,
		books:          books,
		cookies:        adminauth.NewCookies(cfg),
		short:          newAdminCookies(cfg),
		choices:        choices,
		confirm:        confirm,
		baseURL:        originOf(cfg.BaseURL),
		floodLimiter:   newLimiter(adminFloodLimit, adminFloodPeriod),
		attemptLimiter: newLimiter(adminAttemptLimit, adminAttemptPeriod),
		accountLimiter: newLimiter(adminAccountLimit, adminAccountPeriod),
		sessionLimiter: newLimiter(adminSessionLimit, adminSessionPeriod),
		logoutLimiter:  newLimiter(adminLogoutLimit, adminLogoutPeriod),
		log:            log,
	}, nil
}

// adminLoginPath is where the panel's sign-in page is mounted.
//
// 🔴 IT BECAME A CONSTANT IN M7-01 BECAUSE A SECOND SURFACE STARTED LINKING TO IT.
// The string appeared four times in this file — two route registrations and two
// redirects — which was survivable while every user of it was in this file. The
// landing page's "Sign in" link is the fifth use and the first one in a different
// feature, and a marketing page pointing at a route somebody renamed is a dead
// button on the most public URL in the product. handler.Marketing reads this
// constant, so the link and the route are the same string at compile time rather
// than two strings a test has to notice have diverged.
const adminLoginPath = "/admin/login"

// Mount registers the routes on r.
//
// The protected group carries httpx.RequireAdmin, which is what M6-02 mounts the
// dashboard inside. Everything it guards costs one resolver read plus one UPDATE
// per request (adminauth.Manager.Verify), so it is applied to the group and never
// globally.
func (a *AdminAuth) Mount(r chi.Router) {
	r.Get(adminLoginPath, a.LoginPage)
	r.Post(adminLoginPath, a.Login)
	r.Get("/admin/login/choose", a.ChoosePage)
	r.Post("/admin/login/choose", a.Choose)

	// THE DASHBOARD GROUP. Protect() carries the whole chain, so the panel mounts
	// inside this group — or uses Protect() in its own — and inherits every stage.
	//
	// M6-02 FILLED IT. mountSections (dashboard.go) registers one GET per
	// pages.PanelSection, /admin among them — Transactions is the section sign-in
	// lands on. There is no bare placeholder route left.
	r.Group(func(r chi.Router) {
		r.Use(a.Protect())
		a.mountSections(r)
	})

	// 🔴 THE PANEL'S MUTATING ROUTES ARE MOUNTED OUTSIDE THAT GROUP, on purpose.
	// They need the Origin check ahead of the resolver, and middleware added inside
	// the group above can only ever run after it. See AdminAuth.ProtectWriting for
	// the measurement that made this a separate mount rather than one more r.Use.
	a.mountWriting(r)

	// 🔴 SIGN-OUT IS A SEPARATE GROUP, AND THE REASON IS AN INVARIANT: ENDING YOUR
	// OWN SESSION MUST NOT BE REFUSABLE BY A THIRD PARTY.
	//
	// Round 12 put the refusing address gate in front of both protected routes. An
	// audit measured what that bought: an operator's own 100 page loads spent half
	// the window, somebody sharing the address key (one NAT, one IPv6 /64) spent the
	// rest, and the victim's POST /admin/logout came back 429 with the session still
	// LIVE. Nothing else could have ended it: "sign out everywhere" has no route
	// mounted, disabling an admin is M6-05, and there is no server-side expiry.
	//
	// 🔴 ROUND 14 THEN OVERCORRECTED, AND AN AUDIT MEASURED THAT TOO. Exempting
	// sign-out from refusal entirely did not make it "unbudgeted", it made it
	// UNBOUNDED — a different threat model and a worse one:
	//
	//	10000 anonymous POST /admin/logout, ONE address, Origin set
	//	  -> 10000 resolver reads, codes {303: 10000}, nothing refused
	//	CONTROL, 10000 anonymous GET /admin
	//	  -> 3000 reads, {303: 3000, 429: 7000}
	//
	// One route with no ceiling at all, on the SAME connection pool the tap surface
	// depends on. The choice had been framed as a binary — refuse (lockout) or never
	// refuse (unbounded) — and there is a third option, which is what this group now
	// does: sign-out gets its OWN ceiling, ten times the panel's.
	//
	// WHAT THAT TRADES, EXACTLY, because it is a weaker invariant than round 14's
	// and must not be described as the same one:
	//	BEFORE  a third party could never refuse your sign-out; an anonymous caller
	//	        could make unlimited resolver reads on this route.
	//	NOW     a third party must spend adminLogoutLimit requests in one window to
	//	        refuse your sign-out — 30000, ten times what it takes to deny the
	//	        rest of the panel, and loud in the flood log — and the amplifier is
	//	        bounded at the same number.
	// A legitimate operator signs out a handful of times per window and cannot
	// approach it (measured: TestAdminLogout_CannotBeBlockedByAThirdParty burns BOTH
	// the panel ceiling and 3000 sign-outs, and the victim still signs out).
	//
	// THE CHAIN:
	//	logoutGate      sign-out's own wide ceiling — the ONLY thing that may refuse
	//	                here, and it is deliberately not the panel's bucket.
	//	meterOnly       still charges the PANEL bucket so this route cannot be used
	//	                as a free amplifier for the routes that share it.
	//	sameOriginGate  a FREE refusal, BEFORE the resolver, so a browser-driven
	//	                cross-site flood costs zero database work. Defence in depth,
	//	                not the bound: curl sets an Origin header trivially.
	//	requireAdmin    the gate itself.
	r.Group(func(r chi.Router) {
		r.Use(a.logoutGate)
		r.Use(a.meterOnly("admin_logout"))
		r.Use(a.sameOriginGate)
		r.Use(a.requireAdmin())
		r.Post("/admin/logout", a.Logout)
	})
}

// floodGate is the per-address ceiling as a middleware, so it can be mounted in
// front of the resolver rather than called from inside a handler.
//
// It is the same budget and the same counter the four unauthenticated auth routes
// charge (a.flooded); nothing new is introduced. M6-02 mounts the dashboard inside
// this group and inherits it.
func (a *AdminAuth) floodGate(where string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a.flooded(w, r, clientIP(r), where) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// meterOnly charges the address budget and NEVER refuses. It keeps a route from
// being a free amplifier for the budget the other routes rely on, without letting
// a third party turn that budget into a lockout.
func (a *AdminAuth) meterOnly(where string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if n := a.floodLimiter.Charge(clientIP(r)); a.floodLimiter.FirstOverLimit(n) {
				a.log.Warn("panel auth flood ceiling reached (metered, not refused)",
					"ip", clientIP(r), "at", where, "limit", adminFloodLimit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// logoutGate is sign-out's own address ceiling: very wide, and separate from the
// panel's so that burning one cannot deny the other. See the sign-out group for
// the invariant it trades and the measurement behind it.
func (a *AdminAuth) logoutGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if n := a.logoutLimiter.Charge(ip); n > adminLogoutLimit {
			if a.logoutLimiter.FirstOverLimit(n) {
				a.log.Warn("panel sign-out ceiling reached", "ip", ip,
					"limit", adminLogoutLimit, "period", adminLogoutPeriod.String())
			}
			a.renderProblem(w, r, http.StatusTooManyRequests, problemAdminTooMany)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOriginGate refuses a cross-origin request BEFORE the resolver runs, so the
// refusal is free. Defence in depth only — an attacker who is not a browser sets
// the header themselves.
//
// ⚠️ THE LOG LINE NAMES THE ROUTE, and it did not until M6-04 added a second user
// of this gate. It said "panel sign-out refused" whatever it had refused, so a
// cross-origin attack on the decision endpoint would have appeared in the log as a
// sign-out event and sent an incident response at the wrong route. The method and
// path are what distinguish them, so both are printed.
func (a *AdminAuth) sameOriginGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.sameOrigin(r) {
			a.log.Warn("panel request refused: not same-origin",
				"method", r.Method, "path", r.URL.Path, "ip", clientIP(r))
			a.redirect(w, "/admin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sessionGate is the THIRD stage: a per-session budget, applied AFTER the identity
// is known and keyed on the session's own UUID. It bounds what one session can do
// without letting anybody else spend that budget.
func (a *AdminAuth) sessionGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := httpx.AdminOf(r)
		if !id.Live() {
			next.ServeHTTP(w, r)
			return
		}
		key := id.Admin.SessionID.String()
		if n := a.sessionLimiter.Charge(key); n > adminSessionLimit {
			if a.sessionLimiter.FirstOverLimit(n) {
				a.log.Warn("panel session budget reached", "session_id", key,
					"limit", adminSessionLimit, "period", adminSessionPeriod.String())
			}
			a.renderProblem(w, r, http.StatusTooManyRequests, problemAdminTooMany)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin is the bare gate, without any budget. Only the sign-out group uses
// it directly; everything else goes through Protect.
func (a *AdminAuth) requireAdmin() func(http.Handler) http.Handler {
	return httpx.RequireAdmin(a.cookies, a.admins, http.HandlerFunc(a.requireLogin))
}

// Protect is the middleware M6-02 puts in front of every dashboard route. It is
// exported so the dashboard does not have to know how an admin is resolved, and so
// there is exactly ONE place that decides what an unauthenticated panel request
// gets.
// 🔴 IT CARRIES THE BUDGETS, NOT JUST THE GATE, and that is a correction. Round 12
// added the address shield inside Mount's own group while leaving this EXPORTED
// symbol as the bare gate — so an M6-02 that mounted the dashboard in its own
// group with a.Protect() would have reinstated the unbudgeted resolver read that
// round 12 existed to close. The guarantee lived in a comment saying "M6-02 mounts
// inside this group"; nothing enforced it, and the exported symbol was the one
// WITHOUT the protection. Composing here makes the safe thing the only thing a
// caller can reach.
func (a *AdminAuth) Protect() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Order is tap.go's, and it is load-bearing: shield -> identity -> session
		// budget. The shield must precede the resolver (it is what bounds the cost
		// of an anonymous caller) and the session budget must follow it (it needs
		// an identity to key on).
		return a.floodGate("admin_panel")(a.requireAdmin()(a.sessionGate(next)))
	}
}

// ProtectWriting is Protect with the Origin check inserted BEFORE the resolver. It
// is what a state-changing panel route mounts (today: POST /admin/review, M6-04).
//
// 🔴 THE POSITION OF sameOriginGate IS THE WHOLE DIFFERENCE, AND THE FIRST VERSION
// PUT IT IN THE WRONG PLACE. M6-04 shipped the gate INSIDE the protected group,
// i.e. after requireAdmin and after sessionGate, while its own comment said it was
// "the same defence POST /admin/logout uses". Measured, it was not — sign-out mounts
// the gate ahead of the resolver on purpose, and adminlogin.go states the reason in
// one line: "a FREE refusal, BEFORE the resolver, so a browser-driven cross-site
// flood costs zero database work".
//
// WHAT THE WRONG ORDER COST, MEASURED with a resolver counter on the real router:
//
//	cross-origin POST /admin/review  -> 303, resolver calls 1   <- the defect
//	cross-origin POST /admin/logout  -> 303, resolver calls 0
//	300 cross-origin POSTs           -> 429 at #301, and then the operator's OWN
//	                                    GET /admin answered 429 as well
//
// So a page on a DIFFERENT ORIGIN OF THE SAME SITE — a subdomain with an XSS, a
// subdomain takeover, the http twin of the https origin; SameSite=Lax stops a true
// cross-site page but not these — could spend a signed-in manager's whole session
// budget and lock them out of their own panel for ten minutes, unable to clear the
// approval queue, while charging us 301 unbudgeted session lookups.
//
// 🔴 IT IS ALSO THE SAME MISTAKE THIS FILE ALREADY RECORDS AT adminFloodLimit:
// copying a pattern without COUNTING ITS PARTS. tap.go's shape is
// ByAddress -> Identify -> BySession and round 12 copied only the first stage;
// M6-04 copied sign-out's gate without copying its POSITION.
func (a *AdminAuth) ProtectWriting() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return a.floodGate("admin_panel")(
			a.sameOriginGate(
				a.requireAdmin()(
					a.sessionGate(next))))
	}
}

// requireLogin is what an unauthenticated panel request lands on: a 303 to the
// login form.
//
// A DATABASE FAILURE GETS THE SAME REDIRECT but a different LOG LINE. Telling the
// visitor two stories would mean the panel announcing its own outage to anyone who
// asks; not telling OURSELVES would mean an outage looking like a wave of expired
// sessions.
func (a *AdminAuth) requireLogin(w http.ResponseWriter, r *http.Request) {
	if err := httpx.AdminOf(r).Err; err != nil {
		a.log.Error("panel: could not resolve the admin session", "err", err)
	}
	a.redirect(w, adminLoginPath)
}

// The failure screens. problemAdminRestart is ONE value used for several internal
// outcomes, exactly as problemBadLink is in the activation flow, and for the same
// reason: the distinction belongs in audit_log, not in a response body.
var (
	problemAdminNoCookie = pages.ProblemView{
		Title:   "Your browser didn't keep the sign-in",
		Message: "Tappa needs a cookie to sign you in, and this browser is not storing one.",
		Hint:    "Allow cookies for this site, or turn off private browsing, then try again.",
	}
	problemAdminRestart = pages.ProblemView{
		Title:   "That sign-in step expired",
		Message: "The choice of business is only valid for a few minutes.",
		Hint:    "Start again from the sign-in page.",
	}
	problemAdminTooMany = pages.ProblemView{
		Title:   "Too many attempts",
		Message: "We have stopped accepting sign-ins from here for a few minutes.",
		Hint:    "Wait a little and try again.",
	}
	problemAdminServer = pages.ProblemView{
		Title:   "Something went wrong on our side",
		Message: "You were not signed in and nothing was changed.",
		Hint:    "Try again in a minute.",
	}
	// 🔴 THIS SCREEN EXISTS SO THAT A FAILED READ CANNOT LOOK LIKE A QUIET DAY
	// (section 4.6, and the class M5-11 closed). When the panel cannot reach the
	// database the honest answer is "we could not look", never a page listing no
	// records — those are different facts and only one of them is measured.
	// The wording says explicitly that nothing is missing FROM THE RECORD, because
	// the first thing a manager fears on this screen is that a day was lost.
	problemPanelUnavailable = pages.ProblemView{
		Title:   "We could not read your records",
		Message: "The panel could not reach the database, so this page is not showing anything. Nothing has been changed and no record has been lost.",
		Hint:    "Try again in a minute.",
	}
	// problemPanelWriteFailed is the same outage told to somebody who was WRITING.
	//
	// 🔴 THE SENTENCE ABOVE IS WRONG ON A WRITE PATH, and an audit measured how wrong:
	// a manager who pressed a button is told "this page is not showing anything", which
	// describes a screen they were not reading and says nothing about the act they just
	// attempted. The two facts they need are whether it happened and whether to press
	// again.
	//
	// ✅ "NOTHING WAS WRITTEN" IS A MEASURED CLAIM, NOT A COMFORT. Every panel write
	// runs inside db.WithTenant, so a failed statement rolls the whole transaction back
	// — the record AND its audit row — which is asserted for this task's own path by
	// TestManualDB_TheRecordAndItsAuditRowSHAREATransaction (it breaks the trail side
	// and counts rows). A security lens confirmed the same for the shipped sentence's
	// last clause.
	//
	// ⚠️ IT IS USED ON internal/handler/manualentry.go's CALL SITES ONLY, AND THE REST
	// ARE COUNTED RATHER THAN CONVERTED — by a test, not by this comment. Converting
	// them is a mechanical edit across five files owned by M6-04/05/06 and belongs to
	// whoever owns those screens; the pattern is here, ready.
	//
	// 🔴 THE CENSUS IS DERIVED AND PRINTED BY
	// TestPanelProblemPages_CountTheWriteRoutesStillTellingReadersTheirPageIsEmpty,
	// which parses this package and classifies every call site against the routes
	// mountWriting registers. An integer written here would be a second representation
	// of a set the code owns — the exact shape that drifted three times in
	// mountWriting's own comment, and once already in the first version of this one
	// (it said 33; the census says 32, because the earlier count had scooped up a
	// mention inside a comment).
	problemPanelWriteFailed = pages.ProblemView{
		Title:   "We could not save that",
		Message: "The panel could not reach the database, so nothing was written: no record, no change and no trail entry. What you were looking at before is unchanged.",
		Hint:    "Try again in a minute — pressing again will not enter it twice.",
	}
)

// LoginPage serves GET /admin/login: mint the synchronizer token and render the
// form.
func (a *AdminAuth) LoginPage(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.flooded(w, r, ip, "admin_login_page") {
		return
	}
	// A FRESH pair on every render, rather than reusing an existing cookie. It
	// costs 64 bytes of randomness and it means a token left in a shared browser is
	// superseded the moment anyone loads the page again.
	st, err := newAdminLoginState()
	if err != nil {
		// The message says "csrf" rather than the word redline R7 matches
		// inside a log call: an innocent MESSAGE reddening CI is how a net gets
		// loosened under pressure (activate.go states the same reasoning).
		a.log.Error("panel: minting the sign-in csrf value failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	a.short.setLogin(w, st)
	// Any choice blob from an abandoned attempt is dead the moment a new login
	// starts: leaving it would let a stale verified set survive into a new attempt.
	a.short.clear(w, adminChoiceCookieName)
	a.render(w, r, http.StatusOK, pages.AdminLogin(pages.AdminLoginView{CSRFToken: st.csrf}))
}

// Login serves POST /admin/login.
//
// ORDER OF CHECKS, and why each is where it is:
//
//	flood ceiling     BEFORE anything; the only position a limiter can shed load.
//	Origin            strict. A cross-origin POST here would be login CSRF, whose
//	                  result is the VICTIM signed in as the ATTACKER.
//	login cookie      no cookie means no synchronizer token to compare.
//	form parse
//	synchronizer      the load-bearing CSRF check.
//	attempt budget    BEFORE the password work, because the password work is the
//	                  expensive thing it exists to bound (~380 ms per candidate).
//	Authenticate      the resolver, the bcrypt loop and the dummy, all inside
//	                  adminauth so no handler can skip one.
func (a *AdminAuth) Login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.flooded(w, r, ip, "admin_login") {
		return
	}
	st, ok := a.beginPost(w, r, ip, "admin_login")
	if !ok {
		return
	}

	// THE BUDGET IS CHECKED BEFORE THE FAILURE IS COUNTED, so exactly
	// adminAttemptLimit failures are answered normally and the next one is refused
	// — rather than the limit-th request being the one that trips.
	if !a.attemptLimiter.Allowed(ip) {
		// The refusal itself is charged (ratelimit.go's rule: a limiter whose own
		// refusals are free is a cheaper way to reach the thing it protects), and
		// the log line is written once per window.
		if n := a.attemptLimiter.Charge(ip); a.attemptLimiter.FirstOverLimit(n) {
			a.log.Warn("panel sign-in rate limited", "scope", "address", "ip", ip,
				"limit", adminAttemptLimit, "period", adminAttemptPeriod.String())
		}
		a.renderProblem(w, r, http.StatusTooManyRequests, problemAdminTooMany)
		return
	}

	// The email is normalised only by trimming surrounding whitespace. CASE IS NOT
	// touched here: admin_users.email is citext and migration 00011's resolver
	// compares with OPERATOR(public.=), so the DATABASE is the single authority on
	// what "the same address" means. Lower-casing in Go would be a SECOND
	// canonicalisation, which is the M5-01/02/03 failure class ("a check and its
	// consumer must see the same representation; the fix is to delete the second
	// representation, not to teach it twice").
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")

	auth, err := a.admins.Authenticate(r.Context(), email, password)
	if err != nil && !errors.Is(err, adminauth.ErrBadCredentials) {
		// A database or internal failure is NOT bad credentials: saying so would
		// tell every operator at once that their password stopped working, and would
		// hide an outage behind a UX message.
		a.log.Error("panel sign-in failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	if auth.Truncated() {
		// The one signal that keeps the lockout named at adminauth.MaxCandidates
		// from being invisible. No email in the line (section 4.7 spirit: an address
		// is personal data and this line is written on an unauthenticated path).
		a.log.Warn("panel sign-in: candidate list was truncated by the per-request cap",
			"ip", ip, "resolved", auth.Resolved, "compared", len(auth.Attempts))
	}
	if err != nil {
		a.failLogin(w, r, ip, st, auth)
		return
	}

	verified := auth.Verified()
	if len(verified) == 1 {
		// The ordinary path, and the reason the signed blob is off it entirely.
		a.completeLogin(w, r, ip, verified[0])
		return
	}

	blob, err := a.choices.mint(verified, st.bind)
	if err != nil {
		a.log.Error("panel sign-in: minting the business choice failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	a.short.set(w, adminChoiceCookieName, blob, adminChoiceCookieMaxAge)
	a.redirect(w, "/admin/login/choose")
}

// ChoosePage serves GET /admin/login/choose: the "which business?" screen.
//
// THE BUSINESS NAMES ARE READ HERE AND NOT EARLIER, which is migration 00011's
// requirement and not an implementation detail: resolve_admin_by_email
// deliberately does not touch `tenants`, because a business name reachable before
// authentication is itself an enumeration signal. GetAdminForTenantChoice runs
// inside each tenant's own context and only after a password has verified.
func (a *AdminAuth) ChoosePage(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.flooded(w, r, ip, "admin_choose_page") {
		return
	}
	st, verified, ok := a.readChoice(w, r)
	if !ok {
		return
	}
	choices, err := a.admins.TenantChoices(r.Context(), verified)
	if err != nil {
		a.log.Error("panel sign-in: reading the business list failed", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	if len(choices) == 0 {
		// Every verified identity was disabled between the password check and this
		// read. There is nothing to offer and nothing to explain to the visitor.
		//
		// 🔴 BUT IT MUST NOT BE SILENT, AND FOR ONE ROUND IT WAS. This branch used to
		// end with "the audit trail already carries the successful comparison", and a
		// closing audit measured that to be FALSE: the multi-candidate branch of
		// Login mints the blob and answers 303 without a.record or a.log,
		// internal/adminauth writes no audit rows at all, and every a.record call
		// site is on a different path. So a password that VERIFIED against two or
		// more real accounts left no trace anywhere if those accounts were disabled
		// between the two steps.
		//
		// That is the M5-11 class exactly — a sentence declaring a trail the system
		// does not produce, which tells the next reader not to look. The row is
		// written instead of the sentence being softened, because section 4.6 wants
		// a refused attempt visible and this one is genuinely interesting: somebody
		// held a correct password for accounts that have just been switched off.
		//
		// It is attributable: verified[0] is an identity whose digest verified, so
		// the tenant is one this request authenticated against — never a tenant the
		// caller merely named.
		if len(verified) > 0 {
			a.record(r.Context(), audit.Event{
				TenantID: verified[0].TenantID,
				ActorID:  &verified[0].AdminUserID,
				Action:   ActionAdminLoginFailed,
				Target:   verified[0].AdminUserID.String(),
				Detail: adminLoginDetail{
					Outcome:          "rejected",
					Reason:           "password verified but every business was disabled before the choice",
					VerifiedBusiness: len(verified),
				},
			})
		}
		a.log.Warn("panel sign-in: every verified business was disabled before the choice",
			"ip", clientIP(r), "verified", len(verified))
		a.resetLogin(w)
		a.renderProblem(w, r, http.StatusOK, problemAdminRestart)
		return
	}
	v := pages.AdminChooseView{CSRFToken: st.csrf}
	for _, c := range choices {
		v.Businesses = append(v.Businesses, pages.AdminBusiness{
			TenantID:   c.TenantID.String(),
			TenantName: c.TenantName,
			Role:       c.Role,
		})
	}
	a.render(w, r, http.StatusOK, pages.AdminChoose(v))
}

// Choose serves POST /admin/login/choose — the second half of PHASE B OBLIGATION
// 5.
//
// The posted tenant is checked against the AUTHENTICATED set (selectVerified), so
// a client that posts a tenant it did not authenticate for is refused. That check
// is the whole reason this endpoint exists as a separate step rather than trusting
// the picker's markup.
func (a *AdminAuth) Choose(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.flooded(w, r, ip, "admin_choose") {
		return
	}
	if !a.sameOrigin(r) {
		a.log.Warn("panel business choice refused: not same-origin", "ip", ip)
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminRestart)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminServer)
		return
	}
	st, verified, ok := a.readChoice(w, r)
	if !ok {
		return
	}
	if !st.csrfMatches(r.PostFormValue("csrf")) {
		a.log.Warn("panel business choice refused: csrf mismatch", "ip", ip)
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminRestart)
		return
	}

	tenantID, err := uuid.Parse(r.PostFormValue("tenant_id"))
	if err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminRestart)
		return
	}
	chosen, ok := selectVerified(verified, tenantID)
	if !ok {
		// 🔴 THE CROSS-TENANT AUTHENTICATION BYPASS, REFUSED. Somebody posted a
		// tenant that is not in the set their password verified against. It is
		// loud: an error-level log AND an audit row.
		//
		// THE ROW IS WRITTEN INTO A TENANT WE DID AUTHENTICATE FOR, never into the
		// posted one. Writing into the posted tenant would BE the cross-tenant
		// write this endpoint exists to refuse — it would hand an unauthenticated
		// caller a way to append rows to any tenant's undeletable audit_log.
		a.log.Error("panel business choice refused: tenant not in the verified set",
			"ip", ip, "attempted_tenant_id", tenantID)
		if len(verified) > 0 {
			a.record(r.Context(), audit.Event{
				TenantID: verified[0].TenantID,
				ActorID:  &verified[0].AdminUserID,
				Action:   ActionAdminLoginRefused,
				Target:   verified[0].AdminUserID.String(),
				Detail: adminLoginDetail{
					Outcome:          "refused",
					Reason:           "chosen business was not in the verified set",
					AttemptedTenant:  tenantID.String(),
					VerifiedBusiness: len(verified),
				},
			})
		}
		a.resetLogin(w)
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminRestart)
		return
	}
	a.completeLogin(w, r, ip, chosen)
}

// completeLogin issues the session, writes the cookie, records the success and
// sends the operator to the panel.
//
// THE SHORT-LIVED COOKIES ARE CLEARED HERE, on the path that completes the login.
// A choice blob is a bearer credential for the accounts inside it
// (logincontext.go), and leaving one in a browser is a habit worth not forming.
//
// ⚠️ CLEARING IS NOT INVALIDATING, and the earlier wording ("the one path that
// SPENDS them") implied it was. The blob carries no server-side use count: a caller
// that kept a copy can present it again until the five-minute TTL expires, and
// TestAdminChoose_BlobIsNotSingleUse measures exactly that (session count 1 -> 2).
// What does not change is the SET — every replay is still confined to businesses
// whose password verified in the same attempt, which is the obligation.
func (a *AdminAuth) completeLogin(w http.ResponseWriter, r *http.Request, ip string, v adminauth.Verified) {
	issued, err := a.admins.Issue(r.Context(), v)
	if err != nil {
		// The password was right and the account is active, but no session exists.
		// This must be visible: it is not "wrong credentials" and telling the
		// operator so would send them looking for a password problem they do not
		// have.
		a.log.Error("panel sign-in: issuing the session failed",
			"admin_user_id", v.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	if err := a.cookies.Set(w, issued.Token); err != nil {
		// Unreachable in practice (Set fails only on an empty token, and Issue has
		// already refused to return one), and recorded anyway: it leaves the same
		// state as the branch above — a live session row nobody can use — so it must
		// leave the same trail (section 4.6). An unreachable branch still teaches
		// the next reader what the rule is.
		a.log.Error("panel sign-in: writing the session cookie failed",
			"admin_user_id", v.AdminUserID, "err", err)
		a.record(r.Context(), audit.Event{
			TenantID: v.TenantID,
			ActorID:  &v.AdminUserID,
			Action:   ActionAdminLoginFailed,
			Target:   v.AdminUserID.String(),
			Detail: adminLoginDetail{
				Outcome: "session_cookie_failed",
				Reason:  "the session was created but its cookie could not be written",
			},
		})
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	a.resetLogin(w)
	a.record(r.Context(), audit.Event{
		TenantID: v.TenantID,
		ActorID:  &v.AdminUserID,
		Action:   ActionAdminLoginSucceeded,
		Target:   v.AdminUserID.String(),
		Detail: adminLoginDetail{
			Outcome:   "ok",
			SessionID: issued.Session.ID.String(),
		},
	})
	a.log.Info("panel sign-in", "admin_user_id", v.AdminUserID, "ip", ip)
	a.redirect(w, "/admin")
}

// failLogin is the SINGLE exit for every credential failure.
//
// 🔴 PHASE B OBLIGATION 1 LIVES IN THIS FUNCTION. "Unknown email", "wrong
// password" and "disabled admin" reach it through one call site with one argument
// they cannot influence, and it renders ONE view with ONE status. The three are
// distinguishable in adminauth.Authentication.Attempts — which is written to
// audit_log, where the distinction belongs — and nowhere in the response.
//
// The BODY is identical byte for byte apart from the synchronizer token, which is
// per-cookie rather than per-outcome; TestAdminLogin_ThreeFailuresAreByteIdentical
// pins both readings (same cookie -> literally identical bytes; different cookies
// -> identical after normalising the one named field).
func (a *AdminAuth) failLogin(w http.ResponseWriter, r *http.Request, ip string, st adminLoginState, auth adminauth.Authentication) {
	a.attemptLimiter.Charge(ip)

	// ⚠️ THE AUDIT TRAIL IS ATTRIBUTABLE ONLY WHEN THE EMAIL RESOLVED, AND THAT GAP
	// IS REAL. The MECHANISM is directly below: with no candidates the loop does not
	// execute, so nothing is written. The SCHEMA is why the gap is not closed rather
	// than why it exists — audit_log.tenant_id is NOT NULL with an FK to tenants
	// (migration 00005), so there is no tenant to attribute an unknown address to,
	// and db/queries/audit.sql explains at length why no "system tenant" is invented
	// to paper over it. Those attempts exist in the process log and in the address
	// budget, and nowhere else.
	//
	// (An earlier wording made the NOT NULL constraint the cause of the zero. An
	// audit separated the two: both facts are true, the causality was shifted.)
	//
	// So the M6-01 card's criterion "failed sign-ins are rate limited and written to
	// audit_log" is met for attempts against a KNOWN address and is NOT met for
	// attempts against an unknown one. That is stated here rather than left for a
	// reader to discover from an empty table.
	for _, at := range auth.Attempts {
		key := at.AdminUserID.String()
		if !a.accountLimiter.Allowed(key) {
			if n := a.accountLimiter.Charge(key); a.accountLimiter.FirstOverLimit(n) {
				// ONE row saying the trail is being throttled, then silence. The
				// M5-02 lesson: an append-only table nobody can delete from must
				// never be reachable without a budget.
				a.record(r.Context(), audit.Event{
					TenantID: at.TenantID,
					ActorID:  &at.AdminUserID,
					Action:   ActionAdminLoginLimited,
					Target:   key,
					Detail: adminLoginDetail{
						Outcome: "rate_limited",
						Reason:  "too many failed sign-ins against this account",
						// The ordinal of the FIRST suppressed attempt. It is the
						// only count the database can be given at this moment —
						// see the limit at adminAccountLimit.
						SuppressedFrom: n,
					},
				})
				a.log.Warn("panel sign-in rate limited", "scope", "account",
					"admin_user_id", at.AdminUserID, "ip", ip)
			}
			continue
		}
		a.accountLimiter.Charge(key)
		reason := "wrong password"
		if at.PasswordMatched && !at.Active {
			// A genuinely different security event: the CORRECT password on a
			// disabled account. The visitor is told nothing extra; the owner's trail
			// says exactly what happened.
			reason = "correct password on a disabled account"
		}
		a.record(r.Context(), audit.Event{
			TenantID: at.TenantID,
			ActorID:  &at.AdminUserID,
			Action:   ActionAdminLoginFailed,
			Target:   key,
			Detail: adminLoginDetail{
				Outcome:          "rejected",
				Reason:           reason,
				VerifiedBusiness: 0,
			},
		})
	}
	if len(auth.Attempts) == 0 {
		// Unattributable. Logged WITHOUT the address that was tried (section 4.7
		// spirit: an email is personal data and this path is unauthenticated, so a
		// flood of guesses would otherwise write a list of guessed addresses into
		// the process log).
		a.log.Info("panel sign-in refused", "reason", "no such admin", "ip", ip)
	}
	a.renderLoginFailure(w, r, st)
}

// renderLoginFailure writes THE one failure response. It is a separate function
// with no parameters carrying an outcome, so there is no argument through which a
// caller could vary what a failure looks like.
func (a *AdminAuth) renderLoginFailure(w http.ResponseWriter, r *http.Request, st adminLoginState) {
	a.render(w, r, http.StatusUnauthorized, pages.AdminLogin(pages.AdminLoginView{
		CSRFToken: st.csrf,
		Failed:    true,
	}))
}

// Logout serves POST /admin/logout.
//
// IT IS BEHIND THE GATE, so the session to revoke is the one the request proved it
// holds — there is no id to post and therefore nothing to forge.
//
// IT IS A POST AND IT CHECKS ORIGIN. A GET would let any page sign an operator out
// with an <img> tag; that is an annoyance rather than a breach, but it is free to
// refuse. There is no synchronizer token here because the login cookie that
// carries one is cleared at sign-in; the Origin check is the whole defence and is
// stated as such rather than implied.
func (a *AdminAuth) Logout(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.sameOrigin(r) {
		a.log.Warn("panel sign-out refused: not same-origin", "ip", clientIP(r))
		a.redirect(w, "/admin")
		return
	}
	if err := a.admins.Revoke(r.Context(), id.Admin.TenantID, id.Admin.SessionID); err != nil {
		// Clearing the cookie without revoking would leave a live session behind a
		// browser that thinks it signed out — the worst of both. Say so and keep the
		// operator on a page that still works.
		a.log.Error("panel sign-out: revoking the session failed",
			"admin_user_id", id.Admin.AdminUserID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAdminServer)
		return
	}
	a.cookies.Clear(w)
	a.record(r.Context(), audit.Event{
		TenantID: id.Admin.TenantID,
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionAdminLoggedOut,
		Target:   id.Admin.AdminUserID.String(),
		Detail:   adminLoginDetail{Outcome: "ok", SessionID: id.Admin.SessionID.String()},
	})
	a.redirect(w, adminLoginPath)
}

// adminLoginDetail is the audit payload for this flow.
//
// IT IS A PURPOSE-BUILT STRUCT, NOT A MAP (internal/audit's package doc explains
// why): there is no field here through which an email, a password or a digest
// could travel, so no later edit adds one by accident and the section 4.7 promise
// is a property of the type rather than of the author's memory. Everything in it
// is an opaque id or a fixed string written in this file.
type adminLoginDetail struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
	// SessionID is the panel session this event concerns. It is a row id, never a
	// token or a hash — neither of those has a field here.
	SessionID string `json:"session_id,omitempty"`
	// AttemptedTenant is the tenant a caller asked for and was refused. Present
	// only on ActionAdminLoginRefused, where it is the whole point of the row.
	AttemptedTenant string `json:"attempted_tenant,omitempty"`
	// SuppressedFrom is the attempt ordinal at which this account's audit budget
	// tripped, i.e. the first failure that was NOT written. It is present only on
	// ActionAdminLoginLimited and it is a START, not a total — see the limit
	// documented at adminAccountLimit.
	SuppressedFrom int `json:"suppressed_from,omitempty"`
	// VerifiedBusiness is how many businesses the password verified against.
	VerifiedBusiness int `json:"verified_businesses,omitempty"`
}

// beginPost runs the checks every state-changing panel auth POST shares: Origin,
// the login cookie, the form, and the synchronizer token.
//
// It returns ok=false having ALREADY answered the request.
func (a *AdminAuth) beginPost(w http.ResponseWriter, r *http.Request, ip, where string) (adminLoginState, bool) {
	if !a.sameOrigin(r) {
		a.log.Warn("panel auth refused: not same-origin", "at", where, "ip", ip)
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminRestart)
		return adminLoginState{}, false
	}
	st, ok := a.short.readLogin(r)
	if !ok {
		// No cookie: either the form was never loaded in this browser, or cookies
		// are blocked. The screen says which to fix. This is NOT the credential
		// failure and does not have to look like one — it reveals nothing about any
		// account, because the branch is decided entirely by the caller's own
		// request.
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminNoCookie)
		return adminLoginState{}, false
	}
	if err := r.ParseForm(); err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminServer)
		return adminLoginState{}, false
	}
	if !st.csrfMatches(r.PostFormValue("csrf")) {
		// An attack signal, not a user slip. It is NOT charged to the attempt
		// budget: that budget's meaning is "logins that made the server run bcrypt",
		// and this one is refused before any password work happens. The flood
		// ceiling has already been charged.
		a.log.Warn("panel auth refused: csrf mismatch", "at", where, "ip", ip)
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminRestart)
		return adminLoginState{}, false
	}
	return st, true
}

// readChoice reads and verifies the signed candidate set for both halves of the
// picker. It returns ok=false having already answered.
func (a *AdminAuth) readChoice(w http.ResponseWriter, r *http.Request) (adminLoginState, []adminauth.Verified, bool) {
	st, ok := a.short.readLogin(r)
	if !ok {
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminNoCookie)
		return adminLoginState{}, nil, false
	}
	ck, err := r.Cookie(adminChoiceCookieName)
	if err != nil || ck.Value == "" {
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminRestart)
		return adminLoginState{}, nil, false
	}
	verified, err := a.choices.parse(ck.Value, st.bind)
	if err != nil {
		// A stale blob and a tampered one get the SAME screen; the difference is
		// only in what we log. A signature failure is worth an error line because it
		// means somebody presented a value this deployment did not mint (or minted
		// for a different browser); an expiry is ordinary.
		if errors.Is(err, errChoiceSignature) || errors.Is(err, errChoiceMalformed) {
			a.log.Error("panel business choice rejected", "ip", clientIP(r), "err", err)
		}
		a.resetLogin(w)
		a.renderProblem(w, r, http.StatusBadRequest, problemAdminRestart)
		return adminLoginState{}, nil, false
	}
	return st, verified, true
}

// resetLogin expires both short-lived cookies. Called when they are spent and when
// they are refused, so a dead credential never lingers in a browser.
func (a *AdminAuth) resetLogin(w http.ResponseWriter) {
	a.short.clear(w, adminChoiceCookieName)
	a.short.clear(w, adminLoginCookieName)
}

// flooded is the entry gate on every panel auth handler. It reports true (and has
// already answered) when this address is over the ceiling.
//
// IT WRITES NO AUDIT ROW, a stated limit rather than an oversight: the check runs
// before any credential is resolved, so no tenant is known, and resolving one
// first would defeat the only purpose of a pre-database shield. The trail for a
// flooded caller is the process log, written once per window so the shield cannot
// be turned into an unbounded disk writer.
func (a *AdminAuth) flooded(w http.ResponseWriter, r *http.Request, ip, where string) bool {
	n := a.floodLimiter.Charge(ip)
	if n <= adminFloodLimit {
		return false
	}
	if a.floodLimiter.FirstOverLimit(n) {
		a.log.Warn("panel auth flood ceiling reached", "ip", ip, "at", where,
			"limit", adminFloodLimit, "period", adminFloodPeriod.String())
	}
	a.renderProblem(w, r, http.StatusTooManyRequests, problemAdminTooMany)
	return true
}

// sameOrigin checks the Origin header against this deployment's own, STRICTLY: an
// absent Origin falls back to the fetch metadata and is refused if that is missing
// too.
//
// STRICT ON EVERY PANEL POST, unlike the activation flow's Submit, which allows an
// absent header and leans on its synchronizer token. Two reasons the trade is
// different here: the audience is a desktop browser signing into an administrative
// panel rather than an arbitrary phone at a plaque, and every browser that can run
// the panel sends Origin on a cross-origin POST. What refusing buys is the ONE
// attack the synchronizer token cannot stop — an attacker who completes step 1 in
// their own browser holds a matching (cookie, token) pair and can plant both, so
// the token matches and only the Origin does not.
//
// IT IS A SEPARATE FUNCTION FROM Activation.sameOrigin, DELIBERATELY. The two are
// near-identical and merging them would be the tidier diff, but the activation
// version carries a `strict` flag whose two settings were argued over four audit
// rounds, and re-shaping it as a side effect of adding a panel is how a settled
// flow acquires an unreviewed change. The duplication is named so it is a decision
// rather than an accident.
//
// A malformed configured BaseURL makes this check pass; config.Load does not
// validate the URL's shape and this is not the place to start (the same note
// session.NewCookies carries).
func (a *AdminAuth) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
		case "same-origin", "same-site":
			return true
		default:
			return false
		}
	}
	return strings.EqualFold(strings.TrimRight(origin, "/"), strings.TrimRight(a.baseURL, "/"))
}

// record writes an audit event and never lets its failure change the outcome of
// the request — but never swallows it either (section 7): a trail that cannot be
// written is an ERROR, because it means section 4.6 is not being kept.
func (a *AdminAuth) record(ctx context.Context, e audit.Event) {
	if _, err := a.audit.Record(ctx, e); err != nil {
		a.log.Error("audit write failed", "action", e.Action, "err", err)
	}
}

// ptr is the one-line helper for audit.Event.ActorID, which is a *uuid.UUID
// because migration 00005 makes the column polymorphic and nullable.
func ptr(id uuid.UUID) *uuid.UUID { return &id }

// render writes one component with the headers every panel auth screen needs.
//
// Cache-Control: no-store on all of them, failure pages included. These responses
// carry an operator's name and a synchronizer token; a back button on a shared
// machine must not resurrect either, and no intermediary should keep a copy.
//
// THE CSP IS SET HERE, which the activation family does NOT do (tap.go sets it,
// the nine activation screens do not — a known asymmetry M5-02 left behind). These
// screens carry PASSWORD FORMS, so the new surface starts with the header rather
// than inheriting the gap. form-action 'self' is the one directive with a concrete
// threat behind it here: a browser that honours it will not let a password form be
// repointed at another host.
func (a *AdminAuth) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	a.renderWithPolicy(w, r, status, c, adminCSP)
}

// renderScripted is render for the ONE panel page that loads a script.
//
// 🔴 IT IS A SEPARATE ENTRY POINT SO THAT THE WIDENING IS PER-PAGE RATHER THAN
// PER-PANEL. M6-03 needed a script on the transactions section; giving the whole
// panel script-src would have let every screen that loads nothing permit one.
// Both policies are DERIVED FROM ONE BASE STRING below, so this is a parameter on
// a single representation and not the "two policies to keep in step" shape that
// this file argues against elsewhere.
func (a *AdminAuth) renderScripted(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	a.renderWithPolicy(w, r, status, c, adminScriptedCSP)
}

// ⚠️ LIMIT — NO Referrer-Policy HEADER (M6-03, informational). Measured on the
// wire across six panel URLs: none of them sends one. It has NO EFFECT TODAY,
// because the policy above is default-src 'none' with 'self' everywhere else, so
// these pages cannot generate a cross-origin request for a Referer to ride on --
// and layout.documentHead already sends <meta name="referrer" content="no-referrer">,
// which covers navigations.
//
// IT IS WRITTEN DOWN BECAUSE THE URL CHANGED SHAPE — THREE TIMES, AND THE THIRD
// CHANGE UNDID THE SECOND.
//
//	2026-08-06  the transactions URL began carrying a TYPED PERSON'S NAME in
//	            ?employee= — the reader's own query, typed by them about their own
//	            tenant.
//	2026-08-07  the employees section's "Next page" link carried
//	            ?after_name=<a real employee's full name>. A different thing: not
//	            something the reader typed, but a row the SERVER read out of the
//	            database and printed into an address — so it reached browser history
//	            and any shared screen without anybody choosing to put it there.
//	2026-08-07  ✅ that half is GONE. The roster's paging anchor is an id now
//	            (?after_id=<uuid>) and the server looks the name up. The change was
//	            made for a §4.6 defect — an unbounded name in a query string had to
//	            be length-bounded, and a dropped cursor repeated a page — and this
//	            disclosure closed with it.
//	            TestEmployeesSection_NoEmployeeNameTravelsInAPagingLink holds it.
//
// WHAT IS LEFT is the 2026-08-06 line: a name the reader typed themselves, about
// their own tenant, in their own address bar. That is the shape this note was
// originally comfortable with. No §4.7 breach and nothing leaves the origin — the
// policy above is default-src 'none' with 'self' everywhere else, so these pages
// cannot generate a cross-origin request for a Referer to ride on, and
// layout.documentHead already sends <meta name="referrer" content="no-referrer">.
// The header remains hardening worth doing the first time any panel page links or
// embeds anything external.
func (a *AdminAuth) renderWithPolicy(w http.ResponseWriter, r *http.Request, status int, c templ.Component, policy string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", policy)
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		// The status line is already on the wire, so there is nothing to send but a
		// log line. Never swallowed (section 7).
		a.log.Error("rendering the panel page failed", "err", err)
	}
}

func (a *AdminAuth) renderProblem(w http.ResponseWriter, r *http.Request, status int, v pages.ProblemView) {
	a.render(w, r, status, pages.Problem(v))
}

// redirect answers 303 See Other: the browser follows with GET, which turns a POST
// into a plain page load and makes a refresh harmless.
func (a *AdminAuth) redirect(w http.ResponseWriter, to string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", to)
	w.WriteHeader(http.StatusSeeOther)
}

// adminCSP is the content policy for every panel screen that loads NO script,
// which is all of them except the transactions section.
//
// It is tap.go's policy WITHOUT script-src: these screens load no script, so
// naming one would widen the policy for nothing. default-src 'none' already
// forbids scripts, and dropping the directive means adding a script is a visible
// edit here rather than a silent inheritance.
//
//	default-src 'none'      nothing loads unless named below
//	style-src / font-src    our own origin only; there is no CDN to allow
//	form-action 'self'      a password form cannot be repointed at another host
//	base-uri 'none'         a <base> tag cannot re-root the relative asset paths
//	frame-ancestors 'none'  the panel must not be framed under someone else's page
const adminCSP = "default-src 'none'; style-src 'self'; font-src 'self'; " +
	"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// adminScriptedCSP is adminCSP plus EXACTLY what HTMX needs, and nothing else.
//
// 🔴 THE WIDENING M6-03 OWED, WRITTEN DOWN WITH ITS SIZE. M6-02 shipped the panel
// with no script and recorded that adding one had to be a deliberate, visible edit
// rather than an inherited default. This is that edit.
//
// ⚠️ THIS PARAGRAPH NAMED A TEST THAT DOES NOT EXIST, and it is worth saying so
// here because this comment is the one thing somebody reads before widening the
// policy again. It said "TestPanelScreens_LoadNoScriptAndReachNoThirdParty turns
// red on the first <script> tag". That test was RENAMED BY THIS VERY TASK (its
// header had gone stale in the same way), and the round that renamed it fixed the
// duplicate sentence in dashboard_test.go while leaving THIS one -- a fix that left
// its own subject stale one file away.
//
// WHAT ACTUALLY GUARDS THIS, and what it does NOT guard:
//
//	TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty (dashboard_test.go)
//	  - a page names script-src IF AND ONLY IF it loads a script, and connect-src
//	    comes with it;
//	  - EXACTLY ONE panel URL sends the widened policy, and it is the transactions
//	    section.
//
// It does NOT "turn red on the first <script> tag" -- a new scripted page whose
// policy widens correctly is ACCEPTED by the correspondence and then caught by the
// cardinality. That is the design: adding a second scripted screen is allowed and
// must be argued for in that test, rather than being impossible.
//
// IT GROWS BY TWO DIRECTIVES. adminCSP names six directives; this names eight
// (counted, not eyeballed -- the earlier "five ... seven" was wrong in both
// figures, and the only job that number has is letting a reader COUNT the
// widening):
//
//	script-src 'self'   the vendored htmx.min.js, served from our own origin.
//	                    There is NO CDN entry: the file is in web/static/vendor/ with
//	                    its version, source URL and sha256 recorded beside it, and
//	                    embedded in the binary — the same discipline the brand
//	                    faces came in under (M5-04).
//	connect-src 'self'  htmx pages with XMLHttpRequest, and XHR is governed by
//	                    connect-src, which FALLS BACK TO default-src when it is
//	                    absent. default-src is 'none', so without this directive
//	                    the script would load and every paging request it made
//	                    would be blocked by the browser. Measured in a real
//	                    browser rather than reasoned about — see the M6-03 notes in
//	                    docs/plan/m6-dashboard.md.
//
// 🔴 WHAT IT DOES NOT GAIN, and these are the two that matter: no 'unsafe-inline'
// and no 'unsafe-eval'. hx-get / hx-target / hx-swap are ATTRIBUTES that htmx's own
// code reads with getAttribute; the browser never evaluates them, so they are not
// inline script. htmx does contain one `new Function` and one `eval`, and both are
// reachable only through syntaxes this product does not use — hx-on:* handlers,
// the `js:` prefix on hx-vals/hx-headers, and bracketed event filters. That
// abstinence is asserted rather than assumed (transactions_test.go).
//
// ⚠️ THE COST OF THE SPLIT, STATED: two constants exist where there was one. They
// are not two representations — the second is the first plus a named suffix, on
// the line below — and exactly one panel SECTION sends the second.
//
// 🔴 THAT LAST CLAUSE USED TO SAY "which is itself pinned by a test" AND IT WAS NOT
// TRUE WHEN IT WAS WRITTEN. The test measured only the per-page correspondence
// (a page names script-src iff it loads one), which every page satisfies when ALL
// of them load the script — an audit mutated exactly that and the package stayed
// green. The CARDINALITY is now asserted, in
// TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty, and this sentence is
// true because that test counts rather than because this comment says so.
//
// ⚠️ "SECTION" IS THE HONEST WORD, NOT "URL". The cardinality check walks
// pages.PanelSections, and /admin/dockets is deliberately not in that table — so a
// fragment route that started sending the scripted policy is caught by
// TestDocketFragment_UsesTheUnwidenedPolicy rather than by the count.
const adminScriptedCSP = adminCSP + "; script-src 'self'; connect-src 'self'"
