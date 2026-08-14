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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

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

	// OperatorAdminIDs is who may publish Tappa's OWN legal texts (M7-06): the
	// privacy policy, the terms, the company details and the cookie notice. It is an
	// allow-list of admin_users.id values.
	//
	// 🔴 IT IS AN id AND NOT AN EMAIL, AND THE FIRST VERSION OF THIS FIELD WAS THE
	// EMAIL. A security audit broke it end to end, and the break is worth writing out
	// because it is not obvious: admin_users.email is unique PER TENANT
	// (admin_users_tenant_email_key is UNIQUE (tenant_id, email)), sign-up is PUBLIC,
	// and nothing verifies an address. So anybody who knew an allow-listed address
	// could register their own business with that same address, sign in as its owner,
	// and reach this screen. Measured: a stranger tenant holding the operator's
	// address got 200 on GET /admin/legal and 303 on POST, and rewrote the published
	// privacy policy. An allow-list is only worth the uniqueness of its key, and an
	// email address in this schema has none.
	//
	// admin_users.id is the table's PRIMARY KEY — globally unique — and it is assigned
	// by the database (gen_random_uuid()), never by a caller: internal/domain/signup
	// reads it back off the INSERT. There is no registration, form or header that lets
	// somebody choose it, which is the whole property the old key lacked.
	//
	// 🔴 IT IS FAIL-CLOSED AND THE EMPTY LIST IS THE PROOF. Unset means NOBODY may
	// reach the screen — not everybody. There is no "unconfigured means open" branch
	// anywhere on this path, and internal/handler's
	// TestOperatorGate_EmptyAllowListAdmitsNobody drives an admin who IS on the list,
	// empties the list, and drives them again.
	//
	// ⚠️ AN ADMIN id IS NOT A SECRET AND NOT A CREDENTIAL. It is already in every
	// audit_log row that admin caused, and holding it gets nobody past the password —
	// which is why the refusal page may show a signed-in admin their OWN id, so the
	// person who runs this deployment can configure it without a database session.
	OperatorAdminIDs []uuid.UUID

	// ResetDelivery names the transport that carries an admin password-reset link
	// to the administrator's own address (M7-04 phase B, TAPPA_RESET_DELIVERY).
	//
	// 🔴 TODAY THE ONLY LEGAL VALUE IS "none", AND THAT IS AN HONEST STATE RATHER
	// THAN A PLACEHOLDER. Q02 — which mail provider, in which region, under which
	// processing agreement — is unanswered, so this repository has no mail
	// transport at all. What this field must NOT do is imply one: a value naming a
	// provider, or a second field carrying an API endpoint, would be choosing Q02
	// by the back door (the M7-04 card's correction 5 makes the same point about
	// the schema, which is why 00019 has no delivery column either).
	//
	// 🔴 AND THERE IS NO INTERIM CHANNEL, WHICH IS THE DIFFERENCE FROM invites.
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
	} else {
		push(trustedProxySanity(c.TrustedProxies, c.IsProd()))
	}
	// Unset is the inert value, not an error: a deployment that publishes no legal
	// text is a deployment where nobody needs the screen. It is inert in the
	// FAIL-CLOSED direction — see the field comment.
	if c.OperatorAdminIDs, err = operatorAdminIDs(env("TAPPA_OPERATOR_ADMIN_IDS", "")); err != nil {
		push(err)
	}
	if c.ResetDelivery, err = resetDelivery(env("TAPPA_RESET_DELIVERY", ResetDeliveryNone)); err != nil {
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

// operatorAdminIDs parses TAPPA_OPERATOR_ADMIN_IDS, the allow-list for the screen
// that publishes Tappa's own legal texts (M7-06).
//
// IT FOLLOWS prefixes ABOVE DELIBERATELY, INCLUDING THE PART THAT HURTS: empty means
// the inert value rather than an error, one element is parsed at a time, and ONE BAD
// ELEMENT FAILS THE WHOLE LOAD naming the variable and the element. A list that
// silently dropped the entry with the typo would hand the operator a screen they
// cannot reach and no reason why.
//
// 🔴 A uuid, NOT AN EMAIL, AND THE REASON IS A MEASURED BREAK — see
// Config.OperatorAdminIDs. Parsing here is what makes "the caller cannot declare
// this value" true at start-up rather than at the gate: a malformed entry is a
// startup failure, so the running server's list contains only real uuids.
//
// THE NIL uuid IS REFUSED. It is a valid uuid literal, so a stray placeholder would
// parse; and while no admin_users row can carry it (the column defaults to
// gen_random_uuid() and is the PK), refusing it here means the list cannot contain a
// value whose only possible meaning is "somebody left this unfinished".
func operatorAdminIDs(s string) ([]uuid.UUID, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil // nobody: the screen exists and admits no one
	}
	seen := make(map[uuid.UUID]bool)
	var out []uuid.UUID
	for _, part := range strings.Split(s, ",") {
		raw := strings.TrimSpace(part)
		if raw == "" {
			return nil, fmt.Errorf("TAPPA_OPERATOR_ADMIN_IDS: %q: empty entry (a stray comma?)", part)
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("TAPPA_OPERATOR_ADMIN_IDS: %q: %w (this is an admin_users.id, not an email address: an email is unique only within one business, so an address-keyed allow-list can be joined by registering a business with that address)", raw, err)
		}
		if id == uuid.Nil {
			return nil, fmt.Errorf("TAPPA_OPERATOR_ADMIN_IDS: %q: the nil uuid names no admin", raw)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// ResetDeliveryNone is the only transport this repository implements for admin
// password-reset links: there isn't one. See Config.ResetDelivery.
const ResetDeliveryNone = "none"

// resetDelivery parses TAPPA_RESET_DELIVERY.
//
// EMPTY IS "none", AND AN UNKNOWN VALUE IS A STARTUP FAILURE. The package doc's rule
// is "never a silent default"; the value with a default here is the one that does
// LESS, and the one that must never be guessed at is a transport nothing implements.
// Naming the open question in the error is the point of the message: whoever set this
// variable is trying to make the product send mail, and the answer they need is which
// decision is missing rather than which string is invalid.
func resetDelivery(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" || v == ResetDeliveryNone {
		return ResetDeliveryNone, nil
	}
	return "", fmt.Errorf("TAPPA_RESET_DELIVERY: %q is not implemented; the only value this build accepts is %q, because no mail transport has been chosen yet (docs/plan/open-questions.md Q02: provider, region and processing agreement). While it is %q the panel's recovery form says so on screen instead of pretending a link was sent",
		s, ResetDeliveryNone, ResetDeliveryNone)
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
