// Package audit appends events to audit_log — the trail that makes CLAUDE.md
// §4.6 ("kayıt asla kaybolmaz") true for things that are not attendance records:
// a replayed activation code, a rate-limited caller, a device takeover.
//
// WHAT THIS PACKAGE IS NOT:
//
//   - It is NOT the attendance record. A tap writes to `transactions` (M5-05);
//     audit_log is the administrative trail beside it.
//   - It does NOT decide anything. It stores what the caller says happened.
//   - It does NOT scrub. A jsonb column cannot inspect its own contents and
//     neither can a generic writer, so §4.7 ("no secrets in detail") is kept by
//     the CALLER building a typed, hand-written detail struct per event — never
//     by dumping a request, a params struct or a map of whatever was in scope.
//     The event types in internal/invite and internal/handler follow that rule
//     and their tests assert the rendered JSON carries no code and no hash.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Database is the narrow slice of *db.DB this package needs, declared HERE at the
// consumer (CLAUDE.md §7).
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Recorder appends audit events. Dependencies are injected as struct fields (§7).
type Recorder struct {
	data Database
}

// New builds a Recorder.
func New(data Database) (*Recorder, error) {
	if data == nil {
		return nil, errors.New("audit: nil database")
	}
	return &Recorder{data: data}, nil
}

// Event is one row of the trail.
//
// TenantID is REQUIRED and this is the package's hard edge: audit_log.tenant_id
// is NOT NULL with an FK to tenants (00005), so an event whose owner is unknown
// cannot be stored at all. Callers that may hold an unattributable event — the
// activation endpoint, when a code resolves to nothing — must log it instead and
// say so out loud; see db/queries/audit.sql for why no "system tenant" is
// invented to paper over it.
type Event struct {
	TenantID uuid.UUID
	// ActorID is the ADMIN who caused the event, or nil for the system and for
	// an employee acting on themselves (00005: the column is polymorphic and
	// FK-less, so it must not be filled with an employee id).
	ActorID *uuid.UUID
	// Action is the event name, e.g. "activation.completed". Free text by schema
	// decision (00005) so a new event type needs no migration.
	Action string
	// Target is the affected entity as opaque text, "" when the event has no
	// single target.
	Target string
	// Detail is marshalled to the jsonb column. It MUST be a purpose-built struct
	// or a small literal map with known keys — see the package doc.
	Detail any
}

// Record appends the event in its OWN transaction and returns the new row's id.
//
// ITS OWN TRANSACTION IS THE POINT, not an accident of convenience. The caller
// that most needs this package is the one whose main transaction FAILED (an
// invite that could not be consumed): db.WithTenant rolls that transaction back,
// so an audit row written inside it would be rolled back with it and the failure
// would vanish — precisely the §4.6 loss the trail exists to prevent. A caller
// that wants the row to share the fate of its transaction uses RecordTx instead,
// and should be able to say why.
func (r *Recorder) Record(ctx context.Context, e Event) (uuid.UUID, error) {
	params, err := e.params()
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = r.data.WithTenant(ctx, e.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).RecordAuditEvent(ctx, params)
		if e != nil {
			return e
		}
		id = row.ID
		return nil
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("audit: record %q: %w", e.Action, err)
	}
	return id, nil
}

// RecordTx appends the event inside a transaction the caller already owns, so it
// commits or rolls back WITH that caller's work. Use it when the event is only
// true if the surrounding change is true (e.g. "these sessions were revoked").
//
// The caller is responsible for having established the tenant context; passing a
// tx from db.WithTenant(e.TenantID, …) is the only correct use, and the RLS WITH
// CHECK on audit_log refuses the row if the tenants disagree.
func (r *Recorder) RecordTx(ctx context.Context, tx pgx.Tx, e Event) (uuid.UUID, error) {
	params, err := e.params()
	if err != nil {
		return uuid.Nil, err
	}
	row, err := store.New(tx).RecordAuditEvent(ctx, params)
	if err != nil {
		return uuid.Nil, fmt.Errorf("audit: record %q: %w", e.Action, err)
	}
	return row.ID, nil
}

// params validates the event and renders its detail to JSON.
//
// A nil Detail becomes `{}` rather than SQL NULL: the column is NOT NULL DEFAULT
// '{}' and 00005 wants every row to carry a well-formed object even when there
// is nothing to say. A Detail that cannot be marshalled is an ERROR, never a
// dropped field (§7 forbids silent acceptance) — and never a fallback like
// fmt.Sprintf("%v", detail), which is how a secret ends up in an audit row.
func (e Event) params() (store.RecordAuditEventParams, error) {
	if e.TenantID == uuid.Nil {
		return store.RecordAuditEventParams{}, errors.New("audit: tenant id is required")
	}
	if e.Action == "" {
		return store.RecordAuditEventParams{}, errors.New("audit: action is required")
	}
	detail := []byte("{}")
	if e.Detail != nil {
		b, err := json.Marshal(e.Detail)
		if err != nil {
			// THE WRAPPED ERROR IS encoding/json's AND IT CAN QUOTE A VALUE —
			// measured, not assumed: json.Marshal(math.Inf(1)) says "unsupported
			// value: +Inf". This is wrapped anyway rather than replaced, because
			// swallowing the cause would leave a caller debugging a silent
			// failure (§7), and the exposure is bounded in a way that can be
			// stated exactly: every Go string marshals successfully, so a SECRET
			// (a code, a hash, a token — all strings) can never reach this
			// branch. What can are NaN/±Inf, channels, functions and cyclic
			// values, none of which is a credential. The message this package
			// adds names only the action.
			return store.RecordAuditEventParams{}, fmt.Errorf("audit: marshal detail for %q: %w", e.Action, err)
		}
		detail = b
	}
	var target *string
	if e.Target != "" {
		t := e.Target
		target = &t
	}
	return store.RecordAuditEventParams{
		TenantID: e.TenantID,
		ActorID:  e.ActorID,
		Action:   e.Action,
		Target:   target,
		Detail:   detail,
	}, nil
}
