package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestOAuthRoutes_AnswerOnlyOnTheMainHost (ADR 0047 §3, §17): /oauth/authorize, /oauth/token and
// /oauth/revoke are the main host's -- on the API host (apihost.go: everything but /v1 is 404) and on
// the operator host (operatorhost.go: everything but /operator and the static files is 404) each is the
// router's 404; and /v1 stays 404 on the main host (the regression half). On the main host the three
// answer as their own surface does.
func TestOAuthRoutes_AnswerOnlyOnTheMainHost(t *testing.T) {
	cfg := walkConfig(t, true)
	f := buildAppFeatures(t, cfg, true)
	h := f.router(cfg)
	main := "taptime.mt"
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/oauth/authorize?client_id=x"},
		{http.MethodPost, "/oauth/token"},
		{http.MethodPost, "/oauth/revoke"},
	} {
		for _, host := range []string{cfg.APIHost, cfg.OperatorHost} {
			if got := answerOf(h, c.method, host, c.path); got.status != http.StatusNotFound {
				t.Errorf("%s %s on %s: %d, want the router's 404", c.method, c.path, host, got.status)
			}
		}
		got := answerOf(h, c.method, main, c.path)
		if got.status == http.StatusNotFound {
			t.Errorf("%s %s on the main host: 404", c.method, c.path)
		}
	}
	if got := answerOf(h, http.MethodGet, main, "/oauth/authorize?client_id=x"); got.status != http.StatusSeeOther ||
		!strings.HasPrefix(got.header.Get("Location"), "/admin/oauth/authorize?") {
		t.Errorf("the front door on the main host: %d %q", got.status, got.header.Get("Location"))
	}
	if got := answerOf(h, http.MethodPost, main, "/oauth/token"); got.status != http.StatusBadRequest || !strings.Contains(got.body, `"invalid_request"`) {
		t.Errorf("a bare token request on the main host: %d %q", got.status, got.body)
	}
	if got := answerOf(h, http.MethodGet, main, "/v1/employees"); got.status != http.StatusNotFound {
		t.Errorf("/v1 on the main host: %d", got.status)
	}
}
