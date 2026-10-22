package tenant

// daycutoff_db_test.go -- M15-07's reads and its one write against REAL Postgres.
//
// 🔴 EVERY TEST BUILDS ITS OWN TENANT. The shared seed tenants' cutoffs are never written
// (the card says so outright, and every direction-chain test of M15-04 depends on them
// staying at the default); the seed is only READ, by the test that keeps the pure test's
// shift table equal to the seed's rows.
//
// FIXTURES ARE NOT CLEANED UP (staff_db_test.go's reason): employees and audit_log refuse
// DELETE to tappa_app, so fresh random ids keep runs apart.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

type cutoffFixture struct {
	t       *testing.T
	data    *db.DB
	cutoffs *DayCutoffs
	tenant  uuid.UUID
	actor   uuid.UUID
}

func newCutoffFixture(t *testing.T, from *time.Time) *cutoffFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the business day settings tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := NewDirectory(data, from)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewDayCutoffs(data, trail, dir, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	f := &cutoffFixture{t: t, data: data, cutoffs: c, tenant: uuid.New(), actor: uuid.New()}
	f.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure)
	        VALUES ($1, 'Cutoff Test Ltd', $2, 'restaurant', 'multi')`, f.tenant, "VAT-"+f.tenant.String())
	return f
}

func (f *cutoffFixture) exec(sql string, args ...any) {
	f.t.Helper()
	f.execIn(f.tenant, sql, args...)
}

func (f *cutoffFixture) execIn(tenantID uuid.UUID, sql string, args ...any) {
	f.t.Helper()
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}); err != nil {
		f.t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

// venue adds a venue with a shift ("" start = none).
func (f *cutoffFixture) venue(name, start, end string, overnight bool) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if start == "" {
		f.exec(`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`, id, f.tenant, name)
		return id
	}
	f.exec(`INSERT INTO locations (id, tenant_id, name, shift_start, shift_end, overnight)
	        VALUES ($1, $2, $3, $4::time, $5::time, $6)`, id, f.tenant, name, start, end, overnight)
	return id
}

func (f *cutoffFixture) department(venue uuid.UUID, name, start, end string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	f.exec(`INSERT INTO departments (id, tenant_id, location_id, name, shift_start, shift_end)
	        VALUES ($1, $2, $3, $4, $5::time, $6::time)`, id, f.tenant, venue, name, start, end)
	return id
}

func (f *cutoffFixture) person(name string, branch *uuid.UUID, department *uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	// A person with no profile branch is an API-managed one, which 00037 requires to carry
	// an external ref (employees_home_or_external_ref).
	var ref *string
	if branch == nil {
		r := "ext-" + id.String()
		ref = &r
	}
	f.exec(`INSERT INTO employees (id, tenant_id, location_id, department_id, full_name, status, invited_at, external_ref)
	        VALUES ($1, $2, $3, $4, $5, 'active', now(), $6)`, id, f.tenant, branch, department, name, ref)
	return id
}

// tap writes one record of a person at a venue, as the parity test of internal/domain/ledger
// writes its fixtures: directly, with its own instants.
func (f *cutoffFixture) tap(person, venue uuid.UUID, at time.Time, dir string) {
	f.t.Helper()
	f.exec(`INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, created_at,
	                                  verdict, channel, type, practice)
	        VALUES ($1, $2, $3, $4, $5, $5, 'ok', 'nfc', $6, false)`, uuid.New(), f.tenant, person, venue, at, dir)
}

func (f *cutoffFixture) set(level CutoffLevel, subject uuid.UUID, value *time.Duration) (CutoffChange, error) {
	return f.cutoffs.Set(context.Background(), SetCutoffCommand{TenantID: f.tenant, ActorID: f.actor, Level: level, SubjectID: subject, Value: value})
}

func (f *cutoffFixture) review(level CutoffLevel, subject uuid.UUID, value *time.Duration, now time.Time) CutoffReview {
	f.t.Helper()
	r, err := f.cutoffs.Review(context.Background(), CutoffReviewCommand{TenantID: f.tenant, Level: level, SubjectID: subject, Value: value, Now: now})
	if err != nil {
		f.t.Fatalf("Review(%s): %v", level, err)
	}
	return r
}

// tapCutoff is what the TAP reads for the person: GetEmployeeForTap's effective_day_cutoff.
func (f *cutoffFixture) tapCutoff(employee uuid.UUID) time.Duration {
	f.t.Helper()
	var out time.Duration
	if err := f.data.WithTenant(context.Background(), f.tenant, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetEmployeeForTap(ctx, store.GetEmployeeForTapParams{TenantID: f.tenant, EmployeeID: employee})
		if err != nil {
			return err
		}
		out = time.Duration(row.EffectiveDayCutoff.Microseconds) * time.Microsecond
		return nil
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *cutoffFixture) trail(action string, target uuid.UUID) []string {
	f.t.Helper()
	var out []string
	if err := f.data.WithTenant(context.Background(), f.tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3 ORDER BY at`,
			f.tenant, action, target.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func dur(h, m int) *time.Duration { d := hm(h, m); return &d }

// TestDayCutoffDB_ABranchCutoffReachesItsProfilePeopleAsFromTheBranch is the card's own
// departmentless tenant: a cutoff written on a branch makes the card of each person whose
// PROFILE branch it is say "from <branch>", with the value the tap itself reads
// (GetEmployeeForTap) -- for the branch's people, for a person with their own value, for a
// person of another branch and for one with no profile branch at all. The write is one row
// and one trail entry with the old and new value; the same value again writes neither.
func TestDayCutoffDB_ABranchCutoffReachesItsProfilePeopleAsFromTheBranch(t *testing.T) {
	f := newCutoffFixture(t, nil)
	sliema := f.venue("Sliema", "10:00", "22:00", false)
	gzira := f.venue("Gzira", "10:00", "22:00", false)
	maria := f.person("Maria Borg", &sliema, nil)
	joe := f.person("Joe Vella", &sliema, nil)
	anna := f.person("Anna Zammit", &gzira, nil)
	api := f.person("Api Person", nil, nil)
	f.exec(`UPDATE employees SET day_cutoff = '07:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, joe)
	// Kim is Sliema's too, in a department with its own value: the department is more
	// specific than the branch, so the branch does not reach her either.
	kitchen := f.department(sliema, "Kitchen", "10:00", "22:00")
	f.exec(`UPDATE departments SET day_cutoff = '04:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, kitchen)
	f.person("Kim Abela", &sliema, &kitchen)

	// Who the branch level reaches: Maria; not Joe (his own value is more specific), not Kim
	// (her department's is), not Anna (another branch), not the person with no profile branch.
	if r := f.review(CutoffBranch, sliema, dur(6, 0), time.Now()); r.People != 1 {
		t.Fatalf("the Sliema level reaches %d people, want Maria alone", r.People)
	}
	change, err := f.set(CutoffBranch, sliema, dur(6, 0))
	if err != nil {
		t.Fatal(err)
	}
	if change.From != nil || change.To == nil || *change.To != hm(6, 0) {
		t.Fatalf("change = %v -> %v, want inherit -> 06:00", change.From, change.To)
	}
	want := map[uuid.UUID]struct {
		value  time.Duration
		source CutoffSource
	}{
		maria: {hm(6, 0), CutoffSource{Level: CutoffBranch, Name: "Sliema"}},
		joe:   {hm(7, 0), CutoffSource{Level: CutoffPerson}},
		anna:  {hm(5, 0), CutoffSource{Level: CutoffBusiness}},
		api:   {hm(5, 0), CutoffSource{Level: CutoffBusiness}},
	}
	for id, w := range want {
		got, err := f.cutoffs.Person(context.Background(), f.tenant, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Value != w.value || got.Source != w.source {
			t.Errorf("%s: card %s %+v, want %s %+v", id, ClockText(&got.Value), got.Source, ClockText(&w.value), w.source)
		}
		if tap := f.tapCutoff(id); tap != got.Value {
			t.Errorf("%s: the card says %s and the tap reads %s", id, ClockText(&got.Value), ClockText(&tap))
		}
	}
	rows := f.trail(ActionLocationDayCutoffUpdated, sliema)
	if len(rows) != 1 || !strings.Contains(rows[0], `"from": ""`) || !strings.Contains(rows[0], `"to": "06:00"`) ||
		!strings.Contains(rows[0], `"level": "branch"`) || !strings.Contains(rows[0], `"subject_id": "`+sliema.String()+`"`) {
		t.Fatalf("trail %v, want one row inherit -> 06:00 naming the branch", rows)
	}
	if _, err := f.set(CutoffBranch, sliema, dur(6, 0)); !errors.Is(err, ErrSameDayCutoff) {
		t.Fatalf("the same value again: %v, want ErrSameDayCutoff", err)
	}
	if n := len(f.trail(ActionLocationDayCutoffUpdated, sliema)); n != 1 {
		t.Fatalf("the same value again wrote a trail row (%d)", n)
	}
	// Clearing is NULL -- inherit -- never 00:00.
	if _, err := f.set(CutoffBranch, sliema, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.tapCutoff(maria); got != hm(5, 0) {
		t.Fatalf("after clearing the branch the tap reads %s, want the business's 05:00", ClockText(&got))
	}
}

// TestDayCutoffDB_TheVenuesTappedInTheLast28DaysAreChecked is the "really worked" criterion:
// a Sliema-profile person who tapped at Rusty Bar (18:00-02:00) 27 days ago makes a 01:00
// Sliema cutoff warn about Rusty Bar's night; a Gzira-profile person whose Rusty Bar tap is
// 29 days old does not -- the window is CutoffTouchedWindow.
func TestDayCutoffDB_TheVenuesTappedInTheLast28DaysAreChecked(t *testing.T) {
	f := newCutoffFixture(t, nil)
	sliema := f.venue("Sliema", "10:00", "22:00", false)
	gzira := f.venue("Gzira", "10:00", "22:00", false)
	rusty := f.venue("Rusty Bar", "18:00", "02:00", true)
	maria := f.person("Maria Borg", &sliema, nil)
	anna := f.person("Anna Zammit", &gzira, nil)
	now := time.Now()
	f.tap(maria, rusty, now.Add(-27*24*time.Hour), "in")
	f.tap(anna, rusty, now.Add(-29*24*time.Hour), "in")

	r := f.review(CutoffBranch, sliema, dur(1, 0), now)
	if len(r.Conflicts) != 1 || r.Conflicts[0].Shift.Owner != "Rusty Bar" || r.Conflicts[0].Cutoff != hm(1, 0) {
		t.Fatalf("Sliema at 01:00: conflicts %+v, want Rusty Bar's night", r.Conflicts)
	}
	if r.People != 1 || r.Suggested == nil || len(CutoffConflicts(*r.Suggested, []CutoffShift{
		{Start: hm(10, 0), End: hm(22, 0)}, {Start: hm(18, 0), End: hm(2, 0), Overnight: true}})) != 0 {
		t.Fatalf("Sliema at 01:00: people %d, suggestion %v -- want 1 and a cutoff outside both bands", r.People, r.Suggested)
	}
	if r := f.review(CutoffBranch, gzira, dur(1, 0), now); len(r.Conflicts) != 0 {
		t.Fatalf("Gzira at 01:00: conflicts %+v, but the Rusty Bar tap is 29 days old", r.Conflicts)
	}
	// The PROFILE branch's shift, never the tapped one's, is the person's own (the first trap):
	// at 23:30 Sliema's 10-22 band (to 24:00) is cut and so is the night Maria worked.
	r = f.review(CutoffBranch, sliema, dur(23, 30), now)
	owners := []string{}
	for _, c := range r.Conflicts {
		owners = append(owners, c.Shift.Owner)
	}
	if strings.Join(owners, ",") != "Rusty Bar,Sliema" {
		t.Fatalf("Sliema at 23:30 cuts %v, want Rusty Bar and Sliema", owners)
	}
	// The same at the person level, where no level's own shift is added: Sliema's 10-22
	// arrives only as Maria's resolved (profile) shift.
	r = f.review(CutoffPerson, maria, dur(23, 30), now)
	owners = owners[:0]
	for _, c := range r.Conflicts {
		owners = append(owners, c.Shift.Owner)
	}
	if strings.Join(owners, ",") != "Rusty Bar,Sliema" {
		t.Fatalf("Maria at 23:30 cuts %v, want her profile branch's shift and the night she worked", owners)
	}
}

// TestDayCutoffDB_ADepartmentsOwnShiftIsWarnedBeforeAnybodyIsPlaced: the first shift entered
// on a department (04:00-12:00) is checked against the cutoff its people would take -- the
// business's 05:00 -- although nobody is in it yet; the department review's suggestion is
// outside the band, and a department cutoff written for it reaches its people over their
// branch's.
func TestDayCutoffDB_ADepartmentsOwnShiftIsWarnedBeforeAnybodyIsPlaced(t *testing.T) {
	f := newCutoffFixture(t, nil)
	plant := f.venue("Plant", "09:00", "17:00", false)
	bakery := f.department(plant, "Pastry & Bakery", "04:00", "12:00")
	now := time.Now()
	r := f.review(CutoffDepartment, bakery, nil, now)
	if r.Changes() || len(r.Conflicts) != 1 || r.Conflicts[0].Shift.Owner != "Pastry & Bakery" || r.Conflicts[0].Cutoff != hm(5, 0) {
		t.Fatalf("an empty department with a 04:00 shift: %+v", r)
	}
	if r.Suggested == nil || CutoffInBand(*r.Suggested, r.Conflicts[0].Shift) {
		t.Fatalf("suggestion %v is inside the band", r.Suggested)
	}
	baker := f.person("Baker", &plant, &bakery)
	f.exec(`UPDATE locations SET day_cutoff = '06:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, plant)
	if _, err := f.set(CutoffDepartment, bakery, dur(23, 0)); err != nil {
		t.Fatal(err)
	}
	got, err := f.cutoffs.Person(context.Background(), f.tenant, baker)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != hm(23, 0) || got.Source != (CutoffSource{Level: CutoffDepartment, Name: "Pastry & Bakery"}) || f.tapCutoff(baker) != hm(23, 0) {
		t.Fatalf("the baker's card %s %+v, tap %v", ClockText(&got.Value), got.Source, f.tapCutoff(baker))
	}
	if r := f.review(CutoffDepartment, bakery, dur(23, 0), now); len(r.Conflicts) != 0 || r.People != 1 {
		t.Fatalf("23:00 still cuts %+v (people %d)", r.Conflicts, r.People)
	}
}

// TestDayCutoffDB_CountsTheOpenCheckInsTheChangeWouldSplit: among the people who inherit the
// level, an entry open right now (ListOpenCheckIns' definition) whose business day the new
// cutoff ends earlier, inside its 18 hours, is counted; a closed pair and an open entry of a
// person with their own cutoff are not. The cutoffs are placed around the entry's own
// instant so the case holds whatever the clock says when the suite runs.
func TestDayCutoffDB_CountsTheOpenCheckInsTheChangeWouldSplit(t *testing.T) {
	f := newCutoffFixture(t, fixtures.BusinessDaySwitchOn())
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	sliema := f.venue("Sliema", "", "", false)
	maria := f.person("Maria Borg", &sliema, nil)
	joe := f.person("Joe Vella", &sliema, nil)
	paul := f.person("Paul Grech", &sliema, nil)
	now := time.Now().Truncate(time.Minute)
	in := now.Add(-2 * time.Hour)
	f.tap(maria, sliema, in, "in")
	f.tap(joe, sliema, in, "in")
	f.exec(`UPDATE employees SET day_cutoff = '05:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, joe)
	f.tap(paul, sliema, in, "in")
	f.tap(paul, sliema, in.Add(time.Hour), "out")

	wall := func(t time.Time) *time.Duration {
		l := t.In(loc)
		return dur(l.Hour(), l.Minute())
	}
	// The business day starts 6 h before the entry (B_old = in + 18 h) and would start 1 h
	// after it (B_new = in + 1 h): earlier, and inside its 18 hours.
	f.exec(`UPDATE locations SET day_cutoff = $3::time WHERE tenant_id = $1 AND id = $2`, f.tenant, sliema, ClockText(wall(in.Add(-6*time.Hour))))
	r := f.review(CutoffBranch, sliema, wall(in.Add(time.Hour)), now)
	if r.OpenSplit != 1 || r.OpenCapped {
		t.Fatalf("split %d (capped %v), want exactly Maria's entry", r.OpenSplit, r.OpenCapped)
	}
	// The same level, unchanged: nothing is split.
	if r := f.review(CutoffBranch, sliema, wall(in.Add(-6*time.Hour)), now); r.OpenSplit != 0 || r.Changes() {
		t.Fatalf("an unchanged cutoff splits %d", r.OpenSplit)
	}
}

// TestDayCutoffDB_AMoveSaysWhenItChangesTheCutoff: moving a person to a branch with another
// cutoff answers the old and new value and the shifts the new one cuts; a move to a branch
// with the same cutoff changes nothing. Nothing is written by asking.
func TestDayCutoffDB_AMoveSaysWhenItChangesTheCutoff(t *testing.T) {
	f := newCutoffFixture(t, nil)
	sliema := f.venue("Sliema", "10:00", "22:00", false)
	plant := f.venue("Plant", "09:00", "17:00", false)
	bakery := f.department(plant, "Pastry & Bakery", "04:00", "12:00")
	julians := f.venue("St Julians", "10:00", "22:00", false)
	f.exec(`UPDATE locations SET day_cutoff = '23:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, sliema)
	f.exec(`UPDATE locations SET day_cutoff = '23:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, julians)
	maria := f.person("Maria Borg", &sliema, nil)
	now := time.Now()

	m, err := f.cutoffs.Move(context.Background(), MoveCutoffCommand{TenantID: f.tenant, EmployeeID: maria, LocationID: plant, DepartmentID: &bakery, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Changes() || m.From != hm(23, 0) || m.To != hm(5, 0) || m.FromSource.Name != "Sliema" || m.ToSource.Level != CutoffBusiness {
		t.Fatalf("move to the bakery: %+v", m)
	}
	if len(m.Conflicts) != 1 || m.Conflicts[0].Shift.Owner != "Pastry & Bakery" {
		t.Fatalf("move to the bakery cuts %+v, want the bakery's 04:00-12:00", m.Conflicts)
	}
	m, err = f.cutoffs.Move(context.Background(), MoveCutoffCommand{TenantID: f.tenant, EmployeeID: maria, LocationID: julians, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if m.Changes() {
		t.Fatalf("a move between two 23:00 branches changes the cutoff: %+v", m)
	}
	if got := f.tapCutoff(maria); got != hm(23, 0) {
		t.Fatalf("asking about a move changed the tap's cutoff to %s", ClockText(&got))
	}
}

// TestDayCutoffDB_AnotherTenantsIdIsRefused: a review or a write naming another business's
// venue, department or person is that level's "unknown" refusal and writes nothing -- the
// subject is looked up inside the caller's tenant (RLS and the explicit predicate).
func TestDayCutoffDB_AnotherTenantsIdIsRefused(t *testing.T) {
	f := newCutoffFixture(t, nil)
	other := newCutoffFixture(t, nil)
	venue := other.venue("Elsewhere", "10:00", "22:00", false)
	dept := other.department(venue, "Kitchen", "04:00", "12:00")
	person := other.person("Someone", &venue, nil)
	for _, tc := range []struct {
		level CutoffLevel
		id    uuid.UUID
		want  error
	}{{CutoffBranch, venue, ErrUnknownVenue}, {CutoffDepartment, dept, ErrUnknownDepartment}, {CutoffPerson, person, ErrUnknownEmployee}} {
		if _, err := f.cutoffs.Review(context.Background(), CutoffReviewCommand{TenantID: f.tenant, Level: tc.level, SubjectID: tc.id, Value: dur(6, 0), Now: time.Now()}); !errors.Is(err, tc.want) {
			t.Errorf("review %s of another tenant: %v, want %v", tc.level, err, tc.want)
		}
		if _, err := f.set(tc.level, tc.id, dur(6, 0)); !errors.Is(err, tc.want) {
			t.Errorf("set %s of another tenant: %v, want %v", tc.level, err, tc.want)
		}
	}
	if got := other.tapCutoff(person); got != hm(5, 0) {
		t.Fatalf("the other tenant's person now reads %s", ClockText(&got))
	}
	var n int
	if err := other.data.WithTenant(context.Background(), other.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action LIKE '%.day_cutoff_updated'`, other.tenant).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("the other tenant's trail holds %d cutoff rows (%v)", n, err)
	}
}

// TestDayCutoffDB_TheBusinessLevelIsWrittenAndNeverEmptied: the business level writes its own
// trail row targeting the business, and an empty value is refused before any read (it is
// the end of every coalesce).
func TestDayCutoffDB_TheBusinessLevelIsWrittenAndNeverEmptied(t *testing.T) {
	f := newCutoffFixture(t, nil)
	if _, err := f.set(CutoffBusiness, uuid.Nil, nil); !errors.Is(err, ErrDayCutoffRequired) {
		t.Fatalf("an empty business cutoff: %v", err)
	}
	if _, err := f.set(CutoffBusiness, uuid.Nil, dur(23, 0)); err != nil {
		t.Fatal(err)
	}
	rows := f.trail(ActionTenantDayCutoffUpdated, f.tenant)
	if len(rows) != 1 || !strings.Contains(rows[0], `"from": "05:00"`) || !strings.Contains(rows[0], `"to": "23:00"`) {
		t.Fatalf("trail %v", rows)
	}
	r := f.review(CutoffBusiness, uuid.Nil, dur(23, 0), time.Now())
	if r.Changes() || r.Stored == nil || *r.Stored != hm(23, 0) || r.SubjectName != "Cutoff Test Ltd" {
		t.Fatalf("review after the write: %+v", r)
	}
}

// TestDayCutoffDB_TheSwitchIsTheDirectorysGate: the status line's two modes come from the
// directory's switch -- unset or in the future is "not active", in the past is "active since"
// that instant.
func TestDayCutoffDB_TheSwitchIsTheDirectorysGate(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	if s := newCutoffFixture(t, nil).cutoffs.Switch(now); s.Active {
		t.Fatalf("unset switch: %+v", s)
	}
	if s := newCutoffFixture(t, &future).cutoffs.Switch(now); s.Active {
		t.Fatalf("a switch an hour ahead: %+v", s)
	}
	on := fixtures.BusinessDaySwitchOn()
	if s := newCutoffFixture(t, on).cutoffs.Switch(now); !s.Active || !s.Since.Equal(*on) {
		t.Fatalf("a switch in the past: %+v, want active since %v", s, *on)
	}
}

// TestDayCutoffDB_TheSeedShiftsAreTheOnesThePureTestNames reads the seed's venues and KM's
// departments back (READ ONLY -- the shared tenants are never written here) and holds them
// equal to the table TestCutoffInBand_TheSeedShiftsAtTheCardsCutoffs measures.
func TestDayCutoffDB_TheSeedShiftsAreTheOnesThePureTestNames(t *testing.T) {
	f := newCutoffFixture(t, nil)
	read := func(tenantID uuid.UUID) map[string]CutoffShift {
		out := map[string]CutoffShift{}
		if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			q := store.New(tx)
			vs, err := q.ListDayCutoffVenues(ctx, store.ListDayCutoffVenuesParams{TenantID: tenantID, RowLimit: 100})
			if err != nil {
				return err
			}
			for _, v := range vs {
				if s := cutoffShiftOf(v.Name, v.ShiftStart, v.ShiftEnd, v.Overnight); s != nil {
					out[v.Name] = *s
				}
			}
			ds, err := q.ListDayCutoffDepartments(ctx, store.ListDayCutoffDepartmentsParams{TenantID: tenantID, RowLimit: 100})
			if err != nil {
				return err
			}
			for _, d := range ds {
				if s := cutoffShiftOf(d.Name, d.ShiftStart, d.ShiftEnd, d.Overnight); s != nil {
					out[d.Name] = *s
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	kf, km := read(fixtures.TenantKF), read(fixtures.TenantKM)
	if len(kf) == 0 || len(km) == 0 {
		t.Fatal("the seed's venues and departments read back empty; CI and `make seed` load them, and this test exists to compare them")
	}
	for _, s := range []CutoffShift{seedRustyBar, seedKFPaceville, seedKFHQ} {
		if kf[s.Owner] != s {
			t.Errorf("seed %s is %+v, the pure test says %+v", s.Owner, kf[s.Owner], s)
		}
	}
	if kf["KF St Julians"] != seedKF1022 {
		t.Errorf("seed KF St Julians is %+v", kf["KF St Julians"])
	}
	for _, s := range []CutoffShift{seedKMPlant, seedKMGeneral, seedKMMeat, seedKMWarehouse, seedKMKitchen, seedKMPastry} {
		if km[s.Owner] != s {
			t.Errorf("seed %s is %+v, the pure test says %+v", s.Owner, km[s.Owner], s)
		}
	}
}

// wallIn is t's wall clock in Malta, the fixture tenant's zone, as a cutoff.
func wallIn(t *testing.T, at time.Time) *time.Duration {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	l := at.In(loc)
	return dur(l.Hour(), l.Minute())
}

// TestDayCutoffDB_OnlyTheEntryTheEngineCouldCloseIsCounted is review ORTA-1's three rules,
// each on its own branch so one cannot stand in for another (cutoffs placed around the
// entries' own instants, so the case holds whatever the clock says):
//
//	(a) Ada's entry 10 h old whose OLD business day ended 5 h ago -- the old rule already
//	    leaves it open; the formula alone would count it.
//	(b) Ben's two open entries, 6 h and 2 h old: the engine can close only the later one
//	    (GetLastOpenTransaction), so one is counted, not two.
//	    (window) Cy's entry 20 h old: past StaleOpenIn the engine closes nothing; the formula
//	    alone would count it.
//	(c) with the switch off nothing is counted at all and the review says it did not count.
func TestDayCutoffDB_OnlyTheEntryTheEngineCouldCloseIsCounted(t *testing.T) {
	f := newCutoffFixture(t, fixtures.BusinessDaySwitchOn())
	now := time.Now().Truncate(time.Minute)
	setBranch := func(venue uuid.UUID, at time.Time) {
		f.exec(`UPDATE locations SET day_cutoff = $3::time WHERE tenant_id = $1 AND id = $2`, f.tenant, venue, ClockText(wallIn(t, at)))
	}

	a := f.venue("A", "", "", false)
	ada := f.person("Ada", &a, nil)
	f.tap(ada, a, now.Add(-10*time.Hour), "in")
	setBranch(a, now.Add(-5*time.Hour)) // B_old = now - 5 h
	if r := f.review(CutoffBranch, a, wallIn(t, now.Add(-8*time.Hour)), now); !r.OpenCounted || r.OpenSplit != 0 {
		t.Errorf("(a) an entry the old rule already leaves open: counted %v, split %d, want 0", r.OpenCounted, r.OpenSplit)
	}

	b := f.venue("B", "", "", false)
	ben := f.person("Ben", &b, nil)
	f.tap(ben, b, now.Add(-6*time.Hour), "in")
	f.tap(ben, b, now.Add(-2*time.Hour), "in")
	setBranch(b, now.Add(-12*time.Hour)) // B_old = now + 12 h for both
	if r := f.review(CutoffBranch, b, wallIn(t, now.Add(-time.Hour)), now); r.OpenSplit != 1 {
		t.Errorf("(b) two open entries of one person: split %d, want 1 -- the engine closes only the latest", r.OpenSplit)
	}

	c := f.venue("C", "", "", false)
	cy := f.person("Cy", &c, nil)
	f.tap(cy, c, now.Add(-20*time.Hour), "in")
	setBranch(c, now.Add(-21*time.Hour)) // B_old = now + 3 h
	if r := f.review(CutoffBranch, c, wallIn(t, now.Add(-17*time.Hour)), now); r.OpenSplit != 0 {
		t.Errorf("an entry older than StaleOpenIn: split %d, want 0", r.OpenSplit)
	}

	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := NewDirectory(f.data, nil)
	if err != nil {
		t.Fatal(err)
	}
	off, err := NewDayCutoffs(f.data, trail, dir, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	r, err := off.Review(context.Background(), CutoffReviewCommand{TenantID: f.tenant, Level: CutoffBranch, SubjectID: b, Value: wallIn(t, now.Add(-time.Hour)), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if r.OpenCounted || r.OpenSplit != 0 {
		t.Errorf("(c) the switch off: counted %v, split %d -- nothing is split while no cutoff applies", r.OpenCounted, r.OpenSplit)
	}
	if r, err := f.cutoffs.Review(context.Background(), CutoffReviewCommand{TenantID: f.tenant, Level: CutoffBranch, SubjectID: b, Unchanged: true, Now: now}); err != nil || r.OpenCounted {
		t.Errorf("an Unchanged review counted the open check-ins (%v)", err)
	}
}

// TestDayCutoffDB_ADepartmentsShiftIsCheckedAtItsMembersOwnCutoff is review ORTA-2's
// measurement: Bakery sits at Plant (06:00), its shift is 05:00-13:00, its one member's
// profile branch is Shop (no value) and the business is 23:00. The member's cutoff is 23:00
// (GetEmployeeForTap), outside the band, so neither the department's review nor the
// business's says "06:00 falls inside Bakery's shift" -- the estimate from the department's
// own venue is for a department with nobody in it.
func TestDayCutoffDB_ADepartmentsShiftIsCheckedAtItsMembersOwnCutoff(t *testing.T) {
	f := newCutoffFixture(t, nil)
	f.exec(`UPDATE tenants SET day_cutoff = '23:00' WHERE id = $1`, f.tenant)
	plant := f.venue("Plant", "", "", false)
	shop := f.venue("Shop", "", "", false)
	f.exec(`UPDATE locations SET day_cutoff = '06:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, plant)
	bakery := f.department(plant, "Bakery", "05:00", "13:00")
	baker := f.person("Baker", &shop, &bakery)
	if got := f.tapCutoff(baker); got != hm(23, 0) {
		t.Fatalf("the member's own cutoff is %s, the case needs 23:00", ClockText(&got))
	}
	now := time.Now()
	for _, level := range []CutoffLevel{CutoffDepartment, CutoffBusiness} {
		r, err := f.cutoffs.Review(context.Background(), CutoffReviewCommand{TenantID: f.tenant, Level: level, SubjectID: bakery, Unchanged: true, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Conflicts) != 0 {
			t.Errorf("%s review: %+v -- the member's cutoff (23:00) does not cut Bakery's 05-13", level, r.Conflicts)
		}
	}
	// The business level's own estimate has the same flaw for a department whose venue has
	// no value: Mill at Yard (no value) runs 05:00-13:00 and the business is 05:00, but its
	// one member's profile branch Store is 23:00 -- the member does not take the business's
	// value at all, so the business level cuts nobody's shift.
	g := newCutoffFixture(t, nil)
	yard := g.venue("Yard", "", "", false)
	store := g.venue("Store", "", "", false)
	g.exec(`UPDATE locations SET day_cutoff = '23:00' WHERE tenant_id = $1 AND id = $2`, g.tenant, store)
	mill := g.department(yard, "Mill", "05:00", "13:00")
	g.person("Miller", &store, &mill)
	if r := g.review(CutoffBusiness, uuid.Nil, dur(5, 0), now); len(r.Conflicts) != 0 {
		t.Errorf("the business level at 05:00: %+v -- Mill's only member takes Store's 23:00", r.Conflicts)
	}
	// With nobody in it, the department's own venue IS the estimate (the first-shift case).
	empty := f.department(plant, "Empty", "05:00", "13:00")
	r, err := f.cutoffs.Review(context.Background(), CutoffReviewCommand{TenantID: f.tenant, Level: CutoffDepartment, SubjectID: empty, Unchanged: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Conflicts) != 1 || r.Conflicts[0].Cutoff != hm(6, 0) {
		t.Fatalf("an empty department at Plant: %+v, want 06:00 inside its 05-13", r.Conflicts)
	}
}
