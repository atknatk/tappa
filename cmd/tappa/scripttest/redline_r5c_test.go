package scripttest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Moved from cmd/tappa/scriptguards_test.go with the other redline tests (T185).

// TestRedline_TheCounterSetMarkIsTheTriggersAlone pins redline-check.sh's R5c (M11 API-2,
// ADR 0028's API-2 note, rounds 3 and 4): the counter-set mark -- the transaction-local setting by
// which 00049's triggers refuse a second counter set (TF001) -- is named nowhere but db/migrations.
// A caller that writes it first steps past the check, and the deadlock it reopens falls on OTHER
// tenants' holding taps (40P01, the NFC record not written). PostgreSQL reads a setting's name
// without regard to case and takes it quoted or spaced, so every spelling counts. On a tree
// holding only the two scripts: every spelling in a migration -- no R5c; each spelling, and the
// plain one in a Go file, a test, a script, a deploy file and the root Makefile -- each file named
// under R5c FAIL, and a non-zero exit. (A name built from pieces is beyond any line grep -- the
// rule's header says so; this test builds its own that way, the rule scanning this very file.)
func TestRedline_TheCounterSetMarkIsTheTriggersAlone(t *testing.T) {
	t.Parallel() // T185: own temp trees and processes, no t.Setenv/t.Chdir
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed; redline-check needs it (CI installs it)")
	}
	spellings := []string{
		"tappa." + "feed_counters", "TAPPA." + "FEED_COUNTERS", "Tappa." + "Feed_Counters",
		`"tappa"."` + `feed_counters"`, "tappa . " + "feed_counters", "tappa\t.\t" + "feed_counters",
	}
	mark := func(name string) string { return "PERFORM pg_catalog.set_config('" + name + "', 'x', true);\n" }
	run := func(files map[string]string) (string, int) {
		t.Helper()
		r := &hookRepo{t: t, dir: t.TempDir()}
		r.git("init", "-q")
		for _, s := range []string{"scripts/redline-check.sh", "scripts/secretscan.sh"} {
			b, err := os.ReadFile(filepath.Join(repoRoot, s))
			if err != nil {
				t.Fatal(err)
			}
			r.write(s, string(b))
		}
		// Every tree the script reads exists (a missing one is a scan error, not a clean scan).
		for _, d := range []string{"cmd", "internal", "db", "web/templates", "web/static/js", "test", "deploy", ".github", "docs/api", "android/app/src/main"} {
			r.write(d+"/keep.txt", "\n")
		}
		if _, ok := files["Makefile"]; !ok {
			r.write("Makefile", "all:\n")
		}
		for rel, body := range files {
			r.write(rel, body)
		}
		cmd := exec.Command("bash", "scripts/redline-check.sh")
		cmd.Dir = r.dir
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	// T137 fix1: R5b reads a set_config's name as PostgreSQL does -- case-insensitively and nothing
	// more -- against R5B_SET_CONFIG. The three spellings PostgreSQL accepts are on the list (exit 0);
	// the three it refuses ("invalid configuration parameter name", measured on 17) are not, so in a
	// migration they are an R5b FAIL. Neither is ever R5c: inside db/migrations the mark is the
	// triggers' own.
	migration := ""
	for _, sp := range spellings[:3] {
		migration += mark(sp)
	}
	if out, code := run(map[string]string{"db/migrations/00099_x.sql": migration}); strings.Contains(out, "R5c") || code != 0 {
		t.Errorf("every spelling PostgreSQL accepts, in a migration, is the triggers' own -- exit %d, R5c fired:\n%s", code, out)
	}
	migration = ""
	for _, sp := range spellings[3:] {
		migration += mark(sp)
	}
	if out, _ := run(map[string]string{"db/migrations/00099_x.sql": migration}); strings.Contains(out, "R5c") ||
		strings.Count(failSection(out, "R5b"), "R5B_SET_CONFIG listesinde degil") != 3 {
		t.Errorf("the spellings PostgreSQL refuses, in a migration: want three R5b set_config FAILs and no R5c:\n%s", out)
	}
	files := map[string]string{}
	for i, sp := range spellings {
		files[fmt.Sprintf("internal/domain/checkin/spelling%d.go", i)] = "// " + mark(sp)
	}
	for _, rel := range []string{"internal/db/x_test.go", "scripts/x.sh", "deploy/x.yaml", "Makefile"} {
		files[rel] = "# " + mark(spellings[0])
	}
	out, code := run(files)
	section := ""
	if i := strings.Index(out, "R5c · FAIL"); i >= 0 {
		section = out[i:]
		if j := strings.Index(section, "\n\n"); j >= 0 {
			section = section[:j]
		}
	}
	if code == 0 {
		t.Errorf("the mark outside the migrations: exit 0, want non-zero:\n%s", out)
	}
	for rel := range files {
		if !strings.Contains(section, rel+":") {
			t.Errorf("the mark in %s (%q) is not named under R5c FAIL:\n%s", rel, strings.TrimSpace(files[rel]), out)
		}
	}
}
