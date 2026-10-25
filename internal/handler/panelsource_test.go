package handler

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web"
)

// The panel's STATIC nets (M12-49): rules held over the .templ SOURCES rather than over
// rendered pages.
//
// 🔴 A RENDERED CORPUS ONLY SEES WHAT SOME FIXTURE DRAWS. The touch-target and form-control
// nets in dashboard_test.go and transactions_test.go read pages a test renders, so a control
// behind a state no fixture sets up is outside them -- measured: the plaque notice's link
// (transactions.templ, plaqueNoticeLink) shipped as a bare `<a class="underline">` on a new
// customer's first screen, because fakeLedger's plaques default to "not measured" and no
// corpus entry drew it. The scan below reads every press target and form control a panel
// template CAN draw, whatever state draws it. The rendered nets stay: they measure what a
// browser receives (a class built in Go, a component's output), which a source scan cannot.

// panelTemplates classifies every template under web/templates. A file is either a panel
// template (true) or named in nonPanelTemplates with the reason it is out; a template in
// neither turns the scan red, so a new screen cannot be added outside the net by not
// being listed.
var panelTemplates = map[string]bool{
	"components/buckets.templ":       true,
	"components/daycutoff.templ":     true,
	"components/docket.templ":        true,
	"components/filterbar.templ":     true,
	"components/notice.templ":        true,
	"components/onetimesecret.templ": true,
	"components/panel.templ":         true,
	"components/panelstates.templ":   true,
	"components/plaque.templ":        true,
	"components/plaquedrawing.templ": true,
	"components/roster.templ":        true,
	"components/staffform.templ":     true,
	"components/venue.templ":         true,
	"components/venuedetail.templ":   true,
	"layout/base.templ":              true,
	"pages/account.templ":            true,
	"pages/admin.templ":              true,
	"pages/adminreset.templ":         true,
	"pages/anomalies.templ":          true,
	"pages/apikeys.templ":            true,
	"pages/billing.templ":            true,
	"pages/daycutoff.templ":          true,
	"pages/businessday.templ":        true,
	"pages/employees.templ":          true,
	"pages/group.templ":              true,
	"pages/locations.templ":          true,
	"pages/manualentry.templ":        true,
	"pages/panelproblem.templ":       true,
	"pages/policies.templ":           true,
	"pages/policychange.templ":       true,
	"pages/reports.templ":            true,
	"pages/review.templ":             true,
	"pages/transactions.templ":       true,
}

// nonPanelTemplates are the templates outside the panel, each with the reason.
var nonPanelTemplates = map[string]string{
	"components/devtools.templ": "the development-only tap simulator strip; it renders on the tap " +
		"screens and only in a development deployment",
	"email/message.templ":          "an e-mail body, read in a mail client and not in the panel",
	"pages/activate.templ":         "the activation screens (CLAUDE.md §9: changed only on request)",
	"pages/tap.templ":              "the employee's tap screen (CLAUDE.md §9: changed only on request)",
	"pages/result.templ":           "the employee's result screen (CLAUDE.md §9: changed only on request)",
	"pages/landing.templ":          "marketing",
	"pages/publicchrome.templ":     "marketing (the public site's shared header, menu and footer, LP-02)",
	"pages/pricing.templ":          "marketing (the public pricing page, LP-06)",
	"pages/security.templ":         "marketing (the public security page, LP-07)",
	"pages/product.templ":          "marketing (the public product tour, LP-08)",
	"pages/forrestaurants.templ":   "marketing (the public restaurants page, LP-09)",
	"pages/forproduction.templ":    "marketing (the public production page, LP-10)",
	"pages/legal.templ":            "marketing (the legal pages)",
	"pages/apiguide.templ":         "marketing (the public API guide)",
	"pages/notfound.templ":         "marketing (the public 404, LP-12)",
	"pages/signup.templ":           "the public sign-up flow, before there is a panel",
	"operatorpages/audit.templ":    "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/billing.templ":  "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/chrome.templ":   "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/enroll.templ":   "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/holdings.templ": "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/home.templ":     "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/legal.templ":    "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/plaques.templ":  "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/signin.templ":   "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/tenants.templ":  "the platform operator's surface (ADR 0020), a separate host",
	"operatorpages/vat.templ":      "the platform operator's surface (ADR 0020), a separate host",
}

// templateSources reads every .templ file under web/templates, keyed by its path relative
// to that directory, and fails on a file that panelTemplates and nonPanelTemplates do not
// classify (or on a listed file that is gone).
func templateSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "web", "templates")
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".templ") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the templates: %v", err)
	}
	for rel := range out {
		_, panel := panelTemplates[rel]
		_, other := nonPanelTemplates[rel]
		if panel == other {
			t.Errorf("web/templates/%s is classified %s; put it in panelTemplates or name it in "+
				"nonPanelTemplates with the reason it is outside the panel",
				rel, map[bool]string{true: "twice", false: "nowhere"}[panel])
		}
	}
	for rel := range panelTemplates {
		if _, ok := out[rel]; !ok {
			t.Errorf("panelTemplates lists %s, which is not under web/templates", rel)
		}
	}
	for rel := range nonPanelTemplates {
		if _, ok := out[rel]; !ok {
			t.Errorf("nonPanelTemplates lists %s, which is not under web/templates", rel)
		}
	}
	return out
}

// panelTemplateSources is templateSources narrowed to the panel.
func panelTemplateSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for rel, src := range templateSources(t) {
		if panelTemplates[rel] {
			out[rel] = src
		}
	}
	return out
}

// templComment matches what a template does not draw: a `//` line and a `/* */` block.
// Both are blanked with their newlines kept, so a reported line number stays the file's.
var (
	templLineComment  = regexp.MustCompile(`(?m)^[ \t]*//.*$`)
	templBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

func withoutTemplComments(src string) string {
	src = templBlockComment.ReplaceAllStringFunc(src, func(c string) string {
		return strings.Repeat("\n", strings.Count(c, "\n"))
	})
	return templLineComment.ReplaceAllString(src, "")
}

// templTag is one opening tag in a template's source.
type templTag struct {
	name  string
	line  int
	attrs string
	// label is the source of the <label> the tag sits inside ("" when none), read so a
	// control pressed through its label is judged by the label -- but only when the tag is
	// that label's own control (labelsIt).
	label string
	// labelsIt: the label's for= names this tag's id, or the label has no for= and this is
	// the first input, select or textarea inside it. A label toggles one control.
	labelsIt bool
}

// templTagOpen finds the elements a person can press or type into.
var templTagOpen = regexp.MustCompile(`<(a|button|summary|input|select|textarea)\b`)

// templTagsIn reads every opening tag of those elements, comments dropped. The end of a
// tag is the first `>` outside a quoted value and outside a `{ ... }` expression, so a
// multi-line tag with conditional attributes is read whole.
func templTagsIn(src string) []templTag {
	src = withoutTemplComments(src)
	var out []templTag
	for _, m := range templTagOpen.FindAllStringSubmatchIndex(src, -1) {
		end := templTagEnd(src, m[1])
		if end < 0 {
			continue
		}
		out = append(out, templTag{
			name:  src[m[2]:m[3]],
			line:  strings.Count(src[:m[0]], "\n") + 1,
			attrs: src[m[1]:end],
			label: enclosingLabel(src[:m[0]]),
		})
		tag := &out[len(out)-1]
		if tag.label != "" {
			tag.labelsIt = labelOwnsControl(src[:m[0]], tag.label, tag.attrs)
		}
	}
	return out
}

// enclosingLabel returns the attributes of the last <label> opened in before and not closed
// again, or "".
func enclosingLabel(before string) string {
	i := strings.LastIndex(before, "<label")
	if i < 0 || strings.Contains(before[i:], "</label>") {
		return ""
	}
	end := templTagEnd(before, i+len("<label"))
	if end < 0 {
		return ""
	}
	return before[i+len("<label") : end]
}

// labelOwnsControl reports whether the label whose attributes are labelAttrs is the label of
// the control whose attributes are attrs (the control begins at the end of before): its for=
// and the control's id= are the same source text, or it has no for= and no other control
// opens between the label and this one.
func labelOwnsControl(before, labelAttrs, attrs string) bool {
	if f, ok := templAttrSource(labelAttrs, "for"); ok {
		id, ok := templAttrSource(attrs, "id")
		return ok && id == f
	}
	i := strings.LastIndex(before, "<label")
	return !regexp.MustCompile(`<(?:input|select|textarea)\b`).MatchString(before[i:])
}

// templAttrSource is an attribute's value as written in the source: the quoted text, or the
// whole `{ ... }` expression.
func templAttrSource(attrs, name string) (string, bool) {
	loc := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(name) + `=`).FindStringIndex(attrs)
	if loc == nil {
		return "", false
	}
	rest := attrs[loc[1]:]
	switch {
	case strings.HasPrefix(rest, `"`):
		if end := strings.IndexByte(rest[1:], '"'); end >= 0 {
			return rest[:end+2], true
		}
	case strings.HasPrefix(rest, "{"):
		return "{" + strings.TrimSpace(balancedBraces(rest)) + "}", true
	}
	return "", false
}

func templTagEnd(src string, from int) int {
	depth := 0
	var quote byte
	for i := from; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' && depth > 0 {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || (c == '`' && depth > 0):
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
		case c == '>' && depth == 0:
			return i
		}
	}
	return -1
}

// classAttrStart finds a class attribute (not data-class or the like).
var classAttrStart = regexp.MustCompile(`(?:^|\s)class=`)

// goStringLiteral is a double-quoted Go string with no escapes worth decoding in a class.
var goStringLiteral = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

// classAlternatives is every class list a tag's source can render. A literal value is one
// list. An expression `{ a, b, ... }` is read item by item: a string literal is always
// there; a templ.KV is conditional and adds nothing to what is guaranteed; a call to a
// function of this package becomes one list per `return` in that function. Anything else
// cannot be read from the source and is reported in unresolved.
func classAlternatives(attrs string, funcs map[string][]string) (alts []string, unresolved []string) {
	for _, loc := range classAttrStart.FindAllStringIndex(attrs, -1) {
		rest := attrs[loc[1]:]
		switch {
		case strings.HasPrefix(rest, `"`):
			if end := strings.IndexByte(rest[1:], '"'); end >= 0 {
				alts = append(alts, rest[1:1+end])
			}
		case strings.HasPrefix(rest, "{"):
			expr := balancedBraces(rest)
			lists := []string{""}
			for _, item := range topLevelItems(expr) {
				item = strings.TrimSpace(item)
				switch {
				case strings.HasPrefix(item, `"`):
					lit := goStringLiteral.FindStringSubmatch(item)
					if lit == nil {
						unresolved = append(unresolved, item)
						continue
					}
					for i := range lists {
						lists[i] += " " + lit[1]
					}
				case strings.HasPrefix(item, "templ.KV("):
					// conditional: guarantees nothing
				default:
					name := calledFunc(item)
					returns, ok := funcs[name]
					if name == "" || !ok {
						unresolved = append(unresolved, item)
						continue
					}
					var next []string
					for _, l := range lists {
						for _, r := range returns {
							next = append(next, l+" "+r)
						}
					}
					lists = next
				}
			}
			alts = append(alts, lists...)
		}
	}
	return alts, unresolved
}

// balancedBraces returns the inside of the `{ ... }` that s begins with.
func balancedBraces(s string) string {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[1:i]
			}
		}
	}
	return ""
}

// topLevelItems splits an expression list on the commas that are not inside a call or a string.
func topLevelItems(expr string) []string {
	var out []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			out = append(out, expr[start:i])
			start = i + 1
		}
	}
	return append(out, expr[start:])
}

var calledFuncRE = regexp.MustCompile(`^(?:[A-Za-z_]\w*\.)?([A-Za-z_]\w*)\(`)

func calledFunc(item string) string {
	if m := calledFuncRE.FindStringSubmatch(item); m != nil {
		return m[1]
	}
	return ""
}

// classFuncReturns reads, from every .templ and hand-written .go file of the template
// packages, the class lists each function can return: one list per `return` statement,
// made of the string literals that statement concatenates. A function with a return that
// carries no literal is left out, so a call to it is reported as unresolved rather than
// read as an empty list.
func classFuncReturns(t *testing.T) map[string][]string {
	t.Helper()
	root := filepath.Join("..", "..", "web", "templates")
	funcStart := regexp.MustCompile(`(?m)^func (?:\([^)]*\) )?([A-Za-z_]\w*)\(`)
	returnRE := regexp.MustCompile(`(?m)^\s*return\s+(.*)$`)
	out := map[string][]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasSuffix(p, "_templ.go") || strings.HasSuffix(p, "_test.go") ||
			!(strings.HasSuffix(p, ".templ") || strings.HasSuffix(p, ".go")) {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		src := withoutTemplComments(string(raw))
		starts := funcStart.FindAllStringSubmatchIndex(src, -1)
		for i, s := range starts {
			end := len(src)
			if i+1 < len(starts) {
				end = starts[i+1][0]
			}
			if next := strings.Index(src[s[0]:end], "\ntempl "); next >= 0 {
				end = s[0] + next
			}
			name := src[s[2]:s[3]]
			var lists []string
			ok := true
			for _, r := range returnRE.FindAllStringSubmatch(src[s[0]:end], -1) {
				lits := goStringLiteral.FindAllStringSubmatch(r[1], -1)
				if len(lits) == 0 {
					ok = false
					break
				}
				var words []string
				for _, l := range lits {
					words = append(words, l[1])
				}
				lists = append(lists, strings.Join(words, " "))
			}
			if ok && len(lists) > 0 {
				out[name] = lists
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading the template packages: %v", err)
	}
	return out
}

// hiddenInput is the one form control that renders nothing to press.
var hiddenInput = regexp.MustCompile(`(?:^|\s)type="hidden"`)

// everyAlternative reports whether every class list a tag's source can render passes ok.
func everyAlternative(attrs string, funcs map[string][]string, ok func(string) bool) bool {
	alts, _ := classAlternatives(attrs, funcs)
	if len(alts) == 0 {
		return false
	}
	for _, a := range alts {
		if !ok(a) {
			return false
		}
	}
	return true
}

// TestPanelTemplates_EveryPressTargetAndFormControlCarriesATouchTargetClass is the touch-target
// rule (README §3.11, CLAUDE.md §9) held over every panel template's SOURCE: each <a>,
// <button>, <summary>, and each visible <input>, <select> and <textarea>, carries one of
// panelTouchTargets in EVERY class list its source can render.
//
// It covers form controls as well as press targets, so it is also the net for
// TestPanelScreens_FormControlsCarryATouchTargetClass's blind spots (a form only a POST or a
// sub-route draws).
func TestPanelTemplates_EveryPressTargetAndFormControlCarriesATouchTargetClass(t *testing.T) {
	sources := panelTemplateSources(t)
	funcs := classFuncReturns(t)

	files, tags, throughLabel := 0, 0, 0
	seen := map[string]bool{}
	for rel, src := range sources {
		files++
		for _, tag := range templTagsIn(src) {
			if tag.name == "input" && hiddenInput.MatchString(tag.attrs) {
				continue
			}
			tags++
			where := rel + ":" + strconv.Itoa(tag.line)
			for key := range knownPanelTargets {
				if strings.HasPrefix(key, rel+"|") && strings.Contains(tag.attrs, strings.TrimPrefix(key, rel+"|")) {
					seen[key] = true
				}
			}
			control := tag.name == "input" || tag.name == "select" || tag.name == "textarea"
			if control && tag.labelsIt && everyAlternative(tag.label, funcs, hasTouchTargetClass) {
				throughLabel++
				continue
			}
			ok := hasTouchTargetClass
			if control {
				ok = hasControlTouchTargetClass
			}
			alts, unresolved := classAlternatives(tag.attrs, funcs)
			if len(alts) == 0 {
				t.Errorf("%s: <%s> has no class the source can read (unresolved: %q); a press "+
					"target or a form control carries one of panelTouchTargets", where, tag.name, unresolved)
				continue
			}
			for _, classes := range alts {
				if !ok(classes) {
					t.Errorf("%s: <%s class=%q> carries none of panelTouchTargets (unresolved "+
						"items: %q). A control under 44 px is missed by a thumb; give it the "+
						"panel's recipe (a registered class, or a display class with min-h-11).",
						where, tag.name, strings.TrimSpace(classes), unresolved)
				}
			}
		}
	}

	// ANTI-VACUITY: the scan read the panel and found the targets it is known to have.
	if files < 25 || tags < 150 || throughLabel < 2 {
		t.Fatalf("scanned %d panel templates and %d targets (%d pressed through a label); the "+
			"panel has more, so the scan is reading the wrong files or the tag reader is broken",
			files, tags, throughLabel)
	}
	var missing []string
	for key := range knownPanelTargets {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("CONTROL: the scan did not read these known targets: %v", missing)
	}
	t.Logf("panel templates scanned: %d, press targets and form controls: %d", files, tags)
}

// knownPanelTargets are targets the scan must read (file|a substring of the tag), chosen
// from the shapes it has to handle: a target only an unmeasured state draws, a POST-only
// screen's button, a multi-line form control, a class from a function, a <summary>.
var knownPanelTargets = map[string]bool{
	"pages/transactions.templ|templ.URL(v.PlaquesHref())": true,
	"pages/billing.templ|btn btn--ink min-h-14":           true,
	"pages/manualentry.templ|name=\"time\"":               true,
	"components/venue.templ|field-input":                  true,
	"components/panel.templ|bar-link":                     true,
	"pages/policies.templ|ruleSwitchClass(":               true,
	"pages/adminreset.templ|name=\"email\"":               true,
}

// TestTemplTouchScanner_RejectsTheThingsItExistsToReject is the scanner's own control, in both
// directions: a reader that found no tags, or that read every class list as passing, would
// leave the scan above green over any template.
func TestTemplTouchScanner_RejectsTheThingsItExistsToReject(t *testing.T) {
	funcs := map[string][]string{"goodClass": {"btn btn--quiet", "btn"}, "halfClass": {"btn", "underline"}}
	pass := func(src string) bool {
		tags := templTagsIn(src)
		if len(tags) != 1 {
			t.Fatalf("%q: read %d tags, want 1", src, len(tags))
		}
		if tags[0].labelsIt && everyAlternative(tags[0].label, funcs, hasTouchTargetClass) {
			return true
		}
		return everyAlternative(tags[0].attrs, funcs, hasTouchTargetClass)
	}
	for _, src := range []string{
		`<a href={ templ.URL(x) } class="underline">Venues</a>`,                                 // the M12-49 defect
		`<a class="min-h-11 underline">x</a>`,                                                   // a raw height on an inline box
		`<a class={ templ.KV("btn", on) }>x</a>`,                                                // only conditionally a target
		`<button class={ halfClass(v) }>x</button>`,                                             // one branch of the function is not
		"<a\n\thref={ templ.URL(v.Href) }\n\tif v.On {\n\t\taria-current=\"page\"\n\t}\n>x</a>", // no class at all
		`<summary class="">More</summary>`,
	} {
		if pass(src) {
			t.Errorf("the scanner accepted %q", src)
		}
	}
	for _, src := range []string{
		`<a class="inline-flex min-h-11 items-center underline">x</a>`,
		`<a class="hidden min-h-11 md:inline-flex">x</a>`,
		`<button class={ "queue-item md:h-full", templ.KV("queue-item--current", c) }>x</button>`,
		`<a class={ goodClass(v) }>x</a>`,
		"<button\n\ttype=\"submit\"\n\tif v.Off {\n\t\tdisabled\n\t}\n\tclass=\"btn\"\n>x</button>",
		`<label class="manual-dircard"><input class="peer sr-only" type="radio"/></label>`,
	} {
		if !pass(src) {
			t.Errorf("the scanner refused %q", src)
		}
	}
	// A label toggles ONE control: the one its for= names, or with no for= the first inside it.
	for _, c := range []struct {
		src  string
		want []bool
	}{
		{`<label for={ p + "-on" } class="inline-flex min-h-11"><input id={ p + "-on" } type="checkbox"/><input class="w-8" type="text"/></label>`, []bool{true, false}},
		{`<label class="manual-dircard"><input class="sr-only" type="radio"/><input class="w-8" type="text"/></label>`, []bool{true, false}},
		{`<label for="a" class="manual-dircard"><input id="b" class="sr-only" type="radio"/></label>`, []bool{false}},
	} {
		tags := templTagsIn(c.src)
		if len(tags) != len(c.want) {
			t.Fatalf("%q: read %d tags, want %d", c.src, len(tags), len(c.want))
		}
		for i, tag := range tags {
			if tag.labelsIt != c.want[i] {
				t.Errorf("%q: control %d is its label's own = %v, want %v", c.src, i, tag.labelsIt, c.want[i])
			}
		}
	}
	if n := len(templTagsIn("// <a class=\"underline\">\n/* <button> */")); n != 0 {
		t.Errorf("the scanner read %d tags out of comments", n)
	}
}

// paragraphOpen is a <p> opening tag (not <path>, <pre> or <param>).
var paragraphOpen = regexp.MustCompile(`<p[\s>]`)

// runningTextLink reads the paragraph a link sits in: whether the paragraph carries words of
// its own beside its links and is not a flex or grid row. A link alone in its <p> (a "Forgot
// password?" line, a button) or in a row of items may be a box; a link with a sentence around
// it is part of that sentence.
func runningTextLink(src string, at int) bool {
	locs := paragraphOpen.FindAllStringIndex(src[:at], -1)
	if len(locs) == 0 {
		return false
	}
	open := locs[len(locs)-1][0]
	end := strings.Index(src[open:], "</p>")
	if end < 0 || open+end < at {
		return false
	}
	body := src[open:][:end]
	// A paragraph laid out as a flex or grid row is a row of items, not a sentence: its
	// links are boxes beside words, and the line box is the row's.
	if tagEnd := templTagEnd(body, 2); tagEnd > 0 {
		alts, _ := classAlternatives(body[2:tagEnd], nil)
		for _, a := range alts {
			for _, c := range strings.Fields(a) {
				if c == "flex" || c == "inline-flex" || c == "grid" || c == "inline-grid" {
					return false
				}
			}
		}
	}
	body = templAnchor.ReplaceAllString(body, " ")
	body = regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(body, " ")
	var words []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || templControlLine.MatchString(line) {
			continue
		}
		words = append(words, line)
	}
	return regexp.MustCompile(`[A-Za-z]{2,}`).MatchString(strings.Join(words, " "))
}

// templAnchor is a whole <a ...>...</a>, its words included.
var templAnchor = regexp.MustCompile(`(?s)<a\b.*?</a>`)

// templControlLine is a line of templ control flow rather than text: if / else / for / switch /
// case / a closing brace, and the `{ " " }` spacer.
var templControlLine = regexp.MustCompile(`^(?:\}?\s*(?:if|else|for|switch|case|default)\b.*|\}|\{\s*" "\s*\})$`)

// TestPanelTemplates_ALinkInRunningTextKeepsTheLineBox: a link inside a sentence uses .hit-link,
// and carries no display or min-height utility.
//
// 🔴 MEASURED IN THE M12-49 SCREENSHOT TOUR. `inline-flex min-h-11` on a link in a paragraph
// gives the link 44 px by making it a 44 px inline block, and the paragraph's line grows around
// it: "For the endpoints and the signature, read the" / a gap / "integration guide." on the
// Developers screen at every width, and the same on The Pass's plaques card. .hit-link keeps the
// link inline and draws the 44 px in its ::after, so the touch rule holds without the gap.
func TestPanelTemplates_ALinkInRunningTextKeepsTheLineBox(t *testing.T) {
	inLineBox := regexp.MustCompile(`^(?:[a-z-]+:)*(?:flex|inline-flex|block|inline-block|grid|inline-grid|min-h-.+|h-.+|size-.+)$`)
	found := map[string]bool{}
	links := 0
	for rel, src := range panelTemplateSources(t) {
		clean := withoutTemplComments(src)
		for _, m := range templTagOpen.FindAllStringSubmatchIndex(clean, -1) {
			if clean[m[2]:m[3]] != "a" || !runningTextLink(clean, m[0]) {
				continue
			}
			links++
			end := templTagEnd(clean, m[1])
			attrs := clean[m[1]:end]
			where := rel + ":" + strconv.Itoa(strings.Count(clean[:m[0]], "\n")+1)
			for key := range knownRunningTextLinks {
				if strings.HasPrefix(key, rel+"|") && strings.Contains(attrs, strings.TrimPrefix(key, rel+"|")) {
					found[key] = true
				}
			}
			alts, unresolved := classAlternatives(attrs, nil)
			if len(alts) == 0 {
				t.Errorf("%s: a link in running text has no class the source can read (%q)", where, unresolved)
				continue
			}
			for _, classes := range alts {
				fields := strings.Fields(classes)
				if !slices.Contains(fields, "hit-link") {
					t.Errorf("%s: a link in running text without .hit-link (%q); it needs a 44 px hit "+
						"area that does not grow the line", where, classes)
				}
				for _, c := range fields {
					if inLineBox.MatchString(c) {
						t.Errorf("%s: a link in running text carries %q, which makes it a box in the "+
							"line and breaks the sentence into gappy lines; .hit-link alone gives the 44 px",
							where, c)
					}
				}
			}
		}
	}
	var missing []string
	for key := range knownRunningTextLinks {
		if !found[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 || links < 5 {
		t.Errorf("CONTROL: %d links in running text read, want at least the 5 the tour found; "+
			"not read as such: %v", links, missing)
	}
}

// knownRunningTextLinks are the links the screenshot tour found inside sentences.
var knownRunningTextLinks = map[string]bool{
	"pages/apikeys.templ|templ.URL(APIGuidePath)":         true, // two: the heading's note and the keys card
	"pages/apikeys.templ|templ.URL(APIGuideWebhooksHref)": true,
	"pages/transactions.templ|templ.URL(p.Href)":          true,
	"pages/transactions.templ|templ.URL(v.PlaquesHref())": true,
}

// TestPanelScreens_APseudoElementTouchTargetIsAnchoredToItsHost holds the half of a touch target
// that TestPanelScreens_TouchTargetClassesReserve44px cannot see: WHERE the reserved box is.
//
// 🔴 A CLASS WHOSE 44 PX LIVES ON ::before OR ::after (.hit-link) RESERVES IT ONLY IF THE PSEUDO-
// ELEMENT IS DRAWN AND PLACED ON THE LINK. The height check reads min-height alone, so dropping
// `relative` from .hit-link (the box escapes to the nearest positioned ancestor) or `absolute`
// from its ::after (the box becomes a 44 px inline box that grows the line again) left it green
// -- measured (M12-49 audit). So, in the COMPILED stylesheet: every rule that gives such a class
// a min-height is on its pseudo-element and declares content and position:absolute, and the
// class itself declares position:relative.
func TestPanelScreens_APseudoElementTouchTargetIsAnchoredToItsHost(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) — run `make css`. THIS IS NOT A PASS.", err)
	}
	ruleRE := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	decl := func(body, prop, value string) bool {
		return regexp.MustCompile(`(?:^|;)` + regexp.QuoteMeta(prop) + `:` + value + `(?:;|$)`).MatchString(body)
	}
	pseudoHosted := []string{}
	for class := range panelTouchTargets {
		self := regexp.MustCompile(`^\.` + regexp.QuoteMeta(class) + `$`)
		pseudo := regexp.MustCompile(`^\.` + regexp.QuoteMeta(class) + `::?(?:after|before)$`)
		var heightOnPseudo, heightOnOther, hostRelative bool
		var pseudoBodies []string
		for _, rule := range ruleRE.FindAllStringSubmatch(string(raw), -1) {
			for _, sel := range strings.Split(rule[1], ",") {
				sel = strings.TrimSpace(sel)
				switch {
				case self.MatchString(sel):
					if decl(rule[2], "position", "relative") {
						hostRelative = true
					}
				case pseudo.MatchString(sel):
					pseudoBodies = append(pseudoBodies, rule[2])
				}
				if strings.Contains(rule[2], "min-height:") && regexp.MustCompile(`\.`+regexp.QuoteMeta(class)+`\b`).MatchString(sel) {
					if pseudo.MatchString(sel) {
						heightOnPseudo = true
					} else {
						heightOnOther = true
					}
				}
			}
		}
		if !heightOnPseudo {
			continue
		}
		pseudoHosted = append(pseudoHosted, class)
		if heightOnOther {
			t.Errorf(".%s reserves a min-height on its pseudo-element and on itself; say which box "+
				"is the target", class)
		}
		if !hostRelative {
			t.Errorf(".%s keeps its 44 px on a pseudo-element but does not declare position:relative: "+
				"the box is placed against some ancestor, not the link", class)
		}
		drawn, absolute := false, false
		for _, b := range pseudoBodies {
			drawn = drawn || regexp.MustCompile(`(?:^|;)content:`).MatchString(b)
			absolute = absolute || decl(b, "position", "absolute")
		}
		if !drawn || !absolute {
			t.Errorf(".%s's pseudo-element declares content: %v, position:absolute: %v -- without content "+
				"it is not drawn, without absolute it is an inline box that grows the line", class, drawn, absolute)
		}
	}
	// ANTI-VACUITY: .hit-link is the known pseudo-hosted target.
	if !slices.Contains(pseudoHosted, "hit-link") {
		t.Fatalf("CONTROL: .hit-link was not read as a pseudo-element target (read: %v); the reader "+
			"is not reading the stylesheet", pseudoHosted)
	}
}
