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

	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/internal/store"
)

// QueueLease is K-17's lease, as invitequeue.sql writes it ('60 seconds').
const QueueLease = 60 * time.Second

// The queue's patience (API-6 review, two rounds).
//
//	a ceiling before the mint   ADR 0022 §9's issuing limits, the process breaker, the per-mailbox
//	                            cap -- all asked BEFORE a code is minted, so a refusal costs no
//	                            invitation row (the rows those limits count) and spends no try: the
//	                            request is due again after queueLimitDelay
//	K-22 closed                 the operator closed the business's API: nothing is minted, the request
//	                            stays and is due again after queueClosedDelay; reopened, it goes on
//	a ceiling after the mint    the breaker or the cap refusing the send itself (a race with the
//	                            question above): delayed like a limit, no try spent
//	a failed send               every other failure, a 4xx included (internal/mail already retried a
//	                            4xx once inside the send): one try, queueBackoff(tries); at
//	                            queueMaxAttempts the queue gives up
//	the lifetime                whatever the class, a request older than queueLifetime is given up --
//	                            so no mix of delays can keep one alive (and its rows growing) forever
//
// Giving up retires the person's undelivered links (RetireUndeliveredInvites) and takes the request
// out of the queue in one transaction; the trail's last attempt is then an invite.undelivered (five
// failures) or invite.email_refused ("request_expired") row, the API answers "undelivered", and a
// resend asks again.
const (
	queueLimitDelay  = 10 * time.Minute
	queueClosedDelay = 15 * time.Minute
	queueMaxAttempts = 5
	queueLifetime    = 72 * time.Hour
	// queueRunMax bounds one RunFor: past it the worker is asked to come back at once, so one
	// tenant's long queue cannot hold the loop away from every other tenant.
	queueRunMax = 25
)

// ErrRequestExpired: the queue gave up on a request older than its lifetime without reaching the
// person. Its invite.email_refused row carries "request_expired".
var ErrRequestExpired = errors.New("invite: the queued invitation could not be sent within its lifetime")

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
	sink  QueueSink
	audit auditRecorder
	// now is the wall clock (tests freeze it with the Manager's).
	now func() time.Time
	// afterMint, when set, runs inside the minting transaction after the invitation is inserted
	// and the recipient read, before the lease is renewed — a test's stopping point. nil in
	// production.
	afterMint func(ctx context.Context, employee uuid.UUID)
}

// QueueSink is the queue's e-mail route: MailSink, and the breaker's question asked before a code
// is minted (M11 API-6 review O-B). Declared here, at the consumer; internal/handler's
// EmailInvitations.QueueSink implements it around the panel's one cap and breaker.
type QueueSink interface {
	MailSink
	// Refusing reports whether the process breaker would refuse a send now.
	Refusing() bool
}

// NewQueue builds the worker. The sink is the e-mail route's (the panel's per-mailbox cap and
// breaker around the one transport — cmd/tappa); the recorder writes the outcome rows.
func NewQueue(m *Manager, sink QueueSink, rec auditRecorder) (*Queue, error) {
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
	deferred                // a limit, the breaker or the mailbox cap: due again later, nothing minted
	closed                  // K-22: the business's API is closed -- nothing minted, the request waits
	expired                 // older than queueLifetime: give up
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
		// THE LIFETIME FIRST, before the gate (API-6 review D-a): "at most 72 hours" holds whatever
		// keeps the request waiting -- a closed API included -- so a closed business's queue is not
		// run every 15 minutes for ever.
		if c.RequestedAt.Add(queueLifetime).Before(q.now()) {
			result = expired
			return errRollback
		}
		// K-22 (ADR 0028 §2: the gate "her yolda", the worker's send included): a business whose
		// API the operator closed has nothing minted or sent for it. The request is NOT dropped --
		// reopened within its lifetime, it goes on (API-6 review O-A). The read takes no lock: a
		// closing committed after it does not stop the one send already in flight (named, ADR 0028).
		open, err := s.GetTenantAPIEnabledAt(ctx, tenant)
		if err != nil {
			return fmt.Errorf("read the API gate: %w", err)
		}
		if open == nil {
			result = closed
			return errRollback
		}
		// THE CEILINGS ARE ASKED BEFORE THE MINT (API-6 review O-B): the issuing limits, the
		// address's own refusals, the breaker and the mailbox cap. A refusal here mints no row --
		// and the rows are what ADR 0022 §9's limits count, the panel's included -- and spends no
		// try. The address is read again after the insert (below), which stays the authority on
		// where the link goes (GetInviteRecipient's ordering argument).
		if err := checkLimits(ctx, s, p); err != nil {
			if _, limit := RefusalReason(err); limit {
				result = deferred
				return errRollback
			}
			return err
		}
		ahead, err := readRecipient(ctx, s, p)
		if err != nil {
			if r, ok := RefusalReason(err); ok {
				result, reason = refused, r
				return errRollback
			}
			return err
		}
		if q.sink.Refusing() || q.sink.RecipientCapped(tenant, ahead.Address) {
			result = deferred
			return errRollback
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
	case closed:
		return q.deferTo(ctx, tenant, c.EmployeeID, lease, q.now().Add(queueClosedDelay))
	case expired:
		ch := &EmailChannel{sink: q.sink, audit: q.audit, channel: queueChannelName}
		rerr := ch.refused(ctx, p, refusalReasons[ErrRequestExpired])
		if err := q.giveUp(ctx, tenant, c.EmployeeID, lease); err != nil {
			return errors.Join(err, rerr)
		}
		return errors.Join(fmt.Errorf("invite: queue: %s: %w", c.EmployeeID, ErrRequestExpired), rerr)
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
	case deliveryDeferred(sendErr):
		// The breaker or the per-mailbox cap refused the send itself -- a race with the question
		// asked before the mint: a LIMIT, which delays the request and spends no try (ADR 0022 §9,
		// plan §2 item 11; API-6 review O-2), bounded by queueLifetime. The minted link is retired
		// by the next mint or by the give-up.
		if err := q.deferTo(ctx, tenant, c.EmployeeID, lease, q.now().Add(queueLimitDelay)); err != nil {
			return errors.Join(err, sendErr)
		}
		return nil
	default:
		return q.failed(ctx, tenant, c, lease, sendErr)
	}
}

// deliveryDeferred reports whether a failed send was refused by one of OUR ceilings -- the
// breaker or the per-mailbox cap (mail.SendError's classes) -- rather than by the relay or the
// network. Those delay; every other failure is a try.
//
// A 4xx (ClassThrottled) IS a try (API-6 review O-B, measured: counted as a delay, 25 × 450 kept a
// request alive with 25 minted codes, 25 e-mails and 25 trail rows, and the delays spent the
// business's issuing limits). internal/mail already retries a 4xx once inside the send, so a 4xx
// here is the second refusal in a row; a domain that answers every recipient 450 is given up after
// queueMaxAttempts like a 550.
func deliveryDeferred(err error) bool {
	var se *mail.SendError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Class {
	case mail.ClassBreaker, mail.ClassRecipientCap:
		return true
	}
	return false
}

// giveUp retires the person's undelivered links and takes the request out of the queue under this
// worker's lease, in one transaction (the lifetime's end; failed does the same at its last try).
func (q *Queue) giveUp(ctx context.Context, tenant, employee uuid.UUID, lease time.Time) error {
	err := q.m.data.WithTenant(context.WithoutCancel(ctx), tenant, func(ctx context.Context, tx pgx.Tx) error {
		s := store.New(tx)
		if _, err := s.RetireUndeliveredInvites(ctx, store.RetireUndeliveredInvitesParams{TenantID: tenant, EmployeeID: employee}); err != nil {
			return fmt.Errorf("retire the undelivered invitations: %w", err)
		}
		if _, err := s.FinishInviteRequest(ctx, store.FinishInviteRequestParams{TenantID: tenant, EmployeeID: employee, LeaseUntil: &lease}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("take the request out of the queue: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("invite: queue: give up on %s: %w", employee, err)
	}
	return nil
}

// failed counts one failed send (FailInviteRequest): the request is due again after the back-off,
// or -- at queueMaxAttempts -- the queue GIVES UP: the person's never-delivered invitations are
// retired (RetireUndeliveredInvites) and the request leaves the queue, in one transaction, so the
// person is left with no live unsent link and the API answers activationEmail "undelivered"
// (the trail's last attempt is this invite.undelivered row). A resend asks again (API-6 review O-1).
func (q *Queue) failed(ctx context.Context, tenant uuid.UUID, c store.ClaimInviteRequestRow, lease time.Time, sendErr error) error {
	gaveUp := false
	var tries int32
	due := q.now().Add(queueBackoff(c.Attempts + 1))
	err := q.m.data.WithTenant(context.WithoutCancel(ctx), tenant, func(ctx context.Context, tx pgx.Tx) error {
		s := store.New(tx)
		n, err := s.FailInviteRequest(ctx, store.FailInviteRequestParams{
			Due: &due, TenantID: tenant, EmployeeID: c.EmployeeID, LeaseUntil: &lease,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // the lease was lost: the request is somebody else's now
		}
		if err != nil {
			return err
		}
		if tries = n; n < queueMaxAttempts {
			return nil
		}
		gaveUp = true
		if _, err := s.RetireUndeliveredInvites(ctx, store.RetireUndeliveredInvitesParams{TenantID: tenant, EmployeeID: c.EmployeeID}); err != nil {
			return fmt.Errorf("retire the undelivered invitations: %w", err)
		}
		if _, err := s.FinishInviteRequest(ctx, store.FinishInviteRequestParams{TenantID: tenant, EmployeeID: c.EmployeeID, LeaseUntil: &due}); err != nil {
			return fmt.Errorf("take the request out of the queue: %w", err)
		}
		return nil
	})
	if err != nil {
		return errors.Join(fmt.Errorf("invite: queue: record a failed send for %s: %w", c.EmployeeID, err), sendErr)
	}
	if gaveUp {
		return fmt.Errorf("invite: queue: gave up after %d failed sends for %s: %w", tries, c.EmployeeID, sendErr)
	}
	return fmt.Errorf("invite: queue: send for %s: %w", c.EmployeeID, sendErr)
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
