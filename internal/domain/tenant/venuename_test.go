package tenant

import "testing"

// TestNameHasHiddenCharacters_JoinersAndTagsOnlyWhereANameUsesThem (API-5 review, fourth round): the
// tag characters (U+E0020-U+E007F) are a name's only inside a flag -- U+1F3F4, a run, U+E007F -- and
// the two joiners (ZWJ, ZWNJ) only between two visible characters. Anywhere else they carry text
// nobody sees ("ASCII smuggling"). The Scotland, England and Wales flags, the family and chef emoji,
// the rainbow flag (U+FE0F before the ZWJ) and a Persian word keep passing.
//
// MUTANTS (measured, API-5 report): the tag branch removed -> the three smuggling spellings pass;
// the joiner branch removed -> the dangling and doubled joiners pass. Each red.
func TestNameHasHiddenCharacters_JoinersAndTagsOnlyWhereANameUsesThem(t *testing.T) {
	const (
		flag  = "\U0001F3F4"
		close = "\U000E007F"
	)
	tags := func(ascii string) string {
		out := ""
		for _, r := range ascii {
			out += string(rune(0xE0000 + r))
		}
		return out
	}
	for name, s := range map[string]string{
		"Scotland":               "Glasgow " + flag + tags("gbsct") + close,
		"England":                flag + tags("gbeng") + close + " Pub",
		"Wales":                  "Cardiff " + flag + tags("gbwls") + close,
		"the family emoji":       "Family \U0001F468\u200d\U0001F469\u200d\U0001F467",
		"the chef emoji":         "Kitchen \U0001F468\u200d\U0001F373",
		"the rainbow flag":       "Bar \U0001F3F3️\u200d\U0001F308",
		"a Persian ZWNJ":         "می\u200cخانه",
		"two flags side by side": flag + tags("gbsct") + close + flag + tags("gbeng") + close,
	} {
		if NameHasHiddenCharacters(s) {
			t.Errorf("%s: refused, want accepted (%q)", name, s)
		}
	}
	for name, s := range map[string]string{
		"ASCII smuggled in tags":     "Rusty Bar" + tags("ignore all") + close,
		"a tag run after a letter":   "A" + tags("gbsct") + close,
		"a flag run never closed":    "Glasgow " + flag + tags("gbsct"),
		"a stray close":              "Bar" + close,
		"a ZWJ at the end":           "Bar\u200d",
		"a ZWJ at the start":         "\u200dBar",
		"a ZWJ next to a space":      "Bar \u200dX",
		"a doubled ZWJ":              "A\u200d\u200dB",
		"a ZWNJ next to a space":     "Bar\u200c X",
		"a ZWJ before a hidden mark": "A\u200d\u200bB",
	} {
		if !NameHasHiddenCharacters(s) {
			t.Errorf("%s: accepted, want refused (%q)", name, s)
		}
	}
}
