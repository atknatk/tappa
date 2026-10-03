//go:build race

package main

// raceBuild is whether this test binary was built with -race: the root-pool type scan
// (mailconfig_test.go) asks `go list` for the export data of the same build, so the
// cache this test was built into answers it (internal/db's race_on_test.go is the
// precedent).
const raceBuild = true
