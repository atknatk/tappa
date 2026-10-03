package brand

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
)

// The numbers ADR 0024 fixes for an uploaded tenant logo (§2 before the decode, §3 after
// it). The output limits are the tenant_branding CHECKs of the same ADR §4.
const (
	// LogoMaxInputBytes is §2.2's part limit. Normalize reads at most one byte past it
	// (logoRead) and refuses what reaches that byte, so a caller can hand it the part
	// directly (TestLogoInput_OverTheLimitRefusedBeforeInspection).
	LogoMaxInputBytes = 512 << 10
	// LogoMaxOutputBytes is §3's re-encoded size limit (tenant_branding.logo CHECK).
	LogoMaxOutputBytes = 256 << 10
	// LogoMaxOutputEdge is §3's long edge after the box filter (logo_width/height ≤ 512).
	LogoMaxOutputEdge = 512
	// LogoJPEGScanCeiling is §2.5's scan ceiling. image/jpeg neither limits scans nor
	// reads a context, and a 2048×2048 scan whose blocks an end-of-band run skips costs
	// about twenty bytes of input and a pass over 65 536 blocks. The measured encoders
	// wrote 6, 10 and 18 scans; the WL-3 note in ADR 0024 records the decode times at
	// this ceiling and why 100.
	LogoJPEGScanCeiling = 100
	// LogoDecodeSlots is §2.6's N: a slot holds a core for the length of a decode, and
	// N < GOMAXPROCS leaves one to the tap path. GOMAXPROCS = 2 under the pod's CPU
	// limit is ADR 0024's derivation; it has not been measured in the pod.
	LogoDecodeSlots = 1

	// LogoMIMEPNG and LogoMIMEJPEG are the two values logoFormat.mime returns for
	// Logo.MIME (tenant_branding.logo_mime CHECK).
	LogoMIMEPNG  = "image/png"
	LogoMIMEJPEG = "image/jpeg"

	logoMinEdge     = 16
	logoMaxEdge     = 2048
	logoMaxPixels   = 1 << 22
	logoJPEGQuality = 85
)

// The refusal classes; ADR 0024 §6 logs a refusal by its class. Normalize builds its
// refusals as logoError values, whose text is the class's text. Two returns are not
// refusals of the upload and are not classes: the context's error (wrapped, when ctx
// ends before or during the decode) and an encoder error (wrapped). Measured:
// TestLogoErrors_TextIsTheClassOnly on its listed inputs, and FuzzNormalize on every
// input of a run (exactly one class, the class's text).
var (
	ErrLogoBusy           = errors.New("brand: logo decoder busy, try again")
	ErrLogoInputTooLarge  = errors.New("brand: logo upload over 512 KiB")
	ErrLogoRead           = errors.New("brand: logo upload could not be read")
	ErrLogoFormat         = errors.New("brand: logo is not a PNG or JPEG")
	ErrLogoDimensions     = errors.New("brand: logo dimensions out of range")
	ErrLogoScans          = errors.New("brand: logo JPEG has too many scans")
	ErrLogoCorrupt        = errors.New("brand: logo image data could not be decoded")
	ErrLogoOutputTooLarge = errors.New("brand: re-encoded logo over 256 KiB")
	ErrLogoVerify         = errors.New("brand: re-encoded logo failed its own check")
)

// logoError carries a refusal class and the error that caused it. Its text is the
// class's text only: the standard decoders' messages can carry values read from the
// upload (image/png formats "Bad chunk length: %d" from four input bytes, and "bit depth
// %d, color type %d"), and ADR 0024 §6 keeps the upload's bytes out of the log. The
// cause stays reachable through errors.As for a caller that needs it — a
// *http.MaxBytesError behind ErrLogoRead, for one.
type logoError struct {
	class error
	cause error
}

func (e *logoError) Error() string { return e.class.Error() }

func (e *logoError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.class}
	}
	return []error{e.class, e.cause}
}

// Logo is a normalized logo: the bytes that are stored and served — the re-encoder's
// output (ADR 0024 §3), not the uploaded bytes — their type, their size and their
// sha256.
type Logo struct {
	Data   []byte
	MIME   string // LogoMIMEPNG or LogoMIMEJPEG
	Width  int    // 1..LogoMaxOutputEdge
	Height int    // 1..LogoMaxOutputEdge
	SHA256 string // lowercase hex sha256 of Data

	// mint is what Normalize returned; this package sets it in one place, the return
	// of normalizeHeld. It is unexported, and Go does not let a composite literal
	// outside this package name it, so such a literal carries a nil mint (reflect and
	// unsafe aside). Normalized reads it (M10 WL-4).
	mint *logoMint
}

// logoMint is the five fields of a Logo as Normalize returned them, with the bytes
// kept as their digest.
type logoMint struct {
	sum    [sha256.Size]byte
	mime   string
	width  int
	height int
}

// Normalized reports whether l is a value Normalize returned whose five exported
// fields are still the ones Normalize set: Data hashes to the digest Normalize
// computed, SHA256 is that digest in lowercase hex, and MIME, Width and Height are
// the values Normalize returned. ADR 0024 §3 stores Normalize's output; the write side
// (internal/domain/tenant, WL-4) refuses a Logo for which this is false. Measured on a
// PNG's and a JPEG's Normalize output, a copy of each, each of the five fields
// changed, a byte of Data changed in place, a literal with the same five values and
// the zero Logo (TestLogo_NormalizedIsTrueOnlyForNormalizeOutput), and on the logos
// the package's tests hand to logoCheckOutput.
func (l Logo) Normalized() bool {
	if l.mint == nil {
		return false
	}
	sum := sha256.Sum256(l.Data)
	return sum == l.mint.sum &&
		l.SHA256 == hex.EncodeToString(sum[:]) &&
		l.MIME == l.mint.mime &&
		l.Width == l.mint.width &&
		l.Height == l.mint.height
}

// LogoGate is ADR 0024 §2.6's process-wide semaphore around the full-size decode. One
// gate is built at wiring and shared by the uploads; the slots are a field of it, not
// a package variable (CLAUDE.md §7).
type LogoGate struct {
	slots chan struct{}
	// stageHook, when set, is called at each logoStage. The package's tests set it to
	// stop a decode at a known point and look at the slot from outside; production
	// code leaves it nil.
	stageHook func(logoStage)
}

type logoStage int

const (
	logoStageAcquired logoStage = iota + 1
	logoStageDecoded
	logoStageResized
	logoStageEncoded
	logoStageReleasing
)

// NewLogoGate builds a gate with the given number of decode slots; production wiring
// passes LogoDecodeSlots.
func NewLogoGate(slots int) (*LogoGate, error) {
	if slots < 1 {
		return nil, fmt.Errorf("brand: logo gate needs at least one slot, got %d", slots)
	}
	return &LogoGate{slots: make(chan struct{}, slots)}, nil
}

// tryAcquire takes a slot without waiting (§2.6: a full gate refuses at once — a
// waiting request would keep its read body and its goroutine).
func (g *LogoGate) tryAcquire() bool {
	select {
	case g.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (g *LogoGate) release() { <-g.slots }

func (g *LogoGate) at(s logoStage) {
	if g.stageHook != nil {
		g.stageHook(s)
	}
}

// Normalize reads at most LogoMaxInputBytes from r and turns them into a Logo, or
// refuses them. It takes no client Content-Type and no file name: the type is read
// from the bytes (ADR 0024 §1).
//
// The read comes first and outside the slot, so a slow sender holds its own request,
// not a slot. A ctx that ended during the read is answered there, before a slot is
// taken (TestLogoGate_ContextEndedDuringTheReadTakesNoSlot). Then a slot is taken
// without waiting; a full gate refuses before the upload is parsed
// (TestLogoGate_FullGateRefusesBeforeDecoding measures what that refusal allocates).
// Inside the slot the four checks run in ADR 0024 §2's order, before a pixel is
// decoded: the sniffed type must be PNG or JPEG; that format's own DecodeConfig must
// succeed; each edge must be 16..2048 px and the pixel count at most 2²²; and a JPEG's
// scan count must be within LogoJPEGScanCeiling. The slot is held through those checks,
// the decode, the box filter, the encode and the output's own check, and given back by
// a deferred release when this call returns, not when ctx ends: the decoders do not
// read a context, and a slot freed early would leave a decode running outside the
// count. A ctx that ends during the decode is honoured once the decode returns.
func (g *LogoGate) Normalize(ctx context.Context, r io.Reader) (Logo, error) {
	if err := ctx.Err(); err != nil {
		return Logo{}, fmt.Errorf("brand: logo: %w", err)
	}
	data, err := logoRead(r)
	if err != nil {
		return Logo{}, &logoError{class: ErrLogoRead, cause: err}
	}
	if len(data) > LogoMaxInputBytes {
		return Logo{}, &logoError{class: ErrLogoInputTooLarge}
	}
	// A request that ended while its body was read does not take a slot: the decoders
	// do not read a context, so a decode it started would run to its end.
	if err := ctx.Err(); err != nil {
		return Logo{}, fmt.Errorf("brand: logo: %w", err)
	}
	if !g.tryAcquire() {
		return Logo{}, &logoError{class: ErrLogoBusy}
	}
	defer func() {
		g.at(logoStageReleasing)
		g.release()
	}()
	g.at(logoStageAcquired)
	return g.normalizeHeld(ctx, data)
}

// logoRead reads r to its end or to one byte past LogoMaxInputBytes, whichever comes
// first, into at most two buffers: 4 KiB, and — once that is full — one of the limit
// plus one byte, which Go's allocator rounds to 520 KiB of 8 KiB pages. What a read
// allocates is set by those two buffers, not by how r splits its data
// (TestLogoRead_AllocatesBoundedByTheLimit, three reader shapes on go1.27.1 and
// go1.26.6: at most the limit plus 32 KiB); io.ReadAll's growth differs between
// releases (measured, the same 518 KB upload: 1 065 360 B on 1.27.1, 2 128 048 B on
// 1.26.6 under -race).
func logoRead(r io.Reader) ([]byte, error) {
	buf := make([]byte, 4<<10)
	n := 0
	for {
		if n == len(buf) {
			if len(buf) > LogoMaxInputBytes {
				return buf, nil // one byte past the limit: the caller refuses
			}
			full := make([]byte, LogoMaxInputBytes+1)
			copy(full, buf)
			buf = full
		}
		m, err := r.Read(buf[n:])
		n += m
		if err == io.EOF {
			return buf[:n], nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// normalizeHeld is the part of Normalize that runs inside a slot.
func (g *LogoGate) normalizeHeld(ctx context.Context, data []byte) (Logo, error) {
	format, cfg, err := logoInspect(data)
	if err != nil {
		return Logo{}, err
	}
	src, err := format.decode(bytes.NewReader(data))
	g.at(logoStageDecoded)
	if err != nil {
		return Logo{}, &logoError{class: ErrLogoCorrupt, cause: err}
	}
	if b := src.Bounds(); b.Dx() != cfg.Width || b.Dy() != cfg.Height {
		return Logo{}, &logoError{class: ErrLogoCorrupt}
	}
	if err := ctx.Err(); err != nil {
		return Logo{}, fmt.Errorf("brand: logo: %w", err)
	}

	w, h := logoTargetSize(cfg.Width, cfg.Height)
	small := logoResize(src, w, h)
	g.at(logoStageResized)

	out, err := format.encode(small)
	g.at(logoStageEncoded)
	if err != nil {
		return Logo{}, fmt.Errorf("brand: logo: encode: %w", err)
	}
	if !logoOutputSizeAllowed(len(out)) {
		return Logo{}, &logoError{class: ErrLogoOutputTooLarge}
	}
	if err := logoVerifyOutput(format, out, w, h); err != nil {
		return Logo{}, err
	}
	sum := sha256.Sum256(out)
	return Logo{
		Data:   out,
		MIME:   format.mime(),
		Width:  w,
		Height: h,
		SHA256: hex.EncodeToString(sum[:]),
		mint:   &logoMint{sum: sum, mime: format.mime(), width: w, height: h},
	}, nil
}

// logoFormat is one of the two accepted formats. The decoder Normalize calls is the
// sniffed format's own, not the image package's registry, so the format that decodes
// is the format that was sniffed (ADR 0024 §1, third gate) by construction; the
// spelling is pinned by TestLogoDecode_CallsTheSniffedFormatsOwnDecoder.
type logoFormat int

const (
	logoFormatPNG logoFormat = iota + 1
	logoFormatJPEG
)

func (f logoFormat) mime() string {
	if f == logoFormatPNG {
		return LogoMIMEPNG
	}
	return LogoMIMEJPEG
}

func (f logoFormat) decodeConfig(r io.Reader) (image.Config, error) {
	if f == logoFormatPNG {
		return png.DecodeConfig(r)
	}
	return jpeg.DecodeConfig(r)
}

func (f logoFormat) decode(r io.Reader) (image.Image, error) {
	if f == logoFormatPNG {
		return png.Decode(r)
	}
	return jpeg.Decode(r)
}

// encode writes PNG as PNG at BestCompression and JPEG as JPEG at quality 85 (§3). It
// is handed the filter's NRGBA, built from pixel values alone, so the uploaded bytes do
// not reach it; which segments and chunks the standard encoders write is what ADR 0024
// S14 measured and the TestLogoMetadata_ tests measure on their inputs.
func (f logoFormat) encode(img *image.NRGBA) ([]byte, error) {
	var buf bytes.Buffer
	if f == logoFormatPNG {
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	var m image.Image = img
	if img.Opaque() {
		// An opaque NRGBA has the same bytes as an RGBA, and the JPEG encoder has a
		// direct path for *image.RGBA. image/jpeg's decoder returns opaque images (Gray,
		// YCbCr, CMYK, or RGBA written with alpha 255 — Go 1.27.1 reader.go), so a JPEG
		// upload takes this path; the generic one is for an image that is not opaque.
		m = &image.RGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
	}
	if err := jpeg.Encode(&buf, m, &jpeg.Options{Quality: logoJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// logoInspect holds the checks that come before the decode (ADR 0024 §1 and
// §2.3–§2.5).
func logoInspect(data []byte) (logoFormat, image.Config, error) {
	var format logoFormat
	switch http.DetectContentType(data) {
	case LogoMIMEPNG:
		format = logoFormatPNG
	case LogoMIMEJPEG:
		format = logoFormatJPEG
	default:
		return 0, image.Config{}, &logoError{class: ErrLogoFormat}
	}
	cfg, err := format.decodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, image.Config{}, &logoError{class: ErrLogoFormat, cause: err}
	}
	if !logoDimensionsAllowed(cfg.Width, cfg.Height) {
		return 0, image.Config{}, &logoError{class: ErrLogoDimensions}
	}
	if format == logoFormatJPEG && logoScanCount(data) > LogoJPEGScanCeiling {
		return 0, image.Config{}, &logoError{class: ErrLogoScans}
	}
	return format, cfg, nil
}

// logoDimensionsAllowed is §2.4: each edge 16..2048 px and at most 2²² pixels. With
// both edges at most 2048 the product is at most 2²², far inside int, and the pixel
// limit names the same set as the edge limit (2048² = 2²²); it is kept as the ADR
// writes it.
func logoDimensionsAllowed(w, h int) bool {
	if w < logoMinEdge || h < logoMinEdge || w > logoMaxEdge || h > logoMaxEdge {
		return false
	}
	return w*h <= logoMaxPixels
}

// logoScanCount counts the FF DA pairs in data: an upper bound on the scans
// image/jpeg's decoder runs on the same bytes, by reading its decode loop and byte
// readers — decode (Go 1.27.1 reader.go:525-671, 1.26.6 520-666), fill, readByte,
// readByteStuffedByte, unreadByteStuffedByte, readFull, ignore, and scan.go's findRST,
// each the same text in 1.26.6 (CI's and the Dockerfile's release) and 1.27.1; the
// packages differ elsewhere (1.27.1 decodes three-component files with non-standard
// subsampling — "flex" — that 1.26.6 refuses; processSOF, makeImg, convertToRGB,
// receiveExtend, and processSOS's MCU geometry). The loop reads a marker as two
// adjacent bytes of the stream — it realigns one byte at a time over bytes that are
// not FF, skips fill bytes one at a time, and takes a top-level RST or a stray FF 00 as
// a marker with no length — and it steps back at most the two bytes of a stuffed byte,
// while a scan header is at least eight bytes; so each scan it runs starts at an FF DA
// pair of its own. The bound rests on that reading, and on the decoder being given
// exactly these bytes (normalizeHeld). TestLogoScans_DishonestFilesRefused measures
// seven placements that four counters which skip or stop by marker bytes miss (its
// comment lists them). A pair inside an APPn segment or a thumbnail is counted too:
// that error refuses.
func logoScanCount(data []byte) int {
	return bytes.Count(data, []byte{0xFF, 0xDA})
}

func logoOutputSizeAllowed(n int) bool { return n <= LogoMaxOutputBytes }

// logoTargetSize scales the long edge down to LogoMaxOutputEdge, rounding the short
// edge to the nearest pixel, at least one; a logo that already fits keeps its size (the
// filter does not enlarge).
func logoTargetSize(w, h int) (int, int) {
	if w <= LogoMaxOutputEdge && h <= LogoMaxOutputEdge {
		return w, h
	}
	if w >= h {
		return LogoMaxOutputEdge, max(1, (h*LogoMaxOutputEdge+w/2)/w)
	}
	return max(1, (w*LogoMaxOutputEdge+h/2)/h), LogoMaxOutputEdge
}

// logoVerifyOutput is ADR 0024 §3's second decode: the bytes about to be stored pass
// the same format gate, decode with the same format's decoder and have the size the
// filter produced, within 1..LogoMaxOutputEdge.
func logoVerifyOutput(format logoFormat, out []byte, w, h int) error {
	if w < 1 || h < 1 || w > LogoMaxOutputEdge || h > LogoMaxOutputEdge {
		return &logoError{class: ErrLogoVerify}
	}
	if http.DetectContentType(out) != format.mime() {
		return &logoError{class: ErrLogoVerify}
	}
	cfg, err := format.decodeConfig(bytes.NewReader(out))
	if err != nil {
		return &logoError{class: ErrLogoVerify, cause: err}
	}
	if cfg.Width != w || cfg.Height != h {
		return &logoError{class: ErrLogoVerify}
	}
	if format == logoFormatJPEG && logoScanCount(out) > LogoJPEGScanCeiling {
		return &logoError{class: ErrLogoVerify}
	}
	img, err := format.decode(bytes.NewReader(out))
	if err != nil {
		return &logoError{class: ErrLogoVerify, cause: err}
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		return &logoError{class: ErrLogoVerify}
	}
	return nil
}
