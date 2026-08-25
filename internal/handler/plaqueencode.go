package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/encode"
	"github.com/atknatk/tappa/internal/httpx"
)

// THE PLAQUE ENCODE RELAY — ADR 0017's HTTP half (M8-05 FAZ B2c-2b).
//
// internal/encode holds a live EV2 session and produces one C-APDU at a time; this
// file is the only thing that drives it. The phone is a pipe (ADR 0017 §2.1): it
// pushes the bytes this endpoint returns at the chip and posts the chip's answer
// back. It computes nothing and carries no key.
//
//	POST /admin/plaques/encode        open a round, get the ISO SELECT
//	POST /admin/plaques/encode/step   post one R-APDU, get the next C-APDU
//	POST /admin/plaques/encode/abort  end a round early
//
// 🔴 WHY IT IS UNDER /admin AND NOT UNDER /api. The panel session cookie is
// Path=/admin (adminauth.CookiePath), so a route outside that prefix arrives with no
// cookie and is permanently signed out. That is also what makes ADR 0017 §6 md. 10
// answerable at all: the identity this endpoint authorises against is the panel's.
//
// 🔴 THE RELAY MUST SEND AN Origin HEADER, AND THIS IS A CONTRACT RATHER THAN A
// DETAIL. These are mutating routes, so they mount inside mountWriting and inherit
// AdminAuth.ProtectWriting — whose sameOriginGate refuses a request that carries
// neither an Origin nor a Sec-Fetch-Site of same-origin/same-site. A native Android
// client sends neither by default. Measured, both ways
// (TestPlaqueEncode_TheRelayMustDeclareItsOrigin): with no Origin the round never
// starts. The relay sets `Origin: <TAPPA_BASE_URL>` on every request; it is one
// header on a client we write ourselves, and the alternative — exempting these three
// routes from the panel's write chain — would make them the only mutating panel
// routes with no CSRF defence at all.
//
// ⚠️ THREE REFUSALS AHEAD OF THIS FILE ANSWER IN THE PANEL'S SHAPE, NOT IN THIS
// ONE, and a relay author needs to know it: floodGate and sessionGate render an HTML
// problem page with 429, and sameOriginGate answers 303 to /admin. They are the
// panel's chain and this endpoint deliberately does not fork it. THE RELAY READS THE
// STATUS CODE, NEVER THE BODY, for anything other than 200.
//
// 🔴 WHAT THIS FILE DOES NOT DO: it never reads a session's contents. The consumer
// interface below names exactly three methods, and none of them can return anything
// from inside a round. That is the mechanism behind a rule an audit paid for in
// B2c-2a — internal/encode's keyring is NOT protected by the store's mutex (its owner
// is whichever goroutine holds s.busy), so a "what is this session holding" surface
// that took st.mu and walked the ring would be a DATA RACE ON LIVE KEY MATERIAL,
// reproduced under -race. Store.Live() is safe and is deliberately absent anyway:
// a method that is not on the interface cannot be reached by an edit to this file
// alone, so widening it is a visible change somewhere a reviewer looks.

// PlaqueEncoder is the slice of *encode.Store this file needs, declared HERE at the
// consumer (CLAUDE.md §7).
//
// 🔴 THE METHOD SET IS THE GATE, see the paragraph above. It is pinned by
// TestPlaqueEncoder_NamesExactlyTheThreeMethodsTheRelayNeeds.
//
// 🔴 IT IS EXPORTED WHILE EVERY OTHER NARROW INTERFACE IN THIS PACKAGE IS NOT, AND
// THE REASON IS GO'S TYPED NIL RATHER THAN TASTE. This is the one dependency that may
// legitimately be absent (see AdminAuth.encoder). If cmd/tappa passed a nil
// *encode.Store, the interface field would hold a non-nil interface wrapping a nil
// pointer: `a.encoder == nil` would be FALSE and the first request would call Begin
// on a nil receiver and panic on its own mutex. Exporting the type lets the wiring
// hold a genuinely nil interface, so the absent case is a value rather than a trap.
type PlaqueEncoder interface {
	Begin(ctx context.Context, tenantID, adminID uuid.UUID, actor string) (encode.ID, encode.Progress, error)
	Step(ctx context.Context, id encode.ID, rapdu []byte) (encode.Progress, error)
	Abort(id encode.ID)
}

// The three routes. They are constants for the reason adminLoginPath is one: the
// test that walks mountWriting reads them, and so will the relay's own contract test.
const (
	plaqueEncodeHref      = "/admin/plaques/encode"
	plaqueEncodeStepHref  = "/admin/plaques/encode/step"
	plaqueEncodeAbortHref = "/admin/plaques/encode/abort"
)

// encodeMaxBody bounds the request body BEFORE it is parsed, the same shape
// checkinMaxBody gives the tap surface.
//
// The largest body this endpoint ever legitimately receives is a session handle (32
// hex characters), a form key and one R-APDU. The chip's biggest response in ADR 0017
// §5.1 is AuthenticateEV2First part one — 16 bytes of ciphertext plus a status word —
// so 8 KiB is roughly two hundred times the traffic and is chosen to match
// checkinMaxBody rather than to be tight.
const encodeMaxBody = 8 << 10

// encodeMaxRAPDUBytes bounds the DECODED R-APDU.
//
// ⚠️ IT IS NOT THE SAME BOUND AS encodeMaxBody AND BOTH ARE NEEDED. The body limit
// stops a large upload from being read; this one stops a well-formed 4 KiB hex string
// from being handed to internal/sun as a 2 KiB response frame. 512 is twice the 258
// bytes a short ISO 7816 R-APDU can carry (256 data + SW1SW2), which is itself far
// more than any step in ADR 0017 §5.1 produces.
const encodeMaxRAPDUBytes = 512

// The FAULT VOCABULARY — the complete set of words this endpoint may put in a
// response body, and the reason it is a set at all.
//
// 🔴 THIS IS ADR 0017 §6 md. 14's SECOND HALF, AND IT IS WHERE THE CONTENT QUESTION
// LIVES. The open item is that nothing MECHANICAL bounds what leaves the process; the
// three protections that exist bind NAMED CHANNELS (internal/encode carries no logger,
// its error messages carry no key bytes, redline-check R7 scans log calls). This
// vocabulary bounds one of the four scalars the writers below take: `fault` may only
// be one of the constants here, which is asserted over the whole PACKAGE by
// TestPlaqueEncode_WritesOnlyDeclaredFaults — both the NAMES at every call site and
// the VALUES themselves.
//
// ⚠️ TWO SENTENCES THAT STOOD HERE WERE MECHANICALLY FALSE, ONE PER ROUND, AND BOTH
// ARE KEPT AS RETRACTIONS RATHER THAN DELETED — they are the reason the writers below
// look the way they do.
//
//	ROUND 1  "An error string never reaches a body, because there is no field of that
//	         shape to put one in." True of the two structs; FALSE of the writer, which
//	         took `body any`.
//	ROUND 2  "The answer is TWO TYPES AND NO OTHERS, enforced by the sealed encodeBody
//	         interface." FALSE: embedding promotes the sealing method, so
//	         `struct{ encodeReply; Leak string }` satisfied it without declaring
//	         anything, and encoding/json flattened the leak onto the wire.
//
// A claim about what may be serialised has to be checked against every SIGNATURE that
// can reach the wire, not against the types one has in mind. The answer this round is
// that no signature takes a caller-composed STRUCT. It does not answer what a
// handler can do with the writer itself — see the counted list at encodeReply.
//
// Errors still have to be diagnosable, so they go to the LOG — where redline-check.sh
// R7 already scans — and the caller gets a word.
const (
	// faultUnavailable: this deployment has no encode surface. See NewAdminAuth.
	faultUnavailable = "encode-unavailable"
	// faultBadRequest: the body was missing, oversized, or not the shape asked for.
	faultBadRequest = "bad-request"
	// faultUnknownSession: no live round for that handle — never seen, finished,
	// aborted or expired. internal/encode refuses to distinguish the four and so
	// does this (ErrUnknownSession's own comment: the handle is a bearer credential).
	faultUnknownSession = "unknown-session"
	// faultBusy: a concurrency limit refused the round — another step in flight, another
	// round already open on this plaque, or the store at capacity.
	faultBusy = "busy"
	// faultRefused: the round failed on its own terms. The chip answered wrongly, the
	// relay lied about a UID, or a gate in driver.go refused. NOT retryable as-is.
	faultRefused = "refused"
	// faultTooMany: the encode budget for this panel session is spent.
	faultTooMany = "too-many-rounds"
	// faultServer: an internal failure. Nothing more is said, deliberately.
	faultServer = "server-error"
)

// encodeFaults is the SAME LIST as a value, so a test can assert that every fault
// this file writes is one of them. Declaring it beside the constants rather than
// deriving it in the test is deliberate: the test's job is to catch a NEW word, and a
// list derived from the source would grow with one.
var encodeFaults = []string{
	faultUnavailable, faultBadRequest, faultUnknownSession,
	faultBusy, faultRefused, faultTooMany, faultServer,
}

// encodeReply is the ONE successful body this endpoint produces.
//
// 🔴 FOUR SCALAR FIELDS AND NO OTHERS, WHICH IS THE POINT OF THE TYPE. There is no
// []byte, no map, no any, no nested struct and no error field — so the question "could
// a key end up in the response" has a structural answer rather than a careful one.
// TestEncodeReply_HasExactlyTheFourFieldsTheRelayNeeds asserts the shape by
// reflection; a fifth field turns it red.
//
// Command IS the byte stream that goes on the wire, and that is exactly ADR 0017
// §2.1's boundary: the phone is handed a SEALED C-APDU and nothing else. What makes
// that a claim rather than a hope is measured one layer down, over every step of the
// round, by internal/encode's TestRelay_NoPlaintextKeyMaterialEverReachesTheWire.
//
// ⚠️ Progress.UIDHex IS DELIBERATELY NOT CARRIED, even though ADR 0003 md. 1 makes a
// uid public and an operator screen would want it. This body is machine-facing and
// the narrowest thing that works is the thing to ship; the uid is on the plaque, in
// the panel's plaque list, and in both audit_log rows the round writes.
type encodeReply struct {
	// Session is the round's handle. It is a BEARER CREDENTIAL (encode.ID says so),
	// which is why it appears here — the relay cannot continue without it — and
	// nowhere else: it is never logged and never put in an error.
	Session string `json:"session"`
	// Command is the next C-APDU, hex. Empty exactly when the round is over.
	Command string `json:"command"`
	// Step names the exchange that just completed. Its values are the ten names in
	// internal/encode's step table and nothing else.
	Step string `json:"step"`
	// Done reports that the CHIP IS PERSONALISED. Read it BEFORE reading anything
	// else — see encode.Progress.Done: a round can be Done AND report an error, and
	// in that case it must NOT be re-run.
	Done bool `json:"done"`
}

// encodeFault is the ONE failure body. One field, and its value is always one of the
// constants above.
type encodeFault struct {
	Fault string `json:"fault"`
}

// 🔴 THERE IS NO "encodeBody" TYPE ANY MORE, AND ITS REMOVAL IS THE POINT RATHER THAN
// A TIDY-UP. Two designs stood here and two different auditors beat both:
//
//	DESIGN 1  writeEncodeJSON(w, status, body any)
//	          -> an anonymous struct with a fifth field went out on the abort path and
//	             the WHOLE PACKAGE stayed green (109.144s, ok).
//	DESIGN 2  a sealed interface: type encodeBody interface{ isEncodeBody() },
//	          implemented only by the two structs. The design-1 mutation became a
//	          COMPILE ERROR, and the claim written beside it — "an anonymous struct can
//	          never implement it" — was TRUE AND IRRELEVANT:
//
//	              struct{ encodeReply; Leak string }{body, "AUDITPROBE-…"}
//
//	          EMBEDS encodeReply, so the method is PROMOTED and the type satisfies the
//	          interface without declaring anything. encoding/json flattens the embedded
//	          struct, so the wire carried
//	          {"session":"","command":"","step":"abort","done":false,"leak":"AUDITPROBE-…"}
//	          measured on the real router, with all five gates PASSING.
//
// 🔴 THE LESSON, AND IT COST SEVEN ROUNDS ACROSS TWO TASKS TO LEARN IN THIS FORM: both
// designs asked "WHICH TYPE MAY BE SERIALISED", and type identity in Go is COMPOSABLE
// — embedding, wrapping, aliasing — so that question has no bottom. Improving the gate
// a third time would have been the same mistake a third time.
//
// SO THE SHAPE OF THE BODY IS NARROWED RATHER THAN THE QUESTION ANSWERED: the two
// writers below build their own literal, and no parameter carries a struct a caller
// composed. Nothing to embed, because no struct is passed.
//
// 🔴 AND THAT IS AS FAR AS IT GOES — A FOURTH AUDIT MEASURED THE REST AND THE HONEST
// WORD IS "COUNTED", NOT "CLOSED" (2026-08-24, tappa-security-auditor F1). Three
// things the gate does NOT bind, each produced end to end rather than reasoned about:
//
//	(a) THE HEADER, NOT JUST THE BODY. The writer scan exempts three functions, and an
//	    exempt function may do ANYTHING with w. One line at the top of
//	    writeEncodeReply — w.Header().Set("X-Audit-Probe", session+hex(command)) —
//	    put both on the wire (measured: status 200 and the header present), with
//	    -race green across three packages, go vet clean and redline-check exit 0.
//	    All five AST gates missed it: none of them is an Encode call and the writing
//	    function is exempt.
//	(b) `command` IS A VALUE, NOT A SCALAR. It is []byte, the caller builds it, and
//	    the call-site pin fixes the EXPRESSION rather than the CONTENT. Two of the
//	    three reply sites are covered by a behavioural test; the third — the p.Done
//	    branch of plaqueEncodeStep — is not, and a probe appended to p.Command there
//	    stayed green.
//	(c) THE LOG IS NOT BOUND AT ALL. redline-check R7 matches on keywords, so
//	    a.log.Info("…", "c", hex(command), "h", string(id)) passes every gate.
//
// ⚠️ AND A FOURTH GATE IS NOT THE ANSWER, WHICH IS THE ONE THING THIS FILE IS SURE OF.
// "Narrow the exempt list" and "pin the header names too" fall into the same class:
// the set of things a function holding w can do (Header · Write · WriteHeader ·
// Hijacker · Flusher · ResponseController · trailers · a panic message · slog · a
// field on some off-surface type) is composable and has no bottom. This is the same
// wall B2c-2a hit five times and this task has now hit three more.
//
// 🔴 THE BLAST RADIUS — AND THE FIRST VERSION OF THIS PARAGRAPH WAS WRONG IN THE
// DANGEROUS DIRECTION, WHICH MATTERS MORE THAN THE GATE DOES (fourth audit,
// 2026-08-24). It said: "No secret is reachable from this surface to leak … the worst
// an added line here could put on the wire is `command` and `session`." MEASURED, and
// false. These three handlers are METHODS ON *AdminAuth, and AdminAuth holds two
// signing keys as PLAIN FIELDS OF THE SAME PACKAGE:
//
//	a.choices.key   logincontext.go      hmac(SessionHMACKey, adminChoiceKeyLabel)
//	a.confirm.key   deactivateconfirm.go hmac(SessionHMACKey, adminConfirmKeyLabel)
//
// No exported accessor is needed — same package, plain read. An auditor combined that
// with channel (a) above (a handler stashes the value, an exempt writer puts it in a
// header) and got both keys onto the wire from POST …/abort with a 200, with build,
// vet, redline-check and all eighteen tests on this surface green. Reproduced here
// with an equality boolean rather than a value (§4.7): both are 32 bytes, both
// reachable, both hmac.Equal to their SessionHMACKey derivation.
//
// THOSE ARE NOT `command`-CLASS DATA. They sign the panel's synchronizer token, the
// verified-candidate set, and the deactivation confirmation — §4.7 material, nothing
// the relay is ever meant to see.
//
// 🔴 SO THE HONEST STATEMENT IS THE OPPOSITE ONE: this surface can reach every field
// of AdminAuth, because it is made of methods on it. What bounds a leak is not the
// absence of anything to leak; it is the three gates above plus the fact that nobody
// has written such a line. And that is exactly why md. 14 is COUNTED rather than
// closed — a claim that understates what is reachable makes a counted gap look
// smaller than it is, and `agent-brief`'s stopping rule 2 only holds while the count
// is right.
//
// ⚠️ WHAT REMAINS TRUE FROM THE OLD PARAGRAPH, NARROWED TO WHAT WAS MEASURED:
// AdminAuth does not retain *config.Config and never reads TagKEK, so the TAG key is
// not reachable this way; PlaqueEncoder's three METHODS return no session state — but
// the INTERFACE is not a wall, and the count says so rather than rounding it down
// (security audit, second pass): the field holds a *encode.Store, so a line here can
// type-assert past the three methods and reach anything the concrete type exports,
// e.g. Store.Live(). ⚠️ THAT IS A NUMBER, NOT A KEY — a live-round count, one
// process-wide integer, in the same class as `step` and nothing like a.confirm.key.
// It is written down because a count that quietly omits a reachable thing is the
// failure mode this whole paragraph exists to correct. Measured by
// TestEncodeSurface_TheEncoderInterfaceIsNotAWallButWhatItReachesIsACount; and
// encode.Progress's four fields (Command, Done, Step, UIDHex — the old text said
// three) are a sealed C-APDU, a flag, a step name and a public uid.
//
// ⚠️ WHAT THIS DOES NOT DECIDE AT ALL, and it is a different question rather than a
// smaller one: what goes INSIDE those values. `command` is answered one layer down by
// internal/encode's TestRelay_NoPlaintextKeyMaterialEverReachesTheWire, which drives a
// complete round and requires no plaintext key material in any of the ten C-APDUs;
// `fault` is answered by the constant vocabulary above; `session` is the bearer handle
// the relay cannot work without; `step` is a name from the driver's step table. The
// two gates are written down separately on purpose — a shape gate that is described as
// if it settled content is how the last three rounds went wrong.

// --- the authority ---------------------------------------------------------------

// plaqueEncodeGrant is ADR 0017 §6 md. 10, answered: WHO may encode, and FOR WHICH
// BUSINESS.
//
// 🔴 IT IS THE ONLY WAY A TENANT REACHES encode.Store.Begin FROM THIS PROCESS, AND
// THE VALUE COMES OFF THE RESOLVED PANEL SESSION — never off the request body, a
// query parameter, a header or a path segment. httpx.AdminIdentity.TenantID says the
// same rule from its own side: "IT IS THE OUTPUT OF RESOLUTION, NEVER AN INPUT (ADR
// 0002 madde 7)". A tenant taken from the body would not close md. 10, it would
// rename it — the caller would still be choosing which business to load a plaque
// into, and RLS would happily accept it because db.WithTenant would set app.tenant_id
// from the same forged value (internal/encode/rows.go's DBRows says exactly this
// about what the database does NOT refuse).
//
// THE MECHANISM, and it is two nets rather than a sentence:
//
//   - This struct is constructed in exactly ONE place, plaqueEncodeGrantOf, and that
//     function reads httpx.AdminOf(r). Both facts are asserted from the AST by
//     TestPlaqueEncodeGrant_IsBuiltOnlyFromTheResolvedAdminSession.
//   - The three handlers read exactly two form keys, `session` and `rapdu`, asserted
//     from the AST by TestPlaqueEncode_ReadsOnlyTheTwoFormKeysTheRelaySends. There is
//     no tenant key to send.
//
// ⚠️ WHAT IT DOES NOT DECIDE, named rather than implied: it does not ask for a ROLE.
// Both panel roles (owner and manager) may encode. See the endpoint's own comment for
// the two readings that were weighed.
type plaqueEncodeGrant struct {
	tenantID uuid.UUID
	// adminID is the RESOLVED admin, and it becomes audit_log.actor_id. It is
	// separate from actor for the reason internal/encode/rows.go's actorIDOf gives:
	// actor is a label, this is an authority.
	adminID uuid.UUID
	actor   string
}

// plaqueEncodeGrantOf derives the grant from the request's resolved panel identity.
//
// It returns false when there is no live admin, which requireAdmin has already made
// impossible on these routes — the check is here because a middleware that is not
// mounted fails OPEN, and this is the one place where failing open would mean writing
// a plaque row into uuid.Nil.
//
// THE ACTOR IS THE ADMIN'S OWN ID, PREFIXED. It has three jobs and the id serves all
// three: it keys encode.DefaultMaxPerActor (so one operator's stuck rounds cannot fill
// the store), it lands in audit_log.detail.claimed_by (rows.go), and it is what a
// forensic reader joins back to admin_users. An email would put personal data in an
// append-only jsonb column nobody can delete from; a device name would be a claim.
// 42 bytes, against encode.MaxActorLen's 200.
func plaqueEncodeGrantOf(r *http.Request) (plaqueEncodeGrant, bool) {
	id := httpx.AdminOf(r)
	if !id.Live() {
		return plaqueEncodeGrant{}, false
	}
	return plaqueEncodeGrant{
		tenantID: id.Admin.TenantID,
		adminID:  id.Admin.AdminUserID,
		actor:    "admin:" + id.Admin.AdminUserID.String(),
	}, true
}

// --- the budget ------------------------------------------------------------------

// encodePlaquesPerWindow is the number of COMPLETE plaques one panel session may
// encode in a ten-minute window.
//
// 🔴 IT IS NOT CHOSEN AGAINST THE JOB, AND THE FIRST VERSION OF THIS BLOCK WAS — AND
// SHIPPED A GATE THAT COULD NEVER FIRE. It said fifty ("one plaque every twelve
// seconds, already faster than a person handling physical objects"), which put the
// budget at 550 requests. MEASURED against the real router:
//
//	550 encode requests on one panel session
//	  -> the first 429 arrives at request 301, from sessionGate, as an HTML page
//
// adminSessionLimit is 300 and it is charged by EVERY panel request INCLUDING these,
// so a budget above 300 is a number with no gate behind it — precisely the shape this
// repository deletes on sight. The reasoning about how fast a person can handle a
// physical plaque was sound and IRRELEVANT: it derived a ceiling for a bucket that
// was never going to be the binding one.
//
// 🔴 SO IT IS DERIVED FROM THE CEILING THAT ALREADY EXISTS, AND WHAT IT BUYS IS
// HEADROOM RATHER THAN A LOWER LIMIT:
//
//	adminSessionLimit                       300 requests per session per window
//	minus ~80 reserved for the panel         -80
//	                                        ----
//	the encode surface's own share          220 requests
//	  / encode.RequestsPerRound() (11)        =  20 plaques
//
// (The panel really keeps 79 of those 80 — encodePanelHeadroom carries the missing
// one and why it is missing.)
//
// ⚠️ WITHOUT THIS GATE THE NUMBER WOULD BE 27 PLAQUES AND THE PANEL WOULD THEN BE
// DEAD FOR THE REST OF THE WINDOW — the operator could not even open the plaque list
// to see what they had just encoded. Twenty plaques and a working panel is the better
// trade, and it is the whole reason the bucket is separate rather than wider.
//
// ⚠️ THE HONEST COST OF BEING WRONG: an operator with a bigger batch waits out the
// window. Nothing is lost — a round refused mid-flight leaves an inventory row and a
// chip that was never touched irreversibly (see the gate below).
const encodePlaquesPerWindow = 20

// encodePanelHeadroom is how many ORDINARY panel requests an operator still has after
// spending the whole encode budget in one window.
//
// 🔴 IT IS A CONSTANT AND IT IS ASSERTED, WHICH IT WAS NOT — AND AN AUDIT NAMED THAT
// AS THE DEFECT (2026-08-24, second round). The 300 − 80 arithmetic lived only in the
// comment above; the sole structural test was `adminEncodeLimit < adminSessionLimit`,
// which a headroom of ONE would satisfy. That is the shape this repository deletes on
// sight: a number that decides something and is bound to nothing.
//
// 🔴 IT IS BOUND TWO WAYS NOW, and the second is a measurement rather than an
// identity. TestEncodeBudget_IsDerivedFromTheRoundAndIsPinned requires
// adminSessionLimit − adminEncodeLimit to equal it; and
// TestPlaqueEncode_ASpentEncodeBudgetLeavesTheOperatorAPANEL burns the encode budget
// on the REAL router and then COUNTS how many panel requests are still served, which
// must be exactly this number. Move either limit and the count moves with it.
//
// WHERE ~80 COMES FROM — adminratelimit.go's own measured denominators, which is why
// it is not re-derived here: a section view is 1 charged request, a filter change 1, a
// decision 2, a deactivation 3, and a roster walk ceil(E / RosterPageSize). So it is
// ~79 section views, or ~26 deactivations, after a full batch. An operator encoding
// plaques is looking at the plaque list between rounds; that is the workload this
// reserves for.
//
// 🔴 THE −1 IS NOT ARITHMETIC TIDINESS, IT IS THE GATE ORDER, AND IT WAS FOUND BY
// MEASUREMENT RATHER THAN BY READING. The first version of this constant was a bare 80
// and the behavioural test measured 79. Reason: sessionGate runs BEFORE encodeGate (it
// has to — a per-session budget cannot be keyed before the session is resolved), so the
// request that DISCOVERS the encode budget is exhausted has already been charged to the
// panel's bucket. An operator who stops at exactly 220 keeps 80; one who tries a 221st
// round keeps 79. The guarantee is the worst case, so the constant is the worst case.
//
// ⚠️ IT IS DERIVED, NOT WRITTEN: change either limit and it follows, which is what
// stops the three numbers from drifting apart the way this file's first budget did.
// (A var rather than a const only because adminEncodeLimit is one — it multiplies
// encode.RequestsPerRound(), a function call, which no constant expression may do.)
var encodePanelHeadroom = adminSessionLimit - adminEncodeLimit - 1

// adminEncodeLimit is the encode budget, in REQUESTS, keyed on the panel session.
//
// 🔴 IT IS DERIVED FROM encode.RequestsPerRound() RATHER THAN WRITTEN OUT, which is
// this repository's standing rule about numbers ("bir sayı ya bir kapıya bağlanır, ya
// tarihlenir, ya silinir").
//
// ⚠️ TWO DIFFERENT ELEVENS MET IN THE SENTENCE THAT USED TO BE HERE, AND AN AUDITOR
// READ IT AS ARITHMETIC THAT DOES NOT CLOSE. It said "Today that is 11 … the table
// grows to eleven exchanges, this becomes 240" — the first eleven is
// RequestsPerRound(), the second is len(roundSteps) AFTER step 8, and they are one
// apart. The two quantities are named apart now:
//
//	len(roundSteps)        10 today, 11 when ADR 0017 §5.1 step 8 ships
//	RequestsPerRound()     11 today, 12 then   (one Begin + one Step per exchange)
//	adminEncodeLimit      220 today, 240 then  (encodePlaquesPerWindow x the above)
//
// So the budget follows the table on its own, and the headroom arithmetic above wants
// re-reading on that day rather than the number wanting editing.
//
// 🔴 WHAT IT DOES TO ADR 0017 §6 md. 12 IS COUNTED BY encodeRowsPerWindow BELOW
// RATHER THAN BY THIS COMMENT, and the reason is that this comment got it wrong. It
// carried a table reading "/ 4 = 55 per session, 750 per address" — a denominator
// nothing measured. encode.RequestsBeforeTheRowIsWritten() measures it against a real
// round and answers FIVE: the row is written by the ACCEPT of the third GetVersion
// frame, which runs when that frame's RESPONSE is fed back in, one Step later than the
// arithmetic assumed.
//
// ⚠️ IT IS A var BECAUSE encode.RequestsPerRound() IS A FUNCTION CALL, which a constant
// expression may not contain, so it is NOT covered by adminratelimit.go's
// TestPanelConstants_ShippedValuesArePinned. It has its own pin,
// TestEncodeBudget_IsDerivedFromTheRoundAndIsPinned, which asserts BOTH the value and
// that it stays below adminSessionLimit — the property the first version lost.
var adminEncodeLimit = encodePlaquesPerWindow * encode.RequestsPerRound()

// encodeRowsPerWindow and encodeRowsPerAddress are ADR 0017 §6 md. 12's real bounds:
// how many uids one panel session, and one source address, can SQUAT in a window.
//
// 🔴 THE DENOMINATOR IS NOT THE LENGTH OF A ROUND. md. 12's hazard is the INVENTORY
// ROW, not the finished plaque: tags.uid is a global PRIMARY KEY, so a row squats that
// uid for every business at once, tappa_app cannot delete it, and its aes_key_ref can
// never be rewritten — cleanup is manual, as tappa_owner (deploy/README.md carries the
// procedure where an operator will find it). The row lands SIX exchanges before the
// round ends, so dividing by RequestsPerRound instead would say the budget permits 20
// squatted rows when it permits 44 — i.e. it would make the bound look 2.2x TIGHTER
// than it is (220/11 against 220/5).
//
// ⚠️ THAT SENTENCE READ "overstate the bound by nearly three" AND WAS WRONG TWICE OVER
// (audit, seventh round): the factor is 11/5 = 2.2, and "overstate the bound" pointed
// the wrong way — the wrong denominator understates the ROWS, which flatters the gate.
// A factor written without its two operands is a factor nobody can check.
//
// 🔴 DERIVED, BECAUSE THE HAND-WRITTEN VERSION WAS WRONG IN THREE PLACES AT ONCE. An
// audit named these figures as bound to nothing; deriving them then showed the
// denominator itself was off by one. Both numbers now move with either limit, and
// TestEncodeBudget_IsDerivedFromTheRoundAndIsPinned prints and pins them.
//
// ⚠️ WHAT THEY ARE NOT: a fix. Rate limiting is one of the three mitigations md. 12
// names — the others are the authorisation gate above and the written cleanup path —
// and the item is COUNTED rather than closed. Every squatted row does carry an
// audit_log entry naming the admin who claimed it (rows.go's loadedDetail.ClaimedBy),
// which is what makes it attributable rather than merely bounded.
var (
	encodeRowsPerWindow  = adminEncodeLimit / encode.RequestsBeforeTheRowIsWritten()
	encodeRowsPerAddress = adminFloodLimit / encode.RequestsBeforeTheRowIsWritten()
)

// adminEncodePeriod matches every other panel window, so an operator sees one
// recovery time rather than two.
const adminEncodePeriod = 10 * time.Minute

// encodeGate is the encode surface's own ceiling, keyed on the panel SESSION id.
//
// IT RUNS AFTER requireAdmin, which is why it is a nested group inside mountWriting
// rather than another stage of ProtectWriting: a per-session budget cannot be keyed
// before the session is resolved (adminSessionLimit's comment makes the same point).
//
// 🔴 WHAT A MID-ROUND REFUSAL DOES TO THE ROUND, DECIDED RATHER THAN LEFT TO
// DISCOVERY. It does NOT abort the session, and the two readings were weighed:
//
//	ABORT IT   the gate would have to parse the body to learn the handle — putting
//	           work on the one path whose entire purpose is to shed work — and it
//	           would hand a caller a way to end a round by exhausting a budget.
//	LET IT DIE the round is orphaned and expires on its own. encode.DefaultTTL is
//	           90 s and the sweeper is two-way (lazy on checkout AND a goroutine on a
//	           15 s tick), so the plain plaque key is wiped whether or not anybody
//	           comes back — which is precisely the guarantee ADR 0017 §6 md. 7 was
//	           written to make independent of callers.
//
// LET IT DIE is chosen, and it is measured rather than asserted:
// TestPlaqueEncode_TheBudgetRefusesAndTheRoundDiesOnItsOwn drives a real
// round to a 429, advances the injected clock past the TTL, fires the sweeper and
// requires Live() to be 0.
//
// WHAT IT COSTS, stated: the chip keeps its own authentication state until the field
// goes away, and the inventory row written at the fourth request stays behind as an
// unassigned plaque with no location — ADR 0017 §5.2's recoverable half ("satır var,
// çip yok"), never the permanent one.
func (a *AdminAuth) encodeGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := httpx.AdminOf(r)
		if !id.Live() {
			// Unreachable behind requireAdmin. It answers the SAME way the three
			// handlers answer the same condition — 500, faultServer — because it IS the
			// same condition (a chain that did not run), and two different answers to
			// one fault is how an operator learns to distrust both.
			writeEncodeFault(w, http.StatusInternalServerError, faultServer)
			return
		}
		key := id.Admin.SessionID.String()
		if n := a.encodeLimiter.Charge(key); n > adminEncodeLimit {
			if a.encodeLimiter.FirstOverLimit(n) {
				a.log.Warn("plaque encode budget reached", "session_id", key,
					"limit", adminEncodeLimit, "period", adminEncodePeriod.String(),
					"plaques_per_window", encodePlaquesPerWindow)
			}
			writeEncodeFault(w, http.StatusTooManyRequests, faultTooMany)
			return
		}
		// 🔴 THE BODY IS BOUNDED HERE AND NOT IN THE HANDLERS, WHICH IS A STRUCTURAL
		// CHOICE RATHER THAN A TIDY-UP. http.MaxBytesReader needs the ResponseWriter,
		// and this file's rule is that a HANDLER may do exactly one thing with a
		// writer: hand it to a reply function. ⚠️ The rule binds handlers; the three
		// EXEMPT writers may do anything with it, which is counted at encodeReply.
		// Bounding in the gate keeps the handler half clean (see
		// TestPlaqueEncode_NoHandlerTouchesTheResponseWriterExceptThroughOneWriter),
		// and it bounds EVERY route in the group at once, including any added later.
		r.Body = http.MaxBytesReader(w, r.Body, encodeMaxBody)
		next.ServeHTTP(w, r)
	})
}

// --- the three handlers ----------------------------------------------------------

// plaqueEncodeBegin serves POST /admin/plaques/encode: open a round and hand back its
// handle and the first C-APDU (ADR 0017 §5.1 step 1, the ISO SELECT).
//
// ⚠️ BOTH PANEL ROLES MAY ENCODE, AND THE TWO READINGS WERE WEIGHED RATHER THAN
// DEFAULTED. Owner-only would match accountSave, whose justification is that it
// rewrites the business's own row; encoding does not — it appends an UNASSIGNED
// plaque with no location, which is invisible to every screen until somebody MOUNTS
// it, and mounting is already open to both roles (plaqueactions.go). The act that
// puts a plaque on a wall is the one that matters to a tap, and it is the one that is
// already gated the way the product wants. The counter-reading, stated: encoding
// squats a globally unique uid (ADR 0017 §6 md. 12) and nobody can clean it up
// without tappa_owner, which is an argument for the narrower role. It is a product
// decision, it is reversible in one line here, and the audit trail names the admin
// either way.
func (a *AdminAuth) plaqueEncodeBegin(w http.ResponseWriter, r *http.Request) {
	if a.encoder == nil {
		writeEncodeFault(w, http.StatusServiceUnavailable, faultUnavailable)
		return
	}
	g, ok := plaqueEncodeGrantOf(r)
	if !ok {
		a.log.Error("plaque encode: no resolved admin on a protected route",
			"hint", "mount AdminAuth.ProtectWriting in front of "+plaqueEncodeHref)
		writeEncodeFault(w, http.StatusInternalServerError, faultServer)
		return
	}
	id, p, err := a.encoder.Begin(r.Context(), g.tenantID, g.adminID, g.actor)
	if err != nil {
		a.log.Warn("plaque encode: a round could not be opened", "err", err, "actor", g.actor)
		writeEncodeStoreFault(w, err)
		return
	}
	writeEncodeReply(w, http.StatusOK, string(id), p.Command, p.Step, p.Done)
}

// plaqueEncodeStep serves POST /admin/plaques/encode/step: one relayed exchange.
//
// 🔴 THE GRANT IS RE-DERIVED HERE EVEN THOUGH Step DOES NOT TAKE A TENANT. The
// tenant a round writes into was fixed at Begin and lives inside the session, so this
// handler could not change it if it wanted to — which is the design. What the check
// buys is that a handle minted by one operator cannot be driven by a request that has
// no identity at all, and it costs one map lookup.
//
// ⚠️ WHAT IT DOES NOT BUY, COUNTED: it does NOT bind the handle to the admin who
// opened it. Any signed-in admin of any tenant who obtains a live handle can drive
// that round — and the round will keep writing into the ORIGINATOR's tenant, because
// the tenant is the session's. encode.ID is a 128-bit random bearer handle that never
// leaves the response body it was minted in, so obtaining one means already holding
// the originator's traffic. Binding it would mean storing an admin id per session in
// internal/encode, which is a change to a package this task deliberately did not
// widen. Named here rather than claimed shut.
func (a *AdminAuth) plaqueEncodeStep(w http.ResponseWriter, r *http.Request) {
	if a.encoder == nil {
		writeEncodeFault(w, http.StatusServiceUnavailable, faultUnavailable)
		return
	}
	if _, ok := plaqueEncodeGrantOf(r); !ok {
		a.log.Error("plaque encode: no resolved admin on a protected route",
			"hint", "mount AdminAuth.ProtectWriting in front of "+plaqueEncodeStepHref)
		writeEncodeFault(w, http.StatusInternalServerError, faultServer)
		return
	}
	id, rapdu, ok := readEncodeStep(r)
	if !ok {
		writeEncodeFault(w, http.StatusBadRequest, faultBadRequest)
		return
	}
	p, err := a.encoder.Step(r.Context(), id, rapdu)
	if err != nil {
		// 🔴 Done IS READ BEFORE THE ERROR, WHICH IS encode.Progress.Done's OWN RULE
		// AND THE MOST CONSEQUENTIAL LINE IN THIS FILE. Step returns Done=true WITH an
		// error when the chip completed but the row could not be marked. A relay told
		// "not done" re-runs; the re-run dies at the row insert on a duplicate primary
		// key; that reads like stale inventory; the obvious fix is deleting the row;
		// the row holds the only stored copy of the plaque key. So a completed round
		// reports itself completed, and the failure goes to the log.
		//
		// ⚠️ THIS ARM IS NOW RARE, AND THAT IS A CHANGE IN WHAT IT MEANS RATHER THAN
		// IN WHAT IT DOES (ninth audit). It used to fire on the ordinary hang-up,
		// because Step marked the row on the REQUEST's context; encode now detaches,
		// so reaching this line means the marking itself failed after the chip was
		// already personalised. The log line below is the in-process half of that
		// signal; the durable half is the plaque.unmarked row, and only the durable
		// half survives log rotation.
		//
		// ⚠️ AND THE TWO HALVES DO NOT FAIL TOGETHER, WHICH IS THE POINT OF KEEPING
		// BOTH: the durable half travels the same pool it reports on, so it is absent
		// for exactly the faults that broke that pool — see the property at
		// internal/domain/tenant.ActionPlaqueUnmarked. This log line survives those,
		// and is lost to rotation instead. Neither is a guarantee on its own.
		if p.Done {
			a.log.Error("plaque encode: the chip completed but the row could not be marked; do NOT re-run this round",
				"err", err, "step", p.Step)
			writeEncodeReply(w, http.StatusOK, string(id), p.Command, p.Step, p.Done)
			return
		}
		a.log.Warn("plaque encode: a step failed", "err", err, "step", p.Step)
		writeEncodeStoreFault(w, err)
		return
	}
	writeEncodeReply(w, http.StatusOK, string(id), p.Command, p.Step, p.Done)
}

// plaqueEncodeAbort serves POST /admin/plaques/encode/abort: the operator gave up,
// the phone left the field, the screen navigated away.
//
// It answers 200 whatever happened, because encode.Store.Abort is deliberately quiet
// about whether the handle was live — telling a caller which would turn a guess into
// an oracle over a bearer handle.
func (a *AdminAuth) plaqueEncodeAbort(w http.ResponseWriter, r *http.Request) {
	if a.encoder == nil {
		writeEncodeFault(w, http.StatusServiceUnavailable, faultUnavailable)
		return
	}
	if _, ok := plaqueEncodeGrantOf(r); !ok {
		a.log.Error("plaque encode: no resolved admin on a protected route",
			"hint", "mount AdminAuth.ProtectWriting in front of "+plaqueEncodeAbortHref)
		writeEncodeFault(w, http.StatusInternalServerError, faultServer)
		return
	}
	id, ok := readEncodeSession(r)
	if !ok {
		writeEncodeFault(w, http.StatusBadRequest, faultBadRequest)
		return
	}
	a.encoder.Abort(id)
	writeEncodeReply(w, http.StatusOK, "", nil, "abort", false)
}

// --- reading the request ----------------------------------------------------------

// readEncodeSession parses the one field an abort carries.
//
// ⚠️ IT TAKES NO ResponseWriter, DELIBERATELY. The body limit is already registered
// by encodeGate, so nothing on the read side needs a writer — which is what makes
// "a handler's only use of w is to hand it to a reply function" a statement with no
// exceptions AMONG HANDLERS. ⚠️ The three reply writers are exempt by name, and that
// exemption is the gap F1 counts at encodeReply.
func readEncodeSession(r *http.Request) (encode.ID, bool) {
	if err := r.ParseForm(); err != nil {
		return "", false
	}
	raw := r.PostFormValue("session")
	if raw == "" || len(raw) > encodeSessionLen {
		return "", false
	}
	return encode.ID(raw), true
}

// encodeSessionLen is how long a handle encode.newID mints is: 16 random bytes as
// hex. It is a LENGTH CEILING on the input rather than an equality test, because the
// handle's format belongs to internal/encode and a second spelling of it here would
// be the two-representations defect this repository keeps paying for. An unknown
// handle is refused by the store, which is the authority.
const encodeSessionLen = 32

// readEncodeStep parses a step: the handle and one hex R-APDU.
func readEncodeStep(r *http.Request) (encode.ID, []byte, bool) {
	id, ok := readEncodeSession(r)
	if !ok {
		return "", nil, false
	}
	// ParseForm has already run inside readEncodeSession; PostFormValue reuses it.
	raw := r.PostFormValue("rapdu")
	if raw == "" || len(raw) > 2*encodeMaxRAPDUBytes {
		return "", nil, false
	}
	rapdu, err := hex.DecodeString(raw)
	if err != nil {
		return "", nil, false
	}
	return id, rapdu, true
}

// --- writing the response ----------------------------------------------------------

// writeEncodeReply is the ONE place a successful body is produced.
//
// 🔴 IT TAKES FOUR SCALARS AND NO VALUE, AND THAT SIGNATURE IS THE THIRD DESIGN OF
// THIS GATE. The first took `body any`; the second took a sealed interface; a
// different auditor beat each. See encodeReply's own comment for the history and for
// why "which TYPE may be serialised" turned out to be a question with no bottom.
//
// The struct that reaches encoding/json is built HERE, from these parameters, and a
// caller has nothing to hand over that could become one. That is the whole of the
// shape argument; what may go INSIDE the four scalars is a separate question and is
// answered separately (see encodeReply).
//
// The C-APDU is hex rather than base64 because every other byte string on this path
// is hex — the uid in tags, the mirrors in the NDEF template, internal/sun's test
// vectors — and a second encoding on one wire is a second thing to get wrong. The
// encoding happens here rather than at the call site so that `command` cannot be some
// other string that merely looks like one. The four argument expressions are pinned by
// TestPlaqueEncode_EveryReplyCallSitePassesTheDriversOwnValues.
func writeEncodeReply(w http.ResponseWriter, status int, session string, command []byte, step string, done bool) {
	encodeResponseHeaders(w, status)
	// 🔴 THE LITERAL IS INSIDE THE FUNCTION. Nothing outside can construct, embed,
	// wrap or substitute it. The error is not reported to the caller: the status line
	// has already gone out, so there is nothing left to say. It is not swallowed
	// either (CLAUDE.md §7) — encoding three strings and a bool fails only if the
	// connection did.
	_ = json.NewEncoder(w).Encode(encodeReply{
		Session: session,
		Command: hex.EncodeToString(command),
		Step:    step,
		Done:    done,
	})
}

// writeEncodeFault is the ONE place a failure body is produced. Its single parameter
// is a string, its literal is built here, and TestPlaqueEncode_WritesOnlyDeclaredFaults
// requires every call site in the PACKAGE to name one of the fault constants.
func writeEncodeFault(w http.ResponseWriter, status int, fault string) {
	encodeResponseHeaders(w, status)
	_ = json.NewEncoder(w).Encode(encodeFault{Fault: fault})
}

// encodeResponseHeaders is the shared header block, and it is SHARED RATHER THAN
// SERIALISING so that the two writers above can each own their own literal.
//
// ⚠️ THERE USED TO BE A THIRD FUNCTION HERE THAT TOOK THE BODY, AND IT WAS THE HOLE.
// `writeEncodeJSON(w, status, body …)` existed so the two writers would not duplicate
// four lines; that parameter was `any` in the first design and a sealed interface in
// the second, and an auditor beat both. Four duplicated lines were never worth a
// parameter through which a value can travel. What is factored out now cannot carry
// anything: a status code and three constant header names.
//
// Cache-Control: no-store for the reason every panel response carries it — the body
// holds a bearer handle for a live round, and no intermediary should keep a copy.
// nosniff because a JSON body that a browser is willing to sniff is a JSON body that
// can be rendered.
func encodeResponseHeaders(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
}

// writeEncodeStoreFault turns a store error into a status and a WORD.
//
// 🔴 IT WRITES RATHER THAN RETURNS, AND THE FIRST VERSION RETURNED — WHICH BROKE THE
// PROPERTY THIS FILE IS BUILT ON. It was `encodeFaultStatus(err) (int, string)` and
// its two callers did `status, fault := …; writeEncodeFault(w, status, fault)`. That
// puts a VARIABLE in the body's one string field, so
// TestPlaqueEncode_WritesOnlyDeclaredFaults could no longer say "every call site names
// a declared constant" — it had to special-case an identifier, and a net with a
// special case is a net with a hole. Measured: the test failed on exactly those two
// sites. Mapping HERE, and calling writeEncodeFault with a literal constant in every
// arm, restores the closed statement.
//
// 🔴 IT IS A CLOSED MAPPING OVER internal/encode's EXPORTED SENTINELS, and the
// default arm is the one that matters: anything unrecognised is `refused`, never the
// error's text. That is what keeps ADR 0017 §5.1's per-step failures — which name
// steps, status words and, in RelayMismatchError's case, two uids and an instruction
// — inside the log where redline-check R7 scans, and out of a body.
func writeEncodeStoreFault(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, encode.ErrUnknownSession):
		writeEncodeFault(w, http.StatusNotFound, faultUnknownSession)
	case errors.Is(err, encode.ErrBusy),
		errors.Is(err, encode.ErrPlaqueBusy),
		errors.Is(err, encode.ErrTooManySessions):
		writeEncodeFault(w, http.StatusConflict, faultBusy)
	case errors.Is(err, encode.ErrStoreClosed):
		writeEncodeFault(w, http.StatusServiceUnavailable, faultUnavailable)
	default:
		writeEncodeFault(w, http.StatusUnprocessableEntity, faultRefused)
	}
}
