package operatorauth_test

// passwordok_external_test.go -- OP-14 phase C, 2nd round: the third eye's B1 through
// internal/handler/operator's real handler, on the shipped router, over a real TCP
// connection that the client closes WHILE the comparison runs. The store answers a done
// context the way pgxpool's Acquire and pgconn's Exec do: nothing is sent, the context's
// error is returned. Before the detach that made ten aborted right passwords spend the
// operator's cap with zero rows (measured by the third eye with cost-12 digests over
// TCP); this file holds the opposite.

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

// operatorFirstFactorCap is the shipped per-operator cap on 'password_ok' rows, restated:
// the constant is unexported, and TestBudgets_TheShippedNumbersArePinned pins it to 10.
const operatorFirstFactorCap = 10

// syncBuffer is a log sink the server's goroutines and the test can share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// abortStore is surfStore whose row write honours a done context (pgx's shape) and which
// keeps the context of the latest login lookup -- the request's own.
type abortStore struct {
	*surfStore
	mu      sync.Mutex
	lookups []context.Context
}

func (s *abortStore) OperatorByEmail(ctx context.Context, email string) (db.OperatorAccount, error) {
	s.mu.Lock()
	s.lookups = append(s.lookups, ctx)
	s.mu.Unlock()
	return s.surfStore.OperatorByEmail(ctx, email)
}

func (s *abortStore) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, e string, id uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("db: record operator auth event: %w", err)
	}
	return s.surfStore.RecordOperatorAuthEvent(ctx, kind, e, id)
}

func (s *abortStore) lastLookup() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lookups) == 0 {
		return nil
	}
	return s.lookups[len(s.lookups)-1]
}

func (s *abortStore) passwordOKs() int {
	n := 0
	for _, k := range s.kinds() {
		if k == db.OperatorPasswordOK {
			n++
		}
	}
	return n
}

// TestSurface_AnAbortedRightPasswordStillLeavesItsRow: operatorFirstFactorCap right
// passwords whose client closes the TCP connection while the comparison runs -- the
// comparison is held until net/http has cancelled the request's context (the PREMISE is
// measured on the lookup's context) -- each still leave their 'password_ok' row and log
// no step failure. The next right password, from a client that waits, gets 303 and its
// challenge cookie past the cap: no new row, ONE WARN line. The window holds the cap's
// rows, never zero.
func TestSurface_AnAbortedRightPasswordStillLeavesItsRow(t *testing.T) {
	kek := make([]byte, 32)
	copy(kek, "op8 surface test KEK, 32 bytes!!")
	store := &abortStore{surfStore: &surfStore{active: map[string]db.OperatorAccount{}}}
	digest, err := bcrypt.GenerateFromPassword([]byte(surfPass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	sealed, err := sun.Seal(kek, id[:], []byte("op8 surface TOTP key"))
	if err != nil {
		t.Fatal(err)
	}
	store.active[surfEmail] = db.OperatorAccount{ID: id, Digest: db.NewPasswordHash(string(digest)), Sealed: db.NewSealedSecret(sealed)}
	logs := &syncBuffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	a, err := operatorauth.New(store, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(kek), TokenHMACKey: operatorauth.NewKey([]byte("op8 surface token HMAC key, 32 b")),
		Now: func() time.Time { return time.Unix(1_900_000_005, 0) }, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	// stop frees a held comparison whatever the test does: on a failure before the release
	// (3rd round, F6) the held handler would otherwise keep the server's Close waiting for
	// ever. The cleanups run in reverse: stop is closed, then the server is closed.
	started, release, stop := make(chan struct{}), make(chan struct{}), make(chan struct{})
	operatorauth.HoldComparisons(a, func() {
		select {
		case started <- struct{}{}:
		case <-stop:
			return
		}
		select {
		case <-release:
		case <-stop:
		}
	})
	s, err := operator.New(a, surfLegal{}, surfTenants{}, surfTenants{}, surfTenants{}, surfTenants{}, surfLegal{}, surfHost, "https://taptime.mt", log)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpx.NewRouter(&config.Config{OperatorHost: surfHost, BaseURL: "https://taptime.mt"}, log, s))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(stop) })
	body := "email=" + url.QueryEscape(surfEmail) + "&password=" + url.QueryEscape(surfPass)
	const failed = "the first sign-in step failed"

	for i := 1; i <= operatorFirstFactorCap; i++ {
		conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(conn, "POST /operator/login HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\n\r\n%s",
			surfHost, surfOrigin, len(body), body)
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("aborted right password %d: the comparison never started", i)
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-store.lastLookup().Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("PREMISE: request %d's context was not cancelled after its client hung up", i)
		}
		release <- struct{}{}
		deadline := time.Now().Add(5 * time.Second)
		for store.passwordOKs() < i && !strings.Contains(logs.String(), failed) && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
		}
		if got := store.passwordOKs(); got != i || strings.Contains(logs.String(), failed) {
			t.Fatalf("aborted right password %d: %d 'password_ok' row(s), a step failure logged=%v; want %d and none",
				i, got, strings.Contains(logs.String(), failed), i)
		}
	}

	go func() {
		select {
		case <-started:
			select {
			case release <- struct{}{}:
			case <-stop:
			}
		case <-stop:
		}
	}()
	cl := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/operator/login", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = surfHost
	req.Header.Set("Origin", surfOrigin)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	challenge := false
	for _, c := range resp.Cookies() {
		challenge = challenge || (c.Name == operatorauth.ChallengeCookieName && c.Value != "")
	}
	if resp.StatusCode != http.StatusSeeOther || !challenge {
		t.Fatalf("the next right password, past the cap: HTTP %d, challenge cookie=%v; want 303 and one", resp.StatusCode, challenge)
	}
	if got, w := store.passwordOKs(), strings.Count(logs.String(), "level=WARN"); got != operatorFirstFactorCap || w != 1 {
		t.Fatalf("the window holds %d 'password_ok' row(s) and %d WARN line(s); want %d and 1", got, w, operatorFirstFactorCap)
	}
	if strings.Contains(logs.String(), surfEmail) {
		t.Fatal("the log carries the operator's address")
	}
}
