package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"
)

type countingSweeper struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *countingSweeper) SweepIdentityEvents(context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return 3, c.err
}

func (c *countingSweeper) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestIdentitySweep_RunsAtOnceAndEveryPeriodUntilTheContextEnds (T294, ADR 0044 §5.9, card EC-2 item
// 14): the loop sweeps once at start, again on every tick, survives a failed sweep (logged, not
// fatal), and returns when its context ends. MUTATION (measured): the ticker's sweep removed -> red.
func TestIdentitySweep_RunsAtOnceAndEveryPeriodUntilTheContextEnds(t *testing.T) {
	s := &countingSweeper{err: errors.New("db down")}
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sweepIdentityEventsEvery(ctx, s, 20*time.Millisecond, slog.New(slog.NewJSONHandler(&buf, nil)))
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for s.n() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%d sweeps in 5 s with a 20 ms period", s.n())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not return when its context ended")
	}
}

// TestIdentitySweep_RunIsWiredBesideTheDraftSweep (T294 closed by EC-2 item 14): run() starts the
// identity events' sweep on the database, at identityEventSweepPeriod (an hour, the drafts' period),
// next to the registration drafts' sweep. MUTATION (measured): the go statement removed from main.go
// -> red.
func TestIdentitySweep_RunIsWiredBesideTheDraftSweep(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !regexp.MustCompile(`(?m)^\s*go sweepIdentityEventsEvery\(ctx, data, identityEventSweepPeriod, slog\.Default\(\)\)$`).MatchString(src) {
		t.Fatal("run() does not start sweepIdentityEventsEvery on the database at identityEventSweepPeriod")
	}
	if !regexp.MustCompile(`go signupFlow\.SweepEvery\(ctx, time\.Hour\)\s*\n(?:\s*//[^\n]*\n)*\s*go sweepIdentityEventsEvery\(`).MatchString(src) {
		t.Error("the identity sweep is not beside the drafts' sweep")
	}
	if identityEventSweepPeriod != time.Hour {
		t.Errorf("identityEventSweepPeriod = %v, want an hour", identityEventSweepPeriod)
	}
}
