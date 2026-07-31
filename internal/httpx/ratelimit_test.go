package httpx

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/session"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// recorder counts audit events without a database. The tap limiter's promise is
// about HOW MANY rows a refused caller can cause, so counting is the measurement.
type recorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorder) Record(_ context.Context, e audit.Event) (uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return uuid.New(), nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func TestLimiter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	l := NewLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := 1; i <= 3; i++ {
		if got := l.Charge("k"); got != i {
			t.Fatalf("charge %d returned %d", i, got)
		}
		if !l.Allowed("k") && i < 3 {
			t.Fatalf("key refused at %d of 3", i)
		}
	}
	if l.Allowed("k") {
		t.Fatal("key still allowed after spending the budget")
	}
	// The transition fires exactly once, which is what bounds writes into an
	// append-only table.
	overs := 0
	for i := 0; i < 5; i++ {
		if l.FirstOverLimit(l.Charge("k")) {
			overs++
		}
	}
	if overs != 1 {
		t.Fatalf("FirstOverLimit fired %d times in one window, want 1", overs)
	}
	// A different key is a different caller.
	if !l.Allowed("other") {
		t.Fatal("an untouched key inherited another key's window")
	}
	// The window rolls.
	now = now.Add(time.Minute)
	if !l.Allowed("k") {
		t.Fatal("the window did not roll")
	}
}

func TestLimiter_EvictsWhenTheMapIsFull(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)
	l := NewLimiter(1, time.Minute)
	l.now = func() time.Time { return now }
	l.maxKeys = 10

	for i := 0; i < 10; i++ {
		l.Charge("old" + strconv.Itoa(i))
	}
	now = now.Add(2 * time.Minute) // every window above is now expired
	l.Charge("fresh")
	if len(l.windows) > 2 {
		t.Fatalf("expired windows were not evicted: %d live", len(l.windows))
	}
	// And when nothing is expired, the map is reset wholesale rather than
	// growing without bound. Resetting forgets counts — the fail-OPEN direction,
	// chosen so one attacker cannot lock everyone out. Measured, not assumed.
	for i := 0; i < 10; i++ {
		l.Charge("live" + strconv.Itoa(i))
	}
	l.Charge("one-more")
	if len(l.windows) > 10 {
		t.Fatalf("the map grew past maxKeys: %d", len(l.windows))
	}
}

// ---------------------------------------------------------------- tap limiter --

func tapProbe() (http.Handler, *int) {
	served := 0
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served++
		w.WriteHeader(http.StatusOK)
	}), &served
}

func tapRequest(peer string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/t", nil)
	r.RemoteAddr = peer
	return r
}

func TestTapLimiter_ByAddress(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	tl := NewTapLimiter(TapLimitParams{Audit: rec, Log: quietLog(), AddressLimit: 3})
	next, served := tapProbe()
	h := RealIP(nil)(tl.ByAddress(next))

	for i := 1; i <= 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, tapRequest("203.0.113.9:5000"))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d of 3 answered %d", i, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, tapRequest("203.0.113.9:5000"))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("the 4th request answered %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("no Retry-After on a 429")
	}
	if *served != 3 {
		t.Fatalf("%d requests reached the handler, want 3", *served)
	}
	// A refusal with no identity CANNOT be audited: audit_log.tenant_id is NOT
	// NULL and no tenant is known before Identify runs. This is the stated limit,
	// measured rather than claimed.
	if rec.count() != 0 {
		t.Fatalf("%d audit rows written with no tenant known", rec.count())
	}
	// Another address is another caller.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, tapRequest("198.51.100.7:5000"))
	if w.Code != http.StatusOK {
		t.Fatalf("a different address was refused with %d", w.Code)
	}
}

// TestTapLimiter_ByAddressUsesTheResolvedClient is the whole point of pairing the
// limiter with RealIP: two phones behind one reverse proxy must NOT share a
// budget, and a caller must not be able to pick its own budget.
func TestTapLimiter_ByAddressUsesTheResolvedClient(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	tl := NewTapLimiter(TapLimitParams{Log: quietLog(), AddressLimit: 2})
	next, _ := tapProbe()
	h := RealIP(trusted)(tl.ByAddress(next))

	send := func(xff string) int {
		r := tapRequest("10.0.0.1:9000")
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	// Client A spends its budget.
	send("203.0.113.9")
	send("203.0.113.9")
	if got := send("203.0.113.9"); got != http.StatusTooManyRequests {
		t.Fatalf("client A over budget answered %d, want 429", got)
	}
	// Client B, same proxy, is untouched. Before M5-03 both shared one key.
	if got := send("198.51.100.7"); got != http.StatusOK {
		t.Fatalf("client B answered %d, want 200", got)
	}
	// And A cannot buy a fresh budget by prepending a forged hop: the walk is
	// right to left, so only what our trusted proxy appended counts.
	if got := send("9.9.9.9, 203.0.113.9"); got != http.StatusTooManyRequests {
		t.Fatalf("a forged left-hand hop bought a new budget: %d", got)
	}
}

func TestTapLimiter_BySession(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	tl := NewTapLimiter(TapLimitParams{Audit: rec, Log: quietLog(), SessionLimit: 2})
	next, served := tapProbe()

	tenant, employee := uuid.New(), uuid.New()
	one := session.Resolved{ID: uuid.New(), TenantID: tenant, EmployeeID: employee}
	two := session.Resolved{ID: uuid.New(), TenantID: tenant, EmployeeID: uuid.New()}

	// The identity is injected the way Identify would, so the test measures the
	// limiter and not the resolver.
	withIdentity := func(id Identity) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tl.BySession(next).ServeHTTP(w, r.WithContext(
				context.WithValue(r.Context(), identityKey{}, id)))
		})
	}

	live := withIdentity(Identity{State: SessionLive, Session: one})
	for i := 1; i <= 2; i++ {
		w := httptest.NewRecorder()
		live.ServeHTTP(w, tapRequest("203.0.113.9:5000"))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d of 2 answered %d", i, w.Code)
		}
	}
	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		live.ServeHTTP(w, tapRequest("203.0.113.9:5000"))
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("over-budget request answered %d, want 429", w.Code)
		}
	}
	// A tenant IS known here, so the refusal is auditable — and it is written
	// ONCE per window, not once per refused request. Twenty refusals, one row:
	// audit_log cannot be deleted from, so an unbounded refusal path would be a
	// cheaper attack than the endpoint it protects (M5-02, blocking finding #1).
	if got := rec.count(); got != 1 {
		t.Fatalf("%d audit rows for 20 refusals, want exactly 1", got)
	}
	if e := rec.events[0]; e.Action != ActionTapRateLimited || e.TenantID != tenant || e.Target != employee.String() {
		t.Fatalf("audit event = %+v", e)
	}

	// A different session is a different budget.
	other := withIdentity(Identity{State: SessionLive, Session: two})
	w := httptest.NewRecorder()
	other.ServeHTTP(w, tapRequest("203.0.113.9:5000"))
	if w.Code != http.StatusOK {
		t.Fatalf("a second session was refused with %d", w.Code)
	}

	// A request with NO session passes through unmetered here: it is already
	// metered by ByAddress, and one shared "anonymous" bucket is exactly the
	// lockout M5-02 measured.
	none := withIdentity(Identity{State: SessionAbsent})
	for i := 0; i < 50; i++ {
		w := httptest.NewRecorder()
		none.ServeHTTP(w, tapRequest("203.0.113.9:5000"))
		if w.Code != http.StatusOK {
			t.Fatalf("a session-less request was refused with %d", w.Code)
		}
	}
	if *served != 2+1+50 {
		t.Fatalf("%d requests reached the handler", *served)
	}
}

// TestTapLimiter_BySessionRefusesToRunWithoutIdentify is the enforcement of the
// mount order. An audit measured the version that only documented it: with
// Identify missing, 100 of 100 requests went through UNMETERED because the zero
// Identity has a nil session id. A contract nothing checks is a comment.
//
// The three sub-cases are the whole point of SessionState's polarity: only the
// UNRESOLVED one is a wiring fault. A session-less tap is legitimate (§5 row 3)
// and must still be served.
func TestTapLimiter_BySessionRefusesToRunWithoutIdentify(t *testing.T) {
	t.Parallel()
	tl := NewTapLimiter(TapLimitParams{Log: quietLog(), SessionLimit: 1000})
	next, served := tapProbe()

	tests := []struct {
		name     string
		wrap     func(*http.Request) *http.Request
		want     int
		wantNext bool
	}{
		{
			// Identify was never mounted: nothing resolved, so nothing is being
			// metered. Shedding is the fail-closed answer.
			name:     "no Identify in the chain",
			wrap:     func(r *http.Request) *http.Request { return r },
			want:     http.StatusInternalServerError,
			wantNext: false,
		},
		{
			// Identify ran and the database failed. Same reasoning: we do not
			// know who this is, so we do not pretend to meter them.
			name: "identity carried an error",
			wrap: func(r *http.Request) *http.Request {
				return r.WithContext(context.WithValue(r.Context(), identityKey{},
					Identity{Err: errNoDatabase}))
			},
			want:     http.StatusInternalServerError,
			wantNext: false,
		},
		{
			// A genuine session-less tap. This one MUST pass: §5 row 3 serves the
			// activation page, and confusing it with the case above would refuse
			// every first-time employee.
			name: "no session, resolved as absent",
			wrap: func(r *http.Request) *http.Request {
				return r.WithContext(context.WithValue(r.Context(), identityKey{},
					Identity{State: SessionAbsent}))
			},
			want:     http.StatusOK,
			wantNext: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := *served
			w := httptest.NewRecorder()
			tl.BySession(next).ServeHTTP(w, tc.wrap(tapRequest("203.0.113.9:5000")))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
			if reached := *served > before; reached != tc.wantNext {
				t.Fatalf("handler reached = %v, want %v", reached, tc.wantNext)
			}
		})
	}
}

var errNoDatabase = errors.New("connection refused")

// TestTapLimiter_RevokedSessionIsStillMetered: a revoked session resolves to a
// real tenant and employee, so it is both meterable and auditable. It must not
// slip past the budget just because it will be refused later.
func TestTapLimiter_RevokedSessionIsStillMetered(t *testing.T) {
	t.Parallel()
	revokedAt := time.Now()
	rec := &recorder{}
	tl := NewTapLimiter(TapLimitParams{Audit: rec, Log: quietLog(), SessionLimit: 1})
	next, _ := tapProbe()
	id := Identity{State: SessionRevoked, Session: session.Resolved{
		ID: uuid.New(), TenantID: uuid.New(), EmployeeID: uuid.New(), RevokedAt: &revokedAt,
	}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tl.BySession(next).ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
	h.ServeHTTP(httptest.NewRecorder(), tapRequest("203.0.113.9:5000"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, tapRequest("203.0.113.9:5000"))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("a revoked session was not metered: %d", w.Code)
	}
	if rec.count() != 1 {
		t.Fatalf("%d audit rows, want 1", rec.count())
	}
}

// TestTapLimiter_NilRecorderStillRefuses: the audit trail is optional, the
// shield is not.
func TestTapLimiter_NilRecorderStillRefuses(t *testing.T) {
	t.Parallel()
	tl := NewTapLimiter(TapLimitParams{Log: quietLog(), SessionLimit: 1})
	next, _ := tapProbe()
	id := Identity{State: SessionLive, Session: session.Resolved{
		ID: uuid.New(), TenantID: uuid.New(), EmployeeID: uuid.New(),
	}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tl.BySession(next).ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
	h.ServeHTTP(httptest.NewRecorder(), tapRequest("203.0.113.9:5000"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, tapRequest("203.0.113.9:5000"))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
}

// TestTapLimiter_DefaultsAreWideEnoughForAShiftChange guards the card's actual
// criterion — "a limit a legitimate tap can never reach" — against a future edit
// that tightens the numbers without redoing the arithmetic. 300 people clocking
// in at once, two requests each, plus a third for retries.
func TestTapLimiter_DefaultsAreWideEnoughForAShiftChange(t *testing.T) {
	t.Parallel()
	const crowd, requestsPerTap = 300, 3
	if crowd*requestsPerTap >= tapAddressLimit {
		t.Fatalf("tapAddressLimit=%d does not clear a %d-person shift change at %d requests each",
			tapAddressLimit, crowd, requestsPerTap)
	}
	// One phone: a reload every five seconds for the whole window is still legal.
	if int(tapSessionPeriod/(5*time.Second)) >= tapSessionLimit {
		t.Fatalf("tapSessionLimit=%d refuses a phone reloading every 5s", tapSessionLimit)
	}
}
