package operator_test

// oplegal_test.go -- M13-14: the legal texts' screen in the redesign. docs/plan/m13-operator-redesign.md,
// card M13-14; docs/design/operator-redesign README §6 "Card", §6.1 "Legal editor", "Legal versions",
// screens.md §12.

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// The legal screen's intro card, its editor cards and its docket, and the pieces of them screens.md
// §12 names, each a whole tag. The intro card spaces its blocks (README §6 "Card": gap-3.5 on the
// markup, no mt-* in the bodies); the h1 is the board's 26 / 28 / 32 px (orchestrator decision
// (e)). The editor card is the one operator card with no padding and no gap (README §6 "Card"
// exception, §6.1 "Legal editor"): its bands carry the card scale's inset, 20 px on the phone and
// 24 px from the tablet -- the head (the path, the decorative dot and the state's words), the
// warning's band, the form (the framed text under its label bar, the help and the button, the
// whole width on the phone). The docket's empty sentence and its cap note are the boards' own.
const (
	legalCard       = `<section class="op-card flex flex-col gap-3.5">`
	legalLabel      = `<p class="op-label">Legal texts</p>`
	legalH1         = `<h1 class="font-display text-[26px] font-bold leading-[1.1] tracking-[-0.03em] md:text-[28px] desk:text-[32px]">Taptime's legal texts</h1>`
	legalIntro      = `<p class="text-[15px] leading-[1.55]">`
	legalIntroWords = `Publishing adds a new version; nothing is overwritten. To undo a change, publish the earlier text again.`
	legalPublic     = `<p class="text-sm text-ink/70">The public pages are on the customer site, under <span class="font-mono">/legal</span>.</p>`
	legalEditorOpen = `<section class="op-card flex flex-col gap-0 overflow-hidden p-0" aria-labelledby="legal-`
	legalHead       = `<div class="flex flex-wrap items-center justify-between gap-x-5 gap-y-1.5 border-b border-line px-5 pt-[18px] pb-3.5 md:px-6 md:pt-5 md:pb-4">`
	legalPath       = `" class="min-w-0 font-mono text-[17px] font-bold leading-[1.3] [overflow-wrap:anywhere] md:text-lg">`
	legalState      = `<p class="flex min-w-0 items-start gap-2 text-sm leading-[1.4] text-ink/75">`
	legalDotLive    = `<span class="mt-1.5 size-2 flex-none rounded-full bg-tappa-green" aria-hidden="true"></span>`
	legalDotNone    = `<span class="mt-1.5 size-2 flex-none rounded-full border-[1.5px] border-ink/45" aria-hidden="true"></span>`
	legalWarnBand   = `<div class="px-5 pt-4 md:px-6">`
	legalFormTag    = `<form method="post" action="/operator/legal" class="flex flex-col gap-3 px-5 pt-[18px] pb-[22px] md:px-6">`
	legalFrame      = `<div class="overflow-hidden rounded-[10px] border border-line bg-paper focus-within:border-tappa-green focus-within:ring-1 focus-within:ring-tappa-green">`
	legalBar        = `<div class="flex min-h-9 items-center border-b border-line bg-ink/[0.04] px-4">`
	legalTextarea   = `" name="body" rows="12" required class="op-input block resize-y rounded-none border-0 bg-transparent px-3.5 py-[18px] leading-[1.7] focus:ring-0 md:px-5 desk:px-[22px]">`
	legalFoot       = `<div class="flex flex-wrap items-center justify-between gap-x-5 gap-y-3">`
	legalHelp       = `<p class="min-w-0 flex-[1_1_220px] text-xs leading-[1.45] text-ink/70">`
	legalHelpWords  = `A blank line starts a new paragraph. Addresses starting with http:// or https://, e-mail addresses and phone numbers ` +
		`starting with + become links; <span class="font-mono">[text](https://…)</span> links your own text. At most 256 KiB.`
	legalButton = `<button type="submit" class="btn btn--primary w-full md:w-auto">Publish a new version</button>`
	legalEmpty  = `<p class="pt-[22px] pb-2 font-display text-[15px] leading-normal">Nothing has been published yet.</p>`
	legalCap    = `<p class="mt-1 border-t-2 border-dashed border-line pt-3 font-display text-[13px] text-ink/70">The newest <span class="font-mono">`
)

// legalWantColumn is the whole content column of v, every editor and version written out: the
// intro card, one editor card per document (its head with the path and the state, the warning when
// the public page is behind, the form with its hidden slug, the text under its label bar, the help
// and the button), and the docket -- its pinned heading, the empty sentence or the rows (each the
// four bare spans and, on a live version, the chip), and the cap note when the list is full.
func legalWantColumn(v operatorpages.LegalView) string {
	e := html.EscapeString
	s := legalCard + legalLabel + legalH1 + legalIntro + legalIntroWords + `</p>` + legalPublic +
		`<p class="text-sm"><a href="/operator" class="op-link">Back to the console</a></p></section>`
	for _, d := range v.Docs {
		s += legalEditorOpen + e(d.Slug) + `">` + legalHead + `<h2 id="legal-` + e(d.Slug) + legalPath + e(d.Path) + `</h2>` + legalState
		if d.Published {
			s += legalDotLive + `<span>Live since <span class="font-mono text-[13px] text-ink">` + e(d.PublishedAt) + `</span></span></p>`
		} else {
			s += legalDotNone + `<span>Not published. The public page shows its placeholder.</span></p>`
		}
		s += `</div>`
		if d.Behind {
			s += legalWarnBand + `<section role="status" class="op-notice op-notice--warn"><p class="font-display font-bold tracking-tight">` +
				`The public page is behind</p><div class="mt-1 text-sm text-ink/85"><p>The newest version, published <span class="font-mono">` +
				e(d.LiveAt) + `</span>, has not reached the public page yet. This editor opened on the text the page shows, so publishing ` +
				`it replaces the newest version.</p><p class="mt-2">Reload this screen to try again. The page also catches up after the ` +
				`next publication or a restart.</p></div></section></div>`
		}
		s += legalFormTag + `<input type="hidden" name="slug" value="` + e(d.Slug) + `">` + legalFrame + legalBar +
			`<label for="legal-body-` + e(d.Slug) + `" class="docket-label">Text</label></div><textarea id="legal-body-` + e(d.Slug) +
			legalTextarea + e(d.Body) + `</textarea></div>` + legalFoot + legalHelp + legalHelpWords + `</p>` + legalButton +
			`</div></form></section>`
	}
	s += `<section class="docket" aria-labelledby="legal-versions"><h2 id="legal-versions" class="docket-label">Versions, newest first</h2>`
	if len(v.Versions) == 0 {
		s += legalEmpty
	} else {
		s += `<ol>`
		for _, r := range v.Versions {
			s += `<li class="op-version"><span>` + e(r.PublishedAt) + `</span><span>` + e(r.Path) + `</span><span>` + e(r.Bytes) +
				`</span><span>` + e(r.Publisher) + `</span>`
			if r.Current {
				s += `<span class="tally tally--active">Live</span>`
			}
			s += `</li>`
		}
		s += `</ol>`
		if v.Limit > 0 && len(v.Versions) >= v.Limit {
			s += legalCap + strconv.Itoa(v.Limit) + `</span> are listed. Older versions are not shown here.</p>`
		}
	}
	return s + `</section>`
}

// legalNotShown is screens.md §12's "Not shown" list as words: an old version's text, a preview, a
// diff, publishing a version again.
var legalNotShown = regexp.MustCompile(`(?i)\b(?:preview(?:ed)?|diff|differences?|compare|republish(?:ed)?|restore|revert|roll ?back|this version|view text|show text)\b`)

// legalTextareaTag is a textarea's opening tag and its attributes.
var legalTextareaTag = regexp.MustCompile(`<textarea\b([^>]*)>`)

// legalAttrs is one opening tag's attributes, in order, as name="value" (a bare name as itself).
var legalAttrs = regexp.MustCompile(`\s([a-z-]+)(?:="([^"]*)")?`)

// legalScreenFindings is TestLegalScreen_IsTheIntroCardTheEditorsAndTheDocket's reading of one
// render of v: one finding per way it departs from what legalWantColumn says.
func legalScreenFindings(name, body string, v operatorpages.LegalView) []string {
	var f []string
	bad := func(format string, a ...any) { f = append(f, name+": "+fmt.Sprintf(format, a...)) }
	start, end := strings.Index(body, "<main"), strings.Index(body, "</main>")
	if start < 0 || end < start || strings.Count(body, "<main") != 1 {
		return []string{name + ": no single <main>"}
	}
	column := body[start : end+len("</main>")]
	open := regexp.MustCompile(`^<main [^>]*>`).FindString(column)
	if !strings.Contains(open, `class="op-main `) {
		return []string{name + ": the column opens with " + open}
	}
	got, want := tight(column[len(open):len(column)-len("</main>")]), tight(legalWantColumn(v))
	if got != want {
		at := 0
		for at < len(got) && at < len(want) && got[at] == want[at] {
			at++
		}
		lo := max(at-80, 0)
		bad("the column departs from screens.md §12 at byte %d:\n  got  …%s\n  want …%s", at, got[lo:min(at+120, len(got))],
			want[lo:min(at+120, len(want))])
	}

	// The page's forms: the bar's sign-out and one editor per document -- each a POST with method
	// before action, one button each; no hx-.
	forms := 1 + len(v.Docs)
	if strings.Count(body, "<form") != forms || strings.Count(body, `<form method="post" action="/operator/`) != forms ||
		strings.Count(body, "<button") != forms || strings.Contains(body, "hx-") {
		bad("%d form(s) (%d POST to the surface), %d button(s) or an hx- attribute on the page; want the sign-out and %d editor(s)",
			strings.Count(body, "<form"), strings.Count(body, `<form method="post" action="/operator/`), strings.Count(body, "<button"), len(v.Docs))
	}
	// Each editor's controls: the hidden slug and the text, whose attributes are exactly these.
	if n := strings.Count(column, "<input"); n != len(v.Docs) || strings.Count(column, `<input type="hidden" name="slug" value="`) != len(v.Docs) {
		bad("%d input(s) in the column, want one hidden slug per document (%d)", n, len(v.Docs))
	}
	tags := legalTextareaTag.FindAllStringSubmatch(column, -1)
	if len(tags) != len(v.Docs) {
		bad("%d text area(s), want one per document (%d)", len(tags), len(v.Docs))
	}
	for i, tag := range tags {
		var names []string
		for _, a := range legalAttrs.FindAllStringSubmatch(tag[1], -1) {
			names = append(names, a[1])
		}
		if i < len(v.Docs) {
			if want := "id name rows required class"; strings.Join(names, " ") != want || !strings.Contains(tag[1], ` name="body" rows="12" required `) ||
				!strings.Contains(tag[1], ` id="legal-body-`+html.EscapeString(v.Docs[i].Slug)+`"`) {
				bad("text area %d carries %q, want exactly %s with name body, 12 rows, required and its document's id", i+1, tag[1], want)
			}
		}
	}
	for _, m := range regexp.MustCompile(`\shref="([^"]*)"`).FindAllStringSubmatch(column, -1) {
		if m[1] != "/operator" {
			bad("the column links %q (the console only; the public pages are text, not a link)", m[1])
		}
	}
	// Nothing hidden: no hidden attribute or class, no sr-only, no invisible; aria-hidden only on
	// the one decorative dot of each editor, which holds nothing.
	for _, c := range regexp.MustCompile(`\sclass="([^"]*)"`).FindAllStringSubmatch(column, -1) {
		for _, u := range strings.Fields(c[1]) {
			if base := u[strings.LastIndex(u, ":")+1:]; base == "hidden" || base == "sr-only" || base == "invisible" || base == "collapse" {
				bad("the column carries the class %q", u)
			}
		}
	}
	if regexp.MustCompile(`<[a-z]+\b[^>]*\shidden[\s>=/]`).MatchString(column) {
		bad("the column carries a hidden attribute")
	}
	dots := strings.Count(column, legalDotLive) + strings.Count(column, legalDotNone)
	if strings.Count(column, "aria-hidden") != len(v.Docs) || dots != len(v.Docs) {
		bad("%d aria-hidden and %d empty status dot(s) in the column, want one dot per document (%d) and no other aria-hidden",
			strings.Count(column, "aria-hidden"), dots, len(v.Docs))
	}
	for _, banned := range []string{"<svg", "<img", "<canvas", "<picture", "<select", "<datalist", "<table", "<script", "<iframe",
		"<details", "<dialog", "<pre", "tabindex", "contenteditable", "download", "onclick", "formaction", "readonly", "disabled",
		`type="checkbox"`, `type="radio"`, `type="text"`} {
		if strings.Contains(column, banned) {
			bad("the column carries %q", banned)
		}
	}
	if strings.Count(column, "role=") != strings.Count(column, `<section role="status" class="op-notice op-notice--warn">`) {
		bad("the column carries a role other than a behind warning's")
	}
	live := 0
	for _, r := range v.Versions {
		if r.Current {
			live++
		}
	}
	if strings.Count(column, "tally--") != live || strings.Count(column, `<span class="tally tally--active">Live</span>`) != live {
		bad("%d chip(s) in the column, want a Live chip on each live version (%d)", strings.Count(column, "tally--"), live)
	}
	text := html.UnescapeString(noticeTag.ReplaceAllString(regexp.MustCompile(`(?s)<textarea\b.*?</textarea>`).ReplaceAllString(column, " "), " "))
	if m := legalNotShown.FindString(text); m != "" {
		bad("the column says %q (screens.md §12 Not shown: an old version's text, a preview, a diff, publishing a version again)", m)
	}
	return f
}

// legalLongView is a render of the legal screen's longest values, for the column test and the
// screenshot round: a path of 120 characters and a publisher's name of 130 with no space, a text of
// 256 KiB less one byte with no space in it, a document not published, the behind warning, and a
// full list of 100 versions with its cap note.
func legalLongView() operatorpages.LegalView {
	long := "/legal/" + strings.Repeat("p", 113)
	v := operatorpages.LegalView{Limit: 100, Docs: []operatorpages.LegalDoc{
		{Slug: "privacy", Path: long, Published: true, PublishedAt: "2026-10-25 23:59 UTC", Body: strings.Repeat("x", 256*1024-1),
			Behind: true, LiveAt: "2026-10-26 00:01 UTC"},
		{Slug: "terms", Path: "/legal/terms", Body: "FAKE terms.\n\nA second paragraph."},
	}}
	for i := 0; i < 100; i++ {
		v.Versions = append(v.Versions, operatorpages.LegalVersionRow{Path: long, PublishedAt: "2026-10-25 23:59 UTC",
			Bytes: "262143 bytes", Publisher: strings.Repeat("M", 130), Current: i == 0})
	}
	return v
}

// TestLegalScreen_IsTheIntroCardTheEditorsAndTheDocket (M13-14; screens.md §12, README §6 "Card",
// §6.1 "Legal editor", "Legal versions"): each legal state -- screens()'s two "legal" renders
// (nothing published; one document live and one not, a full list of one with a live version), and
// renders here of the behind warning, of four documents with a mixed list, and of long values (a
// spaceless path of 120 characters and publisher of 130, a text of 256 KiB less a byte, the warning,
// 100 versions and the cap note) -- is the frame's column holding exactly, tags and words,
// legalWantColumn: the intro card (the op-label, the h1 at 26 / 28 / 32 px, the intro, /legal as
// text, the way back; no top margin), one editor card per document with no padding and no gap
// (the head band with the path, the dot and the state's words; the warning's band; the form band
// with the hidden slug, the framed text under its TEXT bar, the help in ADR 0033's words, the
// button the whole width on the phone), and the docket (its pinned heading, the empty sentence or
// the rows -- <li class="op-version"> with four bare spans and the Live chip on a live version --
// and the cap note). The page: the sign-out and one editor per document are its forms and buttons,
// each a POST to the surface; each text area's attributes are exactly id, name, rows, required and
// class (name body, 12 rows, required); its one link is the console's. Nowhere in the column:
// hidden content (a hidden attribute or class, sr-only, an aria-hidden but the editors' empty
// dots), a drawing or image, a select, a table, a script, a frame, a disclosure or a dialog, a text
// input, a role but the warning's, a chip but Live, the words of screens.md §12's Not shown.
// CONTROLS, each read by the same function and each a finding: a word of the intro changed; the
// intro given back its top margin; the h1 back to its earlier size; the editor given the card's
// padding; the editor given a gap; the text's rows changed; its required dropped; its name
// changed; a maxlength added; the hidden slug dropped; a word of the help changed; the warning
// moved under the form; the dot's aria-hidden dropped; aria-hidden on the state's words; the
// version list hidden; sr-only on a path; the button not the whole width on the phone; a preview
// button; a republish form on a version; Live re-toned; the empty sentence's words changed; the
// cap note's words changed; a link to the customer host.
//
// PART II -- red on any of the above. PART III -- what the screen looks like at each width is the
// screenshot round's (320 / 390 / 834 / 1200 / 1440 px), the docket's rules
// TestLegalVersions_TheScopedRulesKeepTheBoardsColumns'; the warning's words are op10_test.go's and
// opnotice_test.go's -- no completeness claim.
func TestLegalScreen_IsTheIntroCardTheEditorsAndTheDocket(t *testing.T) {
	all := screens(t)
	states := map[string]operatorpages.LegalView{
		"legal": {Limit: 100},
		"legal, published": {Limit: 1,
			Docs: []operatorpages.LegalDoc{{Slug: "privacy", Path: "/legal/privacy", Published: true, PublishedAt: "2026-10-03 09:30 UTC", Body: "T"},
				{Slug: "terms", Path: "/legal/terms"}},
			Versions: []operatorpages.LegalVersionRow{{Path: "/legal/privacy", PublishedAt: "2026-10-03 09:30 UTC", Bytes: "1 bytes",
				Publisher: "tenant admin (legacy)", Current: true}}},
	}
	for name := range all {
		if _, ok := states[name]; strings.HasPrefix(name, "legal") && !ok {
			t.Fatalf("PREMISE: screens() has a legal render %q this test does not read", name)
		}
	}
	states["legal, behind"] = operatorpages.LegalView{Limit: 100, Docs: []operatorpages.LegalDoc{{Slug: "cookies", Path: "/legal/cookies",
		Published: true, PublishedAt: "2026-10-03 09:30 UTC", Body: "FAKE cookie notice, version one.", Behind: true, LiveAt: "2026-10-03 09:31 UTC"}},
		Versions: []operatorpages.LegalVersionRow{
			{Path: "/legal/cookies", PublishedAt: "2026-10-03 09:31 UTC", Bytes: "32 bytes", Publisher: "Ada FAKE Operator", Current: true},
			{Path: "/legal/cookies", PublishedAt: "2026-10-03 09:30 UTC", Bytes: "32 bytes", Publisher: "Ada FAKE Operator"}}}
	states["legal, four documents"] = operatorpages.LegalView{Limit: 100, Docs: []operatorpages.LegalDoc{
		{Slug: "privacy", Path: "/legal/privacy", Published: true, PublishedAt: "2026-10-20 14:12 UTC", Body: "FAKE privacy.\n\nSecond."},
		{Slug: "terms", Path: "/legal/terms", Published: true, PublishedAt: "2026-09-30 16:05 UTC", Body: "FAKE terms."},
		{Slug: "imprint", Path: "/legal/imprint", Published: true, PublishedAt: "2026-07-26 09:00 UTC", Body: "FAKE imprint."},
		{Slug: "cookies", Path: "/legal/cookies"}},
		Versions: []operatorpages.LegalVersionRow{
			{Path: "/legal/privacy", PublishedAt: "2026-10-20 14:12 UTC", Bytes: "18432 bytes", Publisher: "Mara FAKE Vella", Current: true},
			{Path: "/legal/terms", PublishedAt: "2026-09-30 16:05 UTC", Bytes: "24876 bytes", Publisher: "operator", Current: true},
			{Path: "/legal/imprint", PublishedAt: "2026-07-26 09:00 UTC", Bytes: "1204 bytes", Publisher: "tenant admin (legacy)", Current: true},
			{Path: "/legal/privacy", PublishedAt: "2026-07-26 08:58 UTC", Bytes: "15022 bytes", Publisher: "tenant admin (legacy)"}}}
	states["long values"] = legalLongView()
	for _, name := range []string{"legal, behind", "legal, four documents", "long values"} {
		var b strings.Builder
		if err := operatorpages.Legal(states[name]).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		all[name] = b.String()
	}
	for name, v := range states {
		body, ok := all[name]
		if !ok {
			t.Fatalf("PREMISE: no render %q", name)
		}
		for _, finding := range legalScreenFindings(name, body, v) {
			t.Error(finding)
		}
	}
	if !strings.Contains(all["long values"], strings.Repeat("x", 256*1024-1)+"</textarea>") ||
		strings.Count(all["long values"], `<li class="op-version">`) != 100 {
		t.Fatal("PREMISE: the long render does not carry the 256 KiB text and the 100 versions")
	}

	const behindWarn = `<div class="px-5 pt-4 md:px-6"><section role="status" class="op-notice op-notice--warn">`
	for _, c := range []struct{ what, render, from, to string }{
		{"a word of the intro changed", "legal", "nothing is overwritten", "nothing is lost"},
		{"the intro's top margin back", "legal", legalIntro, `<p class="mt-3 text-[15px] leading-[1.55]">`},
		{"the h1 back to its earlier size", "legal", legalH1, `<h1 class="mt-1 font-display text-2xl font-bold tracking-tight">Taptime's legal texts</h1>`},
		{"the editor given the card's padding", "legal, published", `gap-0 overflow-hidden p-0"`, `gap-0 overflow-hidden"`},
		{"the editor given a gap", "legal, published", `flex flex-col gap-0 overflow-hidden`, `flex flex-col gap-3.5 overflow-hidden`},
		{"the text's rows changed", "legal, published", `rows="12"`, `rows="8"`},
		{"the text's required dropped", "legal, published", ` rows="12" required `, ` rows="12" `},
		{"the text's name changed", "legal, published", `name="body" rows`, `name="text" rows`},
		{"a maxlength on the text", "legal, published", `rows="12" required`, `rows="12" required maxlength="262144"`},
		{"the hidden slug dropped", "legal, published", `<input type="hidden" name="slug" value="privacy">`, ""},
		{"a word of the help changed", "legal, published", "become links;", "are linked;"},
		{"the warning moved under the form", "legal, behind", behindWarn, `<div class="px-5 pt-4 md:px-6 order-last"><section role="status" class="op-notice op-notice--warn">`},
		{"the dot's aria-hidden dropped", "legal, published", legalDotLive, `<span class="mt-1.5 size-2 flex-none rounded-full bg-tappa-green"></span>`},
		{"aria-hidden on the state's words", "legal, published", `<span>Live since`, `<span aria-hidden="true">Live since`},
		{"the version list hidden", "legal, published", `<ol><li class="op-version">`, `<ol hidden><li class="op-version">`},
		{"sr-only on a path", "legal, published", `[overflow-wrap:anywhere] md:text-lg">`, `[overflow-wrap:anywhere] md:text-lg sr-only">`},
		{"the button not the whole width on the phone", "legal, published", legalButton,
			`<button type="submit" class="btn btn--primary">Publish a new version</button>`},
		{"a preview button", "legal, published", `</div></form>`, `<a href="/operator/legal" class="op-link">Preview</a></div></form>`},
		{"a republish form on a version", "legal, published", `<span class="tally tally--active">Live</span>`,
			`<span class="tally tally--active">Live</span><form method="post" action="/operator/legal"><button type="submit" class="btn btn--quiet">Publish this version again</button></form>`},
		{"Live re-toned", "legal, published", `<span class="tally tally--active">Live</span>`, `<span class="tally tally--running">Live</span>`},
		{"the empty sentence's words changed", "legal", "Nothing has been published yet.", "Nothing is published yet."},
		{"the cap note's words changed", "long values", "Older versions are not shown here.", "Older versions are hidden."},
		{"a link to the customer host", "legal", `under <span class="font-mono">/legal</span>`,
			`under <a href="https://taptime.mt/legal" class="font-mono">/legal</a>`},
	} {
		mutated := strings.Replace(all[c.render], c.from, c.to, 1)
		if mutated == all[c.render] {
			t.Fatalf("CONTROL %s: %q is not on the page", c.what, c.from)
		}
		if len(legalScreenFindings("control", mutated, states[c.render])) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}

// legalVersionsRules is the legal docket's layout in web/static/css/input.css (README §6.1 "Legal
// versions"), each rule scoped from the docket's heading id, its selector and its utilities
// exactly: the heading's dashed rule; a row the boards' five columns from the tablet (164 / 136 /
// 92 / the rest / 64 px, 172 / 150 / 96 / the rest / 64 px on the desktop, 20 px apart, centred,
// 44 px tall at least), on the phone a wrapping row whose first line is the time and the chip, the
// path the whole second line, the length and the publisher the third; values the data face at
// 13 px, breaking anywhere; the time bold; the length at ink 70 %, never broken, set at its
// column's end from the tablet; the publisher Space Grotesk at 14 px; the chip at the row's end.
var legalVersionsRules = map[string][]string{
	`[aria-labelledby="legal-versions"] > h2`: {"border-b-2", "border-dashed", "border-line", "pb-3"},
	`[aria-labelledby="legal-versions"] .op-version`: {"gap-x-3.5", "gap-y-0.5", "py-3", "text-[13px]", "[overflow-wrap:anywhere]", "md:grid",
		"md:min-h-11", "md:grid-cols-[164px_136px_92px_minmax(0,1fr)_64px]", "md:items-center", "md:gap-x-5", "md:gap-y-1", "md:py-2.5",
		"desk:grid-cols-[172px_150px_96px_minmax(0,1fr)_64px]"},
	`[aria-labelledby="legal-versions"] .op-version > :first-child`:  {"-order-2", "flex-1", "font-bold", "md:order-none"},
	`[aria-labelledby="legal-versions"] .op-version > :nth-child(2)`: {"basis-full"},
	`[aria-labelledby="legal-versions"] .op-version > :nth-child(3)`: {"whitespace-nowrap", "text-ink/70", "md:text-right"},
	`[aria-labelledby="legal-versions"] .op-version > :nth-child(4)`: {"min-w-0", "font-display", "text-sm"},
	`[aria-labelledby="legal-versions"] .op-version > :nth-child(5)`: {"-order-1", "md:order-none", "md:justify-self-end"},
}

// TestLegalVersions_TheScopedRulesKeepTheBoardsColumns (M13-14; README §6.1 "Legal versions", boards
// OpLegal, T-OpLegal, M-OpLegal): in web/static/css/input.css (comments stripped, read as flat
// `selectors { @apply …; }` rules by readScopedRules), every rule whose selector names the legal
// docket's heading id is one of legalVersionsRules' seven, each exactly once, one @apply of exactly
// its utilities. CONTROLS, each read by the same function and each a finding: the row's rule
// deleted; the row not a grid from the tablet; the desktop's tracks changed; the tablet's tracks
// changed; a value with no space no longer breaking; the time not first on the phone; the chip
// not on the time's line on the phone; the time or the chip out of its column from the tablet
// (their order kept); the path not a line of its own on the phone; the length breaking; the
// length not at its column's end; the publisher in the data face; the chip not at the row's end;
// the heading's dashed rule solid; an eighth rule, in the double-quoted, the single-quoted and the
// unquoted spelling.
//
// PART II -- red on any of the above. PART III -- what the rules draw at each width is the
// screenshot round's; the markup is TestLegalScreen_IsTheIntroCardTheEditorsAndTheDocket's -- no
// completeness claim.
func TestLegalVersions_TheScopedRulesKeepTheBoardsColumns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	for _, f := range readScopedRules(css, "legal-versions", legalVersionsRules) {
		t.Error(f)
	}
	const row = `[aria-labelledby="legal-versions"] .op-version {`
	for _, c := range []struct{ what, from, to string }{
		{"the row's rule deleted", row, `.m1314-control-deleted {`},
		{"the row not a grid from the tablet", "[overflow-wrap:anywhere] md:grid md:min-h-11", "[overflow-wrap:anywhere] md:min-h-11"},
		{"the desktop's tracks changed", "desk:grid-cols-[172px_150px_96px_minmax(0,1fr)_64px]", "desk:grid-cols-[172px_150px_96px_minmax(0,1fr)_80px]"},
		{"the tablet's tracks changed", "md:grid-cols-[164px_136px_92px_minmax(0,1fr)_64px]", "md:grid-cols-[172px_150px_96px_minmax(0,1fr)_64px]"},
		{"a value with no space no longer breaking", "text-[13px] [overflow-wrap:anywhere] md:grid", "text-[13px] md:grid"},
		{"the time not first on the phone", "@apply -order-2 flex-1 font-bold", "@apply flex-1 font-bold"},
		{"the chip not on the time's line", "@apply -order-1 md:order-none md:justify-self-end", "@apply md:order-none md:justify-self-end"},
		{"the time out of its column from the tablet", "font-bold md:order-none;", "font-bold;"},
		{"the chip out of its column from the tablet", "@apply -order-1 md:order-none md:justify-self-end", "@apply -order-1 md:justify-self-end"},
		{"the path not a line of its own on the phone", ":nth-child(2) { @apply basis-full; }", ":nth-child(2) { @apply min-w-0; }"},
		{"the length breaking", "@apply whitespace-nowrap text-ink/70", "@apply text-ink/70"},
		{"the length not at its column's end", "text-ink/70 md:text-right;", "text-ink/70;"},
		{"the publisher in the data face", "@apply min-w-0 font-display text-sm", "@apply min-w-0 text-sm"},
		{"the chip not at the row's end", " md:order-none md:justify-self-end;", " md:order-none;"},
		{"the heading's dashed rule solid", `"legal-versions"] > h2 { @apply border-b-2 border-dashed border-line pb-3; }`,
			`"legal-versions"] > h2 { @apply border-b-2 border-line pb-3; }`},
		{"an eighth rule", row, `[aria-labelledby="legal-versions"] .op-version > span { @apply text-xs; }` + "\n  " + row},
		{"an eighth rule, single-quoted", row, `[aria-labelledby='legal-versions'] .op-version { @apply py-0; }` + "\n  " + row},
		{"an eighth rule, unquoted", row, `[aria-labelledby=legal-versions] .op-version > span { @apply hidden; }` + "\n  " + row},
	} {
		mutated := strings.Replace(css, c.from, c.to, 1)
		if mutated == css {
			t.Fatalf("CONTROL %s: %q is not in input.css", c.what, c.from)
		}
		if len(readScopedRules(mutated, "legal-versions", legalVersionsRules)) == 0 {
			t.Errorf("CONTROL: %s is not a finding; the reading is blind", c.what)
		}
	}
}
