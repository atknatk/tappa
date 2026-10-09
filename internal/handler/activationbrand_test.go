package handler

// activationbrand_test.go -- M10 WL-13: a business's brand on the activation family and
// the wizard's logo route, against FAKES below the handler and through the mounted
// router (every response read from rec.Result()). User decisions of 2026-10-09: (1) the
// wizard's four steps, "Activation complete" and "already set up" take the tap screen's
// header and accent; (2) the logo only for a business VIES verified -- the domain read
// drops it, so here a business "not verified" is the brand that read returns: the
// accent alone; (3) the accent fills steps 1-3's primary button and the wizard's other
// green marks turn ink. The real-Postgres half is activationbrand_db_test.go, and the
// VIES gate itself is internal/domain/tenant's activationbrand tests.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/layout"
)

// fakeActivationBrands keeps tenant.BrandReader's activation contract: ActivationBrand
// answers the brand set for a business (the zero PageBrand when none is set), and
// ActivationLogo serves a stored logo only for a business marked verified -- the shape of
// GetTenantActivationLogo's WHERE -- and answers tenant.ErrLogoNotFound itself for every
// other digest.
type fakeActivationBrands struct {
	mu       sync.Mutex
	brands   map[uuid.UUID]tenant.PageBrand
	brandErr error
	logos    map[uuid.UUID]map[string]tenant.StoredLogo
	verified map[uuid.UUID]bool
	logoErr  error

	brandCalls []uuid.UUID
	logoCalls  []logoCall
}

func (f *fakeActivationBrands) ActivationBrand(_ context.Context, tenantID uuid.UUID) (tenant.PageBrand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.brandCalls = append(f.brandCalls, tenantID)
	if f.brandErr != nil {
		return tenant.PageBrand{}, f.brandErr
	}
	return f.brands[tenantID], nil
}

func (f *fakeActivationBrands) ActivationLogo(_ context.Context, tenantID uuid.UUID, digest string) (tenant.StoredLogo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logoCalls = append(f.logoCalls, logoCall{tenantID, digest})
	if f.logoErr != nil {
		return tenant.StoredLogo{}, f.logoErr
	}
	if !f.verified[tenantID] {
		return tenant.StoredLogo{}, tenant.ErrLogoNotFound
	}
	if l, ok := f.logos[tenantID][digest]; ok {
		return l, nil
	}
	return tenant.StoredLogo{}, tenant.ErrLogoNotFound
}

func (f *fakeActivationBrands) calls() ([]uuid.UUID, []logoCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.brandCalls...), append([]logoCall(nil), f.logoCalls...)
}

// brandsFor is a reader whose businesses carry the given brands.
func brandsFor(b map[uuid.UUID]tenant.PageBrand) *fakeActivationBrands {
	return &fakeActivationBrands{brands: b}
}

// dbBrandReader is the production reader over the dev Postgres, for the DB harnesses
// that build the activation flow.
func dbBrandReader(t *testing.T, data *db.DB) *tenant.BrandReader {
	t.Helper()
	r, err := tenant.NewBrandReader(data)
	if err != nil {
		t.Fatalf("NewBrandReader: %v", err)
	}
	return r
}

// wl13DoneSession is the session the "Activation complete" marker names.
var wl13DoneSession = uuid.MustParse("66666666-6666-4666-8666-666666666666")

func wl13LiveSession() *http.Cookie {
	return &http.Cookie{Name: session.CookieName, Value: "FAKEsessionFAKEsessionFAKEsessionFAKEsess12"}
}

// activationScreen is one of the six screens WL-13 brands.
type activationScreen struct {
	name    string
	target  string
	cookies []*http.Cookie
	// session: the screen holds a live session, so its logo is the tap route's.
	session bool
	// buttons is how many elements carry the tap button's class: the accent's slot.
	buttons int
}

func activationScreens() []activationScreen {
	live := wl13LiveSession()
	marker := &http.Cookie{Name: activatedCookieName, Value: wl13DoneSession.String()}
	return []activationScreen{
		{"step 1, welcome", "/activate", []*http.Cookie{codeCookie()}, false, 1},
		{"step 2, privacy", "/activate?step=2", []*http.Cookie{codeCookie()}, false, 1},
		{"step 3, get ready", "/activate?step=3", []*http.Cookie{pendingCookie()}, false, 1},
		{"step 4, tap", "/activate?step=4", []*http.Cookie{pendingCookie()}, false, 0},
		{"activation complete", ActivationCompletePath, []*http.Cookie{live, marker}, true, 0},
		{"already set up", "/activate", []*http.Cookie{live}, true, 0},
	}
}

// activationHandler is the activation router with brands and an optional log sink; the
// session layer resolves wl13DoneSession for testTenant, so both session screens render.
func activationHandler(t *testing.T, brands *fakeActivationBrands, logw io.Writer) http.Handler {
	t.Helper()
	return newHandlerWith(t, &fakeInvites{}, &fakeSessions{sessionID: wl13DoneSession}, &fakeAudit{},
		handlerOpts{brands: brands, log: logw})
}

func askScreen(t *testing.T, h http.Handler, s activationScreen) answer {
	t.Helper()
	a := ask(t, h, http.MethodGet, s.target, s.cookies...)
	if a.status != http.StatusOK {
		t.Fatalf("%s: status %d\n%s", s.name, a.status, a.body)
	}
	return a
}

// activationLogoHeaderHTML is K-2b's header with the logo served from prefix.
func activationLogoHeaderHTML(prefix, sha string, w, h int, alt string) string {
	return strings.Replace(logoHeaderHTML(sha, w, h, alt), `src="/t/logo/`, `src="`+prefix, 1)
}

func logoPrefixOf(s activationScreen) string {
	if s.session {
		return "/t/logo/"
	}
	return "/activate/logo/"
}

// The policies, as literals: a change to activationCSP is a change these tests see.
const (
	activationPolicy     = tapPolicy + "; connect-src 'self'"
	activationPolicyLogo = activationPolicy + "; img-src 'self'"
)

// The four ink marks and their Taptime spellings: undoInk maps a page drawn beside an
// accent back onto the one drawn without, so the rest can be compared byte for byte.
const (
	inkSegment      = `class="wizard-segment wizard-segment--done bg-ink"`
	greenSegment    = `class="wizard-segment wizard-segment--done"`
	inkIndex        = `class="wizard-index text-ink"`
	greenIndex      = `class="wizard-index"`
	inkConsent      = `class="wizard-consent has-[:checked]:border-ink has-[:checked]:bg-ink/5"`
	greenConsent    = `class="wizard-consent"`
	inkConsentBox   = `class="mt-0.5 h-6 w-6 shrink-0 accent-ink"`
	greenConsentBox = `class="mt-0.5 h-6 w-6 shrink-0 accent-tappa-green"`
)

var undoInk = strings.NewReplacer(inkSegment, greenSegment, inkIndex, greenIndex,
	inkConsent, greenConsent, inkConsentBox, greenConsentBox)

// TestActivationScreens_ABrandedBusinessGetsTheTapScreensHeaderAndTheme is decision 1 on
// the six screens, for a business with a logo (VIES verified) and an accent: the header
// is exactly the tap screen's K-2b header, its logo from the wizard's route on steps 1-4
// and from the tap route on the two session screens; the theme stylesheet is linked once,
// right after app.css; one <img>; the policy names img-src; the accent's slot -- the tap
// button's class -- is on steps 1-3 only, so step 4 still has no primary button; and
// outside the header, the theme link and the ink marks the page is the unbranded page
// byte for byte.
func TestActivationScreens_ABrandedBusinessGetsTheTapScreensHeaderAndTheme(t *testing.T) {
	plain := activationHandler(t, &fakeActivationBrands{}, nil)
	branded := activationHandler(t, brandsFor(map[uuid.UUID]tenant.PageBrand{testTenant: testPageBrand(t, true, true)}), nil)
	for _, s := range activationScreens() {
		t.Run(s.name, func(t *testing.T) {
			a, p := askScreen(t, branded, s), askScreen(t, plain, s)
			body := string(a.body)
			want := activationLogoHeaderHTML(logoPrefixOf(s), brandLogoSHA, brandLogoW, brandLogoH, brandTestName)
			if got := headerRE.FindAllString(body, -1); len(got) != 1 || got[0] != want {
				t.Errorf("header:\n got %q\nwant %q", got, want)
			}
			if n := strings.Count(body, "/brand/theme/"); n != 1 || !strings.Contains(body, appCSSLink+themeLink) {
				t.Errorf("theme references = %d, or not the link right after app.css", n)
			}
			if n := strings.Count(body, "<img"); n != 1 {
				t.Errorf("<img count = %d, want 1", n)
			}
			if got := a.header.Get("Content-Security-Policy"); got != activationPolicyLogo {
				t.Errorf("policy = %q, want %q", got, activationPolicyLogo)
			}
			if n := strings.Count(body, `class="tap-button`); n != s.buttons {
				t.Errorf("tap buttons = %d, want %d", n, s.buttons)
			}
			if got, want := withoutBrandSlots([]byte(undoInk.Replace(body))), withoutBrandSlots(p.body); got != want {
				t.Errorf("outside the header, the theme link and the ink marks the page differs from the unbranded one:\n got %s\nwant %s", got, want)
			}
		})
	}
}

// TestActivationScreens_AnAccentWithoutALogoGetsTheInkWordmark: the brand the domain
// read returns for a business VIES did not verify (its logo dropped) or one that set no
// logo -- the accent alone. Every screen: K-2a's ink wordmark, the theme link, no <img>,
// no img-src.
func TestActivationScreens_AnAccentWithoutALogoGetsTheInkWordmark(t *testing.T) {
	h := activationHandler(t, brandsFor(map[uuid.UUID]tenant.PageBrand{testTenant: testPageBrand(t, false, true)}), nil)
	for _, s := range activationScreens() {
		a := askScreen(t, h, s)
		body := string(a.body)
		if got := headerRE.FindAllString(body, -1); len(got) != 1 || got[0] != inkMarkHTML {
			t.Errorf("%s: header\n got %q\nwant %q", s.name, got, inkMarkHTML)
		}
		if !strings.Contains(body, appCSSLink+themeLink) || strings.Contains(body, "<img") {
			t.Errorf("%s: want the theme link and no <img>", s.name)
		}
		if got := a.header.Get("Content-Security-Policy"); got != activationPolicy {
			t.Errorf("%s: policy = %q, want %q", s.name, got, activationPolicy)
		}
	}
}

// TestActivationWizard_TheOtherGreenMarksTurnInkOnlyBesideAnAccent is decision 3: with an
// accent the progress bar's done segments, the 01/02/03 numbers and the ticked consent
// box are ink; without one -- no brand, or a logo alone -- they are Taptime's green, and
// no ink mark is written.
func TestActivationWizard_TheOtherGreenMarksTurnInkOnlyBesideAnAccent(t *testing.T) {
	for _, c := range []struct {
		name         string
		logo, accent bool
	}{{"no brand", false, false}, {"a logo alone", true, false}, {"an accent alone", false, true}, {"a logo and an accent", true, true}} {
		h := activationHandler(t, brandsFor(map[uuid.UUID]tenant.PageBrand{testTenant: testPageBrand(t, c.logo, c.accent)}), nil)
		for _, s := range activationScreens()[:4] {
			body := string(askScreen(t, h, s).body)
			step, _ := strconv.Atoi(strings.TrimPrefix(strings.SplitN(s.name, ",", 2)[0], "step "))
			ink, green := map[bool]int{true: step}, map[bool]int{false: step}
			if got := strings.Count(body, inkSegment); got != ink[c.accent] {
				t.Errorf("%s, %s: %d ink done segment(s), want %d", c.name, s.name, got, ink[c.accent])
			}
			if got := strings.Count(body, greenSegment); got != green[c.accent] {
				t.Errorf("%s, %s: %d green done segment(s), want %d", c.name, s.name, got, green[c.accent])
			}
			indexes := map[int]int{1: 3, 3: 2}[step]
			if got, want := strings.Count(body, inkIndex), map[bool]int{true: indexes}[c.accent]; got != want {
				t.Errorf("%s, %s: %d ink number(s), want %d", c.name, s.name, got, want)
			}
			if got, want := strings.Count(body, greenIndex), map[bool]int{false: indexes}[c.accent]; got != want {
				t.Errorf("%s, %s: %d green number(s), want %d", c.name, s.name, got, want)
			}
			consent := map[int]int{2: 1}[step]
			if got, want := strings.Count(body, inkConsent)+strings.Count(body, inkConsentBox), map[bool]int{true: 2 * consent}[c.accent]; got != want {
				t.Errorf("%s, %s: %d ink consent mark(s), want %d", c.name, s.name, got, want)
			}
			if got, want := strings.Count(body, greenConsent)+strings.Count(body, greenConsentBox), map[bool]int{false: 2 * consent}[c.accent]; got != want {
				t.Errorf("%s, %s: %d green consent mark(s), want %d", c.name, s.name, got, want)
			}
		}
	}
}

// TestActivationWizard_TheInkMarksCompileToInkAfterTheGreen reads the compiled app.css:
// each ink mark is a rule of its own whose colour is the palette's ink, and it comes
// AFTER the component rule that paints the same property green, so it wins by order at
// equal specificity (Tailwind's utilities layer follows its components layer). Without
// the stylesheet the classes above would be names and nothing more. SKIPs when app.css
// has not been built (`make css`): a SKIP is not a pass.
func TestActivationWizard_TheInkMarksCompileToInkAfterTheGreen(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "css", "app.css"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("web/static/css/app.css is not built (make css)")
	}
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, c := range []struct {
		ink, inkDecl, green string
	}{
		{`.bg-ink{`, "background-color:rgb(21 34 25", `.wizard-segment--done{`},
		{`.text-ink{`, "color:rgb(21 34 25", `.wizard-index{`},
		{`.accent-ink{`, "accent-color:#152219", `.wizard-consent{`},
		{`.has-\[\:checked\]\:border-ink:has(:checked){`, "border-color:rgb(21 34 25", `.wizard-consent:has(:checked){`},
		{`.has-\[\:checked\]\:bg-ink\/5:has(:checked){`, "background-color:rgba(21,34,25,.05)", `.wizard-consent:has(:checked){`},
	} {
		i, g := strings.Index(css, c.ink), strings.Index(css, c.green)
		if i < 0 || g < 0 {
			t.Errorf("app.css lacks %q (at %d) or %q (at %d)", c.ink, i, c.green, g)
			continue
		}
		rule := css[i : i+strings.Index(css[i:], "}")]
		if !strings.Contains(rule, c.inkDecl) {
			t.Errorf("%s is %q, want it to carry %q", c.ink, rule, c.inkDecl)
		}
		if i < g {
			t.Errorf("%s comes before %s; it would lose to the green by order", c.ink, c.green)
		}
	}
}

// TestActivationPlaque_StaysTaptimesOnABrandedPage: the waiting screen's plaque -- rings
// and "taptime" -- is the PHYSICAL plaque, which is Taptime's (ADR 0023 §9 item 4), so it
// is byte-identical on a branded business's step 4 and on Taptime's own.
func TestActivationPlaque_StaysTaptimesOnABrandedPage(t *testing.T) {
	plaqueRE := regexp.MustCompile(`<div class="plaque-wait".*?<span class="plaque-wait-phone"></span></div>`)
	step4 := activationScreens()[3]
	plain := plaqueRE.FindString(string(askScreen(t, activationHandler(t, &fakeActivationBrands{}, nil), step4).body))
	branded := plaqueRE.FindString(string(askScreen(t, activationHandler(t,
		brandsFor(map[uuid.UUID]tenant.PageBrand{testTenant: testPageBrand(t, true, true)}), nil), step4).body))
	if plain == "" || !strings.Contains(plain, `text-tappa-green">taptime</span>`) || !strings.Contains(plain, "plaque-wait-ring") {
		t.Fatalf("PREMISE: the unbranded plaque is not the green Taptime drawing: %q", plain)
	}
	if branded != plain {
		t.Errorf("the branded plaque differs:\n got %s\nwant %s", branded, plain)
	}
}

// TestActivationFailureScreens_StayTaptimesForABrandedInvitation: every failure screen of
// the flow and the "phone already in use" confirmation, reached with an invitation of a
// business that has a logo and an accent, are byte-identical (status, policy, body) to
// the same request against a business with no brand, carry no logo route, no theme and
// no img-src -- and no brand is even read on their way.
func TestActivationFailureScreens_StayTaptimesForABrandedInvitation(t *testing.T) {
	expired := func(err error) *fakeInvites {
		return &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return okContext("invited"), err }}
	}
	crossSite := func(h http.Handler) answer {
		req := httptest.NewRequest(http.MethodGet, "/activate?code="+fakeCode, nil)
		req.RemoteAddr = "203.0.113.9:5000"
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: victimSession})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		res := rec.Result()
		b, _ := io.ReadAll(res.Body)
		return answer{res.StatusCode, res.Header, b}
	}
	get := func(target string, cookies ...*http.Cookie) func(http.Handler) answer {
		return func(h http.Handler) answer { return ask(t, h, http.MethodGet, target, cookies...) }
	}
	cases := []struct {
		name     string
		inv      func() *fakeInvites
		sess     func() *fakeSessions
		verifier func() *fakeVerifier
		do       func(http.Handler) answer
		status   int
	}{
		{"no link", nil, nil, nil, get("/activate"), http.StatusOK},
		{"finish setup in this browser", nil, nil, nil, get("/activate?from=tap"), http.StatusOK},
		{"signed out", nil, nil, nil, get("/activate?from=signedout"), http.StatusOK},
		{"expired invitation", func() *fakeInvites { return expired(invite.ErrCodeExpired) }, nil, nil, get("/activate", codeCookie()), http.StatusBadRequest},
		{"spent invitation, from the link", func() *fakeInvites { return expired(invite.ErrCodeUsed) }, nil, nil, get("/activate?code=" + fakeCode), http.StatusBadRequest},
		{"cancelled invitation", func() *fakeInvites { return expired(invite.ErrCodeCancelled) }, nil, nil, get("/activate", codeCookie()), http.StatusBadRequest},
		{"employee not activatable", func() *fakeInvites { return expired(invite.ErrNotActivatable) }, nil, nil, get("/activate", codeCookie()), http.StatusBadRequest},
		{"unknown code", func() *fakeInvites {
			return &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return invite.Context{}, invite.ErrUnknownCode }}
		}, nil, nil, get("/activate", codeCookie()), http.StatusBadRequest},
		{"server error", func() *fakeInvites {
			return &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return okContext("invited"), errors.New("connection reset") }}
		}, nil, nil, get("/activate", codeCookie()), http.StatusInternalServerError},
		{"too many attempts", func() *fakeInvites { return expired(invite.ErrCodeExpired) }, nil, nil, func(h http.Handler) answer {
			for range inviteFailureLimit {
				ask(t, h, http.MethodGet, "/activate", codeCookie())
			}
			return ask(t, h, http.MethodGet, "/activate", codeCookie())
		}, http.StatusTooManyRequests},
		{"phone already in use", func() *fakeInvites { inv, _ := victimHolds(t); return inv },
			func() *fakeSessions { _, s := victimHolds(t); return s }, nil, crossSite, http.StatusOK},
		{"tap: an unknown plaque", nil, nil, func() *fakeVerifier {
			return &fakeVerifier{verify: func(sun.Params) (sun.Result, error) { return sun.Result{}, sun.ErrUnknownTag }}
		}, get(activationTapURL, pendingCookie()), http.StatusBadRequest},
		{"tap: no touch (QR)", nil, nil, nil, get(activationQRURL, pendingCookie()), http.StatusBadRequest},
		{"tap: consent not recorded", func() *fakeInvites {
			return &fakeInvites{activate: func(invite.Code) (invite.Activation, error) {
				return invite.Activation{Context: okContext("invited")}, invite.ErrConsentMissing
			}}
		}, nil, nil, get(activationTapURL, pendingCookie()), http.StatusConflict},
	}
	full := map[uuid.UUID]tenant.PageBrand{
		testTenant:                 testPageBrand(t, true, true),
		attackerContext().TenantID: testPageBrand(t, true, true),
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			build := func(b *fakeActivationBrands) http.Handler {
				inv, sess, v := &fakeInvites{}, &fakeSessions{}, &fakeVerifier{}
				if c.inv != nil {
					inv = c.inv()
				}
				if c.sess != nil {
					sess = c.sess()
				}
				if c.verifier != nil {
					v = c.verifier()
				}
				return newHandlerWith(t, inv, sess, &fakeAudit{}, handlerOpts{verifier: v, brands: b})
			}
			brands := brandsFor(full)
			got, want := c.do(build(brands)), c.do(build(&fakeActivationBrands{}))
			if got.status != c.status {
				t.Fatalf("status %d, want %d\n%s", got.status, c.status, got.body)
			}
			if got.status != want.status || !bytes.Equal(got.body, want.body) ||
				got.header.Get("Content-Security-Policy") != want.header.Get("Content-Security-Policy") {
				t.Errorf("differs from the unbranded answer:\n got %d %s\nwant %d %s", got.status, got.body, want.status, want.body)
			}
			for _, banned := range []string{"/activate/logo/", "/t/logo/", "/brand/theme/", "<img", "bg-ink", "text-ink\"", "accent-ink"} {
				if bytes.Contains(got.body, []byte(banned)) {
					t.Errorf("the body carries %q", banned)
				}
			}
			if p := got.header.Get("Content-Security-Policy"); p != activationPolicy {
				t.Errorf("policy = %q, want %q", p, activationPolicy)
			}
			if reads, _ := brands.calls(); len(reads) != 0 {
				t.Errorf("a failure path read the brand of %v", reads)
			}
		})
	}
}

// TestActivationScreens_ABrandReadFailureDrawsTaptimesPage is §4.6 on the six screens:
// the brand read fails, the screen is the unbranded screen byte for byte (status, policy,
// body), and the log carries exactly ONE error line naming the business -- and no code,
// no binding, no colour, no digest.
func TestActivationScreens_ABrandReadFailureDrawsTaptimesPage(t *testing.T) {
	var logs bytes.Buffer
	broken := &fakeActivationBrands{brandErr: errors.New("connection reset"), brands: map[uuid.UUID]tenant.PageBrand{testTenant: testPageBrand(t, true, true)}}
	h := activationHandler(t, broken, &logs)
	plain := activationHandler(t, &fakeActivationBrands{}, nil)
	for _, s := range activationScreens() {
		logs.Reset()
		got, want := askScreen(t, h, s), askScreen(t, plain, s)
		if !bytes.Equal(got.body, want.body) || got.header.Get("Content-Security-Policy") != want.header.Get("Content-Security-Policy") {
			t.Errorf("%s: differs from the unbranded screen", s.name)
		}
		lines := errorLines(logs.String())
		if len(lines) != 1 || !strings.Contains(lines[0], "reading the business's brand failed") ||
			!strings.Contains(lines[0], "tenant_id="+testTenant.String()) {
			t.Errorf("%s: error lines %q, want one naming the business", s.name, lines)
		}
		for _, secret := range []string{fakeCode, fakeBinding, fakeCSRF, brandTestAccent, brandLogoSHA} {
			if strings.Contains(logs.String(), secret) {
				t.Errorf("%s: the log carries %q", s.name, secret)
			}
		}
	}
}

// TestActivationScreens_AnAccentTheGateRefusesTodayKeepsTheLogo: a stored accent the
// legibility gate refuses today is drawn nowhere -- no theme link, no ink mark -- and the
// logo stays; one WARN line names the business (the tap screen's rule).
func TestActivationScreens_AnAccentTheGateRefusesTodayKeepsTheLogo(t *testing.T) {
	var logs bytes.Buffer
	b := testPageBrand(t, true, false)
	b.AccentRefused = true
	h := activationHandler(t, brandsFor(map[uuid.UUID]tenant.PageBrand{testTenant: b}), &logs)
	for _, s := range activationScreens() {
		logs.Reset()
		body := string(askScreen(t, h, s).body)
		want := activationLogoHeaderHTML(logoPrefixOf(s), brandLogoSHA, brandLogoW, brandLogoH, brandTestName)
		if headerRE.FindString(body) != want || strings.Contains(body, "/brand/theme/") || strings.Contains(body, "bg-ink") {
			t.Errorf("%s: want the logo header, no theme and no ink mark", s.name)
		}
		if n := strings.Count(logs.String(), "level=WARN"); n != 1 || !strings.Contains(logs.String(), "tenant_id="+testTenant.String()) {
			t.Errorf("%s: %d WARN line(s), want one naming the business:\n%s", s.name, n, logs.String())
		}
	}
}

// TestActivationScreens_TheBrandIsTheBusinessThePageNames: the wizard on a phone that
// another business's employee is signed in on is drawn in the INVITATION's brand (the
// business the wizard names), and "already set up" in the SESSION's business's brand.
// Each screen reads exactly one business's brand.
func TestActivationScreens_TheBrandIsTheBusinessThePageNames(t *testing.T) {
	other := attackerContext().TenantID
	otherBrand := tenant.PageBrand{HasLogo: true, Name: "Probe Beta Ltd",
		Logo: tenant.LogoRef{SHA256: digestOf(logoJPEGB), MIME: "image/jpeg", Width: 100, Height: 50}}
	brands := brandsFor(map[uuid.UUID]tenant.PageBrand{testTenant: testPageBrand(t, true, true), other: otherBrand})
	holder := &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{ID: uuid.New(), TenantID: other, EmployeeID: uuid.New()}, nil
	}}
	h := newHandlerWith(t, &fakeInvites{}, holder, &fakeAudit{}, handlerOpts{brands: brands})

	wizard := string(ask(t, h, http.MethodGet, "/activate?step=2", codeCookie(), wl13LiveSession()).body)
	if !strings.Contains(wizard, "This phone belongs to someone else") {
		t.Fatal("PREMISE: the wizard does not see the other business's session")
	}
	if want := activationLogoHeaderHTML("/activate/logo/", brandLogoSHA, brandLogoW, brandLogoH, brandTestName); headerRE.FindString(wizard) != want {
		t.Errorf("the wizard's header is not the invitation's business's:\n%s", headerRE.FindString(wizard))
	}
	if reads, _ := brands.calls(); len(reads) != 1 || reads[0] != testTenant {
		t.Errorf("the wizard read the brands of %v, want only the invitation's business", reads)
	}

	brands.brandCalls = nil
	already := string(ask(t, h, http.MethodGet, "/activate", wl13LiveSession()).body)
	if want := activationLogoHeaderHTML("/t/logo/", digestOf(logoJPEGB), 100, 50, "Probe Beta Ltd"); headerRE.FindString(already) != want {
		t.Errorf("\"already set up\" is not drawn in the session's business's brand:\n%s", headerRE.FindString(already))
	}
	if reads, _ := brands.calls(); len(reads) != 1 || reads[0] != other {
		t.Errorf("\"already set up\" read the brands of %v, want only the session's business", reads)
	}
}

// wcagLuminance is WCAG 2.x relative luminance of an sRGB triple, computed here from the
// definition rather than through internal/brand, so a mistake there is not repeated.
func wcagLuminance(rgb [3]float64) float64 {
	var l [3]float64
	for i, c := range rgb {
		c /= 255
		if c <= 0.04045 {
			l[i] = c / 12.92
		} else {
			l[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
}

func wcagContrast(a, b [3]float64) float64 {
	la, lb := wcagLuminance(a), wcagLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

var themeVarRE = regexp.MustCompile(`--(brand-[a-z-]+):([^;}]+)`)

// TestActivationWizard_TheAccentButtonTextClearsAA measures the accent's slot on the
// branded wizard: the page's theme link is fetched through the real theme route, and on
// that body the button text (--brand-on-accent) on the fill (--brand-accent) is at least
// 4.5:1; where the fill is under 3:1 on porcelain the button carries the ink edge. The
// colours are the ADR 0023 §3 table's and a few more, light and dark.
func TestActivationWizard_TheAccentButtonTextClearsAA(t *testing.T) {
	theme := chi.NewRouter()
	NewBrandTheme().Mount(theme)
	porcelain := [3]float64{0xED, 0xF0, 0xEA}
	hrefRE := regexp.MustCompile(`href="(/brand/theme/[0-9A-F]{6}\.css)"`)
	for _, hex := range []string{"DA291C", "FFC72C", "D98E2B", "1F5C41", "BE3D2A", "757575", "0057B8", "F5F5F5"} {
		c, err := brand.ParseAccent(hex)
		if err != nil {
			t.Fatal(err)
		}
		h := activationHandler(t, brandsFor(map[uuid.UUID]tenant.PageBrand{testTenant: {Accent: c, HasAccent: true}}), nil)
		body := string(askScreen(t, h, activationScreens()[1]).body)
		m := hrefRE.FindStringSubmatch(body)
		if m == nil || !strings.Contains(body, `<button type="submit" class="tap-button">Agree and continue</button>`) {
			t.Fatalf("%s: step 2 links no theme or has no accent slot", hex)
		}
		res := ask(t, theme, http.MethodGet, m[1])
		if res.status != http.StatusOK {
			t.Fatalf("%s: the theme route answered %d for the page's own link", hex, res.status)
		}
		vars := map[string][3]float64{}
		edge := ""
		for _, v := range themeVarRE.FindAllStringSubmatch(string(res.body), -1) {
			if v[1] == "brand-edge" {
				edge = v[2]
				continue
			}
			var rgb [3]float64
			for i, f := range strings.Fields(v[2]) {
				n, err := strconv.Atoi(f)
				if err != nil || i > 2 {
					t.Fatalf("%s: %s is %q", hex, v[1], v[2])
				}
				rgb[i] = float64(n)
			}
			vars[v[1]] = rgb
		}
		fill, text := vars["brand-accent"], vars["brand-on-accent"]
		if fill != [3]float64{float64(c.R), float64(c.G), float64(c.B)} {
			t.Errorf("%s: the theme's fill is %v", hex, fill)
		}
		if r := wcagContrast(fill, text); r < 4.5 {
			t.Errorf("%s: button text %v on %v is %.2f:1, under 4.5:1", hex, text, fill, r)
		}
		if wcagContrast(fill, porcelain) < 3 && edge != "21 34 25" {
			t.Errorf("%s: the fill is under 3:1 on porcelain and the edge is %q, want ink", hex, edge)
		}
	}
}

// TestActivationPage_AZeroBrandIsPageByteForByte: the activation family's shell with no
// brand is layout.Page, byte for byte, for the same children.
func TestActivationPage_AZeroBrandIsPageByteForByte(t *testing.T) {
	child := templ.Raw(`<p>child</p>`)
	render := func(c templ.Component) string {
		var b bytes.Buffer
		if err := c.Render(templ.WithChildren(context.Background(), child), &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if got, want := render(layout.ActivationPage("A page — Taptime", layout.Brand{})), render(layout.Page("A page — Taptime")); got != want {
		t.Errorf("ActivationPage with a zero Brand:\n got %s\nwant %s", got, want)
	}
}

// TestActivationCSP_ALogoWidensByImgSrcAlone pins activationCSPFor against LITERALS.
func TestActivationCSP_ALogoWidensByImgSrcAlone(t *testing.T) {
	if got := activationCSPFor(false); got != activationPolicy {
		t.Errorf("activationCSPFor(false) = %q, want %q", got, activationPolicy)
	}
	if got := activationCSPFor(true); got != activationPolicyLogo {
		t.Errorf("activationCSPFor(true) = %q, want %q", got, activationPolicyLogo)
	}
}

func TestNewActivation_RefusesANilBrandReader(t *testing.T) {
	cfg := tapCfg()
	var typedNil *fakeActivationBrands
	for name, b := range map[string]activationBrands{"nil": nil, "typed nil": typedNil} {
		if a, err := NewActivation(&fakeInvites{}, &fakeSessions{}, &fakeVerifier{}, &fakeAudit{}, b, cfg, discardLogger()); err == nil || a != nil {
			t.Errorf("%s brand reader: got (%v, %v), want a refusal", name, a, err)
		}
	}
}

// ---------------------------------------------------------------------------------
// GET /activate/logo/{sha}
// ---------------------------------------------------------------------------------

func activationLogoPath(digest string) string { return "/activate/logo/" + digest }

// logoBrands stores A's PNG for testTenant and B's JPEG for the probe business, both
// verified unless the caller says otherwise.
func logoBrands() *fakeActivationBrands {
	other := attackerContext().TenantID
	return &fakeActivationBrands{
		logos: map[uuid.UUID]map[string]tenant.StoredLogo{
			testTenant: {digestOf(logoPNGA): {Data: logoPNGA, MIME: "image/png"}},
			other:      {digestOf(logoJPEGB): {Data: logoJPEGB, MIME: "image/jpeg"}},
		},
		verified: map[uuid.UUID]bool{testTenant: true, other: true},
	}
}

// countingInvites is fakeInvites with a count of Lookup calls.
type countingInvites struct {
	*fakeInvites
	mu      sync.Mutex
	lookups int
}

func (c *countingInvites) Lookup(ctx context.Context, code invite.Code) (invite.Context, error) {
	c.mu.Lock()
	c.lookups++
	c.mu.Unlock()
	return c.fakeInvites.Lookup(ctx, code)
}

func (c *countingInvites) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lookups
}

func logoActivation(t *testing.T, inv inviteManager, brands *fakeActivationBrands, rec *fakeAudit) http.Handler {
	t.Helper()
	sess := &fakeSessions{}
	sess.tok = sess.token(t)
	a, err := NewActivation(inv, sess, &fakeVerifier{}, rec, brands, tapCfg(), discardLogger())
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}
	r := chi.NewRouter()
	a.Mount(r)
	return r
}

func TestActivationLogo_AVerifiedBusinessesLogoIsServedWithTheLogoHeaders(t *testing.T) {
	own := digestOf(logoPNGA)
	h := logoActivation(t, &fakeInvites{}, logoBrands(), &fakeAudit{})
	want := answer{http.StatusOK, wantLogoHeader(own, "image/png", "logo.png", len(logoPNGA)), logoPNGA}
	for _, c := range []*http.Cookie{codeCookie(), pendingCookie()} {
		if got := ask(t, h, http.MethodGet, activationLogoPath(own), c); !got.same(want) {
			t.Errorf("cookie %q:\n got %s\nwant %s", c.Value[:8], got, want)
		}
	}
	// The other business's JPEG through its own invitation: the type decides the name.
	hb := logoActivation(t, &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return attackerContext(), nil }}, logoBrands(), &fakeAudit{})
	b := digestOf(logoJPEGB)
	wantB := answer{http.StatusOK, wantLogoHeader(b, "image/jpeg", "logo.jpg", len(logoJPEGB)), logoJPEGB}
	if got := ask(t, hb, http.MethodGet, activationLogoPath(b), codeCookie()); !got.same(wantB) {
		t.Errorf("the JPEG business:\n got %s\nwant %s", got, wantB)
	}
}

func TestActivationLogo_EveryRefusalIsTheSameNotFound(t *testing.T) {
	own, foreign := digestOf(logoPNGA), digestOf(logoJPEGB)
	failing := func(err error) func(invite.Code) (invite.Context, error) {
		return func(invite.Code) (invite.Context, error) { return okContext("invited"), err }
	}
	unverified := logoBrands()
	unverified.verified[testTenant] = false
	cases := []struct {
		name    string
		lookup  func(invite.Code) (invite.Context, error)
		brands  *fakeActivationBrands
		path    string
		cookies []*http.Cookie
		// resolves: whether the request may reach the invitation resolver.
		resolves bool
	}{
		{"no cookie", nil, nil, activationLogoPath(own), nil, false},
		{"only a session cookie", nil, nil, activationLogoPath(own), []*http.Cookie{wl13LiveSession()}, false},
		{"a cookie that does not parse", nil, nil, activationLogoPath(own), []*http.Cookie{{Name: activationCookieName, Value: "garbage"}}, false},
		{"an unknown code", func(invite.Code) (invite.Context, error) { return invite.Context{}, invite.ErrUnknownCode }, nil, activationLogoPath(own), []*http.Cookie{codeCookie()}, true},
		{"an expired invitation", failing(invite.ErrCodeExpired), nil, activationLogoPath(own), []*http.Cookie{codeCookie()}, true},
		{"a spent invitation", failing(invite.ErrCodeUsed), nil, activationLogoPath(own), []*http.Cookie{pendingCookie()}, true},
		{"a cancelled invitation", failing(invite.ErrCodeCancelled), nil, activationLogoPath(own), []*http.Cookie{codeCookie()}, true},
		{"an employee who may not activate", failing(invite.ErrNotActivatable), nil, activationLogoPath(own), []*http.Cookie{codeCookie()}, true},
		{"a business not verified", nil, unverified, activationLogoPath(own), []*http.Cookie{codeCookie()}, true},
		{"a digest nobody stored", nil, nil, activationLogoPath(unknownDigest), []*http.Cookie{codeCookie()}, true},
		{"another business's digest", nil, nil, activationLogoPath(foreign), []*http.Cookie{codeCookie()}, true},
		{"the digest in upper case", nil, nil, activationLogoPath(strings.ToUpper(own)), []*http.Cookie{codeCookie()}, false},
		{"a short digest", nil, nil, activationLogoPath(own[:63]), []*http.Cookie{codeCookie()}, false},
		{"a digest that is not hex", nil, nil, activationLogoPath(strings.Repeat("g", 64)), []*http.Cookie{codeCookie()}, false},
	}
	for _, c := range cases {
		brands := c.brands
		if brands == nil {
			brands = logoBrands()
		}
		inv := &countingInvites{fakeInvites: &fakeInvites{lookup: c.lookup}}
		got := ask(t, logoActivation(t, inv, brands, &fakeAudit{}), http.MethodGet, c.path, c.cookies...)
		if !got.same(wantNotFound()) {
			t.Errorf("%s:\n got %s\nwant the one 404", c.name, got)
		}
		if n := inv.count(); !c.resolves && n != 0 {
			t.Errorf("%s: resolved the invitation %d time(s); it is answered before the resolver", c.name, n)
		}
		_, reads := brands.calls()
		for _, r := range reads {
			if r.tenant != testTenant {
				t.Errorf("%s: read a logo of business %s, not the invitation's", c.name, r.tenant)
			}
		}
	}
}

func TestActivationLogo_IfNoneMatchIsAnsweredOnlyForTheInvitationsOwnLogo(t *testing.T) {
	own, foreign := digestOf(logoPNGA), digestOf(logoJPEGB)
	h := logoActivation(t, &fakeInvites{}, logoBrands(), &fakeAudit{})
	if got := askFrom(t, h, "203.0.113.5:41234", http.MethodGet, activationLogoPath(own),
		http.Header{"If-None-Match": {`"` + own + `"`}}, codeCookie()); !got.same(want304(own)) {
		t.Errorf("own etag:\n got %s\nwant %s", got, want304(own))
	}
	unverified := logoBrands()
	unverified.verified[testTenant] = false
	hu := logoActivation(t, &fakeInvites{}, unverified, &fakeAudit{})
	for name, c := range map[string]struct {
		h      http.Handler
		digest string
	}{"a business not verified, its own digest": {hu, own}, "another business's digest": {h, foreign}} {
		for _, lines := range [][]string{{`"` + c.digest + `"`}, {"*"}} {
			got := askFrom(t, c.h, "203.0.113.5:41234", http.MethodGet, activationLogoPath(c.digest),
				http.Header{"If-None-Match": lines}, codeCookie())
			if !got.same(wantNotFound()) {
				t.Errorf("%s, If-None-Match %q:\n got %s\nwant the one 404", name, lines, got)
			}
		}
	}
}

func TestActivationLogo_AFailureIsNotARefusal(t *testing.T) {
	own := digestOf(logoPNGA)
	broken := logoBrands()
	broken.logoErr = errors.New("connection reset")
	for name, h := range map[string]http.Handler{
		"the resolver fails": logoActivation(t, &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
			return invite.Context{}, errors.New("connection reset")
		}}, logoBrands(), &fakeAudit{}),
		"the reader fails": logoActivation(t, &fakeInvites{}, broken, &fakeAudit{}),
	} {
		got := ask(t, h, http.MethodGet, activationLogoPath(own), codeCookie())
		if got.status != http.StatusInternalServerError || bytes.Contains(got.body, logoPNGA) ||
			got.header.Get("Cache-Control") != "no-store" || got.header.Get("Content-Disposition") != "" {
			t.Errorf("%s = %s, want a 500 with no bytes", name, got)
		}
	}
}

// TestActivationLogo_ARequestSpendsTheFloodCeiling: each logo request is charged once to
// the flow's flood ceiling -- the bucket the wizard's pages spend -- before the resolver
// runs: after floodLimit logo requests from one address the page itself is refused, and
// the refused logo request resolves nothing.
func TestActivationLogo_ARequestSpendsTheFloodCeiling(t *testing.T) {
	own := digestOf(logoPNGA)
	inv := &countingInvites{fakeInvites: &fakeInvites{}}
	h := logoActivation(t, inv, logoBrands(), &fakeAudit{})
	const venue, elsewhere = "198.51.100.7:4000", "198.51.100.8:4000"
	for i := 1; i <= floodLimit; i++ {
		if got := askFrom(t, h, venue, http.MethodGet, activationLogoPath(own), nil, codeCookie()); got.status != http.StatusOK {
			t.Fatalf("logo request %d of %d = %d", i, floodLimit, got.status)
		}
	}
	if n := inv.count(); n != floodLimit {
		t.Fatalf("%d logo requests resolved the invitation %d times", floodLimit, n)
	}
	if got := askFrom(t, h, venue, http.MethodGet, activationLogoPath(own), nil, codeCookie()); got.status != http.StatusTooManyRequests || inv.count() != floodLimit {
		t.Errorf("logo request %d = %d after %d resolutions, want 429 and no resolution", floodLimit+1, got.status, inv.count())
	}
	if got := askFrom(t, h, venue, http.MethodGet, "/activate", nil, codeCookie()); got.status != http.StatusTooManyRequests {
		t.Errorf("the wizard from the same address = %d, want 429 from the shared ceiling", got.status)
	}
	if got := askFrom(t, h, elsewhere, http.MethodGet, "/activate", nil, codeCookie()); got.status != http.StatusOK {
		t.Errorf("control: the wizard from another address = %d, want 200", got.status)
	}
}

// TestActivationLogo_WritesNothing: neither a served logo nor any refusal writes an
// audit row, and refusals do not spend the invitation's window -- after more refused
// logo requests than that window holds, the wizard still answers the invitation's own
// failure normally rather than 429.
func TestActivationLogo_WritesNothing(t *testing.T) {
	rec := &fakeAudit{}
	expired := &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return okContext("invited"), invite.ErrCodeExpired }}
	h := logoActivation(t, expired, logoBrands(), rec)
	for range 3 * inviteFailureLimit {
		ask(t, h, http.MethodGet, activationLogoPath(digestOf(logoPNGA)), codeCookie())
	}
	ok := logoActivation(t, &fakeInvites{}, logoBrands(), rec)
	ask(t, ok, http.MethodGet, activationLogoPath(digestOf(logoPNGA)), codeCookie())
	if len(rec.events) != 0 {
		t.Fatalf("logo requests wrote %d audit row(s): %v", len(rec.events), rec.actions())
	}
	if got := ask(t, h, http.MethodGet, "/activate", codeCookie()); got.status != http.StatusBadRequest {
		t.Errorf("the wizard after %d refused logo requests = %d, want the invitation's own 400 (no window spent)", 3*inviteFailureLimit, got.status)
	}
}
