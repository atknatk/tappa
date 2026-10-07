package handler

// The DEV-ONLY plaque-tap simulator (ADR 0025, "Geliştirme aracı") against fakes:
// the gate, and that no product screen carries the control outside development.
// The real activation through it is in devtap_db_test.go.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

type fakeMinter struct{ calls int }

func (f *fakeMinter) MintNextTapForDevelopment(_ context.Context, uid string) (string, error) {
	f.calls++
	return "/t?tag=" + uid + "&ctr=000001&cmac=0000000000000000", nil
}

type fakePlaqueList struct{ plaques []tenant.Plaque }

func (f fakePlaqueList) Screen(context.Context, uuid.UUID) (tenant.PlaqueScreen, error) {
	return tenant.PlaqueScreen{Plaques: f.plaques}, nil
}

func devCfg(env, base string) *config.Config {
	return &config.Config{Env: env, BaseURL: base, RetentionYears: 2, DevTools: env == config.EnvDev,
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"), InviteHMACKey: []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII")}
}

// TestDevToolsEnabled_IsDevOnALoopbackAddressOnly: TAPPA_ENV=dev alone is not
// enough, because an unset TAPPA_ENV falls back to dev (internal/config).
func TestDevToolsEnabled_IsDevOnALoopbackAddressOnly(t *testing.T) {
	for _, tc := range []struct {
		env, base string
		want      bool
	}{
		{config.EnvDev, "http://localhost:8080", true},
		{config.EnvDev, "http://127.0.0.1:8080", true},
		{config.EnvDev, "http://[::1]:8080", true},
		{config.EnvDev, "https://taptime.mt", false},
		{config.EnvDev, "http://192.168.1.20:8080", false},
		{config.EnvDev, "", false},
		{config.EnvStaging, "http://localhost:8080", false},
		{config.EnvProd, "http://localhost:8080", false},
		{"", "http://localhost:8080", false},
	} {
		if got := DevToolsEnabled(devCfg(tc.env, tc.base)); got != tc.want {
			t.Errorf("env=%q base=%q: enabled=%v, want %v", tc.env, tc.base, got, tc.want)
		}
	}
	if DevToolsEnabled(nil) {
		t.Error("a nil config enabled the dev tools")
	}
	// THE OPT-IN IS REQUIRED: dev on localhost WITHOUT TAPPA_DEV_TOOLS=1 is off.
	noFlag := devCfg(config.EnvDev, "http://localhost:8080")
	noFlag.DevTools = false
	if DevToolsEnabled(noFlag) {
		t.Error("dev on localhost without TAPPA_DEV_TOOLS enabled the dev tools")
	}
}

func newDevTapRouter(t *testing.T, cfg *config.Config, plaques []tenant.Plaque, minter *fakeMinter) http.Handler {
	t.Helper()
	sess := &fakeSessions{}
	sess.tok = sess.token(t)
	d, err := NewDevTap(minter, fakePlaqueList{plaques}, &fakeInvites{}, sess, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewDevTap: %v", err)
	}
	r := chi.NewRouter()
	d.Mount(r)
	return r
}

func postDevTap(h http.Handler, origin string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, components.DevSimulateTapPath, strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestDevTap_DoesNotExistOutsideDevelopment: gate 2 (no route) and gate 3 (the
// handler refuses) on every non-dev shape.
func TestDevTap_DoesNotExistOutsideDevelopment(t *testing.T) {
	wall := uuid.New()
	plaques := []tenant.Plaque{{UID: testPlaqueUID, Status: "active", LocationID: &wall}}
	for _, cfg := range []*config.Config{
		devCfg(config.EnvProd, "https://taptime.mt"),
		devCfg(config.EnvStaging, "https://staging.taptime.mt"),
		devCfg(config.EnvDev, "https://taptime.mt"), // TAPPA_ENV unset in a real deployment
		func() *config.Config {
			c := devCfg(config.EnvDev, "http://localhost:8080")
			c.DevTools = false
			return c
		}(),
	} {
		m := &fakeMinter{}
		h := newDevTapRouter(t, cfg, plaques, m)
		if w := postDevTap(h, cfg.BaseURL, pendingCookie()); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Errorf("env=%s base=%s: POST answered %d, want no route", cfg.Env, cfg.BaseURL, w.Code)
		}
		// Gate 3 in isolation: the handler itself refuses.
		d, _ := NewDevTap(m, fakePlaqueList{plaques}, &fakeInvites{}, &fakeSessions{}, cfg, nil)
		w := httptest.NewRecorder()
		d.Simulate(w, httptest.NewRequest(http.MethodPost, components.DevSimulateTapPath, nil))
		if w.Code != http.StatusNotFound || m.calls != 0 {
			t.Errorf("env=%s: Simulate answered %d and minted %d times", cfg.Env, w.Code, m.calls)
		}
	}
}

// TestDevTap_InDevRedirectsToAMintedTapForThePendingActivation: the control picks
// the invited employee's own wall, mints, and 303s to /t — and refuses a
// cross-origin POST.
func TestDevTap_InDevRedirectsToAMintedTapForThePendingActivation(t *testing.T) {
	other, mine := uuid.New(), uuid.New()
	inv := okContext("invited")
	inv.LocationID = mine
	plaques := []tenant.Plaque{
		{UID: "04AAAAAAAAAA01", Status: "active", LocationID: &other},
		{UID: "04AAAAAAAAAA02", Status: "retired", LocationID: &mine},
		{UID: "04AAAAAAAAAA03", Status: "active", LocationID: &mine},
	}
	m := &fakeMinter{}
	sess := &fakeSessions{}
	sess.tok = sess.token(t)
	d, err := NewDevTap(m, fakePlaqueList{plaques}, &fakeInvites{lookup: func(invite.Code) (invite.Context, error) { return inv, nil }},
		sess, devCfg(config.EnvDev, "http://localhost:8080"), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	d.Mount(r)

	if w := postDevTap(r, "https://evil.example", pendingCookie()); w.Code != http.StatusForbidden || m.calls != 0 {
		t.Fatalf("a cross-origin POST answered %d and minted %d", w.Code, m.calls)
	}
	if w := postDevTap(r, "", pendingCookie()); w.Code != http.StatusForbidden {
		t.Fatalf("a POST with no origin and no fetch metadata answered %d", w.Code)
	}
	for _, o := range []string{"http://127.0.0.1:8080", "http://[::1]:8080"} {
		if w := postDevTap(r, o, pendingCookie()); w.Code != http.StatusSeeOther {
			t.Errorf("a loopback-equivalent origin %s answered %d, want 303", o, w.Code)
		}
	}
	for _, o := range []string{"http://127.0.0.1:9999", "https://localhost:8080", "http://192.168.1.5:8080"} {
		if w := postDevTap(r, o, pendingCookie()); w.Code != http.StatusForbidden {
			t.Errorf("origin %s answered %d, want 403 (another port, scheme or host)", o, w.Code)
		}
	}
	w := postDevTap(r, "http://localhost:8080", pendingCookie())
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/t?tag=04AAAAAAAAAA03&") {
		t.Fatalf("status %d Location %q: want a 303 to the invited employee's own active plaque", w.Code, w.Header().Get("Location"))
	}
	if w := postDevTap(r, "http://localhost:8080"); w.Code != http.StatusConflict {
		t.Errorf("nobody to tap for: status %d, want 409", w.Code)
	}
}

// TestPickPlaque covers the choice: asked-for, preferred wall, first usable, none.
func TestPickPlaque(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	ps := []tenant.Plaque{
		{UID: "U1", Status: "unassigned"},
		{UID: "U2", Status: "active", LocationID: &a},
		{UID: "U3", Status: "lost", LocationID: &b},
		{UID: "U4", Status: "active", LocationID: &b},
	}
	for _, tc := range []struct {
		pref  uuid.UUID
		asked string
		want  string
	}{
		{b, "", "U4"}, {uuid.Nil, "", "U2"}, {uuid.New(), "", "U2"},
		{a, "U4", "U4"}, {a, "U3", ""}, {a, "NOPE", ""},
	} {
		if got := pickPlaque(ps, tc.pref, tc.asked); got != tc.want {
			t.Errorf("pref=%v asked=%q: %q, want %q", tc.pref, tc.asked, got, tc.want)
		}
	}
	if pickPlaque(nil, a, "") != "" {
		t.Error("no plaques picked something")
	}
}

// TestDevStrip_OnlyOnADevelopmentDeployment: outside dev NO screen carries the
// control — the wizard steps, the waiting screen, /activate/complete, the
// already-set-up page; in dev the waiting screen opens it in a new tab. The tap
// and result pages are covered through their templates below.
func TestDevStrip_OnlyOnADevelopmentDeployment(t *testing.T) {
	sid := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	sc := &http.Cookie{Name: "tappa_session", Value: "FAKEsessionFAKEsessionFAKEsessionFAKEsess12"}
	marker := &http.Cookie{Name: activatedCookieName, Value: sid.String()}
	build := func(cfg *config.Config) http.Handler {
		sess := &fakeSessions{sessionID: sid}
		sess.tok = sess.token(t)
		a, err := NewActivation(&fakeInvites{}, sess, &fakeVerifier{}, &fakeAudit{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		// What cmd/tappa does on every deployment: ask. Outside dev it is a no-op.
		a.EnableDevTools(cfg)
		r := chi.NewRouter()
		a.Mount(r)
		return r
	}
	type page struct {
		target  string
		cookies []*http.Cookie
	}
	pagesToCheck := []page{
		{"/activate?step=1", []*http.Cookie{codeCookie()}},
		{"/activate?step=2", []*http.Cookie{codeCookie()}},
		{"/activate?step=3", []*http.Cookie{pendingCookie()}},
		{"/activate?step=4", []*http.Cookie{pendingCookie()}},
		{ActivationCompletePath, []*http.Cookie{sc, marker}},
		{"/activate", []*http.Cookie{sc}}, // already set up
	}
	for _, cfg := range []*config.Config{devCfg(config.EnvProd, "https://taptime.mt"), devCfg(config.EnvStaging, "https://s.taptime.mt"), devCfg(config.EnvDev, "https://taptime.mt")} {
		h := build(cfg)
		for _, p := range pagesToCheck {
			if body := get(t, h, p.target, p.cookies...).Body.String(); strings.Contains(body, components.DevSimulateTapPath) || strings.Contains(body, "DEV ONLY") {
				t.Errorf("env=%s base=%s: %s carries the dev control", cfg.Env, cfg.BaseURL, p.target)
			}
		}
	}

	h := build(devCfg(config.EnvDev, "http://localhost:8080"))
	wait := get(t, h, "/activate?step=4", pendingCookie()).Body.String()
	if !strings.Contains(wait, `action="/dev/simulate-tap"`) || !strings.Contains(wait, `target="_blank"`) {
		t.Error("in dev the waiting screen must offer the simulator, opening a new tab")
	}
	if done := get(t, h, ActivationCompletePath, sc, marker).Body.String(); !strings.Contains(done, `action="/dev/simulate-tap"`) {
		t.Error("in dev /activate/complete must offer the simulator for the next check-in")
	}
	if step2 := get(t, h, "/activate?step=2", codeCookie()).Body.String(); strings.Contains(step2, "DEV ONLY") {
		t.Error("the simulator belongs on the waiting screen, not on the consent step")
	}

	// The tap page never carries it; the result page carries it only with the dev
	// render marker, which only DevToolsEnabled sets.
	render := func(c interface {
		Render(context.Context, io.Writer) error
	}, ctx context.Context) string {
		var sb strings.Builder
		if err := c.Render(ctx, &sb); err != nil {
			t.Fatal(err)
		}
		return sb.String()
	}
	result := pages.Result(pages.ResultView{Verdict: "ok", Venue: "St Julians", At: "09:00:00", Trust: 100}, layout.Logo{})
	if strings.Contains(render(result, context.Background()), "DEV ONLY") {
		t.Error("the result page carries the dev control without the dev marker")
	}
	if !strings.Contains(render(result, components.WithDevTools(context.Background())), "DEV ONLY") {
		t.Error("the result page lacks the dev control in dev")
	}
	tapPage := pages.Tap(pages.TapView{EmployeeName: "Maria Borg", TapContext: "x.y"}, layout.Brand{})
	if strings.Contains(render(tapPage, components.WithDevTools(context.Background())), "DEV ONLY") {
		t.Error("the tap page must never carry the dev control")
	}
}

// TestDevSimulateTapPath_MatchesTheStrip: the route and the strip's form action
// are one string.
func TestDevSimulateTapPath_MatchesTheStrip(t *testing.T) {
	if DevSimulateTapPath != components.DevSimulateTapPath {
		t.Fatalf("route %q != strip action %q", DevSimulateTapPath, components.DevSimulateTapPath)
	}
}

// TestEnableDevTools_CannotTurnItOnOutsideDev: EnableDevTools obeys the gate.
func TestEnableDevTools_CannotTurnItOnOutsideDev(t *testing.T) {
	var a Activation
	a.EnableDevTools(devCfg(config.EnvProd, "http://localhost:8080"))
	var tp Tap
	tp.EnableDevTools(devCfg(config.EnvDev, "https://taptime.mt"))
	if a.devTools || tp.devTools {
		t.Fatal("EnableDevTools switched the strip on outside dev-on-loopback")
	}
	a.EnableDevTools(devCfg(config.EnvDev, "http://localhost:8080"))
	if !a.devTools {
		t.Fatal("EnableDevTools did not switch the strip on in dev")
	}
}
