// Package policy is Tappa's decision engine model (ADR 0004). This file defines
// the policy DOCUMENT — the AWS-IAM-shaped JSON that baseline and tenant
// policies are written in — and turns raw bytes into a typed, well-formed
// Document (Parse). It performs NO evaluation: turning a Document + a runtime
// Context into a Decision is the evaluator's job (M3-04). Parse only shapes
// input; validate.go proves a Document is admissible.
//
// The whole package is PURE: no DB, no HTTP, no time.Now(). Validation happens
// at WRITE time; at evaluation time the document is already assumed valid
// (ADR 0004 §8, M3-03 card). That contract is why the closed lists below are
// enforced here and nowhere else has to re-check them.
//
// The operator, context-key, action and resource vocabularies are CLOSED
// (ADR 0004 §8): anything outside them is a validation ERROR, never a silent
// skip — an engine that silently ignores an unknown rule tells the customer
// "my rule works" when it does not, the most dangerous failure. Growing any of
// these lists requires a new ADR (ADR 0004 §8, "yeni operatör = yeni ADR").
package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Document is a full policy document (ADR 0004 §1). Version and Name are
// metadata; Statements are evaluated in order by the engine (M3-04).
type Document struct {
	Version    string      `json:"version"`
	Name       string      `json:"name"`
	Statements []Statement `json:"statements"`
}

// Statement is one IAM-shaped rule: an Effect applied to one or more Actions on
// one or more Resources when the Condition holds. Sid names the statement so a
// Decision can point back to exactly which rule fired (ADR 0004 §10,
// explainability). Reason is the human-facing explanation surfaced on the
// docket ("FLAGGED — policy: gps-only-review").
type Statement struct {
	Sid       string    `json:"sid"`
	Effect    Effect    `json:"effect"`
	Action    []Action  `json:"action"`
	Resource  []string  `json:"resource"`
	Condition Condition `json:"condition,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

// Condition maps each operator to its (context-key → value) arguments, mirroring
// IAM's condition block (ADR 0004 §1):
//
//	{ "Bool": { "tap:ipMatch": false, "tap:gpsMatch": true } }
//
// A nil/empty Condition matches unconditionally. Map keys are not caught by the
// JSON decoder's DisallowUnknownFields (that only guards struct fields), so
// unknown operators and unknown context keys are rejected explicitly in
// validate.go. After Validate returns nil the value shapes are GUARANTEED per
// operator (see validateOperatorValue), so the evaluator (M3-04) may type-assert
// without re-checking.
type Condition map[Operator]map[ContextKey]any

// Effect is a statement outcome (ADR 0004 §2). Five, because a tap's result is
// five, not two: ok · flag · reject · ignored · redirect-to-activation.
type Effect string

const (
	EffectAllow  Effect = "allow"  // ok — counts toward hours
	EffectReview Effect = "review" // flag — record written, goes to approval queue (§4.6)
	EffectDeny   Effect = "deny"   // reject
	// 🔴 ignore IS RECORDED. An earlier version of this line said "no record", which
	// is false and is a §4.6 claim to get wrong: checkin.Service.Record skips the
	// write for EffectRedirect ALONE, so a debounced duplicate lands in
	// `transactions` with verdict='ignored' and no direction — measured against
	// real Postgres in internal/handler (TestCheckinDB_… row 5: the row count goes
	// up by one). What "ignore" means is that the tap does not COUNT, not that it
	// leaves no trace; the trace is the whole point (§4.6).
	EffectIgnore   Effect = "ignore"   // debounce — recorded, counts toward nothing; GUARDRAIL-ONLY (ADR 0004 §6)
	EffectRedirect Effect = "redirect" // activation page — the one effect that writes NO record; GUARDRAIL-ONLY (ADR 0004 §6)
)

var validEffects = map[Effect]bool{
	EffectAllow: true, EffectReview: true, EffectDeny: true,
	EffectIgnore: true, EffectRedirect: true,
}

// Valid reports whether e is one of the five known effects.
func (e Effect) Valid() bool { return validEffects[e] }

// documentEffect reports whether e may appear in a PARSED (baseline/tenant)
// document. ignore and redirect are guardrail-only (ADR 0004 §6): a tenant that
// could write ignore would silence a whole night shift into unpaid, unqueued,
// invisible taps — §4.6's mirror image, a silent CANCELLATION. They live only in
// the immutable, code-embedded guardrail layer, which is never parsed.
func (e Effect) documentEffect() bool {
	return e == EffectAllow || e == EffectReview || e == EffectDeny
}

// Operator is a condition operator (ADR 0004 §8). The set is CLOSED.
type Operator string

const (
	OpStringEquals       Operator = "StringEquals"
	OpStringNotEquals    Operator = "StringNotEquals"
	OpStringIn           Operator = "StringIn"
	OpBool               Operator = "Bool"
	OpNumericEquals      Operator = "NumericEquals"
	OpNumericLessThan    Operator = "NumericLessThan"
	OpNumericGreaterThan Operator = "NumericGreaterThan"
	OpIpInPrefix         Operator = "IpInPrefix"
	OpExists             Operator = "Exists"
	OpTimeBetween        Operator = "TimeBetween"
)

var validOperators = map[Operator]bool{
	OpStringEquals: true, OpStringNotEquals: true, OpStringIn: true,
	OpBool: true, OpNumericEquals: true, OpNumericLessThan: true,
	OpNumericGreaterThan: true, OpIpInPrefix: true, OpExists: true,
	OpTimeBetween: true,
}

// Valid reports whether op is one of the ten known operators.
func (op Operator) Valid() bool { return validOperators[op] }

// Action is a namespaced action (ADR 0004 §8). The engine is general-purpose —
// it also answers "who may approve a FLAGGED tap", "who may export a report",
// etc. — so actions are namespaced. The set is CLOSED.
type Action string

const (
	ActionTapRecord          Action = "tap:record"
	ActionTapApprove         Action = "tap:approve"
	ActionRecordManual       Action = "record:manual"
	ActionRecordReview       Action = "record:review"
	ActionEmployeeDeactivate Action = "employee:deactivate"
	ActionReportExport       Action = "report:export"
	ActionPolicyEdit         Action = "policy:edit"
)

// allActions is the closed action vocabulary as an ORDERED list, and it is the
// single source: validActions is built from it rather than repeating it.
//
// 🔴 IT USED TO BE A SECOND REPRESENTATION. validActions was a hand-written map
// literal naming the same identifiers as the const block above, so adding an
// eighth action meant remembering to name it twice — and the failure mode of
// forgetting is SILENT in the dangerous direction only half the time: a missing
// map entry makes Valid() reject a legitimate action loudly, but the panel
// (M6-09), which now walks this vocabulary to show which authorities exist, would
// simply not mention it. An action nobody can see is an authority nobody reviews.
// TestActions_CoverEveryDeclaredActionConstant parses this file and holds the two
// in step, in both directions.
//
// THE ORDER IS THE DECLARATION ORDER of the const block, which is tap-first then
// the authorization actions. Callers that render it get a stable list; nothing in
// the evaluator depends on it (Evaluate looks actions up, never walks them).
var allActions = []Action{
	ActionTapRecord, ActionTapApprove, ActionRecordManual, ActionRecordReview,
	ActionEmployeeDeactivate, ActionReportExport, ActionPolicyEdit,
}

var validActions = func() map[Action]bool {
	m := make(map[Action]bool, len(allActions))
	for _, a := range allActions {
		m[a] = true
	}
	return m
}()

// Actions returns the closed action vocabulary in declaration order.
//
// It exists for the PANEL (M6-09): the Policies screen has to list every
// authority the engine knows about, and a hand-written list on that screen would
// be a third copy of this vocabulary — the shape where the newest action is the
// one nobody is shown. A COPY is returned so a caller cannot mutate the source.
func Actions() []Action { return append([]Action(nil), allActions...) }

// Valid reports whether a is one of the known actions.
func (a Action) Valid() bool { return validActions[a] }

// ContextKey is a namespaced evaluation input (ADR 0004 §8). Every key is
// derived on the SERVER; none comes from a client-declared body flag (ADR 0004
// §8, "Tüm bağlam anahtarları SUNUCUDA türetilir"). The set is CLOSED.
type ContextKey string

const (
	CtxTapChannel               ContextKey = "tap:channel"
	CtxTapSunValid              ContextKey = "tap:sunValid"
	CtxTapIPMatch               ContextKey = "tap:ipMatch"
	CtxTapGPSMatch              ContextKey = "tap:gpsMatch"
	CtxTapGPSDistanceM          ContextKey = "tap:gpsDistanceM"
	CtxTapGPSConflict           ContextKey = "tap:gpsConflict"
	CtxTapTrust                 ContextKey = "tap:trust"
	CtxTapCtrGap                ContextKey = "tap:ctrGap"
	CtxTapPageAgeSeconds        ContextKey = "tap:pageAgeSeconds"
	CtxTapPractice              ContextKey = "tap:practice"
	CtxTapDirection             ContextKey = "tap:direction"
	CtxTapQueued                ContextKey = "tap:queued"
	CtxTapOccurredAtSkewSeconds ContextKey = "tap:occurredAtSkewSeconds"
	CtxTagStatus                ContextKey = "tag:status"
	CtxTagLocationID            ContextKey = "tag:locationId"
	CtxEmployeeStatus           ContextKey = "employee:status"
	CtxEmployeeLocationID       ContextKey = "employee:locationId"
	CtxEmployeeDepartmentID     ContextKey = "employee:departmentId"
	CtxEmployeeCrossLocation    ContextKey = "employee:crossLocation"
	CtxLocationID               ContextKey = "location:id"
	CtxTimeLocalHour            ContextKey = "time:localHour"
	CtxTimeWithinShift          ContextKey = "time:withinShift"
	CtxTimeMinutesLate          ContextKey = "time:minutesLate"
	CtxActorRole                ContextKey = "actor:role"
)

var validContextKeys = map[ContextKey]bool{
	CtxTapChannel: true, CtxTapSunValid: true, CtxTapIPMatch: true,
	CtxTapGPSMatch: true, CtxTapGPSDistanceM: true, CtxTapGPSConflict: true,
	CtxTapTrust: true, CtxTapCtrGap: true, CtxTapPageAgeSeconds: true,
	CtxTapPractice: true, CtxTapDirection: true, CtxTapQueued: true,
	CtxTapOccurredAtSkewSeconds: true, CtxTagStatus: true, CtxTagLocationID: true,
	CtxEmployeeStatus: true, CtxEmployeeLocationID: true, CtxEmployeeDepartmentID: true,
	CtxEmployeeCrossLocation: true, CtxLocationID: true, CtxTimeLocalHour: true,
	CtxTimeWithinShift: true, CtxTimeMinutesLate: true, CtxActorRole: true,
}

// Valid reports whether k is one of the known context keys.
func (k ContextKey) Valid() bool { return validContextKeys[k] }

// Layer is where a policy sits (ADR 0004 §4). Guardrails are code, never parsed
// documents, so only baseline and tenant documents ever reach Parse/Validate;
// Validate rejects a LayerGuardrail document as a programming error.
type Layer string

const (
	LayerGuardrail Layer = "guardrail" // code-embedded, immutable — NOT a parsed document
	LayerBaseline  Layer = "baseline"  // Tappa's managed default policy
	LayerTenant    Layer = "tenant"    // the customer's own policies
)

// Parse turns raw policy-document bytes into a typed Document (ADR 0004 §1).
// It enforces the byte-size limit BEFORE decoding (so a hostile multi-megabyte
// body is rejected without being unmarshalled), decodes strictly
// (DisallowUnknownFields catches typoed fields like "efect" that would otherwise
// be silently dropped), and rejects trailing data after the object. It never
// panics on hostile input (proven by FuzzParse); malformed JSON returns an
// error, never a partial Document.
//
// Parse does NOT check the closed vocabularies or the structural limits — call
// Validate (or ParseAndValidate) for that.
func Parse(raw []byte, lim Limits) (Document, error) {
	if len(raw) > lim.MaxDocumentBytes {
		return Document{}, &ValidationError{
			Index: -1,
			Field: fieldDocument,
			Msg:   fmt.Sprintf("document is %d bytes, exceeds the %d-byte limit", len(raw), lim.MaxDocumentBytes),
		}
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var doc Document
	if err := dec.Decode(&doc); err != nil {
		// The json error names a field or an offset, never a policy VALUE, and
		// policy documents carry no secrets — so wrapping it is safe (CLAUDE.md
		// §4.7) and helps the author fix their document.
		return Document{}, fmt.Errorf("policy: parse document: %w", err)
	}
	// Reject trailing junk after the top-level object (e.g. "{...} garbage"),
	// which Decode alone would ignore.
	if _, err := dec.Token(); err != io.EOF {
		return Document{}, fmt.Errorf("policy: parse document: unexpected trailing data after top-level object")
	}
	return doc, nil
}

// ParseAndValidate parses raw bytes and validates the resulting Document for the
// given layer in one call. It is the normal write-path entry point; the
// per-tenant document/version quota (which needs DB counts) is enforced
// separately via CheckTenantQuota.
func ParseAndValidate(raw []byte, layer Layer, lim Limits) (Document, error) {
	doc, err := Parse(raw, lim)
	if err != nil {
		return Document{}, err
	}
	if err := Validate(doc, layer, lim); err != nil {
		return Document{}, err
	}
	return doc, nil
}
