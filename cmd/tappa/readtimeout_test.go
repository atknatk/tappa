package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/httpx"
)

// The T91 measurements: applyTimeouts -- what run() sets on its server -- driven over
// real TCP at a scale a test can wait for. Only the durations are scaled.

// bodyRead is what the handler saw of its body.
type bodyRead struct {
	n       int64
	err     error
	elapsed time.Duration
}

// serveScaled starts a server with applyTimeouts on a loopback port with t's timeouts and h, and stops it
// when the test ends.
func serveScaled(t *testing.T, timeouts serverTimeouts, h http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: h}
	applyTimeouts(srv, timeouts)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			_ = srv.Close()
		}
	})
	return ln.Addr().String()
}

// readingHandler reads the whole body (after setDeadline, when non-zero, sets the
// connection's read deadline the way the logo upload gate does) and reports it.
func readingHandler(setDeadline time.Duration, got chan<- bodyRead) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if setDeadline > 0 {
			if err := http.NewResponseController(w).SetReadDeadline(time.Now().Add(setDeadline)); err != nil {
				got <- bodyRead{err: fmt.Errorf("set deadline: %w", err)}
				return
			}
		}
		n, err := io.Copy(io.Discard, r.Body)
		got <- bodyRead{n: n, err: err, elapsed: time.Since(start)}
		_, _ = io.WriteString(w, "ok")
	})
}

// sendSlowly writes a POST whose body of total bytes arrives in chunk-byte pieces every
// gap, then stalls after stopAfter bytes when stopAfter < total. It returns the
// connection so the caller can read the answer.
func sendSlowly(t *testing.T, addr string, total, chunk, stopAfter int, gap time.Duration) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := fmt.Fprintf(c, "POST /upload HTTP/1.1\r\nHost: taptime.test\r\nContent-Length: %d\r\n\r\n", total); err != nil {
		t.Fatalf("write headers: %v", err)
	}
	go func() {
		sent := 0
		for sent < total && sent < stopAfter {
			n := min(chunk, total-sent, stopAfter-sent)
			if _, err := c.Write([]byte(strings.Repeat("a", n))); err != nil {
				return
			}
			sent += n
			time.Sleep(gap)
		}
	}()
	return c
}

func waitRead(t *testing.T, got <-chan bodyRead, within time.Duration) bodyRead {
	t.Helper()
	select {
	case r := <-got:
		return r
	case <-time.After(within):
		t.Fatalf("the handler was still reading its body after %v: nothing bounds a stalled body", within)
		return bodyRead{}
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// TestServer_ASlowBodyIsCutAtTheReadTimeout: a client that sends its headers and part of
// the body and then stops is cut off by the server's ReadTimeout -- the handler's read
// returns a timeout and its goroutine ends -- while a body that arrives inside the
// window is read whole.
func TestServer_ASlowBodyIsCutAtTheReadTimeout(t *testing.T) {
	scaled := serverTimeouts{readHeader: 300 * time.Millisecond, read: 800 * time.Millisecond, idle: 2 * time.Second}

	t.Run("a stalled body", func(t *testing.T) {
		got := make(chan bodyRead, 1)
		addr := serveScaled(t, scaled, readingHandler(0, got))
		sendSlowly(t, addr, 100, 10, 10, 10*time.Millisecond)
		r := waitRead(t, got, 4*time.Second)
		if !isTimeout(r.err) {
			t.Fatalf("the stalled body's read ended with %v after %d bytes, want a timeout", r.err, r.n)
		}
		if r.n != 10 {
			t.Errorf("read %d bytes, want the 10 that were sent", r.n)
		}
	})

	t.Run("a body inside the window", func(t *testing.T) {
		got := make(chan bodyRead, 1)
		addr := serveScaled(t, scaled, readingHandler(0, got))
		c := sendSlowly(t, addr, 100, 10, 100, 30*time.Millisecond)
		r := waitRead(t, got, 4*time.Second)
		if r.err != nil || r.n != 100 {
			t.Fatalf("the body read %d bytes with %v, want 100 and no error", r.n, r.err)
		}
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		line, err := bufio.NewReader(c).ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "HTTP/1.1 200") {
			t.Fatalf("the answer was %q (%v), want 200", line, err)
		}
	})
}

// TestServer_AHandlersReadDeadlineReplacesTheServers: the logo upload gate's
// SetReadDeadline (internal/handler brandUploadGate) is the one that holds once it is
// set, in both directions -- a longer one lets a body arrive after ReadTimeout, a
// shorter one cuts it before. So the upload keeps its own window whatever ReadTimeout is.
func TestServer_AHandlersReadDeadlineReplacesTheServers(t *testing.T) {
	t.Run("longer than ReadTimeout: the body still arrives", func(t *testing.T) {
		got := make(chan bodyRead, 1)
		addr := serveScaled(t, serverTimeouts{readHeader: 200 * time.Millisecond, read: 400 * time.Millisecond, idle: 2 * time.Second},
			readingHandler(2*time.Second, got))
		sendSlowly(t, addr, 100, 10, 100, 90*time.Millisecond) // ~0.9 s, past the 0.4 s ReadTimeout
		r := waitRead(t, got, 4*time.Second)
		if r.err != nil || r.n != 100 {
			t.Fatalf("read %d bytes with %v: the handler's longer deadline did not replace ReadTimeout", r.n, r.err)
		}
	})
	t.Run("shorter than ReadTimeout: the body is cut at the handler's", func(t *testing.T) {
		got := make(chan bodyRead, 1)
		addr := serveScaled(t, serverTimeouts{readHeader: 200 * time.Millisecond, read: 5 * time.Second, idle: 6 * time.Second},
			readingHandler(300*time.Millisecond, got))
		sendSlowly(t, addr, 100, 10, 100, 90*time.Millisecond) // ~0.9 s, inside the 5 s ReadTimeout
		r := waitRead(t, got, 4*time.Second)
		if !isTimeout(r.err) {
			t.Fatalf("read %d bytes with %v, want the handler's shorter deadline to cut it", r.n, r.err)
		}
		if r.elapsed > 2*time.Second {
			t.Errorf("the cut came after %v, want it near the handler's 300 ms, far from ReadTimeout's 5 s", r.elapsed)
		}
	})
}

// TestServer_AHandlerOutlivingTheReadTimeoutIsNotCut: once the body is read, ReadTimeout
// ends nothing -- a handler still running after it sees no cancelled context and its
// whole answer reaches the client. This is the property a tap's late confirmation and
// the CSV export depend on (server.go).
func TestServer_AHandlerOutlivingTheReadTimeoutIsNotCut(t *testing.T) {
	const answer = 64 << 10
	ctxErr := make(chan error, 1)
	addr := serveScaled(t, serverTimeouts{readHeader: 200 * time.Millisecond, read: 300 * time.Millisecond, idle: 2 * time.Second},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			time.Sleep(800 * time.Millisecond)
			ctxErr <- r.Context().Err()
			_, _ = io.WriteString(w, strings.Repeat("x", answer))
		}))
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Post("http://"+addr+"/", "text/plain", strings.NewReader("body"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil || len(b) != answer {
		t.Fatalf("the answer arrived with %d bytes (%v), want %d", len(b), err, answer)
	}
	if err := <-ctxErr; err != nil {
		t.Fatalf("the handler's context was %v after ReadTimeout passed; a long handler would be cut", err)
	}
}

// TestServer_ProductionTimeoutsAreTheArgued pins the values server.go argues for and
// that run() serves with them.
func TestServer_ProductionTimeoutsAreTheArgued(t *testing.T) {
	if want := 30 * time.Second; productionTimeouts.read != want || httpx.RequestTimeout != want {
		t.Errorf("ReadTimeout %v, httpx.RequestTimeout %v, want both 30 s: change server.go's arithmetic first",
			productionTimeouts.read, httpx.RequestTimeout)
	}
	if productionTimeouts.readHeader != 10*time.Second || productionTimeouts.idle != 90*time.Second {
		t.Errorf("ReadHeaderTimeout %v / IdleTimeout %v changed", productionTimeouts.readHeader, productionTimeouts.idle)
	}
	// The legal text, the largest body ReadTimeout governs: 320 KiB after the worst header
	// allowance must not need more than 20 KB/s.
	if left := productionTimeouts.read - productionTimeouts.readHeader; float64(320<<10)/left.Seconds() > 20<<10 {
		t.Errorf("after the header allowance %v is left for a 320 KiB body", left)
	}
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if strings.Count(string(src), "applyTimeouts(srv, productionTimeouts)") != 1 ||
		strings.Count(string(src), "&http.Server{") != 1 ||
		strings.Contains(string(src), "ReadTimeout:") {
		t.Error("run() does not set its one server's timeouts with applyTimeouts(srv, productionTimeouts)")
	}
}
