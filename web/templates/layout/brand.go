package layout

// brand.go -- what a page takes from a business's brand (M10 WL-9; ADR 0023 §2, §4-§7;
// ADR 0024 §5). Two slots and no more: the header (a logo, or the wordmark) and, on the
// tap screen only, the stylesheet that carries the tap button's accent -- a Theme
// (theme.go, M10 WL-8), which documentHead writes right after app.css.
//
// THE ZERO VALUES ARE TAPTIME'S OWN PAGE. A Brand{} and a Logo{} render exactly the
// markup the shells rendered before WL-9 -- no extra <link>, no <img>, the green
// wordmark -- and internal/handler's TestUnbrandedScreens_AreByteIdenticalToTheGolden
// compares the listed renders with goldens written before this file existed.
//
// THE FIELDS ARE UNEXPORTED AND THE CONSTRUCTORS VALIDATE, so the only image URLs a
// header can write are the three built here: "/t/logo/" (TapLogo), for the Account
// editor's preview inside the panel "/admin/brand/logo/" (PreviewLogo, M10 WL-7), and
// for the activation wizard "/activate/logo/" (ActivationLogo, M10 WL-13), plus a
// 64-digit lower-case hex digest, with a box in 1..512. Any other shape becomes the
// zero Logo, which draws nothing and widens no policy. The handler decides the
// Content-Security-Policy from Logo.Drawn, the same predicate the header draws the
// <img> from, so the page names img-src exactly when it draws an image.

import "strconv"

// tapLogoRoute is the tap surface's logo route (GET /t/logo/{sha}, ADR 0024 §5;
// internal/handler/brandlogo.go). The Account preview reads the panel's route instead
// (previewLogoRoute, PreviewLogo).
const tapLogoRoute = "/t/logo/"

// previewLogoRoute is the panel's logo route (GET /admin/brand/logo/{sha}, ADR 0024 §5).
// The Account editor's preview (M10 WL-7) is drawn inside the panel, where the panel
// cookie (Path=/admin) is what the browser sends and a live employee session is not:
// the tap route would answer it 404 (ADR 0023 §2, "Account önizlemesi"). internal/handler's
// TestBrandPreview_TheLogoIsThePanelRoute requests the src this writes through the
// panel's own logo route.
const previewLogoRoute = "/admin/brand/logo/"

// activationLogoRoute is the activation wizard's logo route (GET /activate/logo/{sha}, M10
// WL-13; internal/handler, activate.go). Steps 1-4 of the wizard are drawn for a browser
// that holds an invitation and NO session, so the tap route -- which answers only a live
// employee session -- would 404 them; this route resolves the business from the
// invitation in the activation cookie instead, and serves the logo only to a business
// VIES verified (ADR 0024 §5's WL-13 note). The "Activation complete" and "already set
// up" screens hold a session and draw TapLogo.
const activationLogoRoute = "/activate/logo/"

// logoMaxEdge is the stored logo's longest edge (ADR 0024 §3; brand.LogoMaxOutputEdge,
// migration 00028's CHECK). A box outside 1..512 is not one a stored logo can have.
const logoMaxEdge = 512

// Logo is a business's logo as a page header draws it: where to fetch it, the stored
// box (written as the <img>'s width and height, so the browser reserves its place
// before the bytes arrive), and its alt text -- the business's name, which is the one
// new piece of text on the tap screen (ADR 0023 §6 item 10).
type Logo struct {
	src           string
	width, height int
	alt           string
}

// TapLogo describes a logo served by the tap surface's route. A digest that is not 64
// lower-case hex digits, or a box outside 1..512, gives the zero Logo: nothing drawn.
func TapLogo(sha256 string, width, height int, alt string) Logo {
	if !isLowerHexDigest(sha256) || width < 1 || width > logoMaxEdge || height < 1 || height > logoMaxEdge {
		return Logo{}
	}
	return Logo{src: tapLogoRoute + sha256, width: width, height: height, alt: alt}
}

// PreviewLogo describes the same logo as TapLogo, served by the panel's route: the
// Account editor's preview of the tap screen (M10 WL-7). The rule for the digest and
// the box is TapLogo's, so a value one of them refuses the other refuses too.
func PreviewLogo(sha256 string, width, height int, alt string) Logo {
	l := TapLogo(sha256, width, height, alt)
	if !l.Drawn() {
		return Logo{}
	}
	l.src = previewLogoRoute + sha256
	return l
}

// ActivationLogo describes the same logo as TapLogo, served by the activation wizard's
// route (M10 WL-13): the wizard's four steps, which a browser sees before it holds a
// session. The rule for the digest and the box is TapLogo's.
func ActivationLogo(sha256 string, width, height int, alt string) Logo {
	l := TapLogo(sha256, width, height, alt)
	if !l.Drawn() {
		return Logo{}
	}
	l.src = activationLogoRoute + sha256
	return l
}

// Drawn reports whether this logo is drawn. It is the page's hasLogo: the handler names
// img-src in the response's policy exactly when this is true (ADR 0024 §5).
func (l Logo) Drawn() bool { return l.src != "" }

func (l Logo) widthAttr() string  { return strconv.Itoa(l.width) }
func (l Logo) heightAttr() string { return strconv.Itoa(l.height) }

// Brand is what the tap screen takes from a business's brand: the header's logo and
// the tap button's accent, as its theme stylesheet (ADR 0023 §2, user decision D-C).
// The result screen takes a Logo alone (BrandedPage) -- it has no accent slot. Since
// M10 WL-13 the activation family takes the same two slots (ActivationBrand).
type Brand struct {
	logo  Logo
	theme Theme
}

// TapBrand builds the tap screen's brand. theme is layout.ThemeOf the stored accent,
// after the read side has passed it through brand.Check (ADR 0023 §3), or the zero
// Theme for none.
func TapBrand(logo Logo, theme Theme) Brand { return Brand{logo: logo, theme: theme} }

// ActivationBrand builds the activation family's brand (M10 WL-13, user decisions of
// 2026-10-09): the tap screen's header and its accent, on the wizard's four steps, on
// "Activation complete" and on "already set up". The logo is ActivationLogo on the
// wizard and TapLogo on the two screens that hold a session; the caller has already
// dropped it for a business VIES did not verify (internal/domain/tenant,
// BrandReader.ActivationBrand). theme follows TapBrand's rule.
func ActivationBrand(logo Logo, theme Theme) Brand { return Brand{logo: logo, theme: theme} }

// Accented reports whether the page links the accent's theme stylesheet. The activation
// wizard reads it to turn its other green marks ink (user decision 3 of 2026-10-09, an
// extension of K-2a: with a tenant accent on the screen, the accent is the one colour).
func (b Brand) Accented() bool { return b.theme.Linked() }

// Logo is the header's logo.
func (b Brand) Logo() Logo { return b.logo }

// ThemeHref is the theme stylesheet's URL, or "" when there is no accent.
func (b Brand) ThemeHref() string { return b.theme.Href() }

// inkWordmark is user decision K-2a (2026-10-02): an accent and no logo turns the
// header's "taptime" ink, so the tenant's button is the one accent colour on the screen.
func (b Brand) inkWordmark() bool { return b.theme.Linked() && !b.logo.Drawn() }

func isLowerHexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
