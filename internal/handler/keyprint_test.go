package handler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestSignedContexts_PrintNoKey (backlog T205): %+v of this package's signed-value types, or of a struct holding one by value, used to print its key.
func TestSignedContexts_PrintNoKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte('A' + i)
	}
	key := config.NewKey(raw)
	bare := fmt.Sprint(raw)
	for name, v := range map[string]any{
		"tap contexts":   tapContexts{key: key},
		"claim contexts": claimContexts{key: key},
		"signup state":   signupState{key: key},
		"admin choices":  adminChoices{key: key},
		"admin confirm":  adminConfirm{key: key},
		"held":           struct{ t tapContexts }{tapContexts{key: key}},
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
