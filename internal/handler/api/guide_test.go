package api

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// TestGuide_IsServedUnderV1 (M11 API-12; the user's decision of 2026-10-16: the guide in the
// repository AND under /v1, served like openapi.yaml): through the shipped router,
//
//	GET /v1/guide.md      200, the English file's bytes, text/markdown; charset=utf-8,
//	GET /v1/guide.tr.md   Content-Language en / tr, Cache-Control no-cache, a strong ETag that is
//	                      the digest of the bytes -- WITHOUT a key, and a garbage Authorization header
//	                      changes nothing (the chain is not on these routes)
//	If-None-Match         the current ETag (also weak, also in a list, also "*") -> 304, no body;
//	                      another -> 200
//	the host gate         off the API host both are the router's own 404 (not the API's JSON)
//	the address bucket    openapi.yaml and the two guides spend ONE bucket per address: with a bucket
//	                      of two, the third document is 429 with Retry-After; another address is not
//	                      affected
//
// MUTANTS (measured, API-12 report): the guide mounted in the authenticated group -> 401 without a
// key; its own bucket -> the third request 200; the ETag check removed -> 200 on a match; no-store
// left from /v1's headers -> red.
func TestGuide_IsServedUnderV1(t *testing.T) {
	r := newRig(t, unknown, func(c *Config) { c.AddressLimit = 2 })
	files := map[string]string{"/v1/guide.md": "integration-guide.md", "/v1/guide.tr.md": "integration-guide.tr.md"}
	langs := map[string]string{"/v1/guide.md": "en", "/v1/guide.tr.md": "tr"}
	for path, file := range files {
		onDisk, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "api", file))
		if err != nil {
			t.Fatal(err)
		}
		fresh := newRig(t, unknown)
		w := fresh.do(http.MethodGet, path, "Bearer not-a-key", "198.51.100.7", nil)
		if w.Code != http.StatusOK || w.Body.String() != string(onDisk) {
			t.Fatalf("%s: %d, %d bytes; want 200 and the file's %d bytes", path, w.Code, w.Body.Len(), len(onDisk))
		}
		h := w.Header()
		if h.Get("Content-Type") != "text/markdown; charset=utf-8" || h.Get("Content-Language") != langs[path] ||
			h.Get("Cache-Control") != "no-cache" || h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: the headers %v", path, h)
		}
		etag := h.Get("ETag")
		want := newGuideDocument(onDisk, langs[path]).etag
		if etag != want || len(etag) != 34 {
			t.Errorf("%s: ETag %q, want %q", path, etag, want)
		}
		for _, inm := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
			w := fresh.do(http.MethodGet, path, "", "198.51.100.7", nil, "If-None-Match", inm)
			if w.Code != http.StatusNotModified || w.Body.Len() != 0 || w.Header().Get("ETag") != etag {
				t.Errorf("%s with If-None-Match %s: %d, %d bytes", path, inm, w.Code, w.Body.Len())
			}
		}
		if w := fresh.do(http.MethodGet, path, "", "198.51.100.7", nil, "If-None-Match", `"0000"`); w.Code != http.StatusOK {
			t.Errorf("%s with another ETag: %d", path, w.Code)
		}
	}
	if strings.Count(string(guideEnglish.body), "\n") < 400 || guideEnglish.etag == guideTurkish.etag {
		t.Fatal("anti-vacuity: the two guides are not two documents")
	}

	// One address bucket for the three documents.
	for i, path := range []string{"/v1/openapi.yaml", "/v1/guide.md", "/v1/guide.tr.md"} {
		w := r.do(http.MethodGet, path, "", "203.0.113.9", nil)
		if i < 2 && w.Code != http.StatusOK {
			t.Fatalf("%s (request %d): %d", path, i+1, w.Code)
		}
		if i == 2 && (w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "") {
			t.Fatalf("the third document from one address: %d %v, want 429 with Retry-After", w.Code, w.Header())
		}
	}
	if w := r.do(http.MethodGet, "/v1/guide.md", "", "203.0.113.10", nil); w.Code != http.StatusOK {
		t.Fatalf("another address: %d", w.Code)
	}

	// Off the API host: the router's own 404.
	h := httpx.NewRouter(&config.Config{APIHost: apiHost}, nil, customerStub{}, r.s)
	for path := range files {
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader("GET " + path + " HTTP/1.1\r\nHost: taptime.mt\r\n\r\n")))
		if err != nil {
			t.Fatal(err)
		}
		req.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound || w.Header().Get("Content-Type") == "application/json" {
			t.Errorf("%s on the main host: %d %q, want the router's 404", path, w.Code, w.Header().Get("Content-Type"))
		}
	}
}
