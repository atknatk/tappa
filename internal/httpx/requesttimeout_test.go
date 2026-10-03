package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// TestNewRouter_EveryRequestCarriesTheRequestTimeout: a feature mounted through
// NewRouter sees a request context whose deadline is RequestTimeout from now -- the
// constant is the router's deadline and not a number written beside it. The tap
// confirmation's brand read is sized against this constant (internal/handler,
// TestNewTap_BoundsTheResultBrandRead), so a router that drifted from it would leave
// that comparison measuring nothing (WL-9 round 3, N3).
func TestNewRouter_EveryRequestCarriesTheRequestTimeout(t *testing.T) {
	t.Parallel()
	var (
		deadline time.Time
		ok       bool
	)
	r := httpx.NewRouter(&config.Config{}, nil, mountFunc(func(r chi.Router) {
		r.Get("/probe", func(w http.ResponseWriter, r *http.Request) {
			deadline, ok = r.Context().Deadline()
			w.WriteHeader(http.StatusNoContent)
		})
	}))
	before := time.Now()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	after := time.Now()
	if w.Code != http.StatusNoContent || !ok {
		t.Fatalf("status %d, deadline set %v; want 204 and a deadline", w.Code, ok)
	}
	if deadline.Before(before.Add(httpx.RequestTimeout)) || deadline.After(after.Add(httpx.RequestTimeout)) {
		t.Fatalf("the request's deadline is %v from the request; want httpx.RequestTimeout (%v)",
			deadline.Sub(before).Round(time.Millisecond), httpx.RequestTimeout)
	}
}
