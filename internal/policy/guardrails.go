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
	// page (ADR 0004 §11). The production value comes from
	// TAPPA_FRESHNESS_SECONDS (default 180 s, M5-10) and is range-checked against
	// THESE constants at startup, so the window cannot be widened past 900 s —
	// which is also where the signed context's TTL sits, i.e. the point beyond
	// which this guardrail could not fire at all.
	//
	// ⚠️ THE MAXIMUM IS AN ACCEPTED NO-OP, AND IT IS SILENT. Configuring exactly
	// 900 makes the window equal that TTL, and the TTL refuses an over-900 s
	// context before the guardrail is consulted — so the recorded-reject band is
	// EXACTLY EMPTY and the deployment is back in the pre-M5-10 state with no
	// warning of any kind. It stays a legal value because it is a real point of a
	// declared range (and internal/config pins both ends as inclusive), but a
	// deployment that wants §4.6's record must configure BELOW 900. The lower end
	// carries no such trap; it is merely tight, and the M5-10 card's own warning
	// about sub-minute windows applies there.
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
//   - tag status:      db/migrations/00004 + 00013 (active|retired|lost|unassigned)
//   - employee status: db/migrations/00003_create_employees_sessions.sql (invited|active|deactivated)
//   - channel:         db/migrations/00005_...sql / internal/sun.Channel  (nfc|qr|manual)
//   - role:            db/migrations/00006_create_admin_users.sql       (owner|manager)
//
// 🔴 THE TAG LIST GREW, AND THAT IS WHY sys:tag-not-active IS NOW AN ALLOW-LIST.
// Migration 00013 added a FOURTH tag status, `unassigned` — a plaque Tappa has
// encoded and loaded but nobody has mounted yet (the inventory model). The
// guardrail used to enumerate the BAD values (retired, lost), so the new value
// matched nothing and a tap on a plaque still in its box fell straight through
// §5 row 1 into the evidence rules below. See tagStatusActive.
const (
	// tagStatusActive is the ONLY status a tap may proceed on. Everything else is
	// refused by sys:tag-not-active, whatever it is called.
	tagStatusActive     = "active"
	tagStatusRetired    = "retired"
	tagStatusLost       = "lost"
	employeeDeactivated = "deactivated"
	channelNFC          = "nfc"
	roleOwner           = "owner"
)

// SecurityAlert names a security-relevant event a firing guardrail asks the
// caller to surface to the tenant's managers (a push — §5). It is a FIXED
// vocabulary term, never data: it carries no session token, GPS coordinate, tag
// key, or any raw value (§4.7). The empty SecurityAlert means "no alert", and only
// a guardrail that ACTUALLY FIRES sets one.
//
// ⚠️ WHAT sys:sun-invalid (order 3) BUYS IS NARROWER THAN "never" (measured
// 2026-08-02; this comment claimed "both alerting guardrails" and was wrong). It
// pre-empts sys:employee-deactivated (5), so a forged SUN cannot manufacture the
// DEACTIVATED alert — that is the ADR 0004 §5 info-leak / push-flood exploit and it
// is closed. It does NOT pre-empt sys:tag-not-active (2), which sits AHEAD of it:
// a forged SUN on a `lost` tag returns sid=sys:tag-not-active with
// alert="lost-tag-tapped". Accepted, not a hole — that alert says only "a plaque
// someone REPORTED LOST was tapped", which is true of the plaque no matter who
// tapped it and names nobody. ADR 0007's family table already records this row as
// "alert present"; only this sentence said otherwise.
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
// 🔴 THE PRODUCTION FRESHNESS VALUE IS NO LONGER THIS ONE (M5-10, 2026-08-02).
// TAPPA_FRESHNESS_SECONDS (default 180 s) is carried into Params by checkin.New,
// so a deployment runs at 180 s and the 900 s below is only the fallback for a
// caller that has no config — tests, and any future wiring that skips Load. It
// is left at the range maximum ON PURPOSE rather than lowered to match: a
// fallback that equals the configured default would make the plumbing
// unfalsifiable, which is exactly how the debounce wiring survived an audit
// (hand-off N3, and again as M5-05's F3).
//
// K1 still owns the occurred_at tolerance; that one is the range maximum for the
// original reason (a grounded ADR 0004 §11 bound, not a guess).
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
// The order is load-bearing (ADR 0004 §5), and it carries THREE separate claims:
//
//   - sys:sun-invalid (3) MUST precede sys:employee-deactivated (5) and
//     sys:person-debounce (8). If it did not, a stolen cookie + stale URL with an
//     invalid SUN would (a) learn whether the account is deactivated and flood
//     managers with alerts — info leak + alert fatigue — and (b) have the replay
//     swallowed by debounce, reopening the §4.4 hole. Both are proven in
//     guardrails_test.go and re-checked by R8 in M3-08.
//
//   - THE PLACEMENT RULE FOR AN ELEVENTH GUARDRAIL — the only GENERAL claim in
//     this list, and the one an addition has to be tested against. §5's five rows
//     map onto tag-not-active (2, row 1), sun-invalid (3, row 2), no-session
//     (4, row 3), employee-deactivated (5, row 4), person-debounce (8, row 5) —
//     but that mapping is a FACT, not the rule. The rule that used to stand here
//     ("a guardrail §5 does not name may sit anywhere that keeps those five in
//     §5's relative order") was measured and is NOT falsifiable: the order that
//     shipped the ADR 0007 regression put §5's rows at positions [2 3 6 7 8],
//     exactly as monotonic as today's [2 3 4 5 8]. It admitted the bug.
//
//     What discriminates is ALERT SURVIVAL. Only the WINNING guardrail's Alert
//     reaches the Decision (evaluate.go), so a guardrail placed ahead of an
//     ALERTING one that can fire on the SAME request DELETES that alert while the
//     deny and the written row still look correct. Hence:
//
//     NOTHING may be placed ahead of an alerting guardrail — sys:employee-
//     deactivated (5), and sys:tag-not-active (2) on a `lost` tag — unless the
//     suppression is DELIBERATE, NAMED HERE, and argued. Exactly four are, all
//     in ADR 0007's family table: sys:tenant-mismatch (1) writes into no tenant
//     at all, so there is no tenant to alert (§4.5); sys:tag-not-active (2)
//     accepts the loss on `retired` and on `lost` trades the alert for a more
//     urgent one; sys:sun-invalid (3) because the tap is FORGED and an
//     unauthenticated request must not manufacture an alert (R8); sys:no-session
//     (4) because it is MUTUALLY EXCLUSIVE with the alert it precedes — the
//     employee:status key and SessionTenantID are set by the same `Employee !=
//     nil` branch in tap.Decide, so a request that matches no-session cannot
//     carry a deactivated status, and no alert is suppressed. ⚠️ THAT LAST ONE
//     IS THE ONLY EXCEPTION WHOSE ARGUMENT LIVES IN ANOTHER PACKAGE, which is
//     its boundary: Evaluate is exported, so a second caller that sets
//     employee:status WITHOUT a SessionTenantID turns this from a vacuous
//     pre-emption into a real one. Any such caller must set both or place its
//     own guard — the invariant is not enforced here. Everything else goes
//     BEHIND. Measured on both orders with employee:status=deactivated held
//     fixed: under the old one sys:tap-freshness and sys:occurred-at-bound
//     deleted the alert while named nowhere — this rule REJECTS that order —
//     and under this one the only deletions left are the four named above.
//
//     The rule is MECHANICAL, not prose: TestGuardrails_NothingUnnamedPreemptsAnAlert
//     walks the shipped slice and flags any guardrail ahead of an alerting one
//     that is absent from the allowlist, with the regression order and an unnamed
//     eleventh guardrail as its two negative controls. Unlike the fixed list in
//     TestGuardrails_NormativeOrder — which catches a REORDER but invites the
//     wrong repair on an ADDITION — its only repairs are "move it behind" or
//     "argue it into the allowlist".
//
//   - sys:employee-deactivated (5) MUST precede the two guardrails §5 does NOT
//     name that can fire on the same request — sys:tap-freshness (6) and
//     sys:occurred-at-bound (7) — because only the WINNING guardrail's Alert is
//     surfaced, so pre-empting §5 row 4 silently deletes its security alert while
//     still writing the row. Measured, fixed and bounded in ADR 0007; pinned by
//     TestGuardrails_TimingRulesDoNotPreemptTheDeactivatedAlert and by the tap
//     engine's TestDecide_DelegatesOrderToPolicy twins.
//
//     A THIRD rule co-fires on that request: sys:person-debounce (8). §5 DOES
//     name it (row 5) and it is already behind, so it was never part of the ADR
//     0007 fix — but it deletes the same alert if moved ahead, and worse, its
//     effect is `ignore`, so the deny would vanish too. The test above carries a
//     case for it as well; without one, only the fixed list in
//     TestGuardrails_NormativeOrder stood between that move and the same defect.
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
		// (falls to #4, sys:no-session) does not match.
		{
			Sid:    "sys:tenant-mismatch",
			Effect: EffectRedirect,
			Reason: "the tapped tag belongs to a different organisation than your session; no record is written",
			Match: func(c Context) bool {
				return c.TagTenantID != nil && c.SessionTenantID != nil &&
					*c.TagTenantID != *c.SessionTenantID
			},
		},

		// 2. sys:tag-not-active — the tag is anything other than `active`. A LOST
		// tag ALSO raises a security alert (a tag reported lost is in use); a
		// retired one does not (routine lifecycle), and neither does any future
		// status. The alert fires only on a real `lost` match here, after
		// sun-invalid (#3) has already pre-empted any forged SUN.
		//
		// 🔴 IT ASKS FOR `active` RATHER THAN LISTING THE BAD VALUES, AND THAT
		// CHANGE HAS A DATE: 2026-08-09, migration 00013. The old form was
		// `s == tagStatusRetired || s == tagStatusLost`, which was complete for the
		// three statuses 00004 defined and became INCOMPLETE the moment 00013 added
		// `unassigned`. A plaque that has been encoded and loaded but never mounted
		// matched neither arm, so §5 row 1 did not fire for it at all.
		//
		// WHAT THE OLD FORM COST, measured rather than imagined (the chain is in
		// 00013's part 3): an unmounted plaque has location_id NULL, which resolves
		// to uuid.Nil, so GetLocationForTap finds nothing and the tap has NO IP
		// range and NO coordinate to be judged against. On NFC the tap still dies at
		// #3, because preview.go treats a non-active status as an unverifiable CMAC.
		// On QR there is no #3 to die at: the tap reached the evidence rules with no
		// evidence and was recorded as `flag` — a real row, in a real approval
		// queue, for a plaque sitting in a box. Not `ok`, and never silent (§4.6
		// holds), but a manager could approve it.
		//
		// THE POINT OF THE INVERSION IS THE NEXT VALUE, not this one. Whatever a
		// future migration adds to the status CHECK is refused here on the day it is
		// added, without anyone remembering to come back. The equivalence for
		// TODAY's vocabulary is not asserted here -- it is measured in
		// guardrails_test.go against the status list read out of the migrations.
		{
			Sid:    "sys:tag-not-active",
			Effect: EffectDeny,
			Reason: "this tag is no longer active",
			Match: func(c Context) bool {
				s, ok := c.Keys[CtxTagStatus].(string)
				return ok && s != tagStatusActive
			},
			Alert: func(c Context) SecurityAlert {
				if s, _ := c.Keys[CtxTagStatus].(string); s == tagStatusLost {
					return AlertLostTagTapped
				}
				return ""
			},
		},

		// 3. sys:sun-invalid — an NFC tap whose SUN failed: CMAC mismatch OR ctr
		// did not advance (§5 row 2; §4.4). MUST precede #5 and #8 (see the
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

		// 4. sys:no-session — a tap with no valid session goes to the activation
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

		// 5. sys:employee-deactivated — a deactivated employee's session tapping.
		// deny + security alert (§5 row 4). AFTER sun-invalid (#3) so a forged SUN
		// can neither learn the account is deactivated nor spam this alert.
		//
		// 🔴 IT SITS AHEAD OF THE TWO TIMING GUARDRAILS (#6, #7) AND THAT POSITION
		// IS THE ALERT (ADR 0007, 2026-08-02). §5 row 4 is "reject + log the attempt
		// + SECURITY ALERT", and only the guardrail that WINS contributes an Alert
		// (evaluate.go) — so any rule that pre-empts this one deletes the alert
		// while still writing the row. Both timing rules were measured doing exactly
		// that: a 300 s-old page, or a client-declared occurred_at 60 s in the
		// future, ended on sys:tap-freshness / sys:occurred-at-bound with
		// Security=false and ZERO tap.security_alert rows. Neither costs an attacker
		// anything — one is WAITING, the other is a form field.
		//
		// The distinction against #3, which DOES pre-empt this deliberately and must
		// keep doing so: sun-invalid means the TAP ITSELF is unauthenticated, and an
		// unauthenticated request must not be able to manufacture an alert (R8 info
		// leak / push flood). On the timing paths the CMAC verified, the counter
		// advanced and the session is live — a real physical touch, merely posted
		// late or with a bogus declared time. There is nothing forged to protect
		// against, and a real deactivated employee standing at a real plaque is
		// precisely the event §5 row 4 wants pushed.
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

		// 6. sys:tap-freshness — an NFC tap whose page is older than the freshness
		// window (ADR 0004 §11 bounded param; M5-10, URL-hoarding A1).
		//
		// NFC-ONLY, and that is a §5 rule rather than an omission: a QR code is
		// photographed and valid indefinitely, so it has no touch to be stale
		// relative to. M5-10 configured the window and deliberately did NOT extend
		// it to QR; the QR ceiling is bounded by base:qr-requires-ip, the person
		// debounce and the rate limits instead
		// (TestCheckinDB_StaleQRPageIsNotDeniedByFreshness).
		//
		// BOUNDARY: strictly greater. A page aged EXACTLY the window is fresh; the
		// deny starts one tick past it. Stated because internal/config pins the
		// range's own boundaries as inclusive, so this one should not be left to be
		// inferred from a comparison operator
		// (TestGuardrails_FreshnessBoundaryIsStrictlyGreater).
		//
		// POSITION: behind #4 AND #5, ahead of #8 (ADR 0007). Behind #5 for the
		// alert (see there). Behind #4 is the SAME move sys:occurred-at-bound made
		// and it is easy to overlook because only the alert motivated the fix: a
		// session-less request with a stale page now takes §5 row 3's activation
		// redirect instead of a recorded reject that names nobody. Ahead of the
		// debounce because a stale page is the more specific and more useful answer
		// than "duplicate" — both record, so nothing is lost either way (§4.6) and
		// this is the reading a manager can act on.
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

		// 7. sys:occurred-at-bound — a tap whose client-declared occurred_at is in
		// the future (skew < 0, where skew = created_at - occurred_at) or older
		// than the max tolerance (ADR 0004 §11; K1). Tap recording only: manual
		// entry is the separate, authorized record:manual action that may
		// legitimately backdate, so it is not subject to this bound.
		//
		// POSITION: behind #5 (ADR 0007), and this one is the sharper of the two —
		// its input is a POST form field (occurred_at, internal/handler/checkin.go),
		// so ahead of #5 it let a deactivated session switch the alert off by
		// declaring any future timestamp. It is also behind #4 now: a session-less
		// request with a bogus occurred_at is §5 row 3's activation redirect, not a
		// record naming nobody.
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
