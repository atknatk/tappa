// Package config loads and validates runtime configuration from the environment.
//
// Rule: a missing or malformed required value is a startup failure, never a
// silent default. A server that boots with an empty session key or no trusted
// proxy list looks healthy while silently breaking proof-of-person and
// proof-of-place. See CLAUDE.md §4.
package config

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/policy"
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

	// InviteHMACKey keys the activation-code MAC (internal/invite). It is a
	// SEPARATE key from SessionHMACKey on purpose — see keySeparation below for
	// the measurement and the reasoning.
	InviteHMACKey []byte // 32 bytes

	GPSRadiusMeters float64
	Debounce        time.Duration

	// RetentionYears is how long attendance records are kept, in whole years. It
	// is REQUIRED and has no default because it is a LEGAL statement: the GDPR
	// Art. 13 notice on the activation page renders this number, and a number
	// that appeared out of a Go constant would be this repo inventing a legal
	// claim on a customer's behalf (M5-02, user decision 2026-07-31).
	//
	// The value in .env / .env.example is a DEVELOPMENT PLACEHOLDER, not legal
	// advice; the real figure is Q13 / backlog B3 and waits on a lawyer. The
	// activation page says so in the rendered text, so an employee reading a dev
	// deployment is not told a fabricated retention period.
	RetentionYears int

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

	// Env is a CLOSED SET because it is a security attribute, not a label.
	// IsProd is exact string equality, and internal/session reads it to decide
	// whether the session cookie gets Secure. TAPPA_ENV=production (or Prod, or a
	// typo) would make IsProd false, and a deployment that believes it is in
	// production would fall through to the base-URL heuristic. That is precisely
	// the "silent default" this package's doc comment forbids, so it is a startup
	// failure instead. The set is closed rather than open-with-a-warning: a new
	// environment name should be a deliberate edit here, next to IsProd.
	push(validEnv(c.Env))

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
	if c.InviteHMACKey, err = key32("TAPPA_INVITE_HMAC_KEY"); err != nil {
		push(err)
	}
	// The invite key must not simply BE the session key. See keySeparation.
	push(keySeparation(c.SessionHMACKey, c.InviteHMACKey))
	// RetentionYears is required (no default): see the field comment.
	if c.RetentionYears, err = intEnvRequiredRange("TAPPA_RETENTION_YEARS", retentionYearsMin, retentionYearsMax); err != nil {
		push(err)
	}
	if c.TrustedProxies, err = prefixes(env("TAPPA_TRUSTED_PROXIES", "")); err != nil {
		push(err)
	}
	// GPS radius and debounce are BOUNDED parameters (ADR 0004 §11): they read the
	// SAME min/max the policy engine declares, so a red line cannot be widened
	// through an env var. A bare `> 0` check let TAPPA_GPS_RADIUS_M=20000000
	// silently disable proof-of-place park-wide (Y-L); the upper bound now rejects
	// it at startup.
	if c.GPSRadiusMeters, err = floatEnvRange("TAPPA_GPS_RADIUS_M", 150, policy.GPSRadiusMinM, policy.GPSRadiusMaxM); err != nil {
		push(err)
	}
	if secs, err := floatEnvRange("TAPPA_DEBOUNCE_SECONDS", 60, policy.DebounceMinSeconds, policy.DebounceMaxSeconds); err != nil {
		push(err)
	} else {
		c.Debounce = time.Duration(secs * float64(time.Second))
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return c, nil
}

// EnvDev, EnvStaging and EnvProd are the only values TAPPA_ENV may take. Load
// rejects anything else; see the check in Load for why this is closed.
const (
	EnvDev     = "dev"
	EnvStaging = "staging"
	EnvProd    = "prod"
)

// envValues is the closed set, in the order the error message lists them.
var envValues = []string{EnvDev, EnvStaging, EnvProd}

func validEnv(v string) error {
	for _, ok := range envValues {
		if v == ok {
			return nil
		}
	}
	return fmt.Errorf("TAPPA_ENV: must be one of %s, got %q", strings.Join(envValues, ", "), v)
}

// IsProd reports whether this process runs in production. Callers use it to pick
// security defaults (internal/session hardens the session cookie on it), so the
// comparison is exact and Load guarantees Env is one of envValues.
//
// LIMITS, stated because they matter. Two ways this reports false without anyone
// having chosen it: (1) a Config built as a struct literal — tests, or future
// wiring that skips Load — bypasses the enum guarantee entirely, and a zero
// Config reports false; (2) an UNSET or empty TAPPA_ENV falls back to the "dev"
// default, which is deliberate (a developer must not have to set it) but means
// "not production" is also the answer when nobody said anything at all.
//
// WHAT IS AND IS NOT COMPENSATED — do not repeat the earlier version's mistake of
// closing this gap by pointing at internal/session. Cookies' zero value is
// Secure, which covers the case where NO Cookies was constructed at all; it does
// NOT cover this one, because NewCookies reads a CONSTRUCTED Config and simply
// believes it. A Config whose Env is missing, misspelled or bypassed takes the
// non-prod branch, and with a plain-http BaseURL that yields a session cookie
// without Secure — measured, not assumed. So:
//
//   - Load enforces the enum, and therefore rejects a WRONG value;
//   - nothing here can detect an ABSENT value, since absent is a valid default;
//   - the remaining defence is operational: a production deployment must set
//     TAPPA_ENV=prod (and should serve https, which makes BaseURL agree).
//
// IsProd is a convenience, never a security boundary by itself.
func (c *Config) IsProd() bool { return c.Env == EnvProd }

// retentionYearsMin / retentionYearsMax bound TAPPA_RETENTION_YEARS.
//
// THESE ARE SANITY BOUNDS, NOT A LEGAL RANGE — the distinction matters, because
// encoding a legal minimum here would be the very thing the field comment
// forbids. What they reject is a TYPO or an operator mistake: 0 (which would
// render "kept for 0 years" on a notice an employee is asked to consent to), a
// negative number, and a figure so large it is indistinguishable from "forever"
// (which GDPR storage limitation, Art. 5(1)(e), does not permit as a
// declaration). 30 is deliberately far above any employment-record retention
// period we have seen so that a genuine legal requirement is never rejected by
// this file.
const (
	retentionYearsMin = 1
	retentionYearsMax = 30
)

// keySeparation refuses a deployment where the invite MAC key and the session
// MAC key are the same bytes.
//
// WHY THIS CHECK EXISTS. internal/invite and internal/session both derive a hex
// HMAC-SHA256 over a 43-character base64url value, and both store the result in
// a `text` column with the same shape. Domain separation between the two is
// carried by TWO independent mechanisms (internal/invite/code.go states them):
// a separate KEY, and a labelled MAC input. The label alone would already make
// one MAC unusable as the other, so this check is not what makes the design
// sound — it catches the realistic OPERATIONAL failure, which is a copy-pasted
// .env where both variables hold the same generated key. That failure is
// invisible in every other way: the app boots, activation works, sessions work,
// and the independence the deployment believes it has is simply absent.
//
// Comparison is crypto/subtle.ConstantTimeCompare rather than bytes.Equal.
// There is no attacker-supplied input here (both values come from the process
// environment at startup), so this is hygiene, not a timing defence — but it
// costs nothing and keeps the rule "secrets are never compared with ==" from
// growing exceptions someone later copies (redline R7).
//
// A nil or short key is NOT reported here: key32 already pushed that error and a
// second message about the same variable would only be noise.
func keySeparation(sessionKey, inviteKey []byte) error {
	if len(sessionKey) != 32 || len(inviteKey) != 32 {
		return nil
	}
	if subtle.ConstantTimeCompare(sessionKey, inviteKey) == 1 {
		return errors.New("TAPPA_INVITE_HMAC_KEY must differ from TAPPA_SESSION_HMAC_KEY: they key two different credential types and sharing one key removes the independence the separation is for (generate: openssl rand -base64 32)")
	}
	return nil
}

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

// intEnvRequiredRange reads an integer env var that has NO DEFAULT: unset or
// empty is a startup failure, exactly like a missing key. It is the twin of
// floatEnvRange for a value where "the operator did not say" is not an
// acceptable answer (package doc: never a silent default).
//
// The range is INCLUSIVE at both ends, matching floatEnvRange, so the two
// helpers cannot disagree about what "within bounds" means.
func intEnvRequiredRange(name string, min, max int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, fmt.Errorf("%s is required (whole years, %d-%d; it is rendered in the GDPR Art. 13 notice, so this repo must not invent it)", name, min, max)
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("%s: must be within [%d, %d], got %d", name, min, max, v)
	}
	return v, nil
}

// floatEnvRange reads a float env var, returning def when unset, and enforces an
// INCLUSIVE [min,max] range (ADR 0004 §11). min/max come from the policy engine's
// bounded-parameter constants so config and the engine share one source: below
// min a protection is meaningless, above max it is effectively off — both are a
// startup failure, never a silent default (package doc).
func floatEnvRange(name string, def, min, max float64) (float64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("%s: must be within [%g, %g], got %v", name, min, max, v)
	}
	return v, nil
}
