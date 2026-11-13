package main

import (
	"context"
	"log/slog"
	"time"
)

// identityEventSweepPeriod is how often identity_sweep_events runs: the registration drafts' period
// (signupFlow.SweepEvery), so the two fixed-period sweeps ADR 0031 §6.1 counts run side by side.
const identityEventSweepPeriod = time.Hour

// identityEventSweeper is what the sweep needs, declared at the consumer (§7). *db.DB is it.
type identityEventSweeper interface {
	SweepIdentityEvents(ctx context.Context) (int64, error)
}

// sweepIdentityEventsEvery runs identity_sweep_events every period until ctx ends (T294; ADR 0044 §5.9,
// ADR 0031 R17): the event log's 90 days and the address change links one day after they ended. It
// sweeps once at once too -- a process that restarts more often than the period would otherwise never
// sweep. A failed sweep is logged and the next tick tries again; the line carries the count of events
// deleted and nothing else.
func sweepIdentityEventsEvery(ctx context.Context, s identityEventSweeper, period time.Duration, log *slog.Logger) {
	sweep := func() {
		n, err := s.SweepIdentityEvents(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.ErrorContext(ctx, "identity: sweeping the identity events failed", "err", err)
			}
			return
		}
		log.InfoContext(ctx, "identity: identity events swept", "deleted", n)
	}
	sweep()
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}
