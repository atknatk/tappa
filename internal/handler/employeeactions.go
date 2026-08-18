package handler

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The EMPLOYEES section's WRITES — M6-05 PHASE B. Three routes, four actions:
// invite, re-invite (the same route pressed again), deactivate, and move somebody
// between venues or departments.
//
// 🔴 THE TENANT AND THE ACTOR ARE NEVER INPUTS (§4.5, §4.7). Both come from
// httpx.AdminOf(r), which the Protect chain resolved from a signed session cookie
// against the database. What a client contributes is an employee id, a venue id, a
// department id — every one of them a NARROWING value inside that tenant, because
// the tenant predicate they are ANDed with is not one the client supplied. A foreign
// id produces no row rather than a foreign write, and internal/domain/tenant turns
// that into a sentence rather than a 500.
//
// 🔴 THE ROUTES MOUNT ProtectWriting AND THE ORDER IS THE POINT.
// floodGate → sameOriginGate → requireAdmin → sessionGate: the Origin check runs
// BEFORE the resolver, so a cross-origin POST costs zero database work. M6-04
// shipped this chain with the gate INSIDE the protected group and an audit measured
// what that bought: 300 cross-origin POSTs spent a signed-in manager's whole session
// budget and locked them out of their own panel for ten minutes, charging 301
// unbudgeted session lookups on the way. That is the second time a chain was copied
// without counting its parts (adminFloodLimit records the first), so this file
// counts them: FOUR stages, and the third is the one that must not be first.
// TestEmployeeActions_CrossOriginCostsNoResolverRead measures the resolver call
// count rather than only the status code.
//
// 🔴 EVERY ACTION WRITES audit_log, AND THE ROW SHARES THE FATE OF THE CHANGE.
// Deactivate and move write theirs with audit.RecordTx inside the domain's own
// transaction (internal/domain/tenant/staff.go says why that is the opposite of
// what most callers of internal/audit want). The invitation's row is written by
// internal/invite's Channel BEFORE the link is shown, which is the ordering that
// package chose deliberately: the trail may over-report a disclosure that failed to
// render, and may never under-report one that happened.
//
// 🔴 §4.7 — THE ACTIVATION LINK IS RENDERED ONCE AND NEVER ANYWHERE ELSE. It exists
// in exactly one response body: the answer to the POST that minted it. It is not put
// in a redirect, not stored in a cookie, not written to audit_log (invite's
// codeShownDetail is a purpose-built struct with no field that could carry it), and
// it cannot be read back — the database keeps the HMAC. See employeeInvite for what a
// refresh does, measured.
//
// ⚠️ "NOT LOGGED" IS TWO CLAIMS AND EACH HAS ITS OWN NET, because for one round the
// first stood in for both. THIS handler's logger is asserted here
// (TestEmployeeInvite_ShowsTheLinkOnceAndNeverLogsIt); the DELIVERY SEAM's is
// asserted where the raw link actually lives
// (internal/invite's TestManagerVisibleChannel_DoesNotLogTheLinkItHolds). An audit
// added a log line to invite.DeliverInvite — the one function that holds the URL —
// and every test here stayed green, so the sentence was covering the wrong logger.

// panelStaff is the slice of internal/domain/tenant this section needs, declared
// HERE at the consumer (§7).
//
// IT IS A SEPARATE FIELD FROM panelLedger AND panelQueue, and the rule is the one
// M6-04 set: a different PACKAGE gets a different field. ledger reads, tenant
// writes, and keeping them apart is what makes "no store call in ledger is not a
// SELECT" a fact grep can check rather than a sentence to trust.
type panelStaff interface {
	Person(ctx context.Context, tenantID, employeeID uuid.UUID) (tenant.Person, error)
	Add(ctx context.Context, c tenant.AddCommand) (tenant.Person, error)
	Deactivate(ctx context.Context, c tenant.DeactivateCommand) (tenant.Person, error)
	Move(ctx context.Context, c tenant.MoveCommand) (tenant.Person, error)
}

// panelInviter is the slice of internal/invite the panel needs: ONE method, and it
// deliberately does not return the code.
//
// 🔴 THE SHAPE OF THIS INTERFACE IS THE §4.7 DESIGN, NOT AN ACCIDENT OF WHAT WAS
// AVAILABLE. invite.Manager.IssueAndDeliver hands the link to a Channel instead of
// returning it, because a returned credential would flow into handlers, templates
// and logs at each caller's discretion and code.go's "one egress" claim would be
// false. This file therefore cannot obtain a code except by implementing
// invite.LinkSink — which is the seam that package built for exactly this screen.
type panelInviter interface {
	IssueAndDeliver(ctx context.Context, p invite.IssueParams, ch invite.Channel) (invite.Invite, error)
}

// The three POST routes, built from the section's own URL rather than written out
// again — the rule employeesHref and reviewHref already follow. If the section
// moves, its actions move with it.
var (
	employeeAddHref        = employeesHref + "/add"
	employeeInviteHref     = employeesHref + "/invite"
	employeeDeactivateHref = employeesHref + "/deactivate"
	employeeMoveHref       = employeesHref + "/move"
)

// maxEmployeeActionBody is the ceiling on an action POST's body.
//
// 🔴 A BARE ParseForm INHERITS net/http's 10 MB. These routes sit in front of a
// database write and a valid session may spend adminSessionLimit requests per
// window, so the unbounded shape is 300 × 10 MB of allocation per session per ten
// minutes from somebody who has merely signed in. The real body is three uuids and
// at most the filter fields the roster echoes back; 8 KiB is already generous. The
// constant and the pattern are review.go's maxReviewBody and checkin.go's
// checkinMaxBody, which bound their routes for the same reason.
const maxEmployeeActionBody = 8 << 10

// The closed vocabularies this section's query string may echo. A reflected
// parameter is a value somebody can put anything into, and the answer is not to
// escape it more carefully but to have nothing to escape (oneOfWords, review.go).
var (
	// "added" (M6-13) joins the two lifecycle words. Like "moved" and unlike
	// "deactivated" it is NOT independently verifiable — a replayed URL cannot be
	// told from a fresh save — so the sentence beside it is built the way the moved
	// one is: it names the state the SAME request read back from the database
	// (who, and where they work), never the event.
	rosterDoneWords = []string{"added", "deactivated", "moved"}
	// 🔴 M6-13 ADDED FIVE WORDS HERE AND THEY WERE DELETED IN THE SAME MILESTONE,
	// BECAUSE NO CODE PATH COULD PRODUCE ONE. `bad-name`, `bad-email`, `bad-role`,
	// `email-taken` and `no-venue` arrived with sentences and headings on the roster
	// template — and every M6-13 refusal answers 200 through addFormAgain with the
	// message beside the offending control, so not one of them was ever written into a
	// query string. An audit counted the rosterReturn call sites: zero produced any of
	// them, and zero tests drove them.
	//
	// THE PRECEDENT IS THIS SECTION'S OWN, ONE CARD EARLIER: M6-12 deleted
	// fillBillingControl's unreachable branch rather than inventing a caller for it. A
	// word nothing produces is a sentence nobody can read, and it is not free — a
	// closed vocabulary reads as a map of what can go wrong, which is exactly what the
	// next person will trust it for.
	rosterProblemWords = []string{
		"unreadable", "unknown", "already-deactivated", "unknown-placement",
		"no-change", "not-invitable",
		// 🔴 "actions-unavailable" IS OURS RATHER THAN THE CALLER'S, and it is a
		// SEPARATE word because it names a different failure. An audit found the card's
		// failed read reusing "unreadable", which the vocabulary defines as "the REQUEST
		// could not be read ... nothing was looked up" — so a manager whose database
		// call failed was told their own submission was malformed. It is in the closed
		// set because the section handler puts it in the query string like the others.
		"actions-unavailable",
		// The two failure modes the confirmation gate creates (user decision,
		// 2026-08-08). They are separate words because they are separate mistakes:
		// one is "you did not confirm, or waited too long", the other is "this browser
		// is confirming a different person".
		"confirm-required", "confirm-stale",
	}
)

// employeeAdd puts somebody on the roster (M6-13).
//
// 🔴 THIS IS THE ROUTE THE PRODUCT SHIPPED WITHOUT. Until now the panel could invite,
// deactivate and move employees and could not create one; the gap was found by a
// customer, in production, who signed up and asked how. `/admin/employees/invite`
// does not close it — employeeInvite takes an employeeID and invites an EXISTING row,
// answering `not-invitable` when there is none.
//
// 🔴 A REJECTED FORM RE-RENDERS INSTEAD OF REDIRECTING, WHICH IS THE OTHER SHAPE THIS
// SECTION USES AND IS DELIBERATE. The three existing actions here are single-button
// acts on somebody already on the list: a redirect with a problem word loses nothing,
// because there was nothing to type. This form has five fields, and answering a
// mistyped address with a 303 would throw the other four away. saveVenue argues the
// same trade and this follows it, so the two forms in this panel behave alike under
// refusal.
//
// 🔴 AND THE SUCCESS PATH STILL REDIRECTS, so a refresh cannot add the same person
// twice. That asymmetry is the point of POST-redirect-GET and it is why the failure
// path is safe to render inline: nothing was written, so there is nothing a re-POST
// could duplicate.
func (a *AdminAuth) employeeAdd(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxEmployeeActionBody)
	if err := r.ParseForm(); err != nil {
		// 🔴 THE PARSE ERROR IS CLASSIFIED, NOT PRINTED. net/url's failure QUOTES THE
		// OFFENDING INPUT, and an audit measured three bytes of a manager's note
		// reaching the process log through exactly this branch in M6-04. A name and an
		// email address travel through this form, so nothing a caller sent is logged.
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel employee add refused: the form could not be read",
			"limit_bytes", maxEmployeeActionBody, "reason", reason)
		a.redirect(w, employeesHref+"?problem=unreadable")
		return
	}
	f := rosterFilterFrom(r.PostForm)

	// THE FORM IS REBUILT FROM WHAT WAS POSTED BEFORE ANYTHING IS VALIDATED, so every
	// refusal below can hand the manager their own typing back. The values are echoed
	// through templ's escaping on the way out, exactly like the venue form's.
	posted := components.StaffFormView{
		FullName:     r.PostFormValue("full_name"),
		Role:         r.PostFormValue("role"),
		Email:        r.PostFormValue("email"),
		LocationID:   strings.TrimSpace(r.PostFormValue("location_id")),
		DepartmentID: strings.TrimSpace(r.PostFormValue("department_id")),
	}

	locationID, err := uuid.Parse(posted.LocationID)
	if err != nil {
		// A MISSING VENUE AND AN UNREADABLE ONE ARE THE SAME INSTRUCTION ("choose a
		// venue"), so they share a sentence. employees.location_id is NOT NULL, so
		// there is no "nowhere" this could mean instead.
		a.log.Warn("panel employee add refused: the posted venue is not a uuid")
		a.addFormAgain(w, r, posted, "location_id", "Choose the venue this person works at.")
		return
	}
	var departmentID *uuid.UUID
	if posted.DepartmentID != "" {
		parsed, err := uuid.Parse(posted.DepartmentID)
		if err != nil {
			// Anything that is neither empty nor a uuid IS an error. Silently treating it
			// as "no department" would file somebody outside their department because a
			// form field was mangled — the same argument employeeMove makes.
			a.log.Warn("panel employee add refused: the posted department is not a uuid")
			a.addFormAgain(w, r, posted, "department_id", "Choose one of this venue's departments, or none.")
			return
		}
		departmentID = &parsed
	}

	person, err := a.staff.Add(r.Context(), tenant.AddCommand{
		TenantID: id.TenantID(),
		// 🔴 THE ACTOR COMES FROM THE SIGNED SESSION. There is no field on this form for
		// it and there could not be: audit_log.actor_id may only hold an admin resolved
		// from a cookie, never a value a request supplied.
		ActorID:      id.Admin.AdminUserID,
		FullName:     posted.FullName,
		Role:         posted.Role,
		Email:        posted.Email,
		LocationID:   locationID,
		DepartmentID: departmentID,
	})
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrEmployeeName):
		a.log.Warn("panel employee add refused: the name cannot be stored")
		a.addFormAgain(w, r, posted, "full_name",
			"A person needs a name we can store, and it has to be shorter than that.")
		return
	case errors.Is(err, tenant.ErrEmployeeEmail):
		a.log.Warn("panel employee add refused: the email cannot be stored")
		a.addFormAgain(w, r, posted, "email",
			"That does not look like an email address. Correct it, or leave it blank.")
		return
	case errors.Is(err, tenant.ErrEmployeeRole):
		a.log.Warn("panel employee add refused: the role cannot be stored")
		a.addFormAgain(w, r, posted, "role", "That job title is too long. Shorten it, or leave it blank.")
		return
	case errors.Is(err, tenant.ErrEmailTaken):
		// 🔴 THE CASE SENTENCE IS PART OF THE REFUSAL, NOT A FOOTNOTE. `email` is
		// citext: a manager who retypes the address with a different capital gets
		// refused again and has no way to know why unless the product says so.
		a.log.Warn("panel employee add refused: that email is already used in this tenant")
		a.addFormAgain(w, r, posted, "email",
			"Somebody in this business already has that address — and capitals make no "+
				"difference, so changing them will not help. Leave it blank if they do not need one.")
		return
	case errors.Is(err, tenant.ErrUnknownPlacement):
		// THE VENUE OR THE DEPARTMENT DID NOT RESOLVE INSIDE THIS BUSINESS. That covers
		// another tenant's id, an invented one, one removed since the page rendered, and
		// a department belonging to a DIFFERENT venue. They are one sentence because
		// they are one instruction, and none of them may be told apart for the reason
		// ErrUnknownEmployee gives: telling them apart answers "does this id exist
		// elsewhere?" for anybody who can post a form.
		a.log.Warn("panel employee add refused: the venue or department is not this tenant's")
		a.addFormAgain(w, r, posted, "location_id",
			"Choose a venue that belongs to this business. A department has to be one of that venue's.")
		return
	default:
		a.log.Error("panel: could not add the employee", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	// 🔴 THE REDIRECT OPENS THE NEW PERSON'S CARD, AND THAT IS WHAT CLOSES THE
	// ADD → INVITE CHAIN ON ONE SCREEN. Somebody added here is 'invited' and cannot
	// tap until an activation link is spent, so the very next thing a manager needs is
	// the invite button — which lives on the action card. Carrying ?manage=<new id>
	// means it is already open when the page comes back, rather than asking them to
	// find a person they have just created in a list of fifty.
	//
	// ⚠️ THE CURSOR IS DROPPED AND THE FILTERS ARE KEPT, and the limit of that is
	// worth stating rather than hiding. The roster is ORDER BY full_name, so a new
	// person lands ALPHABETICALLY, not at the top: on a long roster their ROW may be
	// on a later page, and a filter the manager set (status = active, say) will not
	// match somebody who is 'invited'. What is guaranteed is that the PERSON is on
	// screen — the action card is loaded by id and does not care about either — so the
	// receipt and the next action are always visible even when the row is not.
	a.log.Info("panel employee added", "employee_id", person.ID, "actor_id", id.Admin.AdminUserID)
	f.AfterID = nil
	a.redirect(w, rosterReturn(f, person.ID, "added", ""))
}

// addFormAgain re-renders the roster with the add form open, filled in, and carrying
// the refusal beside the control that caused it.
//
// 🔴 NOTHING WAS WRITTEN ON ANY PATH THAT REACHES HERE, so the 200 is honest: the page
// is the same page, in the same state, with one field marked. It answers 200 rather
// than 400 for the reason accountactions.go gives at the same fork — a form a person
// is looking at is not an API, and the status code a browser acts on here is the one
// that renders the correction.
func (a *AdminAuth) addFormAgain(w http.ResponseWriter, r *http.Request, form components.StaffFormView, field, msg string) {
	form.ErrorField = field
	form.ErrorMessage = msg
	v, ok := a.employeesView(w, r, &form)
	if !ok {
		return
	}
	a.render(w, r, http.StatusOK, pages.AdminEmployees(v))
}

// employeeInvite mints an activation link for one person and SHOWS IT, once.
//
// 🔴 IT RENDERS ITS RESPONSE INSTEAD OF REDIRECTING, WHICH BREAKS THIS PANEL'S OWN
// PATTERN, AND §4.7 IS WHY. Every other panel write answers 303 so that a refresh is
// harmless. A redirect cannot carry this payload: putting the activation link in the
// Location header would write a live credential into the browser's address bar, its
// history, any shared screen and any Referer a later navigation sends — the exact
// disclosure the roster's paging cursor was changed to avoid, only with a secret
// instead of a name. So the link goes in a body that is Cache-Control: no-store and
// nowhere else.
//
// 🔴 WHAT "SHOWN EXACTLY ONCE" MEANS HERE, MEASURED RATHER THAN PROMISED. There is
// no route that can render this link a second time: nothing stores it (00009 keeps
// the HMAC), invite.Manager never returns it, and no GET reads it. A manager who
// loses the page has one way forward, and it is the honest one — press the button
// again, which MINTS A NEW INVITATION with a new code and writes a second disclosure
// row to audit_log. The old invitation stays valid until it expires or is used;
// nothing here cancels it, and cancelling is a different operation the schema is
// explicit about (db/queries/invites.sql: it must NOT reuse used_at).
// TestPanelEmployeesDB_TheInviteLinkIsShownOnceAndNeverReadBack measures both halves.
//
// A BROWSER REFRESH IS THEREFORE A RE-POST, and it re-issues rather than replaying:
// the browser asks first, and the outcome is one more pending invitation and one
// more audit row. That is the cost of the rule above, and it is stated rather than
// hidden behind "shown once".
func (a *AdminAuth) employeeInvite(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f, employeeID, ok := a.readAction(w, r)
	if !ok {
		return
	}

	// THE PERSON IS LOADED FIRST, tenant-scoped, for two reasons that are both about
	// §4.6: the response has to NAME somebody, and an invitation for a deactivated
	// employee would be a code the consuming statement can never spend
	// (db/queries/invites.sql refuses it, deliberately — re-activating through an
	// invite would be a way around the manager-only deactivation). Offering a control
	// whose result is a dead credential is the failure this section is watched for.
	person, err := a.staff.Person(r.Context(), id.TenantID(), employeeID)
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrUnknownEmployee):
		a.log.Warn("panel invite refused: no such employee in this tenant")
		a.redirect(w, rosterReturn(f, uuid.Nil, "", "unknown"))
		return
	default:
		a.log.Error("panel: could not read the employee before inviting", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	if !person.Invitable() {
		a.log.Warn("panel invite refused: the employee is not in an invitable state",
			"employee_id", employeeID, "status", person.Status)
		a.redirect(w, rosterReturn(f, employeeID, "", "not-invitable"))
		return
	}

	// THE SINK IS PER-REQUEST AND LIVES ON THE STACK. A field on AdminAuth would be
	// shared by every concurrent request in the process, which is how one manager's
	// invitation link renders on another manager's screen.
	sink := &panelLinkSink{}
	actor := id.Admin.AdminUserID
	ch, err := invite.NewManagerVisibleChannel(sink, a.audit, &actor)
	if err != nil {
		a.log.Error("panel: could not build the invitation channel", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	inv, err := a.invites.IssueAndDeliver(r.Context(), invite.IssueParams{
		TenantID:   id.TenantID(),
		EmployeeID: employeeID,
	}, ch)
	if err != nil {
		// 🔴 THE ERROR IS LOGGED WITHOUT ITS CAUSE'S TEXT BEING TRUSTED WITH THE LINK.
		// invite.IssueAndDeliver wraps delivery failures with the INVITE ID and never
		// with anything about the code (its comment says so and its Delivery struct is
		// the only place the URL exists), so %v here cannot print one. The manager is
		// told the action failed rather than shown a page with no link on it.
		//
		// ⚠️ WHAT THIS BRANCH COSTS GOT WORSE WITH 00012 AND THE COMMENT DID NOT SAY SO.
		// It used to end "an invitation may well have been created — the row commits
		// before delivery — and it is visible as outstanding rather than LOST", which
		// was complete when issuing only ADDED. Since cancellation it is not: the
		// transaction that commits the new invitation ALSO retires the employee's
		// earlier ones, and delivery runs after it. So a delivery failure can leave an
		// employee whose working link has just been retired and whose replacement was
		// never shown to anybody — nobody can activate until the manager presses again.
		//
		// COUNTED, NOT CLOSED, and the reasons are measured rather than asserted: it is
		// NOT a §4.6 loss (the retirement is recorded in cancelled_at, tappa_app cannot
		// DELETE the rows, and every invitation remains readable), it needs an injected
		// failure to reach at all (the only production DeliverInvite writes one audit
		// row and renders), and it SELF-HEALS on the next press, which the screen
		// invites. Closing it would mean moving delivery inside the transaction — which
		// internal/invite refuses on purpose, because a delivered code that was never
		// committed is worse than a committed one that was never delivered.
		a.log.Error("panel: could not issue the invitation", "employee_id", employeeID, "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	if !sink.shown {
		// The channel returned success without ever calling the sink. That cannot
		// happen through invite.ManagerVisibleChannel, and if it ever does the manager
		// must not be shown a page with an empty link on it (§4.6: no silent failure).
		a.log.Error("panel: the invitation was issued but no link reached the screen",
			"invite_id", inv.ID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	a.render(w, r, http.StatusOK, pages.AdminInviteIssued(pages.InviteIssuedView{
		PanelChrome:   a.chrome(r, pages.TabEmployees),
		Name:          person.Name,
		ActivationURL: sink.link,
		Expires:       expiryPhrase(inv.CreatedAt, inv.ExpiresAt),
		Retired:       inv.RetiredSiblings,
		BackHref:      rosterReturn(f, employeeID, "", ""),
	}))
}

// expiryPhrase says how long the link lasts, as a LENGTH rather than as an instant.
//
// 🔴 IT AVOIDS A TIMEZONE ON PURPOSE, and the alternative was weighed. Every other
// date in this panel is rendered in the tenant's zone (CLAUDE.md §6), which the
// roster reader carries alongside its rows — this response has no rows, so printing
// an absolute deadline would mean either a second database read for the clock or a
// UTC instant on a screen where every other instant is local. A manager reading a
// deadline in the wrong zone tells an employee the wrong day, which is the failure
// worth avoiding; "7 days" is the same sentence in every zone.
//
// It is derived from the invitation's OWN two timestamps rather than from
// internal/invite's TTL constant, which is unexported and would be a second copy of
// a number the row already carries.
func expiryPhrase(createdAt, expiresAt time.Time) string {
	d := expiresAt.Sub(createdAt)
	switch days := int(d.Round(24*time.Hour) / (24 * time.Hour)); {
	case d <= 0:
		// Not reachable through invite.IssueAndDeliver (its TTL bounds are one hour to
		// thirty days), and it is not rendered as "0 days" if it ever is: a link that
		// is already dead must not read as a link that lasts no time at all.
		return "an unknown length of time — check with whoever set this up"
	case days <= 1:
		return "less than a day"
	default:
		return strconv.Itoa(days) + " days"
	}
}

// panelLinkSink is invite.LinkSink for ONE request: it takes the link the channel
// produced and hands it to the template.
//
// 🔴 IT IS THE WHOLE OF THE PANEL'S ACCESS TO A RAW CODE, and internal/invite built
// the seam for it: "it exists so that this package does not import the panel and the
// panel does not import a raw-string function from here". The type is unexported and
// has one field with no getter, so the link cannot travel further than the handler
// that made it.
//
// IT DOES NOT LOG, DOES NOT STORE AND DOES NOT COUNT. A retry queue keeping the URL
// would re-create the plain credential the schema goes to some trouble to avoid
// (invite.Delivery's second obligation).
type panelLinkSink struct {
	link  string
	shown bool
}

func (s *panelLinkSink) ShowActivationLink(_ context.Context, d invite.Delivery) error {
	s.link = d.ActivationURL
	s.shown = true
	return nil
}

// employeeDeactivate ends somebody's employment.
//
// 🔴 THE NEXT TAP BECOMES A reject, AND WHAT PRODUCES THAT IS employees.status PLUS
// THE sys:employee-deactivated GUARDRAIL — not a session revocation (user decision,
// 2026-08-07; db/queries/sessions.sql and internal/domain/tenant/staff.go carry the
// argument). §5 row 4 wants that tap REFUSED, RECORDED and alerted on, and the
// recording is the part revocation would put at risk.
//
// 🔴 IT IS IRREVERSIBLE INSIDE THE PRODUCT and the screen says so BEFORE the button
// is pressed, which is the option docs/adr/0009 listed and did not take for reviews.
//
// ✅ THE CONFIRMATION IS ENFORCED ON THE SERVER (user decision, 2026-08-08), AND
// THIS PARAGRAPH HAS NOW BEEN WRONG IN BOTH DIRECTIONS — which is why it says how it
// was measured each time. It first claimed the POST was "only ever reached from a
// page that has already made the statement"; a security audit posted straight at the
// route and got 303 plus a deactivated employee. It was then corrected to say the
// step was a courtesy. Now the gate exists: the confirmation screen mints a SIGNED
// value bound to one employee and one panel session, the form echoes it, and this
// handler refuses anything without it — measured, with the signature, the session
// binding and the expiry each killed by their own mutation.
//
// ⚠️ "one-shot" USED TO BE IN THAT LIST AND IS NOT ANY MORE. Single use is held by
// the CLIENT honouring a cleared cookie, not by this server; the limit and the
// measurement that produced it are at deactivateconfirm.go.
//
// WHAT IT DOES AND DOES NOT BUY. It guarantees the warning was SERVED before the
// write, which is what ADR 0010 needs from it, since nothing in the product brings a
// deactivated person back. It does not prove the warning was READ, and it is not
// CSRF protection — ProtectWriting refuses a cross-origin POST before the resolver
// runs, and this sits behind that.
func (a *AdminAuth) employeeDeactivate(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f, employeeID, ok := a.readAction(w, r)
	if !ok {
		return
	}
	// 🔴 THE CONFIRMATION IS CHECKED BEFORE ANYTHING IS WRITTEN, and a refusal writes
	// NOTHING: no employee change, no audit row, no invitation. §4.6's question about
	// this branch — "what does the manager see, and what is left in the database" — is
	// answered by the redirect below and by zero rows.
	if why := a.confirmationRefusal(r, employeeID); why != "" {
		a.log.Warn("panel deactivation refused: the confirmation step was not completed",
			"employee_id", employeeID, "reason", why)
		a.redirect(w, rosterReturn(f, employeeID, "", why))
		return
	}
	// THE COOKIE IS CLEARED BEFORE THE WRITE IS ATTEMPTED, so an ordinary browser
	// cannot ride the same confirmation twice — and clearing it on the way IN rather
	// than on success means a failed write leaves no reusable value behind either. The
	// cost is that a manager retrying after a database error walks through the
	// sentence again, which is the right direction for this action.
	//
	// ⚠️ IT IS A REQUEST TO THE CLIENT, NOT A LEDGER, and this line claimed otherwise
	// for one round. A client that re-prints the cookie spends the same confirmation
	// until it expires — measured. Counted, not closed: deactivateconfirm.go says why,
	// and what makes it harmless is DeactivateEmployee's own status predicate.
	a.short.clear(w, adminConfirmCookieName)

	_, err := a.staff.Deactivate(r.Context(), tenant.DeactivateCommand{
		TenantID:   id.TenantID(),
		EmployeeID: employeeID,
		// 🔴 THE ACTOR COMES FROM THE SESSION. There is no field on this form for it
		// and there could not be: the id below was resolved from a signed cookie
		// against admin_users, which is the only thing audit_log.actor_id may hold.
		ActorID: id.Admin.AdminUserID,
	})
	switch {
	case err == nil:
		a.redirect(w, rosterReturn(f, employeeID, "deactivated", ""))
	case errors.Is(err, tenant.ErrUnknownEmployee):
		a.log.Warn("panel deactivation refused: no such employee in this tenant")
		a.redirect(w, rosterReturn(f, uuid.Nil, "", "unknown"))
	case errors.Is(err, tenant.ErrAlreadyDeactivated):
		// §4.6: the second press is answered, not swallowed. Nothing was written and
		// no second audit row exists, and the manager is told which of those is true.
		a.log.Info("panel deactivation: the employee was already deactivated",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "already-deactivated"))
	default:
		// A real failure is a real failure. Redirecting it into "already deactivated"
		// would tell a manager the person's taps are being refused when nothing was
		// written — and would hide an outage behind a plausible sentence.
		a.log.Error("panel: could not deactivate the employee", "err", err, "employee_id", employeeID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
	}
}

// employeeMove re-places somebody: a different venue, a different department, or
// both.
//
// THE DESTINATION IS TWO IDS AND NOTHING ELSE. Neither can widen scope — the
// statement joins the venue inside the caller's tenant and the composite foreign key
// refuses a foreign one structurally (00003) — so what the client chooses is which
// of ITS OWN rows to point at.
func (a *AdminAuth) employeeMove(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f, employeeID, ok := a.readAction(w, r)
	if !ok {
		return
	}
	locationID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("to_location")))
	if err != nil {
		// A MOVE WITH NO VENUE IS UNREADABLE, NOT "unknown venue". employees.location_id
		// is NOT NULL, so there is no "nowhere" to move to and a missing field is a
		// broken submission rather than a destination that does not exist.
		a.log.Warn("panel move refused: the posted venue is not a uuid")
		a.redirect(w, rosterReturn(f, employeeID, "", "unreadable"))
		return
	}
	// 🔴 THE DESTINATION FIELDS ARE NAMED to_location AND to_department, NOT location
	// AND department, AND THE RENAME IS LOAD-BEARING. This form also carries the
	// roster position back, and the roster's FILTERS are called `location` and
	// `department`; one set of names for two meanings would make the move form's
	// destination arrive at rosterFilterFrom as a filter, so pressing Save would move
	// somebody AND silently narrow the list they came back to.
	//
	// AN EMPTY DEPARTMENT IS A DESTINATION, NOT A MISSING FIELD (00003: a bar does not
	// model departments), so "" becomes nil rather than an error. Anything that is
	// neither empty nor a uuid IS an error: silently treating it as "no department"
	// would move somebody out of their department because a form field was mangled.
	var departmentID *uuid.UUID
	if raw := strings.TrimSpace(r.PostFormValue("to_department")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			a.log.Warn("panel move refused: the posted department is not a uuid")
			a.redirect(w, rosterReturn(f, employeeID, "", "unreadable"))
			return
		}
		departmentID = &parsed
	}

	_, err = a.staff.Move(r.Context(), tenant.MoveCommand{
		TenantID:     id.TenantID(),
		EmployeeID:   employeeID,
		ActorID:      id.Admin.AdminUserID,
		LocationID:   locationID,
		DepartmentID: departmentID,
	})
	switch {
	case err == nil:
		a.redirect(w, rosterReturn(f, employeeID, "moved", ""))
	case errors.Is(err, tenant.ErrUnknownEmployee):
		a.log.Warn("panel move refused: no such employee in this tenant")
		a.redirect(w, rosterReturn(f, uuid.Nil, "", "unknown"))
	case errors.Is(err, tenant.ErrUnknownPlacement):
		a.log.Warn("panel move refused: the venue or department is not this tenant's",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "unknown-placement"))
	case errors.Is(err, tenant.ErrSamePlacement):
		// §4.6 AGAIN, IN ITS QUIETEST FORM. Nothing was written and no audit row was
		// added, so the screen must not say "saved" — a manager who mis-clicks would
		// otherwise believe they had changed something.
		a.log.Info("panel move: the placement was already the requested one",
			"employee_id", employeeID)
		a.redirect(w, rosterReturn(f, employeeID, "", "no-change"))
	default:
		a.log.Error("panel: could not move the employee", "err", err, "employee_id", employeeID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
	}
}

// readAction is the shared boundary of all three writes: bound the body, parse it,
// read the subject and re-read the roster position the manager was at.
//
// It answers (filter, employeeID, true) or writes a response and answers false.
//
// 🔴 THE BODY IS BOUNDED BEFORE IT IS PARSED. MaxBytesReader makes ParseForm fail on
// an oversized body rather than buffering it, so the ceiling is enforced by the READ
// rather than by a length check afterwards (review.go's shape).
//
// 🔴 THE PARSE ERROR IS CLASSIFIED, NOT PRINTED. net/url's failure QUOTES THE
// OFFENDING INPUT — an audit measured three bytes of a manager's note reaching the
// process log through exactly this branch in M6-04. Nothing a caller sent appears in
// the log line below.
func (a *AdminAuth) readAction(w http.ResponseWriter, r *http.Request) (ledger.RosterFilter, uuid.UUID, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEmployeeActionBody)
	if err := r.ParseForm(); err != nil {
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel employee action refused: the form could not be read",
			"limit_bytes", maxEmployeeActionBody, "reason", reason)
		a.redirect(w, employeesHref+"?problem=unreadable")
		return ledger.RosterFilter{}, uuid.Nil, false
	}
	// THE ROSTER POSITION TRAVELS THROUGH THE SAME VALIDATOR THE URL USES. The hidden
	// fields on the action forms are the filter values the page was showing, and they
	// are re-validated here rather than trusted: rosterFilterFrom is ONE function over
	// url.Values, so the closed vocabularies, the uuid parsing and the name cleaning
	// cannot differ between the GET that rendered the form and the POST that came
	// back. A second copy is how a POST comes to accept what a GET rejects.
	f := rosterFilterFrom(r.PostForm)
	employeeID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("id")))
	if err != nil {
		a.log.Warn("panel employee action refused: the posted employee id is not a uuid")
		a.redirect(w, rosterReturn(f, uuid.Nil, "", "unreadable"))
		return ledger.RosterFilter{}, uuid.Nil, false
	}
	return f, employeeID, true
}

// rosterReturn builds the 303 target: the roster the manager was looking at, the
// person they acted on, and what happened.
//
// 🔴 EVERY PART OF IT IS SERVER-BUILT FROM VALIDATED VALUES. Nothing the client
// posted is echoed into the Location header as text: the filters have been through
// rosterFilterFrom, the ids are uuid.UUIDs, and `done`/`problem` are members of the
// two closed vocabularies above. There is no field a caller could put a URL in, so
// this cannot become an open redirect.
func rosterReturn(f ledger.RosterFilter, manage uuid.UUID, done, problem string) string {
	q := rosterQuery(f)
	if f.AfterID != nil {
		q.Set("after_id", f.AfterID.String())
	}
	if manage != uuid.Nil {
		q.Set("manage", manage.String())
	}
	setIf(q, "done", oneOfWords(done, rosterDoneWords...))
	setIf(q, "problem", oneOfWords(problem, rosterProblemWords...))
	if len(q) == 0 {
		return employeesHref
	}
	return employeesHref + "?" + q.Encode()
}

// --- the deactivation confirmation gate (user decision, 2026-08-08) --------------
//
// The VALUE is internal/handler/deactivateconfirm.go: an HMAC under a key derived
// from the server's own secret, bound to one employee and one panel session, with a
// server-clock expiry. This half is the plumbing: where it is stored, and how a
// refusal reaches the manager.

// adminConfirmCookieName carries the signed value a second time. Its job is to make
// the confirmation single-use FOR A BROWSER THAT HONOURS Set-Cookie — which is not
// the same as making it single-use, and this comment said the second thing for one
// round.
//
// 🔴 THE COOKIE IS NOT WHAT MAKES THE VALUE TRUSTWORTHY. Authenticity comes from the
// MAC; believing otherwise is exactly the defect an audit forged in two lines when
// the first version had no key at all.
//
// 🔴 AND IT DOES NOT MAKE IT ONE-SHOT ON THE SERVER — measured. `a.short.clear` writes
// `Set-Cookie … Max-Age=-1`, which is a REQUEST to the client; nothing here records
// that a confirmation was spent. A client that re-prints the cookie spends one mint
// repeatedly until it expires (three times, in the audit's probe). The limit and why
// it is not closed are written at deactivateconfirm.go; what makes it harmless is
// DeactivateEmployee's `status <> 'deactivated'` predicate, which is asserted rather
// than assumed.
//
// Attributes are adminCookies' (HttpOnly, Path=/admin so it never reaches the tap
// surface, SameSite=Lax, Secure outside dev). Its Max-Age is a browser courtesy; the
// binding expiry is inside the signature.
const adminConfirmCookieName = "tappa_admin_confirm"

// adminConfirmMaxAge is the cookie's hint, kept in step with adminConfirmTTL. The
// SERVER bound is the one in the payload — a browser that ignored this would still be
// refused.
const adminConfirmMaxAge = int(adminConfirmTTL / time.Second)

// confirmField is the hidden input's name.
const confirmField = "confirm_token"

// setDeactivateConfirmation mints the value for ONE employee in THIS session and
// returns what the form must echo.
func (a *AdminAuth) setDeactivateConfirmation(w http.ResponseWriter, r *http.Request, employeeID uuid.UUID) (string, error) {
	// THE SUBJECT IS A STRING SINCE PAYLOAD v3 (M6-06 phase B, so a plaque uid can be
	// one too). An employee is still identified by its uuid; only the encoding widened.
	return a.setConfirmation(w, r, confirmActionDeactivate, employeeID.String())
}

// setConfirmation mints a value for ONE ACTION on ONE SUBJECT in THIS session.
//
// 🔴 THE ACTION IS PART OF THE SIGNATURE (v2). Before it was, a confirmation minted to
// remove a venue passed the deactivation gate cleanly and was stopped only by the
// DOMAIN, because a location id is not an employee id — a binding that held by luck
// rather than by design. Each gate now names the act it is opening.
func (a *AdminAuth) setConfirmation(w http.ResponseWriter, r *http.Request, action, subject string) (string, error) {
	token, err := a.confirm.mint(action, subject, httpx.AdminOf(r).Admin.SessionID)
	if err != nil {
		return "", err
	}
	a.short.set(w, adminConfirmCookieName, token, adminConfirmMaxAge)
	return token, nil
}

// confirmationRefusal names why a deactivation was refused, or "" when it may go
// ahead.
//
// 🔴 THE SIGNATURE IS CHECKED FIRST AND THE COOKIE SECOND, because they answer
// different questions and only the first one is about trust: "did this server mint
// this, for this person, in this session, recently?" then "has it been spent?".
// Checking the cookie first would let a forged value's failure be reported as a
// replay, which is a sentence about the wrong thing.
func (a *AdminAuth) confirmationRefusal(r *http.Request, employeeID uuid.UUID) string {
	return a.confirmationRefusalFor(r, confirmActionDeactivate, employeeID.String())
}

// confirmationRefusalFor is the same check for any gated action.
func (a *AdminAuth) confirmationRefusalFor(r *http.Request, action, subject string) string {
	sent := strings.TrimSpace(r.PostFormValue(confirmField))
	switch err := a.confirm.parse(sent, action, subject, httpx.AdminOf(r).Admin.SessionID); {
	case err == nil:
	case errors.Is(err, errConfirmOtherPerson):
		// 🔴 THE SECOND TAB. One cookie per browser, so opening another subject's
		// warning replaces this one — and the older tab's form now carries a value
		// minted for something else. The signature is genuine, which is why this is a
		// different sentence rather than "not confirmed".
		return "confirm-stale"
	default:
		return "confirm-required"
	}
	// THE COOKIE MUST STILL AGREE WITH THE FORM. For an ordinary browser this is what
	// makes a confirmation single-use, because the response that spent it cleared the
	// cookie. ⚠️ IT IS NOT A SERVER-SIDE CHECK: a client that re-prints the cookie
	// passes here every time, which is measured and counted at deactivateconfirm.go
	// rather than claimed away. What it does hold against every client is that the
	// value belongs to THIS browser — one taken from somebody else's page is refused.
	ck, err := r.Cookie(adminConfirmCookieName)
	if err != nil || ck.Value == "" ||
		subtle.ConstantTimeCompare([]byte(ck.Value), []byte(sent)) != 1 {
		return "confirm-required"
	}
	return ""
}
