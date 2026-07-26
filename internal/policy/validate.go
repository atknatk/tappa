package policy

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// This file is the WRITE-TIME gate (ADR 0004 §8, M3-03 card): a document is
// admissible only if it survives Validate. Everything downstream — the
// evaluator (M3-04), the store (M3-02), the decision record (M3-07) — assumes a
// stored document already passed here, so no other layer re-checks the closed
// vocabularies or the shapes. The rule is uniform: anything unknown or malformed
// is an ERROR, never a silent skip.

// Named limit defaults, all in ONE place (M3-03 card: "Sınır sabitleri tek
// yerde, adlandırılmış"). They are the DoS ceiling: Evaluate runs on EVERY tap
// and the architecture is a single VPS / single process (CLAUDE.md §1), so a
// tenant writing 200 documents × 500 statements could stall the service for
// every tenant at shift start — and because `transactions` is immutable (§4.3),
// records that fail to form then never come back. policy_versions is append-only
// (§4.3), so an unbounded version count also fills the disk. These ceilings make
// the attack in the card's rationale structurally impossible.
//
// The count/quota limits live in Limits (below) so they can be INJECTED — tests
// prove "passes at the limit, fails at limit+1" with tiny values instead of
// building 500-statement fixtures, and the quota check can be fed live DB
// counts. The fixed structural string caps have no reason to vary, so they stay
// as plain constants.
const (
	// DefaultMaxDocumentBytes bounds the whole document before it is decoded.
	DefaultMaxDocumentBytes = 64 * 1024 // 64 KiB
	// DefaultMaxStatementsPerDocument × DefaultMaxDocumentsPerTenant bounds the
	// per-tenant statement count the evaluator walks per tap.
	DefaultMaxStatementsPerDocument  = 100
	DefaultMaxActionsPerStatement    = 20
	DefaultMaxResourcesPerStatement  = 50
	DefaultMaxConditionsPerStatement = 50 // total (operator,key) predicates
	DefaultMaxIPPrefixesPerCondition = 64
	DefaultMaxDocumentsPerTenant     = 50
	DefaultMaxVersionsPerPolicy      = 200

	// Fixed structural caps (never tenant-tunable). Well inside the byte cap;
	// they keep individual fields sane and error messages bounded.
	maxVersionLen  = 64
	maxNameLen     = 200
	maxSidLen      = 128
	maxReasonLen   = 512
	maxResourceLen = 256
)

// NumericRange is an inclusive [Min,Max] range for a bounded parameter.
type NumericRange struct {
	Min, Max float64
}

func (r NumericRange) contains(v float64) bool { return v >= r.Min && v <= r.Max }

// Limits bounds a policy document. Construct via DefaultLimits (optionally
// tweaked); a zero Limits rejects every document (MaxDocumentBytes == 0).
type Limits struct {
	MaxDocumentBytes          int
	MaxStatementsPerDocument  int
	MaxActionsPerStatement    int
	MaxResourcesPerStatement  int
	MaxConditionsPerStatement int
	MaxIPPrefixesPerCondition int
	MaxDocumentsPerTenant     int
	MaxVersionsPerPolicy      int

	// BoundedParams enforces ADR 0004 §11: some guardrails cannot be switched
	// off but MAY be tuned within a range (debounce 30–300 s, freshness 1–15 min,
	// occurred_at skew 0–72 h, GPS radius 25–1000 m). When a numeric condition
	// (NumericEquals/LessThan/GreaterThan) targets a key present here, its VALUE
	// must fall inside the range or the document is rejected.
	//
	// The ENFORCEMENT MECHANISM lives here and is tested. The concrete
	// key→range wiring is deliberately LEFT EMPTY in DefaultLimits and FLAGGED,
	// not invented: the four ranges and their units belong to M3-05 (guardrail
	// parameters) and to internal/config, which do not exist yet, and only two
	// of them map cleanly onto a single context key (gpsDistanceM,
	// pageAgeSeconds); debounce has no context key at all — it is a pure
	// guardrail parameter, not a document predicate. Guessing a key/unit here
	// would bake an unverified decision into the validator. M3-05 populates this
	// map from the same named constants config uses (ADR 0004 §11).
	BoundedParams map[ContextKey]NumericRange
}

// DefaultLimits returns the production limits (the Default* constants).
// BoundedParams is intentionally nil — see the field comment.
func DefaultLimits() Limits {
	return Limits{
		MaxDocumentBytes:          DefaultMaxDocumentBytes,
		MaxStatementsPerDocument:  DefaultMaxStatementsPerDocument,
		MaxActionsPerStatement:    DefaultMaxActionsPerStatement,
		MaxResourcesPerStatement:  DefaultMaxResourcesPerStatement,
		MaxConditionsPerStatement: DefaultMaxConditionsPerStatement,
		MaxIPPrefixesPerCondition: DefaultMaxIPPrefixesPerCondition,
		MaxDocumentsPerTenant:     DefaultMaxDocumentsPerTenant,
		MaxVersionsPerPolicy:      DefaultMaxVersionsPerPolicy,
		BoundedParams:             nil,
	}
}

// Field names used in ValidationError, so tests assert on stable labels.
const (
	fieldDocument   = "document"
	fieldLayer      = "layer"
	fieldVersion    = "version"
	fieldName       = "name"
	fieldStatements = "statements"
	fieldSid        = "sid"
	fieldEffect     = "effect"
	fieldAction     = "action"
	fieldResource   = "resource"
	fieldCondition  = "condition"
	fieldOperator   = "operator"
	fieldContextKey = "contextKey"
	fieldQuota      = "quota"
)

// ValidationError explains why a document is inadmissible. Its Error() is SAFE
// to log (CLAUDE.md §4.7): it names the statement index, the author's own sid,
// the offending FIELD, and — for closed-vocabulary mismatches — the rejected
// KEYWORD (e.g. an unknown effect), because the author must see it to fix their
// document and a policy keyword is not a secret. It NEVER echoes a condition's
// comparison VALUE (a string, number or IP), keeping the §4.7 discipline even
// though documents are not expected to contain secrets.
type ValidationError struct {
	// Index is the 0-based statement index, or -1 for a document-level problem.
	Index int
	// Sid is the offending statement's sid when known, else "".
	Sid string
	// Field is the part at fault (one of the field* constants).
	Field string
	// Msg is a value-free reason; it may name a closed-vocabulary keyword.
	Msg string
}

func (e *ValidationError) Error() string {
	loc := "policy document"
	if e.Index >= 0 {
		if e.Sid != "" {
			loc = fmt.Sprintf("statement %d (%q)", e.Index, e.Sid)
		} else {
			loc = fmt.Sprintf("statement %d", e.Index)
		}
	}
	field := ""
	if e.Field != "" {
		field = " " + e.Field
	}
	return fmt.Sprintf("policy:%s %s: %s", field, loc, e.Msg)
}

// Validate proves a parsed Document is admissible at the given layer (ADR 0004
// §8, M3-03 card). It returns the FIRST problem as a *ValidationError; the walk
// is deterministic (statements in slice order; condition operators and keys
// sorted) so the same document always yields the same error.
//
// Validate is PURE — no DB, no time. The per-tenant quota, which needs DB
// counts, is a separate call (CheckTenantQuota).
func Validate(doc Document, layer Layer, lim Limits) error {
	switch layer {
	case LayerBaseline, LayerTenant:
		// ok — the only two layers that exist as parsed documents.
	case LayerGuardrail:
		return &ValidationError{Index: -1, Field: fieldLayer,
			Msg: "guardrail policies are code-embedded and immutable, not parsed documents (ADR 0004 §4)"}
	default:
		return &ValidationError{Index: -1, Field: fieldLayer, Msg: "unknown policy layer"}
	}

	if strings.TrimSpace(doc.Version) == "" {
		return &ValidationError{Index: -1, Field: fieldVersion, Msg: "version is required"}
	}
	if len(doc.Version) > maxVersionLen {
		return &ValidationError{Index: -1, Field: fieldVersion,
			Msg: fmt.Sprintf("version exceeds %d characters", maxVersionLen)}
	}
	if strings.TrimSpace(doc.Name) == "" {
		return &ValidationError{Index: -1, Field: fieldName, Msg: "name is required"}
	}
	if len(doc.Name) > maxNameLen {
		return &ValidationError{Index: -1, Field: fieldName,
			Msg: fmt.Sprintf("name exceeds %d characters", maxNameLen)}
	}

	if len(doc.Statements) == 0 {
		return &ValidationError{Index: -1, Field: fieldStatements, Msg: "at least one statement is required"}
	}
	if len(doc.Statements) > lim.MaxStatementsPerDocument {
		return &ValidationError{Index: -1, Field: fieldStatements,
			Msg: fmt.Sprintf("%d statements exceed the limit of %d", len(doc.Statements), lim.MaxStatementsPerDocument)}
	}

	// Sids must be unique: a Decision points back to a statement by sid
	// (ADR 0004 §10), and duplicates would make that reference ambiguous.
	seen := make(map[string]bool, len(doc.Statements))
	for i, st := range doc.Statements {
		if err := validateStatement(i, st, lim); err != nil {
			return err
		}
		if seen[st.Sid] {
			return &ValidationError{Index: i, Sid: st.Sid, Field: fieldSid, Msg: "duplicate sid"}
		}
		seen[st.Sid] = true
	}
	return nil
}

func validateStatement(i int, st Statement, lim Limits) error {
	// --- sid -----------------------------------------------------------------
	if strings.TrimSpace(st.Sid) == "" {
		return &ValidationError{Index: i, Field: fieldSid, Msg: "sid is required"}
	}
	if len(st.Sid) > maxSidLen {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldSid,
			Msg: fmt.Sprintf("sid exceeds %d characters", maxSidLen)}
	}
	// The sys: namespace is RESERVED for code-embedded guardrails (ADR 0004 §4,
	// §6); a parsed document must never claim it. Case-insensitive so "SYS:" is
	// not a sneaky bypass. (The base: namespace is reserved for the baseline
	// layer — enforced in M3-06, which owns the layer-specific rule.)
	if strings.HasPrefix(strings.ToLower(st.Sid), "sys:") {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldSid,
			Msg: "sid uses the reserved sys: namespace (guardrails only)"}
	}

	// --- effect --------------------------------------------------------------
	if !st.Effect.Valid() {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldEffect,
			Msg: fmt.Sprintf("unknown effect %q", st.Effect)}
	}
	if !st.Effect.documentEffect() {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldEffect,
			Msg: fmt.Sprintf("effect %q may only be produced by guardrails, not policy documents", st.Effect)}
	}

	// --- action --------------------------------------------------------------
	if len(st.Action) == 0 {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldAction, Msg: "at least one action is required"}
	}
	if len(st.Action) > lim.MaxActionsPerStatement {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldAction,
			Msg: fmt.Sprintf("%d actions exceed the limit of %d", len(st.Action), lim.MaxActionsPerStatement)}
	}
	for _, a := range st.Action {
		if !a.Valid() {
			return &ValidationError{Index: i, Sid: st.Sid, Field: fieldAction,
				Msg: fmt.Sprintf("unknown action %q", a)}
		}
	}

	// --- resource ------------------------------------------------------------
	if len(st.Resource) == 0 {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldResource, Msg: "at least one resource is required"}
	}
	if len(st.Resource) > lim.MaxResourcesPerStatement {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldResource,
			Msg: fmt.Sprintf("%d resources exceed the limit of %d", len(st.Resource), lim.MaxResourcesPerStatement)}
	}
	for _, r := range st.Resource {
		if len(r) > maxResourceLen {
			return &ValidationError{Index: i, Sid: st.Sid, Field: fieldResource,
				Msg: fmt.Sprintf("resource exceeds %d characters", maxResourceLen)}
		}
		if !validResource(r) {
			// r is the author's own scope string, not a secret; naming it helps
			// them fix a malformed resource.
			return &ValidationError{Index: i, Sid: st.Sid, Field: fieldResource,
				Msg: fmt.Sprintf("resource %q is not one of location/<id>, department/<id>, employee/<id>, or *", r)}
		}
	}

	// --- reason (optional) ---------------------------------------------------
	if len(st.Reason) > maxReasonLen {
		return &ValidationError{Index: i, Sid: st.Sid, Field: "reason",
			Msg: fmt.Sprintf("reason exceeds %d characters", maxReasonLen)}
	}

	// --- condition -----------------------------------------------------------
	return validateCondition(i, st, lim)
}

// validateCondition enforces the closed operator/context-key lists, the
// per-statement predicate limit, and the per-operator value shapes. A nil/empty
// condition is valid (matches unconditionally). Operators and keys are iterated
// in sorted order so the FIRST error is deterministic despite Go's random map
// iteration.
func validateCondition(i int, st Statement, lim Limits) error {
	total := 0
	for _, args := range st.Condition {
		total += len(args)
	}
	if total > lim.MaxConditionsPerStatement {
		return &ValidationError{Index: i, Sid: st.Sid, Field: fieldCondition,
			Msg: fmt.Sprintf("%d condition predicates exceed the limit of %d", total, lim.MaxConditionsPerStatement)}
	}

	for _, op := range sortedOperators(st.Condition) {
		if !op.Valid() {
			return &ValidationError{Index: i, Sid: st.Sid, Field: fieldOperator,
				Msg: fmt.Sprintf("unknown operator %q", op)}
		}
		args := st.Condition[op]
		if len(args) == 0 {
			return &ValidationError{Index: i, Sid: st.Sid, Field: fieldOperator,
				Msg: fmt.Sprintf("operator %q has no context keys", op)}
		}
		for _, key := range sortedKeys(args) {
			if !key.Valid() {
				return &ValidationError{Index: i, Sid: st.Sid, Field: fieldContextKey,
					Msg: fmt.Sprintf("unknown context key %q", key)}
			}
			if err := validateOperatorValue(i, st.Sid, op, key, args[key], lim); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateOperatorValue enforces the value SHAPE each operator requires, so the
// evaluator (M3-04) may type-assert after Validate without re-checking. Numbers
// decode as float64 (encoding/json default); arrays as []any. Errors name the
// operator/key and the expected shape, never the offending VALUE (CLAUDE.md
// §4.7).
//
// Two shape choices are ASSUMPTIONS flagged for review, as the ADR gives no
// example for them:
//   - Exists takes a bool (true = key must be present, false = must be absent),
//     matching IAM's Null operator.
//   - TimeBetween takes exactly two numbers [start,end]; the only plausible
//     target key, time:localHour, is numeric (0–23). Wraparound semantics
//     (e.g. 22→2 for a night shift) are the evaluator's concern (M3-04).
func validateOperatorValue(i int, sid string, op Operator, key ContextKey, val any, lim Limits) error {
	shapeErr := func(want string) error {
		return &ValidationError{Index: i, Sid: sid, Field: fieldCondition,
			Msg: fmt.Sprintf("operator %q on %q expects %s", op, key, want)}
	}

	switch op {
	case OpStringEquals, OpStringNotEquals:
		if _, ok := val.(string); !ok {
			return shapeErr("a string value")
		}

	case OpStringIn:
		arr, ok := val.([]any)
		if !ok || len(arr) == 0 {
			return shapeErr("a non-empty array of strings")
		}
		for _, el := range arr {
			if _, ok := el.(string); !ok {
				return shapeErr("a non-empty array of strings")
			}
		}

	case OpBool, OpExists:
		if _, ok := val.(bool); !ok {
			return shapeErr("a boolean value")
		}

	case OpNumericEquals, OpNumericLessThan, OpNumericGreaterThan:
		f, ok := val.(float64)
		if !ok {
			return shapeErr("a numeric value")
		}
		// Bounded-parameter enforcement (ADR 0004 §11). Only fires for keys the
		// caller registered in Limits.BoundedParams; see that field's comment.
		if r, bounded := lim.BoundedParams[key]; bounded && !r.contains(f) {
			return &ValidationError{Index: i, Sid: sid, Field: fieldCondition,
				Msg: fmt.Sprintf("value for %q is outside its allowed range [%g, %g]", key, r.Min, r.Max)}
		}

	case OpIpInPrefix:
		arr, ok := val.([]any)
		if !ok || len(arr) == 0 {
			return shapeErr("a non-empty array of CIDR prefixes")
		}
		if len(arr) > lim.MaxIPPrefixesPerCondition {
			return &ValidationError{Index: i, Sid: sid, Field: fieldCondition,
				Msg: fmt.Sprintf("%d IP prefixes exceed the limit of %d", len(arr), lim.MaxIPPrefixesPerCondition)}
		}
		for idx, el := range arr {
			s, ok := el.(string)
			if !ok {
				return shapeErr("a non-empty array of CIDR prefixes")
			}
			if _, err := netip.ParsePrefix(s); err != nil {
				// Report the position, not the prefix value.
				return &ValidationError{Index: i, Sid: sid, Field: fieldCondition,
					Msg: fmt.Sprintf("operator %q on %q: element %d is not a valid CIDR prefix", op, key, idx)}
			}
		}

	case OpTimeBetween:
		arr, ok := val.([]any)
		if !ok || len(arr) != 2 {
			return shapeErr("an array of exactly two numbers [start, end]")
		}
		for _, el := range arr {
			if _, ok := el.(float64); !ok {
				return shapeErr("an array of exactly two numbers [start, end]")
			}
		}

	default:
		// Unreachable: the caller already rejected unknown operators. Kept so a
		// future operator added without a case here fails LOUDLY, not silently.
		return &ValidationError{Index: i, Sid: sid, Field: fieldOperator,
			Msg: fmt.Sprintf("operator %q has no value-shape rule", op)}
	}
	return nil
}

// CheckTenantQuota enforces the per-tenant document quota and the per-policy
// version quota (ADR 0004 §9, M3-03 card). It is PURE but needs COUNTS the pure
// validator cannot obtain, so the write path (M3-02 store / M3-07) queries the
// DB for the current counts and passes them here. This keeps internal/policy
// free of DB access (same discipline as the evaluator) while still centralising
// the quota limits in one place.
//
// Counts are the state BEFORE the pending write, so the check uses >= : if the
// tenant already holds the maximum, adding one more would exceed it. Pass
// currentVersionsForPolicy = 0 when creating a brand-new policy.
func CheckTenantQuota(currentDocuments, currentVersionsForPolicy int, lim Limits) error {
	if currentDocuments >= lim.MaxDocumentsPerTenant {
		return &ValidationError{Index: -1, Field: fieldQuota,
			Msg: fmt.Sprintf("tenant already holds the maximum of %d policy documents", lim.MaxDocumentsPerTenant)}
	}
	if currentVersionsForPolicy >= lim.MaxVersionsPerPolicy {
		return &ValidationError{Index: -1, Field: fieldQuota,
			Msg: fmt.Sprintf("policy already holds the maximum of %d versions", lim.MaxVersionsPerPolicy)}
	}
	return nil
}

var resourceTypes = map[string]bool{"location": true, "department": true, "employee": true}

// validResource reports whether r is one of the four resource forms (ADR 0004
// §8): the global wildcard "*", or "<type>/<id>" where type is location,
// department or employee and id is a single non-empty segment (which may itself
// be "*" for a type-wide wildcard, e.g. "location/*").
func validResource(r string) bool {
	if r == "*" {
		return true
	}
	typ, id, ok := strings.Cut(r, "/")
	if !ok || !resourceTypes[typ] || id == "" || strings.Contains(id, "/") {
		return false
	}
	return true
}

func sortedOperators(c Condition) []Operator {
	ops := make([]Operator, 0, len(c))
	for op := range c {
		ops = append(ops, op)
	}
	sort.Slice(ops, func(a, b int) bool { return ops[a] < ops[b] })
	return ops
}

func sortedKeys(args map[ContextKey]any) []ContextKey {
	keys := make([]ContextKey, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool { return keys[a] < keys[b] })
	return keys
}
