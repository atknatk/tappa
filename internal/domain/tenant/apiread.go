package tenant

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// APIReadTimeout bounds every statement of an integration API read -- the punch feed, the history
// query, the employee reads (API-7b review, G-ORTA's extra guard) and the directory (API-8). The surface's buckets bound how
// many requests a key makes; this bounds what one of them can cost the database.
//
// THE NUMBER, AND WHY: the slowest API read measured is well under a second (a history page at a
// million events: median 16 ms, p99 23 ms; the forged-S0 page before the window fix: 660 ms; a cold
// sparse-scope feed page: 273 ms), so 5 s is several times the worst seen; and two of them fit inside
// the request's own deadline (httpx.RequestTimeout, 30 s) with room to spare, so a runaway statement
// is the database's 57014 and this surface's 503 -- the backend freed -- long before the router gives
// up on the request (TestAPIRead_EveryReadCarriesTheBound).
const APIReadTimeout = 5 * time.Second

// boundAPIRead is an API read's first statement.
func boundAPIRead(ctx context.Context, tx pgx.Tx) error {
	return db.BoundStatements(ctx, tx, APIReadTimeout)
}
