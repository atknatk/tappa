package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE POLICIES SECTION'S WRITE SIDE — M6-09 phase B.
//
// 🔴 THE ENGINE IS THE GATE, AND IT IS NOT IN THIS FILE. `policy:edit` is
// owner-only (ADR 0004 K2), and the M6-09 card carried "the panel does not enforce
// it" across three tasks. It is enforced now, inside
// internal/domain/tenant.RuleWriter — every write method authorises before it opens
// a transaction, so a handler in this package cannot reach a policy statement
// without passing the gate even by accident. What THIS file adds is the courtesy
// half: a manager is not shown a control the server would refuse. Both halves are
// required and neither substitutes for the other (locationactions.go's mayRemove
// makes the same argument about the one role check that already shipped).
//
// 🔴 THE TENANT AND THE ACTOR ARE NEVER INPUTS (§4.5, §4.7). Both come from
// httpx.AdminOf(r), which the Protect chain resolved from a signed session cookie
// against the database — including the ROLE, which is what the guardrail keys on.
// A role that could be posted would be an attacker's own claim about themselves.
//
// 🔴 THE ROUTES MOUNT ProtectWriting AND THE ORDER IS THE POINT.
// floodGate → sameOriginGate → requireAdmin → sessionGate: the Origin check runs
// BEFORE the resolver, so a cross-origin POST costs zero database work. A POST
// registered in mountSections would silently take the READ chain — the defect an
// audit measured on POST /admin/review, where 300 cross-origin posts spent a
// manager's whole session budget and charged 301 unbudgeted lookups on the way.
//
// 🔴 TWO STEPS FOR EVERY CHANGE, AND THE FIRST ONE WRITES NOTHING — this is the
// M6-09 card's "takes effect immediately" worry answered. The card's original trap
// said the simulator (M6-10) had to run first; M6-10 is SKIPPED (Q22 moved it to
// M9-06), so that answer is unavailable in v1 and the card records the three that
// are left. This takes option (a): a confirmation step that shows the TEXT of the
// change before it is written, plus a sentence saying plainly what it does NOT
// show — which taps would come out differently. Option (b), an undo, was weighed
// and is NOT taken as a general feature: policy_versions is append-only, so an undo
// is a second version rather than an erasure, and offering it as a button would
// imply a reversibility the table does not have. The ONE place it is taken is a
// change that would lock the author out of their own rulebook, which has no route
// back at all (tenant.RuleWriter.lockoutCheck).
//
// 🔴 §4.7 — NO SECRET PASSES THROUGH THIS FILE. The bodies carry uuids, a closed
// vocabulary of operations, a resource string from a closed grammar and a rule
// name; the confirmation is a MAC over server-chosen fields that is deliberately
// rendered into the page, like the login form's synchronizer token.

// panelScribe is the policies section's write side, declared HERE at the consumer
// (§7).
//
// EVERY METHOD TAKES AN ACTOR, and that is the interface stating the gate rather
// than hoping for it: there is no way to spell a call to any of them without saying
// who is asking, and the concrete type refuses before it opens a transaction.
type panelScribe interface {
	May(ctx context.Context, tenantID uuid.UUID, actor tenant.Actor) bool
	SetEnabled(ctx context.Context, c tenant.EnableCommand) (tenant.SwitchResult, error)
	Bind(ctx context.Context, c tenant.BindCommand) error
	Unbind(ctx context.Context, c tenant.BindCommand) error
	Author(ctx context.Context, c tenant.AuthorCommand) (tenant.AuthoredRule, error)
	RecordRefusal(ctx context.Context, tenantID uuid.UUID, actor tenant.Actor, act, target string) error
}

// The three addresses, built from the SECTION's own href rather than written out
// again — the rule every other section follows. If the tab moves, these move with
// it.
var (
	// policyChangeHref is the confirmation step. It RENDERS rather than redirecting,
	// for the reason manualEntryReview does: what travels between the steps (a rule
	// name, a scope) must not go in a URL, and re-posting it is safe because it writes
	// nothing.
	policyChangeHref = policiesHref + "/change"
	// policyApplyHref is the write. A SEPARATE address so the one route in this file
	// that changes a customer's rulebook is the one route named after doing so.
	policyApplyHref = policiesHref + "/apply"
	// policyVersionHref is the bounded single-version read (phase A's counted gap).
	// It is a GET and it changes nothing.
	policyVersionHref = policiesHref + "/version"
)

// maxPolicyBody is the ceiling on this section's POST bodies.
//
// A BARE ParseForm INHERITS net/http's 10 MB, and a valid session may spend
// adminSessionLimit requests per window — so the unbounded shape is 300 × 10 MB of
// allocation per session per ten minutes from somebody who has merely signed in.
// The real body is a word, two uuids, a resource string, a name and a MAC, plus one
// venue id per venue.
//
// ⚠️ THE PATTERN IS review.go's maxReviewBody; THE NUMBER IS NOT, and this line said
// "the constant and the pattern" while the two differ (8 KiB there, 16 KiB here). The
// difference is deliberate: this body can carry one `venue` field per venue a business
// has, which no review body does. Saying they share a constant would send the next
// author to change one and expect the other to follow.
const maxPolicyBody = 16 << 10

// maxPolicyName bounds the customer's label for their own rule.
//
// ⚠️ IT IS DELIBERATELY BELOW THE VALIDATOR'S OWN CEILING RATHER THAN EQUAL TO IT,
// and an earlier version of this comment claimed the opposite ("it is
// policy.DefaultLimits' document-name bound") -- which was false: that bound is
// internal/policy's maxNameLen, an UNEXPORTED 200, so this file could not read it
// even if it should. Cutting at 120 is a display decision: the name is rendered on a
// docket beside a version stamp, and the validator still has 80 characters of headroom
// to refuse anything this boundary let through. What must not happen is the reverse --
// a boundary ABOVE the validator's, where a customer types a name, reads it back on
// the confirmation screen and then has the save refused for a reason about characters.
const maxPolicyName = 120

// maxPolicyVenues bounds how many venues one authored rule may name.
//
// IT IS A REAL BOUND AND NOT A FORMALITY: each venue becomes a resource string in
// the stored document, and policy.DefaultLimits bounds a document's BYTES — so a
// post naming ten thousand venue ids would be refused by the validator with a
// message about bytes, which tells a customer nothing they can act on. Refusing it
// here says what to do.
const maxPolicyVenues = 200

// policyOps is the closed vocabulary of things this section can do. The form offers
// exactly these and the validator accepts exactly these — ONE list, which is M6-03's
// rule applied to a write: an operation the form offers and the server drops would
// not show an unfiltered page, it would silently refuse a change a customer believes
// they made.
const (
	opEnable  = "enable"
	opDisable = "disable"
	opBind    = "bind"
	opUnbind  = "unbind"
	opAuthor  = "author"
)

var policyOps = []string{opEnable, opDisable, opBind, opUnbind, opAuthor}

// policyProblemWords is the closed vocabulary the section's query string may echo.
// A reflected parameter is a value somebody can put anything into, and the answer is
// to have nothing to escape (oneOfWords).
var policyProblemWords = []string{
	"unreadable", "unknown", "not-permitted", "confirm-required", "confirm-stale",
	"bad-scope", "already-bound", "not-bound", "raced", "not-yours", "lockout",
	"unavailable", "no-such-version", "lockout-stands", "name-taken",
	"cross-location-first", "evidence-off",
}

// policyChange is a posted change after the boundary has validated every field (§7),
// so the domain sees data that is already good.
type policyChange struct {
	op       string
	policyID uuid.UUID
	resource string
	name     string
	basedOn  string
	venues   []uuid.UUID
}

// subject is what the confirmation MAC is bound to.
//
// 🔴 IT BINDS EVERY FIELD THE CONFIRMATION SCREEN SHOWS, INCLUDING THE NAME — AND
// THE NAME WAS MISSING FOR FOUR ROUNDS. This comment used to argue the name out:
// "it is a label, it decides nothing". An audit measured what that bought: a token
// minted while the screen said `Called: Alpha` was spent with `name=Beta…`, the write
// returned 303, and the rule stored was Beta. The confirmation step's whole claim is
// that it shows the TEXT of what will be written; a field it does not bind is a field
// the screen can be wrong about, which is the C′ shape this repository already pays
// for elsewhere (a heading that announces an act is verified against the row the same
// request reads).
//
// 🔴 THE NAME GOES IN AS A DIGEST, NOT AS ITSELF, AND THAT IS WHY IT COULD NOT SIMPLY
// BE APPENDED. adminConfirm.mint refuses a subject containing the payload's own
// separator, and a rule name is free text a customer types — so the one field that
// can contain "|" is the one field that cannot travel raw. A SHA-256 rendered as hex
// cannot contain either separator by construction, and binding a digest binds the
// value: a different name produces a different subject and the token stops verifying.
// It is not a secret and does not need to be — the name is on the screen.
//
// The other parts join with "/" because none of them can contain one that matters —
// a word from policyOps, a uuid, a resource from a closed grammar.
func (c policyChange) subject() string {
	parts := []string{c.op, c.policyID.String(), c.resource, c.basedOn, nameDigest(c.name)}
	for _, v := range c.venues {
		parts = append(parts, v.String())
	}
	return strings.Join(parts, "/")
}

// nameDigest binds a free-text name into a separator-free token. An empty name
// digests to "" so the subject of an operation that has no name is unchanged in shape.
func nameDigest(name string) string {
	if name == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// act is the word the audit trail and the log use for a refused attempt.
// It is the OPERATION, from the closed list, never a sentence.
func (c policyChange) act() string { return c.op }

// policyChangeReview answers the form with exactly what would be written, and mints
// the confirmation bound to it.
//
// 🔴 IT WRITES NO POLICY ROW AND NO AUDIT ROW. The only side effect is a Set-Cookie
// carrying the confirmation's twin. A refusal by the GATE, however, DOES write a
// trail row — see refusePolicyEdit for why an attempt is recorded even though
// nothing changed.
func (a *AdminAuth) policyChangeReview(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	c, ok := a.readPolicyChange(w, r)
	if !ok {
		return
	}
	if !a.scribe.May(r.Context(), id.TenantID(), actorOf(id)) {
		a.refusePolicyEdit(r, id, c)
		a.redirect(w, policiesReturn("not-permitted"))
		return
	}
	// THE RULEBOOK IS READ AFTER THE GATE AND BEFORE THE MINT (M12-38): the page names the
	// rule, its places, its version and its state, and the confirmation is bound to all of
	// it. A failed read is "unavailable" and mints nothing -- a page that could not say
	// what it is about must not offer the button.
	screen, err := a.rules.Screen(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not read the policy layer for a change review", "err", err)
		a.redirect(w, policiesReturn("unavailable"))
		return
	}
	shown := policyChangeShown(c, screen)
	token, err := a.setConfirmation(w, r, confirmActionPolicyEdit, policyChangeSubject(c, shown))
	if err != nil {
		// §4.6: an owner whose confirmation could not be minted is told the action is
		// unavailable, not shown a button that would fail.
		a.log.Error("panel: could not mint the policy-change confirmation", "err", err)
		a.redirect(w, policiesReturn("unavailable"))
		return
	}
	v := a.policyChangeView(r, c, shown, token)
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminPolicyChange(v))
}

// policyChangeApply verifies the confirmation and makes the change.
//
// 🔴 THE CONFIRMATION IS CHECKED BEFORE ANYTHING IS WRITTEN, and a refusal writes no
// policy row. It is bound to the whole change, so editing the posted form after the
// warning was served produces a value that does not verify.
//
// 🔴 THE COOKIE IS CLEARED BEFORE THE WRITE IS ATTEMPTED, so an ordinary browser
// cannot ride one mint twice. It is a REQUEST to the client rather than a ledger —
// deactivateconfirm.go counts what a client that re-prints it can still do. What
// bounds the damage here is that every operation this section offers is IDEMPOTENT
// in its effect: switching an already-off rule off writes nothing at all (the domain
// returns early without a trail row), and a second bind is refused as already-bound.
//
// 🔴 A SECOND Author IS THE EXCEPTION, AND WHAT THIS COMMENT USED TO SAY ABOUT IT WAS
// FALSE. It said the second save "appends a second version, which is visible in the
// history strip and reversible by appending a third". It did not: the form carried no
// policy id, so the second save took the CREATE branch and produced a SECOND POLICY
// with the same name — two rows, both deciding, neither in the other's history, and
// `policies` grants no DELETE, so neither could ever be removed. Two things changed:
// the form now carries the id when the owner asks for a new version, and a create
// whose name is already taken is refused before the row exists (ErrNameTaken). What
// a re-post can still do is append one more VERSION to a policy the owner chose,
// which IS in the history strip and IS superseded by appending another.
//
// ⚠️ AND ONE OUTCOME IS NOT IDEMPOTENT IN THE TRAIL, WHICH IS WORTH NAMING HERE
// BECAUSE IT SURPRISES: a switch that would lock the author out is MADE, refused and
// put back, so each attempt leaves TWO audit rows (policy.disabled and
// policy.enabled). The rule ends as it started — the state is idempotent — but the
// trail is not, and it should not be: each attempt really happened.
func (a *AdminAuth) policyChangeApply(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	c, ok := a.readPolicyChange(w, r)
	if !ok {
		return
	}
	// AN UNCONFIRMED OR FORGED POST COSTS NO READ. The subject below needs the rulebook,
	// so everything about the confirmation that does not -- present, signed by this
	// server, unexpired, this session's, this action's, this browser's -- is checked
	// first, with the word confirmationRefusalFor would give it.
	if why := a.policyConfirmationPrecheck(r); why != "" {
		a.log.Warn("panel policy change refused: the confirmation step was not completed",
			"op", c.op, "reason", why)
		a.redirect(w, policiesReturn(why))
		return
	}
	// 🔴 THE PAGE IS REBUILT FROM A FRESH READ AND ITS DIGEST MUST MATCH THE ONE MINTED.
	// The confirmation binds what the review page SHOWED -- the rule's name, its version
	// and what the change makes of it, its state, its places -- not only the posted
	// fields. A change made between the REVIEW and this read (another owner wrote a
	// version, switched the rule, renamed a venue) leaves the page the owner confirmed
	// untrue, and the token does not verify.
	//
	// ⚠️ WHAT IT DOES NOT CLOSE: the window between this read and the write below. A
	// version appended in those milliseconds is still appended after it; the domain's
	// unique (policy, version_no) only stops two writers taking the SAME number. Closing
	// it needs the write itself to check the version it supersedes (an expected version
	// on tenant.AuthorCommand) -- a separate domain task, not this page.
	screen, err := a.rules.Screen(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not read the policy layer to apply a change", "err", err)
		a.redirect(w, policiesReturn("unavailable"))
		return
	}
	if why := a.confirmationRefusalFor(r, confirmActionPolicyEdit, policyChangeSubject(c, policyChangeShown(c, screen))); why != "" {
		a.log.Warn("panel policy change refused: the confirmation step was not completed",
			"op", c.op, "reason", why)
		a.redirect(w, policiesReturn(why))
		return
	}
	a.short.clear(w, adminConfirmCookieName)

	actor := actorOf(id)
	switch c.op {
	case opEnable, opDisable:
		_, err = a.scribe.SetEnabled(r.Context(), tenant.EnableCommand{
			TenantID: id.TenantID(), Actor: actor, PolicyID: c.policyID, Enabled: c.op == opEnable,
		})
	case opBind:
		err = a.scribe.Bind(r.Context(), tenant.BindCommand{
			TenantID: id.TenantID(), Actor: actor, PolicyID: c.policyID, Resource: c.resource,
		})
	case opUnbind:
		err = a.scribe.Unbind(r.Context(), tenant.BindCommand{
			TenantID: id.TenantID(), Actor: actor, PolicyID: c.policyID, Resource: c.resource,
		})
	case opAuthor:
		_, err = a.scribe.Author(r.Context(), tenant.AuthorCommand{
			TenantID: id.TenantID(), Actor: actor, PolicyID: c.policyID,
			Name: c.name, BasedOn: c.basedOn, Venues: c.venues,
		})
	default:
		// Unreachable: readPolicyChange refuses an operation outside policyOps. The arm
		// exists so a sixth operation added to the vocabulary and forgotten here is a
		// refusal rather than a silent no-op that reports success.
		a.log.Error("panel: an operation passed validation and has no branch", "op", c.op)
		a.redirect(w, policiesReturn("unreadable"))
		return
	}
	if problem := a.policyRefusal(r, id, c, err); problem != "" {
		a.redirect(w, policiesReturn(problem))
		return
	}
	// 🔴 WHERE IT GOES AFTERWARDS IS THE THING ITSELF, NOT A SENTENCE ABOUT IT
	// (M6-04's lesson, taken the way M6-08 took it). The policies page re-reads the
	// rulebook from the database, so the rule's new state, its new version and its new
	// bindings are on the page the owner lands on. Nothing is asserted, because nothing
	// needs to be.
	a.redirect(w, policiesHref)
}

// policyRefusal turns a domain error into a query word, or "" when there was none.
//
// EVERY REFUSAL IS A WORD FROM policyProblemWords and every unexpected error is a
// 500-shaped redirect with a loud log line. A database outage must not be dressed up
// as "you are not allowed to do that": one is our failure and the other is a verdict
// on their request.
func (a *AdminAuth) policyRefusal(r *http.Request, id httpx.AdminIdentity, c policyChange, err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, tenant.ErrNotPermitted):
		// THE GATE REFUSED AT THE WRITE. It was already asked at the review step, so
		// reaching here means either the UI was bypassed or the rulebook changed in
		// between; both are worth a trail row for the same reason a refused venue removal
		// is.
		a.refusePolicyEdit(r, id, c)
		return "not-permitted"
	case errors.Is(err, tenant.ErrUnknownPolicy):
		return "unknown"
	case errors.Is(err, tenant.ErrBadResource):
		return "bad-scope"
	case errors.Is(err, tenant.ErrAlreadyBound):
		return "already-bound"
	case errors.Is(err, tenant.ErrNotBound):
		return "not-bound"
	case errors.Is(err, tenant.ErrVersionRace):
		return "raced"
	case errors.Is(err, tenant.ErrNameTaken):
		return "name-taken"
	case errors.Is(err, tenant.ErrNotYours):
		return "not-yours"
	case errors.Is(err, tenant.ErrCrossLocationFirst):
		// ADR 0029's guard (the domain wrote the trail row in the same transaction it
		// refused in, so there is nothing to record here).
		return "cross-location-first"
	case errors.Is(err, tenant.ErrEvidenceRuleOff):
		return "evidence-off"
	case errors.Is(err, tenant.ErrLockoutStands):
		// THE RARE HALF, AND IT GETS ITS OWN WORD BECAUSE ITS SENTENCE IS THE OPPOSITE
		// ONE. The change stands and the owner is locked out; telling them it was put
		// back would send them away believing the reverse of what happened.
		a.log.Error("panel: a policy change locked its author out and could not be undone",
			"err", err, "op", c.op)
		return "lockout-stands"
	case errors.Is(err, tenant.ErrWouldLockOut):
		return "lockout"
	default:
		a.log.Error("panel: could not change the policy", "err", err, "op", c.op)
		return "unavailable"
	}
}

// refusePolicyEdit records an attempt the engine refused.
//
// 🔴 THE SCREEN NEVER OFFERS THESE CONTROLS TO A MANAGER, SO REACHING HERE MEANS THE
// UI WAS BYPASSED — a stale page, a shared link, or a deliberate POST. It is written
// to audit_log for the reason location.delete_refused is: a structured log line has
// no tenant boundary, no retention story and no reader outside the operator, and the
// person who most needs to know a manager has been trying to rewrite the rules is
// the OWNER. Bloat is bounded by adminSessionLimit (300 requests per session per ten
// minutes), which is measured rather than assumed.
//
// A FAILED TRAIL WRITE DOES NOT CHANGE THE ANSWER. The refusal already happened;
// turning a trail outage into "the panel is unavailable" would report our failure as
// a verdict on their request.
func (a *AdminAuth) refusePolicyEdit(r *http.Request, id httpx.AdminIdentity, c policyChange) {
	a.log.Warn("panel policy change refused: the engine does not allow this actor policy:edit",
		"op", c.op, "actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
	target := c.policyID.String()
	if err := a.scribe.RecordRefusal(r.Context(), id.TenantID(), actorOf(id), c.act(), target); err != nil {
		a.log.Error("panel: could not record a refused policy change", "err", err)
	}
}

// actorOf lifts the resolved identity into the domain's Actor.
//
// 🔴 BOTH FIELDS COME FROM THE SESSION. adminauth.Resolved.Role is filled from the
// admin_users row during session resolution, so this costs no extra query and cannot
// be supplied by the caller — which matters more here than anywhere else in the
// panel, because it is the value the guardrail keys on.
func actorOf(id httpx.AdminIdentity) tenant.Actor {
	return tenant.Actor{ID: id.Admin.AdminUserID, Role: id.Admin.Role}
}

// readPolicyChange bounds the body, parses it and validates every field.
//
// 🔴 THE BODY IS BOUNDED BEFORE IT IS PARSED. MaxBytesReader makes ParseForm fail on
// an oversized body rather than buffering it, so the ceiling is enforced by the READ
// (review.go's shape).
//
// 🔴 THE PARSE ERROR IS CLASSIFIED, NOT PRINTED. net/url's failure QUOTES THE
// OFFENDING INPUT — an audit measured three bytes of a manager's note reaching the
// process log through exactly this branch in M6-04.
func (a *AdminAuth) readPolicyChange(w http.ResponseWriter, r *http.Request) (policyChange, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPolicyBody)
	if err := r.ParseForm(); err != nil {
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel policy change refused: the form could not be read",
			"limit_bytes", maxPolicyBody, "reason", reason)
		a.redirect(w, policiesReturn("unreadable"))
		return policyChange{}, false
	}
	c := policyChange{op: oneOfWords(strings.TrimSpace(r.PostFormValue("op")), policyOps...)}
	if c.op == "" {
		a.log.Warn("panel policy change refused: the operation is not one this section offers")
		a.redirect(w, policiesReturn("unreadable"))
		return policyChange{}, false
	}
	// THE POLICY ID IS OPTIONAL FOR ONE OPERATION ONLY. Authoring a NEW rule has no id
	// yet; every other operation acts on a row that exists, and an absent id there is a
	// refusal rather than a default.
	raw := strings.TrimSpace(r.PostFormValue("policy"))
	if raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			a.log.Warn("panel policy change refused: the posted policy id is not a uuid")
			a.redirect(w, policiesReturn("unreadable"))
			return policyChange{}, false
		}
		c.policyID = parsed
	}
	if c.policyID == uuid.Nil && c.op != opAuthor {
		a.log.Warn("panel policy change refused: this operation needs a policy", "op", c.op)
		a.redirect(w, policiesReturn("unknown"))
		return policyChange{}, false
	}
	switch c.op {
	case opBind, opUnbind:
		c.resource = strings.TrimSpace(r.PostFormValue("resource"))
		// THE GRAMMAR IS THE DOMAIN'S, ASKED RATHER THAN REPEATED. A second copy of
		// "location|department|employee plus a uuid, or *" in this file is a second
		// representation of a vocabulary the engine owns.
		if !tenant.ValidResource(c.resource) {
			a.log.Warn("panel policy change refused: the posted scope is not a resource")
			a.redirect(w, policiesReturn("bad-scope"))
			return policyChange{}, false
		}
	case opAuthor:
		// THE NAME IS CLEANED AND BOUNDED AT THE BOUNDARY, so the domain sees a label it
		// can store. The clip flag is discarded for manualentry.go's reason: the very
		// next screen renders the cleaned name back, before anything is written, so the
		// owner reads exactly what will be stored.
		c.name, _ = cleanBoundedText(r.PostFormValue("name"), maxPolicyName)
		c.basedOn = strings.TrimSpace(r.PostFormValue("based_on"))
		if c.name == "" || c.basedOn == "" {
			a.log.Warn("panel policy change refused: an authored rule needs a name and a source")
			a.redirect(w, policiesReturn("unreadable"))
			return policyChange{}, false
		}
		for _, v := range r.PostForm["venue"] {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			parsed, err := uuid.Parse(v)
			if err != nil {
				a.log.Warn("panel policy change refused: a posted venue id is not a uuid")
				a.redirect(w, policiesReturn("bad-scope"))
				return policyChange{}, false
			}
			c.venues = append(c.venues, parsed)
		}
		if len(c.venues) > maxPolicyVenues {
			a.log.Warn("panel policy change refused: too many venues for one rule",
				"limit", maxPolicyVenues, "posted", len(c.venues))
			a.redirect(w, policiesReturn("bad-scope"))
			return policyChange{}, false
		}
	}
	return c, true
}

// policiesReturn is the section's own address with a problem word on it.
//
// EVERY PART IS SERVER-BUILT FROM A CLOSED LIST, so this cannot become an open
// redirect and there is nothing in it to escape.
func policiesReturn(problem string) string {
	q := url.Values{}
	setIf(q, "problem", oneOfWords(problem, policyProblemWords...))
	if len(q) == 0 {
		return policiesHref
	}
	return policiesHref + "?" + q.Encode()
}

// policyVersionPage renders ONE stored version's body — phase A's counted gap.
//
// IT IS A GET AND IT CHANGES NOTHING, so it is registered in mountSections with the
// other reads. The bound is the shape of the query rather than a LIMIT: one version
// is at most policy.DefaultLimits' byte ceiling, where a PAGE of versions is bounded
// only by a row count that says nothing about bytes.
func (a *AdminAuth) policyVersionPage(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	policyID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("policy")))
	if err != nil {
		a.log.Warn("panel policy version: the requested policy id is not a uuid")
		a.redirect(w, policiesReturn("unreadable"))
		return
	}
	no, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("no")))
	if err != nil || no < 1 {
		a.log.Warn("panel policy version: the requested version number is not a number")
		a.redirect(w, policiesReturn("unreadable"))
		return
	}
	body, err := a.rules.Version(r.Context(), id.TenantID(), policyID, no)
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrUnknownVersion):
		// ONE ANSWER FOR "no such version" AND "another business's version", on purpose:
		// telling those apart across a tenant boundary is a probe.
		a.redirect(w, policiesReturn("no-such-version"))
		return
	default:
		a.log.Error("panel: could not read a policy version", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	v := a.policyVersionView(r, body)
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminPolicyVersion(v))
}

// --- the two screens this file renders -------------------------------------------

// policyChangeView wraps what the confirmation page shows in the panel's chrome and the
// form that makes the change.
//
// THE PAGE'S HEADING IS THE SHELL'S ONE h1 (M12-01): the title is the act, the trail
// leads back to Rules ("Rules / Review a change", the board's words). The kicker slot stays empty -- the board draws what kind of rule
// this is as the eyebrow over the rule's own name, inside the change card.
func (a *AdminAuth) policyChangeView(r *http.Request, c policyChange, shown pages.PolicyChangeShown, token string) pages.PolicyChangeView {
	chrome := a.chrome(r, pages.TabPolicies)
	chrome.Title = shown.Heading
	chrome.Crumbs = []pages.Crumb{{Label: "Rules", Href: policiesHref}, {Label: "Review a change"}}
	return pages.PolicyChangeView{
		PanelChrome:  chrome,
		Shown:        shown,
		Action:       policyApplyHref,
		ConfirmField: confirmField,
		ConfirmToken: token,
		BackHref:     policiesHref,
		Fields:       policyChangeFields(c),
	}
}

// policyChangeSubject is what the confirmation MAC is bound to: the posted change
// (policyChange.subject) AND a digest of everything the page shows about it.
//
// 🔴 THE PAGE SHOWS MORE THAN THE FORM CARRIES, SO THE MAC BINDS MORE THAN THE FORM
// (M12-38). The review page names the rule, prints its version and what the change makes
// of it ("version 2 → version 3"), its state, the places it applies, its history -- all
// read from the rulebook, none of it posted. A token bound to the posted fields alone
// would still open the write after another owner had written version 3 in between, and
// the store would get version 4 under a page that said 3. So the page's whole shown value
// is digested into the subject, and the write rebuilds that value from a fresh read
// (policyChangeApply): a page that has stopped being true cannot be confirmed.
//
// THE OTHER DIRECTION HOLDS BY CONSTRUCTION: every part of policyChange.subject is on
// the page -- the operation as the title and "What", the rule by name (or its id when no
// stored rule has it), the place, the name, the copied rule, the venues by name.
//
// The digest is hex, so it carries neither separator (adminConfirm.mint refuses "|";
// the parts join with "/"). %#v of a value made of strings, ints and slices is
// deterministic, which is all a digest needs.
func policyChangeSubject(c policyChange, shown pages.PolicyChangeShown) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%#v", shown)
	return c.subject() + "/" + hex.EncodeToString(h.Sum(nil))
}

// policyNoSubject is a subject no policy change can have (every real one begins with an
// operation word). policyConfirmationPrecheck asks the parser about it on purpose: a
// genuine token answers "minted for another subject", which is how the precheck learns
// that everything EXCEPT the subject holds without reading the rulebook.
const policyNoSubject = "-"

// policyConfirmationPrecheck refuses, BEFORE the rulebook read the subject needs, every
// confirmation that cannot be this page's: absent, malformed, forged, expired, another
// session's, another action's, or not this browser's (the cookie twin). It returns the
// word confirmationRefusalFor would return, or "" when only the subject is left to check.
//
// ⚠️ ONLY errConfirmOtherPerson PASSES. parse checks the signature, the expiry, the
// session and the action before the subject, and answers errConfirmOtherPerson only when
// all of them held and the subject differed -- which against policyNoSubject is every
// genuine token. Any other answer, including nil (a token somebody minted for "-"), is a
// refusal.
func (a *AdminAuth) policyConfirmationPrecheck(r *http.Request) string {
	sent := strings.TrimSpace(r.PostFormValue(confirmField))
	if sent == "" {
		return "confirm-required"
	}
	err := a.confirm.parse(sent, confirmActionPolicyEdit, policyNoSubject, httpx.AdminOf(r).Admin.SessionID)
	if !errors.Is(err, errConfirmOtherPerson) {
		return "confirm-required"
	}
	ck, cerr := r.Cookie(adminConfirmCookieName)
	if cerr != nil || ck.Value == "" || subtle.ConstantTimeCompare([]byte(ck.Value), []byte(sent)) != 1 {
		return "confirm-required"
	}
	return ""
}

// policyChangeShown builds everything the confirmation page says about one change from
// the change itself and ONE rulebook read (no second call: the newest stored version's
// body is the rule's own statements, already on the screen read).
//
// 🔴 ONLY WHAT THE OPERATION REALLY DOES IS DRAWN AS A CHANGE (D1). A switch moves a
// rule's state, a binding moves the bound list, a written rule moves a name and places.
// The owner never picks an effect -- an authored version copies a shipped rule's effect
// and conditions word for word (tenant.copyOfShipped) -- but a NEW VERSION may copy a
// DIFFERENT shipped rule from the one its predecessor copied, and then its answer or its
// conditions really do change. So the answer row and the conditions row are marked as
// changing exactly when the copied document differs from the version it replaces, and
// a sentence says so; otherwise the pills are printed as what each version says. And
// nothing predicts how a tap would come out (no simulator, M9-06).
//
// 🔴 EVERY STRING IS SERVER-CHOSEN: closed-vocabulary sentences, the rulebook's own
// words and the posted values the boundary already cleaned. templ escapes all of it.
func policyChangeShown(c policyChange, s tenant.PolicyScreen) pages.PolicyChangeShown {
	zone := s.Zone
	if zone == nil {
		zone = time.UTC
	}
	places := newPolicyPlaces(s)
	var sh pages.PolicyChangeShown
	sh.Heading, sh.Summary, sh.Warning = policyChangeWords(c)

	rule, found := policyRuleByID(s, c.policyID)
	var row pages.PolicyRuleView
	if found {
		row = policyRuleView(rule, zone, s.GuardrailsOnly, policyEditor{})
	}
	// DECIDING is the engine's sense of "in use": stored, switched on, and the layer not
	// dropped. A switched-off rule's newest version is stored, not in use.
	deciding := found && rule.State == tenant.StateActive && !s.GuardrailsOnly

	switch {
	case c.policyID == uuid.Nil:
		sh.RuleKind, sh.RuleTitle = "A new rule of your own", c.name
	case !found:
		// THE ID IS PRINTED BECAUSE IT IS WHAT THE BUTTON WOULD ACT ON: a confirmation
		// that hid the one value it is bound to would be the C′ shape again.
		sh.RuleKind, sh.RuleTitle = "A rule this page cannot find", c.policyID.String()
		sh.Notes = append(sh.Notes, "No rule with this id is stored for this business, so "+
			"there is nothing about it to show here.")
	case rule.Managed:
		sh.RuleKind, sh.RuleTitle = "Taptime baseline", row.Title
	default:
		sh.RuleKind, sh.RuleTitle = "One of your own policies", row.Title
	}

	var sids []string
	for _, st := range row.Statements {
		sids = append(sids, st.Sid)
	}
	version := ""
	if found && rule.VersionNo > 0 {
		version = "version " + strconv.Itoa(rule.VersionNo)
	}
	nothing := []pages.PolicyChangeDiffPart{{Text: "—"}}

	switch c.op {
	case opEnable, opDisable:
		if !found {
			sh.Before = pages.PolicyChangeSide{Label: "Before · now", Lines: []pages.PolicyChangeDiffLine{{Label: "State", Parts: nothing}}}
			sh.After = pages.PolicyChangeSide{Label: "After · from the next tap", Lines: []pages.PolicyChangeDiffLine{{Label: "State", Parts: nothing}}}
			break
		}
		target, move := "Switched off by you", "switched on → switched off"
		if c.op == opEnable {
			target, move = "In force", "switched off → switched on"
			// THE RULES PAGE'S OVERRIDE, KEPT: when the engine has dropped the layer a
			// switched-on rule decides nothing, so "In force" would be the page's most
			// confident lie (policyRuleView).
			if s.GuardrailsOnly {
				target = "Stored — but not deciding today"
			}
		}
		was, now := "del", "ins"
		// ALREADY THERE: the domain returns early and writes nothing, so nothing is
		// drawn as moving and the page says so.
		if (c.op == opDisable) == (rule.State == tenant.StateOff) {
			was, now, target = "", "", row.StateLabel
			sh.Notes = append(sh.Notes, "The rule is already in that state, so making this "+
				"change writes nothing.")
		}
		sh.RuleLine = joinDot(nonEmpty(joinDot(sids), version, move))
		sh.Before = pages.PolicyChangeSide{Label: "Before · now", Lines: appendAnswer(
			[]pages.PolicyChangeDiffLine{{Label: "State", Parts: []pages.PolicyChangeDiffPart{{Text: row.StateLabel, Mark: was}}}},
			row.Statements)}
		sh.After = pages.PolicyChangeSide{Label: "After · from the next tap", Lines: appendAnswer(
			[]pages.PolicyChangeDiffLine{{Label: "State", Parts: []pages.PolicyChangeDiffPart{{Text: target, Mark: now}}}},
			row.Statements)}
		sh.Notes = append(sh.Notes, "What the rule says stays as it is; only whether it "+
			"takes part in decisions changes. Nothing is deleted.")

	case opBind, opUnbind:
		sh.RuleLine = joinDot(nonEmpty(joinDot(sids), version, "no new version"))
		bound := slices.Contains(rule.Attachments, c.resource)
		var before, after []pages.PolicyChangeDiffPart
		for _, res := range rule.Attachments {
			mark := ""
			if c.op == opUnbind && res == c.resource {
				mark = "del"
			}
			before = append(before, pages.PolicyChangeDiffPart{Text: places.label(res), Mark: mark})
			if mark == "" {
				after = append(after, pages.PolicyChangeDiffPart{Text: places.label(res)})
			}
		}
		if c.op == opBind && !bound {
			after = append(after, pages.PolicyChangeDiffPart{Text: places.label(c.resource), Mark: "ins"})
		}
		switch {
		case c.op == opBind && bound:
			sh.Notes = append(sh.Notes, "It is already recorded there, so this change will be "+
				"refused as one that was already true.")
		case c.op == opUnbind && !bound:
			sh.Notes = append(sh.Notes, "It is not recorded there, so this change will be "+
				"refused as one there is nothing to remove for.")
		}
		if len(before) == 0 {
			before = []pages.PolicyChangeDiffPart{{Text: "nothing"}}
		}
		if len(after) == 0 {
			after = []pages.PolicyChangeDiffPart{{Text: "nothing"}}
		}
		sh.Before = pages.PolicyChangeSide{Label: "Before · recorded now",
			Lines: []pages.PolicyChangeDiffLine{{Label: "Bound to", Parts: before}}}
		sh.After = pages.PolicyChangeSide{Label: "After · once you make it",
			Lines: []pages.PolicyChangeDiffLine{{Label: "Bound to", Parts: after}}}
		sh.Notes = append(sh.Notes, "Bookkeeping, not scope: what decides where a rule "+
			"applies is the “Where” line on each of its statements, and that does not change here.")

	case opAuthor:
		doc, docFound := shippedRule(c.basedOn)
		afterRes := []string{"location/*"}
		if len(c.venues) > 0 {
			afterRes = afterRes[:0]
			for _, v := range c.venues {
				afterRes = append(afterRes, "location/"+v.String())
			}
		}
		var beforeRes []string
		if len(rule.Statements) > 0 {
			beforeRes = rule.Statements[0].Resources
		}
		// THE COPY IS FLATTENED BY THE SAME FUNCTION THAT FLATTENED THE STORED VERSION, so
		// "differs" below compares like with like.
		copied := tenant.StatementsOf(doc.Document)
		var answer []pages.PolicyChangeDiffPart
		for _, st := range copied {
			answer = append(answer, pages.PolicyChangeDiffPart{
				Text: statementLabel(st.Effect, st.Actions), Effect: st.Effect,
			})
		}
		if !docFound {
			answer = []pages.PolicyChangeDiffPart{{Text: "— not a Taptime rule you can copy"}}
		}
		afterLabel := func(no string) string {
			if s.GuardrailsOnly || (found && !deciding) {
				return "After · " + no
			}
			return "After · " + no + ", from the next tap"
		}
		if !found {
			if c.policyID == uuid.Nil {
				sh.RuleLine = "→ version 1"
			}
			sh.Before = pages.PolicyChangeSide{Label: "Before · no such rule yet", Lines: []pages.PolicyChangeDiffLine{
				{Label: "Called", Parts: nothing}, {Label: "Where", Parts: nothing}, {Label: "Answer", Parts: nothing},
			}}
			var where []pages.PolicyChangeDiffPart
			for _, res := range afterRes {
				where = append(where, pages.PolicyChangeDiffPart{Text: places.label(res), Mark: "ins"})
			}
			sh.After = pages.PolicyChangeSide{Label: afterLabel("version 1"), Lines: []pages.PolicyChangeDiffLine{
				{Label: "Called", Parts: []pages.PolicyChangeDiffPart{{Text: c.name, Mark: "ins"}}},
				{Label: "Where", Parts: where},
				{Label: "Answer", Parts: answer},
			}}
			if c.policyID != uuid.Nil {
				sh.After.Label = afterLabel("a new version")
			}
		} else {
			next := "version " + strconv.Itoa(rule.VersionNo+1)
			sh.RuleLine = joinDot(nonEmpty(joinDot(sids), version+" → "+next))
			if rule.Managed {
				sh.Notes = append(sh.Notes, "This is a Taptime rule, not one of yours: a new "+
					"version of it cannot be written here, so this change will be refused.")
			}
			was, now := "", ""
			if rule.Name != c.name {
				was, now = "del", "ins"
			}
			var whereBefore, whereAfter []pages.PolicyChangeDiffPart
			for _, res := range beforeRes {
				mark := ""
				if !slices.Contains(afterRes, res) {
					mark = "del"
				}
				whereBefore = append(whereBefore, pages.PolicyChangeDiffPart{Text: places.label(res), Mark: mark})
			}
			for _, res := range afterRes {
				mark := ""
				if !slices.Contains(beforeRes, res) {
					mark = "ins"
				}
				whereAfter = append(whereAfter, pages.PolicyChangeDiffPart{Text: places.label(res), Mark: mark})
			}
			if len(whereBefore) == 0 {
				whereBefore = nothing
			}
			use := "newest stored"
			if deciding {
				use = "in use"
			}
			beforeLines := []pages.PolicyChangeDiffLine{
				{Label: "Called", Parts: []pages.PolicyChangeDiffPart{{Text: rule.Name, Mark: was}}},
				{Label: "Where", Parts: whereBefore},
			}
			afterLines := []pages.PolicyChangeDiffLine{
				{Label: "Called", Parts: []pages.PolicyChangeDiffPart{{Text: c.name, Mark: now}}},
				{Label: "Where", Parts: whereAfter},
			}
			// THE ANSWER AND THE CONDITIONS, MARKED ONLY WHERE THE COPY REALLY DIFFERS from
			// the version it replaces (a new version may copy a different shipped rule).
			//
			// 🔴 AND ONLY WHERE THE PAGE KNOWS WHAT THAT VERSION SAYS. A stored version this
			// release cannot read has no statements on the screen read; comparing against
			// that empty list would print "answer differs" and "Only when: none" about a
			// document nobody here has read -- and the confirmation would bind the false
			// sentence. So the page says it cannot tell instead.
			beforeAnswer := answerParts(row.Statements)
			readable := row.Problem == "" && len(row.Statements) > 0
			if !readable {
				which := version
				if which == "" {
					which = "the stored version"
				}
				// TWO CASES, TWO SENTENCES: a version this release cannot parse, and one the
				// screen read does not carry (a switched-off rule's document is not read).
				if row.Problem != "" {
					sh.Notes = append(sh.Notes, capitalFirst(which)+" could not be read, so this page "+
						"cannot say what it answered.")
				} else {
					sh.Notes = append(sh.Notes, "This page does not show what "+which+" says, so it "+
						"cannot say whether this version answers differently.")
				}
			}
			if readable && docFound && !slices.Equal(effectsOf(row.Statements), effectsOfRule(copied)) {
				beforeAnswer = marked(beforeAnswer, "del")
				answer = marked(answer, "ins")
				sh.Notes = append(sh.Notes, "This version’s answer differs from "+version+"’s.")
			}
			if len(beforeAnswer) > 0 {
				beforeLines = append(beforeLines, pages.PolicyChangeDiffLine{Label: "Answer", Parts: beforeAnswer})
			}
			afterLines = append(afterLines, pages.PolicyChangeDiffLine{Label: "Answer", Parts: answer})
			if beforeCond, afterCond := conditionsOfView(row.Statements), conditionsOfRule(copied); readable && docFound && !slices.Equal(beforeCond, afterCond) {
				beforeLines = append(beforeLines, pages.PolicyChangeDiffLine{Label: "Only when", Parts: textParts(beforeCond, "del")})
				afterLines = append(afterLines, pages.PolicyChangeDiffLine{Label: "Only when", Parts: textParts(afterCond, "ins")})
				sh.Notes = append(sh.Notes, "This version’s conditions differ from "+version+"’s.")
			}
			sh.Before = pages.PolicyChangeSide{Label: "Before · " + version + ", " + use, Lines: beforeLines}
			sh.After = pages.PolicyChangeSide{Label: afterLabel(next), Lines: afterLines}
		}
		if docFound {
			sh.Notes = append(sh.Notes, "The answer, the condition and the engine’s reason are "+
				"copied from “"+doc.Name+"” word for word. Only the name and the venues are yours. "+
				"What any tap will now get is not worked out here — there is no simulator yet.")
		} else {
			sh.Notes = append(sh.Notes, "“"+c.basedOn+"” is not a Taptime rule you can base your "+
				"own on, so this change will be refused.")
		}
	}

	// THE SPECIFICS. Places by name, never by uuid; an id no name is known for is printed
	// with a sentence saying so (policyPlaces.label).
	sh.Detail = append(sh.Detail, pages.PolicyChangeLine{Label: "What", Value: policyChangeWhat(c)})
	if c.op == opAuthor {
		var where []string
		if len(c.venues) == 0 {
			where = append(where, places.label("location/*"))
		}
		for _, v := range c.venues {
			where = append(where, places.label("location/"+v.String()))
		}
		sh.Detail = append(sh.Detail,
			pages.PolicyChangeLine{Label: "A version of", Value: c.basedOn},
			pages.PolicyChangeLine{Label: "Where", Value: joinDot(where)})
	}
	if c.resource != "" {
		sh.Detail = append(sh.Detail, pages.PolicyChangeLine{Label: "Place", Value: places.label(c.resource)})
	}
	if found && version != "" {
		label, value := "Newest stored version", version
		if deciding {
			label = "Version in use"
		}
		for _, h := range rule.History {
			if h.Current {
				value += " · " + h.At.In(zone).Format("2 Jan 2006 15:04")
			}
		}
		sh.Detail = append(sh.Detail, pages.PolicyChangeLine{Label: label, Value: value})
	}
	// THE PAGE IS ONLY EVER RENDERED FOR AN ACTOR THE ENGINE GRANTS policy:edit (the
	// review asks May before anything else), so this is a fact about the reader, in the
	// engine's terms rather than a role's.
	sh.Detail = append(sh.Detail, pages.PolicyChangeLine{
		Label: "Who can save", Value: "you — the rules engine grants you policy:edit",
	})

	switch {
	case found:
		sh.History = row.History
		for i := range sh.History {
			// "IN USE" ONLY WHILE THE ENGINE IS USING IT: a switched-off rule's newest
			// version is stored, and nothing is in force.
			if sh.History[i].Marker != "" && !deciding {
				sh.History[i].Marker, sh.History[i].MarkerTone = "newest stored", pages.PolicyToneOff
			}
		}
		sh.HistoryNote = row.HistoryNote
		if len(sh.History) == 0 {
			sh.HistoryEmpty = "No version of this rule is stored yet."
		}
		if !rule.Managed && rule.StoredStamp != "" {
			sh.HistoryOrigin = "Its newest version was copied from the Taptime baseline of " +
				rule.StoredStamp + "."
		}
		if len(row.Statements) > 0 || row.Problem != "" {
			sh.InUseLabel = "The newest stored version, read in full"
			if deciding {
				sh.InUseLabel = "The version in use, read in full"
			}
			sh.InUseProblem = row.Problem
			for _, st := range row.Statements {
				st.Resources = places.labels(st.Resources)
				sh.InUseStatements = append(sh.InUseStatements, st)
			}
		}
	case c.policyID == uuid.Nil:
		sh.HistoryEmpty = "A new rule has no history yet. Making this change stores its version 1."
	default:
		sh.HistoryEmpty = "No rule with this id is stored for this business, so there is no " +
			"history to show."
	}
	return sh
}

// policyChangeWords are the change in words: the title, the lead and the consequence
// sentence, chosen from the operation's closed vocabulary.
//
// 🔴 THE SENTENCES ARE CHOSEN FROM THE OPERATION, WHICH IS A WORD FROM A CLOSED
// LIST, so there is nothing here a caller can put words into.
func policyChangeWords(c policyChange) (heading, summary, warning string) {
	switch c.op {
	case opEnable:
		heading = "Switch this rule back on"
		summary = "From the next tap onwards, this rule takes part in every decision again."
		warning = "Taps judged while it was off were judged without it, and those records " +
			"do not change."
	case opDisable:
		heading = "Switch this rule off"
		summary = "From the next tap onwards, this rule takes no part in any decision. It is " +
			"not deleted: its history stays, so a record decided under it can still explain itself."
		warning = "This is the change most likely to move somebody's hours. A rule that was " +
			"sending taps to The Spike will stop doing so, and taps that would have been " +
			"queued will simply be approved or refused by whatever decides next."
	case opBind:
		heading = "Record where this rule applies"
		summary = "This records the rule as bound to one more place."
		warning = "A binding is a RECORD of scope, not the scope itself. What actually " +
			"decides where a rule applies is the “Where” line on each of its statements — the " +
			"engine reads that and not this. Nothing about how your taps are judged changes here."
	case opUnbind:
		heading = "Stop recording this rule as applying there"
		summary = "This removes one binding. It is the one thing on this page that is a real " +
			"deletion, and it is safe to be one: a binding carries no decision history — a " +
			"record names the rule that decided it where one did, and the exact stored " +
			"version where that rule was a stored one, and none of that lives in these rows."
		warning = "As above: the engine does not read these rows, so how your taps are " +
			"judged does not change. What changes is what this page, and provisioning, will say " +
			"about where the rule belongs."
	case opAuthor:
		// 🔴 THE TWO BRANCHES OF THIS FORM GET DIFFERENT WORDS. They said the same thing
		// for a round — including "the one it replaces stays readable" on a CREATE, where
		// there is nothing to replace. A confirmation step whose whole claim is that it
		// shows what will happen cannot describe two acts identically.
		if c.policyID == uuid.Nil {
			heading = "Write a new rule of your own"
			summary = "This creates a rule that is yours: a new entry beside the Taptime " +
				"baseline, starting at version 1. Nothing existing is changed or replaced."
		} else {
			heading = "Write a new version of your rule"
			summary = "This stores a NEW VERSION of a rule you already have. Nothing is " +
				"overwritten — a policy's versions can never be edited or deleted, so the " +
				"version this one supersedes stays readable for the records it decided."
		}
		warning = "Your copy does NOT switch the Taptime rule off. Until you do that, both are " +
			"weighed together and the more specific one wins where they disagree — so a copy " +
			"scoped to one venue changes that venue and leaves the rest as they were."
	}
	return heading, summary, warning
}

// policyChangeWhat is the operation in a few words, for the "What" line.
func policyChangeWhat(c policyChange) string {
	switch c.op {
	case opEnable:
		return "Switch a rule back on"
	case opDisable:
		return "Switch a rule off"
	case opBind:
		return "Record where a rule applies"
	case opUnbind:
		return "Remove a record of where a rule applies"
	case opAuthor:
		if c.policyID == uuid.Nil {
			return "A new rule of your own"
		}
		return "A new version of one of your rules"
	default:
		return c.op
	}
}

// appendAnswer adds a rule's effects as one "Answer" row of read-only pills -- what the
// version says, unmarked: the operations that use it (a switch) do not move an effect.
func appendAnswer(lines []pages.PolicyChangeDiffLine, statements []pages.PolicyStatementView) []pages.PolicyChangeDiffLine {
	parts := answerParts(statements)
	if len(parts) == 0 {
		return lines
	}
	return append(lines, pages.PolicyChangeDiffLine{Label: "Answer", Parts: parts})
}

// answerParts is a version's effects as read-only pills.
func answerParts(statements []pages.PolicyStatementView) []pages.PolicyChangeDiffPart {
	var parts []pages.PolicyChangeDiffPart
	for _, st := range statements {
		parts = append(parts, pages.PolicyChangeDiffPart{Text: st.Label, Effect: st.Effect})
	}
	return parts
}

// marked returns parts with every one marked ("del" or "ins").
func marked(parts []pages.PolicyChangeDiffPart, mark string) []pages.PolicyChangeDiffPart {
	out := make([]pages.PolicyChangeDiffPart, 0, len(parts))
	for _, p := range parts {
		p.Mark = mark
		out = append(out, p)
	}
	return out
}

// textParts is a list of words as parts with one mark; an empty list is "none".
func textParts(items []string, mark string) []pages.PolicyChangeDiffPart {
	if len(items) == 0 {
		return []pages.PolicyChangeDiffPart{{Text: "none", Mark: mark}}
	}
	out := make([]pages.PolicyChangeDiffPart, 0, len(items))
	for _, it := range items {
		out = append(out, pages.PolicyChangeDiffPart{Text: it, Mark: mark})
	}
	return out
}

func effectsOf(statements []pages.PolicyStatementView) []string {
	out := make([]string, 0, len(statements))
	for _, st := range statements {
		out = append(out, st.Effect)
	}
	return out
}

func effectsOfRule(statements []tenant.RuleStatement) []string {
	out := make([]string, 0, len(statements))
	for _, st := range statements {
		out = append(out, st.Effect)
	}
	return out
}

// conditionsOfView and conditionsOfRule are a version's conditions in the words the
// page prints them, statement by statement, so the two sides compare as printed.
func conditionsOfView(statements []pages.PolicyStatementView) []string {
	var out []string
	for _, st := range statements {
		out = append(out, st.Conditions...)
	}
	return out
}

func conditionsOfRule(statements []tenant.RuleStatement) []string {
	var out []string
	for _, st := range statements {
		for _, c := range st.Conditions {
			out = append(out, c.Key+" "+conditionVerb(c.Operator)+" "+c.Value)
		}
	}
	return out
}

// policyRuleByID finds a stored rule on the screen. The zero id is never found: it is
// what an authoring post for a NEW rule carries, and what a shipped document with no
// stored row reports.
func policyRuleByID(s tenant.PolicyScreen, id uuid.UUID) (tenant.Rule, bool) {
	if id == uuid.Nil {
		return tenant.Rule{}, false
	}
	for _, list := range [][]tenant.Rule{s.Baseline, s.Tenant} {
		for _, r := range list {
			if r.PolicyID == id {
				return r, true
			}
		}
	}
	return tenant.Rule{}, false
}

// shippedRule is the shipped document an authored rule copies, asked of the domain's
// own list (tenant.AuthorableRules) -- the one the write will look it up in.
func shippedRule(name string) (policy.BaselineDoc, bool) {
	for _, doc := range tenant.AuthorableRules() {
		if doc.Name == name {
			return doc, true
		}
	}
	return policy.BaselineDoc{}, false
}

// capitalFirst upper-cases a sentence's first letter ("version 2" -> "Version 2"). The
// strings it gets are this file's own ASCII words.
func capitalFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// nonEmpty drops the empty strings from a list of parts.
func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// policyPlaces names a resource in the business's own words.
type policyPlaces struct {
	names  map[string]string
	capped bool
}

func newPolicyPlaces(s tenant.PolicyScreen) policyPlaces {
	p := policyPlaces{
		names: map[string]string{
			"*":          "every venue and department",
			"location/*": "every venue",
		},
		capped: s.ScopeCapped,
	}
	for _, t := range s.ScopeTargets {
		name := t.Name
		if t.Kind != "location" {
			name += " (" + t.Kind + ")"
		}
		p.names[t.Resource()] = name
	}
	return p
}

// label is a resource's name, or the raw resource with a sentence saying why no name is
// known.
//
// 🔴 AN UNNAMED PLACE IS SHOWN, NOT HIDDEN -- the Rules page's rule (policyEditor.label):
// a binding or a posted venue may name a place this business no longer has, and the
// button would act on it all the same. When the list of places was cut short the page
// does not claim the place is gone; it says the name was not read.
func (p policyPlaces) label(resource string) string {
	if name, ok := p.names[resource]; ok {
		return name
	}
	if p.capped {
		return resource + " (its name is past the places this page read)"
	}
	return resource + " (not a place of this business)"
}

func (p policyPlaces) labels(resources []string) []string {
	out := make([]string, 0, len(resources))
	for _, r := range resources {
		out = append(out, p.label(r))
	}
	return out
}

// policyChangeFields echoes the change back as hidden inputs.
//
// THE MAC IS WHAT MAKES THIS SAFE, not the hidden-ness: every value here is
// re-validated at the write, and the confirmation is bound to the same values, so
// editing one in the page produces a token that does not verify.
func policyChangeFields(c policyChange) []pages.PolicyChangeField {
	out := []pages.PolicyChangeField{{Name: "op", Value: c.op}}
	if c.policyID != uuid.Nil {
		out = append(out, pages.PolicyChangeField{Name: "policy", Value: c.policyID.String()})
	}
	if c.resource != "" {
		out = append(out, pages.PolicyChangeField{Name: "resource", Value: c.resource})
	}
	if c.op == opAuthor {
		out = append(out,
			pages.PolicyChangeField{Name: "name", Value: c.name},
			pages.PolicyChangeField{Name: "based_on", Value: c.basedOn})
		for _, v := range c.venues {
			out = append(out, pages.PolicyChangeField{Name: "venue", Value: v.String()})
		}
	}
	return out
}

// policyVersionView maps one stored version onto its page.
func (a *AdminAuth) policyVersionView(r *http.Request, b tenant.VersionBody) pages.PolicyVersionPageView {
	// THE PAGE'S OWN HEADING (M12-01): the trail back to Rules ends in the version, the
	// title is the rule's name and the eyebrow says whose rule it is.
	chrome := a.chrome(r, pages.TabPolicies)
	version := "version " + strconv.Itoa(b.VersionNo)
	chrome.Title = b.PolicyName
	chrome.Kicker = "One of your own policies · " + version
	if policy.Layer(b.Layer) == policy.LayerBaseline {
		chrome.Title = strings.TrimPrefix(b.PolicyName, baselineNamePrefix)
		chrome.Kicker = "Taptime baseline · " + version
	}
	chrome.Crumbs = []pages.Crumb{{Label: "Rules", Href: policiesHref}, {Label: "Version " + strconv.Itoa(b.VersionNo)}}
	v := pages.PolicyVersionPageView{
		PanelChrome: chrome,
		PolicyName:  b.PolicyName,
		Layer:       b.Layer,
		Version:     version,
		At:          b.At.Format("2 Jan 2006 15:04"),
		Author:      b.Author,
		Bytes:       strconv.Itoa(b.Bytes) + " bytes",
		Problem:     b.Problem,
		BackHref:    policiesHref,
	}
	// THE MARKER IS A CLAIM ABOUT THE ENGINE, so it says which of the two things it
	// means. "Not the newest" is not "wrong" and not "draft": it is what some records
	// were judged by, which is the whole reason this page exists.
	v.Marker = "an earlier version"
	if b.Current {
		v.Marker = "the newest stored version"
	}
	switch {
	case !b.Enabled:
		v.State = "This rule is switched off, so nothing is being judged by any version of it."
	case b.Current:
		v.State = "This is the version the engine would use for the next tap."
	default:
		v.State = "A newer version of this rule exists, so this one decides nothing today. It is " +
			"kept because records decided under it still name it."
	}
	for _, st := range b.Statements {
		s := pages.PolicyStatementView{
			Sid: st.Sid, Effect: st.Effect, Outcome: effectSentence(st.Effect),
			Actions: st.Actions, Resources: st.Resources, Reason: st.Reason,
		}
		for _, c := range st.Conditions {
			s.Conditions = append(s.Conditions, c.Key+" "+conditionVerb(c.Operator)+" "+c.Value)
		}
		v.Statements = append(v.Statements, s)
	}
	return v
}
