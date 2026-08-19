package httpx_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// health_test.go — LIVENESS (/healthz), the half of M8-01's health criterion that
// this package owns. Readiness (/readyz) is internal/handler.Health, because it
// needs a database and this package must never have one.

// TestHealthz_AnswersGetAndHead.
//
// 🔴 A REAL SERVER, NOT httptest.NewRecorder. The recorder does not implement
// net/http's body suppression for HEAD, so the same assertions against a recorder
// would report a body no client ever receives (M7-01 measured this; backlog T29).
//
// HEAD IS THE POINT OF THIS TEST. Before M8-01, chi's r.Get registered GET alone and
// HEAD /healthz answered 405 with `Allow: GET` — and an uptime monitor, which is
// the only kind of client this URL has, reads 405 as an outage.
func TestHealthz_AnswersGetAndHead(t *testing.T) {
	srv := httptest.NewServer(httpx.NewRouter(&config.Config{}, nil))
	t.Cleanup(srv.Close)

	get, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, _ := io.ReadAll(get.Body)
	_ = get.Body.Close()

	if get.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", get.StatusCode)
	}
	// EXACT equality rather than "contains", and that is the guard described below:
	// a body that is exactly two known bytes cannot have acquired a commit hash, a
	// hostname or a configuration value.
	if string(body) != "ok" {
		t.Errorf("GET /healthz body = %q, want %q", body, "ok")
	}
	if got := get.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store — a cached ok answers for a process that has died", got)
	}
	// ⚠️ ASSERTED, NOT COMPARED. This used to be checked only through the GET/HEAD
	// header comparison below, and an audit deleted the header from both health
	// endpoints with the suite staying green: two empty strings compare equal.
	if got := get.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, srv.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("building HEAD: %v", err)
	}
	head, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("HEAD /healthz: %v", err)
	}
	hb, _ := io.ReadAll(head.Body)
	_ = head.Body.Close()

	if head.StatusCode != http.StatusOK {
		t.Errorf("HEAD /healthz = %d, want 200 — this is what an uptime monitor sends first", head.StatusCode)
	}
	if len(hb) != 0 {
		t.Errorf("HEAD /healthz returned %d body bytes; a HEAD response carries none", len(hb))
	}
	for _, h := range []string{"Content-Type", "Cache-Control", "X-Content-Type-Options"} {
		if g, hd := get.Header.Get(h), head.Header.Get(h); g != hd {
			t.Errorf("%s: GET sends %q, HEAD sends %q", h, g, hd)
		}
	}
}

// TestHealthz_CannotDependOnAnything — the structural half of "liveness".
//
// A liveness probe that consults a dependency reports the dependency, and a
// deployment then restarts a perfectly healthy process because its database is
// unreachable. Here that is not a promise in a comment: NewRouter is handed a
// *config.Config, a *slog.Logger and a list of Mounters and NOTHING else, so a
// handler registered by this package has no pool, no session codec and no audit
// sink to reach.
//
// 🔴 THE LOGGER WAS ADDED BY M8-03, AND THE FIRST ROUND'S JUSTIFICATION FOR IT WAS
// FALSIFIED BY MEASUREMENT. That round wrote: "a *slog.Logger cannot query, cannot
// resolve an identity and cannot write a row. It carries no dependency INTO the
// liveness path — AccessLog writes AFTER the response, so /healthz still answers
// with every dependency down." The last clause was wrong, and wrong in exactly the
// way this test is named for. Probed:
//
//   - slog.Handler whose Handle sleeps 2 s -> GET /healthz answered 200 after
//     2.001 s. "After the response" is not "off the request path": the deferred
//     write runs BEFORE the handler goroutine returns, so the client waits for it.
//     In production os.Stdout is a container log pipe, and a node that cannot drain
//     it makes that write block — livenessProbe timeoutSeconds is 2.
//   - slog.Handler whose Handle panics -> GET /healthz answered EOF. The connection
//     died, because AccessLog is mounted outside middleware.Recoverer.
//
// So the widening HAD introduced a dependency: not a queryable one, a
// SCHEDULING one. Both are closed now and both are pinned behaviourally, next door
// in requestlog_test.go (TestHealthz_AnswersWhileTheLogTargetIsUnavailable,
// TestHealthz_AnswersWhileTheLogTargetPanics), because that is where the mechanism
// lives — a healthy /healthz writes no access record at all.
//
// ⚠️ WHAT THIS TEST STILL DOES AND DOES NOT DO, PLAINLY. It is a TYPE assertion:
// anything that can be dialled, pooled or authenticated is refused by the check
// below, and a *slog.Logger passes that check correctly. What it CANNOT see is what
// a permitted type is used FOR — which is precisely how the two failures above got
// in under a green suite. A signature test and a behaviour test catch different
// halves; this one is not the whole guarantee and no longer claims to be.
//
// The signature is the assertion. If somebody adds a database parameter to
// NewRouter, this test says why that is a different decision than it looks.
func TestHealthz_CannotDependOnAnything(t *testing.T) {
	fn := reflect.TypeOf(httpx.NewRouter)
	if fn.NumIn() != 3 || !fn.IsVariadic() {
		t.Fatalf("NewRouter takes %d parameters (variadic=%v); liveness rests on it taking a config, a logger and mounters only",
			fn.NumIn(), fn.IsVariadic())
	}
	if got := fn.In(0).String(); got != "*config.Config" {
		t.Errorf("NewRouter's first parameter is %s, want *config.Config", got)
	}
	if got := fn.In(1).String(); got != "*slog.Logger" {
		t.Errorf("NewRouter's second parameter is %s, want *slog.Logger", got)
	}
	if got := fn.In(2).String(); got != "[]httpx.Mounter" {
		t.Errorf("NewRouter's third parameter is %s, want []httpx.Mounter", got)
	}
	// And the liveness route is served by a router built with NO mounters at all,
	// which is the behavioural half of the same statement.
	srv := httptest.NewServer(httpx.NewRouter(nil, nil))
	t.Cleanup(srv.Close)
	res, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz on a featureless router: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("a router with no features answered %d on /healthz; liveness must not need a feature", res.StatusCode)
	}
}
