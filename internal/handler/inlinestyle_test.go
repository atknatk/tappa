package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// inlineStyle is an inline style in templ source: a style attribute in any of the shapes templ
// accepts -- `style="…"`, `style='…'`, `style={ expr }`, the conditional `style?={ cond }` -- or a
// <style> element. The value has to open with a quote or a brace, so prose that NAMES the
// attribute ("the reference's six inline style= attributes" in landing.templ) is not a hit.
var inlineStyle = regexp.MustCompile(`(?i)\bstyle\s*\??=\s*["'{]|<style[\s>/]`)

// cssComponent is a templ `css name(…) { … }` declaration. It carries no style attribute and no
// <style> in the source, yet a class={ name() } that uses it makes templ render a
// <style type="text/css"> block into the page (templ.ComponentCSSClass, through
// templ.RenderCSSItems) -- which style-src 'self' drops like any other inline style.
var cssComponent = regexp.MustCompile(`^\s*css\s+\w+\s*\(`)

// styleKey is a "style" key in a Go map literal -- templ.Attributes{"style": …} -- which, spread
// into an element with { attrs... }, is a style attribute no markup scan sees. A .templ file holds
// Go functions too (pages/transactions.templ's chipCurrent returns templ.Attributes), so it is read
// there as well as in the .go files.
var styleKey = regexp.MustCompile("[\"`]style[\"`]\\s*:")

// inlineStyleExempt are the ONLY .templ files allowed inline styles, by path under web/templates,
// each with its reason. A new file is scanned until it is listed here with one.
var inlineStyleExempt = map[string]string{
	// The HTML part of every e-mail (email.go). It is never served by this product's HTTP server,
	// so no Content-Security-Policy applies to it, and it can load no stylesheet: email.go's own
	// rule is "nothing remote" (no <link>, no <style>, no url(), measured by
	// TestRender_LoadsNothingAndUsesOnlyTheseElements, which also lists `style` among its only
	// attributes). An inline style is the only styling a mail client keeps, and email_test.go
	// reads each text node's colour and ground FROM those inline styles to measure contrast.
	"email/message.templ": "e-mail body: no CSP, no stylesheet to load; inline style is its only styling",
}

// TestTemplates_CarryNoInlineStyle is the product-wide net for CSP `style-src 'self'`: every
// page this server renders -- the panel, the operator console, the tap, result and activation
// screens, the public site -- is sent under a policy whose style-src is 'self' with no
// 'unsafe-inline', so the browser DROPS an inline style attribute or <style> block. Such markup is
// a silent defect: the page renders without the rule, and no test that reads the body for words
// notices. (Measured before this net: `style="color:#c00"` on the billing month's <h2> left every
// test green.)
//
// THREE SHAPES REACH THE PAGE: a style attribute, a <style> element, and a templ css component --
// `css name() { … }` used as class={ name() } -- which has neither in its source and still renders
// a <style> block (measured: such a component in notice.templ left the first version of this net
// green). The component is read in the .templ source and as templ.ComponentCSSClass in every .go
// file, generated ones included.
//
// It reads the .templ SOURCES rather than rendered pages, on the TestMarketing_ReachesNoThirdParty
// precedent: a screen no corpus renders is still covered.
//
// THE PREMISE IS PINNED TOO: if a policy ever admitted inline style, this net would be guarding a
// rule that no longer exists, so every policy constant in this package is required to say
// "style-src 'self'" and none may say 'unsafe-inline'. The operator console's policy lives in
// internal/handler/operator and is read from its source.
//
// ANTI-VACUITY: it fails unless it scanned the files named in mustScan (panel, operator, tap and
// public pages), unless every exempt file exists and really holds an inline style (so the regex
// sees the shape real templ writes, and an exemption cannot outlive its reason), and unless the
// regex matches each shape templ accepts.
func TestTemplates_CarryNoInlineStyle(t *testing.T) {
	t.Parallel()

	for _, shape := range []string{
		`<h2 style="color:#c00">`, `<p style='margin:0'>`, `<div style={ s }>`,
		`<div style?={ cond }>`, `<div STYLE = "x">`, `<style>`, `<style media="x">`,
	} {
		if !inlineStyle.MatchString(shape) {
			t.Errorf("the scan does not see %q -- an inline style shape templ accepts", shape)
		}
	}
	for _, prose := range []string{"the six inline style= attributes", "styles", "line-style values"} {
		if inlineStyle.MatchString(prose) {
			t.Errorf("the scan reads prose %q as an inline style", prose)
		}
	}
	for _, shape := range []string{"css mutRed() {", "\tcss tone(c string) {", "css  wide ( ) {"} {
		if !cssComponent.MatchString(shape) {
			t.Errorf("the scan does not see %q -- a templ css component, which renders a <style> block", shape)
		}
	}
	for _, shape := range []string{`templ.Attributes{"aria-current": "true", "style": "color:#c00"}`, "{`style`: s}", `"style" : x`} {
		if !styleKey.MatchString(shape) {
			t.Errorf("the scan does not see %q -- a style key in an attribute map", shape)
		}
	}
	for _, prose := range []string{`"aria-current": "true"`, `class="line-style"`, `"styles": x`} {
		if styleKey.MatchString(prose) {
			t.Errorf("the scan reads %q as a style key", prose)
		}
	}
	for _, prose := range []string{"// the css class above", "\tclass={ css }", "panelcss red() {", "@css.Card()"} {
		if cssComponent.MatchString(prose) {
			t.Errorf("the scan reads %q as a templ css component", prose)
		}
	}

	constCSPs := map[string]string{
		"marketingCSP": marketingCSP, "landingCSP": landingCSP, "signupCSP": signupCSP,
		"adminCSP": adminCSP, "adminScriptedCSP": adminScriptedCSP, "adminLoginCSP": adminLoginCSP,
		"tapCSP": tapCSP, "activationCSP": activationCSP,
	}
	policies := map[string]string{
		// The policies the pages are really sent under, computed from the constants above.
		"tapCSPFor(false)": tapCSPFor(false), "tapCSPFor(true)": tapCSPFor(true),
		"activationCSPFor(false)": activationCSPFor(false), "activationCSPFor(true)": activationCSPFor(true),
		`landingCSPFor("", "")`: landingCSPFor("", ""), `landingCSPFor(video, poster)`: landingCSPFor("v.mp4", "p.jpg"),
	}
	for name, csp := range constCSPs {
		policies[name] = csp
	}
	for name, csp := range policies {
		if !strings.Contains(csp, "style-src 'self'") || strings.Contains(csp, "unsafe-inline") {
			t.Errorf("%s = %q: the inline-style net assumes style-src 'self' without 'unsafe-inline'", name, csp)
		}
	}
	operatorSrc := repoFile(t, "internal", "handler", "operator", "render.go")
	if !strings.Contains(operatorSrc, `const operatorCSP = "default-src 'none'; style-src 'self';`) || strings.Contains(operatorSrc, `'unsafe-inline';`) {
		t.Error("internal/handler/operator/render.go: operatorCSP no longer reads style-src 'self' -- the inline-style net assumes it")
	}
	// THE LIST ABOVE IS HAND-KEPT, so it is checked against the sources: every *CSP constant
	// declared in this package must be in constCSPs, and the operator package must declare only
	// operatorCSP -- a new policy cannot be added without this net reading it.
	cspDecl := regexp.MustCompile(`(?m)^(?:const\s+|\t)(\w+CSP)\s*=[^=]`)
	declared := func(dir ...string) []string {
		files, err := filepath.Glob(filepath.Join(append(append([]string{"..", ".."}, dir...), "*.go")...))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range cspDecl.FindAllStringSubmatch(string(raw), -1) {
				names = append(names, m[1])
			}
		}
		sort.Strings(names)
		return names
	}
	var listed []string
	for name := range constCSPs {
		listed = append(listed, name)
	}
	sort.Strings(listed)
	if got := declared("internal", "handler"); strings.Join(got, ",") != strings.Join(listed, ",") {
		t.Errorf("internal/handler declares the policies %v, the inline-style net reads %v -- list every *CSP constant", got, listed)
	}
	if got := declared("internal", "handler", "operator"); strings.Join(got, ",") != "operatorCSP" {
		t.Errorf("internal/handler/operator declares the policies %v, the inline-style net reads only operatorCSP", got)
	}

	root := filepath.Join("..", "..", "web", "templates")
	scanned := map[string]bool{}
	exemptHits := map[string]int{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		raw, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		src := string(raw)
		switch {
		case strings.HasSuffix(rel, ".templ"):
			if _, ok := inlineStyleExempt[rel]; ok {
				exemptHits[rel] = len(inlineStyle.FindAllString(src, -1))
				return nil
			}
			scanned[rel] = true
			for i, line := range strings.Split(src, "\n") {
				if hit := inlineStyle.FindString(line); hit != "" {
					t.Errorf("%s:%d carries an inline style (%q).\n"+
						"Every page is served under style-src 'self', so the browser drops it and the "+
						"page renders without the rule. Write the Tailwind utility literally in the "+
						".templ, or a class in input.css's @layer components.", rel, i+1, hit)
				}
				if hit := styleKey.FindString(line); hit != "" {
					t.Errorf("%s:%d puts a style key in an attribute map (%q): spread into an element it is "+
						"an inline style, and style-src 'self' drops it", rel, i+1, strings.TrimSpace(line))
				}
				if cssComponent.MatchString(line) {
					t.Errorf("%s:%d declares a templ css component (%q): a class={ … } that uses it renders "+
						"a <style> block into the page, which style-src 'self' drops. Use a Tailwind utility "+
						"or a class in input.css's @layer components.", rel, i+1, strings.TrimSpace(line))
				}
			}
		case strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go"):
			scanned[rel] = true
			// templ.ComponentCSSClass is what a css component compiles to, and what hand-written Go
			// could return to a class={ … } to the same effect: RenderCSSItems writes its <style>
			// block. Read in the GENERATED files too, so the compiled output is checked as well as
			// the source the .templ scan reads.
			if strings.Contains(src, "ComponentCSSClass") {
				t.Errorf("%s builds a templ.ComponentCSSClass: rendered through class={ … } it writes a "+
					"<style> block into the page, which style-src 'self' drops", rel)
			}
			if strings.HasSuffix(rel, "_templ.go") {
				// The compiled form of a .templ's Go functions: the same map literal, read again.
				if hit := styleKey.FindString(src); hit != "" {
					t.Errorf("%s puts a style key in an attribute map (%q): spread into an element it is "+
						"an inline style, and style-src 'self' drops it", rel, hit)
				}
				return nil
			}
			// A view type can hand an element its attributes (templ.Attributes spread with
			// `{ attrs... }`), which the .templ scan cannot see. The generated _templ.go files are
			// the .templ files above, compiled.
			if strings.Contains(src, `"style"`) {
				t.Errorf("%s names a \"style\" attribute: an attribute map spread into an element is "+
					"an inline style the .templ scan cannot see, and style-src 'self' drops it", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the templates: %v", err)
	}

	for _, want := range []string{
		"pages/billing.templ", "pages/transactions.templ", "pages/manualentry.templ", "pages/review.templ",
		"components/docket.templ", "components/venue.templ", "operatorpages/chrome.templ",
		"pages/tap.templ", "pages/activate.templ", "pages/landing.templ", "pages/view.go",
		"pages/billing_templ.go", "components/notice_templ.go",
	} {
		if !scanned[want] {
			t.Errorf("the inline-style scan did not read %s", want)
		}
	}
	templs := 0
	for f := range scanned {
		if strings.HasSuffix(f, ".templ") {
			templs++
		}
	}
	if templs < 40 {
		t.Errorf("the inline-style scan read %d .templ files, want at least 40 -- is it walking the right tree?", templs)
	}
	var exempt []string
	for f := range inlineStyleExempt {
		exempt = append(exempt, f)
	}
	sort.Strings(exempt)
	for _, f := range exempt {
		if n, ok := exemptHits[f]; !ok || n == 0 {
			t.Errorf("%s is exempt from the inline-style scan but holds no inline style (found=%v, hits=%d): "+
				"drop the exemption", f, ok, n)
		}
	}
}
