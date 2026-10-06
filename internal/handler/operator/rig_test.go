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
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/legal"
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

// fakeStore answers operatorauth.Store the way 00026's definers answer,
// operator.LegalStore the way 00027's do (OP-10), operator.TenantStore the way 00029's do
// (OP-11) and operator.PlaqueStore the way 00030's does (OP-13), for the arms these tests
// drive, and COUNTS its calls by method -- "the resolver was not called" is a count of
// zero here.
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
	// The legal screen's (OP-10): the versions, oldest first, as op_read_legal_versions
	// would list them newest first; the publications op_publish_legal took; the pages
	// LegalVersions was asked for.
	versions  []db.LegalVersion
	published []fakePublication
	pages     []db.LegalVersionsPage
	// beforePublish and afterPublish run inside PublishLegal, at its entry and after the
	// version is recorded -- a test's hook for a client that leaves (OP-10, round 2).
	beforePublish, afterPublish func()
	// afterVersions runs once LegalVersions has built its answer and released the fake's
	// lock -- a test's hook for another operator publishing between the screen's two
	// reads (OP-10, round 3). It may call PublishLegal.
	afterVersions func()
	// The tenant screens' (OP-11): the tenants op_read_tenants lists, in the order it
	// returns them (newest first -- a test puts them in that order); the overviews
	// op_read_tenant_detail returns, by id; the queries and the ids the screens asked for.
	tenants   []db.TenantSummary
	overviews map[uuid.UUID]db.TenantOverview
	queries   []db.TenantListQuery
	details   []uuid.UUID
	// matches, when set, replaces TenantList's matching rule -- a test that needs a full
	// page of results without the term in the tenants' names.
	matches func(term string, x db.TenantSummary) bool
	// The plaque screen's (OP-13): each tenant's inventory by id, and the ids the screen
	// asked for.
	inventories map[uuid.UUID]fakeInventory
	plaqueAsks  []uuid.UUID
}

// fakeInventory is one tenant's plaques as the fake holds them. Each plaque carries the
// two key columns the schema holds beside it (fakePlaque) -- KEPT here and never returned:
// db.TenantPlaque has no field for them, so TenantPlaques cannot hand them to the screen
// (TestPlaqueScreen_NoKeyReachesThePage puts key-shaped values in them and searches the
// page). total, when not zero, is the count the read reports (a tenant holding more
// plaques than the read returns); asTenant, when set, is the tenant id the read reports
// instead of the one asked for (a store that answers for another tenant).
type fakeInventory struct {
	name     string
	plaques  []fakePlaque
	total    int64
	asTenant uuid.UUID
}

type fakePlaque struct {
	db.TenantPlaque
	aesKeyRef, appKeyRef []byte
}

// fakePublication is one PublishLegal the fake took: the session hash, the document, the
// text and the version's publication time.
type fakePublication struct {
	hash, slug, body string
	at               time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		calls: map[string]int{}, accounts: map[string]db.OperatorAccount{}, byID: map[uuid.UUID]db.OperatorAccount{},
		live: map[string]db.OperatorSession{}, tokens: map[uuid.UUID]string{}, locked: map[uuid.UUID]bool{}, fail: map[string]error{},
		overviews: map[uuid.UUID]db.TenantOverview{}, inventories: map[uuid.UUID]fakeInventory{},
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

// LegalVersions answers as op_begin_read + op_read_legal_versions do: a dead session is
// ErrOperatorRefused; otherwise the versions newest first, at most page.Size.
func (f *fakeStore) LegalVersions(_ context.Context, h string, page db.LegalVersionsPage) ([]db.LegalVersion, error) {
	out, hook, err := f.legalVersions(h, page)
	if err == nil && hook != nil {
		hook()
	}
	return out, err
}

func (f *fakeStore) legalVersions(h string, page db.LegalVersionsPage) ([]db.LegalVersion, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("LegalVersions"); err != nil {
		return nil, nil, err
	}
	if _, ok := f.live[h]; !ok {
		return nil, nil, db.ErrOperatorRefused
	}
	f.pages = append(f.pages, page)
	var out []db.LegalVersion
	for i := len(f.versions) - 1; i >= 0 && len(out) < int(page.Size); i-- {
		out = append(out, f.versions[i])
	}
	return out, f.afterVersions, nil
}

// PublishLegal answers as op_publish_legal does: a cancelled context is its error (a
// statement on a cancelled context does not run); a dead session is ErrOperatorRefused;
// otherwise the version is appended, published by the session's operator, and the
// document's previous version stops being current.
func (f *fakeStore) PublishLegal(ctx context.Context, h, slug, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforePublish != nil {
		f.beforePublish()
	}
	if err := f.enter("PublishLegal"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s, ok := f.live[h]
	if !ok {
		return db.ErrOperatorRefused
	}
	// A minute apart: the screen prints times to the minute, so two versions' times are
	// told apart on the page (the behind warning's LiveAt is measured that way).
	at := time.Unix(1_900_000_000+60*int64(len(f.versions)), 0).UTC()
	f.published = append(f.published, fakePublication{hash: h, slug: slug, body: body, at: at})
	for i := range f.versions {
		if f.versions[i].Slug == slug {
			f.versions[i].Current = false
		}
	}
	admin, name := s.AdminID, "Fake Operator "+s.AdminID.String()[:4]
	f.versions = append(f.versions, db.LegalVersion{
		ID: uuid.New(), Slug: slug, PublishedAt: at,
		BodyBytes: int32(len(body)), PublishedBy: db.LegalPublishedByOperator, PublisherID: &admin, PublisherName: &name,
		Current: true,
	})
	if f.afterPublish != nil {
		f.afterPublish()
	}
	return nil
}

// TenantList answers as op_begin_read + op_read_tenants do, through internal/db's
// TenantList: a term internal/db will not send (invalid UTF-8, a NUL, more than
// db.MaxTenantSearchRunes characters) is ErrTenantSearchRefused -- the call COUNTED, so a
// screen that leaves the refusal to internal/db is seen calling the store; a dead session
// is ErrOperatorRefused; otherwise the tenants whose name holds the term (case aside),
// whose id is the term, or all of them for "", paged by Number and Size. (The address
// branch is the database's: internal/db's tests measure it.)
func (f *fakeStore) TenantList(_ context.Context, h string, q db.TenantListQuery) ([]db.TenantSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("TenantList"); err != nil {
		return nil, err
	}
	if !utf8.ValidString(q.Search) || strings.ContainsRune(q.Search, 0) || utf8.RuneCountInString(q.Search) > db.MaxTenantSearchRunes {
		return nil, db.ErrTenantSearchRefused
	}
	if _, ok := f.live[h]; !ok {
		return nil, db.ErrOperatorRefused
	}
	f.queries = append(f.queries, q)
	var found []db.TenantSummary
	for _, x := range f.tenants {
		switch {
		case f.matches != nil:
			if f.matches(q.Search, x) {
				found = append(found, x)
			}
		case q.Search == "" || strings.Contains(strings.ToLower(x.Name), strings.ToLower(q.Search)) || x.ID.String() == strings.ToLower(q.Search):
			found = append(found, x)
		}
	}
	from := int(q.Number-1) * int(q.Size)
	if from >= len(found) {
		return nil, nil
	}
	return found[from:min(from+int(q.Size), len(found))], nil
}

// TenantDetail answers as op_begin_read + op_read_tenant_detail do: a dead session is
// ErrOperatorRefused; an id the fake holds no overview for is ErrNoSuchTenant.
func (f *fakeStore) TenantDetail(_ context.Context, h string, id uuid.UUID) (db.TenantOverview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("TenantDetail"); err != nil {
		return db.TenantOverview{}, err
	}
	if _, ok := f.live[h]; !ok {
		return db.TenantOverview{}, db.ErrOperatorRefused
	}
	f.details = append(f.details, id)
	o, ok := f.overviews[id]
	if !ok {
		return db.TenantOverview{}, db.ErrNoSuchTenant
	}
	return o, nil
}

// TenantPlaques answers as op_begin_read + op_read_tenant_plaques do: a dead session is
// ErrOperatorRefused; an id the fake holds no inventory for is ErrNoSuchTenant; otherwise
// the tenant's name, its first db.MaxTenantPlaques plaques in the order the fake holds them
// (a test puts them in the list's order) and the total -- the plaque fields only, never the
// key columns beside them.
func (f *fakeStore) TenantPlaques(_ context.Context, h string, id uuid.UUID) (db.TenantPlaqueInventory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enter("TenantPlaques"); err != nil {
		return db.TenantPlaqueInventory{}, err
	}
	if _, ok := f.live[h]; !ok {
		return db.TenantPlaqueInventory{}, db.ErrOperatorRefused
	}
	f.plaqueAsks = append(f.plaqueAsks, id)
	x, ok := f.inventories[id]
	if !ok {
		return db.TenantPlaqueInventory{}, db.ErrNoSuchTenant
	}
	inv := db.TenantPlaqueInventory{TenantID: id, TenantName: x.name, Total: int64(len(x.plaques))}
	if x.total != 0 {
		inv.Total = x.total
	}
	if x.asTenant != uuid.Nil {
		inv.TenantID = x.asTenant
	}
	for i, p := range x.plaques {
		if i == db.MaxTenantPlaques {
			break
		}
		inv.Plaques = append(inv.Plaques, p.TenantPlaque)
	}
	return inv, nil
}

// fakeTexts is operator.LegalTexts: the snapshot (seeded by a test, or filled by a
// refresh) and its refresh, which reads the fake store's publications -- the newest per
// document wins, as ListPublishedLegalDocuments does, with the version's own time. A
// refresh on a cancelled context fails with the context's error, as a query would; each
// refresh records whether its context carried a deadline and how far off it was.
type fakeTexts struct {
	mu        sync.Mutex
	store     *fakeStore
	docs      map[string]legal.Doc
	refreshes int
	fail      error           // Refresh returns it instead of reading
	deadlines []time.Duration // per refresh: time left to the context's deadline, -1 for none
	// beforeRefresh runs inside Refresh before it reads the store -- a test's hook for a
	// publication landing between the screen's list read and its heal (round 3). It may
	// call the store's PublishLegal.
	beforeRefresh func()
}

func newFakeTexts(store *fakeStore) *fakeTexts {
	return &fakeTexts{store: store, docs: map[string]legal.Doc{}}
}

func (t *fakeTexts) Published() map[string]legal.Doc {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]legal.Doc, len(t.docs))
	for k, v := range t.docs {
		out[k] = v
	}
	return out
}

func (t *fakeTexts) Refresh(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.refreshes++
	left := time.Duration(-1)
	if d, ok := ctx.Deadline(); ok {
		left = time.Until(d)
	}
	t.deadlines = append(t.deadlines, left)
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.fail != nil {
		return t.fail
	}
	if t.beforeRefresh != nil {
		t.beforeRefresh()
	}
	t.store.mu.Lock()
	pubs := append([]fakePublication(nil), t.store.published...)
	t.store.mu.Unlock()
	for _, p := range pubs {
		t.docs[p.slug] = legal.Doc{Slug: p.slug, Body: p.body, PublishedAt: p.at, Paragraphs: legal.Paragraphs(p.body)}
	}
	return nil
}

// put seeds the snapshot with a published text (a test whose subject is the read).
func (t *fakeTexts) put(slug, body string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.docs[slug] = legal.Doc{Slug: slug, Body: body, PublishedAt: time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC),
		Paragraphs: legal.Paragraphs(body)}
}

func (t *fakeTexts) refreshCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.refreshes
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
	texts   *fakeTexts
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

// lockedWriter serialises the writes of the rig's two log handlers (text and JSON, each
// with its own lock) into one buffer, so requests the rig serves CONCURRENTLY
// (TestReadBudget_ConcurrentReadsOfOneSessionStopAtTheLimit) write their access records
// without a data race. A test reads the buffer when no request is in flight.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// newRigAt is newRig with its process and access log at level.
func newRigAt(t *testing.T, level slog.Level) *rig {
	t.Helper()
	g := &rig{t: t, store: newFakeStore(), kek: randBytes(t, 32), now: time.Unix(1_900_000_005, 0), logs: &bytes.Buffer{}}
	g.texts = newFakeTexts(g.store)
	log := captureAt(&lockedWriter{w: g.logs}, level)
	auth, err := operatorauth.New(g.store, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(g.kek), TokenHMACKey: operatorauth.NewKey(randBytes(t, 32)),
		Now: func() time.Time { return g.now }, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := operator.New(auth, g.store, g.store, g.store, g.texts, opHost, opBase, log)
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
	cookieLine         string          // a SECOND Cookie header line, after the cookies' own
	remote             string          // "" = 192.0.2.10:4000
	ctx                context.Context // nil = the request's own
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
	if r.ctx != nil {
		hr = hr.WithContext(r.ctx)
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
