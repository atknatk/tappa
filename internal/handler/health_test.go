package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// health_test.go — the readiness endpoint (M8-01).
//
// Every assertion here is about a COST rather than about a status code, because the
// status code is the easy half: /readyz is unauthenticated and unmetered, so what
// had to be measured is what a stranger can spend by driving it — pool acquisitions,
// process log lines, and this process's own cached opinion of itself.

// fakeProbe is a readinessProbe whose outcome the test chooses, counting calls.
type fakeProbe struct {
	mu    sync.Mutex
	calls int
	err   error
	// block, when non-nil, is waited on inside Ping so a hung database can be
	// simulated without sleeping.
	block chan struct{}
	// budget is how long the LAST call was given before its context expires, and
	// hadDeadline says whether it was given one at all. They are what lets a test
	// measure the shipped probe deadline without waiting for it.
	budget      time.Duration
	hadDeadline bool
	// sawCancel records whether the context handed to Ping was already cancelled.
	sawCancel atomic.Bool
}

func (f *fakeProbe) Ping(ctx context.Context) error {
	f.mu.Lock()
	f.calls++
	block, err := f.block, f.err
	if d, ok := ctx.Deadline(); ok {
		f.hadDeadline = true
		f.budget = time.Until(d)
	} else {
		f.hadDeadline = false
	}
	f.mu.Unlock()

	// A REAL pgx ping fails on a context that is already done, so the fake does the
	// same: otherwise a probe wired to the caller's cancellation would look harmless
	// here while it flipped readiness in production.
	if ctx.Err() != nil {
		f.sawCancel.Store(true)
		return ctx.Err()
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (f *fakeProbe) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeProbe) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *fakeProbe) lastBudget() (time.Duration, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.budget, f.hadDeadline
}

// newHealth builds the endpoint with a captured log and a controllable clock.
func newHealth(t *testing.T, probe readinessProbe, logged *bytes.Buffer) *Health {
	t.Helper()
	log := discardLogger()
	if logged != nil {
		log = slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	h, err := NewHealth(probe, log)
	if err != nil {
		t.Fatalf("NewHealth: %v", err)
	}
	return h
}

func healthServer(t *testing.T, h *Health) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	h.Mount(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestHealth_HoldsACapabilityAndNotAPool is the reflection half of the argument on
// readinessProbe, and it exists because the compiler cannot catch the other half.
//
// 🔴 WHAT A SECURITY AUDIT FOUND: the probe used to be a one-method INTERFACE, and
// cmd/tappa satisfied it with *db.DB. An interface value carries the object behind
// the method set, so the pool wrapper — which also exposes the six context-less
// resolvers — was reachable from this package by a type assertion, in a package that
// already imports internal/db. The parameter is now a func, so `NewHealth(data, …)`
// does not compile at all.
//
// THAT COMPILE ERROR COVERS THE WIRING; THIS COVERS THE TYPE. Somebody adding a
// second field (`data *db.DB`) plus a second parameter compiles perfectly well, and
// is exactly how the panel's dependencies arrived. So the field set is asserted, the
// same way TestMarketing_HandlerHoldsNoStatefulDependency asserts it for the public
// surface: a new field here has to be argued for on this list.
func TestHealth_HoldsACapabilityAndNotAPool(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(Health{})
	if typ.NumField() == 0 {
		t.Fatal("handler.Health has no fields; this test would pass over anything")
	}
	allowed := map[string]bool{
		// The capability itself: one call, a context in, an error out.
		"handler.readinessProbe": true,
		"*slog.Logger":           true,
		// The two bounded windows and the injected clock.
		"time.Duration":    true,
		"func() time.Time": true,
		"sync.Mutex":       true,
		"time.Time":        true,
		"error":            true,
		"bool":             true,
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !allowed[f.Type.String()] {
			t.Errorf("handler.Health.%s is a %s.\n"+
				"This endpoint is unauthenticated and unmetered by argument, and what stands in for "+
				"the panel's protections is that it holds ONE bound method rather than an object: no "+
				"pool, no resolver, no session codec, nothing to type-assert. A dependency here has "+
				"to arrive with its own argument — see readinessProbe in health.go.", f.Name, f.Type)
		}
	}
}

// TestReadyz_AnswersTheDatabasesAnswer — the two outcomes, over a real server.
func TestReadyz_AnswersTheDatabasesAnswer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		probeErr error
		want     int
		wantBody string
	}{
		{"the database answered", nil, http.StatusOK, readyBody},
		{"the database did not answer", errors.New("closed pool"), http.StatusServiceUnavailable, notReadyBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe := &fakeProbe{err: tc.probeErr}
			h := newHealth(t, probe.Ping, nil)
			srv := healthServer(t, h)

			res, err := srv.Client().Get(srv.URL + readyPath)
			if err != nil {
				t.Fatalf("GET %s: %v", readyPath, err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)

			if res.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", res.StatusCode, tc.want)
			}
			if string(body) != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
			// A cached readiness answer is a proxy telling a deployment to send taps
			// to a process that has since lost its database.
			if got := res.Header.Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if got := res.Header.Get("Content-Type"); got != readyContentTypeText {
				t.Errorf("Content-Type = %q, want %q", got, readyContentTypeText)
			}
			// ⚠️ ASSERTED ON BOTH ANSWERS, not compared between two responses. An
			// audit deleted this header from both health endpoints and the suite
			// stayed green, because the only check on it was GET == HEAD — and two
			// empty strings are equal. It is cheap and it is the one header that
			// stops a browser from deciding for itself what these bytes are.
			if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
	}
}

// TestReadyz_AnswersHeadFromTheFirstDay.
//
// 🔴 A REAL SERVER, NOT A RECORDER. httptest.ResponseRecorder does not perform the
// body suppression net/http does for HEAD, so the same assertion against a recorder
// reports a body no real client ever receives (M7-01 measured this; backlog T29).
//
// WHY IT MATTERS HERE MORE THAN ANYWHERE: the clients that watch a readiness URL are
// uptime monitors and orchestrators, and they HEAD. Every other r.Get in this
// product answers 405 `Allow: GET` to them.
func TestReadyz_AnswersHeadFromTheFirstDay(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	h := newHealth(t, probe.Ping, nil)
	h.ttl = 0 // both requests below must reach the probe
	srv := healthServer(t, h)

	get, err := srv.Client().Get(srv.URL + readyPath)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	gb, _ := io.ReadAll(get.Body)
	_ = get.Body.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, srv.URL+readyPath, nil)
	if err != nil {
		t.Fatalf("building HEAD: %v", err)
	}
	head, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	hb, _ := io.ReadAll(head.Body)
	_ = head.Body.Close()

	if head.StatusCode != http.StatusOK {
		t.Errorf("HEAD %s = %d, want 200 — 405 is what an uptime monitor reads as an outage", readyPath, head.StatusCode)
	}
	if len(hb) != 0 {
		t.Errorf("HEAD returned %d body bytes; a HEAD response carries none", len(hb))
	}
	if len(gb) == 0 {
		t.Fatal("GET returned an empty body, so the comparison above is vacuous")
	}
	for _, hdr := range []string{"Content-Type", "Cache-Control", "X-Content-Type-Options"} {
		if g, hd := get.Header.Get(hdr), head.Header.Get(hdr); g != hd {
			t.Errorf("%s: GET sends %q, HEAD sends %q", hdr, g, hd)
		}
	}
}

// TestReadyz_ABurstCostsOneQuery — the budget.
//
// 200 concurrent unauthenticated requests must not become 200 pool acquisitions on
// the pool a check-in shares. The POSITIVE CONTROL is the second half: with the
// window set to zero the same 200 requests DO reach the probe, so the first number
// measures the cache rather than a probe that is never called.
func TestReadyz_ABurstCostsOneQuery(t *testing.T) {
	t.Parallel()
	const n = 200

	burst := func(h *Health) {
		srv := healthServer(t, h)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := srv.Client().Get(srv.URL + readyPath)
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
			}()
		}
		wg.Wait()
	}

	cached := &fakeProbe{}
	h := newHealth(t, cached.Ping, nil)
	// A frozen clock: every request inside the window sees the same instant, which
	// is what makes "one query" an exact number rather than a race with wall time.
	frozen := time.Now()
	h.now = func() time.Time { return frozen }
	burst(h)
	if got := cached.count(); got != 1 {
		t.Errorf("%d requests inside the cache window made %d database round trips, want exactly 1", n, got)
	}

	uncached := &fakeProbe{}
	h2 := newHealth(t, uncached.Ping, nil)
	h2.ttl = 0
	burst(h2)
	if got := uncached.count(); got != n {
		t.Errorf("POSITIVE CONTROL: with no cache window, %d requests made %d round trips, want %d — "+
			"the count above proves nothing if the probe is simply never reached", n, got, n)
	}
}

// shortestProbeInterval is the tightest interval a deployment realistically polls
// a readiness endpoint on — Kubernetes' default periodSeconds is 10, and 5 is the
// tightest value anybody writes by hand. It is the yardstick the cache window is
// held against below: a cached answer that can outlive one poll is a cached answer
// that reports a state that has already gone.
const shortestProbeInterval = 5 * time.Second

// TestReadyz_AStaleAnswerCannotOutliveOneProbe pins defaultReadyTTL by BEHAVIOUR.
//
// 🔴 THE CONSTANT CARRIED AN ARGUMENT AND NOTHING HELD IT: an audit set
// defaultReadyTTL to one hour and the whole suite stayed green, which means
// /readyz would have answered "ready" for an hour after the database died while an
// orchestrator kept routing taps to a process that cannot record one. That is the
// exact failure this endpoint exists to prevent, so the window is now measured
// rather than merely chosen.
//
// The clock is injected, so this costs no wall time and asserts an exact number of
// probes instead of racing one.
func TestReadyz_AStaleAnswerCannotOutliveOneProbe(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	h := newHealth(t, probe.Ping, nil) // the SHIPPED window: deliberately not overridden
	now := time.Now()
	h.now = func() time.Time { return now }

	get := func() int {
		rec := httptest.NewRecorder()
		h.Ready(rec, httptest.NewRequest(http.MethodGet, readyPath, nil))
		return rec.Code
	}

	if code := get(); code != http.StatusOK {
		t.Fatalf("first probe = %d, want 200", code)
	}
	// The database goes away without the clock moving: the cache may still answer,
	// and that is the whole point of having one.
	probe.setErr(errors.New("the database went away"))
	if code := get(); code != http.StatusOK {
		t.Fatalf("within the window the cached answer = %d, want the cached 200", code)
	}
	if got := probe.count(); got != 1 {
		t.Fatalf("the window was not in force (%d probes); the assertion below would measure nothing", got)
	}

	// One poll later, the answer MUST have been re-measured.
	now = now.Add(shortestProbeInterval)
	if code := get(); code != http.StatusServiceUnavailable {
		t.Errorf("%v after the database died /readyz still answers %d.\n"+
			"defaultReadyTTL (%v) lets a cached answer outlive a deployment's own poll interval, "+
			"so an orchestrator would keep sending taps to a process that cannot record one",
			shortestProbeInterval, code, defaultReadyTTL)
	}
	if got := probe.count(); got != 2 {
		t.Errorf("the database was asked %d times in total, want 2 — the second poll never reached it", got)
	}
	// And the window is bounded from BELOW as well: a zero window would make every
	// unauthenticated request a pool acquisition, which is what the cache exists to
	// prevent (TestReadyz_ABurstCostsOneQuery measures that side).
	if defaultReadyTTL <= 0 || defaultReadyTTL > shortestProbeInterval {
		t.Errorf("defaultReadyTTL = %v; it must be positive and shorter than one poll (%v)", defaultReadyTTL, shortestProbeInterval)
	}
}

// TestReadyz_TheProbeCarriesTheShippedDeadline pins defaultProbeTimeout.
//
// 🔴 THE HUNG-DATABASE TEST BELOW SETS ITS OWN 50 ms TIMEOUT, so it measures the
// MECHANISM and never the shipped VALUE — an audit set defaultProbeTimeout to
// twenty minutes and everything stayed green. What that would cost is not
// theoretical: check holds the mutex across the probe, so on a database that
// accepts connections and then never answers, EVERY caller of /readyz waits the
// whole timeout. With the shipped 2 s, an audit measured 200 concurrent requests
// against a hung database all answering 503 in 0.10 s wall clock.
//
// It reads the deadline the probe is HANDED rather than waiting for it, so the
// assertion costs nothing.
func TestReadyz_TheProbeCarriesTheShippedDeadline(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{}
	h := newHealth(t, probe.Ping, nil) // the SHIPPED timeout: deliberately not overridden

	rec := httptest.NewRecorder()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, readyPath, nil))

	budget, ok := probe.lastBudget()
	if !ok {
		t.Fatal("the probe was given NO deadline: a database that accepts a connection and never answers would hold every /readyz caller until the router's 30 s timeout, while holding a pool connection")
	}
	t.Logf("the probe was given %v (defaultProbeTimeout = %v)", budget.Round(time.Millisecond), defaultProbeTimeout)
	// The upper bound is the assertion that matters; the lower one keeps somebody
	// from "fixing" a slow database by making the probe unmeetable.
	if budget > shortestProbeInterval {
		t.Errorf("the probe may run for %v, which is longer than a deployment's own poll interval (%v): "+
			"on a hung database every /readyz caller queues behind it for that long",
			budget.Round(time.Millisecond), shortestProbeInterval)
	}
	if budget < 500*time.Millisecond {
		t.Errorf("the probe is given only %v; a database under load would be reported as unreachable", budget)
	}
}

// TestReadyz_AnOutageCostsTwoLogLines — the process log is a budgeted resource.
//
// The window is zero here, so every one of the 200 requests probes and fails: the
// bound being measured is the STATE-CHANGE rule, not the cache. A security audit on
// the marketing surface measured 200 cancelled loads writing 200 ERROR lines, and
// this endpoint is reachable by exactly the same strangers.
func TestReadyz_AnOutageCostsTwoLogLines(t *testing.T) {
	t.Parallel()
	var logged bytes.Buffer
	probe := &fakeProbe{err: errors.New("closed pool")}
	h := newHealth(t, probe.Ping, &logged)
	h.ttl = 0
	srv := healthServer(t, h)

	const n = 200
	for i := 0; i < n; i++ {
		res, err := srv.Client().Get(srv.URL + readyPath)
		if err != nil {
			t.Fatalf("GET %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}
	if got := probe.count(); got != n {
		t.Fatalf("the probe ran %d times, want %d — the log bound below would be measuring the cache instead", got, n)
	}
	if got := strings.Count(logged.String(), "level=ERROR"); got != 1 {
		t.Errorf("%d failed probes wrote %d ERROR lines, want exactly 1:\n%s", n, got, logged.String())
	}

	// And the recovery is announced exactly once, because an operator watching the
	// log needs the turn-over in both directions.
	probe.setErr(nil)
	for i := 0; i < 3; i++ {
		res, err := srv.Client().Get(srv.URL + readyPath)
		if err != nil {
			t.Fatalf("GET after recovery: %v", err)
		}
		if res.StatusCode != http.StatusOK {
			t.Errorf("after recovery status = %d, want 200", res.StatusCode)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}
	if got := strings.Count(logged.String(), "readiness regained"); got != 1 {
		t.Errorf("recovery was logged %d times, want exactly 1:\n%s", got, logged.String())
	}
	if got := strings.Count(logged.String(), "level=ERROR"); got != 1 {
		t.Errorf("the ERROR count changed after recovery (%d); the outage is one event", got)
	}
}

// TestReadyz_TheAnswerSaysNothingAboutTheDeployment — the §4.7 boundary.
//
// The error text below is the SHAPE OF A REAL ONE: pgx names the user, the database,
// the host and the port when a connection fails (measured in
// TestReadyz_ARealDriverErrorNamesTheDeployment, which drives the actual driver
// rather than trusting this string).
func TestReadyz_TheAnswerSaysNothingAboutTheDeployment(t *testing.T) {
	t.Parallel()
	var logged bytes.Buffer
	secretish := []string{"tappa_app", "db.internal.example", "5432", "tappa_production"}
	probe := &fakeProbe{err: errors.New(
		"failed to connect to `user=tappa_app database=tappa_production`: db.internal.example:5432 (db.internal.example): dial error: connection refused")}
	h := newHealth(t, probe.Ping, &logged)
	srv := healthServer(t, h)

	res, err := srv.Client().Get(srv.URL + readyPath)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.StatusCode)
	}
	// Headers as well as the body: a "helpful" X-Error header is the same leak.
	var seen bytes.Buffer
	seen.Write(body)
	if err := res.Header.Write(&seen); err != nil {
		t.Fatalf("serialising headers: %v", err)
	}
	for _, s := range secretish {
		if strings.Contains(seen.String(), s) {
			t.Errorf("the 503 response carries %q from the driver error:\n%s", s, seen.String())
		}
	}

	// POSITIVE CONTROL, and it is what makes the assertion above non-vacuous: the
	// detail exists and reached the OPERATOR. Without this, an error that was empty
	// all along would pass the loop above.
	if !strings.Contains(logged.String(), "db.internal.example") {
		t.Errorf("the cause did not reach the process log either, so the response test proves nothing:\n%s", logged.String())
	}
}

// TestReadyz_ACallerWhoHangsUpCannotFlipReadiness.
//
// 🔴 THE ATTACK THIS CLOSES: if the probe inherited the request's context, any
// stranger could cancel a request mid-probe, have this process cache
// context.Canceled as "the database is down", and write an ERROR line — repeatable
// from a browser tab. context.WithoutCancel severs it.
func TestReadyz_ACallerWhoHangsUpCannotFlipReadiness(t *testing.T) {
	t.Parallel()
	var logged bytes.Buffer
	probe := &fakeProbe{}
	h := newHealth(t, probe.Ping, &logged)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the client is already gone when the handler runs
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, readyPath, nil)
	rec := httptest.NewRecorder()
	h.Ready(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("a cancelled request made this process report itself as %d; readiness belongs to the process, not to one caller", rec.Code)
	}
	if probe.sawCancel.Load() {
		t.Error("the probe was handed an already-cancelled context: the caller's cancellation reached the database check")
	}
	if strings.Contains(logged.String(), "level=ERROR") {
		t.Errorf("a caller hanging up wrote an ERROR line into the process log:\n%s", logged.String())
	}
}

// TestReadyz_AHungDatabaseIsNotReadyRatherThanAHungRequest.
//
// Without a deadline the probe would sit on the router's 30 s timeout while holding
// a pool connection — the readiness check consuming the resource it reports on.
func TestReadyz_AHungDatabaseIsNotReadyRatherThanAHungRequest(t *testing.T) {
	t.Parallel()
	probe := &fakeProbe{block: make(chan struct{})}
	defer close(probe.block)
	h := newHealth(t, probe.Ping, nil)
	h.probeTimeout = 50 * time.Millisecond

	rec := httptest.NewRecorder()
	start := time.Now()
	h.Ready(rec, httptest.NewRequest(http.MethodGet, readyPath, nil))
	took := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a database that never answers gave %d, want 503", rec.Code)
	}
	if took > 5*time.Second {
		t.Errorf("the request took %v; the probe deadline did not bound it", took)
	}
}

// NewHealth must refuse a nil probe: a /readyz that answered 503 for ever because
// nobody passed a pool looks exactly like a database outage and is diagnosed as one.
func TestNewHealth_RefusesToBeBuiltWithoutAProbe(t *testing.T) {
	t.Parallel()
	if _, err := NewHealth(nil, discardLogger()); err == nil {
		t.Error("NewHealth(nil) built an endpoint that can never be ready")
	}
	if _, err := NewHealth((&fakeProbe{}).Ping, nil); err != nil {
		t.Errorf("NewHealth with a nil logger: %v — a nil logger is a default, not an error", err)
	}
}
