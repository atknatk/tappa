package checkin

// requesteddirection_db_test.go -- M15-11 on the real tap path (ADR 0032 §B.3, §B.4, §B.5): the
// direction the person pressed reaches the engine inside the locked section, the row records who
// chose (direction_source), the switch's one gate decides whether the request is heard at all,
// and a chosen checkout is read back by the readers (tappa_checkout_class, the "check-in missing"
// list of M15-06) in the class the engine anchored it as (Decision.MissingIn).
//
// THE WORLDS ARE THE TESTS' OWN (newHoldingWorld), with the business's 05:00 cutoff written by
// the test and employees that have no cutoff of their own (employeeOnTheHierarchy), on the fixed
// week of businessday_db_test.go (Mon 2026-10-19, Tue 2026-10-20, Malta CEST). Predecessors are
// written as tappa_app with occurred_at = created_at at those instants (seedIn / seedOut), so the
// server-clock debounce leg is days away from them.
//
// ⚠️ THESE TESTS COMMIT ROWS WITH A direction_source, and so does every other tap test from this
// card on. 00065's Down refuses over such a row; internal/db's
// TestDirectionSource00065And00066_DownsRefuseAChoiceAndOtherwiseGiveBack sets them aside inside
// its rolled-back savepoint before walking the Down.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

// wantDir is a fresh *tap.Type for a request.
func wantDir(d tap.Type) *tap.Type { return &d }

// seedOut writes the employee's checkout at `at` (occurred_at = created_at), the shape an engine
// checkout of that business day has (business_day_start = dayStart).
func (w *holdingWorld) seedOut(tenant, employee, venue uuid.UUID, uid string, at, dayStart time.Time) {
	w.t.Helper()
	if err := w.data.WithTenant(w.ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at,
		                                                verdict, channel, type, sun_valid, ctr, business_day_start)
		                      VALUES ($1, $2, $3, $4, $5, $5, 'ok', 'nfc', 'out', true, 91, $6)`, tenant, employee, venue, uid, at, dayStart)
		return e
	}); err != nil {
		w.t.Fatalf("seed the checkout: %v", err)
	}
}

// rowSource is the written row's direction_source and type, read as the owner.
func (w *holdingWorld) rowSource(id uuid.UUID) (source, kind *string) {
	w.t.Helper()
	if err := w.owner.QueryRow(w.ctx, `SELECT direction_source, type FROM transactions WHERE id = $1`, id).Scan(&source, &kind); err != nil {
		w.t.Fatalf("read direction_source of %s: %v", id, err)
	}
	return source, kind
}

// assertWhoChose is the column's invariant on one written row (ADR 0032 §B.3; the M15-09
// security lens's note 1): a directed nfc/qr row carries a source, 'employee' exactly when the
// recorded direction differs from the engine's D, and the Result says what the row says.
func (w *holdingWorld) assertWhoChose(res Result, d tap.Type) {
	w.t.Helper()
	source, kind := w.rowSource(res.TransactionID)
	switch {
	case kind == nil:
		w.t.Fatalf("the row has no direction (%s)", res.Decision.Verdict)
	case source == nil:
		w.t.Fatalf("a directed row has a NULL direction_source -- indistinguishable from a row written before M15-11")
	case (*source == string(tap.DirectionEmployee)) != (*kind != string(d)):
		w.t.Fatalf("recorded %s, the engine's D %s, direction_source %q: 'employee' must hold exactly when they differ", *kind, d, *source)
	case res.DirectionSource == nil || string(*res.DirectionSource) != *source:
		w.t.Fatalf("Result.DirectionSource %v, the row %q", res.DirectionSource, *source)
	}
}

// listedWithoutCheckIn is M15-06's "check-in missing" list (ListCheckoutsWithoutCheckIn) for the
// test week, reduced to the employee's rows: their instants and whether each is (b).
func (w *holdingWorld) listedWithoutCheckIn(tenant, employee uuid.UUID) map[time.Time]bool {
	w.t.Helper()
	out := map[time.Time]bool{}
	if err := w.data.WithTenant(w.ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListCheckoutsWithoutCheckIn(ctx, store.ListCheckoutsWithoutCheckInParams{
			TenantID: tenant, FromAt: maltaWall(w.t, monday, 0, 0), ToAt: maltaWall(w.t, tuesday+2, 0, 0), RowLimit: 500})
		for _, r := range rows {
			if r.EmployeeID != nil && *r.EmployeeID == employee {
				out[r.OccurredAt.UTC()] = r.AfterEarlierDay
			}
		}
		return err
	}); err != nil {
		w.t.Fatalf("list the checkouts without a check-in: %v", err)
	}
	return out
}

// TestRecordDB_AChosenDirectionIsRecordedWithWhoChoseIt is the card's DB criterion (ADR 0032
// §B.3): the engine predicts `in` (no open check-in) and the person presses "Clock out" -- the
// row is `out`, 'employee', still `ok` on the network evidence; with no request it is `in`,
// 'auto'; a request equal to D is the engine's choice too ('auto' means recorded = D, not "no
// request arrived").
func TestRecordDB_AChosenDirectionIsRecordedWithWhoChoseIt(t *testing.T) {
	w := newHoldingWorld(t)
	w.exec(`UPDATE tenants SET day_cutoff = '05:00' WHERE id = $1`, w.kf)
	at := maltaWall(t, tuesday, 9, 0)
	cases := []struct {
		name    string
		want    *tap.Type
		dir     tap.Type
		source  tap.DirectionSource
		chosen  bool
		missing tap.CheckoutClass
	}{
		{"want out over the engine's in", wantDir(tap.TypeOut), tap.TypeOut, tap.DirectionEmployee, true, tap.CheckoutOrphan},
		{"no want", nil, tap.TypeIn, tap.DirectionAuto, false, ""},
		{"want in, the engine's own", wantDir(tap.TypeIn), tap.TypeIn, tap.DirectionAuto, false, ""},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emp := w.employeeOnTheHierarchy(w.kf, nil)
			req := w.nfc(w.kf, emp, w.kfTag, w.kf, uint32(101+i), w.kfOnNet)
			req.RequestedDirection = tc.want
			res := recordAt(t, w.svc, w.ctx, at, req)
			if res.Decision.Verdict != tap.VerdictOK || direction(res) != string(tc.dir) || res.Decision.DirectionSource != tc.source {
				t.Fatalf("%s/%s/%q via %s, want ok/%s/%q", res.Decision.Verdict, direction(res), res.Decision.DirectionSource,
					res.Decision.MatchedSid, tc.dir, tc.source)
			}
			if res.Decision.MissingIn != tc.missing {
				t.Errorf("MissingIn %q, want %q", res.Decision.MissingIn, tc.missing)
			}
			if got := strings.Contains(res.Decision.Note, tap.DirectionChosenNote); got != tc.chosen {
				t.Errorf("the record's note %q: carries the chosen-direction note = %v, want %v", res.Decision.Note, got, tc.chosen)
			}
			w.assertWhoChose(res, tap.TypeIn)
		})
	}
}

// TestRecordDB_AChosenCheckoutReadsBackAsTheClassTheEngineAnchored is the motor lens's DÜŞÜK-2
// parity (M15-10): a chosen `out` written through Record is classified by the readers --
// tappa_checkout_class with P, I and W found as ADR 0032 §A.6 defines them, and M15-06's "check-in
// missing" list -- exactly as Decision.MissingIn named it: (b) and (c) are anchors and listed, (a)
// is neither, a break's wrong press is (c). The shape (d) cannot be written by a choice: with the
// switch off the request is dropped, the tap is the engine's `in`, and there is no checkout to read.
func TestRecordDB_AChosenCheckoutReadsBackAsTheClassTheEngineAnchored(t *testing.T) {
	type seed struct {
		out      bool
		day, hh  int
		mm       int
		dayStart time.Time
	}
	tuesday0500 := maltaWall(t, tuesday, 5, 0)
	cases := []struct {
		name     string
		seeds    []seed
		day      int
		hh, mm   int
		switchOn bool
		d        tap.Type // the engine's direction for the scene
		dir      tap.Type // what is recorded
		class    string   // the reader's class of the recorded checkout ("" = no checkout written)
		missing  tap.CheckoutClass
	}{
		// (b), the stale page across the boundary: the screen said "Clock out" for Monday 22:00's
		// check-in; the press arrives at 05:01, a new business day -- D is in, the press is out.
		{"(b) a stale page pressed after the cutoff", []seed{{day: monday, hh: 22}}, tuesday, 5, 1, true,
			tap.TypeIn, tap.TypeOut, "other_day", tap.CheckoutOtherDay},
		// (c): nothing before it.
		{"(c) a checkout with nothing before it", nil, tuesday, 9, 0, true,
			tap.TypeIn, tap.TypeOut, "orphan", tap.CheckoutOrphan},
		// (a): an in of this business day, more than StaleOpenIn earlier -- the out consumes it,
		// the in is on the open list, and the checkout is not "check-in missing".
		{"(a) a checkout after an in of the same day longer than StaleOpenIn", []seed{{day: tuesday, hh: 5, mm: 10}}, tuesday, 23, 30, true,
			tap.TypeIn, tap.TypeOut, "too_long", ""},
		// The break: 11:00 in, 13:00 out (closed), back at 14:00 and "Not right? Clock out" pressed
		// by mistake: P is an out, the day has an in, P written earlier -- (c).
		{"a break's wrong press", []seed{{day: tuesday, hh: 11}, {out: true, day: tuesday, hh: 13, dayStart: tuesday0500}}, tuesday, 14, 0, true,
			tap.TypeIn, tap.TypeOut, "orphan", tap.CheckoutOrphan},
		// (d): the switch off -- the request never reaches the engine; the engine's in is recorded.
		{"(d) with the switch off the choice is not written", nil, tuesday, 9, 0, false,
			tap.TypeIn, tap.TypeIn, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newHoldingWorld(t)
			w.exec(`UPDATE tenants SET day_cutoff = '05:00' WHERE id = $1`, w.kf)
			emp := w.employeeOnTheHierarchy(w.kf, nil)
			for _, s := range tc.seeds {
				at := maltaWall(t, s.day, s.hh, s.mm)
				if s.out {
					w.seedOut(w.kf, emp, w.kfVenue, w.kfTag, at, s.dayStart)
				} else {
					w.seedIn(w.kf, emp, w.kfVenue, w.kfTag, at)
				}
			}
			svc := w.svc
			if !tc.switchOn {
				svc = w.serviceWith(nil, 60*time.Second)
			}
			req := w.nfc(w.kf, emp, w.kfTag, w.kf, 101, w.kfOnNet)
			req.RequestedDirection = wantDir(tap.TypeOut)
			res := recordAt(t, svc, w.ctx, maltaWall(t, tc.day, tc.hh, tc.mm), req)
			if direction(res) != string(tc.dir) {
				t.Fatalf("recorded %s (%s, %s), want %s", direction(res), res.Decision.Verdict, res.Decision.MatchedSid, tc.dir)
			}
			w.assertWhoChose(res, tc.d)
			if res.Decision.MissingIn != tc.missing {
				t.Errorf("Decision.MissingIn %q, want %q", res.Decision.MissingIn, tc.missing)
			}
			classes := w.checkoutClasses(w.kf, emp)
			got := ""
			if tc.dir == tap.TypeOut {
				if len(classes) == 0 {
					t.Fatal("the recorded checkout has no class")
				}
				got = classes[len(classes)-1] // the tap is the person's latest checkout
			}
			if got != tc.class {
				t.Errorf("tappa_checkout_class of the recorded checkout %q (all %v), want %q", got, classes, tc.class)
			}
			// THE PARITY: the engine's anchor is the reader's class wherever the reader lists it.
			if anchored := tc.class == "other_day" || tc.class == "orphan"; anchored != (res.Decision.MissingIn != "") ||
				(anchored && string(res.Decision.MissingIn) != tc.class) {
				t.Errorf("the reader's class %q and Decision.MissingIn %q disagree", tc.class, res.Decision.MissingIn)
			}
			listed := w.listedWithoutCheckIn(w.kf, emp)
			earlier, ok := listed[res.OccurredAt.UTC()]
			switch {
			case ok != (res.Decision.MissingIn != ""):
				t.Errorf("on the check-in-missing list %v, MissingIn %q: the list and the anchor disagree", ok, res.Decision.MissingIn)
			case ok && earlier != (res.Decision.MissingIn == tap.CheckoutOtherDay):
				t.Errorf("listed as after an earlier day %v, MissingIn %q", earlier, res.Decision.MissingIn)
			}
		})
	}
}

// TestRecordDB_TheRequestIsHeardOnlyWhileTheSwitchIsOn is ADR 0032 §B.5 (security lens F-1) at
// the orchestrator, in the switch's two closed modes and the open one as the control: the same
// `want=out` over the engine's `in` is the engine's `in`, 'auto', one row -- with the switch unset
// and with the SERVER's clock before its instant -- and the person's `out`, 'employee', once the
// switch is on. Without S the chosen out would be (d), listed nowhere.
func TestRecordDB_TheRequestIsHeardOnlyWhileTheSwitchIsOn(t *testing.T) {
	at := maltaWall(t, tuesday, 9, 0)
	later := at.Add(time.Hour)
	modes := []struct {
		name   string
		from   *time.Time
		dir    tap.Type
		source tap.DirectionSource
	}{
		{"switch unset", nil, tap.TypeIn, tap.DirectionAuto},
		{"the server's clock before the switch", &later, tap.TypeIn, tap.DirectionAuto},
		{"CONTROL: switch on", fixtures.BusinessDaySwitchOn(), tap.TypeOut, tap.DirectionEmployee},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			w := newHoldingWorld(t)
			w.exec(`UPDATE tenants SET day_cutoff = '05:00' WHERE id = $1`, w.kf)
			emp := w.employeeOnTheHierarchy(w.kf, nil)
			before := w.rows(w.kf)
			req := w.nfc(w.kf, emp, w.kfTag, w.kf, 101, w.kfOnNet)
			req.RequestedDirection = wantDir(tap.TypeOut)
			res := recordAt(t, w.serviceWith(m.from, 60*time.Second), w.ctx, at, req)
			if direction(res) != string(m.dir) || res.Decision.DirectionSource != m.source {
				t.Fatalf("%s/%q, want %s/%q", direction(res), res.Decision.DirectionSource, m.dir, m.source)
			}
			w.assertWhoChose(res, tap.TypeIn)
			if n := w.rows(w.kf) - before; n != 1 {
				t.Errorf("%d rows written, want exactly one", n)
			}
		})
	}
}

// TestHoldingTap_AChosenDirectionAtAPeersPlaqueIsTheEmployersAndKeepsItsNote is the partner-venue
// half (ADR 0027 §5, K-39; ADR 0032 §B.4): KM's employee presses "Clock out" on KF's plaque over the
// engine's `in`. The row is KM's, with the site columns and 'employee'; its note ends with the
// chosen-direction annotation; and KM's day list, which replaces the evidence rule's sentence at a
// partner venue (PartnerVenueNote), keeps the annotation after it (ledger's partnerKeptAnnotations).
// The reader of that note is a db/queries statement filed by internal/db's
// TestEvidenceReaders_EveryOneIsClassified; this card adds none.
func TestHoldingTap_AChosenDirectionAtAPeersPlaqueIsTheEmployersAndKeepsItsNote(t *testing.T) {
	w := newHoldingWorld(t)
	w.exec(`UPDATE tenants SET day_cutoff = '05:00' WHERE id = ANY($1)`, []uuid.UUID{w.km, w.kf})
	emp := w.employeeOnTheHierarchy(w.km, nil)
	req := w.nfc(w.km, emp, w.kfTag, w.kf, 101, w.kfOnNet)
	req.RequestedDirection = wantDir(tap.TypeOut)
	res := recordAt(t, w.svc, w.ctx, maltaWall(t, tuesday, 9, 0), req)
	if res.Decision.Verdict != tap.VerdictOK || direction(res) != "out" || res.Decision.DirectionSource != tap.DirectionEmployee {
		t.Fatalf("%s/%s/%q via %s, want ok/out/employee", res.Decision.Verdict, direction(res), res.Decision.DirectionSource, res.Decision.MatchedSid)
	}
	if err := isSite(w.record(res.TransactionID), w, w.kfTag, &w.kfVenue); err != nil {
		t.Fatal(err)
	}
	w.assertWhoChose(res, tap.TypeIn)
	if !strings.HasSuffix(res.Decision.Note, "; "+tap.DirectionChosenNote) {
		t.Fatalf("the record's note %q does not end with the chosen-direction annotation", res.Decision.Note)
	}

	reader, err := ledger.NewReader(w.data, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	page, err := reader.Day(w.ctx, w.km, ledger.Filter{Date: ledger.Date{Year: 2026, Month: time.October, Day: tuesday}})
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, rec := range page.Records {
		if rec.ID != res.TransactionID {
			continue
		}
		seen = true
		if !strings.HasPrefix(rec.Note, ledger.PartnerVenueNote) || !strings.HasSuffix(rec.Note, "; "+tap.DirectionChosenNote) {
			t.Errorf("KM's day list reads the partner-venue note %q, want %q followed by the kept annotations ending %q",
				rec.Note, ledger.PartnerVenueNote, tap.DirectionChosenNote)
		}
		if strings.Contains(rec.Note, "network proof of place") {
			t.Errorf("the evidence sentence reached the employer's list: %q", rec.Note)
		}
	}
	if !seen {
		t.Fatalf("the record is not on KM's day list (%d records)", len(page.Records))
	}
}
