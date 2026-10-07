package db

// operator_test.go -- the accessors of operator.go (M10 OP-6). The op_* functions they
// call are proven in operatorfuncs_test.go / operatorschema_test.go; this file proves
// what the GO side adds: the error contract (a PgError never leaves), the statements'
// shape (bound parameters only, mirrored in db/queries/operator.sql), the belt beside
// the login lookup's RLS, and that the customer's role cannot use any of them.

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestOperatorErr_NeverCarriesAPgError is OP-7 (c) where the SQL lives: a PostgreSQL
// error leaves as its SQLSTATE only. A synthetic error carrying a DETAIL, a WHERE and a
// constraint name -- the fields 00026 measured leaking row values -- comes back with
// none of them in its text, and errors.As cannot reach a *pgconn.PgError through it;
// 28000 is ErrOperatorRefused; a cancelled context is still errors.Is-able.
func TestOperatorErr_NeverCarriesAPgError(t *testing.T) {
	leaky := &pgconn.PgError{
		Code: "23505", Message: "duplicate key value violates unique constraint",
		Detail: "Key (token_hash)=(FAKEdetailFAKEdetail) already exists.",
		Where:  "FAKEwhere", ConstraintName: "FAKEconstraint", ColumnName: "FAKEcolumn",
	}
	for _, in := range []error{leaky, fmt.Errorf("wrapped: %w", leaky)} {
		out := operatorErr("probe", in)
		var pg *pgconn.PgError
		if errors.As(out, &pg) {
			t.Errorf("a *pgconn.PgError is reachable through %q", out)
		}
		for _, v := range []string{"FAKEdetail", "FAKEwhere", "FAKEconstraint", "FAKEcolumn", "duplicate key"} {
			if strings.Contains(out.Error(), v) {
				t.Errorf("the returned error carries %q: %q", v, out.Error())
			}
		}
		if !strings.Contains(out.Error(), "23505") || !strings.Contains(out.Error(), "probe") {
			t.Errorf("the returned error lost its SQLSTATE or its call name: %q", out.Error())
		}
	}
	if got := operatorErr("probe", &pgconn.PgError{Code: "28000", Detail: "FAKE"}); got != ErrOperatorRefused {
		t.Errorf("28000 became %v, want ErrOperatorRefused", got)
	}
	if got := operatorErr("probe", context.Canceled); !errors.Is(got, context.Canceled) {
		t.Errorf("a cancelled context became %v; it must stay errors.Is-able", got)
	}
	if operatorErr("probe", nil) != nil {
		t.Error("nil became an error")
	}
}

// TestOperatorSQL_OnlyBoundParameters is OP-7 (b) where the SQL lives, read from
// operator.go's own syntax tree: every statement is a package CONSTANT; the only quoted
// literal in any of them is the schema constant 'active' (the two lookups); every
// Exec/QueryRow/Query on the connection takes one of those constants -- never a string built
// at run time -- and passes exactly as many arguments as the statement has $n
// placeholders. And each constant appears, whitespace aside, in db/queries/operator.sql,
// the canonical document (ADR 0021 §2 vi) -- so the mirror cannot drift silently.
func TestOperatorSQL_OnlyBoundParameters(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "operator.go", nil, 0)
	if err != nil {
		t.Fatalf("parse operator.go: %v", err)
	}
	consts := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if !strings.HasSuffix(n.Name, "SQL") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Errorf("%s is not a string literal constant", n.Name)
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", n.Name, err)
				}
				consts[n.Name] = v
			}
		}
	}
	// 7 until OP-10; 00027 added three (op_begin_read, op_read_legal_versions,
	// op_publish_legal), 00029 two (op_read_tenants, op_read_tenant_detail), 00030 one
	// (op_read_tenant_plaques), 00031 one (op_read_audit).
	if len(consts) != 14 {
		t.Fatalf("found %d statement constants in operator.go, want 14 (two lookups, twelve op_* calls)", len(consts))
	}
	quoted := regexp.MustCompile(`'[^']*'`)
	placeholder := regexp.MustCompile(`\$(\d+)`)
	maxParam := map[string]int{}
	for name, sql := range consts {
		for _, q := range quoted.FindAllString(sql, -1) {
			if q != "'active'" {
				t.Errorf("%s carries the quoted literal %s; values are bound parameters", name, q)
			}
		}
		for _, m := range placeholder.FindAllStringSubmatch(sql, -1) {
			n, _ := strconv.Atoi(m[1])
			if n > maxParam[name] {
				maxParam[name] = n
			}
		}
	}
	calls := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Query since OP-10 (op_read_* returns rows): a statement method left out of
		// this list is a call the scan would not read.
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Exec" && sel.Sel.Name != "QueryRow" && sel.Sel.Name != "Query") {
			return true
		}
		calls++
		if len(call.Args) < 2 {
			t.Errorf("%s: %s with no statement", fset.Position(call.Pos()), sel.Sel.Name)
			return true
		}
		id, ok := call.Args[1].(*ast.Ident)
		sql, known := "", false
		if ok {
			sql, known = consts[id.Name]
		}
		if !known {
			t.Errorf("%s: %s takes a statement that is not one of the constants", fset.Position(call.Pos()), sel.Sel.Name)
			return true
		}
		if args := len(call.Args) - 2; args != maxParam[id.Name] {
			t.Errorf("%s: %s passes %d argument(s) to %s, which has %d placeholder(s)", fset.Position(call.Pos()), sel.Sel.Name, args, id.Name, maxParam[id.Name])
		}
		_ = sql
		return true
	})
	if calls != 14 {
		t.Fatalf("%d Exec/QueryRow/Query call(s) in operator.go, want 14 -- one per statement", calls)
	}

	doc, err := os.ReadFile(filepath.Join("..", "..", "db", "queries", "operator.sql"))
	if err != nil {
		t.Fatalf("read operator.sql: %v", err)
	}
	var body strings.Builder
	for _, line := range strings.Split(string(doc), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-- name:") {
			t.Errorf("operator.sql declares a sqlc query (%q); it is a document only", trimmed)
		}
		if !strings.HasPrefix(trimmed, "--") && trimmed != "" {
			t.Errorf("operator.sql has a statement line outside a comment: %q", trimmed)
		}
		body.WriteString(strings.TrimPrefix(trimmed, "--") + " ")
	}
	norm := func(s string) string {
		return strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(s), ";")), " ")
	}
	docText := strings.ReplaceAll(strings.Join(strings.Fields(body.String()), " "), " ;", ";")
	for name, sql := range consts {
		if !strings.Contains(docText, norm(sql)+";") {
			t.Errorf("%s is not mirrored in db/queries/operator.sql (whitespace aside)", name)
		}
	}
}

// TestOperatorByEmail_TheBeltHoldsWhereRLSDoesNot: the login lookup's `status =
// 'active'` is the belt beside tappa_operator's policy. Measured where RLS does NOT
// apply -- the owner, a superuser -- a pending and a disabled account are still no
// answer, and an active one is found case-insensitively; as tappa_operator the same.
// Removing the predicate turns the owner half red (the policy cannot, from there).
func TestOperatorByEmail_TheBeltHoldsWhereRLSDoesNot(t *testing.T) {
	ctx, tx := opTx(t)
	active := opNewActive(t, ctx, tx)
	pending, _ := opNewPending(t, ctx, tx, 0, 0)
	disabled := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`, disabled.id); err != nil {
		t.Fatalf("disable: %v", err)
	}
	var superuser bool
	if err := tx.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname = current_user`).Scan(&superuser); err != nil || !superuser {
		t.Fatalf("PREMISE: the owner session is not a superuser (err %v), so RLS is not out of the way", err)
	}
	for _, as := range []string{"owner (RLS bypassed)", "tappa_operator"} {
		lookup := func(email string) (OperatorAccount, error) {
			if as == "tappa_operator" {
				var a OperatorAccount
				err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) (e error) { a, e = OperatorByEmail(ctx, sp, email); return })
				return a, err
			}
			return OperatorByEmail(ctx, tx, email)
		}
		got, err := lookup(strings.ToUpper(active.email))
		if err != nil || got.ID != active.id || got.Digest.RevealForPasswordComparison() == "" || len(got.Sealed.RevealForOpen()) != 48 {
			t.Fatalf("%s: the active account by its upper-cased address: %v (found=%v)", as, err, got.ID == active.id)
		}
		for name, acc := range map[string]opAccount{"pending": pending, "disabled": disabled} {
			if _, err := lookup(acc.email); !errors.Is(err, ErrNoOperator) {
				t.Errorf("%s: a %s account's address answered %v, want ErrNoOperator", as, name, err)
			}
		}
		if _, err := lookup(strings.Repeat("x", 255)); !errors.Is(err, ErrNoOperator) {
			t.Errorf("%s: an address longer than the column allows: %v", as, err)
		}
		for name, bad := range map[string]string{"a NUL byte": "a\x00b@example.test", "invalid UTF-8": "a\xffb@example.test"} {
			if _, err := lookup(bad); !errors.Is(err, ErrNoOperator) {
				t.Errorf("%s: an address with %s: %v, want ErrNoOperator (not a database error)", as, name, err)
			}
		}
	}
	// The id lookup's belt, from the owner too. The DISABLED account is the probe that
	// can fail: it has both credentials, so without `status = 'active'` the owner reads it
	// back. The pending one has neither, and scanOperator's missing-credential fallback
	// answers ErrNoOperator for it whatever the statement says -- measured (OP-6
	// verification, 2026-09-30): with the pending probe alone, removing the id lookup's
	// predicate stayed green. The active account is the control.
	if got, err := OperatorByID(ctx, tx, active.id); err != nil || got.ID != active.id {
		t.Fatalf("CONTROL: the active account by id, as the owner: %v", err)
	}
	for name, acc := range map[string]opAccount{"pending": pending, "disabled": disabled} {
		if _, err := OperatorByID(ctx, tx, acc.id); !errors.Is(err, ErrNoOperator) {
			t.Errorf("the %s account by id, as the owner: %v, want ErrNoOperator (belt)", name, err)
		}
	}
}

// TestOperatorAccessors_TheCustomerRoleCannotUseThem is ADR 0021 §4 ("gürültülü"):
// every accessor, run on a connection that is tappa_app, fails with the privilege
// error (42501) -- surfaced as a database error, NOT as the operator's refusal and not
// as "no such operator" -- because tappa_app holds no EXECUTE on any op_* and nothing
// on platform_admins.
func TestOperatorAccessors_TheCustomerRoleCannotUseThem(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	for name, call := range map[string]func(c OperatorConn) error{
		"OperatorByEmail": func(c OperatorConn) error { _, e := OperatorByEmail(ctx, c, a.email); return e },
		"OperatorByID":    func(c OperatorConn) error { _, e := OperatorByID(ctx, c, a.id); return e },
		"RecordOperatorAuthEvent": func(c OperatorConn) error {
			return RecordOperatorAuthEvent(ctx, c, OperatorLoginFailed, a.email, uuid.Nil)
		},
		"OpenOperatorSession": func(c OperatorConn) error { return OpenOperatorSession(ctx, c, a.id, opRandHex(t), 1) },
		"CompleteOperatorEnrollment": func(c OperatorConn) error {
			return CompleteOperatorEnrollment(ctx, c, a.id, "x", opFakeDigest("z"), opFakeSealed(1), 1, opRandHex(t))
		},
		"TouchOperatorSession": func(c OperatorConn) error { _, e := TouchOperatorSession(ctx, c, opRandHex(t)); return e },
		"CloseOperatorSession": func(c OperatorConn) error { return CloseOperatorSession(ctx, c, opRandHex(t)) },
		// OP-10 (00027): the version list's first phase is the call that meets the
		// missing EXECUTE, and the publication.
		"LegalVersions": func(c OperatorConn) error {
			_, e := LegalVersions(ctx, c, opRandHex(t), LegalVersionsPage{Number: 1, Size: 10})
			return e
		},
		"PublishLegal": func(c OperatorConn) error { return PublishLegal(ctx, c, opRandHex(t), "privacy", "FAKE text") },
		// OP-11 (00029): both reads meet the missing EXECUTE in their first phase.
		"TenantList": func(c OperatorConn) error {
			_, e := TenantList(ctx, c, opRandHex(t), TenantListQuery{Search: "x", Number: 1, Size: 10})
			return e
		},
		"TenantDetail": func(c OperatorConn) error { _, e := TenantDetail(ctx, c, opRandHex(t), uuid.New()); return e },
		// OP-13 (00030): the inventory meets the missing EXECUTE in its first phase.
		"TenantPlaques": func(c OperatorConn) error { _, e := TenantPlaques(ctx, c, opRandHex(t), uuid.New()); return e },
		// OP-14 (00031): the log's read meets the missing EXECUTE in its first phase -- and
		// the 'password_ok' row is RecordOperatorAuthEvent's, above, the same EXECUTE.
		"OperatorAudit": func(c OperatorConn) error {
			_, e := OperatorAudit(ctx, c, opRandHex(t), OperatorAuditQuery{Kind: OperatorAuditRead, Number: 1, Size: 50})
			return e
		},
		"RecordOperatorAuthEvent password_ok": func(c OperatorConn) error {
			return RecordOperatorAuthEvent(ctx, c, OperatorPasswordOK, "", a.id)
		},
	} {
		err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error { return call(sp) })
		if err == nil || errors.Is(err, ErrOperatorRefused) || errors.Is(err, ErrNoOperator) || !strings.Contains(err.Error(), "SQLSTATE 42501") {
			t.Errorf("%s as tappa_app: %v, want a 42501 database error", name, err)
		}
	}
}

// TestSealedSecret_CopiesInAndOut: the wrapper holds its own copy (the caller's slice
// can be wiped without changing it) and hands out a copy (wiping what Open received
// does not wipe the account's value); the zero value reveals nothing.
func TestSealedSecret_CopiesInAndOut(t *testing.T) {
	in := []byte("0123456789abcdef0123456789abcdef0123456789abcdef")
	s := NewSealedSecret(in)
	clear(in)
	out := s.RevealForOpen()
	if string(out) != "0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatal("wiping the caller's slice changed the wrapped envelope")
	}
	clear(out)
	if string(s.RevealForOpen()) != "0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatal("wiping a revealed copy changed the wrapped envelope")
	}
	if (SealedSecret{}).RevealForOpen() != nil {
		t.Fatal("the zero value revealed something")
	}
}

// TestOperatorAccessors_AnUnstorableAddressIsNoAnswerNotAnError: an address PostgreSQL
// cannot hold as text (a NUL byte, invalid UTF-8) is not an operator's, so the lookup
// answers ErrNoOperator without asking the server and the failure row is written with
// the address sent as NULL -- the unknown-address arm, not a database error. Measured
// (OP-6 verification, 2026-09-30): both came back as SQLSTATE 22021 and the password
// step returned a database error with no comparison and no row. The control sends the
// same row with a storable unknown address.
func TestOperatorAccessors_AnUnstorableAddressIsNoAnswerNotAnError(t *testing.T) {
	ctx, tx := opTx(t)
	count := func() int64 {
		var n int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'unknown_email' AND target_admin_id IS NULL`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	for name, addr := range map[string]string{
		"CONTROL, a storable unknown address": "nobody-" + uuid.NewString()[:8] + "@example.test",
		"a NUL byte":                          "a\x00b@example.test",
		"invalid UTF-8":                       "a\xffb@example.test",
	} {
		before := count()
		err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			if _, err := OperatorByEmail(ctx, sp, addr); !errors.Is(err, ErrNoOperator) {
				return fmt.Errorf("lookup: %w", err)
			}
			return RecordOperatorAuthEvent(ctx, sp, OperatorUnknownEmail, addr, uuid.Nil)
		})
		if err != nil || count() != before+1 {
			t.Errorf("%s: %v, %+d row(s), want no error and 1 unknown_email row without a target", name, err, count()-before)
		}
	}
}
