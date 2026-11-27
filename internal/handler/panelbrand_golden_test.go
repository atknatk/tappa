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
//
// M12-09 replaced ONE panel digest, "AdminDashboard, owner, reports", whose section body is an
// empty state that moved to components.PanelEmpty; the bytes differ only inside that empty
// state's opening tags, and TestPanelHead_EmptySlotsAreTheShellAsItWas undoes exactly that
// change and still finds the pre-M12-01 render.

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
		if s.Tab == pages.TabGroup {
			// GRP-1: these chromes are a business in no group's (InGroup false), whose chrome
			// GRP-1 left byte for byte as it was -- so the Group tab is in none of them, and a
			// Group-section render here would hold the h1 of a section this business is not
			// shown. A holding member's chrome is held by the Group tests (group_test.go).
			continue
		}
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
		unbrandedRender{"layout.Marketing, public", layout.Marketing("Taptime", layout.RobotsPublic, layout.Share{}), body},
		unbrandedRender{"layout.MarketingWithScript, private", layout.MarketingWithScript("Taptime", layout.RobotsPrivate, "/static/js/landing.js", layout.Share{}), body},
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
//
// RE-RENDERED FOR M12-01 (the h1's new size), and for nothing else: each panel render here
// is its 022ba3c render with the h1's class swapped, 63 bytes longer, and the undeclared
// tab (no h1) unchanged. That claim is not left to this comment --
// TestPanelHead_EmptySlotsAreTheShellAsItWas (panelhead_test.go) keeps the 022ba3c digests
// and puts the old class back into every render before comparing.
//
// RE-RENDERED FOR THE TAPTIME MARK (2026-11-27, user decision), and for nothing else: the
// unbranded header, pages.PanelWordmark, ends its word in the plaque and draws the mark
// alone on the rail, both ringless, 498 bytes longer in every panel render. Checked before replacing: each
// render with panelWordmarkBeforeMark put back in place of the new header is its previous
// digest. TestPanelHead_EmptySlotsAreTheShellAsItWas puts it back on every run.
var unbrandedPanelShell = map[string]unbrandedDigest{
	"PanelShell, owner, tab transactions":            {12075, "dba6cc9e32a6e1476d72fe8269e795b33de907ea3126caacad4cdeee27296860"},
	"PanelShell, owner, tab review":                  {12076, "4d2afa3f8b5205615e6edb001ef51f41565a6c0331826676ee855c54df1f1897"},
	"PanelShell, owner, tab employees":               {12073, "9bead6009446f2f01fdd63c9235dd17a2eec6aa9a04822a552d19284591344de"},
	"PanelShell, owner, tab locations":               {12167, "6f423f99913a5c4ec742a0468da01968d3d2a068ca3e7b352ad58f5fea5ef369"},
	"PanelShell, owner, tab reports":                 {12075, "905531342a711c2ab51b501952120d6ff8e395cf06422be4b1850e00c6ceae22"},
	"PanelShell, owner, tab anomalies":               {12147, "db0ae68813cb37b85faefc7a6ccc0c7432ddcc4c444d5bd269733ba90b7ac8c3"},
	"PanelShell, owner, tab policies":                {12137, "74bcd82cd40054a5878959740d6ace64c3bfec179e0babf05778cb46d716f0a6"},
	"PanelShell, owner, tab billing":                 {12141, "25900bb8f16b91de6dc4845f46f52893a526e1aa6d7bf91efcf507c1bbd37197"},
	"PanelShell, owner, tab account":                 {12141, "db36603a881f60d3ec01886f48b9e12635e9e9cc5e4d98a2c3fb2e2449aced2a"},
	"PanelShell, owner, tab developers":              {12147, "adf6113eb9c2de1e68a5a84ea38c84bf339cfe2b8ae989643b055a82275bc221"},
	"PanelShell, manager, review, capped badge":      {10471, "7adb78ba3f508ffec5a806899315a13325f17facfc1929942253f9fa0eb6615d"},
	"PanelShell, owner, transactions, unknown badge": {12113, "af13afb00f6909b11f821014cdb5ab19f7398ce26dfe5588a1fb43f10384e9d8"},
	"PanelShell, owner, transactions, empty queue":   {11927, "f62ffa1c45096fefb3a14d9c1a254722daf20bbb15db345ede23f627237de3e2"},
	"PanelShell, owner, an undeclared tab":           {11864, "7bcc81c329530870f9097e6389389f96388b41ad8a39d20fffe8095975628dbe"},
	"PanelShell, a name to escape":                   {12211, "21f7163d832e238c6ba26379d8a32d9738df7bee2aa00ab4d03bd3ddec033556"},
	"PanelShellWithScript, owner, transactions":      {12131, "dc2a8af4feae4f7cb9b6c8022fd95d5efe0ff25fa9c2315eb6bcc32596f74713"},
	"AdminDashboard, owner, reports":                 {12504, "0e74c31a5fa7c24f302e5cbbbbadf8f22eb9cd52f063ecaf4a569d5c61a52cdd"},
}

// panelWordmarkBeforeMark is pages.PanelWordmark as it rendered before the Taptime mark
// (2026-11-27): the text "t." on the rail and the word without its plaque.
const panelWordmarkBeforeMark = `<header class="side-head"><div class="side-lockup"><span class="side-mark" aria-hidden="true">t.</span><div class="side-words"><span class="side-word">taptime</span> <span class="side-tag">punchless</span></div></div></header>`

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
		if strings.HasPrefix(r.name, "layout.Marketing") {
			body = withoutCredit(t, r.name, body)
		}
		if got := digestUnbranded(body); got != want {
			t.Errorf("%s: %d bytes sha256 %s; %s it was %d bytes sha256 %s.\nrendered:\n%s",
				r.name, got.n, got.sum, from, want.n, want.sum, body)
		}
	}
	t.Logf("%d unbranded renders byte-identical to their goldens: %s", len(renders), strings.Join(names, "; "))
}

// marketingCredit is the operator company's two author tags (user request 2026-10-23),
// which the marketing shells' head has carried since; no other shell has them.
var marketingCredit = `<meta name="author" content="` + layout.CreditName + `"><link rel="author" href="` + layout.CreditURL + `">`

// withoutCredit removes the credit from a marketing render, which must carry it exactly
// once. THE MARKETING DIGESTS ARE STILL df544c1's: the credit is the only change to
// those shells since, so a render with it taken out is the bytes WL-8 pinned, and any
// other change still fails the digest.
func withoutCredit(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	if n := bytes.Count(body, []byte(marketingCredit)); n != 1 {
		t.Errorf("%s: the head carries the operator's credit %d time(s), want once", name, n)
	}
	return bytes.Replace(body, []byte(marketingCredit), nil, 1)
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
