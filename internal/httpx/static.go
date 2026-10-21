package httpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
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
// Two rules, chosen by what the URL promises:
//
//   - fonts/*.woff2 -- cacheImmutable. The file names carry the upstream version
//     (-v22-, -v20-; web/static/fonts/README.md), so new bytes arrive under a new
//     name and the old URL never needs to change. A year with `immutable` means the
//     browser does not even revalidate, which is what removes the swap flash.
//     TestStaticImmutableRule_OnlyVersionedNames refuses an unversioned name under
//     this rule, because a pinned URL whose bytes later change stays stale for a year.
//   - everything else (app.css, the scripts, the vendored htmx) -- cacheRevalidate.
//     These keep their names across deploys, so they are cached but checked: one
//     conditional request per navigation, answered 304 with no body while the bytes
//     are the same, and the new bytes the moment a deploy changes them.
//
// `public` on the fonts is deliberate: they are the same bytes for everyone and carry
// no tenant data. taptime.mt is DNS-only on Cloudflare (deploy/k8s/40-ingress.yaml),
// so no shared cache sits in front today; if one did, caching these would be correct.
const (
	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "no-cache"
)

// immutableAsset reports whether the asset at name (a path relative to web/static,
// no leading slash) is served with cacheImmutable.
func immutableAsset(name string) bool {
	return strings.HasPrefix(name, "fonts/") && path.Ext(name) == ".woff2"
}

// staticFiles serves the embedded asset tree with a strong ETag and a Cache-Control
// on every file response. Directories are still refused by noListing.
//
// The ETag is the quoted hex SHA-256 of the file's bytes, computed once when the
// router is built. A pre-set ETag is what http.ServeContent (under http.FileServer)
// evaluates If-None-Match against, so a matching conditional GET or HEAD answers 304
// with no body -- and on an error response ServeContent deletes the ETag and
// Cache-Control again, so a 416 is never pinned. Last-Modified is not sent: the
// embedded files have no meaningful time, and the ETag is the stronger validator.
type staticFiles struct {
	next  http.Handler
	etags map[string]string // name relative to the tree root -> `"<hex sha256>"`
}

// newStaticFiles hashes every file under fsys and returns the handler. It is meant to
// see the request path with the route prefix already stripped (http.StripPrefix).
func newStaticFiles(fsys fs.FS) (*staticFiles, error) {
	etags := make(map[string]string)
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		etags[name] = `"` + hex.EncodeToString(sum[:]) + `"`
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("static: hashing assets: %w", err)
	}
	return &staticFiles{next: http.FileServer(noListing{http.FS(fsys)}), etags: etags}, nil
}

func (s *staticFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A trailing slash on a file name is answered by FileServer with a redirect, not
	// the file; the headers belong to the file's own response only.
	if p := r.URL.Path; !strings.HasSuffix(p, "/") {
		// FileServer serves path.Clean("/"+p), so the lookup uses the same name.
		name := strings.TrimPrefix(path.Clean("/"+p), "/")
		if tag, ok := s.etags[name]; ok {
			h := w.Header()
			h.Set("ETag", tag)
			if immutableAsset(name) {
				h.Set("Cache-Control", cacheImmutable)
			} else {
				h.Set("Cache-Control", cacheRevalidate)
			}
		}
	}
	s.next.ServeHTTP(w, r)
}
