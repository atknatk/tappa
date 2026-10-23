package handler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The share cards (LP-13): every public page's description, Open Graph lines and share
// image. TestMarketing_ReachesNoThirdParty holds the og:image exemption itself.

// shareCardPage is one public page and the card the ShareCards board gives it.
type shareCardPage struct {
	url         string
	description string
	image       string // the file inside web/static
}

// shareCardPages is the thirteen pages and their cards, each description named by the
// value the page prints it from -- never a second copy of the sentence.
func shareCardPages() []shareCardPage {
	f := pages.ShareImageFiles
	out := []shareCardPage{
		{"/", pages.LandingHeroLede.Text, f.Landing},
		{pages.ProductPath, pages.ProductDescription(pages.ProductCopy(productFigures(securityFigures()))), f.Product},
		{pages.SecurityPath, pages.LandingSecurityLede.Text, f.Security},
		{pages.PricingPath, pages.LandingPricingLede.Text, f.Pricing},
		{pages.ForRestaurantsPath, pages.LandingAudiences[0].Points[1].Text, f.Restaurants},
		{pages.ForProductionPath, pages.LandingAudiences[1].Points[0].Text, f.Production},
		{signupPath, pages.SignOffLine(foundingFreeMonths), f.Signup},
	}
	for _, p := range pages.LegalPages {
		out = append(out, shareCardPage{p.Path, p.Lede, f.Legal})
	}
	out = append(out,
		shareCardPage{pages.APIGuidePath, pages.APIGuideDescription(false), f.API},
		shareCardPage{pages.APIGuideTRPath, pages.APIGuideDescription(true), f.API},
	)
	return out
}

// headMetaRE is one meta element of the head as documentHead writes it.
var headMetaRE = regexp.MustCompile(`<meta (?:name|property)="([^"]+)" content="([^"]*)">`)

// shareMeta is the share lines of body's head, by name, each value unescaped; a name
// written twice keeps both values.
func shareMeta(body string) map[string][]string {
	head := between(body, "<head>", "</head>")
	out := map[string][]string{}
	for _, m := range headMetaRE.FindAllStringSubmatch(head, -1) {
		if m[1] == "description" || strings.HasPrefix(m[1], "og:") || strings.HasPrefix(m[1], "twitter:") {
			out[m[1]] = append(out[m[1]], html.UnescapeString(m[2]))
		}
	}
	return out
}

var titleRE = regexp.MustCompile(`<title>([^<]*)</title>`)

// TestShareCards_EveryPublicPageCarriesItsCard: the thirteen pages each carry a
// description, og:type, og:title (the tab's title), og:description, og:image on the
// configured origin with its size, and the large card -- each line once -- and the image
// is the embedded file, served 200 under /static/og with the bare-name cache rule. The
// pages that are not shared carry none: the public 404 (noindex, a path somebody
// mistyped) and the wizard's later screens.
func TestShareCards_EveryPublicPageCarriesItsCard(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	// testBaseURL is an origin already; written here rather than derived through
	// shareOrigin, so a constructor drawing another host is red.
	origin := testBaseURL
	cards := shareCardPages()
	// The table is the surface: every URL the structural scans fetch, and sign-up.
	want := append(publicSurfaceURLs(), signupPath)
	var got []string
	for _, c := range cards {
		got = append(got, c.url)
	}
	sort.Strings(want)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) || len(got) != 13 {
		t.Fatalf("the share-card table covers %v, want the thirteen public pages %v", got, want)
	}
	static := web.Static()
	for _, c := range cards {
		rec := fetchMarketing(t, r, c.url, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", c.url, rec.Code)
		}
		body := rec.Body.String()
		meta := shareMeta(body)
		title := titleRE.FindStringSubmatch(body)
		if title == nil {
			t.Fatalf("GET %s has no <title>", c.url)
		}
		image := origin + "/static/" + c.image
		for name, value := range map[string]string{
			"description":     c.description,
			"og:type":         "website",
			"og:title":        html.UnescapeString(title[1]),
			"og:description":  c.description,
			"og:image":        image,
			"og:image:width":  "1200",
			"og:image:height": "630",
			"twitter:card":    "summary_large_image",
		} {
			if !reflect.DeepEqual(meta[name], []string{value}) {
				t.Errorf("GET %s: %s is %q, want exactly [%q]", c.url, name, meta[name], value)
			}
		}
		if c.description == "" {
			t.Errorf("%s: the description's source is empty", c.url)
		}
		if len(meta) != 8 {
			t.Errorf("GET %s carries share lines %v, want the eight above", c.url, meta)
		}
		// The image is the file the binary embeds, served where og:image says.
		raw, err := fs.ReadFile(static, c.image)
		if err != nil {
			t.Fatalf("%s: %s is not in the embedded tree: %v", c.url, c.image, err)
		}
		img := fetchMarketing(t, r, strings.TrimPrefix(image, origin), nil)
		if img.Code != http.StatusOK || img.Header().Get("Content-Type") != "image/png" || !bytes.Equal(img.Body.Bytes(), raw) {
			t.Errorf("GET %s = %d %q (%d bytes), want 200 image/png and the embedded file's %d bytes",
				strings.TrimPrefix(image, origin), img.Code, img.Header().Get("Content-Type"), img.Body.Len(), len(raw))
		}
		// og:image is the bare name (shareImagesFor says why), so its rule is the bare
		// name's: revalidate, with the ETag.
		if cc := img.Header().Get("Cache-Control"); cc != cacheRevalidateForTest || img.Header().Get("ETag") == "" {
			t.Errorf("GET %s: Cache-Control %q, ETag %q; want %q and an ETag", strings.TrimPrefix(image, origin),
				cc, img.Header().Get("ETag"), cacheRevalidateForTest)
		}
	}

	// The pages that are not shared.
	nf := fetchMarketing(t, r, "/no-such-page", nil)
	if nf.Code != http.StatusNotFound {
		t.Fatalf("PREMISE: the public 404 answered %d", nf.Code)
	}
	if m := shareMeta(nf.Body.String()); len(m) != 0 {
		t.Errorf("the public 404 carries share lines %v; it is noindex and nobody shares a mistyped path", m)
	}
	shared := 0
	for name, body := range signupSurfaceScreens(t) {
		if len(shareMeta(body)) != 0 {
			shared++
			if name != "business" {
				t.Errorf("the sign-up %s screen carries share lines; only the first step is a page anybody shares", name)
			}
		}
	}
	if shared != 1 {
		t.Errorf("%d sign-up screen(s) carry share lines, want the first step's", shared)
	}
}

// cacheRevalidateForTest is internal/httpx's bare-name rule, a literal so this test is
// not tied to the constant it checks.
const cacheRevalidateForTest = "no-cache"

// TestShareCards_NoOriginNoImage: a deployment whose base URL is not an http(s) URL with
// a host, or a file the tree does not carry, draws no image -- the description and the
// title stay -- rather than an og:image a crawler cannot fetch.
func TestShareCards_NoOriginNoImage(t *testing.T) {
	t.Parallel()
	for base, want := range map[string]string{
		"":                           "",
		"http://localhost:8080":      "http://localhost:8080",
		"https://taptime.mt/":        "https://taptime.mt",
		"https://taptime.mt/app?x=1": "https://taptime.mt",
		"localhost:8080":             "",
		"ftp://taptime.mt":           "",
		"https://":                   "",
		"javascript:alert(1)":        "",
	} {
		if got := shareOrigin(base); got != want {
			t.Errorf("shareOrigin(%q) = %q, want %q", base, got, want)
		}
	}
	fsys := fstest.MapFS{"og/landing.png": {Data: []byte("x")}, "og/product.png": {Mode: fs.ModeDir}}
	imgs := shareImagesFor(fsys, "https://taptime.mt")
	if imgs.Landing != "https://taptime.mt/static/og/landing.png" || imgs.Product != "" || imgs.Pricing != "" {
		t.Errorf("shareImagesFor over a tree with one file = %+v", imgs)
	}
	if got := shareImagesFor(web.Static(), ""); got != (pages.ShareImages{}) {
		t.Errorf("shareImagesFor with no origin = %+v, want none", got)
	}
	r := marketingRouterForBase(t, "")
	m := shareMeta(mustFetchMarketing(t, r, "/"))
	if len(m["og:image"]) != 0 || len(m["twitter:card"]) != 0 || len(m["description"]) != 1 {
		t.Errorf("with no origin / carries %v; want the description and no image", m)
	}
}

// marketingRouterForBase is the marketing router with the public surface's origin set to
// base (TAPPA_BASE_URL), no legal texts and no API host.
func marketingRouterForBase(t *testing.T, base string) http.Handler {
	t.Helper()
	return httpx.NewRouter(nil, nil, NewMarketing(nil, nil, "", base))
}

// pngSize reads a PNG's width and height from its signature and IHDR chunk.
func pngSize(b []byte) (w, h int, ok bool) {
	sig := []byte("\x89PNG\r\n\x1a\n")
	if len(b) < 24 || !bytes.Equal(b[:8], sig) || string(b[12:16]) != "IHDR" {
		return 0, 0, false
	}
	return int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24])), true
}

// TestShareImages_AreNinePNGsAt1200x630: web/static/og holds exactly the nine files
// pages.ShareImageFiles names, each a PNG whose header says 1200×630
// (layout.ShareImageWidth/Height). CONTROL: a header one pixel off, and a file that is
// not a PNG, are refused by the reader.
func TestShareImages_AreNinePNGsAt1200x630(t *testing.T) {
	t.Parallel()
	named := map[string]bool{}
	v := reflect.ValueOf(pages.ShareImageFiles)
	for i := 0; i < v.NumField(); i++ {
		named[v.Field(i).String()] = true
	}
	if len(named) != 9 {
		t.Fatalf("pages.ShareImageFiles names %d distinct file(s), want 9", len(named))
	}
	static := web.Static()
	entries, err := fs.ReadDir(static, "og")
	if err != nil {
		t.Fatalf("reading og: %v", err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		onDisk["og/"+e.Name()] = true
	}
	if !reflect.DeepEqual(onDisk, named) {
		t.Errorf("web/static/og holds %v, want exactly %v", onDisk, named)
	}
	for name := range named {
		raw, err := fs.ReadFile(static, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		w, h, ok := pngSize(raw)
		if !ok || w != layout.ShareImageWidth || h != layout.ShareImageHeight {
			t.Errorf("%s is %dx%d (png=%v), want %dx%d", name, w, h, ok, layout.ShareImageWidth, layout.ShareImageHeight)
		}
	}
	// CONTROL.
	good, _ := fs.ReadFile(static, pages.ShareImageFiles.Landing)
	off := bytes.Clone(good)
	binary.BigEndian.PutUint32(off[20:24], 631)
	if w, h, ok := pngSize(off); ok && w == 1200 && h == 630 {
		t.Error("CONTROL: a 1200x631 header read as 1200x630")
	}
	if _, _, ok := pngSize([]byte("GIF89a................................")); ok {
		t.Error("CONTROL: a GIF read as a PNG")
	}
}

// shareCopyProblems is every public-copy rule a description is held to: no unmeasured
// number or guarantee, no superiority claim, the free months as K6 counts them, none of
// the sentences the page reviews withdrew, no glove, nothing K2 keeps off the site, no
// price (the price is derived; a card is shared and cached far from the page that prints
// it), and no internal name -- the product is Taptime to a reader.
func shareCopyProblems(text string) []string {
	var out []string
	if hit := unmeasuredNumber.FindString(text); hit != "" {
		out = append(out, "unmeasured claim "+hit)
	}
	if hit := superiorityClaim.FindString(text); hit != "" {
		out = append(out, "superiority claim "+hit)
	}
	out = append(out, freeMonthsCopyProblems(text)...)
	for _, refused := range []map[string]string{securityRefusedPhrasings, forProductionRefusedPhrasings} {
		for phrase := range refused {
			if strings.Contains(text, phrase) {
				out = append(out, "withdrawn "+phrase)
			}
		}
	}
	if gloveRE.MatchString(text) {
		out = append(out, "glove")
	}
	lower := strings.ToLower(text)
	for _, word := range k2Forbidden {
		if strings.Contains(lower, word) {
			out = append(out, "K2 "+word)
		}
	}
	if strings.Contains(text, publishedPricePerEmployeeMonth) || strings.ContainsAny(text, "€$£") {
		out = append(out, "price")
	}
	if hit := internalNameRE.FindString(text); hit != "" {
		out = append(out, "internal name "+hit)
	}
	return out
}

// internalNameRE is a word a reader of the public site should never meet: the
// repository's own name, the schema's, a policy id.
var internalNameRE = regexp.MustCompile(`(?i)\btappa\b|\btenants?\b|\b(?:sys|base):[a-z]|\bNTAG\b|\bRLS\b`)

// TestShareCards_DescriptionsPassTheCopyRules: every description and og:title, as
// rendered, passes shareCopyProblems. CONTROL: each rule's shape planted in a description
// is red.
func TestShareCards_DescriptionsPassTheCopyRules(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, c := range shareCardPages() {
		meta := shareMeta(fetchMarketing(t, r, c.url, nil).Body.String())
		for _, name := range []string{"description", "og:title", "og:description"} {
			for _, v := range meta[name] {
				for _, p := range shareCopyProblems(v) {
					t.Errorf("%s %s %q: %s", c.url, name, v, p)
				}
			}
		}
	}
	for _, planted := range []string{
		"Read every punch from a feed.",
		"Start yourself today — first 3 months free.",
		"Four proofs on every single tap.",
		"We guarantee your records.",
		"More accurate than a punch clock.",
		"Hosted in Falkenstein.",
		"Works with gloves on.",
		"From " + publishedPricePerEmployeeMonth + " a person.",
		"Every tenant gets a plaque.",
		"Taptime, built on Tappa.",
	} {
		if len(shareCopyProblems(planted)) == 0 {
			t.Errorf("CONTROL: %q passed the description rules", planted)
		}
	}
}

// TestShareImageExemption_IsNarrow is the control for TestMarketing_ReachesNoThirdParty's
// og:image exemption: it admits the whole og:image element with exactly a URL the
// constructor resolved, and nothing else -- another host, another path on our origin, an
// unnamed file under /static/og, the same URL in another element or attribute all stay
// in front of the scanner.
func TestShareImageExemption_IsNarrow(t *testing.T) {
	t.Parallel()
	urls := shareImageURLs(expectedShareImages(testBaseURL))
	if len(urls) != 9 {
		t.Fatalf("PREMISE: %d share URLs, want 9", len(urls))
	}
	// The constructor resolves exactly these for the test origin.
	if got := shareImagesFor(web.Static(), testBaseURL); got != expectedShareImages(testBaseURL) {
		t.Errorf("shareImagesFor(%q) = %+v, want %+v", testBaseURL, got, expectedShareImages(testBaseURL))
	}
	origin := testBaseURL
	ok := `<meta property="og:image" content="` + urls[0] + `">`
	if body, n := withoutShareImages(ok, urls); n != 1 || externalReference.FindString(body) != "" {
		t.Errorf("the exact og:image element was not admitted (%d, %q)", n, body)
	}
	for _, planted := range []string{
		`<meta property="og:image" content="https://evil.example/static/og/landing.png">`,
		`<meta property="og:image" content="` + strings.Replace(urls[0], origin, "https://cdn.example.test", 1) + `">`,
		`<meta property="og:image" content="` + origin + `/static/js/landing.js">`,
		`<meta property="og:image" content="` + origin + `/static/og/other.png">`,
		`<meta property="og:image" content="` + origin + `/static/og/landing.png?u=https://evil.example">`,
		`<meta name="twitter:image" content="` + urls[0] + `">`,
		`<link rel="preload" href="` + urls[0] + `">`,
		`<img src="` + urls[0] + `">`,
		`<meta property="og:url" content="` + origin + `/">`,
	} {
		body, _ := withoutShareImages(planted, urls)
		if externalReference.FindString(body) == "" {
			t.Errorf("CONTROL: %q passed the scanner after the exemption", planted)
		}
	}
}

// --- what the images draw ----------------------------------------------------------------
//
// The PNGs are drawn from the ShareCards board's markup (K3, user decision of 2026-10-23):
// each card is the board's 1200×630 element with the edits below applied, rendered at
// 1200×630. A test cannot read text out of a PNG, so it reads what the PNG was drawn
// FROM: the board in docs/design and this table. The extractor that drew the committed
// files reads this table (between the two marker comments) rather than a copy of it.

// shareCardNames is the board's cards in its order -- the order of pages.ShareImageFiles.
var shareCardNames = []string{"landing", "product", "pricing", "security", "restaurants", "production", "signup", "legal", "api"}

// shareCardBoardEdits is every change the drawn cards make to the board's markup, by card.
// Each is there because a public page says otherwise:
//   - security: the security review (LP-07) retracted "Four proofs on every single tap."
//     -- a QR check-in carries no signature -- and /security opens with this heading;
//   - product: Grace Attard is a person in the seed (invited, never tapped), and sample
//     people are invented (samplePeopleProblems); The Pass's flagged 09:41 docket is
//     Claire Mifsud's (pages.LandingPanelPassDockets);
//   - api: the API writes a location at /v1/locations/{ref}; it serves no /v1/locations.
//
// share-card-edits:begin
var shareCardBoardEdits = map[string][][2]string{
	"security": {{"Four proofs on every single tap.", "Four checks behind every tap."}},
	"product":  {{"Grace Attard", "Claire Mifsud"}},
	"api":      {{">/v1/locations<", ">/v1/locations/{ref}<"}},
}

// share-card-edits:end

// shareCardSources is each card's markup as drawn: the board's element with its edits
// applied. An edit that no longer matches the board is a fatal error, so the table cannot
// go stale against a board that changed.
func shareCardSources(t *testing.T) map[string]string {
	t.Helper()
	board := repoFile(t, "docs", "design", "landing-redesign", "boards", "ShareCards.dc.html")
	const open = `<div role="img" aria-label="Share image for `
	const caption = `<div style="width: 1200px; box-sizing: border-box; background: #FFFDF4; border: 1px solid #C9D2C8; border-top: 0;`
	parts := strings.Split(board, open)[1:]
	if len(parts) != len(shareCardNames) {
		t.Fatalf("the board draws %d card(s), want %d", len(parts), len(shareCardNames))
	}
	out := map[string]string{}
	for i, p := range parts {
		end := strings.Index(p, caption)
		if end < 0 {
			t.Fatalf("card %d has no caption after it; the board changed shape", i)
		}
		card := open + p[:end]
		for _, e := range shareCardBoardEdits[shareCardNames[i]] {
			if !strings.Contains(card, e[0]) {
				t.Fatalf("the %s card's edit %q matches nothing on the board", shareCardNames[i], e[0])
			}
			card = strings.ReplaceAll(card, e[0], e[1])
		}
		out[shareCardNames[i]] = card
	}
	return out
}

// shareCardPersonRE is a person's name on a drawn docket (the landing's and the tour's).
var shareCardPersonRE = regexp.MustCompile(`<span style="font-size: 22px; font-weight: 700">([^<]+)</span>`)

// TestShareCards_TheImagesSayWhatThePagesSay: what each card draws passes the copy rules
// the descriptions do (shareCopyProblems: no retracted sentence, no price, no internal
// name, ...), names no person of the seed, and every person it does name is one of the
// landing panel's invented samples. CONTROL: the board unedited -- Grace Attard and "Four
// proofs on every single tap." -- is red.
func TestShareCards_TheImagesSayWhatThePagesSay(t *testing.T) {
	t.Parallel()
	seed := seedPeople(t)
	samples := map[string]bool{}
	for _, d := range pages.LandingPanelPassDockets {
		samples[d.Who] = true
	}
	problems := func(cards map[string]string) []string {
		var out []string
		people := 0
		for _, name := range shareCardNames {
			card := cards[name]
			text := screenText(t, "<body>"+card+"</body>")
			for _, p := range shareCopyProblems(text) {
				out = append(out, name+": "+p)
			}
			for _, s := range seed {
				if strings.Contains(text, s) {
					out = append(out, name+": draws "+s+", a person in the seed")
				}
			}
			for _, m := range shareCardPersonRE.FindAllStringSubmatch(card, -1) {
				people++
				if !samples[m[1]] {
					out = append(out, name+": draws "+m[1]+", who is not one of the landing panel's samples")
				}
			}
		}
		if people != 3 {
			out = append(out, fmt.Sprintf("PREMISE: the cards name %d people, want 3 (landing 1, product 2)", people))
		}
		return out
	}
	cards := shareCardSources(t)
	for _, p := range problems(cards) {
		t.Error(p)
	}
	// CONTROL: the board as drawn, before the edits.
	raw := map[string]string{}
	for name, card := range cards {
		for _, e := range shareCardBoardEdits[name] {
			card = strings.ReplaceAll(card, e[1], e[0])
		}
		raw[name] = card
	}
	got := strings.Join(problems(raw), "\n")
	for _, want := range []string{"draws Grace Attard", "Four proofs on every single tap"} {
		if !strings.Contains(got, want) {
			t.Errorf("CONTROL: the unedited board was not refused for %q; got:\n%s", want, got)
		}
	}
}
