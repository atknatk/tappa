package sandbox

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestOnRunner_RefusesAnotherBusinessesContext (API-11 low round, runner.go): on the runner's
// transaction, held in business A's context, onRunner.WithTenant(B) is an error before anything
// runs -- fn is not called, no savepoint is taken, and the transaction is still in A's context and
// usable. WithTenant(A) runs fn in A's context (control).
//
// MUTANT (measured): the refusal removed -- WithTenant(B) runs fn in B's context: red.
//
// WithTenantIn's restore to the outer context is unobservable here by construction: the only call
// that reaches it names the runner's own business, so the "restore" writes the value already there.
// Its mutant (the restore removed) is red in internal/db's
// TestWithTenantIn_SwitchesInASavepointAndGivesTheContextBack, and green in this package -- the
// refusal, not the restore, is what keeps the runner in the key's business.
func TestOnRunner_RefusesAnotherBusinessesContext(t *testing.T) {
	r := newRig(t)
	a, b := uuid.New(), uuid.New()
	o := onRunner{data: r.d}
	setting := func(ctx context.Context, tx pgx.Tx) string {
		t.Helper()
		var got string
		if err := tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	err := r.d.WithTenant(r.ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		on := onTheRunner(ctx, tx, a)
		ran := false
		if err := o.WithTenant(on, b, func(context.Context, pgx.Tx) error { ran = true; return nil }); !errors.Is(err, errAnotherBusiness) {
			t.Errorf("WithTenant(another business): %v, want errAnotherBusiness", err)
		}
		if ran {
			t.Error("fn ran in another business's context")
		}
		if got := setting(ctx, tx); got != a.String() {
			t.Errorf("after the refusal the runner's context is %q, want %s", got, a)
		}
		var inside string
		if err := o.WithTenant(on, a, func(ctx context.Context, sp pgx.Tx) error { inside = setting(ctx, sp); return nil }); err != nil {
			t.Errorf("CONTROL: WithTenant(the key's business): %v", err)
		}
		if inside != a.String() {
			t.Errorf("CONTROL: fn ran in %q, want %s", inside, a)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTenant: %v", err)
	}
}
