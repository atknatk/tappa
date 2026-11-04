package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/mail"
)

// The process-wide breaker (M10 EM-7A, ADR 0022 §9) as the recovery flow meets it: the
// REAL e-mail channel around a REAL mail.Breaker, whose wrapped sender is breakerProbe
// instead of the SMTP transport — the breaker sits in front of the transport, so what
// is measured here is what reaches the transport, without a relay in the way. The
// breaker's own window, concurrency and log lines are internal/mail's breaker_test.go.

// breakerProbe is the sender behind the breaker: it keeps what reached it. When hold
// is set, the NEXT call (only that one) signals entered and waits for hold.
type breakerProbe struct {
	mu      sync.Mutex
	sent    []mail.Message
	hold    chan struct{}
	entered chan struct{}
}

func (p *breakerProbe) Send(ctx context.Context, m mail.Message) (mail.Receipt, error) {
	p.mu.Lock()
	hold, entered := p.hold, p.entered
	p.hold = nil
	p.sent = append(p.sent, m)
	p.mu.Unlock()
	if hold != nil {
		close(entered)
		select {
		case <-hold:
		case <-ctx.Done():
			return mail.Receipt{}, ctx.Err()
		}
	}
	return mail.Receipt{MessageID: "probe-message-id"}, nil
}

func (p *breakerProbe) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent)
}

func (p *breakerProbe) last() mail.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sent[len(p.sent)-1]
}

// holdNext makes the next call wait until the returned release is called.
func (p *breakerProbe) holdNext() (entered <-chan struct{}, release func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	hold, in := make(chan struct{}), make(chan struct{})
	p.hold, p.entered = hold, in
	var once sync.Once
	return in, func() { once.Do(func() { close(hold) }) }
}

// newProbeChannel is the real e-mail channel over a real breaker over p, both logging
// as JSON to w.
func newProbeChannel(t *testing.T, p *breakerProbe, w *bytes.Buffer) (ResetChannel, *mail.Breaker, *slog.Logger) {
	t.Helper()
	log := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
	b, err := mail.NewBreaker(p, mail.BreakerConfig{Log: log})
	if err != nil {
		t.Fatalf("NewBreaker: %v", err)
	}
	ch, err := NewEmailResetChannel(b, adminTestConfig().BaseURL, log)
	if err != nil {
		t.Fatalf("NewEmailResetChannel: %v", err)
	}
	return ch, b, log
}

// fillBreaker lets n sends through b directly, as other flows would.
func fillBreaker(t *testing.T, b *mail.Breaker, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := b.Send(context.Background(), mail.Message{To: "fill@other-flow.example.test"}); err != nil {
			t.Fatalf("fill send %d was refused: %v", i+1, err)
		}
	}
}

// probeDelivery is a deliverable reset link for the channel.
func probeDelivery(to string) ResetDelivery {
	g := grantFor(to)
	return ResetDelivery{Recipient: to, Link: g.Issued.Link(resetLinkBase()), ExpiresAt: g.Issued.Reset.ExpiresAt, ResetID: g.Issued.Reset.ID}
}

// isBreakerRefusal reports whether err is the breaker's *mail.SendError.
func isBreakerRefusal(err error) bool {
	var se *mail.SendError
	return errors.As(err, &se) && se.Class == mail.ClassBreaker && se.SMTPCode == 0
}

// TestEmailResetChannel_TheBreakerCountsTheNoticeAndRefusesTheLink is K7A-3 where it is
// decided — the channel: the "your password was changed" notice is counted by the
// breaker and never refused by it, the reset link is refused past the limit.
func TestEmailResetChannel_TheBreakerCountsTheNoticeAndRefusesTheLink(t *testing.T) {
	t.Parallel()
	notice := PasswordNotice{Recipient: "owner@notice.example.test", TenantID: uuid.New(), AdminUserID: uuid.New()}

	t.Run("300 notices are counted, the link after them is refused, a notice still goes", func(t *testing.T) {
		t.Parallel()
		p := &breakerProbe{}
		var logged bytes.Buffer
		ch, _, _ := newProbeChannel(t, p, &logged)
		for i := 0; i < 300; i++ {
			if err := ch.DeliverPasswordNotice(context.Background(), notice); err != nil {
				t.Fatalf("notice %d: %v", i+1, err)
			}
		}
		if p.count() != 300 {
			t.Fatalf("%d notices reached the sender, want 300", p.count())
		}
		if !ch.RefusingResets() {
			t.Fatal("after 300 notices in the hour the breaker does not refuse a link: the notices were not counted")
		}
		if err := ch.DeliverReset(context.Background(), probeDelivery("owner@notice.example.test")); !isBreakerRefusal(err) {
			t.Fatalf("a reset link after 300 notices: err = %v, want the breaker's refusal", err)
		}
		if p.count() != 300 {
			t.Fatal("a refused reset link reached the sender")
		}
		for i := 0; i < 3; i++ {
			if err := ch.DeliverPasswordNotice(context.Background(), notice); err != nil {
				t.Fatalf("a notice past the limit was refused: %v", err)
			}
		}
		if p.count() != 303 || p.last().To != notice.Recipient {
			t.Fatalf("%d messages reached the sender, the last to the wrong address; want 303 ending with the notice", p.count())
		}
	})

	t.Run("300 links pass and the 301st is refused; a notice still goes", func(t *testing.T) {
		t.Parallel()
		p := &breakerProbe{}
		var logged bytes.Buffer
		ch, _, _ := newProbeChannel(t, p, &logged)
		for i := 0; i < 300; i++ {
			if err := ch.DeliverReset(context.Background(), probeDelivery("owner@links.example.test")); err != nil {
				t.Fatalf("link %d: %v", i+1, err)
			}
		}
		if err := ch.DeliverReset(context.Background(), probeDelivery("owner@links.example.test")); !isBreakerRefusal(err) {
			t.Fatalf("the 301st link: err = %v, want the breaker's refusal", err)
		}
		if err := ch.DeliverPasswordNotice(context.Background(), notice); err != nil {
			t.Fatalf("a notice after 300 links was refused: %v", err)
		}
		if p.count() != 301 {
			t.Fatalf("%d messages reached the sender, want 301", p.count())
		}
	})
}

// TestPasswordNotice_ATrippedBreakerDoesNotStopIt is K7A-3 through the whole notice
// path (passwordChanged → the real channel → the real breaker): with the breaker
// refusing every reset link, a password change still sends its notice to the row's
// address and writes a sent row — and the notice was counted.
func TestPasswordNotice_ATrippedBreakerDoesNotStopIt(t *testing.T) {
	t.Parallel()
	adminID := uuid.New()
	const to = "owner@tripped-notice.example.test"
	p := &breakerProbe{}
	var logged bytes.Buffer
	ch, b, log := newProbeChannel(t, p, &logged)
	trail := &fakeTrail{}
	h, err := NewAdminReset(&fakeResets{recipients: map[uuid.UUID]string{adminID: to}}, ch, trail, adminTestConfig(), log)
	if err != nil {
		t.Fatal(err)
	}
	stopWorkerAtCleanup(t, h)

	fillBreaker(t, b, 299)
	if ch.RefusingResets() {
		t.Fatal("CONTROL FAILED: the breaker refuses after 299 sends")
	}
	changeOnce(t, h, passwordChange{tenantID: uuid.New(), adminUserID: adminID, via: noticeViaAccount})
	if !ch.RefusingResets() {
		t.Fatal("the notice was not counted: 299 sends and one notice leave the breaker passing")
	}
	// The breaker now refuses links (the control) — and the next change is still told.
	changeOnce(t, h, passwordChange{tenantID: uuid.New(), adminUserID: adminID, via: noticeViaRecovery})
	if p.count() != 301 || p.last().To != to {
		t.Fatalf("%d messages reached the sender (want 301), the last to the row's address", p.count())
	}
	if n := trail.count(ActionAdminPasswordNoticeSent); n != 2 || trail.total() != 2 {
		t.Errorf("%d sent notice row(s) of %d rows, want 2 of 2", n, trail.total())
	}
}

// breakerKnownAddress is the address with a FULL WINDOW of grants behind it in a
// newBreakerFlow: the most rows the tripped branch writes synchronously in one request,
// i.e. its slowest case.
const breakerKnownAddress = "owner@tripped.example.test"

// breakerAnswer is one POST /admin/reset as the visitor receives it.
type breakerAnswer struct {
	code    int
	header  http.Header
	body    string
	elapsed time.Duration
}

// postBreakerAnswer asks in a fresh browser. The clock covers the POST alone.
func postBreakerAnswer(t *testing.T, router http.Handler, email string) breakerAnswer {
	t.Helper()
	br := newBrowser(t, router)
	page := br.do(http.MethodGet, adminResetPath, nil)
	csrf := csrfFrom(t, htmlOf(t, page))
	start := time.Now()
	rec := br.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {email}})
	elapsed := time.Since(start)
	return breakerAnswer{code: rec.Code, header: rec.Header().Clone(), body: htmlOf(t, rec), elapsed: elapsed}
}

// breakerFlow is the recovery flow behind a real breaker, tripped or passing.
type breakerFlow struct {
	h      *AdminReset
	router http.Handler
	probe  *breakerProbe
	trail  *fakeTrail
	logged *bytes.Buffer
}

func newBreakerFlow(t *testing.T, tripped bool) breakerFlow {
	t.Helper()
	f := breakerFlow{probe: &breakerProbe{}, trail: &fakeTrail{}, logged: &bytes.Buffer{}}
	ch, b, log := newProbeChannel(t, f.probe, f.logged)
	grants := grantsFor(breakerKnownAddress, adminauth.ResetWindow)
	h, err := NewAdminReset(&fakeResets{grantsFor: map[string][]adminauth.ResetGrant{breakerKnownAddress: grants}}, ch, f.trail, adminTestConfig(), log)
	if err != nil {
		t.Fatal(err)
	}
	stopWorkerAtCleanup(t, h)
	if tripped {
		fillBreaker(t, b, 300)
	}
	f.h, f.router = h, mountReset(h)
	return f
}

// TestAdminReset_ATrippedBreakerRecordsTheGrantAtOnceAndQueuesNothing is K7A-4: with
// the process-wide breaker refusing, each grant of a registered address (a full
// adminauth.ResetWindow of them — the slowest case) is recorded
// undelivered by the REQUEST — the existing undelivered row, the breaker's reason —
// before its response returns, nothing enters the outbox and nothing is sent; the
// handler writes no line per refused grant (the breaker's own trip line is the only
// one); and the response — status, every header, body — is byte-identical to the same
// request's with the breaker passing and to an unregistered address's, each paying
// the floor.
//
// THE CONTROLS: with the breaker passing, the same request's grants are all queued and
// sent (the comparison is between two genuinely different paths); and a deployment
// with no channel answers the same route with a different page (the comparison can
// fail).
func TestAdminReset_ATrippedBreakerRecordsTheGrantAtOnceAndQueuesNothing(t *testing.T) {
	t.Parallel()
	const known, unknown = breakerKnownAddress, "nobody@tripped.example.test"
	const window = adminauth.ResetWindow

	// THE TRIPPED BREAKER.
	cut := newBreakerFlow(t, true)
	got := postBreakerAnswer(t, cut.router, known)
	// Before any drain: the request itself wrote both rows, with the breaker's reason.
	rows := cut.trail.eventsSnapshot()
	if len(rows) != window {
		t.Fatalf("%d row(s) when the response returned, want the %d grants' undelivered rows written by the request", len(rows), window)
	}
	for i, e := range rows {
		d, _ := e.Detail.(adminResetDetail)
		if e.Action != ActionAdminResetUndelivered || d.Outcome != "undelivered" || d.Reason != resetReasonBreaker {
			t.Errorf("row %d is %s / %q / %q, want %s with the breaker's reason", i, e.Action, d.Outcome, d.Reason, ActionAdminResetUndelivered)
		}
	}
	if n := len(cut.h.outbox.jobs); n != 0 {
		t.Errorf("%d grant(s) waiting in the outbox, want none", n)
	}
	drain(t, cut.h)
	if n := cut.probe.count(); n != 300 {
		t.Errorf("%d message(s) reached the sender, want only the 300 that filled the window", n)
	}
	if n := cut.trail.total(); n != window {
		t.Errorf("%d row(s) after the drain, want still %d (no worker row)", n, window)
	}
	lines := strings.Split(strings.TrimSpace(cut.logged.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"msg":"mail breaker:`) {
		t.Errorf("want exactly one line — the breaker's trip — and none per refused grant, got %d:\n%s", len(lines), cut.logged.String())
	}

	// THE SAME REQUEST WITH THE BREAKER PASSING (control: both grants were sent).
	open := newBreakerFlow(t, false)
	want := postBreakerAnswer(t, open.router, known)
	drain(t, open.h)
	if open.probe.count() != window || open.trail.count(ActionAdminResetRequested) != window {
		t.Fatalf("CONTROL FAILED: with the breaker passing, %d sent and %d requested row(s), want %d and %d",
			open.probe.count(), open.trail.count(ActionAdminResetRequested), window, window)
	}
	// AN UNREGISTERED ADDRESS WHILE THE BREAKER REFUSES.
	nobody := postBreakerAnswer(t, cut.router, unknown)

	for name, a := range map[string]breakerAnswer{"tripped, registered": got, "tripped, unregistered": nobody} {
		if a.code != want.code {
			t.Errorf("%s: status %d, want %d", name, a.code, want.code)
		}
		if !reflect.DeepEqual(a.header, want.header) {
			t.Errorf("%s: headers %v, want %v", name, a.header, want.header)
		}
		if a.body != want.body {
			t.Errorf("%s: the body differs from the passing breaker's:\n%s\n---\n%s", name, a.body, want.body)
		}
	}
	// THE FLOOR STAYS HERE BECAUSE A PAUSE CAN ONLY ADD, so this bound holds however
	// loaded the machine and whatever runs beside this test. The ceiling cannot be read on
	// one sample of a parallel test: TestAdminReset_ATrippedBreakerStaysInsideTheFloorBand.
	for name, a := range map[string]breakerAnswer{"tripped, registered": got, "passing, registered": want, "tripped, unregistered": nobody} {
		if a.elapsed < resetRequestFloor {
			t.Errorf("%s answered in %v, under the floor %v", name, a.elapsed, resetRequestFloor)
		}
	}
	// THE INDEPENDENT CONTROL: the comparison above can fail.
	dark, _ := newResetRouter(t, &fakeResets{}, nil, &fakeTrail{})
	if other := postBreakerAnswer(t, dark, known); other.body == want.body {
		t.Fatal("CONTROL FAILED: an undeliverable deployment's page equals the deliverable one, so the comparison proves nothing")
	}
}

// TestAdminReset_ATrippedBreakerStaysInsideTheFloorBand is the clock half of
// TestAdminReset_ATrippedBreakerRecordsTheGrantAtOnceAndQueuesNothing: tripped or passing,
// registered or not, a full window behind the address, the answer lands in
// [resetRequestFloor, resetRequestFloor+50ms], so its time does not say what state the
// breaker is in. The tripped registered arm writes its rows on the request; a drift that
// slows that branch past the floor is what the ceiling catches.
//
// NOT PARALLEL, AND THE MEDIAN IS NOT WHAT MAKES THAT SAFE (T193; CI 41744035367 failed the
// old single sample at 380 ms). Measured beside 128 locked OS threads at GOMAXPROCS=4: with
// three goroutines holding this process's Ps the old single sample was red 24/30 and THIS
// median 18/20, because losing a P repeats from sample to sample; with those goroutines gone
// the old sample was green 30/30. A sequential test never shares the process with a
// t.Parallel one, which is the fix. The median is for what remains, the isolated pause: other
// packages' processes, and a process's first POST (all 9 overruns seen without those
// goroutines were that request; the one attributed spent 145 ms before the floor arms).
//
// THE FLOOR IS READ ON EVERY SAMPLE, the ceiling on each arm's median, because a pause can
// only add: an answer under the floor is the branch showing, however loaded the machine.
//
// ⚠️ THE BLIND SPOT THIS BUYS, NAMED: an overrun in at most two of an arm's five samples is
// not seen. Not repeatable on demand, so no probing oracle (the T181 reasoning).
func TestAdminReset_ATrippedBreakerStaysInsideTheFloorBand(t *testing.T) {
	const (
		trippedRegistered   = "tripped, registered"
		passingRegistered   = "passing, registered"
		trippedUnregistered = "tripped, unregistered"
		samples             = 5
		window              = adminauth.ResetWindow
	)
	took := map[string][]time.Duration{}
	for i := 0; i < samples; i++ {
		// A fresh world per round, interleaved: every sample is the old single sample's
		// shape (a world's first request included), and a machine that gets busier moves
		// every arm.
		cut := newBreakerFlow(t, true)
		took[trippedRegistered] = append(took[trippedRegistered], postBreakerAnswer(t, cut.router, breakerKnownAddress).elapsed)
		// ANTI-VACUITY: the arm named "tripped" wrote a full window on the request.
		if n := cut.trail.count(ActionAdminResetUndelivered); n != window {
			t.Fatalf("CONTROL FAILED: round %d, %d undelivered row(s) when the tripped arm answered, want %d", i+1, n, window)
		}
		open := newBreakerFlow(t, false)
		took[passingRegistered] = append(took[passingRegistered], postBreakerAnswer(t, open.router, breakerKnownAddress).elapsed)
		// Drained inside the round so this arm's worker is not on the next arm's clock.
		drain(t, open.h)
		if n := open.probe.count(); n != window {
			t.Fatalf("CONTROL FAILED: round %d, %d message(s) sent with the breaker passing, want %d", i+1, n, window)
		}
		took[trippedUnregistered] = append(took[trippedUnregistered], postBreakerAnswer(t, cut.router, "nobody@tripped.example.test").elapsed)
	}
	ceiling := resetRequestFloor + 50*time.Millisecond
	for _, name := range []string{trippedRegistered, passingRegistered, trippedUnregistered} {
		all := took[name]
		for j, d := range all {
			if d < resetRequestFloor {
				t.Errorf("%s answered in %v in round %d, under the floor %v: the breaker's state is visible in the clock",
					name, d, j+1, resetRequestFloor)
			}
		}
		sorted := slices.Sorted(slices.Values(all))
		median := sorted[samples/2]
		if median > ceiling {
			t.Errorf("%s: the median of %d answers is %v, over floor+50ms (%v), samples %v: the branch it took is visible in the clock",
				name, samples, median, ceiling, all)
		}
		t.Logf("%s: median %v, samples %v; band [%v, %v]", name, median, all, resetRequestFloor, ceiling)
	}
}

// TestResetOutbox_AGrantTheBreakerRefusesAtTheWorkerIsUndelivered: RefusingResets is a
// question, not a reservation. A grant that found the breaker passing is queued; if the
// window fills before the worker sends it, the breaker refuses it there — an ordinary
// failed send: one undelivered row with the send-failed reason, and a failure line
// naming class=breaker and smtp_code=0 (the class's word reaches the log).
func TestResetOutbox_AGrantTheBreakerRefusesAtTheWorkerIsUndelivered(t *testing.T) {
	t.Parallel()
	p := &breakerProbe{}
	var logged bytes.Buffer
	ch, b, log := newProbeChannel(t, p, &logged)
	first, second := grantsFor("first@race.example.test", 1), grantsFor("second@race.example.test", 1)
	trail := &fakeTrail{}
	h, err := NewAdminReset(&fakeResets{grantsFor: map[string][]adminauth.ResetGrant{
		"first@race.example.test": first, "second@race.example.test": second,
	}}, ch, trail, adminTestConfig(), log)
	if err != nil {
		t.Fatal(err)
	}
	stopWorkerAtCleanup(t, h)
	router := mountReset(h)

	fillBreaker(t, b, 298)
	entered, release := p.holdNext()
	t.Cleanup(release)
	requestReset(t, newBrowser(t, router), "first@race.example.test") // the 299th, held in the sender
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never reached the sender")
	}
	requestReset(t, newBrowser(t, router), "second@race.example.test") // 299 < 300: queued
	if trail.total() != 0 {
		t.Fatalf("CONTROL FAILED: %d row(s) before the worker ran — the second grant was refused by the request", trail.total())
	}
	fillBreaker(t, b, 1) // the window is full now
	release()
	drain(t, h)

	if n := trail.count(ActionAdminResetRequested); n != 1 {
		t.Errorf("%d requested row(s), want 1 (the first grant)", n)
	}
	var reasons []string
	for _, e := range trail.eventsSnapshot() {
		if d, ok := e.Detail.(adminResetDetail); ok && e.Action == ActionAdminResetUndelivered {
			reasons = append(reasons, d.Reason)
		}
	}
	if len(reasons) != 1 || reasons[0] != resetReasonSendFailed {
		t.Errorf("undelivered reasons %q, want one send-failed row", reasons)
	}
	if p.count() != 300 {
		t.Errorf("%d message(s) reached the sender, want 300 (the refused grant not among them)", p.count())
	}
	for _, want := range []string{`"class":"breaker"`, `"smtp_code":0`, `"err_type":"*mail.SendError"`} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("the failure line lacks %s:\n%s", want, logged.String())
		}
	}
}
