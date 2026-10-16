package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	apidocs "github.com/atknatk/tappa/docs/api"
)

// THE INTEGRATION GUIDE (M11 API-12; the user's decision of 2026-10-16: English and Turkish, in the
// repository and under /v1). docs/api/integration-guide.md and its Turkish translation are embedded
// (docs/api's apidocs package) and served beside the OpenAPI document, the way it is served: on the
// API host only (the host gate), WITHOUT a key, metered by the client address alone (the address
// bucket openapi.yaml spends), with /v1's own response headers. They describe the contract and hold
// no business's data.
//
//	GET /v1/guide.md      the guide (English, the original)
//	GET /v1/guide.tr.md   the guide in Turkish
//
// THE NAMES: a path per language, not ?lang= and not Accept-Language. A path names its format like
// openapi.yaml does and its language like the files do (integration-guide.tr.md); a /v1 read refuses
// a parameter it does not name (readQuery -- the three documents too: they take none, so ?lang= and
// ?x=1 are 400, not the English guide), so ?lang= would be the one exception to that rule; and a
// cache keys a path without being told to vary on a header.
//
// CACHING: Cache-Control no-cache with a strong ETag (a digest of the embedded bytes). The guide
// changes with a deploy, so a copy is revalidated on every use; an unchanged guide answers 304 with
// no body. (The rest of /v1 is no-store: its answers name a business's data. These do not.)

// The guide's routes under Prefix.
const (
	guidePath   = "/guide.md"
	guideTRPath = "/guide.tr.md"
)

// guideContentType is RFC 7763's media type for Markdown.
const guideContentType = "text/markdown; charset=utf-8"

// guideDocument is one embedded guide and what its answer carries.
type guideDocument struct {
	body     []byte
	etag     string
	language string
}

func newGuideDocument(body []byte, language string) guideDocument {
	sum := sha256.Sum256(body)
	return guideDocument{body: body, etag: `"` + hex.EncodeToString(sum[:16]) + `"`, language: language}
}

var (
	guideEnglish = newGuideDocument(apidocs.English(), "en")
	guideTurkish = newGuideDocument(apidocs.Turkish(), "tr")
)

// serveGuide answers one guide: 200 with the bytes, or 304 when If-None-Match names its ETag.
func (s *Surface) serveGuide(g guideDocument) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, prob := readQuery(r, nil, problemNoQuery); prob != nil {
			writeProblem(w, prob)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-cache")
		h.Set("ETag", g.etag)
		h.Set("Content-Language", g.language)
		if ifNoneMatch(r.Header.Values("If-None-Match"), g.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		h.Set("Content-Type", guideContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(g.body)
	}
}

// ifNoneMatch reports whether an If-None-Match header names etag (RFC 9110 §13.1.2: a list of entity
// tags, weak comparison, or "*").
func ifNoneMatch(values []string, etag string) bool {
	for _, v := range values {
		for _, tag := range strings.Split(v, ",") {
			tag = strings.TrimPrefix(strings.TrimSpace(tag), "W/")
			if tag == "*" || tag == etag {
				return true
			}
		}
	}
	return false
}
