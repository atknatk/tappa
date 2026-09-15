package handler

import (
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/web"
)

// THE PASSWORD REVEAL BUTTON on /admin/login (2026-09-14, user request).
//
// WHAT THIS FILE IS FOR. The button itself is four lines of markup; what it cost is
// a Content-Security-Policy widening on a page that until now ran no script at all,
// and adminCSP's own comment says the panel names no script-src precisely so that
// "adding a script is a visible edit here rather than a silent inheritance". These
// tests are the other half of that bargain: they hold the SIZE of the widening (one
// directive, one URL) and the no-JavaScript contract (the button ships hidden, so a
// browser that does not run the file sees no control rather than a dead one).
//
// WHAT THEY DELIBERATELY DO NOT RE-ASSERT: anything adminlogin_test.go or
// adminloginsplit_test.go already holds about this screen — the byte-identical
// failure sentence, the form the handler parses, the absent magic-link button.

// shipsHidden reports whether an open tag carries the BARE hidden attribute.
//
// ⚠️ IT IS NOT `\bhidden\b`. That matched aria-hidden="true" — `-` is not a word
// character, so the boundary sits right before "hidden" — and every icon in the
// button carries one. Measured: the first version of this file reported both icons
// as hidden and the assertion that exactly one is fired. The pattern below requires
// whitespace before the word and a tag end or more whitespace after it.
var bareHidden = regexp.MustCompile(`(?is)\shidden(\s|/?>)`)

func shipsHidden(tag string) bool { return bareHidden.MatchString(tag) }

// pwToggleTag returns the open tag of the reveal button, and FAILS if the page does
// not carry exactly one. "Exactly one" is not fussiness: two buttons carrying
// data-pw-toggle would mean the script wires up the first and leaves the second
// dead, which is the failure this whole design exists to avoid.
func pwToggleTag(t *testing.T, body string) string {
	t.Helper()
	re := regexp.MustCompile(`(?is)<button\b[^>]*\bdata-pw-toggle\b[^>]*>`)
	found := re.FindAllString(body, -1)
	if len(found) != 1 {
		t.Fatalf("the sign-in page carries %d reveal button(s), want exactly 1", len(found))
	}
	return found[0]
}

// TestAdminLoginPassword_ToggleShipsHiddenSoNoScriptMeansNoButton is the
// no-JavaScript contract, asserted on the served bytes.
//
// 🔴 THE SERVER MUST SEND THE BUTTON ALREADY HIDDEN. The requirement was "it appears
// once the person starts typing", and the cheap reading of that — ship it visible,
// let a script wire it up — paints a control that does nothing when pressed if the
// script is blocked, fails to load, or is refused by a policy. On a sign-in screen
// that is the worst place in the product for a dead control: the person is already
// unsure whether they typed their password correctly. Hidden-by-default inverts the
// failure: no script, no button, and a form that behaves exactly as it did before.
//
// IT IS THE hidden ATTRIBUTE rather than a class, and web/static/css/input.css
// carries the rule that makes it bite — measured: Tailwind v3's preflight ships NO
// [hidden] rule at all, and it DOES give every svg display:block.
func TestAdminLoginPassword_ToggleShipsHiddenSoNoScriptMeansNoButton(t *testing.T) {
	_, body := loginPage(t)
	tag := pwToggleTag(t, body)

	if !shipsHidden(tag) {
		t.Errorf("the reveal button is served VISIBLE:\n    %s\nA browser that does not "+
			"run /static/js/adminpassword.js would then show a control that does "+
			"nothing when pressed.", tag)
	}
	// The stylesheet half. The attribute hides nothing on its own here, so the rule
	// that gives it teeth is part of the contract, not decoration.
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) — run `make css`. THIS IS NOT A PASS.", err)
	}
	css := string(raw)
	for _, want := range []string{".pw-toggle[hidden]", ".pw-toggle-icon[hidden]"} {
		if !strings.Contains(css, want) {
			t.Errorf("the compiled stylesheet has no %s rule, so the hidden attribute "+
				"does not hide: .pw-toggle sets display:inline-flex and preflight gives "+
				"every svg display:block, both of which outrank a bare [hidden] that "+
				"Tailwind v3 does not even emit", want)
		}
	}

	// AND THE 401 RE-RENDER, which is the same template through a different handler
	// path and is the render a person actually reaches after a typo.
	if !strings.Contains(refusedLoginPage(t), "data-pw-toggle") {
		t.Error("the refused sign-in page carries no reveal button; the two renders of " +
			"this screen have drifted")
	}
}

// TestAdminLoginPassword_ToggleIsARealButtonThatDoesNotSubmit.
//
// 🔴 type="button" IS ONE ATTRIBUTE BETWEEN A REVEAL AND A SIGN-IN ATTEMPT. A
// <button> inside a <form> with no type is a SUBMIT button: pressing "show password"
// would post whatever is in the fields, spend one of the sign-in attempts the rate
// limiter counts, and answer 401 for a password the person had not finished typing.
//
// IT IS ALSO WHY THE CONTROL IS A <button> AND NOT A <div> OR AN <a>. Keyboard
// reachability, Enter and Space, and the "button" role all come free from the
// element; every one of them would have to be re-implemented (and tested) on a div.
func TestAdminLoginPassword_ToggleIsARealButtonThatDoesNotSubmit(t *testing.T) {
	_, body := loginPage(t)
	tag := pwToggleTag(t, body)

	if !strings.Contains(tag, `type="button"`) {
		t.Errorf("the reveal button does not carry type=\"button\":\n    %s\nInside a "+
			"form that makes it a SUBMIT button, so revealing the password would "+
			"post the form.", tag)
	}
	// It must be inside the form it must not submit — a button outside the form
	// would pass the assertion above for the wrong reason.
	form := body[strings.Index(body, `<form method="post" action="/admin/login"`):]
	form = form[:strings.Index(form, "</form>")]
	if !strings.Contains(form, "data-pw-toggle") {
		t.Error("the reveal button is not inside the sign-in form; it is supposed to sit " +
			"inside the password field")
	}
	// ANTI-VACUITY: the form still has exactly one submit button, so this test is not
	// passing because the form lost its buttons.
	if n := strings.Count(form, `type="submit"`); n != 1 {
		t.Fatalf("the sign-in form carries %d submit button(s), want 1", n)
	}
	// And no other element pretends to be the control.
	for _, wrong := range []string{`<div data-pw-toggle`, `<a data-pw-toggle`} {
		if strings.Contains(body, wrong) {
			t.Errorf("the reveal control is rendered as %s; it must be a real <button>", wrong)
		}
	}
}

// TestAdminLoginPassword_ToggleNamesItselfAndItsState.
//
// THE NAME SAYS WHAT THE NEXT PRESS DOES and aria-pressed says where the toggle is
// now. Both, because either alone is ambiguous: "Show password, pressed" does not
// say whether the password is currently showing, and a label with no pressed state
// gives a screen reader nothing to announce when it changes.
//
// THE ICONS ARE aria-hidden because the button's own name already carries the
// meaning; an SVG that announced itself would make the control say everything twice.
// Both are in the markup and the script swaps which one is hidden, so there is no
// icon geometry in JavaScript.
//
// 44x44 IS THE HIT AREA. .pw-toggle is in dashboard_test.go's panelTouchTargets, so
// TestPanelScreens_EveryPressTargetCarriesATouchTargetClass accepts this button and
// TestPanelScreens_TouchTargetClassesReserve44px demands the height really exists in
// the compiled stylesheet.
func TestAdminLoginPassword_ToggleNamesItselfAndItsState(t *testing.T) {
	_, body := loginPage(t)
	tag := pwToggleTag(t, body)

	for _, want := range []string{
		`aria-label="Show password"`,
		`aria-pressed="false"`,
		`aria-controls="admin-password"`,
		`class="pw-toggle"`,
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("the reveal button is missing %s:\n    %s", want, tag)
		}
	}
	if _, ok := panelTouchTargets["pw-toggle"]; !ok {
		t.Error("pw-toggle is not in panelTouchTargets, so nothing holds this control to " +
			"the 44px floor the rest of the panel's press targets are held to")
	}

	// The two icons: one shown, one hidden, both silent, both drawn in the product's
	// own hand (currentColor at stroke-width 1.6).
	icons := regexp.MustCompile(`(?is)<svg\b[^>]*\bdata-pw-icon="([a-z]+)"[^>]*>`).FindAllStringSubmatch(body, -1)
	if len(icons) != 2 {
		t.Fatalf("found %d reveal icons, want 2 (one for each state)", len(icons))
	}
	hidden := 0
	for _, m := range icons {
		for _, want := range []string{`stroke="currentColor"`, `stroke-width="1.6"`, `aria-hidden="true"`} {
			if !strings.Contains(m[0], want) {
				t.Errorf("the %q icon is missing %s:\n    %s", m[1], want, m[0])
			}
		}
		if shipsHidden(m[0]) {
			hidden++
		}
	}
	if hidden != 1 {
		t.Errorf("%d of the 2 icons ship hidden, want exactly 1 — the button would "+
			"otherwise render both glyphs or none", hidden)
	}
}

// TestAdminLoginPassword_FieldKeepsEveryAttributeAPasswordManagerReads.
//
// 🔴 THE FIELD IS WHAT A PASSWORD MANAGER MATCHES ON. name, id,
// autocomplete="current-password" and type="password" are how a browser's own store
// and every third-party manager decide this is the field they saved a secret for. A
// redesign that wraps the input in a positioning div is exactly the change that
// quietly drops one of them, and the symptom — "my manager stopped filling this in"
// — reaches a support inbox rather than a test.
//
// required AND THE PLACEHOLDER ARE HELD FOR A DIFFERENT REASON: they are what the
// user asked to keep untouched, and a test is the only thing that keeps "untouched"
// true through the next edit.
func TestAdminLoginPassword_FieldKeepsEveryAttributeAPasswordManagerReads(t *testing.T) {
	_, body := loginPage(t)

	open := regexp.MustCompile(`(?is)<input\b[^>]*\bid="admin-password"[^>]*>`).FindString(body)
	if open == "" {
		t.Fatal("no input carries id=\"admin-password\"; the label's `for` now points at " +
			"nothing and the script has nothing to find")
	}
	for _, want := range []string{
		`id="admin-password"`,
		`name="password"`,
		`type="password"`,
		`autocomplete="current-password"`,
		`required`,
		`placeholder="••••••••"`,
	} {
		if !strings.Contains(open, want) {
			t.Errorf("the password field no longer carries %s:\n    %s", want, open)
		}
	}
	// THE SERVED TYPE IS password AND STAYS THAT WAY. The script swaps it in the
	// browser; a page that shipped type="text" would show the secret to anyone
	// looking at the screen before a key was pressed.
	if strings.Contains(open, `type="text"`) {
		t.Errorf("the password field is served as type=\"text\":\n    %s", open)
	}
}

// TestAdminLoginPassword_OnlyTheSignInPageWidensIt is the tripwire, and it is the
// reason this task counts as a security change rather than a UI one.
//
// 🔴 THE PANEL'S POLICY NAMES NO script-src ON PURPOSE. adminCSP's comment: "dropping
// the directive means adding a script is a visible edit here rather than a silent
// inheritance." /admin/login is now that visible edit — and the whole value of
// making it visible is lost if the widening then leaks to the screens that did not
// ask for it. So this measures, ON THE WIRE, four surfaces at once:
//
//	/admin/login          GET and the 401 re-render  script-src 'self'   ✅
//	/admin/login/choose   the business picker        no script-src       ✅
//	/admin/reset          password recovery          no script-src       ✅
//	/admin/employees      a panel section            no script-src       ✅
//	/admin/reports        a panel section            no script-src       ✅
//
// AND IT HOLDS THE SHAPE OF THE WIDENING, not just its presence: no 'unsafe-inline'
// (which is what makes a reflected string still unable to become code here), no
// 'unsafe-eval', and no connect-src — this script opens no connection, and a
// directive granted for something that does not happen is how the next one gets
// inherited instead of argued for.
func TestAdminLoginPassword_OnlyTheSignInPageWidensIt(t *testing.T) {
	const widening = "script-src 'self'"

	t.Run("the sign-in page widens, on both renders", func(t *testing.T) {
		b := newBrowser(t, newAdminRouter(t, &fakeAdmins{}, &fakeTrail{}))
		rec := b.do(http.MethodGet, adminLoginPath, nil)
		csp := rec.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, widening) {
			t.Fatalf("GET %s sends %q, which does not permit the reveal script — the "+
				"page would load nothing and log a policy violation", adminLoginPath, csp)
		}
		for _, never := range []string{"unsafe-inline", "unsafe-eval", "connect-src"} {
			if strings.Contains(csp, never) {
				t.Errorf("GET %s widened by more than one directive: %q names %s",
					adminLoginPath, csp, never)
			}
		}
		// The six directives adminCSP already had are all still there: the widening
		// is an addition, not a replacement.
		for _, want := range []string{
			"default-src 'none'", "style-src 'self'", "font-src 'self'",
			"form-action 'self'", "base-uri 'none'", "frame-ancestors 'none'",
		} {
			if !strings.Contains(csp, want) {
				t.Errorf("GET %s lost %q while gaining a script directive: %q",
					adminLoginPath, want, csp)
			}
		}

		// THE 401. It renders the same template through renderLoginFailure, so a
		// policy chosen per call site rather than per page would ship a refused
		// sign-in whose script the browser blocks.
		admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
			return adminauth.Authentication{}, adminauth.ErrBadCredentials
		}}
		fb := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
		csrf := csrfFrom(t, htmlOf(t, fb.do(http.MethodGet, adminLoginPath, nil)))
		fail := fb.do(http.MethodPost, adminLoginPath, url.Values{
			"csrf": {csrf}, "email": {"someone@example.test"}, "password": {"wrong"},
		})
		if fail.Code != http.StatusUnauthorized {
			t.Fatalf("the refused sign-in answered %d, want 401", fail.Code)
		}
		if got := fail.Header().Get("Content-Security-Policy"); !strings.Contains(got, widening) {
			t.Errorf("the 401 re-render sends %q, which refuses the script the page "+
				"loads — the reveal button would be dead exactly where it is most "+
				"wanted", got)
		}
	})

	t.Run("the business picker does not", func(t *testing.T) {
		a := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
		bz := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
		admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
			return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
				{AdminUserID: a.AdminUserID, TenantID: a.TenantID, PasswordMatched: true, Active: true},
				{AdminUserID: bz.AdminUserID, TenantID: bz.TenantID, PasswordMatched: true, Active: true},
			}}, nil
		}}
		b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminLoginPath, nil)))
		if rec := b.do(http.MethodPost, adminLoginPath, url.Values{
			"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
		}); rec.Code != http.StatusSeeOther {
			t.Fatalf("two verified businesses answered %d, want 303 to the picker", rec.Code)
		}
		rec := b.do(http.MethodGet, "/admin/login/choose", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /admin/login/choose = %d, want 200 — this tripwire would "+
				"otherwise be measuring an error page", rec.Code)
		}
		assertNoScriptWidening(t, "/admin/login/choose", rec.Header().Get("Content-Security-Policy"),
			htmlOf(t, rec))
	})

	t.Run("password recovery does not", func(t *testing.T) {
		router, _ := newResetRouter(t, &fakeResets{}, nil, &fakeTrail{})
		b := newBrowser(t, router)
		rec := b.do(http.MethodGet, adminResetPath, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", adminResetPath, rec.Code)
		}
		assertNoScriptWidening(t, adminResetPath, rec.Header().Get("Content-Security-Policy"),
			htmlOf(t, rec))
	})

	// ⚠️ THE PANEL SECTION CHECKED HERE IS NOT /admin, AND THE REASON IS WORTH
	// WRITING DOWN: /admin IS the transactions section (pages.PanelSections), which
	// is the ONE panel screen that legitimately sends script-src — it pages with
	// vendored htmx under adminScriptedCSP. Pointing this tripwire at it would have
	// failed for the right-looking wrong reason. The sections that must stay
	// unwidened are the other nine; two of them are sampled, and
	// TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty holds all of them
	// plus the cardinality.
	t.Run("the unscripted panel sections do not", func(t *testing.T) {
		b := panelBrowser(t)
		for _, href := range []string{"/admin/employees", "/admin/reports"} {
			rec := b.do(http.MethodGet, href, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", href, rec.Code)
			}
			assertNoScriptWidening(t, href, rec.Header().Get("Content-Security-Policy"),
				htmlOf(t, rec))
		}
	})
}

// assertNoScriptWidening holds both halves of "this page did not widen": the policy
// names no script-src, AND the page loads no script. Either alone is a half-truth —
// a page with a script and no directive is broken in the browser and green here.
func assertNoScriptWidening(t *testing.T, where, csp, body string) {
	t.Helper()
	if csp == "" {
		t.Fatalf("GET %s answers with no Content-Security-Policy at all", where)
	}
	if strings.Contains(csp, "script-src") {
		t.Errorf("GET %s sends a policy naming script-src (%q). The widening belongs to "+
			"/admin/login, which loads one file; a directive that spreads to the "+
			"screens that load nothing is the silent inheritance adminCSP's comment "+
			"exists to prevent.", where, csp)
	}
	if strings.Contains(strings.ToLower(body), "<script") {
		t.Errorf("GET %s loads a script while sending a policy that forbids one", where)
	}
}

// TestAdminLoginPassword_ScriptIsAnEmbeddedFileCalledByARelativePath.
//
// 🔴 A FILE, NOT AN INLINE TAG, AND NOT A CDN. Inline script would have needed
// 'unsafe-inline' in the policy — which is the one keyword that would make a
// reflected string on this page executable — and a CDN would send the sign-in page's
// URL to a third party on every load. Both are refused here by measurement: the tag
// must carry a src, the src must be relative and under /static, and the file must be
// in the tree web/embed.go embeds (so the deployed binary really has it; a path that
// resolves on a developer's disk and 404s in production is the failure this catches).
func TestAdminLoginPassword_ScriptIsAnEmbeddedFileCalledByARelativePath(t *testing.T) {
	_, body := loginPage(t)

	tags := regexp.MustCompile(`(?is)<script\b([^>]*)>`).FindAllStringSubmatch(body, -1)
	if len(tags) != 1 {
		t.Fatalf("the sign-in page carries %d script tag(s), want exactly 1", len(tags))
	}
	src := ""
	if m := attrValueRE("src").FindStringSubmatch(tags[0][1]); len(m) == 2 {
		src = m[1]
	}
	if src == "" {
		t.Fatalf("the script tag carries no src, so it is an INLINE script and the page "+
			"would need 'unsafe-inline':\n    <script%s>", tags[0][1])
	}
	if strings.Contains(src, "://") || strings.HasPrefix(src, "//") {
		t.Fatalf("the sign-in page loads its script from %q. An absolute URL sends this "+
			"page's address to another host on every load.", src)
	}
	if src != "/static/js/adminpassword.js" {
		t.Fatalf("the script src is %q, want /static/js/adminpassword.js", src)
	}

	raw, err := fs.ReadFile(web.Static(), strings.TrimPrefix(src, "/static/"))
	if err != nil {
		t.Fatalf("%s is not in the embedded asset tree (%v), so the deployed binary "+
			"answers 404 for the one file this page needs", src, err)
	}
	// ANTI-VACUITY: it is the reveal script and not an empty file.
	for _, want := range []string{"admin-password", "data-pw-toggle", "aria-pressed"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the embedded script does not mention %q; it is not the file this "+
				"page's markup was written for", want)
		}
	}
	// 🔴 IT NEVER ASSIGNS THE .hidden PROPERTY, AND THIS ASSERTION IS A SCAR.
	//
	// The first version of the script wrote `svg.hidden = true` to swap the two
	// glyphs. `hidden` is an IDL attribute of HTMLElement; an <svg> is an SVGElement
	// and does not inherit it, so that line created a plain JavaScript property,
	// reflected NOTHING into the DOM, and matched no [hidden] rule — the eye never
	// changed. Every cheap check passed: reading el.hidden back returns the expando
	// that was just written, in both states. It was caught by a SCREENSHOT plus a
	// getComputedStyle read, and the fix is that the script writes the ATTRIBUTE
	// (setAttribute / removeAttribute), which is what the stylesheet reads.
	//
	// THIS IS A SOURCE SCAN, WHICH IS THE HONEST SHAPE FOR IT: no test in this
	// package executes JavaScript, so the property-versus-attribute distinction is
	// invisible to every other net here. It is narrow on purpose — it forbids one
	// spelling, the exact one that failed. COMMENT LINES ARE STRIPPED FIRST, because
	// the file explains the defect in prose and a scan that read its own explanation
	// as the defect would be unfixable.
	var code []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			code = append(code, line)
		}
	}
	if regexp.MustCompile(`\.hidden\s*=[^=]`).MatchString(strings.Join(code, "\n")) {
		t.Error("the reveal script assigns the .hidden PROPERTY. On the <svg> icons " +
			"that silently does nothing (SVGElement has no `hidden` IDL attribute) " +
			"and the glyph never changes, while a test that reads the property back " +
			"stays green. Write the attribute instead: setAttribute('hidden','') / " +
			"removeAttribute('hidden').")
	}

	// 🔴 AND IT REACHES NOTHING. The policy grants script-src and nothing else, so a
	// script that tried to fetch would be blocked at runtime — but the honest place
	// to say "this makes no request" is here, where a future edit would show up.
	for _, never := range []string{"fetch(", "XMLHttpRequest", "localStorage", "sessionStorage", "document.cookie", "geolocation"} {
		if strings.Contains(string(raw), never) {
			t.Errorf("the reveal script mentions %s. It is permitted to touch one "+
				"input's type and one button's visibility; anything else needs a "+
				"policy directive this page does not send.", never)
		}
	}
}

// refusedLoginPage drives a real failed sign-in and returns the 401 body.
func refusedLoginPage(t *testing.T) string {
	t.Helper()
	admins := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{}, adminauth.ErrBadCredentials
	}}
	b := newBrowser(t, newAdminRouter(t, admins, &fakeTrail{}))
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminLoginPath, nil)))
	rec := b.do(http.MethodPost, adminLoginPath, url.Values{
		"csrf": {csrf}, "email": {"someone@example.test"}, "password": {"wrong"},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("the refused sign-in answered %d, want 401", rec.Code)
	}
	return htmlOf(t, rec)
}
