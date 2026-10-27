package handler

// panelbusiness_test.go -- M14-12: the business's name in the panel shell on every section of
// every business, and the "Switch business" link behind the identity flag (ADR 0031 §5.3, §8,
// K-5; panelbusiness.go, pages/panelbusinessview.go).

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// shellBusinessName is the name the session's statement returns in these tests: one that needs
// escaping, with Maltese letters.
const shellBusinessName = `Ħaġar <Qim> & "Kebabs"`

// shellBrowser is the real panel, with the identity flag as given, signed in as an owner whose
// session resolves with TenantName name and Linked linked; brands answers the chrome's brand read.
// The membership list -- the switch page's cross-business read -- fails the test if anything asks
// for it: the shell must not.
func shellBrowser(t *testing.T, flag, linked bool, name string, brands panelBrands) (*browser, *fakeAdmins) {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner", Linked: linked, TenantName: name,
		}, nil
	}}
	// M14-18: the account page's "Your businesses" (flag on) is the one panel page that reads the
	// list -- in its body, never in the shell; every other path asking for it is the failure.
	var once sync.Once
	var path string
	admins.memberships = func() ([]adminauth.Membership, error) {
		if path != accountHref {
			once.Do(func() { t.Errorf("a panel section asked for the person's membership list (%s)", path) })
		}
		return nil, nil
	}
	cfg := adminTestConfig()
	cfg.AdminIdentity = flag
	records := newFakeLedger()
	records.asOf = panelBrandAsOf
	h, err := NewAdminAuth(admins, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), brands, newFakeBrandWriter(), nil, &fakeNotices{}, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	// GETs only: the wrapper is not a registered panel router (it is a func, not a map key), and no
	// test of this file posts a bound form through it.
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path = req.URL.Path
		r.ServeHTTP(w, req)
	})
	b := newBrowser(t, wrapped)
	b.cookies[adminauth.CookieName] = panelCookie().Value
	return b, admins
}

// businessBlock is the exact markup the shell draws for an unbranded business named name (escaped
// here), with the switch link when link.
func businessBlock(name string, link bool) string {
	s := `<div class="side-biz">`
	if name != "" {
		s += `<p class="side-biz-name">` + htmlEscaper.Replace(name) + `</p>`
	}
	if link {
		s += `<a href="/admin/switch" class="side-switch">Switch business</a>`
	}
	return s + `</div>`
}

// TestPanelShell_NamesTheBusinessOnEverySection: every section of an unbranded business draws the
// business block exactly once, between the header and the section navigation, holding the name the
// session's statement returned (escaped) -- with the identity flag off and on, for a linked and an
// unlinked row; an empty tenants.name prints businessName's fallback. The name costs no read: each
// request resolves the session once and asks for no membership list, and the brand reader is asked
// once per request as before (TestPanelBrand_* count it). Each page still has exactly one <h1>.
//
// MUTATIONS (measured): the Name line of panelBusiness removed -> red; the block's call in the
// shell removed -> red; the name read from the brand instead of the session -> red.
func TestPanelShell_NamesTheBusinessOnEverySection(t *testing.T) {
	assertSectionTableIsUsable(t)
	for _, c := range []struct {
		flag, linked bool
		name, want   string
	}{
		{false, false, shellBusinessName, shellBusinessName},
		{false, true, shellBusinessName, shellBusinessName},
		{true, false, shellBusinessName, shellBusinessName},
		{true, true, shellBusinessName, shellBusinessName},
		{false, false, "", "This business"},
		{true, true, "", "This business"},
	} {
		b, admins := shellBrowser(t, c.flag, c.linked, c.name, newFakeBrands())
		block := businessBlock(c.want, c.flag)
		for _, s := range pages.PanelSections {
			where := s.Href
			got := getSection(t, b, s.Href)
			if got.status != http.StatusOK {
				t.Fatalf("flag %v, linked %v, GET %s: %d", c.flag, c.linked, where, got.status)
			}
			if n := strings.Count(got.body, `class="side-biz"`); n != 1 {
				t.Errorf("flag %v, linked %v, GET %s: %d business blocks, want 1", c.flag, c.linked, where, n)
			}
			i, head, nav := strings.Index(got.body, block), strings.Index(got.body, `</header>`), strings.Index(got.body, `<nav class="side-nav"`)
			if i < 0 || head < 0 || nav < 0 || i != head+len(`</header>`) || i+len(block) != nav {
				t.Errorf("flag %v, linked %v, GET %s: the block %q is not the markup between the header and the navigation",
					c.flag, c.linked, where, block)
			}
			if n := strings.Count(got.body, "<h1"); n != 1 {
				t.Errorf("flag %v, linked %v, GET %s: %d <h1>, want 1", c.flag, c.linked, where, n)
			}
		}
		if n := admins.verifiedCount(); n != len(pages.PanelSections) {
			t.Errorf("flag %v, linked %v: %d session resolutions for %d requests, want one each",
				c.flag, c.linked, n, len(pages.PanelSections))
		}
	}
}

// TestPanelShell_TheSwitchLinkIsFlagGated (ADR 0031 §8): with TAPPA_ADMIN_IDENTITY off no section
// carries the switch page's path or the link's words -- linked row or not, branded or not; with it
// on every section carries exactly one link to /admin/switch, in the block, for a linked and an
// unlinked row alike (the link is always there: one business or many) and for a branded business,
// whose block is then the link alone because its header prints the name.
//
// MUTATIONS (measured): SurfaceShellSwitchLink's row opened with the flag off -> red; the
// SwitchHref line of panelBusiness removed -> red; the link drawn only for a linked row -> red.
func TestPanelShell_TheSwitchLinkIsFlagGated(t *testing.T) {
	assertSectionTableIsUsable(t)
	for _, flag := range []bool{false, true} {
		for _, linked := range []bool{false, true} {
			for _, branded := range []bool{false, true} {
				brands := newFakeBrands()
				if branded {
					brands.set(wl8Brand(t, true, false), nil)
				}
				b, _ := shellBrowser(t, flag, linked, shellBusinessName, brands)
				for _, s := range pages.PanelSections {
					where := s.Href
					body := getSection(t, b, s.Href).body
					links := strings.Count(body, `href="/admin/switch"`)
					words := strings.Count(body, "Switch business")
					switch {
					case !flag && (links != 0 || words != 0 || strings.Contains(body, "/admin/switch")):
						t.Errorf("flag off, linked %v, branded %v, GET %s: the page names the switch (%d links, %d words)",
							linked, branded, where, links, words)
					case flag && (links != 1 || words != 1):
						t.Errorf("flag on, linked %v, branded %v, GET %s: %d switch links and %d mentions, want one",
							linked, branded, where, links, words)
					}
					if branded {
						want := `<div class="side-biz"><a href="/admin/switch" class="side-switch">Switch business</a></div>`
						n := strings.Count(body, `class="side-biz"`)
						if flag && (n != 1 || !strings.Contains(body, want)) {
							t.Errorf("flag on, branded, GET %s: the block is not the link alone", where)
						}
						if !flag && n != 0 {
							t.Errorf("flag off, branded, GET %s: %d business blocks beside the header that names the business", where, n)
						}
						if strings.Contains(body, `class="side-biz-name"`) {
							t.Errorf("flag %v, branded, GET %s: the name is printed twice", flag, where)
						}
					}
				}
			}
		}
	}
}

// TestPanelShell_FlagOffLinkedAndUnlinkedAreTheSameBytes (ADR 0031 §8, R4): with the flag off a
// linked row's session and an unlinked one's get the same shell bytes -- status, policy, and the
// whole page outside the section's content well -- from every section: the shell's name is the
// business's, whatever the row's link. (The well is left out because some fakes mint ids per
// instance -- the policies section's rule ids -- which says nothing about the session.)
func TestPanelShell_FlagOffLinkedAndUnlinkedAreTheSameBytes(t *testing.T) {
	assertSectionTableIsUsable(t)
	unlinked, _ := shellBrowser(t, false, false, shellBusinessName, newFakeBrands())
	linked, _ := shellBrowser(t, false, true, shellBusinessName, newFakeBrands())
	outside := func(where, body string) string {
		open, end := strings.Index(body, `<main class="panel-main">`), strings.Index(body, `</main>`)
		if open < 0 || end < open {
			t.Fatalf("%s: no content well", where)
		}
		return body[:open] + body[end:]
	}
	for _, s := range pages.PanelSections {
		a, b := getSection(t, unlinked, s.Href), getSection(t, linked, s.Href)
		if a.status != b.status || a.csp != b.csp || outside(s.Href, a.body) != outside(s.Href, b.body) {
			t.Errorf("flag off, GET %s: a linked and an unlinked session get different shells", s.Href)
		}
	}
}

// namedPanelRenders is the PANEL half of unbrandedChromeRenders (panelbrand_golden_test.go), by
// the same names and from the same fixed inputs, with biz as the chrome's business block. That
// file is kept replayable in an export of df544c1 and so cannot name pages.PanelBusiness; this
// copy can, and TestPanelShell_TheNamedShellIsTheGoldenPlusTheBlock holds its names to the
// golden's.
func namedPanelRenders(biz pages.PanelBusiness) []unbrandedRender {
	body := templ.Raw(`<p>the section's own body</p>`)
	chrome := func(role string, tab pages.PanelTab, p pages.PendingBadge) pages.PanelChrome {
		return pages.PanelChrome{FullName: "Maria Borg", Role: role, Tab: tab, Pending: p, Business: biz}
	}
	waiting := pages.PendingBadge{Known: true, N: 3}
	var out []unbrandedRender
	for _, s := range pages.PanelSections {
		if s.Tab == pages.TabGroup {
			continue
		}
		name := string(s.Tab)
		if s.Tab == pages.TabAPIKeys {
			name = "developers"
		}
		out = append(out, unbrandedRender{"PanelShell, owner, tab " + name, pages.PanelShell(chrome("owner", s.Tab, waiting)), body})
	}
	return append(out,
		unbrandedRender{"PanelShell, manager, review, capped badge",
			pages.PanelShell(chrome("manager", pages.TabReview, pages.PendingBadge{Known: true, N: 100, Capped: true})), body},
		unbrandedRender{"PanelShell, owner, transactions, unknown badge",
			pages.PanelShell(chrome("owner", pages.TabTransactions, pages.PendingBadge{})), body},
		unbrandedRender{"PanelShell, owner, transactions, empty queue",
			pages.PanelShell(chrome("owner", pages.TabTransactions, pages.PendingBadge{Known: true})), body},
		unbrandedRender{"PanelShell, owner, an undeclared tab",
			pages.PanelShell(chrome("owner", pages.PanelTab("nope"), waiting)), body},
		unbrandedRender{"PanelShell, a name to escape",
			pages.PanelShell(pages.PanelChrome{FullName: `Ċikku <O'Brien> & "Co"`, Role: "owner", Tab: pages.TabAccount, Pending: waiting, Business: biz}), body},
		unbrandedRender{"PanelShellWithScript, owner, transactions",
			pages.PanelShellWithScript(chrome("owner", pages.TabTransactions, waiting), "/static/vendor/htmx.min.js"), body},
		unbrandedRender{"AdminDashboard, owner, reports",
			pages.AdminDashboard(pages.AdminDashboardView{PanelChrome: chrome("owner", pages.TabReports, waiting)}), nil},
	)
}

// TestPanelShell_TheNamedShellIsTheGoldenPlusTheBlock: M14-12 changes an unbranded business's shell
// by exactly the business block. Every panel render of the WL-8 golden's corpus, drawn with a
// business name (flag off) and with a name and the switch link (flag on), carries the block
// exactly once, and with that one block taken out its bytes are the golden's digest
// (unbrandedPanelShell) -- so nothing else in the shell moved. The golden itself, whose chromes
// carry no business, is unchanged (TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8).
func TestPanelShell_TheNamedShellIsTheGoldenPlusTheBlock(t *testing.T) {
	for _, link := range []bool{false, true} {
		biz := pages.PanelBusiness{Name: shellBusinessName}
		if link {
			biz.SwitchHref = adminSwitchHref
		}
		block := businessBlock(shellBusinessName, link)
		renders := namedPanelRenders(biz)
		if len(renders) != len(unbrandedPanelShell) {
			t.Errorf("%d named renders, the golden holds %d panel renders", len(renders), len(unbrandedPanelShell))
		}
		for _, r := range renders {
			want, ok := unbrandedPanelShell[r.name]
			if !ok {
				t.Errorf("%s: not a render the golden holds", r.name)
				continue
			}
			body := renderUnbranded(t, r)
			if n := bytes.Count(body, []byte(block)); n != 1 {
				t.Errorf("link %v, %s: the block is drawn %d times, want once", link, r.name, n)
				continue
			}
			if got := digestUnbranded(bytes.Replace(body, []byte(block), nil, 1)); got != want {
				t.Errorf("link %v, %s: without the block, %d bytes sha256 %s; the golden is %d bytes sha256 %s",
					link, r.name, got.n, got.sum, want.n, want.sum)
			}
		}
	}
}

// TestPanelBusinessView_TheZeroValueDrawsNothing: a chrome with no business renders no block (the
// golden corpus relies on it), and a branded chrome with a name but no link renders none either.
func TestPanelBusinessView_TheZeroValueDrawsNothing(t *testing.T) {
	for name, c := range map[string]pages.PanelChrome{
		"no business":             {FullName: "Maria Borg", Role: "owner", Tab: pages.TabReports},
		"branded, name, no link":  {FullName: "Maria Borg", Role: "owner", Tab: pages.TabReports, Brand: pages.PanelBrand{Name: "X", Theme: layout.ThemeOf(wl8Accent(t))}, Business: pages.PanelBusiness{Name: "X"}},
		"unbranded, empty fields": {FullName: "Maria Borg", Role: "owner", Tab: pages.TabReports, Business: pages.PanelBusiness{}},
	} {
		var buf bytes.Buffer
		if err := pages.PanelShell(c).Render(templ.WithChildren(context.Background(), templ.Raw(`<p>body</p>`)), &buf); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(buf.String(), "side-biz") || strings.Contains(buf.String(), "side-switch") {
			t.Errorf("%s: the shell draws a business block", name)
		}
	}
}

// hidingDeclarations are the declarations that take an element's text off the screen while
// leaving it in the document: the visually-hidden recipe's (position, 1px box, clip) and the
// plain ways.
var hidingDeclarations = []string{"clip:rect(0,0,0,0)", "position:absolute", "width:1px", "display:none",
	"visibility:hidden", "opacity:0"}

// hiddenRulesOf returns, for class, every flat rule of the compiled stylesheet whose selector names
// it and that carries one of hidingDeclarations, and how many rules name it at all. It reads rules
// inside @media too (the flat-rule shape TestPanelScreens_TouchTargetClassesReserve44px reads).
func hiddenRulesOf(css, class string) (hidden []string, named int) {
	ruleRE := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	sel := regexp.MustCompile(`\.` + regexp.QuoteMeta(class) + `(?:[^-\w]|$)`)
	for _, rule := range ruleRE.FindAllStringSubmatch(css, -1) {
		if !sel.MatchString(rule[1]) {
			continue
		}
		named++
		for _, d := range hidingDeclarations {
			if strings.Contains(rule[2], d) {
				hidden = append(hidden, rule[0])
				break
			}
		}
	}
	return hidden, named
}

// TestPanelShell_TheRailShowsTheBusinessName (M14-12 review): no rule of the compiled stylesheet,
// at any width, takes the unbranded business's name off the screen -- on the tablet's rail
// included, where an unbranded business has no initial or logo, so a hidden name would leave the
// business unnamed (and M14-13's refusal page sends the person to the panel to see it). The
// name's rules are found at the phone, rail and sidebar widths. CONTROL: the same reader finds the
// branded header's name block hidden on the rail (it is, on purpose: the branded rail shows the
// logo or the initial), so a reader that sees nothing cannot pass.
//
// ⚠️ IT NEEDS A COMPILED web/static/css/app.css (CI runs make css first); without one it SKIPs,
// and a SKIP IS NOT A PASS.
//
// MUTATION (measured): the rail's screen-reader-only utility put back on the name's rule in
// input.css, make css -> red.
func TestPanelShell_TheRailShowsTheBusinessName(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) -- run `make css`. THIS IS NOT A PASS.", err)
	}
	css := string(raw)
	if hidden, named := hiddenRulesOf(css, "side-named"); named == 0 || len(hidden) == 0 {
		t.Fatalf("CONTROL: the branded name block has %d rules and %d hiding ones; the reader is not "+
			"reading the stylesheet", named, len(hidden))
	}
	hidden, named := hiddenRulesOf(css, "side-biz-name")
	if named < 3 {
		t.Errorf("%d compiled rules name the business's name, want its phone, rail and sidebar rules", named)
	}
	for _, r := range hidden {
		t.Errorf("a compiled rule takes the business's name off the screen: %s", r)
	}
}
