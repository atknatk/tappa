package layout

// Alternate is one translation of a page, for the head's <link rel="alternate" hreflang>
// (M11 DOCS-1: the integration guide in English and Turkish). Href is a site path the
// caller's router serves -- a constant, never built from a request -- and Lang its BCP 47
// tag. Only MarketingTranslated writes them; every other shell passes none and its head is
// byte-for-byte what it was (TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8).
type Alternate struct {
	Lang string
	Href string
}
