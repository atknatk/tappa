package operator

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// THE TENANT SCREENS (M10 OP-11, phase B; ADR 0020 §4, §9; ADR 0021 §2 v, OP-11 note).
// The list of tenants with its search, and one tenant's overview -- the first operator
// screens that show TENANT data. All three routes sit in the console's group (routes.go,
// mount): host gate, security headers, flood gate, same-origin gate (a GET that a browser
// labels same-site or cross-site is the sign-in redirect), requireOperator, sessionGate.
//
// EVERY VIEW IS A TWO-PHASE READ through TenantStore: op_begin_read commits the read's
// audit row, then op_read_tenants / op_read_tenant_detail consume the ticket. The ticket
// never reaches this package (internal/db hands it from one phase to the other); this
// file writes no SQL. Each view is charged TWICE -- sessionGate one unit of the session's
// request budget, the handler one unit of its read budget (spendRead; until OP-13 phase B
// the second unit was the session budget's too) -- because it is two database
// transactions (surface.go, sessionLimit and readLimit).
//
// THE SEARCH TERM IS SENT IN THE REQUEST'S BODY, NOT IN ITS URL. A term can be a panel
// account's email address -- personal data. In a URL it would be written to the
// ingress's access log and the browser's history (the enrollment token left the query
// string for the same reason, ADR 0020 §3.5); this process's own access record logs the
// route pattern, never the URL (internal/httpx's requestlog). So:
//   - GET /operator/tenants is the FIRST PAGE OF EVERY TENANT and reads nothing from the
//     URL: a ?q= or ?page= there is not a search;
//   - POST /operator/tenants carries the term (q) and the page (page) in its form, and
//     every page of a search -- of an empty term too -- is another POST (the pager's
//     forms carry both as hidden fields). The POST answers 200 with the page: there is
//     no redirect, because a redirect's GET would need the term in its URL, a cookie or
//     state on the server; a reload that resubmits the form (a browser asks first: the
//     answer is a POST's and no-store) is one more read, which the audit records as one;
//   - the term is held as a formValue (form.go: it prints its placeholder) from the form
//     to the two places it is meant to go -- the store's query and the page's own search
//     box, "matching" line and pager -- escaped by templ.
//
// WHERE THE TERM GOES, BY DESIGN: the result page it was searched from; internal/db,
// which binds it to op_begin_read and op_read_tenants (and a database that logs its
// statements with their parameters -- development's log_statement = all -- writes it to
// its own server log; production logs none). WHERE IT WAS MEASURED NOT TO GO: on the
// listed arms of TestTenantSearch_NoLogLineHeaderOrOtherPageCarriesTheTerm (through a real
// server: the process and access log, every response header -- Location among them --
// and the body of every page but the result page) and of
// TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor (G17, arms A44-A59); and in the
// audit rows and read tickets of TestE2E_TenantScreensReadThroughTheDefinersAndAuditEachRead,
// where the audit row records the term's CLASS (none, text, address, id) and the ticket
// row a hash bound to it (00029; ADR 0021, OP-11 note md. 3).

// TenantStore is the operator database's slice the tenant screens need, declared at the
// consumer (CLAUDE.md §7). *db.OperatorDB implements it by delegating to internal/db's
// TenantList and TenantDetail (TestOperatorDB_EveryMethodDelegatesVerbatim), and that
// type's method set is derived from this interface, LegalStore and operatorauth.Store
// (TestOperatorDB_IsTheStoreAndNothingMore). sessionHash is operatorauth's SessionHash of
// the request's token -- on ADR 0020 §5's never-log list. The two signatures carry no
// read ticket: each method is both phases.
type TenantStore interface {
	// TenantList is op_begin_read + op_read_tenants: a page of the tenant list, newest
	// first, filtered by q.Search (a name substring, a panel account's whole address, a
	// tenant id; "" for every tenant). A term internal/db will not send is
	// db.ErrTenantSearchRefused; a dead session db.ErrOperatorRefused.
	TenantList(ctx context.Context, sessionHash string, q db.TenantListQuery) ([]db.TenantSummary, error)
	// TenantDetail is op_begin_read + op_read_tenant_detail: one tenant's identity and four
	// counts. An id no tenant has is db.ErrNoSuchTenant -- after the 'read' row naming it
	// committed.
	TenantDetail(ctx context.Context, sessionHash string, tenantID uuid.UUID) (db.TenantOverview, error)
}

// tenantPageSize is the screens' page: 50 tenants, the panel's roster page (M6-05). The
// database caps a page at 200 (ADR 0021 §2 iii); the size is this constant, never the
// client's.
const tenantPageSize = 50

// maxTenantPage bounds the page a search may ask for. A page is OFFSET (page-1) x size,
// and the list's cost grows with the offset: the database sorts everything up to it
// (00029 has no index for the order; the OP-11 A card's md. 12 measured a 200-row page
// 2 000 at 580 ms on 586 538 development tenants). 1 000 pages of 50 reach the newest
// 50 000 tenants -- production counts tenants in tens -- and past that the search finds a
// tenant; the pager offers no page beyond it.
const maxTenantPage = 1000

// tenantList is GET /operator/tenants: the first page of every tenant, newest first. It
// reads nothing from the URL (the file's comment).
func (s *Surface) tenantList(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	s.listTenants(w, r, id, hash, formValue{}, 1)
}

// searchTenants is POST /operator/tenants: a page of the tenants the posted term finds.
//
// The refusals, each before any store call and each a fixed page that does not repeat
// the term: a request body over maxFormBytes (413), an unreadable form (400), a term this
// screen will not send (400: tenantSearchTerm), a page outside 1..maxTenantPage (400).
func (s *Surface) searchTenants(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemTenantSearchTooLarge) {
		return
	}
	term, ok := tenantSearchTerm(postValue(r, "q"))
	if !ok {
		s.problem(w, r, http.StatusBadRequest, problemTenantSearchRefused)
		return
	}
	page, ok := tenantPage(r.PostForm.Get("page"))
	if !ok {
		s.problem(w, r, http.StatusBadRequest, problemTenantPageRefused)
		return
	}
	s.listTenants(w, r, id, hash, term, page)
}

// listTenants charges the read's unit of the read budget, reads the page and renders it.
func (s *Surface) listTenants(w http.ResponseWriter, r *http.Request, id operatorauth.Identity, hash string, term formValue, page int32) {
	if !s.spendRead(w, r, id) {
		return
	}
	rows, err := s.tenantStore.TenantList(r.Context(), hash,
		db.TenantListQuery{Search: term.reveal(), Number: page, Size: tenantPageSize})
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		// The session predicate passed in sessionGate and refused now: the session ended
		// in between. The sign-in, as the gate would answer.
		s.redirect(w, pathSignIn)
		return
	case errors.Is(err, db.ErrTenantSearchRefused):
		// tenantSearchTerm refuses a superset of what internal/db refuses (invalid UTF-8,
		// a NUL, more than db.MaxTenantSearchRunes characters), so this is not reached
		// through the form; it is answered as that refusal would be.
		s.problem(w, r, http.StatusBadRequest, problemTenantSearchRefused)
		return
	case err != nil:
		// internal/db's error is the call and a SQLSTATE (operatorErr); it carries neither
		// the term nor the session hash (internal/db's
		// TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions reads three of its texts),
		// and neither is an argument of this line.
		s.log.ErrorContext(r.Context(), "operator: the tenant list could not be read", "page", int(page), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemTenantsUnreadable)
		return
	}
	s.render(w, r, http.StatusOK, operatorpages.Tenants(tenantsView(rows, term, page)))
}

// tenantOverview is GET /operator/tenants/{id}: one tenant's overview, its name in the
// banner (operatorpages.TenantScreen; ADR 0020 §9).
//
// The id is read from the path and checked HERE: the 36-character hyphenated form, in
// either case (tenantID) -- anything else is a 404 with no store call and no unit of the
// read budget. An id that names no tenant is a 404 too, a different page, after the
// database has committed the 'read' row naming it (00029: phase one does not look the
// tenant up, so a refused call cannot tell anyone whether it exists).
func (s *Surface) tenantOverview(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	if !s.spendRead(w, r, id) {
		return
	}
	o, err := s.tenantStore.TenantDetail(r.Context(), hash, tenant)
	switch {
	case errors.Is(err, db.ErrNoSuchTenant):
		s.problem(w, r, http.StatusNotFound, problemNoSuchTenant)
		return
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return
	case err != nil:
		// A tenant's id is not personal data and names what could not be read; the hash
		// is not an argument of this line.
		s.log.ErrorContext(r.Context(), "operator: a tenant's overview could not be read", "tenant_id", tenant.String(), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemTenantUnreadable)
		return
	}
	name, err := tenantBanner(o.ID, o.Name)
	if err != nil {
		// Not reached: tenantBanner names a tenant without a visible name by its id,
		// which the database always returns. Were it reached, the zero name makes
		// TenantScreen refuse and render answers a plain 500 -- this line says why first.
		s.log.ErrorContext(r.Context(), "operator: a tenant could not be named on its screen", "tenant_id", o.ID.String(), "err", err)
	}
	s.render(w, r, http.StatusOK, operatorpages.TenantOverview(tenantOverviewView(o, name)))
}

// tenantSearchTerm is the posted term as the list sends it, or false for one this screen
// refuses: its ends are trimmed of white space (a pasted address keeps a trailing space
// often enough; a name search for leading or trailing space finds nothing useful), and
// what is left must be valid UTF-8, at most db.MaxTenantSearchRunes characters, with no
// control character (C0, C1 -- NUL and line breaks among them) and no line or paragraph
// separator: a search is one line. internal/db refuses invalid UTF-8, a NUL and a longer
// term itself (ErrTenantSearchRefused, no round trip); this rule is that one and more,
// checked at the boundary (CLAUDE.md §7). An empty term is every tenant.
//
// The term stays a formValue on its way out, so a log attribute or a %v given it by
// mistake prints the placeholder.
func tenantSearchTerm(posted formValue) (formValue, bool) {
	term := strings.TrimSpace(posted.reveal())
	if !utf8.ValidString(term) || utf8.RuneCountInString(term) > db.MaxTenantSearchRunes {
		return formValue{}, false
	}
	for _, r := range term {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return formValue{}, false
		}
	}
	return formValue{v: &term}, true
}

// tenantPage is the posted page number: absent or empty is the first page; otherwise
// decimal digits only, 1..maxTenantPage. No sign, no space, no exponent.
func tenantPage(raw string) (int32, bool) {
	if raw == "" {
		return 1, true
	}
	if len(raw) > len(strconv.Itoa(maxTenantPage)) {
		return 0, false
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > maxTenantPage {
		return 0, false
	}
	return int32(n), true
}

// tenantID is the path's tenant id: the 36-character hyphenated form, either case. The
// other forms uuid.Parse takes (braces, urn:uuid:, 32 hex digits) are not ids of this
// screen's links and are refused, so one tenant has one path.
func tenantID(raw string) (uuid.UUID, bool) {
	if len(raw) != 36 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// namedVisibly reports whether a tenant's name has a character a reader can see --
// legal.go's visibleText rule. tenants.name has no CHECK against an empty or blank name
// (the OP-11 A card's md. 14.5; dev held none, measured), and a name of white space,
// zero-width or filler characters would render a banner and a link that name nobody.
func namedVisibly(name string) bool { return visibleText(name) }

// tenantBanner is the name a tenant's screen carries in its banner and title: the
// tenant's own name, or -- when the name has no visible character -- the placeholder
// that names the tenant by its id (operatorpages.UnnamedTenant). The overview and the
// plaque screen (OP-13) both call it, with the id and the name their one read returned.
func tenantBanner(id uuid.UUID, name string) (operatorpages.TenantName, error) {
	if namedVisibly(name) {
		return operatorpages.NewTenantName(name)
	}
	return operatorpages.UnnamedTenant(id.String())
}

// tenantsView builds the list screen. Next is offered only after a FULL page (a page of
// fewer rows is the last) and never past maxTenantPage; a full last page is followed by
// an empty one, which the screen says.
func tenantsView(rows []db.TenantSummary, term formValue, page int32) operatorpages.TenantsView {
	v := operatorpages.TenantsView{
		Search:  term.reveal(),
		Page:    int(page),
		HasNext: len(rows) >= tenantPageSize && page < maxTenantPage,
	}
	for _, x := range rows {
		row := operatorpages.TenantRow{
			ID:        x.ID.String(),
			Path:      pathTenants + "/" + x.ID.String(),
			CreatedAt: utcStamp(x.CreatedAt),
			Plan:      x.Plan,
		}
		if namedVisibly(x.Name) {
			row.Name = x.Name
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}

// tenantOverviewView builds the overview screen: the identity facts and the four counts,
// formatted for the page's mono figures, and (OP-13) the path of the tenant's plaques.
func tenantOverviewView(o db.TenantOverview, name operatorpages.TenantName) operatorpages.TenantOverviewView {
	count := func(n int64) string { return strconv.FormatInt(n, 10) }
	return operatorpages.TenantOverviewView{
		Name:            name,
		ID:              o.ID.String(),
		PlaquesPath:     plaquesPath(o.ID),
		CreatedAt:       utcStamp(o.CreatedAt),
		Plan:            o.Plan,
		BusinessType:    o.BusinessType,
		Locations:       count(o.Locations),
		ActiveEmployees: count(o.ActiveEmployees),
		ActivePlaques:   count(o.ActivePlaques),
		ActiveAdmins:    count(o.ActiveAdmins),
	}
}
