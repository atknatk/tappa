package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/mail"
)

// The owners' notices queue (ownernotice.go) over fakes: every named branch ends in its rows, and no row
// or log line carries an address.

// ownerAddr is a placeholder address under the reserved .test domain, built at run time.
func ownerAddr(label string) string { return label + "@" + "owners.example.test" }

// fakeOwnerDir is the owners' read and their addresses. gate, when set, holds every NoticeOwners until
// it is closed (or the context ends); panicOwners makes NoticeOwners panic.
type fakeOwnerDir struct {
	mu          sync.Mutex
	owners      map[uuid.UUID][]uuid.UUID
	addresses   map[uuid.UUID]string
	ownersErr   error
	gate        chan struct{}
	entered     chan struct{}
	panicOwners bool
}

func (d *fakeOwnerDir) NoticeOwners(ctx context.Context, tenantID uuid.UUID) ([]uuid.UUID, error) {
	d.mu.Lock()
	gate, entered, fail, boom := d.gate, d.entered, d.ownersErr, d.panicOwners
	owners := d.owners[tenantID]
	d.mu.Unlock()
	if boom {
		panic("owners read panicked for " + ownerAddr("leak"))
	}
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return owners, fail
}

func (d *fakeOwnerDir) NoticeRecipient(_ context.Context, _ uuid.UUID, id uuid.UUID) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.addresses[id], nil
}

// fakeOwnerSender keeps what it was handed. block holds every send until the context ends; panicFor
// makes the send to that address panic with the address in the value.
type fakeOwnerSender struct {
	mu       sync.Mutex
	sent     []mail.Message
	block    bool
	panicFor string
	started  chan struct{}
}

func (s *fakeOwnerSender) SendExempt(ctx context.Context, m mail.Message) (mail.Receipt, error) {
	s.mu.Lock()
	block, boom, started := s.block, s.panicFor, s.started
	s.mu.Unlock()
	if boom != "" && m.To == boom {
		panic("send panicked for " + m.To)
	}
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if block {
		<-ctx.Done()
		return mail.Receipt{}, ctx.Err()
	}
	s.mu.Lock()
	s.sent = append(s.sent, m)
	s.mu.Unlock()
	return mail.Receipt{MessageID: "fake-message-id"}, nil
}

func (s *fakeOwnerSender) to() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, m := range s.sent {
		out = append(out, m.To)
	}
	return out
}

// ownerFixture is one business with owners, a queue over fakes, and the log it writes.
type ownerFixture struct {
	tenant, employee uuid.UUID
	owners           []uuid.UUID
	dir              *fakeOwnerDir
	send             *fakeOwnerSender
	trail            *fakeTrail
	logs             *syncBuffer
	q                *OwnerNotices
}

func newOwnerFixture(t *testing.T, owners int, sender bool) *ownerFixture {
	t.Helper()
	f := &ownerFixture{tenant: uuid.New(), employee: uuid.New(), send: &fakeOwnerSender{}, trail: &fakeTrail{}, logs: &syncBuffer{}}
	f.dir = &fakeOwnerDir{owners: map[uuid.UUID][]uuid.UUID{}, addresses: map[uuid.UUID]string{}}
	for i := range owners {
		id := uuid.New()
		f.owners = append(f.owners, id)
		f.dir.addresses[id] = ownerAddr(fmt.Sprintf("owner%d", i))
	}
	f.dir.owners[f.tenant] = f.owners
	var s ownerNoticeSender
	if sender {
		s = f.send
	}
	log := slog.New(slog.NewJSONHandler(f.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	q, err := newOwnerNotices(s, f.dir, adminTestConfig().BaseURL, f.trail, log)
	if err != nil {
		t.Fatal(err)
	}
	f.q = q
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = q.Drain(ctx)
	})
	return f
}

func (f *ownerFixture) notice() OwnerNotice {
	return OwnerNotice{Kind: ownerNoticeDeletionRequest, TenantID: f.tenant, EmployeeID: f.employee,
		EmployeeName: "Maria Borg", At: time.Now(), Zone: time.UTC}
}

// drain waits for every queued job's rows.
func (f *ownerFixture) drain(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.q.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
}

// rows is every outcome row, its detail decoded; it also checks the shape every row shares.
func (f *ownerFixture) rows(t *testing.T) []ownerNoticeDetailRow {
	t.Helper()
	var out []ownerNoticeDetailRow
	for _, e := range f.trail.eventsSnapshot() {
		if e.Action != ActionEmployeeDeletionNoticeSent && e.Action != ActionEmployeeDeletionNoticeUndelivered {
			t.Errorf("unexpected action %q", e.Action)
			continue
		}
		if e.TenantID != f.tenant || e.Target != f.employee.String() || e.ActorID != nil {
			t.Errorf("row %q: tenant %v target %q actor %v, want the business, the employee and no actor", e.Action, e.TenantID, e.Target, e.ActorID)
		}
		b, err := json.Marshal(e.Detail)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("@")) {
			t.Errorf("row %q carries an '@': %s", e.Action, b)
		}
		var d ownerNoticeDetailRow
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatal(err)
		}
		d.action = e.Action
		out = append(out, d)
	}
	return out
}

type ownerNoticeDetailRow struct {
	action      string
	Outcome     string `json:"outcome"`
	AdminUserID string `json:"admin_user_id"`
	Reason      string `json:"reason"`
}

func countNoticeRows(rows []ownerNoticeDetailRow, action, reason string) int {
	n := 0
	for _, r := range rows {
		if r.action == action && (reason == "" || r.Reason == reason) {
			n++
		}
	}
	return n
}

func (f *ownerFixture) noAddressInLogs(t *testing.T) {
	t.Helper()
	if s := f.logs.String(); strings.Contains(s, "@") {
		t.Errorf("a log line carries an '@':\n%s", s)
	}
}

// TestOwnerNotices_EveryOwnerIsEMailedAndEachRowNamesOnlyTheOwnersID: one job, five owners (more than the
// four sends at once): each owner's address gets one e-mail with the fixed subject, the employee's name
// and no address in the body, and each owner one sent row naming their id and nothing else.
func TestOwnerNotices_EveryOwnerIsEMailedAndEachRowNamesOnlyTheOwnersID(t *testing.T) {
	f := newOwnerFixture(t, 5, true)
	f.q.Submit(context.Background(), f.notice())
	f.drain(t)
	got := f.send.to()
	if len(got) != 5 {
		t.Fatalf("%d e-mails, want 5", len(got))
	}
	for _, m := range f.send.sent {
		if m.Subject != "An employee asked to delete their Taptime account" {
			t.Errorf("subject %q", m.Subject)
		}
		if !strings.Contains(m.Text, "Maria Borg asked to delete their Taptime account") {
			t.Errorf("the body does not name the employee:\n%s", m.Text)
		}
		if strings.Contains(m.Text, "@") || strings.Contains(m.HTML, "@") {
			t.Errorf("the body carries an '@'")
		}
	}
	rows := f.rows(t)
	if len(rows) != 5 || countNoticeRows(rows, ActionEmployeeDeletionNoticeSent, "") != 5 {
		t.Fatalf("rows %+v, want 5 sent", rows)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.AdminUserID] = true
	}
	for _, id := range f.owners {
		if !seen[id.String()] {
			t.Errorf("owner %s has no row", id)
		}
	}
	f.noAddressInLogs(t)
}

// TestOwnerNotices_AFullQueueWritesOneRowAtOnceAndSendsNothing: both workers held in the owners' read,
// the queue's 32 places taken; the next Submit returns with its one undelivered row (no owner named)
// already written, and that notice is never sent.
func TestOwnerNotices_AFullQueueWritesOneRowAtOnceAndSendsNothing(t *testing.T) {
	f := newOwnerFixture(t, 1, true)
	f.dir.gate, f.dir.entered = make(chan struct{}), make(chan struct{}, ownerNoticeWorkers)
	for range ownerNoticeWorkers {
		f.q.Submit(context.Background(), f.notice())
	}
	for range ownerNoticeWorkers {
		<-f.dir.entered
	}
	f.dir.mu.Lock()
	f.dir.entered = nil
	f.dir.mu.Unlock()
	for range ownerNoticeQueueSize {
		f.q.Submit(context.Background(), f.notice())
	}
	if n := f.trail.total(); n != 0 {
		t.Fatalf("CONTROL FAILED: %d rows before the queue is full", n)
	}
	f.q.Submit(context.Background(), f.notice())
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].Reason != ownerReasonQueueFull || rows[0].AdminUserID != "" || rows[0].Outcome != "undelivered" {
		t.Fatalf("rows %+v, want one owner-less undelivered row (queue full)", rows)
	}
	close(f.dir.gate)
	f.drain(t)
	// Every notice the queue took ends in its owner's one row (sent, or past the owner's daily cap);
	// the overflowing one has only its queue-full row.
	taken := ownerNoticeWorkers + ownerNoticeQueueSize
	rows = f.rows(t)
	if len(rows) != taken+1 || countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonQueueFull) != 1 {
		t.Fatalf("%d rows, want %d (one per notice taken) + 1 (the queue-full row)", len(rows), taken)
	}
	if n := len(f.send.to()); n != ownerNoticeLimit {
		t.Fatalf("%d e-mails, want the owner's daily %d", n, ownerNoticeLimit)
	}
}

// TestOwnerNotices_ASubmitAfterTheDrainIsRefusedWithARow: the queue closed, a late notice neither panics
// nor sends; its one undelivered row says the queue was closed.
func TestOwnerNotices_ASubmitAfterTheDrainIsRefusedWithARow(t *testing.T) {
	f := newOwnerFixture(t, 2, true)
	f.drain(t)
	f.q.Submit(context.Background(), f.notice())
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].Reason != ownerReasonQueueShut || rows[0].AdminUserID != "" {
		t.Fatalf("rows %+v, want one owner-less undelivered row (queue closed)", rows)
	}
	if n := len(f.send.to()); n != 0 {
		t.Fatalf("%d e-mails after the drain, want 0", n)
	}
}

// TestOwnerNotices_APanicBecomesAnUndeliveredRowAndTheWorkerLives: a send that panics with the address
// in its value gives that owner a fault row and the others their sent rows; an owners' read that panics
// gives the job one owner-less fault row; the panic's value is never logged; the workers go on.
func TestOwnerNotices_APanicBecomesAnUndeliveredRowAndTheWorkerLives(t *testing.T) {
	f := newOwnerFixture(t, 3, true)
	setPanic := func(sendTo string, owners bool) {
		f.send.mu.Lock()
		f.send.panicFor = sendTo
		f.send.mu.Unlock()
		f.dir.mu.Lock()
		f.dir.panicOwners = owners
		f.dir.mu.Unlock()
	}
	setPanic(f.dir.addresses[f.owners[1]], false)
	f.q.Submit(context.Background(), f.notice())
	f.drainOne(t, 3)
	setPanic("", true)
	f.q.Submit(context.Background(), f.notice())
	f.drainOne(t, 4)
	setPanic("", false)
	f.q.Submit(context.Background(), f.notice())
	f.drain(t)

	rows := f.rows(t)
	if n := countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonFault); n != 1 {
		t.Errorf("%d per-owner fault rows, want 1", n)
	}
	if n := countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonJobFault); n != 1 {
		t.Errorf("%d job fault rows, want 1", n)
	}
	if n := countNoticeRows(rows, ActionEmployeeDeletionNoticeSent, ""); n != 2+3 {
		t.Errorf("%d sent rows, want 5 (two owners of the first job, all three of the last)", n)
	}
	if len(rows) != 7 {
		t.Errorf("%d rows, want 7", len(rows))
	}
	if !strings.Contains(f.logs.String(), ownerNoticeLogPanicked) {
		t.Error("the panic left no line")
	}
	f.noAddressInLogs(t)
}

// drainOne waits until the trail holds want rows (a job's rows are written by a worker).
func (f *ownerFixture) drainOne(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.trail.total() < want {
		if time.Now().After(deadline) {
			t.Fatalf("%d rows after 5 s, want %d", f.trail.total(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestOwnerNotices_TheJobsGraceCutsEveryOwnerNotYetSent: six owners, a relay that never answers, a job
// grace of 300 ms: the four sends in flight and the two never started all end undelivered with the
// time-up reason, one row each, none sent.
func TestOwnerNotices_TheJobsGraceCutsEveryOwnerNotYetSent(t *testing.T) {
	f := newOwnerFixture(t, 6, true)
	f.send.block = true
	f.q.jobGrace = 300 * time.Millisecond
	start := time.Now()
	f.q.Submit(context.Background(), f.notice())
	f.drainOne(t, 6)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the job took %v, want about its 300 ms grace", took)
	}
	rows := f.rows(t)
	if len(rows) != 6 || countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonTimeUp) != 6 {
		t.Fatalf("rows %+v, want 6 undelivered (time up)", rows)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.AdminUserID] = true
	}
	if len(seen) != 6 {
		t.Errorf("%d distinct owners in the rows, want 6", len(seen))
	}
}

// TestOwnerNotices_TheDrainCutsTheSendsAndRecordsTheJobsNotStarted: two jobs in flight against a relay
// that never answers, three queued; a drain of 1.5 s stops the sends at its 1 s reserve: each owner in
// flight gets the drain's reason, each queued job one owner-less "not started" row, and Drain returns
// inside its budget.
func TestOwnerNotices_TheDrainCutsTheSendsAndRecordsTheJobsNotStarted(t *testing.T) {
	f := newOwnerFixture(t, 1, true)
	f.send.block = true
	f.send.started = make(chan struct{}, ownerNoticeWorkers)
	for range ownerNoticeWorkers {
		f.q.Submit(context.Background(), f.notice())
	}
	for range ownerNoticeWorkers {
		<-f.send.started
	}
	for range 3 {
		f.q.Submit(context.Background(), f.notice())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := f.q.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if took := time.Since(start); took < 400*time.Millisecond || took > 1500*time.Millisecond {
		t.Errorf("the drain took %v, want the sends cut at about 500 ms and the rows inside the budget", took)
	}
	rows := f.rows(t)
	if n := countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonDrainEnded); n != ownerNoticeWorkers {
		t.Errorf("%d drain-ended rows, want %d", n, ownerNoticeWorkers)
	}
	if n := countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonNotStarted); n != 3 {
		t.Errorf("%d not-started rows, want 3", n)
	}
	if len(rows) != ownerNoticeWorkers+3 {
		t.Errorf("rows %+v", rows)
	}
}

// TestOwnerNotices_TheDrainCuttingTheOwnersReadSaysTheProcessStopped: a job whose owners' read the drain
// cuts gets one owner-less row saying the process stopped -- not that the owners could not be read.
func TestOwnerNotices_TheDrainCuttingTheOwnersReadSaysTheProcessStopped(t *testing.T) {
	f := newOwnerFixture(t, 1, true)
	f.dir.gate, f.dir.entered = make(chan struct{}), make(chan struct{}, 1)
	f.q.Submit(context.Background(), f.notice())
	<-f.dir.entered
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := f.q.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	rows := f.rows(t)
	if len(rows) != 1 || rows[0].Reason != ownerReasonOwnersCut || rows[0].AdminUserID != "" {
		t.Fatalf("rows %+v, want one owner-less row: %q", rows, ownerReasonOwnersCut)
	}
	if sent := f.send.to(); len(sent) != 0 {
		t.Fatalf("%d e-mails, want 0", len(sent))
	}
}

// blockingTrail is a recorder whose every write waits for its context to end, as a stalled Postgres.
type blockingTrail struct{}

func (b *blockingTrail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	<-ctx.Done()
	return uuid.Nil, ctx.Err()
}

// TestOwnerNotices_DrainReturnsWhenItsBudgetIsSpent: a relay that never answers and a trail that never
// writes: Drain returns an error at its deadline (not after a 5 s row grace), and the workers are gone
// within a second after it.
func TestOwnerNotices_DrainReturnsWhenItsBudgetIsSpent(t *testing.T) {
	dir := &fakeOwnerDir{owners: map[uuid.UUID][]uuid.UUID{}, addresses: map[uuid.UUID]string{}}
	tenant, owner := uuid.New(), uuid.New()
	dir.owners[tenant] = []uuid.UUID{owner}
	dir.addresses[owner] = ownerAddr("stalled")
	send := &fakeOwnerSender{block: true}
	q, err := newOwnerNotices(send, dir, adminTestConfig().BaseURL, &blockingTrail{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	q.Submit(context.Background(), OwnerNotice{Kind: ownerNoticeDeletionRequest, TenantID: tenant, EmployeeID: uuid.New(), At: time.Now()})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = q.Drain(ctx)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain = %v, want the deadline", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the drain returned after %v, want about 300 ms", took)
	}
	select {
	case <-q.done:
	case <-time.After(time.Second):
		t.Fatal("the workers were still running a second after the spent drain")
	}
}

// TestOwnerNotices_TheDailyCapIsExactPerOwnerAndKind: 25 notices for a business with two owners: each
// owner is e-mailed exactly ownerNoticeLimit times; the other notices end undelivered with the cap's
// reason (one rate-limited line per owner); an owner of another business is still e-mailed.
func TestOwnerNotices_TheDailyCapIsExactPerOwnerAndKind(t *testing.T) {
	f := newOwnerFixture(t, 2, true)
	other, otherOwner := uuid.New(), uuid.New()
	f.dir.owners[other] = []uuid.UUID{otherOwner}
	f.dir.addresses[otherOwner] = ownerAddr("elsewhere")
	const n = 25
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() { defer wg.Done(); f.q.Submit(context.Background(), f.notice()) }()
	}
	wg.Wait()
	o := f.notice()
	o.TenantID = other
	f.q.Submit(context.Background(), o)
	f.drain(t)

	perOwner := map[string]int{}
	for _, to := range f.send.to() {
		perOwner[to]++
	}
	for _, id := range f.owners {
		if got := perOwner[f.dir.addresses[id]]; got != ownerNoticeLimit {
			t.Errorf("owner %s e-mailed %d times, want %d", id, got, ownerNoticeLimit)
		}
	}
	if perOwner[ownerAddr("elsewhere")] != 1 {
		t.Error("another business's owner was not e-mailed")
	}
	limited := 0
	for _, e := range f.trail.eventsSnapshot() {
		if b, _ := json.Marshal(e.Detail); strings.Contains(string(b), ownerReasonLimited) {
			limited++
		}
	}
	if limited != 2*(n-ownerNoticeLimit) {
		t.Errorf("%d rate-limited rows, want %d", limited, 2*(n-ownerNoticeLimit))
	}
	if c := strings.Count(f.logs.String(), "owner notice rate limited"); c != 2 {
		t.Errorf("%d rate-limited lines, want 2 (one per owner)", c)
	}
}

// TestOwnerNotices_WithoutEMailEveryOwnerGetsTheOffRow: a deployment that sends no e-mail reads the
// owners and writes each one's row saying so, sending nothing.
func TestOwnerNotices_WithoutEMailEveryOwnerGetsTheOffRow(t *testing.T) {
	f := newOwnerFixture(t, 3, false)
	f.q.Submit(context.Background(), f.notice())
	f.drain(t)
	rows := f.rows(t)
	if len(rows) != 3 || countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonOff) != 3 {
		t.Fatalf("rows %+v, want 3 off rows", rows)
	}
	if len(f.send.to()) != 0 {
		t.Fatal("an e-mail was sent with e-mail off")
	}
}

// TestOwnerNotices_NoOwnersOrAFailedReadIsOneRow: a business with no owner who can sign in, and an
// owners' read that fails, each end in one owner-less undelivered row.
func TestOwnerNotices_NoOwnersOrAFailedReadIsOneRow(t *testing.T) {
	f := newOwnerFixture(t, 0, true)
	f.q.Submit(context.Background(), f.notice())
	f.drainOne(t, 1)
	f.dir.mu.Lock()
	f.dir.ownersErr = errors.New("read failed")
	f.dir.mu.Unlock()
	f.q.Submit(context.Background(), f.notice())
	f.drain(t)
	rows := f.rows(t)
	if len(rows) != 2 || countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonNoOwners) != 1 ||
		countNoticeRows(rows, ActionEmployeeDeletionNoticeUndelivered, ownerReasonOwnersRead) != 1 {
		t.Fatalf("rows %+v, want one no-owners row and one read-failed row", rows)
	}
}

// TestOwnerNotices_ATrippedBreakerDoesNotStopIt [G37-d, the unit half]: with the process-wide breaker
// refusing (300 sends in the hour; the control: Refusing says true), the notice still reaches the sender
// behind the breaker and the row says sent.
func TestOwnerNotices_ATrippedBreakerDoesNotStopIt(t *testing.T) {
	p := &breakerProbe{}
	b, err := mail.NewBreaker(p, mail.BreakerConfig{Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	fillBreaker(t, b, 300)
	if !b.Refusing() {
		t.Fatal("CONTROL FAILED: the breaker passes after 300 sends")
	}
	tenant, owner := uuid.New(), uuid.New()
	dir := &fakeOwnerDir{owners: map[uuid.UUID][]uuid.UUID{tenant: {owner}}, addresses: map[uuid.UUID]string{owner: ownerAddr("tripped")}}
	trail := &fakeTrail{}
	q, err := NewEmailOwnerNotices(b, true, dir, adminTestConfig().BaseURL, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	q.Submit(context.Background(), OwnerNotice{Kind: ownerNoticeDeletionRequest, TenantID: tenant, EmployeeID: uuid.New(), At: time.Now()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := q.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if p.count() != 301 || p.last().To != ownerAddr("tripped") {
		t.Fatalf("%d messages reached the sender, want 301 with the last to the owner", p.count())
	}
	if trail.count(ActionEmployeeDeletionNoticeSent) != 1 || trail.total() != 1 {
		t.Fatalf("%d sent of %d rows, want 1 of 1", trail.count(ActionEmployeeDeletionNoticeSent), trail.total())
	}
}

// TestOwnerNotices_EveryKindHasItsTwoActionsAndItsLetter: each kind's row names two distinct actions of
// its own and renders under the test base; an unknown kind is refused at Submit with no row.
func TestOwnerNotices_EveryKindHasItsTwoActionsAndItsLetter(t *testing.T) {
	actions := map[string]bool{}
	for kind, l := range ownerNoticeLetters {
		if l.sent == "" || l.undelivered == "" || l.sent == l.undelivered || actions[l.sent] || actions[l.undelivered] {
			t.Errorf("kind %s: actions %q / %q are not two of its own", kind, l.sent, l.undelivered)
		}
		actions[l.sent], actions[l.undelivered] = true, true
		if _, err := l.render(context.Background(), adminTestConfig().BaseURL, OwnerNotice{Kind: kind, At: time.Now()}); err != nil {
			t.Errorf("kind %s does not render: %v", kind, err)
		}
	}
	f := newOwnerFixture(t, 1, true)
	f.q.Submit(context.Background(), OwnerNotice{Kind: "no_such_kind", TenantID: f.tenant, EmployeeID: f.employee})
	f.drain(t)
	if f.trail.total() != 0 || len(f.send.to()) != 0 {
		t.Fatal("an unknown kind wrote a row or sent an e-mail")
	}
}
