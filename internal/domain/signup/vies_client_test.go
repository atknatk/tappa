package signup

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// THE PRODUCTION CLIENT, AGAINST A LOCAL SERVER (M10 OP-16 B; backlog T106). Every other test of
// this file's neighbour hands newCheckerAt the local server's own srv.Client(), so the client
// NewChecker builds -- its redirect refusal, its request bound, its dial, handshake and
// response-header bounds -- is not the one they drive: any of those could be removed and they
// would stay green (the OP-16C third eye's E11/E12). The tests here drive NewChecker's checker
// with only its base URL moved to a local server, through the same unexported field newCheckerAt
// sets; nothing here reaches the European Commission.

// productionCheckerAt is NewChecker's checker -- its *http.Client and transport exactly -- with
// its base URL moved to base.
func productionCheckerAt(base string) *Checker {
	c := NewChecker()
	c.baseURL = base
	return c
}

// TestVIESProductionClient_RefusesToFollowARedirect: NewChecker's client turns a redirect into
// Unknown and never asks the host it was sent to.
//
// PART I -- for each of 301, 302, 303, 307 and 308 from the lookup's path to another path of the
// same server that answers {"isValid":true}: Check is Unknown, the lookup's path was asked once,
// the redirect's target never. CONTROL: the same server through srv.Client() -- a client that
// follows redirects -- is Valid with the target asked once, so a production client that followed
// would have reported a confirmation the register never gave.
//
// PART II -- the five statuses above. PART III -- a redirect to another host is the same client
// rule (CheckRedirect refuses every redirect before it is followed); not driven -- no
// completeness claim.
func TestVIESProductionClient_RefusesToFollowARedirect(t *testing.T) {
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			var asked, followed atomic.Int32
			mux := http.NewServeMux()
			mux.HandleFunc("/MT/vat/12345678", func(w http.ResponseWriter, r *http.Request) {
				asked.Add(1)
				http.Redirect(w, r, "/elsewhere", code)
			})
			mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, _ *http.Request) {
				followed.Add(1)
				_, _ = w.Write([]byte(`{"isValid":true}`))
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			if got := productionCheckerAt(srv.URL).Check(context.Background(), "MT12345678"); got != VATUnknown {
				t.Errorf("the production client answered %v through a %d, want unknown", got, code)
			}
			if asked.Load() != 1 || followed.Load() != 0 {
				t.Errorf("the lookup was asked %d time(s) and the redirect's target %d, want 1 and 0", asked.Load(), followed.Load())
			}
			if got := newCheckerAt(srv.URL, srv.Client()).Check(context.Background(), "MT12345678"); got != VATValid || followed.Load() != 1 {
				t.Fatalf("CONTROL: a client that follows the %d answered %v with the target asked %d time(s); the redirect is not what this measures",
					code, got, followed.Load())
			}
		})
	}
}

// silentServer accepts a request and answers nothing until the test ends: no status line, no
// header -- or, with headersFirst, a 200 header and the first bytes of a body, then nothing.
func silentServer(t *testing.T, headersFirst bool) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if headersFirst {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"isValid":`))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	return srv
}

// within runs fn and fails the test if it has not returned by limit -- so a bound that was
// removed is a red test, not a hung one. It returns how long fn took.
func within(t *testing.T, limit time.Duration, fn func()) time.Duration {
	t.Helper()
	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		fn()
		done <- time.Since(start)
	}()
	select {
	case d := <-done:
		return d
	case <-time.After(limit):
		t.Fatalf("still waiting after %v: the bound is gone", limit)
		return 0
	}
}

// TestVIESProductionClient_GivesUpOnASilentServerWithinItsBound: NewChecker's client does not wait
// for a register that says nothing.
//
// PART I -- through Check, with the caller's context carrying NO deadline: a server that sends
// no status line, and one that sends a 200 header and the first bytes of a body and then
// nothing, are each Unknown after between 2.5 s and viesTimeout + 1.5 s. Through the client
// ALONE (the request carries no deadline at all -- Check's own context bound is not in play):
// the silent server's request fails, and the stalled body's read fails, each within the same
// window -- the client's own request bound (http.Client.Timeout) and response-header bound; and a
// server that accepts the TCP connection of an https URL and never speaks TLS is Unknown through
// Check after between 1.5 s and 2.8 s -- the handshake bound (2 s), shorter than the request's
// 3 s, so a client without it would wait the 3 s.
//
// PART II -- the four servers above. PART III -- the dial bound (2 s) needs an address that
// accepts no TCP connection and answers no refusal, which a local test cannot make reliably;
// not driven -- no completeness claim.
func TestVIESProductionClient_GivesUpOnASilentServerWithinItsBound(t *testing.T) {
	low, high := 2500*time.Millisecond, viesTimeout+1500*time.Millisecond
	for _, c := range []struct {
		name         string
		headersFirst bool
	}{{"no status line", false}, {"a body that stalls", true}} {
		t.Run(c.name+", through Check", func(t *testing.T) {
			srv := silentServer(t, c.headersFirst)
			var got VATStatus
			d := within(t, high+2*time.Second, func() { got = productionCheckerAt(srv.URL).Check(context.Background(), "MT12345678") })
			if got != VATUnknown || d < low || d > high {
				t.Errorf("Check answered %v after %v, want unknown within [%v, %v]", got, d, low, high)
			}
		})
		t.Run(c.name+", the client alone", func(t *testing.T) {
			srv := silentServer(t, c.headersFirst)
			client := NewChecker().client
			var err error
			d := within(t, high+2*time.Second, func() {
				req, rerr := http.NewRequest(http.MethodGet, srv.URL+"/MT/vat/12345678", nil)
				if rerr != nil {
					err = rerr
					return
				}
				resp, derr := client.Do(req)
				if derr != nil {
					err = derr
					return
				}
				defer func() { _ = resp.Body.Close() }()
				_, err = io.ReadAll(resp.Body)
			})
			if err == nil || d < low || d > high {
				t.Errorf("the client alone: err %v after %v, want an error within [%v, %v]", err, d, low, high)
			}
		})
	}
	t.Run("a TLS handshake that never comes", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		var held []net.Conn
		accepted := make(chan struct{})
		go func() {
			defer close(accepted)
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				held = append(held, conn) // said nothing, kept open
			}
		}()
		t.Cleanup(func() {
			_ = ln.Close()
			<-accepted
			for _, c := range held {
				_ = c.Close()
			}
		})
		var got VATStatus
		d := within(t, 6*time.Second, func() {
			got = productionCheckerAt("https://"+ln.Addr().String()).Check(context.Background(), "MT12345678")
		})
		if got != VATUnknown || d < 1500*time.Millisecond || d > 2800*time.Millisecond {
			t.Errorf("Check answered %v after %v, want unknown within [1.5s, 2.8s] (the handshake's own bound)", got, d)
		}
	})
}

// TestVIESCheck_LogsNothingOnAnyPath (the orchestrator's K16-4; backlog T107, the VIES client's
// link): Check takes no logger, so the only lines it could write are on the process's global
// sinks -- log/slog's default logger and the log package's -- or on standard error by a print.
//
// PART I -- with both global sinks captured, Check is driven through every outcome kind on a
// local server -- a confirmation, a refusal, an outage inside a 200 (MS_UNAVAILABLE), a 500, a
// body that is not JSON, a redirect (the production client) -- and a number that is not in the
// format (no request at all): the captured output is empty, so in particular the number is in
// no form of it. CONTROL: a line written through each captured sink is found. And vies.go's own
// source imports neither log nor log/slog and calls no print: no fmt.Print*, fmt.Fprint*,
// println, print and no os.Stderr or os.Stdout.
//
// PART II -- the outcomes and the source facts above. PART III -- a line written by another
// package Check calls (net/http's own server-side logging is the server's, not this client's)
// is code review's -- no completeness claim.
func TestVIESCheck_LogsNothingOnAnyPath(t *testing.T) {
	var captured bytes.Buffer
	prevSlog, prevFlags, prevOut := slog.Default(), log.Flags(), log.Writer()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(&captured)
	t.Cleanup(func() {
		slog.SetDefault(prevSlog)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	const number = "MT87654321"
	for name, h := range map[string]http.HandlerFunc{
		"confirmed": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"isValid":true,"userError":"VALID"}`))
		},
		"refused": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"isValid":false,"userError":"INVALID"}`))
		},
		"an outage": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"isValid":false,"userError":"MS_UNAVAILABLE"}`))
		},
		"a 500":    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"not JSON": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>` + number + `</html>`)) },
		"a redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere?n="+number, http.StatusFound)
		},
		"no response": nil,
	} {
		if h == nil {
			// A number not in the format: no request is made at all.
			_ = productionCheckerAt("http://127.0.0.1:1").Check(context.Background(), "MT1234")
		} else {
			srv := httptest.NewServer(h)
			_ = newCheckerAt(srv.URL, srv.Client()).Check(context.Background(), number)
			_ = productionCheckerAt(srv.URL).Check(context.Background(), number)
			srv.Close()
		}
		if captured.Len() != 0 {
			t.Errorf("%s: Check wrote to a global log sink: %q", name, captured.String())
			captured.Reset()
		}
	}
	slog.Info("control", "n", number)
	log.Print("control " + number)
	if strings.Count(captured.String(), number) != 2 {
		t.Fatalf("CONTROL: a line through each captured sink is not found: %q", captured.String())
	}
	f, err := parser.ParseFile(token.NewFileSet(), "vies.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == "log" || p == "log/slog" {
			t.Errorf("vies.go imports %s", p)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "println" || id.Name == "print") {
				t.Errorf("vies.go calls the builtin %s", id.Name)
			}
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" && (strings.HasPrefix(sel.Sel.Name, "Print") || strings.HasPrefix(sel.Sel.Name, "Fprint")) {
					t.Errorf("vies.go calls fmt.%s", sel.Sel.Name)
				}
			}
		case *ast.SelectorExpr:
			if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "os" && (x.Sel.Name == "Stderr" || x.Sel.Name == "Stdout") {
				t.Errorf("vies.go names os.%s", x.Sel.Name)
			}
		}
		return true
	})
}

// viesBodyBound is the bound on VIES's answer body, written here as a number (16 KiB) and not
// read from viesMaxBody: a test that builds its body from the constant grows with it, and a
// bound raised to 16 MiB stayed green (the OP-16 B security audit's D1 / S18).
const viesBodyBound = 16 << 10 // 16 384 bytes

// TestVIESCheck_TheBodyBoundIsSixteenKiBToTheByte (the OP-16 B security audit's D1): the bound
// on the answer's body is 16 KiB, on the byte.
//
// PART I -- the production checker against a local server that answers a well-formed
// confirmation ({"isValid":true,"userError":"VALID",...}) padded to an exact length, with its
// Content-Length: 16 384 bytes is read and is Valid; 16 385 bytes -- the same answer with one
// more padding byte -- is Unknown; 17 KiB is Unknown. viesMaxBody is 16 384. CONTROL: the
// bodies are the length they claim, and the shortest of them is the shape status() reads as
// Valid, so the 16 385-byte Unknown is the bound and nothing else.
//
// PART II -- the three lengths above. PART III -- a body sent without a Content-Length
// (chunked) reaches the same reader; not driven -- no completeness claim.
func TestVIESCheck_TheBodyBoundIsSixteenKiBToTheByte(t *testing.T) {
	if viesMaxBody != viesBodyBound {
		t.Errorf("viesMaxBody = %d, want %d (16 KiB)", viesMaxBody, viesBodyBound)
	}
	answer := func(size int) []byte {
		head, tail := `{"isValid":true,"userError":"VALID","padding":"`, `"}`
		return []byte(head + strings.Repeat("x", size-len(head)-len(tail)) + tail)
	}
	for _, c := range []struct {
		size int
		want VATStatus
	}{
		{viesBodyBound, VATValid},
		{viesBodyBound + 1, VATUnknown},
		{17 << 10, VATUnknown},
	} {
		body := answer(c.size)
		if len(body) != c.size {
			t.Fatalf("PREMISE: the %d-byte answer is %d bytes", c.size, len(body))
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write(body)
		}))
		got := productionCheckerAt(srv.URL).Check(context.Background(), "MT12345678")
		srv.Close()
		if got != c.want {
			t.Errorf("a %d-byte confirmation is %v, want %v", c.size, got, c.want)
		}
	}
}
