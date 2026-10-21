package httpx_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web"
)

// The cache rules of /static (2026-10-21, internal/httpx/static.go). Production
// measured every asset response with no Cache-Control, no ETag and no Last-Modified,
// so a browser fetched app.css and the six faces again on every navigation and the
// `font-display: swap` faces flashed the fallback each time.

const (
	wantImmutable  = "public, max-age=31536000, immutable"
	wantRevalidate = "no-cache"
	staticFont     = "fonts/space-grotesk-v22-latin-wght.woff2"
)

type staticResponse struct {
	code   int
	header http.Header
	body   []byte
}

func staticDo(t *testing.T, srv *httptest.Server, method, path, host string, hdr map[string]string) staticResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	if host != "" {
		req.Host = host
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	// No redirect following: a directory must not be answered by following one into a
	// file, and a redirect carrying these headers is itself a finding.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<21))
	if err != nil {
		t.Fatalf("%s %s: reading body: %v", method, path, err)
	}
	return staticResponse{res.StatusCode, res.Header, body}
}

// strongETag is the validator the handler must send for name: the quoted hex SHA-256
// of the embedded bytes, computed here independently of the handler.
func strongETag(t *testing.T, name string) (string, []byte) {
	t.Helper()
	b, err := fs.ReadFile(web.Static(), name)
	if err != nil {
		t.Fatalf("reading the embedded %s: %v", name, err)
	}
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:]) + `"`, b
}

// TestStatic_CacheHeadersAndConditionalRequests drives the real embedded tree through
// NewRouter -- on the customer host and on the operator host, which reaches the same
// route through operatorHostOnly's staticPrefix case.
func TestStatic_CacheHeadersAndConditionalRequests(t *testing.T) {
	const opHost = "ops.example.test"
	srv := httptest.NewServer(httpx.NewRouter(&config.Config{OperatorHost: opHost}, nil))
	t.Cleanup(srv.Close)

	type asset struct {
		name, cache, contentType string
	}
	assets := []asset{
		{"js/tap.js", wantRevalidate, "text/javascript; charset=utf-8"},
		{"vendor/htmx.min.js", wantRevalidate, "text/javascript; charset=utf-8"},
		{staticFont, wantImmutable, "font/woff2"},
		{"fonts/ibm-plex-mono-v20-latin-700.woff2", wantImmutable, "font/woff2"},
		// app.css is a build product (make css; gitignored, CI builds it before the
		// tests). It is the 132 KB the report was about, so it is asserted whenever
		// the embedded tree has it.
		{"css/app.css", wantRevalidate, "text/css; charset=utf-8"},
	}

	for _, host := range []string{"", opHost} {
		for _, a := range assets {
			t.Run(host+"/"+a.name, func(t *testing.T) {
				if _, err := fs.Stat(web.Static(), a.name); errors.Is(err, fs.ErrNotExist) && a.name == "css/app.css" {
					t.Skip("web/static/css/app.css is not built (make css) -- THIS IS NOT A PASS")
				}
				etag, bytes := strongETag(t, a.name)
				url := "/static/" + a.name

				for _, method := range []string{http.MethodGet, http.MethodHead} {
					r := staticDo(t, srv, method, url, host, nil)
					if r.code != http.StatusOK {
						t.Fatalf("%s %s = %d, want 200", method, url, r.code)
					}
					if got := r.header.Get("ETag"); got != etag {
						t.Errorf("%s %s: ETag = %q, want %q (quoted sha256 of the bytes)", method, url, got, etag)
					}
					if got := r.header.Get("Cache-Control"); got != a.cache {
						t.Errorf("%s %s: Cache-Control = %q, want %q", method, url, got, a.cache)
					}
					if got := r.header.Get("Content-Type"); got != a.contentType {
						t.Errorf("%s %s: Content-Type = %q, want %q (unchanged)", method, url, got, a.contentType)
					}
					if got := r.header.Get("Last-Modified"); got != "" {
						t.Errorf("%s %s: Last-Modified = %q; the embedded tree has no time to send", method, url, got)
					}
					if method == http.MethodGet && string(r.body) != string(bytes) {
						t.Errorf("GET %s served %d bytes that are not the embedded file (%d)", url, len(r.body), len(bytes))
					}
					if method == http.MethodHead && len(r.body) != 0 {
						t.Errorf("HEAD %s carried a body of %d bytes", url, len(r.body))
					}

					// The revalidation a browser sends: the validator it holds.
					c := staticDo(t, srv, method, url, host, map[string]string{"If-None-Match": etag})
					if c.code != http.StatusNotModified {
						t.Errorf("%s %s with If-None-Match: %s = %d, want 304", method, url, etag, c.code)
					}
					if len(c.body) != 0 {
						t.Errorf("%s %s 304 carried a body of %d bytes", method, url, len(c.body))
					}
					if got := c.header.Get("ETag"); got != etag {
						t.Errorf("%s %s 304: ETag = %q, want %q", method, url, got, etag)
					}
					if got := c.header.Get("Cache-Control"); got != a.cache {
						t.Errorf("%s %s 304: Cache-Control = %q, want %q", method, url, got, a.cache)
					}

					// CONTROL: a validator for other bytes is not a match, so the 304
					// above is the ETag comparison and not "always 304".
					stale := staticDo(t, srv, method, url, host, map[string]string{"If-None-Match": `"0000"`})
					if stale.code != http.StatusOK {
						t.Errorf("%s %s with a stale If-None-Match = %d, want 200", method, url, stale.code)
					}
				}
			})
		}

		// Directories are still refused, and the refusal carries no cache rule.
		for _, dir := range []string{"/static/", "/static/fonts/", "/static/fonts", "/static/js/"} {
			r := staticDo(t, srv, http.MethodGet, dir, host, nil)
			if r.code == http.StatusOK {
				t.Errorf("GET %s (host %q) = 200: directory listing is open", dir, host)
			}
			if strings.Contains(string(r.body), "woff2") {
				t.Errorf("GET %s (host %q) leaked a file name", dir, host)
			}
			if cc, et := r.header.Get("Cache-Control"), r.header.Get("ETag"); et != "" || cc == wantImmutable {
				t.Errorf("GET %s (host %q) = %d with Cache-Control %q, ETag %q; a refusal must not be cached as an asset", dir, host, r.code, cc, et)
			}
		}
	}

	// A missing file is a plain 404 with neither header.
	r := staticDo(t, srv, http.MethodGet, "/static/fonts/missing-v1-.woff2", "", nil)
	if r.code != http.StatusNotFound || r.header.Get("ETag") != "" || r.header.Get("Cache-Control") == wantImmutable {
		t.Errorf("GET a missing font = %d, ETag %q, Cache-Control %q; want 404 with no asset headers",
			r.code, r.header.Get("ETag"), r.header.Get("Cache-Control"))
	}
}

// versionedName is what makes a year of `immutable` safe: the upstream version in the
// file name (-v22-, -v20-; web/static/fonts/README.md), so new bytes arrive under a
// new URL.
var versionedName = regexp.MustCompile(`-v\d+-`)

// TestStaticImmutableRule_OnlyVersionedNames serves EVERY file in the embedded tree and
// reads the rule off the responses: each one carries an ETag and a Cache-Control, and
// each one served immutable has a versioned name. An unversioned font added under
// fonts/ would be pinned in browsers for a year under a URL whose bytes can change --
// rename it with its version (or narrow immutableAsset) instead.
func TestStaticImmutableRule_OnlyVersionedNames(t *testing.T) {
	// CONTROL: the pattern says what this test claims it says.
	if !versionedName.MatchString("space-grotesk-v22-latin-wght.woff2") || versionedName.MatchString("space-grotesk-latin-wght.woff2") {
		t.Fatal("CONTROL FAILED: the versioned-name pattern does not separate a versioned name from an unversioned one")
	}

	srv := httptest.NewServer(httpx.NewRouter(&config.Config{}, nil))
	t.Cleanup(srv.Close)

	immutable, total := 0, 0
	err := fs.WalkDir(web.Static(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		total++
		r := staticDo(t, srv, http.MethodHead, "/static/"+name, "", nil)
		if r.code != http.StatusOK {
			t.Errorf("HEAD /static/%s = %d, want 200", name, r.code)
			return nil
		}
		if r.header.Get("ETag") == "" {
			t.Errorf("/static/%s has no ETag; a browser cannot revalidate it", name)
		}
		switch cc := r.header.Get("Cache-Control"); cc {
		case wantImmutable:
			immutable++
			if !versionedName.MatchString(name) {
				t.Errorf("/static/%s is served %q but its name carries no version (-vNN-): "+
					"new bytes under this URL would stay stale for a year", name, cc)
			}
		case wantRevalidate:
		default:
			t.Errorf("/static/%s: Cache-Control = %q, want %q or %q", name, cc, wantImmutable, wantRevalidate)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded tree: %v", err)
	}
	// CONTROL: the rule is not vacuous -- the six faces are served immutable.
	if immutable < 6 {
		t.Errorf("%d of %d files served immutable; want at least the six woff2 faces", immutable, total)
	}
}
