package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The per-mailbox cap's state, bound and digest, read from INSIDE the package: what it
// keeps in place of an address, what it forgets past its bound, and that its key never
// reaches the log. The behaviour as a caller sees it is recipientcap_test.go's.

// stepClock is an injected clock a test moves by hand.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *stepClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *stepClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// acceptAll is the transport these tests wrap: it accepts everything and keeps nothing.
type acceptAll struct{}

func (acceptAll) Send(context.Context, Message) (Receipt, error) { return Receipt{}, nil }

// capOn builds a cap around a breaker around acceptAll, on clk, logging JSON to w.
func capOn(t *testing.T, clk *stepClock, w *bytes.Buffer) *RecipientCap {
	t.Helper()
	var h slog.Handler = slog.DiscardHandler
	if w != nil {
		h = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	b, err := NewBreaker(acceptAll{}, BreakerConfig{Now: clk.now, Log: slog.New(h)})
	if err != nil {
		t.Fatal(err)
	}
	rc, err := NewRecipientCap(b)
	if err != nil {
		t.Fatal(err)
	}
	return rc
}

// strandsOf walks v by reflection — unexported fields, pointers, interfaces, slices,
// arrays, maps (keys and values), following each pointer once — and returns every
// string it holds and every byte sequence (a []byte, or an array of bytes).
func strandsOf(v reflect.Value) (strs []string, raw [][]byte) {
	seen := map[uintptr]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.String:
			strs = append(strs, v.String())
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			walk(v.Elem())
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		case reflect.Slice, reflect.Array:
			if v.Kind() == reflect.Slice && v.IsNil() {
				return
			}
			if v.Type().Elem().Kind() == reflect.Uint8 {
				b := make([]byte, v.Len())
				for i := range b {
					b[i] = byte(v.Index(i).Uint())
				}
				raw = append(raw, b)
				return
			}
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			if v.IsNil() {
				return
			}
			it := v.MapRange()
			for it.Next() {
				walk(it.Key())
				walk(it.Value())
			}
		}
	}
	walk(v)
	return strs, raw
}

// testScope is the scope these tests send under — a business id, by the invitation
// route's convention; the state scan looks for it too.
const testScope = "9e1f2c3d-0000-4000-8000-0000000000ee"

// sendsAndOneRefusal sends RecipientHourLimit messages of testScope that must pass and
// one that must be refused as recipient_cap, cycling through the spellings of one
// mailbox.
func sendsAndOneRefusal(t *testing.T, rc *RecipientCap, spellings []string) {
	t.Helper()
	for i := 0; i <= RecipientHourLimit; i++ {
		_, err := rc.Send(context.Background(), testScope, Message{To: spellings[i%len(spellings)]})
		var se *SendError
		switch {
		case i < RecipientHourLimit && err != nil:
			t.Fatalf("send %d was refused: %v", i+1, err)
		case i == RecipientHourLimit && (!errors.As(err, &se) || se.Class != ClassRecipientCap):
			t.Fatalf("send %d: err = %v, want the cap's refusal", i+1, err)
		}
	}
}

// TestRecipientCap_HoldsADigestAndNeverTheAddress is K7C-2's storage rule: after sends
// to one mailbox in three spellings (and a refusal), the cap's whole state — walked by
// reflection, the breaker it wraps included — holds no string and no byte sequence
// carrying the address, its lower-cased or normalised form, its local part, its tag or
// its domain. CONTROL: the walk does reach the mailbox map, because it finds the
// mailbox's digest there.
func TestRecipientCap_HoldsADigestAndNeverTheAddress(t *testing.T) {
	t.Parallel()
	clk := &stepClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	rc := capOn(t, clk, nil)
	spellings := []string{"Maria.Borg+Payroll@Example.Test", "maria.borg@example.test", "MARIA.BORG+X@EXAMPLE.TEST"}
	sendsAndOneRefusal(t, rc, spellings)
	if !rc.Capped(testScope, spellings[0]) {
		t.Fatal("CONTROL FAILED: the mailbox's count is not full")
	}

	strs, raw := strandsOf(reflect.ValueOf(rc))
	forbidden := []string{"maria.borg+payroll@example.test", "maria.borg@example.test", "maria.borg", "payroll", "example.test", testScope}
	for _, s := range strs {
		for _, f := range forbidden {
			if strings.Contains(strings.ToLower(s), f) {
				t.Errorf("the cap holds a string carrying %q", f)
			}
		}
	}
	for _, b := range raw {
		for _, f := range forbidden {
			if bytes.Contains(bytes.ToLower(b), []byte(f)) {
				t.Errorf("the cap holds bytes carrying %q", f)
			}
		}
	}
	digest := rc.digest(testScope, spellings[1])
	found := false
	for _, b := range raw {
		if bytes.Equal(b, digest[:]) {
			found = true
		}
	}
	if !found {
		t.Fatal("CONTROL FAILED: the walk never reached the mailbox's digest, so it proves nothing about the map")
	}
	if len(rc.boxes) != 1 {
		t.Fatalf("%d mailbox(es) for three spellings of one address, want 1", len(rc.boxes))
	}
}

// TestRecipientCap_TheDigestNeverReachesTheLog is K7C-6's other half: through a whole
// stretch — sends, a refusal, the question, the quiet hour — the log holds neither the
// mailbox's digest (hex in either case, base64 in either alphabet, or its raw bytes)
// nor any part of the address.
func TestRecipientCap_TheDigestNeverReachesTheLog(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	clk := &stepClock{t: t0}
	var logged bytes.Buffer
	rc := capOn(t, clk, &logged)
	const addr = "Leak.Probe+Tag@Probe.Example.Test"
	sendsAndOneRefusal(t, rc, []string{addr, strings.ToLower(addr)})
	rc.Capped(testScope, addr)
	clk.set(t0.Add(2 * time.Hour))
	rc.Capped(testScope, "someone-else@example.test")
	if !strings.Contains(logged.String(), "mail recipient cap") || strings.Count(logged.String(), "\n") != 2 {
		t.Fatalf("CONTROL FAILED: want the first-refusal and the quiet line, got:\n%s", logged.String())
	}
	d := rc.digest(testScope, addr)
	if bytes.Contains(logged.Bytes(), d[:]) {
		t.Errorf("the raw digest reached the log")
	}
	all := strings.ToLower(logged.String())
	for what, v := range map[string]string{
		"the hex digest":                hex.EncodeToString(d[:]),
		"the base64 digest":             base64.StdEncoding.EncodeToString(d[:]),
		"the base64url digest":          base64.RawURLEncoding.EncodeToString(d[:]),
		"the digest's first bytes, hex": hex.EncodeToString(d[:4]),
		"the address":                   addr,
		"the normalised form":           "leak.probe@probe.example.test",
		"the local part":                "leak.probe",
		"the domain":                    "probe.example.test",
	} {
		if strings.Contains(all, strings.ToLower(v)) {
			t.Errorf("%s reached the log:\n%s", what, logged.String())
		}
	}
}

// TestRecipientCap_ForgetsTheStalestMailboxPastItsBound is K7C-2's memory bound, with
// the bound lowered to three: a fourth mailbox forgets the one sent to LONGEST AGO (so
// it may be sent to again — the fail-open direction) and the map stays at three; a
// mailbox whose newest send is a day old is forgotten without any pressure.
func TestRecipientCap_ForgetsTheStalestMailboxPastItsBound(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	clk := &stepClock{t: t0}
	rc := capOn(t, clk, nil)
	rc.max = 3
	send := func(addr string) error {
		_, err := rc.Send(context.Background(), testScope, Message{To: addr})
		return err
	}
	for i := 0; i < 5; i++ {
		if err := send("a@bound.example.test"); err != nil {
			t.Fatal(err)
		}
	}
	for i, addr := range []string{"b@bound.example.test", "c@bound.example.test"} {
		clk.set(t0.Add(time.Duration(i+1) * time.Minute))
		if err := send(addr); err != nil {
			t.Fatal(err)
		}
	}
	if !rc.Capped(testScope, "a@bound.example.test") || len(rc.boxes) != 3 {
		t.Fatalf("CONTROL FAILED: a is not full or the map holds %d", len(rc.boxes))
	}
	clk.set(t0.Add(3 * time.Minute))
	if err := send("d@bound.example.test"); err != nil {
		t.Fatal(err)
	}
	if len(rc.boxes) != 3 || rc.order.Len() != 3 {
		t.Fatalf("past its bound the cap holds %d mailbox(es) (%d in order), want 3", len(rc.boxes), rc.order.Len())
	}
	if _, kept := rc.boxes[rc.digest(testScope, "a@bound.example.test")]; kept {
		t.Error("the mailbox sent to longest ago was not the one forgotten")
	}
	for _, addr := range []string{"b@bound.example.test", "c@bound.example.test", "d@bound.example.test"} {
		if _, kept := rc.boxes[rc.digest(testScope, addr)]; !kept {
			t.Errorf("%s was forgotten instead of the stalest", addr)
		}
	}
	if rc.Capped(testScope, "a@bound.example.test") {
		t.Error("a forgotten mailbox is still refused")
	}

	clk.set(t0.Add(3*time.Minute + RecipientDayWindow))
	rc.Capped(testScope, "anyone@bound.example.test")
	if len(rc.boxes) != 0 || rc.order.Len() != 0 {
		t.Errorf("a day after every send the cap still holds %d mailbox(es)", len(rc.boxes))
	}
}

// TestRecipientCap_TheBoundOutlastsADayOfTheBreaker holds recipientCapEntries above
// what the breaker can let through in a RecipientDayWindow: the cap records only sends
// the breaker let through, so with the bound above that number the mailbox forgotten
// for room is always one whose newest send is already a day old — the bound never
// lifts a live ceiling. It also measures what a FULL map costs and logs it.
func TestRecipientCap_TheBoundOutlastsADayOfTheBreaker(t *testing.T) {
	perDay := BreakerLimit * int(RecipientDayWindow/BreakerWindow)
	if recipientCapEntries <= perDay {
		t.Fatalf("recipientCapEntries %d does not exceed a day of the breaker (%d sends)", recipientCapEntries, perDay)
	}
	clk := &stepClock{t: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}
	rc := capOn(t, clk, nil)
	if rc.max != recipientCapEntries {
		t.Fatalf("a fresh cap is bounded at %d, want recipientCapEntries (%d)", rc.max, recipientCapEntries)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	rc.mu.Lock()
	for i := 0; i < recipientCapEntries; i++ {
		k := rc.digest(testScope, fmt.Sprintf("m%d@bound.example.test", i))
		for s := 0; s < RecipientDayLimit; s++ {
			rc.record(k, clk.now())
		}
	}
	rc.mu.Unlock()
	runtime.GC()
	runtime.ReadMemStats(&after)
	held := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("a full cap (%d keys x %d sends) holds about %.1f MiB of heap",
		recipientCapEntries, RecipientDayLimit, float64(held)/(1<<20))
	if held > 16<<20 {
		t.Errorf("a full cap holds %d bytes, over the 16 MiB this bound was sized for", held)
	}
	runtime.KeepAlive(rc)
}
