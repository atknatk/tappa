package adminauth

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// workgate.go -- the PROCESS-WIDE ceiling on password work (backlog T86).
//
// Every bcrypt this package does -- the sign-in's 8 or 1 + 8 comparisons, the account page's
// re-proof, a password change, a reset, a registration's draft, its confirmation, an invitation's
// acceptance -- runs inside one slot of ONE WorkGate, built at wiring and shared by Manager, Resets,
// SignupClaims and AdminInvites (WithWorkGate). Every budget in front of those paths is keyed per
// address, per account or per link (internal/handler/adminratelimit.go), so a caller with many
// addresses could run as many cost-12 comparisons at once as it had requests in flight, and the tap
// surface shares this process: CPU a panel flood takes is CPU a tap cannot have.
//
// THE NUMBERS (measured 2026-11-07 on the development workstation, 16 cores, go1.26.10, no -race, a
// shared machine at load ~46-53; scratch program, not committed):
//
//	one cost-12 comparison                            308-412 ms (5 samples)
//	tap-sized CPU job (2 000 SHA-256), GOMAXPROCS=2, 6 s each, two runs:
//	  no bcrypt running                               p99 1.02-1.07 ms   max 1.1-1.5 ms
//	  1 bcrypt loop                                   p99 1.06 ms        max 1.1-4.0 ms
//	  2 bcrypt loops                                  p99 24-32 ms       max 30-133 ms
//	  8 bcrypt loops                                  p99 39-128 ms      max 134-195 ms
//
// So bcrypt that leaves one P free costs the tap nothing measurable and bcrypt on every P costs it
// tens of milliseconds at p99 and over 100 ms at the tail. The production pod is limited to 2 CPUs
// (deploy/k8s/20-app.yaml, read) and Go since 1.25 sets GOMAXPROCS from that limit (release notes,
// read; not measured on the pod), hence WorkSlots: half the Ps, at least one -- 1 slot in production.
//
// A SLOT IS ONE OPERATION, NOT ONE COMPARISON. Each gated method enters once, before it reads
// anything, and its comparisons run sequentially on one goroutine, so K slots are at most K busy
// cores. Entering per comparison would let a sign-in stall half-way, with a timing that depended on
// how many comparisons it had already made.
//
// 🔴 THE WAIT AND THE REFUSAL SAY NOTHING ABOUT THE ADDRESS (ADR 0031 §5.6). The gate is entered
// FIRST -- before the address is looked up, before any budget that reads a database -- so how long a
// request waits, and whether it is refused, depends only on what else is running, never on what was
// typed. A refusal is ErrBusy, decided in one of two time classes, both independent of the input:
// at once (the queue is full, or the request's source already has two in line) or after WorkWait. The
// handler answers it with one fixed page.
//
// ONE GLOBAL PLACE PER SOURCE (the security audit's finding, 2026-11-07): a request carries its client
// address bucket (WithWorkSource; httpx.RealIP sets it from httpx.RateKey for every request -- an IPv4
// address, an IPv6 /64), and a source holds at most ONE place in the gate, running or waiting in the
// global queue. Without the rule (denetimde, a count, not a measurement) two sources with their own
// right password kept the slot busy and three more the queue, refusing every sign-in, reset,
// registration, acceptance and password change in every business. With it, keeping the gate full takes
// 1 + workQueuePerSlot = 3 sources per slot (ADR 0039).
//
// ITS SECOND REQUEST WAITS IN THE SOURCE'S OWN LINE, OUTSIDE THE GLOBAL QUEUE (the third eye,
// 2026-11-07): the common second request is one person's double submit, two tabs or a mobile resend,
// and refusing it showed "busy" while the first one signed the person in (reasoned, not measured in a
// browser). So a source has at most one request in the gate and one waiting for that one's turn -- a
// place that holds no slot and no queue place -- and its third is refused at once (the queue-full time
// class, decided from the address the request came FROM, never from the address typed). Everything
// waits at most WorkWait in all.
//
// WHAT IT DOES NOT DO, named: it is not a per-caller budget -- a flood from enough addresses fills it
// and a legitimate sign-in is then refused with "busy, try again" (that is the trade: the panel's
// sign-in sheds load so the tap does not). Two people behind ONE address (an office NAT) signing in at
// the same moment: the second waits for the first; a third is refused at once and retries. And THE SLOT IS HELD FOR THE WHOLE
// OPERATION, its database round trips and the identity decoy's wait included (AuthenticatePerson,
// freshIdentitySession, ChangeOwnPassword): with one slot, a slow database queues every panel sign-in
// behind it. It does not cover the operator surface
// (internal/operatorauth: its own process-wide enrollment budget and per-address work budget, and an
// address restriction at the ingress). And a struct built without a constructor has a nil gate, which
// gates nothing (only this package's own tests build one).

// ErrBusy is every gated method's refusal when no slot came free in time (or the request ended while
// it waited). Nothing was compared, written or charged to the caller's account.
var ErrBusy = errors.New("adminauth: the password work is at capacity")

// WorkWait is how long a request waits for a slot. A waiter waits for WHOLE operations ahead of it:
// a sign-in holds its slot for 1 + MaxCandidates comparisons, ~2.8-3.7 s at the readings above (more
// under load), not for one comparison. The server sets no WriteTimeout (cmd/tappa), so the wait is the
// gate's alone.
const WorkWait = 10 * time.Second

// signInHold is how long one sign-in holds a slot at this package's pinned security budget for a
// cost-12 comparison (380 ms; adminratelimit.go sizes the panel's budgets on it): 1 + MaxCandidates of
// them, 3.42 s.
const signInHold = (1 + MaxCandidates) * 380 * time.Millisecond

// workQueuePerSlot bounds how many requests may WAIT per slot: as many as WorkWait can serve behind a
// running sign-in, WorkWait / signInHold = 2. A longer queue only turns into refusals after the whole
// wait, each holding a goroutine and a connection. MEASURED (2026-11-07, this workstation, no -race,
// load ~70, one slot, nine 8-comparison cost-12 sign-ins sent 30 ms apart): the running one answered at
// 4.47 s, the first waiter at 9.08 s, the second at 13.85 s (admitted at 9.08), the other six were refused
// after 10.1-10.3 s -- two waiters served, as derived. Beyond the queue a request is refused at once.
const workQueuePerSlot = int(WorkWait / signInHold)

// WorkSlots is the production slot count: half of GOMAXPROCS, at least one (see the measurement
// above: one P left to the tap).
func WorkSlots() int {
	if n := runtime.GOMAXPROCS(0) / 2; n > 1 {
		return n
	}
	return 1
}

// workWarnEvery spaces the refusal's WARN line: a flood must not become an unbounded log writer.
const workWarnEvery = time.Minute

// WorkGate is the semaphore. Its slots are a field, never package state (CLAUDE.md §7).
type WorkGate struct {
	slots chan struct{}
	queue chan struct{}
	// held is each source's own line (sourceLine, enter); an entry is deleted when its line is empty.
	mu   sync.Mutex
	held map[string]*sourceLine
	wait time.Duration
	log  *slog.Logger
	// lastWarn is the unix second of the last refusal line; refusals counts since then.
	lastWarn atomic.Int64
	refusals atomic.Int64
}

// NewWorkGate builds a gate of slots concurrent operations, slots x workQueuePerSlot waiters and
// wait per waiter. Production wiring passes WorkSlots() and WorkWait.
func NewWorkGate(slots int, wait time.Duration, log *slog.Logger) (*WorkGate, error) {
	if slots < 1 {
		return nil, errors.New("adminauth: work gate needs at least one slot")
	}
	if wait < 0 {
		return nil, errors.New("adminauth: work gate wait must not be negative")
	}
	return newWorkGate(slots, wait, log), nil
}

func newWorkGate(slots int, wait time.Duration, log *slog.Logger) *WorkGate {
	if log == nil {
		log = slog.Default()
	}
	return &WorkGate{
		slots: make(chan struct{}, slots),
		queue: make(chan struct{}, slots*workQueuePerSlot),
		held:  map[string]*sourceLine{},
		wait:  wait,
		log:   log,
	}
}

type workSourceKey struct{}

// WithWorkSource names the request's source for the gate's per-source line: the client
// address bucket (httpx.RateKey), set by httpx.RealIP for every request. A context without one is
// admitted without the rule (a test, a caller outside the router).
func WithWorkSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, workSourceKey{}, source)
}

// WorkSourceOf reports the source WithWorkSource put in ctx ("" for none).
func WorkSourceOf(ctx context.Context) string { return workSource(ctx) }

func workSource(ctx context.Context) string {
	s, _ := ctx.Value(workSourceKey{}).(string)
	return s
}

// sourceLine is one source's own line in front of the gate: its turn (a one-place token: whoever holds
// it runs or waits in the global queue) and how many of its requests are in line, at most two -- the one
// holding the turn and one waiting for it OUTSIDE the global queue (the 2026-11-07 third eye: a double
// submit, two tabs or a mobile resend must wait for the first, not be shown "busy" while the first signs
// the person in and its cookie is lost).
type sourceLine struct {
	turn chan struct{}
	n    int
}

// join puts a request in its source's line; nil if the line is full (a third request).
func (g *WorkGate) join(source string) *sourceLine {
	g.mu.Lock()
	defer g.mu.Unlock()
	l := g.held[source]
	if l == nil {
		l = &sourceLine{turn: make(chan struct{}, 1)}
		g.held[source] = l
	}
	if l.n >= 2 {
		return nil
	}
	l.n++
	return l
}

// leave takes a request out of its source's line; the line is forgotten when empty, so the map is never
// larger than twice the slots and the queue.
func (g *WorkGate) leave(source string, l *sourceLine) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if l.n--; l.n <= 0 {
		delete(g.held, source)
	}
}

// enter takes a slot, waiting at most g.wait in all, and returns its release. A nil gate admits
// everything.
//
// With a source: the request first takes its source's turn -- at once, or by waiting for the source's
// previous request to finish, holding NO global place meanwhile; a third request of the source is
// refused at once. Then it competes for the slot and the global queue like any request, within what is
// left of the same g.wait. So filling the gate still costs one source per place, and how long a request
// waits depends only on its own source's earlier request and the general load.
func (g *WorkGate) enter(ctx context.Context) (func(), error) {
	if g == nil {
		return func() {}, nil
	}
	t := time.NewTimer(g.wait)
	defer t.Stop()
	source := workSource(ctx)
	var line *sourceLine
	if source != "" {
		if line = g.join(source); line == nil {
			// The source already has a request running and one waiting: refused at once.
			return nil, g.refuse()
		}
		select {
		case line.turn <- struct{}{}:
		case <-t.C:
			g.leave(source, line)
			return nil, g.refuse()
		case <-ctx.Done():
			g.leave(source, line)
			return nil, ErrBusy
		}
	}
	// giveBack returns the source's turn and place in line (nothing without a source).
	giveBack := func() {
		if line != nil {
			<-line.turn
			g.leave(source, line)
		}
	}
	release := func() {
		<-g.slots
		giveBack()
	}
	select {
	case g.slots <- struct{}{}:
		return release, nil
	default:
	}
	select {
	case g.queue <- struct{}{}:
	default:
		giveBack()
		return nil, g.refuse()
	}
	defer func() { <-g.queue }()
	select {
	case g.slots <- struct{}{}:
		return release, nil
	case <-t.C:
		giveBack()
		return nil, g.refuse()
	case <-ctx.Done():
		// The caller left; nobody reads the answer, and the refusal is the same value.
		giveBack()
		return nil, ErrBusy
	}
}

// refuse counts a refusal and writes the WARN line at most once per workWarnEvery. The line names the
// gate's shape and how many were refused since the last one -- never an address or a person.
func (g *WorkGate) refuse() error {
	n := g.refusals.Add(1)
	now := time.Now().Unix()
	last := g.lastWarn.Load()
	if now-last >= int64(workWarnEvery/time.Second) && g.lastWarn.CompareAndSwap(last, now) {
		g.refusals.Add(-n)
		g.log.Warn("panel bcrypt work at capacity, refusing",
			"slots", cap(g.slots), "waiters", cap(g.queue), "wait", g.wait.String(), "refused", n)
	}
	return ErrBusy
}

// Option configures the four constructors that do password work.
type Option func(*options)

type options struct{ gate *WorkGate }

// WithWorkGate shares g: production wiring builds ONE gate and hands it to New, NewResets,
// NewSignupClaims and NewAdminInvites, so the ceiling is the process's and not one per type.
func WithWorkGate(g *WorkGate) Option { return func(o *options) { o.gate = g } }

// gateOf is the gate a constructor installs. Without WithWorkGate it is a gate of its own with one slot
// per P and a backstopQueue-wide queue -- a backstop, never ungated, and wide enough that the package's
// tests (which build many services and race tens of operations on one) are served; production always
// shares one (cmd/tappa, TestWorkGateWiring_OneGateServesEveryPasswordService).
func gateOf(opts []Option) *WorkGate {
	var o options
	for _, f := range opts {
		f(&o)
	}
	if o.gate != nil {
		return o.gate
	}
	g := newWorkGate(runtime.GOMAXPROCS(0), WorkWait, nil)
	g.queue = make(chan struct{}, backstopQueue)
	return g
}

// backstopQueue is the unshared gate's waiting room: wide, because what builds an unshared gate is a
// package test racing tens of cheap operations on one service, which a four-core CI runner's
// slots x workQueuePerSlot refused (measured: 40 simultaneous guesses, 26 refused at once).
const backstopQueue = 256
