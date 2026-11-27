package httpx

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/atknatk/tappa/web"
)

// The cache rules of /static (2026-10-21).
//
// 🔴 WHY THIS EXISTS: a user reported that moving between pages flashed "like a font
// loading", and production measured why. Every /static response -- app.css (132 KB),
// the six woff2 faces, the scripts -- carried no Cache-Control, no ETag and no
// Last-Modified. An embed.FS reports a zero ModTime, so http.FileServer has no
// validator to send, and without one a browser cannot revalidate; it fetched the
// stylesheet and the faces again on every navigation, and because the faces are
// `font-display: swap` each page first painted the fallback face and then swapped.
//
// The first fix (PR #4) pinned the faces and put everything else under `no-cache` with
// an ETag. That left one conditional request per navigation for app.css and the page's
// script, answered 304 -- and a stylesheet BLOCKS RENDER, so on a slow link the page
// still waited one round trip before it could paint. The rule now is a URL's promise:
//
//   - /static/<name>?v=<version> -- cacheImmutable, for ANY file, when the version is
//     the one web.Assets computes from the bytes this binary carries. The pages write
//     that URL (layout's documentHead through web.AssetURL), so a deploy that changes
//     app.css changes the URL every page names and the old pin is simply never asked
//     for again. A year with `immutable` means the browser does not revalidate.
//   - fonts/*.woff2 with no query -- cacheImmutable. The file names carry the upstream
//     version (-v22-, -v20-; web/static/fonts/README.md), so new bytes arrive under a
//     new name. TestStaticImmutableRule_OnlyVersionedNames refuses an unversioned name
//     under this rule, because a pinned URL whose bytes later change stays stale for
//     a year.
//   - everything else -- cacheRevalidate (`no-cache` + the ETag, answered 304 while the
//     bytes are the same). That is the bare URL (a page rendered by a binary without
//     the table, a page a browser kept from before this rule) AND A WRONG OR EXTRA
//     QUERY: ?v= naming another deploy's bytes is served this deploy's bytes, and
//     pinning them for a year under that URL would lock a browser to bytes that do not
//     match the HTML that names it -- during a rolling deploy (the page from an old
//     pod, the asset from a new one) and for good after a rollback brings that HTML
//     back. The version is compared exactly -- the whole raw query must be
//     `v=<version>` -- so there is one immutable URL per file, not one per spelling.
//
// The table reaches the pages through the request context, installed by cmd/tappa on
// its http.Server (assetContext) and not by NewRouter: a test that mounts NewRouter
// renders the bare URLs, so the page tests and byte goldens do not move with app.css.
//
// `public` is deliberate: these are the same bytes for everyone and carry no tenant
// data. taptime.mt is DNS-only on Cloudflare (deploy/k8s/40-ingress.yaml), so no
// shared cache sits in front today; if one did, caching these would be correct.
const (
	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "no-cache"
)

// FaviconPath is the one embedded asset served OUTSIDE /static (user decision, 2026-11-27).
//
// A browser asks for /favicon.ico on its own whenever a page names no icon, and no page
// here names one: a <link rel=icon> in layout's documentHead would change the head of the
// tap, activation and result screens (CLAUDE.md §9, their byte goldens), and every page's
// CSP falls back to default-src 'none' for images, which blocks such a link in Chrome
// (measured; docs/design/brand-assets/PLAN.md §5). The browser's own request is not a
// page load and names no policy, so answering it at the root gives every tab its icon
// with no page, policy or golden changing. Until this route the request reached the
// public 404 page on the customer host (a render and an access record per tab) and
// http.NotFound on the operator host.
//
// IT IS THE SAME FILE AND THE SAME HANDLER AS /static/favicon.ico, so it answers every
// method, every conditional request and every refusal exactly as the asset route does,
// and it is unmetered for the same reason: it touches no pool, takes no lock and writes
// nothing. Its cache rule is cacheRule's, unchanged: the bare URL -- the one a browser
// asks for -- is cacheRevalidate, since the name carries no version and a new icon must
// reach a browser that already holds the old one; /favicon.ico?v=<this binary's version>
// is cacheImmutable exactly as /static/favicon.ico?v=<version> is. No page writes that
// URL today.
const FaviconPath = "/favicon.ico"

// statedType is the Content-Type this server states for name rather than leaving it to
// http.ServeContent, or "" for none.
//
// .ico is the one entry, because ServeContent's answer for it is not ours to rely on: the
// standard library's own table says image/vnd.microsoft.icon, and mime reads the host's
// mime.types over that table, so the type would depend on the image the binary runs in.
// image/x-icon is the type browsers name it by and the one the MIME sniffing standard
// assigns these bytes. A stated type is sent with nosniff, so the browser takes it as
// stated.
func statedType(name string) string {
	if path.Ext(name) == ".ico" {
		return "image/x-icon"
	}
	return ""
}

// immutableName reports whether the asset at name (a path relative to web/static,
// no leading slash) is pinned by its name alone, with no ?v=.
func immutableName(name string) bool {
	return strings.HasPrefix(name, "fonts/") && path.Ext(name) == ".woff2"
}

// cacheRule is the Cache-Control for name requested with the raw query rawQuery
// (forceQuery: the URL ended in a bare "?").
func (s *staticFiles) cacheRule(name, rawQuery string, forceQuery bool) string {
	if rawQuery == "" && !forceQuery {
		if immutableName(name) {
			return cacheImmutable
		}
		return cacheRevalidate
	}
	if v, ok := s.assets.Version(name); ok && rawQuery == "v="+v {
		return cacheImmutable
	}
	return cacheRevalidate
}

// staticFiles serves the embedded asset tree with a strong ETag and a Cache-Control
// on every file response. Directories are still refused by noListing.
//
// The ETag is the quoted hex SHA-256 of the file's bytes, computed once when the
// router is built. A pre-set ETag is what http.ServeContent (under http.FileServer)
// evaluates If-None-Match against, so a matching conditional GET or HEAD answers 304
// with no body. On a refusal neither header survives: ServeContent deletes both on the
// errors it sends through serveError (a 416), and assetStatus below deletes them on every
// other non-2xx, non-304 status -- the 412 of a failed If-Match among them, which
// ServeContent writes directly and would otherwise send pinned. Last-Modified is not sent: the
// embedded files have no meaningful time, and the ETag is the stronger validator.
type staticFiles struct {
	next   http.Handler
	assets *web.Assets
}

// newStaticFiles hashes every file under fsys and returns the handler. It is meant to
// see the request path with the route prefix already stripped (http.StripPrefix).
func newStaticFiles(fsys fs.FS) (*staticFiles, error) {
	assets, err := web.HashAssets(fsys)
	if err != nil {
		return nil, fmt.Errorf("static: %w", err)
	}
	return &staticFiles{next: http.FileServer(noListing{http.FS(fsys)}), assets: assets}, nil
}

func (s *staticFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A trailing slash on a file name is answered by FileServer with a redirect, not
	// the file; the headers belong to the file's own response only.
	if p := r.URL.Path; !strings.HasSuffix(p, "/") {
		// FileServer serves path.Clean("/"+p), so the lookup uses the same name.
		name := strings.TrimPrefix(path.Clean("/"+p), "/")
		if sum, ok := s.assets.Sum(name); ok {
			h := w.Header()
			h.Set("ETag", `"`+sum+`"`)
			h.Set("Cache-Control", s.cacheRule(name, r.URL.RawQuery, r.URL.ForceQuery))
			stated := statedType(name)
			if stated != "" {
				// Set before ServeContent runs, which then uses it instead of deducing one.
				h.Set("Content-Type", stated)
				h.Set("X-Content-Type-Options", "nosniff")
			}
			w = &assetStatus{ResponseWriter: w, statedType: stated}
		}
	}
	s.next.ServeHTTP(w, r)
}

// assetStatus keeps the asset headers on the answers that ARE the file -- 2xx, and 304
// (the file, unchanged) -- and removes them from every other status before it is written.
//
// 🔴 http.ServeContent DOES NOT DO THIS FOR ALL OF ITS ERRORS, AND ONE OF THEM WAS PINNED.
// Its range failure (416) goes through serveError, which deletes ETag and Cache-Control;
// its precondition failure does not: `If-Match` naming other bytes on a versioned URL
// answered 412 carrying `public, max-age=31536000, immutable` (measured by the audit,
// 2026-10-21). A refusal is not the file and must not be cached as it. The ETag goes with
// Cache-Control, which is the contract serveError already applies to the 416.
// TestStatic_VersionedURLsArePinnedOnlyWhenTheVersionMatches holds both statuses.
//
// A stated Content-Type (statedType) goes with them on a refusal: it names the file, and a
// 412 written with no body would otherwise announce an image it does not carry. A type a
// refusal set for itself (http.Error's text/plain on the 416) is a different value and stays.
type assetStatus struct {
	http.ResponseWriter
	wroteHeader bool
	statedType  string
}

func (a *assetStatus) WriteHeader(code int) {
	// An informational status (103 Early Hints) is not the response: the final status
	// follows it, and that one decides the headers. 101 is final -- the connection
	// changes protocol -- and is treated like any other non-2xx.
	if code >= 100 && code <= 199 && code != http.StatusSwitchingProtocols {
		a.ResponseWriter.WriteHeader(code)
		return
	}
	if !a.wroteHeader {
		a.wroteHeader = true
		if (code < 200 || code > 299) && code != http.StatusNotModified {
			h := a.Header()
			h.Del("Cache-Control")
			h.Del("ETag")
			if a.statedType != "" && h.Get("Content-Type") == a.statedType {
				h.Del("Content-Type")
			}
		}
	}
	a.ResponseWriter.WriteHeader(code)
}

func (a *assetStatus) Write(b []byte) (int, error) {
	// An implicit header is a 200, which keeps the asset headers.
	a.wroteHeader = true
	return a.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the connection's writer (flush, deadlines).
func (a *assetStatus) Unwrap() http.ResponseWriter { return a.ResponseWriter }

// Flush keeps http.Flusher visible to a caller that type-asserts for it.
func (a *assetStatus) Flush() {
	if f, ok := a.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
