package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestRedline_TheLexersReadTheSameUnderEveryAwk (T137 fix1, DÜŞÜK 8): redline's two SQL lexers
// (sql_lex, sql_scan) are awk programs, and the awk that runs them differs by machine -- macOS
// ships the one-true-awk (BSD), Ubuntu's base image mawk, and an `apt-get install gawk` makes
// gawk the `awk` (measured 2026-10-19: ubuntu:24.04 and golang:1.26 have mawk alone; macOS's
// /usr/bin/awk is version 20200816). A lexer that reads one byte differently under one of them is a
// redline that passes on a laptop and fails in CI, or the other way round. So the same inputs --
// every shipped migration and the shapes T137 measured (continuations across \r, \v, \f, E/U&
// strings, nested comments, named dollar quotes, bytes above 0x7f) -- go through both lexers under
// every awk this machine has, and every output must be byte-for-byte the first one's. CI installs
// gawk, mawk and original-awk (.github/workflows/ci.yml) so all three families are compared there;
// with fewer than two the test skips -- it is not a pass -- and in CI (CI set) fewer than three is red.
func TestRedline_TheLexersReadTheSameUnderEveryAwk(t *testing.T) {
	type impl struct {
		name, path string
		args       []string
	}
	var impls []impl
	seen := map[string]bool{}
	for _, c := range []impl{{name: "awk"}, {name: "gawk"}, {name: "mawk"}, {name: "original-awk"}, {name: "nawk"}, {name: "busybox", args: []string{"awk"}}} {
		p, err := exec.LookPath(c.name)
		if err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			real = p
		}
		key := real + " " + strings.Join(c.args, " ")
		if seen[key] {
			continue
		}
		seen[key] = true
		c.path = real
		impls = append(impls, c)
	}
	// In CI the step "Install ripgrep and three awks" puts gawk, mawk and original-awk on the
	// runner, so fewer than three there means the step changed or failed -- and a skip would let
	// the comparison silently stop being made. Red in CI, skipped (not passed) elsewhere.
	if os.Getenv("CI") != "" && len(impls) < 3 {
		t.Fatalf("CI is set and only %d awk implementation(s) were found (%v): the CI step installs gawk, mawk and "+
			"original-awk, so all three families must be compared here", len(impls), impls)
	}
	if len(impls) < 2 {
		t.Skipf("only %d awk implementation(s) on this machine (%v); nothing to compare -- CI installs three", len(impls), impls)
	}

	in := t.TempDir()
	migrations, err := filepath.Glob(filepath.Join(repoRoot, "db", "migrations", "*.sql"))
	if err != nil || len(migrations) < 50 {
		t.Fatalf("anti-vacuity: %d shipped migrations (%v)", len(migrations), err)
	}
	for _, m := range migrations {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(in, filepath.Base(m)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i, up := range []string{
		"SELECT E'a'\n'\\'';\nGRANT DELETE ON public.employees TO tappa_app;\nSELECT 'x';",
		"SELECT E'a'\r'\\'';\nSELECT 'b'\v\n\f'c';\nSELECT U&'d'\n  -- note\n'e';",
		"DO 'BEGIN EXEC'\n'UTE ''GRANT DELETE ON public.employees TO tappa_app''; END';",
		"SELECT pg_catalog.set_config('standard_' || 'conforming_strings', 'off', true);\nSELECT 'x\\'';",
		"/* a /* b */ it's */ SELECT $x$/*$x$, 1 AS \"*/\", 1 AS a$$, E'\\\\', 'caf\xc3\xa9', U&\"d\\0061t\";",
		"DO $body$ BEGIN EXECUTE $q$GRANT DELETE ON public.transactions TO tappa_app$q$; END $body$;",
		"COMMENT ON FUNCTION public.zz_f() IS $$Do not drop or alter this.$$;\nSET LOCAL NAMES 'SJIS';",
		"SELECT 'unterminated\nGRANT DELETE ON public.transactions TO tappa_app;",
	} {
		body := t137Migration(up)
		if err := os.WriteFile(filepath.Join(in, "99"+string(rune('a'+i))+"_case.sql"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(in, "*.sql"))
	if err != nil {
		t.Fatal(err)
	}

	// Both lexers are lifted out of the script exactly as it defines them (the same extraction a
	// reviewer runs by hand), so the test reads the code that ships, not a copy of it.
	const driver = `set -euo pipefail
script=$1; shift
eval "$(awk '/^sql_lex\(\) \{/{f=1} f{print} f&&/^\}/{f=0}' "$script")"
eval "$(awk '/^sql_scan\(\) \{/{f=1} f{print} f&&/^\}/{f=0}' "$script")"
for f in "$@"; do printf '== %s\n' "${f##*/}"; sql_lex <"$f"; echo '-- scan'; sql_scan "$f"; done`
	// Every awk reads the inputs at the same time (T139): the slowest (the one-true-awk family, ~8 s
	// here against ~1-2 s for mawk and gawk) sets the wall time instead of the sum.
	type result struct {
		out    []byte
		err    error
		stderr string
	}
	results := make([]result, len(impls))
	shims := make([]string, len(impls))
	for i, im := range impls {
		shims[i] = t.TempDir()
		args := ""
		for _, a := range im.args {
			args += " " + a
		}
		if err := os.WriteFile(filepath.Join(shims[i], "awk"), []byte("#!/bin/sh\nexec "+im.path+args+" \"$@\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := range impls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command("bash", append([]string{"-c", driver, "driver", filepath.Join(repoRoot, "scripts", "redline-check.sh")}, files...)...)
			cmd.Env = append(os.Environ(), "PATH="+shims[i]+string(os.PathListSeparator)+os.Getenv("PATH"))
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			results[i] = result{out: out, err: err, stderr: stderr.String()}
		}()
	}
	wg.Wait()

	var first []byte
	for i, im := range impls {
		out, err := results[i].out, results[i].err
		if err != nil {
			t.Fatalf("%s: %v\n%s", im.name, err, results[i].stderr)
		}
		if !bytes.Contains(out, []byte("\nbody\tquoted\t")) || !bytes.Contains(out, []byte("\nsetcfg\t!")) {
			t.Fatalf("anti-vacuity: %s's output holds no quoted body or no set_config record", im.name)
		}
		if first == nil {
			first = out
			t.Logf("reference: %s (%s), %d bytes over %d files", im.name, im.path, len(out), len(files))
			continue
		}
		if !bytes.Equal(out, first) {
			a, b := strings.Split(string(first), "\n"), strings.Split(string(out), "\n")
			for j := 0; j < len(a) && j < len(b); j++ {
				if a[j] != b[j] {
					t.Fatalf("%s (%s) reads differently from %s at output line %d:\n  %s: %q\n  %s: %q",
						im.name, im.path, impls[0].name, j+1, impls[0].name, a[j], im.name, b[j])
				}
			}
			t.Fatalf("%s (%s): %d output lines, %s: %d", im.name, im.path, len(b), impls[0].name, len(a))
		}
		t.Logf("%s (%s): identical", im.name, im.path)
	}
}
