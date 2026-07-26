package tap

import (
	"net/netip"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/google/uuid"
)

// markSessionPresent is a NON-NIL placeholder that marks "a session exists" in the
// policy Context (see Decide). Its BYTES ARE NEVER COMPARED: the only guardrail
// that reads a non-nil SessionTenantID for its VALUE is sys:tenant-mismatch, and
// that guardrail also requires TagTenantID to be non-nil — which Decide leaves nil
// (tag/session tenant ids are a later M5 Input, see the tenant-mismatch deferral
// in Decide). sys:no-session reads only its nil-ness. A FIXED value (not a fresh
// random one per call) keeps Decide deterministic, preserving the purity proof.
var markSessionPresent = uuid.UUID{0: 0x01}

// Decide turns one tap's evidence (Input) into a single explainable Decision. It
// is the tap engine's ONLY entry point and it is PURE: a deterministic function of
// its Input, with no clock, no randomness, no DB and no HTTP (see the types.go
// package doc). It writes NO record — a Decision only describes what should happen;
// persisting it (and the §4.6 write-and-flag path) is the M5 caller's job.
//
// HOW IT WORKS (M4-03). Decide does THREE things and no more:
//
//  1. Computes the FACTS that are arithmetic, not policy: does the source IP fall
//     in a registered range (ipMatch), is the GPS within the radius (gpsMatch),
//     how far is it (gpsDistanceM), and is GPS present-but-far (gpsConflict). These
//     are §5's "four evidences" reduced to booleans/numbers.
//  2. Builds a policy.Context from those facts plus the raw evidence, then calls
//     policy.Evaluate(in.PolicySet, ctx) EXACTLY ONCE. Every §5 row is a policy
//     rule: rows 1-5 are guardrails, rows 6-7 the baseline. Decide DELEGATES the
//     decision — it does NOT re-implement the §5 order and NEVER short-circuits
//     with its own "if tag lost -> reject" / "if deactivated -> reject" / "if
//     debounce -> ignore" before Evaluate. Doing so would re-open the R8 exploits
//     the guardrail ORDER defends against (info leak, replay window) by letting
//     Decide's order win over policy's; the order is M3-05's slice, full stop.
//  3. Maps the returned effect to a Verdict: allow->ok, review->flag, deny->reject,
//     ignore->ignored. The one non-recording effect, redirect (sys:no-session, §5
//     line 3), becomes Redirect=RedirectActivation with NO verdict — the single
//     path that writes nothing, because there is no session and so no person to
//     record against (§4.6's sole exception). Every OTHER path yields a recorded
//     verdict, and the no-match fallback for tap:record is review->flag, so a tap
//     is NEVER silently approved and NEVER silently dropped (§4.6).
//
// SCOPE (M4-03 only). Type (direction, M4-04), Trust (M4-06), lateness (M4-05) and
// the Practice/QR-channel derivations (M4-06) are NOT computed here — those fields
// take their zero value and the later tasks extend Decide. This task resolves §5
// rows 1-7 end to end; the arithmetic/report fields come next.
//
// The signature is fixed (M4-02): Decide takes ONE Input value, so the per-tenant
// policy Set travels IN the Input (in.PolicySet), assembled by the M5 caller
// (guardrails + baseline + tenant policies, with the real append-only version ids).
// See the M4-02 card correction for why a field, not a second parameter.
func Decide(in Input) Decision {
	// --- 1. Facts (arithmetic, not policy) -----------------------------------
	ipMatch := ipMatches(in.SourceIP, in.LocationIPs)
	gpsMatch, gpsConflict, gpsDistanceM, haveGPS := gpsFacts(in.GPS, in.LocationGPS, in.GPSRadiusM)

	// --- 2. Build the policy Context -----------------------------------------
	// Keys are the closed document vocabulary (document.go). A key that a guardrail
	// or baseline reads MUST be present or the rule silently does not fire (M3-04:
	// "missing key != false"); a key nothing reads is a harmless recorded input.
	keys := map[policy.ContextKey]any{
		policy.CtxTapChannel:     string(in.Channel),
		policy.CtxTapSunValid:    in.SUN.Valid,
		policy.CtxTapCtrGap:      in.SUN.CtrGap, // base:ctr-gap-review (Q21); toFloat handles int
		policy.CtxTapIPMatch:     ipMatch,
		policy.CtxTapGPSMatch:    gpsMatch,
		policy.CtxTapGPSConflict: gpsConflict,
	}
	if haveGPS {
		// A DISTANCE in metres, never the raw coordinate (§4.7) — this is exactly
		// what makes policy_context (migration 0008) a safe frozen snapshot.
		keys[policy.CtxTapGPSDistanceM] = gpsDistanceM
	}
	if in.Tag != nil {
		keys[policy.CtxTagStatus] = string(in.Tag.Status) // sys:tag-not-active (§5 row 1)
		keys[policy.CtxTagLocationID] = in.Tag.Location.String()
		keys[policy.CtxLocationID] = in.Tag.Location.String() // the TAPPED location (§5/M4-05)
	}
	if in.Employee != nil {
		keys[policy.CtxEmployeeStatus] = string(in.Employee.Status) // sys:employee-deactivated (§5 row 4)
		if in.Employee.Location != nil {
			keys[policy.CtxEmployeeLocationID] = in.Employee.Location.String()
		}
		if in.Employee.Department != nil {
			keys[policy.CtxEmployeeDepartmentID] = in.Employee.Department.String()
		}
	}

	ctx := policy.Context{
		// Every tap is a tap:record request scoped to the TAPPED location, so a
		// tenant can override policy at one branch (location/<id> beats the baseline
		// location/*, ADR 0004 §7 — the Rusty Bar exception).
		Action:    policy.ActionTapRecord,
		Resources: tapResources(in.Tag),
		Keys:      keys,
	}
	// Session presence for sys:no-session (§5 line 3). tap's session-presence
	// signal is Employee (types.go: Employee==nil => no session), which we translate
	// into the Context's SessionTenantID nil-ness. Input carries no real tenant id
	// yet (a later M5 field feeding sys:tenant-mismatch), so a non-nil session uses
	// the value-less placeholder markSessionPresent — see its doc for why the value
	// is never inspected.
	if in.Employee != nil {
		ctx.SessionTenantID = &markSessionPresent
	}
	// Per-person debounce FACT for sys:person-debounce (§5 line 5): seconds since
	// THIS PERSON's last tap. The WINDOW (60 s, tenant-tunable) is applied by the
	// guardrail from in.PolicySet's Params — person-based, not tag-based, so
	// different people can tap one plaque back-to-back and each is recorded. nil
	// (no previous tap) never debounces — the safe zero value (§4.6).
	if in.LastForPerson != nil {
		gap := in.Now.Sub(in.LastForPerson.OccurredAt).Seconds()
		ctx.SecondsSincePersonLastTap = &gap
	}

	// --- 3. Delegate to the policy engine (ONE call, no early return) --------
	pd := policy.Evaluate(in.PolicySet, ctx)

	// --- 4. Apply the returned effect --------------------------------------
	dec := Decision{
		IPMatch:  ipMatch,
		GPSMatch: gpsMatch,
		// The docket's human-facing "why" comes straight from the deciding rule.
		Note:     pd.Reason,
		Security: pd.SecurityAlert != "", // §5 row 4 / lost-tag: a manager push
		// Carried so the immutable record can explain itself forever (M3-07,
		// migration 0008: matched_sid / policy_layer / policy_version_id).
		MatchedSid:      pd.MatchedSid,
		Layer:           pd.Layer,
		PolicyVersionID: pd.PolicyVersionID,
	}
	switch pd.Effect {
	case policy.EffectAllow:
		dec.Verdict = VerdictOK
	case policy.EffectReview:
		dec.Verdict = VerdictFlag
	case policy.EffectDeny:
		dec.Verdict = VerdictReject
	case policy.EffectIgnore:
		dec.Verdict = VerdictIgnored
	case policy.EffectRedirect:
		// §5 line 3 (and sys:tenant-mismatch): NO record is written. Verdict is left
		// empty on purpose — the caller keys off Redirect, and a redirect is the
		// ABSENCE of a record, not an ok/flag/reject/ignored one.
		dec.Redirect = RedirectActivation
	default:
		// Unreachable: Evaluate returns only the five effects above. Fail SAFE to
		// review (record + queue) rather than drop or silently approve (§4.6).
		dec.Verdict = VerdictFlag
	}
	return dec
}

// ipMatches reports whether src falls inside any of the location's registered
// static IP ranges — the "where" evidence (§5). An invalid/zero src (no client IP
// resolved) never matches. src is Unmap()'d so a 4-in-6 address compares against
// v4 prefixes as expected (mirrors policy's toAddr).
func ipMatches(src netip.Addr, prefixes []netip.Prefix) bool {
	if !src.IsValid() {
		return false
	}
	addr := src.Unmap()
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// gpsFacts reduces the backup "where" evidence to the booleans/number policy reads.
// It returns have=false when either coordinate is absent (no GPS was read, or the
// location has none registered): then match/conflict are false and no distance is
// meaningful. When both are present: match is within the radius (§5 row 6, strict
// "< radius" per geo.WithinRadius), and conflict is its complement — GPS present
// but OUTSIDE the radius (base:gps-conflict-review, Y-E: an on-site proxy shows a
// far GPS while the IP still matches). distanceM is a metre distance, never a raw
// coordinate (§4.7).
func gpsFacts(gps, locGPS *geo.Point, radiusM float64) (match, conflict bool, distanceM float64, have bool) {
	if gps == nil || locGPS == nil {
		return false, false, 0, false
	}
	distanceM = geo.Distance(*gps, *locGPS)
	match = geo.WithinRadius(*gps, *locGPS, radiusM)
	return match, !match, distanceM, true
}

// tapResources is the concrete resource a tap targets: the TAPPED location
// (Tag.Location), as "location/<id>". A nil Tag (a caller contract violation —
// types.go says Tag is always present) yields no resource, so no baseline matches
// and the request falls to the fail-to-review default rather than panicking.
func tapResources(tag *Tag) []string {
	if tag == nil {
		return nil
	}
	return []string{"location/" + tag.Location.String()}
}
