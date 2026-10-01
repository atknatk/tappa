package operatorauth

// harness_db_test.go -- how this package's DATABASE tests reach PostgreSQL.
//
// 🔴 NO TEST DOUBLE STANDS IN FOR THE DATABASE. Every Store method below calls the
// PRODUCTION accessor in internal/db/operator.go -- the same statement text OP-7's
// db.OperatorDB will run -- on a connection that IS tappa_operator for PostgreSQL's
// privilege checks. The only thing the harness adds is that identity: both operator
// roles are born NOLOGIN (01-roles.sql), so the tests connect as the owner and switch
// with SET LOCAL SESSION AUTHORIZATION (internal/db/operatorschema_test.go's method,
// and the reason it is not SET ROLE is written there). A fake store could not prove a
// single one of this package's database acceptances: the lock, the replay guard, the
// session predicate and the one-winner race are all the database's.
//
// TWO SHAPES:
//   - txStore: one owner transaction, REPEATABLE READ, rolled back when the test ends
//     (nothing is committed); every call in its own savepoint as tappa_operator, so a
//     refusal does not abort the test's transaction.
//   - poolStore: every call in its own COMMITTED transaction on its own pooled
//     connection -- the shape production has (a pool, one statement per call) and the
//     only one in which "N racers, one winner" can be observed. What it writes STAYS
//     in the development database; the tests that use it name what they leave.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/sun"
)

// ownerDSN is the migration role's URL, or a skip. Test-only owner access (redline R5
// excludes _test.go for exactly this).
func ownerDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the operator tests impersonate tappa_operator from the owner session (real Postgres required -- CLAUDE.md §8)")
	}
	return dsn
}

// asFn runs fn on a connection that is tappa_operator for the duration of one call.
type asFn func(ctx context.Context, fn func(db.OperatorConn) error) error

// opStore implements Store by calling internal/db's operator accessors through as.
type opStore struct{ as asFn }

func (s opStore) OperatorByEmail(ctx context.Context, email string) (db.OperatorAccount, error) {
	var out db.OperatorAccount
	err := s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.OperatorByEmail(ctx, c, email); return })
	return out, err
}

func (s opStore) OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	var out db.OperatorAccount
	err := s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.OperatorByID(ctx, c, id); return })
	return out, err
}

func (s opStore) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, email string, admin uuid.UUID) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.RecordOperatorAuthEvent(ctx, c, kind, email, admin) })
}

func (s opStore) OpenOperatorSession(ctx context.Context, admin uuid.UUID, h string, step int64) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.OpenOperatorSession(ctx, c, admin, h, step) })
}

func (s opStore) CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, raw, digest string, sealed []byte, step int64, h string) error {
	return s.as(ctx, func(c db.OperatorConn) error {
		return db.CompleteOperatorEnrollment(ctx, c, admin, raw, digest, sealed, step, h)
	})
}

func (s opStore) TouchOperatorSession(ctx context.Context, h string) (db.OperatorSession, error) {
	var out db.OperatorSession
	err := s.as(ctx, func(c db.OperatorConn) (e error) { out, e = db.TouchOperatorSession(ctx, c, h); return })
	return out, err
}

func (s opStore) CloseOperatorSession(ctx context.Context, h string) error {
	return s.as(ctx, func(c db.OperatorConn) error { return db.CloseOperatorSession(ctx, c, h) })
}

// operatorTablesTestLock is the cross-package advisory lock internal/db's opTx takes
// EXCLUSIVE (its 00026 tests run DDL -- Down/Up, TRUNCATE, a dropped CHECK -- on the
// operator tables inside their transactions). This package's database tests hold it
// SHARED for their whole duration, so `go test ./...`, which runs the two test binaries
// in parallel, makes them take turns: the first full run without it lost one internal/db
// test to a deadlock (40P01) and another to its lock_timeout (55P03). The SAME TEXT is
// spelled in internal/db/operatorschema_test.go;
// TestHarness_TheTablesLockIsTheOneInternalDBTakes reads that file.
const operatorTablesTestLock = "tappa/test/operator-tables"

// sharedTablesLock takes the lock SHARED at session level on conn; closing the
// connection releases it. Taken before any table lock, so it cannot join a cycle.
func sharedTablesLock(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
}

// ownerTx opens a dedicated owner connection and a REPEATABLE READ transaction rolled
// back when the test ends: every count the test takes sees its own writes and nobody
// else's, so an audit delta of 0 means "this call wrote nothing".
func ownerTx(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	dsn := ownerDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	sharedTablesLock(t, ctx, conn)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Logf("rollback: %v", err)
		}
	})
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'`); err != nil {
		t.Fatalf("lock_timeout: %v", err)
	}
	return ctx, tx
}

// txStore: each call in a savepoint of tx, as tappa_operator. On error the savepoint
// (and the SET LOCAL with it) is rolled back and the error returned; on success the
// identity is put back BEFORE the savepoint is released, because a released SET LOCAL
// survives into the parent (operatorschema_test.go's opAs, same reasoning).
func txStore(t *testing.T, tx pgx.Tx) opStore {
	return opStore{as: func(ctx context.Context, fn func(db.OperatorConn) error) error {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Errorf("savepoint: %v", err)
			return err
		}
		if _, err := sp.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
			t.Errorf("become tappa_operator: %v", err)
			_ = sp.Rollback(ctx)
			return err
		}
		if err := isOperator(ctx, sp); err != nil {
			t.Errorf("%v", err)
			_ = sp.Rollback(ctx)
			return err
		}
		if callErr := fn(sp); callErr != nil {
			if err := sp.Rollback(ctx); err != nil {
				t.Errorf("rollback to savepoint: %v", err)
			}
			return callErr
		}
		if _, err := sp.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION DEFAULT`); err != nil {
			t.Errorf("back to the owner: %v", err)
			return err
		}
		return sp.Commit(ctx)
	}}
}

// isOperator is the harness checking its own identity switch, on every call: each
// database test here is worthless if the statements run as the owner -- a superuser,
// for whom neither 00026's grants nor its RLS policy exist. Measured (OP-6
// verification, 2026-09-30): with the switch replaced by a no-op statement the whole
// package stayed green.
func isOperator(ctx context.Context, q querier) error {
	var cu, su string
	if err := q.QueryRow(ctx, `SELECT current_user::text, session_user::text`).Scan(&cu, &su); err != nil {
		return fmt.Errorf("harness: read the identity: %w", err)
	}
	if cu != "tappa_operator" || su != "tappa_operator" {
		return fmt.Errorf("harness: statements would run as current_user=%s session_user=%s, not tappa_operator", cu, su)
	}
	return nil
}

// storedUnderTokenKey reports whether the session row of admin carries
// hex(HMAC-SHA256(key, token)) -- computed HERE, independently of token.go, so the
// equality pins WHICH key the flow hashes with (ADR 0020 §2: TAPPA_OPERATOR_TOKEN_HMAC_KEY,
// its own variable) and not only that the flow agrees with itself. Nothing is printed:
// the answer is a count. Measured (OP-6 verification, 2026-09-30): hashing every token
// under the TOTP KEK, or under the derived challenge key, stayed green before this
// existed -- Verify and the sign-in agreed with each other either way.
func storedUnderTokenKey(t *testing.T, ctx context.Context, q querier, key []byte, tok SessionToken, admin uuid.UUID) bool {
	t.Helper()
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(tok.reveal()))
	want := hex.EncodeToString(m.Sum(nil))
	return countInt(t, ctx, q, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1 AND token_hash = $2`, admin, want) == 1
}

// countingStore counts the calls that follow the enrollment's paid steps, so a test
// can show a refusal reached the database or did not.
type countingStore struct {
	Store
	completes int
	byID      int
}

func (c *countingStore) OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	c.byID++
	return c.Store.OperatorByID(ctx, id)
}

func (c *countingStore) CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, raw, digest string, sealed []byte, step int64, h string) error {
	c.completes++
	return c.Store.CompleteOperatorEnrollment(ctx, admin, raw, digest, sealed, step, h)
}

// ownerPool is a pool on the owner DSN, closed when the test ends.
func ownerPool(t *testing.T, size int) *pgxpool.Pool {
	t.Helper()
	lockConn, err := pgx.Connect(context.Background(), ownerDSN(t))
	if err != nil {
		t.Fatalf("connect for the tables lock: %v", err)
	}
	t.Cleanup(func() {
		if err := lockConn.Close(context.Background()); err != nil {
			t.Logf("close: %v", err)
		}
	})
	sharedTablesLock(t, context.Background(), lockConn)
	cfg, err := pgxpool.ParseConfig(ownerDSN(t))
	if err != nil {
		t.Fatalf("parse the owner DSN: %v", err)
	}
	cfg.MaxConns = int32(size)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("owner pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// poolStore: each call in its own COMMITTED transaction as tappa_operator. It never
// calls t.Fatal (it runs on racing goroutines); errors are returned.
func poolStore(pool *pgxpool.Pool) opStore {
	return opStore{as: func(ctx context.Context, fn func(db.OperatorConn) error) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
			return err
		}
		if err := isOperator(ctx, tx); err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}}
}

// ------------------------------------------------------------------ fixtures --

// querier is what the owner-side fixture statements run on (a tx or a pool).
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// testKey returns 32 fresh random bytes -- a KEK or an HMAC key that exists for one
// test and is never written anywhere.
func testKey(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, keyLen)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

// fixturePassphrase is the sign-in passphrase of every fixture account: 24 runes,
// built at run time so no credential-shaped literal sits in the source.
var fixturePassphrase = strings.Repeat("op6 fixture ", 2)

// fixtureDigest is ONE cost-Cost digest of fixturePassphrase, made once per test
// binary: every fixture shares it, so the suite pays that bcrypt once, not per test.
var fixtureDigest = sync.OnceValues(func() (string, error) { return hashPassword(fixturePassphrase) })

type fixtureAccount struct {
	id    uuid.UUID
	email string
	key   []byte // the plain TOTP key, so the test can compute codes
}

// newActiveAccount inserts an ACTIVE operator as the owner (the only role that may --
// ADR 0020 §6): the shared digest, a fresh 160-bit TOTP key sealed under kek with the
// account id as AAD -- exactly what enrollment stores.
func newActiveAccount(t *testing.T, ctx context.Context, q querier, kek []byte) fixtureAccount {
	t.Helper()
	digest, err := fixtureDigest()
	if err != nil {
		t.Fatalf("fixture digest: %v", err)
	}
	a := fixtureAccount{id: uuid.New(), key: make([]byte, SecretBytes)}
	a.email = "op6-" + a.id.String()[:12] + "@example.test"
	if _, err := rand.Read(a.key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	sealed, err := sun.Seal(kek, a.id[:], a.key)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed)
		VALUES ($1, $2, 'op6 fixture', 'active', $3, $4)`, a.id, a.email, digest, sealed); err != nil {
		t.Fatalf("insert an active operator: %v", err)
	}
	return a
}

// newPendingAccount inserts a PENDING operator whose enrollment token (returned raw)
// is stored as EnrollmentTokenHash -- the function OP-9 will use -- issued now, alive
// for ttl, both times from ONE database clock read (the OP-9 rule, 00026's warning).
func newPendingAccount(t *testing.T, ctx context.Context, q querier, ttl time.Duration) (fixtureAccount, string) {
	t.Helper()
	tok, err := NewEnrollmentToken()
	if err != nil {
		t.Fatalf("enrollment token: %v", err)
	}
	a := fixtureAccount{id: uuid.New()}
	a.email = "op6p-" + a.id.String()[:12] + "@example.test"
	if _, err := q.Exec(ctx, `
		WITH c AS (SELECT clock_timestamp() AS now)
		INSERT INTO platform_admins (id, email, display_name, status, enroll_token_hash, enroll_issued_at, enroll_expires_at)
		SELECT $1, $2, 'op6 pending', 'pending', $3, c.now, c.now + make_interval(secs => $4) FROM c`,
		a.id, a.email, tok.Hash(), ttl.Seconds()); err != nil {
		t.Fatalf("insert a pending operator: %v", err)
	}
	return a, tok.RevealForLink()
}

// dbStepTime is a Go time in the middle of the DATABASE's current 30-second step,
// read at least three seconds before the step ends. op_open_session binds the step to
// the database's clock (cur ± 1); pinning the Authenticator's clock to the same step
// keeps Go's cur and the database's cur equal, and a boundary crossed during the test
// is still inside the database's ± 1.
func dbStepTime(t *testing.T, ctx context.Context, q querier) time.Time {
	t.Helper()
	for {
		var s int64
		var left float64
		if err := q.QueryRow(ctx, `
			SELECT floor(extract(epoch FROM clock_timestamp()) / 30)::bigint,
			       (30 - mod(extract(epoch FROM clock_timestamp()), 30))::float8`).Scan(&s, &left); err != nil {
			t.Fatalf("read the database's step: %v", err)
		}
		if left > 3 {
			return time.Unix(s*PeriodSeconds+PeriodSeconds/2, 0)
		}
		time.Sleep(time.Duration((left + 0.3) * float64(time.Second)))
	}
}

// codeAt is the six-digit code for key at t, computed with this package's own HOTP --
// which the published RFC tables pin independently (totp_test.go).
func codeAt(key []byte, t time.Time) string {
	var out [Digits]byte
	hotp(sha1New, key, uint64(step(t)), out[:])
	return string(out[:])
}

// wrongCodeAt is a six-digit code that is NOT valid anywhere in the window at t -- a
// guess that fails for certain, not with probability 1 - 3e-6.
func wrongCodeAt(key []byte, t time.Time) string {
	valid := map[string]bool{}
	for d := int64(-SkewSteps); d <= SkewSteps; d++ {
		valid[codeAt(key, t.Add(time.Duration(d*PeriodSeconds)*time.Second))] = true
	}
	for c := 0; ; c++ {
		s := strings.Repeat("0", Digits) + itoa(c)
		s = s[len(s)-Digits:]
		if !valid[s] {
			return s
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// spentIn is how many events key has charged in b's current window (0 if none).
func spentIn(b *budget, key string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if w, ok := b.windows[key]; ok {
		return w.count
	}
	return 0
}

// countInt reads one integer as the owner.
func countInt(t *testing.T, ctx context.Context, q querier, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := q.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// auditRows counts the operator audit table in the test's snapshot.
func auditRows(t *testing.T, ctx context.Context, q querier) int64 {
	t.Helper()
	return countInt(t, ctx, q, `SELECT count(*) FROM operator_audit_log`)
}

// failures reads an account's TOTP failure counter (the lock's input, 00026).
func failures(t *testing.T, ctx context.Context, q querier, id uuid.UUID) int64 {
	t.Helper()
	return countInt(t, ctx, q, `SELECT totp_failures FROM platform_admins WHERE id = $1`, id)
}

// sharedDummy is ONE real cost-Cost dummy digest for every Authenticator a test builds
// through newAuth: New pays a cost-12 bcrypt per call, and under -race in the full suite
// that made this package take 380 s and stretched internal/db (which waits on the shared
// tables lock) from about 100 s to 331 s. The production constructor -- New making its
// own dummy -- is driven by TestPassword_TheDigestAndTheDummyAreBothCostTwelve and by
// the external tests.
var sharedDummy = sync.OnceValues(newDummyDigest)

// newAuth builds an Authenticator over store with fresh keys and the clock fixed at
// now, through New's own refusals and build (the constructor minus the dummy's
// bcrypt). The logger writes into logs, so a test can read what was logged.
func newAuth(t *testing.T, store Store, now time.Time) (*Authenticator, *strings.Builder) {
	t.Helper()
	var logs strings.Builder
	cfg := Config{
		TOTPKEK:      NewKey(testKey(t)),
		TokenHMACKey: NewKey(testKey(t)),
		Now:          func() time.Time { return now },
		Log:          newTestLogger(&logs),
	}
	if err := checkConfig(store, cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	dummy, err := sharedDummy()
	if err != nil {
		t.Fatalf("dummy digest: %v", err)
	}
	return build(store, cfg, dummy), &logs
}

// warmDigests pays the two shared bcrypts (the dummy and the fixture digest) BEFORE a
// database test takes the shared tables lock and opens its transaction, so internal/db's
// DDL tests, which wait on that lock, do not also wait for key schedules (a cost-12
// bcrypt is ~4.4 s under -race on this machine).
func warmDigests(t *testing.T) {
	t.Helper()
	if _, err := sharedDummy(); err != nil {
		t.Fatalf("dummy digest: %v", err)
	}
	if _, err := fixtureDigest(); err != nil {
		t.Fatalf("fixture digest: %v", err)
	}
}

// challengeFor is the password step's product minted directly, for tests whose subject
// is the TOTP step or the session and not the password: it saves a cost-12 comparison
// per test (the password step itself is TestPassword_EveryArmPaysOneComparisonAtTheSameCost's).
func challengeFor(t *testing.T, a *Authenticator, acc fixtureAccount) Challenge {
	t.Helper()
	c, err := a.mintChallenge(acc.id)
	if err != nil {
		t.Fatalf("mint a challenge: %v", err)
	}
	return c
}

// TestHarness_TheTablesLockIsTheOneInternalDBTakes: the two packages must name the same
// advisory lock, or they stop taking turns and the collision the lock exists for comes
// back as an occasional deadlock. The spelling is read from internal/db's source.
func TestHarness_TheTablesLockIsTheOneInternalDBTakes(t *testing.T) {
	src, err := os.ReadFile("../db/operatorschema_test.go")
	if err != nil {
		t.Fatalf("read internal/db/operatorschema_test.go: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, `const operatorTablesTestLock = "`+operatorTablesTestLock+`"`) ||
		!strings.Contains(s, "pg_advisory_lock(hashtext($1))`, operatorTablesTestLock") {
		t.Fatalf("internal/db's opTx no longer takes the %q advisory lock EXCLUSIVE; the two packages' operator-table tests would race again", operatorTablesTestLock)
	}
}

// must is the fixture shorthand for a token that cannot fail to be minted outside a
// broken crypto/rand.
func must(t EnrollmentToken, err error) EnrollmentToken {
	if err != nil {
		panic("mint an enrollment token: " + err.Error())
	}
	return t
}
