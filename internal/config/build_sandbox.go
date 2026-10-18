//go:build sandbox

package config

// sandboxBuild is true in the sandbox build (`go build -tags sandbox`, make build-sandbox) -- the
// one binary that may start with TAPPA_ENV=sandbox, and may start with nothing else (envForBuild;
// ADR 0028 §11).
const sandboxBuild = true
