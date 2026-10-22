package ledger

// checkout_parity_db_test.go -- M15-05 against real Postgres (CLAUDE.md §8): the checkout
// classification's two representations -- tap.ClassifyCheckout fed by the ledger's walker
// (checkout.go) over ListWorkedShiftEvents' rows, and tappa_checkout_class (migration 00064) fed
// by P, I and W found with subqueries -- give one class to every checkout; the open list
// (ListOpenCheckIns) is the reports section's "Open" set; and the 18 hours of the SQL function are
// tap.StaleOpenIn's.
//
// EVERY TEST BUILDS ITS OWN TENANT (cpNewWorld) and writes rows with the instants, business days
// and write instants the case is about, as the owner in the tenant's context (the feed trigger's
// rule). It does not touch the seed tenant. The rows live two weeks back so that a manager's
// record typed through manual.Recorder is inside its 90-day bound on any day the suite runs.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	manualentry "github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

type cpWorld struct {
	t      *testing.T
	ctx    context.Context
	data   *db.DB
	owner  *pgx.Conn
	r      *Reader
	tenant uuid.UUID
	venue  uuid.UUID
	admin  uuid.UUID
	zone   *time.Location
	// week is the reporting week the rows are placed in: the one that held the day 14 days ago.
	week Period
}

func cpNewWorld(t *testing.T) *cpWorld {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; rows with chosen write instants are written by the owner)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	data, err := db.New(ctx, &config.Config{DatabaseURL: appDSN})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	r, err := NewReader(data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	w := &cpWorld{t: t, ctx: ctx, data: data, owner: owner, r: r, tenant: uuid.New(), zone: malta(t)}
	if _, err := owner.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
	                              VALUES ($1, 'm15-05 checkout', $2, 'restaurant', 'multi', 'Europe/Malta')`,
		w.tenant, "VAT-M1505-"+w.tenant.String()); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'm15-05 venue') RETURNING id`, w.tenant).Scan(&w.venue); err != nil {
		t.Fatal(err)
	}
	w.admin = uuid.New()
	if _, err := owner.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		w.admin, w.tenant, "owner-"+w.admin.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53)); err != nil {
		t.Fatal(err)
	}
	ago := time.Now().In(w.zone).AddDate(0, 0, -14)
	w.week = WeekOf(Date{Year: ago.Year(), Month: ago.Month(), Day: ago.Day()}, w.zone)
	return w
}

// day is a wall clock on the week's day d (0 = Monday; -1 = the Sunday before), in Malta.
func (w *cpWorld) day(d, hh, mm int) time.Time {
	f := w.week.FirstDay
	return time.Date(f.Year, f.Month, f.Day+d, hh, mm, 0, 0, w.zone).UTC()
}

// s is the 05:00 business day of an instant, as the engine freezes it.
func (w *cpWorld) s(at time.Time) *time.Time { return maltaDay(w.t, at) }

func (w *cpWorld) employee(name string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	if _, err := w.owner.Exec(w.ctx, `INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at, location_id)
	                                  VALUES ($1, $2, $3, $4, 'active', now(), $5)`,
		id, w.tenant, "ext-"+id.String()[:8], name+" "+id.String()[:6], w.venue); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// cpRow is one record of a fixture chain. A zero id is a fresh one; a nil wrote is "written at
// its own instant"; typed is a manager's row (channel manual, entered_by the owner); practice is a
// historical training row (practice = true, an `in` -- ADR 0008; nothing writes one any more).
// serverWrite leaves created_at to its DEFAULT now() -- the write instant the database gives a
// real row, later than every row written before it (M15-06's trap: a row that comes later is
// never written with a created_at in the past); wrote is then ignored.
type cpRow struct {
	id          uuid.UUID
	dir         string
	at          time.Time
	day         *time.Time
	wrote       *time.Time
	typed       bool
	practice    bool
	serverWrite bool
}

// chain writes a person's rows, each in a transaction of its own as the owner in the tenant's
// context, with the write instant the row names.
func (w *cpWorld) chain(person uuid.UUID, rows []cpRow) {
	w.t.Helper()
	for _, r := range rows {
		id := r.id
		if id == uuid.Nil {
			id = uuid.New()
		}
		wrote := r.at
		if r.wrote != nil {
			wrote = *r.wrote
		}
		channel, enteredBy := "nfc", any(nil)
		if r.typed {
			channel, enteredBy = "manual", w.admin
		}
		tx, err := w.owner.Begin(w.ctx)
		if err != nil {
			w.t.Fatal(err)
		}
		if _, err := tx.Exec(w.ctx, `SELECT set_config('app.tenant_id', $1, true)`, w.tenant.String()); err != nil {
			w.t.Fatal(err)
		}
		if r.serverWrite {
			_, err = tx.Exec(w.ctx, `INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at,
			                                                   verdict, channel, type, business_day_start, entered_by, practice)
			                         VALUES ($1, $2, $3, $4, $5, 'ok', $6, $7, $8, $9, $10)`,
				id, w.tenant, person, w.venue, r.at, channel, r.dir, r.day, enteredBy, r.practice)
		} else {
			_, err = tx.Exec(w.ctx, `INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, created_at,
			                                                   verdict, channel, type, business_day_start, entered_by, practice)
			                         VALUES ($1, $2, $3, $4, $5, $6, 'ok', $7, $8, $9, $10, $11)`,
				id, w.tenant, person, w.venue, r.at, wrote, channel, r.dir, r.day, enteredBy, r.practice)
		}
		if err != nil {
			_ = tx.Rollback(w.ctx)
			w.t.Fatalf("write %+v: %v", r, err)
		}
		if err := tx.Commit(w.ctx); err != nil {
			w.t.Fatal(err)
		}
	}
}

// classOf is one checkout's class as one side computed it, in (occurred_at, id) order.
type classOf struct {
	At    time.Time
	Class string
}

// sqlClasses classifies each of a person's checkouts with tappa_checkout_class, finding P, I and W
// the way ADR 0032 §A.6 defines them: P the directed, non-practice record just before it in
// (occurred_at, id) order; I an `in` before it in that order with occurred_at >= its business day;
// W P's created_at later than its own. Called as tappa_app, the role 00064 grants EXECUTE to.
func (w *cpWorld) sqlClasses(person uuid.UUID) []classOf {
	w.t.Helper()
	var out []classOf
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT o.occurred_at,
			       coalesce(public.tappa_checkout_class(
			           CASE WHEN p.type = 'in' THEN p.occurred_at END,
			           p.type = 'out',
			           EXISTS (SELECT 1 FROM transactions i
			                    WHERE i.tenant_id = o.tenant_id AND i.employee_id = o.employee_id
			                      AND i.type = 'in' AND NOT i.practice
			                      AND (i.occurred_at, i.id) < (o.occurred_at, o.id)
			                      AND i.occurred_at >= o.business_day_start),
			           p.created_at > o.created_at,
			           o.occurred_at, o.business_day_start), '')
			  FROM transactions o
			  LEFT JOIN LATERAL (
			      SELECT x.type, x.occurred_at, x.created_at FROM transactions x
			       WHERE x.tenant_id = o.tenant_id AND x.employee_id = o.employee_id
			         AND x.type IS NOT NULL AND NOT x.practice
			         AND (x.occurred_at, x.id) < (o.occurred_at, o.id)
			       ORDER BY x.occurred_at DESC, x.id DESC LIMIT 1) p ON true
			 WHERE o.tenant_id = $1 AND o.employee_id = $2 AND o.type = 'out' AND NOT o.practice
			 ORDER BY o.occurred_at, o.id`, w.tenant, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c classOf
			if err := rows.Scan(&c.At, &c.Class); err != nil {
				return err
			}
			c.At = c.At.UTC()
			out = append(out, c)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatalf("classify in SQL: %v", err)
	}
	return out
}

// goClasses classifies each of a person's checkouts the way the reports section does: the rows of
// ListWorkedShiftEvents (the production read, here over the whole history), mapped by events(),
// walked by checkoutChain.
func (w *cpWorld) goClasses(person uuid.UUID) []classOf {
	w.t.Helper()
	var rows []store.ListWorkedShiftEventsRow
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		rows, err = store.New(tx).ListWorkedShiftEvents(ctx, store.ListWorkedShiftEventsParams{
			TenantID: w.tenant, FromAt: w.week.From.AddDate(0, 0, -30), UntilAt: w.week.To.AddDate(0, 0, 30), RowLimit: 10000,
		})
		return err
	}); err != nil {
		w.t.Fatalf("list worked shift events: %v", err)
	}
	evs, _ := events(rows, "Europe/Malta", nil)
	var c checkoutChain
	var out []classOf
	for _, e := range evs {
		if e.EmployeeID != person {
			continue
		}
		if e.Direction == "in" {
			c.checkin(e.At, e.CreatedAt)
			continue
		}
		out = append(out, classOf{At: e.At.UTC(), Class: string(c.checkout(e.At, e.CreatedAt, e.DayStart))})
	}
	return out
}

// sortedIDs is n fresh ids in ascending order -- the (occurred_at, id) tie-breaker's order.
func sortedIDs(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids
}

func tp(v time.Time) *time.Time { return &v }

// cpCase is one fixture chain and the classes its checkouts must get, in order.
type cpCase struct {
	name string
	rows []cpRow
	want []string
}

// cpCases is every cell and bound the card names (M15-05 step 5): the six classes, in.At == S,
// exactly 18 h and 18 h + 1 µs, a AND b, two rows at one instant (both id orders), I's ordering
// at one instant, W's equality, the break and the correction, and the Phase C flow.
func (w *cpWorld) cpCases() []cpCase {
	mon := func(hh, mm int) time.Time { return w.day(0, hh, mm) }
	tue := func(hh, mm int) time.Time { return w.day(1, hh, mm) }
	sMon := w.s(mon(9, 0))
	us := time.Microsecond
	tapped := func(dir string, at time.Time) cpRow { return cpRow{dir: dir, at: at, day: w.s(at)} }
	two, twoB := sortedIDs(2), sortedIDs(2)
	nextA, nextB := sortedIDs(2), sortedIDs(2)
	three, threeB := sortedIDs(3), sortedIDs(3)
	late := tp(tue(21, 0))
	return []cpCase{
		{"closes: one business day", []cpRow{tapped("in", mon(9, 0)), tapped("out", mon(17, 0))}, []string{"closes"}},
		{"closes: the in AT the business day start", []cpRow{{dir: "in", at: *sMon, day: sMon}, tapped("out", mon(10, 0))}, []string{"closes"}},
		{"closes: exactly 18 h inside one business day", []cpRow{{dir: "in", at: *sMon, day: sMon}, {dir: "out", at: sMon.Add(18 * time.Hour), day: sMon}}, []string{"closes"}},
		{"(a) too_long: 18 h + 1 µs", []cpRow{{dir: "in", at: *sMon, day: sMon}, {dir: "out", at: sMon.Add(18*time.Hour + us), day: sMon}}, []string{"too_long"}},
		{"(a) too_long: the historical stale shape, S NULL, 24 h", []cpRow{{dir: "in", at: mon(9, 0)}, {dir: "out", at: tue(9, 5)}}, []string{"too_long"}},
		{"closes: S NULL, 8 h (a row before M15)", []cpRow{{dir: "in", at: mon(9, 0)}, {dir: "out", at: mon(17, 0)}}, []string{"closes"}},
		{"(b) other_day: the in 1 µs before the business day", []cpRow{{dir: "in", at: sMon.Add(-us)}, {dir: "out", at: mon(6, 0), day: sMon}}, []string{"other_day"}},
		{"(b) other_day: a AND b -> b (the Sunday 10:00 in, 20 h)", []cpRow{tapped("in", w.day(-1, 10, 0)), tapped("out", mon(6, 0))}, []string{"other_day"}},
		{"(b) then (c): the open slot empties", []cpRow{tapped("in", mon(15, 0)), tapped("out", tue(7, 0)), tapped("out", tue(8, 0))}, []string{"other_day", "orphan"}},
		{"(c) orphan: a checkout alone, S set", []cpRow{tapped("out", mon(10, 0))}, []string{"orphan"}},
		{"(d) unanchored: a typed checkout alone", []cpRow{{dir: "out", at: mon(10, 0), typed: true}}, []string{"unanchored"}},
		{"(e) the correction: 09 in, 20 out, typed 17 out written later", []cpRow{tapped("in", mon(9, 0)), tapped("out", mon(20, 0)),
			{dir: "out", at: mon(17, 0), typed: true, wrote: tp(mon(21, 0))}}, []string{"closes", "extra"}},
		{"(c) the break: 09 in, 12 out, 17 out written in order", []cpRow{tapped("in", mon(9, 0)), tapped("out", mon(12, 0)), tapped("out", mon(17, 0))}, []string{"closes", "orphan"}},
		{"(c) W equality: the correction written in the same instant as the checkout it precedes", []cpRow{tapped("in", mon(9, 0)),
			{dir: "out", at: mon(20, 0), day: w.s(mon(20, 0)), wrote: tp(mon(21, 0))},
			{dir: "out", at: mon(17, 0), typed: true, wrote: tp(mon(21, 0))}}, []string{"closes", "orphan"}},
		{"(c) Phase C's own flow: Monday's checkout approved later, Tuesday has no in", []cpRow{tapped("in", mon(15, 0)), tapped("out", tue(7, 0)),
			{dir: "out", at: mon(23, 0), typed: true, wrote: tp(tue(9, 0))}}, []string{"closes", "orphan"}},
		{"one instant: the out's id is the larger -- it closes", []cpRow{{id: two[0], dir: "in", at: mon(9, 0), day: sMon}, {id: two[1], dir: "out", at: mon(9, 0), day: sMon}}, []string{"closes"}},
		{"one instant: the in's id is the larger -- the out has nothing before it", []cpRow{{id: twoB[1], dir: "in", at: mon(9, 0), day: sMon}, {id: twoB[0], dir: "out", at: mon(9, 0), day: sMon}}, []string{"orphan"}},
		{"one instant, I by order: the in first (smallest id) -- the surplus (e)", []cpRow{
			{id: three[0], dir: "in", at: mon(10, 0), day: sMon},
			{id: three[1], dir: "out", at: mon(10, 0), typed: true, wrote: late},
			{id: three[2], dir: "out", at: mon(10, 0), day: sMon}}, []string{"closes", "extra"}},
		{"one instant, I by order: the in last (largest id) -- no in before the checkout, (c)", []cpRow{
			{id: threeB[2], dir: "in", at: mon(10, 0), day: sMon},
			{id: threeB[0], dir: "out", at: mon(10, 0), typed: true, wrote: late},
			{id: threeB[1], dir: "out", at: mon(10, 0), day: sMon}}, []string{"unanchored", "orphan"}},
		{"Monday in, Tuesday in, Tuesday out", []cpRow{tapped("in", mon(9, 0)), tapped("in", tue(9, 0)), tapped("out", tue(17, 0))}, []string{"closes"}},
		{"two ins and one out of one day, S NULL: the first in stays open", []cpRow{{dir: "in", at: mon(9, 0)}, {dir: "in", at: mon(10, 0)}, {dir: "out", at: mon(17, 0)}}, []string{"closes"}},
		{"closes: the Rusty Bar night", []cpRow{tapped("in", mon(18, 5)), tapped("out", tue(2, 10))}, []string{"closes"}},
		// A historical training row between an in and its out is skipped by every reader (ADR 0008):
		// the out closes the in, and the in is not listed open.
		{"closes: a training row between the in and its out", []cpRow{tapped("in", mon(9, 0)),
			{dir: "in", at: mon(10, 0), practice: true}, tapped("out", mon(17, 0))}, []string{"closes"}},
		// Two NEXT records at one instant: the id decides which is first. Written in the same order
		// (the in, then the out) in both, so a reader that orders by occurred_at alone meets them in
		// one physical order and gets one of the two wrong.
		{"one instant, two next records: the out's id is the smaller -- it closes the 09:00 in", []cpRow{tapped("in", mon(9, 0)),
			{id: nextA[1], dir: "in", at: mon(17, 0), day: sMon}, {id: nextA[0], dir: "out", at: mon(17, 0), day: sMon}}, []string{"closes"}},
		{"one instant, two next records: the in's id is the smaller -- the 09:00 in stays open", []cpRow{tapped("in", mon(9, 0)),
			{id: nextB[0], dir: "in", at: mon(17, 0), day: sMon}, {id: nextB[1], dir: "out", at: mon(17, 0), day: sMon}}, []string{"closes"}},
	}
}

// TestCheckoutParity_GoAndSQLClassifyEveryCellAlike is the card's parity test (M15-05 step 5; ADR
// 0032 §A.6 "İki temsil, bir parite testi"): every case's checkouts get the same class, in the
// same order, from both representations, and it is the class the case names. The SQL function's
// 18 hours are read out of the catalogue (pg_get_functiondef) and must be tap.StaleOpenIn -- the
// schema-to-code binding TestGuardrails_TagNotActiveIsAnAllowList makes for the tag statuses.
func TestCheckoutParity_GoAndSQLClassifyEveryCellAlike(t *testing.T) {
	w := cpNewWorld(t)
	seen := map[string]bool{}
	for _, tc := range w.cpCases() {
		t.Run(tc.name, func(t *testing.T) {
			person := w.employee("Parity")
			w.chain(person, tc.rows)
			sqlSide, goSide := w.sqlClasses(person), w.goClasses(person)
			var sqlNames, goNames []string
			for i := range sqlSide {
				sqlNames = append(sqlNames, sqlSide[i].Class)
				seen[sqlSide[i].Class] = true
			}
			for i := range goSide {
				goNames = append(goNames, goSide[i].Class)
			}
			if strings.Join(sqlNames, ",") != strings.Join(tc.want, ",") {
				t.Errorf("SQL classes %v, want %v", sqlNames, tc.want)
			}
			if strings.Join(goNames, ",") != strings.Join(tc.want, ",") {
				t.Errorf("Go classes %v, want %v", goNames, tc.want)
			}
			if len(sqlSide) != len(goSide) {
				t.Fatalf("SQL classified %d checkouts, Go %d", len(sqlSide), len(goSide))
			}
			for i := range sqlSide {
				if !sqlSide[i].At.Equal(goSide[i].At) || sqlSide[i].Class != goSide[i].Class {
					t.Errorf("checkout %d: SQL %v %q, Go %v %q -- two representations, two answers",
						i, sqlSide[i].At, sqlSide[i].Class, goSide[i].At, goSide[i].Class)
				}
			}
		})
	}
	for _, c := range []tap.CheckoutClass{tap.CheckoutCloses, tap.CheckoutTooLong, tap.CheckoutOtherDay, tap.CheckoutOrphan, tap.CheckoutExtra, tap.CheckoutUnanchored} {
		if !seen[string(c)] {
			t.Errorf("PREMISE: no case reached %q -- the parity is not over all six classes", c)
		}
	}

	// The SQL function's duration is tap.StaleOpenIn's.
	var def string
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT pg_get_functiondef('public.tappa_checkout_class(timestamptz, boolean, boolean, boolean, timestamptz, timestamptz)'::regprocedure)`).Scan(&def)
	}); err != nil {
		t.Fatalf("read the function's definition: %v", err)
	}
	lits := regexp.MustCompile(`interval '([^']*)'`).FindAllStringSubmatch(def, -1)
	if len(lits) != 1 {
		t.Fatalf("tappa_checkout_class carries %d interval literals, want exactly one (the stale bound): %s", len(lits), def)
	}
	var seconds int64
	if err := w.owner.QueryRow(w.ctx, `SELECT extract(epoch FROM $1::interval)::bigint`, lits[0][1]).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	if time.Duration(seconds)*time.Second != tap.StaleOpenIn {
		t.Errorf("tappa_checkout_class's bound is %q (%ds), tap.StaleOpenIn is %v -- the two representations disagree",
			lits[0][1], seconds, tap.StaleOpenIn)
	}
}

// openListed is ListOpenCheckIns over a window, as (name, instant) keys.
func (w *cpWorld) openListed(from, to time.Time) map[string]bool {
	w.t.Helper()
	out := map[string]bool{}
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListOpenCheckIns(ctx, store.ListOpenCheckInsParams{TenantID: w.tenant, FromAt: from, ToAt: to, RowLimit: 1000})
		for _, r := range rows {
			out[deref(r.EmployeeName)+" @ "+r.OccurredAt.UTC().Format(time.RFC3339Nano)] = true
		}
		return err
	}); err != nil {
		w.t.Fatalf("list open check-ins: %v", err)
	}
	return out
}

// TestOpenCheckIns_TheListIsTheReportsOpenSet is the card's "list = report" criterion (M15-05;
// ADR 0032 §A.6, the end of anomalies.sql's "legitimate difference"): for one tenant and one local
// week, the reports section's Open entries and ListOpenCheckIns are the same set -- over every
// parity chain, laid out in that week, and three cases the card names: Monday in, Tuesday in,
// Tuesday out keeps Monday listed; Monday 15:00 in, Tuesday 07:00 out (S Tuesday 05:00) is (b) --
// Monday listed and an OpenEntry, no hours; the open slot shape is listed once, not twice. Neither
// read is capped here (ReportEventCap and the list's row limit are outside the measurement, as the
// card says).
func TestOpenCheckIns_TheListIsTheReportsOpenSet(t *testing.T) {
	w := cpNewWorld(t)
	names := map[string]string{}
	for _, tc := range w.cpCases() {
		person := w.employee("Week")
		w.chain(person, tc.rows)
		names[tc.name] = person.String()[:6]
	}
	rep, err := w.r.Hours(w.ctx, w.tenant, ReportFilter{Day: w.week.FirstDay})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Truncated {
		t.Fatal("PREMISE: the read was truncated")
	}
	fromReport := map[string]bool{}
	for _, o := range rep.Open {
		fromReport[o.EmployeeName+" @ "+o.At.UTC().Format(time.RFC3339Nano)] = true
	}
	fromList := w.openListed(w.week.From, w.week.To)
	if len(fromList) == 0 {
		t.Fatal("PREMISE: nothing is open in the week")
	}
	for k := range fromList {
		if !fromReport[k] {
			t.Errorf("listed open but not an OpenEntry of the report: %s", k)
		}
	}
	for k := range fromReport {
		if !fromList[k] {
			t.Errorf("an OpenEntry of the report the list does not show: %s", k)
		}
	}
	// The named cases, by the person's name.
	for _, c := range []struct {
		name string
		open int
	}{
		{"Monday in, Tuesday in, Tuesday out", 1},
		{"two ins and one out of one day, S NULL: the first in stays open", 1},
		{"closes: a training row between the in and its out", 0},
		{"one instant, two next records: the out's id is the smaller -- it closes the 09:00 in", 1},
		{"one instant, two next records: the in's id is the smaller -- the 09:00 in stays open", 1},
		{"(b) then (c): the open slot empties", 1},
		{"(b) other_day: a AND b -> b (the Sunday 10:00 in, 20 h)", 0}, // the in is the previous week's
		{"(e) the correction: 09 in, 20 out, typed 17 out written later", 0},
		{"(a) too_long: the historical stale shape, S NULL, 24 h", 1},
	} {
		n := 0
		for k := range fromList {
			if strings.Contains(k, " "+names[c.name]+" @ ") {
				n++
			}
		}
		if n != c.open {
			t.Errorf("%s: %d listed, want %d", c.name, n, c.open)
		}
	}

	// The third eye's case alone, through the report: Monday listed, an OpenEntry, no hours.
	solo := cpNewWorld(t)
	person := solo.employee("Solo")
	in := solo.day(0, 15, 0)
	out := solo.day(1, 7, 0)
	solo.chain(person, []cpRow{{dir: "in", at: in, day: solo.s(in)}, {dir: "out", at: out, day: solo.s(out)}})
	r, err := solo.r.Hours(solo.ctx, solo.tenant, ReportFilter{Day: solo.week.FirstDay})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Open) != 1 || !r.Open[0].At.Equal(in) || r.Totals.Worked != 0 || r.Totals.Awaiting != 0 {
		t.Errorf("(b) through Hours: Open %+v, Worked %v, Awaiting %v -- want Monday 15:00 open and no hours", r.Open, r.Totals.Worked, r.Totals.Awaiting)
	}
	if l := solo.openListed(solo.week.From, solo.week.To); len(l) != 1 {
		t.Errorf("(b): the list holds %v, want Monday 15:00 alone", l)
	}
}

// TestOpenCheckIns_ACutoffChangeKeepsTheEntryListedAndInventsNoCheckout is the card's step 6
// bound (ADR 0032 §A.6: "muhafazakâr sınır"): GetLastOpenTransaction does not use the
// classification, and while the cutoff is fixed it need not (TestLastOpen_TheCheckInItHidesIsOneTheEngineWouldNotClose).
// When the cutoff CHANGES between two taps it can hide an entry the engine would now close:
// Tuesday 10:00 in under a 14:00 cutoff (its day began Monday 14:00), Tuesday 15:00 out of the
// next business day (S Tuesday 14:00 -> (b)); the cutoff moves to 05:00 and the person taps at
// 16:00. Shown the 10:00 entry, the engine would close it (same 05:00 day, 6 h); the query hides
// it -- the later out exists -- so the engine says `in`. The bound is the conservative one: no
// checkout is invented, no hours are made, and the 10:00 entry stays on the open list.
func TestOpenCheckIns_ACutoffChangeKeepsTheEntryListedAndInventsNoCheckout(t *testing.T) {
	w := cpNewWorld(t)
	person := w.employee("Cutoff")
	afternoon := tap.DayCutoff{Cutoff: 14 * time.Hour, Timezone: "Europe/Malta"}
	morning := tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Europe/Malta"}
	x, y, z := w.day(1, 10, 0), w.day(1, 15, 0), w.day(1, 16, 0)
	sx, _ := tap.BusinessDayStart(x, afternoon)
	sy, _ := tap.BusinessDayStart(y, afternoon)
	w.chain(person, []cpRow{{dir: "in", at: x, day: &sx}, {dir: "out", at: y, day: &sy}})
	if got := w.sqlClasses(person); len(got) != 1 || got[0].Class != "other_day" {
		t.Fatalf("PREMISE: the 15:00 checkout is %v, want other_day under the 14:00 cutoff", got)
	}

	var open *store.Transaction
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetLastOpenTransaction(ctx, store.GetLastOpenTransactionParams{TenantID: w.tenant, EmployeeID: &person, At: z})
		if err == nil {
			open = &row
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if open != nil {
		t.Fatalf("GetLastOpenTransaction at 16:00 returned %v; the later 15:00 checkout hides the 10:00 entry", open.ID)
	}
	if dec := tap.Decide(engineInput(z, nil, &morning)); dec.Type == nil || *dec.Type != tap.TypeIn {
		t.Errorf("the 16:00 tap with nothing open is %v, want in", dec.Type)
	}
	shown := tap.Decide(engineInput(z, &tap.Transaction{ID: uuid.New(), OccurredAt: x, Direction: tap.TypeIn}, &morning))
	if shown.Type == nil || *shown.Type != tap.TypeOut {
		t.Errorf("PREMISE: shown the 10:00 entry the engine says %v -- the case does not reach the bound it pins", shown.Type)
	}
	if l := w.openListed(w.week.From, w.week.To); len(l) != 1 {
		t.Errorf("the 10:00 entry is not the one open entry: %v", l)
	}
}

// TestCheckoutParity_ACorrectionTypedThroughTheRecorderIsTheSurplus is the card's correction
// criterion with the write the product makes (M15-05's trap: a correction is written through
// manual.Recorder, so its created_at is the database's now() -- later than the engine rows, as a
// real correction is): engine 09:00 in, engine 20:00 out (both business days frozen), the manager
// types 17:00 out. The report pays 8 h and counts 20:00 StartedEarlier (ADR 0011's result); 20:00
// is (e) in SQL and in Go; nothing is listed open.
func TestCheckoutParity_ACorrectionTypedThroughTheRecorderIsTheSurplus(t *testing.T) {
	w := cpNewWorld(t)
	person := w.employee("Corrected")
	in, out := w.day(0, 9, 0), w.day(0, 20, 0)
	w.chain(person, []cpRow{{dir: "in", at: in, day: w.s(in)}, {dir: "out", at: out, day: w.s(out)}})
	trail, err := audit.New(w.data)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := manualentry.NewRecorder(w.data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Record(w.ctx, manualentry.Entry{TenantID: w.tenant, EmployeeID: person, EnteredBy: w.admin,
		Direction: manualentry.Out, At: w.day(0, 17, 0)}); err != nil {
		t.Fatalf("type the correction: %v", err)
	}
	want := []string{"closes", "extra"}
	for side, got := range map[string][]classOf{"SQL": w.sqlClasses(person), "Go": w.goClasses(person)} {
		var names []string
		for _, c := range got {
			names = append(names, c.Class)
		}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Errorf("%s classes %v, want %v", side, names, want)
		}
	}
	rep, err := w.r.Hours(w.ctx, w.tenant, ReportFilter{Day: w.week.FirstDay})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Totals.Worked != 8*time.Hour || rep.StartedEarlier != 1 || rep.Totals.Open != 0 || rep.Totals.TooLong != 0 {
		t.Errorf("Worked %v StartedEarlier %d Open %d TooLong %d, want 8h 1 0 0", rep.Totals.Worked, rep.StartedEarlier, rep.Totals.Open, rep.Totals.TooLong)
	}
	if l := w.openListed(w.week.From, w.week.To); len(l) != 0 {
		t.Errorf("listed open: %v, want nothing", l)
	}
}

// TestCountMaskedOpenCheckIns_SplitsByTheOneClassification measures both of the tally's numbers
// (M15-05 review, ORTA-2): two check-ins of the week with a training row after them -- one that
// nothing closes, one closed by its own checkout (the training row sits between that pair) -- and a
// control with no training row, which the tally does not count. {StillOpen 1, ClosedByItsCheckout 1}.
func TestCountMaskedOpenCheckIns_SplitsByTheOneClassification(t *testing.T) {
	w := cpNewWorld(t)
	mon := func(hh, mm int) time.Time { return w.day(0, hh, mm) }
	tapped := func(dir string, at time.Time) cpRow { return cpRow{dir: dir, at: at, day: w.s(at)} }
	w.chain(w.employee("Masked open"), []cpRow{tapped("in", mon(9, 0)), {dir: "in", at: mon(10, 0), practice: true}})
	// The training row sits BETWEEN the in and its out: the statement's own next-record lookup must
	// skip it, or the in reads open (the final turn's finding: without `NOT n.practice` in THIS
	// statement's LATERAL the tally answered {2 0}).
	w.chain(w.employee("Masked closed"), []cpRow{tapped("in", mon(9, 0)), {dir: "in", at: mon(12, 0), practice: true}, tapped("out", mon(17, 0))})
	w.chain(w.employee("Unmasked"), []cpRow{tapped("in", mon(9, 0))})
	var got store.CountMaskedOpenCheckInsRow
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		got, err = store.New(tx).CountMaskedOpenCheckIns(ctx, store.CountMaskedOpenCheckInsParams{TenantID: w.tenant, FromAt: w.week.From, ToAt: w.week.To})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got.StillOpen != 1 || got.ClosedByItsCheckout != 1 {
		t.Errorf("CountMaskedOpenCheckIns = %+v, want {StillOpen:1 ClosedByItsCheckout:1}", got)
	}
}
