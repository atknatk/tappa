package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The LOCATIONS section's VENUE AND DEPARTMENT WRITES — M6-06 PHASE A: add a
// venue, edit a venue, add a department, edit a department. The create and the
// edit share a route because they share a form; which one it is comes from whether
// the form carried an id.
//
// 🔴 THE TENANT AND THE ACTOR ARE NEVER INPUTS (§4.5, §4.7). Both come from
// httpx.AdminOf(r), which the Protect chain resolved from a signed session cookie
// against the database. What a client contributes is a venue id or a department id
// — narrowing values inside that tenant, because the tenant predicate they are
// ANDed with is not one the client supplied. A foreign id produces no row rather
// than a foreign write, and internal/domain/tenant turns that into a sentence
// rather than a 500.
//
// 🔴 THE ROUTES MOUNT ProtectWriting AND THE ORDER IS THE POINT.
// floodGate → sameOriginGate → requireAdmin → sessionGate: the Origin check runs
// BEFORE the resolver, so a cross-origin POST costs zero database work. M6-04
// shipped that chain with the gate INSIDE the protected group and an audit measured
// what it bought: 300 cross-origin POSTs spent a signed-in manager's whole session
// budget and locked them out of their own panel for ten minutes, charging 301
// unbudgeted session lookups on the way. Registering these two anywhere but
// mountWriting would silently take the READ chain instead.
//
// 🔴 BOTH ACTS WRITE audit_log AND THE ROW SHARES THE FATE OF THE CHANGE.
// internal/domain/tenant/venue.go writes it with audit.RecordTx inside the domain's
// own transaction, and its header says why that matters more here than anywhere
// else in the panel: `locations` has no updated_at, so a change to the evidence a
// tap is judged on would otherwise leave no trace anywhere at all.
//
// 🔴 A REJECTED FORM RE-RENDERS INSTEAD OF REDIRECTING, WHICH BREAKS THIS PANEL'S
// OWN PATTERN AND IS WORTH THE SENTENCE. Every other panel write answers 303 with a
// word from a closed vocabulary. That works when the input is one id and one verb;
// this form carries eight fields, and a redirect would throw away everything the
// manager typed to report that one of them was wrong. So a validation failure
// answers 200 with the same form, the same values and a sentence naming the field —
// and nothing is written, so a browser re-post is harmless. A SUCCESSFUL save still
// redirects, which is what keeps a refresh from saving twice.

// panelVenues is the slice of internal/domain/tenant this section needs, declared
// HERE at the consumer (§7).
//
// IT IS A SEPARATE FIELD FROM panelStaff even though both are the same PACKAGE, and
// that is a departure from the rule M6-04 set ("a different package gets a different
// field") which is worth stating rather than smuggling. The rule exists so that
// "no store call in ledger is not a SELECT" stays checkable; it is about keeping
// READS and WRITES apart, and these are both writes. What decides it here is the
// constructor: NewAdminAuth already takes seven dependencies, and a wider panelStaff
// carrying six unrelated methods would let a change to the venue side break the
// employee side's wiring with no compile error at the call site. Two narrow
// interfaces, two concrete values, one package.
type panelVenues interface {
	Screen(ctx context.Context, tenantID uuid.UUID) (tenant.VenueScreen, error)
	// Venue and Department resolve ONE row by id. They are the fallback behind the
	// capped Screen lists: a row past venuePageLimit is still the tenant's, and
	// venueForms says why answering it from the slice alone made the product state
	// something untrue.
	Venue(ctx context.Context, tenantID, venueID uuid.UUID) (tenant.Venue, error)
	Department(ctx context.Context, tenantID, departmentID uuid.UUID) (tenant.Department, error)
	SaveVenue(ctx context.Context, c tenant.VenueCommand) (tenant.Venue, error)
	SaveDepartment(ctx context.Context, c tenant.DepartmentCommand) (tenant.Department, error)
	// VenueReferences and DepartmentReferences count what still points at a row, so
	// the screen can withhold the delete control and say why with numbers. They are
	// the SCREEN's information; the ON DELETE RESTRICT keys are the guard.
	VenueReferences(ctx context.Context, tenantID, venueID uuid.UUID) (tenant.References, error)
	DepartmentReferences(ctx context.Context, tenantID, departmentID uuid.UUID) (tenant.References, error)
	DeleteVenue(ctx context.Context, c tenant.DeleteCommand) error
	DeleteDepartment(ctx context.Context, c tenant.DeleteCommand) error
	// ConfirmRemoval answers "did THIS admin remove THIS row just now?" from the
	// append-only trail. It decides whether an acknowledgement is PRINTED and
	// authorises nothing — locations.go/removalReceipt says why that distinction is
	// the point.
	ConfirmRemoval(ctx context.Context, tenantID, actorID uuid.UUID, action, target string) (tenant.RemovalReceipt, error)
}

// saveVenue adds a venue or rewrites one.
//
// 🔴 WHAT A SUCCESSFUL SAVE MAY HAVE DONE. Emptying static_ips and clearing the
// coordinate is a valid, silent configuration change that moves every future tap at
// this venue onto §5 row 7 — recorded, `flag`, sent to the approval queue.
//
// ⚠️ AND THE BANNER DOES NOT SAY IT — the VENUE CARD does, and this comment used to
// claim the opposite. There is no such word: the vocabulary is venue-saved /
// venue-added, and a save that empties everything redirects to ?done=venue-saved
// (measured). That is deliberate and web/templates/pages/locations.templ argues it
// where the banner is written: what a manager needs after editing a venue is whether
// it still has proof of place, and the card beneath the banner states it FROM THE ROW
// the same request just read. A banner repeating it would be a second place for that
// fact to drift out of step. Two comments in one change disagreed; this was the
// wrong one.
func (a *AdminAuth) saveVenue(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readVenueBody(w, r) {
		return
	}

	sub, field, msg := parseVenueForm(r)
	if msg != "" {
		// 🔴 NOTHING WAS WRITTEN AND THE MANAGER KEEPS THEIR TYPING. The form is
		// rebuilt from what they posted — re-validated on the way back out by the same
		// escaping templ applies to everything — so a correction is one field rather
		// than eight.
		a.log.Warn("panel venue save refused: the form did not validate",
			"field", field, "venue_id", sub.VenueID)
		a.renderVenueFormAgain(w, r, venueFormFromSubmission(sub, r), field, msg)
		return
	}

	saved, err := a.venues.SaveVenue(r.Context(), tenant.VenueCommand{
		TenantID: id.TenantID(),
		VenueID:  sub.VenueID,
		// 🔴 THE ACTOR COMES FROM THE SESSION. There is no field on this form for it
		// and there could not be: the id below was resolved from a signed cookie
		// against admin_users, which is the only thing audit_log.actor_id may hold.
		ActorID:   id.Admin.AdminUserID,
		Name:      sub.Name,
		StaticIPs: sub.StaticIPs,
		GPS:       sub.GPS,
		Shift:     sub.Shift,
		WiFiSSID:  sub.WiFiSSID,
	})
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrUnknownVenue):
		a.log.Warn("panel venue save refused: no such venue in this tenant")
		a.redirect(w, venueReturn("", "unknown-venue"))
		return
	case errors.Is(err, tenant.ErrVenueName):
		// The domain's own bound refused a name the boundary let through, which means
		// the two disagree. It is reported as a form failure rather than a 500 — the
		// manager can still fix it — and logged as the inconsistency it is.
		a.log.Error("panel venue save: the domain refused a name the boundary accepted")
		a.renderVenueFormAgain(w, r, venueFormFromSubmission(sub, r), "name",
			"That name cannot be stored. Try a shorter one.")
		return
	default:
		a.log.Error("panel: could not save the venue", "err", err, "venue_id", sub.VenueID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	done := "venue-saved"
	if sub.VenueID == uuid.Nil {
		done = "venue-added"
	}
	a.log.Info("panel venue saved", "venue_id", saved.ID,
		"proof_of_place", saved.HasProofOfPlace())
	a.redirect(w, venueReturn(done, ""))
}

// saveDepartment adds a department or rewrites one.
func (a *AdminAuth) saveDepartment(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readVenueBody(w, r) {
		return
	}

	sub, field, msg := parseDepartmentForm(r)
	if msg != "" {
		a.log.Warn("panel department save refused: the form did not validate",
			"field", field, "department_id", sub.DepartmentID)
		a.renderDepartmentFormAgain(w, r, sub, field, msg)
		return
	}

	saved, err := a.venues.SaveDepartment(r.Context(), tenant.DepartmentCommand{
		TenantID:     id.TenantID(),
		DepartmentID: sub.DepartmentID,
		ActorID:      id.Admin.AdminUserID,
		LocationID:   sub.LocationID,
		Name:         sub.Name,
		Shift:        sub.Shift,
	})
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrUnknownVenue):
		// The venue the department was to sit in is not this business's. It is a
		// SEPARATE word from "unknown-department" because it sends the manager
		// somewhere else: to the venue list, not to the department they were editing.
		a.log.Warn("panel department save refused: the venue is not this tenant's")
		a.redirect(w, venueReturn("", "unknown-venue"))
		return
	case errors.Is(err, tenant.ErrUnknownDepartment):
		a.log.Warn("panel department save refused: no such department in this tenant")
		a.redirect(w, venueReturn("", "unknown-department"))
		return
	case errors.Is(err, tenant.ErrDepartmentName):
		a.log.Error("panel department save: the domain refused a name the boundary accepted")
		a.renderDepartmentFormAgain(w, r, sub, "name",
			"That name cannot be stored. Try a shorter one.")
		return
	default:
		a.log.Error("panel: could not save the department", "err", err,
			"department_id", sub.DepartmentID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	done := "department-saved"
	if sub.DepartmentID == uuid.Nil {
		done = "department-added"
	}
	a.log.Info("panel department saved", "department_id", saved.ID)
	a.redirect(w, venueReturn(done, ""))
}

// readVenueBody bounds the body and parses it.
//
// 🔴 THE BODY IS BOUNDED BEFORE IT IS PARSED. MaxBytesReader makes ParseForm fail on
// an oversized body rather than buffering it, so the ceiling is enforced by the READ
// rather than by a length check afterwards (review.go's shape, employeeactions.go's
// shape).
//
// 🔴 THE PARSE ERROR IS CLASSIFIED, NOT PRINTED. net/url's failure QUOTES THE
// OFFENDING INPUT — an audit measured three bytes of a manager's note reaching the
// process log through exactly this branch in M6-04. Nothing a caller sent appears in
// the log line below.
func (a *AdminAuth) readVenueBody(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxVenueBody)
	if err := r.ParseForm(); err != nil {
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel venue action refused: the form could not be read",
			"limit_bytes", maxVenueBody, "reason", reason)
		a.redirect(w, venueReturn("", "unreadable"))
		return false
	}
	return true
}

// renderVenueFormAgain answers a rejected venue form with the manager's own values
// and a sentence naming the field. It is 200 rather than 400 because the response IS
// the form: a browser showing an error page would lose the values, which is the cost
// this whole shape exists to avoid.
//
// 🔴 IT STILL READS THE LISTS, so the page behind the card is the current one. If
// that read fails the manager is shown a problem page rather than a form floating
// over nothing — the failure is ours and saying so is §4.6.
func (a *AdminAuth) renderVenueFormAgain(w http.ResponseWriter, r *http.Request, form components.VenueFormView, field, msg string) {
	screen, err := a.venues.Screen(r.Context(), httpx.AdminOf(r).TenantID())
	if err != nil {
		a.log.Error("panel: could not re-read the venues after a rejected form", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	plaques, err := a.locationsPlaques(r)
	if err != nil {
		a.log.Error("panel: could not re-read the plaques after a rejected form", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	form.ErrorField = field
	form.ErrorMessage = msg
	v := a.locationsShell(r, screen, plaques)
	v.VenueForm = &form
	bindFormTargets(&v)
	a.render(w, r, http.StatusOK, pages.AdminLocations(v))
}

// renderDepartmentFormAgain is the same for the department form.
//
// 🔴 THE VENUE IS RESOLVED FROM THE DEPARTMENT, NOT FROM THE POSTED FORM, AND GETTING
// THAT BACKWARDS PRINTED A SENTENCE ABOUT A BROKEN DATABASE AT SOMEBODY WHO MISTYPED
// A NAME. On an edit the form is VenueFixed and therefore renders NO location_id
// control at all, so the POST carries none — yet this path used to look the venue name
// up by `r.PostFormValue("location_id")`, which is always empty there. The lookup
// missed every time, VenueName stayed "", and components/venue.templ printed "Venue
// could not be read" — a string venueview.go documents as meaning THE JOIN MISSED, a
// repair problem. Measured: every rejected edit (empty name, over-long name,
// half-filled shift) rendered it.
//
// If the department cannot be resolved at all, the id is not this business's, and
// re-rendering an edit card for it would be answering a question we should refuse:
// the manager is redirected with the same word the save path uses.
func (a *AdminAuth) renderDepartmentFormAgain(w http.ResponseWriter, r *http.Request, sub departmentSubmission, field, msg string) {
	id := httpx.AdminOf(r)
	screen, err := a.venues.Screen(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not re-read the venues after a rejected form", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	plaques, err := a.locationsPlaques(r)
	if err != nil {
		a.log.Error("panel: could not re-read the plaques after a rejected form", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	form, outcome := a.departmentFormFromSubmission(r, sub, screen)
	switch outcome {
	case formOK:
	case formUnknownDepartment:
		a.log.Warn("panel department form refused: no such department in this tenant")
		a.redirect(w, venueReturn("", "unknown-department"))
		return
	case formUnknownVenue:
		a.log.Warn("panel department form refused: the venue is not this tenant's")
		a.redirect(w, venueReturn("", "unknown-venue"))
		return
	default:
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	form.ErrorField = field
	form.ErrorMessage = msg
	v := a.locationsShell(r, screen, plaques)
	v.DepartmentForm = &form
	bindFormTargets(&v)
	a.render(w, r, http.StatusOK, pages.AdminLocations(v))
}

// formOutcome says what rebuilding a rejected form found.
type formOutcome int

const (
	formOK formOutcome = iota
	formUnknownDepartment
	formUnknownVenue
	formUnavailable
)

// locationsShell builds the section around a card, WITHOUT reading the query string
// for one — the POST that lands here has no ?venue= and must not grow one.
//
// 🔴 IT IS THE ONLY PLACE THE SHELL IS BUILT, INCLUDING FOR THE GET. Both paths
// filled this view model separately until the copies were found to disagree: the
// GET set a count and this one set none, so a capped list on the re-render path read
// "Only the first  venues are shown". §4.6 asks a truncated list to say it is
// truncated, which a sentence with a hole in it does not do.
//
// 🔴 AND THE TWO COUNTS ARE COUNTED SEPARATELY. Rendering the venue count into the
// departments sentence was the second half of the same defect — a business with 3
// venues and 250 departments was told "only the first 3 departments are shown",
// which is false and unfalsifiable from the page. The lists are capped
// independently, so each names its own length.
//
// 🔴 IT ALSO FILLS THE PLAQUE LISTS (M6-06 phase B), for the reason the paragraph
// above gives about the venue count: the moment a second builder existed the two
// copies drifted, and a re-render path that showed no plaques would tell a manager
// their stock is empty because their venue name was too long.
func (a *AdminAuth) locationsShell(r *http.Request, screen tenant.VenueScreen, plaques tenant.PlaqueScreen) pages.LocationsView {
	v := pages.LocationsView{
		PanelChrome:       a.chrome(r, pages.TabLocations),
		Queried:           true,
		VenuesCapped:      screen.VenuesCapped,
		DepartmentsCapped: screen.DepartmentsCapped,
		VenueLimit:        strconv.Itoa(len(screen.Venues)),
		DepartmentLimit:   strconv.Itoa(len(screen.Departments)),
		AddVenueHref:      locationsHref + "?venue=new",
		AddDepartmentHref: locationsHref + "?department=new",
		CloseHref:         locationsHref,
		VenueAction:       venueSaveHref,
		DepartmentAction:  departmentSaveHref,
	}
	v.Venues, v.Departments = venueRowViews(screen)
	fillPlaques(&v, plaques, screen, plaques.Zone)
	return v
}

// venueFormFromSubmission rebuilds the venue form from what was POSTED rather than
// from what validated.
//
// 🔴 IT READS THE RAW FIELDS, NOT THE PARSED ONES, AND THAT IS THE WHOLE POINT. The
// submission is only partly filled when validation stopped early — the value that
// was WRONG is by definition not in it — so rebuilding from the parsed struct would
// silently blank the field the manager needs to fix. They get their own text back.
func venueFormFromSubmission(sub venueSubmission, r *http.Request) components.VenueFormView {
	heading, submit := "Add a venue", "Add venue"
	idText := ""
	if sub.VenueID != uuid.Nil {
		heading, submit = "Edit venue", "Save venue"
		idText = sub.VenueID.String()
	}
	return components.VenueFormView{
		Heading:    heading,
		ID:         idText,
		Name:       r.PostFormValue("name"),
		Ranges:     r.PostFormValue("static_ips"),
		GPSLat:     r.PostFormValue("gps_lat"),
		GPSLng:     r.PostFormValue("gps_lng"),
		ShiftStart: r.PostFormValue("shift_start"),
		ShiftEnd:   r.PostFormValue("shift_end"),
		Overnight:  r.PostFormValue("overnight") != "",
		WiFiSSID:   r.PostFormValue("wifi_ssid"),
		Submit:     submit,
	}
}

// departmentFormFromSubmission rebuilds the department form from what was POSTED,
// with the VENUE resolved from the database rather than from the body.
//
// 🔴 AN EDIT AND A CREATE RESOLVE THE VENUE FROM DIFFERENT PLACES, AND CONFLATING
// THEM WAS THE BUG. On an EDIT the venue is a property of the DEPARTMENT — the form
// renders no control for it, so the body cannot carry it and asking the body is
// asking the wrong thing. On a CREATE the venue is exactly what the manager chose, so
// the body is the only source there is.
//
// 🔴 AND BOTH FALL BACK PAST THE CEILING. The screen lists are capped
// (venuePageLimit), so a department or a venue beyond the cap is absent from them —
// the same gap venueForms closes for the GET path, one function along. Without the
// fallback here, a rejected CREATE at a beyond-cap venue would re-render with that
// venue missing from the dropdown and a DIFFERENT one selected, silently moving the
// department the manager was adding.
func (a *AdminAuth) departmentFormFromSubmission(r *http.Request, sub departmentSubmission, screen tenant.VenueScreen) (components.DepartmentFormView, formOutcome) {
	tenantID := httpx.AdminOf(r).TenantID()
	f := components.DepartmentFormView{
		Heading:    "Add a department",
		Submit:     "Add department",
		Name:       r.PostFormValue("name"),
		ShiftStart: r.PostFormValue("shift_start"),
		ShiftEnd:   r.PostFormValue("shift_end"),
		Overnight:  r.PostFormValue("overnight") != "",
	}

	if sub.DepartmentID != uuid.Nil {
		dep, found, err := a.departmentByID(r, tenantID, sub.DepartmentID, screen)
		switch {
		case err != nil:
			a.log.Error("panel: could not resolve the department behind a rejected form", "err", err)
			return f, formUnavailable
		case !found:
			return f, formUnknownDepartment
		}
		f.ID = sub.DepartmentID.String()
		// THE HEADING NAMES THE ROW, NOT THE DRAFT. The manager may have typed a new
		// name and had it refused; the card is still about the department they opened.
		f.Heading = "Edit " + dep.Name
		f.Submit = "Save department"
		f.VenueFixed = true
		f.VenueID = dep.LocationID.String()
		f.VenueName = dep.LocationName
		return f, formOK
	}

	// CREATE: the chosen venue comes from the body, and the dropdown must still
	// contain it after the refusal or the manager's choice is silently changed.
	options := venueOptionViews(screen.Venues)
	f.VenueID = r.PostFormValue("location_id")
	if sub.LocationID != uuid.Nil && !hasOption(options, f.VenueID) {
		ven, err := a.venues.Venue(r.Context(), tenantID, sub.LocationID)
		switch {
		case err == nil:
			options = append(options, components.OptionView{
				Value: ven.ID.String(), Label: ven.Name,
			})
		case errors.Is(err, tenant.ErrUnknownVenue):
			// Not this business's venue. The save path would refuse it with the same
			// word, so the form is not re-offered around a venue that cannot receive it.
			return f, formUnknownVenue
		default:
			a.log.Error("panel: could not resolve the venue behind a rejected form", "err", err)
			return f, formUnavailable
		}
	}
	f.Venues = options
	return f, formOK
}

// departmentByID answers from the page's own list first and falls back to the
// tenant-scoped by-id read for a row past the ceiling.
func (a *AdminAuth) departmentByID(r *http.Request, tenantID, departmentID uuid.UUID, screen tenant.VenueScreen) (tenant.Department, bool, error) {
	for _, d := range screen.Departments {
		if d.ID == departmentID {
			return d, true, nil
		}
	}
	dep, err := a.venues.Department(r.Context(), tenantID, departmentID)
	switch {
	case err == nil:
		return dep, true, nil
	case errors.Is(err, tenant.ErrUnknownDepartment):
		return tenant.Department{}, false, nil
	default:
		return tenant.Department{}, false, err
	}
}

func hasOption(options []components.OptionView, value string) bool {
	if value == "" {
		return false
	}
	for _, o := range options {
		if o.Value == value {
			return true
		}
	}
	return false
}

// venueReturn builds the 303 target.
//
// 🔴 EVERY PART OF IT IS SERVER-BUILT FROM A CLOSED VOCABULARY. Nothing the client
// posted is echoed into the Location header, and there is no field a caller could
// put a URL in, so this cannot become an open redirect.
func venueReturn(done, problem string) string {
	q := ""
	if w := oneOfWords(done, venueDoneWords...); w != "" {
		q = "?done=" + w
	} else if w := oneOfWords(problem, venueProblemWords...); w != "" {
		q = "?problem=" + w
	}
	return locationsHref + q
}

// --- removal (user decision, 2026-08-08: option (b), unreferenced rows only) --------

// deleteVenue removes a venue nothing points at.
//
// 🔴 THE COUNT DECIDES WHETHER THE CONTROL IS OFFERED; THE FOREIGN KEYS DECIDE WHETHER
// THE DELETE HAPPENS. Six ON DELETE RESTRICT keys reference these tables, and between
// the screen counting zero and this press landing another manager can hire somebody
// into the venue. Postgres then answers 23503, the row survives, and the domain turns
// that into ErrVenueInUse — a sentence, never a 500. Belt and braces, and the braces
// are the ones that actually hold.
//
// 🔴 IT REUSES THE ROSTER'S CONFIRMATION RATHER THAN INVENTING A SECOND ONE. Measured
// against deactivateconfirm.go: that value is bound to ONE ACTION, ONE SUBJECT ID and ONE
// PANEL SESSION, HMAC-signed under a key derived from the server's secret, with a TTL
// and a future-skew bound — nothing in it is employee-specific, so a venue id fits its
// `mint(action, subjectID, sessionID)` shape exactly. The ACTION field is version 2,
// added by this task: without it a removal confirmation opened the deactivation gate. Half-copying a pattern is a mistake this
// repository has now made three times (deactivateconfirm.go counts them), and the
// cheapest way not to make it a fourth is to call the same code.
//
// AND THE CASE FOR CONFIRMING IS STRONGER HERE THAN THERE. A deactivation sets a
// status a manager can reverse; this removes a row, and §4.3 means the audit entry is
// all that is left of it.
func (a *AdminAuth) deleteVenue(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readVenueBody(w, r) {
		return
	}
	venueID, ok := a.deleteTarget(w, r)
	if !ok {
		return
	}
	// 🔴 THE ROLE GATE RUNS BEFORE THE CONFIRMATION GATE. A manager must not be able to
	// learn anything from the order of refusals, and checking the cheap, local fact
	// first also means a bypassed POST never reaches the trail lookup.
	if !mayRemove(id) {
		a.refuseRemovalByRole(w, r, id, "venue", venueID)
		return
	}
	if why := a.confirmationRefusalFor(r, confirmActionDeleteVenue, venueID.String()); why != "" {
		a.log.Warn("panel venue delete refused: not confirmed", "reason", why)
		a.redirect(w, venueReturn("", why))
		return
	}

	err := a.venues.DeleteVenue(r.Context(), tenant.DeleteCommand{
		TenantID: id.TenantID(),
		ID:       venueID,
		ActorID:  id.Admin.AdminUserID,
	})
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrUnknownVenue):
		a.log.Warn("panel venue delete refused: no such venue in this tenant")
		a.redirect(w, venueReturn("", "unknown-venue"))
		return
	case errors.Is(err, tenant.ErrVenueInUse):
		// 🔴 THE RACE, AND IT IS A SENTENCE RATHER THAN A FAILURE. The screen offered
		// the control because nothing pointed at the venue when it rendered; something
		// does now. Nothing was removed and nothing is wrong with the request.
		a.log.Warn("panel venue delete refused: something was placed here after the screen rendered")
		a.redirect(w, venueReturn("", "venue-in-use"))
		return
	default:
		a.log.Error("panel: could not delete the venue", "err", err, "venue_id", venueID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// 🔴 THE COOKIE IS CLEARED AFTER A SUCCESSFUL REMOVAL, WHICH IS THE OPPOSITE OF
	// WHAT THE DEACTIVATION GATE DOES, AND THE DIFFERENCE IS DELIBERATE. That gate
	// clears BEFORE the write (employeeactions.go) so a failed write leaves no reusable
	// value; the cost it accepts is that a manager retrying after a database error
	// walks through the warning again.
	//
	// A REMOVAL HAS A FAILURE MODE THAT ONE DOES NOT: the FK race. Something can be
	// placed at the venue between the screen counting zero and this press landing, and
	// the answer is "it is not empty any more" — a refusal about the WORLD, not about
	// the manager's intent. Throwing away their confirmation for that would make them
	// re-read a warning to retry an act they already confirmed, for a reason that was
	// never theirs. So the value survives a refusal and dies with the act.
	//
	// ⚠️ THE ASYMMETRY COSTS SOMETHING AND IT IS COUNTED, NOT WAVED THROUGH: for the
	// window remaining on the token, a refused removal can be re-pressed without a
	// second warning. That is exactly what a manager clearing the venue and trying
	// again wants, and it is why the single-use limit at deactivateconfirm.go matters
	// less here than there — a second DELETE of a row already gone matches nothing.
	a.short.clear(w, adminConfirmCookieName)
	a.log.Info("panel venue deleted", "venue_id", venueID)
	// 🔴 THE REDIRECT NAMES THE ROW IT REMOVED, AND THE SECTION VERIFIES IT AGAINST THE
	// TRAIL BEFORE SAYING ANYTHING (user decision, 2026-08-09, reading C'). The id is a
	// CLIENT value from here on, which is exactly why it is only ever used as a lookup
	// KEY: the sentence and the name come from the audit row it finds, or there is no
	// sentence. That is M6-05's rule (2) — verify against a row the same request read —
	// applied to the one act where the row itself is gone.
	a.redirect(w, venueRemovalReturn("venue-deleted", venueID))
}

// deleteDepartment removes a department nothing points at. Two keys rather than four
// (employees_department_fk, transactions_department_fk), same shape and same rules —
// the symmetry is the point, because a manager should not learn two different logics
// from two lists on one screen.
func (a *AdminAuth) deleteDepartment(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readVenueBody(w, r) {
		return
	}
	departmentID, ok := a.deleteTarget(w, r)
	if !ok {
		return
	}
	if !mayRemove(id) {
		a.refuseRemovalByRole(w, r, id, "department", departmentID)
		return
	}
	if why := a.confirmationRefusalFor(r, confirmActionDeleteDepartment, departmentID.String()); why != "" {
		a.log.Warn("panel department delete refused: not confirmed", "reason", why)
		a.redirect(w, venueReturn("", why))
		return
	}

	err := a.venues.DeleteDepartment(r.Context(), tenant.DeleteCommand{
		TenantID: id.TenantID(),
		ID:       departmentID,
		ActorID:  id.Admin.AdminUserID,
	})
	switch {
	case err == nil:
	case errors.Is(err, tenant.ErrUnknownDepartment):
		a.log.Warn("panel department delete refused: no such department in this tenant")
		a.redirect(w, venueReturn("", "unknown-department"))
		return
	case errors.Is(err, tenant.ErrDepartmentInUse):
		a.log.Warn("panel department delete refused: something was placed here after the screen rendered")
		a.redirect(w, venueReturn("", "department-in-use"))
		return
	default:
		a.log.Error("panel: could not delete the department", "err", err,
			"department_id", departmentID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// Cleared on success only — deleteVenue carries the argument for the asymmetry.
	a.short.clear(w, adminConfirmCookieName)
	a.log.Info("panel department deleted", "department_id", departmentID)
	a.redirect(w, venueRemovalReturn("department-deleted", departmentID))
}

// deleteTarget reads the id a removal names.
//
// AN UNREADABLE ID IS "unreadable", NOT "unknown", because nothing was looked up —
// the distinction the section's vocabulary makes and unresolvedCardProblem follows.
func (a *AdminAuth) deleteTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.PostFormValue("id"))
	target, err := uuid.Parse(raw)
	if err != nil || target == uuid.Nil {
		a.log.Warn("panel delete refused: the id could not be read")
		a.redirect(w, venueReturn("", "unreadable"))
		return uuid.Nil, false
	}
	return target, true
}

// --- who may remove (user decision, 2026-08-09) --------------------------------------

// adminRoleOwner is the role a removal requires.
//
// 🔴 THE ROLE COLUMN HAS EXISTED SINCE M6-02 AND NOTHING READ IT. Measured by a
// security audit: db/migrations/00006 declares
// `role text NOT NULL CHECK (role IN ('owner','manager'))`, the development database
// holds 30 359 owners and 7 417 managers, and 2 814 tenants have both — while no panel
// write path read the field at all. The gap is old; what changed is its COST. M6-05's
// writes are reversible states (a deactivation can be undone by adding somebody
// again); this phase opened the product's first IRREVERSIBLE path, and there is no
// archive to restore from. So the same gap now means a manager can permanently destroy
// a venue, and the only surviving trace is audit_log.
//
// ⚠️ SCOPE: REMOVAL ONLY. Creating, editing and deactivating are unchanged — a silent
// widening of this into other acts would be exactly the "quiet expansion" the working
// rules forbid, and nobody has asked for it.
const adminRoleOwner = "owner"

// mayRemove reports whether this session's admin may remove a venue or a department.
//
// 🔴 THE ROLE COMES FROM THE RESOLVED SESSION, WHICH READ IT FROM THE DATABASE. It is
// not a cookie claim and not a form field: adminauth.Resolved.Role is filled from the
// admin_users row during session resolution, so this costs NO extra query and cannot
// be supplied by the caller.
func mayRemove(id httpx.AdminIdentity) bool {
	return id.Live() && id.Admin.Role == adminRoleOwner
}

// Audit actions for a refused removal (user decision, 2026-08-09).
//
// 🔴 THE NAMES FOLLOW THE FIVE REFUSALS THIS PRODUCT ALREADY WRITES rather than
// inventing a sixth convention: activation.failed, admin.login.failed,
// tap.rate_limited, admin.login.tenant_refused, admin.login.rate_limited. Two shapes
// live in that list — `<subject>.<past participle>` and `<subject>.<verb>_<refusal>` —
// and these take the second, because `location.deleted` is already taken by the act
// that SUCCEEDED and a reader scanning `action LIKE 'location.%'` must not have to
// look twice to tell them apart.
const (
	ActionLocationDeleteRefused   = "location.delete_refused"
	ActionDepartmentDeleteRefused = "department.delete_refused"
)

// refusedRemovalDetail is the audit row's payload for a refused removal.
//
// 🔴 EXPLICIT EMPTIES, NO omitempty ON THE FACTS — the lesson deletedDetail records.
// A key that is absent cannot be told apart from a value that was empty, and this row
// exists precisely so somebody can reconstruct what was attempted.
//
// 🔴 §4.7: THERE IS NO FIELD HERE A SECRET COULD TRAVEL IN. No token, no cookie, no
// coordinate, no session id — the confirmation is not even read before the refusal, so
// there is nothing to leak. What it carries is the act, the role that was insufficient
// and the row that was aimed at, all of which the tenant already owns.
type refusedRemovalDetail struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
	// What is "venue" or "department" — the same word the screen uses, so the trail
	// reads the way the manager's screen did.
	What string `json:"what"`
	// Role is the role the actor actually held. It is the whole substance of the
	// refusal and it is the actor's own attribute, not a secret.
	Role string `json:"role"`
	// RequiredRole is what the act needs, so the row is readable without knowing the
	// product's rules.
	RequiredRole string `json:"required_role"`
}

// refuseRemovalByRole records a refused attempt and answers it.
//
// 🔴 THE SCREEN NEVER OFFERS THIS CONTROL TO A MANAGER, SO REACHING HERE MEANS THE UI
// WAS BYPASSED — a stale page, a shared link, or a deliberate POST. Both halves are
// required and neither substitutes for the other: hiding the button is the courtesy,
// this is the guarantee.
//
// 🔴 THE ATTEMPT IS WRITTEN TO audit_log (user decision, 2026-08-09) AND THE DECIDING
// ARGUMENT WAS VISIBILITY, NOT SHAPE. Five refusals already use this table, so the
// shape risk is low; what the process log cannot do is show an OWNER that their
// manager has been trying to remove venues. A structured log line has no tenant
// boundary, no retention story and no reader outside the operator. Bloat is bounded by
// adminSessionLimit, which caps one session at 300 requests per 10 minutes — measured,
// not assumed.
//
// 🔴 IT IS ITS OWN TRANSACTION (audit.Record, not RecordTx) BECAUSE THERE IS NO OTHER.
// Nothing is being deleted — that is the point — so there is no surrounding write for
// the row to share a fate with. This is the case audit.Record's own comment describes:
// the caller who most needs a trail row is the one whose act did NOT happen.
//
// 🔴 AND A FAILED AUDIT WRITE DOES NOT CHANGE THE ANSWER. The manager still gets the
// refusal sentence, never a 500. This is a RECORDING path, not an authorising one: the
// refusal already happened in the line above, and turning a trail outage into "the
// panel is unavailable" would report OUR failure as a verdict on THEIR request.
// a.record logs the failure loudly and returns — the same behaviour every one of the
// five precedents relies on.
//
// ⚠️ BOTH THE LOG LINE AND THE AUDIT ROW ARE KEPT, AND THAT IS NOT TWO
// REPRESENTATIONS OF ONE FACT — it is one event written for two readers, which is what
// adminlogin.go already does at ActionAdminLoginRefused. The log serves the operator
// during an incident: immediate, greppable, carrying operational context the trail has
// no column for. The audit row serves the tenant's own owner next quarter:
// tenant-scoped, durable, append-only. Dropping either would silently remove a reader.
func (a *AdminAuth) refuseRemovalByRole(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, what string, target uuid.UUID) {
	a.log.Warn("panel removal refused: this admin is not an owner",
		"what", what, "target", target, "actor_id", id.Admin.AdminUserID,
		"role", id.Admin.Role)

	action := ActionLocationDeleteRefused
	if what == "department" {
		action = ActionDepartmentDeleteRefused
	}
	a.record(r.Context(), audit.Event{
		// §4.5: the tenant is the SESSION's, never the request's.
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   action,
		Target:   target.String(),
		Detail: refusedRemovalDetail{
			Outcome:      "refused",
			Reason:       "removing is reserved for an owner",
			What:         what,
			Role:         id.Admin.Role,
			RequiredRole: adminRoleOwner,
		},
	})

	a.redirect(w, venueReturn("", "not-permitted"))
}
