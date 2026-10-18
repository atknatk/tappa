//go:build !sandbox

package config

// sandboxBuild is false in every binary built WITHOUT the sandbox tag -- the production build
// among them. Such a binary refuses TAPPA_ENV=sandbox (envForBuild; ADR 0028 §11).
const sandboxBuild = false
