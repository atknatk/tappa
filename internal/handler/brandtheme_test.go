package handler

// brandtheme_test.go -- M10 WL-5: the theme route, GET /brand/theme/{HEX}.css
// (ADR 0023 §4, claim A).
//
// Responses here are read from rec.Result() or, in the two wire tests, byte for byte
// off a real server's connection -- not from rec.Header(): Header() is the handler's
// live map after it returned, not what was sent (the OP-8 lesson,
// docs/plan/agent-brief.md).
//
// The route is driven through httpx.NewRouter, the router cmd/tappa serves, built
// with no configuration: RequestID, RealIP (no trusted proxy), AccessLog (no
// logger), Recoverer and Timeout are in those measurements. The middleware NewRouter
// mounts only when its configuration asks for it -- the operator host gate, when
// Config.OperatorHost (TAPPA_OPERATOR_HOST) is set -- is driven by
// TestBrandTheme_TheOperatorHostGateLeavesItToTheCustomerHost.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/session"
)

// brandThemeBodyRE is ADR 0023 claim A's body: exactly one :root rule with the
// three properties in this order, each an "R G B" decimal triple; --brand-edge
// may instead be the word none (ThemeCSS's comment has why).
var brandThemeBodyRE = regexp.MustCompile(`^:root\{` +
	`--brand-accent:(\d{1,3}) (\d{1,3}) (\d{1,3});` +
	`--brand-on-accent:(\d{1,3}) (\d{1,3}) (\d{1,3});` +
	`--brand-edge:(?:(\d{1,3}) (\d{1,3}) (\d{1,3})|none)\}$`)

// brandThemeHeaders is the header map the tests below expect a 200 to carry through
// the router built with no configuration: ADR 0023 §4's three.
var brandThemeHeaders = http.Header{
	"Content-Type":           {"text/css; charset=utf-8"},
	"Cache-Control":          {"public, max-age=31536000, immutable"},
	"X-Content-Type-Options": {"nosniff"},
}

func brandThemeRouter() http.Handler { return httpx.NewRouter(nil, nil, NewBrandTheme()) }

// brandThemeAnswer is one response as it was sent.
type brandThemeAnswer struct {
	status int
	header http.Header
	body   string
}

func brandThemeDo(t *testing.T, h http.Handler, req *http.Request) brandThemeAnswer {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the recorded body: %v", err)
	}
	return brandThemeAnswer{status: res.StatusCode, header: res.Header, body: string(b)}
}

// brandThemeNotFound is the router's own answer for a path nothing is mounted on.
func brandThemeNotFound(t *testing.T, h http.Handler) brandThemeAnswer {
	t.Helper()
	a := brandThemeDo(t, h, httptest.NewRequest(http.MethodGet, "/no/route/is/mounted/here", nil))
	if a.status != http.StatusNotFound {
		t.Fatalf("PREMISE: the router answers an unrouted path with %d, not 404", a.status)
	}
	return a
}

// TestBrandTheme_TheConstructorTakesNothing is ADR 0023 claim A's constructor pin. It
// asserts: NewBrandTheme's compiled signature has no parameter and one result,
// *BrandTheme; BrandTheme is a struct with no field; and, as a control, that the same
// reading sees NewHealth's parameters. So no pool, query interface, session codec or
// audit sink can be handed to the route through its constructor or its struct.
//
// It reads the COMPILED signature and struct through reflect rather than the
// source through go/types: the property is a parameter count and a field count,
// both fully present in the compiled types, and reading them needs no `go list`
// subprocess and no toolchain match. A type alias, a dot import or an embedded
// field is resolved before reflect sees the type.
//
// PART II -- what it catches (each measured, WL-5 card): a variadic parameter added
// to NewBrandTheme; a query-interface field or an embedded *http.Client added to
// BrandTheme; NewBrandTheme returning httpx.Mounter instead of *BrandTheme.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandTheme_TheConstructorTakesNothing(t *testing.T) {
	fn := reflect.TypeOf(NewBrandTheme)
	if fn.Kind() != reflect.Func {
		t.Fatalf("NewBrandTheme is a %s, not a function", fn.Kind())
	}
	if fn.NumIn() != 0 {
		in := make([]string, fn.NumIn())
		for i := range in {
			in[i] = fn.In(i).String()
		}
		t.Errorf("NewBrandTheme takes %v; it takes nothing (ADR 0023 section 4: the theme route "+
			"is given no pool and no query interface, like /healthz)", in)
	}
	if fn.NumOut() != 1 || fn.Out(0) != reflect.TypeOf(&BrandTheme{}) {
		t.Errorf("NewBrandTheme returns %v, want exactly *BrandTheme", fn)
	}
	st := reflect.TypeOf(BrandTheme{})
	if st.Kind() != reflect.Struct || st.NumField() != 0 {
		fields := []string{}
		if st.Kind() == reflect.Struct {
			for i := range st.NumField() {
				fields = append(fields, st.Field(i).Name+" "+st.Field(i).Type.String())
			}
		}
		t.Errorf("BrandTheme is %s with fields %v; it is an empty struct, so the handler "+
			"holds nothing a dependency could be smuggled into", st.Kind(), fields)
	}
	// CONTROL: the reading is not blind. A constructor in this package that does take
	// dependencies reads as such.
	if reflect.TypeOf(NewHealth).NumIn() == 0 {
		t.Fatal("CONTROL: reflect reads NewHealth as taking nothing; this reading is blind")
	}
}

// brandThemeLinear and brandThemeLuminance are this test's own WCAG 2.x relative
// luminance. They ORDER colours; the decisions below are brand.Check's.
var brandThemeLinear = func() (t [256]float64) {
	for v := range t {
		c := float64(v) / 255
		if c <= 0.04045 {
			t[v] = c / 12.92
		} else {
			t[v] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return t
}()

func brandThemeLuminance(c brand.Color) float64 {
	return 0.2126*brandThemeLinear[c.R] + 0.7152*brandThemeLinear[c.G] + 0.0722*brandThemeLinear[c.B]
}

// The four answers the gate gives, in the order they occur as luminance rises.
const (
	brandThemePaperText = iota // accepted, paper label
	brandThemeRefused          // refused: the red band
	brandThemeInkText          // accepted, ink label, no edge
	brandThemeInkEdge          // accepted, ink label, ink edge
)

// brandThemeClass is the gate's answer for c, in brand's own words: paper is
// OnColor(black) and ink is OnColor(white), so the palette is brand's, not a copy.
func brandThemeClass(c brand.Color) int {
	f, err := brand.Check(c)
	switch {
	case err != nil:
		return brandThemeRefused
	case f.Text == brand.OnColor(brand.Color{}):
		return brandThemePaperText
	case f.Edge:
		return brandThemeInkEdge
	default:
		return brandThemeInkText
	}
}

// brandThemeFlip is one place along luminance where the gate's answer changes, and
// the 8-bit colour nearest it on each side.
type brandThemeFlip struct {
	name           string
	from, to       int
	below, above   brand.Color
	lBelow, lAbove float64
}

var (
	brandThemeFlipsOnce sync.Once
	brandThemeFlipsVal  []brandThemeFlip
	brandThemeFlipsErr  string
)

// brandThemeFlips finds the three places the gate's answer changes -- the paper
// text boundary, the ink text boundary and the porcelain edge boundary of ADR 0023
// §3 -- WITHOUT a boundary value: the 256 greys are walked to find between which
// two greys each change happens, and then every 8-bit colour whose luminance lies
// between those two greys is asked (brand.Check) which side it is on. The largest
// luminance on the lower side and the smallest on the upper side are the nearest
// hex either side of the boundary. This function holds no literal hex and no
// literal luminance; the boundaries are where brand's decision flips.
func brandThemeFlips(t *testing.T) []brandThemeFlip {
	t.Helper()
	brandThemeFlipsOnce.Do(func() {
		type window struct{ lo, hi float64 }
		var flips []brandThemeFlip
		var windows []window
		prev := brandThemeClass(brand.Color{})
		for v := 1; v < 256; v++ {
			g := brand.Color{R: uint8(v), G: uint8(v), B: uint8(v)}
			cl := brandThemeClass(g)
			if cl == prev {
				continue
			}
			lo := brand.Color{R: uint8(v - 1), G: uint8(v - 1), B: uint8(v - 1)}
			flips = append(flips, brandThemeFlip{from: prev, to: cl, lBelow: -1, lAbove: 2})
			windows = append(windows, window{brandThemeLuminance(lo), brandThemeLuminance(g)})
			prev = cl
		}
		want := [][2]int{
			{brandThemePaperText, brandThemeRefused},
			{brandThemeRefused, brandThemeInkText},
			{brandThemeInkText, brandThemeInkEdge},
		}
		if len(flips) != len(want) {
			brandThemeFlipsErr = "the greys change the gate's answer " + strconv.Itoa(len(flips)) +
				" times; ADR 0023 section 3 has three boundaries (paper text, ink text, porcelain edge)"
			return
		}
		for i, w := range want {
			if flips[i].from != w[0] || flips[i].to != w[1] {
				brandThemeFlipsErr = "the greys change the gate's answer in an order ADR 0023 section 3 does not describe"
				return
			}
		}
		flips[0].name, flips[1].name, flips[2].name = "paper text boundary", "ink text boundary", "porcelain edge boundary"
		for x := 0; x < 1<<24; x++ {
			c := brand.Color{R: uint8(x >> 16), G: uint8(x >> 8), B: uint8(x)}
			l := brandThemeLuminance(c)
			for i := range flips {
				if l < windows[i].lo || l > windows[i].hi {
					continue
				}
				f := &flips[i]
				switch brandThemeClass(c) {
				case f.from:
					if l > f.lBelow {
						f.below, f.lBelow = c, l
					}
				case f.to:
					if l < f.lAbove {
						f.above, f.lAbove = c, l
					}
				}
			}
		}
		for _, f := range flips {
			if !(f.lBelow >= 0 && f.lAbove <= 1 && f.lBelow < f.lAbove) {
				brandThemeFlipsErr = f.name + ": no colour found on one side"
				return
			}
		}
		brandThemeFlipsVal = flips
	})
	if brandThemeFlipsErr != "" {
		t.Fatal(brandThemeFlipsErr)
	}
	return brandThemeFlipsVal
}

// brandThemeWantBody checks a 200 body against the colour it was asked for, by
// decoding the body's digits rather than by rebuilding it: the accent triple is the
// colour's bytes, the label is paper or ink as the gate says, the edge is ink or
// none as the gate says.
func brandThemeWantBody(t *testing.T, where string, c brand.Color, body string) {
	t.Helper()
	m := brandThemeBodyRE.FindStringSubmatch(body)
	if m == nil {
		t.Errorf("%s: body %q is not ADR 0023's three properties on :root", where, body)
		return
	}
	num := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil || n > 255 || strconv.Itoa(n) != s {
			t.Errorf("%s: %q in the body is not a canonical 0-255 decimal", where, s)
		}
		return n
	}
	triple := func(i int) brand.Color {
		return brand.Color{R: uint8(num(m[i])), G: uint8(num(m[i+1])), B: uint8(num(m[i+2]))}
	}
	ink := brand.OnColor(brand.Color{R: 255, G: 255, B: 255})
	f, err := brand.Check(c)
	if err != nil {
		t.Errorf("%s: answered 200 for %s, which brand.Check refuses: %v", where, c.Hex(), err)
		return
	}
	if got := triple(1); got != c {
		t.Errorf("%s: --brand-accent is %s, asked for %s", where, got.Hex(), c.Hex())
	}
	if got := triple(4); got != f.Text {
		t.Errorf("%s: --brand-on-accent is %s, the gate's label is %s", where, got.Hex(), f.Text.Hex())
	}
	switch {
	case f.Edge && m[7] == "":
		t.Errorf("%s: --brand-edge is none, the gate asks for an edge on %s", where, c.Hex())
	case f.Edge && triple(7) != ink:
		t.Errorf("%s: --brand-edge is %s, the edge is ink (%s)", where, triple(7).Hex(), ink.Hex())
	case !f.Edge && m[7] != "":
		t.Errorf("%s: --brand-edge is a colour, the gate asks for no edge on %s", where, c.Hex())
	}
	if want, _ := brand.ThemeCSS(c); body != want {
		t.Errorf("%s: body %q, brand.ThemeCSS gives %q", where, body, want)
	}
}

// TestBrandTheme_AnswersOnlyACanonicalLegibleHex is ADR 0023 claim A's 200/404
// matrix through httpx.NewRouter. It asserts, on the paths it drives: each accepted
// canonical hex (the table's, black, white, and the computed boundary neighbours on
// the accepted side) answers 200 with exactly the three headers and a body in the
// grammar that carries the hex's own fill; the accepted ones cover both Edge
// branches; each refused path answers with the same status, header map and body as
// a path nothing is mounted on.
//
// The red band's edges are not written here: brandThemeFlips finds the nearest hex
// either side of the paper text and ink text boundaries (and of the porcelain edge
// boundary, for the edge's two branches) from brand.Check's own answers.
//
// PART II -- what it catches: a refused (red band) or non-canonical hex answered 200;
// an accepted canonical hex answered anything but 200; a 404 that differs from the
// router's (status, any header, body); a 200 whose header map is not exactly the
// three; a 200 body outside the grammar or not matching the asked colour's fill.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandTheme_AnswersOnlyACanonicalLegibleHex(t *testing.T) {
	h := brandThemeRouter()
	notFound := brandThemeNotFound(t, h)

	type accepted struct {
		name string
		c    brand.Color
	}
	var ok []accepted
	var refused []string
	for _, f := range brandThemeFlips(t) {
		t.Logf("%s: %s (%.12f) | %s (%.12f)", f.name, f.below.Hex(), f.lBelow, f.above.Hex(), f.lAbove)
		for _, side := range []struct {
			where string
			c     brand.Color
			class int
		}{{"below", f.below, f.from}, {"above", f.above, f.to}} {
			if side.class == brandThemeRefused {
				refused = append(refused, "/brand/theme/"+side.c.Hex()+".css")
			} else {
				ok = append(ok, accepted{f.name + ", " + side.where, side.c})
			}
		}
	}
	// Ordinary accepted accents, as the request spells them (ADR 0023 §3's table,
	// black and white). The path is the input; what the answer must be comes from
	// brand.Check.
	for _, hex := range []string{"1F5C41", "DA291C", "FFC72C", "BE3D2A", "D98E2B", "000000", "FFFFFF"} {
		c, err := brand.ParseAccent(hex)
		if err != nil {
			t.Fatalf("PREMISE: %s does not parse: %v", hex, err)
		}
		ok = append(ok, accepted{hex, c})
	}
	for _, a := range ok {
		path := "/brand/theme/" + a.c.Hex() + ".css"
		got := brandThemeDo(t, h, httptest.NewRequest(http.MethodGet, path, nil))
		if got.status != http.StatusOK {
			t.Errorf("%s: GET %s = %d, want 200", a.name, path, got.status)
			continue
		}
		if !reflect.DeepEqual(got.header, brandThemeHeaders) {
			t.Errorf("%s: GET %s headers %v, want exactly %v", a.name, path, got.header, brandThemeHeaders)
		}
		brandThemeWantBody(t, a.name+" "+path, a.c, got.body)
	}
	// Both branches of the edge are among the accepted answers above.
	edges := map[bool]int{}
	for _, a := range ok {
		edges[brand.Edge(a.c)]++
	}
	if edges[true] == 0 || edges[false] == 0 {
		t.Fatalf("PREMISE: the accepted cases cover Edge = %v only", edges)
	}

	refused = append(refused,
		// the red band, ADR 0023 §3's two examples
		"/brand/theme/808080.css", "/brand/theme/E0457B.css",
		// other spellings of an accepted colour
		"/brand/theme/1f5c41.css", "/brand/theme/1F5c41.css", "/brand/theme/%231F5C41.css",
		"/brand/theme/%31F5C41.css", "/brand/theme/1F5C41%20.css", "/brand/theme/%201F5C41.css",
		"/brand/theme/0x1F5C41.css", "/brand/theme/%EF%BC%911F5C41.css",
		// three, five, seven digits; not hex; empty
		"/brand/theme/FFF.css", "/brand/theme/ABC.css", "/brand/theme/1F5C4.css",
		"/brand/theme/1F5C41A.css", "/brand/theme/GGGGGG.css", "/brand/theme/.css",
		// the extension
		"/brand/theme/1F5C41", "/brand/theme/1F5C41.CSS", "/brand/theme/1F5C41.Css",
		"/brand/theme/1F5C41.json", "/brand/theme/1F5C41.css.css", "/brand/theme/1F5C41.cs",
		"/brand/theme/1F5C41.css%00", "/brand/theme/1F5C41%2Ecss",
		// bytes that would end the rule or the declaration, newlines, traversal
		"/brand/theme/1F5C41%0A.css", "/brand/theme/1F5C41%0D%0A.css", "/brand/theme/1F5C41;.css",
		"/brand/theme/1F5C41%3B.css", "/brand/theme/1F5C41%7D.css", "/brand/theme/1F5C41%7B.css",
		"/brand/theme/..%2F1F5C41.css", "/brand/theme/%2E%2E/1F5C41.css", "/brand/theme/../theme/1F5C41.css",
		"/brand/theme/1F5C41%2F.css",
		// more path, less path
		"/brand/theme/1F5C41.css/", "/brand/theme/1F5C41.css/x", "/brand/theme/x/1F5C41.css",
		"/brand/theme/", "/brand/theme", "/brand/1F5C41.css", "/brand/theme//1F5C41.css",
		// a query, even an empty one
		"/brand/theme/1F5C41.css?v=1", "/brand/theme/1F5C41.css?", "/brand/theme/1F5C41.css?%0A",
	)
	for _, path := range refused {
		got := brandThemeDo(t, h, httptest.NewRequest(http.MethodGet, path, nil))
		if got.status != notFound.status || !reflect.DeepEqual(got.header, notFound.header) || got.body != notFound.body {
			t.Errorf("GET %s = %d %v %q; want the router's 404, %d %v %q",
				path, got.status, got.header, got.body, notFound.status, notFound.header, notFound.body)
		}
	}
	t.Logf("accepted cases: %d, refused cases: %d", len(ok), len(refused))
}

// brandThemeWire is one response as its bytes arrived: the status line, the header
// lines exactly as written (sorted, Date's value replaced once it parses as an HTTP
// date), the body, and the bytes that came between the end of this response and the
// router's 404 for the second request on the same connection.
type brandThemeWire struct {
	status  string
	headers []string
	body    string
	extra   string
}

// brandThemeOnTheWire sends one request on a fresh keep-alive connection and reads
// the response off the connection by hand, not through net/http's parser: that parser
// takes Connection and Transfer-Encoding out of the header map and hands a HEAD
// response no body, so a header or a byte the server should not send can pass a check
// made on what it returns (both measured, 4th and 5th-round audits). The request
// itself carries no Connection header, because a server answers a request that asks
// to close with a Connection: close of its own. The body is Content-Length bytes (none
// for HEAD). Then a second request, for an unrouted path and asking to close, goes on
// the same connection, and the bytes up to the server's close must end with exactly
// the answer that request gets on a connection of its own (brandThemeReference404:
// status line, headers with Date's value masked, the 19-byte body, then the close).
// What comes before that answer belongs to the first response and is returned as
// extra; bytes that do not end with it fail the test (a status line alone is not that
// answer -- W1b and W2h on the WL-5 card). A 5-second deadline turns a connection that
// stalls into a failure.
func brandThemeOnTheWire(t *testing.T, addr, method, path string) brandThemeWire {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(c, "%s %s HTTP/1.1\r\nHost: wl5.test\r\n\r\n", method, path); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(c)
	var w brandThemeWire
	length := 0
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("%s %s: reading the header block: %v (read %q)", method, path, err, line)
		}
		if line == "\r\n" {
			break
		}
		// A line not ended by CRLF keeps its bare LF and so matches no expected line.
		line = strings.TrimSuffix(line, "\r\n")
		if w.status == "" {
			w.status = line
			continue
		}
		name, value, _ := strings.Cut(line, ": ")
		switch name {
		case "Date":
			if _, err := http.ParseTime(value); err != nil {
				t.Errorf("%s %s: Date %q: %v", method, path, value, err)
			}
			line = "Date: (an HTTP date)"
		case "Content-Length":
			if length, err = strconv.Atoi(value); err != nil {
				t.Fatalf("%s %s: Content-Length %q: %v", method, path, value, err)
			}
		}
		w.headers = append(w.headers, line)
	}
	slices.Sort(w.headers)
	if method == http.MethodHead {
		length = 0
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(br, body); err != nil {
		t.Fatalf("%s %s: reading %d body bytes: %v", method, path, length, err)
	}
	w.body = string(body)
	if _, err := fmt.Fprintf(c, "GET /no/route/is/mounted/here HTTP/1.1\r\nHost: wl5.test\r\nConnection: close\r\n\r\n"); err != nil {
		t.Errorf("%s %s: the connection took no second request: %v", method, path, err)
		return w
	}
	rest, err := io.ReadAll(br)
	if err != nil {
		t.Errorf("%s %s: reading the second response: %v", method, path, err)
	}
	got, want := brandThemeMaskDate(string(rest)), brandThemeMaskDate(brandThemeReference404(t, addr))
	extra, ok := strings.CutSuffix(got, want)
	if !ok {
		t.Errorf("%s %s: after the first response came\n  %q\nwhich does not end with the router's 404 for the second request\n  %q",
			method, path, got, want)
	}
	w.extra = extra
	return w
}

// brandThemeDateLine is a Date header line in a raw response.
var brandThemeDateLine = regexp.MustCompile("\r\nDate: [^\r\n]*\r\n")

func brandThemeMaskDate(raw string) string {
	return brandThemeDateLine.ReplaceAllString(raw, "\r\nDate: (an HTTP date)\r\n")
}

// brandThemeReference404 is the bytes the router sends, up to its close, for an
// unrouted GET that asks to close, on a connection of its own. It asserts that this is
// one 404 whose body is net/http's 19-byte "404 page not found\n".
func brandThemeReference404(t *testing.T, addr string) string {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(c, "GET /no/route/is/mounted/here HTTP/1.1\r\nHost: wl5.test\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("reading the reference 404: %v", err)
	}
	ref := string(raw)
	if !strings.HasPrefix(ref, "HTTP/1.1 404 Not Found\r\n") || !strings.HasSuffix(ref, "\r\n\r\n404 page not found\n") ||
		strings.Count(ref, "HTTP/1.1 ") != 1 {
		t.Fatalf("PREMISE: the reference 404 is %q", ref)
	}
	return ref
}

// TestBrandTheme_OnTheWireOnlyNetHTTPAddsHeaders runs the router in a real server
// and reads the responses byte for byte (brandThemeOnTheWire). It asserts: 1F5C41's
// 200 has the status line HTTP/1.1 200 OK and, sorted, exactly the header lines
// Cache-Control, Content-Length (the body's length), Content-Type, Date and
// X-Content-Type-Options, the three with their values; its body is ThemeCSS's; 808080's
// refusal has the status line, the header lines (Date's value aside) and the body of
// the 404 for an unrouted path; and after each of the three responses the connection
// carries exactly the router's 404 for a second request and then closes.
//
// PART II -- what it catches (each measured, WL-5 card): a header added to the 200 or
// to the refusal on the way to the wire, Connection: close included; a trailer; a 200
// sent chunked; a missing header; a hijacked 200 followed by a status line of its own.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandTheme_OnTheWireOnlyNetHTTPAddsHeaders(t *testing.T) {
	srv := httptest.NewServer(brandThemeRouter())
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	green, err := brand.ParseAccent("1F5C41")
	if err != nil {
		t.Fatal(err)
	}
	want, err := brand.ThemeCSS(green)
	if err != nil {
		t.Fatal(err)
	}
	got := brandThemeOnTheWire(t, addr, http.MethodGet, "/brand/theme/1F5C41.css")
	wantHeaders := []string{
		"Cache-Control: public, max-age=31536000, immutable",
		"Content-Length: " + strconv.Itoa(len(want)),
		"Content-Type: text/css; charset=utf-8",
		"Date: (an HTTP date)",
		"X-Content-Type-Options: nosniff",
	}
	if got.status != "HTTP/1.1 200 OK" || !slices.Equal(got.headers, wantHeaders) || got.body != want || got.extra != "" {
		t.Errorf("on the wire the 200 is\n  %q\n  %q\n  body %q, then %q\nwant\n  %q\n  %q\n  body %q, then nothing",
			got.status, got.headers, got.body, got.extra, "HTTP/1.1 200 OK", wantHeaders, want)
	}
	ref := brandThemeOnTheWire(t, addr, http.MethodGet, "/no/route/is/mounted/here")
	refused := brandThemeOnTheWire(t, addr, http.MethodGet, "/brand/theme/808080.css")
	if refused.status != ref.status || !slices.Equal(refused.headers, ref.headers) || refused.body != ref.body ||
		refused.extra != "" || ref.extra != "" {
		t.Errorf("on the wire the refusal is %q %q %q then %q; the router's 404 is %q %q %q then %q",
			refused.status, refused.headers, refused.body, refused.extra, ref.status, ref.headers, ref.body, ref.extra)
	}
}

// TestBrandTheme_HeadIsGetWithoutTheBody measures the HEAD decision (Mount's comment)
// on the wire, reading the bytes by hand (brandThemeOnTheWire): Go's HTTP client hands
// a HEAD response NoBody and does not read what follows it (the 4th-round audit
// measured a hijacked body going unseen by the client-side check this test used
// before). It asserts, for FFC72C (accepted) and 808080 (refused): HEAD's status line
// and sorted header lines equal GET's, Date's value aside (Content-Length included);
// after HEAD's header block, and after GET's body, the connection carries exactly the
// router's 404 for a second request and then closes.
//
// PART II -- what it catches (each measured, WL-5 card): HEAD answered 405 (the
// GET-only route); a GET sent chunked while HEAD is not; a header line on one of
// the two methods only; bytes written after HEAD's header block (a handler that
// hijacks the HEAD connection and writes FFC72C's 81-byte body, or a status line of
// its own) or after GET's body.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandTheme_HeadIsGetWithoutTheBody(t *testing.T) {
	srv := httptest.NewServer(brandThemeRouter())
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	for _, path := range []string{"/brand/theme/FFC72C.css", "/brand/theme/808080.css"} {
		get := brandThemeOnTheWire(t, addr, http.MethodGet, path)
		head := brandThemeOnTheWire(t, addr, http.MethodHead, path)
		if head.status != get.status || !slices.Equal(head.headers, get.headers) {
			t.Errorf("HEAD %s is %q %q; GET is %q %q", path, head.status, head.headers, get.status, get.headers)
		}
		if head.extra != "" {
			t.Errorf("HEAD %s: the server wrote %q after the header block", path, head.extra)
		}
		if get.extra != "" {
			t.Errorf("GET %s: the server wrote %q after the body", path, get.extra)
		}
	}
}

// TestBrandTheme_TheAnswerDoesNotDependOnWhoAsks is ADR 0023 claim A's "authenticates
// nobody" half, measured. It asserts, for 1F5C41 and E0457B: the answer -- status,
// header map, body -- with an employee session cookie, with a panel session cookie
// (both by their real names), with an Authorization header, and with forwarding and
// fetch-metadata headers, each equals the answer without them; and the answer
// without them carries no Set-Cookie and no Vary.
//
// PART II -- what it catches: a response that changes with any of those request
// headers; a Set-Cookie or a Vary on either answer.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandTheme_TheAnswerDoesNotDependOnWhoAsks(t *testing.T) {
	h := brandThemeRouter()
	for _, path := range []string{"/brand/theme/1F5C41.css", "/brand/theme/E0457B.css"} {
		bare := brandThemeDo(t, h, httptest.NewRequest(http.MethodGet, path, nil))
		for name, dress := range map[string]func(*http.Request){
			"employee session cookie": func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: session.CookieName, Value: "anything"})
			},
			"panel session cookie": func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: "anything"})
			},
			"authorization header": func(r *http.Request) { r.Header.Set("Authorization", "Bearer anything") },
			"forwarding and fetch metadata": func(r *http.Request) {
				r.Header.Set("X-Forwarded-For", "203.0.113.7")
				r.Header.Set("X-Request-Id", "wl5-probe")
				r.Header.Set("Origin", "https://elsewhere.example")
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				r.Header.Set("Accept", "text/html")
			},
		} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			dress(req)
			got := brandThemeDo(t, h, req)
			if got.status != bare.status || !reflect.DeepEqual(got.header, bare.header) || got.body != bare.body {
				t.Errorf("%s with %s: %d %v %q; without: %d %v %q", path, name,
					got.status, got.header, got.body, bare.status, bare.header, bare.body)
			}
		}
		if bare.header.Get("Set-Cookie") != "" || bare.header.Get("Vary") != "" {
			t.Errorf("%s sets a cookie or varies: %v", path, bare.header)
		}
	}
}

// TestBrandTheme_TheOperatorHostGateLeavesItToTheCustomerHost measures the route
// behind the operator host gate, which NewRouter mounts when Config.OperatorHost
// (TAPPA_OPERATOR_HOST) is set. It
// asserts, for 1F5C41, FFC72C and 808080: on the customer host the answer behind the
// gate has the status, header map and body of the router built with no
// configuration; on the operator host it equals the gate's 404 for a path nothing
// is mounted on; and 1F5C41 on the customer host is a 200 with exactly the three
// headers.
//
// PART II -- what it catches: the route answering differently on the customer host
// once the gate is mounted (a header, the status, the body); the theme served on the
// operator host.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandTheme_TheOperatorHostGateLeavesItToTheCustomerHost(t *testing.T) {
	const operatorHost, customerHost = "ops.wl5.test", "time.wl5.test"
	gated := httpx.NewRouter(&config.Config{OperatorHost: operatorHost}, nil, NewBrandTheme())
	plain := brandThemeRouter()
	on := func(h http.Handler, host, path string) brandThemeAnswer {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = host
		return brandThemeDo(t, h, req)
	}
	ref := on(gated, operatorHost, "/no/route/is/mounted/here")
	if ref.status != http.StatusNotFound {
		t.Fatalf("PREMISE: the gate answers an unrouted path on the operator host with %d", ref.status)
	}
	for _, path := range []string{"/brand/theme/1F5C41.css", "/brand/theme/FFC72C.css", "/brand/theme/808080.css"} {
		want := on(plain, customerHost, path)
		got := on(gated, customerHost, path)
		if got.status != want.status || !reflect.DeepEqual(got.header, want.header) || got.body != want.body {
			t.Errorf("customer host %s: %d %v %q behind the gate, %d %v %q without it",
				path, got.status, got.header, got.body, want.status, want.header, want.body)
		}
		ops := on(gated, operatorHost, path)
		if ops.status != ref.status || !reflect.DeepEqual(ops.header, ref.header) || ops.body != ref.body {
			t.Errorf("operator host %s: %d %v %q; the gate's 404 is %d %v %q",
				path, ops.status, ops.header, ops.body, ref.status, ref.header, ref.body)
		}
	}
	if a := on(gated, customerHost, "/brand/theme/1F5C41.css"); a.status != http.StatusOK || !reflect.DeepEqual(a.header, brandThemeHeaders) {
		t.Errorf("PREMISE: the customer host's 200 is %d %v", a.status, a.header)
	}
}

// TestBrandTheme_OtherMethodsAre405 drives the seven standard methods other than GET
// and HEAD (POST, PUT, PATCH, DELETE, OPTIONS, TRACE, CONNECT) on an accepted and on a
// refused hex, and asserts 405 with an Allow naming exactly GET and HEAD; it drives
// PROPFIND, a method chi does not know, and asserts 405 (the status only -- that chi
// sends no Allow for it was measured once, not asserted).
//
// PART II -- what it catches: the route registered for another standard method (that
// method then answers 200 or 404 instead of 405); an Allow naming any other method;
// an unknown method answered anything but 405.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandTheme_OtherMethodsAre405(t *testing.T) {
	h := brandThemeRouter()
	for _, path := range []string{"/brand/theme/1F5C41.css", "/brand/theme/808080.css"} {
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodTrace, http.MethodConnect} {
			a := brandThemeDo(t, h, httptest.NewRequest(m, path, nil))
			allow := slices.Sorted(slices.Values(a.header.Values("Allow")))
			if a.status != http.StatusMethodNotAllowed || !slices.Equal(allow, []string{"GET", "HEAD"}) {
				t.Errorf("%s %s = %d, Allow %v; want 405 with Allow GET and HEAD", m, path, a.status, allow)
			}
		}
		if a := brandThemeDo(t, h, httptest.NewRequest("PROPFIND", path, nil)); a.status != http.StatusMethodNotAllowed {
			t.Errorf("PROPFIND %s = %d, want 405", path, a.status)
		}
	}
}

// brandThemeShipped is the ledger of (route, body digest) pairs this route has been
// served under, oldest first. RULE, KEPT BY REVIEW (the test below does not enforce
// it -- an entry's digest rewritten in place keeps it green, measured): a pair is
// added for each new set of bodies, and an entry is not edited or removed, because a
// browser or proxy may hold its bodies for a year (immutable).
var brandThemeShipped = []struct{ route, digest, since string }{
	{"/brand/theme/{file}", "2be8e06ab95602ffdef5c448f48d46d702320cf2dfd2ad80fd4dc10b6b5e19fd", "M10 WL-5, 2026-10-03"},
}

// brandThemeBodiesDigest is a sha256 over what ThemeCSS answers for a fixed set of
// colours: ADR 0023 §3's table, black, white, and the nearest hex either side of the
// three places the gate's answer changes (brandThemeFlips, computed from brand's own
// answers, so a palette or threshold change that moves a boundary moves them too) --
// fifteen colours. A refusal is recorded as REFUSED. Each line is "HEX=body".
func brandThemeBodiesDigest(t *testing.T) string {
	t.Helper()
	var colours []brand.Color
	for _, x := range []string{"808080", "E0457B", "DA291C", "FFC72C", "1F5C41", "BE3D2A", "D98E2B", "000000", "FFFFFF"} {
		c, err := brand.ParseAccent(x)
		if err != nil {
			t.Fatalf("PREMISE: %s: %v", x, err)
		}
		colours = append(colours, c)
	}
	for _, f := range brandThemeFlips(t) {
		colours = append(colours, f.below, f.above)
	}
	h := sha256.New()
	for _, c := range colours {
		body, err := brand.ThemeCSS(c)
		switch {
		case errors.Is(err, brand.ErrAccentIllegible):
			body = "REFUSED"
		case err != nil:
			t.Fatalf("ThemeCSS(%s): %v", c.Hex(), err)
		}
		fmt.Fprintf(h, "%s=%s\n", c.Hex(), body)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestBrandTheme_ANewBodyNeedsANewRoute backs the route's immutable cache
// (Cache-Control: public, max-age=31536000, immutable). The URL names a colour; the
// body also depends on the palette, on ThemeCSS's format and on Check's thresholds,
// and a browser holding a body under max-age=31536000, immutable does not ask again
// for a year. What it asserts, and nothing more: Mount registers exactly one path
// (read from the router with
// chi.Walk, not from the constant); no route and no digest appears twice in
// brandThemeShipped; today's digest (brandThemeBodiesDigest) appears there exactly
// once; and that entry's route is the path Mount registers.
//
// PART II -- what it catches: a palette constant, a Check threshold or ThemeCSS's
// output changed with the ledger unchanged (measured: a palette hex, the edge
// threshold, the property order); a second ledger entry under a route already in it;
// Mount registering a path other than the ledger's (measured: the constant
// unchanged and Mount given another literal; the constant changed with the bodies
// unchanged). It does not catch a body change on a colour outside the digest's set
// while all fifteen answer as before, an entry deleted from the ledger, or an
// entry's digest rewritten in place (measured: a palette change with the one entry's
// digest rewritten stays green) -- the ledger line changing is what review sees.
// PART III: a form not on that list is code review's -- no completeness claim.
func TestBrandTheme_ANewBodyNeedsANewRoute(t *testing.T) {
	r := chi.NewRouter()
	NewBrandTheme().Mount(r)
	mounted := map[string]bool{}
	if err := chi.Walk(r, func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted[route] = true
		return nil
	}); err != nil {
		t.Fatalf("walking the routes Mount registered: %v", err)
	}
	if len(mounted) != 1 {
		t.Fatalf("Mount registered the paths %v; the ledger below holds one path per set of bodies",
			slices.Sorted(maps.Keys(mounted)))
	}
	route := slices.Collect(maps.Keys(mounted))[0]
	digest := brandThemeBodiesDigest(t)
	routes, digests := map[string]bool{}, map[string]bool{}
	for _, e := range brandThemeShipped {
		if routes[e.route] || digests[e.digest] {
			t.Errorf("the ledger repeats route %q or digest %s: one route serves one set of bodies, "+
				"for as long as a cache may hold it", e.route, e.digest)
		}
		routes[e.route], digests[e.digest] = true, true
	}
	var found []string
	for _, e := range brandThemeShipped {
		if e.digest == digest {
			found = append(found, e.route)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the theme bodies' digest is %s and the ledger has it %d times.\n"+
			"What ThemeCSS answers has changed (the palette, a Check threshold or the body's "+
			"format). Browsers hold the old body for a year under the old URL, so the route "+
			"should change too: give brandThemeRoute a new path (a version segment) and ADD "+
			"{route, digest} to brandThemeShipped, keeping the old entries.", digest, len(found))
	}
	if found[0] != route {
		t.Errorf("these bodies were shipped under %q, Mount registers %q", found[0], route)
	}
}
