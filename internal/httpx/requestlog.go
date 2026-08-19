package httpx

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Request-id correlation and the one access record per response (M8-03).
//
// 🔴 WHAT WAS ACTUALLY WRONG BEFORE THIS FILE, MEASURED. router.go mounted
// chi's middleware.RequestID from M5-03 until round 4 of this card, so every request
// has carried an id for months — and NOTHING read it. A scan of the production tree for
// RequestID|request_id|requestID|X-Request-Id found exactly ONE hit, the r.Use
// line that mints it. So the id existed, cost an allocation per request, and
// reached no log line: two records from the same request could not be joined, and
// a 5xx left no record at all because nothing logged a response status.
//
// 🔴 WHY THE HANDLER WRAPPER RATHER THAN A LOGGER PASSED DOWN THE CALL CHAIN. Both
// shapes were measured on this tree (the numbers are in the M8-03 hand-off). A
// per-request logger fetched at the call site — reqLog(r).Error(...) — changes the
// RECEIVER EXPRESSION at 323 of the tree's 346 logger call sites, and redline-check.sh's
// R7b matches personal data inside a log call only when the receiver is spelled
// log/slog/fmt (backlog T51 measured that dependency). So that shape buys
// correlation by silently switching off the §4.7 net at 323 sites. This one does
// not: a.log.ErrorContext(ctx, ...) still reads as `log.` to R7b.
//
// ⚠️ EVERY COUNT IN THE PARAGRAPH BELOW IS FROM THE TREE **BEFORE THIS CARD** (the
// commit M8-03 branched from), because the counts exist to justify a choice made
// against that tree. Saying which tree is not pedantry: a first round wrote 347 and
// 52 here while deploy/README.md wrote 346 and 51 for the same fact, and the same
// paragraph then derived 323 = 346 − 23 from the number it had just contradicted.
// Re-measured with an AST walk that mirrors this repository's own scan rules
// (cmd/tappa/observability_test.go): BEFORE = 346 call sites · 51 · 272 · 23 · 46
// files · 0 *Context calls. AFTER this card = 349 · 54 · 272 · 23 · 47 files · 33
// *Context calls. 346 is the right number here and 323 = 346 − 23 now follows from it.
// The AFTER pair read 53 · 273 for a round, which was arithmetically impossible:
// the three call sites this card ADDED all sit inside a function that carries a
// context.Context (checkin.go's s.log.Log and s.log.WarnContext in Record, and
// writeAccessRecord's log.LogAttrs below), so nothing could enter the
// *http.Request-only bucket and 272 cannot move. 51 + 3 = 54. deploy/README.md's
// limit 26 carries the per-file breakdown.
//
// ⚠️ COUNTED LIMIT, STATED PLAINLY: this wrapper can only stamp records whose call
// used a *Context* variant, because those are the only ones slog hands a context
// to. Converting all 346 call sites is the package-wide transformation backlog T51
// measured and refused (224 sites in internal/handler alone), so M8-03 converted
// 32 — the tap decision chain — and left the rest. Measured on the tree those 346
// sit in: 51 of the call sites are inside a function that carries a
// context.Context, 272 inside one that carries only an *http.Request, and 23 inside
// one that carries NEITHER, so those 23 (start-up and background lines) could not
// be correlated by any shape at all. A log line without a request_id is not broken,
// it was never reachable. The access record below is what still ties such a line to
// a request, by time and route rather than by id.

// The names below are ALERT INTERFACE, not internal spelling. deploy/README.md's
// alert rules filter on these exact strings, and TestObservability_AlertSignalNames
// (cmd/tappa) fails if a constant here and the rule in that document disagree —
// because a renamed field does not break a query, it makes it match nothing, which
// is an alert that dies without saying so.
const (
	// LogRequestIDKey is the attribute every correlated record carries.
	LogRequestIDKey = "request_id"

	// EventHTTPRequest is emitted once per response. It is the 5xx-rate signal.
	EventHTTPRequest = "http.request"

	// The attributes of EventHTTPRequest that an alert rule reads.
	LogStatusKey     = "status"
	LogRouteKey      = "route"
	LogMethodKey     = "method"
	LogDurationMSKey = "duration_ms"
)

// routeUnmatched is the route value for a request chi matched no pattern for
// (404, 405). It is a literal rather than the path, for the reason WithRequestID's
// sibling comment below gives: the path is caller-supplied, the pattern is not.
const routeUnmatched = "(unmatched)"

// ---------------------------------------------------------------------------
// THE REQUEST ID ITSELF (M8-03 round 4, S1).
//
// 🔴 WHY THIS PACKAGE MINTS ITS OWN INSTEAD OF MOUNTING chi's middleware.RequestID.
// chi v5.3.1's middleware/request_id.go is four lines of policy:
// `requestID := r.Header.Get(RequestIDHeader)`, and if that is not empty it is used
// AS IS. So the value of the one field this card put on EVERY access record was
// chosen by the caller, with no length bound, no character bound and no identity.
// That was harmless for months precisely because nothing read the id; this card is
// what connected it to the log, so this card is what has to bound it.
//
// MEASURED ON THIS TREE, BEFORE THE CHANGE (probe: a real net.Listener, the
// production router, a slog.JSONHandler wrapped exactly as cmd/tappa wraps it):
//
//	900 KB X-Request-Id + GET /nope   ->     921 757 bytes of log from ONE 404
//	400 KB X-Request-Id + GET /nope   ->     409 757 bytes
//	30 such requests, 218 ms          ->  27 652 710 bytes (26.4 MiB)
//
// The node's whole retention window is 10 MiB × 5 = 50 MiB (deploy/README.md), so
// roughly 57 unauthenticated requests erase every record in it — including
// checkin.EventTapSecurityAlert and handler.EventReadinessLost, the latter of which
// has NO database copy and is written once per outage. A stolen session tapping from
// a deactivated account could delete its own alarm.
//
// ⚠️ AND THE FRONT DOOR ALREADY NARROWS IT, WHICH IS SAID HERE BECAUSE IT IS NOT A
// FIX AND MUST NOT BE READ AS ONE. Measured against the deployed ingress the same
// day: 8 000 bytes -> 404 (admitted), 12 000 bytes -> 400 on HTTP/1.1 and a framing
// error on HTTP/2. So ingress-nginx caps a header line at about 8 KB. That cap
// belongs to a SHARED controller with an empty ConfigMap that ~20 other applications
// share (deploy/k8s/40-ingress.yaml documents the sharing), it is a default nobody
// here owns, and 8 KB per record still erases a 50 MiB window in a few thousand
// requests. A bound this product depends on has to live in this product.
//
// 🔴 THE SHAPE WAS CHOSEN BY MEASUREMENT, NOT BY TASTE. Three were weighed:
//
//	(a) REFUSE the inbound header and always mint. Simplest, and the cost looked
//	    theoretical until it was measured. It is not: the ingress access log's LAST
//	    field is $req_id, and a probe with a distinctive value came back through the
//	    controller's own log as that exact value — so a client-supplied id is
//	    forwarded unchanged AND recorded by nginx beside the CLIENT ADDRESS, which
//	    this product's access record deliberately never carries (§4.7, AccessLog's
//	    comment). That join is the only attribution channel an operator has for a
//	    flood. (a) would have destroyed it to fix a flood.
//	(b) ACCEPT BUT BOUND — this. A value that passes acceptableRequestID is kept, so
//	    the nginx join survives for every real generator (nginx $request_id is 32 hex
//	    characters; a UUID, a W3C trace id and an Envoy x-request-id all pass).
//	    Anything else is REPLACED, silently, and the request is served normally —
//	    refusing the request would turn a log-hygiene rule into an availability
//	    lever, which §4.6's spirit ("evidence is never thrown away") argues against.
//	(c) TRUNCATE at write time. Rejected as insufficient rather than wrong: it
//	    shortens one record and leaves the caller's bytes in the chi context for the
//	    life of the request, leaves the number of records per request untouched, and
//	    still lets the caller CHOOSE the content of a field on every record.
//
// ⚠️ WHAT (b) DOES NOT BUY, COUNTED. An id that passes the filter is still
// caller-CHOSEN: an attacker can make every record they cause carry a value of their
// picking, and can collide with an id an operator is filtering on. That is a
// nuisance, not a flood, and it is the price of keeping the nginx join. The
// attribution answer is unchanged and is written here so nobody has to rediscover
// it: when records arrive faster than they should, the client address is in the
// INGRESS log, on the same $req_id — never in ours.
const (
	// RequestIDHeader is the inbound spelling, matching what ingress-nginx forwards.
	RequestIDHeader = "X-Request-Id"

	// MaxRequestIDLen is the hard cap on an ACCEPTED inbound id. 64 is chosen from
	// the generators that actually exist rather than from a round number: nginx
	// $request_id is 32, a UUID is 36, a W3C traceparent is 55, and rand.Text below
	// is 26. Nothing legitimate is near it, and it is four orders of magnitude below
	// the 921 757-byte record measured above.
	MaxRequestIDLen = 64

	// MaxHeaderBytes is what cmd/tappa gives http.Server. Go's default is 1 MiB,
	// which is the ceiling every measurement above ran into.
	//
	// It does NOT close S1 on its own — 1 MiB of request id would still be 1 MiB of
	// log line — and it is set anyway because it bounds header channels this file
	// knows nothing about (a huge Cookie, a huge User-Agent, a hundred invented
	// headers), which no per-header rule can. 16 KiB is 2× what the shared ingress
	// admits per header line (8 KB, measured) and far above this product's own
	// worst case: the largest legitimate request it makes is a cookie jar, and the
	// ten cookie names in internal/handler + internal/session + internal/adminauth
	// carry a 43-character token as their longest value.
	//
	// ⚠️ COUNTED LIMIT, MEASURED: a request over this size is answered 431 by
	// net/http DURING HEADER PARSING, before any middleware runs — so it produces NO
	// access record at all (probed: 0 bytes of log). A caller hammering the ceiling
	// is therefore invisible to this process. That is not evidence being discarded
	// (§4.6): the request never reached a decision, nothing about it was known, and
	// the ingress log — which has the client address — still has the line. It is
	// written down because "no records" and "no requests" look identical here.
	MaxHeaderBytes = 16 << 10
)

// requestIDKey is this package's context key. It is a struct{} type declared here
// rather than chi's middleware.RequestIDKey because the id no longer comes from
// chi: leaving the read pointed at chi's key while the write came from here would
// have produced an empty id on every request, silently.
type requestIDKey struct{}

// RequestID puts a BOUNDED request id in the context of every request.
//
// It never fails a request. An unacceptable inbound value is replaced, not
// rejected: the header is diagnostic plumbing, and a 400 here would let anyone turn
// a log-correlation feature into a denial of service against real employees tapping
// in. Nothing is logged about the replacement either — a record per rejected header
// is the same unbounded, caller-driven writer this rule exists to remove.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !acceptableRequestID(id) {
			id = mintRequestID()
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// acceptableRequestID reports whether an inbound value may be carried into the log.
//
// The character set is ASCII letters, digits, '-', '_' and '.'. It is a positive
// list rather than a ban list on purpose: it admits every id format this deployment
// can actually receive and excludes, structurally, the quote, backslash, newline,
// space, control byte and non-ASCII byte that a log-injection attempt needs.
//
// ⚠️ IT IS DEFENCE IN DEPTH, NOT THE ESCAPING. Measured before this change:
// slog.JSONHandler and slog's text handler BOTH escaped a hand-built forged record
// out of an `X-Request-Id: a" level=ERROR msg="…` header — no synthetic line was
// produced. The encoders were doing their job; this makes the property hold without
// depending on which encoder TAPPA_LOG_FORMAT selected.
func acceptableRequestID(v string) bool {
	if v == "" || len(v) > MaxRequestIDLen {
		return false
	}
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// mintRequestID returns this process's own id for a request.
//
// crypto/rand.Text is 26 characters of the RFC 4648 base32 alphabet (A–Z, 2–7), so
// it needs no error path (it panics rather than returning a degraded value), no
// package-level counter and no process prefix — which is what keeps this out of §7's
// "no package-level singleton" argument entirely. chi's generator was
// `hostname/base62-000001`; the hostname is a pod name and the '/' is outside the
// character set above, so a chi-shaped id would not even pass this package's own
// filter. TestRequestID_AMintedIDPassesItsOwnFilter pins that it does.
func mintRequestID() string { return rand.Text() }

// requestIDFrom returns the id RequestID put in ctx, or "".
func requestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// The two probe routes, as PATTERNS. HealthPath is registered by NewRouter in this
// package; ReadyPath is registered by internal/handler.Health, which is a different
// owner on purpose (M8-01) — so the literal is repeated here and
// internal/handler/health_test.go asserts the two spellings are the same string.
const (
	HealthPath = "/healthz"
	ReadyPath  = "/readyz"
)

// probeDesignedStatus is, per probe route, the set of statuses that route is
// DESIGNED to answer with. An answer inside the set is the system working; an
// answer outside it is a defect.
//
// 🔴 THIS TABLE IS WHY THE ACCESS RECORD SKIPS A HEALTHY PROBE, AND BOTH HALVES
// WERE MEASURED (M8-03 round 2, 2026-08-19). The first round logged every response,
// probes included, and that was wrong in three separate ways:
//
//  1. LIVENESS BECAME A FUNCTION OF THE LOG TARGET. With a slog.Handler whose
//     Handle sleeps 2 s, GET /healthz answered 200 after 2.001 s. In production
//     os.Stdout is a container log pipe; if the node cannot drain it the write
//     BLOCKS, the livenessProbe (timeoutSeconds 2, failureThreshold 3) fails, and
//     the kubelet kills a process that is perfectly healthy. That is precisely the
//     failure TestHealthz_CannotDependOnAnything is named for, reintroduced by the
//     observability card. Now /healthz -> 200 writes NOTHING, so there is nothing
//     on that path to block on.
//
//     ⚠️ NO SINGLE "AFTER" FIGURE IS FROZEN HERE, AND THAT IS THE CORRECTION OF A
//     MEASURED DRIFT: this comment published 0.6 ms while deploy/README.md published
//     1.1 ms for the SAME fact, and a third measurement gave 0.711 ms — one fact,
//     three numbers, because a wall-clock latency on a dev laptop is not a constant.
//     Re-measured across 21 consecutive requests against a target that stalls for 2 s:
//     0.23–0.82 ms, i.e. the probe does not touch the log target at all. The standing
//     proof is not the number but TestHealthz_AnswersWhileTheLogTargetIsUnavailable,
//     which recomputes the comparison (answer < stall) on every run.
//
//  2. /readyz's DESIGNED 503 FIRED THE "SERVER IS BROKEN" RULE. status >= 500 is
//     ERROR, and deploy/README.md's 5xx rule is "level=ERROR on http.request, more
//     than 5 in 5 minutes". The kubelet probes /readyz every 5 s, so a database
//     wobble produced 60 ERROR records per 5 minutes and tripped a threshold of 5
//     in about 25 seconds — while reporting the wrong cause.
//
//  3. VOLUME. 17 280 readiness + 8 640 liveness probes a day at ~197 bytes each is
//     ~5.1 MB/day of access records with ZERO user traffic, which is ~100% of the
//     log at rest. The runbook's retention arithmetic was computed against a pod
//     running the pre-M8-03 binary and therefore missed all of it.
//
// 🔴 AND WHAT KEEPS THE FAILURE VISIBLE — this is the half that makes the skip
// legitimate rather than convenient (§4.6: evidence is not thrown away):
//
//   - A GENUINE readiness failure is logged by the endpoint's OWNER, once per
//     transition, at both edges: internal/handler.Health emits EventReadinessLost
//     and EventReadinessRegained. deploy/README.md alerts on the lost event (rule 6).
//     Two nets over one path is what this repository refuses elsewhere (R7c's
//     comment in scripts/redline-check.sh); this is the same call.
//
//     ⚠️ IT IS A TRADE, NOT A STRICT IMPROVEMENT, AND THE SECOND HALF IS WRITTEN
//     DOWN. Better: one record per state change instead of 12 a minute, carrying the
//     database error, with a start AND an end. Narrower: the signal is tied to the
//     MOMENT the outage began, not to its duration. TestReadyz_AnOutageCostsTwoLogLines
//     measures 200 failed probes producing exactly ONE record, so an outage
//     that started before an operator's query window is invisible in that window,
//     and if that single line is rotated out the signal is gone entirely. This
//     endpoint does NOT repeat while failing; announceKEKRotationWindow in
//     cmd/tappa deliberately does repeat (every 15 min) for the same class of
//     continuing state. deploy/README.md's limit 28 carries the full accounting.
//
//   - An UNDESIGNED status is still recorded in full, at ERROR when it is a 5xx.
//     That is what the set is for rather than a bare "skip probes": if h.Ready
//     panics, Recoverer writes 500, 500 is not in /readyz's set, and the access
//     record fires exactly as it would on any other route. A blanket exclusion
//     would have made a panicking readiness handler silent.
//
//   - /healthz cannot fail and log about it — a process that cannot answer HTTP
//     cannot write either. That failure is reported by the kubelet, and no record
//     is lost by not attempting one.
var probeDesignedStatus = map[string]map[int]bool{
	HealthPath: {http.StatusOK: true},
	ReadyPath:  {http.StatusOK: true, http.StatusServiceUnavailable: true},
}

// ProbeRoutes returns the route patterns whose designed answers are kept out of
// the access record. It exists so internal/handler can assert that the pattern it
// registers for readiness is the one this package excludes — the two live in
// different packages by design, and a silent disagreement would put every
// readiness probe back into the log without anything noticing.
func ProbeRoutes() []string {
	out := make([]string, 0, len(probeDesignedStatus))
	for route := range probeDesignedStatus {
		out = append(out, route)
	}
	return out
}

// probeAnsweredAsDesigned reports whether this is a probe route answering the way
// it is built to.
func probeAnsweredAsDesigned(route string, status int) bool {
	return probeDesignedStatus[route][status]
}

// WithRequestID wraps h so a record logged with a *Context method gains
// request_id when the context carries this package's request id.
//
// It is a Handler wrapper rather than a logger attribute because the id is
// per-REQUEST and the logger is per-PROCESS: cmd/tappa builds one *slog.Logger at
// start-up and injects it into every service, so there is no point at which a
// With("request_id", …) could be attached that would still be right on the next
// request.
func WithRequestID(h slog.Handler) slog.Handler {
	if h == nil {
		return nil
	}
	return requestIDHandler{Handler: h}
}

type requestIDHandler struct{ slog.Handler }

func (h requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	id := requestIDFrom(ctx)
	if id == "" {
		return h.Handler.Handle(ctx, r)
	}
	// Clone before adding: slog.Record shares its attribute backing array with
	// copies, so AddAttrs on the value we were handed can write into a record
	// another handler in a fanout still holds. Handler.Handle's contract says a
	// handler should not modify the record it is given.
	c := r.Clone()
	c.AddAttrs(slog.String(LogRequestIDKey, id))
	return h.Handler.Handle(ctx, c)
}

// WithAttrs and WithGroup have to re-wrap. Without them, logger.With(...) returns
// the INNER handler and every record from that derived logger loses its
// request_id — silently, because a missing attribute looks exactly like a request
// that had no id.
func (h requestIDHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return requestIDHandler{Handler: h.Handler.WithAttrs(as)}
}

func (h requestIDHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return requestIDHandler{Handler: h.Handler.WithGroup(name)}
}

// AccessLog emits exactly one record per response.
//
// 🔴 IT LOGS THE ROUTE PATTERN AND NEVER THE URL, AND THAT IS A §4.7 DECISION
// RATHER THAN BREVITY. This product's two most sensitive credentials travel in the
// QUERY STRING: the plaque's SUN signature (internal/sun/params.go: tag, ctr,
// cmac) and the activation invite code (internal/handler/activate.go reads
// ?code=). r.URL.RequestURI(), r.URL.String() and httputil-style "%s %s" logging
// would put a live CMAC and a live invite code into a log that has no retention
// bound and no tenant boundary — two entries of §4.7's never-log list, in the one
// line that runs on every request.
//
// chi's RoutePattern() is the opposite kind of value: it is one of the literal
// patterns registered in this process, so the set of things this field can ever
// contain is fixed at compile time and contains no user input at all. A path
// VARIABLE (/admin/employees/{id}) stays a brace, not an id.
//
// It also never logs the client address. httpx.ClientIP is evidence the decision
// engine scores (§5) and it is written to the transactions row; repeating it on
// every request would build a second, unbounded copy of a movement history in a
// place GDPR erasure (Q13) cannot reach.
//
// LEVEL CARRIES THE SIGNAL: 5xx is Error, 4xx is Info, the rest is Info. An
// operator filtering level=ERROR gets exactly the responses that are this
// server's fault, which is what the M8-03 5xx rule keys on. A probe route
// answering as designed is not written at all — probeDesignedStatus above has the
// measurement and the three failures that motivated it.
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	// Per-middleware rather than package level (§7 forbids the package singleton),
	// and it is what keeps the warning below from becoming its own flood.
	var warnedOnce sync.Once
	return func(next http.Handler) http.Handler {
		if log == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				status := ww.Status()
				if status == 0 {
					// Nothing wrote a header: net/http will send 200.
					status = http.StatusOK
				}
				route := routeOf(r)
				if probeAnsweredAsDesigned(route, status) {
					return
				}
				level := slog.LevelInfo
				if status >= 500 {
					level = slog.LevelError
				}
				writeAccessRecord(&warnedOnce, log, r.Context(), level, status, r.Method, route, time.Since(start))
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// writeAccessRecord emits the record and CONTAINS a panic raised by the log target
// itself.
//
// 🔴 WITHOUT THIS, A PANICKING slog.Handler KILLED THE CONNECTION ON EVERY ROUTE
// (measured, M8-03 round 2): GET /healthz and GET /probe both answered `EOF`, the
// stack ending in net/http.(*conn).serve — NOT in middleware.Recoverer. That is not
// an accident of ordering, it is the cost of the ordering: AccessLog is mounted
// OUTSIDE Recoverer on purpose (router.go argues why), so nothing downstream can
// recover what AccessLog itself raises. A middleware that turns a logging defect
// into a dropped connection is worse than the missing record it was added to
// provide, so the one call that sits outside the net brings its own.
//
// ⚠️ IT DOES NOT SWALLOW THE HANDLER'S PANIC, AND THAT WAS MEASURED RATHER THAN
// ASSUMED, because Go's rule here is genuinely surprising: a recover() in a
// deferred function of a helper that RETURNS NORMALLY does not stop a panic that is
// already unwinding an outer frame. Probed on this toolchain with all three
// combinations — handler panics / target panics / both — the handler's panic
// reaches Recoverer every time and only the target's own panic is caught here.
// TestAccessLog_PanicIsRecordedAsA5xx is the standing proof.
//
// ⚠️ COUNTED LIMIT: the dropped record is GONE. There is no second sink to write it
// to — the process logger is the thing that just failed — so the warning goes
// straight to stderr and says so. The panic VALUE is not printed: it comes from a
// handler that was part-way through formatting a record and can therefore carry
// whatever the caller was logging (§4.7).
//
// ⚠️ AND THE WARNING'S SCOPE IS PER MIDDLEWARE INSTALLATION, NOT PER PROCESS. The
// text said "once per process" for a round; measured, two AccessLog installations in
// one process print it TWICE, because warnedOnce is a local of the AccessLog call
// (§7 forbids the package-level singleton that would make the claim true). The
// sentence was lowered to the mechanism rather than the mechanism raised to the
// sentence, because the alternative is a package singleton this repository refuses.
// cmd/tappa mounts exactly one router, so in this binary the two coincide — but
// that is a property of the caller, and it is now said as one.
// TestAccessLog_ADroppedRecordIsAnnouncedOnce measures both halves.
func writeAccessRecord(warnedOnce *sync.Once, log *slog.Logger, ctx context.Context,
	level slog.Level, status int, method, route string, took time.Duration,
) {
	defer func() {
		if rec := recover(); rec != nil {
			warnedOnce.Do(func() {
				fmt.Fprintln(os.Stderr, "httpx: the slog handler PANICKED while writing an access "+
					"record. That record is lost and this warning prints once per AccessLog "+
					"middleware installation, not once per process; the response itself was "+
					"unaffected. The panic value is withheld deliberately.")
			})
		}
	}()
	log.LogAttrs(ctx, level, EventHTTPRequest,
		slog.Int(LogStatusKey, status),
		slog.String(LogMethodKey, method),
		slog.String(LogRouteKey, route),
		slog.Int64(LogDurationMSKey, took.Milliseconds()),
	)
}

// routeOf returns the chi pattern this request matched, or routeUnmatched.
//
// The deferred read is deliberate: chi fills RouteContext DURING routing, so this
// is only populated once next.ServeHTTP has returned. Reading it before would give
// the empty string on every request — which would still compile, still log, and
// quietly make the route field useless.
func routeOf(r *http.Request) string {
	rc := chi.RouteContext(r.Context())
	if rc == nil {
		return routeUnmatched
	}
	if p := rc.RoutePattern(); p != "" {
		return p
	}
	return routeUnmatched
}
