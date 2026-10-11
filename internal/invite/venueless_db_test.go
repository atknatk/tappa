package invite

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestActivationContext_AVenuelessEmployeeHasNoVenueAndNoWiFi is K-14 (M11 API-1) on the
// activation's read: an employee the integration manages may have no home venue, and the
// context then names none -- LocationID uuid.Nil, no venue name, no network -- instead of
// failing the read the wizard and the dev tap both depend on. The CONTROL is the same tenant's
// employee with a venue and a Wi-Fi name.
func TestActivationContext_AVenuelessEmployeeHasNoVenueAndNoWiFi(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusInvited, "KF-Staff")
	venueless := uuid.New()
	if err := d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, full_name, status, external_ref)
		                      VALUES ($1, $2, 'API person', 'invited', $3)`, venueless, f.tenantID, "API-"+venueless.String()[:8])
		return e
	}); err != nil {
		t.Fatal(err)
	}
	got, err := m.ActivationContext(context.Background(), f.tenantID, venueless)
	if err != nil {
		t.Fatalf("a venue-less employee's activation context failed: %v", err)
	}
	if got.LocationID != uuid.Nil || got.LocationName != "" || got.WiFiSSID != "" || got.FullName != "API person" {
		t.Errorf("context %+v; want no venue, no network and the person's name", got)
	}
	control, err := m.ActivationContext(context.Background(), f.tenantID, f.employeeID)
	if err != nil || control.LocationID != f.locationID || control.WiFiSSID != "KF-Staff" {
		t.Errorf("CONTROL: the employee with a venue reads %+v (%v), want the venue and its network", control, err)
	}
}
