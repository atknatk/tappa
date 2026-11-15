package tenant

// apphome_db_test.go -- Q41's "Doğrula" (M16-07): the app's home screen (GET /app/v1/me) predicts
// the direction WITHOUT a plaque, and the measurement is that it answers EXACTLY what GET /t's
// TapPage answers for the same person at the same server instant whenever the page's plaque half
// of the gate is open (predict = true: an active plaque whose CMAC verified, or a QR) -- in every
// state the gate distinguishes: no open check-in, an open one, an open one past the business day's
// end, the switch off, the switch not yet on, a deactivated person, and the person's own cutoff
// (ADR 0032 K1: the profile's business day, not the tapped venue's -- so a plaque at another venue
// of the same business changes nothing). Real Postgres; every case a fresh tenant.

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/test/fixtures"
)

// samePrediction reports whether two predictions are the same answer (both absent, or equal).
func samePrediction(a, b *tap.Prediction) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.DeepEqual(*a, *b)
}

func TestAppHomeDB_ThePredictionWithoutAPlaqueIsTheTapPagesOwn(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	// compare asks both reads at one instant and holds them equal; it returns the prediction.
	compare := func(t *testing.T, f *predictionFixture, employee, venue uuid.UUID, at time.Time) *tap.Prediction {
		t.Helper()
		f.dir.clock = func() time.Time { return at }
		page, err := f.dir.TapPage(ctx, f.tenantID, employee, venue, true)
		if err != nil {
			t.Fatalf("TapPage: %v", err)
		}
		home, err := f.dir.AppHome(ctx, f.tenantID, employee)
		if err != nil {
			t.Fatalf("AppHome: %v", err)
		}
		if !samePrediction(page.Prediction, home.Prediction) {
			t.Fatalf("at %s: TapPage predicts %+v, AppHome %+v -- the plaqueless prediction is not the page's", at, page.Prediction, home.Prediction)
		}
		if page.EmployeeName != home.EmployeeName || !reflect.DeepEqual(page.Brand, home.Brand) {
			t.Fatalf("the name or the brand differ: page %q %+v, home %q %+v", page.EmployeeName, page.Brand, home.EmployeeName, home.Brand)
		}
		return home.Prediction
	}

	t.Run("switch on: in, then out, then a second venue of the business", func(t *testing.T) {
		f := newPredictionFixture(t, fixtures.BusinessDaySwitchOn())
		if p := compare(t, f, f.employeeID, f.locationID, now); p == nil || p.Direction != tap.TypeIn || p.BusinessDayStart == nil {
			t.Fatalf("no open check-in: %+v, want in with a business day", p)
		}
		f.openIn(t, f.employeeID, now.Add(-time.Hour))
		if p := compare(t, f, f.employeeID, f.locationID, now); p == nil || p.Direction != tap.TypeOut {
			t.Fatalf("an open check-in: %+v, want out", p)
		}
		second := uuid.New()
		f.exec(t, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Sliema')`, second, f.tenantID)
		if p := compare(t, f, f.employeeID, second, now); p == nil || p.Direction != tap.TypeOut {
			t.Fatalf("the business's other venue: %+v, want the same out (K1: the person's day)", p)
		}
	})

	t.Run("an open check-in from before the business day", func(t *testing.T) {
		f := newPredictionFixture(t, fixtures.BusinessDaySwitchOn())
		f.openIn(t, f.employeeID, now.Add(-30*time.Hour))
		compare(t, f, f.employeeID, f.locationID, now)
	})

	t.Run("switch off", func(t *testing.T) {
		f := newPredictionFixture(t, nil)
		if p := compare(t, f, f.employeeID, f.locationID, now); p != nil {
			t.Fatalf("switch unset: %+v, want none", p)
		}
	})

	t.Run("switch not yet on", func(t *testing.T) {
		from := now.Add(time.Hour)
		f := newPredictionFixture(t, &from)
		if p := compare(t, f, f.employeeID, f.locationID, now); p != nil {
			t.Fatalf("before the switch: %+v, want none", p)
		}
		if p := compare(t, f, f.employeeID, f.locationID, from); p == nil {
			t.Fatal("at the switch: want a prediction")
		}
	})

	t.Run("a deactivated person", func(t *testing.T) {
		f := newPredictionFixture(t, fixtures.BusinessDaySwitchOn())
		gone := uuid.New()
		f.exec(t, `INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, activated_at, deactivated_at)
		           VALUES ($1, $2, $3, 'Joseph Camilleri', 'deactivated', now(), now(), now())`, gone, f.tenantID, f.locationID)
		f.farCutoff(t, gone)
		if p := compare(t, f, gone, f.locationID, now); p != nil {
			t.Fatalf("a deactivated person: %+v, want none", p)
		}
	})
}

// TestAppHomeDB_RefusesAMissingPerson: the request's tenant and person are required (they come
// from a verified signature; a zero one is a caller bug), and a person of another tenant is not
// found under this one (§4.5).
func TestAppHomeDB_RefusesAMissingPerson(t *testing.T) {
	f := newPredictionFixture(t, fixtures.BusinessDaySwitchOn())
	if _, err := f.dir.AppHome(context.Background(), uuid.Nil, f.employeeID); err == nil {
		t.Fatal("a nil tenant was answered")
	}
	if _, err := f.dir.AppHome(context.Background(), uuid.New(), f.employeeID); err == nil {
		t.Fatal("the person was found under a tenant they are not in")
	}
}
