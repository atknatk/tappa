package ledger

// people_db_test.go -- the People screen's reads (M12-17 ...), against real Postgres, as tappa_app
// in the reading tenant's context -- the panel's shape. The holding (K-39) cases live in
// people_holding_db_test.go.

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// peopleDB is the application pool, or a skip when no database is configured.
func peopleDB(t *testing.T) *db.DB {
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

// peopleTenant is one business of the People fixtures, written through the application role in
// its own context.
type peopleTenant struct {
	t           *testing.T
	data        *db.DB
	id          uuid.UUID
	venues      map[string]uuid.UUID
	departments map[string]uuid.UUID
}

func newPeopleTenant(t *testing.T, data *db.DB, label string) *peopleTenant {
	t.Helper()
	p := &peopleTenant{t: t, data: data, id: uuid.New(), venues: map[string]uuid.UUID{}, departments: map[string]uuid.UUID{}}
	p.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
	        VALUES ($1, $2, $3, 'bar', 'multi', $4)`, p.id, "People "+label, "VAT-PEOPLE-"+p.id.String(), maltaTenantZone)
	return p
}

func (p *peopleTenant) exec(sql string, args ...any) {
	p.t.Helper()
	if err := p.data.WithTenant(context.Background(), p.id, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}); err != nil {
		p.t.Fatalf("%s: %v", sql, err)
	}
}

func (p *peopleTenant) venue(name string) uuid.UUID {
	p.t.Helper()
	id := uuid.New()
	p.exec(`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`, id, p.id, name)
	p.venues[name] = id
	return id
}

func (p *peopleTenant) department(name, venue string) uuid.UUID {
	p.t.Helper()
	id := uuid.New()
	p.exec(`INSERT INTO departments (id, tenant_id, location_id, name) VALUES ($1, $2, $3, $4)`, id, p.id, p.venues[venue], name)
	p.departments[name] = id
	return id
}

// peoplePerson is one employee of the fixtures. venue "" is an API-managed row with no home venue
// (K-14), which the schema only admits with an external_ref.
type peoplePerson struct {
	name, status, venue, department, role string
}

func (p *peopleTenant) person(e peoplePerson) uuid.UUID {
	p.t.Helper()
	id := uuid.New()
	var venue, department, ref, role any
	if e.venue != "" {
		venue = p.venues[e.venue]
	} else {
		ref = "ext-" + id.String()[:8]
	}
	if e.department != "" {
		department = p.departments[e.department]
	}
	if e.role != "" {
		role = e.role
	}
	if e.status == "" {
		e.status = StatusActive
	}
	p.exec(`INSERT INTO employees (id, tenant_id, location_id, department_id, external_ref, full_name, role, status)
	        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, p.id, venue, department, ref, e.name, role, e.status)
	return id
}

// failOneQueryDB is the production Database whose transactions answer ONE named sqlc statement
// with a genuine statement error (division by zero, executed on the real connection), so the
// transaction really is aborted -- the shape a permission slip or a statement timeout produces.
// Everything else, the real db.WithTenant and its COMMIT included, is production's.
type failOneQueryDB struct {
	real *db.DB
	name string
}

func (d failOneQueryDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, failOneQueryTx{Tx: tx, name: d.name})
	})
}

// failOneQueryTx intercepts the named statement on the transaction and on every savepoint
// opened on it.
type failOneQueryTx struct {
	pgx.Tx
	name string
}

func (f failOneQueryTx) Begin(ctx context.Context) (pgx.Tx, error) {
	sp, err := f.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failOneQueryTx{Tx: sp, name: f.name}, nil
}

func (f failOneQueryTx) swap(sql string, args []any) (string, []any) {
	if strings.HasPrefix(sql, "-- name: "+f.name+" ") {
		return "SELECT 1/0", nil
	}
	return sql, args
}

func (f failOneQueryTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	sql, args = f.swap(sql, args)
	return f.Tx.Query(ctx, sql, args...)
}

func (f failOneQueryTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	sql, args = f.swap(sql, args)
	return f.Tx.QueryRow(ctx, sql, args...)
}

// walkRoster pages through the whole roster for f and answers every person it listed.
func walkRoster(t *testing.T, r *Reader, tenantID uuid.UUID, f RosterFilter) []Person {
	t.Helper()
	var all []Person
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatal("the roster never reached its last page")
		}
		s, err := r.Roster(context.Background(), tenantID, f)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, s.People...)
		if s.NextID == nil {
			return all
		}
		f.AfterID = s.NextID
	}
}

// TestStatusTally_CountsWhatTheRosterLists is M12-17's definition: for every filter set, each
// status chip is exactly how many people the list holds with that chip picked, walked page by
// page; the everyone counts ignore every filter.
func TestStatusTally_CountsWhatTheRosterLists(t *testing.T) {
	data := peopleDB(t)
	b := newPeopleTenant(t, data, "Tally")
	b.venue("Bar")
	b.venue("Kitchen")
	b.department("Front", "Bar")
	b.department("Line", "Kitchen")
	people := []peoplePerson{
		{name: "Anna Borg", status: StatusActive, venue: "Bar", department: "Front"},
		{name: "Daniel Vella", status: StatusActive, venue: "Bar"},
		{name: "Hannah Grech", status: StatusInvited, venue: "Bar", department: "Front"},
		{name: "Karl Farrugia", status: StatusDeactivated, venue: "Bar"},
		{name: "Maria Camilleri", status: StatusActive, venue: "Kitchen", department: "Line"},
		{name: "Joanna Zammit", status: StatusDeactivated, venue: "Kitchen", department: "Line"},
		{name: "Paul Attard", status: StatusInvited, venue: "Kitchen"},
		{name: "Integration Only", status: StatusActive}, // no home venue (K-14)
		// The escaping is shared: a name carrying LIKE's wildcards is found by its own
		// characters and matched by nothing else's.
		{name: "Fifty%_Off", status: StatusInvited, venue: "Kitchen"},
	}
	for _, p := range people {
		b.person(p)
	}

	r, err := NewReader(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		f    RosterFilter
	}{
		{"no filter", RosterFilter{}},
		{"venue", RosterFilter{LocationID: ptrUUID(b.venues["Bar"])}},
		{"department", RosterFilter{DepartmentID: ptrUUID(b.departments["Line"])}},
		{"name", RosterFilter{Name: "an"}},
		{"name with a wildcard", RosterFilter{Name: "%_"}},
		{"venue and name", RosterFilter{LocationID: ptrUUID(b.venues["Kitchen"]), Name: "a"}},
		// The status filter narrows the list and NOT the chips: each chip is what picking it
		// would show.
		{"status is not a chip filter", RosterFilter{Status: StatusActive, Name: "a"}},
	}
	everyone := StatusCounts{}
	for _, p := range people {
		addTo(&everyone, p.status)
	}
	if everyone.Total() != len(people) {
		t.Fatalf("fixture: %d people, %+v", len(people), everyone)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := r.Roster(context.Background(), b.id, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if s.Tally.State != Measured {
				t.Fatalf("tally state %v, want Measured", s.Tally.State)
			}
			if s.Tally.Everyone != everyone {
				t.Errorf("everyone %+v, want %+v (the kicker ignores every filter)", s.Tally.Everyone, everyone)
			}
			for _, status := range EmployeeStatuses {
				f := tc.f
				f.Status = status
				listed := walkRoster(t, r, b.id, f)
				if got := s.Tally.Matching.Of(status); got != len(listed) {
					t.Errorf("the %s chip says %d, the list with it picked holds %d", status, got, len(listed))
				}
			}
		})
	}
	// Anti-vacuity: the cases above must disagree with each other somewhere, or a tally that
	// ignored its filters would pass.
	if s, _ := r.Roster(context.Background(), b.id, RosterFilter{Name: "%_"}); s.Tally.Matching.Total() != 1 {
		t.Errorf("the wildcard name matches %d people, want exactly the one named with them", s.Tally.Matching.Total())
	}
}

func addTo(c *StatusCounts, status string) {
	c.counts[statusIndex(status)]++
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }

// TestRLS_StatusTally_AnotherTenantsPeopleAreNotCounted: A's connection does not count B's
// people. The isolation probe carries no tenant predicate (CLAUDE.md §6 -- a WHERE would make the
// 0 the WHERE's, not RLS's); the production statement is then asked, from A's context, for B's
// tenant id.
func TestRLS_StatusTally_AnotherTenantsPeopleAreNotCounted(t *testing.T) {
	data := peopleDB(t)
	a := newPeopleTenant(t, data, "RLS tally A")
	b := newPeopleTenant(t, data, "RLS tally B")
	a.venue("Door")
	b.venue("Door")
	a.person(peoplePerson{name: "A one", venue: "Door"})
	for _, n := range []string{"B one", "B two", "B three"} {
		b.person(peoplePerson{name: n, venue: "Door", status: StatusInvited})
	}

	// The statement's own shape with every tenant predicate removed.
	const whereless = `SELECT e.status, count(*)::bigint FROM employees e GROUP BY e.status`
	seenBy := func(tenant uuid.UUID) map[string]int64 {
		seen := map[string]int64{}
		if err := data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, whereless)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var s string
				var n int64
				if err := rows.Scan(&s, &n); err != nil {
					return err
				}
				seen[s] = n
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return seen
	}
	if got := seenBy(b.id); got[StatusInvited] != 3 || len(got) != 1 {
		t.Fatalf("positive control: B's own context counts %v, want 3 invited", got)
	}
	if got := seenBy(a.id); got[StatusActive] != 1 || len(got) != 1 {
		t.Errorf("RLS: A's tenant-filter-free count is %v, want A's one active person and nothing of B's", got)
	}

	for _, ctxTenant := range []uuid.UUID{a.id, b.id} {
		var rows []store.CountPanelEmployeesByStatusRow
		if err := data.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = store.New(tx).CountPanelEmployeesByStatus(ctx, store.CountPanelEmployeesByStatusParams{TenantID: b.id})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		switch {
		case ctxTenant == b.id && (len(rows) != 1 || rows[0].Everyone != 3):
			t.Errorf("positive control: B's context, B's tally: %+v", rows)
		case ctxTenant == a.id && len(rows) != 0:
			t.Errorf("RLS: A's context asked for B's tally and got %+v", rows)
		}
	}
}

// TestRoster_AFailedTallyCostsTheChipsAndNeverTheRoster drives Roster through its COMMIT with the
// count failing for real: the list stands, the tally is ReadFailed (never a zero), and the
// failure is logged.
func TestRoster_AFailedTallyCostsTheChipsAndNeverTheRoster(t *testing.T) {
	data := peopleDB(t)
	b := newPeopleTenant(t, data, "Tally fails")
	b.venue("Bar")
	b.person(peoplePerson{name: "Still listed", venue: "Bar"})

	var logged logCounter
	r, err := NewReader(failOneQueryDB{real: data, name: "CountPanelEmployeesByStatus"}, slog.New(&logged))
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Roster(context.Background(), b.id, RosterFilter{})
	if err != nil {
		t.Fatalf("Roster returned %v: a failed chip count took the roster with it", err)
	}
	if !s.Queried || len(s.People) != 1 || len(s.Options.Locations) != 1 {
		t.Fatalf("the page came back queried=%v with %d people and %d venues", s.Queried, len(s.People), len(s.Options.Locations))
	}
	if s.Tally.State != ReadFailed || s.Tally.Everyone.Total() != 0 || s.Tally.Matching.Total() != 0 {
		t.Errorf("tally %+v, want ReadFailed and no figure", s.Tally)
	}
	if logged.errors() != 1 {
		t.Errorf("%d errors logged, want 1", logged.errors())
	}

	// Control: the same reader shape without the injected failure measures.
	ok, err := NewReader(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := ok.Roster(context.Background(), b.id, RosterFilter{}); err != nil || s.Tally.State != Measured || s.Tally.Everyone.Of(StatusActive) != 1 {
		t.Errorf("control: %+v, %v", s.Tally, err)
	}
}

// TestStatusTally_SharesTheRostersPredicates: the chips are counted with the list's own venue,
// department and name predicates, spelled the same way, and with neither its status predicate
// nor its cursor. A predicate edited in one statement and not the other is red here before any
// manager sees a chip that disagrees with the list under it.
func TestStatusTally_SharesTheRostersPredicates(t *testing.T) {
	list := querySource(t, "employees.sql", "ListPanelEmployees")
	count := querySource(t, "employees.sql", "CountPanelEmployeesByStatus")
	predicate := regexp.MustCompile(`\(sqlc\.narg\('(\w+)'\)::\w+ IS NULL\s+OR [^()]*(?:\([^()]*\)[^()]*)*\)`)
	shared := map[string]string{}
	for _, m := range predicate.FindAllStringSubmatch(list, -1) {
		shared[m[1]] = strings.Join(strings.Fields(m[0]), " ")
	}
	for _, name := range []string{"location_id", "department_id", "full_name"} {
		p, ok := shared[name]
		if !ok {
			t.Fatalf("ListPanelEmployees has no %s predicate the scan can find (found %v)", name, shared)
		}
		if !strings.Contains(strings.Join(strings.Fields(count), " "), p) {
			t.Errorf("CountPanelEmployeesByStatus does not carry ListPanelEmployees' %s predicate:\n  %s", name, p)
		}
	}
	for _, absent := range []string{"narg('status')", "narg('cursor_name')", "narg('cursor_id')"} {
		if strings.Contains(count, absent) {
			t.Errorf("CountPanelEmployeesByStatus carries %s: a chip would then count one status or one page", absent)
		}
	}
}

// querySource is one named statement of db/queries/<file>, its comment lines dropped.
func querySource(t *testing.T, file, name string) string {
	t.Helper()
	b, err := os.ReadFile("../../../db/queries/" + file)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "-- name: "+name+" ")
	if start < 0 {
		t.Fatalf("%s has no statement %s", file, name)
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\n-- name: "); next >= 0 {
		rest = rest[:next]
	}
	var body strings.Builder
	for _, line := range strings.Split(rest, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			body.WriteString(line + "\n")
		}
	}
	return body.String()
}

// peopleTap is one record of the fixtures. kind "" is a record with no direction (a refusal or
// an ignored repeat).
type peopleTap struct {
	who      uuid.UUID
	venue    string
	at       time.Time
	kind     string
	verdict  string
	channel  string
	practice bool
}

func (p *peopleTenant) tap(r peopleTap) uuid.UUID {
	p.t.Helper()
	if r.verdict == "" {
		r.verdict = "ok"
	}
	if r.channel == "" {
		r.channel = "nfc"
	}
	var kind, venue any
	if r.kind != "" {
		kind = r.kind
	}
	if r.venue != "" {
		venue = p.venues[r.venue]
	}
	id := uuid.New()
	p.exec(`INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, type, verdict, channel, practice, queued)
	        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $7 = 'flag')`,
		id, p.id, r.who, venue, r.at, kind, r.verdict, r.channel, r.practice)
	return id
}

// review records a manager's decision on a flagged record, by a manager of this business.
func (p *peopleTenant) review(txn uuid.UUID, outcome string) {
	p.t.Helper()
	admin := uuid.New()
	p.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		admin, p.id, "owner-"+admin.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	p.exec(`INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, $4)`,
		p.id, txn, admin, outcome)
}

// TestRoster_EachRowCarriesItsRoleAndLastTap is M12-18's definition: the role as typed, and the
// newest non-practice record's instant, verdict word and channel -- or none.
func TestRoster_EachRowCarriesItsRoleAndLastTap(t *testing.T) {
	data := peopleDB(t)
	b := newPeopleTenant(t, data, "Last tap")
	b.venue("Bar")
	base := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)

	worked := b.person(peoplePerson{name: "A worked a shift", venue: "Bar", role: "Barista"})
	b.tap(peopleTap{who: worked, venue: "Bar", at: base, kind: "in"})
	b.tap(peopleTap{who: worked, venue: "Bar", at: base.Add(8 * time.Hour), kind: "out", verdict: "flag"})
	// A practice row after the last real one is not attendance and is not the last tap.
	b.tap(peopleTap{who: worked, venue: "Bar", at: base.Add(9 * time.Hour), kind: "in", practice: true})

	refused := b.person(peoplePerson{name: "B was refused", venue: "Bar"})
	b.tap(peopleTap{who: refused, venue: "Bar", at: base, kind: "in"})
	b.tap(peopleTap{who: refused, venue: "Bar", at: base.Add(time.Hour), verdict: "reject"})

	byHand := b.person(peoplePerson{name: "C was entered by hand", venue: "Bar"})
	b.tap(peopleTap{who: byHand, venue: "Bar", at: base.Add(2 * time.Hour), kind: "in", channel: "manual"})

	never := b.person(peoplePerson{name: "D never tapped", venue: "Bar"})
	onlyPractice := b.person(peoplePerson{name: "E only practised", venue: "Bar"})
	b.tap(peopleTap{who: onlyPractice, venue: "Bar", at: base, kind: "in", practice: true})

	// Another business's newer record of nobody here must not appear anywhere.
	other := newPeopleTenant(t, data, "Last tap other")
	other.venue("Bar")
	stranger := other.person(peoplePerson{name: "A stranger", venue: "Bar"})
	other.tap(peopleTap{who: stranger, venue: "Bar", at: base.Add(20 * time.Hour), kind: "in"})

	r, err := NewReader(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Roster(context.Background(), b.id, RosterFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if s.LastTaps != Measured {
		t.Fatalf("last taps %v, want Measured", s.LastTaps)
	}
	got := map[uuid.UUID]Person{}
	for _, p := range s.People {
		got[p.ID] = p
	}
	if len(got) != 5 {
		t.Fatalf("%d people listed, want 5", len(got))
	}
	want := map[uuid.UUID]*LastTap{
		worked:       {At: base.Add(8 * time.Hour), Verdict: "flag", Channel: "nfc"},
		refused:      {At: base.Add(time.Hour), Verdict: "reject", Channel: "nfc"},
		byHand:       {At: base.Add(2 * time.Hour), Verdict: "ok", Channel: "manual"},
		never:        nil,
		onlyPractice: nil,
	}
	for id, w := range want {
		p := got[id]
		switch {
		case w == nil && p.LastTap != nil:
			t.Errorf("%s: last tap %+v, want none", p.Name, *p.LastTap)
		case w != nil && p.LastTap == nil:
			t.Errorf("%s: no last tap, want %+v", p.Name, *w)
		case w != nil && (!p.LastTap.At.Equal(w.At) || p.LastTap.Verdict != w.Verdict || p.LastTap.Channel != w.Channel):
			t.Errorf("%s: last tap %+v, want %+v", p.Name, *p.LastTap, *w)
		}
	}
	if got[worked].Role != "Barista" || got[never].Role != "" {
		t.Errorf("roles %q / %q, want the typed title and nothing", got[worked].Role, got[never].Role)
	}
}

// TestRoster_AFailedLastTapReadCostsTheTapsAndNeverTheRoster: with ListLastTapsForEmployees
// failing for real, the roster and its chips stand, no row claims a last tap, and the page says
// the read failed rather than that nobody tapped.
func TestRoster_AFailedLastTapReadCostsTheTapsAndNeverTheRoster(t *testing.T) {
	data := peopleDB(t)
	b := newPeopleTenant(t, data, "Last tap fails")
	b.venue("Bar")
	who := b.person(peoplePerson{name: "Tapped once", venue: "Bar"})
	b.tap(peopleTap{who: who, venue: "Bar", at: time.Now().Add(-time.Hour), kind: "in"})

	var logged logCounter
	r, err := NewReader(failOneQueryDB{real: data, name: "ListLastTapsForEmployees"}, slog.New(&logged))
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Roster(context.Background(), b.id, RosterFilter{})
	if err != nil {
		t.Fatalf("Roster returned %v: a failed last-tap read took the roster with it", err)
	}
	if len(s.People) != 1 || s.LastTaps != ReadFailed || s.People[0].LastTap != nil {
		t.Fatalf("people %d, last taps %v, row tap %+v; want the row, ReadFailed and no tap", len(s.People), s.LastTaps, s.People[0].LastTap)
	}
	if s.Tally.State != Measured {
		t.Errorf("the chips are %v: the failed last-tap savepoint cost the tally too", s.Tally.State)
	}
	if logged.errors() != 1 {
		t.Errorf("%d errors logged, want 1", logged.errors())
	}
	ok, err := NewReader(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := ok.Roster(context.Background(), b.id, RosterFilter{}); err != nil || s.LastTaps != Measured || s.People[0].LastTap == nil {
		t.Errorf("control: %v, %v", s.LastTaps, err)
	}
}

// TestRLS_LastTaps_AnotherTenantsRecordsAreNotRead: A's connection reads none of B's records,
// even when it is handed B's employee ids. The probe carries no tenant predicate (CLAUDE.md §6).
func TestRLS_LastTaps_AnotherTenantsRecordsAreNotRead(t *testing.T) {
	data := peopleDB(t)
	a := newPeopleTenant(t, data, "RLS last A")
	b := newPeopleTenant(t, data, "RLS last B")
	a.venue("Door")
	b.venue("Door")
	aWho := a.person(peoplePerson{name: "A", venue: "Door"})
	bWho := b.person(peoplePerson{name: "B", venue: "Door"})
	at := time.Now().Add(-time.Hour)
	a.tap(peopleTap{who: aWho, venue: "Door", at: at, kind: "in"})
	b.tap(peopleTap{who: bWho, venue: "Door", at: at, kind: "in"})

	// The statement's own shape with every tenant predicate removed.
	const whereless = `
		SELECT p.employee_id
		FROM unnest($1::uuid[]) AS p(employee_id)
		CROSS JOIN LATERAL (
		    SELECT t.occurred_at FROM transactions t
		    WHERE t.employee_id = p.employee_id AND NOT t.practice
		    ORDER BY t.occurred_at DESC LIMIT 1) lt`
	both := []uuid.UUID{aWho, bWho}
	seenBy := func(tenant uuid.UUID) map[uuid.UUID]bool {
		seen := map[uuid.UUID]bool{}
		if err := data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, whereless, both)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			for _, id := range ids {
				seen[id] = true
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return seen
	}
	if seen := seenBy(b.id); !seen[bWho] {
		t.Fatal("positive control: B's own context does not see B's record")
	}
	seen := seenBy(a.id)
	if !seen[aWho] {
		t.Fatal("positive control: A's context does not see A's own record")
	}
	if seen[bWho] {
		t.Error("RLS: A's tenant-filter-free read sees B's record")
	}

	for _, ctxTenant := range []uuid.UUID{a.id, b.id} {
		var rows []store.ListLastTapsForEmployeesRow
		if err := data.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = store.New(tx).ListLastTapsForEmployees(ctx, store.ListLastTapsForEmployeesParams{TenantID: b.id, EmployeeIds: both})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		switch {
		case ctxTenant == b.id && (len(rows) != 1 || rows[0].EmployeeID != bWho):
			t.Errorf("positive control: B's context, B's last taps: %+v", rows)
		case ctxTenant == a.id && len(rows) != 0:
			t.Errorf("RLS: A's context asked for B's last taps and got %+v", rows)
		}
	}
}

// TestRecentTaps_TheLastSevenNewestFirst is M12-19's definition: one person's seven newest
// non-practice records, newest first, each its instant, direction ("" for none), verdict word,
// channel and a manager's later decision.
func TestRecentTaps_TheLastSevenNewestFirst(t *testing.T) {
	data := peopleDB(t)
	b := newPeopleTenant(t, data, "Recent")
	b.venue("Bar")
	who := b.person(peoplePerson{name: "Has a history", venue: "Bar"})
	neighbour := b.person(peoplePerson{name: "Taps beside them", venue: "Bar"})
	base := time.Date(2026, time.September, 1, 9, 0, 0, 0, time.UTC)
	hour := func(n int) time.Time { return base.Add(time.Duration(n) * time.Hour) }
	// Ten records over ten hours; the newest seven are hours 3..9 -- except hour 8 is practice,
	// so the seven are 9, 7, 6, 5, 4, 3, 2.
	var flagged uuid.UUID
	for n := 0; n < 10; n++ {
		r := peopleTap{who: who, venue: "Bar", at: hour(n), kind: []string{"in", "out"}[n%2]}
		switch n {
		case 8:
			r.practice = true
		case 9:
			r.kind, r.verdict = "", "reject"
		case 5:
			r.verdict = "flag"
		case 4:
			r.kind, r.verdict = "", "ignored"
		case 3:
			r.channel = "manual"
		}
		id := b.tap(r)
		if n == 5 {
			flagged = id
		}
	}
	b.review(flagged, "approved")
	// Another person's newer record is not this person's.
	b.tap(peopleTap{who: neighbour, venue: "Bar", at: hour(20), kind: "in"})

	r, err := NewReader(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := r.RecentTaps(context.Background(), b.id, who)
	if got.State != Measured || len(got.Taps) != RecentTapCount {
		t.Fatalf("state %v with %d taps, want Measured and %d", got.State, len(got.Taps), RecentTapCount)
	}
	want := []RecentTap{
		{At: hour(9), Direction: "", Verdict: "reject", Channel: "nfc"},
		{At: hour(7), Direction: "out", Verdict: "ok", Channel: "nfc"},
		{At: hour(6), Direction: "in", Verdict: "ok", Channel: "nfc"},
		{At: hour(5), Direction: "out", Verdict: "flag", Channel: "nfc", Review: "approved"},
		{At: hour(4), Direction: "", Verdict: "ignored", Channel: "nfc"},
		{At: hour(3), Direction: "out", Verdict: "ok", Channel: "manual"},
		{At: hour(2), Direction: "in", Verdict: "ok", Channel: "nfc"},
	}
	for i, w := range want {
		g := got.Taps[i]
		if !g.At.Equal(w.At) || g.Direction != w.Direction || g.Verdict != w.Verdict || g.Channel != w.Channel || g.Review != w.Review {
			t.Errorf("tap %d: %+v, want %+v", i, g, w)
		}
	}

	// Somebody with no record is a MEASURED empty strip; an id of no one here is the same.
	none := b.person(peoplePerson{name: "Never tapped", venue: "Bar"})
	for _, id := range []uuid.UUID{none, uuid.New()} {
		if got := r.RecentTaps(context.Background(), b.id, id); got.State != Measured || len(got.Taps) != 0 {
			t.Errorf("%v: state %v with %d taps, want a measured empty strip", id, got.State, len(got.Taps))
		}
	}
}

// TestRecentTaps_AFailedReadSaysSoAndNeverEmpty: the strip's statement failing for real is
// ReadFailed and logged -- never a Measured empty strip, which would read as "no record yet".
func TestRecentTaps_AFailedReadSaysSoAndNeverEmpty(t *testing.T) {
	data := peopleDB(t)
	b := newPeopleTenant(t, data, "Recent fails")
	b.venue("Bar")
	who := b.person(peoplePerson{name: "Tapped", venue: "Bar"})
	b.tap(peopleTap{who: who, venue: "Bar", at: time.Now().Add(-time.Hour), kind: "in"})

	var logged logCounter
	r, err := NewReader(failOneQueryDB{real: data, name: "ListRecentTapsForEmployee"}, slog.New(&logged))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.RecentTaps(context.Background(), b.id, who); got.State != ReadFailed || len(got.Taps) != 0 {
		t.Errorf("state %v with %d taps, want ReadFailed and none", got.State, len(got.Taps))
	}
	if logged.errors() != 1 {
		t.Errorf("%d errors logged, want 1", logged.errors())
	}
	ok, err := NewReader(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ok.RecentTaps(context.Background(), b.id, who); got.State != Measured || len(got.Taps) != 1 {
		t.Errorf("control: %v with %d taps", got.State, len(got.Taps))
	}
}

// TestRLS_RecentTaps_AnotherTenantsRecordsAreNotRead: A's connection reads none of B's records,
// even asked for B's employee. The probe carries no tenant predicate (CLAUDE.md §6).
func TestRLS_RecentTaps_AnotherTenantsRecordsAreNotRead(t *testing.T) {
	data := peopleDB(t)
	a := newPeopleTenant(t, data, "RLS recent A")
	b := newPeopleTenant(t, data, "RLS recent B")
	b.venue("Door")
	bWho := b.person(peoplePerson{name: "B", venue: "Door"})
	b.tap(peopleTap{who: bWho, venue: "Door", at: time.Now().Add(-time.Hour), kind: "in"})

	const whereless = `SELECT count(*) FROM transactions t WHERE t.employee_id = $1 AND NOT t.practice`
	countBy := func(tenant uuid.UUID) int64 {
		var n int64
		if err := data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, whereless, bWho).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if countBy(b.id) != 1 {
		t.Fatal("positive control: B's own context does not see B's record")
	}
	if n := countBy(a.id); n != 0 {
		t.Errorf("RLS: A's tenant-filter-free read sees %d of B's records", n)
	}
	for _, ctxTenant := range []uuid.UUID{a.id, b.id} {
		var rows []store.ListRecentTapsForEmployeeRow
		if err := data.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			rows, err = store.New(tx).ListRecentTapsForEmployee(ctx, store.ListRecentTapsForEmployeeParams{TenantID: b.id, EmployeeID: &bWho})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		switch {
		case ctxTenant == b.id && len(rows) != 1:
			t.Errorf("positive control: B's context, B's strip: %+v", rows)
		case ctxTenant == a.id && len(rows) != 0:
			t.Errorf("RLS: A's context asked for B's strip and got %+v", rows)
		}
	}
}
