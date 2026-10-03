package brand

// Fixture builders for the logo tests. The files the tests feed Normalize are built by
// code, here and with the standard encoders; the package has no testdata directory.
// The builders write the bytes by hand where the standard encoders do not: CMYK/YCCK
// and progressive JPEG, interlaced PNG, metadata segments and chunks, the scan bombs.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// ---------------------------------------------------------------- PNG

var imgPNGSignature = []byte("\x89PNG\r\n\x1a\n")

func imgPNGChunk(typ string, data []byte) []byte {
	var b bytes.Buffer
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	b.Write(n[:])
	b.WriteString(typ)
	b.Write(data)
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(data)
	binary.BigEndian.PutUint32(n[:], crc.Sum32())
	b.Write(n[:])
	return b.Bytes()
}

func imgPNGFile(chunks ...[]byte) []byte {
	out := append([]byte(nil), imgPNGSignature...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}

func imgPNGIHDR(w, h uint32, depth, colorType, interlace byte) []byte {
	d := make([]byte, 13)
	binary.BigEndian.PutUint32(d[0:], w)
	binary.BigEndian.PutUint32(d[4:], h)
	d[8], d[9], d[12] = depth, colorType, interlace
	return imgPNGChunk("IHDR", d)
}

func imgZlib(t testing.TB, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib: %v", err)
	}
	return b.Bytes()
}

// imgAdam7 is the PNG interlace pass table: x0, y0, dx, dy.
var imgAdam7 = [7][4]int{{0, 0, 8, 8}, {4, 0, 8, 8}, {0, 4, 4, 8}, {2, 0, 4, 4}, {0, 2, 2, 4}, {1, 0, 2, 2}, {0, 1, 1, 2}}

// imgPNGRaw is the filtered scanline stream (filter type 0 on every row) of a w×h image
// whose pixel (x, y) is pix(x, y); interlaced builds the seven Adam7 passes.
func imgPNGRaw(w, h int, interlaced bool, pix func(x, y int) []byte) []byte {
	var raw []byte
	if !interlaced {
		for y := 0; y < h; y++ {
			raw = append(raw, 0)
			for x := 0; x < w; x++ {
				raw = append(raw, pix(x, y)...)
			}
		}
		return raw
	}
	for _, p := range imgAdam7 {
		x0, y0, dx, dy := p[0], p[1], p[2], p[3]
		if w <= x0 || h <= y0 {
			continue
		}
		for y := y0; y < h; y += dy {
			raw = append(raw, 0)
			for x := x0; x < w; x += dx {
				raw = append(raw, pix(x, y)...)
			}
		}
	}
	return raw
}

// imgPNGForged is a PNG of depth 8 or 16 and colour type 0, 2, 4 or 6 built byte by
// byte, interlaced or not, with extra chunks inserted before IDAT and bytes after IEND.
func imgPNGForged(t testing.TB, w, h int, depth, colorType byte, interlaced bool,
	pix func(x, y int) []byte, beforeIDAT [][]byte, afterIEND []byte) []byte {
	t.Helper()
	il := byte(0)
	if interlaced {
		il = 1
	}
	chunks := [][]byte{imgPNGIHDR(uint32(w), uint32(h), depth, colorType, il)}
	chunks = append(chunks, beforeIDAT...)
	chunks = append(chunks, imgPNGChunk("IDAT", imgZlib(t, imgPNGRaw(w, h, interlaced, pix))))
	chunks = append(chunks, imgPNGChunk("IEND", nil))
	out := imgPNGFile(chunks...)
	return append(out, afterIEND...)
}

// imgPNGChunkStorm is a 64×64 paletted PNG with n empty tEXt chunks between PLTE and
// IDAT. image/png's DecodeConfig reads a paletted PNG's chunks up to IDAT, and each
// skipped chunk costs it a 4 KiB heap buffer: a storm makes the inspection itself
// allocate, before a pixel is decoded.
func imgPNGChunkStorm(t testing.TB, n int) []byte {
	t.Helper()
	out := imgPNGFile(imgPNGIHDR(64, 64, 8, 3, 0), imgPNGChunk("PLTE", []byte{10, 20, 30}))
	empty := imgPNGChunk("tEXt", []byte("a\x00"))
	for i := 0; i < n; i++ {
		out = append(out, empty...)
	}
	out = append(out, imgPNGChunk("IDAT", imgZlib(t, make([]byte, 64*65)))...)
	return append(out, imgPNGChunk("IEND", nil)...)
}

// imgPNGChunkTypes lists a PNG's chunk types in order; ok is false if the walk ran off
// the bytes before IEND.
func imgPNGChunkTypes(data []byte) (types []string, trailing int, ok bool) {
	if !bytes.HasPrefix(data, imgPNGSignature) {
		return nil, 0, false
	}
	i := len(imgPNGSignature)
	for i+8 <= len(data) {
		n := int(binary.BigEndian.Uint32(data[i:]))
		typ := string(data[i+4 : i+8])
		if n < 0 || i+12+n > len(data) {
			return types, 0, false
		}
		types = append(types, typ)
		i += 12 + n
		if typ == "IEND" {
			return types, len(data) - i, true
		}
	}
	return types, 0, false
}

func imgEncodePNG(t testing.TB, m image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return b.Bytes()
}

func imgEncodeJPEG(t testing.TB, m image.Image, q int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: q}); err != nil {
		t.Fatalf("jpeg encode: %v", err)
	}
	return b.Bytes()
}

// imgQuadrants is a w×h opaque image in four flat quadrants, so a colour check survives
// JPEG's blocks and the box filter away from the seams.
func imgQuadrants(w, h int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	q := [4]color.NRGBA{{200, 30, 40, 255}, {20, 160, 60, 255}, {30, 60, 190, 255}, {235, 235, 235, 255}}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			k := 0
			if x >= w/2 {
				k++
			}
			if y >= h/2 {
				k += 2
			}
			m.SetNRGBA(x, y, q[k])
		}
	}
	return m
}

// imgNoise is a w×h opaque image of deterministic pseudo-random pixels (xorshift), the
// worst case for both encoders.
func imgNoise(w, h int, seed uint32) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	s := seed | 1
	for i := 0; i < len(m.Pix); i += 4 {
		s ^= s << 13
		s ^= s >> 17
		s ^= s << 5
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = byte(s), byte(s>>8), byte(s>>16), 255
	}
	return m
}

// ---------------------------------------------------------------- JPEG

// imgJPEGSegment is one marker segment with its length field.
func imgJPEGSegment(marker byte, payload []byte) []byte {
	out := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
	return append(out, payload...)
}

// imgJPEGInsertAfterSOI puts segments right after a JPEG's SOI.
func imgJPEGInsertAfterSOI(data []byte, segs ...[]byte) []byte {
	out := append([]byte(nil), data[:2]...)
	for _, s := range segs {
		out = append(out, s...)
	}
	return append(out, data[2:]...)
}

// imgExifGPS is an APP1 "Exif" segment whose TIFF body has an IFD0 pointing at a GPS
// IFD with latitude and longitude (synthetic values: 1°2'3" N, 4°5'6" E).
func imgExifGPS() []byte {
	be := binary.BigEndian
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	// IFD0 at 8: one entry, GPSInfo (0x8825) LONG 1 -> GPS IFD at 26.
	ifd0 := make([]byte, 2+12+4)
	be.PutUint16(ifd0[0:], 1)
	be.PutUint16(ifd0[2:], 0x8825)
	be.PutUint16(ifd0[4:], 4)
	be.PutUint32(ifd0[6:], 1)
	be.PutUint32(ifd0[10:], 26)
	tiff = append(tiff, ifd0...)
	// GPS IFD at 26: four entries, values after it at 26+2+48+4 = 80.
	gps := make([]byte, 2+4*12+4)
	be.PutUint16(gps[0:], 4)
	entry := func(i int, tag, typ uint16, count, value uint32) {
		o := 2 + 12*i
		be.PutUint16(gps[o:], tag)
		be.PutUint16(gps[o+2:], typ)
		be.PutUint32(gps[o+4:], count)
		be.PutUint32(gps[o+8:], value)
	}
	entry(0, 1, 2, 2, uint32('N')<<24)
	entry(1, 2, 5, 3, 80)
	entry(2, 3, 2, 2, uint32('E')<<24)
	entry(3, 4, 5, 3, 104)
	tiff = append(tiff, gps...)
	rat := func(vals ...uint32) []byte {
		out := make([]byte, 8*len(vals))
		for i, v := range vals {
			be.PutUint32(out[8*i:], v)
			be.PutUint32(out[8*i+4:], 1)
		}
		return out
	}
	tiff = append(tiff, rat(1, 2, 3)...)
	tiff = append(tiff, rat(4, 5, 6)...)
	return imgJPEGSegment(0xE1, append([]byte("Exif\x00\x00"), tiff...))
}

// imgJPEGComp is one frame component; h and v are its sampling factors.
type imgJPEGComp struct {
	id   byte
	h, v int
}

// imgJPEGForge describes a JPEG this file writes byte by byte. Every block is flat: its
// DC coefficient carries the level and its AC coefficients are zero, ended by EOB or an
// end-of-band run. The quantization table is all ones, so a block whose sample level is
// p decodes to p exactly.
type imgJPEGForge struct {
	w, h  int
	comps []imgJPEGComp
	jfif  bool
	adobe int // -1: no APP14; otherwise its colour transform byte
	// level is component c's sample at block (bx, by) of that component's grid, 0..255.
	level func(c, bx, by int) int

	progressive bool
	// dcScans is how many times the progressive DC scan is written (at least one).
	dcScans int
	// acScans is the number of first AC scans (Ss=1, Se=63) after the DC scans, taken
	// over the components in turn; zero means one per component.
	acScans int
	// refineScans is the number of AC refinement scans (Ah=1, Al=0) after them.
	refineScans int
	// seqScans is how many times a sequential frame's scan is written (at least one).
	seqScans int
	// eobRun is the longest end-of-band run written: 1 writes an EOB per block (what an
	// encoder writes); 32767 is the longest the format can say.
	eobRun int
	// hide places bytes the decoder handles one way and a counter that skips by length
	// fields another. Before the DC scan's marker, which the decoder resyncs over and a
	// segment-length walker does not: "rst" (FF D0), "ff00" (FF 00), "fill" (FF FF),
	// "junk" (00 11 22). "app1-sos": an APP1 segment whose payload is FF DA FF FF — the
	// decoder skips the payload by APP1's length; a counter that finds FF DA and then
	// skips "its header" by the next two bytes jumps 65 535 bytes. "dqt-app": the last
	// four DQT values are FF E1 FF F0 — table values to the decoder; to a counter that
	// skips an APPn payload wherever it sees FF Ex, an APP1 of 65 520 bytes. "app1-eoi":
	// an APP1 whose payload is FF D9 — the decoder skips it; a counter that stops at the
	// first FF D9 (an Exif thumbnail's EOI sits there in real files) stops before the
	// scans.
	hide string
}

// imgHuff is a canonical Huffman code table built from the DHT counts and values.
type imgHuff struct {
	bits [16]byte
	vals []byte
	code map[byte][2]uint32 // value -> code, length
}

func imgNewHuff(bits [16]byte, vals []byte) imgHuff {
	h := imgHuff{bits: bits, vals: vals, code: map[byte][2]uint32{}}
	code, k := uint32(0), 0
	for l := 1; l <= 16; l++ {
		for i := 0; i < int(bits[l-1]); i++ {
			h.code[vals[k]] = [2]uint32{code, uint32(l)}
			code++
			k++
		}
		code <<= 1
	}
	return h
}

func (h imgHuff) dht(class, id byte) []byte {
	p := []byte{class<<4 | id}
	p = append(p, h.bits[:]...)
	return append(p, h.vals...)
}

// The DC table gives category 0 a one-bit code (one bit per flat block in a sequential
// scan, the cheapest a block can be); the AC table holds EOB and the fourteen
// end-of-band run symbols, four bits each.
var (
	imgDCHuff = imgNewHuff([16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	imgACHuff = imgNewHuff([16]byte{0, 0, 0, 15},
		[]byte{0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0, 0xB0, 0xC0, 0xD0, 0xE0})
)

// imgBitWriter writes entropy-coded bytes with FF00 stuffing.
type imgBitWriter struct {
	out  []byte
	acc  uint32
	nacc uint32
}

func (b *imgBitWriter) put(v, n uint32) {
	for i := int(n) - 1; i >= 0; i-- {
		b.acc = b.acc<<1 | (v>>uint(i))&1
		b.nacc++
		if b.nacc == 8 {
			b.byteOut(byte(b.acc))
			b.acc, b.nacc = 0, 0
		}
	}
}

func (b *imgBitWriter) byteOut(c byte) {
	b.out = append(b.out, c)
	if c == 0xFF {
		b.out = append(b.out, 0x00)
	}
}

func (b *imgBitWriter) flush() []byte {
	for b.nacc != 0 {
		b.put(1, 1)
	}
	return b.out
}

func (b *imgBitWriter) huff(h imgHuff, sym byte) {
	c, ok := h.code[sym]
	if !ok {
		panic("imgBitWriter: symbol not in table")
	}
	b.put(c[0], c[1])
}

func (b *imgBitWriter) dc(diff int) {
	cat, mag := 0, diff
	if mag < 0 {
		mag = -mag
	}
	for mag > 0 {
		cat++
		mag >>= 1
	}
	b.huff(imgDCHuff, byte(cat))
	if cat > 0 {
		v := diff
		if v < 0 {
			v += 1<<cat - 1
		}
		b.put(uint32(v), uint32(cat))
	}
}

// eob writes an end-of-band run of n blocks (1..32767).
func (b *imgBitWriter) eob(n int) {
	r := 0
	for 1<<(r+1) <= n {
		r++
	}
	b.huff(imgACHuff, byte(r<<4))
	if r > 0 {
		b.put(uint32(n-1<<r), uint32(r))
	}
}

func (f imgJPEGForge) maxHV() (int, int) {
	mh, mv := 1, 1
	for _, c := range f.comps {
		mh, mv = max(mh, c.h), max(mv, c.v)
	}
	return mh, mv
}

// blocks lists the blocks a scan over the given components visits, in the order
// image/jpeg's decoder visits them (scan.go): MCU order for an interleaved scan; for a
// one-component scan, raster order over that component's grid, skipping blocks that
// start outside the image. Each entry is component index, bx, by.
func (f imgJPEGForge) blocks(scan []int) [][3]int {
	mh, mv := f.maxHV()
	mxx := (f.w + 8*mh - 1) / (8 * mh)
	myy := (f.h + 8*mv - 1) / (8 * mv)
	var out [][3]int
	if len(f.comps) == 1 || len(scan) == 1 {
		c := scan[0]
		hi, vi := f.comps[c].h, f.comps[c].v
		if len(f.comps) == 1 {
			hi, vi = 1, 1
		}
		q := mxx * hi
		for n := 0; n < mxx*myy*hi*vi; n++ {
			bx, by := n%q, n/q
			if bx*8 >= f.w || by*8 >= f.h {
				continue
			}
			out = append(out, [3]int{c, bx, by})
		}
		return out
	}
	for my := 0; my < myy; my++ {
		for mx := 0; mx < mxx; mx++ {
			for _, c := range scan {
				hi, vi := f.comps[c].h, f.comps[c].v
				for j := 0; j < hi*vi; j++ {
					out = append(out, [3]int{c, hi*mx + j%hi, vi*my + j/hi})
				}
			}
		}
	}
	return out
}

func (f imgJPEGForge) sos(scan []int, ss, se, ah, al byte, data []byte) []byte {
	p := []byte{byte(len(scan))}
	for _, c := range scan {
		p = append(p, f.comps[c].id, 0x00)
	}
	p = append(p, ss, se, ah<<4|al)
	return append(imgJPEGSegment(0xDA, p), data...)
}

// dcScan codes every block's DC level; in a sequential scan each block is followed by
// its AC part, here an EOB or a share of an end-of-band run.
func (f imgJPEGForge) dcScan(scan []int, withAC bool) []byte {
	var bw imgBitWriter
	pred := map[int]int{}
	blocks := f.blocks(scan)
	run := 0
	for i, b := range blocks {
		dc := 8 * (f.level(b[0], b[1], b[2]) - 128)
		bw.dc(dc - pred[b[0]])
		pred[b[0]] = dc
		if !withAC {
			continue
		}
		if run > 0 {
			run--
			continue
		}
		n := min(len(blocks)-i, max(1, f.eobRun))
		bw.eob(n)
		run = n - 1
	}
	return bw.flush()
}

// eobScan codes a whole AC scan over one component as end-of-band runs.
func (f imgJPEGForge) eobScan(c int) []byte {
	var bw imgBitWriter
	left := len(f.blocks([]int{c}))
	for left > 0 {
		n := min(left, max(1, f.eobRun))
		bw.eob(n)
		left -= n
	}
	return bw.flush()
}

// bytes writes the file.
func (f imgJPEGForge) bytes() []byte {
	out := []byte{0xFF, 0xD8}
	if f.jfif {
		out = append(out, imgJPEGSegment(0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"))...)
	}
	if f.adobe >= 0 {
		out = append(out, imgJPEGSegment(0xEE, []byte{'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, byte(f.adobe)})...)
	}
	dqt := append([]byte{0x00}, bytes.Repeat([]byte{1}, 64)...)
	if f.hide == "dqt-app" {
		// The four highest-frequency entries: the blocks are flat, so these
		// coefficients are zero and the values change no pixel.
		copy(dqt[1+60:], []byte{0xFF, 0xE1, 0xFF, 0xF0})
	}
	out = append(out, imgJPEGSegment(0xDB, dqt)...)
	sof := []byte{8, byte(f.h >> 8), byte(f.h), byte(f.w >> 8), byte(f.w), byte(len(f.comps))}
	for _, c := range f.comps {
		sof = append(sof, c.id, byte(c.h<<4|c.v), 0)
	}
	marker := byte(0xC0)
	if f.progressive {
		marker = 0xC2
	}
	out = append(out, imgJPEGSegment(marker, sof)...)
	out = append(out, imgJPEGSegment(0xC4, append(imgDCHuff.dht(0, 0), imgACHuff.dht(1, 0)...))...)

	switch f.hide {
	case "":
	case "rst":
		out = append(out, 0xFF, 0xD0)
	case "ff00":
		out = append(out, 0xFF, 0x00)
	case "fill":
		out = append(out, 0xFF, 0xFF)
	case "junk":
		out = append(out, 0x00, 0x11, 0x22)
	case "app1-sos":
		out = append(out, imgJPEGSegment(0xE1, []byte{0xFF, 0xDA, 0xFF, 0xFF})...)
	case "app1-eoi":
		out = append(out, imgJPEGSegment(0xE1, []byte{0xFF, 0xD9})...)
	case "dqt-app":
	default:
		panic("imgJPEGForge: unknown hide " + f.hide)
	}

	all := make([]int, len(f.comps))
	for i := range all {
		all[i] = i
	}
	if !f.progressive {
		for i := 0; i < max(1, f.seqScans); i++ {
			out = append(out, f.sos(all, 0, 63, 0, 0, f.dcScan(all, true))...)
		}
		return append(out, 0xFF, 0xD9)
	}
	for i := 0; i < max(1, f.dcScans); i++ {
		out = append(out, f.sos(all, 0, 0, 0, 0, f.dcScan(all, false))...)
	}
	ac := f.acScans
	if ac == 0 {
		ac = len(f.comps)
	}
	for i := 0; i < ac; i++ {
		c := i % len(f.comps)
		out = append(out, f.sos([]int{c}, 1, 63, 0, 0, f.eobScan(c))...)
	}
	for i := 0; i < f.refineScans; i++ {
		c := i % len(f.comps)
		out = append(out, f.sos([]int{c}, 1, 63, 1, 0, f.eobScan(c))...)
	}
	return append(out, 0xFF, 0xD9)
}

// imgGrayForge is a w×h one-component JPEG whose level is level(bx, by).
func imgGrayForge(w, h int, level func(bx, by int) int) imgJPEGForge {
	return imgJPEGForge{
		w: w, h: h, comps: []imgJPEGComp{{1, 1, 1}}, jfif: true, adobe: -1,
		level: func(_, bx, by int) int { return level(bx, by) }, eobRun: 1,
	}
}

// imgCMYKForge is a w×h 4:4:4 four-component JPEG with an Adobe APP14 segment whose
// transform byte is transform (0: CMYK, 2: YCCK); levels are the stored samples.
func imgCMYKForge(w, h int, transform int, levels [4]int) imgJPEGForge {
	return imgJPEGForge{
		w: w, h: h, adobe: transform, eobRun: 1,
		comps: []imgJPEGComp{{1, 1, 1}, {2, 1, 1}, {3, 1, 1}, {4, 1, 1}},
		level: func(c, _, _ int) int { return levels[c] },
	}
}

// imgJPEGWalk follows a JPEG by its segment lengths: the walker a scan counter could be
// written as, and the reader of the encoder's own (honest) output. It returns the
// markers it passed in order and stops at the first byte where a marker should be and
// is not, or at EOI.
func imgJPEGWalk(data []byte) (markers []byte, clean bool) {
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil, false
	}
	markers = append(markers, 0xD8)
	i := 2
	for i+2 <= len(data) {
		if data[i] != 0xFF {
			return markers, false
		}
		m := data[i+1]
		markers = append(markers, m)
		if m == 0xD9 {
			return markers, i+2 == len(data)
		}
		if i+4 > len(data) {
			return markers, false
		}
		i += 2 + int(binary.BigEndian.Uint16(data[i+2:]))
		if m != 0xDA {
			continue
		}
		for i+1 < len(data) && (data[i] != 0xFF || data[i+1] == 0x00 || (data[i+1] >= 0xD0 && data[i+1] <= 0xD7)) {
			i++
		}
	}
	return markers, false
}

// imgWalkerScanCount is the scan count of a counter that follows segment lengths.
func imgWalkerScanCount(data []byte) int {
	markers, _ := imgJPEGWalk(data)
	return bytes.Count(markers, []byte{0xDA})
}

// imgSkipHeaderScanCount finds FF DA in the raw bytes, counts it, and skips the scan
// header by the length field after it and the entropy data after that — a counter
// that trusts the two bytes behind each FF DA it finds.
func imgSkipHeaderScanCount(data []byte) int {
	n, i := 0, 0
	for i+1 < len(data) {
		if data[i] != 0xFF || data[i+1] != 0xDA {
			i++
			continue
		}
		n++
		if i+4 > len(data) {
			break
		}
		i += 2 + int(binary.BigEndian.Uint16(data[i+2:]))
		for i+1 < len(data) && (data[i] != 0xFF || data[i+1] == 0x00 || (data[i+1] >= 0xD0 && data[i+1] <= 0xD7)) {
			i++
		}
	}
	return n
}

// imgStopAtEOIScanCount counts FF DA in the raw bytes up to the first FF D9 — a counter
// that takes the first EOI it meets for the end of the image.
func imgStopAtEOIScanCount(data []byte) int {
	if i := bytes.Index(data, []byte{0xFF, 0xD9}); i >= 0 {
		data = data[:i]
	}
	return bytes.Count(data, []byte{0xFF, 0xDA})
}

// imgSkipAppScanCount counts FF DA in the raw bytes but skips an APPn or COM payload by
// its length wherever it sees FF E0..FF EF or FF FE — a counter that leaves metadata
// out of the count.
func imgSkipAppScanCount(data []byte) int {
	n, i := 0, 0
	for i+1 < len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		switch m := data[i+1]; {
		case m == 0xDA:
			n++
			i += 2
		case (m >= 0xE0 && m <= 0xEF) || m == 0xFE:
			if i+4 > len(data) {
				return n
			}
			i += 2 + int(binary.BigEndian.Uint16(data[i+2:]))
		default:
			i++
		}
	}
	return n
}
