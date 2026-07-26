package policy

import (
	"testing"

	"github.com/google/uuid"
)

// --- builders ---------------------------------------------------------------

// tapStmt is a tap:record statement with a chosen sid, effect, resources and
// condition; Reason defaults to the sid so decision.Reason is checkable.
func tapStmt(sid string, eff Effect, resources []string, cond Condition) Statement {
	return Statement{Sid: sid, Effect: eff, Action: []Action{ActionTapRecord}, Resource: resources, Condition: cond, Reason: sid}
}

// pol wraps statements into a Policy of the given layer with a fresh version id.
func pol(layer Layer, sts ...Statement) Policy {
	return Policy{VersionID: uuid.New(), Layer: layer, Document: Document{Version: "2026-07-26", Name: "test", Statements: sts}}
}

// tapReq is a tap:record request at the given concrete resources (default: a
// single Rusty Bar location) with the given context keys.
func tapReq(keys map[ContextKey]any, resources ...string) Context {
	if len(resources) == 0 {
		resources = []string{"location/rusty-bar"}
	}
	return Context{Action: ActionTapRecord, Resources: resources, Keys: keys}
}

// alwaysGuard is a guardrail whose predicate is constant, for order/terminal tests.
func alwaysGuard(sid string, eff Effect, fires bool) Guardrail {
	return Guardrail{Sid: sid, Effect: eff, Reason: sid, Match: func(Context) bool { return fires }}
}

// --- guardrails: ordered, first match terminal ------------------------------

// TestEvaluate_GuardrailFirstMatchTerminal proves the FIRST matching guardrail
// wins and NO lower layer runs — even a lower guardrail, a baseline and a tenant
// policy that would all otherwise fire are silenced.
func TestEvaluate_GuardrailFirstMatchTerminal(t *testing.T) {
	set := Set{
		Guardrails: []Guardrail{
			alwaysGuard("sys:first", EffectRedirect, true),
			alwaysGuard("sys:second", EffectDeny, true), // also matches, must NOT be chosen
		},
		// A baseline allow that would win if guardrails were skipped.
		Baseline: []Policy{pol(LayerBaseline, tapStmt("base:allow", EffectAllow, []string{"*"}, nil))},
		Tenant:   []Policy{pol(LayerTenant, tapStmt("t:allow", EffectAllow, []string{"*"}, nil))},
	}
	d := Evaluate(set, tapReq(nil))
	if d.Effect != EffectRedirect || d.MatchedSid != "sys:first" {
		t.Fatalf("want first guardrail (redirect/sys:first), got %+v", d)
	}
	if d.Layer != LayerGuardrail {
		t.Fatalf("guardrail decision must carry LayerGuardrail, got %q", d.Layer)
	}
	if d.PolicyVersionID != nil {
		t.Fatal("guardrail decision must have nil PolicyVersionID (code, not DB)")
	}
}

// TestEvaluate_GuardrailOrderMatters proves order is load-bearing: swap the two
// guardrails and the OTHER one wins. (This is the shape M3-05's 1->10 order test
// builds on: a guardrail earlier in the slice pre-empts a later one.)
func TestEvaluate_GuardrailOrderMatters(t *testing.T) {
	deny := alwaysGuard("sys:deny", EffectDeny, true)
	ignore := alwaysGuard("sys:ignore", EffectIgnore, true)

	d1 := Evaluate(Set{Guardrails: []Guardrail{deny, ignore}}, tapReq(nil))
	if d1.MatchedSid != "sys:deny" {
		t.Fatalf("deny-first: want sys:deny, got %q", d1.MatchedSid)
	}
	d2 := Evaluate(Set{Guardrails: []Guardrail{ignore, deny}}, tapReq(nil))
	if d2.MatchedSid != "sys:ignore" {
		t.Fatalf("ignore-first: want sys:ignore, got %q", d2.MatchedSid)
	}
}

// TestEvaluate_GuardrailSkippedWhenNotMatching proves a non-matching (and a
// nil-Match) guardrail is skipped and evaluation falls through to lower layers.
func TestEvaluate_GuardrailSkippedWhenNotMatching(t *testing.T) {
	set := Set{
		Guardrails: []Guardrail{
			{Sid: "sys:nil-match", Effect: EffectDeny, Match: nil}, // never fires
			alwaysGuard("sys:inactive", EffectDeny, false),         // predicate false
		},
		Baseline: []Policy{pol(LayerBaseline, tapStmt("base:allow", EffectAllow, []string{"*"}, nil))},
	}
	d := Evaluate(set, tapReq(nil))
	if d.MatchedSid != "base:allow" || d.Effect != EffectAllow {
		t.Fatalf("want fall-through to base:allow, got %+v", d)
	}
	if d.Layer != LayerBaseline || d.PolicyVersionID == nil {
		t.Fatalf("baseline decision must carry LayerBaseline and a version id, got %+v", d)
	}
}

// TestEvaluate_GuardrailSeesAction proves a guardrail's Match can scope itself by
// action (the mechanism M3-05 uses for sys:policy-edit-owner-only etc.).
func TestEvaluate_GuardrailSeesAction(t *testing.T) {
	g := Guardrail{Sid: "sys:policy-edit-owner-only", Effect: EffectDeny, Reason: "owner only",
		Match: func(c Context) bool { return c.Action == ActionPolicyEdit }}
	set := Set{Guardrails: []Guardrail{g}}

	// policy:edit -> guardrail fires.
	d := Evaluate(set, Context{Action: ActionPolicyEdit, Resources: []string{"*"}})
	if d.MatchedSid != "sys:policy-edit-owner-only" {
		t.Fatalf("policy:edit should hit the guardrail, got %+v", d)
	}
	// tap:record -> guardrail does not fire, falls to default review.
	d = Evaluate(set, tapReq(nil))
	if d.MatchedSid != "default" || d.Effect != EffectReview {
		t.Fatalf("tap:record should skip the authz guardrail, got %+v", d)
	}
}

// --- baseline + tenant: most restrictive wins -------------------------------

// TestEvaluate_MostRestrictiveWinsAtEqualSpecificity proves that among matches of
// EQUAL specificity, the most restrictive effect wins: deny > review > allow.
func TestEvaluate_MostRestrictiveWinsAtEqualSpecificity(t *testing.T) {
	cases := []struct {
		name    string
		effects []Effect
		want    Effect
	}{
		{"deny beats review beats allow", []Effect{EffectAllow, EffectReview, EffectDeny}, EffectDeny},
		{"review beats allow", []Effect{EffectAllow, EffectReview}, EffectReview},
		{"allow alone", []Effect{EffectAllow}, EffectAllow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sts []Statement
			for i, e := range tc.effects {
				sts = append(sts, tapStmt(string(rune('a'+i)), e, []string{"location/*"}, nil))
			}
			d := Evaluate(Set{Tenant: []Policy{pol(LayerTenant, sts...)}}, tapReq(nil))
			if d.Effect != tc.want {
				t.Fatalf("want %q, got %q (sid %q)", tc.want, d.Effect, d.MatchedSid)
			}
		})
	}
}

// TestEvaluate_BaselineAndTenantTogether proves the two layers are pooled: a
// tenant deny out-restricts a baseline allow at the same specificity.
func TestEvaluate_BaselineAndTenantTogether(t *testing.T) {
	set := Set{
		Baseline: []Policy{pol(LayerBaseline, tapStmt("base:allow", EffectAllow, []string{"location/*"}, nil))},
		Tenant:   []Policy{pol(LayerTenant, tapStmt("t:deny", EffectDeny, []string{"location/*"}, nil))},
	}
	d := Evaluate(set, tapReq(nil))
	if d.Effect != EffectDeny || d.MatchedSid != "t:deny" || d.Layer != LayerTenant {
		t.Fatalf("want tenant deny to win at equal specificity, got %+v", d)
	}
}

// --- specific resource overrides general (ADR 0004 §7, the Rusty Bar exception)

// TestEvaluate_SpecificResourceBeatsRestrictiveness is the headline tie-break: a
// specific-resource ALLOW beats a general-resource REVIEW even though allow is
// less restrictive. This is the "IP required everywhere, but GPS is enough at
// Rusty Bar" case.
func TestEvaluate_SpecificResourceBeatsRestrictiveness(t *testing.T) {
	set := Set{
		Baseline: []Policy{pol(LayerBaseline,
			tapStmt("base:review-all", EffectReview, []string{"location/*"}, nil),
		)},
		Tenant: []Policy{pol(LayerTenant,
			tapStmt("t:allow-rusty", EffectAllow, []string{"location/rusty-bar"}, nil),
		)},
	}
	// At Rusty Bar: the specific allow wins despite being less restrictive.
	d := Evaluate(set, tapReq(nil, "location/rusty-bar"))
	if d.Effect != EffectAllow || d.MatchedSid != "t:allow-rusty" {
		t.Fatalf("at rusty-bar want specific allow, got %+v", d)
	}
	// At another location: the specific statement does not match; the general
	// review governs.
	d = Evaluate(set, tapReq(nil, "location/st-julians"))
	if d.Effect != EffectReview || d.MatchedSid != "base:review-all" {
		t.Fatalf("at st-julians want general review, got %+v", d)
	}
}

// TestEvaluate_SpecificityLadder proves the full ladder exact > type-wildcard >
// global, independent of effect restrictiveness.
func TestEvaluate_SpecificityLadder(t *testing.T) {
	set := Set{Tenant: []Policy{pol(LayerTenant,
		tapStmt("global-deny", EffectDeny, []string{"*"}, nil),
		tapStmt("type-review", EffectReview, []string{"location/*"}, nil),
		tapStmt("exact-allow", EffectAllow, []string{"location/rusty-bar"}, nil),
	)}}
	d := Evaluate(set, tapReq(nil, "location/rusty-bar"))
	if d.MatchedSid != "exact-allow" {
		t.Fatalf("exact resource must win the ladder, got %q", d.MatchedSid)
	}
}

// --- determinism ------------------------------------------------------------

// TestEvaluate_Deterministic proves a full tie (equal specificity AND equal
// effect across many statements, spread over baseline and tenant) resolves to the
// SAME MatchedSid on every run — no dependence on Go's random map iteration.
func TestEvaluate_Deterministic(t *testing.T) {
	build := func() Set {
		return Set{
			Baseline: []Policy{pol(LayerBaseline,
				tapStmt("base:r1", EffectReview, []string{"location/*"}, Condition{OpBool: {CtxTapIPMatch: false}}),
				tapStmt("base:r2", EffectReview, []string{"location/*"}, Condition{OpBool: {CtxTapGPSMatch: true}}),
			)},
			Tenant: []Policy{pol(LayerTenant,
				tapStmt("t:r3", EffectReview, []string{"location/*"}, Condition{OpStringEquals: {CtxTapChannel: "nfc"}}),
				tapStmt("t:r4", EffectReview, []string{"location/*"}, nil),
			)},
		}
	}
	keys := map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: true, CtxTapChannel: "nfc"}
	first := Evaluate(build(), tapReq(keys)).MatchedSid
	for i := 0; i < 500; i++ {
		if got := Evaluate(build(), tapReq(keys)).MatchedSid; got != first {
			t.Fatalf("non-deterministic MatchedSid: %q vs %q", first, got)
		}
	}
	// The fixed walk is baseline-first, document order: base:r1 is first to match.
	if first != "base:r1" {
		t.Fatalf("full-tie winner should be first-in-walk base:r1, got %q", first)
	}
}

// --- defaults (ADR 0004 §3) -------------------------------------------------

// TestEvaluate_Defaults proves the two no-match defaults: tap:record -> review,
// every other action (INCLUDING tap:approve) -> deny. All carry MatchedSid
// "default" and a nil version.
func TestEvaluate_Defaults(t *testing.T) {
	cases := []struct {
		action Action
		want   Effect
	}{
		{ActionTapRecord, EffectReview},
		{ActionTapApprove, EffectDeny}, // authz despite tap: prefix (ADR 0004 §3)
		{ActionPolicyEdit, EffectDeny},
		{ActionReportExport, EffectDeny},
		{ActionRecordManual, EffectDeny},
		{ActionRecordReview, EffectDeny},
		{ActionEmployeeDeactivate, EffectDeny},
	}
	for _, tc := range cases {
		t.Run(string(tc.action), func(t *testing.T) {
			d := Evaluate(Set{}, Context{Action: tc.action, Resources: []string{"*"}})
			if d.Effect != tc.want {
				t.Fatalf("default for %q = %q, want %q", tc.action, d.Effect, tc.want)
			}
			if d.MatchedSid != "default" {
				t.Fatalf("default MatchedSid = %q, want \"default\"", d.MatchedSid)
			}
			if d.PolicyVersionID != nil {
				t.Fatal("default must have nil PolicyVersionID")
			}
		})
	}
}

// --- eval-time anomaly must NOT unconditionalize a deny (M3-04 card) ---------

// TestEvaluate_UnknownOperatorDoesNotUnconditionalizeDeny is the exploit the card
// warns about: a stored deny whose condition now names an unknown operator must
// become INERT (fail-safe non-match), not fire unconditionally and reject every
// tap. The result falls through to the default review, and the anomaly is
// reported.
func TestEvaluate_UnknownOperatorDoesNotUnconditionalizeDeny(t *testing.T) {
	var rec []Anomaly
	set := Set{
		OnAnomaly: func(a Anomaly) { rec = append(rec, a) },
		Baseline: []Policy{pol(LayerBaseline,
			// Once-valid deny; after a rollback "StringRegex" is unknown.
			tapStmt("base:deny-regex", EffectDeny, []string{"*"}, Condition{"StringRegex": {CtxTapChannel: "qr"}}),
		)},
	}
	d := Evaluate(set, tapReq(map[ContextKey]any{CtxTapChannel: "qr"}))
	if d.Effect == EffectDeny {
		t.Fatal("unknown operator made a deny fire unconditionally — every tap would be rejected")
	}
	if d.Effect != EffectReview || d.MatchedSid != "default" {
		t.Fatalf("want fall-through to default review, got %+v", d)
	}
	if len(rec) == 0 || rec[0].Kind != AnomalyUnknownOperator {
		t.Fatalf("the eval-time anomaly signal was lost: %+v", rec)
	}
}

// TestEvaluate_UnknownOperatorDoesNotUnconditionalizeAllow proves the same
// fail-safe protects the other direction: a stranded allow does not silently
// approve.
func TestEvaluate_UnknownOperatorDoesNotUnconditionalizeAllow(t *testing.T) {
	set := Set{
		OnAnomaly: discard,
		Tenant: []Policy{pol(LayerTenant,
			tapStmt("t:allow-regex", EffectAllow, []string{"*"}, Condition{"StringRegex": {CtxTapChannel: "qr"}}),
		)},
	}
	d := Evaluate(set, tapReq(map[ContextKey]any{CtxTapChannel: "qr"}))
	if d.Effect == EffectAllow {
		t.Fatal("unknown operator let an allow fire unconditionally")
	}
	if d.MatchedSid != "default" {
		t.Fatalf("want default fall-through, got %+v", d)
	}
}

// TestEvaluate_DefaultAnomalySinkDoesNotPanic proves that when OnAnomaly is nil
// the fallback slog path runs without panicking (the signal still is not lost).
func TestEvaluate_DefaultAnomalySinkDoesNotPanic(t *testing.T) {
	set := Set{ // OnAnomaly nil -> slog fallback
		Tenant: []Policy{pol(LayerTenant,
			tapStmt("t:deny-regex", EffectDeny, []string{"*"}, Condition{"StringRegex": {CtxTapChannel: "qr"}}),
		)},
	}
	d := Evaluate(set, tapReq(map[ContextKey]any{CtxTapChannel: "qr"}))
	if d.Effect == EffectDeny {
		t.Fatal("deny fired unconditionally even on the slog fallback path")
	}
}

// --- action / resource non-matches ------------------------------------------

// TestEvaluate_ActionMismatchDoesNotFire proves a statement scoped to a different
// action is skipped.
func TestEvaluate_ActionMismatchDoesNotFire(t *testing.T) {
	// A tenant statement only for report:export must not touch a tap:record req.
	st := Statement{Sid: "t:export", Effect: EffectAllow, Action: []Action{ActionReportExport}, Resource: []string{"*"}}
	set := Set{Tenant: []Policy{pol(LayerTenant, st)}}
	d := Evaluate(set, tapReq(nil))
	if d.MatchedSid != "default" || d.Effect != EffectReview {
		t.Fatalf("action mismatch should fall to default review, got %+v", d)
	}
}

// TestEvaluate_ResourceMismatchDoesNotFire proves a statement whose resource does
// not intersect the request is skipped.
func TestEvaluate_ResourceMismatchDoesNotFire(t *testing.T) {
	set := Set{Tenant: []Policy{pol(LayerTenant,
		tapStmt("t:dept-only", EffectDeny, []string{"department/kitchen"}, nil),
	)}}
	// Request targets a location only — the department statement must not match.
	d := Evaluate(set, tapReq(nil, "location/rusty-bar"))
	if d.Effect == EffectDeny {
		t.Fatalf("resource mismatch fired: %+v", d)
	}
	if d.MatchedSid != "default" {
		t.Fatalf("want default, got %+v", d)
	}
}

// TestEvaluate_MultiResourceRequest proves a request carrying several concrete
// resources matches a statement scoped to any one of them, at that pattern's
// specificity.
func TestEvaluate_MultiResourceRequest(t *testing.T) {
	set := Set{Tenant: []Policy{pol(LayerTenant,
		tapStmt("t:kitchen-deny", EffectDeny, []string{"department/kitchen"}, nil),
		tapStmt("t:loc-review", EffectReview, []string{"location/*"}, nil),
	)}}
	// Request is a kitchen employee tapping at a location: both match; deny
	// (exact department) out-specifics the type-wildcard location review.
	d := Evaluate(set, tapReq(nil, "location/rusty-bar", "department/kitchen"))
	if d.MatchedSid != "t:kitchen-deny" || d.Effect != EffectDeny {
		t.Fatalf("want exact department deny to win, got %+v", d)
	}
}

// --- explainability & condition gating --------------------------------------

// TestEvaluate_ConditionGatesMatch proves a statement fires only when its
// condition holds, and that Reason and version id propagate to the Decision.
func TestEvaluate_ConditionGatesMatch(t *testing.T) {
	p := pol(LayerTenant, Statement{
		Sid: "t:gps-only-review", Effect: EffectReview, Action: []Action{ActionTapRecord},
		Resource: []string{"location/*"}, Reason: "verified via GPS only",
		Condition: Condition{OpBool: {CtxTapIPMatch: false, CtxTapGPSMatch: true}},
	})
	set := Set{Tenant: []Policy{p}}

	// Condition holds -> statement fires, Reason + version id surface.
	d := Evaluate(set, tapReq(map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: true}))
	if d.MatchedSid != "t:gps-only-review" || d.Reason != "verified via GPS only" {
		t.Fatalf("want the gated statement to fire with its reason, got %+v", d)
	}
	if d.PolicyVersionID == nil || *d.PolicyVersionID != p.VersionID {
		t.Fatalf("PolicyVersionID must point at the deciding version, got %+v", d.PolicyVersionID)
	}

	// Condition fails -> falls to default.
	d = Evaluate(set, tapReq(map[ContextKey]any{CtxTapIPMatch: true, CtxTapGPSMatch: true}))
	if d.MatchedSid != "default" {
		t.Fatalf("condition should have gated the statement out, got %+v", d)
	}
}

// TestEvaluate_EveryDecisionIsExplainable sweeps the distinct decision paths and
// asserts each carries a non-empty MatchedSid and a known Layer (ADR 0004 §10).
func TestEvaluate_EveryDecisionIsExplainable(t *testing.T) {
	decisions := []Decision{
		Evaluate(Set{Guardrails: []Guardrail{alwaysGuard("sys:x", EffectDeny, true)}}, tapReq(nil)),
		Evaluate(Set{Tenant: []Policy{pol(LayerTenant, tapStmt("t:a", EffectAllow, []string{"*"}, nil))}}, tapReq(nil)),
		Evaluate(Set{}, tapReq(nil)), // default review
		Evaluate(Set{}, Context{Action: ActionPolicyEdit, Resources: []string{"*"}}), // default deny
	}
	known := map[Layer]bool{LayerGuardrail: true, LayerBaseline: true, LayerTenant: true}
	for i, d := range decisions {
		if d.MatchedSid == "" {
			t.Errorf("decision %d has an empty MatchedSid: %+v", i, d)
		}
		if !known[d.Layer] {
			t.Errorf("decision %d has an unknown Layer %q", i, d.Layer)
		}
		if !d.Effect.Valid() {
			t.Errorf("decision %d has an invalid effect %q", i, d.Effect)
		}
	}
}
