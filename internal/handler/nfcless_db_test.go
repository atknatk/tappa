package handler

// nfcless_db_test.go -- M16-23 end to end against real Postgres: a phone that cannot tap a plaque
// finishes its activation with a venue screen's live QR code, or with a manager's approval. The
// browser walks the real wizard (e2e_db_test.go's harness: consent, then the proof), the app the
// real bind; the panel's decision is the real domain (tenant.Staff); every assertion reads rows.
//
// Neither road writes a `transactions` row (CLAUDE.md §5, "Aktivasyon dokunuşu"); every refusal
// with a tenant is an activation.failed row (§4.6); the pairing code and the invite code reach no
// audit detail (§7).

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

type nfcHarness struct {
	*harness
	keys    *liveqr.Keys
	screens *liveqr.Screens
	staff   *tenant.Staff
}

// newNFCLessHarness is the e2e harness with GET /q turned on exactly as cmd/tappa's openLiveQR does
// (Tap.ServeLiveQR before Mount) and the panel's domain beside it.
func newNFCLessHarness(t *testing.T) *nfcHarness {
	t.Helper()
	h := newHarness(t, "invited")
	secret := sha256.Sum256([]byte("m16-23 nfc-less test secret, not a key"))
	keys, err := liveqr.NewKeys(secret[:])
	if err != nil {
		t.Fatal(err)
	}
	screens, err := liveqr.NewScreens(keys, h.data, h.trail)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.tap.ServeLiveQR(keys, h.data); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.tap.Mount(r)
	h.activation.Mount(r)
	h.server.Close()
	h.server = httptest.NewServer(r)
	t.Cleanup(h.server.Close)
	staff, err := tenant.NewStaff(h.data, h.trail, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return &nfcHarness{harness: h, keys: keys, screens: screens, staff: staff}
}

// runningScreenAt creates, pairs and stamps a screen at venue -- a screen that is ON (it asked for a
// code just now), as the gate counts one.
func (h *nfcHarness) runningScreenAt(t *testing.T, tenantID, venue uuid.UUID) liveqr.Screen {
	t.Helper()
	ctx := context.Background()
	created, err := h.screens.Create(ctx, liveqr.CreateCommand{TenantID: tenantID, ActorID: uuid.New(), LocationID: venue})
	if err != nil {
		t.Fatalf("create a screen: %v", err)
	}
	sc, _, err := h.screens.Pair(ctx, created.Code)
	if err != nil {
		t.Fatalf("pair the screen: %v", err)
	}
	if err := h.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).TouchLiveQRDisplay(ctx, store.TouchLiveQRDisplayParams{TenantID: tenantID, ID: sc.ID})
		return e
	}); err != nil {
		t.Fatalf("stamp the screen: %v", err)
	}
	return sc
}

func (h *nfcHarness) tokenAgo(t *testing.T, sc liveqr.Screen, ago time.Duration) string {
	t.Helper()
	tok, err := h.keys.Mint(sc.ID, sc.LocationID, time.Now().Add(-ago), liveqr.TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// consented is a browser that walked the wizard to its consent (step 2), with the invitation's code.
func (h *nfcHarness) consented(t *testing.T) (*http.Client, string) {
	t.Helper()
	code := h.issue(t)
	c := h.client(t)
	h.consent(t, c, code)
	return c, code
}

// ask presses "My phone can't tap" on the waiting screen and returns the answer (redirects
// followed, as a browser does).
func (h *nfcHarness) ask(t *testing.T, c *http.Client) (*http.Response, string) {
	t.Helper()
	page := h.getBody(t, c, "/activate?step=3")
	resp, err := c.PostForm(h.server.URL+ActivationApprovalPath, url.Values{"csrf": {formToken(t, page)}})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	return resp, body(t, resp)
}

// collect presses "Check now" on the approval screen.
func (h *nfcHarness) collect(t *testing.T, c *http.Client) (*http.Response, string) {
	t.Helper()
	page := h.getBody(t, c, ActivationApprovalPath)
	resp, err := c.PostForm(h.server.URL+ActivationApprovalCollectPath, url.Values{"csrf": {formToken(t, page)}})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return resp, body(t, resp)
}

func (h *nfcHarness) approvalState(t *testing.T, c *http.Client) string {
	t.Helper()
	var s struct{ State string }
	if err := json.Unmarshal([]byte(h.getBody(t, c, ActivationApprovalStatusPath)), &s); err != nil {
		t.Fatalf("approval status body: %v", err)
	}
	return s.State
}

// request is the person's newest request: its id and pairing code (read as tappa_app).
func (h *nfcHarness) request(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	var (
		id   uuid.UUID
		code string
	)
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id, pairing_code FROM activation_requests WHERE tenant_id = $1 AND employee_id = $2
			ORDER BY created_at DESC LIMIT 1`, h.tenantID, h.employeeID).Scan(&id, &code)
	}); err != nil {
		t.Fatalf("the request: %v", err)
	}
	return id, code
}

// manager commits a manager of tenantID with email (or none), under the panel tables lock
// (holdPanelTablesLock; cmd/opadmin's TestTablesLock_AdminRowsAreWrittenUnderTheHolder).
func (h *nfcHarness) manager(t *testing.T, tenantID uuid.UUID, email string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	release := holdPanelTablesLock(t)
	defer release()
	owner := holdingOwner(t)
	if _, err := owner.Exec(context.Background(),
		`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		 VALUES ($1, $2, 'm16-23 manager', $3, $4, 'manager', 'active')`,
		id, tenantID, email, fixtures.UnusablePasswordHash); err != nil {
		t.Fatalf("a manager: %v", err)
	}
	return id
}

func (h *nfcHarness) transactions(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`,
			h.tenantID, h.employeeID).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// ---------------------------------------------------------------------------------------------

// TestNFCLessDB_ALiveQRScanActivatesTheConsentedBrowser is the card's live QR road: a browser that
// consented scans its own business's running screen -> "Activation complete", the person active,
// one live session, the invitation spent, NO transactions row, and activation.completed naming the
// proof and the screen. MUTANT (measured): CompleteByLiveQR's finishActivation replaced by the
// no-session redirect -> this test is red.
func TestNFCLessDB_ALiveQRScanActivatesTheConsentedBrowser(t *testing.T) {
	h := newNFCLessHarness(t)
	sc := h.runningScreenAt(t, h.tenantID, h.locationID)
	c, code := h.consented(t)

	resp, page := h.tapWith(t, c, liveURL(h.tokenAgo(t, sc, 0)))
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("live QR scan = %d, want the completion page; body:\n%s", resp.StatusCode, page)
	}
	h.assertEmployeeStatus(t, "active")
	h.assertInviteConsumed(t, true)
	h.assertLiveSessionCount(t, 1)
	if n := h.transactions(t); n != 0 {
		t.Fatalf("%d transactions rows: the activating scan is not attendance", n)
	}
	h.assertAudit(t, ActionActivationCompleted, 1)
	h.assertAuditMentions(t, ActionActivationCompleted, `"proof": "live_qr"`)
	h.assertAuditMentions(t, ActionActivationCompleted, sc.ID.String())
	h.assertAuditDoesNotMention(t, code)
}

// TestNFCLessDB_ALiveQRThatCannotActivateIsOneScreenAndARow: a forged code, a code past its window
// (a photograph), another business's screen and a removed screen each answer the ONE scan-failed
// screen, activate nobody, write no transactions row, and leave an activation.failed row with the
// reason -- the foreign screen's id NOT in this tenant's trail (§4.5). MUTANT (measured): the
// tenant comparison in liveQRActivationRefusal removed -> the foreign case activates.
func TestNFCLessDB_ALiveQRThatCannotActivateIsOneScreenAndARow(t *testing.T) {
	h := newNFCLessHarness(t)
	own := h.runningScreenAt(t, h.tenantID, h.locationID)
	other := newTapHarness(t)
	foreign := h.runningScreenAt(t, other.tenantID, other.locationID)
	removed := h.runningScreenAt(t, h.tenantID, h.locationID)
	if _, err := h.screens.Revoke(context.Background(), liveqr.RevokeCommand{TenantID: h.tenantID, ActorID: uuid.New(), DisplayID: removed.ID}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	c, _ := h.consented(t)
	for _, tc := range []struct {
		name, token, reason string
	}{
		{"forged", "not-a-token", "live_qr_malformed"},
		{"photographed", h.tokenAgo(t, own, 2*time.Minute), "live_qr_expired"},
		{"foreign screen", h.tokenAgo(t, foreign, 0), "foreign_tenant_screen"},
		{"removed screen", h.tokenAgo(t, removed, 0), "screen_not_active"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, page := h.tapWith(t, c, liveURL(tc.token))
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, html.EscapeString(problemActivationScanFailed.Title)) {
				t.Fatalf("= %d, want 400 and the scan-failed screen; body:\n%s", resp.StatusCode, page)
			}
			h.assertEmployeeStatus(t, "invited")
			h.assertInviteConsumed(t, false)
			if n := h.transactions(t); n != 0 {
				t.Fatalf("%d transactions rows for a refused activation", n)
			}
			h.assertAuditMentions(t, ActionActivationFailed, `"reason": "`+tc.reason+`"`)
		})
	}
	h.assertAuditDoesNotMention(t, foreign.ID.String())
}

// TestNFCLessDB_ManagerApprovalActivatesTheBrowserThatAsked is the card's second road end to end:
// "My phone can't tap" opens a request (the code on the phone, nothing consumed); a manager
// approves it through the panel's domain (audit row with the actor); the browser's next look
// finishes the setup -- one session, the invitation spent, the request collected once, NO
// transactions row -- and the pairing code reaches no audit detail. MUTANT (measured): the
// collection dropped from ActivateApproved -> the request stays uncollected and this test is red.
func TestNFCLessDB_ManagerApprovalActivatesTheBrowserThatAsked(t *testing.T) {
	h := newNFCLessHarness(t)
	c, code := h.consented(t)
	if page := h.getBody(t, c, "/activate?step=3"); !strings.Contains(page, "My phone can't tap") {
		t.Fatalf("the waiting screen of a business with no screen offers no approval:\n%s", page)
	}

	resp, page := h.ask(t, c)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != ActivationApprovalPath {
		t.Fatalf("ask = %d at %s, want the approval page", resp.StatusCode, resp.Request.URL)
	}
	reqID, pairing := h.request(t)
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(pairing) || !strings.Contains(page, pairing[:3]+" "+pairing[3:]) {
		t.Fatalf("the approval page does not show the request's code %q grouped:\n%s", pairing, page)
	}
	h.assertEmployeeStatus(t, "invited")
	h.assertInviteConsumed(t, false)
	h.assertAudit(t, ActionApprovalRequested, 1)
	if got := h.approvalState(t, c); got != "waiting" {
		t.Fatalf("status = %q, want waiting", got)
	}

	pending, err := h.staff.PendingApprovals(context.Background(), h.tenantID)
	if err != nil || len(pending) != 1 || pending[0].ID != reqID || pending[0].PairingCode != pairing {
		t.Fatalf("the panel lists %+v (%v), want the one request with its code", pending, err)
	}
	admin := h.manager(t, h.tenantID, "m1623-"+uuid.NewString()+"@iso.example")
	if _, err := h.staff.DecideApproval(context.Background(), tenant.DecideApprovalCommand{
		TenantID: h.tenantID, RequestID: reqID, ActorID: admin, Approve: true,
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	h.assertAudit(t, tenant.ActionActivationApproved, 1)
	if got := h.approvalState(t, c); got != "approved" {
		t.Fatalf("status after the approval = %q, want approved", got)
	}

	resp, page = h.collect(t, c)
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("collect = %d, want the completion page; body:\n%s", resp.StatusCode, page)
	}
	h.assertEmployeeStatus(t, "active")
	h.assertInviteConsumed(t, true)
	h.assertLiveSessionCount(t, 1)
	if n := h.transactions(t); n != 0 {
		t.Fatalf("%d transactions rows: the approved activation is not attendance", n)
	}
	h.assertCount(t, `SELECT count(*) FROM activation_requests WHERE tenant_id = $1 AND employee_id = $2 AND collected_at IS NOT NULL`, 1, "collected requests")
	h.assertAuditMentions(t, ActionActivationCompleted, `"proof": "manager_approval"`)
	h.assertAuditMentions(t, ActionActivationCompleted, admin.String())
	h.assertAuditDoesNotMention(t, pairing)
	h.assertAuditDoesNotMention(t, code)
	// The decision is one way.
	if _, err := h.staff.DecideApproval(context.Background(), tenant.DecideApprovalCommand{
		TenantID: h.tenantID, RequestID: reqID, ActorID: admin, Approve: false,
	}); !errors.Is(err, tenant.ErrApprovalClosed) {
		t.Fatalf("a second decision = %v, want ErrApprovalClosed", err)
	}
}

// TestNFCLessDB_ARunningScreenAtTheVenueClosesTheManagerApproval is the card's gate test: with a
// running screen at the person's venue the waiting screen offers no approval, AND a request posted
// anyway is refused by the server (409, the scan screen) -- no request row, a recorded refusal. A
// screen at ANOTHER venue of the business does not close it (SEÇİM: the person's home venue). A
// screen that stopped polling does not close it either. MUTANT (measured): OpenApproval's count
// check removed -> the posted request opens a row and this test is red.
func TestNFCLessDB_ARunningScreenAtTheVenueClosesTheManagerApproval(t *testing.T) {
	h := newNFCLessHarness(t)
	h.runningScreenAt(t, h.tenantID, h.locationID)
	c, _ := h.consented(t)
	page := h.getBody(t, c, "/activate?step=3")
	if strings.Contains(page, "My phone can't tap") || !strings.Contains(page, "scan the code on the Taptime screen") {
		t.Fatalf("with a running screen the waiting screen must send the person to it, not to a manager:\n%s", page)
	}
	resp, err := c.PostForm(h.server.URL+ActivationApprovalPath, url.Values{"csrf": {formToken(t, h.getBody(t, c, "/activate?step=1"))}})
	if err != nil {
		t.Fatal(err)
	}
	b := body(t, resp)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(b, html.EscapeString(problemApprovalScreenAvailable.Title)) {
		t.Fatalf("a request where a screen runs = %d, want 409 and the scan screen; body:\n%s", resp.StatusCode, b)
	}
	h.assertCount(t, `SELECT count(*) FROM activation_requests WHERE tenant_id = $1 AND employee_id = $2`, 0, "requests")
	h.assertAuditMentions(t, ActionActivationFailed, `"reason": "screen_available"`)

	t.Run("a screen at another venue does not close it", func(t *testing.T) {
		h := newNFCLessHarness(t)
		other := uuid.New()
		if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Sliema')`, other, h.tenantID)
			return e
		}); err != nil {
			t.Fatal(err)
		}
		h.runningScreenAt(t, h.tenantID, other)
		c, _ := h.consented(t)
		if resp, _ := h.ask(t, c); resp.Request.URL.Path != ActivationApprovalPath {
			t.Fatalf("ask = %d at %s, want the approval page", resp.StatusCode, resp.Request.URL)
		}
	})
}

// TestNFCLessDB_AManagerCannotApproveTheirOwnPhone: the deciding account's address equal to the
// person's (case aside) is refused -- approval and rejection -- the request stays open and the
// attempt is trailed; another manager's approval then goes through. MUTANT (measured): the self
// branch in DecideApproval removed -> the first approval succeeds.
func TestNFCLessDB_AManagerCannotApproveTheirOwnPhone(t *testing.T) {
	h := newNFCLessHarness(t)
	addr := "m1623-self-" + uuid.NewString() + "@iso.example"
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE employees SET email = $3 WHERE tenant_id = $1 AND id = $2`, h.tenantID, h.employeeID, addr)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	c, _ := h.consented(t)
	h.ask(t, c)
	reqID, _ := h.request(t)
	self := h.manager(t, h.tenantID, strings.ToUpper(addr))
	for _, approve := range []bool{true, false} {
		if _, err := h.staff.DecideApproval(context.Background(), tenant.DecideApprovalCommand{
			TenantID: h.tenantID, RequestID: reqID, ActorID: self, Approve: approve,
		}); !errors.Is(err, tenant.ErrSelfApproval) {
			t.Fatalf("self decision (approve=%v) = %v, want ErrSelfApproval", approve, err)
		}
	}
	h.assertAudit(t, tenant.ActionActivationApprovalRefused, 2)
	if got := h.approvalState(t, c); got != "waiting" {
		t.Fatalf("status after a refused self-approval = %q, want waiting", got)
	}
	other := h.manager(t, h.tenantID, "m1623-"+uuid.NewString()+"@iso.example")
	if _, err := h.staff.DecideApproval(context.Background(), tenant.DecideApprovalCommand{
		TenantID: h.tenantID, RequestID: reqID, ActorID: other, Approve: true,
	}); err != nil {
		t.Fatalf("another manager's approval: %v", err)
	}
}

// TestNFCLessDB_ARejectedOrExpiredRequestActivatesNothing: a rejection is final for that request
// (one way) and "Check now" finishes nothing; a request past its expiry is "expired", the panel no
// longer lists it, and a decision is refused. Neither consumes the invitation.
func TestNFCLessDB_ARejectedOrExpiredRequestActivatesNothing(t *testing.T) {
	h := newNFCLessHarness(t)
	c, _ := h.consented(t)
	h.ask(t, c)
	reqID, _ := h.request(t)
	admin := h.manager(t, h.tenantID, "m1623-"+uuid.NewString()+"@iso.example")
	if _, err := h.staff.DecideApproval(context.Background(), tenant.DecideApprovalCommand{
		TenantID: h.tenantID, RequestID: reqID, ActorID: admin, Approve: false,
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	h.assertAudit(t, tenant.ActionActivationRejected, 1)
	if got := h.approvalState(t, c); got != "rejected" {
		t.Fatalf("status = %q, want rejected", got)
	}
	resp, page := h.collect(t, c)
	if resp.Request.URL.Path != ActivationApprovalPath || !strings.Contains(page, "didn&#39;t approve this phone") {
		t.Fatalf("collect after a rejection = %d at %s, want the rejected page:\n%s", resp.StatusCode, resp.Request.URL, page)
	}
	h.assertInviteConsumed(t, false)
	if _, err := h.staff.DecideApproval(context.Background(), tenant.DecideApprovalCommand{
		TenantID: h.tenantID, RequestID: reqID, ActorID: admin, Approve: true,
	}); !errors.Is(err, tenant.ErrApprovalClosed) {
		t.Fatalf("approving a rejected request = %v, want ErrApprovalClosed", err)
	}

	// "Ask again" opens a new request; then time passes it by (the owner shortens its window to a
	// millisecond -- the lifetime CHECK still holds; the state trigger does not watch the column).
	h.ask(t, c)
	again, _ := h.request(t)
	if again == reqID {
		t.Fatal("asking again did not open a new request")
	}
	owner := holdingOwner(t)
	if _, err := owner.Exec(context.Background(),
		`UPDATE activation_requests SET expires_at = created_at + interval '1 millisecond' WHERE id = $1`, again); err != nil {
		t.Fatalf("age the request: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := h.approvalState(t, c); got != "expired" {
		t.Fatalf("status of an aged request = %q, want expired", got)
	}
	if pending, err := h.staff.PendingApprovals(context.Background(), h.tenantID); err != nil || len(pending) != 0 {
		t.Fatalf("the panel lists %d (%v), want none", len(pending), err)
	}
	if _, err := h.staff.DecideApproval(context.Background(), tenant.DecideApprovalCommand{
		TenantID: h.tenantID, RequestID: again, ActorID: admin, Approve: true,
	}); !errors.Is(err, tenant.ErrApprovalClosed) {
		t.Fatalf("approving an expired request = %v, want ErrApprovalClosed", err)
	}
	h.assertInviteConsumed(t, false)
	if n := h.transactions(t); n != 0 {
		t.Fatalf("%d transactions rows", n)
	}
}

// ---------------------------------------------------------------------------------------------
// The app.

// approvalSigned is the approval route's request p signs over bind's challenge.
func (h *appHarness) approvalSigned(t *testing.T, p *appPhone, b appBindResponse, code, path string) *http.Request {
	t.Helper()
	body := jsonBody(t, appApprovalRequest{Code: code, PublicKey: p.publicWire(t), Platform: "android"})
	return h.signedRequest(t, p, b.ActivationID, b.Challenge, path, body)
}

// TestAppNFCLessDB_ALiveQRCompletesTheAppActivation: complete with liveQR (and no sun) creates the
// device -- no transactions row, activation.completed with the screen; both fields at once is a
// 400 that consumes nothing; a photographed code is activation_tap_failed.
func TestAppNFCLessDB_ALiveQRCompletesTheAppActivation(t *testing.T) {
	h := newAppLiveHarness(t)
	sc := h.live.screenAt(t, h.tenantID, h.locationID)
	emp, code := h.invitedEmployee(t, "invited")
	before := h.transactionCount(t)
	p := newAppPhone(t)
	b := h.bind(t, p, code)

	sign := func(in appCompleteRequest) *httptest.ResponseRecorder {
		return h.serve(h.signedRequest(t, p, b.ActivationID, b.Challenge, AppActivateCompletePath, jsonBody(t, in)))
	}
	both := sign(appCompleteRequest{Code: code, PublicKey: p.publicWire(t), Platform: "android", SUN: h.nextSUN(t), LiveQR: liveQuery(h.live.token(t, sc))})
	if both.Code != http.StatusBadRequest || strings.TrimSpace(both.Body.String()) != `{"error":"bad_request"}` {
		t.Fatalf("both proofs = %d %s, want 400 bad_request", both.Code, both.Body.String())
	}
	stale := sign(appCompleteRequest{Code: code, PublicKey: p.publicWire(t), Platform: "android", LiveQR: liveQuery(h.live.tokenIssuedAgo(t, sc, 2*time.Minute))})
	if stale.Code != http.StatusBadRequest || strings.TrimSpace(stale.Body.String()) != `{"error":"activation_tap_failed"}` {
		t.Fatalf("a photographed code = %d %s, want activation_tap_failed", stale.Code, stale.Body.String())
	}
	if h.employeeStatus(t, emp) != "invited" || h.liveDevices(t, emp) != 0 {
		t.Fatal("a refused live QR activated somebody")
	}
	ok := sign(appCompleteRequest{Code: code, PublicKey: p.publicWire(t), Platform: "android", LiveQR: liveQuery(h.live.token(t, sc))})
	if ok.Code != http.StatusOK {
		t.Fatalf("complete with a live QR = %d %s", ok.Code, ok.Body.String())
	}
	if h.employeeStatus(t, emp) != "active" || h.liveDevices(t, emp) != 1 {
		t.Fatalf("after the live QR: status %q, %d devices", h.employeeStatus(t, emp), h.liveDevices(t, emp))
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions went %d -> %d", before, after)
	}
	if n := h.countWhere(t, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2::text AND action = 'activation.completed'
		AND detail->>'proof' = 'live_qr' AND detail->>'display_id' = '`+sc.ID.String()+`'`, emp); n != 1 {
		t.Fatalf("%d activation.completed rows naming the live QR and its screen, want 1", n)
	}
}

// TestAppNFCLessDB_ManagerApprovalCreatesTheDeviceOnTheNextStatusRead: the app's request answers
// the pairing code; the status read is "waiting" until a manager approves, then the SAME read
// creates the device (one transaction with the collection) -- and a running screen at the venue
// refuses the request with screen_available.
func TestAppNFCLessDB_ManagerApprovalCreatesTheDeviceOnTheNextStatusRead(t *testing.T) {
	h := newAppLiveHarness(t)
	staff, err := tenant.NewStaff(h.data, h.trail, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	emp, code := h.invitedEmployee(t, "invited")
	before := h.transactionCount(t)
	look := h.unsigned(t, AppActivateLookupPath, jsonBody(t, appLookupRequest{Code: code}))
	if !strings.Contains(look.Body.String(), `"managerApproval":true`) {
		t.Fatalf("lookup with no screen = %s, want managerApproval true", look.Body.String())
	}
	p := newAppPhone(t)
	b := h.bind(t, p, code)

	open := h.serve(h.approvalSigned(t, p, b, code, AppActivateApprovalPath))
	var waiting appApprovalResponse
	if open.Code != http.StatusOK || json.Unmarshal(open.Body.Bytes(), &waiting) != nil || waiting.State != "waiting" ||
		!regexp.MustCompile(`^[0-9]{6}$`).MatchString(waiting.PairingCode) {
		t.Fatalf("approval = %d %s, want waiting with a code", open.Code, open.Body.String())
	}
	status := func() appApprovalResponse {
		w := h.serve(h.approvalSigned(t, p, b, code, AppActivateApprovalStatusPath))
		var out appApprovalResponse
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("status = %d %s", w.Code, w.Body.String())
		}
		return out
	}
	if s := status(); s.State != "waiting" || s.PairingCode != waiting.PairingCode {
		t.Fatalf("status = %+v, want waiting with the same code", s)
	}
	pending, err := staff.PendingApprovals(context.Background(), h.tenantID)
	if err != nil || len(pending) != 1 || pending[0].Client != "app" {
		t.Fatalf("the panel lists %+v (%v)", pending, err)
	}
	release := holdPanelTablesLock(t)
	admin := uuid.New()
	if _, err := h.live.owner.Exec(context.Background(),
		`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		 VALUES ($1, $2, 'm16-23 manager', $3, $4, 'owner', 'active')`,
		admin, h.tenantID, "m1623-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash); err != nil {
		release()
		t.Fatalf("a manager: %v", err)
	}
	release()
	if _, err := staff.DecideApproval(context.Background(), tenant.DecideApprovalCommand{
		TenantID: h.tenantID, RequestID: pending[0].ID, ActorID: admin, Approve: true,
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	done := status()
	if done.State != "approved" || done.DeviceID == "" {
		t.Fatalf("status after the approval = %+v, want approved with the device", done)
	}
	if h.employeeStatus(t, emp) != "active" || h.liveDevices(t, emp) != 1 {
		t.Fatalf("after the approval: status %q, %d devices", h.employeeStatus(t, emp), h.liveDevices(t, emp))
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions went %d -> %d", before, after)
	}
	p.id = uuid.MustParse(done.DeviceID)
	if me := h.read(t, p, http.MethodGet, AppMePath, ""); me.Code != http.StatusOK {
		t.Fatalf("GET /me with the approved device = %d", me.Code)
	}

	t.Run("a running screen refuses the request", func(t *testing.T) {
		sc := h.live.screenAt(t, h.tenantID, h.locationID)
		if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := store.New(tx).TouchLiveQRDisplay(ctx, store.TouchLiveQRDisplayParams{TenantID: h.tenantID, ID: sc.ID})
			return e
		}); err != nil {
			t.Fatal(err)
		}
		emp2, code2 := h.invitedEmployee(t, "invited")
		look := h.unsigned(t, AppActivateLookupPath, jsonBody(t, appLookupRequest{Code: code2}))
		if !strings.Contains(look.Body.String(), `"managerApproval":false`) {
			t.Fatalf("lookup with a running screen = %s, want managerApproval false", look.Body.String())
		}
		p2 := newAppPhone(t)
		b2 := h.bind(t, p2, code2)
		w := h.serve(h.approvalSigned(t, p2, b2, code2, AppActivateApprovalPath))
		if w.Code != http.StatusConflict || strings.TrimSpace(w.Body.String()) != `{"error":"screen_available"}` {
			t.Fatalf("approval where a screen runs = %d %s, want 409 screen_available", w.Code, w.Body.String())
		}
		if n := h.countWhere(t, `SELECT count(*) FROM activation_requests WHERE tenant_id = $1 AND employee_id = $2`, emp2); n != 0 {
			t.Fatalf("%d requests opened where a screen runs", n)
		}
	})
}
