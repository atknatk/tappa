package claim

// open_db_test.go -- M16-43 against real Postgres: the app's "Missing times" list (Open), the
// still-open rule under Submit's lock (Y1), and the person's own claims (Sent).

import (
	"errors"
	"testing"
	"time"
)

// TestOpenDB_TheStillOpenCheckInTakesNoClaim ([G43-k]): a 09:00 check-in still open at 12:00 is not
// listed and Submit refuses it with ErrStillOpen, writing nothing; once 18 h have passed it is
// listed and takes a claim; a check-in a later tap closed is never listed.
func TestOpenDB_TheStillOpenCheckInTakesNoClaim(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(1, 9, 0), w.home)
	w.svc.now = func() time.Time { return w.at(1, 12, 0) }
	cards, err := w.svc.Open(w.ctx, w.tenant, w.who)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Fatalf("at 12:00 the open check-in is listed: %+v", cards)
	}
	if _, err := w.submit(in, KindOut, w.at(1, 11, 0)); !errors.Is(err, ErrStillOpen) {
		t.Fatalf("submit on the still-open check-in: %v, want ErrStillOpen", err)
	}
	if n := w.claims(); n != 0 {
		t.Fatalf("%d claims after ErrStillOpen, want 0", n)
	}
	if n := w.trailRows(ActionSubmitted); n != 0 {
		t.Fatalf("%d claim.submitted rows after ErrStillOpen, want 0", n)
	}

	// 18 h and a minute later the check-in is stale: the next tap would be an `in`.
	w.svc.now = func() time.Time { return w.at(2, 3, 1) }
	cards, err = w.svc.Open(w.ctx, w.tenant, w.who)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].AnchorID != in || cards[0].Kind != KindOut || cards[0].Zone != "UTC" {
		t.Fatalf("after 18 h: %+v, want the check-in as one clock-out card", cards)
	}
	if cards[0].Prefilled == nil || !cards[0].Prefilled.Equal(w.at(1, 17, 0)) {
		t.Fatalf("prefill %v, want the shift's 17:00 (Offer's Prefill)", cards[0].Prefilled)
	}
	w.mustSubmit(in, KindOut, w.at(1, 17, 0))
	cards, err = w.svc.Open(w.ctx, w.tenant, w.who)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 0 {
		t.Fatalf("after the claim the record is still listed: %+v", cards)
	}

	// A check-in a later tap closed misses nothing.
	w2 := newWorld(t)
	w2.tapAt("in", w2.at(1, 9, 0), w2.home)
	w2.tapAt("out", w2.at(1, 18, 0), w2.home)
	w2.svc.now = func() time.Time { return w2.at(2, 12, 0) }
	if cards, err := w2.svc.Open(w2.ctx, w2.tenant, w2.who); err != nil || len(cards) != 0 {
		t.Fatalf("a closed pair: %+v, %v; want nothing", cards, err)
	}
}

// TestOpenDB_TheListIgnoresK4AndLooksBackAWeek ([G43-f]): a check-in with no checkout followed by a
// later day's pair is listed (however often the result screen asked); one older than 7 days is
// not; a person who is not active gets nothing.
func TestOpenDB_TheListIgnoresK4AndLooksBackAWeek(t *testing.T) {
	w := newWorld(t)
	old := w.tapAt("in", w.at(0, 9, 0), w.home)
	missed := w.tapAt("in", w.at(8, 9, 0), w.home)
	w.tapAt("in", w.at(9, 9, 0), w.home)
	w.tapAt("out", w.at(9, 17, 0), w.home)
	w.tapAt("in", w.at(10, 9, 0), w.home)
	w.tapAt("out", w.at(10, 17, 0), w.home)
	w.svc.now = func() time.Time { return w.at(11, 12, 0) }
	cards, err := w.svc.Open(w.ctx, w.tenant, w.who)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].AnchorID != missed {
		t.Fatalf("cards %+v, want only the day-8 check-in (day 0's is %v, older than a week)", cards, old)
	}
	w.exec(`UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE tenant_id = $1 AND id = $2`, w.tenant, w.who)
	if cards, err := w.svc.Open(w.ctx, w.tenant, w.who); err != nil || len(cards) != 0 {
		t.Fatalf("deactivated: %+v, %v; want nothing", cards, err)
	}
}

// TestOpenDB_SentShowsTheDecisionAndTheHourOnlyWhenAllowed ([G43-l]): a sent claim is pending, then
// approved; the approved hour is carried only when the caller shows hours.
func TestOpenDB_SentShowsTheDecisionAndTheHourOnlyWhenAllowed(t *testing.T) {
	w := newWorld(t)
	in := w.tapAt("in", w.at(1, 9, 0), w.home)
	w.tapAt("in", w.at(2, 9, 0), w.home)
	w.tapAt("out", w.at(2, 17, 0), w.home)
	sub := w.mustSubmit(in, KindOut, w.at(1, 17, 0))
	sent, zone, err := w.svc.Sent(w.ctx, w.tenant, w.who, true)
	if err != nil {
		t.Fatal(err)
	}
	if zone != "UTC" || len(sent) != 1 || sent[0].State != SentPending || sent[0].Kind != KindOut ||
		!sent[0].AnchorAt.Equal(w.at(1, 9, 0)) || !sent[0].ClaimedAt.Equal(w.at(1, 17, 0)) || sent[0].ApprovedAt != nil {
		t.Fatalf("sent %+v zone %q", sent, zone)
	}
	if _, err := w.decide(sub.ID, Approved, nil, true); err != nil {
		t.Fatal(err)
	}
	sent, _, err = w.svc.Sent(w.ctx, w.tenant, w.who, false)
	if err != nil {
		t.Fatal(err)
	}
	if sent[0].State != SentApproved || sent[0].ApprovedAt != nil {
		t.Fatalf("hours hidden: %+v, want approved with no hour", sent[0])
	}
	sent, _, err = w.svc.Sent(w.ctx, w.tenant, w.who, true)
	if err != nil {
		t.Fatal(err)
	}
	if sent[0].ApprovedAt == nil || !sent[0].ApprovedAt.Equal(w.at(1, 17, 0)) {
		t.Fatalf("hours shown: %+v, want the approved 17:00", sent[0])
	}
}
