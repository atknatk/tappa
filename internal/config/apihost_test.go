package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestLoad_APIHostIsOptionalAndOneSpelling (M11 API-4, ADR 0028 §13, K-9): TAPPA_API_HOST unset is
// "no API" and loads with an empty host; set, it is the operator host's one spelling (a lower-case DNS
// name and nothing else), and never TAPPA_BASE_URL's host or TAPPA_OPERATOR_HOST. Each refusal names
// the variable and never repeats the value.
func TestLoad_APIHostIsOptionalAndOneSpelling(t *testing.T) {
	t.Run("unset is no API", func(t *testing.T) {
		setRequired(t)
		c, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if c.APIHost != "" {
			t.Fatalf("APIHost %q with the variable unset", c.APIHost)
		}
	})
	for _, h := range []string{"api.taptime.mt", "api.localhost", "api"} {
		t.Run("good/"+h, func(t *testing.T) {
			setRequired(t)
			t.Setenv("TAPPA_BASE_URL", "https://taptime.mt")
			t.Setenv("TAPPA_API_HOST", h)
			c, err := config.Load()
			if err != nil {
				t.Fatalf("%q refused: %v", h, err)
			}
			if c.APIHost != h {
				t.Fatalf("APIHost %q, want %q", c.APIHost, h)
			}
		})
	}
	secretish := "postgres://" + "tappa_app:" + "hunter2@db/tappa"
	for i, h := range []string{
		"API.taptime.mt", "api.taptime.mt.", "api.taptime.mt:443", "https://api.taptime.mt", "api.taptime.mt/v1",
		"user@api.taptime.mt", " api.taptime.mt", "api..taptime.mt", "-api.taptime.mt", "api_x.taptime.mt", "[::1]",
		"   ", secretish,
	} {
		t.Run(fmt.Sprintf("bad/%d", i), func(t *testing.T) {
			setRequired(t)
			t.Setenv("TAPPA_API_HOST", h)
			_, err := config.Load()
			if err == nil {
				t.Fatalf("bad host #%d (%q) loaded", i, h)
			}
			if !strings.Contains(err.Error(), "TAPPA_API_HOST") {
				t.Errorf("refused without naming the variable: %q", err.Error())
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("the refusal repeats the value: %q", err.Error())
			}
		})
	}
	for _, tc := range []struct{ base, host string }{
		{"https://taptime.mt", "taptime.mt"},
		{"https://API.TapTime.MT", "api.taptime.mt"},
	} {
		t.Run("base host/"+tc.host, func(t *testing.T) {
			setRequired(t)
			t.Setenv("TAPPA_BASE_URL", tc.base)
			t.Setenv("TAPPA_API_HOST", tc.host)
			if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "TAPPA_BASE_URL") {
				t.Fatalf("the base URL's host as the API host: %v", err)
			}
		})
	}
	t.Run("the operator host", func(t *testing.T) {
		setRequired(t)
		setOperator(t)
		t.Setenv("TAPPA_BASE_URL", "https://taptime.mt")
		t.Setenv("TAPPA_OPERATOR_HOST", "ops.taptime.mt")
		t.Setenv("TAPPA_API_HOST", "ops.taptime.mt")
		if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "TAPPA_OPERATOR_HOST") {
			t.Fatalf("the operator host as the API host: %v", err)
		}
		t.Setenv("TAPPA_API_HOST", "api.taptime.mt")
		if _, err := config.Load(); err != nil {
			t.Fatalf("CONTROL: api.taptime.mt beside ops.taptime.mt: %v", err)
		}
	})
}
