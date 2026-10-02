//go:build race

package db

// raceBuild is whether this test binary was built with -race: the exec-mode type scan
// asks `go list` for the export data of the same build, so the cache this test was built
// into answers it (internal/operatorauth's race_on_test.go is the precedent).
const raceBuild = true
