package invite

// queue.go — K-17's invitation queue worker (M11 API-6; ADR 0028 §9, §13; ADR 0022's M11 note).
// An employee the integration API opens with an address, an address change of somebody not yet
// activated, and resend-activation all ASK for an invitation: a row in invite_requests. This file
// is what turns a row into a sent invitation — the e-mail route's own rules (email.go), driven by
// a lease instead of a manager's press.
//
// WHAT IT GUARANTEES, AND HOW:
//
//   - THE CODE IS MINTED AT SEND TIME, inside the minting transaction, and never waits anywhere:
//     the queue holds no code and no hash (00037 gives the table five columns; TestInviteQueue_HoldsNoCode
//     reads them and this worker's output). The raw code leaves through the one seam it always
//     had: Channel.DeliverInvite, here the EmailChannel.
//   - THE OLD INVITATIONS ARE RETIRED IN THE SAME TRANSACTION as the new one is inserted
//     (IssueAndDeliver's rule and order: retire, then insert).
//   - TWO PODS NEVER MINT ONE REQUEST TOGETHER (K-17's 60-second lease): a pod claims ONE due
//     request (SKIP LOCKED — a second pod takes another or none), and renews the lease as the LAST
//     statement of its minting transaction, matching the value it claimed: a request re-queued by
//     an address change or re-claimed after an expired lease is not this pod's any more, and the
//     mint is rolled back. The renewed lease runs 60 s from the mint, longer than the send's bound
//     (EmailSendGrace, 10 s) plus its audit row's (EmailRecordGrace, 5 s); only then does the
//     request leave the queue (FinishInviteRequest, under the same lease).
//   - THE LIMITS ARE THE PANEL'S (ADR 0022 §9, EM-7C): checkLimits counts the same employee_invites
//     rows under the same lock, and the sink's per-mailbox cap is the panel's one instance — the
//     queue and the panel share the counters. A limit DELAYS the request (the row stays, due again
//     later); it never refuses it (plan §2 item 11).
//   - ONLY AN 'invited' PERSON IS SENT ONE (K-15): status is read before anything is retired and
//     again FOR SHARE inside the minting transaction, after the retirements and before the insert
//     — the lock order of the activation and of an address change (invitations, then the
//     employee), so none of the three deadlocks against it, and none can commit between the read
//     and the mint's commit.
//
// K-19: RunFor is handed ONE tenant (internal/webhook's worker) and reads only that tenant's rows,
// in WithTenant with an explicit tenant_id predicate on every statement. When a request is due
// later than now it says so (Later), and the worker notices the tenant again then — the queue is
// never scanned across tenants.
//
// IT LOGS NOTHING (the package's rule): every failure comes back as an error that names no value,
// and the sink's error is a *mail.SendError (a class and a code).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// QueueLease is K-17's lease, as invitequeue.sql writes it ('60 seconds').
const QueueLease = 60 * time.Second

// The queue's patience. A limit (ADR 0022 §9) or a refused mailbox cap delays a request by
// queueLimitDelay; a failed send by queueBackoff(attempt). After queueMaxAttempts failed sends the
// request leaves the queue: every try left an invite.undelivered row, and resend-activation (or
// the panel) asks again.
const (
	queueLimitDelay  = 10 * time.Minute
	queueMaxAttempts = 5
	// queueRunMax bounds one RunFor: past it the worker is asked to come back at once, so one
	// tenant's long queue cannot hold the loop away from every other tenant.
	queueRunMax = 25
)

func queueBackoff(attempt int32) time.Duration {
	switch {
	case attempt <= 1:
		return time.Minute
	case attempt == 2:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}

// queueChannelName is the detail.channel of the queue's invite.code_emailed /
// invite.undelivered / invite.email_refused rows — the panel's e-mail route writes "email", so
// the trail tells a sent-on-request invitation from a pressed one.
const queueChannelName = "email_queue"

// Later is RunFor's answer "there is work for this tenant that is not due yet": internal/webhook's
// worker reads DueIn and notices the tenant again after it. It is not a failure.
type Later struct{ After time.Duration }

func (l *Later) Error() string { return "invite: queued work is due later" }

// DueIn is how long until the tenant should be run again.
func (l *Later) DueIn() time.Duration { return l.After }

// Queue is the invitation queue's worker job. Dependencies are struct fields (§7).
type Queue struct {
	m     *Manager
	sink  MailSink
	audit auditRecorder
	// now is the wall clock (tests freeze it with the Manager's).
	now func() time.Time
	// afterMint, when set, runs inside the minting transaction after the invitation is inserted
	// and the recipient read, before the lease is renewed — a test's stopping point. nil in
	// production.
	afterMint func(ctx context.Context, employee uuid.UUID)
}

// NewQueue builds the worker. The sink is the e-mail route's (the panel's per-mailbox cap and
// breaker around the one transport — cmd/tappa); the recorder writes the outcome rows.
func NewQueue(m *Manager, sink MailSink, rec auditRecorder) (*Queue, error) {
	switch {
	case m == nil:
		return nil, errors.New("invite: queue: nil manager")
	case sink == nil:
		return nil, errors.New("invite: queue: nil mail sink")
	case rec == nil:
		return nil, errors.New("invite: queue: nil audit recorder")
	}
	return &Queue{m: m, sink: sink, audit: rec, now: m.clock}, nil
}

// RunFor drains the tenant's due requests, one at a time, each under its own lease. A request
// whose send failed is put back with a back-off and the next one is taken -- one bad address does
// not hold the rest. It returns the failures it met joined with a *Later when requests remain that
// are due later (a limit, a back-off, another pod's lease); nil when the queue is empty and
// nothing failed.
func (q *Queue) RunFor(ctx context.Context, tenant uuid.UUID) error {
	if tenant == uuid.Nil {
		return errors.New("invite: queue: no tenant")
	}
	var failures []error
	for range queueRunMax {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		claim, found, err := q.claim(ctx, tenant)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		if !found {
			return withFailures(failures, q.whenNext(ctx, tenant))
		}
		if err := q.process(ctx, tenant, claim); err != nil {
			failures = append(failures, err)
		}
	}
	return withFailures(failures, &Later{After: 0})
}

// withFailures joins the failures a run met with how it ended. With no failure the ending is
// returned AS IT IS -- a bare *Later is the worker's "not a failure, come back" (errors.Join would
// wrap even one error).
func withFailures(failures []error, end error) error {
	if len(failures) == 0 {
		return end
	}
	return errors.Join(append(failures, end)...)
}

// claim takes the tenant's oldest due request for QueueLease, in a transaction of its own.
func (q *Queue) claim(ctx context.Context, tenant uuid.UUID) (store.ClaimInviteRequestRow, bool, error) {
	var row store.ClaimInviteRequestRow
	found := false
	err := q.m.data.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		r, err := store.New(tx).ClaimInviteRequest(ctx, tenant)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		row, found = r, true
		return nil
	})
	if err != nil {
		return row, false, fmt.Errorf("invite: queue: claim: %w", err)
	}
	if found && row.LeaseUntil == nil {
		return row, false, errors.New("invite: queue: a claim returned no lease")
	}
	return row, found, nil
}

// whenNext reports the earliest moment a waiting request of the tenant is due, as a *Later; nil
// when nothing waits.
func (q *Queue) whenNext(ctx context.Context, tenant uuid.UUID) error {
	var next *time.Time
	err := q.m.data.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		n, err := store.New(tx).NextInviteRequestDue(ctx, tenant)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		next = n
		return nil
	})
	if err != nil {
		return fmt.Errorf("invite: queue: next due: %w", err)
	}
	if next == nil {
		return nil
	}
	return &Later{After: max(next.Sub(q.now()), time.Second)}
}

// outcome is what the minting transaction decided.
type outcome int

const (
	minted   outcome = iota // an invitation exists; send it
	skipped                 // the person is not 'invited' any more: the request is done
	deferred                // a limit or the mailbox cap: due again later
	refused                 // an address the route cannot use: the request is done, the row says why
	lost                    // the lease is not this worker's any more: leave the request alone
)

// process mints, sends and settles one claimed request.
func (q *Queue) process(ctx context.Context, tenant uuid.UUID, c store.ClaimInviteRequestRow) error {
	p := IssueParams{TenantID: tenant, EmployeeID: c.EmployeeID}
	lease := *c.LeaseUntil
	code, err := newCode()
	if err != nil {
		return fmt.Errorf("invite: queue: %w", err)
	}
	hash, err := code.hash(q.m.hmacKey)
	if err != nil {
		return fmt.Errorf("invite: queue: %w", err)
	}

	var (
		row    store.CreateInviteRow
		to     Recipient
		result outcome
		reason string
	)
	err = q.m.data.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
		s := store.New(tx)
		status, err := s.GetEmployeeInviteStatus(ctx, store.GetEmployeeInviteStatusParams{TenantID: tenant, EmployeeID: c.EmployeeID})
		if err != nil {
			return fmt.Errorf("read the person: %w", err)
		}
		if status != statusInvited {
			result = skipped
			return nil
		}
		if err := checkLimits(ctx, s, p); err != nil {
			if _, limit := RefusalReason(err); limit {
				result = deferred
				return errRollback
			}
			return err
		}
		if _, err := s.CancelPendingInvitesForEmployee(ctx, store.CancelPendingInvitesForEmployeeParams{TenantID: tenant, EmployeeID: c.EmployeeID}); err != nil {
			return fmt.Errorf("retire invitations: %w", err)
		}
		status, err = s.LockEmployeeForQueuedInvite(ctx, store.LockEmployeeForQueuedInviteParams{TenantID: tenant, EmployeeID: c.EmployeeID})
		if err != nil {
			return fmt.Errorf("lock the person: %w", err)
		}
		if status != statusInvited {
			// Activated or deactivated between the first read and the lock: nothing of this
			// transaction may stand (the retirement included).
			result = skipped
			return errRollback
		}
		if row, err = s.CreateInvite(ctx, store.CreateInviteParams{
			TenantID: tenant, EmployeeID: c.EmployeeID, CodeHash: hash, ExpiresAt: q.m.clock().Add(defaultTTL),
		}); err != nil {
			return fmt.Errorf("mint: %w", err)
		}
		to, err = readRecipient(ctx, s, p)
		switch {
		case err == nil && q.sink.RecipientCapped(tenant, to.Address):
			result = deferred
			return errRollback
		case err != nil:
			if r, ok := RefusalReason(err); ok {
				result, reason = refused, r
				return errRollback
			}
			return err
		}
		if q.afterMint != nil {
			q.afterMint(ctx, c.EmployeeID)
		}
		renewed, err := s.RenewInviteRequestLease(ctx, store.RenewInviteRequestLeaseParams{TenantID: tenant, EmployeeID: c.EmployeeID, LeaseUntil: &lease})
		if errors.Is(err, pgx.ErrNoRows) {
			result = lost
			return errRollback
		}
		if err != nil {
			return fmt.Errorf("renew the lease: %w", err)
		}
		if renewed == nil {
			return errors.New("renew the lease: no lease returned")
		}
		lease = *renewed
		result = minted
		return nil
	})
	if err != nil && !errors.Is(err, errRollback) {
		return fmt.Errorf("invite: queue: mint for %s: %w", c.EmployeeID, err)
	}

	switch result {
	case skipped:
		return q.finish(ctx, tenant, c.EmployeeID, lease)
	case deferred:
		return q.deferTo(ctx, tenant, c.EmployeeID, lease, q.now().Add(queueLimitDelay))
	case lost:
		return nil
	case refused:
		ch := &EmailChannel{sink: q.sink, audit: q.audit, channel: queueChannelName}
		rerr := ch.refused(ctx, p, reason)
		if err := q.finish(ctx, tenant, c.EmployeeID, lease); err != nil {
			return errors.Join(err, rerr)
		}
		return rerr
	}

	inv := Invite{ID: row.ID, TenantID: row.TenantID, EmployeeID: row.EmployeeID, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}
	sink := &sentSink{MailSink: q.sink}
	ch := &EmailChannel{sink: sink, audit: q.audit, channel: queueChannelName}
	sendErr := ch.DeliverInvite(ctx, Delivery{Invite: inv, ActivationURL: q.m.activationURL(code), Recipient: to})
	switch {
	case sink.sent:
		// Sent. An error here is the outcome row that could not be written (ErrNotRecorded): the
		// invitation still went, so the request is done, and the error travels so the worker's log
		// says the trail is short of a row.
		if err := q.finish(ctx, tenant, c.EmployeeID, lease); err != nil {
			return errors.Join(err, sendErr)
		}
		return sendErr
	case c.Attempts >= queueMaxAttempts:
		if err := q.finish(ctx, tenant, c.EmployeeID, lease); err != nil {
			return errors.Join(err, sendErr)
		}
		return fmt.Errorf("invite: queue: gave up after %d tries for %s: %w", c.Attempts, c.EmployeeID, sendErr)
	default:
		if err := q.deferTo(ctx, tenant, c.EmployeeID, lease, q.now().Add(queueBackoff(c.Attempts))); err != nil {
			return errors.Join(err, sendErr)
		}
		return fmt.Errorf("invite: queue: send for %s: %w", c.EmployeeID, sendErr)
	}
}

// sentSink remembers whether the sink accepted the e-mail -- what decides whether the request is
// done, whatever became of the outcome row.
type sentSink struct {
	MailSink
	sent bool
}

func (s *sentSink) SendInvitation(ctx context.Context, d Delivery) (string, error) {
	id, err := s.MailSink.SendInvitation(ctx, d)
	s.sent = err == nil
	return id, err
}

// errRollback ends a minting transaction that decided not to mint: WithTenant rolls back on any
// error, and process reads the decision from result.
var errRollback = errors.New("invite: queue: nothing to commit")

// finish takes the request out of the queue under this worker's lease (a request re-queued in
// the meantime stays).
func (q *Queue) finish(ctx context.Context, tenant, employee uuid.UUID, lease time.Time) error {
	err := q.m.data.WithTenant(context.WithoutCancel(ctx), tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := store.New(tx).FinishInviteRequest(ctx, store.FinishInviteRequestParams{TenantID: tenant, EmployeeID: employee, LeaseUntil: &lease})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("invite: queue: finish %s: %w", employee, err)
	}
	return nil
}

// deferTo leaves the request in the queue, due again at due, under this worker's lease.
func (q *Queue) deferTo(ctx context.Context, tenant, employee uuid.UUID, lease, due time.Time) error {
	err := q.m.data.WithTenant(context.WithoutCancel(ctx), tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := store.New(tx).DeferInviteRequest(ctx, store.DeferInviteRequestParams{Due: &due, TenantID: tenant, EmployeeID: employee, LeaseUntil: &lease})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("invite: queue: defer %s: %w", employee, err)
	}
	return nil
}
