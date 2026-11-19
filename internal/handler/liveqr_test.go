package handler

// liveqr_test.go -- M16-20: the panel's live QR screens (liveqrpanel.go) through the REAL panel router
// with a fake screens service, and the screen page (livescreen.go) through its own routes with a fake
// one. The real service against real Postgres is liveqr_db_test.go.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/web/templates/pages"
)

// fakeLiveQR is the panel's screens service: it records each command and answers what the test set.
type fakeLiveQR struct {
	mu        sync.Mutex
	code      liveqr.PairingCode
	created   []liveqr.CreateCommand
	revoked   []liveqr.RevokeCommand
	list      []liveqr.Listed
	listErr   error
	revokeErr error
	revokeAt  uuid.UUID
}

func (f *fakeLiveQR) Create(_ context.Context, c liveqr.CreateCommand) (liveqr.Created, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, c)
	return liveqr.Created{ID: uuid.New(), Code: f.code, ExpiresAt: time.Now().Add(liveqr.PairingLifetime)}, nil
}

func (f *fakeLiveQR) List(context.Context, uuid.UUID, uuid.UUID) ([]liveqr.Listed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.list, f.listErr
}

func (f *fakeLiveQR) Revoke(_ context.Context, c liveqr.RevokeCommand) (liveqr.Revoked, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, c)
	return liveqr.Revoked{LocationID: f.revokeAt}, f.revokeErr
}

const liveQRScreenURLForTest = testBaseURL + "/screen"

// liveQRPanel is a signed-in admin of the given role on the real panel router, with the live QR on
// (svc non-nil) or off. The first venue of twoVenues is also reachable by id (fakeVenues.Venue).
func liveQRPanel(t *testing.T, role string, svc *fakeLiveQR) (*browser, *fakeVenues) {
	t.Helper()
	venues := twoVenues()
	first := venues.screen.Venues[0]
	venues.beyondVenues = map[uuid.UUID]tenant.Venue{first.ID: first}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "KF " + role,
		}, nil
	}}
	var opts []AdminAuthOption
	if svc != nil {
		opts = append(opts, LiveQRScreens(svc, liveQRScreenURLForTest))
	}
	b := newBrowser(t, newAdminRouterWithPlaques(t, admins, venues, &fakePlaques{}, opts...))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b, venues
}

func newTestCode(t *testing.T) liveqr.PairingCode {
	t.Helper()
	c, err := liveqr.NewPairingCode()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestLiveQRPanel_TheVenueDetailListsItsScreensAndOffersTheButton: the open venue's detail draws
// "Live QR screens" with "Add a live QR screen" (a bound POST carrying the venue), each screen's state
// as a word, and a Remove only where the screen can still show a code.
func TestLiveQRPanel_TheVenueDetailListsItsScreensAndOffersTheButton(t *testing.T) {
	now := time.Now()
	paired, waiting, dead := uuid.New(), uuid.New(), uuid.New()
	seen := now.Add(-3 * time.Minute)
	lapsed := uuid.New()
	svc := &fakeLiveQR{list: []liveqr.Listed{
		{ID: paired, State: liveqr.StatePaired, CreatedAt: now, PairingExpiresAt: now, PairedAt: &now, LastSeenAt: &seen},
		{ID: lapsed, State: liveqr.StateLapsed, CreatedAt: now, PairingExpiresAt: now, PairedAt: &now},
		{ID: waiting, State: liveqr.StateWaiting, CreatedAt: now, PairingExpiresAt: now.Add(9 * time.Minute)},
		{ID: dead, State: liveqr.StateRevoked, CreatedAt: now, PairingExpiresAt: now, RevokedAt: &now},
	}}
	b, venues := liveQRPanel(t, "manager", svc)
	venue := venues.screen.Venues[0].ID
	rec := b.do(http.MethodGet, locationsHref+"?at="+venue.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Live QR screens", "Add a live QR screen", liveQRScreenURLForTest,
		`value="` + venue.String() + `"`, "Showing codes", "Waiting for its code", "Removed", "Pairing ran out", "last seen " + seen.In(time.UTC).Format("2 Jan 15:04"),
		`name="display_id" value="` + paired.String() + `"`, `name="display_id" value="` + waiting.String() + `"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the detail does not carry %q", want)
		}
	}
	if strings.Contains(body, `name="display_id" value="`+dead.String()+`"`) || strings.Contains(body, `name="display_id" value="`+lapsed.String()+`"`) {
		t.Error("a removed or run-out screen offers Remove")
	}
	if !strings.Contains(body, `action="`+liveQRCreateHref) || !strings.Contains(body, `action="`+liveQRRevokeHref) {
		t.Error("the two forms do not post to the two routes")
	}

	svc.listErr = errors.New("db down")
	if rec := b.do(http.MethodGet, locationsHref+"?at="+venue.String(), nil); rec.Code != http.StatusInternalServerError ||
		strings.Contains(rec.Body.String(), "No screen has been added") {
		t.Fatalf("a failed read answered %d -- §4.6: never an empty list", rec.Code)
	}
}

// TestLiveQRPanel_UnconfiguredDrawsNothingAndMountsNothing: without the LiveQRScreens option (no
// TAPPA_LIVE_QR_KEY) there is no block, and the two routes are not the panel's (405, nothing runs).
func TestLiveQRPanel_UnconfiguredDrawsNothingAndMountsNothing(t *testing.T) {
	b, venues := liveQRPanel(t, "owner", nil)
	rec := b.do(http.MethodGet, locationsHref+"?at="+venues.screen.Venues[0].ID.String(), nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Live QR screens") {
		t.Fatalf("GET = %d; the block is drawn without the feature", rec.Code)
	}
	for _, path := range []string{liveQRCreateHref, liveQRRevokeHref} {
		if rec := b.do(http.MethodPost, path, url.Values{"location_id": {uuid.NewString()}}); rec.Code == http.StatusOK || rec.Code == http.StatusSeeOther {
			t.Errorf("POST %s = %d without the feature", path, rec.Code)
		}
	}
}

// TestLiveQRPanel_AddShowsTheCodeOnceAndBothRolesMay: owner AND manager (SEÇİM: the plaque acts'
// rule) add a screen; the answer is 200 -- never a redirect, so the code is in no address -- with
// the code in its three groups, no-store, and the command carries the session's tenant and admin and
// the posted venue. The venue page afterwards does not carry the code.
func TestLiveQRPanel_AddShowsTheCodeOnceAndBothRolesMay(t *testing.T) {
	for _, role := range []string{"owner", "manager"} {
		t.Run(role, func(t *testing.T) {
			svc := &fakeLiveQR{code: newTestCode(t)}
			b, venues := liveQRPanel(t, role, svc)
			venue := venues.screen.Venues[0].ID
			rec := b.do(http.MethodPost, liveQRCreateHref, url.Values{"location_id": {venue.String()}})
			if rec.Code != http.StatusOK {
				t.Fatalf("POST = %d, want 200", rec.Code)
			}
			shown := svc.code.ForThePanelOnce()
			body := rec.Body.String()
			if !strings.Contains(body, shown) || !strings.Contains(body, liveQRScreenURLForTest) || !strings.Contains(body, "10 minutes") {
				t.Fatal("the answer does not show the code, the address and its lifetime")
			}
			if rec.Header().Get("Location") != "" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Location %q, Cache-Control %q", rec.Header().Get("Location"), rec.Header().Get("Cache-Control"))
			}
			if len(svc.created) != 1 || svc.created[0].TenantID != panelTestTenant || svc.created[0].ActorID != panelTestAdmin ||
				svc.created[0].LocationID != venue {
				t.Fatalf("commands %+v", svc.created)
			}
			if page := b.do(http.MethodGet, locationsHref+"?at="+venue.String(), nil).Body.String(); strings.Contains(page, shown) {
				t.Fatal("the venue page carries the code")
			}
		})
	}
}

// TestLiveQRPanel_RemoveRedirectsToTheVenue: Remove revokes the posted screen as this session's admin
// and 303s to the venue it stood at; an unknown screen is a 404 page and nothing else.
func TestLiveQRPanel_RemoveRedirectsToTheVenue(t *testing.T) {
	venueOf := uuid.New()
	svc := &fakeLiveQR{revokeAt: venueOf}
	b, _ := liveQRPanel(t, "manager", svc)
	screen := uuid.New()
	rec := b.do(http.MethodPost, liveQRRevokeHref, url.Values{"display_id": {screen.String()}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != locationsHref+"?at="+venueOf.String()+"#venue" {
		t.Fatalf("POST = %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(svc.revoked) != 1 || svc.revoked[0].DisplayID != screen || svc.revoked[0].TenantID != panelTestTenant ||
		svc.revoked[0].ActorID != panelTestAdmin {
		t.Fatalf("commands %+v", svc.revoked)
	}
	svc.revokeErr = liveqr.ErrUnknownScreen
	if rec := b.do(http.MethodPost, liveQRRevokeHref, url.Values{"display_id": {uuid.NewString()}}); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown screen = %d, want 404", rec.Code)
	}
	if rec := b.do(http.MethodPost, liveQRRevokeHref, url.Values{"display_id": {"not-a-uuid"}}); rec.Code != http.StatusNotFound || len(svc.revoked) != 2 {
		t.Fatalf("an unreadable id = %d (%d calls)", rec.Code, len(svc.revoked))
	}
}

// TestLiveQRPanel_AddingIsBudgetedPerBusiness: the 31st screen in an hour is 429 and reaches no
// service call.
func TestLiveQRPanel_AddingIsBudgetedPerBusiness(t *testing.T) {
	svc := &fakeLiveQR{code: newTestCode(t)}
	b, venues := liveQRPanel(t, "owner", svc)
	form := url.Values{"location_id": {venues.screen.Venues[0].ID.String()}}
	for i := range liveQRCreateLimit {
		if rec := b.do(http.MethodPost, liveQRCreateHref, form); rec.Code != http.StatusOK {
			t.Fatalf("add %d = %d", i+1, rec.Code)
		}
	}
	if rec := b.do(http.MethodPost, liveQRCreateHref, form); rec.Code != http.StatusTooManyRequests || len(svc.created) != liveQRCreateLimit {
		t.Fatalf("over the budget: %d, %d calls", rec.Code, len(svc.created))
	}
}

// --- the screen page, with a fake service -------------------------------------------------------

type fakeScreenService struct {
	mu    sync.Mutex
	pairs int
}

func (f *fakeScreenService) Pair(context.Context, liveqr.PairingCode) (liveqr.Screen, liveqr.ScreenSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pairs++
	return liveqr.Screen{}, liveqr.ScreenSession{}, liveqr.ErrCodeRefused
}
func (f *fakeScreenService) Resolve(context.Context, liveqr.ScreenSession) (liveqr.Screen, error) {
	return liveqr.Screen{}, liveqr.ErrNoScreen
}
func (f *fakeScreenService) Token(liveqr.Screen) (string, time.Time, error) {
	return "", time.Time{}, errors.New("unused")
}

func screenRouter(t *testing.T, svc liveScreenService, log *slog.Logger) http.Handler {
	t.Helper()
	s, err := NewLiveScreen(svc, "https://taptime.mt", true, log)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	s.Mount(r)
	return r
}

func screenDo(h http.Handler, method, path string, form url.Values, mutate func(*http.Request)) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.RemoteAddr = "198.51.100.7:4000"
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestLiveScreen_TheFormPostsToThePairRoute: the pairing form's literal action is the route Mount
// registers, the cookie's path is the page's root, and the form page carries the tap screen's policy
// (no script on it at all), no-store and no code echo.
func TestLiveScreen_TheFormPostsToThePairRoute(t *testing.T) {
	if LiveScreenPairPath != "/screen/pair" || LiveScreenPath != liveqr.ScreenCookiePath || LiveScreenCodePath != "/screen/code" {
		t.Fatalf("paths %q %q %q", LiveScreenPath, LiveScreenPairPath, LiveScreenCodePath)
	}
	rec := screenDo(screenRouter(t, &fakeScreenService{}, discardLogger()), http.MethodGet, LiveScreenPath, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/screen/pair"`) || strings.Contains(body, "<script") {
		t.Fatal("the form does not post to /screen/pair, or the page loads a script")
	}
	if rec.Header().Get("Content-Security-Policy") != tapCSP || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers %v", rec.Header())
	}
}

// TestLiveScreen_ThePolicyIsTheTapScreensPlusThePoll: the QR page's policy is the tap screen's with
// connect-src 'self' and nothing more -- no inline anything, no img-src (the code is inline SVG), no
// other origin.
func TestLiveScreen_ThePolicyIsTheTapScreensPlusThePoll(t *testing.T) {
	if liveScreenCSP != tapCSP+"; connect-src 'self'" {
		t.Fatalf("policy %q", liveScreenCSP)
	}
	for _, banned := range []string{"unsafe", "img-src", "http", "*", "data:"} {
		if strings.Contains(liveScreenCSP, banned) {
			t.Errorf("the policy names %q", banned)
		}
	}
	var buf bytes.Buffer
	if err := pages.LiveScreen(pages.LiveScreenView{VenueName: "Rusty Bar", CodeURL: LiveScreenCodePath}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	if strings.Count(page, "<script") != 1 || !strings.Contains(page, `src="/static/js/livescreen.js"`) ||
		strings.Contains(page, "style=") || strings.Contains(page, "<style") || strings.Contains(page, "<img") {
		t.Fatal("the QR page is not one external script, no inline style and no image")
	}
	if !strings.Contains(page, `data-code-url="/screen/code"`) || !strings.Contains(page, `d=""`) {
		t.Fatal("the page does not start with an empty drawing and the poll's address")
	}
}

// TestLiveScreen_CrossSitePairingIsRefused: a pairing posted from another site never reaches the
// service.
func TestLiveScreen_CrossSitePairingIsRefused(t *testing.T) {
	svc := &fakeScreenService{}
	h := screenRouter(t, svc, discardLogger())
	for _, site := range []string{"cross-site", "same-site"} {
		rec := screenDo(h, http.MethodPost, LiveScreenPairPath, url.Values{"code": {"7KQ2-M9XD-4TRWZ"}}, func(r *http.Request) {
			r.Header.Set("Sec-Fetch-Site", site)
		})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s = %d", site, rec.Code)
		}
	}
	if svc.pairs != 0 {
		t.Fatalf("%d pairing(s) reached the service", svc.pairs)
	}
}

// TestLiveScreen_AForeignOriginPairingIsRefused (DÜŞÜK 5): without Sec-Fetch-Site, an Origin that is
// not TAPPA_BASE_URL's -- "null" included -- is refused before the service is asked; the base URL's
// own origin, and no Origin at all, pass.
func TestLiveScreen_AForeignOriginPairingIsRefused(t *testing.T) {
	svc := &fakeScreenService{}
	h := screenRouter(t, svc, discardLogger())
	form := url.Values{"code": {"7KQ2-M9XD-4TRWZ"}}
	for _, origin := range []string{"https://evil.example", "null", "http://taptime.mt", "https://taptime.mt.evil.example"} {
		rec := screenDo(h, http.MethodPost, LiveScreenPairPath, form, func(r *http.Request) { r.Header.Set("Origin", origin) })
		if rec.Code != http.StatusForbidden {
			t.Fatalf("Origin %q = %d, want 403", origin, rec.Code)
		}
	}
	if svc.pairs != 0 {
		t.Fatalf("%d pairing(s) reached the service", svc.pairs)
	}
	for _, origin := range []string{"https://taptime.mt", ""} {
		rec := screenDo(h, http.MethodPost, LiveScreenPairPath, form, func(r *http.Request) {
			if origin != "" {
				r.Header.Set("Origin", origin)
			}
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("Origin %q = %d, want the service's refusal (400)", origin, rec.Code)
		}
	}
	if svc.pairs != 2 {
		t.Fatalf("%d pairing(s) reached the service, want 2", svc.pairs)
	}
}

// TestLiveScreen_PairingFailuresAreBudgeted: 20 refused codes from one address, then 429 before the
// service is asked -- and an unreadable code counts as a failure too. The refusal page is one
// sentence for every reason and never echoes what was typed.
func TestLiveScreen_PairingFailuresAreBudgeted(t *testing.T) {
	svc := &fakeScreenService{}
	h := screenRouter(t, svc, discardLogger())
	typed := "7KQ2-M9XD-4TRWZ"
	for i := range liveScreenPairFailureLimit {
		form := url.Values{"code": {typed}}
		if i%2 == 1 {
			form = url.Values{"code": {"not a code"}}
		}
		rec := screenDo(h, http.MethodPost, LiveScreenPairPath, form, nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "That code did not work") ||
			strings.Contains(rec.Body.String(), typed) || strings.Contains(rec.Body.String(), "not a code") {
			t.Fatalf("attempt %d = %d", i+1, rec.Code)
		}
	}
	rec := screenDo(h, http.MethodPost, LiveScreenPairPath, url.Values{"code": {typed}}, nil)
	if rec.Code != http.StatusTooManyRequests || svc.pairs != liveScreenPairFailureLimit/2 {
		t.Fatalf("over the budget: %d, %d service calls", rec.Code, svc.pairs)
	}
	other := screenDo(h, http.MethodPost, LiveScreenPairPath, url.Values{"code": {typed}}, func(r *http.Request) { r.RemoteAddr = "198.51.100.8:4000" })
	if other.Code != http.StatusBadRequest {
		t.Fatalf("another address = %d: the budget is per address", other.Code)
	}
}

// TestLiveScreen_NoSessionNoCode: the poll without a paired screen's cookie is 401 "unpaired" -- no
// drawing -- and a cookie that names nothing is cleared.
func TestLiveScreen_NoSessionNoCode(t *testing.T) {
	h := screenRouter(t, &fakeScreenService{}, discardLogger())
	if rec := screenDo(h, http.MethodGet, LiveScreenCodePath, nil, nil); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), `"unpaired"`) {
		t.Fatalf("no cookie = %d %s", rec.Code, rec.Body.String())
	}
	rec := screenDo(h, http.MethodGet, LiveScreenCodePath, nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: liveqr.SessionCookieName, Value: strings.Repeat("A", 43)})
	})
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("an unknown cookie = %d, Set-Cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
}

// TestLiveScreenScript_KeepsItsPromises reads web/static/js/livescreen.js (activate.js's scanner):
// it touches no location, storage, cookie or other origin, writes no markup (only attributes on the
// server's drawing and `hidden` flips), checks the path's shape before drawing it, and HIDES the code
// once its time has passed -- the card's "an offline screen does not keep showing the old code".
func TestLiveScreenScript_KeepsItsPromises(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "js", "livescreen.js"))
	if err != nil {
		t.Fatalf("read livescreen.js: %v", err)
	}
	code := stripJSComments(string(src))
	for _, banned := range []string{"geolocation", "localStorage", "sessionStorage", "document.cookie",
		"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "http://", "https://",
		"textContent", "createElement", "Date.parse"} {
		if strings.Contains(code, banned) {
			t.Errorf("livescreen.js uses %q", banned)
		}
	}
	for _, want := range []string{"credentials: 'same-origin'", "cache: 'no-store'", "PATH_SHAPE.test(body.d)",
		"now() >= deadline", "hideCode()", "box.hidden = true", "path.setAttribute('d', '')", "performance.now",
		// DÜŞÜK 9: the display is counted from the request's SEND, so a slow answer shortens it.
		"var sentAt = now();", "deadline = sentAt + body.expiresIn * 1000",
		// DÜŞÜK 8: a paused screen keeps asking, every minute.
		"res.status === 409", "schedule(PAUSED_MS)", "PAUSED_MS = 60000"} {
		if !strings.Contains(code, want) {
			t.Errorf("livescreen.js lost %q", want)
		}
	}
}
