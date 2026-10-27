package handler

import (
	"net/http"
	"path"
	"strings"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PUBLIC 404 (LP-12, user decision K4; ADR 0034).
//
// Until this card every path nothing was mounted on answered net/http's plain
// "404 page not found\n". Several answers in the product are built to be BYTE-EQUAL
// to that one, because their equality is what they protect:
//
//   - the tenant theme's refusal (/brand/theme/…) answers exactly what an unrouted
//     path answers, so an unknown or illegible accent cannot be told from no route at
//     all (ADR 0023 §4; brandtheme_test.go);
//   - the logo routes' paths of the wrong shape (/t/logo/, /admin/brand/logo/…/x) get
//     the router's own answer, which brandlogo_test.go pins as a fixed value (the logo
//     refusal itself is one answer of its own, ADR 0024);
//   - the operator host gate and the API host gate (internal/httpx operatorhost.go,
//     apihost.go) answer foreign paths with http.NotFound -- "the router's own 404" --
//     and the operator surface's hostGate does the same on the customer host
//     (ADR 0020 §4: on the main host the operator surface looks as if it did not exist).
//
// So the page is NOT the router's answer for every unknown path. Marketing.Mount binds
// notFound as the router's NotFound handler, and notFound answers with the page only
// when all of these hold, and with http.NotFound -- the same bytes as before -- when
// any does not:
//
//  1. the method is GET or HEAD. Nothing else on this surface reads a page;
//  2. the path's first segment is not one of plainNotFoundRoots below: the panel, the
//     tap and activation flows, their JSON endpoints, the brand assets, the static
//     tree, the operator surface, the integration API, the probes and the dev tools.
//     Those surfaces keep the plain answer under their whole prefix, so a path that
//     starts inside the panel never shows the public chrome, and a probe of /brand/…
//     sees one answer whatever comes after it.
//
// The HOST is not read here, and that is a property of where the request can come
// from rather than an omission: on the operator host operatorHostOnly answers every
// path but /operator… and /static/… before the router routes (and "/" with its
// redirect), on the API host apiHostOnly answers every path but /v1…, and those three
// prefixes are on the list. cmd/tappa's TestAppRouter_UnknownPathsAnswerByHostAndPrefix
// drives both hosts through the composed router.
//
// THE PAGE IS THE SAME BYTES FOR EVERY PATH AND EVERY VISITOR: it prints neither the
// path nor anything from the request, and the log line a failed render writes does not
// name the path (writePage, logPath "").
func (m *Marketing) notFound(w http.ResponseWriter, r *http.Request) {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || underPlainNotFoundRoot(r) {
		http.NotFound(w, r)
		return
	}
	m.writePage(w, r, http.StatusNotFound, notFoundCacheControl, marketingCSP, "",
		pages.NotFound(pages.NotFoundView{SignInHref: adminLoginPath, SignupHref: m.signupHref}))
}

// notFoundCacheControl is the 404 page's Cache-Control, and it is NOT the public pages'
// `public, max-age=300`. Those are a handful of fixed URLs, each worth keeping; the 404
// answers an unbounded set, and a shared cache that kept it would store one entry per
// address a scanner made up. It would also keep answering 404 for up to five minutes
// at an address a deploy has just given a page. The page costs a render of fixed
// components, no read and no lock, so not caching it costs nothing worth buying back.
const notFoundCacheControl = "no-store"

// plainNotFoundRoots are the first path segments under which an unknown path keeps
// http.NotFound's answer. Each is read off the constant its surface registers where
// one exists. TestAppRouter_EveryNonPublicRouteIsUnderAPlainRoot (cmd/tappa) walks the
// composed router and holds every route outside the public site to one of these, so a
// surface added under a new prefix is red there rather than answered with the page.
var plainNotFoundRoots = []string{
	firstPathSegment(adminLoginPath),       // admin: the panel, sign-in, reset, logos, webhooks
	firstPathSegment(TapPath),              // t: the tap page and the tap logo
	firstPathSegment(ActivationStatusPath), // activate: the wizard, its status, its logo
	firstPathSegment(apiCheckinPath),       // api: /api/checkin and /api/activate
	firstPathSegment(brandThemeRoute),      // brand: the theme stylesheets
	firstPathSegment(staticRoot),           // static: the embedded asset tree
	firstPathSegment(httpx.OperatorPrefix), // operator: the operator surface
	firstPathSegment(httpx.APIPrefix),      // v1: the integration API
	firstPathSegment(httpx.HealthPath),     // healthz
	firstPathSegment(httpx.ReadyPath),      // readyz
	firstPathSegment(DevSimulateTapPath),   // dev: the dev-only tap simulator
}

// apiCheckinPath is the tap's JSON endpoint (tap.go mounts it; /api/activate shares its
// root). staticRoot is the asset route's prefix (internal/httpx registers it; the
// constant there is not exported).
const (
	apiCheckinPath = "/api/checkin"
	staticRoot     = "/static/"
)

// underPlainNotFoundRoot reports whether r's path starts under one of plainNotFoundRoots.
//
// It reads the path three ways and answers yes if any of them does: the decoded path,
// the escaped path the router routes on (r.URL.RawPath, when the request carries one),
// and the decoded path cleaned of dot segments. Any leading run of slashes or
// backslashes is skipped and the comparison ignores case. So "/admin/../x",
// "//admin/x", "/%61dmin/x", "/admin%2Fx" and "/ADMIN/x" all keep the plain answer:
// the router routes none of them into the panel, but a path that SPELLS the panel's
// prefix is the one place a visitor must not see the public chrome (LP-12 boundary).
func underPlainNotFoundRoot(r *http.Request) bool {
	candidates := []string{r.URL.Path, path.Clean("/" + r.URL.Path)}
	if r.URL.RawPath != "" {
		candidates = append(candidates, r.URL.RawPath)
	}
	for _, p := range candidates {
		seg := firstPathSegment(p)
		for _, root := range plainNotFoundRoots {
			if strings.EqualFold(seg, root) {
				return true
			}
		}
	}
	return false
}

// firstPathSegment is p's first segment: leading slashes and backslashes skipped, up to
// the next one ("/admin/login" -> "admin", "/t" -> "t", "/" -> "").
func firstPathSegment(p string) string {
	p = strings.TrimLeft(p, `/\`)
	if i := strings.IndexAny(p, `/\`); i >= 0 {
		p = p[:i]
	}
	return p
}
