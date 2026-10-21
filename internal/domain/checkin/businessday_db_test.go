package checkin

// businessday_db_test.go — M15-04's acceptance on the real tap path (ADR 0032 §A.2, §A.5, §A.8):
// the orchestrator gathers the person's resolved cutoff, hands it to the engine only while the
// business-day switch is on (by the SERVER's clock), and freezes the business day on a directed
// nfc/qr row and on nothing else.
//
// EVERY TEST HERE BUILDS ITS OWN TENANTS (newHoldingWorld) and writes their cutoffs — the
// business, a branch — on purpose: these tests exercise the cutoff and the hierarchy, so they
// must not lean on the seed tenant's 05:00 nor move it (fixtures.GiveFarDayCutoff's rule). Their
// employees have NO person-tier cutoff (employeeOnTheHierarchy), so the value comes from the tier
// under test.
//
// THE CLOCK IS THE SERVICE'S (svc.now): the taps happen on fixed wall clocks of a fixed week
// (Mon 2026-10-19, Tue 2026-10-20, Malta CEST, before the 2026-10-25 change), so no test depends
// on the hour it runs at. Predecessors are written as tappa_app with occurred_at = created_at at
// those instants — the database's clock cannot be moved through Record.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/test/fixtures"
)

// maltaWall is a wall clock of the test week in Europe/Malta.
func maltaWall(t *testing.T, day, hh, mm int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2026, 10, day, hh, mm, 0, 0, loc)
}

// Monday and Tuesday of the test week.
const (
	monday  = 19
	tuesday = 20
)

// serviceWith is a second Service over the world's database with the given switch and debounce
// window — the world's own runs the switch on and the shipped 60 s.
func (w *holdingWorld) serviceWith(from *time.Time, debounce time.Duration) *Service {
	w.t.Helper()
	trail, err := audit.New(w.data)
	if err != nil {
		w.t.Fatal(err)
	}
	svc, err := New(w.data, trail, &config.Config{GPSRadiusMeters: 150, Debounce: debounce, Freshness: 180 * time.Second,
		BusinessDayFrom: from}, quietLogger())
	if err != nil {
		w.t.Fatal(err)
	}
	return svc
}

// employeeOnTheHierarchy is an active employee with NO cutoff of their own (the world's employee
// helper gives one far from now), on the given profile branch (nil: none — the API-managed shape,
// K-14). Their business day is the branch's or the business's.
func (w *holdingWorld) employeeOnTheHierarchy(tenant uuid.UUID, profile *uuid.UUID) uuid.UUID {
	w.t.Helper()
	id := w.employee(tenant, "active")
	w.exec(`UPDATE employees SET day_cutoff = NULL, location_id = $3 WHERE tenant_id = $1 AND id = $2`, tenant, id, profile)
	return id
}

// branch is a location of tenant with its own cutoff (NULL: inherit) and shift, and an active
// plaque on its wall.
func (w *holdingWorld) branch(tenant uuid.UUID, cutoff, shiftStart, shiftEnd string, overnight bool) (uuid.UUID, string) {
	w.t.Helper()
	var id uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name, static_ips, shift_start, shift_end, overnight, day_cutoff)
	                                   VALUES ($1, $2, '{203.0.113.0/24}', $3::time, $4::time, $5, $6::time) RETURNING id`,
		tenant, "branch "+uuid.NewString()[:8], shiftStart, shiftEnd, overnight, nullable(cutoff)).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id, w.tag(tenant, &id, "active", 100)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// seedIn writes the employee's check-in at `at` on the tenant's own plaque, as tappa_app in the
// tenant's context: occurred_at and created_at both at `at`, so neither debounce leg sees it.
func (w *holdingWorld) seedIn(tenant, employee, venue uuid.UUID, uid string, at time.Time) {
	w.t.Helper()
	if err := w.data.WithTenant(w.ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at,
		                                                verdict, channel, type, sun_valid, ctr)
		                      VALUES ($1, $2, $3, $4, $5, $5, 'ok', 'nfc', 'in', true, 90)`, tenant, employee, venue, uid, at)
		return e
	}); err != nil {
		w.t.Fatalf("seed the check-in: %v", err)
	}
}

// dayStart is the row's frozen business day (business_day_start), nil for NULL.
func (w *holdingWorld) dayStart(id uuid.UUID) *time.Time {
	w.t.Helper()
	var v *time.Time
	if err := w.owner.QueryRow(w.ctx, `SELECT business_day_start FROM transactions WHERE id = $1`, id).Scan(&v); err != nil {
		w.t.Fatalf("read business_day_start of %s: %v", id, err)
	}
	return v
}

// recordAt runs one tap through svc with the service's clock at `now`.
func recordAt(t *testing.T, svc *Service, ctx context.Context, now time.Time, req Request) Result {
	t.Helper()
	svc.now = func() time.Time { return now }
	defer func() { svc.now = nil }()
	res, err := svc.Record(ctx, req)
	if err != nil {
		t.Fatalf("Record at %v: %v", now, err)
	}
	return res
}

func direction(res Result) string {
	if res.Decision.Type == nil {
		return "none"
	}
	return string(*res.Decision.Type)
}

// sameInstant says whether a frozen business day is exactly want.
func sameInstant(got *time.Time, want time.Time) bool { return got != nil && got.Equal(want) }

// TestRecordDB_ACheckInOfThePreviousBusinessDayStaysOpen is ADR 0032 §A.1 through Record, on a
// business of the test's own with the shipped 05:00: the "17 h" week — Monday 16:00 check-in,
// nothing at the end of the day, Tuesday 09:00 tap. 17 h is inside StaleOpenIn, so only the
// business day makes Tuesday's tap an `in`; the Monday entry stays open (LeftOpen, previous
// business day) and the row freezes Tuesday 05:00 Malta. The employee has no profile branch, so
// the business's cutoff is the one that applies (§A.2).
func TestRecordDB_ACheckInOfThePreviousBusinessDayStaysOpen(t *testing.T) {
	w := newHoldingWorld(t)
	w.exec(`UPDATE tenants SET day_cutoff = '05:00' WHERE id = $1`, w.kf)
	emp := w.employeeOnTheHierarchy(w.kf, nil)
	w.seedIn(w.kf, emp, w.kfVenue, w.kfTag, maltaWall(t, monday, 16, 0))

	res := recordAt(t, w.svc, w.ctx, maltaWall(t, tuesday, 9, 0), w.nfc(w.kf, emp, w.kfTag, w.kf, 101, w.kfOnNet))
	want := maltaWall(t, tuesday, 5, 0)
	if direction(res) != "in" {
		t.Fatalf("Tuesday 09:00 = %s (%s), want in: Monday's check-in is of the previous business day", direction(res), res.Decision.MatchedSid)
	}
	if lo := res.Decision.LeftOpen; lo == nil || lo.Reason != tap.LeftOpenPreviousBusinessDay {
		t.Errorf("LeftOpen = %+v, want the previous business day", lo)
	}
	if !sameInstant(res.Decision.BusinessDayStart, want) || !sameInstant(w.dayStart(res.TransactionID), want) {
		t.Errorf("business day: decision %v, row %v, want %v (Tuesday 05:00 Malta)",
			res.Decision.BusinessDayStart, w.dayStart(res.TransactionID), want.UTC())
	}
}

// TestRecordDB_TheRustyBarNightIsOneBusinessDay is §5's Rusty Bar rule under the business day
// (ADR 0032 §A.2): a branch shaped like Rusty Bar (18:00–02:00, overnight) with the business's
// 05:00 — the 18:05 check-in and the 02:10 checkout are two engine-written rows of ONE business
// day: the second is the `out` and both freeze Monday 05:00 Malta.
//
// The two rows go through Record, so the second waits out the server-clock debounce leg (the
// database's created_at age, ADR 0006): this test's service runs the range minimum of 30 s and
// polls the database's own clock, and runs in parallel with the rest of the package.
func TestRecordDB_TheRustyBarNightIsOneBusinessDay(t *testing.T) {
	t.Parallel()
	w := newHoldingWorld(t)
	w.exec(`UPDATE tenants SET day_cutoff = '05:00' WHERE id = $1`, w.kf)
	bar, barTag := w.branch(w.kf, "", "18:00", "02:00", true)
	emp := w.employeeOnTheHierarchy(w.kf, &bar)
	svc := w.serviceWith(fixtures.BusinessDaySwitchOn(), 30*time.Second)

	in := recordAt(t, svc, w.ctx, maltaWall(t, monday, 18, 5), w.nfc(w.kf, emp, barTag, w.kf, 101, w.kfOnNet))
	if direction(in) != "in" {
		t.Fatalf("18:05 = %s, want in", direction(in))
	}
	for start := time.Now(); ; time.Sleep(250 * time.Millisecond) {
		var age float64
		if err := w.owner.QueryRow(w.ctx, `SELECT EXTRACT(EPOCH FROM clock_timestamp() - created_at)::float8 FROM transactions WHERE id = $1`,
			in.TransactionID).Scan(&age); err != nil {
			t.Fatal(err)
		}
		if age > 31 {
			break
		}
		if time.Since(start) > 2*time.Minute {
			t.Fatalf("the check-in is %.1f s old after %v", age, time.Since(start))
		}
	}
	out := recordAt(t, svc, w.ctx, maltaWall(t, tuesday, 2, 10), w.nfc(w.kf, emp, barTag, w.kf, 102, w.kfOnNet))
	if direction(out) != "out" {
		t.Fatalf("02:10 = %s (%s), want out: the night is one business day", direction(out), out.Decision.MatchedSid)
	}
	want := maltaWall(t, monday, 5, 0)
	if a, b := w.dayStart(in.TransactionID), w.dayStart(out.TransactionID); !sameInstant(a, want) || !sameInstant(b, want) {
		t.Errorf("business days %v and %v, want both %v (Monday 05:00 Malta)", a, b, want.UTC())
	}
}

// TestRecordDB_TheBusinessDayIsTheProfileBranchesNotTheTappedOnes is the hierarchy end to end
// (ADR 0032 §A.2, K1): branch A cuts at 06:00, branch B at 04:00, the business at 05:00. A
// person of A tapping B's plaque meets A's 06:00 — the chain is the person's, not the plaque's —
// and a person with no profile branch meets the business's 05:00, never the tapped B's.
func TestRecordDB_TheBusinessDayIsTheProfileBranchesNotTheTappedOnes(t *testing.T) {
	w := newHoldingWorld(t)
	w.exec(`UPDATE tenants SET day_cutoff = '05:00' WHERE id = $1`, w.kf)
	a, _ := w.branch(w.kf, "06:00", "09:00", "17:00", false)
	b, bTag := w.branch(w.kf, "04:00", "09:00", "17:00", false)
	tuesday0900 := maltaWall(t, tuesday, 9, 0)

	ofA := w.employeeOnTheHierarchy(w.kf, &a)
	w.seedIn(w.kf, ofA, b, bTag, maltaWall(t, tuesday, 5, 30)) // before A's 06:00, after B's 04:00 and the business's 05:00
	res := recordAt(t, w.svc, w.ctx, tuesday0900, w.nfc(w.kf, ofA, bTag, w.kf, 101, w.kfOnNet))
	if direction(res) != "in" || !sameInstant(res.Decision.BusinessDayStart, maltaWall(t, tuesday, 6, 0)) {
		t.Errorf("A's person on B's plaque: %s, business day %v, want in from Tuesday 06:00 (A's cutoff, not B's 04:00)",
			direction(res), res.Decision.BusinessDayStart)
	}

	noProfile := w.employeeOnTheHierarchy(w.kf, nil)
	w.seedIn(w.kf, noProfile, b, bTag, maltaWall(t, tuesday, 4, 30)) // after B's 04:00, before the business's 05:00
	res = recordAt(t, w.svc, w.ctx, tuesday0900, w.nfc(w.kf, noProfile, bTag, w.kf, 102, w.kfOnNet))
	if direction(res) != "in" || !sameInstant(res.Decision.BusinessDayStart, maltaWall(t, tuesday, 5, 0)) {
		t.Errorf("no profile branch, on B's plaque: %s, business day %v, want in from Tuesday 05:00 (the business's)",
			direction(res), res.Decision.BusinessDayStart)
	}
}

// TestHoldingTap_TheBusinessDayIsTheEmployersOnThePeersPlaque is §A.2's holding sentence (ADR
// 0027 §7): KM's employee tapping KF's plaque meets KM's hierarchy — KM cuts at 06:00, KF and
// KF's venue at 04:00. A KM check-in at 05:30 is of the previous business day by KM's cutoff, so
// the 09:00 tap on KF's plaque is an `in` that freezes Tuesday 06:00 on KM's row (with the site
// columns); by KF's it would have been the `out`.
func TestHoldingTap_TheBusinessDayIsTheEmployersOnThePeersPlaque(t *testing.T) {
	w := newHoldingWorld(t)
	w.exec(`UPDATE tenants SET day_cutoff = '06:00' WHERE id = $1`, w.km)
	w.exec(`UPDATE tenants SET day_cutoff = '04:00' WHERE id = $1`, w.kf)
	w.exec(`UPDATE locations SET day_cutoff = '04:00' WHERE id = $1`, w.kfVenue)
	emp := w.employeeOnTheHierarchy(w.km, nil)
	w.seedIn(w.km, emp, w.kmVenue, w.kmTag, maltaWall(t, tuesday, 5, 30))

	res := recordAt(t, w.svc, w.ctx, maltaWall(t, tuesday, 9, 0), w.nfc(w.km, emp, w.kfTag, w.kf, 101, w.kfOnNet))
	want := maltaWall(t, tuesday, 6, 0)
	if direction(res) != "in" || !sameInstant(res.Decision.BusinessDayStart, want) {
		t.Fatalf("KM's person on KF's plaque: %s (%s), business day %v, want in from Tuesday 06:00 (KM's cutoff)",
			direction(res), res.Decision.Verdict, res.Decision.BusinessDayStart)
	}
	if !sameInstant(w.dayStart(res.TransactionID), want) {
		t.Errorf("the row's business day %v, want %v", w.dayStart(res.TransactionID), want.UTC())
	}
	if err := isSite(w.record(res.TransactionID), w, w.kfTag, &w.kfVenue); err != nil {
		t.Error(err)
	}
}

// TestRecordDB_OnlyADirectedTapFreezesABusinessDay is §A.5's NULL rule in the row (the unit half
// is TestInsertParams_FreezesTheBusinessDayOnlyOnADirectedTap): with the switch on, a reject
// (a lost plaque — no direction) and a manager's manual record (directed, and the engine DID
// compute its business day) both write NULL; a directed QR tap writes it.
func TestRecordDB_OnlyADirectedTapFreezesABusinessDay(t *testing.T) {
	w := newHoldingWorld(t)
	tuesday0900 := maltaWall(t, tuesday, 9, 0)

	lost := recordAt(t, w.svc, w.ctx, tuesday0900, w.nfc(w.kf, w.employeeOnTheHierarchy(w.kf, nil), w.kfLost, w.kf, 101, w.kfOnNet))
	if lost.Decision.Verdict != tap.VerdictReject || lost.TransactionID == uuid.Nil {
		t.Fatalf("PREMISE: a lost plaque is a recorded reject, got %s", lost.Decision.Verdict)
	}
	if v := w.dayStart(lost.TransactionID); v != nil {
		t.Errorf("a reject froze a business day: %v", *v)
	}

	author := uuid.New()
	manual := w.nfc(w.kf, w.employeeOnTheHierarchy(w.kf, nil), w.kfTag, w.kf, 0, w.kfOnNet)
	manual.Channel, manual.EnteredBy, manual.CMACVerified = tap.ChannelManual, &author, false
	m := recordAt(t, w.svc, w.ctx, tuesday0900, manual)
	if m.Decision.Type == nil || m.Decision.BusinessDayStart == nil {
		t.Fatalf("PREMISE: the manual record is directed and the engine computed its business day: %s, %v",
			direction(m), m.Decision.BusinessDayStart)
	}
	if v := w.dayStart(m.TransactionID); v != nil {
		t.Errorf("a manual record froze a business day: %v", *v)
	}

	qr := recordAt(t, w.svc, w.ctx, tuesday0900, w.qr(w.kf, w.employeeOnTheHierarchy(w.kf, nil), w.kfTag, w.kf, w.kfOnNet))
	if qr.Decision.Type == nil || !sameInstant(w.dayStart(qr.TransactionID), maltaWall(t, tuesday, 5, 0)) {
		t.Errorf("a directed QR tap: %s, row business day %v, want Tuesday 05:00 Malta", direction(qr), w.dayStart(qr.TransactionID))
	}
}

// The business-day switch's modes (ADR 0032 §A.8; motor lens ORTA-1). The scene is the one the
// settings warning describes (§A.7): a shift of 04:00–12:00 under a 05:00 cutoff — the 03:58
// check-in "counts for the previous day" and the 12:05 checkout becomes a check-in. With the
// switch OFF the engine has no cutoff (StaleOpenIn alone), so the 12:05 tap closes the 03:58
// entry as it always did, and nothing is frozen.
func cutShiftScene(t *testing.T) (w *holdingWorld, emp uuid.UUID, tag string, checkout time.Time) {
	t.Helper()
	w = newHoldingWorld(t)
	w.exec(`UPDATE tenants SET day_cutoff = '05:00' WHERE id = $1`, w.kf)
	bakery, tag := w.branch(w.kf, "", "04:00", "12:00", false)
	emp = w.employeeOnTheHierarchy(w.kf, &bakery)
	w.seedIn(w.kf, emp, bakery, tag, maltaWall(t, tuesday, 3, 58))
	return w, emp, tag, maltaWall(t, tuesday, 12, 5)
}

// assertUncut is the switch-off outcome of cutShiftScene: `out`, no business day anywhere.
func assertUncut(t *testing.T, w *holdingWorld, res Result) {
	t.Helper()
	if direction(res) != "out" {
		t.Errorf("12:05 = %s (%s), want out: with the switch off the 04:00–12:00 shift is not cut", direction(res), res.Decision.MatchedSid)
	}
	if res.Decision.BusinessDayStart != nil || res.Decision.LeftOpen != nil {
		t.Errorf("the decision carries a business day %v / left open %+v with the switch off", res.Decision.BusinessDayStart, res.Decision.LeftOpen)
	}
	if v := w.dayStart(res.TransactionID); v != nil {
		t.Errorf("the row froze a business day %v with the switch off", *v)
	}
}

// TestRecordDB_TheBusinessDaySwitchUnsetIsTheRuleWithoutACutoff: TAPPA_BUSINESS_DAY_FROM unset.
func TestRecordDB_TheBusinessDaySwitchUnsetIsTheRuleWithoutACutoff(t *testing.T) {
	w, emp, tag, checkout := cutShiftScene(t)
	res := recordAt(t, w.serviceWith(nil, 60*time.Second), w.ctx, checkout, w.nfc(w.kf, emp, tag, w.kf, 101, w.kfOnNet))
	assertUncut(t, w, res)

	// CONTROL: the same scene with the switch on IS cut — so the outcome above is the switch's.
	w2, emp2, tag2, checkout2 := cutShiftScene(t)
	if on := recordAt(t, w2.svc, w2.ctx, checkout2, w2.nfc(w2.kf, emp2, tag2, w2.kf, 101, w2.kfOnNet)); direction(on) != "in" {
		t.Fatalf("CONTROL: with the switch on the 12:05 tap is %s, want in (the 05:00 cutoff cuts the shift)", direction(on))
	}
}

// TestRecordDB_TheBusinessDaySwitchIsOffBeforeItsInstantByTheServersClock: a value set, and the
// SERVER's clock before it — off (security lens F-2: the server's clock decides). Its mirror is
// the declared instant: a tap that DECLARES an instant before the value while the server's clock
// is past it is judged by the new rule — the switch must not be steerable by backdating. And the
// card's literal shape — the declared instant after the value, the server's clock before it — is
// necessarily a FUTURE declaration, which sys:occurred-at-bound rejects: no direction, nothing
// frozen.
func TestRecordDB_TheBusinessDaySwitchIsOffBeforeItsInstantByTheServersClock(t *testing.T) {
	t.Run("the server's clock before the value is off", func(t *testing.T) {
		w, emp, tag, checkout := cutShiftScene(t)
		from := checkout.Add(time.Hour)
		assertUncut(t, w, recordAt(t, w.serviceWith(&from, 60*time.Second), w.ctx, checkout, w.nfc(w.kf, emp, tag, w.kf, 101, w.kfOnNet)))
	})
	t.Run("a declared instant before the value does not switch it off", func(t *testing.T) {
		w, emp, tag, checkout := cutShiftScene(t)
		from := checkout.Add(-time.Minute)
		req := w.nfc(w.kf, emp, tag, w.kf, 101, w.kfOnNet)
		declared := checkout.Add(-6 * time.Minute) // before the value; base:queued-window flags it, directed all the same
		req.OccurredAt = &declared
		res := recordAt(t, w.serviceWith(&from, 60*time.Second), w.ctx, checkout, req)
		if direction(res) != "in" || !sameInstant(w.dayStart(res.TransactionID), maltaWall(t, tuesday, 5, 0)) {
			t.Errorf("declared %v, server %v, switch %v: %s (%s), row business day %v — want in from Tuesday 05:00: "+
				"the switch is read on the server's clock", declared, checkout, from, direction(res), res.Decision.MatchedSid,
				w.dayStart(res.TransactionID))
		}
	})
	t.Run("a declared instant after the value with the server's clock before it is a rejected future claim", func(t *testing.T) {
		w, emp, tag, checkout := cutShiftScene(t)
		from := checkout.Add(30 * time.Second)
		req := w.nfc(w.kf, emp, tag, w.kf, 101, w.kfOnNet)
		declared := checkout.Add(time.Minute)
		req.OccurredAt = &declared
		res := recordAt(t, w.serviceWith(&from, 60*time.Second), w.ctx, checkout, req)
		if res.Decision.Verdict != tap.VerdictReject || res.Decision.Type != nil || res.Decision.BusinessDayStart != nil {
			t.Fatalf("a future declaration: %s/%s, business day %v — want a reject with no direction", res.Decision.Verdict,
				direction(res), res.Decision.BusinessDayStart)
		}
		if v := w.dayStart(res.TransactionID); v != nil {
			t.Errorf("the reject froze a business day %v", *v)
		}
	})
}
