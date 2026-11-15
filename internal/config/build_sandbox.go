//go:build sandbox

package config

// sandboxBuild is true in the sandbox build (`go build -tags sandbox`, make build-sandbox) -- the
// one binary that may start with TAPPA_ENV=sandbox, and may start with nothing else (envForBuild;
// ADR 0028 §11).
const sandboxBuild = true

// credentialWord is the sandbox build's environment word inside every credential: tt_<kind>_test_
// (internal/oauth: Env, ADR 0047 §7.1, §8.6).
const credentialWord = "test"
