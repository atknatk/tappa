package brand

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// logoPauser is a stageHook that records every stage it sees and stops the first call
// that reaches stage at until resume is closed.
type logoPauser struct {
	at      logoStage
	reached chan struct{}
	resume  chan struct{}
	once    sync.Once
	mu      sync.Mutex
	seen    map[logoStage]int
}

func logoNewPauser(at logoStage) *logoPauser {
	return &logoPauser{at: at, reached: make(chan struct{}), resume: make(chan struct{}), seen: map[logoStage]int{}}
}

func (p *logoPauser) hook(s logoStage) {
	p.mu.Lock()
	p.seen[s]++
	p.mu.Unlock()
	if s != p.at {
		return
	}
	first := false
	p.once.Do(func() { first = true })
	if first {
		close(p.reached)
		<-p.resume
	}
}

func (p *logoPauser) count(s logoStage) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen[s]
}

type logoResult struct {
	logo Logo
	err  error
}

func logoGo(g *LogoGate, ctx context.Context, data []byte) <-chan logoResult {
	done := make(chan logoResult, 1)
	go func() {
		l, err := g.Normalize(ctx, bytes.NewReader(data))
		done <- logoResult{l, err}
	}()
	return done
}

// logoProbe is a second upload while the gate is held: it must come back ErrLogoBusy at
// once. A probe that waits for the slot fails after five seconds instead of hanging.
func logoProbe(t *testing.T, g *LogoGate, data []byte) error {
	t.Helper()
	select {
	case r := <-logoGo(g, context.Background(), data):
		return r.err
	case <-time.After(5 * time.Second):
		t.Fatal("the probe waited for the slot; a full gate must refuse at once")
		return nil
	}
}

func logoWait(t *testing.T, ch <-chan logoResult) logoResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(60 * time.Second):
		t.Fatal("the held call did not return")
		return logoResult{}
	}
}

// TestLogoGate_SlotHeldThroughDecodeResizeEncode stops a call at each stage inside the
// slot — just after the slot is taken, after the decode, after the box filter, after the
// encode and just before the slot is given back — and sends a second upload from
// outside: at every stage it is refused as ErrLogoBusy without taking the slot. After
// the held call returns, the same upload is normalized.
func TestLogoGate_SlotHeldThroughDecodeResizeEncode(t *testing.T) {
	held := imgEncodeJPEG(t, imgQuadrants(900, 700), 85)
	probe := imgEncodePNG(t, imgQuadrants(40, 40))
	for _, stage := range []logoStage{logoStageAcquired, logoStageDecoded, logoStageResized, logoStageEncoded, logoStageReleasing} {
		g := logoNewGate(t, 1)
		p := logoNewPauser(stage)
		g.stageHook = p.hook
		done := logoGo(g, context.Background(), held)
		<-p.reached
		if err := logoProbe(t, g, probe); !errors.Is(err, ErrLogoBusy) {
			t.Errorf("stage %d: probe err = %v, want ErrLogoBusy", stage, err)
		}
		if n := p.count(logoStageAcquired); n != 1 {
			t.Errorf("stage %d: %d calls took the slot", stage, n)
		}
		close(p.resume)
		if r := logoWait(t, done); r.err != nil {
			t.Fatalf("stage %d: held call: %v", stage, r.err)
		}
		if err := logoProbe(t, g, probe); err != nil {
			t.Errorf("stage %d: after the held call returned, the probe was refused: %v", stage, err)
		}
	}
}

// TestLogoGate_FullGateRefusesBeforeDecoding: with the slot held, three uploads are
// refused as ErrLogoBusy without reaching a stage inside the slot:
//   - the most expensive file to decode the gate admits (2048×2048 progressive 4:4:4
//     CMYK, about 96 MiB to decode), in well under a second and within 1 MiB;
//   - a paletted PNG with 37 000 empty tEXt chunks, whose inspection alone allocates
//     about 145 MiB (image/png's DecodeConfig reads them), within 1 MiB — the inspection
//     did not run;
//   - an SVG, as ErrLogoBusy rather than ErrLogoFormat.
//
// The 1 MiB holds the read (logoRead: at most the 512 KiB limit plus 32 KiB,
// TestLogoRead_AllocatesBoundedByTheLimit). Released,
// the same two files go through and allocate at least 64 MiB each — the positive
// control that the refusals skipped real work.
func TestLogoGate_FullGateRefusesBeforeDecoding(t *testing.T) {
	f := imgCMYKForge(2048, 2048, 0, [4]int{10, 20, 30, 40})
	f.progressive, f.eobRun = true, 32767
	heavy := f.bytes()
	storm := imgPNGChunkStorm(t, 37000)
	if len(storm) > LogoMaxInputBytes {
		t.Fatalf("the storm is %d bytes, over the input limit", len(storm))
	}

	g := logoNewGate(t, 1)
	p := logoNewPauser(logoStageAcquired)
	g.stageHook = p.hook
	done := logoGo(g, context.Background(), imgEncodePNG(t, imgQuadrants(64, 64)))
	<-p.reached

	refuse := func(name string, data []byte) {
		var err error
		var took time.Duration
		n := logoAllocs(func() {
			start := time.Now()
			select {
			case r := <-logoGo(g, context.Background(), data):
				err = r.err
			case <-time.After(5 * time.Second):
				t.Fatalf("%s: the upload waited for the slot; a full gate must refuse at once", name)
			}
			took = time.Since(start)
		})
		if !errors.Is(err, ErrLogoBusy) {
			t.Errorf("%s: err = %v, want ErrLogoBusy", name, err)
		}
		t.Logf("%s, %d B, refused while full: %v, %d bytes allocated", name, len(data), took, n)
		if took > time.Second || n > 1<<20 {
			t.Errorf("%s: the refusal took %v and %d bytes; it did more than read the upload", name, took, n)
		}
	}
	refuse("2048² progressive CMYK", heavy)
	refuse("paletted PNG, 37 000 tEXt", storm)
	refuse("SVG", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	if p.count(logoStageAcquired) != 1 || p.count(logoStageDecoded) != 0 {
		t.Errorf("a refused call reached a stage inside the slot: %v", p.seen)
	}
	close(p.resume)
	if r := logoWait(t, done); r.err != nil {
		t.Fatal(r.err)
	}

	for name, data := range map[string][]byte{"2048² progressive CMYK": heavy, "paletted PNG, 37 000 tEXt": storm} {
		var err error
		control := logoAllocs(func() { _, err = g.Normalize(context.Background(), bytes.NewReader(data)) })
		if err != nil || control < 64<<20 {
			t.Fatalf("POSITIVE CONTROL FAILED: %s with the slot free: err %v, %d bytes", name, err, control)
		}
		t.Logf("positive control, slot free, %s: %d bytes allocated", name, control)
	}
}

// logoCancelAtEOF is an upload body whose request ends while it is read: the read
// that reaches EOF cancels the context first.
type logoCancelAtEOF struct {
	r      *bytes.Reader
	cancel func()
}

func (c logoCancelAtEOF) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if err == io.EOF {
		c.cancel()
	}
	return n, err
}

// TestLogoGate_ContextEndedDuringTheReadTakesNoSlot: a request whose context ends
// while its body is read gets context.Canceled without taking the slot — no stage
// inside the slot runs and the call allocates what the read does, within 1 MiB —
// though its body is
// the 2048×2048 progressive CMYK file that holds the slot for about a second when
// decoded. The positive control: the same body read without the cancellation takes the
// slot and is decoded (at least 64 MiB).
func TestLogoGate_ContextEndedDuringTheReadTakesNoSlot(t *testing.T) {
	f := imgCMYKForge(2048, 2048, 0, [4]int{10, 20, 30, 40})
	f.progressive, f.eobRun, f.acScans = true, 32767, LogoJPEGScanCeiling-1
	heavy := f.bytes()
	g := logoNewGate(t, 1)
	var acquired atomic.Int64
	g.stageHook = func(s logoStage) {
		if s == logoStageAcquired {
			acquired.Add(1)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var err error
	n := logoAllocs(func() {
		_, err = g.Normalize(ctx, logoCancelAtEOF{r: bytes.NewReader(heavy), cancel: cancel})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	t.Logf("ended during the read: %d bytes allocated, slot taken %d times", n, acquired.Load())
	if acquired.Load() != 0 || n > 1<<20 {
		t.Errorf("the ended request took the slot %d times and allocated %d bytes", acquired.Load(), n)
	}
	if len(g.slots) != 0 {
		t.Errorf("%d slots still taken", len(g.slots))
	}

	control := logoAllocs(func() {
		_, err = g.Normalize(context.Background(), logoCancelAtEOF{r: bytes.NewReader(heavy), cancel: func() {}})
	})
	if err != nil || acquired.Load() != 1 || control < 64<<20 {
		t.Fatalf("POSITIVE CONTROL FAILED: the same body read whole: err %v, slot taken %d times, %d bytes", err, acquired.Load(), control)
	}
}

// TestLogoGate_CancelledContextKeepsTheSlotUntilTheDecodeReturns is ADR 0024 §2.6's
// cancellation rule, twice. Held: the call is stopped after its decode, its context is
// cancelled, and for 200 ms every probe is refused; resumed, the call returns the
// context's error and the slot is free. Live: a 1024×1024 progressive decode with the
// ceiling's refinement scans (a few hundred milliseconds) runs unpaused, its context is
// cancelled as soon as it holds the slot, and probes sent every 10 ms until it returns
// are all refused.
func TestLogoGate_CancelledContextKeepsTheSlotUntilTheDecodeReturns(t *testing.T) {
	probe := imgEncodePNG(t, imgQuadrants(40, 40))

	t.Run("held", func(t *testing.T) {
		g := logoNewGate(t, 1)
		p := logoNewPauser(logoStageDecoded)
		g.stageHook = p.hook
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := logoGo(g, ctx, imgEncodeJPEG(t, imgQuadrants(900, 700), 85))
		<-p.reached
		cancel()
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			if err := logoProbe(t, g, probe); !errors.Is(err, ErrLogoBusy) {
				t.Fatalf("after cancel, before the call returned: probe err = %v, want ErrLogoBusy", err)
			}
			time.Sleep(5 * time.Millisecond)
		}
		close(p.resume)
		r := logoWait(t, done)
		if !errors.Is(r.err, context.Canceled) {
			t.Errorf("the cancelled call returned %v, want context.Canceled", r.err)
		}
		if p.count(logoStageResized) != 0 {
			t.Error("the cancelled call went on to the box filter")
		}
		if err := logoProbe(t, g, probe); err != nil {
			t.Errorf("after the call returned: %v", err)
		}
	})

	t.Run("live", func(t *testing.T) {
		f := imgGrayForge(1024, 1024, func(int, int) int { return 77 })
		f.progressive, f.eobRun, f.acScans, f.refineScans = true, 32767, 1, LogoJPEGScanCeiling-2
		heavy := f.bytes()
		g := logoNewGate(t, 1)
		started := make(chan struct{})
		var once sync.Once
		g.stageHook = func(s logoStage) {
			if s == logoStageAcquired {
				once.Do(func() { close(started) })
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		begin := time.Now()
		done := logoGo(g, ctx, heavy)
		<-started
		cancel()
		probes := 0
		for {
			select {
			case r := <-done:
				if !errors.Is(r.err, context.Canceled) {
					t.Errorf("the cancelled call returned %v, want context.Canceled", r.err)
				}
				t.Logf("decode ran %v; %d probes after cancel, all refused", time.Since(begin), probes)
				if probes == 0 {
					t.Fatal("no probe ran while the decode did; the case shows nothing")
				}
				if err := logoProbe(t, g, probe); err != nil {
					t.Errorf("after the call returned: %v", err)
				}
				return
			default:
			}
			err := logoProbe(t, g, probe)
			select {
			case r := <-done:
				// The call may have returned while the probe ran; the probe's answer
				// then says nothing about the slot during the decode.
				done = logoReplay(r)
				continue
			default:
			}
			if !errors.Is(err, ErrLogoBusy) {
				t.Fatalf("probe %d while the cancelled decode still ran: err = %v, want ErrLogoBusy", probes, err)
			}
			probes++
			time.Sleep(10 * time.Millisecond)
		}
	})
}

func logoReplay(r logoResult) <-chan logoResult {
	ch := make(chan logoResult, 1)
	ch <- r
	return ch
}

// TestLogoGate_ConcurrentDecodesNeverExceedN runs 16 goroutines of five uploads each
// against gates of one and three slots, counting the calls inside the slot from the
// stage hook (in at logoStageAcquired, out at logoStageReleasing). In the run the count
// stays at or under the gate's N; some uploads are refused as busy and some are
// normalized. Run under -race.
func TestLogoGate_ConcurrentDecodesNeverExceedN(t *testing.T) {
	data := imgEncodeJPEG(t, imgQuadrants(600, 400), 85)
	for _, n := range []int{1, 3} {
		g := logoNewGate(t, n)
		var inside, most, ok, busy atomic.Int64
		g.stageHook = func(s logoStage) {
			switch s {
			case logoStageAcquired:
				c := inside.Add(1)
				for {
					m := most.Load()
					if c <= m || most.CompareAndSwap(m, c) {
						break
					}
				}
				time.Sleep(time.Millisecond)
			case logoStageReleasing:
				inside.Add(-1)
			}
		}
		var wg sync.WaitGroup
		for w := 0; w < 16; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 5; i++ {
					_, err := g.Normalize(context.Background(), bytes.NewReader(data))
					switch {
					case err == nil:
						ok.Add(1)
					case errors.Is(err, ErrLogoBusy):
						busy.Add(1)
					default:
						t.Errorf("unexpected error: %v", err)
					}
				}
			}()
		}
		wg.Wait()
		t.Logf("N=%d: most inside %d, normalized %d, busy %d", n, most.Load(), ok.Load(), busy.Load())
		if most.Load() > int64(n) {
			t.Errorf("N=%d: %d calls were inside the slot at once", n, most.Load())
		}
		if ok.Load() == 0 || busy.Load() == 0 {
			t.Errorf("N=%d: normalized %d, busy %d; the run did not contend", n, ok.Load(), busy.Load())
		}
		if inside.Load() != 0 || len(g.slots) != 0 {
			t.Errorf("N=%d: %d still inside, %d slots still taken", n, inside.Load(), len(g.slots))
		}
	}
}

// TestLogoGate_ErrorPathsGiveTheSlotBack: after a refusal from inside the slot — a
// decode error, an output over 256 KiB, a context cancelled during the decode — the
// next upload on the same one-slot gate is normalized.
func TestLogoGate_ErrorPathsGiveTheSlotBack(t *testing.T) {
	good := imgEncodePNG(t, imgQuadrants(48, 48))
	corrupt := imgPNGFile(imgPNGIHDR(64, 64, 8, 6, 0), imgPNGChunk("IDAT", imgZlib(t, []byte{0, 1, 2})), imgPNGChunk("IEND", nil))
	noise := imgEncodePNG(t, imgNoise(400, 400, 3))
	g := logoNewGate(t, 1)
	steps := []struct {
		name string
		run  func() error
		want error
	}{
		{"decode error", func() error {
			_, err := g.Normalize(context.Background(), bytes.NewReader(corrupt))
			return err
		}, ErrLogoCorrupt},
		{"output over 256 KiB", func() error {
			_, err := g.Normalize(context.Background(), bytes.NewReader(noise))
			return err
		}, ErrLogoOutputTooLarge},
		{"cancelled during the decode", func() error {
			ctx, cancel := context.WithCancel(context.Background())
			g.stageHook = func(s logoStage) {
				if s == logoStageDecoded {
					cancel()
				}
			}
			defer func() { g.stageHook = nil }()
			_, err := g.Normalize(ctx, bytes.NewReader(good))
			return err
		}, context.Canceled},
	}
	for _, s := range steps {
		if err := s.run(); !errors.Is(err, s.want) {
			t.Fatalf("%s: err = %v, want %v", s.name, err, s.want)
		}
		if _, err := g.Normalize(context.Background(), bytes.NewReader(good)); err != nil {
			t.Errorf("after %s the next upload was refused: %v", s.name, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Normalize(ctx, bytes.NewReader(good)); !errors.Is(err, context.Canceled) {
		t.Errorf("an already cancelled context: err = %v", err)
	}
	if len(g.slots) != 0 {
		t.Errorf("%d slots still taken", len(g.slots))
	}
}

func TestNewLogoGate_RefusesFewerThanOneSlot(t *testing.T) {
	for _, n := range []int{0, -1} {
		if g, err := NewLogoGate(n); err == nil || g != nil {
			t.Errorf("NewLogoGate(%d) = %v, %v", n, g, err)
		}
	}
	g := logoNewGate(t, LogoDecodeSlots)
	if cap(g.slots) != 1 || LogoDecodeSlots != 1 {
		t.Errorf("LogoDecodeSlots = %d, gate capacity %d; ADR 0024 §2.6 derives N = 1", LogoDecodeSlots, cap(g.slots))
	}
}
