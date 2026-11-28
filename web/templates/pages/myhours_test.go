package pages

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestMyHoursTemplate_NoClassNameInAComment is skill tappa-brand's Tailwind trap for the My hours page
// (M16-32): Tailwind scans a .templ file as raw text, comments included, so a utility named in a comment
// compiles a rule nothing renders (or keeps one alive). Every class the template's markup uses, and the
// seven words the skill measured compiling dead rules from prose, must be absent from its comment lines.
func TestMyHoursTemplate_NoClassNameInAComment(t *testing.T) {
	b, err := os.ReadFile("myhours.templ")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	classes := map[string]bool{}
	for _, m := range regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(src, -1) {
		for _, c := range strings.Fields(m[1]) {
			classes[c] = true
		}
	}
	if len(classes) < 5 {
		t.Fatalf("CONTROL FAILED: %d class names read from the markup", len(classes))
	}
	for _, w := range []string{"filter", "visible", "relative", "min-h-16", "static", "fixed", "hidden"} {
		classes[w] = true
	}
	word := regexp.MustCompile(`[A-Za-z0-9:/_\-]+`)
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, w := range word.FindAllString(trimmed, -1) {
			if classes[w] {
				t.Errorf("myhours.templ:%d: the comment names the class %q", i+1, w)
			}
		}
	}
}
