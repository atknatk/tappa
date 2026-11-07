package operatorauth_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/sun"
)

// TestSurface_ASignInTheClientLeftBeforeItsLookupRingsNoAlarm (T213), a real socket through net/http:
// a valid form and a FIN, with no session. The handler in front of the router waits until net/http
// has cancelled the request's context (the PREMISE is measured there, not assumed), so the lookup,
// which follows the client by design (LE5), answers the cancellation.
//
//   - no alarm: 503, no ERROR line and no access record; six of these otherwise ring rule 5;
//   - no oracle: a right password, a wrong one and an unknown address are the same status and the
//     same bytes, with no comparison made and no row written;
//   - CONTROL: a client that stayed still gets its real answer (303 and 401), so this is not a sign-in
//     that stopped working.
func TestSurface_ASignInTheClientLeftBeforeItsLookupRingsNoAlarm(t *testing.T) {
	kek := make([]byte, 32)
	copy(kek, "t213 half-close KEK, 32 bytes!!!")
	now := time.Unix(1_900_000_005, 0)
	digest, err := bcrypt.GenerateFromPassword([]byte(surfPass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	sealed, err := sun.Seal(kek, id[:], []byte("t213 half TOTP key!!"))
	if err != nil {
		t.Fatal(err)
	}
	store := &halfStore{
		acc:   db.OperatorAccount{ID: id, Digest: db.NewPasswordHash(string(digest)), Sealed: db.NewSealedSecret(sealed)},
		email: surfEmail, hold: func() {}, now: now,
	}
	logs := &syncBuffer{}
	log := slog.New(httpx.WithRequestID(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	a, err := operatorauth.New(store, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(kek), TokenHMACKey: operatorauth.NewKey([]byte("t213 half-close HMAC key, 32 by!")),
		Now: func() time.Time { return now }, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	var comparisons atomic.Int64
	operatorauth.HoldComparisons(a, func() { comparisons.Add(1) })
	s, err := operator.New(a, surfLegal{}, surfTenants{}, surfTenants{}, surfTenants{}, surfTenants{}, surfTenants{}, surfTenants{}, surfTenants{},
		surfVIES{}, surfLegal{}, surfHost, "https://taptime.mt", log)
	if err != nil {
		t.Fatal(err)
	}
	router := httpx.NewRouter(&config.Config{OperatorHost: surfHost, BaseURL: "https://taptime.mt"}, log, s)
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
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	send := func(form url.Values, half bool) (int, []byte) {
		t.Helper()
		conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		body := form.Encode()
		head := fmt.Sprintf("POST /operator/login HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nContent-Type: application/x-www-form-urlencoded\r\nConnection: close\r\nContent-Length: %d\r\n",
			surfHost, surfOrigin, len(body))
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
			t.Fatalf("half-closed=%v: no answer: %v", half, err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("half-closed=%v: the body was cut: %v", half, err)
		}
		return resp.StatusCode, b
	}
	login := func(email, given string) url.Values { return url.Values{"email": {email}, "password": {given}} }

	var first []byte
	for name, form := range map[string]url.Values{
		"a right password":   login(surfEmail, surfPass),
		"a wrong password":   login(surfEmail, surfPass+"!"),
		"an unknown address": login("nobody@example.test", surfPass),
	} {
		logged := len(logs.String())
		rows, compared := len(store.kinds()), comparisons.Load()
		code, body := send(form, true)
		written := logs.String()[logged:]
		if code != http.StatusServiceUnavailable {
			t.Errorf("%s: %d, want 503", name, code)
		}
		if first == nil {
			first = body
		} else if !bytes.Equal(body, first) {
			t.Errorf("%s: the body differs from the other arms' (%d and %d bytes)", name, len(body), len(first))
		}
		if strings.Contains(written, "level=ERROR") || strings.Contains(written, "http.request") {
			t.Errorf("%s: an ERROR or an access record was written:\n%s", name, written)
		}
		if n := len(store.kinds()) - rows; n != 0 {
			t.Errorf("%s: %d row(s) written", name, n)
		}
		if n := comparisons.Load() - compared; n != 0 {
			t.Errorf("%s: %d comparison(s) made", name, n)
		}
	}
	if code, _ := send(login(surfEmail, surfPass), false); code != http.StatusSeeOther {
		t.Errorf("CONTROL: the right password from a client that stayed = %d, want 303", code)
	}
	if code, _ := send(login(surfEmail, surfPass+"!"), false); code != http.StatusUnauthorized {
		t.Errorf("CONTROL: a wrong password from a client that stayed = %d, want 401", code)
	}
}
