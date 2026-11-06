package db

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/config"
)

// The canaries of TestNew_AConnectFailureCarriesOnlyItsClass: every part of a DSN that pgx or a server
// could repeat. The password's is the one that has never been measured in pgx's text; it is here so
// that the day it is, this test says so.
const (
	t84User     = "canaryuser"
	t84Password = "canarypassword"
	t84Database = "canarydb"
	t84Host     = "canaryhost.invalid"
	t84App      = "canaryapp"
	t84Setting  = "canarysetting"
	t84Value    = "canaryvalue"
)

// TestNew_AConnectFailureCarriesOnlyItsClass (backlog T84): New's error names the step and the class,
// the exact text an operator greps for, with no part of the DSN in any rendering.
//
// CONTROL, per row with a real connection: pgx's own error for the same DSN does carry the row's
// canaries -- so the search is looking at the right bytes, and the day pgx stops saying them this test
// stops claiming to prove anything.
//
// MUTANT (measured, 2026-11-06, reverted): `return nil, fmt.Errorf("db: ping: %w", err)` back in newDB
// -- red here on every row.
func TestNew_AConnectFailureCarriesOnlyItsClass(t *testing.T) {
	type row struct {
		name, dsn, want string
		// inPgx are the canaries pgx's own text carries for this DSN.
		inPgx []string
	}
	userinfo := url.UserPassword(t84User, t84Password).String()
	params := "?sslmode=disable&application_name=" + t84App
	closed := closedAddr(t)
	rows := []row{
		{
			"nothing listens", "postgres://" + userinfo + "@" + closed + "/" + t84Database + params,
			"db: ping: connection refused", []string{t84User, t84Database},
		},
		{
			"the host does not resolve", "postgres://" + userinfo + "@" + t84Host + ":5432/" + t84Database + params,
			"db: ping: the database host name does not resolve", []string{t84User, t84Database, t84Host},
		},
	}
	if app := os.Getenv("DATABASE_URL"); app != "" {
		u, err := url.Parse(app)
		if err != nil || u.User == nil {
			t.Fatalf("DATABASE_URL (%d chars) is not a URL with a user", len(app))
		}
		rows = append(rows,
			row{
				"the server refuses the credential", "postgres://" + userinfo + "@" + u.Host + "/" + strings.TrimPrefix(u.Path, "/") + params,
				"db: ping: SQLSTATE 28P01", []string{t84User},
			},
			row{
				"the database does not exist", "postgres://" + u.User.String() + "@" + u.Host + "/" + t84Database + params,
				"db: ping: SQLSTATE 3D000", []string{t84Database},
			},
			row{
				"the server refuses a setting's value", "postgres://" + u.User.String() + "@" + u.Host + u.Path + "?sslmode=disable&datestyle=" + t84Value,
				"db: ping: SQLSTATE 22023", []string{t84Value},
			},
			row{
				"the server does not know a setting", "postgres://" + u.User.String() + "@" + u.Host + u.Path + "?sslmode=disable&" + t84Setting + "=" + t84Value,
				"db: ping: SQLSTATE 42704", []string{t84Setting},
			},
		)
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			conn, raw := pgx.Connect(ctx, r.dsn)
			if raw == nil {
				_ = conn.Close(ctx)
				t.Fatal("CONTROL: this DSN connects; the row measures nothing")
			}
			for _, c := range r.inPgx {
				if !strings.Contains(raw.Error(), c) {
					t.Errorf("CONTROL: pgx's own error no longer carries %q; this row looks at nothing", c)
				}
			}
			d, err := New(ctx, &config.Config{DatabaseURL: config.NewDSN(r.dsn)})
			if err == nil {
				d.Close()
				t.Fatal("New opened a pool on this DSN")
			}
			if err.Error() != r.want {
				t.Errorf("New's error is %q, want %q", err, r.want)
			}
			for i, text := range renderEverywhere(err) {
				for _, c := range []string{t84User, t84Password, t84Database, t84Host, t84App, t84Setting, t84Value} {
					if strings.Contains(text, c) {
						t.Fatalf("rendering #%d of New's error carries %q", i, c)
					}
				}
			}
		})
	}
}

// TestPoolStepErr_KeepsTheClassAndDropsTheText drives the classifier with the errors no live database
// hands out on demand: a server sentence full of canaries in every text field of a PgError, a joined
// sslmode=prefer pair, the read-back's wrap (logparams.go, whose text reaches no log only because of this
// function), and the three refusals that pass through.
//
// MUTANT (measured, 2026-11-06, reverted): the last line of poolStepErr as %w -- red on every text row.
func TestPoolStepErr_KeepsTheClassAndDropsTheText(t *testing.T) {
	const canary = "canarytext"
	server := &pgconn.PgError{
		Severity: "FATAL", Code: "28000", Message: canary, Detail: canary, Hint: canary, Where: canary,
		SchemaName: canary, TableName: canary, ColumnName: canary, ConstraintName: canary, File: canary, Routine: canary,
	}
	dial := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("%s: %w", canary, os.ErrDeadlineExceeded)}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"a server's sentence", fmt.Errorf("%s: %w", canary, server), "db: ping: SQLSTATE 28000"},
		{"two attempts", errors.Join(fmt.Errorf("%s: %w", canary, server), fmt.Errorf("%s: %w", canary, server)), "db: ping: attempt 1: SQLSTATE 28000; attempt 2: SQLSTATE 28000"},
		{"the read-back's wrap", fmt.Errorf("db: reading %s on a new connection: %w", canary, dial), "db: ping: network error"},
		{"an unknown failure", errors.New(canary), "db: ping: an error of type *errors.errorString"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := poolStepErr("ping", tc.err)
			if got.Error() != tc.want {
				t.Errorf("poolStepErr = %q, want %q", got, tc.want)
			}
			for i, text := range renderEverywhere(got) {
				if strings.Contains(text, canary) {
					t.Fatalf("rendering #%d carries the wrapped error's text", i)
				}
			}
		})
	}

	got := poolStepErr("ping", fmt.Errorf("%s: %w", canary, context.DeadlineExceeded))
	if !errors.Is(got, context.DeadlineExceeded) || strings.Contains(got.Error(), canary) {
		t.Errorf("a timeout must stay on the chain with no text: %v", got)
	}

	var notPinned *logParameterNotPinnedError
	if got := poolStepErr("ping", fmt.Errorf("%s: %w", canary, &logParameterNotPinnedError{Got: "-1"})); !errors.As(got, &notPinned) || strings.Contains(got.Error(), canary) {
		t.Errorf("the not-pinned refusal must pass through without the wrap's text: %v", got)
	}
	var read *logParameterReadError
	if got := poolStepErr("ping", fmt.Errorf("%s: %w", canary, &logParameterReadError{Code: "25P02"})); !errors.As(got, &read) || strings.Contains(got.Error(), canary) {
		t.Errorf("the read-back's refusal must pass through without the wrap's text: %v", got)
	}
	var mode *execModeRefusedError
	if got := poolStepErr("ping", fmt.Errorf("%s: %w", canary, &execModeRefusedError{})); !errors.As(got, &mode) || strings.Contains(got.Error(), canary) {
		t.Errorf("the exec-mode refusal must pass through without the wrap's text: %v", got)
	}
}

// TestOpeningThePool_WrapsNoDriverError is the steps no live database fails on demand (the role's three
// statements, the stuck-writer read after the ping): in the functions that run while the customer pool
// opens, every fmt.Errorf is either free of %w or wraps a context outcome. It reads the source, because a
// server's text there is exactly what no test reaches. The list is the functions newDB calls; a new step
// with its own wrap has to be added here to be seen, which is what TestConstructors_BodiesAreTheReviewedOnes pins for newDB.
//
// MUTANT (measured, 2026-11-06, reverted): `fmt.Errorf("db: read connection role: %w", err)` back in
// readRole, and the same in requireFeedTimeouts -- red here on each.
func TestOpeningThePool_WrapsNoDriverError(t *testing.T) {
	steps := map[string]bool{"newDB": true, "readRole": true, "requireFeedTimeouts": true}
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, file := range []string{"pool.go", "feedtimeouts.go"} {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !steps[fn.Name.Name] {
				continue
			}
			seen[fn.Name.Name] = true
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Errorf" {
					return true
				}
				format, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					return true
				}
				text, err := strconv.Unquote(format.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(text, "%w") {
					t.Errorf("%s: %s wraps an error with %%w: pgx's and the server's text would reach the boot log "+
						"(backlog T84); use poolStepErr", fset.Position(call.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
	for name := range steps {
		if !seen[name] {
			t.Errorf("%s is not in pool.go or feedtimeouts.go any more; this test reads nothing for it", name)
		}
	}
}
