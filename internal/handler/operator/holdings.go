package operator

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// THE HOLDINGS (M11 API-1a; ADR 0027 §1, ADR 0021 §2 v; migration 00042). A holding groups
// businesses (tenants) that share one timezone (K-36); its membership is written by the platform
// operator alone, here. Four routes, all in the console's group (routes.go, mount): host gate,
// security headers, flood gate, same-origin gate (a GET a browser labels same-site or cross-site
// is the sign-in redirect; a POST from another origin is 403 before any store call),
// requireOperator, sessionGate.
//
//	GET  /operator/holdings                              the list (a two-phase read: op_begin_read +
//	                                                     op_read_holdings), a page of it, and the
//	                                                     sentence of the last write (below)
//	POST /operator/holdings                              create one (op_create_holding)
//	POST /operator/holdings/{id}/members                 add a business (op_add_holding_member)
//	POST /operator/holdings/{id}/members/{tenant}/remove end a business's membership
//	                                                     (op_remove_holding_member)
//
// 🔴 A WRITE'S ANSWER SAYS NOTHING ABOUT WHAT IT FOUND (ADR 0021 §2 v 7, B14). The database adds
// a business only when every rule holds -- the holding and the business exist, the business has
// no membership live or ended less than 60 seconds ago, its timezone is every member's (K-36), no
// member carries its API code -- and answers the SAME whether it added or refused; the removal
// answers the same whether there was a live membership to end or not. So every accepted write is
// a 303 to the list with what was ASKED in the query (done, holding, tenant), and the list's own
// audited read says what HAPPENED (holdingResultNotice): the sentence comes from the GET's read,
// never from the POST. A query value is used only to look a row of that read up; it is never
// printed -- the page prints what the read returned and fixed sentences.
//
// THE PAGE NUMBER IS IN THE URL (?page=N) -- unlike the tenant list and the audit log, whose
// pages are posted (a search term can be an address; tenants.go). A page number and two ids are
// not personal data, and the 303 after a write needs a GET it can land on.
//
// WHAT IS LOGGED: the holding's and the business's ids, the operator's id and internal/db's error
// (a call and a SQLSTATE). Never the session token or its hash (ADR 0020 §5), and not the
// holding's name (it is the operator's label, not a credential -- it simply has no reason to be
// in the process log).

// HoldingStore is the operator database's slice the holdings screen needs, declared at the
// consumer (CLAUDE.md §7). *db.OperatorDB implements it by delegating to internal/db's Holdings,
// CreateHolding, AddHoldingMember and RemoveHoldingMember, and that type's method set is derived
// from this interface and the other stores (TestOperatorDB_IsTheStoreAndNothingMore). sessionHash
// is operatorauth's SessionHash of the request's token -- on ADR 0020 §5's never-log list.
type HoldingStore interface {
	// Holdings is op_begin_read + op_read_holdings: a page of db.HoldingsPerPage rows, one per
	// membership (live and ended) and one per holding without any, newest holding first and a
	// holding's live memberships before its ended ones. A dead session is db.ErrOperatorRefused.
	Holdings(ctx context.Context, sessionHash string, page int32) ([]db.HoldingRow, error)
	// CreateHolding is op_create_holding: an empty holding named name; the answer is its id. A
	// name outside the database's rule is SQLSTATE 22023 and nothing is written.
	CreateHolding(ctx context.Context, sessionHash, name string) (uuid.UUID, error)
	// AddHoldingMember is op_add_holding_member. nil says the call was ACCEPTED, not that the
	// business was added (the file's comment).
	AddHoldingMember(ctx context.Context, sessionHash string, holdingID, tenantID uuid.UUID) error
	// RemoveHoldingMember is op_remove_holding_member. nil says the call was accepted, not that
	// a membership ended.
	RemoveHoldingMember(ctx context.Context, sessionHash string, holdingID, tenantID uuid.UUID) error
}

// maxHoldingPage bounds the page the list may be asked for: 1 000 pages of db.HoldingsPerPage
// rows reach 200 000 memberships -- production will count holdings in ones -- and the pager
// offers no page beyond it. (The database takes up to 999 999 999.)
const maxHoldingPage = 1000

// THE TWO BOUNDS, each one step's, each a number (TestHoldings_TheTwoBoundsAreTheirNumbers):
//
//   - holdingsReadTimeout bounds a read of the list -- both its phases -- on the GET and on the
//     POST whose refusal re-renders the list (that POST runs on answered(r), detached from the
//     client, so its read needs a bound of its own). vat.go's vatReadTimeout and its reasoning:
//     far above two statements on a reachable database; what it bounds is a lock wait or a stalled
//     connection.
//   - holdingWriteTimeout bounds a write -- one statement through an op_* -- detached from the
//     client: legal.go's legalWriteTimeout, its reasoning and its value.
//
// A POST runs one of the two, never both (a refused form reads and writes nothing; an accepted one
// writes and reads nothing), so the longest a POST runs past its session gate is the larger:
// holdingWriteTimeout, the legal publication's.
const (
	holdingsReadTimeout = 5 * time.Second
	holdingWriteTimeout = 10 * time.Second
)

// holdingDone is what a write asked for, as the 303 after it names it: one of three words, and
// the only values holdingResultPath writes into a URL.
type holdingDone string

const (
	holdingCreated holdingDone = "created"
	holdingAdd     holdingDone = "add"
	holdingRemove  holdingDone = "remove"
)

// holdingResult is the query of the 303 after a write, parsed: what was asked, of which holding
// and (add, remove) of which business. Its zero value asks nothing.
type holdingResult struct {
	done            holdingDone
	holding, tenant uuid.UUID
}

// holdingResultPath is the list with what a write asked for -- the 303's target:
// /operator/holdings?done=<word>&holding=<id>[&tenant=<id>]. Every value is a word of holdingDone
// or a uuid's canonical text, so nothing a client sent reaches the Location but the two ids the
// handler parsed (or the database returned) -- TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions
// holds every redirect to a constant path or to this function.
func holdingResultPath(done holdingDone, holding, tenant uuid.UUID) string {
	p := pathHoldings + "?done=" + string(done) + "&holding=" + holding.String()
	if tenant != uuid.Nil {
		p += "&tenant=" + tenant.String()
	}
	return p
}

// holdingsPage is GET /operator/holdings: a page of the holdings and their memberships, and -- when
// the query names a write -- the sentence the read says about it.
//
// The refusals, in order: the session (storeSession); a page that is not 1..maxHoldingPage in
// plain digits -- 404 with no store call and no unit of the read budget. A malformed done, holding
// or tenant is not a refusal: the page renders with no sentence. Then the read budget and the read.
func (s *Surface) holdingsPage(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	page, ok := holdingPage(r.URL.Query().Get("page"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemHoldingsPageRefused)
		return
	}
	result := holdingResultOf(r.URL.Query().Get("done"), r.URL.Query().Get("holding"), r.URL.Query().Get("tenant"))
	rows, ok := s.readHoldings(w, r, id, hash, page)
	if !ok {
		return
	}
	s.render(w, r, http.StatusOK, operatorpages.Holdings(holdingsView(rows, page, holdingResultNotice(result, rows, page))))
}

// createHolding is POST /operator/holdings: a new, empty holding.
//
// The order, and what each refusal costs:
//
//	the session (storeSession)                 -> the sign-in
//	a body over maxFormBytes / unreadable      -> 413 / 400, no store call
//	a name the boundary refuses (holdingName)  -> 422, the list with the name's sentence: one
//	                                              read (and a unit of the read budget), no write
//	the write: a dead session                  -> the sign-in
//	the write: any other error (22023 among    -> 503, a fault page
//	them -- holdingName refuses more than the
//	database does, so it is not reached through
//	the form)
//	the write accepted                         -> 303 to the list, done=created and the new id
//
// From the form on, the request runs on answered(r) (vat.go's precedent): a client that leaves
// still gets the holding, and a half-closed one the same status and bytes.
func (s *Surface) createHolding(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemHoldingFormTooLarge) {
		return
	}
	r = answered(r)
	name, ok := holdingName(r.PostForm.Get("name"))
	if !ok {
		s.refuseHoldingForm(w, r, id, hash, holdingNoticeName)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), holdingWriteTimeout)
	defer cancel()
	holding, err := s.holdingStore.CreateHolding(ctx, hash, name)
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		// The session predicate passed in sessionGate and refused now: the session ended in
		// between. The sign-in, as the gate would answer.
		s.redirect(w, pathSignIn)
		return
	case err != nil:
		// internal/db's error is the call and a SQLSTATE (operatorErr); neither the name nor the
		// session hash is an argument of this line.
		s.log.ErrorContext(r.Context(), "operator: a holding could not be created", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemHoldingNotCreated)
		return
	}
	s.log.InfoContext(r.Context(), "operator created a holding",
		"holding_id", holding.String(), "admin_id", id.AdminID.String())
	s.redirect(w, holdingResultPath(holdingCreated, holding, uuid.Nil))
}

// addHoldingMember is POST /operator/holdings/{id}/members: ask the database to add the business
// the form names (tenant_id) to the holding the path names.
//
//	the session (storeSession)                 -> the sign-in
//	a holding id that is not the hyphenated    -> 404, no store call
//	form (tenantID's rule)
//	a body over maxFormBytes / unreadable      -> 413 / 400, no store call
//	a tenant_id that is not the hyphenated     -> 422, the list with the id's sentence: one read,
//	form, its ends trimmed                        no write
//	the write: a dead session                  -> the sign-in
//	the write: any other error                 -> 503, a fault page
//	the write accepted                         -> 303 to the list, done=add, the two ids -- and
//	                                              the list's read says whether it was added
func (s *Surface) addHoldingMember(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	holding, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemHoldingNotAnID)
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemHoldingFormTooLarge) {
		return
	}
	r = answered(r)
	tenant, ok := tenantID(strings.TrimSpace(r.PostForm.Get("tenant_id")))
	if !ok {
		s.refuseHoldingForm(w, r, id, hash, holdingNoticeTenantID)
		return
	}
	if !s.writeMembership(w, r, holding, tenant, func(ctx context.Context) error {
		return s.holdingStore.AddHoldingMember(ctx, hash, holding, tenant)
	}) {
		return
	}
	s.log.InfoContext(r.Context(), "operator asked to add a business to a holding",
		"holding_id", holding.String(), "tenant_id", tenant.String(), "admin_id", id.AdminID.String())
	s.redirect(w, holdingResultPath(holdingAdd, holding, tenant))
}

// removeHoldingMember is POST /operator/holdings/{id}/members/{tenant}/remove: ask the database to
// end the business's live membership of the holding. The two ids are the path's (the list's
// Remove button posts to it); the body is not read.
//
//	the session (storeSession)                 -> the sign-in
//	either id not the hyphenated form          -> 404, no store call
//	the write: a dead session                  -> the sign-in
//	the write: any other error                 -> 503, a fault page
//	the write accepted                         -> 303 to the list, done=remove, the two ids
func (s *Surface) removeHoldingMember(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	holding, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemHoldingNotAnID)
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "tenant"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemHoldingNotAnID)
		return
	}
	r = answered(r)
	if !s.writeMembership(w, r, holding, tenant, func(ctx context.Context) error {
		return s.holdingStore.RemoveHoldingMember(ctx, hash, holding, tenant)
	}) {
		return
	}
	s.log.InfoContext(r.Context(), "operator asked to remove a business from a holding",
		"holding_id", holding.String(), "tenant_id", tenant.String(), "admin_id", id.AdminID.String())
	s.redirect(w, holdingResultPath(holdingRemove, holding, tenant))
}

// writeMembership runs one membership write under holdingWriteTimeout, detached from the client,
// and answers its two failures itself: a dead session is the sign-in; any other error -- 22023
// among them (a constraint the database caught: nothing was written) -- is a 503 page that sends
// the operator to the list, whose read says what the database holds. It reports false when it
// has answered.
func (s *Surface) writeMembership(w http.ResponseWriter, r *http.Request, holding, tenant uuid.UUID,
	write func(context.Context) error) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), holdingWriteTimeout)
	defer cancel()
	err := write(ctx)
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return false
	case err != nil:
		s.log.ErrorContext(r.Context(), "operator: a holding membership change was not confirmed",
			"holding_id", holding.String(), "tenant_id", tenant.String(), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemHoldingChangeNotConfirmed)
		return false
	}
	return true
}

// refuseHoldingForm answers a form the boundary refused: 422 and the list (page 1) with the
// refusal's sentence above it. It is a read -- one unit of the read budget, the read's own
// refusals (readHoldings) -- and it writes nothing.
func (s *Surface) refuseHoldingForm(w http.ResponseWriter, r *http.Request, id operatorauth.Identity, hash string,
	notice holdingNotice) {
	rows, ok := s.readHoldings(w, r, id, hash, 1)
	if !ok {
		return
	}
	s.render(w, r, http.StatusUnprocessableEntity, operatorpages.Holdings(holdingsView(rows, 1, notice)))
}

// readHoldings charges the read's unit of the read budget, reads the page under
// holdingsReadTimeout, and answers the read's refusals itself: a session the read refuses is the
// sign-in's 303; any other error is a 503 whose log line names the page and not the session's
// hash. It reports false when it has answered.
func (s *Surface) readHoldings(w http.ResponseWriter, r *http.Request, id operatorauth.Identity, hash string,
	page int32) ([]db.HoldingRow, bool) {
	if !s.spendRead(w, r, id) {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), holdingsReadTimeout)
	defer cancel()
	rows, err := s.holdingStore.Holdings(ctx, hash, page)
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return nil, false
	case err != nil:
		s.log.ErrorContext(r.Context(), "operator: the holdings could not be read", "page", int(page), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemHoldingsUnreadable)
		return nil, false
	}
	return rows, true
}

// holdingPage is the query's page number: absent or empty is the first page; otherwise decimal
// digits only, 1..maxHoldingPage -- the tenant pager's rule (tenantPage). No sign, no space, no
// exponent.
func holdingPage(raw string) (int32, bool) {
	if raw == "" {
		return 1, true
	}
	if len(raw) > len(strconv.Itoa(maxHoldingPage)) {
		return 0, false
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > maxHoldingPage {
		return 0, false
	}
	return int32(n), true
}

// holdingName is the posted name as op_create_holding is sent it, or false for one this screen
// refuses: its ends trimmed of white space, then valid UTF-8, 1..db.MaxHoldingNameRunes
// characters, no control character (C0, C1 -- NUL and line breaks among them), no line or
// paragraph separator, and at least one character a reader can see (legal.go's visibleText).
//
// IT REFUSES MORE THAN THE DATABASE DOES, ON PURPOSE: holdings_name_shape (00041) asks for
// `name = btrim(name)`, 1..120 characters and no `[:cntrl:]`; a name of zero-width characters or
// one spanning two lines passes it and would show nothing (or break the list's line). So a 22023
// from the write is not reached through the form, and is answered as a fault (createHolding).
func holdingName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > db.MaxHoldingNameRunes {
		return "", false
	}
	for _, c := range name {
		if unicode.IsControl(c) || c == '\u2028' || c == '\u2029' {
			return "", false
		}
	}
	if !visibleText(name) {
		return "", false
	}
	return name, true
}

// holdingResultOf parses the 303's query. Anything it does not take -- a word that is not one of
// the three, an id that is not the hyphenated form, an add or a removal without its business --
// is the zero result: no sentence, and nothing of the query on the page.
func holdingResultOf(done, holding, tenant string) holdingResult {
	d := holdingDone(done)
	if d != holdingCreated && d != holdingAdd && d != holdingRemove {
		return holdingResult{}
	}
	h, ok := tenantID(holding)
	if !ok {
		return holdingResult{}
	}
	res := holdingResult{done: d, holding: h}
	if d == holdingCreated {
		return res
	}
	t, ok := tenantID(tenant)
	if !ok {
		return holdingResult{}
	}
	res.tenant = t
	return res
}

// holdingNotice is the sentence above the list: a write's outcome as the read shows it, or a
// refused form's reason. holdingNoticeNone draws nothing.
type holdingNotice int

const (
	holdingNoticeNone holdingNotice = iota
	holdingNoticeCreated
	holdingNoticeAdded
	holdingNoticeNotAdded
	holdingNoticeRemoved
	holdingNoticeNotRemoved
	holdingNoticeStillLive
	holdingNoticeElsewhere
	holdingNoticeName
	holdingNoticeTenantID
)

// holdingSentence is one notice's tone, heading and sentence; rules says the database's four
// rules for an add follow it.
type holdingSentence struct {
	tone              operatorpages.HoldingNoticeTone
	heading, sentence string
	rules             bool
}

// holdingSentences are the notices' words (skill tappa-brand: a message does not blame; it says
// what happened and what to do). Each outcome is a fact the read showed, said as such: "Added."
// when the business is a live member of the holding, "Removed." when its latest membership of it
// has ended. Pinned word for word: TestHoldings_TheResultIsReadNotPosted.
var holdingSentences = map[holdingNotice]holdingSentence{
	holdingNoticeCreated: {operatorpages.HoldingNoticeOK, "Created.",
		"The new holding is in the list below.", false},
	holdingNoticeAdded: {operatorpages.HoldingNoticeOK, "Added.",
		"The business is a live member of this holding.", false},
	holdingNoticeNotAdded: {operatorpages.HoldingNoticeWarn, "Not added — nothing changed.",
		"The database adds a business only when every rule below holds. The list shows each member's timezone and API code.", true},
	holdingNoticeRemoved: {operatorpages.HoldingNoticeOK, "Removed.",
		"The business can join a holding again, and its owner can change its timezone, after 60 seconds.", false},
	holdingNoticeNotRemoved: {operatorpages.HoldingNoticeWarn, "Nothing was removed — it was not a live member.",
		"The list below shows the holding as the database holds it now.", false},
	holdingNoticeStillLive: {operatorpages.HoldingNoticeWarn, "It is a live member of this holding.",
		"The list below shows its membership as the database holds it now. Remove it from there if it should leave.", false},
	holdingNoticeElsewhere: {operatorpages.HoldingNoticeWarn, "The result is not on this page.",
		"This page does not hold the whole holding, so it cannot say what happened. Find the holding below or on the next pages.", false},
	holdingNoticeName: {operatorpages.HoldingNoticeWarn, "That name cannot be used",
		"A holding's name is one line of 1 to 120 characters, with at least one visible character. Nothing was created.", false},
	holdingNoticeTenantID: {operatorpages.HoldingNoticeWarn, "That is not a business id",
		"Paste the business's id from its overview: 36 characters of letters, digits and hyphens. Nothing was changed.", false},
}

// holdingAddRules are the database's rules for an add (op_add_holding_member, 00042), in the
// order the list shows the facts that decide them.
var holdingAddRules = []string{
	"Its timezone is the timezone of every member of the holding.",
	"It is in no other holding, and at least 60 seconds have passed since it last left one.",
	"Its API code is not one a member of the holding already has.",
	"The holding and the business both exist.",
}

// holdingGroup is one holding of a page: its rows in the read's order, and where they sit on it.
type holdingGroup struct {
	rows        []db.HoldingRow
	first, last bool
}

// holdingGroups splits a page's rows into holdings, in the read's order (newest holding first).
func holdingGroups(rows []db.HoldingRow) []holdingGroup {
	var out []holdingGroup
	for _, r := range rows {
		if n := len(out); n > 0 && out[n-1].rows[0].HoldingID == r.HoldingID {
			out[n-1].rows = append(out[n-1].rows, r)
			continue
		}
		out = append(out, holdingGroup{rows: []db.HoldingRow{r}})
	}
	if len(out) > 0 {
		out[0].first, out[len(out)-1].last = true, true
	}
	return out
}

// holdingResultNotice is the sentence the page's own read says about a write: never what the POST
// was answered (it was accepted, whatever the database found).
//
// THE READ IS A PAGE, so a sentence is said only where the page holds the evidence. A holding's
// rows are WHOLE on the page unless it is the page's last group on a full page (it may go on to
// the next) or its first group on a page after the first (it may have begun on the one before);
// and an absent holding is ABSENT only on a first page that is not full (that page is every row).
//
// THE READ SAYS WHAT IS, NOT WHO DID IT (a stated limit): "Added." is said for a live membership
// and "Removed." for a latest membership that has ended, whether this write or an earlier one made
// it so -- a form posted twice, or a stale page's Remove button, gets the same sentence. The read
// has no field that could tell the two apart (op_read_holdings returns the membership, not the
// call that wrote it), and the writes' answers say nothing by design.
//
//	created  the holding on the page                               -> Created.
//	         absent, and the page is every row                       -> nothing
//	add      the business a live member of the holding on the page   -> Added.
//	         the holding whole and the business not a live member,
//	         or the holding absent                                   -> Not added -- and the rules
//	remove   the holding whole: the business's latest membership of
//	         it ended                                                -> Removed.
//	         a live membership of it                                 -> it is a live member
//	         no membership of it, or the holding absent              -> Nothing was removed
//	any      the page cannot tell                                    -> the result is not here
func holdingResultNotice(res holdingResult, rows []db.HoldingRow, page int32) holdingNotice {
	if res.done == "" {
		return holdingNoticeNone
	}
	groups := holdingGroups(rows)
	full := len(rows) >= db.HoldingsPerPage
	everything := page == 1 && !full
	var g *holdingGroup
	for i := range groups {
		if groups[i].rows[0].HoldingID == res.holding {
			g = &groups[i]
			break
		}
	}
	whole := g != nil && !(full && g.last) && !(page > 1 && g.first)
	switch res.done {
	case holdingCreated:
		switch {
		case g != nil:
			return holdingNoticeCreated
		case everything:
			// The read holds every holding and not this one: a link that names no holding (the
			// write's own 303 names the id the database returned). Nothing to say about it.
			return holdingNoticeNone
		}
	case holdingAdd:
		if g != nil && liveMember(g.rows, res.tenant) {
			return holdingNoticeAdded
		}
		if whole || (g == nil && everything) {
			return holdingNoticeNotAdded
		}
	case holdingRemove:
		switch {
		case whole:
			for _, r := range g.rows {
				if r.MemberTenantID != nil && *r.MemberTenantID == res.tenant {
					// The read's order puts a holding's live memberships first and its ended
					// ones newest first, so the business's first row is its latest membership.
					if r.LeftAt == nil {
						return holdingNoticeStillLive
					}
					return holdingNoticeRemoved
				}
			}
			return holdingNoticeNotRemoved
		case g == nil && everything:
			return holdingNoticeNotRemoved
		}
	}
	return holdingNoticeElsewhere
}

// liveMember reports whether rows hold a live membership of tenant.
func liveMember(rows []db.HoldingRow, tenant uuid.UUID) bool {
	for _, r := range rows {
		if r.MemberTenantID != nil && *r.MemberTenantID == tenant && r.LeftAt == nil {
			return true
		}
	}
	return false
}

// holdingsView builds the screen: the notice, then the page's holdings with their memberships.
// Next is offered only after a FULL page and never past maxHoldingPage; a full last page is
// followed by an empty one, which the screen says. A holding or a business whose name shows
// nothing is named by its id (namedVisibly).
func holdingsView(rows []db.HoldingRow, page int32, notice holdingNotice) operatorpages.HoldingsView {
	v := operatorpages.HoldingsView{
		Page:    int(page),
		HasNext: len(rows) >= db.HoldingsPerPage && page < maxHoldingPage,
	}
	if n, ok := holdingSentences[notice]; ok {
		v.NoticeTone, v.NoticeHeading, v.Notice = n.tone, n.heading, n.sentence
		if n.rules {
			v.NoticeRules = holdingAddRules
		}
	}
	groups := holdingGroups(rows)
	for i, g := range groups {
		head := g.rows[0]
		h := operatorpages.HoldingView{
			ID:          head.HoldingID.String(),
			CreatedAt:   utcStamp(head.HoldingCreatedAt),
			MembersPath: holdingMembersPath(head.HoldingID),
			// A holding's rows can be cut by the page: say so where it happens.
			ContinuedFromBefore: page > 1 && i == 0,
			ContinuesAfter:      v.HasNext && i == len(groups)-1,
		}
		if namedVisibly(head.HoldingName) {
			h.Name = head.HoldingName
		}
		for _, r := range g.rows {
			if r.MemberTenantID == nil {
				continue
			}
			h.Members = append(h.Members, holdingMemberView(head.HoldingID, r))
		}
		v.Holdings = append(v.Holdings, h)
	}
	return v
}

// holdingMemberView is one membership as the list says it: the business (its overview a link),
// its timezone and API code (what decides an add), when it joined and -- ended -- left (UTC), and
// for a live membership the path its Remove button posts to.
func holdingMemberView(holding uuid.UUID, r db.HoldingRow) operatorpages.HoldingMemberView {
	t := *r.MemberTenantID
	m := operatorpages.HoldingMemberView{
		TenantID:   t.String(),
		TenantPath: pathTenants + "/" + t.String(),
		Live:       r.LeftAt == nil,
	}
	if r.MemberName != nil && namedVisibly(*r.MemberName) {
		m.Name = *r.MemberName
	}
	if r.MemberTimezone != nil {
		m.Timezone = *r.MemberTimezone
	}
	if r.MemberAPICode != nil {
		m.APICode = *r.MemberAPICode
	}
	if r.JoinedAt != nil {
		m.JoinedAt = utcStamp(*r.JoinedAt)
	}
	if r.LeftAt != nil {
		m.LeftAt = utcStamp(*r.LeftAt)
	} else {
		m.RemovePath = holdingRemovePath(holding, t)
	}
	return m
}

// holdingMembersPath is a holding's add form's action, /operator/holdings/<id>/members.
func holdingMembersPath(holding uuid.UUID) string {
	return pathHoldings + "/" + holding.String() + "/members"
}

// holdingRemovePath is a live membership's Remove button's action,
// /operator/holdings/<id>/members/<tenant>/remove.
func holdingRemovePath(holding, tenant uuid.UUID) string {
	return holdingMembersPath(holding) + "/" + tenant.String() + "/remove"
}
