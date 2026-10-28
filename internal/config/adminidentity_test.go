package config_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestLoad_AdminIdentityIsAClosedSetAndFailsClosed is the panel identity flag (ADR 0031 §8, M14-08):
// unset and "off" load as off, "on" as on, and every other spelling -- a case variant, a blank, a
// boolean somebody guessed -- is a STARTUP FAILURE that names the variable and the two values and
// does not repeat the value it refused.
func TestLoad_AdminIdentityIsAClosedSetAndFailsClosed(t *testing.T) {
	setRequired(t)
	for value, want := range map[string]bool{"": false, config.AdminIdentityOff: false, config.AdminIdentityOn: true} {
		t.Setenv(config.EnvAdminIdentity, value)
		c, err := config.Load()
		if err != nil {
			t.Fatalf("%s=%q must load: %v", config.EnvAdminIdentity, value, err)
		}
		if c.AdminIdentity != want {
			t.Errorf("%s=%q gave AdminIdentity=%v, want %v", config.EnvAdminIdentity, value, c.AdminIdentity, want)
		}
	}
	for _, bad := range []string{"On", "ON", "OFF", " on", "on ", "true", "1", "yes", "enabled", "qq-identity-sentinel"} {
		t.Setenv(config.EnvAdminIdentity, bad)
		_, err := config.Load()
		if err == nil {
			t.Errorf("%s=%q loaded: a guessed spelling would boot with the surfaces in a state nobody chose", config.EnvAdminIdentity, bad)
			continue
		}
		for _, must := range []string{config.EnvAdminIdentity, `"off"`, `"on"`, "ADR 0031"} {
			if !strings.Contains(err.Error(), must) {
				t.Errorf("%s=%q: the error does not say %s: %v", config.EnvAdminIdentity, bad, must, err)
			}
		}
		if bad == "qq-identity-sentinel" && strings.Contains(err.Error(), bad) {
			t.Errorf("the refusal repeats the value it refused: %v", err)
		}
	}
}

// TestAnnounceAdminIdentity_TheStateIsOnTheStartUpLine: one INFO line in each state, naming the
// variable and the value.
func TestAnnounceAdminIdentity_TheStateIsOnTheStartUpLine(t *testing.T) {
	for _, on := range []bool{false, true} {
		var buf bytes.Buffer
		config.AnnounceAdminIdentity(slog.New(slog.NewJSONHandler(&buf, nil)), on)
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 1 {
			t.Fatalf("%d lines, want exactly one: %q", len(lines), buf.String())
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
			t.Fatalf("not JSON: %v", err)
		}
		value, says := config.AdminIdentityOff, "OFF"
		if on {
			value, says = config.AdminIdentityOn, "ON"
		}
		if rec["level"] != "INFO" || rec["variable"] != config.EnvAdminIdentity || rec["admin_identity"] != value ||
			!strings.Contains(rec["msg"].(string), says) {
			t.Fatalf("line = %v, want INFO, variable %s, admin_identity %q, msg saying %s", rec, config.EnvAdminIdentity, value, says)
		}
	}
}
