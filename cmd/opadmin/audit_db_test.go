package main

// audit_db_test.go -- M10 OP-14 D (migration 00033): each opadmin action leaves exactly one
// operator_audit_log row, and a refused one leaves none. Applied as the owner inside the
// test's own transaction, which is rolled back (opadmin_db_test.go's harness, its lock and its
// clock: every script generated here is generated with the database's clock).

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// ownerAuditRow is one operator_audit_log row naming an account, every column but its id.
type ownerAuditRow struct {
	kind                   string
	session, actor, tenant *uuid.UUID
	scope                  *string
	number, size           *int32
	detail                 string
	at                     time.Time
}

// ownerRowsOf reads the rows of the three owner kinds that name the account, oldest first.
func ownerRowsOf(t *testing.T, ctx context.Context, tx pgx.Tx, id string) []ownerAuditRow {
	t.Helper()
	rows, err := tx.Query(ctx, `
		SELECT kind, session_id, actor_admin_id, target_tenant_id, target_scope, page_number, page_size, detail::text, at
		  FROM public.operator_audit_log
		 WHERE target_admin_id = $1 AND kind = ANY ($2)
		 ORDER BY at, id`, id, []string{auditKindCreate, auditKindResetMFA, auditKindDisable})
	if err != nil {
		t.Fatalf("read the account's owner rows: %v", err)
	}
	defer rows.Close()
	var out []ownerAuditRow
	for rows.Next() {
		var r ownerAuditRow
		if err := rows.Scan(&r.kind, &r.session, &r.actor, &r.tenant, &r.scope, &r.number, &r.size, &r.detail, &r.at); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// ownerRowCount is the number of rows of the three owner kinds the transaction sees, every
// account's: an application must move it by exactly one, a refusal by none.
func ownerRowCount(t *testing.T, ctx context.Context, tx pgx.Tx) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM public.operator_audit_log WHERE kind = ANY ($1)`,
		[]string{auditKindCreate, auditKindResetMFA, auditKindDisable}).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAudit_EachActionLeavesExactlyOneRowAndARefusalNone (M10 OP-14 D; ADR 0020 §5's owner
// exception, ADR 0021's OP-14 D note):
//
// PART I -- on one account, applied as the owner: create, then (after the link enrolled it)
// reset-mfa, then disable TWICE. Each application moves the count of owner rows in the whole
// log by exactly ONE, and that row is the action's kind naming THIS account by id, with no
// session, no actor, no tenant, no scope, no page and detail {} -- no address, no name -- and
// an `at` inside the application, by the database's clock (read before and after it). The
// account's owner rows are, oldest first, operator_created, operator_mfa_reset,
// operator_disabled, operator_disabled, and each kind is one internal/db's ByOwner names.
// Each refusal moves the count by NONE: the same create applied again (the account's id
// exists), reset-mfa naming an id no account has, the same reset-mfa applied again, disable
// with another account's address, reset-mfa of the disabled account, and create with the
// disabled account's address (platform_admins_email_key). And the price (ADR 0021 OP-14 D LD9,
// 2nd round): the owner's DELETE of an account opadmin created is refused, 23503 on
// operator_audit_log_target_admin_id_fkey -- CONTROL: an account no row names is deleted.
//
// PART II -- red on: an action that writes no row, two rows, a row of another kind or for
// another account, a row carrying a session, an actor, a tenant, a scope, a page or a detail,
// a row dated outside its application, a refusal that leaves a row, and a created account
// that can be deleted. PART III -- These actions and refusals only (no completeness claim).
func TestAudit_EachActionLeavesExactlyOneRowAndARefusalNone(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	email := randomEmail(t, "op14d.audit.")
	const host = "ops.taptime.mt"

	// apply runs one script and checks the one row it must leave, of kind for id.
	apply := func(what, sql, id, kind string) {
		t.Helper()
		n0 := ownerRowCount(t, ctx, tx)
		mine := len(ownerRowsOf(t, ctx, tx, id))
		before := dbNow(t, ctx, tx)
		mustApply(t, ctx, tx, sql)
		after := dbNow(t, ctx, tx)
		if d := ownerRowCount(t, ctx, tx) - n0; d != 1 {
			t.Fatalf("%s: the owner rows of the whole log moved by %d, want exactly 1", what, d)
		}
		rows := ownerRowsOf(t, ctx, tx, id)
		if len(rows) != mine+1 {
			t.Fatalf("%s: the account has %d owner row(s), want %d", what, len(rows), mine+1)
		}
		r := rows[len(rows)-1]
		var detail map[string]any
		if err := json.Unmarshal([]byte(r.detail), &detail); err != nil || len(detail) != 0 {
			t.Errorf("%s: detail is %d bytes (err %v), want the empty object", what, len(r.detail), err)
		}
		if r.kind != kind || r.session != nil || r.actor != nil || r.tenant != nil || r.scope != nil || r.number != nil || r.size != nil {
			t.Errorf("%s: the row is kind %q, session %v, actor %v, tenant %v, scope %v, page %v/%v; want %q and nothing else",
				what, r.kind, r.session != nil, r.actor != nil, r.tenant != nil, r.scope != nil, r.number != nil, r.size != nil, kind)
		}
		if r.at.Before(before) || r.at.After(after) {
			t.Errorf("%s: the row is dated %v, outside its application [%v, %v] by the database's clock", what, r.at, before, after)
		}
		if !db.OperatorAuditKind(r.kind).ByOwner() {
			t.Errorf("%s: internal/db's ByOwner does not name the kind %q", what, r.kind)
		}
	}
	// refused runs a script the database must refuse with a message holding want, and checks
	// it left no owner row anywhere.
	refused := func(what, sql, want string) {
		t.Helper()
		n0 := ownerRowCount(t, ctx, tx)
		refusedWith(t, ctx, tx, sql, want)
		if d := ownerRowCount(t, ctx, tx) - n0; d != 0 {
			t.Errorf("%s: a refused application moved the owner rows by %d", what, d)
		}
	}

	g := dbGen(t, ctx, tx, "create", "--email", email, "--name", "Op Fourteen D", "--host", host)
	apply("create", g.sql, g.id, auditKindCreate)
	refused("the same create again", g.sql, "refused by constraint platform_admins_")
	if _, err := o.enroll(ctx, linkOf(t, g)); err != nil {
		t.Fatalf("the create link did not enroll: %v", err)
	}

	unknown := uuid.NewString()
	refused("reset-mfa of an id no account has",
		dbGen(t, ctx, tx, "reset-mfa", "--id", unknown, "--email", email, "--host", host).sql, "no operator account has BOTH id "+unknown)
	if n := len(ownerRowsOf(t, ctx, tx, unknown)); n != 0 {
		t.Errorf("%d owner row(s) name the unknown id", n)
	}
	r := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", strings.ToUpper(email), "--host", host)
	apply("reset-mfa", r.sql, g.id, auditKindResetMFA)
	refused("the same reset-mfa again", r.sql, "this script was already applied")

	other := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op14d.other."), "--name", "Other", "--host", host)
	apply("create (another account)", other.sql, other.id, auditKindCreate)
	_, mismatched, _ := runCmd(t, true, "disable", "--id", g.id, "--email", readAccount(t, ctx, tx, other.id).email)
	refused("disable with another account's address", mismatched, "no operator account has BOTH id "+g.id)

	_, d, _ := runCmd(t, true, "disable", "--id", g.id, "--email", email)
	apply("disable", d, g.id, auditKindDisable)
	apply("disable, applied a second time", d, g.id, auditKindDisable)
	refused("reset-mfa of the disabled account",
		dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", host).sql, "is disabled")
	// LD9's other half: the disabled account keeps its address, so a new create with it is refused.
	refused("create with the disabled account's address",
		dbGen(t, ctx, tx, "create", "--email", strings.ToUpper(email), "--name", "Op Fourteen D again", "--host", host).sql,
		"refused by constraint platform_admins_email_key")

	var kinds []string
	for _, row := range ownerRowsOf(t, ctx, tx, g.id) {
		kinds = append(kinds, row.kind)
	}
	if want := []string{auditKindCreate, auditKindResetMFA, auditKindDisable, auditKindDisable}; strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("the account's owner rows are %v, want %v", kinds, want)
	}
	if rows := ownerRowsOf(t, ctx, tx, other.id); len(rows) != 1 || rows[0].kind != auditKindCreate {
		t.Errorf("the other account's owner rows are %d, want its one 'operator_created'", len(rows))
	}

	// ADR 0021 OP-14 D LD9: an account opadmin created can no longer be deleted, not even by
	// the owner -- its 'operator_created' row names it (ON DELETE RESTRICT) and the log is
	// append-only. `other` never enrolled: that row is the only one naming it. CONTROL: an
	// account with no row naming it (written here directly, not by opadmin) is deleted.
	deleteIn := func(id string) (int64, error) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		defer func() {
			if err := sp.Rollback(ctx); err != nil {
				t.Fatalf("rollback to savepoint: %v", err)
			}
		}()
		tag, err := sp.Exec(ctx, `DELETE FROM public.platform_admins WHERE id = $1`, id)
		return tag.RowsAffected(), err
	}
	var bare string
	if err := tx.QueryRow(ctx, `INSERT INTO public.platform_admins (email, display_name, status)
	                             VALUES ($1, 'FAKE no trace', 'disabled') RETURNING id::text`,
		randomEmail(t, "op14d.bare.")).Scan(&bare); err != nil {
		t.Fatal(err)
	}
	if n, err := deleteIn(bare); err != nil || n != 1 {
		t.Fatalf("CONTROL FAILED: deleting an account no row names: %d row(s), err %v", n, err)
	}
	var pgErr *pgconn.PgError
	if n, err := deleteIn(other.id); !errors.As(err, &pgErr) || pgErr.Code != "23503" ||
		pgErr.ConstraintName != "operator_audit_log_target_admin_id_fkey" {
		t.Errorf("deleting an account opadmin created: %d row(s), err %v; want 23503 on operator_audit_log_target_admin_id_fkey", n, err)
	}
}

// keptStore is txStore that keeps what the database answered op_complete_enrollment, so a
// refusal can be told apart from one operatorauth makes before it asks.
type keptStore struct {
	txStore
	got *error
}

func (s keptStore) CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, raw, digest string, sealed []byte, step int64, h string) error {
	err := s.txStore.CompleteOperatorEnrollment(ctx, admin, raw, digest, sealed, step, h)
	*s.got = err
	return err
}

// keptSide is newOperatorSide over keptStore.
func keptSide(t *testing.T, ctx context.Context, tx pgx.Tx, got *error) *operatorSide {
	t.Helper()
	o := newOperatorSide(t, ctx, tx)
	key := func() operatorauth.Key {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		return operatorauth.NewKey(b)
	}
	auth, err := operatorauth.New(keptStore{txStore: txStore{t: t, tx: tx}, got: got}, operatorauth.Config{
		TOTPKEK: key(), TokenHMACKey: key(), Now: func() time.Time { return o.now }, Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("operatorauth.New: %v", err)
	}
	o.auth = auth
	return o
}

// TestDisable_TheUnusedLinkOfADisabledAccountIsRefused (ADR 0021 OP-14 D LD9, 3rd round of the
// review):
//
// PART I -- an account created by opadmin and never enrolled, then disabled, still carries its
// unused link (disable leaves the issuance columns as they are), and that link is refused BY THE
// DATABASE: op_complete_enrollment answers 28000 (db.ErrOperatorRefused; operatorauth says it as
// ErrEnrollment). The account stays disabled with its link unused and no session. CONTROL: the
// same link, before the disable, in a savepoint rolled back, enrolls the account (the database
// says yes, the account turns active) -- so nothing on the Go side refuses this link. What
// refuses it after the disable is op_complete_enrollment's `a.status = 'pending'` (00026), the
// only thing that does: LD9's way out of a mistyped account (disable it, create another) rests
// on it.
//
// PART II -- red on: a disabled account's unused link that enrolls it (the third eye's X6, the
// condition written `status <> 'active'`), a refusal that changes the account or opens a
// session, and a refusal the database did not make. PART III -- this account shape only (no
// completeness claim).
func TestDisable_TheUnusedLinkOfADisabledAccountIsRefused(t *testing.T) {
	ctx, tx := ownerTx(t)
	email := randomEmail(t, "op14d.unused.")
	g := dbGen(t, ctx, tx, "create", "--email", email, "--name", "Op Fourteen D Unused", "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, g.sql)
	created := readAccount(t, ctx, tx, g.id)
	if created.status != "pending" || created.hash == nil || created.used != nil {
		t.Fatalf("PREMISE: the created account is %q with a link %v, used %v", created.status, created.hash != nil, created.used != nil)
	}

	var answer error
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if _, err := keptSide(t, ctx, sp, &answer).enroll(ctx, linkOf(t, g)); err != nil || answer != nil {
		t.Fatalf("CONTROL FAILED: the link before the disable: %v (the database: %v)", err, answer)
	}
	if r := readAccount(t, ctx, sp, g.id); r.status != "active" || r.used == nil {
		t.Fatalf("CONTROL FAILED: the enrolled account is %q, link used %v", r.status, r.used != nil)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback to savepoint: %v", err)
	}

	_, d, _ := runCmd(t, true, "disable", "--id", g.id, "--email", email)
	mustApply(t, ctx, tx, d)
	disabled := readAccount(t, ctx, tx, g.id)
	if disabled.status != "disabled" || disabled.hash == nil || *disabled.hash != *created.hash || disabled.used != nil {
		t.Fatalf("PREMISE: after the disable the account is %q, link kept %v, used %v -- the case this test is about is gone",
			disabled.status, disabled.hash != nil && *disabled.hash == *created.hash, disabled.used != nil)
	}

	answer = nil
	_, err = keptSide(t, ctx, tx, &answer).enroll(ctx, linkOf(t, g))
	if !errors.Is(err, operatorauth.ErrEnrollment) || !errors.Is(answer, db.ErrOperatorRefused) {
		t.Errorf("the unused link of the disabled account: %v (the database: %v); want ErrEnrollment from the database's 28000", err, answer)
	}
	if r := readAccount(t, ctx, tx, g.id); r.status != "disabled" || r.used != nil || r.sessions != 0 {
		t.Errorf("after the refused link the account is %q, link used %v, %d session(s); want disabled, unused, none",
			r.status, r.used != nil, r.sessions)
	}
}

// runbookVerifySQL is the opadmin runbook's verify step, as written: the heredoc of the first
// psql call that reads one in deploy/README.md's operator accounts section.
func runbookVerifySQL(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
	if err != nil {
		t.Fatalf("read the runbook: %v", err)
	}
	s := string(b)
	i := strings.Index(s, "\n## Operator accounts (M10 OP-9)")
	if i < 0 {
		t.Fatal("the runbook has no operator accounts section")
	}
	s = s[i+1:]
	if j := strings.Index(s, "\n## "); j >= 0 {
		s = s[:j]
	}
	const open = "<<'SQL'\n"
	k := strings.Index(s, open)
	if k < 0 {
		t.Fatal("the operator accounts section has no psql heredoc")
	}
	s = s[k+len(open):]
	e := strings.Index(s, "\nSQL\n")
	if e < 0 {
		t.Fatal("the heredoc does not end")
	}
	return s[:e]
}

// TestRunbook_TheVerifyQueryShowsEachActionsRow (ADR 0021 OP-14 D LD11, 3rd round of the review):
//
// PART I -- the runbook's verify step, read from deploy/README.md and run as written (simple
// protocol, as the owner -- psql's way), names for each account its last owner row: after a
// create 'operator_created', after a disable 'operator_disabled', each at most a minute old. And
// it tells an untraced application apart: a disable whose script lacks the audit row -- this
// generator's script with the row taken out, the shape of a script from an opadmin before
// 00033's commit (the third eye measured that opadmin's own disable leaving no row) -- disables
// the account and the step still names 'operator_created'.
//
// PART II -- red on: a verify step that fails on the schema, names no row, another kind or a
// stale time after an application, and one that cannot tell an untraced disable apart.
// PART III -- create and disable only (no completeness claim).
func TestRunbook_TheVerifyQueryShowsEachActionsRow(t *testing.T) {
	ctx, tx := ownerTx(t)
	verify := runbookVerifySQL(t)
	if !strings.Contains(verify, "operator_audit_log") {
		t.Fatal("PREMISE: the runbook's verify step does not read operator_audit_log")
	}
	// lastRow runs the step and returns its last statement's row for the account: kind and age.
	lastRow := func(id string) (string, int) {
		t.Helper()
		res, err := tx.Conn().PgConn().Exec(ctx, verify).ReadAll()
		if err != nil {
			t.Fatalf("the runbook's verify step: %v", err)
		}
		last := res[len(res)-1]
		var names []string
		for _, f := range last.FieldDescriptions {
			names = append(names, f.Name)
		}
		if strings.Join(names, ",") != "id,kind,at,seconds_ago" {
			t.Fatalf("the verify step's last statement returns %v, want id, kind, at, seconds_ago", names)
		}
		for _, row := range last.Rows {
			if string(row[0]) == id {
				if row[1] == nil {
					return "", -1
				}
				age, err := strconv.Atoi(string(row[3]))
				if err != nil {
					t.Fatalf("seconds_ago %q: %v", row[3], err)
				}
				return string(row[1]), age
			}
		}
		t.Fatalf("the verify step lists no row for %s", id)
		return "", 0
	}
	fresh := func(what, id, want string) {
		t.Helper()
		if kind, age := lastRow(id); kind != want || age < 0 || age > 60 {
			t.Errorf("after %s the verify step names %q, %d s old; want %q, at most a minute", what, kind, age, want)
		}
	}

	const host = "ops.taptime.mt"
	email := randomEmail(t, "op14d.verify.")
	g := dbGen(t, ctx, tx, "create", "--email", email, "--name", "Op Fourteen D Verify", "--host", host)
	mustApply(t, ctx, tx, g.sql)
	fresh("create", g.id, auditKindCreate)
	_, d, _ := runCmd(t, true, "disable", "--id", g.id, "--email", email)
	mustApply(t, ctx, tx, d)
	fresh("disable", g.id, auditKindDisable)

	otherEmail := randomEmail(t, "op14d.untraced.")
	h := dbGen(t, ctx, tx, "create", "--email", otherEmail, "--name", "Op Fourteen D Untraced", "--host", host)
	mustApply(t, ctx, tx, h.sql)
	_, hd, _ := runCmd(t, true, "disable", "--id", h.id, "--email", otherEmail)
	row := auditRow(subcommand{id: h.id}, auditKindDisable)
	if strings.Count(hd, row) != 1 {
		t.Fatal("PREMISE: the disable script does not carry its audit row exactly once")
	}
	n0 := ownerRowCount(t, ctx, tx)
	mustApply(t, ctx, tx, strings.Replace(hd, row, "", 1))
	if r := readAccount(t, ctx, tx, h.id); r.status != "disabled" || ownerRowCount(t, ctx, tx) != n0 {
		t.Fatalf("PREMISE: the untraced disable left the account %q and moved the owner rows by %d", r.status, ownerRowCount(t, ctx, tx)-n0)
	}
	if kind, _ := lastRow(h.id); kind != auditKindCreate {
		t.Errorf("after an untraced disable the verify step names %q; want the account's 'operator_created' -- not its disable", kind)
	}
}
