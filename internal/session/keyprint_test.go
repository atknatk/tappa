package session

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestManager_PrintsNoKey (backlog T205): %+v of a Manager, or of a struct holding one, used to print the
// session HMAC key.
func TestManager_PrintsNoKey(t *testing.T) {
	m := &Manager{hmacKey: config.NewKey(fakeHMACKey)}
	bare := fmt.Sprint(fakeHMACKey)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%q", "%d"} {
		for _, v := range []any{m, *m, struct{ m Manager }{*m}} {
			text := fmt.Sprintf(verb, v)
			if strings.Contains(text, string(fakeHMACKey)) || strings.Contains(text, fmt.Sprintf("%x", fakeHMACKey)) ||
				strings.Contains(text, bare[1:len(bare)-1]) {
				t.Errorf("a Manager under %s prints the key", verb)
			}
		}
	}
}
