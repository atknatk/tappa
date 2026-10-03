package layout

import "github.com/atknatk/tappa/internal/brand"

// Theme is the tenant accent's stylesheet a page links AFTER app.css (ADR 0023 §4), or
// none. documentHead writes it as the one <link rel="stylesheet"> that follows app.css;
// the route answers a :root rule with the three --brand-* variables, which win over
// app.css's defaults by order (input.css's base layer has the argument).
//
// IT IS BUILT FROM A brand.Color AND FROM NOTHING ELSE, so the href this package
// writes is always "/brand/theme/" + six upper-case hex digits + ".css": a path on our
// own origin, in the one spelling GET /brand/theme/{HEX}.css answers (internal/handler,
// brandtheme.go). There is no constructor from a string, so no caller can hand the
// document head a URL. internal/handler's TestPanelBrand_TheThemeHrefIsTheThemeRoute
// requests ThemeOf(c).Href() through the router for the colours it lists and gets that
// route's 200 and body.
//
// THE ZERO VALUE IS "NO THEME" and writes nothing, so a shell that is not given one --
// every shell but the panel's today -- renders as it did before the type existed.
//
// Linking it is a decision about the RESPONSE, not about the business: the caller
// passes a Theme only for a colour brand.Check passes today (the route 404s the
// others, and the page would then keep app.css's defaults anyway) and only on a page
// with an accent slot (ADR 0023 §4: the tap screen and the panel shell).
type Theme struct{ hex string }

// ThemeOf is the theme stylesheet for one accent.
func ThemeOf(c brand.Color) Theme { return Theme{hex: c.Hex()} }

// Linked reports whether this is a theme to link (false for the zero value).
func (t Theme) Linked() bool { return t.hex != "" }

// Href is the stylesheet's path, or "" for the zero value.
func (t Theme) Href() string {
	if t.hex == "" {
		return ""
	}
	return "/brand/theme/" + t.hex + ".css"
}
