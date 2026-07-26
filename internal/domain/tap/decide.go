package tap

import (
	"net/netip"
	"time"

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
	var crossLocation bool
	if in.Employee != nil {
		keys[policy.CtxEmployeeStatus] = string(in.Employee.Status) // sys:employee-deactivated (§5 row 4)
		if in.Employee.Location != nil {
			keys[policy.CtxEmployeeLocationID] = in.Employee.Location.String()
		}
		if in.Employee.Department != nil {
			keys[policy.CtxEmployeeDepartmentID] = in.Employee.Department.String()
		}
		// Cross-location FACT (Q17, M4-05), computed BEFORE Evaluate so the baseline
		// sees it: a tap at a location OTHER than the employee's home location is
		// normal chain movement, affirmed by base:cross-location-note (allow) and
		// RECORDED for the report, never penalised. It needs BOTH the home location
		// (Employee.Location) and the tapped location (Tag.Location); with either
		// absent there is no "other" location to be away from, so it is not cross-
		// location (false). The verdict is unaffected — the tap is judged on the
		// TAPPED location's evidence and shift (Input.Shift), so a cross-location
		// worker is never spuriously "late" for being away from home.
		crossLocation = in.Employee.Location != nil && in.Tag != nil &&
			*in.Employee.Location != in.Tag.Location
		keys[policy.CtxEmployeeCrossLocation] = crossLocation
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
		// M4-05 report fact, independent of which policy won the tiebreak (see the
		// CrossLocation doc): recorded so the report shows cross-location taps apart.
		CrossLocation: crossLocation,
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

	// --- 5. Direction (M4-04) ------------------------------------------------
	// Only records that JOIN the in/out chain carry a direction: ok and flag. A
	// flagged tap is still a real record queued for approval (§4.6) and becomes
	// part of the worked-hours chain once approved, so it needs a direction too.
	// reject, ignored and the no-session redirect carry NONE — Type stays nil (a
	// pointer so "no direction" is unambiguous, types.go Decision.Type). Direction
	// is a pure TOGGLE against the person's last OPEN check-in, never calendar day.
	if dec.Verdict == VerdictOK || dec.Verdict == VerdictFlag {
		dir, note := resolveDirection(in)
		dec.Type = &dir
		dec.Note = appendNote(dec.Note, note)
	}

	// --- 6. Lateness (M4-05) -------------------------------------------------
	// A REPORT output only: how many minutes late this CHECK-IN is against the
	// employee's resolved shift (Input.Shift). It NEVER touches dec.Verdict — a
	// late tap stays ok (§5). Computed AFTER direction because only a check-IN can
	// be "late" (a checkout is not), and only ok/flag records carry a direction at
	// all (reject/ignored/redirect keep Type nil -> MinutesLate stays nil).
	dec.MinutesLate = lateness(in, dec.Type)

	return dec
}

// lateness reports how many minutes late a CHECK-IN is against the employee's
// resolved shift, or nil when it is not computed. It is a REPORT output and NEVER
// changes the verdict (§5, M4-05): a three-hours-late tap is still ok. It returns
// nil when there is no shift, when dir is not a check-IN (a check-OUT is not
// "late" — this is what keeps the Rusty Bar 02:00 exit out of the late column), or
// when the shift's timezone cannot be resolved. A positive value is minutes late;
// <= 0 means on time or early. Duration/integer minutes only, so no float touches
// a time/attendance figure (§6); a pointer so "not computed" is distinct from "0".
func lateness(in Input, dir *Type) *int {
	if in.Shift == nil || dir == nil || *dir != TypeIn {
		return nil
	}
	start, ok := shiftStartInstant(in.Now, *in.Shift)
	if !ok {
		return nil // unknown/empty timezone: do not guess a wall clock (M4-05 trap)
	}
	// Signed minutes, truncated toward zero: N full minutes after the shift start.
	// Both operands are UTC instants (§6) — no local time enters the arithmetic;
	// the wall-clock/DST resolution happened in shiftStartInstant.
	m := int(in.Now.Sub(start) / time.Minute)
	return &m
}

// shiftStartInstant returns the UTC instant of the shift's START for the tap's
// local day. ok is false when the zone name is empty or unknown — the caller then
// declines to compute lateness rather than silently measure against UTC.
//
// WHY wall-clock components, NOT midnight + Duration. A shift start is a WALL time
// (Q01: "10:00–22:00 is a wall clock, not an absolute instant"). Building it with
// time.Date(y, m, d, hh, mm, ss, ns, loc) lets the ZONE place it, so the Malta DST
// transitions are correct: on the March spring-forward day (02:00->03:00) a 09:00
// local start is 07:00 UTC (UTC+2), while in winter it is 08:00 UTC (UTC+1); on
// the October fall-back day (03:00->02:00) the zone likewise resolves the repeated
// hour. Adding the Start Duration to a local/UTC midnight instead would measure
// ABSOLUTE hours and land an hour off across a transition (that day is 23 or 25 h
// long) — the exact bug types.go's Shift doc warns against.
//
// For an OVERNIGHT shift (Rusty Bar 18:00->02:00) a tap in the after-midnight tail
// (local time-of-day < End, e.g. 01:00 < 02:00) belongs to the shift that STARTED
// THE PREVIOUS DAY, so the start date is rolled back one day; time.Date normalises
// the day underflow (month/year and DST included).
func shiftStartInstant(now time.Time, s Shift) (time.Time, bool) {
	// An empty zone is treated as unresolvable, NOT as UTC: time.LoadLocation("")
	// returns UTC without error, which would silently compute lateness against the
	// wrong wall clock (Malta is UTC+1/+2). Q01 guarantees a non-empty zone in
	// production; this guards a caller bug rather than trusting it.
	if s.Timezone == "" {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(loc)
	y, mo, d := local.Date()
	if s.Overnight && localTimeOfDay(local) < s.End {
		d-- // the after-midnight tail belongs to the previous day's shift start
	}
	return time.Date(y, mo, d,
		int(s.Start/time.Hour),
		int((s.Start%time.Hour)/time.Minute),
		int((s.Start%time.Minute)/time.Second),
		int(s.Start%time.Second),
		loc), true
}

// localTimeOfDay is t's wall-clock time of day as a Duration since local midnight
// — used only to place an overnight tap in either the evening (>= Start) or the
// after-midnight tail (< End) portion of its shift.
func localTimeOfDay(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour +
		time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second +
		time.Duration(t.Nanosecond())
}

// staleOpenInThreshold is how long a check-in may stay OPEN before this engine
// treats a closing tap as a probable FORGOTTEN CHECKOUT (§5, M4-04). Beyond it the
// tap is still resolved to out — never silently in — but annotated (staleOpenInNote)
// so the manager's anomaly report surfaces it for a manual correction (§5: "the
// system produces NO checkout; the open record is listed as an anomaly").
//
// WHY 18h, and why a FIXED duration rather than the resolved Shift length:
//   - No plausible single continuous work session reaches 18h. A double shift with
//     overtime and breaks stays under it, and the Rusty Bar overnight shift
//     (18:00->02:00, ~8h) sits comfortably inside — so a legitimate long night is
//     NEVER mis-flagged, while a check-in left open across a day is.
//   - Deriving the threshold from Input.Shift would need the timezone/DST wall-clock
//     resolution that belongs to M4-05 (types.go Shift doc); this M4-04 guard must
//     also work when Input.Shift is nil. A fixed span keeps direction resolution
//     pure arithmetic on UTC instants (§6) and out of M4-05's scope.
const staleOpenInThreshold = 18 * time.Hour

// staleOpenInNote is the docket/report annotation for a closing tap whose open
// check-in is older than staleOpenInThreshold. It names an anomaly, carries no
// secret and no raw coordinate (§4.7), and is appended to — never replaces — the
// deciding policy's reason.
const staleOpenInNote = "stale open check-in (possible forgotten checkout)"

// resolveDirection decides whether this tap is a check-IN or check-OUT by TOGGLING
// against the person's last OPEN check-in (Input.LastOpenIn) — the §5/M4-04 rule.
// It returns the direction and an optional note to append.
//
// Open check-in present -> this tap CLOSES it -> out. None -> in. That is the whole
// rule; there is deliberately NO "today's records" window. A calendar-day filter is
// the classic overnight-shift bug: it would refuse to pair the Rusty Bar 02:10 exit
// with the previous evening's 18:05 entry because they fall on different dates. The
// caller's GetLastOpenTransaction query is likewise date-unfiltered (occurred_at
// DESC, M5/M1-08); Decide must NOT assume a window either. All timing here is pure
// UTC arithmetic on Input.Now and Transaction.OccurredAt (both UTC, §6) — no local
// time, no time.Now(); conversion to wall-clock is M4-05's render concern only.
//
// PRACTICE records never hold the chain open (§5, M4-04). Primary enforcement is the
// caller's query, which excludes practice so a practice record is never passed as
// LastOpenIn (types.go Transaction.Practice). We ALSO defend in depth here: a
// practice LastOpenIn is treated as no open in (-> in), so a caller bug cannot keep
// a real check-in "open" behind a training tap (the M4-06 exploit that would inflate
// hours). This is only about a PRIOR practice record's effect on the chain; whether
// the CURRENT tap is itself practice is derived in M4-06 (Decision.Practice, from
// Employee.ActivatedAt) — a practice tap still gets a direction computed here and is
// still recorded, it simply must never later reappear as a LastOpenIn.
func resolveDirection(in Input) (dir Type, note string) {
	open := in.LastOpenIn
	if open == nil || open.Practice {
		return TypeIn, ""
	}
	// An open check-in exists -> close it. Toggle, not calendar day.
	dir = TypeOut
	if in.Now.Sub(open.OccurredAt) > staleOpenInThreshold {
		note = staleOpenInNote
	}
	return dir, note
}

// appendNote joins a policy-derived note and a direction-derived one with "; ",
// keeping either alone when the other is empty. It preserves the deciding rule's
// human "why" (Decision.Note) while adding M4-04's stale-open-in annotation.
func appendNote(existing, add string) string {
	switch {
	case add == "":
		return existing
	case existing == "":
		return add
	default:
		return existing + "; " + add
	}
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
