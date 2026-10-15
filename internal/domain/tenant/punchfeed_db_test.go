package tenant

// punchfeed_db_test.go -- M11 API-7: the feed reader's window and its cursor across a counter gap
// (ADR 0028 §5), against real Postgres with the shipped triggers. The HTTP contract, the four shapes,
// the outsider and the concurrent readers are internal/handler/api's punches_db_test.go.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestPunchFeed_TheWindowBoundsASparseScopeAndAGapIsCrossed: KF's feed is one own tap, seven taps of
// KM's employee on KF's plaques (KF's location scope only) and one more own tap. With a window of
// three positions, KF's employer scope reads the first tap, then an EMPTY page that still says
// hasMore, then the last tap -- each page touched at most three positions, the cursor always moved
// forward, nothing was skipped. The location scope, two at a time, reads all nine in order (an own tap is in both scopes).
// Then the restore runbook's step (ADR 0028 §5: every counter moved a million ahead): a reader at
// the old end -- or anywhere in the gap -- is caught up AT THE NEW COUNTER (never refused, never
// left behind); the next tap, past the gap, is one page away; a position past the counter is
// ErrCursorAhead.
func TestPunchFeed_TheWindowBoundsASparseScopeAndAGapIsCrossed(t *testing.T) {
	f := newSiteFixture(t)
	feed, err := NewPunchFeed(f.data)
	if err != nil {
		t.Fatal(err)
	}
	feed.window = 3
	venue := f.venue("KF feed venue")
	uid := f.plaque(venue)
	kfEmp := uuid.New()
	f.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'KF-FEED', 'KF Staff', 'active', now())`, kfEmp, f.kf)
	own := func() {
		t.Helper()
		if err := f.data.WithTenant(f.ctx, f.kf, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
			                        VALUES ($1, $2, $3, $4, now(), 'ok', 'nfc', 'in')`, f.kf, kfEmp, venue, uid)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	own()
	for range 7 {
		f.tap(venue, uid, "ok", time.Now())
	}
	own()

	type step struct {
		seqs []int64
		next int64
		more bool
	}
	walk := func(role FeedRole, limit int, from int64) []step {
		t.Helper()
		var out []step
		for range 100 {
			p, err := feed.PunchPage(f.ctx, f.kf, role, from, limit)
			if err != nil {
				t.Fatal(err)
			}
			s := step{next: p.Next, more: p.HasMore}
			for _, e := range p.Events {
				s.seqs = append(s.seqs, e.Seq)
			}
			out = append(out, s)
			if p.Next < from {
				t.Fatalf("the cursor went back: %d after %d", p.Next, from)
			}
			from = p.Next
			if !p.HasMore {
				return out
			}
		}
		t.Fatal("the feed did not end")
		return nil
	}
	got := walk(FeedEmployer, 10, 0)
	want := []step{{[]int64{1}, 3, true}, {nil, 6, true}, {[]int64{9}, 9, false}}
	if len(got) != len(want) {
		t.Fatalf("employer scope: %+v, want %+v", got, want)
	}
	for i := range want {
		if len(got[i].seqs) != len(want[i].seqs) || got[i].next != want[i].next || got[i].more != want[i].more ||
			(len(want[i].seqs) == 1 && got[i].seqs[0] != want[i].seqs[0]) {
			t.Fatalf("employer scope page %d: %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
	var location []int64
	for _, s := range walk(FeedLocation, 2, 0) {
		location = append(location, s.seqs...)
	}
	for i, s := range location {
		if s != int64(i+1) || len(location) != 9 {
			t.Fatalf("location scope: %v, want 1..9 (KF's own taps are in both scopes)", location)
		}
	}

	f.exec(`UPDATE feed_counters SET last_seq = last_seq + 1000000 WHERE tenant_id = $1`, f.kf)
	for _, from := range []int64{9, 500000, 1000009} {
		p, err := feed.PunchPage(f.ctx, f.kf, FeedEmployer, from, 10)
		if err != nil || len(p.Events) != 0 || p.Next != 1000009 || p.HasMore {
			t.Fatalf("from %d past the restore step: %+v %v, want caught up at 1000009", from, p, err)
		}
	}
	own()
	p, err := feed.PunchPage(f.ctx, f.kf, FeedEmployer, 9, 10)
	if err != nil || len(p.Events) != 1 || p.Events[0].Seq != 1000010 || p.Next != 1000010 || p.HasMore {
		t.Fatalf("the tap past the gap: %+v %v", p, err)
	}
	if _, err := feed.PunchPage(f.ctx, f.kf, FeedEmployer, 1000011, 10); !errors.Is(err, ErrCursorAhead) {
		t.Fatalf("a position past the counter: %v, want ErrCursorAhead", err)
	}
	for _, bad := range []struct {
		role  FeedRole
		after int64
		limit int
	}{{"both", 0, 1}, {FeedEmployer, -1, 1}, {FeedEmployer, 0, 0}} {
		if _, err := feed.PunchPage(f.ctx, f.kf, bad.role, bad.after, bad.limit); err == nil || errors.Is(err, ErrCursorAhead) ||
			!strings.HasPrefix(err.Error(), "tenant: ") {
			t.Errorf("%+v: %v, want a refusal", bad, err)
		}
	}
}
