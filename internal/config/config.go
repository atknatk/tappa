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
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/mail"
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

	// TagKEKPrevious is the KEK a plaque's wrapped key may ALSO be sealed under:
	// OPTIONAL, nil when unset, and 32 bytes when set.
	//
	// It exists for exactly one situation — a KEK rotation (cmd/rotatekek, and
	// the "KEK döndürme" runbook in deploy/README.md). Re-sealing the park is not
	// instantaneous, so for a while some rows are under the new KEK and some
	// under the old one. A process that knows only ONE of them answers 500 to
	// every tap on the other half, which is §4.6 ("kayıt asla kaybolmaz") failing
	// underneath the product rather than inside it. Holding both makes the mixed
	// park readable IN BOTH DIRECTIONS — while the rotation runs, and while a
	// rollback un-runs it.
	//
	// 🔴 IT IS A ROTATION WINDOW, NOT A SETTING. While this is set, a LEAKED KEK
	// still opens rows, so the rotation has not actually bought anything yet. The
	// runbook's last step is to unset it and roll out; that step is the rotation.
	TagKEKPrevious []byte

	// InviteHMACKey keys the activation-code MAC (internal/invite). It is a
	// SEPARATE key from SessionHMACKey on purpose — see keySeparation below for
	// the measurement and the reasoning.
	InviteHMACKey []byte // 32 bytes

	// The platform operator's surface (M10 OP-7; ADR 0020, ADR 0021): its own pool,
	// its two keys and its own host. FOUR VARIABLES, ONE SET — all four or none
	// (loadOperatorSurface): none leaves every field empty and the surface off
	// (/operator answers 503, the customer product is untouched); some but not all is
	// a startup failure naming which are missing.
	//
	// The two keys sit here as raw []byte like every other key in this struct, and
	// that is a recorded decision rather than an oversight (docs/plan/m10-platform.md,
	// OP-4 block, "Kabullere bağlananlar — OP-7", the 8th-round decision (1)):
	// internal/db imports this package, so this package cannot import
	// internal/operatorauth and cannot hold its redacting Key. cmd/tappa converts the
	// bytes with operatorauth.NewKey at the one place it builds the Authenticator.
	// Redacting this struct as a whole is backlog T80; nothing in this repository
	// formats a Config, and OP-7 adds no place that does.
	//
	// OperatorDatabaseURL is tappa_operator's DSN (internal/db.NewOperatorDB). It
	// carries a password, so no error in this package or in the pool's constructor
	// repeats it.
	OperatorDatabaseURL string
	// OperatorTOTPKEK seals and opens the operators' TOTP secrets (ADR 0020 §1);
	// 32 bytes, and different from every other key here (operatorKeySeparation).
	OperatorTOTPKEK []byte
	// OperatorTokenHMACKey keys the operator session token's hash (ADR 0020 §2);
	// 32 bytes, its OWN variable rather than a label on the session key, and
	// different from every other key here.
	OperatorTokenHMACKey []byte
	// OperatorHost is the separate host the operator surface lives on (ADR 0020 §4;
	// D-B: ops.taptime.mt). Validated as ONE spelling — a lower-case DNS name with no
	// scheme, port, path or user part — and never TAPPA_BASE_URL's own host. It is
	// configuration, not a secret: it is the one operator value a log line may name.
	OperatorHost string

	// ResetDelivery names the transport that carries an admin password-reset link
	// to the administrator's own address (M7-04 phase B, TAPPA_RESET_DELIVERY):
	// ResetDeliveryNone or ResetDeliveryEmail. Since M10 EM-9 the same value decides
	// the "your password was changed" notice too: one switch for both e-mails an
	// administrator receives (internal/handler's passwordnotice.go).
	//
	// Q02 IS ANSWERED by ADR 0022 (AWS SES in eu-central-1, spoken to as a plain SMTP
	// relay with STARTTLS), so "email" is a value this package ACCEPTS — and when it is
	// set, the transport's settings must be complete and valid (Mail, below), or the
	// process does not start. cmd/tappa builds the channel for it since M10 EM-5 (the
	// reset path's delivery runs off the request, ADR 0022 §6); the shipped ConfigMap
	// still says "none", and switching it is a deploy decision (ADR 0022 §12). The value
	// names the MECHANISM, not a provider — the provider is TAPPA_SMTP_HOST's business.
	//
	// 🔴 THERE IS NO INTERIM CHANNEL, WHICH IS THE DIFFERENCE FROM invites.
	// internal/invite ships ManagerVisibleChannel — an uncomfortable name for
	// showing an activation link on the manager's own screen — because a manager
	// seeing an employee's code is an ACCEPTED risk (ADR 0005 Y-D). The equivalent
	// here would be showing the reset link to whoever typed the address into a
	// PUBLIC form, and ADR 0015 says in one line what that is: "ele geçirmeyle
	// arasında duran şey SQL değil, ham token'ın yöneticinin kendi satırındaki
	// adrese teslim edilip çağırana asla döndürülmemesidir". A visible reset link
	// is an account takeover with a button on it. So the honest interim state is
	// "this deployment cannot send the link", and the screen says exactly that.
	//
	// AN UNKNOWN VALUE IS A STARTUP FAILURE, never a silent fallback: an operator
	// who writes TAPPA_RESET_DELIVERY=smtp must find out at boot that nothing
	// implements it, rather than from a customer who never received a link.
	ResetDelivery string

	// InviteDelivery names where an employee's activation link goes
	// (TAPPA_INVITE_DELIVERY): InviteDeliveryPanel — the manager's own screen,
	// internal/invite.ManagerVisibleChannel, an ACCEPTED risk (ADR 0005 Y-D) and the
	// shipped default — or InviteDeliveryEmail, the employee's own address (ADR 0022
	// §7): cmd/tappa builds the invitation's e-mail route for it since M10 EM-7B, from
	// the same transport as the reset flow's. Switching the shipped ConfigMap is a
	// deploy decision (ADR 0022 §12); an unknown value is a startup failure.
	InviteDelivery string

	// Mail is the transactional e-mail transport's configuration (ADR 0022 §5), in the
	// shape internal/mail.New takes. cmd/tappa calls mail.New with it ONCE when either
	// flow is "email" and gives that one transport to both (M10 EM-5, EM-7B).
	//
	// 🔴 IT IS THE ZERO VALUE UNLESS A FLOW ABOVE IS "email". With both flows off
	// (none + panel) the TAPPA_SMTP_* and TAPPA_MAIL_* variables are neither READ nor
	// VALIDATED (ADR 0022 §5): the runbook's intermediate state — credentials already in
	// the Secret, the ConfigMap still none/panel — must boot exactly as before, and a
	// field nothing should use stays empty so nothing can use it by accident. With a flow
	// on, every required setting must be present and valid, or Load fails naming the
	// VARIABLE (loadMail).
	//
	// Username and Password are mail.Credential, wrapped here, at load: they print as a
	// placeholder through this struct's exported fields
	// (TestLoad_MailCredentialsAreRedactedInTheConfig). That is all it buys — the rest of
	// this struct still holds raw keys, and redacting it as a whole is backlog T80.
	//
	// RootCAs IS NEVER SET (ADR 0022 §2): nil means the image's system roots, and no
	// variable of this package names a certificate authority. A private root would let
	// whoever holds it read the SMTP credentials and every link in transit.
	// TestMailConfig_NoProductCodeSetsTheRootPool and
	// TestPackaging_ConfigNamesNoCertificateVariable pin the two halves.
	Mail mail.Config

	GPSRadiusMeters float64
	Debounce        time.Duration

	// Freshness is how long a minted tap page stays usable before
	// sys:tap-freshness denies it (M5-10). It is a bounded parameter, read from
	// the SAME policy constants the engine declares (ADR 0004 §11), and it
	// reaches the guardrail through checkin.New — a value range-checked here and
	// then never plumbed is hand-off N3's exact failure, and it happened once
	// already with Debounce.
	//
	// 🔴 THE LIMIT THIS VALUE DOES NOT REACH, stated because it is a deliberate
	// user decision (2026-08-02) and not an oversight. A second, INDEPENDENT
	// ceiling bounds a tap page: the signed context's TTL (internal/handler,
	// tapContextTTL = 15 min), which refuses at parse time with an UNRECORDED
	// 400. So with the shipped 180 s window:
	//
	//	     age <= 180 s   ordinary tap
	//	180 s < age <= 900 s   RECORDED reject, verdict reject, sys:tap-freshness
	//	     age >  900 s   UNRECORDED 400 "tap again" (the TTL, not this value)
	//
	// THE TOP BAND IS A DECISION, AND CALLING IT A NECESSITY WOULD BE FALSE. Past
	// the TTL the MAC is refused, so the TAP is unverified — no verified tag,
	// counter, channel or location. The SESSION is not: the handler resolves the
	// identity before it parses the context, so the tenant and the employee are
	// authenticated at that moment, and migration 00005's nullable tag_uid would
	// have accepted an attributed row. Not writing one was chosen because the 400
	// is not silent (the page says to tap again), the person is at the plaque, and
	// there is no attendance event to record — see tapContextTTL in
	// internal/handler for the full statement. Narrowing this value widens the
	// RECORDED band; it never narrows the TTL.
	//
	// ⚠️ THE TOP OF THE RANGE IS AN ACCEPTED NO-OP. Setting 900 makes this value
	// equal the TTL, and since parse refuses an over-900 s context first, the
	// recorded band is EXACTLY EMPTY — the pre-M5-10 state, reached with no
	// warning (measured: window 900, page 870 s -> 200 ok / base:ip-or-gps-ok
	// instead of a sys:tap-freshness reject). It stays accepted because it is a
	// legal point of an ADR 0004 §11 range, but an operator who wants a recorded
	// band must stay BELOW 900.
	Freshness time.Duration

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

	// DevTools is the EXPLICIT opt-in for the development-only plaque-tap
	// simulator (ADR 0026, "Geliştirme aracı"): TAPPA_DEV_TOOLS=1. It is never
	// on by default, and Load REFUSES TO START when it is set on any TAPPA_ENV
	// other than dev — the simulator must not be enabled by the absence of
	// configuration (an unset TAPPA_ENV falls back to dev and an unset
	// TAPPA_BASE_URL to localhost, so those two alone are not a gate).
	DevTools bool

	// LogFormat is "text" or "json" and it is a DEPLOYMENT decision rather than a
	// taste, because the two readers of this process's output want opposite things
	// (M8-03, measured on the live cluster 2026-08-19).
	//
	//	dev        a person, through `go run` or `kubectl logs`. logfmt is readable.
	//	prod       an OpenTelemetry collector. The signoz k8s-infra-otel-agent
	//	           DaemonSet tails /var/log/pods/*/*/*.log and its exclude list does
	//	           NOT exclude this namespace, so every line here is already being
	//	           shipped. Its filelog receiver runs ONE operator, `container`,
	//	           which unwraps the CRI envelope and nothing else — there is no
	//	           logfmt parser in the pipeline, so a `key=value` body arrives as
	//	           one opaque string and the M8-03 alert rules would have to be
	//	           substring matches on it.
	//
	// The DEFAULT IS text, so nothing about a developer's terminal changes; the
	// ConfigMap sets json for the deployment that has a collector.
	LogFormat string
}

func Load() (*Config, error) {
	c := &Config{
		Env:      env("TAPPA_ENV", "dev"),
		Addr:     env("TAPPA_ADDR", ":8080"),
		BaseURL:  env("TAPPA_BASE_URL", "http://localhost:8080"),
		LogLevel: env("TAPPA_LOG_LEVEL", "info"),
		// 🔴 UNLIKE TAPPA_LOG_LEVEL, A TYPO HERE IS A STARTUP FAILURE. parseLevel
		// falls back to info on an unparseable level, which is the right shape for a
		// verbosity knob. It is the WRONG shape here: TAPPA_LOG_FORMAT=jsn would
		// silently give a prod deployment logfmt again, the collector would index
		// nothing, and every alert rule in deploy/README.md would match zero rows —
		// reading exactly like "no rejects, no 5xx, all healthy". A closed set makes
		// that a refusal to boot instead of a silent all-clear.
		LogFormat: env("TAPPA_LOG_FORMAT", LogFormatText),
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
	push(validLogFormat(c.LogFormat))
	devTools, devErr := devToolsFlag(c.Env)
	c.DevTools = devTools
	push(devErr)

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
	// TAPPA_TAG_KEK_PREVIOUS is OPTIONAL, but an optional value that is SET must
	// still be valid: unset means nil, and anything else must decode to 32 bytes
	// or the process refuses to start. Treating a malformed previous KEK as "not
	// set" would be the worst of both — the operator believes the rotation window
	// is open, every tap on a not-yet-re-sealed plaque answers 500, and nothing
	// says why.
	if c.TagKEKPrevious, err = optionalKey32("TAPPA_TAG_KEK_PREVIOUS"); err != nil {
		push(err)
	}
	// Equal KEKs are refused for the same reason cmd/rotatekek refuses them: the
	// only realistic way to get here is one value pasted into both variables, and
	// it would leave the fallback doing nothing while every surface reports a
	// rotation in progress.
	push(kekSeparation(c.TagKEK, c.TagKEKPrevious))
	if c.InviteHMACKey, err = key32("TAPPA_INVITE_HMAC_KEY"); err != nil {
		push(err)
	}
	// The invite key must not simply BE the session key. See keySeparation.
	push(keySeparation(c.SessionHMACKey, c.InviteHMACKey))
	// The operator surface: all four variables or none, then each one valid, then
	// neither operator key equal to any other key. It runs after every other key is
	// read because the last check compares against all of them.
	for _, err := range loadOperatorSurface(c) {
		push(err)
	}
	push(operatorKeySeparation(c))
	// RetentionYears is required (no default): see the field comment.
	if c.RetentionYears, err = intEnvRequiredRange("TAPPA_RETENTION_YEARS", retentionYearsMin, retentionYearsMax); err != nil {
		push(err)
	}
	if c.TrustedProxies, err = prefixes(env("TAPPA_TRUSTED_PROXIES", "")); err != nil {
		push(err)
	} else {
		push(trustedProxySanity(c.TrustedProxies, c.IsProd()))
	}
	if c.ResetDelivery, err = deliveryMode(envResetDelivery, ResetDeliveryNone, ResetDeliveryEmail); err != nil {
		push(err)
	}
	if c.InviteDelivery, err = deliveryMode(envInviteDelivery, InviteDeliveryPanel, InviteDeliveryEmail); err != nil {
		push(err)
	}
	// After both modes and TAPPA_ENV: whether the transport's settings are read at all
	// depends on the first, and what a host may be depends on the second.
	for _, err := range loadMail(c) {
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
	// 180 s (3 min) is the SHIPPED window, and it is a product choice rather than
	// the range's midpoint: the gap between the chip rewriting the URL and a
	// person pressing the button is seconds, plus unlocking a phone and reading
	// the screen on venue wifi. policy.DefaultParams() deliberately keeps the
	// range MAXIMUM as its no-config fallback, so this line is what makes the
	// guardrail's band non-empty in production — deleting it does not fail to
	// compile, it silently restores 900 s (see the DB test named in that file).
	if secs, err := floatEnvRange("TAPPA_FRESHNESS_SECONDS", 180, policy.FreshnessMinSeconds, policy.FreshnessMaxSeconds); err != nil {
		push(err)
	} else {
		c.Freshness = time.Duration(secs * float64(time.Second))
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

// The closed set for TAPPA_LOG_FORMAT (M8-03).
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// logFormatValues is the closed set, in the order the error message lists them.
var logFormatValues = []string{LogFormatText, LogFormatJSON}

func validLogFormat(v string) error {
	for _, ok := range logFormatValues {
		if v == ok {
			return nil
		}
	}
	return fmt.Errorf("TAPPA_LOG_FORMAT: must be one of %s, got %q", strings.Join(logFormatValues, ", "), v)
}

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

// kekSeparation refuses a previous KEK that is byte-identical to the primary.
//
// It is deliberately SILENT when previous is unset (nil), because unset is the
// steady state and by far the most common one — this must not become an error
// every deployment sees. The comparison is constant-time for the same reason
// keySeparation's is: it compares two secrets, and a startup path is still a
// path.
func kekSeparation(kek, previous []byte) error {
	if len(previous) == 0 {
		return nil
	}
	if len(kek) != 32 || len(previous) != 32 {
		return nil // the length errors above already say this; do not say it twice
	}
	if subtle.ConstantTimeCompare(kek, previous) == 1 {
		return errors.New("TAPPA_TAG_KEK_PREVIOUS must differ from TAPPA_TAG_KEK: it is the key the park is " +
			"being rotated AWAY from, so setting it to the current key opens a rotation window that rotates " +
			"nothing. Unset it when no rotation is in progress")
	}
	return nil
}

// The four variables of the platform operator's surface (M10 OP-7). They are
// constants so the set below, the loader and the error messages cannot spell one
// differently from another.
const (
	envOperatorDatabaseURL  = "TAPPA_OPERATOR_DATABASE_URL"
	envOperatorTOTPKEK      = "TAPPA_OPERATOR_TOTP_KEK"
	envOperatorTokenHMACKey = "TAPPA_OPERATOR_TOKEN_HMAC_KEY"
	envOperatorHost         = "TAPPA_OPERATOR_HOST"
)

// OperatorSurfaceVariables returns the four variables that configure the operator
// surface, in a fresh slice. They are ONE set: the loader refuses some-but-not-all,
// and deploy/k8s/20-app.yaml must take all four from tappa-secrets with
// `optional: true` (cmd/tappa's packaging test reads this list rather than a copy).
func OperatorSurfaceVariables() []string {
	return []string{envOperatorDatabaseURL, envOperatorTOTPKEK, envOperatorTokenHMACKey, envOperatorHost}
}

// OperatorSurfaceConfigured reports whether ANY operator field is set. Load
// guarantees all four or none; a Config built as a struct literal (a test, or wiring
// that skips Load) does not have that guarantee, and "any" is the fail-CLOSED reading
// of a partial one: the caller goes on to open the surface, and the constructors
// refuse the missing piece (an empty DSN, a key of the wrong size) instead of the
// surface silently staying off.
func (c *Config) OperatorSurfaceConfigured() bool {
	return c.OperatorDatabaseURL != "" || len(c.OperatorTOTPKEK) > 0 ||
		len(c.OperatorTokenHMACKey) > 0 || c.OperatorHost != ""
}

// loadOperatorSurface reads the four operator variables as ONE set.
//
// 🔴 SOME-BUT-NOT-ALL IS A STARTUP FAILURE, NOT A SURFACE THAT IS "PARTLY ON". A
// DSN without its KEK cannot sign anybody in, a KEK without its DSN has nothing to
// open, and a host without either names a surface that does not exist; each half-
// state would boot looking healthy and fail on the operator's first request, which
// is this package's "never a silent default" rule. NONE is the inert state and is
// not an error: the surface is off, /operator answers 503, and the customer product
// never notices (ADR 0020 §4, risk 7).
//
// "Set" means a non-empty value — the same reading optionalKey32 uses — so a value
// of blanks counts as set and must then be valid. For the DSN, valid starts with "not
// blank" (2b): pgx parses a DSN of whitespace as an EMPTY connection string — every
// default, the local socket — so the 2nd auditor's " " beside three valid variables
// booted with the surface "unavailable" instead of refusing; Load refuses it here.
// Messages name VARIABLES and
// lengths, never a value: the DSN carries a password, the keys are keys, and a
// mis-pasted value in the host variable could be either.
func loadOperatorSurface(c *Config) []error {
	var set, missing []string
	for _, name := range OperatorSurfaceVariables() {
		if os.Getenv(name) != "" {
			set = append(set, name)
		} else {
			missing = append(missing, name)
		}
	}
	switch {
	case len(set) == 0:
		return nil
	case len(missing) > 0:
		return []error{fmt.Errorf("the operator surface is configured by four variables together, and only some are set "+
			"(set: %s; missing: %s). Set all four, or none: with none the surface is off, /operator answers 503 and "+
			"the customer product is unaffected (deploy/README.md, operator surface runbook)",
			strings.Join(set, ", "), strings.Join(missing, ", "))}
	}
	var errs []error
	c.OperatorDatabaseURL = os.Getenv(envOperatorDatabaseURL)
	if strings.TrimSpace(c.OperatorDatabaseURL) == "" {
		errs = append(errs, errors.New("TAPPA_OPERATOR_DATABASE_URL: is set but holds only whitespace, which "+
			"PostgreSQL's client reads as an empty connection string (every default, a local socket). Set the "+
			"operator's DSN, or unset all four operator variables"))
	}
	var err error
	if c.OperatorTOTPKEK, err = key32(envOperatorTOTPKEK); err != nil {
		errs = append(errs, err)
	}
	if c.OperatorTokenHMACKey, err = key32(envOperatorTokenHMACKey); err != nil {
		errs = append(errs, err)
	}
	if c.OperatorHost, err = operatorHost(os.Getenv(envOperatorHost), c.BaseURL); err != nil {
		errs = append(errs, err)
	}
	return errs
}

// operatorHost validates TAPPA_OPERATOR_HOST and returns it unchanged.
//
// ONE SPELLING, REFUSED RATHER THAN NORMALISED (prefixes' lesson above, measured
// three times in this repository: a check and its consumer must see the same form).
// The value is compared with a request's host by OP-8's host gate; accepting
// "OPS.taptime.mt", "ops.taptime.mt." or "ops.taptime.mt:443" here would hand that
// gate a second spelling to get wrong. So the value is a lower-case DNS name —
// letters, digits, hyphens and dots, labels of 1-63 characters, no leading or
// trailing hyphen — and nothing else: no scheme, no port, no path, no user part.
// OP-8 decided the request side: httpx.OnHost drops a request's port and one trailing
// dot and ignores case before comparing with this one spelling.
//
// IT IS NEVER TAPPA_BASE_URL's HOST. ADR 0020 §4 puts the operator surface on its OWN
// host (its `__Host-` cookie, its same-origin check); the base URL's host in this
// variable would put it on the customer product's canonical host. The comparison
// ignores case because the base URL's host is not normalised anywhere.
// ⚠️ THAT IS THE ONLY HOST THIS CHECK KNOWS. The ingress serves the customer product on
// other hosts as well (deploy/k8s/40-ingress.yaml: www.taptime.mt, tappa.everva.com.tr),
// and an operator host equal to one of those is accepted here; keeping the operator
// surface off the customer hosts is the two-way host gate's job (OP-8: httpx's
// operatorHostOnly and internal/handler/operator's hostGate). What an operator host equal
// to a customer host does is measured and counted there (m10-platform.md, OP-8 card
// correction).
//
// The error never repeats the value: whatever was pasted into this variable by
// mistake — a DSN, a key — would otherwise reach the process log.
func operatorHost(v, baseURL string) (string, error) {
	if !isDNSHostName(v) {
		return "", errors.New("TAPPA_OPERATOR_HOST: must be a lower-case DNS host name such as ops.taptime.mt: " +
			"letters, digits, hyphens and dots only, with no scheme, port, path, user part or trailing dot " +
			"(the value is not repeated here, because a mis-pasted DSN or key would reach the log)")
	}
	if u, err := url.Parse(baseURL); err == nil && strings.EqualFold(u.Hostname(), v) {
		return "", errors.New("TAPPA_OPERATOR_HOST: must not be TAPPA_BASE_URL's host. The operator surface lives on " +
			"a host of its own, not on the customer product's canonical host (ADR 0020 §4)")
	}
	return v, nil
}

// isDNSHostName reports whether s is a lower-case DNS host name: 1-253 bytes, dot-
// separated labels of 1-63 bytes drawn from [a-z0-9-], none starting or ending with
// a hyphen. A single label (localhost) is a host name; an empty label (a leading,
// doubled or trailing dot) is not.
func isDNSHostName(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			b := label[i]
			if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '-' {
				return false
			}
		}
	}
	return true
}

// namedKey is one key of this Config with the variable it came from. operator marks
// the two keys operatorKeySeparation holds apart from everything else.
type namedKey struct {
	name     string
	v        []byte
	operator bool
}

// namedKeys is EVERY key this Config holds. TestNamedKeys_ListEveryKeyFieldOfTheConfig
// derives the set of []byte fields from the struct itself, so a key added to Config
// and not listed here is a red test rather than a key the separation rule never sees.
func (c *Config) namedKeys() []namedKey {
	return []namedKey{
		{"TAPPA_SESSION_HMAC_KEY", c.SessionHMACKey, false},
		{"TAPPA_TAG_KEK", c.TagKEK, false},
		{"TAPPA_TAG_KEK_PREVIOUS", c.TagKEKPrevious, false},
		{"TAPPA_INVITE_HMAC_KEY", c.InviteHMACKey, false},
		{envOperatorTOTPKEK, c.OperatorTOTPKEK, true},
		{envOperatorTokenHMACKey, c.OperatorTokenHMACKey, true},
	}
}

// operatorKeySeparation refuses an operator key that is byte-identical to ANY other
// key of this Config — the other operator key included (ADR 0020 §1, §2; the
// OP-7 acceptance).
//
// WHY, IN THIS REPOSITORY'S TERMS. The TOTP KEK seals every operator's second factor
// with the same AES-256-GCM layout internal/sun.Wrap uses for plaque keys (OP-6
// measured that a Wrap ref IS a Seal envelope: what keeps the two apart is the KEK
// and the AAD length), so a TOTP KEK equal to TAPPA_TAG_KEK collapses that separation
// to the AAD alone. The token HMAC key is its own variable precisely so that the
// operator's sessions are independent of the customer's keys
// (internal/adminauth/token.go's own warning), and an equal pair of bytes is that
// independence absent while every surface reports it present. The realistic cause is
// keySeparation's: one generated value pasted into two variables.
//
// ONLY PAIRS THAT INVOLVE AN OPERATOR KEY are checked. Widening the rule to every
// pair (say, the tag KEK against the invite key) would add refusals for a production
// configuration nobody has measured against them; keySeparation and kekSeparation
// keep their own two pairs. A key of the wrong length is skipped: key32 already
// reported it, and a second message about the same variable is noise.
//
// Constant time, for keySeparation's reason: hygiene on a path with no attacker
// input, so "secrets are never compared with ==" grows no exception.
func operatorKeySeparation(c *Config) error {
	keys := c.namedKeys()
	var clashes []string
	for i, a := range keys {
		if !a.operator || len(a.v) != 32 {
			continue
		}
		for j, b := range keys {
			// Each unordered pair once: an operator key against a key listed before it
			// is skipped when that key is an operator key too (it was compared already).
			if j == i || len(b.v) != 32 || (b.operator && j < i) {
				continue
			}
			if subtle.ConstantTimeCompare(a.v, b.v) == 1 {
				clashes = append(clashes, a.name+" = "+b.name)
			}
		}
	}
	if len(clashes) == 0 {
		return nil
	}
	return errors.New("operator keys must differ from each other and from every other key " +
		"(identical: " + strings.Join(clashes, "; ") + "). Generate each one separately: openssl rand -base64 32")
}

// optionalKey32 is key32 for a variable that may legitimately be absent: unset
// yields (nil, nil), and anything present must still be a valid 32-byte base64
// key.
func optionalKey32(name string) ([]byte, error) {
	if os.Getenv(name) == "" {
		return nil, nil
	}
	return key32(name)
}

// devToolsFlag reads TAPPA_DEV_TOOLS: unset or "0" is off, "1" is on, anything
// else is a startup error, and "1" outside TAPPA_ENV=dev is a startup error too.
func devToolsFlag(env string) (bool, error) {
	switch v := strings.TrimSpace(os.Getenv("TAPPA_DEV_TOOLS")); v {
	case "", "0":
		return false, nil
	case "1":
		if env != EnvDev {
			return false, fmt.Errorf("TAPPA_DEV_TOOLS=1 is only allowed with TAPPA_ENV=dev, got TAPPA_ENV=%q: "+
				"the plaque-tap simulator must never run on a shared deployment", env)
		}
		return true, nil
	default:
		return false, fmt.Errorf("TAPPA_DEV_TOOLS: must be 1 or unset, got %q", v)
	}
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

// trustedProxySanity refuses (in production) or shouts about (everywhere else) a
// TAPPA_TRUSTED_PROXIES that contains a DEFAULT ROUTE — 0.0.0.0/0 or ::/0.
//
// WHY THIS IS NOT PEDANTRY. internal/httpx walks X-Forwarded-For from the right
// and stops at the first UNTRUSTED address, which is what makes a forged
// left-hand entry worthless. Trust everyone and there is no untrusted address to
// stop at: the walk runs off the left end of the chain and returns the entry the
// CLIENT wrote. Measured in that package, not reasoned about — with 0.0.0.0/0
// and "X-Forwarded-For: 1.2.3.4, 5.6.7.8" the resolved client is 1.2.3.4. So a
// default route does not widen the trust boundary, it INVERTS the defence: proof
// of place (§5: an IP match is 50 of 100 trust points) and every abuse budget's
// key become caller-chosen. "TrustedProxies=everything" is strictly worse than
// "TrustedProxies=empty", which ignores the header entirely.
//
// PROD IS AN ERROR, NON-PROD IS A WARNING, following this package's own rule that
// a value which quietly breaks a security property must not be a silent default.
// A developer may genuinely want to test the proxy path from a container with an
// address they cannot predict; a production box has a known ingress and no
// reason for one.
//
// WHAT MAKES Bits()==0 SUFFICIENT — and it was NOT sufficient before this round.
// A check on a value is only as good as its agreement with the code that USES
// the value, and an audit measured the gap: ::ffff:0.0.0.0/96 reads as Bits()==96
// here and behaved as 0.0.0.0/0 in the resolver. That is fixed UPSTREAM, in
// prefixes, by refusing the v4-mapped spelling outright — so by the time this
// function runs, every range has exactly one writing and "/0" is the only way a
// SINGLE ENTRY can say "everybody". This check is complete over the values that
// can reach it BECAUSE of that refusal, not on its own merits.
//
// (The same lesson has now cost this repo three findings: HTTPS:// past a
// lower-case prefix test in M5-01, Cross-Site past a case-sensitive compare in
// M5-02, and this one. A validator must see the exact form its consumer sees.)
//
// LIMITS, stated rather than implied:
//   - This catches a SINGLE entry that means everybody. A UNION that covers the
//     space — "0.0.0.0/1,128.0.0.0/1" — is not detected. It takes deliberate
//     effort rather than one plausible spelling, and detecting coverage across
//     entries would mean this package deciding how wide an ingress may be.
//   - A /1, or any range far larger than a deployment's real ingress, is
//     accepted. How wide is too wide is a deployment fact this package cannot
//     know, and refusing a legitimate but broad range would be a startup failure
//     invented out of a guess.
func trustedProxySanity(ps []netip.Prefix, isProd bool) error {
	var defaults []string
	for _, p := range ps {
		if p.Bits() == 0 {
			defaults = append(defaults, p.String())
		}
	}
	if len(defaults) == 0 {
		return nil
	}
	const why = "trusting every address makes X-Forwarded-For fully forgeable: the resolver stops at the first UNTRUSTED hop, so with a default route it returns the value the client wrote (proof-of-place is worth 50 trust points, CLAUDE.md §5). List the real ingress addresses instead, or leave it empty to ignore the header entirely"
	if isProd {
		return fmt.Errorf("TAPPA_TRUSTED_PROXIES: refusing the default route %s: %s", strings.Join(defaults, ", "), why)
	}
	slog.Warn("TAPPA_TRUSTED_PROXIES contains a default route; this would be a startup failure in production",
		"prefixes", strings.Join(defaults, ", "), "why", why)
	return nil
}

// prefixes parses TAPPA_TRUSTED_PROXIES and enforces ONE SPELLING PER RANGE.
//
// 🔴 WHY THE 4-IN-6 FORM IS REFUSED RATHER THAN ACCEPTED. A security audit
// measured the previous arrangement, and it was the sharpest kind of bug: a
// check and the code it protects looking at two different representations of the
// same value. internal/httpx used to UNMAP a v4-mapped prefix (::ffff:0.0.0.0/96
// becomes 0.0.0.0/0 once the mapping's 96 bits come off), while the default-route
// gate below looked at the RAW prefix and saw Bits()==96. Measured end to end,
// with real config.Load and real httpx.NewRouter, TAPPA_ENV=prod and an ordinary
// internet caller as the peer:
//
//	0.0.0.0/0                       -> REFUSED
//	::ffff:0.0.0.0/96               -> loaded, and the CALLER CHOSE ITS OWN ADDRESS
//	::ffff:10.0.0.1/96              -> loaded, same
//	127.0.0.1/32,::ffff:0.0.0.0/96  -> loaded, same
//
// No error, no warning, and proof-of-place (50 trust points, §5) forgeable by
// anyone. And the spelling is not exotic: on a dual-stack box an operator SEES
// their proxy as ::ffff:10.0.0.1 and writes what they see.
//
// THE FIX IS TO DELETE THE SECOND REPRESENTATION, not to teach the gate about
// it. Two options existed. Teaching the gate to unmap would mean the same
// canonicalisation living in two packages that cannot share code (httpx imports
// config, so config cannot import httpx without a cycle, and inverting that would
// make the lowest layer depend on the HTTP router) — two copies of a security
// rule is how this bug was born. Refusing the form instead means there is exactly
// ONE way to write any range, so the gate and the resolver cannot look at
// different things: there is only one thing to look at.
//
// THE COST, stated: an operator who writes ::ffff:10.0.0.0/104 now gets a startup
// error naming the IPv4 form to use instead. That is worse than silently
// supporting it and far better than the two outcomes it replaces — silently
// trusting everybody, or (if the unmapping were simply removed) a trusted-proxy
// entry that matches nothing and is never noticed. internal/httpx DROPS the form
// as well, so a caller constructing RealIP directly cannot reach the walk with it
// either; this is the loud half, that is the fail-closed half.
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
		if p.Addr().Is4In6() {
			return nil, fmt.Errorf("TAPPA_TRUSTED_PROXIES: %q: write IPv4 ranges in IPv4 form (%s/%d), not as v4-mapped IPv6: the mapped form is a second spelling of the same range, and a second spelling is how a range slips past the checks on this value",
				strings.TrimSpace(part), p.Addr().Unmap(), max(p.Bits()-96, 0))
		}
		out = append(out, p)
	}
	return out, nil
}

// The two delivery modes (ADR 0022 §5). Each flow is a CLOSED SET of two: the value
// that sends nothing by e-mail, which is also the default, and "email".
const (
	// ResetDeliveryNone: no reset link is sent, and the panel's recovery form says so
	// on screen. See Config.ResetDelivery.
	ResetDeliveryNone = "none"
	// ResetDeliveryEmail: the reset link goes to the administrator's own address.
	ResetDeliveryEmail = "email"
	// InviteDeliveryPanel: the activation link is shown on the manager's panel.
	InviteDeliveryPanel = "panel"
	// InviteDeliveryEmail: the activation link goes to the employee's own address.
	InviteDeliveryEmail = "email"
)

// The transactional e-mail variables (ADR 0022 §5). They are constants so the loader,
// its messages and cmd/tappa's manifest tests cannot spell one differently from another.
//
// 🔴 THESE NAMES NEVER ENTER A fmt OR log CALL DIRECTLY. redline R7 reads the text of
// such a call — literals and identifiers alike — for words like the one the credential
// variable's name ends in, so a variable reaches a message only as the neutral `name`
// parameter of a helper (mailRequired, mailRule, smtpHost, smtpPort), key32's precedent.
// The identifiers are chosen to stay clear of those words too.
const (
	envResetDelivery  = "TAPPA_RESET_DELIVERY"
	envInviteDelivery = "TAPPA_INVITE_DELIVERY"
	envSMTPHost       = "TAPPA_SMTP_HOST"
	envSMTPPort       = "TAPPA_SMTP_PORT"
	envSMTPUser       = "TAPPA_SMTP_USERNAME"
	envSMTPPass       = "TAPPA_SMTP_PASSWORD"
	envMailFrom       = "TAPPA_MAIL_FROM"
	envMailReplyTo    = "TAPPA_MAIL_REPLY_TO"
)

// SMTPCredentialVariables returns the two variables that carry the relay's credentials,
// in a fresh slice. They are the only e-mail settings that are secrets:
// deploy/k8s/20-app.yaml must take both from tappa-secrets with `optional: true` (a
// deployment with both flows off never holds them), and neither may be a ConfigMap key
// (cmd/tappa's packaging test reads this list rather than a copy).
func SMTPCredentialVariables() []string { return []string{envSMTPUser, envSMTPPass} }

// smtpPortDefault is the STARTTLS submission port ADR 0022 §1 chose (and SES's).
const smtpPortDefault = 587

// smtpPortImplicitTLS is SMTPS, refused in every environment (ADR 0022 §5): on this
// port the server expects a TLS handshake before any SMTP, and internal/mail speaks
// SMTP first and upgrades with STARTTLS — the two would wait on each other until the
// send's deadline, on every send.
const smtpPortImplicitTLS = 465

// deliveryMode parses one flow's mode from the variable name.
//
// EMPTY IS off, AND AN UNKNOWN VALUE IS A STARTUP FAILURE. The package doc's rule is
// "never a silent default"; the value with a default here is the one that sends NOTHING
// by e-mail, and the one that must never be guessed at is a transport. Case and
// surrounding blanks are forgiven, because the set has two members and no two of them
// differ only in case. The message names the set and never repeats the value: a value
// pasted into the wrong variable could be anything, a credential included.
func deliveryMode(name, off, on string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch v {
	case "":
		return off, nil
	case off, on:
		return v, nil
	}
	return "", fmt.Errorf("%s: must be %q or %q (ADR 0022 §5; empty means %q). "+
		"The value is not repeated here", name, off, on, off)
}

// loadMail reads and validates the transport's settings into c.Mail — ONLY when a flow
// is "email" (Config.Mail says why both-off reads nothing).
//
// 🔴 FAIL-CLOSED, AND EVERY REFUSAL NAMES ITS VARIABLE AND NEVER ITS VALUE. A flow set
// to "email" with a setting missing would boot a process that fails on its first send
// — for a reset, silently, because the request answers the same either way (ADR 0022
// §6). So every required setting is checked here, all of them in one pass, each
// refusal its own error. The value is never repeated: these variables hold a
// credential, and a value pasted into the wrong one would reach the process log.
//
// THE RULES ARE NOT RESTATED HERE. What a sender, a Reply-To or a credential may be is
// internal/mail's rule (ADR 0022 §4, mail.New) and this function asks it (mailRule)
// rather than keeping a second copy that could drift. What is decided here is what only
// the deployment knows: which settings are required, the port, and — in production —
// what the relay's host may be (smtpHost).
func loadMail(c *Config) []error {
	var why string
	switch {
	case c.ResetDelivery == ResetDeliveryEmail && c.InviteDelivery == InviteDeliveryEmail:
		why = envResetDelivery + " and " + envInviteDelivery + " are " + ResetDeliveryEmail
	case c.ResetDelivery == ResetDeliveryEmail:
		why = envResetDelivery + " is " + ResetDeliveryEmail
	case c.InviteDelivery == InviteDeliveryEmail:
		why = envInviteDelivery + " is " + InviteDeliveryEmail
	default:
		return nil
	}
	var errs []error
	push := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	// "Set" means not blank: a value of spaces is refused as missing, the operator DSN's
	// precedent (loadOperatorSurface) — mail.New would accept a password of spaces.
	present := func(name string) (string, bool) {
		v := os.Getenv(name)
		if strings.TrimSpace(v) == "" {
			push(mailRequired(name, why))
			return "", false
		}
		return v, true
	}
	host, hostOK := present(envSMTPHost)
	if hostOK {
		push(smtpHost(envSMTPHost, host, c.IsProd()))
	}
	port, err := smtpPort(envSMTPPort, os.Getenv(envSMTPPort))
	push(err)
	user, userOK := present(envSMTPUser)
	if userOK {
		push(mailRule(envSMTPUser, func(m *mail.Config) { m.Username = mail.NewCredential(user) }))
	}
	pass, passOK := present(envSMTPPass)
	if passOK {
		push(mailRule(envSMTPPass, func(m *mail.Config) { m.Password = mail.NewCredential(pass) }))
	}
	from, fromOK := present(envMailFrom)
	if fromOK {
		push(mailRule(envMailFrom, func(m *mail.Config) { m.From = from }))
	}
	// OPTIONAL: unset means no Reply-To header (replies go to From). Set, it is held to
	// the RECIPIENT rule — one bare address — which is mail.New's to apply.
	replyTo := os.Getenv(envMailReplyTo)
	if replyTo != "" {
		push(mailRule(envMailReplyTo, func(m *mail.Config) { m.ReplyTo = replyTo }))
	}
	if len(errs) > 0 {
		return errs
	}
	c.Mail = mail.Config{
		Host:     host,
		Port:     port,
		Username: mail.NewCredential(user),
		Password: mail.NewCredential(pass),
		From:     from,
		ReplyTo:  replyTo,
	}
	return nil
}

// mailRequired is a missing e-mail setting's refusal, naming the variable and the flow
// that needs it.
func mailRequired(name, why string) error {
	return fmt.Errorf("%s is required while %s (ADR 0022 §5): the process would otherwise start and fail "+
		"on its first send. Set it before switching a flow to email (deploy/README.md, the transactional "+
		"e-mail runbook), or switch the flow back off", name, why)
}

// mailProbe is a configuration mail.New accepts, with no value from the environment in
// it. mailRule starts from it and replaces ONE field, so a refusal can only be about the
// value under test. The relay host is a reserved name (RFC 2606 .invalid) and nothing is
// dialled: mail.New builds a sender and checks its fields, it opens no connection.
func mailProbe() mail.Config {
	return mail.Config{
		Host:     "relay.invalid",
		Port:     smtpPortDefault,
		Username: mail.NewCredential("probe"),
		Password: mail.NewCredential("probe"),
		From:     "probe@relay.invalid",
	}
}

// mailRule asks internal/mail — the ONE place the sender, Reply-To and credential rules
// live (ADR 0022 §4) — whether a value is acceptable, and names the variable when it is
// not. mail.New's messages name its own field and never a value.
func mailRule(name string, set func(*mail.Config)) error {
	probe := mailProbe()
	set(&probe)
	if _, err := mail.New(probe); err != nil {
		return fmt.Errorf("%s: refused by the transport's own rule (ADR 0022 §4; the value is not repeated here): %w", name, err)
	}
	return nil
}

// smtpPort parses the relay's port: unset is 587; otherwise a decimal 1-65535, and
// never 465 (smtpPortImplicitTLS), in ANY environment — the refusal is about the
// protocol, not about how careful a deployment has to be.
func smtpPort(name, raw string) (int, error) {
	if raw == "" {
		return smtpPortDefault, nil
	}
	p, err := strconv.Atoi(raw)
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("%s: must be a port number, 1-65535 (unset means %d; the value is not repeated here)",
			name, smtpPortDefault)
	}
	if p == smtpPortImplicitTLS {
		return 0, fmt.Errorf("%s: %d is implicit TLS (SMTPS) and is refused in every environment: the transport "+
			"speaks SMTP first and upgrades with STARTTLS, so on that port both ends wait for the other until "+
			"every send times out (ADR 0022 §5). Use %d", name, smtpPortImplicitTLS, smtpPortDefault)
	}
	return p, nil
}

// smtpHost validates the relay's host.
//
// EVERYWHERE: ONE SPELLING — a lower-case DNS name (isDNSHostName, operatorHost's rule:
// no scheme, port, path or trailing dot, refused rather than normalised) or an IP
// address literal. The one spelling is what keeps the production rule below from
// being walked around by CASE OR A TRAILING DOT: "LOCALHOST" and "localhost." never get
// that far. It does not make the rule complete over loopback names — see the limits.
//
// IN PRODUCTION, NOT `localhost`, NOT `*.localhost`, NOT AN ADDRESS (ADR 0022 §5): `localhost`,
// any `*.localhost` (RFC 6761: loopback), an IP literal (netip.ParseAddr accepts it),
// and a name whose LAST label starts with a digit. That last one is how an IPv4 address
// is spelled in the forms C resolvers accept (127.1, 0x7f000001) and no top-level domain
// starts with a digit. Why the relay must be a name: the TLS certificate is verified
// against it, and the production relay is a public service behind a public certificate;
// net/smtp's own PlainAuth sends credentials in clear to exactly `localhost`,
// `127.0.0.1` and `::1` (ADR 0022 S3) — internal/mail refuses AUTH without STARTTLS
// itself, and this is the second, independent fence. Development may point anywhere
// (ADR 0022 §12: an `email` mode there is an accepted, counted risk).
//
// WHAT THIS DOES NOT DO: it does not hold the host to a list of allowed relays — a
// writer of the ConfigMap could point it at their own server with a publicly trusted
// certificate (ADR 0022 counted limit 24); and it reads the SPELLING, not what the name
// resolves to — other loopback names (ip6-localhost, localhost.localdomain, a single
// label such as relay that the cluster's search list may complete) are accepted in
// production (round 2, the third eye's B7; the EM-3 note in ADR 0022 counts them).
func smtpHost(name, v string, prod bool) error {
	_, ipErr := netip.ParseAddr(v)
	isIP := ipErr == nil
	if !isIP && !isDNSHostName(v) {
		return fmt.Errorf("%s: must be a lower-case DNS host name such as email-smtp.eu-central-1.amazonaws.com, "+
			"with no scheme, port or trailing dot (the value is not repeated here)", name)
	}
	if !prod {
		return nil
	}
	labels := strings.Split(v, ".")
	last := labels[len(labels)-1]
	if isIP || v == "localhost" || strings.HasSuffix(v, ".localhost") || (last[0] >= '0' && last[0] <= '9') {
		return fmt.Errorf("%s: in production the relay must not be localhost, a *.localhost name or an IP "+
			"address in any spelling (ADR 0022 §5; the value is not repeated here)", name)
	}
	return nil
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
//
// 🟡 KNOWN LIMIT, MEASURED AND LEFT OPEN (2026-08-02, M5-10 audit): **NaN PASSES**.
// strconv.ParseFloat accepts "NaN" (and "nan"; a SIGN is not accepted — "+nan" is
// an "invalid syntax" error, Go allows a sign only on Inf), and every NaN comparison
// is false — so `v < min || v > max` is false and the range check waves it through.
// What each of the three callers then does with it, measured:
//
//	TAPPA_GPS_RADIUS_M=NaN       Load err=nil, checkin.New err=nil. The radius is
//	                             NaN, so every distance comparison is false: GPS
//	                             never matches. NARROWING (GPS-only taps flag
//	                             instead of ok), §4.6 intact — but silent.
//	TAPPA_DEBOUNCE_SECONDS=NaN   time.Duration(NaN * 1s) is the int64 minimum
//	                             (-2562047h47m16.85s), so `cfg.Debounce > 0` is
//	                             false in checkin.New and it silently falls back to
//	                             DefaultParams()'s 60 s. Shipped behaviour, by luck.
//	TAPPA_FRESHNESS_SECONDS=NaN  the SAME negative duration, but checkin.New's
//	                             `cfg.Freshness <= 0` catches it and refuses to
//	                             start. Fail-closed, and the only one that is.
//
// So the hazard is not the value, it is that two of the three are caught (or not)
// by an accident of the layer below rather than here. THE FIX IS ONE LINE —
// rejecting math.IsNaN(v) beside the range check closes all three at the source —
// and it is deliberately NOT taken here: it is outside M5-10's scope and belongs
// to whoever owns config hardening. Effect today is LOW (narrowing or shipped
// defaults, no record lost); the reason to close it is honesty, not exposure.
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
