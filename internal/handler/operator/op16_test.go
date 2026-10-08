package operator_test

// op16_test.go -- M10 OP-16 phase B: /operator/tenants/{id}/vat on the shipped router with the
// fake store and the fake VIES client (rig_test.go). The header table of its response classes
// (C142-C165); the three answers -- only a verdict is recorded, and no answer is a 503 with the
// screen and its sentence; a number VIES does not take is never sent; the number is the
// server's, never the client's; VIES is asked with no store call open; the write survives a
// client that leaves; the refusals reach no store; the read and VIES budgets; the number on no
// log line or header (backlog T107); the four states the customer's account screen's, by
// partition and by word; the screen's words, chips and contrast; and the package's structural
// pins on the number, the body and the signup package. Against PostgreSQL: op16_db_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// ------------------------------------------------------------------ fixtures --

// The fixtures' numbers. vatNumberOK is in the format VIES takes (Ireland's: a digit, a letter,
// five digits, a letter -- its longest run of digits is five, so no six-digit needle of the
// leak test sits inside it); vatNumberOdd is not in any country's format.
const (
	vatNumberOK  = "IE9F23456K"
	vatNumberOdd = "IE1234"
)

// The header table's two tenants: fixed ids, so the 303's Location is a designed constant
// (designedHeaders, op8r2_test.go).
var (
	vatHeaderTenant = uuid.MustParse("51600000-0000-4000-8000-000000000016")
	vatHeaderOdd    = uuid.MustParse("51600000-0000-4000-8000-000000000017")
)

// errFakeVATUnbound is the fake's answer for a write 00034 refuses as unbound to a committed
// read (SQLSTATE 22023), in internal/db's spelling of a database error.
var errFakeVATUnbound = errors.New("fake: db: record tenant vat check: database error (SQLSTATE 22023)")

// vatPathOf is the VAT screen of tenant id.
func vatPathOf(id string) string { return "/operator/tenants/" + id + "/vat" }

// seedVAT puts one tenant's VAT record into the fake under id (a new id for uuid.Nil) and
// returns the id.
func (g *rig) seedVAT(id uuid.UUID, name, number string, verified *bool, checkedAt *time.Time) uuid.UUID {
	g.t.Helper()
	if id == uuid.Nil {
		id = uuid.New()
	}
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	g.store.vats[id] = fakeVAT{name: name, number: number, verified: verified, checkedAt: checkedAt}
	return id
}

func (g *rig) vatWrites() []fakeVATWrite {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return append([]fakeVATWrite(nil), g.store.vatWrites...)
}

func (g *rig) vatRecord(id uuid.UUID) fakeVAT {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return g.store.vats[id]
}

// vatTrace is the VAT flow's trace and its overlaps, and resets both.
func (g *rig) vatTrace() ([]string, []string) {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	tr, ov := g.store.vatTrace, g.store.vatOverlaps
	g.store.vatTrace, g.store.vatOverlaps = nil, nil
	return tr, ov
}

// recheck posts the VAT screen's form -- empty, as the page's form is.
func (g *rig) recheck(id uuid.UUID, c *http.Cookie) *httptest.ResponseRecorder {
	g.t.Helper()
	return g.post(vatPathOf(id.String()), url.Values{}, c)
}

// The screen's words, written out here rather than read from the handler, so a changed word is
// red. The four labels are the customer's account screen's
// (TestVATScreen_TheFourStatesAreCalledWhatTheAccountScreenCallsThem reads them there).
const (
	wordVATNotChecked = "Not checked"
	wordVATNoAnswer   = "No answer"
	wordVATConfirmed  = "Confirmed"
	wordVATNotFound   = "Not found"

	sentenceVATNoAnswerNotice = "The EU VAT register (VIES) did not answer. Nothing was changed; the result below still stands. Try again in a few minutes."
	sentenceVATFormatNotice   = "It is not in the format VIES accepts for its country, so it was not sent. Nothing was changed. " +
		"The number can only be corrected by the platform owner, in the database: neither the business nor this screen can change it."
	sentenceVATBudgetNotice = "This session has asked VIES ten times in ten minutes. Wait ten minutes, then try again. Nothing was changed."
	sentenceVATNotAskable   = "This number is not in the format VIES accepts for its country, so it cannot be sent from here. Only the platform owner can correct it, in the database."
)

// vatForm is the screen's form, as the page writes it, for tenant id.
func vatForm(id string) string {
	return `<form method="post" action="` + vatPathOf(id) + `" class="mt-4"><button type="submit" class="btn btn--primary">Ask VIES again</button></form>`
}

// ------------------------------------------------------------ the header table --

// TestOperatorHeaders_TheVATClassesCarryThePolicy drives the twenty-four response classes of
// the VAT screen (C142-C165) with the 40-class test's hostile drive and checks
// (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these twenty-four, at WriteHeader (the recorder's Result().Header): the
// designed status, route, header names and values (designedHeaders: no Location but the
// sign-in's and, on the two recorded answers, the screen's own path); no hostile value
// (hostileValues) in a header value or in the body, raw or query-escaped -- every one's last
// request carries the hostile query, which holds a VAT number under the name vat_number
// (hostileDrive), and every POST's body carries that number too, so a POST that read its number
// from the request, or echoed it, is red here; no script; and the counts below: C155
// (cross-origin) and C165 (PUT) make no store call and no VIES call; C146, C150, C156 and C160
// make no TenantVAT call; C146-C150, C153-C161 and C164 no write; VIES is asked only on C151,
// C152, C153, C162 and C163 -- once each, about the tenant's own number.
//
// PART II -- the list above, on these twenty-four classes.
//
// PART III -- This test measures the twenty-four classes and the raw and query-escaped forms
// only; anything else (examples: a class a later handler adds, a hostile value echoed
// HTML-escaped or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_TheVATClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	g.seedVAT(vatHeaderTenant, "FAKE Header VAT Ltd", vatNumberOK, nil, nil)
	g.seedVAT(vatHeaderOdd, "FAKE Header Odd Ltd", vatNumberOdd, nil, nil)
	tenant := vatHeaderTenant.String()
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
	hostileBody := url.Values{"vat_number": hostileQuery["vat_number"]}
	view := func(id string, c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: vatPathOf(id), cookies: c, header: sfs}
	}
	ask := func(id string, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: vatPathOf(id), form: hostileBody, origin: opOrigin, cookies: c}
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
	answering := func(a operator.VATAnswer, r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			rq := r()
			g.vies.set(a)
			defer g.vies.set(operator.VATAnswerUnknown)
			return send(rq)
		}
	}
	failing := func(method string, err error, a operator.VATAnswer, r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			rq := r()
			g.store.mu.Lock()
			g.store.fail[method] = err
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); delete(g.store.fail, method); g.store.mu.Unlock() }()
			g.vies.set(a)
			defer g.vies.set(operator.VATAnswerUnknown)
			return send(rq)
		}
	}
	once := func(r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return send(r()) }
	}
	calls := map[string]map[string]int{}
	counted := func(class string, drive func() *httptest.ResponseRecorder) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			reads, writes, total, asked := g.store.count("TenantVAT"), len(g.vatWrites()), g.store.total(), len(g.vies.askedFor())
			w := drive()
			calls[class] = map[string]int{"TenantVAT": g.store.count("TenantVAT") - reads, "writes": len(g.vatWrites()) - writes,
				"all": g.store.total() - total, "VIES": len(g.vies.askedFor()) - asked}
			return w
		}
	}
	// spentReads is a session with its 60 reads spent on VAT views (readLimit).
	spentReads := func() *http.Cookie {
		t.Helper()
		c := signIn()
		for i := 0; i < 60; i++ {
			if w := send(view(tenant, c)); w.Code != http.StatusOK {
				t.Fatalf("PREMISE: VAT view %d = %d", i+1, w.Code)
			}
		}
		return c
	}
	// spentVIES is a session with its ten VIES lookups spent on answered re-checks (viesLimit).
	spentVIES := func() *http.Cookie {
		t.Helper()
		c := signIn()
		g.vies.set(operator.VATAnswerValid)
		defer g.vies.set(operator.VATAnswerUnknown)
		for i := 0; i < operator.VIESLimitForTest; i++ {
			if w := send(ask(tenant, c)); w.Code != http.StatusSeeOther {
				t.Fatalf("PREMISE: re-check %d = %d", i+1, w.Code)
			}
		}
		return c
	}
	malformed := strings.ReplaceAll(tenant, "-", "")
	dead := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}
	classes := []headerClass{
		{"C142 tenant VAT", 200, once(func() req { return view(tenant, signIn()) })},
		{"C143 tenant VAT without a cookie", 303, once(func() req { return view(tenant) })},
		{"C144 tenant VAT with a dead cookie", 303, once(func() req { return view(tenant, dead) })},
		{"C145 tenant VAT, a same-site read", 303, once(func() req {
			r := view(tenant, signIn())
			r.header = map[string]string{"Sec-Fetch-Site": "same-site"}
			return r
		})},
		{"C146 tenant VAT, a malformed id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C146", once(func() req { return view(malformed, c) }))()
		}},
		{"C147 tenant VAT, an id no tenant has", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C147", once(func() req { return view(uuid.NewString(), c) }))()
		}},
		{"C148 tenant VAT, the read fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C148", failing("TenantVAT", errFakeDB, operator.VATAnswerUnknown, func() req { return view(tenant, c) }))()
		}},
		{"C149 tenant VAT, the read's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C149", failing("TenantVAT", db.ErrOperatorRefused, operator.VATAnswerUnknown, func() req { return view(tenant, c) }))()
		}},
		{"C150 tenant VAT, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spentReads()
			return counted("C150", once(func() req { return view(tenant, c) }))()
		}},
		{"C151 re-check, VIES confirms the number", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C151", answering(operator.VATAnswerValid, func() req { return ask(tenant, c) }))()
		}},
		{"C152 re-check, VIES does not know the number", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C152", answering(operator.VATAnswerInvalid, func() req { return ask(tenant, c) }))()
		}},
		{"C153 re-check, VIES does not answer", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C153", answering(operator.VATAnswerUnknown, func() req { return ask(tenant, c) }))()
		}},
		{"C154 re-check without a cookie", 303, once(func() req { return ask(tenant) })},
		{"C155 re-check, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C155", answering(operator.VATAnswerValid, func() req {
				r := ask(tenant, c)
				r.origin, r.header = "https://taptime.mt", map[string]string{"Sec-Fetch-Site": "same-site"}
				return r
			}))()
		}},
		{"C156 re-check, a malformed id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C156", answering(operator.VATAnswerValid, func() req { return ask(malformed, c) }))()
		}},
		{"C157 re-check, an id no tenant has", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C157", answering(operator.VATAnswerValid, func() req { return ask(uuid.NewString(), c) }))()
		}},
		{"C158 re-check, a number VIES does not take", 422, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C158", answering(operator.VATAnswerValid, func() req { return ask(vatHeaderOdd.String(), c) }))()
		}},
		{"C159 re-check, the VIES budget refused", 429, func() *httptest.ResponseRecorder {
			c := spentVIES()
			return counted("C159", answering(operator.VATAnswerValid, func() req { return ask(tenant, c) }))()
		}},
		{"C160 re-check, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spentReads()
			return counted("C160", answering(operator.VATAnswerValid, func() req { return ask(tenant, c) }))()
		}},
		{"C161 re-check, the read fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C161", failing("TenantVAT", errFakeDB, operator.VATAnswerValid, func() req { return ask(tenant, c) }))()
		}},
		{"C162 re-check, the write fails", 503, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C162", failing("RecordTenantVATCheck", errFakeVATUnbound, operator.VATAnswerValid, func() req { return ask(tenant, c) }))()
		}},
		{"C163 re-check, the write's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C163", failing("RecordTenantVATCheck", db.ErrOperatorRefused, operator.VATAnswerInvalid, func() req { return ask(tenant, c) }))()
		}},
		{"C164 re-check, the read's session is refused", 303, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C164", failing("TenantVAT", db.ErrOperatorRefused, operator.VATAnswerValid, func() req { return ask(tenant, c) }))()
		}},
		{"C165 PUT on tenant VAT", 405, counted("C165", answering(operator.VATAnswerValid, func() req {
			return req{method: http.MethodPut, path: vatPathOf(tenant), origin: opOrigin, form: hostileBody}
		}))},
	}
	if len(classes) != 24 {
		t.Fatalf("PREMISE: %d classes, the comment says 24", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(142+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 142+i, c.name, 142+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	for class, want := range map[string]map[string]int{
		"C146": {"TenantVAT": 0, "writes": 0, "VIES": 0}, "C147": {"TenantVAT": 1, "writes": 0, "VIES": 0},
		"C148": {"writes": 0, "VIES": 0}, "C149": {"writes": 0, "VIES": 0}, "C150": {"TenantVAT": 0, "writes": 0, "VIES": 0},
		"C151": {"TenantVAT": 1, "writes": 1, "VIES": 1}, "C152": {"TenantVAT": 1, "writes": 1, "VIES": 1},
		"C153": {"TenantVAT": 1, "writes": 0, "VIES": 1}, "C155": {"all": 0, "VIES": 0},
		"C156": {"TenantVAT": 0, "writes": 0, "VIES": 0}, "C157": {"TenantVAT": 1, "writes": 0, "VIES": 0},
		"C158": {"TenantVAT": 1, "writes": 0, "VIES": 0}, "C159": {"TenantVAT": 1, "writes": 0, "VIES": 0},
		"C160": {"TenantVAT": 0, "writes": 0, "VIES": 0}, "C161": {"writes": 0, "VIES": 0},
		"C162": {"TenantVAT": 1, "writes": 0, "VIES": 1}, "C163": {"TenantVAT": 1, "writes": 0, "VIES": 1},
		"C164": {"writes": 0, "VIES": 0}, "C165": {"all": 0, "VIES": 0},
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
	for _, n := range g.vies.askedFor() {
		if n != vatNumberOK {
			t.Errorf("VIES was asked about %q; the only number it may be sent is the tenant's own", n)
		}
	}
	for _, w := range g.vatWrites() {
		if w.number != vatNumberOK || w.tenant != vatHeaderTenant {
			t.Errorf("a write for %s carried %q; want the tenant's own number for the path's tenant", w.tenant, w.number)
		}
	}
}

// ------------------------------------------------------------------ the answers --

// TestVATRecheck_OnlyAVerdictIsRecorded: what each of VIES's answers does, through the shipped
// handler.
//
// PART I -- a tenant never asked: VIES confirms -> 303 to the screen, ONE write of true for the
// read's number, and the screen the redirect lands on says "Confirmed" with the time the
// verdict was stamped (UTC, to the minute); VIES does not know the number -> 303, one write of
// false, "Not found"; VIES does not answer -> 503, NO write, the record unchanged, and the
// response is the screen itself -- the banner's name, the number, the verdict still on file
// ("Not found") and the no-answer notice ("The EU VAT register (VIES) did not answer. Nothing
// was changed; the result below still stands. Try again in a few minutes.") above it, and the
// verdict's time still labelled as its answer's ("Last answer", the security audit's D2: this
// ask wrote nothing) -- with one WARN line naming the tenant's id; a VATAnswer this build does not name (7, -1) is answered exactly as no answer.
// Each POST made one read and asked VIES once.
//
// PART II -- the list above. PART III -- these answers only (no completeness claim).
func TestVATRecheck_OnlyAVerdictIsRecorded(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedVAT(uuid.Nil, "FAKE Answers Ltd", vatNumberOK, nil, nil)
	path := vatPathOf(id.String())

	answer := func(a operator.VATAnswer, wantCode int, wantWrites int) *httptest.ResponseRecorder {
		t.Helper()
		g.vies.set(a)
		reads, asked := g.store.count("TenantVAT"), len(g.vies.askedFor())
		w := g.recheck(id, c)
		if w.Code != wantCode {
			t.Fatalf("answer %d: %d, want %d; log: %s", a, w.Code, wantCode, g.logs.String())
		}
		if n := len(g.vatWrites()); n != wantWrites {
			t.Fatalf("answer %d: %d write(s) in all, want %d", a, n, wantWrites)
		}
		if g.store.count("TenantVAT") != reads+1 || len(g.vies.askedFor()) != asked+1 {
			t.Fatalf("answer %d: %d read(s) and %d VIES call(s), want one of each", a,
				g.store.count("TenantVAT")-reads, len(g.vies.askedFor())-asked)
		}
		return w
	}
	screen := func() string {
		t.Helper()
		w := g.get(path, c)
		if w.Code != http.StatusOK {
			t.Fatalf("the screen = %d", w.Code)
		}
		return w.Body.String()
	}

	// VIES confirms.
	w := answer(operator.VATAnswerValid, http.StatusSeeOther, 1)
	if loc := w.Result().Header.Get("Location"); loc != path {
		t.Errorf("the recorded answer redirects to %q, want the screen %q", loc, path)
	}
	if wr := g.vatWrites()[0]; !wr.valid || wr.number != vatNumberOK || wr.tenant != id {
		t.Errorf("the write = %+v, want true for the read's number and the path's tenant", wr)
	}
	body := screen()
	if !strings.Contains(body, `<span class="tally tally--vat-confirmed">`+wordVATConfirmed+`</span>`) ||
		!strings.Contains(body, `Last answer <span class="font-mono">2026-10-07 14:03 UTC</span>`) {
		t.Errorf("after a confirmation the screen does not say %q with the verdict's time", wordVATConfirmed)
	}

	// VIES does not know the number.
	answer(operator.VATAnswerInvalid, http.StatusSeeOther, 2)
	if wr := g.vatWrites()[1]; wr.valid || wr.number != vatNumberOK {
		t.Errorf("the second write = %+v, want false for the read's number", wr)
	}
	body = screen()
	if !strings.Contains(body, `<span class="tally tally--vat-not-found">`+wordVATNotFound+`</span>`) ||
		!strings.Contains(body, `Last answer <span class="font-mono">2026-10-07 14:04 UTC</span>`) {
		t.Errorf("after a refusal the screen does not say %q with the verdict's time", wordVATNotFound)
	}

	// VIES does not answer, and two values no case names: no write, the screen with its notice.
	before := g.vatRecord(id)
	for _, a := range []operator.VATAnswer{operator.VATAnswerUnknown, operator.VATAnswer(7), operator.VATAnswer(-1)} {
		g.logs.Reset()
		w := answer(a, http.StatusServiceUnavailable, 2)
		body := w.Body.String()
		if !strings.Contains(body, html.EscapeString(sentenceVATNoAnswerNotice)) ||
			!strings.Contains(body, "VIES did not answer") ||
			!strings.Contains(body, `<span class="tally tally--vat-not-found">`+wordVATNotFound+`</span>`) ||
			!strings.Contains(body, "FAKE Answers Ltd") || !strings.Contains(body, vatNumberOK) ||
			!strings.Contains(body, vatForm(id.String())) {
			t.Errorf("answer %d: the 503 is not the screen with the no-answer notice and the verdict still on file", a)
		}
		// The time under the verdict is its ANSWER's (the security audit's D2): this ask got
		// none and wrote nothing, so the line must not claim to date the last ask.
		if !strings.Contains(body, `Last answer <span class="font-mono">2026-10-07 14:04 UTC</span>`) ||
			strings.Contains(body, "Last asked") {
			t.Errorf("answer %d: the 503 screen does not date the verdict by its answer", a)
		}
		if strings.Index(body, html.EscapeString(sentenceVATNoAnswerNotice)) > strings.Index(body, `aria-labelledby="tenant-vat"`) {
			t.Errorf("answer %d: the notice is not above the record", a)
		}
		if got := g.vatRecord(id); !reflect.DeepEqual(got, before) {
			t.Errorf("answer %d changed the record: %+v -> %+v", a, before, got)
		}
		if n := strings.Count(g.logs.String(), "VIES did not answer a VAT re-check"); n != 2 || !strings.Contains(g.logs.String(), id.String()) {
			t.Errorf("answer %d: %d no-answer line(s) across the two formats (want one record), or it does not name the tenant", a, n)
		}
	}
}

// TestVATRecheck_ANumberVIESDoesNotTakeIsNeverSent: a tenant whose number is not in a format
// VIES accepts (signup.ValidVATFormat -- a short one, an unknown prefix, lower case, white space
// inside, empty) is never sent.
//
// PART I -- for each: the screen shows the number and, in the form's place, the sentence
// "This number is not in the format VIES accepts for its country, so it cannot be sent from
// here." and no form; a POST is 422 with the screen and the format notice, one read, VIES asked 0
// times, no write; after five such POSTs (25 in all) a format-valid tenant is still asked ten
// times before the eleventh is refused by the VIES budget -- a refused number spends none of it.
//
// PART II -- the list above. PART III -- these numbers only; the format rule itself is
// internal/domain/signup's (its own tests).
func TestVATRecheck_ANumberVIESDoesNotTakeIsNeverSent(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	g.vies.set(operator.VATAnswerValid)
	for _, number := range []string{vatNumberOdd, "ZZ12345678", "ie9f23456k", "IE9F 3456K", ""} {
		id := g.seedVAT(uuid.Nil, "FAKE Odd Number Ltd", number, nil, nil)
		w := g.get(vatPathOf(id.String()), c)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), sentenceVATNotAskable) ||
			strings.Contains(w.Body.String(), `action="`+vatPathOf(id.String())+`"`) {
			t.Errorf("%q: the screen = %d, or it offers the form, or it does not say why not", number, w.Code)
		}
		for i := 0; i < 5; i++ {
			reads := g.store.count("TenantVAT")
			w := g.recheck(id, c)
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), html.EscapeString(sentenceVATFormatNotice)) ||
				!strings.Contains(w.Body.String(), "This number cannot be sent to VIES") {
				t.Fatalf("%q: a re-check = %d, or the page does not carry the format notice", number, w.Code)
			}
			if g.store.count("TenantVAT") != reads+1 {
				t.Errorf("%q: the re-check made %d read(s), want 1", number, g.store.count("TenantVAT")-reads)
			}
		}
	}
	if n := len(g.vies.askedFor()); n != 0 {
		t.Fatalf("VIES was asked %d time(s) about numbers it does not take", n)
	}
	if n := len(g.vatWrites()); n != 0 {
		t.Fatalf("%d write(s) for numbers VIES was never asked about", n)
	}
	// None of the 25 spent the VIES budget.
	ok := g.seedVAT(uuid.Nil, "FAKE Good Number Ltd", vatNumberOK, nil, nil)
	for i := 0; i < operator.VIESLimitForTest; i++ {
		if w := g.recheck(ok, c); w.Code != http.StatusSeeOther {
			t.Fatalf("re-check %d after 25 refused numbers = %d, want 303 -- a refused number spent the VIES budget", i+1, w.Code)
		}
	}
	if w := g.recheck(ok, c); w.Code != http.StatusTooManyRequests {
		t.Errorf("the eleventh lookup = %d, want 429", w.Code)
	}
}

// TestVATRecheck_TheNumberIsTheServersNotTheClients (the orchestrator's K16-1): the POST takes
// no number from the request.
//
// PART I -- the screen's form is exactly its button (no input of any kind); POSTs carrying a
// different number in the body under vat_number, number, vat and q, in the query string under
// the same names, a JSON body naming one, and a body over the forms' 16 KiB bound, each
// answered by VIES as valid: every one is 303, VIES was asked about the read's number and
// nothing else, and every write carries the read's number. CONTROL: the posted numbers are
// format-valid, so a handler that used one would have sent it.
//
// PART II -- the list above. PART III -- these requests only (no completeness claim); the
// structural side is TestVATNumber_TheListedSitesAloneReadIt.
func TestVATRecheck_TheNumberIsTheServersNotTheClients(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedVAT(uuid.Nil, "FAKE Own Number Ltd", vatNumberOK, nil, nil)
	path := vatPathOf(id.String())
	w := g.get(path, c)
	form := regexp.MustCompile(`(?s)<form method="post" action="` + regexp.QuoteMeta(path) + `"[^>]*>(.*?)</form>`).FindStringSubmatch(w.Body.String())
	if form == nil || strings.Contains(form[1], "<input") || strings.Contains(form[1], "<textarea") ||
		strings.Contains(form[1], "<select") || !strings.Contains(w.Body.String(), vatForm(id.String())) {
		t.Fatalf("the screen's form is not its button alone: %q", form)
	}
	g.vies.set(operator.VATAnswerValid)
	const other = "IE1A23456Z"
	sendRaw := func(target, contentType, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://"+opHost+target, strings.NewReader(body))
		r.Host = opHost
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("Origin", opOrigin)
		r.AddCookie(c)
		r.RemoteAddr = "192.0.2.10:4000"
		rec := httptest.NewRecorder()
		g.h.ServeHTTP(rec, r)
		return rec
	}
	posts := map[string]func() *httptest.ResponseRecorder{
		"the body": func() *httptest.ResponseRecorder {
			return g.post(path, url.Values{"vat_number": {other}, "number": {other}, "vat": {other}, "q": {other}}, c)
		},
		"the query": func() *httptest.ResponseRecorder {
			return g.post(path+"?vat_number="+other+"&number="+other+"&vat="+other, url.Values{}, c)
		},
		"a JSON body": func() *httptest.ResponseRecorder {
			return sendRaw(path, "application/json", `{"vat_number":"`+other+`"}`)
		},
		"a body over the bound": func() *httptest.ResponseRecorder {
			return sendRaw(path, "application/x-www-form-urlencoded", "vat_number="+other+"&pad="+strings.Repeat("x", 20<<10))
		},
	}
	for name, send := range posts {
		if w := send(); w.Code != http.StatusSeeOther {
			t.Errorf("%s: %d, want 303", name, w.Code)
		}
	}
	asked := g.vies.askedFor()
	if len(asked) != len(posts) {
		t.Fatalf("PREMISE: VIES was asked %d time(s) for %d posts", len(asked), len(posts))
	}
	for _, n := range asked {
		if n != vatNumberOK {
			t.Errorf("VIES was asked about %q; the read's number is %q", n, vatNumberOK)
		}
	}
	for _, wr := range g.vatWrites() {
		if wr.number != vatNumberOK {
			t.Errorf("a write carried %q; the read's number is %q", wr.number, vatNumberOK)
		}
	}
	if !strings.HasPrefix(other, "IE") || len(other) != len(vatNumberOK) {
		t.Fatal("CONTROL: the posted number is not a format-valid number a handler could have sent")
	}
}

// TestVATRecheck_VIESIsAskedWithNoStoreCallOpen (the card's T2): VIES is asked after the read
// has returned and before the write is made, and no store call enters while it is being asked.
//
// PART I -- with VIES held for 30 ms: an answered re-check's trace is exactly TenantVAT, the
// VIES call's start and end, RecordTenantVATCheck; an unanswered one's TenantVAT and the VIES
// call; a refused number's TenantVAT alone; no store call entered during any VIES call.
// operator.VATStore's two methods take no function -- a store method that ran the VIES call
// inside its own transaction would need one. (On PostgreSQL,
// TestE2E_VATRecheckRecordsVIESAnswerAndTheTenantReadsIt measures
// the operator connection idle with no transaction while VIES is asked; internal/db's
// TestOperatorDB_TheVATMethodsHoldNoConnectionOnceTheyReturn the pool holding no connection
// after the read.)
//
// PART II -- the list above. PART III -- these flows only (no completeness claim).
func TestVATRecheck_VIESIsAskedWithNoStoreCallOpen(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedVAT(uuid.Nil, "FAKE Order Ltd", vatNumberOK, nil, nil)
	odd := g.seedVAT(uuid.Nil, "FAKE Order Odd Ltd", vatNumberOdd, nil, nil)
	g.vies.hold = 30 * time.Millisecond
	g.vatTrace()
	for _, c2 := range []struct {
		name   string
		answer operator.VATAnswer
		tenant uuid.UUID
		want   []string
	}{
		{"answered", operator.VATAnswerValid, id, []string{"TenantVAT", "vies>", "<vies", "RecordTenantVATCheck"}},
		{"refused", operator.VATAnswerInvalid, id, []string{"TenantVAT", "vies>", "<vies", "RecordTenantVATCheck"}},
		{"unanswered", operator.VATAnswerUnknown, id, []string{"TenantVAT", "vies>", "<vies"}},
		{"a number VIES does not take", operator.VATAnswerValid, odd, []string{"TenantVAT"}},
	} {
		g.vies.set(c2.answer)
		g.recheck(c2.tenant, c)
		trace, overlaps := g.vatTrace()
		if !slices.Equal(trace, c2.want) {
			t.Errorf("%s: the trace is %v, want %v", c2.name, trace, c2.want)
		}
		if len(overlaps) != 0 {
			t.Errorf("%s: %v", c2.name, overlaps)
		}
	}
	st := reflect.TypeOf((*operator.VATStore)(nil)).Elem()
	if st.NumMethod() != 2 {
		t.Fatalf("PREMISE: operator.VATStore has %d methods, want 2", st.NumMethod())
	}
	for i := 0; i < st.NumMethod(); i++ {
		m := st.Method(i)
		for j := 0; j < m.Type.NumIn(); j++ {
			if m.Type.In(j).Kind() == reflect.Func {
				t.Errorf("operator.VATStore.%s takes a function (%s): the shape that would run VIES inside a store call", m.Name, m.Type.In(j))
			}
		}
	}
}

// TestVATRecheck_AClientThatLeavesStillGetsItsAnswerRecorded: past the id check an ask runs to its
// end whatever the client does (answered(r); the write's own detached context; legal.go's
// precedent).
//
// PART I -- three requests whose context is cancelled in flight (the fake store's TenantVAT and
// the fake VIES refuse nothing on their own, but the fake store refuses a cancelled context, as
// a statement on one does not run): (1) the client leaves before the read (a hook in the read
// cancels the request's context) and VIES confirms -- the read, VIES and the write all run: 303,
// one write; (2) the client leaves while VIES is being asked and VIES confirms -- VIES's context
// was NOT cancelled and carried a deadline more than 3 s and at most 4 s away (vatAskTimeout),
// and exactly one write was made, for the read's number, under a deadline more than 9 s and at
// most 10 s away (vatWriteTimeout) -- both bounds written here as numbers, not read from the
// handler, so a bound changed in vat.go is red here as well as in
// TestVATRecheck_TheThreeBoundsAreTheirNumbers; (3) the client leaves while VIES is being asked and VIES does not
// answer -- 503 with the screen and its notice, rendered (not the plain 500 of a page rendered on
// a cancelled context), and no write. CONTROL: a client that stays is answered and written the
// same way.
//
// PART II -- the list above. PART III -- these three requests only (no completeness claim).
func TestVATRecheck_AClientThatLeavesStillGetsItsAnswerRecorded(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedVAT(uuid.Nil, "FAKE Leaving Ltd", vatNumberOK, nil, nil)
	leave := func(when string, a operator.VATAnswer) *httptest.ResponseRecorder {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		g.vies.set(a)
		switch when {
		case "before the read":
			g.store.mu.Lock()
			g.store.beforeVATRead = cancel
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); g.store.beforeVATRead = nil; g.store.mu.Unlock() }()
		case "while VIES is asked":
			g.vies.during = cancel
			defer func() { g.vies.during = nil }()
		}
		return g.do(req{method: http.MethodPost, host: opHost, path: vatPathOf(id.String()), form: url.Values{}, origin: opOrigin,
			cookies: []*http.Cookie{c}, ctx: ctx})
	}
	lastAsk := func() (bool, time.Duration) {
		g.vies.mu.Lock()
		defer g.vies.mu.Unlock()
		n := len(g.vies.cancelled)
		return g.vies.cancelled[n-1], g.vies.deadlines[n-1]
	}

	// (1) before the read.
	if w := leave("before the read", operator.VATAnswerValid); w.Code != http.StatusSeeOther || len(g.vatWrites()) != 1 {
		t.Fatalf("a client that left before the read = %d with %d write(s), want 303 and the answer recorded", w.Code, len(g.vatWrites()))
	}
	// (2) while VIES is asked, VIES confirms.
	if w := leave("while VIES is asked", operator.VATAnswerValid); w.Code != http.StatusSeeOther {
		t.Fatalf("a client that left while VIES was asked = %d, want 303", w.Code)
	}
	if cancelled, left := lastAsk(); cancelled || left <= 3*time.Second || left > 4*time.Second {
		t.Errorf("VIES's context: cancelled %v, %v left; want not cancelled, with a deadline more than 3s and at most 4s away", cancelled, left)
	}
	writes := g.vatWrites()
	if len(writes) != 2 || writes[1].number != vatNumberOK || !writes[1].valid {
		t.Fatalf("a client that left while VIES was asked got %d write(s) (%+v), want its answer recorded", len(writes), writes)
	}
	if d := writes[1].deadline; d <= 9*time.Second || d > 10*time.Second {
		t.Errorf("the write's context has %v left, want a deadline of its own more than 9s and at most 10s away", d)
	}
	// (3) while VIES is asked, VIES does not answer.
	w := leave("while VIES is asked", operator.VATAnswerUnknown)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), html.EscapeString(sentenceVATNoAnswerNotice)) ||
		len(g.vatWrites()) != 2 {
		t.Errorf("no answer to a client that left = %d (notice %v) with %d write(s), want the 503 screen and no write", w.Code,
			strings.Contains(w.Body.String(), html.EscapeString(sentenceVATNoAnswerNotice)), len(g.vatWrites()))
	}
	// CONTROL: a client that stays.
	g.vies.set(operator.VATAnswerInvalid)
	if w := g.recheck(id, c); w.Code != http.StatusSeeOther || len(g.vatWrites()) != 3 {
		t.Errorf("CONTROL: a client that stays = %d with %d write(s)", w.Code, len(g.vatWrites()))
	}
}

// TestVATRecheck_TheReadIsBoundedOnBothMethods (round 2's F1): the screen's read carries a
// deadline of its own, vatReadTimeout, on the GET and on the POST. The POST runs on answered(r),
// which drops the router's deadline (httpx.RequestTimeout) with the client's cancellation, so
// without its own bound its read would run as long as the database let it.
//
// PART I -- the deadline of the read's context, as the fake store's TenantVAT sees it on entry,
// on three requests: the screen (GET); a re-check (POST, VIES confirms: 303); a re-check whose
// client left before the read (the read's hook cancels the request's context; 303) -- each more
// than 4 s and at most 5 s away (vatReadTimeout, written here as its number). CONTROL: the fake
// reports a context that carries no deadline as having none, so a read without one is seen.
//
// PART II -- the three requests above. PART III -- these three only (no completeness claim).
func TestVATRecheck_TheReadIsBoundedOnBothMethods(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedVAT(uuid.Nil, "FAKE Bounded Read Ltd", vatNumberOK, nil, nil)
	g.vies.set(operator.VATAnswerValid)
	lastRead := func() time.Duration {
		t.Helper()
		g.store.mu.Lock()
		defer g.store.mu.Unlock()
		if len(g.store.vatReadDeadlines) == 0 {
			t.Fatal("PREMISE: no read was made")
		}
		return g.store.vatReadDeadlines[len(g.store.vatReadDeadlines)-1]
	}
	for _, a := range []struct {
		name string
		code int
		send func() *httptest.ResponseRecorder
	}{
		{"GET, the screen", http.StatusOK, func() *httptest.ResponseRecorder { return g.get(vatPathOf(id.String()), c) }},
		{"POST, a re-check", http.StatusSeeOther, func() *httptest.ResponseRecorder { return g.recheck(id, c) }},
		{"POST, a re-check whose client left before the read", http.StatusSeeOther, func() *httptest.ResponseRecorder {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			g.store.mu.Lock()
			g.store.beforeVATRead = cancel
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); g.store.beforeVATRead = nil; g.store.mu.Unlock() }()
			return g.do(req{method: http.MethodPost, host: opHost, path: vatPathOf(id.String()), form: url.Values{}, origin: opOrigin,
				cookies: []*http.Cookie{c}, ctx: ctx})
		}},
	} {
		if w := a.send(); w.Code != a.code {
			t.Fatalf("PREMISE: %s = %d, want %d", a.name, w.Code, a.code)
		}
		if left := lastRead(); left <= 4*time.Second || left > 5*time.Second {
			t.Errorf("%s: the read's context has %v left, want a deadline more than 4s and at most 5s away", a.name, left)
		}
	}
	// CONTROL: a context without a deadline is reported as one.
	if _, err := g.store.TenantVAT(context.Background(), "not a live session", id); !errors.Is(err, db.ErrOperatorRefused) {
		t.Fatalf("CONTROL: the fake's read on a dead session = %v", err)
	}
	if left := lastRead(); left != -1 {
		t.Errorf("CONTROL: a context with no deadline was reported as %v away", left)
	}
}

// TestVATRecheck_TheThreeBoundsAreTheirNumbers (round 2's F3) pins vat.go's three bounds as
// numbers: the read 5 s, the VIES call 4 s, the write 10 s -- the write's the same as legal.go's
// legalWriteTimeout -- and VATRecheckWorstCase their sum, 19 s. That the sum fits in the HTTP
// drain on shutdown is cmd/tappa's TestShutdownBudget_TheVATRecheckNestsInsideTheHTTPGrace;
// that each bound reaches its step's context is TestVATRecheck_TheReadIsBoundedOnBothMethods and
// TestVATRecheck_AClientThatLeavesStillGetsItsAnswerRecorded.
func TestVATRecheck_TheThreeBoundsAreTheirNumbers(t *testing.T) {
	for _, b := range []struct {
		name      string
		got, want time.Duration
	}{
		{"vatReadTimeout", operator.VATReadTimeoutForTest, 5 * time.Second},
		{"vatAskTimeout", operator.VATAskTimeoutForTest, 4 * time.Second},
		{"vatWriteTimeout", operator.VATWriteTimeoutForTest, 10 * time.Second},
		{"vatWriteTimeout against legal.go's legalWriteTimeout", operator.VATWriteTimeoutForTest, operator.LegalWriteTimeoutForTest},
		{"VATRecheckWorstCase", operator.VATRecheckWorstCase, 19 * time.Second},
	} {
		if b.got != b.want {
			t.Errorf("%s = %v, want %v", b.name, b.got, b.want)
		}
	}
}

// TestVATRecheck_EveryRefusalWritesNothing: the screen's refusals and faults, and what each
// reaches.
//
// PART I -- a malformed id (GET and POST): 404, the not-an-id page, no store call; a
// cross-origin POST: 403, no store call; a POST without a cookie: the sign-in's 303, no store
// call; an id no tenant has: 404 (the no-such-tenant page) after one read, VIES not asked; the
// read failing: 503 ("This tenant's VAT number could not be loaded ... Nothing was sent to
// VIES."), VIES not asked, a log line naming the tenant; the read's session refused: the
// sign-in's 303, VIES not asked; a store that answers for ANOTHER tenant: 503, VIES not asked,
// that tenant's name and number on no page; the write failing with 22023: 503 ("The re-check was
// not confirmed"), its log line naming the tenant and the SQLSTATE; the write's session
// refused: the sign-in's 303. No arm writes.
//
// PART II -- the list above. PART III -- these arms only (no completeness claim).
func TestVATRecheck_EveryRefusalWritesNothing(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedVAT(uuid.Nil, "FAKE Refusals Ltd", vatNumberOK, nil, nil)
	other := g.seedVAT(uuid.Nil, "FAKE Someone Else Ltd", "IE8B76543Q", nil, nil)
	g.store.mu.Lock()
	x := g.store.vats[other]
	x.asTenant = uuid.New()
	g.store.vats[other] = x
	g.store.mu.Unlock()
	g.vies.set(operator.VATAnswerValid)
	malformed := strings.ReplaceAll(id.String(), "-", "")
	fail := func(m string, err error) {
		g.store.mu.Lock()
		defer g.store.mu.Unlock()
		if err == nil {
			delete(g.store.fail, m)
		} else {
			g.store.fail[m] = err
		}
	}
	type arm struct {
		name         string
		send         func() *httptest.ResponseRecorder
		code         int
		page         string
		store, reads int
	}
	for _, a := range []arm{
		{"GET, a malformed id", func() *httptest.ResponseRecorder { return g.get(vatPathOf(malformed), c) }, 404, "That link does not name a tenant", 1, 0},
		{"POST, a malformed id", func() *httptest.ResponseRecorder { return g.post(vatPathOf(malformed), url.Values{}, c) }, 404, "That link does not name a tenant", 1, 0},
		{"POST, cross-origin", func() *httptest.ResponseRecorder {
			return g.do(req{method: http.MethodPost, host: opHost, path: vatPathOf(id.String()), form: url.Values{}, origin: "https://taptime.mt",
				header: map[string]string{"Sec-Fetch-Site": "same-site"}, cookies: []*http.Cookie{c}})
		}, 403, "That request did not come from this site", 0, 0},
		{"POST, no cookie", func() *httptest.ResponseRecorder { return g.post(vatPathOf(id.String()), url.Values{}) }, 303, "", 0, 0},
		{"POST, an id no tenant has", func() *httptest.ResponseRecorder { return g.post(vatPathOf(uuid.NewString()), url.Values{}, c) }, 404,
			"There is no tenant with that id", 2, 1},
		{"POST, the read fails", func() *httptest.ResponseRecorder {
			fail("TenantVAT", errFakeDB)
			defer fail("TenantVAT", nil)
			return g.recheck(id, c)
		}, 503, "This tenant&#39;s VAT number could not be loaded", 2, 1},
		{"POST, the read's session is refused", func() *httptest.ResponseRecorder {
			fail("TenantVAT", db.ErrOperatorRefused)
			defer fail("TenantVAT", nil)
			return g.recheck(id, c)
		}, 303, "", 2, 1},
		{"GET, another tenant's answer", func() *httptest.ResponseRecorder { return g.get(vatPathOf(other.String()), c) }, 503,
			"This tenant&#39;s VAT number could not be loaded", 2, 1},
		{"POST, another tenant's answer", func() *httptest.ResponseRecorder { return g.recheck(other, c) }, 503,
			"This tenant&#39;s VAT number could not be loaded", 2, 1},
	} {
		total, reads, asked := g.store.total(), g.store.count("TenantVAT"), len(g.vies.askedFor())
		g.logs.Reset()
		w := a.send()
		if w.Code != a.code || (a.page != "" && !strings.Contains(w.Body.String(), a.page)) {
			t.Errorf("%s: %d, want %d with %q", a.name, w.Code, a.code, a.page)
		}
		if a.code == 303 && w.Result().Header.Get("Location") != "/operator/login" {
			t.Errorf("%s: redirected to %q, want the sign-in", a.name, w.Result().Header.Get("Location"))
		}
		// store counts the gate's predicate too: 1 for a request that passed the gate.
		if got := g.store.total() - total; got != a.store {
			t.Errorf("%s: %d store call(s), want %d", a.name, got, a.store)
		}
		if got := g.store.count("TenantVAT") - reads; got != a.reads {
			t.Errorf("%s: %d read(s), want %d", a.name, got, a.reads)
		}
		if got := len(g.vies.askedFor()) - asked; got != 0 {
			t.Errorf("%s: VIES asked %d time(s)", a.name, got)
		}
		if strings.Contains(w.Body.String(), "FAKE Someone Else Ltd") || strings.Contains(w.Body.String(), "IE8B76543Q") {
			t.Errorf("%s: another tenant's name or number reached the page", a.name)
		}
		if a.name == "POST, the read fails" && (!strings.Contains(g.logs.String(), "VAT number could not be read") || !strings.Contains(g.logs.String(), id.String())) {
			t.Errorf("%s: no log line naming the tenant", a.name)
		}
	}
	// The write's two refusals: VIES answered, nothing was recorded.
	for _, a := range []struct {
		name string
		err  error
		code int
		page string
	}{
		{"the write fails (22023)", errFakeVATUnbound, 503, "The re-check was not confirmed"},
		{"the write's session is refused", db.ErrOperatorRefused, 303, ""},
	} {
		fail("RecordTenantVATCheck", a.err)
		g.logs.Reset()
		asked := len(g.vies.askedFor())
		w := g.recheck(id, c)
		fail("RecordTenantVATCheck", nil)
		if w.Code != a.code || (a.page != "" && !strings.Contains(w.Body.String(), a.page)) || len(g.vies.askedFor()) != asked+1 {
			t.Errorf("%s: %d, want %d with %q after one VIES call", a.name, w.Code, a.code, a.page)
		}
		if a.err == errFakeVATUnbound && (!strings.Contains(g.logs.String(), "SQLSTATE 22023") || !strings.Contains(g.logs.String(), id.String()) ||
			!strings.Contains(g.logs.String(), "a VAT re-check could not be recorded")) {
			t.Errorf("%s: the log line does not name the tenant and the SQLSTATE", a.name)
		}
	}
	if n := len(g.vatWrites()); n != 0 {
		t.Errorf("the refusals made %d write(s)", n)
	}
}

// ------------------------------------------------------------------ the budgets --

// TestVATBudget_ARecheckIsThreeReadsAndOneVIESUnit measures surface.go's viesLimit (10 VIES
// lookups per session per window) beside readLimit (60 reads), on the VAT screen.
//
// PART I -- one session, from a fresh address per request: ten re-checks, each the screen
// (GET), the ask (POST, VIES confirms: 303) and the screen after it (the 303's GET) -- 30 x 2xx
// and 3xx, 30 reads, 10 VIES calls; the eleventh ask is 429 with the screen and the budget
// notice ("This session has asked VIES ten times in ten minutes. ..."), made its read (31), did
// not ask VIES (still 10) and wrote nothing; its WARN record names limit=10 period=10m0s, once
// (the window's first refusal). The two budgets are apart: the session then reads the screen 29
// times more -- 60 reads in all -- and the 61st read is 429 at the READ budget (the signed-in
// "Too many requests" page) with no read made. A second session of the same operator asks VIES
// at once (the budget is the session's, not the account's).
//
// PART II -- the counts above. PART III -- these sequences only (no completeness claim).
func TestVATBudget_ARecheckIsThreeReadsAndOneVIESUnit(t *testing.T) {
	g := newRig(t)
	id := g.seedVAT(uuid.Nil, "FAKE Budget Ltd", vatNumberOK, nil, nil)
	path := vatPathOf(id.String())
	f := g.active()
	c := g.signIn(f)
	n := 0
	send := func(r req) *httptest.ResponseRecorder {
		n++
		r.host = opHost
		r.remote = fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1)
		r.cookies = []*http.Cookie{c}
		if r.method == http.MethodGet {
			r.header = map[string]string{"Sec-Fetch-Site": "same-origin"}
		} else {
			r.origin, r.form = opOrigin, url.Values{}
		}
		return g.do(r)
	}
	g.vies.set(operator.VATAnswerValid)
	for i := 0; i < operator.VIESLimitForTest; i++ {
		if w := send(req{method: http.MethodGet, path: path}); w.Code != http.StatusOK {
			t.Fatalf("re-check %d: the screen = %d", i+1, w.Code)
		}
		w := send(req{method: http.MethodPost, path: path})
		if w.Code != http.StatusSeeOther {
			t.Fatalf("re-check %d: the ask = %d", i+1, w.Code)
		}
		if w := send(req{method: http.MethodGet, path: w.Result().Header.Get("Location")}); w.Code != http.StatusOK {
			t.Fatalf("re-check %d: the screen after it = %d", i+1, w.Code)
		}
	}
	if r, v := g.store.count("TenantVAT"), len(g.vies.askedFor()); r != 30 || v != 10 {
		t.Fatalf("ten re-checks made %d read(s) and %d VIES call(s), want 30 and 10", r, v)
	}
	g.logs.Reset()
	w := send(req{method: http.MethodPost, path: path})
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), sentenceVATBudgetNotice) ||
		!strings.Contains(w.Body.String(), "Too many VAT re-checks") {
		t.Fatalf("the eleventh ask = %d, want 429 with the screen and the budget notice", w.Code)
	}
	if r, v, wr := g.store.count("TenantVAT"), len(g.vies.askedFor()), len(g.vatWrites()); r != 31 || v != 10 || wr != 10 {
		t.Errorf("the eleventh ask: reads %d, VIES calls %d, writes %d; want 31, 10, 10", r, v, wr)
	}
	if k := strings.Count(g.logs.String(), "operator VIES budget reached"); k != 2 || !strings.Contains(g.logs.String(), "limit=10 period=10m0s") {
		t.Errorf("the VIES budget's refusal wrote %d line(s) across the two formats (want one record), or not limit=10 period=10m0s", k)
	}
	for i := 0; i < 29; i++ {
		if w := send(req{method: http.MethodGet, path: path}); w.Code != http.StatusOK {
			t.Fatalf("read %d after the VIES budget = %d, want 200 -- the two budgets are not apart", 32+i, w.Code)
		}
	}
	reads := g.store.count("TenantVAT")
	if w := send(req{method: http.MethodGet, path: path}); w.Code != http.StatusTooManyRequests || !signedInTooMany(w.Body.String()) ||
		g.store.count("TenantVAT") != reads {
		t.Errorf("the 61st read = %d with %d read(s) made, want 429 at the read budget with none", w.Code, g.store.count("TenantVAT")-reads)
	}
	c = g.signIn(f)
	if w := send(req{method: http.MethodPost, path: path}); w.Code != http.StatusSeeOther {
		t.Errorf("CONTROL: a new session of the same operator asks = %d, want 303", w.Code)
	}
}

// ------------------------------------------------------------- the number's way --

// vatForms are the forms of a VAT number a log line or a header would carry it in: as stored,
// lower case, inside %q, inside a JSON string, URL query-escaped, and -- for a number with eight
// characters after its country prefix -- those eight.
func vatForms(n string) map[string]string {
	q := strconv.Quote(n)
	j, _ := json.Marshal(n)
	out := map[string]string{"raw": n, "lower case": strings.ToLower(n), "%q": q[1 : len(q)-1], "JSON": string(j[1 : len(j)-1]),
		"query-escaped": url.QueryEscape(n)}
	if len(n) >= 10 {
		out["without its prefix"] = n[2:]
	}
	return out
}

// TestVATRecheck_TheNumberIsOnNoLogLineOrHeader (the orchestrator's K16-4; backlog T107): a
// tenant's VAT number is on no line of the process or access log and in no response header, on
// every branch of the screen.
//
// PART I -- a number made for the run (IE-shaped: a random digit, letter, five digits and
// letter), a second, shorter one VIES does not take, and a third made for the run the same way,
// held by a record the store reports as ANOTHER tenant's (a read that answers for another
// tenant than the path's) -- the three searched in the forms of vatForms, on the rig's log (the
// process log AND the access log, both shipped formats, at Debug) and on every header value at
// WriteHeader, of these fourteen arms: the screen; the re-check answered valid and invalid
// (303); VIES not answering (503, the screen); the read failing (503); the read's session
// refused; the write failing with 22023 (503); the write's session refused; an id no tenant has;
// the second tenant's screen and its re-check (422); the store answering for another tenant, on
// the GET and on the POST (503, VIES not asked); the VIES budget (429, the screen). The body is
// searched too, on the arms whose answer is NOT the screen (the redirects and the fault and
// refusal pages): the number is on the screen by design, and on nothing else. AND THE LINES'
// SHAPE, which a number search alone cannot see (an attribute that carries an EMPTY number --
// the other-tenant branch drops the row before any line is written): every JSON record of an
// arm whose message names VAT or VIES is one of the five lines vat.go writes (vatLines), with
// exactly that line's attributes besides time, level, msg and the request id.
// CONTROL: the number logged directly through a logger of the rig's shape is found in every form
// but lower case; a record of one of the five messages with an attribute too many, and a record
// of a sixth message naming VAT, are both reported by the shape check; the arms wrote lines of
// their own (the recorded answer's INFO, the no-answer WARN, the read's and the write's ERROR,
// the budget's WARN).
//
// PART II -- the arms above. PART III -- These arms and these forms only; the adapter
// (cmd/tappa's TestOperatorVIES_OnlyAVerdictBecomesAVerdict) and the VIES client
// (internal/domain/signup's TestVIESCheck_LogsNothingOnAnyPath) are their own links of the
// chain -- no completeness claim.
func TestVATRecheck_TheNumberIsOnNoLogLineOrHeader(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	b := randBytes(t, 5)
	number := fmt.Sprintf("IE%d%c%05d%c", b[0]%10, 'A'+rune(b[1]%26), (int(b[2])<<8|int(b[3]))%100000, 'A'+rune(b[4]%26))
	if len(number) != len(vatNumberOK) {
		t.Fatalf("PREMISE: the run's number %q is not IE-shaped", number)
	}
	oddNumber := "IE" + number[4:8]
	f := randBytes(t, 5)
	foreignNumber := fmt.Sprintf("IE%d%c%05d%c", f[0]%10, 'A'+rune(f[1]%26), (int(f[2])<<8|int(f[3]))%100000, 'A'+rune(f[4]%26))
	if foreignNumber == number || strings.Contains(foreignNumber, oddNumber[2:]) || strings.Contains(number, foreignNumber[2:]) {
		foreignNumber = "IE" + string('0'+rune((number[2]-'0'+1)%10)) + number[3:]
	}
	id := g.seedVAT(uuid.Nil, "FAKE Quiet Number Ltd", number, nil, nil)
	odd := g.seedVAT(uuid.Nil, "FAKE Quiet Odd Ltd", oddNumber, nil, nil)
	wrong := g.seedVAT(uuid.Nil, "FAKE Quiet Foreign Row Ltd", foreignNumber, nil, nil)
	g.store.mu.Lock()
	x := g.store.vats[wrong]
	x.asTenant = uuid.New()
	g.store.vats[wrong] = x
	g.store.mu.Unlock()
	fail := func(m string, err error) {
		g.store.mu.Lock()
		defer g.store.mu.Unlock()
		if err == nil {
			delete(g.store.fail, m)
		} else {
			g.store.fail[m] = err
		}
	}
	type arm struct {
		name      string
		code      int
		screen    bool // the answer is the VAT screen: the body carries the number by design
		drive     func() *httptest.ResponseRecorder
		wantLines []string
	}
	ask := func(a operator.VATAnswer) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { g.vies.set(a); return g.recheck(id, c) }
	}
	failing := func(m string, err error, a operator.VATAnswer) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			fail(m, err)
			defer fail(m, nil)
			return ask(a)()
		}
	}
	arms := []arm{
		{"the screen", 200, true, func() *httptest.ResponseRecorder { return g.get(vatPathOf(id.String()), c) }, nil},
		{"answered valid", 303, false, ask(operator.VATAnswerValid), []string{"operator recorded a VAT re-check"}},
		{"answered invalid", 303, false, ask(operator.VATAnswerInvalid), []string{"operator recorded a VAT re-check"}},
		{"VIES did not answer", 503, true, ask(operator.VATAnswerUnknown), []string{"VIES did not answer a VAT re-check"}},
		{"the read fails", 503, false, failing("TenantVAT", errFakeDB, operator.VATAnswerValid), []string{"VAT number could not be read"}},
		{"the read's session is refused", 303, false, failing("TenantVAT", db.ErrOperatorRefused, operator.VATAnswerValid), nil},
		{"the write fails (22023)", 503, false, failing("RecordTenantVATCheck", errFakeVATUnbound, operator.VATAnswerValid),
			[]string{"a VAT re-check could not be recorded", "SQLSTATE 22023"}},
		{"the write's session is refused", 303, false, failing("RecordTenantVATCheck", db.ErrOperatorRefused, operator.VATAnswerInvalid), nil},
		{"an id no tenant has", 404, false, func() *httptest.ResponseRecorder { return g.post(vatPathOf(uuid.NewString()), url.Values{}, c) }, nil},
		{"the odd number's screen", 200, true, func() *httptest.ResponseRecorder { return g.get(vatPathOf(odd.String()), c) }, nil},
		{"the odd number's re-check", 422, true, func() *httptest.ResponseRecorder { return g.recheck(odd, c) }, nil},
		// The store answers with another tenant's row: a 503 that is not the screen, so the
		// body is searched as well -- neither tenant's number may be anywhere.
		{"GET, the store answers for another tenant", 503, false, func() *httptest.ResponseRecorder { return g.get(vatPathOf(wrong.String()), c) },
			[]string{"VAT number could not be read"}},
		{"POST, the store answers for another tenant", 503, false, func() *httptest.ResponseRecorder { return g.recheck(wrong, c) },
			[]string{"VAT number could not be read"}},
		// By here the session has asked VIES five times (valid, invalid, no answer, and the two
		// write arms); the arm spends five more, unanswered, and the eleventh is the refusal.
		{"the VIES budget", 429, true, func() *httptest.ResponseRecorder {
			for i := 0; i < operator.VIESLimitForTest-5; i++ {
				ask(operator.VATAnswerUnknown)()
			}
			g.logs.Reset()
			return ask(operator.VATAnswerValid)()
		}, []string{"operator VIES budget reached"}},
	}
	for _, a := range arms {
		g.logs.Reset()
		w := a.drive()
		if w.Code != a.code {
			t.Errorf("PREMISE: %s = %d, want %d -- the arm is not the branch it names", a.name, w.Code, a.code)
		}
		logs := g.logs.String()
		var headers strings.Builder
		for k, vs := range w.Result().Header {
			headers.WriteString(k + ": " + strings.Join(vs, "\n") + "\n")
		}
		for _, n := range []string{number, oddNumber, foreignNumber} {
			for form, v := range vatForms(n) {
				if strings.Contains(logs, v) {
					t.Errorf("%s: the log carries a number (%s)", a.name, form)
				}
				if strings.Contains(headers.String(), v) {
					t.Errorf("%s: a response header carries a number (%s)", a.name, form)
				}
				if !a.screen && strings.Contains(w.Body.String(), v) {
					t.Errorf("%s: a page that is not the VAT screen carries a number (%s)", a.name, form)
				}
			}
		}
		for _, line := range a.wantLines {
			if !strings.Contains(logs, line) {
				t.Errorf("PREMISE: %s wrote no %q line -- the arm is not the branch it names", a.name, line)
			}
		}
		for _, p := range vatLineShapes(logs) {
			t.Errorf("%s: %s", a.name, p)
		}
	}
	// CONTROL: the search finds the number when it IS logged, through a logger of the rig's
	// shape (both shipped formats, Debug).
	var control strings.Builder
	captureAt(&control, slog.LevelDebug).Info("control", "n", number)
	for form, v := range vatForms(number) {
		if form != "lower case" && !strings.Contains(control.String(), v) {
			t.Errorf("CONTROL: the number logged directly is not found in the %s form", form)
		}
	}
	// CONTROL: the shape check reports an attribute too many -- even an empty one -- and a line
	// vat.go does not write.
	var extra, sixth strings.Builder
	captureAt(&extra, slog.LevelDebug).Error("operator: a tenant's VAT number could not be read", "tenant_id", id.String(), "err", errFakeDB, "n", "")
	captureAt(&sixth, slog.LevelDebug).Warn("operator: a VAT read answered for another tenant", "tenant_id", id.String())
	if len(vatLineShapes(extra.String())) != 1 || len(vatLineShapes(sixth.String())) != 1 {
		t.Errorf("CONTROL: the shape check reports %d problem(s) for an attribute too many and %d for a sixth line, want 1 and 1",
			len(vatLineShapes(extra.String())), len(vatLineShapes(sixth.String())))
	}
}

// vatLines are the five lines vat.go writes, by message, with the attributes each carries
// besides the record's time, level and msg and the request id httpx.WithRequestID adds.
var vatLines = map[string][]string{
	"operator recorded a VAT re-check":                                   {"admin_id", "answer", "tenant_id"},
	"operator: VIES did not answer a VAT re-check; nothing was recorded": {"tenant_id"},
	"operator: a tenant's VAT number could not be read":                  {"err", "tenant_id"},
	"operator: a VAT re-check could not be recorded":                     {"err", "tenant_id"},
	"operator VIES budget reached":                                       {"limit", "period", "session_id"},
}

// vatLineShapes reads the JSON records of logs and reports each whose message names VAT or VIES
// and is not one of vatLines' five, or is one of them with other attributes than its own.
func vatLineShapes(logs string) []string {
	var out []string
	for _, line := range strings.Split(logs, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			out = append(out, fmt.Sprintf("a JSON record does not parse (%v)", err))
			continue
		}
		msg, _ := rec["msg"].(string)
		if !strings.Contains(msg, "VAT") && !strings.Contains(msg, "VIES") {
			continue
		}
		want, ok := vatLines[msg]
		if !ok {
			out = append(out, fmt.Sprintf("a line vat.go does not write names VAT or VIES: %q", msg))
			continue
		}
		var keys []string
		for k := range rec {
			if k != slog.TimeKey && k != slog.LevelKey && k != slog.MessageKey && k != httpx.LogRequestIDKey {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		if !slices.Equal(keys, want) {
			out = append(out, fmt.Sprintf("the %q line carries %v, want %v", msg, keys, want))
		}
	}
	return out
}

// ------------------------------------------------------------- the four states --

// vatStatus is one shape of the two columns.
func vatStatus(verified *bool, checkedAt *time.Time) db.TenantVATStatus {
	return db.TenantVATStatus{TenantID: uuid.New(), TenantName: "FAKE", Number: vatNumberOK, Verified: verified, CheckedAt: checkedAt}
}

// TestVATScreen_TheFourStatesAreTheAccountScreensFour: the screen's partition of the two
// columns into migration 00017's four states is the customer's account screen's --
// internal/domain/tenant's VATCheck.State -- on every shape of the pair (no time, a time; no
// verdict, true, false: six shapes, the two the product does not write included).
func TestVATScreen_TheFourStatesAreTheAccountScreensFour(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for _, v := range []*bool{nil, ptr(true), ptr(false)} {
		for _, c := range []*time.Time{nil, &at} {
			got := operator.VATStateForTest(vatStatus(v, c))
			want := string(tenant.VATCheck{Verified: v, CheckedAt: c}.State())
			if got != want {
				t.Errorf("verified %v, checked %v: the operator screen reads %q, the account screen %q", v, c, got, want)
			}
			seen[got] = true
		}
	}
	if len(seen) != 4 {
		t.Errorf("PREMISE: six shapes gave %d state(s), want the four", len(seen))
	}
}

// accountVATWords reads internal/handler/account.go's fillAccountVAT: for each case clause of
// its switch, the tenant.VATState constant (by name) or "default", and the string literal
// assigned to v.VATStateWord in it.
func accountVATWords(t *testing.T) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "account.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		fd, ok := n.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "fillAccountVAT" {
			return true
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			key := "default"
			if len(cc.List) == 1 {
				if sel, ok := cc.List[0].(*ast.SelectorExpr); ok {
					key = sel.Sel.Name
				}
			}
			for _, st := range cc.Body {
				as, ok := st.(*ast.AssignStmt)
				if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
					continue
				}
				if sel, ok := as.Lhs[0].(*ast.SelectorExpr); ok && sel.Sel.Name == "VATStateWord" {
					if lit, ok := as.Rhs[0].(*ast.BasicLit); ok {
						if s, err := strconv.Unquote(lit.Value); err == nil {
							out[key] = s
						}
					}
				}
			}
			return false
		})
		return false
	})
	return out
}

// TestVATScreen_TheFourStatesAreCalledWhatTheAccountScreenCallsThem: an operator on the phone
// with a business reads the word the business reads. The labels of vat.go's four states are the
// literals internal/handler/account.go's fillAccountVAT assigns for the same states --
// VATValid, VATInvalid, VATNoAnswer, and its default branch (never checked); the "Not checked"
// sentence names the sign-up cohort the way the account screen does ("could not reach the
// register", "cannot tell them apart" -- TestAccount_TheNoAnswerCohortIsCalledTheSameThingOnBothScreens).
// CONTROL: the reading of account.go found four words.
func TestVATScreen_TheFourStatesAreCalledWhatTheAccountScreenCallsThem(t *testing.T) {
	account := accountVATWords(t)
	if len(account) != 4 {
		t.Fatalf("CONTROL: read %d word(s) off fillAccountVAT (%v), want four", len(account), account)
	}
	words := operator.VATWordsForTest()
	for state, clause := range map[string]string{"valid": "VATValid", "invalid": "VATInvalid", "no-answer": "VATNoAnswer", "never-checked": "default"} {
		if words[state].Label != account[clause] {
			t.Errorf("%s: the operator screen says %q, the account screen %q", state, words[state].Label, account[clause])
		}
	}
	nc := words["never-checked"].Sentence
	if !strings.Contains(nc, "could not reach the register") || !strings.Contains(nc, "cannot tell them apart") {
		t.Errorf("the never-checked sentence does not name the sign-up cohort as the account screen does: %q", nc)
	}
}

// TestVATScreen_EveryStateSaysItsWordAndSentence: each of the four states through the shipped
// handler. PART I -- the banner and the title carry the tenant's name ("VAT number — <name> —
// Taptime operator"), escaped; the number is on the page in the data face; the chip is the
// state's word in the state's tally class; the state's sentence; "Last answer <UTC minute>"
// beside a verdict and "Asked <UTC minute>" beside no answer (the security audit's D2: an ask
// VIES did not answer writes nothing, so beside a verdict the time is the answer's) for a
// stored time given at UTC+2 (the conversion is the render's), "No time of asking is on file."
// with none; the form; the ways back to the overview and the tenants. The overview links
// the screen as "See this tenant's VAT number" and shows no number itself. PART II -- the list
// above. PART III -- no completeness claim.
func TestVATScreen_EveryStateSaysItsWordAndSentence(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	at := time.Date(2026, 9, 30, 11, 30, 0, 0, time.FixedZone("UTC+2", 2*3600))
	words := operator.VATWordsForTest()
	// The two verdicts' sentences, written out (the security audit's D2): a verdict is dated by
	// VIES's last ANSWER -- an ask it did not answer writes nothing -- never by its last ask.
	for state, want := range map[string]string{
		"valid":   "VIES confirmed this number when it last answered.",
		"invalid": "VIES did not recognise this number when it last answered. Nothing in the product depends on it; an invoice to a number the register does not know is a matter for the business's accountant.",
	} {
		if got := words[state].Sentence; got != want {
			t.Errorf("the %s sentence is %q, want %q", state, got, want)
		}
	}
	for state, w := range words {
		if strings.Contains(w.Sentence, "last asked") {
			t.Errorf("the %s sentence dates the record by its last ask: %q", state, w.Sentence)
		}
	}
	const name = `Rusty <Bar> & "Grill"`
	for _, s := range []struct {
		state, class string
		verified     *bool
		checkedAt    *time.Time
	}{
		{"never-checked", "tally--vat-not-checked", nil, nil},
		{"no-answer", "tally--vat-no-answer", nil, &at},
		{"valid", "tally--vat-confirmed", ptr(true), &at},
		{"invalid", "tally--vat-not-found", ptr(false), &at},
	} {
		id := g.seedVAT(uuid.Nil, name, vatNumberOK, s.verified, s.checkedAt)
		w := g.get(vatPathOf(id.String()), c)
		body := w.Body.String()
		esc := html.EscapeString(name)
		banner := strings.Index(body, `<section class="op-tenant" aria-label="Tenant">`)
		if w.Code != http.StatusOK || banner < 0 || !strings.Contains(body[banner:], "<bdi>"+esc+"</bdi>") ||
			!strings.Contains(body, "<title>VAT number — "+esc+" — Taptime operator</title>") {
			t.Fatalf("%s: %d, or the banner or the title does not carry the escaped name", s.state, w.Code)
		}
		want := []string{
			`<dd class="mt-1 break-all font-mono text-lg font-bold">` + vatNumberOK + `</dd>`,
			`<span class="tally ` + s.class + `">` + words[s.state].Label + `</span>`,
			html.EscapeString(words[s.state].Sentence),
			vatForm(id.String()),
			`<a href="/operator/tenants/` + id.String() + `" class="op-link">Back to the overview</a>`,
			`<a href="/operator/tenants" class="op-link">Back to the tenants</a>`,
		}
		switch {
		case s.checkedAt != nil && s.verified != nil:
			want = append(want, `Last answer <span class="font-mono">2026-09-30 09:30 UTC</span>`)
		case s.checkedAt != nil:
			want = append(want, `Asked <span class="font-mono">2026-09-30 09:30 UTC</span>`)
		default:
			want = append(want, "No time of asking is on file.")
		}
		for _, x := range want {
			if !strings.Contains(body, x) {
				t.Errorf("%s: the screen lacks %q", s.state, x)
			}
		}
		if strings.Contains(body, "Last asked") || strings.Contains(body, "Last answer") != (s.checkedAt != nil && s.verified != nil) {
			t.Errorf("%s: the time line does not follow the stored time and verdict", s.state)
		}
	}
	id := g.seedVAT(uuid.Nil, name, vatNumberOK, nil, nil)
	g.seedOverview(db.TenantOverview{ID: id, Name: name, Plan: "founding", BusinessType: "bar"})
	w := g.get("/operator/tenants/"+id.String(), c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<a href="`+vatPathOf(id.String())+`" class="op-link">See this tenant's VAT number</a>`) ||
		strings.Contains(w.Body.String(), vatNumberOK) {
		t.Errorf("the overview = %d; it does not link the VAT screen, or it shows the number itself", w.Code)
	}
}

// TestVATScreen_TheChipsAndTextClearAA recomputes, from tailwind.config.js's palette, the
// contrast of the VAT screen's chips: ink on green-lite (confirmed), on tomato at 10% over paper
// (not found), on saffron-lite (no answer), on line at 10% over paper (not checked) and on ink at
// 10% (an unset tone) -- each against AA's 4.5:1 (the chips' words are 11px). No ground is new.
func TestVATScreen_TheChipsAndTextClearAA(t *testing.T) {
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
		{"ink on green-lite (confirmed)", ink, hexOf("green-lite")},
		{"ink on tomato at 10% over paper (not found)", ink, over(hexOf("tomato"), 0.1, paper)},
		{"ink on saffron-lite (no answer)", ink, hexOf("saffron-lite")},
		{"ink on line at 10% over paper (not checked)", ink, over(hexOf("line"), 0.1, paper)},
		{"ink on ink at 10% over paper (an unset tone)", ink, over(ink, 0.1, paper)},
	} {
		if got := contrast(c.fg, c.bg); got < 4.5 {
			t.Errorf("%s = %.2f:1, want at least 4.5:1", c.what, got)
		} else {
			t.Logf("%s = %.2f:1", c.what, got)
		}
	}
}

// TestVATScreen_EachChipHasTheRuleTheContrastTestComputes ties the chips the screen writes to
// the grounds above: for each of the five tones the chip switch renders (read off a render of
// every tone), web/static/css/input.css has exactly one rule whose selector list names its
// class, and that rule's @apply names the ground and the frame of its tone and nothing else.
// PART II -- red on: a tone class with no rule, with two rules, or with another ground, frame or
// a third utility; two tones drawing one class. PART III -- a rule outside the one-line @apply
// shape this reads is code review's -- no completeness claim.
func TestVATScreen_EachChipHasTheRuleTheContrastTestComputes(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[operatorpages.VATTone][2]string{ // ground, frame
		operatorpages.VATToneConfirmed:  {"bg-green-lite", "border-tappa-green"},
		operatorpages.VATToneNotFound:   {"bg-tomato/10", "border-tomato"},
		operatorpages.VATToneNoAnswer:   {"bg-saffron-lite", "border-saffron"},
		operatorpages.VATToneNotChecked: {"bg-line/10", "border-line"},
		operatorpages.VATToneUnknown:    {"bg-ink/10", "border-ink"},
	}
	chipRe := regexp.MustCompile(`<span class="tally (tally--[a-z-]+)">`)
	ruleRe := regexp.MustCompile(`(?m)((?:^\s*\.[a-z-]+,\s*\n)*^\s*\.[a-z-]+\s*)\{\s*@apply ([^;]+);\s*\}`)
	rules := ruleRe.FindAllStringSubmatch(string(css), -1)
	if len(rules) < 10 {
		t.Fatalf("PREMISE: %d one-line @apply rule(s) read from input.css", len(rules))
	}
	classes := map[string]bool{}
	for tone, w := range want {
		var b strings.Builder
		if err := operatorpages.TenantVAT(operatorpages.TenantVATView{Name: mustName(t, "FAKE Tone Ltd"), Label: "L", Sentence: "S",
			Tone: tone}).Render(t.Context(), &b); err != nil {
			t.Fatal(err)
		}
		m := chipRe.FindAllStringSubmatch(b.String(), -1)
		if len(m) != 1 {
			t.Fatalf("tone %d: %d chip(s) rendered", tone, len(m))
		}
		class := m[0][1]
		if classes[class] {
			t.Errorf("two tones draw %s", class)
		}
		classes[class] = true
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

// ifaceMethod is interface typ's method name in p.
func ifaceMethod(t *testing.T, p *types.Package, typ, name string) *types.Func {
	t.Helper()
	tn, ok := p.Scope().Lookup(typ).(*types.TypeName)
	if !ok {
		t.Fatalf("PREMISE: %s declares no type %s", p.Path(), typ)
	}
	it, ok := tn.Type().Underlying().(*types.Interface)
	if !ok {
		t.Fatalf("PREMISE: %s.%s is not an interface", p.Path(), typ)
	}
	for i := 0; i < it.NumMethods(); i++ {
		if it.Method(i).Name() == name {
			return it.Method(i)
		}
	}
	t.Fatalf("PREMISE: %s.%s has no method %s", p.Path(), typ, name)
	return nil
}

// TestVATNumber_TheListedSitesAloneReadIt (M10 OP-16 B; K16-1, K16-4, K16-6).
//
// PART I -- the shipped package: the field db.TenantVATStatus.Number is used at exactly five
// sites -- VN1a the argument of signup.ValidVATFormat in (*Surface).recheckVAT and VN1b in
// (*Surface).renderVAT, VN1c the second argument of operator.VATChecker's CheckVAT in
// recheckVAT, VN1d the fourth argument of operator.VATStore's RecordTenantVATCheck in
// recheckVAT, VN1e the value of the Number key of an operatorpages.TenantVATView literal in
// renderVAT; CheckVAT and RecordTenantVATCheck are called once each, both in recheckVAT, and the
// write's context argument is an identifier recheckVAT defines from context.WithTimeout over
// context.WithoutCancel; of internal/domain/signup the package uses ValidVATFormat and nothing
// else; the four VAT functions (tenantVAT, recheckVAT, readVAT, renderVAT) call neither readForm
// nor postValue. CONTROL: the Number field resolves at five uses, CheckVAT and
// RecordTenantVATCheck at one call each, ValidVATFormat at two.
//
// PART II -- red on: VN1 a use of db.TenantVATStatus.Number at any other site (a log
// attribute, a format call, a concatenation, a second VIES call); VN2 CheckVAT or
// RecordTenantVATCheck called other than once, or outside recheckVAT; VN3 the write's context
// not the detached one; VN4 a use of any object of internal/domain/signup but ValidVATFormat (a
// NewChecker, a Checker, its VATStatus); VN5 readForm or postValue called in one of the four VAT
// functions.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: the
// number copied into another variable first, a reader in another package) is code review's --
// no completeness claim.
func TestVATNumber_TheListedSitesAloneReadIt(t *testing.T) {
	tp := typedOperator(t)
	dbp := tp.imported(t, "github.com/atknatk/tappa/internal/db")
	sp := tp.imported(t, "github.com/atknatk/tappa/internal/domain/signup")
	op := tp.imported(t, operatorpagesPkgPath)
	number := lookupMember(t, dbp, "TenantVATStatus", "Number")
	validFormat := lookupFunc(t, sp, "ValidVATFormat")
	checkVAT := ifaceMethod(t, tp.pkg, "VATChecker", "CheckVAT")
	record := ifaceMethod(t, tp.pkg, "VATStore", "RecordTenantVATCheck")
	recheck := tp.method(t, "Surface", "recheckVAT")
	render := tp.method(t, "Surface", "renderVAT")
	vatFuncs := []*types.Func{tp.method(t, "Surface", "tenantVAT"), recheck, tp.method(t, "Surface", "readVAT"), render}
	view, ok := op.Scope().Lookup("TenantVATView").(*types.TypeName)
	if !ok {
		t.Fatal("PREMISE: operatorpages declares no TenantVATView")
	}
	var bad []string
	sites := map[string]int{}
	for _, id := range tp.usesOf(number) {
		sel, ok := tp.parents[id].(*ast.SelectorExpr)
		if !ok {
			bad = append(bad, "VN1 the Number field used other than as a selector at "+tp.where(id.Pos()))
			continue
		}
		site := ""
		if call, i := tp.argOf(sel); call != nil {
			switch callee := tp.callee(call); {
			case callee == validFormat && i == 0 && tp.in(recheck, sel.Pos()):
				site = "VN1a"
			case callee == validFormat && i == 0 && tp.in(render, sel.Pos()):
				site = "VN1b"
			case callee == checkVAT && i == 1 && tp.in(recheck, sel.Pos()):
				site = "VN1c"
			case callee == record && i == 3 && tp.in(recheck, sel.Pos()):
				site = "VN1d"
			}
		} else if kv, ok := tp.parents[sel].(*ast.KeyValueExpr); ok && kv.Value == sel {
			if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Number" && tp.in(render, sel.Pos()) {
				if lit, ok := tp.parents[kv].(*ast.CompositeLit); ok && isNamed(tp.info.Types[lit].Type, view) {
					site = "VN1e"
				}
			}
		}
		if site == "" {
			bad = append(bad, "VN1 the Number field at "+tp.where(id.Pos()))
			continue
		}
		sites[site]++
	}
	for _, s := range []string{"VN1a", "VN1b", "VN1c", "VN1d", "VN1e"} {
		if sites[s] != 1 {
			bad = append(bad, fmt.Sprintf("VN1 site %s used %d time(s), want 1", s, sites[s]))
		}
	}
	for _, c := range []struct {
		name string
		fn   *types.Func
	}{{"CheckVAT", checkVAT}, {"RecordTenantVATCheck", record}} {
		n := 0
		for _, id := range tp.usesOf(c.fn) {
			n++
			call := tp.callOf(id)
			if call == nil || !tp.in(recheck, id.Pos()) {
				bad = append(bad, "VN2 "+c.name+" used at "+tp.where(id.Pos()))
				continue
			}
			if c.fn != record {
				continue
			}
			// VN3: the write's context is detached.
			ctxID, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
			detached := false
			if ok {
				def := tp.info.Uses[ctxID]
				ast.Inspect(tp.decl(id.Pos()), func(n ast.Node) bool {
					as, ok := n.(*ast.AssignStmt)
					if !ok || len(as.Rhs) != 1 {
						return true
					}
					for _, l := range as.Lhs {
						if li, ok := l.(*ast.Ident); ok && tp.info.Defs[li] != nil && tp.info.Defs[li] == def {
							if outer, ok := as.Rhs[0].(*ast.CallExpr); ok && qualSel(outer.Fun) == "context.WithTimeout" && len(outer.Args) == 2 {
								if inner, ok := outer.Args[0].(*ast.CallExpr); ok && qualSel(inner.Fun) == "context.WithoutCancel" {
									detached = true
								}
							}
						}
					}
					return true
				})
			}
			if !detached {
				bad = append(bad, "VN3 the write's context is not context.WithTimeout(context.WithoutCancel(...)) at "+tp.where(id.Pos()))
			}
		}
		if n != 1 {
			bad = append(bad, fmt.Sprintf("VN2 %s used %d time(s), want 1", c.name, n))
		}
	}
	formats := 0
	for id, obj := range tp.info.Uses {
		if obj == nil || obj.Pkg() != sp {
			continue
		}
		if obj != validFormat {
			bad = append(bad, "VN4 internal/domain/signup's "+obj.Name()+" at "+tp.where(id.Pos()))
			continue
		}
		formats++
	}
	readForm, postValue := tp.method(t, "Surface", "readForm"), lookupFunc(t, tp.pkg, "postValue")
	for _, f := range []types.Object{readForm, postValue} {
		for _, id := range tp.usesOf(f) {
			for _, fn := range vatFuncs {
				if tp.in(fn, id.Pos()) {
					bad = append(bad, "VN5 "+f.Name()+" in "+funcName(fn)+" at "+tp.where(id.Pos()))
				}
			}
		}
	}
	slices.Sort(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if len(tp.usesOf(number)) != 5 || formats != 2 {
		t.Fatalf("CONTROL: the Number field resolves at %d use(s), ValidVATFormat at %d; want 5 and 2", len(tp.usesOf(number)), formats)
	}
}

// qualSel is a call's function expression as pkg.Name, or "".
func qualSel(e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			return id.Name + "." + sel.Sel.Name
		}
	}
	return ""
}
