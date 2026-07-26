package policy

import (
	"net/netip"
	"testing"
)

// discard is a no-op anomaly sink for tests that assert only match/no-match.
func discard(Anomaly) {}

// evalCond is a tiny wrapper so operator tests read as "does this condition hold
// for this context".
func evalCond(cond Condition, keys map[ContextKey]any) bool {
	return evalCondition("t", LayerTenant, cond, Context{Keys: keys}, discard)
}

// TestEvalOperator_EachOperator gives every one of the ten operators a POSITIVE
// (holds) and a NEGATIVE (does not hold) case with the key PRESENT — proving the
// comparison logic itself, separate from the missing-key rule tested elsewhere.
func TestEvalOperator_EachOperator(t *testing.T) {
	cases := []struct {
		name string
		cond Condition
		keys map[ContextKey]any
		want bool
	}{
		// StringEquals
		{"StringEquals hit", Condition{OpStringEquals: {CtxTapChannel: "qr"}}, map[ContextKey]any{CtxTapChannel: "qr"}, true},
		{"StringEquals miss", Condition{OpStringEquals: {CtxTapChannel: "qr"}}, map[ContextKey]any{CtxTapChannel: "nfc"}, false},
		// StringNotEquals
		{"StringNotEquals hit", Condition{OpStringNotEquals: {CtxTapChannel: "qr"}}, map[ContextKey]any{CtxTapChannel: "nfc"}, true},
		{"StringNotEquals miss", Condition{OpStringNotEquals: {CtxTapChannel: "qr"}}, map[ContextKey]any{CtxTapChannel: "qr"}, false},
		// StringIn
		{"StringIn hit", Condition{OpStringIn: {CtxActorRole: []any{"owner", "manager"}}}, map[ContextKey]any{CtxActorRole: "manager"}, true},
		{"StringIn miss", Condition{OpStringIn: {CtxActorRole: []any{"owner", "manager"}}}, map[ContextKey]any{CtxActorRole: "staff"}, false},
		// Bool
		{"Bool hit", Condition{OpBool: {CtxTapIPMatch: true}}, map[ContextKey]any{CtxTapIPMatch: true}, true},
		{"Bool miss", Condition{OpBool: {CtxTapIPMatch: true}}, map[ContextKey]any{CtxTapIPMatch: false}, false},
		// NumericEquals
		{"NumericEquals hit", Condition{OpNumericEquals: {CtxTapTrust: float64(100)}}, map[ContextKey]any{CtxTapTrust: float64(100)}, true},
		{"NumericEquals miss", Condition{OpNumericEquals: {CtxTapTrust: float64(100)}}, map[ContextKey]any{CtxTapTrust: float64(20)}, false},
		// NumericLessThan
		{"NumericLessThan hit", Condition{OpNumericLessThan: {CtxTapGPSDistanceM: float64(150)}}, map[ContextKey]any{CtxTapGPSDistanceM: float64(40)}, true},
		{"NumericLessThan miss (equal is not less)", Condition{OpNumericLessThan: {CtxTapGPSDistanceM: float64(150)}}, map[ContextKey]any{CtxTapGPSDistanceM: float64(150)}, false},
		// NumericGreaterThan
		{"NumericGreaterThan hit", Condition{OpNumericGreaterThan: {CtxTapCtrGap: float64(0)}}, map[ContextKey]any{CtxTapCtrGap: float64(3)}, true},
		{"NumericGreaterThan miss (equal is not greater)", Condition{OpNumericGreaterThan: {CtxTapCtrGap: float64(0)}}, map[ContextKey]any{CtxTapCtrGap: float64(0)}, false},
		// IpInPrefix
		{"IpInPrefix hit", Condition{OpIpInPrefix: {CtxLocationID: []any{"10.0.0.0/8", "192.168.1.0/24"}}}, map[ContextKey]any{CtxLocationID: "192.168.1.7"}, true},
		{"IpInPrefix miss", Condition{OpIpInPrefix: {CtxLocationID: []any{"10.0.0.0/8"}}}, map[ContextKey]any{CtxLocationID: "192.168.1.7"}, false},
		// Exists (present)
		{"Exists true, key present", Condition{OpExists: {CtxTapGPSDistanceM: true}}, map[ContextKey]any{CtxTapGPSDistanceM: float64(1)}, true},
		{"Exists false, key present", Condition{OpExists: {CtxTapGPSDistanceM: false}}, map[ContextKey]any{CtxTapGPSDistanceM: float64(1)}, false},
		// TimeBetween (normal window)
		{"TimeBetween hit normal", Condition{OpTimeBetween: {CtxTimeLocalHour: []any{float64(9), float64(17)}}}, map[ContextKey]any{CtxTimeLocalHour: float64(12)}, true},
		{"TimeBetween miss normal", Condition{OpTimeBetween: {CtxTimeLocalHour: []any{float64(9), float64(17)}}}, map[ContextKey]any{CtxTimeLocalHour: float64(20)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalCond(tc.cond, tc.keys); got != tc.want {
				t.Fatalf("evalCond = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEvalOperator_TimeBetweenWraparound proves the night-shift wraparound
// (CLAUDE.md §5: a 02:00 checkout on an 18:00-02:00 shift must match).
func TestEvalOperator_TimeBetweenWraparound(t *testing.T) {
	cond := Condition{OpTimeBetween: {CtxTimeLocalHour: []any{float64(18), float64(2)}}}
	inWindow := []float64{18, 20, 23, 0, 1, 2}
	for _, h := range inWindow {
		if !evalCond(cond, map[ContextKey]any{CtxTimeLocalHour: h}) {
			t.Errorf("hour %g should be inside the 18->2 window", h)
		}
	}
	outWindow := []float64{3, 10, 17}
	for _, h := range outWindow {
		if evalCond(cond, map[ContextKey]any{CtxTimeLocalHour: h}) {
			t.Errorf("hour %g should be outside the 18->2 window", h)
		}
	}
}

// TestEvalOperator_MissingKeyIsNotFalse is the critical distinction (M3-04 card):
// with the key ABSENT, every operator but Exists does NOT match — it is not
// treated as false. StringNotEquals is the sharpest case: a naive `got != want`
// would wrongly MATCH on an absent key.
func TestEvalOperator_MissingKeyIsNotFalse(t *testing.T) {
	empty := map[ContextKey]any{}
	cases := []struct {
		name string
		cond Condition
	}{
		{"StringEquals", Condition{OpStringEquals: {CtxTapChannel: "qr"}}},
		{"StringNotEquals (must NOT match absent)", Condition{OpStringNotEquals: {CtxTapChannel: "qr"}}},
		{"StringIn", Condition{OpStringIn: {CtxActorRole: []any{"owner"}}}},
		{"Bool", Condition{OpBool: {CtxTapIPMatch: false}}},
		{"NumericEquals", Condition{OpNumericEquals: {CtxTapTrust: float64(0)}}},
		{"NumericLessThan", Condition{OpNumericLessThan: {CtxTapGPSDistanceM: float64(150)}}},
		{"NumericGreaterThan", Condition{OpNumericGreaterThan: {CtxTapCtrGap: float64(0)}}},
		{"IpInPrefix", Condition{OpIpInPrefix: {CtxLocationID: []any{"0.0.0.0/0"}}}},
		{"TimeBetween", Condition{OpTimeBetween: {CtxTimeLocalHour: []any{float64(0), float64(23)}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if evalCond(tc.cond, empty) {
				t.Fatalf("%s matched on an ABSENT key; missing key must not be false", tc.name)
			}
		})
	}
}

// TestEvalOperator_ExistsHandlesAbsence proves Exists is the sole operator
// defined on absence: Exists:false matches when the key is gone, Exists:true does
// not.
func TestEvalOperator_ExistsHandlesAbsence(t *testing.T) {
	empty := map[ContextKey]any{}
	if !evalCond(Condition{OpExists: {CtxTapGPSDistanceM: false}}, empty) {
		t.Error("Exists:false should match an ABSENT key")
	}
	if evalCond(Condition{OpExists: {CtxTapGPSDistanceM: true}}, empty) {
		t.Error("Exists:true should NOT match an ABSENT key")
	}
}

// TestEvalOperator_BadContextValueIsAnomaly proves a wrong-typed CONTEXT value
// (a server bug) is a fail-safe non-match AND is reported as a bad-value anomaly.
func TestEvalOperator_BadContextValueIsAnomaly(t *testing.T) {
	cases := []struct {
		name string
		cond Condition
		keys map[ContextKey]any
	}{
		{"Bool wants bool, got string", Condition{OpBool: {CtxTapIPMatch: true}}, map[ContextKey]any{CtxTapIPMatch: "true"}},
		{"StringEquals wants string, got number", Condition{OpStringEquals: {CtxTapChannel: "qr"}}, map[ContextKey]any{CtxTapChannel: float64(1)}},
		{"Numeric wants number, got string", Condition{OpNumericLessThan: {CtxTapGPSDistanceM: float64(1)}}, map[ContextKey]any{CtxTapGPSDistanceM: "40"}},
		{"IpInPrefix wants IP, got number", Condition{OpIpInPrefix: {CtxLocationID: []any{"10.0.0.0/8"}}}, map[ContextKey]any{CtxLocationID: float64(10)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec []Anomaly
			ok := evalCondition("t", LayerTenant, tc.cond, Context{Keys: tc.keys}, func(a Anomaly) { rec = append(rec, a) })
			if ok {
				t.Fatal("condition with a wrong-typed context value matched")
			}
			if len(rec) != 1 || rec[0].Kind != AnomalyBadContextValue {
				t.Fatalf("want exactly one bad-value anomaly, got %+v", rec)
			}
		})
	}
}

// TestEvalOperator_NumericAcceptsIntWidths proves the defensive toFloat: server
// code may hand an int/int32/int64 rather than a JSON float64.
func TestEvalOperator_NumericAcceptsIntWidths(t *testing.T) {
	cond := Condition{OpNumericLessThan: {CtxTapGPSDistanceM: float64(150)}}
	for _, v := range []any{int(40), int32(40), int64(40), float32(40)} {
		if !evalCond(cond, map[ContextKey]any{CtxTapGPSDistanceM: v}) {
			t.Errorf("numeric context value of type %T not accepted", v)
		}
	}
}

// TestEvalOperator_IpInPrefixAcceptsAddrType proves toAddr accepts a netip.Addr
// context value directly (not only a string).
func TestEvalOperator_IpInPrefixAcceptsAddrType(t *testing.T) {
	cond := Condition{OpIpInPrefix: {CtxLocationID: []any{"10.0.0.0/8"}}}
	if !evalCond(cond, map[ContextKey]any{CtxLocationID: netip.MustParseAddr("10.1.2.3")}) {
		t.Error("netip.Addr context value not matched against its prefix")
	}
}

// TestEvalCondition_AndSemantics proves a multi-predicate condition holds only if
// ALL predicates hold, and that the boolean result is independent of predicate
// order (the same map, evaluated many times, is stable).
func TestEvalCondition_AndSemantics(t *testing.T) {
	cond := Condition{
		OpBool:         {CtxTapIPMatch: false, CtxTapGPSMatch: true},
		OpStringEquals: {CtxTapChannel: "nfc"},
	}
	allTrue := map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: true, CtxTapChannel: "nfc"}
	if !evalCond(cond, allTrue) {
		t.Fatal("all predicates satisfied but condition did not hold")
	}
	oneFalse := map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: true, CtxTapChannel: "qr"}
	if evalCond(cond, oneFalse) {
		t.Fatal("one predicate failed but condition held (AND broken)")
	}
	// Stability across many runs (map iteration is random).
	for i := 0; i < 100; i++ {
		if !evalCond(cond, allTrue) {
			t.Fatal("non-deterministic AND result")
		}
	}
}

// TestEvalCondition_Empty proves a nil/empty condition holds unconditionally.
func TestEvalCondition_Empty(t *testing.T) {
	if !evalCond(nil, nil) {
		t.Error("nil condition should hold unconditionally")
	}
	if !evalCond(Condition{}, map[ContextKey]any{}) {
		t.Error("empty condition should hold unconditionally")
	}
}

// TestEvalOperator_BadDocValueShapeIsAnomaly proves defence-in-depth: even though
// validate.go guarantees doc-value shapes, a wrong-shaped DOCUMENT value here is a
// fail-safe non-match reported as a bad-value anomaly, never a panic. (The key is
// present with a well-typed context value, so only the doc value is at fault.)
func TestEvalOperator_BadDocValueShapeIsAnomaly(t *testing.T) {
	cases := []struct {
		name string
		cond Condition
		keys map[ContextKey]any
	}{
		{"StringEquals doc not string", Condition{OpStringEquals: {CtxTapChannel: float64(1)}}, map[ContextKey]any{CtxTapChannel: "qr"}},
		{"StringIn doc not array", Condition{OpStringIn: {CtxActorRole: "owner"}}, map[ContextKey]any{CtxActorRole: "owner"}},
		{"Bool doc not bool", Condition{OpBool: {CtxTapIPMatch: "true"}}, map[ContextKey]any{CtxTapIPMatch: true}},
		{"Numeric doc not number", Condition{OpNumericLessThan: {CtxTapGPSDistanceM: "150"}}, map[ContextKey]any{CtxTapGPSDistanceM: float64(1)}},
		{"IpInPrefix doc not array", Condition{OpIpInPrefix: {CtxLocationID: "10.0.0.0/8"}}, map[ContextKey]any{CtxLocationID: "10.1.2.3"}},
		{"TimeBetween doc wrong length", Condition{OpTimeBetween: {CtxTimeLocalHour: []any{float64(9)}}}, map[ContextKey]any{CtxTimeLocalHour: float64(12)}},
		{"TimeBetween doc non-number", Condition{OpTimeBetween: {CtxTimeLocalHour: []any{"9", "17"}}}, map[ContextKey]any{CtxTimeLocalHour: float64(12)}},
		{"Exists doc not bool", Condition{OpExists: {CtxTapGPSDistanceM: "yes"}}, map[ContextKey]any{CtxTapGPSDistanceM: float64(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec []Anomaly
			ok := evalCondition("t", LayerTenant, tc.cond, Context{Keys: tc.keys}, func(a Anomaly) { rec = append(rec, a) })
			if ok {
				t.Fatal("condition with a wrong-shaped doc value matched")
			}
			if len(rec) != 1 || rec[0].Kind != AnomalyBadContextValue {
				t.Fatalf("want exactly one bad-value anomaly, got %+v", rec)
			}
		})
	}
}

// TestEvalOperator_StringInSkipsNonStringElements proves a non-string element in
// the doc array is skipped, not a panic; the match is decided by the string ones.
func TestEvalOperator_StringInSkipsNonStringElements(t *testing.T) {
	cond := Condition{OpStringIn: {CtxActorRole: []any{float64(1), "manager"}}}
	if !evalCond(cond, map[ContextKey]any{CtxActorRole: "manager"}) {
		t.Error("string element after a non-string element should still match")
	}
	if evalCond(cond, map[ContextKey]any{CtxActorRole: "staff"}) {
		t.Error("no string element matches -> should not hold")
	}
}

// TestToFloat covers every accepted numeric width and the reject path.
func TestToFloat(t *testing.T) {
	oks := []any{float64(1), float32(1), int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1)}
	for _, v := range oks {
		if f, ok := toFloat(v); !ok || f != 1 {
			t.Errorf("toFloat(%T) = (%v,%v), want (1,true)", v, f, ok)
		}
	}
	if _, ok := toFloat("1"); ok {
		t.Error("toFloat(string) should not be ok")
	}
}

// TestToAddr covers the netip.Addr path, the parseable-string path, the
// unparseable-string reject and the wrong-type reject.
func TestToAddr(t *testing.T) {
	if a, ok := toAddr(netip.MustParseAddr("10.0.0.1")); !ok || a.String() != "10.0.0.1" {
		t.Errorf("toAddr(Addr) = (%v,%v)", a, ok)
	}
	if a, ok := toAddr("10.0.0.2"); !ok || a.String() != "10.0.0.2" {
		t.Errorf("toAddr(string) = (%v,%v)", a, ok)
	}
	if _, ok := toAddr("not-an-ip"); ok {
		t.Error("toAddr(unparseable) should not be ok")
	}
	if _, ok := toAddr(float64(10)); ok {
		t.Error("toAddr(number) should not be ok")
	}
}

// TestRestrictiveness pins the effect ordering deny > ignore > review > allow, and
// that the guardrail-only redirect ranks below allow (it never enters the
// baseline/tenant comparison, but the ranking is total and defensive).
func TestRestrictiveness(t *testing.T) {
	if !(restrictiveness(EffectDeny) > restrictiveness(EffectIgnore) &&
		restrictiveness(EffectIgnore) > restrictiveness(EffectReview) &&
		restrictiveness(EffectReview) > restrictiveness(EffectAllow) &&
		restrictiveness(EffectAllow) > restrictiveness(EffectRedirect)) {
		t.Fatalf("restrictiveness order broken: deny=%d ignore=%d review=%d allow=%d redirect=%d",
			restrictiveness(EffectDeny), restrictiveness(EffectIgnore), restrictiveness(EffectReview),
			restrictiveness(EffectAllow), restrictiveness(EffectRedirect))
	}
}

// TestEvalCondition_UnknownOperatorAndKey proves an unknown operator or key at
// eval time makes the condition NOT hold and is reported (deterministically, in
// sorted order).
func TestEvalCondition_UnknownOperatorAndKey(t *testing.T) {
	t.Run("unknown operator", func(t *testing.T) {
		var rec []Anomaly
		ok := evalCondition("t", LayerTenant, Condition{"StringRegex": {CtxTapChannel: "qr"}},
			Context{Keys: map[ContextKey]any{CtxTapChannel: "qr"}}, func(a Anomaly) { rec = append(rec, a) })
		if ok {
			t.Fatal("condition with an unknown operator held")
		}
		if len(rec) != 1 || rec[0].Kind != AnomalyUnknownOperator {
			t.Fatalf("want one unknown-operator anomaly, got %+v", rec)
		}
	})
	t.Run("unknown key", func(t *testing.T) {
		var rec []Anomaly
		ok := evalCondition("t", LayerBaseline, Condition{OpBool: {"tap:mood": true}},
			Context{Keys: map[ContextKey]any{}}, func(a Anomaly) { rec = append(rec, a) })
		if ok {
			t.Fatal("condition with an unknown key held")
		}
		if len(rec) != 1 || rec[0].Kind != AnomalyUnknownKey || rec[0].Layer != LayerBaseline {
			t.Fatalf("want one unknown-key anomaly at baseline layer, got %+v", rec)
		}
	})
}
