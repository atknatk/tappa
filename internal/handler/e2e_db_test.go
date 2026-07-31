package handler

// End-to-end against a REAL Postgres and the REAL router: issue an invitation,
// open the link, consent, submit, get a session cookie, and find the employee
// active with the invitation spent. Then prove the same code cannot do it twice —
// once sequentially, and once with N goroutines racing under -race.
//
// WHY NOT FAKES HERE. Everything this file measures is a property of the
// DATABASE: the atomic single-use consumption (§4.4), the tenant isolation, the
// audit row. activate_test.go covers the HTTP behaviour with fakes; neither file
// can replace the other.
//
// Fixtures are NOT cleaned up (tappa_app has REVOKE DELETE on the tables
// involved). Fresh random UUIDs keep runs from colliding; `make db-reset` clears
// the dev database.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
)

// linkChannel captures the activation URL, which is how a real delivery
// mechanism receives it (invite.Channel). No test-only accessor exists or is
// needed.
type linkChannel struct{ url string }

func (c *linkChannel) DeliverInvite(_ context.Context, d invite.Delivery) error {
	c.url = d.ActivationURL
	return nil
}

type harness struct {
	server     *httptest.Server
	data       *db.DB
	invites    *invite.Manager
	tenantID   uuid.UUID
	locationID uuid.UUID
	employeeID uuid.UUID
}

func newHarness(t *testing.T, employeeStatus string) *harness {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping activation end-to-end tests (real Postgres required)")
	}
	// Obviously fake, distinct 32-byte keys (agent-brief madde 2). They MUST
	// differ: config.Load refuses equal keys and this fixture honours the same
	// rule even though it does not go through Load.
	cfg := &config.Config{
		Env:            config.EnvDev,
		BaseURL:        "http://localhost:8080",
		DatabaseURL:    dsn,
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"),
		InviteHMACKey:  []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		RetentionYears: 2,
	}

	data, err := db.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	sessions, err := session.New(data, cfg)
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	invites, err := invite.New(data, cfg)
	if err != nil {
		t.Fatalf("invite.New: %v", err)
	}
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	act, err := NewActivation(invites, sessions, trail, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}
	r := chi.NewRouter()
	act.Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	h := &harness{
		server: srv, data: data, invites: invites,
		tenantID: uuid.New(), locationID: uuid.New(), employeeID: uuid.New(),
	}
	err = data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi')`,
			h.tenantID, "VAT-"+h.tenantID.String()[:8]); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng, wifi_ssid)
			 VALUES ($1, $2, 'St Julians', '{203.0.113.0/24}', 35.918, 14.489, 'KF-StJulians-Staff')`,
			h.locationID, h.tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Maria Borg', $4, now())`,
			h.employeeID, h.tenantID, h.locationID, employeeStatus)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return h
}

// issue mints an invitation and returns the raw code, taken off the delivery
// channel exactly as a mail sender would.
func (h *harness) issue(t *testing.T) string {
	t.Helper()
	ch := &linkChannel{}
	if _, err := h.invites.IssueAndDeliver(context.Background(), invite.IssueParams{
		TenantID: h.tenantID, EmployeeID: h.employeeID,
	}, ch); err != nil {
		t.Fatalf("IssueAndDeliver: %v", err)
	}
	u, err := url.Parse(ch.url)
	if err != nil {
		t.Fatalf("activation url: %v", err)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatal("the delivered link carries no code")
	}
	return code
}

func (h *harness) client(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

// formToken pulls the synchronizer token out of a rendered activation form —
// exactly what a browser does when it submits. Driving the flow through the token
// (rather than around it) is what makes these tests prove the CSRF measure works
// for a legitimate user instead of only against an attacker.
func formToken(t *testing.T, page string) string {
	t.Helper()
	m := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(page)
	if len(m) != 2 {
		t.Fatal("the activation form carries no synchronizer token")
	}
	return m[1]
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// TestE2E_ActivationFlow is the card's acceptance path, end to end.
func TestE2E_ActivationFlow(t *testing.T) {
	h := newHarness(t, "invited")
	code := h.issue(t)
	c := h.client(t)

	// 1. Open the link. The client follows the 303 to a clean /activate.
	resp, err := c.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatalf("GET /activate: %v", err)
	}
	page := body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Request.URL.RawQuery != "" {
		t.Errorf("the code survived in the URL: %s", resp.Request.URL)
	}
	for _, want := range []string{"Maria Borg", "Kebab Factory Ltd", "KF-StJulians-Staff", "No fingerprints"} {
		if !strings.Contains(page, want) {
			t.Errorf("the activation page is missing %q", want)
		}
	}
	if strings.Contains(page, code) {
		t.Fatal("the activation page carries the raw code")
	}

	token := formToken(t, page)

	// 2. Submit WITHOUT consent: nothing may be consumed.
	noConsent, err := c.PostForm(h.server.URL+"/api/activate", url.Values{"csrf": {token}})
	if err != nil {
		t.Fatalf("POST without consent: %v", err)
	}
	if got := body(t, noConsent); !strings.Contains(got, "Please tick the box") {
		t.Error("the consent gate did not come back with the form")
	}
	if noConsent.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", noConsent.StatusCode)
	}
	h.assertEmployeeStatus(t, "invited")

	// 3. Submit WITH consent.
	done, err := c.PostForm(h.server.URL+"/api/activate", url.Values{"consent": {"yes"}, "csrf": {token}})
	if err != nil {
		t.Fatalf("POST /api/activate: %v", err)
	}
	confirmation := body(t, done)
	if done.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 after following the redirect", done.StatusCode)
	}
	if !strings.Contains(done.Request.URL.Path, "/activate/done") {
		t.Errorf("landed on %s, want /activate/done", done.Request.URL.Path)
	}
	if !strings.Contains(confirmation, "All done") {
		t.Error("the confirmation screen did not render")
	}
	if strings.Contains(confirmation, code) {
		t.Fatal("the confirmation carries the raw code")
	}

	// 4. The session cookie is in the jar, and the activation cookie is gone.
	u, _ := url.Parse(h.server.URL)
	var sessionCookie, activationCookie string
	for _, ck := range c.Jar.Cookies(u) {
		switch ck.Name {
		case session.CookieName:
			sessionCookie = ck.Value
		case activationCookieName:
			activationCookie = ck.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("no session cookie: the phone was not remembered")
	}
	if activationCookie != "" {
		t.Error("the spent activation cookie is still in the browser")
	}

	// 5. The database agrees.
	h.assertEmployeeStatus(t, "active")
	h.assertInviteConsumed(t, true)
	h.assertSessionCount(t, 1)
	h.assertAudit(t, ActionActivationCompleted, 1)
	h.assertAudit(t, invite.ActionCodeShownToManager, 0) // no channel wrote one here

	// 6. THE SAME CODE CANNOT DO IT AGAIN. A fresh browser opens the same link.
	second := h.client(t)
	replay, err := second.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatalf("replay GET: %v", err)
	}
	replayBody := body(t, replay)
	if replay.StatusCode != http.StatusBadRequest {
		t.Fatalf("replay status = %d, want 400", replay.StatusCode)
	}
	if !strings.Contains(replayBody, "Ask your manager for a new one") {
		t.Error("a replayed link must land on the generic failure page")
	}
	h.assertSessionCount(t, 1)
	// TWO failures are recorded by this test, and both are meant to be: the
	// consent-less submit in step 2 and the replay in step 6. §4.6 wants an
	// attempt that goes nowhere to leave a trace, so the count is the assertion —
	// dropping either row would be the bug.
	h.assertAudit(t, ActionActivationFailed, 2)
}

// TestE2E_ConcurrentActivationsProduceExactlyOneSession is the §4.4 proof at the
// HTTP boundary: N browsers POST the SAME code at the same instant and exactly
// one walks away with a session.
func TestE2E_ConcurrentActivationsProduceExactlyOneSession(t *testing.T) {
	h := newHarness(t, "invited")
	code := h.issue(t)

	// All N racers share one browser state: the same cookie, so the same
	// synchronizer token. The race being measured is the CONSUMPTION, not the
	// token check.
	const raceToken = "RACEtokenRACEtokenRACEtokenRACEtokenRACE123"

	const n = 10 // below inviteFailureLimit so the limiter is not what wins
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
		statuses  []int
	)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, h.server.URL+"/api/activate",
				strings.NewReader("consent=yes&csrf="+url.QueryEscape(raceToken)))
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(&http.Cookie{Name: activationCookieName, Value: raceToken + "." + code})
			client := &http.Client{
				Timeout: 15 * time.Second,
				// Do not follow the redirect: the 303 itself is the signal.
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			<-start
			resp, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)

			mu.Lock()
			statuses = append(statuses, resp.StatusCode)
			if resp.StatusCode == http.StatusSeeOther {
				successes++
			}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if successes != 1 {
		t.Fatalf("%d of %d concurrent activations succeeded, want exactly 1 (statuses: %v)", successes, n, statuses)
	}
	h.assertSessionCount(t, 1)
	h.assertInviteConsumed(t, true)
	h.assertEmployeeStatus(t, "active")
}

// TestE2E_SecondDeviceRevokesTheFirst is the decision this task makes explicit: a
// valid, unused invitation for an ALREADY ACTIVE employee is treated as a new
// phone, and the old phone is signed out.
func TestE2E_SecondDeviceRevokesTheFirst(t *testing.T) {
	h := newHarness(t, "invited")

	// First device.
	first := h.client(t)
	code1 := h.issue(t)
	p1, err := first.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code1))
	if err != nil {
		t.Fatalf("first GET: %v", err)
	}
	t1 := formToken(t, body(t, p1))
	r1, err := first.PostForm(h.server.URL+"/api/activate", url.Values{"consent": {"yes"}, "csrf": {t1}})
	if err != nil {
		t.Fatalf("first POST: %v", err)
	}
	_ = body(t, r1)
	h.assertSessionCount(t, 1)

	// Second device, second invitation.
	second := h.client(t)
	code2 := h.issue(t)
	page, err := second.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code2))
	if err != nil {
		t.Fatalf("second GET: %v", err)
	}
	warn := body(t, page)
	if !strings.Contains(warn, "This is a new phone") {
		t.Error("the second activation must warn before signing the other phone out")
	}
	t2 := formToken(t, warn)
	r2, err := second.PostForm(h.server.URL+"/api/activate", url.Values{"consent": {"yes"}, "csrf": {t2}})
	if err != nil {
		t.Fatalf("second POST: %v", err)
	}
	confirmation := body(t, r2)
	if !strings.Contains(confirmation, "other phone has been signed out") {
		t.Error("the confirmation must say what happened to the other phone")
	}

	// Two session rows exist, and exactly one is live: the new one.
	h.assertSessionCount(t, 2)
	h.assertLiveSessionCount(t, 1)
	h.assertAudit(t, ActionDeviceReplaced, 1)

	// The OLD device's cookie no longer resolves to a live session — measured by
	// asking the confirmation page, which verifies the session it is given.
	stale, err := first.Get(h.server.URL + "/activate/done")
	if err != nil {
		t.Fatalf("stale GET: %v", err)
	}
	if got := body(t, stale); !strings.Contains(got, "keep the sign-in") {
		t.Error("the revoked device must no longer be recognised")
	}
}

// TestE2E_ManagerVisibleChannelLeavesATrail: the Q02 stopgap is recorded, because
// ADR 0005 Y-D's detection signal needs the raw material.
func TestE2E_ManagerVisibleChannelLeavesATrail(t *testing.T) {
	h := newHarness(t, "invited")
	trail, err := audit.New(h.data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	sink := &linkChannel{}
	admin := uuid.New()
	ch, err := invite.NewManagerVisibleChannel(sinkAdapter{sink}, trail, &admin)
	if err != nil {
		t.Fatalf("NewManagerVisibleChannel: %v", err)
	}
	if _, err := h.invites.IssueAndDeliver(context.Background(), invite.IssueParams{
		TenantID: h.tenantID, EmployeeID: h.employeeID,
	}, ch); err != nil {
		t.Fatalf("IssueAndDeliver: %v", err)
	}
	if sink.url == "" {
		t.Fatal("the panel sink received no link")
	}
	h.assertAudit(t, invite.ActionCodeShownToManager, 1)
}

// sinkAdapter presents the capture channel as an invite.LinkSink.
type sinkAdapter struct{ c *linkChannel }

func (s sinkAdapter) ShowActivationLink(ctx context.Context, d invite.Delivery) error {
	return s.c.DeliverInvite(ctx, d)
}

func (h *harness) assertEmployeeStatus(t *testing.T, want string) {
	t.Helper()
	var got string
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM employees WHERE tenant_id = $1 AND id = $2`,
			h.tenantID, h.employeeID).Scan(&got)
	}); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if got != want {
		t.Fatalf("employees.status = %q, want %q", got, want)
	}
}

func (h *harness) assertInviteConsumed(t *testing.T, want bool) {
	t.Helper()
	var n int
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2 AND used_at IS NOT NULL`,
			h.tenantID, h.employeeID).Scan(&n)
	}); err != nil {
		t.Fatalf("read used_at: %v", err)
	}
	if want && n == 0 {
		t.Fatal("no invitation was consumed")
	}
	if !want && n != 0 {
		t.Fatalf("%d invitations were consumed, want 0", n)
	}
}

func (h *harness) assertSessionCount(t *testing.T, want int) {
	t.Helper()
	h.assertCount(t, `SELECT count(*) FROM sessions WHERE tenant_id = $1 AND employee_id = $2`, want, "sessions")
}

func (h *harness) assertLiveSessionCount(t *testing.T, want int) {
	t.Helper()
	h.assertCount(t,
		`SELECT count(*) FROM sessions WHERE tenant_id = $1 AND employee_id = $2 AND revoked_at IS NULL`,
		want, "live sessions")
}

func (h *harness) assertCount(t *testing.T, query string, want int, what string) {
	t.Helper()
	var n int
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, query, h.tenantID, h.employeeID).Scan(&n)
	}); err != nil {
		t.Fatalf("count %s: %v", what, err)
	}
	if n != want {
		t.Fatalf("%s = %d, want %d", what, n, want)
	}
}

// assertAudit counts rows for one action AND proves the trail carries no secret:
// no row's detail may contain the invite code or a 64-hex hash.
func (h *harness) assertAudit(t *testing.T, action string, want int) {
	t.Helper()
	var n int
	var details []string
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			h.tenantID, action, h.employeeID.String()).Scan(&n); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1`, h.tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if e := rows.Scan(&d); e != nil {
				return e
			}
			details = append(details, d)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read audit_log: %v", err)
	}
	if n != want {
		t.Fatalf("audit rows for %q = %d, want %d", action, n, want)
	}
	for _, d := range details {
		if hexRun(d) {
			t.Fatalf("an audit detail contains a 64-hex value, which is the shape of a code hash: %s", d)
		}
	}
}

// hexRun reports whether s contains a run of 64 lowercase hex characters — the
// shape of employee_invites.code_hash (00009). A cheap, specific tripwire for the
// one value that must never reach the trail.
func hexRun(s string) bool {
	run := 0
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			run++
			if run >= 64 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}
