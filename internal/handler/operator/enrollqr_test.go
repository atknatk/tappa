package operator_test

// enrollqr_test.go -- the enrollment key's QR code (ADR 0020 §3, QR note; the user's
// decision of 2026-10-09). The encoder's own proof is internal/qrcode's (libqrencode's
// reference matrices and an independent decoder); these tests hold what the PAGE does with
// it: the code is the page's own URI, drawn inside the page, ink on paper with its quiet
// zone, under an unchanged policy, on the first load only. That no other response and no
// log line carries it is the leak test's G19.

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/qrcode"
)

// paletteHex is a colour token's hex as tailwind.config.js defines it, upper case.
func paletteHex(t *testing.T, token string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`'?` + regexp.QuoteMeta(token) + `'?:\s*'(#[0-9A-Fa-f]{6})'`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("tailwind.config.js has no %s", token)
	}
	return strings.ToUpper(string(m[1]))
}

func rgbOf(t *testing.T, hex string) [3]float64 {
	t.Helper()
	var c [3]float64
	for i := range 3 {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		c[i] = float64(v)
	}
	return c
}

// enrollSVG is the one inline SVG of an enrollment page: its opening tag's attributes, in
// order, and its children's markup.
var enrollSVG = regexp.MustCompile(`(?s)<svg ([^>]*)>(.*?)</svg>`)

var svgChildren = regexp.MustCompile(`^\s*<title>Scan with your authenticator app</title>\s*` +
	`<rect width="(\d+)" height="(\d+)" fill="(#[0-9A-Fa-f]{6})"></rect>\s*` +
	`<path d="([^"]*)" fill="(#[0-9A-Fa-f]{6})"></path>\s*$`)

var svgModule = regexp.MustCompile(`M(\d+) (\d+)h1v1h-1z`)

// TestEnrollQR_IsTheKeysURIInkOnPaper -- the first load of the enrollment page, through
// the router:
//
//   - the page holds ONE svg, with exactly the attributes class="op-qr", role="img",
//     aria-label "Scan with your authenticator app", viewBox "0 0 N N", width and height
//     equal, whole pixels per module and at least 200, shape-rendering="crispEdges" -- in
//     that order, no other (no style, no xmlns, no href); its children are exactly a
//     title of the same words, one rect -- the N x N ground -- and one path;
//   - the ground is tailwind.config.js's paper, the path tailwind.config.js's ink (dark
//     modules on a light ground, not the reverse), 16.17:1 apart;
//   - the path is unit squares only, one per dark module, and the dark set is exactly
//     internal/qrcode's level-M symbol of the URI the page prints, with a four-module
//     quiet zone (N = the symbol's side + 8) in which no module is dark;
//   - that URI carries the key the page prints as grouped base32;
//   - the code comes before the key and the URI (the text is under the code);
//   - the body has no style attribute, no style element, no data: URI and no img; the
//     policy is policyEnroll, unchanged, and the response is no-store and no-referrer.
func TestEnrollQR_IsTheKeysURIInkOnPaper(t *testing.T) {
	g := newRig(t)
	w := g.get("/operator/enroll?id=" + uuid.NewString())
	if w.Code != http.StatusOK {
		t.Fatalf("GET the enrollment page = %d", w.Code)
	}
	page := w.Body.String()

	h := w.Result().Header
	if got := h.Values("Content-Security-Policy"); len(got) != 1 || got[0] != policyEnroll {
		t.Errorf("Content-Security-Policy = %q, want the unchanged enrollment policy", got)
	}
	if h.Get("Cache-Control") != "no-store" || h.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Cache-Control %q, Referrer-Policy %q", h.Get("Cache-Control"), h.Get("Referrer-Policy"))
	}
	for _, never := range []string{" style=", "<style", "data:", "<img", "xlink:", "<use", "<image", "<foreignObject"} {
		if strings.Contains(page, never) {
			t.Errorf("the enrollment page carries %q", never)
		}
	}

	if n := strings.Count(page, "<svg"); n != 1 {
		t.Fatalf("the enrollment page holds %d svg element(s), want 1", n)
	}
	m := enrollSVG.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("PREMISE: no svg element could be read")
	}
	attrs := regexp.MustCompile(`([a-zA-Z-]+)="([^"]*)"`).FindAllStringSubmatch(m[1], -1)
	var names []string
	at := map[string]string{}
	for _, a := range attrs {
		names = append(names, a[1])
		at[a[1]] = a[2]
	}
	if got := strings.Join(names, " "); got != "class role aria-label viewBox width height shape-rendering" {
		t.Errorf("the svg's attributes are %q", got)
	}
	if at["class"] != "op-qr" || at["role"] != "img" || at["aria-label"] != "Scan with your authenticator app" ||
		at["shape-rendering"] != "crispEdges" {
		t.Errorf("the svg's attributes are %v", at)
	}
	kids := svgChildren.FindStringSubmatch(m[2])
	if kids == nil {
		t.Fatalf("the svg's children are not a title, a rect and a path: %.300q", m[2])
	}
	n, err := strconv.Atoi(kids[1])
	if err != nil || kids[2] != kids[1] || at["viewBox"] != "0 0 "+kids[1]+" "+kids[1] {
		t.Fatalf("the ground is %s x %s in a viewBox of %q", kids[1], kids[2], at["viewBox"])
	}
	px, err := strconv.Atoi(at["width"])
	if err != nil || at["height"] != at["width"] || px%n != 0 || px < 200 {
		t.Errorf("the code is drawn %s x %s pixels for %d modules: want a square, whole pixels per module, at least 200", at["width"], at["height"], n)
	}

	ink, paper := paletteHex(t, "ink"), paletteHex(t, "paper")
	if strings.ToUpper(kids[3]) != paper || strings.ToUpper(kids[5]) != ink {
		t.Errorf("the ground is %s and the modules %s; want paper %s and ink %s", kids[3], kids[5], paper, ink)
	}
	if c := contrast(rgbOf(t, ink), rgbOf(t, paper)); c < 16 {
		t.Errorf("ink on paper = %.2f:1", c)
	}

	d := kids[4]
	if svgModule.ReplaceAllString(d, "") != "" {
		t.Fatal("the path holds something other than unit squares")
	}
	drawn := map[[2]int]bool{}
	for _, sq := range svgModule.FindAllStringSubmatch(d, -1) {
		x, _ := strconv.Atoi(sq[1])
		y, _ := strconv.Atoi(sq[2])
		if drawn[[2]int{x, y}] {
			t.Fatalf("the module (%d, %d) is drawn twice", x, y)
		}
		drawn[[2]int{x, y}] = true
	}

	um := regexp.MustCompile(`<p class="op-uri">([^<]+)</p>`).FindStringSubmatch(page)
	km := regexp.MustCompile(`<p class="op-key">([A-Z2-7 ]+)</p>`).FindStringSubmatch(page)
	if um == nil || km == nil {
		t.Fatal("PREMISE: the page prints no URI or no key")
	}
	uri := html.UnescapeString(um[1])
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "otpauth" || u.Query().Get("secret") != strings.ReplaceAll(km[1], " ", "") {
		t.Fatalf("the printed URI does not carry the printed key")
	}
	code, err := qrcode.Encode([]byte(uri), qrcode.M)
	if err != nil {
		t.Fatal(err)
	}
	want := code.Bitmap(qrcode.QuietZone)
	if len(want) != n || n != code.Size()+8 {
		t.Fatalf("the drawing is %d modules wide; the URI's symbol with its quiet zone is %d", n, len(want))
	}
	for y := range want {
		for x := range want[y] {
			if drawn[[2]int{x, y}] != want[y][x] {
				t.Fatalf("module (%d, %d): drawn %v, the URI's symbol has %v", x, y, drawn[[2]int{x, y}], want[y][x])
			}
			quiet := x < 4 || y < 4 || x >= n-4 || y >= n-4
			if quiet && drawn[[2]int{x, y}] {
				t.Fatalf("a dark module in the quiet zone at (%d, %d)", x, y)
			}
		}
	}
	if len(drawn) == 0 {
		t.Fatal("PREMISE: the path draws nothing")
	}

	svgAt, keyAt, uriAt := strings.Index(page, "<svg"), strings.Index(page, `class="op-key"`), strings.Index(page, `class="op-uri"`)
	if svgAt >= keyAt || keyAt >= uriAt {
		t.Errorf("the code, the key and the URI are at %d, %d, %d; want the code first", svgAt, keyAt, uriAt)
	}
	t.Logf("version %d, %d modules with the quiet zone, %d x %d pixels, %d dark modules, path %d bytes",
		code.Version(), n, px, px, len(drawn), len(d))
}

// TestEnrollQR_OnlyOnTheFirstLoad: the code is on the enrollment page's first load and on
// no other page -- a re-render after a refused attempt has no key and no svg (the handler
// cannot open the blob), and of the 40 renders screens() makes, the one enrollment render
// that carries a QR matrix is the only one with an svg, and it draws one.
func TestEnrollQR_OnlyOnTheFirstLoad(t *testing.T) {
	g := newRig(t)
	id := uuid.NewString()
	w := g.get("/operator/enroll?id=" + id)
	if strings.Count(w.Body.String(), "<svg") != 1 {
		t.Fatal("PREMISE: the first load draws no code")
	}
	blob := regexp.MustCompile(`name="blob" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if blob == nil {
		t.Fatal("PREMISE: the first load has no blob")
	}
	w = g.post("/operator/enroll", url.Values{"id": {id}, "token": {"FAKEtoken"}, "blob": {html.UnescapeString(blob[1])},
		"password": {"FAKE enroll qr passphrase"}, "password_again": {"FAKE enroll qr passphrase, other"}, "code": {"000000"}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "The two passwords differ") {
		t.Fatalf("PREMISE: the re-render is not the passwords-differ branch (%d)", w.Code)
	}
	if strings.Contains(w.Body.String(), "<svg") || strings.Contains(w.Body.String(), `class="op-key"`) {
		t.Error("the re-render after a refused attempt draws a code or prints a key")
	}
	for name, page := range screens(t) {
		want := 0
		if name == "enroll" {
			want = 1
		}
		if got := strings.Count(page, "<svg"); got != want {
			t.Errorf("%s: %d svg element(s), want %d", name, got, want)
		}
	}
}
