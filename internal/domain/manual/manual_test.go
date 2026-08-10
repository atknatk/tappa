package manual

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
)

// manual_test.go -- the PURE half of M6-08: the vocabulary, the two time bounds and
// the refusals that happen before a database is ever reached.
//
// Everything about what lands in `transactions` is in manualentry_db_test.go
// (internal/handler), against real Postgres, because this package's whole point is
// that the narrowing lives in SQL rather than here.

// TestParseDirection_RefusesEverythingButTheTwo.
//
// 🔴 A DEFAULT WOULD BE THE DEFECT. Defaulting to `in` turns a mistyped form into an
// entry nothing ever closes; defaulting to `out` closes somebody's shift at a time
// nobody chose. Both silently change payroll, which is why the vocabulary is a named
// type with one constructor.
func TestParseDirection_RefusesEverythingButTheTwo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want Direction
		ok   bool
	}{
		{"in", In, true},
		{"out", Out, true},
		{"", "", false},
		{"IN", "", false},
		{" in", "", false},
		{"sideways", "", false},
		{"in;out", "", false},
	}
	for _, c := range cases {
		got, ok := ParseDirection(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("ParseDirection(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// fixedRecorder is a Recorder whose clock is nailed down, so the two bounds can be
// measured rather than slept through.
func fixedRecorder(t *testing.T, now time.Time) *Recorder {
	t.Helper()
	r, err := NewRecorder(stubDB{}, stubTrail{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	r.now = func() time.Time { return now }
	return r
}

// TestBounds_IsTheTABLEOfWhatAManagerMayCLAIM.
//
// 🔴 THE TWO BOUNDS PULL IN OPPOSITE DIRECTIONS AND BOTH ARE DELIBERATE. Backdating
// is the ORDINARY case here -- internal/policy's sys:occurred-at-bound exempts this
// path by name for exactly that reason -- so MaxBackdate is far looser than the tap's
// 72 h. The FUTURE bound is nearly absent for the opposite reason: a record of work
// not yet done is not a record, and the only slack is the gap between the manager's
// wall clock and the server's.
func TestBounds_IsTheTABLEOfWhatAManagerMayCLAIM(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	r := fixedRecorder(t, now)
	cases := []struct {
		name string
		at   time.Time
		want error
	}{
		{"right now", now, nil},
		{"a second ago", now.Add(-time.Second), nil},
		{"the shift that just ended", now.Add(-8 * time.Hour), nil},
		{"inside the future grace", now.Add(FutureGrace - time.Second), nil},
		{"exactly at the future grace", now.Add(FutureGrace), nil},
		{"one second past the future grace", now.Add(FutureGrace + time.Second), ErrFuture},
		{"tomorrow", now.Add(24 * time.Hour), ErrFuture},
		{"one second inside the backdate bound", now.Add(-MaxBackdate + time.Second), nil},
		{"exactly at the backdate bound", now.Add(-MaxBackdate), nil},
		{"one second past the backdate bound", now.Add(-MaxBackdate - time.Second), ErrTooOld},
		{"the year typed wrong", now.AddDate(-1, 0, 0), ErrTooOld},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := r.Bounds(c.at)
			if !errors.Is(got, c.want) {
				t.Fatalf("Bounds(%s) = %v, want %v", c.at.Sub(now), got, c.want)
			}
		})
	}
}

// TestBackdated_SeparatesReconstructionFromClosingTheShiftThatJustEnded.
//
// It is a PRESENTATION and TRAIL fact rather than a gate -- every value Bounds admits
// is writable either way -- and the threshold is FutureGrace's mirror so the two
// cannot drift apart.
func TestBackdated_SeparatesReconstructionFromClosingTheShiftThatJustEnded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	r := fixedRecorder(t, now)
	if r.Backdated(now) {
		t.Error("a record of this instant is reported as backdated")
	}
	if r.Backdated(now.Add(-FutureGrace)) {
		t.Error("a record inside the grace is reported as backdated")
	}
	if !r.Backdated(now.Add(-time.Hour)) {
		t.Error("a record an hour old is NOT reported as backdated, so the trail and the " +
			"confirmation screen would call a reconstruction a live checkout")
	}
}

// TestRecord_RefusesBeforeItReachesTheDatabase.
//
// 🔴 EVERY CASE HERE MUST NOT TOUCH THE DATABASE, AND THAT IS ASSERTED RATHER THAN
// ASSUMED: stubDB fails the test if it is ever entered. The point is section 4.6's
// second half -- a refusal leaves NOTHING behind -- proved at the earliest layer that
// can prove it.
func TestRecord_RefusesBeforeItReachesTheDatabase(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	good := Entry{
		TenantID: uuid.New(), EmployeeID: uuid.New(), EnteredBy: uuid.New(),
		Direction: Out, At: now.Add(-2 * time.Hour),
	}
	cases := []struct {
		name string
		mut  func(e *Entry)
		want error
	}{
		{"no tenant", func(e *Entry) { e.TenantID = uuid.Nil }, nil},
		{"no employee", func(e *Entry) { e.EmployeeID = uuid.Nil }, ErrUnknownEmployee},
		{"no administrator", func(e *Entry) { e.EnteredBy = uuid.Nil }, nil},
		{"no direction", func(e *Entry) { e.Direction = "" }, ErrDirection},
		{"a direction nobody offered", func(e *Entry) { e.Direction = "sideways" }, ErrDirection},
		{"ahead of the clock", func(e *Entry) { e.At = now.Add(time.Hour) }, ErrFuture},
		{"past the backdate bound", func(e *Entry) { e.At = now.Add(-MaxBackdate - time.Hour) }, ErrTooOld},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := NewRecorder(stubDB{t: t}, stubTrail{}, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatalf("NewRecorder: %v", err)
			}
			r.now = func() time.Time { return now }
			e := good
			c.mut(&e)
			_, gotErr := r.Record(context.Background(), e)
			if gotErr == nil {
				t.Fatalf("the entry was accepted; every case here is a refusal")
			}
			if c.want != nil && !errors.Is(gotErr, c.want) {
				t.Fatalf("error = %v, want %v -- each refusal is a different sentence to a "+
					"manager (section 4.6)", gotErr, c.want)
			}
		})
	}
}

// TestNewRecorder_RefusesToBeBuiltWithoutATrail.
//
// 🔴 A RECORDER THAT CANNOT WRITE THE TRAIL WOULD APPEND ATTENDANCE RECORDS WITH NO
// AUDIT ROW, which is the state the card's fourth criterion forbids. The M5-04 lesson
// is that a capability can be delivered, tested and dead in the wired product because
// two halves were never assembled, so the failure has to be impossible to CONSTRUCT.
func TestNewRecorder_RefusesToBeBuiltWithoutATrail(t *testing.T) {
	t.Parallel()
	if _, err := NewRecorder(nil, stubTrail{}, nil); err == nil {
		t.Error("a Recorder was built with no database")
	}
	if _, err := NewRecorder(stubDB{}, nil, nil); err == nil {
		t.Error("a Recorder was built with no audit trail, so it could write records that " +
			"nobody can be shown to have entered")
	}
	if _, err := NewRecorder(stubDB{}, stubTrail{}, nil); err != nil {
		t.Errorf("a fully wired Recorder was refused: %v", err)
	}
}

// stubDB fails the test if anything reaches it. It is not a fake database -- there is
// no such thing worth having here -- it is an assertion that a refusal never got as
// far as opening a transaction.
type stubDB struct{ t *testing.T }

func (s stubDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	if s.t != nil {
		s.t.Error("a refused entry opened a database transaction. Section 4.6's question " +
			"about a refusal is what is LEFT BEHIND, and the cheapest answer is not to start.")
	}
	return errors.New("manual: stubDB must not be reached")
}

type stubTrail struct{}

func (stubTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, errors.New("manual: stubTrail must not be reached")
}
