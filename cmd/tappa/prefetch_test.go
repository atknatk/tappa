package main

import (
	"flag"
	"regexp"
	"strings"
	"sync"
)

// prefetch_test.go (T185) -- the two builds of the command start when the test process does, not
// when the first test that needs one reaches it.
//
// WHY. cmd/tappa is one of the slowest packages of `make check` and ran into CI's 30-minute limit
// once (backlog T185). Its sequential phase -- every test that does not call t.Parallel, one after
// another -- held both builds (theArtifact, sandboxArtifact). Each is computed once per process
// already (a sync.Once) and only READ by the tests; started here, each runs beside the sequential
// phase instead of inside it. What every test asserts is unchanged: it waits on the same Once and
// reads the same result. (The shipped migrations' redline scan, the third such fixture, moved with
// the script tests to cmd/tappa/scripttest, which prefetches it the same way.)
//
// WHEN. Only for a fixture one of whose tests the run selects (anySelected: the -run/-skip
// patterns read the way `go test` reads their first level) -- a targeted `go test -run X` starts
// nothing it does not need. A misread pattern costs time, never a result: an unstarted build is
// run by its first test, as before; a started one nobody uses is waited for before TestMain
// removes it.
//
// ENVIRONMENT. Started before m.Run, so before any t.Setenv or t.Chdir of a sequential test: the
// builds see the environment the process was started with (exec captures it at Start) and use
// absolute paths only.

var prefetch struct {
	wg sync.WaitGroup
}

// The tests that read each build. A test added later that reads one and is not listed here only
// means a targeted run of it does not prefetch; a whole-package run (no -run) prefetches both.
var (
	artifactReaders = []string{
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
	if f := flag.Lookup("test.list"); f != nil && f.Value.String() != "" {
		return
	}
	start := func(f func()) {
		prefetch.wg.Add(1)
		go func() { defer prefetch.wg.Done(); f() }()
	}
	if anySelected(artifactReaders) {
		start(func() { buildOnce.Do(buildTheArtifact) })
	}
	if anySelected(sandboxArtifactReaders) {
		start(func() { sandboxBuildOnce.Do(buildTheSandboxArtifact) })
	}
}

func stopPrefetch() { prefetch.wg.Wait() }

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
