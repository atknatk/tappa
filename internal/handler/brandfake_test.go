package handler

// brandfake_test.go -- the doubles and fixtures the brand editor's tests share (M10
// WL-7): a brand writer that records what it was handed, a decode gate that counts
// how often it was asked, logo files built in code (no binary fixture in the tree), a
// panel wired with them, and request builders for multipart bodies -- well-formed and
// deliberately not.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// fakeBrandWriter is the double for tenant.Brands. It records every command it was
// handed, in order, and answers with the error a test set (nil by default).
type fakeBrandWriter struct {
	mu      sync.Mutex
	accents []tenant.BrandAccentCommand
	logos   []tenant.BrandLogoCommand
	clearsA []tenant.BrandClearCommand
	clearsL []tenant.BrandClearCommand
	err     error
	logoErr error
}

func newFakeBrandWriter() *fakeBrandWriter { return &fakeBrandWriter{} }

func (f *fakeBrandWriter) SaveAccent(_ context.Context, c tenant.BrandAccentCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accents = append(f.accents, c)
	return f.err
}

func (f *fakeBrandWriter) ClearAccent(_ context.Context, c tenant.BrandClearCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearsA = append(f.clearsA, c)
	return f.err
}

func (f *fakeBrandWriter) SaveLogo(_ context.Context, c tenant.BrandLogoCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logos = append(f.logos, c)
	if f.logoErr != nil {
		return f.logoErr
	}
	return f.err
}

func (f *fakeBrandWriter) ClearLogo(_ context.Context, c tenant.BrandClearCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearsL = append(f.clearsL, c)
	return f.err
}

// writes is how many commands reached the writer, of any kind.
func (f *fakeBrandWriter) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.accents) + len(f.logos) + len(f.clearsA) + len(f.clearsL)
}

func (f *fakeBrandWriter) savedLogos() []tenant.BrandLogoCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tenant.BrandLogoCommand(nil), f.logos...)
}

func (f *fakeBrandWriter) savedAccents() []tenant.BrandAccentCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tenant.BrandAccentCommand(nil), f.accents...)
}

func (f *fakeBrandWriter) clears() (accent, logo []tenant.BrandClearCommand) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tenant.BrandClearCommand(nil), f.clearsA...), append([]tenant.BrandClearCommand(nil), f.clearsL...)
}

// countingGate wraps a decode gate and counts the calls that reached it, so a test can
// assert that a refusal came BEFORE the decoder. When fail is set it answers that
// error instead of calling the real gate.
type countingGate struct {
	mu    sync.Mutex
	inner logoNormalizer
	calls int
	fail  error
	// seen is the length of each body the gate was handed.
	seen []int
}

func (g *countingGate) Normalize(ctx context.Context, r io.Reader) (brand.Logo, error) {
	data, err := io.ReadAll(r)
	g.mu.Lock()
	g.calls++
	g.seen = append(g.seen, len(data))
	fail := g.fail
	g.mu.Unlock()
	if err != nil {
		return brand.Logo{}, err
	}
	if fail != nil {
		return brand.Logo{}, fail
	}
	return g.inner.Normalize(ctx, bytes.NewReader(data))
}

func (g *countingGate) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// --- logo files, built in code ------------------------------------------------------

// solidPNG is a w x h PNG of one colour.
func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("encoding a PNG: %v", err)
	}
	return b.Bytes()
}

// noisyJPEG is a w x h JPEG of a gradient with noise, at quality q: a file whose size
// grows with w, h and q, so a test can ask for one larger than a threshold.
func noisyJPEG(t *testing.T, w, h, q int, seed uint64) []byte {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := uint8(rng.IntN(64))
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), n + uint8(y*128/h), 255 - n, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatalf("encoding a JPEG: %v", err)
	}
	return b.Bytes()
}

// inkLogo is a small dark PNG that normalizes and is not light on porcelain.
func inkLogo(t *testing.T) []byte {
	t.Helper()
	return solidPNG(t, 64, 32, color.NRGBA{0x15, 0x22, 0x19, 0xFF})
}

// paleLogo is a small PNG almost the colour of porcelain: light on it.
func paleLogo(t *testing.T) []byte {
	t.Helper()
	return solidPNG(t, 64, 32, color.NRGBA{0xF4, 0xF6, 0xF2, 0xFF})
}

// --- multipart bodies ---------------------------------------------------------------

// logoForm is a well-formed multipart/form-data body with one file part per entry,
// each under its own name, file name and part Content-Type.
type logoPart struct {
	name, fileName, contentType string
	data                        []byte
}

func logoMultipart(t *testing.T, parts ...logoPart) (body []byte, contentType string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		disp := fmt.Sprintf(`form-data; name=%q`, p.name)
		if p.fileName != "" {
			disp += fmt.Sprintf(`; filename=%q`, p.fileName)
		}
		h.Set("Content-Disposition", disp)
		if p.contentType != "" {
			h.Set("Content-Type", p.contentType)
		}
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatalf("multipart part: %v", err)
		}
		if _, err := w.Write(p.data); err != nil {
			t.Fatalf("multipart write: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return b.Bytes(), mw.FormDataContentType()
}

// oneLogo is the body a browser sends for one chosen file.
func oneLogo(t *testing.T, data []byte) ([]byte, string) {
	t.Helper()
	return logoMultipart(t, logoPart{name: "logo", fileName: "logo.png", contentType: "image/png", data: data})
}

// --- a panel wired for the brand routes ---------------------------------------------

// brandPanel is the panel's real wiring (real budgets, real chain) with the doubles the
// brand tests control, and the log it writes.
type brandPanel struct {
	h      *AdminAuth
	admins *fakeAdmins
	trail  *fakeTrail
	writer *fakeBrandWriter
	brands *fakeBrands
	gate   *countingGate
	logs   *lockedBuffer
	router http.Handler
}

// newBrandPanel resolves every session to role in tenantID, and wraps the panel's own
// decode gate in a counter.
func newBrandPanel(t *testing.T, role string, tenantID uuid.UUID) *brandPanel {
	t.Helper()
	p := &brandPanel{
		admins: &fakeAdmins{verify: func() (adminauth.Resolved, error) {
			return adminauth.Resolved{
				SessionID: panelTestSession, TenantID: tenantID, AdminUserID: panelTestAdmin,
				Role: role, FullName: "Maria Borg",
			}, nil
		}},
		trail:  &fakeTrail{},
		writer: newFakeBrandWriter(),
		brands: newFakeBrands(),
		logs:   &lockedBuffer{},
	}
	records := newFakeLedger()
	h, err := NewAdminAuth(p.admins, p.trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), p.brands, p.writer, nil, &fakeNotices{}, adminTestConfig(),
		slog.New(slog.NewTextHandler(p.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	p.gate = &countingGate{inner: h.logoGate}
	h.logoGate = p.gate
	p.h = h
	r := chi.NewRouter()
	h.Mount(r)
	p.router = r
	return p
}

// browser is a signed-in browser over the panel (httptest recorder: no connection, so
// the upload route's read deadline cannot be set and it answers 500 -- the upload tests
// use server()).
func (p *brandPanel) browser(t *testing.T) *browser {
	t.Helper()
	b := newBrowser(t, p.router)
	b.cookies[adminauth.CookieName] = panelCookie().Value
	return b
}

// server is the panel behind a real HTTP server, for the upload route.
func (p *brandPanel) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(p.router)
	t.Cleanup(s.Close)
	return s
}

// noRedirects is a client that reports a redirect rather than following it.
func noRedirects() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// upload posts body to the logo route of s as a signed-in, same-origin browser does.
func upload(t *testing.T, s *httptest.Server, body []byte, contentType string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, s.URL+brandLogoHref, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", testBaseURL)
	req.AddCookie(panelCookie())
	res, err := noRedirects().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", brandLogoHref, err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	return res, string(got)
}

// outcomeOf is the brand word a 303 to the section carries, or "" when the answer is
// not that redirect.
func outcomeOf(res *http.Response) string {
	if res.StatusCode != http.StatusSeeOther {
		return ""
	}
	loc := res.Header.Get("Location")
	const prefix = "/admin/account?brand="
	if !strings.HasPrefix(loc, prefix) || !strings.HasSuffix(loc, "#brand") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(loc, prefix), "#brand")
}
