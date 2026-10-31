package scripttest

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// redlineRun runs scripts/redline-check.sh (and the secretscan.sh it sources) over a fresh tree
// holding only the two scripts, every tree they read and the given files. It returns the output
// and the exit code. TestRedline_TheCounterSetMarkIsTheTriggersAlone's harness, shared shape.
func redlineRun(t *testing.T, files map[string]string) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed; redline-check needs it (CI installs it)")
	}
	out, code, err := redlineExec(context.Background(), t.TempDir(), files)
	if err != nil {
		t.Fatal(err)
	}
	return out, code
}

// redlineExec is redlineRun without a *testing.T, so the shipped tree's scan can start before any
// test does (prefetch_test.go): it builds the tree in dir (empty, owned by the caller) and runs the
// script there. A cancelled ctx ends the script.
func redlineExec(ctx context.Context, dir string, files map[string]string) (string, int, error) {
	write := func(rel, body string) error {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(body), 0o644)
	}
	// hookRepo.git's flags: no user git setting reaches the scan's `git ls-files`.
	git := exec.CommandContext(ctx, "git", "-c", "user.name=t", "-c", "user.email=t@example.test",
		"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "init.defaultBranch=main",
		"-c", "core.hooksPath=/dev/null", "-c", "advice.nestedTag=false", "init", "-q")
	git.Dir = dir
	if out, err := git.CombinedOutput(); err != nil {
		return "", 0, fmt.Errorf("git init: %v\n%s", err, out)
	}
	for _, s := range []string{"scripts/redline-check.sh", "scripts/secretscan.sh"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, s))
		if err != nil {
			return "", 0, err
		}
		if err := write(s, string(b)); err != nil {
			return "", 0, err
		}
	}
	for _, d := range []string{"cmd", "internal", "db", "web/templates", "web/static/js", "test", "deploy", ".github", "docs/api", "android/app/src/main"} {
		if err := write(d+"/keep.txt", "\n"); err != nil {
			return "", 0, err
		}
	}
	if err := write("Makefile", "all:\n"); err != nil {
		return "", 0, err
	}
	for rel, body := range files {
		if err := write(rel, body); err != nil {
			return "", 0, err
		}
	}
	cmd := exec.CommandContext(ctx, "bash", "scripts/redline-check.sh")
	cmd.Dir = dir
	// Cancelling kills bash; the one short child it may be running (grep, awk) ends by itself, and
	// WaitDelay bounds the wait for the pipe that child holds. No process group of its own, so a
	// terminal's Ctrl-C still reaches the scan.
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", 0, fmt.Errorf("redline-check: %w", ctx.Err())
	}
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		return "", 0, err
	}
	return string(out), code, nil
}

// failSection is every block the script printed under "[<code> · FAIL]", joined.
func failSection(out, code string) string {
	var b strings.Builder
	rest := out
	for {
		i := strings.Index(rest, "["+code+" · FAIL]")
		if i < 0 {
			return b.String()
		}
		block := rest[i:]
		if j := strings.Index(block, "\n\n"); j >= 0 {
			block = block[:j]
		}
		b.WriteString(block + "\n")
		rest = rest[i+1:]
	}
}

// randomKeyBody is 43 characters of base64url drawn at run time -- a value of an integration key's
// shape that exists only inside this test's temporary tree, never in a file of the repository.
func randomKeyBody(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// TestRedline_R4R7R7bR7dCatchDeliberateViolations is ADR 0028 §3's "her ekleme kasıtlı ihlalde
// kırmızı" (M11 API-3). Each addition was first counted on the tree (0 new hits, the rule's own
// comment); here each is shown to fire on a deliberate violation and to stay quiet on its control:
//
//	R4   the schema-qualified counter write (the table named public.tags), quoted
//	     `"public"."tags"` -- and the old bare form still; the guarded one (`last_ctr <`) is quiet.
//	     2nd round, the statement arm: ONLY, an alias, the statement split over lines (SQL, a
//	     migration's body, a Go raw string) and a non-strict `<=` guard fire; AdvanceTagCounter's
//	     own guarded shape is quiet, and so is a _test.go file (the arm's named exclusion)
//	R7   `api_key`, `apiKey`, `bearer`, `signing_key` inside a log/fmt call, and the Authorization
//	     header read inside one (`Header.Get("Authorization")`, `Header["Authorization"]`, across
//	     lines); "api key created" with a key id, and another header, are quiet. 2nd round: the
//	     raw key's one exit (`RevealToTheOwnerOnce()`), the panel's `NewKey` and
//	     `Header.Values("Authorization")`
//	R7b  `.FirstName`, `.LastName`, `.EmployeeName`, `"first_name"`, `"employee_name"` in a log call;
//	     `"employee_id"` is quiet
//	R7d  a value of the key's shape after `tt_live_`, `tt_test_` and the webhook signing key's
//	     `tt_whsec_` -- in a document and in Go source; a filler (43 `x`) and a `<…>` placeholder
//	     -- the way the documents write an example -- are quiet. 2nd round: `%20`/`%3D` and the
//	     `\n`/`\t`/`\r` escapes on the left, and a key cut to 42 or 32 characters, fire; 31
//	     characters and a word that only ends in the prefix's letters are quiet
//
// Every violation file is named under its rule's FAIL and the exit is non-zero; the controls'
// tree exits 0 with none of the four FAIL sections.
//
// MUTANTS (measured): each addition reverted alone -- R4's pattern back to `UPDATE +tags`, the
// three R7 roots and the header alternative removed, R7b's two new alternatives removed, R7d's
// c_tappa call removed -- turns this test red on exactly that rule's files.
func TestRedline_R4R7R7bR7dCatchDeliberateViolations(t *testing.T) {
	t.Parallel() // T185: own temp trees and processes, no t.Setenv/t.Chdir
	body := randomKeyBody(t)
	// THE SPELLINGS ARE ASSEMBLED AT RUN TIME: this file is itself in the tree the real script
	// scans, so a violation written out here would turn the repository red. "S@." is slog's
	// call, "L@." log's, "F@." fmt's and "UPD@TE" the statement.
	spell := strings.NewReplacer("S@.", "s"+"log.", "L@.", "l"+"og.", "F@.", "f"+"mt.", "UPD@TE", "UPD"+"ATE", "upd@te", "upd"+"ate")
	goFile := func(stmt string) string {
		return "package x\n\nfunc f() {\n\t" + stmt + "\n}\n"
	}
	violations := map[string]map[string]string{
		"R4": {
			"db/queries/r4_public.sql":    "UPD@TE public.tags SET last_ctr = $2 WHERE uid = $1;\n",
			"db/queries/r4_quoted.sql":    "UPD@TE \"public\".\"tags\" SET last_ctr = $2 WHERE uid = $1;\n",
			"db/queries/r4_spaced.sql":    "UPD@TE public . tags SET last_ctr = $2 WHERE uid = $1;\n",
			"internal/x/r4_bare.go":       goFile("_ = `UPD@TE tags SET last_ctr = $2 WHERE uid = $1`"),
			"internal/x/r4_lower.go":      goFile("_ = `upd@te public.tags set last_ctr = $2`"),
			"db/queries/r4_quotedcol.sql": "UPD@TE public.tags SET \"last_ctr\" = $2 WHERE uid = $1;\n",
			// 2nd round: the statement arm -- ONLY, an alias, split lines, a non-strict guard.
			"db/queries/r4_only.sql":             "UPD@TE ONLY tags SET last_ctr = $2 WHERE uid = $1;\n",
			"db/queries/r4_alias_split.sql":      "UPD@TE tags t\nSET last_ctr = @ctr\nFROM prev\nWHERE t.uid = prev.old_uid;\n",
			"db/migrations/99998_r4_holding.sql": "    UPD@TE public.tags AS t\n       SET last_ctr = p_ctr\n     WHERE t.uid = v.old_uid\n       AND v.old_ctr <= p_ctr;\n",
			"internal/x/r4_raw.go":               goFile("_ = `UPD@TE tags AS t\n\t\tSET last_ctr = $2\n\t\tWHERE t.uid = $1`"),
		},
		"R7": {
			"internal/x/r7_apikey.go":    goFile(`S@.Info("issued", "api_key", k)`),
			"internal/x/r7_camel.go":     goFile(`_ = F@.Errorf("x %s", apiKey)`),
			"internal/x/r7_bearer.go":    goFile(`L@.Printf("auth %s", bearerValue)`),
			"internal/x/r7_signing.go":   goFile(`a.L@.Warn("x", "signing_key", sk)`),
			"internal/x/r7_header.go":    goFile(`S@.Info("x", "h", r.Header.Get("Authorization"))`),
			"internal/x/r7_headermap.go": goFile("S@.Warn(\"x\",\n\t\t\"h\", r.Header[\"Authorization\"],\n\t)"),
			// 2nd round: the value's own names and Header.Values.
			"internal/x/r7_reveal.go": goFile(`S@.Info("x", "k", created.Key.RevealToTheOwnerOnce())`),
			"internal/x/r7_newkey.go": goFile(`a.L@.Info("x", "k", v.NewKey)`),
			"internal/x/r7_values.go": goFile(`S@.Info("x", "h", r.Header.Values("Authorization"))`),
		},
		"R7b": {
			"internal/x/r7b_first.go":    goFile(`S@.Info("x", "n", e.FirstName)`),
			"internal/x/r7b_last.go":     goFile(`a.L@.Info("x", "n", e.LastName)`),
			"internal/x/r7b_employee.go": goFile(`S@.Info("x", "n", p.EmployeeName)`),
			"internal/x/r7b_key.go":      goFile(`S@.Info("x", "first_name", n)`),
			"internal/x/r7b_snake.go":    goFile(`S@.Info("x", "employee_name", n)`),
		},
		"R7d": {
			"docs/r7d_live.md":  "the key is tt_live_" + body + " -- do not share it\n",
			"docs/r7d_test.md":  "sandbox: tt_test_" + body + "\n",
			"docs/r7d_whsec.md": "signing: `tt_whsec_" + body + "`\n",
			"internal/x/r7d.go": goFile(`_ = "tt_live_` + body + `"`),
			// 2nd round: an escape or a URL encoding on the left, and a cut key.
			"docs/r7d_url.md":   "curl -H Authorization:Bearer%20tt_live_" + body + "\n",
			"docs/r7d_pct.md":   "?key%3Dtt_live_" + body + "\n",
			"docs/r7d_json.md":  `{"log":"first line\ntt_live_` + body + `"}` + "\n",
			"docs/r7d_tab.md":   `"col\ttt_test_` + body + `"` + "\n",
			"docs/r7d_cr.md":    `"x\rtt_whsec_` + body + `"` + "\n",
			"docs/r7d_cut42.md": "tt_live_" + body[:42] + "\n",
			"docs/r7d_cut32.md": "tt_live_" + body[:32] + "\n",
		},
	}
	all := map[string]string{}
	for _, files := range violations {
		for rel, src := range files {
			all[rel] = spell.Replace(src)
		}
	}
	out, code := redlineRun(t, all)
	if code == 0 {
		t.Errorf("the deliberate violations: exit 0, want non-zero:\n%s", out)
	}
	for rule, files := range violations {
		section := failSection(out, rule)
		for rel := range files {
			if !strings.Contains(section, rel+":") {
				t.Errorf("%s is not named under %s FAIL:\n%s", rel, rule, out)
			}
		}
	}
	if strings.Contains(out, body) {
		t.Error("the report printed the key's value (R7d prints a class, never the line)")
	}

	controls := map[string]string{
		"db/queries/c_r4.sql":       "UPD@TE public.tags SET last_ctr = $2 WHERE uid = $1 AND last_ctr < $2;\n",
		"internal/x/c_r7.go":        goFile(`S@.Info("api key created", "key_id", id)`),
		"internal/x/c_r7_hdr.go":    goFile(`S@.Info("x", "h", r.Header.Get("Content-Type"))`),
		"internal/x/c_r7b.go":       goFile(`S@.Info("x", "employee_id", id)`),
		"docs/c_r7d_filler.md":      "an example: tt_live_" + strings.Repeat("x", 43) + "\n",
		"docs/c_r7d_placeholder.md": "Authorization: Bearer tt_live_<43 characters of base64url>\n",
		// 2nd round: the guarded statement in AdvanceTagCounter's own shape (the guard in the CTE's
		// alias); the statement arm's NAMED exclusion -- a test file's deliberate unguarded write;
		// a piece shorter than 32; and a word that merely ends in the prefix's letters.
		"db/queries/c_r4_cte.sql": "UPD@TE tags t\nSET last_ctr = @ctr\nFROM prev\nWHERE t.uid = prev.old_uid\n  AND prev.old_ctr < @ctr\nRETURNING t.uid;\n",
		"internal/x/c_r4_test.go": goFile("_ = `UPD@TE tags\n\t\tSET last_ctr = 0\n\t\tWHERE uid = $1`"),
		"docs/c_r7d_cut31.md":     "tt_live_" + body[:31] + "\n",
		"docs/c_r7d_word.md":      "xtt_live_" + body + "\n",
	}
	for rel, src := range controls {
		controls[rel] = spell.Replace(src)
	}
	out, code = redlineRun(t, controls)
	if code != 0 {
		t.Errorf("the controls: exit %d, want 0:\n%s", code, out)
	}
	for _, rule := range []string{"R4", "R7", "R7b", "R7d"} {
		if s := failSection(out, rule); s != "" {
			t.Errorf("a control fired %s:\n%s", rule, s)
		}
	}
}
