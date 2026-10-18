package endpoints

// deliveries.go -- the owner's DELIVERY LOG and "RESEND" (M11 API-10, ADR 0028 §6, §2, §10). The
// deliveries are written by the punch feed's triggers and sent by internal/webhook/delivery's job;
// this file is the panel's side of them: read the business's deliveries a page at a time, and ask for
// one pending or dead delivery to be sent again now.
//
// THE LOG names a delivery by its id, the punch it carries (the business's own feed row), when it was
// queued, its state, its tries, and the last try's closed class and status -- never the address, never
// the remote's text (ADR 0028 §6: neither is stored).
//
// THE RETENTION (T130): the log and the resend see a delivered or dead delivery for
// delivery.RetentionDays after it settled -- the window the sweep deletes behind (internal/webhook/
// delivery), applied in the reads too, so the owner never sees a row the next sweep would take. A
// pending delivery is always seen.
//
// RESEND is owner-only, needs the API open (K-22) and the endpoint on (ADR 0028 §6: "yeniden
// gönder"de K-22 ve active yeniden denetlenir), and spends ONE slot of the bucket it shares with the
// ping (ten an hour per business): the slot row (ActionResend, target = the delivery) is written under
// the bucket's lock in the same transaction that makes the delivery due, so a refused resend spends
// nothing and a resend spends exactly one. After the commit the business is put into K-19's set (the
// worker then sends it -- no scan). Closed while the business is suspended (ADR 0028 §10: "panelden
// yeniden gönder" is in the 403 column -- ActResend in openWhileSuspended's closed default).

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/webhook/delivery"
)

// The resend's refusals (fixed sentinels; none carries a value).
var (
	ErrNoDelivery = errors.New("webhook delivery: no such delivery of this business")
	ErrDelivered  = errors.New("webhook delivery: the delivery already arrived")
	ErrInFlight   = errors.New("webhook delivery: the delivery is being sent right now")
)

// DeliveryPageSize is how many deliveries one page of the log shows.
const DeliveryPageSize = 25

// WorkNoticer is K-19's set (internal/webhook.Worker), declared at the consumer (§7). Notice must not
// block.
type WorkNoticer interface {
	Notice(tenant uuid.UUID)
}

// NoticeWorkTo makes a committed resend -- and a committed switch-on, re-address or rotation
// (endpoints.go: reopened) -- put its business into n (cmd/tappa wires the worker once).
func (s *Service) NoticeWorkTo(n WorkNoticer) { s.work = n }

// DeliveryPosition is where a page of the log starts: strictly before (At, ID) in newest-first order.
// The zero value is the newest end.
type DeliveryPosition struct {
	At time.Time
	ID uuid.UUID
}

// IsZero reports the newest end.
func (p DeliveryPosition) IsZero() bool { return p.At.IsZero() && p.ID == uuid.Nil }

// Delivery is one row of the log.
type Delivery struct {
	ID        uuid.UUID
	PunchID   uuid.UUID
	CreatedAt time.Time
	State     string
	Attempts  int
	// InFlight: a worker holds a live lease on it (being sent now).
	InFlight bool
	// NextAt is when a pending delivery's next try is due.
	NextAt      time.Time
	DeliveredAt *time.Time
	// Class and Status are the last try's (empty / 0 when there was none, or none applies).
	Class  string
	Status int
}

// Resendable reports whether "resend" may be offered for it.
func (d Delivery) Resendable() bool {
	return (d.State == "pending" || d.State == "dead") && !d.InFlight
}

// DeliveryPage is one page of the log.
type DeliveryPage struct {
	Deliveries []Delivery
	// Next is where the following (older) page starts; zero when this page is the oldest.
	Next DeliveryPosition
}

// farFuture is the newest end of the log as a bound: no row is created after it.
var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// Deliveries reads one page of the business's delivery log, newest first, from pos.
func (s *Service) Deliveries(ctx context.Context, tenantID, actorID uuid.UUID, pos DeliveryPosition) (DeliveryPage, error) {
	before, beforeID := farFuture, uuid.Max
	if !pos.IsZero() {
		before, beforeID = pos.At, pos.ID
	}
	var out DeliveryPage
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		rows, err := q.ListWebhookDeliveries(ctx, store.ListWebhookDeliveriesParams{
			TenantID: tenantID, BeforeAt: before, BeforeID: beforeID, PageSize: DeliveryPageSize + 1,
			RetentionDays: delivery.RetentionDays,
		})
		if err != nil {
			return fmt.Errorf("read the deliveries: %w", err)
		}
		n := min(len(rows), DeliveryPageSize)
		out.Deliveries = make([]Delivery, 0, n)
		for _, r := range rows[:n] {
			d := Delivery{
				ID: r.ID, PunchID: r.PunchID, CreatedAt: r.CreatedAt, State: r.State, Attempts: int(r.Attempt),
				InFlight: r.InFlight, NextAt: r.NextAttemptAt, DeliveredAt: r.DeliveredAt,
			}
			if r.LastErrorClass != nil {
				d.Class = *r.LastErrorClass
			}
			if r.LastStatus != nil {
				d.Status = int(*r.LastStatus)
			}
			out.Deliveries = append(out.Deliveries, d)
		}
		if len(rows) > DeliveryPageSize {
			last := out.Deliveries[n-1]
			out.Next = DeliveryPosition{At: last.CreatedAt, ID: last.ID}
		}
		return nil
	})
	if err != nil {
		return DeliveryPage{}, pass(err, "list deliveries")
	}
	return out, nil
}

type resendDetail struct {
	EndpointID string `json:"endpoint_id"`
	Via        string `json:"via"`
}

// Resend makes one pending or dead delivery due now, spending a slot of the ping/resend bucket.
func (s *Service) Resend(ctx context.Context, tenantID, actorID, deliveryID uuid.UUID) error {
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		if err := requireOpen(ctx, q, tenantID); err != nil {
			return err
		}
		ep, err := endpointID(ctx, q, tenantID)
		if err != nil {
			return err
		}
		if !ep.Active {
			return ErrDisabled
		}
		// The bucket's lock first (Ping's order), then the delivery's row.
		lock, err := q.LockWebhookBucket(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("lock the bucket: %w", err)
		}
		if lock.ID != tenantID {
			return errors.New("webhook endpoint: the bucket's lock named another business")
		}
		d, err := q.LockWebhookDeliveryForResend(ctx, store.LockWebhookDeliveryForResendParams{
			TenantID: tenantID, ID: deliveryID, RetentionDays: delivery.RetentionDays,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoDelivery
		}
		if err != nil {
			return fmt.Errorf("read the delivery: %w", err)
		}
		switch {
		case d.State == "delivered":
			return ErrDelivered
		case d.InFlight:
			return ErrInFlight
		}
		b, err := q.ReadWebhookBucket(ctx, bucketRead(tenantID))
		if err != nil {
			return fmt.Errorf("read the bucket: %w", err)
		}
		if int(b.Spent) >= BucketLimit {
			return &BucketFull{NextAt: b.Oldest.Add(BucketWindow)}
		}
		n, err := q.ResendWebhookDelivery(ctx, store.ResendWebhookDeliveryParams{TenantID: tenantID, ID: deliveryID})
		if err != nil {
			return fmt.Errorf("resend: %w", err)
		}
		if n != 1 {
			// The row is locked and was pending or dead and not leased a statement ago.
			return errors.New("webhook endpoint: the delivery could not be made due")
		}
		_, err = s.record(ctx, tx, tenantID, actorID, ActionResend, deliveryID, resendDetail{EndpointID: ep.ID.String(), Via: viaPanel})
		return err
	})
	if err != nil {
		var full *BucketFull
		if errors.As(err, &full) {
			return full
		}
		for _, sentinel := range []error{ErrNoDelivery, ErrDelivered, ErrInFlight} {
			if errors.Is(err, sentinel) {
				return sentinel
			}
		}
		return pass(err, "resend")
	}
	if s.work != nil {
		s.work.Notice(tenantID)
	}
	return nil
}
