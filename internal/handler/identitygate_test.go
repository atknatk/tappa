package handler

import (
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// gatedRouter is the panel as production mounts it plus one identity route mounted the way M14-10
// and its siblings will -- a.gated(<surface>, <the whole chain>) -- for GET (Protect) and POST
// (ProtectWriting), under the flag value given.
func gatedRouter(t *testing.T, flag bool, admins *fakeAdmins) http.Handler {
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
	reached := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	r.Method(http.MethodGet, "/admin/zz-gated", h.gated(adminauth.SurfaceSwitch, h.Protect()(reached)))
	r.Method(http.MethodPost, "/admin/zz-gated", h.gated(adminauth.SurfaceSwitch, h.ProtectWriting()(reached)))
	return r
}

// liveAdmins is a fakeAdmins whose cookie resolves to a live owner.
func liveAdmins() *fakeAdmins {
	tenant, admin, session := uuid.New(), uuid.New(), uuid.New()
	return &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: session, TenantID: tenant, AdminUserID: admin, Role: "owner", FullName: "Gate Owner"}, nil
	}}
}

// TestGated_AClosedRouteIsAPathThePanelNeverHad (ADR 0031 §8, R4): while a surface is closed its
// route answers what an unmounted path under /admin answers -- status, every header, body -- for a
// signed-out visitor, a signed-in session, a cross-origin POST and a same-origin POST; and nothing of
// the chain runs (no session is resolved). With the flag on the same route reaches its chain.
//
// MUTATION (measured): gated returning h whatever the gate says -> the signed-out GET is a 303 to
// the login page, the signed-in one a 418 -- red.
func TestGated_AClosedRouteIsAPathThePanelNeverHad(t *testing.T) {
	type req struct {
		name, method string
		signedIn     bool
		origin       string
	}
	reqs := []req{
		{"a signed-out GET", http.MethodGet, false, testBaseURL},
		{"a signed-in GET", http.MethodGet, true, testBaseURL},
		{"a cross-origin POST", http.MethodPost, true, "https://evil.example"},
		{"a same-origin POST", http.MethodPost, true, testBaseURL},
	}
	for _, rq := range reqs {
		t.Run(rq.name, func(t *testing.T) {
			admins := liveAdmins()
			router := gatedRouter(t, false, admins)
			answer := func(path string) (int, http.Header, string) {
				b := newBrowser(t, router)
				b.origin = rq.origin
				if rq.signedIn {
					b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
				}
				var form url.Values
				if rq.method == http.MethodPost {
					form = url.Values{"x": {"1"}}
				}
				rec := b.do(rq.method, path, form)
				return rec.Code, rec.Header(), rec.Body.String()
			}
			gs, gh, gb := answer("/admin/zz-gated")
			ns, nh, nb := answer("/admin/zz-never-mounted")
			if gs != http.StatusNotFound || gs != ns || gb != nb || !maps.EqualFunc(gh, nh, slices.Equal[[]string]) {
				t.Fatalf("closed route: %d %v %q\nunmounted path: %d %v %q", gs, gh, gb, ns, nh, nb)
			}
			if n := admins.verifiedCount(); n != 0 {
				t.Errorf("a closed route resolved the session %d time(s): its chain ran", n)
			}
		})
	}

	t.Run("open, the chain runs", func(t *testing.T) {
		router := gatedRouter(t, true, liveAdmins())
		out := newBrowser(t, router).do(http.MethodGet, "/admin/zz-gated", nil)
		if out.Code != http.StatusSeeOther {
			t.Errorf("signed out, open: %d, want the chain's 303 to the sign-in", out.Code)
		}
		b := newBrowser(t, router)
		b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		if in := b.do(http.MethodGet, "/admin/zz-gated", nil); in.Code != http.StatusTeapot {
			t.Errorf("signed in, open: %d, want the handler's 418", in.Code)
		}
	})
}
