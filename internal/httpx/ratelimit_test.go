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

// TestLimiter_TryChargeDecidesAsAllowedThenChargeDid pins that the atomic step is
// a drop-in for the two-step shape it replaces (M10 EM-5A, 2nd round): over one
// window, its expiry and a second window, a TryCharge limiter and an
// Allowed-then-Charge twin give the same decision and the same count for every
// event, sequentially.
func TestLimiter_TryChargeDecidesAsAllowedThenChargeDid(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	one, two := NewLimiter(3, time.Minute), NewLimiter(3, time.Minute)
	one.now = func() time.Time { return now }
	two.now = one.now
	for i := 0; i < 14; i++ {
		if i == 7 {
			now = now.Add(time.Minute) // the window rolls mid-sequence
		}
		within, n := one.TryCharge("k")
		allowed := two.Allowed("k")
		m := two.Charge("k")
		if within != allowed || n != m {
			t.Fatalf("event %d: TryCharge said (%v, %d), Allowed+Charge said (%v, %d)", i, within, n, allowed, m)
		}
	}
}

// TestLimiter_TryChargeIsExactUnderConcurrency is the property the two-step shape
// lacked: 200 goroutines charging one key of a budget of 10 at once get EXACTLY 10
// "within" answers, and the one charge that crossed the line (FirstOverLimit) went to
// a REFUSED caller — which is the caller that writes the single "rate limited" row.
// Repeated, with -race in CI's run, because a race is a probability.
func TestLimiter_TryChargeIsExactUnderConcurrency(t *testing.T) {
	t.Parallel()
	for run := 0; run < 50; run++ {
		l := NewLimiter(10, time.Minute)
		var (
			mu            sync.Mutex
			within, overs int
			overRefused   bool
			wg            sync.WaitGroup
			start         = make(chan struct{})
		)
		for g := 0; g < 200; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				ok, n := l.TryCharge("k")
				mu.Lock()
				defer mu.Unlock()
				if ok {
					within++
				}
				if l.FirstOverLimit(n) {
					overs++
					overRefused = !ok
				}
			}()
		}
		close(start)
		wg.Wait()
		if within != 10 || overs != 1 || !overRefused {
			t.Fatalf("run %d: %d within (want 10), %d first-over (want 1), first-over refused: %v (want true)",
				run, within, overs, overRefused)
		}
	}
}

// TestLimiter_ReserveDecidesAsAllowedThenChargeDid pins that a reservation is a drop-in for the
// two-step shape it replaces (backlog T90) WHEN EVENTS ARE SEQUENTIAL: a twin limiter driven the old
// way -- Allowed before the work, Charge on a refusal and on a chargeable outcome -- gives the same
// decision and the same count for every event, over one window, its expiry and a second window, with
// chargeable and free outcomes interleaved. Only concurrency tells them apart
// (TestLimiter_ReserveIsExactUnderConcurrency).
func TestLimiter_ReserveDecidesAsAllowedThenChargeDid(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	one, two := NewLimiter(3, time.Minute), NewLimiter(3, time.Minute)
	one.now = func() time.Time { return now }
	two.now = one.now
	// chargeable[i]: whether event i turns out to count (a failure) or to be free (a success).
	chargeable := []bool{true, false, true, false, false, true, true, true, false, true, true, false, true, true, true, true}
	for i, counts := range chargeable {
		if i == 9 {
			now = now.Add(time.Minute) // the window rolls mid-sequence
		}
		res, within, first, _ := one.Reserve("k")
		allowed := two.Allowed("k")
		if within != allowed {
			t.Fatalf("event %d: Reserve said within=%v, Allowed said %v", i, within, allowed)
		}
		switch {
		case !within:
			if m := two.Charge("k"); first != two.FirstOverLimit(m) {
				t.Fatalf("event %d: Reserve said first=%v, FirstOverLimit said %v", i, first, two.FirstOverLimit(m))
			}
		case counts:
			if !res.Keep() {
				t.Fatalf("event %d: Keep found nothing to keep", i)
			}
			two.Charge("k")
		default:
			res.Refund()
		}
		if a, b := one.windows["k"].count, two.windows["k"].count; a != b {
			t.Fatalf("event %d: counts %d (Reserve) and %d (Allowed+Charge)", i, a, b)
		}
	}
}

// TestLimiter_ReserveIsExactUnderConcurrency is the property the two-step shape lacked: 200 goroutines
// reserving one key of a budget of 10 at once, holding the unit for a moment (the request's work) and
// then keeping it or giving it back. At no instant are more than 10 units held; exactly one refusal is
// told it is the first; and when every outcome is chargeable exactly 10 are inside the budget.
// Repeated, with -race in CI's run, because a race is a probability.
func TestLimiter_ReserveIsExactUnderConcurrency(t *testing.T) {
	t.Parallel()
	for _, keepAll := range []bool{true, false} {
		for run := 0; run < 30; run++ {
			l := NewLimiter(10, time.Minute)
			var (
				mu                    sync.Mutex
				held, maxHeld, within int
				firsts                int
				wg                    sync.WaitGroup
				start                 = make(chan struct{})
			)
			for g := 0; g < 200; g++ {
				wg.Add(1)
				go func(g int) {
					defer wg.Done()
					<-start
					res, ok, first, _ := l.Reserve("k")
					mu.Lock()
					if first {
						firsts++
					}
					if ok {
						within++
						held++
						if held > maxHeld {
							maxHeld = held
						}
					}
					mu.Unlock()
					if !ok {
						return
					}
					time.Sleep(time.Millisecond)
					mu.Lock()
					held--
					mu.Unlock()
					if keepAll || g%2 == 0 {
						res.Keep()
					} else {
						res.Refund()
					}
				}(g)
			}
			close(start)
			wg.Wait()
			if maxHeld > 10 || firsts != 1 {
				t.Fatalf("keepAll=%v run %d: %d units held at once (budget 10), %d first refusals (want 1)",
					keepAll, run, maxHeld, firsts)
			}
			if keepAll && within != 10 {
				t.Fatalf("run %d: %d within the budget, want exactly 10", run, within)
			}
		}
	}
}

// TestLimiter_RefundGivesBackOnlyItsOwnUnit: a refund decrements its own window once, never a later
// window, never below zero; Keep after a refund (or a second refund) settles nothing.
func TestLimiter_RefundGivesBackOnlyItsOwnUnit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	l := NewLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	a, _, _, _ := l.Reserve("k")
	b, _, _, _ := l.Reserve("k")
	a.Refund()
	a.Refund()
	if got := l.windows["k"].count; got != 1 {
		t.Fatalf("count after one refund (twice) = %d, want 1", got)
	}
	if a.Keep() {
		t.Fatal("Keep after Refund settled the reservation again")
	}
	now = now.Add(time.Minute) // a new window
	c, _, _, _ := l.Reserve("k")
	b.Refund() // b's window is gone: nothing of the new one may be given back
	if got := l.windows["k"].count; got != 1 {
		t.Fatalf("an old reservation's refund moved the new window: count %d, want 1", got)
	}
	if !c.Keep() || c.Keep() {
		t.Fatal("Keep must settle an unsettled reservation exactly once")
	}
	var none *Reservation
	none.Refund()
	if none.Keep() {
		t.Fatal("a nil reservation has nothing to keep")
	}
}

// TestLimiter_ReserveReportsTheCrossingOncePerWindowAcrossRefunds pins the window flag behind Reserve's
// `first` (the 2026-11-07 audit: a `count == limit+1` version passed every other test). 10 reserved, the
// 11th refused and told it is the first; 5 refunded, 4 reserved again (count back at 10), one more
// refused at count 11 again -- and told it is NOT the first.
func TestLimiter_ReserveReportsTheCrossingOncePerWindowAcrossRefunds(t *testing.T) {
	t.Parallel()
	l := NewLimiter(10, time.Hour)
	var held []*Reservation
	for i := 0; i < 10; i++ {
		res, within, first, _ := l.Reserve("k")
		if !within || first {
			t.Fatalf("reservation %d: within=%v first=%v", i, within, first)
		}
		held = append(held, res)
	}
	if _, within, first, n := l.Reserve("k"); within || !first || n != 11 {
		t.Fatalf("11th: within=%v first=%v count=%d, want a refusal that is the first, at 11", within, first, n)
	}
	for _, res := range held[:5] {
		res.Refund()
	}
	for i := 0; i < 4; i++ {
		if _, within, _, _ := l.Reserve("k"); !within {
			t.Fatalf("re-reservation %d after the refunds was refused", i)
		}
	}
	if _, within, first, n := l.Reserve("k"); within || first || n != 11 {
		t.Fatalf("second crossing: within=%v first=%v count=%d, want a refusal at 11 that is NOT the first", within, first, n)
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
	if w.Result().Header.Get("Retry-After") == "" {
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
	// A tap is the page and the button; once the tap screen draws the business's logo
	// (M10 WL-6 / WL-9, ADR 0024 §5) a phone without it cached also fetches
	// GET /t/logo/{sha}, which runs this limiter too. One retry on top.
	const crowd = 300
	const page, button, coldLogo, retry = 1, 1, 1, 1
	if requestsPerTap := page + button + coldLogo + retry; crowd*requestsPerTap >= tapAddressLimit {
		t.Fatalf("tapAddressLimit=%d does not clear a %d-person shift change at %d requests each",
			tapAddressLimit, crowd, requestsPerTap)
	}
	// One phone: a reload every five seconds for the whole window is still legal, even
	// with its cache off, so that every reload pays for the logo again.
	if reloads := int(tapSessionPeriod / (5 * time.Second)); reloads*(page+coldLogo) >= tapSessionLimit {
		t.Fatalf("tapSessionLimit=%d refuses a phone reloading every 5s with its cache off (%d requests)",
			tapSessionLimit, reloads*(page+coldLogo))
	}
}
