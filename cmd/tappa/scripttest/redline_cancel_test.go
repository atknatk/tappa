package scripttest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRedline_ACancelledScanRunsItsExitTrap (T185 audit, DUSUK): a scan redlineExec runs and its
// ctx cancels -- the shipped tree's prefetch, cancelled by TestMain when no reader ran -- ends
// through the script's own TERM trap, so its EXIT trap removes the temp files it made in TMPDIR.
// exec's default cancel is SIGKILL, which runs no trap and leaves them (measured: the mutant
// below).
//
// The cancel lands after the traps are set, by construction: an `rg` shim on PATH marks "ready"
// the first time it runs while BOTH of the script's mktemp files exist, and the script makes no
// `rg` call between those mktemps and its trap lines (have_rg's two calls come before them). The
// shim then holds that one call for 2 s, the window in which the test cancels -- and bash, which
// runs a trap only once its running child has ended, ends right after it.
//
// MUTANT (measured): cmd.Cancel removed from redlineExec (exec's SIGKILL) -- red, both mktemp
// files left in TMPDIR.
func TestRedline_ACancelledScanRunsItsExitTrap(t *testing.T) {
	t.Parallel()
	realRG, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("rg is not installed; redline-check needs it (CI installs it)")
	}
	shims, tmp, marks := t.TempDir(), t.TempDir(), t.TempDir()
	ready, held := filepath.Join(marks, "ready"), filepath.Join(marks, "held")
	shim := "#!/bin/sh\n" +
		"if ls \"$TMPDIR\"/tappa-redline-scan-err.* >/dev/null 2>&1 && ls \"$TMPDIR\"/tappa-redline-r7d-files.* >/dev/null 2>&1; then\n" +
		"  : > '" + ready + "'\n" +
		"  if [ ! -e '" + held + "' ]; then : > '" + held + "'; sleep 2; fi\n" +
		"fi\n" +
		"exec '" + realRG + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shims, "rg"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		out  string
		code int
		err  error
	}
	done := make(chan result, 1)
	work := t.TempDir()
	go func() {
		out, code, err := redlineExec(ctx, work, nil,
			"TMPDIR="+tmp, "PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"))
		done <- result{out, code, err}
	}()

	var r result
	for deadline := time.Now().Add(60 * time.Second); ; {
		select {
		case r = <-done:
			t.Fatalf("the scan ended (exit %d, %v) before it reached its first rg call past the traps:\n%s", r.code, r.err, r.out)
		default:
		}
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the scan never reached an rg call with both of its temp files made")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancelled := time.Now()
	cancel()
	select {
	case r = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the cancelled scan did not end")
	}
	t.Logf("the cancelled scan ended %v after the cancel", time.Since(cancelled).Round(time.Millisecond))
	if !errors.Is(r.err, context.Canceled) {
		t.Fatalf("want the cancel's error, got exit %d, %v:\n%s", r.code, r.err, r.out)
	}
	left, err := filepath.Glob(filepath.Join(tmp, "tappa-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		var names []string
		for _, l := range left {
			names = append(names, filepath.Base(l))
		}
		t.Fatalf("the cancelled scan left %d tappa-* temp entries in TMPDIR (its EXIT trap did not run): %s", len(left), strings.Join(names, " "))
	}
}
