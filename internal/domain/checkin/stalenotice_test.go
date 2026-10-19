package checkin

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/policy"
)

const staleLine = "checkin: tenant baseline is older than the shipped one; M7-03 owns the upgrade"

// TestPolicySetDB_TheStaleBaselineLineIsOncePerTenant (ADR 0029 follow-up): a tenant
// whose stored baseline predates the shipped stamp is told about it ONCE per process,
// however many taps it makes; a second such tenant once too; a tenant on the shipped
// baseline never. Through forTenant against real Postgres, with the logger
// production's Service builds (staleOnce set as New sets it).
//
// MUTANT (measured): the once-check removed -> 5 lines for five taps, red.
func TestPolicySetDB_TheStaleBaselineLineIsOncePerTenant(t *testing.T) {
	data := openPolicyDB(t)
	var out bytes.Buffer
	sets := policySets{data: data, params: policy.DefaultParams(),
		log: slog.New(slog.NewJSONHandler(&out, nil)), staleOnce: newOncePerTenant(staleNoticeTenants)}
	ctx := context.Background()
	old := func() uuid.UUID {
		id := newPolicyTenant(t, data)
		d := policy.Baseline()[0]
		doc := d.Document
		doc.Version = "2026-07-26"
		seedPolicyDocument(t, data, id, d.Name, doc)
		return id
	}
	a, b, current := old(), old(), newPolicyTenant(t, data)
	for range 5 {
		sets.forTenant(ctx, a)
		sets.forTenant(ctx, current)
	}
	sets.forTenant(ctx, b)
	sets.forTenant(ctx, b)
	lines := map[string]int{}
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.Contains(l, staleLine) {
			for _, id := range []uuid.UUID{a, b, current} {
				if strings.Contains(l, id.String()) {
					lines[id.String()]++
				}
			}
		}
	}
	if lines[a.String()] != 1 || lines[b.String()] != 1 || lines[current.String()] != 0 {
		t.Fatalf("stale-baseline lines: first old tenant %d (5 taps), second %d (2 taps), current %d; want 1, 1, 0",
			lines[a.String()], lines[b.String()], lines[current.String()])
	}
}

// TestOncePerTenant_IsBoundedAndSafeUnderConcurrency: 64 goroutines asking about the
// same tenant get exactly one "first"; the memory holds at most max tenants and the
// oldest is forgotten first (asked again, it is "first" again -- a repeated line, never
// a lost one); a nil set answers "first" every time.
func TestOncePerTenant_IsBoundedAndSafeUnderConcurrency(t *testing.T) {
	o := newOncePerTenant(3)
	id := uuid.New()
	var wg sync.WaitGroup
	var mu sync.Mutex
	firsts := 0
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if o.first(id) {
				mu.Lock()
				firsts++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if firsts != 1 {
		t.Fatalf("%d goroutines were told they were first, want 1", firsts)
	}
	others := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for _, x := range others {
		o.first(x)
	}
	if len(o.seen) != 3 || len(o.order) != 3 {
		t.Fatalf("holds %d / %d tenants, want the bound 3", len(o.seen), len(o.order))
	}
	if !o.first(id) {
		t.Error("the oldest tenant was not forgotten past the bound")
	}
	if o.first(others[2]) {
		t.Error("a recent tenant was forgotten")
	}
	var none *oncePerTenant
	for range 2 {
		if !none.first(id) {
			t.Error("a nil set must answer first every time")
		}
	}
}
