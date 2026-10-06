package pages

import "github.com/atknatk/tappa/web/templates/layout"

// The panel chrome's tenant brand (M10 WL-8; ADR 0023 §2, K4; skill tappa-brand ->
// "Tenant slots"). A business that set a brand sees, at the top of every /admin
// section: a 4 px stripe in its accent, its logo, its name, and under the name the
// co-brand line ("taptime · punchless", in the tone the wordmark's tagline has). The
// panel's own actions do not change colour: primary buttons, the tab marker and the
// focus rings stay the palette's (K4). The stripe is decoration -- aria-hidden, no
// text, no action -- which is the stated exception to the skill's "one accent colour"
// rule.
//
// 🔴 THE ZERO VALUE IS THE CHROME BEFORE WL-8, BYTE FOR BYTE. A business with no
// brand, one whose brand could not be read (internal/handler logs it, section 4.6),
// and one whose only accent the gate refuses today all get PanelBrand{} -- and with
// it the wordmark, no stylesheet after app.css, no <img>, no business name, and the
// panel's Content-Security-Policy unchanged. internal/handler's
// TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8 holds the renders it names to
// digests taken before this change.

// PanelBrand is what the chrome draws of the business. The handler builds it from
// internal/domain/tenant's PanelBrand read.
type PanelBrand struct {
	// Name is the business's name: the visible text beside the logo. Set whenever the
	// brand is. It is NOT the logo's alt here, which is empty (panelBrandHeader says
	// why; the tap screen, where no name is shown, keeps the skill's alt = name).
	Name string
	// Logo is the <img>; its zero value draws none.
	Logo PanelLogo
	// Theme is the accent's stylesheet. Linked, the shell writes it after app.css and
	// the chrome draws the stripe, which reads the accent; the zero value does neither.
	Theme layout.Theme
	// Preview is the same logo as the TAP SCREEN's header draws it -- the stored box,
	// the business's name as its alt -- but served by the panel's route
	// (layout.PreviewLogo): the Account editor's preview (M10 WL-7) draws it. The chrome
	// does not. The handler builds it from the same read, in the same branch, as Logo,
	// so the preview draws an <img> only on a page whose chrome draws one and whose
	// policy therefore already names img-src (DrawsLogo below);
	// TestPanelBrandView_ThePreviewLogoIsDrawnOnlyWhereTheChromesIs holds the pair.
	Preview layout.Logo
}

// Branded reports whether the chrome draws the business's header (stripe, logo, name)
// in place of the wordmark.
func (b PanelBrand) Branded() bool { return b.Logo.Src != "" || b.Theme.Linked() }

// DrawsLogo reports whether the chrome contains the logo's <img>. It is the predicate
// the template draws the chrome's <img> by, and internal/handler's renderPanel passes
// the same answer to adminCSPFor -- img-src is named by a response that draws an image
// and by no other (ADR 0023 §4, ADR 0024 §5). The Account editor's preview draws a
// second <img> from Preview, which is set only where this is true (M10 WL-7).
func (b PanelBrand) DrawsLogo() bool { return b.Logo.Src != "" }

// DrawsLogo is the chrome's answer, for the handler's render path.
func (c PanelChrome) DrawsLogo() bool { return c.Brand.DrawsLogo() }

// TapHeader is the tap screen's header as the Account editor's preview draws it (M10
// WL-7): the preview logo and the theme the shell links, both from this one value --
// what is SAVED, as the chrome read it. It is the preview's only source for the
// header, so nothing a refused save puts back on the editor's form can reach it.
func (b PanelBrand) TapHeader() layout.Brand { return layout.TapBrand(b.Preview, b.Theme) }

// PanelLogo is the logo's <img>: its path and the box it is drawn in. The box is
// written as the width and height attributes, so the browser reserves it before the
// image arrives and the header does not move when it does (skill tappa-brand: "the
// width/height attributes are computed from the stored size").
type PanelLogo struct {
	Src    string
	Width  int
	Height int
}

// The logo's slot in the panel header: 32 px high (the skill's proposal for the
// panel), at most 192 px wide -- six times the height, so a long wordmark-shaped logo
// keeps its proportions without pushing the business's name off a phone-width header.
const (
	panelLogoMaxHeight = 32
	panelLogoMaxWidth  = 192
)

// PanelLogoOf is the <img> for a stored logo of width w and height h (pixels, as
// stored: 1-512 each, migration 00028) served at src. The box keeps the stored
// proportions, fits inside 192x32 and is never larger than the stored image (a small
// logo is not blown up into a blurred one). Each side is rounded to the nearest pixel
// and is at least 1. A non-positive size, which the table cannot hold, draws nothing.
func PanelLogoOf(src string, w, h int) PanelLogo {
	if src == "" || w <= 0 || h <= 0 {
		return PanelLogo{}
	}
	bw, bh := w, h
	switch {
	case w <= panelLogoMaxWidth && h <= panelLogoMaxHeight:
		// fits as stored
	case w*panelLogoMaxHeight >= h*panelLogoMaxWidth:
		// at least as wide as the slot's 6:1: the width binds
		bw, bh = panelLogoMaxWidth, roundedRatio(h, panelLogoMaxWidth, w)
	default:
		bw, bh = roundedRatio(w, panelLogoMaxHeight, h), panelLogoMaxHeight
	}
	return PanelLogo{Src: src, Width: bw, Height: bh}
}

// roundedRatio is a*b/c rounded to the nearest integer (halves up), and at least 1.
// Integers only: the inputs are pixel counts no larger than 512 x 192.
func roundedRatio(a, b, c int) int {
	n := (2*a*b + c) / (2 * c)
	if n < 1 {
		return 1
	}
	return n
}
