package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// What ci.yml's job spends around go's own limits. The two first are that file's own sum; the two others
// are the worst `make check` step measured and what runs before it.
const (
	ciAroundTheSlowestPackage = 8*time.Minute + 15*time.Second
	ciSandboxStepOrdinary     = 8 * time.Minute
	ciBeforeMakeCheck         = 51 * time.Second
	ciMakeCheckOrdinary       = 25*time.Minute + 11*time.Second // run 41739040505, before T190; 17:25 on run 42214884680 after it
)

var (
	makeTestTimeoutRE    = regexp.MustCompile(`(?m)^test: require-db-env\n\tgo test -race -count=1 -timeout=(\d+)m \./\.\.\.$`)
	makeCoverTimeoutRE   = regexp.MustCompile(`(?m)^cover: require-db-env\n\tgo test -race -coverprofile=coverage\.out -timeout=(\d+)m \./\.\.\.$`)
	makeSandboxTimeoutRE = regexp.MustCompile(`(?m)^check-sandbox: require-db-env\n(?:\t.*\n)*?\tgo test -race -count=1 -timeout=(\d+)m -tags sandbox `)
	ciJobTimeoutRE       = regexp.MustCompile(`(?m)^\s*timeout-minutes: (\d+)\s*$`)
	yamlCommentBreakRE   = regexp.MustCompile(`\n\s*# `)
)

// ciTimeouts are the values, in minutes.
type ciTimeouts struct{ test, cover, sandbox, job int }

// parseCITimeouts reads the values and names any it cannot find.
func parseCITimeouts(makefile, ciYML string) (v ciTimeouts, bad []string) {
	minutes := func(re *regexp.Regexp, text, what string) int {
		m := re.FindStringSubmatch(text)
		if m == nil {
			bad = append(bad, what+" is not found in its expected shape")
			return 0
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			bad = append(bad, what+": "+err.Error())
		}
		return n
	}
	v.test = minutes(makeTestTimeoutRE, makefile, "`make test`'s -timeout")
	v.cover = minutes(makeCoverTimeoutRE, makefile, "`make cover`'s -timeout")
	v.sandbox = minutes(makeSandboxTimeoutRE, makefile, "`make check-sandbox`'s -timeout")
	if n := len(ciJobTimeoutRE.FindAllString(ciYML, -1)); n != 1 {
		bad = append(bad, fmt.Sprintf("ci.yml carries %d timeout-minutes lines, want the one job's", n))
	}
	v.job = minutes(ciJobTimeoutRE, ciYML, "ci.yml's timeout-minutes")
	return v, bad
}

// ciTimeoutViolations reads the two files' text. It is a pure function so the shapes it refuses can be
// driven below: the shipped values satisfy every rule, so the shipped files alone cannot show one firing.
func ciTimeoutViolations(makefile, ciYML string) []string {
	v, bad := parseCITimeouts(makefile, ciYML)
	if len(bad) > 0 {
		return bad
	}
	test, cover, sandbox, job := v.test, v.cover, v.sandbox, v.job

	// Go's goroutine dump naming the stuck test only reaches the log if go's alarm fires before the runner
	// cancels the job. A hang is one package at its limit, the other step in its ordinary time.
	jobLimit := time.Duration(job) * time.Minute
	if worst := time.Duration(test)*time.Minute + ciAroundTheSlowestPackage + ciSandboxStepOrdinary; worst >= jobLimit {
		bad = append(bad, fmt.Sprintf("a hang in `make test` is at %s and the job is cancelled at %s: go's dump never prints", worst, jobLimit))
	}
	if worst := ciBeforeMakeCheck + ciMakeCheckOrdinary + time.Duration(sandbox)*time.Minute; worst >= jobLimit {
		bad = append(bad, fmt.Sprintf("a hang in `make check-sandbox` is at %s and the job is cancelled at %s: go's dump never prints", worst, jobLimit))
	}
	if cover != test {
		bad = append(bad, fmt.Sprintf("`make cover` has -timeout=%dm and `make test` -timeout=%dm: the same suite with coverage on top", cover, test))
	}
	// The sum ci.yml sizes the job from is written out, so a value does not change without it.
	sum := fmt.Sprintf("8:15 + %d:00 = %d:15 at worst for `make check`, + ~8:00 for the sandbox step = ~%d:15, under %d.",
		test, test+8, test+16, job)
	if !strings.Contains(yamlCommentBreakRE.ReplaceAllString(ciYML, " "), sum) {
		bad = append(bad, "ci.yml's comment no longer states the sum these values give: "+sum)
	}
	return bad
}

// TestCITimeouts_TheJobOutlastsEveryGoAlarm binds the Makefile's -timeout values to ci.yml's job limit,
// which used to rest on a comment (T102). The table's edits are derived from the parsed values, so it
// pins the rules and not today's numbers: a change that keeps them, its written sum included, stays green.
func TestCITimeouts_TheJobOutlastsEveryGoAlarm(t *testing.T) {
	t.Parallel()
	read := func(rel ...string) string {
		raw, err := os.ReadFile(filepath.Join(append([]string{repoRoot}, rel...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	makefile, ciYML := read("Makefile"), read(".github", "workflows", "ci.yml")
	if bad := ciTimeoutViolations(makefile, ciYML); len(bad) > 0 {
		t.Fatalf("the shipped files: %v", bad)
	}

	v, bad := parseCITimeouts(makefile, ciYML)
	if len(bad) > 0 {
		t.Fatalf("the shipped files: %v", bad)
	}
	testAt := func(m int) string { return fmt.Sprintf("\tgo test -race -count=1 -timeout=%dm ./...", m) }
	coverAt := func(m int) string { return fmt.Sprintf("-coverprofile=coverage.out -timeout=%dm", m) }
	sandboxAt := func(m int) string { return fmt.Sprintf("-count=1 -timeout=%dm -tags sandbox", m) }
	jobAt := func(m int) string { return fmt.Sprintf("timeout-minutes: %d", m) }
	sumLine := fmt.Sprintf("    # %d:15 at worst for `make check`, + ~8:00 for the sandbox step = ~%d:15, under %d.",
		v.test+8, v.test+16, v.job)
	for _, tc := range []struct {
		name          string
		file          *string
		old, new, why string
	}{
		{"make test raised past the job", &makefile, testAt(v.test), testAt(v.job), "go's dump never prints"},
		{"make test raised, the sum left behind", &makefile, testAt(v.test), testAt(v.test + 1), "no longer states the sum"},
		{"make test loses its -timeout", &makefile, testAt(v.test), "\tgo test -race -count=1 ./...", "`make test`'s -timeout is not found"},
		{"the job lowered under the hang", &ciYML, jobAt(v.job), jobAt(v.test + 16), "go's dump never prints"},
		{"the job changed, the sum left behind", &ciYML, jobAt(v.job), jobAt(v.job + 5), "no longer states the sum"},
		{"the job's limit gone", &ciYML, jobAt(v.job), "", "0 timeout-minutes lines"},
		{"a second job's limit", &ciYML, jobAt(v.job), jobAt(v.job) + "\n    timeout-minutes: 5", "2 timeout-minutes lines"},
		{"the sum rewritten", &ciYML, sumLine, "    # about 40 minutes", "no longer states the sum"},
		{"check-sandbox raised past the job", &makefile, sandboxAt(v.sandbox), sandboxAt(v.job), "`make check-sandbox` is at"},
		{"check-sandbox loses its -timeout", &makefile, sandboxAt(v.sandbox), "-count=1 -tags sandbox", "`make check-sandbox`'s -timeout is not found"},
		{"cover back on go's default", &makefile, coverAt(v.cover), "-coverprofile=coverage.out", "`make cover`'s -timeout is not found"},
		{"cover drifts from test", &makefile, coverAt(v.cover), coverAt(v.test - 1), "the same suite with coverage on top"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edited := map[*string]string{&makefile: makefile, &ciYML: ciYML}
			edited[tc.file] = strings.Replace(*tc.file, tc.old, tc.new, 1)
			if edited[tc.file] == *tc.file {
				t.Fatalf("CONTROL FAILED: %q is not in the file", tc.old)
			}
			bad := strings.Join(ciTimeoutViolations(edited[&makefile], edited[&ciYML]), " | ")
			if !strings.Contains(bad, tc.why) {
				t.Fatalf("got %q, want a complaint containing %q", bad, tc.why)
			}
		})
	}
}
