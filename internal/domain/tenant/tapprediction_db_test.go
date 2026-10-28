package tenant

// tapprediction_db_test.go -- M15-12's directory half of the tap screen's predicted direction
// (ADR 0032 §B.2), against REAL Postgres: the engine's own function over the engine's own reads,
// only when the caller asked, only for an active person, only while the business-day switch is
// on at the SERVER's clock -- the directory's, never an instant a caller hands in (security lens
// F-2). Every case is a fresh tenant of its own (newFixture), with a business day far from now
// (fixtures.FarDayCutoff), so the seed's tenants are never written.

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/test/fixtures"
)

// predictionFixture is newFixture's tenant with the switch set to from and the person's own
// cutoff far from now; cutoff is that value.
type predictionFixture struct {
	*fixture
	cutoff time.Duration
}

func newPredictionFixture(t *testing.T, from *time.Time) *predictionFixture {
	t.Helper()
	f := newFixture(t)
	dir, err := NewDirectory(f.data, from)
	if err != nil {
		t.Fatal(err)
	}
	f.dir = dir
	pf := &predictionFixture{fixture: f}
	pf.cutoff = pf.farCutoff(t, f.employeeID)
	return pf
}

// farCutoff gives an employee of the fixture's tenant a cutoff far from now and returns it.
func (f *predictionFixture) farCutoff(t *testing.T, employeeID uuid.UUID) time.Duration {
	t.Helper()
	var c time.Duration
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		c, err = fixtures.GiveFarDayCutoff(ctx, tx, f.tenantID, employeeID, time.Now())
		return err
	}); err != nil {
		t.Fatalf("far cutoff: %v", err)
	}
	return c
}

// exec runs one statement in the fixture's tenant.
func (f *predictionFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// openIn writes a check-in of the person at the venue, at, and returns its id.
func (f *predictionFixture) openIn(t *testing.T, employeeID uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(t, `INSERT INTO transactions (id, tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice)
	           VALUES ($1, $2, $3, $4, $5, $5, 'ok', 'nfc', 'in', false)`, id, f.tenantID, employeeID, f.locationID, at)
	return id
}

// TestTapPageDB_ThePredictionIsTheEnginesOwn: with the switch on, an active person and the
// caller asking, TapPage's Prediction is exactly tap.PredictDirection at the directory's clock
// over the person's open check-in and resolved cutoff -- `in` with none, `out` with one an hour
// old -- and nothing is predicted when the caller did not ask, for a deactivated person, for
// another tenant's plaque or for a plaque on no wall.
func TestTapPageDB_ThePredictionIsTheEnginesOwn(t *testing.T) {
	f := newPredictionFixture(t, fixtures.BusinessDaySwitchOn())
	now := time.Now().UTC().Truncate(time.Microsecond)
	f.dir.clock = func() time.Time { return now }
	cut := &tap.DayCutoff{Cutoff: f.cutoff, Timezone: "Europe/Malta"}
	ctx := context.Background()

	facts, err := f.dir.TapPage(ctx, f.tenantID, f.employeeID, f.locationID, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := tap.PredictDirection(now, nil, cut); facts.Prediction == nil || !reflect.DeepEqual(*facts.Prediction, want) || want.Direction != tap.TypeIn || want.BusinessDayStart == nil {
		t.Fatalf("no open check-in: %+v, want %+v (in, with a business day)", facts.Prediction, want)
	}

	openAt := now.Add(-time.Hour)
	open := f.openIn(t, f.employeeID, openAt)
	facts, err = f.dir.TapPage(ctx, f.tenantID, f.employeeID, f.locationID, true)
	if err != nil {
		t.Fatal(err)
	}
	want := tap.PredictDirection(now, &tap.Transaction{ID: open, OccurredAt: openAt, Direction: tap.TypeIn}, cut)
	if facts.Prediction == nil || !reflect.DeepEqual(*facts.Prediction, want) || want.Direction != tap.TypeOut {
		t.Fatalf("an open check-in an hour old: %+v, want %+v (out)", facts.Prediction, want)
	}
	if facts.PredictionErr != nil || facts.EmployeeName != "Maria Borg" || facts.LocationName != "St Julians" {
		t.Fatalf("the page's other facts: %+v", facts)
	}

	if facts, err := f.dir.TapPage(ctx, f.tenantID, f.employeeID, f.locationID, false); err != nil || facts.Prediction != nil {
		t.Errorf("the caller did not ask: %+v, %v -- want no prediction", facts.Prediction, err)
	}

	gone := uuid.New()
	f.exec(t, `INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, activated_at, deactivated_at)
	           VALUES ($1, $2, $3, 'Joseph Camilleri', 'deactivated', now(), now(), now())`, gone, f.tenantID, f.locationID)
	f.farCutoff(t, gone)
	f.openIn(t, gone, openAt)
	if facts, err := f.dir.TapPage(ctx, f.tenantID, gone, f.locationID, true); err != nil || facts.Prediction != nil || facts.EmployeeName != "Joseph Camilleri" {
		t.Errorf("a deactivated person: %+v, %v -- want the page without a prediction", facts, err)
	}

	otherTenant, otherLocation := uuid.New(), uuid.New()
	f.seed(t, otherTenant, otherLocation, uuid.New(), "Somebody Else", "Elsewhere")
	if facts, err := f.dir.TapPage(ctx, f.tenantID, f.employeeID, otherLocation, true); err == nil || facts.Prediction != nil {
		t.Errorf("another tenant's plaque: %+v, %v -- want ErrForeignLocation and no prediction", facts.Prediction, err)
	}
	if facts, err := f.dir.TapPage(ctx, f.tenantID, f.employeeID, uuid.Nil, true); err == nil || facts.Prediction != nil {
		t.Errorf("a plaque on no wall: %+v, %v -- want ErrForeignLocation and no prediction", facts.Prediction, err)
	}
}

// TestTapPageDB_TheSwitchAndTheDayAreJudgedAtTheServersClock pins security lens F-2 for the
// screen (M15-04's DÜŞÜK turn): the switch is asked at the DIRECTORY's clock -- off with none,
// off a second before its instant, on at it -- and the open check-in is read at that same instant
// (an entry written after it is not open yet, so the prediction is `in`). TapPage and PeerTapPage
// take no instant at all, so no declared one can reach either question.
func TestTapPageDB_TheSwitchAndTheDayAreJudgedAtTheServersClock(t *testing.T) {
	ctx := context.Background()
	real := time.Now().UTC().Truncate(time.Microsecond)
	from := real.Add(-3 * time.Hour)

	off := newPredictionFixture(t, nil)
	off.dir.clock = func() time.Time { return real }
	if facts, err := off.dir.TapPage(ctx, off.tenantID, off.employeeID, off.locationID, true); err != nil || facts.Prediction != nil {
		t.Fatalf("switch unset: %+v, %v -- want no prediction", facts.Prediction, err)
	}

	f := newPredictionFixture(t, &from)
	f.openIn(t, f.employeeID, real.Add(-time.Hour))
	for _, c := range []struct {
		name  string
		clock time.Time
		want  *tap.Type
	}{
		{"a second before the switch", from.Add(-time.Second), nil},
		{"at the switch, before the check-in was written", from, ptr(tap.TypeIn)},
		{"now, the check-in open", real, ptr(tap.TypeOut)},
	} {
		f.dir.clock = func() time.Time { return c.clock }
		facts, err := f.dir.TapPage(ctx, f.tenantID, f.employeeID, f.locationID, true)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		switch {
		case c.want == nil && facts.Prediction != nil:
			t.Errorf("%s: %+v, want no prediction", c.name, *facts.Prediction)
		case c.want != nil && (facts.Prediction == nil || facts.Prediction.Direction != *c.want):
			t.Errorf("%s: %+v, want %s", c.name, facts.Prediction, *c.want)
		}
	}

	timeType := reflect.TypeOf(time.Time{})
	for name, fn := range map[string]any{"TapPage": (*Directory).TapPage, "PeerTapPage": (*Directory).PeerTapPage} {
		ft := reflect.TypeOf(fn)
		for i := 0; i < ft.NumIn(); i++ {
			if in := ft.In(i); in == timeType || in == reflect.PointerTo(timeType) {
				t.Errorf("%s takes a %v: an instant a caller chose could reach the switch (F-2)", name, in)
			}
		}
	}
}

func ptr[T any](v T) *T { return &v }

// TestTapPrediction_AsksTheOneSwitchAndNoOther is the card's single-gate criterion at the source
// (motor lens ORTA-2, N4): the prediction reads the switch ONLY through Directory.DayCutoff, at
// the directory's clock, and the directory's DayCutoff and the orchestrator's gather both call
// tap.SwitchedDayCutoff -- so a mutation that closes the gate in one place closes it for the page
// and the record together, and a second switch check here (its own read of the value, its own
// comparison with a clock) is red. The behaviour of the one gate on both surfaces is
// internal/handler's TestTapDB_ThePageAndTheRecordAskTheOneSwitch.
func TestTapPrediction_AsksTheOneSwitchAndNoOther(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	code := func(src string) string {
		var b strings.Builder
		for _, line := range strings.Split(src, "\n") {
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
				continue
			}
			b.WriteString(line + "\n")
		}
		return b.String()
	}
	pred := code(read("tapprediction.go"))
	for _, want := range []string{
		"now := d.clock()",
		"d.DayCutoff(now, ",
		"At: now,",
		"tap.PredictDirection(now, open, cutoff)",
		"GetEmployeeForTap(",
		"GetLastOpenTransaction(",
	} {
		if !strings.Contains(pred, want) {
			t.Errorf("tapprediction.go lacks %q", want)
		}
	}
	for _, never := range []string{"businessDayFrom", "BusinessDayFrom", "SwitchedDayCutoff", "time.Now", "ListOpenCheckIns", ".Before(", ".After("} {
		if strings.Contains(pred, never) {
			t.Errorf("tapprediction.go carries %q: a second switch, clock or definition of open beside the one gate", never)
		}
	}
	if n := strings.Count(pred, "d.DayCutoff("); n != 1 {
		t.Errorf("%d DayCutoff calls in the prediction, want 1", n)
	}
	dir := code(read("directory.go"))
	if !strings.Contains(dir, "return tap.SwitchedDayCutoff(d.businessDayFrom, now, cutoff, timezone)") {
		t.Error("Directory.DayCutoff is not tap.SwitchedDayCutoff over the directory's switch")
	}
	if !strings.Contains(dir, "clock: time.Now}") {
		t.Error("NewDirectory does not set the server's clock")
	}
	if !strings.Contains(code(read("../checkin/checkin.go")), "tap.SwitchedDayCutoff(s.businessDayFrom, now,") {
		t.Error("the orchestrator's gather does not ask tap.SwitchedDayCutoff at its server clock")
	}
}
