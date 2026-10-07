package operatorauth

// passwordok_db_test.go -- OP-14 phase C against PostgreSQL: the 'password_ok' row the
// password step writes through the real op_record_auth_event (00031), on the harness's
// rolled-back transaction (harness_db_test.go's txStore). NOTHING HERE COMMITS: every
// row, account and counter below goes with the test's transaction.

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// readOnlyStore is txStore with the call's savepoint made READ ONLY first: every write
// the call attempts is refused by the server itself (SQLSTATE 25006), and the savepoint
// is always rolled back, which undoes the SET LOCAL with it (measured on the development
// database, 2026-10-07: transaction_read_only reads "off" again after ROLLBACK TO
// SAVEPOINT). A database that still answers the lookup but refuses the write -- not a
// stand-in error.
func readOnlyStore(t *testing.T, tx pgx.Tx) opStore {
	return opStore{as: func(ctx context.Context, fn func(db.OperatorConn) error) error {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Errorf("savepoint: %v", err)
			return err
		}
		defer func() {
			if err := sp.Rollback(ctx); err != nil {
				t.Errorf("rollback to savepoint: %v", err)
			}
		}()
		for _, s := range []string{`SET LOCAL transaction_read_only = on`, `SET LOCAL SESSION AUTHORIZATION tappa_operator`} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Errorf("%s: %v", s, err)
				return err
			}
		}
		if err := isOperator(ctx, sp); err != nil {
			t.Errorf("%v", err)
			return err
		}
		return fn(sp)
	}}
}

// refusingStore sends the writes it names to a read-only savepoint and everything else to
// the real store.
type refusingStore struct {
	Store
	ro         Store
	passwordOK bool // op_record_auth_event('password_ok', ...) is refused
	open       bool // op_open_session is refused
}

func (s refusingStore) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, email string, id uuid.UUID) error {
	if s.passwordOK && kind == db.OperatorPasswordOK {
		return s.ro.RecordOperatorAuthEvent(ctx, kind, email, id)
	}
	return s.Store.RecordOperatorAuthEvent(ctx, kind, email, id)
}

func (s refusingStore) OpenOperatorSession(ctx context.Context, id uuid.UUID, h string, step int64) error {
	if s.open {
		return s.ro.OpenOperatorSession(ctx, id, h, step)
	}
	return s.Store.OpenOperatorSession(ctx, id, h, step)
}

// operatorRows is the kinds of the rows that name id -- as the target or as the actor --
// in the order they were written.
func operatorRows(t *testing.T, ctx context.Context, q querier, id uuid.UUID) []string {
	t.Helper()
	var kinds []string
	if err := q.QueryRow(ctx, `SELECT coalesce(array_agg(kind ORDER BY at, id), '{}') FROM operator_audit_log
	                            WHERE target_admin_id = $1 OR actor_admin_id = $1`, id).Scan(&kinds); err != nil {
		t.Fatalf("read the operator's rows: %v", err)
	}
	return kinds
}

// passwordOKRows counts id's 'password_ok' rows and, of those, the ones in the shape
// 00031 gives the kind: no session, no actor, no tenant, no scope, no page, an empty
// detail -- an operator named by id and nothing else.
func passwordOKRows(t *testing.T, ctx context.Context, q querier, id uuid.UUID) (all, shaped int64) {
	t.Helper()
	if err := q.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE session_id IS NULL AND actor_admin_id IS NULL AND target_tenant_id IS NULL
		                          AND target_scope IS NULL AND page_number IS NULL AND page_size IS NULL
		                          AND detail = '{}'::jsonb)
		  FROM operator_audit_log WHERE kind = 'password_ok' AND target_admin_id = $1`, id).Scan(&all, &shaped); err != nil {
		t.Fatalf("read the password_ok rows: %v", err)
	}
	return all, shaped
}

// rowText is every audit row naming id, as JSON text, for a search.
func rowText(t *testing.T, ctx context.Context, q querier, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `SELECT coalesce(string_agg(row_to_json(l)::text, ' '), '') FROM operator_audit_log l
	                            WHERE l.target_admin_id = $1 OR l.actor_admin_id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read the rows as text: %v", err)
	}
	return s
}

// TestPasswordOK_EachSignInArmLeavesItsRows is the OP-14 C acceptance, arm by arm,
// through the real definers: a right password whose code never comes leaves ONE
// 'password_ok' row and nothing else -- the trail OP-6 12c left only in the process log;
// with the right code it is followed by the 'login' row; with a wrong code by a
// 'totp_failed' row (which alone moves the lock counter); a wrong password leaves its
// 'login_failed' row and NO 'password_ok'. Each row is in its kind's shape, the order is
// the order of the steps, and the operator's rows carry neither the client address nor
// the operator's address. Below the caps the sign-in logs NOTHING (the 12c Info line is
// gone: the row replaced it).
func TestPasswordOK_EachSignInArmLeavesItsRows(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, logs := newAuth(t, txStore(t, tx), now)
	right := func(fixtureAccount) string { return fixturePassphrase }
	tests := []struct {
		name     string
		password func(fixtureAccount) string
		passErr  error
		code     func(key []byte, now time.Time) string // nil: the code step never comes
		codeErr  error
		want     []string
		counter  int64
	}{
		{"right password, the code never comes", right, nil, nil, nil, []string{"password_ok"}, 0},
		{"right password, right code", right, nil, codeAt, nil, []string{"password_ok", "login"}, 0},
		{"right password, wrong code", right, nil, wrongCodeAt, ErrCodeRejected, []string{"password_ok", "totp_failed"}, 1},
		{"wrong password", func(fixtureAccount) string { return fixturePassphrase + "!" }, ErrRefused, nil, nil, []string{"login_failed"}, 0},
	}
	for _, tc := range tests {
		acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
		before := auditRows(t, ctx, tx)
		c, err := a.Password(ctx, testAddr, strings.ToUpper(acc.email), tc.password(acc))
		if !errors.Is(err, tc.passErr) || (err == nil) != (c.reveal() != "") {
			t.Fatalf("%s: the password step: %v (challenge minted=%v), want %v", tc.name, err, c.reveal() != "", tc.passErr)
		}
		// The trail is in the database by the time the step returns its challenge (C2).
		if all, _ := passwordOKRows(t, ctx, tx, acc.id); err == nil && all != 1 {
			t.Fatalf("%s: %d 'password_ok' row(s) when the challenge was returned, want 1", tc.name, all)
		}
		if tc.code != nil {
			now = resyncAuth(t, ctx, tx, a)
			if _, err := a.TOTP(ctx, c, tc.code(acc.key, now)); !errors.Is(err, tc.codeErr) {
				t.Fatalf("%s: the code step: %v, want %v", tc.name, err, tc.codeErr)
			}
		}
		got := operatorRows(t, ctx, tx, acc.id)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") || auditRows(t, ctx, tx)-before != int64(len(tc.want)) {
			t.Errorf("%s: rows %v (%+d in all), want %v", tc.name, got, auditRows(t, ctx, tx)-before, tc.want)
		}
		all, shaped := passwordOKRows(t, ctx, tx, acc.id)
		if wantOK := int64(strings.Count(strings.Join(tc.want, ","), "password_ok")); all != wantOK || shaped != wantOK {
			t.Errorf("%s: %d 'password_ok' row(s), %d of them in the kind's shape (no session, actor, tenant, scope, page; detail {}); want %d",
				tc.name, all, shaped, wantOK)
		}
		text := strings.ToLower(rowText(t, ctx, tx, acc.id))
		if strings.Contains(text, strings.ToLower(acc.email)) || strings.Contains(text, testAddr) {
			t.Errorf("%s: a row carries the operator's address or the client's", tc.name)
		}
		if f := failures(t, ctx, tx, acc.id); f != tc.counter {
			t.Errorf("%s: the lock counter is %d, want %d", tc.name, f, tc.counter)
		}
	}
	if logs.Len() != 0 {
		t.Errorf("the sign-in below the caps logged %d byte(s); want none (the row is the trail)", logs.Len())
	}
}

// TestPasswordOK_PasswordlessJunkCannotSilenceIt is the OP-14 card's C1 through the real
// definer: with the process-wide cap on password-LESS rows spent -- the PREMISE is
// measured: an unknown address's row is now silent -- a right password still writes its
// 'password_ok' row, charges its operator's cap and not the shared one. OP-6 md. 9
// measured the same conflict for totp_failed: a kind under the shared cap is a kind
// anyone can silence.
func TestPasswordOK_PasswordlessJunkCannotSilenceIt(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	a, _ := newAuth(t, txStore(t, tx), dbStepTime(t, ctx, tx))
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	for a.limits.auditCap.charge("") < auditCapLimit {
	}
	before := auditRows(t, ctx, tx)
	if _, err := a.Password(ctx, "203.0.113.20", "nobody-"+uuid.NewString()[:6]+"@example.test", fixturePassphrase); !errors.Is(err, ErrRefused) {
		t.Fatalf("PREMISE: an unknown address: %v, want ErrRefused", err)
	}
	if d := auditRows(t, ctx, tx) - before; d != 0 {
		t.Fatalf("PREMISE: with the shared cap spent an unknown address still wrote %d row(s); the cap is not spent", d)
	}
	shared := spentIn(a.limits.auditCap, "")
	c, err := a.Password(ctx, "203.0.113.21", acc.email, fixturePassphrase)
	if err != nil || c.reveal() == "" {
		t.Fatalf("the right password: %v", err)
	}
	if all, shaped := passwordOKRows(t, ctx, tx, acc.id); all != 1 || shaped != 1 || auditRows(t, ctx, tx)-before != 1 {
		t.Fatalf("with the shared cap spent, the right password wrote %d 'password_ok' row(s) (%+d in all), want 1: password-less junk silenced it",
			all, auditRows(t, ctx, tx)-before)
	}
	if spentIn(a.limits.auditCap, "") != shared || spentIn(a.limits.firstFactor, acc.id.String()) != 1 {
		t.Fatalf("the right password charged the shared cap %+d time(s) and its operator's %d; want 0 and 1",
			spentIn(a.limits.auditCap, "")-shared, spentIn(a.limits.firstFactor, acc.id.String()))
	}
}

// TestPasswordOK_PastTheOperatorsCapNoRowOneWarn: an operator one row short of the cap;
// three right passwords from three addresses -- the first writes the last row of the
// window, the next two write none and are SERVED all the same; ONE WARN line names the
// operator -- none after the cap's last row is written (2nd round, the third eye's B4: a
// line on the last WRITTEN row stayed green), one from the first row not written -- and
// no line carries an address. Another operator's row is still written.
func TestPasswordOK_PastTheOperatorsCapNoRowOneWarn(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	a, logs := newAuth(t, txStore(t, tx), dbStepTime(t, ctx, tx))
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	other := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	// One row short of the cap, nine of them WRITTEN (3rd round, F1: past the cap a request
	// is served without a row only once the cap's rows are written).
	for i := 0; i < firstFactorLimit-1; i++ {
		_, _, w, _ := a.limits.firstFactor.take(acc.id.String())
		a.limits.firstFactor.wrote(w)
	}
	before := auditRows(t, ctx, tx)
	addrs := []string{"198.51.100.31", "198.51.100.32", "198.51.100.33"}
	for i, addr := range addrs {
		c, err := a.Password(ctx, addr, acc.email, fixturePassphrase)
		if err != nil || c.reveal() == "" {
			t.Fatalf("right password %d, at or past the operator's cap: not served (%v)", i+1, err)
		}
		if all, _ := passwordOKRows(t, ctx, tx, acc.id); all != 1 {
			t.Fatalf("right password %d: %d 'password_ok' row(s), want 1 (the cap's last, then none)", i+1, all)
		}
		// The line is for the first row NOT written: none after the cap's last row.
		if w, want := strings.Count(logs.String(), trailCapMsg), min(i, 1); w != want {
			t.Fatalf("right password %d: %d WARN line(s), want %d", i+1, w, want)
		}
	}
	if d := auditRows(t, ctx, tx) - before; d != 1 {
		t.Fatalf("three right passwords at the cap wrote %d row(s) in all, want 1", d)
	}
	if _, err := a.Password(ctx, addrs[0], other.email, fixturePassphrase); err != nil {
		t.Fatalf("another operator's right password: %v", err)
	}
	if all, _ := passwordOKRows(t, ctx, tx, other.id); all != 1 {
		t.Fatalf("another operator, while the first is past its cap: %d row(s), want 1", all)
	}
	checkOnlyTrailCapLines(t, logs.String(), acc.id)
	for _, s := range append(addrs, acc.email, other.email) {
		if strings.Contains(logs.String(), s) {
			t.Fatal("the log carries a client address or an operator's address")
		}
	}
}

// TestPasswordOK_ARowThatCannotBeWrittenFailsTheStep: the database answers the lookup and
// REFUSES the 'password_ok' write (a read-only savepoint: the server's own 25006). The
// password step fails CLOSED: an error that is none of the sentinels a caller answers
// with a page (so OP-8's handler answers 503), NO challenge -- its cookie setter refuses
// the zero value and writes no header -- and no row, and (2nd round) the operator's cap
// gets its charge back. The error and the log carry neither
// the operator's address, nor the client's, nor the password, nor the operator's id.
//
// THE PRICE, MEASURED: the same refusal on the NEXT step's write stops op_open_session
// too -- the sign-in already needed a writable database one step later. CONTROL: the same
// account through the real store writes its row and gets its challenge.
func TestPasswordOK_ARowThatCannotBeWrittenFailsTheStep(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	store := txStore(t, tx)
	a, logs := newAuth(t, refusingStore{Store: store, ro: readOnlyStore(t, tx), passwordOK: true}, now)
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	before := auditRows(t, ctx, tx)
	c, err := a.Password(ctx, testAddr, acc.email, fixturePassphrase)
	if err == nil {
		t.Fatal("the password step succeeded although its 'password_ok' row was refused")
	}
	for _, s := range []error{ErrRefused, ErrThrottled, ErrChallenge, ErrCodeRejected, ErrLocked, ErrNoSession} {
		if errors.Is(err, s) {
			t.Fatalf("a refused trail answered as %v; it is a server failure, not an answer about the credential", s)
		}
	}
	if !strings.Contains(err.Error(), "SQLSTATE 25006") {
		t.Fatalf("PREMISE: the failure is not the server's read-only refusal: %v", err)
	}
	if c.reveal() != "" {
		t.Fatal("a challenge was minted although its trail was not written")
	}
	w := httptest.NewRecorder()
	if SetChallengeCookie(w, c) == nil || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("the step's zero challenge could be set as a cookie")
	}
	if d := auditRows(t, ctx, tx) - before; d != 0 {
		t.Fatalf("the refused step wrote %d row(s)", d)
	}
	// 2nd round (the third eye's B1, its database half): the server's refusal gives the
	// operator's cap its charge back -- a failover cannot spend the cap without rows.
	if spent := spentIn(a.limits.firstFactor, acc.id.String()); spent != 0 {
		t.Fatalf("the refused write left the operator's cap at %d, want 0 (its charge given back)", spent)
	}
	for _, s := range []string{acc.email, strings.ToUpper(acc.email), testAddr, fixturePassphrase, acc.id.String()} {
		for _, r := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
			if strings.Contains(r, s) {
				t.Fatal("the error carries the operator's address, the client's, the password or the operator's id")
			}
		}
		// The log names the operator -- by id, once, in the refusal line (3rd round, F4) --
		// and carries nothing else.
		if s != acc.id.String() && strings.Contains(logs.String(), s) {
			t.Fatal("the log carries the operator's address, the client's or the password")
		}
	}
	checkFirstFactorLog(t, logs.String(), nil, []uuid.UUID{acc.id})

	// The same Authenticator (its KEK opens the account's envelope), its store swapped.
	a.store = refusingStore{Store: store, ro: readOnlyStore(t, tx), open: true}
	if _, err := a.TOTP(ctx, challengeFor(t, a, acc), codeAt(acc.key, now)); err == nil || !strings.Contains(err.Error(), "SQLSTATE 25006") {
		t.Fatalf("THE PRICE: the same refusal on op_open_session: %v, want the server's 25006", err)
	}
	if n := countInt(t, ctx, tx, `SELECT count(*) FROM platform_sessions WHERE admin_id = $1`, acc.id); n != 0 {
		t.Fatalf("THE PRICE: %d session(s) opened through a refused write", n)
	}

	a.store = store
	if c, err := a.Password(ctx, testAddr, acc.email, fixturePassphrase); err != nil || c.reveal() == "" {
		t.Fatalf("CONTROL: the right password through the real store: %v", err)
	}
	if all, _ := passwordOKRows(t, ctx, tx, acc.id); all != 1 {
		t.Fatalf("CONTROL: %d 'password_ok' row(s), want 1", all)
	}
}

// TestPasswordOK_TouchesNoLockCounter: the 'password_ok' row moves no lock counter, in
// either direction. An operator with three failures: a right password leaves the counter
// at 3 and the account unlocked, and two wrong codes then lock it at 5 -- the count went
// on from 3. Locked: a right password writes its row and neither resets the counter nor
// shortens the lock, and the right code is still refused for the lock. (The database
// half -- op_record_auth_event's own UPDATE -- is internal/db's
// TestOpRecordAuthEvent_PasswordOKNamesItsAccountAndTouchesNoCounter; this is the
// password step's half: the row it writes, through the definer it calls.)
func TestPasswordOK_TouchesNoLockCounter(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	now := dbStepTime(t, ctx, tx)
	a, _ := newAuth(t, txStore(t, tx), now)
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_failures = 3 WHERE id = $1`, acc.id); err != nil {
		t.Fatalf("count three failures: %v", err)
	}
	lockedUntil := func() *time.Time {
		t.Helper()
		var u *time.Time
		if err := tx.QueryRow(ctx, `SELECT totp_locked_until FROM platform_admins WHERE id = $1`, acc.id).Scan(&u); err != nil {
			t.Fatalf("read the lock: %v", err)
		}
		return u
	}
	c, err := a.Password(ctx, testAddr, acc.email, fixturePassphrase)
	if err != nil {
		t.Fatalf("the right password: %v", err)
	}
	if f, u := failures(t, ctx, tx, acc.id), lockedUntil(); f != 3 || u != nil {
		t.Fatalf("after a right password: counter %d, locked until %v; want 3 and no lock", f, u)
	}
	for i := 0; i < 2; i++ {
		if _, err := a.TOTP(ctx, c, wrongCodeAt(acc.key, now)); !errors.Is(err, ErrCodeRejected) {
			t.Fatalf("wrong code %d: %v", i+1, err)
		}
	}
	locked := lockedUntil()
	if f := failures(t, ctx, tx, acc.id); f != 5 || locked == nil {
		t.Fatalf("two wrong codes after it: counter %d, locked=%v; want 5 and locked (the count went on from 3)", f, locked != nil)
	}
	if _, err := a.Password(ctx, "198.51.100.41", acc.email, fixturePassphrase); err != nil {
		t.Fatalf("the right password on a locked account (the password step does not read the lock): %v", err)
	}
	if f, u := failures(t, ctx, tx, acc.id), lockedUntil(); f != 5 || u == nil || !u.Equal(*locked) {
		t.Fatalf("a right password while locked: counter %d, lock moved=%v; want 5 and the same lock", f, u == nil || !u.Equal(*locked))
	}
	if all, _ := passwordOKRows(t, ctx, tx, acc.id); all != 2 {
		t.Fatalf("%d 'password_ok' row(s), want 2", all)
	}
	now = resyncAuth(t, ctx, tx, a)
	if _, err := a.TOTP(ctx, c, codeAt(acc.key, now)); !errors.Is(err, ErrLocked) {
		t.Fatalf("the right code after it: %v, want ErrLocked", err)
	}
}

// TestPasswordOK_AnAbortedRightPasswordStillLeavesItsRow is the third eye's B1 (OP-14 C,
// 2nd round) on the real definer: the client hangs up while the comparison runs --
// net/http cancels r.Context(), here the comparison cancels the request's context
// itself. Each of firstFactorLimit such right passwords is still SERVED and still leaves
// its 'password_ok' row: the row is written on a context detached from the request's
// cancellation (FirstFactorRecordGrace). The next right password, not aborted, is past
// the cap: served, no row, ONE WARN line -- and the window holds firstFactorLimit rows,
// not zero. Before the detach (measured by the third eye, and by mutation M19 here) the
// done context reached the write, nothing was sent, the cap was charged, and the window's
// trail could be emptied.
func TestPasswordOK_AnAbortedRightPasswordStillLeavesItsRow(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	a, logs := newAuth(t, txStore(t, tx), dbStepTime(t, ctx, tx))
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	var hangUp context.CancelFunc
	compare := a.compareFn
	a.compareFn = func(d, p []byte) error {
		if hangUp != nil {
			hangUp()
		}
		return compare(d, p)
	}
	for i := 1; i <= firstFactorLimit; i++ {
		rctx, cancel := context.WithCancel(ctx)
		hangUp = cancel
		addr := fmt.Sprintf("198.51.100.%d", 100+i)
		c, err := a.Password(rctx, addr, acc.email, fixturePassphrase)
		if rctx.Err() == nil {
			t.Fatal("PREMISE: the request's context was not cancelled during the comparison")
		}
		cancel()
		if err != nil || c.reveal() == "" {
			t.Fatalf("aborted right password %d: %v (challenge minted=%v); want it served", i, err, c.reveal() != "")
		}
		if all, shaped := passwordOKRows(t, ctx, tx, acc.id); all != int64(i) || shaped != int64(i) {
			t.Fatalf("after %d aborted right password(s): %d 'password_ok' row(s), want %d", i, all, i)
		}
	}
	hangUp = nil
	if logs.Len() != 0 {
		t.Fatalf("within the cap the aborted requests logged %d byte(s), want none", logs.Len())
	}
	c, err := a.Password(ctx, "198.51.100.200", acc.email, fixturePassphrase)
	if err != nil || c.reveal() == "" {
		t.Fatalf("the next right password, past the cap: %v; want it served", err)
	}
	if all, _ := passwordOKRows(t, ctx, tx, acc.id); all != firstFactorLimit {
		t.Fatalf("the window holds %d 'password_ok' row(s), want %d", all, firstFactorLimit)
	}
	checkOnlyTrailCapLines(t, logs.String(), acc.id)
}

// lockedStore serialises every call on the test's one connection: a pgx.Tx is not safe for
// concurrent use, and the 3rd round's F1 test races requests over it.
type lockedStore struct {
	mu *sync.Mutex
	in Store
}

func (s lockedStore) OperatorByEmail(ctx context.Context, e string) (db.OperatorAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in.OperatorByEmail(ctx, e)
}

func (s lockedStore) OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in.OperatorByID(ctx, id)
}

func (s lockedStore) RecordOperatorAuthEvent(ctx context.Context, k db.OperatorAuthEvent, e string, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in.RecordOperatorAuthEvent(ctx, k, e, id)
}

func (s lockedStore) OpenOperatorSession(ctx context.Context, id uuid.UUID, h string, step int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in.OpenOperatorSession(ctx, id, h, step)
}

func (s lockedStore) CompleteOperatorEnrollment(ctx context.Context, id uuid.UUID, raw, d string, sealed []byte, step int64, h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in.CompleteOperatorEnrollment(ctx, id, raw, d, sealed, step, h)
}

func (s lockedStore) TouchOperatorSession(ctx context.Context, h string) (db.OperatorSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in.TouchOperatorSession(ctx, h)
}

func (s lockedStore) CloseOperatorSession(ctx context.Context, h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.in.CloseOperatorSession(ctx, h)
}

// gatedStore holds every 'password_ok' write at gate (counting the ones held), then sends
// it to to -- the real store, or a read-only one (the server's 25006): a database whose
// writes stall and then land or fail (a failover, a saturated pool).
type gatedStore struct {
	Store
	to   Store
	gate chan struct{}
	held *atomic.Int64
}

func (g gatedStore) RecordOperatorAuthEvent(ctx context.Context, k db.OperatorAuthEvent, e string, id uuid.UUID) error {
	if k != db.OperatorPasswordOK {
		return g.Store.RecordOperatorAuthEvent(ctx, k, e, id)
	}
	g.held.Add(1)
	<-g.gate
	return g.to.RecordOperatorAuthEvent(ctx, k, e, id)
}

// exactCompare accepts exactly the fixture passphrase: the comparison is not these tests'
// subject, and eleven cost-12 comparisons under -race would take most of a minute.
func exactCompare(_, p []byte) error {
	if string(p) == fixturePassphrase {
		return nil
	}
	return errors.New("operatorauth test: not the fixture passphrase")
}

// TestPasswordOK_DB_ACapFullOfUnwrittenRowsServesNoChallenge is the security audit's P2
// (OP-14 C, 3rd round, F1) on the real definer. firstFactorLimit right passwords whose
// 'password_ok' writes are held in flight, and one more right password while they are:
//   - writes that then FAIL (the server's 25006): the extra request was REFUSED -- no
//     challenge -- and the window holds 0 rows with the cap at 0 (before F1 it got a
//     challenge, the window 0 rows: the trail of B1, by concurrency and an outage);
//   - writes that then LAND: the extra request was refused all the same (its rows were not
//     written when it came), the window holds firstFactorLimit rows, and the next right
//     password is served past the cap without a row, with the one suppression line.
func TestPasswordOK_DB_ACapFullOfUnwrittenRowsServesNoChallenge(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	mu := &sync.Mutex{}
	live, ro := lockedStore{mu: mu, in: txStore(t, tx)}, lockedStore{mu: mu, in: readOnlyStore(t, tx)}
	now := dbStepTime(t, ctx, tx)
	for _, writesFail := range []bool{true, false} {
		var held atomic.Int64
		gate := make(chan struct{})
		to := Store(live)
		if writesFail {
			to = ro
		}
		a, logs := newAuth(t, gatedStore{Store: live, to: to, gate: gate, held: &held}, now)
		a.compareFn = exactCompare
		mu.Lock()
		acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
		mu.Unlock()
		results := make(chan error, firstFactorLimit)
		for i := 0; i < firstFactorLimit; i++ {
			addr := fmt.Sprintf("198.51.100.%d", 10+i)
			go func() {
				_, err := a.Password(ctx, addr, acc.email, fixturePassphrase)
				results <- err
			}()
		}
		deadline := time.Now().Add(30 * time.Second)
		for held.Load() < firstFactorLimit && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if held.Load() != firstFactorLimit {
			t.Fatalf("PREMISE: %d write(s) in flight, want %d", held.Load(), firstFactorLimit)
		}
		// The extra request runs aside, bounded: one that writes a row of its own would wait at
		// the gate (a mutant without the cap does), and the test must say so, not hang.
		type answer struct {
			c   Challenge
			err error
		}
		extra := make(chan answer, 1)
		go func() {
			c, err := a.Password(ctx, "198.51.100.250", acc.email, fixturePassphrase)
			extra <- answer{c, err}
		}()
		var got answer
		select {
		case got = <-extra:
		case <-time.After(10 * time.Second):
			close(gate)
			t.Fatalf("writes fail=%v: the request past a cap of writes in flight did not return -- it waits on a write of its own", writesFail)
		}
		c, err := got.c, got.err
		close(gate)
		failed := 0
		for i := 0; i < firstFactorLimit; i++ {
			if <-results != nil {
				failed++
			}
		}
		if !errors.Is(err, errFirstFactorPending) || c.reveal() != "" {
			t.Fatalf("writes fail=%v: the right password past a cap of writes in flight: %v (challenge minted=%v); want errFirstFactorPending and no challenge",
				writesFail, err, c.reveal() != "")
		}
		mu.Lock()
		rows, _ := passwordOKRows(t, ctx, tx, acc.id)
		mu.Unlock()
		spent := spentIn(a.limits.firstFactor, acc.id.String())
		if writesFail {
			if failed != firstFactorLimit || rows != 0 || spent != 0 {
				t.Fatalf("writes fail: %d of %d held step(s) failed, %d row(s), the cap at %d; want all, 0 and 0", failed, firstFactorLimit, rows, spent)
			}
			checkFirstFactorLog(t, logs.String(), nil, []uuid.UUID{acc.id})
			continue
		}
		if failed != 0 || rows != firstFactorLimit || spent != firstFactorLimit {
			t.Fatalf("writes land: %d held step(s) failed, %d row(s), the cap at %d; want 0, %d and %d", failed, rows, spent, firstFactorLimit, firstFactorLimit)
		}
		if c, err := a.Password(ctx, "198.51.100.251", acc.email, fixturePassphrase); err != nil || c.reveal() == "" {
			t.Fatalf("writes land: the next right password, past a cap of WRITTEN rows: %v; want it served", err)
		}
		checkFirstFactorLog(t, logs.String(), []uuid.UUID{acc.id}, []uuid.UUID{acc.id})
	}
}

// TestPasswordOK_WrongPasswordsToItsAddressNeverSpendTheCap is the security audit's P7
// (OP-14 C, 3rd round, F2): "only a right password spends the operator's cap" through the
// password-less route anyone who knows the address has -- wrong passwords to the
// operator's OWN address, more than firstFactorLimit of them. Each is refused and charges
// nothing of the operator's cap -- nor opens its window (5th round, D2: a written count
// is pinned too, not only the charge); then the right password writes its row and the
// cap holds exactly that one charge and that one written row.
func TestPasswordOK_WrongPasswordsToItsAddressNeverSpendTheCap(t *testing.T) {
	warmDigests(t)
	ctx, tx := ownerTx(t)
	a, _ := newAuth(t, txStore(t, tx), dbStepTime(t, ctx, tx))
	a.compareFn = exactCompare
	acc := newActiveAccount(t, ctx, tx, a.keys.kek.bytes())
	for i := 0; i < firstFactorLimit+2; i++ {
		addr := fmt.Sprintf("198.51.100.%d", 150+i)
		if _, err := a.Password(ctx, addr, acc.email, fixturePassphrase+"x"); !errors.Is(err, ErrRefused) {
			t.Fatalf("PREMISE: wrong password %d: %v", i+1, err)
		}
	}
	if spent := spentIn(a.limits.firstFactor, acc.id.String()); spent != 0 {
		t.Fatalf("%d wrong passwords to the operator's address charged its first-factor cap %d time(s), want 0", firstFactorLimit+2, spent)
	}
	// 5th round (the closing audit's D2): nor do they count as WRITTEN rows -- a window
	// they opened could hold a written count no row stands for, and past the cap that
	// count is what serves a request without a row. They open no window at all.
	if w, ok := windowState(a.limits.firstFactor, acc.id.String()); ok {
		t.Fatalf("%d wrong passwords to the operator's address opened its first-factor window (counter %d, written %d); want none",
			firstFactorLimit+2, w.count, w.written)
	}
	c, err := a.Password(ctx, "198.51.100.220", acc.email, fixturePassphrase)
	all, _ := passwordOKRows(t, ctx, tx, acc.id)
	w, _ := windowState(a.limits.firstFactor, acc.id.String())
	if spent := spentIn(a.limits.firstFactor, acc.id.String()); err != nil || c.reveal() == "" || all != 1 || spent != 1 || w.written != 1 {
		t.Fatalf("then the right password: %v (challenge minted=%v), %d 'password_ok' row(s), the cap at %d, written %d; want served, 1, 1 and 1",
			err, c.reveal() != "", all, spent, w.written)
	}
}
