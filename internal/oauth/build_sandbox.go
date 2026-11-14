//go:build sandbox

package oauth

// THE SANDBOX BUILD'S HALF (`go build -tags sandbox`, make build-sandbox; ADR 0047 §8.6, §10.4):
// tt_<kind>_test_ values, and the loopback redirect URI a developer's local client needs -- port
// included and matched byte for byte, like every registered URI.
const (
	// Env is the environment word inside every prefix: tt_<kind>_test_.
	Env = "test"
	// LoopbackRedirects: the sandbox build registers and redirects to a loopback URI.
	LoopbackRedirects = true
)
