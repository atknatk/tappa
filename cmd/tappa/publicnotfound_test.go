package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// publicnotfound_test.go -- the public 404 (LP-12, K4; ADR 0034) on the router appRouter
// composes, the one the binary serves. internal/handler's TestPublicNotFound_* hold the page
// and the plain answers on the marketing router alone; the brand theme tests
// (internal/handler/brandtheme_test.go) build a router WITHOUT Marketing, so they keep
// describing the refusal against the router's own 404 but no longer describe production.
// These tests are production's: every request below is answered by the full composition
// and compared with the same composition minus Marketing -- the composition whose unknown
// paths all answered net/http's plain 404 before this card.

// customerHost is the product host the requests below are addressed to.
const customerHost = "taptime.mt"

// wireAnswer is one response as the recorder saw it.
type wireAnswer struct {
	status int
	header http.Header
	body   string
}

func (a wireAnswer) same(b wireAnswer) bool {
	return a.status == b.status && reflect.DeepEqual(a.header, b.header) && a.body == b.body
}

func (a wireAnswer) isThePage() bool {
	return a.status == http.StatusNotFound && strings.Contains(a.body, pages.NotFoundSentence)
}

func answerOf(h http.Handler, method, host, path string) wireAnswer {
	req := httptest.NewRequest(method, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return wireAnswer{rec.Code, rec.Result().Header, rec.Body.String()}
}

// withoutMarketing is appRouter's composition with every feature but Marketing.
func (f appFeatures) withoutMarketing(cfg *config.Config) http.Handler {
	return httpx.NewRouter(cfg, nil, f.activation, f.tap, f.panel, f.logos, f.signupFlow, f.resetFlow,
		f.ready, f.theme, f.operator, f.api, f.devTools)
}

// publicRoutes is every "METHOD /pattern" the public site's two features mount: Marketing
// and the sign-up wizard.
func (f appFeatures) publicRoutes(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, m := range []httpx.Mounter{f.marketing, f.signupFlow} {
		r := chi.NewRouter()
		m.Mount(r)
		for _, mr := range walkOf(t, r) {
			out[mr] = true
		}
	}
	return out
}

// publicUnderThePanelRoot are the routes the sign-up wizard mounts under /admin (M14-17, ADR 0031
// §5.10): the registration's confirmation needs the panel cookie, whose Path is /admin
// (internal/handler/signupconfirm.go, "WHY UNDER /admin"). They are known routes, so no 404 is
// theirs; an unknown path beside them is the panel root's and keeps its plain answer, which this
// test goes on to probe. Counting them as public would call /admin a mixed root.
var publicUnderThePanelRoot = map[string]bool{
	"GET /admin/signup/confirm":  true,
	"POST /admin/signup/confirm": true,
}

// firstSegment is a route pattern's first path segment ("/admin/login" -> "admin").
func firstSegment(pattern string) string {
	p := strings.TrimPrefix(pattern, "/")
	if i := strings.Index(p, "/"); i >= 0 {
		p = p[:i]
	}
	return p
}

// TestAppRouter_EveryNonPublicRouteIsUnderAPlainRoot walks the composed router (the three
// optional surfaces off, and on) and takes the first segment of every route the public
// site does not mount -- the panel's, the tap's, activation's, the brand assets', the
// static tree's, the probes', the operator surface's, the API's, the dev tools'. Under
// each, an unrouted path, GET and HEAD, on the customer host, answers byte for byte what
// the composition without Marketing answers, and never the page; under each of the public
// site's own roots the same probe gets the page. So a surface mounted under a new prefix
// is red here until internal/handler's plainNotFoundRoots names it.
// CONTROL: the composition without Marketing answers the public probes plainly.
func TestAppRouter_EveryNonPublicRouteIsUnderAPlainRoot(t *testing.T) {
	for _, on := range []bool{false, true} {
		cfg := walkConfig(t, on)
		f := buildAppFeatures(t, cfg, on)
		full, before := f.router(cfg), f.withoutMarketing(cfg)
		public := f.publicRoutes(t)

		plainSegs, publicSegs := map[string]bool{}, map[string]bool{}
		for _, mr := range walkOf(t, full) {
			_, pattern, _ := strings.Cut(mr, " ")
			if public[mr] && !publicUnderThePanelRoot[mr] {
				publicSegs[firstSegment(pattern)] = true
				continue
			}
			plainSegs[firstSegment(pattern)] = true
		}
		want := []string{"activate", "admin", "api", "brand", "healthz", "operator", "readyz", "static", "t"}
		if on {
			want = append(want, "dev", "v1")
		}
		for _, s := range want {
			if !plainSegs[s] {
				t.Errorf("on=%v: the walk found no route under /%s; it is not reading the composition", on, s)
			}
		}
		for s := range plainSegs {
			if publicSegs[s] && s != "" {
				t.Errorf("on=%v: /%s holds both public and non-public routes; one answer cannot fit both", on, s)
			}
		}

		var segs []string
		for s := range plainSegs {
			segs = append(segs, s)
		}
		sort.Strings(segs)
		for _, s := range segs {
			path := "/" + s + "/lp12-unrouted"
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				got, was := answerOf(full, method, customerHost, path), answerOf(before, method, customerHost, path)
				if got.isThePage() || !got.same(was) {
					t.Errorf("on=%v: %s %s: %d %v %q; without Marketing %d %v %q",
						on, method, path, got.status, got.header, got.body, was.status, was.header, was.body)
				}
			}
		}
		for s := range publicSegs {
			path := "/" + s + "/lp12-unrouted"
			if s == "" {
				path = "/lp12-unrouted"
			}
			got := answerOf(full, http.MethodGet, customerHost, path)
			if !got.isThePage() || got.header.Get("Cache-Control") != "no-store" {
				t.Errorf("on=%v: GET %s: %d %q without the public 404 page", on, path, got.status, got.header.Get("Cache-Control"))
			}
			if was := answerOf(before, http.MethodGet, customerHost, path); was.status != http.StatusNotFound || was.body != "404 page not found\n" {
				t.Errorf("CONTROL on=%v: without Marketing GET %s = %d %q, want the plain 404", on, path, was.status, was.body)
			}
		}
		t.Logf("on=%v: plain roots %v; public roots %d", on, segs, len(publicSegs))
	}
}

// TestAppRouter_UnknownPathsAnswerByHostAndPrefix drives the composed router on its three
// hosts (with the optional surfaces on) and on the one host they have when off:
//
//   - the customer host: an unknown public path gets the page; an unknown path under the
//     brand, panel, tap, activation, API, operator and static prefixes answers what the
//     composition without Marketing answers; and an unknown theme file, a refused accent
//     and an unrouted path under /brand/ answer the same bytes -- net/http's plain 404,
//     the reference brandtheme_test.go measures the refusal against;
//   - the operator host and the API host: every one of those paths, public or not, answers
//     what it answered without Marketing, and never the page (ADR 0020 §4, ADR 0028 §13).
func TestAppRouter_UnknownPathsAnswerByHostAndPrefix(t *testing.T) {
	publicPaths := []string{"/no-such-page", "/legal/no-such-document", "/pricing/x", "/"}
	plainPaths := []string{
		"/brand/no-such-asset", "/brand/theme/no-such-file.css", "/brand/theme/808080.css",
		"/admin/no-such-screen", "/t/logo/", "/activate/no-such-step", "/api/no-such-endpoint",
		"/operator/no-such-screen", "/v1/no-such-resource", "/static/no-such-file.css", "/static",
	}
	plain := wireAnswer{http.StatusNotFound,
		http.Header{"Content-Type": {"text/plain; charset=utf-8"}, "X-Content-Type-Options": {"nosniff"}},
		"404 page not found\n"}
	for _, on := range []bool{false, true} {
		cfg := walkConfig(t, on)
		f := buildAppFeatures(t, cfg, on)
		full, before := f.router(cfg), f.withoutMarketing(cfg)

		for _, path := range publicPaths {
			if path == "/" {
				continue
			}
			if got := answerOf(full, http.MethodGet, customerHost, path); !got.isThePage() {
				t.Errorf("on=%v: customer host GET %s = %d without the page", on, path, got.status)
			}
		}
		for _, path := range plainPaths {
			got, was := answerOf(full, http.MethodGet, customerHost, path), answerOf(before, http.MethodGet, customerHost, path)
			if got.isThePage() || !got.same(was) {
				t.Errorf("on=%v: customer host GET %s: %d %v %q; without Marketing %d %v %q",
					on, path, got.status, got.header, got.body, was.status, was.header, was.body)
			}
		}
		for _, path := range []string{"/brand/no-such-asset", "/brand/theme/no-such-file.css", "/brand/theme/808080.css"} {
			if got := answerOf(full, http.MethodGet, customerHost, path); !got.same(plain) {
				t.Errorf("on=%v: customer host GET %s = %d %v %q; a probe of /brand/ sees one answer, %d %v %q",
					on, path, got.status, got.header, got.body, plain.status, plain.header, plain.body)
			}
		}
		if got := answerOf(full, http.MethodGet, customerHost, "/brand/theme/1F5C41.css"); got.status != http.StatusOK {
			t.Errorf("PREMISE on=%v: the theme route answers its own accent with %d", on, got.status)
		}
		if !on {
			continue
		}
		for _, host := range []string{cfg.OperatorHost, cfg.APIHost} {
			for _, path := range append(append([]string{}, publicPaths...), plainPaths...) {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					got, was := answerOf(full, method, host, path), answerOf(before, method, host, path)
					if got.isThePage() || strings.Contains(got.body, `class="lp-head"`) || !got.same(was) {
						t.Errorf("%s %s on %s: %d %v %q; without Marketing %d %v %q",
							method, path, host, got.status, got.header, got.body, was.status, was.header, was.body)
					}
				}
			}
		}
		// The operator host's unknown /operator path is the gate's plain 404 (the surface
		// is configured here), not the page.
		if got := answerOf(full, http.MethodGet, cfg.OperatorHost, "/operator/no-such-screen"); got.status != http.StatusNotFound || got.body != plain.body {
			t.Errorf("operator host GET /operator/no-such-screen = %d %q, want the plain 404", got.status, got.body)
		}
	}
}

// TestAppRouter_ThePublicNotFoundIsLoggedWithoutItsPath: the composed router's access
// record for a request the page answers carries the 404 and not the caller's path
// (TestAccessLog_UnroutedRequestStillGetsARecord's property, on the page's route).
func TestAppRouter_ThePublicNotFoundIsLoggedWithoutItsPath(t *testing.T) {
	cfg := walkConfig(t, false)
	f := buildAppFeatures(t, cfg, false)
	var buf bytes.Buffer
	h := appRouter(cfg, slog.New(slog.NewJSONHandler(&buf, nil)), f.activation, f.tap, f.panel, f.panel.InviteAcceptance(), f.logos, f.marketing,
		f.signupFlow, f.resetFlow, f.ready, f.theme, f.operator, f.api, f.devTools)
	if got := answerOf(h, http.MethodGet, customerHost, "/lp12canary/x?q=lp12canary"); !got.isThePage() {
		t.Fatalf("PREMISE: the canary path got %d without the page", got.status)
	}
	if strings.Contains(buf.String(), "lp12canary") {
		t.Errorf("the access record names the path: %s", buf.String())
	}
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("one access record expected: %v: %s", err, buf.String())
	}
	if status, _ := rec[httpx.LogStatusKey].(float64); int(status) != http.StatusNotFound {
		t.Errorf("the access record's status is %v, want 404", rec[httpx.LogStatusKey])
	}
}
