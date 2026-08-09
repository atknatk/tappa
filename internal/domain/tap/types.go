// Package tap is the tap decision engine's TYPE and SIGNATURE layer (M4-02). It
// fixes what Decide consumes (Input) and produces (Decision) — and, crucially,
// nails down Decide's PURITY so the whole §5 decision table can be proven with
// table-driven tests (M4-07) and replayed byte-for-byte later (M3-07).
//
// PURITY (CLAUDE.md §7, m4-tap-motoru.md M4-02, mirrors internal/sun and
// internal/policy). Decide is a pure function of its Input:
//
//   - NO clock: "now" arrives as Input.Now — the package NEVER calls time.Now().
//     This is what makes the overnight-shift tests deterministic (a test that read
//     the wall clock would break at midnight — M4-05/M4-07 traps).
//   - NO randomness (no math/rand), NO DB, NO HTTP. Every fact — SUN validity, the
//     tapped location's IPs/GPS, the person's last taps, the shift — is supplied by
//     the caller, already resolved on the SERVER (never a client-declared body
//     flag; see Channel and Employee.ActivatedAt below).
//   - It IMPORTS internal/geo and internal/policy (both pure — no DB/HTTP/clock)
//     for geo.Point and the policy Set/Context/Layer it delegates the decision to
//     (M4-03), but it does NOT import internal/sun, internal/store or internal/db:
//     pulling in sun would drag database/sql + pgx + store into this package's
//     dependency graph and destroy the purity proof. That is exactly why SUNResult
//     is tap's OWN domain type rather than sun.Result (see SUNResult) — the caller
//     maps the store/sun rows into these pure domain types at the M5 handler
//     boundary.
//
// Decide DOES NOT WRITE RECORDS. It returns a Decision — a description of what
// should happen — and touches no database. Persisting the tap (and honouring §4.6
// "records are never lost") is the caller's job in M5; the one path that writes
// NOTHING (the no-session redirect, §5 line 3) is expressed here as Redirect, not
// as a suppressed write.
//
// The Decide BODY is M4-03 (build the policy.Context, call policy.Evaluate, map
// the effect to a Verdict, compute direction/trust/lateness). M4-02 defines only
// the types and the signature; see decide.go.
package tap

import (
	"net/netip"
	"time"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Enums — every decision-relevant vocabulary is a TYPED string, never a bare
// string or int (m4-tap-motoru.md M4-02 trap: type-safe enums). The string
// values match the transactions table's CHECK vocabularies verbatim
// (migration 0005: type in|out, verdict ok|flag|reject|ignored, channel
// nfc|qr|manual) and the tags/employees status CHECKs (migrations 0004/0003), so
// the M5 store<->domain map is a straight assignment with no translation table.
// ---------------------------------------------------------------------------

// Channel is HOW the tap arrived. It is SERVER-DERIVED, never read from a client
// body (m4-tap-motoru.md M4-06 trap): nfc when the request carried a genuine
// ctr+cmac, qr for the SUN-less fallback, manual for a manager-entered record.
// A forged channel would let a QR tap masquerade as nfc and slip past
// base:qr-requires-ip, so the value here is the one the server derived from the
// presence of ctr/cmac — the type deliberately offers no place for a client claim.
type Channel string

const (
	ChannelNFC    Channel = "nfc"    // physical NTAG 424 DNA tap: ctr + cmac present
	ChannelQR     Channel = "qr"     // SUN-less fallback: no proof of moment (§5, base:qr-requires-ip)
	ChannelManual Channel = "manual" // manager-entered: entered_by set, SUN not sought
)

// Verdict is the tap's outcome — the value written to transactions.verdict. It is
// the Decision's headline field and maps 1:1 from the policy effect in M4-03
// (allow->ok, review->flag, deny->reject, ignore->ignored).
type Verdict string

const (
	VerdictOK      Verdict = "ok"      // counts toward worked hours (§5 line 6)
	VerdictFlag    Verdict = "flag"    // recorded AND queued for approval (§4.6, §5 line 7)
	VerdictReject  Verdict = "reject"  // refused (§5 lines 1,2,4)
	VerdictIgnored Verdict = "ignored" // per-person debounce swallow (§5 line 5)
)

// Type is a tap's direction, in or out. It is resolved by toggling against the
// person's LAST OPEN check-in — NOT by calendar day (§5, M4-04): an 18:00 shift's
// 02:00 exit must pair with that evening's entry. On a Decision it is a *Type
// because reject/ignored/redirect carry NO direction (see Decision.Type).
type Type string

const (
	TypeIn  Type = "in"
	TypeOut Type = "out"
)

// Redirect names a special NON-recording outcome — the one exception to "every
// tap is a record" (§5 line 3). Its zero value RedirectNone means "no redirect,
// this decision produces a normal record"; RedirectActivation tells the caller to
// serve the activation page and write NOTHING (there is no session, so there is no
// person to record against). Keeping this separate from Verdict is deliberate: a
// redirect is not an ok/flag/reject/ignored record, it is the absence of one.
type Redirect string

const (
	RedirectNone       Redirect = ""           // default: a real record is produced
	RedirectActivation Redirect = "activation" // no session -> activation page, no record (§5 line 3)
)

// TagStatus is the tapped plaque's lifecycle state (tags.status CHECK, migrations
// 00004 and 00013). EVERYTHING EXCEPT TagActive is a §5 line-1 reject — the rule is
// stated that way round on purpose; see below.
type TagStatus string

const (
	TagActive  TagStatus = "active"
	TagRetired TagStatus = "retired"
	TagLost    TagStatus = "lost"
	// TagUnassigned: encoded and loaded by Tappa, not yet mounted on a wall
	// (migration 00013's inventory model). A tap on one is refused like any other
	// non-active status.
	TagUnassigned TagStatus = "unassigned"
)

// 🔴 THIS VOCABULARY EXISTS IN THREE PLACES AND THAT IS DELIBERATE, BUT IT COST
// SOMETHING ONCE, SO THE ARRANGEMENT IS WRITTEN DOWN.
//
//	db/migrations/00004 + 00013   the CHECK constraint -- the SOURCE OF TRUTH
//	internal/policy/guardrails.go the value sys:tag-not-active compares against
//	here                          the typed value the decision engine carries
//
// They cannot be derived from one another at run time: policy deliberately does not
// import the store (it stays a pure, self-contained rule engine), and neither
// package may read migration files while serving a tap. So the copies are kept in
// step by DISCIPLINE at the source and by a TEST at the boundary --
// TestGuardrails_TagNotActiveIsAnAllowList derives the status list from
// db/migrations and fails if the guardrail's behaviour and the schema's vocabulary
// disagree.
//
// WHAT IT COST: 00013 added the fourth value and only the schema knew. The
// guardrail was enumerating the BAD statuses (retired, lost), so `unassigned`
// matched no rule and fell through §5 line 1 entirely. Both the guardrail and this
// list are now written so that the DEFAULT for an unknown value is refusal: the
// guardrail asks for `active` rather than listing its opposites, and a status this
// list has never heard of still arrives here as a plain string that is not
// TagActive. A fifth status is therefore refused on the day the migration adds it,
// with or without this constant being updated.

// EmployeeStatus is the tapping employee's lifecycle state (employees.status
// CHECK, migration 0003). deactivated is the §5 line-4 reject-with-alert. NOTE the
// value "active": an "invited" (not-yet-activated) employee is not a valid tapping
// session and is represented upstream as a nil Employee (no session), NOT as this
// status — see Input.Employee for why nil and deactivated are DIFFERENT decisions.
type EmployeeStatus string

const (
	EmployeeInvited     EmployeeStatus = "invited"
	EmployeeActive      EmployeeStatus = "active"
	EmployeeDeactivated EmployeeStatus = "deactivated"
)

// ---------------------------------------------------------------------------
// Evidence / fact sub-types — the minimal, PURE domain shapes the four evidences
// (SUN, session, IP, GPS) and the shift/history reduce to. Each carries ONLY the
// fields the decision needs (m4-tap-motoru.md M4-02: "each type carries only the
// fields it needs"); the caller maps richer store/sun rows into these at M5.
// ---------------------------------------------------------------------------

// SUNResult is what SUN verification learned, as a PURE fact. It is tap's OWN type
// and NOT sun.Result on purpose: sun.Result embeds db.ResolvedTag and pulls in
// internal/db + internal/store + pgx (database/sql), which would break this
// package's purity proof (go list -deps). The M5 caller maps sun.Result ->
// SUNResult: Valid <- sun.Result.SUNValid, CtrGap <- int(sun.Result.CtrGap). Like
// sun.Result it is a FACT, never a verdict — the decision engine maps it to a
// verdict (§5 line 2).
type SUNResult struct {
	// Valid is true ONLY when the chip's CMAC verified AND the monotonic counter
	// advanced (the SUN-invalid cases of §5 line 2 all collapse to false). It is
	// false for a QR fallback, a retired/lost tag, a bad CMAC and a replay.
	Valid bool
	// CtrGap is the number of chip reads skipped since we last saw this tag,
	// surfaced as DATA for the base:ctr-gap-review policy (M3-06, Q21) — tap applies
	// NO threshold, that judgment is policy's. Meaningful only when Valid is true.
	CtrGap int
}

// Tag is the tapped plaque, reduced to what the decision reads: its lifecycle
// status (§5 line 1) and the location it is mounted at. Location is the TAPPED
// location — the one whose static IP / GPS / shift the tap is matched against (§5,
// M4-05), never the employee's profile location.
type Tag struct {
	Status   TagStatus
	Location uuid.UUID
}

// Employee is the tapping session's employee, or nil.
//
// nil vs deactivated are DIFFERENT decisions (m4-tap-motoru.md M4-02 trap, §5):
//
//   - Input.Employee == nil       -> NO valid session (§5 line 3): serve the
//     activation page, write no record (RedirectActivation).
//   - Employee.Status==deactivated -> a real session on a disabled account
//     (§5 line 4): reject, log the attempt, raise a security alert.
//
// Collapsing these into one field (e.g. a bare "active bool") would make the two
// outcomes indistinguishable; the pointer answers "is there a session" and Status
// answers "what state is that session's account in". This type MAKES the M4-03
// distinction possible; it does not make the decision.
type Employee struct {
	// Status is the account lifecycle state (§5 line 4 reads deactivated).
	Status EmployeeStatus
	// Location is the employee's PROFILE (home) location, or nil if unassigned.
	// Compared against the tapped Tag.Location to detect a cross-location tap
	// (Q17, base:cross-location-note, M4-05) — a normal chain move, noted, never
	// penalised. This is the profile location, distinct from the tapped location.
	Location *uuid.UUID
	// Department is the employee's department, or nil. Shift resolution prefers the
	// department shift over the location shift (§5, Q17, M4-05); this field lets the
	// caller resolve which Shift to put in Input.Shift.
	Department *uuid.UUID
	// ActivatedAt is when the invite was activated, the SERVER-side source for the
	// practice flag (§5, M4-06): the first genuine tap after activation is a
	// practice tap. It lives here — not in Input as a client-supplied bool —
	// precisely so practice CANNOT be claimed by the request body (the M4-06
	// exploit: body practice=true to keep a real check-in open). Zero value means
	// "activation time unknown" and the M4-06 logic treats it accordingly.
	ActivatedAt time.Time
}

// Transaction is a prior tap reduced to what direction and debounce need. The
// caller supplies two of these in Input (LastForPerson, LastOpenIn); both may be
// nil. It carries no tenant/verdict/location — those are the caller's to persist,
// not the decision's to read.
type Transaction struct {
	// ID identifies the record this decision may pair with (e.g. the open check-in
	// an out closes), for the caller to reference when it writes.
	ID uuid.UUID
	// OccurredAt is when the prior tap CLAIMS to have happened (UTC, §6). It orders
	// the direction chain and supplies the DECLARED leg of the debounce gap.
	//
	// 🔴 IT IS CLIENT-DECLARABLE, and so is the CHOICE of which row lands here
	// (the query orders by this column). Neither the value nor the selection can
	// therefore carry the debounce on its own — see Input.SecondsSinceLastRecordedTap and
	// ADR 0006.
	OccurredAt time.Time
	// Direction is the prior tap's in/out.
	Direction Type
	// Practice marks a training tap; practice records DO NOT participate in the
	// direction chain (§5, M4-04) — the caller must not pass a practice record as
	// LastOpenIn, and this flag lets a check be asserted.
	Practice bool
}

// Shift is the employee's applicable shift, already RESOLVED by the caller
// (department shift preferred over location shift — §5, Q17, M4-05). nil means no
// shift is defined, so lateness is not computed. Lateness is a REPORT output and
// never changes the verdict (M4-05): a late tap is still ok.
type Shift struct {
	// Start and End are wall-clock TIMES OF DAY, encoded as the offset from local
	// midnight (e.g. 4h == 04:00). This mirrors the DB `time` columns
	// (locations/departments.shift_start/shift_end, migration 0002) which pgx yields
	// as microseconds-since-midnight. M4-05 combines these with Timezone and the
	// tap's date to build the shift's UTC instant — and MUST do so via wall-clock
	// components + time.Date(...loc), not by adding this Duration to a UTC midnight,
	// so the Malta DST transitions (March/October) resolve correctly.
	Start time.Duration
	End   time.Duration
	// Overnight is true when End is on the NEXT day (Rusty Bar 18:00->02:00,
	// migration 0002 overnight column): the shift crosses midnight.
	Overnight bool
	// Timezone is the IANA zone name the wall-clock times are expressed in (e.g.
	// "Europe/Malta"), resolved by the caller from the tenant/location (Q01). M4-05
	// loads it with time.LoadLocation; a sub-second/DST-safe conversion depends on
	// the real zone, never a fixed offset.
	Timezone string
}

// ---------------------------------------------------------------------------
// Input / Decision — Decide's contract.
// ---------------------------------------------------------------------------

// Input is everything Decide needs about one tap, all server-derived. Every field
// is an EXPLICIT, TYPED field — nothing is hidden behind interface{} (M4-02 trap:
// the explicit field list IS this engine's testability). The four evidences map
// as: SUN = proof of moment; Employee (the session) = who; SourceIP/LocationIPs =
// where; GPS/LocationGPS = backup where (§5).
type Input struct {
	// Now is the tap instant (UTC, §6). The caller supplies it — the package never
	// calls time.Now() — so overnight-shift and debounce tests are deterministic.
	Now time.Time

	// OccurredAt is when the tap CLAIMS to have happened (UTC, §6), as opposed to
	// Now, which is when the server is deciding it. The two differ only for a tap
	// that was recorded offline and posted later (the M9-01 queue).
	//
	// 🔴 IT IS THE ONE UNVERIFIED CLIENT INPUT ON THE WHOLE PATH, and it is the
	// most important field of an attendance record (M5-05 trap K1). None of the
	// four evidences carries a time: a SUN payload has no clock, an IP has no
	// clock, a GPS fix is not a timestamp and a session cookie is not one either.
	// So an employee who genuinely taps at 09:00 — real chip, matching IP — can
	// declare occurred_at=07:00 and claim two hours that never happened. The
	// ceiling on that is the sys:occurred-at-bound GUARDRAIL, which cannot be
	// switched off, and Decide feeds it by computing the skew below. The softer,
	// tenant-tunable review threshold (base:queued-window) sits underneath it.
	//
	// The ZERO VALUE means "this tap is happening now": Decide substitutes Now, so
	// the skew is exactly 0 and the guardrail cannot fire on a live tap. That is
	// also what keeps every caller written before this field existed unaffected.
	OccurredAt time.Time
	// PageIssuedAt is when the SERVER minted the tap page's signed context, or the
	// zero value when there is no page behind this record (a manual entry).
	//
	// 🔴 IT IS WHAT KEEPS sys:tap-freshness ALIVE. That guardrail denies an NFC tap
	// whose page has gone stale, and it reads tap:pageAgeSeconds — a key that, until
	// this field existed, NOTHING set. A missing key never matches (M3-04's
	// invariant), so guardrail #6 was inert: not weakened, not tunable, simply
	// unable to fire. An audit measured it and this is the fix.
	//
	// It is an INSTANT, not an age, so the age is computed from Input.Now like every
	// other elapsed value here — the package has no clock of its own, and a caller
	// passing a pre-computed age could pass one measured against a different one.
	// The stamp is server-minted and authenticated; a client cannot choose it.
	//
	// WHAT IS AND IS NOT COVERED, measured rather than assumed. TWO ceilings bound
	// a tap page and they answer DIFFERENTLY: this guardrail's window
	// (TAPPA_FRESHNESS_SECONDS, shipped 180 s) produces a RECORDED reject, while
	// the signed context's own TTL (15 min, internal/handler) refuses at parse
	// time with an UNRECORDED 400. Until M5-10 the window defaulted to 900 s —
	// the SAME number as the TTL — so the guardrail's band was empty and every
	// over-age page was answered by the TTL. It is not empty now: 180..900 s is a
	// record, which is what §4.6 wants there. Past 900 s the answer is still the
	// error page, and that is a counted LIMIT (M5-10 card), not a closed hole.
	PageIssuedAt time.Time

	// OccurredAtFromClient reports whether OccurredAt was DECLARED BY THE CALLER
	// rather than read from the server clock. It is what tap:queued is derived
	// from (with the skew) — never a client-supplied "queued" flag, which is the
	// ADR 0004 §8 rule and the reason Input has no such field. A live tap leaves
	// it false; only the offline queue sets it.
	OccurredAtFromClient bool

	// Channel is the SERVER-DERIVED arrival path (see Channel). Never a client flag.
	Channel Channel

	// SUN is the proof-of-moment fact (see SUNResult). For a QR/manual tap it is the
	// zero value (Valid=false); §5 line 2 reads it only for the nfc channel.
	SUN SUNResult

	// Tag is the tapped plaque (status + location). Always present for a resolved
	// tap; an unknown UID never reaches Decide (sun.ErrUnknownTag is rejected at M5).
	Tag *Tag

	// Employee is the tapping session's employee, or nil for NO session (§5 line 3).
	// nil and Status==deactivated are different decisions — see Employee.
	Employee *Employee

	// TagTenantID and SessionTenantID are the two halves of the tenant-mismatch
	// check — the §4.5 hole state.md tracked as hand-off N5, closed by M5-05.
	//
	// 🔴 WHY THIS CANNOT BE LEFT TO RLS. The tag lookup is CONTEXT-LESS by
	// construction (ADR 0002 item 7): a tap carries a uid, and the tenant is what
	// resolving it PRODUCES, so row-level security has no tenant to filter on and
	// does NOT hide another tenant's plaque. Until both ids reach the policy
	// engine, sys:tenant-mismatch never fires and an employee of tenant B standing
	// physically inside tenant A — matching A's IP and GPS — is written an `ok`
	// check-in in A. Feeding these two fields is what stops it.
	//
	// ⚠️ "THERE IS NO SECOND LINE OF DEFENCE" IS WHAT THIS SAID, and measuring it
	// proved the sentence too strong — and the truth less comforting. There IS a
	// second net and it is worse than none: the composite FK
	// transactions_tag_fk (tag_uid, tenant_id) refuses a row citing another
	// tenant's tag, so an UNFED engine reaches the `ok` DECISION and then fails the
	// write with 23503 — an HTTP 500 and NO record (measured in psql, and by
	// mutating the production write path). The schema therefore converts an
	// isolation violation into a §4.6 record loss rather than preventing one. The
	// guardrail is what produces the outcome anybody would want: refuse, say why,
	// and record nothing because nothing should be recorded.
	//
	// SessionTenantID is the tenant the SESSION resolved to (httpx.Identity),
	// TagTenantID the tenant the TAG resolved to, RE-RESOLVED at POST time rather
	// than trusted from the page. Both nil keeps the pre-M5-05 behaviour: session
	// presence alone is signalled to sys:no-session and the mismatch guardrail
	// stays inert (see Decide, which refuses to compare a real tenant against its
	// value-less presence placeholder — a comparison that would fabricate a
	// mismatch and, because a mismatch REDIRECTS, drop a record).
	TagTenantID     *uuid.UUID
	SessionTenantID *uuid.UUID

	// SourceIP is the request's real client IP (resolved through the trusted proxy
	// at the httpx layer), the "where" evidence.
	SourceIP netip.Addr
	// LocationIPs are the tapped location's registered static IP ranges. IP evidence
	// matches when SourceIP is contained in any of these (computed in M4-03/M4-06).
	LocationIPs []netip.Prefix

	// GPS is the coordinate read AT THE MOMENT of the tap (§4.2 — never background),
	// or nil if the browser gave none. The backup "where" evidence.
	GPS *geo.Point
	// LocationGPS is the tapped location's registered coordinate, or nil if unset.
	LocationGPS *geo.Point
	// GPSRadiusM is the tenant's configured match radius in metres (config, default
	// 150) — supplied, never baked in (geo.WithinRadius takes it as a parameter).
	GPSRadiusM float64

	// LastForPerson is this PERSON's most recent tap BY DECLARED TIME (any
	// direction), or nil if they have none. It supplies the DECLARED leg of the
	// per-PERSON debounce (§5 line 5) — person-based so different people can tap
	// one plaque back-to-back and each is recorded.
	LastForPerson *Transaction
	// SecondsSinceLastRecordedTap is how long ago this person's most recent TAP
	// was WRITTEN, measured ENTIRELY ON THE DATABASE CLOCK
	// (clock_timestamp() − created_at over the nfc/qr channels), or nil if they
	// have no recorded tap.
	//
	// 🔴 IT IS THE ONLY LEG A CALLER CANNOT STEER, and ADR 0006 is the whole story
	// of why a second one was needed. LastForPerson above is selected by a
	// client-declarable column and carries a client-declarable value, so BOTH the
	// distance and the choice of predecessor are reachable from a POST body: a
	// declared past inflates the distance, and a declared time under the person's
	// existing newest row freezes the selection so the predecessor never advances.
	// Each was measured producing 20 counted rows from ONE scanned QR in about a
	// third of a second.
	//
	// 🔴 IT IS AN AGE AND NOT A TIMESTAMP, and that is load-bearing rather than
	// tidy. Handing Go a timestamp to subtract from its own clock mixes two clock
	// domains, and under lock contention it INVERTS: the request captures Now
	// before it waits, while created_at belongs to a LATER transaction, so the
	// subtraction goes negative and the leg is discarded — measured, a fully
	// serialised request still came back `ok` for exactly that reason. Both ends
	// are now read from the same clock, so there is no skew to reason about and no
	// dependency on two hosts agreeing.
	//
	// nil means "no recorded tap", which never debounces (§4.6's safe zero value).
	SecondsSinceLastRecordedTap *float64
	// LastOpenIn is the person's last OPEN check-in (an "in" with no matching "out"),
	// or nil. Drives direction: open in present -> this tap is out, else in (§5,
	// M4-04). A practice record is never passed here (Transaction.Practice).
	LastOpenIn *Transaction

	// Shift is the employee's resolved shift for lateness (department > location,
	// §5/M4-05), or nil when none is defined. Lateness never changes the verdict.
	Shift *Shift

	// Debounce is the per-person debounce window (§5: 60s), passed in rather than
	// hard-coded so it is configurable and the test can set it explicitly.
	//
	// SUPERSEDED by PolicySet (M4-03, N3): the debounce WINDOW is now applied by the
	// sys:person-debounce guardrail inside PolicySet (from its ADR 0004 §11 Params),
	// so Decide computes only the FACT (seconds since the person's last tap) and does
	// not consult this field. It is retained for the M4-02 contract; M5 must feed the
	// SAME value into policy.Params when it assembles PolicySet so the two agree
	// (both default to 60 s today, so there is no drift yet).
	Debounce time.Duration

	// PolicySet is the per-tenant decision Set — the ordered guardrails plus the
	// baseline and tenant policies — that Decide delegates every §5 decision to
	// (policy.Evaluate). It lives IN the Input (rather than as a second Decide
	// parameter) so the M4-02 signature `func(Input) Decision` — asserted in
	// types_test.go — stays fixed and Decide remains a pure function of ONE value.
	//
	// The M5 caller assembles it with the REAL append-only version ids:
	// Guardrails(tenantParams) + BaselinePolicies(realVersionID) + the tenant's own
	// policies. Guardrails and the baseline are code; only the version ids and the
	// tenant layer come from the DB — so Decide itself never touches the DB and its
	// purity holds (internal/policy is a pure layer). A zero PolicySet (no rules)
	// makes tap:record fall to the fail-to-review default -> flag, never a silent ok.
	PolicySet policy.Set
}

// Decision is Decide's output — a DESCRIPTION of what should happen, not a written
// record (§4.6: persisting it, including the write-and-flag path, is the M5
// caller's job). Fields not relevant to a given verdict take their zero value
// (e.g. Type is nil for reject/ignored/redirect; Redirect is RedirectNone for a
// normal record).
type Decision struct {
	// Verdict is the outcome written to transactions.verdict (ok|flag|reject|
	// ignored). Set on every non-redirect path — a redirect (§5 line 3) writes no
	// record, so its Verdict is not persisted.
	Verdict Verdict
	// Type is the resolved direction (in|out), or nil when the verdict carries none
	// (reject, ignored, redirect). A pointer so "no direction" is unambiguous.
	Type *Type
	// Trust is the confidence score 20 + 50(IP) + 30(GPS) -> 20/50/70/100 (§5,
	// M4-06). It is INDEPENDENT of Verdict: a flag can carry 20, an ok 50 (GPS-only).
	Trust int
	// IPMatch reports whether SourceIP fell inside a registered LocationIPs range.
	IPMatch bool
	// GPSMatch reports whether GPS was within GPSRadiusM of LocationGPS.
	GPSMatch bool
	// Note is a human-facing annotation for the docket/report (e.g. "verified via
	// GPS" when IP is absent, or a cross-location / stale-open-in note, §5/M4-04/05).
	// It never contains a secret or a raw GPS coordinate (§4.7).
	Note string
	// CrossLocation is true when the tap happened at a location OTHER than the
	// employee's home location (Employee.Location != Tag.Location) — normal chain
	// movement (Q17, M4-05). It is a REPORT fact, recorded so the report can show
	// cross-location taps separately; it NEVER penalises the tap. The SAME fact is
	// also placed in the policy Context (employee:crossLocation) so base:cross-
	// location-note affirms it and it freezes into policy_context (M3-07). It is a
	// Decision field TOO because that baseline allow never wins the sid over
	// base:ip-or-gps-ok, so the deciding Note would not otherwise reveal it — the
	// report reads this field directly, independent of which allow won the tiebreak.
	CrossLocation bool
	// MinutesLate is how many minutes late a CHECK-IN is against the employee's
	// resolved shift (Input.Shift), or nil when it is not computed: no shift, a
	// check-OUT (a checkout is not "late"), or an unresolvable timezone. Positive
	// means late; <= 0 means on time or early. It is a REPORT output and NEVER
	// changes the verdict (§5, M4-05): a late tap is still ok. Minutes, not float
	// hours, so no float touches a time figure (§6); a pointer so "not computed" is
	// unambiguously distinct from "0 minutes late".
	MinutesLate *int
	// Practice marks a training tap (TRAINING stamp; never counts toward hours, §5,
	// M4-06). SERVER-derived from Employee.ActivatedAt, never a client claim.
	Practice bool
	// Redirect is the non-recording outcome selector — RedirectActivation for the
	// no-session case (§5 line 3), RedirectNone otherwise (see Redirect).
	Redirect Redirect
	// Security is true when this decision must raise a security alert to the tenant's
	// managers — a deactivated account tapped (§5 line 4) or a lost tag tapped. It is
	// a bool here; M4-03 sets it from policy.Decision.SecurityAlert being non-empty.
	Security bool

	// --- Explainability, carried from the policy Decision (M4-03) -------------
	// These bind the record to WHY it got its verdict (M3-07, migration 0008:
	// matched_sid / policy_layer / policy_version_id) so a past tap explains itself
	// forever, even after the tenant edits its policy. The M5 write path persists
	// them; Decide only carries them out of policy.Evaluate.

	// MatchedSid is the sid of the deciding statement or guardrail — "sys:..." for a
	// guardrail, a "base:..."/tenant sid for a stored decision, or "default" for the
	// no-match fallback. MACHINE-FILTERABLE, so a report can count taps by rule.
	MatchedSid string
	// Layer is where the deciding rule lives (guardrail | baseline | tenant). A
	// guardrail decision and the built-in default both carry LayerGuardrail; the
	// "default" MatchedSid tells them apart.
	Layer policy.Layer
	// PolicyVersionID is the append-only policy_versions row a baseline/tenant
	// decision came from, or nil for a guardrail/default decision (code, not in the
	// DB). Pinning it is what keeps a past record's reason stable after a later edit.
	PolicyVersionID *uuid.UUID

	// PolicyContext is the EXACT key map that was handed to policy.Evaluate — the
	// frozen decision snapshot migration 0008's policy_context column exists for,
	// and the input half of "replay this tap and get the same verdict" (M9-06).
	//
	// IT IS THE SAME MAP, NOT A RECONSTRUCTION, and that is the whole point. The
	// alternative was for the M5-05 write path to rebuild it from Input, which
	// means the same facts computed in two places by two pieces of code that are
	// free to drift — the failure this repo has now paid for three times (a check
	// and its consumer seeing different representations of one value). Decide
	// computes the facts once, evaluates on them, and hands out what it evaluated.
	//
	// §4.7: it carries a GPS DISTANCE IN METRES and never a coordinate, because
	// the map it comes from never held one (see Decide). It holds no token, no
	// CMAC, no key and no invite code — none of those is a context key at all.
	//
	// WHAT IS NOT IN IT, stated so a reader does not infer more: the values
	// computed AFTER the evaluation (direction, practice, trust, minutes late) are
	// not here, because they did not feed the decision; each has its own column on
	// the record. Nor are the guardrail-only Context fields (the two tenant ids,
	// the debounce gap, reviewer identity), which are not document vocabulary —
	// the tenant and employee are columns on the row, and the debounce gap is
	// recomputable from the person's previous row.
	PolicyContext map[policy.ContextKey]any
}
