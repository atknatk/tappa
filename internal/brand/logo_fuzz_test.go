package brand

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"testing"
)

// logoFuzzSeeds is FuzzNormalize's seed corpus: small valid files of the layouts the
// normalization test names, and the hostile shapes of ADR 0024 (header bombs,
// polyglots, metadata carriers, truncations, scan bombs, the dishonest files).
func logoFuzzSeeds(t testing.TB) [][]byte {
	pic := imgQuadrants(40, 32)
	var gifBuf bytes.Buffer
	if err := gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 16, 16), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	pal := image.NewPaletted(image.Rect(0, 0, 24, 24), color.Palette{color.NRGBA{0, 0, 0, 0}, color.NRGBA{200, 30, 40, 255}})
	pal.SetColorIndex(5, 5, 1)
	prog := imgGrayForge(32, 24, func(bx, by int) int { return 50 + 30*bx + by })
	prog.progressive = true
	cmyk := imgCMYKForge(24, 24, 0, [4]int{200, 100, 50, 250})
	cmykProg := cmyk
	cmykProg.progressive = true
	pngOK := imgEncodePNG(t, pic)
	jpegOK := imgEncodeJPEG(t, pic, 85)
	html := []byte("<html><script>alert(1)</script></html>")
	seeds := [][]byte{
		pngOK,
		jpegOK,
		imgEncodePNG(t, pal),
		imgEncodePNG(t, image.NewGray16(image.Rect(0, 0, 20, 20))),
		imgEncodeJPEG(t, image.NewGray(image.Rect(0, 0, 20, 20)), 85),
		prog.bytes(),
		cmyk.bytes(),
		cmykProg.bytes(),
		imgCMYKForge(24, 24, 2, [4]int{90, 150, 110, 240}).bytes(),
		imgPNGForged(t, 20, 18, 16, 6, true, func(x, y int) []byte { return []byte{byte(x), 0, byte(y), 0, 9, 9, 255, 255} }, nil, nil),
		imgPNGForged(t, 20, 18, 8, 4, false, func(x, y int) []byte { return []byte{byte(x * 9), byte(y * 9)} }, nil, html),
		imgPNGFile(imgPNGIHDR(30000, 30000, 8, 6, 0), imgPNGChunk("IDAT", imgZlib(t, []byte{0, 0, 0, 0, 0})), imgPNGChunk("IEND", nil)),
		imgPNGFile(imgPNGIHDR(0x80000000, 16, 8, 6, 0), imgPNGChunk("IEND", nil)),
		append(append([]byte(nil), imgPNGSignature...), html...),
		append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, html...),
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		gifBuf.Bytes(),
		imgJPEGInsertAfterSOI(jpegOK, imgExifGPS(), imgJPEGSegment(0xFE, html)),
		append(append([]byte(nil), jpegOK...), html...),
		pngOK[:len(pngOK)/2],
		jpegOK[:len(jpegOK)-3],
		logoScanFile(16, 16, LogoJPEGScanCeiling+1, 90, ""),
		logoScanFile(16, 16, LogoJPEGScanCeiling, 90, "rst"),
		logoScanFile(16, 16, LogoJPEGScanCeiling+1, 90, "junk"),
	}
	return seeds
}

// FuzzNormalize: on the inputs of a run (the seed corpus under go test, generated
// inputs under -fuzz) Normalize does not panic, and
//   - a refusal matches exactly one refusal class and its text is that class's text;
//   - a success is a logo that re-decodes with its own format's decoder at the size it
//     claims, within 1..512 px and 256 KiB, whose SHA256 is its digest, made from an
//     input that sniffs as the same format, whose header is within 16..2048 px and, for
//     a JPEG, whose raw scan count is within the ceiling.
func FuzzNormalize(f *testing.F) {
	for _, s := range logoFuzzSeeds(f) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		g, err := NewLogoGate(1)
		if err != nil {
			t.Fatal(err)
		}
		l, err := g.Normalize(context.Background(), bytes.NewReader(data))
		if err != nil {
			matched := 0
			for _, c := range logoClasses {
				if c == ErrLogoBusy {
					continue
				}
				if errors.Is(err, c) {
					matched++
					if err.Error() != c.Error() {
						t.Fatalf("refusal text %q is not the class text %q", err.Error(), c.Error())
					}
				}
			}
			if matched != 1 {
				t.Fatalf("err %v matches %d refusal classes", err, matched)
			}
			return
		}
		logoCheckOutput(t, l)
		if got := http.DetectContentType(data); got != l.MIME {
			t.Fatalf("input sniffs as %q, output is %q", got, l.MIME)
		}
		var cfg image.Config
		if l.MIME == LogoMIMEPNG {
			cfg, err = png.DecodeConfig(bytes.NewReader(data))
		} else {
			cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
			if n := logoScanCount(data); n > LogoJPEGScanCeiling {
				t.Fatalf("accepted a JPEG with %d raw scans", n)
			}
		}
		if err != nil || !logoDimensionsAllowed(cfg.Width, cfg.Height) {
			t.Fatalf("accepted an input whose header is %d×%d (%v)", cfg.Width, cfg.Height, err)
		}
		if l.Width > cfg.Width || l.Height > cfg.Height {
			t.Fatalf("a %d×%d input grew to %d×%d", cfg.Width, cfg.Height, l.Width, l.Height)
		}
	})
}
