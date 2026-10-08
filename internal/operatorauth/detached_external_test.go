package operatorauth_test

// detached_external_test.go -- OP-14 phase E on the wire: internal/handler/operator's real
// handlers on the shipped router, over real TCP connections that the client HALF-closes --
// FIN right after the request, still reading. net/http then cancels the request's context
// and still delivers the answer (the OP-14 C security audit's P8). The store answers a done
// context the way pgxpool and pgconn do (nothing done, the context's error returned), so a
// statement still on the request's context fails here exactly where it would on PostgreSQL.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

// halfStore is the fake: one active account, the auth events written, a lock switch that
// makes the lookup report the account locked and op_open_session refuse (the database's
// shape), a hold that runs before the code step's lookup and before every row, and refuse:
// a row kind whose write the database refuses (internal/db's error shape: a fixed text and a
// SQLSTATE), which is not recorded.
type halfStore struct {
	mu     sync.Mutex
	acc    db.OperatorAccount
	email  string
	locked bool
	events []db.OperatorAuthEvent
	refuse map[db.OperatorAuthEvent]error
	hold   func()
	now    time.Time
}

func (s *halfStore) done(ctx context.Context, what string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("db: %s: %w", what, err)
	}
	return nil
}

func (s *halfStore) OperatorByEmail(ctx context.Context, email string) (db.OperatorAccount, error) {
	if err := s.done(ctx, "operator by email"); err != nil {
		return db.OperatorAccount{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if email == s.email {
		return s.acc, nil
	}
	return db.OperatorAccount{}, db.ErrNoOperator
}

func (s *halfStore) OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	s.hold()
	if err := s.done(ctx, "operator by id"); err != nil {
		return db.OperatorAccount{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.acc.ID {
		return db.OperatorAccount{}, db.ErrNoOperator
	}
	acc := s.acc
	if s.locked {
		until := s.now.Add(15 * time.Minute)
		acc.LockedUntil = &until
	}
	return acc, nil
}

func (s *halfStore) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, _ string, _ uuid.UUID) error {
	s.hold()
	if err := s.done(ctx, "record operator auth event"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refuse[kind]; err != nil {
		return err
	}
	s.events = append(s.events, kind)
	return nil
}

func (s *halfStore) OpenOperatorSession(ctx context.Context, _ uuid.UUID, _ string, _ int64) error {
	if err := s.done(ctx, "open operator session"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return db.ErrOperatorRefused
	}
	return nil
}

func (s *halfStore) CompleteOperatorEnrollment(ctx context.Context, _ uuid.UUID, _, _ string, _ []byte, _ int64, _ string) error {
	if err := s.done(ctx, "complete operator enrollment"); err != nil {
		return err
	}
	return db.ErrOperatorRefused
}

func (s *halfStore) TouchOperatorSession(context.Context, string) (db.OperatorSession, error) {
	return db.OperatorSession{}, db.ErrOperatorRefused
}

func (s *halfStore) CloseOperatorSession(context.Context, string) error { return db.ErrOperatorRefused }

func (s *halfStore) kinds() []db.OperatorAuthEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]db.OperatorAuthEvent(nil), s.events...)
}

// wireAnswer is one response as the client read it: the status, the headers without Date
// (the one header that names the moment), the body's bytes and the cookies set -- and the
// request id the request was sent with.
type wireAnswer struct {
	status  int
	header  http.Header
	body    []byte
	cookies []*http.Cookie
	rid     string
}

func (w wireAnswer) same(o wireAnswer) bool {
	return w.status == o.status && bytes.Equal(w.body, o.body) && fmt.Sprint(w.header) == fmt.Sprint(o.header)
}

// TestSurface_AHalfClosedRefusalIsTheSameAnswer is the OP-14 E card's md. 5: every refusal
// of the sign-in is the same status, the same headers (Date aside) and the same body bytes
// whether the client stayed or half-closed its connection, and the half-closed request
// still leaves its row. In each half-closed request the client sends its FIN once the step is
// held at its first statement after the form was read -- the comparison, the code step's
// lookup, the enrollment's row -- and the step stays held until net/http has cancelled the
// request's context (the PREMISE is measured on the context the handler was given).
//
// 3rd round: the FIN WAITS for the hold. It used to go out right after the request, and
// under load net/http could cancel the context before the password step's lookup -- which
// follows the client by design (LE5): no comparison, no row, 503 -- so the premise failed
// once in a full -race run of the package (the right password's arm, "held 0"). Sent
// while the step is held, the FIN lands after the lookup on every arm, which is the
// shape the security audit's P8 measured (FIN during the comparison). The arms: a wrong password and an unknown address (401,
// login_failed / unknown_email); the code step's wrong code (401, totp_failed); with the
// account locked, a wrong code and the RIGHT code (one 401 page for both, totp_failed and
// locked -- a client that half-closes cannot tell them apart under the lock); a malformed
// enrollment link (400, enrollment_failed); and (2nd round, the third eye's F1) a valid
// enrollment page with a wrong first code (401, the form rendered again, enrollment_failed).
// CONTROL: a right password half-closed is 303 with its challenge cookie. Measured before
// the detach (P8): a wrong password half-closed was a plain 500 with no row while the right
// one was 303 -- the guess's result, read without a trail.
//
// 2nd round, the third eye's F2: the answer is rendered on a context that keeps the
// request's VALUES. The last arm makes the wrong password's row a database refusal: both
// clients get the same 503, no row, and each ERROR line the step failure writes carries the
// request id that request was sent with (the logger is wrapped as cmd/tappa wraps it,
// httpx.WithRequestID). A context without the request's values (context.Background) would
// answer alike and log the failure with no request id -- a line nobody can join to its
// request.
func TestSurface_AHalfClosedRefusalIsTheSameAnswer(t *testing.T) {
	kek := make([]byte, 32)
	copy(kek, "op14e half-close KEK, 32 bytes!!")
	key := []byte("op14e half TOTP key!")
	now := time.Unix(1_900_000_005, 0)
	digest, err := bcrypt.GenerateFromPassword([]byte(surfPass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	sealed, err := sun.Seal(kek, id[:], key)
	if err != nil {
		t.Fatal(err)
	}
	var armed atomic.Bool
	var reqCtx atomic.Pointer[context.Context]
	var held, missed atomic.Int64
	entered := make(chan struct{}, 16)
	wait := func() {
		if !armed.Load() {
			return
		}
		held.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-(*reqCtx.Load()).Done():
		case <-time.After(5 * time.Second):
			missed.Add(1)
		}
	}
	store := &halfStore{
		acc:   db.OperatorAccount{ID: id, Digest: db.NewPasswordHash(string(digest)), Sealed: db.NewSealedSecret(sealed)},
		email: surfEmail, hold: wait, now: now,
	}
	logs := &syncBuffer{}
	log := slog.New(httpx.WithRequestID(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	a, err := operatorauth.New(store, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(kek), TokenHMACKey: operatorauth.NewKey([]byte("op14e half-close HMAC key, 32 b!")),
		Now: func() time.Time { return now }, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	operatorauth.HoldComparisons(a, wait)
	s, err := operator.New(a, surfLegal{}, surfTenants{}, surfTenants{}, surfTenants{}, surfTenants{}, surfTenants{}, surfVIES{}, surfLegal{}, surfHost,
		"https://taptime.mt", log)
	if err != nil {
		t.Fatal(err)
	}
	router := httpx.NewRouter(&config.Config{OperatorHost: surfHost, BaseURL: "https://taptime.mt"}, log, s)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		reqCtx.Store(&ctx)
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	send := func(path string, form url.Values, half bool, cookies ...*http.Cookie) wireAnswer {
		t.Helper()
		conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		body, rid := form.Encode(), uuid.NewString()
		var head strings.Builder
		head.WriteString("POST " + path + " HTTP/1.1\r\nHost: " + surfHost + "\r\nOrigin: " + surfOrigin +
			"\r\nContent-Type: application/x-www-form-urlencoded\r\nConnection: close\r\n" +
			httpx.RequestIDHeader + ": " + rid + "\r\n")
		for _, c := range cookies {
			head.WriteString("Cookie: " + c.Name + "=" + c.Value + "\r\n")
		}
		head.WriteString(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body)))
		for len(entered) > 0 {
			<-entered
		}
		held0, missed0 := held.Load(), missed.Load()
		armed.Store(half)
		defer armed.Store(false)
		if _, err := io.WriteString(conn, head.String()+body); err != nil {
			t.Fatal(err)
		}
		if half {
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatalf("POST %s: PREMISE: the step never reached its first held statement", path)
			}
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
		}
		if err := conn.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("POST %s (half-closed=%v): no answer: %v", path, half, err)
		}
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("POST %s (half-closed=%v): the body was cut: %v", path, half, err)
		}
		if half && (held.Load() == held0 || missed.Load() != missed0) {
			t.Fatalf("POST %s: PREMISE: the step was not held until net/http cancelled the request's context (held %d, missed %d)",
				path, held.Load()-held0, missed.Load()-missed0)
		}
		h := resp.Header.Clone()
		h.Del("Date")
		return wireAnswer{status: resp.StatusCode, header: h, body: b, cookies: resp.Cookies(), rid: rid}
	}
	login := func(email, given string) url.Values { return url.Values{"email": {email}, "password": {given}} }
	// alike sends form twice -- the client staying, then half-closing -- and requires one
	// answer with status want and the kind's row after each.
	alike := func(name, path string, form url.Values, want int, kind db.OperatorAuthEvent, cookies ...*http.Cookie) wireAnswer {
		t.Helper()
		stayed := send(path, form, false, cookies...)
		n := len(store.kinds())
		halfClosed := send(path, form, true, cookies...)
		k := store.kinds()
		if stayed.status != want || !stayed.same(halfClosed) {
			t.Errorf("%s: the client that stayed got %d (%d bytes), the half-closed one %d (%d bytes); want one answer, %d",
				name, stayed.status, len(stayed.body), halfClosed.status, len(halfClosed.body), want)
		}
		if len(k) != n+1 || k[n] != kind {
			t.Errorf("%s, half-closed: the rows after it %v; want one more, %s", name, k[n:], kind)
		}
		t.Logf("%s: %d, %d body bytes, both answers alike=%v", name, halfClosed.status, len(halfClosed.body), stayed.same(halfClosed))
		return halfClosed
	}

	alike("a wrong password", "/operator/login", login(surfEmail, surfPass+"!"), http.StatusUnauthorized, db.OperatorLoginFailed)
	alike("an unknown address", "/operator/login", login("nobody@example.test", surfPass), http.StatusUnauthorized, db.OperatorUnknownEmail)

	right := send("/operator/login", login(surfEmail, surfPass), true)
	var challenge *http.Cookie
	for _, c := range right.cookies {
		if c.Name == operatorauth.ChallengeCookieName && c.Value != "" {
			challenge = c
		}
	}
	if right.status != http.StatusSeeOther || challenge == nil {
		t.Fatalf("CONTROL: the right password, half-closed: %d, challenge cookie=%v; want 303 and one", right.status, challenge != nil)
	}

	code := func(c string) url.Values { return url.Values{"code": {c}} }
	alike("a wrong code", "/operator/login/totp", code(otpNotAt(key, now)), http.StatusUnauthorized, db.OperatorTOTPFailed, challenge)

	store.mu.Lock()
	store.locked = true
	store.mu.Unlock()
	lockedWrong := alike("a wrong code while locked", "/operator/login/totp", code(otpNotAt(key, now)), http.StatusUnauthorized, db.OperatorTOTPFailed, challenge)
	n := len(store.kinds())
	lockedRight := send("/operator/login/totp", code(otpAt(key, now)), true, challenge)
	if !lockedRight.same(lockedWrong) || len(lockedRight.cookies) != 0 {
		t.Errorf("under the lock, half-closed: the right code got %d (%d bytes, %d cookie(s)), a wrong one %d (%d bytes); want one answer",
			lockedRight.status, len(lockedRight.body), len(lockedRight.cookies), lockedWrong.status, len(lockedWrong.body))
	}
	if k := store.kinds(); len(k) != n+1 || k[n] != db.OperatorLocked {
		t.Errorf("the right code under the lock, half-closed: rows after it %v; want one 'locked'", k[n:])
	}

	enroll := url.Values{"id": {uuid.NewString()}, "token": {"short"}, "blob": {"x"},
		"password": {surfPass}, "password_again": {surfPass}, "code": {"000000"}}
	alike("a malformed enrollment link", "/operator/enroll", enroll, http.StatusBadRequest, db.OperatorEnrollmentFailed)

	// 2nd round, F1: a real enrollment page -- its key and its blob -- and a wrong first code.
	// The step refuses after every check Go can make but the code, writes its row, and the
	// form is rendered again (renderEnroll's 401).
	pending := uuid.NewString()
	page, err := http.NewRequest(http.MethodGet, srv.URL+"/operator/enroll?id="+pending, nil)
	if err != nil {
		t.Fatal(err)
	}
	page.Host = surfHost
	pageResp, err := http.DefaultClient.Do(page)
	if err != nil {
		t.Fatal(err)
	}
	pageBody, err := io.ReadAll(pageResp.Body)
	_ = pageResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	km, bm := surfKeyRE.FindSubmatch(pageBody), surfBlobRE.FindSubmatch(pageBody)
	if pageResp.StatusCode != http.StatusOK || km == nil || bm == nil {
		t.Fatalf("PREMISE: the enrollment page = %d without its key or blob", pageResp.StatusCode)
	}
	pendingKey, err := base32.StdEncoding.DecodeString(strings.ReplaceAll(string(km[1]), " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	wrongFirst := url.Values{"id": {pending}, "token": {strings.Repeat("T", 43)}, "blob": {html.UnescapeString(string(bm[1]))},
		"password": {surfPass}, "password_again": {surfPass}, "code": {otpNotAt(pendingKey, now)}}
	alike("a wrong first enrollment code", "/operator/enroll", wrongFirst, http.StatusUnauthorized, db.OperatorEnrollmentFailed)

	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, "level=ERROR") {
			t.Errorf("a step failed: an ERROR line was logged (%d characters)", len(l))
		}
	}

	// 2nd round, F2: a row the database refuses -- the step's error, one 503 for both
	// clients, and each failure's ERROR line joined to its request by the request id.
	store.mu.Lock()
	store.refuse = map[db.OperatorAuthEvent]error{db.OperatorLoginFailed: errors.New("db: record operator auth event: database error (SQLSTATE 25006)")}
	store.mu.Unlock()
	before, logged := len(store.kinds()), len(logs.String())
	stayed := send("/operator/login", login(surfEmail, surfPass+"!"), false)
	halfClosed := send("/operator/login", login(surfEmail, surfPass+"!"), true)
	if stayed.status != http.StatusServiceUnavailable || !stayed.same(halfClosed) || len(store.kinds()) != before {
		t.Errorf("a refused row: the client that stayed got %d (%d bytes), the half-closed one %d (%d bytes), %d row(s) written; want one 503 and none",
			stayed.status, len(stayed.body), halfClosed.status, len(halfClosed.body), len(store.kinds())-before)
	}
	failures := map[string]bool{}
	for _, l := range strings.Split(logs.String()[logged:], "\n") {
		if strings.Contains(l, "level=ERROR") && strings.Contains(l, "the first sign-in step failed") {
			for _, rid := range []string{stayed.rid, halfClosed.rid} {
				if strings.Contains(l, httpx.LogRequestIDKey+"="+rid) {
					failures[rid] = true
				}
			}
		}
	}
	if !failures[stayed.rid] || !failures[halfClosed.rid] {
		t.Errorf("a refused row: the step failure's ERROR line carries its request's id for the client that stayed=%v, the half-closed one=%v; want both",
			failures[stayed.rid], failures[halfClosed.rid])
	}
	t.Logf("a refused row: %d, %d body bytes, both answers alike=%v, request id on both ERROR lines=%v",
		halfClosed.status, len(halfClosed.body), stayed.same(halfClosed), failures[stayed.rid] && failures[halfClosed.rid])

	if strings.Contains(logs.String(), surfEmail) || strings.Contains(logs.String(), "nobody@example.test") {
		t.Fatal("the log carries an address")
	}
}
