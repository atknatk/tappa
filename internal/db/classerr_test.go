package db

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/config"
)

// poolStatementCalls is one call per function of this package that runs a statement on the customer
// pool (d.quiet()), keyed by the function's name. TestPoolStatements_GoThroughQuiet holds the keys to
// the source in both directions, so a new statement is measured by
// TestPoolStatements_AConnectFailureCarriesOnlyItsClass the day it is written.
var poolStatementCalls = map[string]func(context.Context, *DB) error{
	"WithTenant": func(ctx context.Context, d *DB) error {
		return d.WithTenant(ctx, uuid.New(), func(context.Context, pgx.Tx) error { return nil })
	},
	"GetTagByUID": func(ctx context.Context, d *DB) error {
		_, err := d.GetTagByUID(ctx, "04A1B2C3D4E5F6")
		return err
	},
	"GetEmployeeBySessionHash": func(ctx context.Context, d *DB) error {
		_, err := d.GetEmployeeBySessionHash(ctx, "h")
		return err
	},
	"GetInviteByCodeHash": func(ctx context.Context, d *DB) error {
		_, err := d.GetInviteByCodeHash(ctx, "h")
		return err
	},
	"GetAdminByEmail": func(ctx context.Context, d *DB) error {
		_, err := d.GetAdminByEmail(ctx, "a@b.example")
		return err
	},
	"GetAdminSessionByTokenHash": func(ctx context.Context, d *DB) error {
		_, err := d.GetAdminSessionByTokenHash(ctx, "h")
		return err
	},
	"GetPasswordResetByTokenHash": func(ctx context.Context, d *DB) error {
		_, err := d.GetPasswordResetByTokenHash(ctx, "h")
		return err
	},
	"GetAPIKeyByHash": func(ctx context.Context, d *DB) error {
		_, err := d.GetAPIKeyByHash(ctx, "h")
		return err
	},
	"ResolveIdentityByEmail": func(ctx context.Context, d *DB) error {
		_, err := d.ResolveIdentityByEmail(ctx, "a@b.example")
		return err
	},
	"OpenIdentitySession": func(ctx context.Context, d *DB) error {
		_, err := d.OpenIdentitySession(ctx, uuid.New(), uuid.Nil, "h", PasswordHash{})
		return err
	},
	"IdentityMemberships": func(ctx context.Context, d *DB) error {
		_, err := d.IdentityMemberships(ctx, "h")
		return err
	},
	"SwitchIdentitySession": func(ctx context.Context, d *DB) error {
		_, err := d.SwitchIdentitySession(ctx, "h", uuid.New(), "h2")
		return err
	},
	"RevokeIdentitySessions": func(ctx context.Context, d *DB) error {
		_, err := d.RevokeIdentitySessions(ctx, "h", true)
		return err
	},
	"LinkLegacySession": func(ctx context.Context, d *DB) error {
		_, err := d.LinkLegacySession(ctx, "h", "h2")
		return err
	},
	"RecordFailedIdentityLogin": func(ctx context.Context, d *DB) error {
		_, err := d.RecordFailedIdentityLogin(ctx, uuid.New(), "e")
		return err
	},
	"IdentitySessionDecoy": func(ctx context.Context, d *DB) error {
		return d.IdentitySessionDecoy(ctx)
	},
	"IdentityPasswordForSession": func(ctx context.Context, d *DB) error {
		_, err := d.IdentityPasswordForSession(ctx, "h")
		return err
	},
	"SetIdentityPassword": func(ctx context.Context, d *DB) error {
		_, err := d.SetIdentityPassword(ctx, "h", "d")
		return err
	},
	"ConsumeIdentityRowReset": func(ctx context.Context, d *DB) error {
		_, err := d.ConsumeIdentityRowReset(ctx, "h", "d")
		return err
	},
	"OpenIdentityReset": func(ctx context.Context, d *DB) error {
		_, err := d.OpenIdentityReset(ctx, uuid.New(), "h", time.Now().Add(time.Hour))
		return err
	},
	"WithdrawIdentityReset": func(ctx context.Context, d *DB) error {
		_, err := d.WithdrawIdentityReset(ctx, "h")
		return err
	},
	"ConsumeIdentityReset": func(ctx context.Context, d *DB) error {
		_, err := d.ConsumeIdentityReset(ctx, "h", "d")
		return err
	},
	"GetIdentityResetByTokenHash": func(ctx context.Context, d *DB) error {
		_, err := d.GetIdentityResetByTokenHash(ctx, "h")
		return err
	},
	"OpenSignupClaim": func(ctx context.Context, d *DB) error {
		_, err := d.OpenSignupClaim(ctx, "h", "a@b.example", []byte(`{}`), "d", time.Now().Add(time.Hour))
		return err
	},
	"GetSignupClaimByTokenHash": func(ctx context.Context, d *DB) error {
		_, err := d.GetSignupClaimByTokenHash(ctx, "h")
		return err
	},
	"SweepSignupClaims": func(ctx context.Context, d *DB) error {
		_, err := d.SweepSignupClaims(ctx)
		return err
	},
	"GetAdminInviteByTokenHash": func(ctx context.Context, d *DB) error {
		_, err := d.GetAdminInviteByTokenHash(ctx, "h")
		return err
	},
	"AcceptAdminInvite": func(ctx context.Context, d *DB) error {
		_, err := d.AcceptAdminInvite(ctx, "h", "h2")
		return err
	},
	"AcceptAdminInviteNew": func(ctx context.Context, d *DB) error {
		_, err := d.AcceptAdminInviteNew(ctx, "h", "d")
		return err
	},
	"RequestIdentityVerification": func(ctx context.Context, d *DB) error {
		_, err := d.RequestIdentityVerification(ctx, "h", "h2")
		return err
	},
	"VerifyIdentityEmail": func(ctx context.Context, d *DB) error {
		_, err := d.VerifyIdentityEmail(ctx, "h", "h2")
		return err
	},
	"ConfirmWindow": func(ctx context.Context, d *DB) error {
		_, err := d.ConfirmWindow(ctx, "h")
		return err
	},
	"KeepAll": func(ctx context.Context, d *DB) error {
		return d.KeepAll(ctx, "h")
	},
	"UnlinkMembership": func(ctx context.Context, d *DB) error {
		_, err := d.UnlinkMembership(ctx, "h", uuid.New())
		return err
	},
	"RelinkMembership": func(ctx context.Context, d *DB) error {
		_, err := d.RelinkMembership(ctx, "h", uuid.New())
		return err
	},
	"RequestIdentityLink": func(ctx context.Context, d *DB) error {
		_, err := d.RequestIdentityLink(ctx, "h", "h2")
		return err
	},
	"LinkByEmail": func(ctx context.Context, d *DB) error {
		_, err := d.LinkByEmail(ctx, "h", "h2")
		return err
	},
}

// rawPoolReaders are the functions allowed to read d.pool itself, each with its reason.
var rawPoolReaders = map[string]string{
	"Ping":                "GET /readyz's classifier reads the driver's error itself (TestReadyz_TheProbeHandsTheClassifierTheDriversError)",
	"Close":               "returns no error",
	"quiet":               "the boundary",
	"readRole":            "an opening step: poolStepErr reduces it to a class (T84, TestOpeningThePool_WrapsNoDriverError)",
	"requireFeedTimeouts": "an opening step: poolStepErr reduces it to a class (T84, TestOpeningThePool_WrapsNoDriverError)",
}

// stoppableProxy forwards TCP to target until stop, which closes the listener and every connection it
// carried: a pool opened through it then fails every new connection the way a database that went away
// after the boot does.
type stoppableProxy struct {
	ln    net.Listener
	mu    sync.Mutex
	conns []net.Conn
}

func startStoppableProxy(t *testing.T, target string) *stoppableProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &stoppableProxy{ln: ln}
	t.Cleanup(p.stop)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s, err := net.Dial("tcp", target)
			if err != nil {
				_ = c.Close()
				continue
			}
			p.mu.Lock()
			p.conns = append(p.conns, c, s)
			p.mu.Unlock()
			go func() { _, _ = io.Copy(s, c); _ = s.Close() }()
			go func() { _, _ = io.Copy(c, s); _ = c.Close() }()
		}
	}()
	return p
}

func (p *stoppableProxy) stop() {
	_ = p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
}

// TestPoolStatements_AConnectFailureCarriesOnlyItsClass (backlog T208) is the row's measurement made
// permanent: a pool opened through a proxy that then stops, and every statement of the pool after it.
// Each error names its step and the class; the role, the database and the address are in no rendering
// of it; the chain still holds pgx's ConnectError (so the call reached the pool -- the row's control --
// and Transient and errors.As read what they read before).
//
// BEFORE (measured, 2026-11-08, this test's setup on the unchanged tree): `db: begin tx: failed to
// connect to user=tappa_app database=tappa: 127.0.0.1:56588 (127.0.0.1): dial error: dial tcp
// 127.0.0.1:56588: connect: connection refused`, and the same for GetTagByUID, IdentityMemberships
// and ResolveIdentityByEmail. AFTER: `db: begin tx: cannot connect (connection refused)`.
//
// MUTANTS (measured, 2026-11-08, reverted): quietErr returning err as it is -- red on every row;
// classOnlyError.Unwrap returning nil -- red on every row (the ConnectError is gone from the chain).
func TestPoolStatements_AConnectFailureCarriesOnlyItsClass(t *testing.T) {
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required -- CLAUDE.md §8)")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		t.Fatalf("DATABASE_URL (%d chars) is not a URL with a user", len(raw))
	}
	p := startStoppableProxy(t, u.Host)
	u.Host = p.ln.Addr().String()
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d, err := New(ctx, &config.Config{DatabaseURL: config.NewDSN(u.String())})
	if err != nil {
		t.Fatalf("New through the proxy: %v", err)
	}
	defer d.Close()
	p.stop()
	d.pool.Reset()

	canaries := []string{"user=", "database=", u.User.Username(), strings.TrimPrefix(u.Path, "/") + "`", port, "dial"}
	conn, rawErr := pgx.Connect(ctx, u.String())
	if rawErr == nil {
		_ = conn.Close(ctx)
		t.Fatal("CONTROL: the stopped proxy still connects; the rows measure nothing")
	}
	for _, c := range canaries {
		if !strings.Contains(rawErr.Error(), c) {
			t.Errorf("CONTROL: pgx's own error no longer carries %q; the search below looks at nothing", c)
		}
	}

	names := make([]string, 0, len(poolStatementCalls))
	for name := range poolStatementCalls {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			err := poolStatementCalls[name](ctx, d)
			if err == nil {
				t.Fatal("the call succeeded through a stopped proxy")
			}
			var connect *pgconn.ConnectError
			if !errors.As(err, &connect) {
				t.Fatalf("no ConnectError on the chain: the call did not reach the pool, or the chain was cut: %v", err)
			}
			if !Transient(err) {
				t.Errorf("Transient no longer sees a refused connection: %v", err)
			}
			if !strings.HasSuffix(err.Error(), ": cannot connect (connection refused)") {
				t.Errorf("error is %q, want the step and \"cannot connect (connection refused)\"", err)
			}
			for i, text := range renderEverywhere(err) {
				for _, c := range canaries {
					if strings.Contains(text, c) {
						t.Fatalf("rendering #%d carries %q: %s", i, c, text)
					}
				}
			}
		})
	}

	expired, stop := context.WithTimeout(ctx, time.Nanosecond)
	defer stop()
	<-expired.Done()
	if err := poolStatementCalls["WithTenant"](expired, d); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("an expired context must stay on the chain for errors.Is: %v", err)
	}
}

// TestPoolStatements_GoThroughQuiet holds the boundary in the source: outside rawPoolReaders no function
// of this package reads a DB's pool field (the operator pool's methods read their own, which go through
// operatorErr), and the functions that call quiet() are exactly poolStatementCalls's keys.
//
// MUTANTS (measured, 2026-11-08, reverted): GetTagByUID back on d.pool.QueryRow -- red (a raw read
// outside the list, and GetTagByUID missing from the callers); the "KeepAll" row deleted from
// poolStatementCalls -- red.
func TestPoolStatements_GoThroughQuiet(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	callers := map[string]bool{}
	rawSeen := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			recv := ""
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
					if id, ok := star.X.(*ast.Ident); ok {
						recv = id.Name
					}
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "pool":
					if recv == "OperatorDB" {
						return true
					}
					if _, ok := rawPoolReaders[fn.Name.Name]; ok && recv == "DB" {
						rawSeen[fn.Name.Name] = true
						return true
					}
					t.Errorf("%s: %s reads the pool field outside quiet(): a connection failure there renders pgx's "+
						"text, the role, the database and the address (backlog T208)", fset.Position(sel.Pos()), fn.Name.Name)
				case "quiet":
					callers[fn.Name.Name] = true
				}
				return true
			})
		}
	}
	for name := range rawPoolReaders {
		if !rawSeen[name] {
			t.Errorf("CONTROL: %s is allowed to read the pool but the scan saw no such read; drop it from the list", name)
		}
	}
	for name := range callers {
		if _, ok := poolStatementCalls[name]; !ok {
			t.Errorf("%s calls quiet() and has no row in poolStatementCalls: its connection failure is not measured", name)
		}
	}
	for name := range poolStatementCalls {
		if !callers[name] {
			t.Errorf("poolStatementCalls has a row for %s, which does not call quiet()", name)
		}
	}
}

// TestQuietErr_KeepsTheChainAndDropsTheText drives quietErr with pgx's own failures (a closed port, and
// with a database, a refused credential whose PgError the callers read by SQLSTATE) and with the errors
// that must pass through as the same value.
//
// MUTANT (measured, 2026-11-08, reverted): the *net.OpError case removed -- red on "an address on a
// network error".
func TestQuietErr_KeepsTheChainAndDropsTheText(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	closed := closedAddr(t)
	type row struct {
		name, dsn, want string
		code            string
	}
	rows := []row{{"nothing listens", "postgres://" + t84User + ":" + t84Password + "@" + closed + "/" + t84Database + "?sslmode=disable", "cannot connect (connection refused)", ""}}
	if app := os.Getenv("DATABASE_URL"); app != "" {
		u, err := url.Parse(app)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row{"the server refuses the credential", "postgres://" + t84User + ":" + t84Password + "@" + u.Host + u.Path + "?sslmode=disable",
			"cannot connect (SQLSTATE 28P01)", "28P01"})
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			conn, raw := pgx.Connect(ctx, r.dsn)
			if raw == nil {
				_ = conn.Close(ctx)
				t.Fatal("CONTROL: this DSN connects")
			}
			if !strings.Contains(raw.Error(), t84User) {
				t.Fatalf("CONTROL: pgx's text no longer carries the user: %v", raw)
			}
			got := quietErr(raw)
			if got.Error() != r.want {
				t.Errorf("quietErr = %q, want %q", got, r.want)
			}
			var connect *pgconn.ConnectError
			if !errors.As(got, &connect) || Transient(got) != Transient(raw) {
				t.Errorf("the chain changed: ConnectError found %v, Transient %v (pgx's: %v)", errors.As(got, &connect), Transient(got), Transient(raw))
			}
			var pg *pgconn.PgError
			if r.code != "" && (!errors.As(got, &pg) || pg.Code != r.code) {
				t.Errorf("the server's SQLSTATE is not on the chain any more: %v", got)
			}
			for i, text := range renderEverywhere(got) {
				for _, c := range []string{t84User, t84Password, t84Database, closed} {
					if strings.Contains(text, c) {
						t.Fatalf("rendering #%d carries %q: %s", i, c, text)
					}
				}
			}
		})
	}

	addr := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 7), Port: 6543}
	op := &net.OpError{Op: "read", Net: "tcp", Source: addr, Addr: addr, Err: syscall.ECONNRESET}
	if got := quietErr(op); got.Error() != "the connection failed (connection reset by peer)" || !errors.Is(got, syscall.ECONNRESET) {
		t.Errorf("an address on a network error: quietErr = %q", got)
	}

	for _, same := range []error{
		&pgconn.PgError{Code: "23505", Message: "duplicate key"}, pgx.ErrNoRows, context.Canceled,
		context.DeadlineExceeded, io.ErrUnexpectedEOF, errors.New("conn closed"),
	} {
		if got := quietErr(same); got != same {
			t.Errorf("quietErr(%v) = %v, want the same value", same, got)
		}
	}
	if quietErr(nil) != nil {
		t.Error("quietErr(nil) is not nil")
	}
}

// TestOperatorErr_AConnectFailureCarriesOnlyItsClass: the operator pool's statements reach the same text
// through operatorErr's non-PostgreSQL branch, which wrapped pgx's error as it was.
//
// MUTANT (measured, 2026-11-08, reverted): `fmt.Errorf("db: %s: %w", what, err)` back -- red.
func TestOperatorErr_AConnectFailureCarriesOnlyItsClass(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	closed := closedAddr(t)
	conn, raw := pgx.Connect(ctx, "postgres://"+t84User+"@"+closed+"/"+t84Database+"?sslmode=disable")
	if raw == nil {
		_ = conn.Close(ctx)
		t.Fatal("CONTROL: a closed port connects")
	}
	got := operatorErr("operator by email", raw)
	if got.Error() != "db: operator by email: cannot connect (connection refused)" {
		t.Errorf("operatorErr = %q", got)
	}
	var connect *pgconn.ConnectError
	if !errors.As(got, &connect) {
		t.Error("the ConnectError is not on the chain any more")
	}
	for i, text := range renderEverywhere(got) {
		for _, c := range []string{t84User, t84Database, closed} {
			if strings.Contains(text, c) {
				t.Fatalf("rendering #%d carries %q: %s", i, c, text)
			}
		}
	}
}
