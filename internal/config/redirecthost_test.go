package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// TestLoad_RedirectHostsAreTheClosedListAndCollideWithNoServedHost (backlog T71): Load sets
// RedirectHosts to the two redirect hosts whatever the environment says, and refuses a base URL,
// an operator host or an API host equal to one of them -- each of those would be answered with a
// redirect instead of served. The refusal names the variable.
//
// MUTANTS (measured): each of the three collision cases in redirectHosts removed -> its row red;
// the list emptied -> the first row red.
func TestLoad_RedirectHostsAreTheClosedListAndCollideWithNoServedHost(t *testing.T) {
	want := []string{"tappa.everva.com.tr", "www.taptime.mt"}
	t.Run("the closed list", func(t *testing.T) {
		setRequired(t)
		t.Setenv("TAPPA_BASE_URL", "https://taptime.mt")
		c, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(c.RedirectHosts, want) {
			t.Fatalf("RedirectHosts %q, want %q", c.RedirectHosts, want)
		}
		// A copy: a caller that edits its Config does not edit the next Load's list.
		c.RedirectHosts[0] = "changed.example"
		if c2, err := config.Load(); err != nil || !slices.Equal(c2.RedirectHosts, want) {
			t.Fatalf("after editing one Config, the next Load gave %q (%v)", c2.RedirectHosts, err)
		}
	})
	for _, tc := range []struct {
		name, variable string
		set            func(t *testing.T)
	}{
		{"the base URL's host", "TAPPA_BASE_URL", func(t *testing.T) {
			t.Setenv("TAPPA_BASE_URL", "https://www.taptime.mt")
		}},
		{"the base URL's host, another case of the other one", "TAPPA_BASE_URL", func(t *testing.T) {
			t.Setenv("TAPPA_BASE_URL", "http://tappa.everva.com.tr:8080")
		}},
		{"the operator host", "TAPPA_OPERATOR_HOST", func(t *testing.T) {
			setOperator(t)
			t.Setenv("TAPPA_OPERATOR_HOST", "www.taptime.mt")
		}},
		{"the API host", "TAPPA_API_HOST", func(t *testing.T) {
			t.Setenv("TAPPA_API_HOST", "tappa.everva.com.tr")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("TAPPA_BASE_URL", "https://taptime.mt")
			tc.set(t)
			_, err := config.Load()
			if err == nil {
				t.Fatal("loaded")
			}
			if !strings.Contains(err.Error(), tc.variable+": ") || !strings.Contains(err.Error(), "redirect host") {
				t.Errorf("refused without naming %s and the redirect host: %q", tc.variable, err.Error())
			}
		})
	}
	t.Run("CONTROL: the shipped operator and API hosts load", func(t *testing.T) {
		setRequired(t)
		t.Setenv("TAPPA_BASE_URL", "https://taptime.mt")
		setOperator(t)
		t.Setenv("TAPPA_API_HOST", "api.taptime.mt")
		if _, err := config.Load(); err != nil {
			t.Fatal(err)
		}
	})
}
