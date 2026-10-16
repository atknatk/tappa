// Package delivery is the webhook SENDER (M11 API-10; ADR 0028 §6, §7, §2, §10): a Job of K-19's
// worker (internal/webhook) that takes ONE business's due webhook_deliveries rows -- written by the
// punch feed's triggers, one per target event (00049) -- and posts each, signed, to the business's
// endpoint through internal/egress.
//
// It is a package of its own, beside internal/webhook rather than in it, for internal/webhook/endpoints'
// reason: internal/webhook is the worker and reads nothing itself (TestWorker_NoTenantWideRead pins it
// to no store, no pgx); this package reads and writes one business's rows, always in WithTenant with an
// explicit tenant_id filter (CLAUDE.md §4.5; every query it names is in db/queries/webhooks.sql or
// apikeys.sql, whose belts are TestWebhookQueries_CarryAnExplicitTenantPredicate and
// TestAPIKeyQueries_CarryAnExplicitTenantPredicate -- TestDeliveryJob_NamesOnlyBeltedQueries).
//
// THE LEASE MODEL (ADR 0028 §6, normative):
//
//  1. a SHORT transaction claims the business's one oldest-due delivery (FOR UPDATE SKIP LOCKED,
//     lease_until = now + Lease), re-reads K-22's gate, the endpoint (switch, address, sealed key)
//     and the event, and commits -- or rolls the claim back when the delivery may not go now;
//  2. the request goes OUT, with NO transaction open (TestWebhookWorker_NoOpenTransactionDuringHTTP);
//  3. a second short transaction records the result -- under the same lease value, so a delivery
//     whose lease ran out and was claimed again (another pod, a resend) is not overwritten.
//
// ONE DELIVERY PER CLAIM, AT MOST runMax PER RUN. The lease must cover what is sent under it: one
// delivery is one exchange (egress.TotalTimeout, 10 s) and its result row, so Lease is 60 s whatever
// the backlog. A batch of N claimed together would need N × 10 s of lease (sent one after another) or
// N connections at once (sent in parallel) -- neither buys anything at punch volumes, and a pod that
// dies holds one delivery, not N.
//
// ONE RUN IS ONE TURN OF A SHARED LOOP, AND IT IS BOUNDED IN TIME (M11 API-10 review, ORTA-2). K-19's
// worker is one goroutine: while a run sends, every other business's deliveries -- and API-6's
// invitation queue -- wait. runMax alone bounded the COUNT, not the time: 50 deliveries to a receiver
// that never answers held the loop 50 × 10 s (measured: a fast business's one delivery waited 10.6 s
// behind it at a 200 ms client timeout, ~8 min 50 s at the production 10 s). So a run starts no new
// claim once RunBudget has passed since it began, and answers "come back at once" (the worker puts the
// business back BEHIND every business noticed meanwhile -- first come, first served). A turn therefore
// holds the loop for at most RunBudget plus what the last claim begun inside it costs: that claim's
// transaction, one exchange (egress.TotalTimeout) and its result write (recordGrace) -- 10 + 10 + 5 s,
// i.e. ~25 s plus the claim transaction's database time (which has no clock of its own but the
// context's). When the receiver does not answer at all, a single timeout already spends the budget:
// one claim, one exchange and its write, ~15 s plus the database. (The review round wrote "< 20 s"; it
// left out the result write and the claim's transaction -- DÜŞÜK round.) The worst wait of a business
// noticed now is that bound times the businesses ahead of it in the set (and their other jobs' turns). Sending in parallel was not taken: it would need one lease per connection in flight
// and a second bound (connections) for the same guarantee, which one goroutine and a budget give
// without either.
//
// THE GATES (ADR 0028 §6: "gönderim anında ... K-22 ve uç noktanın active'i yeniden denetlenir"). The
// claim's transaction reads the business's API gate and the endpoint's switch; when either is closed
// -- or the stored address no longer passes the URL rule, or the sealed key does not open -- the claim
// is ROLLED BACK: the delivery stays pending, its try not spent, and nothing is sent (K-26: switching
// the endpoint off really stops the queue). IT WAITS, IT DOES NOT DIE -- the decision, and why: every
// one of these is the owner's or the operator's to undo (switch on, reopen, re-address, rotate), and a
// punch recorded while the endpoint was on was owed to it; dropping the queue on a switch-off would make
// "off for a minute" lose deliveries silently (CLAUDE.md §4.6's spirit), while a delivery left waiting
// goes once the gate opens: at once when the owner switches the endpoint on, re-addresses it or
// rotates its key (the panel's act notices the business -- internal/webhook/endpoints), otherwise at
// the next recheck. The run then says "come back in GateRecheck", a timer for this one business (K-19)
// -- ONE per business however often it is run meanwhile (the worker keeps the earliest; before the
// review a held business grew a timer chain per tap and per /v1 request). (Events recorded WHILE the
// endpoint is off get no delivery at all -- 00049's trigger writes one only for an active endpoint --
// so the integrator's gap is the feed's to fill.) The
// re-read is in the transaction that commits immediately before the request: a switch-off committed
// after it does not stop the one request already leaving (named, as the invitation queue names it).
//
// THE SCHEDULE (ADR 0028 §6: "1 dk · 5 dk · 30 dk · 2 sa · 6 sa → dead"), read literally: the first try
// is at once; after a failed try the next waits 1 min, 5 min, 30 min, 2 h, 6 h; the try after the
// 6-hour wait is the SIXTH, and a sixth failure is `dead` (MaxAttempts; TestBackoff_TheScheduleIsTheADRs).
// EVERY failure class is retried on that schedule -- timeout, tls, refused and http_5xx (the receiver
// or the network is down), http_4xx (a 401 of a receiver that has not switched to a rotated key, a 404
// of a path being fixed: the owner's to repair within the schedule, and the schedule is the bound) and
// blocked_address (the dial is refused before a packet leaves, so retrying costs the remote nothing; a
// DNS answer can change). No class is permanent from Taptime's side; six tries over ~8.6 hours is the
// whole cost. A resend makes a delivery due at once and keeps its try count: a dead delivery revived
// gets ONE more try (internal/webhook/endpoints' Resend).
//
// SHUTDOWN (ADR 0028 §6, "kapanış bütçesi"): the worker's context is cancelled at Drain; egress's Post
// returns at once and THE RESULT IS NOT RECORDED -- whether the remote received it is unknown, and a
// "timeout" would spend a try for Taptime's own restart. The lease runs out and the delivery is tried
// again (the receiver deduplicates by id + version -- plan §2 item 16) once the business is noticed
// again (K-19; plan §2 item 20).
//
// 🔴 NOTHING OF THE ADDRESS OR THE KEY IS LOGGED OR RETURNED (CLAUDE.md §7; ADR 0028 §3, §6): egress
// hands back a closed class and a status, never Go's error text; this package's log lines carry the
// tenant, the endpoint's and the delivery's ids, the attempt, the class and the status. The key is
// opened in memory for the one signature and is a webhook.SigningKey (a placeholder everywhere it is
// printed); the address is an egress.Endpoint (the same).
package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/punchwire"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/internal/webhook"
)

// Lease is how long a claim holds a delivery: one exchange (egress.TotalTimeout) and its result row,
// with room to spare.
const Lease = 60 * time.Second

// MaxAttempts is the try after which a failed delivery is dead: the first, then one after each of
// Backoff's five waits.
const MaxAttempts = 6

// Backoff is the wait after the n-th failed try (Backoff[n-1]), ADR 0028 §6's schedule.
var Backoff = [MaxAttempts - 1]time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// The run's two clocks (the defaults New sets; Pace changes them).
const (
	// RunBudget is how long one run may keep starting claims (see the header): one exchange's own
	// ceiling, so a turn holds the loop for about two exchanges' time and one result write (~25 s plus
	// the database -- the header).
	RunBudget = egress.TotalTimeout
	// GateRecheck is when a business whose deliveries wait at a closed gate is looked at again.
	GateRecheck = 15 * time.Minute
)

const (
	// runMax bounds one RunFor's count (see the header).
	runMax = 50
	// recordGrace bounds the result's write after the request returned (it is written even when the
	// worker is being drained at that moment: the result is known).
	recordGrace = 5 * time.Second
)

// IDPrefix is a delivery's external id prefix (ADR 0028 §8, K-8).
const IDPrefix = "dlv_"

// EventPunch is the body's event name for a punch (the ping's is "ping" -- internal/webhook/endpoints).
const EventPunch = "punch"

// The states (00048's webhook_deliveries_state CHECK).
const (
	StatePending   = "pending"
	StateDelivered = "delivered"
	StateDead      = "dead"
)

// Body is what a delivery posts: the event name, the delivery's id, and the punch exactly as
// GET /v1/punches answers it (punchwire.Punch's fields, flattened). It is the same bytes on every try
// of one delivery (nothing in it is the moment of sending), so a receiver's deduplication by id +
// version (plan §2 item 16) sees one punch.
type Body struct {
	Event      string `json:"event"`
	DeliveryID string `json:"deliveryId"`
	punchwire.Punch
}

// Database is WithTenant, declared at the consumer (CLAUDE.md §7).
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Sender is internal/egress's Post.
type Sender interface {
	Post(ctx context.Context, to egress.Endpoint, body []byte, header http.Header) egress.Result
}

// Job is the delivery job. Dependencies are struct fields (§7).
type Job struct {
	kek  []byte
	data Database
	send Sender
	log  *slog.Logger
	// budget and recheck are RunBudget and GateRecheck unless Pace changed them.
	budget, recheck time.Duration
}

// New builds the job. The KEK is TAPPA_WEBHOOK_KEK (the endpoint's sealed key opens under it); the
// sender is the panel's ping client (one egress.Client per process).
func New(kek []byte, data Database, send Sender, log *slog.Logger) (*Job, error) {
	if len(kek) != 32 {
		return nil, fmt.Errorf("webhook delivery: the KEK must be 32 bytes, got %d", len(kek))
	}
	if data == nil || send == nil {
		return nil, errors.New("webhook delivery: nil database or sender")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Job{kek: append([]byte(nil), kek...), data: data, send: send, log: log, budget: RunBudget, recheck: GateRecheck}, nil
}

// Pace sets the run budget and the closed gate's recheck. cmd/tappa never calls it (the defaults are
// the design); the tests of the worker's fairness shorten both, the way they shorten egress's timeout.
// No non-test file of the repository may reach it (TestPace_IsReachedOnlyFromTests). It is called
// before the job is handed to a worker: it is not safe beside a run.
func (j *Job) Pace(budget, recheck time.Duration) error {
	if budget <= 0 || recheck <= 0 {
		return errors.New("webhook delivery: a pace must be positive")
	}
	j.budget, j.recheck = budget, recheck
	return nil
}

// Later is RunFor's "this business has deliveries due later" -- internal/webhook.DueLater.
type Later struct{ After time.Duration }

func (l *Later) Error() string { return "webhook delivery: deliveries are due later" }

// DueIn is how long until the business should be run again.
func (l *Later) DueIn() time.Duration { return l.After }

var _ webhook.DueLater = (*Later)(nil)

// RunFor sends the business's due deliveries, one claim at a time, at most runMax and no new claim
// after the run budget. It returns nil when nothing is pending, a *Later when deliveries wait (a
// back-off, another worker's lease, a closed gate, or runMax or the budget reached), the context's
// error when the worker is draining, and an error -- that names no address and no key -- when the
// database failed.
func (j *Job) RunFor(ctx context.Context, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return errors.New("webhook delivery: no tenant")
	}
	began := time.Now()
	for range runMax {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Since(began) >= j.budget {
			return &Later{After: 0}
		}
		w, outcome, err := j.claim(ctx, tenantID)
		if err != nil {
			return err
		}
		switch outcome {
		case nothingDue:
			return j.whenNext(ctx, tenantID)
		case held:
			j.log.InfoContext(ctx, "webhook: the business's deliveries wait; nothing is sent",
				"tenant_id", tenantID.String(), "endpoint_id", w.endpoint.String(), "reason", w.heldBy)
			return &Later{After: j.recheck}
		}
		if err := j.deliver(ctx, tenantID, w); err != nil {
			return err
		}
	}
	return &Later{After: 0}
}

type outcome int

const (
	nothingDue outcome = iota
	held
	claimed
)

// work is one claimed delivery and what sending it needs.
type work struct {
	id, endpoint uuid.UUID
	attempt      int32
	lease        time.Time
	to           egress.Endpoint
	key          webhook.SigningKey
	event        tenant.PunchEvent
	// heldBy names why a claim was given back (a fixed word, never a value).
	heldBy string
}

// errHeld rolls a claim back: the delivery may not go now.
var errHeld = errors.New("webhook delivery: held")

// claim is step 1: one delivery, the gates, the endpoint and the event, in one short transaction.
func (j *Job) claim(ctx context.Context, tenantID uuid.UUID) (work, outcome, error) {
	var (
		w   work
		out = nothingDue
	)
	hold := func(reason string) error {
		w.heldBy, out = reason, held
		return errHeld
	}
	err := j.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		c, err := q.ClaimWebhookDelivery(ctx, store.ClaimWebhookDeliveryParams{
			LeaseSeconds: int32(Lease / time.Second), TenantID: tenantID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if c.LeaseUntil == nil {
			return errors.New("claim: no lease returned")
		}
		w.id, w.endpoint, w.attempt, w.lease = c.ID, c.EndpointID, c.Attempt, *c.LeaseUntil
		// K-22 (ADR 0028 §2: "kapı her yolda", the worker's send included).
		open, err := q.GetTenantAPIEnabledAt(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("read the API gate: %w", err)
		}
		if open == nil {
			return hold("api_closed")
		}
		ep, err := q.GetWebhookEndpointForSigning(ctx, tenantID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && ep.ID != c.EndpointID) {
			// The business has one endpoint and a delivery's FK names it (00048): unreachable.
			return errors.New("the delivery's endpoint is not the business's endpoint")
		}
		if err != nil {
			return fmt.Errorf("read the endpoint: %w", err)
		}
		if !ep.Active {
			return hold("endpoint_off")
		}
		if w.to, err = egress.EndpointFromStore(ep.Url); err != nil {
			return hold("stored_address")
		}
		plain, err := sun.Open(j.kek, ep.ID[:], ep.SigningKeySealed)
		if err != nil {
			return hold("key_unreadable")
		}
		if w.key, err = webhook.ParseSigningKey(string(plain)); err != nil {
			return hold("key_unreadable")
		}
		ev, err := q.GetPunchEventForDelivery(ctx, store.GetPunchEventForDeliveryParams{TenantID: tenantID, Seq: c.PunchEventSeq})
		if err != nil {
			return fmt.Errorf("read the event: %w", err)
		}
		w.event = eventOf(ev)
		out = claimed
		return nil
	})
	if errors.Is(err, errHeld) {
		return w, held, nil
	}
	if err != nil {
		return work{}, nothingDue, fmt.Errorf("webhook delivery: %w", err)
	}
	return w, out, nil
}

func eventOf(r store.GetPunchEventForDeliveryRow) tenant.PunchEvent {
	return tenant.PunchEvent{
		Seq: r.Seq, PunchID: r.PunchID, Version: r.Version, Status: r.Status, PunchTime: r.PunchTime, RecordedAt: r.RecordedAt,
		Channel: r.Channel, Direction: r.Direction,
		EmployeeID: r.EmployeeID, EmployeeRef: r.EmployeeRef, EmployeeCode: r.EmployeeCode, EmployeeCompany: r.EmployeeCompany,
		LocationID: r.LocationID, LocationRef: r.LocationRef, LocationName: r.LocationName, LocationCompany: r.LocationCompany,
		TagUID: r.TagUid,
	}
}

// Encode is the body of a delivery: the bytes that are signed and sent.
func Encode(deliveryID uuid.UUID, e tenant.PunchEvent) ([]byte, error) {
	return json.Marshal(Body{Event: EventPunch, DeliveryID: IDPrefix + deliveryID.String(), Punch: punchwire.Of(e)})
}

// Header is a delivery's request header -- the ping's set (internal/webhook/endpoints): the content
// type, the user agent and the signature of exactly these body bytes.
func Header(key webhook.SigningKey, body []byte) http.Header {
	return http.Header{
		"Content-Type":          {"application/json"},
		"User-Agent":            {"Taptime-Webhooks/1"},
		webhook.SignatureHeader: {key.Sign(body)},
	}
}

// deliver is steps 2 and 3.
func (j *Job) deliver(ctx context.Context, tenantID uuid.UUID, w work) error {
	body, err := Encode(w.id, w.event)
	if err != nil {
		return fmt.Errorf("webhook delivery: encode %s: %w", w.id, err)
	}
	res := j.send.Post(ctx, w.to, body, Header(w.key, body))
	if err := ctx.Err(); err != nil {
		// Drained mid-request: nothing is recorded, the lease runs out (the header's SHUTDOWN).
		j.log.InfoContext(context.WithoutCancel(ctx), "webhook: a delivery in flight was abandoned at shutdown; it is tried again after its lease",
			"tenant_id", tenantID.String(), "delivery_id", w.id.String())
		return err
	}
	r := Decide(w.attempt, res)
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordGrace)
	defer cancel()
	var row store.RecordWebhookDeliveryResultRow
	lost := false
	err = j.data.WithTenant(rctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		lease := w.lease
		var e error
		row, e = store.New(tx).RecordWebhookDeliveryResult(ctx, store.RecordWebhookDeliveryResultParams{
			State: r.State, DelaySeconds: int32(r.Delay / time.Second), LastStatus: r.Status, LastErrorClass: r.Class,
			TenantID: tenantID, ID: w.id, LeaseUntil: &lease,
		})
		if errors.Is(e, pgx.ErrNoRows) {
			lost = true
			return nil
		}
		return e
	})
	if err != nil {
		return fmt.Errorf("webhook delivery: record the result of %s: %w", w.id, err)
	}
	args := []any{"tenant_id", tenantID.String(), "endpoint_id", w.endpoint.String(), "delivery_id", w.id.String(),
		"class", string(res.Class), "status", res.Status}
	switch {
	case lost:
		j.log.WarnContext(ctx, "webhook: a delivery's lease ran out before its result was recorded; the result is dropped", args...)
	case row.State == StateDelivered:
		j.log.InfoContext(ctx, "webhook: delivered", append(args, "attempt", row.Attempt)...)
	default:
		j.log.WarnContext(ctx, "webhook: a delivery failed", append(args, "attempt", row.Attempt, "state", row.State)...)
	}
	return nil
}

// Result is what a try's outcome writes: the state, the wait before the next try (pending only), and
// the closed class and status (nil when there is none).
type Result struct {
	State  string
	Delay  time.Duration
	Status *int32
	Class  *string
}

// Decide maps a try's egress.Result to its row, given the tries spent BEFORE it (the claimed attempt).
func Decide(attemptBefore int32, res egress.Result) Result {
	var r Result
	if res.Status > 0 {
		s := int32(res.Status)
		r.Status = &s
	}
	if res.Delivered() {
		r.State = StateDelivered
		return r
	}
	class := string(res.Class)
	if class == "" {
		// A non-2xx answer without a class cannot come from egress (statusResult classes every
		// answer); recorded in the closed set rather than as nothing.
		class = string(egress.ClassRefused)
	}
	r.Class = &class
	tries := attemptBefore + 1
	if tries >= MaxAttempts {
		r.State = StateDead
		return r
	}
	r.State, r.Delay = StatePending, Backoff[max(tries, 1)-1]
	return r
}

// whenNext reports when the business's next pending delivery is due, as a *Later; nil when nothing is
// pending.
func (j *Job) whenNext(ctx context.Context, tenantID uuid.UUID) error {
	var next store.NextWebhookDeliveryDueRow
	err := j.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		next, e = store.New(tx).NextWebhookDeliveryDue(ctx, tenantID)
		return e
	})
	if err != nil {
		return fmt.Errorf("webhook delivery: next due: %w", err)
	}
	if next.Pending == 0 {
		return nil
	}
	return &Later{After: max(time.Duration(next.DueInSeconds)*time.Second, time.Second)}
}
