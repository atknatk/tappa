// Package config loads and validates runtime configuration from the environment.
//
// Rule: a missing or malformed required value is a startup failure, never a
// silent default. A server that boots with an empty session key or no trusted
// proxy list looks healthy while silently breaking proof-of-person and
// proof-of-place. See CLAUDE.md §4.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env     string
	Addr    string
	BaseURL string

	// TrustedProxies bounds which hops may set X-Forwarded-For. Client IP is
	// proof-of-place; an unbounded proxy list lets a caller forge its location.
	TrustedProxies []netip.Prefix

	DatabaseURL string

	SessionHMACKey []byte // 32 bytes
	TagKEK         []byte // 32 bytes, wraps per-tag NTAG AES keys

	GPSRadiusMeters float64
	Debounce        time.Duration

	LogLevel string
}

func Load() (*Config, error) {
	c := &Config{
		Env:      env("TAPPA_ENV", "dev"),
		Addr:     env("TAPPA_ADDR", ":8080"),
		BaseURL:  env("TAPPA_BASE_URL", "http://localhost:8080"),
		LogLevel: env("TAPPA_LOG_LEVEL", "info"),
	}

	var errs []error
	push := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	c.DatabaseURL = os.Getenv("DATABASE_URL")
	if c.DatabaseURL == "" {
		push(errors.New("DATABASE_URL is required"))
	}
	// The app must never hold the migration role: RLS is skipped for table
	// owners and BYPASSRLS roles, which would silently void tenant isolation.
	if c.DatabaseURL != "" && c.DatabaseURL == os.Getenv("DATABASE_MIGRATE_URL") {
		push(errors.New("DATABASE_URL must not equal DATABASE_MIGRATE_URL: the app connects as tappa_app (NOBYPASSRLS), migrations as tappa_owner"))
	}

	var err error
	if c.SessionHMACKey, err = key32("TAPPA_SESSION_HMAC_KEY"); err != nil {
		push(err)
	}
	if c.TagKEK, err = key32("TAPPA_TAG_KEK"); err != nil {
		push(err)
	}
	if c.TrustedProxies, err = prefixes(env("TAPPA_TRUSTED_PROXIES", "")); err != nil {
		push(err)
	}
	if c.GPSRadiusMeters, err = floatEnv("TAPPA_GPS_RADIUS_M", 150); err != nil {
		push(err)
	}
	if secs, err := floatEnv("TAPPA_DEBOUNCE_SECONDS", 60); err != nil {
		push(err)
	} else {
		c.Debounce = time.Duration(secs * float64(time.Second))
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return c, nil
}

func (c *Config) IsProd() bool { return c.Env == "prod" }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func key32(name string) ([]byte, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return nil, fmt.Errorf("%s is required (generate: openssl rand -base64 32)", name)
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: not valid base64: %w", name, err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("%s: want 32 bytes, got %d", name, len(b))
	}
	return b, nil
}

func prefixes(s string) ([]netip.Prefix, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil // no proxy: RemoteAddr is used verbatim
	}
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		p, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("TAPPA_TRUSTED_PROXIES: %q: %w", part, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func floatEnv(name string, def float64) (float64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("%s: must be positive, got %v", name, v)
	}
	return v, nil
}
