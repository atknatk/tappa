package brand

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// TestLogoResize_AreaWeightsAreExact: shrinking three pixels to two gives each output
// pixel two thirds of its outer source pixel and one third of the middle one; and for
// every source length up to 300 and every smaller output length, each output pixel's
// weights add up to the source length and each source pixel's to the output length.
func TestLogoResize_AreaWeightsAreExact(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 3, 1))
	src.Pix = []byte{0, 255, 30}
	got := logoResize(src, 2, 1)
	if got.Pix[0] != 85 || got.Pix[4] != 105 {
		t.Errorf("3→2 gave %d and %d, want 85 = (2·0+255)/3 and 105 = (255+2·30)/3", got.Pix[0], got.Pix[4])
	}
	for sn := 1; sn <= 300; sn++ {
		for dn := 1; dn <= sn; dn++ {
			perSrc := make([]uint64, sn)
			for d, taps := range logoTaps(sn, dn) {
				var sum uint64
				for _, tp := range taps {
					sum += tp.weight
					perSrc[tp.src] += tp.weight
				}
				if sum != uint64(sn) {
					t.Fatalf("logoTaps(%d, %d)[%d] weighs %d", sn, dn, d, sum)
				}
			}
			for s, w := range perSrc {
				if w != uint64(dn) {
					t.Fatalf("logoTaps(%d, %d): source %d weighs %d in all", sn, dn, s, w)
				}
			}
		}
	}
}

// TestLogoResize_TransparencyLendsNoColour: an opaque red pixel next to a fully
// transparent green one averages to half-transparent red. Averaging unpremultiplied
// samples would give a half-transparent olive.
func TestLogoResize_TransparencyLendsNoColour(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	src.SetNRGBA(1, 0, color.NRGBA{0, 255, 0, 0})
	got := logoResize(src, 1, 1).NRGBAAt(0, 0)
	if got.R < 254 || got.G != 0 || got.B != 0 || got.A < 127 || got.A > 128 {
		t.Errorf("got %v, want about {255 0 0 128}", got)
	}
	empty := logoResize(image.NewNRGBA(image.Rect(0, 0, 4, 4)), 2, 2)
	for _, p := range empty.Pix {
		if p != 0 {
			t.Fatalf("a transparent image came out with %v", empty.Pix)
		}
	}
}

// TestLogoResize_IdentityWhenItFits: at its own size the filter returns each pixel as
// the standard library's NRGBA model converts its premultiplied 16-bit colour, for an
// image with every alpha value and for each decoded layout used elsewhere. (An NRGBA
// pixel with a small alpha can lose a level on that round trip — {133 8 140 15} comes
// back {133 7 140 15} — which is the premultiplied filter's cost, not the identity's.)
func TestLogoResize_IdentityWhenItFits(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 37, 23))
	for i := range src.Pix {
		src.Pix[i] = byte(i*131 + i/7)
	}
	gray16 := image.NewGray16(image.Rect(0, 0, 9, 9))
	for i := range gray16.Pix {
		gray16.Pix[i] = byte(i * 37)
	}
	ycc := image.NewYCbCr(image.Rect(0, 0, 16, 16), image.YCbCrSubsampleRatio420)
	for i := range ycc.Y {
		ycc.Y[i] = byte(i)
	}
	cmyk := image.NewCMYK(image.Rect(0, 0, 8, 8))
	for i := range cmyk.Pix {
		cmyk.Pix[i] = byte(i * 13)
	}
	for _, m := range []image.Image{src, gray16, ycc, cmyk} {
		b := m.Bounds()
		got := logoResize(m, b.Dx(), b.Dy())
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				want := color.NRGBAModel.Convert(m.(image.RGBA64Image).RGBA64At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				if g := got.NRGBAAt(x, y); g != want {
					t.Fatalf("%T at (%d,%d): got %v, want %v", m, x, y, g, want)
				}
			}
		}
	}
}

// TestLogoResize_FineDetailAveragesInsteadOfAliasing is the quality case: a one-pixel
// black and white checkerboard at 2048×2048 shrinks to an even mid grey (the box filter
// averages every source pixel it covers); a filter that sampled one pixel per output
// would give black or white. Two runs give the same bytes.
func TestLogoResize_FineDetailAveragesInsteadOfAliasing(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 2048, 2048))
	for y := 0; y < 2048; y++ {
		for x := 0; x < 2048; x++ {
			if (x+y)%2 == 0 {
				src.Pix[y*src.Stride+x] = 255
			}
		}
	}
	got := logoResize(src, 512, 512)
	for i := 0; i < len(got.Pix); i += 4 {
		if v := got.Pix[i]; v < 127 || v > 128 || got.Pix[i+3] != 255 {
			t.Fatalf("pixel %d is %v, want mid grey", i/4, got.Pix[i:i+4])
		}
	}
	if again := logoResize(src, 512, 512); !bytes.Equal(again.Pix, got.Pix) {
		t.Fatal("two runs of the filter differ")
	}
}

// BenchmarkLogoResize_2048To512 is the filter's cost on the decoded layouts a 2048×2048
// upload produces.
func BenchmarkLogoResize_2048To512(b *testing.B) {
	r := image.Rect(0, 0, 2048, 2048)
	for _, m := range []image.Image{
		image.NewYCbCr(r, image.YCbCrSubsampleRatio420),
		image.NewGray(r),
		image.NewCMYK(r),
		image.NewRGBA(r),
		image.NewNRGBA64(r),
	} {
		b.Run(logoTypeName(m), func(b *testing.B) {
			for b.Loop() {
				logoResize(m, 512, 512)
			}
		})
	}
}
