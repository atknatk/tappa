package operator_test

// op8_db_test.go -- M10 OP-8 end to end against PostgreSQL: the operator surface on the
// shipped router, the real operatorauth.Authenticator, and a Store whose seven methods
// call internal/db's PRODUCTION accessors (the statements db.OperatorDB runs -- its
// methods delegate verbatim, TestOperatorDB_EveryMethodDelegatesVerbatim) on a
// connection that IS tappa_operator for PostgreSQL's privilege checks and RLS.
//
// HOW tappa_operator IS REACHED, MEASURED (2026-10-02): the role is NOLOGIN on the
// development database (rolcanlogin = f) and this task gives it no login. The one DSN
// form tried did not switch identity either: the owner's DSN with `options=-c
// session_authorization=tappa_operator` connected as current_user = session_user =
// tappa_owner (the startup option was ignored), so that DSN does not pass
// db.NewOperatorDB's role gate.
// The operator tests' own method is used instead (internal/operatorauth/harness_db_test.go,
// internal/db/operatorpool_test.go's asOperator): the owner's connection, ONE REPEATABLE
// READ transaction per test, rolled back at the end, and each store call in a savepoint
// under SET LOCAL SESSION AUTHORIZATION tappa_operator, with the identity re-read on each
// call. The operator tables' rows these tests write are in those rolled-back
// transactions (counted below: 0 left in the operator tables).
//
// WHAT THIS FILE DOES COMMIT, MEASURED (3rd round, F3 -- the 2nd round's header said
// "nothing is committed -- this file leaves no row behind", and it was wrong from the
// moment B12 added real customer sessions): ONE test,
// TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue, commits on tappa_app's pool, per
// run, 1 tenant ('OP8 Cross Cookie Ltd'), 1 location, 1 employee, 1 admin user
// (op8-cross-<id>@example.test), 1 admin session and 1 employee session -- the customer
// managers verify them on their own connections, so they cannot sit in this file's
// transaction. Counted on the development database (2026-10-02, owner connection,
// read-only transaction) around one run of that test and around one run of this file's
// TestE2E_ tests: +1 of each of those six, and 0 in platform_admins, platform_sessions,
// operator_audit_log,
// audit_log, tags and transactions. Accepted as the panel database tests' persistent
// fixtures are (internal/handler does not clean them up); the class is the backlog's
// T81 (test rows accumulating in the development database).
//
// The operator tables are shared with internal/db's DDL tests: this file holds the
// "tappa/test/operator-tables" advisory lock SHARED for each test's whole duration
// (OP-6 md. 16), as internal/operatorauth's database tests do.

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

// operatorTablesTestLock is internal/db's opTx lock, taken SHARED here
// (TestE2E_TheTablesLockIsTheOneInternalDBTakes reads internal/db's spelling).
const operatorTablesTestLock = "tappa/test/operator-tables"

// e2e is one test's database rig: the owner transaction, the store on it, and the
// surface on the shipped router.
type e2e struct {
	t    *testing.T
	ctx  context.Context
	tx   pgx.Tx
	kek  []byte
	now  time.Time
	auth *operatorauth.Authenticator
	h    http.Handler
	logs strings.Builder
	// touches counts op_touch_session calls the store made (the session predicate).
	touches *int
}

type dbQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// asOperator checks the savepoint's identity before each statement: a test here would not
// measure 00026 if a statement ran as the owner, a superuser to whom 00026's grants and
// RLS policy do not apply.
func asOperator(ctx context.Context, q dbQuerier) error {
	var cu, su string
	if err := q.QueryRow(ctx, `SELECT current_user::text, session_user::text`).Scan(&cu, &su); err != nil {
		return fmt.Errorf("read the identity: %w", err)
	}
	if cu != "tappa_operator" || su != "tappa_operator" {
		return fmt.Errorf("statements would run as current_user=%s session_user=%s", cu, su)
	}
	return nil
}

// e2eStore implements operatorauth.Store with internal/db's accessors, each call in a
// savepoint of the test's transaction as tappa_operator. A refused call rolls its
// savepoint back -- as its own transaction would be in production; a successful one puts
// the identity back before releasing (a released SET LOCAL survives into the parent).
type e2eStore struct {
	t       *testing.T
	tx      pgx.Tx
	touches *int
}

func (s e2eStore) as(ctx context.Context, fn func(db.OperatorConn) error) error {
	sp, err := s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := sp.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
		_ = sp.Rollback(ctx)
		return err
	}
	if err := asOperator(ctx, sp); err != nil {
		s.t.Errorf("harness: %v", err)
		_ = sp.Rollback(ctx)
		return err
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

func (s e2eStore) OperatorByEmail(ctx context.Context, email string) (out db.OperatorAccount, err error) {
	err = s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.OperatorByEmail(ctx, c, email); return })
	return
}

func (s e2eStore) OperatorByID(ctx context.Context, id uuid.UUID) (out db.OperatorAccount, err error) {
	err = s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.OperatorByID(ctx, c, id); return })
	return
}

func (s e2eStore) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, email string, admin uuid.UUID) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.RecordOperatorAuthEvent(ctx, c, kind, email, admin) })
}

func (s e2eStore) OpenOperatorSession(ctx context.Context, admin uuid.UUID, h string, step int64) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.OpenOperatorSession(ctx, c, admin, h, step) })
}

func (s e2eStore) CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, raw, digest string, sealed []byte, step int64, h string) error {
	return s.as(ctx, func(c db.OperatorConn) error {
		return db.CompleteOperatorEnrollment(ctx, c, admin, raw, digest, sealed, step, h)
	})
}

func (s e2eStore) TouchOperatorSession(ctx context.Context, h string) (out db.OperatorSession, err error) {
	*s.touches++
	err = s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.TouchOperatorSession(ctx, c, h); return })
	return
}

func (s e2eStore) CloseOperatorSession(ctx context.Context, h string) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.CloseOperatorSession(ctx, c, h) })
}

// e2ePassphrase is the fixtures' passphrase (built at run time: no credential-shaped
// literal in the source); e2eDigest its ONE cost-12 digest per test binary (00026's
// CHECK refuses a cheaper one -- `\$2[aby]\$1[2-4]\$`).
var (
	e2ePassphrase = strings.Repeat("op8 e2e ", 3)
	e2eDigest     = sync.OnceValues(func() (string, error) {
		b, err := bcryptCost12(e2ePassphrase)
		return b, err
	})
)

// newE2E opens the owner transaction (or skips without a database), takes the tables
// lock, reads the database's TOTP step and builds the Authenticator with its clock in
// the middle of that step (op_open_session binds the step to the DATABASE's clock, cur
// ± 1), the Surface and the shipped router.
func newE2E(t *testing.T) *e2e {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the operator surface's end-to-end tests need PostgreSQL (CLAUDE.md §8)")
	}
	if _, err := e2eDigest(); err != nil { // paid before the lock is held
		t.Fatalf("fixture digest: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Logf("close: %v", err)
		}
	})
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Logf("rollback: %v", err)
		}
	})
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'`); err != nil {
		t.Fatal(err)
	}
	g := &e2e{t: t, ctx: ctx, tx: tx, kek: randBytes(t, 32), touches: new(int)}
	g.now = g.dbStepTime()
	log := debugCapture(&g.logs)
	g.auth, err = operatorauth.New(e2eStore{t: t, tx: tx, touches: g.touches}, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(g.kek), TokenHMACKey: operatorauth.NewKey(randBytes(t, 32)),
		Now: func() time.Time { return g.now }, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := operator.New(g.auth, noStore{}, noStore{}, noStore{}, noStore{}, noStore{}, noTexts{}, opHost, opBase, log)
	if err != nil {
		t.Fatal(err)
	}
	g.h = httpx.NewRouter(&config.Config{OperatorHost: opHost, BaseURL: opBase}, log, s)
	return g
}

// dbStepTime is a Go time in the middle of the database's current 30-second step, read
// at least three seconds before the step ends (internal/operatorauth's harness emsal).
func (g *e2e) dbStepTime() time.Time {
	g.t.Helper()
	for {
		var s int64
		var left float64
		if err := g.tx.QueryRow(g.ctx, `
			SELECT floor(extract(epoch FROM clock_timestamp()) / 30)::bigint,
			       (30 - mod(extract(epoch FROM clock_timestamp()), 30))::float8`).Scan(&s, &left); err != nil {
			g.t.Fatalf("read the database's step: %v", err)
		}
		if left > 3 {
			return time.Unix(s*30+15, 0)
		}
		time.Sleep(time.Duration((left + 0.3) * float64(time.Second)))
	}
}

// resync moves the Authenticator's clock to the database's CURRENT step, for the
// request about to send a code. Pinned only in newE2E, the clock's step falls outside
// the database's cur ± 1 once a test has run for 33 to 60 s (cmd/opadmin lost
// TestResetMFA_KillsTheOldSessionsAndIssuesANewLink to exactly that in CI run
// 37138148744; here, the sign-in was measured refused the same way once the database's
// step was two ahead). A replay is sent WITHOUT a resync -- it must carry the step the
// first use stored -- and stepStillBound guards it instead.
func (g *e2e) resync() {
	g.t.Helper()
	g.now = g.dbStepTime()
}

// stepStillBound fails the test unless the database's step is STILL within one of
// codeTime's step, read right AFTER a replay the database was meant to refuse: the
// clock only moves forward, so the replay itself was inside op_open_session's cur ± 1
// and refused for the replay alone (internal/operatorauth's helper of the same name).
func (g *e2e) stepStillBound(codeTime time.Time) {
	g.t.Helper()
	var cur int64
	if err := g.tx.QueryRow(g.ctx, `SELECT floor(extract(epoch FROM clock_timestamp()) / 30)::bigint`).Scan(&cur); err != nil {
		g.t.Fatalf("read the database's step: %v", err)
	}
	if d := cur - codeTime.Unix()/30; d < -1 || d > 1 {
		g.t.Fatalf("the run was too slow for a replay to be refused for the replay's reason alone: the database is at step %d, the replayed code's step is %d", cur, codeTime.Unix()/30)
	}
}

func (g *e2e) exec(sql string, args ...any) {
	g.t.Helper()
	if _, err := g.tx.Exec(g.ctx, sql, args...); err != nil {
		g.t.Fatalf("%s: %v", sql, err)
	}
}

func (g *e2e) count(sql string, args ...any) int {
	g.t.Helper()
	var n int
	if err := g.tx.QueryRow(g.ctx, sql, args...).Scan(&n); err != nil {
		g.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// account inserts an operator in the given status as the owner (ADR 0020 §6: tappa_owner
// inserts platform_admins): an active or disabled one with the shared digest and a fresh 160-bit
// key sealed under the rig's KEK with the account id as AAD.
func (g *e2e) account(status string) fixture {
	g.t.Helper()
	digest, err := e2eDigest()
	if err != nil {
		g.t.Fatal(err)
	}
	f := fixture{id: uuid.New(), password: e2ePassphrase, key: randBytes(g.t, 20)}
	f.email = "op8-" + f.id.String()[:12] + "@example.test"
	sealed, err := sun.Seal(g.kek, f.id[:], f.key)
	if err != nil {
		g.t.Fatal(err)
	}
	g.exec(`INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed)
	        VALUES ($1, $2, 'op8 fixture', $3, $4, $5)`, f.id, f.email, status, digest, sealed)
	return f
}

// pending inserts a PENDING operator whose link token is returned raw, issued now and
// alive for 30 minutes, both stamps from ONE database clock read (00026's rule).
func (g *e2e) pending() (fixture, string) {
	g.t.Helper()
	tok, err := operatorauth.NewEnrollmentToken()
	if err != nil {
		g.t.Fatal(err)
	}
	f := fixture{id: uuid.New()}
	f.email = "op8p-" + f.id.String()[:12] + "@example.test"
	g.exec(`WITH c AS (SELECT clock_timestamp() AS now)
	        INSERT INTO platform_admins (id, email, display_name, status, enroll_token_hash, enroll_issued_at, enroll_expires_at)
	        SELECT $1, $2, 'op8 pending', 'pending', $3, c.now, c.now + interval '30 minutes' FROM c`,
		f.id, f.email, tok.Hash())
	return f, tok.RevealForLink()
}

func (g *e2e) do(r req) *httptest.ResponseRecorder {
	g.t.Helper()
	rg := &rig{t: g.t, h: g.h}
	return rg.do(r)
}

func (g *e2e) post(path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	g.t.Helper()
	return g.do(req{method: http.MethodPost, host: opHost, path: path, form: form, origin: opOrigin, cookies: cookies})
}

func (g *e2e) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	g.t.Helper()
	return g.do(req{method: http.MethodGet, host: opHost, path: path, cookies: cookies,
		header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
}

// signIn drives the password and TOTP steps for f through the surface, on a resynced
// clock.
func (g *e2e) signIn(f fixture) *http.Cookie {
	g.t.Helper()
	g.resync()
	rg := &rig{t: g.t, h: g.h, now: g.now}
	return rg.signIn(f)
}

// rows counts f's audit rows of kind, as the actor or as the target.
func (g *e2e) rows(f fixture, kind string) int {
	g.t.Helper()
	return g.count(`SELECT count(*)::int FROM operator_audit_log
	                WHERE kind = $2 AND (actor_admin_id = $1 OR target_admin_id = $1)`, f.id, kind)
}

// TestE2E_TheTablesLockIsTheOneInternalDBTakes: this file and internal/db's opTx must
// name the same advisory lock, or the two packages' operator-table tests stop taking
// turns (OP-6 md. 16). The spelling is read from internal/db's source.
func TestE2E_TheTablesLockIsTheOneInternalDBTakes(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "db", "operatorschema_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `const operatorTablesTestLock = "`+operatorTablesTestLock+`"`) {
		t.Fatalf("internal/db no longer names the %q lock", operatorTablesTestLock)
	}
}

// TestE2E_SignInRunsTheRealDefinersEndToEnd: the password step, the TOTP step
// (op_open_session -- its 'login' row), the console behind the session gate
// (op_touch_session, which advances last_used_at), the sign-out (op_close_session -- its
// 'logout' row) and the console again (303: the session is revoked) -- each step through
// the surface, against the real definers.
func TestE2E_SignInRunsTheRealDefinersEndToEnd(t *testing.T) {
	g := newE2E(t)
	f := g.account("active")
	sess := g.signIn(f)
	if n := g.rows(f, "login"); n != 1 {
		t.Fatalf("the sign-in wrote %d 'login' row(s), want 1", n)
	}
	if n := g.count(`SELECT count(*)::int FROM platform_sessions WHERE admin_id = $1 AND mfa_verified_at IS NOT NULL AND revoked_at IS NULL`, f.id); n != 1 {
		t.Fatalf("%d live MFA-stamped session(s) for the operator, want 1", n)
	}
	g.exec(`UPDATE platform_sessions SET last_used_at = clock_timestamp() - interval '10 minutes' WHERE admin_id = $1`, f.id)
	if w := g.get("/operator", sess); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "You are signed in") {
		t.Fatalf("the console with the live session = %d", w.Code)
	}
	if n := g.count(`SELECT count(*)::int FROM platform_sessions WHERE admin_id = $1 AND last_used_at > clock_timestamp() - interval '1 minute'`, f.id); n != 1 {
		t.Fatal("op_touch_session did not advance last_used_at")
	}
	w := g.post("/operator/logout", nil, sess)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" {
		t.Fatalf("sign-out = %d %q", w.Code, w.Result().Header.Get("Location"))
	}
	if n := g.rows(f, "logout"); n != 1 {
		t.Fatalf("the sign-out wrote %d 'logout' row(s), want 1", n)
	}
	if n := g.count(`SELECT count(*)::int FROM platform_sessions WHERE admin_id = $1 AND revoked_at IS NOT NULL`, f.id); n != 1 {
		t.Fatal("the sign-out did not revoke the session")
	}
	if w := g.get("/operator", sess); w.Code != http.StatusSeeOther {
		t.Fatalf("the console with the revoked session = %d, want 303", w.Code)
	}
}

// TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes (the "Every" of the name is these
// four arms): with tappa_operator's real RLS -- which shows the login lookup the ACTIVE
// accounts -- an unknown address, a wrong
// password, a PENDING account and a DISABLED account each answer 401 with the same body,
// and each leaves ONE pre-session row: login_failed for the wrong password, unknown_email
// for the other three (the pending and disabled rows name their account as the target --
// ADR 0021 §1's D3; the unknown one names nobody). ADR 0020 §3: "her giriş
// operator_audit_log'da"; "pending ... aynı yanıt".
func TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes(t *testing.T) {
	g := newE2E(t)
	active, disabled := g.account("active"), g.account("disabled")
	pend, _ := g.pending()
	unknownBefore := g.count(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'unknown_email' AND target_admin_id IS NULL`)
	var body string
	for _, c := range []struct {
		name, email, password string
		check                 func() bool
	}{
		{"an unknown address", "nobody-" + uuid.NewString()[:8] + "@example.test", e2ePassphrase, func() bool {
			return g.count(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'unknown_email' AND target_admin_id IS NULL`) == unknownBefore+1
		}},
		{"a wrong password", active.email, e2ePassphrase + "x", func() bool { return g.rows(active, "login_failed") == 1 }},
		{"a pending account", pend.email, e2ePassphrase, func() bool { return g.rows(pend, "unknown_email") == 1 }},
		{"a disabled account", disabled.email, e2ePassphrase, func() bool { return g.rows(disabled, "unknown_email") == 1 }},
	} {
		w := g.post("/operator/login", url.Values{"email": {c.email}, "password": {c.password}})
		if w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 {
			t.Errorf("%s: %d with %d cookie(s), want 401 and none", c.name, w.Code, len(w.Result().Cookies()))
		}
		if body == "" {
			body = w.Body.String()
		} else if w.Body.String() != body {
			t.Errorf("%s: the body differs from the unknown address's", c.name)
		}
		if !c.check() {
			t.Errorf("%s: its audit row is not there", c.name)
		}
	}
	if !strings.Contains(body, "We could not sign you in") {
		t.Fatal("CONTROL: the refused page does not carry the failure sentence")
	}
}

// TestE2E_AReplayedCodeIsRefused: the code that opened a session, sent once more at the
// code step, is refused -- op_open_session's `totp_last_step < step` (ADR 0020 §1, §4.4's
// mirror) -- and the
// refusal is a 'totp_failed' row that counts toward the lock. CONTROL: the first use
// signed in.
func TestE2E_AReplayedCodeIsRefused(t *testing.T) {
	g := newE2E(t)
	f := g.account("active")
	g.signIn(f) // the CONTROL: this code opened a session
	w := g.post("/operator/login", url.Values{"email": {f.email}, "password": {f.password}})
	ch := cookie(w, operatorauth.ChallengeCookieName)
	if ch == nil {
		t.Fatalf("the second password step = %d with no challenge", w.Code)
	}
	// No resync: the replay carries the step the sign-in's resync took and the database
	// stored. That the database still binds that step (one password step later) is
	// checked, not assumed: stepStillBound turns a run too slow for it red.
	w = g.post("/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, ch)
	if w.Code != http.StatusUnauthorized || cookie(w, operatorauth.SessionCookieName) != nil || !strings.Contains(w.Body.String(), "That code was not accepted") {
		t.Fatalf("the replayed code = %d, session cookie %v", w.Code, cookie(w, operatorauth.SessionCookieName) != nil)
	}
	g.stepStillBound(g.now)
	if n := g.rows(f, "totp_failed"); n != 1 {
		t.Fatalf("the replay wrote %d totp_failed row(s), want 1", n)
	}
	if n := g.count(`SELECT count(*)::int FROM platform_sessions WHERE admin_id = $1`, f.id); n != 1 {
		t.Fatalf("%d session(s) for the operator, want 1", n)
	}
}

// TestE2E_WrongCodesLockTheAccountAndEachLeavesARow: five wrong codes are five
// totp_failed rows and the database's lock (00026: N = 5, 15 minutes); then the RIGHT code
// is refused by op_open_session, answered with the locked page, and leaves a 'locked'
// row. ADR 0020 §3: "N hatalı TOTP'ta hesap kilidi + audit satırı" -- through the surface.
func TestE2E_WrongCodesLockTheAccountAndEachLeavesARow(t *testing.T) {
	g := newE2E(t)
	f := g.account("active")
	w := g.post("/operator/login", url.Values{"email": {f.email}, "password": {f.password}})
	ch := cookie(w, operatorauth.ChallengeCookieName)
	if ch == nil {
		t.Fatalf("the password step = %d with no challenge", w.Code)
	}
	wrong := wrongCodeAt(f.key, g.now)
	for i := 1; i <= 5; i++ {
		w = g.post("/operator/login/totp", url.Values{"code": {wrong}}, ch)
		if w.Code != http.StatusUnauthorized || cookie(w, operatorauth.SessionCookieName) != nil {
			t.Fatalf("wrong code %d = %d", i, w.Code)
		}
		if n := g.rows(f, "totp_failed"); n != i {
			t.Fatalf("after wrong code %d: %d totp_failed row(s)", i, n)
		}
	}
	if n := g.count(`SELECT count(*)::int FROM platform_admins WHERE id = $1 AND totp_locked_until > clock_timestamp()`, f.id); n != 1 {
		t.Fatal("five wrong codes did not lock the account")
	}
	// Resynced, so the database refuses the right code for the lock and not for a step
	// the test's running time left behind.
	g.resync()
	w = g.post("/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, ch)
	if w.Code != http.StatusUnauthorized || cookie(w, operatorauth.SessionCookieName) != nil || !strings.Contains(w.Body.String(), "Sign-in is locked for a while") {
		t.Fatalf("the right code while locked = %d, session cookie %v", w.Code, cookie(w, operatorauth.SessionCookieName) != nil)
	}
	if n := g.rows(f, "locked"); n != 1 {
		t.Fatalf("the locked attempt wrote %d 'locked' row(s), want 1", n)
	}
	if n := g.count(`SELECT count(*)::int FROM platform_sessions WHERE admin_id = $1`, f.id); n != 0 {
		t.Fatalf("a locked account got %d session(s)", n)
	}
}

// TestE2E_TheSessionGateRefusesEveryDeadSession: the console runs op_touch_session's
// predicate, and each dead shape -- past 8 hours, idle past 30 minutes, no MFA stamp,
// revoked, the operator disabled -- is the sign-in redirect; each live shape at the edge
// renders. The rows are aged by the owner (the predicate reads the database's wall clock).
func TestE2E_TheSessionGateRefusesEveryDeadSession(t *testing.T) {
	g := newE2E(t)
	f := g.account("active")
	sess := g.signIn(f)
	var sid uuid.UUID
	if err := g.tx.QueryRow(g.ctx, `SELECT id FROM platform_sessions WHERE admin_id = $1`, f.id).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		g.exec(`UPDATE platform_sessions SET created_at = clock_timestamp(), last_used_at = clock_timestamp(),
		          revoked_at = NULL, mfa_verified_at = clock_timestamp() WHERE id = $1`, sid)
		g.exec(`UPDATE platform_admins SET status = 'active' WHERE id = $1`, f.id)
	}
	for _, c := range []struct {
		name  string
		sql   string
		alive bool
	}{
		{"7h59m old", `UPDATE platform_sessions SET created_at = clock_timestamp() - interval '7 hours 59 minutes', last_used_at = clock_timestamp() - interval '1 minute' WHERE id = $1`, true},
		{"8h01m old", `UPDATE platform_sessions SET created_at = clock_timestamp() - interval '8 hours 1 minute', last_used_at = clock_timestamp() - interval '1 minute' WHERE id = $1`, false},
		{"idle 29 minutes", `UPDATE platform_sessions SET last_used_at = clock_timestamp() - interval '29 minutes' WHERE id = $1`, true},
		{"idle 31 minutes", `UPDATE platform_sessions SET last_used_at = clock_timestamp() - interval '31 minutes' WHERE id = $1`, false},
		{"no MFA stamp", `UPDATE platform_sessions SET mfa_verified_at = NULL WHERE id = $1`, false},
		{"revoked", `UPDATE platform_sessions SET revoked_at = clock_timestamp() WHERE id = $1`, false},
		{"operator disabled", `UPDATE platform_admins SET status = 'disabled' WHERE id = (SELECT admin_id FROM platform_sessions WHERE id = $1)`, false},
	} {
		reset()
		g.exec(c.sql, sid)
		w := g.get("/operator", sess)
		if c.alive && w.Code != http.StatusOK {
			t.Errorf("%s: %d, want the console", c.name, w.Code)
		}
		if !c.alive && (w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login") {
			t.Errorf("%s: %d %q, want 303 to the sign-in", c.name, w.Code, w.Result().Header.Get("Location"))
		}
	}
}

var (
	e2eKeyRE  = regexp.MustCompile(`<p class="op-key">([A-Z2-7 ]+)</p>`)
	e2eBlobRE = regexp.MustCompile(`name="blob" value="([^"]+)"`)
)

// enrollPage opens the enrollment page for id and returns the TOTP key it shows and the
// sealed blob of its form.
func (g *e2e) enrollPage(id uuid.UUID) ([]byte, string) {
	g.t.Helper()
	w := g.get("/operator/enroll?id=" + id.String())
	km, bm := e2eKeyRE.FindStringSubmatch(w.Body.String()), e2eBlobRE.FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || km == nil || bm == nil {
		g.t.Fatalf("the enrollment page = %d without its key or blob", w.Code)
	}
	key, err := base32.StdEncoding.DecodeString(strings.ReplaceAll(km[1], " ", ""))
	if err != nil {
		g.t.Fatal(err)
	}
	return key, html.UnescapeString(bm[1])
}

// TestE2E_EnrollmentCompletesThroughTheDefinerAndIsRateLimited: a pending account's link
// (id in the query, token in the form as the page's script puts it there) completes
// through op_complete_enrollment -- the account becomes active, its 'enrollment' row is
// written, the first session is open and the console renders with it. The SAME link
// again is refused by the database (a used token) with no session and an
// enrollment_failed row. The per-address enrollment share (3 per window, OP-6 12c) then
// refuses the address's fourth well-formed attempt with 429, no new enrollment_failed row
// and no new session (the counts below).
func TestE2E_EnrollmentCompletesThroughTheDefinerAndIsRateLimited(t *testing.T) {
	g := newE2E(t)
	f, tok := g.pending()
	key, blob := g.enrollPage(f.id)
	// Every form is built right before its request, on a resynced clock: each refusal
	// below is then the link's or the budget's, never a step left behind.
	form := func(token, blob string, key []byte) url.Values {
		g.resync()
		return url.Values{"id": {f.id.String()}, "token": {token}, "blob": {blob}, "password": {e2ePassphrase},
			"password_again": {e2ePassphrase}, "code": {totpAt(key, g.now)}}
	}
	w := g.post("/operator/enroll", form(tok, blob, key))
	sess := cookie(w, operatorauth.SessionCookieName)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator" || sess == nil {
		t.Fatalf("the enrollment = %d %q, session %v\n%s", w.Code, w.Result().Header.Get("Location"), sess != nil, g.logs.String())
	}
	if n := g.count(`SELECT count(*)::int FROM platform_admins WHERE id = $1 AND status = 'active' AND enroll_used_at IS NOT NULL`, f.id); n != 1 {
		t.Fatal("the account is not active with its token used")
	}
	if n := g.rows(f, "enrollment"); n != 1 {
		t.Fatalf("%d 'enrollment' row(s), want 1", n)
	}
	if w := g.get("/operator", sess); w.Code != http.StatusOK {
		t.Fatalf("the console with the enrollment's session = %d", w.Code)
	}
	// The same link again (2nd attempt from this address): the database refuses the used
	// token; a third with another well-formed token is refused the same way.
	for i, tk := range []string{tok, base64.RawURLEncoding.EncodeToString(randBytes(t, 32))} {
		key2, blob2 := g.enrollPage(f.id)
		w := g.post("/operator/enroll", form(tk, blob2, key2))
		if w.Code != http.StatusBadRequest || cookie(w, operatorauth.SessionCookieName) != nil || !strings.Contains(w.Body.String(), "This setup link does not work") {
			t.Fatalf("refused link %d = %d", i+1, w.Code)
		}
		if n := g.rows(f, "enrollment_failed"); n != i+1 {
			t.Fatalf("after refused link %d: %d enrollment_failed row(s)", i+1, n)
		}
	}
	// The fourth well-formed attempt from the same address: the address's share is spent.
	key4, blob4 := g.enrollPage(f.id)
	w = g.post("/operator/enroll", form(base64.RawURLEncoding.EncodeToString(randBytes(t, 32)), blob4, key4))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("the fourth enrollment from one address = %d, want 429", w.Code)
	}
	if n := g.rows(f, "enrollment_failed"); n != 2 {
		t.Fatalf("the refused fourth attempt wrote a row (%d enrollment_failed)", n)
	}
	if n := g.count(`SELECT count(*)::int FROM platform_sessions WHERE admin_id = $1`, f.id); n != 1 {
		t.Fatalf("%d session(s), want only the enrollment's", n)
	}
}

// TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue is the OP-8 acceptance "çapraz
// çerez ... (ve tersi)" against the real resolvers on BOTH sides, with REAL sessions:
//
//   - CUSTOMER -> OPERATOR: a panel session and an employee session are ISSUED by the
//     customer side's own managers (adminauth.Manager.Issue, session.Manager.Issue, on
//     tappa_app's pool), their cookie values read off the Set-Cookie their own codecs
//     write. Each value under the operator session cookie's name REACHES op_touch_session
//     (counted: exactly one predicate call per request) and is the sign-in redirect,
//     with the dead cookie cleared; under the challenge cookie's name it is the password
//     step again, with no account lookup.
//   - OPERATOR -> CUSTOMER: a LIVE operator session token under the panel's cookie name
//     to the panel's resolver (httpx.RequireAdmin + adminauth.Manager) is refused, and
//     under the employee cookie's name to the tap surface's resolver (httpx.Identify +
//     session.Manager) resolves to no session.
//
// CONTROLS: each value is live on its OWN side (the customer managers verify their own
// tokens; the console renders with the operator's); each customer resolver was really
// asked (its Verify ran once per request, not short-circuited).
//
// PERSISTENT TEST DATA, as the panel database tests leave (internal/handler does not
// clean its fixtures up): one tenant, one location, one employee, one admin user,
// one panel session and one employee session per run, committed on tappa_app's pool
// because the customer managers read them on their own connections (counted: the file
// header). The operator rows stay in this test's rolled-back transaction.
func TestE2E_CrossCookie_NeitherSideAcceptsTheOthersValue(t *testing.T) {
	app := os.Getenv("DATABASE_URL")
	if app == "" {
		t.Skip("DATABASE_URL not set")
	}
	g := newE2E(t)
	f := g.account("active")
	sess := g.signIn(f)
	if w := g.get("/operator", sess); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: the operator session is not live (%d)", w.Code)
	}

	// The customer side, for real.
	cfg := &config.Config{Env: "dev", DatabaseURL: app, SessionHMACKey: randBytes(t, 32), BaseURL: "http://localhost:8080"}
	data, err := db.New(g.ctx, cfg)
	if err != nil {
		t.Fatalf("customer pool: %v", err)
	}
	t.Cleanup(data.Close)
	admins, err := adminauth.New(data, cfg)
	if err != nil {
		t.Fatal(err)
	}
	employees, err := session.New(data, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tenantID, locationID, employeeID, adminID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	digest, err := bcrypt.GenerateFromPassword(randBytes(t, 16), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := data.WithTenant(g.ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                          VALUES ($1, 'OP8 Cross Cookie Ltd', $2, 'bar', 'single')`, tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'OP8 Venue')`, locationID, tenantID); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status)
		                          VALUES ($1, $2, $3, 'OP8 Employee', 'active')`, employeeID, tenantID, locationID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		                      VALUES ($1, $2, 'OP8 Owner', $3, $4, 'owner', 'active')`,
			adminID, tenantID, "op8-cross-"+adminID.String()+"@example.test", string(digest))
		return e
	}); err != nil {
		t.Fatalf("customer fixture: %v", err)
	}
	panelIssued, err := admins.Issue(g.ctx, adminauth.Verified{AdminUserID: adminID, TenantID: tenantID})
	if err != nil {
		t.Fatalf("issue a panel session: %v", err)
	}
	empIssued, err := employees.Issue(g.ctx, session.IssueParams{TenantID: tenantID, EmployeeID: employeeID})
	if err != nil {
		t.Fatalf("issue an employee session: %v", err)
	}
	valueOf := func(set func(http.ResponseWriter) error, name string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		if err := set(rec); err != nil {
			t.Fatal(err)
		}
		c := cookie(rec, name)
		if c == nil || c.Value == "" {
			t.Fatalf("PREMISE: %s wrote no cookie", name)
		}
		return c.Value
	}
	panelValue := valueOf(func(w http.ResponseWriter) error { return adminauth.NewCookies(cfg).Set(w, panelIssued.Token) }, adminauth.CookieName)
	empValue := valueOf(func(w http.ResponseWriter) error { return session.NewCookies(cfg).Set(w, empIssued.Token) }, session.CookieName)
	if _, err := admins.Verify(g.ctx, panelIssued.Token); err != nil {
		t.Fatalf("CONTROL: the panel session is not live on its own side: %v", err)
	}
	if _, err := employees.Verify(g.ctx, empIssued.Token); err != nil {
		t.Fatalf("CONTROL: the employee session is not live on its own side: %v", err)
	}

	// CUSTOMER -> OPERATOR.
	for name, v := range map[string]string{adminauth.CookieName: panelValue, session.CookieName: empValue} {
		before := *g.touches
		w := g.get("/operator", &http.Cookie{Name: operatorauth.SessionCookieName, Value: v})
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" {
			t.Errorf("a live %s value under the operator session name = %d %q, want 303 to the sign-in", name, w.Code, w.Result().Header.Get("Location"))
		}
		if got := *g.touches - before; got != 1 {
			t.Errorf("a live %s value under the operator session name made %d op_touch_session call(s), want exactly 1", name, got)
		}
		if c := cookie(w, operatorauth.SessionCookieName); c == nil || c.MaxAge >= 0 {
			t.Errorf("a live %s value under the operator session name: the dead cookie was not cleared", name)
		}
		w = g.post("/operator/login/totp", url.Values{"code": {totpAt(f.key, g.now)}}, &http.Cookie{Name: operatorauth.ChallengeCookieName, Value: v})
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "That sign-in step expired") || cookie(w, operatorauth.SessionCookieName) != nil {
			t.Errorf("a live %s value under the challenge name = %d, want the password step again", name, w.Code)
		}
	}

	// OPERATOR -> CUSTOMER.
	av := &countingAdminVerifier{inner: admins}
	refused := false
	panel := httpx.RequireAdmin(adminauth.NewCookies(cfg), av, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refused = true
		w.WriteHeader(http.StatusSeeOther)
	}))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	r := httptest.NewRequest(http.MethodGet, "http://taptime.mt/admin", nil)
	r.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: sess.Value})
	w := httptest.NewRecorder()
	panel.ServeHTTP(w, r)
	if !refused || w.Code != http.StatusSeeOther || av.calls != 1 {
		t.Errorf("the operator token under %s: refused %v, %d, resolver calls %d; want refused by a resolver that ran once",
			adminauth.CookieName, refused, w.Code, av.calls)
	}
	// CONTROL: the same resolver accepts the panel's own session.
	r = httptest.NewRequest(http.MethodGet, "http://taptime.mt/admin", nil)
	r.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: panelValue})
	refused = false
	w = httptest.NewRecorder()
	panel.ServeHTTP(w, r)
	if refused || w.Code != http.StatusOK {
		t.Errorf("CONTROL: the panel resolver refused its own live session (%d)", w.Code)
	}
	sv := &countingSessionVerifier{inner: employees}
	var live bool
	tap := httpx.Identify(session.NewCookies(cfg), sv)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		live = httpx.IdentityOf(r).Live()
	}))
	r = httptest.NewRequest(http.MethodGet, "http://taptime.mt/t", nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.Value})
	tap.ServeHTTP(httptest.NewRecorder(), r)
	if live || sv.calls != 1 {
		t.Errorf("the operator token under %s: live %v, resolver calls %d; want a resolver that ran once and found nothing",
			session.CookieName, live, sv.calls)
	}
	// CONTROL: the same resolver resolves the employee's own session.
	r = httptest.NewRequest(http.MethodGet, "http://taptime.mt/t", nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: empValue})
	tap.ServeHTTP(httptest.NewRecorder(), r)
	if !live {
		t.Error("CONTROL: the tap resolver did not resolve its own live session")
	}
}

type countingAdminVerifier struct {
	inner *adminauth.Manager
	calls int
}

func (c *countingAdminVerifier) Verify(ctx context.Context, t adminauth.Token) (adminauth.Resolved, error) {
	c.calls++
	return c.inner.Verify(ctx, t)
}

type countingSessionVerifier struct {
	inner *session.Manager
	calls int
}

func (c *countingSessionVerifier) Verify(ctx context.Context, t session.Token) (session.Resolved, error) {
	c.calls++
	return c.inner.Verify(ctx, t)
}

// bcryptCost12 is the fixture digest at 00026's floor cost.
func bcryptCost12(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), 12)
	return string(b), err
}
