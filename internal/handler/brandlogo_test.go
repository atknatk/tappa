package handler

// brandlogo_test.go -- M10 WL-6: the two logo routes against FAKES, mounted the way
// cmd/tappa mounts them (Tap, AdminAuth and BrandLogos on one router), so every request
// runs the real chains and the real budgets. Every response is read from rec.Result(),
// never from the recorder's live header map (agent-brief: a header written and then
// removed would still be in that map); one class of each answer is also compared with
// what a real server puts on the wire.
//
// The fake reader keeps tenant.BrandReader's contract -- a digest is found only under
// the tenant that stored it, and every miss is tenant.ErrLogoNotFound itself.
// brandread_db_test.go measures the real reader keeping it against Postgres, and
// brandlogo_db_test.go drives the routes over the real reader end to end.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/pages"
)

var (
	logoTenantA      = uuid.MustParse("a1a1a1a1-0000-4000-8000-00000000000a")
	logoTenantB      = uuid.MustParse("b2b2b2b2-0000-4000-8000-00000000000b")
	logoTapSession   = uuid.MustParse("a1a1a1a1-0000-4000-8000-0000000000e1")
	logoPanelSession = uuid.MustParse("a1a1a1a1-0000-4000-8000-0000000000a1")
)

// The two stored logos. The routes never decode what they serve, so the bytes only
// have to be distinct; they start with each format's signature for readability.
var (
	logoPNGA  = []byte("\x89PNG\r\n\x1a\n-wl6-fixture-business-A")
	logoJPEGB = []byte("\xff\xd8\xff\xe0-wl6-fixture-business-B")
)

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// unknownDigest is correctly spelled and stored by nobody.
var unknownDigest = digestOf([]byte("wl6: a digest nobody stored"))

type logoCall struct {
	tenant uuid.UUID
	digest string
}

// fakeLogoReader keeps tenant.BrandReader's contract: found only under the tenant
// that stored it; every miss is tenant.ErrLogoNotFound itself.
type fakeLogoReader struct {
	mu    sync.Mutex
	logos map[uuid.UUID]map[string]tenant.StoredLogo
	err   error
	calls []logoCall
}

func newFakeLogoReader() *fakeLogoReader {
	return &fakeLogoReader{logos: map[uuid.UUID]map[string]tenant.StoredLogo{
		logoTenantA: {digestOf(logoPNGA): {Data: logoPNGA, MIME: "image/png"}},
		logoTenantB: {digestOf(logoJPEGB): {Data: logoJPEGB, MIME: "image/jpeg"}},
	}}
}

func (f *fakeLogoReader) Logo(_ context.Context, tenantID uuid.UUID, digest string) (tenant.StoredLogo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, logoCall{tenantID, digest})
	if f.err != nil {
		return tenant.StoredLogo{}, f.err
	}
	if l, ok := f.logos[tenantID][digest]; ok {
		return l, nil
	}
	return tenant.StoredLogo{}, tenant.ErrLogoNotFound
}

func (f *fakeLogoReader) callLog() []logoCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]logoCall(nil), f.calls...)
}

// liveEmployee and livePanel resolve a session for the given business.
func liveEmployee(tenantID uuid.UUID) *fakeSessions {
	return &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{ID: logoTapSession, TenantID: tenantID, EmployeeID: testEmployee}, nil
	}}
}

func livePanel(tenantID uuid.UUID) *fakeAdmins {
	return &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: logoPanelSession, TenantID: tenantID, AdminUserID: panelTestAdmin,
			Role: "manager", FullName: "KF Manager",
		}, nil
	}}
}

// logoSurfaces builds the three handlers as cmd/tappa does.
func logoSurfaces(t *testing.T, reader logoReader, sess *fakeSessions, admins *fakeAdmins) (*Tap, *AdminAuth, *BrandLogos) {
	t.Helper()
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		sess, &fakeCheckins{}, &fakeAudit{}, tapCfg(), discardLogger())
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	ledger := newFakeLedger()
	panel, err := NewAdminAuth(admins, &fakeTrail{}, ledger, ledger, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, adminTestConfig(), discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	logos, err := NewBrandLogos(reader, panel, tp, discardLogger())
	if err != nil {
		t.Fatalf("NewBrandLogos: %v", err)
	}
	return tp, panel, logos
}

// logoRouter mounts the three on one chi router.
func logoRouter(t *testing.T, reader logoReader, sess *fakeSessions, admins *fakeAdmins) http.Handler {
	t.Helper()
	tp, panel, logos := logoSurfaces(t, reader, sess, admins)
	r := chi.NewRouter()
	tp.Mount(r)
	panel.Mount(r)
	logos.Mount(r)
	return r
}

func employeeCookie() *http.Cookie { return sessionCookie() }

func panelCookie() *http.Cookie {
	return &http.Cookie{Name: adminauth.CookieName, Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
}

// answer is one response as the client receives it: status, every header, body.
type answer struct {
	status int
	header http.Header
	body   []byte
}

func (a answer) same(b answer) bool {
	return a.status == b.status && reflect.DeepEqual(a.header, b.header) && bytes.Equal(a.body, b.body)
}

func (a answer) String() string {
	keys := make([]string, 0, len(a.header))
	for k := range a.header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(strconv.Itoa(a.status))
	for _, k := range keys {
		sb.WriteString(" | " + k + ": " + strings.Join(a.header[k], ", "))
	}
	sb.WriteString(" | body " + strconv.Quote(string(a.body)))
	return sb.String()
}

// askFrom sends one request from addr and reads the answer from rec.Result().
func askFrom(t *testing.T, h http.Handler, addr, method, target string, header http.Header, cookies ...*http.Cookie) answer {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = addr
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", method, target, err)
	}
	return answer{status: res.StatusCode, header: res.Header, body: body}
}

func ask(t *testing.T, h http.Handler, method, target string, cookies ...*http.Cookie) answer {
	t.Helper()
	return askFrom(t, h, "203.0.113.5:41234", method, target, nil, cookies...)
}

// wantLogoHeader is ADR 0024 §5's list, plus the Content-Length this route sets so the
// body is not chunked. Nothing else: an extra header turns the exact comparison red.
func wantLogoHeader(digest, mime, name string, n int) http.Header {
	return http.Header{
		"Cache-Control":                {"private, max-age=31536000, immutable"},
		"Content-Disposition":          {`inline; filename="` + name + `"`},
		"Content-Length":               {strconv.Itoa(n)},
		"Content-Security-Policy":      {"default-src 'none'; sandbox"},
		"Content-Type":                 {mime},
		"Cross-Origin-Resource-Policy": {"same-origin"},
		"Etag":                         {`"` + digest + `"`},
		"X-Content-Type-Options":       {"nosniff"},
	}
}

// wantNotFound is the one refusal.
func wantNotFound() answer {
	return answer{
		status: http.StatusNotFound,
		header: http.Header{
			"Cache-Control":          {"no-store"},
			"Content-Length":         {strconv.Itoa(len(logoNotFoundBody))},
			"Content-Type":           {"text/plain; charset=utf-8"},
			"X-Content-Type-Options": {"nosniff"},
		},
		body: []byte(logoNotFoundBody),
	}
}

func tapLogoPath(digest string) string   { return "/t/logo/" + digest }
func panelLogoPath(digest string) string { return "/admin/brand/logo/" + digest }

// ---------------------------------------------------------------------------------
// The 200: headers exactly, type and file name from the stored type.
// ---------------------------------------------------------------------------------

func TestLogoRoutes_TheHeadersAreExactlyTheADRs(t *testing.T) {
	cases := []struct {
		name     string
		as       uuid.UUID
		data     []byte
		mime     string
		fileName string
	}{
		{"business A's PNG", logoTenantA, logoPNGA, "image/png", "logo.png"},
		{"business B's JPEG", logoTenantB, logoJPEGB, "image/jpeg", "logo.jpg"},
	}
	for _, c := range cases {
		h := logoRouter(t, newFakeLogoReader(), liveEmployee(c.as), livePanel(c.as))
		digest := digestOf(c.data)
		want := answer{status: http.StatusOK, header: wantLogoHeader(digest, c.mime, c.fileName, len(c.data)), body: c.data}
		for _, route := range []struct {
			name   string
			path   string
			cookie *http.Cookie
		}{
			{"GET /t/logo", tapLogoPath(digest), employeeCookie()},
			{"GET /admin/brand/logo", panelLogoPath(digest), panelCookie()},
		} {
			got := ask(t, h, http.MethodGet, route.path, route.cookie)
			if !got.same(want) {
				t.Errorf("%s, %s:\n got %s\nwant %s", c.name, route.name, got, want)
			}
			// THE REQUEST NAMES NOTHING ABOUT THE ANSWER: a query naming a type, a file
			// name and another business, and request headers naming a type, leave the
			// answer byte for byte as it was.
			hostile := askFrom(t, h, "203.0.113.5:41234", http.MethodGet,
				route.path+"?type=text/html&mime=text/html&name=logo.html&tenant="+logoTenantB.String(),
				http.Header{"Accept": {"text/html"}, "Content-Type": {"text/html"}}, route.cookie)
			if !hostile.same(want) {
				t.Errorf("%s, %s, with a hostile query and headers:\n got %s\nwant %s", c.name, route.name, hostile, want)
			}
		}
	}

	// THE TYPE IS THE STORED COLUMN, NOT A SNIFF: bytes that sniff as GIF, stored as
	// image/png, are served as image/png. (The gate never stores such a pair; the route
	// still must not second-guess the column -- ADR 0024 §5, "sunum anında koklanmaz".)
	gifLike := []byte("GIF89a-wl6-bytes-that-sniff-as-gif")
	if sniffed := http.DetectContentType(gifLike); sniffed != "image/gif" {
		t.Fatalf("fixture: the bytes sniff as %q, not image/gif", sniffed)
	}
	reader := newFakeLogoReader()
	reader.logos[logoTenantA][digestOf(gifLike)] = tenant.StoredLogo{Data: gifLike, MIME: "image/png"}
	h := logoRouter(t, reader, liveEmployee(logoTenantA), livePanel(logoTenantA))
	want := answer{http.StatusOK, wantLogoHeader(digestOf(gifLike), "image/png", "logo.png", len(gifLike)), gifLike}
	if got := ask(t, h, http.MethodGet, tapLogoPath(digestOf(gifLike)), employeeCookie()); !got.same(want) {
		t.Errorf("stored image/png, bytes sniffing as GIF:\n got %s\nwant %s", got, want)
	}
}

// TestLogoRoutes_AnUnresolvedIdentityIsNotNoSession calls the two handlers WITHOUT
// their chains -- the wiring mistake. The tap handler answers 500 (tap.go's rule: an
// unresolved identity is "we do not know", not "no session"); the panel handler, whose
// chain answers every unauthenticated request itself, answers the 404. Neither reads.
func TestLogoRoutes_AnUnresolvedIdentityIsNotNoSession(t *testing.T) {
	reader := newFakeLogoReader()
	_, _, logos := logoSurfaces(t, reader, liveEmployee(logoTenantA), livePanel(logoTenantA))
	call := func(handler http.HandlerFunc, path string) answer {
		t.Helper()
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("sha", digestOf(logoPNGA))
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(employeeCookie())
		req.AddCookie(panelCookie())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		handler(rec, req)
		res := rec.Result()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("reading: %v", err)
		}
		return answer{res.StatusCode, res.Header, body}
	}
	if got := call(logos.tapLogo, tapLogoPath(digestOf(logoPNGA))); got.status != http.StatusInternalServerError || bytes.Contains(got.body, logoPNGA) {
		t.Errorf("tap handler with no identity resolved = %s, want 500 and no bytes", got)
	}
	if got := call(logos.panelLogo, panelLogoPath(digestOf(logoPNGA))); !got.same(wantNotFound()) {
		t.Errorf("panel handler with no identity resolved = %s, want the one 404", got)
	}
	if calls := reader.callLog(); len(calls) != 0 {
		t.Errorf("a handler with no resolved identity asked the reader %v", calls)
	}
}

// TestLogoRoutes_TheWireCarriesWhatTheHandlerSets runs the PRODUCTION router
// (httpx.NewRouter: request id, real-ip, access log, recoverer, timeout) on a real
// server and compares what the client receives with what the recorder saw. It is the
// measurement of what the global middleware adds to these responses: net/http's Date
// and nothing else.
func TestLogoRoutes_TheWireCarriesWhatTheHandlerSets(t *testing.T) {
	reader := newFakeLogoReader()
	tp, panel, logos := logoSurfaces(t, reader, liveEmployee(logoTenantA), livePanel(logoTenantA))
	srv := httptest.NewServer(httpx.NewRouter(adminTestConfig(), discardLogger(), tp, panel, logos))
	t.Cleanup(srv.Close)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	onWire := func(path string, header http.Header, cookie *http.Cookie) answer {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		for k, vs := range header {
			req.Header[k] = vs
		}
		req.AddCookie(cookie)
		res, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if res.Header.Get("Date") == "" {
			t.Fatalf("GET %s: no Date header on the wire", path)
		}
		h := res.Header.Clone()
		h.Del("Date")
		return answer{status: res.StatusCode, header: h, body: body}
	}

	digest := digestOf(logoPNGA)
	cases := []struct {
		name   string
		path   string
		header http.Header
		cookie *http.Cookie
		want   answer
	}{
		{"200, tap route", tapLogoPath(digest), nil, employeeCookie(),
			answer{http.StatusOK, wantLogoHeader(digest, "image/png", "logo.png", len(logoPNGA)), logoPNGA}},
		{"200, panel route", panelLogoPath(digest), nil, panelCookie(),
			answer{http.StatusOK, wantLogoHeader(digest, "image/png", "logo.png", len(logoPNGA)), logoPNGA}},
		{"404, another business's digest", tapLogoPath(digestOf(logoJPEGB)), nil, employeeCookie(), wantNotFound()},
		{"304, own digest named in If-None-Match", tapLogoPath(digest),
			http.Header{"If-None-Match": {`"` + digest + `"`}}, employeeCookie(), want304(digest)},
	}
	for _, c := range cases {
		if got := onWire(c.path, c.header, c.cookie); !got.same(c.want) {
			t.Errorf("%s on the wire (Date removed):\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------------
// Every "no" is one answer, and the cheap ones never reach the reader.
// ---------------------------------------------------------------------------------

func TestLogoRoutes_EveryRefusalIsTheSameNotFound(t *testing.T) {
	reader := newFakeLogoReader()
	h := logoRouter(t, reader, liveEmployee(logoTenantA), livePanel(logoTenantA))
	own, foreign := digestOf(logoPNGA), digestOf(logoJPEGB)
	reference := wantNotFound()

	// POSITIVE CONTROL: the session and the own digest really do produce a logo on both
	// routes, so every 404 below is a refusal and not a broken fixture.
	for _, c := range []struct {
		path   string
		cookie *http.Cookie
	}{{tapLogoPath(own), employeeCookie()}, {panelLogoPath(own), panelCookie()}} {
		if got := ask(t, h, http.MethodGet, c.path, c.cookie); got.status != http.StatusOK || !bytes.Equal(got.body, logoPNGA) {
			t.Fatalf("control: GET %s = %s, want 200 with business A's logo", c.path, got)
		}
	}

	type refusal struct {
		name   string
		path   func(prefix string) string
		reader bool // whether the reader is (correctly) asked
	}
	refusals := []refusal{
		{"another business's digest", func(p string) string { return p + foreign }, true},
		{"a digest nobody stored", func(p string) string { return p + unknownDigest }, true},
		{"another business's digest, its tenant named in the query", func(p string) string {
			return p + foreign + "?tenant=" + logoTenantB.String() + "&tenant_id=" + logoTenantB.String()
		}, true},
		{"the own digest in upper case", func(p string) string { return p + strings.ToUpper(own) }, false},
		{"63 digits", func(p string) string { return p + own[:63] }, false},
		{"65 digits", func(p string) string { return p + own + "0" }, false},
		{"the digest plus .png", func(p string) string { return p + own + ".png" }, false},
		{"an encoded slash inside the digest", func(p string) string { return p + own[:32] + "%2F" + own[32:] }, false},
		{"an encoded dot-dot", func(p string) string { return p + "%2E%2E" }, false},
		{"dot-dot", func(p string) string { return p + ".." }, false},
		{"64 characters, not hex", func(p string) string { return p + strings.Repeat("g", 64) }, false},
	}

	before := len(reader.callLog())
	asked := 0
	for _, route := range []struct {
		name   string
		prefix string
		cookie *http.Cookie
	}{{"tap", "/t/logo/", employeeCookie()}, {"panel", "/admin/brand/logo/", panelCookie()}} {
		for _, r := range refusals {
			n := len(reader.callLog())
			got := ask(t, h, http.MethodGet, r.path(route.prefix), route.cookie)
			if !got.same(reference) {
				t.Errorf("%s route, %s:\n got %s\nwant %s", route.name, r.name, got, reference)
			}
			calls := reader.callLog()[n:]
			switch {
			case r.reader && len(calls) != 1:
				t.Errorf("%s route, %s: the reader was asked %d times, want 1", route.name, r.name, len(calls))
			case !r.reader && len(calls) != 0:
				t.Errorf("%s route, %s: a malformed digest reached the reader (%v) -- it must be refused "+
					"before the logo read", route.name, r.name, calls)
			}
			if r.reader {
				asked++
			}
		}
	}
	// EVERY READ RAN UNDER THE SESSION'S BUSINESS: never B's, whatever the query said.
	for _, c := range reader.callLog()[before:] {
		if c.tenant != logoTenantA {
			t.Errorf("the reader was asked under %s; the session is business A's (%s)", c.tenant, logoTenantA)
		}
	}
	if got := len(reader.callLog()) - before; got != asked {
		t.Errorf("reader calls = %d, want %d", got, asked)
	}

	// The tap route without a live session: no cookie, a revoked session, and a panel
	// cookie (which the tap chain does not read). Same 404; the reader is never asked.
	revoked := &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{ID: logoTapSession, TenantID: logoTenantA, EmployeeID: testEmployee}, session.ErrRevoked
	}}
	unknownCookie := &fakeSessions{verify: func() (session.Resolved, error) {
		return session.Resolved{}, session.ErrNoSession
	}}
	for _, c := range []struct {
		name    string
		sess    *fakeSessions
		cookies []*http.Cookie
	}{
		{"no cookie", liveEmployee(logoTenantA), nil},
		{"a revoked session", revoked, []*http.Cookie{employeeCookie()}},
		{"a cookie that names no session", unknownCookie, []*http.Cookie{employeeCookie()}},
		{"only a panel cookie", liveEmployee(logoTenantA), []*http.Cookie{panelCookie()}},
	} {
		r := newFakeLogoReader()
		hh := logoRouter(t, r, c.sess, livePanel(logoTenantA))
		for _, digest := range []string{own, foreign, unknownDigest} {
			if got := ask(t, hh, http.MethodGet, tapLogoPath(digest), c.cookies...); !got.same(reference) {
				t.Errorf("tap route, %s, digest %s…:\n got %s\nwant %s", c.name, digest[:8], got, reference)
			}
		}
		if calls := r.callLog(); len(calls) != 0 {
			t.Errorf("tap route, %s: the reader was asked %v", c.name, calls)
		}
	}
}

// TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer measures the paths that
// never reach a logo handler: the router answers them itself (chi's 404, or 405 for a
// method), from the URL alone -- before either chain, so no session is resolved, no
// budget is spent and no business is read. Their body is the router's, not the logo
// refusal's; the difference depends on the path's SHAPE and on nothing a session holds.
func TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer(t *testing.T) {
	reader := newFakeLogoReader()
	admins := livePanel(logoTenantA)
	h := logoRouter(t, reader, liveEmployee(logoTenantA), admins)
	own, foreign := digestOf(logoPNGA), digestOf(logoJPEGB)

	routerNotFound := answer{
		status: http.StatusNotFound,
		header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}, "X-Content-Type-Options": {"nosniff"}},
		body:   []byte("404 page not found\n"),
	}
	for _, path := range []string{
		"/t/logo/", "/t/logo/" + own + "/x", "/admin/brand/logo/", "/admin/brand/logo/" + own + "/x",
	} {
		for _, cookie := range []*http.Cookie{employeeCookie(), panelCookie()} {
			if got := ask(t, h, http.MethodGet, path, cookie); !got.same(routerNotFound) {
				t.Errorf("GET %s: got %s, want the router's own 404 %s", path, got, routerNotFound)
			}
		}
	}

	// Every other method: 405 from the router, the same for the own digest and another
	// business's -- so the method cannot tell the two apart either. The seven standard
	// methods below are answered with `Allow: GET`; a method the router does not know
	// (FOO) gets the 405 with no Allow header at all.
	for _, c := range []struct {
		method string
		allow  []string
	}{
		{http.MethodHead, []string{http.MethodGet}},
		{http.MethodPost, []string{http.MethodGet}},
		{http.MethodPut, []string{http.MethodGet}},
		{http.MethodDelete, []string{http.MethodGet}},
		{http.MethodPatch, []string{http.MethodGet}},
		{http.MethodOptions, []string{http.MethodGet}},
		{http.MethodTrace, []string{http.MethodGet}},
		{"FOO", nil},
	} {
		for _, prefix := range []struct {
			p      string
			cookie *http.Cookie
		}{{"/t/logo/", employeeCookie()}, {"/admin/brand/logo/", panelCookie()}} {
			mine := ask(t, h, c.method, prefix.p+own, prefix.cookie)
			theirs := ask(t, h, c.method, prefix.p+foreign, prefix.cookie)
			if mine.status != http.StatusMethodNotAllowed || !mine.same(theirs) {
				t.Errorf("%s %s…: own %s, another business's %s; want one 405", c.method, prefix.p, mine, theirs)
			}
			if allow := mine.header.Values("Allow"); !reflect.DeepEqual(allow, c.allow) {
				t.Errorf("%s %s…: Allow = %v, want %v", c.method, prefix.p, allow, c.allow)
			}
		}
	}
	if calls := reader.callLog(); len(calls) != 0 {
		t.Errorf("the reader was asked %v by a path or method the routes do not serve", calls)
	}
	if n := admins.verifiedCount(); n != 0 {
		t.Errorf("the panel resolver ran %d time(s) for answers the router gave by itself", n)
	}
}

// ---------------------------------------------------------------------------------
// Each route reads its own surface's session and no other.
// ---------------------------------------------------------------------------------

func TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession(t *testing.T) {
	own := digestOf(logoPNGA)

	// An employee cookie and NO panel session, at the panel route: the panel's
	// signed-out answer, no bytes, and the panel resolver is not even asked (there is no
	// panel cookie to resolve).
	reader := newFakeLogoReader()
	admins := livePanel(logoTenantA)
	h := logoRouter(t, reader, liveEmployee(logoTenantA), admins)
	got := ask(t, h, http.MethodGet, panelLogoPath(own), employeeCookie())
	if got.status != http.StatusSeeOther || got.header.Get("Location") != adminLoginPath {
		t.Fatalf("panel route with only an employee cookie = %s, want 303 to %s", got, adminLoginPath)
	}
	if bytes.Contains(got.body, logoPNGA) || got.header.Get("Content-Type") == "image/png" {
		t.Fatalf("panel route with only an employee cookie served the logo: %s", got)
	}
	if calls := reader.callLog(); len(calls) != 0 {
		t.Fatalf("panel route with only an employee cookie asked the reader %v", calls)
	}
	if n := admins.verifiedCount(); n != 0 {
		t.Fatalf("the panel resolver ran %d time(s) without a panel cookie", n)
	}

	// A panel cookie whose session is revoked, expired or unknown (adminauth answers all
	// of them ErrNoSession): the same signed-out answer, no bytes, no read -- the
	// resolver ran and said no.
	signedOut := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{}, adminauth.ErrNoSession
	}}
	reader = newFakeLogoReader()
	h = logoRouter(t, reader, liveEmployee(logoTenantA), signedOut)
	got = ask(t, h, http.MethodGet, panelLogoPath(own), panelCookie(), employeeCookie())
	if got.status != http.StatusSeeOther || got.header.Get("Location") != adminLoginPath || bytes.Contains(got.body, logoPNGA) {
		t.Fatalf("panel route with a signed-out panel session = %s, want 303 to %s and no bytes", got, adminLoginPath)
	}
	if n, calls := signedOut.verifiedCount(), reader.callLog(); n != 1 || len(calls) != 0 {
		t.Fatalf("signed-out panel session: resolver ran %d time(s), reader asked %v; want 1 and none", n, calls)
	}

	// BOTH cookies, for DIFFERENT businesses: the employee is business B's, the panel
	// session business A's. Each route serves its own surface's business and refuses
	// the other's digest -- the identity a route reads is its own surface's.
	h = logoRouter(t, newFakeLogoReader(), liveEmployee(logoTenantB), livePanel(logoTenantA))
	both := []*http.Cookie{employeeCookie(), panelCookie()}
	for _, c := range []struct {
		path string
		want int
	}{
		{panelLogoPath(digestOf(logoPNGA)), http.StatusOK},
		{panelLogoPath(digestOf(logoJPEGB)), http.StatusNotFound},
		{tapLogoPath(digestOf(logoJPEGB)), http.StatusOK},
		{tapLogoPath(digestOf(logoPNGA)), http.StatusNotFound},
	} {
		if got := ask(t, h, http.MethodGet, c.path, both...); got.status != c.want {
			t.Errorf("GET %s with both cookies = %d, want %d", c.path, got.status, c.want)
		}
	}
}

// ---------------------------------------------------------------------------------
// If-None-Match: 304 for the session's own logo only.
// ---------------------------------------------------------------------------------

// want304 is the Not Modified answer: the validators and the policy headers, no type,
// no file name, no body.
func want304(digest string) answer {
	return answer{
		status: http.StatusNotModified,
		header: http.Header{
			"Cache-Control":                {"private, max-age=31536000, immutable"},
			"Content-Security-Policy":      {"default-src 'none'; sandbox"},
			"Cross-Origin-Resource-Policy": {"same-origin"},
			"Etag":                         {`"` + digest + `"`},
			"X-Content-Type-Options":       {"nosniff"},
		},
		body: []byte{},
	}
}

func TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo(t *testing.T) {
	h := logoRouter(t, newFakeLogoReader(), liveEmployee(logoTenantA), livePanel(logoTenantA))
	own, foreign := digestOf(logoPNGA), digestOf(logoJPEGB)
	ok := answer{status: http.StatusOK, header: wantLogoHeader(own, "image/png", "logo.png", len(logoPNGA)), body: logoPNGA}

	for _, route := range []struct {
		prefix string
		cookie *http.Cookie
	}{{"/t/logo/", employeeCookie()}, {"/admin/brand/logo/", panelCookie()}} {
		cases := []struct {
			name  string
			lines []string
			want  answer
		}{
			{"the own etag", []string{`"` + own + `"`}, want304(own)},
			{"the own etag, weak", []string{`W/"` + own + `"`}, want304(own)},
			{"*", []string{"*"}, want304(own)},
			{"a list ending in the own etag", []string{`"x", W/"y" ,"` + own + `"`}, want304(own)},
			{"the own etag on a second line", []string{`"x"`, `"` + own + `"`}, want304(own)},
			{"another etag", []string{`"` + foreign + `"`}, ok},
			{"the own digest unquoted", []string{own}, ok},
			{"the own digest in upper case", []string{`"` + strings.ToUpper(own) + `"`}, ok},
			{"an unterminated tag before the own etag", []string{`"x, "` + own + `"`}, ok},
			{"empty", []string{""}, ok},
		}
		for _, c := range cases {
			got := askFrom(t, h, "203.0.113.5:41234", http.MethodGet, route.prefix+own,
				http.Header{"If-None-Match": c.lines}, route.cookie)
			if !got.same(c.want) {
				t.Errorf("%s, If-None-Match %q:\n got %s\nwant %s", route.prefix, c.lines, got, c.want)
			}
		}
		// ANOTHER BUSINESS'S DIGEST, named in the header too, or "*": the 404 is
		// byte-identical to the unknown digest's -- the header cannot turn a refusal
		// into a 304, because it is read only after this business's own row was found.
		for _, lines := range [][]string{{`"` + foreign + `"`}, {"*"}, {`W/"` + foreign + `"`}} {
			for _, digest := range []string{foreign, unknownDigest} {
				got := askFrom(t, h, "203.0.113.5:41234", http.MethodGet, route.prefix+digest,
					http.Header{"If-None-Match": lines}, route.cookie)
				if !got.same(wantNotFound()) {
					t.Errorf("%s%s… with If-None-Match %q:\n got %s\nwant the one 404", route.prefix, digest[:8], lines, got)
				}
			}
		}
	}
}

func TestIfNoneMatch_TheScanFollowsRFC9110(t *testing.T) {
	etag := `"abc"`
	cases := []struct {
		lines []string
		want  bool
	}{
		{nil, false},
		{[]string{""}, false},
		{[]string{`"abc"`}, true},
		{[]string{`W/"abc"`}, true},
		{[]string{` "abc" `}, true},
		{[]string{`*`}, true},
		{[]string{`"x","abc"`}, true},
		{[]string{`"x" , W/"abc"`}, true},
		{[]string{`"x"`, `"abc"`}, true},
		{[]string{`,,"abc"`}, true},
		{[]string{`"abcd"`}, false},
		{[]string{`abc`}, false},
		{[]string{`"ABC"`}, false},
		{[]string{`"a"bc"`}, false},
		{[]string{`"x`, `"abc"`}, false},
		{[]string{"\"x\x01\",\"abc\""}, false},
		{[]string{`w/"abc"`}, false},
		// A malformed member ends the scan -- but only what comes AFTER it is unread. A
		// match found before it stands (net/http's checkIfNoneMatch does the same).
		{[]string{`"abc", garbage`}, true},
		{[]string{`*garbage`}, true},
		{[]string{`"abc" junk`}, true},
		{[]string{`"abc"`, `garbage`}, true},
		{[]string{`garbage, "abc"`}, false},
	}
	for _, c := range cases {
		if got := ifNoneMatchNames(c.lines, etag); got != c.want {
			t.Errorf("ifNoneMatchNames(%q) = %v, want %v", c.lines, got, c.want)
		}
	}
}

func TestLogoDigest_OnlySixtyFourLowerCaseHexDigits(t *testing.T) {
	good := digestOf(logoPNGA)
	if !isLogoDigest(good) || !isLogoDigest(strings.Repeat("0", 64)) || !isLogoDigest(strings.Repeat("f", 64)) {
		t.Fatal("a 64-digit lower-case hex digest was refused")
	}
	for _, bad := range []string{
		"", good[:63], good + "0", strings.ToUpper(good), good[:63] + "G", good[:63] + "g",
		good[:63] + " ", good[:63] + "/", good[:62] + "%2", strings.Repeat("\u00e9", 32),
	} {
		if isLogoDigest(bad) {
			t.Errorf("isLogoDigest(%q) = true", bad)
		}
	}
}

func TestLogoFileName_FollowsTheStoredType(t *testing.T) {
	for mime, want := range map[string]string{"image/png": "logo.png", "image/jpeg": "logo.jpg"} {
		if got, ok := logoFileName(mime); !ok || got != want {
			t.Errorf("logoFileName(%q) = %q, %v; want %q", mime, got, ok, want)
		}
	}
	for _, mime := range []string{"", "image/gif", "image/svg+xml", "IMAGE/PNG", "image/png ", "text/html"} {
		if got, ok := logoFileName(mime); ok {
			t.Errorf("logoFileName(%q) = %q; a type outside the two must not be served", mime, got)
		}
	}
}

// A stored type the route cannot name, and a reader error, are answered 500 -- reached
// only after the session's own business was read, so neither says anything about any
// other business.
func TestLogoRoutes_AReadFailureIsNotARefusal(t *testing.T) {
	broken := newFakeLogoReader()
	broken.err = errors.New("connection reset")
	odd := newFakeLogoReader()
	odd.logos[logoTenantA][digestOf(logoPNGA)] = tenant.StoredLogo{Data: logoPNGA, MIME: "image/svg+xml"}
	for name, reader := range map[string]*fakeLogoReader{"a reader error": broken, "an unnamed type": odd} {
		h := logoRouter(t, reader, liveEmployee(logoTenantA), livePanel(logoTenantA))
		for _, c := range []struct {
			path   string
			cookie *http.Cookie
		}{{tapLogoPath(digestOf(logoPNGA)), employeeCookie()}, {panelLogoPath(digestOf(logoPNGA)), panelCookie()}} {
			got := ask(t, h, http.MethodGet, c.path, c.cookie)
			if got.status != http.StatusInternalServerError || bytes.Contains(got.body, logoPNGA) ||
				got.header.Get("Cache-Control") != "no-store" || got.header.Get("Content-Disposition") != "" {
				t.Errorf("%s at %s = %s, want a 500 with no bytes", name, c.path, got)
			}
		}
	}
}

// ---------------------------------------------------------------------------------
// The budgets: a logo request is charged once, to its surface's buckets.
// ---------------------------------------------------------------------------------

// TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget measures, against the budgets the
// product builds (httpx.TapSessionLimit / TapAddressLimit, adminSessionLimit /
// adminFloodLimit -- no limiter built here), that one logo request costs exactly ONE
// charge to its surface's address bucket and ONE to its session bucket, and that the
// buckets are the ones the pages spend: the request after the budget's last is refused
// whether it is a page or a logo.
func TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget(t *testing.T) {
	own := digestOf(logoPNGA)
	const venue, elsewhere = "198.51.100.7:4000", "198.51.100.8:4000"

	t.Run("tap session bucket", func(t *testing.T) {
		h := logoRouter(t, newFakeLogoReader(), liveEmployee(logoTenantA), livePanel(logoTenantA))
		limit := httpx.TapSessionLimit()
		for i := 1; i < limit; i++ {
			if got := askFrom(t, h, venue, http.MethodGet, tapLogoPath(own), nil, employeeCookie()); got.status != http.StatusOK {
				t.Fatalf("logo request %d of %d = %d", i, limit, got.status)
			}
		}
		// The budget's LAST request is the page: one charge per logo request leaves it.
		if got := askFrom(t, h, venue, http.MethodGet, tapURL(), nil, employeeCookie()); got.status != http.StatusOK {
			t.Fatalf("GET /t as request %d = %d, want 200 (the logos charged more than once each)", limit, got.status)
		}
		for _, target := range []string{tapLogoPath(own), tapURL()} {
			if got := askFrom(t, h, venue, http.MethodGet, target, nil, employeeCookie()); got.status != http.StatusTooManyRequests {
				t.Fatalf("%s after %d requests = %d, want 429 from the shared session bucket", target, limit, got.status)
			}
		}
	})

	t.Run("tap address bucket", func(t *testing.T) {
		var verifies atomic.Int32
		sess := &fakeSessions{verify: func() (session.Resolved, error) {
			verifies.Add(1)
			return session.Resolved{ID: logoTapSession, TenantID: logoTenantA, EmployeeID: testEmployee}, nil
		}}
		reader := newFakeLogoReader()
		h := logoRouter(t, reader, sess, livePanel(logoTenantA))
		limit := httpx.TapAddressLimit()
		// ANONYMOUS logo requests from a venue's address: 404, and charged -- and
		// without any session resolution or read behind them.
		for i := 1; i <= limit; i++ {
			if got := askFrom(t, h, venue, http.MethodGet, tapLogoPath(own), nil); got.status != http.StatusNotFound {
				t.Fatalf("anonymous logo request %d of %d = %d, want 404", i, limit, got.status)
			}
		}
		if n, calls := verifies.Load(), reader.callLog(); n != 0 || len(calls) != 0 {
			t.Fatalf("%d anonymous logo requests resolved %d session(s) and made %d read(s); want 0 and 0",
				limit, n, len(calls))
		}
		if got := askFrom(t, h, venue, http.MethodGet, tapURL(), nil, employeeCookie()); got.status != http.StatusTooManyRequests {
			t.Fatalf("a real tap from the same address after %d anonymous logo requests = %d, want 429 "+
				"(the address bucket the page spends)", limit, got.status)
		}
		if got := askFrom(t, h, elsewhere, http.MethodGet, tapURL(), nil, employeeCookie()); got.status != http.StatusOK {
			t.Fatalf("control: the same session from another address = %d, want 200", got.status)
		}
	})

	t.Run("panel session bucket", func(t *testing.T) {
		h := logoRouter(t, newFakeLogoReader(), liveEmployee(logoTenantA), livePanel(logoTenantA))
		for i := 1; i < adminSessionLimit; i++ {
			if got := askFrom(t, h, venue, http.MethodGet, panelLogoPath(own), nil, panelCookie()); got.status != http.StatusOK {
				t.Fatalf("panel logo request %d of %d = %d", i, adminSessionLimit, got.status)
			}
		}
		if got := askFrom(t, h, venue, http.MethodGet, transactionsHref, nil, panelCookie()); got.status != http.StatusOK {
			t.Fatalf("GET %s as request %d = %d, want 200", transactionsHref, adminSessionLimit, got.status)
		}
		for _, target := range []string{panelLogoPath(own), transactionsHref} {
			if got := askFrom(t, h, venue, http.MethodGet, target, nil, panelCookie()); got.status != http.StatusTooManyRequests {
				t.Fatalf("%s after %d requests = %d, want 429 from the shared session bucket", target, adminSessionLimit, got.status)
			}
		}
	})

	t.Run("panel address bucket", func(t *testing.T) {
		h := logoRouter(t, newFakeLogoReader(), liveEmployee(logoTenantA), livePanel(logoTenantA))
		for i := 1; i <= adminFloodLimit; i++ {
			if got := askFrom(t, h, venue, http.MethodGet, panelLogoPath(own), nil); got.status != http.StatusSeeOther {
				t.Fatalf("anonymous panel logo request %d of %d = %d, want 303", i, adminFloodLimit, got.status)
			}
		}
		if got := askFrom(t, h, venue, http.MethodGet, transactionsHref, nil, panelCookie()); got.status != http.StatusTooManyRequests {
			t.Fatalf("a signed-in page from the same address after %d anonymous logo requests = %d, want 429",
				adminFloodLimit, got.status)
		}
		if got := askFrom(t, h, elsewhere, http.MethodGet, transactionsHref, nil, panelCookie()); got.status != http.StatusOK {
			t.Fatalf("control: the same session from another address = %d, want 200", got.status)
		}
	})
}

func TestNewBrandLogos_RefusesAMissingDependency(t *testing.T) {
	tp, panel, _ := logoSurfaces(t, newFakeLogoReader(), liveEmployee(logoTenantA), livePanel(logoTenantA))
	var typedNil *fakeLogoReader
	for name, build := range map[string]func() (*BrandLogos, error){
		"nil reader":       func() (*BrandLogos, error) { return NewBrandLogos(nil, panel, tp, nil) },
		"typed-nil reader": func() (*BrandLogos, error) { return NewBrandLogos(typedNil, panel, tp, nil) },
		"nil panel":        func() (*BrandLogos, error) { return NewBrandLogos(newFakeLogoReader(), nil, tp, nil) },
		"nil tap":          func() (*BrandLogos, error) { return NewBrandLogos(newFakeLogoReader(), panel, nil, nil) },
	} {
		if b, err := build(); err == nil || b != nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// ---------------------------------------------------------------------------------
// The page half: img-src is named only by a response that draws an image.
// ---------------------------------------------------------------------------------

// TestPagePolicies_ALogoWidensByImgSrcAlone pins the two functions against LITERALS:
// with hasLogo false each is today's policy byte for byte, with true it is that plus
// one directive. A literal and not the constant, so a change to the constant is also a
// change this test sees.
func TestPagePolicies_ALogoWidensByImgSrcAlone(t *testing.T) {
	const tapToday = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; " +
		"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	const adminToday = "default-src 'none'; style-src 'self'; font-src 'self'; " +
		"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	const scriptedToday = adminToday + "; script-src 'self'; connect-src 'self'"
	const widening = "; img-src 'self'"
	for _, c := range []struct {
		name      string
		got, want string
	}{
		{"tapCSPFor(false)", tapCSPFor(false), tapToday},
		{"tapCSPFor(true)", tapCSPFor(true), tapToday + widening},
		{"adminCSPFor(false)", adminCSPFor(false), adminToday},
		{"adminCSPFor(true)", adminCSPFor(true), adminToday + widening},
		{"logoImagePolicy(adminScriptedCSP, false)", logoImagePolicy(adminScriptedCSP, false), scriptedToday},
		{"logoImagePolicy(adminScriptedCSP, true)", logoImagePolicy(adminScriptedCSP, true), scriptedToday + widening},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

var imgElementRE = regexp.MustCompile(`(?i)<img[\s/>]`)

// namesImgSrc reports whether a policy carries an img-src directive.
func namesImgSrc(policy string) bool {
	for _, d := range strings.Split(policy, ";") {
		if f := strings.Fields(d); len(f) > 0 && strings.EqualFold(f[0], "img-src") {
			return true
		}
	}
	return false
}

// imagePolicyAgrees is the correspondence: img-src named IF AND ONLY IF the body has an
// <img>.
func imagePolicyAgrees(body, policy string) bool {
	return imgElementRE.MatchString(body) == namesImgSrc(policy)
}

// TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage is ADR 0023 §4's rule over the
// panel's and the tap surface's renders: a response's policy names img-src if and only
// if its body contains an <img>. Since WL-8 the panel chrome draws the business's logo,
// so the corpus holds both halves: every section of a business with a logo (the
// responses that draw it and must name img-src -- counted below, the TRUE half) and of
// businesses without one. A page that names it without drawing an image, or draws one
// without naming it, turns this red. Since WL-9 the tap and result screens draw it
// too, and the corpus counts their two logo-bearing renders in the TRUE half.
//
// THE CORPUS IS LISTED, NOT DERIVED, AND THE LIST IS PRINTED: every row of
// pages.PanelSections, unbranded and under three brands, plus the named panel and tap
// renders below. A render that is not in it is not covered -- PART III of
// brandlogo.go's claim.
func TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage(t *testing.T) {
	// THE PREDICATE CAN FAIL, both ways (anti-vacuity: a predicate that always said
	// "agrees" would pass every render below; the corpus's TRUE half is counted at the
	// end).
	if imagePolicyAgrees(`<img src="/t/logo/x" alt="">`, tapCSPFor(false)) ||
		imagePolicyAgrees(`<p>no image</p>`, tapCSPFor(true)) ||
		imagePolicyAgrees(`<IMG/src=x>`, adminCSPFor(false)) ||
		!imagePolicyAgrees(`<img src="/admin/brand/logo/x" alt="">`, adminCSPFor(true)) ||
		!imagePolicyAgrees(`<p>no image</p>`, adminCSPFor(false)) {
		t.Fatal("the correspondence predicate does not separate the four cases")
	}

	type render struct {
		name string
		a    answer
	}
	var corpus []render
	add := func(name string, a answer) {
		t.Helper()
		if len(a.body) == 0 {
			t.Fatalf("%s rendered an empty body; it cannot be what this test thinks it is", name)
		}
		corpus = append(corpus, render{name, a})
	}

	// PANEL: every section, signed in, through the real router.
	assertSectionTableIsUsable(t)
	b := panelBrowser(t)
	read := func(rec *httptest.ResponseRecorder) answer {
		res := rec.Result()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("reading a panel answer: %v", err)
		}
		return answer{res.StatusCode, res.Header, body}
	}
	for _, s := range pages.PanelSections {
		add("panel GET "+s.Href, read(b.do(http.MethodGet, s.Href, nil)))
	}
	// PANEL, BRANDED (WL-8): every section under three brands. The two with a logo draw
	// it on every section; the accent alone draws no image.
	brands := newFakeBrands()
	branded, _ := brandedPanel(t, brands)
	logoRenders := 0
	for _, v := range []struct {
		name         string
		accent, logo bool
	}{{"accent and logo", true, true}, {"accent alone", true, false}, {"logo alone", false, true}} {
		brands.set(wl8Brand(t, v.accent, v.logo), nil)
		for _, s := range pages.PanelSections {
			add("panel GET "+s.Href+", branded with "+v.name, read(branded.do(http.MethodGet, s.Href, nil)))
			if v.logo {
				logoRenders++
			}
		}
	}
	// M10 WL-7: the Account section re-rendered for a refused colour -- the editor's other
	// render path -- with a logo (the preview draws a second <img>) and without one.
	for _, withLogo := range []bool{true, false} {
		bp := newBrandPanel(t, "owner", panelTestTenant)
		bp.brands.set(wl8Brand(t, true, withLogo), nil)
		add("panel POST "+brandAccentHref+", refused colour, logo "+strconv.FormatBool(withLogo),
			read(bp.browser(t).do(http.MethodPost, brandAccentHref, url.Values{"accent_hex": {"808080"}})))
		if withLogo {
			logoRenders++
		}
	}
	withRecords := panelBrowserWith(t, ledgerWithRecords(1, true))
	add("panel GET "+transactionsHref+" with a record", read(withRecords.do(http.MethodGet, transactionsHref, nil)))
	add("panel GET "+docketFragmentPath+" with a record", read(withRecords.do(http.MethodGet, docketFragmentPath, nil)))
	anon := newBrowser(t, newAdminRouter(t, &fakeAdmins{}, &fakeTrail{}))
	add("panel GET "+adminLoginPath, read(anon.do(http.MethodGet, adminLoginPath, nil)))
	failing := newFakeLedger()
	failing.err = errors.New("connection reset")
	fb := newBrowser(t, newAdminRouterWithLedger(t, livePanel(panelTestTenant), &fakeTrail{}, failing))
	fb.cookies[adminauth.CookieName] = panelCookie().Value
	add("panel GET "+transactionsHref+" with the ledger down", read(fb.do(http.MethodGet, transactionsHref, nil)))

	// TAP: the page, its failure screens, the confirmation screen per verdict and
	// practice, the POST failures and the 429 -- the renders
	// TestTapResponses_CarryTheContentSecurityPolicy drives, plus the verdict sweep.
	h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	add("tap GET /t", ask(t, h, http.MethodGet, tapURL(), sessionCookie()))
	add("tap GET /t, malformed url", ask(t, h, http.MethodGet, "/t?tag=nonsense", sessionCookie()))
	hu, _ := newTapHandler(t, &fakePreviewer{err: sun.ErrUnknownTag}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	add("tap GET /t, unknown plaque", ask(t, hu, http.MethodGet, tapURL(), sessionCookie()))
	he, _ := newTapHandler(t, &fakePreviewer{err: errTapProbe}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	add("tap GET /t, server error", ask(t, he, http.MethodGet, tapURL(), sessionCookie()))
	postAnswer := func(svc *fakeCheckins) answer {
		t.Helper()
		hc, tp := newCheckinHandler(t, svc, fixedSessions())
		form := "ctx=" + mintedContext(t, tp, tapFixedSessionID, tapContext{
			UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
			TagTenantID: testTenant, LocationID: tapLocation,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.5:41234"
		req.AddCookie(sessionCookie())
		rec := httptest.NewRecorder()
		hc.ServeHTTP(rec, req)
		return read(rec)
	}
	for _, verdict := range []string{"ok", "flag", "reject", "ignored"} {
		for _, practice := range []bool{false, true} {
			add("tap POST, "+verdict+", practice "+strconv.FormatBool(practice), postAnswer(&fakeCheckins{result: checkin.Result{
				Outcome:  checkin.OutcomeRecorded,
				Decision: decisionOf(verdict, "in", practice),
			}}))
		}
	}
	add("tap POST, unknown plaque", postAnswer(&fakeCheckins{err: checkin.ErrUnknownTag}))
	add("tap POST, another employer's plaque", postAnswer(&fakeCheckins{result: checkin.Result{Outcome: checkin.OutcomeForeignTenant}}))
	hs, _ := newCheckinHandler(t, &fakeCheckins{}, fixedSessions())
	stale := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader("ctx=not-a-context"))
	stale.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	stale.RemoteAddr = "203.0.113.5:41234"
	stale.AddCookie(sessionCookie())
	staleRec := httptest.NewRecorder()
	hs.ServeHTTP(staleRec, stale)
	add("tap POST, stale context", read(staleRec))
	_, tp := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	limited := httptest.NewRecorder()
	tp.renderTooManyRequests(limited, httptest.NewRequest(http.MethodGet, tapURL(), nil))
	add("tap 429", read(limited))
	// M10 WL-9: renders of a business WITH a brand -- the first on the tap surface
	// that draw an <img> (limit 2 of the WL-6 note, the tap half).
	add("tap GET /t, logo and accent", tapAnswer(t, &fakeDirectory{facts: brandedFacts(t, true, true)}))
	add("tap GET /t, accent and no logo", tapAnswer(t, &fakeDirectory{facts: brandedFacts(t, false, true)}))
	add("tap GET /t, another business's plaque, own brand set", tapAnswer(t, &fakeDirectory{
		facts: tenant.TapPageFacts{EmployeeName: "Maria Borg", Brand: testPageBrand(t, true, true)}, err: tenant.ErrForeignLocation}))
	add("tap POST, ok, logo and accent", resultAnswer(t, &fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, true, true)},
		okResult(), testTenant, tapLocation))
	add("tap POST, ok, accent and no logo", resultAnswer(t, &fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, false, true)},
		okResult(), testTenant, tapLocation))
	// Of these five, the two with a logo on the business's own plaque draw it.
	logoRenders += 2

	names := make([]string, 0, len(corpus))
	drawn := 0
	for _, r := range corpus {
		names = append(names, r.name)
		if imgElementRE.Match(r.a.body) {
			drawn++
		}
		policy := r.a.header.Get("Content-Security-Policy")
		if policy == "" {
			t.Errorf("%s (%d) carries no Content-Security-Policy", r.name, r.a.status)
			continue
		}
		if !imagePolicyAgrees(string(r.a.body), policy) {
			t.Errorf("%s (%d): body has <img>: %v, policy names img-src: %v.\npolicy: %q\n"+
				"A page that names img-src without drawing an image widens its policy for nothing; "+
				"one that draws an image its policy forbids is broken in the browser.",
				r.name, r.a.status, imgElementRE.MatchString(string(r.a.body)), namesImgSrc(policy), policy)
		}
	}
	// THE TRUE HALF IS MEASURED (WL-8): exactly the logo-bearing renders draw an <img>,
	// so the correspondence above was checked on responses that name img-src too, and
	// not only on its absence.
	if drawn != logoRenders || logoRenders == 0 {
		t.Errorf("%d renders draw an <img>; the corpus holds %d logo-bearing renders (panel and tap)", drawn, logoRenders)
	}
	t.Logf("img-src correspondence over %d renders (%d panel sections; %d draw the logo): %s",
		len(corpus), len(pages.PanelSections), drawn, strings.Join(names, "; "))
}
