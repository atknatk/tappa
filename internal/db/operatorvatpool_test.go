package db

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestOperatorDB_TheVATMethodsHoldNoConnectionOnceTheyReturn (M10 OP-16 B; the card's T2): the
// operator surface asks VIES between OperatorDB.TenantVAT and OperatorDB.RecordTenantVATCheck,
// and up to VIES's three seconds pass there -- so neither method may leave a connection
// acquired from the operator's pool (a connection held is a transaction that can be held).
//
// PART I -- on a pool built by the production opener (as tappa_operator), through the two
// METHODS the surface calls: after TenantVAT of an id no tenant has (both phases ran: the 'read'
// row committed, the second phase found nothing -- ErrNoSuchTenant) the pool's acquired count is
// 0; after RecordTenantVATCheck with a session no one holds (refused, ErrOperatorRefused, nothing
// written) it is 0. CONTROL: a connection acquired by hand is counted (1), and 0 once released.
//
// WHAT IT LEAVES (opLiveFixture's class, as TestTenantVAT_OnThePoolTheTwoPhasesAreTwoTransactions):
// one committed 'read' row naming a random tenant id no tenant has, the fixture's operator account
// (disabled at cleanup) and its session (revoked at cleanup). No tenant is read or written.
//
// PART II -- the two calls above. PART III -- a method that kept a connection on another path
// (a success with a real tenant) is not driven here; the handler's E2E measures the operator
// connection idle while VIES is asked -- no completeness claim.
func TestOperatorDB_TheVATMethodsHoldNoConnectionOnceTheyReturn(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("open the operator pool: %v", err)
	}
	defer o.Close()
	unknown := uuid.New()
	if _, err := o.TenantVAT(ctx, f.hash, unknown); !errors.Is(err, ErrNoSuchTenant) {
		t.Fatalf("TenantVAT of an unknown tenant: %v, want ErrNoSuchTenant", err)
	}
	if n := o.pool.Stat().AcquiredConns(); n != 0 {
		t.Errorf("after TenantVAT returned, the pool holds %d connection(s), want 0", n)
	}
	if err := o.RecordTenantVATCheck(ctx, opRandHex(t), unknown, "VAT-OP16-ZZ", true); !errors.Is(err, ErrOperatorRefused) {
		t.Fatalf("RecordTenantVATCheck with no session: %v, want ErrOperatorRefused", err)
	}
	if n := o.pool.Stat().AcquiredConns(); n != 0 {
		t.Errorf("after RecordTenantVATCheck returned, the pool holds %d connection(s), want 0", n)
	}
	c, err := o.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	held := o.pool.Stat().AcquiredConns()
	c.Release()
	if held != 1 || o.pool.Stat().AcquiredConns() != 0 {
		t.Fatalf("CONTROL: a connection acquired by hand counts %d, and %d once released; want 1 and 0", held, o.pool.Stat().AcquiredConns())
	}
}
