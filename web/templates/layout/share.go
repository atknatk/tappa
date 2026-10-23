package layout

import "strconv"

// Share is what a public page shows when its link is pasted somewhere that draws a
// preview (LP-13): the head's description, its Open Graph lines and its card type.
//
// ONLY THE MARKETING SHELLS TAKE ONE. documentHead is the head of every page in the
// product, and every other shell passes the zero value, which writes nothing: the tap,
// activation, panel and operator heads stay byte for byte what they were
// (TestUnbrandedScreens_AreByteIdenticalToTheGolden). A page that is not meant to be
// shared -- the public 404, a wizard step past the first -- passes the zero value too.
//
// Description is one of the page's own sentences, never typed here. Image is an
// ABSOLUTE URL, because a crawler reads og:image without the page's address to resolve
// it against: the configured origin (TAPPA_BASE_URL) and a file under /static/og,
// resolved once at start-up by internal/handler and never built from a request -- the
// page renders the same bytes for every visitor and every Host header. "" writes no
// image and no large card. The image's alternative text (og:image:alt,
// twitter:image:alt) is the page's title: every card draws the page's own heading under
// the brand, so the title says what the image shows without a new sentence to pin.
type Share struct {
	Description string
	Image       string
}

// ShareImageWidth and ShareImageHeight are the size every share image is drawn at, the
// one Open Graph and the large summary card both ask for. internal/handler reads each
// committed PNG's header and holds it to these.
const (
	ShareImageWidth  = 1200
	ShareImageHeight = 630
)

// imageWidth and imageHeight are the two sizes as the head writes them.
func (Share) imageWidth() string  { return strconv.Itoa(ShareImageWidth) }
func (Share) imageHeight() string { return strconv.Itoa(ShareImageHeight) }
