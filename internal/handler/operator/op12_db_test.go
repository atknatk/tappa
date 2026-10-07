package operator_test

// op12_db_test.go -- M10 OP-12 phase B end to end against PostgreSQL: the operator signs in
// through the surface and reads one tenant's billing months through op_begin_read +
// op_read_tenant_billing (00032), on newLegalE2E's committed rig (op10_db_test.go: one owner
// connection switched to tappa_operator, each statement its own committed transaction --
// the read's second phase refuses a ticket whose transaction has not committed; the billing
// read's second phase opens its own transaction on it for its time bound).
//
// WHAT ONE RUN LEAVES BEHIND. TestE2E_BillingScreenIsTheTenantsOwnFigureMonthByMonth COMMITS a
// fixture tenant -- the billing screen reads committed rows through the operator's own
// connection, and a frozen month is a row nobody may delete (billing_periods is append-only,
// 00016) -- on tappa_app's pool through the tenant's own paths (internal/domain/billing's
// fixture shape: the rows in the tenant's context, the commercial terms by the owner, the two
// closes by billing.Book.Close): 1 tenant ('FAKE op12b …'), 1 location, 1 owner account
// (an unusable password), 5 employees, 2 billing_periods rows and their 2 audit_log rows.
// The class is the billing package's own (its database tests commit the same per test) and the
// backlog's T81. Both tests leave, by construction, one operator account (op10b-…,
// newLegalE2E's; disabled at cleanup), its sessions (revoked at cleanup) and its audit rows --
// login, logout and the 'read' rows they count; read tickets are deleted at cleanup.

import (
	"context"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/test/fixtures"
)

// billingFixture is one committed tenant whose billing months take every state on the
// screen's first page, and the tenant's own Book over tappa_app's pool.
type billingFixture struct {
	id   uuid.UUID
	name string
	book *billing.Book
	zone *time.Location
	// cur is the month the tenant is in when the fixture was built; paid the frozen charged
	// month (cur-2), free the frozen free one (cur-3).
	cur, paid, free billing.Month
}

// newBillingFixture commits the tenant (the file header lists what it leaves): a founding
// tenant in Europe/Malta that signed up on the 15th of the month five before the current
// one, at 1.10 a person; two people active since sign-up, one who left on the 10th of the
// month three back, and one whose status disagrees with its dates (never counted, the
// floor); the free month cur-3 and the first charged month cur-2 closed by the product's own
// Book.Close; AFTER the closes a fifth person joins (active since sign-up) and the price
// becomes 1.30 -- so the frozen figures are not today's.
func newBillingFixture(t *testing.T, l *legalE2E) *billingFixture {
	t.Helper()
	appDSN := os.Getenv("DATABASE_URL")
	app, err := db.New(l.ctx, &config.Config{DatabaseURL: appDSN})
	if err != nil {
		t.Fatalf("tappa_app's pool: %v", err)
	}
	t.Cleanup(app.Close)
	trail, err := audit.New(app)
	if err != nil {
		t.Fatal(err)
	}
	book, err := billing.NewBook(app, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	f := &billingFixture{id: uuid.New(), book: book, zone: malta}
	f.name = "FAKE op12b E2E " + f.id.String()[:8]
	f.cur = billing.MonthOf(time.Now(), malta)
	f.paid, f.free = f.cur.Add(-2), f.cur.Add(-3)
	signup := f.cur.Add(-5)
	signedUp := time.Date(signup.Year, signup.Month, 15, 12, 0, 0, 0, malta)
	left := f.cur.Add(-3)
	leftAt := time.Date(left.Year, left.Month, 10, 17, 0, 0, 0, malta)
	location, admin := uuid.New(), uuid.New()
	ownerAddress := "op12b-owner-" + f.id.String() + "@billing.example"
	unusable := fixtures.UnusablePasswordHash
	hire := func(ctx context.Context, tx pgx.Tx, name, status string, activated, deactivated *time.Time) error {
		_, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status, activated_at, deactivated_at)
		                      VALUES ($1, $2, $3, $4, $5, $6, $7)`, uuid.New(), f.id, location, name, status, activated, deactivated)
		return e
	}
	err = app.WithTenant(l.ctx, f.id, func(ctx context.Context, tx pgx.Tx) error {
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			  VALUES ($1, $2, $3, 'restaurant', 'multi', 'Europe/Malta')`, []any{f.id, f.name, "VAT-" + f.id.String()}},
			{`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng) VALUES ($1, $2, 'FAKE St Julians', 35.918, 14.489)`,
				[]any{location, f.id}},
			{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'FAKE Owner', $3, $4, 'owner')`,
				[]any{admin, f.id, ownerAddress, unusable}},
		} {
			if _, e := tx.Exec(ctx, s.sql, s.args...); e != nil {
				return e
			}
		}
		for _, e := range []error{
			hire(ctx, tx, "FAKE Person A", "active", &signedUp, nil),
			hire(ctx, tx, "FAKE Person B", "active", &signedUp, nil),
			hire(ctx, tx, "FAKE Person C", "deactivated", &signedUp, &leftAt),
			hire(ctx, tx, "FAKE Person D", "active", nil, nil), // disagrees with its dates: the floor
		} {
			if e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("commit the billing fixture: %v", err)
	}
	// The operator's half: the plan, the sign-up instant and the price (00016 keeps them from
	// tappa_app).
	if _, err := l.owner.Exec(l.ctx, `UPDATE tenants SET plan = 'founding', created_at = $2, price_per_employee_month = 1.10
	                                   WHERE id = $1`, f.id, signedUp); err != nil {
		t.Fatalf("set the commercial terms: %v", err)
	}
	for _, m := range []billing.Month{f.free, f.paid} {
		if _, err := book.Close(l.ctx, f.id, m, admin); err != nil {
			t.Fatalf("close %s through the product's own path: %v", m, err)
		}
	}
	if err := app.WithTenant(l.ctx, f.id, func(ctx context.Context, tx pgx.Tx) error {
		return hire(ctx, tx, "FAKE Person E", "active", &signedUp, nil)
	}); err != nil {
		t.Fatalf("hire after the closes: %v", err)
	}
	if _, err := l.owner.Exec(l.ctx, `UPDATE tenants SET price_per_employee_month = 1.30 WHERE id = $1`, f.id); err != nil {
		t.Fatalf("change the price after the closes: %v", err)
	}
	return f
}

// screenRow is one month of the screen as the page says it.
type screenRow struct {
	month billing.Month
	word  string
	facts map[string]string
	free  bool
	floor string
}

var (
	screenChip  = regexp.MustCompile(`<span class="tally tally--[a-z-]+">([^<]*)</span>`)
	screenFact  = regexp.MustCompile(`<span class="text-sm">([A-Za-z ]+) <span class="font-mono">([^<]*)</span></span>`)
	screenMonth = regexp.MustCompile(`^\s*<span class="font-mono font-bold">([^<]*)</span>`)
	screenFloor = regexp.MustCompile(`The count is a FLOOR: <span class="font-mono">([0-9]+)</span>`)
)

// readScreen parses the rows of a billing page.
func readScreen(t *testing.T, body string) []screenRow {
	t.Helper()
	var out []screenRow
	for _, m := range billingRowRe.FindAllStringSubmatch(body, -1) {
		r := m[1]
		mm, cm := screenMonth.FindStringSubmatch(r), screenChip.FindStringSubmatch(r)
		if mm == nil || cm == nil {
			t.Fatalf("a row without a month or a chip: %s", r)
		}
		at, err := time.Parse("January 2006", mm[1])
		if err != nil {
			t.Fatalf("the month %q: %v", mm[1], err)
		}
		row := screenRow{month: billing.Month{Year: at.Year(), Month: at.Month()}, word: html.UnescapeString(cm[1]), facts: map[string]string{},
			free: strings.Contains(r, "Inside the founding offer")}
		for _, f := range screenFact.FindAllStringSubmatch(r, -1) {
			row.facts[f[1]] = html.UnescapeString(f[2])
		}
		if fm := screenFloor.FindStringSubmatch(r); fm != nil {
			row.floor = fm[1]
		}
		out = append(out, row)
	}
	return out
}

// eur is the screen's spelling of an amount the tenant's Book returns (billing.Money: EUR).
func eur(t *testing.T, m billing.Money) string {
	t.Helper()
	if m.Currency() != "EUR" {
		t.Fatalf("PREMISE: the fixture's money is %s, not EUR", m.Currency())
	}
	return "€" + m.String()
}

// TestE2E_BillingScreenIsTheTenantsOwnFigureMonthByMonth is OP-12's end-to-end acceptance on
// PostgreSQL (the file header says what it commits) -- "tutarlar tenant önizlemesiyle aynı":
//
//  1. the tenant's overview links its billing screen by its canonical path;
//  2. page 1 (GET) and page 2 (POST page=2): 200; the banner carries the tenant's name; twelve
//     rows each, newest first from the month the tenant is in -- the fixture's, or the next one
//     if a month turned between the fixture (Go's clock) and the read (the database's), the
//     rows then aligned on it (2nd round), never any other and never back between the pages;
//     for EVERY month of both pages, the row is the tenant's own Book.Period for that
//     month on tappa_app's pool (the tenant's own path, internal/domain/billing): the state's
//     word (frozen ⇔ Frozen; before sign-up ⇔ !AfterSignup; ended and not closed ⇔ !Frozen,
//     AfterSignup and HasEnded; running otherwise -- HasEnded is not compared for a month that
//     ended between the screen's read and the Book's), and for a month after sign-up the count,
//     the price and the amount (money's exact digits), the plan, the free window, the floor's
//     number, the first charged month (live) or the closing time (frozen) and the period, in the
//     month's zone; PREMISES: page 1 holds all four states, the frozen charged month's figures
//     are not today's preview (the roster and the price changed after the close: the frozen row
//     was not recomputed), a live month carries the floor (one disagreeing record);
//  3. exactly ONE committed 'read' row per view -- scope tenant_billing, the tenant named, the
//     page and 12, detail {} -- and one consumed ticket per row, none unconsumed;
//  4. the screen writes nothing: the tenant's billing_periods rows before = after;
//  5. sessions the predicate refuses -- never MFA-stamped, revoked, idle 31 minutes -- get the
//     sign-in's 303 on the GET and the POST and leave no row; CONTROL: a planted live session
//     reads the screen.
func TestE2E_BillingScreenIsTheTenantsOwnFigureMonthByMonth(t *testing.T) {
	l := newLegalE2E(t)
	fx := newBillingFixture(t, l)
	sess := l.signIn(l.f)
	path := "/operator/tenants/" + fx.id.String() + "/billing"
	periods := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM billing_periods WHERE tenant_id = $1`, fx.id)
	}
	reads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                   AND target_scope = 'tenant_billing'`, l.f.id)
	}
	allReads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1`, l.f.id)
	}
	frozenBefore := periods()
	if frozenBefore != 2 {
		t.Fatalf("PREMISE: the fixture holds %d frozen month(s), want 2", frozenBefore)
	}

	// 1. The overview links the screen.
	if w := l.get("/operator/tenants/"+fx.id.String(), sess); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `<a href="`+path+`" class="op-link">See this tenant's billing</a>`) {
		t.Fatalf("the overview = %d, or it does not link %s", w.Code, path)
	}

	// 2. Two pages, month by month against the tenant's own Book.
	before, allBefore := reads(), allReads()
	states := map[string]int{}
	// turned is how many months the clock has turned since the fixture took fx.cur from Go's
	// clock: the read takes the current month from the database's, later. A month that turns in
	// between (the last moments of a month in Malta) moves the screen's first row to fx.cur+1;
	// the rows are then aligned on the screen's own current month, which may be fx.cur or the
	// one after it and never anything else, and never move back between the pages (2nd round:
	// the first version compared against fx.cur and was red in that window).
	turned := 0
	for page := 1; page <= 2; page++ {
		readAt := time.Now()
		var w *httptest.ResponseRecorder
		if page == 1 {
			w = l.get(path, sess)
		} else {
			w = l.post(path, url.Values{"page": {"2"}}, sess)
		}
		if w.Code != http.StatusOK {
			t.Fatalf("page %d = %d; log: %s", page, w.Code, l.logs.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "<title>Billing — "+html.EscapeString(fx.name)+" — Taptime operator</title>") {
			t.Errorf("page %d's title does not carry the tenant's name", page)
		}
		rows := readScreen(t, body)
		if len(rows) != db.TenantBillingMonthsPerPage {
			t.Fatalf("page %d lists %d month(s), want %d", page, len(rows), db.TenantBillingMonthsPerPage)
		}
		switch current := rows[0].month.Add(12 * (page - 1)); {
		case current == fx.cur.Add(turned):
		case turned == 0 && current == fx.cur.Add(1):
			turned = 1
			t.Logf("the month turned between the fixture and page %d's read; the rows are aligned on %s", page, current)
		default:
			t.Fatalf("page %d opens with %s: the current month is %s, neither the fixture's %s nor (a month turning) the next",
				page, rows[0].month, current, fx.cur.Add(turned))
		}
		for i, r := range rows {
			want := fx.cur.Add(turned - 12*(page-1) - i)
			if r.month != want {
				t.Fatalf("page %d row %d is %s, want %s (the tenant's own months, newest first)", page, i+1, r.month, want)
			}
			d, err := fx.book.Period(l.ctx, fx.id, r.month)
			if err != nil {
				t.Fatalf("the tenant's own Period for %s: %v", r.month, err)
			}
			states[r.word]++
			endedBetween := !d.Frozen && d.To.After(readAt) && !d.To.After(time.Now())
			var word string
			switch {
			case d.Frozen:
				word = "Frozen"
			case !d.AfterSignup:
				word = "Before sign-up"
			case d.HasEnded:
				word = "Ended, not closed by the business"
			default:
				word = "Live — month running"
			}
			if r.word != word && !endedBetween {
				t.Errorf("%s: the screen says %q, the tenant's own path %q", r.month, r.word, word)
			}
			if !d.AfterSignup {
				if len(r.facts) != 0 || r.free || r.floor != "" {
					t.Errorf("%s, before sign-up, carries a figure: %v", r.month, r.facts)
				}
				continue
			}
			zone, err := time.LoadLocation(d.Zone)
			if err != nil {
				t.Fatal(err)
			}
			local := func(at time.Time) string { return at.In(zone).Format("2006-01-02 15:04") }
			wantFacts := map[string]string{
				"Plan": d.Plan, "People counted": strconv.Itoa(d.EmployeeCount),
				"Price per person": eur(t, d.UnitPrice), "Amount": eur(t, d.AmountDue),
				"Counted over": local(d.From) + " to " + local(d.To) + " " + d.Zone,
			}
			if d.Frozen {
				wantFacts["Closed"] = local(d.ClosedAt) + " " + d.Zone
			} else {
				wantFacts["First charged month"] = time.Date(d.FirstChargeableMonth.Year, d.FirstChargeableMonth.Month, 1, 0, 0, 0, 0,
					time.UTC).Format("January 2006")
			}
			if len(r.facts) != len(wantFacts) {
				t.Errorf("%s: the screen's facts %v, want %v", r.month, r.facts, wantFacts)
			}
			for k, v := range wantFacts {
				if r.facts[k] != v {
					t.Errorf("%s: %s is %q on the screen and %q on the tenant's own path", r.month, k, r.facts[k], v)
				}
			}
			if r.free != d.Free {
				t.Errorf("%s: the free window on the screen %v, on the tenant's own path %v", r.month, r.free, d.Free)
			}
			wantFloor := ""
			if d.UnstampedEmployees > 0 {
				wantFloor = strconv.Itoa(d.UnstampedEmployees)
			}
			if r.floor != wantFloor {
				t.Errorf("%s: the floor on the screen %q, on the tenant's own path %q", r.month, r.floor, wantFloor)
			}
		}
	}
	for _, w := range []string{"Frozen", "Live — month running", "Ended, not closed by the business", "Before sign-up"} {
		if states[w] == 0 {
			t.Errorf("PREMISE: no month of the two pages is %q", w)
		}
	}
	frozen, err := fx.book.Period(l.ctx, fx.id, fx.paid)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := fx.book.Preview(l.ctx, fx.id, fx.paid)
	if err != nil {
		t.Fatal(err)
	}
	if !frozen.Frozen || frozen.EmployeeCount == preview.EmployeeCount || frozen.UnitPrice == preview.UnitPrice ||
		frozen.AmountDue == preview.AmountDue {
		t.Errorf("PREMISE: the frozen charged month (%d x %s = %s) is not apart from today's preview (%d x %s = %s)",
			frozen.EmployeeCount, frozen.UnitPrice, frozen.AmountDue, preview.EmployeeCount, preview.UnitPrice, preview.AmountDue)
	}
	if cur, err := fx.book.Period(l.ctx, fx.id, fx.cur.Add(turned)); err != nil || cur.UnstampedEmployees == 0 {
		t.Errorf("PREMISE: the current month carries no disagreeing record (%v)", err)
	}

	// 3. One 'read' row per view; one consumed ticket per row.
	if n := reads() - before; n != 2 {
		t.Errorf("the two views wrote %d tenant_billing 'read' row(s), want 2", n)
	}
	if n, all := reads()-before, allReads()-allBefore; all != n {
		t.Errorf("the views wrote %d 'read' row(s) of another scope", all-n)
	}
	var target uuid.UUID
	var page, size int
	var detail string
	if err := l.owner.QueryRow(l.ctx, `SELECT target_tenant_id, page_number, page_size, detail::text FROM operator_audit_log
	                                    WHERE kind = 'read' AND actor_admin_id = $1 AND target_scope = 'tenant_billing'
	                                    ORDER BY at DESC, id DESC LIMIT 1`, l.f.id).Scan(&target, &page, &size, &detail); err != nil {
		t.Fatalf("the last billing 'read' row: %v", err)
	}
	if target != fx.id || page != 2 || size != 12 || detail != `{}` {
		t.Errorf("the POST's row names %s, page %d of %d, detail %s; want the tenant, 2, 12, {}", target, page, size, detail)
	}

	// 5. Dead sessions.
	n := allReads()
	for name, c := range map[string]*http.Cookie{
		"never MFA-stamped": l.plantSession(false, ""),
		"revoked":           l.plantSession(true, "revoked_at = clock_timestamp()"),
		"idle 31 minutes":   l.plantSession(true, "last_used_at = clock_timestamp() - interval '31 minutes'"),
	} {
		if res := l.get(path, c).Result(); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/operator/login" {
			t.Errorf("%s session: the GET = %d %q, want the sign-in's 303", name, res.StatusCode, res.Header.Get("Location"))
		}
		if res := l.post(path, url.Values{"page": {"2"}}, c).Result(); res.StatusCode != http.StatusSeeOther ||
			res.Header.Get("Location") != "/operator/login" {
			t.Errorf("%s session: the POST = %d %q, want the sign-in's 303", name, res.StatusCode, res.Header.Get("Location"))
		}
	}
	if got := allReads(); got != n {
		t.Errorf("the dead sessions wrote %d 'read' row(s)", got-n)
	}
	if w := l.get(path, l.plantSession(true, "")); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: a planted live MFA-stamped session = %d on the billing screen, want 200", w.Code)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.consumed_at IS NULL`, l.f.id); n != 0 {
		t.Errorf("%d unconsumed read ticket(s), want 0", n)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.kind = 'tenant_billing'`, l.f.id); n != reads() {
		t.Errorf("%d tenant_billing ticket(s), want one per tenant_billing 'read' row (%d)", n, reads())
	}

	// 4. The screen wrote nothing.
	if got := periods(); got != frozenBefore {
		t.Errorf("the tenant has %d billing_periods row(s) after the screen, %d before", got, frozenBefore)
	}
	if w := l.post("/operator/logout", nil, sess); w.Code != http.StatusSeeOther {
		t.Fatalf("sign-out = %d", w.Code)
	}
}

// phaseTwoHook is the operator's connection with a hook run as the billing read opens its
// second phase's transaction -- the moment between the two phases.
type phaseTwoHook struct {
	*pgx.Conn
	before func()
}

func (c phaseTwoHook) Begin(ctx context.Context) (pgx.Tx, error) {
	c.before()
	return c.Conn.Begin(ctx)
}

// blockingBilling is liveStore whose billing read runs on phaseTwoHook.
type blockingBilling struct {
	*liveStore
	before func()
}

func (b blockingBilling) TenantBilling(ctx context.Context, h string, id uuid.UUID, page int32) (db.TenantBillingTimeline, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return db.TenantBilling(ctx, phaseTwoHook{Conn: b.conn, before: b.before}, h, id, page)
}

// TestE2E_ASlowBillingReadIsA503AndLeavesItsTicketUnconsumed measures the billing read's time
// bound (db.TenantBillingReadTimeout, SET LOCAL in the read's second phase) through the screen,
// on PostgreSQL. Between the two phases a third connection (the owner) takes a row lock on the
// read's just-committed ticket, so phase two's consuming UPDATE waits on it -- a slow read, made
// on demand and on nothing but this read's own ticket row. The id read is one no tenant has:
// phase two waits before it looks a tenant up, so the bound is measured with no tenant's data
// involved, and the control's answer is the unknown id's 404.
//
//  1. the screen answers 503 with the billing's problem page -- no "€", no "0.00" -- after at
//     least the bound and well before the request's own deadline (httpx.RequestTimeout), and
//     the process log's fault line carries SQLSTATE 57014 (the bound, not the router's
//     deadline);
//  2. the read's 'read' row, committed by phase one, stays: +1 row of scope tenant_billing
//     naming the id; its ticket stays UNCONSUMED -- phase two's transaction, the consumption
//     with it, rolled back -- and is still unconsumed after the lock is released (nothing
//     consumes it later; it expires);
//  3. CONTROL: the same view without the lock is the unknown id's 404 at once, writes its own
//     row and consumes its own ticket; the blocked one is still unconsumed.
//
// It takes the bound's 15 seconds once.
func TestE2E_ASlowBillingReadIsA503AndLeavesItsTicketUnconsumed(t *testing.T) {
	l := newLegalE2E(t)
	tenant := uuid.New()
	sess := l.signIn(l.f)
	path := "/operator/tenants/" + tenant.String() + "/billing"
	lockConn, err := pgx.Connect(l.ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatalf("connect for the lock: %v", err)
	}
	t.Cleanup(func() { _ = lockConn.Close(context.Background()) })
	var lockTx pgx.Tx
	locked := 0
	before := func() {
		var err error
		if lockTx, err = lockConn.Begin(l.ctx); err != nil {
			t.Errorf("begin the lock: %v", err)
			return
		}
		rows, err := lockTx.Query(l.ctx, `SELECT k.id FROM operator_read_tickets k
		                                  WHERE k.session_id IN (SELECT s.id FROM platform_sessions s WHERE s.admin_id = $1)
		                                    AND k.kind = 'tenant_billing' AND k.consumed_at IS NULL FOR UPDATE`, l.f.id)
		if err != nil {
			t.Errorf("lock the ticket: %v", err)
			return
		}
		for rows.Next() {
			locked++
		}
		rows.Close()
	}
	live := &liveStore{conn: l.op}
	s, err := operator.New(l.auth, live, live, live, live, blockingBilling{liveStore: live, before: before}, l.texts, opHost, opBase,
		debugCapture(&l.logs))
	if err != nil {
		t.Fatal(err)
	}
	shipped := l.h
	l.h = httpx.NewRouter(&config.Config{OperatorHost: opHost, BaseURL: opBase}, debugCapture(&l.logs), s)
	ticketsOpen := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
		                   WHERE s.admin_id = $1 AND k.kind = 'tenant_billing' AND k.consumed_at IS NULL`, l.f.id)
	}
	reads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                   AND target_scope = 'tenant_billing' AND target_tenant_id = $2`, l.f.id, tenant)
	}
	r0 := reads()
	l.logs.Reset()
	start := time.Now()
	w := l.get(path, sess)
	took := time.Since(start)
	if lockTx != nil {
		if err := lockTx.Rollback(l.ctx); err != nil {
			t.Errorf("release the lock: %v", err)
		}
	}
	l.h = shipped
	if locked != 1 {
		t.Fatalf("PREMISE: the hook locked %d ticket(s), want the read's one", locked)
	}
	t.Logf("the slow read answered %d after %v (the bound: %v)", w.Code, took.Round(time.Millisecond), db.TenantBillingReadTimeout)
	body := w.Body.String()
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(body, "This tenant&#39;s billing could not be loaded") ||
		strings.Contains(body, "€") || strings.Contains(body, "0.00") {
		t.Errorf("a slow read = %d; want 503 with the billing's problem page and no figure", w.Code)
	}
	if took < db.TenantBillingReadTimeout || took > db.TenantBillingReadTimeout+(httpx.RequestTimeout-db.TenantBillingReadTimeout)/2 {
		t.Errorf("the slow read answered after %v; want at least the bound (%v) and well before the request's deadline (%v)",
			took, db.TenantBillingReadTimeout, httpx.RequestTimeout)
	}
	if !strings.Contains(l.logs.String(), "SQLSTATE 57014") {
		t.Error("the fault line does not carry SQLSTATE 57014: the answer was not the read's own bound")
	}
	if n := reads() - r0; n != 1 {
		t.Errorf("the slow read left %d 'read' row(s), want the 1 phase one committed", n)
	}
	if n := ticketsOpen(); n != 1 {
		t.Errorf("%d unconsumed billing ticket(s) after the slow read and the lock's release, want its 1", n)
	}
	start = time.Now()
	if w := l.get(path, sess); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "There is no tenant with that id") ||
		time.Since(start) >= db.TenantBillingReadTimeout {
		t.Fatalf("CONTROL: the same view without the lock = %d after %v, want the unknown id's 404 at once", w.Code, time.Since(start))
	}
	if n := reads() - r0; n != 2 {
		t.Errorf("the two views left %d 'read' row(s), want 2", n)
	}
	if n := ticketsOpen(); n != 1 {
		t.Errorf("%d unconsumed billing ticket(s) after the control, want still the slow read's 1", n)
	}
}
