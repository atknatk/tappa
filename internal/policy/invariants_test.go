package policy

// This file holds the M3-08 INVARIANT tests — the ones that are NOT guardrails
// (not an effect the engine returns) but rules the design as a whole must satisfy
// (M3-05 card: "Guardrail degil, invariant … ayri test edilir"):
//
//   - §4.1  no policy input is derived from a person's body — CLAUDE.md §4.1
//           forbids collecting or storing any body-derived data. The closed
//           context-key vocabulary and the typed Context fields are LOCKED to
//           their exact, human-verified-safe surface, so a new input cannot be
//           added silently (it needs an ADR — ADR 0004 §8 — and that review is
//           where a body-derived input would be caught).
//   - §4.6  the policy-layer counterpart of "records are never lost" — a tap that
//           clears every guardrail but lacks place-evidence is REVIEWED, never
//           dropped: no default/baseline path answers evidence-absence with deny
//           (silent reject), ignore or redirect (record not written). The write
//           path is M5-05; here we prove the policy layer never asks for the
//           record to be thrown away.
//
// It also pins the structural invariant that makes the whole unrelaxability story
// hold: a guardrail may only be RESTRICTIVE.

import (
	"reflect"
	"strings"
	"testing"
)

// --- guardrails may only be restrictive (effect × guardrail-layer completeness)

// TestInvariant_GuardrailsAreOnlyRestrictive proves every guardrail's effect is
// one of deny/ignore/redirect — NEVER allow or review. An allowing guardrail could
// not be a red line, and this is the structural reason the property test holds: the
// terminal first-match layer can only ever restrict. (TestGuardrails_NormativeOrder
// already pins the sids/order; this adds the effect invariant, not covered there.)
func TestInvariant_GuardrailsAreOnlyRestrictive(t *testing.T) {
	for _, g := range DefaultGuardrails() {
		if !isRestrictive(g.Effect) {
			t.Errorf("guardrail %q has effect %q; a guardrail may only be restrictive (deny/ignore/redirect), never allow/review",
				g.Sid, g.Effect)
		}
		if !strings.HasPrefix(g.Sid, "sys:") {
			t.Errorf("guardrail %q leaked outside the reserved sys: namespace", g.Sid)
		}
	}
}

// --- §4.1: no context key or Context field is body-derived --------------------

// TestInvariant_NoBodyDerivedContextKey is the §4.1 invariant (M3-05's system
// "no body-data" rule): NO policy input may be derived from a person's body —
// CLAUDE.md §4.1 forbids collecting or storing any body-derived data. The engine's
// ONLY inputs are the closed context-key vocabulary and the typed Context fields;
// this test LOCKS both to their exact, human-verified-safe surface (place / time /
// status / role / id signals only). Adding ANY new key or field breaks the test
// and forces the ADR-level review the closed list requires (ADR 0004 §8, "yeni
// anahtar = yeni ADR") — that review is where a body-derived input would be caught,
// so one can never enter silently.
//
// It is deliberately STRUCTURAL (an exact-surface lock) rather than a
// forbidden-term keyword match: the keyword scan lives in redline-check.sh R1,
// which scans PRODUCTION code. Pinning the surface here means this test needs to
// name no forbidden term itself, and any drift in the input surface fails loudly.
func TestInvariant_NoBodyDerivedContextKey(t *testing.T) {
	// (a) the closed context-key vocabulary is EXACTLY these 24 keys (ADR 0004 §8),
	// every one a place/time/status/role/tap signal — none a body attribute (§4.1).
	wantKeys := map[ContextKey]bool{
		CtxTapChannel: true, CtxTapSunValid: true, CtxTapIPMatch: true,
		CtxTapGPSMatch: true, CtxTapGPSDistanceM: true, CtxTapGPSConflict: true,
		CtxTapTrust: true, CtxTapCtrGap: true, CtxTapPageAgeSeconds: true,
		CtxTapPractice: true, CtxTapDirection: true, CtxTapQueued: true,
		CtxTapOccurredAtSkewSeconds: true, CtxTagStatus: true, CtxTagLocationID: true,
		CtxEmployeeStatus: true, CtxEmployeeLocationID: true, CtxEmployeeDepartmentID: true,
		CtxEmployeeCrossLocation: true, CtxLocationID: true, CtxTimeLocalHour: true,
		CtxTimeWithinShift: true, CtxTimeMinutesLate: true, CtxActorRole: true,
	}
	if len(wantKeys) != 24 {
		t.Fatalf("the golden key set itself must list 24 keys (ADR 0004 §8), lists %d", len(wantKeys))
	}
	if len(validContextKeys) != len(wantKeys) {
		t.Fatalf("closed context-key vocabulary has %d keys, expected %d — a new key needs an ADR (ADR 0004 §8) and review it is not body-derived (§4.1)",
			len(validContextKeys), len(wantKeys))
	}
	for k := range wantKeys {
		if !validContextKeys[k] {
			t.Errorf("documented key %q is missing from validContextKeys", k)
		}
	}
	for k := range validContextKeys {
		if !wantKeys[k] {
			t.Errorf("undocumented context key %q was added — needs an ADR (ADR 0004 §8) and review it carries no body-derived data (§4.1)", k)
		}
	}

	// (b) the typed, server-derived Context fields are EXACTLY these — the request's
	// action/resources, the key map, and resolved ids; none a body attribute (§4.1).
	// A body-derived input, if ever added, would surface here as a new field and fail.
	wantFields := map[string]bool{
		"Action": true, "Resources": true, "Keys": true,
		"SessionTenantID": true, "TagTenantID": true,
		"SecondsSincePersonLastTap": true, "ReviewerID": true, "SubjectEmployeeID": true,
	}
	rt := reflect.TypeOf(Context{})
	if rt.NumField() != len(wantFields) {
		t.Fatalf("Context has %d fields, expected %d — a new field needs review it is not body-derived (§4.1)", rt.NumField(), len(wantFields))
	}
	for i := 0; i < rt.NumField(); i++ {
		if name := rt.Field(i).Name; !wantFields[name] {
			t.Errorf("Context grew a field %q — review it carries no body-derived data (§4.1) and add it to the golden set", name)
		}
	}
}

// --- §4.6: evidence-absence is reviewed, never dropped -----------------------

// TestInvariant_EvidenceAbsenceIsReviewedNotDropped is the §4.6 invariant at the
// POLICY layer (M3-05's "never lose a record"; write path M5-05). A tap:record that
// clears every guardrail but proves place with NEITHER IP nor GPS must be KEPT for
// REVIEW — never deny (silent reject), never ignore/redirect (record not written).
//
// It runs each evidence-absent variant against BOTH a freshly provisioned tenant
// (full baseline attached) AND a tenant with NO policy at all (guardrails only) —
// the two ways evidence-absence reaches a default. No existing test asserts the
// "never deny/ignore/redirect under evidence-absence" invariant as such; the
// overlap with baseline effect tests is intentional and this framing is the added
// value.
func TestInvariant_EvidenceAbsenceIsReviewedNotDropped(t *testing.T) {
	variants := []struct {
		name    string
		overlay map[ContextKey]any
	}{
		{"no ip, no gps", map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: false}},
		{"no ip, gps signal absent", map[ContextKey]any{CtxTapIPMatch: false}},
		{"no evidence + cross-location", map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: false, CtxEmployeeCrossLocation: true}},
		{"no evidence + practice", map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: false, CtxTapPractice: true}},
		{"no evidence + night hour", map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: false, CtxTimeLocalHour: float64(2)}},
	}
	stacks := []struct {
		name string
		set  Set
	}{
		{"full baseline + guardrails (provisioned tenant)", Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline()}},
		{"guardrails only (no policy at all)", Set{Guardrails: DefaultGuardrails()}},
	}
	for _, st := range stacks {
		for _, v := range variants {
			t.Run(st.name+"/"+v.name, func(t *testing.T) {
				d := Evaluate(st.set, benignTap(v.overlay))
				if d.Effect == EffectDeny || d.Effect == EffectIgnore || d.Effect == EffectRedirect {
					t.Fatalf("evidence-absent tap was DROPPED/REJECTED (%s via %s) — §4.6 requires it be kept for review",
						d.Effect, d.MatchedSid)
				}
				if d.Effect != EffectReview {
					t.Fatalf("evidence-absent tap should be REVIEW, got %s via %s", d.Effect, d.MatchedSid)
				}
			})
		}
	}
}
