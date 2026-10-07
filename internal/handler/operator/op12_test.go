package operator_test

// op12_test.go -- M10 OP-12 phase B: /operator/tenants/{id}/billing on the shipped router with
// the fake store (rig_test.go). The header table of its response classes (C122-C141), the
// screen's words (every state its word and sentence, no verdict, an unreadable month kept
// visible with no figure), a frozen and a live month drawn apart, the floor sentence, no
// itemisation, the money (one bridge, the symbol in the render layer, no float), a failed
// read as a problem page and never a zero invoice, the boundary's refusals before the store,
// the URL read for nothing, the banner, the months' own zones, the pager, the docket, the
// contrast of the chips, the read budget the screen shares with the other screens and the
// read's time bound against the request's deadline. Against PostgreSQL: op12_db_test.go.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"html"
	"maps"
	"math/big"
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
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// ------------------------------------------------------------------ fixtures --

// mustNumeric is s as pgx scans a numeric's text (1.10 is 110 x 10^-2).
func mustNumeric(s string) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		panic(fmt.Sprintf("numeric %q: %v", s, err))
	}
	return n
}

// pgxZero is a zero as pgx DECODES it from the wire: 0 x 10^0, its scale lost (ADR 0021's
// OP-12 note, L4) -- not the 0 x 10^-2 a text scan gives.
func pgxZero() pgtype.Numeric { return pgtype.Numeric{Int: big.NewInt(0), Exp: 0, Valid: true} }

func monthDate(y int, m time.Month) pgtype.Date {
	return pgtype.Date{Time: time.Date(y, m, 1, 0, 0, 0, 0, time.UTC), Valid: true}
}

// localBounds is the month's two instants in zone, the second exclusive.
func localBounds(y int, m time.Month, zone string) (time.Time, time.Time) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		panic(err)
	}
	from := time.Date(y, m, 1, 0, 0, 0, 0, loc)
	return from.UTC(), from.AddDate(0, 1, 0).UTC()
}

// billingMonth describes one month the fake returns; month builds it in the read's shape.
type billingMonth struct {
	y                 int
	m                 time.Month
	state             string // "frozen", "running", "unclosed", "before"
	people, unstamped int32
	price, amount     string // "" with zero: the pgx zero
	free              bool
	zone, plan        string
	currency          string // frozen only; "" = EUR
	closedAt          time.Time
	firstCharged      pgtype.Date
	zero              bool // the amount is pgx's zero (0 x 10^0)
}

func (b billingMonth) month() db.TenantBillingMonth {
	out := db.TenantBillingMonth{Month: monthDate(b.y, b.m)}
	if b.state == "before" {
		return out
	}
	zone, plan := b.zone, b.plan
	if zone == "" {
		zone = "Europe/Malta"
	}
	if plan == "" {
		plan = "founding"
	}
	from, to := localBounds(b.y, b.m, zone)
	price, amount := b.price, b.amount
	if price == "" {
		price = "1.10"
	}
	out.AfterSignup = true
	out.From, out.To, out.Zone, out.Plan = &from, &to, &zone, &plan
	out.Free, out.EmployeeCount, out.UnstampedEmployees = ptr(b.free), ptr(b.people), ptr(b.unstamped)
	out.UnitPrice = mustNumeric(price)
	if b.zero {
		out.AmountDue = pgxZero()
	} else {
		out.AmountDue = mustNumeric(amount)
	}
	switch b.state {
	case "frozen":
		cur := b.currency
		if cur == "" {
			cur = "EUR"
		}
		out.Frozen, out.Currency, out.ClosedAt, out.HasEnded = true, &cur, ptr(b.closedAt), ptr(true)
	case "running":
		out.FirstChargeableMonth, out.HasEnded = b.firstCharged, ptr(false)
	case "unclosed":
		out.FirstChargeableMonth, out.HasEnded = b.firstCharged, ptr(true)
	default:
		panic("unknown state " + b.state)
	}
	return out
}

// billingPageOfEveryState is twelve months, newest first, from (y, m): a founding tenant that
// signed up five months before m, three free months, the first charged one frozen and the one
// before it frozen free, the others live -- running, ended and not closed (one with three
// disagreeing records), ended and not closed in the free window -- and six before sign-up.
func billingPageOfEveryState(y int, m time.Month) []db.TenantBillingMonth {
	at := func(back int) (int, time.Month) {
		t := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, -back, 0)
		return t.Year(), t.Month()
	}
	fy, fm := at(2)
	first := monthDate(fy, fm)
	var out []db.TenantBillingMonth
	for i, b := range []billingMonth{
		{state: "running", people: 3, amount: "3.30", firstCharged: first},
		{state: "unclosed", people: 2, unstamped: 3, amount: "2.20", firstCharged: first},
		{state: "frozen", people: 2, amount: "2.20", closedAt: time.Date(fy, fm+1, 2, 10, 30, 0, 0, time.UTC)},
		{state: "frozen", people: 2, free: true, zero: true, closedAt: time.Date(fy, fm, 3, 9, 15, 0, 0, time.UTC)},
		{state: "unclosed", people: 1, free: true, zero: true, firstCharged: first},
		{state: "unclosed", people: 1, free: true, amount: "0.00", firstCharged: first},
		{state: "before"}, {state: "before"}, {state: "before"}, {state: "before"}, {state: "before"}, {state: "before"},
	} {
		b.y, b.m = at(i)
		out = append(out, b.month())
	}
	return out
}

// billingBeforeSignupMonths is n months before sign-up, newest first, from (y, m).
func billingBeforeSignupMonths(y int, m time.Month, n int) []db.TenantBillingMonth {
	var out []db.TenantBillingMonth
	for i := 0; i < n; i++ {
		t := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, -i, 0)
		out = append(out, db.TenantBillingMonth{Month: monthDate(t.Year(), t.Month())})
	}
	return out
}

// seedBilling puts one tenant's months into the fake and returns its id.
func (g *rig) seedBilling(name string, months ...db.TenantBillingMonth) uuid.UUID {
	g.t.Helper()
	id := uuid.New()
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	g.store.billing[id] = fakeBilling{name: name, months: months}
	return id
}

func (g *rig) billingAsks() []fakeBillingAsk {
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	return append([]fakeBillingAsk(nil), g.store.billingAsks...)
}

// billingPathOf is the billing screen of tenant id.
func billingPathOf(id string) string { return "/operator/tenants/" + id + "/billing" }

// billingRowRe is one month of the screen.
var billingRowRe = regexp.MustCompile(`(?s)<li class="op-row">(.*?)</li>`)

// fact is a row's labelled fact as the screen writes it.
func fact(label, value string) string {
	return `<span class="text-sm">` + label + ` <span class="font-mono">` + html.EscapeString(value) + `</span></span>`
}

// The screen's words, written out here rather than read from the handler, so a changed
// word is red.
const (
	wordFrozen   = "Frozen"
	wordRunning  = "Live — month running"
	wordUnclosed = "Ended, not closed by the business"
	wordBefore   = "Before sign-up"
	wordUnread   = "Unreadable"

	sentenceFrozen   = "Closed by the business; every figure was read from the frozen record, and nothing recomputes it."
	sentenceRunning  = "A live count, worked out afresh on each view: it moves until the month ends and the business closes it."
	sentenceUnclosed = "This month is over and the business has not closed it, so its figures are still a live count, worked out afresh on each view."
	sentenceBefore   = "This month is before the business signed up; there is nothing to count for it."
	sentenceUnread   = "This month's row could not be read as a billing month, so no figure is shown for it — not even a zero."
)

// ------------------------------------------------------------ the header table --

// TestOperatorHeaders_TheBillingClassesCarryThePolicy drives the twenty response classes of
// the billing screen (C122-C141) with the 40-class test's hostile drive and checks
// (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these twenty, at WriteHeader (the recorder's Result().Header): the
// designed status, route, header names and values (designedHeaders: no Location but the
// sign-in's on any of them); no hostile value (hostileValues) in a header value or in the
// body, raw or query-escaped -- every one's last request carries the hostile query, which
// holds a page under the form's own name (hostileDrive adds it on
// /operator/tenants/{id}/billing; C136's hand-built request gets it from hostileOn), so a GET
// or a POST that read its page from the URL, or echoed one, is red here; no script; and the
// store counts below: C134 (cross-origin) and C141 (PUT) make no store call; C126, C131,
// C135, C136, C137, C138 and C140 make no TenantBilling call.
//
// PART II -- the list above, on these twenty classes.
//
// PART III -- This test measures the twenty classes and the raw and query-escaped forms only;
// anything else (examples: a class a later handler adds, a hostile value echoed HTML-escaped
// or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_TheBillingClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	tenant := g.seedBilling("FAKE Header Billing Ltd", append(billingPageOfEveryState(2026, time.October),
		billingBeforeSignupMonths(2025, time.October, 12)...)...)
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
	view := func(id string, c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: billingPathOf(id), cookies: c, header: sfs}
	}
	page := func(id string, form url.Values, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: billingPathOf(id), form: form, origin: opOrigin, cookies: c}
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
			g.store.fail["TenantBilling"] = err
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); delete(g.store.fail, "TenantBilling"); g.store.mu.Unlock() }()
			return send(rq)
		}
	}
	once := func(r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return send(r()) }
	}
	calls := map[string]map[string]int{}
	counted := func(class string, drive func() *httptest.ResponseRecorder) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			before, total := g.store.count("TenantBilling"), g.store.total()
			w := drive()
			calls[class] = map[string]int{"TenantBilling": g.store.count("TenantBilling") - before, "all": g.store.total() - total}
			return w
		}
	}
	// spent is a session with its 60 reads spent on billing views (readLimit).
	spent := func() *http.Cookie {
		t.Helper()
		c := signIn()
		for i := 0; i < 60; i++ {
			if w := send(view(tenant.String(), c)); w.Code != http.StatusOK {
				t.Fatalf("PREMISE: billing view %d = %d", i+1, w.Code)
			}
		}
		return c
	}
	malformed := strings.ReplaceAll(tenant.String(), "-", "")
	dead := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}
	classes := []headerClass{
		{"C122 tenant billing", 200, once(func() req { return view(tenant.String(), signIn()) })},
		{"C123 tenant billing without a cookie", 303, once(func() req { return view(tenant.String()) })},
		{"C124 tenant billing with a dead cookie", 303, once(func() req { return view(tenant.String(), dead) })},
		{"C125 tenant billing, a same-site read", 303, once(func() req {
			r := view(tenant.String(), signIn())
			r.header = map[string]string{"Sec-Fetch-Site": "same-site"}
			return r
		})},
		{"C126 tenant billing, a malformed id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C126", once(func() req { return view(malformed, c) }))()
		}},
		{"C127 tenant billing, an id no tenant has", 404, once(func() req { return view(uuid.NewString(), signIn()) })},
		{"C128 tenant billing, the read fails", 503, failing(errFakeDB, func() req { return view(tenant.String(), signIn()) })},
		{"C129 tenant billing, the read times out", 503, failing(errFakeBillingTimeout, func() req { return view(tenant.String(), signIn()) })},
		{"C130 tenant billing, the read's session is refused", 303,
			failing(db.ErrOperatorRefused, func() req { return view(tenant.String(), signIn()) })},
		{"C131 tenant billing, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spent()
			return counted("C131", once(func() req { return view(tenant.String(), c) }))()
		}},
		{"C132 tenant billing, page 2", 200, once(func() req { return page(tenant.String(), url.Values{"page": {"2"}}, signIn()) })},
		{"C133 tenant billing page without a cookie", 303, once(func() req { return page(tenant.String(), url.Values{"page": {"2"}}) })},
		{"C134 tenant billing page, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C134", func() *httptest.ResponseRecorder {
				r := page(tenant.String(), url.Values{"page": {"2"}}, c)
				r.origin, r.header = "https://taptime.mt", map[string]string{"Sec-Fetch-Site": "same-site"}
				return send(r)
			})()
		}},
		{"C135 tenant billing page, an oversized form", 413, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C135", once(func() req { return page(tenant.String(), url.Values{"page": {strings.Repeat("9", 20<<10)}}, c) }))()
		}},
		{"C136 tenant billing page, an unreadable form", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C136", func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "http://"+opHost+billingPathOf(tenant.String()), strings.NewReader("page=%zz"))
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
		{"C137 tenant billing page, a page refused", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C137", once(func() req { return page(tenant.String(), url.Values{"page": {"6"}}, c) }))()
		}},
		{"C138 tenant billing page, a malformed id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C138", once(func() req { return page(malformed, url.Values{"page": {"2"}}, c) }))()
		}},
		{"C139 tenant billing page, the read fails", 503,
			failing(errFakeDB, func() req { return page(tenant.String(), url.Values{"page": {"3"}}, signIn()) })},
		{"C140 tenant billing page, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := spent()
			return counted("C140", once(func() req { return page(tenant.String(), url.Values{"page": {"2"}}, c) }))()
		}},
		{"C141 PUT on tenant billing", 405, counted("C141", once(func() req {
			return req{method: http.MethodPut, path: billingPathOf(tenant.String()), origin: opOrigin, form: url.Values{}}
		}))},
	}
	if len(classes) != 20 {
		t.Fatalf("PREMISE: %d classes, the comment says 20", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(122+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 122+i, c.name, 122+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	for class, want := range map[string]map[string]int{
		"C126": {"TenantBilling": 0}, "C131": {"TenantBilling": 0}, "C134": {"all": 0}, "C135": {"TenantBilling": 0},
		"C136": {"TenantBilling": 0}, "C137": {"TenantBilling": 0}, "C138": {"TenantBilling": 0}, "C140": {"TenantBilling": 0},
		"C141": {"all": 0},
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

// ------------------------------------------------------------------- the words --

// billingPage renders tenant's billing screen through the rig, page 1, as a signed-in
// operator, and returns its body and rows.
func billingPage(t *testing.T, g *rig, c *http.Cookie, id uuid.UUID) (string, []string) {
	t.Helper()
	w := g.get(billingPathOf(id.String()), c)
	if w.Code != http.StatusOK {
		t.Fatalf("the billing screen = %d; log: %s", w.Code, g.logs.String())
	}
	var rows []string
	for _, m := range billingRowRe.FindAllStringSubmatch(w.Body.String(), -1) {
		rows = append(rows, m[1])
	}
	return w.Body.String(), rows
}

// verdictWords are the words -- as patterns over lower-cased text, their inflections included --
// that would claim a payment state, a fact the product does not hold (B3 of the OP-12 card:
// the screen says what the record says, not a verdict). 2nd round: the first list matched
// whole words only, so "owed" passed a list that held "owe" (the third eye's MZ10).
var verdictWords = []*regexp.Regexp{
	regexp.MustCompile(`\bow(e|ed|es|ing)\b`),
	regexp.MustCompile(`\bdue\b`),
	regexp.MustCompile(`\b(un)?settled\b`),
	regexp.MustCompile(`\bpa(y|ys|id|yment|yments|yable)\b`),
	regexp.MustCompile(`\boverdue\b`),
	regexp.MustCompile(`\bunpaid\b`),
	regexp.MustCompile(`\bdebts?\b`),
	regexp.MustCompile(`\boutstanding\b`),
	regexp.MustCompile(`\blate\b`),
	regexp.MustCompile(`\barrears\b`),
}

// verdictControls are one phrase per verdict pattern, each of which the pattern must match --
// so a pattern emptied or misspelt is red rather than silently matching nothing.
var verdictControls = []string{"it is owed", "amount due", "unsettled", "payment pending", "overdue", "unpaid", "a debt",
	"outstanding", "paid late", "in arrears"}

// verdictsIn is every verdict pattern that matches text, lower-cased.
func verdictsIn(text string) []string {
	low := strings.ToLower(text)
	var out []string
	for _, v := range verdictWords {
		if m := v.FindString(low); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// TestBillingScreen_EveryStateSaysItsWordAndNoVerdict: the billing screen's reading of each
// month, through the shipped handler (billing.go's billingRowOf and the five words).
//
// PART I -- a page of twelve months (billingPageOfEveryState): running, ended and not closed
// (with disagreeing records), frozen and charged, frozen in the free window (its amount the
// pgx zero), ended and not closed in the free window twice, six before sign-up. Each row
// opens with its month in the data face, its chip carries the state's word and the chip
// class of its tone, its sentence is the state's; "Ended, not closed by the business" is on
// exactly the three rows the read returns as not frozen, ended and after sign-up, and on no
// other; a row before sign-up carries no figure (no "People counted", no "€"), and every row
// after sign-up carries its count, price, amount, plan and period; a frozen row carries
// "Closed" and no "First charged month", a live row the opposite. Seven shapes a later read
// could send are drawn as Unreadable with NO figure and logged with the month: a live month
// lacking its count, a month before sign-up carrying a count, a frozen month with no currency,
// a live month with no first charged month, a frozen month whose amount has three decimal
// places and a live month whose unit price has three (MoneyFromNumeric's two refusals -- 3rd
// round: the price's was not driven, and dropping its error drew the row with an empty price,
// green; S8), and a frozen month not after sign-up; a month with no date likewise. No pattern
// of verdictWords (inflections included: owe/owed/owes/owing, pay/paid/payment, (un)settled,
// due, …) matches, lower-cased, the two pages or the screen's three problem pages (a page
// refusal, an oversized form, a failed read); CONTROL: each pattern matches its phrase of
// verdictControls.
//
// PART II -- the list above. PART III -- These twelve and six months and these strings only
// (no completeness claim).
func TestBillingScreen_EveryStateSaysItsWordAndNoVerdict(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	tenant := g.seedBilling("FAKE States Ltd", billingPageOfEveryState(2026, time.October)...)
	body, rows := billingPage(t, g, c, tenant)
	if len(rows) != 12 {
		t.Fatalf("the screen lists %d row(s), want 12", len(rows))
	}
	type want struct {
		month, word, class, sentence string
		figures, frozen              bool
	}
	wants := []want{
		{"October 2026", wordRunning, "tally--running", sentenceRunning, true, false},
		{"September 2026", wordUnclosed, "tally--unclosed", sentenceUnclosed, true, false},
		{"August 2026", wordFrozen, "tally--frozen", sentenceFrozen, true, true},
		{"July 2026", wordFrozen, "tally--frozen", sentenceFrozen, true, true},
		{"June 2026", wordUnclosed, "tally--unclosed", sentenceUnclosed, true, false},
		{"May 2026", wordUnclosed, "tally--unclosed", sentenceUnclosed, true, false},
	}
	for _, m := range []string{"April 2026", "March 2026", "February 2026", "January 2026", "December 2025", "November 2025"} {
		wants = append(wants, want{m, wordBefore, "tally--before-signup", sentenceBefore, false, false})
	}
	for i, w := range wants {
		r := rows[i]
		if !strings.HasPrefix(strings.TrimSpace(r), `<span class="font-mono font-bold">`+w.month+`</span>`) {
			t.Errorf("row %d does not open with %s in the data face", i+1, w.month)
		}
		if !strings.Contains(r, `<span class="tally `+w.class+`">`+html.EscapeString(w.word)+`</span>`) {
			t.Errorf("row %d (%s): no %s chip saying %q", i+1, w.month, w.class, w.word)
		}
		if !strings.Contains(r, `<span class="w-full text-sm">`+html.EscapeString(w.sentence)+`</span>`) {
			t.Errorf("row %d (%s): the sentence is not %q", i+1, w.month, w.sentence)
		}
		for _, label := range []string{"People counted", "Price per person", "Amount", "Plan", "Counted over"} {
			if got := strings.Contains(r, `<span class="text-sm">`+label+` <span class="font-mono">`); got != w.figures {
				t.Errorf("row %d (%s): %q present = %v, want %v", i+1, w.month, label, got, w.figures)
			}
		}
		if !w.figures && strings.Contains(r, "€") {
			t.Errorf("row %d (%s), before sign-up, carries an amount", i+1, w.month)
		}
		if w.figures {
			if got := strings.Contains(r, `<span class="text-sm">Closed <span`); got != w.frozen {
				t.Errorf("row %d (%s): Closed present = %v, want %v", i+1, w.month, got, w.frozen)
			}
			if got := strings.Contains(r, `<span class="text-sm">First charged month <span`); got != !w.frozen {
				t.Errorf("row %d (%s): First charged month present = %v, want %v", i+1, w.month, got, !w.frozen)
			}
		}
	}
	if n := strings.Count(body, ">"+wordUnclosed+"<"); n != 3 {
		t.Errorf("%q is on %d row(s), want the 3 the read returns as not frozen, ended and after sign-up", wordUnclosed, n)
	}
	for _, f := range []string{fact("People counted", "2"), fact("Price per person", "€1.10"), fact("Amount", "€2.20"),
		fact("Plan", "founding"), fact("First charged month", "August 2026"), fact("Amount", "€0.00")} {
		if !strings.Contains(body, f) {
			t.Errorf("the page does not carry %q", f)
		}
	}

	// Shapes the read does not document: unreadable, no figure, logged.
	odd := billingPageOfEveryState(2026, time.October)
	odd[0].EmployeeCount = nil                  // a live month lacking a figure
	odd[2].Currency = nil                       // a frozen month with no currency
	odd[1].FirstChargeableMonth = pgtype.Date{} // a live month with no first charged month
	odd[3].AmountDue = mustNumeric("0.005")     // more than two decimal places
	odd[4].UnitPrice = mustNumeric("1.105")     // a live month's unit price, more than two decimal places
	odd[6].EmployeeCount = ptr(int32(4))        // a month before sign-up carrying a count
	odd[7].Frozen = true                        // frozen, yet before sign-up
	odd[8].Month = pgtype.Date{}                // a month with no date
	oddTenant := g.seedBilling("FAKE Odd Shapes Ltd", odd...)
	g.logs.Reset()
	oddBody, oddRows := billingPage(t, g, c, oddTenant)
	for _, i := range []int{0, 1, 2, 3, 4, 6, 7, 8} {
		r := oddRows[i]
		if !strings.Contains(r, `<span class="tally tally--unreadable">`+wordUnread+`</span>`) ||
			!strings.Contains(r, `<span class="w-full text-sm">`+html.EscapeString(sentenceUnread)+`</span>`) {
			t.Errorf("odd row %d is not drawn as unreadable", i+1)
		}
		if strings.Contains(r, "€") || strings.Contains(r, "People counted") || strings.Contains(r, "0.00") {
			t.Errorf("odd row %d, unreadable, carries a figure", i+1)
		}
	}
	if !strings.Contains(oddRows[8], `<span class="font-mono font-bold">A month with no date</span>`) {
		t.Error("the month with no date is not named as such")
	}
	if n := strings.Count(g.logs.String(), "operator: a billing month could not be read"); n != 2*8 { // text and JSON
		t.Errorf("%d log line(s) for the eight unreadable months across the two formats, want 16", n)
	}
	// The verdict words: on the two pages and on the billing screen's three fixed problem
	// pages (a page refusal, an oversized form, a failed read).
	pages := map[string]string{"the page": body, "the odd shapes' page": oddBody}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"the page refusal":   g.post(billingPathOf(tenant.String()), url.Values{"page": {"6"}}, c),
		"the oversized form": g.post(billingPathOf(tenant.String()), url.Values{"page": {strings.Repeat("1", 17<<10)}}, c),
	} {
		pages[name] = w.Body.String()
	}
	g.store.mu.Lock()
	g.store.fail["TenantBilling"] = errFakeDB
	g.store.mu.Unlock()
	pages["the failed read"] = g.get(billingPathOf(tenant.String()), c).Body.String()
	g.store.mu.Lock()
	delete(g.store.fail, "TenantBilling")
	g.store.mu.Unlock()
	for name, title := range map[string]string{"the page refusal": "That page does not exist",
		"the oversized form": "That request was too large", "the failed read": "This tenant&#39;s billing could not be loaded"} {
		if !strings.Contains(pages[name], title) {
			t.Fatalf("PREMISE: %s is not the problem page titled %q", name, title)
		}
	}
	for name, page := range pages {
		if found := verdictsIn(page); len(found) != 0 {
			t.Errorf("%s carries the verdict word(s) %q", name, found)
		}
	}
	if len(verdictControls) != len(verdictWords) {
		t.Fatalf("PREMISE: %d control phrase(s) for %d pattern(s)", len(verdictControls), len(verdictWords))
	}
	for i, v := range verdictWords {
		if !v.MatchString(verdictControls[i]) {
			t.Errorf("CONTROL: the verdict pattern %s does not match %q", v, verdictControls[i])
		}
	}
}

// TestBilling_AFrozenMonthAndALiveMonthDoNotRenderAlike is the tenant screen's obligation
// (TestBilling_AFrozenMonthAndADraftDoNotRenderAlike) on the operator's: a month with the SAME
// figures, once frozen and once live (running, and ended and not closed), never draws alike,
// and the difference is in more than one place.
//
// PART I -- the three rows carry the same month, count, price, amount, plan and period, and
// differ pairwise in the chip's word, the chip's class and the sentence; the frozen one alone
// says when it was closed and the live ones alone their first charged month. CONTROL: two
// rows of the same live state with the same figures render identically, so the difference
// above is the state's.
//
// PART II -- red on a frozen and a live row equal in any one of the word, the class, the
// sentence, or the closed/first-charged fact. PART III -- no completeness claim.
func TestBilling_AFrozenMonthAndALiveMonthDoNotRenderAlike(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	first := monthDate(2026, time.March)
	base := billingMonth{y: 2026, m: time.July, people: 24, price: "1.50", amount: "36.00", firstCharged: first,
		closedAt: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)}
	frozen, running, unclosed := base, base, base
	frozen.state, running.state, unclosed.state = "frozen", "running", "unclosed"
	months := []db.TenantBillingMonth{frozen.month(), running.month(), unclosed.month(), unclosed.month()}
	months = append(months, billingBeforeSignupMonths(2025, time.December, 8)...)
	_, rows := billingPage(t, g, c, g.seedBilling("FAKE Alike Ltd", months...))
	if rows[2] != rows[3] {
		t.Fatal("CONTROL: two live rows with the same state and figures render differently")
	}
	chip := regexp.MustCompile(`<span class="tally (tally--[a-z-]+)">([^<]*)</span>`)
	sentence := regexp.MustCompile(`<span class="w-full text-sm">([^<]*)</span>`)
	for i, j := range [][2]int{{0, 1}, {0, 2}, {1, 2}} {
		a, b := rows[j[0]], rows[j[1]]
		ca, cb := chip.FindStringSubmatch(a), chip.FindStringSubmatch(b)
		sa, sb := sentence.FindStringSubmatch(a), sentence.FindStringSubmatch(b)
		if ca == nil || cb == nil || sa == nil || sb == nil {
			t.Fatalf("pair %d: a row has no chip or no sentence", i)
		}
		if ca[1] == cb[1] || ca[2] == cb[2] || sa[1] == sb[1] {
			t.Errorf("rows %d and %d share a chip class (%v), a word (%v) or a sentence (%v)", j[0]+1, j[1]+1,
				ca[1] == cb[1], ca[2] == cb[2], sa[1] == sb[1])
		}
	}
	for _, f := range []string{fact("People counted", "24"), fact("Price per person", "€1.50"), fact("Amount", "€36.00"),
		fact("Counted over", "2026-07-01 00:00 to 2026-08-01 00:00 Europe/Malta")} {
		for i := 0; i < 3; i++ {
			if !strings.Contains(rows[i], f) {
				t.Errorf("row %d does not carry the shared figure %q", i+1, f)
			}
		}
	}
	if !strings.Contains(rows[0], fact("Closed", "2026-08-02 12:00 Europe/Malta")) || strings.Contains(rows[0], "First charged month") {
		t.Error("the frozen row does not say when it was closed, or carries the live rule")
	}
	for i := 1; i < 3; i++ {
		if !strings.Contains(rows[i], fact("First charged month", "March 2026")) || strings.Contains(rows[i], ">Closed <") {
			t.Errorf("live row %d does not carry its first charged month, or says it was closed", i+1)
		}
	}
}

// TestBillingScreen_SaysTheCountIsAFloor is the tenant screen's §4.6 obligation
// (TestBilling_SaysTheCountIsAFloorOnTheScreen) on the operator's.
//
// PART I -- a month with no disagreeing record says nothing about a floor; a frozen and a
// live month with three each say "FLOOR", "upper bound" and "not the amount missing", with
// the 3 in the data face, on their own rows only.
//
// PART II -- red on the sentence dropped, printed with nothing to admit, or its words or its
// number changed. PART III -- no completeness claim.
func TestBillingScreen_SaysTheCountIsAFloor(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	quiet := billingMonth{y: 2026, m: time.September, state: "unclosed", people: 2, amount: "2.20", firstCharged: monthDate(2026, time.May)}
	loudLive, loudFrozen := quiet, quiet
	loudLive.unstamped, loudLive.m = 3, time.August
	loudFrozen.unstamped, loudFrozen.m, loudFrozen.state, loudFrozen.closedAt = 3, time.July, "frozen", time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	months := append([]db.TenantBillingMonth{quiet.month(), loudLive.month(), loudFrozen.month()},
		billingBeforeSignupMonths(2026, time.June, 9)...)
	body, rows := billingPage(t, g, c, g.seedBilling("FAKE Floor Ltd", months...))
	if strings.Contains(rows[0], "FLOOR") || strings.Contains(rows[0], "upper bound") {
		t.Error("a month with no disagreeing record says the count is a floor")
	}
	for _, i := range []int{1, 2} {
		for _, w := range []string{"The count is a FLOOR: ", `<span class="font-mono">3</span> employee records of this business`,
			"upper bound", "not the amount missing"} {
			if !strings.Contains(rows[i], w) {
				t.Errorf("row %d (three disagreeing records) does not carry %q", i+1, w)
			}
		}
	}
	if n := strings.Count(body, "FLOOR"); n != 2 {
		t.Errorf("the floor sentence is on %d row(s), want 2", n)
	}
}

// TestBillingScreen_OffersNoItemisationAnywhere is the tenant screen's obligation
// (TestBilling_OffersNoItemisationAnywhere) on the operator's: nothing on the page offers or
// carries the people behind a count.
//
// PART I -- every href and action on the page (page 3, so the pager has both forms) is one of
// the tenant's overview, the tenant list, the screen's own path (the pager's posts) and the
// bar's sign-out, besides /static/; the page says no person is listed; the read's month type,
// the view and the row have exactly the fields written below -- none for a person -- read by
// reflection.
//
// PART II -- red on a fifth target, the sentence dropped, or a field added to any of the three
// types. PART III -- A field added under one of these names with another meaning is code
// review's -- no completeness claim.
func TestBillingScreen_OffersNoItemisationAnywhere(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	tenant := g.seedBilling("FAKE Itemised Ltd", append(append(billingPageOfEveryState(2026, time.October),
		billingBeforeSignupMonths(2025, time.October, 12)...), billingBeforeSignupMonths(2024, time.October, 12)...)...)
	w := g.post(billingPathOf(tenant.String()), url.Values{"page": {"3"}}, c)
	if w.Code != http.StatusOK {
		t.Fatalf("page 3 = %d", w.Code)
	}
	body := w.Body.String()
	allowed := map[string]bool{"/operator/tenants/" + tenant.String(): true, "/operator/tenants": true,
		billingPathOf(tenant.String()): true, "/operator/logout": true}
	n := 0
	for _, m := range regexp.MustCompile(`\s(href|action)="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		if strings.HasPrefix(m[2], "/static/") {
			continue
		}
		n++
		if !allowed[m[2]] {
			t.Errorf("the billing screen points at %q, which is none of its four targets", m[2])
		}
	}
	if n < 5 {
		t.Fatalf("PREMISE: %d target(s) read off page 3", n)
	}
	if !strings.Contains(body, "no person is") || !strings.Contains(body, "A count is all a month keeps") {
		t.Error("the page does not say that a count is all it keeps and no person is listed")
	}
	fields := func(v any) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			out = append(out, rt.Field(i).Name)
		}
		return out
	}
	for name, c := range map[string]struct {
		got, want []string
	}{
		"db.TenantBillingMonth": {fields(db.TenantBillingMonth{}), []string{"Month", "AfterSignup", "Frozen", "From", "To", "Zone", "Plan",
			"FirstChargeableMonth", "Free", "EmployeeCount", "UnstampedEmployees", "UnitPrice", "Currency", "AmountDue", "ClosedAt", "HasEnded"}},
		"db.TenantBillingTimeline": {fields(db.TenantBillingTimeline{}), []string{"TenantID", "TenantName", "Page", "Months"}},
		"operatorpages.TenantBillingView": {fields(operatorpages.TenantBillingView{}), []string{"Name", "ID", "OverviewPath", "BillingPath",
			"Page", "LastPage", "MonthsPerPage", "Rows"}},
		"operatorpages.BillingRow": {fields(operatorpages.BillingRow{}), []string{"Month", "Label", "Sentence", "Tone", "HasFigures", "Plan",
			"People", "UnitPrice", "Amount", "Free", "FirstCharged", "ClosedAt", "Period", "Floor"}},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s's fields are %v, want %v", name, c.got, c.want)
		}
	}
}

// ------------------------------------------------------------------- the money --

// TestBillingMoney_PutsTheSymbolInTheRenderLayer is the tenant screen's
// TestBillingMoney_PutsTheSymbolInTheRenderLayer on the operator's moneyText, with the
// numeric shapes pgx hands over (ADR 0021's OP-12 note, L4).
//
// PART I -- 1.10 is €1.10; pgx's zero (0 x 10^0, its scale lost) is €0.00; a text zero (0 x
// 10^-2) is €0.00; 1.5 (scale 1) is €1.50; 2019.00 is €2019.00, ungrouped; GBP prints its code
// after the digits; three decimal places, NULL, NaN and an infinity are refused (an error,
// never a string), and so is a missing currency; billing.Money.String carries no symbol.
//
// PART II -- the cases above. PART III -- no completeness claim.
func TestBillingMoney_PutsTheSymbolInTheRenderLayer(t *testing.T) {
	for _, c := range []struct {
		name     string
		n        pgtype.Numeric
		currency string
		want     string
	}{
		{"a price", mustNumeric("1.10"), "EUR", "€1.10"},
		{"pgx's zero", pgxZero(), "EUR", "€0.00"},
		{"a text zero", mustNumeric("0.00"), "EUR", "€0.00"},
		{"a scale of one", mustNumeric("1.5"), "EUR", "€1.50"},
		{"no grouping", mustNumeric("2019.00"), "EUR", "€2019.00"},
		{"a currency with no symbol here", mustNumeric("5.00"), "GBP", "5.00 GBP"},
	} {
		got, err := operator.MoneyTextForTest(c.n, c.currency)
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v; want %q", c.name, got, err, c.want)
		}
	}
	for _, c := range []struct {
		name     string
		n        pgtype.Numeric
		currency string
	}{
		{"three decimal places", mustNumeric("1.105"), "EUR"},
		{"NULL", pgtype.Numeric{}, "EUR"},
		{"NaN", pgtype.Numeric{NaN: true, Valid: true}, "EUR"},
		{"an infinity", pgtype.Numeric{InfinityModifier: pgtype.Infinity, Valid: true}, "EUR"},
		{"no currency", mustNumeric("1.10"), ""},
	} {
		if got, err := operator.MoneyTextForTest(c.n, c.currency); err == nil || got != "" {
			t.Errorf("%s: %q, %v; want a refusal", c.name, got, err)
		}
	}
	if s := billing.NewMoney(201900, "EUR").String(); strings.ContainsAny(s, "€$£,") {
		t.Errorf("billing.Money.String has acquired a symbol or a separator: %q", s)
	}
}

// TestBillingMoney_OneBridgeAndNoFloat pins the billing screen's money path on the package's
// types (typedOperator: the build's own files, type-checked).
//
// Threat model: an ACCIDENTAL drift -- a figure worked out, rounded or re-summed in Go where
// the read already holds it exact. Code written to slip past a type-checked reading of this
// package (a helper in another package, reflection, a value smuggled through an interface) is
// for code review.
//
// PART I -- the shipped package: billing.MoneyFromNumeric is used once, in moneyText;
// db.TenantBillingMonth's UnitPrice and AmountDue are used, in billingRowOf only, as direct
// arguments of moneyText (once each) and in a NULL test (.Valid, the month before sign-up
// that must carry none); no selector of the package names pgtype.Numeric's
// Float64Value, Int64Value or Value, or its Int or Exp field; THE MONEY PATH'S FILES -- every
// file of the package that declares moneyText, billingRowOf or readBilling, whatever its name
// (3rd round) -- have no expression of a floating-point type or an untyped float constant and do
// not import math/big, and nothing in the package names or has the type big.Float or big.Rat
// (or their constructors); those files multiply, divide and take a remainder in billingPage only
// (the page number's digits); operatorpages.BillingRow's Amount and UnitPrice are each written exactly once in
// the package -- as a key of a keyed BillingRow literal in billingRowOf whose value is a local
// variable defined once, by `x, err := moneyText(m.AmountDue, …)` (m.UnitPrice for the
// price), used nowhere else, never addressed, never assigned again -- and every BillingRow
// literal of the package is keyed. CONTROL: the scan resolves the one MoneyFromNumeric call,
// the two field uses and the two row writes, and the file scan walks the declarations of all
// three money functions (a Fatal otherwise: a scan of no file is not a pass).
//
// PART II -- red on: NF1 billing.MoneyFromNumeric used outside moneyText, or a count there
// other than 1; NF2 UnitPrice or AmountDue of db.TenantBillingMonth used other than as a
// direct argument of moneyText or the operand of .Valid in billingRowOf, or as an argument a
// count other than one each (a NULL test more than once); NF3 a use of
// pgtype.Numeric's Float64Value, Int64Value, Value, Int or Exp anywhere in the package; NF4 an
// expression of a money-path file of type float32 or float64 (named or not) or an untyped float
// constant; (2nd round) a money-path file importing math/big, or any expression or name of the
// package of type big.Float or big.Rat (pointer or not) or naming Float, Rat, NewFloat, NewRat
// or ParseFloat of math/big; NF5 a *, /, % (or *=, /=, %=) in a money-path file outside
// billingPage (2nd round: / and % added); NF6 (2nd round) BillingRow.Amount or .UnitPrice written or read
// other than as above -- a selector write (row.Amount = …), a value that is not the one
// variable, a variable defined other than by that moneyText call, assigned again, addressed or
// used a second time -- or an unkeyed BillingRow literal. 2nd round: the third eye's MZ2 (a
// big.Float division in moneyText) and MZ3 (the amount re-summed with Money.Add in a loop)
// were green on the first version; NF4 and NF6 are what turn them red. 3rd round: NF4 and NF5
// scanned the file NAMED billing.go, so renaming it (S9) or moving moneyText with a float into
// a file of its own (S9b) left them nothing to scan, green; they are now anchored on the three
// functions, and the CONTROL above refuses a scan that did not reach them.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: a
// conversion in another package the handler calls, a float hidden behind an interface, a
// figure rebuilt from strings inside moneyText) is code review's -- no completeness claim.
func TestBillingMoney_OneBridgeAndNoFloat(t *testing.T) {
	tp := typedOperator(t)
	bp := tp.imported(t, "github.com/atknatk/tappa/internal/domain/billing")
	dp := tp.imported(t, "github.com/atknatk/tappa/internal/db")
	pp := tp.imported(t, "github.com/jackc/pgx/v5/pgtype")
	moneyText := lookupFunc(t, tp.pkg, "moneyText")
	rowOf := lookupFunc(t, tp.pkg, "billingRowOf")
	billingPage := lookupFunc(t, tp.pkg, "billingPage")
	var bad []string
	bridges := 0
	for _, id := range tp.usesOf(lookupFunc(t, bp, "MoneyFromNumeric")) {
		if tp.in(moneyText, id.Pos()) && tp.callOf(id) != nil {
			bridges++
			continue
		}
		bad = append(bad, "NF1 billing.MoneyFromNumeric at "+tp.where(id.Pos()))
	}
	if bridges != 1 {
		bad = append(bad, fmt.Sprintf("NF1 billing.MoneyFromNumeric called %d time(s) in moneyText, want 1", bridges))
	}
	fieldUses, nullTests := map[string]int{}, map[string]int{}
	for _, name := range []string{"UnitPrice", "AmountDue"} {
		for _, id := range tp.usesOf(lookupMember(t, dp, "TenantBillingMonth", name)) {
			sel, _ := tp.parents[id].(*ast.SelectorExpr)
			if sel == nil || !tp.in(rowOf, id.Pos()) {
				bad = append(bad, "NF2 db.TenantBillingMonth."+name+" at "+tp.where(id.Pos()))
				continue
			}
			if c, _ := tp.argOf(sel); c != nil && tp.callee(c) == moneyText {
				fieldUses[name]++
				continue
			}
			if valid, ok := tp.parents[sel].(*ast.SelectorExpr); ok && valid.X == sel && valid.Sel.Name == "Valid" {
				nullTests[name]++
				continue
			}
			bad = append(bad, "NF2 db.TenantBillingMonth."+name+" at "+tp.where(id.Pos()))
		}
	}
	if fieldUses["UnitPrice"] != 1 || fieldUses["AmountDue"] != 1 || nullTests["UnitPrice"] > 1 || nullTests["AmountDue"] > 1 {
		bad = append(bad, fmt.Sprintf("NF2 the two money fields reach moneyText %v times and are NULL-tested %v times, want once each and at most once each",
			fieldUses, nullTests))
	}
	for _, name := range []string{"Float64Value", "Int64Value", "Value", "Int", "Exp"} {
		for _, id := range tp.usesOf(lookupMember(t, pp, "Numeric", name)) {
			bad = append(bad, "NF3 pgtype.Numeric."+name+" at "+tp.where(id.Pos()))
		}
	}
	// NF4 and NF5 read the money path's files: every file that declares one of the three
	// functions, whatever it is named.
	anchors := map[types.Object]string{moneyText: "moneyText", rowOf: "billingRowOf",
		tp.method(t, "Surface", "readBilling"): "readBilling"}
	var moneyFiles []*ast.File
	for _, f := range tp.files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && anchors[tp.info.Defs[fd.Name]] != "" {
				moneyFiles = append(moneyFiles, f)
				break
			}
		}
	}
	walked := map[string]bool{}
	for _, f := range moneyFiles {
		file := filepath.Base(tp.fset.File(f.Pos()).Name())
		for _, im := range f.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); p == "math/big" {
				bad = append(bad, "NF4 "+file+", a file of the money path, imports math/big")
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if fd, ok := n.(*ast.FuncDecl); ok && fd.Body != nil {
				if name := anchors[tp.info.Defs[fd.Name]]; name != "" {
					walked[name] = true
				}
			}
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			if tv, ok := tp.info.Types[e]; ok && tv.Type != nil {
				if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Info()&types.IsFloat != 0 {
					bad = append(bad, "NF4 a float expression at "+tp.where(e.Pos()))
				}
			}
			switch x := n.(type) {
			case *ast.BinaryExpr:
				if (x.Op == token.MUL || x.Op == token.QUO || x.Op == token.REM) && !tp.in(billingPage, x.Pos()) {
					bad = append(bad, "NF5 a "+x.Op.String()+" at "+tp.where(x.Pos()))
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok && (as.Tok == token.MUL_ASSIGN || as.Tok == token.QUO_ASSIGN || as.Tok == token.REM_ASSIGN) {
				bad = append(bad, "NF5 a "+as.Tok.String()+" at "+tp.where(as.Pos()))
			}
			return true
		})
	}
	for _, name := range anchors {
		if !walked[name] {
			t.Fatalf("CONTROL: the float and arithmetic scan (NF4, NF5) did not walk %s's declaration; it would scan nothing", name)
		}
	}
	// NF4, package-wide: math/big's inexact kinds, by type and by name.
	isBigInexact := func(typ types.Type) bool {
		if p, ok := typ.(*types.Pointer); ok {
			typ = p.Elem()
		}
		n, ok := typ.(*types.Named)
		return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "math/big" && (n.Obj().Name() == "Float" || n.Obj().Name() == "Rat")
	}
	bigSeen := map[token.Pos]bool{}
	for e, tv := range tp.info.Types {
		if tv.Type != nil && isBigInexact(tv.Type) && !bigSeen[e.Pos()] {
			bigSeen[e.Pos()] = true
			bad = append(bad, "NF4 a big.Float or big.Rat at "+tp.where(e.Pos()))
		}
	}
	for id, obj := range tp.info.Uses {
		if obj.Pkg() != nil && obj.Pkg().Path() == "math/big" &&
			slices.Contains([]string{"Float", "Rat", "NewFloat", "NewRat", "ParseFloat"}, obj.Name()) && !bigSeen[id.Pos()] {
			bigSeen[id.Pos()] = true
			bad = append(bad, "NF4 math/big."+obj.Name()+" at "+tp.where(id.Pos()))
		}
	}
	// NF6: the row's two money strings come from moneyText's reading of the month's own
	// figure and from nothing else.
	op := tp.imported(t, operatorpagesPkgPath)
	rowType := op.Scope().Lookup("BillingRow").Type()
	for _, f := range tp.files {
		ast.Inspect(f, func(n ast.Node) bool {
			if cl, ok := n.(*ast.CompositeLit); ok && types.Identical(tp.info.TypeOf(cl), rowType) {
				for _, el := range cl.Elts {
					if _, keyed := el.(*ast.KeyValueExpr); !keyed {
						bad = append(bad, "NF6 an unkeyed BillingRow literal at "+tp.where(cl.Pos()))
						break
					}
				}
			}
			return true
		})
	}
	// definedByMoneyText is the one way NF6 lets the variable v come to be: `v, err :=
	// moneyText(m.<source>, …)` in billingRowOf, its only definition; every other mention of v
	// but the literal's value at use is a finding.
	definedByMoneyText := func(v *types.Var, source string, use *ast.Ident) []string {
		var out []string
		defs := 0
		for id, obj := range tp.info.Defs {
			if obj != v {
				continue
			}
			as, ok := tp.parents[id].(*ast.AssignStmt)
			if !ok || as.Tok != token.DEFINE || len(as.Rhs) != 1 || len(as.Lhs) != 2 || as.Lhs[0] != id {
				out = append(out, "NF6 "+v.Name()+" defined other than as `"+v.Name()+", err := moneyText(…)` at "+tp.where(id.Pos()))
				continue
			}
			c, ok := ast.Unparen(as.Rhs[0]).(*ast.CallExpr)
			if !ok || tp.callee(c) != moneyText || len(c.Args) != 2 {
				out = append(out, "NF6 "+v.Name()+" defined other than by moneyText at "+tp.where(id.Pos()))
				continue
			}
			sel, ok := ast.Unparen(c.Args[0]).(*ast.SelectorExpr)
			if !ok || tp.info.Uses[sel.Sel] != lookupMember(t, dp, "TenantBillingMonth", source) {
				out = append(out, "NF6 "+v.Name()+" is moneyText of something other than the month's "+source+" at "+tp.where(id.Pos()))
				continue
			}
			defs++
		}
		if defs != 1 {
			out = append(out, fmt.Sprintf("NF6 %s defined %d time(s) by moneyText(m.%s, …), want 1", v.Name(), defs, source))
		}
		for id, obj := range tp.info.Uses {
			if obj == v && id != use {
				out = append(out, "NF6 "+v.Name()+" used again (assigned, addressed or passed on) at "+tp.where(id.Pos()))
			}
		}
		return out
	}
	rowWrites := map[string]int{}
	for field, source := range map[string]string{"Amount": "AmountDue", "UnitPrice": "UnitPrice"} {
		for _, id := range tp.usesOf(lookupMember(t, op, "BillingRow", field)) {
			kv, ok := tp.parents[id].(*ast.KeyValueExpr)
			if !ok || kv.Key != id || !tp.in(rowOf, id.Pos()) {
				bad = append(bad, "NF6 BillingRow."+field+" written or read other than as billingRowOf's literal key at "+tp.where(id.Pos()))
				continue
			}
			val, ok := kv.Value.(*ast.Ident)
			v, _ := tp.info.Uses[val].(*types.Var)
			if !ok || v == nil || !tp.in(rowOf, v.Pos()) {
				bad = append(bad, "NF6 BillingRow."+field+"'s value is not a variable of billingRowOf at "+tp.where(kv.Value.Pos()))
				continue
			}
			if found := definedByMoneyText(v, source, val); len(found) != 0 {
				bad = append(bad, found...)
				continue
			}
			rowWrites[field]++
		}
	}
	if rowWrites["Amount"] != 1 || rowWrites["UnitPrice"] != 1 {
		bad = append(bad, fmt.Sprintf("NF6 BillingRow's Amount and UnitPrice written %v time(s) as moneyText's reading, want once each", rowWrites))
	}
	slices.Sort(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if bridges != 1 || len(fieldUses) != 2 || len(rowWrites) != 2 {
		t.Fatalf("CONTROL: %d bridge call(s), %d money field(s) and %d row write(s) resolved; the scan is blind",
			bridges, len(fieldUses), len(rowWrites))
	}
}

// ------------------------------------------------------------- the failures --

// TestBillingScreen_AFailedReadIsAProblemPageNeverAZeroInvoice: CLAUDE.md §4.6 in its money
// form on the operator's screen -- the tenant screen's rule, "a failed read is a problem page,
// never a zero invoice".
//
// PART I -- a store error, the read's time bound (SQLSTATE 57014), an answer for another
// tenant, an answer for another page and an answer of eleven months are each 503 with the
// problem page that says no figure was read; none of the five pages carries "€", "0.00",
// "Amount" or a month's word; each wrote one ERROR line naming the tenant's id and the page and
// not the session's hash (every hash the fake holds a session under is searched). An id no
// tenant has is 404 with its page; a session the read refuses is the sign-in's 303. CONTROL:
// the same tenant read without a fault is 200 and carries "€".
//
// PART II -- the list above. PART III -- no completeness claim.
func TestBillingScreen_AFailedReadIsAProblemPageNeverAZeroInvoice(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	months := billingPageOfEveryState(2026, time.October)
	tenant := g.seedBilling("FAKE Faults Ltd", months...)
	other := uuid.New()
	short := uuid.New()
	g.store.mu.Lock()
	g.store.billing[other] = fakeBilling{name: "FAKE Other Ltd", months: months, asTenant: uuid.New()}
	g.store.billing[short] = fakeBilling{name: "FAKE Short Ltd", months: months[:11]}
	wrongPage := uuid.New()
	g.store.billing[wrongPage] = fakeBilling{name: "FAKE Wrong Page Ltd", months: months, asPage: 2}
	g.store.mu.Unlock()
	if w := g.get(billingPathOf(tenant.String()), c); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "€") {
		t.Fatalf("CONTROL: the tenant without a fault = %d", w.Code)
	}
	for name, cs := range map[string]struct {
		id  uuid.UUID
		err error
	}{
		"a store error":              {tenant, errFakeDB},
		"the read's time bound":      {tenant, errFakeBillingTimeout},
		"an answer for another one":  {other, nil},
		"an answer for another page": {wrongPage, nil},
		"an answer of eleven months": {short, nil},
	} {
		g.store.mu.Lock()
		if cs.err != nil {
			g.store.fail["TenantBilling"] = cs.err
		}
		g.store.mu.Unlock()
		g.logs.Reset()
		w := g.get(billingPathOf(cs.id.String()), c)
		g.store.mu.Lock()
		delete(g.store.fail, "TenantBilling")
		g.store.mu.Unlock()
		body := w.Body.String()
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(body, "This tenant&#39;s billing could not be loaded") ||
			!strings.Contains(body, "No figure was read, so none is shown") {
			t.Errorf("%s: %d, or not the billing's problem page", name, w.Code)
		}
		for _, figure := range []string{"€", "0.00", "Amount", wordFrozen, wordRunning, wordBefore} {
			if strings.Contains(body, html.EscapeString(figure)) {
				t.Errorf("%s: the problem page carries %q", name, figure)
			}
		}
		logs := g.logs.String()
		if n := strings.Count(logs, "operator: a tenant's billing could not be read"); n != 2 ||
			!strings.Contains(logs, "tenant_id="+cs.id.String()) || !strings.Contains(logs, "page=1") {
			t.Errorf("%s: %d fault line(s) across the two formats, or the line does not name the tenant and the page", name, n)
		}
		g.store.mu.Lock()
		hashes := 0
		for h := range g.store.live {
			hashes++
			if strings.Contains(logs, h) {
				t.Errorf("%s: the log carries the session hash", name)
			}
		}
		g.store.mu.Unlock()
		if hashes == 0 {
			t.Fatalf("PREMISE: %s: the fake holds no session hash to search for", name)
		}
	}
	if w := g.get(billingPathOf(uuid.NewString()), c); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "There is no tenant with that id") {
		t.Errorf("an id no tenant has = %d, want 404 and its page", w.Code)
	}
	g.store.mu.Lock()
	g.store.fail["TenantBilling"] = db.ErrOperatorRefused
	g.store.mu.Unlock()
	if res := g.get(billingPathOf(tenant.String()), c).Result(); res.StatusCode != http.StatusSeeOther ||
		res.Header.Get("Location") != "/operator/login" {
		t.Errorf("a session the read refuses = %d %q, want the sign-in's 303", res.StatusCode, res.Header.Get("Location"))
	}
}

// TestBillingScreen_TheBoundaryRefusesBeforeTheStore: the POST's form, checked in
// pageTenantBilling before the store and before the read budget.
//
// PART I -- pages "0", "6", "-1", "+2", " 2", "2 ", "02", "1.5", "1e0", "abc", a full-width
// two, an Arabic-Indic two and 18446744073709551617 (a 64-bit wrap) are each 400 with the page
// refusal, which says the screen has pages 1 to db.MaxTenantBillingPage and nothing was read;
// a body over maxFormBytes is 413; "page=%zz" is 400; a malformed id is 404 on the GET and on
// the POST -- none makes a TenantBilling call; an absent or empty page is page 1, and 1 to 5
// are themselves. maxBillingPage is db.MaxTenantBillingPage and 5 x 12 months is
// billing.HistoryCap.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestBillingScreen_TheBoundaryRefusesBeforeTheStore(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	tenant := g.seedBilling("FAKE Boundary Ltd", append(append(append(append(billingPageOfEveryState(2026, time.October),
		billingBeforeSignupMonths(2025, time.October, 12)...), billingBeforeSignupMonths(2024, time.October, 12)...),
		billingBeforeSignupMonths(2023, time.October, 12)...), billingBeforeSignupMonths(2022, time.October, 12)...)...)
	if operator.MaxBillingPageForTest != db.MaxTenantBillingPage || operator.MaxBillingPageForTest*db.TenantBillingMonthsPerPage != billing.HistoryCap {
		t.Fatalf("maxBillingPage %d, db.MaxTenantBillingPage %d, x %d months, HistoryCap %d", operator.MaxBillingPageForTest,
			db.MaxTenantBillingPage, db.TenantBillingMonthsPerPage, billing.HistoryCap)
	}
	path := billingPathOf(tenant.String())
	before := g.store.count("TenantBilling")
	for _, p := range []string{"0", "6", "-1", "+2", " 2", "2 ", "02", "1.5", "1e0", "abc", "\uff12", "\u0662", "18446744073709551617"} {
		w := g.post(path, url.Values{"page": {p}}, c)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(),
			"A tenant&#39;s billing has pages 1 to "+strconv.Itoa(db.MaxTenantBillingPage)+". Nothing was read") {
			t.Errorf("page %q = %d, want 400 with the page refusal", p, w.Code)
		}
	}
	if w := g.post(path, url.Values{"page": {strings.Repeat("1", 17<<10)}}, c); w.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(w.Body.String(), "Nothing was read") {
		t.Errorf("an oversized form = %d, want 413", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "http://"+opHost+path, strings.NewReader("page=%zz"))
	r.Host = opHost
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", opOrigin)
	r.AddCookie(c)
	r.RemoteAddr = "192.0.2.10:4000"
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("an unreadable form = %d, want 400", w.Code)
	}
	malformed := billingPathOf(strings.ToUpper(strings.ReplaceAll(tenant.String(), "-", "")))
	if w := g.get(malformed, c); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "That link does not name a tenant") {
		t.Errorf("a malformed id (GET) = %d", w.Code)
	}
	if w := g.post(malformed, url.Values{"page": {"2"}}, c); w.Code != http.StatusNotFound {
		t.Errorf("a malformed id (POST) = %d", w.Code)
	}
	if n := g.store.count("TenantBilling") - before; n != 0 {
		t.Errorf("the refusals made %d TenantBilling call(s), want 0", n)
	}
	for _, form := range []url.Values{{}, {"page": {""}}, {"page": {"2"}}, {"page": {"3"}}, {"page": {"4"}}, {"page": {"5"}}} {
		if w := g.post(path, form, c); w.Code != http.StatusOK {
			t.Fatalf("the form %v = %d", form, w.Code)
		}
	}
	var pages []int32
	for _, a := range g.billingAsks() {
		pages = append(pages, a.page)
	}
	if !slices.Equal(pages, []int32{1, 1, 2, 3, 4, 5}) {
		t.Errorf("the store was asked for pages %v, want [1 1 2 3 4 5]", pages)
	}
}

// TestBillingScreen_ReadsNothingFromTheURL: the page travels in the POST's body only
// (billing.go's file comment).
//
// PART I -- GET .../billing?page=3 reaches the store as page 1; a POST to .../billing?page=3
// with an empty body as page 1; a POST whose URL says 3 and whose body says 2 as page 2. The
// pager's forms post to the screen's path with no query, and no link on the page carries a
// page.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestBillingScreen_ReadsNothingFromTheURL(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	tenant := g.seedBilling("FAKE URL Ltd", append(append(billingPageOfEveryState(2026, time.October),
		billingBeforeSignupMonths(2025, time.October, 12)...), billingBeforeSignupMonths(2024, time.October, 12)...)...)
	path := billingPathOf(tenant.String())
	w := g.get(path+"?page=3", c)
	g.post(path+"?page=3", url.Values{}, c)
	w2 := g.post(path+"?page=3", url.Values{"page": {"2"}}, c)
	var pages []int32
	for _, a := range g.billingAsks() {
		pages = append(pages, a.page)
	}
	if !slices.Equal(pages, []int32{1, 1, 2}) {
		t.Errorf("the store was asked for pages %v, want [1 1 2]", pages)
	}
	for _, b := range []string{w.Body.String(), w2.Body.String()} {
		if strings.Contains(b, "page=") || strings.Contains(b, path+"?") {
			t.Error("a link or an action on the page carries a page in its URL")
		}
		if !strings.Contains(b, `<form method="post" action="`+path+`">`) {
			t.Error("the pager does not post to the screen's own path")
		}
	}
}

// ------------------------------------------------------------- the rendering --

// TestBillingScreen_NamesTheTenantAndEscapesWhatItNamed: the banner and the values a tenant
// or a later read could make hostile.
//
// PART I -- the banner and the document title carry the tenant's name escaped, inside the
// banner's bdi; a tenant whose name shows nothing is "Unnamed tenant" and its id; a plan and a
// currency code carrying markup are printed escaped (no "<script" on the page).
//
// PART II -- the list above. PART III -- no completeness claim.
func TestBillingScreen_NamesTheTenantAndEscapesWhatItNamed(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const name = "Rusty <Bar> & \"Grill\" \u202eevil"
	const esc = "Rusty &lt;Bar&gt; &amp; &#34;Grill&#34; \u202eevil"
	odd := billingMonth{y: 2026, m: time.September, state: "frozen", people: 2, amount: "2.20", plan: `<script>x</script>`,
		currency: `<b>X`, closedAt: time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)}
	months := append([]db.TenantBillingMonth{odd.month()}, billingBeforeSignupMonths(2026, time.August, 11)...)
	body, rows := billingPage(t, g, c, g.seedBilling(name, months...))
	banner := strings.Index(body, `<section class="op-tenant" aria-label="Tenant">`)
	if banner < 0 || !strings.Contains(body[banner:], "<bdi>"+esc+"</bdi>") {
		t.Error("the banner does not carry the tenant's name escaped in a bdi")
	}
	if !strings.Contains(body, "<title>Billing — "+esc+" — Taptime operator</title>") {
		t.Error("the document title does not carry the tenant's name escaped")
	}
	if strings.Contains(body, "<script") || strings.Contains(body, "<b>X") || strings.Contains(body, "<Bar>") {
		t.Error("a value was written raw")
	}
	if !strings.Contains(rows[0], fact("Plan", `<script>x</script>`)) || !strings.Contains(rows[0], fact("Amount", "2.20 <b>X")) {
		t.Error("the plan or the amount's currency code is not printed escaped")
	}
	unnamed := g.seedBilling("\u200b \u2060", months...)
	ub, _ := billingPage(t, g, c, unnamed)
	if !strings.Contains(ub, "Unnamed tenant") || !strings.Contains(ub, unnamed.String()) {
		t.Error("a tenant whose name shows nothing is not named by its id")
	}
}

// TestBillingScreen_TimesAreTheMonthsOwnZone: CLAUDE.md §6 -- the database holds UTC, the
// render layer turns it local -- in the zone each month was counted in.
//
// PART I -- a frozen Malta month closed at 10:30 UTC prints its closing at 12:30
// Europe/Malta and its period from 00:00 on the 1st to 00:00 on the next 1st; a frozen month
// counted in Etc/GMT-14 prints both in that zone (the 1st's 00:00 there is the day before in
// UTC); a month whose zone this binary cannot load prints in UTC and says so. No time on the
// page is the UTC reading of a local one.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestBillingScreen_TimesAreTheMonthsOwnZone(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	malta := billingMonth{y: 2026, m: time.September, state: "frozen", people: 2, amount: "2.20",
		closedAt: time.Date(2026, 10, 2, 10, 30, 0, 0, time.UTC)}
	kiritimati := malta
	kiritimati.m, kiritimati.zone, kiritimati.closedAt = time.August, "Etc/GMT-14", time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)
	unknown := malta.month()
	unknown.Month = monthDate(2026, time.July)
	unknownZone := "Mars/Olympus_Mons"
	unknown.Zone = &unknownZone
	months := append([]db.TenantBillingMonth{malta.month(), kiritimati.month(), unknown}, billingBeforeSignupMonths(2026, time.June, 9)...)
	_, rows := billingPage(t, g, c, g.seedBilling("FAKE Zones Ltd", months...))
	for i, want := range [][]string{
		{fact("Closed", "2026-10-02 12:30 Europe/Malta"), fact("Counted over", "2026-09-01 00:00 to 2026-10-01 00:00 Europe/Malta")},
		{fact("Closed", "2026-09-02 01:00 Etc/GMT-14"), fact("Counted over", "2026-08-01 00:00 to 2026-09-01 00:00 Etc/GMT-14")},
		{fact("Closed", "2026-10-02 10:30 UTC")},
	} {
		for _, w := range want {
			if !strings.Contains(rows[i], w) {
				t.Errorf("row %d does not carry %q", i+1, w)
			}
		}
	}
	if strings.Contains(rows[0], "10:30") || strings.Contains(rows[1], "2026-07-31") {
		t.Error("a time is printed in UTC where the month's zone was known")
	}
}

// TestBillingScreen_PagesAreFiveOfTwelve: the pager (billing.templ's billingPager).
//
// PART I -- page 1 offers "Older months" (a form posting page 2 to the screen's path) and no
// "Newer months"; page 3 offers both (2 and 4); page 5 offers only "Newer months" (4) and says
// it is the oldest page, 60 months; each page's heading names its number and 5, in the data
// face, and lists twelve rows.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestBillingScreen_PagesAreFiveOfTwelve(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	months := billingPageOfEveryState(2026, time.October)
	for y := 2025; y >= 2022; y-- {
		months = append(months, billingBeforeSignupMonths(y, time.October, 12)...)
	}
	tenant := g.seedBilling("FAKE Pages Ltd", months...)
	path := billingPathOf(tenant.String())
	form := func(page int, label string) string {
		return `<form method="post" action="` + path + `"><input type="hidden" name="page" value="` + strconv.Itoa(page) +
			`"> <button type="submit" class="op-link">` + label + `</button></form>`
	}
	for _, c2 := range []struct {
		page         int
		older, newer bool
	}{{1, true, false}, {3, true, true}, {5, false, true}} {
		w := g.post(path, url.Values{"page": {strconv.Itoa(c2.page)}}, c)
		body := w.Body.String()
		if w.Code != http.StatusOK || len(billingRowRe.FindAllString(body, -1)) != 12 {
			t.Fatalf("page %d = %d with %d row(s)", c2.page, w.Code, len(billingRowRe.FindAllString(body, -1)))
		}
		if got := strings.Contains(body, form(c2.page+1, "Older months")); got != c2.older {
			t.Errorf("page %d: Older months offered = %v, want %v", c2.page, got, c2.older)
		}
		if got := strings.Contains(body, form(c2.page-1, "Newer months")); got != c2.newer {
			t.Errorf("page %d: Newer months offered = %v, want %v", c2.page, got, c2.newer)
		}
		heading := `page <span class="font-mono">` + strconv.Itoa(c2.page) + `</span> of <span class="font-mono">5</span>`
		if !strings.Contains(body, heading) {
			t.Errorf("page %d: the heading does not say %q", c2.page, heading)
		}
		if got := strings.Contains(body, `This is the oldest page this screen shows: <span class="font-mono">60</span> months`); got != (c2.page == 5) {
			t.Errorf("page %d: the oldest-page sentence present = %v", c2.page, got)
		}
	}
}

// TestBillingScreen_WearsTheDocketAnatomy pins the screen's brand anatomy (skill tappa-brand:
// the kitchen docket; OP-13 and OP-14's anatomy pins are the precedent).
//
// PART I -- the months sit in ONE <section class="docket"> labelled by its heading, and every
// row is inside it; the heading is a docket label with the page numbers in the data face;
// every figure of a row is in the data face; the pager's buttons and (2nd round) the two ways
// back -- to the overview, to the tenants -- are op-link controls; and
// web/static/css/input.css gives op-link the 44 px touch target.
//
// PART II -- red on: the docket replaced or doubled; a row outside it; the heading's class or
// a mono number dropped; a figure out of the data face; a pager button's or a way back's class
// changed; the touch target dropped. PART III -- These classes only (no completeness claim).
func TestBillingScreen_WearsTheDocketAnatomy(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	months := append(billingPageOfEveryState(2026, time.October), billingBeforeSignupMonths(2025, time.October, 24)...)
	tenant := g.seedBilling("FAKE Anatomy Ltd", months...)
	w := g.post(billingPathOf(tenant.String()), url.Values{"page": {"1"}}, c)
	body := w.Body.String()
	const docket = `<section class="docket" aria-labelledby="tenant-billing">`
	start := strings.Index(body, docket)
	if strings.Count(body, docket) != 1 || strings.Count(body, `<section class="docket"`) != 1 || start < 0 {
		t.Fatal("the months are not in one docket section")
	}
	end := start + strings.Index(body[start:], "</section>")
	rows := billingRowRe.FindAllStringIndex(body, -1)
	if len(rows) != 12 {
		t.Fatalf("PREMISE: %d row(s)", len(rows))
	}
	for _, r := range rows {
		if r[0] < start || r[1] > end {
			t.Fatal("a row sits outside the docket")
		}
	}
	if !strings.Contains(body, `<h2 id="tenant-billing" class="docket-label">Newest first · page <span class="font-mono">1</span> of <span class="font-mono">5</span></h2>`) {
		t.Error("the docket's heading is not a docket label with its page numbers in the data face")
	}
	for _, label := range []string{"Plan", "People counted", "Price per person", "Amount", "First charged month", "Closed", "Counted over"} {
		if !regexp.MustCompile(`<span class="text-sm">` + label + ` <span class="font-mono">[^<]+</span></span>`).MatchString(body) {
			t.Errorf("the %s fact is not in the data face", label)
		}
	}
	if !strings.Contains(body, `<button type="submit" class="op-link">Older months</button>`) {
		t.Error("the pager's button is not an op-link button")
	}
	// 2nd round: the screen's two ways back are op-link links too (the 44 px target below).
	for _, link := range []string{`<a href="/operator/tenants/` + tenant.String() + `" class="op-link">Back to the overview</a>`,
		`<a href="/operator/tenants" class="op-link">Back to the tenants</a>`} {
		if !strings.Contains(body, link) {
			t.Errorf("the screen does not carry %s", link)
		}
	}
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	if m := regexp.MustCompile(`(?m)^\s*\.op-link\s*\{\s*@apply ([^;]+);`).FindStringSubmatch(string(css)); m == nil ||
		!slices.Contains(strings.Fields(m[1]), "min-h-11") {
		t.Error("input.css's .op-link rule does not give the 44 px touch target (min-h-11)")
	}
}

// TestBillingScreen_TheChipsAndTextClearAA recomputes, from tailwind.config.js's palette, the
// contrast of the billing screen's text on its grounds (WCAG 2.1 relative luminance, sRGB; a
// translucent token composited on paper, the docket's ground): ink on the four chip grounds
// (green-lite for a frozen month, saffron-lite for one ended and not closed, line at 10% for a
// running month and one before sign-up, ink at 10% for an unreadable one), ink and ink at 70%
// on paper (the rows, the help line), tappa-green on paper (the two ways back, in the intro
// card) and tappa-green on porcelain (the pager, which sits outside the docket on the page's
// ground -- 2nd round: the first version said "on paper" of it, which it is not) -- each
// against AA's 4.5:1 (the chips' words are 11px, the rest 14px and smaller). No ground is
// new: the four are the plaque screen's and the review queue's.
func TestBillingScreen_TheChipsAndTextClearAA(t *testing.T) {
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
		{"ink on green-lite (frozen)", ink, hexOf("green-lite")},
		{"ink on saffron-lite (ended, not closed)", ink, hexOf("saffron-lite")},
		{"ink on line at 10% over paper (running, before sign-up)", ink, over(hexOf("line"), 0.1, paper)},
		{"ink on ink at 10% over paper (unreadable)", ink, over(ink, 0.1, paper)},
		{"ink on paper", ink, paper},
		{"ink at 70% on paper", over(ink, 0.7, paper), paper},
		{"tappa-green on paper (the ways back, in the intro card)", hexOf("tappa-green"), paper},
		{"tappa-green on porcelain (the pager, outside the docket)", hexOf("tappa-green"), hexOf("porcelain")},
	} {
		if got := contrast(c.fg, c.bg); got < 4.5 {
			t.Errorf("%s = %.2f:1, want at least 4.5:1", c.what, got)
		} else {
			t.Logf("%s = %.2f:1", c.what, got)
		}
	}
}

// TestBillingScreen_EachChipHasTheRuleTheContrastTestComputes ties the chips the screen writes
// to the grounds TestBillingScreen_TheChipsAndTextClearAA computes: for each of the five tones
// the billing chip switch renders (read off a render of every tone), web/static/css/input.css
// has exactly one rule whose selector list names its class, and that rule's @apply names the
// ground and the frame of its tone and nothing else (2nd round: a third utility -- the third
// eye's MZ8, text-saffron in the waiting tone, 2.27:1 -- passed the first version) -- the
// brand's fixed mapping. It reads input.css, the committed source.
//
// PART II -- red on: a tone class with no rule, with two rules, or with a rule of another
// ground or frame or of a third utility; two tones drawing one class. PART III -- A rule written outside the
// `selectors { @apply … }` shape this reads, or a later rule that overrides one, is code
// review's -- no completeness claim.
func TestBillingScreen_EachChipHasTheRuleTheContrastTestComputes(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[operatorpages.BillingTone][2]string{ // ground, frame
		operatorpages.BillingToneFrozen:       {"bg-green-lite", "border-tappa-green"},
		operatorpages.BillingToneUnclosed:     {"bg-saffron-lite", "border-saffron"},
		operatorpages.BillingToneRunning:      {"bg-line/10", "border-line"},
		operatorpages.BillingToneBeforeSignup: {"bg-line/10", "border-line"},
		operatorpages.BillingToneUnreadable:   {"bg-ink/10", "border-ink"},
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
		if err := operatorpages.TenantBilling(operatorpages.TenantBillingView{Name: mustName(t, "FAKE Tone Ltd"), Page: 1, LastPage: 5,
			MonthsPerPage: 12, Rows: []operatorpages.BillingRow{{Month: "M", Label: "L", Sentence: "S", Tone: tone}}}).Render(t.Context(), &b); err != nil {
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

// inkRGB is ink #152219 as the Tailwind standalone CLI writes a colour (internal/handler's
// TestCompiledCSS_StampWordIsInk reads the same spelling).
const inkRGB = "rgb(21 34 25"

// tallyWordUtility is an @apply utility of the chip's base rule that is allowed to start with
// "text-": the word's ink and the arbitrary size.
func tallyWordUtility(u string) bool { return u == "text-ink" || strings.HasPrefix(u, "text-[") }

// TestTallyRules_NoToneRuleColoursTheWord pins, in web/static/css/input.css (the committed
// source; CI reads it), that every chip of the product -- the review queue's, the people's, the
// operator's plaques (OP-13) and billing months (OP-12) -- writes its WORD in the base rule's
// ink, and its tone only as a frame and a ground. The brand decision is the stamp's
// (TestCompiledCSS_StampWordIsInk: the word is ink, the status colour is the frame); a chip's
// word is 11px, so on a tone's own colour it misses AA (saffron on saffron-lite 2.27:1, the
// third eye's MZ8, which the first version of this screen's tests let through).
//
// Threat model: an ACCIDENTAL drift -- a tone rule that gains a text colour. A rule written to
// recolour the word without naming a chip (.docket * …, an attribute selector) is for code
// review, as is a utility class added to a chip in a template other than this screen's (whose
// chip markup TestBillingScreen_EveryStateSaysItsWordAndNoVerdict pins whole).
//
// PART I -- comments stripped, every flat rule whose selector text names `.tally` is either the
// base rule (its selector exactly `.tally`; exactly one; its @apply carries text-ink and no
// other text- utility but an arbitrary size, and no opacity utility) or a tone rule (every
// selector of its list exactly `.tally--<name>`; its body one @apply of border- and bg-
// utilities only -- no text-, no opacity, no declaration of its own); and the tone rules name,
// among others, the five billing tones and OP-13's five plaque ones. CONTROL: a tone rule with
// a text-saffron utility added, read by the same function, is a finding.
//
// PART II -- red on: a tone rule with a text-, opacity- or other non-frame non-ground utility, or
// a declaration of its own (color: …); a second base rule, or one without text-ink or with
// another colour; a rule that names a chip in any other shape (a compound or descendant
// selector); one of the ten tones missing. PART III -- the escapes in the threat model;
// TestCompiledCSS_TallyWordIsInkOnEveryChip reads the compiled half -- no completeness claim.
func TestTallyRules_NoToneRuleColoursTheWord(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	tones, findings := readTallyRules(string(css))
	for _, f := range findings {
		t.Error(f)
	}
	for _, want := range []string{"frozen", "running", "unclosed", "before-signup", "unreadable",
		"mounted", "unencoded", "stock", "withdrawn", "unrecognised"} {
		if !tones[want] {
			t.Errorf("no tone rule names .tally--%s (read: %v)", want, slices.Sorted(maps.Keys(tones)))
		}
	}
	mutated := strings.Replace(string(css), "bg-saffron-lite; }", "bg-saffron-lite text-saffron; }", 1)
	if mutated == string(css) {
		t.Fatal("CONTROL: the waiting tone's rule is not where this test looks for it")
	}
	if _, f := readTallyRules(mutated); len(f) == 0 {
		t.Error("CONTROL: a tone rule with text-saffron added is not a finding; the reading is blind")
	}
}

// readTallyRules is TestTallyRules_NoToneRuleColoursTheWord's reading of input.css: the tones
// its rules name and every rule naming a chip that is not of the two allowed shapes.
func readTallyRules(css string) (map[string]bool, []string) {
	src := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	rule := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	names := regexp.MustCompile(`\.tally\b`)
	tone := regexp.MustCompile(`^\.tally--([a-z][a-z0-9-]*)$`)
	apply := regexp.MustCompile(`^@apply ([^;]+);$`)
	tones := map[string]bool{}
	var findings []string
	base := 0
	for _, m := range rule.FindAllStringSubmatch(src, -1) {
		sel, body := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		if !names.MatchString(sel) {
			continue
		}
		a := apply.FindStringSubmatch(body)
		if sel == ".tally" {
			base++
			if a == nil || !slices.Contains(strings.Fields(a[1]), "text-ink") {
				findings = append(findings, "the chip's base rule does not apply text-ink: .tally { "+body+" }")
				continue
			}
			for _, u := range strings.Fields(a[1]) {
				if (strings.HasPrefix(u, "text-") && !tallyWordUtility(u)) || strings.Contains(u, "opacity") {
					findings = append(findings, "the chip's base rule applies "+u)
				}
			}
			continue
		}
		var list []string
		for _, s := range strings.Split(sel, ",") {
			tm := tone.FindStringSubmatch(strings.TrimSpace(s))
			if tm == nil {
				findings = append(findings, "a rule names a chip in a shape this reading does not allow: "+sel)
				list = nil
				break
			}
			list = append(list, tm[1])
		}
		if list == nil {
			continue
		}
		if a == nil {
			findings = append(findings, "a tone rule is not one @apply: "+sel+" { "+body+" }")
			continue
		}
		ok := true
		for _, u := range strings.Fields(a[1]) {
			if !(strings.HasPrefix(u, "border-") || strings.HasPrefix(u, "bg-")) || strings.Contains(u, "opacity") {
				findings = append(findings, "a tone rule applies "+u+", which is neither a frame nor a ground: "+sel)
				ok = false
			}
		}
		if ok {
			for _, n := range list {
				tones[n] = true
			}
		}
	}
	if base != 1 {
		findings = append(findings, fmt.Sprintf("%d base rule(s) for the chip, want 1", base))
	}
	return tones, findings
}

// TestCompiledCSS_TallyWordIsInkOnEveryChip is TestTallyRules_NoToneRuleColoursTheWord's
// compiled half, on internal/handler's TestCompiledCSS_StampWordIsInk's reading: in
// web/static/css/app.css every flat rule whose selector text names `.tally` declares, if it
// declares `color:` at all, ink; a rule whose selectors are all `.tally--<name>` declares no
// `color:`, no `--tw-text-opacity` and no `opacity:`; exactly one rule is the base `.tally`,
// and it declares ink. The tones the screen's chips use -- the five billing ones and OP-13's
// five -- each have a compiled rule.
//
// Threat model and escapes as in TestTallyRules_NoToneRuleColoursTheWord, plus the stamp test's
// LIMIT 5 (a cascade this scan does not resolve). 🔴 IT SKIPS WHEN app.css IS ABSENT, which it
// is on CI (.gitignore keeps the compiled sheet out; ci.yml builds no CSS) -- a skip is not a
// pass; the source half above is the one CI runs.
//
// PART II -- red on: a tone rule declaring a colour or a text opacity (the third eye's MZ8
// compiles to exactly that); a base rule of another colour, or none, or two; a tone missing.
// PART III -- no completeness claim.
func TestCompiledCSS_TallyWordIsInkOnEveryChip(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "app.css"))
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) -- run `make css`. THIS IS NOT A PASS.", err)
	}
	rule := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	names := regexp.MustCompile(`\.tally\b`)
	tone := regexp.MustCompile(`^\.tally--([a-z][a-z0-9-]*)$`)
	colour := regexp.MustCompile(`(?:^|;)color:([^;]*)`)
	tones := map[string]bool{}
	base := 0
	for _, m := range rule.FindAllStringSubmatch(string(raw), -1) {
		sel, decls := strings.TrimSpace(m[1]), m[2]
		if !names.MatchString(sel) {
			continue
		}
		for _, c := range colour.FindAllStringSubmatch(decls, -1) {
			if !strings.Contains(c[1], inkRGB) {
				t.Errorf("a rule naming a chip colours the word other than ink: %s{%s}", sel, decls)
			}
		}
		if sel == ".tally" {
			base++
			if !colour.MatchString(decls) {
				t.Errorf("the chip's base rule declares no colour: .tally{%s}", decls)
			}
			continue
		}
		var list []string
		for _, s := range strings.Split(sel, ",") {
			tm := tone.FindStringSubmatch(strings.TrimSpace(s))
			if tm == nil {
				list = nil
				break
			}
			list = append(list, tm[1])
		}
		if list == nil {
			continue // a compound shape: only its colour (above) is read
		}
		if colour.MatchString(decls) || strings.Contains(decls, "--tw-text-opacity") || regexp.MustCompile(`(?:^|;)opacity:`).MatchString(decls) {
			t.Errorf("a tone rule sets the word's colour or opacity: %s{%s}", sel, decls)
		}
		for _, n := range list {
			tones[n] = true
		}
	}
	if base != 1 {
		t.Errorf("%d compiled base rule(s) for the chip, want 1", base)
	}
	for _, want := range []string{"frozen", "running", "unclosed", "before-signup", "unreadable",
		"mounted", "unencoded", "stock", "withdrawn", "unrecognised"} {
		if !tones[want] {
			t.Errorf("the compiled CSS has no rule for .tally--%s", want)
		}
	}
}

// ----------------------------------------------------------------- the budget --

// TestBillingBudget_EachViewIsOneReadOfTheSessionsSharedBudget measures surface.go's readLimit
// (60 reads per session per window) on the billing screen beside another screen, and
// sessionLimit (100 requests) beside it -- the OP-12 re-count of the two numbers.
//
// PART I -- one session, from a fresh address per request: 20 plaque views, 20 billing GETs
// and 20 billing POSTs (page 2), interleaved -- 60 x 200, 40 TenantBilling calls; then one
// more billing GET and one more POST are each 429 with the predicate run
// (TouchOperatorSession +1) and no TenantBilling call, each the SIGNED-IN form of the page;
// then 38 console views are 200 (62 + 38 = 100 requests) and the 101st request is 429 at the
// gate. Refusals that spend no read: a second session's 5 malformed-id GETs and POSTs, 5 pages
// refused (0 and 6), 5 bodies over maxFormBytes and 5 unreadable forms are 404, 404, 400, 413
// and 400 with no store call, and the session still makes 30 GETs and 30 POSTs -- all 200 --
// before its 61st view, a GET, is 429.
//
// PART II -- the counts above. PART III -- These sequences only (no completeness claim).
func TestBillingBudget_EachViewIsOneReadOfTheSessionsSharedBudget(t *testing.T) {
	g := newRig(t)
	plaques := g.seedPlaques("FAKE Billing Budget Ltd", plaqueAt("04A1B2C3D4E5B1", "unassigned", nil, nil, nil))
	tenant := g.seedBilling("FAKE Billing Budget Ltd", append(billingPageOfEveryState(2026, time.October),
		billingBeforeSignupMonths(2025, time.October, 12)...)...)
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
	path := billingPathOf(tenant.String())
	billingGet := func(c *http.Cookie) req { return req{method: http.MethodGet, path: path, cookies: []*http.Cookie{c}} }
	billingPost := func(c *http.Cookie) req {
		return req{method: http.MethodPost, path: path, form: url.Values{"page": {"2"}}, cookies: []*http.Cookie{c}}
	}
	plaque := func(c *http.Cookie) req {
		return req{method: http.MethodGet, path: plaquePath(plaques.String()), cookies: []*http.Cookie{c}}
	}
	a := g.signIn(g.active())
	for i := 0; i < 20; i++ {
		for _, rq := range []req{plaque(a), billingGet(a), billingPost(a)} {
			if w := send(rq); w.Code != http.StatusOK {
				t.Fatalf("read %d of one session (%s %s) = %d, want 200", 3*i+1, rq.method, rq.path, w.Code)
			}
		}
	}
	if got := g.store.count("TenantBilling"); got != 40 {
		t.Fatalf("PREMISE: %d billing read(s) for 40 billing views", got)
	}
	for _, rq := range []req{billingGet(a), billingPost(a)} {
		touches, reads := g.store.count("TouchOperatorSession"), g.store.count("TenantBilling")
		w := send(rq)
		if w.Code != http.StatusTooManyRequests || !signedInTooMany(w.Body.String()) {
			t.Errorf("the 61st read (%s) = %d, want 429 with the signed-in page", rq.method, w.Code)
		}
		if g.store.count("TouchOperatorSession") != touches+1 || g.store.count("TenantBilling") != reads {
			t.Errorf("%s refused by the read budget: predicate +%d, TenantBilling +%d; want +1, +0", rq.method,
				g.store.count("TouchOperatorSession")-touches, g.store.count("TenantBilling")-reads)
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
	malformed := billingPathOf(strings.ReplaceAll(tenant.String(), "-", ""))
	sendRaw := func(body string) *httptest.ResponseRecorder {
		n++
		r := httptest.NewRequest(http.MethodPost, "http://"+opHost+path, strings.NewReader(body))
		r.Host = opHost
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", opOrigin)
		r.AddCookie(b)
		r.RemoteAddr = fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1)
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w
	}
	before := g.store.count("TenantBilling")
	for i := 0; i < 5; i++ {
		page := "0"
		if i%2 == 1 {
			page = "6"
		}
		if w := send(req{method: http.MethodGet, path: malformed, cookies: []*http.Cookie{b}}); w.Code != http.StatusNotFound {
			t.Fatalf("a malformed-id GET = %d", w.Code)
		}
		if w := send(req{method: http.MethodPost, path: malformed, form: url.Values{"page": {"2"}}, cookies: []*http.Cookie{b}}); w.Code != http.StatusNotFound {
			t.Fatalf("a malformed-id POST = %d", w.Code)
		}
		for _, rf := range []struct {
			body string
			want int
		}{{"page=" + page, 400}, {"page=" + strings.Repeat("1", 16<<10), 413}, {"page=%zz", 400}} {
			if w := sendRaw(rf.body); w.Code != rf.want {
				t.Fatalf("a refused billing form = %d, want %d", w.Code, rf.want)
			}
		}
	}
	if got := g.store.count("TenantBilling") - before; got != 0 {
		t.Fatalf("PREMISE: the 25 refusals made %d billing read(s), want 0", got)
	}
	for i := 0; i < 30; i++ {
		for _, rq := range []req{billingGet(b), billingPost(b)} {
			if w := send(rq); w.Code != http.StatusOK {
				t.Fatalf("billing view %d after 25 refusals = %d, want 200 -- a refusal spent a read", 2*i+1, w.Code)
			}
		}
	}
	if w := send(billingGet(b)); w.Code != http.StatusTooManyRequests {
		t.Errorf("the 61st billing view after 25 refusals = %d, want 429", w.Code)
	}
}

// slowestBillingRead is the slowest billing read measured (ADR 0021's OP-12 note, L5: twelve
// live months of the development database's largest roster, 4.6-6.1 s; the third eye's
// separate runs 2.2-2.5 s).
const slowestBillingRead = 6100 * time.Millisecond

// TestBillingRead_TheBoundFiresBeforeTheRequestDeadline holds internal/db's time bound of the
// billing read (db.TenantBillingReadTimeout) to the arithmetic its comment gives: at least
// twice the slowest read measured, and at most half the request's own deadline
// (httpx.RequestTimeout) -- so a slow read is the database's 57014 and the screen's designed
// 503, with room for the session predicate, phase one and the render, and never the router's
// deadline. (That the bound is SET LOCAL in the read's own transaction is internal/db's
// TestTenantBilling_ThePhaseTwoTransactionCarriesItsOwnTimeBound; that a read past it is a 503
// with its ticket unconsumed is op12_db_test.go's TestE2E_ASlowBillingReadIsA503AndLeavesItsTicketUnconsumed.)
func TestBillingRead_TheBoundFiresBeforeTheRequestDeadline(t *testing.T) {
	if 2*db.TenantBillingReadTimeout > httpx.RequestTimeout {
		t.Errorf("2 x the billing read's bound (%v) is past the request's deadline (%v)", db.TenantBillingReadTimeout, httpx.RequestTimeout)
	}
	if db.TenantBillingReadTimeout < 2*slowestBillingRead {
		t.Errorf("the billing read's bound (%v) is under twice the slowest read measured (%v)", db.TenantBillingReadTimeout, slowestBillingRead)
	}
}
