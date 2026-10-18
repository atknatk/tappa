//go:build sandbox

package config_test

import "testing"

// fixtureEnv is the TAPPA_ENV setRequired gives every test in the sandbox build: the one value that
// build starts with (envForBuild).
const fixtureEnv = "sandbox"

// untaggedOnly skips a test that loads TAPPA_ENV=dev, staging or prod: the sandbox build refuses
// every one of them before anything else is read (TestLoad_TheBuildDecidesTheSandboxValue), so
// the test's subject is the untagged build's, and that build runs it.
func untaggedOnly(t *testing.T) {
	t.Helper()
	t.Skip("loads a TAPPA_ENV the sandbox build refuses; the untagged run covers it")
}
