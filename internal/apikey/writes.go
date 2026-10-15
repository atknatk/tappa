package apikey

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// THE WRITE RUNNER (M11 API-4, ADR 0028 §8): every /v1 write runs through Writes.Run, in ONE
// transaction in the key's tenant context (WithTenant + an explicit tenant_id filter on every
// statement -- CLAUDE.md §4.5):
//
//	the sweep           at most 100 of THIS tenant's ledger rows older than 24 hours (K-19: no
//	                    cross-tenant sweep, ever), SKIP LOCKED, in a SHORT TRANSACTION OF ITS OWN
//	                    that commits before the write's begins
//	the key's own row   the one expired row this request's key would collide with -- the first
//	                    statement of the write's transaction
//	the claim           with an Idempotency-Key: INSERT ... ON CONFLICT DO NOTHING on
//	                    api_idempotency's primary key, BEFORE the work. A concurrent request with
//	                    the same key waits on that key until this transaction ends; then it finds
//	                    the committed row and replays it -- or, when this one rolled back, claims
//	                    it itself
//	the work            the caller's, in the same transaction
//	the result          the status and the record's id into the claimed row (never a body)
//
// 🔴 WHY THE SWEEP IS NOT IN THE WRITE'S TRANSACTION (API-4 review D1, measured). It was, and it ran
// before the key's own delete: write A's sweep locked the tenant's 100 oldest expired rows, B's sweep
// skipped them and locked the next 100, and with A's key's row in B's set and B's in A's, the two
// per-key deletes waited on each other -- 40P01, deterministically, on a tenant with more than 100
// expired rows. Even without the cycle, a sweep row held for a write's whole lifetime made a write
// with ANOTHER key wait for it. Committed on its own, a sweep's locks last one statement, so the
// only thing a write's transaction can wait on is a request with the SAME key -- which is meant to
// wait (TestIdempotency_TwoKeysOverAFullSweepNeitherDeadlockNorWait).
//
// A process that dies anywhere in between rolls the whole transaction back, the claim with it: no
// "processing" row can be left behind (the ADR's eliminated option -- a claim in a transaction of
// its own -- is exactly the shape that leaves one). The idempotency key, the request hash and the
// ledger are never logged; this package logs nothing.

// IdempotencyKeyMax is the longest Idempotency-Key, in bytes (00052's api_idempotency_key_shape
// CHECK says the same). A recorded key answers for itself for 24 hours (db/queries/apiidempotency.sql);
// after that it is free again.
const IdempotencyKeyMax = 255

// The runner's refusals.
var (
	// ErrIdempotencyMismatch: the key was used before, within 24 hours, by a request with a
	// different method, route or body -- 422 (ADR 0028 §8).
	ErrIdempotencyMismatch = errors.New("api key: this idempotency key was used with a different request")
	// ErrIdempotencyKeyShape: the key is not 1 to 255 visible ASCII characters (400).
	ErrIdempotencyKeyShape = errors.New("api key: an idempotency key is 1 to 255 visible ASCII characters")
	// ErrUnrecorded: a committed ledger row carries no result. The runner never commits one; it
	// would take a hand-written row. Answered as an outage, never as a success.
	ErrUnrecorded = errors.New("api key: a recorded idempotency key carries no result")
)

// ValidIdempotencyKey reports whether k is 1 to 255 visible ASCII characters (0x21-0x7E): the
// header's whole grammar here, and 00052's CHECK.
func ValidIdempotencyKey(k string) bool {
	if len(k) == 0 || len(k) > IdempotencyKeyMax {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 0x21 || k[i] > 0x7e {
			return false
		}
	}
	return true
}

// validRequestHash is 64 lower-case hex digits (00052's CHECK).
func validRequestHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for i := 0; i < len(h); i++ {
		if (h[i] < '0' || h[i] > '9') && (h[i] < 'a' || h[i] > 'f') {
			return false
		}
	}
	return true
}

// WriteRequest is one /v1 write as the runner sees it.
type WriteRequest struct {
	// Principal is the resolved key: the ONE tenant every statement is scoped to.
	Principal Principal
	// IdempotencyKey is the header's value, "" when the request carried none.
	IdempotencyKey string
	// RequestHash identifies the request (method, route and body -- internal/handler/api), 64
	// lower-case hex digits. Required with an IdempotencyKey; ignored without one.
	RequestHash string
}

// Outcome is what a write produced and what the ledger keeps: the HTTP status (2xx) and the id of
// the record the write produced or touched, nil when it names none.
type Outcome struct {
	Status int
	Ref    *uuid.UUID
}

// WorkFunc is the write itself, in the runner's transaction. An error rolls everything back --
// the claim included -- so a failed write leaves no ledger row and a retry runs it again.
type WorkFunc func(ctx context.Context, tx pgx.Tx) (Outcome, error)

// Writes is the runner. Its one dependency is WithTenant (§7).
type Writes struct {
	data Database
	// afterClaim, when set, runs right after a fresh claim, inside the transaction -- a test's
	// stopping point (the checkin package's recordHooks precedent). nil in production.
	afterClaim func(ctx context.Context, tx pgx.Tx)
	// beforeKey, when set, runs at the start of the write's transaction, after the sweep committed
	// and before the key's own expired row is deleted -- a test's stopping point. nil in production.
	beforeKey func(ctx context.Context)
}

// NewWrites builds the runner.
func NewWrites(data Database) (*Writes, error) {
	if data == nil {
		return nil, errors.New("api key: nil database")
	}
	return &Writes{data: data}, nil
}

// Run runs one write (the package comment above): the lazy delete, the claim, work, the result,
// one transaction. replayed is true when an earlier request with the same key and the same request
// hash had committed: out is then ITS outcome and work did not run.
func (w *Writes) Run(ctx context.Context, req WriteRequest, work WorkFunc) (out Outcome, replayed bool, err error) {
	p := req.Principal
	switch {
	case p.TenantID == uuid.Nil || p.KeyID == uuid.Nil:
		return Outcome{}, false, errors.New("api key: a write needs a resolved key")
	case work == nil:
		return Outcome{}, false, errors.New("api key: nil write")
	case req.IdempotencyKey != "" && !ValidIdempotencyKey(req.IdempotencyKey):
		return Outcome{}, false, ErrIdempotencyKeyShape
	case req.IdempotencyKey != "" && !validRequestHash(req.RequestHash):
		return Outcome{}, false, errors.New("api key: an idempotent write needs its request hash")
	}
	idem := req.IdempotencyKey != ""
	// The sweep commits ON ITS OWN, before the write's transaction: its row locks live for one
	// statement, never for the write's lifetime (see the package comment's measurement).
	if err := w.data.WithTenant(ctx, p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := store.New(tx).DeleteExpiredIdempotency(ctx, p.TenantID)
		return err
	}); err != nil {
		return Outcome{}, false, fmt.Errorf("api key: the lazy delete: %w", err)
	}
	err = w.data.WithTenant(ctx, p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if w.beforeKey != nil {
			w.beforeKey(ctx)
		}
		if idem {
			key := store.DeleteExpiredIdempotencyForKeyParams{TenantID: p.TenantID, KeyID: p.KeyID, IdemKey: req.IdempotencyKey}
			if _, err := q.DeleteExpiredIdempotencyForKey(ctx, key); err != nil {
				return fmt.Errorf("api key: free an expired idempotency key: %w", err)
			}
			n, err := q.ClaimIdempotency(ctx, store.ClaimIdempotencyParams{
				TenantID: p.TenantID, KeyID: p.KeyID, IdemKey: req.IdempotencyKey, RequestHash: req.RequestHash,
			})
			if err != nil {
				return fmt.Errorf("api key: claim the idempotency key: %w", err)
			}
			if n == 0 {
				row, err := q.GetIdempotency(ctx, store.GetIdempotencyParams{TenantID: p.TenantID, KeyID: p.KeyID, IdemKey: req.IdempotencyKey})
				if err != nil {
					// pgx.ErrNoRows too: the row the claim collided with is gone already -- an
					// outage-class answer, and the client retries with the same key.
					return fmt.Errorf("api key: read the recorded idempotency key: %w", err)
				}
				switch {
				case row.RequestHash != req.RequestHash:
					return ErrIdempotencyMismatch
				case row.Status == nil:
					return ErrUnrecorded
				}
				out, replayed = Outcome{Status: int(*row.Status), Ref: row.ResultRef}, true
				return nil
			}
			if w.afterClaim != nil {
				w.afterClaim(ctx, tx)
			}
		}
		o, err := work(ctx, tx)
		if err != nil {
			return err
		}
		if o.Status < http.StatusOK || o.Status > 299 {
			return fmt.Errorf("api key: a write's outcome must be a 2xx status, got %d", o.Status)
		}
		if idem {
			st := int16(o.Status)
			n, err := q.RecordIdempotency(ctx, store.RecordIdempotencyParams{
				Status: &st, ResultRef: o.Ref, TenantID: p.TenantID, KeyID: p.KeyID, IdemKey: req.IdempotencyKey,
			})
			if err != nil {
				return fmt.Errorf("api key: record the outcome: %w", err)
			}
			if n != 1 {
				return fmt.Errorf("api key: recording the outcome matched %d rows, want the claim's 1", n)
			}
		}
		out = o
		return nil
	})
	if err != nil {
		return Outcome{}, false, err
	}
	return out, replayed, nil
}
