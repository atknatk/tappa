package handler

import (
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// gatedPanel is the panel as production mounts it plus one identity surface mounted the way M14-10
// and its siblings will -- a.mountGated(r, <surface>, ...) -- with a GET (Protect) and a POST
// (ProtectWriting) route, under the flag value given.
func gatedPanel(t *testing.T, flag bool, admins *fakeAdmins) http.Handler {
	t.Helper()
	cfg := adminTestConfig()
	cfg.AdminIdentity = flag
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(),
		newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	reached := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h.mountGated(r, adminauth.SurfaceSwitch, func(r chi.Router) {
		r.With(h.Protect()).Get("/admin/zz-gated", reached)
		r.With(h.ProtectWriting()).Post("/admin/zz-gated", reached)
	})
	return r
}

// liveAdmins is a fakeAdmins whose cookie resolves to a live owner.
func liveAdmins() *fakeAdmins {
	tenant, admin, session := uuid.New(), uuid.New(), uuid.New()
	return &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: session, TenantID: tenant, AdminUserID: admin, Role: "owner", FullName: "Gate Owner"}, nil
	}}
}

// gateMethods are every method a client can send the panel.
var gateMethods = []string{http.MethodGet, http.MethodPost, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodPatch}

// TestMountGated_AClosedSurfaceIsAPathThePanelNeverHad (ADR 0031 §8, R4; M14-08's audit): while a
// surface is closed its paths answer, for EVERY method -- GET, POST, HEAD, PUT, DELETE, OPTIONS,
// PATCH -- signed in and signed out, same-origin and cross-origin, exactly what a path under /admin
// the panel never had answers: status, every header, body. Nothing of a chain runs (no session is
// resolved). Open, the same paths reach their chains.
//
// MUTATION (measured): the routes bound with r.Get / r.Post while closed and refused at request time
// (the first version's gated()) -> HEAD, PUT, DELETE, OPTIONS and PATCH answer 405 with an Allow
// header, red; mountGated mounting whatever the gate says -> the GET is the chain's 303, red.
func TestMountGated_AClosedSurfaceIsAPathThePanelNeverHad(t *testing.T) {
	for _, method := range gateMethods {
		for _, who := range []struct {
			name     string
			signedIn bool
			origin   string
		}{
			{"signed out", false, testBaseURL},
			{"signed in", true, testBaseURL},
			{"signed in, cross-origin", true, "https://evil.example"},
		} {
			t.Run(method+"/"+who.name, func(t *testing.T) {
				admins := liveAdmins()
				router := gatedPanel(t, false, admins)
				answer := func(path string) (int, http.Header, string) {
					var body *strings.Reader
					if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
						body = strings.NewReader("x=1")
					} else {
						body = strings.NewReader("")
					}
					req := httptest.NewRequest(method, path, body)
					req.RemoteAddr = "203.0.113.9:1234"
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					req.Header.Set("Origin", who.origin)
					if who.signedIn {
						req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
					}
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					return rec.Code, rec.Header(), rec.Body.String()
				}
				gs, gh, gb := answer("/admin/zz-gated")
				ns, nh, nb := answer("/admin/zz-never-mounted")
				if gs != http.StatusNotFound || gs != ns || gb != nb || !maps.EqualFunc(gh, nh, slices.Equal[[]string]) {
					t.Fatalf("closed surface: %d %v %q\nnever-had path: %d %v %q", gs, gh, gb, ns, nh, nb)
				}
				if n := admins.verifiedCount(); n != 0 {
					t.Errorf("a closed surface resolved the session %d time(s): a chain ran", n)
				}
			})
		}
	}

	t.Run("open, the chains run", func(t *testing.T) {
		router := gatedPanel(t, true, liveAdmins())
		out := newBrowser(t, router).do(http.MethodGet, "/admin/zz-gated", nil)
		if out.Code != http.StatusSeeOther {
			t.Errorf("signed out, open: %d, want the chain's 303 to the sign-in", out.Code)
		}
		b := newBrowser(t, router)
		b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		if in := b.do(http.MethodGet, "/admin/zz-gated", nil); in.Code != http.StatusTeapot {
			t.Errorf("signed in, open: %d, want the handler's 418", in.Code)
		}
		if in := b.do(http.MethodPost, "/admin/zz-gated", nil); in.Code != http.StatusTeapot {
			t.Errorf("signed in POST, open: %d, want the handler's 418", in.Code)
		}
	})
}
