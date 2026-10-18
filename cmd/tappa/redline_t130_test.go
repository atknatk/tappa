package main

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
// round's audit measured green (W6, W7, W10) and the neighbours of each.
func TestRedline_R5bWaiverIsThe00058DownAlone(t *testing.T) {
	const name = "00058_keep_the_deliveries_ninety_days.sql"
	b, err := os.ReadFile(filepath.Join(repoRoot, "db", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	shipped := string(b)
	const (
		revoke = "REVOKE DELETE ON public.webhook_deliveries FROM tappa_app;\n"
		drop   = "DROP POLICY webhook_deliveries_delete_settled ON public.webhook_deliveries;\n"
		down   = "-- +goose Down\n"
	)
	for _, s := range []string{revoke, drop, down} {
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
		{"no revoke", name, strings.Replace(shipped, revoke, "", 1)},
		{"the revoke after the drop", name, strings.Replace(strings.Replace(shipped, revoke, "", 1), drop, drop+revoke, 1)},
		{"the policy dropped twice", name, strings.Replace(shipped, drop, drop+drop, 1)},
		{"another file's name", "00060_keep_the_deliveries_ninety_days.sql", shipped},
	} {
		if c.body == shipped && c.file == name {
			t.Fatalf("%s: the variant did not change the file", c.what)
		}
		out, code := redlineRun(t, map[string]string{"db/migrations/" + c.file: c.body})
		if fail := failSection(out, "R5b"); code == 0 || !strings.Contains(fail, c.file+": politika SILINIYOR") {
			t.Errorf("%s: exit %d, R5b FAIL %q -- want a non-zero exit and the drop named:\n%s", c.what, code, fail, out)
		}
		// W6 leaves the Down as shipped: its drop is still the waived one; the Up's is the FAIL.
		if strings.Contains(out, waived) != strings.HasPrefix(c.what, "W6") {
			t.Errorf("%s: the waiver fired %v", c.what, strings.Contains(out, waived))
		}
	}
}
