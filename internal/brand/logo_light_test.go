package brand

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"
)

// lightPNG encodes a w x h NRGBA picture whose pixel at (x, y) is at(x, y).
func lightPNG(t *testing.T, w, h int, at func(x, y int) color.NRGBA) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.SetNRGBA(x, y, at(x, y))
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return b.Bytes()
}

// TestLogoLight_AlphaWeightedLuminanceOnPorcelain measures LogoContrastOnPorcelain on
// Normalize outputs (64 x 32 unless said): the ratio for each picture against an
// independent computation from the formula (ADR 0023 §3: linearised channels,
// 0.2126/0.7152/0.0722, porcelain EDF0EA), and the warning on each side of 1.5. A
// transparent pixel is weighed by its alpha (zero), so a dark mark on a transparent
// ground is the mark's luminance and a white mark on one is white's. A picture with no
// opaque pixel is 1:1. Half ink, half white is the mean of the two linear luminances,
// 1.66:1 for black and white -- not the mean of the 8-bit values.
func TestLogoLight_AlphaWeightedLuminanceOnPorcelain(t *testing.T) {
	lin := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	lum := func(r, g, b uint8) float64 { return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b) }
	ratio := func(a, b float64) float64 {
		if a < b {
			a, b = b, a
		}
		return (a + 0.05) / (b + 0.05)
	}
	porcelain := lum(0xED, 0xF0, 0xEA)
	solid := func(c color.NRGBA) func(int, int) color.NRGBA { return func(int, int) color.NRGBA { return c } }
	white, black := color.NRGBA{255, 255, 255, 255}, color.NRGBA{0, 0, 0, 255}
	clear := color.NRGBA{}
	for _, tc := range []struct {
		name  string
		at    func(x, y int) color.NRGBA
		want  float64
		light bool
	}{
		{"white", solid(white), ratio(1, porcelain), true},
		{"porcelain itself", solid(color.NRGBA{0xED, 0xF0, 0xEA, 255}), 1, true},
		{"ink", solid(color.NRGBA{0x15, 0x22, 0x19, 255}), ratio(lum(0x15, 0x22, 0x19), porcelain), false},
		{"saffron", solid(color.NRGBA{0xD9, 0x8E, 0x2B, 255}), ratio(lum(0xD9, 0x8E, 0x2B), porcelain), false},
		{"C5C5C5, the nearest grey above the threshold", solid(color.NRGBA{0xC5, 0xC5, 0xC5, 255}), ratio(lum(0xC5, 0xC5, 0xC5), porcelain), false},
		{"C6C6C6, the nearest grey below it", solid(color.NRGBA{0xC6, 0xC6, 0xC6, 255}), ratio(lum(0xC6, 0xC6, 0xC6), porcelain), true},
		{"half black, half white", func(x, _ int) color.NRGBA {
			if x < 32 {
				return black
			}
			return white
		}, ratio(0.5, porcelain), false},
		{"a black mark on a transparent ground", func(x, _ int) color.NRGBA {
			if x < 8 {
				return black
			}
			return clear
		}, ratio(0, porcelain), false},
		{"a white mark on a transparent ground", func(x, _ int) color.NRGBA {
			if x < 8 {
				return white
			}
			return clear
		}, ratio(1, porcelain), true},
		{"nothing opaque", solid(clear), 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := logoMustNormalize(t, lightPNG(t, 64, 32, tc.at))
			got, err := LogoContrastOnPorcelain(l)
			if err != nil {
				t.Fatalf("LogoContrastOnPorcelain: %v", err)
			}
			if math.Abs(got-tc.want) > 0.01 {
				t.Errorf("ratio %.4f, want %.4f", got, tc.want)
			}
			light, err := LogoLooksLight(l)
			if err != nil || light != tc.light {
				t.Errorf("LogoLooksLight = %v (%v), want %v (ratio %.4f)", light, err, tc.light, got)
			}
		})
	}
	// The two grey rows are the nearest greys on each side of 1.5 (measured: C5C5C5
	// 1.5002:1, C6C6C6 1.4847:1), so moving the threshold by one grey step is seen.
	// Turning "below" into "at most" is NOT: no fixture lands on exactly 1.5, and no
	// solid grey can (the two nearest straddle it), so that change is equivalent on
	// every input measured here.
	if hi, lo := ratio(lum(0xC5, 0xC5, 0xC5), porcelain), ratio(lum(0xC6, 0xC6, 0xC6), porcelain); !(hi >= LogoLightRatio && lo < LogoLightRatio) {
		t.Fatalf("PREMISE: C5C5C5 %.4f:1 and C6C6C6 %.4f:1 do not straddle %.1f", hi, lo, LogoLightRatio)
	}
	if r := ratio(0.5, porcelain); math.Abs(r-1.66) > 0.005 {
		t.Fatalf("PREMISE: half black, half white is %.4f:1, the comment says 1.66", r)
	}
	// A JPEG is opaque: white is light, and its type's own decoder reads it.
	var jb bytes.Buffer
	m := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for i := range m.Pix {
		m.Pix[i] = 0xFF
	}
	if err := jpeg.Encode(&jb, m, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	jl := logoMustNormalize(t, jb.Bytes())
	if jl.MIME != LogoMIMEJPEG {
		t.Fatalf("PREMISE: a JPEG normalized to %s", jl.MIME)
	}
	if light, err := LogoLooksLight(jl); err != nil || !light {
		t.Errorf("a white JPEG: LogoLooksLight = %v (%v), want true", light, err)
	}
}

// TestLogoLight_ReadsOnlyANormalizeOutput: a Logo that is not an unchanged Normalize
// output -- a literal with valid PNG bytes, a Normalize output with a field changed,
// and the zero Logo -- is refused with ErrLogoNotNormalized and decoded not at all.
func TestLogoLight_ReadsOnlyANormalizeOutput(t *testing.T) {
	data := lightPNG(t, 64, 32, func(int, int) color.NRGBA { return color.NRGBA{0, 0, 0, 255} })
	l := logoMustNormalize(t, data)
	changed := l
	changed.Width++
	for name, x := range map[string]Logo{
		"a literal":       {Data: l.Data, MIME: l.MIME, Width: l.Width, Height: l.Height, SHA256: l.SHA256},
		"a field changed": changed,
		"the zero Logo":   {},
	} {
		if _, err := LogoContrastOnPorcelain(x); err != ErrLogoNotNormalized {
			t.Errorf("%s: err %v, want ErrLogoNotNormalized", name, err)
		}
		if _, err := LogoLooksLight(x); err != ErrLogoNotNormalized {
			t.Errorf("%s: LogoLooksLight err %v, want ErrLogoNotNormalized", name, err)
		}
	}
}

// TestDefaultAccent_IsTappaGreen: the editor's default is the palette's tappa-green,
// the colour the :root defaults give the tap button (TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme).
func TestDefaultAccent_IsTappaGreen(t *testing.T) {
	if got := DefaultAccent().Hex(); got != "1F5C41" {
		t.Errorf("DefaultAccent = %s, want 1F5C41", got)
	}
	if _, err := Check(DefaultAccent()); err != nil {
		t.Errorf("the default accent does not pass the gate: %v", err)
	}
}
