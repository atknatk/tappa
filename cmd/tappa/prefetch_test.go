package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// prefetch_test.go (T185) — the package's three most expensive READ-ONLY fixtures start when the
// test process does, not when the first test that needs one reaches it.
//
// WHY. cmd/tappa is the slowest package of `make check` and ran into CI's 30-minute limit once
// (backlog T185). Its sequential phase -- every test that does not call t.Parallel, one after
// another -- was most of it, and three single things in it were the scan of every shipped migration
// by scripts/redline-check.sh (TestRedline_T137_TheBodyWhitelistIsPinned; ~1 s of a mostly
// single-threaded bash per migration, 83 of them) and the two builds of the command
// (theArtifact, sandboxArtifact). Each is computed once per process already (a sync.Once) and only
// READ by the tests; started here, each runs beside the sequential phase instead of inside it.
// What every test asserts is unchanged: it waits on the same Once and reads the same result.
//
// WHEN. Only for a fixture one of whose tests the run selects (selected: the -run/-skip patterns
// read the way `go test` reads their first level) -- a targeted `go test -run X` starts nothing it
// does not need. A misread pattern costs time, never a result: an unstarted fixture is computed by
// its first test, as before; a started one nobody uses is cancelled (the scan) or waited for (a
// build) before TestMain removes it.
//
// ENVIRONMENT. Started before m.Run, so before any t.Setenv or t.Chdir of a sequential test: the
// processes see the environment the process was started with (exec captures it at Start) and use
// absolute paths only.

var prefetch struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// The tests that read each fixture. A test added later that reads one and is not listed here only
// means a targeted run of it does not prefetch; a whole-package run (no -run) prefetches all three.
var (
	shippedScanReaders = []string{"TestRedline_T137_TheBodyWhitelistIsPinned"}
	artifactReaders    = []string{
		"TestArtifact_BootsInProductionWithTheShippedConfigMap", "TestArtifact_RefusesAnEmailDeliveryThisBuildLacks",
		"TestArtifact_ServesFromAnEmptyWorkingDirectory", "TestArtifact_SaysWhatItIsEVENWhenTheBootFails",
		"TestArtifact_BootsAndStopsWithTheResetEmailChannel", "TestPackaging_TheArtifactKnowsWhatItWasBuiltFrom",
		"TestPackaging_TheArtifactCarriesTheTimezoneDatabase", "TestConfig_UntaggedBinaryRefusesSandbox",
		"TestVerifyImage_RefusesAWrongBuildTag",
	}
	sandboxArtifactReaders = []string{"TestConfig_UntaggedBinaryRefusesSandbox", "TestVerifyImage_RefusesAWrongBuildTag"}
)

// startPrefetch is called by TestMain after flag.Parse and before m.Run; stopPrefetch after m.Run.
func startPrefetch() {
	ctx, cancel := context.WithCancel(context.Background())
	prefetch.cancel = cancel
	if f := flag.Lookup("test.list"); f != nil && f.Value.String() != "" {
		return
	}
	start := func(f func()) {
		prefetch.wg.Add(1)
		go func() { defer prefetch.wg.Done(); f() }()
	}
	if _, err := exec.LookPath("rg"); err == nil && anySelected(shippedScanReaders) {
		start(func() { shippedScan.once.Do(func() { shippedScan.scanTree(ctx) }) })
	}
	if anySelected(artifactReaders) {
		start(func() { buildOnce.Do(buildTheArtifact) })
	}
	if anySelected(sandboxArtifactReaders) {
		start(func() { sandboxBuildOnce.Do(buildTheSandboxArtifact) })
	}
}

func stopPrefetch() {
	prefetch.cancel()
	prefetch.wg.Wait()
}

// anySelected: would `go test` run one of these top-level tests? -run's and -skip's first
// '/'-separated element is matched against the name (a -skip with more elements skips subtests
// only). An unparseable pattern counts as "selected": the cost of a wrong guess is time.
func anySelected(names []string) bool {
	first := func(flagName string) (string, bool) {
		f := flag.Lookup(flagName)
		if f == nil {
			return "", false
		}
		p := f.Value.String()
		parts := strings.Split(p, "/")
		return parts[0], len(parts) == 1
	}
	runPat, _ := first("test.run")
	skipPat, skipTop := first("test.skip")
	var runRe, skipRe *regexp.Regexp
	if runPat != "" {
		runRe, _ = regexp.Compile(runPat)
	}
	if skipPat != "" && skipTop {
		skipRe, _ = regexp.Compile(skipPat)
	}
	for _, n := range names {
		if runRe != nil && !runRe.MatchString(n) {
			continue
		}
		if skipRe != nil && skipRe.MatchString(n) {
			continue
		}
		return true
	}
	return false
}

// shippedScan is ONE run of the real scripts/redline-check.sh over every shipped migration (the
// tree redlineRun builds, holding all of db/migrations/*.sql), shared by the tests that read it.
var shippedScan shippedScanResult

type shippedScanResult struct {
	once sync.Once
	tree map[string]string // "db/migrations/<name>" -> content, as scanned
	out  string
	code int
	err  error
}

// scanTree is called only inside s.once.Do. A cancelled ctx (TestMain after m.Run) ends the scan.
func (s *shippedScanResult) scanTree(ctx context.Context) {
	dir := filepath.Join(repoRoot, "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		s.err = err
		return
	}
	tree := map[string]string{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			s.err = err
			return
		}
		tree["db/migrations/"+e.Name()] = string(b)
	}
	work, err := os.MkdirTemp("", "redline-shipped-")
	if err != nil {
		s.err = err
		return
	}
	defer os.RemoveAll(work)
	s.tree = tree
	s.out, s.code, s.err = redlineExec(ctx, work, tree)
}
