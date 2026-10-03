//go:build linux || darwin

package brand

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// logoWorstFiles are the most expensive inputs to decode that Normalize admits, by ADR
// 0024 S7's measurements: 2048×2048 progressive 4:4:4 CMYK (96.02 MiB there) and
// 2048×2048 16-bit RGBA interlaced PNG (64.22 MiB), plus the two ordinary 2048² shapes.
func logoWorstFiles(t testing.TB) map[string][]byte {
	t.Helper()
	cmyk := imgCMYKForge(2048, 2048, 0, [4]int{10, 20, 30, 40})
	cmyk.progressive, cmyk.eobRun = true, 32767
	px := []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xFF, 0xFF}
	png16 := imgPNGForged(t, 2048, 2048, 16, 6, true, func(int, int) []byte { return px }, nil, nil)
	files := map[string][]byte{
		"jpeg cmyk 4:4:4 progressive 2048": cmyk.bytes(),
		"png rgba 16-bit interlaced 2048":  png16,
		"png rgba 8-bit 2048":              imgEncodePNG(t, imgQuadrants(2048, 2048)),
		"jpeg 4:2:0 2048":                  imgEncodeJPEG(t, imgQuadrants(2048, 2048), 85),
	}
	for name, f := range files {
		if len(f) > LogoMaxInputBytes {
			t.Fatalf("%s is %d bytes, over the input limit", name, len(f))
		}
	}
	return files
}

// logoHeapPeak samples the heap's object bytes every 100 µs while f runs and returns
// the largest sample: the live-plus-unswept heap the call drove the process to.
func logoHeapPeak(f func()) uint64 {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	var peak uint64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			metrics.Read(sample)
			if v := sample[0].Value.Uint64(); v > peak {
				peak = v
			}
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Microsecond):
			}
		}
	}()
	f()
	close(stop)
	wg.Wait()
	return peak
}

// TestLogoMemory_WorstDecodeAllocations measures each worst file through Normalize:
// TotalAlloc for the call and the sampled heap peak. The bound asserted is 128 MiB per
// call — the "tepe" the WL-3 note's 2 × (N × tepe + taban) arithmetic uses; the
// measured values are logged. A measurement of four 2048×2048 decodes, so it skips
// under -race (CI's `make test`); the gate tests keep one 96 MiB decode under -race.
func TestLogoMemory_WorstDecodeAllocations(t *testing.T) {
	if testing.Short() || logoRaceBuild {
		t.Skip("a memory measurement: four 2048×2048 decodes, not under -race")
	}
	for name, data := range logoWorstFiles(t) {
		var err error
		runtime.GC()
		var total uint64
		peak := logoHeapPeak(func() {
			total = logoAllocs(func() { _, err = logoNormalize(t, data) })
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%-34s %7d B in: TotalAlloc %6.2f MiB, heap objects peak %6.2f MiB",
			name, len(data), float64(total)/(1<<20), float64(peak)/(1<<20))
		if total > 128<<20 {
			t.Errorf("%s: one call allocated %.2f MiB, over 128 MiB", name, float64(total)/(1<<20))
		}
	}
}

// logoMaxRSS is the process's peak resident set in bytes. On Linux it is VmHWM from
// /proc/self/status, the high-water mark of the current address space, which execve
// replaces (proc(5); this branch has not been run here); getrusage's ru_maxrss is not
// used there because Linux carries it across exec, so a child reported its parent's
// peak (measured by the WL-3 audit: 141.9 MiB before and after in a container). On
// darwin it is ru_maxrss, in bytes.
func logoMaxRSS(t testing.TB) uint64 {
	t.Helper()
	if runtime.GOOS == "linux" {
		status, err := os.ReadFile("/proc/self/status")
		if err != nil {
			t.Fatalf("reading /proc/self/status: %v", err)
		}
		for _, line := range strings.Split(string(status), "\n") {
			if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
				f := strings.Fields(rest)
				if len(f) != 2 || f[1] != "kB" {
					t.Fatalf("unexpected VmHWM line %q", line)
				}
				kb, err := strconv.ParseUint(f[0], 10, 64)
				if err != nil {
					t.Fatalf("VmHWM line %q: %v", line, err)
				}
				return kb * 1024
			}
		}
		t.Fatal("/proc/self/status has no VmHWM line")
	}
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatalf("getrusage: %v", err)
	}
	return uint64(ru.Maxrss)
}

const logoMemoryChildEnv = "TAPPA_LOGO_MEMORY_CHILD"

// TestLogoMemory_Child is the measured process of TestLogoMemory_ProcessRSS; it does
// nothing unless that test started it. It reads one file, records its peak RSS, runs
// three Normalize calls one after another on a one-slot gate, and prints the peak RSS
// again.
func TestLogoMemory_Child(t *testing.T) {
	path := os.Getenv(logoMemoryChildEnv)
	if path == "" {
		t.Skip("run by TestLogoMemory_ProcessRSS")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	before := logoMaxRSS(t)
	g := logoNewGate(t, LogoDecodeSlots)
	for i := 0; i < 3; i++ {
		if _, err := g.Normalize(context.Background(), bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Printf("LOGO_RSS before=%d after=%d\n", before, logoMaxRSS(t))
}

// TestLogoMemory_ProcessRSS runs each worst file in a fresh process (this test binary,
// TestLogoMemory_Child) with GOGC and GOMEMLIMIT unset — the pod's configuration today
// (ADR 0024: no GC setting in the repository) — and reads the process's peak RSS
// (logoMaxRSS), refusing a reading whose "before" is over 32 MiB or not below "after":
// such a peak is not the child's own. The Linux branch was built here (GOOS=linux) and
// has not been run; the audit's container ran the earlier getrusage version, which is
// how the inheritance was found. The bound asserted is the pod's 512 MiB limit; the
// values are logged for the WL-3 note. This is a local process, not the pod: the
// product's own baseline and the container are not in it. Skipped under -race, whose
// shadow memory would be counted — so CI, which runs -race, does not run it.
func TestLogoMemory_ProcessRSS(t *testing.T) {
	if testing.Short() || logoRaceBuild {
		t.Skip("needs a non-race build and four 2048×2048 decodes")
	}
	dir := t.TempDir()
	for name, data := range logoWorstFiles(t) {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_"))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestLogoMemory_Child$", "-test.count=1")
		var env []string
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "GOGC=") && !strings.HasPrefix(kv, "GOMEMLIMIT=") {
				env = append(env, kv)
			}
		}
		cmd.Env = append(env, logoMemoryChildEnv+"="+path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: child: %v\n%s", name, err, out)
		}
		var before, after uint64
		for _, line := range strings.Split(string(out), "\n") {
			if rest, ok := strings.CutPrefix(line, "LOGO_RSS "); ok {
				for _, kv := range strings.Fields(rest) {
					k, v, _ := strings.Cut(kv, "=")
					n, perr := strconv.ParseUint(v, 10, 64)
					if perr != nil {
						t.Fatalf("%s: child line %q: %v", name, line, perr)
					}
					if k == "before" {
						before = n
					} else {
						after = n
					}
				}
			}
		}
		if after == 0 {
			t.Fatalf("%s: the child printed no measurement:\n%s", name, out)
		}
		// The child has read one file and run no decode when it takes "before"; a peak
		// that large there is some other process's, and the reading measures nothing.
		if before > 32<<20 || before >= after {
			t.Fatalf("%s: the child's peak before the decodes is %.1f MiB (after %.1f MiB); it is not this process's own",
				name, float64(before)/(1<<20), float64(after)/(1<<20))
		}
		t.Logf("%-34s peak RSS %6.1f MiB (process before the decodes %5.1f MiB)",
			name, float64(after)/(1<<20), float64(before)/(1<<20))
		if after > 512<<20 {
			t.Errorf("%s: peak RSS %.1f MiB is over the pod's 512 MiB", name, float64(after)/(1<<20))
		}
	}
}
