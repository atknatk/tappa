package operator_test

// holdings_test.go -- M11 API-1a: /operator/holdings and its two membership routes on the shipped
// router with the fake store (rig_test.go). The header table of their response classes
// (C166-C199); the name and the ids checked at the boundary; the sentence read from the list, not
// from the write, on every branch and across a page's edges; the pager; the writes' failures and
// their log lines; a write that outlives its client, and the two bounds; the read budget; the
// names escaped. Against PostgreSQL: holdings_db_test.go.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// ------------------------------------------------------------------ fixtures --

// The header table's ids: the holding the create class makes, and the holding and business the
// membership classes name -- fixed, so the 303s' Locations are designed constants
// (designedHeaders, op8r2_test.go).
const (
	holdingHeaderCreated = "61000000-0000-4000-8000-0000000000a1"
	holdingHeaderHolding = "61000000-0000-4000-8000-0000000000a2"
	holdingHeaderTenant  = "61000000-0000-4000-8000-0000000000a3"
)

// errFakeHoldingRefused is the fake's answer for a write the database refuses with 22023 (a
// constraint it caught: nothing written), in internal/db's spelling of a database error.
var errFakeHoldingRefused = errors.New("fake: db: add holding member: database error (SQLSTATE 22023)")

// The screen's sentences, written out here rather than read from the handler, so a changed word
// is red.
const (
	headingCreated     = "Created."
	headingAdded       = "Added."
	headingNotAdded    = "Not added — nothing changed."
	headingRemoved     = "Removed."
	headingNotRemoved  = "Nothing was removed — it was not a live member."
	headingStillLive   = "It is a live member of this holding."
	headingElsewhere   = "The result is not on this page."
	headingNameRefused = "That name cannot be used"
	headingIDRefused   = "That is not a business id"
	sentenceRemoved    = "The business can join a holding again after 60 seconds."
	ruleTimezone       = "Its timezone is the timezone of every member of the holding."
	ruleOneHolding     = "It is in no other holding, and at least 60 seconds have passed since it last left one."
	ruleAPICode        = "Its API code is not one a member of the holding already has."
	ruleExists         = "The holding and the business both exist."
)

// everyHeading is the nine headings a page can carry above the list.
var everyHeading = []string{headingCreated, headingAdded, headingNotAdded, headingRemoved, headingNotRemoved, headingStillLive,
	headingElsewhere, headingNameRefused, headingIDRefused}

// headingsOn is the headings of everyHeading a body carries.
func headingsOn(body string) []string {
	var out []string
	for _, h := range everyHeading {
		if strings.Contains(body, ">"+h+"</p>") {
			out = append(out, h)
		}
	}
	return out
}

// seedHolding puts a holding into the fake, after the ones already there (the fake's order is the
// list's: newest first, so a test seeds the newest first).
func (g *rig) seedHolding(id uuid.UUID, name string, created time.Time, members ...fakeMembership) uuid.UUID {
	g.t.Helper()
	if id == uuid.Nil {
		id = uuid.New()
	}
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	g.store.holdings = append(g.store.holdings, fakeHolding{id: id, name: name, createdAt: created, members: members})
	return id
}

// seedHoldingTenant makes a business the fake's memberships can name.
func (g *rig) seedHoldingTenant(id uuid.UUID, name, timezone string, apiCode *string) uuid.UUID {
	g.t.Helper()
	if id == uuid.Nil {
		id = uuid.New()
	}
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	g.store.holdingTenants[id] = fakeHoldingTenant{name: name, timezone: timezone, apiCode: apiCode}
	return id
}

func (g *rig) holdingWrites() []fakeHoldingWrite {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return append([]fakeHoldingWrite(nil), g.store.holdingWrites...)
}

func (g *rig) holdingReads() []fakeHoldingRead {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return append([]fakeHoldingRead(nil), g.store.holdingReads...)
}

// failStore makes method answer err until the returned function is called.
func (g *rig) failStore(method string, err error) func() {
	g.store.mu.Lock()
	g.store.fail[method] = err
	g.store.mu.Unlock()
	return func() {
		g.store.mu.Lock()
		delete(g.store.fail, method)
		g.store.mu.Unlock()
	}
}

// liveMembership is a live membership of tenant, joined at at.
func liveMembership(tenant uuid.UUID, at time.Time) fakeMembership {
	return fakeMembership{tenant: tenant, joined: at}
}

// endedMembership is a membership of tenant, joined at at and ended a minute later.
func endedMembership(tenant uuid.UUID, at time.Time) fakeMembership {
	left := at.Add(time.Minute)
	return fakeMembership{tenant: tenant, joined: at, left: &left}
}

func membersPathOf(holding string) string { return "/operator/holdings/" + holding + "/members" }

func removePathOf(holding, tenant string) string {
	return membersPathOf(holding) + "/" + tenant + "/remove"
}

// resultPathOf is the list with a write's query, as the 303 writes it.
func resultPathOf(done, holding, tenant string) string {
	p := "/operator/holdings?done=" + done + "&holding=" + holding
	if tenant != "" {
		p += "&tenant=" + tenant
	}
	return p
}

// ------------------------------------------------------------ the header table --

// TestOperatorHeaders_TheHoldingClassesCarryThePolicy drives the thirty-four response classes of
// the holdings screen (C166-C199) with the 40-class test's hostile drive and checks
// (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these thirty-four, at WriteHeader (the recorder's Result().Header): the
// designed status, route, header names and values (designedHeaders: no Location but the
// sign-in's and, on the three accepted writes, the list with the closed word and the ids the
// handler parsed or the database returned); no hostile value (hostileValues) in a header value or
// in the body, raw or query-escaped -- every one's last request carries the hostile query
// (hostileDrive; the list's GET without its page, which that GET reads by design); no script; and
// the counts below: the cross-origin POSTs (C177, C186, C195), the 405s (C183, C192, C199) and the
// same-site read (C169) make no store call; the malformed page and ids (C170, C187, C196), the
// oversized forms (C178, C189) and the read budget (C173, C180) make no read and no write; a
// refused name or business id (C179, C188) makes one read and no write; each accepted write
// (C175, C184, C193) one write and no read.
//
// PART II -- the list above, on these thirty-four classes.
//
// PART III -- This test measures the thirty-four classes and the raw and query-escaped forms
// only; anything else (examples: a class a later handler adds, a hostile value echoed
// HTML-escaped or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_TheHoldingClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	holding, tenant := uuid.MustParse(holdingHeaderHolding), uuid.MustParse(holdingHeaderTenant)
	g.seedHoldingTenant(tenant, "FAKE Header Business Ltd", "Europe/Malta", nil)
	g.seedHolding(holding, "FAKE Header Group", fakeHoldingClock.Add(-time.Hour))
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
	list := func(path string, c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: path, cookies: c, header: sfs}
	}
	create := func(name string, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: "/operator/holdings", form: url.Values{"name": {name}}, origin: opOrigin, cookies: c}
	}
	add := func(h, tenantID string, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: membersPathOf(h), form: url.Values{"tenant_id": {tenantID}}, origin: opOrigin, cookies: c}
	}
	remove := func(h, tn string, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: removePathOf(h, tn), form: url.Values{}, origin: opOrigin, cookies: c}
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
			reads, writes, total, touches := g.store.count("Holdings"), len(g.holdingWrites()), g.store.total(), g.store.count("TouchOperatorSession")
			w := drive()
			calls[class] = map[string]int{"Holdings": g.store.count("Holdings") - reads, "writes": len(g.holdingWrites()) - writes,
				"all": g.store.total() - total, "touches": g.store.count("TouchOperatorSession") - touches}
			return w
		}
	}
	// spentReads is a session with its 60 reads spent on views of the list (readLimit).
	spentReads := func() *http.Cookie {
		t.Helper()
		c := signIn()
		for i := 0; i < 60; i++ {
			if w := send(list("/operator/holdings", c)); w.Code != http.StatusOK {
				t.Fatalf("PREMISE: view %d = %d", i+1, w.Code)
			}
		}
		return c
	}
	malformed := strings.ReplaceAll(holdingHeaderHolding, "-", "")
	dead := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}
	big := strings.Repeat("9", 20<<10)
	classes := []headerClass{
		{"C166 holdings", 200, once(func() req { return list("/operator/holdings", signIn()) })},
		{"C167 holdings without a cookie", 303, once(func() req { return list("/operator/holdings") })},
		{"C168 holdings with a dead cookie", 303, once(func() req { return list("/operator/holdings", dead) })},
		{"C169 holdings, a same-site read", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C169", once(func() req {
				r := list("/operator/holdings", c)
				r.header = map[string]string{"Sec-Fetch-Site": "same-site"}
				return r
			}))()
		}},
		{"C170 holdings, a page that is not a number", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C170", once(func() req { return list("/operator/holdings?page=x", c) }))()
		}},
		{"C171 holdings, the read fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C171", failing("Holdings", errFakeDB, func() req { return list("/operator/holdings", c) }))()
		}},
		{"C172 holdings, the read's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("Holdings", db.ErrOperatorRefused, func() req { return list("/operator/holdings", c) })()
		}},
		{"C173 holdings, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spentReads()
			return counted("C173", once(func() req { return list("/operator/holdings", c) }))()
		}},
		{"C174 holdings, a write's result", 200, once(func() req {
			return list(resultPathOf("add", holdingHeaderHolding, holdingHeaderTenant), signIn())
		})},
		{"C175 create", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			g.store.mu.Lock()
			g.store.nextHolding = uuid.MustParse(holdingHeaderCreated)
			g.store.mu.Unlock()
			return counted("C175", once(func() req { return create("FAKE Header Created Group", c) }))()
		}},
		{"C176 create without a cookie", 303, once(func() req { return create("FAKE Header No Cookie") })},
		{"C177 create, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C177", once(func() req { return crossOrigin(create("FAKE Header Cross", c)) }))()
		}},
		{"C178 create, an oversized form", 413, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C178", once(func() req { return create(big, c) }))()
		}},
		{"C179 create, a name refused", 422, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C179", once(func() req { return create("   ", c) }))()
		}},
		{"C180 create, a name refused past the read budget", 429, func() *httptest.ResponseRecorder {
			c := spentReads()
			return counted("C180", once(func() req { return create("", c) }))()
		}},
		{"C181 create, the write fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C181", failing("CreateHolding", errFakeDB, func() req { return create("FAKE Header Fails", c) }))()
		}},
		{"C182 create, the write's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("CreateHolding", db.ErrOperatorRefused, func() req { return create("FAKE Header Refused", c) })()
		}},
		{"C183 PUT on holdings", 405, counted("C183", once(func() req {
			return req{method: http.MethodPut, path: "/operator/holdings", origin: opOrigin, form: url.Values{"name": {"FAKE"}}}
		}))},
		{"C184 add", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C184", once(func() req { return add(holdingHeaderHolding, holdingHeaderTenant, c) }))()
		}},
		{"C185 add without a cookie", 303, once(func() req { return add(holdingHeaderHolding, holdingHeaderTenant) })},
		{"C186 add, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C186", once(func() req { return crossOrigin(add(holdingHeaderHolding, holdingHeaderTenant, c)) }))()
		}},
		{"C187 add, a malformed holding id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C187", once(func() req { return add(malformed, holdingHeaderTenant, c) }))()
		}},
		{"C188 add, a malformed business id", 422, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C188", once(func() req { return add(holdingHeaderHolding, "not-a-business", c) }))()
		}},
		{"C189 add, an oversized form", 413, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C189", once(func() req { return add(holdingHeaderHolding, big, c) }))()
		}},
		{"C190 add, the write fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("AddHoldingMember", errFakeHoldingRefused, func() req { return add(holdingHeaderHolding, holdingHeaderTenant, c) })()
		}},
		{"C191 add, the write's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("AddHoldingMember", db.ErrOperatorRefused, func() req { return add(holdingHeaderHolding, holdingHeaderTenant, c) })()
		}},
		{"C192 GET on a holding's members", 405, counted("C192", once(func() req {
			return list(membersPathOf(holdingHeaderHolding))
		}))},
		{"C193 remove", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C193", once(func() req { return remove(holdingHeaderHolding, holdingHeaderTenant, c) }))()
		}},
		{"C194 remove without a cookie", 303, once(func() req { return remove(holdingHeaderHolding, holdingHeaderTenant) })},
		{"C195 remove, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C195", once(func() req { return crossOrigin(remove(holdingHeaderHolding, holdingHeaderTenant, c)) }))()
		}},
		{"C196 remove, a malformed business id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C196", once(func() req { return remove(holdingHeaderHolding, malformed, c) }))()
		}},
		{"C197 remove, the write fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("RemoveHoldingMember", errFakeDB, func() req { return remove(holdingHeaderHolding, holdingHeaderTenant, c) })()
		}},
		{"C198 remove, the write's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return failing("RemoveHoldingMember", db.ErrOperatorRefused, func() req { return remove(holdingHeaderHolding, holdingHeaderTenant, c) })()
		}},
		{"C199 GET on a removal", 405, counted("C199", once(func() req {
			return list(removePathOf(holdingHeaderHolding, holdingHeaderTenant))
		}))},
	}
	if len(classes) != 34 {
		t.Fatalf("PREMISE: %d classes, the comment says 34", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(166+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 166+i, c.name, 166+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	for class, want := range map[string]map[string]int{
		"C169": {"all": 0}, "C170": {"Holdings": 0, "writes": 0, "touches": 1}, "C171": {"writes": 0},
		"C173": {"Holdings": 0, "writes": 0}, "C175": {"Holdings": 0, "writes": 1}, "C177": {"all": 0},
		"C178": {"Holdings": 0, "writes": 0}, "C179": {"Holdings": 1, "writes": 0}, "C180": {"Holdings": 0, "writes": 0},
		"C181": {"Holdings": 0, "writes": 0}, "C183": {"all": 0}, "C184": {"Holdings": 0, "writes": 1}, "C186": {"all": 0},
		"C187": {"Holdings": 0, "writes": 0}, "C188": {"Holdings": 1, "writes": 0}, "C189": {"Holdings": 0, "writes": 0},
		"C192": {"all": 0}, "C193": {"Holdings": 0, "writes": 1}, "C195": {"all": 0}, "C196": {"Holdings": 0, "writes": 0},
		"C199": {"all": 0},
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
	for _, w := range g.holdingWrites() {
		if w.method != "CreateHolding" && (w.holding != holding || w.tenant != tenant) {
			t.Errorf("a %s carried %s / %s; want the path's holding and the form's or path's business", w.method, w.holding, w.tenant)
		}
	}
}

// ------------------------------------------------------------ the boundary --

// TestHoldings_TheNameIsCheckedAtTheBoundary: holdings.go's holdingName, through the handler.
//
// PART I -- each refused name -- empty, white space alone, 121 characters, a NUL, a tab, a line
// break, NEL (U+0085, a C1 control), a line or a paragraph separator, zero-width and filler
// characters alone, invalid UTF-8 -- is 422 with the list and the name's sentence, ONE read and no
// CreateHolding call, and the page does not carry the posted name (a marker inside it is searched).
// Accepted names are sent trimmed at their ends and as typed in between: "  FAKE Kebab Group  "
// as "FAKE Kebab Group", 120 characters, Maltese letters. An oversized body is 413 and an
// unreadable one 400, both with no store call. CONTROL: the accepted names are 303s and reach the
// store.
//
// PART II -- the list above. PART III -- these names only (no completeness claim).
func TestHoldings_TheNameIsCheckedAtTheBoundary(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	for _, name := range []string{"", "   ", "\t\n", strings.Repeat("ż", 121), "FAKEmark\x00b", "FAKEmark\tb", "FAKEmark\nb",
		"FAKEmark\u0085b", "FAKEmark\u2028b", "FAKEmark\u2029b", "\u200b\u2060\ufeff", "\u3164\u115f", "FAKEmark\xffb"} {
		reads, creates := g.store.count("Holdings"), g.store.count("CreateHolding")
		w := g.post("/operator/holdings", url.Values{"name": {name}}, c)
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), headingNameRefused) {
			t.Errorf("the name %q = %d, want 422 with the name's sentence", name, w.Code)
		}
		if g.store.count("Holdings") != reads+1 || g.store.count("CreateHolding") != creates {
			t.Errorf("the name %q: %d read(s), %d create(s); want 1 and 0", name, g.store.count("Holdings")-reads,
				g.store.count("CreateHolding")-creates)
		}
		if strings.Contains(w.Body.String(), "FAKEmark") {
			t.Errorf("the name %q was echoed onto the page", name)
		}
	}
	for posted, sent := range map[string]string{
		"  FAKE Kebab Group  ":     "FAKE Kebab Group",
		strings.Repeat("ż", 120):   strings.Repeat("ż", 120),
		"FAKE Ħal Qormi — Ġ & <ż>": "FAKE Ħal Qormi — Ġ & <ż>",
	} {
		n := len(g.holdingWrites())
		if w := g.post("/operator/holdings", url.Values{"name": {posted}}, c); w.Code != http.StatusSeeOther {
			t.Fatalf("CONTROL: the name %q = %d, want 303", posted, w.Code)
		}
		ws := g.holdingWrites()
		if len(ws) != n+1 || ws[n].name != sent {
			t.Errorf("the name %q reached the store as %q, want %q", posted, ws[len(ws)-1].name, sent)
		}
	}
	for raw, want := range map[string]bool{"FAKE": true, " FAKE ": true, "": false, "a\u00a0": true, "\u00a0": false,
		strings.Repeat("a", 120): true, strings.Repeat("a", 121): false, "a\u007fb": false} {
		if _, ok := operator.HoldingNameForTest(raw); ok != want {
			t.Errorf("holdingName(%q) accepted = %v, want %v", raw, ok, want)
		}
	}
	total := g.store.total()
	if w := g.post("/operator/holdings", url.Values{"name": {strings.Repeat("x", 17<<10)}}, c); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("an oversized create = %d, want 413", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/holdings", strings.NewReader("name=%zz"))
	r.Host = opHost
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", opOrigin)
	r.AddCookie(c)
	r.RemoteAddr = "192.0.2.11:1"
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "That form could not be read") {
		t.Errorf("an unreadable create = %d, want 400", w.Code)
	}
	if n := g.store.total() - total; n != 2 { // the two session predicates, nothing else
		t.Errorf("the oversized and the unreadable create made %d store call(s), want 2 (the gate's)", n)
	}
}

// TestHoldings_IDsAreCheckedAtTheBoundary: the holding id in the add's path, the business id in
// its form, and both ids in the removal's path.
//
// PART I -- a holding id in the add's path that is not the 36-character hyphenated form (32 hex
// digits, braces, urn:uuid:, a word) is 404 with no read and no write; a business id in the add's
// form that is not that form (empty, a word, 32 hex digits, braces, urn:uuid:, one character too
// many) is 422 with the list and the id's sentence, one read and no write; the form's id is
// trimmed at its ends and taken in either case (the Location writes it in lower case); either id
// of the removal's path not in the form is 404 with no store call past the gate. CONTROL: a
// well-formed add and removal are 303s to the list with what they asked.
//
// PART II -- the list above. PART III -- these ids only (no completeness claim).
func TestHoldings_IDsAreCheckedAtTheBoundary(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	holding, tenant := uuid.New(), uuid.New()
	for _, bad := range []string{strings.ReplaceAll(holding.String(), "-", ""), "{" + holding.String() + "}",
		"urn:uuid:" + holding.String(), "FAKE"} {
		touches, total := g.store.count("TouchOperatorSession"), g.store.total()
		if w := g.post(membersPathOf(url.PathEscape(bad)), url.Values{"tenant_id": {tenant.String()}}, c); w.Code != http.StatusNotFound ||
			!strings.Contains(w.Body.String(), "That link does not name a holding or a business") {
			t.Errorf("an add to the holding %q = %d, want 404", bad, w.Code)
		}
		if g.store.total()-total != g.store.count("TouchOperatorSession")-touches {
			t.Errorf("an add to the holding %q reached the store past the gate", bad)
		}
	}
	for _, bad := range []string{"", "FAKE", strings.ReplaceAll(tenant.String(), "-", ""), "{" + tenant.String() + "}",
		"urn:uuid:" + tenant.String(), tenant.String() + "0"} {
		reads, n := g.store.count("Holdings"), len(g.holdingWrites())
		w := g.post(membersPathOf(holding.String()), url.Values{"tenant_id": {bad}}, c)
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), headingIDRefused) {
			t.Errorf("the business id %q = %d, want 422 with the id's sentence", bad, w.Code)
		}
		if g.store.count("Holdings") != reads+1 || len(g.holdingWrites()) != n {
			t.Errorf("the business id %q: %d read(s) and %d write(s), want 1 and 0", bad, g.store.count("Holdings")-reads,
				len(g.holdingWrites())-n)
		}
	}
	w := g.post(membersPathOf(holding.String()), url.Values{"tenant_id": {"  " + strings.ToUpper(tenant.String()) + "\t"}}, c)
	ws := g.holdingWrites()
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != resultPathOf("add", holding.String(), tenant.String()) ||
		ws[len(ws)-1].tenant != tenant || ws[len(ws)-1].holding != holding {
		t.Errorf("CONTROL: a padded, upper-case business id = %d to %q", w.Code, w.Result().Header.Get("Location"))
	}
	for _, p := range []string{removePathOf(strings.ReplaceAll(holding.String(), "-", ""), tenant.String()),
		removePathOf(holding.String(), strings.ReplaceAll(tenant.String(), "-", "")), removePathOf(holding.String(), "FAKE")} {
		touches, total := g.store.count("TouchOperatorSession"), g.store.total()
		if w := g.post(p, url.Values{}, c); w.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", p, w.Code)
		}
		if g.store.total()-total != g.store.count("TouchOperatorSession")-touches {
			t.Errorf("POST %s reached the store past the gate", p)
		}
	}
	if w := g.post(removePathOf(holding.String(), tenant.String()), url.Values{}, c); w.Code != http.StatusSeeOther ||
		w.Result().Header.Get("Location") != resultPathOf("remove", holding.String(), tenant.String()) {
		t.Errorf("CONTROL: a removal = %d to %q", w.Code, w.Result().Header.Get("Location"))
	}
}

// ------------------------------------------------------------ the sentence --

// TestHoldings_TheResultIsReadNotPosted: the sentence above the list comes from the GET's own
// read (holdingResultNotice), never from the write -- on every branch, and across a page's edges.
//
// PART I, one page (the fake holds H1 with A live and B ended, H2 empty; C is a business in no
// holding): created H2 -> "Created."; add A to H1 -> "Added."; add B or C to H1 -> "Not added --
// nothing changed." with the four rules; add A to a holding the read does not hold -> "Not
// added"; remove B from H1 -> "Removed." and its sentence; remove A from H1 -> A is a live member;
// remove C from H1, A from H2, A from an absent holding -> "Nothing was removed"; created naming an
// absent holding -> nothing (the page is every row). Each page carries ONE heading. A query the
// screen does not take -- an unknown word, the word in capitals, a malformed holding, an add with
// no business or a malformed one -- draws no heading and is byte for byte the page with no
// query; and no id of a query is on its page unless the read returned it.
//
// PART II, two full pages (X: 150 live members, newest; Y: 100 live members): on page 1, X is
// whole -- an add to X of a business not in it is "Not added" -- and Y is cut -- an add of Y's
// member shown on page 1 is "Added.", of one on page 2 or of no member "the result is not on this
// page", a removal from Y likewise; on page 2, Y began on page 1 -- an add of a member shown there
// is "Added.", anything else not on this page; an add to a holding the full page does not hold is
// not on this page. The cut is said in words: Y's last memberships "on the next page", its first
// "on the page before".
//
// PART III -- these shapes only (no completeness claim).
func TestHoldings_TheResultIsReadNotPosted(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	at := fakeHoldingClock.Add(-24 * time.Hour)
	a := g.seedHoldingTenant(uuid.Nil, "FAKE A Ltd", "Europe/Malta", nil)
	b := g.seedHoldingTenant(uuid.Nil, "FAKE B Ltd", "Europe/Malta", nil)
	cc := g.seedHoldingTenant(uuid.Nil, "FAKE C Ltd", "Europe/London", nil)
	h2 := g.seedHolding(uuid.Nil, "FAKE H2 Group", at.Add(time.Hour))
	h1 := g.seedHolding(uuid.Nil, "FAKE H1 Group", at, liveMembership(a, at), endedMembership(b, at.Add(time.Minute)))
	absent := uuid.New()
	view := func(path string) string {
		t.Helper()
		w := g.get(path, c)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		return w.Body.String()
	}
	plain := view("/operator/holdings")
	if hs := headingsOn(plain); len(hs) != 0 {
		t.Fatalf("PREMISE: the page with no query carries %v", hs)
	}
	for _, tc := range []struct {
		name, done, holding, tenant, want string
	}{
		{"created H2", "created", h2.String(), "", headingCreated},
		{"add A to H1", "add", h1.String(), a.String(), headingAdded},
		{"add B to H1", "add", h1.String(), b.String(), headingNotAdded},
		{"add C to H1", "add", h1.String(), cc.String(), headingNotAdded},
		{"add A to an absent holding", "add", absent.String(), a.String(), headingNotAdded},
		{"remove B from H1", "remove", h1.String(), b.String(), headingRemoved},
		{"remove A from H1", "remove", h1.String(), a.String(), headingStillLive},
		{"remove C from H1", "remove", h1.String(), cc.String(), headingNotRemoved},
		{"remove A from H2", "remove", h2.String(), a.String(), headingNotRemoved},
		{"remove A from an absent holding", "remove", absent.String(), a.String(), headingNotRemoved},
	} {
		body := view(resultPathOf(tc.done, tc.holding, tc.tenant))
		if hs := headingsOn(body); len(hs) != 1 || hs[0] != tc.want {
			t.Errorf("%s: the page says %v, want [%s]", tc.name, hs, tc.want)
		}
		if tc.want == headingNotAdded {
			for _, rule := range []string{ruleTimezone, ruleOneHolding, ruleAPICode, ruleExists} {
				if !strings.Contains(body, "<li>"+rule+"</li>") {
					t.Errorf("%s: the rule %q is not listed", tc.name, rule)
				}
			}
		} else if strings.Contains(body, ruleTimezone) {
			t.Errorf("%s: the rules of an add are listed under %q", tc.name, tc.want)
		}
		if tc.want == headingRemoved && !strings.Contains(body, sentenceRemoved) {
			t.Errorf("%s: the removal's sentence is not on the page", tc.name)
		}
		if strings.Contains(body, absent.String()) {
			t.Errorf("%s: the query's absent holding id is on the page", tc.name)
		}
	}
	for _, q := range []string{
		resultPathOf("created", absent.String(), ""),
		resultPathOf("zz", h1.String(), a.String()), resultPathOf("ADD", h1.String(), a.String()),
		resultPathOf("add", strings.ReplaceAll(h1.String(), "-", ""), a.String()), resultPathOf("add", h1.String(), ""),
		resultPathOf("remove", h1.String(), "{"+a.String()+"}"), "/operator/holdings?done=&holding=&tenant=",
	} {
		if body := view(q); body != plain {
			t.Errorf("GET %s is not the page with no query (headings %v)", q, headingsOn(body))
		}
	}

	// PART II: two full pages.
	g2 := newRig(t)
	c2 := g2.signIn(g2.active())
	base := fakeHoldingClock.Add(-48 * time.Hour)
	var xMembers, yMembers []fakeMembership
	var yIDs []uuid.UUID // newest first: the order the read lists live memberships in
	for i := 0; i < 150; i++ {
		xMembers = append(xMembers, liveMembership(uuid.New(), base.Add(time.Duration(i)*time.Second)))
	}
	for i := 99; i >= 0; i-- {
		id := uuid.New()
		yMembers = append(yMembers, liveMembership(id, base.Add(time.Duration(i)*time.Second)))
		yIDs = append(yIDs, id)
	}
	x := g2.seedHolding(uuid.Nil, "FAKE X Group", base.Add(time.Hour), xMembers...)
	y := g2.seedHolding(uuid.Nil, "FAKE Y Group", base, yMembers...)
	onPage1, onPage2 := yIDs[0], yIDs[99] // the read's rows 151-200 are Y's 50 newest; rows 201-250 the rest
	view2 := func(path string) string {
		t.Helper()
		w := g2.get(path, c2)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		return w.Body.String()
	}
	for _, tc := range []struct {
		name, path, want string
	}{
		{"page 1, add a stranger to X (whole)", resultPathOf("add", x.String(), uuid.NewString()), headingNotAdded},
		{"page 1, add Y's member shown here", resultPathOf("add", y.String(), onPage1.String()), headingAdded},
		{"page 1, add Y's member on page 2", resultPathOf("add", y.String(), onPage2.String()), headingElsewhere},
		{"page 1, add a stranger to Y (cut)", resultPathOf("add", y.String(), uuid.NewString()), headingElsewhere},
		{"page 1, remove from Y (cut)", resultPathOf("remove", y.String(), onPage1.String()), headingElsewhere},
		{"page 1, add to an absent holding", resultPathOf("add", uuid.NewString(), onPage1.String()), headingElsewhere},
		{"page 2, add Y's member shown here", "/operator/holdings?page=2&done=add&holding=" + y.String() + "&tenant=" + onPage2.String(), headingAdded},
		{"page 2, add Y's member on page 1", "/operator/holdings?page=2&done=add&holding=" + y.String() + "&tenant=" + onPage1.String(), headingElsewhere},
		{"page 2, remove from Y (began before)", "/operator/holdings?page=2&done=remove&holding=" + y.String() + "&tenant=" + onPage2.String(), headingElsewhere},
	} {
		if hs := headingsOn(view2(tc.path)); len(hs) != 1 || hs[0] != tc.want {
			t.Errorf("%s: the page says %v, want [%s]", tc.name, hs, tc.want)
		}
	}
	page1, page2 := view2("/operator/holdings"), view2("/operator/holdings?page=2")
	if !strings.Contains(page1, "More of its memberships are on the next page.") || strings.Contains(page1, "on the page before") ||
		!strings.Contains(page2, "Its first memberships are on the page before.") || strings.Contains(page2, "on the next page") {
		t.Error("the cut of Y's memberships is not said on the two pages")
	}
	if strings.Count(page1, `<span class="tally tally--active">Live</span>`) != 200 ||
		strings.Count(page2, `<span class="tally tally--active">Live</span>`) != 50 {
		t.Error("PREMISE: the two pages do not hold 200 and 50 live memberships")
	}
}

// TestHoldings_PagesForwardOnlyAfterAFullPage: the list's page is the query's, 1..1000 in plain
// digits; anything else is refused before the store, and the pager offers the next page only
// after a full one.
//
// PART I -- a page that is not 1..maxHoldingPage in plain digits ("0", "1001", "-1", "+1", " 1",
// "1 ", "1e3", "x", "00001", "2147483648") is 404 with the page that names the range -- read off it
// and compared with maxHoldingPage -- and no read; "" and "01" are page 1, "2" page 2 (the store
// is asked for that page). A page of db.HoldingsPerPage rows offers "Next page" (?page=2); one
// row fewer offers none; page maxHoldingPage offers none however full; a later page offers
// "Previous page".
//
// PART II -- the list above. PART III -- these values only (no completeness claim).
func TestHoldings_PagesForwardOnlyAfterAFullPage(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	for _, bad := range []string{"0", "1001", "-1", "%2B1", "%201", "1%20", "1e3", "x", "00001", "2147483648"} {
		reads := g.store.count("Holdings")
		w := g.get("/operator/holdings?page="+bad, c)
		if w.Code != http.StatusNotFound ||
			!strings.Contains(w.Body.String(), "pages 1 to "+strconv.Itoa(operator.MaxHoldingPageForTest)+".") {
			t.Errorf("page %q = %d, want 404 naming pages 1 to %d", bad, w.Code, operator.MaxHoldingPageForTest)
		}
		if g.store.count("Holdings") != reads {
			t.Errorf("page %q reached the store", bad)
		}
	}
	for raw, want := range map[string]int32{"": 1, "01": 1, "2": 2, "1000": 1000} {
		n := len(g.holdingReads())
		if w := g.get("/operator/holdings?page="+raw, c); w.Code != http.StatusOK {
			t.Fatalf("page %q = %d", raw, w.Code)
		}
		if rs := g.holdingReads(); len(rs) != n+1 || rs[n].page != want {
			t.Errorf("page %q asked the store for %v, want page %d", raw, rs[n:], want)
		}
	}
	members := func(n int) []fakeMembership {
		var out []fakeMembership
		for i := 0; i < n; i++ {
			out = append(out, liveMembership(uuid.New(), fakeHoldingClock.Add(time.Duration(i)*time.Second)))
		}
		return out
	}
	full, short := newRig(t), newRig(t)
	full.seedHolding(uuid.Nil, "FAKE Full Group", fakeHoldingClock, members(db.HoldingsPerPage)...)
	short.seedHolding(uuid.Nil, "FAKE Short Group", fakeHoldingClock, members(db.HoldingsPerPage-1)...)
	fc, sc := full.signIn(full.active()), short.signIn(short.active())
	if body := full.get("/operator/holdings", fc).Body.String(); !strings.Contains(body, `href="/operator/holdings?page=2"`) {
		t.Error("a full page does not offer the next one")
	}
	if body := short.get("/operator/holdings", sc).Body.String(); strings.Contains(body, "Next page") {
		t.Error("a page one row short of full offers a next one")
	}
	full.store.mu.Lock()
	// Page 1000 of a list whose rows reach it: 999 full pages of other holdings first.
	var before []fakeHolding
	for i := 0; i < 999; i++ {
		before = append(before, fakeHolding{id: uuid.New(), name: "FAKE Filler", createdAt: fakeHoldingClock.Add(time.Hour),
			members: members(db.HoldingsPerPage)})
	}
	full.store.holdings = append(before, full.store.holdings...)
	full.store.mu.Unlock()
	last := full.get("/operator/holdings?page="+strconv.Itoa(operator.MaxHoldingPageForTest), fc).Body.String()
	if strings.Contains(last, "Next page") || !strings.Contains(last, `href="/operator/holdings?page=999"`) {
		t.Error("page 1000 offers a next page, or no previous one")
	}
}

// ------------------------------------------------------------ the writes --

// TestHoldings_EachWriteAnswersItsFailures: the three writes' answers and their log lines.
//
// PART I -- for each of create, add and remove: a session the write refuses is 303 to the sign-in
// with no cookie cleared; 22023 (a constraint the database caught) and any other error are 503
// with the write's fault page and ONE ERROR line carrying the SQLSTATE (and, for a membership, the
// two ids) and no "asked" line (the access record of the 503 is the router's own); the accepted write is 303 to the list with what was asked and ONE
// INFO line naming the operator, the holding and -- for a membership -- the business. No line of
// any of them carries the session token, its hash or the holding's name.
//
// PART II -- the list above. PART III -- these branches only (no completeness claim).
func TestHoldings_EachWriteAnswersItsFailures(t *testing.T) {
	g := newRig(t)
	f := g.active()
	c := g.signIn(f)
	holding, tenant := uuid.New(), uuid.New()
	const name = "FAKE Logged Group Name"
	writes := []struct {
		method, info, title string
		send                func() *httptest.ResponseRecorder
		ids                 []string
	}{
		{"CreateHolding", "operator created a holding", "The holding was not confirmed",
			func() *httptest.ResponseRecorder { return g.post("/operator/holdings", url.Values{"name": {name}}, c) }, nil},
		{"AddHoldingMember", "operator asked to add a business to a holding", "The change was not confirmed",
			func() *httptest.ResponseRecorder {
				return g.post(membersPathOf(holding.String()), url.Values{"tenant_id": {tenant.String()}}, c)
			}, []string{holding.String(), tenant.String()}},
		{"RemoveHoldingMember", "operator asked to remove a business from a holding", "The change was not confirmed",
			func() *httptest.ResponseRecorder {
				return g.post(removePathOf(holding.String(), tenant.String()), url.Values{}, c)
			},
			[]string{holding.String(), tenant.String()}},
	}
	g.store.mu.Lock()
	var hashes []string
	for h := range g.store.live {
		hashes = append(hashes, h)
	}
	g.store.mu.Unlock()
	clean := func(what, logs string) {
		t.Helper()
		for _, v := range append([]string{c.Value, name}, hashes...) {
			if strings.Contains(logs, v) {
				t.Errorf("%s: a log line carries the session token, its hash or the name", what)
			}
		}
	}
	for _, wr := range writes {
		done := g.failStore(wr.method, db.ErrOperatorRefused)
		g.logs.Reset()
		w := wr.send()
		done()
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" || len(w.Result().Cookies()) != 0 {
			t.Errorf("%s refused: %d to %q, cookies %d; want the sign-in and no cookie cleared", wr.method, w.Code,
				w.Result().Header.Get("Location"), len(w.Result().Cookies()))
		}
		clean(wr.method+" refused", g.logs.String())
		for _, fault := range []error{errFakeHoldingRefused, errFakeDB} {
			done := g.failStore(wr.method, fault)
			g.logs.Reset()
			w := wr.send()
			done()
			logs := g.logs.String()
			if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), wr.title) {
				t.Errorf("%s with %v = %d, want 503 with %q", wr.method, fault, w.Code, wr.title)
			}
			// The text handler's line of the fault (the access record of the 503 is the router's).
			if strings.Count(logs, `level=ERROR msg="operator: a holding`) != 1 || strings.Contains(logs, wr.info) ||
				!strings.Contains(logs, fault.Error()) {
				t.Errorf("%s with %v: the log is not one ERROR line carrying the error: %s", wr.method, fault, logs)
			}
			for _, id := range wr.ids {
				if !strings.Contains(logs, id) {
					t.Errorf("%s with %v: the ERROR line does not name %s", wr.method, fault, id)
				}
			}
			clean(wr.method+" failing", logs)
		}
		g.logs.Reset()
		w = wr.send()
		logs := g.logs.String()
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Result().Header.Get("Location"), "/operator/holdings?done=") {
			t.Errorf("%s accepted = %d to %q, want 303 to the list", wr.method, w.Code, w.Result().Header.Get("Location"))
		}
		if strings.Count(logs, "level=INFO msg=\""+wr.info+"\"") != 1 || !strings.Contains(logs, "admin_id="+f.id.String()) {
			t.Errorf("%s accepted: no one INFO line naming the operator: %s", wr.method, logs)
		}
		ids := wr.ids
		if wr.method == "CreateHolding" {
			ws := g.holdingWrites()
			ids = []string{strings.TrimPrefix(w.Result().Header.Get("Location"), "/operator/holdings?done=created&holding=")}
			if len(ws) == 0 || ws[len(ws)-1].method != "CreateHolding" {
				t.Fatal("PREMISE: the accepted create is not the fake's last write")
			}
		}
		for _, id := range ids {
			if !strings.Contains(logs, id) {
				t.Errorf("%s accepted: the INFO line does not name %s", wr.method, id)
			}
		}
		clean(wr.method+" accepted", logs)
	}
}

// TestHoldings_AClientThatLeavesStillGetsTheWrite: each of the three writes, sent on a request
// whose context is already cancelled (the client left after sending it), is still made -- the
// fake refuses a statement on a cancelled context, so a recorded write ran on a live one -- and
// answered 303 as for a client that stayed; a refused name on such a request is still the 422
// list, its read run on a live context. CONTROL: a GET of the list on a cancelled request is not
// read (the GET follows its client).
func TestHoldings_AClientThatLeavesStillGetsTheWrite(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	holding, tenant := uuid.New(), uuid.New()
	left, cancel := context.WithCancel(context.Background())
	cancel()
	for _, r := range []req{
		{method: http.MethodPost, path: "/operator/holdings", form: url.Values{"name": {"FAKE Left Group"}}},
		{method: http.MethodPost, path: membersPathOf(holding.String()), form: url.Values{"tenant_id": {tenant.String()}}},
		{method: http.MethodPost, path: removePathOf(holding.String(), tenant.String()), form: url.Values{}},
	} {
		r.host, r.origin, r.cookies, r.ctx = opHost, opOrigin, []*http.Cookie{c}, left
		n := len(g.holdingWrites())
		if w := g.do(r); w.Code != http.StatusSeeOther || len(g.holdingWrites()) != n+1 {
			t.Errorf("POST %s from a client that left = %d with %d write(s); want 303 and the write", r.path, w.Code,
				len(g.holdingWrites())-n)
		}
	}
	reads := g.store.count("Holdings")
	w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/holdings", form: url.Values{"name": {" "}}, origin: opOrigin,
		cookies: []*http.Cookie{c}, ctx: left})
	if w.Code != http.StatusUnprocessableEntity || g.store.count("Holdings") != reads+1 || !strings.Contains(w.Body.String(), headingNameRefused) {
		t.Errorf("a refused name from a client that left = %d; want the 422 list", w.Code)
	}
	if w := g.do(req{method: http.MethodGet, host: opHost, path: "/operator/holdings", cookies: []*http.Cookie{c}, ctx: left,
		header: map[string]string{"Sec-Fetch-Site": "same-origin"}}); w.Code == http.StatusOK {
		t.Error("CONTROL: a GET whose client left rendered the list; the GET is meant to follow its client")
	}
}

// TestHoldings_TheTwoBoundsAreTheirNumbers pins holdings.go's two bounds as numbers -- the list's
// read 5 s, a write 10 s (legal.go's) -- and measures each on the store call it bounds: the read's
// context (GET and a refused form alike) and each write's carry a deadline within a second under
// their bound.
func TestHoldings_TheTwoBoundsAreTheirNumbers(t *testing.T) {
	if operator.HoldingsReadTimeoutForTest != 5*time.Second || operator.HoldingWriteTimeoutForTest != 10*time.Second {
		t.Fatalf("the bounds are %v and %v, want 5s and 10s", operator.HoldingsReadTimeoutForTest, operator.HoldingWriteTimeoutForTest)
	}
	if operator.HoldingWriteTimeoutForTest != operator.LegalWriteTimeoutForTest {
		t.Errorf("a holding write's bound %v is not the legal publication's %v", operator.HoldingWriteTimeoutForTest,
			operator.LegalWriteTimeoutForTest)
	}
	g := newRig(t)
	c := g.signIn(g.active())
	holding, tenant := uuid.New(), uuid.New()
	g.get("/operator/holdings", c)
	g.post("/operator/holdings", url.Values{"name": {""}}, c)
	g.post("/operator/holdings", url.Values{"name": {"FAKE Bound Group"}}, c)
	g.post(membersPathOf(holding.String()), url.Values{"tenant_id": {tenant.String()}}, c)
	g.post(removePathOf(holding.String(), tenant.String()), url.Values{}, c)
	within := func(d, bound time.Duration) bool { return d > bound-time.Second && d <= bound }
	rs, ws := g.holdingReads(), g.holdingWrites()
	if len(rs) != 2 || len(ws) != 3 {
		t.Fatalf("PREMISE: %d read(s) and %d write(s), want 2 and 3", len(rs), len(ws))
	}
	for _, r := range rs {
		if !within(r.deadline, operator.HoldingsReadTimeoutForTest) {
			t.Errorf("a read's deadline was %v away, want within a second under %v", r.deadline, operator.HoldingsReadTimeoutForTest)
		}
	}
	for _, w := range ws {
		if !within(w.deadline, operator.HoldingWriteTimeoutForTest) {
			t.Errorf("%s's deadline was %v away, want within a second under %v", w.method, w.deadline, operator.HoldingWriteTimeoutForTest)
		}
	}
}

// TestHoldings_CreateAddRemoveThroughTheScreen walks the screen the way an operator does, each
// POST's 303 followed: a holding created ("Created.", its name on the list); a business added
// ("Added.", the business on the list, live, with its timezone and API code, and its Remove
// button); a business of another timezone ("Not added -- nothing changed." and the rules, the
// business not on the list); the first business removed ("Removed.", the membership ended, its
// Remove button gone). A second removal of the same business says "Removed." again: the read shows
// its latest membership ended and cannot tell an earlier removal from this one (the screen's
// stated limit -- holdingResultNotice).
func TestHoldings_CreateAddRemoveThroughTheScreen(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	code := "KF"
	a := g.seedHoldingTenant(uuid.Nil, "FAKE Walk A Ltd", "Europe/Malta", &code)
	b := g.seedHoldingTenant(uuid.Nil, "FAKE Walk B Ltd", "Europe/London", nil)
	follow := func(w *httptest.ResponseRecorder) string {
		t.Helper()
		loc := w.Result().Header.Get("Location")
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/operator/holdings?done=") {
			t.Fatalf("a write = %d to %q, want 303 to the list", w.Code, loc)
		}
		r := g.get(loc, c)
		if r.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", loc, r.Code)
		}
		return r.Body.String()
	}
	w := g.post("/operator/holdings", url.Values{"name": {"FAKE Walk Group"}}, c)
	body := follow(w)
	holding := strings.TrimPrefix(w.Result().Header.Get("Location"), "/operator/holdings?done=created&holding=")
	if hs := headingsOn(body); len(hs) != 1 || hs[0] != headingCreated || !strings.Contains(body, "<bdi>FAKE Walk Group</bdi>") ||
		!strings.Contains(body, "No business has been in this holding.") {
		t.Fatalf("after the create the page says %v", headingsOn(body))
	}
	body = follow(g.post(membersPathOf(holding), url.Values{"tenant_id": {a.String()}}, c))
	if hs := headingsOn(body); len(hs) != 1 || hs[0] != headingAdded || !strings.Contains(body, "<bdi>FAKE Walk A Ltd</bdi>") ||
		!strings.Contains(body, `API code <span class="font-mono">KF</span>`) ||
		!strings.Contains(body, `Timezone <span class="font-mono">Europe/Malta</span>`) ||
		!strings.Contains(body, `action="`+removePathOf(holding, a.String())+`"`) {
		t.Fatalf("after adding A the page says %v", headingsOn(body))
	}
	body = follow(g.post(membersPathOf(holding), url.Values{"tenant_id": {b.String()}}, c))
	if hs := headingsOn(body); len(hs) != 1 || hs[0] != headingNotAdded || strings.Contains(body, "FAKE Walk B Ltd") ||
		!strings.Contains(body, "<li>"+ruleTimezone+"</li>") {
		t.Fatalf("after adding B (another timezone) the page says %v", headingsOn(body))
	}
	body = follow(g.post(removePathOf(holding, a.String()), url.Values{}, c))
	if hs := headingsOn(body); len(hs) != 1 || hs[0] != headingRemoved || !strings.Contains(body, `<span class="tally tally--deactivated">Ended</span>`) ||
		strings.Contains(body, `action="`+removePathOf(holding, a.String())+`"`) {
		t.Fatalf("after removing A the page says %v", headingsOn(body))
	}
	body = follow(g.post(removePathOf(holding, a.String()), url.Values{}, c))
	if hs := headingsOn(body); len(hs) != 1 || hs[0] != headingRemoved {
		t.Fatalf("after removing A again the page says %v", headingsOn(body))
	}
}

// ------------------------------------------------------------ the budget --

// TestHoldingsBudget_AViewIsOneReadAndAWriteIsNone measures the holdings against surface.go's two
// budgets on one session, each request from its own address.
//
// PART I -- 10 accepted writes (4 creates, 3 adds, 3 removals) make no read; 59 views of the list
// and one refused name are the session's 60 reads; then a view of the list and a refused name are
// 429 with the signed-in page and no read, while a create, an add and a removal are still 303s and
// reach the store (a write is not a read). 75 requests in all -- under sessionLimit.
//
// PART II -- the counts above. PART III -- this sequence only (no completeness claim).
func TestHoldingsBudget_AViewIsOneReadAndAWriteIsNone(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	ad := &addrs{}
	send := func(r req) *httptest.ResponseRecorder {
		r.host, r.remote, r.cookies = opHost, ad.next()+":1", []*http.Cookie{c}
		if r.method == http.MethodGet {
			r.header = map[string]string{"Sec-Fetch-Site": "same-origin"}
		} else {
			r.origin = opOrigin
		}
		return g.do(r)
	}
	holding, tenant := uuid.New(), uuid.New()
	create := func(name string) req {
		return req{method: http.MethodPost, path: "/operator/holdings", form: url.Values{"name": {name}}}
	}
	add := req{method: http.MethodPost, path: membersPathOf(holding.String()), form: url.Values{"tenant_id": {tenant.String()}}}
	remove := req{method: http.MethodPost, path: removePathOf(holding.String(), tenant.String()), form: url.Values{}}
	list := req{method: http.MethodGet, path: "/operator/holdings"}
	for i, r := range []req{create("FAKE B1"), create("FAKE B2"), create("FAKE B3"), create("FAKE B4"), add, add, add, remove, remove, remove} {
		if w := send(r); w.Code != http.StatusSeeOther {
			t.Fatalf("write %d = %d", i+1, w.Code)
		}
	}
	if n := g.store.count("Holdings"); n != 0 {
		t.Fatalf("10 writes made %d read(s), want 0", n)
	}
	for i := 0; i < 59; i++ {
		if w := send(list); w.Code != http.StatusOK {
			t.Fatalf("view %d = %d", i+1, w.Code)
		}
	}
	if w := send(create(" ")); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the 60th read (a refused name) = %d", w.Code)
	}
	reads := g.store.count("Holdings")
	for _, r := range []req{list, create("")} {
		if w := send(r); w.Code != http.StatusTooManyRequests || !signedInTooMany(w.Body.String()) {
			t.Errorf("%s %s past the read budget = %d, want 429 with the signed-in page", r.method, r.path, w.Code)
		}
	}
	if g.store.count("Holdings") != reads {
		t.Errorf("the refused reads reached the store %d time(s)", g.store.count("Holdings")-reads)
	}
	n := len(g.holdingWrites())
	for _, r := range []req{create("FAKE B5"), add, remove} {
		if w := send(r); w.Code != http.StatusSeeOther {
			t.Errorf("%s %s past the read budget = %d, want 303 (a write is not a read)", r.method, r.path, w.Code)
		}
	}
	if len(g.holdingWrites()) != n+3 {
		t.Errorf("the writes past the read budget made %d write(s), want 3", len(g.holdingWrites())-n)
	}
}

// ------------------------------------------------------------ the names --

// TestHoldingsScreen_EscapesWhatOperatorsAndBusinessesNamed: a holding's name (the operator's
// text) and a business's name (its own) reach the page escaped and isolated for bidirectional
// text; a name that shows nothing is replaced by the words for it, the id beside it.
func TestHoldingsScreen_EscapesWhatOperatorsAndBusinessesNamed(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	at := fakeHoldingClock
	named := g.seedHoldingTenant(uuid.Nil, `FAKE <b>Bar</b> & "Grill" `+"\u202e"+`evil`, "Europe/Malta", nil)
	blank := g.seedHoldingTenant(uuid.Nil, "\u200b\u3164", "Europe/Malta", nil)
	blankHolding := g.seedHolding(uuid.Nil, "\u2060", at.Add(time.Minute), liveMembership(blank, at))
	g.seedHolding(uuid.Nil, `FAKE <script>x</script> & "Group" `+"\u202e"+`evil`, at, liveMembership(named, at))
	body := g.get("/operator/holdings", c).Body.String()
	for _, want := range []string{
		`<bdi>FAKE &lt;script&gt;x&lt;/script&gt; &amp; &#34;Group&#34; ` + "\u202e" + `evil</bdi>`,
		`<bdi>FAKE &lt;b&gt;Bar&lt;/b&gt; &amp; &#34;Grill&#34; ` + "\u202e" + `evil</bdi>`,
		`<span class="italic">Unnamed holding</span>`, `<span class="italic">Unnamed tenant</span>`,
		blankHolding.String(), blank.String(),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not carry %q", want)
		}
	}
	for _, never := range []string{"<script>x", "<b>Bar"} {
		if strings.Contains(body, never) {
			t.Errorf("the page carries %q unescaped", never)
		}
	}
	if strings.Contains(body, fmt.Sprintf("<bdi>%s</bdi>", "\u2060")) {
		t.Error("a holding named by an invisible character is drawn by that name")
	}
}
