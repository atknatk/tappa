package operator_test

// op15_test.go -- M10 OP-15 phase C: a tenant's suspension on the operator surface, on the shipped
// router with the fake store (rig_test.go). The header table of the two writes' response classes
// (C218-C242); the answer that is the same whatever the tenant held; the tenant that is the path's;
// the box and the closed reason set; the refusals that write nothing; a write that survives a
// client that leaves or half-closes; the write's bound; the budgets; no posted value on a log
// line; the overview's state line, its one form and the security hold's closed warning; the list's
// chip; the chips' contrast and rules; the reason labels; and the package's structural pins on the
// two writes. Against PostgreSQL: op15_db_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/types"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// ------------------------------------------------------------------ fixtures --

// The header table's two tenant ids: fixed, so the 303s' Location is a designed constant
// (designedHeaders, op8r2_test.go). susHeaderUnknown names no tenant.
var (
	susHeaderTenant  = uuid.MustParse("51500000-0000-4000-8000-000000000015")
	susHeaderUnknown = uuid.MustParse("51500000-0000-4000-8000-000000000099")
)

// errFakeSuspendRefused is the fake's answer for a write the database refuses (SQLSTATE 22023), in
// internal/db's spelling of a database error.
var errFakeSuspendRefused = errors.New("fake: db: suspend tenant: database error (SQLSTATE 22023)")

func suspendPathOf(id string) string   { return "/operator/tenants/" + id + "/suspend" }
func reinstatePathOf(id string) string { return "/operator/tenants/" + id + "/reinstate" }
func overviewPathOf(id string) string  { return "/operator/tenants/" + id }

// suspendForm and reinstateForm are the two forms as the overview's page posts them, the box
// ticked.
func suspendForm(reason string) url.Values { return url.Values{"reason": {reason}, "confirm": {"yes"}} }
func reinstateForm() url.Values            { return url.Values{"confirm": {"yes"}} }

// seedTenant puts one tenant's overview into the fake (a new id for uuid.Nil) and returns the id.
func (g *rig) seedTenant(id uuid.UUID, name string, since *time.Time, reason db.SuspensionReason) uuid.UUID {
	g.t.Helper()
	if id == uuid.Nil {
		id = uuid.New()
	}
	return g.seedOverview(db.TenantOverview{ID: id, Name: name, Plan: "founding", BusinessType: "restaurant",
		CreatedAt: time.Date(2026, 7, 5, 9, 30, 0, 0, time.UTC), SuspendedAt: since, SuspensionReason: reason})
}

func (g *rig) suspensionWrites() []fakeSuspensionWrite {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return append([]fakeSuspensionWrite(nil), g.store.suspensionWrites...)
}

func (g *rig) setStoreFailure(m string, err error) {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	if err == nil {
		delete(g.store.fail, m)
	} else {
		g.store.fail[m] = err
	}
}

// The screen's words, written out here rather than read from the handler, so a changed word is
// red.
const (
	wordNonPayment      = "Non-payment"
	wordTermsBreach     = "Breach of the terms"
	wordSecurityHold    = "Security hold"
	wordCustomerRequest = "At the business&#39;s request" // as the page escapes it

	chipActive    = `<span class="tally tally--tenant-active">Active</span>`
	chipSuspended = `<span class="tally tally--tenant-suspended">Suspended</span>`

	holdHeading = "A security hold freezes writes. It does not contain an account."
)

// holdSentences are the security hold warning's closed text, sentence by sentence as the page
// writes it -- static template text, which templ writes as typed (ADR 0025 §5): what stays open to whoever holds a session (reads, the CSV, staff,
// invitations -- which sign a new phone in and the employee's other phones out --, manual
// records with no approval that a correction can only shorten), what closes to the business, the
// banner, and the three acts that stop an account (disabling, a password reset that asks for no
// current password and ends every session, a change of one's own password).
var holdSentences = []string{
	"Whoever holds a session of this business keeps it. While the business is suspended, that session still:",
	"reads every section and exports the CSV files;",
	"adds people to the staff and sends new invitations — an invitation signs a new phone in as an employee and signs that employee's other phones out;",
	"enters manual records, which are written with no approval and count in the hours at once. A record that cuts a real employee's hours cannot be undone by another record: a correction can only shorten.",
	"The business's own defensive writes stop too: it cannot deactivate an employee or replace or remove a plaque. Every section shows a banner saying its writes are suspended — the sentence names no reason, but the freeze itself tells the session holder something changed.",
	"What does stop an account:",
	"disabling the panel account — today the platform owner sets it to disabled in the database;",
	"a password reset by the business's rightful admin — it does not ask for the current password and ends every session of that account, so it is the tool when the password may have been changed;",
	"the rightful admin changing their own password — it ends their other sessions, and asks for the current password.",
}

// spaced is html with every run of white space made one space -- templ keeps the template's line
// breaks inside a text, and a sentence is compared word for word.
func spaced(s string) string { return strings.Join(strings.Fields(s), " ") }

// ------------------------------------------------------------ the header table --

// TestOperatorHeaders_TheSuspensionClassesCarryThePolicy drives the twenty-five response classes
// of the suspension's two writes (C218-C242) with the 40-class test's hostile drive and checks
// (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these twenty-five, at WriteHeader (the recorder's Result().Header): the
// designed status, route, header names and values (designedHeaders: no Location but the sign-in's
// and, on the six accepted writes, the overview of the PATH's tenant -- an id no tenant has
// included); no hostile value (hostileValues) in a header value or in the body, raw or
// query-escaped -- every request carries the hostile query, which holds a reason, a confirm value
// and another tenant's id under the name id (hostileDrive), and every POST's body carries an id
// and a tenant field naming that other id, so a write that took its tenant from the request
// rather than its path, or echoed a posted value, is red here; no script; and the counts below:
// C221, C223, C231, C235, C236 and C242 make no store call; C222 no write call; C224-C228 and
// C237-C239 no write call; the six accepted classes ONE write call each, for the path's tenant
// and (suspend) the posted reason; the two failing classes and the two refused sessions one call
// and no write.
//
// PART II -- the list above, on these twenty-five classes.
//
// PART III -- This test measures the twenty-five classes and the raw and query-escaped forms
// only; anything else (examples: a class a later handler adds, a hostile value echoed
// HTML-escaped or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_TheSuspensionClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	g.seedTenant(susHeaderTenant, "FAKE Header Suspension Ltd", nil, "")
	tenant, unknown := susHeaderTenant.String(), susHeaderUnknown.String()
	ad := &addrs{}
	last := &driveLog{}
	send := func(r req) *httptest.ResponseRecorder {
		if r.remote == "" {
			r.remote = ad.next() + ":1"
		}
		r.host = opHost
		*last = driveLog{r.method, r.path}
		return g.do(hostileDrive(r))
	}
	hostile := func(f url.Values) url.Values {
		out := url.Values{"id": {hostileQueryID}, "tenant": {hostileQueryID}}
		for k, v := range f {
			out[k] = v
		}
		return out
	}
	post := func(path string, f url.Values, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: path, form: hostile(f), origin: opOrigin, cookies: c}
	}
	signIn := func() *http.Cookie {
		t.Helper()
		f := g.active()
		ch := cookie(send(req{method: http.MethodPost, path: "/operator/login", origin: opOrigin,
			form: url.Values{"email": {f.email}, "password": {f.password}}}), operatorauth.ChallengeCookieName)
		if ch == nil {
			t.Fatal("PREMISE: no challenge")
		}
		s := cookie(send(req{method: http.MethodPost, path: "/operator/login/totp", origin: opOrigin,
			form: url.Values{"code": {totpAt(f.key, g.now)}}, cookies: []*http.Cookie{ch}}), operatorauth.SessionCookieName)
		if s == nil {
			t.Fatal("PREMISE: no session")
		}
		return s
	}
	calls := map[string]map[string]int{}
	counted := func(class string, r func(c *http.Cookie) req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			rq := r(signIn())
			sus, rei, writes, total := g.store.count("SuspendTenant"), g.store.count("ReinstateTenant"), len(g.suspensionWrites()), g.store.total()
			w := send(rq)
			calls[class] = map[string]int{"SuspendTenant": g.store.count("SuspendTenant") - sus, "ReinstateTenant": g.store.count("ReinstateTenant") - rei,
				"writes": len(g.suspensionWrites()) - writes, "all": g.store.total() - total}
			return w
		}
	}
	failing := func(m string, err error, class string, r func(c *http.Cookie) req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			g.setStoreFailure(m, err)
			defer g.setStoreFailure(m, nil)
			return counted(class, r)()
		}
	}
	malformed := strings.ReplaceAll(tenant, "-", "")
	dead := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}
	big := strings.Repeat("9", 20<<10)
	cross := func(rq req) req {
		rq.origin, rq.header = "https://taptime.mt", map[string]string{"Sec-Fetch-Site": "same-site"}
		return rq
	}
	sus, rei := suspendPathOf, reinstatePathOf
	classes := []headerClass{
		{"C218 suspend, a tenant that is not suspended", 303, counted("C218", func(c *http.Cookie) req {
			return post(sus(tenant), suspendForm("non_payment"), c)
		})},
		{"C219 suspend, a tenant already suspended", 303, counted("C219", func(c *http.Cookie) req {
			return post(sus(tenant), suspendForm("security_hold"), c)
		})},
		{"C220 suspend, an id no tenant has", 303, counted("C220", func(c *http.Cookie) req {
			return post(sus(unknown), suspendForm("terms_breach"), c)
		})},
		{"C221 suspend without a cookie", 303, counted("C221", func(*http.Cookie) req {
			return post(sus(tenant), suspendForm("non_payment"))
		})},
		{"C222 suspend with a dead cookie", 303, counted("C222", func(*http.Cookie) req {
			return post(sus(tenant), suspendForm("non_payment"), dead)
		})},
		{"C223 suspend, cross-origin", 403, counted("C223", func(c *http.Cookie) req {
			return cross(post(sus(tenant), suspendForm("non_payment"), c))
		})},
		{"C224 suspend, a malformed id", 404, counted("C224", func(c *http.Cookie) req {
			return post(sus(malformed), suspendForm("non_payment"), c)
		})},
		{"C225 suspend, the box not ticked", 400, counted("C225", func(c *http.Cookie) req {
			return post(sus(tenant), url.Values{"reason": {"non_payment"}}, c)
		})},
		{"C226 suspend, a reason outside the set", 400, counted("C226", func(c *http.Cookie) req {
			return post(sus(tenant), url.Values{"reason": {"qry_hostile_reason"}, "confirm": {"yes"}}, c)
		})},
		{"C227 suspend, an oversized form", 413, counted("C227", func(c *http.Cookie) req {
			return post(sus(tenant), url.Values{"reason": {"non_payment"}, "confirm": {"yes"}, "pad": {big}}, c)
		})},
		{"C228 suspend, a form that does not parse", 400, counted("C228", func(c *http.Cookie) req {
			return req{method: http.MethodPost, path: sus(tenant), raw: "confirm=yes&reason=%zz", origin: opOrigin, cookies: []*http.Cookie{c}}
		})},
		{"C229 suspend, the write fails", 503, failing("SuspendTenant", errFakeSuspendRefused, "C229", func(c *http.Cookie) req {
			return post(sus(tenant), suspendForm("non_payment"), c)
		})},
		{"C230 suspend, the write's session is refused", 303, failing("SuspendTenant", db.ErrOperatorRefused, "C230", func(c *http.Cookie) req {
			return post(sus(tenant), suspendForm("non_payment"), c)
		})},
		{"C231 PUT on suspend", 405, counted("C231", func(c *http.Cookie) req {
			r := post(sus(tenant), suspendForm("non_payment"), c)
			r.method = http.MethodPut
			return r
		})},
		{"C232 reinstate, a suspended tenant", 303, counted("C232", func(c *http.Cookie) req {
			return post(rei(tenant), reinstateForm(), c)
		})},
		{"C233 reinstate, a tenant that is not suspended", 303, counted("C233", func(c *http.Cookie) req {
			return post(rei(tenant), reinstateForm(), c)
		})},
		{"C234 reinstate, an id no tenant has", 303, counted("C234", func(c *http.Cookie) req {
			return post(rei(unknown), reinstateForm(), c)
		})},
		{"C235 reinstate without a cookie", 303, counted("C235", func(*http.Cookie) req { return post(rei(tenant), reinstateForm()) })},
		{"C236 reinstate, cross-origin", 403, counted("C236", func(c *http.Cookie) req {
			return cross(post(rei(tenant), reinstateForm(), c))
		})},
		{"C237 reinstate, a malformed id", 404, counted("C237", func(c *http.Cookie) req { return post(rei(malformed), reinstateForm(), c) })},
		{"C238 reinstate, the box not ticked", 400, counted("C238", func(c *http.Cookie) req {
			return post(rei(tenant), url.Values{"confirm": {"on"}}, c)
		})},
		{"C239 reinstate, an oversized form", 413, counted("C239", func(c *http.Cookie) req {
			return post(rei(tenant), url.Values{"confirm": {"yes"}, "pad": {big}}, c)
		})},
		{"C240 reinstate, the write fails", 503, failing("ReinstateTenant", errFakeSuspendRefused, "C240", func(c *http.Cookie) req {
			return post(rei(tenant), reinstateForm(), c)
		})},
		{"C241 reinstate, the write's session is refused", 303, failing("ReinstateTenant", db.ErrOperatorRefused, "C241", func(c *http.Cookie) req {
			return post(rei(tenant), reinstateForm(), c)
		})},
		{"C242 GET on reinstate", 405, counted("C242", func(c *http.Cookie) req {
			return req{method: http.MethodGet, path: rei(tenant), cookies: []*http.Cookie{c}, header: map[string]string{"Sec-Fetch-Site": "same-origin"}}
		})},
	}
	if len(classes) != 25 {
		t.Fatalf("PREMISE: %d classes, the comment says 25", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(218+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 218+i, c.name, 218+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	none := map[string]int{"all": 0}
	noWrite := map[string]int{"SuspendTenant": 0, "ReinstateTenant": 0, "writes": 0}
	for class, want := range map[string]map[string]int{
		"C218": {"SuspendTenant": 1, "ReinstateTenant": 0, "writes": 1}, "C219": {"SuspendTenant": 1, "writes": 1},
		"C220": {"SuspendTenant": 1, "writes": 1}, "C221": none, "C222": noWrite, "C223": none, "C224": noWrite,
		"C225": noWrite, "C226": noWrite, "C227": noWrite, "C228": noWrite,
		"C229": {"SuspendTenant": 1, "writes": 0}, "C230": {"SuspendTenant": 1, "writes": 0}, "C231": none,
		"C232": {"ReinstateTenant": 1, "SuspendTenant": 0, "writes": 1}, "C233": {"ReinstateTenant": 1, "writes": 1},
		"C234": {"ReinstateTenant": 1, "writes": 1}, "C235": none, "C236": none, "C237": noWrite, "C238": noWrite,
		"C239": noWrite, "C240": {"ReinstateTenant": 1, "writes": 0}, "C241": {"ReinstateTenant": 1, "writes": 0}, "C242": none,
	} {
		got, ok := calls[class]
		if !ok {
			t.Errorf("PREMISE: %s's calls were not counted", class)
			continue
		}
		for m, n := range want {
			if got[m] != n {
				t.Errorf("%s: %d %s, want %d", class, got[m], m, n)
			}
		}
	}
	writes := g.suspensionWrites()
	if len(writes) != 6 {
		t.Fatalf("%d write(s) recorded, want the six accepted classes'", len(writes))
	}
	for i, want := range []struct {
		method string
		tenant uuid.UUID
		reason db.SuspensionReason
	}{
		{"SuspendTenant", susHeaderTenant, db.SuspensionNonPayment}, {"SuspendTenant", susHeaderTenant, db.SuspensionSecurityHold},
		{"SuspendTenant", susHeaderUnknown, db.SuspensionTermsBreach}, {"ReinstateTenant", susHeaderTenant, ""},
		{"ReinstateTenant", susHeaderTenant, ""}, {"ReinstateTenant", susHeaderUnknown, ""},
	} {
		if w := writes[i]; w.method != want.method || w.tenant != want.tenant || w.reason != want.reason {
			t.Errorf("write %d: %s for %s with %q, want %s for the path's tenant %s with %q", i+1, w.method, w.tenant, w.reason,
				want.method, want.tenant, want.reason)
		}
	}
}

// ------------------------------------------------------------------ the answers --

// TestSuspension_TheAnswerIsTheSameWhateverTheTenantHeld (ADR 0021 §2 v 7; ADR 0025 §2 md. 7): the
// writes say nothing about what the tenant held -- the overview, read again, does.
//
// PART I -- suspend (customer_request) for a tenant that is not suspended, one that is (since
// 2026-09-01 08:00 UTC, terms_breach), and an id no tenant has; then reinstate for a tenant that
// is suspended, one that is not, and the id no tenant has: each is 303 to the overview of the
// path's id with an empty body, and the six answers' header names are one set and their Location
// differs only by the id; each made exactly one write call for the path's id. The overviews the
// redirects land on: the first tenant Suspended "since" the fake's stamp with "At the business's
// request"; the second still since 2026-09-01 08:00 UTC -- the first suspension's time kept
// (K15-4) -- with the reason replaced; the unknown id the no-such-tenant 404; after the
// reinstatements both tenants Active.
//
// PART II -- the six writes above. PART III -- these six only (no completeness claim).
func TestSuspension_TheAnswerIsTheSameWhateverTheTenantHeld(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	before := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	active := g.seedTenant(uuid.Nil, "FAKE Active Ltd", nil, "")
	held := g.seedTenant(uuid.Nil, "FAKE Held Ltd", &before, db.SuspensionTermsBreach)
	unknown := uuid.New()
	shape := func(w *httptest.ResponseRecorder) string {
		var names []string
		for k := range w.Result().Header {
			names = append(names, k)
		}
		slices.Sort(names)
		return fmt.Sprintf("%d %v body=%q", w.Code, names, w.Body.String())
	}
	var shapes []string
	for _, a := range []struct {
		path string
		form url.Values
		id   uuid.UUID
	}{
		{suspendPathOf(active.String()), suspendForm("customer_request"), active},
		{suspendPathOf(held.String()), suspendForm("customer_request"), held},
		{suspendPathOf(unknown.String()), suspendForm("customer_request"), unknown},
	} {
		n := len(g.suspensionWrites())
		w := g.post(a.path, a.form, c)
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != overviewPathOf(a.id.String()) || w.Body.Len() != 0 {
			t.Errorf("POST %s = %d to %q (%d bytes), want 303 to the path's overview and no body", a.path, w.Code,
				w.Result().Header.Get("Location"), w.Body.Len())
		}
		if ws := g.suspensionWrites(); len(ws) != n+1 || ws[n].tenant != a.id || ws[n].reason != db.SuspensionCustomerRequest {
			t.Errorf("POST %s made %d write(s), want one for the path's tenant with the posted reason", a.path, len(ws)-n)
		}
		shapes = append(shapes, shape(w))
	}
	first := g.overviewOf(active)
	body := g.get(overviewPathOf(active.String()), c).Body.String()
	if first.SuspendedAt == nil || !strings.Contains(body, chipSuspended) || !strings.Contains(body, wordCustomerRequest) ||
		!strings.Contains(body, first.SuspendedAt.UTC().Format("2006-01-02 15:04")+" UTC") {
		t.Errorf("the overview after suspending an active tenant does not say Suspended, its time and the reason")
	}
	body = g.get(overviewPathOf(held.String()), c).Body.String()
	if o := g.overviewOf(held); o.SuspendedAt == nil || !o.SuspendedAt.Equal(before) || !strings.Contains(body, "2026-09-01 08:00 UTC") ||
		!strings.Contains(body, wordCustomerRequest) || strings.Contains(body, wordTermsBreach) {
		t.Errorf("a second suspension did not keep the first time and replace the reason on the overview")
	}
	if w := g.get(overviewPathOf(unknown.String()), c); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "There is no tenant with that id") {
		t.Errorf("the overview of the unknown id = %d, want the no-such-tenant 404", w.Code)
	}
	for _, a := range []struct {
		id uuid.UUID
	}{{held}, {g.seedTenant(uuid.Nil, "FAKE Never Held Ltd", nil, "")}, {unknown}} {
		n := len(g.suspensionWrites())
		w := g.post(reinstatePathOf(a.id.String()), reinstateForm(), c)
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != overviewPathOf(a.id.String()) || w.Body.Len() != 0 {
			t.Errorf("reinstate %s = %d to %q, want 303 to the path's overview and no body", a.id, w.Code, w.Result().Header.Get("Location"))
		}
		if ws := g.suspensionWrites(); len(ws) != n+1 || ws[n].tenant != a.id || ws[n].method != "ReinstateTenant" {
			t.Errorf("reinstate %s made %d write(s), want one for the path's tenant", a.id, len(ws)-n)
		}
		shapes = append(shapes, shape(w))
	}
	for i, s := range shapes {
		if s != shapes[0] {
			t.Errorf("answer %d is %s, answer 1 is %s: the answer tells what the tenant held", i+1, s, shapes[0])
		}
	}
	for _, id := range []uuid.UUID{held} {
		if body := g.get(overviewPathOf(id.String()), c).Body.String(); !strings.Contains(body, chipActive) || strings.Contains(body, chipSuspended) {
			t.Errorf("the overview after reinstating %s does not say Active", id)
		}
	}
}

// TestSuspension_TheTenantIsThePathsNotTheForms: an id posted in the body (id, tenant, tenant_id)
// and in the query (id, tenant) naming ANOTHER tenant is never read -- the write is the path's
// tenant's, the redirect is the path's overview, and the other tenant is untouched; for both
// writes.
func TestSuspension_TheTenantIsThePathsNotTheForms(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	mine := g.seedTenant(uuid.Nil, "FAKE Mine Ltd", nil, "")
	other := g.seedTenant(uuid.Nil, "FAKE Other Ltd", nil, "")
	q := "?id=" + other.String() + "&tenant=" + other.String()
	extra := func(f url.Values) url.Values {
		f.Set("id", other.String())
		f.Set("tenant", other.String())
		f.Set("tenant_id", other.String())
		return f
	}
	for _, a := range []struct{ path string }{
		{suspendPathOf(mine.String()) + q}, {reinstatePathOf(mine.String()) + q},
	} {
		f := reinstateForm()
		if strings.Contains(a.path, "/suspend") {
			f = suspendForm("non_payment")
		}
		n := len(g.suspensionWrites())
		w := g.post(a.path, extra(f), c)
		ws := g.suspensionWrites()
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != overviewPathOf(mine.String()) ||
			len(ws) != n+1 || ws[n].tenant != mine {
			t.Errorf("POST %s with another tenant's id posted = %d to %q, write for %v; want the path's tenant", a.path, w.Code,
				w.Result().Header.Get("Location"), ws[n:])
		}
	}
	if o := g.overviewOf(other); o.SuspendedAt != nil {
		t.Error("the tenant named in the body and the query was suspended")
	}
}

// TestSuspension_NothingIsWrittenWithoutTheBoxOrAKnownReason (the orchestrator's K15-10 and K15-1):
// the box is exactly "yes" and the reason exactly a member of db.SuspensionReasons, or the answer
// is a 400 page and the store is not asked to write.
//
// PART I -- suspend with the box missing, empty, "on", "YES", "yes ", "true", "1": 400, "The box
// was not ticked", no write; with the box ticked and the reason missing, empty, "other",
// "NON_PAYMENT", " non_payment", "non_payment ", "non-payment", a sentence of free text, a reason
// with a NUL, "suspended": 400, "Choose one of the reasons", no write; reinstate with the box
// missing, "on", "YES": 400, no write. No refusal page carries the posted value or the tenant's
// id. CONTROL: each of the four reasons with the box ticked is a 303 and one write carrying it;
// reinstate with the box ticked is a 303 and one write.
//
// PART II -- the values above. PART III -- these values only (no completeness claim).
func TestSuspension_NothingIsWrittenWithoutTheBoxOrAKnownReason(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedTenant(uuid.Nil, "FAKE Careful Ltd", nil, "")
	refused := func(path string, f url.Values, page string, posted string) {
		t.Helper()
		n := g.store.count("SuspendTenant") + g.store.count("ReinstateTenant")
		w := g.post(path, f, c)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), page) {
			t.Errorf("POST %s %v = %d, want 400 with %q", path, f, w.Code, page)
		}
		if got := g.store.count("SuspendTenant") + g.store.count("ReinstateTenant") - n; got != 0 {
			t.Errorf("POST %s %v made %d write call(s), want none", path, f, got)
		}
		// The posted value is looked for from <main on (M13-01): the operator nav before it says
		// aria-hidden="true" on its icons, constant markup that is no echo of a posted "true".
		inMain := w.Body.String()
		if at := strings.Index(inMain, "<main"); at >= 0 {
			inMain = inMain[at:]
		} else {
			t.Errorf("POST %s %v: the refusal page has no <main", path, f)
		}
		if strings.Contains(w.Body.String(), id.String()) || (len(strings.TrimSpace(posted)) > 2 && strings.Contains(inMain, html.EscapeString(posted))) {
			t.Errorf("POST %s %v: the refusal page carries the tenant's id or the posted value", path, f)
		}
	}
	for _, box := range []*string{nil, ptr(""), ptr("on"), ptr("YES"), ptr("yes "), ptr("true"), ptr("1")} {
		f := url.Values{"reason": {"non_payment"}}
		posted := ""
		if box != nil {
			f.Set("confirm", *box)
			posted = *box
		}
		refused(suspendPathOf(id.String()), f, "The box was not ticked", posted)
		r := url.Values{}
		if box != nil {
			r.Set("confirm", *box)
		}
		refused(reinstatePathOf(id.String()), r, "The box was not ticked", posted)
	}
	for _, reason := range []*string{nil, ptr(""), ptr("other"), ptr("NON_PAYMENT"), ptr(" non_payment"), ptr("non_payment "),
		ptr("non-payment"), ptr("FAKE op15c free text: the owner has not paid since July"), ptr("security_hold\x00"), ptr("suspended")} {
		f := url.Values{"confirm": {"yes"}}
		posted := ""
		if reason != nil {
			f.Set("reason", *reason)
			posted = *reason
		}
		refused(suspendPathOf(id.String()), f, "Choose one of the reasons", posted)
	}
	if n := len(g.suspensionWrites()); n != 0 {
		t.Fatalf("the refusals recorded %d write(s)", n)
	}
	// CONTROL.
	for _, r := range db.SuspensionReasons() {
		w := g.post(suspendPathOf(id.String()), suspendForm(string(r)), c)
		ws := g.suspensionWrites()
		if w.Code != http.StatusSeeOther || len(ws) == 0 || ws[len(ws)-1].reason != r {
			t.Errorf("CONTROL: suspend with %q = %d, write %v", r, w.Code, ws)
		}
	}
	if w := g.post(reinstatePathOf(id.String()), reinstateForm(), c); w.Code != http.StatusSeeOther || len(g.suspensionWrites()) != 5 {
		t.Errorf("CONTROL: reinstate = %d with %d write(s)", w.Code, len(g.suspensionWrites()))
	}
}

// TestSuspension_EveryRefusalWritesNothing: the writes' other refusals and faults, and what each
// reaches.
//
// PART I -- for each write: a malformed id: 404, the not-an-id page, the gate's call and no
// other; a cross-origin POST: 403, no store call; a POST without a cookie: the sign-in's 303, no
// store call; an oversized body: 413, "That request was too large ... Nothing was changed."; a
// body that does not parse: 400, "That form could not be read"; the write failing (22023): 503,
// the not-confirmed page -- which does NOT say nothing changed -- and one ERROR line naming the
// tenant's id and the SQLSTATE; the write's session refused: the sign-in's 303. No arm records a
// write.
//
// PART II -- the list above. PART III -- these arms only (no completeness claim).
func TestSuspension_EveryRefusalWritesNothing(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedTenant(uuid.Nil, "FAKE Refusals Ltd", nil, "")
	malformed := strings.ReplaceAll(id.String(), "-", "")
	for _, route := range []struct {
		name, method string
		path         func(string) string
		form         url.Values
		failPage     string
	}{
		{"suspend", "SuspendTenant", suspendPathOf, suspendForm("non_payment"), "The suspension was not confirmed"},
		{"reinstate", "ReinstateTenant", reinstatePathOf, reinstateForm(), "Lifting the suspension was not confirmed"},
	} {
		type arm struct {
			name  string
			send  func() *httptest.ResponseRecorder
			code  int
			page  string
			store int
		}
		big := url.Values{"pad": {strings.Repeat("9", 20<<10)}}
		for k, v := range route.form {
			big[k] = v
		}
		for _, a := range []arm{
			{"a malformed id", func() *httptest.ResponseRecorder { return g.post(route.path(malformed), route.form, c) }, 404,
				"That link does not name a tenant", 1},
			{"cross-origin", func() *httptest.ResponseRecorder {
				return g.do(req{method: http.MethodPost, host: opHost, path: route.path(id.String()), form: route.form, origin: "https://taptime.mt",
					header: map[string]string{"Sec-Fetch-Site": "same-site"}, cookies: []*http.Cookie{c}})
			}, 403, "That request did not come from this site", 0},
			{"no cookie", func() *httptest.ResponseRecorder { return g.post(route.path(id.String()), route.form) }, 303, "", 0},
			{"an oversized body", func() *httptest.ResponseRecorder { return g.post(route.path(id.String()), big, c) }, 413,
				"Nothing was changed.", 1},
			{"a body that does not parse", func() *httptest.ResponseRecorder {
				return g.do(req{method: http.MethodPost, host: opHost, path: route.path(id.String()), raw: "confirm=yes&reason=%zz",
					origin: opOrigin, cookies: []*http.Cookie{c}})
			}, 400, "That form could not be read", 1},
			{"the write fails", func() *httptest.ResponseRecorder {
				g.setStoreFailure(route.method, errFakeSuspendRefused)
				defer g.setStoreFailure(route.method, nil)
				return g.post(route.path(id.String()), route.form, c)
			}, 503, route.failPage, 2},
			{"the write's session is refused", func() *httptest.ResponseRecorder {
				g.setStoreFailure(route.method, db.ErrOperatorRefused)
				defer g.setStoreFailure(route.method, nil)
				return g.post(route.path(id.String()), route.form, c)
			}, 303, "", 2},
		} {
			total := g.store.total()
			g.logs.Reset()
			w := a.send()
			if w.Code != a.code || (a.page != "" && !strings.Contains(html.UnescapeString(w.Body.String()), a.page)) {
				t.Errorf("%s, %s: %d, want %d with %q", route.name, a.name, w.Code, a.code, a.page)
			}
			if a.code == 303 && w.Result().Header.Get("Location") != "/operator/login" {
				t.Errorf("%s, %s: redirected to %q, want the sign-in", route.name, a.name, w.Result().Header.Get("Location"))
			}
			if got := g.store.total() - total; got != a.store {
				t.Errorf("%s, %s: %d store call(s), want %d", route.name, a.name, got, a.store)
			}
			if a.name == "the write fails" {
				body := html.UnescapeString(w.Body.String())
				if strings.Contains(body, "Nothing was changed") || !strings.Contains(body, "does not mean it was not made") {
					t.Errorf("%s: the fault page claims nothing changed, or does not say the write may have been made", route.name)
				}
				logs := g.logs.String()
				if !strings.Contains(logs, "could not be confirmed") || !strings.Contains(logs, id.String()) || !strings.Contains(logs, "SQLSTATE 22023") {
					t.Errorf("%s: the fault's log line does not name the tenant and the SQLSTATE", route.name)
				}
			}
		}
	}
	if n := len(g.suspensionWrites()); n != 0 {
		t.Errorf("the refusals recorded %d write(s)", n)
	}
}

// ------------------------------------------------------------- the client leaves --

// TestSuspension_AClientThatLeavesStillGetsItsWriteMade: past the id check a write runs to its end
// whatever the client does (answered(r); the write's own detached context; the VAT re-check's
// precedent).
//
// PART I -- for each write, a request whose context the fake store's hook cancels inside the
// write, before the write checks its context (the fake refuses a cancelled context, as a
// statement on one does not run): 303 to the overview and the write recorded, under a deadline
// more than 9 s and at most 10 s away (suspensionWriteTimeout, written here as its number).
// CONTROL: a client that stays is answered and written the same way, and a write whose context is
// cancelled is refused by the fake (so the arm above measures the detach).
//
// PART II -- the two writes above. PART III -- these requests only (no completeness claim).
func TestSuspension_AClientThatLeavesStillGetsItsWriteMade(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedTenant(uuid.Nil, "FAKE Leaving Ltd", nil, "")
	for _, a := range []struct {
		path string
		form url.Values
	}{{suspendPathOf(id.String()), suspendForm("terms_breach")}, {reinstatePathOf(id.String()), reinstateForm()}} {
		ctx, cancel := context.WithCancel(context.Background())
		g.store.mu.Lock()
		g.store.beforeSuspension = cancel
		g.store.mu.Unlock()
		n := len(g.suspensionWrites())
		w := g.do(req{method: http.MethodPost, host: opHost, path: a.path, form: a.form, origin: opOrigin, cookies: []*http.Cookie{c}, ctx: ctx})
		g.store.mu.Lock()
		g.store.beforeSuspension = nil
		g.store.mu.Unlock()
		cancel()
		ws := g.suspensionWrites()
		if w.Code != http.StatusSeeOther || len(ws) != n+1 {
			t.Fatalf("POST %s, the client leaving before the write = %d with %d write(s), want 303 and the write made", a.path, w.Code, len(ws)-n)
		}
		if d := ws[n].deadline; d <= 9*time.Second || d > 10*time.Second {
			t.Errorf("POST %s: the write's context has %v left, want a deadline of its own more than 9s and at most 10s away", a.path, d)
		}
		// CONTROL: a client that stays.
		if w := g.post(a.path, a.form, c); w.Code != http.StatusSeeOther || len(g.suspensionWrites()) != n+2 {
			t.Errorf("CONTROL: POST %s from a client that stays = %d", a.path, w.Code)
		}
	}
	// CONTROL: the fake refuses a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g.store.mu.Lock()
	var h string
	for k := range g.store.live {
		h = k
	}
	g.store.mu.Unlock()
	if err := g.store.SuspendTenant(ctx, h, id, db.SuspensionNonPayment); !errors.Is(err, context.Canceled) {
		t.Fatalf("CONTROL: the fake's write on a cancelled context = %v, want context.Canceled", err)
	}
}

// TestSuspension_AHalfClosedClientGetsTheSameAnswer (OP-14 E's answered): a client whose request's
// context is already cancelled when the handler runs -- net/http's answer to a client that
// half-closes its connection -- gets, past the id check, the same status and the same bytes as a
// client that stayed: the refused box (400 page), the refused reason (400 page), the failing
// write (503 page), and an accepted write (303, written). A page rendered on the cancelled context
// would be a plain 500 instead. CONTROL: the same refusal rendered by a component on a cancelled
// context fails (the measurement can see the difference).
func TestSuspension_AHalfClosedClientGetsTheSameAnswer(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedTenant(uuid.Nil, "FAKE Half Closed Ltd", nil, "")
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	send := func(ctx context.Context, path string, f url.Values) *httptest.ResponseRecorder {
		return g.do(req{method: http.MethodPost, host: opHost, path: path, form: f, origin: opOrigin, cookies: []*http.Cookie{c}, ctx: ctx})
	}
	for _, a := range []struct {
		name, path string
		form       url.Values
		fail       string
		code       int
	}{
		{"the box not ticked", suspendPathOf(id.String()), url.Values{"reason": {"non_payment"}}, "", 400},
		{"a reason outside the set", suspendPathOf(id.String()), url.Values{"reason": {"other"}, "confirm": {"yes"}}, "", 400},
		{"reinstate, the box not ticked", reinstatePathOf(id.String()), url.Values{}, "", 400},
		{"the write fails", suspendPathOf(id.String()), suspendForm("non_payment"), "SuspendTenant", 503},
		{"reinstate, the write fails", reinstatePathOf(id.String()), reinstateForm(), "ReinstateTenant", 503},
		{"an accepted write", suspendPathOf(id.String()), suspendForm("non_payment"), "", 303},
		{"an accepted reinstatement", reinstatePathOf(id.String()), reinstateForm(), "", 303},
	} {
		if a.fail != "" {
			g.setStoreFailure(a.fail, errFakeSuspendRefused)
		}
		stayed := send(context.Background(), a.path, a.form)
		n := len(g.suspensionWrites())
		left := send(gone, a.path, a.form)
		if a.fail != "" {
			g.setStoreFailure(a.fail, nil)
		}
		if stayed.Code != a.code || left.Code != stayed.Code || left.Body.String() != stayed.Body.String() {
			t.Errorf("%s: a client that stayed = %d, one that half-closed = %d (same bytes %v); want %d both",
				a.name, stayed.Code, left.Code, left.Body.String() == stayed.Body.String(), a.code)
		}
		if a.code == 303 && len(g.suspensionWrites()) != n+1 {
			t.Errorf("%s: the half-closed client's write was not made", a.name)
		}
	}
	// CONTROL.
	var b strings.Builder
	if err := operatorpages.Problem(operatorpages.ProblemView{Title: "T", Message: "M"}).Render(gone, &b); err == nil {
		t.Fatal("CONTROL: a page renders on a cancelled context; this test cannot see answered(r) missing")
	}
}

// TestSuspension_TheWriteBoundIsItsNumber pins suspension.go's bound as a number: 10 s, the same as
// legal.go's legalWriteTimeout, and SuspensionWriteWorstCase equal to it (one statement). That it
// fits in the HTTP drain on shutdown is cmd/tappa's
// TestShutdownBudget_TheSuspensionWriteNestsInsideTheHTTPGrace; that it reaches the write's context
// is TestSuspension_AClientThatLeavesStillGetsItsWriteMade.
func TestSuspension_TheWriteBoundIsItsNumber(t *testing.T) {
	for _, b := range []struct {
		name      string
		got, want time.Duration
	}{
		{"suspensionWriteTimeout", operator.SuspensionWriteTimeoutForTest, 10 * time.Second},
		{"suspensionWriteTimeout against legal.go's legalWriteTimeout", operator.SuspensionWriteTimeoutForTest, operator.LegalWriteTimeoutForTest},
		{"SuspensionWriteWorstCase", operator.SuspensionWriteWorstCase, 10 * time.Second},
	} {
		if b.got != b.want {
			t.Errorf("%s = %v, want %v", b.name, b.got, b.want)
		}
	}
	if operator.SuspensionConfirmedForTest != "yes" {
		t.Errorf("the box's value is %q, want \"yes\" (the overview's checkbox posts it)", operator.SuspensionConfirmedForTest)
	}
}

// ------------------------------------------------------------------ the budgets --

// TestSuspensionBudget_AWriteIsOneRequestAndNoRead (ADR 0025 §5: "yazma bir istek birimidir; PRG
// okuması bir okuma birimidir"; surface.go, sessionLimit).
//
// PART I -- one session, from a fresh address per request: 60 overview views (the read budget,
// 60) are 200; a suspension after them is still 303 and written -- the write charges no read --
// while the overview its redirect lands on is the read budget's 429; 38 more writes, alternately
// reinstate and suspend, are each 303 and written; the session has now made 100 requests, and the
// 101st -- a write -- is the session gate's 429 with no write call.
//
// PART II -- the counts above. PART III -- one session (no completeness claim).
func TestSuspensionBudget_AWriteIsOneRequestAndNoRead(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedTenant(uuid.Nil, "FAKE Budget Ltd", nil, "")
	n := 0
	send := func(r req) *httptest.ResponseRecorder {
		n++
		r.host, r.cookies, r.remote = opHost, []*http.Cookie{c}, fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1)
		return g.do(r)
	}
	view := func() *httptest.ResponseRecorder {
		return send(req{method: http.MethodGet, path: overviewPathOf(id.String()), header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
	}
	write := func(path string, f url.Values) *httptest.ResponseRecorder {
		return send(req{method: http.MethodPost, path: path, form: f, origin: opOrigin})
	}
	for i := 0; i < 60; i++ {
		if w := view(); w.Code != http.StatusOK {
			t.Fatalf("PREMISE: overview %d = %d", i+1, w.Code)
		}
	}
	if w := write(suspendPathOf(id.String()), suspendForm("non_payment")); w.Code != http.StatusSeeOther || len(g.suspensionWrites()) != 1 {
		t.Fatalf("a suspension after the session's 60 reads = %d with %d write(s), want 303 and the write: the write must charge no read",
			w.Code, len(g.suspensionWrites()))
	}
	if w := view(); w.Code != http.StatusTooManyRequests {
		t.Errorf("the overview after the reads were spent = %d, want the read budget's 429", w.Code)
	}
	for i := 0; i < 38; i++ {
		path, f := reinstatePathOf(id.String()), reinstateForm()
		if i%2 == 1 {
			path, f = suspendPathOf(id.String()), suspendForm("non_payment")
		}
		if w := write(path, f); w.Code != http.StatusSeeOther {
			t.Fatalf("write %d = %d, want 303 (the session has made %d requests)", i+2, w.Code, n)
		}
	}
	if n != 100 || len(g.suspensionWrites()) != 39 {
		t.Fatalf("PREMISE: %d request(s) and %d write(s) so far, want 100 and 39", n, len(g.suspensionWrites()))
	}
	calls := g.store.count("SuspendTenant") + g.store.count("ReinstateTenant")
	if w := write(suspendPathOf(id.String()), suspendForm("non_payment")); w.Code != http.StatusTooManyRequests ||
		g.store.count("SuspendTenant")+g.store.count("ReinstateTenant") != calls {
		t.Errorf("the session's 101st request, a write = %d with %d write call(s), want the gate's 429 and none", w.Code,
			g.store.count("SuspendTenant")+g.store.count("ReinstateTenant")-calls)
	}
}

// ------------------------------------------------------------------ the log --

// TestSuspension_NoPostedValueReachesALogLine (suspension.go's header; CLAUDE.md §7): through the
// shipped router at Debug, in both production formats.
//
// PART I -- arms: a suspension (with extra posted fields), a reinstatement, a reason outside the
// set, the box not ticked with a value, an oversized body, the write failing for each write, the
// write's session refused; each posts the hostile values below. None of the posted values, no
// session hash the store saw and not the session cookie is on the process or access log, raw or
// query-escaped. Every JSON record whose message speaks of a suspension or a reinstatement is one
// of the four lines suspension.go writes and carries exactly its attributes: the success lines
// tenant_id, admin_id (and reason, whose value is a member of the closed set), the fault lines
// tenant_id and err -- and the refusals write none of them. CONTROL: a hostile value logged
// directly is found.
//
// PART II -- the arms above. PART III -- these arms and two formats only (no completeness claim).
func TestSuspension_NoPostedValueReachesALogLine(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedTenant(uuid.Nil, "FAKE Log Ltd", nil, "")
	other := uuid.New()
	const (
		hostileReason  = "FAKEop15cHOSTILEreason"
		hostileConfirm = "FAKEop15cHOSTILEconfirm"
		hostileExtra   = "FAKEop15cHOSTILEextra"
	)
	hostile := []string{hostileReason, hostileConfirm, hostileExtra, other.String(), c.Value}
	withExtra := func(f url.Values) url.Values {
		f.Set("note", hostileExtra)
		f.Set("id", other.String())
		return f
	}
	g.logs.Reset()
	for _, a := range []struct {
		path string
		form url.Values
		fail string
		err  error
	}{
		{suspendPathOf(id.String()), withExtra(suspendForm("security_hold")), "", nil},
		{reinstatePathOf(id.String()), withExtra(reinstateForm()), "", nil},
		{suspendPathOf(id.String()), withExtra(url.Values{"reason": {hostileReason}, "confirm": {"yes"}}), "", nil},
		{suspendPathOf(id.String()), withExtra(url.Values{"reason": {"non_payment"}, "confirm": {hostileConfirm}}), "", nil},
		{reinstatePathOf(id.String()), withExtra(url.Values{"confirm": {hostileConfirm}}), "", nil},
		{suspendPathOf(id.String()), withExtra(url.Values{"reason": {hostileReason + strings.Repeat("x", 20<<10)}}), "", nil},
		{suspendPathOf(id.String()), withExtra(suspendForm("non_payment")), "SuspendTenant", errFakeSuspendRefused},
		{reinstatePathOf(id.String()), withExtra(reinstateForm()), "ReinstateTenant", errFakeSuspendRefused},
		{suspendPathOf(id.String()), withExtra(suspendForm("non_payment")), "SuspendTenant", db.ErrOperatorRefused},
	} {
		if a.fail != "" {
			g.setStoreFailure(a.fail, a.err)
		}
		g.post(a.path, a.form, c)
		if a.fail != "" {
			g.setStoreFailure(a.fail, nil)
		}
	}
	logs := g.logs.String()
	g.store.mu.Lock()
	for h := range g.store.live {
		hostile = append(hostile, h)
	}
	for _, w := range g.store.suspensionWrites {
		hostile = append(hostile, w.hash)
	}
	g.store.mu.Unlock()
	for _, v := range hostile {
		for _, form := range []string{v, url.QueryEscape(v)} {
			if strings.Contains(logs, form) {
				t.Errorf("a posted value or a session credential (length %d) is on the log", len(v))
			}
		}
	}
	want := map[string][]string{
		"operator suspended a tenant":                               {"admin_id", "reason", "tenant_id"},
		"operator reinstated a tenant":                              {"admin_id", "tenant_id"},
		"operator: a tenant's suspension could not be confirmed":    {"err", "tenant_id"},
		"operator: a tenant's reinstatement could not be confirmed": {"err", "tenant_id"},
	}
	seen := map[string]int{}
	for _, line := range strings.Split(logs, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a JSON log record does not parse: %v", err)
		}
		msg, _ := rec["msg"].(string)
		low := strings.ToLower(msg)
		if !strings.Contains(low, "suspen") && !strings.Contains(low, "reinstat") {
			continue
		}
		keys, ok := want[msg]
		if !ok {
			t.Errorf("a log record speaks of a suspension and is not one of suspension.go's four lines: %q", msg)
			continue
		}
		seen[msg]++
		var got []string
		for k := range rec {
			switch k {
			case "time", "level", "msg", "request_id":
				continue
			}
			got = append(got, k)
		}
		slices.Sort(got)
		if !slices.Equal(got, keys) {
			t.Errorf("%q carries %v, want exactly %v", msg, got, keys)
		}
		if r, ok := rec["reason"].(string); ok && !db.SuspensionReason(r).Known() {
			t.Errorf("%q logs a reason outside the closed set", msg)
		}
	}
	for msg, n := range map[string]int{"operator suspended a tenant": 1, "operator reinstated a tenant": 1,
		"operator: a tenant's suspension could not be confirmed": 1, "operator: a tenant's reinstatement could not be confirmed": 1} {
		if seen[msg] != n {
			t.Errorf("%q written %d time(s), want %d", msg, seen[msg], n)
		}
	}
	// CONTROL.
	g.logs.Reset()
	captureAt(&lockedWriter{w: g.logs}, slog.LevelDebug).Info("control", "v", hostileReason)
	if !strings.Contains(g.logs.String(), hostileReason) {
		t.Fatal("CONTROL: a value logged directly is not found")
	}
}

// ------------------------------------------------------------------ the screen --

// TestTenantOverview_SaysTheSuspensionAndOffersOneForm (ADR 0025 §5): the overview's state line and
// its one form.
//
// PART I -- a tenant that is not suspended: the "Active" chip, the suspend form (action
// /operator/tenants/<id>/suspend; the four reasons as radios of name reason, in the closed set's
// order, each with its label; the box confirm=yes; no field naming a tenant; no free-text field)
// and no reinstate form; a suspended tenant (since 11:30 at UTC+2): the "Suspended" chip, "since
// 2026-10-08 09:30 UTC", the reason's label, the reinstate form (the box, nothing else) and no
// suspend form; a stored code this build does not name, "zz_later_reason": its raw text and the
// unrecognised chip; a code that is not a token, "<b>x</b>": the unrecognised chip and no raw
// text; an empty code: the unrecognised chip.
//
// PART II -- the cases above. PART III -- these cases only (no completeness claim).
func TestTenantOverview_SaysTheSuspensionAndOffersOneForm(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	active := g.seedTenant(uuid.Nil, "FAKE Active Ltd", nil, "")
	body := g.get(overviewPathOf(active.String()), c).Body.String()
	suspendAction := `<form method="post" action="` + suspendPathOf(active.String()) + `"`
	if !strings.Contains(body, chipActive) || strings.Contains(body, chipSuspended) || strings.Count(body, suspendAction) != 1 ||
		strings.Contains(body, `action="`+reinstatePathOf(active.String())+`"`) {
		t.Fatalf("an active tenant's overview: chip or forms wrong")
	}
	form := body[strings.Index(body, suspendAction):]
	form = form[:strings.Index(form, "</form>")]
	radios := regexp.MustCompile(`<input id="suspend-reason-([a-z_]+)" class="[^"]*" type="radio" name="reason" value="([a-z_]+)" required`).FindAllStringSubmatch(form, -1)
	var order []string
	for _, r := range radios {
		if r[1] != r[2] {
			t.Errorf("a radio's id names %s and its value is %s", r[1], r[2])
		}
		order = append(order, r[2])
	}
	var wantOrder []string
	for _, r := range db.SuspensionReasons() {
		wantOrder = append(wantOrder, string(r))
	}
	if !slices.Equal(order, wantOrder) {
		t.Errorf("the form's reasons are %v, want the closed set's order %v", order, wantOrder)
	}
	for code, label := range map[string]string{"non_payment": wordNonPayment, "terms_breach": wordTermsBreach,
		"security_hold": wordSecurityHold, "customer_request": wordCustomerRequest} {
		if !strings.Contains(form, `<label for="suspend-reason-`+code+`" class="inline-flex min-h-11 items-center text-sm">`+label+`</label>`) {
			t.Errorf("the reason %s is not labelled %q", code, label)
		}
	}
	if !strings.Contains(form, `<input type="checkbox" name="confirm" value="yes" required class="h-5 w-5 shrink-0 accent-tappa-green">`) {
		t.Error("the suspend form has no confirm box posting yes")
	}
	for _, field := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(form, -1) {
		if field[1] != "reason" && field[1] != "confirm" {
			t.Errorf("the suspend form carries a field %q: it carries the reason and the box, nothing else", field[1])
		}
	}
	if strings.Contains(form, "<textarea") || strings.Contains(form, `type="text"`) {
		t.Error("the suspend form carries a free-text field")
	}

	plus2 := time.FixedZone("x", 2*3600)
	since := time.Date(2026, 10, 8, 11, 30, 0, 0, plus2)
	held := g.seedTenant(uuid.Nil, "FAKE Held Ltd", &since, db.SuspensionNonPayment)
	body = g.get(overviewPathOf(held.String()), c).Body.String()
	reinstateAction := `<form method="post" action="` + reinstatePathOf(held.String()) + `"`
	if !strings.Contains(body, chipSuspended) || strings.Contains(body, chipActive) || strings.Count(body, reinstateAction) != 1 ||
		strings.Contains(body, `action="`+suspendPathOf(held.String())+`"`) {
		t.Fatalf("a suspended tenant's overview: chip or forms wrong")
	}
	if !regexp.MustCompile(`<dt class="docket-label">Suspended since</dt>\s*<dd class="mt-1 break-all font-mono text-sm">2026-10-08 09:30 UTC</dd>`).MatchString(body) ||
		strings.Contains(body, "11:30") {
		t.Error("the suspension's start is not printed in UTC, mono")
	}
	if !regexp.MustCompile(`<dt class="docket-label">Reason</dt>\s*<dd class="mt-1 text-sm">\s*` + wordNonPayment + `\s*</dd>`).MatchString(body) {
		t.Error("the suspension's reason is not its label")
	}
	rform := body[strings.Index(body, reinstateAction):]
	rform = rform[:strings.Index(rform, "</form>")]
	var fields []string
	for _, f := range regexp.MustCompile(`name="([^"]+)"`).FindAllStringSubmatch(rform, -1) {
		fields = append(fields, f[1])
	}
	if !slices.Equal(fields, []string{"confirm"}) || !strings.Contains(rform, `value="yes"`) {
		t.Errorf("the reinstate form's fields are %v, want the box alone", fields)
	}
	for code, want := range map[db.SuspensionReason]string{
		"zz_later_reason": `<span class="font-mono">zz_later_reason</span> <span class="tally tally--unrecognised">Unrecognised</span>`,
		"<b>x</b>":        `<span class="tally tally--unrecognised">Unrecognised</span>`,
		"":                `<span class="tally tally--unrecognised">Unrecognised</span>`,
	} {
		id := g.seedTenant(uuid.Nil, "FAKE Unknown Reason Ltd", &since, code)
		body := g.get(overviewPathOf(id.String()), c).Body.String()
		if !strings.Contains(body, want) {
			t.Errorf("a stored reason %q is not drawn as unrecognised", code)
		}
		if code == "<b>x</b>" && (strings.Contains(body, "&lt;b&gt;x") || strings.Contains(body, "<b>x</b>")) {
			t.Error("a stored reason that is not a token is printed")
		}
	}
}

// TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice (ADR 0025 §5, the orchestrator's
// 4th-round decision): the security hold's warning is closed text, in the suspend form, shown by
// the stylesheet only while the security-hold radio is checked.
//
// PART I -- the active tenant's overview: exactly one radio carries the op-reason-hold class, and
// it is security_hold's, described by the warning's id; the warning (id suspend-hold-warning,
// class op-hold-warning) is in the same op-reasons parent as the radios and AFTER every one of
// them -- the general-sibling rule can reach it; it carries the heading and every sentence of
// holdSentences, word for word, and nothing of the tenant (its name, its id). web/static/css/input.css
// has the three rules -- the reasons' grid, the warning hidden, and the sibling rule showing it on
// the hold radio's :checked --; when the compiled app.css is present (make css), it carries the
// warning hidden and `.op-reason-hold:checked~.op-hold-warning{display:block}`. A suspended
// tenant's overview has no warning.
//
// PART II -- red on: the warning or a sentence of it missing or changed; the warning before a radio
// or outside their parent; the hold class on another radio or on none; a rule missing.
// PART III -- whether a browser applies the rule is not measured here (no browser in the suite);
// the markup and the rules are -- no completeness claim.
func TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const name = "FAKE Hold Warning Ltd"
	id := g.seedTenant(uuid.Nil, name, nil, "")
	body := g.get(overviewPathOf(id.String()), c).Body.String()
	start := strings.Index(body, `<div class="op-reasons mt-1">`)
	if start < 0 {
		t.Fatal("the suspend form has no reasons' parent")
	}
	parent := body[start:]
	warnAt := strings.Index(parent, `<div id="suspend-hold-warning" class="op-hold-warning">`)
	if warnAt < 0 {
		t.Fatal("the warning is not in the reasons' parent")
	}
	if strings.Count(parent[:warnAt], `type="radio"`) != 4 || strings.Contains(parent[warnAt:], `type="radio"`) {
		t.Error("the warning does not follow all four radios in their parent")
	}
	// The parent closes after the warning: the warning's own div, the notice's section and its
	// inner div are balanced first.
	warning := parent[warnAt:]
	warning = warning[:strings.Index(warning, "</section>")]
	hold := regexp.MustCompile(`<input id="suspend-reason-([a-z_]+)" class="op-reason-hold `).FindAllStringSubmatch(body, -1)
	if len(hold) != 1 || hold[0][1] != "security_hold" {
		t.Errorf("the hold class is on %v, want security_hold's radio alone", hold)
	}
	if !strings.Contains(body, `value="security_hold" required aria-describedby="suspend-hold-warning"`) {
		t.Error("the security-hold radio is not described by the warning")
	}
	text := spaced(warning)
	if !strings.Contains(text, holdHeading) {
		t.Error("the warning's heading is missing or changed")
	}
	for _, s := range holdSentences {
		if !strings.Contains(text, spaced(s)) {
			t.Errorf("the warning lacks %q", s)
		}
	}
	if strings.Contains(warning, name) || strings.Contains(warning, id.String()) {
		t.Error("the warning carries the tenant's data; it is closed text")
	}
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		`.op-reasons { @apply grid grid-cols-[auto_1fr] items-center gap-x-3; }`,
		`.op-hold-warning { @apply col-span-2 mt-2 hidden; }`,
		`.op-reason-hold:checked ~ .op-hold-warning { @apply block; }`,
	} {
		if strings.Count(string(css), rule) != 1 {
			t.Errorf("input.css does not hold the rule %q exactly once", rule)
		}
	}
	if compiled, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "app.css")); err == nil {
		if !strings.Contains(string(compiled), `.op-reason-hold:checked~.op-hold-warning{display:block}`) ||
			!regexp.MustCompile(`\.op-hold-warning\{display:none;`).Match(compiled) {
			t.Error("the compiled app.css does not hide the warning and show it on the hold radio")
		}
	} else {
		t.Log("app.css not built (make css): the compiled half is not measured on this run")
	}
	since := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	held := g.seedTenant(uuid.Nil, "FAKE Held Ltd", &since, db.SuspensionSecurityHold)
	if body := g.get(overviewPathOf(held.String()), c).Body.String(); strings.Contains(body, "op-hold-warning") || strings.Contains(body, holdHeading) {
		t.Error("a suspended tenant's overview carries the suspend form's warning")
	}
}

// TestTenantList_ASuspendedTenantCarriesTheChip (K15-3): the list row of a tenant with a suspension
// start carries the Suspended chip, between its plan and its id; a row without one carries none.
func TestTenantList_ASuspendedTenantCarriesTheChip(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	since := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	held, free := uuid.New(), uuid.New()
	g.store.mu.Lock()
	g.store.tenants = []db.TenantSummary{
		{ID: held, Name: "FAKE Held Ltd", Plan: "founding", SuspendedAt: &since},
		{ID: free, Name: "FAKE Free Ltd", Plan: "standard"},
	}
	g.store.mu.Unlock()
	body := g.get("/operator/tenants", c).Body.String()
	if !strings.Contains(body, "<span>founding</span> "+chipSuspended+` <span class="break-all text-xs text-ink/70">`+held.String()+`</span>`) &&
		!regexp.MustCompile(`<span>founding</span>\s*`+regexp.QuoteMeta(chipSuspended)+`\s*<span class="break-all text-xs text-ink/70">`+held.String()).MatchString(body) {
		t.Error("the suspended tenant's row does not carry the chip between its plan and its id")
	}
	if strings.Count(body, chipSuspended) != 1 {
		t.Errorf("%d chip(s) on the list, want the suspended tenant's one", strings.Count(body, chipSuspended))
	}
}

// TestSuspensionWords_NameEveryReasonAndNothingElse: suspension.go's labels are one per member of
// db.SuspensionReasons and no other, none empty, no two alike -- so a reason a later migration
// adds is red here until the screen has a word for it (the database half:
// TestOperator00063_TheReasonSetIsOneSetInEveryCopy reads this map's keys from the source).
func TestSuspensionWords_NameEveryReasonAndNothingElse(t *testing.T) {
	words := operator.SuspensionWordsForTest()
	var keys, want []string
	labels := map[string]bool{}
	for k, w := range words {
		keys = append(keys, string(k))
		if strings.TrimSpace(w) == "" || labels[w] {
			t.Errorf("the reason %s's label %q is empty or another reason's", k, w)
		}
		labels[w] = true
	}
	for _, r := range db.SuspensionReasons() {
		want = append(want, string(r))
	}
	slices.Sort(keys)
	slices.Sort(want)
	if len(want) != 4 || !slices.Equal(keys, want) {
		t.Errorf("the labels name %v, want exactly %v", keys, want)
	}
}

// TestSuspensionScreen_TheChipsAndTextClearAA recomputes, from tailwind.config.js's palette, the
// contrast of the suspension's chips on paper (the overview's card and the list's docket): ink on
// green-lite (active), on tomato at 10% over paper (suspended) and on ink at 10% over paper (an
// unrecognised reason) -- each against AA's 4.5:1 (the chips' words are 11px). No ground is new.
func TestSuspensionScreen_TheChipsAndTextClearAA(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatal(err)
	}
	hexOf := func(token string) [3]float64 {
		m := regexp.MustCompile(`'?` + regexp.QuoteMeta(token) + `'?:\s*'#([0-9A-Fa-f]{6})'`).FindSubmatch(b)
		if m == nil {
			t.Fatalf("tailwind.config.js has no %s", token)
		}
		var c [3]float64
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseUint(string(m[1][2*i:2*i+2]), 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			c[i] = float64(v)
		}
		return c
	}
	over := func(fg [3]float64, alpha float64, bg [3]float64) [3]float64 {
		var c [3]float64
		for i := range c {
			c[i] = alpha*fg[i] + (1-alpha)*bg[i]
		}
		return c
	}
	ink, paper := hexOf("ink"), hexOf("paper")
	for _, c := range []struct {
		what   string
		fg, bg [3]float64
	}{
		{"ink on green-lite (active)", ink, hexOf("green-lite")},
		{"ink on tomato at 10% over paper (suspended)", ink, over(hexOf("tomato"), 0.1, paper)},
		{"ink on ink at 10% over paper (an unrecognised reason)", ink, over(ink, 0.1, paper)},
	} {
		if got := contrast(c.fg, c.bg); got < 4.5 {
			t.Errorf("%s = %.2f:1, want at least 4.5:1", c.what, got)
		} else {
			t.Logf("%s = %.2f:1", c.what, got)
		}
	}
}

// TestSuspensionScreen_EachChipHasTheRuleTheContrastTestComputes ties the two chips the overview
// and the list write to the grounds above: for each, web/static/css/input.css has exactly one rule
// whose selector list names its class, and that rule's @apply names the ground and the frame of
// its tone and nothing else. PART II -- red on: a chip class with no rule, with two, or with
// another ground, frame or a third utility. PART III -- a rule outside the one-line @apply shape
// this reads is code review's -- no completeness claim.
func TestSuspensionScreen_EachChipHasTheRuleTheContrastTestComputes(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	ruleRe := regexp.MustCompile(`(?m)((?:^\s*\.[a-z-]+,\s*\n)*^\s*\.[a-z-]+\s*)\{\s*@apply ([^;]+);\s*\}`)
	rules := ruleRe.FindAllStringSubmatch(string(css), -1)
	if len(rules) < 10 {
		t.Fatalf("PREMISE: %d one-line @apply rule(s) read from input.css", len(rules))
	}
	for class, w := range map[string][2]string{ // ground, frame
		"tally--tenant-active":    {"bg-green-lite", "border-tappa-green"},
		"tally--tenant-suspended": {"bg-tomato/10", "border-tomato"},
	} {
		var found []string
		for _, r := range rules {
			for _, sel := range strings.Split(r[1], ",") {
				if strings.TrimSpace(sel) == "."+class {
					found = append(found, r[2])
				}
			}
		}
		if len(found) != 1 {
			t.Errorf("%s: %d rule(s) in input.css, want one", class, len(found))
			continue
		}
		tokens := strings.Fields(found[0])
		if len(tokens) != 2 || !slices.Contains(tokens, w[0]) || !slices.Contains(tokens, w[1]) {
			t.Errorf("%s's rule applies %q; want the ground %s and the frame %s and nothing else", class, found[0], w[0], w[1])
		}
	}
}

// ------------------------------------------------------------ the structural pins --

// TestSuspension_TheListedSitesAloneWrite (M10 OP-15 C; ADR 0025 §5).
//
// PART I -- the shipped package: operator.SuspensionStore's SuspendTenant is called once, in
// (*Surface).suspendTenant, and its ReinstateTenant once, in (*Surface).reinstateTenant; in each
// call the context argument is an identifier the handler defines from context.WithTimeout over
// context.WithoutCancel with suspensionWriteTimeout, and the tenant argument is the identifier the
// handler defines from tenantID's result; SuspendTenant's reason argument is the identifier the
// handler defines by converting r.PostForm.Get("reason") to db.SuspensionReason; each handler calls
// answered once, after its tenantID call and before its readForm call. CONTROL: the two methods
// resolve at one call each.
//
// PART II -- red on: SU1 either method called other than once or outside its handler; SU2 the
// write's context not the detached, bounded one; SU3 the tenant argument not the path's; SU4 the
// reason argument not the posted field's conversion; SU5 answered called other than once in a
// handler, or before its id check or after its form read.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: the tenant
// copied into another variable first, a write made through another type) is code review's -- no
// completeness claim.
func TestSuspension_TheListedSitesAloneWrite(t *testing.T) {
	tp := typedOperator(t)
	suspend := ifaceMethod(t, tp.pkg, "SuspensionStore", "SuspendTenant")
	reinstate := ifaceMethod(t, tp.pkg, "SuspensionStore", "ReinstateTenant")
	suspendH := tp.method(t, "Surface", "suspendTenant")
	reinstateH := tp.method(t, "Surface", "reinstateTenant")
	tenantIDFn := lookupFunc(t, tp.pkg, "tenantID")
	answeredFn := lookupFunc(t, tp.pkg, "answered")
	readForm := tp.method(t, "Surface", "readForm")
	timeout, _ := tp.pkg.Scope().Lookup("suspensionWriteTimeout").(*types.Const)
	if timeout == nil {
		t.Fatal("PREMISE: the package declares no suspensionWriteTimeout")
	}
	// definedBy reports whether e is an identifier its declaration assigns from a call matching ok.
	definedBy := func(e ast.Expr, ok func(*ast.CallExpr) bool) bool {
		id, isID := ast.Unparen(e).(*ast.Ident)
		if !isID {
			return false
		}
		def := tp.info.Uses[id]
		found := false
		ast.Inspect(tp.decl(id.Pos()), func(n ast.Node) bool {
			as, isAs := n.(*ast.AssignStmt)
			if !isAs || len(as.Rhs) != 1 {
				return true
			}
			for _, l := range as.Lhs {
				if li, isID := l.(*ast.Ident); isID && tp.info.Defs[li] != nil && tp.info.Defs[li] == def {
					if c, isCall := ast.Unparen(as.Rhs[0]).(*ast.CallExpr); isCall && ok(c) {
						found = true
					}
				}
			}
			return true
		})
		return found
	}
	detached := func(c *ast.CallExpr) bool {
		if qualSel(c.Fun) != "context.WithTimeout" || len(c.Args) != 2 {
			return false
		}
		inner, isCall := c.Args[0].(*ast.CallExpr)
		bound, isID := ast.Unparen(c.Args[1]).(*ast.Ident)
		return isCall && qualSel(inner.Fun) == "context.WithoutCancel" && isID && tp.info.Uses[bound] == timeout
	}
	pathTenant := func(c *ast.CallExpr) bool { return tp.callee(c) == tenantIDFn }
	postedReason := func(c *ast.CallExpr) bool {
		tv, ok := tp.info.Types[c.Fun]
		if !ok || !tv.IsType() || types.TypeString(tv.Type, nil) != "github.com/atknatk/tappa/internal/db.SuspensionReason" || len(c.Args) != 1 {
			return false
		}
		get, isCall := ast.Unparen(c.Args[0]).(*ast.CallExpr)
		if !isCall || len(get.Args) != 1 {
			return false
		}
		sel, isSel := get.Fun.(*ast.SelectorExpr)
		if !isSel || sel.Sel.Name != "Get" {
			return false
		}
		pf, isSel := sel.X.(*ast.SelectorExpr)
		v := tp.info.Types[get.Args[0]].Value
		return isSel && pf.Sel.Name == "PostForm" && v != nil && v.ExactString() == `"reason"`
	}
	var bad []string
	for _, c := range []struct {
		name    string
		fn      *types.Func
		handler *types.Func
	}{{"SuspendTenant", suspend, suspendH}, {"ReinstateTenant", reinstate, reinstateH}} {
		n := 0
		for _, id := range tp.usesOf(c.fn) {
			n++
			call := tp.callOf(id)
			if call == nil || !tp.in(c.handler, id.Pos()) {
				bad = append(bad, "SU1 "+c.name+" used at "+tp.where(id.Pos()))
				continue
			}
			if !definedBy(call.Args[0], detached) {
				bad = append(bad, "SU2 "+c.name+"'s context is not context.WithTimeout(context.WithoutCancel(...), suspensionWriteTimeout) at "+tp.where(id.Pos()))
			}
			if !definedBy(call.Args[2], pathTenant) {
				bad = append(bad, "SU3 "+c.name+"'s tenant is not tenantID's result at "+tp.where(id.Pos()))
			}
			if c.fn == suspend && !definedBy(call.Args[3], postedReason) {
				bad = append(bad, "SU4 SuspendTenant's reason is not db.SuspensionReason(r.PostForm.Get(\"reason\")) at "+tp.where(id.Pos()))
			}
		}
		if n != 1 {
			bad = append(bad, fmt.Sprintf("SU1 %s used %d time(s), want 1", c.name, n))
		}
	}
	for _, h := range []*types.Func{suspendH, reinstateH} {
		var answers, ids, reads []ast.Node
		for _, id := range tp.usesOf(answeredFn) {
			if tp.in(h, id.Pos()) {
				answers = append(answers, id)
			}
		}
		for _, id := range tp.usesOf(tenantIDFn) {
			if tp.in(h, id.Pos()) {
				ids = append(ids, id)
			}
		}
		for _, id := range tp.usesOf(readForm) {
			if tp.in(h, id.Pos()) {
				reads = append(reads, id)
			}
		}
		if len(answers) != 1 || len(ids) != 1 || len(reads) != 1 {
			bad = append(bad, fmt.Sprintf("SU5 %s calls answered %d, tenantID %d and readForm %d time(s), want one each", funcName(h), len(answers), len(ids), len(reads)))
			continue
		}
		if answers[0].Pos() < ids[0].Pos() || answers[0].Pos() > reads[0].Pos() {
			bad = append(bad, "SU5 "+funcName(h)+" calls answered before its id check or after its form read")
		}
	}
	slices.Sort(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if len(tp.usesOf(suspend)) != 1 || len(tp.usesOf(reinstate)) != 1 {
		t.Fatalf("CONTROL: SuspendTenant resolves at %d use(s), ReinstateTenant at %d; want one each", len(tp.usesOf(suspend)), len(tp.usesOf(reinstate)))
	}
}
