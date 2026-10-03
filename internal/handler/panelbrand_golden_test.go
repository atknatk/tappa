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
// command). A render that is not in unbrandedChromeRenders is not held -- PART III.

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
		out = append(out, unbrandedRender{
			"PanelShell, owner, tab " + string(s.Tab), pages.PanelShell(chrome("owner", s.Tab, waiting)), body,
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
		unbrandedRender{"layout.PageWithScript", layout.PageWithScript("A page — Taptime", "/static/js/tap.js"), body},
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
	"PanelShell, owner, tab transactions":            {1933, "6ea94a81b3bce260589397d479dae41bdde9f8133c7012d20157d12b94b2736f"},
	"PanelShell, owner, tab review":                  {1933, "361b043c0cc5c892a16d0655ef5892471d69b76a3b41ea6bccfe3e3e8e274733"},
	"PanelShell, owner, tab employees":               {1930, "bd791ffea1dff7542bced8ca92250f874f4de890d32b4338bdfdbb717b22ac21"},
	"PanelShell, owner, tab locations":               {1946, "f600a076e0387f0c4a1a5aeb88cf217d9a6f5dfbfeade7ee842f6eb27f77ffe6"},
	"PanelShell, owner, tab reports":                 {1928, "7844c43b750d2fc9847b25999640c1b8f6d6fa4f506eab1d9da3ea93ab9d00fc"},
	"PanelShell, owner, tab anomalies":               {1930, "62f327dae28a7d8ea506038b44a1937d38af95eb3b4c9f50bc9e5757d9ba1f0d"},
	"PanelShell, owner, tab policies":                {1929, "13d9c5d31714e75cd4fbba91ca4f462635ab14f9d9f357e5ca480273205f8745"},
	"PanelShell, owner, tab billing":                 {1928, "b760ec0c0d1d0c0849221a20cf6d32c897c214366164cdd04c9a8d8f1eb66744"},
	"PanelShell, owner, tab account":                 {1928, "9cc375f69698b5a1ddce3ae607278cc9af7b43a189b46bb3af3bedc3ad86c882"},
	"PanelShell, manager, review, capped badge":      {1897, "e3846d3c5771a4c4dab47c6c0ef72249dedc019757ce34fb3a5a792d205cdbe7"},
	"PanelShell, owner, transactions, unknown badge": {1952, "427bf16a5eb3ccbe649beb5a8e4c8dd3853be78c2c085fd09179de54749ed69e"},
	"PanelShell, owner, transactions, empty queue":   {1859, "2d3b205ddf2d180eb008156845647d8310f720902660c5b72e318a4a169460ff"},
	"PanelShell, owner, an undeclared tab":           {1819, "9ae50ef96516899eebb2e7858d5d00b8badbe690d6dbc390a9ac66fe7ae6842c"},
	"PanelShell, a name to escape":                   {1963, "d5a731d009615b89436c94e8bc54eb1ed2202e327bc88bd81960beb7cc6ea68e"},
	"PanelShellWithScript, owner, transactions":      {1989, "6b349a01804a411d79874a66b433cb188a2d3f1580c644d58a2f5cb0370c32a3"},
	"AdminDashboard, owner, reports":                 {2278, "51c26dfdd840c8656b8c5ccadbe1cbe6a774d7e26fc1f4af6e114d02bededfcc"},
	"layout.Page":                                    {751, "48e1d0d1e5613447b116b8433e8b09e201c468194fe4f94a5aeaf3c34eded655"},
	"layout.PageWithScript":                          {798, "651c72839b42551a6243ff04d2108c2dc572d1d340831a0c2599afc506e4085f"},
	"layout.Marketing, public":                       {418, "7a041e0a20ff3279ea35c4a0d75526c1c1b6a880fe1cb2ab6e77d1936207dc23"},
	"layout.MarketingWithScript, private":            {473, "136e58c6558802fe7a906e35cd3d0551b6ddd717fb1e60f556c34790582b9469"},
	"layout.Auth, no script":                         {434, "4058bbc5591c3dd2178648a916508d15c66724d5c5be0cc5266125b7134c19df"},
	"layout.Auth, a script":                          {483, "9381dd03c38e63d1e44124b3faa39e5e0ae4498a8691dd24d4fd5a86068d39b0"},
	"layout.Operator":                                {431, "351dc3118a49669cc22ff089e7b395ae6b5ee44c3e694972bcef290a3993463d"},
	"layout.OperatorWithScript":                      {490, "149cd73492810613e5787970b52b0fa3fd4006866279f0940061bae4b42c663a"},
}

// TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8 asserts, for every render of
// unbrandedChromeRenders, that its bytes are the bytes df544c1 rendered (length and
// sha256), and that the corpus and the golden name the same renders. It prints the
// count and the names.
func TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8(t *testing.T) {
	renders := unbrandedChromeRenders()
	if len(renders) != len(unbrandedBeforeWL8) {
		t.Errorf("the corpus has %d renders and the golden %d; they must name the same renders",
			len(renders), len(unbrandedBeforeWL8))
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(renders))
	for _, r := range renders {
		if seen[r.name] {
			t.Fatalf("the corpus names %q twice", r.name)
		}
		seen[r.name] = true
		names = append(names, r.name)
		want, ok := unbrandedBeforeWL8[r.name]
		if !ok {
			t.Errorf("%s: no digest from before WL-8", r.name)
			continue
		}
		body := renderUnbranded(t, r)
		if got := digestUnbranded(body); got != want {
			t.Errorf("%s: %d bytes sha256 %s; before WL-8 it was %d bytes sha256 %s.\nrendered:\n%s",
				r.name, got.n, got.sum, want.n, want.sum, body)
		}
	}
	t.Logf("%d unbranded renders byte-identical to df544c1: %s", len(renders), strings.Join(names, "; "))
}
