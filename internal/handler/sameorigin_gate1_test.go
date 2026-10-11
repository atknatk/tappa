package handler

// GATE-1 (K-37, ADR 0028 §11): the four sameOrigin copies refuse a request whose
// Sec-Fetch-Site says same-site or cross-site, on EVERY branch — with an Origin or
// without one, and on the activation Submit's strict=false branch too — and leave
// every other header combination exactly as it was.
//
// THE GRIDS BELOW ARE THE WHOLE CONTRACT, AND THE "before" GRID IS MEASURED, NOT
// RECALLED: it is what the code answered before GATE-1, run cell by cell against the
// unchanged functions. "after" is what the code answers now. The two differ ONLY in
// the same-site and cross-site columns (TestSameOrigin_EveryOtherHeaderCombinationIsUnchanged
// checks that structurally as well as against the code), so the change is a
// narrowing and nothing else.
//
// EACH COPY HAS ITS OWN GRID, although four of the five are identical today. The
// duplication of the functions is deliberate (adminlogin.go says why), so a change
// to one copy must turn exactly that copy's grid red and no other.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/signup"
)

// gate1Base is the configured origin every copy is built with here.
const gate1Base = "https://panel.example.test"

// gate1Origins are the grid's rows, in order. "" omits the header.
var gate1Origins = []struct{ name, value string }{
	{"no Origin", ""},
	{"own Origin", gate1Base},
	{"foreign Origin", "https://evil.example"},
	{"Origin: null", "null"},
}

// gate1Sites are the grid's columns, in order. "" omits the header.
var gate1Sites = []string{"", "none", "same-origin", "same-site", "cross-site"}

// gate1Narrowed reports whether a Sec-Fetch-Site column is one GATE-1 changed.
func gate1Narrowed(site string) bool { return site == "same-site" || site == "cross-site" }

type gate1Copy struct {
	name  string
	check func(*http.Request) bool
	// before and after: one line per gate1Origins row, one mark per gate1Sites
	// column — A accepted, R refused. Columns: (absent) none same-origin same-site cross-site.
	before, after [4]string
	// bare is what a request with no Origin and no (or `none`) fetch metadata gets:
	// refused by the strict copies and Continue (the login CSRF gate), accepted by
	// Submit (its synchronizer token carries it).
	bare bool
}

func gate1Copies() []gate1Copy {
	login := &AdminAuth{baseURL: gate1Base}
	wizard := &Signup{baseURL: gate1Base}
	reset := &AdminReset{baseURL: gate1Base}
	act := &Activation{baseURL: gate1Base}
	return []gate1Copy{
		{
			name:  "AdminAuth.sameOrigin (adminlogin.go)",
			check: login.sameOrigin,
			before: [4]string{
				/* no Origin      */ "R R A A R",
				/* own Origin     */ "A A A A A",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "R R A A R",
			},
			after: [4]string{
				/* no Origin      */ "R R A R R",
				/* own Origin     */ "A A A R R",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "R R A R R",
			},
		},
		{
			name:  "Signup.sameOrigin (signup.go)",
			check: wizard.sameOrigin,
			before: [4]string{
				/* no Origin      */ "R R A A R",
				/* own Origin     */ "A A A A A",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "R R A A R",
			},
			after: [4]string{
				/* no Origin      */ "R R A R R",
				/* own Origin     */ "A A A R R",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "R R A R R",
			},
		},
		{
			name:  "AdminReset.sameOrigin (adminreset.go)",
			check: reset.sameOrigin,
			before: [4]string{
				/* no Origin      */ "R R A A R",
				/* own Origin     */ "A A A A A",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "R R A A R",
			},
			after: [4]string{
				/* no Origin      */ "R R A R R",
				/* own Origin     */ "A A A R R",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "R R A R R",
			},
		},
		{
			name:  "Activation.sameOrigin strict=true (Continue)",
			check: func(r *http.Request) bool { return act.sameOrigin(r, true) },
			before: [4]string{
				/* no Origin      */ "R R A A R",
				/* own Origin     */ "A A A A A",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "R R A A R",
			},
			after: [4]string{
				/* no Origin      */ "R R A R R",
				/* own Origin     */ "A A A R R",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "R R A R R",
			},
		},
		{
			// strict=false is the branch that used to accept ANY Sec-Fetch-Site when
			// Origin was absent; K-37 makes it read the header there too.
			name:  "Activation.sameOrigin strict=false (Submit)",
			check: func(r *http.Request) bool { return act.sameOrigin(r, false) },
			bare:  true,
			before: [4]string{
				/* no Origin      */ "A A A A A",
				/* own Origin     */ "A A A A A",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "A A A A A",
			},
			after: [4]string{
				/* no Origin      */ "A A A R R",
				/* own Origin     */ "A A A R R",
				/* foreign Origin */ "R R R R R",
				/* Origin: null   */ "A A A R R",
			},
		},
	}
}

// gate1Cell reads one mark out of a grid line.
func gate1Cell(t *testing.T, line string, col int) bool {
	t.Helper()
	marks := strings.Fields(line)
	if len(marks) != len(gate1Sites) {
		t.Fatalf("grid line %q has %d marks, want %d", line, len(marks), len(gate1Sites))
	}
	switch marks[col] {
	case "A":
		return true
	case "R":
		return false
	}
	t.Fatalf("grid line %q: mark %q is neither A nor R", line, marks[col])
	return false
}

// gate1Request builds the POST one cell describes.
func gate1Request(origin, site string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if site != "" {
		r.Header.Set("Sec-Fetch-Site", site)
	}
	return r
}

func gate1Verdict(ok bool) string {
	if ok {
		return "accepted"
	}
	return "refused"
}

// TestSameOrigin_RefusesSameSiteAndCrossSiteOnEveryBranch is K-37 itself: every
// copy, every Origin shape (absent, own, foreign, null), the activation Submit's
// strict=false branch included, refuses same-site and cross-site.
//
// THE SPELLING IS NOT A WAY AROUND IT. The header's grammar is lower case, but M5-01
// shipped a bug where a case-sensitive test let "Cross-Site" through (crossSite's
// comment), so the refusal is case- and space-insensitive in all four copies.
func TestSameOrigin_RefusesSameSiteAndCrossSiteOnEveryBranch(t *testing.T) {
	spellings := []string{"same-site", "cross-site", "Same-Site", "CROSS-SITE", " same-site ", "Cross-Site"}
	for _, c := range gate1Copies() {
		for row, o := range gate1Origins {
			for col, site := range gate1Sites {
				if gate1Narrowed(site) && gate1Cell(t, c.after[row], col) {
					t.Errorf("%s: the after grid accepts %s + Sec-Fetch-Site %q; K-37 refuses it on every branch",
						c.name, o.name, site)
				}
			}
			for _, site := range spellings {
				if got := c.check(gate1Request(o.value, site)); got {
					t.Errorf("%s: %s + Sec-Fetch-Site %q was accepted, want refused (K-37)",
						c.name, o.name, site)
				}
			}
		}
	}
}

// TestSameOrigin_EveryOtherHeaderCombinationIsUnchanged pins the rest of the grid:
// every cell outside the same-site and cross-site columns answers what it answered
// before GATE-1 — in particular the strict copies and Continue still REFUSE a
// request with no Origin and no (or `none`) fetch metadata, which is the login CSRF
// gate adminlogin.go describes, and the activation Submit still accepts it.
func TestSameOrigin_EveryOtherHeaderCombinationIsUnchanged(t *testing.T) {
	for _, c := range gate1Copies() {
		for row, o := range gate1Origins {
			for col, site := range gate1Sites {
				before, after := gate1Cell(t, c.before[row], col), gate1Cell(t, c.after[row], col)
				if gate1Narrowed(site) {
					continue // TestSameOrigin_RefusesSameSiteAndCrossSiteOnEveryBranch's cells
				}
				// STRUCTURAL: the change is a narrowing of two columns and nothing else.
				if before != after {
					t.Errorf("%s: %s + Sec-Fetch-Site %q is %s before and %s after; only the "+
						"same-site and cross-site columns may change", c.name, o.name, site,
						gate1Verdict(before), gate1Verdict(after))
				}
				if got := c.check(gate1Request(o.value, site)); got != after {
					t.Errorf("%s: %s + Sec-Fetch-Site %q was %s, want %s (unchanged by GATE-1)",
						c.name, o.name, site, gate1Verdict(got), gate1Verdict(after))
				}
			}
		}
	}
	// The cells the K-37 text names, spelled out so a grid edit cannot quietly drop
	// them: the strict copies and Continue refuse a bare request (the login CSRF gate),
	// Submit accepts it.
	for _, c := range gate1Copies() {
		for _, site := range []string{"", "none"} {
			if got := c.check(gate1Request("", site)); got != c.bare {
				t.Errorf("%s: no Origin + Sec-Fetch-Site %q was %s, want %s",
					c.name, site, gate1Verdict(got), gate1Verdict(c.bare))
			}
		}
	}
}

// TestSameOrigin_NoReferrerPagesStillPass: a page served with Referrer-Policy:
// no-referrer posts `Origin: null` + `Sec-Fetch-Site: same-origin` (measured in
// headless Chrome, internal/httpx/operatorhost.go). Every copy accepts it, and the
// plain same-origin POST with no Origin header at all.
func TestSameOrigin_NoReferrerPagesStillPass(t *testing.T) {
	for _, c := range gate1Copies() {
		for _, origin := range []string{"null", ""} {
			if !c.check(gate1Request(origin, "same-origin")) {
				t.Errorf("%s: Origin %q + Sec-Fetch-Site same-origin was refused; a no-referrer "+
					"page of our own posts exactly that", c.name, origin)
			}
		}
		if !c.check(gate1Request(gate1Base, "same-origin")) {
			t.Errorf("%s: our own Origin + same-origin was refused", c.name)
		}
	}
}

// gate1Post sends one request through a real router with the two headers under test.
func gate1Post(h http.Handler, target string, form url.Values, origin, site string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.41:5000"
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if site != "" {
		req.Header.Set("Sec-Fetch-Site", site)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// gate1RefusedShapes are the request shapes K-37 refuses at a route: same-site and
// cross-site, beside our own Origin and with no Origin at all.
var gate1RefusedShapes = []struct{ name, origin, site string }{
	{"own Origin + same-site", "own", "same-site"},
	{"own Origin + cross-site", "own", "cross-site"},
	{"no Origin + same-site", "", "same-site"},
	{"no Origin + cross-site", "", "cross-site"},
}

// TestSameOrigin_EveryGateRouteRefusesSameSiteBeforeItsWork drives one route behind
// each copy through its real router, so the grid above is measured where it is used:
// the refused shapes never reach the work the gate protects, and a same-origin
// control with every other input identical does — so the refusal is the Origin gate's
// and not some later check's.
func TestSameOrigin_EveryGateRouteRefusesSameSiteBeforeItsWork(t *testing.T) {
	t.Run("panel: sign-out never reaches the resolver", func(t *testing.T) {
		var verifyCalls int
		admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
			verifyCalls++
			return adminauth.Resolved{SessionID: uuid.New(), TenantID: uuid.New(),
				AdminUserID: uuid.New(), Role: "owner", FullName: "KF Owner"}, nil
		}}
		h := newAdminRouter(t, admins, &fakeTrail{})
		cookie := &http.Cookie{Name: adminauth.CookieName, Value: strings.Repeat("A", 43)}
		own := func(o string) string {
			if o == "own" {
				return testBaseURL
			}
			return o
		}
		for _, s := range gate1RefusedShapes {
			verifyCalls = 0
			rec := gate1Post(h, "/admin/logout", url.Values{}, own(s.origin), s.site, cookie)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" || verifyCalls != 0 {
				t.Errorf("%s: %d -> %q with %d resolver call(s), want 303 -> /admin and 0",
					s.name, rec.Code, rec.Header().Get("Location"), verifyCalls)
			}
		}
		verifyCalls = 0
		if rec := gate1Post(h, "/admin/logout", url.Values{}, testBaseURL, "same-origin", cookie); rec.Code != http.StatusSeeOther || verifyCalls != 1 {
			t.Fatalf("control: a same-origin sign-out answered %d with %d resolver call(s), want 303 and 1",
				rec.Code, verifyCalls)
		}
	})

	t.Run("signup: the account step never reaches the provisioner", func(t *testing.T) {
		attempt := func(origin, site string) (*httptest.ResponseRecorder, int) {
			p := &fakeProvisioner{}
			d, _ := newSignupDriver(t, p, &fakeVAT{status: signup.VATValid})
			csrf := d.walkToAccount(t)
			d.advance(time.Minute)
			rec := d.do(http.MethodPost, signupAccountPath, accountForm(csrf), func(r *http.Request) {
				r.Header.Del("Origin")
				if origin == "own" {
					r.Header.Set("Origin", testBaseURL)
				}
				if site != "" {
					r.Header.Set("Sec-Fetch-Site", site)
				}
			})
			return rec, p.count()
		}
		for _, s := range gate1RefusedShapes {
			if rec, n := attempt(s.origin, s.site); rec.Code != http.StatusBadRequest || n != 0 {
				t.Errorf("%s: %d with %d provisioner call(s), want 400 and 0", s.name, rec.Code, n)
			}
		}
		if rec, n := attempt("own", "same-origin"); rec.Code != http.StatusSeeOther || n != 1 {
			t.Fatalf("control: a same-origin account step answered %d with %d provisioner call(s), want 303 and 1",
				rec.Code, n)
		}
	})

	t.Run("recovery: the request form never reaches IssueForEmail", func(t *testing.T) {
		attempt := func(origin, site string) (*httptest.ResponseRecorder, int) {
			resets := &fakeResets{}
			router, h := newResetRouter(t, resets, &recordingChannel{}, &fakeTrail{})
			h.sleep = func(time.Duration) {} // the constant-time floor is not under test
			b := newBrowser(t, router)
			csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
			var cookies []*http.Cookie
			for name, value := range b.cookies {
				cookies = append(cookies, &http.Cookie{Name: name, Value: value})
			}
			if origin == "own" {
				origin = testBaseURL
			}
			rec := gate1Post(router, adminResetPath, url.Values{"csrf": {csrf}, "email": {"x@y.test"}},
				origin, site, cookies...)
			issue, _ := resets.calls()
			return rec, issue
		}
		for _, s := range gate1RefusedShapes {
			if rec, n := attempt(s.origin, s.site); rec.Code != http.StatusBadRequest || n != 0 {
				t.Errorf("%s: %d with %d IssueForEmail call(s), want 400 and 0", s.name, rec.Code, n)
			}
		}
		if rec, n := attempt("own", "same-origin"); rec.Code != http.StatusOK || n != 1 {
			t.Fatalf("control: a same-origin request form answered %d with %d IssueForEmail call(s), want 200 and 1",
				rec.Code, n)
		}
	})

	// The activation tests' configured origin (newHandlerWith).
	const activationOrigin = "http://localhost:8080"
	activationShape := func(o string) string {
		if o == "own" {
			return activationOrigin
		}
		return o
	}

	t.Run("activation Submit (strict=false): consent is never recorded", func(t *testing.T) {
		attempt := func(origin, site string) *httptest.ResponseRecorder {
			h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
			return gate1Post(h, "/api/activate", consent(), origin, site, codeCookie())
		}
		for _, s := range gate1RefusedShapes {
			if rec := attempt(activationShape(s.origin), s.site); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: %d, want 400", s.name, rec.Code)
			}
		}
		// THE CONTROLS ARE THE BRANCH strict=false EXISTS FOR: no Origin, and no or
		// same-origin fetch metadata, still pass.
		for _, site := range []string{"", "same-origin"} {
			if rec := attempt("", site); rec.Code != http.StatusSeeOther {
				t.Fatalf("control: no Origin + Sec-Fetch-Site %q answered %d, want 303 (Submit accepts it)",
					site, rec.Code)
			}
		}
	})

	t.Run("activation Continue (strict=true): no activation is started", func(t *testing.T) {
		attempt := func(origin, site string) *httptest.ResponseRecorder {
			h := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
			return gate1Post(h, "/activate", url.Values{"code": {fakeCode}}, origin, site)
		}
		for _, s := range gate1RefusedShapes {
			if rec := attempt(activationShape(s.origin), s.site); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: %d, want 400", s.name, rec.Code)
			}
		}
		if rec := attempt(activationOrigin, "same-origin"); rec.Code != http.StatusSeeOther || cookieNamed(rec, activationCookieName) == nil {
			t.Fatalf("control: a same-origin Continue answered %d, want 303 with the activation cookie", rec.Code)
		}
	})
}
