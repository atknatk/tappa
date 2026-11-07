package endpoints

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestService_PrintsNoKEK (backlog T205): %+v of an endpoint Service, or of a struct holding one by value, used to print its key.
func TestService_PrintsNoKEK(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte('A' + i)
	}
	key := config.NewKey(raw)
	bare := fmt.Sprint(raw)
	for name, v := range map[string]any{
		"service":  &Service{kek: key},
		"by value": Service{kek: key},
		"held":     struct{ s Service }{Service{kek: key}},
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
