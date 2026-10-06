package brand

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

// LogoLightRatio is the contrast below which the Account editor warns that a logo may
// be hard to see (M10 WL-7; m10's WL-7 row, corrected by WL-0 from paper to porcelain):
// the tap and result screens draw the logo straight on the porcelain page (layout's
// BrandHeader), so porcelain is the ground the logo is measured against. A warning,
// never a refusal: a logo that is light on purpose is the business's to keep.
const LogoLightRatio = 1.5

// ErrLogoNotNormalized: LogoContrastOnPorcelain was handed a Logo that is not an
// unchanged Normalize output (Logo.Normalized). It decodes only those, so the decode is
// always of a re-encoded image of at most 512 x 512 pixels -- never of an upload.
var ErrLogoNotNormalized = errors.New("brand: the logo is not a Normalize output")

// LogoContrastOnPorcelain is the WCAG contrast ratio between porcelain and the logo's
// alpha-weighted mean relative luminance: each pixel's luminance (ADR 0023 §3's
// formula, accentLuminance) weighted by its opacity, a/255, so a transparent pixel
// counts for nothing and the porcelain behind it is not mistaken for the logo. A logo
// with no opaque pixel at all has nothing to see; its ratio is 1, the ratio of
// porcelain to itself.
//
// It is the mean of the LINEAR luminance, not of the 8-bit values: a logo that is half
// ink and half white averages to a mid grey that clears 1.5:1 (1.66:1 for black and
// white), and a pale logo that is light all over does not.
//
// It reads only a Normalize output (Logo.Normalized), decoded with the stored type's
// own decoder, so what it decodes is at most LogoMaxOutputEdge on each side.
func LogoContrastOnPorcelain(l Logo) (float64, error) {
	if !l.Normalized() {
		return 0, ErrLogoNotNormalized
	}
	var (
		img image.Image
		err error
	)
	switch l.MIME {
	case LogoMIMEPNG:
		img, err = png.Decode(bytes.NewReader(l.Data))
	case LogoMIMEJPEG:
		img, err = jpeg.Decode(bytes.NewReader(l.Data))
	default:
		return 0, fmt.Errorf("brand: logo type %q", l.MIME)
	}
	if err != nil {
		return 0, fmt.Errorf("brand: decoding a normalized logo: %w", err)
	}
	var weighted, weight float64
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A == 0 {
				continue
			}
			w := float64(c.A) / 255
			weighted += w * accentLuminance(Color{R: c.R, G: c.G, B: c.B})
			weight += w
		}
	}
	porcelain := accentLuminance(paletteColor(palettePorcelainHex))
	if weight == 0 {
		return 1, nil
	}
	return luminanceContrast(weighted/weight, porcelain), nil
}

// LogoLooksLight reports whether the Account editor warns about the logo: its contrast
// on porcelain is below LogoLightRatio.
func LogoLooksLight(l Logo) (bool, error) {
	r, err := LogoContrastOnPorcelain(l)
	if err != nil {
		return false, err
	}
	return r < LogoLightRatio, nil
}

// luminanceContrast is contrastRatio over two luminances rather than two colours.
func luminanceContrast(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}
