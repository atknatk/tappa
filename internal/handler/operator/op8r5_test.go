package operator_test

// op8r5_test.go -- OP-8's fifth round: the shared runner of the header tables, the class
// -> route table (classRoutes) both header tables are held to, the eight classes the 4th
// audit found outside the 40 (N7), the walked-routes completeness test (N5) and the wire
// test of the recorder's snapshot (F1 (b)).
//
// WHAT IS MEASURED -- the response headers AT WriteHeader: httptest.ResponseRecorder's
// Result().Header is the snapshot it takes when the status line is written. net/http's
// ResponseWriter.Header documents that changing the map after WriteHeader does not change
// the header section, with two exceptions: 1xx informational responses, and trailers (a
// key with http.TrailerPrefix, or one the Trailer header announced, set after
// WriteHeader is sent as a trailer -- the 5th audit's T01/T02; the recorder puts those in
// Result().Trailer, which no test here reads). The 4th audit's H01 and L01 changed the
// live map after WriteHeader and were green while the tests read w.Header(); the tests
// here and in op8r2_test.go read Result().Header, and
// TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries measures, on three classes
// (C1, C18, C28), that this snapshot equals the HEADER SECTION a real server sends as
// net/http's client parses it, Content-Length and Date aside (serverAdded).

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// headerClass is one response class of the header tables: its name ("C<n> …"), its
// status and the drive that produces it.
type headerClass struct {
	name   string
	status int
	drive  func() *httptest.ResponseRecorder
}

// driveLog is the method and path of the last request a class's drive sent.
type driveLog struct{ method, path string }

// classRoute is a class's last request, as a chi route (the pattern chi.Walk reports),
// and its status.
type classRoute struct {
	method, pattern string
	status          int
}

// classRoutes maps each class of the two header tables to its route and status. The
// header tests hold each drive to its entry (the method and path it sent last, the status
// it got); TestOperatorHeaders_TheWalkedRoutesEachHaveAClass holds the entries to the
// routes the surface mounts.
var classRoutes = map[string]classRoute{
	"C1": {"GET", "/operator/login", 200}, "C2": {"POST", "/operator/login", 401}, "C3": {"POST", "/operator/login", 303},
	"C4": {"POST", "/operator/login", 429}, "C5": {"POST", "/operator/login", 503}, "C6": {"POST", "/operator/login", 403},
	"C7": {"POST", "/operator/login", 413}, "C8": {"POST", "/operator/login", 400},
	"C9": {"GET", "/operator/login/totp", 303}, "C10": {"GET", "/operator/login/totp", 200},
	"C11": {"POST", "/operator/login/totp", 303}, "C12": {"POST", "/operator/login/totp", 303},
	"C13": {"POST", "/operator/login/totp", 401}, "C14": {"POST", "/operator/login/totp", 401},
	"C15": {"POST", "/operator/login/totp", 401}, "C16": {"POST", "/operator/login/totp", 429},
	"C17": {"POST", "/operator/login/totp", 503},
	"C18": {"GET", "/operator/enroll", 200}, "C19": {"GET", "/operator/enroll", 400},
	"C20": {"POST", "/operator/enroll", 400}, "C21": {"POST", "/operator/enroll", 400}, "C22": {"POST", "/operator/enroll", 401},
	"C23": {"POST", "/operator/enroll", 400}, "C24": {"POST", "/operator/enroll", 429}, "C25": {"POST", "/operator/enroll", 503},
	"C26": {"POST", "/operator/enroll", 303},
	"C27": {"GET", "/operator/", 200}, "C28": {"GET", "/operator/", 303}, "C29": {"GET", "/operator/", 303},
	"C30": {"GET", "/operator/", 303}, "C31": {"GET", "/operator/", 429}, "C32": {"GET", "/operator/", 303},
	"C33": {"POST", "/operator/logout", 303}, "C34": {"POST", "/operator/logout", 303}, "C35": {"POST", "/operator/logout", 503},
	"C36": {"POST", "/operator/logout", 429}, "C37": {"POST", "/operator/logout", 403},
	"C38": {"GET", "/operator/login", 429}, "C39": {"GET", "/operator/no-such", 404}, "C40": {"PUT", "/operator/login", 405},
	"C41": {"HEAD", "/operator/", 405}, "C42": {"POST", "/operator/", 405}, "C43": {"OPTIONS", "/operator/", 405},
	"C44": {"PUT", "/operator/login/totp", 405}, "C45": {"PUT", "/operator/enroll", 405}, "C46": {"GET", "/operator/logout", 405},
	"C47": {"POST", "/operator/login/totp", 413}, "C48": {"POST", "/operator/enroll", 413},
}

// routeOf is a request's method and path as a chi route: the query dropped, and the
// prefix itself written as the pattern chi.Walk reports for the console ("/operator/").
func routeOf(l driveLog) string {
	p, _, _ := strings.Cut(l.path, "?")
	if p == operator.Prefix {
		p += "/"
	}
	return l.method + " " + p
}

// runHeaderClasses drives each class and holds it to classRoutes (its last request and
// its status), to designedHeaders (checkDesignedHeaders, on the snapshot at WriteHeader)
// and to the script/policy pairing; it returns the IDs of the classes whose body loads a
// script, in order.
func runHeaderClasses(t *testing.T, classes []headerClass, last *driveLog) []string {
	t.Helper()
	var scripted []string
	for _, c := range classes {
		*last = driveLog{}
		w := c.drive()
		id, _, _ := strings.Cut(c.name, " ")
		route, ok := classRoutes[id]
		if !ok {
			t.Fatalf("PREMISE: %s has no entry in classRoutes", c.name)
		}
		if w.Code != c.status || c.status != route.status {
			t.Errorf("%s: %d, want %d (classRoutes: %d) -- the class is not reached", c.name, w.Code, c.status, route.status)
			continue
		}
		if got, want := routeOf(*last), route.method+" "+route.pattern; got != want {
			t.Errorf("%s: the drive's last request is %s, classRoutes says %s", c.name, got, want)
		}
		d, ok := designedHeaders[id]
		if !ok {
			t.Fatalf("PREMISE: %s has no designed headers", c.name)
		}
		checkDesignedHeaders(t.Errorf, c.name, w, d)
		want := policyBase
		if strings.Contains(w.Body.String(), "<script") {
			want = policyEnroll
			scripted = append(scripted, id)
		}
		if got := w.Result().Header.Values("Content-Security-Policy"); len(got) != 1 || got[0] != want {
			t.Errorf("%s: CSP %q, want %q (a body that loads a script needs the enrollment policy; no other body may have it)", c.name, got, want)
		}
	}
	return scripted
}

// TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy drives the eight
// classes C41-C48 the 4th audit found outside the 40 (N7): HEAD, POST and OPTIONS on
// /operator, PUT on /operator/login/totp and /operator/enroll, GET on /operator/logout
// (each 405), and an oversized form on the code step and on the enrollment (each 413) --
// with the 40-class test's hostile drive and checks (runHeaderClasses).
//
// PART I -- measured on these eight, at WriteHeader: the designed status, route, header
// names and values (designedHeaders); no hostile value (hostileValues) in a header value or
// in the body, raw or query-escaped -- the hostile query rides on the last request of the
// four whose path is the code step or the enrollment (C44, C45, C47, C48); no script; 0
// store calls on the six 405s.
//
// PART II -- the list above, on these eight classes.
//
// PART III -- This test measures the eight classes and the raw and query-escaped forms
// only; anything else (examples: a class a later handler adds, a hostile value echoed
// HTML-escaped or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	ad := &addrs{}
	f := g.active()
	last := &driveLog{}
	send := func(r req) *httptest.ResponseRecorder {
		if r.remote == "" {
			r.remote = ad.next() + ":1"
		}
		r.host = opHost
		*last = driveLog{r.method, r.path}
		return g.do(hostileDrive(r))
	}
	sfs := map[string]string{"Sec-Fetch-Site": "same-origin"}
	big := strings.Repeat("9", 20<<10)
	before := g.store.total()
	method := func(m, path string) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			r := req{method: m, path: path, header: sfs}
			if m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions {
				r = req{method: m, path: path, origin: opOrigin, form: url.Values{}}
			}
			return send(r)
		}
	}
	wrongMethods := []headerClass{
		{"C41 HEAD on the console", 405, method(http.MethodHead, "/operator")},
		{"C42 POST on the console", 405, method(http.MethodPost, "/operator")},
		{"C43 OPTIONS on the console", 405, method(http.MethodOptions, "/operator")},
		{"C44 PUT on the code step", 405, method(http.MethodPut, "/operator/login/totp")},
		{"C45 PUT on the enrollment", 405, method(http.MethodPut, "/operator/enroll")},
		{"C46 GET on the sign-out", 405, method(http.MethodGet, "/operator/logout")},
	}
	scripted := runHeaderClasses(t, wrongMethods, last)
	if n := g.store.total() - before; n != 0 {
		t.Errorf("the six 405 classes made %d store call(s), want 0", n)
	}
	oversized := []headerClass{
		{"C47 code step, oversized form", 413, func() *httptest.ResponseRecorder {
			w := send(req{method: http.MethodPost, path: "/operator/login", origin: opOrigin,
				form: url.Values{"email": {f.email}, "password": {f.password}}})
			ch := cookie(w, operatorauth.ChallengeCookieName)
			if ch == nil {
				t.Fatal("PREMISE: no challenge")
			}
			return send(req{method: http.MethodPost, path: "/operator/login/totp", origin: opOrigin,
				form: url.Values{"code": {big}}, cookies: []*http.Cookie{ch}})
		}},
		{"C48 enrollment, oversized form", 413, func() *httptest.ResponseRecorder {
			return send(req{method: http.MethodPost, path: "/operator/enroll", origin: opOrigin,
				form: url.Values{"id": {uuid.New().String()}, "password": {big}}})
		}},
	}
	scripted = append(scripted, runHeaderClasses(t, oversized, last)...)
	for i, c := range append(wrongMethods, oversized...) {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(41+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 41+i, c.name, 41+i)
		}
	}
	if len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
}

// TestOperatorHeaders_TheWalkedRoutesEachHaveAClass is the completeness check the 4th
// audit asked for (N5), on the surface's MOUNTED routes as chi.Walk reports them.
//
// PART I -- the shipped surface: chi.Walk reports eight method x route pairs on five
// routes; classRoutes has, for each pair, a class with a status other than 405, and for
// each route a 405 class with a method not mounted on it; classRoutes' keys are C1-C48.
//
// PART II -- red on: a walked pair with no class; a walked route with no 405 class; a key
// of classRoutes outside C1-C48 or a missing one. (The header tests hold each class's
// drive to its classRoutes entry: an entry naming a route its class does not drive is red
// there -- N5c in the card's 5th-round table is the converse.)
//
// PART III -- This test checks the routes chi.Walk reports only; a response a route can
// give that no class drives (examples: a new branch inside a handler) is code review's --
// no completeness claim.
func TestOperatorHeaders_TheWalkedRoutesEachHaveAClass(t *testing.T) {
	g := newRig(t)
	r := chi.NewRouter()
	g.surface.Mount(r)
	walked := map[string][]string{}
	if err := chi.Walk(r, func(m, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		walked[route] = append(walked[route], m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pairs := 0
	for _, ms := range walked {
		pairs += len(ms)
	}
	if len(walked) < 5 || pairs < 8 {
		t.Fatalf("PREMISE: chi.Walk reported %d route(s), %d pair(s)", len(walked), pairs)
	}
	covered, wrongMethod := map[string]bool{}, map[string]bool{}
	for _, c := range classRoutes {
		if c.status == http.StatusMethodNotAllowed {
			if ms, ok := walked[c.pattern]; ok && !slices.Contains(ms, c.method) {
				wrongMethod[c.pattern] = true
			}
			continue
		}
		covered[c.method+" "+c.pattern] = true
	}
	var missing []string
	for route, ms := range walked {
		for _, m := range ms {
			if !covered[m+" "+route] {
				missing = append(missing, m+" "+route+": no class")
			}
		}
		if !wrongMethod[route] {
			missing = append(missing, route+": no 405 class")
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Error(m)
	}
	for i := 1; i <= 48; i++ {
		if _, ok := classRoutes["C"+strconv.Itoa(i)]; !ok {
			t.Errorf("classRoutes has no C%d", i)
		}
	}
	if len(classRoutes) != 48 {
		t.Errorf("classRoutes has %d entries, want C1-C48", len(classRoutes))
	}
}

// serverAdded: of the headers net/http's server adds to the three classes' header
// sections (the handler sets none of them), the two that the client's parse keeps in
// Response.Header; the wire test drops them by name. The server's other framing headers
// are outside the comparison without being listed here, measured (5th audit and a scratch
// probe, 2026-10-02): Transfer-Encoding (chunked on C18) is moved by net/http's client
// parser into Response.TransferEncoding, and Connection is not written for these HTTP/1.1
// keep-alive requests (the wire carries it for a Connection: close request, C1, and an
// HTTP/1.0 keep-alive one, C28).
var serverAdded = []string{"Content-Length", "Date"}

// TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries compares the object the
// header tests read -- the recorder's Result().Header, the snapshot at WriteHeader -- with
// the header section a real server sends, on three classes (4th audit, F1 (b)).
//
// PART I -- on three classes (C1 the sign-in page, C18 the enrollment page, C28 the
// console's 303 without a cookie) sent through a real httptest.Server by net/http's
// client (HTTP/1.1, keep-alive), the status is the class's and the header section as the
// client parses it (Response.Header) equals the recorder's Result().Header for the same
// request, with Content-Length and Date left out by name (serverAdded). CONTROL: a
// handler that sets a header and deletes it after WriteHeader shows it on the wire and in
// Result().Header and not in the live w.Header().
//
// PART II -- the three classes above.
//
// PART III -- This test compares the three classes' header sections only; anything else
// (examples: another class's wire such as the 303s that set cookies, the trailer section,
// a hijacked connection -- the recorder does not support Hijack -- and the server's
// framing headers Transfer-Encoding and Connection) is code review's -- no completeness
// claim.
func TestOperatorHeaders_TheRecorderSnapshotIsWhatTheWireCarries(t *testing.T) {
	g := newRig(t)
	srv := httptest.NewServer(g.h)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	wire := func(h http.Handler, path string, header map[string]string) (int, http.Header) {
		t.Helper()
		s := srv
		if h != nil {
			s = httptest.NewServer(h)
			defer s.Close()
		}
		r, err := http.NewRequest(http.MethodGet, s.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = opHost
		for k, v := range header {
			r.Header.Set(k, v)
		}
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		for _, k := range serverAdded {
			res.Header.Del(k)
		}
		return res.StatusCode, res.Header
	}
	same := func(a, b http.Header) bool {
		if len(a) != len(b) {
			return false
		}
		for k, vs := range a {
			if strings.Join(vs, "\n") != strings.Join(b.Values(k), "\n") {
				return false
			}
		}
		return true
	}
	sfs := map[string]string{"Sec-Fetch-Site": "same-origin"}
	for _, c := range []struct{ class, path string }{
		{"C1", "/operator/login"},
		{"C18", "/operator/enroll?id=" + uuid.New().String()},
		{"C28", "/operator"},
	} {
		status, fromWire := wire(nil, c.path, sfs)
		res := g.do(req{method: http.MethodGet, host: opHost, path: c.path, header: sfs, remote: "198.51.100.40:1"}).Result()
		rec := res.Header
		if status != res.StatusCode || status != classRoutes[c.class].status {
			t.Errorf("%s: the wire's status %d, the recorder's %d, classRoutes' %d", c.class, status, res.StatusCode, classRoutes[c.class].status)
		}
		if !same(fromWire, rec) {
			t.Errorf("%s: the wire's headers %v differ from the recorder's snapshot %v", c.class, fromWire, rec)
		}
		if len(fromWire) < 4 {
			t.Errorf("PREMISE: %s: %d header(s) on the wire", c.class, len(fromWire))
		}
	}
	// CONTROL: a header changed after WriteHeader.
	late := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Late", "on-the-wire")
		w.WriteHeader(http.StatusOK)
		delete(w.Header(), "X-Late")
	})
	rec := httptest.NewRecorder()
	late.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	_, fromWire := wire(late, "/", nil)
	if fromWire.Get("X-Late") != "on-the-wire" || rec.Result().Header.Get("X-Late") != "on-the-wire" || rec.Header().Get("X-Late") != "" {
		t.Fatalf("CONTROL: wire %q, snapshot %q, live map %q -- want the first two set and the live map empty",
			fromWire.Get("X-Late"), rec.Result().Header.Get("X-Late"), rec.Header().Get("X-Late"))
	}
}
