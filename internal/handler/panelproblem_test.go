package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/pages"
)

// The panel's problem card (M12-10): AdminAuth.renderProblem draws pages.PanelProblem,
// and pages.Problem stays the tap and activation screens' page
// (TestUnbrandedScreens_AreByteIdenticalToTheGolden holds those bytes).

const (
	panelProblemCard      = `<section class="flex max-w-xl flex-col gap-3 rounded-[18px] border border-line bg-paper p-6">`
	panelProblemChipOpen  = `<span class="rounded-md border border-ink px-2 py-0.5 font-mono text-xs font-bold tracking-[0.1em]">`
	panelProblemOwnerPill = `<span class="inline-flex items-center gap-1.5 rounded-md bg-ink px-2 py-0.5 font-mono text-[11px] tracking-[0.1em] text-paper">`
	panelProblemSignIn    = `href="/admin/login"`
	panelProblemThePass   = `href="/admin"`
)

// TestPanelProblem_TheCodeChipIsTheStatusTheResponseAnswersWith: the chip prints the
// status renderProblem was given, which is the response's own status line; a problem
// sent with 200 prints no chip rather than a "200".
func TestPanelProblem_TheCodeChipIsTheStatusTheResponseAnswersWith(t *testing.T) {
	h := newAdminRouterWithBrands(t, &fakeAdmins{}, &fakeBrands{}, discardLogger())
	for _, c := range []struct {
		status int
		v      pages.ProblemView
		chip   bool
	}{
		{http.StatusInternalServerError, problemPanelUnavailable, true},
		{http.StatusBadRequest, problemAdminRestart, true},
		{http.StatusTooManyRequests, problemAdminTooMany, true},
		{http.StatusForbidden, problemBillingOwnerOnly, true},
		{http.StatusServiceUnavailable, problemAPIKeysUnavailable, true},
		{http.StatusOK, problemAdminRestart, false},
	} {
		rec := httptest.NewRecorder()
		h.renderProblem(rec, httptest.NewRequest(http.MethodGet, transactionsHref, nil), c.status, c.v)
		if rec.Code != c.status {
			t.Fatalf("%d %q: answered %d", c.status, c.v.Title, rec.Code)
		}
		body := htmlOf(t, rec)
		if n := strings.Count(body, panelProblemCard); n != 1 {
			t.Fatalf("%d %q: the panel card is drawn %d times, want 1", c.status, c.v.Title, n)
		}
		chip := panelProblemChipOpen + strconv.Itoa(c.status) + `</span>`
		if got := strings.Contains(body, chip); got != c.chip {
			t.Errorf("%d %q: chip %q drawn %v, want %v", c.status, c.v.Title, chip, got, c.chip)
		}
		if !c.chip && strings.Contains(body, panelProblemChipOpen) {
			t.Errorf("%d %q: a chip is drawn for a non-error status", c.status, c.v.Title)
		}
	}
}

// TestPanelProblem_TheOwnerPillComesFromTheConstant: the two owner-only refusals carry
// OwnerOnly and draw the pill; no other panel, tap or activation constant does.
func TestPanelProblem_TheOwnerPillComesFromTheConstant(t *testing.T) {
	owner := map[string]pages.ProblemView{
		"problemBillingOwnerOnly": problemBillingOwnerOnly,
		"problemAPIKeysOwnerOnly": problemAPIKeysOwnerOnly,
	}
	others := map[string]pages.ProblemView{
		"problemPanelUnavailable": problemPanelUnavailable, "problemPanelWriteFailed": problemPanelWriteFailed,
		"problemAdminServer": problemAdminServer, "problemAdminRestart": problemAdminRestart,
		"problemAdminTooMany": problemAdminTooMany, "problemAdminNoCookie": problemAdminNoCookie,
		"problemAdminSignInAgain":  problemAdminSignInAgain,
		"problemAccountUnreadable": problemAccountUnreadable, "problemAPIKeysUnavailable": problemAPIKeysUnavailable,
		"tapProblemServer": tapProblemServer, "tapProblemBadURL": tapProblemBadURL,
		"problemServer": problemServer, "problemBadLink": problemBadLink,
	}
	h := newAdminRouterWithBrands(t, &fakeAdmins{}, &fakeBrands{}, discardLogger())
	for name, v := range owner {
		if !v.OwnerOnly {
			t.Errorf("%s does not carry OwnerOnly", name)
		}
		rec := httptest.NewRecorder()
		h.renderProblem(rec, httptest.NewRequest(http.MethodGet, transactionsHref, nil), http.StatusForbidden, v)
		body := htmlOf(t, rec)
		if !strings.Contains(body, panelProblemOwnerPill) || !strings.Contains(body, "OWNER ONLY") {
			t.Errorf("%s: no owner pill", name)
		}
	}
	for name, v := range others {
		if v.OwnerOnly {
			t.Errorf("%s carries OwnerOnly; it is not a refusal about the caller's role", name)
		}
		rec := httptest.NewRecorder()
		h.renderProblem(rec, httptest.NewRequest(http.MethodGet, transactionsHref, nil), http.StatusInternalServerError, v)
		if body := htmlOf(t, rec); strings.Contains(body, "OWNER ONLY") || strings.Contains(body, panelProblemOwnerPill) {
			t.Errorf("%s: an owner pill on a problem that is not owner-only", name)
		}
	}

	// THROUGH THE ROUTER: a manager at the billing section gets the pill.
	rec := billingBrowser(t, newFakeBooks(), "manager").do(http.MethodGet, billingHref, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("billing as a manager: %d, want 403", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, panelProblemOwnerPill) || !strings.Contains(body, panelProblemChipOpen+"403</span>") {
		t.Error("billing as a manager: the card has no owner pill or no 403 chip")
	}
	// A signed-in manager is sent back to The Pass, not to the sign-in page.
	if !strings.Contains(body, panelProblemThePass) || strings.Contains(body, panelProblemSignIn) {
		t.Error("billing as a manager: the way on is not The Pass")
	}
}

// TestPanelProblem_AnUnauthenticatedProblemLinksToTheSignInPage: a problem met without
// a live panel session offers the sign-in page and never The Pass; a signed-in one
// offers The Pass. Driven through the router both ways, plus the bare-request case (a
// refusal in front of the resolver).
func TestPanelProblem_AnUnauthenticatedProblemLinksToTheSignInPage(t *testing.T) {
	// The sign-in family: the business picker with no choice cookie.
	b := newBrowser(t, newAdminRouter(t, &fakeAdmins{}, &fakeTrail{}))
	rec := b.do(http.MethodGet, "/admin/login/choose", nil)
	if rec.Code < 400 {
		t.Fatalf("the picker without a choice answered %d, want a problem", rec.Code)
	}
	signedOut := htmlOf(t, rec)

	// A refusal in front of the resolver: the request never resolved a session.
	bare := httptest.NewRecorder()
	h := newAdminRouterWithBrands(t, &fakeAdmins{}, &fakeBrands{}, discardLogger())
	h.renderProblem(bare, httptest.NewRequest(http.MethodGet, transactionsHref, nil),
		http.StatusTooManyRequests, problemAdminTooMany)

	for name, body := range map[string]string{"picker": signedOut, "before the resolver": htmlOf(t, bare)} {
		if !strings.Contains(body, panelProblemCard) {
			t.Fatalf("%s: not the panel card", name)
		}
		if n := strings.Count(body, panelProblemSignIn); n != 1 {
			t.Errorf("%s: the sign-in link appears %d times, want 1", name, n)
		}
		if !strings.Contains(body, ">Go to sign in</a>") {
			t.Errorf("%s: the link is not labelled \"Go to sign in\"", name)
		}
		if strings.Contains(body, panelProblemThePass) || strings.Contains(body, "Back to The Pass") {
			t.Errorf("%s: an unauthenticated problem offers The Pass", name)
		}
	}

	// CONTROL: a signed-in failed read offers The Pass and not the sign-in page.
	acc := newFakeAccount()
	acc.readErr = errors.New("boom")
	rec = accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole).do(http.MethodGet, accountHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed account read answered %d, want 500", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, panelProblemThePass+` class=`) || !strings.Contains(body, ">Back to The Pass</a>") {
		t.Error("a signed-in problem does not offer The Pass")
	}
	if strings.Contains(body, panelProblemSignIn) {
		t.Error("a signed-in problem offers the sign-in page")
	}
}

// TestPanelProblem_KeepsTheConstantsWordsAndAddsNone: the card says the constant's
// title, message and hint, the code, the pill and the one link -- nothing else; in
// particular no promise about taps and no tap-screen button.
func TestPanelProblem_KeepsTheConstantsWordsAndAddsNone(t *testing.T) {
	h := newAdminRouterWithBrands(t, &fakeAdmins{}, &fakeBrands{}, discardLogger())
	for _, v := range []pages.ProblemView{problemPanelUnavailable, problemPanelWriteFailed, problemAccountUnreadable, problemAPIKeysOwnerOnly} {
		rec := httptest.NewRecorder()
		h.renderProblem(rec, httptest.NewRequest(http.MethodGet, transactionsHref, nil), http.StatusForbidden, v)
		body := htmlOf(t, rec)
		for _, want := range []string{v.Title, v.Message, v.Hint} {
			if !strings.Contains(body, htmlText(want)) {
				t.Errorf("%q: the card does not say %q", v.Title, want)
			}
		}
		for _, never := range []string{"still being recorded", "tap-button", "Try again</a>", "<form", "<script", "style="} {
			if strings.Contains(body, never) {
				t.Errorf("%q: the card carries %q", v.Title, never)
			}
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != adminCSP {
			t.Errorf("%q: policy %q, want adminCSP's literal", v.Title, got)
		}
	}
}
