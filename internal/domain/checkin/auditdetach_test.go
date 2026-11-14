package checkin

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
)

type liveCtxTrail struct{ n int }

func (r *liveCtxTrail) Record(ctx context.Context, _ audit.Event) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	r.n++
	return uuid.New(), nil
}

// TestRecord_TheTrailRowSurvivesTheTapperLeaving (T284 (a), user decision 2026-11-12): the tap's
// audit row is written under audit.Detached, so a tap whose request context is already done
// still leaves its row (the attendance record itself is written before this runs).
func TestRecord_TheTrailRowSurvivesTheTapperLeaving(t *testing.T) {
	t.Parallel()
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	trail := &liveCtxTrail{}
	if _, err := trail.Record(gone, audit.Event{}); err == nil {
		t.Fatal("CONTROL FAILED: the fake writes under a cancelled context")
	}
	s := &Service{trail: trail, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.record(gone, audit.Event{TenantID: uuid.New(), Action: "tap.test"})
	if trail.n != 1 {
		t.Fatalf("%d row(s) under a cancelled request context, want 1", trail.n)
	}
}
