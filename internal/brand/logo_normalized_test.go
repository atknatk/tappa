package brand

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// TestLogo_NormalizedIsTrueOnlyForNormalizeOutput measures Logo.Normalized (M10 WL-4):
// true on a PNG's and a JPEG's Normalize output and on a value copy of each; false on
// each of the five exported fields changed, on a byte of Data changed in place, on a
// composite literal carrying the same five values, on a copy whose Data and SHA256 are
// both replaced by another valid pair, and on the zero Logo.
func TestLogo_NormalizedIsTrueOnlyForNormalizeOutput(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			m.SetNRGBA(x, y, color.NRGBA{R: uint8(4 * x), G: uint8(8 * y), B: 90, A: 255})
		}
	}
	var pngIn, jpegIn, otherIn bytes.Buffer
	if err := png.Encode(&pngIn, m); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if err := jpeg.Encode(&jpegIn, m, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	// A different picture, so its Normalize output is a different valid (Data, SHA256)
	// pair from both of the above.
	o := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for i := range o.Pix {
		o.Pix[i] = 0xC8
	}
	if err := png.Encode(&otherIn, o); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	other := logoMustNormalize(t, otherIn.Bytes())

	for _, in := range []struct {
		name string
		data []byte
	}{{"png", pngIn.Bytes()}, {"jpeg", jpegIn.Bytes()}} {
		t.Run(in.name, func(t *testing.T) {
			l := logoMustNormalize(t, in.data)
			if !l.Normalized() {
				t.Fatal("Normalize's output: Normalized() = false")
			}
			cp := l
			if !cp.Normalized() {
				t.Fatal("a value copy of Normalize's output: Normalized() = false")
			}

			changed := map[string]func(*Logo){
				"Data replaced": func(x *Logo) { x.Data = append([]byte(nil), x.Data[:len(x.Data)-1]...) },
				"MIME": func(x *Logo) {
					x.MIME = map[string]string{LogoMIMEPNG: LogoMIMEJPEG, LogoMIMEJPEG: LogoMIMEPNG}[x.MIME]
				},
				"Width":  func(x *Logo) { x.Width++ },
				"Height": func(x *Logo) { x.Height-- },
				"SHA256": func(x *Logo) { x.SHA256 = other.SHA256 },
				"Data and SHA256 from another Normalize output": func(x *Logo) {
					x.Data, x.SHA256 = other.Data, other.SHA256
				},
			}
			for name, change := range changed {
				x := l
				change(&x)
				if x.Normalized() {
					t.Errorf("%s changed: Normalized() = true", name)
				}
			}

			inPlace := l
			inPlace.Data = append([]byte(nil), l.Data...)
			if !inPlace.Normalized() {
				t.Fatal("a copy whose Data is an equal copy: Normalized() = false")
			}
			inPlace.Data[len(inPlace.Data)/2] ^= 0x01
			if inPlace.Normalized() {
				t.Error("one byte of Data changed in place: Normalized() = true")
			}

			literal := Logo{Data: l.Data, MIME: l.MIME, Width: l.Width, Height: l.Height, SHA256: l.SHA256}
			if literal.Normalized() {
				t.Error("a composite literal with Normalize's five values: Normalized() = true")
			}
		})
	}

	if (Logo{}).Normalized() {
		t.Error("the zero Logo: Normalized() = true")
	}
}
