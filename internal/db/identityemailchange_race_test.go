package db

// identityemailchange_race_test.go -- T173 EC-1 (00102, ADR 0044 §5.3, §5.6) with COMMITTED data and real
// concurrency (run under -race): two presses of one link, one new address raced by a change, a
// registration and an invitation, a change beside a reset consumption, S-1 (a reset opened while the
// address moves) and S-2 (the undo beside the person's own session writers), and the unlink's clock read
// after its locks. Every world is removed again by its t.Cleanup (the identities, their links, resets and
// events; the rows unlinked) -- the tenants and the audit rows stay, as identity_db_test.go's do.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ecLive is a committed person: a VERIFIED identity, a linked row in each of its businesses, a live
// session of the first.
type ecLive struct {
	email   string
	ident   uuid.UUID
	tenants []uuid.UUID
	rows    []uuid.UUID
	session string
}

func ecOwner(t *testing.T) *pgx.Conn {
	t.Helper()
	owner, err := pgx.Connect(context.Background(), opOwnerDSN(t))
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	return owner
}

func ecExec(t *testing.T, owner *pgx.Conn, sql string, args ...any) {
	t.Helper()
	if _, err := owner.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func ecInt(t *testing.T, owner *pgx.Conn, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := owner.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// ecForget removes what an identity left: its rows unlinked, its links, resets, events, the identity.
func ecForget(t *testing.T, owner *pgx.Conn, ids ...uuid.UUID) {
	t.Helper()
	for _, id := range ids {
		for _, s := range []string{
			`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL, unlinked_at = NULL WHERE identity_id = $1`,
			`DELETE FROM admin_identity_verifications WHERE identity_id = $1`,
			`DELETE FROM admin_identity_resets WHERE identity_id = $1`,
			`DELETE FROM admin_identity_events WHERE identity_id = $1`,
			`DELETE FROM admin_identities WHERE id = $1`,
		} {
			if _, err := owner.Exec(context.Background(), s, id); err != nil {
				t.Errorf("cleanup %s: %v", s, err)
			}
		}
	}
}

func newECLive(t *testing.T, owner *pgx.Conn, n int) *ecLive {
	t.Helper()
	w := &ecLive{email: ecNew()}
	if err := owner.QueryRow(context.Background(), `INSERT INTO admin_identities (email, password_hash, email_verified_at) VALUES ($1, $2, now() - interval '1 day') RETURNING id`,
		w.email, ecDigest()).Scan(&w.ident); err != nil {
		t.Fatalf("commit the identity: %v", err)
	}
	t.Cleanup(func() { ecForget(t, owner, w.ident) })
	for i := 0; i < n; i++ {
		tn, r := uuid.New(), uuid.New()
		ecExec(t, owner, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'bar', 'single')`, tn, "T173 race "+tn.String()[:8], "VAT-T173-"+tn.String())
		ecExec(t, owner, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
		                  VALUES ($1, $2, 'T173 Race', $3, $4, 'owner', $5, 'backfill', now() - interval '2 days')`, r, tn, w.email, ecDigest(), w.ident)
		w.tenants, w.rows = append(w.tenants, tn), append(w.rows, r)
	}
	w.session = opRandHex(t)
	ecExec(t, owner, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`, w.tenants[0], w.rows[0], w.session)
	return w
}

// ask commits a change link of w for address through the Go wrapper and returns its hash.
func (w *ecLive) ask(t *testing.T, d *DB, address string) string {
	t.Helper()
	link := opRandHex(t)
	r, err := d.RequestEmailChange(context.Background(), w.session, NewPasswordHash(ecDigest()), link, address)
	if err != nil || r.Refusal != "" || r.ChangeID == uuid.Nil {
		t.Fatalf("PREMISE: the request: %+v, %v", r, err)
	}
	return link
}

// ecQuiet fails the test if err is anything but the definers' one refusal -- and in any case if its text
// names address (no address in a DETAIL, CLAUDE.md §7).
func ecQuiet(t *testing.T, what string, err error, address string) {
	t.Helper()
	if err != nil && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(address)) {
		t.Errorf("%s: the error names the address: %v", what, err)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("%s failed (no 500): %v", what, err)
	}
}

// TestEmailChangeRace_TwoPressesOfOneLinkChangeOnce: one link pressed N times at once with the same
// session (a double click, two tabs) -- exactly one press changes the address and writes ONE undo link and
// ONE email_changed event; every other press is the refusal (pgx.ErrNoRows), never an error.
func TestEmailChangeRace_TwoPressesOfOneLinkChangeOnce(t *testing.T) {
	d, owner := appDB(t), ecOwner(t)
	w := newECLive(t, owner, 2)
	address := ecNew()
	link := w.ask(t, d, address)
	const n = 6
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		revert := opRandHex(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c, err := d.ChangeEmail(context.Background(), link, w.session, revert)
			mu.Lock()
			defer mu.Unlock()
			if err == nil && c.NewEmail == address {
				wins++
				return
			}
			ecQuiet(t, "a second press", err, address)
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d of %d presses changed the address, want exactly 1", wins, n)
	}
	if ecInt(t, owner, `SELECT count(*) FROM admin_identity_verifications WHERE identity_id = $1 AND purpose = 'revert'`, w.ident) != 1 ||
		ecInt(t, owner, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'email_changed'`, w.ident) != 1 ||
		ecInt(t, owner, `SELECT count(*) FROM admin_users WHERE identity_id = $1 AND email = $2::citext`, w.ident, address) != 2 {
		t.Error("not one undo link, one event and the mirror in both businesses")
	}
}

// TestEmailChangeRace_OneNewAddressEndsInOneIdentity (ADR 0044 §5.3, S13's twin): a confirmation that moves
// an identity to address X, a registration claim of X (a new identity) and an invitation accepted with a
// new password for X all queue on X's advisory lock (a gate holds it, then lets go), in rotating orders.
// Every round ends with exactly ONE identity of X, no error but the refusals, and no error naming X.
func TestEmailChangeRace_OneNewAddressEndsInOneIdentity(t *testing.T) {
	d, owner := appDB(t), ecOwner(t)
	ctx := context.Background()
	gate := ecOwner(t)
	const rounds = 6
	outcomes := map[string]int{}
	for round := 0; round < rounds; round++ {
		w := newECLive(t, owner, 1)
		address := ecNew()
		link := w.ask(t, d, address)
		t.Cleanup(func() {
			rows, err := owner.Query(context.Background(), `SELECT id FROM admin_identities WHERE email = $1::citext`, address)
			if err != nil {
				t.Errorf("cleanup: %v", err)
				return
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			if err != nil {
				t.Errorf("cleanup: %v", err)
			}
			ecForget(t, owner, ids...)
		})
		// The registration's draft names a business its claim creates in its own transaction.
		claimTenant, claimHash := uuid.New(), opRandHex(t)
		name, vat := "T173 claim "+claimTenant.String()[:8], "VAT-T173C-"+claimTenant.String()
		draft := fmt.Sprintf(`{"full_name": "T173 Owner", "business_name": %q, "vat_number": %q, "business_type": "restaurant", `+
			`"structure": "single", "vat_check": "unknown", "venues": ["Front door"]}`, name, vat)
		if _, err := d.OpenSignupClaim(ctx, claimHash, address, []byte(draft), opFakeDigest("r"), time.Now().Add(time.Hour)); err != nil {
			t.Fatalf("open the claim: %v", err)
		}
		// The invitation of X into another business.
		g, by, inviteHash := uuid.New(), uuid.New(), opRandHex(t)
		ecExec(t, owner, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'bar', 'single')`, g, "T173 invite "+g.String()[:8], "VAT-T173I-"+g.String())
		ecExec(t, owner, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'T173 Inviter', $3, $4, 'owner')`, by, g, ecNew(), ecDigest())
		ecExec(t, owner, `INSERT INTO admin_invites (tenant_id, email, full_name, role, token_hash, invited_by, expires_at)
		                  VALUES ($1, $2, 'T173 Invitee', 'manager', $3, $4, now() + interval '1 day')`, g, address, inviteHash, by)
		// 00077's Down refuses while admin_invites holds a row (the Down walks of this package run it).
		t.Cleanup(func() {
			if _, err := owner.Exec(context.Background(), `DELETE FROM admin_invites WHERE tenant_id = $1`, g); err != nil {
				t.Errorf("cleanup the invitation: %v", err)
			}
		})

		var key int64
		if err := gate.QueryRow(ctx, `SELECT hashtextextended('admin-identity-email:' || lower($1), 0)`, address).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if _, err := gate.Exec(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
			t.Fatal(err)
		}
		claimer, err := pgx.Connect(ctx, opOwnerDSN(t))
		if err != nil {
			t.Fatal(err)
		}
		var (
			wg                             sync.WaitGroup
			changeErr, claimErr, inviteErr error
			changed                        bool
		)
		starts := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-starts[0]
			_, changeErr = d.ChangeEmail(ctx, link, w.session, opRandHex(t))
			changed = changeErr == nil
		}()
		go func() {
			defer wg.Done()
			<-starts[1]
			claimErr = pgx.BeginFunc(ctx, claimer, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'restaurant', 'single')`, claimTenant, name, vat); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, claimTenant.String()); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_app`); err != nil {
					return err
				}
				var admin uuid.UUID
				return tx.QueryRow(ctx, claimSignupSQL, claimHash, claimTenant, nil, nil).Scan(&admin)
			})
		}()
		go func() {
			defer wg.Done()
			<-starts[2]
			_, inviteErr = d.AcceptAdminInviteNew(ctx, inviteHash, opFakeDigest("s"))
		}()
		waiting := func(n int) bool {
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				var k int
				if err := gate.QueryRow(ctx, `SELECT count(*) FROM pg_locks l WHERE l.locktype = 'advisory' AND NOT l.granted
				                               AND l.classid = ((($1::int8) >> 32) & 4294967295)::oid AND l.objid = (($1::int8) & 4294967295)::oid`, key).Scan(&k); err != nil {
					return false
				}
				if k >= n {
					return true
				}
				time.Sleep(2 * time.Millisecond)
			}
			return false
		}
		order := []int{round % 3, (round + 1) % 3, (round + 2) % 3}
		if round%2 == 1 {
			order[1], order[2] = order[2], order[1]
		}
		queued := true
		for k, i := range order {
			close(starts[i])
			queued = queued && waiting(k+1)
		}
		if _, err := gate.Exec(ctx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
			t.Errorf("let the gate go: %v", err)
		}
		wg.Wait()
		_ = claimer.Close(ctx)
		if !queued {
			t.Fatalf("round %d: the three callers did not all queue on the address's lock", round)
		}
		ecQuiet(t, "the change", changeErr, address)
		ecQuiet(t, "the claim", claimErr, address)
		ecQuiet(t, "the invitation", inviteErr, address)
		if n := ecInt(t, owner, `SELECT count(*) FROM admin_identities WHERE email = $1::citext`, address); n != 1 {
			t.Fatalf("round %d: %d identities of the address, want exactly 1", round, n)
		}
		switch {
		case changed && claimErr != nil && inviteErr != nil:
			outcomes["the change"]++
		case !changed && claimErr == nil && inviteErr != nil:
			outcomes["the claim"]++
		case !changed && claimErr != nil && inviteErr == nil:
			outcomes["the invitation"]++
		default:
			t.Fatalf("round %d: change=%v claim=%v invite=%v -- not one winner", round, changed, claimErr, inviteErr)
		}
	}
	t.Logf("winners over %d rounds: %v", rounds, outcomes)
	if len(outcomes) < 2 {
		t.Errorf("anti-vacuity: only %v won -- the rotation did not reach both orders", outcomes)
	}
}

// TestEmailChangeRace_AChangeBesideAResetConsumptionDoesNotDeadlock: a confirmation and the consumption of
// the identity's live reset link, at once, N rounds: no 40P01, no error, and exactly one of the two takes
// effect (a change first cancels the link; a consumption first ends the confirming session).
func TestEmailChangeRace_AChangeBesideAResetConsumptionDoesNotDeadlock(t *testing.T) {
	d, owner := appDB(t), ecOwner(t)
	ctx := context.Background()
	for round := 0; round < 8; round++ {
		w := newECLive(t, owner, 2)
		address := ecNew()
		link := w.ask(t, d, address)
		reset := opRandHex(t)
		if o, err := d.OpenIdentityReset(ctx, w.ident, reset, time.Now().Add(time.Hour), w.email, 5*time.Second); err != nil || !o.Issued {
			t.Fatalf("PREMISE: the reset: %+v, %v", o, err)
		}
		var (
			wg                sync.WaitGroup
			changeErr, eatErr error
		)
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, changeErr = d.ChangeEmail(ctx, link, w.session, opRandHex(t)) }()
		go func() { defer wg.Done(); <-start; _, eatErr = d.ConsumeIdentityReset(ctx, reset, opFakeDigest("t")) }()
		close(start)
		wg.Wait()
		ecQuiet(t, "the change", changeErr, address)
		ecQuiet(t, "the consumption", eatErr, address)
		if (changeErr == nil) == (eatErr == nil) {
			t.Fatalf("round %d: change %v, consumption %v -- want exactly one to take effect", round, changeErr, eatErr)
		}
	}
}

// ecHolding runs fn as tappa_app inside an owner transaction that stays OPEN (its locks held) until the
// returned commit is called -- before which pre (owner statements, e.g. a mutated body) runs in the same
// transaction, and after fn before (owner statements again) the commit.
type ecHold struct {
	conn *pgx.Conn
	tx   pgx.Tx
}

func ecBegin(t *testing.T, pre ...string) *ecHold {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range pre {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return &ecHold{conn: conn, tx: tx}
}

// app runs one statement as tappa_app in the held transaction and counts its rows.
func (h *ecHold) app(t *testing.T, sql string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	if _, err := h.tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_app`); err != nil {
		t.Fatal(err)
	}
	rows, err := h.tx.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	n := 0
	for rows.Next() {
		n++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	if _, err := h.tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION DEFAULT`); err != nil {
		t.Fatal(err)
	}
	return n
}

func (h *ecHold) commit(t *testing.T, before ...string) {
	t.Helper()
	ctx := context.Background()
	for _, s := range before {
		if _, err := h.tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	if err := h.tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func (h *ecHold) rollback(t *testing.T) {
	t.Helper()
	if err := h.tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatal(err)
	}
}

// ecWaitFor waits until a backend runs a statement holding fragment and waits on a lock.
func ecWaitFor(t *testing.T, owner *pgx.Conn, fragment string) bool {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ecInt(t, owner, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND strpos(query, $1) > 0`, fragment) > 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// ecDef is fn's live definition, and the same with cut replaced by put.
func ecDef(t *testing.T, owner *pgx.Conn, fn, cut, put string) (orig, mutated string) {
	t.Helper()
	if err := owner.QueryRow(context.Background(), `SELECT pg_get_functiondef($1::regprocedure)`, fn).Scan(&orig); err != nil {
		t.Fatal(err)
	}
	if strings.Count(orig, cut) != 1 {
		t.Fatalf("PREMISE: %s holds %q %d times", fn, cut, strings.Count(orig, cut))
	}
	return orig, strings.Replace(orig, cut, put, 1)
}

// TestEmailChangeRace_AResetOpenedWhileTheAddressMovesNeverReachesTheOldAddress (S-1, ADR 0044 §5.3): a reset
// open for the OLD address (its recipient) and a change -- or an undo -- of the same identity, each order
// made deterministic by a transaction held open: the open after the move answers 0 rows; the open before
// it is cancelled by it. Either way no live reset link of the identity remains. MUTATION: the change's and
// the undo's reset advisory lock cut (inside the held transaction, the body restored before it commits) ->
// the open reads the old address before the move commits, waits on the identity's row, and its link to
// the old address outlives the move -- red.
func TestEmailChangeRace_AResetOpenedWhileTheAddressMovesNeverReachesTheOldAddress(t *testing.T) {
	d, owner := appDB(t), ecOwner(t)
	ctx := context.Background()
	const resetLock = "    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-reset:' || v_identity::text, 0));\n"
	type mover struct {
		name string
		fn   string
		// arrange commits what the move needs and returns the statement that moves (args), the address the
		// identity holds BEFORE it (the open's recipient) and the one after.
		arrange func(w *ecLive) (sql string, args []any, before, after string)
	}
	movers := []mover{
		{"the change", ecChangeFn, func(w *ecLive) (string, []any, string, string) {
			address := ecNew()
			return `SELECT * FROM identity_change_email($1, $2, $3)`, []any{w.ask(t, d, address), w.session, opRandHex(t)}, w.email, address
		}},
		{"the undo", ecRevertFn, func(w *ecLive) (string, []any, string, string) {
			address, revert := ecNew(), opRandHex(t)
			if _, err := d.ChangeEmail(ctx, w.ask(t, d, address), w.session, revert); err != nil {
				t.Fatalf("PREMISE: the change: %v", err)
			}
			return `SELECT * FROM identity_revert_email_change($1)`, []any{revert}, address, w.email
		}},
	}
	live := func(w *ecLive) int64 {
		return ecInt(t, owner, `SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1 AND used_at IS NULL AND cancelled_at IS NULL`, w.ident)
	}
	type opened struct {
		o   OpenedIdentityReset
		err error
	}
	for _, m := range movers {
		t.Run(m.name+"/moved first", func(t *testing.T) {
			w := newECLive(t, owner, 2)
			sql, args, before, _ := m.arrange(w)
			h := ecBegin(t)
			if n := h.app(t, sql, args...); n != 1 {
				h.rollback(t)
				t.Fatalf("PREMISE: the move answered %d rows", n)
			}
			done := make(chan opened, 1)
			go func() {
				o, err := d.OpenIdentityReset(ctx, w.ident, opRandHex(t), time.Now().Add(time.Hour), before, 10*time.Second)
				done <- opened{o, err}
			}()
			if !ecWaitFor(t, owner, "identity_open_reset") {
				h.rollback(t)
				<-done
				t.Fatal("the open never waited on the move")
			}
			h.commit(t)
			if r := <-done; !errors.Is(r.err, pgx.ErrNoRows) {
				t.Errorf("the open after the move answered %+v, %v; want pgx.ErrNoRows", r.o, r.err)
			}
			if n := live(w); n != 0 {
				t.Errorf("%d live reset links after the move", n)
			}
		})
		t.Run(m.name+"/opened first", func(t *testing.T) {
			w := newECLive(t, owner, 2)
			sql, args, before, _ := m.arrange(w)
			h := ecBegin(t)
			if n := h.app(t, `SELECT * FROM identity_open_reset($1, $2, now() + interval '1 hour', $3) WHERE issued`, w.ident, opRandHex(t), before); n != 1 {
				h.rollback(t)
				t.Fatalf("PREMISE: the open answered %d rows", n)
			}
			done := make(chan error, 1)
			go func() {
				var err error
				switch m.fn {
				case ecChangeFn:
					_, err = d.ChangeEmail(ctx, args[0].(string), args[1].(string), args[2].(string))
				default:
					_, err = d.RevertEmailChange(ctx, args[0].(string))
				}
				done <- err
			}()
			if !ecWaitFor(t, owner, "identity_") {
				h.rollback(t)
				<-done
				t.Fatal("the move never waited on the open")
			}
			h.commit(t)
			if err := <-done; err != nil {
				t.Fatalf("the move after the open: %v", err)
			}
			if n := live(w); n != 0 {
				t.Errorf("%d live reset links after the move (the open's link was not cancelled)", n)
			}
			_ = sql
		})
		t.Run(m.name+"/mutation: no reset lock", func(t *testing.T) {
			w := newECLive(t, owner, 2)
			sql, args, before, _ := m.arrange(w)
			orig, mutated := ecDef(t, owner, m.fn, resetLock, "")
			h := ecBegin(t, mutated)
			if n := h.app(t, sql, args...); n != 1 {
				h.rollback(t)
				t.Fatalf("PREMISE: the mutated move answered %d rows", n)
			}
			done := make(chan opened, 1)
			go func() {
				o, err := d.OpenIdentityReset(ctx, w.ident, opRandHex(t), time.Now().Add(time.Hour), before, 10*time.Second)
				done <- opened{o, err}
			}()
			if !ecWaitFor(t, owner, "identity_open_reset") {
				h.rollback(t)
				<-done
				t.Fatal("PREMISE: without the lock the open did not even wait on the identity's row")
			}
			h.commit(t, orig) // the body restored in the same transaction: the mutation never commits
			r := <-done
			if r.err != nil || !r.o.Issued || live(w) != 1 {
				t.Errorf("MUTATION: without the reset lock the open answered %+v, %v and %d links live -- the lock is not what held the case", r.o, r.err, live(w))
			}
		})
	}
	// A few free-running rounds of the change against an open of the old address.
	for round := 0; round < 6; round++ {
		w := newECLive(t, owner, 1)
		address := ecNew()
		link := w.ask(t, d, address)
		var (
			wg        sync.WaitGroup
			changeErr error
		)
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, changeErr = d.ChangeEmail(ctx, link, w.session, opRandHex(t)) }()
		go func() {
			defer wg.Done()
			<-start
			_, err := d.OpenIdentityReset(ctx, w.ident, opRandHex(t), time.Now().Add(time.Hour), w.email, 10*time.Second)
			ecQuiet(t, "the open", err, w.email)
		}()
		close(start)
		wg.Wait()
		if changeErr != nil {
			t.Fatalf("round %d: the change: %v", round, changeErr)
		}
		if n := live(w); n != 0 {
			t.Fatalf("round %d: %d live reset links after the change", round, n)
		}
	}
}

// TestEmailChangeRace_TheUndoLeavesNoLiveSession (S-2, ADR 0044 §5.6): the undo against the person's own
// session writers at once -- a switch, a sign-in with the identity's digest, a link-by-email press -- N
// rounds: whatever the order, the identity's rows hold no live session afterwards (a writer first is ended
// by the undo; a writer after it is refused: the session revoked, the digest the sentinel), and no call
// fails.
func TestEmailChangeRace_TheUndoLeavesNoLiveSession(t *testing.T) {
	d, owner := appDB(t), ecOwner(t)
	ctx := context.Background()
	for round := 0; round < 6; round++ {
		w := newECLive(t, owner, 2)
		address, revert := ecNew(), opRandHex(t)
		if _, err := d.ChangeEmail(ctx, w.ask(t, d, address), w.session, revert); err != nil {
			t.Fatalf("PREMISE: the change: %v", err)
		}
		// A row of the new address linked to nobody, and the link that would link it.
		other := uuid.New()
		ecExec(t, owner, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'bar', 'single')`, other, "T173 S-2 "+other.String()[:8], "VAT-T173S-"+other.String())
		ecExec(t, owner, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, created_at) VALUES ($1, $2, 'T173 S-2', $3, $4, 'owner', now() - interval '1 hour')`,
			uuid.New(), other, address, opFakeDigest("u"))
		linkHash := opRandHex(t)
		if _, err := d.RequestIdentityLink(ctx, w.session, linkHash); err != nil {
			t.Fatalf("PREMISE: the link request: %v", err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 4)
		calls := []func() error{
			func() error { _, err := d.RevertEmailChange(ctx, revert); return err },
			func() error {
				_, err := d.SwitchIdentitySession(ctx, w.session, w.tenants[1], opRandHex(t))
				return err
			},
			func() error {
				_, err := d.OpenIdentitySession(ctx, w.ident, w.tenants[0], opRandHex(t), NewPasswordHash(ecDigest()))
				return err
			},
			func() error { _, err := d.LinkByEmail(ctx, linkHash, w.session); return err },
		}
		for i, call := range calls {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; errs[i] = call() }()
		}
		close(start)
		wg.Wait()
		if errs[0] != nil {
			t.Fatalf("round %d: the undo: %v", round, errs[0])
		}
		for i, err := range errs[1:] {
			ecQuiet(t, fmt.Sprintf("round %d writer %d", round, i+1), err, address)
		}
		if n := ecInt(t, owner, `SELECT count(*) FROM admin_sessions s JOIN admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
		                           WHERE a.identity_id = $1 AND s.revoked_at IS NULL`, w.ident); n != 0 {
			t.Fatalf("round %d: %d live sessions of the identity after the undo", round, n)
		}
	}
}

// TestEmailChange00102_TheUnlinksUndoClockIsReadAfterTheLocks (K-E16 (a)): an unlink that waits on the
// identity's lock while the undo link expires goes through once the lock is free -- "live" is judged
// with the clock read AFTER the locks. MUTATION (in the waiting transaction only, never committed): the
// block's clock replaced by now() -- the call's start, before the wait -> the expired link still counts,
// 0 rows, red.
func TestEmailChange00102_TheUnlinksUndoClockIsReadAfterTheLocks(t *testing.T) {
	_, owner := appDB(t), ecOwner(t)
	ctx := context.Background()
	run := func(pre ...string) int {
		w := newECLive(t, owner, 2)
		ecExec(t, owner, `UPDATE admin_identities SET confirm_opened_at = now(), confirm_open_until = now() + interval '7 days', confirm_scope = 'reset' WHERE id = $1`, w.ident)
		fresh := opRandHex(t)
		ecExec(t, owner, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, authenticated_at) VALUES ($1, $2, $3, clock_timestamp())`, w.tenants[0], w.rows[0], fresh)
		var expires time.Time
		if err := owner.QueryRow(ctx, `INSERT INTO admin_identity_verifications (identity_id, token_hash, expires_at, purpose, address)
		                                VALUES ($1, $2, clock_timestamp() + interval '2 seconds', 'revert', $3) RETURNING expires_at`, w.ident, opRandHex(t), ecNew()).Scan(&expires); err != nil {
			t.Fatal(err)
		}
		lock := ecBegin(t)
		if _, err := lock.tx.Exec(ctx, `SELECT 1 FROM admin_identities WHERE id = $1 FOR UPDATE`, w.ident); err != nil {
			t.Fatal(err)
		}
		caller := ecBegin(t, pre...)
		done := make(chan int, 1)
		go func() { done <- caller.app(t, `SELECT * FROM identity_unlink_backfilled($1, $2)`, fresh, w.tenants[1]) }()
		if !ecWaitFor(t, owner, "identity_unlink_backfilled") {
			lock.rollback(t)
			<-done
			caller.rollback(t)
			t.Fatal("the unlink never waited on the identity's lock")
		}
		time.Sleep(time.Until(expires) + 300*time.Millisecond)
		lock.rollback(t)
		n := <-done
		caller.rollback(t) // nothing of the call (nor a mutation) is kept
		return n
	}
	if n := run(); n != 1 {
		t.Errorf("an unlink that waited past the undo link's end answered %d rows, want 1", n)
	}
	_, mutated := ecDef(t, owner, ecUnlinkFn, "       AND v.expires_at > v_now;\n    IF FOUND THEN", "       AND v.expires_at > pg_catalog.now();\n    IF FOUND THEN")
	if n := run(mutated); n != 0 {
		t.Errorf("MUTATION: with the clock read before the wait the unlink still answered %d rows", n)
	}
}

// ecLifetimeRecheck is the request's and the confirmation's verdict on the session AFTER their locks, as
// 00102 writes it (identity_switch_session's 00085 rule).
const ecLifetimeRecheck = "    IF (v_found AND v_auth > v_now - interval '12 hours') IS NOT TRUE THEN\n"

// TestEmailChange00102_ACallThatWaitsPastTheSessionsEndWritesNothing: a session with two seconds of its 12
// hours left starts a request -- and, separately, a confirmation -- while another transaction holds the
// identity's lock; the lock is let go after the end. The call writes nothing: the lifetime is judged again
// after the locks, with the clock read again. MUTATION (in the waiting transaction only, never committed):
// that verdict reduced to "the session row is there" -> the request writes its link / the confirmation
// moves the address, red.
func TestEmailChange00102_ACallThatWaitsPastTheSessionsEndWritesNothing(t *testing.T) {
	d, owner := appDB(t), ecOwner(t)
	ctx := context.Background()
	type call struct {
		name, fn string
		prepare  func(w *ecLive) (sql string, args []any)
	}
	calls := []call{
		{"the request", ecRequestFn, func(w *ecLive) (string, []any) {
			return `SELECT * FROM identity_request_email_change($1, $2, $3, $4) WHERE refusal IS NULL`, []any{w.session, ecDigest(), opRandHex(t), ecNew()}
		}},
		{"the confirmation", ecChangeFn, func(w *ecLive) (string, []any) {
			return `SELECT * FROM identity_change_email($1, $2, $3)`, []any{w.ask(t, d, ecNew()), w.session, opRandHex(t)}
		}},
	}
	run := func(c call, mutate bool) int {
		w := newECLive(t, owner, 2)
		sql, args := c.prepare(w)
		var end time.Time
		if err := owner.QueryRow(ctx, `UPDATE admin_sessions SET created_at = clock_timestamp() - interval '12 hours' + interval '2 seconds',
		                                  authenticated_at = clock_timestamp() - interval '12 hours' + interval '2 seconds'
		                                WHERE token_hash = $1 RETURNING authenticated_at + interval '12 hours'`, w.session).Scan(&end); err != nil {
			t.Fatal(err)
		}
		var pre []string
		if mutate {
			_, mutated := ecDef(t, owner, c.fn, ecLifetimeRecheck, "    IF v_found IS NOT TRUE THEN\n")
			pre = append(pre, mutated)
		}
		lock := ecBegin(t)
		if _, err := lock.tx.Exec(ctx, `SELECT 1 FROM admin_identities WHERE id = $1 FOR UPDATE`, w.ident); err != nil {
			t.Fatal(err)
		}
		caller := ecBegin(t, pre...)
		done := make(chan int, 1)
		go func() { done <- caller.app(t, sql, args...) }()
		fragment := strings.TrimPrefix(strings.SplitN(c.fn, "(", 2)[0], "public.")
		if !ecWaitFor(t, owner, fragment) {
			lock.rollback(t)
			<-done
			caller.rollback(t)
			t.Fatalf("%s never waited on the identity's lock", c.name)
		}
		time.Sleep(time.Until(end) + 500*time.Millisecond)
		lock.rollback(t)
		n := <-done
		caller.rollback(t) // nothing of the call (nor a mutation) is kept
		return n
	}
	for _, c := range calls {
		if n := run(c, false); n != 0 {
			t.Errorf("%s that waited past the session's end answered %d rows, want 0", c.name, n)
		}
		if n := run(c, true); n != 1 {
			t.Errorf("MUTATION: %s with the post-lock verdict cut still answered %d rows -- the case does not test it", c.name, n)
		}
	}
}
