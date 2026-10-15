// Package endpoints is the owner's webhook endpoint (M11 API-9, ADR 0028 §6, §7, §10): create it,
// rotate its signing key, change its address, switch it off and on, and send it a test ping. One
// endpoint per business (webhook_endpoints_one_per_tenant). The deliveries themselves -- the table,
// the worker, the retries, the delivery log, "resend" -- are API-10's; this package hands that work
// two things it needs: the sealed key's AAD rule (the endpoint id) and the ping/resend bucket.
//
// It is a package of its own, beside internal/webhook rather than in it, because internal/webhook is
// K-19's worker and is pinned to read nothing itself (TestWorker_NoTenantWideRead: no internal/db, no
// store, no pgx). This package reads and writes one business's rows, always in WithTenant with an
// explicit tenant_id filter (CLAUDE.md §4.5).
//
// EVERY ACT IS OWNER-ONLY AND RE-READS THE ROLE in its own transaction (internal/apikey's mayManage:
// the handler's gate is not taken on its word) and writes its trail row in the SAME transaction
// (RecordTx). The trail names the endpoint by its id and says what was done and through what
// ("via":"panel") -- NEVER the address, never the key (ADR 0028 §6; CLAUDE.md §7).
//
// K-22 ("kapı her yolda", ADR 0028 §2): creating, rotating, re-addressing, switching on and pinging
// need the business's integration API open. SWITCHING OFF never does -- it is a defence (K-26), like
// revoking a key (ADR 0028's API-3 note item 8).
//
// SUSPENSION (ADR 0028 §10; plan §4.7). OP-15's tenants.suspended_at is not on this branch, so no
// act here reads a suspension yet; the table below is the decision, written where OP-15's gate will
// read it (OpenWhileSuspended), and pinned (TestEndpoints_SuspensionClassesAreSection10). API-4b
// measures the gate itself once the column exists.
//
//	act                          while suspended    ADR 0028 §10
//	switch off (Disable)         OPEN               K-26: "uç nokta devre dışı bırakma"
//	create                       403                "uç nokta oluşturma"
//	rotate the key               403                "döndürme"
//	change the address           403                "adres değiştirme"
//	test ping                    403                "test ping"
//	switch on (Enable)           403                not named -> closed by default ("tabloda adı
//	                                                geçmeyen her M11 yazması askıda kapalıdır")
//
// 🔴 THE SIGNING KEY (ADR 0028 §6, K-12; CLAUDE.md §4.7). Minted here, sealed with sun.Seal under
// TAPPA_WEBHOOK_KEK with AAD = the endpoint id's 16 bytes (so a sealed value copied to another row
// does not open), stored only sealed, and returned to the owner on two answers only -- Created and
// Rotated -- as a webhook.SigningKey, which prints a placeholder everywhere else. Nothing returns it
// afterwards: there is no "show". A rotation overwrites the seal, so the old key signs nothing from
// the commit on.
package endpoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/internal/webhook"
)

// The trail's actions (target = the endpoint's id). detail carries "via" and, on a ping's result,
// the closed class and the status -- nothing else.
const (
	ActionCreated     = "webhook_endpoint.created"
	ActionRotated     = "webhook_endpoint.rotated"
	ActionReaddressed = "webhook_endpoint.readdressed"
	ActionDisabled    = "webhook_endpoint.disabled"
	ActionEnabled     = "webhook_endpoint.enabled"
	// ActionPing is a SLOT of the bucket: written, under the bucket's lock, BEFORE the ping is sent.
	ActionPing = "webhook_endpoint.ping"
	// ActionPingResult is what the ping came back with (class, status), written after.
	ActionPingResult = "webhook_endpoint.ping_result"
	// ActionResend is API-10's "resend" from the panel -- named here because it spends the SAME
	// bucket (ADR 0028 §6). API-10 writes it, under LockWebhookBucket, exactly as Ping writes its slot.
	ActionResend = "webhook_delivery.resend"

	viaPanel = "panel"
)

// BucketActions are the trail rows that spend the one bucket (ADR 0028 §6: "ping ve yeniden gönder
// aynı kovayı paylaşır").
var BucketActions = []string{ActionPing, ActionResend}

// BucketLimit is the bucket's size: ten an hour, per business (ADR 0028 §6).
const BucketLimit = 10

// BucketWindow is the bucket's window -- its ONE source: ReadWebhookBucket counts over it
// (bucketRead passes it as @window_seconds) and the next free slot is dated by it.
const BucketWindow = time.Hour

// bucketRead is ReadWebhookBucket's argument for this business: the bucket's actions and window.
func bucketRead(tenantID uuid.UUID) store.ReadWebhookBucketParams {
	return store.ReadWebhookBucketParams{TenantID: tenantID, Actions: BucketActions, WindowSeconds: int32(BucketWindow / time.Second)}
}

const (
	ownerRole       = "owner"
	liveAdmin       = "active"
	onePerTenantKey = "webhook_endpoints_one_per_tenant"
)

// The acts' refusals. Each is a fixed sentinel; none carries an address or a key.
var (
	ErrNotPermitted = errors.New("webhook endpoint: only an active owner manages the webhook")
	ErrAPIClosed    = errors.New("webhook endpoint: the business's integration API is not open")
	ErrExists       = errors.New("webhook endpoint: the business already has its one endpoint")
	ErrNoEndpoint   = errors.New("webhook endpoint: the business has no endpoint")
	ErrDisabled     = errors.New("webhook endpoint: the endpoint is switched off")
	ErrUnchanged    = errors.New("webhook endpoint: the endpoint was already in that state")
	// ErrStoredAddress: the address on file no longer passes the URL rule (a rule that tightened
	// after it was written) -- nothing is sent to it; the owner gives a new one.
	ErrStoredAddress = errors.New("webhook endpoint: the address on file does not pass the address rule")
	// ErrKeyUnreadable: the sealed key does not open under this deployment's KEK (a KEK that changed
	// without a re-seal). Nothing is signed; the owner rotates.
	ErrKeyUnreadable = errors.New("webhook endpoint: the signing key on file cannot be opened")
	// ErrResultNotRecorded: the ping was sent and its Result is in the answer, but its trail row
	// could not be written. The slot was spent all the same.
	ErrResultNotRecorded = errors.New("webhook endpoint: the ping's result could not be recorded")
)

// BucketFull is the ping's refusal when the business has spent its ten slots of the hour. NextAt is
// when the oldest slot frees -- the honest answer to "when can I try again".
type BucketFull struct{ NextAt time.Time }

func (b *BucketFull) Error() string { return "webhook endpoint: the hour's ten pings are spent" }

// Database is WithTenant, declared at the consumer (CLAUDE.md §7).
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Trail is RecordTx.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Sender is internal/egress's Post: one exchange, a closed class back, no text.
type Sender interface {
	Post(ctx context.Context, to egress.Endpoint, body []byte, header http.Header) egress.Result
}

// Service is the owner's acts on the business's endpoint.
type Service struct {
	kek   []byte
	data  Database
	trail Trail
	send  Sender
	now   func() time.Time
}

// New builds the service. The KEK is TAPPA_WEBHOOK_KEK (32 bytes); a deployment without it builds no
// Service and the panel says the webhook is not configured.
func New(kek []byte, data Database, trail Trail, send Sender) (*Service, error) {
	if len(kek) != 32 {
		return nil, fmt.Errorf("webhook endpoint: the KEK must be 32 bytes, got %d", len(kek))
	}
	if data == nil || trail == nil || send == nil {
		return nil, errors.New("webhook endpoint: nil database, trail or sender")
	}
	return &Service{kek: append([]byte(nil), kek...), data: data, trail: trail, send: send, now: time.Now}, nil
}

// Act names an act for the suspension table.
type Act uint8

// The acts.
const (
	actUnnamed Act = iota
	ActCreate
	ActRotate
	ActReaddress
	ActDisable
	ActEnable
	ActPing
)

// openWhileSuspended is ADR 0028 §10's left column as far as the endpoint goes. Everything else --
// the zero Act included -- is closed.
var openWhileSuspended = map[Act]bool{ActDisable: true}

// OpenWhileSuspended reports whether act stays open while the business is suspended (ADR 0028 §10,
// K-26). OP-15's gate reads it.
func OpenWhileSuspended(act Act) bool { return openWhileSuspended[act] }

// Endpoint is the panel's view of the business's endpoint. No key.
type Endpoint struct {
	ID uuid.UUID
	// Address is the canonical address; zero when the stored one no longer passes the rule
	// (AddressRefused).
	Address        egress.Endpoint
	AddressRefused bool
	Active         bool
	CreatedAt      time.Time
	RotatedAt      *time.Time
}

// Bucket is the ping/resend bucket as the owner sees it.
type Bucket struct {
	Spent int
	Limit int
	// NextAt is when a slot frees when the bucket is full; zero otherwise.
	NextAt time.Time
}

// Listing is the panel's read.
type Listing struct {
	APIEnabled bool
	// Endpoint is nil when the business has none.
	Endpoint *Endpoint
	Bucket   Bucket
}

// Created is the answer to "create": the one (with Rotated) that carries the signing key.
type Created struct {
	ID  uuid.UUID
	Key webhook.SigningKey
}

// Rotated is the answer to "rotate": the new key.
type Rotated struct {
	ID        uuid.UUID
	Key       webhook.SigningKey
	RotatedAt time.Time
}

// Pinged is what a ping came back with.
type Pinged struct {
	ID     uuid.UUID
	Result egress.Result
}

type viaDetail struct {
	Via string `json:"via"`
}

type pingResultDetail struct {
	Class  string `json:"class"`
	Status int    `json:"status"`
	Via    string `json:"via"`
}

// mayManage is the domain's role gate (internal/apikey's): an active owner of THIS tenant, read in
// the act's own transaction and context.
func mayManage(ctx context.Context, q *store.Queries, tenantID, actorID uuid.UUID) error {
	a, err := q.GetAdminByID(ctx, store.GetAdminByIDParams{ID: actorID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotPermitted
	}
	if err != nil {
		return fmt.Errorf("read the actor: %w", err)
	}
	if a.Role != ownerRole || a.Status != liveAdmin {
		return ErrNotPermitted
	}
	return nil
}

// apiOpen is K-22's gate, read in the act's transaction.
func apiOpen(ctx context.Context, q *store.Queries, tenantID uuid.UUID) (bool, error) {
	at, err := q.GetTenantAPIEnabledAt(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("read the gate: %w", err)
	}
	return at != nil, nil
}

func requireOpen(ctx context.Context, q *store.Queries, tenantID uuid.UUID) error {
	open, err := apiOpen(ctx, q, tenantID)
	if err != nil {
		return err
	}
	if !open {
		return ErrAPIClosed
	}
	return nil
}

func (s *Service) record(ctx context.Context, tx pgx.Tx, tenantID, actorID uuid.UUID, action string, target uuid.UUID, detail any) (uuid.UUID, error) {
	return s.trail.RecordTx(ctx, tx, audit.Event{
		TenantID: tenantID, ActorID: &actorID, Action: action, Target: target.String(), Detail: detail,
	})
}

// List reads the business's endpoint, its API gate and its bucket.
func (s *Service) List(ctx context.Context, tenantID, actorID uuid.UUID) (Listing, error) {
	var out Listing
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		var err error
		if out.APIEnabled, err = apiOpen(ctx, q, tenantID); err != nil {
			return err
		}
		row, err := q.GetWebhookEndpoint(ctx, tenantID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return fmt.Errorf("read the endpoint: %w", err)
		default:
			e := &Endpoint{ID: row.ID, Active: row.Active, CreatedAt: row.CreatedAt, RotatedAt: row.RotatedAt}
			if addr, aerr := egress.EndpointFromStore(row.Url); aerr == nil {
				e.Address = addr
			} else {
				e.AddressRefused = true
			}
			out.Endpoint = e
		}
		b, err := q.ReadWebhookBucket(ctx, bucketRead(tenantID))
		if err != nil {
			return fmt.Errorf("read the bucket: %w", err)
		}
		out.Bucket = Bucket{Spent: int(b.Spent), Limit: BucketLimit}
		if int(b.Spent) >= BucketLimit {
			out.Bucket.NextAt = b.Oldest.Add(BucketWindow)
		}
		return nil
	})
	if err != nil {
		return Listing{}, pass(err, "list")
	}
	return out, nil
}

// CreateCommand is "create the endpoint". The actor and the tenant come from the session.
type CreateCommand struct {
	TenantID, ActorID uuid.UUID
	Address           string
}

// Create validates the address, mints and seals a key under the new endpoint's id and inserts the
// row with its trail row -- one transaction. The key is in the answer and nowhere else.
func (s *Service) Create(ctx context.Context, c CreateCommand) (Created, error) {
	addr, err := egress.ParseEndpoint(c.Address)
	if err != nil {
		return Created{}, err
	}
	id := uuid.New()
	key, sealed, err := s.mintSealed(id)
	if err != nil {
		return Created{}, err
	}
	err = s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, c.TenantID, c.ActorID); err != nil {
			return err
		}
		if err := requireOpen(ctx, q, c.TenantID); err != nil {
			return err
		}
		_, err := q.InsertWebhookEndpoint(ctx, store.InsertWebhookEndpointParams{
			TenantID: c.TenantID, ID: id, Url: addr.StoredForm(), SigningKeySealed: sealed,
		})
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == onePerTenantKey {
			return ErrExists
		}
		if err != nil {
			// pgx does not quote bind parameters into its errors: the url and the seal stay out.
			return fmt.Errorf("insert: %w", err)
		}
		_, err = s.record(ctx, tx, c.TenantID, c.ActorID, ActionCreated, id, viaDetail{Via: viaPanel})
		return err
	})
	if err != nil {
		return Created{}, pass(err, "create")
	}
	return Created{ID: id, Key: key}, nil
}

// mintSealed mints a key and seals it for endpoint id.
func (s *Service) mintSealed(id uuid.UUID) (webhook.SigningKey, []byte, error) {
	key, err := webhook.MintSigningKey()
	if err != nil {
		return webhook.SigningKey{}, nil, err
	}
	sealed, err := sun.Seal(s.kek, id[:], key.SealInput())
	if err != nil {
		return webhook.SigningKey{}, nil, fmt.Errorf("webhook endpoint: seal: %w", err)
	}
	return key, sealed, nil
}

// endpointID reads the business's endpoint id, ErrNoEndpoint when there is none.
func endpointID(ctx context.Context, q *store.Queries, tenantID uuid.UUID) (store.GetWebhookEndpointRow, error) {
	row, err := q.GetWebhookEndpoint(ctx, tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNoEndpoint
	}
	if err != nil {
		return row, fmt.Errorf("read the endpoint: %w", err)
	}
	return row, nil
}

// Rotate gives the endpoint a new signing key. The old one is dead from the commit on.
func (s *Service) Rotate(ctx context.Context, tenantID, actorID uuid.UUID) (Rotated, error) {
	var out Rotated
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		if err := requireOpen(ctx, q, tenantID); err != nil {
			return err
		}
		row, err := endpointID(ctx, q, tenantID)
		if err != nil {
			return err
		}
		key, sealed, err := s.mintSealed(row.ID)
		if err != nil {
			return err
		}
		at, err := q.RotateWebhookEndpointKey(ctx, store.RotateWebhookEndpointKeyParams{
			SigningKeySealed: sealed, TenantID: tenantID, ID: row.ID,
		})
		if err != nil {
			return fmt.Errorf("rotate: %w", err)
		}
		out = Rotated{ID: row.ID, Key: key}
		if at != nil {
			out.RotatedAt = *at
		}
		_, err = s.record(ctx, tx, tenantID, actorID, ActionRotated, row.ID, viaDetail{Via: viaPanel})
		return err
	})
	if err != nil {
		return Rotated{}, pass(err, "rotate")
	}
	return out, nil
}

// Readdress points the endpoint at a new address. The key is unchanged.
func (s *Service) Readdress(ctx context.Context, tenantID, actorID uuid.UUID, address string) (uuid.UUID, error) {
	addr, err := egress.ParseEndpoint(address)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		if err := requireOpen(ctx, q, tenantID); err != nil {
			return err
		}
		row, err := endpointID(ctx, q, tenantID)
		if err != nil {
			return err
		}
		id = row.ID
		if _, err := q.ReaddressWebhookEndpoint(ctx, store.ReaddressWebhookEndpointParams{
			Url: addr.StoredForm(), TenantID: tenantID, ID: row.ID,
		}); err != nil {
			return fmt.Errorf("readdress: %w", err)
		}
		_, err = s.record(ctx, tx, tenantID, actorID, ActionReaddressed, row.ID, viaDetail{Via: viaPanel})
		return err
	})
	if err != nil {
		return uuid.Nil, pass(err, "readdress")
	}
	return id, nil
}

// Disable switches the endpoint off. Open whatever K-22 says (K-26: a defence).
func (s *Service) Disable(ctx context.Context, tenantID, actorID uuid.UUID) (uuid.UUID, error) {
	return s.setActive(ctx, tenantID, actorID, false)
}

// Enable switches the endpoint back on. Needs the API open (K-22).
func (s *Service) Enable(ctx context.Context, tenantID, actorID uuid.UUID) (uuid.UUID, error) {
	return s.setActive(ctx, tenantID, actorID, true)
}

func (s *Service) setActive(ctx context.Context, tenantID, actorID uuid.UUID, active bool) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		if active {
			if err := requireOpen(ctx, q, tenantID); err != nil {
				return err
			}
		}
		row, err := endpointID(ctx, q, tenantID)
		if err != nil {
			return err
		}
		id = row.ID
		n, err := q.SetWebhookEndpointActive(ctx, store.SetWebhookEndpointActiveParams{Active: active, TenantID: tenantID, ID: row.ID})
		if err != nil {
			return fmt.Errorf("switch: %w", err)
		}
		if n == 0 {
			return ErrUnchanged
		}
		action := ActionDisabled
		if active {
			action = ActionEnabled
		}
		_, err = s.record(ctx, tx, tenantID, actorID, action, row.ID, viaDetail{Via: viaPanel})
		return err
	})
	if err != nil {
		return uuid.Nil, pass(err, "switch")
	}
	return id, nil
}

// pingBody is the ping's synthetic payload (ADR 0028 §6: event "ping"). It names no punch, no
// employee and no venue.
type pingBody struct {
	Event  string `json:"event"`
	ID     string `json:"id"`
	SentAt string `json:"sentAt"`
}

// Ping sends the endpoint a signed, synthetic event. The slot is spent -- the trail row written --
// under the bucket's lock and committed BEFORE the request goes out; no transaction is open while
// the request is in flight; the result is a second trail row. The answer is the closed class and the
// status, never a text.
func (s *Service) Ping(ctx context.Context, tenantID, actorID uuid.UUID) (Pinged, error) {
	var (
		id   uuid.UUID
		addr egress.Endpoint
		key  webhook.SigningKey
		slot uuid.UUID
	)
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayManage(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		if err := requireOpen(ctx, q, tenantID); err != nil {
			return err
		}
		row, err := q.GetWebhookEndpointForSigning(ctx, tenantID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoEndpoint
		}
		if err != nil {
			return fmt.Errorf("read the endpoint: %w", err)
		}
		id = row.ID
		if !row.Active {
			return ErrDisabled
		}
		if addr, err = egress.EndpointFromStore(row.Url); err != nil {
			return ErrStoredAddress
		}
		plain, err := sun.Open(s.kek, row.ID[:], row.SigningKeySealed)
		if err != nil {
			return ErrKeyUnreadable
		}
		if key, err = webhook.ParseSigningKey(string(plain)); err != nil {
			return ErrKeyUnreadable
		}
		lock, err := q.LockWebhookBucket(ctx, tenantID)
		if err != nil {
			// No row: this business's tenants row is not visible in this context -- no lock was
			// taken, so nothing may be counted as if it were.
			return fmt.Errorf("lock the bucket: %w", err)
		}
		if lock.ID != tenantID {
			return errors.New("webhook endpoint: the bucket's lock named another business")
		}
		b, err := q.ReadWebhookBucket(ctx, bucketRead(tenantID))
		if err != nil {
			return fmt.Errorf("read the bucket: %w", err)
		}
		if int(b.Spent) >= BucketLimit {
			return &BucketFull{NextAt: b.Oldest.Add(BucketWindow)}
		}
		slot, err = s.record(ctx, tx, tenantID, actorID, ActionPing, row.ID, viaDetail{Via: viaPanel})
		return err
	})
	if err != nil {
		var full *BucketFull
		if errors.As(err, &full) {
			return Pinged{ID: id}, full
		}
		return Pinged{ID: id}, pass(err, "ping")
	}

	body, err := json.Marshal(pingBody{Event: "ping", ID: slot.String(), SentAt: s.now().UTC().Format(time.RFC3339)})
	if err != nil {
		return Pinged{ID: id}, fmt.Errorf("webhook endpoint: encode the ping: %w", err)
	}
	header := http.Header{
		"Content-Type":          {"application/json"},
		"User-Agent":            {"Taptime-Webhooks/1"},
		webhook.SignatureHeader: {key.Sign(body)},
	}
	res := s.send.Post(ctx, addr, body, header)

	// The result's row: its own short transaction. A failure to write it is returned -- the ping
	// itself happened, and the slot is spent either way.
	werr := s.data.WithTenant(context.WithoutCancel(ctx), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := s.record(ctx, tx, tenantID, actorID, ActionPingResult, id,
			pingResultDetail{Class: string(res.Class), Status: res.Status, Via: viaPanel})
		return err
	})
	if werr != nil {
		return Pinged{ID: id, Result: res}, fmt.Errorf("%w: %w", ErrResultNotRecorded, werr)
	}
	return Pinged{ID: id, Result: res}, nil
}

// pass returns a refusal sentinel as it is and wraps anything else.
func pass(err error, act string) error {
	for _, s := range []error{ErrNotPermitted, ErrAPIClosed, ErrExists, ErrNoEndpoint, ErrDisabled, ErrUnchanged,
		ErrStoredAddress, ErrKeyUnreadable} {
		if errors.Is(err, s) {
			return s
		}
	}
	return fmt.Errorf("webhook endpoint: %s: %w", act, err)
}
