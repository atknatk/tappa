package pages

import "strings"

// --- share cards (LP-13) ---------------------------------------------------------------
//
// What a public page shows when its link is pasted into a chat or a feed: the head's
// description and one 1200×630 image (layout.Share). The ShareCards board names the
// image of every page and the sentence its description comes from. Every description
// but one is a sentence the page itself prints -- a Line, a lede, the sign-off -- read
// from where the page prints it, so a description cannot say something the page no
// longer does. The exception is the integration guide's (the board's N1): the guide is
// Markdown with no Line of its own, so its description is a Line here, pinned to the
// two API facts it states (APIGuideDescription).
//
// THE IMAGES ARE COMMITTED PNGs under web/static/og, drawn from the board's markup at
// 1200×630 (K3, user decision of 2026-10-23). No price is drawn into one: the price is
// derived (TestLanding_PriceMatchesTheSchemaItIsCharged) and an image is not.

// ShareImages is one value per share image, in the board's order: the four legal
// documents share one image and the guide's two languages share one. ShareImageFiles
// holds each file's path inside web/static; internal/handler resolves each to the full
// URL a crawler reads, once, at start-up.
type ShareImages struct {
	Landing     string
	Product     string
	Pricing     string
	Security    string
	Restaurants string
	Production  string
	Signup      string
	Legal       string
	API         string
}

// ShareImageFiles is where each image sits inside web/static. The names are fixed:
// a link already shared keeps naming the file it was shared with.
var ShareImageFiles = ShareImages{
	Landing:     "og/landing.png",
	Product:     "og/product.png",
	Pricing:     "og/pricing.png",
	Security:    "og/security.png",
	Restaurants: "og/restaurants.png",
	Production:  "og/production.png",
	Signup:      "og/signup.png",
	Legal:       "og/legal.png",
	API:         "og/api.png",
}

// SignOffLine is the green sign-off's sentence -- "Start yourself today." and the free
// months (K6) -- on every page that ends on it, and the sign-up wizard's description.
// One function, so the shared link and the button above it say the same thing.
func SignOffLine(freeMonths int) string {
	return strings.TrimSpace("Start yourself today. " + LandingFreeMonthsLine(freeMonths).Text)
}

// ProductDescription is the product tour's description: the first two sentences of its
// lede (the third, "These are the screens in between.", points at the page and says
// nothing on its own).
func ProductDescription(c ProductText) string {
	return leadingSentences(c.Lede.Text, 2)
}

// APIGuideDescription is the integration guide's description, in the page's language:
// what the guide's section 1.1 says the API lets a business's own system do, pinned to
// the two facts it states. "Every counted tap" is the feed's own word
// (FactAPIFeedsEveryCountedTap): a record a manager types is not in the feed, so neither
// page says "every punch".
func APIGuideDescription(turkish bool) Line {
	facts := []Fact{FactAPIWritesPeopleAndLocations, FactAPIFeedsEveryCountedTap}
	if turkish {
		return Line{Text: "Lokasyonları ve çalışanları yazın, sayılan her dokunuşu bir akıştan okuyun ya da " +
			"webhook ile alın: Taptime entegrasyon API'si, açıklamalı.", Facts: facts}
	}
	return Line{Text: "Write locations and employees, read every counted tap from a feed or receive it by " +
		"webhook: the Taptime integration API, explained.", Facts: facts}
}

// leadingSentences is the first n sentences of text, a sentence ending in ". ".
func leadingSentences(text string, n int) string {
	end := 0
	for i := 0; i < n; i++ {
		j := strings.Index(text[end:], ". ")
		if j < 0 {
			return text
		}
		end += j + 1
		if i < n-1 {
			end++
		}
	}
	return text[:end]
}
