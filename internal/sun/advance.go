package sun

import (
	"context"
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Atomic counter advance -- THE replay guard (CLAUDE.md §4.4, the single most
// critical step of the whole product). This file is a thin wrapper around the
// M1-08 store.AdvanceTagCounter query; it deliberately holds NO comparison logic
// of its own. Atomicity lives in ONE SQL statement (the query's strict
// less-than guard inside a single UPDATE ... RETURNING; see db/queries/tags.sql),
// never in Go: a read-then-compare-then-write here would be a TOCTOU replay
// hole, and a sync.Mutex would be a
// single-process fiction that collapses the moment a second instance runs. The
// database row lock is the only correct synchronisation point, so this wrapper
// does nothing but call the query and translate its 0-row outcome into a reject.
//
// ORDER IS FIXED (m2-sun.md M2-06, skill tappa-sun): the CMAC must be verified
// BEFORE the counter is advanced. This step is kept SEPARATE from verifyMAC so
// that M2-07's sun.Verify can sequence them explicitly -- advancing first would
// let an attacker push the counter forward with invalid-CMAC requests and get
// legitimate taps rejected (a DoS). AdvanceCounter must therefore only ever be
// called after the MAC has already been confirmed.
//
// NO THRESHOLD LIVES HERE. The gap (skipped reads) is returned as data for the
// caller; the decision of whether a large gap is suspicious belongs to the
// base:ctr-gap-review policy (M3-06, Q21), NOT to internal/sun. Forward jumps are
// accepted unconditionally at this layer: strict `ctr > last_ctr` is enough, we
// do NOT require last_ctr+1 (someone may have tapped the plaque without opening
// the page). The old "> 1000 -> flag" threshold was removed for exactly this
// reason (URL prefetching alone produces ~10-wide gaps).

// ErrReplay reports that the counter did not advance: the presented ctr was not
// strictly greater than the stored last_ctr. This is the expected, non-error
// outcome of a replayed tap (same or smaller ctr) OR of losing a concurrent race
// for the same (uid, ctr) -- the SQL UPDATE matched 0 rows and returned
// pgx.ErrNoRows. It is a distinct sentinel (not the raw pgx.ErrNoRows and not a
// generic error) so a caller can errors.Is it to REJECT the tap, while any other
// non-nil error signals an actual infrastructure failure to surface. It is never
// a silent success: §4.6 forbids silent approval, and a replay is a hard reject.
var ErrReplay = errors.New("sun: tap rejected: counter did not advance (replay or concurrent tap)")

// counterAdvancer is the one-method slice of store.Querier that AdvanceCounter
// needs, declared at the CONSUMER (CLAUDE.md §7) rather than depending on the
// whole producer-defined interface. The tenant-scoped store.New(tx) obtained
// inside a db.WithTenant transaction satisfies it directly, and so does any test
// double implementing just this method -- which lets the error-mapping logic be
// unit-tested without a database while the concurrency proof runs against real
// Postgres.
type counterAdvancer interface {
	AdvanceTagCounter(ctx context.Context, arg store.AdvanceTagCounterParams) (store.AdvanceTagCounterRow, error)
}

// AdvanceCounter atomically advances tag uid's monotonic read counter to ctr,
// scoped to tenantID, and returns the gap (ctr - old_last_ctr - 1, i.e. the
// number of reads skipped since we last saw this tag). q must be a tenant-scoped
// querier from an in-progress db.WithTenant transaction, so the UPDATE runs
// under RLS with the tenant context set.
//
// Outcomes:
//   - success: the counter advanced, gap is the skipped-read count, err is nil.
//   - replay/race: the ctr did not strictly exceed last_ctr, so the atomic UPDATE
//     matched 0 rows; gap is 0 and err is ErrReplay (errors.Is-matchable) -> reject.
//   - infrastructure failure: any other error, wrapped for context -> surface it.
//
// ctr is the numeric read counter (Params.Ctr), a 24-bit value in [0, 2^24-1];
// it always fits store's int32 column (max 2^31-1), so the conversion below is
// lossless for every valid tag counter. The counter is NEVER compared in Go here
// -- the strict less-than test happens atomically inside the single UPDATE
// (store.AdvanceTagCounter), which is what makes concurrent taps for the same
// (uid, ctr) resolve to exactly one winner.
func AdvanceCounter(ctx context.Context, q counterAdvancer, tenantID uuid.UUID, uid string, ctr uint32) (gap int32, err error) {
	row, err := q.AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{
		Ctr:      int32(ctr),
		TenantID: tenantID,
		Uid:      uid,
	})
	if err != nil {
		// 0 rows updated means the strict less-than guard rejected the
		// counter: a replay (equal/smaller ctr) or the losing side of a
		// concurrent race. Map it to the ErrReplay sentinel; leave every other
		// error wrapped so real DB failures stay distinguishable from a reject.
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrReplay
		}
		return 0, fmt.Errorf("sun: advance counter: %w", err)
	}
	return row.CtrGap, nil
}
