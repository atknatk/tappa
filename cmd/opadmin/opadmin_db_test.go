package main

// opadmin_db_test.go -- the SQL this command writes, applied to a real PostgreSQL as the
// owner, and the account it makes driven through internal/operatorauth's own
// enrollment, sign-in and session check against 00026 (CLAUDE.md §8: no fake database).
//
// 🔴 ROLLED BACK, EXCEPT TWO TESTS. Every application runs inside the test's own owner
// transaction, which is rolled back when the test ends -- except
// TestApply_AFailureAnywhereLeavesNoRow and TestApply_ARowHeldElsewhereFailsFastNotForever,
// which apply a whole script at top level because the script's OWN transaction is what
// they measure; every one of those runs carries an injected failure (or waits out
// lock_timeout), so a correct script commits no row. If a broken script does commit,
// their cleanup tries to delete that pending row by id and the test is already red.
// ⚠️ Since migration 00033 (M10 OP-14 D) a committed create also commits its
// 'operator_created' audit row, which names the account (ON DELETE RESTRICT) and is
// append-only: the delete then FAILS, the cleanup reports it, and the pending account and
// its row stay in the database -- residue of a script that was broken, never of a correct
// one.
//
// The operator-tables advisory lock (internal/db/operatorschema_test.go takes it
// EXCLUSIVE for its DDL tests) is held SHARED for each test's whole duration
// (m10-platform.md, OP-6 card correction md. 16).
//
// The NOLOGIN operator role is reached the way internal/operatorauth's harness reaches
// it: SET LOCAL SESSION AUTHORIZATION tappa_operator from the owner's session, inside a
// savepoint per call, with the identity checked on every call.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// operatorTablesTestLock is internal/db's opTx lock; TestHarness_TheTablesLockIsTheOneInternalDBTakes
// (internal/operatorauth) and TestDB_TheTablesLockIsTheOneInternalDBTakes (here) read
// that file to keep the spellings equal.
const operatorTablesTestLock = "tappa/test/operator-tables"

// phrase is every fixture operator's sign-in passphrase: 24 runes, built at run time.
var phrase = strings.Repeat("op9 fixture ", 2)

func ownerDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; opadmin's SQL is applied as the owner (real Postgres required -- CLAUDE.md §8)")
	}
	return dsn
}

// ownerConn is a dedicated owner connection, closed when the test ends.
func ownerConn(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, ownerDSN(t))
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// sharedTablesLock holds the operator-tables lock SHARED on its own connection for the
// rest of the test. ONCE PER TEST TREE, at the top level of the test (or of the one
// helper it calls): a subtest or a helper must not ask for it again on another
// connection while its parent holds it -- see TestTablesLock_IsTakenOncePerTestTree.
func sharedTablesLock(t *testing.T, ctx context.Context) {
	t.Helper()
	if _, err := ownerConn(t, ctx).Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
}

// ownerTx is an owner transaction rolled back when the test ends, under the tables lock.
func ownerTx(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	sharedTablesLock(t, ctx)
	return ctx, beginOwnerTx(t, ctx)
}

// beginOwnerTx is ownerTx WITHOUT the tables lock, for a subtest whose parent already
// holds it for the whole tree.
func beginOwnerTx(t *testing.T, ctx context.Context) pgx.Tx {
	t.Helper()
	tx, err := ownerConn(t, ctx).Begin(ctx)
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Logf("rollback: %v", err)
		}
	})
	return tx
}

// execScriptStmt sends one statement of a generated script on pc the way psql does: a
// plain statement as a simple query, the COPY entry as COPY … FROM STDIN with its data
// line as the copy data (psql's "\\." terminator is the end of that data, not data).
func execScriptStmt(ctx context.Context, pc *pgconn.PgConn, st string) error {
	if c, data, ok := copyParts(st); ok {
		_, err := pc.CopyFrom(ctx, strings.NewReader(data+"\n"), c)
		return err
	}
	_, err := pc.Exec(ctx, st).ReadAll()
	return err
}

// applyInTx applies a generated script's statements between its BEGIN and COMMIT in a
// savepoint of tx: released on success, rolled back on error, which is returned. On
// success it then does what the script's COMMIT would have done to the session: the
// two SET LOCALs go back to their defaults and the ON COMMIT DROP payload table is
// dropped, so the test can apply a second script in the same transaction.
func applyInTx(t *testing.T, ctx context.Context, tx pgx.Tx, sql string) error {
	t.Helper()
	stmts := stmtsOf(t, sql)
	if len(stmts) != 7 || stmts[0] != "BEGIN;" || stmts[6] != "COMMIT;" {
		t.Fatalf("the script is not BEGIN … COMMIT around five statements: %d statements", len(stmts))
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	for _, st := range stmts[1:6] {
		if err := execScriptStmt(ctx, sp.Conn().PgConn(), st); err != nil {
			if rerr := sp.Rollback(ctx); rerr != nil {
				t.Fatalf("rollback to savepoint: %v", rerr)
			}
			return err
		}
	}
	if _, err := sp.Exec(ctx, `SET LOCAL search_path TO DEFAULT; SET LOCAL lock_timeout TO DEFAULT; DROP TABLE pg_temp.opadmin_in`); err != nil {
		t.Fatalf("undo the script's session state: %v", err)
	}
	if err := sp.Commit(ctx); err != nil {
		t.Fatalf("release savepoint: %v", err)
	}
	return nil
}

func mustApply(t *testing.T, ctx context.Context, tx pgx.Tx, sql string) {
	t.Helper()
	if err := applyInTx(t, ctx, tx, sql); err != nil {
		t.Fatalf("applying opadmin's SQL as the owner: %v", err)
	}
}

// ------------------------------------------------------- the operator side --

// txStore is operatorauth.Store over the test's transaction, each call in its own
// savepoint as tappa_operator (internal/operatorauth's txStore, written again here: it
// is that package's test code).
type txStore struct {
	t  *testing.T
	tx pgx.Tx
}

func (s txStore) as(ctx context.Context, fn func(db.OperatorConn) error) error {
	sp, err := s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := sp.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
		_ = sp.Rollback(ctx)
		return err
	}
	var cu, su string
	if err := sp.QueryRow(ctx, `SELECT current_user::text, session_user::text`).Scan(&cu, &su); err != nil ||
		cu != "tappa_operator" || su != "tappa_operator" {
		_ = sp.Rollback(ctx)
		s.t.Errorf("harness: the call would run as current_user=%s session_user=%s (%v)", cu, su, err)
		return errors.New("harness identity")
	}
	if err := fn(sp); err != nil {
		if rerr := sp.Rollback(ctx); rerr != nil {
			s.t.Errorf("rollback to savepoint: %v", rerr)
		}
		return err
	}
	if _, err := sp.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION DEFAULT`); err != nil {
		return err
	}
	return sp.Commit(ctx)
}

func (s txStore) OperatorByEmail(ctx context.Context, email string) (db.OperatorAccount, error) {
	var out db.OperatorAccount
	err := s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.OperatorByEmail(ctx, c, email); return })
	return out, err
}

func (s txStore) OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	var out db.OperatorAccount
	err := s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.OperatorByID(ctx, c, id); return })
	return out, err
}

func (s txStore) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, email string, admin uuid.UUID) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.RecordOperatorAuthEvent(ctx, c, kind, email, admin) })
}

func (s txStore) OpenOperatorSession(ctx context.Context, admin uuid.UUID, h string, step int64) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.OpenOperatorSession(ctx, c, admin, h, step) })
}

func (s txStore) CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, raw, digest string, sealed []byte, step int64, h string) error {
	return s.as(ctx, func(c db.OperatorConn) error {
		return db.CompleteOperatorEnrollment(ctx, c, admin, raw, digest, sealed, step, h)
	})
}

func (s txStore) TouchOperatorSession(ctx context.Context, h string) (db.OperatorSession, error) {
	var out db.OperatorSession
	err := s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.TouchOperatorSession(ctx, c, h); return })
	return out, err
}

func (s txStore) CloseOperatorSession(ctx context.Context, h string) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.CloseOperatorSession(ctx, c, h) })
}

// operatorSide is an Authenticator over the test transaction, with its clock pinned to
// the middle of the DATABASE's current 30-second step (op_complete_enrollment binds the
// first code's step to the database clock, cur ± 1) -- and pinned AGAIN before every
// enrollment (resync).
type operatorSide struct {
	t    *testing.T
	tx   pgx.Tx
	auth *operatorauth.Authenticator
	now  time.Time
	addr int
}

func newOperatorSide(t *testing.T, ctx context.Context, tx pgx.Tx) *operatorSide {
	t.Helper()
	o := &operatorSide{t: t, tx: tx}
	o.resync(ctx)
	key := func() operatorauth.Key {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		return operatorauth.NewKey(b)
	}
	auth, err := operatorauth.New(txStore{t: t, tx: tx}, operatorauth.Config{
		TOTPKEK: key(), TokenHMACKey: key(), Now: func() time.Time { return o.now }, Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("operatorauth.New: %v", err)
	}
	o.auth = auth
	return o
}

// resync pins the clock to the middle of the database's CURRENT step, read at least
// three seconds before that step ends. Pinned only once, the clock's step fell outside
// the database's cur ± 1 as soon as a test had run for 33 to 60 s -- the bound depends
// on where in its step the pin fell -- and the next enrollment was refused: CI run
// 37138148744, TestResetMFA_KillsTheOldSessionsAndIssuesANewLink, 39 s under -race
// ("the new link: operatorauth: enrollment refused"). Reproduced by holding that
// test's last enrollment until the database's step was two ahead of the pinned one;
// one ahead stayed green. op_complete_enrollment does not compare with totp_last_step,
// so a fresh step per enrollment is no replay this file needs to avoid.
func (o *operatorSide) resync(ctx context.Context) {
	o.t.Helper()
	for {
		var step int64
		var left float64
		if err := o.tx.QueryRow(ctx, `SELECT floor(extract(epoch FROM clock_timestamp()) / 30)::bigint,
		                                     (30 - mod(extract(epoch FROM clock_timestamp()), 30))::float8`).Scan(&step, &left); err != nil {
			o.t.Fatalf("read the database's step: %v", err)
		}
		if left > 3 {
			o.now = time.Unix(step*30+15, 0)
			return
		}
		time.Sleep(time.Duration((left + 0.3) * float64(time.Second)))
	}
}

// nextAddr is a fresh documentation address per call: the enrollment budget is three
// per address (operatorauth limits.go).
func (o *operatorSide) nextAddr() string {
	o.addr++
	return fmt.Sprintf("192.0.2.%d", o.addr)
}

// totpCode is RFC 6238 (HMAC-SHA1, 30 s, 6 digits), written out so the test does not
// borrow the code it checks (cmd/tappa's operator_test.go precedent).
func totpCode(key []byte, at time.Time) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, key)
	_, _ = m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1000000)
}

// enroll opens the link the way the enrollment page does: a fresh TOTP key for the
// link's account, then the link's secret, the new passphrase and the first code -- all
// on a clock resynced to the database's step, so a refusal is the link's and never a
// step the test's own running time left behind.
func (o *operatorSide) enroll(ctx context.Context, link string) (operatorauth.Issued, error) {
	o.resync(ctx)
	u, err := url.Parse(link)
	if err != nil {
		return operatorauth.Issued{}, err
	}
	id, err := uuid.Parse(u.Query().Get("id"))
	if err != nil {
		return operatorauth.Issued{}, err
	}
	p, err := o.auth.BeginEnrollment(id)
	if err != nil {
		return operatorauth.Issued{}, err
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(p.Secret.Base32())
	if err != nil {
		return operatorauth.Issued{}, err
	}
	return o.auth.CompleteEnrollment(ctx, o.nextAddr(), id, u.Fragment, p.Blob, phrase, totpCode(key, o.now))
}

// ------------------------------------------------------------------ helpers --

func linkOf(t *testing.T, g generated) string {
	t.Helper()
	l := linkRE.FindString(g.report)
	if l == "" {
		t.Fatal("no link in the report")
	}
	return l
}

func randomEmail(t *testing.T, prefix string) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return prefix + hex.EncodeToString(b) + "@Example.test"
}

type accountRow struct {
	status, email, name                     string
	hash                                    *string
	issued, expires                         *time.Time
	used, locked                            *time.Time
	ttlExact, noDigest, noEnvelope          bool
	failures                                int
	liveSessions, revokedSessions, sessions int
}

func readAccount(t *testing.T, ctx context.Context, q pgx.Tx, id string) accountRow {
	t.Helper()
	var r accountRow
	err := q.QueryRow(ctx, `
		SELECT a.status, a.email::text, a.display_name, a.enroll_token_hash, a.enroll_issued_at,
		       a.enroll_expires_at, a.enroll_used_at, a.totp_locked_until,
		       coalesce(a.enroll_expires_at - a.enroll_issued_at = interval '30 minutes', false),
		       a.password_hash IS NULL, a.totp_secret_sealed IS NULL, a.totp_failures,
		       (SELECT count(*) FROM public.platform_sessions s WHERE s.admin_id = a.id AND s.revoked_at IS NULL),
		       (SELECT count(*) FROM public.platform_sessions s WHERE s.admin_id = a.id AND s.revoked_at IS NOT NULL),
		       (SELECT count(*) FROM public.platform_sessions s WHERE s.admin_id = a.id)
		  FROM public.platform_admins a WHERE a.id = $1`, id).Scan(
		&r.status, &r.email, &r.name, &r.hash, &r.issued, &r.expires, &r.used, &r.locked,
		&r.ttlExact, &r.noDigest, &r.noEnvelope, &r.failures, &r.liveSessions, &r.revokedSessions, &r.sessions)
	if err != nil {
		t.Fatalf("read the account: %v", err)
	}
	return r
}

// clockReader is what dbNow and dbGen read the database clock on.
type clockReader interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// dbGen generates a script as if the generating machine's clock were the database's,
// read now: the generation time is then at or before the database clock whenever the
// script is applied, which is the condition generationGuards sets. The guards
// themselves are driven with explicit offsets (TestScript_GenerationGuards).
func dbGen(t *testing.T, ctx context.Context, q clockReader, args ...string) generated {
	t.Helper()
	return generateAt(t, dbNow(t, ctx, q), args...)
}

func dbNow(t *testing.T, ctx context.Context, q clockReader) time.Time {
	t.Helper()
	var now time.Time
	if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now
}

// createdAndEnrolled is the common fixture: an account made by opadmin create and
// enrolled through its link, with its first session.
func createdAndEnrolled(t *testing.T, ctx context.Context, tx pgx.Tx, o *operatorSide, email string) (generated, operatorauth.Issued) {
	t.Helper()
	g := dbGen(t, ctx, tx, "create", "--email", email, "--name", "Op Nine", "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, g.sql)
	issued, err := o.enroll(ctx, linkOf(t, g))
	if err != nil {
		t.Fatalf("the create link did not enroll: %v", err)
	}
	if _, err := o.auth.Verify(ctx, issued.Token); err != nil {
		t.Fatalf("the enrollment's session does not verify: %v", err)
	}
	return g, issued
}

// --------------------------------------------------------------------- tests --

// TestDB_TheTablesLockIsTheOneInternalDBTakes: this package names the advisory lock
// internal/db's DDL tests take exclusively, or the two would race.
func TestDB_TheTablesLockIsTheOneInternalDBTakes(t *testing.T) {
	src, err := os.ReadFile("../../internal/db/operatorschema_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `const operatorTablesTestLock = "`+operatorTablesTestLock+`"`) {
		t.Fatalf("internal/db no longer names the %q lock", operatorTablesTestLock)
	}
}

// TestCreate_TheLinkEnrollsTheAccount: create's SQL, applied as the owner, writes a
// pending account -- the address as typed, the name byte for byte, the hash of the
// link's secret (the database's own sha256 agrees), issued at the database's clock
// during the application and expiring exactly 30 minutes later -- that no sign-in
// sees; the link opens it through operatorauth's enrollment exactly once; the first
// session verifies and the operator signs in.
func TestCreate_TheLinkEnrollsTheAccount(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	email := randomEmail(t, "OP9.Create.")
	g := dbGen(t, ctx, tx, "create", "--email", email, "--name", "Op Nine Şahin", "--host", "ops.taptime.mt")

	before := dbNow(t, ctx, tx)
	mustApply(t, ctx, tx, g.sql)
	after := dbNow(t, ctx, tx)

	r := readAccount(t, ctx, tx, g.id)
	if r.status != "pending" || r.email != email || r.name != "Op Nine Şahin" || !r.noDigest || !r.noEnvelope || r.used != nil {
		t.Errorf("the new account: status %q, email as typed %v, name %v, no digest %v, no envelope %v, unused %v",
			r.status, r.email == email, r.name == "Op Nine Şahin", r.noDigest, r.noEnvelope, r.used == nil)
	}
	if r.hash == nil || *r.hash != operatorauth.EnrollmentTokenHash(g.secret) {
		t.Error("the stored hash is not operatorauth's hash of the link's secret")
	}
	if r.issued == nil || r.issued.Before(before) || r.issued.After(after) || !r.ttlExact {
		t.Errorf("issued at %v, want within the application [%v, %v]; expires - issued = 30 min exactly: %v", r.issued, before, after, r.ttlExact)
	}

	// Pending: no sign-in sees it (00026's policy shows active rows only).
	if _, err := o.auth.Password(ctx, o.nextAddr(), email, phrase); !errors.Is(err, operatorauth.ErrRefused) {
		t.Errorf("sign-in to a pending account: %v, want ErrRefused", err)
	}

	issued, err := o.enroll(ctx, linkOf(t, g))
	if err != nil {
		t.Fatalf("the link did not enroll the account: %v", err)
	}
	id, err := o.auth.Verify(ctx, issued.Token)
	if err != nil || id.AdminID.String() != g.id {
		t.Fatalf("the first session: %v, admin %v; want the account %s", err, id.AdminID, g.id)
	}
	if r := readAccount(t, ctx, tx, g.id); r.status != "active" || r.used == nil {
		t.Errorf("after enrollment: status %q, used %v", r.status, r.used != nil)
	}
	if _, err := o.enroll(ctx, linkOf(t, g)); !errors.Is(err, operatorauth.ErrEnrollment) {
		t.Errorf("the same link a second time: %v, want ErrEnrollment", err)
	}
	if _, err := o.auth.Password(ctx, o.nextAddr(), strings.ToLower(email), phrase); err != nil {
		t.Errorf("sign-in with the enrolled passphrase (address in another case): %v", err)
	}
}

// TestCreate_AnExpiredLinkIsRefused: the link's lifetime is the database's. A probe
// moves the issuance back by 30 min + 1 s (both columns, so the schema's ceiling
// holds) and the enrollment is refused; moved forward by 31 s -- 30 s left -- the
// same link enrolls. No clock is injected: op_complete_enrollment compares with
// clock_timestamp().
func TestCreate_AnExpiredLinkIsRefused(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	g := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op9.expired."), "--name", "Op Nine", "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, g.sql)

	shift := func(by string) {
		t.Helper()
		var past bool
		if err := tx.QueryRow(ctx, `UPDATE public.platform_admins
		                               SET enroll_issued_at = enroll_issued_at + $2::interval,
		                                   enroll_expires_at = enroll_expires_at + $2::interval
		                             WHERE id = $1
		                         RETURNING enroll_expires_at <= clock_timestamp()`, g.id, by).Scan(&past); err != nil {
			t.Fatalf("probe: %v", err)
		}
		t.Logf("after a shift of %s the link has expired: %v", by, past)
	}
	shift("-30 minutes -1 second")
	if _, err := o.enroll(ctx, linkOf(t, g)); !errors.Is(err, operatorauth.ErrEnrollment) {
		t.Fatalf("an expired link: %v, want ErrEnrollment", err)
	}
	if r := readAccount(t, ctx, tx, g.id); r.status != "pending" || r.used != nil {
		t.Fatalf("a refused link changed the account: status %q", r.status)
	}
	shift("31 seconds")
	if _, err := o.enroll(ctx, linkOf(t, g)); err != nil {
		t.Fatalf("CONTROL FAILED: the same link with 30 seconds left: %v", err)
	}
}

// TestResetMFA_KillsTheOldSessionsAndIssuesANewLink: on an enrolled, locked account,
// reset-mfa (address given in another case) revokes the live session, removes the
// envelope and the digest, resets the lock, returns the account to pending with a new
// hash issued now for 30 minutes; the old link stays refused, the new one enrolls.
func TestResetMFA_KillsTheOldSessionsAndIssuesANewLink(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	email := randomEmail(t, "op9.reset.")
	g, s1 := createdAndEnrolled(t, ctx, tx, o, email)
	if _, err := tx.Exec(ctx, `UPDATE public.platform_admins SET totp_failures = 7, totp_locked_until = clock_timestamp() + interval '10 minutes'
	                            WHERE id = $1`, g.id); err != nil {
		t.Fatal(err)
	}

	r := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", strings.ToUpper(email), "--host", "ops.taptime.mt")
	before := dbNow(t, ctx, tx)
	mustApply(t, ctx, tx, r.sql)
	after := dbNow(t, ctx, tx)

	if _, err := o.auth.Verify(ctx, s1.Token); !errors.Is(err, operatorauth.ErrNoSession) {
		t.Errorf("the session from before the reset: %v, want ErrNoSession", err)
	}
	row := readAccount(t, ctx, tx, g.id)
	if row.status != "pending" || !row.noDigest || !row.noEnvelope || row.failures != 0 || row.locked != nil || row.used != nil {
		t.Errorf("after reset-mfa: status %q, digest removed %v, envelope removed %v, failures %d, locked %v, unused %v",
			row.status, row.noDigest, row.noEnvelope, row.failures, row.locked != nil, row.used == nil)
	}
	if row.hash == nil || *row.hash != operatorauth.EnrollmentTokenHash(r.secret) || !row.ttlExact ||
		row.issued.Before(before) || row.issued.After(after) {
		t.Error("after reset-mfa the hash, the issue time or the 30 minutes are not the new link's")
	}
	if row.liveSessions != 0 || row.revokedSessions != 1 {
		t.Errorf("sessions: %d live, %d revoked; want 0 and 1", row.liveSessions, row.revokedSessions)
	}
	if _, err := o.auth.Password(ctx, o.nextAddr(), email, phrase); !errors.Is(err, operatorauth.ErrRefused) {
		t.Errorf("sign-in after the reset: %v, want ErrRefused", err)
	}
	if _, err := o.enroll(ctx, linkOf(t, g)); !errors.Is(err, operatorauth.ErrEnrollment) {
		t.Errorf("the old link after the reset: %v, want ErrEnrollment", err)
	}
	s2, err := o.enroll(ctx, linkOf(t, r))
	if err != nil {
		t.Fatalf("the new link: %v", err)
	}
	if _, err := o.auth.Verify(ctx, s2.Token); err != nil {
		t.Errorf("the new session: %v", err)
	}
	if _, err := o.auth.Verify(ctx, s1.Token); !errors.Is(err, operatorauth.ErrNoSession) {
		t.Errorf("the old session after re-enrollment: %v, want ErrNoSession", err)
	}
}

// refusedWith applies sql expecting the database to refuse it with a message holding
// want, and returns the error.
func refusedWith(t *testing.T, ctx context.Context, tx pgx.Tx, sql, want string) error {
	t.Helper()
	err := applyInTx(t, ctx, tx, sql)
	var pg *pgconn.PgError
	if err == nil || !errors.As(err, &pg) || !strings.Contains(pg.Message, want) {
		t.Errorf("want a refusal containing %q, got %v", want, err)
	}
	if pg != nil && pg.Detail != "" {
		t.Errorf("the refusal carries a DETAIL line")
	}
	return err
}

// TestResetMFA_RefusesWhatItMustNot: an id no account has, an id with another
// account's address, the same script applied twice, and a disabled account are each
// refused, and the account fields the test reads are as they were.
func TestResetMFA_RefusesWhatItMustNot(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	email := randomEmail(t, "op9.refuse.")
	g, s1 := createdAndEnrolled(t, ctx, tx, o, email)
	other := randomEmail(t, "op9.other.")
	og := dbGen(t, ctx, tx, "create", "--email", other, "--name", "Other", "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, og.sql)
	before := readAccount(t, ctx, tx, g.id)

	unknown := uuid.NewString()
	refusedWith(t, ctx, tx, dbGen(t, ctx, tx, "reset-mfa", "--id", unknown, "--email", email, "--host", "ops.taptime.mt").sql,
		"no operator account has BOTH id "+unknown)
	refusedWith(t, ctx, tx, dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", other, "--host", "ops.taptime.mt").sql,
		"no operator account has BOTH id "+g.id)
	if got := readAccount(t, ctx, tx, g.id); got.status != "active" || got.liveSessions != 1 || *got.hash != *before.hash {
		t.Fatalf("a refused reset changed the account: %+v", got.status)
	}

	r := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, r.sql)
	once := readAccount(t, ctx, tx, g.id)
	refusedWith(t, ctx, tx, r.sql, "this script was already applied")
	if again := readAccount(t, ctx, tx, g.id); !again.issued.Equal(*once.issued) {
		t.Error("the second application changed the issue time")
	}
	if _, err := o.auth.Verify(ctx, s1.Token); !errors.Is(err, operatorauth.ErrNoSession) {
		t.Errorf("the session after the reset: %v", err)
	}

	_, d, _ := runCmd(t, true, "disable", "--id", g.id, "--email", email)
	mustApply(t, ctx, tx, d)
	refusedWith(t, ctx, tx, dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt").sql,
		"is disabled")
	if got := readAccount(t, ctx, tx, g.id); got.status != "disabled" {
		t.Errorf("a refused reset of a disabled account left it %q", got.status)
	}
}

// TestDisable_EndsSessionsAndSignIn: disable (address in another case) revokes the
// live session and turns the account away at sign-in; after a second application the
// status and the session count are as they were; the used link stays refused.
func TestDisable_EndsSessionsAndSignIn(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	email := randomEmail(t, "op9.disable.")
	g, s1 := createdAndEnrolled(t, ctx, tx, o, email)
	if _, err := o.auth.Password(ctx, o.nextAddr(), email, phrase); err != nil {
		t.Fatalf("CONTROL FAILED: sign-in before the disable: %v", err)
	}

	_, d, _ := runCmd(t, true, "disable", "--id", g.id, "--email", strings.ToUpper(email))
	mustApply(t, ctx, tx, d)
	if _, err := o.auth.Verify(ctx, s1.Token); !errors.Is(err, operatorauth.ErrNoSession) {
		t.Errorf("the session after disable: %v, want ErrNoSession", err)
	}
	if _, err := o.auth.Password(ctx, o.nextAddr(), email, phrase); !errors.Is(err, operatorauth.ErrRefused) {
		t.Errorf("sign-in after disable: %v, want ErrRefused", err)
	}
	r := readAccount(t, ctx, tx, g.id)
	if r.status != "disabled" || r.liveSessions != 0 || r.revokedSessions != 1 {
		t.Errorf("after disable: status %q, %d live and %d revoked sessions", r.status, r.liveSessions, r.revokedSessions)
	}
	first := revokedAt(t, ctx, tx, g.id)
	mustApply(t, ctx, tx, d)
	if r := readAccount(t, ctx, tx, g.id); r.status != "disabled" || r.sessions != 1 {
		t.Errorf("a second disable: status %q, %d sessions", r.status, r.sessions)
	}
	if again := revokedAt(t, ctx, tx, g.id); len(first) != 1 || len(again) != 1 || !first[0].Equal(again[0]) {
		t.Errorf("a second disable changed the revoked session's revoked_at: %v -> %v", first, again)
	}
	if _, err := o.enroll(ctx, linkOf(t, g)); !errors.Is(err, operatorauth.ErrEnrollment) {
		t.Errorf("the used link of a disabled account: %v, want ErrEnrollment", err)
	}
}

// TestCreate_StoresHostileInputByteForByte: an address and a name made of SQL
// metacharacters arrive in the database exactly as typed; a second account for the
// same address in another case is refused by the unique constraint, NAMED, with no
// DETAIL and without the address in the message.
func TestCreate_StoresHostileInputByteForByte(t *testing.T) {
	ctx, tx := ownerTx(t)
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	email := "o'b$$r`{|}~%" + hex.EncodeToString(b) + "@x-y.test"
	name := "Zqx' $opadmin$ ; -- \\ %s \"Ş\" $$ end"
	g := dbGen(t, ctx, tx, "create", "--email", email, "--name", name, "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, g.sql)
	if r := readAccount(t, ctx, tx, g.id); r.email != email || r.name != name {
		t.Errorf("stored %q / %q, want the input byte for byte", r.email, r.name)
	}
	edge := dbGen(t, ctx, tx, "create", "--email", email254, "--name", strings.Repeat("ş", 200), "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, edge.sql)
	if r := readAccount(t, ctx, tx, edge.id); r.email != email254 || r.name != strings.Repeat("ş", 200) {
		t.Error("the 254-byte address or the 200-character name was not stored as given")
	}
	dup := dbGen(t, ctx, tx, "create", "--email", strings.ToUpper(email), "--name", "Dup", "--host", "ops.taptime.mt")
	err := refusedWith(t, ctx, tx, dup.sql, "refused by constraint platform_admins_email_key (SQLSTATE 23505)")
	if err != nil && (strings.Contains(err.Error(), hex.EncodeToString(b)) || strings.Contains(strings.ToLower(err.Error()), "x-y.test")) {
		t.Error("the refusal repeats the address")
	}
}

// pgCode is the SQLSTATE of err, or "".
func pgCode(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

// applyLikePsql sends a script's statements one by one on conn and keeps going after
// an error -- psql without ON_ERROR_STOP, measured to behave so (exit 0 throughout).
// It returns each statement's error.
func applyLikePsql(ctx context.Context, conn *pgx.Conn, stmts []string) []error {
	errs := make([]error, len(stmts))
	for i, st := range stmts {
		errs[i] = execScriptStmt(ctx, conn.PgConn(), st)
	}
	return errs
}

// insertBefore puts stmt into stmts before the first statement that has the prefix.
func insertBefore(t *testing.T, stmts []string, prefix, stmt string) []string {
	t.Helper()
	for i, s := range stmts {
		if strings.HasPrefix(s, prefix) {
			return append(append(append([]string{}, stmts[:i]...), stmt), stmts[i:]...)
		}
	}
	t.Fatalf("no statement starts with %q", prefix)
	return nil
}

// TestApply_AFailureAnywhereLeavesNoRow: create's script, applied at top level the way
// psql applies it by default (statement by statement, going on after an error), with a
// failure injected (a) before the payload table and the COPY, (b) between the COPY and
// the DO block, (c) inside the DO block after the INSERT, (d) between the DO block and
// COMMIT, and (e) the output of a run whose report could not be written (the poison in
// COMMIT's place): no account row is left in these five, and no audit row naming the
// account either (M10 OP-14 D: the DO block writes both, one statement). POSITIVE
// CONTROL: the same applier, with COMMIT replaced by a count and a ROLLBACK, sees the
// account row and its one 'operator_created' row the DO block writes. And reset-mfa with
// a failure between its two UPDATEs (before its audit row) or after its inner block (after
// it) leaves the account and its session as they were, and the account's owner rows -- of
// every owner kind, and of 'operator_mfa_reset' -- counted as many as before (its one
// 'operator_created'); CONTROL: the unbroken script, in a savepoint rolled back, is counted
// as one more. What that count measures is the DO block's atomicity, not where the audit row
// sits in it: a row written before the failure goes with it either way (2nd round: measured
// with the audit INSERT moved first; TestSQL_EachActionWritesOneAuditRowInItsDoBlock is the
// pin on its place). Every script here is generated with the DATABASE's clock
// (T94: the report subtest's run() was the last one handed this machine's clock).
func TestApply_AFailureAnywhereLeavesNoRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sharedTablesLock(t, ctx)
	check := ownerConn(t, ctx)
	count := func(id string) int {
		var n, audit int
		if err := check.QueryRow(ctx, `SELECT (SELECT count(*) FROM public.platform_admins WHERE id = $1),
		                                      (SELECT count(*) FROM public.operator_audit_log WHERE target_admin_id = $1)`, id).Scan(&n, &audit); err != nil {
			t.Fatal(err)
		}
		if audit != 0 {
			t.Errorf("%d audit row(s) name the account %s after a failure (the account row count is %d)", audit, id, n)
		}
		return n
	}
	cleanup := func(id string) {
		t.Cleanup(func() {
			tag, err := check.Exec(context.Background(), `DELETE FROM public.platform_admins WHERE id = $1 AND status = 'pending'`, id)
			if err != nil || tag.RowsAffected() != 0 {
				t.Errorf("cleanup of %s: %d rows deleted (a script committed), err %v", id, tag.RowsAffected(), err)
			}
		})
	}

	for _, tc := range []struct{ name, where string }{
		{"before the COPY", "CREATE TEMP "},
		{"before the DO block", "DO "},
		{"inside the DO block", ""},
		{"after the DO block", "COMMIT;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := dbGen(t, ctx, check, "create", "--email", randomEmail(t, "op9.atomic."), "--name", "Op Nine", "--host", "ops.taptime.mt")
			cleanup(g.id)
			stmts := stmtsOf(t, g.sql)
			if tc.where == "" {
				if !strings.HasPrefix(stmts[5], "DO ") || !strings.Contains(stmts[5], "\n    RAISE NOTICE") {
					t.Fatal("the DO block has no closing NOTICE to inject before")
				}
				stmts[5] = strings.Replace(stmts[5], "\n    RAISE NOTICE", "\n    PERFORM 1/0;\n    RAISE NOTICE", 1)
			} else {
				stmts = insertBefore(t, stmts, tc.where, "SELECT 1/0;")
			}
			errs := applyLikePsql(ctx, ownerConn(t, ctx), stmts)
			failed := 0
			for _, e := range errs {
				if e != nil {
					failed++
				}
			}
			if failed == 0 || !strings.Contains(fmt.Sprint(errs), "division by zero") {
				t.Fatalf("the injected failure did not happen: %v", errs)
			}
			if n := count(g.id); n != 0 {
				t.Errorf("%d account row(s) left after a failure %s", n, tc.name)
			}
		})
	}

	t.Run("positive control", func(t *testing.T) {
		g := dbGen(t, ctx, check, "create", "--email", randomEmail(t, "op9.control."), "--name", "Op Nine", "--host", "ops.taptime.mt")
		cleanup(g.id)
		stmts := stmtsOf(t, g.sql)
		last := len(stmts) - 1
		if last < 0 || stmts[last] != "COMMIT;" {
			t.Fatalf("the script does not end in COMMIT: %d statements", len(stmts))
		}
		stmts[last] = "ROLLBACK;"
		conn := ownerConn(t, ctx)
		var inside int
		for _, st := range stmts[:last] {
			if err := execScriptStmt(ctx, conn.PgConn(), st); err != nil {
				t.Fatalf("CONTROL FAILED: %v", err)
			}
		}
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM public.platform_admins WHERE id = $1`, g.id).Scan(&inside); err != nil || inside != 1 {
			t.Fatalf("CONTROL FAILED: inside the script's transaction the row count is %d (err %v), want 1", inside, err)
		}
		var trace int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM public.operator_audit_log
		                               WHERE target_admin_id = $1 AND kind = 'operator_created'`, g.id).Scan(&trace); err != nil || trace != 1 {
			t.Fatalf("CONTROL FAILED: inside the script's transaction %d 'operator_created' row(s) name the account (err %v), want 1", trace, err)
		}
		if _, err := conn.Exec(ctx, stmts[last]); err != nil {
			t.Fatal(err)
		}
		if n := count(g.id); n != 0 {
			t.Fatalf("the control's ROLLBACK left %d rows", n)
		}
	})

	t.Run("a report that could not be written", func(t *testing.T) {
		var out bytes.Buffer
		args := []string{"create", "--email", randomEmail(t, "op9.noreport."), "--name", "Op Nine", "--host", "ops.taptime.mt"}
		// The database's clock, not this machine's (T94): with this machine's clock ahead of
		// the database's by more than the time to the DO block, the generation guard refuses
		// the DO block, and the statement in COMMIT's place then answers 25P02 (an aborted
		// transaction), not the poison's P0001 -- derived from generationGuards, not reproduced.
		if code := run(args, &out, failingWriter{}, true, dbNow(t, ctx, check)); code != exitRefused {
			t.Fatalf("exit %d, want %d", code, exitRefused)
		}
		id := idOf(t, out.String())
		cleanup(id)
		stmts := stmtsOf(t, out.String())
		conn := ownerConn(t, ctx)
		errs := applyLikePsql(ctx, conn, stmts)
		if pgCode(errs[len(errs)-1]) != "P0001" {
			t.Fatalf("the statement in COMMIT's place: %v, want the poison's P0001", errs[len(errs)-1])
		}
		if err := conn.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if n := count(id); n != 0 {
			t.Errorf("%d account row(s) after applying the output of a run whose report failed", n)
		}
	})

	t.Run("reset-mfa between its two updates", func(t *testing.T) {
		// NOT ownerTx: this tree already holds the tables lock (above), and a second
		// SHARED request on another connection queues behind any EXCLUSIVE request
		// internal/db made in between, which waits on the first -- held until this
		// subtest returns. Measured: CI run 37084001714 lost this subtest and an
		// internal/db test to that wait (3 minutes each).
		tx := beginOwnerTx(t, ctx)
		o := newOperatorSide(t, ctx, tx)
		email := randomEmail(t, "op9.atomicreset.")
		g, s1 := createdAndEnrolled(t, ctx, tx, o, email)
		before := readAccount(t, ctx, tx, g.id)
		// The account's owner rows (2nd round of the OP-14 D review: the comment above named
		// this count and the subtest did not take it): every owner kind, and the reset's own.
		ownerRows := func() (all, resets int) {
			t.Helper()
			if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE kind = $3)
			                             FROM public.operator_audit_log
			                            WHERE target_admin_id = $1 AND kind = ANY ($2)`, g.id,
				[]string{auditKindCreate, auditKindResetMFA, auditKindDisable}, auditKindResetMFA).Scan(&all, &resets); err != nil {
				t.Fatal(err)
			}
			return all, resets
		}
		all0, resets0 := ownerRows()
		if all0 != 1 || resets0 != 0 {
			t.Fatalf("PREMISE: the account has %d owner row(s), %d of them resets; want its one 'operator_created'", all0, resets0)
		}
		r := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt")
		const second = "\n        UPDATE public.platform_sessions"
		const closing = "\n    RAISE NOTICE"
		if !strings.Contains(r.sql, second) || strings.Count(r.sql, closing) != 1 {
			t.Fatal("reset-mfa's SQL has no sessions UPDATE, or not one closing NOTICE, to inject before")
		}
		// Two places: between the two UPDATEs (before the audit row), and after the inner block
		// (the audit row already written by the failing DO block -- it goes with it).
		late := strings.Replace(r.sql, closing, "\n    PERFORM 1/0;"+closing, 1)
		if strings.Index(late, "PERFORM 1/0") < strings.Index(late, "INSERT INTO public.operator_audit_log") {
			t.Fatal("PREMISE: the late failure is not after the audit row")
		}
		for name, broken := range map[string]string{
			"between its two updates":   strings.Replace(r.sql, second, "\n        PERFORM 1/0;"+second, 1),
			"after its audit row write": late,
		} {
			if err := applyInTx(t, ctx, tx, broken); pgCode(err) != "22012" {
				t.Fatalf("the injected failure %s: %v, want 22012", name, err)
			}
			after := readAccount(t, ctx, tx, g.id)
			if after.status != "active" || after.noEnvelope || after.noDigest || *after.hash != *before.hash || after.liveSessions != 1 {
				t.Errorf("a reset-mfa failing %s changed the account: status %q, envelope removed %v, live sessions %d",
					name, after.status, after.noEnvelope, after.liveSessions)
			}
			if all, resets := ownerRows(); all != all0 || resets != resets0 {
				t.Errorf("a reset-mfa failing %s left %d owner row(s), %d of them resets; want %d and %d", name, all, resets, all0, resets0)
			}
		}
		// CONTROL: the same script unbroken, in a savepoint rolled back afterwards, is counted --
		// one more owner row, and it is the reset's.
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		if err := applyInTx(t, ctx, sp, r.sql); err != nil {
			t.Fatalf("CONTROL FAILED: the unbroken reset-mfa: %v", err)
		}
		if all, resets := ownerRows(); all != all0+1 || resets != resets0+1 {
			t.Errorf("CONTROL FAILED: the unbroken reset-mfa is counted as %d owner row(s), %d of them resets; want %d and %d",
				all, resets, all0+1, resets0+1)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
		if _, err := o.auth.Verify(ctx, s1.Token); err != nil {
			t.Errorf("the session after a failed reset-mfa: %v", err)
		}
	})
}

// TestApply_ARowHeldElsewhereFailsFastNotForever: while another transaction holds an
// uncommitted account with the same address, create's script waits on that row and is
// cancelled by its own lock_timeout (55P03) after about 5 seconds -- not after the other
// transaction ends -- and leaves no row.
func TestApply_ARowHeldElsewhereFailsFastNotForever(t *testing.T) {
	ctx, tx := ownerTx(t)
	email := randomEmail(t, "op9.lock.")
	// The holder is a plain owner INSERT, so this test measures the waiting script alone.
	if _, err := tx.Exec(ctx, `
		WITH c AS (SELECT clock_timestamp() AS now)
		INSERT INTO public.platform_admins (email, display_name, status, enroll_token_hash, enroll_issued_at, enroll_expires_at)
		SELECT $1, 'Holder', 'pending', repeat('a', 64), c.now, c.now + interval '30 minutes' FROM c`, email); err != nil {
		t.Fatalf("the holder's row: %v", err)
	}

	g := dbGen(t, ctx, tx, "create", "--email", email, "--name", "Waiter", "--host", "ops.taptime.mt")
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, sweeper := ownerConn(t, ctx), ownerConn(t, ctx)
	t.Cleanup(func() {
		tag, err := sweeper.Exec(context.Background(),
			`DELETE FROM public.platform_admins WHERE id = $1 AND status = 'pending'`, g.id)
		if err != nil || tag.RowsAffected() != 0 {
			t.Errorf("cleanup: %d rows deleted (the script committed), err %v", tag.RowsAffected(), err)
		}
	})
	stmts := stmtsOf(t, g.sql)
	do := -1
	for i, st := range stmts {
		if strings.HasPrefix(st, "DO ") {
			do = i
		}
	}
	if do < 0 {
		t.Fatal("the script has no DO block")
	}
	start := time.Now()
	errs := applyLikePsql(wctx, conn, stmts)
	waited := time.Since(start)
	if pgCode(errs[do]) != "55P03" {
		t.Fatalf("the DO block: %v, want 55P03 from lock_timeout (waited %v)", errs[do], waited)
	}
	if waited < 4*time.Second || waited > 15*time.Second {
		t.Errorf("the script waited %v; lock_timeout is %s", waited, lockTimeout)
	}
	if bad := serverSaw(t, g.sql, errs); len(bad) > 0 {
		t.Errorf("the lock timeout reached the server with a value: %v", bad)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM public.platform_admins WHERE id = $1`, g.id).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows for the waiting script's account (err %v)", n, err)
	}
}

// TestApply_ASecondCreateAndAMismatchedDisableAreRefused: the same create script
// applied twice is refused the second time by a named constraint (the id it carries
// already exists); disable with an id and another account's address is refused; the
// account is unchanged by both.
func TestApply_ASecondCreateAndAMismatchedDisableAreRefused(t *testing.T) {
	ctx, tx := ownerTx(t)
	g := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op9.twice."), "--name", "Op Nine", "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, g.sql)
	before := readAccount(t, ctx, tx, g.id)
	err := refusedWith(t, ctx, tx, g.sql, "refused by constraint platform_admins_")
	t.Logf("the second create was refused with: %v", err)

	other := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op9.other."), "--name", "Other", "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, other.sql)
	_, d, _ := runCmd(t, true, "disable", "--id", g.id, "--email", readAccount(t, ctx, tx, other.id).email)
	refusedWith(t, ctx, tx, d, "no operator account has BOTH id "+g.id)
	if after := readAccount(t, ctx, tx, g.id); after.status != "pending" || *after.hash != *before.hash || !after.issued.Equal(*before.issued) {
		t.Errorf("a refused application changed the account: status %q", after.status)
	}
}

// TestApply_RefusesARoleRLSWouldFilter: applied by tappa_app or by tappa_operator, each
// subcommand's script is stopped by its role guard -- the first statement of the DO
// block -- with the guard's message. POSITIVE CONTROL: the owner applies the same
// create script.
func TestApply_RefusesARoleRLSWouldFilter(t *testing.T) {
	ctx, tx := ownerTx(t)
	g := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op9.role."), "--name", "Op Nine", "--host", "ops.taptime.mt")
	r := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", "x@example.test", "--host", "ops.taptime.mt")
	_, d, _ := runCmd(t, true, "disable", "--id", g.id, "--email", "x@example.test")
	for _, role := range []string{"tappa_app", "tappa_operator"} {
		for name, sql := range map[string]string{"create": g.sql, "reset-mfa": r.sql, "disable": d} {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sp.Exec(ctx, "SET LOCAL SESSION AUTHORIZATION "+role); err != nil {
				t.Fatalf("become %s: %v", role, err)
			}
			var applyErr error
			for _, st := range stmtsOf(t, sql)[1:6] {
				if applyErr = execScriptStmt(ctx, sp.Conn().PgConn(), st); applyErr != nil {
					break
				}
			}
			if rerr := sp.Rollback(ctx); rerr != nil {
				t.Fatalf("rollback to savepoint: %v", rerr)
			}
			var pg *pgconn.PgError
			if !errors.As(applyErr, &pg) || !strings.Contains(pg.Message, "opadmin "+name+": "+role+" is neither a superuser nor BYPASSRLS") {
				t.Errorf("%s applied by %s: %v, want the role guard's refusal", name, role, applyErr)
			}
		}
	}
	mustApply(t, ctx, tx, g.sql)
	if r := readAccount(t, ctx, tx, g.id); r.status != "pending" {
		t.Fatalf("CONTROL FAILED: the owner's application left status %q", r.status)
	}
}

// TestResetMFA_ABAReplayIsRefused: reset R1 applied and its link used, reset R2 applied
// and its link used, then R1 applied again: refused, and R1's used link stays refused.
func TestResetMFA_ABAReplayIsRefused(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	email := randomEmail(t, "op9.aba.")
	g, _ := createdAndEnrolled(t, ctx, tx, o, email)
	r1 := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, r1.sql)
	if _, err := o.enroll(ctx, linkOf(t, r1)); err != nil {
		t.Fatalf("R1's link: %v", err)
	}
	r2 := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, r2.sql)
	if _, err := o.enroll(ctx, linkOf(t, r2)); err != nil {
		t.Fatalf("R2's link: %v", err)
	}
	before := readAccount(t, ctx, tx, g.id)
	if err := applyInTx(t, ctx, tx, r1.sql); err == nil {
		t.Error("R1 applied a second time after R2 was applied and used: accepted")
	}
	if after := readAccount(t, ctx, tx, g.id); after.status != before.status || *after.hash != *before.hash {
		t.Errorf("the account moved from %q to %q", before.status, after.status)
	}
	if _, err := o.enroll(ctx, linkOf(t, r1)); !errors.Is(err, operatorauth.ErrEnrollment) {
		t.Errorf("R1's used link after the replay: %v, want ErrEnrollment", err)
	}
}

// idOf reads the account id out of a script's header.
func idOf(t *testing.T, sql string) string {
	t.Helper()
	const marker = "for operator account "
	i := strings.Index(sql, marker)
	if i < 0 || len(sql) < i+len(marker)+36 {
		t.Fatal("the script header names no account")
	}
	return sql[i+len(marker) : i+len(marker)+36]
}

// revokedAt is the revoked_at of every revoked session of the account.
func revokedAt(t *testing.T, ctx context.Context, tx pgx.Tx, id string) []time.Time {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT revoked_at FROM public.platform_sessions WHERE admin_id = $1 AND revoked_at IS NOT NULL ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// scriptValues are the values a script must not hand the server outside its COPY data:
// every hex field of the data line after the padding (the hash among them), and the
// text each field decodes to when that is UTF-8 (the address, the name).
func scriptValues(t *testing.T, sql string) []string {
	t.Helper()
	for _, st := range stmtsOf(t, sql) {
		if _, data, ok := copyParts(st); ok {
			f := strings.Fields(strings.TrimPrefix(data, "-- "))
			if len(f) < 2 || f[0] != copyPadding {
				t.Fatal("the COPY data line does not start with the padding")
			}
			values := f[1:]
			for _, h := range f[1:] {
				if raw, err := hex.DecodeString(h); err == nil && utf8.Valid(raw) && len(raw) >= 3 {
					values = append(values, string(raw))
				}
			}
			return values
		}
	}
	t.Fatal("the script has no COPY data")
	return nil
}

// serverSaw reports which of the script's values reached the server in a form its log
// writes: a statement TEXT (log_statement, log_min_duration_statement, and the
// STATEMENT line of log_min_error_statement) or a field of an error the server
// returned (ERROR, DETAIL, HINT, CONTEXT, the internal query -- the lines the server
// logs beside STATEMENT). The COPY data is the one channel it does not count: it is not
// statement text.
func serverSaw(t *testing.T, sql string, errs []error) []string {
	t.Helper()
	values := scriptValues(t, sql)
	var seen []string
	look := func(where, text string) {
		for _, v := range values {
			if strings.Contains(text, v) {
				seen = append(seen, where)
			}
		}
	}
	for _, text := range statementTexts(t, sql) {
		look("statement text", text)
	}
	for _, err := range errs {
		if err == nil {
			continue
		}
		look("error text", err.Error())
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			for _, f := range []string{pg.Message, pg.Detail, pg.Hint, pg.Where, pg.InternalQuery} {
				look("error field", f)
			}
		}
	}
	return seen
}

// TestLog_TheValuesReachTheServerOnlyAsCopyData: for the refusals listed here, applied
// as psql applies a script (statement by statement), neither the statement texts nor
// the returned error fields carry the link secret's hash or a hex field of the data
// line: an unknown account (reset-mfa), an address already taken (create, the
// constraint path), the same reset applied twice, an A-B-A replay, a script older than
// its window, a script from the future, and the role guard (tappa_app). The lock-timeout
// path is measured in TestApply_ARowHeldElsewhereFailsFastNotForever. POSITIVE CONTROL:
// the same check sees a hash the server echoes in an error message.
func TestLog_TheValuesReachTheServerOnlyAsCopyData(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	email := randomEmail(t, "op9.log.")
	g, _ := createdAndEnrolled(t, ctx, tx, o, email)

	apply := func(sql string) []error {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		stmts := stmtsOf(t, sql)
		errs := make([]error, 0, len(stmts))
		for _, st := range stmts[1:6] {
			e := execScriptStmt(ctx, sp.Conn().PgConn(), st)
			errs = append(errs, e)
			if e != nil {
				break
			}
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return errs
	}
	refused := func(name, sql, want string) {
		t.Helper()
		errs := apply(sql)
		last := errs[len(errs)-1]
		if last == nil || !strings.Contains(last.Error(), want) {
			t.Errorf("%s: %v, want a refusal containing %q", name, last, want)
		}
		if bad := serverSaw(t, sql, errs); len(bad) > 0 {
			t.Errorf("%s: a value reached the server as %v", name, bad)
		}
	}

	refused("unknown account", dbGen(t, ctx, tx, "reset-mfa", "--id", uuid.NewString(), "--email", email, "--host", "ops.taptime.mt").sql,
		"no operator account has BOTH id")
	refused("address taken", dbGen(t, ctx, tx, "create", "--email", strings.ToUpper(email), "--name", "Dup", "--host", "ops.taptime.mt").sql,
		"refused by constraint platform_admins_email_key")
	r1 := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, r1.sql)
	refused("same reset twice", r1.sql, "this script was already applied")
	if _, err := o.enroll(ctx, linkOf(t, r1)); err != nil {
		t.Fatal(err)
	}
	r2 := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, r2.sql)
	refused("A-B-A replay", r1.sql, "not before this script was generated")
	old := generateAt(t, dbNow(t, ctx, tx).Add(-31*time.Minute), "create", "--email", randomEmail(t, "op9.log.old."), "--name", "Old", "--host", "ops.taptime.mt")
	refused("too old", old.sql, "is more than 30 minutes old")
	future := generateAt(t, dbNow(t, ctx, tx).Add(time.Minute), "create", "--email", randomEmail(t, "op9.log.future."), "--name", "Future", "--host", "ops.taptime.mt")
	refused("from the future", future.sql, "ahead of the database clock")

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, "SET LOCAL SESSION AUTHORIZATION tappa_app"); err != nil {
		t.Fatal(err)
	}
	roleSQL := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op9.log.role."), "--name", "Role", "--host", "ops.taptime.mt").sql
	var roleErrs []error
	for _, st := range stmtsOf(t, roleSQL)[1:6] {
		e := execScriptStmt(ctx, sp.Conn().PgConn(), st)
		roleErrs = append(roleErrs, e)
		if e != nil {
			break
		}
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if last := roleErrs[len(roleErrs)-1]; last == nil || !strings.Contains(last.Error(), "is neither a superuser nor BYPASSRLS") {
		t.Errorf("role guard: %v", last)
	}
	if bad := serverSaw(t, roleSQL, roleErrs); len(bad) > 0 {
		t.Errorf("role guard: a value reached the server as %v", bad)
	}

	// POSITIVE CONTROL: an error that echoes a data-line value is seen. The value is
	// synthetic -- it replaces the hash in a copy of the script -- so the echo sends no
	// real link hash to the server.
	synthetic := strings.Repeat("5a", 32)
	planted := strings.Replace(r2.sql, scriptValues(t, r2.sql)[0], synthetic, 1)
	sp, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, echo := sp.Exec(ctx, "SELECT $1::int", synthetic)
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if bad := serverSaw(t, planted, []error{echo}); len(bad) == 0 {
		t.Fatalf("CONTROL FAILED: an error echoing a data-line value (%v) was not seen", pgCode(echo))
	}
}

// TestScript_GenerationGuards: for create AND reset-mfa, a script is accepted 29 minutes
// after its generation time and refused after 31; refused when its generation time is
// 250 ms, 1 s or 20 s ahead of the database clock, and the same refused 1-s file is
// accepted once the database clock has passed it; and a reset-mfa generated before the account's last
// issuance is refused. The reset-mfa cases move the account's last issuance two hours
// back first, so the replay guard does not stand in for the age guard. The skew between
// this machine and the database is logged.
func TestScript_GenerationGuards(t *testing.T) {
	ctx, tx := ownerTx(t)
	t.Logf("database clock minus this machine's clock: %v", time.Until(dbNow(t, ctx, tx)))

	create := func(offset time.Duration) generated {
		t.Helper()
		return generateAt(t, dbNow(t, ctx, tx).Add(offset), "create", "--email", randomEmail(t, "op9.guard."), "--name", "Guard", "--host", "ops.taptime.mt")
	}
	mustApply(t, ctx, tx, create(-29*time.Minute).sql)
	refusedWith(t, ctx, tx, create(-31*time.Minute).sql, "is more than 30 minutes old")
	refusedWith(t, ctx, tx, create(20*time.Second).sql, "ahead of the database clock")
	refusedWith(t, ctx, tx, create(250*time.Millisecond).sql, "ahead of the database clock")
	ahead := create(time.Second)
	refusedWith(t, ctx, tx, ahead.sql, "ahead of the database clock")
	waitPast(t, ctx, tx, time.Second+200*time.Millisecond)
	mustApply(t, ctx, tx, ahead.sql)

	g := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op9.guard.reset."), "--name", "Guard", "--host", "ops.taptime.mt")
	early := generateAt(t, dbNow(t, ctx, tx).Add(-time.Second), "reset-mfa", "--id", g.id, "--email", readEmailOf(g), "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, g.sql)
	refusedWith(t, ctx, tx, early.sql, "not before this script was generated")

	reset := func(offset time.Duration) generated {
		t.Helper()
		if _, err := tx.Exec(ctx, `UPDATE public.platform_admins
		                              SET enroll_issued_at = clock_timestamp() - interval '2 hours',
		                                  enroll_expires_at = clock_timestamp() - interval '90 minutes'
		                            WHERE id = $1`, g.id); err != nil {
			t.Fatalf("move the last issuance back: %v", err)
		}
		return generateAt(t, dbNow(t, ctx, tx).Add(offset), "reset-mfa", "--id", g.id, "--email", readEmailOf(g), "--host", "ops.taptime.mt")
	}
	refusedWith(t, ctx, tx, reset(-31*time.Minute).sql, "is more than 30 minutes old")
	mustApply(t, ctx, tx, reset(-29*time.Minute).sql)
	refusedWith(t, ctx, tx, reset(20*time.Second).sql, "ahead of the database clock")
	refusedWith(t, ctx, tx, reset(250*time.Millisecond).sql, "ahead of the database clock")
	aheadReset := reset(time.Second)
	refusedWith(t, ctx, tx, aheadReset.sql, "ahead of the database clock")
	waitPast(t, ctx, tx, time.Second+200*time.Millisecond)
	mustApply(t, ctx, tx, aheadReset.sql)
}

// waitPast sleeps until the database clock has moved d past the moment it is called.
func waitPast(t *testing.T, ctx context.Context, q clockReader, d time.Duration) {
	t.Helper()
	until := dbNow(t, ctx, q).Add(d)
	for dbNow(t, ctx, q).Before(until) {
		time.Sleep(50 * time.Millisecond)
	}
}

// TestResetMFA_AClockAheadDoesNotReopenAUsedLink: the auditor's probe. A machine whose
// clock is 10 minutes ahead generates R1 and R2; R1 is refused at its first application
// (generation time ahead of the database clock), so there is no used R1 link for a
// later replay to re-open. And with a clock 2 s ahead: R1 is refused, then accepted once
// the database clock has passed its generation time; its link is used; R2 is applied
// and used; R1 applied again is refused and its link stays refused -- the replay window
// the 30-second allowance left in round 2 (README O9-5) does not appear with the zero
// allowance. If R1 is accepted at +10 minutes the test goes on and reports the
// replay it then measures.
func TestResetMFA_AClockAheadDoesNotReopenAUsedLink(t *testing.T) {
	ctx, tx := ownerTx(t)
	o := newOperatorSide(t, ctx, tx)
	email := randomEmail(t, "op9.ahead.")
	g, _ := createdAndEnrolled(t, ctx, tx, o, email)
	reset := func(offset time.Duration) generated {
		t.Helper()
		return generateAt(t, dbNow(t, ctx, tx).Add(offset), "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt")
	}

	r1 := reset(10 * time.Minute)
	if err := applyInTx(t, ctx, tx, r1.sql); err == nil {
		t.Error("R1 generated by a clock 10 minutes ahead was accepted")
		if _, err := o.enroll(ctx, linkOf(t, r1)); err != nil {
			t.Fatalf("R1's link: %v", err)
		}
		time.Sleep(4 * time.Second)
		r2 := reset(10*time.Minute + 4*time.Second)
		mustApply(t, ctx, tx, r2.sql)
		if err := applyInTx(t, ctx, tx, r1.sql); err == nil {
			_, eerr := o.enroll(ctx, linkOf(t, r1))
			t.Errorf("and R1 applied again after R2 was accepted; R1's used link then enrolls: %v", eerr == nil)
		}
		return
	} else if !strings.Contains(err.Error(), "ahead of the database clock") {
		t.Fatalf("R1 at +10 minutes: %v, want the generation-time refusal", err)
	}

	r1 = reset(2 * time.Second)
	refusedWith(t, ctx, tx, r1.sql, "ahead of the database clock")
	waitPast(t, ctx, tx, 2*time.Second+200*time.Millisecond)
	mustApply(t, ctx, tx, r1.sql)
	if _, err := o.enroll(ctx, linkOf(t, r1)); err != nil {
		t.Fatalf("R1's link: %v", err)
	}
	r2 := dbGen(t, ctx, tx, "reset-mfa", "--id", g.id, "--email", email, "--host", "ops.taptime.mt")
	mustApply(t, ctx, tx, r2.sql)
	if _, err := o.enroll(ctx, linkOf(t, r2)); err != nil {
		t.Fatalf("R2's link: %v", err)
	}
	refusedWith(t, ctx, tx, r1.sql, "not before this script was generated")
	if _, err := o.enroll(ctx, linkOf(t, r1)); !errors.Is(err, operatorauth.ErrEnrollment) {
		t.Errorf("R1's used link after the refused replay: %v, want ErrEnrollment", err)
	}
}

// withData is sql with its COPY data line replaced by data (which may hold several
// lines).
func withData(t *testing.T, sql, data string) string {
	t.Helper()
	for _, st := range stmtsOf(t, sql) {
		if _, old, ok := copyParts(st); ok {
			return strings.Replace(sql, "\n"+old+"\n\\.\n", "\n"+data+"\n\\.\n", 1)
		}
	}
	t.Fatal("the script has no COPY data")
	return ""
}

// TestPayload_OnlyTheOneLineOfTheExpectedShapeIsAccepted: create's script with its data
// line edited by hand is refused by the DO block for each of the eight edits listed --
// a second line, a field more, a field less, the marker changed, the padding changed,
// the padding missing, a non-hex field, an upper-case hash -- and accepted unedited
// (POSITIVE CONTROL).
func TestPayload_OnlyTheOneLineOfTheExpectedShapeIsAccepted(t *testing.T) {
	ctx, tx := ownerTx(t)
	g := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op9.payload."), "--name", "Payload", "--host", "ops.taptime.mt")
	var line string
	for _, st := range stmtsOf(t, g.sql) {
		if _, d, ok := copyParts(st); ok {
			line = d
		}
	}
	f := strings.Fields(line) // "--", padding, hash, email, name
	if len(f) != 5 {
		t.Fatalf("the data line has %d fields", len(f))
	}
	join := func(fields ...string) string { return strings.Join(fields, " ") }
	for name, data := range map[string]string{
		"a second line":       line + "\n" + line,
		"a field more":        line + " 00",
		"a field less":        join(f[0], f[1], f[2], f[3]),
		"the marker changed":  join("XX", f[1], f[2], f[3], f[4]),
		"the padding changed": join(f[0], strings.Replace(f[1], "x", "y", 1), f[2], f[3], f[4]),
		"the padding missing": join(f[0], f[2], f[3], f[4]),
		"a non-hex field":     join(f[0], f[1], f[2], f[3]+"g0", f[4]),
		"an upper-case hash":  join(f[0], f[1], strings.ToUpper(f[2]), f[3], f[4]),
	} {
		refusedWith(t, ctx, tx, withData(t, g.sql, data), "the COPY data is not the one line this script carries")
		if r := countAccount(t, ctx, tx, g.id); r != 0 {
			t.Errorf("%s: %d account rows", name, r)
		}
	}
	mustApply(t, ctx, tx, g.sql)
	if countAccount(t, ctx, tx, g.id) != 1 {
		t.Fatal("CONTROL FAILED: the unedited script wrote no account")
	}
}

func countAccount(t *testing.T, ctx context.Context, tx pgx.Tx, id string) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM public.platform_admins WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestCopy_ADataErrorShowsThePaddingNotTheValues: a COPY data error (here an extra
// column) puts the first 100 bytes of the line into the error's CONTEXT -- the callback
// a cancel during the COPY reaches too -- and those bytes hold the marker and the
// padding, and of the script's data-line values neither the whole value nor its first
// 8 bytes. POSITIVE CONTROL: a line of synthetic
// values without the padding shows its first value in the CONTEXT.
func TestCopy_ADataErrorShowsThePaddingNotTheValues(t *testing.T) {
	ctx, tx := ownerTx(t)
	g := dbGen(t, ctx, tx, "create", "--email", randomEmail(t, "op9.context."), "--name", "Context", "--host", "ops.taptime.mt")
	var line string
	for _, st := range stmtsOf(t, g.sql) {
		if _, d, ok := copyParts(st); ok {
			line = d
		}
	}
	where := func(data string) string {
		t.Helper()
		err := applyInTx(t, ctx, tx, withData(t, g.sql, data))
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "22P04" {
			t.Fatalf("want the COPY data error 22P04, got %v", err)
		}
		return pg.Where
	}
	got := where(line + "\tz")
	if !strings.Contains(got, "-- "+copyPadding[:min(40, len(copyPadding))]) {
		t.Errorf("the CONTEXT does not show the start of the line: %d bytes", len(got))
	}
	for _, v := range scriptValues(t, g.sql) {
		if strings.Contains(got, v) || (len(v) >= 8 && strings.Contains(got, v[:8])) {
			t.Errorf("the CONTEXT of a COPY data error holds a data-line value or its first 8 bytes (%d-byte value)", len(v))
		}
	}
	synthetic := strings.Repeat("6b", 32)
	if c := where("-- " + synthetic + "\tz"); !strings.Contains(c, synthetic) {
		t.Fatalf("CONTROL FAILED: a value at the start of the line is not in the CONTEXT: %d bytes", len(c))
	}
}

// readEmailOf is the address a generated create's report names.
func readEmailOf(g generated) string {
	for _, line := range strings.Split(g.report, "\n") {
		if v, ok := strings.CutPrefix(line, "  email        "); ok {
			return v
		}
	}
	return ""
}
