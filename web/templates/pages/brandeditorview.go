package pages

import (
	"strings"

	"github.com/atknatk/tappa/web/templates/components"
)

// The Account section's "Your brand" editor (M10 WL-7; ADR 0023 §2, ADR 0024 §6).
//
// 🔴 THE PREVIEW SHOWS WHAT IS SAVED, AND ONLY THAT. It is drawn from the panel chrome
// itself, not from this view: accountBrandPreview takes the PanelChrome the page is
// drawn in -- its header from PanelBrand.TapHeader (the panel's logo route and the
// theme the shell links), its greeting from the signed-in admin's name. BrandEditor,
// which a refused save rewrites (the colour that was tried goes into Picker), is not an
// input of the preview at all, so a candidate colour has no path to the preview's
// header (the ink wordmark of decision K-2a) or to its button, whose accent is the
// shell's one theme stylesheet. A colour somebody has picked but not saved is shown by
// the colour input itself, which the browser paints -- a second stylesheet for a
// candidate would rewrite :root and paint the panel's stripe too (ADR 0023 §2,
// "Account önizlemesi"). TestBrandPreview_ARefusedColourLeavesThePreviewAsSaved holds
// the preview of every refused save byte-equal to the same business's plain load.
//
// 🔴 THE PREVIEW CANNOT BE SENT. It is drawn outside every <form>, its button is the
// tap button's preview face (type="button", pages.TapButtonPreview), and the block is
// inert, so the dead button is not a tab stop either. The editor's own three forms
// post to the three brand routes and nowhere else.

// BrandEditor is the editor's view.
type BrandEditor struct {
	// CanEdit decides whether the three forms are drawn. It is the courtesy half of the
	// owner-only gate; internal/handler refuses the POSTs on its own.
	CanEdit bool
	// WhyNotEdit is what a manager reads where the forms would be.
	WhyNotEdit string

	// HasLogo and HasAccent say what is saved, from the chrome's read the preview is
	// drawn from; they decide whether "Remove the logo" and "Back to Taptime green" are
	// offered.
	HasLogo   bool
	HasAccent bool

	// Picker is the colour input's value, "#rrggbb": the saved accent, Taptime green
	// when none is saved, or -- on a refused save -- the colour that was tried, which
	// the input then shows in its own swatch.
	Picker string
	// Typed is what the hex box holds: "" on a plain load, what was typed after a
	// refused save.
	Typed string
	// AccentError is the message under the two accent inputs after a refused save, and
	// AccentErrorCode the colour code it ends on ("1F5C41", "#757575"). The code is
	// drawn in mono after the sentence: it is data, not prose (skill tappa-brand;
	// accountStillOnFile's precedent).
	AccentError     string
	AccentErrorCode string
	// Suggest is the colour offered in place of an illegible one ("#1A818D"), or "".
	// SuggestHex is the same colour as the form posts it ("1A818D").
	Suggest    string
	SuggestHex string

	// Outcome is the notice the section opens with after one of the three POSTs, or
	// the zero value. It comes from a closed vocabulary in internal/handler.
	Outcome BrandOutcome

	// The three routes, from internal/handler's constants.
	LogoPostHref, AccentPostHref, ResetPostHref string
	// LogoMaxKB is the part limit the logo input's help text names, from
	// internal/brand.LogoMaxInputBytes.
	LogoMaxKB int
}

// BrandOutcome is one notice: its tone, its heading and its sentence.
type BrandOutcome struct {
	Tone     components.Tone
	Heading  string
	Sentence string
}

// Shown reports whether there is a notice to draw.
func (o BrandOutcome) Shown() bool { return o.Heading != "" }

// AccentDescribedBy is the hex box's aria-describedby: its help, plus the error when
// there is one (AccountView.DescribedBy's rule).
func (e BrandEditor) AccentDescribedBy() string {
	if e.AccentError == "" {
		return "brand-accent-help"
	}
	return "brand-accent-help brand-accent-error"
}

// PickerValue is the colour input's value attribute. An input of type color accepts
// only "#" and six hex digits and lower-cases them (HTML's "valid simple colour"); the
// handler builds Picker from a brand.Color, so this only lower-cases.
func (e BrandEditor) PickerValue() string { return strings.ToLower(e.Picker) }

// SuggestValue is the suggestion's swatch value, in the colour input's spelling.
func (e BrandEditor) SuggestValue() string { return strings.ToLower(e.Suggest) }
