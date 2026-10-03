package brand

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"testing"
	"time"
)

// logoScanFile is a progressive JPEG whose raw scan count (FF DA pairs) is scans: one
// DC scan and scans-1 first AC scans, every one an end-of-band run.
func logoScanFile(w, h, scans int, level int, hide string) []byte {
	f := imgGrayForge(w, h, func(int, int) int { return level })
	f.progressive, f.eobRun, f.acScans, f.hide = true, 32767, scans-1, hide
	return f.bytes()
}

// TestLogoScans_CeilingAcceptedOneMoreRefused is ADR 0024 §2.5's ceiling at its edge:
// a progressive JPEG with exactly LogoJPEGScanCeiling scans is normalized, one with a
// scan more is refused as ErrLogoScans; at 2048×2048 the refusal allocates what
// inspection allocates, not what a decode of that layout allocates (the positive
// control decodes the same layout with the fewest scans: the allocation is the
// layout's, not the scan count's). The real encoders measured in the WL-3 note write
// 6, 10 and 18 scans; the ceiling must not refuse them.
func TestLogoScans_CeilingAcceptedOneMoreRefused(t *testing.T) {
	if LogoJPEGScanCeiling < 18 {
		t.Fatalf("ceiling %d is under 18, the most scans a measured encoder wrote (ADR 0024 S10)", LogoJPEGScanCeiling)
	}
	at := logoScanFile(64, 64, LogoJPEGScanCeiling, 90, "")
	over := logoScanFile(64, 64, LogoJPEGScanCeiling+1, 90, "")
	if n := logoScanCount(at); n != LogoJPEGScanCeiling {
		t.Fatalf("fixture has %d scans, want %d", n, LogoJPEGScanCeiling)
	}
	l, err := logoNormalize(t, at)
	if err != nil {
		t.Fatalf("%d scans: %v", LogoJPEGScanCeiling, err)
	}
	logoCheckOutput(t, l)
	if _, err := logoNormalize(t, over); !errors.Is(err, ErrLogoScans) {
		t.Fatalf("%d scans: err = %v, want ErrLogoScans", LogoJPEGScanCeiling+1, err)
	}

	cmyk := func(acScans int) []byte {
		f := imgCMYKForge(2048, 2048, 0, [4]int{10, 20, 30, 40})
		f.progressive, f.eobRun, f.acScans = true, 32767, acScans
		return f.bytes()
	}
	for _, layout := range []struct {
		name          string
		data, control []byte
	}{
		{"gray 2048", logoScanFile(2048, 2048, LogoJPEGScanCeiling+1, 90, ""), logoScanFile(2048, 2048, 2, 90, "")},
		{"cmyk 2048", cmyk(LogoJPEGScanCeiling), cmyk(4)},
	} {
		if n := logoScanCount(layout.data); n != LogoJPEGScanCeiling+1 {
			t.Fatalf("%s: fixture has %d scans", layout.name, n)
		}
		var err error
		n := logoAllocs(func() { _, err = logoNormalize(t, layout.data) })
		if !errors.Is(err, ErrLogoScans) {
			t.Errorf("%s: err = %v, want ErrLogoScans", layout.name, err)
		}
		var decErr error
		control := logoAllocs(func() { _, decErr = jpeg.Decode(bytes.NewReader(layout.control)) })
		if decErr != nil || control < 16<<20 {
			t.Fatalf("POSITIVE CONTROL FAILED: %s decodes with err %v and %d bytes; the refusal's allocation says nothing", layout.name, decErr, control)
		}
		t.Logf("%s, %d scans, %d bytes: refused with %d bytes allocated; decoding the layout allocates %d", layout.name, LogoJPEGScanCeiling+1, len(layout.data), n, control)
		if n > 1<<20 {
			t.Errorf("%s: the refusal allocated %d bytes", layout.name, n)
		}
	}
}

// logoUndercounters are four scan counters that skip or stop by marker bytes
// (logo_forge_test.go): the segment walker, the one that trusts the two bytes behind
// each FF DA it finds, the one that skips APPn/COM payloads wherever it sees their
// marker bytes, and the one that stops at the first FF D9.
var logoUndercounters = map[string]func([]byte) int{
	"segment walker":    imgWalkerScanCount,
	"skips scan header": imgSkipHeaderScanCount,
	"skips APPn":        imgSkipAppScanCount,
	"stops at FF D9":    imgStopAtEOIScanCount,
}

// TestLogoScans_DishonestFilesRefused is the "dürüst olmayan" case of ADR 0024 §2.5,
// in seven placements, each named with the counter it fools:
//   - before the DC scan's marker, bytes the decoder resyncs over and the segment
//     walker does not — a top-level RST (FF D0), a stray FF 00, fill bytes (FF FF),
//     three bytes that are not a marker (00 11 22);
//   - an APP1 whose payload is FF DA FF FF — the counter that skips "the scan header"
//     behind each FF DA it finds jumps 65 535 bytes;
//   - DQT values FF E1 FF F0 — the counter that skips APPn payloads where it sees
//     FF Ex jumps 65 520 bytes;
//   - an APP1 whose payload is FF D9 — the counter that stops at the first FF D9 (an
//     Exif thumbnail's EOI sits there in real files) stops before the scans.
//
// For each, with LogoJPEGScanCeiling+1 scans the decoder runs:
//   - the named counter counts at most LogoJPEGScanCeiling (the trick works);
//   - the standard decoder decodes the file and every pixel has the DC scan's level,
//     so it ran the scan the counter did not see;
//   - the raw count exceeds the ceiling and Normalize refuses with ErrLogoScans.
//
// The same placement with a raw count of exactly LogoJPEGScanCeiling is normalized: the
// refusal is the count's, not the trick's. The control: on an honest file the four
// counters count what the raw count does.
func TestLogoScans_DishonestFilesRefused(t *testing.T) {
	const level = 77
	honest := logoScanFile(64, 64, LogoJPEGScanCeiling+1, level, "")
	for name, count := range logoUndercounters {
		if n := count(honest); n != LogoJPEGScanCeiling+1 {
			t.Fatalf("CONTROL FAILED: the %s counts %d scans on an honest file of %d", name, n, LogoJPEGScanCeiling+1)
		}
	}
	placements := []struct{ hide, fools string }{
		{"rst", "segment walker"}, {"ff00", "segment walker"}, {"fill", "segment walker"},
		{"junk", "segment walker"}, {"app1-sos", "skips scan header"}, {"dqt-app", "skips APPn"},
		{"app1-eoi", "stops at FF D9"},
	}
	for _, pl := range placements {
		hide := pl.hide
		t.Run(hide, func(t *testing.T) {
			data := logoScanFile(64, 64, LogoJPEGScanCeiling+1, level, hide)
			if w := logoUndercounters[pl.fools](data); w > LogoJPEGScanCeiling {
				t.Fatalf("the %s counts %d scans; the file does not fool it", pl.fools, w)
			}
			img, err := jpeg.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("the decoder refuses the file (%v); the trick is not one it resyncs over", err)
			}
			g, ok := img.(*image.Gray)
			if !ok {
				t.Fatalf("decoded %T", img)
			}
			for i, p := range g.Pix {
				if p != level {
					t.Fatalf("pixel %d is %d, want %d: the decoder did not run the hidden DC scan", i, p, level)
				}
			}
			// The APP1 placement carries one FF DA of its own; the raw count includes it.
			extra := logoScanCount(data) - (LogoJPEGScanCeiling + 1)
			if extra < 0 || extra > 1 {
				t.Fatalf("raw count %d for %d scans", logoScanCount(data), LogoJPEGScanCeiling+1)
			}
			if _, err := logoNormalize(t, data); !errors.Is(err, ErrLogoScans) {
				t.Fatalf("err = %v, want ErrLogoScans", err)
			}
			at := logoScanFile(64, 64, LogoJPEGScanCeiling-extra, level, hide)
			if n := logoScanCount(at); n != LogoJPEGScanCeiling {
				t.Fatalf("the accept control has a raw count of %d", n)
			}
			l, err := logoNormalize(t, at)
			if err != nil {
				t.Fatalf("the same placement with a raw count of %d: %v", LogoJPEGScanCeiling, err)
			}
			logoCheckOutput(t, l)
		})
	}
}

// TestLogoScans_WorstCaseDecodeTime measures, once per run, the eight decode-bound
// files the WL-3 note in ADR 0024 records, all 2048×2048 and one or four components:
// progressive at the ceiling with first AC scans, with AC refinement scans, and with
// most of 512 KiB spent on repeated DC scans before the refinement scans; and the
// sequential files whose cost is bounded by their size rather than their scan count
// (one bit per block per scan, as many scans as 512 KiB holds). Each goes through
// Normalize and is normalized; the times are logged. The only bound asserted is the
// router's 30 s (ADR 0024 §2.5's budget). It is a measurement, so it skips under -race
// (CI's `make test`), where the instrumented decoder's time is not the product's; run
// it with `go test -run TestLogoScans_WorstCaseDecodeTime -v ./internal/brand/`.
func TestLogoScans_WorstCaseDecodeTime(t *testing.T) {
	if testing.Short() || logoRaceBuild {
		t.Skip("a timing measurement: eight 2048×2048 decodes, not under -race")
	}
	gray := func() imgJPEGForge {
		f := imgGrayForge(2048, 2048, func(int, int) int { return 77 })
		f.eobRun = 32767
		return f
	}
	cmyk := func() imgJPEGForge {
		f := imgCMYKForge(2048, 2048, 0, [4]int{10, 20, 30, 40})
		f.eobRun = 32767
		return f
	}
	type worst struct {
		name string
		f    imgJPEGForge
	}
	var cases []worst
	g := gray()
	g.progressive, g.acScans = true, LogoJPEGScanCeiling-1
	cases = append(cases, worst{"gray progressive, first AC scans", g})
	g = gray()
	g.progressive, g.acScans, g.refineScans = true, 1, LogoJPEGScanCeiling-2
	cases = append(cases, worst{"gray progressive, refinement scans", g})
	c := cmyk()
	c.progressive, c.acScans = true, LogoJPEGScanCeiling-1
	cases = append(cases, worst{"cmyk 4:4:4 progressive, first AC scans", c})
	c = cmyk()
	c.progressive, c.acScans, c.refineScans = true, 4, LogoJPEGScanCeiling-5
	cases = append(cases, worst{"cmyk 4:4:4 progressive, refinement scans", c})
	g = gray()
	g.progressive, g.dcScans, g.acScans, g.refineScans = true, 60, 1, LogoJPEGScanCeiling-61
	cases = append(cases, worst{"gray progressive, 60 DC + refinement", g})
	c = cmyk()
	c.progressive, c.dcScans, c.acScans, c.refineScans = true, 15, 4, LogoJPEGScanCeiling-19
	cases = append(cases, worst{"cmyk 4:4:4 progressive, 15 DC + refinement", c})
	g = gray()
	g.seqScans = 60
	cases = append(cases, worst{"gray sequential, 60 scans", g})
	c = cmyk()
	c.seqScans = 15
	cases = append(cases, worst{"cmyk 4:4:4 sequential, 15 scans", c})

	for _, tc := range cases {
		data := tc.f.bytes()
		if len(data) > LogoMaxInputBytes || logoScanCount(data) > LogoJPEGScanCeiling {
			t.Fatalf("%s: %d bytes, %d scans — outside what Normalize admits", tc.name, len(data), logoScanCount(data))
		}
		start := time.Now()
		l, err := logoNormalize(t, data)
		d := time.Since(start)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		logoCheckOutput(t, l)
		t.Logf("%-42s %3d scans %7d B  Normalize %v", tc.name, logoScanCount(data), len(data), d.Round(time.Millisecond))
		if d > 30*time.Second {
			t.Errorf("%s took %v, over the router's 30 s", tc.name, d)
		}
	}
}
