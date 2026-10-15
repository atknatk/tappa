package api

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// THE SUSPENSION CHECKPOINT (ADR 0028 §10; plan §4.7). ADR 0025 makes a suspension a WRITE lock;
// the API applies the same, with K-26's two defensive writes left open. API-4 builds the ONE place
// a /v1 write asks; the table's tests are API-4b's (they need OP-15's tenants.suspended_at, which
// this branch does not have yet).
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
// transaction -- under READ COMMITTED that is the state at the READ, not at the commit: a suspension
// committed between Admit and the write's commit is not seen by that write. Whether the read must
// lock the tenants row (FOR SHARE, against the operator's FOR NO KEY UPDATE) is API-4b's to measure
// with OP-15's column (plan §5, API-4b). A failure to read the suspension is CLOSED: the write is
// refused as an outage (503), never let through (TestWrite_ACheckpointThatCannotReadIsClosed).

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
// at the consumer (§7).
type Suspension interface {
	Suspended(ctx context.Context, tx pgx.Tx, tenant uuid.UUID) (bool, error)
}

// NoSuspensionColumn is the Suspension of a schema WITHOUT ADR 0025's column: no business can be
// suspended there, so it always answers false. It is what this branch wires (OP-15, which adds
// tenants.suspended_at, is not merged into it); OP-15's merge replaces it with the column's read,
// and API-4b's table test is what proves the replacement (plan §5: API-4b depends on OP-15).
type NoSuspensionColumn struct{}

// Suspended answers false: there is no suspension to read.
func (NoSuspensionColumn) Suspended(context.Context, pgx.Tx, uuid.UUID) (bool, error) {
	return false, nil
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
