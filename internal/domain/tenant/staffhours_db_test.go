package tenant

// staffhours_db_test.go -- M16-30: [G30-a] the owner's My hours window and its audit row are one
// transaction; [G30-b] a manager, a disabled owner and another business's owner are refused and
// change nothing; [G30-c] a value outside 1, 2, 4, 13 is refused by the domain and by 00117's CHECK.

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestStaffHoursDB_OwnerSetsTheWindowWithItsTrailRow(t *testing.T) {
	ctx := context.Background()
	f := newBrandFixture(t)
	accounts, err := NewAccounts(f.data, f.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	read := func(tenantID uuid.UUID) (weeks *int16, rows int) {
		t.Helper()
		if err := f.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT staff_hours_weeks FROM tenants WHERE id = $1`, tenantID).Scan(&weeks); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
				tenantID, ActionStaffHoursUpdated).Scan(&rows)
		}); err != nil {
			t.Fatal(err)
		}
		return weeks, rows
	}
	if w, rows := read(f.tenant); w != nil || rows != 0 {
		t.Fatalf("a new business: weeks %v, rows %d; want off and none", w, rows)
	}
	four := 4
	if err := accounts.SetStaffHoursWeeks(ctx, f.tenant, f.owner, &four); err != nil {
		t.Fatal(err)
	}
	if w, rows := read(f.tenant); w == nil || *w != 4 || rows != 1 {
		t.Fatalf("after the owner's save: weeks %v, rows %d", w, rows)
	}
	if err := accounts.SetStaffHoursWeeks(ctx, f.tenant, f.owner, nil); err != nil {
		t.Fatal(err)
	}
	var detail string
	if err := f.data.WithTenant(ctx, f.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2 AND detail->'after' = 'null'::jsonb`,
			f.tenant, ActionStaffHoursUpdated).Scan(&detail)
	}); err != nil {
		t.Fatal(err)
	}
	if canonicalJSON(t, detail) != canonicalJSON(t, `{"before":4,"after":null}`) {
		t.Fatalf("detail %s", detail)
	}
	for name, actor := range map[string]uuid.UUID{"manager": f.manager, "disabled owner": f.disabled, "foreign owner": f.foreignOwner} {
		if err := accounts.SetStaffHoursWeeks(ctx, f.tenant, actor, &four); !errors.Is(err, ErrStaffHoursNotPermitted) {
			t.Fatalf("%s: err %v, want ErrStaffHoursNotPermitted", name, err)
		}
	}
	if w, rows := read(f.tenant); w != nil || rows != 2 {
		t.Fatalf("after three refusals: weeks %v, rows %d; want unchanged", w, rows)
	}
	if w, _ := read(f.foreign); w != nil {
		t.Fatal("the other business's setting moved")
	}
	for _, n := range []int{0, 3, 52} {
		if err := accounts.SetStaffHoursWeeks(ctx, f.tenant, f.owner, &n); !errors.Is(err, ErrStaffHoursOption) {
			t.Fatalf("%d weeks: err %v, want ErrStaffHoursOption", n, err)
		}
		err := f.data.WithTenant(ctx, f.tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE tenants SET staff_hours_weeks = $2 WHERE id = $1`, f.tenant, n)
			return err
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatalf("%d weeks past the domain: %v, want the CHECK's 23514", n, err)
		}
	}
}
