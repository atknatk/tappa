package brand

import (
	"image"
	"image/color"
)

// logoTap is one source pixel's share of one output pixel along an axis.
type logoTap struct {
	src    int
	weight uint64
}

// logoTaps is the exact area overlap of each output pixel with the source pixels
// along one axis. Measured in units where a source pixel is dn wide and an output pixel
// sn wide, output pixel d covers [d·sn, (d+1)·sn) and source pixel s covers
// [s·dn, (s+1)·dn); a tap's weight is their overlap, so an output pixel's weights add up
// to sn (TestLogoResize_AreaWeightsAreExact checks each pair of lengths up to 300). The
// arithmetic is integer because Go may fuse a float multiply-add into one rounding on
// arm64 and not on amd64, which would make the filter's pixels differ between machines;
// the tests ran on amd64. This keeps the filter's output machine-independent, not the
// stored bytes: the encoders' output can change between Go releases (a 300×200 noise
// PNG re-encodes to 180 383 B on 1.26.6 and 180 355 B on 1.27.1; the JPEG was the same
// on both), so a logo's sha256 is not stable across a toolchain upgrade.
func logoTaps(sn, dn int) [][]logoTap {
	taps := make([][]logoTap, dn)
	for d := 0; d < dn; d++ {
		lo, hi := d*sn, (d+1)*sn
		for s := lo / dn; s*dn < hi; s++ {
			a, b := max(lo, s*dn), min(hi, (s+1)*dn)
			if b > a {
				taps[d] = append(taps[d], logoTap{src: s, weight: uint64(b - a)})
			}
		}
	}
	return taps
}

// logoResize is ADR 0024 §3's box filter: each output pixel is the area-weighted mean
// of the source pixels it covers, taken over premultiplied 16-bit samples (so a
// transparent pixel lends no colour to its neighbours) in integer arithmetic, rounded
// to nearest. With dw, dh equal to the source size each pixel maps to itself, and the
// output is the source converted to 8-bit NRGBA. Beyond the output it keeps the tap
// lists and two rows of accumulators (1.22 MB per 2048² → 512² call with the output,
// BenchmarkLogoResize_2048To512); a source row is read once for each output row it
// overlaps.
func logoResize(src image.Image, dw, dh int) *image.NRGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	read := logoPixelReader(src)
	xt, yt := logoTaps(sw, dw), logoTaps(sh, dh)
	norm := uint64(sw) * uint64(sh)
	half := norm / 2

	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	row := make([]uint64, 4*dw)
	acc := make([]uint64, 4*dw)
	for dy := 0; dy < dh; dy++ {
		clear(acc)
		for _, ty := range yt[dy] {
			clear(row)
			sy := b.Min.Y + ty.src
			for dx := 0; dx < dw; dx++ {
				var r, g, bl, a uint64
				for _, tx := range xt[dx] {
					c := read(b.Min.X+tx.src, sy)
					r += tx.weight * uint64(c.R)
					g += tx.weight * uint64(c.G)
					bl += tx.weight * uint64(c.B)
					a += tx.weight * uint64(c.A)
				}
				row[4*dx], row[4*dx+1], row[4*dx+2], row[4*dx+3] = r, g, bl, a
			}
			for i, v := range row {
				acc[i] += ty.weight * v
			}
		}
		out := dst.Pix[dy*dst.Stride : dy*dst.Stride+4*dw]
		for dx := 0; dx < dw; dx++ {
			a := (acc[4*dx+3] + half) / norm
			if a == 0 {
				out[4*dx], out[4*dx+1], out[4*dx+2], out[4*dx+3] = 0, 0, 0, 0
				continue
			}
			for c := 0; c < 3; c++ {
				v := (acc[4*dx+c] + half) / norm
				// Un-premultiply as color.NRGBAModel does: v ≤ a by construction, so
				// the result fits in 16 bits.
				out[4*dx+c] = uint8((v * 0xffff / a) >> 8)
			}
			out[4*dx+3] = uint8(a >> 8)
		}
	}
	return dst
}

// logoPixelReader returns src's premultiplied 16-bit colour at (x, y). The image types
// the tests decode (Gray, Gray16, YCbCr, CMYK, RGBA, RGBA64, NRGBA, NRGBA64, Paletted)
// implement image.RGBA64Image, whose RGBA64At does not box a color.Color per pixel; the
// At path is kept for an image that does not.
func logoPixelReader(src image.Image) func(x, y int) color.RGBA64 {
	if m, ok := src.(image.RGBA64Image); ok {
		return m.RGBA64At
	}
	return func(x, y int) color.RGBA64 {
		r, g, b, a := src.At(x, y).RGBA()
		return color.RGBA64{R: uint16(r), G: uint16(g), B: uint16(b), A: uint16(a)}
	}
}
