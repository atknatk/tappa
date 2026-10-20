package components

import (
	"bytes"
	"context"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/internal/qrcode"
	"github.com/atknatk/tappa/internal/sun"
)

// renderDrawing is one component's HTML.
func renderDrawing(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// drawingOf builds the view the way internal/handler does — the helper's path and
// side for the plaque URL — so the test compares the template against the helper,
// not against itself.
func drawingOf(t *testing.T, uid string) (PlaqueDrawingView, *qrcode.Code) {
	t.Helper()
	raw, err := hex.DecodeString(uid)
	if err != nil {
		t.Fatal(err)
	}
	u, err := sun.PlaqueQRURL("https://taptime.mt/t", raw)
	if err != nil {
		t.Fatal(err)
	}
	code, err := qrcode.Encode([]byte(u), qrcode.M)
	if err != nil {
		t.Fatal(err)
	}
	return PlaqueDrawingView{
		Last4:  uid[len(uid)-4:],
		QRPath: code.SVGPath(qrcode.QuietZone),
		QRSide: code.SVGSide(qrcode.QuietZone),
	}, code
}

// TestPlaqueDrawing_DrawsTheHelpersPathVerbatim: the template computes no module. The
// d it writes is the view's path byte for byte, inside a square exactly the helper's
// side wide, ink on paper.
func TestPlaqueDrawing_DrawsTheHelpersPathVerbatim(t *testing.T) {
	v, code := drawingOf(t, "04AC7E5500091A")
	html := renderDrawing(t, PlaqueDrawing(v))

	paths := regexp.MustCompile(`<path class="fill-ink" d="([^"]*)"`).FindAllStringSubmatch(html, -1)
	if len(paths) != 1 {
		t.Fatalf("the drawing has %d ink path(s), want 1:\n%s", len(paths), html)
	}
	if paths[0][1] != code.SVGPath(qrcode.QuietZone) {
		t.Errorf("the drawn d is not the helper's path")
	}
	side := code.SVGSide(qrcode.QuietZone)
	for _, want := range []string{
		`viewBox="0 0 ` + strconv.Itoa(side) + ` ` + strconv.Itoa(side) + `"`,
		`<rect width="` + strconv.Itoa(side) + `" height="` + strconv.Itoa(side) + `" class="fill-paper">`,
		`shape-rendering="crispEdges"`,
		`width="` + strconv.Itoa(3*side) + `" height="` + strconv.Itoa(3*side) + `"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the drawing lacks %s", want)
		}
	}
	// Ink on paper, never on green: the only colours the code is drawn in.
	for _, banned := range []string{"tappa-green", "fill-tappa", "fill-paper\" d=", "style="} {
		if strings.Contains(html, banned) {
			t.Errorf("the drawing contains %q", banned)
		}
	}
}

// TestPlaqueDrawing_SaysTheSkillsWordsAsOneImage: the copy is skill tappa-brand's,
// word for word, and the figure is one named image.
func TestPlaqueDrawing_SaysTheSkillsWordsAsOneImage(t *testing.T) {
	v, _ := drawingOf(t, "04AC7E5500091A")
	html := renderDrawing(t, PlaqueDrawing(v))
	for _, want := range []string{
		"Hold your phone here",
		"Or scan — same thing",
		"PLAQUE 091A",
		"drawing, not to scale",
		`<figure role="img" aria-label="` + v.Label() + `"`,
		`<svg viewBox=`, // the code itself is part of the image, not said twice
		`aria-hidden="true" class="h-auto max-w-full"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the drawing does not contain %q", want)
		}
	}
	if !strings.Contains(v.Label(), "091A") {
		t.Errorf("the label %q does not name the plaque", v.Label())
	}
	if strings.Count(html, `aria-hidden="true"`) != 2 {
		t.Errorf("want the rings and the code hidden from a screen reader (2 aria-hidden), got %d",
			strings.Count(html, `aria-hidden="true"`))
	}
	if strings.Contains(strings.ToLower(html), "brand") {
		t.Error("the drawing names a class with \"brand\" in it")
	}
}

// TestPlaqueDrawing_AZeroViewDrawsNothing: no code, no square that reads as one.
func TestPlaqueDrawing_AZeroViewDrawsNothing(t *testing.T) {
	for _, v := range []PlaqueDrawingView{{}, {Last4: "091A"}, {QRPath: "M4 4h1v1h-1z"}, {QRSide: 29}} {
		if html := renderDrawing(t, PlaqueDrawing(v)); strings.TrimSpace(html) != "" {
			t.Errorf("%+v rendered %q, want nothing", v, html)
		}
	}
}

// TestPlaqueNotOnAWall_SaysSoAndDrawsNothing: a venue with no plaque on a wall gets
// the sentence and no drawing.
func TestPlaqueNotOnAWall_SaysSoAndDrawsNothing(t *testing.T) {
	html := renderDrawing(t, PlaqueNotOnAWall())
	if !strings.Contains(html, "No plaque is on a wall here yet") {
		t.Errorf("got %q", html)
	}
	for _, absent := range []string{"<svg", "<figure", "PLAQUE"} {
		if strings.Contains(html, absent) {
			t.Errorf("the empty state renders %q", absent)
		}
	}
}
