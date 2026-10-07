package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/mail"
)

// --- fakes for the outbox --------------------------------------------------------

// gateChannel holds every send until release is closed (or the send's context
// ends, like the real transport). started receives once per call, so a test knows
// the worker is inside a send.
type gateChannel struct {
	release chan struct{}
	started chan ResetDelivery

	mu        sync.Mutex
	delivered []ResetDelivery
	ctxErrs   []error
	notices   int
}

func newGateChannel() *gateChannel {
	return &gateChannel{release: make(chan struct{}), started: make(chan ResetDelivery, 64)}
}

func (c *gateChannel) DeliverReset(ctx context.Context, d ResetDelivery) error {
	c.started <- d
	select {
	case <-c.release:
	case <-ctx.Done():
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.delivered = append(c.delivered, d)
	c.ctxErrs = append(c.ctxErrs, ctx.Err())
	return ctx.Err()
}

// DeliverPasswordNotice is not this fake's subject: the notice never goes through the
// outbox (M10 EM-9), so it answers at once and counts the call.
func (c *gateChannel) DeliverPasswordNotice(context.Context, PasswordNotice) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notices++
	return nil
}

func (c *gateChannel) snapshot() ([]ResetDelivery, []error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ResetDelivery(nil), c.delivered...), append([]error(nil), c.ctxErrs...)
}

// waitStarted fails the test if the worker has not entered a send within the limit.
func (c *gateChannel) waitStarted(t *testing.T) ResetDelivery {
	t.Helper()
	select {
	case d := <-c.started:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never entered a send")
		return ResetDelivery{}
	}
}

// panicChannel panics on the calls whose 1-based number is in panicOn, with a value
// that carries the recipient and the link — the shape of a panic a template or a
// transport could raise mid-message — and succeeds otherwise.
type panicChannel struct {
	panicOn map[int]bool

	mu    sync.Mutex
	calls int
	sent  []ResetDelivery
}

func (c *panicChannel) DeliverReset(_ context.Context, d ResetDelivery) error {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()
	if c.panicOn[n] {
		panic("composing for " + d.Recipient + " " + d.Link)
	}
	c.mu.Lock()
	c.sent = append(c.sent, d)
	c.mu.Unlock()
	return nil
}

// DeliverPasswordNotice panics like DeliverReset does, on the same call numbering,
// with a value carrying the recipient — the notice's own panic test drives it.
func (c *panicChannel) DeliverPasswordNotice(_ context.Context, n PasswordNotice) error {
	c.mu.Lock()
	c.calls++
	k := c.calls
	c.mu.Unlock()
	if c.panicOn[k] {
		panic("composing the notice for " + n.Recipient)
	}
	return nil
}

// ctxTrail is fakeTrail that refuses a write whose context has already ended — what
// a real database does — and can hold each write for a while, honouring the
// context like a real round trip.
type ctxTrail struct {
	fakeTrail
	hold    time.Duration
	entered chan struct{} // optional: receives (without blocking) when a write starts
}

func (f *ctxTrail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
	}
	if f.hold > 0 {
		t := time.NewTimer(f.hold)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
		}
	}
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	return f.fakeTrail.Record(ctx, e)
}

// panickyTrail records every event and then panics on the first one — the shape of
// an audit writer that fails AFTER the row exists.
type panickyTrail struct {
	fakeTrail
	once sync.Once
}

func (f *panickyTrail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	id, err := f.fakeTrail.Record(ctx, e)
	f.once.Do(func() { panic("audit writer fault after the insert") })
	return id, err
}

// rowsPerReset counts the outcome rows (requested / undelivered) per reset id.
func rowsPerReset(t *testing.T, events []audit.Event) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, e := range events {
		if e.Action != ActionAdminResetRequested && e.Action != ActionAdminResetUndelivered {
			continue
		}
		d, ok := e.Detail.(adminResetDetail)
		if !ok {
			t.Fatalf("detail is %T, want adminResetDetail", e.Detail)
		}
		out[d.ResetID]++
	}
	return out
}

// reasonsOf lists the undelivered rows' reasons, sorted.
func reasonsOf(events []audit.Event) []string {
	var out []string
	for _, e := range events {
		if e.Action != ActionAdminResetUndelivered {
			continue
		}
		if d, ok := e.Detail.(adminResetDetail); ok {
			out = append(out, d.Reason)
		}
	}
	sort.Strings(out)
	return out
}

// grantsFor makes n grants with distinct reset ids, distinct administrators and
// distinct tokens (so distinct links), so the per-account audit budget never decides
// a row in a test that is not about it and each link can be told apart.
func grantsFor(recipient string, n int) []adminauth.ResetGrant {
	out := make([]adminauth.ResetGrant, n)
	for i := range out {
		out[i] = grantFor(recipient)
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			panic(err)
		}
		out[i].Issued.Token = adminauth.ParseResetToken(base64.RawURLEncoding.EncodeToString(raw))
	}
	return out
}

// newOutboxFlow is newResetRouter with a trail of the caller's choosing and a logger
// over a buffer.
func newOutboxFlow(t *testing.T, resets panelResets, ch ResetChannel, trail auditRecorder, logged *bytes.Buffer) (http.Handler, *AdminReset) {
	t.Helper()
	h, err := NewAdminReset(resets, ch, trail, adminTestConfig(),
		slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	stopWorkerAtCleanup(t, h)
	r := chi.NewRouter()
	h.Mount(r)
	return r, h
}

// --- acceptance 1: the response does not wait for the relay ----------------------

// TestAdminReset_TimingIsFlatWhileTheRelayTakesTwoSeconds is ADR 0022 İddia E's
// EM-5 measurement: with the channel taking TWO SECONDS per send, the registered and
// the unregistered medians both sit in [resetRequestFloor, resetRequestFloor+50ms].
//
// 🔴 THE CONTROL DRIVES THE SYNCHRONOUS PATH THIS TEST EXISTS TO RULE OUT: h.deliver,
// called directly with the same two-second channel, takes at least two seconds — far
// outside the band. So a band this test accepts is one the send is NOT in; before
// EM-5 Request called exactly that function inline, and the M10 EM-5 card records
// the mutation that puts it back and turns this test red.
//
// NOT PARALLEL: it measures the clock, and the band is 50 ms wide.
func TestAdminReset_TimingIsFlatWhileTheRelayTakesTwoSeconds(t *testing.T) {
	const relay = 2 * time.Second
	known := "slow-relay@registered.example.test"
	resets := &fakeResets{
		grantsFor:  map[string][]adminauth.ResetGrant{known: {grantFor(known)}},
		issueDelay: 40 * time.Millisecond,
	}
	trail := &fakeTrail{}
	ch := &recordingChannel{delay: relay}
	router, h := newResetRouter(t, resets, ch, trail)

	const samples = 5
	var hit, miss []time.Duration
	for i := 0; i < samples; i++ {
		hit = append(hit, timeOneRequest(t, router, known))
		miss = append(miss, timeOneRequest(t, router, "nobody@unregistered.example.test"))
	}
	sort.Slice(hit, func(i, j int) bool { return hit[i] < hit[j] })
	sort.Slice(miss, func(i, j int) bool { return miss[i] < miss[j] })
	mHit, mMiss := hit[samples/2], miss[samples/2]
	ceiling := resetRequestFloor + 50*time.Millisecond
	t.Logf("relay %v: median registered %v, unregistered %v; band [%v, %v]", relay, mHit, mMiss, resetRequestFloor, ceiling)
	for name, m := range map[string]time.Duration{"registered": mHit, "unregistered": mMiss} {
		if m < resetRequestFloor || m > ceiling {
			t.Errorf("the %s median is %v, outside [%v, %v]: with a %v relay the response is waiting "+
				"for the send, or the floor is gone", name, m, resetRequestFloor, ceiling, relay)
		}
	}

	// ANTI-VACUITY: the registered arm really minted and handed over every time —
	// five grants, each ending in exactly one row once the outbox is drained (the
	// drain cancels what the two-second relay has not finished).
	ctx, cancel := context.WithTimeout(context.Background(), ResetDrainWriteReserve+500*time.Millisecond)
	defer cancel()
	if err := h.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	rows := rowsPerReset(t, trail.eventsSnapshot())
	if len(ch.all()) == 0 || len(rows) != 1 {
		// One grantFor value is reused by the fake for every registered request, so
		// the five grants share one reset id: five rows under that one id.
		t.Fatalf("the channel saw %d send(s) and the trail holds rows for %d reset id(s); the "+
			"registered arm did not reach the outbox", len(ch.all()), len(rows))
	}
	for id, n := range rows {
		if n != samples {
			t.Errorf("reset %s: %d row(s), want %d (one per registered request)", id, n, samples)
		}
	}

	// THE CONTROL: the synchronous path, timed with the same relay.
	inline, err := NewAdminReset(&fakeResets{}, &recordingChannel{delay: relay}, &fakeTrail{}, adminTestConfig(),
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	stopWorkerAtCleanup(t, inline)
	var decided bool
	start := time.Now()
	inline.deliver(context.Background(), "203.0.113.9", grantFor(known), &decided)
	if took := time.Since(start); took <= ceiling {
		t.Fatalf("CONTROL FAILED: the synchronous send took %v, inside the band (<= %v), so the band "+
			"cannot tell a request that waits for the relay from one that does not", took, ceiling)
	}
}

// --- acceptance 2, 3, 5: one row per grant, on every path ------------------------

// TestResetOutbox_EveryGrantInsideTheBudgetEndsInExactlyOneRow is ADR 0022 İddia F's
// EM-5 measurement: on each of the five paths a grant can take — sent, send failed,
// outbox full or closed, worker panic, shutdown drain — a grant INSIDE its account's
// audit budget ends in exactly ONE outcome row, and a panic after the row adds none.
// The last case is the name's other half: past the budget a grant has NO row of its
// own — the budget's rows and one rate_limited row are all there is (§6.5, by design).
func TestResetOutbox_EveryGrantInsideTheBudgetEndsInExactlyOneRow(t *testing.T) {
	t.Parallel()
	const who = "rows@registered.example.test"

	request := func(t *testing.T, router http.Handler) {
		t.Helper()
		if body := requestReset(t, newBrowser(t, router), who); !strings.Contains(body, "Check your email") {
			t.Fatalf("the request did not answer with the deliverable page:\n%s", body)
		}
	}
	exactlyOne := func(t *testing.T, grants []adminauth.ResetGrant, events []audit.Event) {
		t.Helper()
		rows := rowsPerReset(t, events)
		for _, g := range grants {
			if n := rows[g.Issued.Reset.ID.String()]; n != 1 {
				t.Errorf("grant %s ended with %d row(s), want exactly 1", g.Issued.Reset.ID, n)
			}
		}
		if len(rows) != len(grants) {
			t.Errorf("rows for %d reset id(s), want %d", len(rows), len(grants))
		}
	}

	t.Run("sent: three accounts behind one address, three requested rows", func(t *testing.T) {
		t.Parallel()
		grants := grantsFor(who, 3)
		trail := &fakeTrail{}
		ch := &recordingChannel{}
		router, h := newResetRouter(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}}, ch, trail)
		request(t, router)
		drain(t, h)
		exactlyOne(t, grants, trail.eventsSnapshot())
		if n := trail.count(ActionAdminResetRequested); n != 3 {
			t.Errorf("%d requested row(s), want 3", n)
		}
		if n := len(ch.all()); n != 3 {
			t.Errorf("the channel received %d link(s), want 3 — one per account", n)
		}
	})

	t.Run("send failed: one undelivered row", func(t *testing.T) {
		t.Parallel()
		grants := grantsFor(who, 1)
		trail := &fakeTrail{}
		router, h := newResetRouter(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}},
			&recordingChannel{err: errors.New("relay said no")}, trail)
		request(t, router)
		drain(t, h)
		exactlyOne(t, grants, trail.eventsSnapshot())
		if got := reasonsOf(trail.eventsSnapshot()); len(got) != 1 || got[0] != resetReasonSendFailed {
			t.Errorf("undelivered reasons %q, want [%q]", got, resetReasonSendFailed)
		}
	})

	t.Run("outbox full: the overflow is recorded at once and never sent", func(t *testing.T) {
		t.Parallel()
		first := grantsFor(who, 1)
		rest := grantsFor("rest@registered.example.test", resetOutboxSize+1)
		trail := &fakeTrail{}
		ch := newGateChannel()
		router, h := newResetRouter(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{
			who: first, "rest@registered.example.test": rest,
		}}, ch, trail)
		request(t, router)
		ch.waitStarted(t) // the worker holds the first grant; the outbox is empty
		if body := requestReset(t, newBrowser(t, router), "rest@registered.example.test"); !strings.Contains(body, "Check your email") {
			t.Fatalf("the overflowing request did not answer with the deliverable page")
		}
		close(ch.release)
		drain(t, h)
		all := append(append([]adminauth.ResetGrant{}, first...), rest...)
		exactlyOne(t, all, trail.eventsSnapshot())
		if got := reasonsOf(trail.eventsSnapshot()); len(got) != 1 || got[0] != resetReasonOutboxFull {
			t.Errorf("undelivered reasons %q, want exactly one %q", got, resetReasonOutboxFull)
		}
		delivered, _ := ch.snapshot()
		if len(delivered) != 1+resetOutboxSize {
			t.Errorf("the channel received %d send(s), want %d (the one in flight and a full outbox)",
				len(delivered), 1+resetOutboxSize)
		}
	})

	t.Run("outbox closed for shutdown: recorded at once, never sent", func(t *testing.T) {
		t.Parallel()
		grants := grantsFor(who, 2)
		trail := &fakeTrail{}
		ch := &recordingChannel{}
		router, h := newResetRouter(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}}, ch, trail)
		drain(t, h) // the drain has begun: the outbox takes nothing more
		request(t, router)
		exactlyOne(t, grants, trail.eventsSnapshot())
		if got := reasonsOf(trail.eventsSnapshot()); len(got) != 2 || got[0] != resetReasonOutboxShut {
			t.Errorf("undelivered reasons %q, want two %q", got, resetReasonOutboxShut)
		}
		if n := len(ch.all()); n != 0 {
			t.Errorf("the channel received %d link(s) after the drain began, want 0", n)
		}
	})

	t.Run("worker panic before the row: undelivered", func(t *testing.T) {
		t.Parallel()
		grants := grantsFor(who, 1)
		trail := &fakeTrail{}
		router, h := newResetRouter(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}},
			&panicChannel{panicOn: map[int]bool{1: true}}, trail)
		request(t, router)
		drain(t, h)
		exactlyOne(t, grants, trail.eventsSnapshot())
		if got := reasonsOf(trail.eventsSnapshot()); len(got) != 1 || got[0] != resetReasonWorkerFault {
			t.Errorf("undelivered reasons %q, want [%q]", got, resetReasonWorkerFault)
		}
	})

	t.Run("worker panic after the row: no second row", func(t *testing.T) {
		t.Parallel()
		grants := grantsFor(who, 2)
		trail := &panickyTrail{}
		var logged bytes.Buffer
		router, h := newOutboxFlow(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}},
			&recordingChannel{}, trail, &logged)
		request(t, router)
		drain(t, h)
		exactlyOne(t, grants, trail.eventsSnapshot())
		if n := trail.count(ActionAdminResetRequested); n != 2 {
			t.Errorf("%d requested row(s), want 2: the first grant's row was written before the "+
				"writer panicked, and the worker went on to the second", n)
		}
		if !strings.Contains(logged.String(), "worker recovered") {
			t.Errorf("the panic left no line:\n%s", logged.String())
		}
	})

	t.Run("shutdown drain: in flight and queued end undelivered, unsent", func(t *testing.T) {
		t.Parallel()
		grants := grantsFor(who, 3)
		// A trail that refuses a write on an ended context, as Postgres does: the row
		// of the send the drain cancelled must be written on a context of its OWN
		// (ADR 0022 §6.3), not on the send's.
		trail := &ctxTrail{}
		ch := newGateChannel()
		var logged bytes.Buffer
		router, h := newOutboxFlow(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}}, ch, trail, &logged)
		request(t, router)
		ch.waitStarted(t)
		// A budget just over the reserve: the sends stop almost at once and the three
		// rows are written inside what is left.
		ctx, cancel := context.WithTimeout(context.Background(), ResetDrainWriteReserve+200*time.Millisecond)
		defer cancel()
		if err := h.Drain(ctx); err != nil {
			t.Fatalf("drain: %v", err)
		}
		exactlyOne(t, grants, trail.eventsSnapshot())
		want := []string{resetReasonDrainEnded, resetReasonDrainEnded, resetReasonSendFailed}
		sort.Strings(want)
		if got := reasonsOf(trail.eventsSnapshot()); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("undelivered reasons %q, want %q", got, want)
		}
		if delivered, errs := ch.snapshot(); len(delivered) != 1 || errs[0] == nil {
			t.Errorf("the channel saw %d send(s) (want 1, the one in flight) and it ended with %v "+
				"(want its context cancelled by the drain)", len(delivered), errs)
		}
	})

	t.Run("one account past its audit budget: the budget holds", func(t *testing.T) {
		t.Parallel()
		one := grantFor(who)
		var grants []adminauth.ResetGrant
		for i := 0; i < adminResetAccountLimit+2; i++ {
			g := one
			g.Issued.Reset.ID = uuid.New()
			grants = append(grants, g)
		}
		trail := &fakeTrail{}
		router, h := newResetRouter(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}},
			&recordingChannel{err: errors.New("relay said no")}, trail)
		request(t, router)
		drain(t, h)
		if n := trail.count(ActionAdminResetUndelivered); n != adminResetAccountLimit {
			t.Errorf("%d undelivered row(s) for one administrator, want the budget (%d)", n, adminResetAccountLimit)
		}
		if n := trail.count(ActionAdminResetLimited); n != 1 {
			t.Errorf("%d rate_limited row(s), want 1", n)
		}
		if rows := rowsPerReset(t, trail.eventsSnapshot()); len(rows) != adminResetAccountLimit {
			t.Errorf("rows for %d grant(s), want %d — the two past the budget have none of their own",
				len(rows), adminResetAccountLimit)
		}
	})
}

// TestAdminReset_AFullOutboxRecordsTheGrantAtOnceAndDoesNotWait is ADR 0022 §6.1's
// fallback: when the outbox will not take a grant, the REQUEST writes its undelivered
// row — before the response returns — sends nothing for it, and still answers at the
// floor rather than waiting for room.
func TestAdminReset_AFullOutboxRecordsTheGrantAtOnceAndDoesNotWait(t *testing.T) {
	const first, filler, late = "first@registered.example.test", "filler@registered.example.test", "late@registered.example.test"
	lateGrant := grantFor(late)
	trail := &fakeTrail{}
	ch := newGateChannel()
	router, h := newResetRouter(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{
		first: grantsFor(first, 1), filler: grantsFor(filler, resetOutboxSize), late: {lateGrant},
	}}, ch, trail)

	// One grant into the worker's hands, THEN exactly resetOutboxSize behind it: the
	// order makes "full" deterministic rather than a race with the worker's dequeue.
	requestReset(t, newBrowser(t, router), first)
	ch.waitStarted(t)
	requestReset(t, newBrowser(t, router), filler)
	if n := trail.total(); n != 0 {
		t.Fatalf("%d row(s) before the overflow; the filler did not fit, so 'full' is not what is measured", n)
	}

	took := timeOneRequest(t, router, late)
	// Read BEFORE any drain: the row must already be there.
	rows := rowsPerReset(t, trail.eventsSnapshot())
	if n := rows[lateGrant.Issued.Reset.ID.String()]; n != 1 {
		t.Errorf("the overflowing grant has %d row(s) when its response returns, want 1", n)
	}
	if took > resetRequestFloor+50*time.Millisecond {
		t.Errorf("the request answered in %v with the outbox full; it waited for room (floor %v)", took, resetRequestFloor)
	}
	close(ch.release)
	drain(t, h)
	delivered, _ := ch.snapshot()
	for _, d := range delivered {
		if d.ResetID == lateGrant.Issued.Reset.ID {
			t.Error("the overflowing grant was sent after all; a grant recorded undelivered must not be")
		}
	}
	if rows := rowsPerReset(t, trail.eventsSnapshot()); rows[lateGrant.Issued.Reset.ID.String()] != 1 {
		t.Errorf("the overflowing grant ended with %d row(s), want exactly 1", rows[lateGrant.Issued.Reset.ID.String()])
	}
}

// TestResetOutbox_APanickingSendBecomesUndeliveredAndTheWorkerLives: the panic value
// (which here carries the recipient and the link) reaches no log, the grant ends
// undelivered, and the SAME worker delivers the next grant.
func TestResetOutbox_APanickingSendBecomesUndeliveredAndTheWorkerLives(t *testing.T) {
	t.Parallel()
	const who = "panic-probe@registered.example.test"
	grants := grantsFor(who, 2)
	trail := &fakeTrail{}
	ch := &panicChannel{panicOn: map[int]bool{1: true}}
	var logged bytes.Buffer
	router, h := newOutboxFlow(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}}, ch, trail, &logged)
	requestReset(t, newBrowser(t, router), who)
	drain(t, h)

	if n := trail.count(ActionAdminResetUndelivered); n != 1 {
		t.Errorf("%d undelivered row(s), want 1 (the panicking send)", n)
	}
	if n := trail.count(ActionAdminResetRequested); n != 1 {
		t.Errorf("%d requested row(s), want 1 — the worker must survive the panic and deliver the next grant", n)
	}
	ch.mu.Lock()
	sent := len(ch.sent)
	ch.mu.Unlock()
	if sent != 1 {
		t.Errorf("the channel completed %d send(s) after the panic, want 1", sent)
	}
	got := logged.String()
	if !strings.Contains(got, "worker recovered") {
		t.Errorf("the panic left no line:\n%s", got)
	}
	link := grants[0].Issued.Link(strings.TrimRight(testBaseURL, "/") + adminResetNewPath)
	for what, secret := range map[string]string{"recipient": who, "link": link, "panic text": "composing for"} {
		if strings.Contains(strings.ToLower(got), strings.ToLower(secret)) {
			t.Errorf("the %s reached the process log:\n%s", what, got)
		}
	}
}

// TestResetOutbox_ASpentBudgetAbandonsTheRowBeingWritten: when the drain's WHOLE
// budget passes while a row is still being written (a database that stopped
// answering), Drain returns an error at the deadline and the write in flight is
// abandoned at once rather than held for resetAuditGrace — the process is about to
// close its pool, and pgxpool's Close waits for every acquired connection. The worker
// is gone shortly after; the grants it held end with NO row, which is the counted
// limit this test makes visible rather than closes.
func TestResetOutbox_ASpentBudgetAbandonsTheRowBeingWritten(t *testing.T) {
	t.Parallel()
	const who, late = "stuck-db@registered.example.test", "late-request@registered.example.test"
	grants := grantsFor(who, 2)
	lateGrant := grantFor(late)
	trail := &ctxTrail{hold: resetAuditGrace + time.Second, entered: make(chan struct{}, 1)}
	var logged bytes.Buffer
	router, h := newOutboxFlow(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants, late: {lateGrant}}},
		&recordingChannel{}, trail, &logged)
	requestReset(t, newBrowser(t, router), who)
	select {
	case <-trail.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never started writing a row")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := h.Drain(ctx); err == nil {
		t.Fatal("Drain reported success while the row it waited for was still being written")
	}
	select {
	case <-h.outbox.done:
	case <-time.After(time.Second):
		t.Fatalf("the worker was still writing %v after the budget was spent; a row held past the budget "+
			"holds the database pool's close with it", time.Since(start))
	}
	if n := trail.total(); n != 0 {
		t.Errorf("%d row(s) written after the budget was spent, want 0 (they are abandoned)", n)
	}
	if !strings.Contains(logged.String(), "audit write failed") {
		t.Errorf("the abandoned rows left no line (§7 — never swallowed):\n%s", logged.String())
	}

	// AND A REQUEST STILL IN THE HTTP DRAIN IS NOT TIED TO THE OUTBOX'S SPENT BUDGET:
	// the HTTP drain outlasts the outbox's, the pool is still open, and the request's
	// own fallback row must be written. (The worker is gone — q.done above — so the
	// trail can stop holding without a race.)
	trail.hold = 0
	requestReset(t, newBrowser(t, router), late)
	if rows := rowsPerReset(t, trail.eventsSnapshot()); rows[lateGrant.Issued.Reset.ID.String()] != 1 {
		t.Errorf("a grant minted by a request after the outbox's budget was spent has %d row(s), want 1 — "+
			"its fallback row was tied to the outbox's budget instead of the request's", rows[lateGrant.Issued.Reset.ID.String()])
	}
}

// TestResetOutbox_AnEndedStopperEndsTheContextAtOnce: a send or a row started AFTER
// the drain stopped sending (or spent its budget) gets a context that is ALREADY
// ended when boundedBy returns — not one that context.AfterFunc's goroutine ends a
// moment later, a window in which a write could begin. Control: a live stopper
// leaves the context live.
func TestResetOutbox_AnEndedStopperEndsTheContextAtOnce(t *testing.T) {
	t.Parallel()
	ended, end := context.WithCancel(context.Background())
	end()
	ctx, cancel := boundedBy(context.Background(), time.Hour, ended)
	defer cancel()
	if ctx.Err() == nil {
		t.Error("the context was still live when boundedBy returned, though its stopper had already ended")
	}
	live, keep := context.WithCancel(context.Background())
	defer keep()
	ctx2, cancel2 := boundedBy(context.Background(), time.Hour, live)
	defer cancel2()
	if ctx2.Err() != nil {
		t.Fatal("CONTROL FAILED: a live stopper ended the context")
	}
}

// freshResets mints a NEW grant for one administrator on every call — what the real
// adminauth.Resets does for one address (a new row, a new token, the siblings
// retired) — so concurrent requests produce distinct reset ids for one account.
type freshResets struct {
	mu     sync.Mutex
	base   adminauth.ResetGrant
	minted []adminauth.ResetGrant
}

func (f *freshResets) IssueForEmail(context.Context, string) ([]adminauth.ResetGrant, error) {
	g := grantsFor(f.base.Recipient, 1)[0]
	g.Issued.Reset.TenantID, g.Issued.Reset.AdminUserID = f.base.Issued.Reset.TenantID, f.base.Issued.Reset.AdminUserID
	f.mu.Lock()
	f.minted = append(f.minted, g)
	f.mu.Unlock()
	return []adminauth.ResetGrant{g}, nil
}

func (f *freshResets) Consume(context.Context, adminauth.ResetToken, string) (adminauth.ConsumedReset, db.ResolvedPasswordReset, error) {
	return adminauth.ConsumedReset{}, db.ResolvedPasswordReset{}, adminauth.ErrResetUnusable
}

// NoticeRecipient: no link is ever spent here (Consume refuses), so no notice asks.
func (f *freshResets) NoticeRecipient(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "", nil
}

// TestResetOutbox_ConcurrentRequestsForOneAccountStayInsideItsBudget: twenty
// requests for ONE administrator at once, through the real router, from twenty
// addresses (so the per-address request budget refuses none) — in each of the three
// states that decide WHO writes the rows:
//
//	empty outbox   every grant is queued; the one worker writes every row
//	closed outbox  the drain has begun; every request writes its own fallback row
//	full outbox    the worker holds another grant and the queue is full; every
//	               request writes its own fallback row while the worker is busy
//
// In each, every request gets the same page and the trail holds EXACTLY the
// account's audit budget of rows plus ONE rate_limited row (M10 EM-5A 2nd round:
// with "Allowed, then Charge" the two fallback states wrote past the budget and lost
// the rate_limited row; httpx.Limiter.TryCharge makes it one step).
func TestResetOutbox_ConcurrentRequestsForOneAccountStayInsideItsBudget(t *testing.T) {
	t.Parallel()
	const who, n = "busy@registered.example.test", 20
	for _, state := range []string{"empty", "closed", "full"} {
		t.Run(state+" outbox", func(t *testing.T) {
			t.Parallel()
			resets := &freshResets{base: grantFor(who)}
			trail := &fakeTrail{}
			var ch ResetChannel = &recordingChannel{}
			var gate *gateChannel
			if state == "full" {
				gate = newGateChannel()
				ch = gate
			}
			router, h := newResetRouter(t, resets, ch, trail)
			switch state {
			case "closed":
				drain(t, h)
			case "full":
				// Another administrator's grant in the worker's hands, then exactly
				// resetOutboxSize behind it — all directly, before the twenty.
				fill := grantsFor("filler@registered.example.test", 1+resetOutboxSize)
				req := httptest.NewRequest(http.MethodPost, adminResetPath, nil)
				h.dispatch(req, "203.0.113.50", fill[0])
				gate.waitStarted(t)
				for _, g := range fill[1:] {
					h.dispatch(req, "203.0.113.50", g)
				}
			}

			// The forms first, on this goroutine (their helpers may stop the test);
			// then the n POSTs at once.
			browsers := make([]*browser, n)
			tokens := make([]string, n)
			for i := range browsers {
				browsers[i] = newBrowser(t, router)
				browsers[i].ip = "198.51.100." + strconv.Itoa(i+1) + ":4000"
				tokens[i] = csrfFrom(t, htmlOf(t, browsers[i].do(http.MethodGet, adminResetPath, nil)))
			}
			var wg sync.WaitGroup
			codes := make([]int, n)
			pages := make([]string, n)
			start := make(chan struct{})
			for i := range browsers {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					rec := browsers[i].do(http.MethodPost, adminResetPath, url.Values{"csrf": {tokens[i]}, "email": {who}})
					codes[i], pages[i] = rec.Code, rec.Body.String()
				}(i)
			}
			close(start)
			wg.Wait()
			if gate != nil {
				close(gate.release)
			}
			drain(t, h)

			for i := range pages {
				if codes[i] != http.StatusOK || pages[i] != pages[0] {
					t.Errorf("request %d answered %d and a page %s the first one", i, codes[i],
						map[bool]string{true: "equal to", false: "different from"}[pages[i] == pages[0]])
				}
			}
			target := resets.base.Issued.Reset.AdminUserID.String()
			rows, limited := 0, 0
			for _, e := range trail.eventsSnapshot() {
				if e.Target != target {
					continue
				}
				switch e.Action {
				case ActionAdminResetRequested, ActionAdminResetUndelivered:
					rows++
				case ActionAdminResetLimited:
					limited++
				}
			}
			if rows != adminResetAccountLimit || limited != 1 {
				t.Errorf("%d outcome row(s) and %d rate_limited row(s) for one account, want exactly %d and 1",
					rows, limited, adminResetAccountLimit)
			}
			if state == "empty" {
				if got := len(ch.(*recordingChannel).all()); got != n {
					t.Errorf("the channel received %d link(s), want %d — sending is not what the audit budget bounds", got, n)
				}
			}
		})
	}
}

// TestResetOutbox_RacingRowWritersStayInsideTheBudget is the second-round audit's
// measurement turned into a test (M10 EM-5A): many writers of one budget at once, the
// budget's read and write in one step or not.
//
//	account, outbox closed   40 fallback rows for one administrator at once
//	account, worker + fallback  60 grants for one administrator at once into a queue
//	                         the worker is draining: the worker's rows and the
//	                         overflow's fallback rows race for the same budget
//	link                     40 refusals of one recovery link at once
//
// Each run: EXACTLY the budget of rows and EXACTLY one rate_limited row. Repeated,
// because a race is a probability — measured before the fix on the first shape: 244
// of 300 runs over budget (up to 24 rows), 63 with no rate_limited row.
func TestResetOutbox_RacingRowWritersStayInsideTheBudget(t *testing.T) {
	t.Parallel()
	count := func(events []audit.Event, target string) (rows, limited int) {
		for _, e := range events {
			if e.Target != target {
				continue
			}
			switch e.Action {
			case ActionAdminResetRequested, ActionAdminResetUndelivered, ActionAdminResetRefused:
				rows++
			case ActionAdminResetLimited:
				limited++
			}
		}
		return rows, limited
	}
	sameAccount := func(n int) []adminauth.ResetGrant {
		one := grantFor("race@registered.example.test")
		out := make([]adminauth.ResetGrant, n)
		for i := range out {
			out[i] = one
			out[i].Issued.Reset.ID = uuid.New()
		}
		return out
	}
	race := func(n int, f func(i int)) {
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				f(i)
			}(i)
		}
		close(start)
		wg.Wait()
	}
	check := func(t *testing.T, run int, events []audit.Event, target string, budget int) {
		t.Helper()
		if rows, limited := count(events, target); rows != budget || limited != 1 {
			t.Fatalf("run %d: %d row(s) and %d rate_limited row(s), want exactly %d and 1", run, rows, limited, budget)
		}
	}
	req := httptest.NewRequest(http.MethodPost, adminResetPath, nil)

	t.Run("account, outbox closed", func(t *testing.T) {
		t.Parallel()
		for run := 0; run < 100; run++ {
			trail := &fakeTrail{}
			_, h := newResetRouter(t, &fakeResets{}, &recordingChannel{}, trail)
			drain(t, h)
			grants := sameAccount(40)
			race(len(grants), func(i int) { h.dispatch(req, "203.0.113.60", grants[i]) })
			check(t, run, trail.eventsSnapshot(), grants[0].Issued.Reset.AdminUserID.String(), adminResetAccountLimit)
		}
	})

	t.Run("account, worker and fallback together", func(t *testing.T) {
		t.Parallel()
		for run := 0; run < 30; run++ {
			trail := &fakeTrail{}
			_, h := newResetRouter(t, &fakeResets{}, &recordingChannel{delay: time.Millisecond}, trail)
			grants := sameAccount(60)
			race(len(grants), func(i int) { h.dispatch(req, "203.0.113.61", grants[i]) })
			drain(t, h)
			check(t, run, trail.eventsSnapshot(), grants[0].Issued.Reset.AdminUserID.String(), adminResetAccountLimit)
		}
	})

	t.Run("link refusals", func(t *testing.T) {
		t.Parallel()
		for run := 0; run < 100; run++ {
			trail := &fakeTrail{}
			_, h := newResetRouter(t, &fakeResets{}, nil, trail)
			resolved := db.ResolvedPasswordReset{ID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(),
				ExpiresAt: time.Now().Add(time.Hour)}
			race(40, func(int) {
				h.recordForLink(context.Background(), resolved, audit.Event{
					TenantID: resolved.TenantID, ActorID: ptr(resolved.AdminUserID), Action: ActionAdminResetRefused,
					Target: resolved.AdminUserID.String(), Detail: adminResetDetail{Outcome: "refused"},
				})
			})
			check(t, run, trail.eventsSnapshot(), resolved.AdminUserID.String(), adminResetLinkLimit)
		}
	})
}

// TestAdminResetBudgets_EveryGateIsExactUnderConcurrentCallers holds the three
// reset-flow budgets the racing-rows test does not reach to the same property: many
// callers from ONE address at once get EXACTLY the budget (M10 EM-5A, 3rd round; the
// closing audit's P3 probe, turned into a test). Each is httpx.Limiter.TryCharge; split
// back into "Allowed, then Charge", callers that meet at the limiter can all read
// "allowed" before any of them charges.
//
//	request    N POST /admin/reset at once → exactly 20 answered, N−20 × 429, one WARN
//	submit     N POST /admin/reset/new at once → exactly 10 reach Consume, N−10 × 429,
//	           one WARN
//	process    N presentations of a link at once on a deployment with no channel →
//	log        exactly adminResetUnknownLimit (60) lines
//
// Every request is BUILT before the start line, so the callers meet at the limiter
// instead of being spread out by their own set-up, and the response floor is a no-op
// here (its own tests hold it; at 250 ms a run it would only cap how many runs fit).
// Repeated, because a race is a probability. Measured on the development machine
// with each gate split back (mutations MY04a/b/e): 7 to 48 percent of runs
// overshoot without -race and 53 to 90 percent with it, so at the lowest of those
// rates 200 runs let a split gate pass with probability 0.93^200, about 5e-7. A
// slower or single-core runner lowers the rate; the count is not a proof.
func TestAdminResetBudgets_EveryGateIsExactUnderConcurrentCallers(t *testing.T) {
	t.Parallel()
	// A well-formed link value (43 base64url characters) that is visibly synthetic;
	// the fakes never compare it.
	token := strings.Repeat("A", 43)
	const runs = 200
	gate := func(t *testing.T, resets panelResets, ch ResetChannel, logged *bytes.Buffer) http.Handler {
		t.Helper()
		h, err := NewAdminReset(resets, ch, &fakeTrail{}, adminTestConfig(),
			slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
		if err != nil {
			t.Fatalf("NewAdminReset: %v", err)
		}
		h.sleep = func(time.Duration) {}
		stopWorkerAtCleanup(t, h)
		r := chi.NewRouter()
		h.Mount(r)
		return r
	}
	// sameAddress builds n browsers on one client address (distinct ports).
	sameAddress := func(t *testing.T, router http.Handler, n int) []*browser {
		bs := make([]*browser, n)
		for i := range bs {
			bs[i] = newBrowser(t, router)
			bs[i].ip = "203.0.113.77:" + strconv.Itoa(4000+i)
		}
		return bs
	}
	// build is browser.do without the serving: the same cookies, origin and address.
	build := func(b *browser, method, path string, form url.Values) *http.Request {
		var req *http.Request
		if form == nil {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.RemoteAddr = b.ip
		if method == http.MethodPost && b.origin != "" {
			req.Header.Set("Origin", b.origin)
		}
		for name, value := range b.cookies {
			req.AddCookie(&http.Cookie{Name: name, Value: value})
		}
		return req
	}
	// fire serves every prepared request at once and returns the status codes.
	fire := func(router http.Handler, reqs []*http.Request) []int {
		recs := make([]*httptest.ResponseRecorder, len(reqs))
		for i := range recs {
			recs[i] = httptest.NewRecorder()
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range reqs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				router.ServeHTTP(recs[i], reqs[i])
			}(i)
		}
		close(start)
		wg.Wait()
		codes := make([]int, len(recs))
		for i, rec := range recs {
			codes[i] = rec.Code
		}
		return codes
	}
	count := func(codes []int, want int) int {
		n := 0
		for _, c := range codes {
			if c == want {
				n++
			}
		}
		return n
	}

	t.Run("request", func(t *testing.T) {
		t.Parallel()
		const n = 100
		for run := 0; run < runs; run++ {
			var logged bytes.Buffer
			router := gate(t, &fakeResets{}, &recordingChannel{}, &logged)
			reqs := make([]*http.Request, n)
			for i, b := range sameAddress(t, router, n) {
				csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
				reqs[i] = build(b, http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {"p3@unregistered.example.test"}})
			}
			codes := fire(router, reqs)
			ok, refused := count(codes, http.StatusOK), count(codes, http.StatusTooManyRequests)
			warns := strings.Count(logged.String(), "scope=address")
			if ok != adminResetRequestLimit || refused != n-adminResetRequestLimit || warns != 1 {
				t.Fatalf("run %d: %d answered, %d refused, %d warning(s); want %d, %d, 1",
					run, ok, refused, warns, adminResetRequestLimit, n-adminResetRequestLimit)
			}
		}
	})

	t.Run("submit", func(t *testing.T) {
		t.Parallel()
		const n = 100
		for run := 0; run < runs; run++ {
			var logged bytes.Buffer
			resets := &fakeResets{consumeErr: adminauth.ErrResetUnusable}
			router := gate(t, resets, &recordingChannel{}, &logged)
			reqs := make([]*http.Request, n)
			for i, b := range sameAddress(t, router, n) {
				openLink(t, b, token)
				csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetNewPath, nil)))
				reqs[i] = build(b, http.MethodPost, adminResetNewPath, url.Values{
					"csrf": {csrf}, "password": {"a-good-enough-password"}, "password_confirm": {"a-good-enough-password"},
				})
			}
			codes := fire(router, reqs)
			_, consumed := resets.calls()
			refused := count(codes, http.StatusTooManyRequests)
			warns := strings.Count(logged.String(), "scope=submit")
			if consumed != adminResetSubmitLimit || refused != n-adminResetSubmitLimit || warns != 1 {
				t.Fatalf("run %d: %d reached Consume, %d refused, %d warning(s); want %d, %d, 1",
					run, consumed, refused, warns, adminResetSubmitLimit, n-adminResetSubmitLimit)
			}
		}
	})

	t.Run("process log", func(t *testing.T) {
		t.Parallel()
		const n = 160
		for run := 0; run < runs; run++ {
			var logged bytes.Buffer
			router := gate(t, &fakeResets{}, nil, &logged)
			reqs := make([]*http.Request, n)
			for i, b := range sameAddress(t, router, n) {
				reqs[i] = build(b, http.MethodGet, adminResetNewPath+"?t="+token, nil)
			}
			fire(router, reqs)
			if lines := strings.Count(logged.String(), "a recovery link was presented"); lines != adminResetUnknownLimit {
				t.Fatalf("run %d: %d process-log line(s) for %d presentations, want exactly %d",
					run, lines, n, adminResetUnknownLimit)
			}
		}
	})
}

// TestResetOutbox_TheRequestEndingDoesNotCancelTheSend: the visitor's request ends
// (its context is cancelled the moment the handler returns, and here explicitly)
// while the send is still in flight; the send completes and the row says requested.
func TestResetOutbox_TheRequestEndingDoesNotCancelTheSend(t *testing.T) {
	t.Parallel()
	const who = "leaves@registered.example.test"
	grants := grantsFor(who, 1)
	trail := &fakeTrail{}
	ch := newGateChannel()
	router, h := newResetRouter(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}}, ch, trail)

	b := newBrowser(t, router)
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, adminResetPath,
		strings.NewReader(url.Values{"csrf": {csrf}, "email": {who}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testBaseURL)
	req.RemoteAddr = b.ip
	for name, value := range b.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST answered %d", rec.Code)
	}
	ch.waitStarted(t)
	cancel() // the visitor is gone
	time.Sleep(50 * time.Millisecond)
	close(ch.release)
	drain(t, h)

	_, errs := ch.snapshot()
	if len(errs) != 1 || errs[0] != nil {
		t.Errorf("the send ended with %v; the request's end must not cancel it", errs)
	}
	if n := trail.count(ActionAdminResetRequested); n != 1 {
		t.Errorf("%d requested row(s), want 1", n)
	}
}

// cancellingResets resolves to its grants and, in the same call, ends the request's
// context — the shape of a visitor who closes the tab while the grants are minted.
type cancellingResets struct {
	grants []adminauth.ResetGrant
	cancel context.CancelFunc
}

func (f *cancellingResets) IssueForEmail(context.Context, string) ([]adminauth.ResetGrant, error) {
	f.cancel()
	return f.grants, nil
}

func (f *cancellingResets) Consume(context.Context, adminauth.ResetToken, string) (adminauth.ConsumedReset, db.ResolvedPasswordReset, error) {
	return adminauth.ConsumedReset{}, db.ResolvedPasswordReset{}, adminauth.ErrResetUnusable
}

// NoticeRecipient: no link is ever spent here (Consume refuses), so no notice asks.
func (f *cancellingResets) NoticeRecipient(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "", nil
}

// TestResetOutbox_AClientThatLeavesStillGetsItsFallbackRow: when the outbox will not
// take a grant (closing, or full), the REQUEST writes its undelivered row — and the
// visitor may already have gone. The row's context is the request's WITHOUT its
// cancellation, so a trail that refuses a write on an ended context (as Postgres
// does) still records it. Tied to the request's own context, the grant would be left
// with no row at all.
func TestResetOutbox_AClientThatLeavesStillGetsItsFallbackRow(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"closing", "full"} {
		t.Run(state+" outbox", func(t *testing.T) {
			t.Parallel()
			const who = "leaves-early@registered.example.test"
			grants := grantsFor(who, 2)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			trail := &ctxTrail{}
			var ch ResetChannel = &recordingChannel{}
			var gate *gateChannel
			if state == "full" {
				gate = newGateChannel()
				ch = gate
			}
			var logged bytes.Buffer
			router, h := newOutboxFlow(t, &cancellingResets{grants: grants, cancel: cancel}, ch, trail, &logged)
			switch state {
			case "closing":
				drain(t, h)
			case "full":
				fill := grantsFor("filler@registered.example.test", 1+resetOutboxSize)
				req := httptest.NewRequest(http.MethodPost, adminResetPath, nil)
				h.dispatch(req, "203.0.113.70", fill[0])
				gate.waitStarted(t)
				for _, g := range fill[1:] {
					h.dispatch(req, "203.0.113.70", g)
				}
			}
			b := newBrowser(t, router)
			csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, adminResetPath,
				strings.NewReader(url.Values{"csrf": {csrf}, "email": {who}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", testBaseURL)
			req.RemoteAddr = b.ip
			for name, value := range b.cookies {
				req.AddCookie(&http.Cookie{Name: name, Value: value})
			}
			router.ServeHTTP(httptest.NewRecorder(), req)
			if ctx.Err() == nil {
				t.Fatal("CONTROL FAILED: the request's context did not end during the request")
			}
			rows := rowsPerReset(t, trail.eventsSnapshot())
			for _, g := range grants {
				if n := rows[g.Issued.Reset.ID.String()]; n != 1 {
					t.Errorf("grant %s has %d row(s) after its visitor left, want 1", g.Issued.Reset.ID, n)
				}
			}
			if gate != nil {
				close(gate.release)
			}
		})
	}
}

// errTrail refuses every write, as a database that has gone away does.
type errTrail struct{}

func (errTrail) Record(context.Context, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, errors.New("the audit database is unreachable")
}

// panicFirstTrail panics on its first write WITHOUT recording it, then records.
type panicFirstTrail struct {
	fakeTrail
	once sync.Once
}

func (f *panicFirstTrail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	f.once.Do(func() { panic("audit writer fault before the insert") })
	return f.fakeTrail.Record(ctx, e)
}

// TestResetOutbox_TheGrantAndBudgetLinesCarryTheRequestsID: the EIGHT lines this
// flow writes about a grant or a budget — the send failure in both of deliver's
// branches (a plain error, and the *mail.SendError the e-mail channel returns for
// every relay refusal), the account budget's and the link budget's rate-limited
// lines, a failed audit write, the panic line, the refused hand-over and the e-mail
// channel's accepted line — are *Context calls on a context that keeps the
// originating request's values, so the process logger's request-id wrapper
// (httpx.WithRequestID, as cmd/tappa installs it) stamps the request's id on each.
// The SendError line's keys are also held to ADR 0022 §10's closed set. Lines the
// flow writes about anything else (the request path's own refusals) are not
// measured here.
func TestResetOutbox_TheGrantAndBudgetLinesCarryTheRequestsID(t *testing.T) {
	t.Parallel()
	const who = "correlate@registered.example.test"
	const failed = "panel recovery: delivery failed"
	const limited = "panel recovery rate limited"
	sameAccount := func(n int) []adminauth.ResetGrant {
		one := grantFor(who)
		out := make([]adminauth.ResetGrant, n)
		for i := range out {
			out[i] = one
			out[i].Issued.Reset.ID = uuid.New()
		}
		return out
	}
	spent := time.Now().Add(-time.Minute)
	replayed := db.ResolvedPasswordReset{ID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(),
		ExpiresAt: time.Now().Add(time.Hour), UsedAt: &spent}
	for _, tc := range []struct {
		name    string
		resets  panelResets
		channel func(t *testing.T, log *slog.Logger) ResetChannel
		trail   auditRecorder
		closed  bool
		drive   func(t *testing.T, r http.Handler)
		want    []string // the lines measured; each must appear and carry the id
		keysToo bool     // the measured lines' keys must also be in the closed set
	}{
		{name: "send failures past the budget (a plain error)",
			resets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: sameAccount(adminResetAccountLimit + 2)}},
			channel: func(*testing.T, *slog.Logger) ResetChannel {
				return &recordingChannel{err: errors.New("relay said no")}
			},
			trail: &fakeTrail{}, want: []string{failed, limited}},
		{name: "a relay refusal (*mail.SendError)",
			resets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grantsFor(who, 1)}},
			channel: func(*testing.T, *slog.Logger) ResetChannel {
				return &recordingChannel{err: &mail.SendError{Class: mail.ClassRejected, SMTPCode: 550}}
			},
			trail: &fakeTrail{}, want: []string{failed}, keysToo: true},
		{name: "a failed audit write",
			resets:  &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grantsFor(who, 1)}},
			channel: func(*testing.T, *slog.Logger) ResetChannel { return &recordingChannel{} },
			trail:   errTrail{}, want: []string{"audit write failed"}},
		{name: "a panicking send",
			resets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grantsFor(who, 1)}},
			channel: func(*testing.T, *slog.Logger) ResetChannel {
				return &panicChannel{panicOn: map[int]bool{1: true}}
			},
			trail: &fakeTrail{}, want: []string{"panel recovery: a delivery panicked; the worker recovered and moved on"}},
		{name: "a refused hand-over",
			resets:  &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grantsFor(who, 1)}},
			channel: func(*testing.T, *slog.Logger) ResetChannel { return &recordingChannel{} },
			trail:   &fakeTrail{}, closed: true,
			want: []string{"panel recovery: the delivery queue did not take the link, so it was not sent"}},
		{name: "an accepted send (the e-mail channel)",
			resets: &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grantsFor(who, 1)}},
			channel: func(t *testing.T, log *slog.Logger) ResetChannel {
				relay := newFakeRelay(t, relayScript{})
				user, pass := relayCredentials(t)
				ch, err := NewEmailResetChannel(relay.sender(t, user, pass, 5*time.Second), adminTestConfig().BaseURL, log)
				if err != nil {
					t.Fatalf("NewEmailResetChannel: %v", err)
				}
				return ch
			},
			trail: &fakeTrail{}, want: []string{"panel recovery: the relay accepted the reset e-mail"}},
		{name: "one link's refusals past its budget",
			resets:  &fakeResets{consumeErr: adminauth.ErrResetUnusable, consumeResolved: replayed},
			channel: func(*testing.T, *slog.Logger) ResetChannel { return &recordingChannel{} },
			trail:   &fakeTrail{},
			drive: func(t *testing.T, r http.Handler) {
				// One more replay than the link's budget, each from its own address so
				// the per-address submit budget refuses none.
				for i := 0; i < adminResetLinkLimit+1; i++ {
					b := newBrowser(t, r)
					b.ip = "198.51.100." + strconv.Itoa(i+1) + ":9000"
					submitLink(t, b, strings.Repeat("A", 43), "a-good-enough-password")
				}
			},
			want: []string{limited}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logged bytes.Buffer
			log := slog.New(httpx.WithRequestID(slog.NewJSONHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
			h, err := NewAdminReset(tc.resets, tc.channel(t, log), tc.trail, adminTestConfig(), log)
			if err != nil {
				t.Fatal(err)
			}
			stopWorkerAtCleanup(t, h)
			r := chi.NewRouter()
			r.Use(httpx.RequestID)
			h.Mount(r)
			if tc.closed {
				drain(t, h)
			}
			if tc.drive != nil {
				tc.drive(t, r)
			} else {
				requestReset(t, newBrowser(t, r), who)
			}
			drain(t, h)

			measured := map[string]bool{}
			for _, m := range tc.want {
				measured[m] = true
			}
			seen := map[string]bool{}
			var measuredLines []string
			for _, line := range strings.Split(strings.TrimSpace(logged.String()), "\n") {
				var rec map[string]any
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					t.Fatalf("a log line is not JSON: %v", err)
				}
				msg, _ := rec["msg"].(string)
				if !measured[msg] {
					continue
				}
				seen[msg] = true
				measuredLines = append(measuredLines, line)
				if id, _ := rec[httpx.LogRequestIDKey].(string); id == "" {
					t.Errorf("a %q line carries no request id; it cannot be joined to its request:\n%s", msg, line)
				}
			}
			for _, m := range tc.want {
				if !seen[m] {
					t.Errorf("CONTROL FAILED: no %q line was written:\n%s", m, logged.String())
				}
			}
			if tc.keysToo {
				joined := strings.Join(measuredLines, "\n")
				if off := offClosedSet(t, joined, httpx.LogRequestIDKey); len(off) > 0 {
					t.Errorf("keys outside ADR 0022 §10's closed set: %q\n%s", off, joined)
				}
				if !strings.Contains(joined, `"class":"rejected"`) || !strings.Contains(joined, `"smtp_code":550`) {
					t.Errorf("the SendError line does not carry its class and code:\n%s", joined)
				}
			}
		})
	}
}

// TestResetOutbox_AnOfferRacingTheDrainNeverSendsOnAClosedQueue is the second-round
// audit's probe as a test: 200 times, 30 hand-overs race one Drain. The closed flag
// and the queue's close are under one lock with the hand-over's send; read without
// it, an offer can send on the queue Drain just closed — a panic in the request
// goroutine (measured: "send on closed channel" under -race).
func TestResetOutbox_AnOfferRacingTheDrainNeverSendsOnAClosedQueue(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, adminResetPath, nil)
	for run := 0; run < 200; run++ {
		trail := &fakeTrail{}
		h, err := NewAdminReset(&fakeResets{}, &recordingChannel{}, trail, adminTestConfig(), slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatal(err)
		}
		grants := grantsFor("race-close@registered.example.test", 30)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range grants {
			wg.Add(1)
			go func(g adminauth.ResetGrant) {
				defer wg.Done()
				<-start
				h.dispatch(req, "203.0.113.80", g)
			}(grants[i])
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.Drain(ctx); err != nil {
				t.Errorf("run %d: drain: %v", run, err)
			}
		}()
		close(start)
		wg.Wait()
		drain(t, h)
		if rows := rowsPerReset(t, trail.eventsSnapshot()); len(rows) != len(grants) {
			t.Fatalf("run %d: rows for %d of %d grants", run, len(rows), len(grants))
		}
	}
}

// TestResetOutbox_ASecondFaultLeavesTheWorkerAlive pins "the undelivered row is
// itself contained": the send panics AND the audit writer panics on the fallback row
// — a second fault in the same grant. The worker survives it (the grant is left
// without a row: the counted limit "a panic inside the audit writer"), logs both, and
// the next grant is delivered and recorded requested.
func TestResetOutbox_ASecondFaultLeavesTheWorkerAlive(t *testing.T) {
	t.Parallel()
	grants := grantsFor("double-fault@registered.example.test", 2)
	trail := &panicFirstTrail{}
	var logged bytes.Buffer
	router, h := newOutboxFlow(t, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{"double-fault@registered.example.test": grants}},
		&panicChannel{panicOn: map[int]bool{1: true}}, trail, &logged)
	requestReset(t, newBrowser(t, router), "double-fault@registered.example.test")
	drain(t, h)
	rows := rowsPerReset(t, trail.eventsSnapshot())
	if rows[grants[1].Issued.Reset.ID.String()] != 1 || trail.count(ActionAdminResetRequested) != 1 {
		t.Errorf("the second grant has %d row(s) and the trail %d requested row(s), want 1 and 1 — the worker "+
			"must outlive a second fault in the grant before", rows[grants[1].Issued.Reset.ID.String()],
			trail.count(ActionAdminResetRequested))
	}
	if n := strings.Count(logged.String(), "worker recovered"); n != 2 {
		t.Errorf("%d panic line(s), want 2 (the send's and the audit writer's):\n%s", n, logged.String())
	}
}
