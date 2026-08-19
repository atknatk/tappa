package handler

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Health is READINESS: GET /readyz, the endpoint that answers "can this process
// actually serve a tap?" rather than "is this process alive?" (M8-01).
//
// 🔴 IT IS A SEPARATE TYPE FROM LIVENESS ON PURPOSE, AND THE SPLIT IS STRUCTURAL.
// /healthz is registered by internal/httpx.NewRouter, which is handed no database
// at all, so it cannot depend on one however the code changes. This type IS handed
// one — that is the whole difference between the two questions — and everything
// below exists to bound what that costs.
//
// 🔴 WHY IT NEEDS AN ARGUMENT AT ALL: internal/handler/marketing.go states, in
// writing, why /healthz and /static/* are unmetered — "a request here renders a
// fixed component tree and returns; it touches no pool, takes no lock and appends to
// no table". A readiness endpoint that queries the database BREAKS that sentence for
// its own URL: it is unauthenticated, and it would put a pool acquisition on a path
// any stranger can drive as fast as they like — the same pool a check-in shares.
// Measured on the shipped pgx pool: pgxpool.Ping acquires a connection and executes
// the empty query, so N requests would be N acquisitions.
//
// SO THE BUDGET IS A CACHE RATHER THAN A LIMITER, and that choice is measured too.
// httpx.Limiter keys a map by caller address and resets the whole map at 100k keys
// (marketing.go argues the same point for the landing page): it would spend memory
// proportional to the number of ATTACKERS to protect a resource whose cost is
// already collapsible. One cached outcome collapses it completely — the pool sees at
// most one probe per readyTTL no matter how many callers ask, so the answer costs
// O(1) memory and O(1) queries per second. The tests measure both halves: 200
// concurrent requests produce exactly one Ping.
//
// AND THE SAME CACHE BOUNDS THE PROCESS LOG, which marketing.go names as the fourth
// scarce thing after a security audit measured 200 cancelled page loads writing 200
// ERROR lines. Here the bound is stronger than the cache: a failure is logged when
// the OUTCOME CHANGES, so an hour-long outage under any amount of traffic is two
// lines — one when it starts and one when it ends.
//
// ⚠️ WHAT IT DOES NOT PROVE, stated because a health check that over-claims is worse
// than none. A successful probe means a connection was acquired and the server
// answered. It does NOT mean migrations are applied, RLS is enabled, or any table
// holds a row — and a readiness check CANNOT check those with this connection:
// internal/db.Ping records why a table read here would answer 0 rows on a healthy
// database (tappa_app is NOBYPASSRLS and there is no tenant context outside
// WithTenant), which is a check whose success proves nothing.
type Health struct {
	probe readinessProbe
	log   *slog.Logger

	// ttl is how long one outcome answers for. One second, chosen against the thing
	// that actually calls this URL: orchestrators and uptime monitors probe every
	// 5-30 s, so a real probe practically never gets a cached answer, while a flood
	// is collapsed to one query per second.
	ttl time.Duration
	// probeTimeout bounds the query itself. A database that has stopped answering
	// but not closed the socket is the failure mode this exists for: without a
	// deadline the request would sit on the router's 30 s timeout while HOLDING a
	// pool connection, i.e. the readiness check would consume the resource it is
	// reporting on.
	probeTimeout time.Duration
	// now is injected so the cache window is testable without sleeping.
	now func() time.Time

	mu      sync.Mutex
	probed  time.Time // zero: never probed
	err     error     // the last outcome
	failing bool      // what the last LOGGED state was
}

// readinessProbe is the ONE capability this endpoint needs, declared at the
// consumer (CLAUDE.md §7).
//
// 🔴 IT IS A FUNCTION AND NOT AN INTERFACE, AND A SECURITY AUDIT IS WHY. The first
// version declared `interface { Ping(ctx) error }`, which is a correct consumer-side
// interface and was measured as such — but an interface VALUE carries the whole
// object behind the method set. cmd/tappa satisfied it by passing *db.DB, so the
// concrete pool wrapper entered this package for the first time, and that wrapper
// also exposes the six context-less resolvers (internal/db/resolve.go) — the lookups
// that run with NO tenant context because they PRODUCE one. internal/handler already
// imports internal/db (adminreset.go), so nothing but discipline stood between a
// later edit and `h.probe.(*db.DB).GetAdminSessionByTokenHash(…)`.
//
// A FUNCTION VALUE CARRIES ONE BOUND METHOD AND NOTHING ELSE. There is no dynamic
// type to assert back to, so the capability is not forbidden — it is absent. The
// wiring reads `handler.NewHealth(data.Ping, …)`, and passing the pool itself does
// not compile, which is a stronger guarantee than any test: this task already
// measured the same shape working better than a test, in that a migration runner
// cannot be imported into cmd/tappa because goose is not a module dependency at all.
//
// WHAT IT STILL SAYS ABOUT THE ENDPOINT is unchanged: one call, taking a context and
// returning only an error. No pgx.Tx, no row, so /readyz cannot report on tenant data
// without this line, internal/db and this file's argument all changing together.
type readinessProbe func(ctx context.Context) error

// readyPath is the readiness endpoint. It must be the same string as
// httpx.ReadyPath, which is how the access record knows this route's 503 is a
// design rather than a defect — TestReady_TheProbeRouteIsTheOneHttpxExcludes pins
// the two together, because the packages are separate on purpose and a silent
// disagreement would put 17 280 records a day back into the log.
const readyPath = "/readyz"

// The ALERT INTERFACE of readiness (M8-03 round 2).
//
// 🔴 THESE TWO EVENTS ARE NOW THE ONLY LOG SIGNAL A READINESS OUTAGE PRODUCES, so
// they are pinned like the tap events are. httpx.AccessLog no longer writes an
// http.request record for /readyz's designed 200 and 503: the kubelet probes this
// URL every 5 s, which produced 60 ERROR records per 5 minutes during any database
// wobble and tripped the 5xx rule's threshold of 5 in roughly 25 seconds while
// naming the wrong cause (measured, requestlog.go has the numbers).
//
// What replaces it is strictly better and this is why the trade was taken: one
// record per STATE CHANGE rather than twelve per minute, carrying the driver error
// the response deliberately withholds, at both edges so an outage has a start and
// an end. deploy/README.md alerts on the lost event with a threshold of one.
//
// The message IS the event name, matching checkin.EventTapDecision — a rule keyed
// on a prose sentence breaks the first time somebody improves the prose. The prose
// that used to be here is now in the runbook row, where an operator reads it.
const (
	// EventReadinessLost: the database did not answer, so /readyz reports 503
	// until it does.
	EventReadinessLost = "readiness.lost"
	// EventReadinessRegained: the database answered again, so /readyz reports 200.
	EventReadinessRegained = "readiness.regained"
)

// The two bodies. They are constants and they are the ENTIRE response: see
// notReadyBody for why the failing one says so little.
const (
	readyBody = "ready"
	// 🔴 IT NAMES NO CAUSE, AND THAT IS THE §4.7 BOUNDARY OF THIS ENDPOINT. A driver
	// error names the host, the port, the user and the database — measured on this
	// product's own pgx: "failed to connect to `user=tappa_app database=tappa`:
	// 127.0.0.1:1 (127.0.0.1): dial error: connection refused". Handing that to an
	// unauthenticated caller is a free map of the deployment's private side.
	//
	// IT IS STRUCTURAL RATHER THAN A REDACTION. This handler never formats the error
	// into the response at all — there is no code path that could, so there is
	// nothing for a future driver's more talkative error to slip through. The
	// operator is not left in the dark: the cause goes to the process log, at most
	// once per outage, where it is already correlated by request id.
	//
	// 🔴 AND SINCE ROUND 4 THE LOG DOES NOT CARRY THE MAP EITHER. For one round this
	// paragraph argued the map was too much for an unauthenticated HTTP caller while
	// the state-change record below handed the identical text to a collector this
	// product does not operate. readinessFailure resolves that: the record carries a
	// classified failure and an address-free cause, and the argument here is now made
	// once rather than contradicted next door.
	notReadyBody = "not ready"
)

const (
	defaultReadyTTL      = 1 * time.Second
	defaultProbeTimeout  = 2 * time.Second
	readyContentTypeText = "text/plain; charset=utf-8"
)

// NewHealth builds the readiness endpoint.
func NewHealth(probe readinessProbe, log *slog.Logger) (*Health, error) {
	if probe == nil {
		// Fail at wiring rather than at the first probe: a /readyz that answered 503
		// for ever because nobody passed a probe would look exactly like a database
		// outage, and would be diagnosed as one. A nil func is comparable, so this
		// check is the same one line it was when the parameter was an interface.
		return nil, errors.New("handler: NewHealth: nil readiness probe")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Health{
		probe:        probe,
		log:          log,
		ttl:          defaultReadyTTL,
		probeTimeout: defaultProbeTimeout,
		now:          time.Now,
	}, nil
}

// Mount registers GET and HEAD /readyz.
//
// 🔴 HEAD IS MOUNTED FROM THE FIRST DAY THIS ENDPOINT EXISTS. Every chi r.Get in
// this product registers GET alone, so a HEAD arrives as 405 `Allow: GET` (backlog
// T29, measured across /healthz, /admin/login, /activate and /t) — and the clients
// that watch a readiness URL are precisely the ones that send HEAD. Being born with
// that defect would have meant an uptime monitor reporting the check as broken while
// the service was fine.
func (h *Health) Mount(r chi.Router) {
	r.Get(readyPath, h.Ready)
	r.Head(readyPath, h.Ready)
}

// Ready answers the readiness probe: 200 when the database answered, 503 when it
// did not.
//
// no-store on both answers. A cached "ready" is a proxy telling a deployment to send
// traffic to a process that has since lost its database, which is the one thing this
// endpoint exists to prevent.
func (h *Health) Ready(w http.ResponseWriter, r *http.Request) {
	err := h.check(r.Context())

	w.Header().Set("Content-Type", readyContentTypeText)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(notReadyBody))
		return
	}
	_, _ = w.Write([]byte(readyBody))
}

// check returns the current readiness, probing at most once per ttl.
//
// 🔴 THE PROBE DOES NOT INHERIT THE CALLER'S CANCELLATION, and that is a defence
// rather than a detail. If it did, a client that hung up mid-probe would hand this
// process a context.Canceled — which would be cached as "not ready" and logged as a
// database outage. Anybody could then flip the deployment's readiness, and fill its
// log, from a browser tab they close. context.WithoutCancel severs that: the
// probe is bounded by probeTimeout and by nothing the caller controls. The values
// it inherits (deadline-free, no request cancellation) are exactly right, because
// the answer belongs to the PROCESS and not to this request.
//
// The lock is held across the probe on purpose: concurrent callers queue behind one
// in-flight query rather than each starting their own, so a burst costs one
// acquisition and everybody after it reads the cache.
func (h *Health) check(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.probed.IsZero() && h.now().Sub(h.probed) < h.ttl {
		return h.err
	}

	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.probeTimeout)
	defer cancel()
	err := h.probe(probeCtx)
	h.probed = h.now()
	h.err = err

	// STATE CHANGES ARE LOGGED, STATES ARE NOT. What a human needs from this log is
	// when readiness turned over; the continuing state is what an alert rule watches
	// (M8-03). Logging every failed probe instead would put an unbounded, caller-
	// driven writer into the process log — the defect a security audit measured on
	// the marketing surface.
	switch {
	case err != nil && !h.failing:
		h.failing = true
		class, cause := readinessFailure(err)
		h.log.LogAttrs(ctx, slog.LevelError, EventReadinessLost,
			slog.String(LogErrClassKey, class), slog.String(LogErrCauseKey, cause))
	case err == nil && h.failing:
		h.failing = false
		h.log.InfoContext(ctx, EventReadinessRegained)
	}
	return err
}

// The two attributes EventReadinessLost carries. ALERT INTERFACE, like the event
// names above: deploy/README.md's troubleshooting row reads them by these spellings.
const (
	// LogErrClassKey names WHAT KIND of failure it was, from a closed set.
	LogErrClassKey = "err_class"
	// LogErrCauseKey is the most specific ADDRESS-FREE reason the driver gave.
	LogErrCauseKey = "err_cause"
)

// The closed set of err_class values. It is closed so an alert rule or a runbook
// row can enumerate it; readinessFailure is the only producer.
const (
	errClassServer   = "server"   // Postgres answered and refused — err_cause is the SQLSTATE
	errClassTimeout  = "timeout"  // the probe's own deadline expired
	errClassCanceled = "canceled" // the caller went away
	errClassDNS      = "dns"      // the host name did not resolve
	errClassDial     = "dial"     // the socket did not open
	errClassOther    = "other"    // classified by nothing above
)

// readinessFailure reduces a driver error to a class and an address-free cause.
//
// 🔴 IT EXISTS BECAUSE THIS FILE WAS SAYING TWO THINGS AT ONCE (M8-03 round 4).
// notReadyBody's comment above argues, at length, that handing a pgx error to an
// unauthenticated HTTP caller is "a free map of the deployment's private side" —
// and then the state-change record handed the SAME map to a log the product does
// not operate the storage of. Measured on this product's own pgx, all four
// realistic failures:
//
//	connection refused  failed to connect to `user=… database=…`: <host>:<port> …
//	name not resolved   failed to connect to `user=… database=…`: … <hostname> …
//	wrong password      … <host>:<port> … tls error: server refused TLS connection …
//	                    … failed SASL auth: … (SQLSTATE 28P01)
//	probe timed out     context deadline exceeded            <- the only clean one
//
// Role, database, host, port and the deployment's TLS posture, in a record that is
// shipped to a collector whose retention this product cannot verify. The password
// is NOT in there (measured twice, independently) — this is a map, not a key.
//
// ⚠️ WHAT THE OPERATOR LOSES, COUNTED HONESTLY. The driver's sentence is gone, so
// "connection refused" and "no route to host" both read `dial` + the syscall's own
// word, and a failure nothing below matches reads `other` with no cause at all.
// What is KEPT is what triage actually branches on: an auth or permission failure
// arrives as `server` plus its SQLSTATE (28P01 for a bad password, 3D000 for a
// missing database), a name that will not resolve as `dns`, a socket that will not
// open as `dial`, and the probe outliving its own deadline as `timeout`. The
// deployment's address is something the operator already has — it is in
// deploy/k8s/05-config.yaml and in the Secret — so this removes it from the party
// that has the LOG without having the config, which is a wider set than the party
// with kubectl.
func readinessFailure(err error) (class, cause string) {
	// Order matters: a Postgres-level refusal is the most specific thing that can be
	// known, and it arrives WRAPPED in a *pgconn.ConnectError, so asking about the
	// connection first would classify a bad password as a dial failure.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return errClassServer, pgErr.Code
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errClassTimeout, ""
	case errors.Is(err, context.Canceled):
		return errClassCanceled, ""
	}
	// DNSError.Err is the reason ("no such host"); DNSError.Name is the HOSTNAME and
	// is deliberately not read.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return errClassDNS, dnsErr.Err
	}
	// OpError.Err is the syscall's own word ("connect: connection refused"); the
	// OpError's Addr and Source are the map and are deliberately not read.
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Err != nil {
		return errClassDial, opErr.Err.Error()
	}
	return errClassOther, ""
}
