package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PUBLIC 404 (LP-12, K4; ADR 0034). notfound.go says which unknown paths get the page
// and which keep the router's plain answer; these tests hold both halves on the marketing
// router, and cmd/tappa's TestAppRouter_UnknownPathsAnswerByHostAndPrefix and
// TestAppRouter_EveryNonPublicRouteIsUnderAPlainRoot hold them on the composed router and
// on the operator and API hosts.
//
// The 404 is not a row of pages.PublicPages and not in marketingURLs(): every scan over
// that list asks for a 200. It is scanned here instead, with the same scanners: the
// policy, no third party, every link resolving, the footer's two red lines, one <h1>,
// the chrome in its .lp root, no internal name, no unmeasured figure, no raw colour.

// publicNotFoundPaths are unknown paths on the public site: one per public root, a path
// below a public page, and one with a query. Each must get the page.
var publicNotFoundPaths = []string{
	"/no-such-page", "/pricing/", "/pricing/nothing", "/legal/no-such-document", "/for/nothing",
	"/docs/nothing", "/signup/nothing", "/product/x/y", "/security.html", "/x?y=1",
	"/administrator", "/terms", "/brandx", "/staticx", "/v2/x", "/tx",
}

// plainNotFoundPaths keep the router's plain answer: every root of plainNotFoundRoots, and
// the shapes that spell one without being routed into it.
var plainNotFoundPaths = []string{
	"/admin/no-such-screen", "/admin", "/admin/brand/logo/", "/t/no-such-thing", "/t/logo/",
	"/activate/no-such-step", "/api/no-such-endpoint", "/brand/no-such-asset",
	"/brand/theme/a/b", "/brand", "/static", "/operator/no-such-screen", "/v1/no-such-resource",
	"/healthz/x", "/readyz/x", "/dev/no-such-tool",
	"/admin/../no-such-page", "//admin/x", "/%61dmin/x", "/admin%2Fx", "/ADMIN/x", `/\admin/x`,
	"/x/../brand/y", "/Brand/theme/x",
}

// notFoundAnswer is one response as the recorder saw it.
type notFoundAnswer struct {
	status int
	header http.Header
	body   string
}

func notFoundDo(h http.Handler, method, target string) notFoundAnswer {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return notFoundAnswer{rec.Code, rec.Result().Header, rec.Body.String()}
}

func (a notFoundAnswer) same(b notFoundAnswer) bool {
	return a.status == b.status && reflect.DeepEqual(a.header, b.header) && a.body == b.body
}

// TestPublicNotFound_IsTheChromeAndOneSentence: every unknown public path answers 404
// with the page -- the shared chrome in the site's .lp root, the one sentence as the one
// <h1>, a link to / -- under the marketing policy, no-store, nosniff and noindex; and the
// page passes the public surface's scanners. Under both structural configurations.
func TestPublicNotFound_IsTheChromeAndOneSentence(t *testing.T) {
	t.Parallel()
	for _, c := range structuralConfigs(t) {
		for _, path := range publicNotFoundPaths {
			rec := fetchMarketing(t, c.r, path, nil)
			body := rec.Body.String()
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s: GET %s = %d, want 404", c.name, path, rec.Code)
				continue
			}
			for h, want := range map[string]string{
				"Content-Type":            "text/html; charset=utf-8",
				"Content-Security-Policy": marketingCSP,
				"Cache-Control":           "no-store",
				"X-Content-Type-Options":  "nosniff",
			} {
				if got := rec.Header().Get(h); got != want {
					t.Errorf("%s: GET %s %s = %q, want %q", c.name, path, h, got, want)
				}
			}
			for _, want := range []string{
				`<meta name="robots" content="noindex, nofollow">`,
				"<title>" + pages.NotFoundTitle + "</title>",
				`<a class="lp-button lp-button--green" href="/">` + pages.NotFoundHomeLabel + `</a>`,
				`<header class="lp-head"`, `<footer class="lp-foot">`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("%s: GET %s does not carry %q", c.name, path, want)
				}
			}
			if n := strings.Count(body, "<h1"); n != 1 || !strings.Contains(between(body, "<h1", "</h1>"), pages.NotFoundSentence) {
				t.Errorf("%s: GET %s: %d <h1>, or the one is not the sentence", c.name, path, n)
			}
			for _, p := range lpRootProblems(body, false) {
				t.Errorf("%s: GET %s: %s", c.name, path, p)
			}
			problems, followed, _ := pageLinkProblems(t, c, path, body)
			for _, p := range problems {
				t.Errorf("%s: %s", c.name, p)
			}
			if followed < len(pages.LegalPages)+1 {
				t.Errorf("%s: GET %s: followed %d link(s); the footer alone carries more", c.name, path, followed)
			}
			scanned, credits := withoutCreditHref(body)
			if credits != 2 {
				t.Errorf("%s: GET %s carries the credit href %d time(s), want 2 (the head's "+
					"rel=\"author\" and the footer's \"Built by\")", c.name, path, credits)
			}
			if hit := externalReference.FindString(scanned); hit != "" {
				t.Errorf("%s: GET %s reaches outside this origin: %q", c.name, path, hit)
			}
			lower := strings.ToLower(body)
			for _, forbidden := range []string{"<script", "<img", "<iframe", "<object", "<embed", "<form", " style="} {
				if strings.Contains(lower, forbidden) {
					t.Errorf("%s: GET %s renders %q", c.name, path, forbidden)
				}
			}
			footer := screenText(t, "<body>"+between(body, "<footer", "</footer>")+"</body>")
			for _, s := range []string{
				"Taptime collects no biometric data of any kind: no fingerprints, no face, no voice.",
				"A phone's position is read only at the moment somebody presses the button.",
			} {
				if !strings.Contains(footer, s) {
					t.Errorf("%s: GET %s: the footer does not say %q", c.name, path, s)
				}
			}
			text := screenText(t, body)
			if strings.Contains(text, "Tappa") {
				t.Errorf("%s: GET %s shows the internal code name", c.name, path)
			}
			if hit := unmeasuredNumber.FindString(text); hit != "" {
				t.Errorf("%s: GET %s claims %q", c.name, path, hit)
			}
			if hit := rawHexRE.FindString(body); hit != "" {
				t.Errorf("%s: GET %s carries the raw colour %q", c.name, path, hit)
			}
		}
	}
	src := repoFile(t, "web", "templates", "pages", "notfound.templ")
	if hit := rawHexRE.FindString(src); hit != "" {
		t.Errorf("notfound.templ carries the raw colour %q", hit)
	}
	// ONE SENTENCE (K4): the main column says the sentence and the way back, nothing else.
	body := mustFetchNotFound(t, marketingRouter(t), "/no-such-page")
	main := strings.Join(strings.Fields(screenText(t, "<body>"+between(body, `<main id="main">`, "</main>")+"</body>")), " ")
	if want := "404 " + pages.NotFoundSentence + " " + pages.NotFoundHomeLabel; main != want {
		t.Errorf("the 404's main column reads %q, want %q", main, want)
	}
	if strings.Count(pages.NotFoundSentence, ".") != 1 || !strings.HasSuffix(pages.NotFoundSentence, ".") {
		t.Errorf("NotFoundSentence %q is not one sentence", pages.NotFoundSentence)
	}
}

func mustFetchNotFound(t *testing.T, r http.Handler, path string) string {
	t.Helper()
	rec := fetchMarketing(t, r, path, nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), pages.NotFoundSentence) {
		t.Fatalf("PREMISE: GET %s = %d without the 404 page", path, rec.Code)
	}
	return rec.Body.String()
}

// TestPublicNotFound_TheSameBytesForEveryPathAndVisitor: the page reflects nothing -- two
// different unknown paths (one carrying a canary in its path and query) and two different
// visitors get the same body, and the canary is nowhere in it.
func TestPublicNotFound_TheSameBytesForEveryPathAndVisitor(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	const canary = "lp12canary"
	plain := mustFetchNotFound(t, r, "/no-such-page")
	loaded := fetchMarketing(t, r, "/"+canary+"/%3Cb%3E"+canary+"?q="+canary, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: adminauthCookieNameForTest, Value: "not-a-real-token"})
		req.AddCookie(&http.Cookie{Name: "tappa_session", Value: "also-not-real"})
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		req.Header.Set("Referer", "https://example.test/"+canary)
		req.RemoteAddr = "198.51.100.4:44444"
	})
	if loaded.Code != http.StatusNotFound {
		t.Fatalf("GET with the canary = %d, want 404", loaded.Code)
	}
	if loaded.Body.String() != plain {
		t.Error("two unknown paths, two visitors: the 404 page differs")
	}
	if strings.Contains(strings.ToLower(loaded.Body.String()), canary) {
		t.Error("the 404 page reflects the request")
	}
	if loaded.Header().Get("Set-Cookie") != "" {
		t.Errorf("the 404 page sets a cookie: %q", loaded.Header().Get("Set-Cookie"))
	}
}

// TestPublicNotFound_AnswersHeadWithNoBodyAndTheSameHeaders drives a real server (a
// recorder does not suppress a HEAD body; see TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders).
func TestPublicNotFound_AnswersHeadWithNoBodyAndTheSameHeaders(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(marketingRouter(t))
	defer srv.Close()
	do := func(method string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+"/no-such-page", nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		return res, b
	}
	get, gb := do(http.MethodGet)
	head, hb := do(http.MethodHead)
	if get.StatusCode != http.StatusNotFound || head.StatusCode != http.StatusNotFound {
		t.Fatalf("GET %d, HEAD %d; want 404 twice", get.StatusCode, head.StatusCode)
	}
	if len(hb) != 0 || !bytes.Contains(gb, []byte(pages.NotFoundSentence)) {
		t.Errorf("HEAD carried %d body bytes; GET carried the page = %v", len(hb), bytes.Contains(gb, []byte(pages.NotFoundSentence)))
	}
	for _, h := range []string{"Content-Type", "Cache-Control", "Content-Security-Policy", "X-Content-Type-Options"} {
		if g, hd := get.Header.Get(h), head.Header.Get(h); g != hd {
			t.Errorf("GET sends %s=%q and HEAD sends %q", h, g, hd)
		}
	}
}

// TestPublicNotFound_EveryOtherAnswerIsTheRoutersOwn: under every non-public root, on the
// shapes that spell one, and for every method but GET and HEAD on a public path, the
// marketing router answers exactly what the router answers with no Marketing mounted --
// net/http's plain 404, header for header and byte for byte. CONTROL: the comparison sees
// the page where the page is served.
func TestPublicNotFound_EveryOtherAnswerIsTheRoutersOwn(t *testing.T) {
	t.Parallel()
	bare := httpx.NewRouter(nil, nil)
	r := marketingRouter(t)
	check := func(method, path string) {
		t.Helper()
		want := notFoundDo(bare, method, path)
		if want.status != http.StatusNotFound || (method != http.MethodHead && want.body != "404 page not found\n") {
			t.Fatalf("PREMISE: the bare router answers %s %s with %d %q", method, path, want.status, want.body)
		}
		if got := notFoundDo(r, method, path); !got.same(want) {
			t.Errorf("%s %s: %d %v %q; the router's own answer is %d %v %q",
				method, path, got.status, got.header, got.body, want.status, want.header, want.body)
		}
	}
	for _, path := range plainNotFoundPaths {
		check(http.MethodGet, path)
		check(http.MethodHead, path)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		check(method, "/no-such-page")
		check(method, "/admin/no-such-screen")
	}
	// Every root of the list has a path above.
	for _, root := range plainNotFoundRoots {
		found := false
		for _, p := range plainNotFoundPaths {
			if firstPathSegment(p) == root {
				found = true
			}
		}
		if !found {
			t.Errorf("plainNotFoundRoots names %q and no path here probes it", root)
		}
	}
	// CONTROL: where the page is served, the two routers differ.
	if notFoundDo(r, http.MethodGet, "/no-such-page").same(notFoundDo(bare, http.MethodGet, "/no-such-page")) {
		t.Error("CONTROL: the comparison cannot tell the page from the router's answer")
	}
}

// TestPublicNotFound_ALeavingVisitorIsLoggedWithoutThePath: a failed render of the 404
// logs, as every page on this surface does, and never names the path that was asked for.
func TestPublicNotFound_ALeavingVisitorIsLoggedWithoutThePath(t *testing.T) {
	t.Parallel()
	const canary = "lp12canary"
	for _, err := range []error{context.Canceled, io.ErrClosedPipe} {
		var buf bytes.Buffer
		m := NewMarketing(nil, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), "", "")
		m.notFound(failingWriter{ResponseWriter: httptest.NewRecorder(), err: err},
			httptest.NewRequest(http.MethodGet, "/"+canary+"?q="+canary, nil))
		out := buf.String()
		if strings.TrimSpace(out) == "" {
			t.Errorf("%v: nothing was logged", err)
		}
		if strings.Contains(out, canary) {
			t.Errorf("%v: the log names the path: %s", err, out)
		}
	}
}
