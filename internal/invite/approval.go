package invite

import (
	"context"
	"crypto/rand"
	"encoding"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/internal/store"
)

// THE MANAGER-APPROVED ACTIVATION, THE PHONE'S HALF (M16-23; migration 00115). A phone that cannot
// tap a plaque -- no NFC reader -- finishes the activation ADR 0026 started without one:
//
//	consent   unchanged: the wizard's POST /api/activate or the app's bind records the consent on
//	          the invitation, bound to ONE browser or ONE app key (the binding).
//	request   "My phone can't tap" opens a request on the invitation for THAT binding, with a
//	          six-digit pairing code and a short expiry (OpenApproval). Only where no live QR screen
//	          runs (the gate, ApprovalOffered): a screen is a stronger proof and the phone is sent
//	          to it instead -- refused HERE, not merely hidden by the page.
//	decision  a manager approves or rejects it in the panel (internal/domain/tenant, the panel's
//	          half), comparing the code on the phone in front of them with the one in the list.
//	collect   the phone's next status read finds the approval and spends the invitation in ONE
//	          transaction with the approval's collection (ActivateApproved / ActivateDeviceApproved):
//	          "consumed on a manager's approval" implies "that approval was collected, once".
//
// WHAT DOES NOT CHANGE: the consumption is Activate's / ActivateDevice's own statement, with the
// SAME binding rule -- the approval completes an activation only for the browser or key whose
// consent is the invitation's current one. A request whose consent moved to another phone
// collects nothing (ErrConsentMissing), and no `transactions` row is written by any of it: the
// activation is not attendance (CLAUDE.md §5, "Aktivasyon dokunuşu").

// ApprovalTTL is how long a request can be approved and its approval collected: long enough to walk
// to the manager and for them to find the panel, short enough that a code shown once is dead before
// the day is out. 00115 caps it at one hour.
const ApprovalTTL = 15 * time.Minute

// approvalScreenSeenWithin is how recently a live QR screen must have asked for a code to count as
// RUNNING for the gate (CountLiveQRScreensForApproval). The screen's last_seen_at is stamped at most
// every ten minutes while it polls (00114), so twice that tells a screen that is on from one that
// was switched off or unplugged -- which shows nothing to scan and so must not close the door.
const approvalScreenSeenWithin = 20 * time.Minute

// Refusals of the approval path. Like the rest of this package's sentinels they are for the trail,
// not for the visitor; each has its audited word (internal/handler inviteFailureReasons).
var (
	// ErrScreenAvailable: a live QR screen runs where this person works, so the manager approval is
	// not offered; the phone scans the screen instead (the card's gate). Context POPULATED.
	ErrScreenAvailable = errors.New("invite: a live QR screen runs where this person works")
	// ErrNoApproval: this binding never opened a request on the invitation. Context POPULATED.
	ErrNoApproval = errors.New("invite: no approval request for this binding")
	// ErrApprovalNotGranted: the collection found no approved, uncollected, unexpired request for
	// this binding -- the race between a status read and the collection. Nothing was consumed.
	ErrApprovalNotGranted = errors.New("invite: no approval to collect")
)

// PairingCode is the six digits the phone shows and the panel lists. It is NOT a credential -- the
// approval is the manager's press -- but the card keeps it out of every log line and audit detail,
// so it prints redacted on every path and Digits is its one egress (the phone's screen, the panel's
// row).
type PairingCode struct{ v *string }

var (
	_ fmt.Formatter          = PairingCode{}
	_ fmt.Stringer           = PairingCode{}
	_ fmt.GoStringer         = PairingCode{}
	_ slog.LogValuer         = PairingCode{}
	_ encoding.TextMarshaler = PairingCode{}
)

const pairingRedacted = "invite.PairingCode(redacted)"

// ParsePairingCode wraps six digits read from the database.
func ParsePairingCode(v string) PairingCode { return PairingCode{v: &v} }

func (PairingCode) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(pairingRedacted)) }
func (PairingCode) String() string               { return pairingRedacted }
func (PairingCode) GoString() string             { return pairingRedacted }
func (PairingCode) LogValue() slog.Value         { return slog.StringValue(pairingRedacted) }
func (PairingCode) MarshalText() ([]byte, error) { return []byte(pairingRedacted), nil }

// Digits is the code's one egress: the screen that shows it.
func (p PairingCode) Digits() string {
	if p.v == nil {
		return ""
	}
	return *p.v
}

// newPairingCode draws six uniform digits from crypto/rand. A crypto/rand failure is returned, never
// ignored (§7).
func newPairingCode() (PairingCode, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return PairingCode{}, fmt.Errorf("invite: read randomness: %w", err)
	}
	return ParsePairingCode(fmt.Sprintf("%06d", n.Int64())), nil
}

// Approval states, as the phone reads them.
const (
	ApprovalWaiting  = "waiting"
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
	// ApprovalExpired is a request nobody decided in time, one a newer request withdrew, or an
	// approval that ran out before it was collected.
	ApprovalExpired = "expired"
	// ApprovalCollected is an approval already spent -- the activation it completed is done.
	ApprovalCollected = "collected"
)

// Clients of a request (00115's CHECK).
const (
	ApprovalClientBrowser = "browser"
	ApprovalClientApp     = "app"
)

// Approval is one request as the phone sees it.
type Approval struct {
	Context
	RequestID   uuid.UUID
	PairingCode PairingCode
	State       string
	ExpiresAt   time.Time
	// DecidedBy is the panel account that decided, uuid.Nil while undecided.
	DecidedBy uuid.UUID
}

// ApprovalOffered is THE GATE (the card's user decision; SEÇİM recorded on the card): the manager
// approval is offered only where no live QR screen RUNS. Where is the person's HOME venue
// (Context.LocationID -- the venue the wizard names); for a person with none (K-14) it is the whole
// business. A screen at another venue of a chain does not close the door for somebody standing at
// their own, screenless one.
func (m *Manager) ApprovalOffered(ctx context.Context, tenantID, locationID uuid.UUID) (bool, error) {
	var n int32
	err := m.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		n, e = store.New(tx).CountLiveQRScreensForApproval(ctx, screensParams(tenantID, locationID))
		return e
	})
	if err != nil {
		return false, fmt.Errorf("invite: approval offered: %w", err)
	}
	return n == 0, nil
}

func screensParams(tenantID, locationID uuid.UUID) store.CountLiveQRScreensForApprovalParams {
	p := store.CountLiveQRScreensForApprovalParams{
		TenantID:               tenantID,
		SessionLifetimeSeconds: int32(liveqr.ScreenSessionLifetime / time.Second),
		SeenWithinSeconds:      int32(approvalScreenSeenWithin / time.Second),
	}
	if locationID != uuid.Nil {
		id := locationID
		p.LocationID = &id
	}
	return p
}

// OpenApproval opens a request for the invitation's consent bound to b (client: ApprovalClientBrowser
// or ApprovalClientApp). In ONE transaction: the gate (ErrScreenAvailable), the binding (the
// invitation's current consent must be b's -- ErrConsentMissing otherwise), the withdrawal of the
// invitation's open request, and the new one. It consumes nothing.
//
// Outcomes mirror Lookup's: a populated Context with the invitation's refusal (ErrCodeUsed,
// ErrCodeCancelled, ErrCodeExpired, ErrNotActivatable), ErrUnknownCode with a zero Context.
func (m *Manager) OpenApproval(ctx context.Context, c Code, b Binding, client string) (Approval, error) {
	if client != ApprovalClientBrowser && client != ApprovalClientApp {
		return Approval{}, fmt.Errorf("invite: open approval: unknown client %q", client)
	}
	ictx, err := m.Lookup(ctx, c)
	if err != nil {
		return Approval{Context: ictx}, err
	}
	bindingHash, err := b.hash(m.hmacKey.Reveal())
	if errors.Is(err, errMalformed) {
		return Approval{Context: ictx}, ErrConsentMissing
	}
	if err != nil {
		return Approval{Context: ictx}, fmt.Errorf("invite: open approval: %w", err)
	}
	code, err := newPairingCode()
	if err != nil {
		return Approval{Context: ictx}, err
	}
	out := Approval{Context: ictx, PairingCode: code, State: ApprovalWaiting}
	err = m.data.WithTenant(ctx, ictx.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		n, e := q.CountLiveQRScreensForApproval(ctx, screensParams(ictx.TenantID, ictx.LocationID))
		if e != nil {
			return fmt.Errorf("count screens: %w", e)
		}
		if n > 0 {
			return ErrScreenAvailable
		}
		if _, e := q.WithdrawOpenActivationRequests(ctx, store.WithdrawOpenActivationRequestsParams{
			TenantID: ictx.TenantID, InviteID: ictx.InviteID,
		}); e != nil {
			return fmt.Errorf("withdraw: %w", e)
		}
		row, e := q.CreateActivationRequest(ctx, store.CreateActivationRequestParams{
			Client:      client,
			BindingHash: bindingHash,
			PairingCode: code.Digits(),
			TtlSeconds:  int32(ApprovalTTL / time.Second),
			TenantID:    ictx.TenantID,
			InviteID:    ictx.InviteID,
		})
		if e != nil {
			return e
		}
		out.RequestID, out.ExpiresAt = row.ID, row.ExpiresAt
		return nil
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, ErrScreenAvailable):
		return Approval{Context: ictx}, ErrScreenAvailable
	case errors.Is(err, pgx.ErrNoRows):
		// The INSERT selects the invitation for THIS binding while it is usable: nothing came back.
		// Re-read so the trail names why -- the invitation died in between, or the consent is
		// another phone's.
		if again, lerr := m.Lookup(ctx, c); lerr != nil {
			return Approval{Context: again}, lerr
		}
		return Approval{Context: ictx}, ErrConsentMissing
	case isUniqueViolation(err, "activation_requests_one_open_key"):
		// Two opens of one invitation raced and the other committed first: ONE open request is the
		// table's rule. Nothing was written here. Not a refusal of the link -- an infrastructure
		// answer ("try again"), and the retry withdraws the winner like any later open.
		return Approval{Context: ictx}, fmt.Errorf("invite: open approval: a concurrent open won: %w", err)
	default:
		return Approval{Context: ictx}, fmt.Errorf("invite: open approval: %w", err)
	}
}

// ApprovalState reads the latest request b opened on the invitation, and the invitation itself: an
// invitation that can no longer be spent answers its own refusal (Lookup's), whatever the request
// says. ErrNoApproval: b never asked.
func (m *Manager) ApprovalState(ctx context.Context, c Code, b Binding) (Approval, error) {
	ictx, err := m.Lookup(ctx, c)
	if err != nil {
		return Approval{Context: ictx}, err
	}
	bindingHash, err := b.hash(m.hmacKey.Reveal())
	if errors.Is(err, errMalformed) {
		return Approval{Context: ictx}, ErrNoApproval
	}
	if err != nil {
		return Approval{Context: ictx}, fmt.Errorf("invite: approval state: %w", err)
	}
	var row store.GetActivationRequestForBindingRow
	err = m.data.WithTenant(ctx, ictx.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).GetActivationRequestForBinding(ctx, store.GetActivationRequestForBindingParams{
			BindingHash: bindingHash, TenantID: ictx.TenantID, InviteID: ictx.InviteID,
		})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{Context: ictx}, ErrNoApproval
	}
	if err != nil {
		return Approval{Context: ictx}, fmt.Errorf("invite: approval state: %w", err)
	}
	out := Approval{
		Context:     ictx,
		RequestID:   row.ID,
		PairingCode: ParsePairingCode(row.PairingCode),
		ExpiresAt:   row.ExpiresAt,
		State:       approvalStateOf(row),
	}
	if row.DecidedBy != nil {
		out.DecidedBy = *row.DecidedBy
	}
	return out, nil
}

func approvalStateOf(row store.GetActivationRequestForBindingRow) string {
	switch {
	case row.CollectedAt != nil:
		return ApprovalCollected
	case row.Decision != nil && *row.Decision == "rejected":
		return ApprovalRejected
	case row.Expired || row.WithdrawnAt != nil:
		return ApprovalExpired
	case row.Decision != nil && *row.Decision == "approved":
		return ApprovalApproved
	default:
		return ApprovalWaiting
	}
}

// ApprovedActivation is a completed activation on a manager's approval: the activation, the request
// it collected, and the panel account that approved it.
type ApprovedActivation struct {
	Activation
	RequestID  uuid.UUID
	ApprovedBy uuid.UUID
}

// ActivateApproved is Activate on a manager's approval (the browser): the SAME consumption and the
// same Q40 (a) app-device revocation, and -- in the same transaction, right after the consuming
// statement and before anything is revoked -- the collection of the approved request b opened
// (CollectActivationApproval). No approval to collect rolls everything back: ErrApprovalNotGranted,
// nothing consumed.
func (m *Manager) ActivateApproved(ctx context.Context, c Code, b Binding) (ApprovedActivation, error) {
	var req store.CollectActivationApprovalRow
	collect, err := m.collector(b, &req)
	if err != nil {
		return ApprovedActivation{}, err
	}
	act, err := m.consume(ctx, c, b, func(ctx context.Context, q *store.Queries, out *Activation) error {
		if e := collect(ctx, q, out); e != nil {
			return e
		}
		ids, e := q.RevokeAppDevicesForEmployee(ctx, store.RevokeAppDevicesForEmployeeParams{
			TenantID: out.TenantID, EmployeeID: out.EmployeeID,
		})
		if e != nil {
			return fmt.Errorf("invite: revoke the app devices: %w", e)
		}
		out.AppDevicesRevoked = len(ids)
		return nil
	})
	out := ApprovedActivation{Activation: act, RequestID: req.ID}
	if req.DecidedBy != nil {
		out.ApprovedBy = *req.DecidedBy
	}
	if errors.Is(err, ErrApprovalNotGranted) {
		return out, ErrApprovalNotGranted
	}
	return out, err
}

// ApprovedDeviceActivation is ActivateDeviceApproved's result.
type ApprovedDeviceActivation struct {
	DeviceActivation
	RequestID  uuid.UUID
	ApprovedBy uuid.UUID
}

// ActivateDeviceApproved is ActivateDevice on a manager's approval (the app): the collection first,
// then ActivateDevice's revocations and the device's birth, one transaction.
func (m *Manager) ActivateDeviceApproved(ctx context.Context, c Code, b Binding, d DeviceParams) (ApprovedDeviceActivation, error) {
	var req store.CollectActivationApprovalRow
	collect, err := m.collector(b, &req)
	if err != nil {
		return ApprovedDeviceActivation{}, err
	}
	act, err := m.activateDevice(ctx, c, b, d, collect)
	out := ApprovedDeviceActivation{DeviceActivation: act, RequestID: req.ID}
	if req.DecidedBy != nil {
		out.ApprovedBy = *req.DecidedBy
	}
	if errors.Is(err, ErrApprovalNotGranted) {
		return out, ErrApprovalNotGranted
	}
	return out, err
}

// collector is the continuation step that collects the approval b opened, writing the collected row
// into req. Its binding hash is computed once, here.
func (m *Manager) collector(b Binding, req *store.CollectActivationApprovalRow) (func(context.Context, *store.Queries, *Activation) error, error) {
	bindingHash, err := b.hash(m.hmacKey.Reveal())
	if errors.Is(err, errMalformed) {
		return nil, ErrConsentMissing
	}
	if err != nil {
		return nil, fmt.Errorf("invite: collect approval: %w", err)
	}
	return func(ctx context.Context, q *store.Queries, out *Activation) error {
		row, e := q.CollectActivationApproval(ctx, store.CollectActivationApprovalParams{
			BindingHash: bindingHash, TenantID: out.TenantID, InviteID: out.InviteID,
		})
		if errors.Is(e, pgx.ErrNoRows) {
			// Travels out of consume wrapped (afterConsumeError); the callers unwrap it.
			return ErrApprovalNotGranted
		}
		if e != nil {
			return fmt.Errorf("invite: collect approval: %w", e)
		}
		*req = row
		return nil
	}, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == constraint
}
