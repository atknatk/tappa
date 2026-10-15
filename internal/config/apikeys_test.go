package config_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// allKeyVariables are the eight key variables Config holds (M11 API-3). That the list covers
// every key FIELD of Config is TestNamedKeys_ListEveryKeyFieldOfTheConfig's job.
var allKeyVariables = []string{
	"TAPPA_SESSION_HMAC_KEY", "TAPPA_TAG_KEK", "TAPPA_TAG_KEK_PREVIOUS", "TAPPA_INVITE_HMAC_KEY",
	"TAPPA_API_KEY_HMAC_KEY", "TAPPA_WEBHOOK_KEK", "TAPPA_OPERATOR_TOTP_KEK", "TAPPA_OPERATOR_TOKEN_HMAC_KEY",
}

// setEveryKeyDistinct sets the required variables and every one of the eight keys to its own
// random value (the operator surface on, so its two keys are read).
func setEveryKeyDistinct(t *testing.T) {
	t.Helper()
	setRequired(t)
	setOperator(t)
	for _, n := range allKeyVariables {
		k, _ := randKey(t)
		t.Setenv(n, k)
	}
}

// TestConfig_EqualEnvKeysRefuseToStart is ADR 0028 §3's acceptance ("eşit env anahtarıyla
// başlamaz"): TAPPA_API_KEY_HMAC_KEY and TAPPA_WEBHOOK_KEK are in namedKeys and EVERY unordered
// pair of the eight keys is compared -- the rule that compared only pairs with an operator key
// (config.go:688-692 before API-3) is general now. Each of the 28 pairs set equal is a refusal
// naming both variables (the two pairs with a dedicated check -- session/invite, tag KEK/previous --
// name both in their own sentence); with all eight distinct the configuration loads. And the API
// key's HMAC key is required: unset, short or not base64 -- no start; the webhook KEK is optional
// in dev (required elsewhere since API-9 -- TestConfig_WebhookSettingsAreRequiredOutsideDev), and
// when set must be 32 bytes.
//
// MUTANT (measured): keysSeparation's inner loop restricted to pairs with an operator key (the
// pre-API-3 rule) -> 13 of the 28 pairs load.
func TestConfig_EqualEnvKeysRefuseToStart(t *testing.T) {
	t.Run("control: eight distinct keys load", func(t *testing.T) {
		setEveryKeyDistinct(t)
		c, err := config.Load()
		if err != nil {
			t.Fatalf("eight distinct keys must load: %v", err)
		}
		if len(c.APIKeyHMACKey) != 32 || len(c.WebhookKEK) != 32 {
			t.Fatalf("the API key's HMAC key is %d bytes and the webhook KEK %d, want 32 and 32", len(c.APIKeyHMACKey), len(c.WebhookKEK))
		}
	})
	pairs := 0
	for i, a := range allKeyVariables {
		for _, b := range allKeyVariables[i+1:] {
			pairs++
			t.Run(a+"="+b, func(t *testing.T) {
				setEveryKeyDistinct(t)
				same, _ := randKey(t)
				t.Setenv(a, same)
				t.Setenv(b, same)
				_, err := config.Load()
				if err == nil {
					t.Fatalf("%s equal to %s loaded", a, b)
				}
				msg := err.Error()
				if !strings.Contains(msg, a) || !strings.Contains(msg, b) || !strings.Contains(msg, "must differ") {
					t.Errorf("the refusal does not name the identical pair %s / %s: %q", a, b, msg)
				}
				if strings.Contains(msg, same) {
					t.Errorf("the refusal repeats the key's value")
				}
			})
		}
	}
	if pairs != 28 {
		t.Fatalf("walked %d pairs, want 28", pairs)
	}

	for _, c := range []struct{ what, value, say string }{
		{"unset", "", "TAPPA_API_KEY_HMAC_KEY is required"},
		{"short", base64.StdEncoding.EncodeToString(make([]byte, 16)), "want 32 bytes"},
		{"not base64", "!!!not-base64!!!", "not valid base64"},
	} {
		t.Run("the API key's HMAC key "+c.what+" refuses to start", func(t *testing.T) {
			setEveryKeyDistinct(t)
			t.Setenv("TAPPA_API_KEY_HMAC_KEY", c.value)
			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), c.say) {
				t.Fatalf("got %v, want a refusal saying %q", err, c.say)
			}
		})
	}
	t.Run("the webhook KEK unset loads, short refuses", func(t *testing.T) {
		setEveryKeyDistinct(t)
		t.Setenv("TAPPA_WEBHOOK_KEK", "")
		c, err := config.Load()
		if err != nil || c.WebhookKEK != nil {
			t.Fatalf("an unset webhook KEK: %v, %d bytes; want a load and nil", err, len(c.WebhookKEK))
		}
		t.Setenv("TAPPA_WEBHOOK_KEK", base64.StdEncoding.EncodeToString(make([]byte, 31)))
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "TAPPA_WEBHOOK_KEK") {
			t.Fatalf("a 31-byte webhook KEK: %v, want a refusal naming it", err)
		}
	})
}
