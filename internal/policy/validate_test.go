package policy

import (
	"errors"
	"strings"
	"testing"
)

// --- builders ---------------------------------------------------------------

// validStatement returns a minimal admissible statement. Tests copy and mutate
// it so each case differs from a KNOWN-GOOD baseline by exactly one field — the
// mutation discipline the third-eye asks for (a rejection means nothing unless
// the un-mutated version is accepted).
func validStatement() Statement {
	return Statement{
		Sid:      "ok-1",
		Effect:   EffectReview,
		Action:   []Action{ActionTapRecord},
		Resource: []string{"location/*"},
		Condition: Condition{
			OpBool: {CtxTapIPMatch: false, CtxTapGPSMatch: true},
		},
		Reason: "example",
	}
}

func validDoc() Document {
	return Document{
		Version:    "2026-07-24",
		Name:       "example policy",
		Statements: []Statement{validStatement()},
	}
}

// mustPass asserts the un-mutated baseline is accepted (the positive control).
func mustPass(t *testing.T, doc Document) {
	t.Helper()
	if err := Validate(doc, LayerTenant, DefaultLimits()); err != nil {
		t.Fatalf("baseline document should be valid, got: %v", err)
	}
}

// wantField asserts err is a *ValidationError on the given field.
func wantField(t *testing.T, err error, field string) *ValidationError {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %T: %v", err, err)
	}
	if field != "" && ve.Field != field {
		t.Fatalf("want field %q, got %q (msg: %s)", field, ve.Field, ve.Msg)
	}
	return ve
}

// --- positive control -------------------------------------------------------

func TestValidate_Valid(t *testing.T) {
	mustPass(t, validDoc())
	// Also valid at the baseline layer.
	if err := Validate(validDoc(), LayerBaseline, DefaultLimits()); err != nil {
		t.Fatalf("valid baseline-layer document rejected: %v", err)
	}
}

// TestValidate_MultiOperatorCondition covers a valid statement with several
// operators and keys (exercises the sorted, deterministic condition walk).
func TestValidate_MultiOperatorCondition(t *testing.T) {
	doc := validDoc()
	doc.Statements[0].Condition = Condition{
		OpBool:            {CtxTapIPMatch: false, CtxTapGPSMatch: true},
		OpStringEquals:    {CtxTapChannel: "qr"},
		OpNumericLessThan: {CtxTapGPSDistanceM: float64(150)},
	}
	if err := Validate(doc, LayerTenant, DefaultLimits()); err != nil {
		t.Fatalf("valid multi-operator condition rejected: %v", err)
	}
}

// TestValidate_Deterministic proves the FIRST error is stable across runs
// despite Go's random map iteration: two unknown context keys under different
// operators must always surface the same one.
func TestValidate_Deterministic(t *testing.T) {
	build := func() Document {
		doc := validDoc()
		doc.Statements[0].Condition = Condition{
			OpBool:         {"zzz:unknown": true},
			OpStringEquals: {"aaa:unknown": "x"},
		}
		return doc
	}
	first := Validate(build(), LayerTenant, DefaultLimits()).Error()
	for i := 0; i < 50; i++ {
		if got := Validate(build(), LayerTenant, DefaultLimits()).Error(); got != first {
			t.Fatalf("non-deterministic error: %q vs %q", first, got)
		}
	}
}

// TestValidate_AllDocumentEffects proves the three document-legal effects pass.
func TestValidate_AllDocumentEffects(t *testing.T) {
	for _, e := range []Effect{EffectAllow, EffectReview, EffectDeny} {
		doc := validDoc()
		doc.Statements[0].Effect = e
		if err := Validate(doc, LayerTenant, DefaultLimits()); err != nil {
			t.Errorf("effect %q should be valid, got: %v", e, err)
		}
	}
}

// --- closed vocabularies: unknown → ERROR (never silent skip) ---------------

func TestValidate_UnknownEffect(t *testing.T) {
	mustPass(t, validDoc())
	doc := validDoc()
	doc.Statements[0].Effect = "alllow" // typo
	ve := wantField(t, Validate(doc, LayerTenant, DefaultLimits()), fieldEffect)
	if !strings.Contains(ve.Msg, "unknown effect") {
		t.Fatalf("msg = %q, want 'unknown effect'", ve.Msg)
	}
}

func TestValidate_UnknownAction(t *testing.T) {
	mustPass(t, validDoc())
	doc := validDoc()
	doc.Statements[0].Action = []Action{"tap:teleport"}
	wantField(t, Validate(doc, LayerTenant, DefaultLimits()), fieldAction)
}

func TestValidate_UnknownOperator(t *testing.T) {
	mustPass(t, validDoc())
	doc := validDoc()
	doc.Statements[0].Condition = Condition{"StringRegex": {CtxTapChannel: "qr"}}
	wantField(t, Validate(doc, LayerTenant, DefaultLimits()), fieldOperator)
}

func TestValidate_UnknownContextKey(t *testing.T) {
	mustPass(t, validDoc())
	doc := validDoc()
	doc.Statements[0].Condition = Condition{OpStringEquals: {"tap:mood": "happy"}}
	wantField(t, Validate(doc, LayerTenant, DefaultLimits()), fieldContextKey)
}

// TestValidate_AllOperatorsValidShapes proves every known operator with a
// correct value shape is accepted (operator-name coverage).
func TestValidate_AllOperatorsValidShapes(t *testing.T) {
	cases := map[Operator]map[ContextKey]any{
		OpStringEquals:       {CtxTapChannel: "qr"},
		OpStringNotEquals:    {CtxTapChannel: "nfc"},
		OpStringIn:           {CtxActorRole: []any{"owner", "manager"}},
		OpBool:               {CtxTapIPMatch: true},
		OpNumericEquals:      {CtxTapTrust: float64(100)},
		OpNumericLessThan:    {CtxTapGPSDistanceM: float64(150)},
		OpNumericGreaterThan: {CtxTapCtrGap: float64(0)},
		OpIpInPrefix:         {CtxLocationID: []any{"10.0.0.0/8", "192.168.1.0/24"}},
		OpExists:             {CtxTapGPSDistanceM: true},
		OpTimeBetween:        {CtxTimeLocalHour: []any{float64(18), float64(2)}},
	}
	for op, args := range cases {
		doc := validDoc()
		doc.Statements[0].Condition = Condition{op: args}
		if err := Validate(doc, LayerTenant, DefaultLimits()); err != nil {
			t.Errorf("operator %q with valid shape rejected: %v", op, err)
		}
	}
}

// TestValidate_OperatorValueShapes proves each operator rejects a wrong-shaped
// value (so the evaluator can trust shapes without re-checking).
func TestValidate_OperatorValueShapes(t *testing.T) {
	cases := []struct {
		name string
		cond Condition
	}{
		{"StringEquals wants string", Condition{OpStringEquals: {CtxTapChannel: float64(1)}}},
		{"StringIn wants array", Condition{OpStringIn: {CtxActorRole: "owner"}}},
		{"StringIn wants string elements", Condition{OpStringIn: {CtxActorRole: []any{float64(1)}}}},
		{"StringIn rejects empty array", Condition{OpStringIn: {CtxActorRole: []any{}}}},
		{"Bool wants bool", Condition{OpBool: {CtxTapIPMatch: "true"}}},
		{"Numeric wants number", Condition{OpNumericLessThan: {CtxTapGPSDistanceM: "150"}}},
		{"IpInPrefix wants array", Condition{OpIpInPrefix: {CtxLocationID: "10.0.0.0/8"}}},
		{"IpInPrefix rejects bad CIDR", Condition{OpIpInPrefix: {CtxLocationID: []any{"not-a-cidr"}}}},
		{"IpInPrefix rejects empty array", Condition{OpIpInPrefix: {CtxLocationID: []any{}}}},
		{"Exists wants bool", Condition{OpExists: {CtxTapGPSDistanceM: "yes"}}},
		{"TimeBetween wants two elements", Condition{OpTimeBetween: {CtxTimeLocalHour: []any{float64(18)}}}},
		{"TimeBetween wants numbers", Condition{OpTimeBetween: {CtxTimeLocalHour: []any{"18", "02"}}}},
		{"operator with no keys", Condition{OpBool: {}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validDoc()
			doc.Statements[0].Condition = tc.cond
			if err := Validate(doc, LayerTenant, DefaultLimits()); err == nil {
				t.Fatalf("condition %+v was accepted", tc.cond)
			}
		})
	}
}

// --- sys: namespace reserved ------------------------------------------------

func TestValidate_SysNamespaceReserved(t *testing.T) {
	for _, sid := range []string{"sys:tag-not-active", "SYS:sneaky", "Sys:mixed"} {
		doc := validDoc()
		doc.Statements[0].Sid = sid
		ve := wantField(t, Validate(doc, LayerTenant, DefaultLimits()), fieldSid)
		if !strings.Contains(ve.Msg, "sys:") {
			t.Errorf("sid %q: msg = %q, want mention of sys:", sid, ve.Msg)
		}
		// Same rejection at the baseline layer — sys: is guardrail-only.
		if err := Validate(doc, LayerBaseline, DefaultLimits()); err == nil {
			t.Errorf("sid %q accepted at baseline layer", sid)
		}
	}
	// A sid that merely starts with "sys" but not "sys:" is fine.
	doc := validDoc()
	doc.Statements[0].Sid = "system-health-note"
	if err := Validate(doc, LayerTenant, DefaultLimits()); err != nil {
		t.Fatalf("non-namespace sid 'system-health-note' rejected: %v", err)
	}
}

// TestValidate_SysInActionAndKey proves sys: cannot sneak in via an action or a
// context key either: both are closed lists with no sys: members, so a sys:
// token there is rejected as unknown (brief: "sid ve — varsa — action/anahtar").
func TestValidate_SysInActionAndKey(t *testing.T) {
	doc := validDoc()
	doc.Statements[0].Action = []Action{"sys:shutdown"}
	wantField(t, Validate(doc, LayerTenant, DefaultLimits()), fieldAction)

	doc = validDoc()
	doc.Statements[0].Condition = Condition{OpBool: {"sys:override": true}}
	wantField(t, Validate(doc, LayerTenant, DefaultLimits()), fieldContextKey)
}

// --- ignore / redirect rejected in documents --------------------------------

func TestValidate_GuardrailOnlyEffects(t *testing.T) {
	for _, e := range []Effect{EffectIgnore, EffectRedirect} {
		for _, layer := range []Layer{LayerBaseline, LayerTenant} {
			doc := validDoc()
			doc.Statements[0].Effect = e
			ve := wantField(t, Validate(doc, layer, DefaultLimits()), fieldEffect)
			if !strings.Contains(ve.Msg, "guardrail") {
				t.Errorf("effect %q at %s: msg = %q, want mention of guardrail", e, layer, ve.Msg)
			}
		}
	}
}

// --- bounded parameters (ADR 0004 §11) --------------------------------------

// TestValidate_BoundedParam proves the enforcement MECHANISM: with an injected
// range, an in-range value passes and an out-of-range value is rejected. (The
// concrete production key→range wiring is deferred to M3-05 — see the
// Limits.BoundedParams comment.)
func TestValidate_BoundedParam(t *testing.T) {
	lim := DefaultLimits()
	lim.BoundedParams = map[ContextKey]NumericRange{
		CtxTapGPSDistanceM: {Min: 25, Max: 1000},
	}

	build := func(v float64) Document {
		doc := validDoc()
		doc.Statements[0].Condition = Condition{OpNumericLessThan: {CtxTapGPSDistanceM: v}}
		return doc
	}

	// Exactly at both bounds: accepted.
	for _, v := range []float64{25, 1000, 500} {
		if err := Validate(build(v), LayerTenant, lim); err != nil {
			t.Errorf("in-range value %g rejected: %v", v, err)
		}
	}
	// Just outside: rejected.
	for _, v := range []float64{24, 1001} {
		if err := Validate(build(v), LayerTenant, lim); err == nil {
			t.Errorf("out-of-range value %g accepted", v)
		}
	}
	// A numeric key NOT in BoundedParams is unconstrained.
	doc := validDoc()
	doc.Statements[0].Condition = Condition{OpNumericLessThan: {CtxTapTrust: float64(999999)}}
	if err := Validate(doc, LayerTenant, lim); err != nil {
		t.Errorf("unbounded key rejected: %v", err)
	}
}

// --- quantitative limits: at limit passes, +1 fails -------------------------

func TestValidate_LimitStatementsPerDocument(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxStatementsPerDocument = 2

	doc := validDoc()
	doc.Statements = []Statement{stWithSid("a"), stWithSid("b")}
	if err := Validate(doc, LayerTenant, lim); err != nil {
		t.Fatalf("document at statement limit rejected: %v", err)
	}
	doc.Statements = append(doc.Statements, stWithSid("c"))
	wantField(t, Validate(doc, LayerTenant, lim), fieldStatements)
}

func TestValidate_LimitActionsPerStatement(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxActionsPerStatement = 2

	doc := validDoc()
	doc.Statements[0].Action = []Action{ActionTapRecord, ActionTapApprove}
	if err := Validate(doc, LayerTenant, lim); err != nil {
		t.Fatalf("statement at action limit rejected: %v", err)
	}
	doc.Statements[0].Action = append(doc.Statements[0].Action, ActionReportExport)
	wantField(t, Validate(doc, LayerTenant, lim), fieldAction)
}

func TestValidate_LimitResourcesPerStatement(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxResourcesPerStatement = 2

	doc := validDoc()
	doc.Statements[0].Resource = []string{"location/a", "location/b"}
	if err := Validate(doc, LayerTenant, lim); err != nil {
		t.Fatalf("statement at resource limit rejected: %v", err)
	}
	doc.Statements[0].Resource = append(doc.Statements[0].Resource, "location/c")
	wantField(t, Validate(doc, LayerTenant, lim), fieldResource)
}

func TestValidate_LimitConditionsPerStatement(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxConditionsPerStatement = 2

	doc := validDoc()
	doc.Statements[0].Condition = Condition{OpBool: {CtxTapIPMatch: true, CtxTapGPSMatch: false}}
	if err := Validate(doc, LayerTenant, lim); err != nil {
		t.Fatalf("statement at condition limit rejected: %v", err)
	}
	// Three predicates across two operators → over the limit.
	doc.Statements[0].Condition = Condition{
		OpBool:         {CtxTapIPMatch: true, CtxTapGPSMatch: false},
		OpStringEquals: {CtxTapChannel: "qr"},
	}
	wantField(t, Validate(doc, LayerTenant, lim), fieldCondition)
}

func TestValidate_LimitIPPrefixesPerCondition(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxIPPrefixesPerCondition = 2

	doc := validDoc()
	doc.Statements[0].Condition = Condition{OpIpInPrefix: {CtxLocationID: []any{"10.0.0.0/8", "10.1.0.0/16"}}}
	if err := Validate(doc, LayerTenant, lim); err != nil {
		t.Fatalf("condition at IP-prefix limit rejected: %v", err)
	}
	doc.Statements[0].Condition = Condition{OpIpInPrefix: {CtxLocationID: []any{"10.0.0.0/8", "10.1.0.0/16", "10.2.0.0/16"}}}
	wantField(t, Validate(doc, LayerTenant, lim), fieldCondition)
}

func TestCheckTenantQuota(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxDocumentsPerTenant = 3
	lim.MaxVersionsPerPolicy = 5

	// At the document quota boundary: 2 existing docs → adding one is fine; 3 → full.
	if err := CheckTenantQuota(2, 0, lim); err != nil {
		t.Fatalf("adding a 3rd document (limit 3) rejected: %v", err)
	}
	if err := CheckTenantQuota(3, 0, lim); err == nil {
		t.Fatal("adding a 4th document (limit 3) accepted")
	} else {
		wantField(t, err, fieldQuota)
	}

	// Version quota.
	if err := CheckTenantQuota(0, 4, lim); err != nil {
		t.Fatalf("adding a 5th version (limit 5) rejected: %v", err)
	}
	if err := CheckTenantQuota(0, 5, lim); err == nil {
		t.Fatal("adding a 6th version (limit 5) accepted")
	}
}

// --- layer & required fields ------------------------------------------------

func TestValidate_Layer(t *testing.T) {
	if err := Validate(validDoc(), LayerGuardrail, DefaultLimits()); err == nil {
		t.Fatal("guardrail-layer document accepted; guardrails are code, not documents")
	} else {
		wantField(t, err, fieldLayer)
	}
	if err := Validate(validDoc(), Layer("nonsense"), DefaultLimits()); err == nil {
		t.Fatal("unknown layer accepted")
	}
}

func TestValidate_RequiredFields(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*Document)
		field string
	}{
		{"missing version", func(d *Document) { d.Version = "  " }, fieldVersion},
		{"missing name", func(d *Document) { d.Name = "" }, fieldName},
		{"no statements", func(d *Document) { d.Statements = nil }, fieldStatements},
		{"missing sid", func(d *Document) { d.Statements[0].Sid = "" }, fieldSid},
		{"no actions", func(d *Document) { d.Statements[0].Action = nil }, fieldAction},
		{"no resources", func(d *Document) { d.Statements[0].Resource = nil }, fieldResource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validDoc()
			tc.mut(&doc)
			wantField(t, Validate(doc, LayerTenant, DefaultLimits()), tc.field)
		})
	}
}

func TestValidate_DuplicateSid(t *testing.T) {
	doc := validDoc()
	doc.Statements = []Statement{stWithSid("dup"), stWithSid("dup")}
	ve := wantField(t, Validate(doc, LayerTenant, DefaultLimits()), fieldSid)
	if !strings.Contains(ve.Msg, "duplicate") {
		t.Fatalf("msg = %q, want 'duplicate'", ve.Msg)
	}
}

// --- resource forms ---------------------------------------------------------

func TestValidate_ResourceForms(t *testing.T) {
	valid := []string{"*", "location/*", "location/rusty-bar", "department/kitchen", "employee/abc-123"}
	for _, r := range valid {
		doc := validDoc()
		doc.Statements[0].Resource = []string{r}
		if err := Validate(doc, LayerTenant, DefaultLimits()); err != nil {
			t.Errorf("resource %q rejected: %v", r, err)
		}
	}
	invalid := []string{"", "location", "region/eu", "location/", "location/a/b", "sys:foo", "/x"}
	for _, r := range invalid {
		doc := validDoc()
		doc.Statements[0].Resource = []string{r}
		if err := Validate(doc, LayerTenant, DefaultLimits()); err == nil {
			t.Errorf("resource %q accepted", r)
		}
	}
}

// --- §4.7: errors never echo comparison VALUES ------------------------------

func TestValidationError_NoValueLeak(t *testing.T) {
	lim := DefaultLimits()
	lim.BoundedParams = map[ContextKey]NumericRange{CtxTapGPSDistanceM: {Min: 25, Max: 1000}}

	// A bounded-range violation must not echo the offending number.
	doc := validDoc()
	doc.Statements[0].Condition = Condition{OpNumericLessThan: {CtxTapGPSDistanceM: float64(987654)}}
	err := Validate(doc, LayerTenant, lim)
	if err == nil {
		t.Fatal("expected out-of-range rejection")
	}
	if strings.Contains(err.Error(), "987654") {
		t.Fatalf("error leaked the offending value: %s", err.Error())
	}

	// A bad IP prefix must not echo the prefix string.
	doc = validDoc()
	doc.Statements[0].Condition = Condition{OpIpInPrefix: {CtxLocationID: []any{"SENSITIVE-ADDR/99"}}}
	err = Validate(doc, LayerTenant, DefaultLimits())
	if err == nil {
		t.Fatal("expected bad-CIDR rejection")
	}
	if strings.Contains(err.Error(), "SENSITIVE-ADDR") {
		t.Fatalf("error leaked the offending prefix: %s", err.Error())
	}
}

// --- fixed structural length caps -------------------------------------------

func TestValidate_LengthCaps(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(*Document)
		field string
	}{
		{"version too long", func(d *Document) { d.Version = strings.Repeat("v", maxVersionLen+1) }, fieldVersion},
		{"name too long", func(d *Document) { d.Name = strings.Repeat("n", maxNameLen+1) }, fieldName},
		{"sid too long", func(d *Document) { d.Statements[0].Sid = strings.Repeat("s", maxSidLen+1) }, fieldSid},
		{"reason too long", func(d *Document) { d.Statements[0].Reason = strings.Repeat("r", maxReasonLen+1) }, "reason"},
		{"resource too long", func(d *Document) {
			d.Statements[0].Resource = []string{"location/" + strings.Repeat("x", maxResourceLen)}
		}, fieldResource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validDoc()
			tc.mut(&doc)
			wantField(t, Validate(doc, LayerTenant, DefaultLimits()), tc.field)
		})
	}
}

// TestParseAndValidate_ParseError proves a parse failure short-circuits before
// Validate runs.
func TestParseAndValidate_ParseError(t *testing.T) {
	if _, err := ParseAndValidate([]byte(`{not json`), LayerTenant, DefaultLimits()); err == nil {
		t.Fatal("malformed JSON accepted by ParseAndValidate")
	}
}

// TestValidationError_Format exercises every Error() branch: document-level,
// statement with sid, and statement without sid.
func TestValidationError_Format(t *testing.T) {
	cases := []struct {
		err  *ValidationError
		want []string // substrings that must appear
	}{
		{&ValidationError{Index: -1, Field: fieldVersion, Msg: "version is required"}, []string{"policy document", "version"}},
		{&ValidationError{Index: 2, Sid: "my-rule", Field: fieldEffect, Msg: "bad"}, []string{"statement 2", `"my-rule"`, "effect"}},
		{&ValidationError{Index: 0, Field: fieldSid, Msg: "sid is required"}, []string{"statement 0"}},
	}
	for _, tc := range cases {
		got := tc.err.Error()
		for _, sub := range tc.want {
			if !strings.Contains(got, sub) {
				t.Errorf("Error() = %q, missing %q", got, sub)
			}
		}
	}
}

// stWithSid is a valid statement with a chosen sid (for count/dup tests).
func stWithSid(sid string) Statement {
	s := validStatement()
	s.Sid = sid
	return s
}
