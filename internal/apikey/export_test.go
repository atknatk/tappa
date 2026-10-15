package apikey

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// KeyedHashForTest exposes the stored value's construction to the external tests' canaries.
func KeyedHashForTest(macKey []byte, raw string) string {
	h, err := keyedHash(macKey, raw)
	if err != nil {
		panic(err)
	}
	return h
}

// SetAfterClaimForTest installs the runner's stopping point right after a fresh claim.
func SetAfterClaimForTest(w *Writes, f func(ctx context.Context, tx pgx.Tx)) { w.afterClaim = f }
