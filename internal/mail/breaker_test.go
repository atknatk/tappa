package mail_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// clock is an injected time source a test moves by hand: a window passes without
// anybody waiting for it.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// countingSender is what the breaker wraps in these tests: it counts what reached it
// and accepts everything.
type countingSender struct {
	mu   sync.Mutex
	sent int
}

func (s *countingSender) Send(context.Context, mail.Message) (mail.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent++
	return mail.Receipt{MessageID: "id"}, nil
}

func (s *countingSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent
}

// newBreaker wraps a fresh countingSender, on c, logging to w (nil discards).
func newBreaker(t *testing.T, c *clock, w *bytes.Buffer) (*mail.Breaker, *countingSender) {
	t.Helper()
	inner := &countingSender{}
	var h slog.Handler = slog.DiscardHandler
	if w != nil {
		h = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	b, err := mail.NewBreaker(inner, mail.BreakerConfig{Now: c.now, Log: slog.New(h)})
	if err != nil {
		t.Fatalf("NewBreaker: %v", err)
	}
	return b, inner
}

// mustPass sends n messages that must all be let through.
func mustPass(t *testing.T, b *mail.Breaker, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := b.Send(context.Background(), validMessage()); err != nil {
			t.Fatalf("send %d of %d was refused: %v", i+1, n, err)
		}
	}
}

// mustRefuse asserts one send is refused as breaker, with no code and the text shape
// every SendError has.
func mustRefuse(t *testing.T, b *mail.Breaker, inner *countingSender) {
	t.Helper()
	before := inner.count()
	_, err := b.Send(context.Background(), validMessage())
	sendError(t, err, mail.ClassBreaker, 0)
	if inner.count() != before {
		t.Fatal("a refused send reached the wrapped sender")
	}
}

// TestBreaker_The300thPassesAndThe301stIsRefusedUntilTheHourPasses is K7A-1's
// threshold: 300 sends in an hour pass, the 301st is refused before the wrapped sender
// is reached, and the refusal lasts exactly until the oldest of the 300 is an hour old.
// The counts are LITERALS, so a changed BreakerLimit turns this red.
func TestBreaker_The300thPassesAndThe301stIsRefusedUntilTheHourPasses(t *testing.T) {
	t.Parallel()
	c := newClock()
	t0 := c.now()
	b, inner := newBreaker(t, c, nil)

	mustPass(t, b, 299)
	c.set(t0.Add(59 * time.Minute)) // the 300th, late in the same hour
	mustPass(t, b, 1)
	if inner.count() != 300 {
		t.Fatalf("%d sends reached the wrapped sender, want 300", inner.count())
	}
	mustRefuse(t, b, inner)

	c.set(t0.Add(time.Hour - time.Nanosecond))
	mustRefuse(t, b, inner)

	// At t0+1h the 299 sent at t0 are an hour old: exactly 299 slots free.
	c.set(t0.Add(time.Hour))
	mustPass(t, b, 299)
	mustRefuse(t, b, inner)
	if inner.count() != 599 {
		t.Fatalf("%d sends reached the wrapped sender, want 599", inner.count())
	}
}

// TestBreaker_TheWindowSlidesOneSendAtATime: the window is the hour ending NOW, not
// the clock hour. 100 sends at t0 and 200 at t0+30m fill it; at t0+1h only the first
// 100 have aged out, so exactly 100 pass; at t0+1h30m the next 200 do. A window that
// reset every hour would let 300 through at t0+1h.
func TestBreaker_TheWindowSlidesOneSendAtATime(t *testing.T) {
	t.Parallel()
	c := newClock()
	t0 := c.now()
	b, inner := newBreaker(t, c, nil)

	mustPass(t, b, 100)
	c.set(t0.Add(30 * time.Minute))
	mustPass(t, b, 200)
	mustRefuse(t, b, inner)

	c.set(t0.Add(time.Hour))
	mustPass(t, b, 100)
	mustRefuse(t, b, inner)

	c.set(t0.Add(90*time.Minute - time.Nanosecond))
	mustRefuse(t, b, inner)
	c.set(t0.Add(90 * time.Minute))
	mustPass(t, b, 200)
	mustRefuse(t, b, inner)
}

// TestBreaker_TheShippedLimitIs300AnHour pins the shipped numbers (K7A-1, ADR 0022 §9)
// and the class's word, which the audit rows and the log lines carry.
func TestBreaker_TheShippedLimitIs300AnHour(t *testing.T) {
	t.Parallel()
	if mail.BreakerLimit != 300 || mail.BreakerWindow != time.Hour {
		t.Errorf("the breaker ships %d per %v, want 300 per hour (ADR 0022's EM-7A note)", mail.BreakerLimit, mail.BreakerWindow)
	}
	if mail.ClassBreaker != "breaker" {
		t.Errorf("ClassBreaker is %q, want \"breaker\"", mail.ClassBreaker)
	}
}

// TestBreaker_AnExemptSendIsCountedAndNeverRefused is K7A-3: the "your password was
// changed" notice's method is counted like every send and never refused.
func TestBreaker_AnExemptSendIsCountedAndNeverRefused(t *testing.T) {
	t.Parallel()
	t.Run("300 exempt sends shut it for refusable ones, and exempt ones still go", func(t *testing.T) {
		t.Parallel()
		c := newClock()
		t0 := c.now()
		b, inner := newBreaker(t, c, nil)
		for i := 0; i < 300; i++ {
			if _, err := b.SendExempt(context.Background(), validMessage()); err != nil {
				t.Fatalf("exempt send %d: %v", i+1, err)
			}
		}
		mustRefuse(t, b, inner)
		if !b.Refusing() {
			t.Error("Refusing says false after 300 counted sends")
		}
		for i := 0; i < 50; i++ {
			if _, err := b.SendExempt(context.Background(), validMessage()); err != nil {
				t.Fatalf("an exempt send past the limit was refused: %v", err)
			}
		}
		if inner.count() != 350 {
			t.Fatalf("%d sends reached the wrapped sender, want 350", inner.count())
		}
		// Every one of them was counted: an hour after the LAST, there is room again.
		c.set(t0.Add(time.Hour))
		mustPass(t, b, 300)
		mustRefuse(t, b, inner)
	})
	t.Run("299 sends and one exempt send fill it", func(t *testing.T) {
		t.Parallel()
		c := newClock()
		b, inner := newBreaker(t, c, nil)
		mustPass(t, b, 299)
		if _, err := b.SendExempt(context.Background(), validMessage()); err != nil {
			t.Fatalf("exempt send: %v", err)
		}
		mustRefuse(t, b, inner)
	})
}

// TestBreaker_ExactlyTheLimitPassesUnderConcurrency: 1000 sends started together on a
// fresh breaker, 20 times — exactly 300 reach the wrapped sender every time, and every
// other one is a breaker refusal. Run under -race.
func TestBreaker_ExactlyTheLimitPassesUnderConcurrency(t *testing.T) {
	t.Parallel()
	for run := 0; run < 20; run++ {
		c := newClock()
		b, inner := newBreaker(t, c, nil)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		refused, other := 0, 0
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := b.Send(context.Background(), validMessage())
				var se *mail.SendError
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
				case errors.As(err, &se) && se.Class == mail.ClassBreaker && se.SMTPCode == 0:
					refused++
				default:
					other++
				}
			}()
		}
		close(start)
		wg.Wait()
		if inner.count() != 300 || refused != 700 || other != 0 {
			t.Fatalf("run %d: %d passed, %d refused as breaker, %d other errors; want 300, 700, 0",
				run, inner.count(), refused, other)
		}
	}
}

// TestBreaker_RefusingRecordsNothingAndIsARefusal: the recovery flow's question
// (K7A-4) takes no slot — 1000 calls on a fresh breaker leave room for 300 sends —
// answers true once the window is full, and that answer is a refusal: it alone trips
// the breaker (one Warn line).
func TestBreaker_RefusingRecordsNothingAndIsARefusal(t *testing.T) {
	t.Parallel()
	c := newClock()
	var logged bytes.Buffer
	b, inner := newBreaker(t, c, &logged)
	for i := 0; i < 1000; i++ {
		if b.Refusing() {
			t.Fatal("Refusing says true on a breaker that let nothing through")
		}
	}
	mustPass(t, b, 300)
	if logged.Len() != 0 {
		t.Fatalf("a line was written before anything was refused:\n%s", logged.String())
	}
	for i := 0; i < 5; i++ {
		if !b.Refusing() {
			t.Fatal("Refusing says false after 300 sends in the hour")
		}
	}
	if inner.count() != 300 {
		t.Fatalf("%d sends reached the wrapped sender, want 300", inner.count())
	}
	if lines := logLines(t, &logged); len(lines) != 1 || lines[0]["level"] != "WARN" {
		t.Fatalf("want exactly the trip line after Refusing refused, got %d line(s):\n%s", len(lines), logged.String())
	}
}

// logLines decodes one JSON object per line.
func logLines(t *testing.T, w *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(w.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a log line is not JSON: %v\n%s", err, line)
		}
		out = append(out, rec)
	}
	return out
}

// keysOf is the set of a line's keys.
func keysOf(rec map[string]any) map[string]bool {
	out := map[string]bool{}
	for k := range rec {
		out[k] = true
	}
	return out
}

// TestBreaker_LogsTheTripOnceAndTheCloseOnce is K7A-6: one Warn line when the breaker
// first refuses, none for the refusals after it, one Info line once a whole window has
// passed with no refusal — each carrying only counters and the window. The messages
// sent through it carry an address, a subject, two bodies, a ref and a tenant's name,
// and none of them reaches the log (the breaker never reads a message).
func TestBreaker_LogsTheTripOnceAndTheCloseOnce(t *testing.T) {
	t.Parallel()
	const (
		to      = "breaker-probe@tenant.example.test"
		subject = "Join Kebab Probe Factory on Taptime"
		tenant  = "Kebab Probe Factory"
		text    = "Kebab Probe Factory invited you: https://taptime.test/activate?code=PROBECODE"
		ref     = "invite:4f1c0a52-probe"
	)
	m := mail.Message{To: to, Subject: subject, Text: text, HTML: "<p>" + text + "</p>", Ref: ref}
	c := newClock()
	t0 := c.now()
	var logged bytes.Buffer
	b, inner := newBreaker(t, c, &logged)

	for i := 0; i < 300; i++ {
		if _, err := b.Send(context.Background(), m); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	for i := 0; i < 40; i++ {
		c.set(t0.Add(time.Duration(i) * time.Minute)) // refusals keep coming for 39 minutes
		mustRefuse(t, b, inner)
		_, _ = b.Send(context.Background(), m) // one more, with the probe message
		b.Refusing()
	}
	lines := logLines(t, &logged)
	if len(lines) != 1 {
		t.Fatalf("%d line(s) after 120 refusals, want exactly the one trip line:\n%s", len(lines), logged.String())
	}
	trip := lines[0]
	wantTrip := map[string]bool{"time": true, "level": true, "msg": true, "limit": true, "window": true}
	if got := keysOf(trip); len(got) != len(wantTrip) || !subset(got, wantTrip) {
		t.Errorf("the trip line's keys are %v, want exactly %v", got, wantTrip)
	}
	if trip["level"] != "WARN" || trip["limit"] != float64(300) || trip["window"] != "1h0m0s" {
		t.Errorf("the trip line is %v", trip)
	}

	// An hour after the LAST refusal (t0+39m), the next call closes the stretch.
	c.set(t0.Add(39*time.Minute + time.Hour - time.Nanosecond))
	if _, err := b.Send(context.Background(), m); err != nil {
		t.Fatalf("a send an hour after the window filled was refused: %v", err)
	}
	if n := len(logLines(t, &logged)); n != 1 {
		t.Fatalf("a close line was written before a whole window passed without a refusal (%d lines)", n)
	}
	c.set(t0.Add(39*time.Minute + time.Hour))
	if _, err := b.Send(context.Background(), m); err != nil {
		t.Fatalf("send: %v", err)
	}
	lines = logLines(t, &logged)
	if len(lines) != 2 {
		t.Fatalf("%d line(s), want the trip and one close line:\n%s", len(lines), logged.String())
	}
	closed := lines[1]
	wantClose := map[string]bool{"time": true, "level": true, "msg": true, "limit": true, "window": true, "refused": true}
	if got := keysOf(closed); len(got) != len(wantClose) || !subset(got, wantClose) {
		t.Errorf("the close line's keys are %v, want exactly %v", got, wantClose)
	}
	if closed["level"] != "INFO" || closed["refused"] != float64(120) {
		t.Errorf("the close line is %v, want INFO with refused=120", closed)
	}

	all := logged.String()
	for what, v := range map[string]string{"address": to, "subject": subject, "tenant name": tenant,
		"body": "activate?code=", "code": "PROBECODE", "ref": ref, "domain": "tenant.example.test"} {
		if strings.Contains(strings.ToLower(all), strings.ToLower(v)) {
			t.Errorf("the %s reached the log:\n%s", what, all)
		}
	}
	// CONTROL: the scan reads a log that does hold the lines it judges.
	if !strings.Contains(all, "mail breaker") {
		t.Fatal("CONTROL FAILED: the scanned log holds no breaker line")
	}
}

func subset(got, want map[string]bool) bool {
	for k := range got {
		if !want[k] {
			return false
		}
	}
	return true
}

// TestBreaker_ASustainedOverloadTripsOnce: three hours at twice the limit (ten sends a
// minute) write ONE trip line — a load hovering above the limit frees a slot a minute
// and refuses the next send, and that must not become a pair of lines per slot — then,
// after an hour with no send at all, the next send writes one close line.
func TestBreaker_ASustainedOverloadTripsOnce(t *testing.T) {
	t.Parallel()
	c := newClock()
	t0 := c.now()
	var logged bytes.Buffer
	b, inner := newBreaker(t, c, &logged)
	passed, refused := 0, 0
	for minute := 0; minute < 180; minute++ {
		c.set(t0.Add(time.Duration(minute) * time.Minute))
		for i := 0; i < 10; i++ {
			if _, err := b.Send(context.Background(), validMessage()); err == nil {
				passed++
			} else {
				refused++
			}
		}
	}
	if passed != inner.count() || refused == 0 || passed > 3*300 {
		t.Fatalf("passed %d (wrapped sender saw %d), refused %d: want refusals and at most 900 passed", passed, inner.count(), refused)
	}
	if lines := logLines(t, &logged); len(lines) != 1 {
		t.Fatalf("%d line(s) over three hours of overload, want exactly one trip line:\n%s", len(lines), logged.String())
	}
	c.set(t0.Add(179*time.Minute + time.Hour))
	mustPass(t, b, 1)
	lines := logLines(t, &logged)
	if len(lines) != 2 || lines[1]["level"] != "INFO" || lines[1]["refused"] != float64(refused) {
		t.Fatalf("want the trip and one close line counting %d refusals, got:\n%s", refused, logged.String())
	}
}

// TestNewBreaker_RefusesANilSender: no breaker around nothing; and a Breaker that
// NewBreaker did not build sends nothing.
func TestNewBreaker_RefusesANilSender(t *testing.T) {
	t.Parallel()
	if _, err := mail.NewBreaker(nil, mail.BreakerConfig{}); err == nil {
		t.Error("NewBreaker accepted a nil sender")
	}
	for name, b := range map[string]*mail.Breaker{"nil": nil, "zero": {}} {
		if _, err := b.Send(context.Background(), validMessage()); !errors.Is(err, mail.ErrNotConfigured) {
			t.Errorf("%s Breaker: Send = %v, want ErrNotConfigured", name, err)
		}
		if _, err := b.SendExempt(context.Background(), validMessage()); !errors.Is(err, mail.ErrNotConfigured) {
			t.Errorf("%s Breaker: SendExempt = %v, want ErrNotConfigured", name, err)
		}
		if b.Refusing() {
			t.Errorf("%s Breaker: Refusing = true", name)
		}
	}
}
