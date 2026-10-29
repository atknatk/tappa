package pages

// THE LINK BY E-MAIL (M14-22; ADR 0031 §5.7's note of 2026-10-29): the panel's banner and the page the
// e-mailed link brings the person to. Every leaf is a string, a count or a flag (the §4.7 walls walk
// the views by type). NOTHING HERE NAMES A BUSINESS other than the one the session is in: the banner
// says "another business" before the mailbox proof (the user's decision), and the page after it
// points to "Switch business", which lists them.

// PanelAddBusiness is the shell's banner. The zero value draws nothing.
type PanelAddBusiness struct {
	// Href is the request's form target ("Add it", a bound POST) and the page's address.
	Href string
	// Finish: this browser holds the e-mailed link (it was opened before the person signed in), so the
	// banner sends the person to the page that spends it instead of offering a new e-mail.
	Finish bool
}

// Shown reports whether there is a banner to draw.
func (b PanelAddBusiness) Shown() bool { return b.Href != "" }

// LinkBusinessesView is the page of the e-mailed link (GET /admin/account/link-businesses).
type LinkBusinessesView struct {
	PanelChrome
	// Done and Problem are the outcome sentences (closed tables in internal/handler).
	Done    string
	Problem string
	// ConfirmHref is drawn when this browser holds the e-mailed link: the one press that spends it.
	ConfirmHref string
	// RequestHref is "Send me the link" -- drawn when there is a business to add and no link in hand.
	RequestHref string
	// Sent: a link was just e-mailed ("Check your inbox").
	Sent bool
	// SwitchHref is "Switch business", after the businesses were added.
	SwitchHref string
	// Unlinked: the session is an older per-business sign-in, which cannot add anything.
	Unlinked bool
}
