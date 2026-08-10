package handler

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The WALL PLAQUES half of the locations section — M6-06 PHASE B. Its writes
// are plaqueactions.go.
//
// 🔴 THE PANEL CANNOT CREATE A PLAQUE AND THIS FILE IS BUILT AROUND THAT (user
// decision, 2026-08-08: the inventory model). Tappa encodes a plaque, wraps its
// AES key under the KEK and LOADS the row; the panel's whole vocabulary is list,
// read, BIND, UN-BIND and RETIRE — never CREATE and never DELETE. There is no "add a
// plaque" control here, no route for
// one, and no query behind it — db/queries/tags.sql ships no INSERT.
//
// 🔴 §4.7 — NO KEY PASSES THROUGH THIS FILE, AND THE WALL IS A TYPE RATHER THAN A
// HABIT. tenant.Plaque has no []byte field, the view models have none, and none of
// the shipped tag queries selects aes_key_ref. The card's "encoded/pending"
// criterion is answered by a WORD derived from whether the plaque has a wall —
// keyStateOf below is the only thing that produces it, and it reads nothing about
// cryptography. What that word IS and IS NOT backed by is written at
// components/plaqueview.go and in internal/domain/tenant/plaque.go; the short
// version is that tags.aes_key_ref is NOT NULL by schema, so a row that exists is
// a plaque Tappa loaded, and NOTHING checks that the stored envelope is well
// formed (backlog T7).
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), which
// the Protect chain resolved from a signed session cookie against the database.
// What a client contributes is a plaque UID — and a uid is a GLOBAL primary key, so
// the tenant predicate on GetTagForTenant is doing real work rather than
// decorating: without it a manager could read another business's plaque by typing a
// uid they read off a wall in another building.
//
// 🔴 §4.6 — A FAILED READ IS AN ERROR, NEVER AN EMPTY LIST. "This business has no
// plaques" is a claim, and a timeout is not evidence for it. PlaquesQueried gates
// every empty state and is set only after the database answered.

// The POST routes this half mounts, built from the section's own URL — the rule
// venueSaveHref already follows, so a form cannot post to a path nothing mounts.
var (
	plaqueMountHref   = locationsHref + "/plaque/mount"
	plaqueReplaceHref = locationsHref + "/plaque/replace"
	plaqueUnmountHref = locationsHref + "/plaque/unmount"
)

// plaqueActWords are the acknowledgement words and the audit action each one must
// be verified against.
//
// 🔴 EACH WORD POINTS AT A DIFFERENT ACTION, AND THAT IS THE WHOLE MECHANISM. A
// replace writes BOTH plaque.retired (for the old uid) and plaque.mounted (for the
// new one); a plain mount writes only plaque.mounted. Verifying "replaced" against
// plaque.mounted would therefore print "Plaque replaced" after a mere mount — the
// M6-04 defect (a URL claiming something that did not happen) in a new costume. So
// "plaque-replaced" is checked against plaque.RETIRED, which only a replacement
// writes, and the id in the address is the RETIRED plaque's.
var plaqueActWords = map[string]string{
	"plaque-mounted":  tenant.ActionPlaqueMounted,
	"plaque-replaced": tenant.ActionPlaqueRetired,
	// The un-mount writes its own action, so this word cannot be printed after a
	// mount or a replacement either.
	"plaque-unmounted": tenant.ActionPlaqueUnmounted,
}

// maxPlaqueBody is the ceiling on a plaque POST's body.
//
// IT IS SMALLER THAN maxVenueBody BECAUSE THE FORM IS SMALLER: two uids, or a uid
// and a venue id. A bare ParseForm would inherit net/http's 10 MB, and a valid
// session may spend adminSessionLimit requests per window — the argument
// maxVenueBody makes at length.
const maxPlaqueBody = 4 << 10

// locationsPlaques reads the plaque side of the section.
//
// IT IS A SEPARATE READ FROM THE VENUES because they are separate domains objects
// with separate failure modes, and §4.6 wants the manager told WHICH read failed.
func (a *AdminAuth) locationsPlaques(r *http.Request) (tenant.PlaqueScreen, error) {
	return a.plaques.Screen(r.Context(), httpx.AdminOf(r).TenantID())
}

// fillPlaques puts the three lists and the cap notice on the view.
//
// 🔴 THREE LISTS, AND THE SPLIT IS A DECISION WITH A MEASUREMENT BEHIND IT — the
// two readings are argued in pages.LocationsView. In short: ListTagsForTenant
// orders `location_id NULLS FIRST, uid`, which puts stock first (useful) and then
// sorts the mounted half BY LOCATION UUID (a value this page never prints). So the
// SQL order is kept as the tiebreaker and the grouping is done here.
func fillPlaques(v *pages.LocationsView, screen tenant.PlaqueScreen, venues tenant.VenueScreen, zone *time.Location) {
	names := venueNames(venues)
	for _, p := range screen.Plaques {
		row := plaqueRowView(p, names, zone)
		switch {
		case p.OnAWall():
			v.Mounted = append(v.Mounted, row)
		case p.InStock():
			v.Stock = append(v.Stock, row)
		default:
			v.OutOfService = append(v.OutOfService, row)
		}
	}
	// THE MOUNTED LIST IS SORTED BY THE NAME THE PAGE SHOWS, not by the id it does
	// not. sort.SliceStable keeps the query's uid order for two plaques at the same
	// venue, so the page is deterministic without a second sort key.
	sort.SliceStable(v.Mounted, func(i, j int) bool {
		return v.Mounted[i].Venue < v.Mounted[j].Venue
	})
	// OUT OF SERVICE READS NEWEST FIRST. "What came off the wall lately" is the
	// question this list answers; a plaque with no retirement stamp (a `lost` one
	// that was never retired) sorts after the stamped ones rather than before.
	sort.SliceStable(v.OutOfService, func(i, j int) bool {
		return v.OutOfService[i].RetiredAt > v.OutOfService[j].RetiredAt
	})
	v.PlaquesQueried = true
	v.PlaquesCapped = screen.Capped
	v.PlaqueLimit = strconv.Itoa(len(screen.Plaques))
}

// venueNames maps a venue id onto the name the page prints.
//
// ⚠️ THE VENUE LIST IS CAPPED (venuePageLimit) AND THE PLAQUE LIST IS NOT KEYED TO
// IT, so a plaque mounted at a venue past the ceiling resolves to "". That is a
// THIRD state, not the stock one, and PlaqueRowView.VenueUnknown carries it —
// components/plaque.templ then prints "Venue could not be read", which is the
// treatment VenueRow already gives a missed department join.
//
// ⚠️ AN EARLIER VERSION OF THIS COMMENT CLAIMED THE TEMPLATE ALREADY DID THAT AND
// IT DID NOT: the only else-branch was "Not mounted yet", so a plaque ON A WALL was
// rendered as stock inside the "Plaques on the wall" list. Measured 2026-08-08: the
// largest tenant has 9 venues against a ceiling of 200, so nobody meets it — but a
// sentence describing behaviour the code does not have is the class this task keeps
// paying for, so the behaviour was added rather than the sentence softened.
func venueNames(screen tenant.VenueScreen) map[uuid.UUID]string {
	out := make(map[uuid.UUID]string, len(screen.Venues))
	for _, v := range screen.Venues {
		out[v.ID] = v.Name
	}
	return out
}

// plaqueRowView renders one plaque for the docket.
func plaqueRowView(p tenant.Plaque, names map[uuid.UUID]string, zone *time.Location) components.PlaqueRowView {
	row := components.PlaqueRowView{
		UID:        p.UID,
		Status:     plaqueStatusWord(p.Status),
		Counter:    strconv.FormatInt(int64(p.LastCtr), 10),
		LastSeen:   plaqueStamp(p.LastSeen, zone),
		KeyState:   keyStateOf(p),
		Replaces:   p.Replaces,
		ReplacedBy: p.ReplacedBy,
		RetiredAt:  plaqueStamp(p.RetiredAt, zone),
		Loaded:     plaqueStamp(&p.CreatedAt, zone),
		Frozen:     !p.Changeable(),
		// 🔴 THE LINK CARRIES THE ROW'S OWN SPELLING, AND THE LOOKUP PRESERVES IT
		// (tenant.PlaqueRef). Those two facts are ONE mechanism: `tags.uid` is char(14)
		// and case-sensitive, so a link built from the stored value and a lookup that
		// folded case would disagree — which is exactly what was measured happening, on
		// a row this very list had printed.
		OpenHref: locationsHref + "?plaque=" + p.UID,
	}
	if p.LocationID != nil {
		row.Venue = names[*p.LocationID]
		// MOUNTED BUT UNNAMED IS ITS OWN STATE. The venue list is capped and the
		// plaque list is not keyed to it, so a name can be missing for a plaque that
		// is very much on a wall; the template says which of the two it is.
		row.VenueUnknown = row.Venue == ""
	}
	return row
}

// plaqueStatusWord turns the column's vocabulary into the manager's.
//
// 🔴 THE DEFAULT IS A SAFE WORD, NOT THE RAW VALUE, AND IT IS FAIL-CLOSED. The
// status set is a schema CHECK (00004 + 00013) and it has GROWN once already —
// `unassigned` arrived in 00013 and a deny-list in the policy engine did not see
// it, which is the defect that round closed. A fifth value would reach this screen
// as "Not in service", which offers no action, rather than as a raw column value
// rendered as if the panel understood it.
func plaqueStatusWord(status string) string {
	switch status {
	case tenant.PlaqueActive:
		return "In service"
	case tenant.PlaqueUnassigned:
		return "In stock"
	case tenant.PlaqueRetired:
		return "Retired"
	case tenant.PlaqueLost:
		return "Reported lost"
	default:
		return "Not in service"
	}
}

// keyStateOf is the M6-06 card's "encoded/pending" criterion, and it is the ONLY
// thing on this screen that says anything about a plaque's key.
//
// 🔴 IT READS NO KEY. It reads whether the plaque has a wall. The word "encoded"
// is licensed by a SCHEMA fact rather than by an inspection: tags.aes_key_ref is
// `bytea NOT NULL` (00004) and only Tappa's loader writes rows, so a row that
// exists is a plaque somebody encoded and loaded. "Pending" means pending a WALL,
// which is what the inventory model made representable (00013 part 3: pending =
// location_id IS NULL).
//
// ⚠️ WHAT IT DOES NOT CLAIM, stated because a word that overclaims is worse than no
// word: nothing here verifies the stored envelope is the well-formed 44-byte KEK
// wrapping. This screen cannot check it — checking would mean SELECTing the column,
// which is exactly what §4.7 forbids here — so the gap is backlog T7's and it needs a
// schema CHECK, not a screen.
//
// 🔴 AND THE COST OF THAT GAP IS BIGGER THAN A BAD WORD ON A CARD — measured by a
// security audit, 2026-08-10, on a row whose aes_key_ref is the 2-byte test
// placeholder: the card says "Encoded by Tappa", and an NFC tap on that plaque
// answers 500 with NO ROW WRITTEN to `transactions` (the chip counter stays where it
// was). That is a §4.6 loss on the tap path — the refusal leaves no trace — and it is
// NOT this panel's defect: the panel cannot create such a row (db/queries ships no
// INSERT over `tags`) and cannot repair one. It is recorded here because this is
// where the misleading WORD is produced, so whoever fixes T7 finds the whole cost in
// one place.
func keyStateOf(p tenant.Plaque) string {
	if p.InStock() {
		return "Encoded by Tappa — pending a wall"
	}
	return "Encoded by Tappa"
}

// plaqueStamp renders an instant in the tenant's zone, or "" for none.
//
// nil IS "" AND "" IS RENDERED AS WORDS BY THE TEMPLATE. A zero time.Time would be
// orderable and formattable and would print 1 January year one, which is the exact
// mistake the pointer exists to make unavailable (the data layer's own lesson at
// ListTagLastSeen).
func plaqueStamp(t *time.Time, zone *time.Location) string {
	if t == nil {
		return ""
	}
	if zone == nil {
		zone = time.UTC
	}
	return t.In(zone).Format("2 Jan 2006 15:04")
}

// plaqueCard builds the card for ?plaque=<uid>, or nil.
//
// 🔴 IT IS THE THIRD CARD ON THIS SECTION AND AT MOST ONE IS EVER OPEN. venueForms
// takes ?venue= first and ?department= second; this runs only when neither opened
// anything, so the precedence is explicit rather than emergent.
//
// A REAL READ FAILURE IS AN ERROR, NOT A SENTENCE (§4.6). It is returned so the
// caller renders a problem page: "we could not find that plaque" would be a claim
// about the business built on a database that did not answer.
func (a *AdminAuth) plaqueCard(w http.ResponseWriter, r *http.Request, screen tenant.PlaqueScreen, venues tenant.VenueScreen, zone *time.Location) (*components.PlaqueFormView, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("plaque"))
	if raw == "" {
		return nil, nil
	}
	// 🔴 PlaqueRef, NOT PlaqueUID: THIS IS A LOOKUP AND THE COLUMN IS CASE-SENSITIVE.
	// The first version of this line upper-cased, and it was measured answering "that
	// plaque id is not one of this business's" about a row the same page had just
	// listed — because `tags.uid` is char(14) and the table still holds 18 010
	// lower-case rows that predate 00013. A screen that lists a row it cannot open is
	// the defect this section spent four rounds closing.
	uid, err := tenant.PlaqueRef(raw)
	if err != nil {
		// Not a plaque id: nothing was looked up, and the section's own sentence
		// covers it — the "unreadable" / "unknown" distinction unresolvedCardProblem
		// makes for venues.
		return nil, nil
	}
	p, found, err := a.plaqueByUID(r, uid, screen)
	switch {
	case err != nil:
		return nil, err
	case !found:
		return nil, nil
	}
	f := plaqueCardOf(p, screen, venues, zone)
	// 🔴 THE TRAIL IS READ ONLY FOR THE OPEN CARD, never for the list. It is one
	// bounded query for one plaque, and putting it on every row would pay for it
	// N times to answer a question a manager asks about ONE door — the same
	// measurement that moved phase A's reference count off the venue list.
	trail, err := a.plaques.History(r.Context(), httpx.AdminOf(r).TenantID(), p.UID)
	if err != nil {
		// §4.6: OUR read failed, and "nothing has been done to this plaque" is a claim.
		return nil, err
	}
	f.Trail = plaqueTrailView(trail, zone)
	// 🔴 THE REPLACEMENT IS CONFIRMED, AND THE CARD *IS* THE WARNING.
	//
	// Phase A's removals use a two-step shape — a link to ?confirm=remove, which mints
	// and arms a one-click button — because there is nothing else on that card to
	// decide. A replacement already requires a CHOICE (which spare goes on the wall),
	// so a second step would either throw the choice away or add a screen to an act
	// the card budgets 30 seconds for. Minting on THIS GET keeps the property the
	// mechanism actually buys, stated at deactivateconfirm.go: reaching the
	// irreversible write implies the server RENDERED the warning, in response to a
	// request for it, for THIS person, in THIS session, inside the window.
	//
	// ⚠️ THE SAME COUNTED LIMIT AS PHASE A APPLIES (backlog T14): this mint happens on
	// a GET, which is outside the Origin gate, so a cross-origin top-level link can
	// make an owner SEE a warning. They cannot read the response and the POST that
	// spends it is still Origin-gated.
	//
	// 🔴 A FAILURE TO MINT SUPPRESSES THE CONTROL RATHER THAN SHIPPING A DEAD BUTTON —
	// removalView's rule. A form whose POST can only be refused is the defect this
	// card exists to avoid.
	// 🔴 THE SECOND STEP OF THE UN-MOUNT, AND IT REPLACES THE FIRST CARD RATHER THAN
	// SITTING BESIDE IT. Exactly one confirmation is minted per render (the cookie is
	// one per browser), so asking for the un-mount warning withdraws the replacement
	// form for that render — phase A's two-step shape, applied where it is needed.
	if f.Mode == "onwall" && r.URL.Query().Get("confirm") == "unmount" {
		f.Mode = "unmount"
		f.Action = plaqueUnmountHref
		f.Successors = nil
		f.Blocked = ""
		f.Submit = "Take it off the wall"
	}

	// 🔴 AT MOST ONE CONFIRMATION IS MINTED PER RENDER, and the switch is exhaustive
	// rather than a chain of ifs so that a fourth mode cannot inherit whichever arm
	// happened to be last. The un-mount LINK carries no token — only the FORM behind
	// it does — so a card offering a link and a form arms exactly the form.
	var action string
	switch {
	case f.Mode == "unmount":
		action = confirmActionUnmountPlaque
	case f.Mode == "onwall" && f.Action != "":
		action = confirmActionReplacePlaque
	}
	if action != "" {
		token, err := a.setConfirmation(w, r, action, p.UID)
		if err != nil {
			a.log.Error("panel: could not mint the plaque confirmation",
				"err", err, "action", action)
			// A FAILURE TO MINT SUPPRESSES THE CONTROL RATHER THAN SHIPPING A DEAD
			// BUTTON — removalView's rule. The un-mount link survives, because it leads
			// to another GET rather than to a write.
			f.Mode = ""
			f.Action = ""
			f.Successors = nil
			f.Blocked = "We could not prepare that just now. Try again in a moment."
			return &f, nil
		}
		f.ConfirmField = confirmField
		f.ConfirmToken = token
	}
	return &f, nil
}

// plaqueByUID answers from the page's own list first and falls back to the
// tenant-scoped by-uid read for a row past the ceiling — venueByID's rule, and for
// the reason venueForms states: a row past the cap is not a row that stopped
// existing.
func (a *AdminAuth) plaqueByUID(r *http.Request, uid string, screen tenant.PlaqueScreen) (tenant.Plaque, bool, error) {
	for _, p := range screen.Plaques {
		if p.UID == uid {
			return p, true, nil
		}
	}
	p, err := a.plaques.Plaque(r.Context(), httpx.AdminOf(r).TenantID(), uid)
	switch {
	case err == nil:
		return p, true, nil
	case errors.Is(err, tenant.ErrUnknownPlaque), errors.Is(err, tenant.ErrPlaqueUID):
		return tenant.Plaque{}, false, nil
	default:
		return tenant.Plaque{}, false, err
	}
}

// plaqueCardOf decides which state the card is in — the Mode values are enumerated
// at components.PlaqueFormView.Mode, which is the one place they are listed.
//
// 🔴 A CONTROL IS OFFERED ONLY WHERE THE SERVER WOULD ACCEPT THE PRESS. Every arm
// that cannot lead to a write says WHY in a sentence instead — the property
// TestLocationsSection_OffersNoActionTheServerWouldRefuse holds for the department
// form and this file's tests hold for plaques. The order of the arms is the order
// of the refusals: what the DATABASE will not accept first, then what the BUSINESS
// does not have.
func plaqueCardOf(p tenant.Plaque, screen tenant.PlaqueScreen, venues tenant.VenueScreen, zone *time.Location) components.PlaqueFormView {
	f := components.PlaqueFormView{
		Heading:   p.UID,
		CloseHref: locationsHref,
		Plaque:    plaqueRowView(p, venueNames(venues), zone),
	}
	switch {
	case !p.Changeable():
		// The frozen development residue (00013's NOT VALID constraint). The row's own
		// docket already carries the warning; this is why there is no control.
		f.Blocked = "This plaque cannot be changed, so it cannot be mounted or replaced."
	case p.InStock():
		options := venueOptionViews(venues.Venues)
		if len(options) == 0 {
			f.Blocked = "Add a venue first — a plaque is mounted at an entrance, and this business has none yet."
			break
		}
		f.Mode = "mount"
		f.Action = plaqueMountHref
		f.Venues = options
		f.Submit = "Mount this plaque"
	case p.OnAWall():
		// 🔴 TAKING IT DOWN IS ALWAYS ON OFFER FOR A MOUNTED PLAQUE, and that is the
		// repair for a defect an audit measured: a plaque put on the WRONG door could
		// not be moved, could not go back to stock, and with no spare in the box this
		// card offered nothing at all — while every tap there was judged against the
		// wrong venue's IP and coordinate (§5 row 7, the approval queue).
		f.UnmountHref = locationsHref + "?plaque=" + p.UID + "&confirm=unmount"
		spares := stockOptions(screen, p.UID)
		if len(spares) == 0 {
			// ⚠️ AND THE SENTENCE NO LONGER ENDS AT "ask us for one". A manager whose
			// plaque is on the wrong door does not need a spare — they need it off the
			// wall — and the old copy sent them to us for a thing they already had.
			f.Blocked = "There is no spare plaque in stock to swap this one for. " +
				"If it is simply on the wrong door, take it off the wall below and mount " +
				"it where it belongs — that costs no spare."
			f.Mode = "onwall"
			break
		}
		f.Mode = "onwall"
		f.Action = plaqueReplaceHref
		f.Successors = spares
		f.Submit = "Replace this plaque"
	default:
		f.Blocked = "This plaque is out of service. Its record is kept — the taps it took still point at it — but it cannot go back on a wall."
	}
	return f
}

// stockOptions is the spare plaques a replacement may choose from.
//
// IT EXCLUDES THE PLAQUE BEING REPLACED, which cannot be in stock anyway (it is
// `active`) — the filter is there so that a future status change cannot make the
// card offer a plaque as its own successor. The domain refuses that with
// ErrSamePlaque; this is the courtesy.
//
// A FROZEN ROW IS NOT OFFERED. The database would refuse the write with 23514, and
// a control that leads to a refusal is the defect this section has spent four
// review rounds closing.
func stockOptions(screen tenant.PlaqueScreen, exclude string) []components.OptionView {
	out := make([]components.OptionView, 0, len(screen.Plaques))
	for _, p := range screen.Plaques {
		if !p.InStock() || !p.Changeable() || p.UID == exclude {
			continue
		}
		out = append(out, components.OptionView{Value: p.UID, Label: p.UID})
	}
	return out
}

// plaqueActionWords is the ONE place a trail action becomes a manager's word.
//
// 🔴 IT IS A MAP WITH A DERIVED COMPLETENESS CHECK, NOT A switch, AND THAT IS THE
// SIXTH TIME THIS TASK HAS PAID FOR A HAND-MAINTAINED LIST. The switch it replaces
// named two actions; the same change set added a THIRD (plaque.unmounted) and the
// panel printed the raw database word at a manager —
// "plaque.unmounted 10 Aug 2026 02:17 · E2E Owner" — while three comments claimed the
// render layer turned it into a sentence. The others in this family: a guardrail
// deny-list that missed a fourth tag status, a §4.7 scan of seven forbidden words, a
// type wall built as a deny-list, and a hand-counted vocabulary slice.
//
// TestPlaqueTrail_NamesEveryActionTheDOMAINCanWrite derives the action set from
// internal/domain/tenant's own const block with go/ast and fails on any value that
// is missing here. So the list cannot go stale: adding an action without a word is
// RED, and nobody has to remember this file exists.
var plaqueActionWords = map[string]string{
	tenant.ActionPlaqueMounted:   "Mounted",
	tenant.ActionPlaqueRetired:   "Retired",
	tenant.ActionPlaqueUnmounted: "Taken off the wall",
}

// plaqueTrailView turns the trail's own vocabulary into the manager's.
//
// 🔴 AN UNMAPPED ACTION FALLS BACK TO THE RAW WORD, WHICH IS THE FAIL-VISIBLE
// DIRECTION AND IS NOT A LICENCE. It means a future action shows up ugly rather than
// invisibly; the completeness test is what stops that from shipping. The previous
// version had the same fallback and a comment calling it "a FUTURE plaque act" —
// while the act it was describing had shipped in the same diff.
func plaqueTrailView(events []tenant.PlaqueEvent, zone *time.Location) []components.PlaqueEventView {
	out := make([]components.PlaqueEventView, 0, len(events))
	for _, e := range events {
		what, ok := plaqueActionWords[e.Action]
		if !ok {
			what = e.Action
		}
		at := e.At
		out = append(out, components.PlaqueEventView{
			What:     what,
			When:     plaqueStamp(&at, zone),
			Who:      e.ActorName,
			BySystem: e.BySystem,
		})
	}
	return out
}

// plaqueReceipt verifies a plaque acknowledgement, or answers "say nothing".
//
// 🔴 THIS IS M6-05's RULE (2) AND M6-06 PHASE A's READING C', APPLIED TO AN ACT
// WHOSE ROW IS STILL THERE. The rows a replacement touched ARE rendered beneath the
// banner, so a state-shaped heading could have backed itself — but "replaced" is an
// EVENT claim, and the only thing that can support one is the actor's own trail
// row. The audit entry carries the actor, which the tags row cannot.
//
// 🔴 THE FAST PATH PAYS NOTHING. The query runs only when the word is one of those
// in plaqueActWords AND the address carries a readable uid.
//
// 🔴 A FAILED READ IS SILENCE, NOT A PROBLEM PAGE. Nothing is authorised by this
// answer; it decides whether a sentence appears.
func (a *AdminAuth) plaqueReceipt(r *http.Request, done string) *components.PlaqueReceiptView {
	action, needsCheck := plaqueActWords[done]
	if !needsCheck {
		return nil
	}
	// A LOOKUP KEY into audit_log.target, so the spelling is the one that was written.
	uid, err := tenant.PlaqueRef(r.URL.Query().Get("id"))
	if err != nil {
		// Nothing was looked up. A malformed uid is indistinguishable from an
		// unmatched one in everything the caller can observe: no banner either way.
		return nil
	}
	id := httpx.AdminOf(r)
	receipt, err := a.plaques.ConfirmPlaqueAct(r.Context(), id.TenantID(),
		id.Admin.AdminUserID, action, uid)
	if err != nil {
		a.log.Error("panel: could not read the plaque receipt", "err", err)
		return nil
	}
	if !receipt.Found {
		return nil
	}
	return &components.PlaqueReceiptView{
		Replaced:  done == "plaque-replaced",
		Unmounted: done == "plaque-unmounted",
		Name:      receipt.Name,
	}
}

// plaqueReturn builds the 303 a completed plaque act lands on.
//
// 🔴 THE UID IS A LOOKUP KEY AND NEVER A SOURCE OF TEXT — venueRemovalReturn's
// rule. It is a client value the moment it is in the address bar, so the section
// uses it only to find the actor's own audit entry; the sentence and the name come
// from THAT row.
func plaqueReturn(done, uid string) string {
	w := oneOfWords(done, venueDoneWords...)
	if w == "" || uid == "" {
		return locationsHref
	}
	return locationsHref + "?done=" + w + "&id=" + uid
}
