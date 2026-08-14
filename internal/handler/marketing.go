package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/web/templates/pages"
)

// Marketing is the product's PUBLIC face (M7-01): the landing page at / and the
// four legal documents under /legal.
//
// 🔴 UNTIL THIS TYPE EXISTED THE ROOT OF THE SITE WAS A 404. The router registered
// exactly two things at the top level — /healthz and /static/* — and everything
// else lived under /t, /activate or /admin. This is the first route in the product
// that a stranger is MEANT to arrive at.
//
// 🔴 IT HAS NO IDENTITY, AND THAT IS WHY NONE OF THE PANEL'S PROTECTIONS APPLY TO
// IT. AdminAuth.Protect, ProtectWriting, the per-session budget and requireAdmin
// all begin by resolving somebody; there is nobody here. So the security profile
// had to be DERIVED rather than copied, and the derivation is this type's shape:
//
//	no *db.DB          -> it cannot make a database query, structurally
//	no session.Cookies -> it cannot read or write a cookie
//	no auditRecorder   -> it writes nothing anywhere
//	no *limiter        -> see "WHY THERE IS NO RATE LIMIT" below
//
// Those are not promises in a comment; there is no field to reach any of them
// through, so no handler on this type can acquire one without this file changing.
// TestMarketing_HandlerHoldsNoStatefulDependency asserts the shape by reflection so
// a later task cannot quietly add one.
//
// WHY THERE IS NO RATE LIMIT, and this was the decision the task was told to
// measure rather than assume. Every other limiter in this product protects a
// SCARCE thing: the activation budgets bound writes into an append-only table and
// the process log, the tap budget bounds database work on the shared pool, and the
// panel's bounds the resolver's read-plus-update per request. This surface spends
// none of them. A request here renders a fixed component tree and returns; it
// touches no pool, takes no lock and appends to no table. The two routes already in
// this class — /healthz and /static/* — are unmetered for the same reason and have
// been since M0.
//
// 🔴 THE PROCESS LOG IS THE FOURTH SCARCE THING AND THIS PARAGRAPH USED TO OMIT IT,
// WHILE NAMING IT TWO LINES ABOVE AS SOMETHING THE ACTIVATION BUDGET PROTECTS. A
// security audit measured the omission: 200 cancelled page loads wrote 200 ERROR
// lines from render below, against 1 line for 2000 equivalent attempts on
// /activate. The inventory is now complete, and the fix is at the source rather
// than a budget — a cancelled request is logged at DEBUG, because it is not a
// fault. What remains at ERROR is only what this server did wrong, which is rare
// and therefore self-limiting. See render.
//
// AND A LIMITER HERE WOULD HAVE A COST THAT IS NOT ZERO, which is the half that
// decided it. httpx.Limiter keys a map by caller address, bounded at 100k keys,
// and RESETS THE WHOLE MAP when the bound is reached — deliberately failing open.
// The landing page is by construction the most widely fetched URL in the
// deployment and the one a crawler walks, so it is the likeliest thing to drive a
// window map to its ceiling; a limiter mounted here would spend memory to protect
// nothing. Bandwidth is the only resource this surface consumes and a limiter does
// not bound bandwidth — a reverse proxy does, and the cacheable response below is
// what lets one help.
//
// ⚠️ AND THE COST STOPS THERE, WHICH AN EARLIER VERSION OF THIS PARAGRAPH GOT
// WRONG. It said such a limiter would make "the reset that forgets attempts more
// frequent", as though the eviction were shared. It is not: httpx.NewLimiter
// allocates a FRESH windows map per instance (ratelimit.go, one line in the
// constructor), so a landing-page limiter reaching its ceiling would forget only
// its own counters and could not touch the activation, tap or panel budgets. The
// decision is unchanged — the reason is one sentence smaller.
//
// 🔴 NO COUNT OF LIMITER INSTANCES APPEARS HERE, DELIBERATELY. The rewrite that
// fixed the sentence above put a figure in it ("eleven of them") and the figure
// was ALSO wrong — a hand count that missed the two inside httpx.NewTapLimiter
// because the grep behind it searched for the wrong spelling. That is the second
// wrong number in the same paragraph, so the number is gone rather than corrected:
// the argument rests on the constructor allocating per instance, which is a
// property of one line of code, and no count makes it truer.
//
// ⚠️ THE LIMIT OF THAT REASONING, STATED: it holds because these pages are static.
// The moment this surface gains a POST — the sign-up wizard, M7-02 — it stops being
// true, and that endpoint arrives with its own budget and its own bot challenge
// (the M7-02 card requires both). This type is not where that goes.
type Marketing struct {
	// signupHref is where "Start free" points.
	//
	// ✅ IT IS signupPath AS OF M7-02, AND UNTIL THEN IT WAS "". M7-01 shipped this
	// field empty on purpose — the wizard did not exist and a button that answers
	// 404 is the exact class of defect that page was written to avoid — and its card
	// named the two things M7-02 owed: "rotayı mount etmek ve
	// handler.Marketing.signupHref'i doldurmak". This is the second.
	//
	// IT IS THE ROUTE CONSTANT, NOT A STRING. handler.Signup.Mount registers
	// signupPath, so the link and the route are the same value at compile time
	// rather than two strings a test has to notice have diverged — the shape
	// adminLoginPath already uses for "Sign in".
	signupHref string
	log        *slog.Logger
	// texts is where the four legal documents' published bodies come from (M7-06).
	//
	// 🔴 IT IS THE THIRD FIELD ON THIS TYPE AND IT ARRIVED WITH ITS OWN ARGUMENT,
	// WHICH IS WHAT THE RULE ABOVE ASKS FOR. The list of permitted field types in
	// TestMarketing_HandlerHoldsNoStatefulDependency is deliberately short so that a
	// dependency cannot be added quietly; adding this one to it IS the visible edit,
	// and this is the argument.
	//
	// WHAT IT DOES NOT BREAK, and this is the whole point of its shape. The rate-limit
	// reasoning above rests on one sentence — "A request here renders a fixed
	// component tree and returns; it touches no pool, takes no lock and appends to no
	// table" — and a legalReader cannot make that false: its ONE method takes no
	// context.Context, returns no error, and reads an in-memory snapshot
	// (internal/domain/legal.Store). A method that cannot be cancelled and cannot fail
	// is not doing I/O. TestLegalReader_CannotReachTheDatabase asserts the signature
	// by reflection, so the guarantee survives somebody later implementing this
	// interface with something that queries — they would have to change the signature,
	// and the signature is what the test reads.
	//
	// ⚠️ THE ALTERNATIVE WAS A *db.DB HERE AND IT WAS REJECTED ON A MEASUREMENT, not
	// on taste: /legal/* is unmetered by the argument above, and a per-request SELECT
	// would have put an unauthenticated, unbudgeted path onto the pool that check-in
	// shares. What the snapshot costs instead is written down in internal/domain/legal.
	texts legalReader
}

// legalReader is the slice of internal/domain/legal.Store the PUBLIC surface needs.
// One method, no context, no error — see the field above for why that signature is
// the guarantee rather than a convenience.
type legalReader interface {
	Published() map[string]legal.Doc
}

// NewMarketing builds the public surface. There is nothing to inject: the pages
// are static, and the ABSENCE of parameters is the guarantee described above.
//
// ⚠️ signupHref IS SET HERE RATHER THAN PASSED IN, and that is what keeps the
// reflection test above meaningful. A parameter would make "does this deployment
// offer sign-up" a caller's decision, i.e. a second place the landing page's button
// could be wrong; a constant means the button exists exactly when the route does.
// texts may be nil, and that is the honest default rather than a hole: a nil reader
// means every legal page renders exactly what it rendered before M7-06 — the
// skeleton, saying nothing is in force. A constructor that refused nil would make
// this surface unbuildable for the twenty tests that do not care about legal texts,
// to protect against a failure mode whose worst outcome is under-claiming.
func NewMarketing(texts legalReader, log *slog.Logger) *Marketing {
	if log == nil {
		log = slog.Default()
	}
	return &Marketing{signupHref: signupPath, texts: texts, log: log}
}

// Mount registers the routes on r.
//
// 🔴 IT MOUNTS NO MIDDLEWARE OF ITS OWN, which is the visible half of the decision
// argued on the type. What these routes DO get is what the router applies to
// everything — request id, the bounded real-ip resolution, panic recovery and the
// 30 second timeout (internal/httpx.NewRouter) — and every one of those is pure and
// cheap. Nothing here needs the tap group's identity resolver or the panel's
// resolver, and mounting either would mean a database read on the one page that
// exists to be hammered by strangers.
//
// THE LEGAL ROUTES COME FROM pages.LegalPages RATHER THAN FROM LITERALS HERE, which
// is the shape mountSections already uses for the panel. The footer renders the
// same slice, so a document cannot exist in the navigation without a route or the
// other way round.
// 🔴 EVERY ROUTE ANSWERS HEAD AS WELL AS GET, AND THIS SURFACE IS THE ONLY ONE
// THAT DOES. Measured on the real router: chi's r.Get registers GET alone, so
// HEAD / , HEAD /legal/privacy and HEAD /healthz all answered 405 with
// `Allow: GET`, while HEAD /static/css/app.css answered 200 because
// http.FileServer handles the method itself.
//
// THAT 405 IS NOT NEW AND IS NOT A DEFECT OF THIS TASK — it is how every route in
// the product has always behaved. What IS new is the EXPOSURE: / and the four legal
// documents are the first URLs in this product that crawlers, uptime monitors and
// link checkers reach, and those clients HEAD before they GET. A 405 on the site's
// root reads as a broken site to a link checker and is a poor signal to a crawler,
// where a 405 on /activate is seen by nobody because that URL arrives in a personal
// message.
//
// SO THE FIX IS SCOPED TO THE SURFACE THAT HAS THE EXPOSURE, not applied to the
// router. Making every route in the product answer HEAD is a different decision
// with a different blast radius (it would newly admit HEAD to the tap endpoint and
// the panel, each of which meters and resolves identity), and it belongs to
// whoever owns that surface. It is reported for the backlog rather than taken here.
//
// WHAT IT COSTS: nothing per request that GET does not already cost, and net/http
// discards the body for a HEAD response itself — asserted on a real server, not a
// recorder, by TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders, because
// httptest.ResponseRecorder does NOT do that suppression and would have made this
// look correct while it was not.
func (m *Marketing) Mount(r chi.Router) {
	r.Get("/", m.Landing)
	r.Head("/", m.Landing)
	for _, p := range pages.LegalPages {
		page := p
		serve := func(w http.ResponseWriter, r *http.Request) {
			m.legal(w, r, page)
		}
		r.Get(page.Path, serve)
		r.Head(page.Path, serve)
	}
}

// Landing serves GET /.
func (m *Marketing) Landing(w http.ResponseWriter, r *http.Request) {
	m.render(w, r, pages.Landing(pages.LandingView{
		SignupHref:            m.signupHref,
		SignInHref:            adminLoginPath,
		PricePerEmployeeMonth: publishedPricePerEmployeeMonth,
		FreeMonths:            foundingFreeMonths,
	}))
}

// legal serves one document under /legal.
//
// 🔴 THE TEXT COMES FROM A MAP LOOKUP AND NOTHING ELSE HAPPENS HERE (M7-06). No
// query, no transaction, no cookie: the body is whatever the snapshot held when this
// request arrived, so this handler is as cacheable and as unmetered as it was when
// the pages were skeletons. A document with no text renders the skeleton, which is
// the state the whole of pages.legal.templ was designed around.
func (m *Marketing) legal(w http.ResponseWriter, r *http.Request, p pages.LegalPage) {
	v := pages.LegalPageView{Page: p, SignInHref: adminLoginPath}
	if p.Path == cookieNoticePath {
		v.Cookies = cookieNotice()
	}
	if m.texts != nil {
		if d, ok := m.texts.Published()[legalSlugOf(p.Path)]; ok {
			// ALREADY SPLIT. The paragraphs are computed once when a publication installs
			// the snapshot, not per request — this page is unmetered, so per-request work
			// here is work an anonymous caller can ask for as often as they like.
			v.Body = d.Paragraphs
			// The date a document was published is shown UTC, spelled out, because
			// "12/08/26" means two different days on two sides of an ocean and this is
			// the one page where that matters.
			v.PublishedAt = d.PublishedAt.UTC().Format("2 January 2006")
		}
	}
	m.render(w, r, pages.Legal(v))
}

// render writes one component with the headers this surface needs.
//
// 🔴 IT IS DELIBERATELY NOT AdminAuth.render, AND THE DIFFERENCE IS ONE HEADER. The
// panel sends Cache-Control: no-store because a panel response carries an
// operator's name and a synchronizer token, and a back button on a shared machine
// must not resurrect either. NONE OF THAT IS TRUE HERE, and it is not true
// structurally rather than by inspection: this type holds no session codec and no
// database, so it has nothing per-visitor to put in a body. The bytes are the same
// for every caller — asserted, not assumed, by
// TestMarketing_TheSameBytesForEveryVisitor, which drives the surface with
// different cookies and different client addresses and compares.
//
// So the marketing pages are CACHEABLE, which is the opposite of the panel and is
// the point: this is the one surface in the product where a reverse proxy or a CDN
// absorbing repeats is a feature rather than a leak. Copying the panel's header
// here would have been the "copied half the pattern" defect running in reverse —
// bringing across the protection that belongs to a different threat model.
//
// max-age is FIVE MINUTES rather than a day. The page carries a price and a
// sign-in link, and the price is the number a customer will hold us to; five
// minutes bounds how long a stale one can be served while still absorbing the
// burst that a link being shared produces. `public` is safe precisely because of
// the sentence above: a shared cache may keep this because nothing in it belongs
// to one visitor.
func (m *Marketing) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", marketingCSP)
	w.Header().Set("Cache-Control", marketingCacheControl)
	w.WriteHeader(http.StatusOK)
	if err := c.Render(r.Context(), w); err != nil {
		// 🔴 A CLIENT THAT WENT AWAY IS NOT AN ERROR, AND LOGGING IT AS ONE WAS
		// MEASURED COSTING SOMETHING REAL. 200 cancelled page loads produced 200
		// ERROR lines here — one per request, unbudgeted, on the most public URL in
		// the deployment. The same 2000 attempts against /activate produced ONE
		// line, because that flow's `unknown` budget exists precisely to bound the
		// PROCESS LOG.
		//
		// ⚠️ AND THE ATTACK IS THE LESSER HALF. An ordinary visitor on a slow phone
		// who navigates away mid-render produces exactly the same line. So these
		// were never reports of a fault: they were reports of somebody leaving,
		// filed at the severity reserved for things that need a human, which is how
		// a real render failure becomes unfindable.
		//
		// Debug, not Error, and it still is not swallowed (§7) — the distinction is
		// severity, not silence. Everything else keeps Error: a template that fails
		// to render, or a write that fails on a live connection, is this server's
		// fault and is rare.
		if errors.Is(err, context.Canceled) {
			m.log.Debug("a visitor left before the page finished", "path", r.URL.Path)
			return
		}
		// The status line is already on the wire, so there is nothing to send but a
		// log line. Never swallowed (§7).
		m.log.Error("rendering a marketing page failed", "err", err)
	}
}

// marketingCSP is the content policy for the public surface.
//
// IT IS DERIVED DIRECTIVE BY DIRECTIVE FROM WHAT THESE PAGES ACTUALLY LOAD, not
// inherited from the panel:
//
//	default-src 'none'      nothing loads unless named below
//	style-src 'self'        one stylesheet, /static/css/app.css
//	font-src 'self'         the six woff2 files app.css declares, same origin
//	form-action 'self'      there is NO form on this surface today. The directive
//	                        does not fall back to default-src, so its absence would
//	                        genuinely leave a future form free to post anywhere —
//	                        and M7-02 puts the sign-up form on exactly these pages.
//	base-uri 'none'         a <base> tag cannot re-root the relative asset paths
//	frame-ancestors 'none'  these pages are not embedded in anybody else's
//
// WHAT IS NOT NAMED IS THE OTHER HALF OF THE STATEMENT. There is no script-src,
// because there is no script and layout.Marketing has no slot to put one in. There
// is no img-src, because the page loads no image at all: the plaque and the docket
// in the hero are drawn with the existing brand rules, and the perforation is a CSS
// radial-gradient rather than a file. Naming a directive for something that does
// not load would make adding it a silent inheritance instead of a visible edit,
// which is the argument adminCSP makes about script-src from the other side.
//
// ⚠️ IT IS BYTE-IDENTICAL TO adminCSP TODAY AND THAT IS NOT A DEPENDENCY. Both
// surfaces happen to load exactly one stylesheet and the brand faces and nothing
// else, so the same six directives fall out of both derivations. Writing
// `= adminCSP` would have made a future widening of the PANEL widen the public
// surface too, silently. TestMarketing_PolicyNamesWhatThePageLoads checks the
// directives one at a time rather than comparing the two strings, so the two are
// free to diverge without a test having to be edited to permit it.
const marketingCSP = "default-src 'none'; style-src 'self'; font-src 'self'; " +
	"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// marketingCacheControl is the header the panel's no-store is NOT copied into. See
// render above for the argument and for the test that measures its premise.
const marketingCacheControl = "public, max-age=300"

// publishedPricePerEmployeeMonth is the price this page prints, formatted.
//
// 🔴 IT IS A SECOND COPY OF A NUMBER THE DATABASE ALSO HOLDS, and the duplication
// is unavoidable rather than sloppy: migration 00016 makes 1.50 the DEFAULT of
// tenants.price_per_employee_month, which is a per-tenant column — a tenant on a
// negotiated price is a legitimate row, so the landing page cannot read "the"
// price out of the database. What it publishes is the OFFER (docs/handoff.md §11),
// and the offer has to equal the default a new tenant is created with or the first
// invoice contradicts the page that sold it.
//
// TestLanding_PriceMatchesTheSchemaItIsCharged re-reads the DEFAULT out of the
// migration and compares. That is the whole reason this is a named constant with a
// comment instead of a literal in a template.
const publishedPricePerEmployeeMonth = "1.50"

// foundingFreeMonths is the founding offer's free period.
//
// THE PRODUCT ENFORCES THIS ONE rather than merely advertising it: migration
// 00016's tappa_first_chargeable_month adds `interval '3 months'` when plan =
// 'founding', db/queries/billing.sql calls it, and the panel's billing screen shows
// which month is the first chargeable one. Pinned against the migration by
// TestLanding_FreeMonthsMatchTheFunctionThatGrantsThem.
const foundingFreeMonths = 3

// cookieNoticePath is the document that carries the measured cookie table. It is
// looked up by path rather than by index so reordering pages.LegalPages cannot
// silently move the table onto the imprint.
const cookieNoticePath = "/legal/cookies"

// The two cookie lifetimes this package cannot reach.
//
// ⚠️ THEY ARE MIRRORS OF UNEXPORTED CONSTANTS AND ARE MARKED AS SUCH. Four of the
// six cookies are written by THIS package, so their Max-Age is used directly below
// and cannot drift. The employee session's year and the panel session's twelve
// hours live in internal/session and internal/adminauth as unexported constants,
// and widening either package's API for a marketing page would be the tail wagging
// the dog. Instead TestCookieNotice_LifetimesMatchTheCodeThatWritesThem re-reads
// both literals out of those two files and fails if these copies disagree — the
// same discipline the price above is held to.
const (
	sessionCookieMaxAge      = 365 * 24 * 60 * 60 // internal/session/cookie.go
	adminSessionCookieMaxAge = 12 * 60 * 60       // internal/adminauth/cookie.go
	// sessionCookiePath mirrors the Path internal/session writes. It is "/" because
	// the employee cookie must accompany /t, /api/checkin and the activation flow.
	sessionCookiePath = "/"
)

// cookieNotice is the cookie table on /legal/cookies, BUILT FROM THE CONSTANTS THAT
// WRITE THE COOKIES.
//
// 🔴 A DISCLOSURE OBLIGATION KEPT BY HAND GOES STALE ON THE NEXT FEATURE. Every
// name below is the constant itself rather than a string typed into a template, so
// renaming a cookie renames it here; and
// TestCookieNotice_ListsExactlyTheCookiesTheProductSets derives the set of cookie
// names from the source of internal/session, internal/adminauth and this package
// and fails when the product grows a seventh that this table does not mention.
//
// SIX COOKIES, AND THE SET IS THE MEASUREMENT rather than a claim: every
// http.SetCookie in non-test code writes one of these six.
//
// ⚠️ WHAT THE TABLE DOES NOT SAY, deliberately. It does not name the VALUE of
// anything (§4.7) and it does not say what a value is derived from beyond "a
// hash" — a cookie notice is a disclosure of purpose and duration, not a design
// document for whoever is attacking it.
func cookieNotice() []pages.CookieRow {
	const flags = "HttpOnly · SameSite=Lax · Secure"
	return []pages.CookieRow{
		{
			Name: session.CookieName,
			Purpose: "Remembers which employee this phone belongs to, so a tap at a plaque " +
				"knows who tapped. Set once, when the personal invitation link is opened.",
			Lifetime: humanSeconds(sessionCookieMaxAge),
			Scope:    sessionCookiePath,
			Flags:    flags,
		},
		{
			Name: activationCookieName,
			Purpose: "Carries the invitation while that one page is being completed, so the " +
				"code stays out of the address bar, out of browser history and out of any " +
				"link that leaves this site. Cleared as soon as the invitation is used.",
			Lifetime: humanSeconds(activationCookieMaxAge),
			Scope:    sessionCookiePath,
			Flags:    flags,
		},
		{
			Name: adminauth.CookieName,
			Purpose: "Keeps a manager signed in to the dashboard. A separate cookie from the " +
				"employee one, sent only to the dashboard, so neither can be used in the " +
				"other's place.",
			Lifetime: humanSeconds(adminSessionCookieMaxAge),
			Scope:    adminauth.CookiePath,
			Flags:    flags,
		},
		{
			Name: adminLoginCookieName,
			Purpose: "Protects the dashboard sign-in form against being submitted from another " +
				"site. Exists only while somebody is on the sign-in page.",
			Lifetime: humanSeconds(adminLoginCookieMaxAge),
			Scope:    adminauth.CookiePath,
			Flags:    flags,
		},
		{
			Name: adminChoiceCookieName,
			Purpose: "Set only when one person manages more than one organisation: it carries " +
				"the choice being offered, signed by this server, between the password step " +
				"and the pick-a-business step.",
			Lifetime: humanSeconds(adminChoiceCookieMaxAge),
			Scope:    adminauth.CookiePath,
			Flags:    flags,
		},
		{
			Name: adminConfirmCookieName,
			Purpose: "Set for a moment when a manager is asked to confirm something that cannot " +
				"be undone, so the confirmation belongs to that manager, that session and " +
				"that one action.",
			Lifetime: humanSeconds(adminConfirmMaxAge),
			Scope:    adminauth.CookiePath,
			Flags:    flags,
		},
		// THE TWO M7-04 ADDED. Both are scoped to the dashboard, and the second is the
		// only cookie in this table that carries a CREDENTIAL rather than a form
		// token — which is exactly why it is a cookie at all: it keeps the recovery
		// link out of the address bar, out of browser history and out of any link
		// that leaves this site (internal/handler/adminreset.go).
		{
			Name: adminResetCookieName,
			Purpose: "Protects the 'I cannot get in' form against being submitted from another " +
				"site. Exists only while somebody is on that page.",
			Lifetime: humanSeconds(adminResetCookieMaxAge),
			Scope:    adminauth.CookiePath,
			Flags:    flags,
		},
		{
			Name: adminResetLinkCookieName,
			Purpose: "Carries a recovery link while a new password is being chosen, so the link " +
				"stays out of the address bar and out of browser history. Cleared as soon as " +
				"the link is used or refused.",
			Lifetime: humanSeconds(adminResetLinkCookieMaxAge),
			Scope:    adminResetLinkCookiePath,
			Flags:    flags,
		},
		// THE TWO M7-02 ADDED. They are scoped to /signup, so they never accompany a
		// tap, a panel request or a view of this very page — which is also what keeps
		// the marketing responses identical for every visitor and therefore cacheable.
		{
			Name: signupCookieName,
			Purpose: "Protects the registration form against being submitted from another site. " +
				"Exists only while somebody is filling the form in.",
			Lifetime: humanSeconds(signupCookieMaxAge),
			Scope:    signupCookiePath,
			Flags:    flags,
		},
		{
			Name: signupStateCookieName,
			Purpose: "Carries the answers already given — the business name, the VAT number and " +
				"the places — from one step of the registration form to the next, signed by " +
				"this server. It never holds a password. Cleared as soon as the business is " +
				"created.",
			Lifetime: humanSeconds(signupCookieMaxAge),
			Scope:    signupCookiePath,
			Flags:    flags,
		},
	}
}

// humanSeconds prints a Max-Age in words WITHOUT ROUNDING. Every lifetime in the
// table divides exactly into days, hours or minutes, so the reader is given the
// real number rather than an "about". If a future cookie does not divide, the
// default arm prints the seconds rather than inventing a nearest unit — a cookie
// notice that rounds is a cookie notice that is wrong.
func humanSeconds(sec int) string {
	unit := func(n int, singular, plural string) string {
		if n == 1 {
			return "1 " + singular
		}
		return strconv.Itoa(n) + " " + plural
	}
	switch {
	case sec >= 24*3600 && sec%(24*3600) == 0:
		return unit(sec/(24*3600), "day", "days")
	case sec >= 3600 && sec%3600 == 0:
		return unit(sec/3600, "hour", "hours")
	case sec >= 60 && sec%60 == 0:
		return unit(sec/60, "minute", "minutes")
	default:
		return unit(sec, "second", "seconds")
	}
}
