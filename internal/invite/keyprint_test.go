package invite

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestManager_PrintsNoKey (backlog T205): %+v of an invite Manager, or of a struct holding one by value, used to print its key.
func TestManager_PrintsNoKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte('A' + i)
	}
	key := config.NewKey(raw)
	bare := fmt.Sprint(raw)
	for name, v := range map[string]any{
		"manager":  &Manager{hmacKey: key},
		"by value": Manager{hmacKey: key},
		"held":     struct{ m Manager }{Manager{hmacKey: key}},
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
