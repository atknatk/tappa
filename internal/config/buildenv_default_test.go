//go:build !sandbox

package config_test

import "testing"

// fixtureEnv is the TAPPA_ENV setRequired gives every test: unset, the dev default of an untagged
// build.
const fixtureEnv = ""

// untaggedOnly marks a test that loads TAPPA_ENV=dev, staging or prod: an untagged build runs it.
func untaggedOnly(*testing.T) {}
