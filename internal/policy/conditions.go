package policy

// This file is the CONDITION evaluator (ADR 0004 §8; M3-04 card). It answers one
// question — "does this statement's Condition hold for this Context?" — and the
// ten operators it does so with. It is called only by evaluate.go, only for a
// statement whose action and resource already matched.
//
// Three rules are load-bearing (M3-04 card acceptance criteria):
//
//   - AND semantics (IAM condition block): a Condition holds only if EVERY
//     predicate holds. Predicates are walked in SORTED operator/key order (the
//     same helpers validate.go uses) so the FIRST anomaly reported is
//     deterministic despite Go's random map iteration. The boolean RESULT is
//     order-independent regardless (it is an AND).
//
//   - Missing key is NOT false. For every operator except Exists, an absent
//     context key makes the predicate simply not match — it is neither an error
//     nor an implicit false. This is why StringNotEquals on an absent key does
//     NOT match (a naive `got != want` with a zero got would wrongly match).
//     Exists is the sole operator defined on absence.
//
//   - Unknown operator / unknown key at EVAL time -> the predicate FAILS and the
//     event is reported. Never the reverse: skipping an unresolvable predicate
//     (treating it as satisfied) would turn a conditional deny into an
//     UNCONDITIONAL one and reject every tap (ADR 0004 §9 rollback scenario,
//     M3-04 card). A wrong-typed CONTEXT value (a server bug — context values are
//     not write-time validated) is handled the same fail-safe way.
//
// Document VALUE shapes are guaranteed by validate.go (validateOperatorValue), so
// a failed type assertion on a doc value here is defence-in-depth, reported as a
// bad-value anomaly, never a panic.

import "net/netip"

// evalCondition reports whether cond holds for ctx (AND across all predicates).
// A nil/empty condition holds unconditionally (the loop body never runs). Every
// anomaly encountered is reported via report; any anomaly (or any normal
// non-match) sets the result to false.
func evalCondition(sid string, layer Layer, cond Condition, ctx Context, report func(Anomaly)) bool {
	matched := true
	for _, op := range sortedOperators(cond) {
		if !op.Valid() {
			report(Anomaly{Kind: AnomalyUnknownOperator, Layer: layer, Sid: sid, Operator: op})
			matched = false
			continue
		}
		args := cond[op]
		for _, key := range sortedKeys(args) {
			if !key.Valid() {
				report(Anomaly{Kind: AnomalyUnknownKey, Layer: layer, Sid: sid, Operator: op, Key: key})
				matched = false
				continue
			}
			ok, bad := evalOperator(op, key, args[key], ctx)
			switch {
			case bad:
				report(Anomaly{Kind: AnomalyBadContextValue, Layer: layer, Sid: sid, Operator: op, Key: key})
				matched = false
			case !ok:
				matched = false
			}
		}
	}
	return matched
}

// evalOperator evaluates ONE predicate (op on key, compared to docVal) against
// ctx. It returns:
//
//	match=true            the predicate holds
//	match=false, bad=false a NORMAL non-match — includes an absent key for every
//	                       operator but Exists (missing key != false)
//	bad=true              the predicate is unevaluable because the CONTEXT value
//	                       is the wrong type (server bug); fail-safe non-match,
//	                       reported as an anomaly by evalCondition
//
// op is already known-valid (evalCondition checked). docVal shapes are guaranteed
// by write-time validation; a failed assertion on docVal returns bad=true.
func evalOperator(op Operator, key ContextKey, docVal any, ctx Context) (match, bad bool) {
	// Exists is the ONLY operator defined on an absent key (ADR 0004 §8; IAM's
	// Null operator): true = key must be present, false = key must be absent.
	if op == OpExists {
		want, ok := docVal.(bool)
		if !ok {
			return false, true
		}
		_, present := ctx.Keys[key]
		return present == want, false
	}

	ctxVal, present := ctx.Keys[key]
	if !present {
		// Missing key is not false: every non-Exists operator does not match.
		return false, false
	}

	switch op {
	case OpStringEquals, OpStringNotEquals:
		want, ok := docVal.(string)
		if !ok {
			return false, true
		}
		got, ok := ctxVal.(string)
		if !ok {
			return false, true
		}
		eq := got == want
		if op == OpStringEquals {
			return eq, false
		}
		return !eq, false

	case OpStringIn:
		arr, ok := docVal.([]any)
		if !ok {
			return false, true
		}
		got, ok := ctxVal.(string)
		if !ok {
			return false, true
		}
		for _, el := range arr {
			if s, ok := el.(string); ok && s == got {
				return true, false
			}
		}
		return false, false

	case OpBool:
		want, ok := docVal.(bool)
		if !ok {
			return false, true
		}
		got, ok := ctxVal.(bool)
		if !ok {
			return false, true
		}
		return got == want, false

	case OpNumericEquals, OpNumericLessThan, OpNumericGreaterThan:
		want, ok := docVal.(float64)
		if !ok {
			return false, true
		}
		got, ok := toFloat(ctxVal)
		if !ok {
			return false, true
		}
		switch op {
		case OpNumericEquals:
			return got == want, false
		case OpNumericLessThan:
			return got < want, false
		default: // OpNumericGreaterThan
			return got > want, false
		}

	case OpIpInPrefix:
		arr, ok := docVal.([]any)
		if !ok {
			return false, true
		}
		addr, ok := toAddr(ctxVal)
		if !ok {
			return false, true
		}
		for _, el := range arr {
			s, ok := el.(string)
			if !ok {
				continue
			}
			pfx, err := netip.ParsePrefix(s)
			if err != nil {
				continue // validated at write time; skip defensively if not parseable now
			}
			if pfx.Contains(addr) {
				return true, false
			}
		}
		return false, false

	case OpTimeBetween:
		arr, ok := docVal.([]any)
		if !ok || len(arr) != 2 {
			return false, true
		}
		start, ok1 := toFloat(arr[0])
		end, ok2 := toFloat(arr[1])
		if !ok1 || !ok2 {
			return false, true
		}
		got, ok := toFloat(ctxVal)
		if !ok {
			return false, true
		}
		return timeBetween(got, start, end), false

	default:
		// Unreachable: evalCondition rejects unknown operators before calling.
		// Kept so a future operator added without a case here fails safe (no
		// match) rather than silently matching.
		return false, true
	}
}

// toFloat converts a context number to float64. Context values are produced by
// server code, which may use any integer or float width, so all are accepted;
// non-numeric values return ok=false (a bad-value anomaly upstream). JSON-decoded
// document numbers are already float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// toAddr converts a context IP to a netip.Addr. It accepts a netip.Addr directly
// or a parseable string; anything else (or an unparseable string) returns
// ok=false. The result is Unmap()'d so a 4-in-6 address compares against v4
// prefixes as expected.
func toAddr(v any) (netip.Addr, bool) {
	switch a := v.(type) {
	case netip.Addr:
		return a.Unmap(), true
	case string:
		addr, err := netip.ParseAddr(a)
		if err != nil {
			return netip.Addr{}, false
		}
		return addr.Unmap(), true
	default:
		return netip.Addr{}, false
	}
}

// timeBetween reports whether v lies in the INCLUSIVE window [start, end], with
// WRAPAROUND when start > end — a night shift (ADR 0004 §8, validate.go's
// TimeBetween note): [18, 2] covers 18..23 and 0..2, so a 02:00 checkout on a
// Rusty Bar 18:00-02:00 shift matches (CLAUDE.md §5). Both ends are inclusive.
func timeBetween(v, start, end float64) bool {
	if start <= end {
		return v >= start && v <= end
	}
	return v >= start || v <= end
}
