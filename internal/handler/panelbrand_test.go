package handler

// panelbrand_test.go -- M10 WL-8: the business's brand in the panel chrome, against
// fakes, through the REAL router and the real AdminAuth wiring. Every response is read
// from rec.Result() (htmlOf; the recorder's live header map is not the wire).
//
// The brand reader is a fake that keeps tenant.BrandReader.PanelBrand's contract for
// what the chrome sees; the reader's own behaviour against Postgres -- one statement,
// its own business only, the gate on the read side, half rows refused -- is
// internal/domain/tenant's brandread_db_test.go.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// fakeBrands is the chrome's brand reader. It answers one brand (or one error) and
// records the business each call named.
type fakeBrands struct {
	mu    sync.Mutex
	brand tenant.PanelBrand
	err   error
	calls []uuid.UUID
}

// newFakeBrands answers "no brand" -- what every panel test that does not care about
// the brand gets, so their pages are the chrome as before WL-8.
func newFakeBrands() *fakeBrands { return &fakeBrands{} }

func (f *fakeBrands) PanelBrand(_ context.Context, tenantID uuid.UUID) (tenant.PanelBrand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tenantID)
	if f.err != nil {
		return tenant.PanelBrand{}, f.err
	}
	return f.brand, nil
}

// set makes the reader answer b (or err) from now on and forgets the calls so far. One
// router serves every variant of a test, so the other fakes' values (fresh ids per
// fake) are the same in every response compared.
func (f *fakeBrands) set(b tenant.PanelBrand, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.brand, f.err, f.calls = b, err, nil
}

func (f *fakeBrands) callLog() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.calls...)
}

// The brand fixtures. The name needs escaping in text and in an attribute; the accent
// takes paper text and no edge (ADR 0023 §3's table); the logo is business A's of
// brandlogo_test.go, stored 512x128.
const wl8Name = `Ta' Ħaġar & "Sons" <Ltd>`

func wl8Accent(t *testing.T) brand.Color {
	t.Helper()
	c, err := brand.ParseAccent("DA291C")
	if err != nil {
		t.Fatalf("ParseAccent: %v", err)
	}
	if _, err := brand.Check(c); err != nil {
		t.Fatalf("PREMISE: DA291C does not pass the gate: %v", err)
	}
	return c
}

func wl8Brand(t *testing.T, accent, logo bool) tenant.PanelBrand {
	t.Helper()
	b := tenant.PanelBrand{Name: wl8Name}
	if accent {
		b.Accent, b.HasAccent = wl8Accent(t), true
	}
	if logo {
		b.Logo = tenant.LogoRef{SHA256: digestOf(logoPNGA), MIME: "image/png", Width: 512, Height: 128}
		b.HasLogo = true
	}
	return b
}

// newAdminRouterWithBrands is the panel's real wiring with the caller's brand reader and
// a logger the caller can read.
func newAdminRouterWithBrands(t *testing.T, admins *fakeAdmins, brands panelBrands, log *slog.Logger) *AdminAuth {
	t.Helper()
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), brands, newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), log)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	return h
}

// ownerOf resolves a live owner session for tenantID.
func ownerOf(tenantID uuid.UUID) *fakeAdmins {
	return &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: tenantID, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
}

// lockedBuffer is a log sink the handler writes to and the test reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// brandedPanel is a signed-in owner's browser over the panel, with brands as its brand
// reader, and the log the panel writes.
func brandedPanel(t *testing.T, brands panelBrands) (*browser, *lockedBuffer) {
	t.Helper()
	logs := &lockedBuffer{}
	h := newAdminRouterWithBrands(t, ownerOf(panelTestTenant), brands,
		slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = panelCookie().Value
	return b, logs
}

// sectionAnswer is one section's response as the wire carries it.
type sectionAnswer struct {
	status int
	csp    string
	body   string
}

func getSection(t *testing.T, b *browser, href string) sectionAnswer {
	t.Helper()
	rec := b.do(http.MethodGet, href, nil)
	res := rec.Result()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("GET %s: reading the body: %v", href, err)
	}
	return sectionAnswer{res.StatusCode, res.Header.Get("Content-Security-Policy"), string(body)}
}

// The panel's two policies as literals (TestPagePolicies_ALogoWidensByImgSrcAlone's
// spelling): a change to the constants is a change this file sees too.
const (
	wl8AdminPolicy    = "default-src 'none'; style-src 'self'; font-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	wl8ScriptedPolicy = wl8AdminPolicy + "; script-src 'self'; connect-src 'self'"
	wl8ImgSrc         = "; img-src 'self'"
)

// wantSectionPolicy is the policy a section answers with today: the scripted one on
// the transactions section, plus img-src when the chrome draws the logo.
func wantSectionPolicy(href string, logo bool) string {
	p := wl8AdminPolicy
	if href == transactionsHref {
		p = wl8ScriptedPolicy
	}
	if logo {
		p += wl8ImgSrc
	}
	return p
}

// wordmarkHTML is layout.Wordmark rendered: the chrome's opening when not branded.
func wordmarkHTML(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	if err := layout.Wordmark().Render(context.Background(), &b); err != nil {
		t.Fatalf("rendering the wordmark: %v", err)
	}
	return b.String()
}

var (
	stylesheetRE = regexp.MustCompile(`<link rel="stylesheet" href="([^"]*)">`)
	imgTagRE     = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	brandHeadRE  = regexp.MustCompile(`(?s)<header class="flex flex-col gap-3">.*?</header>`)
)

// stylesheetsOf is every stylesheet the page links, in order, and whether they are all
// in the head.
func stylesheetsOf(body string) (hrefs []string, allInHead bool) {
	head := body
	if i := strings.Index(body, "</head>"); i >= 0 {
		head = body[:i]
	}
	for _, m := range stylesheetRE.FindAllStringSubmatch(body, -1) {
		hrefs = append(hrefs, m[1])
	}
	return hrefs, len(stylesheetRE.FindAllString(head, -1)) == len(hrefs)
}

// attr is one attribute's value on one tag, as written (escaped).
func attr(tag, name string) (string, bool) {
	m := regexp.MustCompile(`\s` + name + `="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// TestPanelBrand_UnbrandedSectionsAreTheWordmarkChrome drives every section of
// pages.PanelSections, as an owner, through the real router, for four businesses the
// chrome must not brand: no brand; a brand read that fails; an accent the gate refuses
// and no logo; a logo whose stored digest is not 64 lower-case hex digits. Each
// response: 200; the policy as today (the literal; scripted on transactions); the
// wordmark exactly once and before the tab bar; one stylesheet, app.css, in the head;
// no <img>, no stripe, no theme or logo path, no business name, no co-brand line; the
// same bytes as the no-brand business's response. The brand reader is asked once per
// request, for the session's business. The failed and the inconsistent reads are
// logged at ERROR with the tenant id; the refused accent at WARN.
func TestPanelBrand_UnbrandedSectionsAreTheWordmarkChrome(t *testing.T) {
	assertSectionTableIsUsable(t)
	wordmark := wordmarkHTML(t)
	logoOnly := wl8Brand(t, false, true)
	logoOnly.Logo.SHA256 = strings.ToUpper(logoOnly.Logo.SHA256)
	variants := []struct {
		name    string
		brand   tenant.PanelBrand
		err     error
		wantLog string // a substring the log must carry; "" for none
		level   string
	}{
		{"no brand", tenant.PanelBrand{}, nil, "", ""},
		{"the read fails", tenant.PanelBrand{}, errors.New("connection reset by peer"),
			"could not read the business's brand", "level=ERROR"},
		{"the accent is refused today, no logo", tenant.PanelBrand{AccentRefused: true}, nil,
			"does not pass the legibility gate today", "level=WARN"},
		{"the stored digest is malformed", logoOnly, nil,
			"is not 64 lower-case hex digits", "level=ERROR"},
	}
	brands := newFakeBrands()
	b, logs := brandedPanel(t, brands)
	reference := map[string]string{}
	for _, v := range variants {
		brands.set(v.brand, v.err)
		logs.Reset()
		for _, s := range pages.PanelSections {
			got := getSection(t, b, s.Href)
			where := v.name + ", GET " + s.Href
			if got.status != http.StatusOK {
				t.Fatalf("%s: %d", where, got.status)
			}
			if want := wantSectionPolicy(s.Href, false); got.csp != want {
				t.Errorf("%s: policy %q, want today's %q", where, got.csp, want)
			}
			// M10 WL-7: the Account section's brand editor draws the tap screen's header in
			// its preview -- the wordmark, unbranded -- so the chrome is read with that
			// one region taken out (withoutBrandEditor counts it: once on the Account
			// section, nowhere else). The full bodies are still compared below.
			chrome := withoutBrandEditor(t, s.Href, got.body)
			nav := strings.Index(chrome, `<nav class="tab-bar"`)
			if strings.Count(chrome, wordmark) != 1 || nav < 0 || strings.Index(chrome, wordmark) > nav {
				t.Errorf("%s: the wordmark is not the chrome's opening (count %d)", where, strings.Count(chrome, wordmark))
			}
			if hrefs, inHead := stylesheetsOf(got.body); !slices.Equal(hrefs, []string{"/static/css/app.css"}) || !inHead {
				t.Errorf("%s: stylesheets %v (all in head: %v), want app.css alone", where, hrefs, inHead)
			}
			for _, never := range []string{"<img", "panel-stripe", "/brand/theme/", "/admin/brand/logo/",
				"taptime · punchless", "Ħaġar", htmlEscaper.Replace(wl8Name)} {
				if strings.Contains(strings.ToLower(got.body), strings.ToLower(never)) {
					t.Errorf("%s: the page carries %q", where, never)
				}
			}
			if ref, ok := reference[s.Href]; !ok {
				reference[s.Href] = got.body
			} else if got.body != ref {
				t.Errorf("%s: the body differs from the no-brand business's", where)
			}
		}
		calls := brands.callLog()
		if len(calls) != len(pages.PanelSections) {
			t.Errorf("%s: the brand reader was asked %d times for %d section requests, want one each",
				v.name, len(calls), len(pages.PanelSections))
		}
		for _, c := range calls {
			if c != panelTestTenant {
				t.Errorf("%s: the brand reader was asked for %s, not the session's business", v.name, c)
			}
		}
		out := logs.String()
		if v.wantLog == "" {
			if strings.Contains(out, "brand") || strings.Contains(out, "accent") {
				t.Errorf("%s: the panel logged about the brand:\n%s", v.name, out)
			}
			continue
		}
		var lines []string
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, v.wantLog) {
				lines = append(lines, l)
			}
		}
		if len(lines) != len(pages.PanelSections) {
			t.Errorf("%s: %d log lines carry %q, want one per section request:\n%s", v.name, len(lines), v.wantLog, out)
		}
		for _, l := range lines {
			if !strings.Contains(l, v.level) || !strings.Contains(l, "tenant_id="+panelTestTenant.String()) {
				t.Errorf("%s: log line %q does not carry %s and the tenant id", v.name, l, v.level)
			}
			for _, never := range []string{"Ħaġar", strings.ToLower(digestOf(logoPNGA))} {
				if strings.Contains(strings.ToLower(l), strings.ToLower(never)) {
					t.Errorf("%s: log line %q carries %q", v.name, l, never)
				}
			}
		}
	}
}

// TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection drives every section as
// an owner of a business with an accent and a logo, with an accent alone, and with a
// logo alone. Each response, against the same section of the no-brand business:
//   - with an accent, the head links app.css and then /brand/theme/DA291C.css and
//     nothing else, and the chrome opens with one empty aria-hidden stripe; without
//     one, neither (and no other element carries the stripe's class);
//   - with a logo, exactly one <img>: src /admin/brand/logo/<digest>, width 128 and
//     height 32 (512x128 into the 192x32 slot), and an alt attribute that is PRESENT
//     AND EMPTY (round 1's decision: the name is the visible text beside it; a
//     missing alt and the name written back are both red); the policy is today's
//     plus img-src 'self'; without one, no <img> and today's policy;
//   - the name (escaped) in a paragraph whose class list is exactly the display
//     one, with no colour class -- so it is the page's ink on porcelain, 14.32:1 --
//     and the co-brand line in exactly the wordmark tagline's classes (ink/70,
//     5.70:1); no wordmark;
//   - with the theme link and the header taken out and the wordmark put back, the
//     no-brand business's bytes -- nothing else on the page changed, the primary
//     buttons included (K4).
//
// A fourth business has a logo and an accent the gate refuses today: it gets the
// logo's header without a stripe or a theme link, and one WARN line per request, and
// nothing that business's log lines carry names its brand: not the name (whole, or its
// first eight characters), not the refused accent's hex, not the logo's digest (whole,
// or its first eight digits) -- panelBrand's doc says so and this is where it is
// measured. The three others log nothing about the brand.
func TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection(t *testing.T) {
	assertSectionTableIsUsable(t)
	wordmark := wordmarkHTML(t)
	brands := newFakeBrands()
	b, logs := brandedPanel(t, brands)
	// M10 WL-7: the Account section's brand editor is the subject of its own tests
	// (brandactions_test.go); here every page is the chrome around a body, so the editor
	// region is taken out of the reference and of every answer (withoutBrandEditor:
	// once on the Account section, nowhere else).
	reference := map[string]string{}
	for _, s := range pages.PanelSections {
		reference[s.Href] = withoutBrandEditor(t, s.Href, getSection(t, b, s.Href).body)
	}
	themeLink := `<link rel="stylesheet" href="/brand/theme/DA291C.css">`
	appLink := `<link rel="stylesheet" href="/static/css/app.css">`
	stripe := `<div aria-hidden="true" class="panel-stripe"></div>`
	for _, v := range []struct {
		name                  string
		accent, logo, refused bool
	}{
		{"accent and logo", true, true, false},
		{"accent alone", true, false, false},
		{"logo alone", false, true, false},
		{"logo and an accent the gate refuses today", false, true, true},
	} {
		served := wl8Brand(t, v.accent, v.logo)
		served.AccentRefused = v.refused
		if v.refused {
			// The reader keeps a refused accent out of Accent; the fake hands it over
			// anyway, so a log line that printed b.Accent would show it.
			refusedAccent, err := brand.ParseAccent("808080")
			if err != nil {
				t.Fatal(err)
			}
			served.Accent = refusedAccent
		}
		brands.set(served, nil)
		logs.Reset()
		for _, s := range pages.PanelSections {
			got := getSection(t, b, s.Href)
			where := v.name + ", GET " + s.Href
			if got.status != http.StatusOK {
				t.Fatalf("%s: %d", where, got.status)
			}
			if want := wantSectionPolicy(s.Href, v.logo); got.csp != want {
				t.Errorf("%s: policy %q, want %q", where, got.csp, want)
			}
			wantSheets := []string{"/static/css/app.css"}
			if v.accent {
				wantSheets = append(wantSheets, "/brand/theme/DA291C.css")
				if !strings.Contains(got.body, appLink+themeLink) {
					t.Errorf("%s: the theme link does not follow app.css directly", where)
				}
			}
			if hrefs, inHead := stylesheetsOf(got.body); !slices.Equal(hrefs, wantSheets) || !inHead {
				t.Errorf("%s: stylesheets %v (all in head: %v), want %v", where, hrefs, inHead, wantSheets)
			}
			if n, want := strings.Count(got.body, "panel-stripe"), map[bool]int{true: 1, false: 0}[v.accent]; n != want {
				t.Errorf("%s: %d elements carry the stripe's class, want %d", where, n, want)
			}
			if v.accent && !strings.Contains(got.body, `<header class="flex flex-col gap-3">`+stripe) {
				t.Errorf("%s: the stripe is not the chrome's first element, empty and aria-hidden", where)
			}
			// The chrome with the Account editor's region taken out (M10 WL-7): that region
			// carries the preview's own <img>, the tap screen's header shape, and is
			// measured by TestBrandPreview_IsTheTapScreensOwnComponentsAndCannotSubmit.
			chrome := withoutBrandEditor(t, s.Href, got.body)
			imgs := imgTagRE.FindAllString(chrome, -1)
			switch {
			case !v.logo && len(imgs) != 0:
				t.Errorf("%s: %d <img> without a logo", where, len(imgs))
			case v.logo && len(imgs) != 1:
				t.Errorf("%s: %d <img>, want the logo alone", where, len(imgs))
			case v.logo:
				for name, want := range map[string]string{
					"src": "/admin/brand/logo/" + digestOf(logoPNGA), "width": "128", "height": "32", "alt": "",
				} {
					if got, ok := attr(imgs[0], name); !ok || got != want {
						t.Errorf("%s: the logo's %s is %q (%v), want %q", where, name, got, ok, want)
					}
				}
			}
			if !strings.Contains(got.body, `<p class="font-display text-lg font-bold tracking-tight">`+htmlEscaper.Replace(wl8Name)+"</p>") ||
				!strings.Contains(got.body, `<p class="font-mono text-[10px] uppercase tracking-widest text-ink/70">taptime · punchless</p>`) {
				t.Errorf("%s: the header does not carry the name and the co-brand line in exactly their classes", where)
			}
			if strings.Contains(chrome, wordmark) {
				t.Errorf("%s: the wordmark is still drawn beside the business's header", where)
			}
			header := brandHeadRE.FindAllString(chrome, -1)
			if len(header) != 1 {
				t.Fatalf("%s: %d brand headers", where, len(header))
			}
			back := strings.Replace(strings.Replace(chrome, themeLink, "", 1), header[0], wordmark, 1)
			if back != reference[s.Href] {
				t.Errorf("%s: with the theme link and the header undone, the page is not the no-brand page", where)
			}
		}
		if n := len(brands.callLog()); n != len(pages.PanelSections) {
			t.Errorf("%s: the brand reader was asked %d times for %d requests", v.name, n, len(pages.PanelSections))
		}
		out := logs.String()
		if !v.refused {
			if strings.Contains(out, "brand") || strings.Contains(out, "accent") {
				t.Errorf("%s: a readable brand was logged about:\n%s", v.name, out)
			}
			continue
		}
		warned := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "level=WARN") && strings.Contains(l, "does not pass the legibility gate today") &&
				strings.Contains(l, "tenant_id="+panelTestTenant.String()) {
				warned++
			}
		}
		if warned != len(pages.PanelSections) || strings.Contains(out, "level=ERROR") {
			t.Errorf("%s: %d WARN lines for %d requests (or an ERROR):\n%s", v.name, warned, len(pages.PanelSections), out)
		}
		digest := digestOf(logoPNGA)
		for _, never := range []string{wl8Name, string([]rune(wl8Name)[:8]), "808080", digest, digest[:8]} {
			if strings.Contains(strings.ToLower(out), strings.ToLower(never)) {
				t.Errorf("%s: the log carries %q:\n%s", v.name, never, out)
			}
		}
	}
}

// TestPanelBrand_TheThemeHrefIsTheThemeRoute requests layout.ThemeOf(c).Href() through
// the router with the theme route mounted, for the accents of ADR 0023 §3's table the
// gate passes, black and white: 200, and the body brand.ThemeCSS(c) builds. The zero
// Theme links nothing and has no href.
func TestPanelBrand_TheThemeHrefIsTheThemeRoute(t *testing.T) {
	if (layout.Theme{}).Linked() || (layout.Theme{}).Href() != "" {
		t.Fatal("the zero Theme links something")
	}
	h := httpx.NewRouter(nil, nil, NewBrandTheme())
	for _, hex := range []string{"DA291C", "FFC72C", "1F5C41", "000000", "FFFFFF"} {
		c, err := brand.ParseAccent(hex)
		if err != nil {
			t.Fatal(err)
		}
		th := layout.ThemeOf(c)
		if !th.Linked() || th.Href() != "/brand/theme/"+hex+".css" {
			t.Fatalf("ThemeOf(%s) = linked %v, href %q", hex, th.Linked(), th.Href())
		}
		want, err := brand.ThemeCSS(c)
		if err != nil {
			t.Fatalf("PREMISE: ThemeCSS(%s): %v", hex, err)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, th.Href(), nil))
		res := rec.Result()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK || string(body) != want {
			t.Errorf("GET %s: %d %q, want 200 %q", th.Href(), res.StatusCode, body, want)
		}
	}
}

// TestPanelBrand_TheLogoSrcIsTheLogoRoute mounts the panel and the two logo routes as
// cmd/tappa does, for business A with its logo in the logo reader and in its brand.
// The <img src> a section draws, requested with the same panel cookie, answers 200
// with that logo's bytes; adminLogoHref is the route's path for the digest.
func TestPanelBrand_TheLogoSrcIsTheLogoRoute(t *testing.T) {
	if got := adminLogoHref(digestOf(logoPNGA)); got != panelLogoPath(digestOf(logoPNGA)) {
		t.Fatalf("adminLogoHref = %q, want %q", got, panelLogoPath(digestOf(logoPNGA)))
	}
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		liveEmployee(logoTenantA), &fakeCheckins{}, &fakeAudit{}, tapCfg(), discardLogger())
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	brands := &fakeBrands{brand: wl8Brand(t, true, true)}
	panel := newAdminRouterWithBrands(t, livePanel(logoTenantA), brands, discardLogger())
	logos, err := NewBrandLogos(newFakeLogoReader(), panel, tp, discardLogger())
	if err != nil {
		t.Fatalf("NewBrandLogos: %v", err)
	}
	r := chi.NewRouter()
	tp.Mount(r)
	panel.Mount(r)
	logos.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = panelCookie().Value

	page := getSection(t, b, transactionsHref)
	imgs := imgTagRE.FindAllString(page.body, -1)
	if len(imgs) != 1 {
		t.Fatalf("the section drew %d <img>", len(imgs))
	}
	src, _ := attr(imgs[0], "src")
	rec := b.do(http.MethodGet, src, nil)
	res := rec.Result()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !bytes.Equal(got, logoPNGA) || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("GET %s: %d %q, %d bytes; want 200 image/png and business A's logo",
			src, res.StatusCode, res.Header.Get("Content-Type"), len(got))
	}
}

// TestPanelBrand_TheSignInFamilyStaysTaptime walks a two-business sign-in to the
// picker (AdminChoose) with a fully branded brand reader wired in, and renders a panel
// problem page. What it asserts, and no more: the brand reader is asked 0 times over
// the whole walk; the picker and the problem page carry the wordmark, no stripe, no
// <img>, no theme or logo path, no co-brand line, and adminCSP's literal; the sign-in
// page's policy names no img-src. The sign-in page has a shell and a policy of its own
// (layout.Auth, adminLoginCSP) and is not checked for the wordmark here; the
// password-reset family is not driven by this test.
func TestPanelBrand_TheSignInFamilyStaysTaptime(t *testing.T) {
	v1 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	v2 := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
			{AdminUserID: v1.AdminUserID, TenantID: v1.TenantID, PasswordMatched: true, Active: true},
			{AdminUserID: v2.AdminUserID, TenantID: v2.TenantID, PasswordMatched: true, Active: true},
		}}, nil
	}}
	brands := &fakeBrands{brand: wl8Brand(t, true, true)}
	h := newAdminRouterWithBrands(t, admins, brands, discardLogger())
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)

	wordmark := wordmarkHTML(t)
	login := b.do(http.MethodGet, "/admin/login", nil)
	csrf := csrfFrom(t, htmlOf(t, login))
	if rec := b.do(http.MethodPost, "/admin/login", url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login/choose" {
		t.Fatalf("two verified businesses: %d %q, want 303 to the picker", rec.Code, rec.Header().Get("Location"))
	}
	picker := b.do(http.MethodGet, "/admin/login/choose", nil)
	problem := httptest.NewRecorder()
	h.renderProblem(problem, httptest.NewRequest(http.MethodGet, transactionsHref, nil),
		http.StatusInternalServerError, problemPanelUnavailable)
	for name, rec := range map[string]*httptest.ResponseRecorder{"picker": picker, "panel problem": problem} {
		body := htmlOf(t, rec)
		if !strings.Contains(body, wordmark) {
			t.Errorf("%s: no wordmark", name)
		}
		for _, never := range []string{"<img", "panel-stripe", "/brand/theme/", "/admin/brand/logo/", "taptime · punchless"} {
			if strings.Contains(body, never) {
				t.Errorf("%s carries %q", name, never)
			}
		}
		if got := rec.Result().Header.Get("Content-Security-Policy"); got != wl8AdminPolicy {
			t.Errorf("%s: policy %q, want %q", name, got, wl8AdminPolicy)
		}
	}
	if got := login.Result().Header.Get("Content-Security-Policy"); strings.Contains(got, "img-src") {
		t.Errorf("sign-in page policy %q names img-src", got)
	}
	if n := len(brands.callLog()); n != 0 {
		t.Errorf("the brand reader was asked %d times by the sign-in family; want 0", n)
	}
}

// TestPanelBrand_TheReaderIsRequired: NewAdminAuth refuses a nil brand reader and a
// typed nil one.
func TestPanelBrand_TheReaderIsRequired(t *testing.T) {
	records := newFakeLedger()
	for name, brands := range map[string]panelBrands{"nil": nil, "typed nil": (*fakeBrands)(nil)} {
		_, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
			&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
			newFakeAccount(), brands, newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), discardLogger())
		if err == nil {
			t.Errorf("a %s brand reader was accepted", name)
		}
	}
}

// TestPanelLogoOf_KeepsTheProportionsInsideTheSlot: the listed sizes give the listed
// boxes; and over every stored size the table admits (1-512 by 1-512), the box fits
// in 192x32, is no larger than the stored image, has a side on the slot's edge or is
// the stored size, and its other side is the exact proportion rounded to the nearest
// pixel (or 1 when that rounds to 0). A missing src or a non-positive side draws none.
func TestPanelLogoOf_KeepsTheProportionsInsideTheSlot(t *testing.T) {
	for _, c := range []struct{ w, h, bw, bh int }{
		{512, 128, 128, 32}, {512, 16, 192, 6}, {100, 100, 32, 32}, {20, 20, 20, 20},
		{192, 32, 192, 32}, {193, 32, 192, 32}, {1, 512, 1, 32}, {512, 1, 192, 1},
		{512, 512, 32, 32}, {300, 40, 192, 26}, {48, 33, 47, 32},
	} {
		got := pages.PanelLogoOf("/x", c.w, c.h)
		if got.Src != "/x" || got.Width != c.bw || got.Height != c.bh {
			t.Errorf("PanelLogoOf(%dx%d) = %dx%d (%q), want %dx%d", c.w, c.h, got.Width, got.Height, got.Src, c.bw, c.bh)
		}
	}
	for _, c := range []struct {
		src  string
		w, h int
	}{{"", 10, 10}, {"/x", 0, 10}, {"/x", 10, 0}, {"/x", -1, 10}} {
		if got := pages.PanelLogoOf(c.src, c.w, c.h); got != (pages.PanelLogo{}) {
			t.Errorf("PanelLogoOf(%q, %d, %d) = %+v, want none", c.src, c.w, c.h, got)
		}
	}
	const maxW, maxH = 192, 32
	checked := 0
	for w := 1; w <= 512; w++ {
		for h := 1; h <= 512; h++ {
			got := pages.PanelLogoOf("/x", w, h)
			bw, bh := got.Width, got.Height
			fail := func(why string) {
				t.Fatalf("PanelLogoOf(%dx%d) = %dx%d: %s", w, h, bw, bh, why)
			}
			if bw < 1 || bh < 1 || bw > maxW || bh > maxH || bw > w || bh > h {
				fail("outside the slot or larger than stored")
			}
			switch {
			case w <= maxW && h <= maxH:
				if bw != w || bh != h {
					fail("a logo that fits is not drawn as stored")
				}
			case bw == maxW:
				// width binds: |bh - h*maxW/w| <= 1/2, or bh is the floor of 1
				if d := 2*bh*w - 2*h*maxW; (d > w || d < -w) && !(bh == 1 && 2*h*maxW < w) {
					fail("height is not the rounded proportion")
				}
			case bh == maxH:
				if d := 2*bw*h - 2*w*maxH; (d > h || d < -h) && !(bw == 1 && 2*w*maxH < h) {
					fail("width is not the rounded proportion")
				}
			default:
				fail("neither side is on the slot's edge")
			}
			checked++
		}
	}
	if checked != 512*512 {
		t.Fatalf("checked %d sizes, want %d", checked, 512*512)
	}
}

// panelShellPages derives, from the generated Go of web/templates/pages, the page
// components that render the panel shell: every top-level function whose body calls
// PanelShell or PanelShellWithScript.
func panelShellPages(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "..", "web", "templates", "pages")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), "_templ.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", e.Name(), err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !ast.IsExported(fn.Name.Name) {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && (id.Name == "PanelShell" || id.Name == "PanelShellWithScript") {
						out[fn.Name.Name] = true
					}
				}
				return true
			})
		}
	}
	return out
}

// shellRenderFindings reads one Go source of package handler and lists what breaks the
// rule: a page component of shells, named in the call, may be rendered only through
// renderPanel or renderScripted, and the chrome passed beside it is the PanelChrome of
// the same view value the page is given (`v.PanelChrome, pages.X(v)`). It also returns
// how many conforming calls it saw.
func shellRenderFindings(fset *token.FileSet, name string, src any, shells map[string]bool) (findings []string, ok int, err error) {
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		return nil, 0, err
	}
	ast.Inspect(f, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		for i, arg := range call.Args {
			page, isPage := arg.(*ast.CallExpr)
			if !isPage {
				continue
			}
			ps, isPS := page.Fun.(*ast.SelectorExpr)
			if !isPS || !shells[ps.Sel.Name] {
				continue
			}
			if x, isID := ps.X.(*ast.Ident); !isID || x.Name != "pages" {
				continue
			}
			pos := fset.Position(call.Pos())
			switch sel.Sel.Name {
			case "renderPanel", "renderScripted":
			default:
				findings = append(findings, fmt.Sprintf("%s: pages.%s is rendered through %s, not renderPanel/renderScripted",
					pos, ps.Sel.Name, sel.Sel.Name))
				continue
			}
			viewOf := func(e ast.Expr) string {
				if id, isID := e.(*ast.Ident); isID {
					return id.Name
				}
				return ""
			}
			chrome := ""
			if i > 0 {
				if cs, isCS := call.Args[i-1].(*ast.SelectorExpr); isCS && cs.Sel.Name == "PanelChrome" {
					chrome = viewOf(cs.X)
				}
			}
			page0 := ""
			if len(page.Args) == 1 {
				page0 = viewOf(page.Args[0])
			}
			if chrome == "" || chrome != page0 {
				findings = append(findings, fmt.Sprintf("%s: pages.%s is given %q and its chrome is not that value's PanelChrome",
					pos, ps.Sel.Name, page0))
				continue
			}
			ok++
		}
		return true
	})
	return findings, ok, nil
}

// TestPanelRenders_AShellPageIsRenderedWithItsChromesPolicy is the static half of
// "img-src is named by a response that draws an image": a page drawn in the panel
// shell reaches the wire through renderPanel or renderScripted, whose policy follows
// the chrome's DrawsLogo -- never through render (which says false) or
// renderWithPolicy directly -- and the chrome it is given is its own view's.
//
// THE SHELL PAGES ARE DERIVED (panelShellPages, from the generated Go), not listed, and
// the scan reads every non-test .go file of this package. Its negative control feeds
// it the shapes it exists for. It sees a page component NAMED in the call; a component
// held in a variable and rendered later is not seen (PART III).
func TestPanelRenders_AShellPageIsRenderedWithItsChromesPolicy(t *testing.T) {
	shells := panelShellPages(t)
	for _, want := range []string{"AdminTransactions", "AdminReview", "AdminAccount", "AdminDashboard", "AdminInviteIssued"} {
		if !shells[want] {
			t.Fatalf("PREMISE: the derivation did not find %s among the shell pages %v", want, shells)
		}
	}
	if shells["AdminChoose"] || shells["Problem"] || shells["TransactionDockets"] {
		t.Fatalf("PREMISE: a page outside the shell was derived as a shell page: %v", shells)
	}

	fset := token.NewFileSet()
	for _, c := range []struct {
		name, src string
		bad       bool
	}{
		{"through render", `package p; func f() { a.render(w, r, 200, pages.AdminReview(v)) }`, true},
		{"through renderWithPolicy", `package p; func f() { a.renderWithPolicy(w, r, 200, pages.AdminReview(v), adminCSPFor(true)) }`, true},
		{"another view's chrome", `package p; func f() { a.renderPanel(w, r, 200, other.PanelChrome, pages.AdminReview(v)) }`, true},
		{"a chrome built elsewhere", `package p; func f() { a.renderPanel(w, r, 200, a.chrome(r, tab), pages.AdminReview(v)) }`, true},
		{"the shipped shape", `package p; func f() { a.renderPanel(w, r, 200, v.PanelChrome, pages.AdminReview(v)) }`, false},
		{"the scripted shape", `package p; func f() { a.renderScripted(w, r, 200, v.PanelChrome, pages.AdminTransactions(v)) }`, false},
		{"a page outside the shell", `package p; func f() { a.render(w, r, 200, pages.Problem(v)) }`, false},
	} {
		findings, _, err := shellRenderFindings(fset, c.name+".go", c.src, shells)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if (len(findings) != 0) != c.bad {
			t.Errorf("CONTROL %s: findings %v, want some: %v", c.name, findings, c.bad)
		}
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	conforming, scanned := 0, 0
	var all []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		scanned++
		findings, ok, err := shellRenderFindings(fset, f, nil, shells)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		all = append(all, findings...)
		conforming += ok
	}
	sort.Strings(all)
	for _, f := range all {
		t.Error(f)
	}
	// ANTI-VACUITY: the panel renders its fifteen shell pages from more than twenty
	// places; a scan that saw none read the wrong files.
	if conforming < 20 || scanned < 10 {
		t.Fatalf("saw %d conforming shell renders in %d files; the scan is reading the wrong thing", conforming, scanned)
	}
	t.Logf("%d shell pages derived; %d shell renders in %d files, each through renderPanel/renderScripted with its own chrome",
		len(shells), conforming, scanned)
}

// withoutBrandEditor is body with the Account section's "Your brand" editor (M10 WL-7)
// taken out: the <section id="brand"> element and everything inside it, matched by
// counting nested <section> tags. It requires the region exactly once on the Account
// section and nowhere on any other, so the cut can neither miss the editor nor hide
// anything else. And it requires what it cuts to hold the tap screen's header shape
// ONLY inside the preview block: with that block (previewSpan) taken out too, the
// region draws no wordmark, no "taptime" or "punchless" word, no co-brand line, no
// <header> and no <img> -- so a second header drawn elsewhere in the editor, which the
// chrome's own counts no longer see once the region is cut, is seen here.
func withoutBrandEditor(t *testing.T, href, body string) string {
	t.Helper()
	const open = `<section id="brand"`
	want := 0
	if href == accountHref {
		want = 1
	}
	if n := strings.Count(body, open); n != want {
		t.Fatalf("%s: the brand editor's region is on the page %d times, want %d", href, n, want)
	}
	start := strings.Index(body, open)
	if start < 0 {
		return body
	}
	depth := 0
	for i := start; i < len(body); {
		switch {
		case strings.HasPrefix(body[i:], "<section"):
			depth++
			i += len("<section")
		case strings.HasPrefix(body[i:], "</section>"):
			depth--
			i += len("</section>")
			if depth == 0 {
				brandEditorDrawsNoSecondHeader(t, href, body[start:i])
				return body[:start] + body[i:]
			}
		default:
			i++
		}
	}
	t.Fatalf("%s: the brand editor's region does not close", href)
	return ""
}

// brandEditorDrawsNoSecondHeader is withoutBrandEditor's check on the region it cuts:
// outside the one preview block, nothing of the tap screen's or the chrome's header.
func brandEditorDrawsNoSecondHeader(t *testing.T, href, region string) {
	t.Helper()
	_, start, end, ok := previewSpan(t, region)
	if !ok {
		t.Fatalf("%s: the brand editor has no preview block", href)
	}
	rest := region[:start] + region[end:]
	for _, never := range []string{wordmarkHTML(t), ">taptime<", ">punchless<", "taptime · punchless", "<header", "<img"} {
		if n := strings.Count(rest, never); n != 0 {
			t.Errorf("%s: outside its preview, the brand editor draws %q %d times", href, never, n)
		}
	}
}
