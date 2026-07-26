package policy

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- helpers ----------------------------------------------------------------

// defaultSet is a Set carrying the production guardrails and no policies, so a
// tap that clears every guardrail falls through to the built-in default.
func defaultSet() Set { return Set{Guardrails: DefaultGuardrails()} }

// cleanTap is a valid NFC tap that matches NO guardrail: same-tenant session and
// tag, active tag, valid SUN, fresh page, no skew, active employee, no prior
// personal tap. Each call builds a FRESH Keys map so a test may mutate one key
// without touching another case. Tweaking one field is how each guardrail is
// isolated (guardrails are terminal in order, so only the target may match).
func cleanTap(tenant uuid.UUID) Context {
	tid := tenant
	sid := tenant
	return Context{
		Action:    ActionTapRecord,
		Resources: []string{"location/rusty-bar"},
		Keys: map[ContextKey]any{
			CtxTapChannel:               "nfc",
			CtxTapSunValid:              true,
			CtxTagStatus:                "active",
			CtxEmployeeStatus:           "active",
			CtxTapPageAgeSeconds:        5.0,
			CtxTapOccurredAtSkewSeconds: 0.0,
		},
		SessionTenantID: &sid,
		TagTenantID:     &tid,
	}
}

// --- normative order (ADR 0004 §5; the 1->10 slice) -------------------------

// TestGuardrails_NormativeOrder pins the exact 1->10 order and sys: namespace.
// This is the ONE place the order lives; the deeper property test is M3-08.
func TestGuardrails_NormativeOrder(t *testing.T) {
	want := []string{
		"sys:tenant-mismatch",
		"sys:tag-not-active",
		"sys:sun-invalid",
		"sys:tap-freshness",
		"sys:occurred-at-bound",
		"sys:no-session",
		"sys:employee-deactivated",
		"sys:person-debounce",
		"sys:policy-edit-owner-only",
		"sys:no-self-review",
	}
	gs := DefaultGuardrails()
	if len(gs) != len(want) {
		t.Fatalf("want %d guardrails, got %d", len(want), len(gs))
	}
	for i, w := range want {
		if gs[i].Sid != w {
			t.Errorf("guardrail %d: want %q, got %q", i, w, gs[i].Sid)
		}
		if !strings.HasPrefix(gs[i].Sid, "sys:") {
			t.Errorf("guardrail %d sid %q is not in the reserved sys: namespace", i, gs[i].Sid)
		}
		if gs[i].Match == nil {
			t.Errorf("guardrail %d (%q) has a nil Match", i, gs[i].Sid)
		}
	}
}

// TestGuardrails_CleanTapFallsThrough proves a valid tap matches NO guardrail
// (every Match returns false) and reaches the default review with no alert.
func TestGuardrails_CleanTapFallsThrough(t *testing.T) {
	d := Evaluate(defaultSet(), cleanTap(uuid.New()))
	if d.MatchedSid != "default" || d.Effect != EffectReview {
		t.Fatalf("a clean tap should skip all guardrails and default to review, got %+v", d)
	}
	if d.SecurityAlert != "" {
		t.Fatalf("a clean tap must not raise a security alert, got %q", d.SecurityAlert)
	}
}

// --- each guardrail fires on its own condition, with the right effect+alert --

func TestGuardrails_EachFiresOnItsOwnCondition(t *testing.T) {
	tenant := uuid.New()
	otherTenant := uuid.New()
	reviewer := uuid.New()

	cases := []struct {
		name      string
		build     func() Context
		wantSid   string
		wantEff   Effect
		wantAlert SecurityAlert
	}{
		{
			name:    "1 tenant-mismatch -> redirect, no record",
			build:   func() Context { c := cleanTap(tenant); c.TagTenantID = &otherTenant; return c },
			wantSid: "sys:tenant-mismatch", wantEff: EffectRedirect,
		},
		{
			name:    "2 tag retired -> deny, NO alert",
			build:   func() Context { c := cleanTap(tenant); c.Keys[CtxTagStatus] = "retired"; return c },
			wantSid: "sys:tag-not-active", wantEff: EffectDeny,
		},
		{
			name:    "2 tag lost -> deny + alert",
			build:   func() Context { c := cleanTap(tenant); c.Keys[CtxTagStatus] = "lost"; return c },
			wantSid: "sys:tag-not-active", wantEff: EffectDeny, wantAlert: AlertLostTagTapped,
		},
		{
			name:    "3 sun-invalid (nfc) -> deny",
			build:   func() Context { c := cleanTap(tenant); c.Keys[CtxTapSunValid] = false; return c },
			wantSid: "sys:sun-invalid", wantEff: EffectDeny,
		},
		{
			name: "4 tap-freshness (int page age) -> deny",
			// int value exercises toFloat's integer path.
			build: func() Context {
				c := cleanTap(tenant)
				c.Keys[CtxTapPageAgeSeconds] = FreshnessMaxSeconds + 1
				return c
			},
			wantSid: "sys:tap-freshness", wantEff: EffectDeny,
		},
		{
			name:    "5 occurred-at in future -> deny",
			build:   func() Context { c := cleanTap(tenant); c.Keys[CtxTapOccurredAtSkewSeconds] = -1.0; return c },
			wantSid: "sys:occurred-at-bound", wantEff: EffectDeny,
		},
		{
			name: "5 occurred-at too old -> deny",
			build: func() Context {
				c := cleanTap(tenant)
				c.Keys[CtxTapOccurredAtSkewSeconds] = OccurredAtSkewMaxSeconds + 1
				return c
			},
			wantSid: "sys:occurred-at-bound", wantEff: EffectDeny,
		},
		{
			name:    "6 no-session -> redirect, no record",
			build:   func() Context { c := cleanTap(tenant); c.SessionTenantID = nil; return c },
			wantSid: "sys:no-session", wantEff: EffectRedirect,
		},
		{
			name:    "7 employee-deactivated -> deny + alert",
			build:   func() Context { c := cleanTap(tenant); c.Keys[CtxEmployeeStatus] = "deactivated"; return c },
			wantSid: "sys:employee-deactivated", wantEff: EffectDeny, wantAlert: AlertDeactivatedEmployeeTapped,
		},
		{
			name:    "8 person-debounce -> ignore",
			build:   func() Context { c := cleanTap(tenant); g := 5.0; c.SecondsSincePersonLastTap = &g; return c },
			wantSid: "sys:person-debounce", wantEff: EffectIgnore,
		},
		{
			name: "9 policy:edit non-owner -> deny",
			build: func() Context {
				return Context{Action: ActionPolicyEdit, Resources: []string{"*"}, Keys: map[ContextKey]any{CtxActorRole: "manager"}}
			},
			wantSid: "sys:policy-edit-owner-only", wantEff: EffectDeny,
		},
		{
			name: "9 policy:edit missing role -> deny (fail-closed)",
			build: func() Context {
				return Context{Action: ActionPolicyEdit, Resources: []string{"*"}}
			},
			wantSid: "sys:policy-edit-owner-only", wantEff: EffectDeny,
		},
		{
			name: "10 self-review -> deny",
			build: func() Context {
				return Context{Action: ActionTapApprove, Resources: []string{"employee/x"}, ReviewerID: &reviewer, SubjectEmployeeID: &reviewer}
			},
			wantSid: "sys:no-self-review", wantEff: EffectDeny,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Evaluate(defaultSet(), tc.build())
			if d.MatchedSid != tc.wantSid || d.Effect != tc.wantEff {
				t.Fatalf("want %s / %s, got %s / %s (%+v)", tc.wantEff, tc.wantSid, d.Effect, d.MatchedSid, d)
			}
			if d.SecurityAlert != tc.wantAlert {
				t.Fatalf("want alert %q, got %q", tc.wantAlert, d.SecurityAlert)
			}
			if d.Layer != LayerGuardrail {
				t.Fatalf("guardrail decision must carry LayerGuardrail, got %q", d.Layer)
			}
			if d.PolicyVersionID != nil {
				t.Fatalf("guardrail decision must have a nil PolicyVersionID (code, not DB)")
			}
		})
	}
}

// --- ORDER exploit (ADR 0004 §5; R8) ----------------------------------------

// TestGuardrails_SunInvalidPreemptsDeactivatedAndDebounce is the R8 test: a
// context matching sys:sun-invalid (3), sys:employee-deactivated (7) AND
// sys:person-debounce (8) at once must resolve to sun-invalid, and must NOT leak
// a security alert. A forged SUN with a stolen cookie thus learns nothing about
// the account and cannot flood managers with pushes (info leak + alert fatigue),
// and the replay is not swallowed by debounce (ADR 0004 §5; §4.4).
func TestGuardrails_SunInvalidPreemptsDeactivatedAndDebounce(t *testing.T) {
	tenant := uuid.New()
	gap := 5.0
	c := cleanTap(tenant)
	c.Keys[CtxTapSunValid] = false            // matches sun-invalid (3)
	c.Keys[CtxEmployeeStatus] = "deactivated" // would match deactivated (7)
	c.SecondsSincePersonLastTap = &gap        // would match debounce (8)

	d := Evaluate(defaultSet(), c)
	if d.MatchedSid != "sys:sun-invalid" || d.Effect != EffectDeny {
		t.Fatalf("sun-invalid must win over deactivated and debounce, got %+v", d)
	}
	if d.SecurityAlert != "" {
		t.Fatalf("a forged SUN produced alert %q — the push-flood / info-leak the order prevents", d.SecurityAlert)
	}
}

// TestGuardrails_OrderIsLoadBearing is the NON-VACUOUS proof: the SAME context
// under a WRONG order (deactivated moved ahead of sun-invalid) reintroduces the
// exact leak. This shows the correct result comes from the ORDER, not from the
// context happening to be safe.
func TestGuardrails_OrderIsLoadBearing(t *testing.T) {
	tenant := uuid.New()
	gap := 5.0
	c := cleanTap(tenant)
	c.Keys[CtxTapSunValid] = false
	c.Keys[CtxEmployeeStatus] = "deactivated"
	c.SecondsSincePersonLastTap = &gap

	// Wrong order: move sys:employee-deactivated to the front.
	var dea Guardrail
	var rest []Guardrail
	for _, g := range DefaultGuardrails() {
		if g.Sid == "sys:employee-deactivated" {
			dea = g
			continue
		}
		rest = append(rest, g)
	}
	wrong := append([]Guardrail{dea}, rest...)

	d := Evaluate(Set{Guardrails: wrong}, c)
	if d.MatchedSid != "sys:employee-deactivated" || d.SecurityAlert != AlertDeactivatedEmployeeTapped {
		t.Fatalf("the wrong order should reintroduce the deactivation leak, got %+v", d)
	}
}

// --- person-based debounce (§5) ---------------------------------------------

// TestGuardrails_DebounceIsPersonBased proves debounce keys on the PERSON, not
// the tag: the same person within the window is ignored, but a DIFFERENT person
// on the same plaque (no personal history -> nil gap) is recorded — a queue of
// people tapping one plaque all count.
func TestGuardrails_DebounceIsPersonBased(t *testing.T) {
	tenant := uuid.New()

	recent := 5.0
	same := cleanTap(tenant)
	same.SecondsSincePersonLastTap = &recent
	if d := Evaluate(defaultSet(), same); d.Effect != EffectIgnore || d.MatchedSid != "sys:person-debounce" {
		t.Fatalf("same person 5s after their last tap must be ignored, got %+v", d)
	}

	other := cleanTap(tenant) // different person on the same plaque -> nil gap
	if d := Evaluate(defaultSet(), other); d.Effect == EffectIgnore {
		t.Fatalf("a different person on the same plaque must NOT be debounced, got %+v", d)
	}

	// Boundary: gap == window is NOT within it; a negative gap never debounces.
	atWindow := 60.0
	at := cleanTap(tenant)
	at.SecondsSincePersonLastTap = &atWindow
	if d := Evaluate(defaultSet(), at); d.Effect == EffectIgnore {
		t.Fatalf("gap == window must not debounce, got %+v", d)
	}
	neg := -3.0
	nc := cleanTap(tenant)
	nc.SecondsSincePersonLastTap = &neg
	if d := Evaluate(defaultSet(), nc); d.Effect == EffectIgnore {
		t.Fatalf("a negative gap must not debounce, got %+v", d)
	}
}

// TestGuardrails_DebounceWindowIsParameterised proves the tenant-tuned window
// flows into the closure: 100s is outside a 60s window but inside a 300s window.
func TestGuardrails_DebounceWindowIsParameterised(t *testing.T) {
	tenant := uuid.New()
	gap := 100.0
	c := cleanTap(tenant)
	c.SecondsSincePersonLastTap = &gap

	if d := Evaluate(defaultSet(), c); d.Effect == EffectIgnore {
		t.Fatalf("100s gap with the 60s default window must not debounce, got %+v", d)
	}
	p := DefaultParams()
	p.DebounceWindow = 300 * time.Second
	if d := Evaluate(Set{Guardrails: Guardrails(p)}, c); d.Effect != EffectIgnore {
		t.Fatalf("100s gap with a 300s window must debounce, got %+v", d)
	}
}

// --- tenant isolation (§4.5) ------------------------------------------------

// TestGuardrails_TenantMismatchWritesNoRecord: a KM session tapping a KF tag
// redirects (Effect=redirect => the write path writes NOTHING, ADR 0004 §2), so
// the tap lands in NEITHER tenant and a KF manager never sees the KM employee.
func TestGuardrails_TenantMismatchWritesNoRecord(t *testing.T) {
	kf := uuid.New()
	km := uuid.New()
	c := cleanTap(kf)       // tag belongs to KF
	c.SessionTenantID = &km // session belongs to KM
	d := Evaluate(defaultSet(), c)
	if d.Effect != EffectRedirect || d.MatchedSid != "sys:tenant-mismatch" {
		t.Fatalf("a KM session on a KF tag must redirect (no record), got %+v", d)
	}
	if d.PolicyVersionID != nil {
		t.Fatalf("guardrail decision must not carry a version id")
	}
}

// TestGuardrails_NoSessionScopedToTaps: a sessionless TAP redirects to
// activation, but a sessionless AUTHORIZATION action must NOT redirect (it fails
// closed to deny) — otherwise no-session would send an unauthenticated
// report:export to a tap activation page.
func TestGuardrails_NoSessionScopedToTaps(t *testing.T) {
	c := cleanTap(uuid.New())
	c.SessionTenantID = nil
	if d := Evaluate(defaultSet(), c); d.MatchedSid != "sys:no-session" || d.Effect != EffectRedirect {
		t.Fatalf("a sessionless tap should redirect via sys:no-session, got %+v", d)
	}

	az := Context{Action: ActionReportExport, Resources: []string{"*"}}
	d := Evaluate(defaultSet(), az)
	if d.MatchedSid == "sys:no-session" {
		t.Fatalf("no-session must not fire for an authz action, got %+v", d)
	}
	if d.MatchedSid != "default" || d.Effect != EffectDeny {
		t.Fatalf("a sessionless authz action should default-deny, got %+v", d)
	}
}

// --- QR exemption for sun-invalid (§5) --------------------------------------

// TestGuardrails_SunInvalidExemptsQR: a QR tap carries no SUN (sunValid=false is
// expected), so sys:sun-invalid must NOT deny it — the baseline (base:qr-requires-ip)
// governs QR (M3-06). Freshness is likewise NFC-only.
func TestGuardrails_SunInvalidExemptsQR(t *testing.T) {
	c := cleanTap(uuid.New())
	c.Keys[CtxTapChannel] = "qr"
	c.Keys[CtxTapSunValid] = false
	c.Keys[CtxTapPageAgeSeconds] = FreshnessMaxSeconds + 100 // stale, but QR is valid indefinitely
	d := Evaluate(defaultSet(), c)
	if d.MatchedSid == "sys:sun-invalid" || d.MatchedSid == "sys:tap-freshness" {
		t.Fatalf("a QR tap must not be denied by sun-invalid or freshness, got %+v", d)
	}
}

// --- freshness is parameterised ---------------------------------------------

func TestGuardrails_FreshnessWindowIsParameterised(t *testing.T) {
	tenant := uuid.New()
	c := cleanTap(tenant)
	c.Keys[CtxTapPageAgeSeconds] = 120.0

	if d := Evaluate(defaultSet(), c); d.MatchedSid == "sys:tap-freshness" {
		t.Fatalf("120s page with the 900s default window is fresh, got %+v", d)
	}
	p := DefaultParams()
	p.FreshnessWindow = 60 * time.Second
	if d := Evaluate(Set{Guardrails: Guardrails(p)}, c); d.MatchedSid != "sys:tap-freshness" || d.Effect != EffectDeny {
		t.Fatalf("120s page with a 60s window must be denied stale, got %+v", d)
	}
}

// --- authorization guardrails: negative branches ----------------------------

// TestGuardrails_PolicyEditOwnerNotBlocked: the owner-only guardrail does NOT
// fire for the owner (it then fails closed to the default deny, since no policy
// grants it here — M3-06 baseline grants owner policy:edit).
func TestGuardrails_PolicyEditOwnerNotBlocked(t *testing.T) {
	c := Context{Action: ActionPolicyEdit, Resources: []string{"*"}, Keys: map[ContextKey]any{CtxActorRole: "owner"}}
	d := Evaluate(defaultSet(), c)
	if d.MatchedSid == "sys:policy-edit-owner-only" {
		t.Fatalf("the owner must not be blocked by the owner-only guardrail, got %+v", d)
	}
	if d.MatchedSid != "default" || d.Effect != EffectDeny {
		t.Fatalf("owner policy:edit with no granting policy should default-deny, got %+v", d)
	}
}

// TestGuardrails_SelfReviewOnlyWhenSamePerson: distinct reviewer/subject do not
// trip self-review.
func TestGuardrails_SelfReviewOnlyWhenSamePerson(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	c := Context{Action: ActionTapApprove, Resources: []string{"employee/x"}, ReviewerID: &a, SubjectEmployeeID: &b}
	if d := Evaluate(defaultSet(), c); d.MatchedSid == "sys:no-self-review" {
		t.Fatalf("distinct reviewer/subject must not trip self-review, got %+v", d)
	}
}

// --- bounded parameters: single source, wired into DefaultLimits (ADR §11) --

// TestBoundedParams_DefaultLimitsHasExactlyThreeKeys proves the M3-03 hook is now
// filled with the three document-mappable keys, each range read from the named
// constants — and that debounce (which has NO context key) is NOT among them.
func TestBoundedParams_DefaultLimitsHasExactlyThreeKeys(t *testing.T) {
	bp := DefaultLimits().BoundedParams
	want := map[ContextKey]NumericRange{
		CtxTapGPSDistanceM:          {Min: GPSRadiusMinM, Max: GPSRadiusMaxM},
		CtxTapPageAgeSeconds:        {Min: FreshnessMinSeconds, Max: FreshnessMaxSeconds},
		CtxTapOccurredAtSkewSeconds: {Min: OccurredAtSkewMinSeconds, Max: OccurredAtSkewMaxSeconds},
	}
	if len(bp) != len(want) {
		t.Fatalf("want %d bounded keys, got %d (%v)", len(want), len(bp), bp)
	}
	for k, r := range want {
		if bp[k] != r {
			t.Errorf("bounded range for %q = %+v, want %+v", k, bp[k], r)
		}
	}
}

// TestBoundedParams_DocumentValuesEnforced proves a document predicate outside a
// bounded range is rejected at write time (including the Y-L park-wide-disable
// value), and an in-range one passes.
func TestBoundedParams_DocumentValuesEnforced(t *testing.T) {
	lim := DefaultLimits()
	doc := func(key ContextKey, val float64) Document {
		return Document{Version: "v1", Name: "n", Statements: []Statement{{
			Sid: "s", Effect: EffectReview, Action: []Action{ActionTapRecord},
			Resource: []string{"*"}, Condition: Condition{OpNumericLessThan: {key: val}},
		}}}
	}
	bad := []struct {
		key ContextKey
		val float64
	}{
		{CtxTapGPSDistanceM, 20000000},        // Y-L: env-style park-wide disable
		{CtxTapGPSDistanceM, 10},              // below 25
		{CtxTapPageAgeSeconds, 5000},          // above 900
		{CtxTapOccurredAtSkewSeconds, -1},     // below 0
		{CtxTapOccurredAtSkewSeconds, 999999}, // above 72h
	}
	for _, tc := range bad {
		if err := Validate(doc(tc.key, tc.val), LayerTenant, lim); err == nil {
			t.Errorf("value %g on %q should be rejected (out of range)", tc.val, tc.key)
		}
	}
	if err := Validate(doc(CtxTapGPSDistanceM, 150), LayerTenant, lim); err != nil {
		t.Errorf("in-range value 150 on gpsDistanceM should pass: %v", err)
	}
}

// --- Params.Validate / DefaultParams (ADR §11) ------------------------------

func TestParams_Validate(t *testing.T) {
	if err := DefaultParams().Validate(); err != nil {
		t.Fatalf("DefaultParams must be in range: %v", err)
	}
	base := DefaultParams()
	bad := []struct {
		name string
		mut  func(*Params)
	}{
		{"debounce below min", func(p *Params) { p.DebounceWindow = 10 * time.Second }},
		{"debounce above max", func(p *Params) { p.DebounceWindow = 301 * time.Second }},
		{"freshness below min", func(p *Params) { p.FreshnessWindow = 30 * time.Second }},
		{"freshness above max", func(p *Params) { p.FreshnessWindow = 20 * time.Minute }},
		{"skew below min", func(p *Params) { p.OccurredAtSkewMax = -time.Second }},
		{"skew above max", func(p *Params) { p.OccurredAtSkewMax = 100 * time.Hour }},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mut(&p)
			if err := p.Validate(); err == nil {
				t.Fatalf("%s should fail Validate", tc.name)
			}
		})
	}
}
