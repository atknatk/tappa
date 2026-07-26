package policy

// This file is the CENTRAL M3-08 deliverable: the UNRELAXABILITY PROOF (ADR 0004
// §4-§5; M3-08 card: "gevşetilemezlik kanıtı"). Its headline test proves the
// non-negotiable property — no randomly generated tenant (or baseline) policy can
// turn a guardrail's deny/ignore/redirect verdict into allow.
//
// The generator is DETERMINISTIC (a fixed-seed math/rand, never the auto-seeded
// global) so the exact same (context, adversarial-policy) pairs run on every
// invocation and under -count=N — a failure is always reproducible and CI can
// never flake (M3-08 card: "tohum sabit").
//
// The test is NON-VACUOUS by construction, proven on EVERY iteration, not just by
// a one-off manual check: for each generated case it also evaluates the SAME
// adversarial documents with the guardrails REMOVED and asserts the result IS
// allow (the CONTROL). That proves the generator really produces a
// guardrail-overriding allow attempt whose ONLY obstacle is the guardrail layer —
// so a green run means "the guardrails held", never "the attack was toothless".
// (The M3-08 report additionally shows that breaking guardrail terminality turns
// this test RED.)

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	// propertySeed fixes the generator: same sequence every run and under -count=N,
	// so a failure reproduces and CI cannot flake (M3-08 card). Changing it explores
	// a different but equally-fixed sequence.
	propertySeed int64 = 20260726
	// propertyIterations is large enough to exercise every scenario × many document
	// shapes, cheap because Evaluate/Validate are pure functions.
	propertyIterations = 2000
)

// --- the property -----------------------------------------------------------

// TestGuardrail_NoTenantPolicyCanLoosen is the unrelaxability property (M3-08):
// across propertyIterations deterministically-generated cases, a VALID adversarial
// tenant (and, half the time, baseline) document that tries as hard as it can to
// force an allow NEVER flips a guardrail's restrictive verdict. On every iteration
// it also proves NON-VACUITY (the CONTROL: the same documents DO allow once the
// guardrails are removed) and DETERMINISM (same inputs → same Decision).
func TestGuardrail_NoTenantPolicyCanLoosen(t *testing.T) {
	rng := rand.New(rand.NewSource(propertySeed))
	scenarios := guardrailScenarios()

	for i := 0; i < propertyIterations; i++ {
		sc := scenarios[rng.Intn(len(scenarios))]
		ctx := sc.build(rng)

		// (0) SANITY: the scenario really trips a RESTRICTIVE guardrail with no
		// policies present — the very verdict a tenant must not be able to flip.
		bare := Evaluate(Set{Guardrails: DefaultGuardrails()}, ctx)
		if bare.Layer != LayerGuardrail || !isRestrictive(bare.Effect) {
			t.Fatalf("iter %d [%s]: scenario did not trip a restrictive guardrail (got %s/%s) — generator bug; ctx=%s",
				i, sc.name, bare.Effect, bare.MatchedSid, ctxSummary(ctx))
		}

		// (1) A VALID, adversarial tenant document — all-allow, broad resources,
		// holding conditions. Assert it is admissible: a property test must feed
		// documents that survive write-time validation (M3-08: "geçerli rastgele").
		tdoc := randAllowDoc(rng, ctx, i, "t:", "adversarial-tenant")
		if err := Validate(tdoc, LayerTenant, DefaultLimits()); err != nil {
			t.Fatalf("iter %d [%s]: generated tenant document is INVALID (%v); doc=%s", i, sc.name, err, mustJSON(tdoc))
		}
		tenant := []Policy{{VersionID: uuid.New(), Layer: LayerTenant, Document: tdoc}}

		// Half the time add an adversarial BASELINE document too — proving neither
		// non-guardrail layer can loosen a guardrail.
		var baseline []Policy
		if rng.Intn(2) == 0 {
			bdoc := randAllowDoc(rng, ctx, i, "base:", "adversarial-baseline")
			if err := Validate(bdoc, LayerBaseline, DefaultLimits()); err != nil {
				t.Fatalf("iter %d [%s]: generated baseline document is INVALID (%v); doc=%s", i, sc.name, err, mustJSON(bdoc))
			}
			baseline = []Policy{{VersionID: uuid.New(), Layer: LayerBaseline, Document: bdoc}}
		}

		// (2) NON-VACUITY CONTROL: without guardrails, these same documents DO win
		// an allow. If this ever fails the generator stopped attacking and a green
		// run would prove nothing.
		ctrl := Evaluate(Set{Baseline: baseline, Tenant: tenant, OnAnomaly: discard}, ctx)
		if ctrl.Effect != EffectAllow {
			t.Fatalf("iter %d [%s]: NON-VACUITY BROKEN — without guardrails the adversarial policy should ALLOW, got %s/%s; tenant=%s",
				i, sc.name, ctrl.Effect, ctrl.MatchedSid, mustJSON(tdoc))
		}

		// (3) THE PROPERTY: with the guardrails present, the restrictive verdict
		// stands unchanged — no allow, still the guardrail layer, still a sys: sid.
		full := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: baseline, Tenant: tenant, OnAnomaly: discard}, ctx)
		if full.Effect == EffectAllow {
			t.Fatalf("iter %d [%s]: GUARDRAIL LOOSENED TO ALLOW by a policy — %s/%s; tenant=%s ctx=%s",
				i, sc.name, full.Effect, full.MatchedSid, mustJSON(tdoc), ctxSummary(ctx))
		}
		if full.Effect != bare.Effect || full.Layer != LayerGuardrail || !strings.HasPrefix(full.MatchedSid, "sys:") {
			t.Fatalf("iter %d [%s]: guardrail verdict changed under policies: bare=%s/%s full=%s/%s; tenant=%s",
				i, sc.name, bare.Effect, bare.MatchedSid, full.Effect, full.MatchedSid, mustJSON(tdoc))
		}

		// (4) DETERMINISM: the same (broken/conflicting) set yields the same
		// Decision every time (M3-08: "aynı girdi → aynı çıktı").
		again := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: baseline, Tenant: tenant, OnAnomaly: discard}, ctx)
		if again.Effect != full.Effect || again.MatchedSid != full.MatchedSid || again.Layer != full.Layer {
			t.Fatalf("iter %d [%s]: non-deterministic decision %s/%s vs %s/%s",
				i, sc.name, full.Effect, full.MatchedSid, again.Effect, again.MatchedSid)
		}
	}
}

// isRestrictive reports whether e is one of the three guardrail-only restrictive
// effects — the verdicts the property forbids a policy from turning into allow.
func isRestrictive(e Effect) bool {
	return e == EffectDeny || e == EffectIgnore || e == EffectRedirect
}

// --- scenario generators (each trips exactly one restrictive guardrail) ------

// gscenario names a context builder that trips a restrictive guardrail. Each build
// randomises its benign surroundings (location, magnitudes) from rng so the same
// guardrail is exercised under many shapes.
type gscenario struct {
	name  string
	build func(rng *rand.Rand) Context
}

// guardrailScenarios returns one builder per restrictive guardrail (redirect/deny/
// ignore across guardrails 1-10). Tap scenarios start from cleanTap — a context
// that clears EVERY guardrail — and mutate exactly one field, so the intended
// guardrail is the first (and only) match (guardrails are terminal in order).
func guardrailScenarios() []gscenario {
	return []gscenario{
		{"1 tenant-mismatch -> redirect", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			other := uuid.New()
			c.TagTenantID = &other
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"2 tag-retired -> deny", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			c.Keys[CtxTagStatus] = "retired"
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"2 tag-lost -> deny", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			c.Keys[CtxTagStatus] = "lost"
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"3 sun-invalid -> deny", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			c.Keys[CtxTapSunValid] = false
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"4 tap-freshness -> deny", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			c.Keys[CtxTapPageAgeSeconds] = float64(FreshnessMaxSeconds + 1 + rng.Intn(1000))
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"5 occurred-at future -> deny", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			c.Keys[CtxTapOccurredAtSkewSeconds] = -float64(1 + rng.Intn(100))
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"5 occurred-at too old -> deny", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			c.Keys[CtxTapOccurredAtSkewSeconds] = float64(OccurredAtSkewMaxSeconds + 1 + rng.Intn(1000))
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"6 no-session -> redirect", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			c.SessionTenantID = nil
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"7 employee-deactivated -> deny", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			c.Keys[CtxEmployeeStatus] = "deactivated"
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"8 person-debounce -> ignore", func(rng *rand.Rand) Context {
			c := cleanTap(uuid.New())
			gap := float64(rng.Intn(60)) // 0..59 s, inside the 60 s default window
			c.SecondsSincePersonLastTap = &gap
			c.Resources = []string{randLoc(rng)}
			return c
		}},
		{"9 policy-edit non-owner -> deny", func(rng *rand.Rand) Context {
			keys := map[ContextKey]any{}
			if rng.Intn(2) == 0 { // half the time a non-owner role, half a missing role
				keys[CtxActorRole] = "manager"
			}
			return Context{Action: ActionPolicyEdit, Resources: []string{"*"}, Keys: keys}
		}},
		{"10 self-review -> deny", func(rng *rand.Rand) Context {
			id := uuid.New()
			return Context{Action: ActionTapApprove, Resources: []string{"employee/x"},
				ReviewerID: &id, SubjectEmployeeID: &id, Keys: map[ContextKey]any{}}
		}},
	}
}

// --- adversarial document generator (VALID, all-allow) -----------------------

// randAllowDoc builds a VALID document (Validate-passing at the layer implied by
// sidPrefix) that tries as hard as it can to force an allow for ctx. Its FIRST
// statement is the guaranteed matcher: allow, resource "*", the request's action,
// and NO condition — so it matches ctx unconditionally and makes the no-guardrail
// CONTROL deterministically allow. Extra all-allow statements add broad resources
// and holding conditions for shape variety. Every statement is allow — the attack
// is precisely "turn this into allow".
func randAllowDoc(rng *rand.Rand, ctx Context, idx int, sidPrefix, name string) Document {
	sts := []Statement{{
		Sid:      fmt.Sprintf("%sattack-%d-0", sidPrefix, idx),
		Effect:   EffectAllow,
		Action:   randActionsIncluding(rng, ctx.Action),
		Resource: []string{"*"},
		Reason:   "adversarial: force allow (guaranteed matcher)",
	}}
	for j, extra := 1, rng.Intn(3); j <= extra; j++ {
		sts = append(sts, Statement{
			Sid:       fmt.Sprintf("%sattack-%d-%d", sidPrefix, idx, j),
			Effect:    EffectAllow,
			Action:    randActionsIncluding(rng, ctx.Action),
			Resource:  randResources(rng, ctx),
			Condition: randHoldingCondition(rng, ctx),
			Reason:    "adversarial: force allow (extra)",
		})
	}
	return Document{Version: "2026-07-26", Name: name, Statements: sts}
}

// randActionsIncluding returns a valid action list that ALWAYS contains must, plus
// a random subset of the others — so the statement matches ctx.Action while
// exercising multi-action statements. Bounded well under MaxActionsPerStatement.
func randActionsIncluding(rng *rand.Rand, must Action) []Action {
	all := []Action{
		ActionTapRecord, ActionTapApprove, ActionRecordManual, ActionRecordReview,
		ActionEmployeeDeactivate, ActionReportExport, ActionPolicyEdit,
	}
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	out := []Action{must}
	for _, a := range all[:rng.Intn(len(all)+1)] {
		if a != must {
			out = append(out, a)
		}
	}
	return out
}

// randResources returns a valid resource set that ALWAYS includes "*" (so the
// statement matches any request) plus a random subset of other valid patterns,
// including ctx's concrete resources — exercising the specificity ladder while
// keeping the control's match guaranteed.
func randResources(rng *rand.Rand, ctx Context) []string {
	pool := append([]string{"location/*", "location/rusty-bar", "department/kitchen", "employee/x"}, ctx.Resources...)
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	out := []string{"*"}
	seen := map[string]bool{"*": true}
	for _, r := range pool[:rng.Intn(len(pool)+1)] {
		if validResource(r) && !seen[r] {
			out = append(out, r)
			seen[r] = true
		}
	}
	return out
}

// randHoldingCondition returns either nil (unconditional) or an Exists condition
// over present ctx keys — which is always a valid shape (bool) AND always holds
// (the keys are present), so an adversarial extra statement still matches. Using
// Exists avoids bounded-range validation concerns on numeric keys.
func randHoldingCondition(rng *rand.Rand, ctx Context) Condition {
	if len(ctx.Keys) == 0 || rng.Intn(2) == 0 {
		return nil
	}
	keys := make([]ContextKey, 0, len(ctx.Keys))
	for k := range ctx.Keys {
		if k.Valid() {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	args := map[ContextKey]any{}
	for _, k := range keys[:1+rng.Intn(len(keys))] {
		args[k] = true // Exists:true — holds because the key is present
	}
	return Condition{OpExists: args}
}

// randLoc returns a random valid location resource, for benign surroundings.
func randLoc(rng *rand.Rand) string {
	locs := []string{"location/rusty-bar", "location/st-julians", "location/hq", "location/valletta"}
	return locs[rng.Intn(len(locs))]
}

// --- failure-report helpers (§4.7-safe: synthetic data, key NAMES only) ------

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	return string(b)
}

// ctxSummary renders a context for a failure message using key NAMES only (never
// values), keeping the §4.7 no-value discipline even though these inputs are
// synthetic.
func ctxSummary(c Context) string {
	names := make([]string, 0, len(c.Keys))
	for _, k := range sortedKeys(c.Keys) {
		names = append(names, string(k))
	}
	return fmt.Sprintf("action=%s resources=%v keys=%v session=%v tag=%v debounce=%v reviewer=%v",
		c.Action, c.Resources, names, c.SessionTenantID != nil, c.TagTenantID != nil,
		c.SecondsSincePersonLastTap != nil, c.ReviewerID != nil)
}
