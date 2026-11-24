package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/web/templates/email"
)

// THE OWNERS' NOTICES (M16-37's notice path; the M16-35 card's "Owner e-postası" paragraph).
//
// An event about an employee that the business's OWNERS must hear of by e-mail -- today one kind, an
// employee's "Delete my account" (Q46 (b)) -- is handed to ownerNotices, and the request that caused it
// answers at once. A bounded background job does the rest: it reads the business's owners
// (resolve_admin_owners_by_tenant, 00081: ORDER BY a.id, LIMIT 200), each owner's address on the
// password notice's path (adminauth.Resets.NoticeRecipient -- the admin_users.email mirror the
// identity's definer writes, ADR 0031 K-14), and sends each owner one e-mail.
//
// 🔴 WHY NOT IN THE REQUEST: up to 200 owners, each send bounded by invite.EmailSendGrace (10 s), while
// the apps read for 15 s (Android) and 20 s (iOS). The answer (204) must not wait for a relay.
//
// 🔴 WHY NOT THE RESET OUTBOX: that FIFO is shared with an anonymous public form; every job here is an
// authenticated act of an employee of the business (a signed app request), so the two must not starve
// each other. This queue is its own: ownerNoticeQueueSize jobs, ownerNoticeWorkers workers, at most
// ownerNoticeSendsPerJob sends at once inside a job, each send inside the send grace, every job inside
// OwnerNoticeJobGrace.
//
// THE BREAKER COUNTS THE NOTICE AND NEVER REFUSES IT (Breaker.SendExempt, the password notice's rule):
// the anonymous traffic that can fill the process-wide window must not silence what the business owes
// its employee an answer about.
//
// 🔴 A KIND IS A ROW OF ownerNoticeLetters, NOT A BRANCH. Each kind names its two audit actions and its
// renderer; the queue, the owners' read, the cap, the drain and every outcome row are shared. A later
// self_activation (M16-35) is one more constant and one more row.
//
// EVERY OUTCOME IS AN AUDIT ROW IN THE BUSINESS (target = the employee, no actor, detail = the owner's
// admin_user_id and fixed words, NEVER an address -- ownerNoticeDetail has no field one could travel in):
//   - sent, per owner;
//   - undelivered, per owner: no e-mail in this deployment, the owner's daily cap spent, no address, the
//     address unreadable, the relay refusing, the job's time or the drain cutting it, a panic;
//   - undelivered, ONE per job with no owner named: the queue full or closed (written synchronously, by
//     the request), a job the drain never started, the owners unreadable or none, a panic before the
//     owners were known.
//
// COUNTED LIMITS: a process that DIES loses the queue and what is in flight with no row (the reset
// outbox's accepted gap; the People card's line is the deletion row itself, which committed first and
// is unaffected). A send the relay accepted after a cut is recorded undelivered. Rows past an owner's
// cap are still written (one per request per owner), so their volume is the requests' own -- each
// request already writes its employee.deletion_requested row. The cap is per process and per fixed
// window, so a burst at the boundary reaches twice the limit.

// The owners' notices' sizes and clocks.
const (
	ownerNoticeQueueSize   = 32
	ownerNoticeWorkers     = 2
	ownerNoticeSendsPerJob = 4
	// OwnerNoticeJobGrace bounds one job from its start: every owner's read, render and send. A job
	// it cuts gives every owner not yet sent an undelivered row. It is NOT a shutdown term: the drain
	// (Drain, ResetDrainGrace) cuts a job long before this.
	OwnerNoticeJobGrace = 120 * time.Second
	// ownerNoticeRecordGrace bounds one outcome row's write, separately from the send (EM-5's lesson).
	ownerNoticeRecordGrace = 5 * time.Second
	// ownerNoticeLimit / ownerNoticePeriod: how many notices of ONE kind one owner may be SENT a day.
	ownerNoticeLimit  = 20
	ownerNoticePeriod = 24 * time.Hour
)

// ownerNoticeKind chooses a notice's audit actions and letter (ownerNoticeLetters).
type ownerNoticeKind string

// ownerNoticeDeletionRequest is an employee's "Delete my account" (M16-37, Q46 (b)).
const ownerNoticeDeletionRequest ownerNoticeKind = "deletion_request"

// The deletion request notice's audit actions: one per owner (or one per job when no owner was reached).
const (
	ActionEmployeeDeletionNoticeSent        = "employee.deletion_notice.sent"
	ActionEmployeeDeletionNoticeUndelivered = "employee.deletion_notice.undelivered"
)

// ownerNoticeLetter is one kind's row: its two actions and how its e-mail is rendered.
type ownerNoticeLetter struct {
	sent, undelivered string
	render            func(ctx context.Context, baseURL string, n OwnerNotice) (mail.Message, error)
}

// ownerNoticeLetters is every kind the queue knows. A kind not here is refused at Submit.
var ownerNoticeLetters = map[ownerNoticeKind]ownerNoticeLetter{
	ownerNoticeDeletionRequest: {
		sent:        ActionEmployeeDeletionNoticeSent,
		undelivered: ActionEmployeeDeletionNoticeUndelivered,
		render: func(ctx context.Context, baseURL string, n OwnerNotice) (mail.Message, error) {
			return email.RenderDeletionRequested(ctx, signInLink(baseURL), email.DeletionRequestedView{
				BaseURL: baseURL, EmployeeName: n.EmployeeName, RequestedAt: n.At, Zone: n.Zone,
				Deactivated: n.Deactivated,
			})
		},
	},
}

// The fixed reasons an undelivered row carries.
const (
	ownerReasonOff         = "this deployment sends no e-mail (TAPPA_RESET_DELIVERY is none), so the owner was not e-mailed"
	ownerReasonLimited     = "too many notices to this owner today, so the owner was not e-mailed"
	ownerReasonNoAddress   = "the owner has no active address to send the notice to"
	ownerReasonReadFailed  = "the owner's address could not be read, so the owner was not e-mailed"
	ownerReasonRenderFail  = "the notice could not be built, so the owner was not e-mailed"
	ownerReasonSendFailed  = "the notice could not be handed to the delivery channel"
	ownerReasonTimeUp      = "the notice ran out of time before this owner was e-mailed"
	ownerReasonDrainEnded  = "the process stopped before this owner was e-mailed"
	ownerReasonFault       = "the notice stopped on an internal fault; the owner may not have been e-mailed"
	ownerReasonQueueFull   = "the notice queue was full, so no owner was e-mailed"
	ownerReasonQueueShut   = "the notice queue was closed for shutdown, so no owner was e-mailed"
	ownerReasonNotStarted  = "the process stopped before the notice started, so no owner was e-mailed"
	ownerReasonOwnersRead  = "the business's owners could not be read, so no owner was e-mailed"
	ownerReasonNoOwners    = "the business has no owner who can sign in, so no owner was e-mailed"
	ownerReasonJobFault    = "the notice stopped on an internal fault before its owners were known; no owner may have been e-mailed"
	ownerNoticeLogPanicked = "owner notice: the notice panicked; the request stands and the notice went on"
)

// OwnerNotice is one event the owners hear of. It carries no address: the employee's is never read, and
// each owner's is read by the job.
type OwnerNotice struct {
	Kind       ownerNoticeKind
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// EmployeeName is the person's name as the business stores it (the letter gates it, nameShown).
	EmployeeName string
	// At is when the event happened; Zone the business's zone (nil reads UTC).
	At   time.Time
	Zone *time.Location
	// Deactivated says the employee was deactivated when the event happened (a deletion request then
	// revokes nothing -- T59), so the letter must not say they were signed out.
	Deactivated bool
}

// ownerNoticeDirectory is what the job reads, declared at the consumer (§7). *adminauth.Resets is it.
type ownerNoticeDirectory interface {
	NoticeOwners(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error)
	NoticeRecipient(ctx context.Context, tenantID, adminUserID uuid.UUID) (string, error)
}

// ownerNoticeSender is the one send the job makes: *mail.Breaker's SendExempt.
type ownerNoticeSender interface {
	SendExempt(ctx context.Context, m mail.Message) (mail.Receipt, error)
}

// ownerNoticeDetail is an outcome row's payload. A purpose-built struct with NO field an address could
// travel in (passwordNoticeDetail's rule): the owner's id, fixed words, and a failed send's class and
// reply code.
type ownerNoticeDetail struct {
	Outcome     string `json:"outcome"`
	AdminUserID string `json:"admin_user_id,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Class       string `json:"class,omitempty"`
	SMTPCode    int    `json:"smtp_code,omitempty"`
}

// ownerNoticeJob is one queued notice.
type ownerNoticeJob struct {
	// base is the request's context WITHOUT its cancellation (its values -- the request id -- kept).
	base context.Context
	n    OwnerNotice
}

// OwnerNotices is the owners' notices queue and its workers. Build it with NewEmailOwnerNotices and
// drain it at shutdown (Drain).
type OwnerNotices struct {
	dir ownerNoticeDirectory
	// send is nil when this deployment sends no e-mail: every owner then gets the "off" row.
	send    ownerNoticeSender
	baseURL string
	audit   auditRecorder
	log     *slog.Logger
	limiter *limiter

	// sendGrace and jobGrace are invite.EmailSendGrace and OwnerNoticeJobGrace; fields so a test can
	// make them short.
	sendGrace, jobGrace time.Duration

	jobs chan ownerNoticeJob
	// mu guards closed and the close of jobs, so a Submit never sends on a closed channel.
	mu     sync.Mutex
	closed bool

	// sendsStopped: the drain's send phase is over (in-flight sends end, queued jobs get one row each).
	sendsStopped context.Context
	stopSending  context.CancelFunc
	// budgetSpent: the drain's whole budget is spent (a row still being written is abandoned).
	budgetSpent context.Context
	spendBudget context.CancelFunc

	// done is closed when every worker has returned.
	done chan struct{}
}

// NewEmailOwnerNotices builds the owners' notices around the process-wide breaker and starts its
// workers. on is TAPPA_RESET_DELIVERY=email -- the one switch for the letters that go to an
// administrator's own address (the password notice's); off, or with no breaker, nothing is sent and
// every owner gets a row saying so. dir reads the owners and their addresses (*adminauth.Resets).
//
// With e-mail on it renders each kind once with a probe, so a TAPPA_BASE_URL the letters cannot carry
// refuses to boot instead of recording every notice undelivered.
func NewEmailOwnerNotices(sender *mail.Breaker, on bool, dir ownerNoticeDirectory, baseURL string, trail auditRecorder, log *slog.Logger) (*OwnerNotices, error) {
	var s ownerNoticeSender
	if on && sender != nil {
		s = sender
	}
	return newOwnerNotices(s, dir, baseURL, trail, log)
}

func newOwnerNotices(s ownerNoticeSender, dir ownerNoticeDirectory, baseURL string, trail auditRecorder, log *slog.Logger) (*OwnerNotices, error) {
	if isNil(dir) {
		return nil, errors.New("handler: nil owner notice directory")
	}
	if isNil(trail) {
		return nil, errors.New("handler: nil owner notice trail")
	}
	if log == nil {
		log = slog.Default()
	}
	if s != nil {
		for kind, l := range ownerNoticeLetters {
			if _, err := l.render(context.Background(), baseURL, OwnerNotice{Kind: kind, At: time.Now()}); err != nil {
				return nil, fmt.Errorf("handler: the %s owner notice cannot be built from TAPPA_BASE_URL: %w", kind, err)
			}
		}
	}
	o := &OwnerNotices{
		dir: dir, send: s, baseURL: baseURL, audit: trail, log: log,
		limiter:   newLimiter(ownerNoticeLimit, ownerNoticePeriod),
		sendGrace: invite.EmailSendGrace, jobGrace: OwnerNoticeJobGrace,
		jobs: make(chan ownerNoticeJob, ownerNoticeQueueSize),
		done: make(chan struct{}),
	}
	o.sendsStopped, o.stopSending = context.WithCancel(context.Background())
	o.budgetSpent, o.spendBudget = context.WithCancel(context.Background())
	o.start()
	return o, nil
}

// start runs the workers; done closes when the last returns.
func (o *OwnerNotices) start() {
	var wg sync.WaitGroup
	for range ownerNoticeWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range o.jobs {
				o.handle(j)
			}
		}()
	}
	go func() { wg.Wait(); close(o.done) }()
}

// ownerNoticeBudgetKey is the cap's key: the kind and the owner, under a prefix of their own.
func ownerNoticeBudgetKey(kind ownerNoticeKind, adminUserID uuid.UUID) string {
	return "owner-notice:" + string(kind) + ":" + adminUserID.String()
}

// Submit hands one notice to the queue WITHOUT BLOCKING and returns. When the queue will not take it --
// full, or closed by the drain -- it writes the notice's ONE undelivered row here, synchronously, and
// sends nothing. It never panics and never returns an error: the event it is about has committed.
func (o *OwnerNotices) Submit(ctx context.Context, n OwnerNotice) {
	base := context.WithoutCancel(ctx)
	defer func() {
		if recover() != nil {
			o.log.ErrorContext(base, ownerNoticeLogPanicked, "kind", string(n.Kind), "tenant_id", n.TenantID, "employee_id", n.EmployeeID)
		}
	}()
	l, ok := ownerNoticeLetters[n.Kind]
	if !ok {
		o.log.ErrorContext(base, "owner notice: an unknown kind was submitted; no owner was e-mailed",
			"tenant_id", n.TenantID, "employee_id", n.EmployeeID)
		return
	}
	accepted, closing := o.offer(ownerNoticeJob{base: base, n: n})
	if accepted {
		return
	}
	reason, state := ownerReasonQueueFull, "full"
	if closing {
		reason, state = ownerReasonQueueShut, "closing"
	}
	o.log.WarnContext(base, "owner notice: the queue did not take the notice, so no owner was e-mailed",
		"kind", string(n.Kind), "tenant_id", n.TenantID, "employee_id", n.EmployeeID, "queue", state)
	// The request's own row context, not the drain's: a request still in flight during shutdown runs
	// inside the HTTP drain, which outlasts this queue's (the reset outbox's fallback rule).
	rctx, cancel := context.WithTimeout(base, ownerNoticeRecordGrace)
	defer cancel()
	o.write(rctx, n, l.undelivered, ownerNoticeDetail{Outcome: "undelivered", Reason: reason})
}

// offer hands one job over without blocking; closing says the queue is closed.
func (o *OwnerNotices) offer(j ownerNoticeJob) (accepted, closing bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return false, true
	}
	select {
	case o.jobs <- j:
		return true, false
	default:
		return false, false
	}
}

// ownerJobState is what a job has decided, shared by its send goroutines and its fault path.
type ownerJobState struct {
	mu          sync.Mutex
	jobDecided  bool // the job's one owner-less row has begun
	owners      []uuid.UUID
	ownersKnown bool
	started     []bool // a send goroutine owns this owner's row
	decided     []bool // this owner's row has begun
}

// decide marks owner i's row as begun and reports whether it was not before (one row per owner).
func (s *ownerJobState) decide(i int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.decided[i] {
		return false
	}
	s.decided[i] = true
	return true
}

// handle runs one job to its rows. A panic ends the job, never the worker (this goroutine is under no
// Recoverer: re-raising would kill the process and the queue with it). The value is never logged.
func (o *OwnerNotices) handle(j ownerNoticeJob) {
	l := ownerNoticeLetters[j.n.Kind]
	st := &ownerJobState{}
	if !o.contain(j, func() { o.run(j, l, st) }) {
		return
	}
	// The fault rows are themselves contained: a recorder that panics too is logged and the worker
	// moves on.
	o.contain(j, func() { o.faultRows(j, l, st) })
}

// contain runs f and reports whether it PANICKED, logging a fixed sentence with the job's ids.
func (o *OwnerNotices) contain(j ownerNoticeJob, f func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
			o.log.ErrorContext(j.base, ownerNoticeLogPanicked,
				"kind", string(j.n.Kind), "tenant_id", j.n.TenantID, "employee_id", j.n.EmployeeID)
		}
	}()
	f()
	return false
}

// faultRows writes, after a panic in the job itself, the rows nothing else will: one owner-less row when
// the owners were not known yet, else a row for every owner no send goroutine took.
func (o *OwnerNotices) faultRows(j ownerNoticeJob, l ownerNoticeLetter, st *ownerJobState) {
	st.mu.Lock()
	if !st.ownersKnown {
		first := !st.jobDecided
		st.jobDecided = true
		st.mu.Unlock()
		if first {
			o.recordWorker(j, l.undelivered, ownerNoticeDetail{Outcome: "undelivered", Reason: ownerReasonJobFault})
		}
		return
	}
	var left []int
	for i := range st.owners {
		if !st.started[i] && !st.decided[i] {
			st.decided[i] = true
			left = append(left, i)
		}
	}
	owners := st.owners
	st.mu.Unlock()
	for _, i := range left {
		o.recordOwner(j, l.undelivered, owners[i], ownerNoticeDetail{Outcome: "undelivered", Reason: ownerReasonFault})
	}
}

// run is one job: the drain's check, the owners' read, then each owner in the resolver's order, at most
// ownerNoticeSendsPerJob at once, inside the job's grace and the drain's send phase.
func (o *OwnerNotices) run(j ownerNoticeJob, l ownerNoticeLetter, st *ownerJobState) {
	jobless := func(reason string) {
		st.mu.Lock()
		first := !st.jobDecided
		st.jobDecided = true
		st.mu.Unlock()
		if first {
			o.recordWorker(j, l.undelivered, ownerNoticeDetail{Outcome: "undelivered", Reason: reason})
		}
	}
	if o.sendsStopped.Err() != nil {
		jobless(ownerReasonNotStarted)
		return
	}
	jobCtx, cancelJob := boundedBy(j.base, o.jobGrace, o.sendsStopped)
	defer cancelJob()

	readCtx, cancelRead := context.WithTimeout(jobCtx, o.sendGrace)
	owners, err := o.dir.NoticeOwners(readCtx, j.n.TenantID)
	cancelRead()
	if err != nil {
		o.log.ErrorContext(j.base, "owner notice: reading the business's owners failed",
			"kind", string(j.n.Kind), "tenant_id", j.n.TenantID, "employee_id", j.n.EmployeeID, "err_type", fmt.Sprintf("%T", err))
		jobless(ownerReasonOwnersRead)
		return
	}
	if len(owners) == 0 {
		jobless(ownerReasonNoOwners)
		return
	}
	st.mu.Lock()
	st.owners, st.ownersKnown = owners, true
	st.started, st.decided = make([]bool, len(owners)), make([]bool, len(owners))
	st.mu.Unlock()

	slots := make(chan struct{}, ownerNoticeSendsPerJob)
	var wg sync.WaitGroup
	for i, id := range owners {
		select {
		case slots <- struct{}{}:
		case <-jobCtx.Done():
		}
		if jobCtx.Err() != nil {
			break
		}
		st.mu.Lock()
		st.started[i] = true
		st.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			o.ownerTurn(jobCtx, j, l, st, i, id)
		}()
	}
	wg.Wait()

	// Every owner no send was started for: the job's time or the drain cut it.
	reason := o.cutReason()
	for i, id := range owners {
		st.mu.Lock()
		skip := st.started[i]
		st.mu.Unlock()
		if !skip && st.decide(i) {
			o.recordOwner(j, l.undelivered, id, ownerNoticeDetail{Outcome: "undelivered", Reason: reason})
		}
	}
}

// cutReason names what ended a job early: the drain, or the job's own grace.
func (o *OwnerNotices) cutReason() string {
	if o.sendsStopped.Err() != nil {
		return ownerReasonDrainEnded
	}
	return ownerReasonTimeUp
}

// ownerTurn is one owner's notice, contained: a panic before the owner's row began becomes that owner's
// fault row (a goroutine's panic would otherwise kill the process).
func (o *OwnerNotices) ownerTurn(jobCtx context.Context, j ownerNoticeJob, l ownerNoticeLetter, st *ownerJobState, i int, id uuid.UUID) {
	if !o.contain(j, func() { o.notifyOwner(jobCtx, j, l, st, i, id) }) {
		return
	}
	o.contain(j, func() {
		if st.decide(i) {
			o.recordOwner(j, l.undelivered, id, ownerNoticeDetail{Outcome: "undelivered", Reason: ownerReasonFault})
		}
	})
}

// notifyOwner sends one owner's e-mail and writes their one row.
func (o *OwnerNotices) notifyOwner(jobCtx context.Context, j ownerNoticeJob, l ownerNoticeLetter, st *ownerJobState, i int, id uuid.UUID) {
	ids := []any{"kind", string(j.n.Kind), "tenant_id", j.n.TenantID, "employee_id", j.n.EmployeeID, "admin_user_id", id}
	undelivered := func(d ownerNoticeDetail) {
		if st.decide(i) {
			d.Outcome = "undelivered"
			o.recordOwner(j, l.undelivered, id, d)
		}
	}
	if o.send == nil {
		undelivered(ownerNoticeDetail{Reason: ownerReasonOff})
		return
	}
	// ONE LOCKED STEP (TryCharge): two jobs for one owner must not both read "allowed".
	if within, n := o.limiter.TryCharge(ownerNoticeBudgetKey(j.n.Kind, id)); !within {
		if o.limiter.FirstOverLimit(n) {
			o.log.WarnContext(j.base, "owner notice rate limited", ids...)
		}
		undelivered(ownerNoticeDetail{Reason: ownerReasonLimited})
		return
	}
	sendCtx, cancelSend := context.WithTimeout(jobCtx, o.sendGrace)
	defer cancelSend()
	to, err := o.dir.NoticeRecipient(sendCtx, j.n.TenantID, id)
	if err != nil {
		if jobCtx.Err() != nil {
			undelivered(ownerNoticeDetail{Reason: o.cutReason()})
			return
		}
		o.log.ErrorContext(j.base, "owner notice: reading the owner's address failed", append(ids, "err_type", fmt.Sprintf("%T", err))...)
		undelivered(ownerNoticeDetail{Reason: ownerReasonReadFailed})
		return
	}
	if to == "" {
		undelivered(ownerNoticeDetail{Reason: ownerReasonNoAddress})
		return
	}
	m, err := l.render(sendCtx, o.baseURL, j.n)
	if err != nil {
		o.log.ErrorContext(j.base, "owner notice: building the e-mail failed", ids...)
		undelivered(ownerNoticeDetail{Reason: ownerReasonRenderFail})
		return
	}
	m.To = to
	receipt, err := o.send.SendExempt(sendCtx, m)
	cancelSend()
	if err != nil {
		if jobCtx.Err() != nil {
			undelivered(ownerNoticeDetail{Reason: o.cutReason()})
			return
		}
		// THE CHANNEL'S TEXT IS NOT LOGGED: its class and reply code, or its Go type.
		d := ownerNoticeDetail{Reason: ownerReasonSendFailed}
		extra := []any{"err_type", fmt.Sprintf("%T", err)}
		if class, code, ok := sendErrorClass(err); ok {
			d.Class, d.SMTPCode = class, code
			extra = append(extra, "class", class, "smtp_code", code)
		}
		o.log.ErrorContext(j.base, "owner notice: delivery failed", append(ids, extra...)...)
		undelivered(d)
		return
	}
	o.log.InfoContext(j.base, "owner notice: the relay accepted the e-mail", append(ids, "message_id", receipt.MessageID)...)
	if st.decide(i) {
		o.recordOwner(j, l.sent, id, ownerNoticeDetail{Outcome: "sent"})
	}
}

// recordOwner writes one owner's row; recordWorker a job's owner-less row. Both on their OWN bounded
// context (never the send's), which also ends when the drain's budget is spent.
func (o *OwnerNotices) recordOwner(j ownerNoticeJob, action string, id uuid.UUID, d ownerNoticeDetail) {
	d.AdminUserID = id.String()
	o.recordWorker(j, action, d)
}

func (o *OwnerNotices) recordWorker(j ownerNoticeJob, action string, d ownerNoticeDetail) {
	ctx, cancel := boundedBy(j.base, ownerNoticeRecordGrace, o.budgetSpent)
	defer cancel()
	o.write(ctx, j.n, action, d)
}

// write is the one audit insert: target the employee, no actor (the event is the employee's own).
func (o *OwnerNotices) write(ctx context.Context, n OwnerNotice, action string, d ownerNoticeDetail) {
	if _, err := o.audit.Record(ctx, audit.Event{
		TenantID: n.TenantID,
		Action:   action,
		Target:   n.EmployeeID.String(),
		Detail:   d,
	}); err != nil {
		o.log.ErrorContext(ctx, audit.EventWriteFailed, "action", action, "err_type", fmt.Sprintf("%T", err))
	}
}

// Drain stops the queue taking notices and waits for the workers inside ctx's deadline, in the reset
// outbox's two phases (cmd/tappa passes ResetDrainGrace): sends continue until ResetDrainWriteReserve
// before the deadline, then the sends in flight end and every owner not sent -- and every job not
// started -- gets its undelivered row inside the reserve. When ctx is done Drain RETURNS (an error),
// abandoning a row still being written: a drain that outlived its budget would push the deferred
// closes (the plaque-key wipe, ADR 0017) behind the kill. A second Drain waits for the first's workers.
func (o *OwnerNotices) Drain(ctx context.Context) error {
	o.mu.Lock()
	if !o.closed {
		o.closed = true
		close(o.jobs)
	}
	o.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		cut := time.AfterFunc(time.Until(deadline)-ResetDrainWriteReserve, o.stopSending)
		defer cut.Stop()
	}
	select {
	case <-o.done:
		return nil
	case <-ctx.Done():
		o.stopSending()
		o.spendBudget()
		o.log.ErrorContext(ctx, "owner notice: the queue did not drain within its budget; its remaining rows are lost",
			"queued_notices", len(o.jobs))
		return fmt.Errorf("handler: the owner notice queue did not drain within its budget: %w", ctx.Err())
	}
}

// ownerZone is the business's zone for the notice's day; a name the runtime cannot load reads UTC (an
// unloadable zone must not cost the owners their notice).
func ownerZone(name string) *time.Location {
	if strings.TrimSpace(name) == "" {
		return time.UTC
	}
	z, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return z
}
