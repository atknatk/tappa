package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
)

// The PANEL CONFIRMATION — a value only this server can mint, bound to one ACTION, one
// SUBJECT and one panel session (user decision, 2026-08-08: "enforce it on the
// server").
//
// ⚠️ IT IS NO LONGER ONLY ABOUT DEACTIVATION, AND THE FILE NAME IS NOW THE OLDEST
// THING IN IT. M6-06 phase A gates two removals with the same primitive rather than
// inventing a second one, so the SUBJECT may be an employee, a location or a
// department, and version 2 of the payload names the ACT as well. The file keeps its
// name because renaming it would move every line of this argument out of `git log
// --follow` for the sake of tidiness.
//
// 🔴 WHY THIS FILE EXISTS: THE FIRST ATTEMPT WAS A PURE DOUBLE-SUBMIT COOKIE AND AN
// AUDIT FORGED IT IN TWO LINES.
//
//	cookie: tappa_admin_confirm=attacker-chosen-value.<employee id>
//	POST   /admin/employees/deactivate  id=<employee id>&confirm_token=attacker-chosen-value
//	-> 303 ?done=deactivated,  employees.status = "deactivated"
//
// The comment beside it said it was "logincontext.go's SHAPE, DELIBERATELY" — and
// that was the defect, because a shape is not a mechanism. What makes that file's
// choice blob unforgeable is an HMAC under a key derived from the server's own
// secret; the confirmation had a cookie, a form field and a constant-time compare
// and NO KEY AT ALL, so the two shapes diverged at exactly the point the guarantee
// comes from. The user decision was "enforce", and what shipped did not.
//
// 🔴 COPYING A PATTERN MEANS COUNTING ITS PARTS — this repository has now half-copied
// one THREE times (M6-01 B's rate-limit chain, M6-04's Origin gate, and this). So
// adminChoices' parts are counted here, and each is marked present or deliberately
// absent:
//
//	 1. key DERIVED at construction from SessionHMACKey under its own label   ✅
//	 2. the zero value is an ERROR in both mint and parse, never a default    ✅
//	 3. a required BINDING the value is useless without                       ✅ (two)
//	 4. a versioned payload with separators no field can contain              ✅
//	 5. LENGTH-PREFIXED MAC input, so the encoding is injective               ✅
//	 6. base64url(payload) "." base64url(signature)                           ✅
//	 7. parse order: shape -> SIGNATURE -> expiry -> fields                   ✅
//	 8. constant-time comparison, never == or bytes.Equal (redline R7)        ✅
//	 9. a version check whose failure is "malformed", never repaired          ✅
//	10. TTL against the SERVER clock, injectable for tests                    ✅
//	11. a FUTURE-SKEW bound, so a backwards clock step cannot widen the TTL   ✅
//	12. an ACTION binding, so a value minted for one gate cannot open another ✅
//
// ⚠️ PART 11 WAS MISSING FROM BOTH THE LIST AND THE CODE, WHICH IS THIS FILE'S OWN
// HEADLINE MISTAKE IN MINIATURE. The list above exists to say "the parts were
// counted"; an audit counted them again and found eleven, not ten — parse tested only
// `now > issued + ttl`, so a confirmation stamped a YEAR ahead verified cleanly. No
// attacker can reach it (issuedAt comes from this process's clock and is inside the
// MAC), which is exactly why it is worth saying out loud: the defect was in the
// COUNTING, and a file that argues for counting has to survive being counted.
//
// ⚠️ WHAT THIS BUYS, STATED HONESTLY AND SMALLER THAN IT SOUNDS. The actor this
// gate faces is the panel's own signed-in operator, and that actor can always GET
// the confirmation page and then POST — so the SECURITY difference is close to zero,
// and the audit that forged the old value said so too. What it buys is the property
// the user asked for and ADR 0010 depends on: reaching the irreversible write implies
// the server RENDERED the warning, in response to a request for it, for THIS person,
// in THIS session, within the window. A script written against the route now has to
// walk the screen a human walks.
//
// 🔴 AND WHAT IT DOES *NOT* BUY IS SINGLE USE — measured, after this file claimed it.
// The value carries no server-side state, so one minted confirmation can be spent as
// many times as its holder likes until it expires: an audit re-printed the cookie
// before each POST and got three deactivations from one mint. Nothing here notices,
// because there is nothing here to notice with.
//
// ⚠️ THIS LIMIT IS STILL OPEN AND IS COUNTED RATHER THAN CLOSED. Closing it needs
// server-side state — a spent-value table or a nonce — with its own retention and its
// own failure modes, which is a different task. A counted hole is safer than one
// claimed shut, and what makes this one survivable is stated where each gate consumes
// it: DeactivateEmployee carries `status <> 'deactivated'`, and a removal is
// idempotent because the second DELETE matches no row.
//
// 🔴 PART 12 WAS ADDED AFTER AN AUDIT SPENT ONE GATE'S VALUE AT ANOTHER'S. Until
// version 2 the payload named a SUBJECT and a SESSION but not the ACT, so a
// confirmation minted to remove a venue verified cleanly at
// /admin/employees/deactivate — the request then failed in the DOMAIN, because a
// location id is not an employee id. That is a gate held by two accidents (the uuid
// spaces not overlapping, and the domain checking), neither of which the token knew
// about. The list above exists precisely so this kind of missing part is COUNTED, and
// it took an outside eye to count to twelve — the same way it took one to count to
// eleven. That is the argument for the list, not against it.
//
// IT IS COUNTED RATHER THAN CLOSED, and the reason is a measurement rather than
// effort: single use needs a row somewhere (infrastructure), and the gain is ZERO —
// the same operator can mint a fresh confirmation with one GET, and every spend after
// the first meets DeactivateEmployee's own `status <> 'deactivated'` predicate, so it
// writes no employees row, no audit row and no second stamp.
// TestEmployeeDeactivate_TheOneShotDependsOnTheClient and its database twin measure
// both halves, and both say to delete themselves if a ledger ever lands.
//
// WHAT IT IS AND IS NOT AGAINST CSRF — AND THE FIRST VERSION UNDERSTATED IT. It said
// flatly "it is not CSRF protection", on the grounds that ProtectWriting refuses a
// cross-origin POST before the resolver runs. That is the reason it is not NEEDED for
// CSRF, and it is not the same as being useless against it: the token is rendered
// into the page and its twin cookie is HttpOnly, SameSite=Lax and Path=/admin, so a
// cross-origin document cannot read either (same-origin policy). If the Origin check
// were ever to fall — most plausibly on a request that carries NO Origin header at
// all, which is the counted limit at TestEmployeeActions_SecFetchSiteSameSitePasses
// TheOriginGate — this would still stand in its way as a second layer.
//
// SAID AT THE RIGHT STRENGTH: it is not the panel's CSRF defence and must not be
// relied on as one, but it is not nothing either. Its PURPOSE remains the other
// question — was the warning served.

// adminConfirmKeyLabel and adminConfirmMACLabel separate this value's key and MAC
// input from every other keyed thing in the process. Two labels rather than one, for
// adminChoices' reason: a key derived for one purpose must not be usable to forge
// another's signature even if a payload were ever made to collide.
const (
	adminConfirmKeyLabel = "tappa/admin-deactivate-confirm/key/v1"
	adminConfirmMACLabel = "tappa/admin-deactivate-confirm/mac/v1"
	// VERSION 2 ADDED THE ACTION BINDING; VERSION 3 WIDENS THE SUBJECT FROM A uuid TO
	// AN OPAQUE STRING. Both older payloads are refused as malformed rather than
	// repaired, which costs at most one re-press inside a ten-minute window at deploy
	// time.
	//
	// 🔴 WHY THE SUBJECT HAD TO WIDEN (M6-06 phase B). A wall plaque's identity is its
	// 14-hex chip uid, not a uuid — so gating "replace this plaque" needed either a
	// SECOND confirmation primitive or this one field loosened. This repository has
	// half-copied a security shape FOUR times; loosening one field of the shipped
	// mechanism is the cheaper mistake, and it is a widening of the DOMAIN, not of the
	// guarantee: the subject is still authenticated, still inside the MAC, still
	// length-prefixed, and still compared in constant time.
	//
	// ⚠️ THE ONE PROPERTY THE uuid TYPE USED TO GIVE FOR FREE was that a subject could
	// never contain the separator. That is now a CHECK rather than a type
	// (mint refuses a subject containing "|"), because the payload's fields are split
	// back by that character.
	adminConfirmVersion = "3"
)

// The actions a confirmation can authorise. They are CONSTANTS rather than free text
// so a typo at a call site is a compile error rather than a value that verifies
// against nothing.
const (
	confirmActionDeactivate       = "employee.deactivate"
	confirmActionDeleteVenue      = "location.delete"
	confirmActionDeleteDepartment = "department.delete"
	// 🔴 REPLACING A WALL PLAQUE (M6-06 phase B). It is gated for a reason phase A's
	// two are NOT gated for: nothing is destroyed — 00004 revokes DELETE on `tags`
	// and every column the act writes is schema-reversible (measured) — but the
	// PRODUCT ships no route that reverses it, so a mis-pressed replacement leaves an
	// entrance where every tap is refused (§5 row 1) until somebody physically swaps
	// the plaque. "The row survives" and "the manager can undo it" are different
	// sentences, and this gate answers the second.
	//
	// ⚠️ MOUNTING IS NOT GATED, AND THAT SENTENCE ONLY BECAME TRUE WHEN THE UNDO PATH
	// SHIPPED. It used to say "nothing stops taking it off again in the same statement
	// the schema already permits" with a "measured" label on it, and an audit drove
	// the opposite: the schema permitted it, the PRODUCT had no statement for it, so a
	// plaque mounted at the wrong door could not be moved, could not go back to stock,
	// and with no spare in the box its card offered nothing at all. UnmountTagFromWall
	// is that statement. A mount is now undone in one press, with no spare spent —
	// which is why it needs no warning and the un-mount does.
	// TestPlaqueWrites_MountNeedsNoConfirmationAndReplaceDoes pins every half, and it
	// asserts on the CONFIRMATION COOKIE rather than on the rendered form: an audit
	// made the card mint a token without rendering one and the whole suite stayed
	// green.
	confirmActionReplacePlaque = "plaque.replace"
	// 🔴 TAKING A PLAQUE OFF A WALL. It is gated for the SAME reason a replacement is,
	// and NOT for the reason a mount is not: a plaque off the wall is `unassigned`, so
	// §5 row 1 refuses every tap at that door until something is mounted there. The
	// act is fully reversible — mount it again, no spare spent — but the DOOR is out
	// of service while it lasts, and that is a thing a manager should be asked about
	// once.
	confirmActionUnmountPlaque = "plaque.unmount"
	// 🔴 TYPING AN ATTENDANCE RECORD BY HAND (M6-08). It is gated for a reason none of
	// the five above share, and the reason is MONEY rather than availability: the row
	// is permanent (`transactions` takes no UPDATE and no DELETE, by privilege and by
	// trigger) and §4.3's own remedy — "a correction is a new row" — was MEASURED
	// against the report engine and only works in half the directions. See
	// manual.CorrectionsOnlyShorten: an appended record can shorten a shift and never
	// lengthen it, so a checkout typed too early under-pays somebody with no one-row
	// way back. docs/adr/0009 lists exactly three options for that shape and calls the
	// third "say it on the screen before the button"; this is that option, taken.
	//
	// ⚠️ ITS SUBJECT IS THE ONLY COMPOUND ONE. Every other gate binds a row's identity
	// — an employee, a venue, a plaque uid. A record has no identity until it exists,
	// so the binding is the STATEMENT: person, direction and instant together
	// (manualConfirmSubject). A confirmation bound to the person alone would let the
	// warning be served for one record and spent on a completely different one.
	confirmActionRecordManual = "record.manual"
	// 🔴 CHANGING THE RULEBOOK (M6-09 phase B). It is gated for a reason that is
	// neither "irreversible" nor "destructive", and saying which is the point.
	// Switching a rule off can be switched back on and a binding can be re-bound —
	// but the DECISIONS made in between cannot be taken back: every tap judged while
	// a rule was off is an immutable row pinned to the version that judged it (§4.3),
	// and the report that pays somebody is built from those rows. So the warning is
	// about the WINDOW rather than about the row.
	//
	// ⚠️ IT IS ALSO THE ANSWER THE M6-09 CARD ASKED FOR AND COULD NOT GET. That card's
	// original trap said the SIMULATOR had to run before a save; M6-10 is `skipped`
	// (Q22 moved it to M9-06), so "show what would change" is unavailable in v1. This
	// gate is the option the card lists as (a) — show the TEXT of the change before
	// writing it — and the screen says plainly which question it does NOT answer.
	//
	// 🔴 ITS SUBJECT IS COMPOUND, like record.manual's and unlike the other five. A
	// policy change is a STATEMENT (an operation, a policy, a scope), not an act on a
	// row, so a confirmation bound to the policy alone would let a warning served for
	// "switch this off" be spent on "bind it everywhere". See policyChange.subject.
	//
	// 🔴 IT IS DECLARED HERE AND NOT BESIDE ITS OWN HANDLER, WHICH IS LOAD-BEARING:
	// TestConfirmation_IsBoundToTheACTIONAndNotOnlyToTheSubject derives the whole
	// action matrix by parsing THIS FILE, so a gate declared anywhere else is a gate
	// nothing checks for cross-action reuse. That test is the one that caught a
	// venue-removal confirmation opening the deactivation gate.
	confirmActionPolicyEdit = "policy.edit"
	// 🔴 FREEZING A BILLING MONTH (M6-12 phase B). It is the eighth gate and the
	// FIRST one whose act is irreversible by the SCHEMA rather than by the absence of
	// a route. The five before it are held by what the product does not ship: a plaque
	// can be re-mounted, a rule can be switched back on, a venue's row is gone but the
	// act is destructive rather than permanent. billing_periods is different in kind —
	// migration 0016 REVOKEs UPDATE and DELETE from tappa_app AND binds the table owner
	// with a trigger, so a wrong month cannot be edited, cannot be removed, and cannot
	// be superseded either: UNIQUE (tenant_id, period_month) refuses a second answer.
	// A mis-pressed close is a permanent, uncorrectable statement about what somebody
	// was charged.
	//
	// ⚠️ THE ONE-SHOT ARGUMENT THIS FILE MAKES FOR THE OTHER SEVEN HOLDS HERE TOO, AND
	// FOR A DIFFERENT REASON. A re-printed cookie can spend one mint repeatedly; what
	// makes that harmless elsewhere is a predicate on the write (DeactivateEmployee's
	// `status <> 'deactivated'`), and here it is the UNIQUE constraint: the second
	// close of the same month raises 23505 and the caller reports "already closed". So
	// a replay writes no billing row, no audit row and no second figure.
	//
	// ITS SUBJECT IS THE MONTH, and that is the whole statement rather than a part of
	// it. Unlike record.manual and policy.edit — whose subjects are compound because
	// their acts have several posted fields — this act has exactly one input. Every
	// figure is derived by the statement that writes the row; db/queries/billing.sql
	// says there is no parameter an amount could be posted into, so there is no second
	// field for a warning to be served about and spent on.
	confirmActionCloseBillingPeriod = "billing.close"
)

// adminConfirmTTL is how long a rendered warning stays spendable.
//
// TEN MINUTES IS THE MIDDLE OF TWO ARGUMENTS. Long enough to read two sentences and
// think; short enough that a manager who walked away comes back to the warning rather
// than to a live button. It is enforced against the SERVER clock inside the signed
// payload — the cookie's Max-Age is a browser hint and is deliberately not the bound.
const adminConfirmTTL = 10 * time.Minute

// adminConfirmFutureSkew tolerates a confirmation that appears to come from the
// future. Both stamps are read from THIS process's clock and the value is
// authenticated, so the only way here is the clock stepping backwards (an NTP
// correction, a suspended VM). Beyond the tolerance it is treated as invalid rather
// than believed — the fail-closed direction, and adminChoices' figure.
//
// ⚠️ IT WIDENS THE ACCEPTANCE BAND, so the number a threat model should use is
// TTL + skew = 11 minutes, not the 10 every other sentence here quotes. The same
// correction is written at adminChoiceFutureSkew and for the same reason.
const adminConfirmFutureSkew = time.Minute

// adminConfirm mints and verifies the value. The key is derived once and held
// privately, so a later mutation of the caller's config slice cannot silently change
// how confirmations sign.
type adminConfirm struct {
	key []byte
	ttl time.Duration
	// now is injectable for tests only; production leaves it nil.
	now func() time.Time
}

func newAdminConfirm(cfg *config.Config) (adminConfirm, error) {
	if cfg == nil {
		return adminConfirm{}, errors.New("handler: nil config")
	}
	if len(cfg.SessionHMACKey) != 32 {
		return adminConfirm{}, fmt.Errorf("handler: session hmac key must be 32 bytes, got %d", len(cfg.SessionHMACKey))
	}
	m := hmac.New(sha256.New, cfg.SessionHMACKey)
	_, _ = m.Write([]byte(adminConfirmKeyLabel)) // hash writes never fail (documented)
	return adminConfirm{key: m.Sum(nil), ttl: adminConfirmTTL}, nil
}

func (c adminConfirm) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// payload renders the authenticated fields.
//
// The separators are '|' and the fields are a single digit, a decimal timestamp and
// two UUIDs — none of which can contain one, so splitting back is unambiguous with no
// escaping. Same reasoning as adminChoices.payload, and the same shape.
func (c adminConfirm) payload(issuedAt time.Time, action, subject string, sessionID uuid.UUID) string {
	return strings.Join([]string{
		adminConfirmVersion,
		strconv.FormatInt(issuedAt.Unix(), 10),
		action,
		subject,
		sessionID.String(),
	}, "|")
}

// sign is adminChoices.sign's construction, length prefix included.
//
// THE LENGTH PREFIX IS NOT DECORATION. Without it the MAC input is
// `label || payload` and a future field could make two different payloads produce the
// same bytes. Writing the payload's length first makes the encoding injective for any
// shape the payload later takes, which is a guarantee the next person to add a field
// does not have to know about.
func (c adminConfirm) sign(payload string) []byte {
	m := hmac.New(sha256.New, c.key)
	_, _ = m.Write([]byte(adminConfirmMACLabel))
	_, _ = m.Write([]byte(strconv.Itoa(len(payload))))
	_, _ = m.Write([]byte("|"))
	_, _ = m.Write([]byte(payload))
	return m.Sum(nil)
}

// mint returns the value the confirmation form carries.
//
// IT REFUSES A ZERO KEY AND A ZERO BINDING. A zero adminConfirm would sign under an
// empty key — a MAC anybody can compute — and a value bound to no employee or no
// session would authorise a deactivation of anybody by anybody. Both are errors
// rather than defaults, which is adminChoices' rule: where a type has no safe zero
// value, the zero value must fail loudly.
func (c adminConfirm) mint(action, subject string, sessionID uuid.UUID) (string, error) {
	if len(c.key) == 0 {
		return "", errors.New("handler: the deactivation confirmation was not configured")
	}
	if subject == "" || sessionID == uuid.Nil {
		return "", errors.New("handler: refusing to sign a confirmation with no binding")
	}
	// AN EMPTY ACTION IS AS BAD AS AN EMPTY SUBJECT: it would sign a value every gate
	// accepts, which is the hole the binding exists to close.
	if action == "" {
		return "", errors.New("handler: refusing to sign a confirmation with no action")
	}
	// 🔴 THE SEPARATOR CHECK THE uuid TYPE USED TO MAKE UNNECESSARY. payload joins on
	// "|" and parse splits on it, so a subject (or an action) carrying one would let
	// two different bindings encode to the same five fields. Both are server-chosen
	// today — a uuid string or a 14-hex uid — but "no caller does that" is the
	// sentence this repository has watched stop being true twice.
	if strings.Contains(subject, "|") || strings.Contains(action, "|") {
		return "", errors.New("handler: refusing to sign a confirmation whose fields carry the separator")
	}
	p := c.payload(c.clock().Truncate(time.Second), action, subject, sessionID)
	return base64.RawURLEncoding.EncodeToString([]byte(p)) + "." +
		base64.RawURLEncoding.EncodeToString(c.sign(p)), nil
}

// The two refusals a caller can act on. They are separate because they are separate
// mistakes and each gets its own sentence on screen (§4.6).
var (
	// errConfirmInvalid: absent, malformed, forged, expired, minted for another
	// session, or minted for another ACTION. Collapsed on purpose — every one of them
	// means "this browser has not been shown THIS warning", and telling them apart
	// would describe the server's internals to whoever is probing it.
	errConfirmInvalid = errors.New("handler: the deactivation was not confirmed")
	// errConfirmOtherPerson: the value is genuine, unexpired and this session's, but
	// it was minted for a DIFFERENT employee — the second-tab case, which deserves its
	// own sentence because the manager did nothing wrong.
	errConfirmOtherPerson = errors.New("handler: that confirmation belongs to another employee")
)

// parse verifies the value against the ACTION, SUBJECT and SESSION it must be bound
// to. It returns only an error: the caller already knows which act and which row it is
// asking about, and a function that RETURNED the subject would invite a caller to
// trust the token's copy of it instead of its own.
//
// ORDER: shape, then SIGNATURE, then expiry, then field decoding, then the bindings.
// The signature comes before anything that interprets the payload, so no field of an
// unauthenticated value is ever acted on. This is logincontext.go's order and
// tapcontext.go's, kept deliberately identical so the three read as one pattern.
func (c adminConfirm) parse(v, action, subject string, sessionID uuid.UUID) error {
	if len(c.key) == 0 {
		return errors.New("handler: the deactivation confirmation was not configured")
	}
	if subject == "" || sessionID == uuid.Nil || action == "" {
		return errConfirmInvalid
	}
	encoded, sig, ok := strings.Cut(v, ".")
	if !ok || encoded == "" || sig == "" {
		return errConfirmInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return errConfirmInvalid
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return errConfirmInvalid
	}
	// Constant time, never == or bytes.Equal (redline R7). ConstantTimeCompare
	// returns 0 for differing lengths, so a truncated signature cannot match.
	if subtle.ConstantTimeCompare(got, c.sign(string(raw))) != 1 {
		return errConfirmInvalid
	}

	parts := strings.Split(string(raw), "|")
	if len(parts) != 5 || parts[0] != adminConfirmVersion {
		// Authenticated but unreadable: minted by this deployment under a different
		// layout. Treated as invalid, never repaired.
		return errConfirmInvalid
	}
	issued, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return errConfirmInvalid
	}
	issuedAt := time.Unix(issued, 0).UTC()
	now := c.clock()
	if now.Sub(issuedAt) > c.ttl || issuedAt.Sub(now) > adminConfirmFutureSkew {
		return errConfirmInvalid
	}
	forAction := parts[2]
	forSubject := parts[3]
	forSession, err := uuid.Parse(parts[4])
	if err != nil {
		return errConfirmInvalid
	}
	// 🔴 THE SESSION BINDING IS CHECKED BEFORE THE EMPLOYEE ONE, so a value minted in
	// somebody else's session is "not confirmed" rather than "that was for another
	// employee" — the second sentence would tell a prober which of the two bindings
	// they missed.
	if forSession != sessionID {
		return errConfirmInvalid
	}
	// 🔴 THE ACTION BINDING, AND IT IS CHECKED BEFORE THE SUBJECT FOR THE SAME REASON
	// THE SESSION IS. A value minted to remove a venue must not open the deactivation
	// gate; reporting that as "that was for another person" would tell a prober which
	// binding they missed. It is `errConfirmInvalid` — the collapsed refusal.
	//
	// ⚠️ WHY IT MATTERS TODAY EVEN THOUGH NOTHING COULD EXPLOIT IT. Before this,
	// audit measured a venue-removal confirmation passing the /admin/employees/
	// deactivate gate cleanly; the request then failed in the DOMAIN, because a
	// location id is not an employee id. That is two accidents deep — the uuid spaces
	// merely do not overlap — and the token itself knew nothing about it. A binding
	// that holds by luck is one a schema change can remove.
	if !hmacEqualString(forAction, action) {
		return errConfirmInvalid
	}
	if !hmacEqualString(forSubject, subject) {
		return errConfirmOtherPerson
	}
	return nil
}

// hmacEqualString compares two action names without leaking which prefix matched.
// They are server-chosen constants, so this is belt rather than braces — but a
// comparison on an authenticated field is the habit redline R7 asks for.
func hmacEqualString(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
