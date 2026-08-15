// Package buildinfo reads the identity the Go toolchain stamps into a binary:
// which commit it was built from, when, and whether the tree was clean.
//
// 🔴 NOTHING HERE PUTS THE IDENTITY INTO THE BINARY, AND THAT IS THE POINT OF THE
// PACKAGE BEING THIS SMALL. `go build` does it by itself: -buildvcs defaults to
// auto and stamps a git checkout without being asked. Measured on the artifact the
// Makefile's `build` target produces (CGO_ENABLED=0 go build -trimpath
// -ldflags="-s -w"), 2026-08-15:
//
//	$ go version -m bin/tappa
//	build	-trimpath=true
//	build	CGO_ENABLED=0
//	build	vcs=git
//	build	vcs.revision=5a86f9b9bb5301ed0a5d5ed79cb533317c81785a
//	build	vcs.time=2026-08-15T01:12:42Z
//	build	vcs.modified=false
//
// ⚠️ AND -s -w DOES NOT STRIP IT, which is why this package exists instead of an
// -ldflags -X plumbing job. Those two flags drop the symbol table and DWARF; the
// build info is a separate blob that `go version -m` and debug.ReadBuildInfo both
// read out of the binary printed above. So the commit was ALREADY in every artifact
// this repository has ever produced — what was missing was anything that READ it.
// A hand-rolled -X ldflag would have been a SECOND version of the same fact, free to
// disagree with the one the toolchain records.
//
// WHAT "VERSION" MEANS HERE, measured rather than assumed: `git tag` in this
// repository lists NOTHING, so there is no semver to print. What the toolchain
// derives instead is a pseudo-version built from the commit date and hash —
// v0.0.0-20260815011242-5a86f9b9bb53 on the binary above — which is the honest
// answer to "which version is this?" for a product that has never cut a tag. The
// day somebody tags a release, Main.Version becomes that tag with no change here.
package buildinfo

import "runtime/debug"

// Unknown is what a field carries when the toolchain recorded nothing for it.
//
// It is a WORD rather than an empty string on purpose: these values are read by a
// person during an incident, and an empty log field ("revision=") reads as a bug in
// the logger, while "unknown" reads as the fact it is — this binary cannot say what
// it was built from. Both a `go build` with -buildvcs=false and a `go test` binary
// land here (measured: test binaries carry no vcs settings, which is why the test
// beside this file drives the pure half directly rather than asserting on its own
// process).
const Unknown = "unknown"

// Build is what the toolchain recorded about the running binary.
type Build struct {
	// Version is the main module's version: a tag when one exists, otherwise the
	// pseudo-version the toolchain derives from the commit.
	Version string
	// Revision is the full 40-character git revision, or Unknown.
	Revision string
	// Time is the commit timestamp in RFC 3339 (UTC), or Unknown. It is the COMMIT's
	// time, not the build's — two builds of the same commit carry the same value,
	// which is what makes it useful for answering "is this the deploy I made?".
	Time string
	// Go is the toolchain that compiled the binary, e.g. "go1.26.5".
	Go string
	// Modified is true when the working tree had uncommitted changes at build time.
	//
	// 🔴 IT IS THE FIELD THAT MATTERS MOST AND THE ONE EASIEST TO IGNORE. A binary
	// built from a dirty tree CANNOT be traced back to a commit: the revision it
	// carries names a state the binary is not. Nothing here refuses such a build —
	// every `make dev` run on a developer's machine is one, and refusing would make
	// the tool unusable — but cmd/tappa logs it at WARN rather than INFO so that a
	// production process saying it is untraceable is visibly different from one that
	// is not.
	Modified bool
	// Stamped is false when there was no VCS information at all.
	Stamped bool
}

// Read returns what the RUNNING binary was built from.
func Read() Build { return read(debug.ReadBuildInfo()) }

// read is the pure half, taking exactly what debug.ReadBuildInfo returns.
//
// Split out so the fallbacks are testable: a test binary is itself unstamped
// (measured), so a test that only called Read could never drive the stamped branch
// and would pass against a function that returned Unknown for everything.
func read(bi *debug.BuildInfo, ok bool) Build {
	b := Build{Version: Unknown, Revision: Unknown, Time: Unknown, Go: Unknown}
	if !ok || bi == nil {
		return b
	}
	if bi.GoVersion != "" {
		b.Go = bi.GoVersion
	}
	// "(devel)" is what the toolchain writes when it has nothing to derive a version
	// from. It is not a version, so it is not reported as one.
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		b.Version = v
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if s.Value != "" {
				b.Revision = s.Value
				b.Stamped = true
			}
		case "vcs.time":
			if s.Value != "" {
				b.Time = s.Value
			}
		case "vcs.modified":
			b.Modified = s.Value == "true"
		}
	}
	return b
}

// LogArgs is this identity as slog key/value pairs, in the order a human reads
// them.
//
// ⚠️ IT CARRIES NOTHING A DEPLOYMENT WOULD MIND AN OPERATOR SEEING, and that is a
// property of the type rather than of this method: a Build holds four strings and a
// bool that the ARTIFACT already publishes to anybody holding it (`go version -m`
// prints all of them). It is deliberately not a place to hang configuration —
// a database host, or any digest of key material, would turn a start-up line into
// a disclosure (CLAUDE.md §4.7).
func (b Build) LogArgs() []any {
	return []any{
		"version", b.Version,
		"revision", b.Revision,
		"committed", b.Time,
		"go", b.Go,
		"modified", b.Modified,
	}
}
