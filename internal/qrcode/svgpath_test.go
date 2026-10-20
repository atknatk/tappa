package qrcode_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/atknatk/tappa/internal/qrcode"
	"github.com/atknatk/tappa/internal/sun"
)

// svgpath_test.go -- SVGPath READ BACK. The path is parsed into modules with a grammar of
// its own (below), the modules are compared with Bitmap and decoded with read_test.go's
// decoder, which shares no code with the encoder. A path that drew a module twice, left a
// gap in a run, shifted by the wrong border or wrote a token the grammar does not know is
// refused here, not trusted.

// svgRun is the one sub-path SVGPath writes: a closed rectangle n modules wide, one high.
var svgRun = regexp.MustCompile(`M([0-9]+) ([0-9]+)h([0-9]+)v1h-([0-9]+)z`)

// parseSVGPath rebuilds a side-square matrix from d. Every byte of d must belong to a
// run; a run must be at least one module wide, close on its own width, stay inside the
// square, and touch no module another run drew. It also reports whether any two runs
// meet end to end in a row -- runs SVGPath should have merged.
func parseSVGPath(d string, side int) (m [][]bool, runs int, unmerged bool, err error) {
	m = make([][]bool, side)
	for y := range m {
		m[y] = make([]bool, side)
	}
	ends := map[[2]int]bool{} // (x just past a run, y)
	at := 0
	for _, loc := range svgRun.FindAllStringSubmatchIndex(d, -1) {
		if loc[0] != at {
			return nil, 0, false, fmt.Errorf("bytes %d-%d are not a run: %q", at, loc[0], d[at:loc[0]])
		}
		at = loc[1]
		num := func(i int) int {
			v, _ := strconv.Atoi(d[loc[2*i]:loc[2*i+1]])
			return v
		}
		x, y, n, back := num(1), num(2), num(3), num(4)
		if n < 1 || back != n {
			return nil, 0, false, fmt.Errorf("run at (%d, %d) is h%d ... h-%d", x, y, n, back)
		}
		if y >= side || x+n > side {
			return nil, 0, false, fmt.Errorf("run at (%d, %d), %d wide, leaves the %d-module square", x, y, n, side)
		}
		if ends[[2]int{x, y}] {
			unmerged = true
		}
		ends[[2]int{x + n, y}] = true
		for i := x; i < x+n; i++ {
			if m[y][i] {
				return nil, 0, false, fmt.Errorf("module (%d, %d) is drawn twice", i, y)
			}
			m[y][i] = true
		}
		runs++
	}
	if at != len(d) {
		return nil, 0, false, fmt.Errorf("bytes %d-%d are not a run: %q", at, len(d), d[at:])
	}
	return m, runs, unmerged, nil
}

// stripBorder is m without border modules on each side, after checking they are light.
func stripBorder(m [][]bool, border int) ([][]bool, error) {
	side := len(m) - 2*border
	out := make([][]bool, side)
	for y := range m {
		for x := range m[y] {
			inside := x >= border && y >= border && x < border+side && y < border+side
			if !inside && m[y][x] {
				return nil, fmt.Errorf("a dark module in the border at (%d, %d)", x, y)
			}
		}
	}
	for y := range out {
		out[y] = m[y+border][border : border+side]
	}
	return out, nil
}

func equalMatrix(a, b [][]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for y := range a {
		if len(a[y]) != len(b[y]) {
			return false
		}
		for x := range a[y] {
			if a[y][x] != b[y][x] {
				return false
			}
		}
	}
	return true
}

// TestSVGPath_ReadsBackToThePlaqueURL is the acceptance round trip (M12-12): the plaque
// URL from sun.PlaqueQRURL, encoded, drawn as one path at the quiet zone and at no border,
// parsed back into modules and decoded -- the decoder returns the URL, at the encoder's
// version, level and mask, at every level.
func TestSVGPath_ReadsBackToThePlaqueURL(t *testing.T) {
	uid, _ := hex.DecodeString("04AC7E5500091A")
	uri, err := sun.PlaqueQRURL("https://time.tappa.mt/t", uid)
	if err != nil {
		t.Fatal(err)
	}
	if uri != "https://time.tappa.mt/t?tag=04AC7E5500091A" {
		t.Fatalf("PREMISE: the plaque URL is %s", uri)
	}
	names := map[qrcode.Level]string{qrcode.L: "L", qrcode.M: "M", qrcode.Q: "Q", qrcode.H: "H"}
	for _, l := range []qrcode.Level{qrcode.L, qrcode.M, qrcode.Q, qrcode.H} {
		c, err := qrcode.Encode([]byte(uri), l)
		if err != nil {
			t.Fatal(err)
		}
		for _, border := range []int{0, qrcode.QuietZone} {
			d := c.SVGPath(border)
			side := c.SVGSide(border)
			if side != c.Size()+2*border {
				t.Fatalf("level %s border %d: SVGSide %d, want %d", names[l], border, side, c.Size()+2*border)
			}
			m, runs, unmerged, err := parseSVGPath(d, side)
			if err != nil {
				t.Fatalf("level %s border %d: %v", names[l], border, err)
			}
			if unmerged {
				t.Errorf("level %s border %d: two runs meet end to end; SVGPath should have merged them", names[l], border)
			}
			if !equalMatrix(m, c.Bitmap(border)) {
				t.Fatalf("level %s border %d: the path's modules are not Bitmap(%d)'s", names[l], border, border)
			}
			sym, err := stripBorder(m, border)
			if err != nil {
				t.Fatalf("level %s border %d: %v", names[l], border, err)
			}
			got, err := read(sym)
			if err != nil {
				t.Fatalf("level %s border %d: read: %v", names[l], border, err)
			}
			if string(got.data) != uri || got.version != c.Version() || got.level != names[l] || got.mask != c.Mask() {
				t.Fatalf("level %s border %d: read %q (version %d level %s mask %d), want %q (version %d mask %d)",
					names[l], border, got.data, got.version, got.level, got.mask, uri, c.Version(), c.Mask())
			}
			t.Logf("level %s border %d: version %d, %d-module side, %d runs, path %d bytes",
				names[l], border, c.Version(), side, runs, len(d))
		}
	}
}

// TestSVGPath_Edges: the smallest symbol (version 1, empty input) and the largest the
// decoder reads (version 10) round-trip through the path and the decoder; version 40
// (the largest there is; outside the decoder's table) is held to Bitmap module by module.
// A negative border draws as border 0, as Bitmap does.
func TestSVGPath_Edges(t *testing.T) {
	longest := func(l qrcode.Level, version int) []byte {
		var data []byte
		for n := 0; ; n++ {
			c, err := qrcode.Encode(make([]byte, n), l)
			if err != nil || c.Version() > version {
				return data
			}
			data = make([]byte, n)
			for i := range data {
				data[i] = byte(i*31 + 7)
			}
		}
	}
	for _, tc := range []struct {
		name    string
		data    []byte
		level   qrcode.Level
		version int
		decode  bool
	}{
		{"version 1, empty", nil, qrcode.H, 1, true},
		{"version 1, full", longest(qrcode.L, 1), qrcode.L, 1, true},
		{"version 10, full", longest(qrcode.M, 10), qrcode.M, 10, true},
		{"version 40, full", bytes.Repeat([]byte{0xA5, 0x00, 0xFF}, 2953/3), qrcode.L, 40, false},
	} {
		c, err := qrcode.Encode(tc.data, tc.level)
		if err != nil || c.Version() != tc.version {
			t.Fatalf("PREMISE: %s: %v, version %d", tc.name, err, c.Version())
		}
		for _, border := range []int{0, 1, qrcode.QuietZone} {
			m, _, unmerged, err := parseSVGPath(c.SVGPath(border), c.SVGSide(border))
			if err != nil {
				t.Fatalf("%s border %d: %v", tc.name, border, err)
			}
			if unmerged {
				t.Errorf("%s border %d: unmerged runs", tc.name, border)
			}
			if !equalMatrix(m, c.Bitmap(border)) {
				t.Fatalf("%s border %d: the path's modules are not Bitmap(%d)'s", tc.name, border, border)
			}
			if !tc.decode {
				continue
			}
			sym, err := stripBorder(m, border)
			if err != nil {
				t.Fatalf("%s border %d: %v", tc.name, border, err)
			}
			got, err := read(sym)
			if err != nil || !bytes.Equal(got.data, tc.data) {
				t.Fatalf("%s border %d: read %d bytes (%v), want the %d input bytes", tc.name, border, len(got.data), err, len(tc.data))
			}
		}
		if c.SVGPath(-3) != c.SVGPath(0) || c.SVGSide(-3) != c.Size() {
			t.Errorf("%s: a negative border does not draw as border 0", tc.name)
		}
	}
}
