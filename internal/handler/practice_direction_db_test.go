package handler

// THE PRACTICE ROW AND THE DIRECTION CHAIN (M5-11, ADR 0008), driven through the
// production write path against real Postgres.
//
// 🔴 WHAT WENT WRONG, AND WHY IT NEEDED ITS OWN FILE. §5 resolves direction by
// toggling against "the person's LAST OPEN check-in". Until ADR 0008 the query that
// answered that returned ONE row and was blind to the practice flag, and the caller
// discarded a practice row without looking at the row beneath it. So a training tap
// that merely sorted newest hid a real, still-open check-in: the checkout came back
// as an `in`, the entry never closed, and NOTHING said so — verdict ok, no note, no
// flag. To a manager it looked like the employee's own forgotten checkout.
//
// It was found by day_db_test.go stepping around it (LIMITS L3) and it is fixed by
// `AND NOT t.practice` in GetLastOpenTransaction. This file is the pin: the same
// two arms that measured the defect, now measured through checkin.Service.Record.
//
// SINCE ADR 0025 THE ENGINE WRITES NO PRACTICE ROW (the activating NFC tap replaced
// the training tap), so the training row each arm needs is SEEDED as the historic
// row it now can only be — transactions are immutable (§4.3), so every practice row
// written before ADR 0025 is still there and still read by this query.
//
// WHY THE MANUAL CHANNEL AND NOT HTTP TAPS. ADR 0006 measures the person-debounce
// on the SERVER clock over `channel IN ('nfc','qr')` rows, so three NFC taps by one
// person cost two real waits (~62 s). A manual row is exempt from that leg — which
// is what lets these six records be written in under a second. What the substitution
// gives up is stated rather than glossed: the direction chain is channel-blind
// (resolveDirection reads only LastOpenIn), so nothing here depends on the channel,
// but the NFC path's own end-to-end evidence is TestSeedDB_ADayAtKFStJulians, whose
// Rusty Bar night shift now runs WITHOUT the declared-time workaround that used to
// step around this defect.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/test/fixtures"
)

// enterManually writes one record through the SAME service the router uses, with a
// declared instant. There is no HTTP surface for a manual entry yet (M6-04); the
// engine output is what matters here, not the transport.
func (f *seedFlow) enterManually(t *testing.T, employeeID uuid.UUID, plaque string, at *time.Time) checkin.Result {
	t.Helper()
	res, err := f.checkins.Record(context.Background(), checkin.Request{
		SessionTenantID: f.tenantID,
		EmployeeID:      employeeID,
		TagUID:          plaque,
		TagTenantID:     f.tenantID,
		Channel:         tap.ChannelManual,
		EnteredBy:       ptrUUID(fixtures.AdminKFOwner),
		OccurredAt:      at,
	})
	if err != nil {
		t.Fatalf("manual entry: %v", err)
	}
	return res
}

// TestSeedDB_APracticeRowNeverHidesAnOlderOpenCheckIn is the control/broken table
// from the M5-11 card, after the fix.
//
// Each arm writes the same three records — a TRAINING tap, a real check-in three
// hours ago, and a third tap now — and differs ONLY in when the training tap claims
// to have happened. Measured before ADR 0008:
//
//	practice 4 h ago (sorts below the real 'in')   third tap `out`, 0 open check-ins
//	practice 100 s ago (sorts above it)            third tap `in`,  2 open check-ins
//
// Both arms must now give the same answer, and the second one is the one that used
// to be wrong. A mutation that drops `AND NOT t.practice` from
// GetLastOpenTransaction turns this red on the second arm.
func TestSeedDB_APracticeRowNeverHidesAnOlderOpenCheckIn(t *testing.T) {
	f := newSeedFlow(t)
	venue := f.venue(t, fixtures.LocKFStJulians)
	plaque := fixtures.TagKFStJulians
	now := time.Now().UTC()

	arms := []struct {
		name       string
		practiceAt time.Duration
		wasDefect  bool
	}{
		{
			name:       "the training tap sorts BELOW the real check-in",
			practiceAt: -4 * time.Hour,
		},
		{
			// 100 s and not "now": the third record must clear the person-debounce
			// on its DECLARED leg too, and 100 s is comfortably past the 30 s window
			// this harness runs at. The defect needs only "newer than the real 'in'".
			name:       "the training tap sorts ABOVE it — the arm that used to break",
			practiceAt: -100 * time.Second,
			wasDefect:  true,
		},
	}
	for _, a := range arms {
		t.Run(a.name, func(t *testing.T) {
			p := f.hire(t, "Practice Chain", venue.ID, "active")

			practiceAt := now.Add(a.practiceAt)
			f.seedHistoricPractice(t, p.id, venue.ID, plaque, practiceAt)

			realIn := now.Add(-3 * time.Hour)
			entry := f.enterManually(t, p.id, plaque, &realIn)
			if entry.Decision.Practice {
				t.Fatal("the engine wrote a TRAINING record; ADR 0025 retired the practice tap")
			}
			if entry.Decision.Type == nil || *entry.Decision.Type != tap.TypeIn {
				t.Fatalf("the real check-in came out as %v, want in — a training tap must not hold the chain open",
					show(entry.Decision.Type))
			}

			exit := f.enterManually(t, p.id, plaque, nil)
			if exit.Decision.Type == nil || *exit.Decision.Type != tap.TypeOut {
				t.Fatalf("the checkout came out as %v, want out. %s", show(exit.Decision.Type),
					"§5 toggles against the person's LAST OPEN check-in, and a training tap is not one")
			}
			if n := f.openCheckIns(t, p.id); n != 0 {
				t.Fatalf("%d check-in(s) left open, want 0: the real entry never closed", n)
			}

			// The TRAINING row itself survives the fix untouched (M5-07): still
			// practice, still an `in`, and still not counted as an open check-in —
			// openCheckIns above carries `AND NOT t.practice`, so the 0 asserts both
			// that the real entry closed and that the training row is not an anomaly.
			if rows := f.rowsFor(t, p.id); rows != 3 {
				t.Fatalf("%d records for three entries, want 3: every decided tap is recorded (§4.6)", rows)
			}
			if a.wasDefect {
				t.Logf("the defect arm now resolves to `out` with 0 open check-ins " +
					"(before ADR 0008: `in`, 2 open)")
			}
		})
	}
}

// TestSeedDB_ASecondActivationWritesNoRecordAndLeavesTheChainAlone: somebody who
// loses their phone and activates a new one (the second-device path) gets NO record
// for the activating tap and no training row after it (ADR 0025), activated_at does
// not move, and their next tap toggles against the check-in that was already open.
//
// (Before ADR 0025 this measured that a re-activation was not a SECOND practice run:
// the practice rule read "first record ever", so a person with history never got
// one. Now no activation produces one, which is the stronger statement.)
func TestSeedDB_ASecondActivationWritesNoRecordAndLeavesTheChainAlone(t *testing.T) {
	f := newSeedFlow(t)
	venue := f.venue(t, fixtures.LocKFStJulians)
	plaque := fixtures.TagKFStJulians
	now := time.Now().UTC()

	p := f.hire(t, "Lost Phone", venue.ID, "active")

	firstAt := now.Add(-5 * time.Hour)
	first := f.enterManually(t, p.id, plaque, &firstAt)
	if first.Decision.Practice || first.Decision.Type == nil || *first.Decision.Type != tap.TypeIn {
		t.Fatalf("precondition: the first record is an ordinary `in`; practice=%v type=%v",
			first.Decision.Practice, show(first.Decision.Type))
	}

	before := f.activatedAt(t, p.id)
	rowsBefore := f.rowsFor(t, p.id)
	f.reactivate(t, p, plaque)
	if got := f.rowsFor(t, p.id); got != rowsBefore {
		t.Fatalf("the activating tap wrote %d record(s); it is not attendance", got-rowsBefore)
	}
	if after := f.activatedAt(t, p.id); !before.Equal(after) {
		t.Errorf("a second activation moved activated_at from %s to %s; "+
			"ConsumeInviteAndActivate COALESCEs it", before, after)
	}

	secondAt := now.Add(-2 * time.Hour)
	second := f.enterManually(t, p.id, plaque, &secondAt)
	if second.Decision.Practice {
		t.Fatal("the first record after a SECOND activation is marked TRAINING")
	}
	if second.Decision.Type == nil || *second.Decision.Type != tap.TypeOut {
		t.Fatalf("the record after re-activation came out as %v, want out: it closes the check-in "+
			"that was open before the phone changed", show(second.Decision.Type))
	}
	f.assertNoOpenCheckIn(t, p.id)
}

// seedHistoricPractice writes one TRAINING row the way the engine wrote them
// before ADR 0025: an `in`, verdict ok, practice=true, both stamps at `at`.
func (f *seedFlow) seedHistoricPractice(t *testing.T, employeeID, locationID uuid.UUID, plaque string, at time.Time) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, type,
			                           occurred_at, created_at, verdict, channel, sun_valid, trust, practice)
			 VALUES ($1, $2, $3, $4, 'in', $5, $5, 'ok', 'nfc', true, 100, true)`,
			f.tenantID, employeeID, locationID, plaque, at)
		return e
	})
	if err != nil {
		t.Fatalf("seeding a historic practice row: %v", err)
	}
}

// activatedAt reads the employee's activation stamp.
func (f *seedFlow) activatedAt(t *testing.T, employeeID uuid.UUID) time.Time {
	t.Helper()
	var at time.Time
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT activated_at FROM employees WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, employeeID).Scan(&at)
	})
	if err != nil {
		t.Fatalf("read activated_at: %v", err)
	}
	return at
}

// reactivate walks an ALREADY ACTIVE employee through a second activation — the
// "new phone" path: the wizard warns, the consent records, and the activating tap
// on plaque revokes the old sessions before issuing the new one (ADR 0025).
func (f *seedFlow) reactivate(t *testing.T, p *phone, plaque string) {
	t.Helper()
	code := f.inviteCode(t, p)
	status, page := f.openPage(t, p, "/activate?code="+url.QueryEscape(code), seedOffSiteAddr)
	if status != http.StatusOK {
		t.Fatalf("GET /activate status = %d, want 200", status)
	}
	if !strings.Contains(page, "This is a new phone") {
		t.Fatal("a second activation must warn before it signs the other phone out")
	}
	f.consentAndTap(t, p, plaque)
}
