package handler

// apiguidepage_test.go -- the integration guide on the product site (M11 DOCS-1, ADR 0030):
//
//	TestAPIGuidePages_AreTheGuideInTheMarketingChrome   /docs/api and /docs/api/tr: 200 HTML, indexable,
//	                                                    the title, the side navigation, the body, the
//	                                                    page's language; HEAD, caching, the policy, no
//	                                                    third party and every link resolving are the
//	                                                    marketing tests' (publicSurfaceURLs)
//	TestAPIGuidePages_LinkTheOtherLanguage              each page links the other
//	TestAPIGuidePages_OfferOpenAPIOnlyWithAnAPIHost     the download link is the API host's, or absent
//	TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel  the two footers, the panel's API keys screen
//	                                                    and its webhook section (to #webhooks)
//	TestAPIGuidePages_ServedOnTheProductHostOnly        404 on the operator host and on the API host

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	apidocs "github.com/atknatk/tappa/docs/api"
	"github.com/atknatk/tappa/internal/apiguide"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

func TestAPIGuidePages_AreTheGuideInTheMarketingChrome(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, c := range []struct {
		path, lang string
		src        []byte
	}{{pages.APIGuidePath, "en", apidocs.English()}, {pages.APIGuideTRPath, "tr", apidocs.Turkish()}} {
		rec := fetchMarketing(t, r, c.path, nil)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("GET %s: %d %q", c.path, rec.Code, rec.Header().Get("Content-Type"))
		}
		g, err := apiguide.Render(c.src, apiguide.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`<meta name="robots" content="index, follow">`,
			"<title>" + htmlText(g.Title) + " — Taptime</title>",
			"<h1 class=\"mt-2 font-display text-3xl font-bold tracking-tight\">" + htmlText(g.Title) + "</h1>",
			`<article class="px-4 py-10 sm:py-14" lang="` + c.lang + `">`,
			`<div class="guide-prose mt-8">`,
			`<h2 id="webhooks"><a href="#webhooks">`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s lacks %q", c.path, want)
			}
		}
		// The side navigation lists every anchored section but the guide's own contents list.
		nav := between(body, `<nav class="hidden lg:block"`, "</nav>")
		listed := 0
		for _, s := range g.Sections {
			link := `href="#` + s.ID + `"`
			if s.ID == apiguide.ContentsID {
				if strings.Contains(nav, link) {
					t.Errorf("GET %s: the side navigation lists the contents list itself", c.path)
				}
				continue
			}
			listed++
			if !strings.Contains(nav, link) {
				t.Errorf("GET %s: the side navigation lacks %s", c.path, link)
			}
		}
		if listed < 50 {
			t.Fatalf("GET %s: %d sections -- not the guide", c.path, listed)
		}
		// The body is the conversion's, byte for byte.
		var b bytes.Buffer
		if err := g.Body().Render(t.Context(), &b); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, b.String()) {
			t.Errorf("GET %s does not carry the converted guide", c.path)
		}
	}
}

// TestAPIGuidePages_LinkTheOtherLanguage. Mutant (measured): either link removed from the template
// -> red.
func TestAPIGuidePages_LinkTheOtherLanguage(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for path, want := range map[string]*regexp.Regexp{
		pages.APIGuidePath:   regexp.MustCompile(`<a href="/docs/api/tr" hreflang="tr" lang="tr"[^>]*>\s*Türkçe\s*</a>`),
		pages.APIGuideTRPath: regexp.MustCompile(`<a href="/docs/api" hreflang="en" lang="en"[^>]*>\s*English\s*</a>`),
	} {
		if !want.MatchString(mustFetchMarketing(t, r, path)) {
			t.Errorf("GET %s does not link the other language (%s)", path, want)
		}
	}
}

func TestAPIGuidePages_OfferOpenAPIOnlyWithAnAPIHost(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.DiscardHandler)
	with := httpx.NewRouter(nil, nil, NewMarketing(nil, log, "api.example.test"))
	without := marketingRouter(t)
	for _, path := range []string{pages.APIGuidePath, pages.APIGuideTRPath} {
		if body := mustFetchMarketing(t, with, path); !strings.Contains(body, `<a href="https://api.example.test/v1/openapi.yaml"`) {
			t.Errorf("GET %s with an API host does not link its openapi.yaml", path)
		}
		if body := mustFetchMarketing(t, without, path); strings.Contains(body, "openapi.yaml\"") || strings.Contains(body, "Download openapi") {
			t.Errorf("GET %s without an API host links an OpenAPI document", path)
		}
	}
}

func TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	footer := `href="` + pages.APIGuidePath + `"`
	for _, path := range []string{"/", pages.LegalPages[0].Path} {
		if body := mustFetchMarketing(t, r, path); !strings.Contains(between(body, "<footer", "</footer>"), footer) {
			t.Errorf("GET %s: the footer does not link the integration guide", path)
		}
	}
	var b bytes.Buffer
	if err := pages.AdminAPIKeys(pages.APIKeysView{}).Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	panel := b.String()
	if !strings.Contains(between(panel, "Integration keys</h2>", "</section>"), footer) {
		t.Error("the API keys screen does not link the integration guide")
	}
	if !strings.Contains(between(panel, `id="webhook"`, "</section>"), `href="`+pages.APIGuidePath+`#webhooks"`) {
		t.Error("the webhook section does not link the guide's webhook section")
	}
}

// TestAPIGuidePages_ServedOnTheProductHostOnly: the operator host serves the operator, and the API
// host serves /v1 and nothing else (the two host gates); the guide's HTML is the product host's.
func TestAPIGuidePages_ServedOnTheProductHostOnly(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{OperatorHost: "ops.example.test", APIHost: "api.example.test"}
	h := httpx.NewRouter(cfg, nil, NewMarketing(nil, slog.New(slog.DiscardHandler), cfg.APIHost))
	for _, c := range []struct {
		host string
		want int
	}{{"example.test", http.StatusOK}, {"ops.example.test", http.StatusNotFound}, {"api.example.test", http.StatusNotFound}} {
		for _, path := range []string{pages.APIGuidePath, pages.APIGuideTRPath} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = c.host
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Errorf("GET %s on %s: %d, want %d", path, c.host, rec.Code, c.want)
			}
		}
	}
}

// between returns s from the first from to the next to after it, or "".
func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], to)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}
