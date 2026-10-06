package operator_test

// op11_test.go -- M10 OP-11 phase B: /operator/tenants and /operator/tenants/{id} on the
// shipped router with the fake store (rig_test.go). The header table of their response
// classes (C67-C93), the read's second unit for each of the three reads (the read
// budget's since OP-13 phase B), the search term's path (the POST body, the result page,
// and nowhere else -- on the wire too), the boundary's refusals before any store call,
// the pager, the overview's banner
// and its placeholder for a tenant whose name shows nothing, the escaping of what a
// tenant and an operator typed, and the screens' contrast. Against PostgreSQL:
// op11_db_test.go.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

// seedTenants puts n tenants into the fake, newest first (the order op_read_tenants
// returns), named name + their number, and returns them.
func (g *rig) seedTenants(n int, name string) []db.TenantSummary {
	g.t.Helper()
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	start := len(g.store.tenants)
	for i := 0; i < n; i++ {
		g.store.tenants = append(g.store.tenants, db.TenantSummary{
			ID: uuid.New(), Name: fmt.Sprintf("%s %05d", name, start+i),
			CreatedAt: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC).Add(-time.Duration(start+i) * time.Minute),
			Plan:      "founding",
		})
	}
	return append([]db.TenantSummary(nil), g.store.tenants[start:]...)
}

// seedOverview puts one tenant's overview into the fake and returns its id.
func (g *rig) seedOverview(o db.TenantOverview) uuid.UUID {
	g.t.Helper()
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	g.store.overviews[o.ID] = o
	return o.ID
}

// lastQuery is the last TenantListQuery the fake was asked (false if none).
func (g *rig) lastQuery() (db.TenantListQuery, bool) {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	if len(g.store.queries) == 0 {
		return db.TenantListQuery{}, false
	}
	return g.store.queries[len(g.store.queries)-1], true
}

func searchForm(term, page string) url.Values { return url.Values{"q": {term}, "page": {page}} }

// TestOperatorHeaders_TheTenantClassesCarryThePolicy drives the twenty-seven response
// classes of the tenant screens (C67-C93) with the 40-class test's hostile drive and
// checks (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these twenty-seven, at WriteHeader (the recorder's
// Result().Header): the designed status, route, header names and values (designedHeaders:
// no Location but the sign-in's on any of them); no hostile value (hostileValues) in a
// header value or in the body, raw or query-escaped -- every one's last request carries
// the hostile query, which since OP-11 holds a search term and a page under the form's own
// names (hostileDrive adds it on /operator/tenants and /operator/tenants/{id}; C78's
// hand-built request gets it from hostileOn), so a list or a search that read its term or
// page from the URL, or echoed one, is red here; no script; and the store counts below:
// C76 (cross-origin), C84 (PUT) and C93 (POST on an overview) make no store call; C73, C77,
// C78, C79, C80 and C83 make no TenantList call; C87 and C91 make no TenantDetail call.
//
// PART II -- the list above, on these twenty-seven classes.
//
// PART III -- This test measures the twenty-seven classes and the raw and query-escaped
// forms only; anything else (examples: a class a later handler adds, a hostile value
// echoed HTML-escaped or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_TheTenantClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	g.seedTenants(3, "FAKE header tenant")
	tenant := g.seedOverview(db.TenantOverview{Name: "FAKE Header Tenant Ltd", Plan: "founding", BusinessType: "bar"})
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
	sfs := map[string]string{"Sec-Fetch-Site": "same-origin"}
	list := func(c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: "/operator/tenants", cookies: c, header: sfs}
	}
	search := func(form url.Values, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: "/operator/tenants", form: form, origin: opOrigin, cookies: c}
	}
	overview := func(id string, c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: "/operator/tenants/" + id, cookies: c, header: sfs}
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
	failing := func(method string, err error, r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			rq := r()
			g.store.mu.Lock()
			g.store.fail[method] = err
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); delete(g.store.fail, method); g.store.mu.Unlock() }()
			return send(rq)
		}
	}
	once := func(r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return send(r()) }
	}
	// none runs the drive and records which store methods it called.
	calls := map[string]map[string]int{}
	counted := func(class string, drive func() *httptest.ResponseRecorder) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			before := map[string]int{}
			for _, m := range []string{"TenantList", "TenantDetail"} {
				before[m] = g.store.count(m)
			}
			total := g.store.total()
			w := drive()
			calls[class] = map[string]int{"TenantList": g.store.count("TenantList") - before["TenantList"],
				"TenantDetail": g.store.count("TenantDetail") - before["TenantDetail"], "all": g.store.total() - total}
			return w
		}
	}
	// spent is a session with its 60 reads spent on list views (readLimit, OP-13 phase B):
	// the next read passes the gate (its 61st unit of 100) and is refused by the read
	// budget.
	spent := func() *http.Cookie {
		t.Helper()
		c := signIn()
		for i := 0; i < 60; i++ {
			if w := send(list(c)); w.Code != http.StatusOK {
				t.Fatalf("PREMISE: list view %d = %d", i+1, w.Code)
			}
		}
		return c
	}
	dead := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}
	classes := []headerClass{
		{"C67 tenant list", 200, once(func() req { return list(signIn()) })},
		{"C68 tenant list without a cookie", 303, once(func() req { return list() })},
		{"C69 tenant list with a dead cookie", 303, once(func() req { return list(dead) })},
		{"C70 tenant list, a same-site read", 303, once(func() req {
			r := list(signIn())
			r.header = map[string]string{"Sec-Fetch-Site": "same-site"}
			return r
		})},
		{"C71 tenant list, the read fails", 503, failing("TenantList", errFakeDB, func() req { return list(signIn()) })},
		{"C72 tenant list, the read's session is refused", 303, failing("TenantList", db.ErrOperatorRefused, func() req { return list(signIn()) })},
		{"C73 tenant list, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spent()
			return counted("C73", once(func() req { return list(c) }))()
		}},
		{"C74 search", 200, once(func() req { return search(searchForm("FAKE header", ""), signIn()) })},
		{"C75 search without a cookie", 303, once(func() req { return search(searchForm("FAKE header", "")) })},
		{"C76 search, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C76", func() *httptest.ResponseRecorder {
				r := search(searchForm("FAKE C76 term", ""), c)
				r.origin, r.header = "https://taptime.mt", map[string]string{"Sec-Fetch-Site": "same-site"}
				return send(r)
			})()
		}},
		{"C77 search, an oversized form", 413, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C77", once(func() req { return search(searchForm(strings.Repeat("x", 20<<10), ""), c) }))()
		}},
		{"C78 search, an unreadable form", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C78", func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/tenants", strings.NewReader("q=%zz"))
				r.Host = opHost
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Origin", opOrigin)
				r.AddCookie(c)
				hostileOn(r)
				r.RemoteAddr = ad.next() + ":1"
				*last = driveLog{r.Method, r.URL.Path}
				w := httptest.NewRecorder()
				g.h.ServeHTTP(w, r)
				return w
			})()
		}},
		{"C79 search, a term refused", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C79", once(func() req { return search(searchForm(strings.Repeat("ż", 255), ""), c) }))()
		}},
		{"C80 search, a page refused", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C80", once(func() req { return search(searchForm("FAKE header", "0"), c) }))()
		}},
		{"C81 search, the read fails", 503, failing("TenantList", errFakeDB, func() req { return search(searchForm("FAKE C81", ""), signIn()) })},
		{"C82 search, the read's session is refused", 303,
			failing("TenantList", db.ErrOperatorRefused, func() req { return search(searchForm("FAKE C82", ""), signIn()) })},
		{"C83 search, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spent()
			return counted("C83", once(func() req { return search(searchForm("FAKE C83", ""), c) }))()
		}},
		{"C84 PUT on the tenant list", 405, counted("C84", once(func() req {
			return req{method: http.MethodPut, path: "/operator/tenants", origin: opOrigin, form: url.Values{}}
		}))},
		{"C85 tenant overview", 200, once(func() req { return overview(tenant.String(), signIn()) })},
		{"C86 tenant overview without a cookie", 303, once(func() req { return overview(tenant.String()) })},
		{"C87 tenant overview, a malformed id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C87", once(func() req { return overview(strings.ReplaceAll(tenant.String(), "-", ""), c) }))()
		}},
		{"C88 tenant overview, an id no tenant has", 404, once(func() req { return overview(uuid.NewString(), signIn()) })},
		{"C89 tenant overview, the read fails", 503, failing("TenantDetail", errFakeDB, func() req { return overview(tenant.String(), signIn()) })},
		{"C90 tenant overview, the read's session is refused", 303,
			failing("TenantDetail", db.ErrOperatorRefused, func() req { return overview(tenant.String(), signIn()) })},
		{"C91 tenant overview, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spent()
			return counted("C91", once(func() req { return overview(tenant.String(), c) }))()
		}},
		{"C92 tenant overview, a same-site read", 303, once(func() req {
			r := overview(tenant.String(), signIn())
			r.header = map[string]string{"Sec-Fetch-Site": "same-site"}
			return r
		})},
		{"C93 POST on a tenant overview", 405, counted("C93", once(func() req {
			return req{method: http.MethodPost, path: "/operator/tenants/" + tenant.String(), origin: opOrigin, form: url.Values{}}
		}))},
	}
	if len(classes) != 27 {
		t.Fatalf("PREMISE: %d classes, the comment says 27", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(67+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 67+i, c.name, 67+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	for class, want := range map[string]map[string]int{
		"C73": {"TenantList": 0}, "C76": {"all": 0}, "C77": {"TenantList": 0}, "C78": {"TenantList": 0},
		"C79": {"TenantList": 0}, "C80": {"TenantList": 0}, "C83": {"TenantList": 0}, "C84": {"all": 0},
		"C87": {"TenantDetail": 0}, "C91": {"TenantDetail": 0}, "C93": {"all": 0},
	} {
		got, ok := calls[class]
		if !ok {
			t.Errorf("PREMISE: %s's store calls were not counted", class)
			continue
		}
		for m, n := range want {
			if got[m] != n {
				t.Errorf("%s: %d %s call(s), want %d", class, got[m], m, n)
			}
		}
	}
}

// TestTenantPages_AReadCountsTwiceAgainstTheSessionBudget measures the two units of each
// of the three tenant reads -- the list, a search, an overview. THE NAME IS OP-11'S AND NO
// LONGER THE WHOLE RULE: until OP-13 phase B both units were the session budget's
// (sessionLimit 200); since then the gate charges one unit of the session's REQUEST budget
// (sessionLimit, 100) and the handler one of its READ budget (readLimit, 60). The name
// stays because docs/plan/m10-platform.md and ADR 0021 cite it in records that are not
// rewritten after the fact.
//
// PART I -- one session, 60 overviews from 60 addresses: 60 x 200 and 60 TenantDetail
// calls; the 61st is 429 from the read budget with no 61st read. Then, for each of the
// three reads, a fresh session: 60 reads of that kind -- all 200 -- and the next read of
// that kind is 429 from the read budget: the gate's predicate ran (TouchOperatorSession
// +1) and the store's read did not. CONTROL: another session reads each. And an overview
// refused for a malformed id spends NO read (2nd round's B2, re-measured for the read
// budget): a session with 30 such overviews (each 404, no TenantDetail call) still reads
// 60 overviews -- all 200 -- and the 61st is 429; had each malformed id spent a read, the
// 31st of those overviews would be. The session's request budget is then at 91 units: 9
// console views are 200 and the next is 429 at the gate.
//
// PART II -- the counts above. PART III -- This test measures these sequences only; a read
// a later screen adds is code review's (and its own test's) -- no completeness claim.
func TestTenantPages_AReadCountsTwiceAgainstTheSessionBudget(t *testing.T) {
	g := newRig(t)
	g.seedTenants(5, "FAKE budget tenant")
	tenant := g.seedOverview(db.TenantOverview{Name: "FAKE Budget Tenant Ltd", Plan: "founding", BusinessType: "cafe"})
	n := 0
	send := func(r req) *httptest.ResponseRecorder {
		n++
		r.host = opHost
		r.remote = fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1)
		return g.do(r)
	}
	sfs := map[string]string{"Sec-Fetch-Site": "same-origin"}
	reads := []struct {
		name, store string
		req         func(*http.Cookie) req
	}{
		{"the list", "TenantList", func(c *http.Cookie) req {
			return req{method: http.MethodGet, path: "/operator/tenants", cookies: []*http.Cookie{c}, header: sfs}
		}},
		{"a search", "TenantList", func(c *http.Cookie) req {
			return req{method: http.MethodPost, path: "/operator/tenants", form: searchForm("FAKE", ""), origin: opOrigin, cookies: []*http.Cookie{c}}
		}},
		{"an overview", "TenantDetail", func(c *http.Cookie) req {
			return req{method: http.MethodGet, path: "/operator/tenants/" + tenant.String(), cookies: []*http.Cookie{c}, header: sfs}
		}},
	}
	a := g.signIn(g.active())
	for i := 0; i < 60; i++ {
		if w := send(reads[2].req(a)); w.Code != http.StatusOK {
			t.Fatalf("overview %d of one session = %d, want 200", i+1, w.Code)
		}
	}
	if got := g.store.count("TenantDetail"); got != 60 {
		t.Fatalf("PREMISE: %d read(s) for 60 overviews", got)
	}
	if w := send(reads[2].req(a)); w.Code != http.StatusTooManyRequests || g.store.count("TenantDetail") != 60 {
		t.Fatalf("the 61st overview of one session = %d with %d read(s), want 429 and 60", w.Code, g.store.count("TenantDetail"))
	}
	for _, rd := range reads {
		b := g.signIn(g.active())
		for i := 0; i < 60; i++ {
			if w := send(rd.req(b)); w.Code != http.StatusOK {
				t.Fatalf("%s: read %d of the session = %d", rd.name, i+1, w.Code)
			}
		}
		touches, stored := g.store.count("TouchOperatorSession"), g.store.count(rd.store)
		if w := send(rd.req(b)); w.Code != http.StatusTooManyRequests {
			t.Fatalf("%s: the 61st read = %d, want 429", rd.name, w.Code)
		}
		if g.store.count("TouchOperatorSession") != touches+1 || g.store.count(rd.store) != stored {
			t.Errorf("%s refused by the read budget: predicate +%d, %s +%d; want +1, +0", rd.name,
				g.store.count("TouchOperatorSession")-touches, rd.store, g.store.count(rd.store)-stored)
		}
		if w := send(rd.req(g.signIn(g.active()))); w.Code != http.StatusOK {
			t.Fatalf("CONTROL: %s from another session = %d", rd.name, w.Code)
		}
	}
	// A malformed id: one unit of the request budget, none of the read budget.
	c := g.signIn(g.active())
	details := g.store.count("TenantDetail")
	for i := 0; i < 30; i++ {
		w := send(req{method: http.MethodGet, path: "/operator/tenants/" + strings.ReplaceAll(tenant.String(), "-", ""),
			cookies: []*http.Cookie{c}, header: sfs})
		if w.Code != http.StatusNotFound {
			t.Fatalf("malformed-id overview %d = %d, want 404", i+1, w.Code)
		}
	}
	if g.store.count("TenantDetail") != details {
		t.Fatalf("PREMISE: the malformed ids reached the store %d time(s)", g.store.count("TenantDetail")-details)
	}
	for i := 0; i < 60; i++ {
		if w := send(reads[2].req(c)); w.Code != http.StatusOK {
			t.Fatalf("overview %d after 30 malformed-id overviews = %d, want 200 -- a malformed id spent a read", i+1, w.Code)
		}
	}
	if w := send(reads[2].req(c)); w.Code != http.StatusTooManyRequests {
		t.Errorf("the 61st overview after 30 malformed-id overviews = %d, want 429 (the read budget)", w.Code)
	}
	console := func() *httptest.ResponseRecorder {
		return send(req{method: http.MethodGet, path: "/operator", cookies: []*http.Cookie{c}, header: sfs})
	}
	for i := 0; i < 9; i++ {
		if w := console(); w.Code != http.StatusOK {
			t.Fatalf("console view %d at 91 units = %d, want 200", i+1, w.Code)
		}
	}
	if w := console(); w.Code != http.StatusTooManyRequests {
		t.Errorf("the 101st request of the session = %d, want 429 at the gate", w.Code)
	}
}

// termNeedles are the forms of a term the search below looks for: the term, its first
// eight and its first four characters, each raw, query-escaped, path-escaped,
// HTML-escaped, %q-quoted (inside) and as a JSON string (inside).
func termNeedles(term string) []string {
	r := []rune(term)
	var out []string
	for _, part := range []string{term, string(r[:8]), string(r[:4])} {
		q := strconv.Quote(part)
		j, _ := json.Marshal(part)
		out = append(out, part, url.QueryEscape(part), url.PathEscape(part), html.EscapeString(part), q[1:len(q)-1],
			string(j[1:len(j)-1]))
	}
	return out
}

// TestTenantSearch_NoLogLineHeaderOrOtherPageCarriesTheTerm measures the search term's path
// (tenants.go's comment) through a REAL server: each request below goes over a TCP
// connection to httptest.NewServer(the shipped router), and its answer is read off the
// wire -- status, every header net/http's client parses (Location and Set-Cookie among
// them) and the body.
//
// PART I -- two terms, a name-shaped one and an address-shaped one, each opening with
// characters no other value of this test carries; their needles (termNeedles: the term,
// its first eight and first four characters, in six spellings). In each arm, at the
// process and access log (both shipped formats at Debug) and in the wire's headers: no
// needle. In each arm's body: no needle, EXCEPT the two result pages, where the term is
// the page's own -- and there the HTML-escaped term IS in the search box, the "matching"
// line and the Next form's hidden q field (the positive control of the body scan), and
// nowhere else on that page: the term and its first four characters each occur exactly
// three times, no href/action/src value carries a needle, and the document title (exactly
// "Tenants — Taptime operator") carries none (3rd round, F1). The
// arms: a search (200, a full page, so the pager carries the term) and an address search
// with spaces around it (200; the store got it trimmed); a term over the bound, a term with
// a line break, a page out of range (400 each, no store call); the store failing (503,
// and its log line is there); the store refusing the session (303, Location exactly
// /operator/login); a cross-origin search (403); the list with the term and a page in its
// query string, and a search whose term is in the query string with an empty form (200
// each; the store got "" for both, page 1). CONTROL: a log line that carries the term is
// found by the same scan.
//
// PART II -- the list above. PART III -- This test measures these arms, these surfaces
// and these spellings; the ingress's log, the browser's history and anything outside this
// process are outside it -- they are why the term is in the body (no completeness claim).
func TestTenantSearch_NoLogLineHeaderOrOtherPageCarriesTheTerm(t *testing.T) {
	g := newRig(t)
	const name = "Żq§ op11b FAKE term"
	const address = "Qz~é.op11b-fake@example.test"
	// More than a page of tenants, none named with either term, and every one of them
	// matching ANY term: a search's result page is full, so its pager carries the term,
	// and no tenant's name puts the term on a page that must not carry it.
	g.seedTenants(operator.TenantPageSizeForTest+3, "FAKE wire tenant")
	g.store.mu.Lock()
	g.store.matches = func(string, db.TenantSummary) bool { return true }
	g.store.mu.Unlock()
	sess := g.signIn(g.active())
	srv := httptest.NewServer(g.h)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	type answer struct {
		status int
		header http.Header
		body   string
		logs   string
	}
	wire := func(method, path string, form url.Values, origin, site string) answer {
		t.Helper()
		g.logs.Reset()
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		r, err := http.NewRequest(method, srv.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = opHost
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("Sec-Fetch-Site", site)
		r.AddCookie(sess)
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return answer{res.StatusCode, res.Header, string(b), g.logs.String()}
	}
	post := func(form url.Values) answer {
		return wire(http.MethodPost, "/operator/tenants", form, opOrigin, "same-origin")
	}
	headerText := func(h http.Header) string {
		var b strings.Builder
		for k, vs := range h {
			for _, v := range vs {
				b.WriteString(k + ": " + v + "\n")
			}
		}
		return b.String()
	}
	found := func(text string, terms ...string) []string {
		var out []string
		for _, term := range terms {
			for _, n := range termNeedles(term) {
				if strings.Contains(text, n) {
					out = append(out, fmt.Sprintf("%d-byte needle", len(n)))
				}
			}
		}
		return out
	}
	queries := func() int { return g.store.count("TenantList") }
	type arm struct {
		name     string
		a        answer
		status   int
		pageTerm string // the term the result page carries by design ("" = none)
	}
	var arms []arm
	run := func(name string, status int, pageTerm string, a answer) answer {
		arms = append(arms, arm{name, a, status, pageTerm})
		return a
	}

	q0 := queries()
	run("a search", 200, name, post(searchForm(name, "")))
	if q, _ := g.lastQuery(); q.Search != name || q.Number != 1 || int(q.Size) != operator.TenantPageSizeForTest {
		t.Errorf("a search reached the store as %+v", q)
	}
	run("an address search, spaces around it", 200, address, post(searchForm("  "+address+" \t", "1")))
	if q, _ := g.lastQuery(); q.Search != address {
		t.Errorf("the store got the address search as %q, want it trimmed", q.Search)
	}
	if queries() != q0+2 {
		t.Fatalf("PREMISE: the two searches made %d store call(s)", queries()-q0)
	}
	run("a term over the bound", 400, "", post(searchForm(name+strings.Repeat("ż", 254), "")))
	run("a term with a line break", 400, "", post(searchForm(name+"\nsecond line", "")))
	run("a page out of range", 400, "", post(searchForm(name, strconv.Itoa(operator.MaxTenantPageForTest+1))))
	if queries() != q0+2 {
		t.Errorf("the three refusals made %d store call(s), want 0", queries()-q0-2)
	}
	g.store.mu.Lock()
	g.store.fail["TenantList"] = errFakeDB
	g.store.mu.Unlock()
	fails := run("the store fails", 503, "", post(searchForm(name, "")))
	g.store.mu.Lock()
	g.store.fail["TenantList"] = db.ErrOperatorRefused
	g.store.mu.Unlock()
	refused := run("the store refuses the session", 303, "", post(searchForm(address, "")))
	g.store.mu.Lock()
	delete(g.store.fail, "TenantList")
	g.store.mu.Unlock()
	if !strings.Contains(fails.logs, "the tenant list could not be read") {
		t.Errorf("PREMISE: the failing search wrote no fault line")
	}
	if loc := refused.header.Values("Location"); len(loc) != 1 || loc[0] != "/operator/login" {
		t.Errorf("the refused session's Location = %q, want exactly /operator/login", loc)
	}
	run("a cross-origin search", 403, "", wire(http.MethodPost, "/operator/tenants", searchForm(name, ""), "https://taptime.mt", "same-site"))
	run("the list, the term in its query string", 200, "",
		wire(http.MethodGet, "/operator/tenants?q="+url.QueryEscape(name)+"&page=2", nil, "", "same-origin"))
	if q, _ := g.lastQuery(); q.Search != "" || q.Number != 1 {
		t.Errorf("the list with the term in its URL reached the store as %+v, want every tenant, page 1", q)
	}
	run("a search, the term in its query string", 200, "",
		wire(http.MethodPost, "/operator/tenants?q="+url.QueryEscape(address)+"&page=2", url.Values{}, opOrigin, "same-origin"))
	if q, _ := g.lastQuery(); q.Search != "" || q.Number != 1 {
		t.Errorf("a search with the term in its URL and an empty form reached the store as %+v, want every tenant, page 1", q)
	}

	for _, a := range arms {
		if a.a.status != a.status {
			t.Errorf("PREMISE: %s = %d, want %d", a.name, a.a.status, a.status)
		}
		if hits := found(a.a.logs, name, address); len(hits) != 0 {
			t.Errorf("%s: the process or access log carries the term: %v", a.name, hits)
		}
		if hits := found(headerText(a.a.header), name, address); len(hits) != 0 {
			t.Errorf("%s: the wire's headers carry the term: %v", a.name, hits)
		}
		if a.pageTerm == "" {
			if hits := found(a.a.body, name, address); len(hits) != 0 {
				t.Errorf("%s: the body carries the term: %v", a.name, hits)
			}
			continue
		}
		other := address
		if a.pageTerm == address {
			other = name
		}
		if hits := found(a.a.body, other); len(hits) != 0 {
			t.Errorf("%s: the body carries the OTHER term: %v", a.name, hits)
		}
		esc := html.EscapeString(a.pageTerm)
		for _, want := range []string{`name="q" value="` + esc + `"`, `<bdi class="font-mono">` + esc + `</bdi>`} {
			if !strings.Contains(a.a.body, want) {
				t.Errorf("%s: the result page does not carry %q -- the body scan's positive control", a.name, want)
			}
		}
		// On the result page itself, no link and no form action carries the term (the
		// forms post it; the page's view comment says so).
		for _, m := range regexp.MustCompile(`\s(href|action|src)="([^"]*)"`).FindAllStringSubmatch(a.a.body, -1) {
			if hits := found(html.UnescapeString(m[2]), name, address); len(hits) != 0 {
				t.Errorf("%s: the result page's %s %q carries the term", a.name, m[1], m[2])
			}
		}
		// WHERE on the result page, exactly (3rd round, F1): the search box, the "matching"
		// line and the pager's one hidden field (page 1 of 53 tenants: Next, no Previous) --
		// 2 + one per pager form = 3. The term escaped, and its first four characters (so a
		// cut-short copy is counted too), each exactly three times; and none of the needles
		// in the document title, which a browser keeps in its history and shows on the tab.
		r := []rune(a.pageTerm)
		for _, part := range []string{a.pageTerm, string(r[:4])} {
			if n := strings.Count(a.a.body, html.EscapeString(part)); n != 3 {
				t.Errorf("%s: the result page carries the term's %d-byte form %d time(s), want 3 (box, matching line, Next)",
					a.name, len(part), n)
			}
		}
		if n := strings.Count(a.a.body, `<input type="hidden" name="q" value="`+esc+`">`); n != 1 {
			t.Errorf("%s: the pager carries the term in %d hidden field(s), want 1 (Next)", a.name, n)
		}
		titles := regexp.MustCompile(`(?s)<title>(.*?)</title>`).FindAllStringSubmatch(a.a.body, -1)
		if len(titles) != 1 || titles[0][1] != "Tenants — Taptime operator" {
			t.Errorf("%s: the result page's title is %q, want exactly one, %q", a.name, titles, "Tenants — Taptime operator")
		} else if hits := found(titles[0][1], name, address); len(hits) != 0 {
			t.Errorf("%s: the document title carries the term: %v", a.name, hits)
		}
	}
	// The full page's pager carries the term in its hidden field (the first search found
	// more than a page).
	if n := strings.Count(arms[0].a.body, `<input type="hidden" name="q" value="`+html.EscapeString(name)+`">`); n != 1 {
		t.Errorf("the first search's pager carries the term in %d hidden field(s), want 1 (Next)", n)
	}
	// CONTROL: the scan finds the term in a log line that carries it.
	var control bytes.Buffer
	captureAt(&control, 0).Info("control", "q", name)
	if len(found(control.String(), name)) == 0 {
		t.Fatal("CONTROL: a log line carrying the term is not found by the scan")
	}
}

// TestTenantSearch_TheBoundaryRefusesBeforeTheStore holds tenants.go's tenantSearchTerm
// and tenantPage to two tables, through POST /operator/tenants.
//
// PART I -- refused with 400 and the page that says nothing was searched, NO store call:
// a term of 255 characters (and one that is 255 after its spaces are trimmed), a NUL, a
// tab, a line feed, a carriage return, DEL, U+0085, the line and paragraph separators,
// invalid UTF-8; a page of 0, -1, +2, " 2", "2 ", 1.5, 1e3, maxTenantPage + 1, 99999, abc,
// a full-width digit. Accepted, the store getting the term TRIMMED and the page as a
// number: 254 characters, a term with spaces and no-break spaces around it, an empty term
// and one of spaces (every tenant), inner spaces, a zero-width joiner, LIKE's
// metacharacters; the pages "", 1, 7 and maxTenantPage. A request body over 16 KiB is 413
// with no store call. "No store call" counts every TenantList call, one the fake refuses
// as internal/db would (ErrTenantSearchRefused) included -- so a bound left to internal/db
// is red here. The refusal pages print db.MaxTenantSearchRunes and maxTenantPage, and the
// search box's maxlength is db.MaxTenantSearchRunes and asks the browser to keep no
// history of the box (autocomplete off).
//
// PART II -- the two tables above. PART III -- This test measures these inputs only (no
// completeness claim).
func TestTenantSearch_TheBoundaryRefusesBeforeTheStore(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	// Every TenantList call, the ones the fake refuses as internal/db would included.
	queries := func() int { return g.store.count("TenantList") }
	postRaw := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/tenants", strings.NewReader(body))
		r.Host = opHost
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", opOrigin)
		r.AddCookie(c)
		r.RemoteAddr = "192.0.2.77:1"
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w
	}
	for name, raw := range map[string]string{
		"255 characters":                "q=" + url.QueryEscape(strings.Repeat("ż", 255)),
		"255 characters after the trim": "q=" + url.QueryEscape(" "+strings.Repeat("ż", 255)+" "),
		"a NUL":                         "q=a%00b",
		"a tab":                         "q=a%09b",
		"a line feed":                   "q=a%0Ab",
		"a carriage return":             "q=a%0Db",
		"DEL":                           "q=a%7Fb",
		"U+0085":                        "q=" + url.QueryEscape("a\u0085b"),
		"U+2028":                        "q=" + url.QueryEscape("a\u2028b"),
		"U+2029":                        "q=" + url.QueryEscape("a\u2029b"),
		"invalid UTF-8":                 "q=a%FFb",
		"invalid UTF-8 at the end":      "q=kebab%C3",
	} {
		before := queries()
		w := postRaw(raw)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "That search could not be used") ||
			!strings.Contains(w.Body.String(), "at most "+strconv.Itoa(db.MaxTenantSearchRunes)+" characters") ||
			!strings.Contains(w.Body.String(), "Nothing was searched") {
			t.Errorf("a term with %s = %d, want 400 and the refusal page naming the bound", name, w.Code)
		}
		if queries() != before {
			t.Errorf("a term with %s reached the store", name)
		}
	}
	for _, page := range []string{"0", "-1", "+2", " 2", "2 ", "1.5", "1e3", strconv.Itoa(operator.MaxTenantPageForTest + 1), "99999", "abc", "２"} {
		before := queries()
		w := postRaw("q=kebab&page=" + url.QueryEscape(page))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "That page does not exist") ||
			!strings.Contains(w.Body.String(), "pages 1 to "+strconv.Itoa(operator.MaxTenantPageForTest)) {
			t.Errorf("page %q = %d, want 400 and the page refusal naming the bound", page, w.Code)
		}
		if queries() != before {
			t.Errorf("page %q reached the store", page)
		}
	}
	for _, tc := range []struct {
		name, term, want string
	}{
		{"254 characters", strings.Repeat("ż", 254), strings.Repeat("ż", 254)},
		{"spaces and no-break spaces around it", " \u00a0kebab\u3000\t", "kebab"},
		{"empty", "", ""},
		{"spaces alone", "   ", ""},
		{"inner spaces", "kebab  factory", "kebab  factory"},
		{"a zero-width joiner", "a\u200db", "a\u200db"},
		{"LIKE's metacharacters", `%_\`, `%_\`},
	} {
		before := queries()
		w := postRaw("q=" + url.QueryEscape(tc.term))
		q, _ := g.lastQuery()
		if w.Code != http.StatusOK || queries() != before+1 || q.Search != tc.want || q.Number != 1 {
			t.Errorf("CONTROL: a term %s = %d, store %+v; want 200 and %q on page 1", tc.name, w.Code, q, tc.want)
		}
	}
	for page, want := range map[string]int32{"": 1, "1": 1, "7": 7, strconv.Itoa(operator.MaxTenantPageForTest): int32(operator.MaxTenantPageForTest)} {
		w := postRaw("q=kebab&page=" + page)
		q, _ := g.lastQuery()
		if w.Code != http.StatusOK || q.Number != want || int(q.Size) != operator.TenantPageSizeForTest {
			t.Errorf("CONTROL: page %q = %d, store %+v; want 200, page %d of %d", page, w.Code, q, want, operator.TenantPageSizeForTest)
		}
	}
	before := queries()
	if w := postRaw("q=" + strings.Repeat("x", 16<<10)); w.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(w.Body.String(), "That search was too large") || queries() != before {
		t.Errorf("a 16 KiB request body = %d with %d store call(s), want 413 and none", w.Code, queries()-before)
	}
	page := g.get("/operator/tenants", c).Body.String()
	if !strings.Contains(page, `name="q" value="" maxlength="`+strconv.Itoa(db.MaxTenantSearchRunes)+`" autocomplete="off"`) {
		t.Errorf("the search box's maxlength is not db.MaxTenantSearchRunes (%d), or it does not turn autocomplete off", db.MaxTenantSearchRunes)
	}
}

// TestTenantList_PagesForwardOnlyAfterAFullPage: the pager (tenants.go's tenantsView).
//
// PART I -- 120 tenants in the fake: page 1 lists 50 in the store's order and offers Next
// (a form posting page 2 and the term) and no Previous; page 2 offers both; page 3 lists
// 20 and offers Previous only; page 4 lists none and says the list ended. With 50 000
// tenants, page maxTenantPage is full and offers no Next. Every call asked the store for
// tenantPageSize -- a "size" field in the form is not read. GET /operator/tenants reads
// page 1 whatever its URL says. An unsearched list (GET, an empty or blank term) has no
// "Tenants matching" line and no "Show every tenant" link; a searched one has both.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestTenantList_PagesForwardOnlyAfterAFullPage(t *testing.T) {
	g := newRig(t)
	seeded := g.seedTenants(120, "FAKE pager tenant")
	c := g.signIn(g.active())
	rowRe := regexp.MustCompile(`(?s)<li class="op-row">(.*?)</li>`)
	pageOf := func(n string) string {
		form := searchForm("", n)
		form.Set("size", "200")
		w := g.post("/operator/tenants", form, c)
		if w.Code != http.StatusOK {
			t.Fatalf("page %s = %d", n, w.Code)
		}
		return w.Body.String()
	}
	pager := func(body string) (prev, next bool) {
		return strings.Contains(body, ">Previous page</button>"), strings.Contains(body, ">Next page</button>")
	}
	for _, tc := range []struct {
		page       string
		rows       int
		prev, next bool
		first      int // index into seeded of the first row
	}{
		{"1", 50, false, true, 0}, {"2", 50, true, true, 50}, {"3", 20, true, false, 100},
	} {
		body := pageOf(tc.page)
		rows := rowRe.FindAllStringSubmatch(body, -1)
		if len(rows) != tc.rows {
			t.Errorf("page %s lists %d row(s), want %d", tc.page, len(rows), tc.rows)
			continue
		}
		if !strings.Contains(rows[0][1], `<span class="break-all text-xs text-ink/70">`+seeded[tc.first].ID.String()+`</span>`) {
			t.Errorf("page %s does not open with the store's row %d (its id printed in the row)", tc.page, tc.first)
		}
		if p, n := pager(body); p != tc.prev || n != tc.next {
			t.Errorf("page %s: previous %v next %v, want %v %v", tc.page, p, n, tc.prev, tc.next)
		}
		if tc.next {
			want, _ := strconv.Atoi(tc.page)
			if !strings.Contains(body, `<input type="hidden" name="page" value="`+strconv.Itoa(want+1)+`">`) {
				t.Errorf("page %s's Next does not post page %d", tc.page, want+1)
			}
		}
	}
	if body := pageOf("4"); len(rowRe.FindAllString(body, -1)) != 0 || !strings.Contains(body, "the list ended on the page before") {
		t.Error("page 4 of 120 tenants does not say the list ended")
	}
	g.store.mu.Lock()
	for _, q := range g.store.queries {
		if int(q.Size) != operator.TenantPageSizeForTest {
			t.Errorf("the store was asked for a page of %d", q.Size)
		}
	}
	g.store.mu.Unlock()
	if w := g.get("/operator/tenants?page=3&q=FAKE", c); w.Code != http.StatusOK {
		t.Fatalf("GET with a query = %d", w.Code)
	} else if q, _ := g.lastQuery(); q.Number != 1 || q.Search != "" {
		t.Errorf("GET /operator/tenants?page=3&q=FAKE reached the store as %+v, want page 1 of every tenant", q)
	}
	// An unsearched list -- GET, or a POST with no term -- has no "matching" line and no
	// way back to every tenant (2nd round, N5); a searched one has both (CONTROL).
	for what, body := range map[string]string{
		"GET":                    g.get("/operator/tenants", c).Body.String(),
		"a POST with no term":    pageOf("1"),
		"a POST with blank term": g.post("/operator/tenants", searchForm("   ", ""), c).Body.String(),
	} {
		if strings.Contains(body, "Tenants matching") || strings.Contains(body, "Show every tenant") {
			t.Errorf("%s: an unsearched list says it is a search's result", what)
		}
	}
	if body := g.post("/operator/tenants", searchForm("FAKE pager", ""), c).Body.String(); !strings.Contains(body, "Tenants matching") ||
		!strings.Contains(body, `<a href="/operator/tenants" class="op-link">Show every tenant</a>`) {
		t.Error("CONTROL: a searched list has no matching line or no way back to every tenant")
	}
	// The last page.
	g.seedTenants(operator.MaxTenantPageForTest*operator.TenantPageSizeForTest, "FAKE many")
	body := pageOf(strconv.Itoa(operator.MaxTenantPageForTest))
	if n := len(rowRe.FindAllString(body, -1)); n != operator.TenantPageSizeForTest {
		t.Fatalf("PREMISE: page %d lists %d row(s)", operator.MaxTenantPageForTest, n)
	}
	if _, next := pager(body); next {
		t.Errorf("the full page %d offers a next page past the bound", operator.MaxTenantPageForTest)
	}
}

// TestTenantOverview_NamesTheTenantInTheBannerAndRefusesABadPath: the overview
// (tenants.go's tenantOverview, operatorpages.TenantOverview inside TenantScreen).
//
// PART I -- a tenant with markup in its name and a sign-up time stored at UTC+2: 200; the
// banner and the document title carry the name escaped, the page its id, sign-up time (in
// UTC, as is the list row's), plan and business type and the four counts, each under its
// own label, in mono; the
// store was asked for that id; the same id in capitals reaches the same tenant. A path
// that is not the 36-character hyphenated id -- 32 hex digits, braces, urn:uuid:, 35 and
// 37 characters, a non-hex letter -- is 404 with the not-an-id page and no TenantDetail
// call. An id the store has no tenant for: 404 with the no-such-tenant page, after one
// call. The store failing: 503, the log line naming the tenant's id and not the session's
// hash. The store refusing the session: 303 to the sign-in.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestTenantOverview_NamesTheTenantInTheBannerAndRefusesABadPath(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const name = `Rusty <Bar> & "Grill"`
	// The database hands times back in the connection's zone; the screen prints UTC
	// (2nd round, B3): 11:30 at UTC+2 is 09:30 UTC.
	plus2 := time.FixedZone("x", 2*3600)
	id := g.seedOverview(db.TenantOverview{Name: name, Plan: "founding", BusinessType: "bar",
		CreatedAt: time.Date(2026, 7, 5, 11, 30, 0, 0, plus2), Locations: 9, ActiveEmployees: 41, ActivePlaques: 7, ActiveAdmins: 2})
	details := func() int { g.store.mu.Lock(); defer g.store.mu.Unlock(); return len(g.store.details) }
	w := g.get("/operator/tenants/"+id.String(), c)
	body := w.Body.String()
	esc := html.EscapeString(name)
	banner := strings.Index(body, `<section class="op-tenant" aria-label="Tenant">`)
	if w.Code != http.StatusOK || banner < 0 || !strings.Contains(body[banner:], "<bdi>"+esc+"</bdi>") ||
		!strings.Contains(body, "<title>Tenant overview — "+esc+" — Taptime operator</title>") {
		t.Fatalf("the overview = %d; the banner or the title does not carry the escaped name", w.Code)
	}
	for label, want := range map[string]string{"Tenant id": id.String(), "Signed up": "2026-07-05 09:30 UTC", "Plan": "founding",
		"Business type": "bar"} {
		if !regexp.MustCompile(`<dt class="docket-label">` + label + `</dt>\s*<dd class="mt-1 break-all font-mono text-sm">` +
			regexp.QuoteMeta(want) + `</dd>`).MatchString(body) {
			t.Errorf("the overview's %s is not %q, mono", label, want)
		}
	}
	for label, want := range map[string]string{"Locations": "9", "Active employees": "41", "Active plaques": "7", "Active panel accounts": "2"} {
		if !regexp.MustCompile(`<dt class="docket-label">` + label + `</dt>\s*<dd class="mt-1 font-mono text-3xl font-bold">` +
			want + `</dd>`).MatchString(body) {
			t.Errorf("the overview's %s figure is not %s, mono", label, want)
		}
	}
	if strings.Contains(body, "11:30") {
		t.Error("the overview prints the sign-up time in its stored zone, not in UTC")
	}
	// A list row: the same time, UTC+2 in, UTC out.
	g.store.mu.Lock()
	g.store.tenants = []db.TenantSummary{{ID: id, Name: name, CreatedAt: time.Date(2026, 7, 5, 11, 30, 0, 0, plus2), Plan: "founding"}}
	g.store.mu.Unlock()
	if list := g.get("/operator/tenants", c).Body.String(); !strings.Contains(list, "<span>Since 2026-07-05 09:30 UTC</span>") ||
		strings.Contains(list, "11:30") {
		t.Error("the list row does not print the sign-up time in UTC")
	}
	g.store.mu.Lock()
	asked := append([]uuid.UUID(nil), g.store.details...)
	g.store.mu.Unlock()
	if len(asked) != 1 || asked[0] != id {
		t.Errorf("the store was asked for %v, want the path's id", asked)
	}
	if w := g.get("/operator/tenants/"+strings.ToUpper(id.String()), c); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), esc) {
		t.Errorf("the id in capitals = %d, want the same tenant", w.Code)
	}
	for _, bad := range []string{strings.ReplaceAll(id.String(), "-", ""), "%7B" + id.String() + "%7D", "urn:uuid:" + id.String(),
		id.String()[:35], id.String() + "0", "g" + id.String()[1:], "x"} {
		before := details()
		w := g.get("/operator/tenants/"+bad, c)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "That link does not name a tenant") || details() != before {
			t.Errorf("the path id %q = %d with %d store call(s), want 404, the not-an-id page and none", bad, w.Code, details()-before)
		}
	}
	before := details()
	if w := g.get("/operator/tenants/"+uuid.NewString(), c); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "There is no tenant with that id") || details() != before+1 {
		t.Errorf("an id no tenant has = %d with %d store call(s), want 404, the no-such-tenant page and one", w.Code, details()-before)
	}
	g.store.mu.Lock()
	g.store.fail["TenantDetail"] = errFakeDB
	var hashes []string
	for h := range g.store.live {
		hashes = append(hashes, h)
	}
	g.store.mu.Unlock()
	g.logs.Reset()
	if w := g.get("/operator/tenants/"+id.String(), c); w.Code != http.StatusServiceUnavailable ||
		!strings.Contains(w.Body.String(), "That tenant could not be loaded") {
		t.Errorf("a failing overview = %d", w.Code)
	}
	if !strings.Contains(g.logs.String(), "overview could not be read") || !strings.Contains(g.logs.String(), id.String()) {
		t.Errorf("the failing overview's log line does not name the tenant")
	}
	for _, h := range hashes {
		if strings.Contains(g.logs.String(), h) || strings.Contains(g.logs.String(), c.Value) {
			t.Errorf("the failing overview's log carries the session's hash or token")
		}
	}
	g.store.mu.Lock()
	g.store.fail["TenantDetail"] = db.ErrOperatorRefused
	g.store.mu.Unlock()
	if w := g.get("/operator/tenants/"+id.String(), c); w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" {
		t.Errorf("a refused session's overview = %d %q, want the sign-in's 303", w.Code, w.Result().Header.Get("Location"))
	}
}

// blankNames are tenant names that show nothing: tenants.name has no CHECK against them.
var blankNames = map[string]string{
	"empty": "", "spaces": "   ", "zero-width": "\u200b\u2060", "a Hangul filler": "\u3164", "a braille blank": "\u2800",
	"a line break": "\r\n",
}

// TestTenantOverview_AnUnnamedTenantIsNamedByItsID: a tenant whose stored name shows
// nothing (blankNames) is named by its id -- the overview's banner says "Unnamed tenant"
// with the id under it and the document title "Unnamed tenant <id>"; the list's row says
// "Unnamed tenant" as its link's text, and its id beside it as the row's own text (not only
// in the link's href). operatorpages.UnnamedTenant
// refuses an empty id. CONTROL: a name with one visible letter among invisible characters
// is the tenant's name.
func TestTenantOverview_AnUnnamedTenantIsNamedByItsID(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	for label, name := range blankNames {
		id := g.seedOverview(db.TenantOverview{Name: name, Plan: "founding", BusinessType: "other"})
		w := g.get("/operator/tenants/"+id.String(), c)
		body := w.Body.String()
		banner := strings.Index(body, `<section class="op-tenant" aria-label="Tenant">`)
		if w.Code != http.StatusOK || banner < 0 ||
			!strings.Contains(body[banner:], `<p class="font-display text-xl font-bold tracking-tight">Unnamed tenant</p>`) ||
			!strings.Contains(body[banner:], `<p class="break-all font-mono text-sm">`+id.String()+`</p>`) ||
			!strings.Contains(body, "<title>Tenant overview — Unnamed tenant "+id.String()+" — Taptime operator</title>") {
			t.Errorf("a tenant named %s: %d, the banner or the title does not name it by its id", label, w.Code)
		}
		g.store.mu.Lock()
		g.store.tenants = []db.TenantSummary{{ID: id, Name: name, Plan: "founding"}}
		g.store.mu.Unlock()
		list := g.get("/operator/tenants", c).Body.String()
		if !strings.Contains(list, `<a href="/operator/tenants/`+id.String()+`" class="op-link"><span class="italic">Unnamed tenant</span></a>`) {
			t.Errorf("a tenant named %s: the list's row does not say Unnamed tenant", label)
		}
		// Its id as the row's visible text, not only inside the link's href (2nd round, B1).
		if !strings.Contains(list, `<span class="break-all text-xs text-ink/70">`+id.String()+`</span>`) {
			t.Errorf("a tenant named %s: the list's row does not print its id beside it", label)
		}
	}
	if _, err := operatorpages.UnnamedTenant(""); err == nil {
		t.Error("UnnamedTenant accepted an empty id")
	}
	id := g.seedOverview(db.TenantOverview{Name: "\u200bA\u2060", Plan: "founding"})
	if body := g.get("/operator/tenants/"+id.String(), c).Body.String(); strings.Contains(body, "Unnamed tenant") ||
		!strings.Contains(body, "<bdi>\u200bA\u2060</bdi>") {
		t.Error("CONTROL: a name with a visible letter is not the banner's name")
	}
}

// TestTenantScreens_EscapeWhatATenantAndAnOperatorTyped: a tenant's name that closes the
// markup around it and opens a script, on the list row and in the overview's banner, and
// a search term that closes the search box's attribute and opens a script, in the box,
// the "matching" line and the pager's hidden field -- none is written raw, each is there
// escaped (so the assertion is not passing on a value that never rendered); and a name
// carrying a right-to-left override sits inside a bdi element.
func TestTenantScreens_EscapeWhatATenantAndAnOperatorTyped(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const evilName = `</bdi></a><script>alert(1)</script>`
	const evilTerm = `"><script>alert(2)</script>`
	const rtl = "\u202eFAKE override"
	id := g.seedOverview(db.TenantOverview{Name: evilName, Plan: "founding"})
	g.store.mu.Lock()
	g.store.tenants = []db.TenantSummary{{ID: id, Name: evilName, Plan: "founding"}, {ID: uuid.New(), Name: rtl, Plan: "founding"}}
	for i := 0; i < operator.TenantPageSizeForTest; i++ {
		g.store.tenants = append(g.store.tenants, db.TenantSummary{ID: uuid.New(), Name: evilTerm + strconv.Itoa(i), Plan: "founding"})
	}
	g.store.mu.Unlock()
	for what, body := range map[string]string{
		"the list":     g.get("/operator/tenants", c).Body.String(),
		"the overview": g.get("/operator/tenants/"+id.String(), c).Body.String(),
		"a search":     g.post("/operator/tenants", searchForm(evilTerm, ""), c).Body.String(),
	} {
		if strings.Contains(body, "<script") {
			t.Errorf("%s writes a script element", what)
		}
		if what != "a search" && !strings.Contains(body, html.EscapeString(evilName)) {
			t.Errorf("%s does not carry the escaped name -- the assertion above may pass on a name that never rendered", what)
		}
		if what == "a search" {
			esc := html.EscapeString(evilTerm)
			for _, want := range []string{`name="q" value="` + esc + `"`, `<bdi class="font-mono">` + esc + `</bdi>`,
				`<input type="hidden" name="q" value="` + esc + `">`} {
				if !strings.Contains(body, want) {
					t.Errorf("a search does not carry the escaped term at %q", want)
				}
			}
		}
		if what == "the list" && !strings.Contains(body, "<bdi>"+rtl+"</bdi>") {
			t.Error("the list does not isolate a name with a direction override")
		}
	}
}

// TestTenantScreens_TheTextClearsAA recomputes, from tailwind.config.js's palette, the
// contrast of the tenant screens' text colours on their grounds (WCAG 2.1 relative
// luminance, sRGB; ink at 70% composited on its ground): ink and ink at 70% on paper (the
// cards and the docket: names, figures, labels, help lines), tappa-green on paper (the
// row and card links) and on porcelain (the pager and the overview's links, on the page
// ground), paper on tappa-green (the search button), ink and ink at 70% on saffron-lite
// (the tenant banner's name, id and label) -- each against AA's 4.5:1 (the figures are
// large, the rest is 14px and smaller, which is not large text).
//
// IT COMPUTES THE PALETTE; IT DOES NOT READ THE TEMPLATES. The template side is
// internal/handler's TestBrand_EveryInkToneClearsAA: it reads every translucent ink tone
// the templates write (text-ink/NN, tenants.templ's included) against the darkest light
// ground in use, so a tone below AA written here is red there, not in this test. Opaque
// tokens on coloured grounds are outside that test too (its own comment says so).
func TestTenantScreens_TheTextClearsAA(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatal(err)
	}
	hex := func(token string) [3]float64 {
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
	ink, paper, porcelain, green, saffronLite := hex("ink"), hex("paper"), hex("porcelain"), hex("tappa-green"), hex("saffron-lite")
	for _, c := range []struct {
		what   string
		fg, bg [3]float64
	}{
		{"ink on paper", ink, paper},
		{"ink at 70% on paper", over(ink, 0.7, paper), paper},
		{"tappa-green on paper", green, paper},
		{"tappa-green on porcelain", green, porcelain},
		{"paper on tappa-green", paper, green},
		{"ink on saffron-lite", ink, saffronLite},
		{"ink at 70% on saffron-lite", over(ink, 0.7, saffronLite), saffronLite},
	} {
		if got := contrast(c.fg, c.bg); got < 4.5 {
			t.Errorf("%s = %.2f:1, want at least 4.5:1", c.what, got)
		} else {
			t.Logf("%s = %.2f:1", c.what, got)
		}
	}
}
