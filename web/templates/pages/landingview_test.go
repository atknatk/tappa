package pages

import "testing"

// TestAllFlags_OnlyASharedSetIsPrintedOnce: the cookie notice drops its attributes
// column only while every row carries the same set.
func TestAllFlags_OnlyASharedSetIsPrintedOnce(t *testing.T) {
	same := []CookieRow{{Flags: "HttpOnly"}, {Flags: "HttpOnly"}}
	if got := AllFlags(same); got != "HttpOnly" {
		t.Errorf("shared flags = %q", got)
	}
	if got := AllFlags(append(same, CookieRow{Flags: "Secure"})); got != "" {
		t.Errorf("differing rows report shared flags %q", got)
	}
	if got := AllFlags(nil); got != "" {
		t.Errorf("no rows report shared flags %q", got)
	}
}
