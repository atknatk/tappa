package main

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// redlineRun runs scripts/redline-check.sh (and the secretscan.sh it sources) over a fresh tree
// holding only the two scripts, every tree they read and the given files. It returns the output
// and the exit code. TestRedline_TheCounterSetMarkIsTheTriggersAlone's harness, shared shape.
func redlineRun(t *testing.T, files map[string]string) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed; redline-check needs it (CI installs it)")
	}
	r := &hookRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q")
	for _, s := range []string{"scripts/redline-check.sh", "scripts/secretscan.sh"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, s))
		if err != nil {
			t.Fatal(err)
		}
		r.write(s, string(b))
	}
	for _, d := range []string{"cmd", "internal", "db", "web/templates", "web/static/js", "test", "deploy", ".github"} {
		r.write(d+"/keep.txt", "\n")
	}
	r.write("Makefile", "all:\n")
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
//	     `"public"."tags"` -- and the old bare form still; the guarded one (`last_ctr <`) is quiet
//	R7   `api_key`, `apiKey`, `bearer`, `signing_key` inside a log/fmt call, and the Authorization
//	     header read inside one (`Header.Get("Authorization")`, `Header["Authorization"]`, across
//	     lines); "api key created" with a key id, and another header, are quiet
//	R7b  `.FirstName`, `.LastName`, `.EmployeeName`, `"first_name"`, `"employee_name"` in a log call;
//	     `"employee_id"` is quiet
//	R7d  a value of the key's shape after `tt_live_`, `tt_test_` and the webhook signing key's
//	     `tt_whsec_` -- in a document and in Go source; a filler (43 `x`) and a `<…>` placeholder
//	     -- the way the documents write an example -- are quiet
//
// Every violation file is named under its rule's FAIL and the exit is non-zero; the controls'
// tree exits 0 with none of the four FAIL sections.
//
// MUTANTS (measured): each addition reverted alone -- R4's pattern back to `UPDATE +tags`, the
// three R7 roots and the header alternative removed, R7b's two new alternatives removed, R7d's
// c_tappa call removed -- turns this test red on exactly that rule's files.
func TestRedline_R4R7R7bR7dCatchDeliberateViolations(t *testing.T) {
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
		},
		"R7": {
			"internal/x/r7_apikey.go":    goFile(`S@.Info("issued", "api_key", k)`),
			"internal/x/r7_camel.go":     goFile(`_ = F@.Errorf("x %s", apiKey)`),
			"internal/x/r7_bearer.go":    goFile(`L@.Printf("auth %s", bearerValue)`),
			"internal/x/r7_signing.go":   goFile(`a.L@.Warn("x", "signing_key", sk)`),
			"internal/x/r7_header.go":    goFile(`S@.Info("x", "h", r.Header.Get("Authorization"))`),
			"internal/x/r7_headermap.go": goFile("S@.Warn(\"x\",\n\t\t\"h\", r.Header[\"Authorization\"],\n\t)"),
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
