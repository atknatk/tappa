package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// raceOptOutDefinition is the one line that takes packages out of the race detector in `make test`
// (the Makefile's RACE_OPT_OUT, with the measurement and what is given up). A pattern there takes the
// detector away from everything it matches, and CLAUDE.md section 8's concurrency proofs (the replay
// race, among others) are only proofs with the detector on: so a third entry, a wider pattern or a
// compiler flag beside them is a decision that is taken here, with its own measurement, and not by
// editing one Makefile line.
const raceOptOutDefinition = "RACE_OPT_OUT := -gcflags=golang.org/x/crypto/blowfish=-race=false -gcflags=regexp/...=-race=false"

// raceOptOutUse is the one line that hands RACE_OPT_OUT to anything, and its targets are the three
// whose recipes raceOptOutRecipes pin. GOFLAGS rather than the go test line: the Makefile says why.
const raceOptOutUse = "test test-short cover: export GOFLAGS += $(RACE_OPT_OUT)"

// raceOptOutRecipes are the three targets that take it. Each must still run with the detector on.
var raceOptOutRecipes = []string{"test", "test-short", "cover"}

// raceOffAnywhere matches the detector being switched off or the compiler being reached, in any
// spelling this Makefile could carry: `-race=false` needs no `=` before it, so an indirect
// `NR := -race=false` and a quoted `-gcflags=all="-l -race=false"` are both found.
var raceOffAnywhere = regexp.MustCompile(`-race=|-gcflags`)

// raceOptOutViolations reads a Makefile's text. It is a pure function so the negative controls below
// can drive it with the shapes it exists to refuse.
func raceOptOutViolations(makefile string) []string {
	var bad []string
	var defs, uses int
	for line := range strings.SplitSeq(makefile, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(line, "RACE_OPT_OUT ") || strings.HasPrefix(line, "RACE_OPT_OUT:") || strings.HasPrefix(line, "RACE_OPT_OUT=") {
			defs++
			if line != raceOptOutDefinition {
				bad = append(bad, "RACE_OPT_OUT is not the closed list: "+line)
			}
			continue
		}
		if raceOffAnywhere.MatchString(line) {
			bad = append(bad, "the detector or the compiler is reached outside RACE_OPT_OUT: "+strings.TrimSpace(line))
		}
		if strings.Contains(line, "RACE_OPT_OUT") {
			uses++
			if line != raceOptOutUse {
				bad = append(bad, "RACE_OPT_OUT is used outside its one line: "+strings.TrimSpace(line))
			}
		}
	}
	if defs != 1 {
		bad = append(bad, "RACE_OPT_OUT is not defined exactly once")
	}
	if uses != 1 {
		bad = append(bad, "RACE_OPT_OUT is not used on exactly one line, and `make test` without it goes back to the 30-minute limit")
	}
	for _, target := range raceOptOutRecipes {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `: require-db-env\n\tgo test -race `)
		if !re.MatchString(makefile) {
			bad = append(bad, "`make "+target+"` no longer runs with -race")
		}
	}
	return bad
}

// TestMakeTest_TheRaceOptOutIsAClosedList holds the shipped Makefile to the list, and every shape below
// is one edit that would quietly take the detector from this repository's own code.
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
	const testLine = "go test -race -count=1 -timeout=30m"
	for _, tc := range []struct{ name, old, new, blame string }{
		{"every package", raceOptOutDefinition, raceOptOutDefinition + " -gcflags=all=-race=false", "not the closed list"},
		{"this repository's packages", raceOptOutDefinition, raceOptOutDefinition + " -gcflags=github.com/atknatk/tappa/...=-race=false", "not the closed list"},
		{"a wider pattern in place of one", "regexp/...=-race=false", "./...=-race=false", "not the closed list"},
		{"one entry gone", " -gcflags=regexp/...=-race=false", "", "not the closed list"},
		{"a quoted flag pair on the definition", raceOptOutDefinition, raceOptOutDefinition + ` -gcflags=all="-l -race=false"`, "not the closed list"},
		{"a recursive definition", raceOptOutDefinition, strings.Replace(raceOptOutDefinition, ":=", "=", 1), "not the closed list"},
		{"switched off on the test line", testLine, "go test -race -gcflags=all=-race=false -count=1 -timeout=30m", "outside RACE_OPT_OUT"},
		{"a quoted flag pair on the test line", testLine, `go test -race -gcflags=all="-l -race=false" -count=1 -timeout=30m`, "outside RACE_OPT_OUT"},
		{"an indirect variable", raceOptOutDefinition, raceOptOutDefinition + "\nNR := -race=false", "outside RACE_OPT_OUT"},
		{"a second definition", raceOptOutDefinition, raceOptOutDefinition + "\n" + raceOptOutDefinition, "exactly once"},
		{"the targets do not take it", "\n" + raceOptOutUse + "\n", "\n", "exactly one line"},
		{"another target takes it", "\n" + raceOptOutUse + "\n", "\n" + raceOptOutUse + "\nbuild: export GOFLAGS += $(RACE_OPT_OUT)\n", "used outside its one line"},
		{"the one line grows a target", raceOptOutUse, "test test-short cover build: export GOFLAGS += $(RACE_OPT_OUT)", "used outside its one line"},
		{"the test target drops -race", "\tgo test -race -count=1 -timeout=30m", "\tgo test -count=1 -timeout=30m", "`make test` no longer runs with -race"},
		{"test-short drops -race", "\tgo test -race -count=1 -short", "\tgo test -count=1 -short", "`make test-short` no longer runs with -race"},
		{"cover drops -race", "\tgo test -race -coverprofile", "\tgo test -coverprofile", "`make cover` no longer runs with -race"},
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
