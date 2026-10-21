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

// The EMPLOYEES SECTION — the roster READ. Its WRITES are employeeactions.go
// (M6-05 phase B, M6-13) and employeeemail.go (M10 EM-6).
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
// ⚠️ THE ACTION CARD IS THE EXCEPTION FOR THE ADDRESS, SINCE M10 EM-6, and only the
// card. rosterActions reads the ONE person's address on file through its own query
// (GetEmployeeEmail) and puts it in RosterActionsView.Email, because changing an
// address starts from seeing it. A row of the list still has no address; an address
// is personal data rather than a credential, and it is never logged (R7b).
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
	v, ok := a.employeesView(w, r, nil)
	if !ok {
		return
	}
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminEmployees(v))
}

// employeesView builds the whole section. It answers (view, true), or writes a
// response itself and answers false.
//
// 🔴 IT IS A SEPARATE FUNCTION FROM THE ROUTE BECAUSE A REJECTED ADD RE-RENDERS THIS
// PAGE RATHER THAN REDIRECTING TO IT (M6-13), and the two must be the SAME page. The
// alternative — a second view builder for the failure path — is the "second
// representation" shape this repository has paid for repeatedly: the copy drifts, and
// the version a manager sees after a mistake is the one nobody looks at.
//
// `rejected` is the add form as it was posted, or nil. When it is set the form is
// forced open with the manager's own typing in it, so a correction costs one field
// rather than five.
func (a *AdminAuth) employeesView(w http.ResponseWriter, r *http.Request, rejected *components.StaffFormView) (pages.EmployeesView, bool) {
	id := httpx.AdminOf(r)
	// 🔴 A REJECTED FORM CARRIES ITS POSITION IN THE BODY, NOT IN THE URL, because the
	// request that reaches here is the POST to the add route — its query string is
	// empty. Reading parseRosterFilter on that path would silently drop the manager's
	// filters and page, so the form would come back correct and the list underneath it
	// would be somebody else's view. Both readers are the SAME validator over
	// url.Values, which is what keeps a POST from accepting what a GET rejects.
	f := parseRosterFilter(r)
	if rejected != nil {
		f = rosterFilterFrom(r.PostForm)
	}

	screen, err := a.ledger.Roster(r.Context(), id.TenantID(), f)
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to an empty roster.
		a.log.Error("panel: could not read the employee roster", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return pages.EmployeesView{}, false
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

	// THE CLOCK THE LAST TAPS ARE READ AGAINST ("today", "yesterday"): one instant per
	// request, so two rows cannot disagree about which day it is.
	now := time.Now()
	chrome := a.chrome(r, pages.TabEmployees)
	chrome.Kicker = peopleKicker(screen.Tally)

	v := pages.EmployeesView{
		PanelChrome: chrome,
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
			Chips:        rosterChips(f, screen.Tally),
		},
	}
	// THE OPEN CARD'S ROW SAYS IT IS THE OPEN ONE. A manage id that does not parse
	// marks nothing, and one of another business matches no row on this page.
	managed, _ := uuid.Parse(r.URL.Query().Get("manage"))
	v.People = make([]components.RosterRowView, 0, len(screen.People))
	for _, p := range screen.People {
		row := rosterRowView(p, zone, now, screen.LastTaps, manageHref(f, p.ID))
		row.Selected = managed != uuid.Nil && p.ID == managed
		v.People = append(v.People, row)
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
	v.Actions, v.Problem = a.rosterActions(w, r, f, screen, v.Problem, now)
	v.Done = a.confirmedRosterAction(r, v.Actions)
	v.Add = a.rosterAdd(r, f, screen, rejected)
	// THE ADD FORM IS ITS OWN LAYOUT ON THE SAME URL (M12-31): a trail back to the list,
	// the eyebrow and the title "New person". Nothing else about the request changes.
	if v.Add.Possible && v.Add.Open {
		v.PanelChrome.Crumbs = []pages.Crumb{
			{Label: pages.SectionLabel(pages.TabEmployees), Href: v.Add.Form.CloseHref},
			{Label: "Add somebody"},
		}
		v.PanelChrome.Kicker = "Add somebody · they activate with their first tap"
		v.PanelChrome.Title = "New person"
	}

	return v, true
}

// addParam is the disclosure that opens the add form, and addOpenValue is the one
// value it responds to.
//
// IT IS A QUERY PARAMETER RATHER THAN A ROUTE OF ITS OWN, which is the locations
// section's shape (?venue=new) and for the same reason: the form needs the roster's
// filters and cursor beside it so Cancel and the eventual save return to the list the
// manager came from. A separate route would have to carry all of that anyway.
const (
	addParam     = "add"
	addOpenValue = "new"
)

// rosterAdd builds the add control, the form, or the sentence that replaces both.
//
// 🔴 Possible IS COMPUTED FROM THE BUSINESS'S OWN VENUES AND IS THE WHOLE OF THE
// EMPTY PATH. The panel always places the people it adds (employees.location_id is NULL
// only for an API-managed row, K-14), so a business with no venue cannot add anybody
// here — and the option list this form would render is exactly the
// list that is empty in that state. Offering the control anyway would offer a
// submission the server can only refuse, which is the defect this section carries two
// tests against. The locations section makes the identical call one table over.
//
// ⚠️ THE OPTIONS COME FROM THE ROSTER READ THAT ALREADY HAPPENED, not from a second
// query. The filter bar renders the same two lists, so the add form costs no extra
// round trip — the cost M6-03 measured and removed was rendering option lists PER ROW,
// not rendering them twice on one page.
func (a *AdminAuth) rosterAdd(r *http.Request, f ledger.RosterFilter, screen ledger.RosterScreen, rejected *components.StaffFormView) components.RosterAddView {
	locations := optionViews(screen.Options.Locations)

	q := rosterQuery(f)
	if f.AfterID != nil {
		q.Set("after_id", f.AfterID.String())
	}
	closeHref := employeesHref
	if len(q) > 0 {
		closeHref = employeesHref + "?" + q.Encode()
	}
	// THE OPEN LINK IS BUILT FROM ITS OWN QUERY RATHER THAN FROM `q`.
	//
	// ⚠️ IT USED TO COPY `q` INTO A NEW map AND THE COMMENT EXPLAINED WHY THAT COPY
	// MATTERED -- that aliasing would make Set below mutate the values closeHref is
	// built from. An audit measured the honest version of that: the copy was
	// behaviour-equivalent, because closeHref is computed ABOVE, so nothing could tell
	// the two apart and no test could pin it. A described hazard with no test is what
	// this file's own standard calls a defect waiting for the comment to be deleted as
	// noise.
	//
	// Asking rosterQuery for a fresh value removes the hazard instead of documenting
	// it: there is no shared map, so there is no ordering to get right and nothing to
	// pin. It is also what the hidden fields below already do -- the previous comment
	// claimed they were built from `q`, which was wrong.
	open := rosterQuery(f)
	if f.AfterID != nil {
		open.Set("after_id", f.AfterID.String())
	}
	open.Set(addParam, addOpenValue)

	v := components.RosterAddView{
		Href:       employeesHref + "?" + open.Encode(),
		Open:       r.URL.Query().Get(addParam) == addOpenValue || rejected != nil,
		Possible:   len(locations) > 0,
		VenuesHref: locationsHref,
	}
	if !v.Possible {
		// 🔴 THE REFUSAL SURVIVES EVEN THOUGH THE FORM DOES NOT (§4.6). A business with
		// no venue still reaches the add route — by a stale tab, a replayed POST, or a
		// venue removed since the page rendered — and the domain refuses it. Before
		// this line the message was attached to a form this branch does not render, so
		// it was dropped: the manager got 200, a page about venues, and no word that
		// anything had been refused. An audit measured exactly that.
		if rejected != nil {
			v.Refusal = rejected.ErrorMessage
		}
		return v
	}
	if !v.Open {
		return v
	}

	// THE POSITION TRAVELS WITH THE FORM. Same mechanism as the action card's forms:
	// the roster's filters and cursor are posted back as hidden fields, re-validated
	// on the way in by the same rosterFilterFrom the URL uses, so a save returns to
	// the list the manager was actually looking at.
	hidden := make([]components.FormField, 0, len(q))
	for key, values := range rosterQuery(f) {
		for _, value := range values {
			hidden = append(hidden, components.FormField{Name: key, Value: value})
		}
	}
	if f.AfterID != nil {
		hidden = append(hidden, components.FormField{Name: "after_id", Value: f.AfterID.String()})
	}
	sort.Slice(hidden, func(i, j int) bool { return hidden[i].Name < hidden[j].Name })

	form := components.StaffFormView{}
	if rejected != nil {
		form = *rejected
	}
	form.Action = employeeAddHref
	form.Hidden = hidden
	form.CloseHref = closeHref
	form.Locations = locations
	form.Departments = optionViews(screen.Options.Departments)
	form.BillingNote = addBillingNote
	form.InviteByEmail = a.inviteMail != nil
	if form.LocationID == "" && len(locations) == 1 {
		// ONE VENUE IS THE COMMON CASE AND PRE-SELECTING IT REMOVES A DECISION THAT HAS
		// ONLY ONE ANSWER.
		//
		// 🔴 WITH MORE THAN ONE IT IS LEFT UNSET, AND THAT ONLY MEANS SOMETHING BECAUSE
		// THE TEMPLATE OFFERS AN EMPTY PLACEHOLDER. This comment used to claim that
		// leaving it unset stopped a manager being defaulted into a venue; an audit
		// measured that it did not, because HTML submits a select's FIRST option when
		// none is marked selected. Omitting `selected` changes nothing about what is
		// sent. The placeholder is the mechanism; this line is only the convenience.
		form.LocationID = locations[0].Value
	}
	v.Form = form
	return v
}

// The two things this screen can truthfully say about the bill, and the CLAIM PHRASE
// inside each that decides which one it is.
//
// 🔴 THERE ARE TWO OF THEM BECAUSE ONE OF THEM WAS UNFALSIFIABLE. This used to be a
// single constant asserted with `strings.Contains(body, addBillingNote)` — a
// TAUTOLOGY: any text at all satisfies it, including the exact opposite of the truth.
// An audit measured it by rewriting the sentence to "Adding somebody puts them on your
// bill immediately, at the full monthly rate." and the whole suite stayed GREEN,
// including a test called ...TheBillingNoteMatchesThePredicate. The test was measuring
// the PREDICATE and never comparing the SENTENCE to it, which is the difference
// between a claim a command settles and a claim somebody defends.
//
// 🔴 WHAT MAKES IT FALSIFIABLE NOW: the DATABASE picks which sentence is correct.
// TestEmployeeAddDB_TheBillingNoteMatchesThePredicate asks
// tappa_employee_is_billable about a row it just created through the panel, and then
// requires the page to carry the matching CLAIM PHRASE and NOT the opposing one. The
// phrases it looks for are declared in the TEST, deliberately not imported from here —
// so rewriting either sentence, or swapping which one is used, turns it red.
//
// WHY THE ANSWER IS "not yet" TODAY (measured 2026-08-17 against this schema, and
// re-measured by that test on every run):
//
//	tappa_employee_lifecycle_status(NULL, NULL)                 -> 'invited'
//	tappa_employee_is_billable('invited', NULL, NULL, from, to) -> false
//
// migration 0016's predicate requires `p_activated_at IS NOT NULL`, and somebody added
// here is born 'invited' with that column NULL. The first line matters as much as the
// second: it is what keeps a freshly added row OUT of `unstamped_employees`, the count
// the billing screen surfaces as an anomaly.
//
// ⚠️ IF THE PREDICATE EVER STARTS BILLING ON CREATION, the repair is to point
// addBillingNote at the other constant — not to edit the words of either. The test
// will name which one it expected.
const (
	billingNoteNotYetBillable = "Adding somebody does not change your bill on its own — " +
		"people start counting towards it when they activate, not when they are added."
	billingNoteBillableOnAdd = "Adding somebody starts charging for them straight away, " +
		"at your plan's monthly rate — they are counted from the moment they are added."
)

// addBillingNote is the one this product currently ships, chosen by the measurement
// above. It is a var rather than a const so that the test can state the alternative
// without the compiler folding the two into one string.
var addBillingNote = billingNoteNotYetBillable

// rosterActions builds the card for the person named by ?manage=, or nothing.
//
// 🔴 A MANAGE LINK THAT RESOLVES TO NOBODY IS ANSWERED, NOT IGNORED (§4.6). A stale
// bookmark, a hand-edited URL or another tenant's employee id all land here, and the
// difference between "no card" and "no card plus a sentence" is whether the manager
// knows why the screen did not do what they asked. The problem word is only set when
// the request did not already carry one, so the outcome of an ACTION is never
// overwritten by the state of the card it returned to.
func (a *AdminAuth) rosterActions(w http.ResponseWriter, r *http.Request, f ledger.RosterFilter, screen ledger.RosterScreen, problem string, now time.Time) (*components.RosterActionsView, string) {
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
	// THE ADDRESS ON FILE (M10 EM-6) — a second read, for the reason GetEmployeeEmail
	// gives: tenant.Person has no address field and keeps it that way. Its failure is
	// answered like the person's: no card and a sentence, never a card that shows "no
	// address" because a read failed — that would invite an owner to type one over an
	// address that is in fact on file.
	email, err := a.staff.Email(r.Context(), httpx.AdminOf(r).TenantID(), employeeID)
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrUnknownEmployee):
		return nil, firstWord(problem, "unknown")
	default:
		a.log.Error("panel: could not read the employee's address for the action card", "err", err)
		return nil, firstWord(problem, "actions-unavailable")
	}

	// THE SUMMARY AND THE STRIP (M12-20, M12-19) ARE READ AFTER THE TWO READS ABOVE,
	// and the order is the tenant gate. Staff.Person answered "this person is on this
	// business's roster"; RecentTaps does not ask that question -- an id it does not
	// know is a Measured, empty strip -- so it is only ever asked about a person Person
	// already found here. Neither read can cost the card: a failure is a state the card
	// prints ("could not be read"), never a missing card or an empty strip.
	summary, err := a.staff.Summary(r.Context(), httpx.AdminOf(r).TenantID(), employeeID)
	if errors.Is(err, tenant.ErrUnknownEmployee) {
		return nil, firstWord(problem, "unknown")
	}
	recent := a.ledger.RecentTaps(r.Context(), httpx.AdminOf(r).TenantID(), person.ID)

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
		Name:          person.Name,
		Status:        person.Status,
		Venue:         person.LocationName,
		Department:    person.DepartmentName,
		NoHome:        person.LocationID == nil,
		Hidden:        hidden,
		CloseHref:     rosterReturn(f, uuid.Nil, "", ""),
		CanInvite:     person.Invitable(),
		InviteLabel:   inviteLabel(person.Status),
		InviteAction:  employeeInviteHref,
		CanDeactivate: !person.Deactivated(),
		Confirming:    confirming,
		ConfirmToken:  confirmToken,
		ConfirmField:  confirmField,
		ConfirmHref:   confirmDeactivateHref(f, person.ID),
		// M6-08. It is built here rather than in the template for the reason every
		// other href on this card is: the section owns its own addresses, and a
		// hand-written target is what M6-04 measured going dead with the whole package
		// green.
		RecordHref:       manualEntryHref + "?id=" + person.ID.String(),
		DeactivateAction: employeeDeactivateHref,
		MoveAction:       employeeMoveHref,
		Locations:        optionViews(screen.Options.Locations),
		Departments:      optionViews(screen.Options.Departments),
		LocationID:       uuidParam(person.LocationID),
		DepartmentID:     uuidParam(person.DepartmentID),
		// M10 EM-6. The form is offered to an OWNER only, which is the same
		// predicate the POST checks (mayChangeEmployeeEmail) — so the screen cannot
		// offer what the server would refuse, and a manager reads the sentence that
		// says who can.
		Email:          email,
		CanChangeEmail: mayChangeEmployeeEmail(httpx.AdminOf(r)),
		EmailAction:    employeeEmailHref,
		// M11 API-1, the same predicate the POST checks (mayChangeExternalRef).
		ExternalRef:          person.ExternalRef,
		CanChangeExternalRef: mayChangeExternalRef(httpx.AdminOf(r)),
		ExternalRefAction:    employeeExternalRefHref,
		Initials:             components.Initials(person.Name),
	}
	fillPersonSummary(v, summary, screen.Zone, now)
	fillRecentTaps(v, recent, screen.Zone)
	// M10 EM-7B. The e-mail mode's card: the button mails the link when there is an
	// address, and an OWNER looking at somebody with none is offered the on-screen
	// fallback — the same predicate and the same "no address" the POST checks again
	// (mayShowInviteLink; internal/invite's OnlyWithoutAddress). The panel mode leaves
	// all four fields zero, which is the card it always rendered.
	if a.inviteMail != nil {
		v.InviteByEmail = true
		v.CanShowLink = person.Invitable() && email == "" && mayShowInviteLink(httpx.AdminOf(r))
		v.ShowLinkField, v.ShowLinkValue = inviteDeliverField, inviteDeliverScreen
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
//	added        VERIFIED, SINCE M6-13 ROUND 4. See below -- it was NOT, and the
//	             consequence was a screen telling a manager that somebody who is
//	             already tapping cannot record time.
//	moved        NOT verifiable, and the sentence is built so it does not need to be.
//	             "Moved from X to Y" would be a claim about a previous state nothing
//	             re-reads; what the screen prints is where the person works NOW, read
//	             in the same request. A replayed URL therefore repeats a true
//	             sentence about the current placement rather than inventing a change.
//
// 🔴 WHY `added` NEEDED A GATE AND `moved` DOES NOT, WHICH IS THE DISTINCTION M6-13
// GOT WRONG. The rule is not "was this word verifiable" but "does the BODY beside it
// assert something nobody re-read". The moved body is only movedLine -- a STATE the
// same request read back. The added body goes further: it says "They cannot record
// time yet. Send them their activation link", which is a claim about the person's
// LIFECYCLE, and nothing was checking it.
//
// An audit measured the cost end to end -- add, activate, then reopen the same
// address: the page told the manager that somebody who is already tapping cannot
// record time, and offered the re-invite control beside it, which for an ACTIVE
// person means issuing a link for a new device. A stale tab or a shared URL is enough.
//
// The repair is the gate its sibling already had: somebody who is no longer 'invited'
// has moved past the state this receipt describes, so the receipt is not shown.
func (a *AdminAuth) confirmedRosterAction(r *http.Request, actions *components.RosterActionsView) string {
	done := oneOfWords(r.URL.Query().Get("done"), rosterDoneWords...)
	if done == "" || actions == nil {
		return ""
	}
	if done == "deactivated" && actions.Status != ledger.StatusDeactivated {
		return ""
	}
	if done == "added" && actions.Status != ledger.StatusInvited {
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

// rosterRowView maps one person onto one row of the list.
//
// lastTaps is the page's last-tap reading: only a Measured page may print "—" for a
// person with no tap, and a failed one says so on every row (§4.6).
func rosterRowView(p ledger.Person, zone *time.Location, now time.Time, lastTaps ledger.Reading, manage string) components.RosterRowView {
	last, note := lastTapWords(p.LastTap, lastTaps, zone, now)
	return components.RosterRowView{
		Name:        p.Name,
		Initials:    components.Initials(p.Name),
		Role:        p.Role,
		Venue:       p.LocationName,
		NoHome:      !p.HasHome,
		Department:  p.DepartmentName,
		Status:      p.Status,
		LastTap:     last,
		LastTapNote: note,
		Devices:     deviceCount(p.LiveDevices),
		ManageHref:  manage,
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

// peopleKicker is the line over the title: what the page is, and how many people the
// business has on its books -- all three statuses, whatever the filters say (M12-17's
// Everyone count). A count that was not measured is left out, never printed as 0.
func peopleKicker(t ledger.StatusTally) string {
	if t.State != ledger.Measured {
		return "Who taps"
	}
	return "Who taps · " + strconv.Itoa(t.Everyone.Total()) + " on the books"
}

// rosterChips builds the status chips: "Everyone", then one per word of panelStatuses,
// each the list with that status and the other filters kept, no cursor. A chip's count
// honours the venue, department and name filters (the tally's Matching half) and is
// printed only when the tally was measured.
func rosterChips(f ledger.RosterFilter, t ledger.StatusTally) []components.RosterChipView {
	counted := t.State == ledger.Measured
	chip := func(label, status string, n int) components.RosterChipView {
		g := f
		g.Status = status
		g.AfterID = nil
		href := employeesHref
		if q := rosterQuery(g); len(q) > 0 {
			href += "?" + q.Encode()
		}
		return components.RosterChipView{
			Label: label, Href: href, Current: f.Status == status, Count: n, Counted: counted,
		}
	}
	out := make([]components.RosterChipView, 0, len(panelStatuses)+1)
	out = append(out, chip("Everyone", "", t.Matching.Total()))
	for _, s := range panelStatuses {
		out = append(out, chip(s.Label, s.Value, t.Matching.Of(s.Value)))
	}
	return out
}

// lastTapWords is what a row says about a person's newest record: the time and the
// day in the tenant's zone, and the one word that changes what it means ("refused",
// "by hand").
//
// 🔴 THREE STATES, NOT TWO (§4.6): a measured page with no tap for this person is
// "—"; a page whose last-tap read failed says it could not be read; a page that never
// asked prints nothing at all.
func lastTapWords(t *ledger.LastTap, state ledger.Reading, zone *time.Location, now time.Time) (string, string) {
	switch {
	case state == ledger.ReadFailed:
		return "could not be read", ""
	case state != ledger.Measured:
		return "", ""
	case t == nil:
		return "—", ""
	}
	at := t.At.In(zone)
	note := ""
	switch {
	case t.Verdict == "reject":
		note = "refused"
	case t.Channel == "manual":
		note = "by hand"
	}
	return at.Format("15:04") + " " + dayWords(at, now.In(zone)), note
}

// dayWords names a day relative to today, in the same zone: "today", "yesterday", the
// weekday within the last week, else the date. A day after today (a clock that ran
// ahead) is a date, never "today".
func dayWords(at, now time.Time) string {
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	day := time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)
	today := time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC)
	switch days := int(today.Sub(day) / (24 * time.Hour)); {
	case days == 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days > 1 && days < 7:
		return at.Format("Mon")
	default:
		return at.Format("2 Jan")
	}
}

// deviceCount is the row's short form of deviceLine: the count of live browser
// sessions and nothing else -- the card has the dates.
func deviceCount(n int) string {
	switch {
	case n <= 0:
		return "None signed in"
	case n == 1:
		return "1 signed in"
	default:
		return strconv.Itoa(n) + " signed in"
	}
}

// signedInLine is the card's sentence about live browser sessions: deviceLine with the
// day the oldest of them was signed in put after the count.
func signedInLine(n int, first, lastUsed *time.Time, zone *time.Location) string {
	line := deviceLine(n, lastUsed, zone)
	if n <= 0 || first == nil {
		return line
	}
	head, rest, _ := strings.Cut(line, " · ")
	return head + " · since " + first.In(zone).Format("2 Jan 2006") + " · " + rest
}

// fillPersonSummary writes the summary's lines onto the card (M12-20).
//
// 🔴 NO DATE IS INVENTED. The lifecycle line uses the stamp that matches the status,
// and an INVITED person's card prints none: no statement of this product writes
// invited_at, so for somebody added in the panel the only true date is the pending
// link's own, which LinkLine carries. A summary that was not read leaves every line
// empty; one that failed says so (SummaryFailed).
func fillPersonSummary(v *components.RosterActionsView, s tenant.PersonSummary, zone *time.Location, now time.Time) {
	if zone == nil {
		zone = time.UTC
	}
	switch s.State {
	case tenant.Measured:
	case tenant.ReadFailed:
		v.SummaryFailed = true
		return
	default:
		return
	}
	v.Role = s.Role
	v.LiveDevices = s.LiveDevices
	v.SignedInLine = signedInLine(s.LiveDevices, s.FirstSignedIn, s.LastUsed, zone)
	switch s.Status {
	case ledger.StatusActive:
		if s.Activated != nil {
			v.SinceLine = "Active since " + rosterDate(s.Activated, zone)
		}
	case ledger.StatusDeactivated:
		if s.Deactivated != nil {
			v.SinceLine = "Deactivated on " + rosterDate(s.Deactivated, zone)
		}
	}
	v.LinkLine = linkLine(s.Status, s.Link, zone, now)
}

// linkLine says whether an activation link is outstanding. It says "created", never
// "sent": what the product knows is that a link exists, not that it reached anybody.
// For somebody already active it speaks only when a link IS outstanding -- "no working
// link" beside an active person would read as a fault.
func linkLine(status string, l tenant.ActivationLink, zone *time.Location, now time.Time) string {
	switch l.State {
	case tenant.Measured:
	case tenant.ReadFailed:
		return "Whether a link is outstanding could not be read just now."
	default:
		return ""
	}
	if l.Pending == 0 || l.Created == nil || l.Expires == nil {
		if status == ledger.StatusInvited {
			return "No working link yet."
		}
		return ""
	}
	created := l.Created.In(zone)
	when := "today"
	if d := dayWords(created, now.In(zone)); d != "today" {
		when = "on " + created.Format("2 Jan")
	}
	return "A link was created " + when + " and works until " + l.Expires.In(zone).Format("2 Jan 15:04") + "."
}

// fillRecentTaps writes the strip (M12-19): day, direction, time and the engine's word.
func fillRecentTaps(v *components.RosterActionsView, rt ledger.RecentTaps, zone *time.Location) {
	if zone == nil {
		zone = time.UTC
	}
	switch rt.State {
	case ledger.Measured:
		v.RecentState = "measured"
	case ledger.ReadFailed:
		v.RecentState = "failed"
		return
	default:
		return
	}
	v.Recent = make([]components.MiniTapView, 0, len(rt.Taps))
	for _, t := range rt.Taps {
		at := t.At.In(zone)
		dir := "—"
		switch t.Direction {
		case "in":
			dir = "IN"
		case "out":
			dir = "OUT"
		}
		v.Recent = append(v.Recent, components.MiniTapView{
			Day:       at.Format("Mon 2"),
			Direction: dir,
			Time:      at.Format("15:04"),
			Verdict:   t.Verdict,
			Word:      verdictWord(t.Verdict),
		})
	}
}

// verdictWord is the engine's verdict as the stamp spells it. An unknown word is said
// as unknown rather than dressed as one of the four.
func verdictWord(v string) string {
	switch v {
	case "ok":
		return "APPROVED"
	case "flag":
		return "FLAGGED"
	case "reject":
		return "REJECTED"
	case "ignored":
		return "IGNORED"
	default:
		return "UNKNOWN"
	}
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
