package tenant

// staffshift_db_test.go -- [G45-i]: the owner's switch and its audit row are one transaction; a
// manager, a disabled owner and another business's owner are refused and change nothing.

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestStaffShiftDB_OwnerSwitchesWithItsTrailRow(t *testing.T) {
	ctx := context.Background()
	f := newBrandFixture(t)
	accounts, err := NewAccounts(f.data, f.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	read := func(tenantID uuid.UUID) (on bool, rows int) {
		t.Helper()
		if err := f.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT staff_shift_visible FROM tenants WHERE id = $1`, tenantID).Scan(&on); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
				tenantID, ActionStaffShiftUpdated).Scan(&rows)
		}); err != nil {
			t.Fatal(err)
		}
		return on, rows
	}

	if on, rows := read(f.tenant); on || rows != 0 {
		t.Fatalf("a new business: on %v, rows %d; want off and none", on, rows)
	}
	if err := accounts.SetStaffShiftVisible(ctx, f.tenant, f.owner, true); err != nil {
		t.Fatal(err)
	}
	if on, rows := read(f.tenant); !on || rows != 1 {
		t.Fatalf("after the owner's press: on %v, rows %d", on, rows)
	}
	var detail string
	if err := f.data.WithTenant(ctx, f.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			f.tenant, ActionStaffShiftUpdated).Scan(&detail)
	}); err != nil {
		t.Fatal(err)
	}
	if canonicalJSON(t, detail) != canonicalJSON(t, `{"before":false,"after":true}`) {
		t.Fatalf("detail %s", detail)
	}
	for name, actor := range map[string]uuid.UUID{"manager": f.manager, "disabled owner": f.disabled, "foreign owner": f.foreignOwner} {
		if err := accounts.SetStaffShiftVisible(ctx, f.tenant, actor, false); !errors.Is(err, ErrStaffShiftNotPermitted) {
			t.Fatalf("%s: err %v, want ErrStaffShiftNotPermitted", name, err)
		}
	}
	if on, rows := read(f.tenant); !on || rows != 1 {
		t.Fatalf("after three refusals: on %v, rows %d; want unchanged", on, rows)
	}
	if on, _ := read(f.foreign); on {
		t.Fatal("the other business's switch moved")
	}
}
