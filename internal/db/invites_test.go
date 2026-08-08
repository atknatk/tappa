package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// invites_test.go -- the M5-02 (phase A) data-layer proofs, against a REAL
// Postgres (CLAUDE.md section 8 / Q04):
//
//   - ConsumeInviteAndActivate is ATOMIC: N goroutines racing on one invite produce
//     EXACTLY one winner. A PERMANENT negative control in the same test performs
//     the naive read-then-write instead and shows it produces N winners -- so the
//     positive result is evidence about the statement, not about luck.
//   - That same statement is the ONLY way this layer reaches status='active':
//     consumption and activation are fused, so an activation implies a spent
//     invite. There is no consume-only and no activate-only query.
//   - A consumed or expired invite cannot be consumed again, and neither can one
//     addressed from another tenant's context. A DEACTIVATED employee's invite is
//     not even stamped -- proven with a caller that swallows the error and COMMITS.
//   - The column shape CHECKs bite: a non-hash code_hash and an infinite expiry are
//     both refused by the DATABASE, so 00009's red-line comments are enforced
//     rather than merely asserted.
//   - activated_at survives a second activation (COALESCE).
//   - tappa_app cannot DELETE an invite and cannot UPDATE anything but used_at
//     (has_table_privilege / has_column_privilege AND real statements -- "absent
//     from the GRANT" is not evidence, M1-04 lesson).
//   - The context-less resolver returns the invite REGARDLESS of consumption or
//     expiry (facts, not decisions).
//
// WHICH ADR 0002 madde 7 CONSTRAINTS THIS FILE MEASURES. The ADR names five, and
// they are properties of the RESOLUTION INTERFACE, not of the definer role:
//
//	(i)   the input is only a KEY (uid / token_hash / code_hash);
//	(ii)  the result is AT MOST ONE row (the keys are unique);
//	(iii) there is NO `SELECT *` surface -- a fixed, minimal column list;
//	(iv)  the caller (tappa_app) has NO direct cross-tenant SELECT on the table,
//	      only EXECUTE on the narrow interface;
//	(v)   a naive "show rows when the context is NULL" RLS branch is FORBIDDEN.
//
// TestResolveInvite_SecurityDefinerProperties measures all five for
// resolve_invite_by_code_hash -- (i) from the function's argument list, (ii) from
// the globally UNIQUE index that underwrites it (a property of the index, not of
// the body), (iii) from the declared result type, (iv) by reading the table
// context-less as tappa_app (0 rows) while the function returns 1, and (v) from the
// policy expression itself (NULLIF form, no OR branch). It ALSO checks the ADR's
// separate least-privilege clause on the definer -- owner tappa_resolver, not a
// superuser, BYPASSRLS, fixed search_path, no PUBLIC EXECUTE -- which is a
// different requirement from the five and is labelled as such in the test.
//
// Cross-tenant invisibility itself (the RLS half of (iv)) is proven in
// rls_test.go's read/write isolation cases, which are the non-vacuous ones.
//
// The invite CODE never appears here: every fixture writes a RANDOM 64-hex string
// straight into code_hash -- the exact shape internal/session's HMAC produces and
// the CHECK enforces. No test invents, derives or prints a code (section 4.7); the
// hashes here are random bytes and are pre-images of nothing.
//
// Fixtures are NOT cleaned up, for the reason store_test.go/rls_test.go give:
// tappa_app has REVOKE DELETE on employee_invites too, so the impossibility of
// teardown IS the audit guarantee. Every id is a fresh random UUID.

// ---------------------------------------------------------------- fixtures --

// newEmployee inserts a committed employee in its own tenant context and returns
// its id. status is a parameter so the deactivated case can be built directly.
func newEmployee(t *testing.T, d *DB, tenantID, locationID uuid.UUID, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Invite Test', $4, now())`,
			id, tenantID, locationID, status)
		return e
	}); err != nil {
		t.Fatalf("newEmployee(%s): %v", status, err)
	}
	return id
}

// randCodeHash returns a fresh 64-lowercase-hex string: the shape
// internal/session's hex HMAC-SHA256 produces and the shape 00009's CHECK
// enforces. It is random bytes, NOT the hash of anything -- no test ever holds an
// invite code (section 4.7).
func randCodeHash(t *testing.T) string {
	t.Helper()
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b[:])
}

// newInvite issues an invite through the generated store query and returns its id
// plus the code hash it was stored under. ttl < 0 creates an ALREADY EXPIRED
// invite (an absent or infinite expiry is unrepresentable -- see
// TestEmployeeInvites_ExpiryCannotBeAbsent).
func newInvite(t *testing.T, d *DB, tenantID, employeeID uuid.UUID, ttl time.Duration) (uuid.UUID, string) {
	t.Helper()
	codeHash := randCodeHash(t)
	var row store.CreateInviteRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreateInvite(ctx, store.CreateInviteParams{
			TenantID:   tenantID,
			EmployeeID: employeeID,
			CodeHash:   codeHash,
			ExpiresAt:  time.Now().Add(ttl),
		})
		return e
	}); err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if row.UsedAt != nil {
		t.Fatalf("a fresh invite has used_at = %v, want NULL", row.UsedAt)
	}
	return row.ID, codeHash
}

// consume runs the real activation statement in the tenant's context. It takes the
// CODE HASH -- not the invite id, not an employee id -- because that is the only
// query that exists: consuming requires knowing the code, and activating requires
// consuming (db/queries/invites.sql).
//
// The error is returned to the caller, so WithTenant rolls the transaction back on
// failure exactly as production does. That is what the deactivated-employee test
// relies on.
func consume(t *testing.T, d *DB, tenantID uuid.UUID, codeHash string) (store.ConsumeInviteAndActivateRow, error) {
	t.Helper()
	var row store.ConsumeInviteAndActivateRow
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).ConsumeInviteAndActivate(ctx, store.ConsumeInviteAndActivateParams{
			CodeHash: codeHash, TenantID: tenantID,
		})
		return e
	})
	return row, err
}

// inviteUsedAt reads an invite's used_at in its tenant context (nil = unconsumed).
func inviteUsedAt(t *testing.T, d *DB, tenantID, inviteID uuid.UUID) *time.Time {
	t.Helper()
	var used *time.Time
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT used_at FROM employee_invites WHERE id = $1 AND tenant_id = $2`,
			inviteID, tenantID).Scan(&used)
	}); err != nil {
		t.Fatalf("read used_at: %v", err)
	}
	return used
}

// ------------------------------------------------------- atomic single use --

// raceInviteGoroutines is high enough that a non-atomic implementation loses
// immediately (the negative control below shows it does) and low enough to stay
// well inside Postgres' default max_connections.
const raceInviteGoroutines = 50

// raceDB returns a tappa_app pool sized to hold the WHOLE race at once. Without
// this the goroutines queue on pgx's small default pool and the "race" degrades
// into a near-sequential run -- the atomicity proof depends on many transactions
// genuinely contending for the same row lock (the internal/sun M2-06 test widens
// the pool for exactly this reason).
func raceDB(t *testing.T) *DB {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping invite DB tests (real Postgres required -- CLAUDE.md section 8, Q04)")
	}
	if !strings.Contains(raw, "pool_max_conns") {
		sep := "?"
		if strings.Contains(raw, "?") {
			sep = "&"
		}
		raw += sep + "pool_max_conns=" + strconv.Itoa(raceInviteGoroutines+4)
	}
	d, err := New(context.Background(), &config.Config{DatabaseURL: raw})
	if err != nil {
		t.Fatalf("New(race pool): %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// TestConsumeInvite_ConcurrentRaceExactlyOneWinner is THE proof that single use is
// enforced by the DATABASE, not by hope. The same shape as the section 4.4 replay
// proof (internal/sun TestAdvanceCounter_ConcurrentRaceExactlyOneWinner): N
// goroutines, each on its OWN pooled connection and transaction, race to consume
// the SAME invite. Exactly one may win; the other N-1 must see pgx.ErrNoRows.
// There is deliberately no mutex -- that would be a single-process fiction and
// would make the test pass for the wrong reason.
//
// Why it matters: two winners means two activations and two session cookies for
// one invitation -- the link opened on two devices, or replayed on purpose.
func TestConsumeInvite_ConcurrentRaceExactlyOneWinner(t *testing.T) {
	d := raceDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")
	_, codeHash := newInvite(t, d, tenantID, employeeID, time.Hour)

	var wins, noRows, others int64
	var launched sync.WaitGroup // every goroutine parked and ready
	var done sync.WaitGroup     // every goroutine finished
	launched.Add(raceInviteGoroutines)
	done.Add(raceInviteGoroutines)
	start := make(chan struct{}) // closed to release all at once

	for i := 0; i < raceInviteGoroutines; i++ {
		go func() {
			defer done.Done()
			launched.Done()
			<-start // block until every goroutine is ready, then rush together

			_, err := consume(t, d, tenantID, codeHash)
			switch {
			case err == nil:
				atomic.AddInt64(&wins, 1)
			case errors.Is(err, pgx.ErrNoRows):
				atomic.AddInt64(&noRows, 1)
			default:
				atomic.AddInt64(&others, 1)
				t.Errorf("unexpected error (want nil or pgx.ErrNoRows): %v", err)
			}
		}()
	}

	launched.Wait() // all parked on <-start: the race is real
	close(start)
	done.Wait()

	if wins != 1 {
		t.Fatalf("winners = %d, want EXACTLY 1 (single use must be atomic in SQL)", wins)
	}
	if noRows != raceInviteGoroutines-1 {
		t.Fatalf("pgx.ErrNoRows = %d, want %d (every loser rejected)", noRows, raceInviteGoroutines-1)
	}
	if others != 0 {
		t.Fatalf("%d goroutines got a non-ErrNoRows error", others)
	}

	// The invite is now dead for everyone, including a later serial attempt.
	if _, err := consume(t, d, tenantID, codeHash); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("post-race consume error = %v, want pgx.ErrNoRows", err)
	}
}

// TestConsumeInvite_ReadThenWriteWouldRace is the NEGATIVE CONTROL for the test
// above (M0-07 lesson: a green test proves nothing unless the same probe goes red
// when the protection is removed). It performs the NAIVE implementation the
// production query deliberately avoids -- SELECT used_at, decide in Go, then
// UPDATE without the guard -- on its own throwaway invite, and asserts that it
// produces MORE THAN ONE winner.
//
// It is deterministic, not luck: a barrier holds every goroutine after its read
// and before its write, which is exactly the interleaving a read-then-write
// implementation permits. If this ever reported 1 winner, the test above would be
// measuring something other than atomicity and both would need re-examining.
//
// The rows it writes are the ordinary used_at stamp; nothing here is a production
// code path.
func TestConsumeInvite_ReadThenWriteWouldRace(t *testing.T) {
	d := raceDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")
	_, codeHash := newInvite(t, d, tenantID, employeeID, time.Hour)

	const n = 8
	var wins int64
	var read, written sync.WaitGroup
	read.Add(n)
	written.Add(n)
	writeGate := make(chan struct{}) // released only after EVERY goroutine has read

	for i := 0; i < n; i++ {
		go func() {
			defer written.Done()

			// Phase 1 -- the naive read, in its own transaction.
			var used *time.Time
			if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx,
					`SELECT used_at FROM employee_invites WHERE code_hash = $1 AND tenant_id = $2`,
					codeHash, tenantID).Scan(&used)
			}); err != nil {
				t.Errorf("naive read: %v", err)
				read.Done()
				return
			}
			free := used == nil // "looks unused -- I may take it"
			read.Done()

			<-writeGate

			// Phase 2 -- the naive write: the guard lives in Go, not in SQL.
			if !free {
				return
			}
			if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx,
					`UPDATE employee_invites SET used_at = now() WHERE code_hash = $1 AND tenant_id = $2`,
					codeHash, tenantID)
				return e
			}); err != nil {
				t.Errorf("naive write: %v", err)
				return
			}
			atomic.AddInt64(&wins, 1)
		}()
	}

	read.Wait()
	close(writeGate)
	written.Wait()

	if wins <= 1 {
		t.Fatalf("read-then-write produced %d winner(s); the negative control must show MORE than 1, otherwise the atomicity test above proves nothing", wins)
	}
	t.Logf("negative control: read-then-write produced %d winners for ONE invite (the atomic query produces exactly 1)", wins)
}

// TestConsumeInvite_DeadInvites: the two states that make an invite unusable are
// both refused, and both look the same from the outside (0 rows) -- the caller
// gets no oracle. Each case is paired with a positive control on a FRESH invite so
// a blanket failure cannot pass as a correct refusal.
func TestConsumeInvite_DeadInvites(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")

	t.Run("already consumed", func(t *testing.T) {
		_, codeHash := newInvite(t, d, tenantID, employeeID, time.Hour)

		// Positive control: the first consumption succeeds, activates the
		// employee and names the invite it spent.
		row, err := consume(t, d, tenantID, codeHash)
		if err != nil {
			t.Fatalf("first consume: %v", err)
		}
		if row.ID != employeeID {
			t.Fatalf("consume returned employee %s, want %s", row.ID, employeeID)
		}
		if row.Status != "active" || row.ActivatedAt == nil {
			t.Fatalf("consume left the employee status=%q activated_at=%v, want active + a stamp", row.Status, row.ActivatedAt)
		}
		if used := inviteUsedAt(t, d, tenantID, row.InviteID); used == nil {
			t.Fatal("the invite is still unconsumed after a successful activation")
		}

		// Negative: the second consumption finds nothing to consume.
		if _, err := consume(t, d, tenantID, codeHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("second consume error = %v, want pgx.ErrNoRows", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		_, expiredHash := newInvite(t, d, tenantID, employeeID, -time.Minute)
		if _, err := consume(t, d, tenantID, expiredHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expired consume error = %v, want pgx.ErrNoRows", err)
		}

		// Positive control: a live invite for the same employee still consumes,
		// so the refusal above is the expiry and not something structural.
		_, liveHash := newInvite(t, d, tenantID, employeeID, time.Hour)
		if _, err := consume(t, d, tenantID, liveHash); err != nil {
			t.Fatalf("positive control: live invite could not be consumed: %v", err)
		}
	})

	// 🔴 CANCELLED IS THE THIRD DEAD SHAPE, AND IT IS THE ONE THE STATEMENT ALONE
	// GUARDS. Migration 00012 added cancelled_at so that issuing a new invitation can
	// retire the employee's older ones — the account-takeover path M6-05 phase B
	// measured end to end: a stale link, opened later, took the second-device route
	// and signed the real employee out.
	//
	// ⚠️ IT IS TESTED HERE, AT THE STATEMENT, BECAUSE THE HTTP TEST COULD NOT SEE IT.
	// A mutation dropped `cancelled_at IS NULL` from this query and the end-to-end
	// test stayed GREEN: internal/invite's Lookup refuses a cancelled code at GET
	// /activate, so the POST never reaches the consuming statement. Two correct
	// layers, and a hole between them — exactly the shape this repository has paid
	// for before. The Go gate is a courtesy; THIS is the authority.
	t.Run("cancelled", func(t *testing.T) {
		_, cancelledHash := newInvite(t, d, tenantID, employeeID, time.Hour)
		cancelPending(t, d, tenantID, employeeID)

		if _, err := consume(t, d, tenantID, cancelledHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("cancelled consume error = %v, want pgx.ErrNoRows — a retired "+
				"invitation must not activate anybody", err)
		}
		// AND IT IS STILL NOT "USED". Cancelling must not write used_at: the two are
		// different answers to "what happened to this code" and db/queries/invites.sql
		// forbids conflating them.
		var used, cancelled *time.Time
		if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT used_at, cancelled_at FROM employee_invites
				 WHERE tenant_id = $1 AND code_hash = $2`, tenantID, cancelledHash).
				Scan(&used, &cancelled)
		}); err != nil {
			t.Fatalf("read stamps: %v", err)
		}
		if used != nil {
			t.Errorf("a CANCELLED invitation carries used_at = %v; cancellation must not "+
				"be recorded as consumption", used)
		}
		if cancelled == nil {
			t.Fatal("the invitation was not cancelled at all; the refusal above proves nothing")
		}

		// POSITIVE CONTROL: a fresh invitation for the same employee still consumes,
		// so the refusal is the cancellation and not something structural.
		_, liveHash := newInvite(t, d, tenantID, employeeID, time.Hour)
		if _, err := consume(t, d, tenantID, liveHash); err != nil {
			t.Fatalf("positive control: a live invite could not be consumed: %v", err)
		}
	})

	t.Run("foreign tenant context", func(t *testing.T) {
		_, codeHash := newInvite(t, d, tenantID, employeeID, time.Hour)
		otherTenant, _ := newTenant(t, d)

		// The explicit tenant_id predicate AND RLS both stand in the way; either
		// alone yields 0 rows, which is the point of belt + braces (section 4.5).
		if _, err := consume(t, d, otherTenant, codeHash); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("cross-tenant consume error = %v, want pgx.ErrNoRows", err)
		}
		// Positive control: in its OWN tenant the same invite consumes fine.
		if _, err := consume(t, d, tenantID, codeHash); err != nil {
			t.Fatalf("positive control: own-tenant consume failed: %v", err)
		}
	})
}

// TestEmployeeInvites_CodeHashMustBeAHash proves 00009's section 4.7 sentence
// ("the CODE itself is never written here") is enforced by the DATABASE and not
// merely asserted in a comment. Without the CHECK, a `text` column would accept a
// raw invite code as happily as a hash and the claim would guard nothing.
//
// Rejected shapes are the ones a mistake would actually produce: a human-typed
// code, a base64url token (what session.Token looks like BEFORE hashing -- the
// single most likely wrong value), uppercase hex, and a wrong-length digest.
// Accepted: exactly 64 lowercase hex chars, which is what
// internal/session/token.go's hex.EncodeToString(HMAC-SHA256) emits.
func TestEmployeeInvites_CodeHashMustBeAHash(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")

	insert := func(codeHash string) error {
		return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx,
				`INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at)
				 VALUES ($1, $2, $3, now() + interval '1 hour')`,
				tenantID, employeeID, codeHash)
			return e
		})
	}

	good := randCodeHash(t)
	rejects := []struct {
		name  string
		value string
	}{
		{"a human-typed code", "KF-7HTQ-2026"},
		{"a base64url token (an UNHASHED session-style token)", "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZmdoaWprbG0"},
		{"uppercase hex", strings.ToUpper(good)},
		{"63 hex chars (one short)", good[:63]},
		{"65 hex chars (one long)", good + "0"},
		{"empty string", ""},
	}
	for _, tc := range rejects {
		err := insert(tc.value)
		if pg := asPgErr(err); pg == nil || pg.Code != "23514" {
			t.Errorf("code_hash = %s: want check_violation (23514), got %v", tc.name, err)
		}
	}

	// Positive control: the real shape is accepted, so the rejections above are the
	// CHECK and not something structural about the insert.
	if err := insert(good); err != nil {
		t.Fatalf("positive control: a 64-lowercase-hex hash was rejected: %v", err)
	}
}

// TestEmployeeInvites_ExpiryCannotBeAbsent pins BOTH ways of spelling "this invite
// never expires" as unwritable, and -- just as importantly -- records the one shape
// that IS still writable, so nobody reads the guarantee as wider than it is.
//
// The 'infinity' case is not hypothetical: an earlier draft of 00009 had only NOT
// NULL, and `expires_at = 'infinity'` inserted cleanly with `now() < expires_at`
// true forever. The CHECK exists because of that measurement.
func TestEmployeeInvites_ExpiryCannotBeAbsent(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")

	insert := func(expiresSQL string) error {
		return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx,
				`INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at)
				 VALUES ($1, $2, $3, `+expiresSQL+`)`,
				tenantID, employeeID, randCodeHash(t))
			return e
		})
	}

	// NULL -> not-null violation (23502): the omitted expiry.
	err := insert("NULL")
	if pg := asPgErr(err); pg == nil || pg.Code != "23502" {
		t.Fatalf("expires_at = NULL: want not_null_violation (23502), got %v", err)
	}

	// 'infinity' -> check violation (23514): the literal never-expires.
	err = insert("'infinity'::timestamptz")
	if pg := asPgErr(err); pg == nil || pg.Code != "23514" {
		t.Fatalf("expires_at = 'infinity': want check_violation (23514), got %v", err)
	}

	// Positive control AND the documented limit in one assertion: an ordinary
	// expiry is accepted, and so is an absurd-but-FINITE one. The schema bounds
	// "no expiry", not "a sensible TTL" -- that is phase B's decision (00009).
	if err := insert("now() + interval '1 hour'"); err != nil {
		t.Fatalf("positive control: an ordinary expiry was rejected: %v", err)
	}
	if err := insert("'9999-12-31 00:00:00+00'::timestamptz"); err != nil {
		t.Fatalf("a finite far-future expiry is expected to be ACCEPTED (the limit this test documents), got: %v", err)
	}
	// '-infinity' means already expired: representable and fail-closed.
	if err := insert("'-infinity'::timestamptz"); err != nil {
		t.Fatalf("'-infinity' (already expired, fail-closed) must stay representable, got: %v", err)
	}
}

// ------------------------------------------------------------- activation ---

// TestActivateEmployee_StampsOnceAndRefusesDeactivated covers the three status
// paths of the activation write.
func TestActivateEmployee_StampsOnceAndRefusesDeactivated(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)

	// activate = spend a fresh invite for this employee. There is no way to reach
	// 'active' without one, which is the point of the fused statement.
	activate := func(employeeID uuid.UUID) (store.ConsumeInviteAndActivateRow, uuid.UUID, error) {
		inviteID, codeHash := newInvite(t, d, tenantID, employeeID, time.Hour)
		row, err := consume(t, d, tenantID, codeHash)
		return row, inviteID, err
	}

	t.Run("invited becomes active and gets a stamp", func(t *testing.T) {
		id := newEmployee(t, d, tenantID, locationID, "invited")
		row, _, err := activate(id)
		if err != nil {
			t.Fatalf("activate invited: %v", err)
		}
		if row.Status != "active" {
			t.Fatalf("status = %q, want active", row.Status)
		}
		if row.ActivatedAt == nil {
			t.Fatal("activated_at is NULL after activation, want a stamp")
		}
	})

	t.Run("second activation keeps the FIRST stamp", func(t *testing.T) {
		id := newEmployee(t, d, tenantID, locationID, "invited")
		first, _, err := activate(id)
		if err != nil {
			t.Fatalf("first activate: %v", err)
		}
		time.Sleep(5 * time.Millisecond) // now() must differ between statements

		// A SECOND invite (the "new phone" case): allowed at this layer, and the
		// original activation stamp must survive it.
		second, _, err := activate(id)
		if err != nil {
			t.Fatalf("second activation (an already-active employee must not error here): %v", err)
		}
		if second.ActivatedAt == nil {
			t.Fatal("activated_at became NULL on re-activation")
		}
		if !second.ActivatedAt.Equal(*first.ActivatedAt) {
			t.Fatalf("activated_at was rewritten: %v -> %v; the COALESCE must keep the first stamp", *first.ActivatedAt, *second.ActivatedAt)
		}
		if second.Status != "active" {
			t.Fatalf("status = %q, want active", second.Status)
		}
	})

	// A deactivated employee cannot be brought back by spending an invite -- that
	// would route around the manager-only deactivation decision. The invite must
	// also SURVIVE the refusal: burning it would lock the person out until a manager
	// issues a new one, which is evidence loss in the section 4.6 sense.
	t.Run("deactivated is refused and the invite is NOT burned", func(t *testing.T) {
		id := newEmployee(t, d, tenantID, locationID, "deactivated")
		_, inviteID, err := activate(id)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("activating a deactivated employee: error = %v, want pgx.ErrNoRows (an invite must not undo a deactivation)", err)
		}

		if used := inviteUsedAt(t, d, tenantID, inviteID); used != nil {
			t.Fatalf("the invite was consumed (used_at = %v) even though activation failed; the transaction must have rolled the CTE stamp back", used)
		}

		// The employee row is untouched: still deactivated, still unstamped.
		var status string
		var activatedAt *time.Time
		if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT status, activated_at FROM employees WHERE id = $1 AND tenant_id = $2`,
				id, tenantID).Scan(&status, &activatedAt)
		}); err != nil {
			t.Fatalf("re-read deactivated employee: %v", err)
		}
		if status != "deactivated" || activatedAt != nil {
			t.Fatalf("deactivated employee changed: status=%q activated_at=%v", status, activatedAt)
		}
	})
}

// ----------------------------------------------------------------- listing --

// TestListPendingInvitesForEmployee: "pending" means usable right now. Consumed
// and expired invites are excluded, another employee's invite is not visible in
// this employee's list, and the live one IS returned (positive control).
func TestListPendingInvitesForEmployee(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")
	otherEmployeeID := newEmployee(t, d, tenantID, locationID, "invited")

	liveID, _ := newInvite(t, d, tenantID, employeeID, time.Hour)
	expiredID, _ := newInvite(t, d, tenantID, employeeID, -time.Minute)
	consumedID, consumedHash := newInvite(t, d, tenantID, employeeID, time.Hour)
	if _, err := consume(t, d, tenantID, consumedHash); err != nil {
		t.Fatalf("prepare consumed invite: %v", err)
	}
	otherID, _ := newInvite(t, d, tenantID, otherEmployeeID, time.Hour)

	var got []store.ListPendingInvitesForEmployeeRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		got, e = store.New(tx).ListPendingInvitesForEmployee(ctx, store.ListPendingInvitesForEmployeeParams{
			TenantID: tenantID, EmployeeID: employeeID,
		})
		return e
	}); err != nil {
		t.Fatalf("ListPendingInvitesForEmployee: %v", err)
	}

	seen := map[uuid.UUID]bool{}
	for _, r := range got {
		seen[r.ID] = true
		if r.EmployeeID != employeeID {
			t.Errorf("listed an invite of employee %s", r.EmployeeID)
		}
	}
	if !seen[liveID] {
		t.Error("the live invite is missing from the pending list (positive control)")
	}
	if seen[expiredID] {
		t.Error("an EXPIRED invite is listed as pending")
	}
	if seen[consumedID] {
		t.Error("a CONSUMED invite is listed as pending")
	}
	if seen[otherID] {
		t.Error("another employee's invite is listed")
	}
}

// ------------------------------------------------------------- resolution ---

// TestGetInviteByCodeHash_ResolvesContextLess: the hand-written resolver reads an
// invite WITHOUT a tenant context (the tenant is the RESULT) and returns the right
// tenant and employee. Proven by calling it on the bare pool (no WithTenant).
func TestGetInviteByCodeHash_ResolvesContextLess(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")
	inviteID, codeHash := newInvite(t, d, tenantID, employeeID, time.Hour)

	got, err := d.GetInviteByCodeHash(context.Background(), codeHash)
	if err != nil {
		t.Fatalf("GetInviteByCodeHash: %v", err)
	}
	if got.TenantID != tenantID {
		t.Fatalf("tenant_id = %s, want %s (resolution must recover the tenant)", got.TenantID, tenantID)
	}
	if got.ID != inviteID || got.EmployeeID != employeeID {
		t.Fatalf("resolved %+v, want invite %s employee %s", got, inviteID, employeeID)
	}
	if got.UsedAt != nil {
		t.Fatalf("used_at = %v, want nil (fresh invite)", got.UsedAt)
	}
	if !got.ExpiresAt.After(time.Now()) {
		t.Fatalf("expires_at = %v, want a future instant", got.ExpiresAt)
	}
}

// TestGetInviteByCodeHash_UnknownReturnsNoRows: an unknown hash yields
// pgx.ErrNoRows, never a zero-valued struct that a caller might treat as a hit.
func TestGetInviteByCodeHash_UnknownReturnsNoRows(t *testing.T) {
	d := appDB(t)
	_, err := d.GetInviteByCodeHash(context.Background(), randCodeHash(t))
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetInviteByCodeHash(unknown) error = %v, want pgx.ErrNoRows", err)
	}
}

// TestGetInviteByCodeHash_CarriesTruthWithoutFiltering: the resolver returns a
// CONSUMED and an EXPIRED invite just as it returns a live one, reporting their
// state instead of hiding them (00009 / the sessions.revoked_at principle). If it
// filtered, a replayed code would be indistinguishable from an unknown one and
// the attempt could not be attributed to anyone -- section 4.6.
func TestGetInviteByCodeHash_CarriesTruthWithoutFiltering(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")

	_, consumedHash := newInvite(t, d, tenantID, employeeID, time.Hour)
	if _, err := consume(t, d, tenantID, consumedHash); err != nil {
		t.Fatalf("prepare consumed invite: %v", err)
	}
	gotConsumed, err := d.GetInviteByCodeHash(context.Background(), consumedHash)
	if err != nil {
		t.Fatalf("resolve consumed invite: %v (it must still resolve)", err)
	}
	if gotConsumed.UsedAt == nil {
		t.Fatal("resolved consumed invite has used_at = nil; the resolver must carry the truth")
	}

	_, expiredHash := newInvite(t, d, tenantID, employeeID, -time.Minute)
	gotExpired, err := d.GetInviteByCodeHash(context.Background(), expiredHash)
	if err != nil {
		t.Fatalf("resolve expired invite: %v (it must still resolve)", err)
	}
	if !gotExpired.ExpiresAt.Before(time.Now()) {
		t.Fatalf("resolved expired invite has expires_at = %v, want the past", gotExpired.ExpiresAt)
	}
	if gotExpired.UsedAt != nil {
		t.Fatalf("expired-but-unused invite has used_at = %v, want nil", gotExpired.UsedAt)
	}
}

// TestResolveInviteColumns_MatchSchema is the drift guard for the hand-written
// SQL in resolve.go (M1-08 handoff): the const mirrors 00009's RETURNS TABLE
// signature BY HAND, so a future column-order change would compile and return
// wrong data. Every field is checked against the inserted value.
func TestResolveInviteColumns_MatchSchema(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")

	before := time.Now()
	inviteID, codeHash := newInvite(t, d, tenantID, employeeID, 2*time.Hour)

	got, err := d.GetInviteByCodeHash(context.Background(), codeHash)
	if err != nil {
		t.Fatalf("GetInviteByCodeHash: %v", err)
	}
	if got.ID != inviteID {
		t.Errorf("ID = %s, want %s (column order/contract drift)", got.ID, inviteID)
	}
	if got.TenantID != tenantID {
		t.Errorf("TenantID = %s, want %s", got.TenantID, tenantID)
	}
	if got.EmployeeID != employeeID {
		t.Errorf("EmployeeID = %s, want %s", got.EmployeeID, employeeID)
	}
	// expires_at was written as now+2h by the caller: it must land in that window,
	// which no other column of this row would satisfy.
	if got.ExpiresAt.Before(before.Add(time.Hour)) || got.ExpiresAt.After(before.Add(3*time.Hour)) {
		t.Errorf("ExpiresAt = %v, want ~%v", got.ExpiresAt, before.Add(2*time.Hour))
	}
	if got.UsedAt != nil {
		t.Errorf("UsedAt = %v, want nil", got.UsedAt)
	}
}

// TestResolveInvite_SecurityDefinerProperties measures, in the LIVE catalog and
// against live data, the ADR 0002 madde 7 constraints for
// resolve_invite_by_code_hash -- rather than trusting the migration text.
//
// It covers TWO different requirements, kept apart on purpose:
//
//	A. The ADR's LEAST-PRIVILEGE clause on the definer: owner tappa_resolver (not
//	   a superuser, which would make the body bypass RLS database-wide), BYPASSRLS
//	   (without it the body sees 0 rows), SECURITY DEFINER, and a fixed
//	   search_path (a mutable one is an injection vector).
//	B. The ADR's FIVE interface constraints, (i)..(v) as labelled below.
//
// The (iv) probe writes nothing and the whole test is read-only apart from its own
// fixture, so it is safe to run alongside another auditor.
func TestResolveInvite_SecurityDefinerProperties(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d) // (iv) is meaningless unless we are the RLS-subject role
	ctx := context.Background()

	var owner string
	var ownerSuper, ownerBypass, secDef bool
	var config []string
	if err := d.pool.QueryRow(ctx, `
		SELECT pg_get_userbyid(p.proowner), r.rolsuper, r.rolbypassrls, p.prosecdef, p.proconfig
		FROM pg_proc p
		JOIN pg_roles r ON r.oid = p.proowner
		WHERE p.proname = 'resolve_invite_by_code_hash'`,
	).Scan(&owner, &ownerSuper, &ownerBypass, &secDef, &config); err != nil {
		t.Fatalf("read function catalog: %v", err)
	}
	if owner != "tappa_resolver" {
		t.Errorf("owner = %q, want tappa_resolver (the least-privileged definer)", owner)
	}
	if ownerSuper {
		t.Error("the definer role is a SUPERUSER: the function body would bypass RLS database-wide")
	}
	if !ownerBypass {
		t.Error("the definer role lacks BYPASSRLS: context-less resolution would return 0 rows")
	}
	if !secDef {
		t.Error("prosecdef = false: the function does not run as its owner")
	}
	wantCfg := "search_path=pg_catalog, pg_temp"
	if len(config) != 1 || config[0] != wantCfg {
		t.Errorf("proconfig = %v, want [%q] (a mutable search_path is an injection vector)", config, wantCfg)
	}

	var publicExec, appExec bool
	if err := d.pool.QueryRow(ctx, `
		SELECT has_function_privilege('public', 'resolve_invite_by_code_hash(text)', 'EXECUTE'),
		       has_function_privilege('tappa_app', 'resolve_invite_by_code_hash(text)', 'EXECUTE')`,
	).Scan(&publicExec, &appExec); err != nil {
		t.Fatalf("read function privileges: %v", err)
	}
	if publicExec {
		t.Error("PUBLIC holds EXECUTE (functions get it by default; the REVOKE must remove it)")
	}
	if !appExec {
		t.Error("tappa_app lacks EXECUTE: the resolution path would be dead")
	}

	// --- (i) the input is only a KEY ---------------------------------------
	// One parameter, of the key's type. A second parameter (a tenant, a flag, a
	// column selector) would widen the interface beyond "look this key up".
	var args, result string
	if err := d.pool.QueryRow(ctx, `
		SELECT pg_get_function_arguments(p.oid), pg_get_function_result(p.oid)
		FROM pg_proc p WHERE p.proname = 'resolve_invite_by_code_hash'`,
	).Scan(&args, &result); err != nil {
		t.Fatalf("read function signature: %v", err)
	}
	if args != "p_code_hash text" {
		t.Errorf("(i) arguments = %q, want exactly %q (key-only input)", args, "p_code_hash text")
	}

	// --- (iii) no SELECT * surface -----------------------------------------
	// The declared result IS the column list; pinning it also catches drift
	// against the hand-written scan order in resolve.go.
	//
	// ⚠️ IT GREW BY ONE COLUMN IN 00012 AND THAT IS WHY IT IS PINNED. cancelled_at
	// joined the list because a retired invitation must be tellable apart from an
	// unknown code — otherwise the audit trail writes the wrong reason for exactly
	// the attempt the cancellation exists to expose. The list is still MINIMAL:
	// code_hash (the input) and created_at (more than the caller needs) are still
	// absent, and this assertion is what makes adding a fourth column a decision
	// rather than an edit.
	const wantResult = "TABLE(id uuid, tenant_id uuid, employee_id uuid, expires_at timestamp with time zone, used_at timestamp with time zone, cancelled_at timestamp with time zone)"
	if result != wantResult {
		t.Errorf("(iii) result = %q, want %q (fixed minimal column list; code_hash and created_at must NOT be returned)", result, wantResult)
	}

	// --- (ii) at most one row ----------------------------------------------
	// STRUCTURAL, from the global UNIQUE index on code_hash -- not a property of
	// the function body. Verified in the catalog so a future migration dropping
	// the constraint fails here.
	var uniqueIdx int
	if err := d.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = i.indkey[0]
		WHERE c.relname = 'employee_invites'
		  AND i.indisunique
		  AND i.indnatts = 1
		  AND a.attname = 'code_hash'`,
	).Scan(&uniqueIdx); err != nil {
		t.Fatalf("read unique index: %v", err)
	}
	if uniqueIdx != 1 {
		t.Errorf("(ii) employee_invites.code_hash single-column UNIQUE indexes = %d, want 1 (this is what bounds the context-less lookup to <=1 row)", uniqueIdx)
	}

	// --- (v) no naive "context is NULL -> show the row" RLS branch -----------
	// The policy must be the normative NULLIF equality and nothing else. An
	// additive OR branch is exactly the rejected GUC-keyed design (ADR 0002).
	var qual, withCheck string
	if err := d.pool.QueryRow(ctx, `
		SELECT qual, with_check FROM pg_policies
		WHERE tablename = 'employee_invites'`,
	).Scan(&qual, &withCheck); err != nil {
		t.Fatalf("read policy: %v", err)
	}
	for _, e := range []struct{ name, expr string }{{"USING", qual}, {"WITH CHECK", withCheck}} {
		if !strings.Contains(e.expr, "NULLIF(current_setting('app.tenant_id'::text, true), ''::text)") {
			t.Errorf("(v) %s expression %q is not the normative NULLIF form", e.name, e.expr)
		}
		if strings.Contains(strings.ToLower(e.expr), " or ") {
			t.Errorf("(v) %s expression %q contains an OR branch; a context-less permit branch is forbidden", e.name, e.expr)
		}
	}

	// --- (iv) tappa_app cannot read cross-tenant directly, only via EXECUTE ---
	// The live proof, not a privilege reading: with NO tenant context a direct
	// SELECT on the table returns 0 rows (FORCE RLS, fail-closed) while the
	// function returns exactly 1 for the same key. Both run as tappa_app on the
	// same pool. Without the positive control (the 1) the 0 would be vacuous.
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")
	_, codeHash := newInvite(t, d, tenantID, employeeID, time.Hour)

	var direct, viaFunc int
	if err := d.pool.QueryRow(ctx,
		`SELECT count(*) FROM employee_invites WHERE code_hash = $1`, codeHash).Scan(&direct); err != nil {
		t.Fatalf("(iv) context-less direct read errored (%v); it must return 0 rows, not fail", err)
	}
	if direct != 0 {
		t.Errorf("(iv) a context-less direct SELECT returned %d rows; RLS must hide the row from tappa_app outside a tenant context", direct)
	}
	if err := d.pool.QueryRow(ctx,
		`SELECT count(*) FROM resolve_invite_by_code_hash($1)`, codeHash).Scan(&viaFunc); err != nil {
		t.Fatalf("(iv) context-less resolution errored: %v", err)
	}
	if viaFunc != 1 {
		t.Errorf("(iv) the bounded interface returned %d rows, want 1 (otherwise the 0 above proves nothing)", viaFunc)
	}
}

// --------------------------------------------------------------- privilege --

// TestEmployeeInvites_AppCannotDelete proves section 4.6 for this table the way
// M1-04 says it must be proven: EMPIRICALLY. db-init's ALTER DEFAULT PRIVILEGES
// grants DELETE on every new table, so leaving DELETE out of the GRANT does
// NOTHING -- only the explicit REVOKE removes it. Both the catalog answer and a
// real DELETE statement are checked, with a SELECT positive control so the denial
// cannot be mistaken for invisibility.
func TestEmployeeInvites_AppCannotDelete(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")
	inviteID, _ := newInvite(t, d, tenantID, employeeID, time.Hour)

	var canDelete, canSelect, canInsert, canUpdateTable bool
	if err := d.pool.QueryRow(context.Background(), `
		SELECT has_table_privilege('tappa_app', 'employee_invites', 'DELETE'),
		       has_table_privilege('tappa_app', 'employee_invites', 'SELECT'),
		       has_table_privilege('tappa_app', 'employee_invites', 'INSERT'),
		       has_table_privilege('tappa_app', 'employee_invites', 'UPDATE')`,
	).Scan(&canDelete, &canSelect, &canInsert, &canUpdateTable); err != nil {
		t.Fatalf("read table privileges: %v", err)
	}
	if canDelete {
		t.Error("tappa_app holds DELETE on employee_invites (the REVOKE is missing or was undone)")
	}
	if !canSelect || !canInsert {
		t.Errorf("tappa_app privileges select=%v insert=%v, want both true", canSelect, canInsert)
	}
	// TABLE-wide UPDATE must be gone: only the used_at column is writable
	// (TestEmployeeInvites_UpdateIsColumnScoped proves what that buys).
	if canUpdateTable {
		t.Error("tappa_app holds TABLE-WIDE UPDATE on employee_invites; only UPDATE (used_at) may be granted")
	}

	// Positive control: the app CAN read its own invite, so the failure below is
	// the missing privilege and not an invisible row.
	if got := rawCountCtx(t, d, tenantID, `SELECT count(*) FROM employee_invites WHERE id = $1`, inviteID); got != 1 {
		t.Fatalf("positive control: app cannot SELECT its own invite (got %d rows)", got)
	}

	errD := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "DELETE FROM employee_invites WHERE id = $1", inviteID)
		return e
	})
	assertPermissionDenied(t, "app DELETE on employee_invites", errD)

	// Still there: the blocked DELETE changed nothing.
	if got := rawCountCtx(t, d, tenantID, `SELECT count(*) FROM employee_invites WHERE id = $1`, inviteID); got != 1 {
		t.Fatalf("invite row count after the blocked DELETE = %d, want 1", got)
	}
}

// TestEmployeeInvites_UpdateIsColumnScoped proves the column-level UPDATE grant --
// the auditor's finding that a table-wide UPDATE let the application do two things
// this repo never intended and never mentioned:
//
//   - RESURRECT an expired invite by pushing expires_at into the future, undoing
//     the expiry guarantee 00009 spends a paragraph on;
//   - REASSIGN a live invite to a different employee of the same tenant, so a code
//     mailed to one person activates another.
//
// (Un-consuming, SET used_at = NULL, was the only one the file admitted to.)
// GRANT UPDATE (used_at) closes the first two at the PRIVILEGE level. Every
// assertion here is a real statement run as tappa_app, not a catalog reading, and
// the consumption positive control proves the grant did not break the one write
// that must work.
//
// THE RESIDUAL LIMIT IS ASSERTED TOO, deliberately: a column grant constrains WHICH
// column may be written, never WHAT value, so SET used_at = NULL still succeeds.
// The last case measures that instead of pretending otherwise -- what forbids it is
// query discipline (no query does it), and making it structural needs a trigger.
func TestEmployeeInvites_UpdateIsColumnScoped(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	tenantID, locationID := newTenant(t, d)
	employeeID := newEmployee(t, d, tenantID, locationID, "invited")
	otherEmployeeID := newEmployee(t, d, tenantID, locationID, "invited")
	inviteID, codeHash := newInvite(t, d, tenantID, employeeID, time.Hour)

	// Catalog view first: used_at writable, the rest not.
	var cUsed, cExpires, cEmployee, cHash, cCreated bool
	if err := d.pool.QueryRow(context.Background(), `
		SELECT has_column_privilege('tappa_app', 'employee_invites', 'used_at',     'UPDATE'),
		       has_column_privilege('tappa_app', 'employee_invites', 'expires_at',  'UPDATE'),
		       has_column_privilege('tappa_app', 'employee_invites', 'employee_id', 'UPDATE'),
		       has_column_privilege('tappa_app', 'employee_invites', 'code_hash',   'UPDATE'),
		       has_column_privilege('tappa_app', 'employee_invites', 'created_at',  'UPDATE')`,
	).Scan(&cUsed, &cExpires, &cEmployee, &cHash, &cCreated); err != nil {
		t.Fatalf("read column privileges: %v", err)
	}
	if !cUsed {
		t.Error("tappa_app cannot UPDATE used_at: consumption would be impossible")
	}
	if cExpires || cEmployee || cHash || cCreated {
		t.Errorf("tappa_app can UPDATE expires_at=%v employee_id=%v code_hash=%v created_at=%v, want all false",
			cExpires, cEmployee, cHash, cCreated)
	}

	// The statements themselves, as tappa_app in its own tenant context.
	exec := func(sql string, args ...any) error {
		return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, sql, args...)
			return e
		})
	}

	assertPermissionDenied(t, "resurrect an invite (SET expires_at)",
		exec(`UPDATE employee_invites SET expires_at = now() + interval '10 years' WHERE id = $1 AND tenant_id = $2`, inviteID, tenantID))
	assertPermissionDenied(t, "reassign an invite (SET employee_id)",
		exec(`UPDATE employee_invites SET employee_id = $3 WHERE id = $1 AND tenant_id = $2`, inviteID, tenantID, otherEmployeeID))
	assertPermissionDenied(t, "rewrite the hash (SET code_hash)",
		exec(`UPDATE employee_invites SET code_hash = $3 WHERE id = $1 AND tenant_id = $2`, inviteID, tenantID, randCodeHash(t)))

	// Positive control: the ONE write the application must be able to make still
	// works through the real query, so the REVOKE did not break activation.
	row, err := consume(t, d, tenantID, codeHash)
	if err != nil {
		t.Fatalf("positive control: consumption broke under the column grant: %v", err)
	}
	if row.ID != employeeID || row.Status != "active" {
		t.Fatalf("consumption returned %s/%q, want %s/active", row.ID, row.Status, employeeID)
	}

	// The residual limit, measured rather than assumed: the privilege system does
	// NOT stop a caller from writing a DIFFERENT value into the granted column, so
	// an un-consume statement succeeds. No query in db/queries does this; that is
	// the only thing standing in its way, and it is written down as such.
	if err := exec(`UPDATE employee_invites SET used_at = NULL WHERE id = $1 AND tenant_id = $2`, inviteID, tenantID); err != nil {
		t.Fatalf("expected the documented LIMIT to hold (un-consume is privilege-legal), got: %v", err)
	}
	if used := inviteUsedAt(t, d, tenantID, inviteID); used != nil {
		t.Fatalf("un-consume did not take effect (used_at = %v); the limit description would be wrong", used)
	}
}

// TestConsumeInvite_DeactivatedEmployeeCannotBurnTheInvite is the proof that the
// invite's survival is STRUCTURAL, not a courtesy of the caller.
//
// A data-modifying CTE executes unconditionally, so before the EXISTS guard was
// added to the inner UPDATE the statement stamped used_at for a deactivated
// employee while activating nobody. That was harmless ONLY while every caller
// propagated pgx.ErrNoRows, because db.WithTenant rolls back on error -- measured:
// with ROLLBACK the invite survived, with COMMIT it was burned. One careless line
// (`row, _ := q.ConsumeInviteAndActivate(...)`) was enough to lose a code and lock
// an employee out until a manager issued another one.
//
// This test simulates exactly that careless caller: it IGNORES the query error and
// returns nil from the transaction function, so WithTenant COMMITS. The invite must
// still be unconsumed afterwards, which is only possible if the database refused to
// write the stamp in the first place.
func TestConsumeInvite_DeactivatedEmployeeCannotBurnTheInvite(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	deactivatedID := newEmployee(t, d, tenantID, locationID, "deactivated")
	inviteID, codeHash := newInvite(t, d, tenantID, deactivatedID, time.Hour)

	// The careless caller: swallow the error, let the transaction COMMIT.
	var queryErr error
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, queryErr = store.New(tx).ConsumeInviteAndActivate(ctx, store.ConsumeInviteAndActivateParams{
			CodeHash: codeHash, TenantID: tenantID,
		})
		return nil // deliberately NOT propagated -- this is the bug being defended against
	}); err != nil {
		t.Fatalf("the transaction itself must commit for this test to mean anything: %v", err)
	}
	if !errors.Is(queryErr, pgx.ErrNoRows) {
		t.Fatalf("query error = %v, want pgx.ErrNoRows (a deactivated employee is never activated)", queryErr)
	}

	// THE ASSERTION: committed, and the invite is still spendable.
	if used := inviteUsedAt(t, d, tenantID, inviteID); used != nil {
		t.Fatalf("the invite was BURNED (used_at = %v) by a committed transaction that activated nobody; the EXISTS guard in the CTE must prevent the stamp", used)
	}

	// Positive control: the very same invite still works once the employee is
	// activatable again -- proving the row above was genuinely intact, not merely
	// unreadable, and that the EXISTS guard does not block the normal path.
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE employees SET status = 'invited' WHERE id = $1 AND tenant_id = $2`, deactivatedID, tenantID)
		return e
	}); err != nil {
		t.Fatalf("re-enable employee for the positive control: %v", err)
	}
	row, err := consume(t, d, tenantID, codeHash)
	if err != nil {
		t.Fatalf("positive control: the surviving invite could not be spent: %v", err)
	}
	if row.ID != deactivatedID || row.Status != "active" {
		t.Fatalf("positive control returned %s/%q, want %s/active", row.ID, row.Status, deactivatedID)
	}
}

// cancelPending retires every spendable invitation of one employee through the
// PRODUCTION statement, so this test cannot pass against a fixture the product could
// not produce.
func cancelPending(t *testing.T, d *DB, tenantID, employeeID uuid.UUID) {
	t.Helper()
	var ids []uuid.UUID
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		ids, e = store.New(tx).CancelPendingInvitesForEmployee(ctx,
			store.CancelPendingInvitesForEmployeeParams{TenantID: tenantID, EmployeeID: employeeID})
		return e
	}); err != nil {
		t.Fatalf("cancel pending invites: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("nothing was cancelled; the fixture has no spendable invitation and the " +
			"assertions that follow would be vacuous")
	}
}
