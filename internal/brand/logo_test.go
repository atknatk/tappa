package brand

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/textproto"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
)

// logoClasses is every refusal class Normalize names.
var logoClasses = []error{
	ErrLogoBusy, ErrLogoInputTooLarge, ErrLogoRead, ErrLogoFormat, ErrLogoDimensions,
	ErrLogoScans, ErrLogoCorrupt, ErrLogoOutputTooLarge, ErrLogoVerify,
}

func logoNewGate(t testing.TB, slots int) *LogoGate {
	t.Helper()
	g, err := NewLogoGate(slots)
	if err != nil {
		t.Fatalf("NewLogoGate(%d): %v", slots, err)
	}
	return g
}

func logoNormalize(t testing.TB, data []byte) (Logo, error) {
	t.Helper()
	return logoNewGate(t, LogoDecodeSlots).Normalize(context.Background(), bytes.NewReader(data))
}

func logoMustNormalize(t testing.TB, data []byte) Logo {
	t.Helper()
	l, err := logoNormalize(t, data)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return l
}

// logoAllocs is the bytes the heap handed out while f ran (runtime TotalAlloc). The
// tests that call it are not parallel, so no other test of the package runs while it
// measures (parallel tests run after the sequential ones).
func logoAllocs(f func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	f()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// logoCheckOutput checks what the tests require of a returned logo: it is a PNG or JPEG
// by sniff, it decodes with that format's own decoder at the size it claims, within
// 1..512 px and 256 KiB, and SHA256 is its digest.
func logoCheckOutput(t testing.TB, l Logo) image.Image {
	t.Helper()
	if l.MIME != LogoMIMEPNG && l.MIME != LogoMIMEJPEG {
		t.Fatalf("MIME = %q", l.MIME)
	}
	if got := http.DetectContentType(l.Data); got != l.MIME {
		t.Fatalf("output sniffs as %q, Logo.MIME says %q", got, l.MIME)
	}
	if len(l.Data) > LogoMaxOutputBytes {
		t.Fatalf("output is %d bytes, over %d", len(l.Data), LogoMaxOutputBytes)
	}
	if l.Width < 1 || l.Height < 1 || l.Width > LogoMaxOutputEdge || l.Height > LogoMaxOutputEdge {
		t.Fatalf("output is %d×%d", l.Width, l.Height)
	}
	sum := sha256.Sum256(l.Data)
	if l.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("SHA256 is not the digest of Data")
	}
	if !l.Normalized() {
		t.Fatal("Normalize's output reports Normalized() == false")
	}
	var img image.Image
	var err error
	if l.MIME == LogoMIMEPNG {
		img, err = png.Decode(bytes.NewReader(l.Data))
	} else {
		img, err = jpeg.Decode(bytes.NewReader(l.Data))
	}
	if err != nil {
		t.Fatalf("output does not decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != l.Width || b.Dy() != l.Height {
		t.Fatalf("output decodes at %v, Logo says %d×%d", b.Size(), l.Width, l.Height)
	}
	return img
}

// TestLogoFormat_RefusesWhatIsNotPNGOrJPEG is ADR 0024 İddia A's format table: the
// five named cases (SVG, GIF, WebP, HTML, HTML behind a PNG signature) and the sniff
// neighbours ADR 0024 S1/S2 measured are refused as ErrLogoFormat, and so is a JPEG the
// decoder reads but the sniff does not name; a PNG and a JPEG of the same picture are
// the positive control.
func TestLogoFormat_RefusesWhatIsNotPNGOrJPEG(t *testing.T) {
	var gifBuf bytes.Buffer
	pal := image.NewPaletted(image.Rect(0, 0, 32, 32), color.Palette{color.Black, color.White})
	if err := gif.Encode(&gifBuf, pal, nil); err != nil {
		t.Fatal(err)
	}
	webp := []byte("RIFF\x24\x00\x00\x00WEBPVP8 \x18\x00\x00\x00\x30\x01\x00\x9d\x01\x2a\x20\x00\x20\x00")
	webp = append(webp, make([]byte, 16)...)
	html := []byte("<!DOCTYPE html><html><body><script>alert(1)</script></body></html>")
	refused := []struct {
		name string
		data []byte
	}{
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><script>alert(1)</script></svg>`)},
		{"svg with xml prolog", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><rect width="32" height="32"/></svg>`)},
		{"gif", gifBuf.Bytes()},
		{"webp", webp},
		{"html", html},
		{"png signature then html", append(append([]byte(nil), imgPNGSignature...), html...)},
		{"jpeg signature then html", append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, html...)},
		{"bmp", append([]byte("BM\x46\x00\x00\x00\x00\x00\x00\x00\x36\x00\x00\x00\x28\x00\x00\x00"), make([]byte, 64)...)},
		{"pdf", []byte("%PDF-1.7\n1 0 obj<<>>endobj\n")},
		{"text", []byte("just a logo, honestly")},
		{"empty", nil},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			_, err := logoNormalize(t, tc.data)
			if !errors.Is(err, ErrLogoFormat) {
				t.Fatalf("err = %v, want ErrLogoFormat (sniffed %q)", err, http.DetectContentType(tc.data))
			}
		})
	}
	// The two neighbours S2 names: the sniff alone admits them, the format's own
	// DecodeConfig does not.
	for _, name := range []string{"png signature then html", "jpeg signature then html"} {
		for _, tc := range refused {
			if tc.name == name {
				if got := http.DetectContentType(tc.data); got != LogoMIMEPNG && got != LogoMIMEJPEG {
					t.Errorf("%s sniffs as %q; the case no longer reaches the second gate", name, got)
				}
			}
		}
	}

	// The first gate on its own: image/jpeg resyncs over a junk byte after SOI, so this
	// file decodes, but it does not start FF D8 FF and does not sniff as a JPEG. The
	// sniff is what refuses it.
	jpg := imgEncodeJPEG(t, imgQuadrants(32, 32), 85)
	unsniffable := append([]byte{0xFF, 0xD8, 0x00}, jpg[2:]...)
	if _, err := jpeg.Decode(bytes.NewReader(unsniffable)); err != nil {
		t.Fatalf("CONTROL: the decoder refuses the junk-after-SOI JPEG (%v); the case does not isolate the sniff", err)
	}
	if got := http.DetectContentType(unsniffable); got == LogoMIMEJPEG {
		t.Fatalf("CONTROL: the junk-after-SOI JPEG sniffs as %q", got)
	}
	if _, err := logoNormalize(t, unsniffable); !errors.Is(err, ErrLogoFormat) {
		t.Errorf("junk after SOI: err = %v, want ErrLogoFormat", err)
	}

	pic := imgQuadrants(64, 48)
	for _, in := range [][]byte{imgEncodePNG(t, pic), imgEncodeJPEG(t, pic, 90)} {
		l := logoMustNormalize(t, in)
		logoCheckOutput(t, l)
		if l.MIME != http.DetectContentType(in) {
			t.Errorf("a %s came out as %s", http.DetectContentType(in), l.MIME)
		}
	}
}

// TestLogoFormat_ClientHeadersDoNotDecide hands Normalize multipart parts whose
// Content-Type and file name disagree with their bytes, in three cases: an SVG labelled
// image/png and logo.png is refused; a JPEG the decoder reads but the sniff does not
// name (a junk byte after SOI) labelled image/jpeg and logo.jpg is refused as
// ErrLogoFormat, so the label does not stand in for the sniff; a PNG labelled
// image/svg+xml and logo.svg is accepted, with the same bytes out as the unlabelled
// reader gives. Normalize has no parameter for either header; these three cases are
// what is measured about the part's own header fields.
func TestLogoFormat_ClientHeadersDoNotDecide(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	pngBytes := imgEncodePNG(t, imgQuadrants(40, 40))
	part := func(contentType, filename string, body []byte) io.Reader {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="logo"; filename="`+filename+`"`)
		h.Set("Content-Type", contentType)
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		p, err := multipart.NewReader(&b, mw.Boundary()).NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if p.Header.Get("Content-Type") != contentType || p.FileName() != filename {
			t.Fatalf("the part does not carry the label under test: %q %q", p.Header.Get("Content-Type"), p.FileName())
		}
		return p
	}
	g := logoNewGate(t, 1)
	if _, err := g.Normalize(context.Background(), part("image/png", "logo.png", svg)); !errors.Is(err, ErrLogoFormat) {
		t.Errorf("SVG labelled image/png: err = %v, want ErrLogoFormat", err)
	}
	jpg := imgEncodeJPEG(t, imgQuadrants(32, 32), 85)
	unsniffable := append([]byte{0xFF, 0xD8, 0x00}, jpg[2:]...)
	if _, err := jpeg.Decode(bytes.NewReader(unsniffable)); err != nil {
		t.Fatalf("CONTROL: the decoder refuses the junk-after-SOI JPEG (%v)", err)
	}
	if _, err := g.Normalize(context.Background(), part("image/jpeg", "logo.jpg", unsniffable)); !errors.Is(err, ErrLogoFormat) {
		t.Errorf("junk-after-SOI JPEG labelled image/jpeg: err = %v, want ErrLogoFormat", err)
	}
	labelled, err := g.Normalize(context.Background(), part("image/svg+xml", "logo.svg", pngBytes))
	if err != nil {
		t.Fatalf("PNG labelled image/svg+xml: %v", err)
	}
	plain := logoMustNormalize(t, pngBytes)
	if labelled.MIME != LogoMIMEPNG || !bytes.Equal(labelled.Data, plain.Data) {
		t.Errorf("the label changed the result: %s %d bytes vs %s %d bytes",
			labelled.MIME, len(labelled.Data), plain.MIME, len(plain.Data))
	}
}

// TestLogoBomb_HugeHeaderRefusedBeforeDecode is İddia B's bomb test: a PNG of under a
// hundred bytes whose IHDR says 30000×30000 and a JPEG whose SOF0 says the same are refused as
// ErrLogoDimensions, and the whole call allocates what reading and DecodeConfig
// allocate (ADR 0024 S3/S5: 1 088 B and 13 616 B), not what a decode would. The
// positive control is the measurement itself: the same call on a header that passes the
// gate (2048×2048, no pixel data) does decode, and allocates the 16 MiB image before
// failing.
func TestLogoBomb_HugeHeaderRefusedBeforeDecode(t *testing.T) {
	bombs := []struct {
		name string
		data []byte
	}{
		{"png 30000x30000", logoPNGHeaderOnly(t, 30000, 30000)},
		{"jpeg 30000x30000", logoJPEGHeaderOnly(30000, 30000)},
	}
	for _, b := range bombs {
		var err error
		n := logoAllocs(func() { _, err = logoNormalize(t, b.data) })
		if !errors.Is(err, ErrLogoDimensions) {
			t.Errorf("%s: err = %v, want ErrLogoDimensions", b.name, err)
		}
		t.Logf("%s: %d input bytes, %d bytes allocated (%.1f per input byte)", b.name, len(b.data), n, float64(n)/float64(len(b.data)))
		if n > 64<<10 {
			t.Errorf("%s: refusing allocated %d bytes; a decode-sized allocation happened before the size gate", b.name, n)
		}
	}
	// ADR 0024 S3's file was 75 bytes; compress/flate's output for the five-byte IDAT
	// differs between Go releases (75 on 1.27.1, 72 on 1.26.7), so the bound is loose.
	if len(bombs[0].data) > 100 {
		t.Errorf("the PNG bomb is %d bytes; it should be the S3 shape, under a hundred", len(bombs[0].data))
	}

	control := logoPNGHeaderOnly(t, 2048, 2048)
	var err error
	n := logoAllocs(func() { _, err = logoNormalize(t, control) })
	if !errors.Is(err, ErrLogoCorrupt) {
		t.Fatalf("POSITIVE CONTROL: 2048×2048 header without pixels: err = %v, want ErrLogoCorrupt", err)
	}
	if n < 16<<20 {
		t.Fatalf("POSITIVE CONTROL FAILED: a call that decodes allocated only %d bytes; the measurement cannot tell a decode from a refusal", n)
	}
	t.Logf("positive control (2048×2048 header, decoded): %d bytes allocated", n)
}

// logoPNGHeaderOnly is a PNG whose IHDR says w×h (RGBA, 8-bit) followed by five bytes
// of pixel data: ADR 0024 S3/S4's shape.
func logoPNGHeaderOnly(t testing.TB, w, h uint32) []byte {
	return imgPNGFile(imgPNGIHDR(w, h, 8, 6, 0), imgPNGChunk("IDAT", imgZlib(t, []byte{0, 0, 0, 0, 0})), imgPNGChunk("IEND", nil))
}

// logoJPEGHeaderOnly is a 64×64 gray JPEG whose SOF0 is rewritten to say w×h: the
// header claims w×h, the data is for 64×64 (ADR 0024 S5's shape).
func logoJPEGHeaderOnly(w, h int) []byte {
	out := imgGrayForge(64, 64, func(int, int) int { return 128 }).bytes()
	sof := bytes.Index(out, []byte{0xFF, 0xC0})
	out[sof+5], out[sof+6], out[sof+7], out[sof+8] = byte(h>>8), byte(h), byte(w>>8), byte(w)
	return out
}

// TestLogoDimensions_EachEdgeAndThePixelCount drives §2.4's gate through Normalize at
// both edges of the range, and the bare predicate at the same points. Accepted sizes
// are real gray images; refused sizes are header-only files, which the size gate
// refuses before their (missing) pixel data would matter.
func TestLogoDimensions_EachEdgeAndThePixelCount(t *testing.T) {
	cases := []struct {
		w, h int
		ok   bool
	}{
		{16, 16, true}, {15, 16, false}, {16, 15, false},
		{2048, 16, true}, {16, 2048, true}, {2049, 16, false}, {16, 2049, false},
		{2048, 2048, true}, {2049, 2049, false}, {4096, 1024, false}, {65535, 65535, false},
	}
	for _, c := range cases {
		if got := logoDimensionsAllowed(c.w, c.h); got != c.ok {
			t.Errorf("logoDimensionsAllowed(%d, %d) = %v, want %v", c.w, c.h, got, c.ok)
		}
		for _, format := range []string{"png", "jpeg"} {
			var in []byte
			switch {
			case format == "png" && c.ok:
				in = imgEncodePNG(t, image.NewGray(image.Rect(0, 0, c.w, c.h)))
			case c.ok:
				in = imgEncodeJPEG(t, image.NewGray(image.Rect(0, 0, c.w, c.h)), 85)
			case format == "png":
				in = logoPNGHeaderOnly(t, uint32(c.w), uint32(c.h))
			default:
				in = logoJPEGHeaderOnly(c.w, c.h)
			}
			l, err := logoNormalize(t, in)
			switch {
			case c.ok && err != nil:
				t.Errorf("%s %d×%d: %v", format, c.w, c.h, err)
			case c.ok:
				logoCheckOutput(t, l)
			case !errors.Is(err, ErrLogoDimensions):
				t.Errorf("%s %d×%d: err = %v, want ErrLogoDimensions", format, c.w, c.h, err)
			}
		}
	}
	// Edges outside the formats' own ranges: PNG refuses a zero or negative edge in its
	// own DecodeConfig (the format gate), a JPEG with height 0 reaches ours.
	zero := imgPNGFile(imgPNGIHDR(0, 16, 8, 6, 0), imgPNGChunk("IEND", nil))
	negative := imgPNGFile(imgPNGIHDR(0x80000000, 16, 8, 6, 0), imgPNGChunk("IEND", nil))
	for name, in := range map[string][]byte{"png zero width": zero, "png negative width": negative} {
		if _, err := logoNormalize(t, in); !errors.Is(err, ErrLogoFormat) {
			t.Errorf("%s: err = %v, want ErrLogoFormat", name, err)
		}
	}
	zeroH := imgGrayForge(64, 64, func(int, int) int { return 128 }).bytes()
	sof := bytes.Index(zeroH, []byte{0xFF, 0xC0})
	zeroH[sof+5], zeroH[sof+6] = 0, 0
	if _, err := logoNormalize(t, zeroH); !errors.Is(err, ErrLogoDimensions) {
		t.Errorf("jpeg height 0: err = %v, want ErrLogoDimensions", err)
	}
}

// TestLogoTruncated_EveryPrefixRefused cuts a PNG, a baseline JPEG, a progressive JPEG
// and an interlaced PNG at every length short of the whole file: no prefix is accepted
// (ADR 0024 S15). The whole files are the positive control.
func TestLogoTruncated_EveryPrefixRefused(t *testing.T) {
	prog := imgGrayForge(48, 40, func(bx, by int) int { return 40 + 20*bx + by })
	prog.progressive = true
	files := map[string][]byte{
		"png":              imgEncodePNG(t, imgQuadrants(40, 24)),
		"jpeg":             imgEncodeJPEG(t, imgQuadrants(40, 24), 85),
		"progressive jpeg": prog.bytes(),
		"interlaced png": imgPNGForged(t, 24, 20, 8, 6, true,
			func(x, y int) []byte { return []byte{byte(x * 9), byte(y * 11), 90, 255} }, nil, nil),
	}
	for name, whole := range files {
		logoCheckOutput(t, logoMustNormalize(t, whole))
		for n := 0; n < len(whole); n++ {
			if l, err := logoNormalize(t, whole[:n]); err == nil {
				t.Errorf("%s cut to %d of %d bytes was accepted (%s %d×%d)", name, n, len(whole), l.MIME, l.Width, l.Height)
			} else if !logoIsClass(err) {
				t.Errorf("%s cut to %d bytes: err %v is not one of the refusal classes", name, n, err)
			}
		}
	}
}

func logoIsClass(err error) bool {
	for _, c := range logoClasses {
		if errors.Is(err, c) {
			return true
		}
	}
	return false
}

// TestLogoMetadata_PNGChunksAndTrailingBytesDoNotSurvive is İddia C for PNG (ADR 0024
// S11, S12): a PNG carrying eXIf with a GPS IFD, tEXt and zTXt with a script, iCCP, and
// HTML after IEND comes out as IHDR IDAT IEND with nothing after it.
func TestLogoMetadata_PNGChunksAndTrailingBytesDoNotSurvive(t *testing.T) {
	exif := imgExifGPS()[4+6:] // the TIFF body of the APP1 segment
	extra := [][]byte{
		imgPNGChunk("eXIf", exif),
		imgPNGChunk("tEXt", []byte("Comment\x00<script>alert(1)</script>")),
		imgPNGChunk("zTXt", append([]byte("Comment\x00\x00"), imgZlib(t, []byte("<script>alert(2)</script>"))...)),
		imgPNGChunk("iCCP", append([]byte("icc\x00\x00"), imgZlib(t, bytes.Repeat([]byte("ICC"), 40))...)),
	}
	trailer := []byte("<html><script>alert(3)</script></html>")
	in := imgPNGForged(t, 64, 64, 8, 6, false,
		func(x, y int) []byte { return []byte{byte(4 * x), byte(4 * y), 128, 255} }, extra, trailer)
	if types, tail, ok := imgPNGChunkTypes(in); !ok || len(types) != 7 || tail != len(trailer) {
		t.Fatalf("fixture: chunks %v, %d trailing bytes, ok %v", types, tail, ok)
	}

	l := logoMustNormalize(t, in)
	logoCheckOutput(t, l)
	types, tail, ok := imgPNGChunkTypes(l.Data)
	if !ok || strings.Join(types, " ") != "IHDR IDAT IEND" || tail != 0 {
		t.Errorf("output chunks %v, %d bytes after IEND (ok %v); want IHDR IDAT IEND and nothing after", types, tail, ok)
	}
	for _, needle := range []string{"<script", "<html", "MM\x00\x2a", "eXIf", "tEXt", "zTXt", "iCCP"} {
		if bytes.Contains(l.Data, []byte(needle)) {
			t.Errorf("output carries %q", needle)
		}
	}
}

// TestLogoMetadata_ExifGPSDoesNotReachTheOutput is İddia C for JPEG and CLAUDE.md §4.2
// (ADR 0024 S13): a JPEG with an Exif APP1 holding a GPS IFD, an APP2 ICC_PROFILE, a
// COM and an APP15 with a script, and HTML after EOI comes out with no APP or COM
// segment, no "Exif", no TIFF header, no "ICC_PROFILE", no script and nothing after
// EOI; its segments are S14's set.
func TestLogoMetadata_ExifGPSDoesNotReachTheOutput(t *testing.T) {
	base := imgEncodeJPEG(t, imgQuadrants(96, 64), 90)
	in := imgJPEGInsertAfterSOI(base,
		imgExifGPS(),
		imgJPEGSegment(0xE2, append([]byte("ICC_PROFILE\x00\x01\x01"), bytes.Repeat([]byte("ICC"), 40)...)),
		imgJPEGSegment(0xFE, []byte("<script>alert(1)</script>")),
		imgJPEGSegment(0xEF, []byte("<html><body>polyglot</body></html>")),
	)
	in = append(in, []byte("<html><script>alert(2)</script></html>")...)
	if !bytes.Contains(in, []byte("Exif\x00\x00MM\x00\x2a")) {
		t.Fatal("fixture carries no Exif TIFF header")
	}

	l := logoMustNormalize(t, in)
	logoCheckOutput(t, l)
	markers, clean := imgJPEGWalk(l.Data)
	if !clean {
		t.Fatalf("output does not walk cleanly to EOI at its last byte: %x", markers)
	}
	allowed := map[byte]bool{0xD8: true, 0xDB: true, 0xC0: true, 0xC4: true, 0xDA: true, 0xD9: true}
	for _, m := range markers {
		if !allowed[m] {
			t.Errorf("output carries marker FF %02X; S14's set is SOI DQT SOF0 DHT SOS EOI", m)
		}
	}
	for _, needle := range []string{"Exif", "MM\x00\x2a", "II\x2a\x00", "ICC_PROFILE", "<script", "<html", "polyglot"} {
		if bytes.Contains(l.Data, []byte(needle)) {
			t.Errorf("output carries %q", needle)
		}
	}
	if bytes.Contains(l.Data, []byte{0xFF, 0xE1}) {
		t.Error("output carries an APP1 marker")
	}
}

// logoSamplePoints are interior points of imgQuadrants' four quadrants, as fractions.
var logoSamplePoints = [][2]float64{{0.25, 0.25}, {0.75, 0.25}, {0.25, 0.75}, {0.75, 0.75}}

// logoNRGBAAt is m's non-premultiplied 8-bit colour at (x, y).
func logoNRGBAAt(m image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(m.At(x, y)).(color.NRGBA)
}

func logoNear(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) bool { v := int(x) - int(y); return v <= tol && v >= -tol }
	if a.A == 0 && b.A == 0 {
		return true
	}
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

// TestLogoNormalize_EveryDecodedLayout is the normalization row of WL-3: CMYK and YCCK
// JPEG (baseline and progressive), gray and 4:2:0 JPEG, 16-bit PNG (RGBA, RGB, gray),
// paletted PNG with tRNS at 1, 2, 4 and 8 bits, interlaced PNG at 8 and 16 bits, and
// gray+alpha PNG. Each comes out in its own format, at the size logoTargetSize gives,
// and with the colour the standard decoder reads from the input at four interior points.
func TestLogoNormalize_EveryDecodedLayout(t *testing.T) {
	quad := imgQuadrants(600, 400)
	rgba64 := image.NewRGBA64(quad.Bounds())
	nrgba64 := image.NewNRGBA64(quad.Bounds())
	gray16 := image.NewGray16(quad.Bounds())
	for y := 0; y < 400; y++ {
		for x := 0; x < 600; x++ {
			c := quad.NRGBAAt(x, y)
			rgba64.Set(x, y, c)
			nc := color.NRGBA64{R: uint16(c.R) * 257, G: uint16(c.G) * 257, B: uint16(c.B) * 257, A: 0xffff}
			if x < 300 && y < 200 {
				nc.A = 0x8000
			}
			nrgba64.SetNRGBA64(x, y, nc)
			gray16.SetGray16(x, y, color.Gray16Model.Convert(c).(color.Gray16))
		}
	}
	paletted := func(n int) *image.Paletted {
		pal := color.Palette{color.NRGBA{0, 0, 0, 0}, color.NRGBA{200, 30, 40, 255}, color.NRGBA{20, 160, 60, 255}, color.NRGBA{30, 60, 190, 255}}
		for len(pal) < n {
			pal = append(pal, color.NRGBA{byte(len(pal)), 9, 9, 255})
		}
		m := image.NewPaletted(image.Rect(0, 0, 300, 200), pal[:n])
		for y := 0; y < 200; y++ {
			for x := 0; x < 300; x++ {
				k := 0
				if x >= 150 {
					k++
				}
				if y >= 100 {
					k += 2
				}
				m.SetColorIndex(x, y, uint8(k%n))
			}
		}
		return m
	}
	quad700 := imgQuadrants(700, 300)
	interlaced8 := imgPNGForged(t, 700, 300, 8, 6, true, func(x, y int) []byte {
		c := quad700.NRGBAAt(x, y)
		return []byte{c.R, c.G, c.B, c.A}
	}, nil, nil)
	interlaced16 := imgPNGForged(t, 520, 520, 16, 6, true, func(x, y int) []byte {
		c := quad.NRGBAAt(x*600/520, y*400/520)
		return []byte{c.R, c.R, c.G, c.G, c.B, c.B, 0xFF, 0xFF}
	}, nil, nil)
	grayAlpha := imgPNGForged(t, 64, 64, 8, 4, false, func(x, y int) []byte {
		return []byte{byte(x * 4), byte(255 - y*2)}
	}, nil, nil)
	cmykProg := imgCMYKForge(640, 480, 0, [4]int{230, 120, 40, 250})
	cmykProg.progressive = true
	ycckProg := imgCMYKForge(80, 80, 2, [4]int{90, 150, 110, 240})
	ycckProg.progressive = true
	grayProg := imgGrayForge(1000, 600, func(bx, by int) int { return 60 + (bx/62)*60 + (by/37)*20 })
	grayProg.progressive = true

	cases := []struct {
		name string
		in   []byte
		mime string
		want string // the decoded input's type, so the row is the layout it names
	}{
		{"jpeg 4:2:0", imgEncodeJPEG(t, quad, 90), LogoMIMEJPEG, "*image.YCbCr"},
		{"jpeg gray", imgEncodeJPEG(t, image.NewGray(image.Rect(0, 0, 64, 64)), 90), LogoMIMEJPEG, "*image.Gray"},
		{"jpeg cmyk baseline", imgCMYKForge(640, 480, 0, [4]int{230, 120, 40, 250}).bytes(), LogoMIMEJPEG, "*image.CMYK"},
		{"jpeg cmyk progressive", cmykProg.bytes(), LogoMIMEJPEG, "*image.CMYK"},
		{"jpeg ycck baseline", imgCMYKForge(80, 80, 2, [4]int{90, 150, 110, 240}).bytes(), LogoMIMEJPEG, "*image.CMYK"},
		{"jpeg ycck progressive", ycckProg.bytes(), LogoMIMEJPEG, "*image.CMYK"},
		{"jpeg gray progressive", grayProg.bytes(), LogoMIMEJPEG, "*image.Gray"},
		{"png rgba 8", imgEncodePNG(t, quad), LogoMIMEPNG, "*image.RGBA"},
		{"png rgba 16 opaque", imgEncodePNG(t, rgba64), LogoMIMEPNG, "*image.RGBA64"},
		{"png nrgba 16 alpha", imgEncodePNG(t, nrgba64), LogoMIMEPNG, "*image.NRGBA64"},
		{"png gray 16", imgEncodePNG(t, gray16), LogoMIMEPNG, "*image.Gray16"},
		{"png paletted 1-bit tRNS", imgEncodePNG(t, paletted(2)), LogoMIMEPNG, "*image.Paletted"},
		{"png paletted 2-bit tRNS", imgEncodePNG(t, paletted(4)), LogoMIMEPNG, "*image.Paletted"},
		{"png paletted 4-bit tRNS", imgEncodePNG(t, paletted(16)), LogoMIMEPNG, "*image.Paletted"},
		{"png paletted 8-bit tRNS", imgEncodePNG(t, paletted(256)), LogoMIMEPNG, "*image.Paletted"},
		{"png interlaced 8", interlaced8, LogoMIMEPNG, "*image.NRGBA"},
		{"png interlaced 16", interlaced16, LogoMIMEPNG, "*image.NRGBA64"},
		{"png gray+alpha", grayAlpha, LogoMIMEPNG, "*image.NRGBA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var src image.Image
			var err error
			if tc.mime == LogoMIMEPNG {
				src, err = png.Decode(bytes.NewReader(tc.in))
			} else {
				src, err = jpeg.Decode(bytes.NewReader(tc.in))
			}
			if err != nil {
				t.Fatalf("fixture does not decode: %v", err)
			}
			if got := logoTypeName(src); got != tc.want {
				t.Fatalf("fixture decodes as %s, the row is about %s", got, tc.want)
			}
			l := logoMustNormalize(t, tc.in)
			out := logoCheckOutput(t, l)
			if l.MIME != tc.mime {
				t.Errorf("MIME %s, want %s", l.MIME, tc.mime)
			}
			sb := src.Bounds()
			ww, wh := logoTargetSize(sb.Dx(), sb.Dy())
			if l.Width != ww || l.Height != wh {
				t.Errorf("size %d×%d, want %d×%d", l.Width, l.Height, ww, wh)
			}
			tol := 2
			if tc.mime == LogoMIMEJPEG {
				tol = 8
			}
			for _, p := range logoSamplePoints {
				want := logoNRGBAAt(src, sb.Min.X+int(p[0]*float64(sb.Dx())), sb.Min.Y+int(p[1]*float64(sb.Dy())))
				got := logoNRGBAAt(out, int(p[0]*float64(l.Width)), int(p[1]*float64(l.Height)))
				if !logoNear(got, want, tol) {
					t.Errorf("at %v: output %v, input %v (tolerance %d)", p, got, want, tol)
				}
			}
		})
	}
}

func logoTypeName(v any) string {
	switch v.(type) {
	case *image.YCbCr:
		return "*image.YCbCr"
	case *image.Gray:
		return "*image.Gray"
	case *image.Gray16:
		return "*image.Gray16"
	case *image.CMYK:
		return "*image.CMYK"
	case *image.RGBA:
		return "*image.RGBA"
	case *image.RGBA64:
		return "*image.RGBA64"
	case *image.NRGBA:
		return "*image.NRGBA"
	case *image.NRGBA64:
		return "*image.NRGBA64"
	case *image.Paletted:
		return "*image.Paletted"
	}
	return "other"
}

// TestLogoOutput_Within512AndTheSizeLimit is the ≤512 px, ≤256 KiB row: long edges
// above 512 come down to 512 with the short edge rounded and at least one; noise whose
// PNG does not fit 256 KiB is refused as ErrLogoOutputTooLarge; and the size
// predicate's boundary is 262 144 bytes exactly.
func TestLogoOutput_Within512AndTheSizeLimit(t *testing.T) {
	sizes := []struct{ w, h, ww, wh int }{
		{2048, 1024, 512, 256}, {2048, 16, 512, 4}, {16, 2048, 4, 512}, {600, 16, 512, 14},
		{513, 100, 512, 100}, {512, 512, 512, 512}, {100, 513, 100, 512}, {2048, 2048, 512, 512},
		{16, 16, 16, 16}, {511, 17, 511, 17},
	}
	for _, s := range sizes {
		if w, h := logoTargetSize(s.w, s.h); w != s.ww || h != s.wh {
			t.Errorf("logoTargetSize(%d, %d) = %d×%d, want %d×%d", s.w, s.h, w, h, s.ww, s.wh)
		}
	}
	for _, s := range sizes[:3] {
		in := imgEncodePNG(t, imgQuadrants(s.w, s.h))
		l := logoMustNormalize(t, in)
		logoCheckOutput(t, l)
		if l.Width != s.ww || l.Height != s.wh {
			t.Errorf("%d×%d came out %d×%d", s.w, s.h, l.Width, l.Height)
		}
	}

	if !logoOutputSizeAllowed(LogoMaxOutputBytes) || logoOutputSizeAllowed(LogoMaxOutputBytes+1) {
		t.Errorf("the output limit is not 262 144 bytes inclusive")
	}
	if LogoMaxOutputBytes != 262144 {
		t.Errorf("LogoMaxOutputBytes = %d; tenant_branding's CHECK is 262 144", LogoMaxOutputBytes)
	}

	// 400×400 noise is the largest square that still fits the 512 KiB input as a PNG.
	noise := imgEncodePNG(t, imgNoise(400, 400, 7))
	if _, err := logoNormalize(t, noise); !errors.Is(err, ErrLogoOutputTooLarge) {
		t.Errorf("noise PNG of %d bytes: err = %v, want ErrLogoOutputTooLarge", len(noise), err)
	}
	jn := imgEncodeJPEG(t, imgNoise(512, 512, 7), 85)
	l, err := logoNormalize(t, jn)
	t.Logf("noise: 400×400 PNG input %d B refused; 512×512 JPEG q85 input %d B -> err %v, output %d B", len(noise), len(jn), err, len(l.Data))
}

// logoDQT is a JPEG's first DQT segment, length field included.
func logoDQT(t testing.TB, data []byte) []byte {
	t.Helper()
	i := bytes.Index(data, []byte{0xFF, 0xDB})
	if i < 0 || i+4 > len(data) {
		t.Fatal("no DQT segment")
	}
	n := int(data[i+2])<<8 | int(data[i+3])
	return data[i : i+2+n]
}

// TestLogoOutput_JPEGQualityIs85 pins ADR 0024 §3's "JPEG → JPEG (q85)" by the
// quantization tables the output carries: they are the ones image/jpeg writes at
// quality 85. The control: its tables at 84 and 86 differ, so the comparison can see
// a change of one step.
func TestLogoOutput_JPEGQualityIs85(t *testing.T) {
	l := logoMustNormalize(t, imgEncodeJPEG(t, imgQuadrants(600, 400), 95))
	ref := func(q int) []byte { return logoDQT(t, imgEncodeJPEG(t, imgQuadrants(16, 16), q)) }
	if !bytes.Equal(logoDQT(t, l.Data), ref(85)) {
		t.Error("the output's quantization tables are not image/jpeg's at quality 85")
	}
	if bytes.Equal(ref(84), ref(85)) || bytes.Equal(ref(86), ref(85)) {
		t.Fatal("CONTROL FAILED: neighbouring qualities write the same tables")
	}
}

// TestLogoInput_OverTheLimitRefusedBeforeInspection: Normalize reads one byte past
// 512 KiB and refuses, so a caller that forgot its own LimitReader is still covered.
func TestLogoInput_OverTheLimitRefusedBeforeInspection(t *testing.T) {
	in := imgEncodePNG(t, imgQuadrants(32, 32))
	big := append(append([]byte(nil), in...), make([]byte, LogoMaxInputBytes+1-len(in))...)
	if _, err := logoNormalize(t, big); !errors.Is(err, ErrLogoInputTooLarge) {
		t.Errorf("512 KiB + 1: err = %v, want ErrLogoInputTooLarge", err)
	}
	exact := big[:LogoMaxInputBytes]
	l, err := logoNormalize(t, exact)
	if err != nil {
		t.Fatalf("a 512 KiB PNG (image then padding after IEND): %v", err)
	}
	logoCheckOutput(t, l)

	broken := io.MultiReader(bytes.NewReader(in[:20]), logoErrReader{})
	_, err = logoNewGate(t, 1).Normalize(context.Background(), broken)
	if !errors.Is(err, ErrLogoRead) || !errors.Is(err, errLogoTestRead) {
		t.Errorf("a failing reader: err = %v, want ErrLogoRead wrapping the reader's error", err)
	}
}

// TestLogoRead_AllocatesBoundedByTheLimit reads uploads of nine sizes around the
// two buffer edges (4 KiB, 512 KiB) through three reader shapes — whole, one byte at a
// time, and half of each request with EOF on the final data (iotest.DataErrReader; the
// other two return their last data and then a separate EOF): each of the 27 reads
// returns min(size, limit+1) bytes equal to the upload's prefix and allocates at most
// the limit plus 32 KiB — the limit plus one byte is a 520 KiB allocation in Go's 8 KiB
// pages, then the 4 KiB buffer and the test's readers. Measured on go1.27.1 and
// go1.26.6: the 17 reads that need the second buffer (4 097 bytes and up in the three
// shapes; 4 096 bytes in "whole" and "one byte") 536 624–537 728 B; the 10 that end in
// the first (0, 1 and 4 095 bytes in the three shapes; 4 096 bytes in "half", whose EOF
// comes with the last data) 4 144–5 248 B. It is the read the full-gate and
// ended-context tests' 1 MiB holds.
func TestLogoRead_AllocatesBoundedByTheLimit(t *testing.T) {
	const bound = LogoMaxInputBytes + 32<<10
	sizes := []int{0, 1, 4<<10 - 1, 4 << 10, 4<<10 + 1, 518100, LogoMaxInputBytes, LogoMaxInputBytes + 1, 600 << 10}
	shapes := map[string]func(io.Reader) io.Reader{
		"whole":    func(r io.Reader) io.Reader { return r },
		"one byte": iotest.OneByteReader,
		"half":     func(r io.Reader) io.Reader { return iotest.DataErrReader(iotest.HalfReader(r)) },
	}
	for _, size := range sizes {
		src := make([]byte, size)
		for i := range src {
			src[i] = byte(i * 7)
		}
		want := src[:min(size, LogoMaxInputBytes+1)]
		for name, shape := range shapes {
			var got []byte
			var err error
			n := logoAllocs(func() { got, err = logoRead(shape(bytes.NewReader(src))) })
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("%s, %d B: got %d B, err %v; want %d B", name, size, len(got), err, len(want))
			}
			if n > bound {
				t.Errorf("%s, %d B: the read allocated %d bytes, over %d", name, size, n, bound)
			}
			t.Logf("%-8s %7d B: %d bytes allocated", name, size, n)
		}
	}
}

var errLogoTestRead = errors.New("iotest: read failed")

type logoErrReader struct{}

func (logoErrReader) Read([]byte) (int, error) { return 0, errLogoTestRead }

// TestLogoErrors_TextIsTheClassOnly: a refusal's text is its class's text, so a log line
// built from the error carries no value read from the upload (ADR 0024 §6). Two causes
// that carry one are measured: a PNG chunk length image/png prints into its own message
// (ErrLogoCorrupt), and a quoted-printable body whose reader names the bad input byte
// (ErrLogoRead — multipart.Part decodes that transfer encoding for its caller). The
// controls show the value is in each cause's own message: image/png's error through
// errors.As, the quoted-printable reader's by reading the same body directly.
func TestLogoErrors_TextIsTheClassOnly(t *testing.T) {
	// An unescaped control byte: the quoted-printable reader names it in its error
	// ("invalid unescaped byte 0x01 in body"); an invalid =XX escape it passes through.
	const body = "logo bytes \x01"
	_, err := logoNewGate(t, 1).Normalize(context.Background(), quotedprintable.NewReader(strings.NewReader(body)))
	if !errors.Is(err, ErrLogoRead) {
		t.Fatalf("a failing quoted-printable body: err = %v, want ErrLogoRead", err)
	}
	if err.Error() != ErrLogoRead.Error() {
		t.Errorf("error text %q is not the class text", err.Error())
	}
	if _, cerr := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body))); cerr == nil || !strings.Contains(cerr.Error(), "0x01") {
		t.Fatalf("CONTROL FAILED: the reader's own error (%v) does not carry the input byte; the case shows nothing", cerr)
	}

	good := imgEncodePNG(t, imgQuadrants(32, 32))
	// After IHDR, an ancillary chunk whose length is 0xFABCDEF1: over 2³¹-1, which
	// image/png reports with the value in its message.
	ihdrEnd := len(imgPNGSignature) + 25
	bad := append(append(append([]byte(nil), good[:ihdrEnd]...), 0xFA, 0xBC, 0xDE, 0xF1, 't', 'E', 'X', 't'), good[ihdrEnd:]...)
	_, err = logoNormalize(t, bad)
	if !errors.Is(err, ErrLogoCorrupt) {
		t.Fatalf("err = %v, want ErrLogoCorrupt", err)
	}
	if err.Error() != ErrLogoCorrupt.Error() {
		t.Errorf("error text %q is not the class text", err.Error())
	}
	var fe png.FormatError
	if !errors.As(err, &fe) || !strings.Contains(fe.Error(), "4206681841") {
		t.Fatalf("CONTROL FAILED: the decoder's own message (%v) does not carry the input value; the case shows nothing", fe)
	}

	for _, in := range [][]byte{nil, []byte("<svg/>"), imgPNGFile(imgPNGIHDR(30000, 30000, 8, 6, 0))} {
		_, err := logoNormalize(t, in)
		matched := 0
		for _, c := range logoClasses {
			if errors.Is(err, c) {
				matched++
				if err.Error() != c.Error() {
					t.Errorf("error text %q, class text %q", err.Error(), c.Error())
				}
			}
		}
		if matched != 1 {
			t.Errorf("err %v matches %d classes, want 1", err, matched)
		}
	}
}

// TestLogoVerifyOutput_RefusesWhatTheFilterDidNotMake drives ADR 0024 §3's second
// decode directly, since on the inputs the other tests make the standard encoders give
// it nothing to refuse: bytes of the other format, a size other than the filter's, a
// side over 512, a cut file and a JPEG over the scan ceiling are each ErrLogoVerify; the
// matching file is the positive control.
func TestLogoVerifyOutput_RefusesWhatTheFilterDidNotMake(t *testing.T) {
	pngOut := imgEncodePNG(t, imgQuadrants(20, 10))
	jpegOut := imgEncodeJPEG(t, imgQuadrants(20, 10), 85)
	if err := logoVerifyOutput(logoFormatPNG, pngOut, 20, 10); err != nil {
		t.Fatalf("POSITIVE CONTROL: %v", err)
	}
	if err := logoVerifyOutput(logoFormatJPEG, jpegOut, 20, 10); err != nil {
		t.Fatalf("POSITIVE CONTROL: %v", err)
	}
	wide := imgEncodePNG(t, image.NewGray(image.Rect(0, 0, 513, 4)))
	scans := logoScanFile(20, 10, LogoJPEGScanCeiling+1, 90, "")
	cases := []struct {
		name   string
		format logoFormat
		out    []byte
		w, h   int
	}{
		{"png bytes as jpeg", logoFormatJPEG, pngOut, 20, 10},
		{"jpeg bytes as png", logoFormatPNG, jpegOut, 20, 10},
		{"another width", logoFormatPNG, pngOut, 21, 10},
		{"another height", logoFormatJPEG, jpegOut, 20, 11},
		{"over 512", logoFormatPNG, wide, 513, 4},
		{"zero", logoFormatPNG, pngOut, 0, 10},
		{"cut png", logoFormatPNG, pngOut[:len(pngOut)-20], 20, 10},
		{"cut jpeg", logoFormatJPEG, jpegOut[:len(jpegOut)-20], 20, 10},
		{"jpeg over the scan ceiling", logoFormatJPEG, scans, 20, 10},
	}
	for _, c := range cases {
		if err := logoVerifyOutput(c.format, c.out, c.w, c.h); !errors.Is(err, ErrLogoVerify) {
			t.Errorf("%s: err = %v, want ErrLogoVerify", c.name, err)
		}
	}
	if _, err := jpeg.Decode(bytes.NewReader(scans)); err != nil {
		t.Fatalf("CONTROL: the over-ceiling file does not decode (%v); only the count can refuse it", err)
	}
}
