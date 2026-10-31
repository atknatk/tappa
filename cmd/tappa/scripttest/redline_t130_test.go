package scripttest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedline_R5bWaiverIsThe00058DownAlone pins redline-check.sh's one R5b waiver (T130, ADR 0028's
// T130 note and its fix round): 00058's Down may drop its own RESTRICTIVE delete policy because the
// same Down first takes tappa_app's DELETE away. The shipped file is waived (a WARN line, no R5b
// FAIL); every variant that widens the reach is an R5b FAIL and a non-zero exit -- the three the first
// round's audit measured green (W6, W7, W10), the low round's four (V05, V06, V16, G4: dynamic SQL,
// a named dollar quote, a block comment between two words, PUBLIC) and the neighbours of each. The
// lexical gate is not the last one: internal/db's
// TestWebhookRetention00058And00059_DownGiveBackAndUpTakesItAgain reads the effective privilege after
// the Down, PUBLIC and membership counted.
func TestRedline_R5bWaiverIsThe00058DownAlone(t *testing.T) {
	t.Parallel() // T185: own temp trees and processes, no t.Setenv/t.Chdir
	const name = "00058_keep_the_deliveries_ninety_days.sql"
	b, err := os.ReadFile(filepath.Join(repoRoot, "db", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	shipped := string(b)
	const (
		revoke  = "REVOKE DELETE ON public.webhook_deliveries FROM tappa_app;\n"
		drop    = "DROP POLICY webhook_deliveries_delete_settled ON public.webhook_deliveries;\n"
		down    = "-- +goose Down\n"
		upGrant = "GRANT DELETE ON public.webhook_deliveries TO tappa_app;\n"
	)
	// The Up's last PL/pgSQL body, the postcondition: from its StatementBegin to the Down.
	post := shipped[strings.LastIndex(shipped[:strings.Index(shipped, down)], "-- +goose StatementBegin\n"):strings.Index(shipped, down)]
	for _, s := range []string{revoke, drop, down, upGrant, post} {
		if strings.Count(shipped, s) != 1 {
			t.Fatalf("PREMISE: %s carries %q %d times, want once", name, s, strings.Count(shipped, s))
		}
	}
	const waived = "KISITLAYICI politika Down'da siliniyor"
	out, _ := redlineRun(t, map[string]string{"db/migrations/" + name: shipped})
	if !strings.Contains(out, waived) || strings.Contains(failSection(out, "R5b"), name) {
		t.Fatalf("the shipped 00058: want the waiver's WARN and no R5b FAIL naming it:\n%s", out)
	}

	for _, c := range []struct{ what, file, body string }{
		{"W6: the same drop in the Up", name, strings.Replace(shipped, down, drop+down, 1)},
		{"W7: DELETE granted back between the revoke and the drop", name,
			strings.Replace(shipped, revoke, revoke+"GRANT DELETE ON public.webhook_deliveries TO tappa_app;\n", 1)},
		{"W10: DELETE granted to PUBLIC between the revoke and the drop", name,
			strings.Replace(shipped, revoke, revoke+"GRANT DELETE ON public.webhook_deliveries TO PUBLIC;\n", 1)},
		{"DELETE on every table of the schema before the drop", name,
			strings.Replace(shipped, revoke, revoke+"GRANT ALL ON ALL TABLES IN SCHEMA public TO tappa_app;\n", 1)},
		{"DELETE granted back after the drop", name,
			strings.Replace(shipped, drop, drop+"GRANT DELETE ON public.webhook_deliveries TO tappa_app;\n", 1)},
		{"a DO block between the revoke and the drop", name,
			strings.Replace(shipped, revoke, revoke+"-- +goose StatementBegin\nDO $$ BEGIN EXECUTE 'GRANT DELETE ON public.webhook_deliveries TO tappa_app'; END $$;\n-- +goose StatementEnd\n", 1)},
		// The low round's four (T130, 2026-10-18), each measured exit 0 before it.
		{"V05: a DO block's EXECUTE grants DELETE back after the drop", name,
			strings.Replace(shipped, drop, drop+"-- +goose StatementBegin\nDO $$ BEGIN EXECUTE 'GRANT DELETE ON public.webhook_deliveries TO tappa_app'; END $$;\n-- +goose StatementEnd\n", 1)},
		{"V06: a GRANT inside a named dollar quote after the drop", name,
			strings.Replace(shipped, drop, drop+"-- +goose StatementBegin\nDO $x$ BEGIN GRANT DELETE ON public.webhook_deliveries TO tappa_app; END $x$;\n-- +goose StatementEnd\n", 1)},
		{"V16: GRANT/**/DELETE after the drop", name,
			strings.Replace(shipped, drop, drop+"GRANT/**/DELETE ON public.webhook_deliveries TO tappa_app;\n", 1)},
		{"G4: DELETE granted to PUBLIC in the Up", name,
			strings.Replace(shipped, upGrant, upGrant+"GRANT DELETE ON public.webhook_deliveries TO PUBLIC;\n", 1)},
		{"a fourth PL/pgSQL body in the Up granting statically", name,
			strings.Replace(shipped, down, "-- +goose StatementBegin\nDO $$ BEGIN GRANT DELETE ON public.webhook_deliveries TO tappa_resolver; END $$;\n-- +goose StatementEnd\n"+down, 1)},
		{"the Up's grant to tappa_app and PUBLIC in one list", name,
			strings.Replace(shipped, upGrant, "GRANT DELETE ON public.webhook_deliveries TO tappa_app, PUBLIC;\n", 1)},
		{"a DO with a quoted body in the Down: its EXECUTE grants DELETE back", name,
			strings.Replace(shipped, drop, drop+"DO 'BEGIN EXECUTE ''GRANT DELETE ON public.webhook_deliveries TO tappa_app''; END';\n", 1)},
		{"a DO with a quoted body in the Up granting statically", name,
			strings.Replace(shipped, upGrant, upGrant+"DO 'BEGIN GRANT DELETE ON public.webhook_deliveries TO tappa_resolver; END';\n", 1)},
		{"a DO with a quoted body in the Up EXECUTE-ing a built GRANT", name,
			strings.Replace(shipped, upGrant, upGrant+"DO 'BEGIN EXECUTE pg_catalog.concat(''GR'', ''ANT DELETE ON public.webhook_deliveries TO tappa_resolver''); END';\n", 1)},
		{"the Up's postcondition DO moved after the Down's drop", name,
			strings.Replace(strings.Replace(shipped, post, "", 1), drop, drop+post, 1)},
		{"an E'' string in the Up (a backslash in the file: the 00058 shape forbids it)", name,
			strings.Replace(shipped, upGrant, upGrant+"SELECT E'\\'';\n", 1)},
		{"no revoke", name, strings.Replace(shipped, revoke, "", 1)},
		{"the revoke after the drop", name, strings.Replace(strings.Replace(shipped, revoke, "", 1), drop, drop+revoke, 1)},
		{"the policy dropped twice", name, strings.Replace(shipped, drop, drop+drop, 1)},
		{"another file's name", "00060_keep_the_deliveries_ninety_days.sql", shipped},
	} {
		if c.body == shipped && c.file == name {
			t.Fatalf("%s: the variant did not change the file", c.what)
		}
		// Each variant is its own tree (every one is a variant of 00058 under the same name), so they
		// run side by side (T139: 22 runs one after another were the package's second-longest test).
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			out, code := redlineRun(t, map[string]string{"db/migrations/" + c.file: c.body})
			if fail := failSection(out, "R5b"); code == 0 || !strings.Contains(fail, c.file+": politika SILINIYOR") {
				t.Errorf("%s: exit %d, R5b FAIL %q -- want a non-zero exit and the drop named:\n%s", c.what, code, fail, out)
			}
			// W6 leaves the Down as shipped: its drop is still the waived one; the Up's is the FAIL.
			if strings.Contains(out, waived) != strings.HasPrefix(c.what, "W6") {
				t.Errorf("%s: the waiver fired %v", c.what, strings.Contains(out, waived))
			}
		})
	}
}

// TestRedline_ABlockCommentSeparatesTokens pins sql_lex's block comment (T130 low round): to
// PostgreSQL `/* */` separates two words, so `GRANT/**/DELETE ON public.transactions TO tappa_app` is
// a GRANT; deleting the comment with no space left `grantdelete …`, which no `^grant ` rule saw
// (measured exit 0 in any migration). Now each variant is §4.3's append-only R5b FAIL, like its
// spaced control.
func TestRedline_ABlockCommentSeparatesTokens(t *testing.T) {
	t.Parallel() // T185: own temp trees and processes, no t.Setenv/t.Chdir
	const file = "db/migrations/00099_zz_probe.sql"
	for _, c := range []struct{ what, grant string }{
		{"control: spaced", "GRANT DELETE ON public.transactions TO tappa_app;"},
		{"an empty block comment", "GRANT/**/DELETE ON public.transactions TO tappa_app;"},
		{"a block comment with text", "GRANT/* x */UPDATE ON public.transactions TO tappa_app;"},
		{"a block comment across lines", "GRANT/*\n x\n*/DELETE ON public.transactions TO tappa_app;"},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			out, code := redlineRun(t, map[string]string{file: "-- +goose Up\n" + c.grant + "\n-- +goose Down\nSELECT 1;\n"})
			if fail := failSection(out, "R5b"); code == 0 || !strings.Contains(fail, file+": append-only tabloya UPDATE/DELETE yetkisi") {
				t.Errorf("%s: exit %d, R5b FAIL %q -- want a non-zero exit and the append-only grant named:\n%s", c.what, code, fail, out)
			}
		})
	}
}
