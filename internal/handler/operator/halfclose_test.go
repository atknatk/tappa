package operator_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// errorLines is how many records of the rig's log are level ERROR. The rig writes each record in
// two forms, text and JSON; this counts the text form.
func errorLines(logs string) int { return strings.Count(logs, "level=ERROR") }

// beforeADecision are the answers the surface gives before any store call -- no session and no
// database behind them, so the arms anyone can reach. Their reasons: T109's three.
func beforeADecision() []struct {
	name string
	r    req
	code int
} {
	return []struct {
		name string
		r    req
		code int
	}{
		{"a body over the bound (readForm's 413)", req{method: http.MethodPost, host: opHost, path: "/operator/login", origin: opOrigin,
			raw: "email=" + strings.Repeat("a", 17<<10)}, http.StatusRequestEntityTooLarge},
		{"a body that does not parse (readForm's 400)", req{method: http.MethodPost, host: opHost, path: "/operator/login", origin: opOrigin,
			raw: "email=%zz"}, http.StatusBadRequest},
		{"another origin (the gate's 403)", req{method: http.MethodPost, host: opHost, path: "/operator/login", origin: "https://evil.example",
			form: url.Values{"email": {"a@example.test"}}}, http.StatusForbidden},
	}
}

// TestSurface_AClientThatLeftBeforeADecisionGetsTheSameAnswer (T109): each answer before a decision
// is the same status and the same bytes whether the client stayed or its request's context is
// already cancelled -- what net/http does for a client that half-closed -- and neither writes an
// ERROR line. Measured before this change: 500, a plain body, and two ERROR lines (the render's
// and the access record's, which is deploy/README.md's rule 5). CONTROL: a component does refuse a
// cancelled context, so this test can see render not detaching.
func TestSurface_AClientThatLeftBeforeADecisionGetsTheSameAnswer(t *testing.T) {
	g := newRig(t)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range beforeADecision() {
		stayed := g.do(tc.r)
		left := tc.r
		left.ctx = gone
		g.logs.Reset()
		got := g.do(left)
		if stayed.Code != tc.code || got.Code != stayed.Code || got.Body.String() != stayed.Body.String() {
			t.Errorf("%s: a client that stayed = %d, one that left = %d (same bytes %v); want %d both",
				tc.name, stayed.Code, got.Code, got.Body.String() == stayed.Body.String(), tc.code)
		}
		if n := errorLines(g.logs.String()); n != 0 {
			t.Errorf("%s: a client that left wrote %d ERROR line(s), want none", tc.name, n)
		}
	}
	var b bytes.Buffer
	component := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error { return ctx.Err() })
	if err := component.Render(gone, &b); !errors.Is(err, context.Canceled) {
		t.Fatalf("CONTROL: a component on a cancelled context = %v, want context.Canceled", err)
	}
}

// TestSurface_AHalfClosedRequestBeforeADecisionIsTheSameAnswerOverTCP is the same measure on a real
// socket through net/http: the client sends the request and its FIN, the handler in front of the
// router waits until net/http has cancelled the request's context (the PREMISE is measured on that
// context, not assumed), and the router then answers. The client, still reading, gets the answer of
// a client that stayed, and no ERROR is written.
//
// net/http notices a FIN only once the body has been read, so the handler in front reads it and
// hands the router a copy: what the router's MaxBytesReader sees is the same bytes.
func TestSurface_AHalfClosedRequestBeforeADecisionIsTheSameAnswerOverTCP(t *testing.T) {
	g := newRig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read the body in front of the router: %v", err)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if r.Header.Get("X-Half-Closed") != "" {
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
				t.Error("PREMISE: net/http did not cancel the request's context after the client's FIN")
			}
		}
		r.Header.Del("X-Half-Closed")
		g.h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	send := func(tc req, half bool) (int, []byte) {
		t.Helper()
		conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		body := tc.raw
		if tc.form != nil {
			body = tc.form.Encode()
		}
		head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nContent-Type: application/x-www-form-urlencoded\r\nConnection: close\r\nContent-Length: %d\r\n",
			tc.path, tc.host, tc.origin, len(body))
		if half {
			head += "X-Half-Closed: 1\r\n"
		}
		if _, err := io.WriteString(conn, head+"\r\n"+body); err != nil {
			t.Fatal(err)
		}
		if half {
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
		}
		if err := conn.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("%s (half-closed=%v): no answer: %v", tc.path, half, err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("%s (half-closed=%v): the body was cut: %v", tc.path, half, err)
		}
		return resp.StatusCode, b
	}
	for _, tc := range beforeADecision() {
		code, body := send(tc.r, false)
		g.logs.Reset()
		halfCode, halfBody := send(tc.r, true)
		if code != tc.code || halfCode != code || !bytes.Equal(halfBody, body) {
			t.Errorf("%s: a client that stayed = %d, one that half-closed = %d (same bytes %v); want %d both",
				tc.name, code, halfCode, bytes.Equal(halfBody, body), tc.code)
		}
		if n := errorLines(g.logs.String()); n != 0 {
			t.Errorf("%s: the half-closed client wrote %d ERROR line(s), want none", tc.name, n)
		}
	}
}

// TestSurface_AFailureIsStillAFailureForAClientThatLeft: the detach hides a client that left, not a
// fault. For a client whose context is already cancelled and for one that stayed:
//   - a component that fails for its own reason is 500 with its ERROR, through the real render;
//   - a screen whose read fails is the screen's 503, with the store's ERROR and the access record
//     at ERROR (rule 5 still sees it).
func TestSurface_AFailureIsStillAFailureForAClientThatLeft(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	refused := errors.New("the component refuses")
	failing := templ.ComponentFunc(func(context.Context, io.Writer) error { return refused })
	for name, ctx := range map[string]context.Context{"stayed": context.Background(), "left": gone} {
		g.logs.Reset()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "http://"+opHost+"/operator", nil).WithContext(ctx)
		operator.RenderForTest(g.surface, w, r, failing)
		if w.Code != http.StatusInternalServerError || !strings.Contains(g.logs.String(), "rendering a screen failed") || errorLines(g.logs.String()) != 1 {
			t.Errorf("%s: a failing component = %d with %d ERROR line(s), want 500 and its line", name, w.Code, errorLines(g.logs.String()))
		}
	}
	g.setStoreFailure("TenantList", errors.New("db: read tenants: database error (SQLSTATE 57014)"))
	defer g.setStoreFailure("TenantList", nil)
	for name, ctx := range map[string]context.Context{"stayed": context.Background(), "left": gone} {
		g.logs.Reset()
		w := g.do(req{method: http.MethodGet, host: opHost, path: "/operator/tenants", cookies: []*http.Cookie{c}, ctx: ctx})
		logs := g.logs.String()
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(logs, "the tenant list could not be read") || errorLines(logs) != 2 {
			t.Errorf("%s: a failing read = %d with %d ERROR line(s); want 503, the store's line and the access record", name, w.Code, errorLines(logs))
		}
	}
}

// TestSurface_AFailedSignInLookupIsStillAFailureForAClientThatLeft (T213): answerClientLeft is for one
// shape, and only that shape loses its ERROR. A client that left AND a lookup that answers the
// client's own cancellation: 503, no ERROR, no access record. Each of the others stays a 503 with the
// step's line and the access record at ERROR, which is what rule 5 is for:
//   - a client that left, the database refusing for its own reason (the real fault during a leaving);
//   - a client that stayed, the lookup answering a cancellation that is not the client's;
//   - a request past its deadline: a slow database, not a client that left.
func TestSurface_AFailedSignInLookupIsStillAFailureForAClientThatLeft(t *testing.T) {
	g := newRig(t)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	late, cancelLate := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancelLate()
	login := url.Values{"email": {"nobody@example.test"}, "password": {"x"}}
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		err    error
		errors int
	}{
		{"left, the lookup cancelled", gone, fmt.Errorf("db: operator by email: %w", context.Canceled), 0},
		{"left, the database refusing", gone, errors.New("db: operator by email: database error (SQLSTATE 25006)"), 2},
		{"stayed, a cancellation not the client's", context.Background(), fmt.Errorf("db: operator by email: %w", context.Canceled), 2},
		{"past its deadline", late, fmt.Errorf("db: operator by email: %w", context.DeadlineExceeded), 2},
	} {
		g.setStoreFailure("OperatorByEmail", tc.err)
		g.logs.Reset()
		w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/login", origin: opOrigin, form: login, ctx: tc.ctx})
		logs := g.logs.String()
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: %d, want 503", tc.name, w.Code)
		}
		if n := errorLines(logs); n != tc.errors {
			t.Errorf("%s: %d ERROR record(s), want %d:\n%s", tc.name, n, tc.errors, logs)
		}
		if records := strings.Count(logs, "level=ERROR msg=http.request"); (tc.errors == 0) != (records == 0) {
			t.Errorf("%s: %d access record(s) at ERROR with %d wanted ERROR line(s)", tc.name, records, tc.errors)
		}
	}
	g.setStoreFailure("OperatorByEmail", nil)
}

// TestLogout_AClientThatLeftStillEndsTheSession (T213), on a request whose context is already
// cancelled, with the fake's statement answering that cancellation as the real one does:
//   - a real session: 303, the cookie cleared and the session gone -- the decision was made;
//   - a forged cookie, no session needed and no flood gate in front: 303, no ERROR and no access
//     record at ERROR, so six of them ring nothing;
//   - a database fault for a client that left is still the page that says the session is open, 503
//     with its two ERROR records.
func TestLogout_AClientThatLeftStillEndsTheSession(t *testing.T) {
	g := newRig(t)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	logout := func(c *http.Cookie) *httptest.ResponseRecorder {
		return g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", origin: opOrigin, cookies: []*http.Cookie{c}, ctx: gone})
	}
	c := g.signIn(g.active())
	live := func() int {
		g.store.mu.Lock()
		defer g.store.mu.Unlock()
		return len(g.store.live)
	}
	before := live()
	g.logs.Reset()
	if w := logout(c); w.Code != http.StatusSeeOther || cookie(w, operatorauth.SessionCookieName) == nil {
		t.Errorf("a real session, half-closed: %d, cookie cleared %v; want 303 and cleared", w.Code, cookie(w, operatorauth.SessionCookieName) != nil)
	}
	if n := before - live(); n != 1 {
		t.Errorf("a real session, half-closed: %d session(s) ended, want 1", n)
	}
	if n := errorLines(g.logs.String()); n != 0 {
		t.Errorf("a real session, half-closed: %d ERROR record(s), want none", n)
	}

	forged := &http.Cookie{Name: operatorauth.SessionCookieName, Value: base64.RawURLEncoding.EncodeToString(randBytes(t, 32))}
	g.logs.Reset()
	if w := logout(forged); w.Code != http.StatusSeeOther {
		t.Errorf("a forged cookie, half-closed: %d, want 303", w.Code)
	}
	if n := errorLines(g.logs.String()); n != 0 {
		t.Errorf("a forged cookie, half-closed: %d ERROR record(s), want none", n)
	}

	g.setStoreFailure("CloseOperatorSession", errFakeDB)
	defer g.setStoreFailure("CloseOperatorSession", nil)
	g.logs.Reset()
	if w := logout(forged); w.Code != http.StatusServiceUnavailable {
		t.Errorf("a database fault, half-closed: %d, want 503", w.Code)
	}
	if n := errorLines(g.logs.String()); n != 2 {
		t.Errorf("a database fault, half-closed: %d ERROR record(s), want 2", n)
	}
}

// TestE2E_AHalfClosedSignOutEndsTheSessionAndWritesItsRow (T213) is the same on PostgreSQL, whose
// driver answers a cancelled context with an error that is context.Canceled: a real session whose
// client left is revoked with its 'logout' row, the cookie no longer signs in, and a forged cookie is
// 303 with no ERROR line.
func TestE2E_AHalfClosedSignOutEndsTheSessionAndWritesItsRow(t *testing.T) {
	l := newLegalE2E(t)
	sess := l.signIn(l.f)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	logout := func(c *http.Cookie) *httptest.ResponseRecorder {
		return l.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", origin: opOrigin, cookies: []*http.Cookie{c}, ctx: gone})
	}
	if w := logout(sess); w.Code != http.StatusSeeOther {
		t.Fatalf("the sign-out, half-closed = %d, want 303", w.Code)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'logout' AND actor_admin_id = $1`, l.f.id); n != 1 {
		t.Errorf("the sign-out, half-closed, wrote %d logout row(s), want 1", n)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM platform_sessions WHERE admin_id = $1 AND revoked_at IS NULL`, l.f.id); n != 0 {
		t.Errorf("%d session(s) of the operator still open, want 0", n)
	}
	if w := l.get("/operator", sess); w.Code != http.StatusSeeOther {
		t.Errorf("the cookie after the sign-out = %d, want the sign-in's 303", w.Code)
	}
	l.logs.Reset()
	forged := &http.Cookie{Name: operatorauth.SessionCookieName, Value: base64.RawURLEncoding.EncodeToString(randBytes(t, 32))}
	if w := logout(forged); w.Code != http.StatusSeeOther {
		t.Errorf("a forged cookie, half-closed = %d, want 303", w.Code)
	}
	if n := errorLines(l.logs.String()); n != 0 {
		t.Errorf("a forged cookie, half-closed: %d ERROR record(s), want none", n)
	}
}
