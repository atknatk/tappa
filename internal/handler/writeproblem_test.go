package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

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
