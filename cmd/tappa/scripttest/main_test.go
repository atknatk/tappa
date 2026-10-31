package scripttest

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// repoRoot is resolved at package initialisation, before any test runs (cmd/tappa's shape).
var repoRoot = func() string {
	abs, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		panic("scripttest: resolving the repository root: " + err.Error())
	}
	return abs
}()

func TestMain(m *testing.M) {
	flag.Parse()
	startPrefetch()
	code := m.Run()
	stopPrefetch()
	os.Exit(code)
}
