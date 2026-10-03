package mail_test

import (
	"bytes"
	"context"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// replyCap mirrors mail's unexported maxReplyBytes for the sizes and budgets
// below. What a grown product cap turns red, measured (EM-2 round 3): at 2x, the
// split row of TestSend_TheReplyCapCoversTheWholeAttempt and
// TestSend_ACutReplyToTheEndOfDataIsAcceptance (their sizes are set against this
// constant), while the heap budget of TestSend_CapsWhatTheRelayCanMakeItRead
// stays green; at 4x, that heap budget too (5.0-6.5 MiB allocated against 4 MiB).
const replyCap = 256 << 10

// floodSize is what the hostile relay sends: 16 times the cap.
const floodSize = 16 * replyCap

// heapPeak returns the bytes allocated while fn runs (runtime.MemStats.TotalAlloc,
// cumulative, garbage included — every process goroutine counts, the fake
// relay's too) and, for the log only, the largest sampled increase of the live
// heap. The ASSERTION is on the allocated figure: it is deterministic and an
// upper bound on what the send could have held at once. The sampled peak is
// printed but not asserted — a 200 µs sampler can miss a peak that lasts a
// millisecond, which is how long a capped send takes.
func heapPeak(fn func()) (peak, allocated uint64) {
	const live = "/memory/classes/heap/objects:bytes"
	sample := []metrics.Sample{{Name: live}}
	read := func() uint64 { metrics.Read(sample); return sample[0].Value.Uint64() }
	runtime.GC()
	base := read()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var max uint64
	wg.Add(1)
	go func() {
		defer wg.Done()
		s := []metrics.Sample{{Name: live}}
		for {
			metrics.Read(s)
			if v := s[0].Value.Uint64(); v > max {
				max = v
			}
			select {
			case <-stop:
				return
			case <-time.After(200 * time.Microsecond):
			}
		}
	}()
	fn()
	close(stop)
	wg.Wait()
	runtime.ReadMemStats(&after)
	if max > base {
		peak = max - base
	}
	return peak, after.TotalAlloc - before.TotalAlloc
}

// TestSend_CapsWhatTheRelayCanMakeItRead: a relay (or anyone answering TCP at its
// address — no certificate is needed before TLS) that sends 16 times the reply
// cap — as one line without a line end, as continuation lines, before TLS or
// through it — ends the attempt as network with no reply code, long before the
// deadline, and the send allocates at most 16 times the cap in all (handshake
// included). Without the cap the same relay held the process to the deadline
// (security audit: 120 MiB of greeting took Sys to 545-562 MiB against a 512Mi
// pod limit).
func TestSend_CapsWhatTheRelayCanMakeItRead(t *testing.T) {
	lines := func(prefix string, lineLen int) []byte {
		var b bytes.Buffer
		body := strings.Repeat("a", lineLen)
		for b.Len() < floodSize {
			b.WriteString(prefix + body + "\r\n")
		}
		return b.Bytes()
	}
	tests := []struct {
		name, at string
		flood    []byte
	}{
		{"greeting: one line, no line end", "greeting", append([]byte("220 "), bytes.Repeat([]byte("a"), floodSize)...)},
		{"greeting: 1 KiB continuation lines", "greeting", lines("220-", 1<<10)},
		{"greeting: empty continuation lines", "greeting", lines("220-", 0)},
		{"EHLO reply before TLS: 1 KiB continuation lines", "ehlo", lines("250-", 1<<10)},
		{"end-of-data reply through TLS: 1 KiB continuation lines", "end", lines("250-", 1<<10)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, always(fakeConfig{floodAt: tt.at, flood: tt.flood}))
			s, _ := newSender(t, f, func(c *mail.Config) { c.Timeout = 5 * time.Second })
			var err error
			var took time.Duration
			peak, allocated := heapPeak(func() {
				_, took, err = sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
			})
			t.Logf("flood %d B: returned after %v, live heap peak +%d KiB, allocated %d KiB", len(tt.flood), took, peak>>10, allocated>>10)
			sendError(t, err, mail.ClassNetwork, 0)
			if took > 2*time.Second {
				t.Fatalf("returned after %v: the deadline, not the cap, ended the attempt", took)
			}
			if allocated > 16*replyCap {
				t.Fatalf("the send allocated %d KiB; the budget is 16x the cap (%d KiB)", allocated>>10, 16*replyCap>>10)
			}
		})
	}
}

// TestSend_ANetworkFailureIsNotRetried: only a 4xx is retried. A connection the
// relay drops after the message's terminating dot (the 250 may have been lost —
// a retry could deliver twice) and one it resets in the middle of DATA both end
// as network after exactly one connection.
func TestSend_ANetworkFailureIsNotRetried(t *testing.T) {
	for _, hang := range []string{"end-drop", "data-rst"} {
		t.Run(hang, func(t *testing.T) {
			f := newFake(t, always(fakeConfig{hang: hang}))
			s, _ := newSender(t, f, nil)
			m := validMessage()
			// Large enough that the client is still writing when the reset arrives.
			m.Text = strings.Repeat("Il-link tiegħek jinsab hawn.\n", 1<<15)
			_, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m)
			sendError(t, err, mail.ClassNetwork, 0)
			time.Sleep(100 * time.Millisecond) // a second dial, if any, would land by now
			if n := f.accepted(); n != 1 {
				t.Fatalf("connections = %d, want 1", n)
			}
		})
	}
}

// TestSend_TheReplyCapCoversTheWholeAttempt: the one counter runs across STARTTLS
// and through the TLS handshake. 200 KiB of valid EHLO before TLS plus 100 KiB of
// valid reply after it ends as network although neither half alone does (the
// controls), so the counter is not reset at STARTTLS; and a relay that leaves
// the client 64 bytes of budget when the handshake starts ends as network with
// the handshake unfinished at the server — the cap, not TLS, decided it — while
// 64 KiB of budget is enough to finish the send (control).
func TestSend_TheReplyCapCoversTheWholeAttempt(t *testing.T) {
	// The bytes the fake sends before TLS besides its EHLO reply: the greeting
	// and the 220 to STARTTLS (fakesmtp_test.go writes exactly these).
	const preTLS = len("220 fake.test ESMTP\r\n") + len("220 2.0.0 ready to start TLS\r\n")
	tests := []struct {
		name      string
		cfg       fakeConfig
		wantClass mail.Class // "" means the send succeeds
		handshake bool       // whether the server completes the TLS handshake
	}{
		{"split: 200 KiB EHLO before TLS + 100 KiB reply after it", fakeConfig{ehloPad: 200 << 10, endPad: 100 << 10}, mail.ClassNetwork, true},
		{"control: the 200 KiB EHLO alone", fakeConfig{ehloPad: 200 << 10}, "", true},
		{"control: the 100 KiB reply alone", fakeConfig{endPad: 100 << 10}, "", true},
		{"inside the handshake: 64 bytes of budget when it starts", fakeConfig{ehloPad: replyCap - preTLS - 64}, mail.ClassNetwork, false},
		{"control: 64 KiB of budget when the handshake starts", fakeConfig{ehloPad: replyCap - preTLS - 64<<10}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, always(tt.cfg))
			s, _ := newSender(t, f, nil)
			_, took, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
			if tt.wantClass == "" {
				if err != nil {
					t.Fatalf("control: Send: %v", err)
				}
			} else {
				sendError(t, err, tt.wantClass, 0)
				if took > 2*time.Second {
					t.Fatalf("returned after %v: the deadline, not the cap, ended the attempt", took)
				}
			}
			sess := f.session(0)
			done := false
			for _, v := range sess.verbs {
				done = done || v == "TLS"
			}
			if done != tt.handshake {
				t.Fatalf("server completed the handshake = %v, want %v (conversation %v)", done, tt.handshake, sess.verbs)
			}
			if f.accepted() != 1 {
				t.Fatalf("connections = %d, want 1 (network is not retried)", f.accepted())
			}
		})
	}
}

// TestSend_ACutReplyToTheEndOfDataIsAcceptance pins a decision (ADR 0022, EM-2
// round 3): when the line the cap cuts is the 250 reply to the end of data, the
// relay DID send "250 " — the message was accepted — so Send reports success.
// The id comes from the cut line under messageID's rules: a 300 KiB line is far
// longer than an id may be, so MessageID is "". Reporting network instead would
// record a delivered message as undelivered. (net/textproto hands back the cut
// line without an error — bufio.Reader.ReadLine drops the error when it has
// data — and nothing is read after the 250.)
func TestSend_ACutReplyToTheEndOfDataIsAcceptance(t *testing.T) {
	cut := append([]byte("250 Ok "), bytes.Repeat([]byte("a"), 300<<10)...)
	f := newFake(t, always(fakeConfig{floodAt: "end", flood: cut}))
	s, _ := newSender(t, f, nil)
	r, took, err := sendWithin(context.Background(), t, 10*time.Second, s, validMessage())
	if err != nil {
		t.Fatalf("Send = %v; a cut 250 is acceptance", err)
	}
	if r.MessageID != "" {
		t.Fatalf("MessageID = %q, want empty", r.MessageID)
	}
	if took > 2*time.Second {
		t.Fatalf("returned after %v: the deadline, not the cap, ended the read", took)
	}
}
