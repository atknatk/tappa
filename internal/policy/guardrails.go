package policy

// This file is the GUARDRAIL layer (ADR 0004 §4-§5; M3-05 card; CLAUDE.md §4,
// §5 rows 1-5). Guardrails are the product's reason for existing made
// unbreakable: the ten immutable system rules that no customer policy can loosen.
//
// THREE properties are structural, not disciplinary:
//
//   - CODE, never DB (ADR 0004 §4). The guardrails are this Go slice. There is no
//     table, column, or API that stores or disables one — a single SQL write must
//     not be able to switch off a red line. A tenant document, however broad an
//     allow it writes, cannot reach this list: Evaluate consults guardrails first
//     and stops on the first match (evaluate.go), before any baseline/tenant
//     document is even looked at.
//
//   - ORDER IS A SECURITY BOUNDARY (ADR 0004 §5). Guardrails() returns them in
//     the NORMATIVE 1->10 order and that order is defined in exactly ONE place
//     (the slice literal below). §5 says "first match wins"; a wrong order is
//     exploitable, so it is fixed in code and locked by the 1->10 property test
//     (M3-08). The two exploits the order defends against (both hunted by
//     tappa-security-auditor R8) are called out on sys:sun-invalid.
//
//   - BOUNDED, NOT TOGGLEABLE (ADR 0004 §11). Some guardrails cannot be switched
//     off but MAY be tuned within a range (debounce, freshness, occurred_at
//     skew). The ranges are the named constants below; they are the SINGLE SOURCE
//     the write-time document validator (DefaultLimits().BoundedParams) and the
//     startup config checks (internal/config) both read, so a range a guardrail
//     declares cannot be silently widened through an env var (Y-L).
//
// Guardrails read their inputs from the Context (evaluate.go): some from the 24
// closed document keys (tag:status, tap:sunValid, ...), some from the explicit
// server-derived Context fields (SessionTenantID, SecondsSincePersonLastTap, ...)
// that are NOT document vocabulary. Every input is derived on the SERVER; none is
// a client-declared flag (ADR 0004 §8). No input derived from a person's body is
// defined anywhere (§4.1) — that invariant is proven separately in M3-08, not by
// a guardrail here.

import (
	"fmt"
	"time"
)

// Bounded-parameter ranges (ADR 0004 §11) — the SINGLE SOURCE OF TRUTH for both
// this package (Guardrails, boundedParams) and internal/config's startup range
// checks. Lower bounds keep a protection meaningful; upper bounds stop it from
// being effectively disabled. Untyped so they convert cleanly to the int (const),
// float64 (config, NumericRange) and time.Duration (Params) each caller needs.
const (
	// Person-debounce window, seconds. A busy restaurant and a weekend office
	// want different windows, but neither may drop to zero (a replay/duplicate
	// slips through) nor grow unbounded (a whole shift silently ignored).
	DebounceMinSeconds = 30
	DebounceMaxSeconds = 300

	// Tap freshness window, seconds — 1..15 min. Bounds the age of an NFC tap
	// page (ADR 0004 §11; the production value is M5-10's).
	FreshnessMinSeconds = 60
	FreshnessMaxSeconds = 900

	// occurred_at skew tolerance, seconds — 0..72 h. How far in the past a
	// client-declared occurred_at may sit before a tap is rejected outright
	// (ADR 0004 §11; K1). Future timestamps are rejected regardless.
	OccurredAtSkewMinSeconds = 0
	OccurredAtSkewMaxSeconds = 72 * 60 * 60 // 259200

	// GPS radius, meters — 25..1000. Proof-of-place ring. Not used by any
	// guardrail (it drives the baseline base:ip-or-gps-ok, M3-06), but its range
	// is enforced here so a document predicate on tap:gpsDistanceM and the
	// TAPPA_GPS_RADIUS_M env var share one source: TAPPA_GPS_RADIUS_M=20000000
	// must be rejected at startup, not silently disable proof-of-place (Y-L).
	GPSRadiusMinM = 25
	GPSRadiusMaxM = 1000
)

// Server-derived value vocabulary the guardrails match on. These mirror the DB
// CHECK constraints — the source of truth for the values the server places into
// Context.Keys — so policy stays self-contained and never imports the store.
//   - tag status:      db/migrations/00004_create_tags.sql            (active|retired|lost)
//   - employee status: db/migrations/00003_create_employees_sessions.sql (invited|active|deactivated)
//   - channel:         db/migrations/00005_...sql / internal/sun.Channel  (nfc|qr|manual)
//   - role:            db/migrations/00006_create_admin_users.sql       (owner|manager)
const (
	tagStatusRetired    = "retired"
	tagStatusLost       = "lost"
	employeeDeactivated = "deactivated"
	channelNFC          = "nfc"
	roleOwner           = "owner"
)

// SecurityAlert names a security-relevant event a firing guardrail asks the
// caller to surface to the tenant's managers (a push — §5). It is a FIXED
// vocabulary term, never data: it carries no session token, GPS coordinate, tag
// key, or any raw value (§4.7). The empty SecurityAlert means "no alert". Because
// only a guardrail that ACTUALLY FIRES sets one, and sys:sun-invalid (order 3)
// pre-empts both alerting guardrails, a forged SUN can never manufacture an alert
// (ADR 0004 §5, the push-flood exploit).
type SecurityAlert string

const (
	// AlertLostTagTapped: a tag reported lost was tapped (sys:tag-not-active on a
	// `lost` tag). A `retired` tag does NOT alert — that is routine lifecycle.
	AlertLostTagTapped SecurityAlert = "lost-tag-tapped"
	// AlertDeactivatedEmployeeTapped: a deactivated employee's session tapped
	// (sys:employee-deactivated).
	AlertDeactivatedEmployeeTapped SecurityAlert = "deactivated-employee-tapped"
)

// Params carries the tenant-resolved, range-checked bounded windows the guardrail
// closures compare against (ADR 0004 §11). They are per-TENANT (they live in the
// guardrail list inside a Set, alongside the policies), not per-request — the
// matching per-request values arrive in the Context. Build them from the same
// bounds internal/config enforces at startup and call Validate before use.
type Params struct {
	// DebounceWindow — sys:person-debounce ignores a repeat by the SAME person
	// within this window (30..300 s).
	DebounceWindow time.Duration
	// FreshnessWindow — sys:tap-freshness denies an NFC tap whose page is older
	// than this (1..15 min).
	FreshnessWindow time.Duration
	// OccurredAtSkewMax — sys:occurred-at-bound denies a tap whose client-declared
	// occurred_at is more than this in the past (0..72 h); a future occurred_at is
	// denied regardless of this value.
	OccurredAtSkewMax time.Duration
}

// Validate reports whether every bounded window sits inside its ADR 0004 §11
// range — the SAME ranges internal/config enforces at startup, read from the same
// constants. A caller (M4/M5) must pass a Validate()'d Params to Guardrails.
func (p Params) Validate() error {
	checks := []struct {
		name     string
		got      float64
		min, max float64
	}{
		{"debounce window", p.DebounceWindow.Seconds(), DebounceMinSeconds, DebounceMaxSeconds},
		{"freshness window", p.FreshnessWindow.Seconds(), FreshnessMinSeconds, FreshnessMaxSeconds},
		{"occurred_at skew max", p.OccurredAtSkewMax.Seconds(), OccurredAtSkewMinSeconds, OccurredAtSkewMaxSeconds},
	}
	for _, c := range checks {
		if c.got < c.min || c.got > c.max {
			return fmt.Errorf("policy: %s = %gs is outside [%g, %g] seconds (ADR 0004 §11)", c.name, c.got, c.min, c.max)
		}
	}
	return nil
}

// DefaultParams returns the shipped defaults for the bounded windows. Debounce is
// 60 s, matching internal/config's TAPPA_DEBOUNCE_SECONDS default. Freshness and
// occurred_at skew default to the MAXIMUM of their declared range: these are
// HARD-deny guardrails, so the most lenient in-range value is the safe default —
// it never rejects a legitimately slow or queued tap, leaving the stricter,
// tenant-tunable review thresholds to the baseline (base:queued-window, M3-06).
//
// FLAGGED (M3-05 devir): M5-10 owns the production freshness value and K1 owns
// the occurred_at tolerance; until they land, the range maximum is used
// DELIBERATELY (a grounded ADR 0004 §11 bound), not guessed.
func DefaultParams() Params {
	return Params{
		DebounceWindow:    60 * time.Second,
		FreshnessWindow:   FreshnessMaxSeconds * time.Second,
		OccurredAtSkewMax: OccurredAtSkewMaxSeconds * time.Second,
	}
}

// boundedParams is the ADR 0004 §11 key->range wiring the M3-03 validator hook
// (DefaultLimits().BoundedParams) was deliberately left waiting for. THREE
// document context keys map onto a bounded range; a document predicate
// (Numeric*) on one of them is rejected at write time if the value falls outside
// the range (validate.go). Debounce has NO context key — it is a pure
// guardrail/config parameter, never a document predicate — so it is enforced only
// in Guardrails and internal/config, not here. A fresh map is returned so callers
// cannot mutate shared state.
func boundedParams() map[ContextKey]NumericRange {
	return map[ContextKey]NumericRange{
		CtxTapGPSDistanceM:          {Min: GPSRadiusMinM, Max: GPSRadiusMaxM},
		CtxTapPageAgeSeconds:        {Min: FreshnessMinSeconds, Max: FreshnessMaxSeconds},
		CtxTapOccurredAtSkewSeconds: {Min: OccurredAtSkewMinSeconds, Max: OccurredAtSkewMaxSeconds},
	}
}

// DefaultGuardrails is Guardrails(DefaultParams()) — the guardrail slice a caller
// that has not resolved tenant-specific windows should use.
func DefaultGuardrails() []Guardrail { return Guardrails(DefaultParams()) }

// Guardrails returns the ten immutable system guardrails in their NORMATIVE
// 1->10 order (ADR 0004 §5; M3-05 card). This slice literal is the ONE place the
// order is defined; Evaluate runs them in slice order and the first match is
// terminal (evaluate.go). p supplies the three bounded windows (ADR 0004 §11);
// pass a Validate()'d Params.
//
// The order is load-bearing (ADR 0004 §5): sys:sun-invalid (3) MUST precede
// sys:employee-deactivated (7) and sys:person-debounce (8). If it did not, a
// stolen cookie + stale URL with an invalid SUN would (a) learn whether the
// account is deactivated and flood managers with alerts — info leak + alert
// fatigue — and (b) have the replay swallowed by debounce, reopening the §4.4
// hole. Both are proven in guardrails_test.go and re-checked by R8 in M3-08.
func Guardrails(p Params) []Guardrail {
	debounce := p.DebounceWindow.Seconds()
	freshness := p.FreshnessWindow.Seconds()
	skewMax := p.OccurredAtSkewMax.Seconds()

	return []Guardrail{
		// 1. sys:tenant-mismatch — a session for tenant B tapping tenant A's tag.
		// redirect, and (because redirect writes NOTHING) the tap lands in NEITHER
		// tenant, so a KF manager never sees a KM employee's name or GPS
		// (§4.5; ADR 0002 Y2). Fires only when BOTH a tag and a session exist and
		// their tenants differ; a request with no tag (authz) or no session
		// (falls to #6) does not match.
		{
			Sid:    "sys:tenant-mismatch",
			Effect: EffectRedirect,
			Reason: "the tapped tag belongs to a different organisation than your session; no record is written",
			Match: func(c Context) bool {
				return c.TagTenantID != nil && c.SessionTenantID != nil &&
					*c.TagTenantID != *c.SessionTenantID
			},
		},

		// 2. sys:tag-not-active — a retired or lost tag. Both deny; a LOST tag
		// ALSO raises a security alert (a tag reported lost is in use), a retired
		// one does not (routine lifecycle). The alert fires only on a real `lost`
		// match here, after sun-invalid (#3) has already pre-empted any forged SUN.
		{
			Sid:    "sys:tag-not-active",
			Effect: EffectDeny,
			Reason: "this tag is no longer active",
			Match: func(c Context) bool {
				s, ok := c.Keys[CtxTagStatus].(string)
				return ok && (s == tagStatusRetired || s == tagStatusLost)
			},
			Alert: func(c Context) SecurityAlert {
				if s, _ := c.Keys[CtxTagStatus].(string); s == tagStatusLost {
					return AlertLostTagTapped
				}
				return ""
			},
		},

		// 3. sys:sun-invalid — an NFC tap whose SUN failed: CMAC mismatch OR ctr
		// did not advance (§5 row 2; §4.4). MUST precede #7 and #8 (see the
		// Guardrails doc). QR carries no SUN, so tap:sunValid=false is EXPECTED
		// there and handled by the baseline (base:qr-requires-ip, M3-06); this
		// guardrail is therefore NFC-only, never denying a legitimate QR tap.
		{
			Sid:    "sys:sun-invalid",
			Effect: EffectDeny,
			Reason: "the tap signature could not be verified",
			Match: func(c Context) bool {
				if ch, _ := c.Keys[CtxTapChannel].(string); ch != channelNFC {
					return false
				}
				valid, ok := c.Keys[CtxTapSunValid].(bool)
				return ok && !valid
			},
		},

		// 4. sys:tap-freshness — an NFC tap whose page is older than the freshness
		// window (ADR 0004 §11 bounded param; M5-10, URL-hoarding A1). QR is valid
		// indefinitely (§5), so this is NFC-only.
		{
			Sid:    "sys:tap-freshness",
			Effect: EffectDeny,
			Reason: "this tap page is too old; please tap again",
			Match: func(c Context) bool {
				if ch, _ := c.Keys[CtxTapChannel].(string); ch != channelNFC {
					return false
				}
				age, ok := toFloat(c.Keys[CtxTapPageAgeSeconds])
				return ok && age > freshness
			},
		},

		// 5. sys:occurred-at-bound — a tap whose client-declared occurred_at is in
		// the future (skew < 0, where skew = created_at - occurred_at) or older
		// than the max tolerance (ADR 0004 §11; K1). Tap recording only: manual
		// entry is the separate, authorized record:manual action that may
		// legitimately backdate, so it is not subject to this bound.
		{
			Sid:    "sys:occurred-at-bound",
			Effect: EffectDeny,
			Reason: "the declared tap time is implausible",
			Match: func(c Context) bool {
				if c.Action != ActionTapRecord {
					return false
				}
				skew, ok := toFloat(c.Keys[CtxTapOccurredAtSkewSeconds])
				return ok && (skew < 0 || skew > skewMax)
			},
		},

		// 6. sys:no-session — a tap with no valid session goes to the activation
		// page, NO record (§5 row 3). Tap recording only: an unauthenticated
		// authorization action must fail closed (deny — #9 or the default),
		// never be redirected to a tap activation page.
		{
			Sid:    "sys:no-session",
			Effect: EffectRedirect,
			Reason: "no active session; sending you to activation",
			Match: func(c Context) bool {
				return c.Action == ActionTapRecord && c.SessionTenantID == nil
			},
		},

		// 7. sys:employee-deactivated — a deactivated employee's session tapping.
		// deny + security alert (§5 row 4). AFTER sun-invalid (#3) so a forged SUN
		// can neither learn the account is deactivated nor spam this alert.
		{
			Sid:    "sys:employee-deactivated",
			Effect: EffectDeny,
			Reason: "this account is deactivated",
			Match: func(c Context) bool {
				s, ok := c.Keys[CtxEmployeeStatus].(string)
				return ok && s == employeeDeactivated
			},
			Alert: func(Context) SecurityAlert { return AlertDeactivatedEmployeeTapped },
		},

		// 8. sys:person-debounce — the SAME PERSON tapping again within the window
		// is ignored (§5 row 5). PERSON-based, not tag-based: the input is seconds
		// since THIS PERSON's last tap, so different people can tap the same
		// plaque back-to-back and each is recorded. A nil gap (no previous tap)
		// never debounces — nil is the safe zero value (§4.6: never silently drop
		// a real record). AFTER sun-invalid (#3) so a replay is not swallowed.
		{
			Sid:    "sys:person-debounce",
			Effect: EffectIgnore,
			Reason: "duplicate tap by the same person within the debounce window",
			Match: func(c Context) bool {
				if c.Action != ActionTapRecord || c.SecondsSincePersonLastTap == nil {
					return false
				}
				gap := *c.SecondsSincePersonLastTap
				return gap >= 0 && gap < debounce
			},
		},

		// 9. sys:policy-edit-owner-only — policy:edit is restricted to the tenant
		// owner; anyone else, or an absent role, is denied (ADR 0004 K2,
		// fail-closed). Scopes itself by action.
		{
			Sid:    "sys:policy-edit-owner-only",
			Effect: EffectDeny,
			Reason: "editing policies is restricted to the organisation owner",
			Match: func(c Context) bool {
				if c.Action != ActionPolicyEdit {
					return false
				}
				role, ok := c.Keys[CtxActorRole].(string)
				return !(ok && role == roleOwner)
			},
		},

		// 10. sys:no-self-review — an actor may not approve/review their own
		// transaction (ADR 0004 Y-C). Fires whenever both ids are present and
		// equal, which scopes it to review actions.
		{
			Sid:    "sys:no-self-review",
			Effect: EffectDeny,
			Reason: "you cannot review your own record",
			Match: func(c Context) bool {
				return c.ReviewerID != nil && c.SubjectEmployeeID != nil &&
					*c.ReviewerID == *c.SubjectEmployeeID
			},
		},
	}
}
