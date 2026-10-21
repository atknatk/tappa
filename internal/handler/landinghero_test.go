package handler

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// THE LANDING HERO (LP-03): the wall plaque, the phone showing the tap screen and the
// ticket the plaque prints, drawn still (README O2), and the four made-up records
// web/static/js/landing.js rotates through them.
//
// Three properties are held here:
//
//   - the ticket is visible with no animation: no compiled rule that reaches its
//     classes hides it (the reference's clip-path did, until an animation revealed it);
//   - every venue the script shows is a venue of the seed, and each record's plaque
//     line names that record's venue;
//   - the page's own markup is the script's first record, so a browser that runs no
//     script shows one whole record rather than a mix.

// heroDocketTagRE finds the hero ticket's opening tag by its id.
var heroDocketTagRE = regexp.MustCompile(`<[a-z]+\b[^>]*\bid="docket"[^>]*>`)

// heroHidingRE finds a declaration that would keep an element from being seen: a
// clip-path inset that clips the whole box away on one axis, a zero opacity, a hidden
// visibility or no box at all.
//
// ⚠️ ITS REACH IS THE RULES THAT NAME THE TICKET, AND NO FURTHER. It does not read an
// ANCESTOR's rules (a hidden .scene or .heroPrinter hides the ticket too), nor the other
// ways a box can vanish -- transform: scale(0), a zero height with overflow hidden, a
// colour equal to the ground, an off-screen position. Those are not what the reference
// did; the clip-path it did use is what this pins, and the screenshots at four widths
// are what showed the ticket on screen.
var heroHidingRE = regexp.MustCompile(`clip-path\s*:\s*inset\([^)]*100%|` +
	`(?:^|[;{\s])opacity\s*:\s*(?:0+(?:\.0*)?|\.0+)%?\s*(?:;|$|!)|` +
	`visibility\s*:\s*hidden|` +
	`(?:^|[;{\s])display\s*:\s*none`)

// cssNameRE is the pattern for one name in a selector: a class (".heroSlip") or an id
// ("#docket") followed by anything but a name character, or a bare element name
// ("dialog") that is not part of a class, an id or a longer name.
func cssNameRE(name string) *regexp.Regexp {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "#") {
		return regexp.MustCompile(regexp.QuoteMeta(name) + `(?:$|[^\w-])`)
	}
	return regexp.MustCompile(`(?:^|[\s>+~(,])` + regexp.QuoteMeta(name) + `(?:$|[^\w-])`)
}

// cssRulesReaching is every style rule in css whose selector list reaches one of
// names (see cssNameRE), keyframe steps aside.
func cssRulesReaching(css string, names []string) []lpCSSRule {
	var res []*regexp.Regexp
	for _, n := range names {
		res = append(res, cssNameRE(n))
	}
	var out []lpCSSRule
	for _, r := range flatCSSRules(css) {
		if r.keyframe {
			continue
		}
	selectors:
		for _, s := range r.selectors {
			for _, re := range res {
				if re.MatchString(s) {
					out = append(out, r)
					break selectors
				}
			}
		}
	}
	return out
}

// hidingRulesFor lists every rule in css that reaches one of names -- a class
// (".heroSlip") or an id ("#docket") -- and hides it. reached counts the rules that
// reach any of them, hiding or not, so a caller can tell an empty match from a clean
// one.
func hidingRulesFor(css string, names []string) (hits []string, reached int) {
	rules := cssRulesReaching(css, names)
	for _, r := range rules {
		if m := heroHidingRE.FindString(r.decls); m != "" {
			hits = append(hits, strings.Join(r.selectors, ", ")+" { "+strings.TrimSpace(m)+" }")
		}
	}
	return hits, len(rules)
}

// heroDocketNames reads the hero ticket's classes and id out of the rendered page.
func heroDocketNames(t *testing.T, body string) []string {
	t.Helper()
	tags := heroDocketTagRE.FindAllString(body, -1)
	if len(tags) != 1 {
		t.Fatalf("the landing renders %d elements with id=\"docket\", want exactly one", len(tags))
	}
	names := []string{"#docket"}
	if m := classAttrRE.FindStringSubmatch(tags[0]); m != nil {
		for _, c := range strings.Fields(m[1]) {
			names = append(names, "."+c)
		}
	}
	if len(names) < 2 {
		t.Fatalf("the hero ticket carries no class (%s); this test reads the rules its classes reach", tags[0])
	}
	return names
}

// TestLandingHero_TheDocketIsVisibleWithNoAnimation: the hero's ticket is drawn in its
// visible state. Every rule in the COMPILED stylesheet that reaches the ticket's id or
// one of its classes -- read from the rendered page, so a renamed class is followed --
// is read for a declaration that hides it (heroHidingRE says what that does not cover).
//
// 🔴 THE DEFECT THIS EXISTS FOR WAS ONE LINE. The reference hid `.lp-docket` with
// `clip-path: inset(0 0 100% 0)` and let its print animation (or a reduced-motion rule
// undoing it) reveal the card. README O2 removes the animation; removing the animation
// and keeping that declaration leaves an invisible ticket on every visitor's screen,
// with every other test green.
func TestLandingHero_TheDocketIsVisibleWithNoAnimation(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	names := heroDocketNames(t, body)
	css := readStaticFile(t, "css/app.css")

	hits, reached := hidingRulesFor(css, names)
	if reached < 2 {
		t.Fatalf("PREMISE: %d compiled rule(s) reach %v; the ticket is unstyled or this read "+
			"the wrong file", reached, names)
	}
	for _, h := range hits {
		t.Errorf("a compiled rule hides the hero's ticket: %s. With no animation to reveal "+
			"it, nothing ever does", h)
	}
}

// TestLandingHero_HidingScanRefusesEachWayOfHiding is the scan's control: each way of
// hiding is reported on the class it reaches, a look-alike class is not read, and the
// ticket's real declarations pass.
func TestLandingHero_HidingScanRefusesEachWayOfHiding(t *testing.T) {
	t.Parallel()
	names := []string{"#docket", ".lp-slip", ".heroSlip"}
	for name, css := range map[string]string{
		"the reference's clip-path":    ".lp .lp-slip{position:relative;clip-path:inset(0 0 100% 0)}",
		"a zero opacity":               ".lp .heroSlip{opacity:0}",
		"a zero opacity, decimal":      ".lp .heroSlip{color:red;opacity:0.0;}",
		"a hidden visibility":          "#docket{visibility:hidden}",
		"no box":                       ".lp .heroSlip{display:none}",
		"inside a media query":         "@media (prefers-reduced-motion:reduce){.lp .lp-slip{clip-path:inset(0 0 100%)}}",
		"second of a selector list":    ".lp .a,.lp .heroSlip{opacity:0}",
		"on a state of the class":      ".lp .heroSlip:not(.x){display:none}",
		"the clip from the other side": ".lp .lp-slip{clip-path:inset(100% 0 0 0)}",
	} {
		if hits, _ := hidingRulesFor(css, names); len(hits) == 0 {
			t.Errorf("CONTROL %s: the scan passed it", name)
		}
	}
	visible := ".lp .lp-slip{position:relative;background:var(--paper)}" +
		".lp .heroSlip{display:flex;transform:rotate(1.5deg);opacity:.92}" +
		".lp .lp-slip::after{clip-path:none}" +
		".lp .lp-slipper{display:none}" + // a different class
		".lp .heroSlipRows{display:none}" // and another
	if hits, reached := hidingRulesFor(visible, names); len(hits) != 0 || reached != 3 {
		t.Errorf("CONTROL: the scan reported %q over %d rule(s) of a visible ticket, want none over 3", hits, reached)
	}
}

// heroRecord is one of landing.js's made-up records.
type heroRecord struct{ n, t, time, loc, plaque string }

// heroRecordRE reads one record of landing.js's `people` list, field by field in the
// order the script writes them.
var heroRecordRE = regexp.MustCompile(`\{\s*n:\s*'([^']*)',\s*t:\s*'([^']*)',\s*time:\s*'([^']*)',` +
	`\s*loc:\s*'([^']*)',\s*plaque:\s*'([^']*)'\s*\}`)

// heroPeopleRE cuts out the `people` list itself, so the count below is of the list's
// records and of nothing else in the file.
var heroPeopleRE = regexp.MustCompile(`(?s)var people = \[(.*?)\];`)

// heroRecordKeyRE counts a record however its values are quoted: every record has an
// `n:` key, in any quotes, and only records do.
var heroRecordKeyRE = regexp.MustCompile(`\bn\s*:`)

// heroRecords reads the script's records, or says why it cannot. The list's records are
// COUNTED by their key, independently of the pattern that reads them, so a record in a
// shape the pattern does not read -- double quotes, another field order, a missing
// field -- is a failure here rather than a record nobody checked.
func heroRecords(js string) ([]heroRecord, string) {
	list := heroPeopleRE.FindStringSubmatch(js)
	if list == nil {
		return nil, "landing.js carries no `var people = [ … ];` list"
	}
	var out []heroRecord
	for _, m := range heroRecordRE.FindAllStringSubmatch(list[1], -1) {
		out = append(out, heroRecord{m[1], m[2], m[3], m[4], m[5]})
	}
	if declared := len(heroRecordKeyRE.FindAllString(list[1], -1)); declared != len(out) {
		return nil, fmt.Sprintf("landing.js's list declares %d record(s) and %d read with all five fields "+
			"in single quotes", declared, len(out))
	}
	if len(out) == 0 {
		return nil, "landing.js declares no record"
	}
	return out, ""
}

// seedVenueRE reads a location row of test/fixtures/seed.sql: its id, its tenant's id,
// then its name, followed by the address ranges no other table's row carries.
var seedVenueRE = regexp.MustCompile(`'[0-9a-f-]{36}',\s*'[0-9a-f-]{36}',\s*'([^']+)',\s*ARRAY\[`)

// seedVenues is every location name in the seed.
func seedVenues(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range seedVenueRE.FindAllStringSubmatch(repoFile(t, "test", "fixtures", "seed.sql"), -1) {
		out = append(out, m[1])
	}
	// The seed is two tenants: ten locations (skill tappa-seed). Fewer means the
	// pattern stopped reading the file, and every venue below would look foreign.
	if len(out) < 10 {
		t.Fatalf("PREMISE: read %d location(s) from seed.sql (%q); the pattern lost the file", len(out), out)
	}
	return out
}

// heroVenueProblems is every record whose venue is not a seed venue, or whose plaque
// line does not name the record's own venue.
func heroVenueProblems(records []heroRecord, venues []string) []string {
	var out []string
	for _, r := range records {
		if !slices.Contains(venues, r.loc) {
			out = append(out, fmt.Sprintf("%s's record shows the venue %q, which the seed does not have", r.n, r.loc))
		}
		if !strings.HasPrefix(r.plaque, strings.ToUpper(r.loc)+" · ") {
			out = append(out, fmt.Sprintf("%s's record puts %q on the plaque, which does not name its venue %q",
				r.n, r.plaque, r.loc))
		}
	}
	return out
}

// TestLandingHero_EveryVenueTheScriptShowsIsASeedVenue: the hero's records are made
// up, but their venues are not -- each is one of the seed's locations (the demo data
// the page names in its trust strip), and the plaque the phone is held to names the
// same venue as the ticket. "KM Facility" was the reference's name for the seed's
// "KM Plant" (README §10, U11) until LP-03.
func TestLandingHero_EveryVenueTheScriptShowsIsASeedVenue(t *testing.T) {
	t.Parallel()
	records, problem := heroRecords(readStaticFile(t, "js/landing.js"))
	if problem != "" {
		t.Fatal(problem)
	}
	if len(records) != 4 {
		t.Errorf("landing.js rotates %d records; the boards draw four", len(records))
	}
	for _, p := range heroVenueProblems(records, seedVenues(t)) {
		t.Error(p)
	}
}

// TestLandingHero_VenueCheckRefusesAVenueTheSeedDoesNotHave is that check's control,
// with the record the reference carried.
func TestLandingHero_VenueCheckRefusesAVenueTheSeedDoesNotHave(t *testing.T) {
	t.Parallel()
	venues := seedVenues(t)
	old := heroRecord{"Anna Farrugia", "CLOCK IN", "04:02", "KM Facility", "KM FACILITY · 2F04-1B9A"}
	if got := heroVenueProblems([]heroRecord{old}, venues); len(got) != 1 {
		t.Errorf("CONTROL: the reference's KM Facility record drew %q, want one problem", got)
	}
	crossed := heroRecord{"Anna Farrugia", "CLOCK IN", "04:02", "KM Plant", "RUSTY BAR · 6D31-C8E2"}
	if got := heroVenueProblems([]heroRecord{crossed}, venues); len(got) != 1 {
		t.Errorf("CONTROL: a plaque naming another venue drew %q, want one problem", got)
	}
	good := heroRecord{"Anna Farrugia", "CLOCK IN", "04:02", "KM Plant", "KM PLANT · 2F04-1B9A"}
	if got := heroVenueProblems([]heroRecord{good}, venues); len(got) != 0 {
		t.Errorf("CONTROL: a seed venue drew %q", got)
	}
	if _, p := heroRecords("var people = [ { n: 'A', t: 'CLOCK IN', loc: 'KM Plant' } ];"); p == "" {
		t.Error("CONTROL: a record missing two fields was read as a record")
	}
	// The auditor's mutation: a record in DOUBLE quotes, carrying a venue the seed does
	// not have, beside a good one. The pattern does not read it, so only the count can
	// catch it -- and before the count was taken from the list's keys, it did not.
	doubled := "var people = [\n" +
		"  { n: 'Maria Vella', t: 'CLOCK IN', time: '09:58', loc: 'KF St Julians', plaque: 'KF ST JULIANS · 91AC-7E55' },\n" +
		"  { n: \"Anna Farrugia\", t: \"CLOCK IN\", time: \"04:02\", loc: \"KM Facility\", plaque: \"KM FACILITY · 2F04-1B9A\" }\n" +
		"];"
	if recs, p := heroRecords(doubled); p == "" {
		t.Errorf("CONTROL: a double-quoted record was skipped silently; read %d record(s)", len(recs))
	}
	if _, p := heroRecords("var people = [ { n: 'A', t: 'T', time: '1', loc: 'L', plaque: 'L · X' } ];"); p != "" {
		t.Errorf("CONTROL: a whole single-quoted record was refused: %s", p)
	}
}

// heroSlots is landing.js's slot map: the element id and the record field it shows.
var heroSlots = map[string]func(heroRecord) string{
	"dName":   func(r heroRecord) string { return r.n },
	"dType":   func(r heroRecord) string { return r.t },
	"dTime":   func(r heroRecord) string { return r.time },
	"dLoc":    func(r heroRecord) string { return r.loc },
	"pName":   func(r heroRecord) string { return r.n },
	"pLoc":    func(r heroRecord) string { return r.loc },
	"pTime":   func(r heroRecord) string { return r.time },
	"pPlaque": func(r heroRecord) string { return r.plaque },
}

// TestLandingHero_TheStillIsTheScriptsFirstRecord: what the page renders in each of the
// script's eight slots is the script's FIRST record, so with no script -- or before
// its first tick -- the ticket, the phone and the plaque show one record, not a mix.
// And the script names each slot the page renders, once: a slot the script fills that
// the page lacks makes it stop (it will not show two records at once), and one the page
// has that the script never fills would freeze on the first record while the rest move.
func TestLandingHero_TheStillIsTheScriptsFirstRecord(t *testing.T) {
	t.Parallel()
	js := readStaticFile(t, "js/landing.js")
	records, problem := heroRecords(js)
	if problem != "" {
		t.Fatal(problem)
	}
	first := records[0]
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	for id, field := range heroSlots {
		re := regexp.MustCompile(`\bid="` + id + `"[^>]*>([^<]*)<`)
		got := re.FindAllStringSubmatch(body, -1)
		if len(got) != 1 {
			t.Errorf("the landing renders %d element(s) with id=%q, want one", len(got), id)
			continue
		}
		if want := field(first); got[0][1] != want {
			t.Errorf("#%s renders %q; the script's first record says %q", id, got[0][1], want)
		}
		if !regexp.MustCompile(`\b` + id + `:\s*'`).MatchString(js) {
			t.Errorf("landing.js does not fill #%s, so it would hold the first record while the others rotate", id)
		}
	}
	// The script's slot map names nothing beyond these eight.
	slotMap := regexp.MustCompile(`(?s)var slots = \{(.*?)\};`).FindStringSubmatch(js)
	if slotMap == nil {
		t.Fatal("PREMISE: landing.js carries no `var slots = {…};` map")
	}
	if n := len(regexp.MustCompile(`\b\w+:\s*'`).FindAllString(slotMap[1], -1)); n != len(heroSlots) {
		t.Errorf("landing.js fills %d slot(s); the page and this test know %d", n, len(heroSlots))
	}
}

// ------------------------------------------------------- the height of a still --

// heroNowrapRE finds `white-space: nowrap` in a rule's declarations.
var heroNowrapRE = regexp.MustCompile(`white-space\s*:\s*nowrap`)

// heroWrapRE finds a flex line that may break: flex-wrap (or the flex-flow shorthand)
// set to wrap or wrap-reverse.
var heroWrapRE = regexp.MustCompile(`flex-(?:wrap|flow)\s*:[^;]*\bwrap\b`)

// heroHeightProblems says why the scene's height could depend on the record shown.
// rotated maps each class that holds rotated words to whether a rule must declare
// nowrap for it; the head is the ticket's flex line; screen is the phone's screen,
// which must clip its own canvas at a fixed height.
func heroHeightProblems(css string, rotated []string, head, screen string) []string {
	var out []string
	for _, class := range rotated {
		rules := cssRulesReaching(css, []string{class})
		nowrap := false
		for _, r := range rules {
			if heroNowrapRE.MatchString(r.decls) {
				nowrap = true
			}
		}
		if !nowrap {
			out = append(out, fmt.Sprintf("no rule gives %s `white-space: nowrap`, so a longer record "+
				"can take a second line", class))
		}
	}
	headRules := cssRulesReaching(css, []string{head})
	if len(headRules) == 0 {
		out = append(out, "no rule reaches "+head)
	}
	for _, r := range headRules {
		if heroWrapRE.MatchString(r.decls) {
			out = append(out, fmt.Sprintf("%s lets its line wrap (%s), so a longer record moves the stamp "+
				"to a second line", strings.Join(r.selectors, ", "), r.decls))
		}
	}
	var height, clip bool
	for _, r := range cssRulesReaching(css, []string{screen}) {
		if regexp.MustCompile(`(?:^|[;\s])height\s*:\s*\d+px`).MatchString(r.decls) {
			height = true
		}
		if regexp.MustCompile(`overflow\s*:\s*hidden`).MatchString(r.decls) {
			clip = true
		}
	}
	if !height || !clip {
		out = append(out, fmt.Sprintf("%s does not hold a fixed height (%v) with its overflow hidden (%v), "+
			"so the phone's greeting can grow the scene", screen, height, clip))
	}
	return out
}

// heroRotatedClasses are the classes that carry the ticket's and the plaque's rotated
// words: the type, the name, the rows (time and venue) and the plaque's venue line.
// The phone's words sit inside .heroScreen, which clips them at a fixed height.
var heroRotatedClasses = []string{".heroSlipType", ".heroSlipName", ".heroSlipRows", ".p-loc"}

// TestLandingHero_TheTicketKeepsItsHeightForEveryRecord: landing.js rotates four
// records through the scene, so the scene's height may not depend on which one is
// shown -- or everything under it jumps on every tick (measured on f357ce6: at 390 and
// 834 "CLOCK OUT" beside the stamp wrapped and the scene grew by 28px).
//
// A Go test cannot measure a font, so this holds the property that makes the height
// independent of the words: the compiled stylesheet lets none of the rotated words
// wrap, lets the ticket's head not reflow, and clips the phone's screen at a fixed
// height. Whether the longest record also FITS its line is a width question, measured
// in a browser for all four records at 390 / 834 / 1200 / 1440 (LP-03 report); a word
// that did not fit would overflow sideways, not push the page down.
//
// The ids are read from the page, so a slot that moves out of its class is caught.
func TestLandingHero_TheTicketKeepsItsHeightForEveryRecord(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	for id, class := range map[string]string{"dType": "heroSlipType", "dName": "heroSlipName", "pPlaque": "p-loc"} {
		re := regexp.MustCompile(`<[a-z]+\b[^>]*\bclass="[^"]*\b` + class + `\b[^"]*"[^>]*\bid="` + id + `"`)
		if !re.MatchString(body) {
			t.Errorf("#%s is not the element of class %s; the rule this test reads is not the one on it", id, class)
		}
	}
	rows, screen, edge := strings.Index(body, `class="heroSlipRows"`), strings.Index(body, `class="heroScreen"`),
		strings.Index(body, `class="heroPhoneEdge"`)
	for _, id := range []string{"dTime", "dLoc"} {
		if at := strings.Index(body, `id="`+id+`"`); rows < 0 || at < rows {
			t.Errorf("#%s is not inside the ticket's rows", id)
		}
	}
	for _, id := range []string{"pName", "pLoc", "pTime"} {
		if at := strings.Index(body, `id="`+id+`"`); screen < 0 || at < screen || at > edge {
			t.Errorf("#%s is not inside the phone's screen", id)
		}
	}
	for _, p := range heroHeightProblems(readStaticFile(t, "css/app.css"), heroRotatedClasses, ".heroSlipHead", ".heroScreen") {
		t.Error(p)
	}
}

// TestLandingHero_HeightCheckRefusesAWrapOrAFreeScreen is that check's control: each of
// the three ways back to a ticket that grows is reported, and the rules as written pass.
func TestLandingHero_HeightCheckRefusesAWrapOrAFreeScreen(t *testing.T) {
	t.Parallel()
	good := ".lp .heroSlipType{white-space:nowrap}.lp .heroSlipName{white-space:nowrap}" +
		".lp .heroSlipRows{white-space:nowrap}.lp .plaque .p-loc{white-space:nowrap}" +
		".lp .heroSlipHead{display:flex;flex-direction:column}" +
		"@media (min-width:1200px){.lp .heroSlipHead{flex-direction:row}}" +
		".lp .heroScreen{position:relative;overflow:hidden;height:134px}"
	if got := heroHeightProblems(good, heroRotatedClasses, ".heroSlipHead", ".heroScreen"); len(got) != 0 {
		t.Fatalf("CONTROL: the rules as written were refused: %q", got)
	}
	for name, css := range map[string]string{
		"the head wraps again (f357ce6)": good + ".lp .heroSlipHead{flex-wrap:wrap}",
		"the head wraps by shorthand":    good + "@media (min-width:768px){.lp .heroSlipHead{flex-flow:row wrap}}",
		"the name may wrap":              strings.Replace(good, ".lp .heroSlipName{white-space:nowrap}", "", 1),
		"the screen has no height":       strings.Replace(good, "height:134px", "min-height:134px", 1),
		"the screen does not clip":       strings.Replace(good, "overflow:hidden;", "", 1),
	} {
		if got := heroHeightProblems(css, heroRotatedClasses, ".heroSlipHead", ".heroScreen"); len(got) == 0 {
			t.Errorf("CONTROL %s: the check passed it", name)
		}
	}
}

// ----------------------------------------------------- the drawing is a drawing --

// heroControlRE finds anything a visitor could press or tab to.
var heroControlRE = regexp.MustCompile(`<(?:a|button|input|select|textarea|summary|details)\b|\btabindex=|\bcontenteditable\b|\bhref=`)

// heroProductClasses are the tap screen's own classes; the drawing imitates them with
// classes of its own (CLAUDE.md §9) and carries none.
var heroProductClasses = []string{"tap-button", "docket", "docket-label"}

// heroDrawingProblems reads the scene's drawing -- from the scene's opening tag to the
// pause button, the scene's last child -- for a control or a class of the real tap
// screen.
func heroDrawingProblems(body string) []string {
	start := strings.Index(body, `<div class="scene"`)
	end := strings.Index(body, `id="heroPause"`)
	if start < 0 || end < 0 || end < start {
		return []string{"PREMISE: the page has no scene followed by its pause button"}
	}
	drawing := body[start:strings.LastIndex(body[:end], "<")]
	var out []string
	// The button is the scene's LAST child, so the drawing is everything before it and
	// nothing hides after it: the button's close is followed by the scene's, the
	// hero grid's and the hero section's. Without this, an element added after the
	// button would sit in the scene unread.
	if close := strings.Index(body[end:], "</button>"); close < 0 ||
		!strings.HasPrefix(body[end+close+len("</button>"):], "</div></div></section>") {
		out = append(out, "the pause button is not the scene's last child; whatever follows it in the "+
			"scene is not read by this check")
	}
	for _, must := range []string{`class="heroTapButton"`, `id="docket"`, `class="plaque"`} {
		if !strings.Contains(drawing, must) {
			out = append(out, "PREMISE: the drawing carries no "+must+" -- the pause button is not its last child")
		}
	}
	for _, m := range heroControlRE.FindAllString(drawing, -1) {
		out = append(out, fmt.Sprintf("the drawing carries %q: the phone's Tap button is a picture, and a "+
			"picture is not something a visitor can press", m))
	}
	for _, m := range classAttrRE.FindAllStringSubmatch(drawing, -1) {
		for _, c := range strings.Fields(m[1]) {
			if slices.Contains(heroProductClasses, c) {
				out = append(out, fmt.Sprintf("the drawing carries the tap screen's own class %q; the real "+
					"screen's styles (and the tenant accent on tap-button) would reach it", c))
			}
		}
	}
	return out
}

// TestLandingHero_TheDrawingHoldsNoControl: the plaque, the phone and the ticket are a
// drawing. Nothing in it can be pressed or focused, and it carries none of the real tap
// screen's classes. The pause button is the scene's one control and sits after it.
func TestLandingHero_TheDrawingHoldsNoControl(t *testing.T) {
	t.Parallel()
	for _, p := range heroDrawingProblems(mustFetchMarketing(t, marketingRouter(t), "/")) {
		t.Error(p)
	}
}

// TestLandingHero_DrawingCheckRefusesAControlOrAProductClass is that check's control.
func TestLandingHero_DrawingCheckRefusesAControlOrAProductClass(t *testing.T) {
	t.Parallel()
	page := `<section class="hero"><div class="heroGrid"><div class="scene" role="group"><div class="plaque"></div>` +
		`<div class="heroTapButton">Tap</div><div class="lp-slip heroSlip" id="docket"></div>` +
		`<button type="button" class="heroPause" id="heroPause" hidden>Pause</button></div></div></section>`
	if got := heroDrawingProblems(page); len(got) != 0 {
		t.Fatalf("CONTROL: the drawing as written was refused: %q", got)
	}
	for name, mutated := range map[string]string{
		"the Tap button a button":    strings.Replace(page, `<div class="heroTapButton">Tap</div>`, `<button class="heroTapButton">Tap</button>`, 1),
		"the product's tap-button":   strings.Replace(page, `class="heroTapButton"`, `class="heroTapButton tap-button"`, 1),
		"the product's docket":       strings.Replace(page, `class="lp-slip heroSlip"`, `class="docket heroSlip"`, 1),
		"a focusable ticket":         strings.Replace(page, `id="docket"`, `id="docket" tabindex="0"`, 1),
		"a link on the plaque":       strings.Replace(page, `<div class="plaque"></div>`, `<div class="plaque"><a href="/t">x</a></div>`, 1),
		"the button not last":        strings.Replace(page, `<div class="heroTapButton">`, `<button id="heroPause">P</button><div class="heroTapButton">`, 1),
		"a control after the button": strings.Replace(page, `Pause</button></div>`, `Pause</button><a href="/t">x</a></div>`, 1),
	} {
		if got := heroDrawingProblems(mutated); len(got) == 0 {
			t.Errorf("CONTROL %s: the check passed it", name)
		}
	}
}

// ---------------------------------------------------------------- the pause --

// pauseState is the pause machine's state: the reader's choice and a mouse over the
// scene.
type pauseState struct{ paused, hovered bool }

// pauseMachineWant is every transition the orchestrator decided (LP-03 re-audit),
// written out state by state rather than computed, so the table in landing.js is
// compared against a list a person can read:
//   - press flips paused and leaves hovered alone (one press pauses, one resumes);
//   - a mouse entering or leaving changes hovered and never paused;
//   - keyboard focus entering writes paused, for good;
//   - there is no event that clears paused but press (no focusout, no hover).
var pauseMachineWant = map[string]map[pauseState]pauseState{
	"press": {
		{false, false}: {true, false}, {false, true}: {true, true},
		{true, false}: {false, false}, {true, true}: {false, true},
	},
	"pointerenter": {
		{false, false}: {false, true}, {false, true}: {false, true},
		{true, false}: {true, true}, {true, true}: {true, true},
	},
	"pointerleave": {
		{false, false}: {false, false}, {false, true}: {false, false},
		{true, false}: {true, false}, {true, true}: {true, false},
	},
	"keyfocus": {
		{false, false}: {true, false}, {false, true}: {true, true},
		{true, false}: {true, false}, {true, true}: {true, true},
	},
}

// pauseMachineRE cuts the transition table out of landing.js. It is JSON on one line.
var pauseMachineRE = regexp.MustCompile(`var PAUSE_MACHINE = (\{.*\});`)

// pauseMachineFuncs are the three functions that read the table and the state, as
// written (whitespace collapsed). The table says what events do; these say that
// nothing else does: next() applies the table and only the table, the word comes
// from paused alone, and the rotation runs while neither fact holds.
var pauseMachineFuncs = []string{
	"function next(state, event) { var out = { paused: state.paused, hovered: state.hovered }; " +
		"var change = PAUSE_MACHINE[event]; " +
		"for (var k in change) out[k] = change[k] === 'flip' ? !out[k] : change[k]; return out; }",
	"function label(state) { return state.paused ? 'Play' : 'Pause'; }",
	"function rotating(state) { return !state.paused && !state.hovered; }",
}

// pauseMachineWiring is how the page's events reach the machine, as written. A mouse
// is hover and touch is not; a focus is keyboard focus only when it is :focus-visible.
var pauseMachineWiring = []string{
	"pause.addEventListener('click', function () { on('press'); });",
	"scene.addEventListener('pointerenter', function (e) { if (e.pointerType === 'mouse') on('pointerenter'); });",
	"scene.addEventListener('pointerleave', function (e) { if (e.pointerType === 'mouse') on('pointerleave'); });",
	"scene.addEventListener('focusin', function (e) { if (keyboardFocus(e.target)) on('keyfocus'); });",
	"function on(event) { state = next(state, event); pause.textContent = label(state); }",
	"try { return el.matches(':focus-visible'); } catch (err) { return false; }",
}

// pauseMachineForbidden are the shapes the first version had, each of which undid a
// decision: a listener that restarts on focus leaving, hover by mouse events that touch
// also fires, and a write to the state that bypasses next().
var pauseMachineForbidden = regexp.MustCompile(`'focusout'|'blur'|'mouseenter'|'mouseleave'|` +
	`\bpaused\s*=[^=]|\bhovered\s*=[^=]|\.textContent\s*=\s*rotating`)

// pauseMachineProblems reads landing.js's pause machine: the table, run over every
// state against pauseMachineWant; the functions and the wiring as written; and the
// absence of any other way to change the state.
func pauseMachineProblems(js string) []string {
	var out []string
	m := pauseMachineRE.FindStringSubmatch(js)
	if m == nil {
		return []string{"landing.js carries no one-line `var PAUSE_MACHINE = {…};` table"}
	}
	var table map[string]map[string]any
	if err := json.Unmarshal([]byte(m[1]), &table); err != nil {
		return []string{"PAUSE_MACHINE is not JSON: " + err.Error()}
	}
	for event := range table {
		if _, ok := pauseMachineWant[event]; !ok {
			out = append(out, fmt.Sprintf("PAUSE_MACHINE has an event %q nobody decided", event))
		}
	}
	for event, want := range pauseMachineWant {
		change, ok := table[event]
		if !ok {
			out = append(out, fmt.Sprintf("PAUSE_MACHINE has no %q", event))
			continue
		}
		for from, to := range want {
			got := map[string]bool{"paused": from.paused, "hovered": from.hovered}
			for field, v := range change {
				if _, known := got[field]; !known {
					out = append(out, fmt.Sprintf("%s writes %q, which is not a fact of the state", event, field))
					continue
				}
				switch v {
				case "flip":
					got[field] = !got[field]
				case true, false:
					got[field] = v.(bool)
				default:
					out = append(out, fmt.Sprintf("%s sets %s to %v; a change is true, false or \"flip\"", event, field, v))
				}
			}
			if g := (pauseState{got["paused"], got["hovered"]}); g != to {
				out = append(out, fmt.Sprintf("%s from %+v goes to %+v, want %+v", event, from, g, to))
			}
		}
	}
	flat := strings.Join(strings.Fields(js), " ")
	for _, want := range append(append([]string{}, pauseMachineFuncs...), pauseMachineWiring...) {
		if !strings.Contains(flat, want) {
			out = append(out, "landing.js does not carry, as written: "+want)
		}
	}
	for _, hit := range pauseMachineForbidden.FindAllString(js, -1) {
		out = append(out, fmt.Sprintf("landing.js carries %q, a way round the pause machine", hit))
	}
	return out
}

// TestLandingHero_ThePauseMachineIsTheTable holds the pause button's behaviour (LP-03
// re-audit, orchestrator decision) in the script itself: its transition table, run
// over all four states for each of the four events, against pauseMachineWant; the
// three functions that apply it; and the wiring from the page's events. Node is not
// in this repository, so the table is JSON and Go runs it.
func TestLandingHero_ThePauseMachineIsTheTable(t *testing.T) {
	t.Parallel()
	for _, p := range pauseMachineProblems(readStaticFile(t, "js/landing.js")) {
		t.Error(p)
	}
}

// TestLandingHero_PauseMachineCheckRefusesEachUndoneDecision is that check's control:
// the first version's two defects and their neighbours are each refused.
func TestLandingHero_PauseMachineCheckRefusesEachUndoneDecision(t *testing.T) {
	t.Parallel()
	js := readStaticFile(t, "js/landing.js")
	if got := pauseMachineProblems(js); len(got) != 0 {
		t.Fatalf("PREMISE: the script as written is refused: %q", got)
	}
	for name, edit := range map[string][2]string{
		"hover changes the word": {
			"function label(state) { return state.paused ? 'Play' : 'Pause'; }",
			"function label(state) { return state.paused || state.hovered ? 'Play' : 'Pause'; }"},
		"focus leaving resumes": {
			"pause.removeAttribute('hidden');",
			"scene.addEventListener('focusout', function () { state = { paused: false, hovered: false }; });\n  pause.removeAttribute('hidden');"},
		"press clears hover too": {
			`"press": {"paused": "flip"}`, `"press": {"paused": "flip", "hovered": false}`},
		"hover pauses for good": {
			`"pointerenter": {"hovered": true}`, `"pointerenter": {"hovered": true, "paused": true}`},
		"keyboard focus only hovers": {
			`"keyfocus": {"paused": true}`, `"keyfocus": {"hovered": true}`},
		"touch counts as hover": {
			"if (e.pointerType === 'mouse') on('pointerenter');", "on('pointerenter');"},
		"any focus is keyboard focus": {
			"if (keyboardFocus(e.target)) on('keyfocus');", "on('keyfocus');"},
		"a write round next()": {
			"pause.removeAttribute('hidden');", "pause.removeAttribute('hidden'); state.paused = false;"},
		"a decided event dropped": {
			`, "keyfocus": {"paused": true}`, ``},
	} {
		if !strings.Contains(js, edit[0]) {
			t.Errorf("CONTROL %s: landing.js no longer carries %q to mutate", name, edit[0])
			continue
		}
		if got := pauseMachineProblems(strings.Replace(js, edit[0], edit[1], 1)); len(got) == 0 {
			t.Errorf("CONTROL %s: the check passed it", name)
		}
	}
}

// heroPauseTagRE reads the pause button's opening tag and its words.
var heroPauseTagRE = regexp.MustCompile(`<button\b([^>]*\bid="heroPause"[^>]*)>([^<]*)</button>`)

// TestLandingHero_ThePauseButtonShipsHiddenAndTheScriptRevealsIt is WCAG 2.2.2 for the
// rotating records (orchestrator decision; not on the boards). The control is a real
// <button type="button"> named by its words, it ships [hidden] so that a page with no
// script shows no dead control, and landing.js reveals it -- only after deciding to
// rotate, and only when it has the button: the rotation never starts without it. The
// stylesheet honours [hidden] over the class's own display. What the button DOES is
// TestLandingHero_ThePauseMachineIsTheTable; it was also driven in Chrome for the
// LP-03 report.
func TestLandingHero_ThePauseButtonShipsHiddenAndTheScriptRevealsIt(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	m := heroPauseTagRE.FindAllStringSubmatch(body, -1)
	if len(m) != 1 {
		t.Fatalf("the landing renders %d pause button(s), want one <button id=\"heroPause\">", len(m))
	}
	attrs, words := m[0][1], strings.TrimSpace(m[0][2])
	if !regexp.MustCompile(`\btype="button"`).MatchString(attrs) {
		t.Errorf("the pause button is not type=\"button\" (%s)", attrs)
	}
	if !regexp.MustCompile(`(?:^|\s)hidden(?:\s|=|$)`).MatchString(attrs) {
		t.Errorf("the pause button does not ship [hidden] (%s); with no script it would be a dead control", attrs)
	}
	if words != "Pause" {
		t.Errorf("the pause button says %q; it names what pressing it does, \"Pause\" while the records rotate", words)
	}

	js := readStaticFile(t, "js/landing.js")
	find := func(s string) int {
		at := strings.Index(js, s)
		if at < 0 {
			t.Errorf("landing.js never does %s", s)
		}
		return at
	}
	get, guard := find("getElementById('heroPause')"), find("if (reduced || !scene) return;")
	reveal, timer := find("pause.removeAttribute('hidden')"), find("setInterval(")
	find("if (!rotating(state)) return;")
	if !(get < guard && guard < reveal && reveal < timer) {
		t.Errorf("landing.js does not find the button, give up without it (or under reduced motion), "+
			"reveal it and only then start the timer, in that order (%d, %d, %d, %d)", get, guard, reveal, timer)
	}

	css := readStaticFile(t, "css/app.css")
	hiddenRule := false
	for _, r := range cssRulesReaching(css, []string{".heroPause"}) {
		for _, s := range r.selectors {
			if strings.Contains(s, "[hidden]") && regexp.MustCompile(`display\s*:\s*none`).MatchString(r.decls) {
				hiddenRule = true
			}
		}
	}
	if !hiddenRule {
		t.Error("no compiled rule hides .heroPause[hidden]; the class's own display wins over the UA's " +
			"[hidden], and the button would show with no script behind it")
	}
}
