package scripttest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedline_M16_11_TrackedButIgnoredIsNotScanned pins S2 (M16-11, the hand-over of M16-02's last
// security check): ripgrep honours .gitignore and never looks at the index, so a file tracked with
// `git add -f` under an ignore pattern is scanned by no R rule -- and S1 exempts mobile/, so it
// does not see one there either. S2 asks git for that set directly. The red tree force-adds three
// such files (the card's mobile/.idea/Leak.kt, a build-output file under the staff app's own
// ignore, one outside mobile/ and android/); the green tree holds the same ignored files untracked,
// which is what an ignore pattern is for, next to a tracked file no pattern matches.
func TestRedline_M16_11_TrackedButIgnoredIsNotScanned(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed; redline-check needs it (CI installs it)")
	}
	files := map[string]string{
		".gitignore":                         ".idea/\n",
		"mobile/android/.gitignore":          "/build/\n",
		"mobile/.idea/Leak.kt":               "package zz\n",
		"mobile/android/build/Gen.kt":        "package zz\n",
		"internal/zz/.idea/Note.go":          "package zz\n",
		"mobile/android/app/src/A.kt":        "package zz\n",
		"mobile/android/settings.gradle.kts": "rootProject.name = \"zz\"\n",
	}
	ignored := []string{"mobile/.idea/Leak.kt", "mobile/android/build/Gen.kt", "internal/zz/.idea/Note.go"}

	run := func(t *testing.T, force bool) (string, int) {
		t.Helper()
		dir := t.TempDir()
		for rel, body := range files {
			p := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// The harness re-runs `git init` in dir, which keeps an existing index: the forced
		// entries made here survive into the scan.
		args := []string{"-c", "core.hooksPath=/dev/null", "-c", "init.defaultBranch=main", "init", "-q"}
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		add := []string{"-C", dir, "add", "mobile/android/app/src/A.kt", "mobile/android/settings.gradle.kts"}
		if force {
			add = append(append([]string{"-C", dir, "add", "-f"}, ignored...), "mobile/android/app/src/A.kt")
		}
		if out, err := exec.Command("git", add...).CombinedOutput(); err != nil {
			t.Fatalf("git add: %v\n%s", err, out)
		}
		out, code, err := redlineExecRoots(context.Background(), dir, redlineRoots, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out, code
	}

	out, code := run(t, true)
	section := failSection(out, "S2")
	if code == 0 {
		t.Errorf("S2: force-added ignored files: want a non-zero exit, got 0:\n%s", out)
	}
	for _, rel := range ignored {
		if !strings.Contains(section, rel+":") {
			t.Errorf("S2: %s is not named under S2 FAIL:\n%s", rel, out)
		}
	}
	if strings.Contains(section, "mobile/android/app/src/A.kt") {
		t.Errorf("S2: a tracked file no pattern matches is named:\n%s", out)
	}

	out, code = run(t, false)
	if code != 0 || strings.Contains(out, "· FAIL]") || strings.Contains(out, "ATLANDI") {
		t.Errorf("S2: ignored files left untracked: want exit 0, no FAIL and no scan error, got exit %d:\n%s", code, out)
	}
}
