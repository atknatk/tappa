package handler

import (
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
var heroHidingRE = regexp.MustCompile(`clip-path\s*:\s*inset\([^)]*100%|` +
	`(?:^|[;{\s])opacity\s*:\s*(?:0+(?:\.0*)?|\.0+)%?\s*(?:;|$|!)|` +
	`visibility\s*:\s*hidden|` +
	`(?:^|[;{\s])display\s*:\s*none`)

// hidingRulesFor lists every rule in css that reaches one of names -- a class
// (".heroSlip") or an id ("#docket") -- and hides it. reached counts the rules that
// reach any of them, hiding or not, so a caller can tell an empty match from a clean
// one.
func hidingRulesFor(css string, names []string) (hits []string, reached int) {
	var res []*regexp.Regexp
	for _, n := range names {
		res = append(res, regexp.MustCompile(regexp.QuoteMeta(n)+`(?:$|[^\w-])`))
	}
	for _, r := range flatCSSRules(css) {
		if r.keyframe {
			continue
		}
		reaches := false
		for _, s := range r.selectors {
			for _, re := range res {
				if re.MatchString(s) {
					reaches = true
				}
			}
		}
		if !reaches {
			continue
		}
		reached++
		if m := heroHidingRE.FindString(r.decls); m != "" {
			hits = append(hits, strings.Join(r.selectors, ", ")+" { "+strings.TrimSpace(m)+" }")
		}
	}
	return hits, reached
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
// is read for a declaration that hides it.
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

// heroRecords reads the script's records, or says why it cannot: a record that does not
// have the five fields in this shape would otherwise be skipped silently.
func heroRecords(js string) ([]heroRecord, string) {
	var out []heroRecord
	for _, m := range heroRecordRE.FindAllStringSubmatch(js, -1) {
		out = append(out, heroRecord{m[1], m[2], m[3], m[4], m[5]})
	}
	if declared := strings.Count(js, "{ n: '"); declared != len(out) {
		return nil, fmt.Sprintf("landing.js declares %d record(s) and %d read with all five fields", declared, len(out))
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
