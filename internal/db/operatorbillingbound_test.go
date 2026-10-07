package db

// operatorbillingbound_test.go -- M10 OP-12 phase B: the billing read's own time bound
// (operator.go: tenantBillingStatementTimeoutSQL, TenantBillingReadTimeout, operatorTxConn).
// What a read past the bound does on the operator's screen -- a 503 with its ticket left
// unconsumed -- is measured end to end in internal/handler/operator's op12_db_test.go
// (TestE2E_ASlowBillingReadIsA503AndLeavesItsTicketUnconsumed).

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestTenantBilling_TheStatementSpellsTheReadTimeout: the statement is the Go bound in
// milliseconds, written out (SET takes no parameter), and SET LOCAL -- never a session SET,
// which would outlive the read on a pooled connection (CLAUDE.md §6's rule for app.tenant_id,
// the same reason).
func TestTenantBilling_TheStatementSpellsTheReadTimeout(t *testing.T) {
	want := "SET LOCAL statement_timeout = " + strconv.FormatInt(TenantBillingReadTimeout.Milliseconds(), 10)
	if tenantBillingStatementTimeoutSQL != want {
		t.Errorf("the bound's statement is %q, want %q", tenantBillingStatementTimeoutSQL, want)
	}
	if TenantBillingReadTimeout <= 0 || TenantBillingReadTimeout%time.Millisecond != 0 {
		t.Errorf("TenantBillingReadTimeout is %v; want a positive whole number of milliseconds", TenantBillingReadTimeout)
	}
}

// opBeginConn is opNoCallConn (operatortenants_test.go) that also answers Begin -- refusing
// it -- so it is an operatorTxConn.
type opBeginConn struct{ opNoCallConn }

func (c *opBeginConn) Begin(context.Context) (pgx.Tx, error) {
	c.calls++
	return nil, errNoCall
}

// TestTenantBilling_AConnectionThatCannotOpenATransactionIsRefusedBeforePhaseOne: a
// connection with no Begin cannot carry phase two's SET LOCAL, so TenantBilling refuses it
// with errBillingNeedsATransaction and sends no statement -- phase one's 'read' row is never
// written for a read that could not be bounded. CONTROL: the same fake with a Begin is sent
// phase one (one call, the fake's error).
func TestTenantBilling_AConnectionThatCannotOpenATransactionIsRefusedBeforePhaseOne(t *testing.T) {
	ctx := context.Background()
	c := &opNoCallConn{}
	if _, err := TenantBilling(ctx, c, "x", uuid.New(), 1); !errors.Is(err, errBillingNeedsATransaction) || c.calls != 0 {
		t.Errorf("a connection with no Begin: %v after %d statement(s); want errBillingNeedsATransaction and none", err, c.calls)
	}
	b := &opBeginConn{}
	if _, err := TenantBilling(ctx, b, "x", uuid.New(), 1); err == nil || errors.Is(err, errBillingNeedsATransaction) || b.calls != 1 {
		t.Fatalf("CONTROL: a connection with a Begin: %v after %d call(s); want phase one sent once", err, b.calls)
	}
}

// boundProbe is a tappa_operator connection whose transactions report the statement_timeout
// in force when their read is sent (seen), read on the transaction itself before the read.
type boundProbe struct {
	*pgx.Conn
	seen []string
}

func (p *boundProbe) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.Conn.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &boundProbeTx{Tx: tx, p: p}, nil
}

type boundProbeTx struct {
	pgx.Tx
	p *boundProbe
}

func (x *boundProbeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	var v string
	if err := x.Tx.QueryRow(ctx, `SHOW statement_timeout`).Scan(&v); err != nil {
		return nil, err
	}
	x.p.seen = append(x.p.seen, v)
	return x.Tx.Query(ctx, sql, args...)
}

// TestTenantBilling_ThePhaseTwoTransactionCarriesItsOwnTimeBound, on PostgreSQL, through the
// shipped accessor on one connection that IS tappa_operator (SET SESSION AUTHORIZATION, the
// pool tests' hook):
//
//   - the connection's statement_timeout before the call is the session's (the operator's
//     pool sets none: 0 on the development server);
//   - phase two's read runs in a transaction where it is 15s -- read on that transaction,
//     just before the read is sent;
//   - after the call the connection's statement_timeout is the session's again: the bound was
//     LOCAL and ended with the read's transaction (the next user of a pooled connection does
//     not inherit it);
//   - the read's 'read' row is committed and its ticket consumed -- the transaction committed
//     (an unknown id, and -- when the database holds a committed tenant without employees, a
//     cheap read -- a known one).
//
// It commits one 'read' row per read (two when a known tenant is found); the fixture's
// cleanup deletes the tickets and revokes the session.
func TestTenantBilling_ThePhaseTwoTransactionCarriesItsOwnTimeBound(t *testing.T) {
	ctx, f := opLiveFixture(t)
	conn := f.connect(t, ctx)
	if err := asOperator(ctx, conn); err != nil {
		t.Fatalf("become tappa_operator: %v", err)
	}
	show := func() string {
		t.Helper()
		var v string
		if err := conn.QueryRow(ctx, `SHOW statement_timeout`).Scan(&v); err != nil {
			t.Fatalf("SHOW statement_timeout: %v", err)
		}
		return v
	}
	before := show()
	if before == "15s" {
		t.Fatalf("PREMISE: the session's statement_timeout is already 15s; the measure would be vacuous")
	}
	p := &boundProbe{Conn: conn}
	reads := f.liveReads(t, ctx, f.session)
	if _, err := TenantBilling(ctx, p, f.hash, uuid.New(), 1); !errors.Is(err, ErrNoSuchTenant) {
		t.Fatalf("TenantBilling of an unknown id: %v, want ErrNoSuchTenant", err)
	}
	want := []string{"15s"}
	if tenant, _, known := opCommittedTenantWithoutStaff(t, ctx, f.owner); known {
		tl, err := TenantBilling(ctx, p, f.hash, tenant, 1)
		if err != nil || len(tl.Months) != TenantBillingMonthsPerPage {
			t.Fatalf("TenantBilling of a committed tenant: %d month(s), %v", len(tl.Months), err)
		}
		want = append(want, "15s")
	}
	if len(p.seen) != len(want) || p.seen[0] != "15s" || p.seen[len(p.seen)-1] != "15s" {
		t.Errorf("statement_timeout inside phase two's transactions: %v, want %v", p.seen, want)
	}
	if after := show(); after != before {
		t.Errorf("statement_timeout on the connection after the read is %q, before it %q: the bound outlived its transaction", after, before)
	}
	if n := f.liveReads(t, ctx, f.session) - reads; n != int64(len(want)) {
		t.Errorf("the read(s) committed %d 'read' row(s), want %d", n, len(want))
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NULL`, f.session); n != 0 {
		t.Errorf("%d ticket(s) of the session left unconsumed, want 0: phase two's transaction did not commit", n)
	}
}
