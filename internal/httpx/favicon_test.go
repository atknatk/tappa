package httpx_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// /favicon.ico (user decision, 2026-11-27; internal/httpx/static.go, FaviconPath): the
// embedded web/static/favicon.ico at the root, for the request a browser makes on its own
// when a page names no icon. No page names one, so no head, policy or golden moved.

const (
	faviconFile = "favicon.ico"
	faviconType = "image/x-icon"
)

// TestFavicon_ServesTheEmbeddedIconAtTheRoot drives the shipped router on the three host
// kinds it has: the customer host and the operator host serve the icon (both serve
// /static), the API host does not (it serves nothing outside /v1). Each row is one request
// and the whole answer it must get.
func TestFavicon_ServesTheEmbeddedIconAtTheRoot(t *testing.T) {
	const opHost, apiHost = "ops.example.test", "api.example.test"
	srv := httptest.NewServer(httpx.NewRouter(&config.Config{OperatorHost: opHost, APIHost: apiHost}, nil))
	t.Cleanup(srv.Close)
	etag, icon := strongETag(t, faviconFile)
	if httpx.FaviconPath != "/"+faviconFile {
		t.Fatalf("PREMISE: FaviconPath is %q; the browser asks for /%s", httpx.FaviconPath, faviconFile)
	}

	type body int
	const (
		noBody body = iota
		theIcon
		anyBody
	)
	tests := []struct {
		name        string
		method      string
		host        string
		path        string
		hdr         map[string]string
		wantCode    int
		wantBody    body
		wantType    string // "" = must not be image/x-icon
		wantNosniff bool
		wantCache   string
		wantETag    bool
	}{
		{name: "GET on the customer host", method: http.MethodGet, path: httpx.FaviconPath,
			wantCode: http.StatusOK, wantBody: theIcon, wantType: faviconType, wantNosniff: true, wantCache: wantRevalidate, wantETag: true},
		{name: "HEAD on the customer host: the headers, no body", method: http.MethodHead, path: httpx.FaviconPath,
			wantCode: http.StatusOK, wantBody: noBody, wantType: faviconType, wantNosniff: true, wantCache: wantRevalidate, wantETag: true},
		{name: "GET on the operator host", method: http.MethodGet, host: opHost, path: httpx.FaviconPath,
			wantCode: http.StatusOK, wantBody: theIcon, wantType: faviconType, wantNosniff: true, wantCache: wantRevalidate, wantETag: true},
		{name: "HEAD on the operator host", method: http.MethodHead, host: opHost, path: httpx.FaviconPath,
			wantCode: http.StatusOK, wantBody: noBody, wantType: faviconType, wantNosniff: true, wantCache: wantRevalidate, wantETag: true},
		// The same file under /static answers the same type: one file, one Content-Type.
		{name: "the same file under /static", method: http.MethodGet, path: "/static/" + faviconFile,
			wantCode: http.StatusOK, wantBody: theIcon, wantType: faviconType, wantNosniff: true, wantCache: wantRevalidate, wantETag: true},
		// The revalidation a browser sends with the validator it holds.
		{name: "a matching If-None-Match is a 304", method: http.MethodGet, path: httpx.FaviconPath,
			hdr: map[string]string{"If-None-Match": etag}, wantCode: http.StatusNotModified, wantBody: noBody,
			wantNosniff: true, wantCache: wantRevalidate, wantETag: true},
		// A refusal is not the file: no cache rule, no validator, no stated image type.
		{name: "a failed If-Match is a 412 that names no image", method: http.MethodGet, path: httpx.FaviconPath,
			hdr: map[string]string{"If-Match": `"0000"`}, wantCode: http.StatusPreconditionFailed, wantBody: noBody, wantNosniff: true},
		// Only the exact path: under and beside it is the router's 404.
		{name: "a path under it", method: http.MethodGet, path: httpx.FaviconPath + "/x",
			wantCode: http.StatusNotFound, wantBody: anyBody, wantNosniff: true},
		{name: "a look-alike name", method: http.MethodGet, path: httpx.FaviconPath + "x",
			wantCode: http.StatusNotFound, wantBody: anyBody, wantNosniff: true},
		// The API host serves nothing outside /v1, the icon included -- as with /static.
		{name: "not on the API host", method: http.MethodGet, host: apiHost, path: httpx.FaviconPath,
			wantCode: http.StatusNotFound, wantBody: anyBody, wantNosniff: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := staticDo(t, srv, tc.method, tc.path, tc.host, tc.hdr)
			if r.code != tc.wantCode {
				t.Fatalf("%s %s (host %q) = %d, want %d", tc.method, tc.path, tc.host, r.code, tc.wantCode)
			}
			switch tc.wantBody {
			case theIcon:
				if string(r.body) != string(icon) {
					t.Errorf("served %d bytes that are not the embedded %s (%d)", len(r.body), faviconFile, len(icon))
				}
			case noBody:
				if len(r.body) != 0 {
					t.Errorf("carried a body of %d bytes, want none", len(r.body))
				}
			}
			if got := r.header.Get("Content-Type"); (tc.wantType != "" && got != tc.wantType) || (tc.wantType == "" && got == faviconType) {
				t.Errorf("Content-Type = %q, want %q", got, tc.wantType)
			}
			if got := r.header.Get("X-Content-Type-Options"); (got == "nosniff") != tc.wantNosniff {
				t.Errorf("X-Content-Type-Options = %q, want nosniff %v", got, tc.wantNosniff)
			}
			if got := r.header.Get("Cache-Control"); got != tc.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tc.wantCache)
			}
			if got := r.header.Get("ETag"); (got == etag) != tc.wantETag || (!tc.wantETag && got != "") {
				t.Errorf("ETag = %q, want it %v (%s)", got, tc.wantETag, etag)
			}
		})
	}
}

// TestFavicon_AnswersEveryMethodAsStaticDoes: /favicon.ico is /static's handler under
// another pattern, so for every method -- the two reads and every other method chi knows --
// its answer is the answer GET/POST/... /static/favicon.ico gets, status, headers (less
// Date) and body. A route-level 405 or a method table of its own would turn this red.
// CONTROL: /static/js/tap.js gets the same status per method, so the reference is the asset
// route's convention and not something particular to the icon.
func TestFavicon_AnswersEveryMethodAsStaticDoes(t *testing.T) {
	srv := httptest.NewServer(httpx.NewRouter(&config.Config{}, nil))
	t.Cleanup(srv.Close)
	tests := []struct {
		name   string
		method string
	}{
		{"GET", http.MethodGet}, {"HEAD", http.MethodHead}, {"POST", http.MethodPost},
		{"PUT", http.MethodPut}, {"PATCH", http.MethodPatch}, {"DELETE", http.MethodDelete},
		{"OPTIONS", http.MethodOptions}, {"TRACE", http.MethodTrace},
	}
	withoutDate := func(h http.Header) http.Header {
		c := maps.Clone(h)
		delete(c, "Date")
		return c
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := staticDo(t, srv, tc.method, httpx.FaviconPath, "", nil)
			want := staticDo(t, srv, tc.method, "/static/"+faviconFile, "", nil)
			if got.code != want.code || string(got.body) != string(want.body) || !headerEqual(withoutDate(got.header), withoutDate(want.header)) {
				t.Errorf("%s %s = %d %v (%d bytes); %s /static/%s = %d %v (%d bytes)", tc.method, httpx.FaviconPath,
					got.code, got.header, len(got.body), tc.method, faviconFile, want.code, want.header, len(want.body))
			}
			if ctl := staticDo(t, srv, tc.method, "/static/js/tap.js", "", nil); ctl.code != want.code {
				t.Errorf("CONTROL: %s /static/js/tap.js = %d, /static/%s = %d", tc.method, ctl.code, faviconFile, want.code)
			}
		})
	}
}

func headerEqual(a, b http.Header) bool {
	return maps.EqualFunc(a, b, slices.Equal[[]string])
}
