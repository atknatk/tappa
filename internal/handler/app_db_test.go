package handler

// app_db_test.go -- /app/v1 (M16-07) against a REAL Postgres, through a real router, with the REAL
// appdevice verifier, the REAL invitation manager, the REAL sun verifier (genuine SUN URLs from
// signedTapURL, AN12196-pinned), the REAL checkin service and policy set. A test client signs every
// request with a fresh P-256 key, exactly as a phone would (the canonical texts come from
// appdevice's exported builders; the signatures from crypto/ecdsa).
//
// What it proves that a fake could not: the activation writes NO transactions row and consumes
// exactly once under N concurrent completes; the app's taps land on the same rows the browser's do
// (§5 rows 3-7, holding), carry client = 'app', and a revoked device writes nothing; Q40 (a)'s two
// halves against the real sessions and app_devices rows.
//
// Fixtures are not cleaned up (tappa_app cannot DELETE tags, transactions, devices); fresh ids.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/appdevice"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/test/fixtures"
)

// appHarness is the tap harness plus the app surface on one router, with the app's and the
// activation's log lines captured.
type appHarness struct {
	*tapHarness
	app     *App
	devices *appdevice.Verifier
	router  http.Handler
	logs    *syncBuffer
	ctrMu   sync.Mutex
	ctr     uint32
}

func newAppHarness(t *testing.T) *appHarness {
	t.Helper()
	th := newTapHarness(t) // skips without DATABASE_URL
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	verifier := sun.NewVerifier(th.data, config.NewKey(kek))
	act, err := NewActivation(th.invites, th.sessions, verifier, th.trail, dbBrandReader(t, th.data), th.cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := appdevice.New(th.data)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := tenant.NewDirectory(th.data, th.cfg.BusinessDayFrom)
	if err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(devices, th.invites, verifier, dir, th.checkins, act, th.trail, th.cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	th.tap.Mount(r)
	act.Mount(r)
	app.Mount(r)
	return &appHarness{tapHarness: th, app: app, devices: devices, router: r, logs: logs, ctr: uint32(th.startCtr)}
}

// nextSUN is the plaque's next genuine read, as the query the app sends.
func (h *appHarness) nextSUN(t *testing.T) string {
	t.Helper()
	h.ctrMu.Lock()
	h.ctr++
	c := h.ctr
	h.ctrMu.Unlock()
	return sunQueryOf(t, signedTapURL(t, tapFakeTagKey, h.tagUID, c))
}

func sunQueryOf(t *testing.T, path string) string {
	t.Helper()
	_, q, ok := strings.Cut(path, "?")
	if !ok {
		t.Fatalf("a tap path without a query")
	}
	return q
}

// appPhone is a phone: its private key (which never leaves it) and, once active, its device id.
type appPhone struct {
	key *ecdsa.PrivateKey
	id  uuid.UUID
}

func newAppPhone(t *testing.T) *appPhone {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &appPhone{key: k}
}

func (p *appPhone) public(t *testing.T) []byte {
	t.Helper()
	raw, err := p.key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (p *appPhone) publicWire(t *testing.T) string {
	return base64.RawURLEncoding.EncodeToString(p.public(t))
}

func (p *appPhone) sign(t *testing.T, text []byte) string {
	t.Helper()
	d := sha256.Sum256(text)
	sig, err := ecdsa.SignASN1(rand.Reader, p.key, d[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(sig)
}

// register gives employeeID an ACTIVE device holding p's key, through the product's query.
func (h *appHarness) register(t *testing.T, p *appPhone, employeeID uuid.UUID) {
	t.Helper()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		label := "iPhone"
		row, e := store.New(tx).CreateAppDevice(ctx, store.CreateAppDeviceParams{
			TenantID: h.tenantID, EmployeeID: employeeID, Platform: "ios", PublicKey: p.public(t), DeviceLabel: &label,
		})
		p.id = row.ID
		return e
	})
	if err != nil {
		t.Fatalf("register a device: %v", err)
	}
}

func (h *appHarness) serve(req *http.Request) *httptest.ResponseRecorder {
	if req.RemoteAddr == "" || req.RemoteAddr == "192.0.2.1:1234" {
		req.RemoteAddr = "203.0.113.5:41234" // inside the fixture venue's 203.0.113.0/24
	}
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

// read sends a signed read at the given instant.
func (h *appHarness) readAt(t *testing.T, p *appPhone, method, path, body string, at time.Time) *httptest.ResponseRecorder {
	t.Helper()
	ts := strconv.FormatInt(at.Unix(), 10)
	text, err := appdevice.CanonicalReadText(method, path, []byte(body), ts, p.id.String())
	if err != nil {
		t.Fatalf("canonical read: %v", err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(appdevice.HeaderDeviceID, p.id.String())
	req.Header.Set(appdevice.HeaderTimestamp, ts)
	req.Header.Set(appdevice.HeaderSignature, p.sign(t, text))
	return h.serve(req)
}

func (h *appHarness) read(t *testing.T, p *appPhone, method, path, body string) *httptest.ResponseRecorder {
	return h.readAt(t, p, method, path, body, time.Now())
}

// nonce asks for a nonce (a signed read) and returns it.
func (h *appHarness) nonce(t *testing.T, p *appPhone) string {
	t.Helper()
	w := h.read(t, p, http.MethodPost, AppNoncePath, "")
	if w.Code != http.StatusOK {
		t.Fatalf("POST %s = %d %s", AppNoncePath, w.Code, w.Body.String())
	}
	var out appNonceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Nonce
}

// signedRequest builds a nonce-signed request (without sending it).
func (h *appHarness) signedRequest(t *testing.T, p *appPhone, deviceID, nonce, path, body string) *http.Request {
	t.Helper()
	text, err := appdevice.CanonicalText(http.MethodPost, path, []byte(body), nonce, deviceID)
	if err != nil {
		t.Fatalf("canonical text: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(appdevice.HeaderDeviceID, deviceID)
	req.Header.Set(appdevice.HeaderNonce, nonce)
	req.Header.Set(appdevice.HeaderSignature, p.sign(t, text))
	return req
}

// post sends a nonce-signed request with a fresh nonce.
func (h *appHarness) post(t *testing.T, p *appPhone, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return h.serve(h.signedRequest(t, p, p.id.String(), h.nonce(t, p), path, body))
}

// tapStart sends tap/start for a SUN query and returns the decoded answer (and the recorder).
func (h *appHarness) tapStart(t *testing.T, p *appPhone, sunQuery string) (appTapStartResponse, *httptest.ResponseRecorder) {
	t.Helper()
	body, _ := json.Marshal(appTapStartRequest{SUN: sunQuery})
	w := h.read(t, p, http.MethodPost, AppTapStartPath, string(body))
	var out appTapStartResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out, w
}

// tap presses: tap/start with the next genuine read, then the nonce-signed tap.
func (h *appHarness) tap(t *testing.T, p *appPhone, extra map[string]any) (appTapResponse, *httptest.ResponseRecorder) {
	t.Helper()
	start, w := h.tapStart(t, p, h.nextSUN(t))
	if w.Code != http.StatusOK {
		t.Fatalf("tap/start = %d %s", w.Code, w.Body.String())
	}
	return h.tapWith(t, p, start.Context, extra)
}

func (h *appHarness) tapWith(t *testing.T, p *appPhone, signedCtx string, extra map[string]any) (appTapResponse, *httptest.ResponseRecorder) {
	t.Helper()
	in := map[string]any{"context": signedCtx}
	for k, v := range extra {
		in[k] = v
	}
	body, _ := json.Marshal(in)
	w := h.post(t, p, AppTapPath, string(body))
	var out appTapResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out, w
}

// invitedEmployee adds an 'invited' person and returns them with a fresh invitation code.
func (h *appHarness) invitedEmployee(t *testing.T, status string) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Rita Vella', $4, now())`, id, h.tenantID, h.locationID, status); e != nil {
			return e
		}
		_, e := fixtures.GiveFarDayCutoff(ctx, tx, h.tenantID, id, time.Now())
		return e
	})
	if err != nil {
		t.Fatalf("invited employee: %v", err)
	}
	return id, h.inviteCodeFor(t, id)
}

func (h *appHarness) inviteCodeFor(t *testing.T, employeeID uuid.UUID) string {
	t.Helper()
	ch := &linkChannel{}
	if _, err := h.invites.IssueAndDeliver(context.Background(), invite.IssueParams{
		TenantID: h.tenantID, EmployeeID: employeeID,
	}, ch); err != nil {
		t.Fatalf("IssueAndDeliver: %v", err)
	}
	return codeFromLink(t, ch.url)
}

func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (h *appHarness) unsigned(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return h.serve(httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
}

// bind binds p's key to code and returns the challenge.
func (h *appHarness) bind(t *testing.T, p *appPhone, code string) appBindResponse {
	t.Helper()
	w := h.unsigned(t, AppActivateBindPath, jsonBody(t, appBindRequest{Code: code, Consent: true, PublicKey: p.publicWire(t)}))
	if w.Code != http.StatusOK {
		t.Fatalf("bind = %d %s", w.Code, w.Body.String())
	}
	var out appBindResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// completeRequest is the complete request p signs over bind's challenge, for a SUN query.
func (h *appHarness) completeRequest(t *testing.T, signer, named *appPhone, b appBindResponse, code, sunQuery string) *http.Request {
	t.Helper()
	body := jsonBody(t, appCompleteRequest{Code: code, PublicKey: named.publicWire(t), Platform: "android", SUN: sunQuery})
	return h.signedRequest(t, signer, b.ActivationID, b.Challenge, AppActivateCompletePath, body)
}

func (h *appHarness) employeeStatus(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM employees WHERE tenant_id = $1 AND id = $2`, h.tenantID, id).Scan(&s)
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// liveDevices counts the person's unrevoked app devices; liveSessions their unrevoked sessions.
func (h *appHarness) liveDevices(t *testing.T, employeeID uuid.UUID) int {
	t.Helper()
	return h.countWhere(t, `SELECT count(*) FROM app_devices WHERE tenant_id = $1 AND employee_id = $2 AND revoked_at IS NULL`, employeeID)
}

func (h *appHarness) liveSessions(t *testing.T, employeeID uuid.UUID) int {
	t.Helper()
	return h.countWhere(t, `SELECT count(*) FROM sessions WHERE tenant_id = $1 AND employee_id = $2 AND revoked_at IS NULL`, employeeID)
}

func (h *appHarness) countWhere(t *testing.T, q string, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, q, h.tenantID, employeeID).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// ---------------------------------------------------------------------------------------------

// TestAppDB_ActivationBindsThenCompletesOnTheFirstTouchAndWritesNoRecord is the card's two-step
// activation end to end: lookup names the person and the employer; bind spends nothing; complete,
// signed by the bound key over the challenge with the first genuine NFC read, advances the counter
// once, activates the person, creates ONE live device -- and writes no transactions row. The new
// device then reads /me.
func TestAppDB_ActivationBindsThenCompletesOnTheFirstTouchAndWritesNoRecord(t *testing.T) {
	h := newAppHarness(t)
	emp, code := h.invitedEmployee(t, "invited")
	before := h.transactionCount(t)

	look := h.unsigned(t, AppActivateLookupPath, jsonBody(t, appLookupRequest{Code: code}))
	if look.Code != http.StatusOK || !strings.Contains(look.Body.String(), "Rita Vella") || !strings.Contains(look.Body.String(), "Kebab Factory Ltd") {
		t.Fatalf("lookup = %d %s", look.Code, look.Body.String())
	}
	p := newAppPhone(t)
	b := h.bind(t, p, code)
	if h.employeeStatus(t, emp) != "invited" || h.liveDevices(t, emp) != 0 {
		t.Fatal("bind activated somebody or created a device")
	}
	ctrBefore := h.lastCtr(t, h.tagUID)
	w := h.serve(h.completeRequest(t, p, p, b, code, h.nextSUN(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", w.Code, w.Body.String())
	}
	var done appCompleteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &done); err != nil {
		t.Fatal(err)
	}
	p.id = uuid.MustParse(done.DeviceID)
	if got := h.lastCtr(t, h.tagUID); got != ctrBefore+1 {
		t.Errorf("last_ctr = %d, want %d: the activating read advances the counter once", got, ctrBefore+1)
	}
	if h.employeeStatus(t, emp) != "active" || h.liveDevices(t, emp) != 1 {
		t.Errorf("after complete: status %q, %d live devices; want active, 1", h.employeeStatus(t, emp), h.liveDevices(t, emp))
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions went %d -> %d: the activating touch is not attendance (ADR 0026)", before, after)
	}
	if n := h.auditCount(t, ActionActivationCompleted); n < 1 {
		t.Error("no activation.completed row")
	}
	me := h.read(t, p, http.MethodGet, AppMePath, "")
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), "Rita Vella") {
		t.Fatalf("GET /me with the new device = %d %s", me.Code, me.Body.String())
	}
	// The spent invitation completes nothing again (a replay of the whole request).
	again := h.serve(h.completeRequest(t, p, p, b, code, h.nextSUN(t)))
	if again.Code != http.StatusBadRequest || strings.TrimSpace(again.Body.String()) != `{"error":"invalid_invite"}` {
		t.Errorf("a second complete = %d %s, want 400 invalid_invite", again.Code, again.Body.String())
	}
	if h.liveDevices(t, emp) != 1 {
		t.Error("a spent invitation created a second device")
	}
}

// TestAppDB_ConcurrentCompletesOfOneReadActivateExactlyOnce: the card's concurrency criterion --
// N goroutines send the SAME complete request (one (tag, ctr)) at once: exactly one 200, one live
// device, the counter advanced by exactly one, no transactions row. -race.
func TestAppDB_ConcurrentCompletesOfOneReadActivateExactlyOnce(t *testing.T) {
	h := newAppHarness(t)
	emp, code := h.invitedEmployee(t, "invited")
	p := newAppPhone(t)
	b := h.bind(t, p, code)
	sunQuery := h.nextSUN(t)
	ctrBefore := h.lastCtr(t, h.tagUID)
	before := h.transactionCount(t)

	const n = 8
	reqs := make([]*http.Request, n)
	for i := range reqs {
		reqs[i] = h.completeRequest(t, p, p, b, code, sunQuery)
	}
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes[i] = h.serve(reqs[i]).Code
		}()
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("%d of %d concurrent completes succeeded (%v), want exactly 1", ok, n, codes)
	}
	if h.liveDevices(t, emp) != 1 {
		t.Errorf("%d live devices, want 1", h.liveDevices(t, emp))
	}
	if got := h.lastCtr(t, h.tagUID); got != ctrBefore+1 {
		t.Errorf("last_ctr = %d, want %d", got, ctrBefore+1)
	}
	if after := h.transactionCount(t); after != before {
		t.Errorf("transactions went %d -> %d", before, after)
	}
}

// TestAppDB_CompleteWithoutTheBoundKeyIsRefusedBeforeAnythingMoves: proof of possession first.
// A request signed by another key than the one the body names is 401 with an empty body; a body
// naming a key that was never bound (signed by that key) gets past the signature but its binding
// matches nothing -- and in both cases the invitation survives and the device is not created.
// The first refusal moves no counter.
func TestAppDB_CompleteWithoutTheBoundKeyIsRefusedBeforeAnythingMoves(t *testing.T) {
	h := newAppHarness(t)
	emp, code := h.invitedEmployee(t, "invited")
	owner, thief := newAppPhone(t), newAppPhone(t)
	b := h.bind(t, owner, code)

	ctrBefore := h.lastCtr(t, h.tagUID)
	w := h.serve(h.completeRequest(t, thief, owner, b, code, h.nextSUN(t)))
	if w.Code != http.StatusUnauthorized || w.Body.Len() != 0 {
		t.Fatalf("a signature by another key = %d %q, want 401 with an empty body", w.Code, w.Body.String())
	}
	if h.lastCtr(t, h.tagUID) != ctrBefore {
		t.Error("a refused proof of possession moved the counter")
	}
	w = h.serve(h.completeRequest(t, thief, thief, b, code, h.nextSUN(t)))
	if w.Code != http.StatusConflict || strings.TrimSpace(w.Body.String()) != `{"error":"activation_not_ready"}` {
		t.Fatalf("an unbound key = %d %s, want 409 activation_not_ready", w.Code, w.Body.String())
	}
	if h.employeeStatus(t, emp) != "invited" || h.liveDevices(t, emp) != 0 {
		t.Fatal("an unbound key activated somebody")
	}
	// The owner still completes.
	if w := h.serve(h.completeRequest(t, owner, owner, b, code, h.nextSUN(t))); w.Code != http.StatusOK {
		t.Fatalf("the bound key = %d %s", w.Code, w.Body.String())
	}
}

// TestAppDB_ASecondBindMovesTheBinding: ADR 0026's rule for the app -- the later bind wins and the
// earlier key's complete is refused without consuming anything.
func TestAppDB_ASecondBindMovesTheBinding(t *testing.T) {
	h := newAppHarness(t)
	emp, code := h.invitedEmployee(t, "invited")
	first, second := newAppPhone(t), newAppPhone(t)
	b1 := h.bind(t, first, code)
	b2 := h.bind(t, second, code)
	if w := h.serve(h.completeRequest(t, first, first, b1, code, h.nextSUN(t))); w.Code != http.StatusConflict {
		t.Fatalf("the moved binding's complete = %d %s, want 409", w.Code, w.Body.String())
	}
	if h.employeeStatus(t, emp) != "invited" {
		t.Fatal("the moved binding activated the person")
	}
	if w := h.serve(h.completeRequest(t, second, second, b2, code, h.nextSUN(t))); w.Code != http.StatusOK {
		t.Fatalf("the current binding's complete = %d %s", w.Code, w.Body.String())
	}
}

// TestAppDB_ARegisteredKeyAsksForANewKeyAndSpendsNothing: the M16-03 review's 23505 rule -- a key
// already registered (here: another person's live device) is refused with new_key_required, and the
// one transaction rolled back the consumption: the invitation still completes with a NEW key.
func TestAppDB_ARegisteredKeyAsksForANewKeyAndSpendsNothing(t *testing.T) {
	h := newAppHarness(t)
	emp, code := h.invitedEmployee(t, "invited")
	reused := newAppPhone(t)
	h.register(t, reused, h.employeeID)
	b := h.bind(t, reused, code)
	w := h.serve(h.completeRequest(t, reused, reused, b, code, h.nextSUN(t)))
	if w.Code != http.StatusConflict || strings.TrimSpace(w.Body.String()) != `{"error":"new_key_required"}` {
		t.Fatalf("a registered key = %d %s, want 409 new_key_required", w.Code, w.Body.String())
	}
	if h.employeeStatus(t, emp) != "invited" || h.liveDevices(t, emp) != 0 || h.liveDevices(t, h.employeeID) != 1 {
		t.Fatal("the refused activation changed somebody's state")
	}
	fresh := newAppPhone(t)
	b2 := h.bind(t, fresh, code)
	if w := h.serve(h.completeRequest(t, fresh, fresh, b2, code, h.nextSUN(t))); w.Code != http.StatusOK {
		t.Fatalf("a new key after new_key_required = %d %s", w.Code, w.Body.String())
	}
}

// TestAppDB_OneWhoCredentialPerPerson is Q40 (a) (the user's decision of 2026-11-14), both halves
// on real rows: an APP activation revokes the person's browser sessions and their older app device;
// a BROWSER activation (the wizard's consumption, invite.Manager.Activate) revokes their app
// devices. A QR read cannot complete an app activation (no touch).
func TestAppDB_OneWhoCredentialPerPerson(t *testing.T) {
	h := newAppHarness(t)
	emp := h.newEmployee(t, "active")
	h.cookieForEmployee(t, emp)
	h.cookieForEmployee(t, emp)
	old := newAppPhone(t)
	h.register(t, old, emp)
	if h.liveSessions(t, emp) != 2 || h.liveDevices(t, emp) != 1 {
		t.Fatal("PREMISE: two sessions and one device")
	}

	code := h.inviteCodeFor(t, emp)
	p := newAppPhone(t)
	b := h.bind(t, p, code)
	qr := h.serve(h.completeRequest(t, p, p, b, code, "tag="+h.tagUID))
	if qr.Code != http.StatusBadRequest || strings.TrimSpace(qr.Body.String()) != `{"error":"activation_needs_touch"}` {
		t.Fatalf("a QR complete = %d %s", qr.Code, qr.Body.String())
	}
	w := h.serve(h.completeRequest(t, p, p, b, code, h.nextSUN(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", w.Code, w.Body.String())
	}
	if s, d := h.liveSessions(t, emp), h.liveDevices(t, emp); s != 0 || d != 1 {
		t.Fatalf("after the app activation: %d live sessions, %d live devices; want 0 and 1 (the new one)", s, d)
	}
	if h.auditCount(t, ActionDeviceReplaced) < 1 {
		t.Error("no activation.device_replaced row for the replaced credentials")
	}

	// The browser half: the wizard's consumption signs the app out.
	code2 := h.inviteCodeFor(t, emp)
	binding := strings.Repeat("A", 43)
	if _, err := h.invites.RecordConsent(context.Background(), invite.ParseCode(code2), invite.ParseBinding(binding)); err != nil {
		t.Fatalf("RecordConsent: %v", err)
	}
	act, err := h.invites.Activate(context.Background(), invite.ParseCode(code2), invite.ParseBinding(binding))
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if act.AppDevicesRevoked != 1 || h.liveDevices(t, emp) != 0 {
		t.Fatalf("the browser activation revoked %d app devices, %d left live; want 1 and 0", act.AppDevicesRevoked, h.liveDevices(t, emp))
	}
}

// TestAppDB_TheAppTapIsTheBrowsersTapWithClientApp: tap/start is GET /t's twin (no record, the
// counter does not move), tap is POST /api/checkin's (one record: §5 row 6 by IP -> ok, client =
// 'app', the counter advanced once); the same request again is 401 (its nonce is spent) and writes
// nothing; a second press inside the window is row 5's RECORDED ignored.
func TestAppDB_TheAppTapIsTheBrowsersTapWithClientApp(t *testing.T) {
	h := newAppHarness(t)
	p := newAppPhone(t)
	h.register(t, p, h.employeeID)
	before := h.countFor(t, h.employeeID)

	sunQuery := h.nextSUN(t)
	ctrBefore := h.lastCtr(t, h.tagUID)
	start, w := h.tapStart(t, p, sunQuery)
	if w.Code != http.StatusOK || start.EmployeeName != "Maria Borg" || start.LocationName != "St Julians" || start.Context == "" {
		t.Fatalf("tap/start = %d %s", w.Code, w.Body.String())
	}
	if h.lastCtr(t, h.tagUID) != ctrBefore || h.countFor(t, h.employeeID) != before {
		t.Fatal("tap/start moved the counter or wrote a record")
	}

	body := jsonBody(t, map[string]any{"context": start.Context, "want": "in"})
	req := h.signedRequest(t, p, p.id.String(), h.nonce(t, p), AppTapPath, body)
	replay := req.Clone(context.Background())
	replay.Body = io.NopCloser(strings.NewReader(body))
	w = h.serve(req)
	if w.Code != http.StatusOK {
		t.Fatalf("tap = %d %s", w.Code, w.Body.String())
	}
	var res appTapResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "ok" || res.Outcome != "recorded" {
		t.Fatalf("tap answered %+v, want a recorded ok", res)
	}
	rec := h.lastRecord(t, h.employeeID)
	if c := h.clientOf(t, rec.ID); c == nil || *c != "app" {
		t.Fatalf("transactions.client = %s, want 'app'", clientText(c))
	}
	if rec.Channel != "nfc" || h.lastCtr(t, h.tagUID) != ctrBefore+1 {
		t.Errorf("channel %q, last_ctr %d; want nfc and %d", rec.Channel, h.lastCtr(t, h.tagUID), ctrBefore+1)
	}
	if w := h.serve(replay); w.Code != http.StatusUnauthorized || w.Body.Len() != 0 {
		t.Fatalf("the replayed tap = %d %q, want 401 with an empty body", w.Code, w.Body.String())
	}
	if n := h.countFor(t, h.employeeID); n != before+1 {
		t.Fatalf("%d records after the replay, want %d", n, before+1)
	}
	again, w := h.tap(t, p, nil)
	if w.Code != http.StatusOK || again.Verdict != "ignored" {
		t.Fatalf("a second press inside the window = %d %+v, want a recorded ignored (§5 row 5)", w.Code, again)
	}
	if n := h.countFor(t, h.employeeID); n != before+2 {
		t.Fatalf("%d records, want %d: row 5 is recorded", n, before+2)
	}
}

// TestAppDB_RowsThreeAndFourLandAsInTheBrowser: a DEACTIVATED person's device is not revoked and
// verifies, so the tap reaches §5 row 4 -- reject, RECORDED, client 'app'; a REVOKED device is row
// 3's twin -- 401 with an empty body, no record, and the warning line.
func TestAppDB_RowsThreeAndFourLandAsInTheBrowser(t *testing.T) {
	h := newAppHarness(t)
	gone := h.newEmployee(t, "deactivated")
	pg := newAppPhone(t)
	h.register(t, pg, gone)
	res, w := h.tap(t, pg, nil)
	if w.Code != http.StatusOK || res.Verdict != "reject" {
		t.Fatalf("a deactivated person's app tap = %d %+v, want a recorded reject (§5 row 4)", w.Code, res)
	}
	rec := h.lastRecord(t, gone)
	if rec.MatchedSid == nil || *rec.MatchedSid != "sys:employee-deactivated" {
		t.Errorf("matched_sid = %v, want sys:employee-deactivated", rec.MatchedSid)
	}
	if c := h.clientOf(t, rec.ID); c == nil || *c != "app" {
		t.Errorf("client = %s, want 'app'", clientText(c))
	}

	pr := newAppPhone(t)
	h.register(t, pr, h.employeeID)
	start, w := h.tapStart(t, pr, h.nextSUN(t))
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	before := h.countFor(t, h.employeeID)
	nonce := h.nonce(t, pr)
	if err := h.devices.Revoke(context.Background(), appdevice.Device{ID: pr.id, TenantID: h.tenantID, EmployeeID: h.employeeID}); err != nil {
		t.Fatal(err)
	}
	w = h.serve(h.signedRequest(t, pr, pr.id.String(), nonce, AppTapPath, jsonBody(t, map[string]any{"context": start.Context})))
	if w.Code != http.StatusUnauthorized || w.Body.Len() != 0 {
		t.Fatalf("a revoked device's tap = %d %q, want 401 empty", w.Code, w.Body.String())
	}
	if _, w := h.tapStart(t, pr, h.nextSUN(t)); w.Code != http.StatusUnauthorized || w.Body.Len() != 0 {
		t.Fatalf("a revoked device's tap/start = %d %q, want 401 empty", w.Code, w.Body.String())
	}
	if n := h.countFor(t, h.employeeID); n != before {
		t.Fatalf("a revoked device wrote %d records", n-before)
	}
	if !strings.Contains(h.logs.String(), signedOutAppTap) {
		t.Error("no warning line for the signed-out device")
	}
}

// TestAppDB_ASignedOutDevicesTapLeavesOneUnattributedWarning: the row-3 twin's log line (the main
// session's decision of 2026-11-14). A revoked device's tap/start and tap each leave ONE WARN line
// "a signed-out app device tapped" with the channel (the SUN query's for tap/start, "unverified"
// for tap -- its channel is inside the signed context) and the first 16 hex characters of SHA-256 of
// the presented id -- and NOTHING that names a business or a person (the id header is unsigned at
// that point), nor the id, the nonce or the signature. An UNKNOWN id gets the byte-identical 401
// and no such line.
func TestAppDB_ASignedOutDevicesTapLeavesOneUnattributedWarning(t *testing.T) {
	h := newAppHarness(t)
	p := newAppPhone(t)
	h.register(t, p, h.employeeID)
	start, w := h.tapStart(t, p, h.nextSUN(t))
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	nonce := h.nonce(t, p)
	if err := h.devices.Revoke(context.Background(), appdevice.Device{ID: p.id, TenantID: h.tenantID, EmployeeID: h.employeeID}); err != nil {
		t.Fatal(err)
	}
	mark := len(h.logs.String())

	_, startW := h.tapStart(t, p, h.nextSUN(t))
	tapReq := h.signedRequest(t, p, p.id.String(), nonce, AppTapPath, jsonBody(t, map[string]any{"context": start.Context}))
	sig := tapReq.Header.Get(appdevice.HeaderSignature)
	tapW := h.serve(tapReq)
	stranger := &appPhone{key: newAppPhone(t).key, id: uuid.New()}
	_, unknownW := h.tapStart(t, stranger, h.nextSUN(t))
	for name, rw := range map[string]*httptest.ResponseRecorder{"tap/start": startW, "tap": tapW, "unknown": unknownW} {
		if rw.Code != http.StatusUnauthorized || rw.Body.Len() != 0 {
			t.Fatalf("%s = %d %q, want 401 with an empty body", name, rw.Code, rw.Body.String())
		}
	}
	if startW.Header().Get("Content-Type") != unknownW.Header().Get("Content-Type") {
		t.Error("the revoked and the unknown device's answers differ in their headers")
	}

	var warns []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(h.logs.String()[mark:]), "\n") {
		if l == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		if m["msg"] == signedOutAppTap {
			warns = append(warns, m)
		}
		for _, forbidden := range []string{p.id.String(), stranger.id.String(), nonce, sig, h.tenantID.String(), h.employeeID.String()} {
			if strings.Contains(l, forbidden) {
				t.Errorf("a log line carries an id, the nonce or the signature: %s", l)
			}
		}
	}
	if len(warns) != 2 {
		t.Fatalf("%d %q lines, want 2 (tap/start and tap; none for the unknown id)", len(warns), signedOutAppTap)
	}
	digest := presentedDeviceDigest(p.id)
	for i, wantChannel := range []string{"nfc", "unverified"} {
		m := warns[i]
		if m["level"] != "WARN" || m["channel"] != wantChannel || m["device_digest"] != digest || len(digest) != 16 {
			t.Errorf("line %d = %v, want WARN, channel %q, device_digest %s", i, m, wantChannel, digest)
		}
		for _, key := range []string{"tenant_id", "employee_id", "nonce", "signature", "device_id"} {
			if _, ok := m[key]; ok {
				t.Errorf("line %d carries %q: %v", i, key, m)
			}
		}
		if len(m) != 5 { // time, level, msg, channel, device_digest
			t.Errorf("line %d has %d keys, want exactly time, level, msg, channel and device_digest: %v", i, len(m), m)
		}
	}
}

// TestAppDB_AMockLocationIsReviewed: the signed body's mockLocation reaches the policy context
// (M16-06's key) and base:mock-location-review flags a tap that IP alone would have made ok; the
// same tap without the flag is ok -- the browser's outcome.
func TestAppDB_AMockLocationIsReviewed(t *testing.T) {
	h := newAppHarness(t)
	p := newAppPhone(t)
	h.register(t, p, h.employeeID)
	res, w := h.tap(t, p, map[string]any{"mockLocation": true, "lat": 35.918, "lng": 14.489})
	if w.Code != http.StatusOK || res.Verdict != "flag" {
		t.Fatalf("a mock-location tap = %d %+v, want flag", w.Code, res)
	}
	rec := h.lastRecord(t, h.employeeID)
	if deref(rec.MatchedSid) != "base:mock-location-review" {
		t.Errorf("matched_sid = %q, want base:mock-location-review", deref(rec.MatchedSid))
	}
	if !strings.Contains(string(rec.PolicyContext), `"tap:mockLocation"`) {
		t.Errorf("policy_context lacks tap:mockLocation: %s", rec.PolicyContext)
	}
	if strings.Contains(string(rec.PolicyContext), "35.918") {
		t.Error("policy_context carries a coordinate")
	}

	other := h.newEmployee(t, "active")
	q := newAppPhone(t)
	h.register(t, q, other)
	res, w = h.tap(t, q, nil)
	if w.Code != http.StatusOK || res.Verdict != "ok" {
		t.Fatalf("the same tap without the flag = %d %+v, want ok", w.Code, res)
	}
}

// TestAppDB_AHoldingTapGivesTheBrowsersResult: a KM person on a KF plaque (ADR 0027) -- the app's
// tap and the browser's tap of the same kind are recorded alike (verdict, sid, plaque, venue).
func TestAppDB_AHoldingTapGivesTheBrowsersResult(t *testing.T) {
	h := newAppHarness(t)
	owner := holdingOwner(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	kf, kfVenue := uuid.New(), uuid.New()
	kfUID := h.newTag(t, kek, kf, kfVenue, 100)
	holdingOf(t, owner, h.tenantID, kf)

	cookie := h.sessionCookieFor(t)
	if w := h.postTap(t, tapContext{UID: kfUID, Ctr: 101, Channel: sun.ChannelNFC, CMACVerified: true,
		TagTenantID: kf, LocationID: kfVenue}, cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("the browser's holding tap = %d", w.Code)
	}
	browser := h.lastRecord(t, h.employeeID)

	other := h.newEmployee(t, "active")
	p := newAppPhone(t)
	h.register(t, p, other)
	start, w := h.tapStart(t, p, sunQueryOf(t, signedTapURL(t, tapFakeTagKey, kfUID, 102)))
	if w.Code != http.StatusOK || start.LocationName == "" {
		t.Fatalf("tap/start on the peer's plaque = %d %s", w.Code, w.Body.String())
	}
	res, w := h.tapWith(t, p, start.Context, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("the app's holding tap = %d %s", w.Code, w.Body.String())
	}
	app := h.lastRecord(t, other)
	appSite, browserSite := h.siteOf(t, app.ID), h.siteOf(t, browser.ID)
	if res.Verdict != browser.Verdict || app.Verdict != browser.Verdict ||
		deref(app.MatchedSid) != deref(browser.MatchedSid) || appSite != browserSite || appSite == "||" {
		t.Fatalf("the app's holding record differs from the browser's:\n app     %s %s %s\n browser %s %s %s",
			app.Verdict, deref(app.MatchedSid), appSite, browser.Verdict, deref(browser.MatchedSid), browserSite)
	}
	if c := h.clientOf(t, app.ID); c == nil || *c != "app" {
		t.Errorf("the app's holding record has client %s", clientText(c))
	}
}

// siteOf is a record's three site columns (ADR 0027), joined for a comparison.
func (h *appHarness) siteOf(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var site string
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT coalesce(site_tenant_id::text, '') || '|' || coalesce(site_location_id::text, '') || '|' || coalesce(site_tag_uid, '')
			FROM transactions WHERE tenant_id = $1 AND id = $2`, h.tenantID, id).Scan(&site)
	})
	if err != nil {
		t.Fatal(err)
	}
	return site
}

// TestAppDB_NothingSecretReachesALogOrABody drives every route once and searches the captured log
// lines and every response for the values §7 never logs: the signature, the nonce, the challenge,
// the public key (raw and hex), the invite code, the SUN read's counter and MAC, the coordinates.
func TestAppDB_NothingSecretReachesALogOrABody(t *testing.T) {
	h := newAppHarness(t)
	_, code := h.invitedEmployee(t, "invited")
	p := newAppPhone(t)
	look := h.unsigned(t, AppActivateLookupPath, jsonBody(t, appLookupRequest{Code: code}))
	b := h.bind(t, p, code)
	sunQuery := h.nextSUN(t)
	req := h.completeRequest(t, p, p, b, code, sunQuery)
	sig := req.Header.Get(appdevice.HeaderSignature)
	w := h.serve(req)
	var done appCompleteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &done); err != nil {
		t.Fatal(err)
	}
	p.id = uuid.MustParse(done.DeviceID)
	n := h.nonce(t, p)
	start, _ := h.tapStart(t, p, h.nextSUN(t))
	tapRes, _ := h.tapWith(t, p, start.Context, map[string]any{"lat": 35.91876, "lng": 14.48912})
	_ = tapRes
	// Refusals too: a bad signature, an unknown code, a stale read.
	h.serve(h.signedRequest(t, newAppPhone(t), p.id.String(), n, AppTapPath, "{}"))
	h.unsigned(t, AppActivateBindPath, jsonBody(t, appBindRequest{Code: code + "x", Consent: true, PublicKey: p.publicWire(t)}))
	h.readAt(t, p, http.MethodGet, AppMePath, "", time.Now().Add(-time.Hour))
	me := h.read(t, p, http.MethodGet, AppMePath, "")

	q := strings.SplitN(sunQuery, "&", 3)
	secrets := map[string]string{
		"signature":      sig,
		"nonce":          n,
		"challenge":      b.Challenge,
		"public key":     p.publicWire(t),
		"public key hex": hex.EncodeToString(p.public(t)),
		"invite code":    code,
		"sun ctr":        q[1],
		"sun cmac":       q[2],
		"latitude":       "35.91876",
		"longitude":      "14.48912",
		"activation id":  b.ActivationID,
		"key digest":     func() string { s := sha256.Sum256(p.public(t)); return hex.EncodeToString(s[:]) }(),
	}
	logs := h.logs.String()
	if logs == "" {
		t.Fatal("PREMISE: nothing was logged; the search would pass vacuously")
	}
	for name, v := range secrets {
		if strings.Contains(logs, v) {
			t.Errorf("the log carries the %s", name)
		}
		for _, body := range []string{look.Body.String(), me.Body.String(), start.Context} {
			if name != "activation id" && strings.Contains(body, v) {
				t.Errorf("a response carries the %s", name)
			}
		}
	}
}

// TestApp_TheSignedPathIsTheWirePath drives the surface through httpx.NewRouter -- the
// composition every feature is mounted in -- and measures the M16-05 review's three routing
// rules: the path the verifier signs is the path on the wire (a trailing slash is NOT cleaned
// into the route: it is a different, unrouted path), the body is the raw bytes (an unsigned
// Content-Encoding header is not acted on), and an unsigned Content-Type changes nothing. Also:
// an unsigned read, a stale read and a read carrying a nonce header are all 401 with an empty body.
func TestApp_TheSignedPathIsTheWirePath(t *testing.T) {
	h := newAppHarness(t)
	h.router = httpx.NewRouter(h.cfg, slog.New(slog.DiscardHandler), h.app)
	p := newAppPhone(t)
	h.register(t, p, h.employeeID)

	if w := h.read(t, p, http.MethodGet, AppMePath, ""); w.Code != http.StatusOK {
		t.Fatalf("PREMISE: a signed /me through the router = %d %s", w.Code, w.Body.String())
	}
	if w := h.read(t, p, http.MethodGet, AppMePath+"/", ""); w.Code != http.StatusNotFound {
		t.Errorf("a signed /me/ = %d, want 404: nothing rewrites the path before the verifier", w.Code)
	}

	body := jsonBody(t, appTapStartRequest{SUN: h.nextSUN(t)})
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	text, err := appdevice.CanonicalReadText(http.MethodPost, AppTapStartPath, []byte(body), ts, p.id.String())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, AppTapStartPath, strings.NewReader(body))
	req.Header.Set(appdevice.HeaderDeviceID, p.id.String())
	req.Header.Set(appdevice.HeaderTimestamp, ts)
	req.Header.Set(appdevice.HeaderSignature, p.sign(t, text))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Content-Encoding", "gzip")
	if w := h.serve(req); w.Code != http.StatusOK {
		t.Errorf("tap/start with unsigned Content-Type/Content-Encoding headers = %d %s, want 200 (they mean nothing)", w.Code, w.Body.String())
	}

	for name, w := range map[string]*httptest.ResponseRecorder{
		"unsigned":    h.serve(httptest.NewRequest(http.MethodGet, AppMePath, nil)),
		"stale":       h.readAt(t, p, http.MethodGet, AppMePath, "", time.Now().Add(-2*appdevice.ReadWindow)),
		"from ahead":  h.readAt(t, p, http.MethodGet, AppMePath, "", time.Now().Add(2*appdevice.ReadWindow)),
		"a stranger":  h.read(t, &appPhone{key: newAppPhone(t).key, id: uuid.New()}, http.MethodGet, AppMePath, ""),
		"another key": h.read(t, &appPhone{key: newAppPhone(t).key, id: p.id}, http.MethodGet, AppMePath, ""),
	} {
		if w.Code != http.StatusUnauthorized || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s read = %d %q (Cache-Control %q), want 401, empty, no-store", name, w.Code, w.Body.String(), w.Header().Get("Cache-Control"))
		}
	}
	mixed := h.signedRequest(t, p, p.id.String(), h.nonce(t, p), AppTapPath, "{}")
	mixed.Header.Set(appdevice.HeaderTimestamp, ts)
	if w := h.serve(mixed); w.Code != http.StatusUnauthorized || w.Body.Len() != 0 {
		t.Errorf("a request carrying both schemes = %d %q, want 401 empty", w.Code, w.Body.String())
	}
}
