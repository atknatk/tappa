//go:build race

package operatorauth_test

// raceBuild is whether this test binary was built with -race: exactImports asks `go
// list` for the export data of the same build, so the cache this test was built into
// answers it.
const raceBuild = true
