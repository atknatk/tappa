package ledger

// pass_db_test.go -- The Pass's widget reads (M12-13 ...), against real Postgres, as tappa_app in
// the reading tenant's context -- the panel's shape. The holding (K-39) cases live with the other
// employer readers in holding_db_test.go (TestEmployerReaders_NineReadersNameTheSite).

import (
	"context"
	"log/slog"
	"os"
	"slices"
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
		{"ListWorkedShiftEvents", func(s Screen) Reading { return s.Tape.State }},
		{"ListOpenCheckIns", func(s Screen) Reading { return s.LeftOpen.State }},
		{"CountPanelTransactionsByVerdict", func(s Screen) Reading { return s.Tally.State }},
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
		{"ListWorkedShiftEvents", s.Tape.State},
		{"ListOpenCheckIns", s.LeftOpen.State},
		{"CountPanelTransactionsByVerdict", s.Tally.State},
	}
}

// TestPass_ALedgerDayReadsNoNowWidget: with a date the screen is the ledger, and no "now" widget
// is asked -- each says NotQueried and claims nothing -- while the day's own figures (the verdict
// tally) are measured.
func TestPass_ALedgerDayReadsNoNowWidget(t *testing.T) {
	data := passDB(t)
	b := newPassTenant(t, data, "Ledger day", "Door")
	s, err := readerAt(t, data, time.Now(), nil).Screen(context.Background(), b.id, Filter{Date: Date{Year: 2026, Month: time.September, Day: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range passStates(s) {
		switch {
		case ledgerDayWidget[w.name] && w.state != Measured:
			t.Errorf("%s is %v on a ledger day, want Measured -- it is a figure about the day", w.name, w.state)
		case !ledgerDayWidget[w.name] && w.state != NotQueried:
			t.Errorf("%s is %v on a ledger day, want NotQueried", w.name, w.state)
		}
	}
}

// ledgerDayWidget names the figures a ledger day DOES read (the day's own).
var ledgerDayWidget = map[string]bool{"CountPanelTransactionsByVerdict": true}

// TestTape_IsReadFromTodaysRecordsAsOfNow drives the tape through Screen against real rows: an
// overnight entry from yesterday shows from 00:00, today's closed shift shows in its hours, an
// entry left open yesterday morning (older than tap.StaleOpenIn) and a training tap show nowhere,
// and the hours stop at the one holding now.
func TestTape_IsReadFromTodaysRecordsAsOfNow(t *testing.T) {
	data := passDB(t)
	now := hLocal(t, "2026-09-02", "10:30:00")
	b := newPassTenant(t, data, "Tape", "Bar", "Kitchen")
	night := b.person("Night shift", "Bar")
	b.tap(passTap{who: night, venue: "Bar", at: hLocal(t, "2026-09-01", "22:00:00"), kind: "in"})
	b.tap(passTap{who: night, venue: "Bar", at: hLocal(t, "2026-09-02", "02:10:00"), kind: "out"})
	day := b.person("Day shift", "Kitchen")
	b.tap(passTap{who: day, venue: "Kitchen", at: hLocal(t, "2026-09-02", "08:05:00"), kind: "in", verdict: "flag"})
	forgot := b.person("Forgot yesterday", "Bar")
	b.tap(passTap{who: forgot, venue: "Bar", at: hLocal(t, "2026-09-01", "08:00:00"), kind: "in"})
	trainee := b.person("Trainee", "Bar")
	b.tap(passTap{who: trainee, venue: "Bar", at: hLocal(t, "2026-09-02", "09:00:00"), kind: "in", practice: true})

	s, err := readerAt(t, data, now, nil).Screen(context.Background(), b.id, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	tp := s.Tape
	if tp.State != Measured || tp.Incomplete {
		t.Fatalf("the tape is %v (incomplete %v), want Measured and complete", tp.State, tp.Incomplete)
	}
	if len(tp.Hours) != 11 || !tp.Hours[0].Equal(hLocal(t, "2026-09-02", "00:00:00")) {
		t.Fatalf("the tape has %d hours from %v, want 11 from local midnight", len(tp.Hours), tp.Hours)
	}
	want := map[string][]int{
		"Tape Bar":     {1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		"Tape Kitchen": {0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1},
	}
	if len(tp.Venues) != len(want) {
		t.Fatalf("venues %+v, want Bar and Kitchen", tp.Venues)
	}
	for _, v := range tp.Venues {
		w, ok := want[v.Name]
		if !ok || v.LocationID != b.venues[strings.TrimPrefix(v.Name, "Tape ")] || v.Partner || v.Unplaced {
			t.Errorf("unexpected row %+v", v)
			continue
		}
		for i := range w {
			if v.People[i] != w[i] {
				t.Errorf("%s: %v, want %v", v.Name, v.People, w)
				break
			}
		}
	}
}

// TestRLS_Tape_AnotherTenantsShiftsAreNotOnThisTape: the tape's statement, asked in A's context
// for B (its explicit filter admits B's rows), answers nothing; a probe with no tenant predicate in
// A's context sees A's records and not B's. Positive control: B's context sees B's.
func TestRLS_Tape_AnotherTenantsShiftsAreNotOnThisTape(t *testing.T) {
	data := passDB(t)
	now := time.Now()
	a := newPassTenant(t, data, "Tape RLS A", "Door")
	b := newPassTenant(t, data, "Tape RLS B", "Door")
	aWho, bWho := a.person("A on shift", "Door"), b.person("B on shift", "Door")
	a.tap(passTap{who: aWho, venue: "Door", at: now.Add(-time.Hour), kind: "in"})
	b.tap(passTap{who: bWho, venue: "Door", at: now.Add(-time.Hour), kind: "in"})

	read := func(ctxTenant, asked uuid.UUID) []store.ListWorkedShiftEventsRow {
		var rows []store.ListWorkedShiftEventsRow
		if err := data.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = store.New(tx).ListWorkedShiftEvents(ctx, store.ListWorkedShiftEventsParams{
				TenantID: asked, FromAt: now.Add(-2 * time.Hour), UntilAt: now, RowLimit: 10})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := read(b.id, b.id); len(rows) != 1 || *rows[0].EmployeeID != bWho {
		t.Fatalf("positive control: B's context reads %+v, want B's one record", rows)
	}
	if rows := read(a.id, b.id); len(rows) != 0 {
		t.Errorf("RLS: A's context asking for B's shift events got %d rows", len(rows))
	}
	var seen []uuid.UUID
	if err := data.WithTenant(context.Background(), a.id, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.employee_id FROM transactions t
		                           LEFT JOIN employees e ON e.id = t.employee_id
		                           LEFT JOIN locations l ON l.id = t.location_id
		                           WHERE t.type IS NOT NULL AND NOT t.practice AND t.occurred_at >= $1 AND t.occurred_at < $2`,
			now.Add(-2*time.Hour), now)
		if err != nil {
			return err
		}
		seen, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(seen, aWho) {
		t.Fatal("positive control: A's tenant-filter-free read does not see A's own record")
	}
	if slices.Contains(seen, bWho) {
		t.Error("RLS: A's tenant-filter-free read sees B's record")
	}
	s, err := readerAt(t, data, now, nil).Screen(context.Background(), a.id, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range s.Tape.Venues {
		if v.LocationID == b.venues["Door"] {
			t.Errorf("RLS: A's tape has B's venue row %+v", v)
		}
	}
}

// TestLeftOpen_CountsYesterdaysCheckInsNoCheckoutClosed is M12-15 against real rows: yesterday's
// calendar day in the tenant's zone, edges included and excluded, flagged entries in, training taps
// and closed entries (closed today included) out.
func TestLeftOpen_CountsYesterdaysCheckInsNoCheckoutClosed(t *testing.T) {
	data := passDB(t)
	now := hLocal(t, "2026-09-02", "10:30:00")
	b := newPassTenant(t, data, "Left open", "Door")
	in := func(name, day, clock, verdict string, practice bool) uuid.UUID {
		who := b.person(name, "Door")
		b.tap(passTap{who: who, venue: "Door", at: hLocal(t, day, clock), kind: "in", verdict: verdict, practice: practice})
		return who
	}
	in("Open since 08:00", "2026-09-01", "08:00:00", "ok", false)
	in("Open since the last minute", "2026-09-01", "23:59:00", "ok", false)
	in("Flagged and open", "2026-09-01", "12:00:00", "flag", false)
	in("Training tap", "2026-09-01", "09:00:00", "ok", true)
	in("Open from the day before", "2026-08-31", "23:30:00", "ok", false)
	in("Open from today", "2026-09-02", "00:00:00", "ok", false)
	night := in("Closed after midnight", "2026-09-01", "20:00:00", "ok", false)
	b.tap(passTap{who: night, venue: "Door", at: hLocal(t, "2026-09-02", "02:00:00"), kind: "out"})

	s, err := readerAt(t, data, now, nil).Screen(context.Background(), b.id, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	want := LeftOpen{State: Measured, N: 3, Day: Date{Year: 2026, Month: time.September, Day: 1}}
	if s.LeftOpen != want {
		t.Errorf("left open %+v, want %+v", s.LeftOpen, want)
	}
}

// TestLeftOpen_SaysNPlusPastTheCap: AnomalyRowCap open check-ins are an exact count; one more is
// "N+" -- the cap of the Odd shapes list the number links to.
func TestLeftOpen_SaysNPlusPastTheCap(t *testing.T) {
	data := passDB(t)
	now := hLocal(t, "2026-09-02", "10:30:00")
	b := newPassTenant(t, data, "Capped", "Door")
	who := b.person("Many entries", "Door")
	add := func(n, offset int) {
		b.exec(`INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, type, verdict, channel)
		        SELECT $1, $2, $3, $4::timestamptz + make_interval(mins => g), 'in', 'ok', 'nfc'
		        FROM generate_series($5::int, $5::int + $6::int - 1) g`,
			b.id, who, b.venues["Door"], hLocal(t, "2026-09-01", "08:00:00"), offset, n)
	}
	read := func() LeftOpen {
		s, err := readerAt(t, data, now, nil).Screen(context.Background(), b.id, Filter{})
		if err != nil {
			t.Fatal(err)
		}
		return s.LeftOpen
	}
	add(AnomalyRowCap, 0)
	if got := read(); got.N != AnomalyRowCap || got.Capped {
		t.Errorf("exactly the cap: %+v, want N=%d, not capped", got, AnomalyRowCap)
	}
	add(1, AnomalyRowCap)
	if got := read(); got.N != AnomalyRowCap || !got.Capped {
		t.Errorf("one past the cap: %+v, want N=%d and Capped", got, AnomalyRowCap)
	}
}

// TestRLS_LeftOpen_AnotherTenantsOpenCheckInsAreNotCounted: ListOpenCheckIns asked in A's context
// for B answers nothing; a probe with no tenant predicate in A's context sees A's open check-in and
// not B's. Positive control: B's own context.
func TestRLS_LeftOpen_AnotherTenantsOpenCheckInsAreNotCounted(t *testing.T) {
	data := passDB(t)
	now := time.Now()
	a := newPassTenant(t, data, "Open RLS A", "Door")
	b := newPassTenant(t, data, "Open RLS B", "Door")
	aWho, bWho := a.person("A open", "Door"), b.person("B open", "Door")
	from := now.Add(-48 * time.Hour)
	a.tap(passTap{who: aWho, venue: "Door", at: now.Add(-30 * time.Hour), kind: "in"})
	b.tap(passTap{who: bWho, venue: "Door", at: now.Add(-30 * time.Hour), kind: "in"})

	count := func(ctxTenant, asked uuid.UUID) int {
		var n int
		if err := data.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := store.New(tx).ListOpenCheckIns(ctx, store.ListOpenCheckInsParams{TenantID: asked, FromAt: from, ToAt: now, RowLimit: 10})
			n = len(rows)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(b.id, b.id); n != 1 {
		t.Fatalf("positive control: B's context counts %d of B's open check-ins, want 1", n)
	}
	if n := count(a.id, b.id); n != 0 {
		t.Errorf("RLS: A's context asking for B's open check-ins got %d", n)
	}
	var seen []uuid.UUID
	if err := data.WithTenant(context.Background(), a.id, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.employee_id FROM transactions t
		                           LEFT JOIN employees e ON e.id = t.employee_id
		                           LEFT JOIN locations l ON l.id = t.location_id
		                           WHERE t.occurred_at >= $1 AND t.occurred_at < $2 AND t.type = 'in' AND NOT t.practice
		                             AND NOT EXISTS (SELECT 1 FROM transactions o WHERE o.employee_id = t.employee_id
		                                               AND o.type = 'out' AND o.occurred_at > t.occurred_at)`, from, now)
		if err != nil {
			return err
		}
		seen, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(seen, aWho) {
		t.Fatal("positive control: A's tenant-filter-free read does not see A's open check-in")
	}
	if slices.Contains(seen, bWho) {
		t.Error("RLS: A's tenant-filter-free read sees B's open check-in")
	}
}
