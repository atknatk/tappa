package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// panelScreensOutsideTheSections renders the panel screens and states the touch-target and
// form-control corpora missed until M12-49, through the real router as a manager's (or a
// signed-out visitor's) browser receives them. panelNonSectionScreens adds them to its corpus,
// and TestPanelScreens_FormControlsCarryATouchTargetClass reads that corpus too.
func panelScreensOutsideTheSections(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	var rec *httptest.ResponseRecorder

	// 🔴 THE SCREENS M12-49 FOUND OUTSIDE THIS CORPUS, each measured: a press target or a
	// form control on them could lose its touch-target class with the whole package green.
	//
	// Billing's warning step: a POST that renders "Freeze this month" -- the last press
	// before a row that can never be corrected (migration 0016).
	bb := billingBrowser(t, newFakeBooks(), billingTestOwnerRole)
	rec = bb.do(http.MethodPost, billingCloseHref, billingForm(billingTestMonth.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("the billing warning step answered %d, want 200", rec.Code)
	}
	out["POST "+billingCloseHref] = htmlOf(t, rec)

	// The venue editor (?venue=<id>, M12-35), a new venue and a new department: each is a
	// page of its own reached by a query, so no section href draws its fields.
	venues := twoVenues()
	vb2 := venueBrowser(t, venues)
	for _, q := range []string{"?venue=" + venues.screen.Venues[0].ID.String(), "?venue=new", "?department=new"} {
		rec = vb2.do(http.MethodGet, locationsHref+q, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s%s answered %d, want 200", locationsHref, q, rec.Code)
		}
		out["GET "+locationsHref+q] = htmlOf(t, rec)
	}

	// Password recovery: the request form, the page after it, and the new-password form the
	// e-mailed link opens. Signed out, the same brand surface as the sign-in page.
	known := "owner@registered.example.test"
	mail := &recordingChannel{}
	resetRouter, resetHandler := newResetRouter(t,
		&fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: {grantFor(known)}}},
		mail, &fakeTrail{})
	rb := newBrowser(t, resetRouter)
	rec = rb.do(http.MethodGet, adminResetPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200", adminResetPath, rec.Code)
	}
	out["GET "+adminResetPath] = htmlOf(t, rec)
	rec = rb.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrfFrom(t, out["GET "+adminResetPath])}, "email": {known}})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s answered %d, want 200", adminResetPath, rec.Code)
	}
	out["POST "+adminResetPath] = htmlOf(t, rec)
	drain(t, resetHandler)
	delivered := mail.all()
	if len(delivered) != 1 {
		t.Fatalf("the recovery channel received %d link(s), want 1", len(delivered))
	}
	// The link sets the browser's recovery cookie and redirects; the form is then read
	// without the token, as a browser reads it.
	rb.do(http.MethodGet, adminResetNewPath+"?t="+strings.TrimPrefix(delivered[0].Link, resetHandler.linkBase+"?t="), nil)
	rec = rb.do(http.MethodGet, adminResetNewPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s answered %d, want 200", adminResetNewPath, rec.Code)
	}
	out["GET "+adminResetNewPath] = htmlOf(t, rec)

	// The business picker, which only a sign-in that verified two businesses reaches.
	first := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	second := adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	chooser := &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return adminauth.Authentication{Resolved: 2, Attempts: []adminauth.Attempt{
			{AdminUserID: first.AdminUserID, TenantID: first.TenantID, PasswordMatched: true, Active: true},
			{AdminUserID: second.AdminUserID, TenantID: second.TenantID, PasswordMatched: true, Active: true},
		}}, nil
	}}
	cb := newBrowser(t, newAdminRouter(t, chooser, &fakeTrail{}))
	csrf := csrfFrom(t, htmlOf(t, cb.do(http.MethodGet, adminLoginPath, nil)))
	if rec = cb.do(http.MethodPost, adminLoginPath, url.Values{
		"csrf": {csrf}, "email": {"owner@example.test"}, "password": {"right"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("two verified businesses answered %d, want 303 to the picker", rec.Code)
	}
	rec = cb.do(http.MethodGet, "/admin/login/choose", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/login/choose answered %d, want 200", rec.Code)
	}
	out["GET /admin/login/choose"] = htmlOf(t, rec)
	return out
}
