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
// THE FIELDS ARE UNEXPORTED AND THE CONSTRUCTORS VALIDATE, so the only image URL a
// header can write is the one built here: "/t/logo/" plus a 64-digit lower-case hex
// digest, with a box in 1..512. Any other shape becomes the zero Logo, which draws
// nothing and widens no policy. The handler decides the Content-Security-Policy from
// Logo.Drawn, the same predicate the header draws the <img> from, so the page names
// img-src exactly when it draws an image.

import "strconv"

// tapLogoRoute is the tap surface's logo route (GET /t/logo/{sha}, ADR 0024 §5;
// internal/handler/brandlogo.go). The Account preview reads the panel's route instead;
// that constructor is WL-7's.
const tapLogoRoute = "/t/logo/"

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

// Drawn reports whether this logo is drawn. It is the page's hasLogo: the handler names
// img-src in the response's policy exactly when this is true (ADR 0024 §5).
func (l Logo) Drawn() bool { return l.src != "" }

func (l Logo) widthAttr() string  { return strconv.Itoa(l.width) }
func (l Logo) heightAttr() string { return strconv.Itoa(l.height) }

// Brand is what the tap screen takes from a business's brand: the header's logo and
// the tap button's accent, as its theme stylesheet (ADR 0023 §2, user decision D-C).
// The result screen takes a Logo alone (BrandedPage) -- it has no accent slot.
type Brand struct {
	logo  Logo
	theme Theme
}

// TapBrand builds the tap screen's brand. theme is layout.ThemeOf the stored accent,
// after the read side has passed it through brand.Check (ADR 0023 §3), or the zero
// Theme for none.
func TapBrand(logo Logo, theme Theme) Brand { return Brand{logo: logo, theme: theme} }

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
