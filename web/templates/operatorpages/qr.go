package operatorpages

import (
	"strconv"
	"strings"
)

// THE ENROLLMENT KEY'S QR CODE, AS INLINE SVG (ADR 0020 §3, QR note). The enrollment
// screen draws EnrollView.QR inside its own response: no image URL, no data: URI, no
// style attribute -- the page's policy (enrollCSP: default-src 'none', style-src 'self')
// is unchanged, and the key travels in no request. Ink modules on a paper ground, the
// colours as SVG presentation attributes, the quiet zone part of the matrix.
//
// The two colours are the palette's ink and paper, written as hex because a presentation
// attribute takes a colour, not a token; TestEnrollQR_IsTheKeysURIInkOnPaper holds them to
// tailwind.config.js's.
const (
	qrInk   = "#152219"
	qrPaper = "#FFFDF4"
)

// qrTargetPixels is the size the code is drawn at, before rounding down to whole pixels
// per module: a phone's camera reads it at arm's length, and it fits the enrollment card
// on a 360-pixel-wide screen. Versions 1-15 come out at 207-264 pixels (version 11's
// 69-module side is the least); from version 16 the three-pixel floor draws more and the
// class's maximum width shrinks it to the column. The URI the page encodes is 134 bytes
// plus the operator host's length: a version 8 code (57 modules, 228 pixels) up to an
// 18-byte host, version 9 (61, 244) up to 46 bytes.
const qrTargetPixels = 264

// qrSide is the code's side in modules, the quiet zone included.
func qrSide(m [][]bool) string { return strconv.Itoa(len(m)) }

// qrViewBox is the SVG's coordinate system: one unit per module.
func qrViewBox(m [][]bool) string { return "0 0 " + qrSide(m) + " " + qrSide(m) }

// qrPixels is the drawn side in CSS pixels: whole pixels per module, at least three, so
// every module is the same width on a screen of device-pixel ratio 1. A side wider than
// its column is shrunk by the class's maximum width.
func qrPixels(m [][]bool) string {
	if len(m) == 0 {
		return "0"
	}
	return strconv.Itoa(len(m) * max(3, qrTargetPixels/len(m)))
}

// qrPath is one SVG path with a unit square sub-path per dark module -- one element
// however many modules, not a rect per module.
func qrPath(m [][]bool) string {
	var b strings.Builder
	for y, row := range m {
		for x, dark := range row {
			if dark {
				b.WriteString("M")
				b.WriteString(strconv.Itoa(x))
				b.WriteString(" ")
				b.WriteString(strconv.Itoa(y))
				b.WriteString("h1v1h-1z")
			}
		}
	}
	return b.String()
}
