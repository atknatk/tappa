package handler

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/brand"
)

// brandThemeRoute is the theme stylesheet's path (ADR 0023 §4). {file} is one
// path segment; brandThemeBody decides what it may be.
const brandThemeRoute = "/brand/theme/{file}"

// BrandTheme serves GET /brand/theme/{HEX}.css: the three --brand-* variables of
// one accent, for a page that links it after app.css (ADR 0023 §4). cmd/tappa
// passes it to httpx.NewRouter.
//
// IT IS GIVEN NO DEPENDENCY. BrandTheme has no fields and NewBrandTheme takes no
// argument, so no pool, query interface, session codec or audit sink reaches the
// handler through either. serve's answer comes from brandThemeBody, which reads the
// request's {file} path segment and its query (RawQuery, ForceQuery). ADR 0023 §4's
// reasons: the URL is unauthenticated and cached as immutable, so a body that
// depended on who asked, or on a tenant's row, would be handed to whoever asked
// next. The hex is not tenant data (ADR 0023, counted limit 4).
// TestBrandTheme_TheConstructorTakesNothing pins the parameter count, the result
// type and the field count; package-level state serve might reach is code review's.
type BrandTheme struct{}

// NewBrandTheme returns the theme route.
func NewBrandTheme() *BrandTheme { return &BrandTheme{} }

// Mount registers the route for GET and HEAD.
//
// HEAD FOR THE REASON /healthz HAS IT (internal/httpx/router.go): a GET-only chi
// route answers HEAD with 405, and this answer has no identity, budget or database
// query behind it (TestBrandTheme_TheAnswerDoesNotDependOnWhoAsks drives four request
// dressings and gets the same bytes), so admitting HEAD has no blast radius. net/http
// drops the body of a HEAD answer. For FFC72C and 808080,
// TestBrandTheme_HeadIsGetWithoutTheBody reads both answers byte for byte and asserts
// that HEAD has GET's status line and header lines (Date's value aside) and that no
// byte follows HEAD's header block.
// TestBrandTheme_OtherMethodsAre405 asserts 405 with Allow naming GET and HEAD for
// the seven other standard methods, and 405 for PROPFIND.
func (t *BrandTheme) Mount(r chi.Router) {
	r.Get(brandThemeRoute, t.serve)
	r.Head(brandThemeRoute, t.serve)
}

// serve answers 200 with the body ThemeCSS builds, or http.NotFound -- the handler
// chi answers an unrouted path with. For the refused paths it drives,
// TestBrandTheme_AnswersOnlyACanonicalLegibleHex asserts the two answers are equal.
//
// The three headers are ADR 0023 §4's. Cache-Control lets a cache keep the body for
// a year without revalidating, which is sound while one URL names one body: the URL
// names the colour, and the body also depends on the palette, ThemeCSS's format and
// Check's thresholds. Changing one of those is meant to come with a new route -- a
// rule for review. TestBrandTheme_ANewBodyNeedsANewRoute turns red when the bodies
// of its fifteen colours change and its ledger does not; its comment lists what it
// does not see. nosniff: a browser is not to read the body as anything but a
// stylesheet.
func (*BrandTheme) serve(w http.ResponseWriter, r *http.Request) {
	body, ok := brandThemeBody(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/css; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	// A failed write here has no caller to report to: the handler holds no logger
	// (the design above); the precedent is /healthz's live.
	_, _ = io.WriteString(w, body)
}

// brandThemeBody is the body for this request, or false when the route refuses it.
//
// ONE COLOUR, ONE URL. The segment must be the hex ParseAccent accepts (six bytes,
// 0-9 or A-F) followed by ".css", and the URL must carry no query: the body does not
// depend on a query, so admitting one would multiply the URLs, each cached for a
// year, under which one body lives. The accent must then pass the gate: ThemeCSS
// returns ErrAccentIllegible where Check does, the serving side ADR 0023 §3 asks for
// (a palette change can move a stored accent out of the accepted set).
func brandThemeBody(r *http.Request) (string, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return "", false
	}
	hex, ok := strings.CutSuffix(chi.URLParam(r, "file"), ".css")
	if !ok {
		return "", false
	}
	c, err := brand.ParseAccent(hex)
	if err != nil {
		return "", false
	}
	body, err := brand.ThemeCSS(c)
	if err != nil {
		return "", false
	}
	return body, true
}
