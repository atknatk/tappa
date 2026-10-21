package api

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// THE SUSPENSION CHECKPOINT (ADR 0028 §10; plan §4.7). ADR 0025 makes a suspension a WRITE lock;
// the API applies the same, with K-26's two defensive writes left open. API-4 built the ONE place
// a /v1 write asks; API-4b wired it to OP-15's column (tenants.suspended_at, 00063) through
// tenant.WriteSuspension and pinned the table below cell by cell against a business the operator
// really suspended (suspension_db_test.go).
//
// §4.7, cell by cell, and where each is decided:
//
//	open while suspended                          where
//	  every GET, /v1/punches, /v1/directory       not a write: never reaches the checkpoint
//	  PUT /v1/employees -- a NEW employee         WriteEmployeeCreate (openWhileSuspended)
//	  resend-activation                           WriteResendActivation (openWhileSuspended)
//	  webhook sending, the invite queue worker,   not a /v1 request: the worker and the triggers
//	  the triggers' event/delivery/projection     never consult it
//	  K-26: key revocation, endpoint disabling    the panel (OP-15's gate must leave them open)
//	403 tenant_suspended
//	  PUT /v1/locations                           WriteLocationPut
//	  PUT /v1/employees changing one (active:false WriteEmployeeChange
//	  included)
//	  DELETE /v1/employees                        WriteEmployeeDelete
//	  key creation; endpoint create/rotate/       the panel (OP-15's gate)
//	  readdress; test ping; "resend" (webhook)
//	  ANY OTHER M11 WRITE                         the zero WriteClass: closed by default
//
// HOW IT CANNOT BE SKIPPED: a /v1 write runs through Surface.write, whose work is handed a Gate;
// the write commits only if the work passed Gate.Admit (write refuses otherwise and rolls back --
// TestWrite_ACommitWithoutTheCheckpointIsRefused). Admit reads the suspension in the write's own
// transaction. A failure to read the suspension is CLOSED: the write is refused as an outage (503),
// never let through (TestWrite_ACheckpointThatCannotReadIsClosed; with the shipped reader,
// TestSuspension_AReadThatFailsIsClosed).
//
// THE READ TAKES NO ROW LOCK -- API-4b's decision, measured (ADR 0028 §10, "API-4b eki"). Under READ
// COMMITTED a plain read is the state at the READ, not at the commit: a suspension committed between
// Admit and the write's commit is not seen by that write. The two locking reads were measured against
// the operator's op_suspend_tenant, which takes the row FOR NO KEY UPDATE (ADR 0025 §2 step 3):
//
//	FOR KEY SHARE  does not conflict with FOR NO KEY UPDATE: the operator does not wait and the
//	               write still reads the old state -- it orders nothing
//	FOR SHARE      conflicts, so every write either commits before the suspension or sees it; but
//	               op_suspend_tenant then waits for every /v1 write of the business in flight, and
//	               share lockers that keep arriving are GRANTED PAST the waiting operator (three
//	               overlapping writers: the operator waited 0.5-4.5 s while 23-204 share locks were
//	               granted; a lock_timeout does not bound the wait, each re-wait starts its own) --
//	               the suspended party, by writing, could hold off its own suspension (a
//	               security_hold among them). The panel's own UpdateTenantAccount and the operator's
//	               other tenant writes (VAT re-check, the API gate) would wait behind /v1 writes too.
//	plain read     the operator never waits for /v1 (TestSuspension_TheReadHoldsNoLockOnTheTenant);
//	               the price, named and pinned: ONE write per request already past its read may
//	               commit after the suspension -- in total, EVERY request in flight at the
//	               suspension's commit, bounded by the request's own limits (RequestTimeout 30 s,
//	               idle_in_transaction_session_timeout 30 s, the pool and the key bucket)
//	               (TestSuspension_AWriteInFlightAtTheSuspensionIsLetThrough) -- the same semantics as
//	               the panel gate, which reads at the request's start (ADR 0025 limit 6: "the effect is
//	               on the next request").
//
// WHAT A REFUSED WRITE LEAVES (API-4b DÜŞÜK round, 2026-10-20): no business row -- the 403 rolls the
// write's transaction back, its Idempotency-Key claim included. Two MAINTENANCE writes are the named
// exceptions, both made before the checkpoint by every authenticated request and both API-4's design:
// the runner's lazy delete of this business's EXPIRED (over 24 h) ledger rows, committed in a
// transaction of its own (apikey.Writes.Run), and the chain's api_keys.last_used_at (at most once a
// minute). TestSuspension_TheSection47Table measures the first and keeps the second out of its trace.

// WriteClass is a /v1 write's §4.7 class. The zero value names nothing and is CLOSED while the
// business is suspended -- a write nobody classified is refused, never let through.
type WriteClass uint8

const (
	writeUnnamed WriteClass = iota
	// WriteLocationPut: PUT /v1/locations/{ref} (API-5). Closed.
	WriteLocationPut
	// WriteEmployeeCreate: PUT /v1/employees/{ref} that opens a new employee (API-6). Open.
	WriteEmployeeCreate
	// WriteEmployeeChange: PUT /v1/employees/{ref} that changes an existing one, active:false
	// included (API-6). Closed.
	WriteEmployeeChange
	// WriteEmployeeDelete: DELETE /v1/employees/{ref} (API-6). Closed.
	WriteEmployeeDelete
	// WriteResendActivation: POST /v1/employees/{ref}/resend-activation (API-6). Open.
	WriteResendActivation
)

// openWhileSuspended is §4.7's left column as far as /v1 writes go. Everything else is closed.
var openWhileSuspended = map[WriteClass]bool{
	WriteEmployeeCreate:   true,
	WriteResendActivation: true,
}

// Suspension answers "is this business suspended?" inside the write's transaction. Declared here,
// at the consumer (§7). The shipped one is tenant.WriteSuspension (cmd/tappa/api.go). API-4's
// always-false NoSuspensionColumn, the stand-in of a schema without the column, is gone with the
// column's arrival: a reader that cannot answer "suspended" must not be wireable
// (TestAPIWiring_TheSuspensionReaderFollowsTheColumn).
type Suspension interface {
	Suspended(ctx context.Context, tx pgx.Tx, tenant uuid.UUID) (bool, error)
}

// Gate is the checkpoint as a write's work sees it: Admit once the work knows which write it is.
type Gate struct {
	suspension Suspension
	tx         pgx.Tx
	tenant     uuid.UUID
	admitted   bool
}

// Admit lets the write through, or refuses it with 403 tenant_suspended (a *Problem the surface
// answers with) when the business is suspended and class is not open while suspended. A failure
// to read the suspension is returned as it is -- an outage, never a pass.
func (g *Gate) Admit(ctx context.Context, class WriteClass) error {
	suspended, err := g.suspension.Suspended(ctx, g.tx, g.tenant)
	if err != nil {
		return err
	}
	if suspended && !openWhileSuspended[class] {
		return problemTenantSuspended
	}
	g.admitted = true
	return nil
}
