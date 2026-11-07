package apikey

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestAuthenticatorAndKeys_PrintNoKey (backlog T205): %+v of an Authenticator or a Keys, or of a struct holding one by value, used to print its key.
func TestAuthenticatorAndKeys_PrintNoKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte('A' + i)
	}
	key := config.NewKey(raw)
	bare := fmt.Sprint(raw)
	for name, v := range map[string]any{
		"authenticator": &Authenticator{key: key},
		"held auth":     struct{ a Authenticator }{Authenticator{key: key}},
		"keys":          &Keys{key: key},
		"held keys":     struct{ k Keys }{Keys{key: key}},
	} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%q", "%d"} {
			text := fmt.Sprintf(verb, v)
			if strings.Contains(text, string(raw)) || strings.Contains(text, fmt.Sprintf("%x", raw)) ||
				strings.Contains(text, bare[1:len(bare)-1]) {
				t.Errorf("%s under %s prints the key", name, verb)
			}
		}
	}
}
