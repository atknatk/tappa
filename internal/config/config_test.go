package config_test

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/policy"
)

// setRequired sets every REQUIRED env var to a valid value and clears the two
// bounded ones to their defaults, so a test can vary exactly one input and reach
// the range checks. config.Load collects ALL errors, so even with everything
// else valid a single out-of-range value surfaces as a startup failure.
func setRequired(t *testing.T) {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32)) // decodes to 32 bytes
	t.Setenv("DATABASE_URL", "postgres://app@localhost/tappa")
	t.Setenv("DATABASE_MIGRATE_URL", "") // must NOT equal DATABASE_URL
	t.Setenv("TAPPA_SESSION_HMAC_KEY", key)
	t.Setenv("TAPPA_TAG_KEK", key)
	// The invite key must DIFFER from the session key (Load rejects equality), so
	// this fixture cannot reuse `key` the way TAPPA_TAG_KEK does.
	t.Setenv("TAPPA_INVITE_HMAC_KEY", otherKey)
	t.Setenv("TAPPA_RETENTION_YEARS", "2") // required, no default
	t.Setenv("TAPPA_TRUSTED_PROXIES", "")
	t.Setenv("TAPPA_GPS_RADIUS_M", "")     // -> default 150
	t.Setenv("TAPPA_DEBOUNCE_SECONDS", "") // -> default 60
}

// otherKey is a valid 32-byte key that is NOT all zeroes, so it differs from the
// one setRequired gives the session.
var otherKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

// TestLoad_InviteKeyMustDifferFromSessionKey measures the domain-separation
// guard: the two MAC keys may not be the same bytes.
//
// The POSITIVE CONTROL is the other half of the test — with distinct keys the
// same configuration loads cleanly — so a green result cannot come from the
// configuration being broken for some unrelated reason.
func TestLoad_InviteKeyMustDifferFromSessionKey(t *testing.T) {
	setRequired(t)
	same := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	t.Setenv("TAPPA_SESSION_HMAC_KEY", same)
	t.Setenv("TAPPA_INVITE_HMAC_KEY", same)

	if _, err := config.Load(); err == nil {
		t.Fatal("identical session and invite keys must be refused at startup")
	} else if !strings.Contains(err.Error(), "TAPPA_INVITE_HMAC_KEY") {
		t.Errorf("error should name the offending variable, got %v", err)
	}

	// Positive control: change one byte and the same environment loads.
	t.Setenv("TAPPA_INVITE_HMAC_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{10}, 32)))
	c, err := config.Load()
	if err != nil {
		t.Fatalf("distinct keys must load: %v", err)
	}
	if len(c.InviteHMACKey) != 32 {
		t.Errorf("invite key length = %d, want 32", len(c.InviteHMACKey))
	}
}

// TestLoad_RetentionYearsIsRequiredAndBounded covers the value the GDPR notice
// renders. It is REQUIRED (a missing legal figure must not become a silent
// default) and bounded against typos.
func TestLoad_RetentionYearsIsRequiredAndBounded(t *testing.T) {
	cases := []struct {
		name string
		val  string
		ok   bool
	}{
		{"unset is a startup failure", "", false},
		{"zero would render \"kept for 0 years\"", "0", false},
		{"negative", "-1", false},
		{"at min", "1", true},
		{"typical", "5", true},
		{"at max", "30", true},
		{"above max is indistinguishable from forever", "31", false},
		{"non-numeric", "two", false},
		{"float is not whole years", "2.5", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("TAPPA_RETENTION_YEARS", tc.val)
			c, err := config.Load()
			if tc.ok && err != nil {
				t.Fatalf("%q should load: %v", tc.val, err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatalf("%q should be refused", tc.val)
				}
				if !strings.Contains(err.Error(), "TAPPA_RETENTION_YEARS") {
					t.Errorf("error should name the variable, got %v", err)
				}
				return
			}
			want, _ := strconv.Atoi(tc.val)
			if c.RetentionYears != want {
				t.Errorf("RetentionYears = %d, want %d", c.RetentionYears, want)
			}
		})
	}
}

func TestLoad_DefaultsWithinRange(t *testing.T) {
	setRequired(t)
	c, err := config.Load()
	if err != nil {
		t.Fatalf("defaults should load cleanly: %v", err)
	}
	if c.GPSRadiusMeters != 150 {
		t.Errorf("default GPS radius = %v, want 150", c.GPSRadiusMeters)
	}
	if c.Debounce.Seconds() != 60 {
		t.Errorf("default debounce = %v, want 60s", c.Debounce)
	}
}

// TestLoad_GPSRadiusRange proves the bound reads policy's constants (single
// source) and rejects both ends — including the Y-L park-wide-disable value that
// the old bare `> 0` check let through.
func TestLoad_GPSRadiusRange(t *testing.T) {
	lo, hi := policy.GPSRadiusMinM, policy.GPSRadiusMaxM
	cases := []struct {
		name string
		val  string
		ok   bool
	}{
		{"below min", strconv.Itoa(lo - 1), false},
		{"at min", strconv.Itoa(lo), true},
		{"at max", strconv.Itoa(hi), true},
		{"just above max", strconv.Itoa(hi + 1), false},
		{"Y-L park-wide disable", "20000000", false},
		{"non-numeric", "abc", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("TAPPA_GPS_RADIUS_M", tc.val)
			_, err := config.Load()
			switch {
			case tc.ok && err != nil:
				t.Fatalf("value %q should load, got %v", tc.val, err)
			case !tc.ok && err == nil:
				t.Fatalf("value %q should be a startup error", tc.val)
			case !tc.ok && !strings.Contains(err.Error(), "TAPPA_GPS_RADIUS_M"):
				t.Fatalf("error should name TAPPA_GPS_RADIUS_M, got %v", err)
			}
		})
	}
}

// TestLoad_DebounceRange proves the same for the debounce window (30..300 s).
func TestLoad_DebounceRange(t *testing.T) {
	lo, hi := policy.DebounceMinSeconds, policy.DebounceMaxSeconds
	cases := []struct {
		name string
		val  string
		ok   bool
	}{
		{"below min", strconv.Itoa(lo - 1), false},
		{"at min", strconv.Itoa(lo), true},
		{"at max", strconv.Itoa(hi), true},
		{"just above max", strconv.Itoa(hi + 1), false},
		{"non-numeric", "xyz", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("TAPPA_DEBOUNCE_SECONDS", tc.val)
			_, err := config.Load()
			switch {
			case tc.ok && err != nil:
				t.Fatalf("value %q should load, got %v", tc.val, err)
			case !tc.ok && err == nil:
				t.Fatalf("value %q should be a startup error", tc.val)
			case !tc.ok && !strings.Contains(err.Error(), "TAPPA_DEBOUNCE_SECONDS"):
				t.Fatalf("error should name TAPPA_DEBOUNCE_SECONDS, got %v", err)
			}
		})
	}
}

// TestLoad_EnvIsAClosedSet: TAPPA_ENV feeds IsProd, which internal/session reads
// to decide whether the session cookie carries Secure. A typo must be a startup
// failure, not a silent downgrade to the base-URL heuristic.
func TestLoad_EnvIsAClosedSet(t *testing.T) {
	t.Run("accepts every documented value", func(t *testing.T) {
		for _, v := range []string{config.EnvDev, config.EnvStaging, config.EnvProd} {
			setRequired(t)
			t.Setenv("TAPPA_ENV", v)
			c, err := config.Load()
			if err != nil {
				t.Fatalf("TAPPA_ENV=%q should load: %v", v, err)
			}
			if c.Env != v {
				t.Fatalf("Env = %q, want %q", c.Env, v)
			}
			if got, want := c.IsProd(), v == config.EnvProd; got != want {
				t.Fatalf("TAPPA_ENV=%q IsProd() = %v, want %v", v, got, want)
			}
		}
	})

	t.Run("unset falls back to the dev default", func(t *testing.T) {
		setRequired(t)
		t.Setenv("TAPPA_ENV", "")
		c, err := config.Load()
		if err != nil {
			t.Fatalf("an unset TAPPA_ENV must keep loading: %v", err)
		}
		if c.Env != config.EnvDev {
			t.Fatalf("default Env = %q, want %q", c.Env, config.EnvDev)
		}
	})

	t.Run("rejects near misses", func(t *testing.T) {
		// "production" is the one that matters: it reads as correct to a human
		// and makes IsProd() false.
		for _, v := range []string{"production", "Prod", "PROD", "prd", "develop", "test", "live", " prod"} {
			setRequired(t)
			t.Setenv("TAPPA_ENV", v)
			_, err := config.Load()
			if err == nil {
				t.Errorf("TAPPA_ENV=%q was accepted; it must be a startup failure", v)
				continue
			}
			if !strings.Contains(err.Error(), "TAPPA_ENV") {
				t.Errorf("TAPPA_ENV=%q error does not name the variable: %v", v, err)
			}
		}
	})
}

// TestLoad_TrustedProxiesDefaultRoute measures the gate added in M5-03.
//
// WHY IT EXISTS. internal/httpx walks X-Forwarded-For from the right and stops
// at the first UNTRUSTED hop. Trust everybody and there is no untrusted hop to
// stop at, so the walk runs off the left end and returns the entry the CLIENT
// wrote — proof of place (50 trust points, CLAUDE.md §5) and every abuse budget
// key become caller-chosen. The behaviour itself is measured next door in
// TestResolveClientIP_DefaultRouteReturnsTheClientsOwnClaim; this test is about
// the deployment never reaching that state in production.
func TestLoad_TrustedProxiesDefaultRoute(t *testing.T) {
	// Every spelling that means "everybody", INCLUDING the ones that only mean it
	// after the resolver is done with them. The last four are the audit's
	// blocking finding: this test used to try /0 forms only, so a v4-mapped
	// prefix — which config saw as Bits()==96 and the resolver treated as
	// 0.0.0.0/0 — had no row anywhere and shipped.
	defaults := []string{
		"0.0.0.0/0", "::/0", "10.0.0.0/8,0.0.0.0/0", "::/0, 127.0.0.1/32",
		"1.2.3.4/0",                      // /0 with host bits set
		"::ffff:0.0.0.0/96",              // v4-mapped: 0.0.0.0/0 to the resolver
		"::ffff:10.0.0.1/96",             // same range, host bits set
		"127.0.0.1/32,::ffff:0.0.0.0/96", // hidden inside an otherwise sane list
	}

	t.Run("prod refuses it", func(t *testing.T) {
		for _, v := range defaults {
			setRequired(t)
			t.Setenv("TAPPA_ENV", config.EnvProd)
			t.Setenv("TAPPA_TRUSTED_PROXIES", v)
			err := loadErr(t)
			if err == nil {
				t.Errorf("TAPPA_TRUSTED_PROXIES=%q was accepted in production", v)
				continue
			}
			if !strings.Contains(err.Error(), "TAPPA_TRUSTED_PROXIES") {
				t.Errorf("%q: the error does not name the variable: %v", v, err)
			}
		}
	})

	t.Run("non-prod allows it, loudly", func(t *testing.T) {
		// A developer testing the proxy path from a container cannot always
		// predict its address. The value is accepted and warned about; the
		// warning is a log line, so what is asserted here is that the
		// configuration still loads and carries the prefix.
		for _, env := range []string{config.EnvDev, config.EnvStaging} {
			setRequired(t)
			t.Setenv("TAPPA_ENV", env)
			t.Setenv("TAPPA_TRUSTED_PROXIES", "0.0.0.0/0")
			c, err := config.Load()
			if err != nil {
				t.Fatalf("TAPPA_ENV=%s: a default route must not be fatal outside production: %v", env, err)
			}
			if len(c.TrustedProxies) != 1 || c.TrustedProxies[0].Bits() != 0 {
				t.Fatalf("TAPPA_ENV=%s: prefixes = %v", env, c.TrustedProxies)
			}
		}
	})

	// The two refusals are NOT the same rule, and the difference is visible in
	// dev: a default route is a RISK JUDGEMENT (fatal in prod, warned about
	// elsewhere), while a v4-mapped prefix is an AMBIGUOUS SPELLING and is
	// refused EVERYWHERE. Ambiguity is never acceptable, because the whole
	// finding was a check and its consumer disagreeing about what a value meant.
	t.Run("the mapped spelling is refused even in dev", func(t *testing.T) {
		for _, v := range []string{"::ffff:0.0.0.0/96", "::ffff:10.0.0.0/104"} {
			setRequired(t)
			t.Setenv("TAPPA_ENV", config.EnvDev)
			t.Setenv("TAPPA_TRUSTED_PROXIES", v)
			err := loadErr(t)
			if err == nil {
				t.Errorf("TAPPA_TRUSTED_PROXIES=%q was accepted in dev", v)
				continue
			}
			// The message has to tell the operator what to write instead, or the
			// refusal just moves the confusion.
			if !strings.Contains(err.Error(), "IPv4 form") {
				t.Errorf("%q: the error does not name the form to use: %v", v, err)
			}
		}
		// Positive control for THIS rule: the same range in IPv4 form loads.
		setRequired(t)
		t.Setenv("TAPPA_ENV", config.EnvDev)
		t.Setenv("TAPPA_TRUSTED_PROXIES", "10.0.0.0/8")
		if err := loadErr(t); err != nil {
			t.Errorf("the IPv4 spelling of the same range must load: %v", err)
		}
	})

	// POSITIVE CONTROL: the gate must be about /0 and nothing else. A real
	// ingress list — including the shipped default and a broad-but-finite private
	// range — has to load in production, or the check would be a startup failure
	// invented out of a guess.
	t.Run("real ingress lists still load in prod", func(t *testing.T) {
		for _, v := range []string{"", "127.0.0.1/32", "10.0.0.0/8", "127.0.0.1/32,10.0.0.0/8,::1/128", "128.0.0.0/1"} {
			setRequired(t)
			t.Setenv("TAPPA_ENV", config.EnvProd)
			t.Setenv("TAPPA_TRUSTED_PROXIES", v)
			if err := loadErr(t); err != nil {
				t.Errorf("TAPPA_TRUSTED_PROXIES=%q must load in production: %v", v, err)
			}
		}
	})
}

// loadErr runs Load and returns only the error, so a caller asserting on
// acceptance does not have to name the config it is throwing away.
func loadErr(t *testing.T) error {
	t.Helper()
	_, err := config.Load()
	return err
}
