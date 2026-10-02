//go:build race

package operator_test

// raceBuild is whether this test binary was built with -race: typedOperator asks `go
// list` for the export data of the same build, so the cache this test was built into
// answers it (internal/operatorauth's exactImports, the precedent).
const raceBuild = true
