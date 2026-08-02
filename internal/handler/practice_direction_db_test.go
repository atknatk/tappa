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
	"github.com/atknatk/tappa/internal/invite"
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
			training := f.enterManually(t, p.id, plaque, &practiceAt)
			if !training.Decision.Practice {
				t.Fatalf("precondition: the person's first record must be the TRAINING tap, practice = false")
			}
			if training.Decision.Type == nil || *training.Decision.Type != tap.TypeIn {
				t.Fatalf("precondition: a practice record is always an `in`, got %v", show(training.Decision.Type))
			}

			realIn := now.Add(-3 * time.Hour)
			entry := f.enterManually(t, p.id, plaque, &realIn)
			if entry.Decision.Practice {
				t.Fatal("the SECOND record is still marked TRAINING: the practice run is spent by any prior row")
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

// TestSeedDB_ASecondActivationIsNotASecondPracticeRun measures the sentence the
// M5-11 card got wrong, and it is here rather than in the card's history because
// the card's "real-life scenario" made the defect look reachable by a route it is
// not reachable by.
//
// The card said: employee checks in -> loses the phone -> re-activates on a new one
// -> "the first record after activation is practice=true by definition", so the
// checkout is a training tap and the entry never closes. MEASURED: it is not. The
// practice run is spent by the person's FIRST RECORD EVER, not by each activation.
// Two independent reasons, either alone sufficient:
//
//	tap.isPracticeTap requires LastForPerson == nil, and a re-activated employee
//	has a history.
//	ConsumeInviteAndActivate COALESCEs activated_at, so a second activation does
//	not even move the stamp the rule reads.
//
// This does NOT make the defect theoretical — it was reachable over plain HTTP with
// one back-dated occurred_at, which is what the test above pins. It makes the CARD'S
// ROUTE wrong, and a wrong route in a defect report is how a fix ends up guarding
// the thing that was never the problem.
func TestSeedDB_ASecondActivationIsNotASecondPracticeRun(t *testing.T) {
	f := newSeedFlow(t)
	venue := f.venue(t, fixtures.LocKFStJulians)
	plaque := fixtures.TagKFStJulians
	now := time.Now().UTC()

	p := f.hire(t, "Lost Phone", venue.ID, "active")

	firstAt := now.Add(-5 * time.Hour)
	first := f.enterManually(t, p.id, plaque, &firstAt)
	if !first.Decision.Practice {
		t.Fatal("precondition: the person's first record must be the TRAINING tap")
	}

	before := f.activatedAt(t, p.id)
	f.reactivate(t, p)
	after := f.activatedAt(t, p.id)
	if !before.Equal(after) {
		t.Errorf("a second activation moved activated_at from %s to %s; "+
			"ConsumeInviteAndActivate COALESCEs it precisely so the practice rule's input is stable",
			before, after)
	}

	secondAt := now.Add(-2 * time.Hour)
	second := f.enterManually(t, p.id, plaque, &secondAt)
	if second.Decision.Practice {
		t.Fatal("the first record after a SECOND activation is marked TRAINING. That would be a " +
			"second unpaid tap for somebody who only changed phones, and it would put a practice " +
			"row on top of an open check-in — the exact shape ADR 0008 is about.")
	}
	// And it is an `in`, not an `out`: the training tap five hours ago is the only
	// earlier row and a training tap is NOT an open check-in (§5, M5-07). That
	// behaviour is unchanged by ADR 0008 and is asserted here so the fix cannot
	// quietly turn practice rows back into chain-holding ones.
	if second.Decision.Type == nil || *second.Decision.Type != tap.TypeIn {
		t.Fatalf("the record after re-activation came out as %v, want in: the only earlier row is "+
			"a training tap, and a training tap never holds the chain open", show(second.Decision.Type))
	}
	if n := f.openCheckIns(t, p.id); n != 1 {
		t.Fatalf("%d open check-in(s), want exactly 1 (the record just written); a training row "+
			"must not be counted as one", n)
	}
}

// activatedAt reads the employee's activation stamp — the other input to §5's
// practice rule.
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
// "new phone" path M5-02 shipped, which revokes the old sessions before issuing the
// new one and lands on the confirmation rather than the tour.
func (f *seedFlow) reactivate(t *testing.T, p *phone) {
	t.Helper()
	ch := &linkChannel{}
	if _, err := f.invites.IssueAndDeliver(context.Background(), invite.IssueParams{
		TenantID: f.tenantID, EmployeeID: p.id,
	}, ch); err != nil {
		t.Fatalf("IssueAndDeliver for %s: %v", p.name, err)
	}
	link, err := url.Parse(ch.url)
	if err != nil {
		t.Fatalf("activation url: %v", err)
	}
	code := link.Query().Get("code")
	if code == "" {
		t.Fatal("the delivered link carries no code")
	}
	status, page := f.openPage(t, p, "/activate?code="+url.QueryEscape(code), seedOffSiteAddr)
	if status != http.StatusOK {
		t.Fatalf("GET /activate status = %d, want 200", status)
	}
	req, err := http.NewRequest(http.MethodPost, f.server.URL+"/api/activate",
		strings.NewReader(url.Values{"consent": {"yes"}, "csrf": {formToken(t, page)}}.Encode()))
	if err != nil {
		t.Fatalf("build activate POST: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("POST /api/activate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-activation status = %d, want 200", resp.StatusCode)
	}
	// A second device skips the tour on purpose (M5-02): the tour's words are about
	// a first tap, and this person has already had theirs.
	if resp.Request.URL.Path != "/activate/done" {
		t.Fatalf("a second activation landed on %s, want /activate/done", resp.Request.URL.Path)
	}
}
