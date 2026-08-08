package handler

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The EMPLOYEES SECTION — the roster READ. Its four WRITES are employeeactions.go
// (M6-05 phase B).
//
// ⚠️ THIS BLOCK USED TO SAY "PHASE A: the LIST, and nothing that changes it" AND
// LISTED THE FOUR ACTIONS AS DELIBERATELY ABSENT. They have landed, so the sentence
// is replaced rather than left standing. The split it described was real and paid
// off — a read path and a write path in one commit get each looked at half as hard —
// but what remains true is narrower: THIS FILE still contains no write. It renders
// the list, parses the filters, and builds the action card that the three POST
// handlers in employeeactions.go act on.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), which the
// Protect chain resolved from a signed session cookie against the database. The
// filters a client DOES control — a name, a status, a venue, a department — are
// narrowing predicates INSIDE that tenant: a foreign id matches nothing, because the
// tenant predicate it is ANDed with is not one the client contributed to.
//
// 🔴 §4.7 — NO SECRET PASSES THROUGH THIS FILE. There is nothing to strip by hand:
// ListPanelEmployees does not select token_hash, device_info, session ids or email
// addresses; ledger.Person has no field for them; components.RosterRowView has none
// either. This handler maps the second onto the third and could not carry one if it
// tried.
//
// 🔴 §4.6 — A FAILED READ IS AN ERROR, NEVER AN EMPTY ROSTER. If the database does
// not answer, this renders a problem page. "Nobody works here" is a claim about a
// business, and a timeout is not evidence for it.
//
// 🔴 §4.6 — AND DEACTIVATED PEOPLE ARE LISTED BY DEFAULT. The status filter narrows
// on request and defaults to empty, which the query reads as "every status". This is
// the debt M6-03 handed over: it measured three ways of shortlisting a roster and
// rejected all three because each made somebody who had left unfindable, and it
// pointed at this section as the place the question gets answered. A default of
// "active only" would reopen it here, so there is no such default and
// TestPanelEmployeesDB_ListsPeopleWhoHaveLEFT is what turns red if one appears.

// panelRoster is the slice of internal/domain/ledger the employees section needs.
//
// IT IS PART OF panelLedger RATHER THAN A THIRD FIELD ON AdminAuth, which is a
// departure from how the review queue was wired and is worth the sentence. M6-04
// split panelQueue off because it reads a DIFFERENT PACKAGE (internal/domain/review
// writes; internal/domain/ledger reads), and keeping them apart is what makes "no
// store call in ledger is not a SELECT" a fact grep can check. The roster is the
// same package, the same concrete *ledger.Reader, and the constructor already passes
// that one value twice — a third identical argument in the same position is the
// shape where somebody eventually passes the wrong one.

// employeesHref is the section's own URL, READ FROM THE SECTION TABLE rather than
// written out again. It is what the filter form submits to and what every "next
// page" link is built from, so if the section ever moves they move with it; if the
// tab is removed altogether this panics at startup rather than silently building
// links to nowhere.
var employeesHref = mustSectionHref(pages.TabEmployees)

// panelStatuses is the lifecycle vocabulary as the dropdown renders it, and it is
// DERIVED from ledger.EmployeeStatuses rather than written out again.
//
// 🔴 THE DROPDOWN AND THE VALIDATOR READ THE SAME SLICE, which is the point. Two
// lists — one rendered, one checked — is the shape where a filter is offered that
// the validator silently drops, and the manager then reads an unfiltered page as a
// filtered one. Here the option cannot be offered unless it is accepted, and cannot
// be accepted unless it is offered. The values are migration 00003's CHECK
// vocabulary; the labels are the same words, capitalised, for the reason
// components.lifecycleTally gives.
var panelStatuses = statusOptions()

func statusOptions() []components.OptionView {
	out := make([]components.OptionView, 0, len(ledger.EmployeeStatuses))
	for _, s := range ledger.EmployeeStatuses {
		out = append(out, components.OptionView{
			Value: s,
			Label: strings.ToUpper(s[:1]) + s[1:],
		})
	}
	return out
}

// employeesSection renders the roster: chrome, filter bar, one page of people.
//
// 🔴 IT LOADS NO SCRIPT, AND PAGING IS A WHOLE-DOCUMENT LINK. The alternative was
// the transactions section's shape — vendored HTMX, a fragment route, hx-get on the
// control — and it was rejected on a cost that is written down rather than felt:
// the panel's widened Content-Security-Policy is pinned to EXACTLY ONE url by
// TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty, and that pin is the
// only thing standing between "one screen needs a script" and "the panel allows
// scripts". Widening it to two is allowed — the test says so — but it has to buy
// something, and here it would buy very little: a roster page is browsed, not
// appended to, so replacing the list is the behaviour a reader wants anyway, and
// the REQUEST cost is identical either way (one request per page, fragment or
// document). What a document costs over a fragment is BYTES.
//
// 🔴 EVERY NUMBER IN THE TABLE BELOW WAS PRODUCED BY THE COMMAND PRINTED WITH IT,
// AND THE REASON THAT IS SPELLED OUT IS THAT AN EARLIER VERSION OF THIS COMMENT
// CARRIED THREE FIGURES NOBODY HAD MEASURED (30 336 / 22 890 / 7 446 — invented,
// withdrawn). The probe renders the REAL handler against the development database's
// largest tenant, varying only ledger.RosterPageSize:
//
//	GET /admin/employees as the largest tenant's owner; len(body), and the sum of
//	len() over every <article class="docket"> on it. Re-runnable: temporarily set
//	RosterPageSize, run the section handler through the real router with a reader
//	built on DATABASE_URL, and subtract.
//
//	page size  document      cards      chrome
//	       25    22 028 B   18 036 B    3 992 B
//	       50    40 103 B   36 111 B    3 992 B   <- SHIPPED
//	      100    76 253 B   72 261 B    3 992 B
//	      500   365 471 B  361 478 B    3 993 B
//	  the WHOLE roster on one page: 6 316 252 B (6.3 MB)
//
// 🔴 THERE IS NO "PAGE-TURNS TO WALK THE ROSTER" COLUMN HERE ANY MORE, AND ITS
// ABSENCE IS THE FIX. It used to be the fifth column, and it was the one quantity in
// this table that DRIFTS: turns are ceil(E / pageSize) for the largest roster, and E
// grows every time `make test` runs — the simulated-day fixture hires into that
// tenant (seedflow_db_test.go's insertEmployee, against fixtures.TenantKF) and
// migration 00003 REVOKEs DELETE on employees, so nothing removes them. Three files
// ended up carrying three different values for it within a day. It now lives in ONE
// place, beside the ceiling it constrains: internal/handler/adminratelimit.go, with
// the query that produces it and the drift warning. The byte columns stay here
// because they do NOT drift — a page holds pageSize rows whatever the payroll is.
//
// So the fragment-versus-document trade is ~4.0 KB per page turn against a second
// scripted url and a second CSP. Somebody who disagrees can reverse it knowingly: it
// means naming PanelShellWithScript here, adding a fragment route, and arguing the
// cardinality up to 2 in that test — deliberately, which is the bargain M6-02 struck.
//
// THE BOTTOM ROW is what "search only, no paging" costs if it renders everybody:
// 7.3x the 867 233 B control M6-03 measured and removed, on a page that is
// Cache-Control: no-store and therefore re-sent on every view. It is why this
// section paginates at all.
func (a *AdminAuth) employeesSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f := parseRosterFilter(r)

	screen, err := a.ledger.Roster(r.Context(), id.TenantID(), f)
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to an empty roster.
		a.log.Error("panel: could not read the employee roster", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	zone := screen.Zone
	if zone == nil {
		zone = time.UTC
	}

	// THE START OF THE LIST IS THIS VIEW WITH ITS FILTERS AND NO CURSOR. It is built
	// unconditionally because it is cheap and because building it only in the empty
	// branch would put a second copy of the query-string logic there.
	start := employeesHref
	if q := rosterQuery(f); len(q) > 0 {
		start += "?" + q.Encode()
	}

	v := pages.EmployeesView{
		PanelChrome: a.chrome(r, pages.TabEmployees),
		Queried:     screen.Queried,
		ZoneLabel:   zone.String(),
		// Paged gates the empty state's third branch — see EmployeesView.Paged for
		// the sentence it stops the product from printing.
		Paged:     f.AfterID != nil,
		StartHref: start,
		// Problem is a CLOSED VOCABULARY read off the query string, so a hand-edited
		// URL can turn one of six sentences on and cannot put anything in it. Every
		// one of the six is a statement about the REQUEST or about a row the server
		// read — none of them describes something that was not looked up.
		Problem: oneOfWords(r.URL.Query().Get("problem"), rosterProblemWords...),
		Filters: components.RosterFilterView{
			Action:       employeesHref,
			Name:         f.Name,
			Status:       f.Status,
			LocationID:   uuidParam(f.LocationID),
			DepartmentID: uuidParam(f.DepartmentID),
			Locations:    optionViews(screen.Options.Locations),
			Departments:  optionViews(screen.Options.Departments),
			Statuses:     panelStatuses,
			Narrowed:     f.Narrowed(),
		},
	}
	v.People = make([]components.RosterRowView, 0, len(screen.People))
	for _, p := range screen.People {
		v.People = append(v.People, rosterRowView(p, zone, manageHref(f, p.ID)))
	}
	if screen.NextID != nil {
		q := rosterQuery(f)
		q.Set("after_id", screen.NextID.String())
		v.MoreHref = employeesHref + "?" + q.Encode()
	}

	// THE ACTION CARD IS PART OF THIS SECTION RATHER THAN A ROUTE OF ITS OWN, and the
	// cost of the alternative is what decided it. Putting the four controls on every
	// row would repeat the venue and department dropdowns RosterPageSize times: the
	// filter bar renders each option list once, and a per-row copy multiplies exactly
	// the control M6-03 measured at 96% of a page and removed. A page-level card costs
	// one option list and one extra charged request when a manager opens it.
	v.Actions, v.Problem = a.rosterActions(w, r, f, screen, v.Problem)
	v.Done = a.confirmedRosterAction(r, v.Actions)

	a.render(w, r, http.StatusOK, pages.AdminEmployees(v))
}

// rosterActions builds the card for the person named by ?manage=, or nothing.
//
// 🔴 A MANAGE LINK THAT RESOLVES TO NOBODY IS ANSWERED, NOT IGNORED (§4.6). A stale
// bookmark, a hand-edited URL or another tenant's employee id all land here, and the
// difference between "no card" and "no card plus a sentence" is whether the manager
// knows why the screen did not do what they asked. The problem word is only set when
// the request did not already carry one, so the outcome of an ACTION is never
// overwritten by the state of the card it returned to.
func (a *AdminAuth) rosterActions(w http.ResponseWriter, r *http.Request, f ledger.RosterFilter, screen ledger.RosterScreen, problem string) (*components.RosterActionsView, string) {
	raw := r.URL.Query().Get("manage")
	if raw == "" {
		return nil, problem
	}
	employeeID, err := uuid.Parse(raw)
	if err != nil {
		return nil, firstWord(problem, "unknown")
	}
	person, err := a.staff.Person(r.Context(), httpx.AdminOf(r).TenantID(), employeeID)
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrUnknownEmployee):
		return nil, firstWord(problem, "unknown")
	default:
		// The roster itself answered, so the page is still worth rendering; what is not
		// acceptable is a card missing with no explanation. The read failed, and the
		// screen says so IN ITS OWN WORDS — "unreadable" belongs to a request nobody
		// could parse, and an audit found this branch borrowing it, which told a manager
		// their submission was malformed when the failure was ours.
		a.log.Error("panel: could not read the employee for the action card", "err", err)
		return nil, firstWord(problem, "actions-unavailable")
	}

	hidden := []components.FormField{{Name: "id", Value: person.ID.String()}}
	for key, values := range rosterQuery(f) {
		for _, value := range values {
			hidden = append(hidden, components.FormField{Name: key, Value: value})
		}
	}
	if f.AfterID != nil {
		hidden = append(hidden, components.FormField{Name: "after_id", Value: f.AfterID.String()})
	}
	sort.Slice(hidden, func(i, j int) bool { return hidden[i].Name < hidden[j].Name })

	// 🔴 THE CONFIRMATION VALUE IS MINTED HERE, WHEN THE SENTENCE IS RENDERED, AND
	// NOWHERE ELSE. That is what makes the two-step a gate rather than a screen: the
	// deactivate POST cannot succeed without a value that only this branch produces,
	// so reaching the write means the warning was served (user decision, 2026-08-08 —
	// before it, posting straight at the route deactivated somebody).
	//
	// A FAILURE TO MINT IS NOT A SILENT ONE. If the random source fails the card still
	// renders, with the deactivate control suppressed and the problem word set — a
	// screen offering a button that cannot work is the mistake this section is
	// watched for.
	confirming := r.URL.Query().Get("confirm") == "deactivate"
	confirmToken := ""
	if confirming && !person.Deactivated() {
		token, err := a.setDeactivateConfirmation(w, r, person.ID)
		if err != nil {
			a.log.Error("panel: could not mint the deactivation confirmation", "err", err)
			confirming = false
			problem = firstWord(problem, "actions-unavailable")
		} else {
			confirmToken = token
		}
	}

	v := &components.RosterActionsView{
		Name:             person.Name,
		Status:           person.Status,
		Venue:            person.LocationName,
		Department:       person.DepartmentName,
		Hidden:           hidden,
		CloseHref:        rosterReturn(f, uuid.Nil, "", ""),
		CanInvite:        person.Invitable(),
		InviteLabel:      inviteLabel(person.Status),
		InviteAction:     employeeInviteHref,
		CanDeactivate:    !person.Deactivated(),
		Confirming:       confirming,
		ConfirmToken:     confirmToken,
		ConfirmField:     confirmField,
		ConfirmHref:      confirmDeactivateHref(f, person.ID),
		DeactivateAction: employeeDeactivateHref,
		MoveAction:       employeeMoveHref,
		Locations:        optionViews(screen.Options.Locations),
		Departments:      optionViews(screen.Options.Departments),
		LocationID:       person.LocationID.String(),
		DepartmentID:     uuidParam(person.DepartmentID),
	}
	return v, problem
}

// confirmedRosterAction decides whether the "done" banner may be shown, HAVING
// CHECKED WHAT IT CAN CHECK.
//
// 🔴 M6-04 SHIPPED A BANNER PRINTED FROM THE QUERY STRING ALONE and an audit
// measured what that meant: a URL that claimed a decision had been recorded with
// zero rows in the database, and it was sendable to somebody else. The rule this
// screen follows instead:
//
//	deactivated  VERIFIED. The banner appears only if the person the card just read
//	             back from the database is in fact deactivated, so a hand-edited or
//	             stale URL says nothing.
//	moved        NOT verifiable, and the sentence is built so it does not need to be.
//	             "Moved from X to Y" would be a claim about a previous state nothing
//	             re-reads; what the screen prints is where the person works NOW, read
//	             in the same request. A replayed URL therefore repeats a true
//	             sentence about the current placement rather than inventing a change.
//
// Either way the banner is gated on the card existing, so it can never be shown
// about somebody the reader cannot see.
func (a *AdminAuth) confirmedRosterAction(r *http.Request, actions *components.RosterActionsView) string {
	done := oneOfWords(r.URL.Query().Get("done"), rosterDoneWords...)
	if done == "" || actions == nil {
		return ""
	}
	if done == "deactivated" && actions.Status != ledger.StatusDeactivated {
		return ""
	}
	return done
}

// inviteLabel names the button.
//
// 🔴 THE TWO WORDS ARE THE CARD'S TWO ACTIONS ("davet et, yeniden davet") AND THEY
// ARE PICKED BY STATUS RATHER THAN BY WHETHER AN INVITATION IS OUTSTANDING. Knowing
// that would mean joining employee_invites into the roster read, which phase A
// deliberately did not do — the invitation table holds the code hash, and a list
// that touched it would be one edit away from rendering one. The status is enough to
// keep the label honest: somebody still 'invited' has never activated, so the button
// sends them their first (or another) link; somebody 'active' already has a phone
// signed in, so a new link is a re-invitation for a new device.
//
// ⚠️ WHAT THE LABEL DOES NOT SAY, AND THIS PARAGRAPH HAS BEEN REWRITTEN TWICE
// BECAUSE THE PRODUCT CHANGED UNDER IT. It first said pressing "Send invite" twice
// mints two links "both valid until they expire OR ONE IS USED" — false, and measured
// false end to end: spending one invitation left its siblings live, and opening a
// stale one later took the second-device path, which signs the real employee out. It
// then said so plainly, as an open risk.
//
// ✅ SINCE 2026-08-08 THE FIRST READING IS TRUE FOR REAL (user decision): issuing
// retires the employee's pending invitations in the same transaction (migration
// 00012's cancelled_at, invite.Manager.IssueAndDeliver), so at most ONE link is
// spendable at a time and the button below cannot multiply a live credential.
// TestPanelEmployeesDB_ASecondInvitationRETIRESTheFirst holds it end to end; the
// statement's own guard is held separately in internal/db, because a mutation showed
// the HTTP test could not see it (Lookup refuses first).
//
// WHAT IS STILL TRUE ABOUT THE LABEL: it does not say whether an invitation is
// currently outstanding, because that would need the employee_invites join phase A
// kept off this page. The consequence is now cosmetic rather than dangerous — a
// second press replaces rather than adds — and the invitation screen says how many
// links it retired.
func inviteLabel(status string) string {
	if status == ledger.StatusActive {
		return "Re-invite"
	}
	return "Send invite"
}

// manageHref is the row's link to its own action card, keeping the filters and the
// page the manager is on so that closing the card returns them to it.
func manageHref(f ledger.RosterFilter, employeeID uuid.UUID) string {
	return rosterReturn(f, employeeID, "", "")
}

// confirmDeactivateHref is the GET that asks "are you sure", carrying the same
// position. It is a LINK rather than a script confirm: this section loads no script,
// and a browser dialog is not something a server can require.
func confirmDeactivateHref(f ledger.RosterFilter, employeeID uuid.UUID) string {
	to := rosterReturn(f, employeeID, "", "")
	if strings.Contains(to, "?") {
		return to + "&confirm=deactivate"
	}
	return to + "?confirm=deactivate"
}

// firstWord keeps the FIRST problem rather than the last: the outcome of the action
// a manager just took outranks anything the page discovered while rendering itself.
func firstWord(existing, fallback string) string {
	if existing != "" {
		return existing
	}
	return fallback
}

// rosterRowView maps one person onto one card.
func rosterRowView(p ledger.Person, zone *time.Location, manage string) components.RosterRowView {
	return components.RosterRowView{
		Name:       p.Name,
		Venue:      p.LocationName,
		Department: p.DepartmentName,
		Status:     p.Status,
		Since:      rosterDate(p.Since, zone),
		Devices:    deviceLine(p.LiveDevices, p.LastUsed, zone),
		ManageHref: manage,
	}
}

// rosterDate formats a lifecycle stamp in the tenant's zone, or "" when there is
// none. THE DAY IS PART OF IT AND THE SECONDS ARE NOT: an employment date is read
// as a day, and printing 14:03:22 beside "invited" would invite a precision the
// question does not have.
func rosterDate(t *time.Time, zone *time.Location) string {
	if t == nil {
		return ""
	}
	return t.In(zone).Format("2 Jan 2006")
}

// deviceLine is the one sentence a row says about signed-in phones.
//
// 🔴 THERE ARE FOUR OUTCOMES AND ONLY THREE ARE OBVIOUS. The trap is the middle
// pair: a person with NO live session and a person whose phone was activated a
// minute ago and never used both have a nil timestamp, and a template joining "N
// devices" to a date field would render them the same way. migration 00003 makes
// last_used_at nullable precisely because it is stamped on USE, so "signed in, not
// used yet" is an ordinary state on the day somebody activates — the day a manager
// is most likely to be looking at this screen.
//
// NOTHING HERE NAMES A DEVICE. The count and the timestamp are the whole of what
// the query selects about sessions; the coarse label the schema does carry
// (device_info) is not selected, because the card asks whether an active device
// EXISTS and when it was last used, and a column with no reader is what weakens the
// argument that unselected columns cannot reach a screen.
func deviceLine(n int, lastUsed *time.Time, zone *time.Location) string {
	if n <= 0 {
		return "None signed in"
	}
	head := "1 signed in"
	if n > 1 {
		head = strconv.Itoa(n) + " signed in"
	}
	if lastUsed == nil {
		return head + " · not used yet"
	}
	return head + " · last used " + lastUsed.In(zone).Format("2 Jan 15:04")
}

// rosterDesignCeiling is the largest payroll this panel PROMISES to let a manager
// browse from A to Z inside one session's request budget.
//
// 🔴 IT EXISTS SO THE PAGE-SIZE DECISION IS AN INVARIANT RATHER THAN A MEMORY.
// Walking a roster costs ceil(E / ledger.RosterPageSize) charged requests and
// adminSessionLimit is what a session may spend, so the largest walkable roster is
// exactly RosterPageSize x adminSessionLimit — 50 x 300 = 15 000 today. Dropping the
// page size back to 25 would silently halve that to 7 500 and put the development
// database's own largest tenant beyond reach again, which is the failure the user
// decision of 2026-08-07 was taken to prevent.
// TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget is the assertion, and
// it is measured going red at 25 rather than assumed to.
//
// WHY A ROUND PROMISE RATHER THAN THE MEASURED FIGURE — AND WHY THE MEASURED FIGURE
// IS NOT WRITTEN HERE. A ceiling pinned to the development database's largest tenant
// would be pinned to a moving target: the simulated-day fixture hires into that
// tenant on every `make test` and employees cannot be deleted (00003 REVOKEs
// DELETE), so the headcount only climbs, and a test anchored to it would go red on
// its own schedule rather than on a defect. Quoting it in this comment is no better,
// which three consecutive audits demonstrated by finding it stale each time; the
// query that answers it lives in adminratelimit.go and
// TestComments_DoNotQuoteTheDriftingRosterSize keeps it from spreading back.
//
// 10 000 is a round number chosen ABOVE that drift with room to spare, and above any
// hospitality or light-manufacturing payroll in the two markets this ships to. It is
// a PROMISE, not a limit on the data: a bigger roster still lists, still filters and
// still pages — what it loses is the guarantee that one session can walk the whole of
// it without waiting out a rate-limit window.
const rosterDesignCeiling = 10000

// parseRosterFilter reads the four filters and the paging cursor from the query
// string.
//
// 🔴 EVERY VALUE IS VALIDATED HERE, AT THE BOUNDARY (§7), so the domain sees data
// that is already good. Anything unrecognised is DROPPED rather than refused, and
// that is a deliberate choice with a cost: a stale bookmark or a hand-edited URL
// shows the unfiltered roster instead of an error page. It is safe because dropping
// can only WIDEN the result within the tenant, never escape it — and it is visible
// because the filter bar echoes the values that actually took effect.
func parseRosterFilter(r *http.Request) ledger.RosterFilter {
	return rosterFilterFrom(r.URL.Query())
}

// rosterFilterFrom is that validator over ANY set of values, and it is one function
// rather than two on purpose (M6-05 phase B).
//
// 🔴 THE POSTED ACTION FORMS CARRY THE SAME FIELDS AND MUST NOT BE VALIDATED
// DIFFERENTLY. Every action form on this screen echoes the roster position back so
// the manager returns to the page they were on, which means the same four filters
// and the same cursor arrive in a POST body. A second parser for the POST side is
// the "second representation" shape this repository keeps paying for: it is how a
// POST comes to accept a status the GET rejects, or to skip the NUL-byte cleaning
// that makes a name safe for the driver. There is one function and both callers use
// it.
func rosterFilterFrom(q url.Values) ledger.RosterFilter {
	var f ledger.RosterFilter

	// THE NAME BOX SHARES ITS VALIDATOR WITH THE TRANSACTIONS SECTION'S, and it is
	// the same function rather than a copy of it. Both boxes send a name fragment to
	// the same ILIKE against the same column, and both had to learn the same two
	// lessons the hard way: a NUL byte is a 500 from the driver, and slicing by BYTE
	// cuts a Maltese ħ or ż in half and is another one. A second copy would be a
	// second place to relearn them.
	f.Name = employeeNameFilter(q.Get("name"))
	f.Status = oneOf(q.Get("status"), panelStatuses)
	f.LocationID = optionalUUID(q.Get("location"))
	f.DepartmentID = optionalUUID(q.Get("department"))

	// 🔴 THE CURSOR IS AN ID AND NOTHING ELSE, AND THE NAME HALF USED TO BE HERE.
	// It was bounded, because a query string is no place for an unbounded value, and
	// a name past the bound was dropped — which does not shrink a page, it repeats
	// one. A security audit measured the consequence on real Postgres: a 605-rune
	// name on a page boundary served page one forever and everybody behind it became
	// unreachable by browsing.
	//
	// TWO SENTENCES THAT USED TO BE IN THIS COMMENT WERE FALSIFIED BY THAT
	// MEASUREMENT, and they are worth recording because both sounded reasonable:
	//
	//	"dropping can only ever WIDEN the result, never escape it"
	//	    it did not widen. Dropping the cursor showed LESS of the roster, because
	//	    the page it dropped back to was one the reader had already seen.
	//	"it is visible because the filter bar echoes the values that took effect"
	//	    the filter bar echoes the FILTERS. The cursor is echoed nowhere, so the
	//	    drop was completely silent.
	//
	// What remains true is the general rule, for the FILTERS: anything unrecognised
	// is dropped rather than refused, and for a filter that genuinely can only widen.
	// The cursor is not a filter and never was, which is why it needed a different
	// answer rather than a bigger bound.
	if id, err := uuid.Parse(q.Get("after_id")); err == nil {
		f.AfterID = &id
	}
	return f
}

// rosterQuery rebuilds the query string for a link that keeps the current filters.
// The cursor is NOT included — the caller adds its own.
func rosterQuery(f ledger.RosterFilter) url.Values {
	q := url.Values{}
	setIf(q, "name", f.Name)
	setIf(q, "status", f.Status)
	setIf(q, "location", uuidParam(f.LocationID))
	setIf(q, "department", uuidParam(f.DepartmentID))
	return q
}
