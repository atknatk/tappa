package policy

// This file is the EVALUATOR (ADR 0004 §4, §5, §7, §10; M3-04 card). Given a Set
// (the guardrail list + the baseline and tenant policies) and a Context (one
// tap's / one request's server-derived inputs) it returns a single explainable
// Decision. It sits on top of the M3-03 document model (document.go / validate.go)
// and re-uses those types verbatim; it never re-defines them.
//
// PURITY (M3-04 card, mirrors internal/sun and internal/domain/tap). Evaluate is
// a pure function of (Set, Context): no DB, no HTTP, no time.Now(). Every input
// — including "now" as time:localHour and every derived signal — arrives in the
// Context, computed on the SERVER (ADR 0004 §8). That is what makes the Decision
// deterministic and what lets M3-07 replay a past tap: same Context, same
// Decision, forever. The one side effect is observability: an eval-time ANOMALY
// (a stored, once-valid document that now names an unknown operator/key after a
// code rollback) is reported through Set.OnAnomaly; it never changes the returned
// Decision, so determinism holds.
//
// EVALUATION ORDER (ADR 0004 §4-§5):
//
//  1. GUARDRAILS run first, in slice order; the FIRST that matches is TERMINAL —
//     no lower layer runs. The order IS a security boundary (ADR 0004 §5): it is
//     M3-05's job to supply the 1->10 list; this file only guarantees "first
//     match wins and stops". A guardrail can return ANY effect, including the
//     tenant-forbidden ignore/redirect (ADR 0004 §6), because it is code, not a
//     parsed document.
//
//  2. BASELINE + TENANT are evaluated TOGETHER (ADR 0004 §4). Among the matching
//     statements the winner is chosen by, in order: (a) MOST SPECIFIC resource
//     wins — location/rusty-bar beats location/* beats * (ADR 0004 §7, the Rusty
//     Bar exception); (b) at equal specificity the MOST RESTRICTIVE effect wins,
//     deny > ignore > review > allow; (c) a full tie is broken by first-encountered
//     in a FIXED walk (baseline before tenant, statements in document order), so
//     the reported MatchedSid is deterministic and NEVER depends on Go's random
//     map iteration (M3-04 card trap).
//
//  3. If nothing matched, the two built-in DEFAULTS apply (ADR 0004 §3):
//     tap-RECORDING actions fail to REVIEW (§4.6, records are never lost); every
//     other action fails CLOSED to DENY. MatchedSid = "default".
//
// Every returned Decision carries MatchedSid and Layer — there is no unexplained
// decision (ADR 0004 §10).

import (
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

// Decision is the evaluator's output — the one explainable verdict for a request
// (ADR 0004 §10). Every field is populated on every path: a caller can always
// answer "which rule decided this, and in which layer".
type Decision struct {
	// Effect is the outcome (allow | review | deny | ignore | redirect). ignore
	// and redirect can only come from a guardrail (ADR 0004 §6).
	Effect Effect
	// MatchedSid is the sid of the statement/guardrail that decided, or the
	// sentinel "default" when nothing matched (ADR 0004 §10, madde 3).
	MatchedSid string
	// PolicyVersionID is the policy_versions row a baseline/tenant decision came
	// from (ADR 0004 §9, feeds M3-07). It is nil for guardrail decisions (code,
	// not in the DB) and for the built-in default.
	PolicyVersionID *uuid.UUID
	// Layer is where the deciding rule lives. Guardrail decisions and the two
	// built-in defaults carry LayerGuardrail (both are code-embedded, nil
	// version); a "default" MatchedSid distinguishes a default from a real
	// guardrail (sys:*) match.
	Layer Layer
	// Reason is the human-facing explanation surfaced on the docket
	// ("FLAGGED — policy: gps-only-review"). It is the statement's / guardrail's
	// Reason, or a fixed sentence for a default.
	Reason string
	// SecurityAlert, when non-empty, names a security event the caller should
	// push to the tenant's managers (§5): a lost tag was tapped, or a deactivated
	// employee's session tapped. It is set ONLY by a guardrail that actually fires
	// and declares an Alert (sys:tag-not-active on `lost`, sys:employee-deactivated)
	// — so the normative order (sys:sun-invalid before both) means a forged SUN can
	// never manufacture one (ADR 0004 §5). It is a fixed vocabulary term, never a
	// secret, GPS coordinate or raw value (§4.7). Empty on every other path.
	SecurityAlert SecurityAlert
}

// Context is one evaluation's input. The M3-04 brief describes it as "a
// context-key -> value map (the 24 keys of M3-03)"; that map is Keys. Action and
// Resources are ALSO per-request and cannot live in Set (which holds per-tenant
// policy data reused across every tap), and Evaluate's fixed one-request
// parameter is this Context — so Context is a small struct wrapping the 24-key
// map plus the request's Action and concrete Resources, all server-derived
// (ADR 0004 §8). Nothing here comes from a client-declared body flag.
type Context struct {
	// Action is the action being requested (tap:record, policy:edit, ...). A
	// statement matches only if this action is in its Action list (exact match;
	// documents carry no action wildcards — validate.go's closed list has none).
	Action Action
	// Resources are the CONCRETE resources the request targets, each of the form
	// "type/id" (e.g. "location/rusty-bar", "department/kitchen", "employee/x").
	// A statement's resource pattern must match at least one of these; the most
	// specific matching pattern sets the statement's specificity (ADR 0004 §7).
	Resources []string
	// Keys are the server-derived condition inputs (the closed ContextKey set of
	// M3-03). A missing key is NOT false: every operator but Exists simply does
	// not match on an absent key (conditions.go, M3-04 card).
	Keys map[ContextKey]any

	// --- Guardrail-only, server-derived inputs (ADR 0004 §8; M3-05) ----------
	// These carry the handful of inputs the code-embedded guardrails need that
	// are NOT part of the closed document ContextKey vocabulary — session
	// presence, tag-vs-session tenant, the per-PERSON debounce gap, reviewer
	// identity. They are explicit TYPED fields, not entries in Keys, so a tenant
	// document can never set them and they cannot collide with (or grow) the
	// closed key list — which needs an ADR to change and, per §4.1, defines no key
	// derived from a person's body. Every one is derived on the SERVER; none is a
	// client-declared flag.
	// Their nil zero values mean "absent", so every existing caller (and every
	// M3-04 test) that leaves them unset is unaffected — and, for debounce, the
	// safe zero value: nil never ignores a real record (§4.6).

	// SessionTenantID is the resolved session's tenant (proof-of-person), or nil
	// when there is no valid session. One field answers both "is there a session"
	// (nil check — sys:no-session) and "whose" (sys:tenant-mismatch).
	SessionTenantID *uuid.UUID
	// TagTenantID is the tenant that owns the tapped tag (server-resolved from the
	// tag UID), or nil for a request with no tag (an authorization action).
	TagTenantID *uuid.UUID
	// SecondsSincePersonLastTap is the gap to the SAME PERSON's previous tap,
	// computed server-side — person-based, never tag-based (§5), so different
	// people can tap one plaque back-to-back and each is recorded. nil means the
	// person has no previous tap, so sys:person-debounce never fires on a first
	// tap.
	SecondsSincePersonLastTap *float64
	// ReviewerID and SubjectEmployeeID drive sys:no-self-review: an actor may not
	// approve a transaction that is their own. Both nil for a non-review request.
	ReviewerID        *uuid.UUID
	SubjectEmployeeID *uuid.UUID
}

// Guardrail is one code-embedded, immutable system rule (ADR 0004 §4-§5). It is
// NOT a parsed document: it may use the sys: namespace and the guardrail-only
// effects (ignore, redirect), neither of which write-time validation would allow
// in a baseline/tenant document (validate.go). M3-05 supplies the ORDERED list;
// this package only runs it. The engine does no action/resource matching for a
// guardrail — Match is a self-contained predicate that inspects whatever it needs
// from the Context (including ctx.Action, so an authorization guardrail like
// sys:policy-edit-owner-only can scope itself). A nil Match never fires.
type Guardrail struct {
	Sid    string
	Effect Effect
	Reason string
	Match  func(ctx Context) bool
	// Alert, when non-nil, is called ONLY if this guardrail fires, to determine
	// the SecurityAlert to surface on the Decision. It may return "" for a firing
	// that needs no alert (sys:tag-not-active alerts on `lost` but not `retired`).
	// It must return only a fixed vocabulary term, never a raw value (§4.7). nil
	// means this guardrail never alerts.
	Alert func(ctx Context) SecurityAlert
}

// Policy is one stored, ALREADY-VALIDATED document (baseline or tenant) paired
// with the policy_versions id it came from (ADR 0004 §9). The document passed
// validate.go at write time, so the evaluator trusts its shapes and never
// re-validates (the closed-list check lives in exactly one place — M3-03).
type Policy struct {
	VersionID uuid.UUID
	Layer     Layer // LayerBaseline or LayerTenant
	Document  Document
}

// Set is everything Evaluate needs for one tenant: the ordered guardrail list and
// the baseline + tenant policies. Baseline and Tenant are kept SEPARATE (not one
// merged slice) so the deterministic tiebreak — baseline before tenant, then
// document order — is guaranteed here regardless of how the caller assembled
// them; for a full tie the two candidates have identical (specificity, effect),
// so the resulting Effect is invariant and only the reported sid is fixed.
//
// OnAnomaly is the eval-time anomaly sink (see reportAnomaly). It is optional:
// when nil, anomalies go to slog.Default() so the signal is never silently lost.
type Set struct {
	Guardrails []Guardrail
	Baseline   []Policy
	Tenant     []Policy
	OnAnomaly  func(Anomaly)
}

// AnomalyKind classifies an eval-time anomaly.
type AnomalyKind string

const (
	// AnomalyUnknownOperator: a stored, once-valid document names an operator the
	// current code no longer knows. Only possible after a code rollback under
	// append-only versioning (ADR 0004 §9): the operator was in the closed list
	// when the document was written and validated, and is not now.
	AnomalyUnknownOperator AnomalyKind = "unknown-operator"
	// AnomalyUnknownKey: likewise for a context key.
	AnomalyUnknownKey AnomalyKind = "unknown-context-key"
	// AnomalyBadContextValue: the Context supplied a value of the wrong type for
	// the operator (a server bug — Context values are not write-time validated).
	AnomalyBadContextValue AnomalyKind = "bad-context-value"
)

// Anomaly describes an eval-time problem WITHOUT any secret or comparison value
// (CLAUDE.md §4.7, §7): it names only policy vocabulary — the deciding sid, the
// layer, and the operator/key at fault — never a context value, an IP, or a
// number. It is reported through Set.OnAnomaly and never appears in the Decision.
type Anomaly struct {
	Kind     AnomalyKind
	Layer    Layer
	Sid      string
	Operator Operator
	Key      ContextKey
}

// reportAnomaly surfaces an eval-time anomaly. The DESIGN CHOICE (M3-04 card,
// "justify the logging design; the signal must NOT be lost"):
//
//   - Correctness does NOT depend on the log. An unknown operator/key makes the
//     predicate FAIL — the statement becomes inert, so a conditional deny can
//     NEVER silently turn unconditional (which would reject every tap). That
//     fail-safe is structural, in conditions.go; the report is purely for
//     OBSERVABILITY, so an operator notices a rollback stranded a live policy.
//   - Evaluate stays a pure function of (Set, Context) -> Decision: the report
//     never touches the returned Decision, so determinism holds. In the normal
//     (no-anomaly) path Evaluate does zero I/O.
//   - The sink is INJECTABLE (Set.OnAnomaly) so it is testable without global
//     state — a test proves "the signal fired" with a recorder, and a caller
//     (e.g. M3-07) can persist anomalies. When nil it falls back to structured
//     slog so a forgetful caller still cannot lose the signal.
//   - It is §4.7-safe: only sid / layer / operator / key are logged, all policy
//     keywords, never a context value.
func (s Set) reportAnomaly(a Anomaly) {
	if s.OnAnomaly != nil {
		s.OnAnomaly(a)
		return
	}
	slog.Warn("policy: eval-time anomaly; statement treated as non-matching",
		"kind", string(a.Kind),
		"layer", string(a.Layer),
		"sid", a.Sid,
		"operator", string(a.Operator),
		"contextKey", string(a.Key),
	)
}

// Resource-pattern specificity (ADR 0004 §7). Higher is more specific and wins.
const (
	specGlobal = 0 // "*"
	specType   = 1 // "location/*"
	specExact  = 2 // "location/rusty-bar"
)

// Evaluate returns the one explainable Decision for ctx against set (ADR 0004
// §4-§5, §10). See the file header for the full order.
func Evaluate(set Set, ctx Context) Decision {
	// 1. Guardrails: ordered, first match terminal (ADR 0004 §5).
	for _, g := range set.Guardrails {
		if g.Match != nil && g.Match(ctx) {
			// The alert (if any) is computed ONLY for the guardrail that actually
			// fires — so it inherits the terminal-order guarantee: an earlier
			// guardrail (e.g. sys:sun-invalid) pre-empting this one means no alert
			// is produced (ADR 0004 §5, the push-flood exploit).
			var alert SecurityAlert
			if g.Alert != nil {
				alert = g.Alert(ctx)
			}
			return Decision{
				Effect:          g.Effect,
				MatchedSid:      g.Sid,
				PolicyVersionID: nil, // guardrails are code, not in the DB
				Layer:           LayerGuardrail,
				Reason:          g.Reason,
				SecurityAlert:   alert,
			}
		}
	}

	// 2. Baseline + tenant together, most-specific-then-most-restrictive wins.
	var best match
	found := false
	consider := func(m match) {
		switch {
		case !found:
			best, found = m, true
		case m.specificity > best.specificity:
			best = m
		case m.specificity < best.specificity:
			// keep best
		case restrictiveness(m.effect) > restrictiveness(best.effect):
			best = m
		default:
			// equal specificity AND equal restrictiveness: keep the
			// first-encountered candidate so MatchedSid is deterministic.
		}
	}
	scan := func(policies []Policy) {
		for _, p := range policies {
			for _, st := range p.Document.Statements {
				if !actionMatches(st.Action, ctx.Action) {
					continue
				}
				spec, ok := statementSpecificity(st.Resource, ctx.Resources)
				if !ok {
					continue
				}
				// Condition is evaluated only for a statement whose action AND
				// resource already match, so anomalies in wholly-irrelevant
				// statements are not reported (they surface on the first request
				// that actually reaches them).
				if !evalCondition(st.Sid, p.Layer, st.Condition, ctx, set.reportAnomaly) {
					continue
				}
				consider(match{
					effect:      st.Effect,
					sid:         st.Sid,
					layer:       p.Layer,
					versionID:   p.VersionID,
					reason:      st.Reason,
					specificity: spec,
				})
			}
		}
	}
	scan(set.Baseline) // baseline first — fixes the full-tie order (see Set doc)
	scan(set.Tenant)

	if found {
		vid := best.versionID
		return Decision{
			Effect:          best.effect,
			MatchedSid:      best.sid,
			PolicyVersionID: &vid,
			Layer:           best.layer,
			Reason:          best.reason,
		}
	}

	// 3. No match anywhere -> the two built-in defaults (ADR 0004 §3).
	return defaultDecision(ctx.Action)
}

// match is one candidate statement that matched the request, with the specificity
// of its winning resource pattern.
type match struct {
	effect      Effect
	sid         string
	layer       Layer
	versionID   uuid.UUID
	reason      string
	specificity int
}

// restrictiveness ranks effects for the equal-specificity tiebreak: deny > ignore
// > review > allow (ADR 0004 §4). ignore never reaches here from a document
// (validate.go forbids it in baseline/tenant), but it is ranked for completeness;
// redirect is guardrail-only and terminal, so it never enters this comparison and
// ranks below allow defensively.
func restrictiveness(e Effect) int {
	switch e {
	case EffectDeny:
		return 4
	case EffectIgnore:
		return 3
	case EffectReview:
		return 2
	case EffectAllow:
		return 1
	default: // EffectRedirect or anything unexpected — cannot appear in a document
		return 0
	}
}

// actionMatches reports whether the request's action is in the statement's action
// list. Exact match only — documents carry no action wildcards (validate.go's
// closed action list has none).
func actionMatches(stmtActions []Action, req Action) bool {
	for _, a := range stmtActions {
		if a == req {
			return true
		}
	}
	return false
}

// statementSpecificity reports whether any of a statement's resource patterns
// matches any of the request's concrete resources, and if so the HIGHEST
// specificity among the matching patterns (ADR 0004 §7). A statement listing both
// "location/rusty-bar" and "*" scopes to a rusty-bar request at specExact.
func statementSpecificity(patterns, reqResources []string) (int, bool) {
	best := -1
	for _, p := range patterns {
		for _, r := range reqResources {
			if spec, ok := patternMatch(p, r); ok && spec > best {
				best = spec
			}
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// patternMatch reports whether a statement resource pattern matches one concrete
// request resource, and the pattern's specificity. Patterns are the four forms
// validate.go admits: "*" (global), "type/*" (type wildcard), "type/id" (exact).
// Concrete request resources are always "type/id".
func patternMatch(pattern, concrete string) (int, bool) {
	if pattern == "*" {
		return specGlobal, true
	}
	pt, pid, pok := strings.Cut(pattern, "/")
	ct, cid, cok := strings.Cut(concrete, "/")
	if !pok || !cok || pt != ct {
		return 0, false
	}
	if pid == "*" {
		return specType, true
	}
	if pid == cid {
		return specExact, true
	}
	return 0, false
}

// reviewDefaultActions are the actions that fail to REVIEW when nothing matches
// (ADR 0004 §3, fail-to-review). Only tap RECORDING defaults to review, because
// §4.6 ("records are never lost") protects an employee's own tap. Every OTHER
// action defaults to DENY (fail-closed) — INCLUDING tap:approve, which is an
// AUTHORIZATION decision despite its tap: prefix (ADR 0004 §3 lists it under the
// deny defaults, as does the M3-08 test). This set is the precise reading of the
// ADR's loose "tap:* -> review" shorthand.
var reviewDefaultActions = map[Action]bool{
	ActionTapRecord: true,
}

// defaultDecision is the no-match fallback (ADR 0004 §3, §10). Both branches carry
// LayerGuardrail with a nil version and MatchedSid "default": the two defaults are
// code-embedded system decisions, and "default" (not a sys: sid) marks them apart
// from a real guardrail match.
func defaultDecision(action Action) Decision {
	if reviewDefaultActions[action] {
		return Decision{
			Effect:     EffectReview,
			MatchedSid: "default",
			Layer:      LayerGuardrail,
			Reason:     "no policy matched; tap recording defaults to review (ADR 0004 §3, fail-to-review)",
		}
	}
	return Decision{
		Effect:     EffectDeny,
		MatchedSid: "default",
		Layer:      LayerGuardrail,
		Reason:     "no policy matched; non-recording actions default to deny (ADR 0004 §3, fail-closed)",
	}
}
