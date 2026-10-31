package scripttest

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

// prefetch_test.go (T185) -- ONE run of the real scripts/redline-check.sh over every shipped
// migration, started when the test process does and shared by the tests that read it.
//
// WHY. The scan is ~1 s of a mostly single-threaded bash per shipped migration; inside the
// sequential phase (TestRedline_T137_TheBodyWhitelistIsPinned, when it lived in cmd/tappa) it was
// 88 s of a 377 s package locally. Started by TestMain, it runs beside the sequential phase. The
// tree is scanned WHOLE, in one run, exactly as before -- the test reads that run's result.
//
// WHEN. Only if the run selects a reader (anySelected, the -run/-skip patterns read the way `go
// test` reads their first level); a misread pattern costs time, never a result. A scan started for
// a test that then does not run is cancelled after m.Run.
//
// ENVIRONMENT. Started before m.Run, so before any test could change the environment or the
// working directory; absolute paths only.
//
// anySelected is the same function as cmd/tappa's prefetch_test.go (a test package cannot import
// another's test files); a change to one is a change to both.

var prefetch struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// shippedScanReaders: the tests that read the shared scan. A test added later that reads it and is
// not listed only means a targeted run of it does not prefetch.
var shippedScanReaders = []string{"TestRedline_T137_TheBodyWhitelistIsPinned"}

func startPrefetch() {
	ctx, cancel := context.WithCancel(context.Background())
	prefetch.cancel = cancel
	if f := flag.Lookup("test.list"); f != nil && f.Value.String() != "" {
		return
	}
	if _, err := exec.LookPath("rg"); err != nil || !anySelected(shippedScanReaders) {
		return
	}
	prefetch.wg.Add(1)
	go func() {
		defer prefetch.wg.Done()
		shippedScan.once.Do(func() { shippedScan.scanTree(ctx) })
	}()
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
