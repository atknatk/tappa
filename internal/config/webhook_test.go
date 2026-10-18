package config_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestConfig_WebhookSettingsAreRequiredOutsideDev (M11 API-9; ADR 0028 §6, §7 -- "liste boşsa prod ve
// sandbox başlamaz"; this branch has no sandbox value yet, so the rule is "every TAPPA_ENV but dev"):
// in prod and staging a missing TAPPA_WEBHOOK_KEK and a missing TAPPA_WEBHOOK_BLOCKED_ADDRESSES each
// refuse to start, naming the variable; in dev both may be absent (the panel says the webhook is not
// configured; the client dials with the registry rules alone). With both set, prod loads.
//
// MUTANT (measured): webhookSettings returning nil -> prod loads without the list, red.
func TestConfig_WebhookSettingsAreRequiredOutsideDev(t *testing.T) {
	untaggedOnly(t)
	for _, env := range []string{config.EnvProd, config.EnvStaging} {
		t.Run(env+" control", func(t *testing.T) {
			setRequired(t)
			t.Setenv("TAPPA_ENV", env)
			c, err := config.Load()
			if err != nil {
				t.Fatalf("both set: %v", err)
			}
			if len(c.WebhookKEK) != 32 || len(c.WebhookBlockedAddresses) != 2 {
				t.Fatalf("KEK %d bytes, %d blocked entries", len(c.WebhookKEK), len(c.WebhookBlockedAddresses))
			}
		})
		for _, v := range []string{"TAPPA_WEBHOOK_KEK", "TAPPA_WEBHOOK_BLOCKED_ADDRESSES"} {
			t.Run(env+" without "+v, func(t *testing.T) {
				setRequired(t)
				t.Setenv("TAPPA_ENV", env)
				t.Setenv(v, "")
				_, err := config.Load()
				if err == nil || !strings.Contains(err.Error(), v+" is required outside TAPPA_ENV=dev") {
					t.Fatalf("got %v", err)
				}
			})
		}
	}
	t.Run("dev without either loads", func(t *testing.T) {
		setRequired(t)
		t.Setenv("TAPPA_WEBHOOK_KEK", "")
		t.Setenv("TAPPA_WEBHOOK_BLOCKED_ADDRESSES", "")
		c, err := config.Load()
		if err != nil || c.WebhookKEK != nil || c.WebhookBlockedAddresses != nil {
			t.Fatalf("dev: %v", err)
		}
	})
}

// TestConfig_WebhookBlockedAddressesParse: single addresses become host prefixes, ranges are
// masked, both families are read; a v4-mapped spelling, a /0, a zone and junk are refused naming the
// entry's position.
func TestConfig_WebhookBlockedAddressesParse(t *testing.T) {
	setRequired(t)
	t.Setenv("TAPPA_WEBHOOK_BLOCKED_ADDRESSES", " 144.76.158.60 , 2a01:4f8::1, 198.51.100.7/24,2001:db8::/48")
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"144.76.158.60/32", "2a01:4f8::1/128", "198.51.100.0/24", "2001:db8::/48"}
	if len(c.WebhookBlockedAddresses) != len(want) {
		t.Fatalf("%v", c.WebhookBlockedAddresses)
	}
	for i, w := range want {
		if c.WebhookBlockedAddresses[i] != netip.MustParsePrefix(w) {
			t.Errorf("entry %d: %v, want %s", i, c.WebhookBlockedAddresses[i], w)
		}
	}
	for _, c := range []struct{ v, say string }{
		{"::ffff:144.76.158.60", "entry 1 is IPv4 written as v4-mapped"},
		{"1.2.3.4,0.0.0.0/0", "entry 2 is a /0"},
		{"fe80::1%eth0", "entry 1 is not an IP address"},
		{"taptime.mt", "entry 1 is not an IP address"},
		{"1.2.3.4,,5.6.7.8", "entry 2 is not an IP address"},
	} {
		setRequired(t)
		t.Setenv("TAPPA_WEBHOOK_BLOCKED_ADDRESSES", c.v)
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), c.say) {
			t.Errorf("%q: %v, want %q", c.v, err, c.say)
		}
	}
}
