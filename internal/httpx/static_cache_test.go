package httpx_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/layout"
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

		// Directories are still refused, and the refusal carries no cache rule. The last
		// row is a FILE name with a trailing slash: FileServer answers it with a redirect,
		// and a redirect pinned for a year under the immutable rule would outlive the file.
		for _, dir := range []string{"/static/", "/static/fonts/", "/static/fonts", "/static/js/", "/static/" + staticFont + "/"} {
			r := staticDo(t, srv, http.MethodGet, dir, host, nil)
			if r.code == http.StatusOK {
				t.Errorf("GET %s (host %q) = 200: directory listing is open", dir, host)
			}
			if strings.Contains(string(r.body), "woff2") {
				t.Errorf("GET %s (host %q) leaked a file name", dir, host)
			}
			if cc, et := r.header.Get("Cache-Control"), r.header.Get("ETag"); et != "" || cc != "" {
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

// assetVersion is the ?v= a page writes for name, recomputed here from the embedded
// bytes (the first 16 hex of the SHA-256) rather than read from web.Assets.
func assetVersion(t *testing.T, name string) string {
	t.Helper()
	etag, _ := strongETag(t, name)
	return strings.Trim(etag, `"`)[:16]
}

// TestStatic_VersionedURLsArePinnedOnlyWhenTheVersionMatches is the rule a page's
// versioned URL relies on (static.go): `?v=<this binary's version>` is pinned for a
// year, and every other query -- another deploy's version, an empty one, the full
// digest, an extra parameter -- is served this binary's bytes under revalidation,
// because pinning them under a URL that names other bytes would lock a browser to the
// wrong stylesheet for a year.
func TestStatic_VersionedURLsArePinnedOnlyWhenTheVersionMatches(t *testing.T) {
	const opHost = "ops.example.test"
	srv := httptest.NewServer(httpx.NewRouter(&config.Config{OperatorHost: opHost}, nil))
	t.Cleanup(srv.Close)

	names := []string{"css/app.css", "js/tap.js", "js/landing.js", "js/operator/enroll.js", "vendor/htmx.min.js", staticFont}
	tests := []struct {
		name  string
		query func(v, sum string) string // the raw query, with "?" when one is sent
		cache string
	}{
		{"matching version", func(v, _ string) string { return "?v=" + v }, wantImmutable},
		{"another deploy's version", func(string, string) string { return "?v=0123456789abcdef" }, wantRevalidate},
		{"the version in upper case", func(v, _ string) string { return "?v=" + strings.ToUpper(v) }, wantRevalidate},
		{"empty version", func(string, string) string { return "?v=" }, wantRevalidate},
		{"a bare question mark", func(string, string) string { return "?" }, wantRevalidate},
		{"the full digest", func(_, sum string) string { return "?v=" + sum }, wantRevalidate},
		{"a prefix of the version", func(v, _ string) string { return "?v=" + v[:8] }, wantRevalidate},
		{"the version plus a parameter", func(v, _ string) string { return "?v=" + v + "&x=1" }, wantRevalidate},
		{"the version twice", func(v, _ string) string { return "?v=" + v + "&v=" + v }, wantRevalidate},
		{"another parameter name", func(v, _ string) string { return "?ver=" + v }, wantRevalidate},
	}

	for _, host := range []string{"", opHost} {
		for _, name := range names {
			if _, err := fs.Stat(web.Static(), name); errors.Is(err, fs.ErrNotExist) && name == "css/app.css" {
				t.Run(host+"/"+name, func(t *testing.T) {
					t.Skip("web/static/css/app.css is not built (make css) -- THIS IS NOT A PASS")
				})
				continue
			}
			etag, bytes := strongETag(t, name)
			v := assetVersion(t, name)
			for _, tc := range tests {
				t.Run(host+"/"+name+"/"+tc.name, func(t *testing.T) {
					url := "/static/" + name + tc.query(v, strings.Trim(etag, `"`))
					for _, method := range []string{http.MethodGet, http.MethodHead} {
						r := staticDo(t, srv, method, url, host, nil)
						if r.code != http.StatusOK {
							t.Fatalf("%s %s = %d, want 200", method, url, r.code)
						}
						if got := r.header.Get("Cache-Control"); got != tc.cache {
							t.Errorf("%s %s: Cache-Control = %q, want %q", method, url, got, tc.cache)
						}
						if got := r.header.Get("ETag"); got != etag {
							t.Errorf("%s %s: ETag = %q, want %q", method, url, got, etag)
						}
						if method == http.MethodGet && string(r.body) != string(bytes) {
							t.Errorf("GET %s served %d bytes that are not the embedded file (%d)", url, len(r.body), len(bytes))
						}

						// The 304 flow is the same under either rule, and keeps the rule.
						c := staticDo(t, srv, method, url, host, map[string]string{"If-None-Match": etag})
						if c.code != http.StatusNotModified || len(c.body) != 0 {
							t.Errorf("%s %s with If-None-Match = %d (%d bytes), want 304 with no body", method, url, c.code, len(c.body))
						}
						if got := c.header.Get("Cache-Control"); got != tc.cache {
							t.Errorf("%s %s 304: Cache-Control = %q, want %q", method, url, got, tc.cache)
						}
						stale := staticDo(t, srv, method, url, host, map[string]string{"If-None-Match": `"0000"`})
						if stale.code != http.StatusOK {
							t.Errorf("%s %s with a stale If-None-Match = %d, want 200", method, url, stale.code)
						}
					}
				})
			}

			// A refusal under the matching version is not the file, so it carries neither
			// asset header -- above all not the year's pin. The 412 is the case
			// http.ServeContent does not clean up itself (static.go, assetStatus).
			t.Run(host+"/"+name+"/refusals under the matching version", func(t *testing.T) {
				url := "/static/" + name + "?v=" + v
				refusals := []struct {
					name string
					hdr  map[string]string
					code int
				}{
					{"range not satisfiable", map[string]string{"Range": "bytes=999999999-"}, http.StatusRequestedRangeNotSatisfiable},
					{"if-match other bytes", map[string]string{"If-Match": `"y"`}, http.StatusPreconditionFailed},
				}
				for _, rf := range refusals {
					r := staticDo(t, srv, http.MethodGet, url, host, rf.hdr)
					if r.code != rf.code {
						t.Errorf("GET %s, %s = %d, want %d", url, rf.name, r.code, rf.code)
					}
					if cc, et := r.header.Get("Cache-Control"), r.header.Get("ETag"); cc != "" || et != "" {
						t.Errorf("GET %s, %s = %d with Cache-Control %q, ETag %q; a refusal must not be cached as the asset", url, rf.name, r.code, cc, et)
					}
				}
				// CONTROL: the same preconditions satisfied answer the file, pinned -- so the
				// refusals above are the status rule, not headers missing everywhere.
				ok := staticDo(t, srv, http.MethodGet, url, host, map[string]string{"If-Match": etag})
				if ok.code != http.StatusOK || ok.header.Get("Cache-Control") != wantImmutable || ok.header.Get("ETag") != etag {
					t.Errorf("GET %s with If-Match: %s = %d, Cache-Control %q, ETag %q; want 200, %q, %q",
						url, etag, ok.code, ok.header.Get("Cache-Control"), ok.header.Get("ETag"), wantImmutable, etag)
				}
				part := staticDo(t, srv, http.MethodGet, url, host, map[string]string{"Range": "bytes=0-0"})
				if part.code != http.StatusPartialContent || part.header.Get("Cache-Control") != wantImmutable {
					t.Errorf("GET %s with Range: bytes=0-0 = %d, Cache-Control %q; want 206 and %q",
						url, part.code, part.header.Get("Cache-Control"), wantImmutable)
				}
			})
		}
	}

	// A version on a name the tree does not hold is a plain 404 with neither header.
	r := staticDo(t, srv, http.MethodGet, "/static/js/missing.js?v=0123456789abcdef", "", nil)
	if r.code != http.StatusNotFound || r.header.Get("ETag") != "" || r.header.Get("Cache-Control") == wantImmutable {
		t.Errorf("GET a missing versioned file = %d, ETag %q, Cache-Control %q; want 404 with no asset headers",
			r.code, r.header.Get("ETag"), r.header.Get("Cache-Control"))
	}
	// And a directory with a version is still refused, unpinned.
	r = staticDo(t, srv, http.MethodGet, "/static/js/?v=0123456789abcdef", "", nil)
	if r.code == http.StatusOK || r.header.Get("Cache-Control") != "" || r.header.Get("ETag") != "" {
		t.Errorf("GET a directory with a version = %d, Cache-Control %q, ETag %q; want a refusal with no asset headers",
			r.code, r.header.Get("Cache-Control"), r.header.Get("ETag"))
	}
}

// TestStatic_TheURLAPageWritesIsTheURLTheServerPins renders a real shell
// (layout.PageWithScript, the tap screen's) with the table the server installs and
// without it, and fetches what it names. With the table every /static URL in the head
// is versioned and pinned; without it the head is the bare URLs -- what every page test
// and byte golden reads.
func TestStatic_TheURLAPageWritesIsTheURLTheServerPins(t *testing.T) {
	assets, err := web.HashAssets(web.Static())
	if err != nil {
		t.Fatal(err)
	}
	page := func(versioned bool) string {
		t.Helper()
		ctx := t.Context()
		if versioned {
			ctx = web.WithAssets(ctx, assets)
		}
		var b strings.Builder
		if err := layout.PageWithScript("A page", "/static/js/tap.js", layout.Brand{}).Render(ctx, &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	srv := httptest.NewServer(httpx.NewRouter(&config.Config{}, nil))
	t.Cleanup(srv.Close)
	headURL := regexp.MustCompile(`<(?:link rel="stylesheet" href|script src)="(/static/[^"]+)"`)

	want := map[string]bool{"js/tap.js": true}
	if _, err := fs.Stat(web.Static(), "css/app.css"); err == nil {
		want["css/app.css"] = true
	} else {
		t.Log("web/static/css/app.css is not built (make css): the stylesheet's half is NOT asserted")
	}

	got := map[string]bool{}
	for _, m := range headURL.FindAllStringSubmatch(page(true), -1) {
		name, query, _ := strings.Cut(strings.TrimPrefix(m[1], "/static/"), "?")
		if !want[name] {
			continue
		}
		got[name] = true
		if query != "v="+assetVersion(t, name) {
			t.Errorf("the shell names %s; want ?v=%s", m[1], assetVersion(t, name))
		}
		if r := staticDo(t, srv, http.MethodGet, m[1], "", nil); r.code != http.StatusOK || r.header.Get("Cache-Control") != wantImmutable {
			t.Errorf("GET %s (the URL the shell writes) = %d, Cache-Control %q; want 200 and %q", m[1], r.code, r.header.Get("Cache-Control"), wantImmutable)
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("the shell rendered with the table names no /static/%s", name)
		}
	}

	bare := page(false)
	if strings.Contains(bare, "?v=") || !strings.Contains(bare, `<link rel="stylesheet" href="/static/css/app.css">`) ||
		!strings.Contains(bare, `<script src="/static/js/tap.js" defer>`) {
		t.Error("the shell rendered without the table does not name the bare URLs; the page tests and byte goldens would move with every CSS edit")
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
// rename it with its version (or narrow immutableName) instead. A ?v= URL is pinned for
// ANY file, so the walk also asks for each file under its version -- and under a wrong
// one, which must not be pinned.
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

		// Under its version every file is pinned -- the URL names the bytes -- and under
		// a version that is not its own, none is.
		v := assetVersion(t, name)
		if cc := staticDo(t, srv, http.MethodHead, "/static/"+name+"?v="+v, "", nil).header.Get("Cache-Control"); cc != wantImmutable {
			t.Errorf("/static/%s?v=%s: Cache-Control = %q, want %q", name, v, cc, wantImmutable)
		}
		wrong := "0000000000000000"
		if v == wrong {
			wrong = "ffffffffffffffff"
		}
		if cc := staticDo(t, srv, http.MethodHead, "/static/"+name+"?v="+wrong, "", nil).header.Get("Cache-Control"); cc != wantRevalidate {
			t.Errorf("/static/%s?v=%s (not its version): Cache-Control = %q, want %q", name, wrong, cc, wantRevalidate)
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

// readmeDigest is one line of the SHA-256 block in web/static/fonts/README.md
// (`sha256sum` output: 64 hex, two spaces, file name).
var readmeDigest = regexp.MustCompile(`(?m)^([0-9a-f]{64})  (\S+)$`)

// TestStaticImmutableRule_BytesMatchTheREADMEDigests closes what a name check cannot:
// new bytes under an existing versioned name would be pinned in browsers for a year.
// Every immutable file's embedded bytes must match its row in the fonts README, every
// immutable file must have a row, and every row must name a file that exists -- so
// changing a face means a new name AND a new row, and a stale row is red too.
func TestStaticImmutableRule_BytesMatchTheREADMEDigests(t *testing.T) {
	readme, err := fs.ReadFile(web.Static(), "fonts/README.md")
	if err != nil {
		t.Fatalf("reading the embedded fonts README: %v", err)
	}
	rows := map[string]string{}
	for _, m := range readmeDigest.FindAllStringSubmatch(string(readme), -1) {
		if _, dup := rows[m[2]]; dup {
			t.Errorf("fonts/README.md lists %s twice", m[2])
		}
		rows[m[2]] = m[1]
	}
	// CONTROL: the parser reads the table (six faces and two licence texts today).
	if len(rows) < 6 {
		t.Fatalf("parsed %d digest rows from fonts/README.md; the table moved or the parser is blind", len(rows))
	}

	for name, want := range rows {
		b, err := fs.ReadFile(web.Static(), path.Join("fonts", name))
		if err != nil {
			t.Errorf("fonts/README.md has a digest for %s, but the file is not in the tree: %v", name, err)
			continue
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("fonts/%s: sha256 %s, README says %s", name, got, want)
		}
	}

	srv := httptest.NewServer(httpx.NewRouter(&config.Config{}, nil))
	t.Cleanup(srv.Close)
	immutable := 0
	err = fs.WalkDir(web.Static(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		r := staticDo(t, srv, http.MethodHead, "/static/"+name, "", nil)
		if r.header.Get("Cache-Control") != wantImmutable {
			return nil
		}
		immutable++
		if path.Dir(name) != "fonts" {
			t.Errorf("/static/%s is served immutable outside fonts/, where no digest table covers it", name)
			return nil
		}
		if _, ok := rows[path.Base(name)]; !ok {
			t.Errorf("/static/%s is served immutable but fonts/README.md has no SHA-256 row for it", name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded tree: %v", err)
	}
	if immutable < 6 {
		t.Errorf("%d files served immutable; want at least the six woff2 faces", immutable)
	}
}
