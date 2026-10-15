package invite

// queue_db_test.go — K-17's invitation queue worker (M11 API-6; ADR 0028 §9) against REAL
// Postgres: the code minted at send time and never held by the queue, two workers ("pods") that
// mint one request once, the lease that a re-queue takes away, the panel's limits delaying (never
// refusing) a request, the address change racing a mint (K-15), and the link the queue sends
// activating through ADR 0026's wizard path. The relay is a fake (fakeMailSink) — no e-mail leaves
// the process; the database, the locks and the trail are real.
//
// The employees are opened by internal/domain/tenant's own API write (PutEmployeeTx), so the
// queue is fed exactly as /v1 feeds it. Nothing is cleaned up (tappa_app holds no DELETE on
// employees, employee_invites or audit_log): fresh ids and addresses keep runs apart.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/internal/webhook"
)

// queueRig is one business, the API's employee domain and a queue worker around a fake relay.
type queueRig struct {
	t      *testing.T
	m      *Manager
	d      *db.DB
	staff  *tenant.Staff
	rec    *audit.Recorder
	sink   *fakeMailSink
	q      *Queue
	tenant uuid.UUID
	key    uuid.UUID // the API key the writes are attributed to (actor_id; no FK)
}

func newQueueRig(t *testing.T) *queueRig {
	t.Helper()
	m, d := testManager(t)
	rec, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := tenant.NewStaff(d, rec, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	sink := &fakeMailSink{id: "<msg@relay.example.test>"}
	q, err := NewQueue(m, sink, rec)
	if err != nil {
		t.Fatal(err)
	}
	f := newMailFixture(t, d, "Queue Test Ltd", ptrBool(true))
	return &queueRig{t: t, m: m, d: d, staff: staff, rec: rec, sink: sink, q: q, tenant: f.tenantID, key: uuid.New()}
}

// hire opens an employee through the API's write, with address email (nil: none).
func (r *queueRig) hire(email *string) uuid.UUID {
	r.t.Helper()
	return r.put(apiRefFor("Q"), email).Employee.ID
}

func (r *queueRig) put(ref string, email *string) tenant.EmployeePutResult {
	r.t.Helper()
	var out tenant.EmployeePutResult
	err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = r.staff.PutEmployeeTx(ctx, tx, tenant.EmployeePut{
			TenantID: r.tenant, ActorID: r.key, Via: "api", ExternalRef: ref,
			FirstName: "Maria", LastName: "Borg", Email: email, Active: true,
		}, func(context.Context, tenant.EmployeeWrite) error { return nil })
		return err
	})
	if err != nil {
		r.t.Fatalf("put %s: %v", ref, err)
	}
	return out
}

func apiRefFor(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

func (r *queueRig) count(q string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, q, args...).Scan(&n)
	}); err != nil {
		r.t.Fatalf("count: %v", err)
	}
	return n
}

func (r *queueRig) invites(employee uuid.UUID) int {
	return r.count(`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2`, r.tenant, employee)
}

func (r *queueRig) queued(employee uuid.UUID) int {
	return r.count(`SELECT count(*) FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, r.tenant, employee)
}

func codeOf(t *testing.T, d Delivery) Code {
	t.Helper()
	i := strings.Index(d.ActivationURL, "code=")
	if i < 0 {
		t.Fatalf("a delivery without a code: %q", d.ActivationURL)
	}
	return ParseCode(d.ActivationURL[i+len("code="):])
}

// TestInviteQueue_HoldsNoCode (ADR 0028 §9: "Kuyrukta kod yoktur — şema + log taraması"):
//
//	the schema   invite_requests has exactly the five columns of §9, typed uuid, uuid,
//	             timestamptz, timestamptz, integer -- no column a code or its hash could be
//	             written into
//	the run      the worker, driven through internal/webhook's worker with a capturing logger, sends
//	             two invitations and fails a third (the relay refuses it): the codes the fake relay
//	             received, their hashes and the addresses appear in NO log line, NO audit detail of
//	             the business and NO invite_requests row; the failure IS logged (so the scan read a
//	             log that had lines in it)
//
// The code is minted at send time: before the run no employee_invites row exists for the people.
func TestInviteQueue_HoldsNoCode(t *testing.T) {
	r := newQueueRig(t)
	var cols []string
	if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT column_name || ':' || data_type FROM information_schema.columns
		                            WHERE table_schema = 'public' AND table_name = 'invite_requests' ORDER BY ordinal_position`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			cols = append(cols, c)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	want := "tenant_id:uuid,employee_id:uuid,requested_at:timestamp with time zone,lease_until:timestamp with time zone,attempts:integer"
	if got := strings.Join(cols, ","); got != want {
		t.Fatalf("invite_requests columns %s, want exactly %s", got, want)
	}

	a, b, c := addr("q1"), addr("q2"), addr("q3")
	people := []uuid.UUID{r.hire(a), r.hire(b)}
	for _, p := range people {
		if n := r.invites(p); n != 0 {
			t.Fatalf("a queued person holds %d invitations before the worker ran; the code is minted at send time", n)
		}
	}
	logs := &lockedBuffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	w, err := webhook.NewWorker(log, r.q)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Notice(r.tenant)
	waitFor(t, func() bool { return len(r.sink.calls()) == 2 })
	// The third fails at the relay: an invite.undelivered row and a logged failure.
	r.sink.mu.Lock()
	r.sink.err = errors.New("relay refused")
	r.sink.mu.Unlock()
	third := r.hire(c)
	w.Notice(r.tenant)
	waitFor(t, func() bool { return len(r.sink.calls()) == 3 && strings.Contains(logs.String(), "a job failed") })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Drain(ctx); err != nil {
		t.Fatal(err)
	}

	var secrets []string
	for _, d := range r.sink.calls() {
		code := codeOf(t, d)
		h, err := code.hash(r.m.hmacKey)
		if err != nil {
			t.Fatal(err)
		}
		secrets = append(secrets, code.reveal(), h, d.Recipient.Address)
	}
	var trail, queue []string
	if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		for q, into := range map[string]*[]string{
			`SELECT detail::text FROM audit_log WHERE tenant_id = $1`:                                                           &trail,
			`SELECT concat_ws(',', employee_id, requested_at, lease_until, attempts) FROM invite_requests WHERE tenant_id = $1`: &queue,
		} {
			rows, err := tx.Query(ctx, q, r.tenant)
			if err != nil {
				return err
			}
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					rows.Close()
					return err
				}
				*into = append(*into, s)
			}
			rows.Close()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(trail) == 0 || r.queued(third) != 1 {
		t.Fatalf("the scan read %d trail rows and the failed request is queued %d times; the run did not happen as set up", len(trail), r.queued(third))
	}
	haystacks := map[string]string{"the log": logs.String(), "the trail": strings.Join(trail, "\n"), "the queue": strings.Join(queue, "\n")}
	for where, hay := range haystacks {
		for _, s := range secrets {
			if s != "" && strings.Contains(strings.ToLower(hay), strings.ToLower(s)) {
				t.Errorf("%s carries a code, a code's hash or an address", where)
			}
		}
	}
	if n := r.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'invite.code_emailed' AND detail->>'channel' = 'email_queue'`, r.tenant); n != 2 {
		t.Fatalf("%d invite.code_emailed rows of the queue, want 2", n)
	}
}

// lockedBuffer is a log sink the worker's goroutine writes while the test reads.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// settled is a run that met no failure: nil, or a bare *Later (a request another pod holds, or one
// due later, is not a failure).
func settled(err error) bool {
	_, later := err.(*Later)
	return err == nil || later
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestInviteQueue_TwoPodsMintOnce (K-17: "kira 60 sn; iki pod aynı daveti iki kez basamaz"; -race):
//
//	(a) two workers -- two Queues, as two pods would hold -- drain one business's 10 requests at
//	    the same moment: every person is minted exactly ONE invitation and sent exactly ONE e-mail,
//	    and the queue is empty
//	(b) the lease outlived: pod A claims and stops after its mint (before the renewal); the lease is
//	    made to run out; pod B claims, mints and sends; A resumes -- its renewal finds B's lease and
//	    A's mint is ROLLED BACK: one invitation (B's), one e-mail, nothing of A's committed
//
// MUTANTS (measured; ADR 0028 "API-6 uygulama notu"): the claim's SKIP LOCKED and lease condition
// removed -> (a) mints twice; the renewal not checked -> (b) commits A's mint too.
func TestInviteQueue_TwoPodsMintOnce(t *testing.T) {
	r := newQueueRig(t)
	podB, err := NewQueue(r.m, r.sink, r.rec)
	if err != nil {
		t.Fatal(err)
	}
	people := make([]uuid.UUID, 10)
	for i := range people {
		people[i] = r.hire(addr("pod"))
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, q := range []*Queue{r.q, podB} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := q.RunFor(context.Background(), r.tenant); !settled(err) {
				t.Errorf("RunFor: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	sent := map[uuid.UUID]int{}
	for _, d := range r.sink.calls() {
		sent[d.Invite.EmployeeID]++
	}
	for _, p := range people {
		if n := r.invites(p); n != 1 || sent[p] != 1 || r.queued(p) != 0 {
			t.Errorf("person %s: %d invitations, %d e-mails, %d queued; want 1, 1, 0", p, n, sent[p], r.queued(p))
		}
	}

	// (b) the lease outlived.
	p := r.hire(addr("lease"))
	before := len(r.sink.calls())
	stopped, resume := make(chan struct{}), make(chan struct{})
	var once, resumed sync.Once
	letGo := func() { resumed.Do(func() { close(resume) }) }
	// A failing assertion must not leave A's transaction open: the pool's Close would wait for it.
	t.Cleanup(letGo)
	r.q.afterMint = func(context.Context, uuid.UUID) {
		once.Do(func() {
			close(stopped)
			<-resume
		})
	}
	done := make(chan error, 1)
	go func() { done <- r.q.RunFor(context.Background(), r.tenant) }()
	<-stopped
	if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE invite_requests SET lease_until = clock_timestamp() - interval '1 second' WHERE tenant_id = $1 AND employee_id = $2`, r.tenant, p)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// B claims the request (the lease is past) and then waits: A's minting transaction holds the
	// business's issuing lock (checkLimits). A is let go once B's lease is on the row.
	podBDone := make(chan error, 1)
	go func() { podBDone <- podB.RunFor(context.Background(), r.tenant) }()
	waitFor(t, func() bool {
		return r.count(`SELECT count(*) FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2 AND lease_until > clock_timestamp()`, r.tenant, p) == 1
	})
	letGo()
	if err := <-done; !settled(err) {
		t.Fatalf("pod A: %v", err)
	}
	if err := <-podBDone; !settled(err) {
		t.Fatalf("pod B: %v", err)
	}
	r.q.afterMint = nil
	if n, s := r.invites(p), len(r.sink.calls())-before; n != 1 || s != 1 || r.queued(p) != 0 {
		t.Fatalf("an outlived lease: %d invitations, %d e-mails, %d queued; want 1, 1, 0 (A's mint rolled back)", n, s, r.queued(p))
	}
}

// TestInviteQueue_LimitsDelayAndNeverRefuse (ADR 0022 §9, plan §2 item 11): with the business one
// invitation short of its hourly limit, the worker sends one and DEFERS the rest -- they stay in
// the queue, due later, the run answers *Later, and no invite.email_refused row is written. The
// panel's count includes the queue's mint (one set of counters).
func TestInviteQueue_LimitsDelayAndNeverRefuse(t *testing.T) {
	r := newQueueRig(t)
	f := mailFixture{d: r.d, tenantID: r.tenant}
	seed := r.hire(nil)
	f.seedInvites(t, seed, TenantHourLimit-1, "now()")
	people := []uuid.UUID{r.hire(addr("lim")), r.hire(addr("lim")), r.hire(addr("lim"))}
	err := r.q.RunFor(context.Background(), r.tenant)
	var later *Later
	if !errors.As(err, &later) || later.DueIn() <= 0 {
		t.Fatalf("a run past the limit: %v, want *Later", err)
	}
	minted, waiting := 0, 0
	for _, p := range people {
		minted += r.invites(p)
		waiting += r.queued(p)
	}
	if minted != 1 || waiting != 2 || len(r.sink.calls()) != 1 {
		t.Fatalf("%d minted, %d waiting, %d sent; want 1, 2, 1", minted, waiting, len(r.sink.calls()))
	}
	if n := r.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'invite.email_refused'`, r.tenant); n != 0 {
		t.Fatalf("%d invite.email_refused rows; a limit delays, it never refuses", n)
	}
	if n := r.count(`SELECT count(*) FROM invite_requests WHERE tenant_id = $1 AND lease_until > now() + interval '5 minutes'`, r.tenant); n != 2 {
		t.Fatalf("%d deferred requests due later, want 2", n)
	}
	// The panel's press for anybody now meets the same limit: the queue's mint is counted.
	if _, err := r.m.IssueAndDeliver(context.Background(), IssueParams{TenantID: r.tenant, EmployeeID: people[0]}, mailRoute(t, r.d, &fakeMailSink{})); !errors.Is(err, ErrTenantHourLimit) {
		t.Fatalf("the panel after the queue's mint: %v, want ErrTenantHourLimit (one set of counters)", err)
	}
}

// TestInviteQueue_OnlyAnInvitedPersonIsSent (K-15): a request for somebody who activated in the
// meantime, or was deactivated, mints nothing, sends nothing and leaves the queue.
func TestInviteQueue_OnlyAnInvitedPersonIsSent(t *testing.T) {
	r := newQueueRig(t)
	active, gone := r.hire(addr("act")), r.hire(addr("gone"))
	if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE employees SET status = 'active', activated_at = now() WHERE tenant_id = $1 AND id = $2`, r.tenant, active); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE tenant_id = $1 AND id = $2`, r.tenant, gone)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.q.RunFor(context.Background(), r.tenant); err != nil {
		t.Fatal(err)
	}
	for _, p := range []uuid.UUID{active, gone} {
		if r.invites(p) != 0 || r.queued(p) != 0 {
			t.Errorf("person %s: %d invitations, %d queued; want none", p, r.invites(p), r.queued(p))
		}
	}
	if len(r.sink.calls()) != 0 {
		t.Fatalf("%d e-mails sent", len(r.sink.calls()))
	}
}

// TestEmployeesPut_EmailChangeKillsOldInviteInSameTransaction (K-15; ADR 0028 §9: "E-posta değişince
// bekleyen davetler aynı işlemde ölür"; the race the brief names: the worker minting while the
// address changes). The worker is stopped INSIDE its minting transaction, after the insert and the
// recipient read (the old address); the API's address change starts and WAITS (pg_locks: its FOR
// UPDATE on the employee against the mint's FOR KEY SHARE); the worker commits and sends to the old
// address; the change then retires that very invitation in ITS transaction, writes the new address
// and releases the request (the worker cannot take it out of the queue). The worker's run goes on
// and mints for the NEW address: the old link is dead (ErrCodeCancelled), the new one live.
//
// MUTANTS (measured; ADR 0028 "API-6 uygulama notu"): the change's second retirement removed ->
// the old link survives (red); the re-queue as ON CONFLICT DO NOTHING -> the worker settles the
// request and the new address never gets one (red). The change's lock taken FOR NO KEY UPDATE, or
// the worker's FOR SHARE removed, is each GREEN on its own -- either lock alone makes the change
// wait for the mint -- and the two removed together are red (the change never waits).
func TestEmployeesPut_EmailChangeKillsOldInviteInSameTransaction(t *testing.T) {
	r := newQueueRig(t)
	ref := apiRefFor("RACE")
	oldAddr, newAddr := addr("old"), addr("new")
	id := r.put(ref, oldAddr).Employee.ID

	minting, release := make(chan struct{}), make(chan struct{})
	second, release2 := make(chan struct{}), make(chan struct{})
	var released, released2, unblocked sync.Once
	letGo := func() { released.Do(func() { close(release) }) }
	letGo2 := func() { released2.Do(func() { close(release2) }) }
	t.Cleanup(letGo)
	t.Cleanup(letGo2)
	mints := 0
	r.q.afterMint = func(context.Context, uuid.UUID) {
		mints++
		switch mints {
		case 1:
			close(minting)
			<-release
		case 2:
			// The worker's NEXT mint (for the new address) is open and not committed: its own
			// retirement of the old link is invisible to everybody else. What the old link is at
			// this moment is what the CHANGE's transaction made it.
			close(second)
			<-release2
		}
	}
	// The relay holds the worker's send until the change has committed, so the worker settles the
	// request AFTER the change re-queued it -- the order in which a re-queue that left the worker's
	// lease standing would lose the new address's invitation.
	sent := make(chan struct{})
	r.sink.block = sent
	unblock := func() { unblocked.Do(func() { close(sent) }) }
	t.Cleanup(unblock)
	worker := make(chan error, 1)
	go func() { worker <- r.q.RunFor(context.Background(), r.tenant) }()
	<-minting
	change := make(chan tenant.EmployeePutResult, 1)
	go func() { change <- r.put(ref, newAddr) }()
	// The change waits on the mint's lock before the worker is let go.
	waitFor(t, func() bool {
		return r.count(`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%name: LockEmployeeForAPI%'`) > 0
	})
	letGo()
	res := <-change
	unblock()
	if !res.Changed || !res.Queued {
		t.Fatalf("the change: %+v, want written and queued", res)
	}
	select {
	case <-second:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not mint again for the released request")
	}
	first := r.sink.calls()
	if len(first) != 1 || first[0].Recipient.Address != *oldAddr {
		t.Fatalf("before the worker's second mint: %d e-mails; want the one minted before the change, to the old address", len(first))
	}
	if _, err := r.m.Lookup(context.Background(), codeOf(t, first[0])); !errors.Is(err, ErrCodeCancelled) {
		t.Fatalf("the old address's link once the change committed: %v, want ErrCodeCancelled (retired in the change's own transaction)", err)
	}
	letGo2()
	if err := <-worker; err != nil {
		t.Fatalf("the worker: %v", err)
	}
	r.q.afterMint = nil
	r.sink.block = nil
	// The worker's run went on after the race: the request the change released was due again, and
	// the same run minted for the NEW address.
	calls := r.sink.calls()
	if len(calls) != 2 || calls[0].Recipient.Address != *oldAddr || calls[1].Recipient.Address != *newAddr {
		t.Fatalf("the race sent %d e-mails; want two -- the one minted before the change (old address), then one to the new address", len(calls))
	}
	if _, err := r.m.Lookup(context.Background(), codeOf(t, calls[0])); !errors.Is(err, ErrCodeCancelled) {
		t.Fatalf("the old address's link after the change: %v, want ErrCodeCancelled (retired in the change's transaction)", err)
	}
	if _, err := r.m.Lookup(context.Background(), codeOf(t, calls[1])); err != nil {
		t.Fatalf("the new address's link: %v, want usable", err)
	}
	if r.queued(id) != 0 || r.invites(id) != 2 {
		t.Fatalf("after the race: %d queued, %d invitations; want 0 and 2", r.queued(id), r.invites(id))
	}
}

// TestInviteQueue_TheLinkActivatesThroughTheWizardsPath (ADR 0026, K-14): the link the queue sends
// to an employee the API opened -- no home venue -- is an ordinary invitation: Lookup greets a
// FIRST activation with the person's name and no venue, the wizard's consent binds a browser, and
// the first NFC tap's Activate spends it and turns the person 'active'. A resend for them is then
// ErrAlreadyActivated.
func TestInviteQueue_TheLinkActivatesThroughTheWizardsPath(t *testing.T) {
	r := newQueueRig(t)
	ref := apiRefFor("ACT")
	id := r.put(ref, addr("act")).Employee.ID
	if err := r.q.RunFor(context.Background(), r.tenant); err != nil {
		t.Fatal(err)
	}
	calls := r.sink.calls()
	if len(calls) != 1 {
		t.Fatalf("%d e-mails, want 1", len(calls))
	}
	code := codeOf(t, calls[0])
	ctx, err := r.m.Lookup(context.Background(), code)
	if err != nil || !ctx.FirstActivation() || ctx.EmployeeID != id || ctx.FullName != "Maria Borg" || ctx.LocationID != uuid.Nil {
		t.Fatalf("Lookup: %+v %v, want a first activation of the person, no venue", ctx, err)
	}
	b := consent(t, r.m, code)
	act, err := r.m.Activate(context.Background(), code, b)
	if err != nil || act.Status != statusActive || act.SecondDeviceReplaced {
		t.Fatalf("Activate: %+v %v", act, err)
	}
	err = r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := r.staff.ResendActivationTx(ctx, tx, tenant.EmployeeResend{TenantID: r.tenant, ActorID: r.key, Via: "api", ExternalRef: ref})
		return err
	})
	if !errors.Is(err, tenant.ErrAlreadyActivated) {
		t.Fatalf("resend after the activation: %v, want ErrAlreadyActivated", err)
	}
}

// failSends makes the fake relay answer every send with err.
func (r *queueRig) failSends(err error) {
	r.sink.mu.Lock()
	r.sink.err = err
	r.sink.mu.Unlock()
}

// due makes the employee's waiting request due now (a deferral or a back-off has passed) and moves
// the person's earlier invitations two hours back, as the real wait would -- so ADR 0022 §9's
// per-person hourly limit (three mints) does not delay the next try in a test that compresses the
// waits. created_at is not the application's to write; the owner's connection does it.
func (r *queueRig) due(employee uuid.UUID) {
	r.t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		r.t.Skip("DATABASE_MIGRATE_URL is required to age the invitations")
	}
	owner, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = owner.Close(context.Background()) }()
	if _, err := owner.Exec(context.Background(), `UPDATE employee_invites SET created_at = created_at - interval '2 hours' WHERE tenant_id = $1 AND employee_id = $2`, r.tenant, employee); err != nil {
		r.t.Fatal(err)
	}
	if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE invite_requests SET lease_until = clock_timestamp() - interval '1 second' WHERE tenant_id = $1 AND employee_id = $2`, r.tenant, employee)
		return e
	}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *queueRig) attempts(employee uuid.UUID) int {
	return r.count(`SELECT coalesce(max(attempts), -1) FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, r.tenant, employee)
}

func (r *queueRig) apiState(employee uuid.UUID) tenant.InvitationState {
	r.t.Helper()
	e, err := r.staff.APIEmployeeByID(context.Background(), r.tenant, employee)
	if err != nil {
		r.t.Fatal(err)
	}
	return e.Invitation
}

// TestInviteQueue_ACeilingDelaysAndSpendsNoTry (ADR 0022 §9, plan §2 item 11: "tavan isteği
// reddettirmez, gönderimi geciktirir"; API-6 review O-2): six refusals by a ceiling -- the
// per-mailbox cap asked in the minting transaction, the cap and the process breaker answering the
// send, a throttling 451 -- leave the request waiting with ZERO tries spent; one real failure (a
// 550) then counts ONE. A ceiling never gives up on anybody.
//
// MUTANTS (measured): a ceiling's refusal counted as a failed send -> tries spent, red; the claim
// counting a try again -> red.
func TestInviteQueue_ACeilingDelaysAndSpendsNoTry(t *testing.T) {
	r := newQueueRig(t)
	p := r.hire(addr("ceil"))
	r.sink.mu.Lock()
	r.sink.capped = true
	r.sink.mu.Unlock()
	if err := r.q.RunFor(context.Background(), r.tenant); !settled(err) {
		t.Fatal(err)
	}
	r.sink.mu.Lock()
	r.sink.capped = false
	r.sink.mu.Unlock()
	if r.attempts(p) != 0 || len(r.sink.calls()) != 0 {
		t.Fatalf("the cap asked before the mint: %d tries, %d sends; want 0, 0", r.attempts(p), len(r.sink.calls()))
	}
	for _, class := range []mail.Class{mail.ClassRecipientCap, mail.ClassBreaker, mail.ClassThrottled, mail.ClassBreaker, mail.ClassRecipientCap} {
		r.due(p)
		r.failSends(&mail.SendError{Class: class, SMTPCode: 451})
		if err := r.q.RunFor(context.Background(), r.tenant); !settled(err) {
			t.Fatalf("%s: %v", class, err)
		}
		if r.attempts(p) != 0 {
			t.Fatalf("a %s refusal spent a try (%d); a ceiling delays and never counts", class, r.attempts(p))
		}
		if r.apiState(p) != tenant.InvitationQueued {
			t.Fatalf("after a %s refusal the API says %q, want queued", class, r.apiState(p))
		}
	}
	r.due(p)
	r.failSends(&mail.SendError{Class: mail.ClassRejected, SMTPCode: 550})
	_ = r.q.RunFor(context.Background(), r.tenant)
	if r.attempts(p) != 1 {
		t.Fatalf("one real failure after six ceilings: %d tries, want 1", r.attempts(p))
	}
}

// TestInviteQueue_GivingUpRetiresTheUnsentLinkAndSaysUndelivered (ADR 0028 §9: "son 1 saatte
// GÖNDERİLMİŞ canlı davet"; API-6 review O-1): five real failures make the queue give up -- the
// request leaves the queue, every link it minted is dead (the last one RETIRED, not left live and
// unsent), and the API answers activationEmail "undelivered"; while it still retried it answered
// "queued", never "sent". A resend then queues a new request (it is not silenced by an unsent link),
// and a successful send turns the answer "sent". The panel's own press whose e-mail failed leaves a
// live but undelivered link: the API says "undelivered", not "sent", and a resend is not a no-op.
//
// MUTANTS (measured): the give-up's retirement removed -> a live unsent link survives, red;
// "sent" read off a live invitation without its delivery row -> the panel's failed press reads
// "sent" and the resend is a no-op, red.
func TestInviteQueue_GivingUpRetiresTheUnsentLinkAndSaysUndelivered(t *testing.T) {
	r := newQueueRig(t)
	ref := apiRefFor("GIVE")
	p := r.put(ref, addr("give")).Employee.ID
	r.failSends(&mail.SendError{Class: mail.ClassRejected, SMTPCode: 550})
	for i := 1; i <= queueMaxAttempts; i++ {
		r.due(p)
		_ = r.q.RunFor(context.Background(), r.tenant)
		if i < queueMaxAttempts && (r.attempts(p) != i || r.apiState(p) != tenant.InvitationQueued) {
			t.Fatalf("after failure %d: %d tries, the API says %q; want %d and queued", i, r.attempts(p), r.apiState(p), i)
		}
	}
	if r.queued(p) != 0 {
		t.Fatal("the queue did not give up after its last try")
	}
	for _, d := range r.sink.calls() {
		if _, err := r.m.Lookup(context.Background(), codeOf(t, d)); err == nil {
			t.Fatal("a link the queue minted and never delivered is still spendable after it gave up")
		}
	}
	if got := r.apiState(p); got != tenant.InvitationUndelivered {
		t.Fatalf("after giving up the API says %q, want undelivered", got)
	}
	resend := func() tenant.EmployeePutResult {
		t.Helper()
		var out tenant.EmployeePutResult
		if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			out, err = r.staff.ResendActivationTx(ctx, tx, tenant.EmployeeResend{TenantID: r.tenant, ActorID: r.key, Via: "api", ExternalRef: ref})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if res := resend(); !res.Queued || res.Employee.Invitation != tenant.InvitationQueued {
		t.Fatalf("a resend after the queue gave up: %+v, want a new request", res.Employee.Invitation)
	}
	r.failSends(nil)
	if err := r.q.RunFor(context.Background(), r.tenant); !settled(err) {
		t.Fatal(err)
	}
	if got := r.apiState(p); got != tenant.InvitationSent {
		t.Fatalf("after a delivered send the API says %q, want sent", got)
	}

	// The panel's press whose e-mail failed: a live, never-delivered link.
	ref2 := apiRefFor("PANEL")
	q := r.put(ref2, addr("panel")).Employee.ID
	if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, r.tenant, q)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	failing := &fakeMailSink{err: &mail.SendError{Class: mail.ClassRejected, SMTPCode: 550}}
	if _, err := r.m.IssueAndDeliver(context.Background(), IssueParams{TenantID: r.tenant, EmployeeID: q}, mailRoute(t, r.d, failing)); err == nil {
		t.Fatal("the failing press reported success")
	}
	if r.count(`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2 AND used_at IS NULL AND cancelled_at IS NULL AND now() < expires_at`, r.tenant, q) != 1 {
		t.Fatal("setup: the failed press should leave one live invitation")
	}
	if got := r.apiState(q); got != tenant.InvitationUndelivered {
		t.Fatalf("a live but undelivered link: the API says %q, want undelivered", got)
	}
	var res tenant.EmployeePutResult
	if err := r.d.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		res, err = r.staff.ResendActivationTx(ctx, tx, tenant.EmployeeResend{TenantID: r.tenant, ActorID: r.key, Via: "api", ExternalRef: ref2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !res.Queued {
		t.Fatal("a resend beside a live but undelivered link was a no-op; only a SENT link silences it")
	}
}
