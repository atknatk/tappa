package legal

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// The link pass runs once per publication over a body of up to 256 KiB
// (op_publish_legal's limit), and two of its first shapes were quadratic (fix round,
// 2026-10-23, measured at 256 KiB before the fix: 1.50 s for a URL followed by ")"
// repeated, 0.75 s for ")." repeated, 0.40 s for repeated `[a](javascript:x)`, 0.62 s
// for repeated `[a](//x)`). The two tests below hold each regression with a measure
// that -race cannot blur.
//
// WHY NOT ONE TIMING RATIO OVER Paragraphs: under -race the regexp's linear cost grows
// ~10x while the quadratic parts (strings.Count, a string copy) do not, so the old code
// scaled only x5-x9 for 16x the input and passed a time-ratio gate (the second audit of
// 5b743023 measured exactly that). So each regression is measured where it lives.

// TestTrimURLTail_IsLinear times trimURLTail alone, where the regexp is not in the way.
// The fixed code trims 256 KiB in about a millisecond, with or without -race; recounting
// the brackets per trimmed character took 0.75-1.5 s. It fails when the 256 KiB run is
// over 50 ms AND it grew more than 64x for 16x the input (linear ~16x, quadratic ~256x),
// so neither a slow machine alone nor a noisy small run alone can fail it. Best of three
// strips GC and scheduler spikes.
func TestTrimURLTail_IsLinear(t *testing.T) {
	best := func(u string) time.Duration {
		var b time.Duration
		for i := 0; i < 3; i++ {
			start := time.Now()
			trimURLTail(u)
			if d := time.Since(start); i == 0 || d < b {
				b = d
			}
		}
		return b
	}
	for _, tail := range []string{")", ").", "]", "]."} {
		mk := func(n int) string { return "https://a.mt/" + strings.Repeat(tail, n/len(tail)) }
		// ANTI-VACUITY: the shape must actually be trimmed back to the URL, or the loop
		// under test is not running at all.
		if got := trimURLTail(mk(64)); got != "https://a.mt/" {
			t.Fatalf("tail %q: trimURLTail left %q", tail, got)
		}
		small, large := best(mk(16<<10)), best(mk(256<<10))
		ratio := float64(large) / float64(max(small, time.Microsecond))
		t.Logf("tail %q: 16 KiB %v, 256 KiB %v (x%.1f)", tail, small, large, ratio)
		if large > 50*time.Millisecond && ratio > 64 {
			t.Errorf("tail %q: 256 KiB took %v, %.1fx the 16 KiB run; trimURLTail is not linear", tail, large, ratio)
		}
	}
}

// TestParagraphs_RefusedLinksAllocateLinearly counts BYTES ALLOCATED, which is
// deterministic: a body of refused explicit links is one long run of text, and growing
// that run with += allocated O(n²) bytes (about 2 GB at 256 KiB). The fixed code cuts
// the run once; it allocates ~19x for 16x the input without -race and ~17x with it (the
// race runtime allocates more per byte, but proportionally). The gate is 48x; the +=
// code measures above 100x with -race and far above without.
//
// It must not run in parallel: TotalAlloc is process-wide, and this test reads it around
// one call while no other test in the package runs (parallel tests wait until every
// sequential one has finished).
func TestParagraphs_RefusedLinksAllocateLinearly(t *testing.T) {
	allocated := func(in string) uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		ps := Paragraphs(in)
		runtime.ReadMemStats(&after)
		// ANTI-VACUITY: the body must come back as one paragraph of literal text.
		if len(ps) != 1 || len(ps[0]) != 1 || ps[0][0].Href != "" || len(ps[0][0].Text) != len(in) {
			t.Fatalf("a body of refused links did not come back as one run of text: %d paragraph(s)", len(ps))
		}
		return after.TotalAlloc - before.TotalAlloc
	}
	for _, unit := range []string{"[a](javascript:x)", "[a](//x)"} {
		mk := func(n int) string { return strings.Repeat(unit, n/len(unit)) }
		small, large := allocated(mk(16<<10)), allocated(mk(256<<10))
		ratio := float64(large) / float64(max(small, 1))
		t.Logf("%s: 16 KiB %d B, 256 KiB %d B (x%.1f)", unit, small, large, ratio)
		if ratio > 48 {
			t.Errorf("%s: 256 KiB allocated %d bytes, %.1fx the 16 KiB run; the text between links is not cut once", unit, large, ratio)
		}
	}
}
