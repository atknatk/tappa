package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
		"sys:no-session",
		"sys:employee-deactivated",
		"sys:tap-freshness",
		"sys:occurred-at-bound",
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

// --- the placement rule, as a STRUCTURAL invariant (ADR 0007) ---------------

// namedAlertPreemptors is the allowlist half of the placement rule stated in
// guardrails.go's Guardrails doc comment: the guardrails whose suppression of a
// later alert is DELIBERATE, NAMED and ARGUED (all four also sit in ADR 0007's
// family table). The reasons are never asserted on — they are here so that
// SHRINKING this map, the only way to make a new pre-emption legal, is an edit
// somebody has to write down and defend rather than a line silently skipped.
var namedAlertPreemptors = map[string]string{
	"sys:tenant-mismatch": "redirect writes into no tenant at all, so there is no tenant to alert (§4.5; ADR 0002 Y2)",
	"sys:tag-not-active":  "accepts the loss on `retired`; on `lost` it trades the alert for a more urgent one",
	"sys:sun-invalid":     "the tap is FORGED, and an unauthenticated request must not manufacture an alert (R8)",
	"sys:no-session":      "mutually exclusive with sys:employee-deactivated — tap.Decide sets employee:status and SessionTenantID together (decide.go), so the two can never match one request. A SECOND caller of the exported Evaluate could break that; see the boundary noted in guardrails.go",
}

// unnamedAlertPreemptions applies the placement rule STRUCTURALLY: only the
// WINNING guardrail's Alert reaches the Decision (evaluate.go), so every
// guardrail sitting AHEAD of an alerting one (Alert != nil) is a potential
// suppressor and must appear in namedAlertPreemptors. It returns one message per
// offending pair, naming the suppressor and its victim.
//
// ⚠️ IT DELIBERATELY DOES NOT RESTATE THE 1->10 ORDER, because a second copy of
// that list would be a twin of TestGuardrails_NormativeOrder and inherit its one
// blind spot: a fixed order list also turns red when a guardrail is ADDED, and
// the natural repair is to write the newcomer into the list — which is precisely
// the wrong move if it landed ahead of an alert. Here the shape of the check
// leaves only two repairs, and both are the right ones: move the newcomer
// BEHIND the alerting guardrails, or argue it into the allowlist above.
//
// It is CONSERVATIVE on purpose: it does not try to prove that a suppressor can
// actually co-fire with its victim. That proof is exactly what a future author
// would get wrong — sys:no-session is the single case where it holds, it rests
// on an invariant in ANOTHER package, and it is written down for that reason.
func unnamedAlertPreemptions(gs []Guardrail) []string {
	var bad []string
	for i, g := range gs {
		if g.Alert == nil {
			continue
		}
		for j, ahead := range gs[:i] {
			if _, named := namedAlertPreemptors[ahead.Sid]; named {
				continue
			}
			bad = append(bad, fmt.Sprintf(
				"%s (position %d) sits ahead of the alerting %s (position %d) and is named in no exception",
				ahead.Sid, j+1, g.Sid, i+1))
		}
	}
	return bad
}

// TestGuardrails_NothingUnnamedPreemptsAnAlert is the mechanical net ADR 0007's
// non-guarantee 4 said did not exist: the shipped order must satisfy the
// placement rule, addition included.
func TestGuardrails_NothingUnnamedPreemptsAnAlert(t *testing.T) {
	t.Parallel()
	if bad := unnamedAlertPreemptions(DefaultGuardrails()); len(bad) > 0 {
		t.Fatalf("the shipped guardrail order deletes a security alert:\n  %s\n"+
			"Either move the guardrail behind the alerting one, or argue it into namedAlertPreemptors "+
			"(guardrails.go, the placement rule).", strings.Join(bad, "\n  "))
	}
}

// TestGuardrails_PlacementRuleRejectsTheRegressionOrder is negative control ONE:
// the order that shipped the ADR 0007 regression must be REJECTED by the rule.
// Without this, the invariant above could pass by being vacuous.
//
// The old order is rebuilt by MOVING blocks rather than by spelling out ten sids,
// so it keeps meaning if an eleventh guardrail is added.
func TestGuardrails_PlacementRuleRejectsTheRegressionOrder(t *testing.T) {
	t.Parallel()
	// Pre-ADR-0007: ... sun-invalid(3), tap-freshness(4), occurred-at-bound(5),
	// no-session(6), employee-deactivated(7) ...
	old := moveAhead(t, DefaultGuardrails(), "sys:tap-freshness", "sys:no-session")
	old = moveAhead(t, old, "sys:occurred-at-bound", "sys:no-session")

	bad := strings.Join(unnamedAlertPreemptions(old), "\n")
	for _, want := range []string{"sys:tap-freshness", "sys:occurred-at-bound"} {
		if !strings.Contains(bad, want) {
			t.Errorf("the rule must reject the regression order and BLAME %s by name; got:\n%s", want, bad)
		}
	}
}

// TestGuardrails_PlacementRuleRejectsAnUnnamedEleventhGuardrail is negative
// control TWO, and the one this invariant exists for. A plausible eleventh
// guardrail (an implausible counter jump — a real abuse rule someone will want)
// placed ahead of sys:employee-deactivated deletes §5 row 4's alert exactly as
// the timing rules did. TestGuardrails_NormativeOrder cannot defend against it:
// its fixed list turns red, and updating the list is both the obvious repair and
// the wrong one. This rule stays red until the guardrail moves or is argued for.
func TestGuardrails_PlacementRuleRejectsAnUnnamedEleventhGuardrail(t *testing.T) {
	t.Parallel()
	eleventh := Guardrail{
		Sid:    "sys:counter-jump",
		Effect: EffectDeny,
		Reason: "the tag counter jumped implausibly far",
		Match:  func(Context) bool { return false },
	}
	gs := insertBefore(t, DefaultGuardrails(), eleventh, "sys:employee-deactivated")

	bad := unnamedAlertPreemptions(gs)
	if len(bad) != 1 || !strings.Contains(bad[0], "sys:counter-jump") {
		t.Fatalf("an eleventh guardrail ahead of sys:employee-deactivated must be rejected BY NAME, got %v", bad)
	}
	// And placing the same guardrail BEHIND the alerting ones is accepted, so the
	// rule constrains placement rather than forbidding growth.
	ok := insertBefore(t, DefaultGuardrails(), eleventh, "sys:person-debounce")
	if got := unnamedAlertPreemptions(ok); len(got) > 0 {
		t.Fatalf("an eleventh guardrail placed behind every alert must be accepted, got %v", got)
	}
}

// insertBefore returns gs with g spliced in immediately before beforeSid.
func insertBefore(t *testing.T, gs []Guardrail, g Guardrail, beforeSid string) []Guardrail {
	t.Helper()
	out := make([]Guardrail, 0, len(gs)+1)
	var done bool
	for _, cur := range gs {
		if cur.Sid == beforeSid {
			out = append(out, g)
			done = true
		}
		out = append(out, cur)
	}
	if !done {
		t.Fatalf("insertBefore: no guardrail %q", beforeSid)
	}
	return out
}

// moveAhead returns gs with movedSid lifted out and re-inserted before beforeSid.
func moveAhead(t *testing.T, gs []Guardrail, movedSid, beforeSid string) []Guardrail {
	t.Helper()
	var moved Guardrail
	rest := make([]Guardrail, 0, len(gs))
	for _, g := range gs {
		if g.Sid == movedSid {
			moved = g
			continue
		}
		rest = append(rest, g)
	}
	if moved.Sid == "" {
		t.Fatalf("moveAhead: no guardrail %q", movedSid)
	}
	return insertBefore(t, rest, moved, beforeSid)
}

// TestGuardrails_TimingRulesDoNotPreemptTheDeactivatedAlert pins the ORDER claim
// ADR 0007 exists for, at the layer that owns the order.
//
// Only the WINNING guardrail's Alert reaches the Decision (evaluate.go), so a rule
// placed ahead of sys:employee-deactivated deletes §5 row 4's security alert while
// still denying and still writing the row — a silent loss, because the record looks
// correct. The two rules §5 does not name are exactly the ones that DID: a stale
// page (WAITING is the whole attack) and a client-declared occurred_at (a POST form
// field). Both were measured suppressing the alert before this ordering.
//
// THE THIRD CASE IS NOT A REGRESSION, IT IS THE GAP THIS TEST LEFT. sys:person-
// debounce (#8) also fires on the very same request — a deactivated employee who
// tapped 10 s ago — and would delete the same alert if it were ever moved ahead of
// #5. It is behind today, so the case passes for the RIGHT reason; without it the
// test covered two of the three rules that can co-fire, and an eleventh guardrail
// placed near the debounce would have had nothing to break (guardrails.go, the
// placement rule).
//
// The contrast with the pre-emption that STAYS is the substance: sys:sun-invalid
// (#3) must keep pre-empting, because there the tap is FORGED and an unauthenticated
// request must not be able to manufacture an alert (R8). On the timing paths the
// CMAC verified and the session is live.
func TestGuardrails_TimingRulesDoNotPreemptTheDeactivatedAlert(t *testing.T) {
	t.Parallel()
	// A non-default freshness window: DefaultParams keeps the range maximum, which
	// equals the signed context's TTL, so the deny band would be empty here.
	p := DefaultParams()
	p.FreshnessWindow = 180 * time.Second
	set := Set{Guardrails: Guardrails(p)}

	cases := []struct {
		name      string
		mutate    func(c *Context)
		wantSid   string
		wantAlert SecurityAlert
	}{
		{"stale_page", func(c *Context) { c.Keys[CtxTapPageAgeSeconds] = 300.0 },
			"sys:employee-deactivated", AlertDeactivatedEmployeeTapped},
		{"occurred_at_in_future", func(c *Context) { c.Keys[CtxTapOccurredAtSkewSeconds] = -60.0 },
			"sys:employee-deactivated", AlertDeactivatedEmployeeTapped},
		{"occurred_at_too_old", func(c *Context) { c.Keys[CtxTapOccurredAtSkewSeconds] = float64(OccurredAtSkewMaxSeconds + 1) },
			"sys:employee-deactivated", AlertDeactivatedEmployeeTapped},
		// The third co-firing rule (see the doc comment): §5 names this one, so it
		// was never part of the ADR 0007 fix — but it deletes the same alert from
		// the same request if it ever moves ahead of #5.
		{"person_debounce", func(c *Context) { g := 10.0; c.SecondsSincePersonLastTap = &g },
			"sys:employee-deactivated", AlertDeactivatedEmployeeTapped},
		// The deliberate exception, restated here so the two live side by side and
		// a future reorder has to break one of them to break the other's meaning.
		{"forged_sun_still_preempts", func(c *Context) { c.Keys[CtxTapSunValid] = false },
			"sys:sun-invalid", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := cleanTap(uuid.New())
			c.Keys[CtxEmployeeStatus] = "deactivated"
			tc.mutate(&c)

			d := Evaluate(set, c)

			if d.MatchedSid != tc.wantSid {
				t.Errorf("MatchedSid = %q, want %q", d.MatchedSid, tc.wantSid)
			}
			if d.SecurityAlert != tc.wantAlert {
				t.Errorf("SecurityAlert = %q, want %q (§5 row 4; ADR 0007)", d.SecurityAlert, tc.wantAlert)
			}
			if d.Effect != EffectDeny {
				t.Errorf("Effect = %q, want deny", d.Effect)
			}
		})
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
			// 🔴 THE FOURTH STATUS (migration 00013): a plaque encoded and loaded but
			// never mounted. It denies with NO alert -- stock sitting in a box is not
			// a security event, unlike a plaque someone reported LOST.
			//
			// Before this guardrail became an allow-list this case reached the
			// evidence rules instead: an unmounted plaque has no location, so it had
			// no IP range and no coordinate to be judged against, and a QR-shaped tap
			// on it was RECORDED as `flag` in a manager's approval queue.
			name:    "2 tag unassigned (00013 stock) -> deny, NO alert",
			build:   func() Context { c := cleanTap(tenant); c.Keys[CtxTagStatus] = "unassigned"; return c },
			wantSid: "sys:tag-not-active", wantEff: EffectDeny,
		},
		{
			name:    "3 sun-invalid (nfc) -> deny",
			build:   func() Context { c := cleanTap(tenant); c.Keys[CtxTapSunValid] = false; return c },
			wantSid: "sys:sun-invalid", wantEff: EffectDeny,
		},
		{
			name: "6 tap-freshness (int page age) -> deny",
			// int value exercises toFloat's integer path.
			build: func() Context {
				c := cleanTap(tenant)
				c.Keys[CtxTapPageAgeSeconds] = FreshnessMaxSeconds + 1
				return c
			},
			wantSid: "sys:tap-freshness", wantEff: EffectDeny,
		},
		{
			name:    "7 occurred-at in future -> deny",
			build:   func() Context { c := cleanTap(tenant); c.Keys[CtxTapOccurredAtSkewSeconds] = -1.0; return c },
			wantSid: "sys:occurred-at-bound", wantEff: EffectDeny,
		},
		{
			name: "7 occurred-at too old -> deny",
			build: func() Context {
				c := cleanTap(tenant)
				c.Keys[CtxTapOccurredAtSkewSeconds] = OccurredAtSkewMaxSeconds + 1
				return c
			},
			wantSid: "sys:occurred-at-bound", wantEff: EffectDeny,
		},
		{
			name:    "4 no-session -> redirect, no record",
			build:   func() Context { c := cleanTap(tenant); c.SessionTenantID = nil; return c },
			wantSid: "sys:no-session", wantEff: EffectRedirect,
		},
		{
			name:    "5 employee-deactivated -> deny + alert",
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

// TestDefaultParams_FreshnessStaysAtTheRangeMaximum pins the fallback ITSELF,
// which nothing did before (measured: changing DefaultParams().FreshnessWindow
// from 900 s to 180 s, and then ALSO deleting checkin.New's assignment, left all
// 13 packages green — both existing guards compare the DB harness's own
// constant, so neither could fire).
//
// The property being pinned is not the number, it is the INEQUALITY: the
// no-config fallback must differ from what internal/config ships
// (TAPPA_FRESHNESS_SECONDS = 180 s). If the two were equal, the one line that
// carries the configured value into the guardrail could be deleted and every
// test would still pass on a value that merely LOOKS right — hand-off N3's
// failure, and M5-05's F3, in the shape they actually recur.
//
// It is asserted against FreshnessMaxSeconds rather than against a literal so
// the pin follows a deliberate range change (ADR 0004 §11) instead of turning
// red on one.
func TestDefaultParams_FreshnessStaysAtTheRangeMaximum(t *testing.T) {
	t.Parallel()
	if got, want := DefaultParams().FreshnessWindow, FreshnessMaxSeconds*time.Second; got != want {
		t.Fatalf("DefaultParams().FreshnessWindow = %v, want the range maximum %v: this fallback must stay "+
			"distinguishable from the shipped 180s, or the config wiring becomes unfalsifiable", got, want)
	}
}

// TestGuardrails_FreshnessBoundaryIsStrictlyGreater pins WHICH side of the
// window the boundary second falls on: `age > window` denies, `age == window`
// does not.
//
// It is pinned for one reason — internal/config pins the OTHER boundary
// deliberately (TestLoad_FreshnessRange asserts 60 and 900 are both accepted, an
// inclusive range), so leaving this one to `<` vs `>=` in a Match closure is an
// asymmetry rather than a gap nobody noticed. Measured: flipping the comparison
// to `>=` left policy, tap and checkin green.
//
// The direction is also the right one on its own terms: a page that is exactly
// as old as the window has not yet exceeded it, and a hard-deny guardrail should
// take the second that is not yet stale.
func TestGuardrails_FreshnessBoundaryIsStrictlyGreater(t *testing.T) {
	t.Parallel()
	p := DefaultParams()
	p.FreshnessWindow = 180 * time.Second

	at := cleanTap(uuid.New())
	at.Keys[CtxTapPageAgeSeconds] = 180.0
	if d := Evaluate(Set{Guardrails: Guardrails(p)}, at); d.MatchedSid == "sys:tap-freshness" {
		t.Fatalf("a page aged exactly the window is NOT stale, got %+v", d)
	}

	past := cleanTap(uuid.New())
	past.Keys[CtxTapPageAgeSeconds] = 180.001
	if d := Evaluate(Set{Guardrails: Guardrails(p)}, past); d.MatchedSid != "sys:tap-freshness" || d.Effect != EffectDeny {
		t.Fatalf("a page one millisecond past the window is stale, got %+v", d)
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

// --- sys:tag-not-active: the allow-list, and its equivalence -----------------

// tagStatusesFromSchema derives the tag status vocabulary from the MIGRATIONS
// rather than restating it, so this test cannot drift from the database the way
// the guardrail itself did.
//
// It reads the LAST `status IN (...)` list that any migration declares for `tags`,
// which is the effective CHECK: 00004 defines three values and 00013 drops that
// constraint and re-adds it with four. Reading the last one is what "effective"
// means for a sequence of migrations, and the anti-vacuity check below refuses a
// result that is empty or suspiciously short.
func tagStatusesFromSchema(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading db/migrations: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // 00001..00013: later files override earlier ones

	// The constraint is written as: CHECK (status IN ('a', 'b', ...)) on `tags`.
	re := regexp.MustCompile(`(?is)tags_status_check\s+CHECK\s*\(\s*status\s+IN\s*\(([^)]*)\)`)
	var last []string
	for _, n := range names {
		raw, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("reading %s: %v", n, err)
		}
		// Only the Up half: a Down block restores the OLD list, and reading it
		// would hand this test the three-value vocabulary 00013 replaced.
		body := string(raw)
		if i := strings.Index(body, "-- +goose Down"); i >= 0 {
			body = body[:i]
		}
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			var vals []string
			for _, v := range strings.Split(m[1], ",") {
				vals = append(vals, strings.Trim(strings.TrimSpace(v), "'"))
			}
			last = vals
		}
	}
	if len(last) < 3 {
		t.Fatalf("derived %d tag status(es) from db/migrations (%v); the schema declares "+
			"at least three, so this reader is not finding the constraint", len(last), last)
	}
	return last
}

// TestGuardrails_TagNotActiveIsAnAllowList is the proof behind the 2026-08-09
// inversion of sys:tag-not-active from a deny-list to an allow-list.
//
// 🔴 IT MEASURES THE EQUIVALENCE INSTEAD OF ASSERTING IT. The claim made when the
// guardrail changed was "for today's vocabulary the two forms are identical". That
// is checkable, so it is checked: for every status the SCHEMA declares, the old
// form (retired OR lost) and the new one (!= active) are compared, and they must
// agree on every value that existed before 00013 -- and DISAGREE on `unassigned`,
// which is the entire reason for the change.
//
// The vocabulary is derived, so a future migration that adds a fifth status makes
// this test speak about it automatically: the new value will be denied by the
// shipped guardrail (that is the point) and the old form will be shown not to.
func TestGuardrails_TagNotActiveIsAnAllowList(t *testing.T) {
	statuses := tagStatusesFromSchema(t)

	// The form the guardrail used to have, kept ONLY here, as the thing being
	// compared against. It is not reachable from production code.
	oldDenyList := func(s string) bool { return s == tagStatusRetired || s == tagStatusLost }

	tenant := uuid.New()
	var sawActive, sawDisagreement bool
	for _, s := range statuses {
		s := s
		t.Run(s, func(t *testing.T) {
			c := cleanTap(tenant)
			c.Keys[CtxTagStatus] = s
			d := Evaluate(defaultSet(), c)

			denied := d.Effect == EffectDeny && d.MatchedSid == "sys:tag-not-active"
			if s == tagStatusActive {
				sawActive = true
				if denied {
					t.Fatalf("an ACTIVE tag was denied by sys:tag-not-active; the allow-list "+
						"has inverted the wrong way and no tap can ever succeed (%q/%q)",
						d.Effect, d.MatchedSid)
				}
				return
			}
			if !denied {
				t.Fatalf("status %q reached %q/%q instead of being denied by "+
					"sys:tag-not-active. Every status other than `active` is refused at "+
					"§5 row 1 -- a guardrail no customer can switch off.", s, d.Effect, d.MatchedSid)
			}
			// Equivalence, per value: agreeing means the change was behaviour-
			// preserving there; disagreeing is only allowed where the OLD form was
			// blind, which is exactly the value 00013 introduced.
			if oldDenyList(s) != denied {
				sawDisagreement = true
				if s != "unassigned" {
					t.Fatalf("the two forms disagree on %q, which existed BEFORE 00013. The "+
						"inversion was justified as behaviour-preserving for the old "+
						"vocabulary; it is not.", s)
				}
			}
			// The alert stays exclusive to `lost` -- widening the deny must not widen
			// the alarm, or every routine retirement pages the managers.
			if s == tagStatusLost && d.SecurityAlert != AlertLostTagTapped {
				t.Fatalf("a lost tag produced alert %q, want %q", d.SecurityAlert, AlertLostTagTapped)
			}
			if s != tagStatusLost && d.SecurityAlert != "" {
				t.Fatalf("status %q produced alert %q; only `lost` alerts", s, d.SecurityAlert)
			}
		})
	}

	// ANTI-VACUITY: without an `active` case the loop could pass while denying
	// everything, and without a disagreement the test would be describing a change
	// that had no effect -- i.e. the guardrail is still a deny-list.
	if !sawActive {
		t.Fatal("the derived vocabulary contains no `active`; this test compared nothing useful")
	}
	if !sawDisagreement {
		t.Fatal("the old deny-list and the shipped allow-list agreed on EVERY status the " +
			"schema declares. That means the fourth status is missing from the schema, or " +
			"the guardrail is still enumerating bad values -- either way the inversion is " +
			"not in effect.")
	}
}
