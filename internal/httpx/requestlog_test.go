package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/httpx"
)

// requestlog_test.go — M8-03 criterion 1 (request-id correlation) and the 5xx
// half of criterion 3.
//
// Every case here is a mutation test. Dropping the WithRequestID wrapper, dropping
// its WithAttrs override, logging the URL instead of the route, or moving
// AccessLog inside Recoverer each turns exactly one of these red.

// jsonLogger returns a logger whose records land in buf as one JSON object per
// line, wrapped the way cmd/tappa wraps the process logger.
func jsonLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(httpx.WithRequestID(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

// records parses buf into one map per line.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// TestWithRequestID_CorrelatesAHandlerLineWithItsAccessRecord is the whole point
// of criterion 1 stated as an assertion: two records emitted during one request
// carry the SAME request_id, and that id is not empty.
//
// It drives a real chi router built by NewRouter, not a hand-made middleware
// chain, because the thing being proven is the WIRING — httpx.RequestID must
// be mounted before AccessLog, and the handler's context must be the one the
// wrapper reads.
func TestWithRequestID_CorrelatesAHandlerLineWithItsAccessRecord(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := jsonLogger(&buf)

	r := httpx.NewRouter(&config.Config{}, log, mountFunc(func(r chi.Router) {
		r.Get("/probe/{id}", func(w http.ResponseWriter, req *http.Request) {
			log.InfoContext(req.Context(), "probe: reached the handler")
			w.WriteHeader(http.StatusTeapot)
		})
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe/abc", nil))

	recs := records(t, &buf)
	if len(recs) != 2 {
		t.Fatalf("want 2 records (handler line + access record), got %d: %s", len(recs), buf.String())
	}

	ids := map[string]bool{}
	for _, rec := range recs {
		id, _ := rec[httpx.LogRequestIDKey].(string)
		if id == "" {
			t.Fatalf("record has no %s: %v", httpx.LogRequestIDKey, rec)
		}
		ids[id] = true
	}
	if len(ids) != 1 {
		t.Fatalf("the two records carry DIFFERENT request ids (%v) — they cannot be joined", ids)
	}
}

// ---------------------------------------------------------------------------
// THE INBOUND HEADER (M8-03 round 4, S1). Every case here reproduces a measurement
// taken against the tree BEFORE the fix, where chi's middleware.RequestID copied
// X-Request-Id into the context verbatim and this card had just connected that
// field to every access record.
// ---------------------------------------------------------------------------

// TestRequestID_AnOversizedHeaderCannotFloodTheLog.
//
// 🔴 MEASURED BEFORE THE FIX, with a real listener and the production router: a
// 900 KB X-Request-Id on ONE unauthenticated GET /nope wrote 921 757 bytes of log,
// and 30 such requests in 218 ms wrote 26.4 MiB — against a whole retention window
// of 50 MiB (10 MiB × 5, deploy/README.md). AFTER: 183 bytes and 5 490 bytes. The
// records that window holds include the tap security alert and readiness.lost, and
// readiness.lost has no database copy.
//
// The bound asserted is deliberately loose: the point is a CONSTANT-size record, not
// a byte count that a field rename would falsify.
func TestRequestID_AnOversizedHeaderCannotFloodTheLog(t *testing.T) {
	t.Parallel()

	huge := strings.Repeat("A", 900*1024)

	var buf bytes.Buffer
	r := httpx.NewRouter(nil, jsonLogger(&buf))
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	req.Header.Set(httpx.RequestIDHeader, huge)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if n := buf.Len(); n > 1024 {
		t.Fatalf("one request with a %d-byte %s produced %d bytes of log. The id is caller-supplied "+
			"and unauthenticated: at this rate a few dozen requests erase the whole retention window, "+
			"including tap.security_alert and readiness.lost.", len(huge), httpx.RequestIDHeader, n)
	}
	recs := records(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 access record, got %d", len(recs))
	}
	id, _ := recs[0][httpx.LogRequestIDKey].(string)
	if len(id) > httpx.MaxRequestIDLen {
		t.Fatalf("request_id is %d characters, over the %d cap", len(id), httpx.MaxRequestIDLen)
	}
	if strings.Contains(id, "AAAA") {
		t.Fatalf("the caller's bytes reached the record: %q", id)
	}
}

// TestRequestID_APlausibleProxyIDIsPreserved is the OTHER half, and it is why the
// inbound header is bounded rather than refused.
//
// 🔴 MEASURED AGAINST THE DEPLOYED INGRESS: a probe request carrying a distinctive
// X-Request-Id came back in ingress-nginx's own access log as that exact value, in
// the $req_id field, on the same line as the CLIENT ADDRESS. This product's access
// record deliberately never carries the address (§4.7), so that join is the only
// attribution channel an operator has. Dropping the inbound header would have closed
// it — a fix that made a flood less traceable.
func TestRequestID_APlausibleProxyIDIsPreserved(t *testing.T) {
	t.Parallel()

	for _, id := range []string{
		"c1b2f3a49d8e7061a2b3c4d5e6f70819",     // nginx $request_id, 32 hex
		"0af7651916cd43dd8448eb211c80319c",     // W3C trace-id
		"7b3f2a10-5c4d-4e6f-8a9b-0c1d2e3f4a5b", // UUID (Envoy x-request-id)
		"probe_id.with-every-allowed-class9",
		strings.Repeat("z", httpx.MaxRequestIDLen), // exactly at the cap
	} {
		t.Run(id[:min(len(id), 12)], func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			r := httpx.NewRouter(nil, jsonLogger(&buf))
			req := httptest.NewRequest(http.MethodGet, "/nope", nil)
			req.Header.Set(httpx.RequestIDHeader, id)
			r.ServeHTTP(httptest.NewRecorder(), req)

			recs := records(t, &buf)
			if len(recs) != 1 {
				t.Fatalf("want 1 record, got %d: %s", len(recs), buf.String())
			}
			if got, _ := recs[0][httpx.LogRequestIDKey].(string); got != id {
				t.Fatalf("request_id = %q, want the proxy's %q — the join with the ingress access "+
					"log (which carries the client address) is broken", got, id)
			}
		})
	}
}

// TestRequestID_AnUnacceptableHeaderIsSilentlyReplaced walks the rejection classes.
//
// SILENTLY, and REPLACED rather than refused, are both decisions: a 400 here would
// let anyone turn a log-hygiene rule into a denial of service against employees
// tapping in, and a log line per rejected header would be the same unbounded,
// caller-driven writer this rule exists to remove.
func TestRequestID_AnUnacceptableHeaderIsSilentlyReplaced(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, value string }{
		{"the auditor's injection attempt", `a" level=ERROR msg="FORGED_LINE_FROM_A_HEADER`},
		{"a newline", "abc\ndef"},
		{"a tab", "abc\tdef"},
		{"a space", "abc def"},
		{"one over the cap", strings.Repeat("z", httpx.MaxRequestIDLen+1)},
		{"chi's own slash-bearing shape", "tappa-6fd5489dcb-q5b4l/Ab3Cd5Ef7G-000001"},
		{"non-ascii", "kimlik-çağrı"},
		{"a control byte", "abc\x00def"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			r := httpx.NewRouter(nil, jsonLogger(&buf))
			req := httptest.NewRequest(http.MethodGet, "/nope", nil)
			// Bypass Header.Set's own sanitising so the control bytes really arrive.
			req.Header[httpx.RequestIDHeader] = []string{tc.value}
			r.ServeHTTP(httptest.NewRecorder(), req)

			recs := records(t, &buf)
			if len(recs) != 1 {
				t.Fatalf("want exactly 1 record — a second one would be a forged line: %s", buf.String())
			}
			got, _ := recs[0][httpx.LogRequestIDKey].(string)
			if got == tc.value {
				t.Fatalf("the unacceptable value %q was carried into the record verbatim", tc.value)
			}
			if got == "" {
				t.Fatalf("the record lost its request_id entirely — correlation is gone: %v", recs[0])
			}
			if strings.Contains(buf.String(), "FORGED_LINE_FROM_A_HEADER") {
				t.Fatalf("the injection payload reached the log: %s", buf.String())
			}
		})
	}
}

// TestRequestID_AMintedIDPassesItsOwnFilter. A generated id that this package's own
// inbound rule would reject is a contradiction waiting to be found by whoever next
// puts a proxy in front of another one — chi's generator was exactly that shape
// (a pod name, then '/', then a counter).
//
// The pattern below RESTATES acceptableRequestID rather than calling it (this is an
// external test package). That is the point: if the rule changes, one of the two has
// to be edited deliberately.
func TestRequestID_AMintedIDPassesItsOwnFilter(t *testing.T) {
	t.Parallel()

	rule := regexp.MustCompile(`^[A-Za-z0-9._-]{1,` + strconv.Itoa(httpx.MaxRequestIDLen) + `}$`)

	seen := map[string]bool{}
	var buf bytes.Buffer
	r := httpx.NewRouter(nil, jsonLogger(&buf))
	for i := 0; i < 50; i++ {
		buf.Reset()
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))
		recs := records(t, &buf)
		id, _ := recs[0][httpx.LogRequestIDKey].(string)
		if !rule.MatchString(id) {
			t.Fatalf("a MINTED request id %q fails this package's own inbound filter %s", id, rule)
		}
		if seen[id] {
			t.Fatalf("request id %q was minted twice in 50 requests — ids that collide cannot join "+
				"two records to one request", id)
		}
		seen[id] = true
	}
}

// TestWithRequestID_SurvivesLoggerWith. A derived logger is the ordinary shape
// (base.With("component", …)), and slog implements With by calling
// Handler.WithAttrs. A wrapper that does not override WithAttrs returns the INNER
// handler, so every record from the derived logger silently loses its id — and a
// missing attribute is indistinguishable from a request that had none.
func TestWithRequestID_SurvivesLoggerWith(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	derived := jsonLogger(&buf).With("component", "probe")

	r := httpx.NewRouter(nil, nil, mountFunc(func(r chi.Router) {
		r.Get("/probe", func(w http.ResponseWriter, req *http.Request) {
			derived.InfoContext(req.Context(), "probe")
		})
	}))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))

	recs := records(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d: %s", len(recs), buf.String())
	}
	if id, _ := recs[0][httpx.LogRequestIDKey].(string); id == "" {
		t.Fatalf("a derived logger lost its request id: %v", recs[0])
	}
}

// TestWithRequestID_WithGroupNestsTheID is a COUNTED LIMIT rather than a feature,
// and it is pinned because the failure it describes is silent.
//
// slog's Handler interface gives a wrapper no way to reach past a group it has
// been asked to open: once WithGroup has been called, every attribute the wrapper
// adds — including this one — lands INSIDE that group. deploy/README.md's alert
// rules filter on a top-level request_id, so a logger built with WithGroup would
// produce records those rules cannot join, with nothing reporting it.
//
// MEASURED: nothing in this repository calls WithGroup (grep, production and test
// trees), so no record shipped today is affected. This test exists so that the
// first caller who adds one is TOLD, here, instead of discovering it from an alert
// that stopped matching.
func TestWithRequestID_WithGroupNestsTheID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	grouped := jsonLogger(&buf).WithGroup("inner")

	r := httpx.NewRouter(nil, nil, mountFunc(func(r chi.Router) {
		r.Get("/probe", func(w http.ResponseWriter, req *http.Request) {
			grouped.InfoContext(req.Context(), "probe")
		})
	}))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))

	recs := records(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d: %s", len(recs), buf.String())
	}
	if _, ok := recs[0][httpx.LogRequestIDKey]; ok {
		t.Fatalf("WithGroup no longer nests the request id — that is an IMPROVEMENT, and the "+
			"limit recorded in internal/httpx/requestlog_test.go and deploy/README.md is now stale. "+
			"Rewrite both, then delete this test: %v", recs[0])
	}
	inner, _ := recs[0]["inner"].(map[string]any)
	if id, _ := inner[httpx.LogRequestIDKey].(string); id == "" {
		t.Fatalf("the id is neither at the root nor inside the group — it was LOST: %v", recs[0])
	}
}

// TestWithRequestID_NonContextCallsAreNotStamped is the COUNTED LIMIT, asserted
// rather than merely written down. slog hands a handler a context only on the
// *Context call variants, so a plain .Info() cannot be correlated — and M8-03
// converted the tap decision chain only, leaving most of the tree on plain calls
// (backlog T51 measured why converting all of them is a package-wide change).
//
// Pinning it stops the limit from being quietly forgotten and then rediscovered
// as a bug: if a future change DOES make plain calls carry an id, this test goes
// red and somebody has to update the claim in deploy/README.md with it.
func TestWithRequestID_NonContextCallsAreNotStamped(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := jsonLogger(&buf)

	r := httpx.NewRouter(nil, nil, mountFunc(func(r chi.Router) {
		r.Get("/probe", func(w http.ResponseWriter, _ *http.Request) {
			log.Info("probe: a plain call, no context")
		})
	}))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/probe", nil))

	recs := records(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d: %s", len(recs), buf.String())
	}
	if _, ok := recs[0][httpx.LogRequestIDKey]; ok {
		t.Fatalf("a plain .Info() gained a request id — the documented limit in "+
			"deploy/README.md and internal/httpx/requestlog.go is now WRONG and must be rewritten: %v", recs[0])
	}
}

// TestAccessLog_NeverLogsTheURL is a §4.7 test, and it is the sharpest one in this
// file. This product carries a live SUN signature (?cmac=) and a live invite code
// (?code=) in the QUERY STRING, so an access record that printed the URL would put
// two never-log values into every request's log line.
func TestAccessLog_NeverLogsTheURL(t *testing.T) {
	t.Parallel()

	const (
		liveCMAC   = "deadbeefdeadbeefdeadbeefdeadbeef"
		liveCode   = "SECRETINVITECODE"
		liveTagUID = "04a1b2c3d4e580"
	)

	var buf bytes.Buffer
	r := httpx.NewRouter(nil, jsonLogger(&buf), mountFunc(func(r chi.Router) {
		r.Get("/t", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	}))

	req := httptest.NewRequest(http.MethodGet,
		"/t?tag="+liveTagUID+"&ctr=000010&cmac="+liveCMAC+"&code="+liveCode, nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	for _, banned := range []string{liveCMAC, liveCode, liveTagUID, "ctr=", "cmac", "?"} {
		if strings.Contains(out, banned) {
			t.Fatalf("the access record leaked %q from the query string: %s", banned, out)
		}
	}

	recs := records(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 access record, got %d: %s", len(recs), out)
	}
	if got := recs[0][httpx.LogRouteKey]; got != "/t" {
		t.Fatalf("route = %v, want the registered pattern %q", got, "/t")
	}
}

// TestAccessLog_RouteIsThePatternNotThePath. A path VARIABLE is caller-supplied;
// the pattern is one of a fixed set decided at compile time. Logging the path
// would make this field's value space unbounded and therefore capable of carrying
// anything a caller put in a URL segment.
func TestAccessLog_RouteIsThePatternNotThePath(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := httpx.NewRouter(nil, jsonLogger(&buf), mountFunc(func(r chi.Router) {
		r.Get("/admin/employees/{id}", func(w http.ResponseWriter, _ *http.Request) {})
	}))
	r.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/admin/employees/9f6d0e2a-secret-in-a-segment", nil))

	recs := records(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if got := recs[0][httpx.LogRouteKey]; got != "/admin/employees/{id}" {
		t.Fatalf("route = %v, want the brace pattern", got)
	}
	if strings.Contains(buf.String(), "secret-in-a-segment") {
		t.Fatalf("a path segment reached the log: %s", buf.String())
	}
}

// TestAccessLog_PanicIsRecordedAsA5xx is the criterion-3 5xx signal, and it is the
// case the middleware ORDER exists for. AccessLog reports the status from a
// deferred read; mounted inside Recoverer it would unwind before anything wrote a
// header and log 200 — so the one failure the rule is for would be invisible
// exactly when it happened.
func TestAccessLog_PanicIsRecordedAsA5xx(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := httpx.NewRouter(nil, jsonLogger(&buf), mountFunc(func(r chi.Router) {
		r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("probe: deliberate") })
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (Recoverer)", w.Code)
	}

	recs := records(t, &buf)
	var access map[string]any
	for _, rec := range recs {
		if rec["msg"] == httpx.EventHTTPRequest {
			access = rec
		}
	}
	if access == nil {
		t.Fatalf("no %s record after a panic: %s", httpx.EventHTTPRequest, buf.String())
	}
	if status, _ := access[httpx.LogStatusKey].(float64); int(status) != http.StatusInternalServerError {
		t.Fatalf("%s = %v, want 500 — the 5xx alert rule would be silent on a panic",
			httpx.LogStatusKey, access[httpx.LogStatusKey])
	}
	if access["level"] != "ERROR" {
		t.Fatalf("level = %v, want ERROR — deploy/README.md's 5xx rule filters on it", access["level"])
	}
}

// TestAccessLog_UnroutedRequestStillGetsARecord. A 404 is the one shape where chi
// matched no pattern at all, so RoutePattern() is empty. Falling back to the PATH
// there would reintroduce caller-controlled data on exactly the requests a scanner
// generates, so the fallback is a literal.
func TestAccessLog_UnroutedRequestStillGetsARecord(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := httpx.NewRouter(nil, jsonLogger(&buf))
	r.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/nope/../%2e%2e/etc/passwd?q=probe-secret", nil))

	recs := records(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d: %s", len(recs), buf.String())
	}
	if got, _ := recs[0][httpx.LogRouteKey].(string); strings.Contains(got, "passwd") || strings.Contains(got, "probe-secret") {
		t.Fatalf("the unmatched route carried the caller's path: %q", got)
	}
	if status, _ := recs[0][httpx.LogStatusKey].(float64); int(status) != http.StatusNotFound {
		t.Fatalf("status = %v, want 404", recs[0][httpx.LogStatusKey])
	}
}

// TestAccessLog_NilLoggerIsSilent. NewRouter(cfg, nil, …) is what every routing
// unit test in this repository uses; it must not panic and must not write.
func TestAccessLog_NilLoggerIsSilent(t *testing.T) {
	t.Parallel()

	r := httpx.NewRouter(nil, nil, mountFunc(func(r chi.Router) {
		r.Get("/probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// ---------------------------------------------------------------------------
// PROBE ROUTES AND THE LOG TARGET ITSELF (M8-03 round 2).
//
// The three tests below are the behavioural half of requestlog.go's
// probeDesignedStatus table and writeAccessRecord. Each one reproduces a
// measurement taken against the FIRST round of this card, where every response —
// probes included — went through a synchronous write to the process logger.
// ---------------------------------------------------------------------------

// slowLogHandler is a slog.Handler whose Handle takes a while. It stands in for
// the real hazard: in production the process logger writes to os.Stdout, which is
// a container log pipe, and a node that cannot drain that pipe makes the write
// BLOCK.
type slowLogHandler struct{ d time.Duration }

func (h slowLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h slowLogHandler) Handle(context.Context, slog.Record) error {
	time.Sleep(h.d)
	return nil
}
func (h slowLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h slowLogHandler) WithGroup(string) slog.Handler      { return h }

// panicLogHandler is a slog.Handler that panics. A handler CAN panic — a nil map
// in a ReplaceAttr, a LogValuer that dereferences a nil pointer, an io.Writer that
// panics on a closed file — and AccessLog is the one log call in this process that
// sits OUTSIDE middleware.Recoverer.
type panicLogHandler struct{}

func (panicLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (panicLogHandler) Handle(context.Context, slog.Record) error {
	panic("probe: the log target itself panicked")
}
func (panicLogHandler) WithAttrs([]slog.Attr) slog.Handler { return panicLogHandler{} }
func (panicLogHandler) WithGroup(string) slog.Handler      { return panicLogHandler{} }

// TestAccessLog_AHealthyProbeIsNotAnEvent.
//
// 🔴 THE MEASUREMENT THIS PINS. deploy/k8s/20-app.yaml probes /readyz every 5 s and
// /healthz every 10 s: 25 920 requests a day at ~197 bytes of access record each is
// ~5.1 MB/day with no users at all, and — the part that actually broke something —
// /readyz's DESIGNED 503 came out at level=ERROR, which is exactly what
// deploy/README.md's 5xx rule filters on with a threshold of 5 events per 5
// minutes. A database wobble tripped "the server is broken" in about 25 seconds.
//
// The 503 case is the sharp one: it asserts that a designed failure is silent HERE,
// which is only defensible because internal/handler.Health logs the transition
// itself (EventReadinessLost / EventReadinessRegained, one record per state change,
// carrying the driver error). Deleting that pair does not turn this test red —
// TestReadyz_AnOutageCostsTwoLogLines is the one that does.
func TestAccessLog_AHealthyProbeIsNotAnEvent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		path   string
		status int
	}{
		{"liveness answering ok", httpx.HealthPath, http.StatusOK},
		{"readiness answering ok", httpx.ReadyPath, http.StatusOK},
		{"readiness answering its designed 503", httpx.ReadyPath, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			r := httpx.NewRouter(nil, jsonLogger(&buf), mountFunc(func(r chi.Router) {
				r.Get(httpx.ReadyPath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
			}))
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tc.path, nil))

			if got := strings.TrimSpace(buf.String()); got != "" {
				t.Fatalf("a probe answering as designed wrote an access record: %s\n"+
					"At the shipped probe periods that is 25 920 records a day with zero users, and "+
					"a 503 among them fires deploy/README.md's 5xx rule 60 times per 5 minutes "+
					"against a threshold of 5.", got)
			}
		})
	}
}

// TestAccessLog_AnUndesignedProbeStatusIsStillRecorded is why the exclusion is a
// SET of designed statuses rather than "skip these two routes".
//
// If h.Ready panics, Recoverer writes 500. A blanket exclusion would have made that
// the one silent 5xx in the product — a readiness handler crashing on every probe,
// with the alert rule reading "healthy". 500 is not in /readyz's designed set, so
// the record fires exactly as it would on any other route.
func TestAccessLog_AnUndesignedProbeStatusIsStillRecorded(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := httpx.NewRouter(nil, jsonLogger(&buf), mountFunc(func(r chi.Router) {
		r.Get(httpx.ReadyPath, func(http.ResponseWriter, *http.Request) { panic("probe: deliberate") })
	}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, httpx.ReadyPath, nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (Recoverer)", w.Code)
	}

	recs := records(t, &buf)
	var access map[string]any
	for _, rec := range recs {
		if rec["msg"] == httpx.EventHTTPRequest {
			access = rec
		}
	}
	if access == nil {
		t.Fatalf("a panicking readiness handler produced NO %s record — the probe-route exclusion "+
			"swallowed a real 5xx:\n%s", httpx.EventHTTPRequest, buf.String())
	}
	if access["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", access["level"])
	}
	if got := access[httpx.LogRouteKey]; got != httpx.ReadyPath {
		t.Errorf("route = %v, want %q", got, httpx.ReadyPath)
	}
}

// TestAccessLog_ContainsAPanicFromTheLogTarget.
//
// 🔴 MEASURED BEFORE THE FIX: with a slog.Handler whose Handle panicked, GET
// /healthz and GET /probe both answered `EOF` — the connection died. The stack
// ended in net/http.(*conn).serve, NOT in middleware.Recoverer, because AccessLog
// is mounted OUTSIDE Recoverer on purpose and nothing downstream can recover what
// AccessLog raises. A logging defect became a dropped request.
func TestAccessLog_ContainsAPanicFromTheLogTarget(t *testing.T) {
	t.Parallel()

	log := slog.New(panicLogHandler{})
	srv := httptest.NewServer(httpx.NewRouter(nil, log, mountFunc(func(r chi.Router) {
		r.Get("/probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	})))
	t.Cleanup(srv.Close)

	res, err := srv.Client().Get(srv.URL + "/probe")
	if err != nil {
		t.Fatalf("GET /probe with a panicking log target: %v\n"+
			"The access record must never be able to take the response down with it.", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418 — the handler's answer must survive the logger", res.StatusCode)
	}
}

// TestAccessLog_ADroppedRecordIsAnnouncedOnce pins the HONESTY half of the recovery
// above, which nothing was holding.
//
// 🔴 THE GAP THIS CLOSES WAS MEASURED BY MUTATION: deleting the whole
// warnedOnce.Do(...) block — i.e. silently swallowing a dropped access record —
// left every test in this package GREEN. The rescue was pinned; the admission that
// something was lost was not. §4.6 is about evidence not disappearing quietly, and
// a containment that says nothing is exactly a quiet disappearance.
//
// ⚠️ AND IT MEASURES THE SCOPE RATHER THAN REPEATING THE CLAIM. The warning used to
// say "once per process". warnedOnce is a local of the AccessLog call, so the true
// scope is one middleware INSTALLATION: the second half below mounts a second router
// in the same process and requires a SECOND warning. If somebody ever makes the
// once-per-process claim true (a package-level sync.Once, which §7 forbids), this
// half goes red and the sentence has to be revisited with it.
//
// NOT t.Parallel: it replaces os.Stderr, which is process-global. Go runs the
// sequential tests in a package to completion before resuming the parallel ones, so
// the parallel test above — which triggers this same warning — cannot interleave.
func TestAccessLog_ADroppedRecordIsAnnouncedOnce(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	real := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	t.Cleanup(func() { os.Stderr = real })

	hit := func(router http.Handler, n int) {
		for i := 0; i < n; i++ {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
			if rec.Code != http.StatusTeapot {
				t.Fatalf("status = %d, want 418", rec.Code)
			}
		}
	}
	mount := mountFunc(func(r chi.Router) {
		r.Get("/probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	})

	first := httpx.NewRouter(nil, slog.New(panicLogHandler{}), mount)
	hit(first, 3)
	second := httpx.NewRouter(nil, slog.New(panicLogHandler{}), mount)
	hit(second, 3)

	os.Stderr = real
	_ = w.Close()
	out := <-done

	const marker = "PANICKED while writing an access record"
	got := strings.Count(out, marker)
	if got != 2 {
		t.Errorf("six dropped access records across TWO AccessLog installations announced themselves "+
			"%d time(s), want 2 (once per installation, three times deduplicated within each). "+
			"0 means the drop is silent and §4.6 is not being kept; 6 means the deduplication is "+
			"gone and a flapping log target becomes its own flood:\n%s", got, out)
	}
	if strings.Contains(out, "probe: the log target itself panicked") {
		t.Errorf("the warning printed the PANIC VALUE. It comes from a handler part-way through "+
			"formatting a record, so it can carry anything the caller was logging (§4.7):\n%s", out)
	}
	if !strings.Contains(out, "not once per process") {
		t.Errorf("the warning no longer states its real scope. warnedOnce is a local of AccessLog, "+
			"so the claim it may make is per INSTALLATION; the count above is the proof:\n%s", out)
	}
}

// TestHealthz_AnswersWhileTheLogTargetIsUnavailable is the liveness guarantee
// restated for the thing M8-03 added.
//
// 🔴 MEASURED BEFORE THE FIX: with a Handle that sleeps 2 s, GET /healthz answered
// 200 after 2.001 s. In production os.Stdout is a container log pipe; a node that
// cannot drain it makes the write block, the livenessProbe (timeoutSeconds 2,
// failureThreshold 3 — deploy/k8s/20-app.yaml) fails, and the kubelet kills a
// healthy process. TestHealthz_CannotDependOnAnything is named for exactly that
// failure and could not see this one, because the dependency arrived as a
// PARAMETER TYPE it already allowed.
//
// The bound is deliberately generous: this is not a latency budget, it is the
// difference between "does not touch the log target" and "waits for it".
func TestHealthz_AnswersWhileTheLogTargetIsUnavailable(t *testing.T) {
	t.Parallel()

	const stall = 2 * time.Second
	srv := httptest.NewServer(httpx.NewRouter(nil, slog.New(slowLogHandler{d: stall})))
	t.Cleanup(srv.Close)

	start := time.Now()
	res, err := srv.Client().Get(srv.URL + httpx.HealthPath)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("GET %s with a stalled log target: %v", httpx.HealthPath, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
	if took >= stall {
		t.Fatalf("GET %s took %v with a log target that stalls for %v — liveness is waiting on the "+
			"log pipe, so a node that cannot drain stdout makes the kubelet kill a healthy process",
			httpx.HealthPath, took, stall)
	}
}

// TestHealthz_AnswersWhileTheLogTargetPanics — the second half of the same
// guarantee. Before the fix this returned EOF.
func TestHealthz_AnswersWhileTheLogTargetPanics(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(httpx.NewRouter(nil, slog.New(panicLogHandler{})))
	t.Cleanup(srv.Close)

	res, err := srv.Client().Get(srv.URL + httpx.HealthPath)
	if err != nil {
		t.Fatalf("GET %s with a panicking log target: %v — a liveness probe reading a dropped "+
			"connection restarts the process", httpx.HealthPath, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Errorf("GET %s = %d %q, want 200 %q", httpx.HealthPath, res.StatusCode, body, "ok")
	}
}
