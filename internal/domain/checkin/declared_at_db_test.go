package checkin

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tap"
)

// TestRecordDB_TheOpenCheckInIsReadAtTheDeclaredInstant is the end-to-end half of
// TestGatherDB_TheOpenCheckInIsTheOneOpenAtTheTapsInstant (M15-02, ADR 0032 §A.4): Record hands
// gather its occurredAt -- the DECLARED instant when the client sent one -- not the server's now.
//
// History (written as tappa_app, created_at moved back with occurred_at so neither debounce leg
// fires): an entry 10 h ago, closed 10 min ago. A tap declaring "20 min ago" (the M9-01 queue's
// shape) was made while the entry was still open, so it is the entry's `out` -- flagged by
// base:queued-window (> 120 s), which is the manager's to see, and directed all the same. The
// same history with no declared instant is an `in`: at now the entry is closed. A Record that
// asked at now would answer `in` to both.
func TestRecordDB_TheOpenCheckInIsReadAtTheDeclaredInstant(t *testing.T) {
	w := newHoldingWorld(t)
	seed := func(employee uuid.UUID) {
		t.Helper()
		err := w.data.WithTenant(w.ctx, w.kf, func(ctx context.Context, tx pgx.Tx) error {
			for _, r := range []struct {
				direction string
				ago       string
			}{{"in", "10 hours"}, {"out", "10 minutes"}} {
				if _, e := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at,
				                                                    verdict, channel, type, sun_valid, ctr)
				                         VALUES ($1, $2, $3, $4, now() - $5::interval, now() - $5::interval, 'ok', 'nfc', $6, true, 90)`,
					w.kf, employee, w.kfVenue, w.kfTag, r.ago, r.direction); e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	declared, live := w.employee(w.kf, "active"), w.employee(w.kf, "active")
	seed(declared)
	seed(live)

	tests := []struct {
		name     string
		employee uuid.UUID
		ctr      uint32
		at       *time.Time
		want     tap.Type
	}{
		{"declared 20 min ago: the entry was open then -> out", declared, 101, func() *time.Time { v := time.Now().Add(-20 * time.Minute); return &v }(), tap.TypeOut},
		{"no declared instant: at now the entry is closed -> in", live, 102, nil, tap.TypeIn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := w.nfc(w.kf, tc.employee, w.kfTag, w.kf, tc.ctr, w.kfOnNet)
			req.OccurredAt = tc.at
			res, err := w.svc.Record(w.ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			got := "none"
			if res.Decision.Type != nil {
				got = string(*res.Decision.Type)
			}
			if got != string(tc.want) {
				t.Errorf("direction %s (%s, %s), want %s", got, res.Decision.Verdict, res.Decision.MatchedSid, tc.want)
			}
		})
	}
}
