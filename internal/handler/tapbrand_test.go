package handler

// tapbrand_test.go -- M10 WL-9: a business's brand on the tap and result screens,
// against FAKES below the handler and through the mounted router (every response is
// read from rec.Result()). User decisions: D-C (2026-09-24) "Logo + tap butonu tenant
// renginde; sonuç ekranında yalnız logo", K-2a (2026-10-02) an accent and no logo makes
// the header's taptime ink, K-2b (2026-10-02) a logo above a small "taptime ·
// punchless" line. The real-Postgres half is tapbrand_db_test.go.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

const (
	brandTestName   = "Kebab Factory Ltd"
	brandTestAccent = "DA291C"
	brandLogoW      = 240
	brandLogoH      = 60
)

// The two policies, as literals: a change to tapCSP is a change these tests see.
const (
	tapPolicy     = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	tapPolicyLogo = tapPolicy + "; img-src 'self'"
)

var brandLogoSHA = digestOf(logoPNGA)

// testPageBrand is the domain's answer for a business with the named slots set.
func testPageBrand(t *testing.T, logo, accent bool) tenant.PageBrand {
	t.Helper()
	var b tenant.PageBrand
	if logo {
		b.HasLogo, b.Name = true, brandTestName
		b.Logo = tenant.LogoRef{SHA256: brandLogoSHA, MIME: "image/png", Width: brandLogoW, Height: brandLogoH}
	}
	if accent {
		c, err := brand.ParseAccent(brandTestAccent)
		if err != nil {
			t.Fatalf("accent: %v", err)
		}
		b.Accent, b.HasAccent = c, true
	}
	return b
}

func brandedFacts(t *testing.T, logo, accent bool) tenant.TapPageFacts {
	f := okFacts()
	f.Brand = testPageBrand(t, logo, accent)
	return f
}

// tapAnswer drives GET /t through the mounted router and masks the signed context.
func tapAnswer(t *testing.T, dir *fakeDirectory) answer {
	t.Helper()
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, dir, fixedSessions())
	a := ask(t, h, http.MethodGet, tapURL(), sessionCookie())
	if a.status == http.StatusOK {
		a.body = maskTapContext(t, "GET /t", a.body)
	}
	return a
}

const (
	appCSSLink      = `<link rel="stylesheet" href="/static/css/app.css">`
	themeLink       = `<link rel="stylesheet" href="/brand/theme/` + brandTestAccent + `.css">`
	tapWordmarkHTML = `<header class="flex items-baseline justify-between"><span class="font-display text-lg font-bold tracking-tight text-tappa-green">taptime</span> <span class="font-mono text-[10px] uppercase tracking-widest text-ink/70">punchless</span></header>`
	inkMarkHTML     = `<header class="flex items-baseline justify-between"><span class="font-display text-lg font-bold tracking-tight text-ink">taptime</span> <span class="font-mono text-[10px] uppercase tracking-widest text-ink/70">punchless</span></header>`
)

// logoHeaderHTML is K-2b's header: the logo in the 24 px slot, its stored box as width
// and height, the business's name as alt, and the co-brand line under it.
func logoHeaderHTML(sha string, w, h int, alt string) string {
	return `<header class="flex flex-col items-start gap-1"><img src="/t/logo/` + sha + `" width="` + strconv.Itoa(w) + `" height="` + strconv.Itoa(h) +
		`" alt="` + alt + `" class="h-6 w-auto max-w-[50%] object-contain object-left"> <span class="font-mono text-[10px] uppercase tracking-widest text-ink/70">taptime · punchless</span></header>`
}

var headerRE = regexp.MustCompile(`<header[^>]*>.*?</header>`)

// withoutBrandSlots removes the two slots -- the theme link and the header -- so the
// rest of two pages can be compared byte for byte.
func withoutBrandSlots(body []byte) string {
	s := strings.Replace(string(body), themeLink, "", 1)
	return headerRE.ReplaceAllString(s, "<header/>")
}

// TestTapPage_TheBrandFillsTheTwoSlotsAndNothingElse is D-C, K-2a and K-2b on the tap
// screen, for the four shapes a business's brand can have. For each: the header is
// exactly the decided markup; the theme stylesheet is linked (right after app.css)
// exactly when there is an accent; the page outside those two slots is byte for byte
// the unbranded page's; the one new text is the logo's alt (the co-brand line carries
// the two words the wordmark already had); the screen still has one button, its word
// "Tap", submitting the form; and the policy names img-src exactly when the logo is
// drawn.
func TestTapPage_TheBrandFillsTheTwoSlotsAndNothingElse(t *testing.T) {
	plain := tapAnswer(t, &fakeDirectory{facts: okFacts()})
	if plain.status != http.StatusOK {
		t.Fatalf("unbranded GET /t = %d", plain.status)
	}
	const unbrandedText = "Tap — Taptime taptime punchless Tapping Hello Maria Borg St Julians {{TAP-CONTEXT}} Tap"
	const logoText = "Tap — Taptime " + brandTestName + " taptime · punchless Tapping Hello Maria Borg St Julians {{TAP-CONTEXT}} Tap"
	logoHeader := logoHeaderHTML(brandLogoSHA, brandLogoW, brandLogoH, brandTestName)
	cases := []struct {
		name         string
		logo, accent bool
		header       string
		theme        bool
		text, policy string
	}{
		{"no brand", false, false, tapWordmarkHTML, false, unbrandedText, tapPolicy},
		{"an accent and no logo (K-2a)", false, true, inkMarkHTML, true, unbrandedText, tapPolicy},
		{"a logo and no accent (K-2b)", true, false, logoHeader, false, logoText, tapPolicyLogo},
		{"a logo and an accent (D-C, K-2b)", true, true, logoHeader, true, logoText, tapPolicyLogo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := tapAnswer(t, &fakeDirectory{facts: brandedFacts(t, c.logo, c.accent)})
			body := string(a.body)
			if a.status != http.StatusOK {
				t.Fatalf("status %d", a.status)
			}
			if got := headerRE.FindAllString(body, -1); len(got) != 1 || got[0] != c.header {
				t.Errorf("header:\n got %q\nwant %q", got, c.header)
			}
			if n := strings.Count(body, "/brand/theme/"); n != map[bool]int{true: 1}[c.theme] {
				t.Errorf("theme references = %d, want %v", n, c.theme)
			}
			if c.theme && !strings.Contains(body, appCSSLink+themeLink) {
				t.Errorf("the theme stylesheet is not the link right after app.css (it overrides the :root defaults by order)")
			}
			if n := strings.Count(body, "<img"); n != map[bool]int{true: 1}[c.logo] {
				t.Errorf("<img count = %d, want logo=%v", n, c.logo)
			}
			if got, want := withoutBrandSlots(a.body), withoutBrandSlots(plain.body); got != want {
				t.Errorf("outside the two slots the page differs from the unbranded one:\n got %s\nwant %s", got, want)
			}
			if got := screenText(t, body); got != c.text {
				t.Errorf("screen text:\n got %q\nwant %q", got, c.text)
			}
			if n := strings.Count(body, "<button"); n != 1 {
				t.Errorf("%d buttons, want 1 (§9)", n)
			}
			if !strings.Contains(body, `<button type="submit" class="tap-button" data-tap-button>Tap</button>`) {
				t.Errorf("the one button is not the tap form's submit with the word Tap")
			}
			if got := a.header.Get("Content-Security-Policy"); got != c.policy {
				t.Errorf("policy = %q, want %q", got, c.policy)
			}
		})
	}
}

// TestTapPage_TheLogoCarriesItsStoredBox: the <img>'s width and height are the stored
// box for each shape a stored logo can have, so the browser reserves its place before
// the bytes arrive (CDP: layout shift 0, WL-9 card).
func TestTapPage_TheLogoCarriesItsStoredBox(t *testing.T) {
	for _, box := range [][2]int{{512, 128}, {128, 512}, {512, 16}, {1, 1}, {512, 512}} {
		f := okFacts()
		f.Brand = tenant.PageBrand{HasLogo: true, Name: brandTestName,
			Logo: tenant.LogoRef{SHA256: brandLogoSHA, MIME: "image/jpeg", Width: box[0], Height: box[1]}}
		a := tapAnswer(t, &fakeDirectory{facts: f})
		want := logoHeaderHTML(brandLogoSHA, box[0], box[1], brandTestName)
		if got := headerRE.FindString(string(a.body)); got != want {
			t.Errorf("%dx%d: header\n got %q\nwant %q", box[0], box[1], got, want)
		}
	}
}

// TestTapPage_AnotherBusinesssPlaqueShowsNoBrand is ADR 0023 §2's mismatch rule at the
// handler: when the directory answers ErrForeignLocation the page is the unbranded
// foreign page byte for byte -- even if the directory also handed back a brand.
func TestTapPage_AnotherBusinesssPlaqueShowsNoBrand(t *testing.T) {
	foreign := tenant.TapPageFacts{EmployeeName: "Maria Borg"}
	plain := tapAnswer(t, &fakeDirectory{facts: foreign, err: tenant.ErrForeignLocation})
	foreign.Brand = testPageBrand(t, true, true)
	a := tapAnswer(t, &fakeDirectory{facts: foreign, err: tenant.ErrForeignLocation})
	if a.status != http.StatusOK {
		t.Fatalf("status %d", a.status)
	}
	if strings.Contains(string(a.body), "/t/logo/") || strings.Contains(string(a.body), "/brand/theme/") {
		t.Fatalf("another business's plaque drew a brand:\n%s", a.body)
	}
	if !bytes.Equal(a.body, plain.body) || a.header.Get("Content-Security-Policy") != tapPolicy {
		t.Fatalf("the page differs from the unbranded foreign page:\n got %s\nwant %s", a.body, plain.body)
	}
}

// capturingTap is NewTap with a logger whose output the test reads.
func capturingTap(t *testing.T, pv *fakePreviewer, dir *fakeDirectory, svc *fakeCheckins) (http.Handler, *Tap, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	tp, err := NewTap(pv, dir, fixedSessions(), svc, noActivation{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(&buf, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	return mountTap(tp), tp, &buf
}

func mountTap(tp *Tap) http.Handler {
	r := chi.NewRouter()
	tp.Mount(r)
	return r
}

// TestTapPage_ABrandReadFailureRendersTaptimesPage is §4.6 / ADR 0024 §7: the brand
// could not be read (a database error, a logo row only partly described -- both reach
// the handler as ErrBrandUnread), so the page is the unbranded page byte for byte and
// one ERROR line says so. The facts' brand is set to prove the handler does not use it.
func TestTapPage_ABrandReadFailureRendersTaptimesPage(t *testing.T) {
	plain := tapAnswer(t, &fakeDirectory{facts: okFacts()})
	unread := fmtErr(tenant.ErrBrandUnread, errors.New("the stored logo is only partly described"))
	h, _, logs := capturingTap(t, &fakePreviewer{preview: okPreview(true)},
		&fakeDirectory{facts: brandedFacts(t, true, true), err: unread}, &fakeCheckins{})
	a := ask(t, h, http.MethodGet, tapURL(), sessionCookie())
	if a.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: a brand never costs the page", a.status)
	}
	a.body = maskTapContext(t, "GET /t", a.body)
	if !bytes.Equal(a.body, plain.body) || a.header.Get("Content-Security-Policy") != tapPolicy {
		t.Fatalf("the page is not the unbranded page:\n got %s\nwant %s", a.body, plain.body)
	}
	if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "partly described") {
		t.Fatalf("want one ERROR line carrying the cause, got:\n%s", logs.String())
	}
}

func fmtErr(sentinel, cause error) error { return fmt.Errorf("%w: %w", sentinel, cause) }

// --- the result screen -------------------------------------------------------------

// resultAnswer posts one tap whose context names tagTenant and wall, and returns the
// confirmation screen as the client receives it.
func resultAnswer(t *testing.T, dir *fakeDirectory, res checkin.Result, tagTenant, wall uuid.UUID) answer {
	t.Helper()
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, dir, fixedSessions(), &fakeCheckins{result: res}, noActivation{},
		&fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	h := mountTap(tp)
	form := url.Values{"ctx": {mintedContext(t, tp, tapFixedSessionID, tapContext{
		UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true, TagTenantID: tagTenant, LocationID: wall,
	})}}
	return readAnswer(t, postForm(t, h, form, sessionCookie()))
}

func readAnswer(t *testing.T, rec *httptest.ResponseRecorder) answer {
	t.Helper()
	res := rec.Result()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return answer{status: res.StatusCode, header: res.Header, body: b}
}

func okResult() checkin.Result {
	return checkin.Result{Outcome: checkin.OutcomeRecorded, Decision: withNote(decisionOf("ok", "in", false), noteIPMatched),
		LocationName: "St Julians", Timezone: "Europe/Malta", BusinessType: "restaurant"}
}

// TestResultScreen_DrawsTheLogoAndNoAccent is D-C on the confirmation screen: a
// business with a logo AND an accent gets the logo header (K-2b) and no theme link --
// the accent the directory returned is not drawn -- and the screen outside the header
// is the unbranded screen's, byte for byte. The read is under the session's tenant.
func TestResultScreen_DrawsTheLogoAndNoAccent(t *testing.T) {
	plainDir := &fakeDirectory{facts: okFacts()}
	plain := resultAnswer(t, plainDir, okResult(), testTenant, tapLocation)
	dir := &fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, true, true)}
	a := resultAnswer(t, dir, okResult(), testTenant, tapLocation)
	body := string(a.body)
	if a.status != http.StatusOK {
		t.Fatalf("status %d", a.status)
	}
	if got, want := headerRE.FindString(body), logoHeaderHTML(brandLogoSHA, brandLogoW, brandLogoH, brandTestName); got != want {
		t.Errorf("header\n got %q\nwant %q", got, want)
	}
	if strings.Contains(body, "/brand/theme/") || strings.Count(body, `rel="stylesheet"`) != 1 {
		t.Errorf("the confirmation screen links a theme (D-C: no accent here)")
	}
	if got, want := withoutBrandSlots(a.body), withoutBrandSlots(plain.body); got != want {
		t.Errorf("outside the header the screen differs:\n got %s\nwant %s", got, want)
	}
	if a.header.Get("Content-Security-Policy") != tapPolicyLogo || plain.header.Get("Content-Security-Policy") != tapPolicy {
		t.Errorf("policies: logo %q, plain %q", a.header.Get("Content-Security-Policy"), plain.header.Get("Content-Security-Policy"))
	}
	if len(dir.resultTenants) != 1 || dir.resultTenants[0] != testTenant {
		t.Errorf("the brand was read under %v, want once under the session's %s", dir.resultTenants, testTenant)
	}
}

// TestResultScreen_NoBrandWhereThePlaqueIsNotThisBusinesss is the mismatch rule on the
// confirmation screen (ADR 0023 §2): the context's plaque on another business's wall,
// or on no wall, draws the unbranded screen without reading the brand; another
// business's plaque as the product answers it (sys:tenant-mismatch, 403) draws the
// problem screen with no brand. Control: the session's own plaque draws the logo.
func TestResultScreen_NoBrandWhereThePlaqueIsNotThisBusinesss(t *testing.T) {
	plain := resultAnswer(t, &fakeDirectory{facts: okFacts()}, okResult(), testTenant, tapLocation)
	for _, c := range []struct {
		name       string
		tagTenant  uuid.UUID
		wall       uuid.UUID
		outcome    checkin.Outcome
		wantStatus int
	}{
		{"another business's plaque, recorded", tapTagTenant, tapLocation, checkin.OutcomeRecorded, http.StatusOK},
		{"a plaque on no wall", testTenant, uuid.Nil, checkin.OutcomeRecorded, http.StatusOK},
		{"another business's plaque, sys:tenant-mismatch", tapTagTenant, tapLocation, checkin.OutcomeForeignTenant, http.StatusForbidden},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := &fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, true, true)}
			res := okResult()
			res.Outcome = c.outcome
			a := resultAnswer(t, dir, res, c.tagTenant, c.wall)
			if a.status != c.wantStatus {
				t.Fatalf("status %d, want %d", a.status, c.wantStatus)
			}
			if strings.Contains(string(a.body), "/t/logo/") || strings.Contains(string(a.body), "/brand/theme/") || strings.Contains(string(a.body), "<img") {
				t.Fatalf("a brand was drawn:\n%s", a.body)
			}
			if a.header.Get("Content-Security-Policy") != tapPolicy {
				t.Fatalf("policy %q", a.header.Get("Content-Security-Policy"))
			}
			if c.outcome == checkin.OutcomeRecorded && !bytes.Equal(a.body, plain.body) {
				t.Fatalf("not the unbranded screen:\n got %s\nwant %s", a.body, plain.body)
			}
			if len(dir.resultTenants) != 0 {
				t.Fatalf("the brand was read %d time(s) for a plaque that is not this business's", len(dir.resultTenants))
			}
		})
	}
	t.Run("control: the session's own plaque", func(t *testing.T) {
		dir := &fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, true, false)}
		a := resultAnswer(t, dir, okResult(), testTenant, tapLocation)
		if !strings.Contains(string(a.body), `<img src="/t/logo/`+brandLogoSHA+`"`) {
			t.Fatalf("the business's own plaque drew no logo:\n%s", a.body)
		}
	})
}

// TestResultScreen_ABrandReadFailureCostsOnlyTheLogo: the read after the record fails;
// the record's screen is the unbranded screen byte for byte and one ERROR line names it.
func TestResultScreen_ABrandReadFailureCostsOnlyTheLogo(t *testing.T) {
	plain := resultAnswer(t, &fakeDirectory{facts: okFacts()}, okResult(), testTenant, tapLocation)
	dir := &fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, true, true),
		resultBrandErr: fmtErr(tenant.ErrBrandUnread, errors.New("connection reset by peer"))}
	_, tp, logs := capturingTap(t, &fakePreviewer{preview: okPreview(true)}, dir, &fakeCheckins{result: okResult()})
	h := mountTap(tp)
	form := url.Values{"ctx": {mintedContext(t, tp, tapFixedSessionID, tapContext{
		UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true, TagTenantID: testTenant, LocationID: tapLocation,
	})}}
	a := readAnswer(t, postForm(t, h, form, sessionCookie()))
	if a.status != http.StatusOK || !bytes.Equal(a.body, plain.body) || a.header.Get("Content-Security-Policy") != tapPolicy {
		t.Fatalf("status %d; not the unbranded screen:\n got %s\nwant %s", a.status, a.body, plain.body)
	}
	if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "connection reset") {
		t.Fatalf("want one ERROR line carrying the cause, got:\n%s", logs.String())
	}
}

// --- the views and the split ---------------------------------------------------------

// TestTapView_FieldCountIsTheSpec is TestResultView_FieldCountIsTheSpec's twin: the
// brand reached the tap screen as a second parameter, not as a fourth field.
func TestTapView_FieldCountIsTheSpec(t *testing.T) {
	if got := reflect.TypeOf(pages.TapView{}).NumField(); got != 3 {
		t.Fatalf("TapView has %d fields, want 3 (§9: ask before adding one)", got)
	}
}

// TestScreens_TakeTheBrandAsAnExplicitParameter pins the two signatures: the tap screen
// takes the view and a layout.Brand, the result screen the view and a layout.Logo --
// no accent can reach it (D-C).
func TestScreens_TakeTheBrandAsAnExplicitParameter(t *testing.T) {
	component := reflect.TypeOf((*templ.Component)(nil)).Elem()
	for _, c := range []struct {
		name string
		fn   any
		in   []reflect.Type
	}{
		{"pages.Tap", pages.Tap, []reflect.Type{reflect.TypeOf(pages.TapView{}), reflect.TypeOf(layout.Brand{})}},
		{"pages.Result", pages.Result, []reflect.Type{reflect.TypeOf(pages.ResultView{}), reflect.TypeOf(layout.Logo{})}},
	} {
		ft := reflect.TypeOf(c.fn)
		var in []reflect.Type
		for i := 0; i < ft.NumIn(); i++ {
			in = append(in, ft.In(i))
		}
		if !reflect.DeepEqual(in, c.in) || ft.NumOut() != 1 || ft.Out(0) != component {
			t.Errorf("%s is %v, want func%v templ.Component", c.name, ft, c.in)
		}
	}
}

func renderString(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

// TestTapButtonFace_ThePreviewFaceCannotSubmit is the split WL-7 depends on (ADR 0023
// §2): the tap screen's docket and button are components the Account preview can call.
// The heading renders the tap page's own docket bytes; the submitting face is the tap
// page's button; the preview face -- and any value outside the two -- is the same class
// and word as a type="button" with no data attribute for tap.js, and no form.
func TestTapButtonFace_ThePreviewFaceCannotSubmit(t *testing.T) {
	page := string(tapAnswer(t, &fakeDirectory{facts: okFacts()}).body)
	if docket := renderString(t, pages.TapHeading("Maria Borg", "St Julians")); !strings.Contains(page, docket) {
		t.Errorf("TapHeading's docket %q is not the tap page's", docket)
	}
	submit := renderString(t, pages.TapButtonFace(pages.TapButtonSubmits))
	if submit != `<button type="submit" class="tap-button" data-tap-button>Tap</button>` || !strings.Contains(page, submit) {
		t.Errorf("the submitting face %q is not the tap page's button", submit)
	}
	for _, use := range []pages.TapButtonUse{pages.TapButtonPreview, pages.TapButtonUse(7)} {
		face := renderString(t, pages.TapButtonFace(use))
		if face != `<button type="button" class="tap-button">Tap</button>` {
			t.Errorf("use %d: face %q, want a type=button with the same class and word, nothing else", use, face)
		}
	}
}

// TestLayoutBrand_RefusesAnyOtherShape: layout writes only the image URL it builds --
// a 64-digit lower-case digest under /t/logo/ with a box in 1..512; every other shape
// is the zero Logo: nothing drawn, no img-src. The theme comes only from a brand.Color
// (layout.ThemeOf, M10 WL-8): the zero Theme links nothing, and a colour links its
// one route spelling.
func TestLayoutBrand_RefusesAnyOtherShape(t *testing.T) {
	good := brandLogoSHA
	for name, l := range map[string]layout.Logo{
		"upper-case digest": layout.TapLogo(strings.ToUpper(good), 10, 10, "x"),
		"short digest":      layout.TapLogo(good[:63], 10, 10, "x"),
		"path in digest":    layout.TapLogo("../"+good[3:], 10, 10, "x"),
		"zero width":        layout.TapLogo(good, 0, 10, "x"),
		"zero height":       layout.TapLogo(good, 10, 0, "x"),
		"width over 512":    layout.TapLogo(good, 513, 10, "x"),
		"height over 512":   layout.TapLogo(good, 10, 513, "x"),
	} {
		if l.Drawn() || l != (layout.Logo{}) {
			t.Errorf("%s: drawn", name)
		}
	}
	if !layout.TapLogo(good, 512, 1, "").Drawn() {
		t.Error("control: a valid logo is not drawn")
	}
	if href := layout.TapBrand(layout.Logo{}, layout.Theme{}).ThemeHref(); href != "" {
		t.Errorf("the zero Theme linked %q", href)
	}
	c, err := brand.ParseAccent(brandTestAccent)
	if err != nil {
		t.Fatalf("accent: %v", err)
	}
	if href := layout.TapBrand(layout.Logo{}, layout.ThemeOf(c)).ThemeHref(); href != "/brand/theme/DA291C.css" {
		t.Errorf("control: accent linked %q", href)
	}
}

// --- what a tap costs, on the real page -------------------------------------------

// TestTapPage_ALogoTapIsTwoChargedRequestsWarmAndThreeCold re-measures ADR 0024's
// arithmetic (WL-6 hand-off) on the REAL page: the router cmd/tappa builds
// (httpx.NewRouter with the tap surface, the logo routes and the theme route), the
// production budgets, and a browser model that fetches every same-origin reference
// the page and the confirmation screen name, keeping what the server marks immutable.
// A tap is GET /t, its references, POST /api/checkin, and the confirmation screen's
// references. What a tap COSTS is measured, not counted: after the tap, the session's
// remaining GET /t before the first 429 is the budget minus what the tap charged.
//
// Cold (no logo cached): 3 -- the page, the logo, the button. Warm (the logo cached
// from an earlier tap): 2. The theme stylesheet and /static are outside the tap
// group and charge nothing; the confirmation screen names the page's logo URL, which
// the phone already holds.
func TestTapPage_ALogoTapIsTwoChargedRequestsWarmAndThreeCold(t *testing.T) {
	const addr = "198.51.100.9:4000"
	logoURL := tapLogoPath(digestOf(logoPNGA))
	tapOnce := func(t *testing.T, h http.Handler, cache map[string]bool) []string {
		t.Helper()
		var fetched []string
		fetchRefs := func(body []byte) {
			for _, m := range refRE.FindAllStringSubmatch(string(body), -1) {
				ref := strings.ReplaceAll(m[2], "&amp;", "&")
				if cache[ref] || !strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "//") {
					continue
				}
				a := askFrom(t, h, addr, http.MethodGet, ref, nil, employeeCookie())
				if a.status != http.StatusOK {
					t.Fatalf("GET %s = %d", ref, a.status)
				}
				fetched = append(fetched, ref)
				if strings.Contains(a.header.Get("Cache-Control"), "immutable") {
					cache[ref] = true
				}
			}
		}
		page := askFrom(t, h, addr, http.MethodGet, tapURL(), nil, employeeCookie())
		if page.status != http.StatusOK {
			t.Fatalf("GET /t = %d", page.status)
		}
		fetched = append(fetched, "/t")
		fetchRefs(page.body)
		req := httptest.NewRequest(http.MethodPost, "/api/checkin",
			strings.NewReader(url.Values{"ctx": {contextFieldOf(t, string(page.body))}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = addr
		req.AddCookie(employeeCookie())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		result := readAnswer(t, rec)
		if result.status != http.StatusOK || !strings.Contains(string(result.body), `src="`+logoURL+`"`) {
			t.Fatalf("POST /api/checkin = %d; the confirmation screen must draw the logo:\n%s", result.status, result.body)
		}
		fetched = append(fetched, "/api/checkin")
		fetchRefs(result.body)
		return fetched
	}
	charged := func(t *testing.T, warm bool) (int, []string) {
		t.Helper()
		sess := liveEmployee(logoTenantA)
		pv := &fakePreviewer{preview: okPreview(true)}
		pv.preview.TenantID = logoTenantA
		f := okFacts()
		f.Brand = testPageBrand(t, true, true)
		tp, err := NewTap(pv, &fakeDirectory{facts: f, resultBrand: testPageBrand(t, true, true)}, sess,
			&fakeCheckins{result: okResult()}, noActivation{}, &fakeAudit{}, tapCfg(), discardLogger())
		if err != nil {
			t.Fatalf("NewTap: %v", err)
		}
		_, panel, _ := logoSurfaces(t, newFakeLogoReader(), sess, livePanel(logoTenantA))
		logos, err := NewBrandLogos(newFakeLogoReader(), panel, tp, discardLogger())
		if err != nil {
			t.Fatalf("NewBrandLogos: %v", err)
		}
		h := httpx.NewRouter(nil, nil, tp, logos, NewBrandTheme())
		cache := map[string]bool{}
		if warm {
			cache[logoURL] = true
		}
		fetched := tapOnce(t, h, cache)
		left := 0
		for left <= httpx.TapSessionLimit() {
			if askFrom(t, h, addr, http.MethodGet, tapURL(), nil, employeeCookie()).status != http.StatusOK {
				break
			}
			left++
		}
		return httpx.TapSessionLimit() - left, fetched
	}
	cold, coldFetched := charged(t, false)
	warm, warmFetched := charged(t, true)
	t.Logf("cold tap fetched %v -> charged %d; warm tap fetched %v -> charged %d", coldFetched, cold, warmFetched, warm)
	if cold != 3 || warm != 2 {
		t.Fatalf("a tap charged %d cold and %d warm, want 3 and 2 (ADR 0024 §5: page + logo when cold, + the button)", cold, warm)
	}
	for _, want := range []string{"/static/css/app.css", "/brand/theme/" + brandTestAccent + ".css", "/static/js/tap.js", logoURL} {
		if !slicesContains(coldFetched, want) {
			t.Fatalf("control: the cold tap never fetched %s (%v), so the count above is not of the real page", want, coldFetched)
		}
	}
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestTapPage_AnAccentTheGateRefusesTodayKeepsTheLogo is the panel chrome's rule on the
// tap screen (accentOf, one read-side gate since the merge with M10 WL-8): the
// directory reports AccentRefused. Beside a logo, the page draws the logo header, no
// theme stylesheet, the logo's policy; without a logo it is the unbranded page byte
// for byte (the wordmark stays green -- K-2a needs an accent). Each logs one WARN line
// and no ERROR.
func TestTapPage_AnAccentTheGateRefusesTodayKeepsTheLogo(t *testing.T) {
	plain := tapAnswer(t, &fakeDirectory{facts: okFacts()})
	for _, withLogo := range []bool{true, false} {
		f := okFacts()
		f.Brand = testPageBrand(t, withLogo, false)
		f.Brand.AccentRefused = true
		h, _, logs := capturingTap(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: f}, &fakeCheckins{})
		a := ask(t, h, http.MethodGet, tapURL(), sessionCookie())
		if a.status != http.StatusOK {
			t.Fatalf("logo=%v: status %d", withLogo, a.status)
		}
		a.body = maskTapContext(t, "GET /t", a.body)
		body := string(a.body)
		if strings.Contains(body, "/brand/theme/") {
			t.Errorf("logo=%v: a refused accent was linked", withLogo)
		}
		if withLogo {
			if got, want := headerRE.FindString(body), logoHeaderHTML(brandLogoSHA, brandLogoW, brandLogoH, brandTestName); got != want {
				t.Errorf("header\n got %q\nwant %q", got, want)
			}
			if a.header.Get("Content-Security-Policy") != tapPolicyLogo {
				t.Errorf("policy %q", a.header.Get("Content-Security-Policy"))
			}
		} else if !bytes.Equal(a.body, plain.body) || a.header.Get("Content-Security-Policy") != tapPolicy {
			t.Errorf("no logo: not the unbranded page:\n got %s\nwant %s", a.body, plain.body)
		}
		if strings.Count(logs.String(), "level=WARN") != 1 || strings.Contains(logs.String(), "level=ERROR") {
			t.Errorf("logo=%v: want one WARN line and no ERROR, got:\n%s", withLogo, logs.String())
		}
	}
}

// lateCommit is a checkinRecorder whose record commits `after` into the request -- the
// way a saturated pool delays the record's own transaction, the condition that also
// stalls the brand read after it (WL-9 round 3, B1). It ignores ctx on purpose: what it
// models is a COMMIT that landed. A record that gave up on the deadline is the other
// branch (renderCheckinFailure), which never reaches the confirmation.
type lateCommit struct {
	*fakeCheckins
	after time.Duration
}

func (l lateCommit) Record(ctx context.Context, req checkin.Request) (checkin.Result, error) {
	time.Sleep(l.after)
	return l.fakeCheckins.Record(ctx, req)
}

// confirmationShape is one way a recorded tap meets the request deadline: budget is
// that deadline, set as production sets it (chi's middleware.Timeout behind
// httpx.RequestID); commitAt is when the record commits; wait is the brand read's
// bound; stall makes the read wait for its context.
type confirmationShape struct {
	name     string
	budget   time.Duration
	commitAt time.Duration
	wait     time.Duration
	stall    bool
}

// postConfirmation drives one tap of a business with a logo through that chain into the
// mounted tap routes, logging through httpx.WithRequestID, and returns the answer, the
// log, the record and brand-read fakes and how long the request took.
func postConfirmation(t *testing.T, s confirmationShape, requestID string) (answer, string, *fakeCheckins, *fakeDirectory, time.Duration) {
	t.Helper()
	svc := &fakeCheckins{result: okResult()}
	dir := &fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, true, true), resultBrandStall: s.stall}
	var logs bytes.Buffer
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, dir, fixedSessions(), lateCommit{svc, s.commitAt}, noActivation{},
		&fakeAudit{}, tapCfg(), slog.New(httpx.WithRequestID(slog.NewTextHandler(&logs, nil))))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	tp.brandWait = s.wait
	h := httpx.RequestID(middleware.Timeout(s.budget)(mountTap(tp)))
	form := url.Values{"ctx": {mintedContext(t, tp, tapFixedSessionID, tapContext{
		UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true, TagTenantID: testTenant, LocationID: tapLocation,
	})}}
	req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(httpx.RequestIDHeader, requestID)
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(sessionCookie())
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, req)
	took := time.Since(start)
	return readAnswer(t, rec), logs.String(), svc, dir, took
}

func errorLines(logs string) []string {
	var out []string
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, "level=ERROR") {
			out = append(out, l)
		}
	}
	return out
}

// TestResultScreen_AStalledBrandReadStillSaysAllDone is S1 of WL-9's security review and
// the window its round 3 found (B1): the brand read after the record stalls (a saturated
// pool, a stuck read) and the answer is the unbranded confirmation byte for byte -- "All
// done" included -- in each of the three ways the record can meet the request deadline,
// set by chi's Timeout as in production: budget left past the read's bound (the bound
// shortened to 50 ms); LESS budget left than the bound (the record commits 1 s into a
// 1.5 s request, the bound is production's resultBrandWait); and the deadline already
// gone when the record commits (an 800 ms request, the commit at 1 s). Each answer is a
// 200 with the whole screen and the unbranded policy, the record and the brand were
// asked for once, exactly one ERROR line names the timeout and carries the request's
// request_id, and the request took the commit plus the read's OWN bound -- not less
// (the request deadline did not end the read) and not more than a second over (the
// bound held).
func TestResultScreen_AStalledBrandReadStillSaysAllDone(t *testing.T) {
	plain := resultAnswer(t, &fakeDirectory{facts: okFacts()}, okResult(), testTenant, tapLocation)
	shapes := []confirmationShape{
		{name: "budget left past the bound", budget: 1500 * time.Millisecond, wait: 50 * time.Millisecond, stall: true},
		{name: "less budget left than the bound", budget: 1500 * time.Millisecond, commitAt: time.Second, wait: resultBrandWait, stall: true},
		{name: "the deadline gone at the commit", budget: 800 * time.Millisecond, commitAt: time.Second, wait: resultBrandWait, stall: true},
	}
	for i, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			id := fmt.Sprintf("wl9-r3-stall-%d", i)
			a, logs, svc, dir, took := postConfirmation(t, s, id)
			if a.status != http.StatusOK || !strings.Contains(string(a.body), "All done — you can close this page.") {
				t.Fatalf("status %d, %d bytes: the recorded tap's confirmation is missing:\n%s", a.status, len(a.body), a.body)
			}
			if !bytes.Equal(a.body, plain.body) || a.header.Get("Content-Security-Policy") != tapPolicy {
				t.Fatalf("not the unbranded confirmation:\n got %s\nwant %s", a.body, plain.body)
			}
			if len(svc.calls) != 1 || len(dir.resultTenants) != 1 {
				t.Fatalf("record asked %d time(s), brand %d time(s); want 1 and 1", len(svc.calls), len(dir.resultTenants))
			}
			if errs := errorLines(logs); len(errs) != 1 || !strings.Contains(errs[0], "deadline exceeded") ||
				!strings.Contains(errs[0], "request_id="+id) {
				t.Fatalf("want one ERROR line naming the timeout and carrying request_id=%s, got:\n%s", id, logs)
			}
			if want := s.commitAt + s.wait; took < want || took > want+time.Second {
				t.Fatalf("the request took %v; want the commit plus the read's own bound (%v), and at most a second more",
					took.Round(time.Millisecond), want)
			}
		})
	}
}

// TestResultScreen_ARecordCommittedAtTheDeadlineKeepsItsLogo: the record commits after
// the request deadline (chi's Timeout at 800 ms, the commit at 1 s) and the brand read
// is healthy, so the business's logo is drawn -- the confirmation with the logo byte for
// byte, its policy, no ERROR line, and no wait. The read runs on a context the request's
// deadline does not end; one derived from the request's context would find it already
// ended and drop the logo of a tap that counted (WL-9 round 3).
func TestResultScreen_ARecordCommittedAtTheDeadlineKeepsItsLogo(t *testing.T) {
	branded := resultAnswer(t, &fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, true, true)},
		okResult(), testTenant, tapLocation)
	if !strings.Contains(string(branded.body), "/t/logo/"+brandLogoSHA) {
		t.Fatalf("fixture: the branded confirmation draws no logo:\n%s", branded.body)
	}
	a, logs, svc, dir, took := postConfirmation(t, confirmationShape{
		budget: 800 * time.Millisecond, commitAt: time.Second, wait: resultBrandWait,
	}, "wl9-r3-healthy")
	if a.status != http.StatusOK || !bytes.Equal(a.body, branded.body) || a.header.Get("Content-Security-Policy") != tapPolicyLogo {
		t.Fatalf("status %d, policy %q: not the confirmation with the logo:\n got %s\nwant %s",
			a.status, a.header.Get("Content-Security-Policy"), a.body, branded.body)
	}
	if len(svc.calls) != 1 || len(dir.resultTenants) != 1 || strings.Contains(logs, "level=ERROR") {
		t.Fatalf("record %d, brand %d, log:\n%s; want 1, 1 and no ERROR", len(svc.calls), len(dir.resultTenants), logs)
	}
	if took > 2*time.Second {
		t.Fatalf("the request took %v; a healthy read waits for nothing", took.Round(time.Millisecond))
	}
}

// TestNewTap_BoundsTheResultBrandRead: the production Tap carries resultBrandWait; the
// constant is the ADR's 2 s (ADR 0023, WL-9 note, Karar 2); and it stays at least an
// order of magnitude under httpx.RequestTimeout, the router's deadline, which the
// confirmation may outlive by this bound. A router timeout brought down towards the
// bound, or a bound raised towards the router's, fails here (WL-9 round 3, N3).
func TestNewTap_BoundsTheResultBrandRead(t *testing.T) {
	_, tp := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	if tp.brandWait != resultBrandWait {
		t.Fatalf("brandWait = %v; want resultBrandWait (%v)", tp.brandWait, resultBrandWait)
	}
	if resultBrandWait != 2*time.Second {
		t.Fatalf("resultBrandWait = %v; ADR 0023 (WL-9 note, Karar 2) says 2 s -- change the ADR and its reasoning first", resultBrandWait)
	}
	if resultBrandWait*10 > httpx.RequestTimeout {
		t.Fatalf("resultBrandWait %v is more than a tenth of httpx.RequestTimeout %v", resultBrandWait, httpx.RequestTimeout)
	}
}
