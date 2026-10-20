package handler

// panelhead_test.go -- M12-01: the section's own heading in the panel shell. PanelChrome
// gained three optional slots (Kicker, Title, Crumbs), pages.PanelActions puts a section's
// controls at the right of the title, and the h1 took a new size. The claims held here:
//
//   - the document has exactly ONE <h1>, with and without a Title, with every slot set;
//   - the breadcrumb trail is a nav named "Breadcrumb" whose last crumb is the current page
//     (text, aria-current="page") and whose earlier crumbs are links -- or text, when the
//     crumb has no href;
//   - the eyebrow, the trail and the actions appear only when set;
//   - with every slot empty, the shell is byte for byte the shell BEFORE M12-01 apart from
//     the h1's class: undoing that one class in each panel render of the WL-8 corpus gives
//     back the digest that render had at 022ba3c (the commit before M12-01).

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

const (
	// panelH1Before is the h1 the shell printed before M12-01.
	panelH1Before = `<h1 class="font-display text-3xl font-bold tracking-tight">`
	// panelH1 is the h1 it prints now: the redesign's display size, stepping with the width.
	panelH1 = `<h1 class="font-display text-3xl md:text-4xl desk:text-[44px] font-bold leading-none md:leading-none tracking-[-0.035em]">`
)

// unbrandedPanelShellBeforeM1201 is unbrandedPanelShell as it stood at 022ba3c, before the h1
// changed size. It is kept, not replaced, so the claim "empty slots change only the h1's
// class" stays measured after the WL-8 digests were re-rendered for the new size.
var unbrandedPanelShellBeforeM1201 = map[string]unbrandedDigest{
	"PanelShell, owner, tab transactions":            {11514, "95e04c427d9b6975187f7c7b41d77598660c88e634d92e9994a9114455bf09a2"},
	"PanelShell, owner, tab review":                  {11515, "8775947a8414f201c451138ffcdd6347fac8a1f399f89976453d4f1e733f5761"},
	"PanelShell, owner, tab employees":               {11512, "2aec88429a68c49bea4ce27b1e302e182562e4945055d7c005301da5053ab7e8"},
	"PanelShell, owner, tab locations":               {11606, "12bf8a04dd143c57361e3a0f75d05728408bbaf86177cfb7a4859bf93d5cdc60"},
	"PanelShell, owner, tab reports":                 {11514, "b404e3cc36203a067231a8e0938e6a1758e1171acabf86343fb69ecea1f8f80e"},
	"PanelShell, owner, tab anomalies":               {11586, "fbc8d92393bdf674d35a252c836c16d589a549baef09cfda6603099028238aee"},
	"PanelShell, owner, tab policies":                {11576, "7345bde2063f7c223115176ace9ed374d6888a977ab2c2da9396038e96a52cdc"},
	"PanelShell, owner, tab billing":                 {11580, "0a7fd6e0ff1702fbed6d2bc6562361ff4687d81c20f540a577fd3cc926bac57d"},
	"PanelShell, owner, tab account":                 {11580, "0d6a503590d81c5f5cf5d852f82b8f85d082becaab68605aed660cb52bc6146c"},
	"PanelShell, owner, tab developers":              {11586, "eb6fce93d70755a2e13aec5ba7537972a0f9743727594c77757e5532484a0ead"},
	"PanelShell, manager, review, capped badge":      {9910, "0a4cae3a28f2f3e3db42e83b2ebebb67e2cb819ef975c103c15953730990ac3c"},
	"PanelShell, owner, transactions, unknown badge": {11552, "f4b7e3f68fb484fea0b509d1d6f875a719a4e9a912181de85a5375517828c67c"},
	"PanelShell, owner, transactions, empty queue":   {11366, "9bf9cfcb7259914e34c185938997b3b7ee1102db6e2407c7aeaf3c2f050929dd"},
	"PanelShell, owner, an undeclared tab":           {11366, "c1c3be44486d86147aa4f0a0c82bb4a28b198b4807d70b3a0a951ae3e17ffc67"},
	"PanelShell, a name to escape":                   {11650, "f5419139eee1216e4ce99cb7ddf2e266717beb24ce2c2b7c66304cc534a7e470"},
	"PanelShellWithScript, owner, transactions":      {11570, "f6506638f6ca298ff0f6e072ea0073131356982b15b6168b48bbd3a99d179a1b"},
	"AdminDashboard, owner, reports":                 {11865, "07e010c0e0e3ae81d9dc48d37416c2f4aa86e2deb0fa98f679434c4aa823119a"},
}

// panelEmptyUndo is what M12-09 changed in AdminDashboard's body (the "not built yet" empty
// state), now and before: components.EmptyState's opening became components.PanelEmpty's.
// The heading and the sentences between them are untouched, so they are not listed.
var panelEmptyUndo = []struct{ now, before string }{
	{
		`<section class="flex flex-col gap-2 rounded-[18px] border border-dashed border-ink/50 p-6"><p class="lbl">The Roll</p><h2 class="font-display text-[19px] font-bold leading-tight tracking-tight text-ink">`,
		`<section class="empty-state"><p class="docket-label">Nothing here yet</p><h2 class="font-display text-lg font-bold tracking-tight text-ink">`,
	},
	{`<div class="text-sm leading-normal text-ink/70">`, `<div class="text-sm text-ink/70">`},
}

// TestPanelHead_EmptySlotsAreTheShellAsItWas: every panel render of the WL-8 corpus (all
// slots empty) prints the new h1 exactly as often as a section is declared (once, or never
// for the undeclared tab), and with that one class put back it is the pre-M12-01 render to
// the byte. MUTANT (measured): a wrapper around the bare h1 -> every declared tab red.
func TestPanelHead_EmptySlotsAreTheShellAsItWas(t *testing.T) {
	n := 0
	for _, r := range unbrandedChromeRenders() {
		if !isPanelRender(r.name) {
			continue
		}
		n++
		want, ok := unbrandedPanelShellBeforeM1201[r.name]
		if !ok {
			t.Errorf("%s: no pre-M12-01 digest", r.name)
			continue
		}
		got := string(renderUnbranded(t, r))
		if r.name == "AdminDashboard, owner, reports" {
			// M12-09 moved this section's BODY to components.PanelEmpty. The body is not the
			// shell, so its old markup is put back too -- each piece exactly once -- and the
			// claims below still hold the shell against the pre-M12-01 digest, the eyebrow
			// check included (the empty state's own label goes with the undo).
			for _, u := range panelEmptyUndo {
				if c := strings.Count(got, u.now); c != 1 {
					t.Fatalf("%s: the M12-09 empty state's %q appears %d times, want 1", r.name, u.now, c)
				}
				got = strings.Replace(got, u.now, u.before, 1)
			}
		}
		wantH1 := 1
		if r.name == "PanelShell, owner, an undeclared tab" {
			wantH1 = 0
		}
		if c := strings.Count(got, "<h1"); c != wantH1 {
			t.Errorf("%s: %d <h1>, want %d", r.name, c, wantH1)
		}
		if c := strings.Count(got, panelH1); c != wantH1 {
			t.Errorf("%s: the new h1 appears %d times, want %d", r.name, c, wantH1)
		}
		for _, never := range []string{`class="panel-head`, `aria-label="Breadcrumb"`, `class="lbl"`, `class="panel-actions"`} {
			if strings.Contains(got, never) {
				t.Errorf("%s: an empty slot drew %q", r.name, never)
			}
		}
		undone := strings.Replace(got, panelH1, panelH1Before, 1)
		if d := digestUnbranded([]byte(undone)); d != want {
			t.Errorf("%s: with the h1's class put back the render is %d bytes %s, before M12-01 it was %d bytes %s -- "+
				"something besides the h1's class changed", r.name, d.n, d.sum, want.n, want.sum)
		}
	}
	if n != len(unbrandedPanelShellBeforeM1201) {
		t.Errorf("checked %d panel renders, the pre-M12-01 table holds %d", n, len(unbrandedPanelShellBeforeM1201))
	}
}

// renderPanelHead renders the scriptless shell around a fixed body.
func renderPanelHead(t *testing.T, c pages.PanelChrome) string {
	t.Helper()
	return renderPanelHeadWith(t, c, nil)
}

// renderPanelHeadWith renders the scriptless shell inside PanelActions(actions), as a section
// with controls writes it; nil actions renders the shell with no wrapper at all.
func renderPanelHeadWith(t *testing.T, c pages.PanelChrome, actions templ.Component) string {
	t.Helper()
	shell := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return pages.PanelShell(c).Render(templ.WithChildren(ctx, templ.Raw(`<p>the section's own body</p>`)), w)
	})
	var root templ.Component = shell
	var children templ.Component
	if actions != nil {
		root, children = pages.PanelActions(actions), shell
	}
	return string(renderUnbranded(t, unbrandedRender{name: "head", c: root, children: children}))
}

// headChrome is an owner on People with records waiting -- the corpus's chrome.
func headChrome() pages.PanelChrome {
	return pages.PanelChrome{FullName: "Maria Borg", Role: "owner", Tab: pages.TabEmployees, Pending: pages.PendingBadge{Known: true, N: 3}}
}

var h1RE = regexp.MustCompile(`(?s)<h1\b[^>]*>(.*?)</h1>`)

// TestPanelHead_OneH1WithAndWithoutATitle: the h1 holds the section's label without a Title
// and the Title with one; with every slot set there is still one h1, and the label never
// becomes a second heading.
func TestPanelHead_OneH1WithAndWithoutATitle(t *testing.T) {
	all := headChrome()
	all.Kicker, all.Title = "People · add", "New person"
	all.Crumbs = []pages.Crumb{{Label: "People", Href: "/admin/employees"}, {Label: "New person"}}
	cancel := templ.Raw(`<a class="btn" href="/admin/employees">Cancel</a>`)
	titleOnly := headChrome()
	titleOnly.Title = "New person"
	for _, tc := range []struct {
		name    string
		c       pages.PanelChrome
		actions templ.Component
		want    string
	}{
		{"no title", headChrome(), nil, "People"},
		{"a title alone", titleOnly, nil, "New person"},
		{"every slot", all, cancel, "New person"},
		{"actions on the section label", headChrome(), cancel, "People"},
	} {
		got := renderPanelHeadWith(t, tc.c, tc.actions)
		hs := h1RE.FindAllStringSubmatch(got, -1)
		if len(hs) != 1 || strings.Count(got, "<h1") != 1 {
			t.Fatalf("%s: %d <h1> in the document, want exactly 1", tc.name, strings.Count(got, "<h1"))
		}
		if hs[0][1] != tc.want {
			t.Errorf("%s: the h1 says %q, want %q", tc.name, hs[0][1], tc.want)
		}
		if !strings.Contains(got, panelH1) {
			t.Errorf("%s: the h1 is not the redesign's size", tc.name)
		}
	}
	// A Title alone changes the words of the bare h1 and nothing else.
	plain, titled := renderPanelHead(t, headChrome()), renderPanelHead(t, titleOnly)
	if strings.Replace(plain, panelH1+"People</h1>", panelH1+"New person</h1>", 1) != titled {
		t.Errorf("a Title alone changed more than the h1's words")
	}
}

var crumbsRE = regexp.MustCompile(`(?s)<nav aria-label="Breadcrumb" class="panel-crumbs"><ol class="panel-crumbs-list">(.*?)</ol></nav>`)

// TestPanelHead_TheLastCrumbIsTheCurrentPage: the trail is one nav named "Breadcrumb"; its
// last crumb is text marked aria-current="page" and carries no link even when given an href;
// the earlier crumbs are 44 px links, or text when they have no href.
func TestPanelHead_TheLastCrumbIsTheCurrentPage(t *testing.T) {
	c := headChrome()
	c.Crumbs = []pages.Crumb{
		{Label: "People", Href: "/admin/employees"},
		{Label: "Nowhere"},
		{Label: "Emma <Caruana>", Href: "/admin/employees?manage=1"},
	}
	got := renderPanelHead(t, c)
	if n := strings.Count(got, `aria-label="Breadcrumb"`); n != 1 {
		t.Fatalf("%d breadcrumb navs, want 1", n)
	}
	m := crumbsRE.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no breadcrumb nav with its ordered list in:\n%s", got)
	}
	trail := m[1]
	want := `<li class="panel-crumbs-item"><a href="/admin/employees" class="panel-crumb">People</a> <span aria-hidden="true">/</span></li>` +
		`<li class="panel-crumbs-item"><span>Nowhere</span> <span aria-hidden="true">/</span></li>` +
		`<li class="panel-crumbs-item"><span aria-current="page">Emma &lt;Caruana&gt;</span></li>`
	if trail != want {
		t.Errorf("the trail is\n%s\nwant\n%s", trail, want)
	}
	if strings.Contains(trail, "manage=1") {
		t.Errorf("the current page's crumb was drawn as a link")
	}
	if n := strings.Count(trail, `aria-current="page"`); n != 1 {
		t.Errorf("%d crumbs marked as the current page, want 1", n)
	}
	for _, pt := range pressTargetsOf(trail) {
		if !hasTouchTargetClass(pt.classes) {
			t.Errorf("a crumb <%s class=%q> carries no touch-target class", pt.tag, pt.classes)
		}
	}
	// One crumb is only the current page.
	c.Crumbs = []pages.Crumb{{Label: "Rules", Href: "/admin/policies"}}
	if m := crumbsRE.FindStringSubmatch(renderPanelHead(t, c)); m == nil ||
		m[1] != `<li class="panel-crumbs-item"><span aria-current="page">Rules</span></li>` {
		t.Errorf("a one-crumb trail is not the current page alone: %q", m)
	}
}

// TestPanelHead_KickerAndActionsOnlyWhenSet: each of the eyebrow, the trail and the actions
// is drawn once when set and not at all when not, the eyebrow before the h1 and the actions
// after it; none of the head's markup carries anything of the tenant's brand.
func TestPanelHead_KickerAndActionsOnlyWhenSet(t *testing.T) {
	kicker := `<p class="lbl">Tue 20 Oct · 14:32 · Europe/Malta</p>`
	actions := `<div class="panel-actions"><a class="btn" href="/admin/employees">Add a person</a></div>`
	trail := `aria-label="Breadcrumb"`
	for _, tc := range []struct {
		name                      string
		set                       func(*pages.PanelChrome) templ.Component
		kicker, actions, hasTrail bool
	}{
		{"a kicker", func(c *pages.PanelChrome) templ.Component {
			c.Kicker = "Tue 20 Oct · 14:32 · Europe/Malta"
			return nil
		}, true, false, false},
		{"actions", func(c *pages.PanelChrome) templ.Component {
			return templ.Raw(`<a class="btn" href="/admin/employees">Add a person</a>`)
		}, false, true, false},
		{"a trail", func(c *pages.PanelChrome) templ.Component {
			c.Crumbs = []pages.Crumb{{Label: "People"}}
			return nil
		}, false, false, true},
		{"all three", func(c *pages.PanelChrome) templ.Component {
			c.Kicker = "Tue 20 Oct · 14:32 · Europe/Malta"
			c.Crumbs = []pages.Crumb{{Label: "People"}}
			return templ.Raw(`<a class="btn" href="/admin/employees">Add a person</a>`)
		}, true, true, true},
	} {
		c := headChrome()
		got := renderPanelHeadWith(t, c, tc.set(&c))
		count := func(s string) int { return strings.Count(got, s) }
		if want := b2i(tc.kicker); count(kicker) != want {
			t.Errorf("%s: the kicker is drawn %d times, want %d", tc.name, count(kicker), want)
		}
		if want := b2i(tc.actions); count(actions) != want {
			t.Errorf("%s: the actions are drawn %d times, want %d", tc.name, count(actions), want)
		}
		if want := b2i(tc.hasTrail); count(trail) != want {
			t.Errorf("%s: the trail is drawn %d times, want %d", tc.name, count(trail), want)
		}
		if count(`<div class="panel-head">`) != 1 {
			t.Errorf("%s: the head's wrapper is drawn %d times, want 1", tc.name, count(`<div class="panel-head">`))
		}
		h1 := strings.Index(got, "<h1")
		if tc.kicker && strings.Index(got, kicker) > h1 {
			t.Errorf("%s: the kicker comes after the h1", tc.name)
		}
		if tc.actions && strings.Index(got, actions) < h1 {
			t.Errorf("%s: the actions come before the h1", tc.name)
		}
	}
}

// TestPanelHead_TheAccentDoesNotReachTheHeading: for a branded business (an accent and a
// name) with every slot set, the stripe is still drawn once, in the side column's header,
// and the heading block carries no stripe and no brand class -- the accent's slots do not
// grow (ADR 0023 §2; the compiled-CSS half is TestCompiledCSS_BrandVariablesOnlyInTheirSlots).
func TestPanelHead_TheAccentDoesNotReachTheHeading(t *testing.T) {
	accent, err := brand.ParseAccent("9A5F14")
	if err != nil {
		t.Fatal(err)
	}
	c := headChrome()
	c.Brand = pages.PanelBrand{Name: "Kebab Factory", Theme: layout.ThemeOf(accent)}
	c.Kicker, c.Title = "People · add", "New person"
	c.Crumbs = []pages.Crumb{{Label: "People", Href: "/admin/employees"}, {Label: "New person"}}
	got := renderPanelHeadWith(t, c, templ.Raw(`<a class="btn" href="/admin/employees">Cancel</a>`))
	if n := strings.Count(got, "panel-stripe"); n != 1 {
		t.Fatalf("the stripe is drawn %d times, want 1", n)
	}
	start := strings.Index(got, `<div class="panel-head">`)
	end := strings.Index(got, "the section's own body")
	if start < 0 || end < start {
		t.Fatalf("no heading block before the body")
	}
	head := got[start:end]
	if strings.Contains(strings.ToLower(head), "brand") || strings.Contains(head, "panel-stripe") ||
		strings.Contains(head, "Kebab Factory") {
		t.Errorf("the heading carries something of the tenant's brand:\n%s", head)
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestPanelActions_ANilWrapperIsNoWrapperAndTheScriptedShellTakesIt: PanelActions(nil) around a
// shell renders the shell byte for byte as without it, and the scripted shell (The Pass's)
// places actions the same way, once, after its h1 -- the wrapper works for both shells
// because neither changed its signature.
func TestPanelActions_ANilWrapperIsNoWrapperAndTheScriptedShellTakesIt(t *testing.T) {
	body := templ.Raw(`<p>the section's own body</p>`)
	render := func(root, inner templ.Component) string {
		t.Helper()
		var b bytes.Buffer
		if err := root.Render(templ.WithChildren(context.Background(), inner), &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	inShell := func(shell templ.Component) templ.Component {
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			return shell.Render(templ.WithChildren(ctx, body), w)
		})
	}
	c := headChrome()
	c.Tab = pages.TabTransactions
	scripted := pages.PanelShellWithScript(c, "/static/vendor/htmx.min.js")
	bare := render(scripted, body)
	if got := render(pages.PanelActions(nil), inShell(scripted)); got != bare {
		t.Errorf("PanelActions(nil) changed the scripted shell's render (%d bytes vs %d)", len(got), len(bare))
	}
	if got := render(pages.PanelActions(nil), inShell(pages.PanelShell(c))); got != render(pages.PanelShell(c), body) {
		t.Errorf("PanelActions(nil) changed the scriptless shell's render")
	}
	got := render(pages.PanelActions(templ.Raw(`<a class="btn" href="/admin">Today</a>`)), inShell(scripted))
	want := `<div class="panel-actions"><a class="btn" href="/admin">Today</a></div>`
	if strings.Count(got, want) != 1 || strings.Index(got, want) < strings.Index(got, "<h1") ||
		strings.Count(got, "<h1") != 1 || strings.Count(got, "htmx.min.js") != 1 {
		t.Errorf("the scripted shell did not place the actions once after its one h1, or lost its script")
	}
}
