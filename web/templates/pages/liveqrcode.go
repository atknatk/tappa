package pages

// LiveQRCodeView is the one page that shows a new live QR screen's pairing code (M16-20): the
// answer to "Add a live QR screen", never a URL a browser can come back to (the code is not in any
// address). It is the code's ONE egress (liveqr.PairingCode.ForThePanelOnce).
type LiveQRCodeView struct {
	PanelChrome
	VenueName string
	// Code is the pairing code in its three groups.
	Code string
	// Minutes is how long it works (liveqr.PairingLifetime).
	Minutes int
	// ScreenURL is where the screen opens (TAPPA_BASE_URL + /screen).
	ScreenURL string
	// BackHref reopens the venue, whose block then lists the new screen as waiting.
	BackHref string
}
