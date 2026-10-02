package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// testCodePackages reports which of deps are test code: the standard library's testing
// packages and pgx's test helpers.
func testCodePackages(deps []string) []string {
	var out []string
	for _, p := range deps {
		if p == "testing" || strings.HasPrefix(p, "testing/") || p == "github.com/jackc/pgx/v5/pgxtest" {
			out = append(out, p)
		}
	}
	return out
}

// goListDeps is `go list -deps` of pkg in the PRODUCTION build: CGO_ENABLED=0 and no
// -race (Makefile's build target, which the Dockerfile runs), for linux/amd64 (2g, N-1:
// the image is built by `docker build` on the deploy workflow's ubuntu-latest runner from
// the golang bookworm image, with no --platform and no GOOS/GOARCH set -- the builder's own
// platform, linux/x64 as the Dockerfile itself says), not this machine's GOOS/GOARCH.
// Test files are never part of a package's dependency closure, so this is what the binary
// links.
func goListDeps(t *testing.T, pkg string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pkg)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Fields(string(out))
}

// TestBinary_LinksNoTestCode is a CLOSED structural rule (M10 OP-7, 2f): the production
// binary's dependency closure contains neither the testing package (nor any testing/...)
// nor github.com/jackc/pgx/v5/pgxtest. Test code does not ship; and pgxtest exported the
// list of every query exec mode that the 3rd closing auditor turned into a per-call
// simple_protocol without writing the mode's type (internal/db's wire file has the
// story), so no package linked into the production binary imports that list. Other
// binaries' closures (cmd/rotatekek) are outside this rule. Measured when written:
// neither is in the closure.
//
// POSITIVE CONTROL: the same check finds testing in pgxtest's own closure.
func TestBinary_LinksNoTestCode(t *testing.T) {
	if got := testCodePackages(goListDeps(t, "github.com/jackc/pgx/v5/pgxtest")); len(got) < 2 {
		t.Fatalf("POSITIVE CONTROL FAILED: pgxtest's closure shows %v; the check cannot see test code", got)
	}
	deps := goListDeps(t, "github.com/atknatk/tappa/cmd/tappa")
	if len(deps) < 100 {
		t.Fatalf("cmd/tappa's closure has %d packages; the listing has gone blind", len(deps))
	}
	if got := testCodePackages(deps); len(got) != 0 {
		t.Errorf("the production binary links test code: %v -- some product package imports it", got)
	}
}
