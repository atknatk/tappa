package handler

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The operator company's credit on the PUBLIC surface (user request 2026-10-23): a
// "Built by" footer link to layout.CreditURL and, in the head, <meta name="author"> and
// <link rel="author">. Only the marketing shells carry it; the employee's tap family,
// the panel, the sign-in family and the operator console never do.

// creditAnchorRE is the footer's anchor: the constant's href, a rel, and the name.
var creditAnchorRE = regexp.MustCompile(`<a href="` + regexp.QuoteMeta(layout.CreditURL) + `" rel="([^"]*)"[^>]*>` + regexp.QuoteMeta(layout.CreditName) + `</a>`)

// TestMarketingCredit_FooterLinkAndHeadTags: every page of the public surface (the
// landing page with its own footer, the legal pages and the guide with the shared
// chrome's, and the sign-up wizard) carries the footer link once and the two head tags.
// The link is followable: no nofollow, sponsored or ugc.
func TestMarketingCredit_FooterLinkAndHeadTags(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, url := range append(publicSurfaceURLs(), signupPath) {
		body := mustFetchMarketing(t, r, url)
		head := between(body, "<head>", "</head>")
		for _, want := range []string{
			`<meta name="author" content="` + layout.CreditName + `">`,
			`<link rel="author" href="` + layout.CreditURL + `">`,
		} {
			if strings.Count(head, want) != 1 {
				t.Errorf("GET %s: the head carries %q %d time(s), want once", url, want, strings.Count(head, want))
			}
		}
		footer := between(body, "<footer", "</footer>")
		ms := creditAnchorRE.FindAllStringSubmatch(footer, -1)
		if len(ms) != 1 {
			t.Errorf("GET %s: the footer carries the \"Built by\" link %d time(s), want once", url, len(ms))
			continue
		}
		if !strings.Contains(footer, "Built by") {
			t.Errorf("GET %s: the footer links the company without saying \"Built by\"", url)
		}
		for _, rel := range strings.Fields(ms[0][1]) {
			switch rel {
			case "nofollow", "sponsored", "ugc":
				t.Errorf("GET %s: the credit link carries rel=%q; it is the operator's own company "+
					"and must stay a followable link", url, rel)
			}
		}
	}
}

// TestMarketingCredit_NotOnTheTapScreenOrAnyOtherShell renders the employee's tap screen
// and every non-marketing layout shell and finds no trace of the credit. The operator
// console's 49 renders are checked in internal/handler/operator
// (TestOperatorScreens_CarryNoMarketingCredit). POSITIVE CONTROL: the marketing shells
// rendered the same way DO carry it, so the scan is looking at the right bytes.
func TestMarketingCredit_NotOnTheTapScreenOrAnyOtherShell(t *testing.T) {
	t.Parallel()
	body := templ.Raw(`<p>body</p>`)
	render := func(c templ.Component, children templ.Component) string {
		t.Helper()
		ctx := context.Background()
		if children != nil {
			ctx = templ.WithChildren(ctx, children)
		}
		var b bytes.Buffer
		if err := c.Render(ctx, &b); err != nil {
			t.Fatalf("render: %v", err)
		}
		return b.String()
	}
	never := map[string]string{
		"tap screen":                render(pages.Tap(pages.TapView{EmployeeName: "Maria", LocationName: "Valletta"}, layout.Brand{}), nil),
		"layout.Page":               render(layout.Page("T"), body),
		"layout.PageWithScript":     render(layout.PageWithScript("T", "/static/js/tap.js", layout.Brand{}), body),
		"layout.ActivationPage":     render(layout.ActivationPage("T", layout.Brand{}), body),
		"layout.BrandedPage":        render(layout.BrandedPage("T", layout.Logo{}), body),
		"layout.Panel":              render(layout.Panel("T", layout.Theme{}), body),
		"layout.PanelWithScript":    render(layout.PanelWithScript("T", "/static/vendor/htmx.min.js", layout.Theme{}), body),
		"layout.Auth":               render(layout.Auth("T", ""), body),
		"layout.Operator":           render(layout.Operator("T"), body),
		"layout.OperatorWithScript": render(layout.OperatorWithScript("T", "/static/js/operator/enroll.js"), body),
	}
	for name, html := range never {
		for _, s := range []string{layout.CreditURL, layout.CreditName, `rel="author"`, `name="author"`} {
			if strings.Contains(html, s) {
				t.Errorf("%s carries %q; the credit belongs to the public surface only", name, s)
			}
		}
	}
	for name, html := range map[string]string{
		"layout.Marketing":           render(layout.Marketing("T", layout.RobotsPublic, layout.Share{}), body),
		"layout.MarketingWithScript": render(layout.MarketingWithScript("T", layout.RobotsPublic, "/static/js/landing.js", layout.Share{}), body),
		"layout.MarketingTranslated": render(layout.MarketingTranslated("T", layout.RobotsPublic, nil, layout.Share{}), body),
	} {
		if !strings.Contains(html, `<link rel="author" href="`+layout.CreditURL+`">`) {
			t.Errorf("POSITIVE CONTROL: %s does not carry the credit, so the scan above is not reading the head", name)
		}
	}
}
