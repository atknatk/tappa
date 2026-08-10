// Package checkin is the tap ORCHESTRATOR: the one place where a physical touch
// becomes a decided, recorded, permanent attendance record (M5-05).
//
// It is the wire M3 and M4 were built for. The policy engine, the ten
// guardrails and tap.Decide were all written, proven and left PURE — no database,
// no HTTP, no clock — which meant that until this package existed, not one real
// request had ever reached them. This is where the four evidences of CLAUDE.md §5
// are collected and handed over:
//
//	SUN     proof of MOMENT   the chip's CMAC (checked when the page was built)
//	                          AND the atomic counter advance, which happens HERE
//	session proof of PERSON   resolved at the request boundary (httpx.Identify)
//	IP      proof of PLACE    the tapped location's registered ranges
//	GPS     backup PLACE      read once, at the moment of the tap (§4.2)
//
// WHY IT IS NOT IN internal/handler AND NOT IN internal/domain/tap. §3 forbids a
// business rule or a query in a handler, and tap must stay PURE — its whole
// correctness proof rests on Decide being a function of one value, and importing
// pgx to fetch a shift would end that. So the orchestration lives between them:
// this package holds the SQL and the sequencing, tap holds the judgement, and the
// handler parses a form and renders a page.
//
// 🔴 THE THREE RED LINES THIS PACKAGE IS ANSWERABLE FOR:
//
//	§4.3  transactions is IMMUTABLE. There is exactly ONE statement in this
//	      package that writes it — InsertTransaction — and no UPDATE or DELETE of
//	      it anywhere. A `flag` is never later turned into an `ok`; approval is a
//	      row in another table (M6-04). (The package DOES insert elsewhere:
//	      audit_log, and the three policy tables when a tenant's baseline is
//	      materialised — policyset.go. None of them touches transactions.)
//
//	      ⚠️ AND SINCE 2026-08-10 THIS PACKAGE IS NO LONGER THE PRODUCT'S ONLY
//	      WRITER OF THAT TABLE. M6-08 added internal/domain/manual: a manager
//	      typing a record for somebody with no phone, and the checkout Q18 says
//	      the system will never invent for itself. The sentence above is still
//	      true as written — it is scoped to THIS package — but a reader who took
//	      it as "one writer in the product" would now be wrong, and that reading
//	      is the one it was written to support. What is still singular here: this
//	      is the only statement the TAP path writes, and the only one that may set
//	      tag_uid, ctr, sun_valid, source_ip, ip_match, gps_lat, gps_lng, gps_match
//	      or the four policy-decision columns. The other writer's statement has no
//	      parameter for any of them.
//
//	      🔴 "A REFUSED RECORD CARRIES NO DIRECTION" IS HELD BY FOUR THINGS, AND
//	      THIS PARAGRAPH NAMED ONLY THE WEAKEST OF THEM FOR ONE ROUND. insertParams
//	      below writes the direction from dec.Type, and internal/domain/tap only
//	      assigns one on ok/flag. That is the first barrier; there are three more:
//
//	        1. decide.go's `if` — and it is NOT a bare code invariant, it is the
//	           subject of internal/domain/tap's
//	           TestDecide_DirectionNilForNonRecordVerdicts (red in four sub-cases
//	           under mutation).
//	        2. The manual writer cannot express the shape at all: its verdict is a
//	           SQL literal, not a parameter (db/queries/transactions.sql).
//	        3. internal/domain/ledger's endpointState FAIL-SAFES an unrecognised or
//	           refused verdict to HoursAwaiting, so such a row would be quarantined
//	           and reported separately rather than paid.
//	        4. Migration 00014's transactions_refusal_has_no_direction — the SCHEMA
//	           now refuses it outright, VALIDATED.
//
//	      ⚠️ THIS BLOCK USED TO SAY THE OPPOSITE AND THE CORRECTION IS KEPT VISIBLE:
//	      it called the rule "a CODE invariant rather than a schema constraint",
//	      said "the database accepts verdict='reject' with type='in'", and cited a
//	      test name that no longer exists. All three were true before 00014 and are
//	      false after it; on a §4.3/§4.6 surface a stale sentence sends the next
//	      reader looking for a barrier in the wrong place. The live measurement is
//	      internal/handler's TestManualDB_TheSchemaREFUSESADirectedRefusalNow, which
//	      drives both refusals and five positive controls.
//	§4.4  The counter advance is ATOMIC and is the ONLY thing separating a fresh
//	      touch from a replay. It is one SQL statement with the comparison inside
//	      it; nothing in this package reads last_ctr and compares it in Go, and a
//	      test pins that the advance really goes through that statement rather
//	      than trusting the shape of this file (see counterAdvancer).
//	§4.6  A RECORD IS NEVER LOST. Every DECIDED tap ends in an INSERT, including
//	      every reject.
//
//	      ⚠️ THREE PATHS END WITHOUT ONE, and an earlier version of this header
//	      said "the one §5 exempts", which was wrong by two. They are:
//	        · §5 row 3, no session — there is no person to record against, and
//	          this package cannot even be reached (the handler answers it);
//	        · sys:tenant-mismatch — a REDIRECT, so the tap lands in neither
//	          tenant, which is the isolation outcome rather than a lost record;
//	        · an unknown tag uid — no tag, no location, nothing to attribute a
//	          record to, so the attempt leaves an audit_log row instead.
//	      Every OTHER outcome is a row, and a failure to write one is surfaced
//	      loudly rather than swallowed.
//
//	      ⚠️ AND THE OTHER EDGE OF THE SAME RULE: `transactions` IS A WRITE
//	      BUDGET. Every accepted POST appends an undeletable row, so an
//	      authenticated caller can fill the table as fast as the shield allows —
//	      measured: one session, one minted context, 40 posts, 40 permanent rows;
//	      at the shipped budgets (300 per session and 3000 per address, each per
//	      ten minutes) that is roughly 43k rows per session and 432k per venue
//	      address per day. It is NOT forgery: every row is attributable and most
//	      of them would be `ignored` or `reject`, so this is noise and storage
//	      rather than fraud. It is written down because the M5-02 argument — that
//	      a protection must not become a cheaper attack than the endpoint it
//	      protects — was made about audit_log alone, and it applies here too. The
//	      fix is the one that also answers the 429 residual: a shared store with
//	      per-venue keying (M8).
package checkin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/sun"
)

// Database is the narrow slice of *db.DB this package needs, declared HERE at
// the consumer (§7).
//
// TWO METHODS, AND THEY ARE DIFFERENT IN KIND. WithTenant runs everything under
// one tenant's row-level security. GetTagByUID is the exception ADR 0002 item 7
// carves out: a tap arrives carrying a uid and nothing else, so the tenant is
// what resolving it PRODUCES and there is no context to run it in. That is also
// exactly why RLS cannot be the tenant-isolation defence on this path and why
// sys:tenant-mismatch has to be fed (hand-off N5).
type Database interface {
	GetTagByUID(ctx context.Context, uid string) (db.ResolvedTag, error)
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// AuditRecorder is the slice of audit.Recorder this package needs (§7).
type AuditRecorder interface {
	Record(ctx context.Context, e audit.Event) (uuid.UUID, error)
}

// counterAdvancer is the ONE query the replay guard needs, declared here at the
// consumer (§7) even though internal/sun declares an identical one — because
// what this package has to be able to prove is different from what that one
// proves.
//
// 🔴 IT EXISTS SO A TEST CAN PIN *WHICH STATEMENT THIS PATH ISSUES*, and the
// reason is a measured gap rather than a preference. internal/sun's contention
// test proves that sun.AdvanceCounter resolves N racers to exactly one winner;
// this package's tests prove that a tap ends in the right record. NEITHER of
// them proved that the tap path CALLS that function — so an audit rewrote the
// body below into a genuine read-then-compare-then-write TOCTOU and `make test
// -race` came back green across all thirteen packages. The same mutation with an
// 80 ms sleep produced "12 of 12 concurrent taps were SUN-valid": a real replay
// hole that the suite's timing simply could not see.
//
// Concurrency was the wrong thing to test for here. The atomicity is proven
// where the statement lives; what belongs at THIS layer is the wiring — that the
// advance goes through AdvanceTagCounter, once, with this tap's tenant, uid and
// counter. A seam makes that assertable without waiting on a race to happen.
type counterAdvancer interface {
	AdvanceTagCounter(ctx context.Context, arg store.AdvanceTagCounterParams) (store.AdvanceTagCounterRow, error)
}

// storeAdvancer is the production seam: it builds the tenant-scoped sqlc querier
// from the in-flight transaction. advance calls it through a FIELD rather than
// naming store.New directly, so a test can substitute a counting double; New
// wires this one, and a test asserts that it did (TestNew_WiresTheProductionAdvancer)
// so the seam cannot quietly become the place the real querier went missing.
func storeAdvancer(tx pgx.Tx) counterAdvancer { return store.New(tx) }

// Audit actions this package writes.
const (
	// ActionTapSecurityAlert is §5 row 4's "log the attempt and raise a security
	// alert", and the lost-tag alert beside it. It accompanies a `reject` that has
	// ALREADY been recorded in transactions, which is what bounds it: this action
	// writes at most one row per attendance row, so it is not a cheaper way to fill
	// an undeletable table than the endpoint itself is (the M5-02 lesson — a
	// protection's cost can be an attack on what it protects). An audit measured
	// the ratio: 40 rejected taps, 40 transactions, 40 audit rows, 1:1.
	//
	// ⚠️ THE OTHER ACTION BELOW IS NOT BOUNDED THE SAME WAY, so the two must not
	// share a sentence. ActionTapUnknownTag writes an audit row with NO transaction
	// beside it. What bounds that one is different: reaching it needs a signed
	// context naming a uid that no longer resolves — and this server minted that
	// context only after the tag DID resolve — on top of the tap rate limiter.
	ActionTapSecurityAlert = "tap.security_alert"
	// ActionTapUnknownTag records a signed context naming a uid that no longer
	// resolves. It cannot become an attendance record (no tag, no location), so
	// the trail is the only place it can leave a mark.
	ActionTapUnknownTag = "tap.unknown_tag"
)

// Errors this package returns. Each is a distinct sentinel so the HTTP layer can
// tell a caller mistake from an outage without reading strings.
var (
	// ErrUnknownTag reports that the tap's uid resolved to no tag. It is
	// sun.ErrUnknownTag re-used rather than a second sentinel for the same fact:
	// the tap page already maps it to "we don't know that plaque", and two
	// sentinels meaning one thing is how a caller ends up handling only one.
	ErrUnknownTag = sun.ErrUnknownTag

	// ErrEnteredByRequired is a `manual` record with no author. See Request.EnteredBy.
	ErrEnteredByRequired = errors.New("checkin: a manual record must name the manager who entered it")

	// ErrInvalidRequest is a malformed request from the caller's own code (a
	// missing session tenant, a missing employee) — a programming error at the
	// handler boundary, never something a person tapping can cause.
	ErrInvalidRequest = errors.New("checkin: invalid request")
)

// Service records taps. Dependencies are injected as struct fields (§7); there is
// no package-level state and no init magic.
type Service struct {
	data     Database
	trail    AuditRecorder
	policies policySets
	// gpsRadiusM is the tenant's proof-of-place ring in metres (config, bounded
	// 25..1000 by ADR 0004 §11 at startup).
	gpsRadiusM float64
	// debounce is the per-person window. It is carried into policy.Params, which
	// is where sys:person-debounce actually reads it — hand-off N3, which was
	// exactly this value being range-checked in config and then never reaching the
	// guardrail, so the shipped 60 s constant decided regardless of configuration.
	debounce time.Duration
	// advancer builds the querier the atomic advance runs on. Production is
	// storeAdvancer; a test substitutes a counting double to pin that this path
	// issues AdvanceTagCounter and nothing else (see counterAdvancer).
	advancer func(pgx.Tx) counterAdvancer
	// now is injectable for tests; production leaves it nil and reads the clock.
	//
	// It is the only clock in the DECISION: tap.Decide never calls time.Now(), so
	// every elapsed value a verdict depends on — the debounce gap, the occurred_at
	// skew, the page age — is measured against this one reading. It is NOT the only
	// clock on the request PATH: the signed context has its own, for minting and
	// expiring (internal/handler/tapcontext.go). Saying otherwise would be a tidier
	// sentence than the code deserves.
	now func() time.Time
	log *slog.Logger
}

// New wires the service. Every dependency is required — a nil one on this path
// fails as a missing hour on a payslip, which is not a failure mode worth having.
func New(data Database, trail AuditRecorder, cfg *config.Config, log *slog.Logger) (*Service, error) {
	switch {
	case data == nil:
		return nil, errors.New("checkin: nil database")
	case trail == nil:
		return nil, errors.New("checkin: nil audit recorder")
	case cfg == nil:
		return nil, errors.New("checkin: nil config")
	}
	if log == nil {
		log = slog.Default()
	}
	// 🔴 HAND-OFF N3 CLOSED HERE. TAPPA_DEBOUNCE_SECONDS was range-checked at
	// startup and then went nowhere: policy.DefaultParams() hard-codes 60 s, so a
	// deployment that configured 120 got 60. Params carries the configured value
	// now, and Validate re-checks it against the SAME ADR 0004 §11 constants
	// config used — belt and braces, and a loud failure rather than a guardrail
	// quietly comparing against an out-of-range window.
	params := policy.DefaultParams()
	if cfg.Debounce > 0 {
		params.DebounceWindow = cfg.Debounce
	}
	// The same plumbing for the freshness window (M5-10). Until this line existed
	// the guardrail compared against policy.DefaultParams()'s 900 s, which EQUALS
	// the signed context's TTL — an empty band, so sys:tap-freshness could not
	// fire at all and every over-age page was an unrecorded 400. With the shipped
	// 180 s the band 180..900 s becomes a RECORDED reject, which is what §4.6
	// wants there. Falsified by
	// TestCheckinDB_ConfiguredFreshnessWindowReachesTheGuardrail: the DB harness
	// runs a NON-DEFAULT window on purpose, so deleting this line turns that test
	// red instead of leaving it green on a degenerate value (the M5-05 F3 lesson).
	//
	// 🔴 A ZERO IS AN ERROR HERE AND A FALLBACK TWO LINES UP, and the asymmetry is
	// the point rather than an inconsistency. Debounce's fallback (60 s) EQUALS
	// its shipped default, so a caller that omits it lands on shipped behaviour.
	// Freshness's fallback WAS DefaultParams()'s 900 s: the range MAXIMUM, and the
	// same number as the signed context's TTL — i.e. precisely the value at which
	// the recorded band is empty and sys:tap-freshness cannot fire at all. So the
	// omission was not a smaller window, it was the guardrail switched off.
	// MEASURED while the assignment below was still conditional, from an external
	// package: checkin.New(data, trail, &config.Config{/* no Freshness */}, log)
	// returned err == nil with an effective window of 15m0s, and TWO in-tree
	// callers were doing exactly that, one of them the harness behind
	// `make simulate-day`.
	//
	// ⚠️ WHAT CLOSES THAT HOLE IS THE NEXT TWO LINES, NOT THIS BLOCK, and an
	// earlier version of this comment implied otherwise. The assignment is
	// UNCONDITIONAL, so a zero reaches params.Validate(), which refuses it against
	// the same ADR 0004 §11 range config uses: delete the `if` and New still fails,
	// with "checkin: policy: freshness window = 0s is outside [60, 900] seconds".
	// This block earns its four lines on the MESSAGE alone — Validate can only
	// report an out-of-range window, which sends the reader hunting for a policy
	// bug, when the actual fault is an unset config field whose fallback history is
	// the paragraph above. TestNew_RefusesAZeroFreshnessWindow asserts that text,
	// so deleting the block is RED rather than green (measured, both ways).
	//
	// THE GUARD IS `<= 0` AND THE MESSAGE MUST NOT SAY "zero", because a NEGATIVE
	// value is reachable and an earlier wording printed "is zero" for it: with
	// TAPPA_FRESHNESS_SECONDS=NaN, config.floatEnvRange returns NaN (every NaN
	// comparison is false, so the range check passes — a known limit documented
	// there) and time.Duration(NaN * 1s) is the int64 MINIMUM, i.e.
	// -2562047h47m16.85s. Measured. This line is what fails that deployment closed,
	// so it is also the line whose text the operator reads; printing the actual
	// value is what turns "why is it zero, I set it" into "my value did not parse".
	if cfg.Freshness <= 0 {
		return nil, fmt.Errorf("checkin: config Freshness is not positive (%v); set it "+
			"(config.Load defaults it to 180s, and TAPPA_FRESHNESS_SECONDS=NaN lands here as a large "+
			"NEGATIVE duration) — falling back would silently run the 900s window, where "+
			"sys:tap-freshness cannot fire", cfg.Freshness)
	}
	params.FreshnessWindow = cfg.Freshness
	if err := params.Validate(); err != nil {
		return nil, fmt.Errorf("checkin: %w", err)
	}
	return &Service{
		data:       data,
		trail:      trail,
		advancer:   storeAdvancer,
		policies:   policySets{data: data, params: params, log: log},
		gpsRadiusM: cfg.GPSRadiusMeters,
		debounce:   params.DebounceWindow,
		log:        log,
	}, nil
}

// Request is one tap, as the HTTP boundary resolved it. EVERY FIELD IS
// SERVER-DERIVED except OccurredAt and GPS, and both of those are named as
// client input where they are used.
type Request struct {
	// SessionTenantID and EmployeeID come from the session cookie (httpx.Identity)
	// — the "who". Both are required: a request without them is a caller bug, not
	// a session-less tap, because §5 row 3 is answered before this package is
	// reached (there is no way to verify a signed tap context without a session).
	SessionTenantID uuid.UUID
	EmployeeID      uuid.UUID

	// TagUID, Ctr, Channel, CMACVerified and TagTenantID all come from the SIGNED
	// tap context the page minted (internal/handler/tapcontext.go), so a client
	// cannot choose any of them.
	//
	// 🔴 Channel IN PARTICULAR (hand-off N2): it is derived by sun.Parse from
	// whether the URL carried ctr and cmac, on the server, at page-build time, and
	// then authenticated. A client that could declare channel=qr would shed
	// sys:sun-invalid and sys:tap-freshness — both are NFC-only — which is why the
	// value travels inside a MAC rather than in a form field.
	TagUID       string
	Ctr          uint32
	Channel      tap.Channel
	CMACVerified bool
	// TagTenantID is what the TAG resolved to when the page was built. It is
	// compared against a FRESH resolve here, and the fresh one is what decides —
	// see Record.
	TagTenantID uuid.UUID

	// SourceIP is the resolved client address (httpx.ClientIP, bounded by the
	// trusted-proxy list). An invalid/zero address simply never matches a range.
	SourceIP netip.Addr

	// GPS is the coordinate read ONCE, at the moment of the tap, or nil (§4.2 —
	// never watched, never from the background). It is CLIENT INPUT: it is
	// validated at the HTTP boundary and, being backup evidence, a wrong or absent
	// one costs trust rather than the record.
	GPS *geo.Point

	// PageIssuedAt is when GET /t minted the signed context — a SERVER clock
	// reading, authenticated, carried back by the browser. It feeds
	// sys:tap-freshness (guardrail #6), which an audit found INERT because
	// tap:pageAgeSeconds had no source anywhere in the product. Zero means "no page
	// behind this record" (a manual entry) and the guardrail then has nothing to
	// judge, which is correct rather than lenient: it is NFC-only.
	PageIssuedAt time.Time

	// OccurredAt is when the tap claims to have happened, or NIL for "the server
	// times this one" (the live case — nothing on the tap page sends a time).
	//
	// ⚠️ IT IS A POINTER BECAUSE THE ZERO VALUE IS A LEGAL DECLARATION. It used to
	// be a plain time.Time with the zero value meaning "not declared", and an audit
	// measured the collision: a client sending 0001-01-01T00:00:00Z landed exactly
	// on the sentinel, so its tap was accepted and stamped with the SERVER's clock,
	// while 0001-01-01T00:00:01Z — one second later — was rejected by
	// sys:occurred-at-bound. Nil cannot be typed into a form field, so the two
	// questions ("was a time declared" and "what was it") no longer share a value.
	//
	// 🔴 IT IS THE ONE UNVERIFIED CLIENT INPUT ON THIS PATH and the most important
	// field of the record (M5-05 trap K1): none of the four evidences carries a
	// clock, so a genuine 09:00 tap can declare 07:00 and claim two hours. The
	// ceiling is the sys:occurred-at-bound GUARDRAIL, which no tenant can switch
	// off; base:queued-window is the softer review threshold underneath it. Both
	// are fed from here through tap.Input.
	OccurredAt *time.Time

	// EnteredBy is the MANAGER who typed this record in, and it is REQUIRED when
	// Channel is manual — a manual record with no author is an attendance row
	// nobody is answerable for. It is refused rather than silently accepted (§7).
	//
	// The check lives here rather than in tap.Decide because Decide is pure and
	// returns no error, and because entered_by is a PROVENANCE field: it says
	// where a record came from, it is not evidence and it changes no verdict.
	// Nothing reaches this package with channel=manual today — the manual entry
	// screen is M6-04 — and the rule lives here rather than waiting for that
	// screen because the WRITE path is where it belongs. What that buys is
	// narrower than "M6-04 cannot arrive without it": a manual record written
	// THROUGH THIS SERVICE is checked, and M6-04 writing one another way would
	// not be. Nothing here can enforce that; the test is a net, not a fence.
	EnteredBy *uuid.UUID
}

// Outcome says what happened to a tap, in the caller's terms.
type Outcome string

const (
	// OutcomeRecorded: a transactions row was written. Result.Decision carries the
	// verdict, which may be any of ok / flag / reject / ignored.
	OutcomeRecorded Outcome = "recorded"
	// OutcomeActivation: §5 row 3 — no session, so the activation page and NO
	// record. It cannot arise from Record (a signed context cannot be verified
	// without a session), and exists so the handler and this package name the same
	// outcomes.
	OutcomeActivation Outcome = "activation"
	// OutcomeForeignTenant: sys:tenant-mismatch — this plaque belongs to another
	// organisation. It REDIRECTS, so no `transactions` row is written in EITHER
	// tenant and no manager sees another tenant's employee.
	//
	// ⚠️ "WRITES NOTHING" IS WHAT THIS COMMENT USED TO SAY AND IT WAS FALSE, which
	// an audit measured: the advance ran before the decision, so a cross-tenant tap
	// moved the OTHER tenant's tags.last_ctr (900 -> 901) while leaving no row
	// anywhere. That is fixed (see advance), and the claim is now written to the
	// size of what is actually guaranteed:
	//
	//	no `transactions` row, in either tenant;
	//	no change to the FOREIGN tenant's state — its counter, its rows, nothing.
	//
	// What this path CAN still write is in the SESSION's OWN tenant: assembling the
	// decision Set materialises that tenant's managed baseline if it has none
	// (policyset.go). That is the caller's own policy layer, written under their
	// own context, and it happens because a decision cannot be reached without the
	// rules — but it is a write, so it is named here rather than covered by a
	// tidier sentence.
	//
	// It is a SEPARATE outcome from OutcomeActivation even though tap.Decide maps
	// both to RedirectActivation (the low-priority note M4-03 left behind): sending
	// somebody with a perfectly good session to an activation page would be a
	// nonsense instruction, and the two are told apart by the deciding sid.
	OutcomeForeignTenant Outcome = "foreign-tenant"
)

// Result is what Record concluded. It carries DOMAIN types only — the guarantee
// is the FIELD LIST below, not a rule somebody remembers: there is no field an
// internal/store row could travel in, so an HTTP response has nothing to render
// one from. Adding one would be the moment that stops being true.
type Result struct {
	Outcome  Outcome
	Decision tap.Decision
	// TransactionID is the recorded row, or uuid.Nil when nothing was recorded.
	TransactionID uuid.UUID
	// OccurredAt is the instant the record carries (§6: UTC).
	OccurredAt time.Time
	// LocationName is the TAPPED venue, for the confirmation screen. Empty when
	// the location could not be read in the session's tenant.
	LocationName string
	// Timezone is the tenant's IANA zone (Q01). It travels so the RENDER layer can
	// show a wall clock — everything below it, including OccurredAt above, is UTC
	// (§6: convert at render, never in storage or in the decision, which is where
	// overnight-shift bugs come from).
	Timezone string
	// BusinessType is the tenant's own category — one of the eight CHECK-
	// constrained values in migration 00001 ('restaurant', 'production', …). It
	// travels for ONE reason (M5-06): the confirmation screen ends with a brand
	// sentence that differs per kind of workplace, and picking it in the render
	// layer needs the category.
	//
	// IT IS NOT A DECISION INPUT and must not become one. Nothing in tap.Input
	// carries it, no policy reads it, and a rule that varied a verdict by business
	// type would belong in internal/policy as a tenant policy, not here.
	//
	// IT IS ALSO NOT AN IDENTITY. A category shared by every restaurant in the
	// system says nothing about WHICH tenant this is, so it does not widen what a
	// screen could disclose (§4.5/§4.7). Per-tenant editable copy is M9-04.
	BusinessType string
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

// Record runs one tap end to end: resolve, advance, gather, decide, write.
//
// THE ORDER IS THE M5-05 CARD'S AND IT IS LOAD-BEARING AT TWO POINTS.
//
//  1. THE TAG IS RE-RESOLVED, not taken from the signed context. Between opening
//     the page and pressing the button, a plaque can be marked lost or retired —
//     that is §5 row 1, and a stale status would record an `ok` for a plaque that
//     was reported stolen thirty seconds ago.
//  2. THE COUNTER IS ADVANCED BEFORE THE DECISION, and only after the CMAC has
//     been confirmed. Advancing first would let anyone push last_ctr forward with
//     rubbish CMACs and have every real tap rejected behind it (§4.4's DoS); not
//     advancing at all would record replays.
//
// 🔴 sun.Verify IS NOT CALLED, AND CALLING IT WOULD BREAK EVERY NFC TAP rather
// than being merely redundant — the difference matters, because "cannot" would
// suggest the compiler stops you and nothing does. The signed context deliberately does
// not carry the chip's CMAC (§4.7 — only the one-bit outcome of checking it
// crosses to the browser), so a Params built here would fail verifyMAC, report
// SUNValid=false, and never advance the counter: every NFC tap in the product
// would silently become a `flag`. The contract, stated identically in
// internal/sun/preview.go, internal/handler/tapcontext.go and the M5-05 card, is
// an AND of two halves:
//
//	SUN VALID == CMACVerified (established at page build, and it does not decay)
//	             AND the atomic advance succeeding (established only here)
//
// A caller that reads the first without doing the second records replays; one
// that does the second without reading the first records forgeries. Both halves
// are below, in that order.
//
// ⚠️ THE COUNTER IS SPENT BEFORE THE RECORD EXISTS, so ANY failure between the
// advance and the insert costs that press its evidence. Written out because it is
// a real window, not a theoretical one, and because an earlier version of this
// note described only ONE of its shapes (a cancelled request) when the audit had
// measured another.
//
// The window covers everything between step 2 and step 5 below:
//
//	· the client disconnects — ctx is the HTTP request's all the way through, so
//	  cancellation aborts the remaining queries;
//	· any database or infrastructure failure in gathering, deciding or inserting.
//	  Measured: with an employee id that will not resolve, `transactions` stays
//	  +0 while last_ctr has already gone 700 -> 701.
//
// WHAT BOUNDS IT TODAY: reaching the second shape needs an infrastructure fault
// rather than a caller's input — tappa_app cannot delete a tag or an employee
// (measured grants), so a caller cannot engineer the mid-flight failure.
//
// IT IS NOT A PERMANENT LOSS AND IT IS NOT SILENT. The counter only moved to a
// value the chip really emitted, so the next physical touch emits a higher one
// and is recorded; the failure is an ERROR log and the person is told "Nothing
// was recorded … Tap the plaque again in a moment." Detaching the write from
// cancellation would trade this for something worse — a request the client
// abandoned still recording attendance, indistinguishable from a real one — so it
// is left as it is and named here.
//
// 🔴 HOW WIDE THE WINDOW IS — MEASURED, AND IT IS NO LONGER "A FEW MILLISECONDS".
// An earlier version of this comment said exactly that, and ADR 0006's per-person
// advisory lock made it false: the window is now `advance` -> acquiring a pool
// connection -> WAITING FOR THIS PERSON'S LOCK -> INSERT, and the wait is bounded
// only by middleware.Timeout(30s) (internal/httpx/router.go). Measured two ways:
// holding that person's exact lock key from outside for 3 s left last_ctr at 701
// with zero rows for the whole 3 s (the tap completed 3.06 s after the POST); and
// self-contained, 50 simultaneous NFC POSTs by one person produced a worst case of
// 1.24 s. The loss shape is unchanged and still not silent — what changed is how
// long the gap can last, and pretending otherwise is the fourth absolute sentence
// this file has had measured out from under it.
func (s *Service) Record(ctx context.Context, req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}
	now := s.clock()
	occurredAt, fromClient := now, false
	if req.OccurredAt != nil {
		occurredAt, fromClient = req.OccurredAt.UTC(), true
	}

	// --- 1. Re-resolve the tag (context-less, ADR 0002 item 7) ----------------
	tagRow, err := s.data.GetTagByUID(ctx, req.TagUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// NOT SWALLOWED (the M2 hand-off). A signed context names a uid that no
			// longer resolves, which cannot happen by accident: the context was
			// minted by this server after the tag DID resolve. There is no tag and
			// no location, so there is nothing to attribute an attendance record to
			// — but there IS a tenant (the session's), so unlike the anonymous case
			// internal/sun describes, the attempt can and does leave a trail.
			s.log.Warn("checkin: signed context names a tag that no longer resolves",
				"tag_uid", req.TagUID, "employee_id", req.EmployeeID, "tenant_id", req.SessionTenantID)
			s.record(ctx, audit.Event{
				TenantID: req.SessionTenantID,
				Action:   ActionTapUnknownTag,
				Target:   req.TagUID,
				Detail:   unknownTagDetail{Channel: string(req.Channel)},
			})
			return Result{}, ErrUnknownTag
		}
		return Result{}, fmt.Errorf("checkin: resolve tag: %w", err)
	}
	if req.TagTenantID != uuid.Nil && req.TagTenantID != tagRow.TenantID {
		// The page said one owner and the database now says another. A tag does not
		// change hands today (no query moves one), so this is either a stale
		// context or something worse — either way the FRESH resolve is what decides
		// below, because it is the current truth and it is the value the isolation
		// guardrail must judge. Recorded as an anomaly rather than a refusal: a
		// refusal here would drop the tap, and §4.6 does not trade a record for an
		// oddity we can log.
		s.log.Error("checkin: the tag changed tenant between the page and the tap",
			"tag_uid", req.TagUID, "context_tenant_id", req.TagTenantID, "resolved_tenant_id", tagRow.TenantID)
	}

	// --- 2. §4.4: the atomic advance -----------------------------------------
	sunValid, ctrGap, err := s.advance(ctx, req, tagRow)
	if err != nil {
		return Result{}, err
	}

	// --- 3. THE SERIALISED SECTION: lock, gather, decide, write --------------
	//
	// 🔴 ONE TRANSACTION, AND THE LOCK IS ITS FIRST STATEMENT (ADR 0006, layer 4).
	// The debounce is a READ-THEN-DECIDE, and it used to lose the same way §4.4
	// says a counter check loses: gather and write were separate transactions, so
	// N simultaneous requests all read BEFORE any of them had written, every one
	// saw the same old predecessor, and not one of them was a duplicate. Measured,
	// -race, one employee: 50 simultaneous POSTs -> 51 counted rows, ZERO ignored,
	// in 0.41 s; TWO simultaneous POSTs were already enough.
	//
	// pg_advisory_xact_lock is TRANSACTION-SCOPED, so COMMIT or ROLLBACK releases
	// it and no error path can leak it. The key is derived from (tenant, employee),
	// so different people do not wait on EACH OTHER'S LOCK (measured: 30 people at
	// once, zero non-200s, every one counted, no slower than before the lock).
	//
	// 🔴 BUT THE LOCK DOES COST UNRELATED PEOPLE, and an earlier version of this
	// comment claimed the opposite ("no new denial-of-service surface is
	// attributable to the lock"). That was measured and it is FALSE. A waiter holds
	// its pool connection while it waits, so a flood aimed at ONE key parks
	// connections that do nothing: sampled pg_stat_activity showed up to 15 of the
	// 16 pooled connections in wait_event='advisory', against 0 for the same row
	// volume spread over separate keys. Other tenants' requests then queue at
	// pool.Begin. A clean A/B (flood 150, victim measured as a SINGLE shot at a
	// fixed 200 ms offset, fresh session each round, warmed tenant, 3 rounds):
	//
	//	one key       flood 1.91/1.38/1.45 s   uninvolved third party 1.60/1.05/1.14 s
	//	separate keys flood 0.372/0.370/0.370  uninvolved third party 0.178/0.178/0.176
	//
	// i.e. an unrelated person's single tap gets 6-9x slower. The ceilings are the
	// ByAddress budget (3000/10min) and middleware.Timeout(30s); no record is lost
	// in either arm (§4.6 holds).
	//
	// ⚠️ MEASURE IT THIS WAY OR NOT AT ALL. The earlier "no difference" reading was
	// an artefact: driving a one-key flood from a SINGLE session makes most requests
	// hit the BySession 300/10min limit and return 429 without ever touching the
	// lock (reproduced: a 200-request one-key flood finished in 40 ms, all rate
	// limited). Measuring the victim in a loop also pushes worst-of-N onto the slow
	// arm. One shot, fresh session, distinct sessions for the flood.
	//
	// A KEY COLLISION (two different people hashing to one 64-bit key) serialises
	// them. It cannot mix their data — every statement inside still carries its own
	// tenant_id filter and runs under RLS — but THE WAIT IS NOT MILLISECONDS. The
	// loser pays for the REMAINDER OF THE WHOLE LOCKED SECTION: measured, a session
	// holding the key for 2 s made a colliding session that arrived 0.4 s later
	// wait 1778.890 ms, and the third-party figures nine lines above
	// (1.60/1.05/1.14 s) are exactly the position a collided person sits in
	// permanently. Ceiling: middleware.Timeout(30s).
	//
	// (An earlier version of this line said "costs one of them milliseconds" — the
	// SIXTH absolute sentence this change had measured out from under it, and the
	// twin of one the sibling query file retracts by name.)
	//
	// WHAT IS DELIBERATELY OUTSIDE IT: the policy set is resolved ABOVE, because
	// materialising a tenant's baseline opens its own transaction and doing that
	// while holding a lock would nest two pool connections; the atomic counter
	// advance stays in step 2, in the TAG tenant's own context, exactly where §4.4
	// and the M5-05 audit put it. NOTHING inside the locked section does HTTP or
	// any other network work — tap.Decide is pure, and the only I/O is this
	// tenant's own reads and one INSERT.
	set := s.policies.forTenant(ctx, req.SessionTenantID)

	var (
		facts tapFacts
		dec   tap.Decision
		id    uuid.UUID
	)
	err = s.data.WithTenant(ctx, req.SessionTenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := store.New(tx).LockEmployeeForTap(ctx, store.LockEmployeeForTapParams{
			TenantID: req.SessionTenantID, EmployeeID: req.EmployeeID,
		}); e != nil {
			return fmt.Errorf("lock employee: %w", e)
		}

		facts, err = s.gather(ctx, tx, req, tappedWall(tagRow.LocationID))
		if err != nil {
			return err
		}

		// --- 4. Decide (one pure call, no rules here) ------------------------
		in := tap.Input{
			Now:                  now,
			OccurredAt:           occurredAt,
			OccurredAtFromClient: fromClient,
			PageIssuedAt:         req.PageIssuedAt,
			Channel:              req.Channel,
			SUN:                  tap.SUNResult{Valid: sunValid, CtrGap: int(ctrGap)},
			Tag: &tap.Tag{
				Status:   tap.TagStatus(tagRow.Status),
				Location: tappedWall(tagRow.LocationID),
			},
			Employee:                    facts.employee,
			TagTenantID:                 &tagRow.TenantID,
			SessionTenantID:             &req.SessionTenantID,
			SourceIP:                    req.SourceIP,
			LocationIPs:                 facts.locationIPs,
			GPS:                         req.GPS,
			LocationGPS:                 facts.locationGPS,
			GPSRadiusM:                  s.gpsRadiusM,
			LastForPerson:               facts.lastForPerson,
			SecondsSinceLastRecordedTap: facts.secondsSinceLastRecordedTap,
			LastOpenIn:                  facts.lastOpenIn,
			Shift:                       facts.shift,
			Debounce:                    s.debounce,
			PolicySet:                   set,
		}
		dec = tap.Decide(in)

		// --- 5. Apply the decision -------------------------------------------
		if dec.Redirect != tap.RedirectNone {
			// The ONLY path that writes nothing (§4.6's single exception). The
			// transaction still commits — it wrote nothing to commit.
			return nil
		}
		id, err = s.write(ctx, tx, req, tagRow, facts, dec, occurredAt)
		return err
	})
	if err != nil {
		// NEVER SWALLOWED (§7). The caller answers "try again" and the person taps
		// again; what must not happen is a screen that says "done" over a record
		// that does not exist.
		return Result{}, err
	}

	if dec.Redirect != tap.RedirectNone {
		// Which of the two redirects it is comes from the deciding sid, because
		// Decide maps both to RedirectActivation.
		out := Result{
			Outcome: OutcomeActivation, Decision: dec,
			OccurredAt: occurredAt, Timezone: facts.timezone,
			BusinessType: facts.businessType,
		}
		if dec.MatchedSid == sidTenantMismatch {
			out.Outcome = OutcomeForeignTenant
			s.log.Warn("checkin: a session tapped another organisation's plaque; no record written",
				"tag_uid", req.TagUID, "employee_id", req.EmployeeID,
				"session_tenant_id", req.SessionTenantID, "tag_tenant_id", tagRow.TenantID)
		}
		return out, nil
	}

	if dec.Security {
		// §5 row 4: the attempt is rejected, RECORDED, and raised. The audit row
		// comes after the transaction row on purpose — the trail refers to a record
		// that exists.
		s.record(ctx, audit.Event{
			TenantID: req.SessionTenantID,
			Action:   ActionTapSecurityAlert,
			Target:   id.String(),
			Detail: securityAlertDetail{
				Sid:      dec.MatchedSid,
				Verdict:  string(dec.Verdict),
				Employee: req.EmployeeID.String(),
				TagUID:   req.TagUID,
			},
		})
	}

	return Result{
		Outcome:       OutcomeRecorded,
		Decision:      dec,
		TransactionID: id,
		OccurredAt:    occurredAt,
		LocationName:  facts.locationName,
		Timezone:      facts.timezone,
		BusinessType:  facts.businessType,
	}, nil
}

// sidTenantMismatch is the guardrail whose redirect is NOT an activation
// redirect. It is written here rather than exported from internal/policy because
// this is a presentation distinction (which page to serve), not a policy one.
const sidTenantMismatch = "sys:tenant-mismatch"

// validate rejects a request its caller built wrong. These are programming
// errors at the handler boundary, not things a person tapping can cause (§7:
// external input is validated at the boundary, the domain sees valid data).
func (r Request) validate() error {
	switch {
	case r.SessionTenantID == uuid.Nil:
		// 🔴 THIS IS WHAT KEEPS sys:tenant-mismatch ALIVE. tap.Decide falls back to
		// a value-less presence marker when no session tenant is supplied, and on
		// that branch the isolation guardrail is inert. Refusing the request makes
		// the fallback unreachable from production rather than merely unlikely.
		return fmt.Errorf("%w: session tenant is required", ErrInvalidRequest)
	case r.EmployeeID == uuid.Nil:
		return fmt.Errorf("%w: employee is required", ErrInvalidRequest)
	case r.Channel == tap.ChannelManual && (r.EnteredBy == nil || *r.EnteredBy == uuid.Nil):
		return ErrEnteredByRequired
	case r.Channel != tap.ChannelManual && r.TagUID == "":
		return fmt.Errorf("%w: a tap needs a tag", ErrInvalidRequest)
	}
	return nil
}

// advance runs §4.4's replay guard and returns the SUN verdict.
//
// THE CONDITIONS ARE THE SAME THREE sun.Verify APPLIES, in the same order, and
// each removes a way to move a counter that must not move:
//
//	channel != nfc      a QR tap has no counter to advance, and a manual record
//	                    has no chip at all.
//	!CMACVerified       advancing on an unverified MAC is the §4.4 denial of
//	                    service: push last_ctr past the chip's real value with
//	                    rubbish and every genuine tap afterwards is a replay.
//	status != active    a retired or lost plaque is §5 row 1 and never reaches
//	                    cryptography or the counter (sun.Verify short-circuits at
//	                    the same point).
//
// A REPLAY IS NOT AN ERROR. ErrReplay means the counter did not advance — the
// tap is SUN-invalid and lands on §5 row 2 as a recorded reject. Only an
// infrastructure failure is returned as an error, because only that leaves us
// unable to say whether the counter moved.
func (s *Service) advance(ctx context.Context, req Request, tagRow db.ResolvedTag) (sunValid bool, gap int32, err error) {
	if req.Channel != tap.ChannelNFC || !req.CMACVerified || tagRow.Status != string(tap.TagActive) {
		return false, 0, nil
	}
	// 🔴 THE FOURTH CONDITION, ADDED AFTER AN AUDIT MEASURED THE THIRD ONE MISSING
	// IT: a session belonging to ANOTHER organisation must not move this tag's
	// counter. Measured before the fix — a tenant-B session tapping a tenant-A
	// plaque left A's last_ctr at 901 (from 900) with ZERO transactions and ZERO
	// audit rows in EITHER tenant, because the advance ran first, inside
	// WithTenant(tagRow.TenantID), i.e. under the OTHER tenant's RLS context. One
	// tenant changing another's row with no trace on either side is a §4.5
	// failure regardless of how small the value is.
	//
	// THE CONCRETE HARM IS NARROW BUT REAL, and worth stating rather than waving
	// at: the counter can only move to a value the chip genuinely emitted (the
	// context is signed and the CMAC was verified), so this is not free
	// advancement — it costs a physical touch of that plaque. What it buys is
	// SPENDING a counter value out from under tenant A: their employee, holding
	// an already-open tap page for that same read, now posts and is told their
	// tap is a replay — a recorded reject in place of a good check-in.
	//
	// WHY THE CHECK IS HERE AND NOT A DECISION. This is not a verdict, it is
	// SCOPE: "do not write into a tenant this request does not belong to", the
	// same class of rule as "a dead plaque never reaches cryptography". The
	// VERDICT for a cross-tenant tap is still sys:tenant-mismatch's, decided by
	// the policy engine below, and this function does not short-circuit it —
	// Decide still runs, still redirects, and still produces no record.
	if tagRow.TenantID != req.SessionTenantID {
		return false, 0, nil
	}
	err = s.data.WithTenant(ctx, tagRow.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		// ONE STATEMENT, and the comparison lives inside it. Nothing here reads
		// last_ctr into Go and compares it — that is the TOCTOU §4.4 forbids by
		// name, and it is why db.ResolvedTag.LastCtr is deliberately never touched
		// on this path even though it is sitting right there in tagRow.
		gap, e = sun.AdvanceCounter(ctx, s.advancer(tx), tagRow.TenantID, tagRow.UID, req.Ctr)
		return e
	})
	switch {
	case err == nil:
		return true, gap, nil
	case errors.Is(err, sun.ErrReplay):
		return false, 0, nil
	default:
		return false, 0, fmt.Errorf("checkin: advance counter: %w", err)
	}
}

// tapFacts is everything the decision needs that lives in the tenant's own rows.
type tapFacts struct {
	employee    *tap.Employee
	locationIPs []netip.Prefix
	locationGPS *geo.Point
	// locationResolved says the tapped location was READABLE in this tenant. It is
	// its own flag rather than "locationName != ''" because a venue may legitimately
	// be named "" one day, and because what the write path needs to know is whether
	// the row exists — not whether it has a display name.
	locationResolved bool
	locationName     string
	timezone         string
	// businessType is carried for the RENDER layer only (Result.BusinessType).
	// It is deliberately not put into tap.Input.
	businessType  string
	departmentID  *uuid.UUID
	shift         *tap.Shift
	lastForPerson *tap.Transaction
	lastOpenIn    *tap.Transaction
	// secondsSinceLastRecordedTap is how long ago this person's most recent nfc/qr
	// record was written, measured on the DATABASE clock — the one debounce leg no
	// caller can steer, and the one that survives lock contention (ADR 0006).
	secondsSinceLastRecordedTap *float64
}

// gather reads the tenant-scoped evidence in ONE transaction, so every fact the
// decision is made from comes from the same snapshot: a shift cannot change
// between reading the employee and reading the location.
//
// IT RUNS IN THE SESSION'S TENANT, not the tag's. For an ordinary tap they are
// the same. For a plaque belonging to another organisation they are not, and then
// the location read returns NO ROW — so the decision is never made on another
// tenant's evidence, and the record (which is not written on that path anyway)
// could not cite another tenant's location even if it were. Refusing such a tap
// is sys:tenant-mismatch's job, not this function's.
//
// THE TAPPED LOCATION COMES FROM THE FRESH TAG ROW, not from the signed context,
// for the same reason the status does: a plaque can be re-mounted, and the venue
// a tap is judged against must be where the plaque is NOW.
// tappedWall flattens the plaque's nullable wall for the decision engine, and it
// is a NAMED FUNCTION rather than two inline dereferences on purpose.
//
// 🔴 THE POINTER STOPS HERE, AND WHY IT STOPS HERE IS THE WHOLE COMMENT.
// db.ResolvedTag.LocationID is a *uuid.UUID because the DRIVER cannot report a SQL
// NULL any other way: measured on pgx v5.10.0, `SELECT NULL::uuid` into a
// uuid.UUID gives err=nil and uuid.Nil, so a plaque sitting in its box arrived
// looking like one mounted at "location zero" (M6-06 phase B closed that at the
// scan boundary). tap.Tag.Location stays FLAT because the decision engine reads it
// for exactly two things — building the `location/<id>` policy resource and
// comparing the tapped venue with the employee's home venue — and BOTH are
// unreachable for a plaque with no wall: §5 row 1 denies every status but `active`,
// and tags_active_requires_location (00013) forbids an active plaque from lacking a
// location. So the flattening is total by construction rather than by hope.
//
// ⚠️ WHAT uuid.Nil THEN MEANS DOWNSTREAM, stated because it is the value this
// function deliberately produces: GetLocationForTap finds no row, locationResolved
// stays false, no IP range and no coordinate reach the decision, and the tap is
// RECORDED without a location_id (§4.6) rather than dropped or crashed. That is the
// same path a plaque belonging to another tenant already takes.
func tappedWall(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

func (s *Service) gather(ctx context.Context, tx pgx.Tx, req Request, tappedLocationID uuid.UUID) (tapFacts, error) {
	var f tapFacts
	err := func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)

		emp, err := q.GetEmployeeForTap(ctx, store.GetEmployeeForTapParams{
			TenantID: req.SessionTenantID, EmployeeID: req.EmployeeID,
		})
		if err != nil {
			// A session resolved to an employee that this tenant cannot see is not a
			// tap outcome, it is a broken invariant (the session row carries a
			// same-tenant FK to the employee). Surfaced, never guessed at.
			return fmt.Errorf("load employee: %w", err)
		}
		f.employee = &tap.Employee{
			Status:     tap.EmployeeStatus(emp.Status),
			Location:   &emp.LocationID,
			Department: emp.DepartmentID,
		}
		if emp.ActivatedAt != nil {
			f.employee.ActivatedAt = *emp.ActivatedAt
		}
		f.departmentID = emp.DepartmentID
		f.timezone = emp.TenantTimezone
		f.businessType = emp.TenantBusinessType

		// The TAPPED location — the venue whose IP, GPS and shift this tap is
		// matched against (§5), never the employee's profile location.
		loc, err := q.GetLocationForTap(ctx, store.GetLocationForTapParams{
			TenantID: req.SessionTenantID, ID: tappedLocationID,
		})
		switch {
		case err == nil:
			f.locationResolved = true
			f.locationIPs = loc.StaticIps
			f.locationName = loc.Name
			f.locationGPS = point(loc.GpsLat, loc.GpsLng)
			f.shift = shiftOf(loc.ShiftStart, loc.ShiftEnd, loc.Overnight, emp.TenantTimezone)
		case errors.Is(err, pgx.ErrNoRows):
			// Another tenant's plaque, or a location this tenant cannot see. No IP
			// range, no coordinate, no shift: there is no evidence to judge against,
			// which is the honest state. sys:tenant-mismatch decides what happens.
		default:
			return fmt.Errorf("load tapped location: %w", err)
		}

		// The department shift BEATS the location shift when there is one (§5, Q17).
		if emp.DepartmentID != nil {
			dept, err := q.GetDepartmentShift(ctx, store.GetDepartmentShiftParams{
				TenantID: req.SessionTenantID, ID: *emp.DepartmentID,
			})
			switch {
			case err == nil:
				if sh := shiftOf(dept.ShiftStart, dept.ShiftEnd, dept.Overnight, emp.TenantTimezone); sh != nil {
					f.shift = sh
				}
			case errors.Is(err, pgx.ErrNoRows):
				// No department row visible: keep the location shift.
			default:
				return fmt.Errorf("load department shift: %w", err)
			}
		}

		// The person's own history: the debounce basis and the direction basis.
		last, err := q.GetLastTransactionForEmployee(ctx, store.GetLastTransactionForEmployeeParams{
			TenantID: req.SessionTenantID, EmployeeID: &req.EmployeeID,
		})
		switch {
		case err == nil:
			f.lastForPerson = transaction(last)
		case errors.Is(err, pgx.ErrNoRows):
			// First tap ever: nil never debounces (§4.6 — the safe zero value).
		default:
			return fmt.Errorf("load last tap: %w", err)
		}

		// THE SERVER-CLOCK LEG OF THE DEBOUNCE (ADR 0006), and it is a SEPARATE
		// query on purpose. The row above is chosen by ORDER BY occurred_at — a
		// client-declarable column — so a caller can steer which record is treated
		// as the predecessor as well as how far away it claims to be. This one is
		// ordered by created_at over the nfc/qr channels, so neither its value nor
		// its selection is reachable from a POST body.
		// The window is the SAME one the guardrail applies, so bounding the query
		// cannot change a decision: anything at or beyond it would have lost the
		// min() anyway. It runs inside the advisory lock, so an unbounded sort over
		// a flooded person's whole history would be paid while holding a pool
		// connection (ADR 0006).
		since, err := q.SecondsSinceLastRecordedTap(ctx, store.SecondsSinceLastRecordedTapParams{
			TenantID: req.SessionTenantID, EmployeeID: &req.EmployeeID,
			WindowSeconds: s.debounce.Seconds(),
		})
		switch {
		case err == nil:
			f.secondsSinceLastRecordedTap = &since
		case errors.Is(err, pgx.ErrNoRows):
			// No recorded tap: nil never debounces (§4.6's safe zero value).
		default:
			return fmt.Errorf("load last recorded tap: %w", err)
		}

		open, err := q.GetLastOpenTransaction(ctx, store.GetLastOpenTransactionParams{
			TenantID: req.SessionTenantID, EmployeeID: &req.EmployeeID,
		})
		switch {
		case err == nil:
			// A PRACTICE record never holds the chain open (§5, M4-04): a training
			// tap must not make the next real tap a checkout.
			//
			// 🔴 THE QUERY DECIDES THIS NOW, AND UNTIL M5-11 IT DID NOT (ADR 0008).
			// GetLastOpenTransaction used to be practice-neutral and this branch was
			// the whole enforcement — but it only ever saw ONE row, so discarding a
			// practice row meant NOT LOOKING AT THE ONE BENEATH IT. A practice row
			// that merely sorted newest hid a real, still-open check-in: the next
			// tap became an `in`, the entry never closed, and nothing said so.
			// `AND NOT t.practice` moved the question to where the ordering is.
			//
			// The check below therefore CANNOT be false today, and it stays anyway —
			// but be precise about WHY, because the tempting reason is the wrong one.
			// It is NOT a safety net for "someday the predicate gets dropped": that
			// cannot happen quietly. MEASURED (M5-11, second round) — replacing the
			// predicate with `AND TRUE` turns THREE named tests red across two
			// packages: TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn (two
			// subtests), TestSeedDB_APracticeRowNeverHidesAnOlderOpenCheckIn, and
			// TestSeedDB_ADayAtKFStJulians ("Ivan's 02:10 tap = in, want out").
			//
			// The real reason is the PACKAGE BOUNDARY CONTRACT: tap.Input.LastOpenIn
			// is documented as the last open NON-PRACTICE check-in, and this line is
			// what a caller does to make that documented sentence true — the engine
			// is a pure function that cannot verify its own inputs. Unreachable code
			// that is CORRECTLY LABELLED is safer than reachable code that is
			// mislabelled, and this whole ADR exists because of the second kind (see
			// resolveDirection's old "primary enforcement" comment). It is NOT what
			// pins the fix — TestGatherDB_APracticeRowDoesNotHideAnOlderOpenCheckIn
			// is, and deleting this branch alone leaves the suite green (measured).
			if !open.Practice {
				f.lastOpenIn = transaction(open)
			}
		case errors.Is(err, pgx.ErrNoRows):
			// No open check-in: this tap is an `in`.
		default:
			return fmt.Errorf("load open check-in: %w", err)
		}
		return nil
	}(ctx, tx)
	if err != nil {
		return tapFacts{}, fmt.Errorf("checkin: gather: %w", err)
	}
	return f, nil
}

// write appends the record. ONE INSERT, no UPDATE, ever (§4.3).
//
// EVERY DECIDED TAP LANDS HERE — ok, flag, reject and ignored alike. §4.6 is not
// "record the good ones": a reject is the outcome the record matters MOST for,
// because it is the one somebody will dispute.
func (s *Service) write(ctx context.Context, tx pgx.Tx, req Request, tagRow db.ResolvedTag, f tapFacts, dec tap.Decision, occurredAt time.Time) (uuid.UUID, error) {
	params, err := s.insertParams(req, tagRow, f, dec, occurredAt)
	if err != nil {
		return uuid.Nil, err
	}
	row, err := store.New(tx).InsertTransaction(ctx, params)
	if err != nil {
		return uuid.Nil, fmt.Errorf("checkin: record tap: %w", err)
	}
	return row.ID, nil
}

// insertParams maps a Decision onto the record's columns.
//
// 🔴 FIDELITY IS THE WHOLE JOB HERE (hand-off N4). Migration 0008's consistency
// CHECK and composite FK only protect §4.6 if this mapping is faithful:
//
//	guardrail / default  policy_version_id NULL, matched_sid names the rule
//	                     ("sys:..." for a guardrail, "default" for the built-in
//	                     fallback — matched_sid is what tells them apart, since
//	                     both carry layer=guardrail).
//	baseline / tenant    policy_version_id present and resolvable in THIS tenant.
//
// Anything else is refused by the database rather than written wrong, and a
// refusal is a lost record. Decision carries exactly these shapes, so the mapping
// below is a straight copy — the danger was never the copy, it was inventing a
// value (a uuid.Nil version id) on the way.
//
// §4.7: policy_context carries a GPS DISTANCE IN METRES, never a coordinate,
// because the map it comes from never held one. The coordinate itself goes into
// gps_lat/gps_lng, which is a database column and not a log line.
func (s *Service) insertParams(req Request, tagRow db.ResolvedTag, f tapFacts, dec tap.Decision, occurredAt time.Time) (store.InsertTransactionParams, error) {
	snapshot, err := json.Marshal(dec.PolicyContext)
	if err != nil {
		// Never silently dropped: the snapshot is what makes a past verdict
		// replayable, and migration 0008 says it can never be back-filled.
		return store.InsertTransactionParams{}, fmt.Errorf("checkin: marshal policy context: %w", err)
	}

	p := store.InsertTransactionParams{
		TenantID:   req.SessionTenantID,
		EmployeeID: &req.EmployeeID,
		OccurredAt: occurredAt,
		Verdict:    string(dec.Verdict),
		Channel:    string(req.Channel),
		EnteredBy:  req.EnteredBy,
		Practice:   dec.Practice,
		// transactions.queued is the APPROVAL queue (migration 00005: flag -> true),
		// NOT the offline queue. The policy key tap:queued means the other thing and
		// is derived from the occurred_at skew; the two are kept apart deliberately.
		Queued:          dec.Verdict == tap.VerdictFlag,
		IpMatch:         &dec.IPMatch,
		GpsMatch:        &dec.GPSMatch,
		PolicyVersionID: dec.PolicyVersionID,
		MatchedSid:      strPtr(dec.MatchedSid),
		PolicyLayer:     strPtr(string(dec.Layer)),
		PolicyContext:   snapshot,
	}
	// sun_valid is THREE-STATE and the third state is the point (migration 00005:
	// "NULL = not evaluated for this channel"). An NFC or QR tap really was
	// judged — false on a QR arrival is the M5-08 criterion and a fact, not an
	// omission — but SUN is MEANINGLESS for a manual record: there is no chip, so
	// writing false would say "we checked and it failed" about a check that never
	// applied. The value comes from the frozen snapshot rather than from a second
	// parameter, so the column and the tap:sunValid the guardrails saw are the
	// same value and cannot disagree.
	if req.Channel != tap.ChannelManual {
		p.SunValid = boolPtr(dec.PolicyContext[policy.CtxTapSunValid])
	}
	if trust := dec.Trust; trust >= 0 {
		t := int16(trust) //nolint:gosec // 20..100 by construction (§5), asserted in the domain tests
		p.Trust = &t
	}
	if dec.Type != nil {
		t := string(*dec.Type)
		p.Type = &t
	}
	if dec.Note != "" {
		n := dec.Note
		p.Note = &n
	}
	if req.SourceIP.IsValid() {
		ip := req.SourceIP.Unmap()
		p.SourceIp = &ip
	}
	if req.Channel != tap.ChannelManual {
		uid := tagRow.UID
		p.TagUid = &uid
	}
	if req.Channel == tap.ChannelNFC {
		// A QR arrival has no chip counter and a manual record has no chip. Writing
		// 0 would say "read number zero", which is a different claim from "there was
		// no counter".
		c := int32(req.Ctr) //nolint:gosec // 24-bit chip counter (sun.Params), always inside int32
		p.Ctr = &c
	}
	// location_id is written ONLY when the tapped location RESOLVED inside this
	// tenant (f.locationResolved). The composite FK (location_id, tenant_id) would
	// refuse a foreign one anyway — and a refused INSERT is a lost record, so the
	// belt is here and the braces are in the schema.
	if f.locationResolved && tagRow.LocationID != nil {
		// THE SECOND CONDITION IS BELT, NOT BRACES, and it is written out because the
		// two facts have different sources: locationResolved says GetLocationForTap
		// found a row, and the pointer says the plaque had a wall at all. They cannot
		// disagree — a nil wall resolves to nothing — but the belt costs one comparison
		// and removes any need for the reader to prove that.
		p.LocationID = tagRow.LocationID
	}
	p.DepartmentID = f.departmentID
	if req.GPS != nil {
		lat, err := numeric(req.GPS.Lat)
		if err != nil {
			return store.InsertTransactionParams{}, err
		}
		lng, err := numeric(req.GPS.Lng)
		if err != nil {
			return store.InsertTransactionParams{}, err
		}
		p.GpsLat, p.GpsLng = lat, lng
	}
	return p, nil
}

// record appends an audit event, never failing the tap over it.
//
// The attendance record is already written by the time this runs, so a failure
// here costs a line in the trail and not an hour on a payslip — but it is LOGGED
// AS AN ERROR rather than swallowed (§7), because a trail that is not being
// written means §4.6 is not being kept even when the record is.
func (s *Service) record(ctx context.Context, e audit.Event) {
	if _, err := s.trail.Record(ctx, e); err != nil {
		s.log.Error("checkin: audit write failed", "action", e.Action, "err", err)
	}
}

// Typed audit details, hand-written per event with known fields — internal/audit's
// rule, and the reason no secret can reach the trail: there is no field for one.
type (
	securityAlertDetail struct {
		Sid      string `json:"sid"`
		Verdict  string `json:"verdict"`
		Employee string `json:"employee_id"`
		TagUID   string `json:"tag_uid"`
	}
	unknownTagDetail struct {
		Channel string `json:"channel"`
	}
)

// --- small mappers -----------------------------------------------------------

// transaction maps a stored row onto the pure domain shape the decision reads.
// It carries only what direction and debounce need; verdict, tenant and location
// stay in the store layer.
func transaction(t store.Transaction) *tap.Transaction {
	out := &tap.Transaction{
		ID:         t.ID,
		OccurredAt: t.OccurredAt,
		Practice:   t.Practice,
	}
	if t.Type != nil {
		out.Direction = tap.Type(*t.Type)
	}
	return out
}

// shiftOf turns a location's or department's stored shift into the domain shape,
// or nil when there is none.
//
// nil MEANS "LATENESS IS NOT COMPUTED", which is different from "on time" — the
// distinction tap.Decide keeps by making MinutesLate a pointer. A half-filled
// shift (a start with no end) is also nil: migration 00002's shift_pair CHECK
// refuses that shape anyway, and guessing an end would invent a wall clock.
func shiftOf(start, end pgtype.Time, overnight bool, timezone string) *tap.Shift {
	if !start.Valid || !end.Valid {
		return nil
	}
	return &tap.Shift{
		Start:     time.Duration(start.Microseconds) * time.Microsecond,
		End:       time.Duration(end.Microseconds) * time.Microsecond,
		Overnight: overnight,
		Timezone:  timezone,
	}
}

// point turns a stored numeric coordinate pair into a domain point, or nil when
// either half is absent (a location with no registered coordinate).
func point(lat, lng pgtype.Numeric) *geo.Point {
	if !lat.Valid || !lng.Valid {
		return nil
	}
	latF, err := lat.Float64Value()
	if err != nil || !latF.Valid {
		return nil
	}
	lngF, err := lng.Float64Value()
	if err != nil || !lngF.Valid {
		return nil
	}
	return &geo.Point{Lat: latF.Float64, Lng: lngF.Float64}
}

// numeric renders a coordinate for the numeric(9,6) columns.
//
// It goes through a DECIMAL STRING rather than a float parameter: the column has
// six decimal places, which is the ~11 cm resolution a coordinate is stored at,
// and handing pgx a float64 leaves the rounding to two layers that disagree about
// it. Six places is also exactly what a valid coordinate needs (three integer
// digits at most), so nothing is lost.
func numeric(v float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(v, 'f', 6, 64)); err != nil {
		return pgtype.Numeric{}, fmt.Errorf("checkin: coordinate is not storable: %w", err)
	}
	return n, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// boolPtr reads a bool out of the frozen policy context. It reads the SNAPSHOT
// rather than taking sun validity as a second parameter so that the sun_valid
// COLUMN and the tap:sunValid the guardrails saw can never disagree — they are
// the same value, read from the same map.
func boolPtr(v any) *bool {
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}
