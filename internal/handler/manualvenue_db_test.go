package handler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/manual"
)

// manualvenue_db_test.go -- K-14 (M11 API-1) on the manager's record: an employee the
// integration manages may have NO home venue, and a record of their work is filed at the
// venue the manager chooses -- this business's venue only, and only for such a person.

// TestManualDB_AVenuelessPersonIsFiledAtTheChosenVenueOnly:
//   - the subject says NeedsVenue and lists this business's venues (and no other's);
//   - no venue chosen, or another business's venue, is manual.ErrVenue and writes NOTHING;
//   - this business's venue is the record's location_id;
//   - for a person WITH a home venue a posted venue is not read: the record is at home.
func TestManualDB_AVenuelessPersonIsFiledAtTheChosenVenueOnly(t *testing.T) {
	f := newManualFixture(t)
	ctx := context.Background()
	venueless := uuid.New()
	var second, foreignVenue uuid.UUID
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, full_name, status, external_ref)
		                          VALUES ($1, $2, 'API person', 'active', $3)`,
			venueless, f.tenantID, "API-"+strings.ToUpper(uuid.NewString()[:8])); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'Sliema') RETURNING id`, f.tenantID).Scan(&second)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.data.WithTenant(ctx, f.foreignTenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'Theirs') RETURNING id`, f.foreignTenant).Scan(&foreignVenue)
	}); err != nil {
		t.Fatal(err)
	}

	subject, err := f.recorder.Subject(ctx, f.tenantID, venueless)
	if err != nil {
		t.Fatalf("Subject: %v", err)
	}
	if !subject.NeedsVenue || subject.LocationName != "" {
		t.Errorf("subject NeedsVenue=%v venue %q, want true and none", subject.NeedsVenue, subject.LocationName)
	}
	if subject.VenueName(f.locationID) == "" || subject.VenueName(second) == "" || subject.VenueName(foreignVenue) != "" {
		t.Errorf("the picker offers %v; want this business's two venues and not another's", subject.Venues)
	}
	if home, err := f.recorder.Subject(ctx, f.tenantID, f.employeeID); err != nil || home.NeedsVenue || len(home.Venues) != 0 {
		t.Errorf("a person WITH a home venue: NeedsVenue=%v, %d venues offered (%v); want false and none", home.NeedsVenue, len(home.Venues), err)
	}

	count := func(employee uuid.UUID) int {
		t.Helper()
		var n int
		if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`, f.tenantID, employee).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	at := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	entry := manual.Entry{TenantID: f.tenantID, EmployeeID: venueless, EnteredBy: f.actorID, Direction: manual.In, At: at}
	for _, c := range []struct {
		what  string
		venue uuid.UUID
	}{{"no venue", uuid.Nil}, {"another business's venue", foreignVenue}, {"an invented venue", uuid.New()}} {
		e := entry
		e.LocationID = c.venue
		if _, err := f.recorder.Record(ctx, e); !errors.Is(err, manual.ErrVenue) {
			t.Errorf("%s answered %v, want manual.ErrVenue", c.what, err)
		}
	}
	if n := count(venueless); n != 0 {
		t.Fatalf("%d record(s) written by refused entries", n)
	}

	e := entry
	e.LocationID = second
	rec, err := f.recorder.Record(ctx, e)
	if err != nil {
		t.Fatalf("this business's venue: %v", err)
	}
	if r := f.read(t, f.tenantID, rec.ID); r.LocationID == nil || *r.LocationID != second || r.DeptID != nil || r.Channel != "manual" {
		t.Errorf("the record is at %v (department %v, channel %s), want the chosen venue %s", r.LocationID, r.DeptID, r.Channel, second)
	}

	home := f.entry(at, manual.Out)
	home.LocationID = second
	rec, err = f.recorder.Record(ctx, home)
	if err != nil {
		t.Fatalf("a person with a home venue: %v", err)
	}
	if r := f.read(t, f.tenantID, rec.ID); r.LocationID == nil || *r.LocationID != f.locationID {
		t.Errorf("a person WITH a home venue was filed at %v; the posted venue must not be read, want home %s", r.LocationID, f.locationID)
	}
}
