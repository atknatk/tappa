package adminauth

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// TestManagerAndResets_PrintNoDerivedKey (backlog T80): a struct holding a Manager or a Resets, or
// either one printed with %+v, reaches their unexported key fields by reflection, where no method
// on the type can run. The keys are derived from TAPPA_SESSION_HMAC_KEY and hash every panel
// session and reset token, so each is held as a config.Key.
//
// CONTROL: the raw derived key does show under the same verbs, so the search is looking at the
// right bytes.
//
// MUTANT (measured, 2026-11-06, reverted): config.Key over a plain string instead of a *string --
// red here.
func TestManagerAndResets_PrintNoDerivedKey(t *testing.T) {
	session := make([]byte, hmacKeyLen)
	for i := range session {
		session[i] = byte(i + 1)
	}
	cfg := &config.Config{SessionHMACKey: config.NewKey(session)}
	manager, err := New(&db.DB{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	resets, err := NewResets(&db.DB{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	type derived struct {
		name string
		key  []byte
	}
	var keys []derived
	for name, derive := range map[string]func([]byte) ([]byte, error){
		"session": deriveKey, "reset": deriveResetKey, "proof": deriveProofKey, "verify": deriveVerifyKey, "link": deriveLinkKey,
	} {
		k, err := derive(session)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, derived{name, k})
	}
	// The %v of a []byte, without its brackets: the bytes as fmt prints them from a raw field.
	raw := func(k []byte) string { s := fmt.Sprint(k); return s[1 : len(s)-1] }
	for _, k := range keys {
		if !strings.Contains(fmt.Sprintf("%+v", struct{ key []byte }{k.key}), raw(k.key)) {
			t.Fatalf("CONTROL: the %s key does not show as a raw field; the search below looks at nothing", k.name)
		}
	}
	type holder struct {
		m *Manager
		r *Resets
	}
	for name, text := range map[string]string{
		"manager %+v": fmt.Sprintf("%+v", manager),
		"manager %#v": fmt.Sprintf("%#v", manager),
		"manager %v":  fmt.Sprintf("%v", manager),
		"resets %+v":  fmt.Sprintf("%+v", resets),
		"resets %#v":  fmt.Sprintf("%#v", resets),
		"holder %+v":  fmt.Sprintf("%+v", holder{manager, resets}),
	} {
		for _, k := range keys {
			if strings.Contains(text, raw(k.key)) || strings.Contains(text, fmt.Sprintf("%x", k.key)) ||
				strings.Contains(text, string(k.key)) || strings.Contains(text, fmt.Sprintf("%q", k.key)) {
				t.Errorf("%s prints the %s key", name, k.name)
			}
		}
	}
}
