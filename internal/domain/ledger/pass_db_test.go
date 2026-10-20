package ledger

// pass_db_test.go -- The Pass's widget reads (M12-13 ...), against real Postgres, as tappa_app in
// the reading tenant's context -- the panel's shape. The holding (K-39) cases live with the other
// employer readers in holding_db_test.go (TestEmployerReaders_NineReadersNameTheSite).

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// passDB is the application pool, or a skip when no database is configured.
func passDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required). Run `make test`.")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	return data
}

// passTenant is one business of the widget fixtures: a Malta tenant with named venues and people,
// written through the application role in its own context.
type passTenant struct {
	t      *testing.T
	data   *db.DB
	id     uuid.UUID
	venues map[string]uuid.UUID
}

func newPassTenant(t *testing.T, data *db.DB, label string, venues ...string) *passTenant {
	t.Helper()
	p := &passTenant{t: t, data: data, id: uuid.New(), venues: map[string]uuid.UUID{}}
	p.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
	        VALUES ($1, $2, $3, 'bar', 'multi', $4)`, p.id, "Pass "+label, "VAT-PASS-"+p.id.String(), maltaTenantZone)
	for _, v := range venues {
		id := uuid.New()
		p.exec(`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`, id, p.id, label+" "+v)
		p.venues[v] = id
	}
	return p
}

func (p *passTenant) exec(sql string, args ...any) {
	p.t.Helper()
	if err := p.data.WithTenant(context.Background(), p.id, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}); err != nil {
		p.t.Fatalf("%s: %v", sql, err)
	}
}

// person adds an employee at the named venue.
func (p *passTenant) person(name, venue string) uuid.UUID {
	p.t.Helper()
	id := uuid.New()
	p.exec(`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, $4, 'active')`,
		id, p.id, p.venues[venue], name)
	return id
}

// passTap is one record of the fixtures. kind "" is a refusal (no direction).
type passTap struct {
	who      uuid.UUID
	venue    string
	at       time.Time
	kind     string
	verdict  string
	channel  string
	practice bool
}

func (p *passTenant) tap(r passTap) {
	p.t.Helper()
	if r.verdict == "" {
		r.verdict = "ok"
	}
	if r.channel == "" {
		r.channel = "nfc"
	}
	var kind, who, venue any
	if r.kind != "" {
		kind = r.kind
	}
	if r.who != uuid.Nil {
		who = r.who
	}
	if r.venue != "" {
		venue = p.venues[r.venue]
	}
	p.exec(`INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, type, verdict, channel, practice, queued)
	        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $6 = 'flag')`,
		p.id, who, venue, r.at, kind, r.verdict, r.channel, r.practice)
}

// readerAt is a Reader standing at a fixed instant.
func readerAt(t *testing.T, data Database, now time.Time, log *slog.Logger) *Reader {
	t.Helper()
	r, err := NewReader(data, log)
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	return r
}

// failOneStatementDB is the production Database whose transactions answer ONE named sqlc statement
// with a genuine statement error (division by zero, executed on the real connection), so the
// transaction really is aborted -- the shape a permission slip or a statement timeout produces.
// Everything else, the real db.WithTenant and its COMMIT included, is production's.
type failOneStatementDB struct {
	real *db.DB
	name string
}

func (d failOneStatementDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, failingTx{Tx: tx, name: d.name})
	})
}

// failingTx intercepts the named statement on the transaction and on every savepoint opened on it.
type failingTx struct {
	pgx.Tx
	name string
}

func (f failingTx) Begin(ctx context.Context) (pgx.Tx, error) {
	sp, err := f.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingTx{Tx: sp, name: f.name}, nil
}

func (f failingTx) swap(sql string, args []any) (string, []any) {
	if strings.HasPrefix(sql, "-- name: "+f.name+" ") {
		return "SELECT 1/0", nil
	}
	return sql, args
}

func (f failingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	sql, args = f.swap(sql, args)
	return f.Tx.Query(ctx, sql, args...)
}

func (f failingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	sql, args = f.swap(sql, args)
	return f.Tx.QueryRow(ctx, sql, args...)
}

// TestFloor_CountsEachPersonOnceAtTheirLatestOpenCheckIn is M12-13's definition, case by case: the
// latest open check-in per person, no older than tap.StaleOpenIn, flagged ones included, practice
// rows and closed entries out, grouped by venue, the big number the sum of the rows.
func TestFloor_CountsEachPersonOnceAtTheirLatestOpenCheckIn(t *testing.T) {
	data := passDB(t)
	// A fixed instant well inside a Malta day, so no fixture straddles a calendar boundary.
	now := hLocal(t, "2026-09-02", "12:30:00")
	b := newPassTenant(t, data, "Floor", "Bar", "Kitchen")
	in := func(who uuid.UUID, venue string, ago time.Duration, verdict string) {
		b.tap(passTap{who: who, venue: venue, at: now.Add(-ago), kind: "in", verdict: verdict})
	}

	open := b.person("Open at the bar", "Bar")
	in(open, "Bar", 2*time.Hour, "ok")
	closed := b.person("Closed", "Bar")
	in(closed, "Bar", 5*time.Hour, "ok")
	b.tap(passTap{who: closed, venue: "Bar", at: now.Add(-time.Hour), kind: "out"})
	moved := b.person("Two entries, latest in the kitchen", "Bar")
	in(moved, "Bar", 3*time.Hour, "ok")
	in(moved, "Kitchen", time.Hour, "ok")
	flagged := b.person("Flagged entry", "Kitchen")
	in(flagged, "Kitchen", 30*time.Minute, "flag")
	practice := b.person("Training tap only", "Bar")
	b.tap(passTap{who: practice, venue: "Bar", at: now.Add(-10 * time.Minute), kind: "in", practice: true})
	stale := b.person("Forgot to tap out yesterday", "Bar")
	in(stale, "Bar", tap.StaleOpenIn+time.Hour, "ok")
	// A refusal with no person and no direction is not anybody on the floor.
	b.tap(passTap{venue: "Bar", at: now.Add(-time.Minute), verdict: "reject"})

	s, err := readerAt(t, data, now, nil).Screen(context.Background(), b.id, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	f := s.Floor
	if f.State != Measured {
		t.Fatalf("the floor is %v, want Measured", f.State)
	}
	if f.People != 3 {
		t.Errorf("the floor counts %d people, want 3 (open at the bar, the latest of two entries, the flagged entry)", f.People)
	}
	if !f.Since.Equal(now.Add(-tap.StaleOpenIn)) {
		t.Errorf("Since is %v, want now - tap.StaleOpenIn = %v", f.Since, now.Add(-tap.StaleOpenIn))
	}
	want := []FloorVenue{
		{LocationID: b.venues["Kitchen"], Name: "Floor Kitchen", People: 2},
		{LocationID: b.venues["Bar"], Name: "Floor Bar", People: 1},
	}
	if len(f.Venues) != len(want) {
		t.Fatalf("venues %+v, want %+v", f.Venues, want)
	}
	sum := 0
	for i := range want {
		if f.Venues[i] != want[i] {
			t.Errorf("venue %d is %+v, want %+v", i, f.Venues[i], want[i])
		}
		sum += f.Venues[i].People
	}
	if sum != f.People {
		t.Errorf("the venues sum to %d and the big number says %d -- they are one read", sum, f.People)
	}
}

// TestRLS_Floor_AnotherTenantsPeopleAreNotOnThisFloor: A's connection does not see B's open
// check-ins. The isolation probe carries NO tenant predicate (CLAUDE.md §6: with one, a 0 would be
// the WHERE's, not RLS's), and the shipped statement asked in A's context FOR B -- so its explicit
// filter admits B's rows -- still answers nothing. Positive control: B's own context sees them.
func TestRLS_Floor_AnotherTenantsPeopleAreNotOnThisFloor(t *testing.T) {
	data := passDB(t)
	now := time.Now()
	a := newPassTenant(t, data, "RLS A", "Door")
	b := newPassTenant(t, data, "RLS B", "Door")
	a.tap(passTap{who: a.person("A on the floor", "Door"), venue: "Door", at: now.Add(-time.Hour), kind: "in"})
	b.tap(passTap{who: b.person("B on the floor", "Door"), venue: "Door", at: now.Add(-time.Hour), kind: "in"})

	// The statement's own shape with every tenant predicate removed.
	const whereless = `
		WITH latest AS (
		    SELECT DISTINCT ON (t.employee_id) t.location_id, t.site_location_id
		    FROM transactions t
		    WHERE t.employee_id IS NOT NULL AND t.type = 'in' AND NOT t.practice AND t.occurred_at >= $1
		      AND NOT EXISTS (SELECT 1 FROM transactions o WHERE o.employee_id = t.employee_id
		                        AND o.type = 'out' AND o.occurred_at > t.occurred_at)
		    ORDER BY t.employee_id, t.occurred_at DESC, t.id DESC)
		SELECT f.location_id FROM latest f LEFT JOIN locations l ON l.id = f.location_id`
	venuesSeenBy := func(tenant uuid.UUID) map[uuid.UUID]bool {
		seen := map[uuid.UUID]bool{}
		if err := data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, whereless, now.Add(-tap.StaleOpenIn))
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[*uuid.UUID])
			for _, id := range ids {
				if id != nil {
					seen[*id] = true
				}
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return seen
	}
	if seen := venuesSeenBy(b.id); !seen[b.venues["Door"]] {
		t.Fatal("positive control: B's own context does not see B's open check-in")
	}
	seen := venuesSeenBy(a.id)
	if !seen[a.venues["Door"]] {
		t.Fatal("positive control: A's context does not see A's own open check-in")
	}
	if seen[b.venues["Door"]] {
		t.Error("RLS: A's tenant-filter-free read sees B's open check-in")
	}

	for _, ctxTenant := range []uuid.UUID{a.id, b.id} {
		var rows []store.CountOnTheFloorByVenueRow
		if err := data.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = store.New(tx).CountOnTheFloorByVenue(ctx, store.CountOnTheFloorByVenueParams{TenantID: b.id, Since: now.Add(-tap.StaleOpenIn)})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		switch {
		case ctxTenant == b.id && (len(rows) != 1 || rows[0].People != 1):
			t.Errorf("positive control: B's context, B's floor: %+v, want one venue with one person", rows)
		case ctxTenant == a.id && len(rows) != 0:
			t.Errorf("RLS: A's context asking for B's floor got %+v, want nothing", rows)
		}
	}
}

// TestPass_AFailedWidgetCostsItsFigureAndNeverThePage drives Screen with ONE widget's statement
// failing for real (the transaction is aborted), through the real COMMIT: the page and every other
// widget stand, the failed one is ReadFailed -- never a zero -- and the error is logged.
func TestPass_AFailedWidgetCostsItsFigureAndNeverThePage(t *testing.T) {
	data := passDB(t)
	now := time.Now()
	b := newPassTenant(t, data, "Broken widget", "Door")
	who := b.person("On the floor", "Door")
	b.tap(passTap{who: who, venue: "Door", at: now.Add(-time.Minute), kind: "in"})

	for _, c := range []struct {
		statement string
		state     func(Screen) Reading
	}{
		{"CountOnTheFloorByVenue", func(s Screen) Reading { return s.Floor.State }},
	} {
		t.Run(c.statement, func(t *testing.T) {
			var logged logCounter
			s, err := readerAt(t, failOneStatementDB{real: data, name: c.statement}, now, slog.New(&logged)).
				Screen(context.Background(), b.id, Filter{})
			if err != nil {
				t.Fatalf("a failed widget took the page: %v", err)
			}
			if !s.Queried || len(s.Records) == 0 {
				t.Fatalf("the page came back queried=%v with %d records", s.Queried, len(s.Records))
			}
			if got := c.state(s); got != ReadFailed {
				t.Errorf("the failed widget is %v, want ReadFailed", got)
			}
			if logged.errors() == 0 {
				t.Error("the failure was not logged")
			}
			// Every OTHER widget was measured: the failure was contained by its own savepoint.
			healthy, err := readerAt(t, data, now, nil).Screen(context.Background(), b.id, Filter{})
			if err != nil {
				t.Fatal(err)
			}
			if got := c.state(healthy); got != Measured {
				t.Errorf("CONTROL: without the injected failure the widget is %v, want Measured", got)
			}
			for _, other := range passStates(s) {
				if other.name != c.statement && other.state != Measured {
					t.Errorf("%s is %v after %s failed -- one widget's failure reached another", other.name, other.state, c.statement)
				}
			}
		})
	}
}

// passStates is every widget's state on a Screen, by the statement that measures it.
func passStates(s Screen) []struct {
	name  string
	state Reading
} {
	return []struct {
		name  string
		state Reading
	}{
		{"CountOnTheFloorByVenue", s.Floor.State},
	}
}

// TestPass_ALedgerDayReadsNoWidget: with a date the screen is the ledger, and no "now" widget is
// asked -- each says NotQueried and claims nothing.
func TestPass_ALedgerDayReadsNoWidget(t *testing.T) {
	data := passDB(t)
	b := newPassTenant(t, data, "Ledger day", "Door")
	s, err := readerAt(t, data, time.Now(), nil).Screen(context.Background(), b.id, Filter{Date: Date{Year: 2026, Month: time.September, Day: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range passStates(s) {
		if w.state != NotQueried && !ledgerDayWidget[w.name] {
			t.Errorf("%s is %v on a ledger day, want NotQueried", w.name, w.state)
		}
	}
}

// ledgerDayWidget names the widgets a ledger day DOES read (the day's own figures).
var ledgerDayWidget = map[string]bool{}
