package db

// operatorlegal_test.go -- migration 00027 (M10 OP-10, phase A): the legal texts on the
// operator's surface. The catalogue half (signatures, grants, the Down, the
// precondition, the temp-table shadow) and the behaviour half (op_begin_read,
// op_read_legal_versions, op_publish_legal, the Go accessors) of ADR 0021 §6's list for
// OP-10 (m10-platform.md, OP-4 block, item "OP-10").
//
// HOW EACH TEST TOUCHES THE SHARED DATABASE -- three shapes, named:
//   - most tests run in opTx: one rolled-back REPEATABLE READ transaction as the owner,
//     identities switched with SET LOCAL SESSION AUTHORIZATION, the operator-tables lock
//     taken EXCLUSIVE (several of them run DDL inside the transaction);
//   - the read side's predicates are reached WITHOUT a commit through a ticket the
//     owner writes with created_xact naming a transaction that really committed
//     (opCommittedXact): the owner may write that column, tappa_opdefiner may not
//     (00026's grant, pinned there and in TestOpReadLegalVersions_AForgedTicketIsRefused);
//   - three tests need a COMMIT because a commit is their subject
//     (TestOpReadLegalVersions_TwoPhaseLifecycle,
//     TestOpReadLegalVersions_AnUncommittedTicketIsInvisibleToAnotherSession,
//     TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions). They take the lock
//     SHARED and write their fixture through opLiveFixture, whose cleanup deletes the
//     tickets, revokes the sessions, disables the account and deletes whatever no
//     committed audit row pins. What stays per run, by construction: one account
//     (disabled) and one session (revoked) for each of the two tests that commit a
//     'read' row, and those two 'read' rows -- operator_audit_log is append-only (00026:
//     its triggers bind the owner too), and its foreign keys keep the account and the
//     session a committed row references. The versions this file writes are written
//     inside transactions that are rolled back (legal_documents' row count was the same
//     before and after a run, measured).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The three functions 00027 creates, by their exact catalogue identity.
var op00027Functions = map[string]struct{ args, result string }{
	"op_begin_read":          {"p_session text, p_kind text, p_params jsonb", "text"},
	"op_read_legal_versions": {"p_session text, p_ticket text, p_page_number integer, p_page_size integer", "TABLE(version_id uuid, slug text, published_at timestamp with time zone, body_bytes integer, publisher_kind text, publisher_admin_id uuid, publisher_name text, is_current boolean)"},
	"op_publish_legal":       {"p_session text, p_slug text, p_body text", "void"},
}

// The fixed refusal messages of 00027 (one per function and reason class).
const (
	beginParamsRefusal = "op_begin_read: read parameters refused"
	beginRefusal       = "op_begin_read: read refused"
	readRefusal        = "op_read_legal_versions: read refused"
	publishRefusal     = "op_publish_legal: document refused"
	touchRefusal00026  = "op_touch_session: operator session refused"
	legalCeilingBytes  = 262144 // ADR 0016 §6: 256 KiB
)

// ------------------------------------------------------------------ helpers --

// opLegalParams is the version list's parameter object as JSON text.
func opLegalParams(number, size int) string {
	return fmt.Sprintf(`{"page_number": %d, "page_size": %d}`, number, size)
}

// opTicketHash is what operator_read_tickets.ticket_hash must hold for a raw ticket
// and a page: SHA-256 over the raw ticket followed by jsonb's canonical text of the
// rebuilt parameter object. jsonb orders keys shorter-first, so page_size precedes
// page_number -- written out here rather than computed by the database, so that a
// change in what the functions hash turns a test red instead of following along.
func opTicketHash(raw string, number, size int) string {
	sum := sha256.Sum256([]byte(raw + fmt.Sprintf(`{"page_size": %d, "page_number": %d}`, size, number)))
	return hex.EncodeToString(sum[:])
}

// opBegin runs op_begin_read as tappa_operator inside a savepoint of tx.
func opBegin(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, kind, params any) (string, error) {
	t.Helper()
	var raw string
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, beginOperatorReadSQL, hash, kind, params).Scan(&raw)
	})
	return raw, err
}

// opScanVersions runs the shipped statement and scans it, returning the database's
// error untouched (operatorErr would turn 28000 into a sentinel; these tests want the
// SQLSTATE and the message).
func opScanVersions(ctx context.Context, q opQuerier, hash, ticket any, number, size any) ([]LegalVersion, error) {
	rows, err := q.Query(ctx, readLegalVersionsSQL, hash, ticket, number, size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LegalVersion
	for rows.Next() {
		var v LegalVersion
		var kind string
		if err := rows.Scan(&v.ID, &v.Slug, &v.PublishedAt, &v.BodyBytes, &kind, &v.PublisherID,
			&v.PublisherName, &v.Current); err != nil {
			return nil, err
		}
		v.PublishedBy = LegalPublisherKind(kind)
		out = append(out, v)
	}
	return out, rows.Err()
}

// opRead runs op_read_legal_versions as tappa_operator inside a savepoint of tx. On
// success the savepoint is released, so the consumption stays in tx.
func opRead(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket any, number, size any) ([]LegalVersion, error) {
	t.Helper()
	var out []LegalVersion
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		out, e = opScanVersions(ctx, sp, hash, ticket, number, size)
		return e
	})
	return out, err
}

// opPublish runs op_publish_legal as tappa_operator inside a savepoint of tx.
func opPublish(t *testing.T, ctx context.Context, tx pgx.Tx, hash string, slug, body any) error {
	t.Helper()
	return opExecAs(t, ctx, tx, "tappa_operator", publishLegalSQL, hash, slug, body)
}

// opCommittedXact returns the id of a transaction that has COMMITTED: a fresh owner
// connection runs one statement in autocommit and then confirms the status itself.
func opCommittedXact(t *testing.T, ctx context.Context) string {
	t.Helper()
	c, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("connect for a committed xid: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	var x, status string
	if err := c.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&x); err != nil {
		t.Fatalf("read an xid: %v", err)
	}
	if err := c.QueryRow(ctx, `SELECT pg_xact_status($1::xid8)`, x).Scan(&status); err != nil || status != "committed" {
		t.Fatalf("PREMISE: the autocommit transaction %s reads %q (%v), want committed", x, status, err)
	}
	return x
}

// opForgeTicket writes, as the owner, a ticket the read side treats as one an EARLIER,
// COMMITTED op_begin_read made: its created_xact names a committed transaction and its
// hash binds raw to the page exactly as op_read_legal_versions recomputes it. It
// returns the raw ticket and the ticket's id. ttl is an interval literal for expires_at
// relative to the wall clock (within 00026's 60-second ceiling).
func opForgeTicket(t *testing.T, ctx context.Context, tx pgx.Tx, session, admin uuid.UUID, number, size int, xact, ttl string) (string, uuid.UUID) {
	t.Helper()
	raw := opRandHex(t)
	var auditID, ticketID uuid.UUID
	// public.-qualified: a temp table of the same name (the shadow test) is first on
	// this session's search_path, the owner's included.
	if err := tx.QueryRow(ctx, `INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_scope)
	                            VALUES ('read', $1, $2, 'legal_versions') RETURNING id`, session, admin).Scan(&auditID); err != nil {
		t.Fatalf("forge the ticket's audit row: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at, created_xact)
		VALUES ($1, $2, 'legal_versions', $3, clock_timestamp() + $4::interval, $5::xid8)
		RETURNING id`, opTicketHash(raw, number, size), session, auditID, ttl, xact).Scan(&ticketID); err != nil {
		t.Fatalf("forge the ticket: %v", err)
	}
	return raw, ticketID
}

// opNamed sets an operator's display name (the owner's statement), so a test can
// recognise it in the version list.
func opNamed(t *testing.T, ctx context.Context, tx pgx.Tx, id uuid.UUID) string {
	t.Helper()
	name := "op10 fixture " + id.String()[:8]
	if _, err := tx.Exec(ctx, `UPDATE public.platform_admins SET display_name = $2 WHERE id = $1`, id, name); err != nil {
		t.Fatalf("name the operator: %v", err)
	}
	return name
}

// opWantClean fails unless err is exactly (code, msg) with no DETAIL and no HINT, and no
// error field carries one of values. Lengths only are printed.
func opWantClean(t *testing.T, err error, code, msg, what string, values ...string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		t.Fatalf("%s: want a PostgreSQL error %s, got %T %v", what, code, err, err)
	}
	if pg.Code != code || pg.Message != msg {
		t.Errorf("%s: %s %q, want %s %q", what, pg.Code, pg.Message, code, msg)
	}
	if pg.Detail != "" || pg.Hint != "" {
		t.Errorf("%s: the error carries a DETAIL (%d bytes) / HINT (%d bytes)", what, len(pg.Detail), len(pg.Hint))
	}
	all := strings.Join([]string{pg.Message, pg.Detail, pg.Hint, pg.Where, pg.ConstraintName, pg.ColumnName}, "\n")
	for _, v := range values {
		if v != "" && strings.Contains(all, v) {
			t.Errorf("%s: the error text carries an argument value (%d characters of it)", what, len(v))
		}
	}
}

// opLive is a COMMITTED operator fixture for the tests whose subject is a commit: an
// active account and an MFA-stamped session (more sessions on request), visible to
// every connection. See the file header for what its cleanup removes and what stays.
type opLive struct {
	dsn      string
	owner    *pgx.Conn
	admin    uuid.UUID
	name     string
	hash     string
	session  uuid.UUID
	sessions []uuid.UUID
}

func opLiveFixture(t *testing.T) (context.Context, *opLive) {
	t.Helper()
	dsn := opOwnerDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	owner, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	// SHARED: these tests write rows, they run no DDL (TestOperatorDB_RunsAsTappaOperator's
	// precedent); the exclusive holders are opTx's.
	if _, err := owner.Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
	f := &opLive{dsn: dsn, owner: owner, admin: uuid.New()}
	f.name = "op10 live " + f.admin.String()[:8]
	if _, err := owner.Exec(ctx, `
		INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed)
		VALUES ($1, $2, $3, 'active', $4, $5)`,
		f.admin, "op10-"+f.admin.String()[:12]+"@example.test", f.name, opFakeDigest("q"), opFakeSealed(0x51)); err != nil {
		t.Fatalf("commit the operator: %v", err)
	}
	t.Cleanup(func() { f.cleanup(t) })
	f.hash, f.session = f.newSession(t, ctx)
	return ctx, f
}

func (f *opLive) newSession(t *testing.T, ctx context.Context) (string, uuid.UUID) {
	t.Helper()
	hash := opRandHex(t)
	var id uuid.UUID
	if err := f.owner.QueryRow(ctx, `INSERT INTO platform_sessions (admin_id, token_hash, mfa_verified_at)
	                                  VALUES ($1, $2, clock_timestamp()) RETURNING id`, f.admin, hash).Scan(&id); err != nil {
		t.Fatalf("commit a session: %v", err)
	}
	f.sessions = append(f.sessions, id)
	return hash, id
}

func (f *opLive) cleanup(t *testing.T) {
	ctx := context.Background()
	for _, s := range []struct{ what, sql string }{
		{"delete the tickets", `DELETE FROM operator_read_tickets WHERE session_id = ANY($1)`},
		{"revoke the sessions", `UPDATE platform_sessions SET revoked_at = coalesce(revoked_at, clock_timestamp()) WHERE id = ANY($1)`},
		{"delete the unreferenced sessions", `DELETE FROM platform_sessions s WHERE s.id = ANY($1)
		    AND NOT EXISTS (SELECT 1 FROM operator_audit_log a WHERE a.session_id = s.id)`},
	} {
		if _, err := f.owner.Exec(ctx, s.sql, f.sessions); err != nil {
			t.Errorf("cleanup: %s: %v", s.what, err)
		}
	}
	for _, s := range []struct{ what, sql string }{
		{"disable the operator", `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`},
		{"delete the operator if unreferenced", `DELETE FROM platform_admins p WHERE p.id = $1
		    AND NOT EXISTS (SELECT 1 FROM platform_sessions s WHERE s.admin_id = p.id)
		    AND NOT EXISTS (SELECT 1 FROM operator_audit_log a WHERE a.actor_admin_id = p.id OR a.target_admin_id = p.id)`},
	} {
		if _, err := f.owner.Exec(ctx, s.sql, f.admin); err != nil {
			t.Errorf("cleanup: %s: %v", s.what, err)
		}
	}
}

// connect opens one more owner connection, closed when the test ends.
func (f *opLive) connect(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// asOperatorTx begins a REPEATABLE READ transaction on c and becomes tappa_operator in
// it. The caller commits or rolls back; a transaction left open is rolled back at the
// end of the test.
func asOperatorTx(t *testing.T, ctx context.Context, c *pgx.Conn) pgx.Tx {
	t.Helper()
	tx, err := c.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Logf("rollback: %v", err)
		}
	})
	if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
		t.Fatalf("become tappa_operator: %v", err)
	}
	return tx
}

// liveReads counts the committed 'read' rows of one session (the owner's connection).
func (f *opLive) liveReads(t *testing.T, ctx context.Context, session uuid.UUID) int64 {
	t.Helper()
	return opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1`, session)
}

// ---------------------------------------------------------------- catalogue --

// TestOperator00027_TheThreeFunctionsAndTheirExactSignatures pins what ADR 0021 §6's
// generic pins leave open for these three: the exact argument list (no actor argument on
// the write -- published_by cannot come from the caller), the exact result (§2 ii's
// fixed column list for the read; the ticket as text for the first phase; void for the
// write), the owner, and that the generic forward pin raises nothing about them.
// legal_documents.published_at's DEFAULT is the wall clock (00027 section 2).
func TestOperator00027_TheThreeFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	for name, want := range op00027Functions {
		var args, result, owner string
		var retset bool
		if err := tx.QueryRow(ctx, `
			SELECT pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
			       pg_get_userbyid(p.proowner), p.proretset
			  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			 WHERE n.nspname = 'public' AND p.proname = $1`, name).Scan(&args, &result, &owner, &retset); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if args != want.args {
			t.Errorf("%s(%s), want (%s)", name, args, want.args)
		}
		if result != want.result {
			t.Errorf("%s returns %s, want %s", name, result, want.result)
		}
		if owner != "tappa_opdefiner" {
			t.Errorf("%s is owned by %s", name, owner)
		}
		if retset != (name == "op_read_legal_versions") {
			t.Errorf("%s proretset = %v", name, retset)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, name); n != 1 {
			t.Errorf("%d functions named %s; an overload would be a second door", n, name)
		}
	}
	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	for name := range op00027Functions {
		if !seen[name] {
			t.Errorf("anti-vacuity: the forward scan did not walk %s", name)
		}
		for _, f := range findings {
			if strings.Contains(f, name+"(") {
				t.Error(f)
			}
		}
	}
	clock, _, err := opFrozenClockFindings(ctx, tx)
	if err != nil {
		t.Fatalf("frozen-clock scan: %v", err)
	}
	for _, f := range clock {
		t.Error(f)
	}
	var def string
	if err := tx.QueryRow(ctx, `SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d
	                             JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
	                            WHERE d.adrelid = 'public.legal_documents'::regclass AND a.attname = 'published_at'`).Scan(&def); err != nil {
		t.Fatalf("read published_at's DEFAULT: %v", err)
	}
	if def != "clock_timestamp()" {
		t.Errorf("legal_documents.published_at DEFAULT %s, want clock_timestamp() (ADR 0021 §2 vii)", def)
	}
}

// TestOperator00027_TheApplicationCanNoLongerWriteALegalText is ADR 0020 §7's
// measurement contract on the live catalogue and as statements.
//
// 🔴 has_table_privilege IS NOT EVIDENCE HERE, AND THE TEST SHOWS WHY BEFORE IT USES THE
// RIGHT MEASURE: inside a savepoint the owner gives tappa_app back ONE column's INSERT
// (00020's shape); has_table_privilege(..., 'INSERT') stays false while
// has_any_column_privilege turns true and the INSERT succeeds. A test that asked the
// table-level question would be green on the database 00020 left behind.
func TestOperator00027_TheApplicationCanNoLongerWriteALegalText(t *testing.T) {
	ctx, tx := opTx(t)
	privs := func(q opQuerier, role string) (table, anyCol bool) {
		t.Helper()
		if err := q.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.legal_documents', 'INSERT'),
		                                  has_any_column_privilege($1, 'public.legal_documents', 'INSERT')`, role).Scan(&table, &anyCol); err != nil {
			t.Fatalf("read %s's INSERT on legal_documents: %v", role, err)
		}
		return table, anyCol
	}
	if table, anyCol := privs(tx, "tappa_app"); table || anyCol {
		t.Errorf("tappa_app: has_table_privilege(INSERT) = %v, has_any_column_privilege(INSERT) = %v; want both false", table, anyCol)
	}
	for _, col := range []string{"id", "slug", "body", "published_at", "published_by"} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_column_privilege('tappa_app', 'public.legal_documents', $1, 'INSERT')`, col).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may {
			t.Errorf("tappa_app may INSERT legal_documents.%s", col)
		}
	}
	// The public read path is untouched: exactly 00020's SELECT columns, nothing to UPDATE
	// or DELETE.
	if got := opColumns(t, ctx, tx, "tappa_app", "legal_documents", "SELECT"); got != "id,slug,body,published_at" {
		t.Errorf("tappa_app SELECT on legal_documents = (%s), want (id,slug,body,published_at)", got)
	}
	if got := opColumns(t, ctx, tx, "tappa_app", "legal_documents", "UPDATE"); got != "" {
		t.Errorf("tappa_app UPDATE on legal_documents = (%s)", got)
	}
	// tappa_operator reaches the table through the op_* only.
	for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
		if got := opColumns(t, ctx, tx, "tappa_operator", "legal_documents", priv); got != "" {
			t.Errorf("tappa_operator %s on legal_documents = (%s), want ()", priv, got)
		}
	}
	for _, role := range []string{"tappa_app", "tappa_operator"} {
		for _, priv := range []string{"DELETE", "TRUNCATE"} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.legal_documents', $2)`, role, priv).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may {
				t.Errorf("%s holds %s on legal_documents", role, priv)
			}
		}
	}

	// As statements.
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `INSERT INTO public.legal_documents (slug, body) VALUES ('privacy', 'FAKE text')`),
		sqlstateInsufficientPrivi, "tappa_app appending a legal version")
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `INSERT INTO public.legal_documents (slug, body) VALUES ('privacy', 'FAKE text')`),
		sqlstateInsufficientPrivi, "tappa_operator appending a legal version directly")
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT count(*) FROM public.legal_documents`),
		sqlstateInsufficientPrivi, "tappa_operator reading legal_documents directly")
	// POSITIVE CONTROL: tappa_app still reads the published text (Store.Refresh's path).
	if err := opExecAs(t, ctx, tx, "tappa_app", `SELECT count(body) FROM public.legal_documents`); err != nil {
		t.Fatalf("tappa_app can no longer read legal_documents.body (%v); the public pages would lose their text", err)
	}
	// tappa_app may call none of the three (ADR 0021 §1).
	for _, call := range []struct {
		name string
		sql  string
		args []any
	}{
		{"op_begin_read", beginOperatorReadSQL, []any{opRandHex(t), legalVersionsReadKind, opLegalParams(1, 10)}},
		{"op_read_legal_versions", readLegalVersionsSQL, []any{opRandHex(t), opRandHex(t), 1, 10}},
		{"op_publish_legal", publishLegalSQL, []any{opRandHex(t), "privacy", "FAKE text"}},
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_app", call.sql, call.args...), sqlstateInsufficientPrivi, "tappa_app calling "+call.name)
	}

	// WHY has_table_privilege CANNOT BE THE EVIDENCE (the control described above).
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if _, err := sp.Exec(ctx, `GRANT INSERT (slug, body) ON legal_documents TO tappa_app`); err != nil {
		t.Fatalf("re-grant 00020's column INSERT: %v", err)
	}
	table, anyCol := privs(sp, "tappa_app")
	if table || !anyCol {
		t.Fatalf("CONTROL: with a column-level INSERT granted, has_table_privilege = %v and has_any_column_privilege = %v; want false and true", table, anyCol)
	}
	if err := opExecAs(t, ctx, sp, "tappa_app", `INSERT INTO public.legal_documents (slug, body) VALUES ('privacy', 'FAKE text')`); err != nil {
		t.Fatalf("CONTROL: with the column grant back, tappa_app's INSERT still failed (%v); the refusal above would not be the grant", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback the control: %v", err)
	}
}

// TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain runs 00027's Down and Up from
// the migration file inside the test's transaction: Down restores 00020's tappa_app
// INSERT and now(), removes the three functions and the definer's grants, puts the
// ticket kind CHECK back to 00026's shape (Up again: the closed set), and puts the
// audit kind CHECK back -- VALIDATED when no 'read'/'legal_publish' row exists, NOT VALID
// when one does (the evidence stays: the table is append-only); Up then takes it all
// again. Both branches are driven: the "no row" one by removing such rows inside the
// transaction with the append-only trigger disabled -- the shape of a fresh clone.
func TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	b, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "00027_move_legal_publishing_to_the_operator.sql"))
	if err != nil {
		t.Fatalf("read 00027: %v", err)
	}
	src := string(b)
	iu, id := strings.Index(src, "-- +goose Up"), strings.Index(src, "-- +goose Down")
	if iu < 0 || id < iu {
		t.Fatalf("00027 does not carry the goose markers in order")
	}
	up, down := src[iu:id], src[id:]
	run := func(q pgx.Tx, sql, what string) {
		t.Helper()
		if _, err := q.Conn().PgConn().Exec(ctx, sql).ReadAll(); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	state := func(q opQuerier) (appInsert bool, fns int64, def string, validated bool) {
		t.Helper()
		if err := q.QueryRow(ctx, `
			SELECT has_any_column_privilege('tappa_app', 'public.legal_documents', 'INSERT'),
			       (SELECT count(*) FROM pg_proc WHERE proname IN ('op_begin_read', 'op_read_legal_versions', 'op_publish_legal')),
			       (SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d JOIN pg_attribute a
			            ON a.attrelid = d.adrelid AND a.attnum = d.adnum
			         WHERE d.adrelid = 'public.legal_documents'::regclass AND a.attname = 'published_at'),
			       (SELECT convalidated FROM pg_constraint WHERE conname = 'operator_audit_log_kind_check')`).
			Scan(&appInsert, &fns, &def, &validated); err != nil {
			t.Fatalf("read the state: %v", err)
		}
		return
	}
	definerOnLegal := func(q opQuerier) bool {
		t.Helper()
		var may bool
		if err := q.QueryRow(ctx, `SELECT has_any_column_privilege('tappa_opdefiner', 'public.legal_documents', 'SELECT')
		                               OR has_any_column_privilege('tappa_opdefiner', 'public.legal_documents', 'INSERT')`).Scan(&may); err != nil {
			t.Fatal(err)
		}
		return may
	}

	// operator_read_tickets_kind_check: 00027's closed set, and after Down 00026's shape.
	const kindClosed, kindShape = `CHECK ((kind = 'legal_versions'::text))`, `CHECK ((kind ~ '^[a-z][a-z_]{0,62}$'::text))`
	ticketKind := func(q opQuerier) string {
		t.Helper()
		var d string
		if err := q.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		                            WHERE conrelid = 'public.operator_read_tickets'::regclass
		                              AND conname = 'operator_read_tickets_kind_check'`).Scan(&d); err != nil {
			t.Fatalf("read the ticket kind CHECK: %v", err)
		}
		return d
	}

	if app, fns, def, validated := state(tx); app || fns != 3 || def != "clock_timestamp()" || !validated {
		t.Fatalf("PREMISE: before Down app INSERT=%v functions=%d default=%s validated=%v", app, fns, def, validated)
	}
	if got := ticketKind(tx); got != kindClosed {
		t.Fatalf("PREMISE: before Down the ticket kind CHECK is %s, want %s", got, kindClosed)
	}

	// Branch 1: a 'read' row exists -> the restored CHECK is NOT VALID.
	a := opNewActive(t, ctx, tx)
	_, sid := opNewSession(t, ctx, tx, a.id, true)
	if _, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_scope)
	                           VALUES ('read', $1, $2, 'legal_versions')`, sid, a.id); err != nil {
		t.Fatalf("a 'read' row: %v", err)
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	run(sp, down, "00027 Down with a 'read' row present")
	app, fns, def, validated := state(sp)
	if !app || fns != 0 || def != "now()" || validated {
		t.Errorf("after Down (a 'read' row present): app INSERT=%v (want true) functions=%d (want 0) default=%s (want now()) kind CHECK validated=%v (want false)",
			app, fns, def, validated)
	}
	if definerOnLegal(sp) {
		t.Error("after Down tappa_opdefiner still holds a privilege on legal_documents")
	}
	if got := ticketKind(sp); got != kindShape {
		t.Errorf("after Down the ticket kind CHECK is %s, want 00026's shape %s", got, kindShape)
	}
	if got := opColumns(t, ctx, sp, "tappa_app", "legal_documents", "INSERT"); got != "slug,body,published_by" {
		t.Errorf("after Down tappa_app INSERT on legal_documents = (%s), want 00020's (slug,body,published_by)", got)
	}
	// The restored CHECK is enforced for a NEW row even when not validated.
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_scope)
	                              VALUES ('read', $1, $2, 'legal_versions')`, sid, a.id),
		sqlstateCheckViolation, "a new 'read' row after Down")
	run(sp, up, "00027 Up again")
	if app, fns, def, validated := state(sp); app || fns != 3 || def != "clock_timestamp()" || !validated {
		t.Errorf("after Up again: app INSERT=%v functions=%d default=%s validated=%v; want false/3/clock_timestamp()/true", app, fns, def, validated)
	}
	if got := ticketKind(sp); got != kindClosed {
		t.Errorf("after Up again the ticket kind CHECK is %s, want %s", got, kindClosed)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback branch 1: %v", err)
	}

	// Branch 2: no such row (a fresh clone's shape) -> the restored CHECK is VALIDATED.
	sp, err = tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	for _, s := range []string{
		`ALTER TABLE operator_audit_log DISABLE TRIGGER operator_audit_log_append_only`,
		`DELETE FROM operator_read_tickets`,
		`DELETE FROM operator_audit_log WHERE kind IN ('read', 'legal_publish')`,
		`ALTER TABLE operator_audit_log ENABLE TRIGGER operator_audit_log_append_only`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatalf("clear the new kinds inside the transaction: %s: %v", s, err)
		}
	}
	run(sp, down, "00027 Down with no 'read' row")
	if _, _, _, validated := state(sp); !validated {
		t.Error("after Down with no 'read'/'legal_publish' row the kind CHECK is NOT VALID; on a fresh clone Down must restore 00026's constraint exactly")
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback branch 2: %v", err)
	}
}

// TestOperator00027_PreconditionRefusesAWrongCluster: 00027's first statement refuses
// the same role shapes 00026's does (absent, over-privileged, joined by membership in
// either direction), with SQLSTATE 55000 and the runbook's name, and passes the cluster
// this suite runs on.
func TestOperator00027_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	b, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "00027_move_legal_publishing_to_the_operator.sql"))
	if err != nil {
		t.Fatalf("read 00027: %v", err)
	}
	up := string(b)[strings.Index(string(b), "-- +goose Up"):]
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00027's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	run := func(sp pgx.Tx) error {
		_, err := sp.Conn().PgConn().Exec(ctx, pre).ReadAll()
		return err
	}
	in := func(what string, setup []string, body func(sp pgx.Tx)) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		defer func() {
			if err := sp.Rollback(ctx); err != nil {
				t.Fatalf("%s: rollback: %v", what, err)
			}
		}()
		for _, s := range setup {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: setup %q: %v", what, s, err)
			}
		}
		body(sp)
	}
	in("roles present", nil, func(sp pgx.Tx) {
		if err := run(sp); err != nil {
			t.Fatalf("the precondition refuses a correct cluster: %v", err)
		}
	})
	for _, r := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"both roles absent", []string{`ALTER ROLE tappa_operator RENAME TO zz_op10_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op10_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner without BYPASSRLS", []string{`ALTER ROLE tappa_opdefiner NOBYPASSRLS`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_opdefiner is a member", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
	} {
		in(r.what, r.setup, func(sp pgx.Tx) {
			code, msg := opCode(run(sp))
			if code != sqlstatePrerequisiteState || !strings.Contains(msg, r.want) || !strings.Contains(msg, "00027") {
				t.Errorf("%s: precondition answered %q %q, want %s naming 00027 and containing %q", r.what, code, msg, sqlstatePrerequisiteState, r.want)
			}
		})
	}
}

// TestOperator00027_CallersTempTableIsNeverRead is ADR 0021 §6's temp-table shadow for
// the three new functions: the caller creates temp tables with every name they touch,
// fills them with forged rows and GRANTs them to tappa_opdefiner (the mandatory step),
// and each function still reads and writes the real ones.
func TestOperator00027_CallersTempTableIsNeverRead(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	name := opNamed(t, ctx, tx, a.id)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	forgedSession := opRandHex(t)
	forgedRaw := opRandHex(t)
	shadowName := "SHADOW-" + uuid.NewString()[:8]
	shadowDoc := uuid.New()

	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		for _, s := range []string{
			`CREATE TEMP TABLE platform_sessions (id uuid DEFAULT gen_random_uuid(), admin_id uuid, token_hash text,
			     created_at timestamptz DEFAULT clock_timestamp(), mfa_verified_at timestamptz,
			     last_used_at timestamptz DEFAULT clock_timestamp(), revoked_at timestamptz)`,
			`CREATE TEMP TABLE platform_admins (id uuid, email text, display_name text, status text)`,
			`CREATE TEMP TABLE operator_audit_log (id uuid DEFAULT gen_random_uuid(), at timestamptz DEFAULT clock_timestamp(),
			     kind text, session_id uuid, actor_admin_id uuid, target_admin_id uuid, target_tenant_id uuid,
			     target_scope text, page_number integer, page_size integer, detail jsonb DEFAULT '{}')`,
			`CREATE TEMP TABLE operator_read_tickets (id uuid DEFAULT gen_random_uuid(), ticket_hash text, session_id uuid,
			     kind text, target_tenant_id uuid, audit_id uuid, created_at timestamptz DEFAULT clock_timestamp(),
			     created_xact xid8 DEFAULT '3'::xid8, expires_at timestamptz, consumed_at timestamptz)`,
			`CREATE TEMP TABLE legal_documents (id uuid DEFAULT gen_random_uuid(), slug text, body text,
			     published_at timestamptz DEFAULT clock_timestamp(), published_by uuid)`,
			`GRANT ALL ON pg_temp.platform_sessions, pg_temp.platform_admins, pg_temp.operator_audit_log,
			     pg_temp.operator_read_tickets, pg_temp.legal_documents TO tappa_opdefiner`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return fmt.Errorf("%s: %w", s[:40], err)
			}
		}
		if _, err := sp.Exec(ctx, `INSERT INTO pg_temp.platform_sessions (admin_id, token_hash, mfa_verified_at) VALUES ($1, $2, clock_timestamp())`,
			a.id, forgedSession); err != nil {
			return err
		}
		if _, err := sp.Exec(ctx, `INSERT INTO pg_temp.platform_admins VALUES ($1, 'shadow@example.test', $2, 'active')`, a.id, shadowName); err != nil {
			return err
		}
		if _, err := sp.Exec(ctx, `INSERT INTO pg_temp.legal_documents (id, slug, body, published_at, published_by)
		                           VALUES ($1, 'privacy', 'SHADOW text', clock_timestamp() + interval '1 day', $2)`, shadowDoc, a.id); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `INSERT INTO pg_temp.operator_read_tickets (ticket_hash, session_id, kind, expires_at)
		                        VALUES ($1, $2, 'legal_versions', clock_timestamp() + interval '30 seconds')`,
			opTicketHash(forgedRaw, 1, 5), session)
		return err
	}); err != nil {
		t.Fatalf("build the shadow as the caller: %v", err)
	}

	// CONTROL: the shadow is reachable by the definer role.
	var reachable int64
	if err := opAs(t, ctx, tx, "tappa_opdefiner", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.legal_documents) + (SELECT count(*) FROM pg_temp.operator_read_tickets)
		                              + (SELECT count(*) FROM pg_temp.platform_admins) + (SELECT count(*) FROM pg_temp.platform_sessions)`).Scan(&reachable)
	}); err != nil {
		t.Fatalf("the definer role cannot read the shadow (%v); the GRANT step is what makes this test mean anything", err)
	}
	if reachable != 4 {
		t.Fatalf("the definer role sees %d forged rows, want 4", reachable)
	}

	realCount := func(sql string, args ...any) int64 { t.Helper(); return opInt(t, ctx, tx, sql, args...) }
	docs0 := realCount(`SELECT count(*) FROM public.legal_documents WHERE published_by = $1`, a.id)
	audit0 := realCount(`SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session)
	tickets0 := realCount(`SELECT count(*) FROM public.operator_read_tickets WHERE session_id = $1`, session)

	// The forged SESSION exists only in the shadow.
	opWantClean(t, opPublish(t, ctx, tx, forgedSession, "privacy", "FAKE text"), sqlstateInvalidAuthorization, touchRefusal00026,
		"op_publish_legal with a session that exists only in the caller's temp table")
	_, err := opBegin(t, ctx, tx, forgedSession, legalVersionsReadKind, opLegalParams(1, 5))
	opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026,
		"op_begin_read with a session that exists only in the caller's temp table")
	// The real session writes into the REAL tables.
	if err := opPublish(t, ctx, tx, hash, "privacy", "FAKE text through the real table"); err != nil {
		t.Fatalf("op_publish_legal with the real session: %v", err)
	}
	if _, err := opBegin(t, ctx, tx, hash, legalVersionsReadKind, opLegalParams(1, 5)); err != nil {
		t.Fatalf("op_begin_read with the real session: %v", err)
	}
	if d := realCount(`SELECT count(*) FROM public.legal_documents WHERE published_by = $1`, a.id) - docs0; d != 1 {
		t.Errorf("the publication reached the REAL legal_documents %d time(s), want 1", d)
	}
	if d := realCount(`SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session) - audit0; d != 2 {
		t.Errorf("%d audit row(s) reached the REAL log, want 2 (legal_publish + read)", d)
	}
	if d := realCount(`SELECT count(*) FROM public.operator_read_tickets WHERE session_id = $1`, session) - tickets0; d != 1 {
		t.Errorf("%d ticket(s) reached the REAL table, want 1", d)
	}
	// The forged TICKET exists only in the shadow.
	_, err = opRead(t, ctx, tx, hash, forgedRaw, 1, 5)
	opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, "op_read_legal_versions with a ticket that exists only in the caller's temp table")
	// A real (owner-forged, committed-xact) ticket reads the REAL rows: the shadow's
	// version is not listed and the publisher's name is the real one.
	raw, ticketID := opForgeTicket(t, ctx, tx, session, a.id, 1, 5, opCommittedXact(t, ctx), "30 seconds")
	vs, err := opRead(t, ctx, tx, hash, raw, 1, 5)
	if err != nil {
		t.Fatalf("op_read_legal_versions with a real ticket: %v", err)
	}
	if len(vs) == 0 || vs[0].PublisherName == nil || *vs[0].PublisherName != name {
		t.Errorf("the newest real version is not the one just published by %q: %+v", name, vs)
	}
	for _, v := range vs {
		if v.ID == shadowDoc || (v.PublisherName != nil && *v.PublisherName == shadowName) {
			t.Errorf("the version list returned a row of the caller's temp table: %+v", v)
		}
	}
	if consumed := realCount(`SELECT count(*) FROM public.operator_read_tickets WHERE id = $1 AND consumed_at IS NOT NULL`, ticketID); consumed != 1 {
		t.Error("the real ticket was not consumed in the REAL table")
	}
	var shadowAudit, shadowDocs, shadowConsumed int64
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.operator_audit_log),
		                                (SELECT count(*) FROM pg_temp.legal_documents),
		                                (SELECT count(*) FROM pg_temp.operator_read_tickets WHERE consumed_at IS NOT NULL)`).
			Scan(&shadowAudit, &shadowDocs, &shadowConsumed)
	}); err != nil {
		t.Fatalf("read the shadow: %v", err)
	}
	if shadowAudit != 0 || shadowDocs != 1 || shadowConsumed != 0 {
		t.Errorf("the CALLER's temp tables were written: audit=%d (want 0) documents=%d (want the 1 forged) consumed tickets=%d (want 0)",
			shadowAudit, shadowDocs, shadowConsumed)
	}
}

// ----------------------------------------------- the read-side source pins --

// opNormalisedSource is a function's prosrc in lower case with every run of whitespace
// collapsed to one space -- the form the source pins below match against.
func opNormalisedSource(src string) string {
	return strings.Join(strings.Fields(strings.ToLower(src)), " ")
}

// opReadConsumptionRules are ADR 0021 §2 v 2/4's conditions on an op_read_*'s ONE
// consuming UPDATE, each a regular expression over the normalised source of that
// UPDATE's WHERE clause, in the POSITIVE spelling §2 v 4 makes normative. %[1]s is the
// alias the UPDATE gives operator_read_tickets.
var opReadConsumptionRules = []struct{ name, re string }{
	{"the hash of (raw ticket, the read's parameters)", `\b%[1]s\.ticket_hash = encode\(sha256\(convert_to\( ?p_ticket \|\| jsonb_build_object\(`},
	{"this session", `\b%[1]s\.session_id = v_session\b`},
	{"this read kind", `\b%[1]s\.kind = '[a-z][a-z_]*'`},
	{"not yet consumed", `\b%[1]s\.consumed_at is null\b`},
	{"not expired by the wall clock", `\b%[1]s\.expires_at > clock_timestamp\(\)`},
	{"created by a COMMITTED transaction, positive form", `\bpg_xact_status\(%[1]s\.created_xact\) = 'committed'`},
}

var (
	opConsumingUpdateRE = regexp.MustCompile(`update public\.operator_read_tickets as ([a-z_][a-z0-9_]*) set consumed_at = clock_timestamp\(\) where ([^;]*);`)
	opTouchIntoRE       = regexp.MustCompile(`select t\.session_id, t\.admin_id into v_session, v_admin from public\.op_touch_session\(p_session\) as t;`)
	opRefuseAfterRE     = regexp.MustCompile(`^ ?if not found then raise exception '[^']*' using errcode = 'invalid_authorization_specification'; end if;`)
	opKindLiteralRE     = `\b%[1]s\.kind = '([a-z][a-z_]*)'`
)

// opReadConsumptionFindings walks EVERY function in public whose name starts op_read_
// -- by name, so an op_read_* a later migration adds (or one whose OWNER TO was
// forgotten) is read the day it exists -- and returns one line per breach: the session
// is resolved through op_touch_session before anything else; the source holds exactly
// ONE UPDATE of operator_read_tickets, of the shape "SET consumed_at = clock_timestamp()
// WHERE ..."; its WHERE carries each of opReadConsumptionRules; the kind it names is a
// member of operator_read_tickets_kind_check; the refusal follows it at once (IF NOT
// FOUND ... 28000); and RETURN QUERY appears only after it. It also returns the names
// it read (anti-vacuity).
func opReadConsumptionFindings(ctx context.Context, q opQuerier) (findings, names []string, err error) {
	var kindCheck string
	if err := q.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
	                            WHERE conrelid = 'public.operator_read_tickets'::regclass
	                              AND conname = 'operator_read_tickets_kind_check'`).Scan(&kindCheck); err != nil {
		return nil, nil, fmt.Errorf("read operator_read_tickets_kind_check: %w", err)
	}
	rows, err := q.Query(ctx, `
		SELECT p.oid::regprocedure::text, p.proname, p.prosrc
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.proname LIKE 'op\_read\_%'
		 ORDER BY 1`)
	if err != nil {
		return nil, nil, err
	}
	type fn struct{ sig, name, src string }
	var fns []fn
	for rows.Next() {
		var f fn
		if err := rows.Scan(&f.sig, &f.name, &f.src); err != nil {
			rows.Close()
			return nil, nil, err
		}
		fns = append(fns, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for _, f := range fns {
		names = append(names, f.name)
		bad := func(format string, a ...any) { findings = append(findings, f.sig+": "+fmt.Sprintf(format, a...)) }
		src := opNormalisedSource(f.src)
		if n := strings.Count(src, "update public.operator_read_tickets"); n != 1 {
			bad("%d UPDATEs of operator_read_tickets, want exactly one consuming UPDATE", n)
			continue
		}
		m := opConsumingUpdateRE.FindStringSubmatchIndex(src)
		if m == nil {
			bad("the UPDATE of operator_read_tickets is not \"UPDATE public.operator_read_tickets AS k SET consumed_at = clock_timestamp() WHERE ...;\"")
			continue
		}
		alias, where := src[m[2]:m[3]], src[m[4]:m[5]]
		for _, r := range opReadConsumptionRules {
			if !regexp.MustCompile(fmt.Sprintf(r.re, regexp.QuoteMeta(alias))).MatchString(where) {
				bad("the consuming UPDATE's WHERE lacks %s", r.name)
			}
		}
		if km := regexp.MustCompile(fmt.Sprintf(opKindLiteralRE, regexp.QuoteMeta(alias))).FindStringSubmatch(where); km != nil &&
			!strings.Contains(kindCheck, "'"+km[1]+"'::text") {
			bad("the consuming UPDATE names kind %q, which operator_read_tickets_kind_check (%s) does not hold", km[1], kindCheck)
		}
		touch := opTouchIntoRE.FindStringIndex(src)
		if touch == nil || touch[0] > m[0] {
			bad("the session is not resolved through op_touch_session(p_session) into v_session before the consuming UPDATE")
		}
		if !opRefuseAfterRE.MatchString(src[m[1]:]) {
			bad("the consuming UPDATE is not followed at once by IF NOT FOUND THEN RAISE ... 28000")
		}
		if rq := strings.Index(src, "return query"); rq < 0 || rq < m[1] {
			bad("RETURN QUERY is missing or comes before the consuming UPDATE")
		}
	}
	return findings, names, nil
}

// TestOpRead_EveryReadConsumesItsTicketAsTheADRSays is the source pin of ADR 0021 §2 v
// 2/4 for every op_read_* in the catalogue: one consuming UPDATE whose WHERE holds the
// six conditions, in the positive spelling, before any row is returned (the rules are
// opReadConsumptionFindings'). The behaviour tests measure op_read_legal_versions; this
// pin is what a later op_read_* meets without anybody listing it.
//
// Why a source pin as well: a review measured a rewrite of the commit condition
// (created_xact <> pg_current_xact_id()) and the removal of the kind condition, each
// leaving every behaviour test green -- the first behaves the same on every ticket the
// tests can build, the second has one read kind to confuse. POSITIVE CONTROLS: copies of
// op_read_legal_versions under another op_read_ name, each with one condition removed
// or rewritten, are created inside savepoints and must each be named; the unmodified
// copy must not be.
func TestOpRead_EveryReadConsumesItsTicketAsTheADRSays(t *testing.T) {
	ctx, tx := opTx(t)
	findings, names, err := opReadConsumptionFindings(ctx, tx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	if !slices.Contains(names, "op_read_legal_versions") {
		t.Fatalf("anti-vacuity: the scan read %v, not op_read_legal_versions", names)
	}

	var def string
	if err := tx.QueryRow(ctx, `SELECT pg_get_functiondef('public.op_read_legal_versions(text, text, integer, integer)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatalf("read the definition: %v", err)
	}
	def = strings.Replace(def, "public.op_read_legal_versions(", "public.op_read_zz_probe(", 1)
	controls := []struct{ name, old, new, want string }{
		{"unmodified copy", "", "", ""},
		{"the commit condition removed", "\n       AND pg_xact_status(k.created_xact) = 'committed';", ";", "COMMITTED"},
		{"the commit condition as created_xact <> pg_current_xact_id()", "pg_xact_status(k.created_xact) = 'committed'", "k.created_xact <> pg_current_xact_id()", "COMMITTED"},
		{"the commit condition negated into an IF", "\n       AND pg_xact_status(k.created_xact) = 'committed';", ";\n    IF pg_xact_status(k.created_xact) <> 'committed' THEN RAISE EXCEPTION 'x'; END IF;", "COMMITTED"},
		{"the kind condition removed", "\n       AND k.kind = 'legal_versions'", "", "this read kind"},
		{"a kind outside the closed set", "k.kind = 'legal_versions'", "k.kind = 'tenant_detail'", "does not hold"},
		{"the consumed condition removed", "\n       AND k.consumed_at IS NULL", "", "not yet consumed"},
		{"the expiry condition removed", "\n       AND k.expires_at > clock_timestamp()", "", "not expired"},
		{"the expiry on a frozen clock", "k.expires_at > clock_timestamp()", "k.expires_at > now()", "not expired"},
		{"the session condition removed", "\n       AND k.session_id = v_session", "", "this session"},
		{"the parameters out of the hash", "p_ticket || jsonb_build_object('page_number', p_page_number,\n                                              'page_size', p_page_size)::text,", "p_ticket,", "the hash"},
		{"the refusal removed", "IF NOT FOUND THEN", "IF false THEN", "IF NOT FOUND"},
	}
	for _, c := range controls {
		t.Run("control/"+c.name, func(t *testing.T) {
			probe := def
			if c.old != "" {
				if strings.Count(probe, c.old) != 1 {
					t.Fatalf("the control's anchor occurs %d times in the definition", strings.Count(probe, c.old))
				}
				probe = strings.Replace(probe, c.old, c.new, 1)
			}
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatalf("savepoint: %v", err)
			}
			defer func() {
				if err := sp.Rollback(ctx); err != nil {
					t.Fatalf("rollback probe: %v", err)
				}
			}()
			if _, err := sp.Exec(ctx, probe); err != nil {
				t.Fatalf("create probe: %v", err)
			}
			got, _, err := opReadConsumptionFindings(ctx, sp)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			var hits []string
			for _, f := range got {
				if strings.Contains(f, "op_read_zz_probe") {
					hits = append(hits, f)
				}
			}
			switch {
			case c.want == "" && len(hits) != 0:
				t.Fatalf("the unmodified copy is flagged: %v", hits)
			case c.want != "" && !slices.ContainsFunc(hits, func(h string) bool { return strings.Contains(h, c.want) }):
				t.Fatalf("the scan did not flag %q (want a finding containing %q); findings: %v", c.name, c.want, hits)
			}
		})
	}
}

// opTicketDraw is the one statement, in normalised form, that may give op_begin_read's
// raw ticket its value: SHA-256 over three uuid_send(pg_catalog.gen_random_uuid()) --
// 366 bits from the server's strong random source (00027 §4.1).
const opTicketDraw = `v_ticket := encode(sha256(uuid_send(pg_catalog.gen_random_uuid()) || uuid_send(pg_catalog.gen_random_uuid()) || uuid_send(pg_catalog.gen_random_uuid())), 'hex');`

// opTicketSourceFindings reads one function's source (by regprocedure) and returns a
// line per breach of the ticket's provenance: v_ticket is assigned exactly once, by
// opTicketDraw; the stored hash is computed from it; and it is what the function returns.
func opTicketSourceFindings(ctx context.Context, q opQuerier, fn string) ([]string, error) {
	var src string
	if err := q.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = $1::regprocedure`, fn).Scan(&src); err != nil {
		return nil, err
	}
	src = opNormalisedSource(src)
	var out []string
	if n := strings.Count(src, "v_ticket :="); n != 1 {
		out = append(out, fmt.Sprintf("v_ticket is assigned %d times, want once", n))
	}
	if !strings.Contains(src, opTicketDraw) {
		out = append(out, "v_ticket is not drawn as sha256 over three uuid_send(pg_catalog.gen_random_uuid())")
	}
	if !strings.Contains(src, "encode(sha256(convert_to(v_ticket || v_bound::text, 'utf8')), 'hex')") {
		out = append(out, "the stored hash is not sha256(v_ticket || v_bound::text)")
	}
	if !strings.Contains(src, "return v_ticket;") {
		out = append(out, "the function does not return v_ticket")
	}
	return out, nil
}

// TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource pins where the raw ticket
// comes from. ADR 0021 §2 v 1's argument that the stored hash says nothing about a later
// read's search term rests on a 256-bit RANDOM ticket, and the behaviour tests cannot
// see the source: a review measured a ticket derived from the clock and the session
// (encode(sha256(convert_to(clock_timestamp()::text || p_session, ...)))) passing every
// one of them -- it is 64 hex characters and two calls differ. So the source is pinned:
// opTicketSourceFindings' rules on op_begin_read, and pg_catalog.gen_random_uuid() is
// the core function (LANGUAGE internal; PostgreSQL documents it as drawn from its
// cryptographically strong source). POSITIVE CONTROL: that clock-derived ticket, in a
// copy of op_begin_read created inside a savepoint, is flagged.
func TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource(t *testing.T) {
	ctx, tx := opTx(t)
	findings, err := opTicketSourceFindings(ctx, tx, "public.op_begin_read(text, text, jsonb)")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Error("op_begin_read: " + f)
	}
	var lang string
	if err := tx.QueryRow(ctx, `SELECT l.lanname FROM pg_proc p JOIN pg_language l ON l.oid = p.prolang
	                            WHERE p.oid = 'pg_catalog.gen_random_uuid()'::regprocedure`).Scan(&lang); err != nil || lang != "internal" {
		t.Errorf("pg_catalog.gen_random_uuid() is LANGUAGE %q (%v), want the core internal function", lang, err)
	}

	var def string
	if err := tx.QueryRow(ctx, `SELECT pg_get_functiondef('public.op_begin_read(text, text, jsonb)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatalf("read the definition: %v", err)
	}
	const draw = `    v_ticket := encode(sha256(uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())), 'hex');`
	if strings.Count(def, draw) != 1 {
		t.Fatalf("the control's anchor occurs %d times in op_begin_read's definition", strings.Count(def, draw))
	}
	probe := strings.Replace(def, "public.op_begin_read(", "public.op_zz_ticket_probe(", 1)
	probe = strings.Replace(probe, draw, `    v_ticket := encode(sha256(convert_to(clock_timestamp()::text || p_session, 'UTF8')), 'hex');`, 1)
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	defer func() {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback probe: %v", err)
		}
	}()
	if _, err := sp.Exec(ctx, probe); err != nil {
		t.Fatalf("create probe: %v", err)
	}
	got, err := opTicketSourceFindings(ctx, sp, "public.op_zz_ticket_probe(text, text, jsonb)")
	if err != nil {
		t.Fatalf("scan the probe: %v", err)
	}
	if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, "gen_random_uuid") }) {
		t.Fatalf("CONTROL FAILED: a clock-derived ticket is not flagged: %v", got)
	}
}

// ------------------------------------------------------------ op_begin_read --

// TestOpBeginRead_WritesOneAuditRowAndStoresNoTicket: an accepted first phase writes
// exactly one audit row (kind 'read', the session and its operator, the scope, the page
// as plain integers -- nothing else) and one ticket bound to it; the ticket is 64 hex
// characters, is stored nowhere, and the stored hash is SHA-256 over the raw ticket and
// the canonical parameter text; its lifetime is 30 seconds by the wall clock and its
// created_xact is this transaction's top-level id.
func TestOpBeginRead_WritesOneAuditRowAndStoresNoTicket(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	audit0 := opAudit(t, ctx, tx)

	raw, err := opBegin(t, ctx, tx, hash, legalVersionsReadKind, opLegalParams(2, 10))
	if err != nil {
		t.Fatalf("op_begin_read: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(raw) {
		t.Fatalf("the ticket is not 64 lower-case hex characters (%d characters)", len(raw))
	}
	if d := opAudit(t, ctx, tx) - audit0; d != 1 {
		t.Fatalf("op_begin_read wrote %d audit rows, want 1", d)
	}
	var (
		auditID, actor             uuid.UUID
		kind, scope, detail        string
		pageNumber, pageSize       int
		targetTenant, targetAdmin  *uuid.UUID
		ticketAudit, ticketSession uuid.UUID
		ticketKind, ticketHash     string
		lifetime                   float64
		ownXact, consumed          bool
		auditText, ticketText      string
	)
	if err := tx.QueryRow(ctx, `
		SELECT l.id, l.actor_admin_id, l.kind, l.target_scope, l.detail::text, l.page_number, l.page_size,
		       l.target_tenant_id, l.target_admin_id, row_to_json(l)::text
		  FROM operator_audit_log l WHERE l.session_id = $1`, session).
		Scan(&auditID, &actor, &kind, &scope, &detail, &pageNumber, &pageSize, &targetTenant, &targetAdmin, &auditText); err != nil {
		t.Fatalf("read the audit row: %v", err)
	}
	if actor != a.id || kind != "read" || scope != legalVersionsReadKind || detail != "{}" || pageNumber != 2 || pageSize != 10 ||
		targetTenant != nil || targetAdmin != nil {
		t.Errorf("audit row: actor ok=%v kind=%s scope=%s detail=%s page=%d/%d tenant=%v admin=%v; want the operator, read, legal_versions, {}, 2/10, no targets",
			actor == a.id, kind, scope, detail, pageNumber, pageSize, targetTenant, targetAdmin)
	}
	if err := tx.QueryRow(ctx, `
		SELECT k.audit_id, k.session_id, k.kind, k.ticket_hash,
		       extract(epoch FROM (k.expires_at - k.created_at))::float8,
		       k.created_xact = pg_current_xact_id(), k.consumed_at IS NOT NULL, row_to_json(k)::text
		  FROM operator_read_tickets k WHERE k.session_id = $1`, session).
		Scan(&ticketAudit, &ticketSession, &ticketKind, &ticketHash, &lifetime, &ownXact, &consumed, &ticketText); err != nil {
		t.Fatalf("read the ticket row: %v", err)
	}
	if ticketAudit != auditID || ticketSession != session || ticketKind != legalVersionsReadKind || consumed {
		t.Errorf("ticket row: bound to its audit row=%v, session=%v, kind=%s, consumed=%v", ticketAudit == auditID, ticketSession == session, ticketKind, consumed)
	}
	if ticketHash != opTicketHash(raw, 2, 10) {
		t.Error("the stored hash is not sha256(raw ticket || canonical parameters)")
	}
	if strings.Contains(ticketText, raw) || strings.Contains(auditText, raw) || strings.Contains(auditText, ticketHash) {
		t.Error("the raw ticket (or its hash, in the audit row) is stored")
	}
	if lifetime < 29.9 || lifetime > 30.1 {
		t.Errorf("the ticket lives %.3f s, want 30 (strictly below 00026's 60-second ceiling)", lifetime)
	}
	if !ownXact {
		t.Error("created_xact is not this transaction's top-level id")
	}
	// Two calls, two tickets.
	raw2, err := opBegin(t, ctx, tx, hash, legalVersionsReadKind, opLegalParams(2, 10))
	if err != nil || raw2 == raw {
		t.Errorf("a second op_begin_read: err=%v, same ticket=%v", err, raw2 == raw)
	}
}

// TestOpBeginRead_RefusesDeadSessionsAndBadParameters: the six dead sessions of
// opDeadSessions (ADR 0021 §6's table) are refused by the session predicate (28000),
// and the 18 kind/parameter cases listed below -- another or a NULL kind, a parameter
// object that is NULL, an array, a string, empty, short of a key or carrying an extra
// one, and page values outside "plain positive integer, size at most 200" -- are refused
// with 22023; none of these 24 calls writes a row, and no error field carries what was
// sent. The controls are three parameter objects at and inside the bounds, accepted.
func TestOpBeginRead_RefusesDeadSessionsAndBadParameters(t *testing.T) {
	ctx, tx := opTx(t)
	count := func() int64 {
		return opInt(t, ctx, tx, `SELECT (SELECT count(*) FROM operator_audit_log) + (SELECT count(*) FROM operator_read_tickets)`)
	}
	for _, d := range opDeadSessions(t, ctx, tx) {
		before := count()
		_, err := opBegin(t, ctx, tx, d.hash, legalVersionsReadKind, opLegalParams(1, 10))
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read, "+d.name, d.hash)
		if n := count() - before; n != 0 {
			t.Errorf("op_begin_read, %s: %d row(s) written", d.name, n)
		}
	}
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	for _, c := range []struct {
		name   string
		kind   any
		params any
	}{
		{"another kind", "tenants", opLegalParams(1, 10)},
		{"NULL kind", nil, opLegalParams(1, 10)},
		{"NULL parameters", legalVersionsReadKind, nil},
		{"an array", legalVersionsReadKind, `[1, 10]`},
		{"a string", legalVersionsReadKind, `"page 1"`},
		{"an empty object", legalVersionsReadKind, `{}`},
		{"page_size missing", legalVersionsReadKind, `{"page_number": 1}`},
		{"an extra key", legalVersionsReadKind, `{"page_number": 1, "page_size": 10, "term": "x"}`},
		{"page_number as a string", legalVersionsReadKind, `{"page_number": "1", "page_size": 10}`},
		{"page_number 1.5", legalVersionsReadKind, `{"page_number": 1.5, "page_size": 10}`},
		{"page_number 1.0", legalVersionsReadKind, `{"page_number": 1.0, "page_size": 10}`},
		{"page_number 0", legalVersionsReadKind, opLegalParams(0, 10)},
		{"page_number -1", legalVersionsReadKind, opLegalParams(-1, 10)},
		{"page_number 10 digits", legalVersionsReadKind, opLegalParams(1000000000, 10)},
		{"page_size 0", legalVersionsReadKind, opLegalParams(1, 0)},
		{"page_size 201", legalVersionsReadKind, opLegalParams(1, 201)},
		{"page_size 999", legalVersionsReadKind, opLegalParams(1, 999)},
		{"page_size null", legalVersionsReadKind, `{"page_number": 1, "page_size": null}`},
	} {
		before := count()
		_, err := opBegin(t, ctx, tx, hash, c.kind, c.params)
		values := []string{hash}
		if s, ok := c.params.(string); ok && len(s) > 2 {
			values = append(values, s)
		}
		opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "op_begin_read, "+c.name, values...)
		if n := count() - before; n != 0 {
			t.Errorf("op_begin_read, %s: %d row(s) written", c.name, n)
		}
	}
	for _, p := range []string{opLegalParams(1, 200), opLegalParams(999999999, 1), `{"page_size": 7, "page_number": 3}`} {
		before := count()
		if _, err := opBegin(t, ctx, tx, hash, legalVersionsReadKind, p); err != nil {
			t.Errorf("CONTROL: op_begin_read refused %s: %v", p, err)
		}
		if n := count() - before; n != 2 {
			t.Errorf("CONTROL: op_begin_read with %s wrote %d rows, want 2 (audit + ticket)", p, n)
		}
	}
}

// TestOpBeginRead_ARefusedWriteCarriesNoTicketHash: a constraint the ticket's own row
// meets (forced here with a CHECK added inside the transaction) is answered with the
// function's fixed refusal and no DETAIL -- an uncaught violation's DETAIL is the failing
// row, and that row holds the ticket hash (never logged, raw or hashed: ADR 0021 §3.5) --
// and the audit row goes with it.
func TestOpBeginRead_ARefusedWriteCarriesNoTicketHash(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	if _, err := tx.Exec(ctx, `ALTER TABLE operator_read_tickets ADD CONSTRAINT zz_op10_refuse CHECK (kind <> 'legal_versions') NOT VALID`); err != nil {
		t.Fatalf("force a refusal: %v", err)
	}
	_, err := opBegin(t, ctx, tx, hash, legalVersionsReadKind, opLegalParams(1, 10))
	opWantClean(t, err, sqlstateInvalidAuthorization, beginRefusal, "op_begin_read whose ticket row a constraint refuses")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1`, session); n != 0 {
		t.Errorf("the refused first phase left %d audit row(s)", n)
	}
}

// ---------------------------------------------------- op_read_legal_versions --

// TestOpReadLegalVersions_ATicketFromThisTransactionIsRefused is ADR 0021 §2 v 3's
// table, rows A1 and A2: a ticket created in THIS transaction -- at the top level, or
// inside a savepoint (released, or still open) -- is refused, because its created_xact is
// the top-level id and that transaction has not committed. CONTROL: the same session,
// kind and page through a ticket whose created_xact names a committed transaction is
// read -- so the refusal is the commit condition and nothing else.
func TestOpReadLegalVersions_ATicketFromThisTransactionIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)

	top, err := opBegin(t, ctx, tx, hash, legalVersionsReadKind, opLegalParams(1, 5))
	if err != nil {
		t.Fatalf("op_begin_read: %v", err)
	}
	_, err = opRead(t, ctx, tx, hash, top, 1, 5)
	opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, "A1: a ticket created at this transaction's top level", top)

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	inner, err := opBegin(t, ctx, sp, hash, legalVersionsReadKind, opLegalParams(1, 5))
	if err != nil {
		t.Fatalf("op_begin_read in a savepoint: %v", err)
	}
	_, err = opRead(t, ctx, sp, hash, inner, 1, 5)
	opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, "A2: a ticket created and read in the same open savepoint", inner)
	if err := sp.Commit(ctx); err != nil {
		t.Fatalf("release savepoint: %v", err)
	}
	_, err = opRead(t, ctx, tx, hash, inner, 1, 5)
	opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, "A2: a ticket created in a released savepoint", inner)

	var topLevel, inProgress int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE created_xact = pg_current_xact_id()),
	                                   count(*) FILTER (WHERE pg_xact_status(created_xact) = 'in progress')
	                              FROM operator_read_tickets WHERE session_id = $1`, session).Scan(&topLevel, &inProgress); err != nil {
		t.Fatal(err)
	}
	if topLevel != 2 || inProgress != 2 {
		t.Errorf("of the two tickets, %d carry the top-level id and %d read 'in progress', want 2 and 2", topLevel, inProgress)
	}

	raw, _ := opForgeTicket(t, ctx, tx, session, a.id, 1, 5, opCommittedXact(t, ctx), "30 seconds")
	if _, err := opRead(t, ctx, tx, hash, raw, 1, 5); err != nil {
		t.Fatalf("CONTROL: a ticket whose transaction COMMITTED was refused: %v", err)
	}
}

// TestOpReadLegalVersions_TwoPhaseLifecycle is the rest of ADR 0021 §2 v 3's table on
// the shipped functions, with REAL commits (the file header says what stays):
// phase one is committed (B1) and its audit row is permanent; a different page or a
// different session is refused; the read returns the rows; a read transaction that is
// ROLLED BACK leaves the audit row and, as ADR 0021 limit 4 records, lets the same ticket
// read again inside its lifetime; once a read has COMMITTED the ticket is refused; a dead
// session is refused; and through all of it there is exactly ONE audit row.
func TestOpReadLegalVersions_TwoPhaseLifecycle(t *testing.T) {
	ctx, f := opLiveFixture(t)
	otherHash, otherSession := f.newSession(t, ctx)
	conn := f.connect(t, ctx)

	tx := asOperatorTx(t, ctx, conn)
	var raw string
	if err := tx.QueryRow(ctx, beginOperatorReadSQL, f.hash, legalVersionsReadKind, opLegalParams(1, 4)).Scan(&raw); err != nil {
		t.Fatalf("phase one: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit phase one: %v", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 1 {
		t.Fatalf("after the committed phase one the session has %d 'read' row(s), want 1", n)
	}

	refused := func(what string, hash string, number, size int32) {
		t.Helper()
		rtx := asOperatorTx(t, ctx, conn)
		_, err := opScanVersions(ctx, rtx, hash, raw, number, size)
		opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, what, raw)
		if err := rtx.Rollback(ctx); err != nil {
			t.Fatalf("rollback: %v", err)
		}
	}
	refused("the ticket with another page_number", f.hash, 2, 4)
	refused("the ticket with another page_size", f.hash, 1, 5)
	refused("the ticket presented by another session of the same operator", otherHash, 1, 4)

	// A dead session: revoked inside the read's own transaction (rolled back).
	{
		rtx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rtx.Exec(ctx, `UPDATE platform_sessions SET revoked_at = clock_timestamp() WHERE id = $1`, f.session); err != nil {
			t.Fatal(err)
		}
		if _, err := rtx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
			t.Fatal(err)
		}
		_, err = opScanVersions(ctx, rtx, f.hash, raw, 1, 4)
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "the ticket presented by a revoked session", raw)
		if err := rtx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// THE READ: four versions written inside the read's transaction (rolled back), newest
	// first: an operator publication, a legacy row with a customer admin's id, a legacy
	// row with none, and a second operator publication of the same slug.
	read := func(commit bool) []LegalVersion {
		t.Helper()
		rtx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rtx.Rollback(context.Background()) }()
		if !commit {
			for _, s := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO legal_documents (slug, body, published_by) VALUES ('terms', 'FAKE legacy terms', $1)`, []any{uuid.New()}},
				{`INSERT INTO legal_documents (slug, body) VALUES ('imprint', 'FAKE legacy imprint')`, nil},
			} {
				if _, err := rtx.Exec(ctx, s.sql, s.args...); err != nil {
					t.Fatalf("legacy fixture: %v", err)
				}
			}
		}
		if _, err := rtx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
			t.Fatal(err)
		}
		if !commit {
			for _, body := range []string{"FAKE privacy v1", "FAKE privacy v2, longer"} {
				if _, err := rtx.Exec(ctx, publishLegalSQL, f.hash, "privacy", body); err != nil {
					t.Fatalf("publish inside the read transaction: %v", err)
				}
			}
		}
		vs, err := opScanVersions(ctx, rtx, f.hash, raw, 1, 4)
		if err != nil {
			t.Fatalf("the read (commit=%v): %v", commit, err)
		}
		if commit {
			if err := rtx.Commit(ctx); err != nil {
				t.Fatalf("commit the read: %v", err)
			}
		}
		return vs
	}
	vs := read(false)
	want := []struct {
		slug    string
		kind    LegalPublisherKind
		named   bool
		bytes   int32
		current bool
	}{
		{"privacy", LegalPublishedByOperator, true, int32(len("FAKE privacy v2, longer")), true},
		{"privacy", LegalPublishedByOperator, true, int32(len("FAKE privacy v1")), false},
		{"imprint", LegalPublishedByLegacy, false, int32(len("FAKE legacy imprint")), true},
		{"terms", LegalPublishedByLegacy, false, int32(len("FAKE legacy terms")), true},
	}
	if len(vs) != len(want) {
		t.Fatalf("the read returned %d rows, want %d", len(vs), len(want))
	}
	for i, w := range want {
		v := vs[i]
		named := v.PublisherID != nil && *v.PublisherID == f.admin && v.PublisherName != nil && *v.PublisherName == f.name
		anonymous := v.PublisherID == nil && v.PublisherName == nil
		if v.Slug != w.slug || v.PublishedBy != w.kind || v.BodyBytes != w.bytes || v.Current != w.current ||
			(w.named && !named) || (!w.named && !anonymous) {
			t.Errorf("row %d = {%s %s bytes=%d current=%v named=%v anonymous=%v}, want {%s %s bytes=%d current=%v named=%v}",
				i, v.Slug, v.PublishedBy, v.BodyBytes, v.Current, named, anonymous, w.slug, w.kind, w.bytes, w.current, w.named)
		}
	}
	if n := f.liveReads(t, ctx, f.session); n != 1 {
		t.Errorf("after a rolled-back read the session has %d 'read' row(s), want the 1 committed by phase one", n)
	}
	// ADR 0021 limit 4, measured: the rolled-back read un-consumed the ticket.
	read(true)
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, f.session); n != 1 {
		t.Errorf("after a committed read %d ticket(s) of the session are consumed, want 1", n)
	}
	refused("the ticket after a COMMITTED read consumed it", f.hash, 1, 4)
	if n := f.liveReads(t, ctx, f.session); n != 1 {
		t.Errorf("at the end the session has %d 'read' row(s), want exactly 1 (one per accepted read, written by phase one)", n)
	}
	if n := f.liveReads(t, ctx, otherSession); n != 0 {
		t.Errorf("the other session has %d 'read' row(s)", n)
	}
}

// TestOpReadLegalVersions_AnUncommittedTicketIsInvisibleToAnotherSession is row D of ADR
// 0021 §2 v 3's table and what it means for the read. While phase one's transaction (A)
// is open, its ticket row is invisible to another connection; and a read that presents
// the ticket from another connection with the SAME operator session (B) does not return
// data -- measured: B waits on a lock (op_touch_session's UPDATE of the session row,
// which A's phase one holds) and, once A rolls back, is refused. (The first version of
// this test expected an immediate refusal and hung for its whole deadline: that wait is
// the session predicate's row lock, which 00026 says serialises calls on one session.)
// Nothing is committed, so the cleanup removes the account and the session too.
func TestOpReadLegalVersions_AnUncommittedTicketIsInvisibleToAnotherSession(t *testing.T) {
	ctx, f := opLiveFixture(t)
	a, b, c := f.connect(t, ctx), f.connect(t, ctx), f.connect(t, ctx)
	txA := asOperatorTx(t, ctx, a)
	var raw string
	if err := txA.QueryRow(ctx, beginOperatorReadSQL, f.hash, legalVersionsReadKind, opLegalParams(1, 5)).Scan(&raw); err != nil {
		t.Fatalf("phase one (uncommitted): %v", err)
	}
	// The row: 1 in A's own transaction, 0 from a third connection (the owner's, so no
	// privilege or RLS stands between it and the table).
	if _, err := txA.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION DEFAULT`); err != nil {
		t.Fatal(err)
	}
	if n := opInt(t, ctx, txA, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1`, f.session); n != 1 {
		t.Fatalf("CONTROL: A's own transaction sees %d ticket(s) of the session, want 1", n)
	}
	if n := opInt(t, ctx, c, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1`, f.session); n != 0 {
		t.Errorf("another connection sees %d uncommitted ticket(s), want 0", n)
	}

	// The read from B: it must not return data while A is open.
	var bPID int32
	if err := b.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&bPID); err != nil {
		t.Fatal(err)
	}
	txB := asOperatorTx(t, ctx, b)
	done := make(chan error, 1)
	go func() {
		_, err := opScanVersions(ctx, txB, f.hash, raw, 1, 5)
		done <- err
	}()
	waiting := false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && !waiting; {
		select {
		case err := <-done:
			t.Fatalf("B's read returned while A's transaction was open (%v); it should wait on the session row", err)
		default:
		}
		var wait *string
		if err := c.QueryRow(ctx, `SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1`, bPID).Scan(&wait); err != nil {
			t.Fatal(err)
		}
		waiting = wait != nil && *wait == "Lock"
		if !waiting {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !waiting {
		t.Fatal("B's read is not waiting on a lock while A's transaction is open")
	}
	if err := txA.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, "the ticket of a transaction that was rolled back, from another connection", raw)
	case <-time.After(30 * time.Second):
		t.Fatal("B's read did not return after A rolled back")
	}
	if err := txB.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 0 {
		t.Errorf("%d 'read' row(s) were committed; phase one was rolled back", n)
	}
}

// TestOpReadLegalVersions_ExpiryIsTheWallClock is ADR 0021 §6 "bilet süresi": an expired
// ticket is refused, and a ticket that expires WHILE the reading transaction is open is
// refused -- through savepoints rolled back three times, and through three exception
// sub-transactions of ONE DO statement whose sleep is INSIDE it (so that
// statement_timestamp(), frozen at the DO's start, would still call the ticket alive).
// The ticket's transaction is a committed one (opForgeTicket); its expiry is written,
// not waited for, except for the 1.5 s the open-transaction cases are about.
// CONTROL first: the same ticket, unexpired, is read (in a savepoint that is rolled back,
// so it stays unconsumed).
func TestOpReadLegalVersions_ExpiryIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	raw, ticketID := opForgeTicket(t, ctx, tx, session, a.id, 1, 5, opCommittedXact(t, ctx), "30 seconds")
	expireIn := func(interval string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `UPDATE operator_read_tickets SET expires_at = clock_timestamp() + $2::interval WHERE id = $1`, ticketID, interval); err != nil {
			t.Fatalf("set the expiry: %v", err)
		}
	}
	readThenUndo := func() error {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := opRead(t, ctx, sp, hash, raw, 1, 5)
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return readErr
	}
	if err := readThenUndo(); err != nil {
		t.Fatalf("CONTROL: the unexpired ticket was refused: %v", err)
	}

	expireIn("-1 second")
	opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, readRefusal, "an expired ticket", raw)

	// Savepoints: the ticket expires 1 s from now, the transaction sleeps 1.5 s.
	expireIn("1 second")
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		opWantClean(t, readThenUndo(), sqlstateInvalidAuthorization, readRefusal,
			fmt.Sprintf("savepoint %d: a ticket that expired while the transaction was open", i+1), raw)
	}

	// One DO statement, the sleep inside it.
	expireIn("1 second")
	var result string
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('tappa_test.session', $1, true), set_config('tappa_test.ticket', $2, true)`, hash, raw); err != nil {
			return err
		}
		if _, err := sp.Exec(ctx, `
			DO $d$
			DECLARE
			    n_data    integer := 0;
			    n_refused integer := 0;
			BEGIN
			    PERFORM pg_sleep(1.5);
			    FOR i IN 1..3 LOOP
			        BEGIN
			            PERFORM * FROM public.op_read_legal_versions(current_setting('tappa_test.session'),
			                                                         current_setting('tappa_test.ticket'), 1, 5);
			            n_data := n_data + 1;
			        EXCEPTION WHEN invalid_authorization_specification THEN
			            n_refused := n_refused + 1;
			        END;
			    END LOOP;
			    PERFORM set_config('tappa_test.result', n_data || '/' || n_refused, true);
			END
			$d$`); err != nil {
			return err
		}
		return sp.QueryRow(ctx, `SELECT current_setting('tappa_test.result')`).Scan(&result)
	}); err != nil {
		t.Fatalf("the DO block: %v", err)
	}
	if result != "0/3" {
		t.Errorf("inside one DO statement whose sleep outlived the ticket: data/refused = %s, want 0/3 (a frozen clock reads the ticket alive)", result)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE id = $1 AND consumed_at IS NOT NULL`, ticketID); n != 0 {
		t.Error("an expired ticket was consumed")
	}
}

// TestOpReadLegalVersions_AForgedTicketIsRefused is ADR 0021 §6 "bilet sahteciliği" for
// the read that now exists: tappa_opdefiner cannot write a ticket's created_xact or
// created_at (42501); the owner, who can, cannot write '1' or '2' (the CHECK); and WHY
// those two layers are what stands: with the CHECK dropped inside this transaction an
// owner-written created_xact = '2' (pg_xact_status says 'committed') IS read in the same
// transaction, while created_xact = this transaction's own id is not. A ticket bound to
// another session is refused.
func TestOpReadLegalVersions_AForgedTicketIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	otherHash, _ := opNewSession(t, ctx, tx, a.id, true)
	var auditID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_scope)
	                            VALUES ('read', $1, $2, 'legal_versions') RETURNING id`, session, a.id).Scan(&auditID); err != nil {
		t.Fatal(err)
	}
	raw := opRandHex(t)
	insert := func(as, extraCol, extraVal string) error {
		t.Helper()
		sql := `INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at` + extraCol + `)
		        VALUES ($1, $2, 'legal_versions', $3, clock_timestamp() + interval '30 seconds'` + extraVal + `)`
		if as == "owner" {
			return opTry(t, ctx, tx, sql, opTicketHash(raw, 1, 5), session, auditID)
		}
		return opExecAs(t, ctx, tx, as, sql, opTicketHash(raw, 1, 5), session, auditID)
	}
	opWant(t, insert("tappa_opdefiner", ", created_xact", ", '2'::xid8"), sqlstateInsufficientPrivi, "the definer writing created_xact")
	opWant(t, insert("tappa_opdefiner", ", created_at", ", clock_timestamp() + interval '1 year'"), sqlstateInsufficientPrivi, "the definer writing created_at")
	opWant(t, insert("tappa_operator", "", ""), sqlstateInsufficientPrivi, "tappa_operator writing a ticket at all")
	opWantConstraint(t, insert("owner", ", created_xact", ", '2'::xid8"), "operator_read_tickets_xact_is_real", "the owner writing created_xact = 2")
	opWantConstraint(t, insert("owner", ", created_xact", ", '1'::xid8"), "operator_read_tickets_xact_is_real", "the owner writing created_xact = 1")

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_xact_is_real`); err != nil {
		t.Fatalf("drop the CHECK inside the transaction: %v", err)
	}
	if _, err := sp.Exec(ctx, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at, created_xact)
	                           VALUES ($1, $2, 'legal_versions', $3, clock_timestamp() + interval '30 seconds', pg_current_xact_id())`,
		opTicketHash(raw, 1, 5), session, auditID); err != nil {
		t.Fatalf("own-xid ticket: %v", err)
	}
	_, err = opRead(t, ctx, sp, hash, raw, 1, 5)
	opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, "a ticket naming this transaction's own id", raw)
	raw2 := opRandHex(t)
	if _, err := sp.Exec(ctx, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at, created_xact)
	                           VALUES ($1, $2, 'legal_versions', $3, clock_timestamp() + interval '30 seconds', '2'::xid8)`,
		opTicketHash(raw2, 1, 5), session, auditID); err != nil {
		t.Fatalf("xid-2 ticket: %v", err)
	}
	_, err = opRead(t, ctx, sp, otherHash, raw2, 1, 5)
	opWantClean(t, err, sqlstateInvalidAuthorization, readRefusal, "a ticket of another session", raw2)
	if _, err := opRead(t, ctx, sp, hash, raw2, 1, 5); err != nil {
		t.Errorf("WHY THE CHECK EXISTS: with it dropped, a ticket whose created_xact is '2' should be READ in the same transaction (pg_xact_status('2') = 'committed'), and the read failed: %v", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestOpReadLegalVersions_PagesAreCappedAndOrdered: ADR 0021 §2 iii's ceiling lives in
// the read's body, not only in op_begin_read -- a ticket for 300 rows (which op_begin_read
// would refuse, so the owner writes it) reads 200; OFFSET follows the page number; the
// order is newest first; body_bytes counts BYTES (a Maltese body with two-byte letters).
// 210 versions are written inside the transaction (rolled back). The accessor's own scan
// (readLegalVersions) is what reads them.
func TestOpReadLegalVersions_PagesAreCappedAndOrdered(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	if _, err := tx.Exec(ctx, `INSERT INTO legal_documents (slug, body)
	                           SELECT 'cookies', 'FAKE cookies version ' || g FROM generate_series(1, 210) AS g`); err != nil {
		t.Fatalf("210 versions: %v", err)
	}
	xact := opCommittedXact(t, ctx)
	read := func(number, size int) []LegalVersion {
		t.Helper()
		raw, _ := opForgeTicket(t, ctx, tx, session, a.id, number, size, xact, "30 seconds")
		var vs []LegalVersion
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			var e error
			vs, e = readLegalVersions(ctx, sp, hash, readTicket{v: &raw}, LegalVersionsPage{Number: int32(number), Size: int32(size)})
			return e
		}); err != nil {
			t.Fatalf("read page %d/%d: %v", number, size, err)
		}
		return vs
	}
	if got := len(read(1, 300)); got != 200 {
		t.Errorf("a 300-row ticket read %d rows, want the body's ceiling of 200", got)
	}
	first, second := read(1, 3), read(2, 3)
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("pages of 3 read %d and %d rows", len(first), len(second))
	}
	all := append(first, second...)
	for i := 1; i < len(all); i++ {
		if all[i].PublishedAt.After(all[i-1].PublishedAt) {
			t.Errorf("row %d is newer than row %d; the list is newest first", i, i-1)
		}
		if all[i].ID == all[i-1].ID {
			t.Errorf("rows %d and %d are the same version; page 2 does not start where page 1 ended", i-1, i)
		}
	}
	if !first[0].Current || first[1].Current || first[0].Slug != "cookies" {
		t.Errorf("the newest 'cookies' version should be the current one and the next not: %+v %+v", first[0], first[1])
	}

	// body_bytes is BYTES (octet_length), not characters: a Maltese text, whose ċ ġ ħ ż
	// are two UTF-8 bytes each, appended as the newest version.
	maltese := "FAKE: Ċertifikat ġdid għall-ħaddiema taż-żona."
	if len(maltese) == utf8.RuneCountInString(maltese) {
		t.Fatal("PREMISE: the Maltese body has as many bytes as characters")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO legal_documents (slug, body) VALUES ('imprint', $1)`, maltese); err != nil {
		t.Fatalf("the Maltese version: %v", err)
	}
	if got := read(1, 1); len(got) != 1 || got[0].BodyBytes != int32(len(maltese)) {
		t.Errorf("the Maltese version reads body_bytes %v, want %d bytes (%d characters)", got, len(maltese), utf8.RuneCountInString(maltese))
	}
}

// ---------------------------------------------------------- op_publish_legal --

// TestOpPublishLegal_WritesTheVersionAndItsAuditRowTogether: one call writes one
// version whose published_by is the SESSION's operator and whose published_at is the
// wall clock (the transaction slept 1.5 s first; a DEFAULT now() would back-date it),
// and one 'legal_publish' audit row naming the session, the operator and -- in detail --
// the slug, the new row's id and its byte length, not the text. A second operator's
// session publishes under that operator.
func TestOpPublishLegal_WritesTheVersionAndItsAuditRowTogether(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := opNewActive(t, ctx, tx), opNewActive(t, ctx, tx)
	hashA, sessionA := opNewSession(t, ctx, tx, a.id, true)
	hashB, _ := opNewSession(t, ctx, tx, b.id, true)
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatal(err)
	}
	body := "FAKE privacy policy written by a test, " + uuid.NewString()
	audit0 := opAudit(t, ctx, tx)
	if err := opPublish(t, ctx, tx, hashA, "privacy", body); err != nil {
		t.Fatalf("op_publish_legal: %v", err)
	}
	if d := opAudit(t, ctx, tx) - audit0; d != 1 {
		t.Errorf("one publication wrote %d audit rows, want 1", d)
	}
	var (
		docID, publishedBy   uuid.UUID
		slug, stored         string
		afterStart, behind   float64
		kind, detail         string
		auditSession, actor  uuid.UUID
		scope                *string
		pageNumber, pageSize *int
	)
	if err := tx.QueryRow(ctx, `SELECT id, slug, body, published_by,
	                                   extract(epoch FROM (published_at - transaction_timestamp()))::float8,
	                                   extract(epoch FROM (clock_timestamp() - published_at))::float8
	                              FROM legal_documents WHERE body = $1`, body).
		Scan(&docID, &slug, &stored, &publishedBy, &afterStart, &behind); err != nil {
		t.Fatalf("read the version: %v", err)
	}
	if slug != "privacy" || stored != body || publishedBy != a.id {
		t.Errorf("version: slug=%s body as typed=%v published_by is the session's operator=%v", slug, stored == body, publishedBy == a.id)
	}
	if afterStart < 1.4 || behind < 0 || behind > 30 {
		t.Errorf("published_at is %.3f s after the transaction start and %.3f s behind the wall clock; written from the wall clock it is >= 1.4 s after a start that slept 1.5 s", afterStart, behind)
	}
	if err := tx.QueryRow(ctx, `SELECT kind, session_id, actor_admin_id, detail::text, target_scope, page_number, page_size
	                              FROM operator_audit_log WHERE session_id = $1 AND kind = 'legal_publish'`, sessionA).
		Scan(&kind, &auditSession, &actor, &detail, &scope, &pageNumber, &pageSize); err != nil {
		t.Fatalf("read the audit row: %v", err)
	}
	wantDetail := fmt.Sprintf(`{"slug": "privacy", "bytes": %d, "document_id": "%s"}`, len(body), docID)
	if actor != a.id || detail != wantDetail || scope != nil || pageNumber != nil || pageSize != nil {
		t.Errorf("audit row: actor is the operator=%v detail=%s (want %s) scope=%v page=%v/%v", actor == a.id, detail, wantDetail, scope, pageNumber, pageSize)
	}
	if strings.Contains(detail, body) {
		t.Error("the audit detail carries the text")
	}
	if err := opPublish(t, ctx, tx, hashB, "terms", "FAKE terms by the second operator"); err != nil {
		t.Fatalf("op_publish_legal by the second operator: %v", err)
	}
	if got := opInt(t, ctx, tx, `SELECT count(*) FROM legal_documents WHERE slug = 'terms' AND published_by = $1`, b.id); got != 1 {
		t.Errorf("the second operator's version carries published_by = that operator %d time(s), want 1", got)
	}
}

// TestOpPublishLegal_AFailedAuditRowTakesTheVersionWithIt: the version and its audit row
// are one statement, so when the audit row cannot be written the version is not there
// either -- forced twice inside savepoints: the definer's INSERT on the log revoked
// (42501, uncaught) and a CHECK refusing the kind (23514, caught: the function's fixed
// refusal). CONTROL first: the same call publishes.
func TestOpPublishLegal_AFailedAuditRowTakesTheVersionWithIt(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	versions := func(q opQuerier) int64 {
		return opInt(t, ctx, q, `SELECT count(*) FROM legal_documents WHERE published_by = $1`, a.id)
	}
	if err := opPublish(t, ctx, tx, hash, "privacy", "FAKE control text"); err != nil {
		t.Fatalf("CONTROL: %v", err)
	}
	for _, c := range []struct {
		what, ddl, code string
	}{
		{"the definer may not write the log", `REVOKE INSERT ON operator_audit_log FROM tappa_opdefiner`, sqlstateInsufficientPrivi},
		{"a CHECK refuses the audit row", `ALTER TABLE operator_audit_log ADD CONSTRAINT zz_op10_no_publish CHECK (kind <> 'legal_publish') NOT VALID`, sqlstateInvalidParameter},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		before := versions(sp)
		if _, err := sp.Exec(ctx, c.ddl); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		opWant(t, opPublish(t, ctx, sp, hash, "privacy", "FAKE text whose audit row fails"), c.code, c.what)
		if d := versions(sp) - before; d != 0 {
			t.Errorf("%s: the version was written without its audit row (%d)", c.what, d)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestOpPublishLegal_RefusesEveryDeadSession: ADR 0021 §6's dead-session table, through
// the publication: 28000 (the session predicate's own message), no version, no row.
func TestOpPublishLegal_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	count := func() int64 {
		return opInt(t, ctx, tx, `SELECT (SELECT count(*) FROM legal_documents) + (SELECT count(*) FROM operator_audit_log)`)
	}
	for _, d := range opDeadSessions(t, ctx, tx) {
		before := count()
		opWantClean(t, opPublish(t, ctx, tx, d.hash, "privacy", "FAKE text from a dead session"),
			sqlstateInvalidAuthorization, touchRefusal00026, "op_publish_legal, "+d.name, d.hash)
		if n := count() - before; n != 0 {
			t.Errorf("op_publish_legal, %s: %d row(s) written", d.name, n)
		}
	}
}

// TestOpPublishLegal_RefusesADocumentWithoutEchoingIt: a slug outside 00020's closed set,
// a NULL slug, a NULL body, a body with no visible character (spaces, and newlines and
// tabs, which 00020's btrim CHECK lets through) and a body over 256 KiB (counted in
// BYTES -- the multi-byte case is 262 145 bytes in far fewer characters) are one fixed
// 22023 with no DETAIL and nothing of the argument in any field, and nothing written.
// CONTROLS: exactly 262 144 bytes, ASCII and multi-byte, are published.
func TestOpPublishLegal_RefusesADocumentWithoutEchoingIt(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	count := func() int64 {
		return opInt(t, ctx, tx, `SELECT (SELECT count(*) FROM legal_documents) + (SELECT count(*) FROM operator_audit_log)`)
	}
	marker := "FAKEMARK" + uuid.NewString()[:8]
	over := marker + strings.Repeat("x", legalCeilingBytes+1-len(marker))
	overMB := marker + strings.Repeat("ü", (legalCeilingBytes-len(marker))/2) + "x"
	if len(over) != legalCeilingBytes+1 || len(overMB) != legalCeilingBytes+1 {
		t.Fatalf("PREMISE: the over-ceiling bodies are %d and %d bytes", len(over), len(overMB))
	}
	for _, c := range []struct {
		name string
		slug any
		body any
	}{
		{"a slug with no page", "refunds", marker + " text"},
		{"a NULL slug", nil, marker + " text"},
		{"a body of spaces (00020's CHECK refuses it too)", "privacy", "      "},
		{"a body of newlines and tabs (00020's CHECK does NOT refuse it)", "privacy", " \n\t\r\n "},
		{"a NULL body", "privacy", nil},
		{"one byte over 256 KiB", "privacy", over},
		{"one byte over 256 KiB, multi-byte", "privacy", overMB},
	} {
		before := count()
		opWantClean(t, opPublish(t, ctx, tx, hash, c.slug, c.body), sqlstateInvalidParameter, publishRefusal,
			"op_publish_legal, "+c.name, marker, "refunds")
		if n := count() - before; n != 0 {
			t.Errorf("op_publish_legal, %s: %d row(s) written", c.name, n)
		}
	}
	exact := strings.Repeat("y", legalCeilingBytes)
	exactMB := strings.Repeat("ü", legalCeilingBytes/2)
	for name, body := range map[string]string{"exactly 256 KiB": exact, "exactly 256 KiB, multi-byte": exactMB} {
		if len(body) != legalCeilingBytes {
			t.Fatalf("PREMISE: %s is %d bytes", name, len(body))
		}
		if err := opPublish(t, ctx, tx, hash, "privacy", body); err != nil {
			t.Errorf("CONTROL: %s was refused: %v", name, err)
		}
	}
}

// TestOpPublishLegal_TheTableStaysAppendOnly: the six statements below try to change or
// remove a version op_publish_legal wrote and are refused -- the owner's by 00020's
// trigger and 00021's TRUNCATE guard (23001), the definer's for want of an UPDATE or
// DELETE grant (42501). A correction is a new row.
func TestOpPublishLegal_TheTableStaysAppendOnly(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	if err := opPublish(t, ctx, tx, hash, "imprint", "FAKE imprint v1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	where := ` WHERE published_by = '` + a.id.String() + `'`
	for _, c := range []struct{ what, as, sql, code string }{
		{"owner UPDATE", "", `UPDATE legal_documents SET body = 'rewritten'` + where, sqlstateRestrictViolation},
		{"owner UPDATE of the date", "", `UPDATE legal_documents SET published_at = clock_timestamp()` + where, sqlstateRestrictViolation},
		{"owner DELETE", "", `DELETE FROM legal_documents` + where, sqlstateRestrictViolation},
		{"owner TRUNCATE", "", `TRUNCATE legal_documents`, sqlstateRestrictViolation},
		{"definer UPDATE", "tappa_opdefiner", `UPDATE public.legal_documents SET body = 'rewritten'` + where, sqlstateInsufficientPrivi},
		{"definer DELETE", "tappa_opdefiner", `DELETE FROM public.legal_documents` + where, sqlstateInsufficientPrivi},
	} {
		var err error
		if c.as == "" {
			err = opTry(t, ctx, tx, c.sql)
		} else {
			err = opExecAs(t, ctx, tx, c.as, c.sql)
		}
		opWant(t, err, c.code, c.what)
	}
	if err := opPublish(t, ctx, tx, hash, "imprint", "FAKE imprint v2"); err != nil {
		t.Fatalf("CONTROL: a correction as a new row was refused: %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM legal_documents`+where); n != 2 {
		t.Errorf("%d versions after a correction, want 2 (append-only)", n)
	}
}

// ------------------------------------------------------- the Go accessors --

// TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions measures LegalVersions'
// comment: on a pool built by the production constructor (openOperatorDB, with the test
// hook that makes the owner's connection tappa_operator) the two phases are two implicit
// transactions -- the call returns rows, phase one's audit row is committed and the
// ticket consumed -- and on ONE transaction the database refuses phase two, which the
// accessor returns as ErrOperatorRefused. The page it reads is the SECOND (of 5 rows),
// and the committed audit row must say so: the page the caller asked for travels
// through both phases (a Number lost on the way would either fail the hash or record
// page 1 -- the audit row's content does not depend on what legal_documents holds). It
// commits one 'read' row (file header). OP-10 phase B: the same read through
// *OperatorDB's METHOD -- the one the /operator/legal screen calls -- on that pool
// returns rows and commits one more 'read' row (two in all).
func TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("open the operator pool: %v", err)
	}
	defer o.Close()

	vs, err := LegalVersions(ctx, o.pool, f.hash, LegalVersionsPage{Number: 2, Size: 5})
	if err != nil {
		t.Fatalf("LegalVersions on the pool: %v", err)
	}
	if len(vs) > 5 {
		t.Errorf("a page of 5 returned %d rows", len(vs))
	}
	if n := f.liveReads(t, ctx, f.session); n != 1 {
		t.Errorf("the session has %d committed 'read' row(s) after one read, want 1", n)
	}
	var pageNumber, pageSize int
	if err := f.owner.QueryRow(ctx, `SELECT page_number, page_size FROM operator_audit_log
	                                  WHERE kind = 'read' AND session_id = $1`, f.session).Scan(&pageNumber, &pageSize); err != nil {
		t.Fatalf("read the committed audit row: %v", err)
	}
	if pageNumber != 2 || pageSize != 5 {
		t.Errorf("the read of page 2 (size 5) committed an audit row for page %d (size %d)", pageNumber, pageSize)
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, f.session); n != 1 {
		t.Errorf("%d consumed ticket(s), want 1", n)
	}

	tx := asOperatorTx(t, ctx, f.connect(t, ctx))
	if _, err := LegalVersions(ctx, tx, f.hash, LegalVersionsPage{Number: 1, Size: 5}); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("LegalVersions inside ONE transaction: %v, want ErrOperatorRefused (the ticket's transaction has not committed)", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := LegalVersions(ctx, o.pool, opRandHex(t), LegalVersionsPage{Number: 1, Size: 5}); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("LegalVersions with an unknown session: %v, want ErrOperatorRefused", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 1 {
		t.Errorf("the refused calls left the session with %d 'read' row(s), want 1", n)
	}
	// Through the method (OP-10 phase B), on the production-built pool.
	if _, err := o.LegalVersions(ctx, f.hash, LegalVersionsPage{Number: 1, Size: 5}); err != nil {
		t.Fatalf("(*OperatorDB).LegalVersions on its pool: %v", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 2 {
		t.Errorf("the method's read left the session with %d committed 'read' row(s), want 2", n)
	}
}

// TestPublishLegal_ErrorContract: the accessor publishes; a dead session is
// ErrOperatorRefused; a document the database refuses is a database error naming 22023
// -- not the refusal sentinel -- through which no *pgconn.PgError is reachable.
func TestPublishLegal_ErrorContract(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	call := func(session, slug, body string) error {
		t.Helper()
		return opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error { return PublishLegal(ctx, sp, session, slug, body) })
	}
	if err := call(hash, "cookies", "FAKE cookie notice"); err != nil {
		t.Fatalf("PublishLegal: %v", err)
	}
	if err := call(opRandHex(t), "cookies", "FAKE cookie notice"); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("an unknown session: %v, want ErrOperatorRefused", err)
	}
	err := call(hash, "refunds", "FAKE text")
	var pg *pgconn.PgError
	if err == nil || errors.Is(err, ErrOperatorRefused) || errors.As(err, &pg) || !strings.Contains(err.Error(), "SQLSTATE 22023") {
		t.Errorf("a refused document: %v, want a 22023 database error that is neither the refusal sentinel nor a reachable PgError", err)
	}
}

// TestReadTicket_PrintsOnlyThePlaceholder: the raw ticket inside readTicket never comes
// out of fmt (every verb, five positions), slog (text and JSON) or encoding/json
// (renderEverywhere's matrix). CONTROL: the same matrix finds the value in a struct that
// holds it as a plain field.
func TestReadTicket_PrintsOnlyThePlaceholder(t *testing.T) {
	raw := opRandHex(t)
	sawPlaceholder := false
	for _, s := range renderEverywhere(readTicket{v: &raw}) {
		if strings.Contains(s, raw) {
			t.Fatalf("the raw ticket was printed (%d-character output)", len(s))
		}
		sawPlaceholder = sawPlaceholder || strings.Contains(s, ticketRedacted)
	}
	if !sawPlaceholder {
		t.Error("the placeholder never appeared; the matrix may not be reaching the type's methods")
	}
	found := false
	for _, s := range renderEverywhere(plainHolder{dsn: raw}) {
		found = found || strings.Contains(s, raw)
	}
	if !found {
		t.Fatal("CONTROL FAILED: the matrix does not find a value held in a plain field")
	}
	if (readTicket{}).reveal() != "" {
		t.Error("the zero ticket reveals something")
	}
}
