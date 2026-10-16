package operator_test

// tenantapi_test.go -- M11 API-13a (K-22): the tenant overview's integration API section and its two
// writes, POST /operator/tenants/{id}/integration and POST /operator/tenants/{id}/integration/close, on the shipped
// router with the fake store (rig_test.go). The header table of their response classes (C200-C217);
// the code checked at the boundary by the database's own rule; the sentence read from the overview,
// not from the write; open -> close -> open; a peer's code; K-19's notice; no session and a
// cross-site POST never reach the store. Against PostgreSQL (the audit rows): tenantapi_db_test.go.

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// The header table's business -- fixed, so the 303s' Locations are designed constants
// (designedHeaders, op8r2_test.go).
const apiHeaderTenant = "61300000-0000-4000-8000-0000000000c1"

// The sentences, written out here rather than read from the handler, so a changed word is red.
const (
	headingAPIOpened     = "Opened."
	headingAPINotOpened  = "Not opened — nothing changed."
	headingAPICodeKept   = "Code not changed — the API stays open under its current code."
	headingAPIClosed     = "Closed."
	headingAPINotClosed  = "It is still open."
	headingAPICode       = "That code cannot be used"
	ruleAPICodeShape     = "The code is a capital letter followed by 1 to 15 capital letters or digits."
	ruleAPICodeInHolding = "No other business in this business's holding has the code, and none that left it less than 60 seconds ago."
)

var everyAPIHeading = []string{headingAPIOpened, headingAPINotOpened, headingAPICodeKept, headingAPIClosed, headingAPINotClosed, headingAPICode}

// apiHeadingsOn returns the API notice headings body carries.
func apiHeadingsOn(body string) []string {
	var out []string
	for _, h := range everyAPIHeading {
		if strings.Contains(body, ">"+h+"</p>") {
			out = append(out, h)
		}
	}
	return out
}

// workRecorder is a WorkNoticer that remembers what it was told.
type workRecorder struct {
	mu      sync.Mutex
	tenants []uuid.UUID
}

func (w *workRecorder) Notice(t uuid.UUID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tenants = append(w.tenants, t)
}

func (w *workRecorder) got() []uuid.UUID {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.tenants)
}

func (g *rig) apiWrites() []fakeAPIWrite {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return slices.Clone(g.store.apiWrites)
}

func (g *rig) overviewOf(id uuid.UUID) db.TenantOverview {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return g.store.overviews[id]
}

// openAPI and closeAPI are the two writes' requests, same-origin.
func openAPI(tenant, code string, c ...*http.Cookie) req {
	return req{method: http.MethodPost, path: "/operator/tenants/" + tenant + "/integration", form: url.Values{"api_code": {code}},
		origin: opOrigin, cookies: c}
}

func closeAPI(tenant string, c ...*http.Cookie) req {
	return req{method: http.MethodPost, path: "/operator/tenants/" + tenant + "/integration/close", form: url.Values{},
		origin: opOrigin, cookies: c}
}

// ------------------------------------------------------------ the header table --

// TestOperatorHeaders_TheTenantAPIClassesCarryThePolicy drives the eighteen response classes of the
// overview's integration API (C200-C217) with the 40-class test's hostile drive and checks
// (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these eighteen, at WriteHeader: the designed status, route and headers (no
// Location but the sign-in's and, on the two accepted writes, the overview with the closed word and
// -- open -- the code that passed the boundary; nothing of the hostile query); no hostile value in a
// header or the body; no script; and the counts: the cross-origin POSTs (C203, C213) and the 405s
// (C210, C217) make no store call; the malformed ids (C204, C214), the oversized form (C206) and the
// read budget (C207) make no read and no write; a refused code (C205) one read and no write; each
// accepted write (C201, C211) one write and no read.
//
// PART II -- the list above, on these eighteen classes.
//
// PART III -- This test measures the eighteen classes and the raw and query-escaped forms only; no
// completeness claim.
func TestOperatorHeaders_TheTenantAPIClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	tenant := uuid.MustParse(apiHeaderTenant)
	g.seedOverview(db.TenantOverview{ID: tenant, Name: "FAKE API Header Business Ltd", Plan: "founding"})
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
	view := func(path string, c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: path, cookies: c, header: sfs}
	}
	crossOrigin := func(r req) req {
		r.origin, r.header = "https://taptime.mt", map[string]string{"Sec-Fetch-Site": "same-site"}
		return r
	}
	once := func(r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return send(r()) }
	}
	failing := func(method string, err error, r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			rq := r()
			defer g.failStore(method, err)()
			return send(rq)
		}
	}
	calls := map[string]map[string]int{}
	counted := func(class string, drive func() *httptest.ResponseRecorder) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			reads, writes, total := g.store.count("TenantDetail"), len(g.apiWrites()), g.store.total()
			w := drive()
			calls[class] = map[string]int{"reads": g.store.count("TenantDetail") - reads, "writes": len(g.apiWrites()) - writes,
				"all": g.store.total() - total}
			return w
		}
	}
	spentReads := func() *http.Cookie {
		t.Helper()
		c := signIn()
		for i := 0; i < 60; i++ {
			if w := send(view("/operator/tenants/"+apiHeaderTenant, c)); w.Code != http.StatusOK {
				t.Fatalf("PREMISE: view %d = %d", i+1, w.Code)
			}
		}
		return c
	}
	malformed := strings.ReplaceAll(apiHeaderTenant, "-", "")
	big := strings.Repeat("9", 20<<10)
	classes := []headerClass{
		{"C200 overview, an open's result", 200, once(func() req {
			return view("/operator/tenants/"+apiHeaderTenant+"?done=api-opened&api_code=KF", signIn())
		})},
		{"C201 open", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C201", once(func() req { return openAPI(apiHeaderTenant, "KF", c) }))()
		}},
		{"C202 open without a cookie", 303, once(func() req { return openAPI(apiHeaderTenant, "KF") })},
		{"C203 open, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C203", once(func() req { return crossOrigin(openAPI(apiHeaderTenant, "KF", c)) }))()
		}},
		{"C204 open, a malformed id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C204", once(func() req { return openAPI(malformed, "KF", c) }))()
		}},
		{"C205 open, a code refused", 422, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C205", once(func() req { return openAPI(apiHeaderTenant, "kf", c) }))()
		}},
		{"C206 open, an oversized form", 413, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C206", once(func() req { return openAPI(apiHeaderTenant, big, c) }))()
		}},
		{"C207 open, a code refused past the read budget", 429, func() *httptest.ResponseRecorder {
			c := spentReads()
			return counted("C207", once(func() req { return openAPI(apiHeaderTenant, "", c) }))()
		}},
		{"C208 open, the write fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("EnableTenantAPI", errFakeDB, func() req { return openAPI(apiHeaderTenant, "KF", c) })()
		}},
		{"C209 open, the write's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("EnableTenantAPI", db.ErrOperatorRefused, func() req { return openAPI(apiHeaderTenant, "KF", c) })()
		}},
		{"C210 GET on the open path", 405, counted("C210", once(func() req {
			return view("/operator/tenants/" + apiHeaderTenant + "/integration")
		}))},
		{"C211 close", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C211", once(func() req { return closeAPI(apiHeaderTenant, c) }))()
		}},
		{"C212 close without a cookie", 303, once(func() req { return closeAPI(apiHeaderTenant) })},
		{"C213 close, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C213", once(func() req { return crossOrigin(closeAPI(apiHeaderTenant, c)) }))()
		}},
		{"C214 close, a malformed id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C214", once(func() req { return closeAPI(malformed, c) }))()
		}},
		{"C215 close, the write fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("DisableTenantAPI", errFakeDB, func() req { return closeAPI(apiHeaderTenant, c) })()
		}},
		{"C216 close, the write's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("DisableTenantAPI", db.ErrOperatorRefused, func() req { return closeAPI(apiHeaderTenant, c) })()
		}},
		{"C217 GET on the close path", 405, counted("C217", once(func() req {
			return view("/operator/tenants/" + apiHeaderTenant + "/integration/close")
		}))},
	}
	if len(classes) != 18 {
		t.Fatalf("PREMISE: %d classes, the comment says 18", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(200+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 200+i, c.name, 200+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	for class, want := range map[string]map[string]int{
		"C201": {"reads": 0, "writes": 1}, "C203": {"all": 0}, "C204": {"reads": 0, "writes": 0},
		"C205": {"reads": 1, "writes": 0}, "C206": {"reads": 0, "writes": 0}, "C207": {"reads": 0, "writes": 0},
		"C210": {"all": 0}, "C211": {"reads": 0, "writes": 1}, "C213": {"all": 0}, "C214": {"reads": 0, "writes": 0},
		"C217": {"all": 0},
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
	for _, w := range g.apiWrites() {
		if w.tenant != tenant || (w.method == "EnableTenantAPI" && w.code != "KF") {
			t.Errorf("a %s carried %s / %q; want the path's business and the form's code", w.method, w.tenant, w.code)
		}
	}
}

// ------------------------------------------------------------ the boundary --

// TestOperatorAPI_TheCodeRuleIsTheDatabases: the boundary's rule is op_enable_tenant_api's own -- the
// regular expression db.APICodePattern is the literal the latest definition of the function (00042)
// and 00038's CHECK carry -- and, through the handler, a code it refuses is 422 with the code's
// sentence, one read and NO write; one it takes is written as posted (its ends trimmed, never case
// folded).
//
// MUTANT (measured): apiCode without its rule (any non-empty text) -> "kf", "K", "1KF" and a
// seventeen-character code reach the store, red.
func TestOperatorAPI_TheCodeRuleIsTheDatabases(t *testing.T) {
	for file, needle := range map[string]string{
		"00038_add_api_access_to_tenants.sql":            "api_code ~ '" + db.APICodePattern + "'",
		"00042_put_holdings_on_the_operator_surface.sql": "p_api_code !~ '" + db.APICodePattern + "'",
		"00039_open_the_api_from_the_operator.sql":       "p_api_code !~ '" + db.APICodePattern + "'",
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), needle) {
			t.Errorf("%s does not carry the rule %q: the boundary's rule is not the database's", file, needle)
		}
	}
	g := newRig(t)
	sess := g.signIn(g.active())
	tenant := g.seedOverview(db.TenantOverview{Name: "FAKE Code Rule Ltd", Plan: "founding"})
	refused := []string{"", "  ", "kf", "K", "1KF", "KF-1", "K F", "ÇF", strings.Repeat("K", 17), "KF\x00", "K\u200bF"}
	for _, code := range refused {
		writes, reads := len(g.apiWrites()), g.store.count("TenantDetail")
		w := g.post("/operator/tenants/"+tenant.String()+"/integration", url.Values{"api_code": {code}}, sess)
		if w.Code != http.StatusUnprocessableEntity || !slices.Equal(apiHeadingsOn(w.Body.String()), []string{headingAPICode}) {
			t.Errorf("code %q: %d, headings %v; want 422 and [%s]", code, w.Code, apiHeadingsOn(w.Body.String()), headingAPICode)
		}
		if len(g.apiWrites()) != writes || g.store.count("TenantDetail") != reads+1 {
			t.Errorf("code %q: %d write(s), %d read(s); want none and one", code, len(g.apiWrites())-writes, g.store.count("TenantDetail")-reads)
		}
	}
	rule := regexp.MustCompile(db.APICodePattern)
	for _, code := range []string{"KF", "KM", " KF2 ", "K1", "A" + strings.Repeat("Z9", 7) + "Z"} {
		w := g.post("/operator/tenants/"+tenant.String()+"/integration", url.Values{"api_code": {code}}, sess)
		got := g.apiWrites()
		if w.Code != http.StatusSeeOther || len(got) == 0 || got[len(got)-1].code != strings.TrimSpace(code) || !rule.MatchString(got[len(got)-1].code) {
			t.Errorf("code %q: %d, last write %+v; want 303 and the trimmed code written", code, w.Code, got)
		}
	}
}

// ------------------------------------------------------------ the result --

// TestOperatorAPI_TheResultIsReadNotPosted: every write is a 303 to the overview with what was ASKED,
// and the overview's own read says what HAPPENED, word for word -- through open -> close -> open:
//
//  1. a business never opened: the section says Closed and No API code, offers "Open the API" and no
//     Close button;
//  2. open under KF: 303 to the overview with done=api-opened&api_code=KF; "Opened." with the state
//     Open, the instant and KF; one K-19 notice for the business;
//  3. change the code to KM while open: "Opened."; the first opening instant is kept;
//  4. close: "Closed."; the state Closed, the code KM kept, no Close button; no notice;
//  5. open again under KM: "Opened.";
//  6. a peer in its holding carries KX and it asks for KX while OPEN under KM: the write is ACCEPTED
//     (303, recorded) and the overview says "Code not changed — the API stays open under its current
//     code." with the rules -- not "Not opened", which would say the API is closed; the code is still
//     KM;
//  7. a stale or forged result (done=api-opened&api_code=QQ while open under KM) says "Code not
//     changed"; done=api-closed while open says "It is still open."; an unknown word or a code outside
//     the rule says nothing -- and the URL's code is never printed;
//  8. closed again, it asks for KX: "Not opened — nothing changed." with the rules (the read shows it
//     closed); a forged done=api-opened&api_code=QQ while closed says the same.
//
// MUTANT (measured): apiResultNotice answering apiNoticeOpened for every open -> step 6 and 7 say
// "Opened.", red; answering apiNoticeNotOpened for every refused ask (API-13a fix) -> step 6 and 7 say
// "Not opened", red; answering apiNoticeCodeKept for every refused ask -> step 8 says "Code not
// changed", red.
func TestOperatorAPI_TheResultIsReadNotPosted(t *testing.T) {
	g := newRig(t)
	work := &workRecorder{}
	g.surface.NoticeWorkTo(work)
	sess := g.signIn(g.active())
	tenant := g.seedOverview(db.TenantOverview{Name: "FAKE Result Business Ltd", Plan: "founding"})
	peer := g.seedOverview(db.TenantOverview{Name: "FAKE Result Peer Ltd", Plan: "founding"})
	overview := "/operator/tenants/" + tenant.String()
	follow := func(what string, r req, wantLoc string) string {
		t.Helper()
		r.host = opHost
		w := g.do(r)
		if loc := w.Result().Header.Get("Location"); w.Code != http.StatusSeeOther || loc != wantLoc {
			t.Fatalf("%s = %d to %q, want 303 to %q", what, w.Code, loc, wantLoc)
		}
		v := g.get(wantLoc, sess)
		if v.Code != http.StatusOK {
			t.Fatalf("the overview after %s = %d", what, v.Code)
		}
		return v.Body.String()
	}
	says := func(what, body string, want ...string) {
		t.Helper()
		if hs := apiHeadingsOn(body); !slices.Equal(hs, want) {
			t.Errorf("after %s the overview says %v, want %v", what, hs, want)
		}
	}
	closeForm := `action="` + overview + `/integration/close"`

	// 1.
	body := g.get(overview, sess).Body.String()
	says("a first view", body)
	if !strings.Contains(body, `<span class="tally tally--deactivated">Closed</span>`) || !strings.Contains(body, "No API code") ||
		!strings.Contains(body, "Open the API") || strings.Contains(body, closeForm) {
		t.Error("a never-opened business: the section does not say Closed / No API code with the open form alone")
	}
	// 2.
	body = follow("opening under KF", openAPI(tenant.String(), "KF", sess), overview+"?done=api-opened&api_code=KF")
	says("opening under KF", body, headingAPIOpened)
	opened := g.overviewOf(tenant).APIEnabledAt
	if !strings.Contains(body, `<span class="tally tally--active">Open</span>`) || !strings.Contains(body, `API code <span class="font-mono">KF</span>`) ||
		!strings.Contains(body, "Change the code") || !strings.Contains(body, closeForm) || opened == nil {
		t.Error("after opening: the section does not show Open, KF, the change form and the Close button")
	}
	if got := work.got(); !slices.Equal(got, []uuid.UUID{tenant}) {
		t.Errorf("after opening, K-19 was told %v, want the business once", got)
	}
	// 3.
	body = follow("changing the code", openAPI(tenant.String(), "KM", sess), overview+"?done=api-opened&api_code=KM")
	says("changing the code", body, headingAPIOpened)
	if o := g.overviewOf(tenant); o.APIEnabledAt == nil || !o.APIEnabledAt.Equal(*opened) || *o.APICode != "KM" {
		t.Errorf("after the code change: %+v, want the first instant kept and KM", o)
	}
	// 4.
	body = follow("closing", closeAPI(tenant.String(), sess), overview+"?done=api-closed")
	says("closing", body, headingAPIClosed)
	if !strings.Contains(body, `<span class="tally tally--deactivated">Closed</span>`) || !strings.Contains(body, `API code <span class="font-mono">KM</span>`) ||
		strings.Contains(body, closeForm) {
		t.Error("after closing: the section does not show Closed with KM kept and no Close button")
	}
	if n := len(work.got()); n != 2 {
		t.Errorf("K-19 was told %d time(s) after the close, want the two opens only", n)
	}
	// 5.
	body = follow("opening again", openAPI(tenant.String(), "KM", sess), overview+"?done=api-opened&api_code=KM")
	says("opening again", body, headingAPIOpened)
	// 6.
	g.store.mu.Lock()
	px := "KX"
	po := g.store.overviews[peer]
	po.APICode = &px
	g.store.overviews[peer] = po
	g.store.apiPeers = map[uuid.UUID][]uuid.UUID{tenant: {peer}}
	g.store.mu.Unlock()
	writes := len(g.apiWrites())
	body = follow("asking for a peer's code", openAPI(tenant.String(), "KX", sess), overview+"?done=api-opened&api_code=KX")
	says("asking for a peer's code", body, headingAPICodeKept)
	if !strings.Contains(body, "<li>"+ruleAPICodeShape+"</li>") || !strings.Contains(body, "<li>"+html.EscapeString(ruleAPICodeInHolding)+"</li>") ||
		len(g.apiWrites()) != writes+1 || *g.overviewOf(tenant).APICode != "KM" {
		t.Error("a peer's code: the overview does not list the rules, the ask was not recorded, or the code changed")
	}
	// 7.
	for q, want := range map[string][]string{
		"?done=api-opened&api_code=QQ": {headingAPICodeKept}, "?done=api-closed": {headingAPINotClosed},
		"?done=api-gone&api_code=KM": nil, "?done=api-opened&api_code=qq": nil, "?done=api-opened": nil,
		"?done=api-opened&api_code=+KM": nil,
	} {
		body := g.get(overview+q, sess).Body.String()
		says(q, body, want...)
		if strings.Contains(body, "QQ") || strings.Contains(body, "qq") {
			t.Errorf("%s: the URL's code is printed", q)
		}
	}
	// 8.
	follow("closing again", closeAPI(tenant.String(), sess), overview+"?done=api-closed")
	body = follow("asking for a peer's code while closed", openAPI(tenant.String(), "KX", sess), overview+"?done=api-opened&api_code=KX")
	says("asking for a peer's code while closed", body, headingAPINotOpened)
	if !strings.Contains(body, "<li>"+ruleAPICodeShape+"</li>") || g.overviewOf(tenant).APIEnabledAt != nil {
		t.Error("a peer's code while closed: the overview does not list the rules, or the API opened")
	}
	says("a forged result while closed", g.get(overview+"?done=api-opened&api_code=QQ", sess).Body.String(), headingAPINotOpened)
}

// ------------------------------------------------------------ the gates --

// TestOperatorAPI_NoSessionAndCrossSiteNeverReachTheStore: without an operator session the two writes
// are the sign-in's 303 (a dead cookie too) and the store is not called; a cross-site POST WITH a live
// session is 403 before any store call (the console group's same-origin gate -- the K-37 rule on the
// operator's host); a GET labelled same-site on the overview is the sign-in redirect. Nothing is
// written and K-19 is told nothing.
//
// MUTANT (measured): the two routes mounted outside the console group (no requireOperator /
// sameOriginGate) -> the cross-site POST is written, red.
func TestOperatorAPI_NoSessionAndCrossSiteNeverReachTheStore(t *testing.T) {
	g := newRig(t)
	work := &workRecorder{}
	g.surface.NoticeWorkTo(work)
	tenant := g.seedOverview(db.TenantOverview{Name: "FAKE Gate Business Ltd", Plan: "founding"})
	dead := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}
	for name, r := range map[string]req{
		"open, no cookie":    openAPI(tenant.String(), "KF"),
		"open, dead cookie":  openAPI(tenant.String(), "KF", dead),
		"close, no cookie":   closeAPI(tenant.String()),
		"close, dead cookie": closeAPI(tenant.String(), dead),
	} {
		r.host = opHost
		w := g.do(r)
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" {
			t.Errorf("%s: %d to %q, want 303 to the sign-in", name, w.Code, w.Result().Header.Get("Location"))
		}
		if n := g.store.count("EnableTenantAPI") + g.store.count("DisableTenantAPI") + g.store.count("TenantDetail"); n != 0 {
			t.Errorf("%s: %d call(s) of the overview's store methods, want none", name, n)
		}
	}
	sess := g.signIn(g.active())
	for name, r := range map[string]req{
		"open":  openAPI(tenant.String(), "KF", sess),
		"close": closeAPI(tenant.String(), sess),
	} {
		for _, site := range []string{"cross-site", "same-site"} {
			r.host, r.origin, r.header = opHost, "https://evil.example", map[string]string{"Sec-Fetch-Site": site}
			total := g.store.total()
			if w := g.do(r); w.Code != http.StatusForbidden {
				t.Errorf("%s, %s: %d, want 403", name, site, w.Code)
			}
			if n := g.store.total() - total; n != 0 {
				t.Errorf("%s, %s: %d store call(s), want none", name, site, n)
			}
		}
	}
	if w := g.do(req{method: http.MethodGet, host: opHost, path: "/operator/tenants/" + tenant.String(), cookies: []*http.Cookie{sess},
		header: map[string]string{"Sec-Fetch-Site": "same-site"}}); w.Code != http.StatusSeeOther {
		t.Errorf("a same-site GET of the overview: %d, want the sign-in redirect", w.Code)
	}
	if len(g.apiWrites()) != 0 || len(work.got()) != 0 {
		t.Errorf("%d write(s), %d notice(s); want none", len(g.apiWrites()), len(work.got()))
	}
}

// TestOperatorAPI_ARefusedOrFailedOpenNoticesNothing: K-19 is told about an ACCEPTED open only -- a code
// the boundary refuses, a write that fails and a session refused at the write tell it nothing; a
// surface never handed a noticer (the rig's default) still answers an open 303.
func TestOperatorAPI_ARefusedOrFailedOpenNoticesNothing(t *testing.T) {
	g := newRig(t)
	sess := g.signIn(g.active())
	tenant := g.seedOverview(db.TenantOverview{Name: "FAKE Notice Business Ltd", Plan: "founding"})
	if w := g.post("/operator/tenants/"+tenant.String()+"/integration", url.Values{"api_code": {"KF"}}, sess); w.Code != http.StatusSeeOther {
		t.Fatalf("CONTROL: an open with no noticer = %d, want 303", w.Code)
	}
	work := &workRecorder{}
	g.surface.NoticeWorkTo(work)
	if w := g.post("/operator/tenants/"+tenant.String()+"/integration", url.Values{"api_code": {"kf"}}, sess); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a refused code = %d", w.Code)
	}
	func() {
		defer g.failStore("EnableTenantAPI", errFakeDB)()
		if w := g.post("/operator/tenants/"+tenant.String()+"/integration", url.Values{"api_code": {"KF"}}, sess); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("a failed write = %d", w.Code)
		}
	}()
	func() {
		defer g.failStore("EnableTenantAPI", db.ErrOperatorRefused)()
		if w := g.post("/operator/tenants/"+tenant.String()+"/integration", url.Values{"api_code": {"KF"}}, sess); w.Code != http.StatusSeeOther {
			t.Fatalf("a refused session = %d", w.Code)
		}
	}()
	if got := work.got(); len(got) != 0 {
		t.Fatalf("K-19 was told %v, want nothing", got)
	}
	if w := g.post("/operator/tenants/"+tenant.String()+"/integration", url.Values{"api_code": {"KF"}}, sess); w.Code != http.StatusSeeOther ||
		!slices.Equal(work.got(), []uuid.UUID{tenant}) {
		t.Fatalf("CONTROL: an accepted open = %d and told %v, want 303 and the business", w.Code, work.got())
	}
}
