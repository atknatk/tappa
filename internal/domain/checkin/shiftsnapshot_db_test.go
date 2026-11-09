package checkin

// shiftsnapshot_db_test.go — T152 (migration 00095, ADR 0042) on the real tap path: the shift the
// engine judged a directed tap against is frozen on its row — the department's when it has one,
// otherwise the TAPPED venue's (§5, Q17), with the zone it was read in — and the engine's own
// Decision.MinutesLate is what that snapshot reproduces.
//
// The clock is the service's (recordAt), on fixed wall clocks of the business-day test week, so no
// test depends on the hour it runs at. Each test builds its own tenant(s) and shifts.

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/domain/tap"
)

// shiftSnapshot is a row's transactions.shift_* columns.
type shiftSnapshot struct {
	source     *string
	start, end pgtype.Time
	overnight  *bool
	zone       *string
	occurredAt time.Time
}

func (w *holdingWorld) shiftSnapshot(id uuid.UUID) shiftSnapshot {
	w.t.Helper()
	var s shiftSnapshot
	if err := w.owner.QueryRow(w.ctx, `SELECT shift_source, shift_start, shift_end, shift_overnight, shift_timezone, occurred_at
	                                    FROM transactions WHERE id = $1`, id).Scan(&s.source, &s.start, &s.end, &s.overnight, &s.zone, &s.occurredAt); err != nil {
		w.t.Fatalf("read the shift snapshot of %s: %v", id, err)
	}
	return s
}

// none says the row recorded nothing (every column NULL).
func (s shiftSnapshot) none() bool {
	return s.source == nil && !s.start.Valid && !s.end.Valid && s.overnight == nil && s.zone == nil
}

// shift is the snapshot as the engine's shape, or nil when it is not a full department/location one.
func (s shiftSnapshot) shift() *tap.Shift {
	if s.source == nil || !s.start.Valid || !s.end.Valid || s.overnight == nil || s.zone == nil {
		return nil
	}
	return &tap.Shift{
		Start: time.Duration(s.start.Microseconds) * time.Microsecond, End: time.Duration(s.end.Microseconds) * time.Microsecond,
		Overnight: *s.overnight, Timezone: *s.zone,
	}
}

// wantSnapshot fails unless the row recorded source with start–end (overnight) in zone, and — for a
// check-in — unless the snapshot reproduces the engine's own lateness.
func wantSnapshot(t *testing.T, w *holdingWorld, res Result, source tap.ShiftSource, start, end time.Duration, overnight bool, zone string) {
	t.Helper()
	s := w.shiftSnapshot(res.TransactionID)
	sh := s.shift()
	if s.source == nil || *s.source != string(source) || sh == nil || sh.Start != start || sh.End != end || sh.Overnight != overnight || sh.Timezone != zone {
		t.Fatalf("snapshot %s %+v, want %s %v-%v overnight=%v %s", str(s.source), sh, source, start, end, overnight, zone)
	}
	if res.Decision.Type != nil && *res.Decision.Type == tap.TypeIn {
		got, engine := tap.MinutesLate(s.occurredAt, *sh), res.Decision.MinutesLate
		if got == nil || engine == nil || *got != *engine {
			t.Errorf("the snapshot measures %v minutes late, the engine said %v -- the record must reproduce Decision.MinutesLate", deref(got), deref(engine))
		}
	}
}

func deref(p *int) any {
	if p == nil {
		return "nil"
	}
	return *p
}

// zoneWall is a wall clock of the business-day test week in zone.
func zoneWall(t *testing.T, zone string, day, hh, mm int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, 10, day, hh, mm, 0, 0, loc)
}

// TestRecordDB_ATapFreezesTheTappedVenuesShiftAndItsZone: a person with no department checks in at
// a venue whose shift is 09:00–17:00, at 09:12 in a tenant whose zone is Europe/Lisbon (not the
// seed's Malta, so a hard-coded zone would be red): the row records 'location', 09:00–17:00, not
// overnight, Europe/Lisbon, and the snapshot measures the 12 minutes the engine measured.
func TestRecordDB_ATapFreezesTheTappedVenuesShiftAndItsZone(t *testing.T) {
	w := newHoldingWorld(t)
	const zone = "Europe/Lisbon"
	biz := w.tenant("T152 Lisbon")
	w.exec(`UPDATE tenants SET timezone = $2 WHERE id = $1`, biz, zone)
	venue, tag := w.branch(biz, "", "09:00", "17:00", false)
	emp := w.employeeOnTheHierarchy(biz, &venue)

	res := recordAt(t, w.svc, w.ctx, zoneWall(t, zone, tuesday, 9, 12), w.nfc(biz, emp, tag, biz, 101, w.kfOnNet))
	if direction(res) != "in" || res.Decision.MinutesLate == nil || *res.Decision.MinutesLate != 12 {
		t.Fatalf("PREMISE: a 09:12 check-in 12 minutes late, got %s late %v (%s)", direction(res), deref(res.Decision.MinutesLate), res.Decision.MatchedSid)
	}
	wantSnapshot(t, w, res, tap.ShiftSourceLocation, 9*time.Hour, 17*time.Hour, false, zone)
}

// TestRecordDB_TheDepartmentsShiftBeatsTheVenuesOnTheRecord is §5's "department shift if any": the
// same venue (09:00–17:00), a person in a department whose shift is 07:00–15:00, checking in at
// 07:20 — the row records 'department' 07:00–15:00 and 20 minutes, never the venue's -40.
func TestRecordDB_TheDepartmentsShiftBeatsTheVenuesOnTheRecord(t *testing.T) {
	w := newHoldingWorld(t)
	venue, tag := w.branch(w.kf, "", "09:00", "17:00", false)
	emp := w.employeeOnTheHierarchy(w.kf, &venue)
	var dept uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO departments (tenant_id, location_id, name, shift_start, shift_end)
	                                   VALUES ($1, $2, 'T152 kitchen', '07:00', '15:00') RETURNING id`, w.kf, venue).Scan(&dept); err != nil {
		t.Fatal(err)
	}
	w.exec(`UPDATE employees SET department_id = $3 WHERE tenant_id = $1 AND id = $2`, w.kf, emp, dept)

	res := recordAt(t, w.svc, w.ctx, maltaWall(t, tuesday, 7, 20), w.nfc(w.kf, emp, tag, w.kf, 101, w.kfOnNet))
	if direction(res) != "in" {
		t.Fatalf("PREMISE: 07:20 = %s, want in", direction(res))
	}
	wantSnapshot(t, w, res, tap.ShiftSourceDepartment, 7*time.Hour, 15*time.Hour, false, "Europe/Malta")
	if got := *res.Decision.MinutesLate; got != 20 {
		t.Errorf("lateness %d, want 20 against the department's 07:00", got)
	}
}

// TestRecordDB_TheRustyBarNightIsFrozenOnBothEnds: the bar's 18:00–02:00 overnight shift is recorded
// on the 18:05 check-in (5 minutes late) and on the 02:10 checkout that closes the night — both
// directed, both 'location' 18:00–02:00 overnight. The check-in is the owner's seed for the person
// who taps out (so no debounce wait), and a second person's live 18:05 tap for the in-shape.
func TestRecordDB_TheRustyBarNightIsFrozenOnBothEnds(t *testing.T) {
	w := newHoldingWorld(t)
	bar, barTag := w.branch(w.kf, "", "18:00", "02:00", true)

	in := recordAt(t, w.svc, w.ctx, maltaWall(t, monday, 18, 5), w.nfc(w.kf, w.employeeOnTheHierarchy(w.kf, &bar), barTag, w.kf, 101, w.kfOnNet))
	if direction(in) != "in" || in.Decision.MinutesLate == nil || *in.Decision.MinutesLate != 5 {
		t.Fatalf("PREMISE: 18:05 = %s late %v, want in, 5", direction(in), deref(in.Decision.MinutesLate))
	}
	wantSnapshot(t, w, in, tap.ShiftSourceLocation, 18*time.Hour, 2*time.Hour, true, "Europe/Malta")

	closer := w.employeeOnTheHierarchy(w.kf, &bar)
	w.seedIn(w.kf, closer, bar, barTag, maltaWall(t, monday, 18, 0))
	out := recordAt(t, w.svc, w.ctx, maltaWall(t, tuesday, 2, 10), w.nfc(w.kf, closer, barTag, w.kf, 102, w.kfOnNet))
	if direction(out) != "out" {
		t.Fatalf("PREMISE: 02:10 = %s (%s), want the night's out", direction(out), out.Decision.MatchedSid)
	}
	wantSnapshot(t, w, out, tap.ShiftSourceLocation, 18*time.Hour, 2*time.Hour, true, "Europe/Malta")
}

// TestRecordDB_ThePeersVenueShiftIsRecordedOnTheEmployersRow is the holding case (ADR 0027 §5): KM's
// person taps KF's plaque — the record is KM's, the shift is the TAPPED venue's (KF St Julians,
// 09:00–17:00), source 'location'.
func TestRecordDB_ThePeersVenueShiftIsRecordedOnTheEmployersRow(t *testing.T) {
	w := newHoldingWorld(t)
	emp := w.employeeOnTheHierarchy(w.km, nil)
	res := recordAt(t, w.svc, w.ctx, maltaWall(t, tuesday, 9, 30), w.nfc(w.km, emp, w.kfTag, w.kf, 101, w.kfOnNet))
	if direction(res) != "in" {
		t.Fatalf("PREMISE: KM's person on KF's plaque = %s (%s), want in", direction(res), res.Decision.Verdict)
	}
	if r := w.record(res.TransactionID); r.tenant != w.km || r.siteVenue == nil || *r.siteVenue != w.kfVenue {
		t.Fatalf("PREMISE: the row is KM's at KF's venue, got tenant %s site venue %v", r.tenant, r.siteVenue)
	}
	wantSnapshot(t, w, res, tap.ShiftSourceLocation, 9*time.Hour, 17*time.Hour, false, "Europe/Malta")
}

// TestRecordDB_OnlyADirectedTapRecordsAShift is the NULL rule in the row (the unit half is
// TestInsertParams_RecordsTheShiftOnlyOnADirectedTap): a venue with NO shift records 'none' (judged
// against nothing, and the report will not re-measure it later); a reject (a lost plaque) and a
// manager's manual record through Record record nothing.
func TestRecordDB_OnlyADirectedTapRecordsAShift(t *testing.T) {
	w := newHoldingWorld(t)
	tuesday0900 := maltaWall(t, tuesday, 9, 0)

	var bare uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name, static_ips) VALUES ($1, 'T152 no shift', '{203.0.113.0/24}') RETURNING id`,
		w.kf).Scan(&bare); err != nil {
		t.Fatal(err)
	}
	bareTag := w.tag(w.kf, &bare, "active", 100)
	free := recordAt(t, w.svc, w.ctx, tuesday0900, w.nfc(w.kf, w.employeeOnTheHierarchy(w.kf, &bare), bareTag, w.kf, 101, w.kfOnNet))
	if direction(free) != "in" || free.Decision.MinutesLate != nil {
		t.Fatalf("PREMISE: a check-in at a venue with no shift, lateness not computed; got %s %v", direction(free), deref(free.Decision.MinutesLate))
	}
	if s := w.shiftSnapshot(free.TransactionID); s.source == nil || *s.source != string(tap.ShiftSourceNone) || s.start.Valid || s.zone != nil {
		t.Errorf("a tap judged against no shift recorded %s %+v, want 'none' and nothing else", str(s.source), s)
	}

	lost := recordAt(t, w.svc, w.ctx, tuesday0900, w.nfc(w.kf, w.employeeOnTheHierarchy(w.kf, nil), w.kfLost, w.kf, 101, w.kfOnNet))
	if lost.Decision.Verdict != tap.VerdictReject || lost.TransactionID == uuid.Nil {
		t.Fatalf("PREMISE: a lost plaque is a recorded reject, got %s", lost.Decision.Verdict)
	}
	if s := w.shiftSnapshot(lost.TransactionID); !s.none() {
		t.Errorf("a reject recorded a shift: %s %+v", str(s.source), s)
	}

	author := uuid.New()
	manual := w.nfc(w.kf, w.employeeOnTheHierarchy(w.kf, &w.kfVenue), w.kfTag, w.kf, 0, w.kfOnNet)
	manual.Channel, manual.EnteredBy, manual.CMACVerified = tap.ChannelManual, &author, false
	m := recordAt(t, w.svc, w.ctx, tuesday0900, manual)
	if m.Decision.Type == nil {
		t.Fatalf("PREMISE: the manual record is directed, got %s", direction(m))
	}
	if s := w.shiftSnapshot(m.TransactionID); !s.none() {
		t.Errorf("a manual record recorded a shift: %s %+v", str(s.source), s)
	}
}
