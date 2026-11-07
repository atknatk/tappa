package signup

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestDraft_PrintsNoPassword (backlog T209): an Account in a Draft holds the form's password for one
// request, and a %+v of either, or of a struct holding one, used to print it (the audit: 566 of 712
// renderings).
//
// MUTANT (measured, 2026-11-06, reverted): config.Key over a plain string -- red here.
func TestDraft_PrintsNoPassword(t *testing.T) {
	const phrase = "a-passphrase-nobody-guesses"
	inHex := hex.EncodeToString([]byte(phrase))
	a := Account{FullName: "Maria", Email: "maria@example.mt", Password: testPassword(phrase)}
	d := Draft{Account: a}
	type holder struct{ d Draft }
	for name, v := range map[string]any{"account": a, "draft": d, "pointer": &d, "held": holder{d}, "slice": []Draft{d}} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
			text := fmt.Sprintf(verb, v)
			if strings.Contains(text, phrase) || strings.Contains(text, inHex) {
				t.Errorf("%s under %s prints the password", name, verb)
			}
		}
		if b, err := json.Marshal(v); err == nil && strings.Contains(string(b), phrase) {
			t.Errorf("%s through encoding/json carries the password", name)
		}
	}
}
