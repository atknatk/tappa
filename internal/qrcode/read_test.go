package qrcode_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/qrcode"
)

// read_test.go -- A DECODER, to read back what Encode drew. It is in the external test
// package so it can reach only the exported API, and it shares no code and no table with
// the encoder: its block table is ISO/IEC 18004 Table 9 typed row by row for versions
// 1-10 (the encoder keeps two per-block columns and derives the rest), its alignment
// centres are Table E.1 (the encoder computes them), its Galois field arithmetic is
// log/antilog tables (the encoder multiplies by shift-and-add), its error check is the
// syndromes (the encoder divides), and its format and version checks divide by the BCH
// generators (the encoder builds the codewords).
//
// The decoder is anchored first: it reads libqrencode's own matrices (testdata/) back to
// their input bytes (TestRead_ReadsTheReferenceMatrices) -- a decoder that misreads the
// standard the way the encoder might is caught there, not trusted. Versions above 10 are
// outside its table; the sweep's hashes hold the encoder there (qrcode_test.go).

// table9 is, per version 1-10 and level L M Q H: the error correction codewords per block,
// then (count, data codewords) for each of the one or two block groups.
var table9 = map[int][4][]int{
	1:  {{7, 1, 19}, {10, 1, 16}, {13, 1, 13}, {17, 1, 9}},
	2:  {{10, 1, 34}, {16, 1, 28}, {22, 1, 22}, {28, 1, 16}},
	3:  {{15, 1, 55}, {26, 1, 44}, {18, 2, 17}, {22, 2, 13}},
	4:  {{20, 1, 80}, {18, 2, 32}, {26, 2, 24}, {16, 4, 9}},
	5:  {{26, 1, 108}, {24, 2, 43}, {18, 2, 15, 2, 16}, {22, 2, 11, 2, 12}},
	6:  {{18, 2, 68}, {16, 4, 27}, {24, 4, 19}, {28, 4, 15}},
	7:  {{20, 2, 78}, {18, 4, 31}, {18, 2, 14, 4, 15}, {26, 4, 13, 1, 14}},
	8:  {{24, 2, 97}, {22, 2, 38, 2, 39}, {22, 4, 18, 2, 19}, {26, 4, 14, 2, 15}},
	9:  {{30, 2, 116}, {22, 3, 36, 2, 37}, {20, 4, 16, 4, 17}, {24, 4, 12, 4, 13}},
	10: {{18, 2, 68, 2, 69}, {26, 4, 43, 1, 44}, {24, 6, 19, 2, 20}, {28, 6, 15, 2, 16}},
}

// totalCodewords is Table 9's total per version, read against the module count.
var totalCodewords = map[int]int{1: 26, 2: 44, 3: 70, 4: 100, 5: 134, 6: 172, 7: 196, 8: 242, 9: 292, 10: 346}

// tableE1 is the alignment pattern centres for versions 1-10.
var tableE1 = map[int][]int{
	1: nil, 2: {6, 18}, 3: {6, 22}, 4: {6, 26}, 5: {6, 30}, 6: {6, 34},
	7: {6, 22, 38}, 8: {6, 24, 42}, 9: {6, 26, 46}, 10: {6, 28, 50},
}

// GF(2^8) mod x^8+x^4+x^3+x^2+1 by tables.
var gfExp, gfLog = func() ([512]byte, [256]int) {
	var exp [512]byte
	var log [256]int
	x := 1
	for i := range 255 {
		exp[i] = byte(x)
		log[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		exp[i] = exp[i-255]
	}
	return exp, log
}()

func gfTimes(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

// gf2Mod is v mod g over GF(2)[x].
func gf2Mod(v, g int) int {
	deg := func(p int) int {
		d := -1
		for ; p > 0; p >>= 1 {
			d++
		}
		return d
	}
	for dg := deg(g); deg(v) >= dg; {
		v ^= g << (deg(v) - dg)
	}
	return v
}

// decoded is what read recovers.
type decoded struct {
	version, mask int
	level         string
	data          []byte
}

// read decodes a symbol given without its quiet zone, m[y][x], dark true.
func read(m [][]bool) (decoded, error) {
	var d decoded
	size := len(m)
	if size < 21 || (size-17)%4 != 0 {
		return d, fmt.Errorf("a %d-module side is no version's", size)
	}
	for _, row := range m {
		if len(row) != size {
			return d, errors.New("the matrix is not square")
		}
	}
	d.version = (size - 17) / 4
	if _, ok := table9[d.version]; !ok {
		return d, fmt.Errorf("version %d is outside this decoder's table", d.version)
	}
	at := func(x, y int) bool { return m[y][x] }

	// THE FORMAT INFORMATION, both copies, bit 14 first.
	var f1, f2 int
	for i := range 15 {
		var a, b [2]int // (x, y) of bit 14-i in copy 1 and copy 2
		switch {
		case i <= 5: // bits 14-9: row 8, columns 0-5
			a = [2]int{i, 8}
		case i == 6: // bit 8
			a = [2]int{7, 8}
		case i == 7: // bit 7
			a = [2]int{8, 8}
		case i == 8: // bit 6
			a = [2]int{8, 7}
		default: // bits 5-0: column 8, rows 5-0
			a = [2]int{8, 14 - i}
		}
		if i <= 6 { // bits 14-8: column 8, rows size-1 up to size-7
			b = [2]int{8, size - 1 - i}
		} else { // bits 7-0: row 8, columns size-8 to size-1
			b = [2]int{size - 15 + i, 8}
		}
		f1 = f1<<1 | b2i(at(a[0], a[1]))
		f2 = f2<<1 | b2i(at(b[0], b[1]))
	}
	if f1 != f2 {
		return d, fmt.Errorf("the two format information copies differ: %015b, %015b", f1, f2)
	}
	if gf2Mod(f1^0b101010000010010, 0b10100110111) != 0 {
		return d, fmt.Errorf("the format information %015b is not a BCH(15,5) codeword", f1)
	}
	info := (f1 ^ 0b101010000010010) >> 10
	d.level = map[int]string{0b01: "L", 0b00: "M", 0b11: "Q", 0b10: "H"}[info>>3]
	d.mask = info & 7
	if !at(8, size-8) {
		return d, errors.New("the dark module is light")
	}

	// THE VERSION INFORMATION, from version 7: both blocks, bit 17 first.
	if d.version >= 7 {
		var v1, v2 int
		for i := 17; i >= 0; i-- {
			v1 = v1<<1 | b2i(at(i/3, size-11+i%3)) // bottom-left block
			v2 = v2<<1 | b2i(at(size-11+i%3, i/3)) // top-right block
		}
		if v1 != v2 || gf2Mod(v1, 0b1111100100101) != 0 || v1>>12 != d.version {
			return d, fmt.Errorf("the version information %018b / %018b is not version %d's BCH(18,6) codeword", v1, v2, d.version)
		}
	}

	// THE FUNCTION PATTERNS: mark them, and check the fixed ones' modules.
	fn := make([][]bool, size)
	for y := range fn {
		fn[y] = make([]bool, size)
	}
	mark := func(x0, y0, w, h int) {
		for y := y0; y < y0+h; y++ {
			for x := x0; x < x0+w; x++ {
				fn[y][x] = true
			}
		}
	}
	for _, c := range [][2]int{{0, 0}, {size - 7, 0}, {0, size - 7}} {
		for dy := range 7 {
			for dx := range 7 {
				ring := min(dx, dy, 6-dx, 6-dy) // 0 outer dark, 1 light, 2-3 the dark centre
				if at(c[0]+dx, c[1]+dy) != (ring != 1) {
					return d, fmt.Errorf("the finder pattern at (%d, %d) is broken at (%d, %d)", c[0], c[1], dx, dy)
				}
			}
		}
	}
	mark(0, 0, 8, 8)
	mark(size-8, 0, 8, 8)
	mark(0, size-8, 8, 8)
	for k := range 8 { // the separators: a light row and a light column inside each 8x8 corner
		for _, p := range [][2]int{
			{k, 7}, {7, k}, // top-left
			{size - 1 - k, 7}, {size - 8, k}, // top-right
			{k, size - 8}, {7, size - 1 - k}, // bottom-left
		} {
			if at(p[0], p[1]) {
				return d, fmt.Errorf("the separator module (%d, %d) is dark", p[0], p[1])
			}
		}
	}
	for i := 8; i < size-8; i++ {
		if at(i, 6) != (i%2 == 0) || at(6, i) != (i%2 == 0) {
			return d, fmt.Errorf("the timing pattern is broken at %d", i)
		}
	}
	mark(0, 6, size, 1)
	mark(6, 0, 1, size)
	pos := tableE1[d.version]
	for i, cy := range pos {
		for j, cx := range pos {
			if (i == 0 && j == 0) || (i == 0 && j == len(pos)-1) || (i == len(pos)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					ring := max(abs(dx), abs(dy))
					if at(cx+dx, cy+dy) != (ring != 1) {
						return d, fmt.Errorf("the alignment pattern at (%d, %d) is broken", cx, cy)
					}
				}
			}
			mark(cx-2, cy-2, 5, 5)
		}
	}
	mark(0, 8, 9, 1)
	mark(8, 0, 1, 9)
	mark(size-8, 8, 8, 1)
	mark(8, size-8, 1, 8)
	if d.version >= 7 {
		mark(0, size-11, 6, 3)
		mark(size-11, 0, 3, 6)
	}

	// THE CODEWORDS: the data modules in placement order, unmasked.
	cond := maskConditions[d.mask]
	var bits []int
	pair := 0 // two-module columns from the right; the first goes up, then they alternate
	for right := size - 1; right > 0; right -= 2 {
		if right == 6 {
			right-- // the vertical timing column is no pair's
		}
		up := pair%2 == 0
		pair++
		for k := range size {
			y := k
			if up {
				y = size - 1 - k
			}
			for _, x := range []int{right, right - 1} {
				if fn[y][x] {
					continue
				}
				bit := b2i(at(x, y))
				if cond(y, x) {
					bit ^= 1
				}
				bits = append(bits, bit)
			}
		}
	}
	total := totalCodewords[d.version]
	if rem := len(bits) - total*8; len(bits)/8 != total || (rem != 0 && rem != 3 && rem != 4 && rem != 7) {
		return d, fmt.Errorf("%d data modules for %d codewords", len(bits), total)
	}
	cw := make([]byte, total)
	for i := range cw {
		for _, b := range bits[i*8 : i*8+8] {
			cw[i] = cw[i]<<1 | byte(b)
		}
	}

	// THE BLOCKS: de-interleave, check each block's syndromes.
	row := table9[d.version][strings.Index("LMQH", d.level)]
	ecc := row[0]
	var lens []int
	for g := 1; g+1 < len(row); g += 2 {
		for range row[g] {
			lens = append(lens, row[g+1])
		}
	}
	sum := 0
	for _, n := range lens {
		sum += n + ecc
	}
	if sum != total {
		return d, fmt.Errorf("Table 9's row for version %d level %s sums to %d codewords, not %d", d.version, d.level, sum, total)
	}
	blocks := make([][]byte, len(lens))
	k := 0
	for col := 0; col < lens[len(lens)-1]; col++ {
		for b, n := range lens {
			if col < n {
				blocks[b] = append(blocks[b], cw[k])
				k++
			}
		}
	}
	for range ecc {
		for b := range blocks {
			blocks[b] = append(blocks[b], cw[k])
			k++
		}
	}
	var data []byte
	for b, blk := range blocks {
		for j := range ecc {
			s := byte(0)
			for _, c := range blk {
				s = gfTimes(s, gfExp[j]) ^ c
			}
			if s != 0 {
				return d, fmt.Errorf("block %d: syndrome %d is %d", b, j, s)
			}
		}
		data = append(data, blk[:lens[b]]...)
	}

	// THE BIT STREAM: byte mode, the count, the bytes, the terminator and the padding.
	r := bitReader{b: data}
	if mode := r.take(4); mode != 0b0100 {
		return d, fmt.Errorf("mode indicator %04b is not byte mode", mode)
	}
	count := r.take(map[bool]int{true: 8, false: 16}[d.version <= 9])
	if r.left() < count*8 {
		return d, fmt.Errorf("a count of %d bytes runs past the data", count)
	}
	for range count {
		d.data = append(d.data, byte(r.take(8)))
	}
	if r.take(min(4, r.left())) != 0 {
		return d, errors.New("the terminator is not zero")
	}
	if r.take(r.left()%8) != 0 {
		return d, errors.New("the bits to the byte boundary are not zero")
	}
	for i := 0; r.left() > 0; i++ {
		if pad := r.take(8); pad != [2]int{0xEC, 0x11}[i%2] {
			return d, fmt.Errorf("pad codeword %d is %02X", i, pad)
		}
	}
	return d, nil
}

// maskConditions are Table 10's, in its own variables: i the row, j the column.
var maskConditions = [8]func(i, j int) bool{
	func(i, j int) bool { return (i+j)%2 == 0 },
	func(i, j int) bool { return i%2 == 0 },
	func(i, j int) bool { return j%3 == 0 },
	func(i, j int) bool { return (i+j)%3 == 0 },
	func(i, j int) bool { return (i/2+j/3)%2 == 0 },
	func(i, j int) bool { return (i*j)%2+(i*j)%3 == 0 },
	func(i, j int) bool { return ((i*j)%2+(i*j)%3)%2 == 0 },
	func(i, j int) bool { return ((i*j)%3+(i+j)%2)%2 == 0 },
}

type bitReader struct {
	b   []byte
	pos int
}

func (r *bitReader) left() int { return len(r.b)*8 - r.pos }

func (r *bitReader) take(n int) int {
	v := 0
	for range n {
		v = v<<1 | int(r.b[r.pos/8]>>(7-r.pos%8)&1)
		r.pos++
	}
	return v
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// matrixOf is a Code's symbol without the quiet zone, through the exported API.
func matrixOf(c *qrcode.Code) [][]bool {
	b := c.Bitmap(0)
	return b
}

func refMatrix(t *testing.T, name string) [][]bool {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
	m := make([][]bool, len(lines))
	for y, line := range lines {
		m[y] = make([]bool, len(line)/2)
		for x := range m[y] {
			m[y][x] = line[2*x] == '#'
		}
	}
	return m
}

// TestRead_ReadsTheReferenceMatrices anchors the decoder: each of libqrencode's six
// named matrices reads back to its input file's bytes, at the level the file is named for.
func TestRead_ReadsTheReferenceMatrices(t *testing.T) {
	for _, v := range []struct{ in, ref, level string }{
		{"short.in", "short-L.txt", "L"}, {"short.in", "short-M.txt", "M"}, {"short.in", "short-Q.txt", "Q"},
		{"short.in", "short-H.txt", "H"}, {"v7.in", "v7-M.txt", "M"}, {"otpauth.in", "otpauth-M.txt", "M"},
	} {
		want, err := os.ReadFile(filepath.Join("testdata", v.in))
		if err != nil {
			t.Fatal(err)
		}
		got, err := read(refMatrix(t, v.ref))
		if err != nil {
			t.Errorf("%s: %v", v.ref, err)
			continue
		}
		if !bytes.Equal(got.data, want) || got.level != v.level {
			t.Errorf("%s: read %q at level %s, want %q at %s", v.ref, got.data, got.level, want, v.level)
		}
	}
}

// TestRead_ReadsWhatEncodeWrote: for every version 1-10 at every level, with inputs of
// several lengths up to the version's capacity -- the lengths the capacity search below
// finds, through Encode alone -- and bytes of every value, the decoder reads Encode's
// symbol back to the input, at Encode's version, level and mask, and finds every check
// (format and version BCH, function patterns, syndromes, padding) satisfied.
func TestRead_ReadsWhatEncodeWrote(t *testing.T) {
	names := map[qrcode.Level]string{qrcode.L: "L", qrcode.M: "M", qrcode.Q: "Q", qrcode.H: "H"}
	read1 := 0
	for _, l := range []qrcode.Level{qrcode.L, qrcode.M, qrcode.Q, qrcode.H} {
		// The longest input of each version: the largest n Encode puts in it.
		longest := map[int]int{}
		for n := 0; ; n++ {
			c, err := qrcode.Encode(make([]byte, n), l)
			if err != nil || c.Version() > 10 {
				break
			}
			longest[c.Version()] = n
		}
		for v := 1; v <= 10; v++ {
			top, ok := longest[v]
			if !ok {
				t.Fatalf("level %s: no input length lands in version %d", names[l], v)
			}
			for _, n := range []int{top, top - 1, top / 2} {
				data := make([]byte, n)
				for i := range data {
					data[i] = byte(i*97 + v*13 + int(l)*7)
				}
				c, err := qrcode.Encode(data, l)
				if err != nil {
					t.Fatal(err)
				}
				got, err := read(matrixOf(c))
				if err != nil {
					t.Errorf("version %d level %s, %d bytes: %v", c.Version(), names[l], n, err)
					continue
				}
				if !bytes.Equal(got.data, data) || got.version != c.Version() || got.level != names[l] || got.mask != c.Mask() {
					t.Errorf("version %d level %s mask %d, %d bytes: read version %d level %s mask %d, %d bytes (equal: %v)",
						c.Version(), names[l], c.Mask(), n, got.version, got.level, got.mask, len(got.data), bytes.Equal(got.data, data))
				}
				read1++
			}
		}
	}
	if read1 < 100 {
		t.Fatalf("PREMISE: %d symbols read", read1)
	}
}

// TestRead_RefusesABrokenSymbol is the decoder's own control: one flipped module in the
// format information, the version information, a finder, the timing pattern, or a data
// codeword with no error correction run -- and the decoder refuses. A decoder that
// accepted anything would pass the two tests above.
func TestRead_RefusesABrokenSymbol(t *testing.T) {
	c, err := qrcode.Encode(bytes.Repeat([]byte("FAKE"), 30), qrcode.M) // version 7
	if err != nil || c.Version() != 7 {
		t.Fatalf("PREMISE: %v, version %d", err, c.Version())
	}
	size := c.Size()
	for _, flip := range []struct {
		name, want string // want: the check that must refuse it
		x, y       int
	}{
		{"format information, first copy", "format information copies differ", 8, 2},
		{"format information, second copy", "format information copies differ", size - 3, 8},
		{"version information", "version information", 2, size - 10},
		{"finder pattern", "finder pattern", 3, 3},
		{"timing pattern", "timing pattern", 10, 6},
		{"a data module", "syndrome", size - 1, size - 1},
	} {
		m := matrixOf(c)
		m[flip.y][flip.x] = !m[flip.y][flip.x]
		if _, err := read(m); err == nil || !strings.Contains(err.Error(), flip.want) {
			t.Errorf("%s flipped at (%d, %d): the decoder answered %v, want a refusal naming %q", flip.name, flip.x, flip.y, err, flip.want)
		}
	}
	if _, err := read(matrixOf(c)); err != nil {
		t.Fatalf("CONTROL: the unbroken symbol: %v", err)
	}
}
