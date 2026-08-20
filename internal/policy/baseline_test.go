package policy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// This file proves the M3-06 acceptance criteria for the Tappa-managed baseline
// (baseline.go): each of the eight tap-evidence statements yields its documented
// effect when guardrails are present but do not match; the authorization allows
// prevent the fail-closed panel lockout of a fresh tenant; base: is reserved from
// tenant documents; ctr-gap-review is resource-scoped and overridable; a tenant
// override never touches a guardrail; and no baseline statement produces
// ignore/redirect. Guardrails come from DefaultGuardrails() (M3-05) throughout, so
// the tests exercise the REAL guardrail+baseline stack, not a stub.

// --- helpers ----------------------------------------------------------------

// stableVersionID gives every baseline policy a fresh id; tests assert on
// MatchedSid/Effect/Layer, never on the id value itself.
func stableVersionID(string) uuid.UUID { return uuid.New() }

// fullBaseline is the complete managed baseline as evaluator Policies.
func fullBaseline() []Policy { return BaselinePolicies(stableVersionID) }

// baselineDocFor returns the single BaselineDoc whose statements include sid, as a
// one-element []Policy — for isolating one baseline statement in a Set.
func baselineDocFor(t *testing.T, sid string) []Policy {
	t.Helper()
	for _, d := range Baseline() {
		for _, st := range d.Document.Statements {
			if st.Sid == sid {
				return []Policy{{VersionID: uuid.New(), Layer: LayerBaseline, Document: d.Document}}
			}
		}
	}
	t.Fatalf("no baseline document contains sid %q", sid)
	return nil
}

// benignTap is a tap:record Context at Rusty Bar that passes EVERY DefaultGuardrail
// (session present and same-tenant as the tag, active tag, valid NFC SUN, fresh
// page, in-bounds skew, active employee, no debounce, no self-review). Per-test
// keys are overlaid so a test controls exactly the baseline signals it means to.
func benignTap(overlay map[ContextKey]any, resources ...string) Context {
	tid := uuid.New()
	keys := map[ContextKey]any{
		CtxTagStatus:                "active",
		CtxTapChannel:               channelNFC,
		CtxTapSunValid:              true,
		CtxTapPageAgeSeconds:        float64(10),
		CtxTapOccurredAtSkewSeconds: float64(5),
		CtxEmployeeStatus:           "active",
	}
	for k, v := range overlay {
		keys[k] = v
	}
	if len(resources) == 0 {
		resources = []string{"location/rusty-bar"}
	}
	return Context{
		Action:          ActionTapRecord,
		Resources:       resources,
		SessionTenantID: &tid,
		TagTenantID:     &tid,
		Keys:            keys,
	}
}

// --- (1) each baseline statement, guardrails present but not matching ---------

// TestBaseline_EachStatementEffect proves every one of the eight tap-evidence
// statements produces its documented effect when the full guardrail list runs
// first and none of them match. Each case isolates ONE baseline document so the
// deciding MatchedSid is unambiguous.
func TestBaseline_EachStatementEffect(t *testing.T) {
	cases := []struct {
		name    string
		sid     string
		overlay map[ContextKey]any
		want    Effect
	}{
		{"ip present -> allow", SidIPOrGPSOK,
			map[ContextKey]any{CtxTapIPMatch: true}, EffectAllow},
		{"gps only -> allow", SidGPSOnlyAllow,
			map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: true}, EffectAllow},
		{"no evidence -> review", SidNoEvidenceReview,
			map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: false}, EffectReview},
		{"qr without ip -> review", SidQRRequiresIP,
			map[ContextKey]any{CtxTapChannel: channelQR, CtxTapIPMatch: false}, EffectReview},
		{"cross-location -> allow", SidCrossLocationNote,
			map[ContextKey]any{CtxEmployeeCrossLocation: true}, EffectAllow},
		{"stale occurred_at -> review", SidQueuedWindow,
			map[ContextKey]any{CtxTapOccurredAtSkewSeconds: float64(BaselineQueuedSkewSeconds + 80)}, EffectReview},
		{"ctr gap -> review", SidCtrGapReview,
			map[ContextKey]any{CtxTapCtrGap: float64(1)}, EffectReview},
		{"gps conflict -> review", SidGPSConflictReview,
			map[ContextKey]any{CtxTapGPSConflict: true}, EffectReview},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := Set{Guardrails: DefaultGuardrails(), Baseline: baselineDocFor(t, tc.sid)}
			d := Evaluate(set, benignTap(tc.overlay))
			if d.Effect != tc.want {
				t.Fatalf("%s: effect = %q, want %q (matched %q)", tc.sid, d.Effect, tc.want, d.MatchedSid)
			}
			if d.MatchedSid != tc.sid {
				t.Fatalf("%s: MatchedSid = %q, want %q", tc.sid, d.MatchedSid, tc.sid)
			}
			if d.Layer != LayerBaseline {
				t.Fatalf("%s: Layer = %q, want baseline", tc.sid, d.Layer)
			}
			if d.PolicyVersionID == nil {
				t.Fatalf("%s: baseline decision must carry a policy version id", tc.sid)
			}
		})
	}
}

// TestBaseline_GPSOnlyCarriesNote proves base:gps-only-allow surfaces its
// "verified via GPS" note (Q16), so the docket can explain a GPS-only allow.
func TestBaseline_GPSOnlyCarriesNote(t *testing.T) {
	set := Set{Guardrails: DefaultGuardrails(), Baseline: baselineDocFor(t, SidGPSOnlyAllow)}
	d := Evaluate(set, benignTap(map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: true}))
	if !strings.Contains(strings.ToLower(d.Reason), "gps") {
		t.Fatalf("gps-only allow reason should mention GPS, got %q", d.Reason)
	}
}

// TestBaseline_GPSConflictReasonDescribesTheTestAndNotAnAttackStory is backlog
// T23's guard.
//
// 🔴 THE REASON IS NOT DOCUMENTATION, IT IS PRODUCT COPY. internal/handler's
// policies screen prints a statement's Reason to the customer verbatim, so a reason
// describing a condition the statement does not test is the product explaining a
// mechanism it does not have. The shipped one said "IP matched but GPS places the
// device away from the location"; the condition is one key, and tap.gpsFacts derives
// it from DISTANCE ALONE — a far GPS fires this statement whether or not any IP
// matched.
//
// It asserts the SHAPE rather than the exact sentence: the reason must speak about
// GPS (what it tests) and must not claim an IP match (what it does not).
func TestBaseline_GPSConflictReasonDescribesTheTestAndNotAnAttackStory(t *testing.T) {
	set := Set{Guardrails: DefaultGuardrails(), Baseline: baselineDocFor(t, SidGPSConflictReview)}
	d := Evaluate(set, benignTap(map[ContextKey]any{CtxTapGPSConflict: true}))
	if d.MatchedSid != SidGPSConflictReview {
		t.Fatalf("matched %q, want %q", d.MatchedSid, SidGPSConflictReview)
	}
	reason := strings.ToLower(d.Reason)
	if !strings.Contains(reason, "gps") {
		t.Errorf("the reason does not mention GPS, which is the only thing it tests: %q", d.Reason)
	}
	// The condition carries no address key at all, so any sentence about the IP is a
	// claim about a narrowing that is not there.
	for _, claim := range []string{"ip matched", "ip match", "the ip"} {
		if strings.Contains(reason, claim) {
			t.Errorf("the reason claims %q, but the statement's condition is %v — the customer "+
				"reads this string on the policies screen: %q", claim, CtxTapGPSConflict, d.Reason)
		}
	}
}

// --- (2) full baseline composes correctly -----------------------------------

// TestBaseline_Composition proves several statements pooled together resolve to
// the right outcome via most-restrictive-wins — the cases that only appear when
// multiple baseline statements match one tap.
func TestBaseline_Composition(t *testing.T) {
	base := fullBaseline()
	cases := []struct {
		name    string
		overlay map[ContextKey]any
		want    Effect
		wantSid string
	}{
		// Q15: a QR tap proven only by GPS matches BOTH qr-requires-ip (review) and
		// gps-only-allow (allow); review wins -> GPS alone is not enough for QR.
		{"qr + gps-only -> review", map[ContextKey]any{
			CtxTapChannel: channelQR, CtxTapIPMatch: false, CtxTapGPSMatch: true},
			EffectReview, SidQRRequiresIP},
		// A cross-location tap with NO evidence must still land on review — the
		// cross-location allow does not open a hole.
		{"cross-location + no evidence -> review", map[ContextKey]any{
			CtxEmployeeCrossLocation: true, CtxTapIPMatch: false, CtxTapGPSMatch: false},
			EffectReview, SidNoEvidenceReview},
		// Y-E on-site proxy: IP matches but GPS conflicts. The IP allow is
		// out-restricted by the conflict review — the attack is flagged despite the
		// "trusted" IP match.
		{"ip match + gps conflict -> review", map[ContextKey]any{
			CtxTapIPMatch: true, CtxTapGPSMatch: false, CtxTapGPSConflict: true},
			EffectReview, SidGPSConflictReview},
		// A clean IP-proven tap sails through to allow.
		{"clean ip tap -> allow", map[ContextKey]any{
			CtxTapIPMatch: true, CtxTapGPSMatch: false},
			EffectAllow, SidIPOrGPSOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: base}, benignTap(tc.overlay))
			if d.Effect != tc.want || d.MatchedSid != tc.wantSid {
				t.Fatalf("got %q/%q, want %q/%q", d.Effect, d.MatchedSid, tc.want, tc.wantSid)
			}
		})
	}
}

// --- (3) authorization: fail-closed lockout is prevented ---------------------

// TestBaseline_AuthzPreventsFailClosedLockout is the headline authorization
// criterion: with ONLY the baseline attached (a freshly provisioned tenant, no
// tenant policies) an owner can use every panel action and a manager the
// operational subset — otherwise the fail-closed default (ADR 0004 §3) would lock
// everyone out. The manager subset now includes employee:deactivate (customer
// decision, 2026-07-26). It ALSO proves the closure still holds: policy:edit stays
// owner-only for a manager, and a request with no role is denied.
func TestBaseline_AuthzPreventsFailClosedLockout(t *testing.T) {
	set := Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline()}
	authz := func(role string, action Action) Decision {
		keys := map[ContextKey]any{}
		if role != "" {
			keys[CtxActorRole] = role
		}
		return Evaluate(set, Context{Action: action, Resources: []string{"*"}, Keys: keys})
	}

	// Owner: full authority — every authorization action allows (NOT the
	// fail-closed default deny).
	for _, a := range []Action{
		ActionReportExport, ActionTapApprove, ActionRecordManual,
		ActionRecordReview, ActionEmployeeDeactivate, ActionPolicyEdit,
	} {
		if d := authz(roleOwner, a); d.Effect != EffectAllow {
			t.Fatalf("owner %q = %q (matched %q), want allow — fresh tenant would be locked out", a, d.Effect, d.MatchedSid)
		}
	}

	// Manager: operational subset allows — now including employee:deactivate
	// (customer decision, 2026-07-26).
	for _, a := range []Action{
		ActionReportExport, ActionTapApprove, ActionRecordManual,
		ActionRecordReview, ActionEmployeeDeactivate,
	} {
		if d := authz(roleManager, a); d.Effect != EffectAllow || d.MatchedSid != SidAuthzManager {
			t.Fatalf("manager %q = %q/%q, want allow via %q", a, d.Effect, d.MatchedSid, SidAuthzManager)
		}
	}

	// Manager: policy:edit is the ONE action still closed — owner-only, denied by
	// the guardrail sys:policy-edit-owner-only for any non-owner.
	if d := authz(roleManager, ActionPolicyEdit); d.Effect != EffectDeny || d.MatchedSid != "sys:policy-edit-owner-only" {
		t.Fatalf("manager policy:edit = %q/%q, want guardrail deny", d.Effect, d.MatchedSid)
	}

	// No role at all -> fail-closed deny (the default), not accidental allow.
	if d := authz("", ActionReportExport); d.Effect != EffectDeny || d.MatchedSid != "default" {
		t.Fatalf("roleless report:export = %q/%q, want default deny", d.Effect, d.MatchedSid)
	}
}

// TestBaseline_OwnerCanEditPolicy makes the subtle owner-policy:edit path explicit:
// the guardrail sys:policy-edit-owner-only only DENIES non-owners; for the owner it
// does not fire, so the owner needs the baseline authz-owner allow to edit policy
// at all — without it the owner would hit the fail-closed default deny and could
// never configure the tenant.
func TestBaseline_OwnerCanEditPolicy(t *testing.T) {
	set := Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline()}
	d := Evaluate(set, Context{Action: ActionPolicyEdit, Resources: []string{"*"},
		Keys: map[ContextKey]any{CtxActorRole: roleOwner}})
	if d.Effect != EffectAllow || d.MatchedSid != SidAuthzOwner {
		t.Fatalf("owner policy:edit = %q/%q, want allow via %q (else owner is locked out of configuration)",
			d.Effect, d.MatchedSid, SidAuthzOwner)
	}
}

// --- (4) base: namespace is reserved from tenant documents -------------------

// TestBaseline_TenantCannotUseBaseSid proves a tenant document may not claim a
// base: sid (case-insensitively), while a normal tenant sid passes.
func TestBaseline_TenantCannotUseBaseSid(t *testing.T) {
	mk := func(sid string) Document {
		return Document{Version: "2026-07-26", Name: "t", Statements: []Statement{
			{Sid: sid, Effect: EffectReview, Action: []Action{ActionTapRecord}, Resource: []string{"*"}},
		}}
	}
	for _, sid := range []string{"base:ip-or-gps-ok", "BASE:sneaky", "Base:mixed"} {
		err := Validate(mk(sid), LayerTenant, DefaultLimits())
		if err == nil {
			t.Fatalf("tenant sid %q must be rejected (base: is reserved)", sid)
		}
		if !strings.Contains(err.Error(), "base:") {
			t.Fatalf("tenant sid %q: error should name the base: reservation, got %v", sid, err)
		}
	}
	// A normal tenant sid is fine.
	if err := Validate(mk("t:my-rule"), LayerTenant, DefaultLimits()); err != nil {
		t.Fatalf("ordinary tenant sid rejected: %v", err)
	}
}

// TestBaseline_BaselineMayUseBaseSid proves the reservation is layer-scoped: a
// BASELINE document may use base: (that is its purpose), and the SAME document
// would be rejected at the tenant layer.
func TestBaseline_BaselineMayUseBaseSid(t *testing.T) {
	doc := Document{Version: "2026-07-26", Name: "b", Statements: []Statement{
		{Sid: "base:custom", Effect: EffectReview, Action: []Action{ActionTapRecord}, Resource: []string{"*"}},
	}}
	if err := Validate(doc, LayerBaseline, DefaultLimits()); err != nil {
		t.Fatalf("baseline document with base: sid rejected: %v", err)
	}
	if err := Validate(doc, LayerTenant, DefaultLimits()); err == nil {
		t.Fatal("the same base: document must be rejected at the tenant layer")
	}
}

// --- (5) every baseline document is admissible and persistable ---------------

// TestBaseline_DocumentsValidateAndRoundTrip proves the shipped baseline is
// admissible at the baseline layer AND survives the real persistence path
// (JSON -> Parse -> Validate), which is how M7-03 will store and later reload it.
// It also proves the whole baseline would be rejected at the tenant layer (base:
// sids), confirming the reservation applies to the real documents.
func TestBaseline_DocumentsValidateAndRoundTrip(t *testing.T) {
	lim := DefaultLimits()
	for _, bd := range Baseline() {
		if err := Validate(bd.Document, LayerBaseline, lim); err != nil {
			t.Fatalf("%q does not validate at the baseline layer: %v", bd.Name, err)
		}
		if err := Validate(bd.Document, LayerTenant, lim); err == nil {
			t.Fatalf("%q must be rejected at the tenant layer (base: sids)", bd.Name)
		}
		raw, err := json.Marshal(bd.Document)
		if err != nil {
			t.Fatalf("%q: marshal: %v", bd.Name, err)
		}
		if _, err := ParseAndValidate(raw, LayerBaseline, lim); err != nil {
			t.Fatalf("%q: JSON round-trip (the M7-03 persist/reload path) failed: %v", bd.Name, err)
		}
		if len(bd.Attachments) == 0 {
			t.Fatalf("%q: has no attachments; M7-03 would bind it to nothing", bd.Name)
		}
	}
}

// TestBaseline_NoIgnoreOrRedirect proves NO baseline statement produces ignore or
// redirect (ADR 0004 §6, guardrail-only) — every effect is allow or review. This
// is stricter than validate.go (which also permits deny in a document); the
// baseline deliberately never denies a tap (that is guardrail territory, §5 rows
// 1-5).
func TestBaseline_NoIgnoreOrRedirect(t *testing.T) {
	for _, bd := range Baseline() {
		for _, st := range bd.Document.Statements {
			if st.Effect != EffectAllow && st.Effect != EffectReview {
				t.Fatalf("%q/%s: effect %q — baseline statements must be allow or review only",
					bd.Name, st.Sid, st.Effect)
			}
		}
	}
}

// TestBaseline_AllSidsPresentOnce proves every documented base: sid ships exactly
// once — a guard against a dropped or duplicated statement after an edit.
func TestBaseline_AllSidsPresentOnce(t *testing.T) {
	want := []string{
		SidIPOrGPSOK, SidGPSOnlyAllow, SidNoEvidenceReview, SidQRRequiresIP,
		SidCrossLocationNote, SidQueuedWindow, SidCtrGapReview, SidGPSConflictReview,
		SidAuthzOwner, SidAuthzManager,
	}
	seen := map[string]int{}
	for _, bd := range Baseline() {
		for _, st := range bd.Document.Statements {
			seen[st.Sid]++
		}
	}
	for _, sid := range want {
		if seen[sid] != 1 {
			t.Fatalf("sid %q appears %d times, want exactly 1", sid, seen[sid])
		}
	}
	total := 0
	for _, n := range seen {
		total += n
	}
	if total != len(want) {
		t.Fatalf("baseline carries %d statements, want %d (an unexpected statement crept in)", total, len(want))
	}
}

// TestBaseline_VersionStamp proves the baseline is versioned and internally
// consistent (every document carries BaselineVersion) — the anchor for the
// no-auto-update policy (existing tenants keep their materialised version until
// they accept a bump).
func TestBaseline_VersionStamp(t *testing.T) {
	if strings.TrimSpace(BaselineVersion) == "" {
		t.Fatal("BaselineVersion must be set")
	}
	for _, bd := range Baseline() {
		if bd.Document.Version != BaselineVersion {
			t.Fatalf("%q: document version %q != BaselineVersion %q", bd.Name, bd.Document.Version, BaselineVersion)
		}
	}
}

// --- (6) ctr-gap-review is resource-scoped and overridable -------------------

// TestBaseline_CtrGapResourceScopedOverride proves base:ctr-gap-review is
// resource-scoped: it stays on at a low-traffic branch (Rusty Bar) but a tenant
// can switch it off at a busy branch (St Julians) with a more-specific override —
// exactly the Q21 shape (URL-hoarding only pays off at quiet plaques).
func TestBaseline_CtrGapResourceScopedOverride(t *testing.T) {
	tenant := []Policy{pol(LayerTenant, Statement{
		Sid: "t:st-julians-ctrgap-ok", Effect: EffectAllow, Action: []Action{ActionTapRecord},
		Resource:  []string{"location/st-julians"}, // specExact beats the baseline's location/*
		Condition: Condition{OpNumericGreaterThan: {CtxTapCtrGap: float64(0)}},
		Reason:    "busy branch: counter gaps are normal here",
	})}
	set := Set{
		Guardrails: DefaultGuardrails(),
		Baseline:   baselineDocFor(t, SidCtrGapReview),
		Tenant:     tenant,
	}
	gap := map[ContextKey]any{CtxTapCtrGap: float64(1)}

	// Low-traffic branch: baseline review still governs.
	d := Evaluate(set, benignTap(gap, "location/rusty-bar"))
	if d.Effect != EffectReview || d.MatchedSid != SidCtrGapReview {
		t.Fatalf("rusty-bar: got %q/%q, want review via %q", d.Effect, d.MatchedSid, SidCtrGapReview)
	}
	// Busy branch: the specific tenant allow overrides the baseline review.
	d = Evaluate(set, benignTap(gap, "location/st-julians"))
	if d.Effect != EffectAllow || d.MatchedSid != "t:st-julians-ctrgap-ok" {
		t.Fatalf("st-julians: got %q/%q, want tenant override allow", d.Effect, d.MatchedSid)
	}
}

// TestBaseline_TenantOverridesGPSOnly proves the Q16 lever: a tenant turns
// GPS-only taps into review with a single equal-specificity statement, and it
// out-restricts the baseline allow (deny/review > allow). IP-proven taps are
// untouched because they are governed by a different statement (base:ip-or-gps-ok).
func TestBaseline_TenantOverridesGPSOnly(t *testing.T) {
	tenant := []Policy{pol(LayerTenant, Statement{
		Sid: "t:require-ip", Effect: EffectReview, Action: []Action{ActionTapRecord},
		Resource:  []string{"location/*"},
		Condition: Condition{OpBool: {CtxTapIPMatch: false, CtxTapGPSMatch: true}},
		Reason:    "this tenant requires IP proof of place",
	})}
	set := Set{
		Guardrails: DefaultGuardrails(),
		Baseline:   baselineDocFor(t, SidGPSOnlyAllow),
		Tenant:     tenant,
	}
	d := Evaluate(set, benignTap(map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: true}))
	if d.Effect != EffectReview || d.MatchedSid != "t:require-ip" {
		t.Fatalf("gps-only with tenant override: got %q/%q, want review via t:require-ip", d.Effect, d.MatchedSid)
	}
}

// --- (7) a tenant/baseline can never loosen a guardrail ----------------------

// TestBaseline_TenantOverrideDoesNotAffectGuardrail proves the layering guarantee:
// however broad a tenant allow (or the baseline) is, a guardrail that matches wins
// and is terminal. A retired tag is denied even under an allow-everything tenant
// policy plus the full baseline.
func TestBaseline_TenantOverrideDoesNotAffectGuardrail(t *testing.T) {
	allowAll := []Policy{pol(LayerTenant,
		tapStmt("t:allow-everything", EffectAllow, []string{"*"}, nil),
	)}
	set := Set{
		Guardrails: DefaultGuardrails(),
		Baseline:   fullBaseline(),
		Tenant:     allowAll,
	}
	// A retired tag trips sys:tag-not-active (guardrail #2), terminal deny.
	ctx := benignTap(map[ContextKey]any{CtxTagStatus: "retired", CtxTapIPMatch: true})
	d := Evaluate(set, ctx)
	if d.Effect != EffectDeny || d.MatchedSid != "sys:tag-not-active" {
		t.Fatalf("guardrail must win over baseline+tenant: got %q/%q, want deny via sys:tag-not-active",
			d.Effect, d.MatchedSid)
	}
	if d.Layer != LayerGuardrail {
		t.Fatalf("guardrail decision must carry LayerGuardrail, got %q", d.Layer)
	}
	// Removing the baseline changes nothing — the guardrail is unconditional.
	d = Evaluate(Set{Guardrails: DefaultGuardrails(), Tenant: allowAll}, ctx)
	if d.Effect != EffectDeny || d.MatchedSid != "sys:tag-not-active" {
		t.Fatalf("guardrail must hold without the baseline too: got %q/%q", d.Effect, d.MatchedSid)
	}
}

// TestBaseline_DisablingBaselineDoesNotWeakenGuardrail proves that disabling the
// baseline entirely (an empty Baseline, as if a tenant switched every managed
// policy off) leaves guardrails fully intact — a deactivated employee's tap is
// still denied with its security alert.
func TestBaseline_DisablingBaselineDoesNotWeakenGuardrail(t *testing.T) {
	set := Set{Guardrails: DefaultGuardrails()} // no baseline, no tenant
	d := Evaluate(set, benignTap(map[ContextKey]any{CtxEmployeeStatus: "deactivated", CtxTapIPMatch: true}))
	if d.Effect != EffectDeny || d.MatchedSid != "sys:employee-deactivated" {
		t.Fatalf("deactivated tap must be denied by guardrail regardless of baseline: got %q/%q", d.Effect, d.MatchedSid)
	}
	if d.SecurityAlert != AlertDeactivatedEmployeeTapped {
		t.Fatalf("expected the deactivated-employee alert, got %q", d.SecurityAlert)
	}
}

// TestBaselinePolicies_ShapeAndOrder proves the M7-03-facing builder returns one
// baseline Policy per document, in order, all layer=baseline with the supplied
// version ids — the exact []Policy the evaluator consumes.
func TestBaselinePolicies_ShapeAndOrder(t *testing.T) {
	docs := Baseline()
	pols := BaselinePolicies(stableVersionID)
	if len(pols) != len(docs) {
		t.Fatalf("BaselinePolicies returned %d policies, want %d", len(pols), len(docs))
	}
	for i, p := range pols {
		if p.Layer != LayerBaseline {
			t.Fatalf("policy %d layer = %q, want baseline", i, p.Layer)
		}
		if p.VersionID == uuid.Nil {
			t.Fatalf("policy %d has a nil version id", i)
		}
		if p.Document.Name != docs[i].Name {
			t.Fatalf("policy %d out of order: %q vs %q", i, p.Document.Name, docs[i].Name)
		}
	}
}
