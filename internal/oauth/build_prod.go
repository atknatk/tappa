//go:build !sandbox

package oauth

// THE PRODUCTION BUILD'S HALF of the build split (ADR 0047 §7.1, §10.4; the API key's K-10 reason,
// cmd/tappa/nosandbox.go): every OAuth value this build issues and accepts carries "live", and a
// loopback redirect URI is refused at the operator's registration and at the authorization
// request's V2 -- whatever the registry row says (the schema CHECK takes both forms because one set
// of migrations runs under both builds).
const (
	// Env is the environment word inside every prefix: tt_<kind>_live_.
	Env = "live"
	// LoopbackRedirects is whether http://localhost:<port>/... and http://127.0.0.1:<port>/... are
	// redirect URIs this build registers and redirects to. Never in production.
	LoopbackRedirects = false
)
