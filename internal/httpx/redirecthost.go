package httpx

import (
	"net/http"
	"strings"
)

// THE REDIRECT HOSTS (backlog T71; user decision 2026-11-08). tappa.everva.com.tr (the deploy
// target through M8) and www.taptime.mt reach this process through
// deploy/k8s/43-redirect-ingress.yaml, and on them this middleware answers EVERY request with a
// permanent redirect to the base URL, keeping the path and the query. Before T71 the ingress
// answered them itself, and ingress-nginx's permanent-redirect cannot carry the request URI
// (43's header), so an old deep link landed on the home page.
//
// It answers before any route runs, so neither host serves a page, takes a tap or opens a
// session: the ingress header's "exactly one host serves the app" still holds, now because of
// this middleware and not because nginx stops the request.
//
// THE TARGET NEVER COMES FROM THE REQUEST. The scheme and host are the configured base URL
// (internal/config validates it as an origin with no path). From the request come only the path
// and the query, and the path is reduced so that it cannot move the redirect to another host:
//
//   - THE PATH IS THE ESCAPED PATH, NOT r.URL.RequestURI(). For a request line in opaque form
//     ("GET http:@evil.example HTTP/1.1", which net/http accepts) RequestURI returns the opaque
//     part, "@evil.example", and the base URL plus that string is
//     "https://taptime.mt@evil.example": a user part and a foreign host. EscapedPath is empty
//     there, and an empty path or one that does not start with "/" (OPTIONS "*", CONNECT)
//     becomes "/".
//   - LEADING SLASHES ARE CUT TO ONE. "//evil.example/x" after an absolute base URL is not
//     scheme-relative (the Location keeps its own host), but a path that starts with "//" is
//     exactly what a relative redirect further on would turn into another host, so it never
//     leaves here. A backslash cannot stand in for the slash: EscapedPath writes it as %5C.
//   - THE QUERY IS KEPT AS SENT (r.URL.RawQuery; "?" alone kept too). net/http has already
//     refused control bytes in the request target, so neither half can split the header, and
//     http.Redirect escapes what is not ASCII.
//
// 301 FOR GET AND HEAD, 308 FOR EVERY OTHER METHOD. A 301 lets a client re-send a POST as a
// GET -- browsers do, and the body is dropped without a word. 308 says the same thing ("moved
// for good") and forbids that change, so a write sent to an old host arrives at the canonical
// one as the same write, to meet that origin's own checks (the panel's same-origin gate refuses
// a cross-origin form). GET and HEAD keep 301 because that is the answer the web expects for a
// moved domain and the one these hosts gave before.
func redirectHosts(baseURL string, hosts []string) func(http.Handler) http.Handler {
	target := strings.TrimSuffix(baseURL, "/")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, h := range hosts {
				if OnHost(r, h) {
					code := http.StatusPermanentRedirect
					if r.Method == http.MethodGet || r.Method == http.MethodHead {
						code = http.StatusMovedPermanently
					}
					http.Redirect(w, r, target+redirectTarget(r), code)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// redirectTarget is the path and query a redirect host keeps (redirectHosts' doc).
func redirectTarget(r *http.Request) string {
	p := r.URL.EscapedPath()
	if !strings.HasPrefix(p, "/") {
		p = "/"
	}
	p = "/" + strings.TrimLeft(p, "/")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		p += "?" + r.URL.RawQuery
	}
	return p
}
