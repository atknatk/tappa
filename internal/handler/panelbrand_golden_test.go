package handler

// panelbrand_golden_test.go -- M10 WL-8: the renders a business WITHOUT a brand gets,
// held to sha256 digests taken from the tree BEFORE WL-8 (df544c1, the m10-a1 branch
// tip WL-8 was built on). ADR 0023 §2 and the skill's "Tenant slots": no brand, no
// change -- no stylesheet after app.css, no <img>, no business name, the wordmark where
// it was.
//
// WHY THE COMPONENTS AND NOT THE ROUTES. WL-8 changed four templates -- the document
// head every shell shares (layout.documentHead), the two panel shells and the chrome
// (pages.PanelShell, pages.PanelShellWithScript, panelChrome) -- and the section bodies
// not at all. A section's whole response also carries what time.Now() said (billing,
// the transactions day), so a digest of it is a digest of the clock. The components
// below are rendered with fixed inputs, so their bytes are a function of the
// templates alone, and the list covers every shell the head reaches and the panel
// shells over every section's tab. That every SECTION response of an unbranded
// business is the chrome rendered here plus its own body is the router-level half:
// TestPanelBrand_UnbrandedSectionsAreTheWordmarkChrome (panelbrand_test.go).
//
// THIS FILE USES NOTHING WL-8 ADDED, on purpose: the same file, copied into an export
// of df544c1, compiles there and printed the digests below (the WL-8 card records the
// command). Since the merge with M10 WL-9 the layout.PageWithScript entry passes a
// zero layout.Brand, which df544c1 does not have: drop that argument to replay the
// export. A render that is not in unbrandedChromeRenders is not held -- PART III.
//
// 🔴 THE PANEL HALF WAS RE-BASED ON 2026-10-20, AND ONLY THE PANEL HALF. The panel shell
// of design "The Pass" (user-approved) replaced the chrome every panel entry renders --
// a sidebar/rail and a phone bar instead of a header and a tab bar -- so those entries
// can no longer be df544c1's bytes. Their digests (unbrandedPanelShell) were printed by
// this test from the commit that introduced that shell, with no brand; from there on the
// claim is the same one WL-8 made: an unbranded business's chrome changes only when the
// shell itself is meant to change, never because of the brand. What df544c1 proved
// structurally is asserted structurally for them (unbrandedPanelCarriesNoBrand). The
// layout entries, which the shell did not touch, are still held to df544c1.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// unbrandedRender is one component and the children it is rendered around.
type unbrandedRender struct {
	name     string
	c        templ.Component
	children templ.Component
}

// unbrandedChromeRenders is the corpus, built from fixed inputs only.
func unbrandedChromeRenders() []unbrandedRender {
	body := templ.Raw(`<p>the section's own body</p>`)
	chrome := func(role string, tab pages.PanelTab, p pages.PendingBadge) pages.PanelChrome {
		return pages.PanelChrome{FullName: "Maria Borg", Role: role, Tab: tab, Pending: p}
	}
	waiting := pages.PendingBadge{Known: true, N: 3}
	var out []unbrandedRender
	// The scriptless panel shell on every section's tab, as an owner (who sees every
	// tab, OwnerOnly included) with records waiting.
	for _, s := range pages.PanelSections {
		name := string(s.Tab)
		if s.Tab == pages.TabAPIKeys {
			// Named by its label: the tab's id beside a sha256 in the golden below is the
			// shape make audit's R7d (scripts/secretscan.sh, a0-token) refuses.
			name = "developers"
		}
		out = append(out, unbrandedRender{
			"PanelShell, owner, tab " + name, pages.PanelShell(chrome("owner", s.Tab, waiting)), body,
		})
	}
	out = append(out,
		// The badge's other three states, a manager's narrower tab bar, a tab nobody
		// declared, and a name that needs escaping.
		unbrandedRender{"PanelShell, manager, review, capped badge",
			pages.PanelShell(chrome("manager", pages.TabReview, pages.PendingBadge{Known: true, N: 100, Capped: true})), body},
		unbrandedRender{"PanelShell, owner, transactions, unknown badge",
			pages.PanelShell(chrome("owner", pages.TabTransactions, pages.PendingBadge{})), body},
		unbrandedRender{"PanelShell, owner, transactions, empty queue",
			pages.PanelShell(chrome("owner", pages.TabTransactions, pages.PendingBadge{Known: true})), body},
		unbrandedRender{"PanelShell, owner, an undeclared tab",
			pages.PanelShell(chrome("owner", pages.PanelTab("nope"), waiting)), body},
		unbrandedRender{"PanelShell, a name to escape",
			pages.PanelShell(pages.PanelChrome{FullName: `Ċikku <O'Brien> & "Co"`, Role: "owner", Tab: pages.TabAccount, Pending: waiting}), body},
		// The scripted shell, as the transactions section names it.
		unbrandedRender{"PanelShellWithScript, owner, transactions",
			pages.PanelShellWithScript(chrome("owner", pages.TabTransactions, waiting), "/static/vendor/htmx.min.js"), body},
		// A whole section page that is the shell and nothing time-dependent.
		unbrandedRender{"AdminDashboard, owner, reports",
			pages.AdminDashboard(pages.AdminDashboardView{PanelChrome: chrome("owner", pages.TabReports, waiting)}), nil},
		// Every other shell that renders layout.documentHead.
		unbrandedRender{"layout.Page", layout.Page("A page — Taptime"), body},
		unbrandedRender{"layout.PageWithScript", layout.PageWithScript("A page — Taptime", "/static/js/tap.js", layout.Brand{}), body},
		unbrandedRender{"layout.Marketing, public", layout.Marketing("Taptime", layout.RobotsPublic), body},
		unbrandedRender{"layout.MarketingWithScript, private", layout.MarketingWithScript("Taptime", layout.RobotsPrivate, "/static/js/landing.js"), body},
		unbrandedRender{"layout.Auth, no script", layout.Auth("Sign in — Taptime", ""), body},
		unbrandedRender{"layout.Auth, a script", layout.Auth("Sign in — Taptime", "/static/js/login.js"), body},
		unbrandedRender{"layout.Operator", layout.Operator("Taptime operator"), body},
		unbrandedRender{"layout.OperatorWithScript", layout.OperatorWithScript("Taptime operator", "/static/js/operator/enroll.js"), body},
	)
	return out
}

// renderUnbranded renders one entry of the corpus.
func renderUnbranded(t *testing.T, r unbrandedRender) []byte {
	t.Helper()
	ctx := context.Background()
	if r.children != nil {
		ctx = templ.WithChildren(ctx, r.children)
	}
	var buf bytes.Buffer
	if err := r.c.Render(ctx, &buf); err != nil {
		t.Fatalf("%s: render: %v", r.name, err)
	}
	return buf.Bytes()
}

// unbrandedDigest is the length and sha256 of one render.
type unbrandedDigest struct {
	n   int
	sum string
}

func digestUnbranded(b []byte) unbrandedDigest {
	s := sha256.Sum256(b)
	return unbrandedDigest{len(b), hex.EncodeToString(s[:])}
}

// unbrandedBeforeWL8 is what df544c1 rendered for each entry (printed by this file's
// corpus in an export of that commit; the WL-8 card has the command).
var unbrandedBeforeWL8 = map[string]unbrandedDigest{
	"layout.Page":                         {751, "48e1d0d1e5613447b116b8433e8b09e201c468194fe4f94a5aeaf3c34eded655"},
	"layout.PageWithScript":               {798, "651c72839b42551a6243ff04d2108c2dc572d1d340831a0c2599afc506e4085f"},
	"layout.Marketing, public":            {418, "7a041e0a20ff3279ea35c4a0d75526c1c1b6a880fe1cb2ab6e77d1936207dc23"},
	"layout.MarketingWithScript, private": {473, "136e58c6558802fe7a906e35cd3d0551b6ddd717fb1e60f556c34790582b9469"},
	"layout.Auth, no script":              {434, "4058bbc5591c3dd2178648a916508d15c66724d5c5be0cc5266125b7134c19df"},
	"layout.Auth, a script":               {483, "9381dd03c38e63d1e44124b3faa39e5e0ae4498a8691dd24d4fd5a86068d39b0"},
	"layout.Operator":                     {431, "351dc3118a49669cc22ff089e7b395ae6b5ee44c3e694972bcef290a3993463d"},
	"layout.OperatorWithScript":           {490, "149cd73492810613e5787970b52b0fa3fd4006866279f0940061bae4b42c663a"},
}

// unbrandedPanelShell is what the panel entries render with no brand in the shell of
// design "The Pass" (2026-10-20), printed by this test from the commit that introduced it.
var unbrandedPanelShell = map[string]unbrandedDigest{
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

// isPanelRender says which corpus entries render the panel shell (and so are held to
// unbrandedPanelShell) rather than one of the layouts df544c1 still pins.
func isPanelRender(name string) bool {
	return strings.HasPrefix(name, "PanelShell") || strings.HasPrefix(name, "AdminDashboard")
}

// TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8 asserts, for every render of
// unbrandedChromeRenders, that its bytes are the bytes its golden holds (length and
// sha256): df544c1's for a layout, the 2026-10-20 shell's for a panel render -- which
// must also carry nothing of a brand and the owner-only API keys link exactly for an
// owner. The corpus and the goldens name the same renders. It prints the count and the
// names.
func TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8(t *testing.T) {
	renders := unbrandedChromeRenders()
	if len(renders) != len(unbrandedBeforeWL8)+len(unbrandedPanelShell) {
		t.Errorf("the corpus has %d renders and the goldens %d+%d; they must name the same renders",
			len(renders), len(unbrandedBeforeWL8), len(unbrandedPanelShell))
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(renders))
	for _, r := range renders {
		if seen[r.name] {
			t.Fatalf("the corpus names %q twice", r.name)
		}
		seen[r.name] = true
		names = append(names, r.name)
		golden, from := unbrandedBeforeWL8, "before WL-8 (df544c1)"
		if isPanelRender(r.name) {
			golden, from = unbrandedPanelShell, "in the 2026-10-20 shell"
		}
		want, ok := golden[r.name]
		if !ok {
			t.Errorf("%s: no digest from %s", r.name, from)
			continue
		}
		body := renderUnbranded(t, r)
		if isPanelRender(r.name) {
			unbrandedPanelCarriesNoBrand(t, r.name, body)
		}
		if got := digestUnbranded(body); got != want {
			t.Errorf("%s: %d bytes sha256 %s; %s it was %d bytes sha256 %s.\nrendered:\n%s",
				r.name, got.n, got.sum, from, want.n, want.sum, body)
		}
	}
	t.Logf("%d unbranded renders byte-identical to their goldens: %s", len(renders), strings.Join(names, "; "))
}

// unbrandedPanelCarriesNoBrand is WL-8's "no brand, no change" as properties of one
// unbranded panel render: app.css is the only stylesheet, and there is no <img>, no
// stripe, no theme or logo route, no co-brand line; the lockup is the product's
// (pages.PanelWordmark), once. And the owner-only API keys section (M11 API-3) is
// linked exactly in an owner's render -- from the sidebar and from the phone's More
// sheet -- and nowhere in a manager's.
func unbrandedPanelCarriesNoBrand(t *testing.T, name string, body []byte) {
	t.Helper()
	page := string(body)
	if hrefs, inHead := stylesheetsOf(page); len(hrefs) != 1 || hrefs[0] != "/static/css/app.css" || !inHead {
		t.Errorf("%s: stylesheets %v (all in head: %v), want app.css alone", name, hrefs, inHead)
	}
	for _, never := range []string{"<img", "panel-stripe", "/brand/theme/", "/admin/brand/logo/", "taptime · punchless", "side-head--named"} {
		if strings.Contains(page, never) {
			t.Errorf("%s: an unbranded chrome carries %q", name, never)
		}
	}
	if n := strings.Count(page, wordmarkHTML(t)); n != 1 {
		t.Errorf("%s: the product's lockup is drawn %d times, want once", name, n)
	}
	owner := strings.Contains(name, "owner") || name == "PanelShell, a name to escape"
	n := strings.Count(page, `href="/admin/api-keys"`)
	switch {
	case owner && n != 2:
		t.Errorf("%s: an owner's chrome links the API keys section %d times, want twice (sidebar and More sheet)", name, n)
	case !owner && n != 0:
		t.Errorf("%s: links the owner-only API keys section %d times, want none", name, n)
	}
}
