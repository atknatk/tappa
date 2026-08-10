package handler

import (
	"errors"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The LOCATIONS SECTION — M6-06 PHASE A: the venues a business operates and the
// departments inside them. Its writes are locationactions.go, and the plaque half
// of the same section is plaques.go / plaqueactions.go (M6-06 phase B).
//
// ⚠️ THIS LINE SAID "its TWO writes" until phase B; the section now mounts seven,
// and the number is dropped rather than corrected — TestPlaqueWrites_AreOnTheWRITINGChain
// walks the real router for the set, so nothing has to be counted here.
//
// 🔴 THIS SECTION EDITS THE EVIDENCE THE DECISION ENGINE JUDGES TAPS ON, WHICH IS
// WHAT MAKES IT DIFFERENT FROM EVERY OTHER PANEL SCREEN. The employees section
// changes what happens to ONE PERSON; a venue's static_ips is the IP half of proof
// of place (CLAUDE.md §5 row 6, 50 of the 100 trust points) and its GPS is the
// backup half. Emptying both does not fail, does not error and shows nothing red —
// it silently moves every future tap at that venue onto §5 row 7, which records
// the tap as `flag` and puts it in the approval queue. So this screen SAYS THAT,
// on the venue itself and beside the field, and internal/domain/tenant/venue.go
// writes the before/after into audit_log inside the write's own transaction.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), which the
// Protect chain resolved from a signed session cookie against the database. What a
// client contributes is a venue id or a department id — narrowing values inside
// that tenant, because the tenant predicate they are ANDed with is not one the
// client supplied. A foreign id produces no row rather than a foreign write.
//
// 🔴 §4.7 — NO SECRET PASSES THROUGH THIS FILE. There is nothing to strip by hand:
// the queries select no key, no token and no hash, and neither the domain types nor
// the view models have a field one could travel in. wifi_ssid is NOT a secret and
// NOT evidence — migration 00010 argues both at length — it is the network name an
// employee is asked to join during activation, and "" means there is none.
//
// 🔴 §4.6 — A FAILED READ IS AN ERROR, NEVER AN EMPTY LIST. "This business has no
// venues" is a claim, and a timeout is not evidence for it. A failed read renders a
// problem page; the empty state is gated on having asked.
//
// 🔴 IT LOADS NO SCRIPT. PanelShell has no script slot (user decision, 2026-08-06),
// so this section could not load one without naming a different component on
// purpose. Everything here is a plain form and a plain link, which keeps the
// panel's widened Content-Security-Policy pinned to exactly ONE url — the
// cardinality TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty holds. The
// one place that costs something is the static-IP list, which is a TEXTAREA (one
// range per line) rather than a scripted repeater; the trade is stated at
// parseRanges.

// locationsHref is the section's own URL, READ FROM THE SECTION TABLE rather than
// written out again — the rule employeesHref and reviewHref already follow. If the
// section ever moves, its forms and links move with it; if the tab is removed
// altogether this panics at startup rather than silently building links to nowhere.
var locationsHref = mustSectionHref(pages.TabLocations)

// The venue and department POST routes, built from the section's own URL. The
// plaque half declares its own in plaques.go; every one of them is walked out of the
// real router by TestPlaqueWrites_AreOnTheWRITINGChain.
var (
	venueSaveHref        = locationsHref + "/venue"
	departmentSaveHref   = locationsHref + "/department"
	venueDeleteHref      = locationsHref + "/venue/delete"
	departmentDeleteHref = locationsHref + "/department/delete"
)

// The closed vocabularies this section's query string may echo. A reflected
// parameter is a value somebody can put anything into, and the answer is not to
// escape it more carefully but to have nothing to escape (oneOfWords, review.go).
//
// ⚠️ THE VALIDATION FAILURES ARE NOT IN THESE LISTS, DELIBERATELY. A rejected form
// re-renders with the manager's own values and a sentence naming the field (see
// saveVenue); it does not redirect. Putting "bad-gps" in the URL would cost the
// manager everything they typed into the other seven fields, which on this form is
// the difference between a correction and a re-entry.
//
// 🔴 AND EVERY WORD HERE IS ONE THE SERVER ACTUALLY EMITS, WHICH IS A PROPERTY WITH
// A TEST (TestVenueVocabulary_HoldsOnlyWordsTheServerEmits) BECAUSE IT WAS ONCE
// FALSE. A "venue-unavailable" word sat in this list that no code path ever
// redirected with — a failed read renders a problem page rather than redirecting —
// so the ONLY way to reach it was to hand-edit the URL, and it then printed "We
// could not load your venues" over a page that had just loaded them. An unreachable
// word in a closed vocabulary is not dead code: it is a sentence about our own
// system that only a stranger can trigger and that is false whenever they do. That
// is the M6-04 defect (a URL claiming something that never happened) in miniature.
var (
	// 🔴 THE TWO REMOVAL WORDS ARE THE ONLY ONES THAT ARE NOT SELF-BACKING, AND THEY
	// CARRY A SEPARATE OBLIGATION. Every other word here survives a replayed URL
	// because the row it describes is rendered directly beneath it; a DELETED row
	// cannot be. These two were briefly shipped unbacked and an audit measured the
	// result: GET ?done=venue-deleted, in a browser that had never posted anything,
	// rendered "Venue removed — The venue has been removed."
	//
	// They are back because the acknowledgement is now VERIFIED (user decision,
	// 2026-08-09, reading C'): the section reads the actor's own audit entry for the
	// id in the address, in the same request, and prints nothing if it is not there.
	// removalReceipt below is the whole of it — and venueRemovalWords is what marks
	// these two as needing that check, so a word added later does not inherit the
	// exemption by sitting in the same slice.
	// 🔴 THE PLAQUE WORDS CARRY THE SAME OBLIGATION AS THE REMOVAL ONES AND ARE
	// MARKED SEPARATELY (plaqueActWords, plaques.go). They announce an EVENT — "this
	// was replaced" — which the rows beneath cannot back on their own, so each is
	// verified against the audit action ITS OWN act writes before a word is printed.
	venueDoneWords = []string{"venue-saved", "venue-added", "department-saved",
		"department-added", "venue-deleted", "department-deleted",
		"plaque-mounted", "plaque-replaced", "plaque-unmounted"}

	// venueRemovalWords are the words that MUST be verified against the trail before
	// they are rendered, and the audit action each one claims.
	venueRemovalWords = map[string]string{
		"venue-deleted":      tenant.ActionLocationDeleted,
		"department-deleted": tenant.ActionDepartmentDeleted,
	}
	venueProblemWords = []string{
		// "unreadable" — the REQUEST could not be read (an oversized body, an id that
		// is not a uuid). Nothing was looked up, so nothing is claimed about anything.
		"unreadable",
		// "unknown-venue" / "unknown-department" — no such row in this business. The
		// two are separate words because they send a manager to different places.
		"unknown-venue", "unknown-department",
		// "venue-in-use" / "department-in-use" — the row was still referenced when the
		// DELETE ran. It is a separate word from "unknown-*" because the row EXISTS and
		// nothing is wrong with the request: somebody was placed here between the screen
		// counting zero and the press landing. §4.6 asks the manager be told which.
		"venue-in-use", "department-in-use",
		// "not-permitted" — the admin is not an owner. It is a SEPARATE word from every
		// refusal above because nothing is wrong with the request or the row: the act
		// itself is reserved (user decision, 2026-08-09).
		"not-permitted",
		// "confirm-required" / "confirm-stale" — the same two words the roster's
		// deactivation uses, because it is the SAME mechanism (deactivateconfirm.go)
		// rather than a second one invented for this screen.
		"confirm-required", "confirm-stale",
		// The plaque refusals (M6-06 phase B). Each is a DIFFERENT thing for a manager
		// to do about it, which is why each gets its own word rather than one shared one:
		//
		//	"unknown-plaque"     no plaque with that id in this business
		//	"plaque-not-stock"   the spare was mounted between the screen and the press
		//	                     — the race, and neither half of the replacement happened
		//	"plaque-not-active"  the plaque being replaced is not on a wall any more
		//	"same-plaque"        a plaque cannot replace itself
		//	"plaque-frozen"      the database refuses to write this row at all. A
		//	                     DEVELOPMENT-ONLY state: 00013's canonical-uid CHECK is
		//	                     NOT VALID, so the 18 010 pre-existing lower-case rows
		//	                     are listed but frozen. Without a word for it a manager
		//	                     would meet a 500 on a row they can see.
		"unknown-plaque", "plaque-not-stock", "plaque-not-active", "same-plaque",
		"plaque-frozen",
		// "plaque-no-wall" — the plaque a manager tried to take DOWN is not on a wall.
		// A separate word from "plaque-not-active" because it sends them somewhere
		// else: that one is about the plaque a replacement would retire.
		"plaque-no-wall",
	}
)

// maxVenueBody is the ceiling on a save POST's body.
//
// 🔴 A BARE ParseForm INHERITS net/http's 10 MB. These routes sit in front of a
// database write and a valid session may spend adminSessionLimit requests per
// window, so the unbounded shape is 300 × 10 MB of allocation per session per ten
// minutes from somebody who has merely signed in. The real body is a name, at most
// maxStaticRanges address ranges, two coordinates, two clock times and an SSID;
// 16 KiB is already generous, and it is double maxEmployeeActionBody because the
// static-IP textarea is the one genuinely multi-line field in the panel.
const maxVenueBody = 16 << 10

// maxStaticRanges bounds how many address ranges one venue may carry.
//
// 🔴 THE COLUMN HAS NO BOUND — static_ips is cidr[] with no length limit — so
// without this a single form post could store an array of arbitrary size, and
// EVERY TAP AT THAT VENUE would then evaluate `@src <<= ANY(...)` against it
// (db/queries/locations.sql, GetLocationByIP). That is the one field on this screen
// where a large value is paid for on the DECISION path rather than only on the
// screen that stores it, which is why it is bounded here rather than left to the
// body limit above.
//
// 32 IS FAR ABOVE ANY REAL VENUE, AND THAT IS ARGUED FROM WHAT A VENUE NEEDS RATHER
// THAN FROM A MEASUREMENT. A venue needs one range per network it presents to staff —
// a single office subnet, or a handful for a site with separate uplinks.
//
// ⚠️ AN EARLIER VERSION OF THIS COMMENT CITED "the largest array in the table holds 2
// entries" AND THAT NUMBER WAS ALREADY FALSE WHEN IT WAS WRITTEN DOWN. Re-measured on
// 2026-08-08 the largest holds 10, and the row carrying it is named "Probe range" — an
// AUDITOR'S OWN FIXTURE. The development database is written by this repository's test
// suite, so any extreme measured in it converges on whatever the tests exercise rather
// than on what customers do. The SHAPE is still worth stating because it is stable:
// over 99.9% of rows carry zero or one range. Distribution when last looked at:
// 1 range x 100 622 · 0 ranges x 21 509 · 2 ranges x 4 · 10 ranges x 1.
const maxStaticRanges = 32

// maxSSIDBytes is migration 00010's own CHECK (octet_length 1..32) restated at the
// boundary, so an over-long SSID is a sentence rather than a 23514 the manager sees
// as a 500. IT IS BYTES, NOT RUNES, because the constraint is — an SSID is an
// 802.11 field with a 32-OCTET limit, and a Maltese ħ costs two of them.
const maxSSIDBytes = 32

// locationsSection renders the venues, the departments, the plaques, and at most
// one edit card.
func (a *AdminAuth) locationsSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)

	screen, err := a.venues.Screen(r.Context(), id.TenantID())
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to an empty list — "this
		// business has no venues" is a claim about a business, and a timeout is not
		// evidence for it.
		a.log.Error("panel: could not read the venues", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// 🔴 THE SAME RULE FOR THE PLAQUES, AND IT IS A SEPARATE READ ON PURPOSE (§4.6).
	// "This business has no plaques" is as much a claim as "no venues", and the two
	// reads fail independently — folding a plaque timeout into an empty plaque list
	// would tell a manager their stock is empty on the strength of a database that
	// did not answer.
	plaques, err := a.locationsPlaques(r)
	if err != nil {
		a.log.Error("panel: could not read the plaques", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	// 🔴 THE SHELL IS BUILT IN ONE PLACE, WHICH IS A REPAIR RATHER THAN A TIDY-UP.
	// This function and locationsShell used to fill the same view model separately,
	// and the copies had already drifted: the re-render path set no count at all, so
	// a capped list there read "Only the first  venues are shown". One builder is
	// what makes the two paths one fact.
	v := a.locationsShell(r, screen, plaques)
	// Problem is a CLOSED VOCABULARY read off the query string, so a hand-edited URL
	// can turn one of a fixed set of sentences on and cannot put anything in it.
	v.Problem = oneOfWords(r.URL.Query().Get("problem"), venueProblemWords...)
	v.Done = oneOfWords(r.URL.Query().Get("done"), venueDoneWords...)
	// 🔴 VERIFIED BEFORE IT IS SAID. For the two removal words this is the only thing
	// that permits a sentence; for every other word it costs nothing and returns nil.
	v.RemovalReceipt = a.removalReceipt(r, v.Done)
	// 🔴 AND THE SAME FOR THE PLAQUE WORDS, against the action each act writes.
	v.PlaqueReceipt = a.plaqueReceipt(r, v.Done)
	// 🔴 AN UNVERIFIED REMOVAL WORD IS DROPPED ENTIRELY, NOT MERELY LEFT UNPRINTED,
	// AND THAT IS A §4.6 REPAIR RATHER THAN TIDYING. A removal word survives in v.Done
	// even when the trail has nothing behind it — and v.Done being non-empty was
	// enough to SKIP the unresolved-card check below. Measured: with the two combined,
	//
	//	?venue=<foreign id>                              -> "We could not find that venue"
	//	?done=venue-deleted&id=<x>&venue=<foreign id>     -> NO SENTENCE AT ALL
	//
	// so an unbacked word silenced a refusal the section is required to make. The word
	// has no meaning once the receipt is nil — nothing will render it — so clearing it
	// restores the only state the rest of this function should see.
	//
	// ⚠️ ONLY THE REMOVAL WORDS ARE CLEARED. A first version of this dropped v.Done
	// whenever the receipt was nil, which is nil for EVERY ordinary word — so
	// ?done=venue-saved lost its banner too. The anti-vacuity assertion in
	// TestLocationsSection_NeverClaimsARemovalItCannotSee caught it on the first run,
	// which is exactly what that assertion is for.
	if _, isRemoval := venueRemovalWords[v.Done]; isRemoval && v.RemovalReceipt == nil {
		v.Done = ""
	}
	// 🔴 AND AN UNVERIFIED PLAQUE WORD IS DROPPED FOR THE SAME REASON, not merely
	// left unprinted. A word that survives in v.Done is enough to SKIP the
	// unresolved-card check below, so an unbacked one would silence a refusal the
	// section is required to make — the §4.6 repair the removal words needed.
	if _, isAct := plaqueActWords[v.Done]; isAct && v.PlaqueReceipt == nil {
		v.Done = ""
	}

	var formErr error
	v.VenueForm, v.DepartmentForm, formErr = a.venueForms(w, r, screen)
	if formErr != nil {
		// §4.6: OUR read failed. "We could not find that venue" would be a claim about
		// the business built on a database that did not answer.
		a.log.Error("panel: could not resolve the requested card", "err", formErr)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// 🔴 THE FORM'S TARGET IS SET IN ONE PLACE, FROM THE SAME CONSTANT THE ROUTER
	// REGISTERS. A form whose action is written out again is a form that can post to
	// a URL nothing mounts — the failure mustSectionHref exists to make impossible
	// for the section itself, applied to every write route it mounts.
	bindFormTargets(&v)
	// 🔴 THE PLAQUE CARD IS THE THIRD AND IT OPENS ONLY WHEN NEITHER OF THE OTHER TWO
	// DID. At most one card is ever open — the rule venueForms already holds for its
	// pair — so the precedence (venue, then department, then plaque) is written here
	// as a condition rather than left to emerge from the order of assignments.
	if v.VenueForm == nil && v.DepartmentForm == nil {
		card, err := a.plaqueCard(w, r, plaques, screen, plaques.Zone)
		if err != nil {
			// §4.6: OUR read failed. "We could not find that plaque" would be a claim
			// about the business built on a database that did not answer.
			a.log.Error("panel: could not resolve the requested plaque", "err", err)
			a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
			return
		}
		v.PlaqueForm = card
	}
	// §4.6: a ?venue=, ?department= or ?plaque= that resolved to nothing is ANSWERED,
	// not silently ignored. It is set here rather than in venueForms so that it
	// cannot overwrite the outcome of an action the manager just took.
	if v.Problem == "" && v.Done == "" {
		v.Problem = unresolvedCardProblem(r, v)
	}

	a.render(w, r, http.StatusOK, pages.AdminLocations(v))
}

// bindFormTargets fills in whichever card is open with the two values every form
// needs and no card should carry its own copy of: where it posts, and where Cancel
// goes.
func bindFormTargets(v *pages.LocationsView) {
	if v.VenueForm != nil {
		v.VenueForm.Action = v.VenueAction
		v.VenueForm.CloseHref = v.CloseHref
	}
	if v.DepartmentForm != nil {
		v.DepartmentForm.Action = v.DepartmentAction
		v.DepartmentForm.CloseHref = v.CloseHref
	}
}

// unresolvedCardProblem names the sentence for a ?venue= or ?department= that
// pointed at nothing.
//
// 🔴 A LINK THAT RESOLVES TO NOBODY IS ANSWERED, NOT IGNORED (§4.6) — the rule
// rosterActions follows. A stale bookmark, a hand-edited URL or another tenant's id
// all land here, and the difference between "no card" and "no card plus a sentence"
// is whether the manager knows why the screen did not do what they asked.
//
// THE ONE CASE THAT IS NOT AN ERROR is ?department=new with no venues yet: the form
// is legitimately withheld because a department must sit in a venue, and the empty
// state below already says so in words. Reporting it as "unknown" would blame the
// manager for a state the screen explains.
func unresolvedCardProblem(r *http.Request, v pages.LocationsView) string {
	q := r.URL.Query()
	venue := strings.TrimSpace(q.Get("venue"))
	department := strings.TrimSpace(q.Get("department"))

	// 🔴 ?venue= WINS AND ?department= IS THEN NOT AN UNANSWERED REQUEST. venueForms
	// opens at most ONE card and takes the venue first, so with both present the
	// department was never looked for — and complaining that it could not be FOUND is
	// a claim about a search that never ran. Measured: ?venue=new&department=new
	// rendered the Add-a-venue form under "That department is not one of this
	// business's", with no department in the request to be missing.
	if venue != "" {
		if v.VenueForm != nil {
			return ""
		}
		// 🔴 "NOT A UUID" AND "NOT THIS BUSINESS'S" ARE DIFFERENT, AND THE SECTION'S OWN
		// VOCABULARY ALREADY SEPARATES THEM. "unreadable" is defined as "nothing was
		// looked up", which is exactly what happens when the id does not parse: no query
		// runs, so the product cannot know whose venue it would have been. Saying "not
		// one of this business's" there was a claim about a lookup that never happened —
		// the same shape as the two findings above it.
		if !isUUID(venue) && venue != "new" {
			return "unreadable"
		}
		return "unknown-venue"
	}

	// 🔴 ?plaque= IS ANSWERED LAST AND ONLY WHEN IT WAS ACTUALLY LOOKED FOR. The
	// section opens at most one card and takes venue, then department, then plaque —
	// so with a venue or a department in the address the plaque was never searched
	// for, and complaining that it could not be FOUND would be a claim about a search
	// that never ran. That is the exact defect the ?venue=new&department=new branch
	// above records.
	if department == "" {
		if plaque := strings.TrimSpace(q.Get("plaque")); plaque != "" && v.PlaqueForm == nil {
			if _, err := tenant.PlaqueRef(plaque); err != nil {
				// Not a plaque id at all: nothing was looked up, so the product cannot know
				// whose plaque it would have been.
				return "unreadable"
			}
			return "unknown-plaque"
		}
	}

	if department != "" && v.DepartmentForm == nil {
		// The one case that is NOT an error: "add a department" with no venues yet. The
		// form is legitimately withheld and the empty state explains it in words, so
		// reporting it would blame the manager for a state the screen describes.
		if department == "new" {
			if len(v.Venues) == 0 {
				return ""
			}
			return "unknown-department"
		}
		if !isUUID(department) {
			return "unreadable"
		}
		return "unknown-department"
	}
	return ""
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// venueForms builds at most one edit card from ?venue= or ?department=.
//
// 🔴 A LINK THAT RESOLVES TO NOTHING IS ANSWERED, NOT IGNORED (§4.6) — the rule
// rosterActions follows. A stale bookmark, a hand-edited URL or another tenant's id
// all land here, and the difference between "no card" and "no card plus a sentence"
// is whether the manager knows why the screen did not do what they asked.
//
// THE LIST THIS REQUEST ALREADY READ IS TRIED FIRST, AND IS NOT THE ONLY ANSWER.
// Scanning the slice costs nothing for the overwhelmingly common case — the whole
// estate is on the page — but the slice is CAPPED (venuePageLimit), and a row past
// the cap is not a row that stopped existing.
//
// 🔴 THAT DISTINCTION WAS ONCE COLLAPSED AND THE PRODUCT LIED ABOUT IT. With only the
// scan, ?venue=<id> for a venue past the ceiling rendered "That venue is not one of
// this business's. Nothing was changed." — false in every clause: the row IS the
// business's, it is in the database, and RLS admits it. The row was unreachable AND
// misdescribed, which is worse than unreachable. So a miss now falls back to the
// tenant-scoped by-id read that already exists (GetPanelLocation /
// GetPanelDepartment, used until now only on the write path), and the two outcomes
// are kept apart:
//
//	found by id            the row is past the cap -> OPEN THE CARD. Nothing is lost.
//	ErrUnknownVenue        the id is another business's or nothing at all -> the
//	                       "not one of this business's" sentence, which is now TRUE
//	                       whenever it appears.
//
// ⚠️ THE FALLBACK IS NOT REACHED TODAY, and saying so is the honest ordering rather
// than a reason to skip it. Measured (2026-08-08): the largest single tenant has 9
// locations and 5 departments against a ceiling of 200.
//
// A REAL READ FAILURE IS AN ERROR, NOT A SENTENCE (§4.6). It is returned so the
// caller renders a problem page: "we could not find that venue" would be a claim
// about the business built on a database that did not answer.
func (a *AdminAuth) venueForms(w http.ResponseWriter, r *http.Request, screen tenant.VenueScreen) (*components.VenueFormView, *components.DepartmentFormView, error) {
	q := r.URL.Query()
	id := httpx.AdminOf(r)

	if raw := strings.TrimSpace(q.Get("venue")); raw != "" {
		if raw == "new" {
			return blankVenueForm(), nil, nil
		}
		venueID, err := uuid.Parse(raw)
		if err != nil {
			// Not a uuid: nothing was looked up, and the section's own sentence covers it.
			return nil, nil, nil
		}
		ven, found, err := a.venueByID(r, id.TenantID(), venueID, screen)
		switch {
		case err != nil:
			return nil, nil, err
		case !found:
			return nil, nil, nil
		}
		f := venueFormOf(ven)
		if err := a.attachVenueRemoval(w, r, &f, ven); err != nil {
			return nil, nil, err
		}
		return &f, nil, nil
	}

	if raw := strings.TrimSpace(q.Get("department")); raw != "" {
		if raw == "new" {
			venueOptions := venueOptionViews(screen.Venues)
			if len(venueOptions) == 0 {
				// A department must sit in a venue (departments.location_id is NOT NULL), so
				// with no venues there is nothing to offer. Rendering the form anyway would
				// offer a submission the server can only refuse.
				return nil, nil, nil
			}
			f := blankDepartmentForm(venueOptions)
			return nil, &f, nil
		}
		departmentID, err := uuid.Parse(raw)
		if err != nil {
			return nil, nil, nil
		}
		// 🔴 AN EDIT NEEDS NO VENUE OPTIONS, and requiring them was the second half of
		// the same bug. UpdateDepartment cannot move a department, so the form renders
		// the venue's NAME rather than a dropdown — yet the old code bailed out when the
		// (also capped) options list was empty. An edit therefore depended on a control
		// it does not render.
		dep, found, err := a.departmentByID(r, id.TenantID(), departmentID, screen)
		switch {
		case err != nil:
			return nil, nil, err
		case !found:
			return nil, nil, nil
		}
		f := departmentFormOf(dep, nil)
		if err := a.attachDepartmentRemoval(w, r, &f, dep); err != nil {
			return nil, nil, err
		}
		return nil, &f, nil
	}
	return nil, nil, nil
}

// --- the boundary (§7): everything a client sends is validated HERE ------------

// venueSubmission is one posted venue form, already validated.
type venueSubmission struct {
	VenueID   uuid.UUID
	Name      string
	StaticIPs []netip.Prefix
	GPS       *tenant.GPS
	Shift     *tenant.Shift
	WiFiSSID  string
}

// departmentSubmission is one posted department form, already validated.
type departmentSubmission struct {
	DepartmentID uuid.UUID
	LocationID   uuid.UUID
	Name         string
	Shift        *tenant.Shift
}

// parseVenueForm validates a posted venue (§7: the boundary validates, the domain
// sees data that is already good). It answers the submission, or the field name and
// the sentence to show beside it.
func parseVenueForm(r *http.Request) (venueSubmission, string, string) {
	var s venueSubmission

	if raw := strings.TrimSpace(r.PostFormValue("id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return s, "", "We could not read which venue that was, so nothing was saved."
		}
		s.VenueID = id
	}

	name, msg := parseName(r.PostFormValue("name"), "venue")
	if msg != "" {
		return s, "name", msg
	}
	s.Name = name

	ips, msg := parseRanges(r.PostFormValue("static_ips"))
	if msg != "" {
		return s, "static_ips", msg
	}
	s.StaticIPs = ips

	gps, msg := parseGPS(r.PostFormValue("gps_lat"), r.PostFormValue("gps_lng"))
	if msg != "" {
		return s, "gps", msg
	}
	s.GPS = gps

	shift, msg := parseShift(r.PostFormValue("shift_start"), r.PostFormValue("shift_end"),
		r.PostFormValue("overnight") != "")
	if msg != "" {
		return s, "shift", msg
	}
	s.Shift = shift

	ssid, msg := parseSSID(r.PostFormValue("wifi_ssid"))
	if msg != "" {
		return s, "wifi_ssid", msg
	}
	s.WiFiSSID = ssid

	return s, "", ""
}

// parseDepartmentForm validates a posted department.
func parseDepartmentForm(r *http.Request) (departmentSubmission, string, string) {
	var s departmentSubmission

	if raw := strings.TrimSpace(r.PostFormValue("id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return s, "", "We could not read which department that was, so nothing was saved."
		}
		s.DepartmentID = id
	}
	// 🔴 THE VENUE IS ONLY READ WHEN CREATING. UpdateDepartment does not name
	// location_id (db/queries/departments.sql says why: moving a department between
	// venues would leave every employee in it holding a pair MoveEmployee refuses),
	// so accepting one here on an edit would be accepting a field the server ignores
	// — which is the "screen offers what the server does not do" defect.
	if s.DepartmentID == uuid.Nil {
		raw := strings.TrimSpace(r.PostFormValue("location_id"))
		id, err := uuid.Parse(raw)
		if err != nil {
			return s, "location_id", "Pick the venue this department belongs to."
		}
		s.LocationID = id
	}

	name, msg := parseName(r.PostFormValue("name"), "department")
	if msg != "" {
		return s, "name", msg
	}
	s.Name = name

	shift, msg := parseShift(r.PostFormValue("shift_start"), r.PostFormValue("shift_end"),
		r.PostFormValue("overnight") != "")
	if msg != "" {
		return s, "shift", msg
	}
	s.Shift = shift

	return s, "", ""
}

// parseName cleans and BOUNDS a name that is about to be stored.
//
// 🔴 IT REFUSES RATHER THAN TRUNCATES, which is the opposite of what the roster's
// name FILTER does and the difference is the point: cutting a search term only
// widens a search, whereas cutting a name somebody is SAVING stores a name they
// did not type and says nothing about it. internal/domain/tenant enforces the same
// bound again — this is the sentence, that is the guarantee.
//
// NUL IS REMOVED FIRST. PostgreSQL text cannot hold a zero byte: it answers
// "invalid byte sequence for encoding UTF8" and the request becomes a 500, which
// was measured on the roster's filter box and is the same driver here.
func parseName(raw, what string) (string, string) {
	s := strings.ToValidUTF8(strings.ReplaceAll(raw, "\x00", ""), "")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "Give the " + what + " a name."
	}
	// COUNTED IN RUNES, NOT BYTES. Maltese names carry ċ ġ ħ ż and the brand ships a
	// latin-ext subset precisely because these names are not ASCII, so a byte bound
	// would refuse ordinary names at four fifths of the stated length.
	if n := utf8.RuneCountInString(s); n > tenant.MaxVenueNameRunes {
		return "", "That name is " + strconv.Itoa(n) + " characters. Keep it to " +
			strconv.Itoa(tenant.MaxVenueNameRunes) + " or fewer."
	}
	return s, ""
}

// parseRanges reads the static IP list out of the textarea.
//
// 🔴 THIS IS THE MOST CONSEQUENTIAL FIELD ON THE SCREEN AND THE ONLY ONE WHOSE
// EMPTY STATE CHANGES A VERDICT. An empty list is a legitimate configuration — it
// is migration 00002's own '{}' and it means "no IP evidence" — but it moves every
// later tap at this venue onto the GPS path, and a tap with no GPS is recorded as
// `flag` (§5 row 7). The screen says so beside the field rather than here.
//
// 🔴 A BARE ADDRESS BECOMES A /32 (OR /128) HERE, BECAUSE netip AND POSTGRES
// DISAGREE ABOUT WHAT ONE IS. Measured (2026-08-08): netip.ParsePrefix("192.168.1.5")
// FAILS with "no '/'", while Postgres accepts '192.168.1.5'::cidr and widens it to
// 192.168.1.5/32. A manager typing a single office address is the ordinary case, so
// the bare form is accepted and given its full-length mask explicitly.
//
// 🔴 AND A PREFIX WITH HOST BITS SET IS REFUSED HERE RATHER THAN AT THE DRIVER,
// WHICH IS THE TRAP THIS FUNCTION EXISTS FOR. Measured, both halves: Go PARSES
// "192.168.1.5/24" happily (netip.ParsePrefix returns 192.168.1.5/24, whose
// Masked() is 192.168.1.0/24 — they differ), and Postgres REFUSES it with
// `ERROR: invalid cidr value ... Value has bits set to right of mask`. Without the
// equality check below, a plausible typo would therefore reach the database and come
// back as a 500 with a driver string in it. The message names the masked form so the
// manager can see what they probably meant.
//
// ONE RANGE PER LINE, AND COMMAS TOO. A textarea is what this section can offer
// without a script; accepting commas as well costs one Fields call and saves the
// manager who pastes a comma-separated list from a network document.
func parseRanges(raw string) ([]netip.Prefix, string) {
	fields := strings.FieldsFunc(strings.ToValidUTF8(raw, ""), func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	out := make([]netip.Prefix, 0, len(fields))
	seen := map[netip.Prefix]bool{}
	for _, f := range fields {
		if len(out) >= maxStaticRanges {
			return nil, "That is more than " + strconv.Itoa(maxStaticRanges) +
				" address ranges. A venue needs one per network its staff connect to."
		}
		p, msg := parseRange(f)
		if msg != "" {
			return nil, msg
		}
		// A DUPLICATE IS DROPPED RATHER THAN REFUSED. Two identical ranges match
		// exactly what one matches, so refusing would be pedantry about a paste; and
		// storing both would grow an array every tap is evaluated against.
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, ""
}

func parseRange(f string) (netip.Prefix, string) {
	if !strings.Contains(f, "/") {
		addr, err := netip.ParseAddr(f)
		if err != nil {
			return netip.Prefix{}, quoteless(f) + " is not an address or a range. " +
				"Use something like 192.168.1.0/24, or a single address like 192.168.1.5."
		}
		// A BARE ADDRESS IS ITS OWN /32 OR /128 — Postgres would do this itself, and
		// doing it here means the value stored is the value this code decided on
		// rather than one two layers negotiated.
		return netip.PrefixFrom(addr, addr.BitLen()), ""
	}
	p, err := netip.ParsePrefix(f)
	if err != nil {
		return netip.Prefix{}, quoteless(f) + " is not an address range. " +
			"Use something like 192.168.1.0/24."
	}
	if p != p.Masked() {
		return netip.Prefix{}, quoteless(f) + " has host bits set, which a range cannot have. " +
			"Did you mean " + p.Masked().String() + "?"
	}
	return p, ""
}

// quoteless renders the offending token back to the manager WITHOUT letting it
// grow the message without bound.
//
// 🔴 IT IS ECHOED INTO THE PAGE, NOT INTO THE URL. templ escapes it on the way out,
// and a validation failure re-renders rather than redirecting, so this never
// becomes a reflected query parameter somebody can send to a colleague. The bound
// is what stops a 16 KiB body turning into a 16 KiB sentence.
func quoteless(s string) string {
	s = strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "")
	const max = 40
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max]) + "…"
	}
	return "“" + s + "”"
}

// parseGPS reads the coordinate pair.
//
// 🔴 BOTH OR NEITHER, AND EMPTY MEANS NULL — NEVER 0,0. locations_gps_pair enforces
// the pair, so a half-filled one would be a 23514 the manager sees as a 500; and
// 0,0 is a real point in the Gulf of Guinea that PASSES that CHECK, so folding an
// empty box into a zero would register every venue 5 000 km from itself and quietly
// cost the GPS half of proof of place at all of them. §5 row 6.
func parseGPS(rawLat, rawLng string) (*tenant.GPS, string) {
	lat := strings.TrimSpace(rawLat)
	lng := strings.TrimSpace(rawLng)
	switch {
	case lat == "" && lng == "":
		return nil, ""
	case lat == "" || lng == "":
		return nil, "A coordinate needs both a latitude and a longitude. " +
			"Leave both empty if this venue has no registered position."
	}
	latF, msg := parseCoordinate(lat, 90, "Latitude", "-90 and 90")
	if msg != "" {
		return nil, msg
	}
	lngF, msg := parseCoordinate(lng, 180, "Longitude", "-180 and 180")
	if msg != "" {
		return nil, msg
	}
	return &tenant.GPS{Lat: latF, Lng: lngF}, ""
}

// parseCoordinate reads ONE coordinate and bounds it.
//
// 🔴 TWO DEFENCES STAND BETWEEN NaN AND THE COLUMN, AND THEY ARE COMPLEMENTARY
// RATHER THAN REDUNDANT — a claim an audit had to correct, so it is written out.
// The original guard was `f < -limit || f > limit`, which reads as "outside the
// range" and is not: EVERY comparison against NaN is false, so NaN satisfied neither
// half and walked through. `strconv.ParseFloat("NaN", 64)` SUCCEEDS, the value then
// reached numericFrom → FormatFloat → "NaN", which pgtype.Numeric accepts, and
// Postgres refused it at locations_gps_lat_check with a 23514 — so a signed-in
// manager typing NaN got the "panel unavailable" page and lost the other seven fields
// they had typed.
//
// TODAY plainDecimalRE REJECTS "NaN" BEFORE withinRange EVER SEES IT. That means
// reverting withinRange alone does NOT turn the suite red through this function, and
// believing otherwise was the error: a defence whose removal no test notices is not a
// defence. So withinRange is a named function with its OWN test
// (TestWithinRange_IsFalseForEveryIncomparableFloat) which calls it directly, and the
// syntax gate has its own cases here. Each is pinned where it acts.
//
// WHY KEEP BOTH. The regexp is about SHAPE and could reasonably be loosened one day
// (a locale-aware coordinate box is a live question); withinRange is about VALUE and
// must hold whatever shape gets through. Ordering them the other way round would make
// the range check the only guard, which is exactly the arrangement that failed.
//
// 🔴 AND THE SYNTAX IS PLAIN DECIMAL, NOT "WHATEVER ParseFloat EATS". Measured
// (2026-08-08): ParseFloat accepts Go's own float literal grammar, which includes hex
// floats ("0x1p3" -> 8), underscore digit separators ("1_0.5" -> 10.5), exponents,
// and — the one that matters — "1e-400", which UNDERFLOWS TO 0 WITH NO ERROR. That
// last one is this file's own defect wearing a different hat: a manager types
// something meaningless and silently registers the venue at 0,0 in the Gulf of
// Guinea, which is precisely the state the empty-box rule above exists to make
// unreachable. §7 says the domain sees data that is already good, so the boundary
// admits the shape a coordinate actually has and nothing else.
//
// "35." AND ".9" ARE STILL ACCEPTED because they are ordinary partial typing and
// Postgres numeric takes both; what is refused is a syntax nobody means to type.
//
// 🔴 A DECIMAL COMMA IS ACCEPTED, NOT REFUSED (user decision, 2026-08-08). Malta and
// Türkiye write 35,9 and the field carries inputmode="decimal", so a phone offers
// whatever separator the locale has — refusing it made the product reject a correct
// number typed correctly. The Unicode minus U+2212 rides along for the same reason:
// macOS and word processors substitute it for the hyphen silently.
//
// 🔴 WHY THIS IS SAFE HERE AND WOULD NOT BE IN A GENERAL NUMBER FIELD. A comma is
// ambiguous in general — 1,234 is either 1.234 or 1234 — and the decision rests on
// two measurements that close that gap for COORDINATES specifically:
//
//	a latitude or longitude has AT MOST THREE INTEGER DIGITS (|lat| <= 90,
//	|lng| <= 180), so a thousands separator has no legitimate appearance in this
//	field at all; and
//	anything carrying TWO separators still fails the shape below, measured over
//	the whole list: "35,917,270" -> "35.917.270", "1.234,56" -> "1.234.56" and the
//	pasted pair "35,9,14,4" -> "35.9.14.4" are all refused after normalising
//	exactly as they were before it.
//
// So the only inputs whose meaning changes are the ones with a single separator and
// at most three integer digits — which in this field is a decimal point and nothing
// else. TestParseGPS_NormalisesTheSeparatorsAPhoneOffers pins both halves.
//
// 🔴 AND THE FORMAT SENTENCE MUST STAY TRUE. It used to say "no comma", which became
// false the moment a comma was accepted — a message that describes a rule the code no
// longer follows is the same defect class this task has spent four rounds closing.
// What remains refused is a degree sign, a compass letter, DMS, a thousands separator
// and every non-decimal float syntax, so the sentence names those.
func parseCoordinate(s string, limit float64, field, rangeText string) (float64, string) {
	s = coordinateSeparators.Replace(s)
	if !plainDecimalRE.MatchString(s) {
		return 0, field + " must be a plain number, like 35.917270 or 35,917270 — " +
			"no degree sign, compass letter or thousands separator."
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// The shape passed but the value did not fit a float64 — an enormous run of
		// digits. It is a FORMAT problem from the manager's side, not a range one.
		return 0, field + " is not a number we can read. Use something like 35.917270."
	}
	if !withinRange(f, limit) {
		return 0, field + " must be between " + rangeText + "."
	}
	return f, ""
}

// withinRange reports whether f is a REAL number inside ±limit.
//
// 🔴 IT IS WRITTEN AS AN INCLUSION, AND THAT IS THE ENTIRE FUNCTION. The natural
// spelling of "reject what is outside the range" is `f < -limit || f > limit`, which
// silently ADMITS NaN: every comparison against NaN is false, so it satisfies neither
// half. Written as `f >= -limit && f <= limit` the same NaN satisfies neither half of
// the inclusion either — and therefore fails it. No special case, no math.IsNaN, and
// nothing to forget when somebody edits the bound.
func withinRange(f, limit float64) bool {
	return f >= -limit && f <= limit
}

// plainDecimalRE is the shape of a coordinate: an optional sign, then digits with at
// most one decimal point, and at least one digit somewhere. No exponent, no hex, no
// underscores, no "NaN", no "Inf".
//
// [0-9] AND NOT \d, DELIBERATELY. Go's \d is ASCII-only so the two agree today, but
// spelling the range out means a future edit to (?s) flags or a move to a Unicode-
// aware matcher cannot quietly admit full-width or Arabic-Indic digits — which
// strconv.ParseFloat would then refuse anyway, turning a shape refusal into a
// different message for the same input.
var plainDecimalRE = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)

// coordinateSeparators maps what a phone or a word processor produces onto what
// Postgres numeric parses.
//
// 🔴 IT IS APPLIED IN parseCoordinate AND NOWHERE ELSE, WHICH IS THE POINT OF IT
// BEING A PACKAGE-LEVEL VALUE WITH THIS COMMENT RATHER THAN AN INLINE CALL. A comma
// is meaningful in every other field on this form: a venue may be named "Kebab
// Factory, Ltd", an SSID may contain one, parseRanges uses commas as the SEPARATOR
// between address ranges, and a shift is a clock. Normalising anywhere but here would
// silently rewrite a manager's data. TestCoordinateNormalisation_TouchesNoOtherField
// drives the other four and requires their commas to survive.
var coordinateSeparators = strings.NewReplacer(
	",", ".",
	// U+2212 MINUS SIGN. macOS substitution and most word processors produce it in
	// place of the ASCII hyphen, and it is invisible in a form field.
	"−", "-",
)

// parseShift reads the working window.
//
// 🔴 BOTH OR NEITHER, AND EMPTY MEANS NULL — NEVER 00:00. This is the single most
// destructive mistake this file could make. A NULL shift means lateness is NOT
// COMPUTED for that venue or department (§5, M4-05, and db/queries/departments.sql
// says it in its own words); 00:00 is a shift that starts at midnight, so folding an
// empty box into a zero would mark every arrival late, forever, with no error
// anywhere and nothing on any screen to notice. locations_shift_pair refuses a
// HALF-filled pair but cannot refuse a plausible midnight, so this boundary is the
// only guard and TestParseShift_EmptyIsNilNotMidnight is what holds it.
//
// OVERNIGHT WITHOUT A SHIFT IS DROPPED rather than refused: the checkbox is beside
// the two boxes and means "the end is on the next day", which is not a statement
// anybody can make about a shift that does not exist.
func parseShift(rawStart, rawEnd string, overnight bool) (*tenant.Shift, string) {
	start := strings.TrimSpace(rawStart)
	end := strings.TrimSpace(rawEnd)
	switch {
	case start == "" && end == "":
		return nil, ""
	case start == "" || end == "":
		return nil, "A shift needs both a start and an end. " +
			"Leave both empty if lateness should not be measured here."
	}
	s, ok := parseClock(start)
	if !ok {
		return nil, "The shift start must be a time of day, like 08:30."
	}
	e, ok := parseClock(end)
	if !ok {
		return nil, "The shift end must be a time of day, like 17:00."
	}
	return &tenant.Shift{Start: s, End: e, Overnight: overnight}, ""
}

// parseClock reads an <input type="time"> value as an offset from midnight.
//
// IT ACCEPTS BOTH "15:04" AND "15:04:05" because that is what browsers send: the
// seconds field appears when a step attribute asks for it, and a value that is
// valid HTML must not be a validation failure here.
func parseClock(s string) (time.Duration, bool) {
	for _, layout := range []string{"15:04", "15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Duration(t.Hour())*time.Hour +
				time.Duration(t.Minute())*time.Minute +
				time.Duration(t.Second())*time.Second, true
		}
	}
	return 0, false
}

// parseSSID reads the network name.
//
// 🔴 IT IS DISPLAY DATA AND NOT EVIDENCE. Migration 00010 argues this at length and
// it bears restating where the field is edited: a client-reported SSID is a string
// the client chose, so treating it as proof of place would add a fourth "evidence"
// anybody can type. Nothing on the tap path reads it. Empty is NOT an error — it
// means "this venue has no network to show" and the activation page skips the step.
//
// THE BOUND IS IN BYTES BECAUSE THE CONSTRAINT IS (00010: octet_length 1..32, which
// is the 802.11 SSID field's own limit). Checking runes here would let a Maltese
// name through that the column then refuses as a 23514 — a 500 for a value the
// manager was told was fine.
func parseSSID(raw string) (string, string) {
	s := strings.ToValidUTF8(strings.ReplaceAll(raw, "\x00", ""), "")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if len(s) > maxSSIDBytes {
		return "", "A network name can be at most " + strconv.Itoa(maxSSIDBytes) +
			" bytes. Accented characters count as two."
	}
	return s, ""
}

// --- view adapters -------------------------------------------------------------

func venueRowViews(screen tenant.VenueScreen) ([]components.VenueRowView, []components.DepartmentRowView) {
	byVenue := map[uuid.UUID][]string{}
	for _, d := range screen.Departments {
		byVenue[d.LocationID] = append(byVenue[d.LocationID], d.Name)
	}
	venues := make([]components.VenueRowView, 0, len(screen.Venues))
	for _, v := range screen.Venues {
		venues = append(venues, components.VenueRowView{
			Name:   v.Name,
			Ranges: ipTexts(v.StaticIPs),
			GPS:    gpsText(v.GPS),
			Shift:  shiftText(v.Shift),
			WiFi:   v.WiFiSSID,
			// 🔴 THE SENTENCE, NOT THE COUNT. §5's rule is what a manager needs to read
			// off this card: with neither IP nor GPS every tap here is recorded and sent
			// to the approval queue. The card does not predict how many taps that will
			// be — that would be a claim about the future — it states the rule.
			NoProofOfPlace: !v.HasProofOfPlace(),
			Departments:    byVenue[v.ID],
			// 🔴 THE COUNT COMES FROM THE DATABASE AND THE NAMES COME FROM A CAPPED
			// SLICE, WHICH IS WHY BOTH ARE PASSED. ListPanelLocations counts the
			// departments in each venue with its own tenant-scoped subquery, so
			// DepartmentCount is the truth; `Departments` above is assembled from
			// screen.Departments, which venuePageLimit may have cut. Until now the count
			// was measured, mapped onto the domain type, asserted in a test — and never
			// rendered, while the possibly-short list WAS. Showing the list without the
			// count is how a venue silently appears to have fewer departments than it
			// has (§4.6), and dropping the count from the query instead would remove the
			// only value that can detect it.
			//
			// COST OF KEEPING IT, measured (2026-08-08, EXPLAIN ANALYZE on the largest
			// tenant in the development database): the subquery is an Index Only Scan on
			// departments_tenant_idx, loops=9, Heap Fetches: 0, 0.505 ms.
			//
			// ⚠️ TWO LIMITS ON THAT NUMBER, because a measurement quoted without its
			// conditions is a claim about conditions nobody checked.
			//   1. loops=9 IS THE LARGEST TENANT THERE IS HERE, not the worst case. The
			//      worst case is bounded by venuePageLimit+1 = 201 loops, and that shape
			//      was NOT measured -- no tenant of that size exists to measure. It is
			//      inferred from the per-loop cost, which is the honest word for it.
			//   2. Heap Fetches: 0 DEPENDS ON THE VISIBILITY MAP being current. A freshly
			//      written departments table -- exactly the state right after a manager
			//      adds several -- will not answer 0 until autovacuum has been round, so
			//      the first reads after a burst of writes cost heap fetches this figure
			//      does not include.
			DepartmentCount:  v.DepartmentCount,
			DepartmentsShown: len(byVenue[v.ID]),
			EditHref:         locationsHref + "?venue=" + v.ID.String(),
		})
	}
	departments := make([]components.DepartmentRowView, 0, len(screen.Departments))
	for _, d := range screen.Departments {
		departments = append(departments, components.DepartmentRowView{
			Name:     d.Name,
			Venue:    d.LocationName,
			Shift:    shiftText(d.Shift),
			EditHref: locationsHref + "?department=" + d.ID.String(),
		})
	}
	return venues, departments
}

func venueFormOf(v tenant.Venue) components.VenueFormView {
	f := components.VenueFormView{
		Heading:  "Edit " + v.Name,
		ID:       v.ID.String(),
		Name:     v.Name,
		Ranges:   strings.Join(ipTexts(v.StaticIPs), "\n"),
		WiFiSSID: v.WiFiSSID,
		Submit:   "Save venue",
	}
	if v.GPS != nil {
		f.GPSLat = strconv.FormatFloat(v.GPS.Lat, 'f', 6, 64)
		f.GPSLng = strconv.FormatFloat(v.GPS.Lng, 'f', 6, 64)
	}
	if v.Shift != nil {
		f.ShiftStart = clockText(v.Shift.Start)
		f.ShiftEnd = clockText(v.Shift.End)
		f.Overnight = v.Shift.Overnight
	}
	return f
}

func blankVenueForm() *components.VenueFormView {
	return &components.VenueFormView{Heading: "Add a venue", Submit: "Add venue"}
}

func departmentFormOf(d tenant.Department, venues []components.OptionView) components.DepartmentFormView {
	f := components.DepartmentFormView{
		Heading: "Edit " + d.Name,
		ID:      d.ID.String(),
		Name:    d.Name,
		Venues:  venues,
		VenueID: d.LocationID.String(),
		// 🔴 THE VENUE IS SHOWN AND NOT EDITABLE, and the screen says why rather than
		// rendering a disabled control. UpdateDepartment does not name location_id
		// (db/queries/departments.sql), so a control here would be one the server
		// ignores — the shape TestLocationsSection_OffersNoActionTheServerWouldRefuse
		// is there to prevent.
		VenueFixed: true,
		VenueName:  d.LocationName,
		Submit:     "Save department",
	}
	if d.Shift != nil {
		f.ShiftStart = clockText(d.Shift.Start)
		f.ShiftEnd = clockText(d.Shift.End)
		f.Overnight = d.Shift.Overnight
	}
	return f
}

func blankDepartmentForm(venues []components.OptionView) components.DepartmentFormView {
	return components.DepartmentFormView{
		Heading: "Add a department",
		Venues:  venues,
		Submit:  "Add department",
	}
}

func venueOptionViews(in []tenant.Venue) []components.OptionView {
	out := make([]components.OptionView, 0, len(in))
	for _, v := range in {
		out = append(out, components.OptionView{Value: v.ID.String(), Label: v.Name})
	}
	return out
}

func ipTexts(in []netip.Prefix) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, p.String())
	}
	return out
}

// gpsText renders a coordinate for display, or "" for none. SIX DECIMAL PLACES,
// which is the column's own resolution (numeric(9,6), about 11 cm) — printing more
// would invent precision the database does not hold.
func gpsText(g *tenant.GPS) string {
	if g == nil {
		return ""
	}
	return strconv.FormatFloat(g.Lat, 'f', 6, 64) + ", " + strconv.FormatFloat(g.Lng, 'f', 6, 64)
}

// shiftText renders a working window for display, or "" for none — and "" is read
// by the template as "lateness is not measured here", which is a different sentence
// from a shift of zero length.
func shiftText(s *tenant.Shift) string {
	if s == nil {
		return ""
	}
	out := clockText(s.Start) + "–" + clockText(s.End)
	if s.Overnight {
		out += " (next day)"
	}
	return out
}

func clockText(d time.Duration) string {
	m := int(d / time.Minute)
	return pad2(m/60) + ":" + pad2(m%60)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// --- the removal control (user decision, 2026-08-08: option (b)) --------------------

// venueByID answers from the page's own list first and falls back to the tenant-scoped
// by-id read for a row past the ceiling. Its twin for departments is in
// locationactions.go, beside the write that first needed it.
func (a *AdminAuth) venueByID(r *http.Request, tenantID, venueID uuid.UUID, screen tenant.VenueScreen) (tenant.Venue, bool, error) {
	for _, v := range screen.Venues {
		if v.ID == venueID {
			return v, true, nil
		}
	}
	ven, err := a.venues.Venue(r.Context(), tenantID, venueID)
	switch {
	case err == nil:
		return ven, true, nil
	case errors.Is(err, tenant.ErrUnknownVenue):
		return tenant.Venue{}, false, nil
	default:
		return tenant.Venue{}, false, err
	}
}

// attachVenueRemoval decides whether this card may offer a removal, and mints the
// confirmation when the manager has asked for the warning.
//
// 🔴 THE REFERENCE COUNT RUNS HERE AND NOT IN THE LIST, AND THAT IS A MEASUREMENT
// RATHER THAN A LAYOUT CHOICE. EXPLAIN (ANALYZE, BUFFERS) on the largest tenant in the
// development database, 2026-08-08:
//
//	four counts on EVERY row of the venue list ..... 249.5 / 195.0 / 197.7 ms
//	four counts for the ONE venue a card is open on . 31.3 /  29.9 /  27.0 ms
//	nothing at all, because the list offers no control  0
//
// WHAT COSTS THE TIME is the transactions predicate: there is no index on
// transactions(tenant_id, location_id), so it becomes a bitmap heap scan. Putting the
// control on the EDIT CARD costs the list NOTHING and pays ~30 ms once, for the venue
// the manager actually opened.
//
// 🔴 THE ELIMINATED ALTERNATIVE — four EXISTS per row instead of four counts — HAS NO
// SINGLE NUMBER, AND FINDING THAT OUT TOOK TWO WRONG ONES. Measured on tenant
// 10000000-…-0001 (9 locations, 27 987 transactions) and 97e30555-… (8 locations,
// 0 transactions):
//
//	written order, tenant with 27 987 transactions ..... 8.4 / 6.4 / 7.3 ms
//	  ...because the SECOND predicate (employees) is true for all 9 rows, so `tags`
//	  and `transactions` are never evaluated at all.
//	transactions predicate FIRST, same tenant ....... 241.8 / 150.7 / 143.0 ms
//	written order, tenant with 0 transactions ......... 0.55 / 1.26 / 0.34 ms
//	  ...because the expensive predicate finds nothing, instantly.
//
// So its cost is a property of the DATA — how many transactions the TENANT has, and
// which predicate happens to match first — spanning three orders of magnitude over
// the same expression. That is the argument for the counts: they cost the same
// whatever the data does.
//
// ⚠️ TWO EARLIER VERSIONS OF THIS BLOCK WERE WRONG AND BOTH WERE LABELLING FAILURES,
// not measurement ones. The first said "four EXISTS … 8.154 ms" full stop — a real
// number for the short-circuiting case, presented as the shape's cost. The second
// "corrected" it to "258.9 / 194.1 / 188.1 ms over UNREFERENCED rows" — also a real
// number, but of a query with NO TENANT FILTER scanning unreferenced locations across
// the whole 122 000-row table, while the sentence named a tenant whose unreferenced
// count is ZERO. Both times the number was true and the population was invented. The
// rule this block now follows: a timing is written with the tenant, its row count and
// its transaction count, or it is not written.
//
// ⚠️ 9 LOCATIONS IS THE LARGEST TENANT THAT EXISTS HERE, NOT THE WORST CASE. The list
// shape is bounded by venuePageLimit+1 = 201 rows, and no tenant of that size exists to
// measure — so 288 ms is a measurement at loops=9 and the 201-row figure is INFERRED
// from the per-loop cost. This query does not scale with the list at all, which is
// what makes the inference safe here.
func (a *AdminAuth) attachVenueRemoval(w http.ResponseWriter, r *http.Request, f *components.VenueFormView, ven tenant.Venue) error {
	// 🔴 THE ROLE GATE IS CONSULTED BEFORE THE QUERY IS ISSUED, NOT MERELY BEFORE THE
	// DECISION IS MADE. An earlier version counted first and then let removalView
	// discard the answer, and the comment there claimed the check came "before the
	// reference count is even consulted" — true of the ORDER of reasoning, false of
	// the ORDER of statements. Measured: a manager opening two cards paid two
	// four-count reads (~30 ms each, the transactions predicate has no index) for a
	// control they were never going to be offered. What a manager may do does not
	// depend on what the venue contains, so the cheaper fact is asked first.
	if !mayRemove(httpx.AdminOf(r)) {
		f.Removal = reservedRemoval("venue", ven.Name, locationsHref+"?venue="+ven.ID.String())
		return nil
	}
	refs, err := a.venues.VenueReferences(r.Context(), httpx.AdminOf(r).TenantID(), ven.ID)
	if err != nil {
		if errors.Is(err, tenant.ErrUnknownVenue) {
			return nil
		}
		return err
	}
	f.Removal = a.removalView(w, r, removalInput{
		Action:        venueDeleteHref,
		ID:            ven.ID,
		Kind:          "venue",
		Name:          ven.Name,
		Refs:          refs,
		Reasons:       venueReasons(refs),
		Counted:       venueCountedNames,
		Param:         "venue",
		ConfirmAction: confirmActionDeleteVenue,
	})
	return nil
}

func (a *AdminAuth) attachDepartmentRemoval(w http.ResponseWriter, r *http.Request, f *components.DepartmentFormView, dep tenant.Department) error {
	// The same ordering as attachVenueRemoval, and for the reason written there.
	if !mayRemove(httpx.AdminOf(r)) {
		f.Removal = reservedRemoval("department", dep.Name,
			locationsHref+"?department="+dep.ID.String())
		return nil
	}
	refs, err := a.venues.DepartmentReferences(r.Context(), httpx.AdminOf(r).TenantID(), dep.ID)
	if err != nil {
		if errors.Is(err, tenant.ErrUnknownDepartment) {
			return nil
		}
		return err
	}
	f.Removal = a.removalView(w, r, removalInput{
		Action:        departmentDeleteHref,
		ID:            dep.ID,
		Kind:          "department",
		Name:          dep.Name,
		Refs:          refs,
		Reasons:       departmentReasons(refs),
		Counted:       departmentCountedNames,
		Param:         "department",
		ConfirmAction: confirmActionDeleteDepartment,
	})
	return nil
}

type removalInput struct {
	Action  string
	ID      uuid.UUID
	Kind    string
	Name    string
	Refs    tenant.References
	Reasons []string
	// Counted names the reference kinds the query behind Refs actually looked at, so
	// the card can say what it checked instead of claiming a universal absence.
	Counted []string
	// Param is the query key this card was opened with, so the confirm link and the
	// cancel link return to the same card.
	Param string
	// ConfirmAction is what the minted value authorises. It is part of the signature,
	// so a venue-removal confirmation cannot open the department gate or the roster's.
	ConfirmAction string
}

// removalView builds the three-state control.
//
// 🔴 A FAILURE TO MINT SUPPRESSES THE CONTROL RATHER THAN SHIPPING A DEAD BUTTON —
// the rule employees.go already follows. A button whose POST can only be refused is
// the defect this card exists to avoid.
func (a *AdminAuth) removalView(w http.ResponseWriter, r *http.Request, in removalInput) *components.RemovalView {
	base := locationsHref + "?" + in.Param + "=" + in.ID.String()
	// 🔴 A MANAGER IS TOLD THE ACT IS RESERVED, NOT SHOWN A BUTTON THAT WILL FAIL. The
	// server guard in locationactions.go is the guarantee; this is the courtesy, and
	// this section has spent four review rounds establishing that offering a control
	// and then refusing the press is the defect to avoid.
	//
	// ⚠️ THIS IS THE SECOND GATE, NOT THE FIRST. Both callers check the role BEFORE
	// issuing the reference count, so a manager never reaches here — this is the
	// belt for a future caller that forgets.
	if !mayRemove(httpx.AdminOf(r)) {
		return reservedRemoval(in.Kind, in.Name, base)
	}
	v := &components.RemovalView{
		Action:       in.Action,
		ID:           in.ID.String(),
		Kind:         in.Kind,
		Name:         in.Name,
		Blocked:      in.Refs.InUse(),
		Reasons:      in.Reasons,
		Counted:      in.Counted,
		ConfirmHref:  base + "&confirm=remove",
		CancelHref:   base,
		ConfirmField: confirmField,
	}
	if v.Blocked {
		return v
	}
	if r.URL.Query().Get("confirm") != "remove" {
		return v
	}
	// ⚠️ THIS MINT HAPPENS ON A **GET**, WHICH IS OUTSIDE THE ORIGIN GATE — a counted
	// limit, not a closed one. sameOriginGate guards mountWriting; a card is opened by
	// navigation, so a cross-origin top-level link to ?venue=<id>&confirm=remove will
	// render the warning and set the confirmation cookie. What that does NOT buy an
	// attacker: they cannot read the response (no CORS), and the POST that spends it is
	// still Origin-gated, so the value is useless to them. What remains is that a
	// third party can cause an owner to SEE a "Remove X?" warning. It is not specific
	// to this phase — M6-05's employees.go ships the identical shape for deactivation —
	// and it is recorded here rather than in a report that nobody re-reads.
	//
	// 🔴 THE SAME CONFIRMATION THE ROSTER USES, NOT A SECOND ONE. deactivateconfirm.go
	// binds its value to ONE ACTION, ONE SUBJECT ID and ONE PANEL SESSION under an
	// HMAC; nothing in it is employee-specific, so a venue id fits
	// mint(action, subjectID, sessionID) exactly.
	// Half-copying a pattern is a mistake this repository has made three times, and
	// calling the same code is the cheapest way not to make it four.
	token, err := a.setConfirmation(w, r, in.ConfirmAction, in.ID.String())
	if err != nil {
		a.log.Error("panel: could not mint the removal confirmation", "err", err)
		return v
	}
	v.Confirming = true
	v.ConfirmToken = token
	return v
}

// venueReasons renders what is in the way, in the order a manager would deal with it.
//
// THE NUMBERS ARE THE POINT. "Still in use" alone leaves somebody hunting; "3
// employees, 1 plaque" tells them how much work it is and where to start.
func venueReasons(r tenant.References) []string {
	var out []string
	out = appendReason(out, r.Departments, "department", "departments")
	out = appendReason(out, r.Employees, "employee", "employees")
	out = appendReason(out, r.Tags, "plaque", "plaques")
	out = appendReason(out, r.Transactions, "recorded tap", "recorded taps")
	return out
}

func departmentReasons(r tenant.References) []string {
	var out []string
	out = appendReason(out, r.Employees, "employee", "employees")
	out = appendReason(out, r.Transactions, "recorded tap", "recorded taps")
	return out
}

func appendReason(out []string, n int, one, many string) []string {
	switch {
	case n <= 0:
		return out
	case n == 1:
		return append(out, "1 "+one)
	default:
		return append(out, strconv.Itoa(n)+" "+many)
	}
}

// --- the verified removal acknowledgement (user decision, 2026-08-09, reading C') ---

// venueRemovalReturn builds the 303 a completed removal lands on.
//
// 🔴 THE ID IS A LOOKUP KEY AND NEVER A SOURCE OF TEXT. It is a client value the
// moment it is in the address bar, so the section uses it only to find the actor's own
// audit entry; the sentence and the venue's name come from THAT row. A heading built
// from the URL would be a sentence the client wrote.
func venueRemovalReturn(done string, id uuid.UUID) string {
	w := oneOfWords(done, venueDoneWords...)
	if w == "" || id == uuid.Nil {
		return locationsHref
	}
	return locationsHref + "?done=" + w + "&id=" + id.String()
}

// removalReceipt verifies a removal acknowledgement, or answers "say nothing".
//
// 🔴 THIS IS M6-05's RULE (2) APPLIED TO THE ONE ACT WHERE THE ROW IS GONE. That rule
// permits an action claim only when the handler checks it against a row THE SAME
// REQUEST READ. After a deletion the only surviving row is the audit entry — which is
// append-only for tappa_app (INSERT and SELECT only, measured), so it cannot be forged
// or erased by anything reachable from here.
//
// 🔴 THE FAST PATH PAYS NOTHING. The query runs only when the word is one of the two
// that need it AND the address carries a readable id. Every other request — the plain
// section, a save, a filter — issues no extra statement at all.
//
// 🔴 A FAILED READ IS SILENCE, NOT A PROBLEM PAGE. Nothing is authorised by this
// answer; it decides whether a sentence appears. Rendering an error would turn a
// display detail into an outage, which is the opposite of §4.6's intent here.
func (a *AdminAuth) removalReceipt(r *http.Request, done string) *components.RemovalReceiptView {
	action, needsCheck := venueRemovalWords[done]
	if !needsCheck {
		return nil
	}
	raw := strings.TrimSpace(r.URL.Query().Get("id"))
	target, err := uuid.Parse(raw)
	if err != nil || target == uuid.Nil {
		// Nothing was looked up. A malformed id is indistinguishable from an
		// unmatched one in everything the caller can observe: no banner either way.
		return nil
	}
	id := httpx.AdminOf(r)
	receipt, err := a.venues.ConfirmRemoval(r.Context(), id.TenantID(),
		id.Admin.AdminUserID, action, target.String())
	if err != nil {
		a.log.Error("panel: could not read the removal receipt", "err", err)
		return nil
	}
	if !receipt.Found {
		return nil
	}
	return &components.RemovalReceiptView{Name: receipt.Name}
}

// reservedRemoval is the card a manager sees: no control, and the reason.
func reservedRemoval(kind, name, cancelHref string) *components.RemovalView {
	return &components.RemovalView{
		Kind:       kind,
		Name:       name,
		Reserved:   true,
		CancelHref: cancelHref,
	}
}

// venueCountedNames and departmentCountedNames are what each reference query really
// looks at, in the order the card lists them.
//
// 🔴 THEY EXIST BECAUSE ONE SENTENCE WAS SERVING TWO DIFFERENT CHECKS. Both cards
// printed "No departments, employees, plaques or records belong to this ..." — true of
// a venue, whose query counts four kinds, and FALSE of a department, whose query
// counts two: departments_location_fk and tags_location_fk point at LOCATIONS, so a
// department has neither sub-departments nor plaques of its own. The card was claiming
// a search that never ran, which is the exact shape unresolvedCardProblem refuses a
// few hundred lines up.
//
// ⚠️ THEY ARE DERIVED FROM THE QUERIES, NOT RE-TYPED FROM MEMORY: each list matches
// the columns its CountXReferences statement selects, and
// TestRemoval_TheCardNamesOnlyWhatItsQueryCounted holds the two in step.
var (
	venueCountedNames      = []string{"departments", "employees", "plaques", "recorded taps"}
	departmentCountedNames = []string{"employees", "recorded taps"}
)
