package tap

import (
	"math"
	"net/netip"
	"time"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/google/uuid"
)

// markSessionPresent is a NON-NIL placeholder that marks "a session exists" in
// the policy Context for a caller that supplies no real session tenant (see
// Decide). Its BYTES ARE NEVER COMPARED, and that is enforced by the ONLY branch
// that installs it: it is used exclusively when Input.SessionTenantID is nil, and
// on that branch Decide also withholds TagTenantID — so sys:tenant-mismatch, the
// one guardrail that reads a SessionTenantID for its VALUE, cannot see it.
// sys:no-session reads only its nil-ness. A FIXED value (not a fresh random one
// per call) keeps Decide deterministic, preserving the purity proof.
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
// BEYOND THE VERDICT. On top of the M4-03 verdict mapping, Decide fills the
// arithmetic/report fields the later tasks own, each a PURE computation, never a
// policy decision and never a client claim:
//   - Type (direction, M4-04): a toggle against the person's last open check-in.
//   - MinutesLate (M4-05): lateness against the resolved shift; report only.
//   - Trust (M4-06): 20 + 50(IP) + 30(GPS); INDEPENDENT of the verdict.
//   - Practice (M4-06): the first record after activation, SERVER-derived from
//     Employee.ActivatedAt + LastForPerson — Input carries no client practice flag,
//     which is what closes the hours-inflation exploit (see isPracticeTap).
//
// The QR channel needs no code here: sys:sun-invalid is NFC-only, so a SUN-less QR
// tap is not denied, and base:qr-requires-ip (Q15) sends a QR tap without an IP
// match to review — both resolved by the policy Set, proven end to end in the tests.
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
	// The declared-time facts (§5 / M5-05 K1). skew = Now - OccurredAt in seconds:
	// positive means the tap claims to have happened in the PAST, negative means
	// the FUTURE. sys:occurred-at-bound denies both a future stamp and one past
	// the tenant's tolerance, and it can only do that if the key is PRESENT — a
	// missing key never matches (M3-04's invariant), which is how a guardrail goes
	// silently dead. So it is set on EVERY tap, including the ordinary live one
	// where OccurredAt is the zero value and the skew is therefore exactly 0.
	skewSeconds, queued := occurredAtFacts(in)
	keys := map[policy.ContextKey]any{
		policy.CtxTapChannel:               string(in.Channel),
		policy.CtxTapSunValid:              in.SUN.Valid,
		policy.CtxTapCtrGap:                in.SUN.CtrGap, // base:ctr-gap-review (Q21); toFloat handles int
		policy.CtxTapIPMatch:               ipMatch,
		policy.CtxTapGPSMatch:              gpsMatch,
		policy.CtxTapGPSConflict:           gpsConflict,
		policy.CtxTapOccurredAtSkewSeconds: skewSeconds,
		policy.CtxTapQueued:                queued,
	}
	// The page's age, for sys:tap-freshness (guardrail #6). Set ONLY when there is
	// a page behind this tap: a manual record has none, and a zero PageIssuedAt
	// read literally would report a fifty-six-year-old page and deny it. Absent is
	// the honest value there — and it is safe, because the guardrail is NFC-only
	// and a manual record is not NFC.
	if !in.PageIssuedAt.IsZero() {
		keys[policy.CtxTapPageAgeSeconds] = in.Now.Sub(in.PageIssuedAt).Seconds()
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
	// Session presence for sys:no-session (§5 line 3) AND the tenant pair for
	// sys:tenant-mismatch (§4.5, hand-off N5). tap's session-presence signal is
	// Employee (types.go: Employee==nil => no session), which becomes the Context's
	// SessionTenantID nil-ness.
	//
	// TWO SHAPES, and the split is deliberate rather than transitional:
	//
	//   - The caller supplied a REAL session tenant (M5-05 always does). Both ids
	//     go into the Context and sys:tenant-mismatch is live: a session for one
	//     organisation tapping another's plaque redirects and writes nothing, so
	//     the tap lands in NEITHER tenant.
	//   - The caller supplied none (every M4 caller, and any test that only cares
	//     that a session exists). Then presence is signalled with the value-less
	//     placeholder and the TAG tenant is deliberately WITHHELD from the Context,
	//     because comparing a real tenant id against a placeholder would produce a
	//     mismatch that is not one — and a mismatch REDIRECTS, which writes no
	//     record (§4.6). Withholding keeps the guardrail inert instead, which is
	//     exactly the pre-N5 behaviour and cannot lose a tap.
	//
	// The second shape is a hazard in one direction only: a caller that forgets
	// SessionTenantID silently loses the isolation check rather than gaining a
	// spurious one. That is why M5-05's service REFUSES a request without a
	// session tenant, and why a mutation test drives a cross-tenant tap end to end.
	if in.Employee != nil {
		if in.SessionTenantID != nil {
			ctx.SessionTenantID = in.SessionTenantID
			ctx.TagTenantID = in.TagTenantID
		} else {
			ctx.SessionTenantID = &markSessionPresent
		}
	}
	// Per-person debounce FACT for sys:person-debounce (§5 line 5): seconds since
	// THIS PERSON's last tap. The WINDOW (60 s, tenant-tunable) is applied by the
	// guardrail from in.PolicySet's Params — person-based, not tag-based, so
	// different people can tap one plaque back-to-back and each is recorded. nil
	// (no previous tap) never debounces — the safe zero value (§4.6).
	if gap := debounceGap(in); gap != nil {
		ctx.SecondsSincePersonLastTap = gap
	}

	// --- 3. Delegate to the policy engine (ONE call, no early return) --------
	pd := policy.Evaluate(in.PolicySet, ctx)

	// --- 4. Apply the returned effect --------------------------------------
	dec := Decision{
		IPMatch:  ipMatch,
		GPSMatch: gpsMatch,
		// Confidence score (M4-06): a PURE function of the two "where" facts, set
		// here BEFORE the verdict switch precisely because it is INDEPENDENT of the
		// verdict — a flagged tap can carry 70 (IP matched but a GPS conflict sent it
		// to review) and a GPS-only ok carries 50. Never derived from dec.Verdict.
		Trust: trustScore(ipMatch, gpsMatch),
		// The docket's human-facing "why" comes straight from the deciding rule.
		Note:     pd.Reason,
		Security: pd.SecurityAlert != "", // §5 row 4 / lost-tag: a manager push
		// M4-05 report fact, independent of which policy won the tiebreak (see the
		// CrossLocation doc): recorded so the report shows cross-location taps apart.
		CrossLocation: crossLocation,
		// Carried so the immutable record can explain itself forever (M3-07,
		// migration 0008: matched_sid / policy_layer / policy_version_id), and the
		// frozen INPUT snapshot beside them (policy_context) so the reason can be
		// replayed rather than merely named.
		MatchedSid:      pd.MatchedSid,
		Layer:           pd.Layer,
		PolicyVersionID: pd.PolicyVersionID,
		PolicyContext:   keys,
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
		// Practice tap (M4-06). Only a RECORDED attendance tap (ok/flag — the same
		// gate as direction) can be a TRAINING tap: a reject/ignored/redirect has no
		// worked hours to exclude. The value is SERVER-derived (isPracticeTap), so a
		// client cannot claim it; a checkout is structurally never practice.
		dec.Practice = isPracticeTap(in)
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

// occurredAtFacts reduces the one unverified client input to the two facts the
// policy layer reads: how far the declared tap time sits from the server's now,
// in seconds, and whether this tap arrived from an offline queue.
//
// skew = Now - OccurredAt. POSITIVE is the past ("this happened an hour ago"),
// NEGATIVE is the future ("this happens in an hour"), and sys:occurred-at-bound
// denies a negative outright — a tap cannot have happened later than the moment
// the server is judging it, so a future stamp is a forged one or a broken clock,
// and neither is worth a recorded `ok`.
//
// A ZERO OccurredAt IS "NOW", NOT 1970. Reading the zero value literally would
// make every caller that never set the field produce a fifty-six-year skew and a
// denied tap — the mirror image of the missing-key problem, and a worse one,
// since it would turn a live tap into a reject. Substituting Now yields a skew of
// exactly 0, which is what a live tap's skew IS.
//
// QUEUED IS DERIVED, NEVER DECLARED (ADR 0004 §8; the M5-05 criterion). It is
// true when the CALLER stated its own occurred_at and that time is genuinely in
// the past — the shape only an offline client produces, since a live tap sends no
// timestamp at all. No threshold is applied here on purpose: how much lateness is
// suspicious is base:queued-window's judgement (a tenant-tunable 120 s) and how
// much is intolerable is sys:occurred-at-bound's, and duplicating either number
// in this function would put a policy threshold in two places.
//
// ⚠️ tap:queued IS NOT transactions.queued. They are different questions wearing
// one word: the CONTEXT KEY means "did this tap come from an offline queue", the
// COLUMN means "is this record in the approval queue" (migration 00005 says so
// where it defines the column: flag -> true). The write path sets the column from
// the verdict, not from this.
func occurredAtFacts(in Input) (skewSeconds float64, queued bool) {
	// 🔴 THE TEST IS "DID THE CALLER DECLARE ONE", NEVER "IS THE VALUE ZERO".
	// This used to read `if in.OccurredAt.IsZero()`, and an audit measured what a
	// sentinel VALUE costs when it is also a legal input:
	//
	//	occurred_at=0001-01-01T00:00:00Z  ->  ok, and the record stored the SERVER's
	//	                                      clock instead of the declared time
	//	occurred_at=0001-01-01T00:00:01Z  ->  reject / sys:occurred-at-bound
	//
	// One second apart, opposite outcomes, and the first silently replaced a time
	// the caller had stated. The harm ran toward the attacker (a denied tap became
	// an honest one) and the record was still written, so nothing was lost — but a
	// guard that a caller can land on by writing a valid timestamp is a guard, not
	// a zero value, and the boundary belongs on an explicit flag.
	//
	// A tap the SERVER timed is by construction happening now — the caller passes
	// its own clock reading — so its skew is 0 and no guardrail can fire on it.
	if !in.OccurredAtFromClient {
		return 0, false
	}
	skewSeconds = in.Now.Sub(in.OccurredAt).Seconds()
	return skewSeconds, skewSeconds > 0
}

// lateness reports how many minutes late a CHECK-IN is against the employee's
// resolved shift, or nil when it is not computed. It is a REPORT output and NEVER
// changes the verdict (§5, M4-05): a three-hours-late tap is still ok. It returns
// nil when there is no shift, when dir is not a check-IN (a check-OUT is not
// "late" — this is what keeps the Rusty Bar 02:00 exit out of the late column), or
// when the shift's timezone cannot be resolved. A positive value is minutes late;
// <= 0 means on time or early. Duration/integer minutes only, so no float touches
// a time/attendance figure (§6); a pointer so "not computed" is distinct from "0".
// 🔴 IT MEASURES THE TAP, NOT THE RECORDING, AND IT DID NOT UNTIL M6-07. This
// function read in.Now — the moment the SERVER was deciding — instead of when the
// tap says it happened. For a live tap the two are the same instant, which is why
// nothing caught it for two milestones: lateness_test.go never sets OccurredAt.
// For the two shapes where they differ it answered a different question entirely.
// Measured end to end, through the real service, before the fix: a manager's manual
// entry declaring 10:17 against a 10:00 shift reported -520 minutes — "eight hours
// early" — because the server clock stood at 01:20 the next morning
// (internal/handler/day_db_test.go, which pinned the wrong answer on purpose so
// this change would be loud).
//
// WHY IT WAS SAFE TO CHANGE HERE RATHER THAN AT M4-05, measured rather than
// assumed: MinutesLate reaches NO column (there is no minutes_late), NO screen (no
// template has a field for it), NO log line and NO policy_context key
// (policy.CtxTimeMinutesLate is declared and never set, so no rule can read it).
// Until M6-07 the value existed only inside a Decision that nothing persisted, so
// there was no stored figure for a corrected one to contradict — which §4.3 would
// have forbidden fixing by back-filling.
func lateness(in Input, dir *Type) *int {
	if in.Shift == nil || dir == nil || *dir != TypeIn {
		return nil
	}
	return MinutesLate(tapInstant(in), *in.Shift)
}

// tapInstant is WHEN THE TAP HAPPENED, which is not always when the server is
// judging it (Input.OccurredAt vs Input.Now).
//
// THE TEST IS THE EXPLICIT FLAG, NEVER "is the value zero", for the reason
// occurredAtFacts spells out at length: a caller that declared nothing leaves
// OccurredAt at the zero value, and reading that literally would measure lateness
// against the year 1.
func tapInstant(in Input) time.Time {
	if in.OccurredAtFromClient {
		return in.OccurredAt
	}
	return in.Now
}

// MinutesLate reports how many minutes late a check-in at `at` is against shift s,
// or nil when the shift's timezone cannot be resolved. A positive value is minutes
// late; <= 0 means on time or early.
//
// 🔴 IT IS EXPORTED FOR THE REPORTS SECTION (M6-07) AND THAT IS WHAT KEEPS THE
// PRODUCT TO ONE ANSWER. There is no minutes_late column and §4.3 forbids adding
// one retrospectively, so the panel has to RECOMPUTE lateness from occurred_at and
// the resolved shift. Recomputing it with a second implementation is the "second
// representation" shape this repository has paid for repeatedly — and here the two
// copies would drift over DST, which is precisely the arithmetic
// shiftStartInstant exists to get right. internal/domain/ledger calls this one.
//
// It stays PURE (no clock, no DB — the package's whole testability argument) and it
// stays integer: Duration and int minutes only, so no float touches an attendance
// figure (§6).
func MinutesLate(at time.Time, s Shift) *int {
	start, ok := shiftStartInstant(at, s)
	if !ok {
		return nil // unknown/empty timezone: do not guess a wall clock (M4-05 trap)
	}
	// Signed minutes, truncated toward zero: N full minutes after the shift start.
	// Both operands are UTC instants (§6) — no local time enters the arithmetic;
	// the wall-clock/DST resolution happened in shiftStartInstant.
	m := int(at.Sub(start) / time.Minute)
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
func shiftStartInstant(at time.Time, s Shift) (time.Time, bool) {
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
	local := at.In(loc)
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

// StaleOpenIn is how long a check-in may stay OPEN before this engine treats a
// closing tap as a probable FORGOTTEN CHECKOUT (§5, M4-04). Beyond it the tap is
// still resolved to out — never silently in — but annotated (staleOpenInNote) so
// the manager's anomaly report surfaces it for a manual correction (§5: "the
// system produces NO checkout; the open record is listed as an anomaly").
//
// 🔴 IT IS EXPORTED FOR M6-07 AND THAT IS WHAT KEEPS ONE THRESHOLD RATHER THAN TWO.
// The reports section has to answer the same question from the other side — "is this
// unclosed check-in still somebody's shift, or is it an anomaly a manager must
// correct?" — and it also needs a horizon for how far past a reporting period to
// look for the closing tap. Both are this quantity. A private copy in
// internal/domain/ledger would be a second representation of a number whose whole
// job is to make the engine and the report agree about one record.
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
const StaleOpenIn = 18 * time.Hour

// staleOpenInNote is the docket/report annotation for a closing tap whose open
// check-in is older than StaleOpenIn. It names an anomaly, carries no
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
// LastOpenIn (types.go Transaction.Practice).
//
// 🔴 THAT SENTENCE WAS FALSE FOR THE WHOLE OF M4 AND M5, AND THE COST WAS A §5
// VIOLATION (ADR 0008, M5-11). GetLastOpenTransaction was practice-NEUTRAL; the
// exclusion lived in checkin's gather, which discarded a practice row WITHOUT
// LOOKING BENEATH IT. A practice row that merely sorted newest hid a real open
// check-in, so the checkout came out as an `in` and the entry never closed —
// silently, over plain HTTP. The query now carries `AND NOT t.practice`, which is
// what makes the sentence above true rather than aspirational. It is written down
// here because the comment, not the code, was the defect: a guard described as
// "primary" that nothing implemented.
//
// We ALSO defend in depth here: a practice LastOpenIn is treated as no open in
// (-> in), so a caller bug cannot keep a real check-in "open" behind a training tap
// (the M4-06 exploit that would inflate hours). MEASURED LIMIT, not a claim: because
// the production query can no longer return such a row, this branch is unreachable
// through checkin and only a hand-built Input reaches it — which is exactly what
// TestDecide_DirectionPracticeOpenInDoesNotCloseChain does, and all it can prove.
//
// This is only about a PRIOR practice record's effect on the chain; whether
// the CURRENT tap is itself practice is derived in M4-06 (Decision.Practice, from
// Employee.ActivatedAt) — a practice tap still gets a direction computed here and is
// still recorded, it simply must never later reappear as a LastOpenIn. That
// direction is ALWAYS `in` and it is a structural fact, not a convention: see
// isPracticeTap.
func resolveDirection(in Input) (dir Type, note string) {
	open := in.LastOpenIn
	if open == nil || open.Practice {
		return TypeIn, ""
	}
	// An open check-in exists -> close it. Toggle, not calendar day.
	dir = TypeOut
	if in.Now.Sub(open.OccurredAt) > StaleOpenIn {
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

// debounceGap is how long ago this person's previous tap happened, for
// sys:person-debounce (§5 row 5). It returns nil when there is nothing to
// measure against, which never debounces (§4.6's safe zero value).
//
// IT IS THE SMALLER OF TWO DISTANCES, and ADR 0006 is why there are two:
//
//	now − LastForPerson.OccurredAt   what the previous record CLAIMS.
//	*SecondsSinceLastRecordedTap     how long ago the SERVER wrote this person's
//	                                 last tap, measured on the DATABASE clock.
//
// 🔴 THE HOLE THIS CLOSES HAS THREE LAYERS, and the first fix only reached one of
// them. All three were measured end to end, against real Postgres, through the
// mounted router:
//
//	DISTANCE   occurred_at arrives in the POST body. A repeat declaring a time in
//	           the past reported an enormous gap, so the guardrail never fired:
//	           20 posts of ONE scanned QR context -> 20 counted rows, 0.51 s.
//	SELECTION  GetLastTransactionForEmployee ordered by occurred_at, so a caller
//	           also chose WHICH row was the predecessor. Declaring a time UNDER
//	           the person's existing newest row makes every new row sort beneath
//	           it: the predecessor never advances and both legs keep measuring one
//	           untouched old record. Measured against an employee with ordinary
//	           history: 20 posts -> 20 counted rows, 0.31 s. Adding a created_at
//	           column to that row fixed the distance and left this untouched.
//	SIGN       sys:occurred-at-bound REFUSES a future stamp but §4.6 still RECORDS
//	           the row, which then wins ORDER BY occurred_at forever and makes the
//	           declared distance NEGATIVE. The guardrail matches on
//	           `gap >= 0 && gap < window`, so a negative gap disabled it entirely:
//	           ONE future-dated POST, then 20 honest taps declaring nothing ->
//	           20 `ok` rows, 0.29 s. Not even flagged; no manager ever sees it.
//
// So the server leg is read from a SEPARATE, SERVER-ORDERED fact
// (SecondsSinceLastRecordedTap: the age of the newest nfc/qr row, computed in SQL)
// rather than off
// whichever row the declared ordering happened to surface. That single change
// answers SELECTION, and it is what makes the whole rule hold: the declared leg
// may be steered, but it can only ever make the gap SMALLER (min), never larger.
//
// A NEGATIVE DECLARED DISTANCE IS DROPPED, NOT CLAMPED, and the choice is
// deliberate in both directions. Dropping is fail-closed against the ATTACKER:
// they gain nothing, because the server leg still answers. Clamping it to zero
// would have been fail-closed against the USER instead — one future-dated row
// would debounce that person's every real tap until the future caught up, which
// turns a self-inflicted mistake into hours they cannot claim. A claim about the
// future is not evidence of nearness; it is simply not evidence, so it is not
// counted.
//
// THE MANUAL EXEMPTION LIVES IN THE QUERY (channel IN ('nfc','qr')) rather than
// here. created_at on a manual row is when a MANAGER TYPED IT, which says nothing
// about where the employee was: counting it would swallow an employee's genuine
// tap thirty seconds after a manager's backdated entry, and would break bulk
// manual entry. Expressing it as a predicate keeps it out of reach — channel is
// server-derived, so no caller can produce a manual predecessor for itself.
func debounceGap(in Input) *float64 {
	gap := math.Inf(1)
	if in.LastForPerson != nil {
		// Dropped when negative: see the sign paragraph above.
		if declared := in.Now.Sub(in.LastForPerson.OccurredAt).Seconds(); declared >= 0 {
			gap = declared
		}
	}
	if in.SecondsSinceLastRecordedTap != nil {
		// Already an age, computed end to end on the database clock, so nothing
		// here mixes clocks. Still guarded: a fixture may seed a row stamped in the
		// future, and a negative age is not proximity.
		if recorded := *in.SecondsSinceLastRecordedTap; recorded >= 0 && recorded < gap {
			gap = recorded
		}
	}
	if math.IsInf(gap, 1) {
		return nil
	}
	return &gap
}

// trustScore is §5's confidence score: a 20-point baseline plus 50 for network
// proof of place (an IP match) and 30 for GPS proof, giving exactly 20/50/70/100
// (M4-06). It is a PURE function of the two "where" facts and is INDEPENDENT of the
// verdict — it measures EVIDENCE, not outcome, so it is deliberately NOT derived
// from the verdict (M4-06 trap): a flagged tap can score 70 and a GPS-only ok 50.
func trustScore(ipMatch, gpsMatch bool) int {
	score := 20
	if ipMatch {
		score += 50
	}
	if gpsMatch {
		score += 30
	}
	return score
}

// isPracticeTap reports whether this tap is the person's FIRST RECORD EVER — the
// §5/M4-06 practice (TRAINING) tap that must never count toward worked hours. It is
// derived ENTIRELY on the server from two facts:
//
//   - the employee has a known activation time (Employee.ActivatedAt is not the
//     zero value), and
//   - the person has NO prior tap (Input.LastForPerson == nil).
//
// ⚠️ "FIRST AFTER ACTIVATION" AND "FIRST EVER" ARE NOT THE SAME SENTENCE, and this
// function means the second one. Somebody who loses their phone and RE-ACTIVATES on
// a new one does NOT get a second training tap: their next record is an ordinary
// one. MEASURED end to end (M5-11) — one record, then a real second activation over
// HTTP (which lands on /activate/done, the second-device path), then another record:
// practice=true, then practice=FALSE. Two independent reasons, either alone enough:
// LastForPerson is non-nil, and activated_at is not even moved by the second
// activation (ConsumeInviteAndActivate COALESCEs it, db/queries/invites.sql).
//
// 🔴 PRACTICE IMPLIES DIRECTION `in`, STRUCTURALLY. This function requires
// LastOpenIn == nil, and resolveDirection returns TypeOut only when LastOpenIn is a
// non-practice row — so a practice record can never be an `out`. Both are computed
// from the same Input in the same branch of Decide, so the implication holds for
// every tap, not by convention. It is what lets GetLastOpenTransaction's NOT EXISTS
// stay practice-neutral while its outer filter excludes practice (ADR 0008), and it
// is pinned by TestDecide_PracticeIsAlwaysAnIn.
//
// It reads NO client-supplied practice flag — Input carries none BY DESIGN
// (types.go), which is what closes the M4-06 hours-inflation exploit: a client that
// could set practice=true on a CHECKOUT would keep the check-in open and over-
// report hours. Here a checkout is STRUCTURALLY never practice, because a checkout
// necessarily has a prior tap, so LastForPerson != nil. A nil Employee (no session)
// is never practice — there is no record to mark. This is a plain BOOLEAN FACT, not
// a separate "training mode" state (M4-06 trap).
//
// DEFENSE IN DEPTH (M4-07, hardening carried over from M4-06 — state.md session
// note): it ALSO requires LastOpenIn == nil, mirroring resolveDirection's stale-
// practice guard. A CONSISTENT M5 query never yields the shape "LastForPerson == nil
// yet LastOpenIn != nil" (an open check-in IS a prior tap for that person), so the
// LastForPerson check already covers the real world. But an INCONSISTENT caller with
// that shape would otherwise mark a checkout (LastOpenIn present -> resolveDirection
// returns out) as practice, re-opening the exact hours-inflation exploit; requiring
// LastOpenIn == nil keeps isPracticeTap and resolveDirection in agreement so a
// checkout can never be practice regardless of which of the two prior-tap signals
// the caller happens to pass.
func isPracticeTap(in Input) bool {
	return in.Employee != nil &&
		!in.Employee.ActivatedAt.IsZero() &&
		in.LastForPerson == nil &&
		in.LastOpenIn == nil
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
