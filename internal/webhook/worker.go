// Package webhook is the integration API's outbound side (ADR 0028 §6, §13): K-19's worker -- the
// in-process set of "tenants with work" and the loop that drains it (M11 API-4) -- and the signing key
// (API-9). The work itself arrives as Jobs: API-6's invitation queue (internal/invite) and API-10's
// delivery -- the lease, the signature, the retries, through the dial-time address gate -- in
// internal/webhook/delivery (a package of its own: this one reads nothing, see below).
//
// 🔴 K-19 (the user's decision, 2026-10-09): THE WORKER NEVER SCANS ACROSS TENANTS. There is no
// "every tenant" anywhere in this package: a tenant enters the set when something HAPPENED to it in
// this process -- a tap recorded (both tenants of a holding tap), an authenticated /v1 request, an
// approval (both tenants of a holding record -- API-10) and the panel's "resend" -- and the loop hands each tenant it takes to
// each Job, which reads ONLY that tenant's rows, in WithTenant with an explicit tenant_id filter. A
// pod that restarts starts with an empty set: work that was pending waits for the tenant's next tap
// or API call (plan §2 item 20 -- the named cost). TestWorker_NoTenantWideRead pins the package
// to that shape: it imports no database package and no store, and every query a Job of it names
// (none today) carries a tenant_id filter.
//
// The set is per PROCESS. Two pods each hold their own and may hand the same tenant to their own
// Job at the same moment (TestWorker_TwoProcessesBothRunTheJob): a Job must therefore take its rows
// under a lease (FOR UPDATE SKIP LOCKED -- ADR 0028 §6) and may assume nothing about being alone.
// The product runs one replica today; the delivery's lease is measured with two
// (TestWebhookWorker_TwoPodsSendOnce, internal/egress).
package webhook

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Job is one kind of per-tenant work (API-6's invitation sender, API-10's delivery). Declared here,
// at the consumer (CLAUDE.md §7). RunFor is handed ONE tenant and must not read another's rows.
type Job interface {
	RunFor(ctx context.Context, tenant uuid.UUID) error
}

// Worker is K-19's set and loop.
type Worker struct {
	jobs []Job
	log  *slog.Logger

	mu      sync.Mutex
	pending map[uuid.UUID]struct{}
	closed  bool

	wake   chan struct{}
	cancel context.CancelFunc
	done   chan struct{}
}

// NewWorker builds the worker with its jobs (none is a valid skeleton: the set fills and drains).
func NewWorker(log *slog.Logger, jobs ...Job) (*Worker, error) {
	if log == nil {
		log = slog.Default()
	}
	for _, j := range jobs {
		if j == nil {
			return nil, errors.New("webhook: nil job")
		}
	}
	return &Worker{jobs: jobs, log: log, pending: map[uuid.UUID]struct{}{}, wake: make(chan struct{}, 1)}, nil
}

// Add gives the worker one more job before it starts (cmd/tappa builds the invitation queue after
// the worker, once the e-mail transport exists). After Start it is refused: the loop reads the jobs
// without a lock.
func (w *Worker) Add(j Job) error {
	if j == nil {
		return errors.New("webhook: nil job")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done != nil || w.closed {
		return errors.New("webhook: a job is added before Start")
	}
	w.jobs = append(w.jobs, j)
	return nil
}

// Notice puts tenant into the set. It never blocks and never fails (it is called on the tap path
// and the API chain); the nil tenant and a notice after Drain are ignored. The set holds a tenant
// once however many times it is noticed, so it is bounded by the number of tenants.
func (w *Worker) Notice(tenant uuid.UUID) {
	if tenant == uuid.Nil {
		return
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.pending[tenant] = struct{}{}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Start runs the loop until Drain or ctx's end. A second Start, or one after Drain, is an error.
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done != nil || w.closed {
		return errors.New("webhook: the worker is started once, before Drain")
	}
	ctx, w.cancel = context.WithCancel(ctx)
	w.done = make(chan struct{})
	go w.loop(ctx)
	return nil
}

func (w *Worker) loop(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		}
		for _, tenant := range w.take() {
			for i, j := range w.jobs {
				if ctx.Err() != nil {
					return
				}
				err := j.RunFor(ctx, tenant)
				if err == nil || errors.Is(err, context.Canceled) {
					continue
				}
				// A job may say its tenant has work due LATER (API-6's invitation queue: a limit, a
				// back-off): the tenant is noticed again then -- a timer for that one tenant, never a
				// scan (K-19).
				var later DueLater
				if errors.As(err, &later) {
					w.noticeAfter(tenant, later.DueIn())
				}
				if _, only := err.(DueLater); only {
					continue
				}
				// The tenant id and the job's place only: a job's error must name no URL, key or
				// address (ADR 0028 §3, §6 -- API-10's job wraps its errors to a closed class).
				w.log.ErrorContext(ctx, "webhook: a job failed for a tenant; it runs again on the tenant's next activity",
					"tenant_id", tenant.String(), "job", i, "err", err)
			}
		}
	}
}

// DueLater is what a Job's error may carry to say "this tenant has work that is not due yet" (the
// invitation queue's *invite.Later). Declared here, at the consumer (§7). A job that returns ONLY a
// DueLater has not failed; one joined with other errors has, and is logged.
type DueLater interface {
	error
	DueIn() time.Duration
}

// noticeAfter notices tenant again after d (at once for d <= 0). A notice after Drain is ignored by
// Notice itself, so a timer that fires during shutdown does nothing.
func (w *Worker) noticeAfter(tenant uuid.UUID, d time.Duration) {
	if d <= 0 {
		w.Notice(tenant)
		return
	}
	time.AfterFunc(d, func() { w.Notice(tenant) })
}

// take empties the set and returns what it held.
func (w *Worker) take() []uuid.UUID {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]uuid.UUID, 0, len(w.pending))
	for t := range w.pending {
		out = append(out, t)
	}
	clear(w.pending)
	return out
}

// Drain stops the worker: no notice is taken any more, the loop's context is cancelled (a job in
// flight sees it; API-10's lease lets its delivery be retried), and Drain waits for the loop to
// return or for ctx's end. It runs beside the HTTP server's Shutdown (cmd/tappa), inside its budget.
// A worker never started drains at once.
func (w *Worker) Drain(ctx context.Context) error {
	w.mu.Lock()
	w.closed = true
	cancel, done := w.cancel, w.done
	w.mu.Unlock()
	if done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.New("webhook: the worker did not stop within the shutdown budget")
	}
}
