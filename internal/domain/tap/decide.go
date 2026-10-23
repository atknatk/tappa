package tap

import (
	"math"
	"net/netip"
	"time"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/netx"
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
//   - Type (direction, M4-04, ADR 0032 §A.1): closes the person's last open
//     check-in only within its business day and StaleOpenIn, else a new `in`;
//     BusinessDayStart and LeftOpen come with it.
//   - MinutesLate (M4-05): lateness against the resolved shift; report only.
//   - Trust (M4-06): 20 + 50(IP) + 30(GPS); INDEPENDENT of the verdict.
//   - Practice: ALWAYS false since ADR 0026 — the activating NFC tap replaced the
//     M4-06 training tap. Input still carries no client practice flag, so nothing
//     can claim one either.
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
		// normal chain movement, NOTED by base:cross-location-note (a note, which
		// decides nothing -- ADR 0029) and RECORDED for the report, never penalised. It needs BOTH the home location
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
			// ADR 0027 §7: the peer fact travels with the real tenant pair and only with
			// it -- beside the presence placeholder there is no pair to be peers.
			ctx.SameHolding = in.SameHolding
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
		Note: pd.Reason,
		// The employee's screen gets the rule's sentence and, below, the direction's
		// annotation -- never the notes (types.go EmployeeNote).
		EmployeeNote: pd.Reason,
		Security:     pd.SecurityAlert != "", // §5 row 4 / lost-tag: a manager push
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
	// The engine's NOTES (ADR 0029) follow the deciding rule's sentence, in the
	// engine's order: "<rule>; <note>…". They are annotations of the record, never
	// its reason -- MatchedSid and the verdict stay the deciding rule's.
	for _, n := range pd.Notes {
		dec.Note = appendNote(dec.Note, n.Reason)
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
	// is resolved against the person's last OPEN check-in within this tap's
	// BUSINESS DAY and StaleOpenIn (ADR 0032 §A.1) — never the calendar day.
	if dec.Verdict == VerdictOK || dec.Verdict == VerdictFlag {
		r := resolveDirection(in)
		dec.Type = &r.dir
		dec.BusinessDayStart = r.dayStart
		dec.LeftOpen = r.leftOpen
		// Two languages for one fact (ADR 0032 §B.4): the record's note is for the
		// manager and the report, the screen's for the person at the plaque.
		dec.Note = appendNote(dec.Note, r.note)
		dec.EmployeeNote = appendNote(dec.EmployeeNote, r.employeeNote)
		// NO PRACTICE TAP ANY MORE (ADR 0026). The first tap after activation used
		// to be a TRAINING record that never counted toward hours (M4-06); the
		// activation itself now happens on a physical NFC tap, which IS the
		// training moment and writes no attendance row at all. So every record
		// this engine decides is an ordinary one and dec.Practice stays false.
		// The field and the transactions.practice column stay: historic rows are
		// immutable (§4.3) and keep being read as practice (resolveDirection,
		// ADR 0008).
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

// StaleOpenIn is the longest a check-in may stay OPEN and still be closed by a tap
// (§5, ADR 0032 §A.1): a tap more than this after the open check-in does NOT close
// it — it is an `in`, and the old entry stays open as a "checkout missing" anomaly
// the manager corrects by hand (§5: "the system produces NO checkout"). Exactly
// StaleOpenIn still closes ("> 18 h is stale").
//
// 🔴 UNTIL ADR 0032 IT WAS ONLY A NOTE, AND THAT IS THE DEFECT M15 FIXES. A tap past
// it was still resolved to `out` and merely annotated, so ONE forgotten checkout
// inverted the person's chain for good: Tuesday's 09:05 arrival closed Monday's
// entry, Tuesday's 17:40 departure became an `in`, Wednesday's arrival closed that
// — and the report paid the nights (measured, ADR 0032 "Ölçülen kusur"). The rule
// now holds with or without a business day: Input.DayCutoff nil still applies it.
//
// 🔴 IT IS EXPORTED AND THAT IS WHAT KEEPS ONE THRESHOLD RATHER THAN TWO. The reports
// section (M6-07, internal/domain/ledger) answers the same question from the other
// side and ClassifyCheckout below draws the (a) line with it; migration 00064's
// tappa_checkout_class spells it as a literal, which M15-05's parity test binds to
// this constant.
//
// WHY 18h, and why a FIXED duration rather than the resolved Shift length:
//   - No plausible single continuous work session reaches 18h. A double shift with
//     overtime and breaks stays under it, and the Rusty Bar overnight shift
//     (18:00->02:00, ~8h) sits comfortably inside — so a legitimate long night is
//     never split, while a check-in left open across a day is.
//   - It must also work when Input.Shift and Input.DayCutoff are nil (every caller
//     before M15-04). A fixed span is pure arithmetic on UTC instants (§6).
const StaleOpenIn = 18 * time.Hour

// staleOpenInNote is the annotation the engine wrote, BEFORE ADR 0032, on an `out`
// that closed a check-in older than StaleOpenIn. Nothing writes it any more; it stays
// because historic records carry it (immutable, §4.3) and their readers must still
// recognise it.
const staleOpenInNote = "stale open check-in (possible forgotten checkout)"

// StaleOpenInNote is staleOpenInNote for a reader that has to tell the direction's annotation
// apart from the deciding rule's sentence in a stored note (appendNote joins them with "; "):
// internal/domain/ledger keeps it on a record at a partner venue whose rule sentence it replaces
// (M11 API-1c, K-39). Historic records only since ADR 0032 — see LeftOpenNote.
const StaleOpenInNote = staleOpenInNote

// LeftOpenNote is the RECORD's annotation (manager, report) on an `in` that left the
// person's previous check-in open (ADR 0032 §A.1, §B.4). It names an anomaly, carries
// no secret and no coordinate (§4.7), and is appended to — never replaces — the
// deciding rule's sentence. Exported for the same reader as StaleOpenInNote.
const LeftOpenNote = "previous check-in left open (no checkout recorded)"

// LeftOpenEmployeeNote is the same fact in the words the person at the plaque reads
// (Decision.EmployeeNote, ADR 0029's split; ADR 0032 §B.4, Q33's "clock in / clock
// out" vocabulary). It follows the rule's sentence after "; " like every
// annotation, so it is lower-case and has no full stop of its own.
const LeftOpenEmployeeNote = "your previous clock-in has no clock-out"

// direction is resolveDirection's answer: the direction, the business day it was
// judged in, the check-in it left open, and that fact's two notes.
type direction struct {
	dir          Type
	dayStart     *time.Time
	leftOpen     *LeftOpen
	note         string
	employeeNote string
}

// resolveDirection decides whether this tap is a check-IN or check-OUT — ADR 0032
// §A.1's table, in its order:
//
//	no open check-in (or a practice one)          -> in
//	open.OccurredAt > at (an out-of-order claim)  -> in
//	open.OccurredAt < S(at)                       -> in, previous check-in left open
//	at − open.OccurredAt > StaleOpenIn            -> in, previous check-in left open
//	otherwise                                     -> out (closes it)
//
// (The ADR lists the 18 h row before the S row; both give `in` and the same note, so
// the order changes only LeftOpen.Reason — previous business day when both hold, the
// classification's a ∧ b → b.)
//
// `at` IS THE TAP'S INSTANT, tapInstant(in) — the declared occurred_at when the
// client stated one, the server's Now otherwise (ADR 0032 §A.4). Until ADR 0032 the
// 18 h comparison read in.Now, so an offline-queued Monday 17:00 checkout arriving
// on Tuesday was measured from Tuesday. GetLastOpenTransaction is bounded by the
// same instant (@at), so `open` is the check-in open AT the tap.
//
// THE BUSINESS DAY IS NOT A CALENDAR DAY. S(at) is the latest cutoff wall clock at
// or before `at` (BusinessDayStart), so with a 05:00 cutoff the Rusty Bar 02:10 exit
// is in the business day that started at 05:00 the previous morning and closes the
// 18:05 entry. No cutoff (Input.DayCutoff nil, or unresolvable) leaves StaleOpenIn
// alone — the engine never invents 05:00. All arithmetic is on UTC instants (§6);
// the wall clock enters only through time.Date(…, loc) inside BusinessDayStart.
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
// Since ADR 0026 no NEW record is practice, so the only practice rows this can meet
// are historic ones; they were always `in` (ADR 0008).
//
// EVERY `out` THIS RETURNS IS A CHECKOUT ClassifyCheckout CALLS "closes" when it is
// read back with the open check-in as its predecessor and this business day as its
// own — the engine never writes a (b) or an (a) (ADR 0032 §A.6); a test pins it.
func resolveDirection(in Input) direction {
	at := tapInstant(in)
	r := direction{dir: TypeIn}
	if in.DayCutoff != nil {
		if s, ok := BusinessDayStart(at, *in.DayCutoff); ok {
			r.dayStart = &s
		}
	}
	open := in.LastOpenIn
	if open == nil || open.Practice || open.OccurredAt.After(at) {
		return r
	}
	switch {
	case r.dayStart != nil && open.OccurredAt.Before(*r.dayStart):
		r.leftOpen = &LeftOpen{ID: open.ID, Reason: LeftOpenPreviousBusinessDay}
	case at.Sub(open.OccurredAt) > StaleOpenIn:
		r.leftOpen = &LeftOpen{ID: open.ID, Reason: LeftOpenStale}
	default:
		r.dir = TypeOut
		return r
	}
	r.note, r.employeeNote = LeftOpenNote, LeftOpenEmployeeNote
	return r
}

// BusinessDayStart is S(at): the instant the business day containing `at` started
// (ADR 0032 §A.3), in UTC. ok is false when the cutoff is outside [0, 24h) or the
// zone is empty or cannot be loaded — the caller then has no business day, never a
// guessed one (an empty zone would load as UTC; Malta is not UTC).
//
// THE ONE REPRESENTATION. The checkout classification, the claim window (§C.3), the
// settings warning (§A.7) and the GET /t prediction (§B.2) all need this boundary;
// each computing its own would be one copy per reader, and over DST copies drift.
//
// HOW (ADR 0032 §A.3). For `at`'s local date d, three candidates — the cutoff's wall
// clock on d − 1, d and d + 1 — and S is the LATEST candidate not after `at`. The
// shape is forced by measurement rather than taste:
//
//   - ONE candidate ("d's, or d − 1's if that is after at") fails where a zone skips
//     MIDNIGHT. America/Santiago and America/Havana spring forward at 00:00, Go
//     normalises the missing 00:00 BACKWARD to 23:00 of the previous date, and the
//     day's cutoff then lies on the wrong side of `at`: with a 00:00 cutoff and
//     at = 2025-09-06 23:00 −04, S came out 09-06 00:00 and Next 09-06 23:00 — `at`
//     not inside its own day; 3366 S(S) ≠ S and 90 at ≥ Next over 2025–2027
//     (measured, M15 engine lens; the instant is pinned in
//     TestDecide_BusinessDayPinsTheMeasuredInstants). d + 1's candidate is the one
//     that IS 23:00 there.
//   - Each candidate's DATE IS NORMALISED FIRST (wallClockOn): d − 1 may be day 0 of
//     a month, and comparing wall clocks against an unnormalised date fails the
//     repeated-hour search — measured by the M15 engine lens on its algorithm:
//     Malta, 02:00 cutoff, S(S) ≠ S 114 times. Here the comparison is against a wall
//     clock built by time.Date (which normalises it too); a mutation comparing with
//     the raw date fails the property test.
//   - A wall clock that happens TWICE (autumn) is resolved explicitly to the EARLIER
//     instant (wallClockOn); Go's time.Date picks the later one, and the boundary
//     instant belongs to the new day, so the earlier one is where that day starts.
//   - A wall clock that does NOT happen (spring, e.g. 02:30 in Malta) is the instant
//     time.Date normalises it to; tests pin the result.
func BusinessDayStart(at time.Time, c DayCutoff) (time.Time, bool) {
	loc, ok := cutoffZone(c)
	if !ok {
		return time.Time{}, false
	}
	return businessDayStartIn(at, c.Cutoff, loc)
}

// NextBusinessDayStart is Next(S): the instant the business day starting at `start`
// ends — S(start + 26 h) (ADR 0032 §A.3), NOT start's local date plus one day. A
// business day is 22–26 h long across every zone the property test sweeps; 26 h past
// a start is therefore always inside the following day and never past its end. Its
// ok is BusinessDayStart's.
func NextBusinessDayStart(start time.Time, c DayCutoff) (time.Time, bool) {
	loc, ok := cutoffZone(c)
	if !ok {
		return time.Time{}, false
	}
	return nextBusinessDayStartIn(start, c.Cutoff, loc)
}

// nextBusinessDayStartIn is NextBusinessDayStart on a loaded zone.
func nextBusinessDayStartIn(start time.Time, cutoff time.Duration, loc *time.Location) (time.Time, bool) {
	return businessDayStartIn(start.Add(26*time.Hour), cutoff, loc)
}

// cutoffZone validates c and loads its zone. LoadLocation runs per call, as it does
// for lateness: no package-level cache (CLAUDE.md §7), and tzdata is embedded.
func cutoffZone(c DayCutoff) (*time.Location, bool) {
	if c.Cutoff < 0 || c.Cutoff >= 24*time.Hour || c.Timezone == "" {
		return nil, false
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, false
	}
	return loc, true
}

// businessDayStartIn is BusinessDayStart on a loaded zone (the property test drives
// it directly so the sweep does not load a zone per point).
func businessDayStartIn(at time.Time, cutoff time.Duration, loc *time.Location) (time.Time, bool) {
	y, m, d := at.In(loc).Date()
	var s time.Time
	found := false
	for k := -1; k <= 1; k++ {
		if c := wallClockOn(y, m, d+k, cutoff, loc); !c.After(at) && (!found || c.After(s)) {
			s, found = c, true
		}
	}
	return s.UTC(), found
}

// wallClockOn is the instant of the wall clock `cutoff` on the local date (y, m, d)
// in loc — the date normalised first, a repeated wall clock resolved to its EARLIER
// instant.
//
// THE EARLIER INSTANT, EXPLICITLY. time.Date returns one instant for a wall clock
// that happens twice and the Go documentation does not promise which (measured: the
// later). The two instants straddle the start of t's zone period: the earlier one
// lies in the PREVIOUS period, at wall − (that period's offset). So when t's period
// began less than 12 h before t (a DST step is at most 2 h — Antarctica/Troll — and
// transitions are months apart), that instant is computed and taken if its own wall
// clock really is the target. A wall clock in a spring GAP has no instant at all; no
// candidate passes the check and t — time.Date's normalisation — stands.
func wallClockOn(y int, m time.Month, d int, cutoff time.Duration, loc *time.Location) time.Time {
	y, m, d = time.Date(y, m, d, 12, 0, 0, 0, loc).Date()
	hh := int(cutoff / time.Hour)
	mm := int(cutoff % time.Hour / time.Minute)
	ss := int(cutoff % time.Minute / time.Second)
	ns := int(cutoff % time.Second)
	t := time.Date(y, m, d, hh, mm, ss, ns, loc)
	if start, _ := t.ZoneBounds(); !start.IsZero() && t.Sub(start) < 12*time.Hour {
		_, before := start.Add(-time.Nanosecond).Zone()
		wall := time.Date(y, m, d, hh, mm, ss, ns, time.UTC) // the wall clock read as UTC
		if u := wall.Add(-time.Duration(before) * time.Second); u.Before(start) && sameWall(u.In(loc), wall) {
			return u
		}
	}
	return t
}

// sameWall reports whether local's wall clock (date and time of day) is wall's,
// read as UTC.
func sameWall(local, wall time.Time) bool {
	ly, lm, ld := local.Date()
	wy, wm, wd := wall.Date()
	return ly == wy && lm == wm && ld == wd && localTimeOfDay(local) == localTimeOfDay(wall)
}

// ClassifyCheckout is ADR 0032 §A.6's classification of one `out`: which of the six
// disjoint classes it is in, given the event before it (prev — P and W), whether
// its business day has an `in` before it (dayHasIn — I), its instant and its frozen
// business day (outDayStart, nil on rows without one: manual, and before M15).
// "Closes" is one of its classes, not a second predicate.
//
// IT IS THE GO HALF OF ONE RULE WHOSE SQL HALF IS tappa_checkout_class (migration
// 00064), and it mirrors that function decision for decision, so the two give one
// answer (M15-05's parity test binds them):
//
//	no instant                                         -> "" (the SQL's NULL)
//	prev is not an in
//	    no business day                                -> unanchored (d)
//	    prev is an out ∧ dayHasIn ∧ prev.WrittenLater  -> extra      (e)
//	    otherwise                                      -> orphan     (c)
//	prev is an in
//	    business day set ∧ prev.At < its start         -> other_day  (b)   (a ∧ b → b)
//	    outAt − prev.At > StaleOpenIn                  -> too_long   (a)
//	    otherwise                                      -> closes
//
// Both closing bounds are INCLUSIVE: an `in` exactly at the business day's start is
// the new day's (the boundary instant belongs to the new day, §A.2), and an out
// exactly StaleOpenIn after its `in` closes it. A Prev whose direction is neither in
// nor out counts as "not an out" — (c) or (d), the reporting side, like the SQL's
// NULL boolean.
//
// The engine does not call it — its question is "should this tap close", answered
// by resolveDirection — but every `out` the engine writes classifies as closes.
func ClassifyCheckout(prev *Prev, dayHasIn bool, outAt time.Time, outDayStart *time.Time) CheckoutClass {
	if outAt.IsZero() {
		return ""
	}
	if prev == nil || prev.Direction != TypeIn {
		switch {
		case outDayStart == nil:
			return CheckoutUnanchored
		case prev != nil && prev.Direction == TypeOut && dayHasIn && prev.WrittenLater:
			return CheckoutExtra
		default:
			return CheckoutOrphan
		}
	}
	switch {
	case outDayStart != nil && prev.At.Before(*outDayStart):
		return CheckoutOtherDay
	case outAt.Sub(prev.At) > StaleOpenIn:
		return CheckoutTooLong
	default:
		return CheckoutCloses
	}
}

// appendNote joins a policy-derived note and a direction-derived one with "; ",
// keeping either alone when the other is empty. It preserves the deciding rule's
// human "why" (Decision.Note) while adding the direction's annotation (ADR 0032's
// left-open note).
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

// TrustBase is §5's baseline confidence: what a record scores when NEITHER piece of
// "where" evidence is present.
//
// 🔴 IT IS EXPORTED SO THAT THE SECOND WRITER OF `transactions` DOES NOT SPELL 20
// AGAIN (M6-08). A manager-entered record has no IP match and no GPS fix to have
// matched — there was no request from the person at all — so its score is exactly
// this number, and internal/domain/manual asks for it rather than carrying a literal.
// One representation: change the baseline here and every writer moves together.
const TrustBase = 20

// trustScore is §5's confidence score: a TrustBase baseline plus 50 for network
// proof of place (an IP match) and 30 for GPS proof, giving exactly 20/50/70/100
// (M4-06). It is a PURE function of the two "where" facts and is INDEPENDENT of the
// verdict — it measures EVIDENCE, not outcome, so it is deliberately NOT derived
// from the verdict (M4-06 trap): a flagged tap can score 70 and a GPS-only ok 50.
func trustScore(ipMatch, gpsMatch bool) int {
	score := TrustBase
	if ipMatch {
		score += 50
	}
	if gpsMatch {
		score += 30
	}
	return score
}

// ipMatches reports whether src falls inside any of the location's registered
// static IP ranges — the "where" evidence (§5). An invalid/zero src (no client IP
// resolved) never matches. src is Unmap()'d so a 4-in-6 address compares against
// v4 prefixes as expected (mirrors policy's toAddr).
//
// 🔴 A STORED RANGE TOO WIDE TO TELL THIS VENUE APART IS NOT EVIDENCE, AND THIS IS
// THE READING HALF OF THAT REFUSAL (backlog T40, M8-04). netx.TooWideForProofOfPlace
// stops such a list being SAVED, but a row written before that guard existed keeps
// producing `ip_match=t, trust=70` and the note "network proof of place" for every
// tap on earth — and transactions are IMMUTABLE (§4.3), so those rows can never be
// corrected. A write guard alone therefore leaves the past open forever, which is
// why the check is also here, where the evidence is READ.
//
// 🔴 IT IS THE SAME FUNCTION, NOT A SECOND PREDICATE, and that is the repair this
// round shipped. The earlier version carried its own rule — skip an entry whose
// prefix has no bits — which closed the single "0.0.0.0/0" spelling and left the
// UNION spelling ("0.0.0.0/1" plus "128.0.0.0/1") reading as a match. Measured on
// the development database while that hole was open: a QR tap from a venue holding
// only those two entries, with no GPS at all, was recorded `verdict=ok
// ip_match=TRUE trust=70` — so the union did not merely manufacture proof, it
// bought a pass around base:qr-requires-ip, which §5 states as "QR: IP is required,
// GPS alone is not enough → flag". Two predicates drift; there is now one, and the
// rule it states is exact: WHAT THE WRITE SIDE WOULD REFUSE TODAY IS NOT EVIDENCE.
//
// 🔴 AND THE SHARED PREDICATE IS A WIDTH RULE NOW, WHICH CHANGED WHAT THIS FUNCTION
// READS. Two further spellings survived a coverage-based version of it — the eight
// prefixes covering everything but 25.0.0.0/8, and the twenty-four covering
// everything but RFC 5737's 192.0.2.0/24 — and both were read here as a match for a
// source anywhere on earth, on QR as well as NFC. The limit and its derivation are on
// netx.TooWideForProofOfPlace; what matters at this call site is that a list wide
// enough to match essentially everybody now answers "no proof" whether or not it
// happens to leave some block out.
//
// ⚠️ THE RULE IS THE WHOLE LIST'S, NOT THE ENTRY'S, AND ONE ROW CHANGED VERDICT
// BECAUSE OF IT. A venue holding "203.0.113.0/24" beside a "0.0.0.0/0" used to
// yield a match for a source inside the /24 (the /0 was skipped, the /24 was left
// to stand on its own merits). That list is REFUSED at save time today, so the
// reading side now answers "no proof" for it as well. The change is fail-closed —
// such a tap falls to GPS, or to §5 row 7 (flag), and a record is written either
// way (§4.6) — and it is the price of having one rule instead of two.
//
// 🔴 THIS PACKAGE STILL IMPORTS NO STORAGE. internal/netx is prefix arithmetic and
// nothing else (net/netip + math/big, no DB, no HTTP, no clock), which is why the
// shared predicate lives there rather than in internal/domain/tenant: §3 requires
// this package to know nothing about storage, and it still does not.
//
// Measured on the development database (2026-08-20 — a DATED observation that grows
// with every audit run, not a bound): 48 venues of 332 855 carry a list this guard
// refuses, all of them residue from this card's own mutation and audit runs. 11
// transactions were recorded at those venues and 4 of them carry ip_match = TRUE.
// Those four are precisely what this half of the guard is for: they say "network
// proof of place", they were written while the hole was open, and §4.3 means no
// correction is possible. The full figures and the query live on
// netx.TooWideForProofOfPlace.
func ipMatches(src netip.Addr, prefixes []netip.Prefix) bool {
	if !src.IsValid() {
		return false
	}
	// "No proof" is the truthful answer when the stored list cannot tell this venue
	// apart from anywhere else, and it is not a rejection: the tap falls back to
	// GPS or lands on §5 row 7 (flag), and a record is written either way (§4.6).
	if _, tooWide := netx.TooWideForProofOfPlace(prefixes); tooWide {
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
