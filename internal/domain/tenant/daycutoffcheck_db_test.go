package tenant

// daycutoffcheck_db_test.go -- M15-08's business day check against REAL Postgres: the check of a
// business of the test's own, and RLS under the statements the check runs.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// TestDayCutoffDB_TheCheckReadsEveryLevelOfItsOwnBusinessAlone: a business of the test's own
// with a cutting value at the business level (a bakery's member at 05:00) and at a venue (01:00,
// whose person tapped at Rusty Bar yesterday) is reported level by level; another business --
// with a shift its own 05:00 cuts -- is not in the first's check, and the first's not in its.
//
// And RLS under the check's statements (CLAUDE.md §6: an isolation test carries NO filter that
// would answer for RLS): in the first business's context, each read the check runs is asked for
// the OTHER business's id -- the explicit predicate then matches the other's rows, so the zero
// rows are RLS's.
func TestDayCutoffDB_TheCheckReadsEveryLevelOfItsOwnBusinessAlone(t *testing.T) {
	f := newCutoffFixture(t, nil)
	other := newCutoffFixture(t, nil)

	plant := f.venue("Plant", "09:00", "17:00", false)
	bakery := f.department(plant, "Bakery", "04:00", "12:00")
	f.person("Rita Zammit", &plant, &bakery)
	gzira := f.venue("Gzira", "", "", false)
	f.exec(`UPDATE locations SET day_cutoff = '01:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, gzira)
	rusty := f.venue("Rusty Bar", "18:00", "02:00", true)
	maria := f.person("Maria Borg", &gzira, nil)
	f.tap(maria, rusty, time.Now().Add(-24*time.Hour), "in")

	ov := other.venue("Elsewhere", "09:00", "17:00", false)
	od := other.department(ov, "Night Bakery", "04:30", "12:30")
	other.person("Someone Else", &ov, &od)

	got, err := f.cutoffs.Check(context.Background(), f.tenant, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.CutShifts != 2 || len(got.Levels) != 2 {
		t.Fatalf("the check = %+v, want two levels cutting two shifts", got)
	}
	if b := got.Levels[0]; b.Level != CutoffBusiness || b.SubjectID != f.tenant || b.People != 1 ||
		len(b.Conflicts) != 1 || b.Conflicts[0].Shift.Owner != "Bakery" || b.Suggested == nil || CutoffInBand(*b.Suggested, b.Conflicts[0].Shift) {
		t.Errorf("the business level = %+v, want Bakery cut at 05:00 and a suggestion outside its band", b)
	}
	if v := got.Levels[1]; v.Level != CutoffBranch || v.SubjectID != gzira || v.SubjectName != "Gzira" || v.Value != hm(1, 0) ||
		len(v.Conflicts) != 1 || v.Conflicts[0].Shift.Owner != "Rusty Bar" {
		t.Errorf("the Gzira level = %+v, want Rusty Bar's night cut at 01:00 (the venue Maria tapped at)", v)
	}
	for _, l := range got.Levels {
		for _, c := range l.Conflicts {
			if c.Shift.Owner == "Night Bakery" {
				t.Errorf("the check of one business names another's shift: %+v", l)
			}
		}
	}
	theirs, err := other.cutoffs.Check(context.Background(), other.tenant, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if theirs.CutShifts != 1 || len(theirs.Levels) != 1 || theirs.Levels[0].Conflicts[0].Shift.Owner != "Night Bakery" {
		t.Errorf("the other business's check = %+v, want its own Night Bakery alone", theirs)
	}

	if err := f.data.WithTenant(context.Background(), f.tenant, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, err := q.GetDayCutoffBusiness(ctx, other.tenant); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("the other business's row read in this one's context: %v", err)
		}
		vs, err := q.ListDayCutoffVenues(ctx, store.ListDayCutoffVenuesParams{TenantID: other.tenant, RowLimit: 100})
		if err != nil {
			return err
		}
		ds, err := q.ListDayCutoffDepartments(ctx, store.ListDayCutoffDepartmentsParams{TenantID: other.tenant, RowLimit: 100})
		if err != nil {
			return err
		}
		ps, err := q.ListDayCutoffPeople(ctx, store.ListDayCutoffPeopleParams{TenantID: other.tenant, RowLimit: 100})
		if err != nil {
			return err
		}
		if len(vs)+len(ds)+len(ps) != 0 {
			t.Errorf("in this business's context the other's venues %d, departments %d, people %d were read", len(vs), len(ds), len(ps))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The other business's rows ARE there for the same statements in its own context, so the
	// zeroes above are not an empty fixture.
	if err := other.data.WithTenant(context.Background(), other.tenant, func(ctx context.Context, tx pgx.Tx) error {
		ds, err := store.New(tx).ListDayCutoffDepartments(ctx, store.ListDayCutoffDepartmentsParams{TenantID: other.tenant, RowLimit: 100})
		if err == nil && len(ds) != 1 {
			t.Errorf("the other business reads %d departments of its own, want 1", len(ds))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
