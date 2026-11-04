package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// raceOptOutPackages is the closed list of packages `make test` compiles without the race detector
// (the Makefile's RACE_OPT_OUT, with the measurement and what is given up). A pattern here takes the
// detector away from everything it matches, and CLAUDE.md section 8's concurrency proofs (the replay
// race, among others) are only proofs with the detector on: so a third entry, or a wider pattern, is a
// decision that is taken here, with its own measurement, and not by editing one Makefile line.
var raceOptOutPackages = []string{"golang.org/x/crypto/blowfish", "regexp/..."}

var raceOptOutEntry = regexp.MustCompile(`(\S*)=-race=(\S*)`)

// raceOptOutViolations reads a Makefile's text. It is a pure function so the negative controls below
// can drive it with the shapes it exists to refuse.
func raceOptOutViolations(makefile string) []string {
	var bad []string
	var defs int
	got := map[string]bool{}
	for line := range strings.SplitSeq(makefile, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		isDef := strings.HasPrefix(line, "RACE_OPT_OUT := ")
		if isDef {
			defs++
		}
		for _, m := range raceOptOutEntry.FindAllStringSubmatch(line, -1) {
			if !isDef {
				bad = append(bad, "the detector is switched off outside RACE_OPT_OUT: "+strings.TrimSpace(line))
				continue
			}
			got[strings.TrimPrefix(m[1], "-gcflags=")] = true
		}
	}
	if defs != 1 {
		bad = append(bad, "RACE_OPT_OUT is not defined exactly once")
	}
	if names := slices.Sorted(maps.Keys(got)); !slices.Equal(names, raceOptOutPackages) {
		bad = append(bad, "RACE_OPT_OUT names "+strings.Join(names, " ")+", the list is "+strings.Join(raceOptOutPackages, " "))
	}
	if !strings.Contains(makefile, "\ntest test-short cover: export GOFLAGS += $(RACE_OPT_OUT)\n") {
		bad = append(bad, "`make test` no longer takes RACE_OPT_OUT, and internal/handler goes back to the 30-minute limit")
	}
	if !regexp.MustCompile(`(?m)^test: require-db-env\n\tgo test -race `).MatchString(makefile) {
		bad = append(bad, "`make test` no longer runs with -race")
	}
	return bad
}

// TestMakeTest_TheRaceOptOutIsAClosedList holds the shipped Makefile to the list, and every shape below
// is the one edit that would quietly take the detector from this repository's own code.
func TestMakeTest_TheRaceOptOutIsAClosedList(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	shipped := string(raw)
	if bad := raceOptOutViolations(shipped); len(bad) > 0 {
		t.Fatalf("the shipped Makefile: %v", bad)
	}
	const def = "RACE_OPT_OUT := -gcflags=golang.org/x/crypto/blowfish=-race=false -gcflags=regexp/...=-race=false"
	if !strings.Contains(shipped, def+"\n") {
		t.Fatalf("CONTROL FAILED: the Makefile does not carry %q, so the shapes below edit nothing", def)
	}
	for _, tc := range []struct{ name, old, new, blame string }{
		{"every package", def, def + " -gcflags=all=-race=false", "RACE_OPT_OUT names"},
		{"this repository's packages", def, def + " -gcflags=github.com/atknatk/tappa/...=-race=false", "RACE_OPT_OUT names"},
		{"a wider pattern in place of one", "regexp/...=-race=false", "./...=-race=false", "RACE_OPT_OUT names"},
		{"one entry gone", " -gcflags=regexp/...=-race=false", "", "RACE_OPT_OUT names"},
		{"switched off on the test line", "go test -race -count=1 -timeout=30m", "go test -race -gcflags=all=-race=false -count=1 -timeout=30m", "outside RACE_OPT_OUT"},
		{"a second definition", def, def + "\n" + def, "exactly once"},
		{"the test target does not take it", "\ntest test-short cover: export GOFLAGS += $(RACE_OPT_OUT)\n", "\n", "no longer takes"},
		{"the test target drops -race", "go test -race -count=1 -timeout=30m", "go test -count=1 -timeout=30m", "no longer runs with -race"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edited := strings.Replace(shipped, tc.old, tc.new, 1)
			if edited == shipped {
				t.Fatalf("CONTROL FAILED: %q is not in the Makefile", tc.old)
			}
			bad := strings.Join(raceOptOutViolations(edited), " | ")
			if !strings.Contains(bad, tc.blame) {
				t.Fatalf("got %q, want a complaint containing %q", bad, tc.blame)
			}
		})
	}
}
