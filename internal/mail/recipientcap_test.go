package mail_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// The per-(scope, mailbox) cap (M10 EM-7C, ADR 0022's EM-7C note) over the REAL breaker
// over a countingSender, on the injected clock of breaker_test.go: what is measured is
// what reaches the transport. The state and log tests that need the cap's own key are
// in recipientcap_internal_test.go.

// The scopes these tests send under — two businesses, by the invitation route's
// convention.
const (
	scopeA = "5b0d6b8e-0000-4000-8000-00000000000a"
	scopeB = "5b0d6b8e-0000-4000-8000-00000000000b"
)

// newCap is a fresh cap around a fresh breaker around a countingSender, on c, logging to
// w (nil discards).
func newCap(t *testing.T, c *clock, w *bytes.Buffer) (*mail.RecipientCap, *mail.Breaker, *countingSender) {
	t.Helper()
	b, inner := newBreaker(t, c, w)
	rc, err := mail.NewRecipientCap(b)
	if err != nil {
		t.Fatalf("NewRecipientCap: %v", err)
	}
	return rc, b, inner
}

// to is validMessage addressed to addr.
func to(addr string) mail.Message {
	m := validMessage()
	m.To = addr
	return m
}

// capPass sends n messages of scope to addr that must all be let through.
func capPass(t *testing.T, rc *mail.RecipientCap, scope, addr string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := rc.Send(context.Background(), scope, to(addr)); err != nil {
			t.Fatalf("send %d of %d to the mailbox was refused: %v", i+1, n, err)
		}
	}
}

// capRefuse asserts one send of scope to addr is refused as recipient_cap, with no
// code, and never reaches the wrapped sender.
func capRefuse(t *testing.T, rc *mail.RecipientCap, inner *countingSender, scope, addr string) {
	t.Helper()
	before := inner.count()
	_, err := rc.Send(context.Background(), scope, to(addr))
	sendError(t, err, mail.ClassRecipientCap, 0)
	if inner.count() != before {
		t.Fatal("a send the cap refused reached the wrapped sender")
	}
}

// TestRecipientCap_TheFifthPassesAndTheSixthIsRefusedUntilTheHourPasses is K7C-1's
// hourly line: five sends of one scope to one mailbox in an hour pass, the sixth is
// refused before the breaker's sender is reached, and the refusal lasts exactly until
// the oldest of the five is an hour old. The counts are LITERALS, so a changed
// RecipientHourLimit turns this red.
func TestRecipientCap_TheFifthPassesAndTheSixthIsRefusedUntilTheHourPasses(t *testing.T) {
	t.Parallel()
	c := newClock()
	t0 := c.now()
	rc, _, inner := newCap(t, c, nil)
	const box = "maria.borg@example.test"

	capPass(t, rc, scopeA, box, 4)
	c.set(t0.Add(59 * time.Minute)) // the fifth, late in the same hour
	capPass(t, rc, scopeA, box, 1)
	if inner.count() != 5 {
		t.Fatalf("%d sends reached the wrapped sender, want 5", inner.count())
	}
	capRefuse(t, rc, inner, scopeA, box)

	c.set(t0.Add(time.Hour - time.Nanosecond))
	capRefuse(t, rc, inner, scopeA, box)

	// At t0+1h the four sent at t0 are an hour old: exactly four slots free.
	c.set(t0.Add(time.Hour))
	capPass(t, rc, scopeA, box, 4)
	capRefuse(t, rc, inner, scopeA, box)
	if inner.count() != 9 {
		t.Fatalf("%d sends reached the wrapped sender, want 9", inner.count())
	}
}

// TestRecipientCap_TheTwentiethInADayPassesAndTheTwentyFirstIsRefused is K7C-1's daily
// line: five sends an hour for four hours make twenty; an hour later the HOUR has room
// and the DAY has not, so the 21st is refused — until the first five are a day old.
func TestRecipientCap_TheTwentiethInADayPassesAndTheTwentyFirstIsRefused(t *testing.T) {
	t.Parallel()
	c := newClock()
	t0 := c.now()
	rc, _, inner := newCap(t, c, nil)
	const box = "daily@example.test"

	for h := 0; h < 4; h++ {
		c.set(t0.Add(time.Duration(h) * time.Hour))
		capPass(t, rc, scopeA, box, 5)
	}
	if inner.count() != 20 {
		t.Fatalf("%d sends reached the wrapped sender, want 20", inner.count())
	}
	c.set(t0.Add(5 * time.Hour)) // the last five are two hours old: the hour has room
	capRefuse(t, rc, inner, scopeA, box)
	c.set(t0.Add(24*time.Hour - time.Nanosecond))
	capRefuse(t, rc, inner, scopeA, box)

	c.set(t0.Add(24 * time.Hour)) // the five sent at t0 are a day old
	capPass(t, rc, scopeA, box, 5)
	capRefuse(t, rc, inner, scopeA, box)
	if inner.count() != 25 {
		t.Fatalf("%d sends reached the wrapped sender, want 25", inner.count())
	}
}

// TestRecipientCap_EachScopeHasItsOwnCount is EM-7C round 3's decision: two scopes —
// two businesses — never share a count, BOTH WAYS. With one scope's count full for a
// mailbox (five, the sixth refused, its question true), the other scope's question says
// false for the same mailbox and it takes all five of its own. Shared, one business's
// sixth press would tell it how many invitations another business had sent the address
// (an exact count), and twenty a day from one business would block another's.
func TestRecipientCap_EachScopeHasItsOwnCount(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{scopeA, scopeB}, {scopeB, scopeA}} {
		first, second := pair[0], pair[1]
		t.Run(first[len(first)-1:]+" then "+second[len(second)-1:], func(t *testing.T) {
			t.Parallel()
			rc, _, inner := newCap(t, newClock(), nil)
			const box = "Shared.Inbox+1@Example.Test"
			capPass(t, rc, first, box, 5)
			capRefuse(t, rc, inner, first, box)
			if !rc.Capped(first, "shared.inbox@example.test") {
				t.Fatal("CONTROL FAILED: the first scope's count is not full after five")
			}
			if rc.Capped(second, "shared.inbox@example.test") {
				t.Fatal("the other scope's question says true for a mailbox it never sent to: the scopes share a count")
			}
			capPass(t, rc, second, "shared.inbox+2@example.test", 5)
			capRefuse(t, rc, inner, second, box)
			if inner.count() != 10 {
				t.Fatalf("%d sends reached the wrapped sender, want ten — five of each scope", inner.count())
			}
		})
	}
}

// TestRecipientCap_TheShippedLimitsAreFiveAnHourAndTwentyADay pins the shipped numbers
// (ADR 0022's EM-7C note, K7C-1) and the class's word, which audit rows and log lines
// carry.
func TestRecipientCap_TheShippedLimitsAreFiveAnHourAndTwentyADay(t *testing.T) {
	t.Parallel()
	if mail.RecipientHourLimit != 5 || mail.RecipientHourWindow != time.Hour {
		t.Errorf("the cap ships %d per %v, want 5 per hour", mail.RecipientHourLimit, mail.RecipientHourWindow)
	}
	if mail.RecipientDayLimit != 20 || mail.RecipientDayWindow != 24*time.Hour {
		t.Errorf("the cap ships %d per %v, want 20 per 24 hours", mail.RecipientDayLimit, mail.RecipientDayWindow)
	}
	if mail.ClassRecipientCap != "recipient_cap" {
		t.Errorf("ClassRecipientCap is %q, want \"recipient_cap\"", mail.ClassRecipientCap)
	}
}

// TestRecipientCap_CaseAndAPlusTagAreOneMailbox is K7C-2's key: the whole address in
// lower case and the local part without its "+tag" — five spellings of one inbox fill
// it, a sixth spelling is refused; a different local part, domain or sub-domain is a
// different mailbox. The dotted and undotted local parts are TWO mailboxes here: a
// provider's aliases are not folded (a counted limit, and this row keeps it measured).
func TestRecipientCap_CaseAndAPlusTagAreOneMailbox(t *testing.T) {
	t.Parallel()
	rc, _, inner := newCap(t, newClock(), nil)
	for _, a := range []string{
		"Maria.Borg@Example.Test",
		"maria.borg+payroll@example.test",
		"MARIA.BORG+X@EXAMPLE.TEST",
		"maria.borg@example.test",
		"maria.borg+a+b@Example.test",
	} {
		capPass(t, rc, scopeA, a, 1)
	}
	for _, a := range []string{"maria.borg@example.test", "Maria.Borg+new@example.TEST", "maria.borg+@example.test"} {
		capRefuse(t, rc, inner, scopeA, a)
	}
	for _, a := range []string{
		"maria.borg@example.org",
		"mario.borg@example.test",
		"maria.borg@sub.example.test",
		"mariaborg@example.test", // a provider alias of the same inbox, maybe: not folded
	} {
		capPass(t, rc, scopeA, a, 1)
	}
}

// TestRecipientCap_ARefusalOfOneCeilingSpendsNothingOfTheOther is K7C-3's order: the
// cap is asked BEFORE the breaker and in the same locked step, so a send the cap
// refuses spends no breaker slot, and a send the breaker refuses is not recorded
// against its mailbox.
func TestRecipientCap_ARefusalOfOneCeilingSpendsNothingOfTheOther(t *testing.T) {
	t.Parallel()
	t.Run("400 cap refusals leave the breaker its 295 slots", func(t *testing.T) {
		t.Parallel()
		rc, _, inner := newCap(t, newClock(), nil)
		const box = "bombed@example.test"
		capPass(t, rc, scopeA, box, 5)
		for i := 0; i < 400; i++ {
			capRefuse(t, rc, inner, scopeA, box)
		}
		for i := 0; i < mail.BreakerLimit-5; i++ {
			capPass(t, rc, scopeA, fmt.Sprintf("other%d@example.test", i), 1)
		}
		_, err := rc.Send(context.Background(), scopeA, to("one-more@example.test"))
		sendError(t, err, mail.ClassBreaker, 0)
	})
	t.Run("25 breaker refusals leave the mailbox its five", func(t *testing.T) {
		t.Parallel()
		c := newClock()
		t0 := c.now()
		rc, _, inner := newCap(t, c, nil)
		for i := 0; i < mail.BreakerLimit; i++ {
			capPass(t, rc, scopeA, fmt.Sprintf("fill%d@example.test", i), 1)
		}
		c.set(t0.Add(time.Minute))
		const box = "late@example.test"
		for i := 0; i < 25; i++ {
			_, err := rc.Send(context.Background(), scopeA, to(box))
			sendError(t, err, mail.ClassBreaker, 0)
		}
		// An hour after the fill the breaker has room again; the mailbox had nothing
		// recorded, so it takes its five — had the 25 been recorded, its DAY would be
		// full until t0+1m+24h.
		c.set(t0.Add(time.Hour))
		capPass(t, rc, scopeA, box, 5)
		capRefuse(t, rc, inner, scopeA, box)
	})
}

// TestRecipientCap_ExactlyFivePassUnderConcurrency: 200 sends of one scope to one
// mailbox (in five spellings) started together on a fresh cap, 20 times — exactly five
// reach the wrapped sender every time and every other one is a recipient_cap refusal.
// Run under -race.
func TestRecipientCap_ExactlyFivePassUnderConcurrency(t *testing.T) {
	t.Parallel()
	spellings := []string{"crowd@example.test", "Crowd@Example.Test", "crowd+1@example.test", "CROWD+2@example.test", "crowd+x@EXAMPLE.test"}
	for run := 0; run < 20; run++ {
		rc, _, inner := newCap(t, newClock(), nil)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		capped, other := 0, 0
		for i := 0; i < 200; i++ {
			wg.Add(1)
			go func(addr string) {
				defer wg.Done()
				<-start
				_, err := rc.Send(context.Background(), scopeA, to(addr))
				var se *mail.SendError
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
				case errors.As(err, &se) && se.Class == mail.ClassRecipientCap && se.SMTPCode == 0:
					capped++
				default:
					other++
				}
			}(spellings[i%len(spellings)])
		}
		close(start)
		wg.Wait()
		if inner.count() != 5 || capped != 195 || other != 0 {
			t.Fatalf("run %d: %d passed, %d refused by the cap, %d other errors; want 5, 195, 0",
				run, inner.count(), capped, other)
		}
	}
}

// TestRecipientCap_CappedRecordsNothingAndIsARefusal: the question the invitation route
// asks takes no slot (1000 of them leave all five), answers true once the scope's count
// is full — in any spelling — and that answer is a refusal: it alone writes the
// first-refusal line. It asks only its own scope (another scope's question stays false)
// and only the cap (with the breaker full and the mailbox empty it answers false).
func TestRecipientCap_CappedRecordsNothingAndIsARefusal(t *testing.T) {
	t.Parallel()
	var logged bytes.Buffer
	rc, _, inner := newCap(t, newClock(), &logged)
	const box = "asked@example.test"
	for i := 0; i < 1000; i++ {
		if rc.Capped(scopeA, box) {
			t.Fatal("the question says true for a mailbox that was sent nothing")
		}
	}
	capPass(t, rc, scopeA, box, 5)
	if logged.Len() != 0 {
		t.Fatalf("a line was written before anything was refused:\n%s", logged.String())
	}
	for i := 0; i < 5; i++ {
		if !rc.Capped(scopeA, "Asked+Q@Example.Test") {
			t.Fatal("the question says false after five sends to the mailbox in the hour")
		}
	}
	if rc.Capped(scopeB, box) {
		t.Error("another scope's question says true for a mailbox it never sent to")
	}
	if inner.count() != 5 {
		t.Fatalf("%d sends reached the wrapped sender, want 5", inner.count())
	}
	if lines := logLines(t, &logged); len(lines) != 1 || lines[0]["level"] != "WARN" ||
		!strings.HasPrefix(fmt.Sprint(lines[0]["msg"]), "mail recipient cap:") {
		t.Fatalf("want exactly the cap's first-refusal line after the question refused, got:\n%s", logged.String())
	}
	for i := 0; i < mail.BreakerLimit-5; i++ {
		capPass(t, rc, scopeA, fmt.Sprintf("fill%d@example.test", i), 1)
	}
	if rc.Capped(scopeA, "fresh@example.test") {
		t.Error("the question answered true for an empty mailbox because the BREAKER is full: it asked the wrong ceiling")
	}
}

// TestRecipientCap_LogsTheFirstRefusalOnceAndTheQuietHourOnce is K7C-6: one Warn line at
// the cap's first refusal and none for the 79 after it, one Info line once a whole hour
// has passed with no refusal — limits and a counter only. The address, in every case
// and form, and the scope never reach the log.
func TestRecipientCap_LogsTheFirstRefusalOnceAndTheQuietHourOnce(t *testing.T) {
	t.Parallel()
	const box = "Cap-Probe+Tag@Tenant.Example.Test"
	c := newClock()
	t0 := c.now()
	var logged bytes.Buffer
	rc, _, inner := newCap(t, c, &logged)
	capPass(t, rc, scopeA, box, 5)
	for i := 0; i < 40; i++ {
		c.set(t0.Add(time.Duration(i) * time.Minute))
		capRefuse(t, rc, inner, scopeA, box)
		rc.Capped(scopeA, box)
	}
	lines := logLines(t, &logged)
	if len(lines) != 1 {
		t.Fatalf("%d line(s) after 80 refusals, want exactly the first-refusal line:\n%s", len(lines), logged.String())
	}
	first := lines[0]
	wantFirst := map[string]bool{"time": true, "level": true, "msg": true, "hour_limit": true, "day_limit": true}
	if got := keysOf(first); len(got) != len(wantFirst) || !subset(got, wantFirst) {
		t.Errorf("the first-refusal line's keys are %v, want exactly %v", got, wantFirst)
	}
	if first["level"] != "WARN" || first["hour_limit"] != float64(5) || first["day_limit"] != float64(20) {
		t.Errorf("the first-refusal line is %v", first)
	}

	// An hour after the LAST refusal (t0+39m) the next call writes the quiet line.
	c.set(t0.Add(39*time.Minute + time.Hour - time.Nanosecond))
	rc.Capped(scopeB, "someone-else@example.test")
	if n := len(logLines(t, &logged)); n != 1 {
		t.Fatalf("a quiet line was written before a whole hour passed without a refusal (%d lines)", n)
	}
	c.set(t0.Add(39*time.Minute + time.Hour))
	rc.Capped(scopeB, "someone-else@example.test")
	lines = logLines(t, &logged)
	if len(lines) != 2 {
		t.Fatalf("%d line(s), want the first-refusal and one quiet line:\n%s", len(lines), logged.String())
	}
	quiet := lines[1]
	wantQuiet := map[string]bool{"time": true, "level": true, "msg": true, "hour_limit": true, "day_limit": true, "refused": true}
	if got := keysOf(quiet); len(got) != len(wantQuiet) || !subset(got, wantQuiet) {
		t.Errorf("the quiet line's keys are %v, want exactly %v", got, wantQuiet)
	}
	if quiet["level"] != "INFO" || quiet["refused"] != float64(80) {
		t.Errorf("the quiet line is %v, want INFO with refused=80", quiet)
	}

	all := strings.ToLower(logged.String())
	for what, v := range map[string]string{"address": box, "normalised address": "cap-probe@tenant.example.test",
		"local part": "cap-probe", "tag": "+tag", "domain": "tenant.example.test", "scope": scopeA} {
		if strings.Contains(all, strings.ToLower(v)) {
			t.Errorf("the %s reached the log:\n%s", what, logged.String())
		}
	}
	// CONTROL: the scan reads a log that does hold the lines it judges.
	if !strings.Contains(all, "mail recipient cap") {
		t.Fatal("CONTROL FAILED: the scanned log holds no cap line")
	}
}

// TestNewRecipientCap_RefusesAMissingBreaker: no cap around nothing or around a Breaker
// NewBreaker did not build; and a cap NewRecipientCap did not build sends nothing.
func TestNewRecipientCap_RefusesAMissingBreaker(t *testing.T) {
	t.Parallel()
	if _, err := mail.NewRecipientCap(nil); err == nil {
		t.Error("NewRecipientCap accepted a nil breaker")
	}
	if _, err := mail.NewRecipientCap(&mail.Breaker{}); err == nil {
		t.Error("NewRecipientCap accepted a zero Breaker")
	}
	for name, rc := range map[string]*mail.RecipientCap{"nil": nil, "zero": {}} {
		if _, err := rc.Send(context.Background(), scopeA, validMessage()); !errors.Is(err, mail.ErrNotConfigured) {
			t.Errorf("%s cap: Send = %v, want ErrNotConfigured", name, err)
		}
		if rc.Capped(scopeA, "x@example.test") {
			t.Errorf("%s cap: the question answered true", name)
		}
	}
}
