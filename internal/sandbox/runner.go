package sandbox

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// ONE CONNECTION PER SIMULATION (M11 API-11 fix 1). A simulation's work runs in the /v1 write
// runner's transaction (api.WriteFunc: the Idempotency-Key claim, the suspension checkpoint, the
// work, the result), which holds a pool connection until it ends. Until this fix the work
// provisioned the plaque in a WithTenant of its own and recorded the tap through the tap page's
// checkin.Service, whose steps each open a transaction of their own -- every simulation held one
// connection and waited for a second. With as many simulations at once as the pool has connections
// none of them got the second: measured on pool_max_conns=4, three at once were 201 in 0.17 s and
// four were 4 x 503 at the request's deadline (the router's 30 s without one); on a pool of 2, four at
// once took a /v1 read sent meanwhile down with them (ADR 0028, API-11 note, item 16's table;
// TestSandbox_SimulationsHoldOneConnectionEach).
//
// Now the whole tap runs on the runner's transaction. onRunner is the database the simulation's
// own SUN pre-check, tap recorder and trail are built over (sandbox.go, New): every call they make
// runs on the transaction the context carries --
//
//	GetTagByUID  db.DB.GetTagByUIDOn: the context-less resolver on the transaction, behind the same
//	             virtual-plaque door (internal/db/virtualtags.go)
//	WithTenant   db.WithTenantIn: a savepoint of the transaction, in the KEY'S business's
//	             context and only in it -- the counter's atomic advance (an own plaque's
//	             store.AdvanceTagCounter in its business, which is the key's; a holding peer's
//	             plaque through holding_advance_tag_counter, the definer function the tap path
//	             calls in the EMPLOYEE's business, which is the key's as well -- never in the
//	             plaque's), the person's lock, the policy set, the decision, the INSERT and the
//	             trail all ask for it
//
// and a call whose context carries no runner transaction is an error, never a fall back to the
// pool: a second connection is exactly what this file exists to rule out.
//
// ANY OTHER TENANT IS REFUSED (API-11 low round, fail-closed): a WithTenant for a business that is
// not the key's is an error, before any savepoint and with the runner's context untouched. The
// simulation never asks for one (both fix-round lenses measured it: zero calls with another tenant,
// zero nested), so the refusal costs nothing today; it means a later change of the tap path that
// asks for the plaque's business fails the request instead of running a statement in a business
// the key does not belong to, or relying on WithTenantIn's restore to give the runner its context
// back (TestOnRunner_RefusesAnotherBusinessesContext).
//
// WHAT THAT CHANGES, on purpose: the claim, the plaque, the counter's advance, the record and the
// ledger's result COMMIT TOGETHER. A tap is recorded exactly once per Idempotency-Key -- a request
// that dies anywhere rolls all of it back, the counter included, and its retry is the first touch
// again -- and a concurrent request with the same key waits on the claim and replays the committed
// answer. The locks the tap takes (the plaque's row, the person's advisory lock, the feed's
// counters) are held to the end of the runner's transaction, which is the result's one UPDATE
// after the record. Nothing the tap page does is changed by it: the tap page keeps its own objects
// on the pool (cmd/tappa's run()).

// runnerKey carries the runner's transaction in a context.
type runnerKey struct{}

type runnerTx struct {
	tx     pgx.Tx
	tenant uuid.UUID
}

// onTheRunner returns ctx carrying tx, the runner's transaction in the key's business's context.
func onTheRunner(ctx context.Context, tx pgx.Tx, tenant uuid.UUID) context.Context {
	return context.WithValue(ctx, runnerKey{}, runnerTx{tx: tx, tenant: tenant})
}

// errOffTheRunner: the simulation's tap path was called without the runner's transaction.
var errOffTheRunner = errors.New("sandbox: a simulated tap's database call outside the runner's transaction")

// errAnotherBusiness: the simulation's tap path asked for a business that is not the key's.
var errAnotherBusiness = errors.New("sandbox: a simulated tap's database call in a business that is not the key's")

func runnerOf(ctx context.Context) (runnerTx, error) {
	rt, ok := ctx.Value(runnerKey{}).(runnerTx)
	if !ok || rt.tx == nil || rt.tenant == uuid.Nil {
		return runnerTx{}, errOffTheRunner
	}
	return rt, nil
}

// onRunner is checkin.Database, sun's resolver and audit.Database for a simulated tap.
type onRunner struct{ data Database }

// GetTagByUID resolves uid on the runner's transaction.
func (o onRunner) GetTagByUID(ctx context.Context, uid string) (db.ResolvedTag, error) {
	rt, err := runnerOf(ctx)
	if err != nil {
		return db.ResolvedTag{}, err
	}
	return o.data.GetTagByUIDOn(ctx, rt.tx, uid)
}

// WithTenant runs fn in a savepoint of the runner's transaction, which is in the key's business's
// context; tenantID must be that business.
func (o onRunner) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	rt, err := runnerOf(ctx)
	if err != nil {
		return err
	}
	if tenantID != rt.tenant {
		return errAnotherBusiness
	}
	return db.WithTenantIn(ctx, rt.tx, rt.tenant, rt.tenant, fn)
}
