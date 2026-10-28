package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// unboundPanelPost are the panel's POST routes that are NOT behind a writing chain, so
// their forms carry no tenant binding (ADR 0031 §5.5): the sign-in family, which runs
// before a session exists, and sign-out, which must never be refusable (adminlogin.go).
var unboundPanelPost = map[string]bool{
	adminLoginPath:        true,
	"/admin/login/choose": true,
	"/admin/logout":       true,
	adminResetPath:        true,
	adminResetNewPath:     true,
	// M14-15: the invitee's page -- reachable without a session of the business (the link from the
	// e-mail); its own Origin check and synchronizer cookie (admininvites.go).
	adminInvitePath: true,
}

// panelRouters maps a test router to the AdminAuth mounted on it, so the test browser can
// compute the binding the page it is "submitting" would have carried (M14-10a). A real
// browser submits the form it was shown; a test that posts without loading the page first
// stands for that browser, and must post what the page would have said. The gate under
// test is the production one; only the client side is simulated here.
var panelRouters sync.Map // http.Handler -> *AdminAuth

// registerPanel records that a is what r serves.
func registerPanel(r http.Handler, a *AdminAuth) {
	panelRouters.Store(r, a)
}

// inheritPanel registers wrapper as serving whatever panel inner serves, for a test that
// wraps a registered router in a handler of its own.
func inheritPanel(wrapper, inner http.Handler) {
	if inner == nil || !reflect.TypeOf(inner).Comparable() {
		return
	}
	if v, ok := panelRouters.Load(inner); ok {
		panelRouters.Store(wrapper, v)
	}
}

// boundPath is path with the binding a page rendered for cookie by the panel behind h
// would carry -- what a test that builds its own request sends where a browser would
// submit the form it was shown.
func boundPath(h http.Handler, cookie, path string) string {
	tb := bindingFor(h, cookie)
	if tb == "" {
		return path
	}
	return path + "?tb=" + tb
}

// bindingFor is the binding a page rendered for this cookie would carry, or "" when the
// handler is not a registered panel router or the cookie resolves to no tenant. The
// tenant comes from the fake's own answer without counting a resolver call, so a test
// that counts Verify calls counts the requests it sent and nothing else.
func bindingFor(h http.Handler, cookie string) string {
	if h == nil || cookie == "" || !reflect.TypeOf(h).Comparable() {
		return ""
	}
	v, ok := panelRouters.Load(h)
	if !ok {
		return ""
	}
	a := v.(*AdminAuth)
	req := httptest.NewRequest(http.MethodGet, adminauth.CookiePath, nil)
	req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: cookie})
	tok, err := a.cookies.Read(req)
	if err != nil {
		return ""
	}
	var res adminauth.Resolved
	switch f := a.admins.(type) {
	case *fakeAdmins:
		if f.verify == nil {
			return ""
		}
		res, err = f.verify()
	default:
		res, err = a.admins.Verify(context.Background(), tok)
	}
	if err != nil {
		return ""
	}
	b, err := a.forms.Bind(tok, res.TenantID)
	if err != nil {
		return ""
	}
	return b
}

// withoutBinding takes every form binding out of a page, the way the test browser's
// learnBinding does: the tests that read pages ask where forms post, and the binding's
// own presence is measured on the raw page by this file's tests.
func withoutBinding(page string) string {
	return boundFormBinding.ReplaceAllString(page, "")
}

// boundFormBinding matches the binding as a rendered action carries it: first in the
// query, or after another parameter (templ writes & as &amp; in an attribute).
var boundFormBinding = regexp.MustCompile(`(\?|&amp;|&)tb=[A-Za-z0-9_-]{43}`)

// --- M14-10a: the tests -----------------------------------------------------------------

// formBindingOwner is a live owner of panelTestTenant.
func formBindingOwner() *fakeAdmins {
	return &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: panelTestSession, TenantID: panelTestTenant,
			AdminUserID: panelTestAdmin, Role: "owner", FullName: "KF Owner"}, nil
	}}
}

// formBindingCookie is the session cookie the tests below present.
const formBindingCookie = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// formBindingAuth is the real AdminAuth over ordinary fakes, mounted, with two probe routes
// behind the two writing chains so a test can see what a handler behind them receives.
func formBindingAuth(t *testing.T, admins *fakeAdmins, probe http.HandlerFunc) (*AdminAuth, http.Handler) {
	t.Helper()
	return formBindingAuthUnder(t, false, admins, probe)
}

// formBindingAuthUnder is formBindingAuth under a panel identity flag (open: the identity
// surfaces' routes are mounted too).
func formBindingAuthUnder(t *testing.T, identity bool, admins *fakeAdmins, probe http.HandlerFunc) (*AdminAuth, http.Handler) {
	t.Helper()
	cfg := adminTestConfig()
	cfg.AdminIdentity = identity
	a, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(),
		newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	a.Mount(r)
	if probe != nil {
		r.With(a.ProtectWriting()).Post("/admin/test-probe", probe)
		r.With(a.protectWritingThroughSuspension()).Post("/admin/test-probe-open", probe)
	}
	registerPanel(r, a)
	return a, r
}

// bindingOf is a's binding for cookie in tenant -- what a page drawn for that pair carries.
func bindingOf(t *testing.T, a *AdminAuth, cookie string, tenant uuid.UUID) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, adminauth.CookiePath, nil)
	req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: cookie})
	tok, err := a.cookies.Read(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := a.forms.Bind(tok, tenant)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// panelPost is a signed-in, same-origin POST of body to target.
func panelPost(h http.Handler, target, cookie string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testBaseURL)
	req.RemoteAddr = "203.0.113.9:1234"
	req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: cookie})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// nonPanelPostActions are the POST forms in web/templates that are not panel writes: the
// sign-in family and sign-out (unboundPanelPost), the sign-up wizard, the activation
// wizard, the tap form, the employee's claim card, and the development tap strip.
var nonPanelPostActions = map[string]bool{
	`"/admin/login"`: true, `"/admin/login/choose"`: true, `"/admin/logout"`: true,
	`"/admin/reset"`: true, `"/admin/reset/new"`: true,
	`"/admin/invite"`: true, // M14-15: the invitee's acceptance (unboundPanelPost)
	`"/signup"`:       true, `"/signup/places"`: true, `"/signup/account"`: true,
	// M14-17: the registration link's confirmation -- it creates a business; no tenant to bind to yet.
	`{ templ.URL(SignupConfirmPath) }`: true,
	`"/activate"`:                      true, `"/api/activate"`: true, `"/api/checkin"`: true,
	`{ templ.SafeURL(DevSimulateTapPath) }`: true,
	// M15-15: the employee's claim card on the tap result -- the tap side's own form, no panel session.
	`"/api/claim"`: true,
}

// boundAction is how a bound form writes its action (components.Bound on the render ctx).
var boundAction = regexp.MustCompile(`^\{ templ\.URL\((components\.)?Bound\(ctx, [^{}]+\)\) \}$`)

// formAction lifts a form tag's action: a quoted literal or a { … } expression.
var formAction = regexp.MustCompile(`\baction=("[^"]*"|\{[^{}]*\})`)

// TestPanelForms_EveryPostFormCarriesTheTenantBinding is the template inventory (M14-10a):
// every POST form in the product's page and component templates either posts through
// components.Bound (the tenant binding) or is one of the named non-panel forms. A panel
// form added without the binding is red here before any browser could post it.
//
// The operator's templates are out of scope: they are another host with another identity
// (ADR 0020), and no panel route is reachable from them.
func TestPanelForms_EveryPostFormCarriesTheTenantBinding(t *testing.T) {
	bound, named := 0, 0
	for _, dir := range []string{"../../web/templates/pages", "../../web/templates/components", "../../web/templates/layout"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.templ"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			s := string(src)
			for i := strings.Index(s, "<form"); i >= 0; {
				end := strings.Index(s[i:], ">")
				if end < 0 {
					t.Fatalf("%s: a <form tag that does not close", f)
				}
				tag := s[i : i+end+1]
				line := strings.Count(s[:i], "\n") + 1
				if strings.Contains(strings.ToLower(tag), `method="post"`) {
					m := formAction.FindStringSubmatch(tag)
					switch {
					case m == nil:
						t.Errorf("%s:%d: a POST form without an action", filepath.Base(f), line)
					case boundAction.MatchString(m[1]):
						bound++
					case nonPanelPostActions[m[1]]:
						named++
					default:
						t.Errorf("%s:%d: a POST form whose action %s does not carry the tenant binding "+
							"(write it as templ.URL(components.Bound(ctx, …)) -- ADR 0031 §5.5)", filepath.Base(f), line, m[1])
					}
				}
				next := strings.Index(s[i+1:], "<form")
				if next < 0 {
					break
				}
				i += 1 + next
			}
		}
	}
	if bound < 40 {
		t.Fatalf("PREMISE: %d bound forms found; the panel has more than forty, so the scan is reading the wrong files", bound)
	}
	t.Logf("the panel's templates hold %d bound POST forms and %d named non-panel ones", bound, named)
}

// TestPanelForms_EveryRenderedPostFormIsBound renders every panel section, and the Account
// section with the brand editor open, for a signed-in owner and reads the RAW page: every
// POST form but sign-out posts with the binding of THIS session in THIS tenant, and the
// page sends no-referrer (S14).
func TestPanelForms_EveryRenderedPostFormIsBound(t *testing.T) {
	owner := formBindingOwner()
	router := newAdminRouter(t, owner, &fakeTrail{})
	brand := newBrandPanel(t, "owner", panelTestTenant)
	want := bindingFor(router, formBindingCookie)
	if want == "" || want != bindingFor(brand.router, formBindingCookie) {
		t.Fatalf("PREMISE: no binding for the owner (%q)", want)
	}
	forms := 0
	scan := func(path, body string) {
		// S14: a page whose forms carry a binding sends no Referer from it.
		if !strings.Contains(body, `<meta name="referrer" content="no-referrer">`) {
			t.Errorf("%s: the page does not send no-referrer", path)
		}
		for _, tag := range regexp.MustCompile(`<form[^>]*>`).FindAllString(body, -1) {
			if !strings.Contains(tag, `method="post"`) {
				continue
			}
			action := regexp.MustCompile(`action="([^"]*)"`).FindStringSubmatch(tag)
			if action == nil {
				t.Errorf("%s: a POST form without an action: %s", path, tag)
				continue
			}
			if action[1] == "/admin/logout" {
				continue
			}
			forms++
			if !strings.HasSuffix(action[1], "tb="+want) {
				t.Errorf("%s: the form posting to %q does not carry this session's binding", path, action[1])
			}
		}
	}
	browserFor := func(h http.Handler) *browser {
		b := newBrowser(t, h)
		b.raw = true
		b.cookies[adminauth.CookieName] = formBindingCookie
		return b
	}
	check := func(h http.Handler, path string) string {
		rec := browserFor(h).do(http.MethodGet, path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		scan(path, rec.Body.String())
		return rec.Body.String()
	}
	for _, s := range pages.PanelSections {
		check(router, s.Href)
	}
	check(brand.router, accountHref)
	// The claim queue (M15-18, brought in by the merge of main into M14) is no PanelSection: its
	// decision form is drawn only with a claim in focus, and its confirmation step only as the answer
	// to an out-of-band Approve. Both are read here, raw.
	claimed := inClaim(claimDay(0, 9, 0), claimDay(0, 5, 0))
	queue := claimPanel(t, formBindingOwner(), newFakeClaimQueue(time.UTC, claimed)).h
	if bindingFor(queue, formBindingCookie) != want {
		t.Fatal("PREMISE: the claim queue's panel binds this session differently")
	}
	beforeClaims := forms
	focus := check(queue, claimsHref+"?focus="+claimed.in.ClaimID.String())
	if !strings.Contains(focus, `name="id" value="`+claimed.in.ClaimID.String()+`"`) {
		t.Fatal("PREMISE: the claim queue drew no decision form for the claim in focus")
	}
	step := browserFor(queue).do(http.MethodPost, claimsHref, decisionForm(claimed.in.ClaimID, "approved"))
	if step.Code != http.StatusOK || !strings.Contains(step.Body.String(), `name="confirm_token"`) {
		t.Fatalf("PREMISE: an out-of-band Approve answered %d without the confirmation step", step.Code)
	}
	scan("the claim queue's confirmation step", step.Body.String())
	if n := forms - beforeClaims; n != 2 {
		t.Errorf("the claim queue's focus page and confirmation step drew %d POST form(s), want 2", n)
	}
	// The identity flag open: the Account section also draws M14-11's sign-out everywhere.
	before := forms
	_, identityOn := formBindingAuthUnder(t, true, formBindingOwner(), nil)
	if bindingFor(identityOn, formBindingCookie) != want {
		t.Fatal("PREMISE: the flag-on panel binds this session differently")
	}
	check(identityOn, accountHref)
	if forms == before {
		t.Error("the flag-on Account section drew no POST form")
	}
	if forms == 0 {
		t.Fatal("PREMISE: no POST form was rendered, so nothing was checked")
	}
	t.Logf("%d rendered POST forms checked", forms)
}

// TestFormBinding_EveryWritingRouteRefusesAPostWithoutItsBinding walks the router: every
// POST the panel mounts behind a writing chain answers 409 to a signed-in, same-origin
// POST that carries no binding, a binding of another business, or a binding of another
// session -- through a TRIPWIRE panel whose every store panics, so the refusal provably
// comes before anything is read or written. The named exceptions -- the sign-in family
// and sign-out (no writing chain), and the encode relay (formbinding.go) -- answer
// something else.
//
// BOTH STATES OF THE IDENTITY FLAG are walked: open, the identity surfaces' routes are mounted
// too (mountGated -- M14-11's sign-out everywhere among them), and they are writes like any other.
func TestFormBinding_EveryWritingRouteRefusesAPostWithoutItsBinding(t *testing.T) {
	routes := map[bool][]string{}
	for _, identity := range []bool{false, true} {
		t.Run(map[bool]string{false: "flag off", true: "flag on"}[identity], func(t *testing.T) {
			routes[identity] = everyWritingRouteRefuses(t, identity)
		})
	}
	if slices.Contains(routes[false], accountSignOutEverywhereHref) || !slices.Contains(routes[true], accountSignOutEverywhereHref) {
		t.Errorf("CONTROL FAILED: %s is bound-walked with the flag off %v and on %v; want only on",
			accountSignOutEverywhereHref, slices.Contains(routes[false], accountSignOutEverywhereHref),
			slices.Contains(routes[true], accountSignOutEverywhereHref))
	}
}

// everyWritingRouteRefuses is one flag state of the walk above; it returns the bound routes.
func everyWritingRouteRefuses(t *testing.T, identity bool) []string {
	t.Helper()
	trail := &fakeTrail{}
	owner := formBindingOwner()
	tripwire := tripwirePanelUnder(t, identity, owner, trail, discardLogger())
	_, open := formBindingAuth(t, formBindingOwner(), nil)
	a, _ := formBindingAuth(t, formBindingOwner(), nil)
	const otherCookie = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	cases := []struct{ name, query string }{
		{"no binding", ""},
		{"another business's binding", "?tb=" + bindingOf(t, a, formBindingCookie, uuid.New())},
		{"another session's binding", "?tb=" + bindingOf(t, a, otherCookie, panelTestTenant)},
		{"two bindings", "?tb=" + bindingOf(t, a, formBindingCookie, panelTestTenant) + "&tb=" + bindingOf(t, a, formBindingCookie, panelTestTenant)},
		{"a binding that is not one", "?tb=" + strings.Repeat("A", adminauth.FormBindingLen)},
	}
	bound := 0
	var boundRoutes []string
	for _, mr := range walkedRoutes(t, tripwire) {
		method, route, _ := strings.Cut(mr, " ")
		if method != http.MethodPost {
			continue
		}
		if unboundPanelPost[route] || encodeRelayRoutes[route] {
			rec, tripped := suspensionDrive(open, method, route)
			if tripped == nil && rec.Code == http.StatusConflict {
				t.Errorf("%s is a named exception and answered 409", mr)
			}
			continue
		}
		bound++
		boundRoutes = append(boundRoutes, route)
		for _, c := range cases {
			var rec *httptest.ResponseRecorder
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Errorf("%s with %s reached a store: %v", mr, c.name, p)
					}
				}()
				rec = panelPost(tripwire, route+c.query, formBindingCookie, strings.NewReader("name=x"))
			}()
			if rec != nil && rec.Code != http.StatusConflict {
				t.Errorf("%s with %s answered %d, want 409", mr, c.name, rec.Code)
			}
		}
	}
	if n := len(trail.eventsSnapshot()); n != 0 {
		t.Errorf("the refusals wrote %d audit row(s), want 0", n)
	}
	if bound < 35 {
		t.Fatalf("PREMISE: %d bound routes walked; the panel has 38 writes behind the two chains (measured, M14-10a)", bound)
	}
	// CONTROL: the right binding gets past the ring (the tripwire then trips or a
	// handler answers -- anything but the binding's 409).
	rec, tripped := func() (rec *httptest.ResponseRecorder, tripped any) {
		defer func() { tripped = recover() }()
		return panelPost(tripwire, venueSaveHref+"?tb="+bindingOf(t, a, formBindingCookie, panelTestTenant), formBindingCookie, strings.NewReader("name=x")), nil
	}()
	if tripped == nil && rec.Code == http.StatusConflict {
		t.Fatal("CONTROL: the session's own binding was refused, so the refusals above measure nothing")
	}
	t.Logf("%d bound POST routes each refused five ways", bound)
	return boundRoutes
}

// TestFormBinding_ARefusedPostIsNeverRead: a POST whose binding does not match is refused
// before its body is read, whatever its size -- including bodies over each handler's own
// http.MaxBytesReader limit, which stay where they are (billingactions.go, brandactions.go,
// accountactions.go, brandupload.go). The control sends the same oversized bodies with
// the right binding: the HANDLER reads them and refuses them itself.
func TestFormBinding_ARefusedPostIsNeverRead(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	right := bindingFor(p.router, formBindingCookie)
	wrong := bindingOf(t, p.h, formBindingCookie, uuid.New())
	for _, tc := range []struct {
		route string
		limit int
		ct    string
	}{
		{accountHref, maxAccountBody, "application/x-www-form-urlencoded"},
		{billingCloseHref, maxBillingBody, "application/x-www-form-urlencoded"},
		{brandAccentHref, brandFormMaxBody, "application/x-www-form-urlencoded"},
		{brandResetHref, brandFormMaxBody, "application/x-www-form-urlencoded"},
		{brandLogoHref, brandUploadMaxBody, "multipart/form-data; boundary=x"},
	} {
		send := func(tb string) (*httptest.ResponseRecorder, int) {
			body := &countingReader{r: strings.NewReader("a=" + strings.Repeat("b", 2*tc.limit))}
			req := httptest.NewRequest(http.MethodPost, tc.route+"?tb="+tb, body)
			req.Header.Set("Content-Type", tc.ct)
			req.Header.Set("Origin", testBaseURL)
			req.RemoteAddr = "203.0.113.9:1234"
			req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: formBindingCookie})
			rec := httptest.NewRecorder()
			p.router.ServeHTTP(rec, req)
			return rec, body.n
		}
		rec, n := send(wrong)
		if rec.Code != http.StatusConflict || n != 0 {
			t.Errorf("%s, a body of twice its %d-byte limit with another business's binding: %d after reading %d byte(s), want 409 after 0",
				tc.route, tc.limit, rec.Code, n)
		}
		rec, n = send(right)
		if rec.Code == http.StatusConflict {
			t.Errorf("CONTROL: %s refused its own binding", tc.route)
		}
		if tc.route != brandLogoHref && (n == 0 || n > tc.limit+64<<10) {
			t.Errorf("CONTROL: %s with its own binding read %d byte(s) of a %d-byte-limited body; the handler's own bound is not the one reading",
				tc.route, n, tc.limit)
		}
	}
	if p.writer.writes() != 0 || len(p.trail.eventsSnapshot()) != 0 {
		t.Errorf("%d brand write(s), %d trail row(s); want none", p.writer.writes(), len(p.trail.eventsSnapshot()))
	}
}

// TestFormBinding_TheHandlerSeesNoBindingAndNoNewTenant: what passes the ring is the same
// request minus its binding. The handler's tenant is AdminOf's (the session row's), the
// render context carries no binding the gate put there, the other query parameters are
// kept byte for byte, and a binding sent in the BODY is never consulted -- the ring reads
// r.URL and nothing else.
func TestFormBinding_TheHandlerSeesNoBindingAndNoNewTenant(t *testing.T) {
	var seen struct {
		raw, uri string
		tenant   uuid.UUID
		ctxBound string
		calls    int
	}
	probe := func(w http.ResponseWriter, r *http.Request) {
		seen.calls++
		seen.raw, seen.uri = r.URL.RawQuery, r.RequestURI
		seen.tenant = httpx.AdminOf(r).TenantID()
		seen.ctxBound = components.Bound(r.Context(), "/x")
		w.WriteHeader(http.StatusNoContent)
	}
	a, h := formBindingAuth(t, formBindingOwner(), probe)
	tb := bindingOf(t, a, formBindingCookie, panelTestTenant)
	for _, route := range []string{"/admin/test-probe", "/admin/test-probe-open"} {
		seen.calls = 0
		rec := panelPost(h, route+"?keep=1&tb="+tb+"&also=2", formBindingCookie, strings.NewReader("x=1"))
		if rec.Code != http.StatusNoContent || seen.calls != 1 {
			t.Fatalf("%s with its binding: %d, %d call(s)", route, rec.Code, seen.calls)
		}
		if seen.raw != "keep=1&also=2" || seen.uri != route+"?keep=1&also=2" {
			t.Errorf("%s: the handler saw query %q and URI %q; the binding must be gone and the rest kept", route, seen.raw, seen.uri)
		}
		if seen.tenant != panelTestTenant {
			t.Errorf("%s: the handler acted in %s, want the session's %s", route, seen.tenant, panelTestTenant)
		}
		if seen.ctxBound != "/x" {
			t.Errorf("%s: the gate put a binding on the handler's context (%q)", route, seen.ctxBound)
		}
		// An encoded spelling of the key (the audit's case): url.Query() decodes it to
		// "tb", so it must be refused rather than passed on with the binding left in
		// RawQuery and RequestURI; and alongside a literal one it is a second binding.
		for _, q := range []string{"t%62=" + tb, "%74b=" + tb, "%74%62=" + tb, "tb=" + tb + "&t%62=" + tb} {
			seen.calls = 0
			rec = panelPost(h, route+"?keep=1&"+q, formBindingCookie, strings.NewReader("x=1"))
			if rec.Code != http.StatusConflict || seen.calls != 0 {
				t.Errorf("%s?%s: %d, %d call(s); want 409 and none -- an encoded key is not the binding", route, q, rec.Code, seen.calls)
			}
		}
		// The strip removes every spelling that decodes to the key.
		if got := dropQueryParam("a=1&t%62=x&%74b=y&tb=z&b=2", "tb"); got != "a=1&b=2" {
			t.Errorf("dropQueryParam left %q, want a=1&b=2", got)
		}
		// The body is not a channel for the binding.
		seen.calls = 0
		rec = panelPost(h, route, formBindingCookie, strings.NewReader("tb="+tb))
		if rec.Code != http.StatusConflict || seen.calls != 0 {
			t.Errorf("%s with the binding in the body only: %d, %d call(s); want 409 and none", route, rec.Code, seen.calls)
		}
	}
}

// TestFormBinding_TheRefusalSaysWhatTheADRSays: the 409 is the panel's problem card with
// ADR 0031 §5.5's sentence, and neither the page nor the log carries the binding.
func TestFormBinding_TheRefusalSaysWhatTheADRSays(t *testing.T) {
	b, logs := panelBrowserWithLogsAndLedger(t, newFakeLedger(), &fakeStaff{}, &fakeInviter{})
	b.cookies[adminauth.CookieName] = formBindingCookie
	sent := strings.Repeat("Q", adminauth.FormBindingLen)
	rec := b.do(http.MethodPost, venueSaveHref+"?tb="+sent, url.Values{"name": {"x"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("a foreign binding answered %d, want 409", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, htmlText("This page belongs to another business — reload it.")) {
		t.Errorf("the 409 does not say the ADR's sentence")
	}
	if strings.Contains(rec.Body.String(), sent) || strings.Contains(logs.String(), sent) {
		t.Error("the refused binding reached the page or the log")
	}
	if !strings.Contains(logs.String(), "panel write refused") || !strings.Contains(logs.String(), venueSaveHref) {
		t.Errorf("the refusal is not logged with its route: %s", logs.String())
	}
}

// TestFormBinding_NoRedirectCarriesTheBinding drives every bound POST route with its
// binding and an empty form, over the ordinary fakes, and reads every Location: none
// carries the binding (S14). A write that succeeds ends in a 303 to a fixed panel path;
// the routes that answer a page instead are logged (they are in the hand-off).
func TestFormBinding_NoRedirectCarriesTheBinding(t *testing.T) {
	for _, identity := range []bool{false, true} {
		t.Run(map[bool]string{false: "flag off", true: "flag on"}[identity], func(t *testing.T) {
			noRedirectCarriesTheBinding(t, identity)
		})
	}
}

// noRedirectCarriesTheBinding is one flag state of the test above.
func noRedirectCarriesTheBinding(t *testing.T, identity bool) {
	a, h := formBindingAuthUnder(t, identity, formBindingOwner(), nil)
	tb := bindingOf(t, a, formBindingCookie, panelTestTenant)
	codes := map[int][]string{}
	for _, mr := range walkedRoutes(t, h) {
		method, route, _ := strings.Cut(mr, " ")
		if method != http.MethodPost || unboundPanelPost[route] || encodeRelayRoutes[route] {
			continue
		}
		rec := panelPost(h, route+"?tb="+tb, formBindingCookie, strings.NewReader(""))
		if rec.Code == http.StatusConflict {
			t.Errorf("%s refused its own binding", route)
		}
		if loc := rec.Header().Get("Location"); strings.Contains(loc, "tb=") || strings.Contains(loc, tb) {
			t.Errorf("%s redirected to %q, which carries the binding", route, loc)
		}
		if strings.Contains(rec.Body.String(), "tb="+tb) && rec.Code/100 == 3 {
			t.Errorf("%s: a redirect body carries the binding", route)
		}
		codes[rec.Code] = append(codes[rec.Code], route)
	}
	for c, rs := range codes {
		t.Logf("%d: %s", c, strings.Join(rs, " "))
	}
}

// TestFormBinding_TheBindingIsReadInOnePlace: in this package's production code the
// binding's parameter is named only by formbinding.go, and nothing reads a "tb" field
// from a form or a query anywhere else -- so no handler can take a tenant from it.
func TestFormBinding_TheBindingIsReadInOnePlace(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	read := regexp.MustCompile(`(Get|FormValue|PostFormValue)\("tb"\)|\["tb"\]`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.HasSuffix(f, "_templ.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if f != "formbinding.go" && strings.Contains(string(src), "FormBindingParam") {
			t.Errorf("%s names the binding's parameter; only formbinding.go may", f)
		}
		if read.Match(src) {
			t.Errorf("%s reads a \"tb\" value directly", f)
		}
	}
}
