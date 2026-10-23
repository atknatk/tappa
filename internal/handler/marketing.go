package handler

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	apidocs "github.com/atknatk/tappa/docs/api"
	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/apiguide"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/webhook"
	"github.com/atknatk/tappa/web"
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
// ⚠️ M8-01 ADDED A THIRD UNMETERED ROUTE THAT IS **NOT** IN THIS CLASS, AND SAYING
// SO HERE IS THE POINT. /readyz does touch the pool — that is what readiness means —
// so the sentence above would have been quietly false about the product if this
// paragraph had been left to describe "the unmetered routes". It is not metered by a
// limiter either, for the memory reason argued below; what bounds it instead is a
// cached outcome, so any number of callers cost at most one query per second and at
// most two log lines per outage. The argument lives with the endpoint
// (internal/handler/health.go), and the class this paragraph describes still has
// exactly two members.
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
	// demoVideoSrc is the URL of the demo recording, or "" when this binary does not
	// carry one. demoPosterSrc is its poster frame, or "" — and it is always "" when
	// there is no video, because a poster on its own is nothing.
	//
	// 🔴 THEY ARE RESOLVED ONCE, IN THE CONSTRUCTOR, AND NOT PER REQUEST, and that is
	// a statement about what they ARE rather than a micro-optimisation: web.Static()
	// is an embed.FS, so the answer is fixed at COMPILE time. A per-request fs.Stat
	// would have implied the asset tree can change while the process runs, which is
	// the one thing web/embed.go exists to make untrue, and it would have put a
	// lookup on the unmetered path this type's whole argument rests on being free.
	//
	// ⚠️ THEY ARE STRINGS AND THE REFLECTION TEST ABOVE ALREADY PERMITS STRINGS, so
	// this pair adds no capability to the type: a path this feature links to is
	// exactly what that allowance was written for. Nothing here can query, log or
	// remember anything about a visitor.
	demoVideoSrc  string
	demoPosterSrc string
	// landingPolicy is the Content-Security-Policy header / sends in THIS build. See
	// landingCSPFor for why it is computed rather than written down as a constant.
	landingPolicy string
	// exampleMonthly is what exampleHeadcount people cost a month at the published
	// price, formatted without a symbol — "90" — or "" when the constant could not
	// be read, in which case the page renders no example (see
	// pages.LandingView.ExampleMonthly). Computed once, here, by exampleTeamMonthly.
	exampleMonthly string
	// pricing is the pricing page's figures (LP-06): what each example team size costs a
	// month and the drawn frozen month, computed ONCE, here, by pricingFigures from the
	// published price in integer cents -- strings and ints only, a value rather than a
	// dependency (TestMarketing_HandlerHoldsNoStatefulDependency walks it). Empty Sizes
	// when the price could not be read: the page then draws no figure at all.
	pricing pages.PricingFigures
	// security and securityCopy are the security page's numbers and its sentences
	// built from them (LP-07), computed ONCE, here, by securityFigures and
	// pages.SecurityCopy: ints, strings and slices of them, a value rather than a
	// dependency (TestMarketing_HandlerHoldsNoStatefulDependency walks it).
	security     pages.SecurityFigures
	securityCopy pages.SecurityText
	// product and productCopy are the product tour's numbers and its sentences built
	// from them (LP-08), computed ONCE, here, by productFigures and pages.ProductCopy: a
	// value rather than a dependency, like the security page's.
	product     pages.ProductFigures
	productCopy pages.ProductText
	// guideEN and guideTR are the integration guide's two pages (M11 DOCS-1, ADR 0030):
	// docs/api's embedded Markdown, converted ONCE, in the constructor, by
	// internal/apiguide. A request serves the converted bytes and converts nothing, so
	// the pages are as unmetered as /legal/* for the same reason: a fixed body, no pool,
	// no cookie. The type holds strings and slices of strings only --
	// TestMarketing_HandlerHoldsNoStatefulDependency reads that, field by field, rather
	// than trusting the name.
	guideEN apiguide.Guide
	guideTR apiguide.Guide
	// openAPIHref is where the guide pages offer the OpenAPI document for download:
	// https://<TAPPA_API_HOST>/v1/openapi.yaml, or "" on a deployment with no API host,
	// where no link is drawn (openAPIHrefFor says why).
	openAPIHref string
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
//
// apiHost is TAPPA_API_HOST (config.APIHost), "" when this deployment has none; the
// integration guide's pages link the OpenAPI document on it (openAPIHrefFor).
func NewMarketing(texts legalReader, log *slog.Logger, apiHost string) *Marketing {
	if log == nil {
		log = slog.Default()
	}
	openAPI := openAPIHrefFor(apiHost)
	guideEN, guideTR := renderGuides(openAPI, log)
	static := web.Static()
	video := staticAssetURL(static, demoVideoAsset)
	poster := ""
	if video != "" {
		poster = staticAssetURL(static, demoPosterAsset)
	}
	example, err := exampleTeamMonthly(publishedPricePerEmployeeMonth, exampleHeadcount)
	if err != nil {
		// A malformed published price is a programming error in a constant, and
		// TestLanding_ExampleTeamCostIsThePriceTimesTheHeadcount fails on it before
		// this can run in production. Should it ever get here, the page says less
		// rather than something wrong: no example is rendered.
		log.Error("landing: the worked example cannot be computed; rendering none", "err", err)
		example = ""
	}
	pricing, err := pricingFigures(publishedPricePerEmployeeMonth, pricingExampleSizes, exampleHeadcount)
	if err != nil {
		// The same programming error as above, and the same answer: the pricing page draws
		// no figure rather than a wrong one.
		log.Error("pricing: the example figures cannot be computed; rendering none", "err", err)
		pricing = pages.PricingFigures{}
	}
	security := securityFigures()
	product := productFigures(security)
	return &Marketing{
		signupHref:     signupPath,
		texts:          texts,
		log:            log,
		demoVideoSrc:   video,
		demoPosterSrc:  poster,
		landingPolicy:  landingCSPFor(video, poster),
		exampleMonthly: example,
		pricing:        pricing,
		security:       security,
		securityCopy:   pages.SecurityCopy(security),
		product:        product,
		productCopy:    pages.ProductCopy(product),
		guideEN:        guideEN,
		guideTR:        guideTR,
		openAPIHref:    openAPI,
	}
}

// openAPIHrefFor is where the guide pages link the OpenAPI document.
//
// WITH AN API HOST it is that host's own /v1/openapi.yaml, the address the guide itself
// names: the document is served there, without a key, beside /v1/guide.md. It is a LINK
// a reader follows, not a resource the page loads -- nothing on the page fetches it.
//
// WITHOUT ONE (dev, or a deployment that has not chosen an API host) there is no link,
// rather than a copy of the document on this host. A deployment without TAPPA_API_HOST
// builds no /v1 at all (fail closed, ADR 0028 §13): it has no API, and offering the
// contract of an API it does not serve would be a promise the deployment does not keep.
// And a second public address for openapi.yaml on the product host would be a second
// copy to keep in step with the one the API host serves.
func openAPIHrefFor(apiHost string) string {
	if apiHost == "" {
		return ""
	}
	return "https://" + apiHost + "/v1/openapi.yaml"
}

// renderGuides converts the two embedded guides, once. A guide that does not convert
// is a programming error in docs/api (internal/apiguide's tests convert both); should
// one reach production, its page answers 500 and this logs it once, here, instead of
// per request.
func renderGuides(openAPIHref string, log *slog.Logger) (en, tr apiguide.Guide) {
	links := map[string]string{
		// A file-relative link between the two Markdown files, or to the OpenAPI document
		// beside them, is drawn as the address that file has on this site. "" (no API
		// host) draws the openapi.yaml link as its text.
		"integration-guide.md":    apiGuidePath,
		"integration-guide.tr.md": apiGuideTRPath,
		"openapi.yaml":            openAPIHref,
	}
	var err error
	if en, err = apiguide.Render(apidocs.English(), apiguide.Options{Links: links}); err != nil {
		log.Error("the integration guide (English) could not be converted; /docs/api answers 500", "err", err)
	}
	if tr, err = apiguide.Render(apidocs.Turkish(), apiguide.Options{Links: links}); err != nil {
		log.Error("the integration guide (Turkish) could not be converted; /docs/api/tr answers 500", "err", err)
	}
	return en, tr
}

// The two files the hero's demo dialog looks for, as paths INSIDE web/static.
//
// 🔴 THEY ARE NAMED HERE AND NOWHERE ELSE. The template holds no /static path of its
// own (pages.LandingView.DemoVideoSrc records why), so renaming the file is one edit
// here rather than a hunt through markup — and a typo produces the honest empty state
// rather than a 404 inside a dialog.
const (
	demoVideoAsset  = "video/demo.mp4"
	demoPosterAsset = "video/demo-poster.jpg"
)

// staticAssetURL returns the public URL of name inside the embedded asset tree, or
// "" when the tree does not carry it.
//
// 🔴 IT TAKES AN fs.FS RATHER THAN CALLING web.Static() ITSELF, and that parameter is
// the whole of its testability. The embedded tree is a compile-time constant, so a
// test that wanted to drive the "the video IS there" branch could not create one — it
// would have to commit a video into the repository in order to exercise a branch.
// With a parameter the branch runs against an fstest.MapFS, and what the SHIPPED
// binary answers is measured separately, by running the server.
//
// A DIRECTORY IS NOT AN ASSET. fs.Stat succeeds on web/static/video itself, and
// <video src="/static/video"> would answer 404 — the file server refuses to list —
// which is precisely the broken element this function exists to prevent.
//
// IT DOES NOT READ THE DISK: fs.Stat on an embed.FS reads a table inside the binary.
// TestPackaging_NoProductionCodeReadsTheDisk names the os.* calls that would.
func staticAssetURL(fsys fs.FS, name string) string {
	info, err := fs.Stat(fsys, name)
	if err != nil || info.IsDir() {
		return ""
	}
	return "/static/" + name
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
// ✅ /healthz ANSWERS HEAD AS OF M8-01, so the measurement above is a record of
// what was true on 2026-08-13 rather than a description of the product. The same
// reasoning decided it — the route is unauthenticated, unmetered and touches
// nothing, and the only clients it has are uptime monitors, which HEAD — and the
// same reasoning still leaves /t, /activate and the panel answering 405 (backlog
// T29). The two health endpoints and this surface are the ones with the exposure;
// the rest are reached from a personal message or behind a sign-in.
//
// WHAT IT COSTS: nothing per request that GET does not already cost, and net/http
// discards the body for a HEAD response itself — asserted on a real server, not a
// recorder, by TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders, because
// httptest.ResponseRecorder does NOT do that suppression and would have made this
// look correct while it was not.
func (m *Marketing) Mount(r chi.Router) {
	r.Get("/", m.Landing)
	r.Head("/", m.Landing)
	// THE PUBLIC SITE'S OWN PAGES (LP-06 …), each under a named path equal to its row of
	// pages.PublicPages. Registered by name, not by ranging over that table, for the
	// reason the guide's routes below give (the employee-route derivation resolves a
	// name); TestPublicPages_TheTableIsTheMountedRoutes holds the table and these
	// registrations equal in both directions.
	r.Get(productPath, m.Product)
	r.Head(productPath, m.Product)
	r.Get(securityPath, m.Security)
	r.Head(securityPath, m.Security)
	r.Get(pricingPath, m.Pricing)
	r.Head(pricingPath, m.Pricing)
	r.Get(forRestaurantsPath, m.ForRestaurants)
	r.Head(forRestaurantsPath, m.ForRestaurants)
	r.Get(forProductionPath, m.ForProduction)
	r.Head(forProductionPath, m.ForProduction)
	for _, p := range pages.LegalPages {
		page := p
		serve := func(w http.ResponseWriter, r *http.Request) {
			m.legal(w, r, page)
		}
		r.Get(page.Path, serve)
		r.Head(page.Path, serve)
	}
	// The integration guide (M11 DOCS-1): same surface, same headers, same HEAD rule.
	// Registered by name, not from a table, so the employee-route derivation
	// (admincookiepath_test.go) resolves both paths and holds the panel cookie off them.
	r.Get(apiGuidePath, m.apiGuideEN)
	r.Head(apiGuidePath, m.apiGuideEN)
	r.Get(apiGuideTRPath, m.apiGuideTR)
	r.Head(apiGuideTRPath, m.apiGuideTR)
}

// The guide's two routes. They are pages.APIGuidePath and pages.APIGuideTRPath -- the
// constants the footer, the panel and the language switch link with --, spelled out here
// so the route scan can read them; TestAPIGuidePages_RoutesAreTheLinkedPaths holds the
// two pairs equal.
const (
	apiGuidePath   = "/docs/api"
	apiGuideTRPath = apiGuidePath + "/tr"
)

func (m *Marketing) apiGuideEN(w http.ResponseWriter, r *http.Request) { m.apiGuide(w, r, false) }
func (m *Marketing) apiGuideTR(w http.ResponseWriter, r *http.Request) { m.apiGuide(w, r, true) }

// apiGuide serves /docs/api or /docs/api/tr: the guide converted at construction, in
// the public chrome, cacheable and indexable like the legal pages.
func (m *Marketing) apiGuide(w http.ResponseWriter, r *http.Request, turkish bool) {
	g := m.guideEN
	if turkish {
		g = m.guideTR
	}
	if g.Title == "" {
		// The conversion failed at start-up and was logged there (renderGuides).
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	m.render(w, r, pages.APIGuide(pages.APIGuideView{
		Guide:       g,
		Turkish:     turkish,
		OpenAPIHref: m.openAPIHref,
		SignInHref:  adminLoginPath,
		SignupHref:  m.signupHref,
	}))
}

// Landing serves GET /.
func (m *Marketing) Landing(w http.ResponseWriter, r *http.Request) {
	m.renderWith(w, r, m.landingPolicy, pages.Landing(pages.LandingView{
		SignupHref:            m.signupHref,
		SignInHref:            adminLoginPath,
		PricePerEmployeeMonth: publishedPricePerEmployeeMonth,
		FreeMonths:            foundingFreeMonths,
		ExampleHeadcount:      exampleHeadcount,
		ExampleMonthly:        m.exampleMonthly,
		DemoVideoSrc:          m.demoVideoSrc,
		DemoPosterSrc:         m.demoPosterSrc,
	}))
}

// pricingPath is pages.PricingPath, spelled out so the route scan can read it; the
// public-page table test holds the two equal.
const pricingPath = "/pricing"

// Pricing serves GET /pricing (LP-06): the price card's four figures as the landing has
// them, and the page's own figures computed once in NewMarketing.
func (m *Marketing) Pricing(w http.ResponseWriter, r *http.Request) {
	m.render(w, r, pages.Pricing(pages.PricingView{
		SignupHref:            m.signupHref,
		SignInHref:            adminLoginPath,
		PricePerEmployeeMonth: publishedPricePerEmployeeMonth,
		FreeMonths:            foundingFreeMonths,
		ExampleHeadcount:      exampleHeadcount,
		ExampleMonthly:        m.exampleMonthly,
		Figures:               m.pricing,
	}))
}

// securityPath is pages.SecurityPath, spelled out so the route scan can read it; the
// public-page table test holds the two equal.
const securityPath = "/security"

// Security serves GET /security (LP-07): the page's figures and sentences, computed
// once in NewMarketing.
func (m *Marketing) Security(w http.ResponseWriter, r *http.Request) {
	m.render(w, r, pages.Security(pages.SecurityView{
		SignupHref: m.signupHref,
		SignInHref: adminLoginPath,
		FreeMonths: foundingFreeMonths,
		Figures:    m.security,
		Copy:       m.securityCopy,
	}))
}

// productPath is pages.ProductPath, spelled out so the route scan can read it; the
// public-page table test holds the two equal.
const productPath = "/product"

// Product serves GET /product (LP-08): the tour's figures and sentences, computed once in
// NewMarketing.
func (m *Marketing) Product(w http.ResponseWriter, r *http.Request) {
	m.render(w, r, pages.Product(pages.ProductView{
		SignupHref: m.signupHref,
		SignInHref: adminLoginPath,
		FreeMonths: foundingFreeMonths,
		Figures:    m.product,
		Copy:       m.productCopy,
	}))
}

// forRestaurantsPath is pages.ForRestaurantsPath, spelled out so the route scan can read
// it; the public-page table test holds the two equal.
const forRestaurantsPath = "/for/restaurants"

// ForRestaurants serves GET /for/restaurants (LP-09). The page's sentences and its demo
// table are values of the pages package (pages.RestaurantsCopy, pages.RestaurantsVenues);
// what the handler hands in is the chrome's two ways in and the founding offer's months.
func (m *Marketing) ForRestaurants(w http.ResponseWriter, r *http.Request) {
	m.render(w, r, pages.ForRestaurants(pages.ForRestaurantsView{
		SignupHref: m.signupHref,
		SignInHref: adminLoginPath,
		FreeMonths: foundingFreeMonths,
	}))
}

// forProductionPath is pages.ForProductionPath, spelled out so the route scan can read it;
// the public-page table test holds the two equal.
const forProductionPath = "/for/production"

// ForProduction serves GET /for/production (LP-10). Its one figure is the repeat window
// the security page already holds (m.security, computed once in NewMarketing), and its
// sentences are built from it here: a handful of strings, no state, no read -- so the
// page keeps no field of its own on Marketing.
func (m *Marketing) ForProduction(w http.ResponseWriter, r *http.Request) {
	f := forProductionFigures(m.security)
	m.render(w, r, pages.ForProduction(pages.ForProductionView{
		SignupHref: m.signupHref,
		SignInHref: adminLoginPath,
		FreeMonths: foundingFreeMonths,
		Figures:    f,
		Copy:       pages.ForProductionCopy(f),
	}))
}

// forProductionFigures is the production page's numbers: the repeat window, the security
// page's own figure (securityFigures holds it to policy.DefaultParams and to the config
// default), so the pages cannot disagree.
func forProductionFigures(s pages.SecurityFigures) pages.ForProductionFigures {
	return pages.ForProductionFigures{DebounceSeconds: s.DebounceSeconds}
}

// productFigures is the product tour's numbers: the debounce window and the webhook
// header, the same values the security page prints (securityFigures holds each to its
// source), so the two pages cannot disagree.
func productFigures(s pages.SecurityFigures) pages.ProductFigures {
	return pages.ProductFigures{DebounceSeconds: s.DebounceSeconds, WebhookHeader: s.WebhookHeader}
}

// The security page's figures that no exported value of the product carries. EACH IS A
// MIRROR OF THE CODE NAMED BESIDE IT, the way foundingFreeMonths mirrors migration
// 00016, and internal/handler's security tests read that code and hold the two equal --
// so a change there turns the page's test red instead of leaving the page wrong.
const (
	// internal/sun's cmacHexLen: the signature in the plaque's link, in hex characters.
	securitySignatureHexChars = 16
	// internal/sun's tagKeyLen (16 bytes), in bits: a plaque's own AES key.
	securityPlaqueKeyBits = 128
	// internal/session's tokenBytes (32), in bits.
	securityTokenBits = 256
	// internal/netx's maxProofHostBitsV4 (25) and maxProofHostBitsV6 (97), as the prefix
	// length of a block that size: 32-25 and 128-97.
	securityIPv4Prefix = 7
	securityIPv6Prefix = 31
	// internal/config's TAPPA_GPS_RADIUS_M default.
	securityGPSRadiusM = 150
	// tap.trustScore's two additions: an address match and a position match.
	securityTrustIP  = 50
	securityTrustGPS = 30
)

// securityFigures is the security page's numbers: the mirrors above, and the ones the
// product exports read from it directly -- the guardrail count from the list itself, the
// default window from policy.DefaultParams (which internal/config's default matches),
// the trust score's base from tap.TrustBase, and the webhook header's name.
func securityFigures() pages.SecurityFigures {
	params := policy.DefaultParams()
	return pages.SecurityFigures{
		SignatureHexChars: securitySignatureHexChars,
		PlaqueKeyBits:     securityPlaqueKeyBits,
		TokenBits:         securityTokenBits,
		IPv4Prefix:        securityIPv4Prefix,
		IPv6Prefix:        securityIPv6Prefix,
		GPSRadiusM:        securityGPSRadiusM,
		DebounceSeconds:   int(params.DebounceWindow / time.Second),
		Guardrails:        len(policy.Guardrails(params)),
		TrustBase:         tap.TrustBase,
		TrustIP:           securityTrustIP,
		TrustGPS:          securityTrustGPS,
		WebhookHeader:     webhook.SignatureHeader,
	}
}

// legal serves one document under /legal.
//
// 🔴 THE TEXT COMES FROM A MAP LOOKUP AND NOTHING ELSE HAPPENS HERE (M7-06). No
// query, no transaction, no cookie: the body is whatever the snapshot held when this
// request arrived, so this handler is as cacheable and as unmetered as it was when
// the pages were skeletons. A document with no text renders the skeleton, which is
// the state the whole of pages.legal.templ was designed around.
func (m *Marketing) legal(w http.ResponseWriter, r *http.Request, p pages.LegalPage) {
	v := pages.LegalPageView{Page: p, SignInHref: adminLoginPath, SignupHref: m.signupHref}
	if p.Path == cookieNoticePath {
		v.Cookies = cookieNotice()
	}
	if m.texts != nil {
		published := m.texts.Published()
		// THE DOCUMENT NAVIGATION'S "not in force yet" MARKS come from the same snapshot
		// as the body, read once, so the page cannot describe two different moments. They
		// depend on what is published and on nothing about the visitor, so the page stays
		// the same bytes for everybody and the public Cache-Control still holds.
		v.PublishedSlugs = make(map[string]bool, len(published))
		for slug := range published {
			v.PublishedSlugs[slug] = true
		}
		if d, ok := published[legalSlugOf(p.Path)]; ok {
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

// legalSlugOf turns "/legal/privacy" into "privacy".
//
// 🔴 THE SLUG IS DERIVED FROM THE PATH RATHER THAN STORED BESIDE IT, which is what
// keeps the database's closed set and the router's closed set from becoming two
// lists that drift. pages.LegalPages owns the URL; 00020's CHECK owns the value; this
// is the one line that says they are the same thing, and
// TestLegalSlugs_AreExactlyTheDocumentsWithAPage fails if any pair stops matching.
// (It lived beside the M7-06 panel's legal screen until M10 OP-10 moved publishing
// to the operator's surface; the public page is its one reader now.)
func legalSlugOf(path string) string {
	return strings.TrimPrefix(path, "/legal/")
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
	m.renderWith(w, r, marketingCSP, c)
}

// renderWith is render with the policy named, and it exists because ONE PAGE on
// this surface now loads something the others do not — see landingCSP. render
// above keeps its three-argument shape so that "the marketing policy" stays the
// default a caller gets without asking.
func (m *Marketing) renderWith(w http.ResponseWriter, r *http.Request, policy string, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", policy)
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

// landingCSP is the policy for / ALONE, and it is marketingCSP plus one directive.
//
// 🔴 THE LANDING PAGE CARRIES A SCRIPT AS OF 2026-09-14 AND THE FOUR LEGAL PAGES DO
// NOT. The user's landing design (docs/design/landing-reference-2026-09-12.html)
// rotates four made-up records through the ticket in the hero, and the instruction
// was to reproduce that page rather than adapt it. A policy that forbids scripts on
// a page that loads one is a broken page, not a strict one — so the directive is
// added where the script is and nowhere else.
//
// WHAT IT IS NOT: 'unsafe-inline'. The script is a FILE, web/static/js/landing.js,
// embedded in the binary and served from /static like every other asset here, so
// 'self' is the whole of the widening. No 'unsafe-eval', no connect-src — the
// script makes no request of any kind.
//
// WHY A SECOND CONSTANT RATHER THAN WIDENING THE FIRST. /legal/* loads exactly what
// it loaded before, and a single widened policy would have handed a script-src to
// four pages that have no script and no reason to grow one. The two are derived
// from one base string so the five shared directives cannot drift.
const landingCSP = marketingCSP + "; script-src 'self'"

// landingCSPFor is landingCSP plus exactly the directives the DEMO ASSETS IN THIS
// BUILD need, and nothing else (2026-09-14).
//
// 🔴 IT IS A FUNCTION AND NOT A THIRD CONSTANT, AND THE REASON IS THE SENTENCE
// marketingCSP ALREADY ARGUES: "a directive named for something that does not load
// makes the next addition a silent inheritance instead of a visible edit". The demo
// recording is not in the repository — the dialog ships showing a sentence, not a
// player — so a constant `media-src 'self'` would be a permission granted today for
// a file that arrives on some later day, on the one page in the product that every
// stranger reaches. Computing it means the binary that carries no video sends BYTE
// FOR BYTE the policy it sent before this task, and the binary the user builds after
// dropping demo.mp4 in sends media-src, because by then the page really does load
// media.
//
// WHY img-src IS HERE AT ALL, since this page loads no image and says so at length:
// a <video poster> IS an image fetch, governed by img-src, and img-src has no
// fallback to media-src — it falls back to default-src 'none'. A poster added
// without this directive would not be a visible error; it would be a black
// rectangle and a console line nobody reads. So the directive arrives with the file
// that needs it and departs with it.
//
// WHAT IS STILL NOT HERE, IN ANY BUILD: no connect-src (the player streams over the
// same media-src), no 'unsafe-inline', no host but 'self'. A YouTube or Vimeo embed
// would have needed frame-src plus a third-party origin plus their cookies on this
// page; self-hosting under /static is what keeps the whole policy to one keyword.
func landingCSPFor(videoSrc, posterSrc string) string {
	policy := landingCSP
	if videoSrc != "" {
		policy += "; media-src 'self'"
	}
	if posterSrc != "" {
		policy += "; img-src 'self'"
	}
	return policy
}

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

// exampleHeadcount is the team size the price card's worked example uses — "A
// 60-person chain runs on €90/month". The user chose the sixty; the ninety is
// computed from it and the published price by exampleTeamMonthly, never typed, so
// that a change to the price cannot leave a stale figure beside it.
const exampleHeadcount = 60

// exampleTeamMonthly is headcount × price, in cents, formatted the way the price
// is ("90", "90.50") and without a currency symbol.
//
// 🔴 IT IS INTEGER ARITHMETIC ON CENTS, NOT A float64 (CLAUDE.md §6: no money in
// floats). The price is parsed as "<euros>.<two digits>" — the shape
// publishedPricePerEmployeeMonth has and TestLanding_PriceMatchesTheSchemaItIsCharged
// already requires — and anything else is an error, which NewMarketing turns into
// "render no example" rather than a wrong number.
func exampleTeamMonthly(price string, headcount int) (string, error) {
	total, err := monthlyCents(price, headcount)
	if err != nil {
		return "", err
	}
	if total%100 == 0 {
		return strconv.Itoa(total / 100), nil
	}
	return fmt.Sprintf("%d.%02d", total/100, total%100), nil
}

// monthlyCents is headcount × price in cents: the one multiplication behind every
// figure the public pages print. The price is "<euros>.<two digits>" and nothing else.
func monthlyCents(price string, headcount int) (int, error) {
	euros, cents, ok := strings.Cut(price, ".")
	if !ok || len(cents) != 2 {
		return 0, fmt.Errorf("example cost: price %q is not <euros>.<cents>", price)
	}
	e, err := strconv.Atoi(euros)
	if err != nil || e < 0 {
		return 0, fmt.Errorf("example cost: price %q: bad euros", price)
	}
	c, err := strconv.Atoi(cents)
	if err != nil || c < 0 {
		return 0, fmt.Errorf("example cost: price %q: bad cents", price)
	}
	if headcount <= 0 {
		return 0, fmt.Errorf("example cost: headcount %d is not positive", headcount)
	}
	return (e*100 + c) * headcount, nil
}

// pricingExampleSizes are the team sizes the pricing page's table prices (the Pricing
// board's four). They are examples, not claims: what the page states about each is the
// arithmetic, and the sixty is the landing's worked example, so the two pages agree.
var pricingExampleSizes = []int{10, 25, exampleHeadcount, 150}

// pricingFigures computes the pricing page's figures from the published price: each
// size's monthly cost and the drawn frozen month for headcount people, all in the
// Billing screen's own money format -- two decimals, "15.00" in the table (the template
// writes the sign) and "€90.00" on the drawing, through billing.Money and money() -- so
// the table and the drawing beside it print amounts one way, the way the screen they
// depict prints them. (The Pricing boards mixed "€15" and "€37.50" inside one column;
// the landing's sentence keeps its own "€90/month".) Integer cents throughout
// (CLAUDE.md §6).
func pricingFigures(price string, sizes []int, headcount int) (pages.PricingFigures, error) {
	var out pages.PricingFigures
	euros := func(cents int) string { return money(billing.NewMoney(int64(cents), billing.DefaultCurrency)) }
	for _, n := range sizes {
		c, err := monthlyCents(price, n)
		if err != nil {
			return pages.PricingFigures{}, err
		}
		out.Sizes = append(out.Sizes, pages.PricingSize{People: n, Monthly: strings.TrimPrefix(euros(c), "€")})
	}
	unit, err := monthlyCents(price, 1)
	if err != nil {
		return pages.PricingFigures{}, err
	}
	total, err := monthlyCents(price, headcount)
	if err != nil {
		return pages.PricingFigures{}, err
	}
	out.Receipt = pages.PricingReceipt{
		People: strconv.Itoa(headcount),
		Price:  euros(unit),
		Total:  euros(total),
	}
	return out, nil
}

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
// names from the non-test Go files under internal/ and cmd/ -- the string literals of the
// two name shapes it reads, `tappa_…` and `__Host-taptime_…` (cookieNameLiteral) -- and
// fails when one of those literals is a name this table does not mention (or that its
// named list of deliberate omissions does not). A cookie named in another shape is not
// read by it; that is code review's.
//
// TEN COOKIES (re-counted 2026-10-02, M10 OP-8 -- this line said "six" from before
// M7-02 and M7-04 added four). TestCookieNotice_ListsExactlyTheCookiesTheProductSets
// holds the table equal, in both directions, to the names of those two shapes it reads
// in the source, less the two operator cookies below.
//
// ⚠️ THE PLATFORM OPERATOR'S TWO COOKIES ARE LEFT OFF ON PURPOSE (OP-8): they are
// `__Host-` cookies set by the operator surface on the operator's host
// (TAPPA_OPERATOR_HOST) for Taptime staff, not for this site's visitors. The scan sees
// them and names the omission with its reason (marketing_test.go, cookiesNotOnTheNotice).
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
			Purpose: "Carries the invitation while the phone is being set up, so the code " +
				"stays out of the address bar, out of browser history and out of any link " +
				"that leaves this site. Once the privacy notice is accepted it also marks " +
				"this browser as the one that accepted it, so only this browser's first tap " +
				"on a plaque can finish setup. Cleared as soon as the invitation is used.",
			Lifetime: "until the invitation expires (at most " +
				humanSeconds(int(activationCookieCeiling/time.Second)) + ")",
			Scope: sessionCookiePath,
			Flags: flags,
		},
		{
			Name: activatedCookieName,
			Purpose: "Set for a few minutes right after a phone finishes setup, so the setup " +
				"page that is still open in another tab can tell that it was THIS setup that " +
				"finished. It holds no password and no code.",
			Lifetime: humanSeconds(activatedCookieMaxAge),
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
