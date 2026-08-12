package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
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
	token, err := a.setConfirmation(w, r, confirmActionPolicyEdit, c.subject())
	if err != nil {
		// §4.6: an owner whose confirmation could not be minted is told the action is
		// unavailable, not shown a button that would fail.
		a.log.Error("panel: could not mint the policy-change confirmation", "err", err)
		a.redirect(w, policiesReturn("unavailable"))
		return
	}
	a.render(w, r, http.StatusOK, pages.AdminPolicyChange(a.policyChangeView(r, c, token)))
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
	if why := a.confirmationRefusalFor(r, confirmActionPolicyEdit, c.subject()); why != "" {
		a.log.Warn("panel policy change refused: the confirmation step was not completed",
			"op", c.op, "reason", why)
		a.redirect(w, policiesReturn(why))
		return
	}
	a.short.clear(w, adminConfirmCookieName)

	actor := actorOf(id)
	var err error
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
	a.render(w, r, http.StatusOK, pages.AdminPolicyVersion(a.policyVersionView(r, body)))
}

// --- the two screens this file renders -------------------------------------------

// policyChangeView turns a validated change into the confirmation screen.
//
// 🔴 THE SENTENCES ARE CHOSEN FROM THE OPERATION, WHICH IS A WORD FROM A CLOSED
// LIST, so there is nothing on this page a caller can put words into. The one value
// that comes from a human — the rule's name — is cleaned and bounded at the boundary
// and rendered as text by templ, which escapes it.
func (a *AdminAuth) policyChangeView(r *http.Request, c policyChange, token string) pages.PolicyChangeView {
	v := pages.PolicyChangeView{
		PanelChrome:  a.chrome(r, pages.TabPolicies),
		Action:       policyApplyHref,
		ConfirmField: confirmField,
		ConfirmToken: token,
		BackHref:     policiesHref,
		Fields:       policyChangeFields(c),
	}
	switch c.op {
	case opEnable:
		v.Heading = "Switch this rule back on"
		v.Summary = "From the next tap onwards, this rule takes part in every decision again."
		v.Warning = "Taps judged while it was off were judged without it, and those records " +
			"do not change."
	case opDisable:
		v.Heading = "Switch this rule off"
		v.Summary = "From the next tap onwards, this rule takes no part in any decision. It is " +
			"not deleted: its history stays, so a record decided under it can still explain itself."
		v.Warning = "This is the change most likely to move somebody's hours. A rule that was " +
			"sending taps to your review queue will stop doing so, and taps that would have been " +
			"queued will simply be approved or refused by whatever decides next."
	case opBind:
		v.Heading = "Record where this rule applies"
		v.Summary = "This records the rule as bound to one more place."
		v.Warning = "A binding is a RECORD of scope, not the scope itself. What actually " +
			"decides where a rule applies is the “Where” line on each of its statements — the " +
			"engine reads that and not this. Nothing about how your taps are judged changes here."
	case opUnbind:
		v.Heading = "Stop recording this rule as applying there"
		v.Summary = "This removes one binding. It is the one thing on this page that is a real " +
			"deletion, and it is safe to be one: a binding carries no decision history — a " +
			"record names the rule that decided it where one did, and the exact stored " +
			"version where that rule was a stored one, and none of that lives in these rows."
		v.Warning = "As above: the engine does not read these rows, so how your taps are " +
			"judged does not change. What changes is what this page, and provisioning, will say " +
			"about where the rule belongs."
	case opAuthor:
		// 🔴 THE TWO BRANCHES OF THIS FORM GET DIFFERENT WORDS. They said the same thing
		// for a round — including "the one it replaces stays readable" on a CREATE, where
		// there is nothing to replace — and the only visible difference between the two
		// screens was a uuid on a detail line. A confirmation step whose whole claim is
		// that it shows what will happen cannot describe two acts identically.
		if c.policyID == uuid.Nil {
			v.Heading = "Write a new rule of your own"
			v.Summary = "This creates a rule that is yours: a new entry beside the Tappa " +
				"baseline, starting at version 1. Nothing existing is changed or replaced."
		} else {
			v.Heading = "Write a new version of your rule"
			v.Summary = "This stores a NEW VERSION of a rule you already have. Nothing is " +
				"overwritten — a policy's versions can never be edited or deleted, so the " +
				"version this one supersedes stays readable for the records it decided."
		}
		v.Warning = "Your copy does NOT switch the Tappa rule off. Until you do that, both are " +
			"weighed together and the more specific one wins where they disagree — so a copy " +
			"scoped to one venue changes that venue and leaves the rest as they were."
	}
	v.Detail = append(v.Detail, pages.PolicyChangeLine{Label: "What", Value: c.op})
	if c.policyID != uuid.Nil {
		v.Detail = append(v.Detail, pages.PolicyChangeLine{Label: "Rule", Value: c.policyID.String()})
	}
	if c.resource != "" {
		v.Detail = append(v.Detail, pages.PolicyChangeLine{Label: "Scope", Value: c.resource})
	}
	if c.op == opAuthor {
		v.Detail = append(v.Detail,
			pages.PolicyChangeLine{Label: "Called", Value: c.name},
			pages.PolicyChangeLine{Label: "A version of", Value: c.basedOn},
			pages.PolicyChangeLine{Label: "Where", Value: venueSummary(c.venues)})
	}
	return v
}

// venueSummary says which venues an authored rule names, WITHOUT pretending an
// empty list means nothing: it means everywhere, which is what the shipped rule
// already says.
func venueSummary(venues []uuid.UUID) string {
	if len(venues) == 0 {
		return "every venue"
	}
	out := make([]string, 0, len(venues))
	for _, v := range venues {
		out = append(out, v.String())
	}
	return strings.Join(out, " · ")
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
	v := pages.PolicyVersionPageView{
		PanelChrome: a.chrome(r, pages.TabPolicies),
		PolicyName:  b.PolicyName,
		Layer:       b.Layer,
		Version:     "version " + strconv.Itoa(b.VersionNo),
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
