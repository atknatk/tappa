package operator_test

// rig_test.go -- the operator surface on the SHIPPED router with a store that needs no
// database. The rig's requests go through httpx.NewRouter with an operator host configured
// (so both halves of the host gate are live), the real operatorauth.Authenticator and
// the real handlers; the Store is the fake part, and it answers the way 00026's
// definers answer (internal/db's ErrNoOperator / ErrOperatorRefused) for the arms these
// tests drive. What the database decides -- replay, the lock, the session
// predicate's clock, the enrollment token -- is op8_db_test.go's, against PostgreSQL.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

const (
	opHost   = "ops.taptime.mt"
	opOrigin = "https://ops.taptime.mt"
	opBase   = "https://taptime.mt"
	// custHost is the customer product's canonical host (TAPPA_BASE_URL's).
	custHost = "taptime.mt"
)

// fakeStore answers operatorauth.Store the way 00026's definers answer, for the arms
// these tests drive, and COUNTS its calls by method -- "the resolver was not called"
// is a count of zero here.
type fakeStore struct {
	mu       sync.Mutex
	calls    map[string]int
	accounts map[string]db.OperatorAccount // ACTIVE accounts by address (tappa_operator's RLS shows active rows)
	byID     map[uuid.UUID]db.OperatorAccount
	events   []db.OperatorAuthEvent
	live     map[string]db.OperatorSession // session hash -> session
	tokens   map[uuid.UUID]string          // pending account -> its raw link token (unused)
	locked   map[uuid.UUID]bool            // accounts whose op_open_session the "database" refuses as locked
	fail     map[string]error              // method -> the error it returns instead
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		calls: map[string]int{}, accounts: map[string]db.OperatorAccount{}, byID: map[uuid.UUID]db.OperatorAccount{},
		live: map[string]db.OperatorSession{}, tokens: map[uuid.UUID]string{}, locked: map[uuid.UUID]bool{}, fail: map[string]error{},
	}
}

func (f *fakeStore) enter(m string) error {
	f.calls[m]++
	return f.fail[m]
}

// total sums the per-method counts.
func (f *fakeStore) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

func (f *fakeStore) count(m string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[m]
}

func (f *fakeStore) liveSessions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.live)
}

func (f *fakeStore) OperatorByEmail(_ context.Context, email string) (db.OperatorAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("OperatorByEmail"); err != nil {
		return db.OperatorAccount{}, err
	}
	a, ok := f.accounts[strings.ToLower(email)]
	if !ok {
		return db.OperatorAccount{}, db.ErrNoOperator
	}
	return a, nil
}

func (f *fakeStore) OperatorByID(_ context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("OperatorByID"); err != nil {
		return db.OperatorAccount{}, err
	}
	a, ok := f.byID[id]
	if !ok {
		return db.OperatorAccount{}, db.ErrNoOperator
	}
	return a, nil
}

func (f *fakeStore) RecordOperatorAuthEvent(_ context.Context, kind db.OperatorAuthEvent, _ string, _ uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("RecordOperatorAuthEvent"); err != nil {
		return err
	}
	f.events = append(f.events, kind)
	return nil
}

func (f *fakeStore) OpenOperatorSession(_ context.Context, admin uuid.UUID, h string, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("OpenOperatorSession"); err != nil {
		return err
	}
	if f.locked[admin] {
		return db.ErrOperatorRefused
	}
	f.live[h] = db.OperatorSession{SessionID: uuid.New(), AdminID: admin}
	return nil
}

func (f *fakeStore) CompleteOperatorEnrollment(_ context.Context, admin uuid.UUID, raw, _ string, _ []byte, _ int64, h string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CompleteOperatorEnrollment"); err != nil {
		return err
	}
	want, ok := f.tokens[admin]
	if !ok || want != raw {
		return db.ErrOperatorRefused
	}
	delete(f.tokens, admin)
	f.live[h] = db.OperatorSession{SessionID: uuid.New(), AdminID: admin}
	return nil
}

func (f *fakeStore) TouchOperatorSession(_ context.Context, h string) (db.OperatorSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("TouchOperatorSession"); err != nil {
		return db.OperatorSession{}, err
	}
	s, ok := f.live[h]
	if !ok {
		return db.OperatorSession{}, db.ErrOperatorRefused
	}
	return s, nil
}

func (f *fakeStore) CloseOperatorSession(_ context.Context, h string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("CloseOperatorSession"); err != nil {
		return err
	}
	if _, ok := f.live[h]; !ok {
		return db.ErrOperatorRefused
	}
	delete(f.live, h)
	return nil
}

// fixture is an active operator the fake store knows: its address, its password (a
// bcrypt at the MINIMUM cost -- these tests are about text and order, not time; the
// cost is operatorauth's pin and 00026's CHECK) and its TOTP key, sealed under the rig's
// KEK with the account id as AAD, exactly as enrollment stores it.
type fixture struct {
	id       uuid.UUID
	email    string
	password string
	key      []byte
}

// rig is one Authenticator + one Surface on the shipped router. Building it costs one
// cost-12 bcrypt (operatorauth.New's dummy digest) -- about 3 s under -race on the
// development machine (measured 2026-10-02) -- so tests share a rig across their
// subtests where the budgets allow.
type rig struct {
	t       *testing.T
	store   *fakeStore
	auth    *operatorauth.Authenticator
	surface *operator.Surface
	h       http.Handler
	kek     []byte
	now     time.Time
	logs    *bytes.Buffer // the process log and the access log, both handlers, Debug (newRigAt: its level)
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// fanout writes each record to each handler -- the shipped text AND JSON handlers at
// Debug, so a test reads what either production format would carry.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range f {
		if err := h.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (f fanout) WithAttrs(as []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(as)
	}
	return out
}

func (f fanout) WithGroup(n string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(n)
	}
	return out
}

// debugCapture is cmd/tappa's logger shape (httpx.WithRequestID around the handler)
// with BOTH shipped formats at Debug, into w.
func debugCapture(w io.Writer) *slog.Logger { return captureAt(w, slog.LevelDebug) }

// captureAt is debugCapture at level -- slog.LevelInfo is production's
// (deploy/k8s/05-config.yaml: TAPPA_LOG_LEVEL "info").
func captureAt(w io.Writer, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	return slog.New(httpx.WithRequestID(fanout{slog.NewTextHandler(w, opts), slog.NewJSONHandler(w, opts)}))
}

func newRig(t *testing.T) *rig {
	t.Helper()
	return newRigAt(t, slog.LevelDebug)
}

// newRigAt is newRig with its process and access log at level.
func newRigAt(t *testing.T, level slog.Level) *rig {
	t.Helper()
	g := &rig{t: t, store: newFakeStore(), kek: randBytes(t, 32), now: time.Unix(1_900_000_005, 0), logs: &bytes.Buffer{}}
	log := captureAt(g.logs, level)
	auth, err := operatorauth.New(g.store, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(g.kek), TokenHMACKey: operatorauth.NewKey(randBytes(t, 32)),
		Now: func() time.Time { return g.now }, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := operator.New(auth, opHost, opBase, log)
	if err != nil {
		t.Fatal(err)
	}
	g.auth, g.surface = auth, s
	g.h = httpx.NewRouter(&config.Config{OperatorHost: opHost, BaseURL: opBase}, log, customer{}, s)
	return g
}

// active adds an active operator to the fake store.
func (g *rig) active() fixture {
	g.t.Helper()
	f := fixture{id: uuid.New(), password: "op8 fixture passphrase", key: randBytes(g.t, 20)}
	f.email = "op8-" + f.id.String()[:8] + "@example.test"
	digest, err := bcrypt.GenerateFromPassword([]byte(f.password), bcrypt.MinCost)
	if err != nil {
		g.t.Fatal(err)
	}
	sealed, err := sun.Seal(g.kek, f.id[:], f.key)
	if err != nil {
		g.t.Fatal(err)
	}
	acc := db.OperatorAccount{ID: f.id, Digest: db.NewPasswordHash(string(digest)), Sealed: db.NewSealedSecret(sealed)}
	g.store.mu.Lock()
	g.store.accounts[strings.ToLower(f.email)] = acc
	g.store.byID[f.id] = acc
	g.store.mu.Unlock()
	return f
}

// totpAt is RFC 6238's code for key at t (HMAC-SHA1, 6 digits, 30 s) -- computed here,
// independently of operatorauth's own implementation.
func totpAt(key []byte, t time.Time) string {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(t.Unix()/30))
	m := hmac.New(sha1.New, key)
	_, _ = m.Write(c[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	out := []byte("000000")
	n := bin % 1_000_000
	for i := 5; i >= 0; i-- {
		out[i] = byte('0' + n%10)
		n /= 10
	}
	return string(out)
}

// wrongCodeAt is a six-digit code valid at no step of the window around t.
func wrongCodeAt(key []byte, t time.Time) string {
	valid := map[string]bool{}
	for d := -1; d <= 1; d++ {
		valid[totpAt(key, t.Add(time.Duration(d*30)*time.Second))] = true
	}
	for c := 0; ; c++ {
		if s := fmt.Sprintf("%06d", c); !valid[s] {
			return s
		}
	}
}

// req is one request through the rig's router.
type req struct {
	method, host, path string
	form               url.Values
	origin             string            // "" = no Origin header
	header             map[string]string // extra headers
	cookies            []*http.Cookie
	cookieLine         string // a SECOND Cookie header line, after the cookies' own
	remote             string // "" = 192.0.2.10:4000
}

func (g *rig) do(r req) *httptest.ResponseRecorder {
	g.t.Helper()
	var body io.Reader
	if r.form != nil {
		body = strings.NewReader(r.form.Encode())
	}
	hr := httptest.NewRequest(r.method, "http://"+r.host+r.path, body)
	hr.Host = r.host
	if r.form != nil {
		hr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if r.origin != "" {
		hr.Header.Set("Origin", r.origin)
	}
	for k, v := range r.header {
		hr.Header.Set(k, v)
	}
	for _, c := range r.cookies {
		hr.AddCookie(c)
	}
	if r.cookieLine != "" {
		hr.Header.Add("Cookie", r.cookieLine)
	}
	hr.RemoteAddr = "192.0.2.10:4000"
	if r.remote != "" {
		hr.RemoteAddr = r.remote
	}
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, hr)
	return w
}

// post is a same-origin form POST on the operator host.
func (g *rig) post(path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	g.t.Helper()
	return g.do(req{method: http.MethodPost, host: opHost, path: path, form: form, origin: opOrigin, cookies: cookies})
}

// get is a same-origin GET on the operator host.
func (g *rig) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	g.t.Helper()
	return g.do(req{method: http.MethodGet, host: opHost, path: path, cookies: cookies,
		header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
}

// cookie returns the Set-Cookie of name on w, or nil.
func cookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// signIn drives the password and TOTP steps for f and returns the live session cookie.
func (g *rig) signIn(f fixture) *http.Cookie {
	g.t.Helper()
	return g.signInAs(f.email, f.password, f.key, g.now)
}

// signInAs is signIn with the code computed at codeTime (a step of the window the
// Authenticator accepts at its own clock).
func (g *rig) signInAs(email, password string, key []byte, codeTime time.Time) *http.Cookie {
	g.t.Helper()
	w := g.post("/operator/login", url.Values{"email": {email}, "password": {password}})
	ch := cookie(w, operatorauth.ChallengeCookieName)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login/totp" || ch == nil {
		g.t.Fatalf("sign-in: password step = %d %q, challenge %v", w.Code, w.Result().Header.Get("Location"), ch != nil)
	}
	w = g.post("/operator/login/totp", url.Values{"code": {totpAt(key, codeTime)}}, ch)
	sc := cookie(w, operatorauth.SessionCookieName)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator" || sc == nil || sc.Value == "" {
		g.t.Fatalf("sign-in: code step = %d %q, session cookie %v", w.Code, w.Result().Header.Get("Location"), sc != nil)
	}
	return sc
}

var errFakeDB = errors.New("fake: the database is unreachable")
