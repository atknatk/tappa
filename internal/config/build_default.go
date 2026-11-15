//go:build !sandbox

package config

// sandboxBuild is false in every binary built WITHOUT the sandbox tag -- the production build
// among them. Such a binary refuses TAPPA_ENV=sandbox (envForBuild; ADR 0028 §11).
const sandboxBuild = false

// credentialWord is the environment word inside every credential this build issues and accepts --
// the OAuth values' tt_<kind>_live_ (internal/oauth: Env, ADR 0047 §7.1). A COMPILE-TIME constant:
// no configuration or variable of a production binary can make it accept a sandbox credential, nor
// (LoopbackRedirects, from sandboxBuild) a loopback redirect URI.
const credentialWord = "live"
