package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestClaudeMD_TheIdentityExceptionIsADR0031sSection61 pins CLAUDE.md §4.5's item (3) to ADR 0031 §6.1's
// quoted text, word for word (whitespace and the quote's "> " aside): §6.1 is the text M14-01 applied
// "birebir", and every later decision that changes it (M14-17b's fourth named exception, the user's
// decision of 2026-10-27) changes both. The item ends where CLAUDE.md's list goes on (" 6. ").
//
// MUTANT (measured): "dört istisna" back to "üç" in CLAUDE.md alone -- red.
func TestClaudeMD_TheIdentityExceptionIsADR0031sSection61(t *testing.T) {
	const start = "**(3) Panel yöneticisinin kimliği**"
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	claude, adr := read("CLAUDE.md"), read(filepath.Join("docs", "adr", "0031-tek-kimlik-cok-isletme.md"))
	i := strings.Index(claude, start)
	k := strings.Index(adr, "> "+start)
	if i < 0 || k < 0 {
		t.Fatal("CLAUDE.md or ADR 0031 §6.1 no longer holds item (3)")
	}
	words := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	l := strings.Index(adr[k:], "\n\n")
	quoted := words(regexp.MustCompile(`(?m)^> ?`).ReplaceAllString(adr[k:k+l], ""))
	item := words(claude[i:])
	if j := strings.Index(item, " 6. **"); j > 0 {
		item = item[:j]
	}
	if item != quoted {
		n := 0
		for n < len(item) && n < len(quoted) && item[n] == quoted[n] {
			n++
		}
		t.Fatalf("CLAUDE.md §4.5 (3) differs from ADR 0031 §6.1 at byte %d:\n CLAUDE.md: …%s\n ADR §6.1:  …%s",
			n, item[max(0, n-40):min(len(item), n+60)], quoted[max(0, n-40):min(len(quoted), n+60)])
	}
	for _, want := range []string{"adlandırılmış dört istisna", "`identity_claim_signup`'ın parola koludur", "adlandırılmış dört istisnanın dışında"} {
		if !strings.Contains(item, want) {
			t.Errorf("CLAUDE.md §4.5 (3) does not say %q", want)
		}
	}
}
