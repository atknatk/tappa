package operator_test

// op14_test.go -- M10 OP-14 phase B: /operator/audit on the shipped router with the fake
// store (rig_test.go). The header table of its response classes (C104-C121), the screen's
// words (every kind its word, an unknown kind, scope, class, filter or slug kept visible as
// unrecognised), what a row says and does not say (no hash, no detail, no full session id),
// the boundary's refusals before the store, the URL read for nothing, the pager, the
// escaping of what operators and tenants named, the contrast of the screen's text, and the
// read budget the audit log shares with the other screens. Against PostgreSQL:
// op14_db_test.go.

import (
	"fmt"
	"go/constant"
	"go/types"
	"html"
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
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// seedAudit puts entries into the fake's log, after what it holds (the log is newest first:
// a test seeds them in that order, all dated before fakeAuditStart).
func (g *rig) seedAudit(entries ...db.OperatorAuditEntry) {
	g.t.Helper()
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	g.store.audit = append(g.store.audit, entries...)
}

// auditAsks is what the fake was asked by the audit screen, in order.
func (g *rig) auditAsks() []db.OperatorAuditQuery {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return append([]db.OperatorAuditQuery(nil), g.store.auditAsks...)
}

// auditForm is the audit screen's form: the kind and the page.
func auditForm(kind, page string) url.Values { return url.Values{"kind": {kind}, "page": {page}} }

// auditEntry is one fake log row of kind, at second sec of 11:30 at UTC+2 (09:30 UTC), so a
// render that skips the conversion to UTC prints 11:30. A pre-session kind has no session
// and no actor; every other kind is done under session by actor.
func auditEntry(kind string, sec int, session, actor *uuid.UUID, actorName *string) db.OperatorAuditEntry {
	return db.OperatorAuditEntry{ID: uuid.New(), At: time.Date(2026, 10, 7, 11, 30, sec, 0, time.FixedZone("UTC+2", 2*3600)),
		Kind: kind, SessionID: session, ActorID: actor, ActorName: actorName, DetailRecognised: true}
}

// auditRowRe is one entry of the audit screen.
var auditRowRe = regexp.MustCompile(`(?s)<li class="op-row">(.*?)</li>`)

// unrecognisedChip is the chip the screen draws beside a value it has no word for.
const unrecognisedChip = `<span class="tally tally--unrecognised">Unrecognised</span>`

// TestOperatorHeaders_TheAuditClassesCarryThePolicy drives the eighteen response classes of
// the audit screen (C104-C121) with the 40-class test's hostile drive and checks
// (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these eighteen, at WriteHeader (the recorder's Result().Header): the
// designed status, route, header names and values (designedHeaders: no Location but the
// sign-in's on any of them); no hostile value (hostileValues) in a header value or in the
// body, raw or query-escaped -- every one's last request carries the hostile query, which
// since OP-14 holds a kind and a page under the form's own names (hostileDrive adds it on
// /operator/audit; C115's hand-built request gets it from hostileOn), so a GET or a POST
// that read its kind or page from the URL, or echoed one, is red here; no script; and the
// store counts below: C113 (cross-origin) and C121 (PUT) make no store call; C110, C114,
// C115, C116, C117 and C120 make no OperatorAudit call.
//
// PART II -- the list above, on these eighteen classes.
//
// PART III -- This test measures the eighteen classes and the raw and query-escaped forms
// only; anything else (examples: a class a later handler adds, a hostile value echoed
// HTML-escaped or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_TheAuditClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	g.seedAudit(auditEntry("unknown_email", 1, nil, nil, nil))
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
	view := func(c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: "/operator/audit", cookies: c, header: sfs}
	}
	filter := func(form url.Values, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: "/operator/audit", form: form, origin: opOrigin, cookies: c}
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
	failing := func(err error, r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			rq := r()
			g.store.mu.Lock()
			g.store.fail["OperatorAudit"] = err
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); delete(g.store.fail, "OperatorAudit"); g.store.mu.Unlock() }()
			return send(rq)
		}
	}
	once := func(r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return send(r()) }
	}
	calls := map[string]map[string]int{}
	counted := func(class string, drive func() *httptest.ResponseRecorder) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			before, total := g.store.count("OperatorAudit"), g.store.total()
			w := drive()
			calls[class] = map[string]int{"OperatorAudit": g.store.count("OperatorAudit") - before, "all": g.store.total() - total}
			return w
		}
	}
	// spent is a session with its 60 reads spent on views of the log (readLimit): the next
	// read passes the gate (its 61st unit of 100) and is refused by the read budget.
	spent := func() *http.Cookie {
		t.Helper()
		c := signIn()
		for i := 0; i < 60; i++ {
			if w := send(view(c)); w.Code != http.StatusOK {
				t.Fatalf("PREMISE: audit view %d = %d", i+1, w.Code)
			}
		}
		return c
	}
	dead := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}
	classes := []headerClass{
		{"C104 audit log", 200, once(func() req { return view(signIn()) })},
		{"C105 audit log without a cookie", 303, once(func() req { return view() })},
		{"C106 audit log with a dead cookie", 303, once(func() req { return view(dead) })},
		{"C107 audit log, a same-site read", 303, once(func() req {
			r := view(signIn())
			r.header = map[string]string{"Sec-Fetch-Site": "same-site"}
			return r
		})},
		{"C108 audit log, the read fails", 503, failing(errFakeDB, func() req { return view(signIn()) })},
		{"C109 audit log, the read's session is refused", 303, failing(db.ErrOperatorRefused, func() req { return view(signIn()) })},
		{"C110 audit log, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spent()
			return counted("C110", once(func() req { return view(c) }))()
		}},
		{"C111 audit filter", 200, once(func() req { return filter(auditForm("login", ""), signIn()) })},
		{"C112 audit filter without a cookie", 303, once(func() req { return filter(auditForm("login", "")) })},
		{"C113 audit filter, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C113", func() *httptest.ResponseRecorder {
				r := filter(auditForm("login", ""), c)
				r.origin, r.header = "https://taptime.mt", map[string]string{"Sec-Fetch-Site": "same-site"}
				return send(r)
			})()
		}},
		{"C114 audit filter, an oversized form", 413, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C114", once(func() req { return filter(auditForm(strings.Repeat("x", 20<<10), ""), c) }))()
		}},
		{"C115 audit filter, an unreadable form", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C115", func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/audit", strings.NewReader("kind=%zz"))
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
		{"C116 audit filter, a kind refused", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C116", once(func() req { return filter(auditForm("zz_no_such_kind", ""), c) }))()
		}},
		{"C117 audit filter, a page refused", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C117", once(func() req { return filter(auditForm("", "1001"), c) }))()
		}},
		{"C118 audit filter, the read fails", 503, failing(errFakeDB, func() req { return filter(auditForm("read", "2"), signIn()) })},
		{"C119 audit filter, the read's session is refused", 303,
			failing(db.ErrOperatorRefused, func() req { return filter(auditForm("read", ""), signIn()) })},
		{"C120 audit filter, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spent()
			return counted("C120", once(func() req { return filter(auditForm("login", ""), c) }))()
		}},
		{"C121 PUT on the audit log", 405, counted("C121", once(func() req {
			return req{method: http.MethodPut, path: "/operator/audit", origin: opOrigin, form: url.Values{}}
		}))},
	}
	if len(classes) != 18 {
		t.Fatalf("PREMISE: %d classes, the comment says 18", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(104+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 104+i, c.name, 104+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	for class, want := range map[string]map[string]int{
		"C110": {"OperatorAudit": 0}, "C113": {"all": 0}, "C114": {"OperatorAudit": 0}, "C115": {"OperatorAudit": 0},
		"C116": {"OperatorAudit": 0}, "C117": {"OperatorAudit": 0}, "C120": {"OperatorAudit": 0}, "C121": {"all": 0},
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

// TestAuditScreen_EveryRowSaysWhatTheLogHolds: the audit screen's reading of each row,
// through the shipped handler (audit.go's words and operatorpages' row).
//
// PART I -- a log of twenty-six rows below the view's own, each dated at UTC+2 11:30:xx:
// the page lists twenty-seven rows, the view's own first ("Read", "Of this audit log",
// "Filter: Every kind", page 1 of 50 per page, the viewing session's first eight digits),
// then the twenty-six in the log's order, each opening with its time in UTC to the second
// in the data face and then its kind's word; each row carries the facts written in its
// case below and none of the facts listed against it: the operator who acted by name, or
// "Before sign-in", or an unnamed operator by id; the account a pre-session row names --
// a pending and a disabled account's names among them (the definer reads every status; the
// screen prints what it is given) -- or an unnamed one by id; the tenant a read named, by
// name and linked to its overview, or unnamed by id and linked, or an id no tenant has with
// no link; the scope's word -- 00032's tenant_billing among them, "Of a tenant's billing"
// (3rd round) --, or "a scope this screen does not show"; a read's page; the
// search class's, the filter's and the slug's word with the byte count; "Detail not shown"
// for a detail the log did not recognise. Kinds, scopes, classes, filters and slugs the
// screen has no word for are drawn as unrecognised: the raw value in the data face when it
// is a lower-case token, nothing printed otherwise (a kind of markup, white space and a
// capital: not on the page at all). A search class the screen does not know is labelled
// "Search:" -- with its raw value or, unprintable, the chip alone -- and a known one is not
// (2nd round: a bare chip named no fact). The page carries no 11:30, no full session id (the
// eight-digit prefix only) and no script.
//
// PART II -- the list above. PART III -- These rows and these strings only (no completeness
// claim).
func TestAuditScreen_EveryRowSaysWhatTheLogHolds(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	s1, a1, a2, a3, a4, a5 := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	t1, t2, t3 := uuid.New(), uuid.New(), uuid.New()
	ops, pending, blank, disabled := "FAKE Ops One", "FAKE Ops Pending", "\u2060 ", "FAKE Ops Disabled"
	tenant := `FAKE <b>Kebab</b> & "Co"`
	read := func(sec int, scope *string) db.OperatorAuditEntry {
		e := auditEntry("read", sec, &s1, &a1, &ops)
		e.Scope = scope
		return e
	}
	type want struct {
		has, hasNot []string
	}
	var log []db.OperatorAuditEntry
	var wants []want
	add := func(e db.OperatorAuditEntry, w want) {
		log, wants = append(log, e), append(wants, w)
	}
	at := func(sec int) string {
		return fmt.Sprintf(`<span class="font-mono font-bold">2026-10-07 09:30:%02d UTC</span>`, sec)
	}
	by := `<span class="text-sm">By <bdi>` + html.EscapeString(ops) + `</bdi></span>`
	session := `<span class="text-sm">Session <span class="font-mono">` + s1.String()[:8] + `</span></span>`
	word := func(w string) string {
		return `<span class="font-display font-bold">` + html.EscapeString(w) + `</span>`
	}
	fact := func(f string) string { return `<span class="text-sm">` + html.EscapeString(f) + `</span>` }
	page := func(n, size string) string {
		return `<span class="text-sm">Page <span class="font-mono">` + n + `</span> (<span class="font-mono">` + size + `</span> per page)</span>`
	}
	unknown := func(label, raw string) string {
		return `<span class="text-sm">` + label + `<span class="font-mono">` + raw + `</span> ` + unrecognisedChip + `</span>`
	}

	e := read(59, ptr("tenant_plaques"))
	e.TargetTenantID, e.TargetTenantName = &t1, &tenant
	add(e, want{has: []string{at(59), word("Read"), by, fact("Of a tenant's plaques"), session,
		`<span class="text-sm">Tenant <a href="/operator/tenants/` + t1.String() + `" class="op-link"><bdi>` + html.EscapeString(tenant) + `</bdi></a></span>`},
		hasNot: []string{"Detail not shown", "Before sign-in", unrecognisedChip}})
	e = read(58, ptr("tenants"))
	e.PageNumber, e.PageSize, e.SearchClass = ptr(int32(2)), ptr(int32(50)), ptr("address")
	add(e, want{has: []string{fact("Of the tenant list"), page("2", "50"), fact("Searched by address")}, hasNot: []string{"Tenant <a", "Search:"}})
	e = read(57, ptr("tenant_detail"))
	e.TargetTenantID = &t2
	add(e, want{has: []string{`<span class="text-sm">A tenant id no tenant has <span class="break-all font-mono">` + t2.String() + `</span></span>`},
		hasNot: []string{"/operator/tenants/" + t2.String()}})
	e = read(56, ptr("tenant_detail"))
	e.TargetTenantID, e.TargetTenantName = &t3, ptr("\u200b\u3164")
	add(e, want{has: []string{`Tenant <a href="/operator/tenants/` + t3.String() + `" class="op-link"><span class="italic">Unnamed tenant</span></a>`,
		`<span class="break-all font-mono">` + t3.String() + `</span>`}})
	e = read(55, ptr("operator_audit"))
	e.PageNumber, e.PageSize, e.FilterKind = ptr(int32(3)), ptr(int32(50)), ptr("login")
	add(e, want{has: []string{fact("Of this audit log"), page("3", "50"), fact("Filter: Signed in")}})
	e = read(54, nil)
	e.DetailRecognised = false
	add(e, want{has: []string{fact("Of a scope this screen does not show"), `<span class="text-sm italic">Detail not shown</span>`}})
	add(read(53, ptr("billing_runs")), want{has: []string{unknown("Of ", "billing_runs")}})
	e = auditEntry("legal_publish", 52, &s1, &a1, &ops)
	e.LegalSlug, e.LegalBytes = ptr("privacy"), ptr(int32(1234))
	add(e, want{has: []string{word("Legal text published"), `<span class="text-sm">Document <span class="font-mono">/legal/privacy</span></span>`,
		`<span class="text-sm"><span class="font-mono">1234</span> bytes</span>`}, hasNot: []string{"Of "}})
	e = auditEntry("legal_publish", 51, &s1, &a1, &ops)
	e.LegalSlug = ptr("zz_slug")
	add(e, want{has: []string{unknown("Document ", "zz_slug")}, hasNot: []string{"/legal/zz_slug"}})
	e = auditEntry("login_failed", 50, nil, nil, nil)
	e.TargetAdminID, e.TargetAdminName = &a2, &pending
	add(e, want{has: []string{word("Sign-in refused: wrong password"), fact("Before sign-in"),
		`<span class="text-sm">Account <bdi>` + html.EscapeString(pending) + `</bdi></span>`}, hasNot: []string{"Session", "By "}})
	add(auditEntry("unknown_email", 49, nil, nil, nil), want{has: []string{word("Sign-in refused: no active account with that address"),
		fact("Before sign-in")}, hasNot: []string{"Account", "Session"}})
	e = auditEntry("totp_failed", 48, nil, nil, nil)
	e.TargetAdminID, e.TargetAdminName = &a3, &blank
	add(e, want{has: []string{word("Code refused"),
		`<span class="text-sm">Account of an unnamed operator <span class="break-all font-mono">` + a3.String() + `</span></span>`}})
	e = auditEntry("locked", 47, nil, nil, nil)
	e.TargetAdminID, e.TargetAdminName = &a1, &ops
	add(e, want{has: []string{word("Account locked after wrong codes"), `Account <bdi>` + html.EscapeString(ops) + `</bdi>`}})
	e = auditEntry("enrollment_failed", 46, nil, nil, nil)
	e.TargetAdminID, e.TargetAdminName = &a2, &pending
	add(e, want{has: []string{word("Setup link refused"), `Account <bdi>` + html.EscapeString(pending) + `</bdi>`}})
	e = auditEntry("password_ok", 45, nil, nil, nil)
	e.TargetAdminID, e.TargetAdminName = &a1, &ops
	add(e, want{has: []string{word("Password accepted, before the code"), fact("Before sign-in")}, hasNot: []string{"ompromised"}})
	add(auditEntry("login", 44, &s1, &a1, &ops), want{has: []string{word("Signed in"), by, session}})
	add(auditEntry("enrollment", 43, &s1, &a4, &disabled), want{has: []string{word("Set up and signed in"),
		`<span class="text-sm">By <bdi>` + html.EscapeString(disabled) + `</bdi></span>`}})
	add(auditEntry("logout", 42, &s1, &a1, &ops), want{has: []string{word("Signed out"), by}})
	add(auditEntry("zz_later_kind", 41, &s1, &a1, &ops), want{has: []string{
		`<span><span class="font-mono font-bold">zz_later_kind</span> ` + unrecognisedChip + `</span>`, by}})
	add(auditEntry("Bad Kind<i>", 40, &s1, &a1, &ops), want{has: []string{`<span>` + unrecognisedChip + `</span>`}, hasNot: []string{"Bad Kind"}})
	add(auditEntry("read", 39, &s1, &a5, &blank), want{has: []string{
		`<span class="text-sm">By an unnamed operator <span class="break-all font-mono">` + a5.String() + `</span></span>`}})
	e = read(38, ptr("tenants"))
	e.SearchClass = ptr("zz_class")
	add(e, want{has: []string{unknown("Search: ", "zz_class")}})
	e = read(35, ptr("tenants"))
	e.SearchClass = ptr("Bad Class")
	add(e, want{has: []string{`<span class="text-sm">Search: ` + unrecognisedChip + `</span>`}, hasNot: []string{"Bad Class"}})
	e = read(37, ptr("operator_audit"))
	e.FilterKind = ptr("all")
	add(e, want{has: []string{fact("Filter: Every kind")}})
	e = read(36, ptr("operator_audit"))
	e.FilterKind = ptr("zz_kind")
	add(e, want{has: []string{unknown("Filter: ", "zz_kind")}})
	e = read(34, ptr("tenant_billing"))
	e.TargetTenantID, e.TargetTenantName, e.PageNumber, e.PageSize = &t1, &tenant, ptr(int32(2)), ptr(int32(12))
	add(e, want{has: []string{fact("Of a tenant's billing"), page("2", "12"),
		`<span class="text-sm">Tenant <a href="/operator/tenants/` + t1.String() + `" class="op-link"><bdi>` + html.EscapeString(tenant) + `</bdi></a></span>`},
		hasNot: []string{unrecognisedChip, "Detail not shown"}})
	if len(log) != 26 {
		t.Fatalf("PREMISE: %d rows, the comment says twenty-six", len(log))
	}
	g.seedAudit(log...)

	w := g.get("/operator/audit", c)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("the audit screen = %d", w.Code)
	}
	rows := auditRowRe.FindAllStringSubmatch(body, -1)
	if len(rows) != 27 {
		t.Fatalf("the screen lists %d row(s), want 27 (the view's own and the twenty-six)", len(rows))
	}
	var viewing uuid.UUID
	g.store.mu.Lock()
	for _, s := range g.store.live {
		viewing = s.SessionID
	}
	g.store.mu.Unlock()
	own := rows[0][1]
	for _, f := range []string{word("Read"), fact("Of this audit log"), fact("Filter: Every kind"), page("1", "50"),
		`<span class="text-sm">Session <span class="font-mono">` + viewing.String()[:8] + `</span></span>`} {
		if !strings.Contains(own, f) {
			t.Errorf("the view's own row does not carry %q", f)
		}
	}
	for i, wt := range wants {
		r := rows[i+1][1]
		if !strings.HasPrefix(strings.TrimSpace(r), `<span class="font-mono font-bold">2026-10-07 09:30:`) {
			t.Errorf("row %d does not open with its time in UTC, in the data face", i+2)
		}
		for _, f := range wt.has {
			if !strings.Contains(r, f) {
				t.Errorf("row %d (%s) does not carry %q", i+2, log[i].Kind, f)
			}
		}
		for _, f := range wt.hasNot {
			if strings.Contains(r, f) {
				t.Errorf("row %d (%s) carries %q", i+2, log[i].Kind, f)
			}
		}
	}
	if strings.Contains(body, "11:30") || strings.Contains(body, "<script") || strings.Contains(body, "<b>") || strings.Contains(body, "<i>") ||
		strings.Count(body, "Search:") != 2 {
		t.Error("a time is printed in its stored zone, markup from a name or a kind is written raw, or \"Search:\" labels other than the two unknown classes")
	}
	for _, id := range []uuid.UUID{s1, viewing} {
		if strings.Contains(body, id.String()) {
			t.Errorf("the page carries a whole session id; it shows the first eight digits only")
		}
	}
	if n := strings.Count(body, unrecognisedChip); n != 7 {
		t.Errorf("%d unrecognised chip(s) on the page, want 7 (a scope, a slug, two kinds, two classes, a filter -- and none elsewhere)", n)
	}
}

// TestAuditWords_NameEveryKindAndNothingElse holds audit.go's kind words to the audit kinds,
// DERIVED: the constants of type db.OperatorAuditKind in internal/db's export data (the build
// being run), read with go/types -- so a kind added to internal/db is red here until the screen
// has a word for it.
//
// PART I -- the derived set is the words' keys and db.OperatorAuditKinds(); password_ok is
// among them (K14-1: the screen names the kind phase C writes); every word is non-empty and
// no two share one; no word says "compromised" (K14-6); a kind the words do not name is an
// unrecognised value carrying its raw text when it is a lower-case token -- a lower-case
// letter, then lower-case letters, digits and underscores, 63 at most -- and nothing
// otherwise (2nd round: a hyphen, a leading digit and a leading underscore are not a token;
// 3rd round: nor is a byte past ASCII -- z and a right-to-left override, z and an accented
// letter --, so no bidi control and no look-alike letter reaches the page from a value).
// CONTROL: the derived set has fourteen members (eleven since 00031, the three owner kinds
// since 00033 -- M10 OP-14 D), password_ok and operator_disabled among them.
//
// PART II -- red on: a constant of the type with no word; a word for a value that is no
// constant; an empty or a shared word; a fallback that names an unknown kind as a known one.
// PART III -- These checks only (no completeness claim).
func TestAuditWords_NameEveryKindAndNothingElse(t *testing.T) {
	tp := typedOperator(t)
	dbPkg := tp.imported(t, "github.com/atknatk/tappa/internal/db")
	kindType, _ := dbPkg.Scope().Lookup("OperatorAuditKind").(*types.TypeName)
	if kindType == nil {
		t.Fatal("PREMISE: internal/db declares no OperatorAuditKind")
	}
	var derived []string
	for _, name := range dbPkg.Scope().Names() {
		c, ok := dbPkg.Scope().Lookup(name).(*types.Const)
		if !ok || !types.Identical(c.Type(), kindType.Type()) {
			continue
		}
		derived = append(derived, constant.StringVal(c.Val()))
	}
	slices.Sort(derived)
	if len(derived) != 14 || !slices.Contains(derived, string(db.OperatorAuditPasswordOK)) ||
		!slices.Contains(derived, string(db.OperatorAuditOperatorDisabled)) {
		t.Fatalf("CONTROL: the derived kinds are %v", derived)
	}
	var listed []string
	for _, k := range db.OperatorAuditKinds() {
		listed = append(listed, string(k))
	}
	slices.Sort(listed)
	words := operator.AuditKindWordsForTest()
	var keys []string
	for k := range words {
		keys = append(keys, string(k))
	}
	slices.Sort(keys)
	if !slices.Equal(keys, derived) || !slices.Equal(listed, derived) {
		t.Errorf("the screen's words name %v; internal/db's constants are %v and OperatorAuditKinds %v", keys, derived, listed)
	}
	seen := map[string]bool{}
	for k, w := range words {
		if w == "" || seen[w] || strings.Contains(strings.ToLower(w), "compromised") {
			t.Errorf("%s: an empty, shared or verdict word %q", k, w)
		}
		seen[w] = true
	}
	for raw, want := range map[string]operatorpages.AuditWord{
		"zz_later_kind":               {Text: "zz_later_kind", Unknown: true},
		"LOGIN":                       {Unknown: true},
		"login ":                      {Unknown: true},
		"":                            {Unknown: true},
		"<b>":                         {Unknown: true},
		"a" + strings.Repeat("b", 63): {Unknown: true},
		"a" + strings.Repeat("b", 62): {Text: "a" + strings.Repeat("b", 62), Unknown: true},
		"a-b":                         {Unknown: true},
		"9abc":                        {Unknown: true},
		"_abc":                        {Unknown: true},
		"a9_b":                        {Text: "a9_b", Unknown: true},
		"z\u202e":                     {Unknown: true},
		"z\u00e9":                     {Unknown: true},
	} {
		if got := operator.AuditKindWordForTest(raw); got != want {
			t.Errorf("the kind %q is said as %+v, want %+v", raw, got, want)
		}
	}
	if got := operator.AuditKindWordForTest("login"); got != (operatorpages.AuditWord{Text: words[db.OperatorAuditLogin]}) {
		t.Errorf("CONTROL: the kind login is said as %+v", got)
	}
}

// TestAuditWords_NameEveryScopeTheNewestMigrationReturns holds audit.go's scope words to the
// read kinds op_read_audit returns as a row's scope, read without a database from the
// migrations: the Up half of the last migration that creates or replaces op_read_audit, its
// `CASE WHEN s.target_scope IN (...)` list in that definition -- the half of
// TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns that runs where no
// PostgreSQL does (3rd round: a read kind a later migration adds is red here until the
// screen has a word for it).
//
// PART I -- the words' keys equal that list; every scope word is non-empty and no two share
// one. CONTROL: the newest definition is 00032's or later's and its list names
// operator_audit and tenant_billing; the definition holds the CASE exactly once.
//
// PART II -- red on: a read kind of the list with no word; a word for a kind the list does
// not name; an empty or a shared word. PART III -- These checks only (no completeness claim):
// it reads the migrations' text, not what a database runs.
func TestAuditWords_NameEveryScopeTheNewestMigrationReturns(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "db", "migrations", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("PREMISE: no migrations read (%v)", err)
	}
	slices.Sort(files)
	definer := regexp.MustCompile(`(?m)^CREATE (?:OR REPLACE )?FUNCTION public\.op_read_audit\(`)
	var newest, up string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		half, _, _ := strings.Cut(string(b), "\n-- +goose Down")
		if definer.MatchString(half) {
			newest, up = f, half
		}
	}
	if newest == "" || filepath.Base(newest) < "00032" {
		t.Fatalf("CONTROL: the newest definition of op_read_audit is in %q, want 00032's or a later one", newest)
	}
	at := definer.FindAllStringIndex(up, -1)
	body, _, found := strings.Cut(up[at[len(at)-1][0]:], "-- +goose StatementEnd")
	if !found {
		t.Fatalf("CONTROL: %s's op_read_audit has no StatementEnd", filepath.Base(newest))
	}
	lists := regexp.MustCompile(`CASE WHEN s\.target_scope IN \(([^)]*)\)`).FindAllStringSubmatch(body, -1)
	if len(lists) != 1 {
		t.Fatalf("CONTROL: %s's op_read_audit holds the scope CASE %d time(s), want once", filepath.Base(newest), len(lists))
	}
	var scopes []string
	for _, m := range regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(lists[0][1], -1) {
		scopes = append(scopes, m[1])
	}
	slices.Sort(scopes)
	if !slices.Contains(scopes, "operator_audit") || !slices.Contains(scopes, "tenant_billing") {
		t.Fatalf("CONTROL: %s's op_read_audit returns the scopes %v", filepath.Base(newest), scopes)
	}
	words := operator.AuditScopeWordsForTest()
	var keys []string
	seen := map[string]bool{}
	for k, w := range words {
		keys = append(keys, k)
		if w == "" || seen[w] {
			t.Errorf("the scope %s has an empty or a shared word %q", k, w)
		}
		seen[w] = true
	}
	slices.Sort(keys)
	if !slices.Equal(keys, scopes) {
		t.Errorf("the screen's scope words name %v; op_read_audit in %s returns %v", keys, filepath.Base(newest), scopes)
	}
}

// TestAuditScreen_TheBoundaryRefusesBeforeTheStore: the POST's form, checked in
// audit.go before any store call and before the read budget.
//
// PART I -- a kind that is not "" or a member of db.OperatorAuditKinds as written -- an
// unknown kind, a member in capitals or with a space before or after it, "all" (the recorded
// filter, not a kind), SQL, a NUL, a member with a trailing line break -- is 400 with the
// kind refusal page and no store call. A page that is not 1..db.MaxOperatorAuditPage in
// decimal digits -- 0, -1, +2, " 2", "2 ", 1.5, 1e3, 1001, 99999, abc, a full-width digit --
// is 400 with the page refusal naming the bound and no store call -- and so is
// 18446744073709551621, twenty digits that wrap a 64-bit integer to 5 if the length guard is
// gone (2nd round). A body over maxFormBytes
// is 413 and an unreadable form (a bad percent escape) 400, no store call. CONTROL: "" and
// each member reach the store as that kind, page 1, size 50; pages "", 1, 7 and 1000 reach
// it as that page; a "size" field is not read.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestAuditScreen_TheBoundaryRefusesBeforeTheStore(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	calls := func() int { return g.store.count("OperatorAudit") }
	postRaw := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/audit", strings.NewReader(body))
		r.Host = opHost
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", opOrigin)
		r.AddCookie(c)
		r.RemoteAddr = "192.0.2.78:1"
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w
	}
	for _, kind := range []string{"zz_no_such_kind", "LOGIN", "Login", " login", "login ", "all", "read' OR '1'='1", "a\x00b", "login\n"} {
		before := calls()
		w := postRaw("kind=" + url.QueryEscape(kind))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "That kind is not in the audit log") ||
			!strings.Contains(w.Body.String(), "Nothing was read") {
			t.Errorf("the kind %q = %d, want 400 and the kind refusal", kind, w.Code)
		}
		if calls() != before {
			t.Errorf("the kind %q reached the store", kind)
		}
	}
	for _, page := range []string{"0", "-1", "+2", " 2", "2 ", "1.5", "1e3", strconv.Itoa(db.MaxOperatorAuditPage + 1), "99999", "abc", "２",
		"18446744073709551621"} {
		before := calls()
		w := postRaw("kind=&page=" + url.QueryEscape(page))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "That page does not exist") ||
			!strings.Contains(w.Body.String(), "pages 1 to "+strconv.Itoa(db.MaxOperatorAuditPage)) {
			t.Errorf("page %q = %d, want 400 and the page refusal naming the bound", page, w.Code)
		}
		if calls() != before {
			t.Errorf("page %q reached the store", page)
		}
	}
	before := calls()
	if w := postRaw("kind=" + strings.Repeat("x", 16<<10)); w.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(w.Body.String(), "That request was too large") || calls() != before {
		t.Errorf("a 16 KiB request body = %d with %d store call(s), want 413 and none", w.Code, calls()-before)
	}
	if w := postRaw("kind=%zz"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "That form could not be read") ||
		calls() != before {
		t.Errorf("an unreadable form = %d with %d store call(s), want 400 and none", w.Code, calls()-before)
	}
	last := func() db.OperatorAuditQuery {
		asks := g.auditAsks()
		if len(asks) == 0 {
			t.Fatal("PREMISE: the store was never asked")
		}
		return asks[len(asks)-1]
	}
	for _, k := range append([]db.OperatorAuditKind{""}, db.OperatorAuditKinds()...) {
		w := postRaw("kind=" + url.QueryEscape(string(k)) + "&size=200")
		if q := last(); w.Code != http.StatusOK || q.Kind != k || q.Number != 1 || int(q.Size) != operator.AuditPageSizeForTest {
			t.Errorf("CONTROL: the kind %q = %d, store %+v; want 200 and %q on page 1 of %d", k, w.Code, q, k, operator.AuditPageSizeForTest)
		}
	}
	if size := operator.AuditPageSizeForTest; size != operator.TenantPageSizeForTest {
		t.Errorf("the audit page is %d rows, want the tenant list's %d (K14-4)", size, operator.TenantPageSizeForTest)
	}
	if size := operator.AuditPageSizeForTest; size != 50 {
		t.Errorf("the audit page is %d rows, want 50 (K14-4)", size)
	}
	for page, want := range map[string]int32{"": 1, "1": 1, "7": 7, strconv.Itoa(db.MaxOperatorAuditPage): int32(db.MaxOperatorAuditPage)} {
		w := postRaw("kind=read&page=" + page)
		if q := last(); w.Code != http.StatusOK || q.Number != want || q.Kind != db.OperatorAuditRead {
			t.Errorf("CONTROL: page %q = %d, store %+v; want 200 and page %d of read", page, w.Code, q, want)
		}
	}
}

// TestAuditScreen_WearsTheDocketAnatomy pins the screen's brand anatomy (skill tappa-brand:
// the kitchen docket; OP-13 B's anatomy pins are the precedent), on page 2 of a filtered log
// so the pager has both forms.
//
// PART I -- the entries sit in ONE <section class="docket"> labelled by its heading, and every
// row is inside it; the heading is a docket label and its page number is in the data face;
// the kind filter is the op-input select and the pager's two buttons are op-link buttons; and
// web/static/css/input.css gives op-input and op-link the 44 px touch target (min-h-11 in
// their one-line or multi-line @apply).
//
// PART II -- red on: the docket section replaced or doubled; a row outside it; the heading's
// class or its mono page number dropped; the select's or a pager button's class changed; the
// touch target dropped from either rule. PART III -- These classes only (no completeness claim).
func TestAuditScreen_WearsTheDocketAnatomy(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	var log []db.OperatorAuditEntry
	for i := 0; i < 120; i++ {
		log = append(log, auditEntry("login", 0, nil, nil, nil))
	}
	g.seedAudit(log...)
	w := g.post("/operator/audit", auditForm("login", "2"), c)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("the audit screen = %d", w.Code)
	}
	const docket = `<section class="docket" aria-labelledby="audit-list">`
	start := strings.Index(body, docket)
	if strings.Count(body, docket) != 1 || strings.Count(body, `<section class="docket"`) != 1 || start < 0 {
		t.Fatal("the entries are not in one docket section")
	}
	end := start + strings.Index(body[start:], "</section>")
	rows := auditRowRe.FindAllStringIndex(body, -1)
	if len(rows) != 50 {
		t.Fatalf("PREMISE: page 2 lists %d row(s)", len(rows))
	}
	for _, r := range rows {
		if r[0] < start || r[1] > end {
			t.Fatal("a row sits outside the docket")
		}
	}
	for what, want := range map[string]string{
		"the docket's heading, a docket label with a mono page number": `<h2 id="audit-list" class="docket-label">Newest first · Signed in · page <span class="font-mono">2</span></h2>`,
		"the kind filter, the op-input select":                         `<select id="audit-kind" name="kind" class="op-input">`,
		"the pager's Previous, an op-link button":                      `<button type="submit" class="op-link">Previous page</button>`,
		"the pager's Next, an op-link button":                          `<button type="submit" class="op-link">Next page</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: %q is not on the page", what, want)
		}
	}
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []string{"op-input", "op-link"} {
		m := regexp.MustCompile(`(?m)^\s*\.` + class + `\s*\{\s*@apply ([^;]+);`).FindStringSubmatch(string(css))
		if m == nil || !slices.Contains(strings.Fields(m[1]), "min-h-11") {
			t.Errorf("input.css's .%s rule does not give the 44 px touch target (min-h-11)", class)
		}
	}
}

// TestAuditScreen_ReadsNothingFromTheURL: the kind and the page travel in the POST's body
// only (audit.go's file comment).
//
// PART I -- GET /operator/audit?kind=login&page=3 reaches the store as every kind, page 1;
// a POST to /operator/audit?kind=login&page=3 with an empty body reaches it as every kind,
// page 1; a POST whose URL says login and page 3 and whose body says read and page 2 reaches
// it as read, page 2. The page's filter form and pager forms post to /operator/audit with no
// query, and no link on the page carries a kind or a page.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestAuditScreen_ReadsNothingFromTheURL(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	g.seedAudit(auditEntry("login", 1, nil, nil, nil))
	last := func() db.OperatorAuditQuery {
		t.Helper()
		a := g.auditAsks()
		if len(a) == 0 {
			t.Fatal("the store was never asked")
		}
		return a[len(a)-1]
	}
	w := g.get("/operator/audit?kind=login&page=3", c)
	if q := last(); w.Code != http.StatusOK || q.Kind != "" || q.Number != 1 {
		t.Errorf("GET with a kind and a page in its URL = %d, store %+v; want every kind, page 1", w.Code, q)
	}
	w = g.post("/operator/audit?kind=login&page=3", url.Values{}, c)
	if q := last(); w.Code != http.StatusOK || q.Kind != "" || q.Number != 1 {
		t.Errorf("a POST with the kind and page in its URL and none in its body = %d, store %+v; want every kind, page 1", w.Code, q)
	}
	w = g.post("/operator/audit?kind=login&page=3", auditForm("read", "2"), c)
	if q := last(); w.Code != http.StatusOK || q.Kind != db.OperatorAuditRead || q.Number != 2 {
		t.Errorf("a POST whose body says read, page 2 = %d, store %+v", w.Code, q)
	}
	body := w.Body.String()
	for _, m := range regexp.MustCompile(`<form method="post" action="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if m[1] != "/operator/audit" && m[1] != "/operator/logout" {
			t.Errorf("a form posts to %q", m[1])
		}
	}
	for _, m := range regexp.MustCompile(`href="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if strings.Contains(m[1], "?") {
			t.Errorf("a link carries a query: %q", m[1])
		}
	}
}

// TestAuditScreen_PagesForwardOnlyAfterAFullPage: the pager (audit.go's auditLogView).
//
// PART I -- a log of 120 'login' rows: filtered to login, page 1 lists 50 and offers Next (a
// form posting page 2 and the kind) and no Previous; page 2 offers both; page 3 lists 20 and
// offers Previous only; page 4 lists none and says the log ended. Unfiltered, the view's own
// rows are listed too: page 1 lists 50 and posts an empty kind. With 50 000 login rows, page
// db.MaxOperatorAuditPage is full, offers no Next and says it is the last page the screen
// shows -- the newest 50,000 entries of the kind chosen. The filter's options are every kind
// and then each audit kind, the chosen one selected.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestAuditScreen_PagesForwardOnlyAfterAFullPage(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	var log []db.OperatorAuditEntry
	for i := 0; i < 120; i++ {
		log = append(log, auditEntry("login", 0, nil, nil, nil))
	}
	g.seedAudit(log...)
	pager := func(body string) (prev, next bool) {
		return strings.Contains(body, ">Previous page</button>"), strings.Contains(body, ">Next page</button>")
	}
	pageOf := func(kind, n string) string {
		w := g.post("/operator/audit", auditForm(kind, n), c)
		if w.Code != http.StatusOK {
			t.Fatalf("page %s of %q = %d", n, kind, w.Code)
		}
		return w.Body.String()
	}
	for _, tc := range []struct {
		page       string
		rows       int
		prev, next bool
	}{{"1", 50, false, true}, {"2", 50, true, true}, {"3", 20, true, false}} {
		body := pageOf("login", tc.page)
		if n := len(auditRowRe.FindAllString(body, -1)); n != tc.rows {
			t.Errorf("login page %s lists %d row(s), want %d", tc.page, n, tc.rows)
		}
		if p, n := pager(body); p != tc.prev || n != tc.next {
			t.Errorf("login page %s: previous %v next %v, want %v %v", tc.page, p, n, tc.prev, tc.next)
		}
		if tc.next {
			n, _ := strconv.Atoi(tc.page)
			if !strings.Contains(body, `<input type="hidden" name="kind" value="login"> <input type="hidden" name="page" value="`+strconv.Itoa(n+1)+`">`) {
				t.Errorf("login page %s's Next does not post the kind and page %d", tc.page, n+1)
			}
		}
		if !strings.Contains(body, `<option value="login" selected>Signed in</option>`) || !strings.Contains(body, `<option value="">Every kind</option>`) {
			t.Errorf("login page %s's filter does not show login chosen", tc.page)
		}
	}
	if body := pageOf("login", "4"); len(auditRowRe.FindAllString(body, -1)) != 0 || !strings.Contains(body, "the log ended on the page before") {
		t.Error("login page 4 of 120 does not say the log ended")
	}
	body := pageOf("", "1")
	if n := len(auditRowRe.FindAllString(body, -1)); n != 50 || !strings.Contains(body, `<input type="hidden" name="kind" value=""> <input type="hidden" name="page" value="2">`) ||
		!strings.Contains(body, `<option value="" selected>Every kind</option>`) {
		t.Errorf("unfiltered page 1 lists %d row(s), or its Next does not post an empty kind", n)
	}
	opts := regexp.MustCompile(`<option value="([^"]*)"`).FindAllStringSubmatch(body, -1)
	var values []string
	for _, o := range opts {
		values = append(values, o[1])
	}
	want := []string{""}
	for _, k := range db.OperatorAuditKinds() {
		want = append(want, string(k))
	}
	if !slices.Equal(values, want) {
		t.Errorf("the filter's options are %v, want every kind and then %v", values, want[1:])
	}
	log = log[:0]
	for i := 0; i < db.MaxOperatorAuditPage*operator.AuditPageSizeForTest; i++ {
		log = append(log, auditEntry("login", 0, nil, nil, nil))
	}
	g.seedAudit(log...)
	body = pageOf("login", strconv.Itoa(db.MaxOperatorAuditPage))
	if n := len(auditRowRe.FindAllString(body, -1)); n != operator.AuditPageSizeForTest {
		t.Fatalf("PREMISE: page %d lists %d row(s)", db.MaxOperatorAuditPage, n)
	}
	if _, next := pager(body); next {
		t.Errorf("the full page %d offers a next page past the bound", db.MaxOperatorAuditPage)
	}
	if !strings.Contains(body, `the newest <span class="font-mono">50,000</span> entries of the`) {
		t.Error("the last page does not say how many entries of one kind the screen reaches")
	}
	if strings.Contains(pageOf("login", "1"), "the last page the screen shows") {
		t.Error("page 1 says it is the last page")
	}
}

// TestAuditScreen_EscapesWhatOperatorsAndTenantsNamed: an operator's display name, an
// account's and a tenant's name that each close the markup around them and open a script,
// and a name carrying a right-to-left override -- none is written raw, each is there escaped
// inside a bdi element (so the assertion is not passing on a value that never rendered).
func TestAuditScreen_EscapesWhatOperatorsAndTenantsNamed(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const evil = `</bdi></a><script>alert(1)</script>`
	const rtl = "\u202eFAKE override"
	s, a, b, tn := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	actor, account, tenant, rtlName := "op "+evil, "acc "+evil, "ten "+evil, rtl
	e1 := auditEntry("read", 3, &s, &a, &actor)
	e1.Scope, e1.TargetTenantID, e1.TargetTenantName = ptr("tenant_detail"), &tn, &tenant
	e2 := auditEntry("login_failed", 2, nil, nil, nil)
	e2.TargetAdminID, e2.TargetAdminName = &b, &account
	e3 := auditEntry("logout", 1, &s, &a, &rtlName)
	g.seedAudit(e1, e2, e3)
	body := g.get("/operator/audit", c).Body.String()
	if strings.Contains(body, "<script") {
		t.Error("the audit screen writes a script element")
	}
	for _, want := range []string{"<bdi>" + html.EscapeString(actor) + "</bdi>", "<bdi>" + html.EscapeString(account) + "</bdi>",
		"<bdi>" + html.EscapeString(tenant) + "</bdi>", "<bdi>" + rtl + "</bdi>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the audit screen does not carry %q escaped in a bdi", want)
		}
	}
}

// TestAuditScreen_NoCredentialFieldReachesThePage: ADR 0021 §3.5's never-log list on the
// audit screen.
//
// PART I -- BY TYPE: the fields of db.OperatorAuditEntry, operatorpages.AuditRow,
// operatorpages.AuditLogView, operatorpages.AuditWord and operatorpages.AuditKindOption are
// EXACTLY the lists below (a field added to any of the five is red here until it is argued
// for -- the slot a session hash, a ticket, an address or a raw detail would need; M10 OP-14 D
// argued for AuditRow.ByOwner, a bool, which can carry no value). ON THE
// PAGE: after a sign-in and a view, the viewing session's cookie value and its hash (the
// fake's key), the operator's address and the TOTP code of the sign-in are on none of three
// views (unfiltered, filtered, a later page); the viewing session's id is there as its
// first eight digits only. CONTROL: the same scan finds the hash when it is put into a name
// the page does show.
//
// PART II -- the five field lists and the scan above. PART III -- The fake's rows carry what
// a test puts in them; the database half -- op_read_audit returns no hash, no ticket and no
// detail (internal/db's TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList) -- is
// internal/db's, and the end-to-end half op14_db_test.go's. No completeness claim.
func TestAuditScreen_NoCredentialFieldReachesThePage(t *testing.T) {
	fields := func(v any) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			out = append(out, rt.Field(i).Name)
		}
		slices.Sort(out)
		return out
	}
	for _, c := range []struct {
		v    any
		want []string
	}{
		{db.OperatorAuditEntry{}, []string{"ActorID", "ActorName", "At", "DetailRecognised", "FilterKind", "ID", "Kind", "LegalBytes",
			"LegalSlug", "PageNumber", "PageSize", "Scope", "SearchClass", "SessionID", "TargetAdminID", "TargetAdminName",
			"TargetTenantID", "TargetTenantName"}},
		{operatorpages.AuditRow{}, []string{"Account", "AccountID", "Actor", "ActorID", "At", "ByOwner", "DetailHidden", "Filter", "Kind", "Legal",
			"LegalBytes", "Page", "PageSize", "Scope", "ScopeHidden", "Search", "Session", "Tenant", "TenantID", "TenantPath"}},
		{operatorpages.AuditLogView{}, []string{"Filter", "HasNext", "Kind", "LastPage", "Options", "Page", "Reach", "Rows"}},
		{operatorpages.AuditWord{}, []string{"Text", "Unknown"}},
		{operatorpages.AuditKindOption{}, []string{"Label", "Selected", "Value"}},
	} {
		if got := fields(c.v); !slices.Equal(got, c.want) {
			t.Errorf("%T's fields are %v, want %v -- a new field is a new slot the page can print", c.v, got, c.want)
		}
	}

	g := newRig(t)
	f := g.active()
	cookie := g.signIn(f)
	g.seedAudit(auditEntry("unknown_email", 1, nil, nil, nil))
	var hash string
	var viewing uuid.UUID
	g.store.mu.Lock()
	for h, s := range g.store.live {
		hash, viewing = h, s.SessionID
	}
	g.store.mu.Unlock()
	needles := []string{cookie.Value, hash, f.email, totpAt(f.key, g.now), viewing.String()}
	pages := []string{
		g.get("/operator/audit", cookie).Body.String(),
		g.post("/operator/audit", auditForm("read", ""), cookie).Body.String(),
		g.post("/operator/audit", auditForm("", "2"), cookie).Body.String(),
	}
	for i, p := range pages {
		for _, n := range needles {
			if strings.Contains(p, n) {
				t.Errorf("view %d carries a credential, the address or a whole session id (length %d)", i+1, len(n))
			}
		}
	}
	if !strings.Contains(pages[0], `<span class="font-mono">`+viewing.String()[:8]+`</span>`) {
		t.Error("PREMISE: the viewing session's first eight digits are not on the page")
	}
	// CONTROL: a hash in a name the page shows is found.
	sid, admin, name := uuid.New(), uuid.New(), "FAKE "+hash
	g.seedAudit(auditEntry("logout", 0, &sid, &admin, &name))
	if control := g.post("/operator/audit", auditForm("logout", ""), cookie).Body.String(); !strings.Contains(control, hash) {
		t.Fatal("CONTROL: a hash printed in a name is not found by the scan")
	}
}

// TestAuditScreen_TheTextClearsAA recomputes, from tailwind.config.js's palette, the contrast
// of the audit screen's text on its grounds (WCAG 2.1 relative luminance, sRGB; a
// translucent token composited on its ground): ink on paper (the rows, the card, the
// "Search:" label of an unknown class -- 2nd round, no colour class of its own), ink at 70%
// on paper (the last-page note), ink on ink at 10% over paper (the unrecognised chip),
// tappa-green on paper (the card's and a row's tenant links) and on porcelain (the pager's
// links), paper on tappa-green (the Show button) -- each against AA's 4.5:1 (the chip is
// 11px, the rest 14px and smaller: none is large text). And it ties the one chip the screen
// writes to the ground computed here: web/static/css/input.css has exactly one rule naming
// tally--unrecognised, and it applies the ink frame on the ink at 10% ground.
func TestAuditScreen_TheTextClearsAA(t *testing.T) {
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
	ink, paper, porcelain, green := hexOf("ink"), hexOf("paper"), hexOf("porcelain"), hexOf("tappa-green")
	for _, c := range []struct {
		what   string
		fg, bg [3]float64
	}{
		{"ink on paper", ink, paper},
		{"ink at 70% on paper", over(ink, 0.7, paper), paper},
		{"ink on ink at 10% over paper (unrecognised)", ink, over(ink, 0.1, paper)},
		{"tappa-green on paper", green, paper},
		{"tappa-green on porcelain", green, porcelain},
		{"paper on tappa-green", paper, green},
	} {
		if got := contrast(c.fg, c.bg); got < 4.5 {
			t.Errorf("%s = %.2f:1, want at least 4.5:1", c.what, got)
		} else {
			t.Logf("%s = %.2f:1", c.what, got)
		}
	}
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	ruleRe := regexp.MustCompile(`(?m)((?:^\s*\.[a-z-]+,\s*\n)*^\s*\.[a-z-]+\s*)\{\s*@apply ([^;]+);\s*\}`)
	var found []string
	for _, r := range ruleRe.FindAllStringSubmatch(string(css), -1) {
		for _, sel := range strings.Split(r[1], ",") {
			if strings.TrimSpace(sel) == ".tally--unrecognised" {
				found = append(found, r[2])
			}
		}
	}
	if len(found) != 1 || !slices.Contains(strings.Fields(found[0]), "bg-ink/10") || !slices.Contains(strings.Fields(found[0]), "border-ink") {
		t.Errorf("tally--unrecognised has %d rule(s) in input.css (%q); want one, the ink frame on ink at 10%%", len(found), found)
	}
}

// TestAuditBudget_EachViewIsOneReadOfTheSessionsSharedBudget measures surface.go's readLimit
// (60 reads per session per window) on the audit log beside another screen, and sessionLimit
// (100 requests) beside it -- the OP-14 re-count of the two numbers.
//
// PART I -- one session, from a fresh address per request: 30 plaque views and 15 GETs and
// 15 POSTs of the audit log, interleaved -- 60 x 200, 30 OperatorAudit calls; then one more
// GET and one more POST of the log are each 429 with the predicate run (TouchOperatorSession
// +1) and no OperatorAudit call, each the SIGNED-IN form of the page (problemTooMany(true));
// then 38 console views are 200 (62 + 38 = 100 requests) and the 101st request is 429 at the
// gate. Refusals that spend no read: a second session's 5 kinds refused, 5 pages outside
// 1..1000 (0 and 1001), 5 bodies over maxFormBytes and 5 unreadable forms are 400, 400, 413
// and 400 with no store call, and the session still makes 60 audit views -- all 200 -- before
// its 61st is 429.
//
// PART II -- the counts above. PART III -- These sequences only (no completeness claim).
func TestAuditBudget_EachViewIsOneReadOfTheSessionsSharedBudget(t *testing.T) {
	g := newRig(t)
	tenant := g.seedPlaques("FAKE Audit Budget Ltd", plaqueAt("04A1B2C3D4E5A1", "unassigned", nil, nil, nil))
	n := 0
	send := func(r req) *httptest.ResponseRecorder {
		n++
		r.host = opHost
		r.remote = fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1)
		if r.method == http.MethodGet {
			r.header = map[string]string{"Sec-Fetch-Site": "same-origin"}
		} else {
			r.origin = opOrigin
		}
		return g.do(r)
	}
	auditGet := func(c *http.Cookie) req {
		return req{method: http.MethodGet, path: "/operator/audit", cookies: []*http.Cookie{c}}
	}
	auditPost := func(c *http.Cookie) req {
		return req{method: http.MethodPost, path: "/operator/audit", form: auditForm("read", ""), cookies: []*http.Cookie{c}}
	}
	plaque := func(c *http.Cookie) req {
		return req{method: http.MethodGet, path: plaquePath(tenant.String()), cookies: []*http.Cookie{c}}
	}
	a := g.signIn(g.active())
	for i := 0; i < 15; i++ {
		for _, rq := range []req{plaque(a), auditGet(a), plaque(a), auditPost(a)} {
			if w := send(rq); w.Code != http.StatusOK {
				t.Fatalf("read %d of one session (%s %s) = %d, want 200", 4*i+1, rq.method, rq.path, w.Code)
			}
		}
	}
	if got := g.store.count("OperatorAudit"); got != 30 {
		t.Fatalf("PREMISE: %d audit read(s) for 30 audit views", got)
	}
	for _, rq := range []req{auditGet(a), auditPost(a)} {
		touches, audits := g.store.count("TouchOperatorSession"), g.store.count("OperatorAudit")
		w := send(rq)
		if w.Code != http.StatusTooManyRequests || !signedInTooMany(w.Body.String()) {
			t.Errorf("the 61st read (%s) = %d, want 429 with the signed-in page", rq.method, w.Code)
		}
		if g.store.count("TouchOperatorSession") != touches+1 || g.store.count("OperatorAudit") != audits {
			t.Errorf("%s refused by the read budget: predicate +%d, OperatorAudit +%d; want +1, +0", rq.method,
				g.store.count("TouchOperatorSession")-touches, g.store.count("OperatorAudit")-audits)
		}
	}
	for i := 0; i < 38; i++ {
		if w := send(req{method: http.MethodGet, path: "/operator", cookies: []*http.Cookie{a}}); w.Code != http.StatusOK {
			t.Fatalf("console view %d after 62 requests = %d, want 200", i+1, w.Code)
		}
	}
	if w := send(req{method: http.MethodGet, path: "/operator", cookies: []*http.Cookie{a}}); w.Code != http.StatusTooManyRequests {
		t.Errorf("the 101st request = %d, want 429 at the gate", w.Code)
	}

	b := g.signIn(g.active())
	sendRaw := func(body string) *httptest.ResponseRecorder {
		n++
		r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/audit", strings.NewReader(body))
		r.Host = opHost
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", opOrigin)
		r.AddCookie(b)
		r.RemoteAddr = fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1)
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w
	}
	before := g.store.count("OperatorAudit")
	for i := 0; i < 5; i++ {
		page := "0"
		if i%2 == 1 {
			page = strconv.Itoa(db.MaxOperatorAuditPage + 1)
		}
		for _, rf := range []struct {
			body string
			want int
		}{{"kind=zz_no_such_kind", 400}, {"kind=&page=" + page, 400}, {"kind=" + strings.Repeat("x", 16<<10), 413}, {"kind=%zz", 400}} {
			if w := sendRaw(rf.body); w.Code != rf.want {
				t.Fatalf("a refused audit form = %d, want %d", w.Code, rf.want)
			}
		}
	}
	if got := g.store.count("OperatorAudit") - before; got != 0 {
		t.Fatalf("PREMISE: the 20 refusals made %d audit read(s), want 0", got)
	}
	for i := 0; i < 60; i++ {
		if w := send(auditGet(b)); w.Code != http.StatusOK {
			t.Fatalf("audit view %d after 20 refusals = %d, want 200 -- a refusal spent a read", i+1, w.Code)
		}
	}
	if w := send(auditGet(b)); w.Code != http.StatusTooManyRequests {
		t.Errorf("the 61st audit view after 20 refusals = %d, want 429", w.Code)
	}
}
