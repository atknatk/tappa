package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// passwordresets_test.go -- the DATA-LAYER proof for M7-04 phase A (admin password
// reset). It runs against a REAL Postgres (CLAUDE.md section 8, Q04): a fake database
// cannot test RLS, cannot test a column-level GRANT and cannot test that two
// concurrent consumers of one token produce exactly one winner.
//
// WHAT IT MEASURES, and why each is here rather than trusted from the migration text:
//
//   1. THE SEVEN CAPABILITIES 00019 CLOSED. Migration 00019 records that before it,
//      as tappa_app inside its own tenant context, one statement could re-point a live
//      reset token at ANOTHER administrator of the same tenant -- an account-takeover
//      primitive with RLS satisfied throughout, because it is the same tenant. That
//      measurement is a paragraph in a file; these are the assertions that keep it
//      true. Each negative arm is paired with a POSITIVE CONTROL, because a privilege
//      probe that fails for the wrong reason (a typo'd column, an empty fixture) goes
//      green while proving nothing.
//   2. THE SIXTH CONTEXT-LESS RESOLVER's five ADR 0002 madde 7 constraints, read out
//      of the LIVE catalog.
//   3. THE ATOMIC SINGLE USE, with N concurrent goroutines under -race and a negative
//      control showing the naive read-then-write producing many winners.
//   4. CROSS-TENANT REFUSAL over the production statement AND a raw, unfiltered RLS
//      probe on the new column -- the two shapes CLAUDE.md section 6 says are
//      different jobs.
//
// NO TEST HERE HOLDS A RESET TOKEN (section 4.7). Every token_hash is 64 random hex
// characters -- the shape internal/adminauth's HMAC produces and the shape 00019's
// CHECK enforces -- and it is the pre-image of nothing. The token itself exists only
// inside internal/adminauth, and even there only in transit.
//
// FIXTURES ARE NOT CLEANED UP, for the reason store_test.go/rls_test.go give: tappa_app
// has REVOKE DELETE on password_resets, so the impossibility of teardown IS the audit
// guarantee.

// ---------------------------------------------------------------- fixtures --

// randTokenHash returns a fresh 64-lowercase-hex string. Random bytes, NOT the hash of
// anything: no test holds a reset token.
func randTokenHash(t *testing.T) string {
	t.Helper()
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b[:])
}

// newReset issues a reset through the REAL query and returns the row plus the hash it
// was stored under. ttl < 0 creates an ALREADY EXPIRED reset (neither an absent nor an
// infinite expiry is representable -- see TestPasswordResets_ExpiryIsBounded).
func newReset(t *testing.T, d *DB, tenantID, adminID uuid.UUID, ttl time.Duration) (store.CreatePasswordResetRow, string) {
	t.Helper()
	hash := randTokenHash(t)
	var row store.CreatePasswordResetRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreatePasswordReset(ctx, store.CreatePasswordResetParams{
			TenantID:    tenantID,
			AdminUserID: adminID,
			TokenHash:   hash,
			ExpiresAt:   time.Now().Add(ttl),
		})
		return e
	}); err != nil {
		t.Fatalf("CreatePasswordReset: %v", err)
	}
	return row, hash
}

// consumeReset runs the real reset statement in the tenant's context, keyed by the
// TOKEN HASH -- the only key that exists, because spending a reset requires knowing
// the token. The error is returned so WithTenant rolls back exactly as production does.
func consumeReset(t *testing.T, d *DB, tenantID uuid.UUID, hash, digest string) (store.ConsumePasswordResetAndSetPasswordRow, error) {
	t.Helper()
	var row store.ConsumePasswordResetAndSetPasswordRow
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).ConsumePasswordResetAndSetPassword(ctx, store.ConsumePasswordResetAndSetPasswordParams{
			TokenHash: hash, TenantID: tenantID, PasswordHash: digest,
		})
		return e
	})
	return row, err
}

// liveResets reads the administrator's still-spendable resets through the production
// query.
func liveResets(t *testing.T, d *DB, tenantID, adminID uuid.UUID) []store.ListLivePasswordResetsForAdminRow {
	t.Helper()
	var rows []store.ListLivePasswordResetsForAdminRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).ListLivePasswordResetsForAdmin(ctx, store.ListLivePasswordResetsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID,
		})
		return e
	}); err != nil {
		t.Fatalf("ListLivePasswordResetsForAdmin: %v", err)
	}
	return rows
}

// otherDigest is a second SHAPE-VALID bcrypt digest, distinct from
// fixtures.UnusablePasswordHash, so a test can tell "the password was written" from
// "the password was left alone" by reading the column back. It hashes nothing and is
// never compared against a password.
const otherDigest = "$2a$04$" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// storedDigest reads an administrator's password_hash back. It is the ONLY place in
// this file that reads the column, and it exists so the assertions are about the
// DATABASE's state rather than about a returned row.
func storedDigest(t *testing.T, d *DB, tenantID, adminID uuid.UUID) string {
	t.Helper()
	var got string
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT password_hash FROM admin_users WHERE id = $1 AND tenant_id = $2`,
			adminID, tenantID).Scan(&got)
	}); err != nil {
		t.Fatalf("read password_hash: %v", err)
	}
	return got
}

// ------------------------------------------------- 1. the closed capabilities --

// TestPasswordResets_WritableColumnsAreExactlyTwo pins 00019's column-level UPDATE
// grant in the catalog, then proves each closure with a live statement.
//
// 🔴 THE has_column_privilege HALF ALONE WOULD BE A WEAK TEST: it asserts what the
// catalog says, not what the server does. Both halves are here, and the live half is
// what would have caught the M1-04 trap (narrowing a GRANT does nothing until the
// table privilege is REVOKEd first -- 00016 and 00017 each record making that mistake).
func TestPasswordResets_WritableColumnsAreExactlyTwo(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	ctx := context.Background()

	want := map[string]bool{
		"id": false, "tenant_id": false, "admin_user_id": false, "token_hash": false,
		"created_at": false, "expires_at": false, "used_at": true, "cancelled_at": true,
	}
	for col, allowed := range want {
		var got bool
		if err := d.pool.QueryRow(ctx,
			`SELECT has_column_privilege('tappa_app', 'password_resets', $1, 'UPDATE')`, col,
		).Scan(&got); err != nil {
			t.Fatalf("has_column_privilege(%s): %v", col, err)
		}
		if got != allowed {
			t.Errorf("UPDATE on password_resets.%s = %v, want %v", col, got, allowed)
		}
	}

	tenantID, _ := newTenant(t, d)
	victim := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	holder := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")
	row, _ := newReset(t, d, tenantID, holder, time.Hour)

	// Each arm is the exact statement 00019 measured as WORKING before it existed.
	// They run in the reset's OWN tenant context, so RLS is satisfied throughout --
	// which is the point: this is a same-tenant privilege escalation that RLS cannot
	// see.
	denied := []struct {
		name string
		sql  string
		arg  any
	}{
		{"A1 re-point a live token at another administrator",
			`UPDATE password_resets SET admin_user_id = $2 WHERE id = $1`, victim},
		{"A2 resurrect: push the expiry out",
			`UPDATE password_resets SET expires_at = now() + interval '10 years' WHERE id = $1`, nil},
		{"A4 graft a chosen hash onto an existing row",
			`UPDATE password_resets SET token_hash = repeat('b', 64) WHERE id = $1`, nil},
		{"rewrite the row's tenant",
			`UPDATE password_resets SET tenant_id = $2 WHERE id = $1`, tenantID},
		{"back-date the request",
			`UPDATE password_resets SET created_at = now() - interval '1 day' WHERE id = $1`, nil},
	}
	for _, c := range denied {
		t.Run(c.name, func(t *testing.T) {
			err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
				if c.arg == nil {
					_, e := tx.Exec(ctx, c.sql, row.ID)
					return e
				}
				_, e := tx.Exec(ctx, c.sql, row.ID, c.arg)
				return e
			})
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("got %v, want SQLSTATE 42501 (insufficient_privilege)", err)
			}
		})
	}

	// POSITIVE CONTROL. The two permitted writes must still work, on the SAME row --
	// otherwise every arm above could be failing because the fixture is unreachable.
	t.Run("positive control: the two granted columns are writable", func(t *testing.T) {
		if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx,
				`UPDATE password_resets SET used_at = now() WHERE id = $1 AND used_at IS NULL`, row.ID); e != nil {
				return e
			}
			_, e := tx.Exec(ctx,
				`UPDATE password_resets SET cancelled_at = now() WHERE id = $1 AND cancelled_at IS NULL`, row.ID)
			return e
		}); err != nil {
			t.Fatalf("the granted writes failed: %v -- every refusal above is then vacuous", err)
		}
	})
}

// TestPasswordResets_ExpiryIsBounded proves the card's "süreli" criterion is a
// property of the DATABASE and not of a comment. 00006 made expires_at NOT NULL, which
// blocks only the OMITTED expiry; 00019 measured two ways to spell "never expires"
// that NOT NULL let through.
func TestPasswordResets_ExpiryIsBounded(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	ctx := context.Background()
	tenantID, _ := newTenant(t, d)
	admin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")

	insert := func(cols, vals string, args ...any) error {
		return d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx,
				`INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, `+cols+`)
				 VALUES ($1, $2, $3, `+vals+`)`,
				append([]any{tenantID, admin, randTokenHash(t)}, args...)...)
			return e
		})
	}

	t.Run("A5 a literally immortal link is refused", func(t *testing.T) {
		err := insert("expires_at", "'infinity'")
		assertCheckViolation(t, err, "password_resets_ttl_ceiling")
	})
	t.Run("a ten-year link is refused", func(t *testing.T) {
		err := insert("expires_at", "now() + interval '10 years'")
		assertCheckViolation(t, err, "password_resets_ttl_ceiling")
	})
	t.Run("A6 a future created_at cannot buy a long window", func(t *testing.T) {
		// The CHECK bounds a SPAN, so the second half of the hole is the OTHER column:
		// a row stamped ten years ahead carries a legal 24-hour TTL that opens in 2036
		// and is live the whole way there. It is closed by the INSERT grant, not by the
		// CHECK -- hence 42501 rather than 23514, and hence this arm exists.
		err := insert("created_at, expires_at", "now() + interval '10 years', now() + interval '10 years'")
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("got %v, want SQLSTATE 42501 -- created_at must not be insertable", err)
		}
	})
	t.Run("positive control: an ordinary one-hour link is accepted", func(t *testing.T) {
		if err := insert("expires_at", "now() + interval '1 hour'"); err != nil {
			t.Fatalf("a legitimate reset was refused: %v -- every refusal above is then vacuous", err)
		}
	})
	t.Run("an already-expired link stays representable", func(t *testing.T) {
		// '-infinity' and a past instant are fail-CLOSED and deliberately allowed
		// (00009's reasoning, restated in 00019): a dead token is harmless and the
		// fixtures that test expiry need to be able to write one.
		if err := insert("expires_at", "now() - interval '1 hour'"); err != nil {
			t.Fatalf("an already-expired reset must remain writable: %v", err)
		}
	})
}

// TestPasswordResets_TokenHashMustBeAHash proves 00019's shape CHECK -- the statement
// that turns 00006's comment ("the reset token is never stored, only its hash") into
// something the database enforces. A7 in that migration wrote the literal string
// 'plaintext-reset-token' into the column and got INSERT 0 1.
//
// THE LIMIT, restated so the test is not read as more than it is: this is a SHAPE
// test, never a PROVENANCE test. A 64-lowercase-hex string that happens to BE the
// token would pass, because no constraint can ask "is this the HMAC of something?".
// Deriving the value under a server-side key is internal/adminauth's obligation.
func TestPasswordResets_TokenHashMustBeAHash(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	ctx := context.Background()
	tenantID, _ := newTenant(t, d)
	admin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")

	insert := func(hash string) error {
		return d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx,
				`INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at)
				 VALUES ($1, $2, $3, now() + interval '1 hour')`,
				tenantID, admin, hash)
			return e
		})
	}

	// The rejected values are written out here rather than derived from the pattern:
	// a case list generated from `^[0-9a-f]{64}$` would be testing the regexp against
	// itself.
	rejected := map[string]string{
		"a plaintext token":            "plaintext-reset-token",
		"the UNHASHED base64url shape": "8Jq2vN4pLm7XeR1tYc0aBd3FgHi5JkLmNoPqRsTuVwX",
		"upper-case hex":               strings.ToUpper(randTokenHash(t)),
		"63 hex characters":            randTokenHash(t)[:63],
		"65 hex characters":            randTokenHash(t) + "0",
		"empty":                        "",
	}
	for name, v := range rejected {
		t.Run(name, func(t *testing.T) {
			assertCheckViolation(t, insert(v), "password_resets_token_hash_shape")
		})
	}
	t.Run("positive control: 64 lower-case hex is accepted", func(t *testing.T) {
		if err := insert(randTokenHash(t)); err != nil {
			t.Fatalf("a hash-shaped value was refused: %v -- every refusal above is then vacuous", err)
		}
	})
}

// assertCheckViolation fails unless err is SQLSTATE 23514 naming the given constraint.
// Naming it matters: a test that accepted ANY 23514 would go green when a DIFFERENT
// constraint fired, which is exactly how a check-constraint test becomes vacuous.
func assertCheckViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("got %v, want a check violation on %s", err, constraint)
	}
	if pgErr.Code != "23514" || pgErr.ConstraintName != constraint {
		t.Fatalf("got SQLSTATE %s on %q, want 23514 on %q", pgErr.Code, pgErr.ConstraintName, constraint)
	}
}

// ------------------------------------------------------ 2. issue and retire --

// TestCreatePasswordReset_RetiresTheLivingSiblings is the M6-05 lesson paid forward.
// That milestone measured, over real HTTP, that two presses of "Send invite" left two
// simultaneously valid links and that the OLDER one still worked after the newer had
// been spent -- a live account-takeover credential in whatever inbox the first link
// reached. This is the same button with a bigger prize.
func TestCreatePasswordReset_RetiresTheLivingSiblings(t *testing.T) {
	d := appDB(t)
	tenantID, _ := newTenant(t, d)
	admin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")

	first, firstHash := newReset(t, d, tenantID, admin, time.Hour)
	if first.RetiredCount != 0 {
		t.Errorf("the first reset retired %d links, want 0", first.RetiredCount)
	}
	second, secondHash := newReset(t, d, tenantID, admin, time.Hour)
	if second.RetiredCount != 1 {
		t.Errorf("the second reset retired %d links, want 1", second.RetiredCount)
	}

	if live := liveResets(t, d, tenantID, admin); len(live) != 1 || live[0].ID != second.ID {
		t.Fatalf("live resets = %d (%v), want exactly the newest one", len(live), live)
	}

	// THE ASSERTION THAT MATTERS is not the count but the CONSEQUENCE: the older link
	// must be unspendable.
	if _, err := consumeReset(t, d, tenantID, firstHash, otherDigest); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the superseded link was spendable (err = %v) -- this is the M6-05 defect", err)
	}
	if storedDigest(t, d, tenantID, admin) != fixtures.UnusablePasswordHash {
		t.Fatal("the superseded link changed the password")
	}

	// POSITIVE CONTROL: the newest link works. Without it, "the old one failed" could
	// simply mean the fixture never produced a spendable link at all.
	if _, err := consumeReset(t, d, tenantID, secondHash, otherDigest); err != nil {
		t.Fatalf("the newest link was not spendable: %v -- the refusal above is then vacuous", err)
	}
	if got := storedDigest(t, d, tenantID, admin); got != otherDigest {
		t.Fatalf("password_hash = %q, want the new digest", got)
	}
}

// TestCreatePasswordReset_RefusesInactiveAdmins covers both halves of 00009's measured
// lesson about data-modifying CTEs: the INSERT must not happen, AND the retirement in
// the CTE must not happen either. The second half is the one that runs unconditionally
// unless an EXISTS guard stops it, and it is the destructive one -- a request naming a
// disabled or foreign administrator would otherwise silently kill live links.
func TestCreatePasswordReset_RefusesInactiveAdmins(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	tenantA, _ := newTenant(t, d)
	tenantB, _ := newTenant(t, d)

	disabled := newAdmin(t, d, tenantA, randAdminEmail(t), "disabled", "manager")
	foreign := newAdmin(t, d, tenantB, randAdminEmail(t), "active", "owner")
	healthy := newAdmin(t, d, tenantA, randAdminEmail(t), "active", "owner")

	// A live link for the DISABLED admin, written before they were disabled in the
	// story this test tells. It is the canary for the unconditional-CTE bug.
	//
	// It has to be planted while the administrator is active, because the guard this
	// test measures would refuse to create it otherwise -- which is itself the first
	// assertion.
	active := newAdmin(t, d, tenantA, randAdminEmail(t), "active", "manager")
	canary, canaryHash := newReset(t, d, tenantA, active, time.Hour)
	if err := d.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1 AND tenant_id = $2`, active, tenantA)
		return e
	}); err != nil {
		t.Fatalf("disable admin: %v", err)
	}

	for _, c := range []struct {
		name  string
		admin uuid.UUID
	}{
		{"a disabled administrator gets no link", disabled},
		{"an administrator of another tenant gets no link", foreign},
		{"an id that is nobody gets no link", uuid.New()},
		{"an administrator disabled after issuing gets no new link", active},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := d.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).CreatePasswordReset(ctx, store.CreatePasswordResetParams{
					TenantID: tenantA, AdminUserID: c.admin,
					TokenHash: randTokenHash(t), ExpiresAt: time.Now().Add(time.Hour),
				})
				return e
			})
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("got %v, want pgx.ErrNoRows", err)
			}
		})
	}

	// 🔴 THE CANARY, AND IT MUST BE PLANTED BY A CARELESS CALLER OR IT MEASURES
	// NOTHING. This assertion was written first in the obvious shape -- call the query
	// through WithTenant, let the ErrNoRows propagate, then check the canary -- and a
	// MUTATION proved it vacuous: with the EXISTS guard deleted from the source SQL and
	// the code regenerated, the test stayed GREEN. The reason is 00009's own sentence
	// about the invite version of this bug: propagating the error makes WithTenant roll
	// the transaction back, so the CTE's cancellation is rolled back with it, and what
	// the test measured was the CALLER's discipline rather than the database's guard.
	//
	// So this arm SWALLOWS the error and COMMITS -- the `row, _ := q.Create...` a
	// careless phase-B author writes -- which is the only shape in which the guard is
	// the last line of defence.
	if err := d.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, _ = store.New(tx).CreatePasswordReset(ctx, store.CreatePasswordResetParams{
			TenantID: tenantA, AdminUserID: active,
			TokenHash: randTokenHash(t), ExpiresAt: time.Now().Add(time.Hour),
		})
		return nil // swallow: the transaction COMMITS
	}); err != nil {
		t.Fatalf("careless-caller probe: %v", err)
	}

	if live := liveResets(t, d, tenantA, active); len(live) != 1 || live[0].ID != canary.ID {
		t.Fatalf("a refused reset request retired the administrator's live link (live = %v) -- the CTE ran unconditionally", live)
	}

	// 🔴 AND THE CANARY IS STILL SPENDABLE, which is what the count above only
	// approximates. The administrator is disabled by now so the consume must refuse --
	// what it must not do is refuse because the link was RETIRED, so the distinction is
	// read off cancelled_at directly.
	if _, err := consumeReset(t, d, tenantA, canaryHash, otherDigest); err != nil {
		var cancelled *time.Time
		if e := d.WithTenant(ctx, tenantA, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT cancelled_at FROM password_resets WHERE id = $1 AND tenant_id = $2`,
				canary.ID, tenantA).Scan(&cancelled)
		}); e != nil {
			t.Fatalf("read cancelled_at: %v", e)
		}
		if cancelled != nil {
			t.Fatalf("the refused request retired the canary (cancelled_at = %v)", cancelled)
		}
	}

	// POSITIVE CONTROL: an active administrator of this tenant still gets a link, so
	// the four refusals above are not "this fixture cannot issue anything".
	if _, hash := newReset(t, d, tenantA, healthy, time.Hour); hash == "" {
		t.Fatal("POSITIVE CONTROL FAILED: an active administrator got no reset link")
	}
}

// ------------------------------------------------------- 3. the atomic spend --

// TestConsumePasswordReset_IsSingleUseUnderConcurrency is the replay proof CLAUDE.md
// section 8 requires in the shape it requires: N goroutines, one token, -race, exactly
// one winner. It is the section 4.4 pattern in another costume -- the danger here is
// not a duplicate attendance row but TWO different passwords written for one reset,
// the later one silently winning.
func TestConsumePasswordReset_IsSingleUseUnderConcurrency(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	tenantID, _ := newTenant(t, d)
	admin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	_, hash := newReset(t, d, tenantID, admin, time.Hour)

	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
		other   int
	)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := consumeReset(t, d, tenantID, hash, otherDigest)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners++
			case errors.Is(err, pgx.ErrNoRows):
			default:
				other++
			}
		}()
	}
	close(start)
	wg.Wait()

	if winners != 1 {
		t.Fatalf("%d of %d concurrent consumers succeeded, want exactly 1", winners, n)
	}
	if other != 0 {
		t.Fatalf("%d consumers failed with something other than ErrNoRows", other)
	}

	// 🔴 THE REAL ASSERTIONS END HERE, AND THAT ORDER IS DELIBERATE. The negative
	// control below is inherently timing-dependent, and an earlier version of this test
	// ended it with t.Skipf when the race did not materialise -- which would have
	// reported the WHOLE test, including the two assertions above, as SKIPPED. This
	// repository counts skips out loud (a bare `go test` silently skipping every DB
	// test is the trap the Makefile spends a page on), so a latent skip that can
	// swallow a red-line proof is not acceptable. The control now reports its outcome
	// with t.Logf and never changes this test's verdict.

	// NEGATIVE CONTROL -- the naive shape, so the test proves the STATEMENT is what
	// makes this safe rather than the database being slow enough to hide the race.
	// Read used_at, decide, then write: under READ COMMITTED every reader sees NULL.
	_, hash2 := newReset(t, d, tenantID, admin, time.Hour)
	naive := 0
	var mu2 sync.Mutex
	var wg2 sync.WaitGroup
	gate := make(chan struct{})
	for i := 0; i < n; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			<-gate
			err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
				var used *time.Time
				if e := tx.QueryRow(ctx,
					`SELECT used_at FROM password_resets WHERE token_hash = $1 AND tenant_id = $2`,
					hash2, tenantID).Scan(&used); e != nil {
					return e
				}
				if used != nil {
					return pgx.ErrNoRows
				}
				_, e := tx.Exec(ctx,
					`UPDATE password_resets SET used_at = now() WHERE token_hash = $1 AND tenant_id = $2`,
					hash2, tenantID)
				return e
			})
			if err == nil {
				mu2.Lock()
				naive++
				mu2.Unlock()
			}
		}()
	}
	close(gate)
	wg2.Wait()
	if naive < 2 {
		t.Logf("NEGATIVE CONTROL INCONCLUSIVE on this run: the naive read-then-write produced "+
			"%d winner(s) rather than several. It is a race and it does not always lose; this "+
			"says nothing about the atomic statement, whose assertions are above and passed.", naive)
	} else {
		t.Logf("negative control: the naive read-then-write produced %d winners for one token, "+
			"against exactly 1 for the atomic statement", naive)
	}
}

// TestConsumePasswordReset_RefusesEveryUnusableState pins that all five ways to be
// unusable collapse into one outcome. They MUST be indistinguishable: the page that
// consumes a reset link is reachable without credentials, so any difference the caller
// can see is a difference an attacker can see.
func TestConsumePasswordReset_RefusesEveryUnusableState(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	tenantID, _ := newTenant(t, d)

	spentAdmin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	_, spent := newReset(t, d, tenantID, spentAdmin, time.Hour)
	if _, err := consumeReset(t, d, tenantID, spent, otherDigest); err != nil {
		t.Fatalf("fixture: the first consume must succeed: %v", err)
	}

	expiredAdmin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	_, expired := newReset(t, d, tenantID, expiredAdmin, -time.Hour)

	retiredAdmin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	_, retired := newReset(t, d, tenantID, retiredAdmin, time.Hour)
	newReset(t, d, tenantID, retiredAdmin, time.Hour) // supersedes the one above

	disabledAdmin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "manager")
	_, disabledHash := newReset(t, d, tenantID, disabledAdmin, time.Hour)
	if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1 AND tenant_id = $2`, disabledAdmin, tenantID)
		return e
	}); err != nil {
		t.Fatalf("disable admin: %v", err)
	}

	for _, c := range []struct{ name, hash string }{
		{"an unknown token", randTokenHash(t)},
		{"a spent token", spent},
		{"an expired token", expired},
		{"a superseded token", retired},
		{"a token whose administrator was disabled", disabledHash},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := consumeReset(t, d, tenantID, c.hash, otherDigest); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("got %v, want pgx.ErrNoRows (every unusable state is one outcome)", err)
			}
		})
	}

	// 🔴 THE STAMP MUST NOT HAVE BEEN SPENT for the disabled administrator: without
	// the EXISTS guard the CTE burns the token while nothing is reset, and unlike an
	// invite, a burned reset link can only be replaced by the person who can no longer
	// log in.
	var used *time.Time
	if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT used_at FROM password_resets WHERE token_hash = $1 AND tenant_id = $2`,
			disabledHash, tenantID).Scan(&used)
	}); err != nil {
		t.Fatalf("read used_at: %v", err)
	}
	if used != nil {
		t.Fatalf("a refused consume BURNED the token (used_at = %v) -- the CTE ran unconditionally", used)
	}

	// POSITIVE CONTROL: a fresh link on a healthy administrator still spends.
	ok := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	_, okHash := newReset(t, d, tenantID, ok, time.Hour)
	if _, err := consumeReset(t, d, tenantID, okHash, otherDigest); err != nil {
		t.Fatalf("a usable token was refused: %v -- every refusal above is then vacuous", err)
	}
}

// TestConsumePasswordReset_RefusesADigestBcryptCannotProcess measures the interaction
// with migration 00018: the new password_hash is bounded by the DATABASE, and a caller
// that passed something else loses the WHOLE statement rather than half of it.
//
// The half-applied outcome is what would hurt: a spent token plus an unchanged
// password is an administrator who can neither log in nor reset again.
func TestConsumePasswordReset_RefusesADigestBcryptCannotProcess(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	tenantID, _ := newTenant(t, d)
	admin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	_, hash := newReset(t, d, tenantID, admin, time.Hour)

	// '' is the arm 00018 prices at 1 504 663x: bcrypt errors in nanoseconds without
	// building a key schedule, so an admin row holding it answers "is this address an
	// administrator?" about a million times faster than an unregistered one.
	if _, err := consumeReset(t, d, tenantID, hash, ""); err == nil {
		t.Fatal("an empty digest was accepted")
	} else {
		assertCheckViolation(t, err, "admin_users_password_hash_is_bcrypt")
	}

	var used *time.Time
	if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT used_at FROM password_resets WHERE token_hash = $1 AND tenant_id = $2`,
			hash, tenantID).Scan(&used)
	}); err != nil {
		t.Fatalf("read used_at: %v", err)
	}
	if used != nil {
		t.Fatal("the token was spent even though the password was not written")
	}
	if got := storedDigest(t, d, tenantID, admin); got != fixtures.UnusablePasswordHash {
		t.Fatalf("password_hash = %q, want it unchanged", got)
	}

	// POSITIVE CONTROL: the same token, with a digest 00018 accepts.
	if _, err := consumeReset(t, d, tenantID, hash, otherDigest); err != nil {
		t.Fatalf("the token was not spendable afterwards: %v", err)
	}
}

// ----------------------------------------------- 4. tenant isolation proofs --

// TestPasswordResets_CrossTenantRefusal is the isolation proof over the SURFACE M7-04
// introduces, in the two shapes CLAUDE.md section 6 insists are different jobs.
//
//	(a) THE PRODUCTION STATEMENT, which carries an explicit tenant_id predicate
//	    (section 4.5 belt) -- so this arm proves the BELT, not RLS.
//	(b) A RAW probe with NO tenant_id predicate at all -- so a 0 can only come from
//	    RLS, and the arm would go red with RLS switched off. It reads the column 00019
//	    ADDS, because a new column inherits the table's policy and "inherits" is a
//	    claim worth measuring once.
//
// Each has its own positive control in the row's OWN tenant.
func TestPasswordResets_CrossTenantRefusal(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	ctx := context.Background()

	tenantA, _ := newTenant(t, d)
	tenantB, _ := newTenant(t, d)
	adminA := newAdmin(t, d, tenantA, randAdminEmail(t), "active", "owner")
	rowA, hashA := newReset(t, d, tenantA, adminA, time.Hour)

	// (a) B knows A's token hash and cannot spend it.
	if _, err := consumeReset(t, d, tenantB, hashA, otherDigest); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("tenant B spent tenant A's reset token: %v", err)
	}
	if got := storedDigest(t, d, tenantA, adminA); got != fixtures.UnusablePasswordHash {
		t.Fatalf("A's password_hash = %q, want unchanged", got)
	}

	// (b) The RAW, UNFILTERED probe. The row is identified by its own primary key, so
	// there is no WHERE that could be responsible for a 0.
	rawCount := func(ctxTenant uuid.UUID) int {
		t.Helper()
		var n int
		if err := d.WithTenant(ctx, ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM password_resets WHERE id = $1 AND cancelled_at IS NULL`,
				rowA.ID).Scan(&n)
		}); err != nil {
			t.Fatalf("raw count: %v", err)
		}
		return n
	}
	if n := rawCount(tenantB); n != 0 {
		t.Fatalf("tenant B sees %d of tenant A's reset rows, want 0", n)
	}
	if n := rawCount(tenantA); n != 1 {
		t.Fatalf("POSITIVE CONTROL FAILED: tenant A sees %d of its own reset rows, want 1 -- the 0 above proves nothing", n)
	}

	// (b') The WRITE half: B cannot forge a row into A even naming A's admin.
	err := d.WithTenant(ctx, tenantB, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at)
			 VALUES ($1, $2, $3, now() + interval '1 hour')`,
			tenantA, adminA, randTokenHash(t))
		return e
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("cross-tenant INSERT got %v, want SQLSTATE 42501 (RLS WITH CHECK)", err)
	}
}

// ------------------------------------- 5. the sixth contained bypass surface --

// TestResolvePasswordReset_SecurityDefinerProperties measures, in the LIVE catalog and
// against live data, that the sixth context-less lookup earns ADR 0002 madde 7's five
// constraints rather than inheriting them from "there are already five" -- which the
// ADR names explicitly as not being an argument.
//
// It covers TWO different requirements, kept apart on purpose:
//
//	A. The ADR's LEAST-PRIVILEGE clause on the definer: owner tappa_resolver (not a
//	   superuser, which would make the body bypass RLS database-wide), BYPASSRLS
//	   (without it the body sees 0 rows), SECURITY DEFINER, fixed search_path.
//	B. The ADR's FIVE interface constraints, (i)..(v) as labelled below.
func TestResolvePasswordReset_SecurityDefinerProperties(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d) // (iv) is meaningless unless we are the RLS-subject role
	ctx := context.Background()

	var owner string
	var ownerSuper, ownerBypass, secDef bool
	var cfg []string
	if err := d.pool.QueryRow(ctx, `
		SELECT pg_get_userbyid(p.proowner), r.rolsuper, r.rolbypassrls, p.prosecdef, p.proconfig
		FROM pg_proc p
		JOIN pg_roles r ON r.oid = p.proowner
		WHERE p.proname = 'resolve_password_reset_by_token_hash'`,
	).Scan(&owner, &ownerSuper, &ownerBypass, &secDef, &cfg); err != nil {
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
	const wantCfg = "search_path=pg_catalog, pg_temp"
	if len(cfg) != 1 || cfg[0] != wantCfg {
		t.Errorf("proconfig = %v, want [%q] (a mutable search_path is an injection vector)", cfg, wantCfg)
	}

	// --- (iv) EXECUTE to tappa_app alone ------------------------------------
	var publicExec, appExec bool
	if err := d.pool.QueryRow(ctx, `
		SELECT has_function_privilege('public', 'resolve_password_reset_by_token_hash(text)', 'EXECUTE'),
		       has_function_privilege('tappa_app', 'resolve_password_reset_by_token_hash(text)', 'EXECUTE')`,
	).Scan(&publicExec, &appExec); err != nil {
		t.Fatalf("read function privileges: %v", err)
	}
	if publicExec {
		t.Error("PUBLIC holds EXECUTE (functions get it by default; the REVOKE must remove it)")
	}
	if !appExec {
		t.Error("tappa_app lacks EXECUTE: the reset path would be dead")
	}

	// --- (i) key-only input, (iii) fixed minimal column list ----------------
	var args, result string
	if err := d.pool.QueryRow(ctx, `
		SELECT pg_get_function_arguments(p.oid), pg_get_function_result(p.oid)
		FROM pg_proc p WHERE p.proname = 'resolve_password_reset_by_token_hash'`,
	).Scan(&args, &result); err != nil {
		t.Fatalf("read function signature: %v", err)
	}
	if args != "p_token_hash text" {
		t.Errorf("(i) arguments = %q, want exactly %q (key-only input)", args, "p_token_hash text")
	}
	const wantResult = "TABLE(id uuid, tenant_id uuid, admin_user_id uuid, expires_at timestamp with time zone, used_at timestamp with time zone, cancelled_at timestamp with time zone)"
	if result != wantResult {
		t.Errorf("(iii) result = %q, want %q (token_hash and created_at must NOT be returned)", result, wantResult)
	}

	// --- (ii) at most one row, STRUCTURALLY ---------------------------------
	// From the global UNIQUE index on token_hash (00006), not from the body.
	var uniqueIdx int
	if err := d.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = i.indkey[0]
		WHERE c.relname = 'password_resets'
		  AND i.indisunique
		  AND i.indnatts = 1
		  AND a.attname = 'token_hash'`,
	).Scan(&uniqueIdx); err != nil {
		t.Fatalf("read unique index: %v", err)
	}
	if uniqueIdx != 1 {
		t.Errorf("(ii) password_resets.token_hash single-column UNIQUE indexes = %d, want 1", uniqueIdx)
	}

	// --- (v) no naive "context is NULL -> show rows" branch ------------------
	var policy string
	if err := d.pool.QueryRow(ctx, `
		SELECT pg_get_expr(p.polqual, p.polrelid)
		FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid
		WHERE c.relname = 'password_resets'`,
	).Scan(&policy); err != nil {
		t.Fatalf("read policy: %v", err)
	}
	if !strings.Contains(policy, "NULLIF") || strings.Contains(strings.ToUpper(policy), " OR ") {
		t.Errorf("(v) policy = %q, want the standard NULLIF form with no additive OR branch", policy)
	}

	// --- the LIVE half of (iv): containment actually works -------------------
	tenantID, _ := newTenant(t, d)
	admin := newAdmin(t, d, tenantID, randAdminEmail(t), "active", "owner")
	row, hash := newReset(t, d, tenantID, admin, time.Hour)

	var direct int
	if err := d.pool.QueryRow(ctx,
		`SELECT count(*) FROM password_resets WHERE token_hash = $1`, hash).Scan(&direct); err != nil {
		t.Fatalf("context-less direct read: %v", err)
	}
	if direct != 0 {
		t.Errorf("a context-less DIRECT read returned %d rows, want 0 (FORCE RLS, fail-closed)", direct)
	}

	got, err := d.GetPasswordResetByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("context-less resolution through the function failed: %v", err)
	}
	if got.ID != row.ID || got.TenantID != tenantID || got.AdminUserID != admin {
		t.Fatalf("resolved %+v, want the row just written", got)
	}
	if got.UsedAt != nil || got.CancelledAt != nil {
		t.Fatalf("a fresh reset resolved as used=%v cancelled=%v, want both nil", got.UsedAt, got.CancelledAt)
	}

	// The resolver CARRIES state and does not FILTER on it: a spent token must still
	// resolve, or the caller cannot tell a replay from a typo (section 4.6).
	if _, err := consumeReset(t, d, tenantID, hash, otherDigest); err != nil {
		t.Fatalf("consume: %v", err)
	}
	spent, err := d.GetPasswordResetByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("a spent reset must still resolve: %v", err)
	}
	if spent.UsedAt == nil {
		t.Fatal("a spent reset resolved with used_at = nil")
	}

	// An unknown hash is pgx.ErrNoRows, not an error the caller has to special-case.
	if _, err := d.GetPasswordResetByTokenHash(ctx, randTokenHash(t)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unknown hash gave %v, want pgx.ErrNoRows", err)
	}
}
