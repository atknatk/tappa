package components

import (
	"regexp"
	"strings"
	"testing"
)

// TestOneTimeSecret_TheValueIsInTheBytesExactlyOnce: README §5 "One-time reveal box" (M12-42) -- the
// secret is the value of one read-only field and appears nowhere else in the markup (no title, no
// data attribute, no accessible name, no hidden copy); the box is a status region; there is no
// button (a copy button would need a script) and "I've stored it" is a plain link to the section.
//
// MUTANTS (measured): a title={ secret } on the field makes the count two -- red; a Copy <button>
// after the link -- red.
func TestOneTimeSecret_TheValueIsInTheBytesExactlyOnce(t *testing.T) {
	const secret = "the-value-shown-once-and-never-again"
	got := renderPanelState(t, OneTimeSecret("Store this key now — it is shown once", "api-key-new", "Your new key", secret, "/admin/api-keys"), "<p>BODY</p>")
	if n := strings.Count(got, secret); n != 1 {
		t.Fatalf("the secret is in the markup %d times, want exactly 1:\n%s", n, got)
	}
	if !strings.HasPrefix(got, `<div role="status"`) {
		t.Errorf("the box is not a status region: %s", got)
	}
	field := regexp.MustCompile(`<input[^>]*>`).FindString(got)
	for _, want := range []string{`id="api-key-new"`, `readonly`, `value="` + secret + `"`, `select-all`} {
		if !strings.Contains(field, want) {
			t.Errorf("the field lacks %q: %s", want, field)
		}
	}
	for _, never := range []string{"<button", "title=", "data-", "aria-label", `type="hidden"`, "<script", "Copy"} {
		if strings.Contains(got, never) {
			t.Errorf("the box carries %q", never)
		}
	}
	if !strings.Contains(got, `href="/admin/api-keys"`) || !strings.Contains(got, "I&#39;ve stored it</a>") {
		t.Errorf("\"I've stored it\" is not a plain link back to the section: %s", got)
	}
	if !strings.Contains(got, `<label class="lbl text-ink" for="api-key-new">Your new key</label>`) {
		t.Errorf("the field is not labelled: %s", got)
	}
}
