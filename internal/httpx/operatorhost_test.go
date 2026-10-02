package httpx_test

import (
	"bufio"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// TestOnHost_ReducesTheRequestHostToTheConfiguredSpelling measures OnHost on the rows
// below: a port is dropped, ONE trailing dot is dropped, case is ignored; the rows marked
// false are hosts the gate must treat as NOT the operator's, and OnHost answers false on
// each. (internal/handler/operator's TestHostGate_TheListedHostReadsOccurOnlyThroughOnHost
// catches its list for the operator half's call; the customer half's call is
// operatorHostOnly's code, read by review.)
func TestOnHost_ReducesTheRequestHostToTheConfiguredSpelling(t *testing.T) {
	const host = "ops.taptime.mt"
	for _, c := range []struct {
		reqHost string
		want    bool
	}{
		{"ops.taptime.mt", true},
		{"ops.taptime.mt:443", true},
		{"ops.taptime.mt:8080", true},
		{"ops.taptime.mt:", true},
		{"OPS.TAPTIME.MT", true},
		{"Ops.Taptime.Mt:443", true},
		{"ops.taptime.mt.", true},
		{"ops.taptime.mt.:443", true},
		{"ops.taptime.mt..", false},
		{"ops.taptime.mt.evil", false},
		{"evil.ops.taptime.mt", false},
		{"xops.taptime.mt", false},
		{"ops-taptime.mt", false},
		{"taptime.mt", false},
		{"www.taptime.mt", false},
		{"tappa.everva.com.tr", false},
		{"", false},
		{"[::1]:8080", false},
		{"127.0.0.1", false},
		{"ops.taptime.mt@evil", false},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = c.reqHost
		if got := httpx.OnHost(r, host); got != c.want {
			t.Errorf("OnHost(%q, %q) = %v, want %v", c.reqHost, host, got, c.want)
		}
	}
	// An unconfigured (empty) host does not match an empty request host.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = ""
	if httpx.OnHost(r, "") {
		t.Error("an empty configured host matched an empty request host")
	}
	// Development's shape: a single-label-plus-localhost host with a port.
	r.Host = "ops.localhost:8080"
	if !httpx.OnHost(r, "ops.localhost") {
		t.Error("ops.localhost:8080 is not ops.localhost")
	}
}

// stubFeatures stands in for both sides of the router: customer routes that answer
// "customer", and an operator stub on httpx.OperatorPrefix that answers "operator" (it
// has NO host gate of its own -- the operator half of the gate is
// internal/handler/operator's; this measures the customer half alone).
type stubFeatures struct{}

func (stubFeatures) Mount(r chi.Router) {
	cust := func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "customer") }
	r.Get("/", cust)
	r.Get("/admin", cust)
	r.Get("/admin/*", cust)
	r.Get("/t", cust)
	r.Post("/api/checkin", cust)
	r.Get("/legal/{slug}", cust)
	op := func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "operator") }
	r.Handle(httpx.OperatorPrefix, http.HandlerFunc(op))
	r.Handle(httpx.OperatorPrefix+"/*", http.HandlerFunc(op))
}

// rawServe parses raw as net/http's server parses a request line and headers
// (http.ReadRequest: an absolute-URI request line sets Host from the URI), and serves it.
func rawServe(t *testing.T, h http.Handler, raw string) *httptest.ResponseRecorder {
	t.Helper()
	r, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	r.RemoteAddr = "192.0.2.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// outcome names where a request landed.
func outcome(w *httptest.ResponseRecorder) string {
	switch b := w.Body.String(); {
	case w.Code == http.StatusOK && b == "customer":
		return "customer"
	case w.Code == http.StatusOK && b == "operator":
		return "operator"
	case w.Code == http.StatusOK:
		return "static"
	case w.Code == http.StatusSeeOther:
		return "303 " + w.Result().Header.Get("Location")
	case w.Code == http.StatusNotFound:
		return "404"
	default:
		return http.StatusText(w.Code)
	}
}

// TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide is the CUSTOMER half of the
// two-way host gate (ADR 0020 §4; M10 OP-8), through the shipped router: on the operator
// host, the rows below serve the operator prefix and the static assets, send the bare
// root to the operator's front door, and answer the customer route rows with 404. Each
// row is a spelling somebody could try to reach a customer page on the operator host --
// or to make the gate mistake one host for the other; the name's "every" is these rows.
// CONTROL: the same customer routes answer on the customer host, and with no operator
// host configured the gate is not mounted.
func TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide(t *testing.T) {
	h := httpx.NewRouter(&config.Config{OperatorHost: "ops.taptime.mt"}, nil, stubFeatures{})
	for _, c := range []struct {
		name, raw, want string
	}{
		// The operator host.
		{"the prefix", "GET /operator HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "operator"},
		{"the prefix, trailing slash", "GET /operator/ HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "operator"},
		{"under the prefix", "GET /operator/login HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "operator"},
		{"a static asset", "GET /static/css/app.css HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "static"},
		{"the bare root", "GET / HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "303 /operator"},
		{"the bare root, HEAD", "HEAD / HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "303 /operator"},
		{"the bare root, POST", "POST / HTTP/1.1\r\nHost: ops.taptime.mt\r\nContent-Length: 0\r\n\r\n", "404"},
		{"the panel", "GET /admin HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"under the panel", "GET /admin/login HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"the tap", "GET /t?tag=04AABBCCDDEE80 HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"a check-in POST", "POST /api/checkin HTTP/1.1\r\nHost: ops.taptime.mt\r\nContent-Length: 0\r\n\r\n", "404"},
		{"a legal page", "GET /legal/cookies HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"the liveness probe", "GET /healthz HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"a look-alike prefix", "GET /operatorx HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"the prefix in capitals", "GET /OPERATOR/login HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"a dot-dot under the prefix", "GET /operator/../admin HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "operator"},
		{"a dot-dot under static", "GET /static/../admin HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"an escaped letter", "GET /%6Fperator/login HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"an escaped slash", "GET /operator%2Flogin HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"a doubled slash", "GET //operator/login HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"the host in capitals", "GET /admin HTTP/1.1\r\nHost: OPS.TAPTIME.MT\r\n\r\n", "404"},
		{"the host with a trailing dot", "GET /admin HTTP/1.1\r\nHost: ops.taptime.mt.\r\n\r\n", "404"},
		{"the host with a port", "GET /admin HTTP/1.1\r\nHost: ops.taptime.mt:8443\r\n\r\n", "404"},
		{"an absolute URI naming the operator host", "GET http://ops.taptime.mt/admin HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "404"},
		{"a forwarded host is not believed (operator Host)", "GET /admin HTTP/1.1\r\nHost: ops.taptime.mt\r\nX-Forwarded-Host: taptime.mt\r\n\r\n", "404"},
		// The customer host: the customer routes, untouched.
		{"CONTROL: the panel on the customer host", "GET /admin HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "customer"},
		{"CONTROL: the root on the customer host", "GET / HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "customer"},
		{"CONTROL: a legal page on the customer host", "GET /legal/cookies HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "customer"},
		{"an absolute URI naming the customer host", "GET http://taptime.mt/admin HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "customer"},
		{"a forwarded host is not believed (customer Host)", "GET /admin HTTP/1.1\r\nHost: taptime.mt\r\nX-Forwarded-Host: ops.taptime.mt\r\n\r\n", "customer"},
		{"no Host at all (HTTP/1.0)", "GET /admin HTTP/1.0\r\n\r\n", "customer"},
		{"a sibling of the operator host", "GET /admin HTTP/1.1\r\nHost: evil.ops.taptime.mt\r\n\r\n", "customer"},
		// This half does not answer /operator on a customer host: that is the surface's
		// own hostGate (internal/handler/operator), so the stub answers here.
		{"the operator prefix on the customer host reaches the surface", "GET /operator HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "operator"},
	} {
		if got := outcome(rawServe(t, h, c.raw)); got != c.want {
			t.Errorf("%s: landed on %q, want %q", c.name, got, c.want)
		}
	}

	// The 404 the gate gives is the router's own: same status, body and headers as a
	// path no feature registered.
	gate := rawServe(t, h, "GET /admin HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n")
	none := rawServe(t, h, "GET /no-such-route HTTP/1.1\r\nHost: taptime.mt\r\n\r\n")
	if gate.Body.String() != none.Body.String() || gate.Result().Header.Get("Content-Type") != none.Result().Header.Get("Content-Type") ||
		gate.Result().Header.Get("X-Content-Type-Options") != none.Result().Header.Get("X-Content-Type-Options") {
		t.Errorf("the gate's 404 is not the router's: %q %v vs %q %v", gate.Body.String(), gate.Result().Header, none.Body.String(), none.Result().Header)
	}

	// CONTROL: with no operator host configured, the gate is not mounted.
	open := httpx.NewRouter(&config.Config{}, nil, stubFeatures{})
	if got := outcome(rawServe(t, open, "GET /admin HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n")); got != "customer" {
		t.Fatalf("CONTROL: with no TAPPA_OPERATOR_HOST the panel on any host landed on %q, want customer", got)
	}
	if got := outcome(rawServe(t, httpx.NewRouter(nil, nil, stubFeatures{}), "GET /admin HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n")); got != "customer" {
		t.Fatalf("CONTROL: with no configuration the panel landed on %q, want customer", got)
	}
}

// TestOperatorHostFile_ImportsOnlyThreeStandardPackages (3rd round).
//
// PART I -- operatorhost.go's import paths are net, net/http and strings (an alias or a
// dot import keeps the path). CONTROL: the parse found the file's package clause.
//
// PART II -- red on a set of import paths other than those three.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: the
// package's other files, what those three packages reach) is code review's -- no
// completeness claim.
func TestOperatorHostFile_ImportsOnlyThreeStandardPackages(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "operatorhost.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name.Name != "httpx" {
		t.Fatalf("CONTROL: parsed package %q", f.Name.Name)
	}
	var got []string
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p)
	}
	slices.Sort(got)
	if want := []string{"net", "net/http", "strings"}; !slices.Equal(got, want) {
		t.Errorf("operatorhost.go imports %v, want %v", got, want)
	}
}
