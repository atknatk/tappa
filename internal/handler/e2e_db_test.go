package handler

// End-to-end against a REAL Postgres, the REAL router and a REAL plaque: issue an
// invitation, walk the wizard, consent — and find NOTHING activated — then tap the
// plaque with a genuinely signed SUN URL and find the employee active, exactly one
// session, the invitation spent, the plaque's counter advanced and no attendance
// row (ADR 0025). Then prove the activation cannot be completed twice, by a replay,
// by racing taps (-race), by another employer's plaque, by a dead plaque, after the
// invitation expired, or without consent.
//
// WHY NOT FAKES HERE. Everything this file measures is a property of the
// DATABASE: the atomic single-use consumption and the atomic counter advance
// (§4.4), the consent predicate, the tenant boundary, the audit rows.
// activate_test.go covers the HTTP behaviour with fakes; neither file can replace
// the other.
//
// Fixtures are NOT cleaned up (tappa_app has REVOKE DELETE on the tables
// involved). Fresh random UUIDs and uids keep runs from colliding.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

// linkChannel captures the activation URL, which is how a real delivery
// mechanism receives it (invite.Channel). No test-only accessor exists or is
// needed.
type linkChannel struct{ url string }

func (c *linkChannel) DeliverInvite(_ context.Context, d invite.Delivery) error {
	c.url = d.ActivationURL
	return nil
}

// harness is the tap harness (real Tap + real Activation on one router, a real
// plaque of this tenant) plus an HTTP server and the employee being activated.
type harness struct {
	*tapHarness
	server *httptest.Server
	// employeeID is the person under test — NOT the tap harness's own already
	// active employee, whose field this shadows on purpose.
	employeeID uuid.UUID
	ctrMu      sync.Mutex
	ctr        uint32
}

func newHarness(t *testing.T, employeeStatus string) *harness {
	t.Helper()
	th := newTapHarness(t) // skips when DATABASE_URL is unset
	h := &harness{tapHarness: th, employeeID: uuid.New(), ctr: uint32(th.startCtr)}
	err := th.data.WithTenant(context.Background(), th.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE locations SET wifi_ssid = 'KF-StJulians-Staff' WHERE id = $1 AND tenant_id = $2`,
			th.locationID, th.tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Maria Borg', $4, now())`,
			h.employeeID, th.tenantID, th.locationID, employeeStatus)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	h.server = httptest.NewServer(th.router)
	t.Cleanup(h.server.Close)
	return h
}

// nextTap is a genuinely signed SUN URL for the harness's plaque with the next
// counter value — what the chip emits on its next touch.
func (h *harness) nextTap(t *testing.T) string {
	t.Helper()
	h.ctrMu.Lock()
	defer h.ctrMu.Unlock()
	h.ctr++
	return signedTapURL(t, tapFakeTagKey, h.tagUID, h.ctr)
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
	return &http.Client{Jar: jar, Timeout: 15 * time.Second}
}

// consent walks the wizard to the consent POST in browser c and returns the page
// it lands on (step 3).
func (h *harness) consent(t *testing.T, c *http.Client, code string) string {
	t.Helper()
	if _, err := c.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code)); err != nil {
		t.Fatalf("open link: %v", err)
	}
	privacy := h.getBody(t, c, "/activate?step=2")
	resp, err := c.PostForm(h.server.URL+"/api/activate", url.Values{"consent": {"yes"}, "csrf": {formToken(t, privacy)}})
	if err != nil {
		t.Fatalf("consent POST: %v", err)
	}
	page := body(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Query().Get("step") != "3" {
		t.Fatalf("consent landed on %s with %d, want step 3", resp.Request.URL, resp.StatusCode)
	}
	return page
}

func (h *harness) getBody(t *testing.T, c *http.Client, path string) string {
	t.Helper()
	resp, err := c.Get(h.server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return body(t, resp)
}

// tapWith opens a tap URL in browser c without following redirects, so a refusal
// and a redirect are both visible as themselves.
func (h *harness) tapWith(t *testing.T, c *http.Client, target string) (*http.Response, string) {
	t.Helper()
	nc := *c
	nc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := nc.Get(h.server.URL + target)
	if err != nil {
		t.Fatalf("tap %s: %v", target, err)
	}
	return resp, body(t, resp)
}

func (h *harness) status(t *testing.T, c *http.Client) string {
	t.Helper()
	var s struct{ State string }
	if err := json.Unmarshal([]byte(h.getBody(t, c, ActivationStatusPath)), &s); err != nil {
		t.Fatalf("status body: %v", err)
	}
	return s.State
}

// formToken pulls the synchronizer token out of a rendered activation form —
// exactly what a browser does when it submits.
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

func (h *harness) cookieValue(c *http.Client, name string) string {
	u, _ := url.Parse(h.server.URL)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

// TestE2E_ActivationFlow is ADR 0025's acceptance path, end to end.
func TestE2E_ActivationFlow(t *testing.T) {
	h := newHarness(t, "invited")
	code := h.issue(t)
	c := h.client(t)

	// 1. Open the link. The client follows the 303 to a clean /activate: step 1.
	resp, err := c.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatalf("GET /activate: %v", err)
	}
	welcome := body(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.RawQuery != "" {
		t.Fatalf("status %d at %s, want 200 at a clean /activate", resp.StatusCode, resp.Request.URL)
	}
	for _, want := range []string{"Maria Borg", "Kebab Factory Ltd", "Tap the plaque"} {
		if !strings.Contains(welcome, want) {
			t.Errorf("step 1 is missing %q", want)
		}
	}

	// 2. Step 2: the notice and the form. Submit WITHOUT consent: nothing recorded.
	privacy := h.getBody(t, c, "/activate?step=2")
	if !strings.Contains(privacy, "No fingerprints") {
		t.Error("step 2 must carry the GDPR notice")
	}
	token := formToken(t, privacy)
	noConsent, err := c.PostForm(h.server.URL+"/api/activate", url.Values{"csrf": {token}})
	if err != nil {
		t.Fatalf("POST without consent: %v", err)
	}
	if got := body(t, noConsent); noConsent.StatusCode != http.StatusBadRequest || !strings.Contains(got, "Please tick the box") {
		t.Errorf("the consent gate answered %d without the error", noConsent.StatusCode)
	}
	h.assertConsented(t, false)

	// 3. Consent. ADR 0025: recorded, bound — and NOTHING activated.
	resp, err = c.PostForm(h.server.URL+"/api/activate", url.Values{"consent": {"yes"}, "csrf": {token}})
	if err != nil {
		t.Fatalf("POST /api/activate: %v", err)
	}
	ready := body(t, resp)
	if resp.Request.URL.Query().Get("step") != "3" || !strings.Contains(ready, "KF-StJulians-Staff") {
		t.Fatalf("consent landed on %s; want step 3 naming the venue network", resp.Request.URL)
	}
	h.assertConsented(t, true)
	h.assertEmployeeStatus(t, "invited")
	h.assertInviteConsumed(t, false)
	h.assertSessionCount(t, 0)
	h.assertAudit(t, ActionActivationConsented, 1)
	if h.cookieValue(c, session.CookieName) != "" {
		t.Fatal("the consent POST left a session cookie")
	}
	if strings.Count(h.cookieValue(c, activationCookieName), ".") != 2 {
		t.Fatal("the activation cookie does not carry the consent binding")
	}

	// 4. The waiting screen, and its poll.
	if wait := h.getBody(t, c, "/activate?step=4"); !strings.Contains(wait, "Now tap the plaque") {
		t.Error("step 4 is not the waiting screen")
	}
	if got := h.status(t, c); got != "waiting" {
		t.Errorf("status before the tap = %q, want waiting", got)
	}

	// 5. THE TAP. A genuinely signed URL with the next counter value.
	ctrBefore := h.lastCtr(t, h.tagUID)
	txBefore := h.transactionCount(t)
	tapURL := h.nextTap(t)
	tapResp, page := h.tapWith(t, c, tapURL)
	if tapResp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("the activating tap answered %d:\n%s", tapResp.StatusCode, page)
	}
	if strings.Contains(page, "<button") || strings.Contains(page, "<form") {
		t.Error("the activation confirmation has a button (§9)")
	}
	if strings.Contains(page, code) {
		t.Fatal("the confirmation carries the raw code")
	}
	h.assertEmployeeStatus(t, "active")
	h.assertInviteConsumed(t, true)
	h.assertSessionCount(t, 1)
	h.assertAudit(t, ActionActivationCompleted, 1)
	h.assertAuditMentions(t, ActionActivationCompleted, h.tagUID)
	if got := h.lastCtr(t, h.tagUID); got != int32(h.ctr) || got <= ctrBefore {
		t.Errorf("tags.last_ctr = %d, want %d: the activating tap must run the ATOMIC advance (§4.4)", got, h.ctr)
	}
	if got := h.transactionCount(t); got != txBefore {
		t.Errorf("the activating tap wrote %d transactions rows; it is not attendance", got-txBefore)
	}
	if h.cookieValue(c, session.CookieName) == "" || h.cookieValue(c, activationCookieName) != "" {
		t.Fatal("after the tap: want a session cookie and no activation cookie")
	}
	if got := h.status(t, c); got != "done" {
		t.Errorf("status after the tap = %q, want done", got)
	}

	// 6. The SAME URL again (a reload): the phone is activated now, so it is an
	// ordinary tap page — and still nothing is consumed or issued twice.
	again, againBody := h.tapWith(t, c, tapURL)
	if again.StatusCode != http.StatusOK || strings.Contains(againBody, "Activation complete") {
		t.Errorf("a reloaded activation URL answered %d with the activation screen", again.StatusCode)
	}
	h.assertSessionCount(t, 1)

	// 7. THE NEXT TAP IS A REAL CHECK-IN, AND NOT PRACTICE (ADR 0025 replaced it).
	_, tapPage := h.tapWith(t, c, h.nextTap(t))
	ctx := regexp.MustCompile(`name="ctx" value="([^"]+)"`).FindStringSubmatch(tapPage)
	if len(ctx) != 2 {
		t.Fatalf("the next tap did not render the tap page:\n%s", tapPage)
	}
	checkin, err := c.PostForm(h.server.URL+"/api/checkin", url.Values{"ctx": {ctx[1]}})
	if err != nil {
		t.Fatalf("POST /api/checkin: %v", err)
	}
	result := body(t, checkin)
	if checkin.StatusCode != http.StatusOK || strings.Contains(result, "TRAINING") {
		t.Fatalf("the first check-in answered %d (TRAINING shown: %v)", checkin.StatusCode, strings.Contains(result, "TRAINING"))
	}
	rec := h.lastRecord(t, h.employeeID)
	if rec.Practice {
		t.Fatal("the first check-in after activation was recorded practice=true; ADR 0025 replaced the practice tap")
	}
	if rec.Type == nil || *rec.Type != "in" {
		t.Errorf("the first check-in's direction = %v, want in", show(rec.Type))
	}

	// 8. The link cannot be used again, in any browser.
	replay, err := h.client(t).Get(h.server.URL + "/activate?code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatalf("replay GET: %v", err)
	}
	if got := body(t, replay); replay.StatusCode != http.StatusBadRequest || !strings.Contains(got, "Ask your manager for a new one") {
		t.Errorf("a spent link answered %d", replay.StatusCode)
	}
	h.assertSessionCount(t, 1)
}

// TestE2E_ConsentWithoutATapActivatesNothing: no consent → no activation, and a
// consent with no tap is also no activation. A tap from a browser that only
// OPENED the link (no binding) is §5 row 3 — back to the wizard — and touches no
// counter.
func TestE2E_ConsentWithoutATapActivatesNothing(t *testing.T) {
	h := newHarness(t, "invited")
	code := h.issue(t)
	c := h.client(t)
	if _, err := c.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code)); err != nil {
		t.Fatalf("open link: %v", err)
	}
	before := h.lastCtr(t, h.tagUID)
	resp, _ := h.tapWith(t, c, h.nextTap(t))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/activate" {
		t.Fatalf("an unconsented tap answered %d %q, want 303 /activate", resp.StatusCode, resp.Header.Get("Location"))
	}
	if h.lastCtr(t, h.tagUID) != before {
		t.Error("an unconsented tap advanced the plaque's counter")
	}
	h.assertSessionCount(t, 0)
	h.assertInviteConsumed(t, false)
	h.assertEmployeeStatus(t, "invited")
}

// TestE2E_ReplayedSUNCannotActivate: a counter that is not strictly greater than
// the plaque's last one — an old URL, a copied one — completes nothing (§4.4).
func TestE2E_ReplayedSUNCannotActivate(t *testing.T) {
	h := newHarness(t, "invited")
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	for _, ctr := range []uint32{uint32(h.startCtr), uint32(h.startCtr) - 50} {
		resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, h.tagUID, ctr))
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "go through") {
			t.Errorf("ctr %d: answered %d, want the refused-tap screen", ctr, resp.StatusCode)
		}
	}
	h.assertSessionCount(t, 0)
	h.assertInviteConsumed(t, false)
	h.assertAudit(t, ActionActivationFailed, 2)
	if got := h.lastCtr(t, h.tagUID); got != h.startCtr {
		t.Errorf("a replay moved tags.last_ctr to %d", got)
	}

	// A forged CMAC on a fresh counter is refused the same way and moves nothing.
	forged := strings.Replace(h.nextTap(t), "&cmac=", "&cmac=0", 1)
	forged = forged[:len(forged)-1]
	if resp, _ := h.tapWith(t, c, forged); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a forged CMAC answered %d", resp.StatusCode)
	}
	if got := h.lastCtr(t, h.tagUID); got != h.startCtr {
		t.Errorf("a forged CMAC moved tags.last_ctr to %d (CMAC must be checked BEFORE the advance)", got)
	}

	// The genuine next touch still works: the refusals cost nothing but rows.
	if resp, page := h.tapWith(t, c, h.nextTap(t)); resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("a genuine tap after the replays answered %d", resp.StatusCode)
	}
	h.assertSessionCount(t, 1)
}

// TestE2E_ConcurrentActivatingTapsProduceExactlyOneSession is the §4.4 proof at
// the HTTP boundary, twice: N copies of the SAME URL, and N DIFFERENT genuine
// URLs, all from the one consented browser at the same instant. Exactly one
// session either way.
func TestE2E_ConcurrentActivatingTapsProduceExactlyOneSession(t *testing.T) {
	for _, distinct := range []bool{false, true} {
		name := "same_url"
		if distinct {
			name = "distinct_counters"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "invited")
			c := h.client(t)
			h.consent(t, c, h.issue(t))
			pending := h.cookieValue(c, activationCookieName)

			const n = 8 // below inviteFailureLimit, so the limiter is not what decides
			urls := make([]string, n)
			for i := range urls {
				if i == 0 || distinct {
					urls[i] = h.nextTap(t)
				} else {
					urls[i] = urls[0]
				}
			}
			var (
				wg        sync.WaitGroup
				mu        sync.Mutex
				completed int
				statuses  []int
			)
			start := make(chan struct{})
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(target string) {
					defer wg.Done()
					req, err := http.NewRequest(http.MethodGet, h.server.URL+target, nil)
					if err != nil {
						t.Error(err)
						return
					}
					req.AddCookie(&http.Cookie{Name: activationCookieName, Value: pending})
					client := &http.Client{Timeout: 15 * time.Second,
						CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
					<-start
					resp, err := client.Do(req)
					if err != nil {
						t.Error(err)
						return
					}
					b, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					mu.Lock()
					statuses = append(statuses, resp.StatusCode)
					if resp.StatusCode == http.StatusOK && strings.Contains(string(b), "Activation complete") {
						completed++
					}
					mu.Unlock()
				}(urls[i])
			}
			close(start)
			wg.Wait()

			if completed != 1 {
				t.Fatalf("%d of %d concurrent activating taps completed, want exactly 1 (statuses %v)", completed, n, statuses)
			}
			h.assertSessionCount(t, 1)
			h.assertInviteConsumed(t, true)
			h.assertAudit(t, ActionActivationCompleted, 1)
		})
	}
}

// TestE2E_ForeignTenantPlaqueCannotActivate: ANY active plaque of the employer is
// fine (user decision) — and a plaque of ANOTHER employer is not, even with a
// genuine signature.
func TestE2E_ForeignTenantPlaqueCannotActivate(t *testing.T) {
	h := newHarness(t, "invited")
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	foreignUID := h.newTag(t, kek, uuid.New(), uuid.New(), 100)
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, foreignUID, 101))
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(page, "finish setup") {
		t.Fatalf("a foreign plaque answered %d", resp.StatusCode)
	}
	// R5: the foreign plaque's counter did NOT move — it is refused on the
	// non-advancing preview, before sun.Verify.
	if got := h.lastCtr(t, foreignUID); got != 100 {
		t.Fatalf("the foreign tenant's plaque counter moved to %d, want 100", got)
	}
	h.assertSessionCount(t, 0)
	h.assertInviteConsumed(t, false)
	h.assertAudit(t, ActionActivationFailed, 1)
	// The other tenant's uid stays out of this tenant's trail (§4.5).
	h.assertAuditMentions(t, ActionActivationFailed, "foreign_tenant_tag")
	h.assertAuditDoesNotMention(t, foreignUID)

	// A SECOND plaque of the employer's own, on another wall — not the one on the
	// employee's profile — activates.
	ownUID := h.extraPlaque(t, kek, 50)
	if resp, page := h.tapWith(t, c, signedTapURL(t, tapFakeTagKey, ownUID, 51)); resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("another plaque of the same employer answered %d", resp.StatusCode)
	}
	h.assertSessionCount(t, 1)
}

// TestE2E_DeadPlaqueCannotActivate: a lost or retired plaque completes nothing and
// its counter is not touched.
func TestE2E_DeadPlaqueCannotActivate(t *testing.T) {
	for _, status := range []string{"lost", "retired"} {
		t.Run(status, func(t *testing.T) {
			h := newHarness(t, "invited")
			c := h.client(t)
			h.consent(t, c, h.issue(t))
			h.retireTag(t, h.tagUID, status)

			resp, page := h.tapWith(t, c, h.nextTap(t))
			if resp.StatusCode != http.StatusConflict || !strings.Contains(page, "in service") {
				t.Fatalf("a %s plaque answered %d", status, resp.StatusCode)
			}
			if got := h.lastCtr(t, h.tagUID); got != h.startCtr {
				t.Errorf("a %s plaque's counter moved to %d", status, got)
			}
			h.assertSessionCount(t, 0)
			h.assertInviteConsumed(t, false)
		})
	}
}

// TestE2E_ExpiredInvitationCannotActivate: the cookie outlives nothing — an
// invitation that expired between consent and tap completes nothing, and the
// dead cookie is cleared.
func TestE2E_ExpiredInvitationCannotActivate(t *testing.T) {
	h := newHarness(t, "invited")
	c := h.client(t)
	h.consent(t, c, h.issue(t))

	owner := ownerPoolForTest(t)
	if _, err := owner.Exec(context.Background(),
		`UPDATE employee_invites SET expires_at = now() - interval '1 second' WHERE tenant_id = $1 AND employee_id = $2`,
		h.tenantID, h.employeeID); err != nil {
		t.Fatalf("expire the invitation: %v", err)
	}
	before := h.lastCtr(t, h.tagUID)
	resp, _ := h.tapWith(t, c, h.nextTap(t))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an expired invitation's tap answered %d, want 400", resp.StatusCode)
	}
	if h.lastCtr(t, h.tagUID) != before {
		t.Error("an expired invitation's tap advanced the counter")
	}
	h.assertSessionCount(t, 0)
	h.assertEmployeeStatus(t, "invited")
	if h.cookieValue(c, activationCookieName) != "" {
		t.Error("the dead activation cookie was left in the browser")
	}
}

// TestE2E_SecondDeviceRevokesTheFirst: a valid invitation for an ALREADY ACTIVE
// employee is a new phone; its activating tap signs the old phone out.
func TestE2E_SecondDeviceRevokesTheFirst(t *testing.T) {
	h := newHarness(t, "invited")

	first := h.client(t)
	h.consent(t, first, h.issue(t))
	if resp, _ := h.tapWith(t, first, h.nextTap(t)); resp.StatusCode != http.StatusOK {
		t.Fatalf("first activation answered %d", resp.StatusCode)
	}
	h.assertSessionCount(t, 1)

	second := h.client(t)
	code2 := h.issue(t)
	if _, err := second.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code2)); err != nil {
		t.Fatalf("second link: %v", err)
	}
	if warn := h.getBody(t, second, "/activate"); !strings.Contains(warn, "This is a new phone") {
		t.Error("the second activation must warn before signing the other phone out")
	}
	h.consent(t, second, code2)
	h.assertSessionCount(t, 1) // consent revoked nothing
	resp, page := h.tapWith(t, second, h.nextTap(t))
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "other phone has been signed out") {
		t.Fatalf("second activation answered %d without saying what happened to the other phone", resp.StatusCode)
	}

	h.assertSessionCount(t, 2)
	h.assertLiveSessionCount(t, 1)
	h.assertAudit(t, ActionDeviceReplaced, 1)
	if got := h.status(t, first); got != "none" {
		t.Errorf("the revoked phone's status = %q, want none", got)
	}
}

// TestE2E_AClientCannotDeclareThePracticeFlag: with the practice tap retired (ADR
// 0025) the engine sets practice on nothing — and no form field a client can
// invent turns it back on, on a first record or on a checkout (the M4-06
// hours-inflation exploit stays closed at the HTTP boundary too).
func TestE2E_AClientCannotDeclareThePracticeFlag(t *testing.T) {
	h := newHarness(t, "invited")
	claims := url.Values{
		"practice": {"true"}, "Practice": {"1"}, "is_practice": {"yes"},
		"training": {"true"}, "practice_tap": {"on"},
	}
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)

	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, claims); w.Code != http.StatusOK {
		t.Fatalf("first tap status = %d", w.Code)
	}
	if rec := h.lastRecord(t, emp); rec.Practice {
		t.Fatalf("a client declared its first record practice (verdict %q)", rec.Verdict)
	}

	other := h.newEmployee(t, "active")
	h.seedTapAgedBy(t, other, 600*time.Second, "in", false)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+2), h.cookieForEmployee(t, other), claims); w.Code != http.StatusOK {
		t.Fatalf("checkout status = %d", w.Code)
	}
	rec := h.lastRecord(t, other)
	if rec.Type == nil || *rec.Type != "out" || rec.Practice {
		t.Fatalf("checkout = %q practice=%v, want out/false", deref(rec.Type), rec.Practice)
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

// extraPlaque mounts one more active plaque of this tenant at a NEW location and
// returns its uid.
func (h *harness) extraPlaque(t *testing.T, kek []byte, startCtr int32) string {
	t.Helper()
	uidBytes := make([]byte, 7)
	if _, err := rand.Read(uidBytes); err != nil {
		t.Fatalf("rand: %v", err)
	}
	uid := strings.ToUpper(hex.EncodeToString(uidBytes))
	tagKey, err := hex.DecodeString(tapFakeTagKey)
	if err != nil {
		t.Fatalf("tag key: %v", err)
	}
	ref, err := sun.Wrap(kek, uidBytes, tagKey)
	if err != nil {
		t.Fatalf("sun.Wrap: %v", err)
	}
	loc := uuid.New()
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng)
			VALUES ($1, $2, 'Sliema', '{198.51.100.0/24}', 35.912, 14.502)`, loc, h.tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			VALUES ($1, $2, $3, $4, $5, 'active')`, uid, h.tenantID, loc, ref, startCtr)
		return e
	}); err != nil {
		t.Fatalf("extra plaque: %v", err)
	}
	return uid
}

func (h *harness) assertConsented(t *testing.T, want bool) {
	t.Helper()
	h.assertCount(t,
		`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2 AND consented_at IS NOT NULL`,
		map[bool]int{true: 1, false: 0}[want], "consented invitations")
}

// assertAuditMentions requires a row of action whose detail contains needle.
func (h *harness) assertAuditMentions(t *testing.T, action, needle string) {
	t.Helper()
	var n int
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND position($3 in detail::text) > 0`,
			h.tenantID, action, needle).Scan(&n)
	}); err != nil {
		t.Fatalf("read audit_log: %v", err)
	}
	if n == 0 {
		t.Fatalf("no %s row mentions %q", action, needle)
	}
}

func (h *harness) assertAuditDoesNotMention(t *testing.T, needle string) {
	t.Helper()
	var n int
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND position($2 in detail::text) > 0`,
			h.tenantID, needle).Scan(&n)
	}); err != nil {
		t.Fatalf("read audit_log: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d audit rows of this tenant mention %q", n, needle)
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
