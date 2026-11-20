package handler

// nfcless_test.go -- M16-23 against fakes: the live QR activation rule (pure), the wizard's
// waiting screen and approval routes (activate_test.go's fake harness), and the panel's
// "Phones waiting for approval" (the panel browser). Real Postgres is nfcless_db_test.go's.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/internal/policy"
)

// TestLiveQRActivationRefusal_IsTheOneRuleForBothReaders: what may activate, case by case -- only a
// verified token of the invitation's OWN business's screen in service, inside its window plus the
// network grace. A foreign screen's id is never handed back (it would reach this tenant's trail).
func TestLiveQRActivationRefusal_IsTheOneRuleForBothReaders(t *testing.T) {
	own, other := uuid.New(), uuid.New()
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	screen := uuid.New()
	ok := func(mut func(*liveQROpening)) liveQROpening {
		o := liveQROpening{
			Verified: true,
			Screen:   db.ResolvedLiveQRDisplay{ID: screen, TenantID: own, LocationID: uuid.New(), LocationActive: true},
			Claims:   liveqr.Claims{DisplayID: screen, IssuedAt: now.Add(-5 * time.Second), Lifetime: liveqr.TokenLifetime},
		}
		if mut != nil {
			mut(&o)
		}
		return o
	}
	revoked := now
	grace := time.Duration(policy.LiveQRNetworkGraceSeconds) * time.Second
	for _, tc := range []struct {
		name, reason string
		o            liveQROpening
		display      uuid.UUID
	}{
		{"a fresh code of our own running screen", "", ok(nil), uuid.Nil},
		{"just inside the grace", "", ok(func(o *liveQROpening) { o.Claims.IssuedAt = now.Add(-liveqr.TokenLifetime - grace) }), uuid.Nil},
		{"past the grace", "live_qr_expired", ok(func(o *liveQROpening) { o.Claims.IssuedAt = now.Add(-liveqr.TokenLifetime - grace - time.Second) }), screen},
		{"a malformed code", "live_qr_malformed", liveQROpening{Refusal: liveRefusalMalformed}, uuid.Nil},
		{"a forged code", "live_qr_signature", liveQROpening{Refusal: liveRefusalSignature}, uuid.Nil},
		{"a screen we do not hold", "live_qr_unknown_screen", liveQROpening{Refusal: liveRefusalUnknownScreen}, uuid.Nil},
		{"another business's screen", "foreign_tenant_screen", ok(func(o *liveQROpening) { o.Screen.TenantID = other }), uuid.Nil},
		{"a removed screen", "screen_not_active", ok(func(o *liveQROpening) { o.Screen.RevokedAt = &revoked }), screen},
		{"a closed venue", "screen_not_active", ok(func(o *liveQROpening) { o.Screen.LocationActive = false }), screen},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reason, display := liveQRActivationRefusal(tc.o, own, now)
			if reason != tc.reason || display != tc.display {
				t.Fatalf("= (%q, %v), want (%q, %v)", reason, display, tc.reason, tc.display)
			}
		})
	}
}

// TestApproval_FormsPostToTheRoutes: the two approval forms' literal actions are the routes'
// constants (the templates cannot import the handler package).
func TestApproval_FormsPostToTheRoutes(t *testing.T) {
	src, err := os.ReadFile("../../web/templates/pages/activate.templ")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`action="` + ActivationApprovalPath + `"`, `action="` + ActivationApprovalCollectPath + `"`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("activate.templ has no form with %s", want)
		}
	}
}

// TestWaitingScreen_OffersTheManagerOnlyWhereNoScreenRuns: the waiting screen draws "My phone
// can't tap" when the gate says no screen runs, and the scan line otherwise.
func TestWaitingScreen_OffersTheManagerOnlyWhereNoScreenRuns(t *testing.T) {
	for _, offered := range []bool{true, false} {
		inv := &fakeInvites{approvalOffered: &offered}
		h := newHandler(t, inv, &fakeSessions{}, &fakeAudit{})
		body := get(t, h, "/activate?step=3", pendingCookie()).Body.String()
		button := strings.Contains(body, "My phone can't tap") && strings.Contains(body, `action="/activate/approval"`)
		line := strings.Contains(body, "scan the code on the Taptime screen at your workplace")
		if button != offered || line == offered {
			t.Errorf("offered=%v: button %v, scan line %v", offered, button, line)
		}
	}
}

// postTo is a same-origin form POST to path from a browser holding a consented activation.
func postTo(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(pendingCookie())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// hasReason reports an event of action whose detail carries reason.
func (f *fakeAudit) hasReason(action, reason string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if d, ok := e.Detail.(activationDetail); ok && e.Action == action && d.Reason == reason {
			return true
		}
	}
	return false
}

// mentions reports whether any event's detail, as JSON, contains needle.
func (f *fakeAudit) mentions(needle string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		b, _ := json.Marshal(e.Detail)
		if strings.Contains(string(b), needle) {
			return true
		}
	}
	return false
}

// TestRequestApproval_TheGateIsTheServers: a request posted where a screen runs is the scan
// screen (409), recorded as screen_available -- not merely a hidden button; elsewhere the request
// opens for the BROWSER and lands on the approval page, recorded as approval_requested with no
// pairing code in the trail. A request without the form token opens nothing.
func TestRequestApproval_TheGateIsTheServers(t *testing.T) {
	t.Run("a screen runs", func(t *testing.T) {
		rec := &fakeAudit{}
		inv := &fakeInvites{openErr: invite.ErrScreenAvailable}
		w := postTo(t, newHandler(t, inv, &fakeSessions{}, rec), ActivationApprovalPath, url.Values{"csrf": {fakeCSRF}})
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "Scan the screen&#39;s code instead") {
			t.Fatalf("= %d, want 409 and the scan screen; body:\n%s", w.Code, w.Body.String())
		}
		if !rec.hasReason(ActionActivationFailed, "screen_available") {
			t.Error("the refusal left no activation.failed row with its reason")
		}
	})
	t.Run("no screen runs", func(t *testing.T) {
		rec := &fakeAudit{}
		inv := &fakeInvites{}
		w := postTo(t, newHandler(t, inv, &fakeSessions{}, rec), ActivationApprovalPath, url.Values{"csrf": {fakeCSRF}})
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != ActivationApprovalPath {
			t.Fatalf("= %d -> %q, want 303 to the approval page", w.Code, w.Header().Get("Location"))
		}
		if inv.openCalls != 1 || inv.openClient != invite.ApprovalClientBrowser {
			t.Errorf("OpenApproval calls %d, client %q", inv.openCalls, inv.openClient)
		}
		if !rec.has(ActionApprovalRequested) || rec.mentions("482913") {
			t.Error("approval_requested is missing, or the trail carries the pairing code")
		}
	})
	t.Run("no form token", func(t *testing.T) {
		inv := &fakeInvites{}
		w := postTo(t, newHandler(t, inv, &fakeSessions{}, &fakeAudit{}), ActivationApprovalPath, url.Values{})
		if w.Code != http.StatusBadRequest || inv.openCalls != 0 {
			t.Fatalf("= %d with %d opens, want 400 and none", w.Code, inv.openCalls)
		}
	})
}

// TestApprovalPage_ShowsTheCodeAndTheState: waiting shows the grouped code and "Check now"; an
// approval "Finish setup"; a rejection and an expiry their sentences and a new-request button.
func TestApprovalPage_ShowsTheCodeAndTheState(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  []string
	}{
		{invite.ApprovalWaiting, []string{"482 913", "Ask your manager to approve this phone", "Check now", "The code works for 15 minutes."}},
		{invite.ApprovalApproved, []string{"Your manager approved this phone", "Finish setup"}},
		{invite.ApprovalRejected, []string{"Your manager didn&#39;t approve this phone", "Ask again"}},
		{invite.ApprovalExpired, []string{"That code ran out of time", "Get a new code"}},
	} {
		t.Run(tc.state, func(t *testing.T) {
			ap := invite.Approval{RequestID: testRequest, PairingCode: invite.ParsePairingCode("482913"), State: tc.state}
			h := newHandler(t, &fakeInvites{approval: &ap}, &fakeSessions{}, &fakeAudit{})
			body := get(t, h, ActivationApprovalPath, pendingCookie()).Body.String()
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("the %s page does not say %q", tc.state, w)
				}
			}
			if tc.state != invite.ApprovalWaiting && strings.Contains(body, "482 913") {
				t.Errorf("the %s page shows the code", tc.state)
			}
		})
	}
}

// TestApprovalStatus_SaysOnlyTheState: the poll answers the state word and nothing about who.
func TestApprovalStatus_SaysOnlyTheState(t *testing.T) {
	ap := invite.Approval{RequestID: testRequest, PairingCode: invite.ParsePairingCode("482913"), State: invite.ApprovalApproved}
	h := newHandler(t, &fakeInvites{approval: &ap}, &fakeSessions{}, &fakeAudit{})
	w := get(t, h, ActivationApprovalStatusPath, pendingCookie())
	if got := strings.TrimSpace(w.Body.String()); got != `{"state":"approved"}` {
		t.Fatalf("status = %s", got)
	}
	if w := get(t, h, ActivationApprovalStatusPath); strings.TrimSpace(w.Body.String()) != `{"state":"none"}` {
		t.Fatalf("status without a pending cookie = %s", w.Body.String())
	}
}

// TestCollectApproval_FinishesOnlyAnApproval: an approved request is consumed through
// ActivateApproved (with the request and the approver in activation.completed); any other state
// sends the browser back to the approval page and consumes nothing.
func TestCollectApproval_FinishesOnlyAnApproval(t *testing.T) {
	for _, state := range []string{invite.ApprovalWaiting, invite.ApprovalRejected, invite.ApprovalExpired} {
		ap := invite.Approval{State: state}
		inv := &fakeInvites{approval: &ap}
		w := postTo(t, newHandler(t, inv, &fakeSessions{}, &fakeAudit{}), ActivationApprovalCollectPath, url.Values{"csrf": {fakeCSRF}})
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != ActivationApprovalPath || inv.approvedCalls != 0 {
			t.Errorf("%s: = %d -> %q with %d consumptions", state, w.Code, w.Header().Get("Location"), inv.approvedCalls)
		}
	}
	ap := invite.Approval{State: invite.ApprovalApproved}
	inv := &fakeInvites{approval: &ap}
	rec := &fakeAudit{}
	w := postTo(t, newHandler(t, inv, &fakeSessions{}, rec), ActivationApprovalCollectPath, url.Values{"csrf": {fakeCSRF}})
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), ActivationCompletePath) || inv.approvedCalls != 1 {
		t.Fatalf("approved: = %d -> %q with %d consumptions", w.Code, w.Header().Get("Location"), inv.approvedCalls)
	}
	if inv.activateCalls != 0 {
		t.Error("an approval was consumed through Activate, which collects nothing")
	}
	if !rec.mentions(proofManagerApproval) || !rec.mentions(testApprover.String()) {
		t.Error("activation.completed does not name the proof and the approver")
	}

	t.Run("the approval ran out in between", func(t *testing.T) {
		ap := invite.Approval{State: invite.ApprovalApproved}
		inv := &fakeInvites{approval: &ap, activateApproved: func(invite.Code) (invite.ApprovedActivation, error) {
			return invite.ApprovedActivation{Activation: invite.Activation{Context: okContext("invited")}}, invite.ErrApprovalNotGranted
		}}
		rec := &fakeAudit{}
		w := postTo(t, newHandler(t, inv, &fakeSessions{}, rec), ActivationApprovalCollectPath, url.Values{"csrf": {fakeCSRF}})
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != ActivationApprovalPath {
			t.Fatalf("= %d -> %q, want back to the approval page", w.Code, w.Header().Get("Location"))
		}
		if !rec.hasReason(ActionActivationFailed, "approval_not_granted") {
			t.Error("no activation.failed row with approval_not_granted")
		}
	})
}

// TestPanelApprovals_TheCardAndThePress: the people section lists the open requests with their
// grouped codes; a press sends the session's tenant and actor (never the body's) and answers each
// outcome with its own sentence; a failed read is said, not drawn as nobody waiting.
func TestPanelApprovals_TheCardAndThePress(t *testing.T) {
	reqID := uuid.New()
	staff := signedInStaff(0)
	staff.approvals = []tenant.PendingApproval{{ID: reqID, EmployeeName: "Rita Vella", LocationName: "St Julians",
		Client: "app", PairingCode: "482913", CreatedAt: time.Now().Add(-3 * time.Minute), ExpiresAt: time.Now().Add(12 * time.Minute)}}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})
	page := htmlOf(t, b.do(http.MethodGet, employeesHref, nil))
	for _, want := range []string{"Phones waiting for approval", "Rita Vella", "482 913", "3 min ago", "St Julians · App",
		`name="decision" value="approve"`, `name="decision" value="reject"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the people section does not draw %q", want)
		}
	}

	form := url.Values{"id": {reqID.String()}, "decision": {"approve"}, "tenant_id": {uuid.NewString()}, "actor_id": {uuid.NewString()}}
	rec := b.do(http.MethodPost, employeeApprovalHref, form)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != employeesHref+"?approval=approved" {
		t.Fatalf("approve = %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(staff.decisions) != 1 || staff.decisions[0].TenantID != panelTestTenant || staff.decisions[0].RequestID != reqID ||
		!staff.decisions[0].Approve || staff.decisions[0].ActorID == uuid.Nil {
		t.Fatalf("the domain got %+v", staff.decisions)
	}

	for _, tc := range []struct {
		err            error
		decision, word string
		sentence       string
	}{
		{nil, "reject", "rejected", "Phone rejected"},
		{tenant.ErrSelfApproval, "approve", "self", "You can&#39;t approve your own phone"},
		{tenant.ErrApprovalClosed, "approve", "closed", "That request is no longer open"},
		{tenant.ErrUnknownApproval, "approve", "unknown", "We could not find that request"},
	} {
		staff.decideErr = tc.err
		rec := b.do(http.MethodPost, employeeApprovalHref, url.Values{"id": {reqID.String()}, "decision": {tc.decision}})
		loc := rec.Header().Get("Location")
		if loc != employeesHref+"?approval="+tc.word {
			t.Errorf("%v: -> %q, want approval=%s", tc.err, loc, tc.word)
			continue
		}
		if body := htmlOf(t, b.do(http.MethodGet, loc, nil)); !strings.Contains(body, tc.sentence) {
			t.Errorf("approval=%s does not say %q", tc.word, tc.sentence)
		}
	}
	staff.decideErr = nil
	if rec := b.do(http.MethodPost, employeeApprovalHref, url.Values{"id": {"nope"}, "decision": {"approve"}}); rec.Header().Get("Location") != employeesHref+"?approval=unreadable" {
		t.Errorf("an unreadable press -> %q", rec.Header().Get("Location"))
	}

	staff.approvalsErr = errors.New("read failed")
	if body := htmlOf(t, b.do(http.MethodGet, employeesHref, nil)); !strings.Contains(body, "We could not read the phones waiting for approval") {
		t.Error("a failed read is not said")
	}
}

// TestLiveShaped_ChoosesOnlyTheWords: a version-2 (live QR) context this deployment signed for the
// session is "live shaped" whatever its age -- so an expired live QR page is told to scan again --
// and a plaque's context, another session's, a forged one and junk are not (M16-23 §9 (b)).
func TestLiveShaped_ChoosesOnlyTheWords(t *testing.T) {
	c, err := newTapContexts(tapCfg())
	if err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	live, err := c.mint(tapContext{Channel: "qr_live", DisplayID: uuid.New(), TokenVerified: true,
		TokenExpiresAt: time.Now().Add(20 * time.Second), TagTenantID: uuid.New(), LocationID: uuid.New()}, session)
	if err != nil {
		t.Fatal(err)
	}
	plaque, err := c.mint(tapContext{UID: "04A1B2C3D4E5F6", Ctr: 7, Channel: "nfc", CMACVerified: true,
		TagTenantID: uuid.New(), LocationID: uuid.New()}, session)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		v       string
		session uuid.UUID
		want    bool
	}{
		"a live QR page's context":   {live, session, true},
		"a plaque page's context":    {plaque, session, false},
		"another session's live one": {live, uuid.New(), false},
		"a live one with a bad tag":  {live[:len(live)-2] + "AA", session, false},
		"junk":                       {"not.a-context", session, false},
	} {
		if got := c.liveShaped(tc.v, tc.session); got != tc.want {
			t.Errorf("%s: liveShaped = %v, want %v", name, got, tc.want)
		}
	}
	if !strings.Contains(liveOr(true, tapProblemStale, tapProblemLiveStale).Hint, "Scan the code on the screen") ||
		liveOr(false, tapProblemStale, tapProblemLiveStale) != tapProblemStale {
		t.Error("liveOr picks the wrong words")
	}
}
