// Package scripttest holds cmd/tappa's tests that drive the repository's shell tooling --
// scripts/redline-check.sh, scripts/secretscan.sh and scripts/git-hooks/pre-push -- through real
// bash runs over temporary trees. It has no code of its own.
//
// WHY A PACKAGE OF ITS OWN (backlog T185): `go test -timeout` is per test binary, and these tests
// were most of cmd/tappa's CPU time -- forked bash, which -race does not slow down -- so with them
// cmd/tappa reached CI's 30-minute limit once. Apart, each half has the whole limit to itself, and
// `go test ./...` runs the two binaries side by side. Nothing here imports cmd/tappa (a main
// package cannot be imported) or any package of this module: what these tests read is files.
package scripttest
