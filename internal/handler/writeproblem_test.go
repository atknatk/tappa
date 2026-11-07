package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The T21 classification, read off rendered pages: a press whose write failed is told
// so in the writer's words, never "this page is not showing anything".

// saysProblem reports whether body is v's card and not the reader's.
func saysProblem(t *testing.T, rec *httptest.ResponseRecorder, v pages.ProblemView) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("answered %d, want 500", rec.Code)
	}
	body := htmlOf(t, rec)
	for _, want := range []string{v.Title, v.Message, v.Hint} {
		if !strings.Contains(body, htmlText(want)) {
			t.Errorf("the page does not say %q", want)
		}
	}
	for _, never := range []string{problemPanelUnavailable.Title, "not showing anything"} {
		if strings.Contains(body, htmlText(never)) {
			t.Errorf("a writer is told %q", never)
		}
	}
}

// TestWriteProblem_AFailedWriteSaysNothingWasWritten: the write itself failed (one
// transaction, rolled back) -- the writer's page.
func TestWriteProblem_AFailedWriteSaysNothingWasWritten(t *testing.T) {
	employeeID := uuid.New()
	staff := &fakeStaff{}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})
	form := deactivateConfirmed(t, b, employeeID, nil)
	staff.deactivateErr = errors.New("connection reset")
	saysProblem(t, b.do(http.MethodPost, employeeDeactivateHref, form), problemPanelWriteFailed)
}

// TestWriteProblem_AFailedReadBeforeTheWriteSaysNothingWasWritten: the read the write
// depends on failed, so the write was never reached -- the writer's page too.
func TestWriteProblem_AFailedReadBeforeTheWriteSaysNothingWasWritten(t *testing.T) {
	staff := &fakeStaff{personErr: errors.New("connection reset")}
	b := panelBrowserWithActions(t, staff, &fakeInviter{})
	saysProblem(t, b.do(http.MethodPost, employeeInviteHref, url.Values{"id": {uuid.New().String()}}),
		problemPanelWriteFailed)
}

// TestWriteProblem_AnInvitationThatNeverCommittedSaysNothingWasWritten: IssueAndDeliver's
// transaction failed (no id) -- nothing minted, the writer's page.
func TestWriteProblem_AnInvitationThatNeverCommittedSaysNothingWasWritten(t *testing.T) {
	b := panelBrowserWithActions(t, &fakeStaff{}, &fakeInviter{err: errors.New("connection reset")})
	saysProblem(t, b.do(http.MethodPost, employeeInviteHref, url.Values{"id": {uuid.New().String()}}),
		problemPanelWriteFailed)
}

// TestWriteProblem_AnInvitationThatCommittedButWasNotShownSaysItMayBeHalfDone: delivery
// failed after the commit (the result carries an id), so "nothing was written" would be
// false -- the unfinished page, which still says no record has been lost.
func TestWriteProblem_AnInvitationThatCommittedButWasNotShownSaysItMayBeHalfDone(t *testing.T) {
	if got := inviteFailure(invite.Invite{ID: uuid.New()}); got != problemPanelWriteUnfinished {
		t.Errorf("a committed invitation gets %q, want the unfinished page", got.Title)
	}
	if got := inviteFailure(invite.Invite{}); got != problemPanelWriteFailed {
		t.Errorf("an invitation that never committed gets %q, want the writer's page", got.Title)
	}
	if !strings.Contains(problemPanelWriteUnfinished.Message, "no record has been lost") {
		t.Error("the unfinished page drops §4.6's sentence")
	}
	for _, never := range []string{"nothing was written", "Nothing has been changed"} {
		if strings.Contains(problemPanelWriteUnfinished.Message, never) {
			t.Errorf("the unfinished page claims %q", never)
		}
	}
}

// TestWriteProblem_ASharedHelperPicksThePageByMethod: a read failure in a helper a GET
// and a POST share is the reader's page to a reader and the writer's to a press.
func TestWriteProblem_ASharedHelperPicksThePageByMethod(t *testing.T) {
	for _, c := range []struct {
		method string
		want   pages.ProblemView
	}{
		{http.MethodGet, problemPanelUnavailable},
		{http.MethodHead, problemPanelUnavailable},
		{http.MethodPost, problemPanelWriteFailed},
	} {
		if got := readFailedProblem(httptest.NewRequest(c.method, "/admin", nil)); got != c.want {
			t.Errorf("%s: %q, want %q", c.method, got.Title, c.want.Title)
		}
	}
	if !strings.Contains(problemPanelUnavailable.Message, "no record has been lost") {
		t.Error("the reader's page drops §4.6's sentence")
	}
}

// THE CALL-SITE WIRES (T21 review): each test below drives one converted call site
// through the router, so a site reverted to the wrong page turns it red -- the census
// sees only the reader's page, and the unit tests above only the helpers.

// TestWriteProblem_AnInvitationCommittedButUndeliveredIsUnfinishedOnEveryRoute: the panel
// mode (employeeInvite) and the e-mail mode's on-screen fallback (employeeInviteOnScreen),
// each with a delivery failure after the commit and with a channel that never showed the
// link.
func TestWriteProblem_AnInvitationCommittedButUndeliveredIsUnfinishedOnEveryRoute(t *testing.T) {
	screen := url.Values{"id": {uuid.NewString()}, inviteDeliverField: {inviteDeliverScreen}}
	for _, c := range []struct {
		name    string
		invites *fakeInviter
		email   bool
	}{
		{"panel mode, delivery failed after the commit", &fakeInviter{afterCommit: errors.New("audit row refused")}, false},
		{"panel mode, the link never reached the screen", &fakeInviter{undelivered: true}, false},
		{"on-screen fallback, delivery failed after the commit", &fakeInviter{afterCommit: errors.New("audit row refused")}, true},
		{"on-screen fallback, the link never reached the screen", &fakeInviter{undelivered: true}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !c.email {
				b := panelBrowserWithActions(t, &fakeStaff{}, c.invites)
				saysProblem(t, b.do(http.MethodPost, employeeInviteHref, url.Values{"id": {uuid.NewString()}}),
					problemPanelWriteUnfinished)
				return
			}
			b, _ := emailPanel(t, "owner", &fakeStaff{}, c.invites, &fakeTrail{}, &fakeMailSender{})
			saysProblem(t, b.do(http.MethodPost, employeeInviteHref, screen), problemPanelWriteUnfinished)
		})
	}
}

// TestWriteProblem_TheRosterReadIsTheReadersOnAGETAndTheWritersAfterARefusedAdd:
// employeesView is shared -- the section's GET and employeeAdd's refusal (addFormAgain)
// -- so the same failed roster read answers two pages.
func TestWriteProblem_TheRosterReadIsTheReadersOnAGETAndTheWritersAfterARefusedAdd(t *testing.T) {
	records := newFakeLedger()
	records.rosterErr = errors.New("connection reset")
	b := panelBrowserWithLedger(t, records, &fakeStaff{addErr: tenant.ErrEmailTaken}, &fakeInviter{})
	saysReader(t, b.do(http.MethodGet, employeesHref, nil))
	saysProblem(t, b.do(http.MethodPost, employeeAddHref, addForm()), problemPanelWriteFailed)
}

// TestWriteProblem_TheClaimQueueReadIsTheReadersOnAGETAndTheWritersAfterARefusedDecision:
// renderClaims is shared -- the queue's GET and claimDecision's refusal (an edited time
// that does not parse, refused before Decide).
func TestWriteProblem_TheClaimQueueReadIsTheReadersOnAGETAndTheWritersAfterARefusedDecision(t *testing.T) {
	f := newFakeClaimQueue(time.UTC)
	f.listErr = errors.New("timeout")
	b := claimPanel(t, claimAdmins("manager", false), f)
	saysReader(t, b.do(http.MethodGet, claimsHref, nil))
	form := decisionForm(uuid.New(), "approved")
	form.Set("at", "not a time")
	saysProblem(t, b.do(http.MethodPost, claimsHref, form), problemPanelWriteFailed)
	if n := len(f.calls); n != 0 {
		t.Fatalf("the refused decision reached Decide %d time(s); the page's claim would be untested", n)
	}
}

// TestWriteProblem_AWindowActWhoseFreshSessionLeftATrailIsUnfinished: accountWindowAct
// failing after its fresh session was opened (and recorded) is the unfinished page; the
// same failure before any session is the writer's.
func TestWriteProblem_AWindowActWhoseFreshSessionLeftATrailIsUnfinished(t *testing.T) {
	fresh := adminauth.Session{ID: uuid.New(), TenantID: panelTestTenant, AdminUserID: panelTestAdmin}
	for _, c := range []struct {
		name string
		act  adminauth.FreshAct
		want pages.ProblemView
	}{
		{"a fresh session was opened", adminauth.FreshAct{Temporary: []adminauth.Session{fresh}, Revoked: []bool{true}}, problemPanelWriteUnfinished},
		{"no session was opened", adminauth.FreshAct{}, problemPanelWriteFailed},
	} {
		t.Run(c.name, func(t *testing.T) {
			admins := &fakeAdmins{}
			admins.ident.unlink = func(adminauth.Resolved, string, uuid.UUID) (adminauth.FreshAct, bool, error) {
				return c.act, false, errors.New("connection reset")
			}
			b, _ := identityPanel(t, true, true, admins, nil)
			saysProblem(t, b.do(http.MethodPost, accountUnlinkHref, url.Values{"tenant_id": {uuid.NewString()}, "password": {"pw"}}), c.want)
		})
	}
}

// saysReader is the reader's page: a GET's failed read.
func saysReader(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("answered %d, want 500", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, htmlText(problemPanelUnavailable.Title)) || strings.Contains(body, htmlText(problemPanelWriteFailed.Title)) {
		t.Error("a reader is not shown the reader's page")
	}
}
