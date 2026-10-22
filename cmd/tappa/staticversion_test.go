package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web"
)

// staticversion_test.go -- the versioned asset URLs (internal/httpx/static.go, 2026-10-21)
// end to end: the page the composed router renders names /static/...?v=<hash>, and that
// exact URL is answered with a year's `immutable` pin and the embedded bytes. The halves
// are tested apart elsewhere (the file server's rule in internal/httpx, the URL in web);
// this is the one place that proves the URL a page WRITES is the URL the server PINS.

// headAssetRE finds the stylesheet and script URLs a page's <head> names.
var headAssetRE = regexp.MustCompile(`<(?:link rel="stylesheet" href|script src)="(/static/[^"]+)"`)

// TestServer_PagesNameVersionedAssetsThatArePinned serves appRouter's composition under
// the BaseContext run() installs, renders the landing page, and fetches every /static
// URL its head names.
func TestServer_PagesNameVersionedAssetsThatArePinned(t *testing.T) {
	assets, err := web.HashAssets(web.Static())
	if err != nil {
		t.Fatal(err)
	}
	cfg := walkConfig(t, false)
	srv := httptest.NewUnstartedServer(buildAppFeatures(t, cfg, false).router(cfg))
	srv.Config.BaseContext = assetContext(assets)
	srv.Start()
	t.Cleanup(srv.Close)

	get := func(path string, hdr map[string]string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer res.Body.Close()
		b, err := io.ReadAll(io.LimitReader(res.Body, 1<<21))
		if err != nil {
			t.Fatalf("GET %s: reading body: %v", path, err)
		}
		return res, b
	}

	res, page := get("/", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", res.StatusCode)
	}
	urls := headAssetRE.FindAllStringSubmatch(string(page), -1)

	// What the head must name: the landing script always (it is committed), and app.css
	// whenever the tree carries it (a build product; CI builds it before the tests).
	want := []string{"js/landing.js"}
	if _, err := fs.Stat(web.Static(), "css/app.css"); err == nil {
		want = append(want, "css/app.css")
	} else if errors.Is(err, fs.ErrNotExist) {
		t.Log("web/static/css/app.css is not built (make css): the stylesheet's half is NOT asserted here")
	}

	seen := map[string]bool{}
	for _, m := range urls {
		u := m[1]
		name, query, _ := strings.Cut(strings.TrimPrefix(u, "/static/"), "?")
		b, err := fs.ReadFile(web.Static(), name)
		if err != nil {
			// A name the tree does not hold stays bare and is not ours to pin.
			continue
		}
		seen[name] = true
		// The version is recomputed here from the bytes, not read from web.Assets.
		sum := sha256.Sum256(b)
		if wantQuery := "v=" + hex.EncodeToString(sum[:])[:16]; query != wantQuery {
			t.Errorf("the page names %s; want the query %q (the first 16 hex of the file's sha256)", u, wantQuery)
			continue
		}

		r, body := get(u, nil)
		if r.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", u, r.StatusCode)
			continue
		}
		if cc := r.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s: Cache-Control = %q; the URL the page writes must be the URL the server pins", u, cc)
		}
		if string(body) != string(b) {
			t.Errorf("GET %s served %d bytes that are not the embedded file (%d)", u, len(body), len(b))
		}
		etag := `"` + hex.EncodeToString(sum[:]) + `"`
		if got := r.Header.Get("ETag"); got != etag {
			t.Errorf("GET %s: ETag = %q, want %q", u, got, etag)
		}
		// A browser that revalidates anyway (a forced reload) still gets a 304 under the pin.
		c, cbody := get(u, map[string]string{"If-None-Match": etag})
		if c.StatusCode != http.StatusNotModified || len(cbody) != 0 {
			t.Errorf("GET %s with If-None-Match = %d (%d bytes), want 304 with no body", u, c.StatusCode, len(cbody))
		}
		if cc := c.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s 304: Cache-Control = %q, want the pin", u, cc)
		}
	}
	for _, name := range want {
		if !seen[name] {
			t.Errorf("GET / names no versioned /static/%s in its head; found %q", name, urls)
		}
	}

	// CONTROL: the same composition WITHOUT the BaseContext renders the bare URLs, so the
	// versions above came from assetContext and not from somewhere a test cannot see.
	bare := httptest.NewServer(buildAppFeatures(t, cfg, false).router(cfg))
	t.Cleanup(bare.Close)
	r, err := bare.Client().Get(bare.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	bareBody, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bareBody), "?v=") || !strings.Contains(string(bareBody), `<script src="/static/js/landing.js" defer>`) {
		t.Error("without the asset context the landing page does not name the bare /static/js/landing.js; the control proves nothing")
	}
}

// TestServer_SetsTheAssetBaseContext holds run() to the wiring the test above drives:
// every http.Server literal in this package sets BaseContext to assetContext(...).
// Without it the pages write bare URLs -- nothing breaks, and every navigation goes back
// to spending a round trip on app.css, which no other test would notice.
func TestServer_SetsTheAssetBaseContext(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	fset := token.NewFileSet()
	servers := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Server" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "http" {
				return true
			}
			servers++
			found := false
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "BaseContext" {
					continue
				}
				if call, ok := kv.Value.(*ast.CallExpr); ok {
					if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "assetContext" {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("%s: this http.Server does not set BaseContext: assetContext(...); its pages would name unversioned assets", fset.Position(lit.Pos()))
			}
			return true
		})
	}
	// Anti-vacuity: the scan found the server it is about.
	if servers == 0 {
		t.Fatal("no http.Server literal found in cmd/tappa: the scan looked at nothing")
	}
}
