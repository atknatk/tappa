package handler

// panelbrand.go -- the business's brand in the panel chrome (M10 WL-8; ADR 0023 §2 and
// K4, ADR 0024 §5 and §7).
//
// Every /admin section of a business that set a brand draws, at the top of its
// chrome: a 4 px stripe in the accent (when the gate passes the accent today), the
// logo from GET /admin/brand/logo/{sha} (when it has one), its name, and the co-brand
// line. The panel's own actions keep the palette's colours (K4). A business with no
// brand gets the chrome as it was before WL-8, byte for byte, and the panel's
// Content-Security-Policy as it was (pages/panelbrandview.go).
//
// THE CLAIM, IN THREE PARTS.
//
// PART I -- THE SHIPPED CODE, MEASURED (against fakes through the real router; every
// response read from rec.Result()):
//   - every section of a business with no brand, whose brand read fails, whose only
//     accent the gate refuses, or whose stored digest is malformed: today's policy, the
//     wordmark, app.css alone, no <img>, stripe, theme or logo path, name or co-brand
//     line, the same bytes in all four; one call to the brand reader per request, for
//     the session's business (what else a section reads is not counted here); the
//     failures logged at ERROR, the refusal at WARN, with the tenant id
//     (TestPanelBrand_UnbrandedSectionsAreTheWordmarkChrome);
//   - the chrome and every shell the document head reaches, unbranded, byte for byte
//     what df544c1 rendered (TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8);
//   - every section of a business with an accent and a logo, an accent alone, a logo
//     alone, a logo beside a refused accent: the theme link right after app.css and the
//     stripe exactly when the accent passes; one <img> exactly when there is a logo, its
//     src the panel logo route, its box from the stored size, its alt present and
//     empty; img-src in the policy exactly then; the name and the co-brand line in
//     exactly their classes; the page otherwise the unbranded page byte for byte; one
//     call to the reader per request; the refusal's log lines carry no name, accent or
//     digest (TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection), and the img-src
//     correspondence over those renders too
//     (TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage);
//   - the theme href and the logo src are the routes' paths
//     (TestPanelBrand_TheThemeHrefIsTheThemeRoute, TestPanelBrand_TheLogoSrcIsTheLogoRoute);
//   - a sign-in walk to the picker and a panel problem page ask the reader 0 times; the
//     picker and the problem page keep the wordmark, no brand trace and adminCSP; the
//     sign-in page's policy names no img-src (TestPanelBrand_TheSignInFamilyStaysTaptime;
//     the reset family is not driven); the constructor
//     refuses a nil reader (TestPanelBrand_TheReaderIsRequired); the logo box
//     (TestPanelLogoOf_KeepsTheProportionsInsideTheSlot);
//   - every shell page named in a render call goes through renderPanel/renderScripted
//     with its own view's chrome (TestPanelRenders_AShellPageIsRenderedWithItsChromesPolicy).
//
// PART II -- NAMED PINS: the tests above, and internal/domain/tenant's PanelBrand tests
// (brandread.go). Each mutation that was run, and the tests it turned red, is in the
// WL-8 card's mutation table.
//
// PART III -- No completeness claim: any change the table does not list is the subject
// of code review.

import (
	"context"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// panelBrands is the slice of tenant.BrandReader the chrome needs, declared at the
// consumer (section 7).
type panelBrands interface {
	PanelBrand(ctx context.Context, tenantID uuid.UUID) (tenant.PanelBrand, error)
}

// panelBrand is the chrome's brand for one panel request.
//
// 🔴 A BRAND THAT CANNOT BE READ DOES NOT FAIL THE PAGE, AND IT IS NOT QUIETLY "NO
// BRAND" EITHER (section 4.6; ADR 0024 §7; the PendingBadge precedent in chrome). The
// section the manager asked for is still worth rendering, so the chrome falls back to
// the unbranded one -- but the failure is logged at ERROR. That includes the stored
// rows the reader refuses rather than guesses at (a half-described logo, an accent
// that is not six upper-case hex digits, a digest that is not 64 lower-case hex
// digits): each is an error here, never a logo drawn with a guessed type or box and
// never a page that silently lost its brand. An accent the gate refuses TODAY (the
// palette moved after it was saved) is not an error: the chrome draws no accent --
// the theme route would 404 it anyway -- and the decision is logged at WARN.
//
// The log lines carry the tenant id and the error; not the business's name, the
// accent or the digest (the WARN line's half of that is measured:
// TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection, refused-accent variant).
// The ERROR lines print the reader's error: the errors the reader builds itself
// (accentOf, logoRefOf) name no stored value, and a database error is wrapped as the
// driver returns it -- not measured here.
func (a *AdminAuth) panelBrand(ctx context.Context, tenantID uuid.UUID) pages.PanelBrand {
	b, err := a.brands.PanelBrand(ctx, tenantID)
	if err != nil {
		a.log.ErrorContext(ctx, "panel: could not read the business's brand; the chrome is drawn without it",
			"tenant_id", tenantID, "err", err)
		return pages.PanelBrand{}
	}
	if b.AccentRefused {
		a.log.WarnContext(ctx, "panel: the stored accent does not pass the legibility gate today; the chrome draws no accent",
			"tenant_id", tenantID)
	}
	if b.HasLogo && !isLogoDigest(b.Logo.SHA256) {
		a.log.ErrorContext(ctx, "panel: the stored logo's digest is not 64 lower-case hex digits; the chrome is drawn without the brand",
			"tenant_id", tenantID)
		return pages.PanelBrand{}
	}
	return panelBrandView(b)
}

// panelBrandView maps the domain read onto the chrome's view. Nothing branded is the
// zero view; a branded business gets its name (businessName's fallback for an empty
// one), its logo's <img> when it has one, and the accent's stylesheet when the gate
// passed it.
func panelBrandView(b tenant.PanelBrand) pages.PanelBrand {
	if !b.Branded() {
		return pages.PanelBrand{}
	}
	v := pages.PanelBrand{Name: businessName(b.Name)}
	if b.HasLogo {
		v.Logo = pages.PanelLogoOf(adminLogoHref(b.Logo.SHA256), b.Logo.Width, b.Logo.Height)
		// The Account editor's preview (M10 WL-7): the tap screen's header shape, the
		// panel's route, from this same read. Only where the chrome draws the logo too,
		// so the preview never adds the page's first <img> (adminCSPFor reads the
		// chrome's DrawsLogo alone).
		if v.Logo.Src != "" {
			v.Preview = layout.PreviewLogo(b.Logo.SHA256, b.Logo.Width, b.Logo.Height, v.Name)
		}
	}
	if b.HasAccent {
		v.Theme = layout.ThemeOf(b.Accent)
	}
	if !v.Branded() {
		// A logo with a box the table cannot hold (PanelLogoOf draws none) and no
		// accent: nothing left to draw, so not half a header with a name alone.
		return pages.PanelBrand{}
	}
	return v
}
