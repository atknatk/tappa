package tenant

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// WriteSuspension is ADR 0025's suspension as /v1's checkpoint reads it (M11 API-4b; ADR 0028 §10;
// internal/handler/api/suspension.go): tenants.suspended_at (00063), read through the WRITE'S OWN
// transaction -- the one apikey.Writes.Run opened in the key's tenant context -- so the answer and
// the write it lets through are one transaction's. It holds nothing: no pool, no cache (a
// suspension takes effect on the next write, never after a cache's lifetime).
//
// The read is GetTenantSuspended (db/queries/tenants.sql): an explicit `id = @tenant_id` and the
// transaction's RLS context (§4.5), and NO row lock -- the measured decision written there and in
// ADR 0028 §10 ("API-4b eki"). A tenant that is not there (absent, or hidden by RLS) is an error,
// which the checkpoint answers as an outage (503): never "not suspended".
type WriteSuspension struct{}

// Suspended reports whether the business is suspended, read in tx.
func (WriteSuspension) Suspended(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (bool, error) {
	suspended, err := store.New(tx).GetTenantSuspended(ctx, tenantID)
	if err != nil {
		return false, fmt.Errorf("tenant: read the suspension: %w", err)
	}
	return suspended, nil
}
