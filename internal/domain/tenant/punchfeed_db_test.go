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

// TestPunchHistory_TheWindowBoundsASparseScope (plan §5 API-7b: "seyrek kapsamda punch_time
// aralığında sınırsız yürüyüş olmaz"): in KF's range, one own tap, seven taps of KM's employee on
// KF's plaque (KF's location scope only) and one more own tap, declared an hour apart. With a window of
// three positions KF's employer walk reads the first tap, then an EMPTY page that still says hasMore,
// then the last tap -- each page touched at most three positions of the range, the position always
// moved forward in (punch_time, seq), and the walk's S0 never changed. The location walk, two at a
// time, reads all nine in punch_time order. A position past the range or past its S0 is
// ErrHistoryRange; an S0 past the counter is ErrCursorAhead.
func TestPunchHistory_TheWindowBoundsASparseScope(t *testing.T) {
	f := newSiteFixture(t)
	feed, err := NewPunchFeed(f.data)
	if err != nil {
		t.Fatal(err)
	}
	feed.window = 3
	venue := f.venue("KF history venue")
	uid := f.plaque(venue)
	kfEmp := uuid.New()
	f.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'KF-HIST', 'KF Staff', 'active', now())`, kfEmp, f.kf)
	t0 := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	own := func(at time.Time) {
		t.Helper()
		if err := f.data.WithTenant(f.ctx, f.kf, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
			                        VALUES ($1, $2, $3, $4, $5, 'ok', 'nfc', 'in')`, f.kf, kfEmp, venue, uid, at)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	own(t0.Add(time.Hour))
	for i := range 7 {
		f.tap(venue, uid, "ok", t0.Add(time.Duration(i+2)*time.Hour))
	}
	own(t0.Add(9 * time.Hour))
	from, to := t0, t0.Add(24*time.Hour)

	type step struct {
		n    int
		next HistoryPosition
		more bool
	}
	walk := func(role FeedRole, limit int) ([]PunchEvent, []step) {
		t.Helper()
		var events []PunchEvent
		var steps []step
		q := HistoryQuery{From: from, To: to, First: true}
		for range 100 {
			p, err := feed.HistoryPage(f.ctx, f.kf, role, q, limit)
			if err != nil {
				t.Fatal(err)
			}
			events = append(events, p.Events...)
			steps = append(steps, step{len(p.Events), p.Next, p.HasMore})
			if !q.First && (p.Next.S0 != q.At.S0 || p.Next.Time.Before(q.At.Time) || (p.Next.Time.Equal(q.At.Time) && p.Next.Seq < q.At.Seq)) {
				t.Fatalf("the position went back or the snapshot moved: %+v after %+v", p.Next, q.At)
			}
			q = HistoryQuery{From: from, To: to, At: p.Next}
			if !p.HasMore {
				return events, steps
			}
		}
		t.Fatal("the walk did not end")
		return nil, nil
	}
	ev, steps := walk(FeedEmployer, 10)
	if len(ev) != 2 || len(steps) != 3 || steps[0].n != 1 || !steps[0].more || steps[1].n != 0 || !steps[1].more || steps[2].n != 1 || steps[2].more {
		t.Fatalf("employer walk: %d events over %+v; want 1, an empty page with hasMore, 1", len(ev), steps)
	}
	if !ev[0].PunchTime.Equal(t0.Add(time.Hour)) || !ev[1].PunchTime.Equal(t0.Add(9*time.Hour)) {
		t.Fatalf("employer walk: %v, %v", ev[0].PunchTime, ev[1].PunchTime)
	}
	ev, _ = walk(FeedLocation, 2)
	for i, e := range ev {
		if len(ev) != 9 || !e.PunchTime.Equal(t0.Add(time.Duration(i+1)*time.Hour)) {
			t.Fatalf("location walk: %d events, the %d-th at %v", len(ev), i, e.PunchTime)
		}
	}
	first, err := feed.HistoryPage(f.ctx, f.kf, FeedLocation, HistoryQuery{From: from, To: to, First: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	s0 := first.Next.S0
	for _, c := range []struct {
		at   HistoryPosition
		want error
	}{
		{HistoryPosition{S0: s0, Time: to}, ErrHistoryRange},
		{HistoryPosition{S0: s0, Time: from.Add(-time.Microsecond)}, ErrHistoryRange},
		{HistoryPosition{S0: s0, Time: from, Seq: s0 + 1}, ErrHistoryRange},
		{HistoryPosition{S0: s0 + 1, Time: from}, ErrCursorAhead},
	} {
		if _, err := feed.HistoryPage(f.ctx, f.kf, FeedLocation, HistoryQuery{From: from, To: to, At: c.at}, 1); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.at, err, c.want)
		}
	}
	for _, r := range [][2]time.Time{{from, from}, {to, from}, {from, from.Add(MaxHistoryRange + time.Microsecond)}} {
		if _, err := feed.HistoryPage(f.ctx, f.kf, FeedLocation, HistoryQuery{From: r[0], To: r[1], First: true}, 1); !errors.Is(err, ErrHistoryRange) {
			t.Errorf("range %v..%v: %v, want ErrHistoryRange", r[0], r[1], err)
		}
	}
}
