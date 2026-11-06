package db

// operatortimeout_test.go -- T98: the operator pool's statement bound (operatortimeout.go), what a
// statement cut off by it leaves in the trail, and that the caller's context reaches the database.
// They commit what opLiveFixture's class commits: 'read' rows naming random tenant ids, and
// TestOperatorWrites_ACutOffWriteLeavesNeitherTheChangeNorItsRow one tenant.

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestOperatorStatementTimeout_IsTheBillingReadsBound: one number, spelled as the server reports
// it. The billing read keeps its own SET LOCAL because a raw connection has no pool's parameter;
// the two are the same arithmetic (operator.go).
func TestOperatorStatementTimeout_IsTheBillingReadsBound(t *testing.T) {
	if OperatorStatementTimeout != TenantBillingReadTimeout {
		t.Errorf("OperatorStatementTimeout = %v, TenantBillingReadTimeout = %v", OperatorStatementTimeout, TenantBillingReadTimeout)
	}
	if operatorStatementTimeoutSetting != "15s" {
		t.Errorf("the setting is spelled %q", operatorStatementTimeoutSetting)
	}
}

// TestOperatorPool_EveryConnectionCarriesTheStatementTimeout: on a pool the production opener built,
// three connections read the bound back, and a DSN that names the key -- in lower or upper case --
// does not change it. CONTROL: on a raw connection the upper-case key does reach the server, so the
// upper-case arm is not passing on a parameter nobody sent.
func TestOperatorPool_EveryConnectionCarriesTheStatementTimeout(t *testing.T) {
	ctx, f := opLiveFixture(t)
	show := func(q interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}) string {
		t.Helper()
		var v string
		if err := q.QueryRow(ctx, `SHOW statement_timeout`).Scan(&v); err != nil {
			t.Fatalf("SHOW statement_timeout: %v", err)
		}
		return v
	}
	raw, err := pgx.Connect(ctx, withParam(f.dsn, "STATEMENT_TIMEOUT", "1234"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close(context.Background()) }()
	if got := show(raw); got != "1234ms" {
		t.Fatalf("CONTROL: an upper-case key on a raw connection reads %q, want it to have reached the server", got)
	}
	for name, dsn := range map[string]string{
		"the DSN as it is":     f.dsn,
		"the DSN says 0":       withParam(f.dsn, "statement_timeout", "0"),
		"the DSN says 0, CAPS": withParam(f.dsn, "STATEMENT_TIMEOUT", "0"),
	} {
		o, err := openOperatorDB(ctx, dsn, asOperator)
		if err != nil {
			t.Fatalf("%s: open: %v", name, err)
		}
		var held []interface{ Release() }
		for i := 0; i < 3; i++ {
			c, err := o.pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, c)
			if got := show(c); got != "15s" {
				t.Errorf("%s: connection %d reads statement_timeout %q, want 15s", name, i, got)
			}
		}
		for _, c := range held {
			c.Release()
		}
		o.Close()
	}
}

// TestOperatorPool_ATimeoutThatDidNotLandIsRefused: a connection on which the startup parameter did
// not take effect -- the hook's SET after startup, the shape of a pooler that drops it -- is refused
// with the sentence's two values and no connection string. CONTROL: the same hook without the SET opens.
func TestOperatorPool_ATimeoutThatDidNotLandIsRefused(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, func(ctx context.Context, c *pgx.Conn) error {
		if err := asOperator(ctx, c); err != nil {
			return err
		}
		_, err := c.Exec(ctx, `SET statement_timeout = 0`)
		return err
	})
	if err == nil {
		o.Close()
		t.Fatal("a pool whose bound did not land was opened")
	}
	if !strings.Contains(err.Error(), `statement_timeout is "0"`) || !strings.Contains(err.Error(), `"15s"`) {
		t.Errorf("the refusal says %q", err)
	}
	if strings.Contains(err.Error(), "postgres://") || errors.Is(err, ErrOperatorUnreachable) {
		t.Errorf("the refusal carries the connection string or calls this unreachable: %q", err)
	}
	ok, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("CONTROL: the same open without the SET: %v", err)
	}
	ok.Close()
}

// TestOperatorPool_AHeldReadIsCutOffAtTheBound: the production number on a connection of the
// production pool, once (fifteen seconds; the short-bound tests below cover the kinds): a read whose
// second phase is held answers SQLSTATE 57014 at the bound and not before, well inside the request's
// deadline, with its 'read' row committed. The connection is held for the read so the hold can go
// between its two phases; the pool's own statements are on it just the same.
func TestOperatorPool_AHeldReadIsCutOffAtTheBound(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	c, err := o.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()
	hold, release := holdOpenTicket(t, ctx, f)
	defer release()
	reads := f.liveReads(t, ctx, f.session)
	start := time.Now()
	_, got := TenantList(ctx, &pausedConn{Conn: c.Conn(), beforeRead: hold}, f.hash, TenantListQuery{Number: 1, Size: 50})
	took := time.Since(start)
	release()
	if got == nil || !strings.Contains(got.Error(), "SQLSTATE 57014") {
		t.Fatalf("a held read = %v, want SQLSTATE 57014", got)
	}
	if took < OperatorStatementTimeout || took > OperatorStatementTimeout+5*time.Second {
		t.Errorf("the held read answered after %v; want the bound (%v) and little more", took, OperatorStatementTimeout)
	}
	if n := f.liveReads(t, ctx, f.session) - reads; n != 1 {
		t.Errorf("%d 'read' row(s) more, want 1", n)
	}
	t.Logf("a held read answered SQLSTATE 57014 after %v", took.Round(time.Millisecond))
}

// pausedConn is a tappa_operator connection that runs beforeRead just before a read's second phase
// goes out, so a test can hold that phase's ticket from another session.
type pausedConn struct {
	*pgx.Conn
	beforeRead func()
}

func (p *pausedConn) pause(sql string) {
	if strings.Contains(sql, "op_read_") && p.beforeRead != nil {
		p.beforeRead()
		p.beforeRead = nil
	}
}

func (p *pausedConn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	p.pause(sql)
	return p.Conn.Query(ctx, sql, args...)
}

func (p *pausedConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	p.pause(sql)
	return p.Conn.QueryRow(ctx, sql, args...)
}

// operatorTimedReads are the reads the bound is for (OP-10, OP-11, OP-13, OP-14, OP-15's holdings,
// OP-16's VAT). The billing read has its own bound and its own E2E
// (TestE2E_ASlowBillingReadIsA503AndLeavesItsTicketUnconsumed).
func operatorTimedReads(hash string) map[string]func(context.Context, OperatorConn) error {
	id := uuid.New()
	return map[string]func(context.Context, OperatorConn) error{
		"legal_versions": func(ctx context.Context, c OperatorConn) error {
			_, err := LegalVersions(ctx, c, hash, LegalVersionsPage{Number: 1, Size: 50})
			return err
		},
		"tenants": func(ctx context.Context, c OperatorConn) error {
			_, err := TenantList(ctx, c, hash, TenantListQuery{Number: 1, Size: 50})
			return err
		},
		"tenant_detail": func(ctx context.Context, c OperatorConn) error {
			_, err := TenantDetail(ctx, c, hash, id)
			return err
		},
		"tenant_plaques": func(ctx context.Context, c OperatorConn) error {
			_, err := TenantPlaques(ctx, c, hash, id)
			return err
		},
		"audit": func(ctx context.Context, c OperatorConn) error {
			_, err := OperatorAudit(ctx, c, hash, OperatorAuditQuery{Number: 1, Size: 50})
			return err
		},
		"tenant_vat": func(ctx context.Context, c OperatorConn) error {
			_, err := TenantVAT(ctx, c, hash, id)
			return err
		},
		"holdings": func(ctx context.Context, c OperatorConn) error {
			_, err := Holdings(ctx, c, hash, 1)
			return err
		},
	}
}

// holdOpenTicket holds the session's unconsumed tickets from another session until the test
// releases it, so a read's second phase -- which consumes its ticket -- waits.
func holdOpenTicket(t *testing.T, ctx context.Context, f *opLive) (hold func(), release func()) {
	t.Helper()
	locker := f.connect(t, ctx)
	var mu sync.Mutex
	var tx pgx.Tx
	hold = func() {
		mu.Lock()
		defer mu.Unlock()
		var err error
		if tx, err = locker.Begin(ctx); err != nil {
			t.Fatalf("begin the hold: %v", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `WITH h AS (SELECT 1 FROM operator_read_tickets
		                             WHERE session_id = $1 AND consumed_at IS NULL FOR UPDATE) SELECT count(*)::int FROM h`, f.session).Scan(&n); err != nil {
			t.Fatalf("hold the ticket: %v", err)
		}
		if n == 0 {
			t.Fatal("PREMISE: no open ticket to hold; the first phase did not commit one")
		}
	}
	release = func() {
		mu.Lock()
		defer mu.Unlock()
		if tx != nil {
			if err := tx.Rollback(ctx); err != nil {
				t.Errorf("release the hold: %v", err)
			}
			tx = nil
		}
	}
	return hold, release
}

// TestOperatorReads_ACutOffReadLeavesTheTrailTrue (T98, CLAUDE.md section 4.5: an access leaves a
// trace), each read, as tappa_operator, on a connection with a short statement_timeout -- the
// production path but for the number (fifteen seconds per arm is what the billing E2E pays once):
//
//   - SECOND PHASE CUT OFF (its ticket held from another session): the call answers SQLSTATE 57014
//     and no data; the read's 'read' row stays, one more, because the first phase committed; its
//     ticket is not consumed. The access was attempted, the row says so, nothing left the database.
//   - the same connection serves the same read afterwards: one more row, no error.
//   - FIRST PHASE CUT OFF (the trail's table held in SHARE mode, which an INSERT waits on): 57014, no
//     row and no ticket -- it rolled back, so there is no ticket a second phase could consume.
func TestOperatorReads_ACutOffReadLeavesTheTrailTrue(t *testing.T) {
	ctx, f := opLiveFixture(t)
	const shortBound = `SET statement_timeout = 300`
	tickets := func() int64 {
		return opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NULL`, f.session)
	}
	cut := func(err error) bool {
		var pg *pgconn.PgError
		return strings.Contains(err.Error(), "SQLSTATE 57014") || (errors.As(err, &pg) && pg.Code == "57014")
	}
	for kind, read := range operatorTimedReads(f.hash) {
		t.Run(kind, func(t *testing.T) {
			conn := f.connect(t, ctx)
			if err := asOperator(ctx, conn); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, shortBound); err != nil {
				t.Fatal(err)
			}
			hold, release := holdOpenTicket(t, ctx, f)
			defer release()
			reads, open := f.liveReads(t, ctx, f.session), tickets()
			err := read(ctx, &pausedConn{Conn: conn, beforeRead: hold})
			release()
			if err == nil || !cut(err) {
				t.Fatalf("second phase held: %v, want SQLSTATE 57014", err)
			}
			if n := f.liveReads(t, ctx, f.session) - reads; n != 1 {
				t.Errorf("second phase cut off: %d 'read' row(s) more, want 1", n)
			}
			if n := tickets() - open; n != 1 {
				t.Errorf("second phase cut off: %d ticket(s) more left open, want 1", n)
			}
			reads, open = f.liveReads(t, ctx, f.session), tickets()
			if err := read(ctx, conn); err != nil && !errors.Is(err, ErrNoSuchTenant) {
				t.Errorf("the same read afterwards: %v", err)
			}
			if n := f.liveReads(t, ctx, f.session) - reads; n != 1 {
				t.Errorf("the read afterwards: %d 'read' row(s) more, want 1", n)
			}
			if n := tickets() - open; n != 0 {
				t.Errorf("the read afterwards: %d ticket(s) more left open, want 0", n)
			}

			locker := f.connect(t, ctx)
			tx, err := locker.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err := tx.Exec(ctx, `LOCK TABLE operator_audit_log IN SHARE MODE`); err != nil {
				t.Fatalf("hold the trail: %v", err)
			}
			reads, open = f.liveReads(t, ctx, f.session), tickets()
			first := read(ctx, conn)
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if first == nil || !cut(first) {
				t.Fatalf("first phase held: %v, want SQLSTATE 57014", first)
			}
			if n := f.liveReads(t, ctx, f.session) - reads; n != 0 {
				t.Errorf("first phase cut off: %d 'read' row(s) more, want none", n)
			}
			if n := tickets() - open; n != 0 {
				t.Errorf("first phase cut off: %d ticket(s) more, want none", n)
			}
		})
	}
}

// TestOperatorWrites_ACutOffWriteLeavesNeitherTheChangeNorItsRow: a write and its audit row are one
// statement, so a write past the bound (the tenant's row held from another session) is neither:
// 57014, the tenant not suspended and no 'tenant_suspended' row. CONTROL: released, the same call
// makes both.
func TestOperatorWrites_ACutOffWriteLeavesNeitherTheChangeNorItsRow(t *testing.T) {
	ctx, f := opLiveFixture(t)
	id := uuid.New()
	if _, err := f.owner.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
	                                VALUES ($1, 'FAKE T98 Ltd', $2, 'restaurant', 'single', 'Europe/Malta')`, id, "T98-"+id.String()); err != nil {
		t.Fatalf("commit a tenant: %v", err)
	}
	conn := f.connect(t, ctx)
	if err := asOperator(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SET statement_timeout = 300`); err != nil {
		t.Fatal(err)
	}
	facts := func() (suspended bool, rows int64) {
		t.Helper()
		if err := f.owner.QueryRow(ctx, `SELECT suspended_at IS NOT NULL FROM tenants WHERE id = $1`, id).Scan(&suspended); err != nil {
			t.Fatal(err)
		}
		return suspended, opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'tenant_suspended' AND target_tenant_id = $1`, id)
	}
	locker := f.connect(t, ctx)
	tx, err := locker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM tenants WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	got := SuspendTenant(ctx, conn, f.hash, id, SuspensionNonPayment)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if got == nil || !strings.Contains(got.Error(), "SQLSTATE 57014") {
		t.Fatalf("the write with the row held: %v, want SQLSTATE 57014", got)
	}
	if s, n := facts(); s || n != 0 {
		t.Errorf("cut off: suspended %v with %d row(s); want neither", s, n)
	}
	if err := SuspendTenant(ctx, conn, f.hash, id, SuspensionNonPayment); err != nil {
		t.Fatalf("CONTROL: the write released: %v", err)
	}
	if s, n := facts(); !s || n != 1 {
		t.Errorf("CONTROL: suspended %v with %d row(s); want both", s, n)
	}
}

// TestOperatorReads_TheCallersContextEndsABlockedRead: the context a read is called with reaches its
// statements. Second phase held, no statement_timeout on the session: the caller's deadline ends the
// wait and the call answers context.DeadlineExceeded -- not a wait until the lock is released, which
// a watchdog does after five seconds, so a read that dropped its context fails here rather than
// hanging. The 'read' row stays. Each arm has a connection of its own: pgx closes one whose statement
// was cancelled.
func TestOperatorReads_TheCallersContextEndsABlockedRead(t *testing.T) {
	ctx, f := opLiveFixture(t)
	for kind, read := range operatorTimedReads(f.hash) {
		t.Run(kind, func(t *testing.T) {
			conn := f.connect(t, ctx)
			if err := asOperator(ctx, conn); err != nil {
				t.Fatal(err)
			}
			hold, release := holdOpenTicket(t, ctx, f)
			defer release()
			reads := f.liveReads(t, ctx, f.session)
			callCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := read(callCtx, &pausedConn{Conn: conn, beforeRead: func() {
				hold()
				time.AfterFunc(5*time.Second, release)
			}})
			took := time.Since(start)
			release()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("the read = %v, want context.DeadlineExceeded", err)
			}
			if took >= 5*time.Second {
				t.Errorf("the read took %v against a 700ms deadline: it waited for the lock", took)
			}
			if n := f.liveReads(t, ctx, f.session) - reads; n != 1 {
				t.Errorf("%d 'read' row(s) more, want 1", n)
			}
		})
	}
}

// TestOperatorSQL_EveryStatementTakesTheCallersContext pins what the test above measures for seven
// reads, for every function of operator.go, from the syntax tree: each call of Exec, Query, QueryRow
// or Begin passes the function's own ctx first, and the file makes no context of its own. It sees
// those four method names only; a statement sent another way is code review's.
func TestOperatorSQL_EveryStatementTakesTheCallersContext(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "operator.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Name == "context" {
			t.Errorf("%s: operator.go makes a context of its own: context.%s", fset.Position(c.Pos()), sel.Sel.Name)
		}
		switch sel.Sel.Name {
		case "Exec", "Query", "QueryRow", "Begin":
			calls++
			if len(c.Args) == 0 {
				t.Errorf("%s: %s has no arguments", fset.Position(c.Pos()), sel.Sel.Name)
				return true
			}
			if id, ok := c.Args[0].(*ast.Ident); !ok || id.Name != "ctx" {
				t.Errorf("%s: %s does not pass the function's ctx first", fset.Position(c.Pos()), sel.Sel.Name)
			}
		}
		return true
	})
	if calls < 20 {
		t.Fatalf("only %d statements found; the scan has gone blind", calls)
	}
}
