package adminauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// reset_db_test.go -- Resets.Issue and Resets.Consume against a REAL Postgres.
//
// 🔴 IT EXISTS BECAUSE AN AUDIT FOUND THE TWO FUNCTIONS HAD **ZERO** TESTS. The first
// round of M7-04 phase A shipped eleven tests for the reset credential and every one
// of them measured the token TYPE or Link; `grep -rn 'NewResets|\.Consume('` over the
// whole tree matched exactly one line, a nil-database refusal. Meanwhile reset.go
// carried three claims under its own 🔴 markers -- revocation shares the transaction,
// the order inside it is load-bearing, and every failure collapses into one error --
// and NOT ONE of them had ever been executed. Everything else in that change set was
// mutation-tested; these three were assertions in prose.
//
// WHAT THE FAKE COULD NOT DO, so nothing here is faked: the reset statement is a
// three-part CTE whose guards live in SQL, revocation is a second statement in the
// same transaction, and the whole point of the atomicity claim is what the DATABASE
// does when a transaction rolls back.
//
// Fixtures are NOT cleaned up, for the reason the rest of this package gives:
// tappa_app has REVOKE DELETE on the admin tables, so the impossibility of teardown IS
// the audit guarantee.
//
// NO TEST HERE PRINTS A TOKEN. Tokens exist only inside the ResetToken values Issue
// returns, which cannot render themselves (§4.7).

// cheapResets builds a Resets whose password hashing runs at bcrypt.MinCost.
//
// 🔬 MEASURED, AND IT IS THE DIFFERENCE BETWEEN A SUITE THAT RUNS AND ONE THAT TIMES
// OUT -- the same measurement fixtureDigest records for the login path: ONE cost-12
// bcrypt costs ~11 s under -race against ~0.6 s without. Consume hashes the new
// password on every call and this file calls it a dozen times.
//
// NOTHING THIS FILE MEASURES DEPENDS ON THE WORK FACTOR -- these are transaction
// boundaries, revocation counts and error mappings. The one property that DOES depend
// on it has its own test at the shipped cost: TestConsume_StoresADigestAtTheShippedCost.
func cheapResets(t *testing.T, data ResetDatabase) *Resets {
	t.Helper()
	r, err := NewResets(data, testConfig())
	if err != nil {
		t.Fatalf("NewResets: %v", err)
	}
	r.digest = func(password string) (string, error) {
		h, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		return string(h), e
	}
	return r
}

// newSessionRow issues a panel session through the real query and returns its id.
func newSessionRow(t *testing.T, d *db.DB, tenantID, adminID uuid.UUID) uuid.UUID {
	t.Helper()
	var row store.CreateAdminSessionRow
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).CreateAdminSession(ctx, store.CreateAdminSessionParams{
			TenantID: tenantID, AdminUserID: adminID, TokenHash: "reset-fixture-" + uuid.NewString(),
		})
		return e
	})
	if err != nil {
		t.Fatalf("CreateAdminSession: %v", err)
	}
	return row.ID
}

// liveSessions counts the administrator's UNREVOKED panel sessions.
func liveSessions(t *testing.T, d *db.DB, tenantID, adminID uuid.UUID) int {
	t.Helper()
	var rows []store.ListAdminSessionsForAdminRow
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).ListAdminSessionsForAdmin(ctx, store.ListAdminSessionsForAdminParams{
			TenantID: tenantID, AdminUserID: adminID,
		})
		return e
	})
	if err != nil {
		t.Fatalf("ListAdminSessionsForAdmin: %v", err)
	}
	n := 0
	for _, r := range rows {
		if r.RevokedAt == nil {
			n++
		}
	}
	return n
}

// readDigest reads an administrator's stored password_hash back out of the database.
func readDigest(t *testing.T, d *db.DB, tenantID, adminID uuid.UUID) string {
	t.Helper()
	var got string
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT password_hash FROM admin_users WHERE id = $1 AND tenant_id = $2`,
			adminID, tenantID).Scan(&got)
	})
	if err != nil {
		t.Fatalf("read password_hash: %v", err)
	}
	return got
}

// tokenIsSpent reports whether the reset row behind an issued token has been consumed.
// It resolves through the production context-less lookup, so it also exercises the
// path that must keep working after a failed consume.
func tokenIsSpent(t *testing.T, r *Resets, d *db.DB, tok ResetToken) bool {
	t.Helper()
	h, err := tok.hash(r.hmacKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	row, err := d.GetPasswordResetByTokenHash(context.Background(), h)
	if err != nil {
		t.Fatalf("GetPasswordResetByTokenHash: %v", err)
	}
	return row.UsedAt != nil
}

// ------------------------------------------------------------ the happy path --

// TestResets_IssueAndConsume_EndToEnd is the walk the first round never took: mint a
// link, spend it, and read every consequence back OUT OF THE DATABASE rather than out
// of the returned struct.
func TestResets_IssueAndConsume_EndToEnd(t *testing.T) {
	d := testDB(t)
	r := cheapResets(t, d)
	ctx := context.Background()

	tenantID := newTenantRow(t, d, "Reset E2E Ltd")
	admin := newAdminRow(t, d, tenantID, randEmail(t), "the-old-password", "active", "owner", "Reset Owner")
	before := readDigest(t, d, tenantID, admin)

	// Three live panel sessions: a laptop, a phone and the one an attacker is holding.
	newSessionRow(t, d, tenantID, admin)
	newSessionRow(t, d, tenantID, admin)
	newSessionRow(t, d, tenantID, admin)
	if n := liveSessions(t, d, tenantID, admin); n != 3 {
		t.Fatalf("fixture: %d live sessions, want 3", n)
	}

	issued, err := r.Issue(ctx, tenantID, admin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if issued.Reset.AdminUserID != admin || issued.Reset.TenantID != tenantID {
		t.Fatalf("Issue returned %+v, want the admin and tenant asked for", issued.Reset)
	}
	if issued.Reset.RetiredCount != 0 {
		t.Errorf("the first link retired %d others, want 0", issued.Reset.RetiredCount)
	}
	// 🔴 ISSUING MUST NOT TOUCH THE PASSWORD. A flow that disabled the old credential
	// on REQUEST would let anybody lock any administrator out by typing their address
	// into a public form -- reset.go argues this and nothing measured it.
	if got := readDigest(t, d, tenantID, admin); got != before {
		t.Fatal("issuing a reset link changed the password")
	}
	if n := liveSessions(t, d, tenantID, admin); n != 3 {
		t.Fatalf("issuing a reset link revoked sessions: %d live, want 3", n)
	}

	consumed, resolved, err := r.Consume(ctx, issued.Token, "the-new-password")
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if consumed.AdminUserID != admin || consumed.TenantID != tenantID || consumed.ResetID != issued.Reset.ID {
		t.Fatalf("Consume returned %+v, want the identity the link was issued for", consumed)
	}
	if resolved.ID != issued.Reset.ID {
		t.Fatalf("resolved %+v, want the row the link names", resolved)
	}
	// 🔴 THE REVOCATION COUNT IS THE ASSERTION, not that revocation "happened": a
	// revoke keyed on the wrong admin, or one that silently matched nothing, returns 0
	// and would have passed a boolean check.
	if consumed.RevokedSessions != 3 {
		t.Fatalf("RevokedSessions = %d, want 3", consumed.RevokedSessions)
	}
	if n := liveSessions(t, d, tenantID, admin); n != 0 {
		t.Fatalf("%d sessions survived the reset, want 0", n)
	}
	if got := readDigest(t, d, tenantID, admin); got == before {
		t.Fatal("the password was not changed")
	} else if bcrypt.CompareHashAndPassword([]byte(got), []byte("the-new-password")) != nil {
		t.Fatal("the stored digest does not verify the new password")
	}
	if !tokenIsSpent(t, r, d, issued.Token) {
		t.Fatal("the token was not marked spent")
	}
}

// TestConsume_StoresADigestAtTheShippedCost is the ONE test in this file that pays the
// real work factor, and it is here because cheapResets deliberately does not.
//
// It mirrors internal/domain/signup's assertion for the other write path: read the
// digest back OUT of the column and check its cost prefix, so a future rewiring that
// stores something cheaper fails here rather than in production. Migration 00018's
// CHECK bounds the column to costs 04..14; this pins that the RESET writes 12.
func TestConsume_StoresADigestAtTheShippedCost(t *testing.T) {
	d := testDB(t)
	r, err := NewResets(d, testConfig()) // NOT cheapResets: the subject is the cost
	if err != nil {
		t.Fatalf("NewResets: %v", err)
	}
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Reset Cost Ltd")
	admin := newAdminRow(t, d, tenantID, randEmail(t), "old", "active", "owner", "Cost Owner")

	issued, err := r.Issue(ctx, tenantID, admin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, _, err := r.Consume(ctx, issued.Token, "a-brand-new-password"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	got := readDigest(t, d, tenantID, admin)
	if want := "$2a$12$"; !strings.HasPrefix(got, want) {
		t.Fatalf("stored digest prefix = %.7q, want %q (adminauth.Cost)", got, want)
	}
}

// ---------------------------------------------------------------- refusals ----

// TestIssue_RefusesAnythingButAnActiveAdminOfThatTenant pins the pgx.ErrNoRows ->
// ErrNoSuchAdmin mapping, which nothing measured.
func TestIssue_RefusesAnythingButAnActiveAdminOfThatTenant(t *testing.T) {
	d := testDB(t)
	r := cheapResets(t, d)
	ctx := context.Background()

	tenantA := newTenantRow(t, d, "Reset Refusal A Ltd")
	tenantB := newTenantRow(t, d, "Reset Refusal B Ltd")
	disabled := newAdminRow(t, d, tenantA, randEmail(t), "p", "disabled", "manager", "Disabled")
	foreign := newAdminRow(t, d, tenantB, randEmail(t), "p", "active", "owner", "Foreign")
	healthy := newAdminRow(t, d, tenantA, randEmail(t), "p", "active", "owner", "Healthy")

	for _, c := range []struct {
		name           string
		tenant, admin  uuid.UUID
		wantSentinel   error
		wantNotWrapped bool
	}{
		{name: "a disabled administrator", tenant: tenantA, admin: disabled, wantSentinel: ErrNoSuchAdmin},
		{name: "an administrator of another tenant", tenant: tenantA, admin: foreign, wantSentinel: ErrNoSuchAdmin},
		{name: "an id that is nobody", tenant: tenantA, admin: uuid.New(), wantSentinel: ErrNoSuchAdmin},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.Issue(ctx, c.tenant, c.admin)
			if !errors.Is(err, c.wantSentinel) {
				t.Fatalf("err = %v, want %v", err, c.wantSentinel)
			}
			// The sentinel must not carry the driver's error forward: a caller that
			// errors.Is(err, pgx.ErrNoRows) would then branch on a database detail.
			if errors.Is(err, pgx.ErrNoRows) {
				t.Fatal("ErrNoSuchAdmin still wraps pgx.ErrNoRows")
			}
		})
	}
	t.Run("nil identifiers are refused before any query", func(t *testing.T) {
		if _, err := r.Issue(ctx, uuid.Nil, healthy); err == nil {
			t.Fatal("a nil tenant was accepted")
		}
		if _, err := r.Issue(ctx, tenantA, uuid.Nil); err == nil {
			t.Fatal("a nil admin was accepted")
		}
	})
	// POSITIVE CONTROL: without it every refusal above could be "this fixture cannot
	// issue anything".
	t.Run("control: an active administrator of this tenant gets a link", func(t *testing.T) {
		if _, err := r.Issue(ctx, tenantA, healthy); err != nil {
			t.Fatalf("Issue: %v -- every refusal above is then vacuous", err)
		}
	})
}

// TestConsume_EveryFailureIsOneError walks all six ways a reset can fail to apply and
// asserts they are INDISTINGUISHABLE to the caller. The page that consumes a reset link
// is reachable without credentials, so a difference the caller can see is a difference
// an attacker can see.
//
// It also pins the half that makes §4.6 possible: the resolved facts come back
// ALONGSIDE the error whenever the token resolved at all, so phase B can write an
// attributable audit row while showing one sentence.
func TestConsume_EveryFailureIsOneError(t *testing.T) {
	d := testDB(t)
	r := cheapResets(t, d)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Reset Failure Ltd")

	spentAdmin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "Spent")
	spent, err := r.Issue(ctx, tenantID, spentAdmin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, _, err := r.Consume(ctx, spent.Token, "first-new-password"); err != nil {
		t.Fatalf("fixture: the first consume must succeed: %v", err)
	}

	// An already-expired link. The ttl field exists for exactly this, so the test does
	// not sleep.
	expiredAdmin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "Expired")
	expiredResets := cheapResets(t, d)
	expiredResets.ttl = -time.Hour
	expired, err := expiredResets.Issue(ctx, tenantID, expiredAdmin)
	if err != nil {
		t.Fatalf("Issue(expired): %v", err)
	}

	retiredAdmin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "Retired")
	retired, err := r.Issue(ctx, tenantID, retiredAdmin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := r.Issue(ctx, tenantID, retiredAdmin); err != nil { // supersedes it
		t.Fatalf("Issue(second): %v", err)
	}

	disabledAdmin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "manager", "ToDisable")
	disabledLink, err := r.Issue(ctx, tenantID, disabledAdmin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1 AND tenant_id = $2`,
			disabledAdmin, tenantID)
		return e
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	unknown, err := newResetToken()
	if err != nil {
		t.Fatalf("newResetToken: %v", err)
	}

	for _, c := range []struct {
		name string
		tok  ResetToken
		// resolves is whether the token names a real row, i.e. whether phase B can
		// attribute the attempt to a tenant.
		resolves bool
	}{
		{"a malformed value", ParseResetToken("not-a-token"), false},
		{"the empty value", ResetToken{}, false},
		{"an unknown token", unknown, false},
		{"a spent token", spent.Token, true},
		{"an expired token", expired.Token, true},
		{"a superseded token", retired.Token, true},
		{"a token whose administrator was disabled", disabledLink.Token, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, resolved, err := r.Consume(ctx, c.tok, "a-replacement-password")
			if !errors.Is(err, ErrResetUnusable) {
				t.Fatalf("err = %v, want ErrResetUnusable", err)
			}
			if c.resolves && resolved.ID == uuid.Nil {
				t.Fatal("the resolved facts were dropped: phase B cannot attribute this attempt (§4.6)")
			}
			if !c.resolves && resolved.ID != uuid.Nil {
				t.Fatalf("a token that names nothing resolved to %+v", resolved)
			}
		})
	}

	// 🔴 THE DISABLED ADMINISTRATOR'S LINK MUST NOT HAVE BEEN BURNED. Unlike an invite,
	// a burned reset link can only be replaced by the person who can no longer log in.
	if tokenIsSpent(t, r, d, disabledLink.Token) {
		t.Fatal("a refused consume spent the token")
	}

	// POSITIVE CONTROL.
	okAdmin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "Fine")
	ok, err := r.Issue(ctx, tenantID, okAdmin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, _, err := r.Consume(ctx, ok.Token, "a-good-new-password"); err != nil {
		t.Fatalf("a usable link was refused: %v -- every refusal above is then vacuous", err)
	}
}

// ------------------------------------------- the two claims nothing had run ---

// TestConsume_ReplayIsNotAFreeSignOutButton is reset.go's second 🔴 claim, executed.
//
// THE ATTACK IT PRICES: somebody who once saw a reset link -- a forwarded mail, a
// shoulder-surfed screen -- holds a value that is useless for changing a password after
// the first use. If replaying it still ran the revocation, they would hold a permanent
// "sign this administrator out of everything" button: not a takeover, but a denial of
// service against one named person, repeatable and free.
func TestConsume_ReplayIsNotAFreeSignOutButton(t *testing.T) {
	d := testDB(t)
	r := cheapResets(t, d)
	ctx := context.Background()

	tenantID := newTenantRow(t, d, "Reset Replay Ltd")
	admin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "Replay Owner")
	newSessionRow(t, d, tenantID, admin)

	issued, err := r.Issue(ctx, tenantID, admin)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	consumed, _, err := r.Consume(ctx, issued.Token, "the-new-password")
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if consumed.RevokedSessions != 1 {
		t.Fatalf("RevokedSessions = %d, want 1", consumed.RevokedSessions)
	}

	// The owner signs back in on two devices AFTER the reset.
	newSessionRow(t, d, tenantID, admin)
	newSessionRow(t, d, tenantID, admin)
	if n := liveSessions(t, d, tenantID, admin); n != 2 {
		t.Fatalf("fixture: %d live sessions after re-login, want 2", n)
	}

	// The replay: same link, three times.
	for i := 0; i < 3; i++ {
		if _, _, err := r.Consume(ctx, issued.Token, "an-attacker-chosen-password"); !errors.Is(err, ErrResetUnusable) {
			t.Fatalf("replay %d: err = %v, want ErrResetUnusable", i, err)
		}
	}
	if n := liveSessions(t, d, tenantID, admin); n != 2 {
		t.Fatalf("a replayed link revoked %d of the 2 live sessions -- it IS a free sign-out button", 2-n)
	}
	if got := readDigest(t, d, tenantID, admin); bcrypt.CompareHashAndPassword([]byte(got), []byte("the-new-password")) != nil {
		t.Fatal("a replayed link changed the password")
	}
}

// txCountingDB wraps a real *db.DB, counts WithTenant calls and can force the FIRST
// transaction to roll back after its body succeeded.
//
// THE ROLLBACK IS THE ONLY WAY TO OBSERVE THE ATOMICITY CLAIM FROM OUTSIDE. There is no
// call through which the revocation can be made to fail on its own, so the claim is
// measured from the other direction: when the transaction does not commit, NOTHING may
// persist -- neither the password, nor the revocation, nor the spent stamp.
type txCountingDB struct {
	inner        *db.DB
	calls        int
	rollbackOnce bool
	fired        bool
}

var errForcedRollback = errors.New("adminauth_test: forced rollback")

func (x *txCountingDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	x.calls++
	if x.rollbackOnce && !x.fired {
		x.fired = true
		return x.inner.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if e := fn(ctx, tx); e != nil {
				return e
			}
			return errForcedRollback
		})
	}
	return x.inner.WithTenant(ctx, tenantID, fn)
}

func (x *txCountingDB) GetPasswordResetByTokenHash(ctx context.Context, h string) (db.ResolvedPasswordReset, error) {
	return x.inner.GetPasswordResetByTokenHash(ctx, h)
}

// TestConsume_BothWritesShareOneTransaction is reset.go's first 🔴 claim -- "a reset
// that could not revoke does not happen at all" -- executed, in the two ways it can be
// observed from outside the package.
//
//	(a) STRUCTURAL: a successful Consume opens EXACTLY ONE transaction. Any split into
//	    two, in either order, moves this number and is caught here whether or not the
//	    split happens to be observable in the data.
//	(b) BEHAVIOURAL: when that transaction does not commit, nothing persists. A
//	    revocation that had escaped into a second transaction would survive the first
//	    one's rollback and leave an administrator signed out of a password change that
//	    never happened.
func TestConsume_BothWritesShareOneTransaction(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Reset Atomicity Ltd")

	t.Run("a successful consume opens exactly one transaction", func(t *testing.T) {
		admin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "One Tx")
		newSessionRow(t, d, tenantID, admin)
		counter := &txCountingDB{inner: d}
		r := cheapResets(t, counter)

		issued, err := r.Issue(ctx, tenantID, admin)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		if counter.calls != 1 {
			t.Fatalf("Issue opened %d transactions, want 1", counter.calls)
		}
		counter.calls = 0
		if _, _, err := r.Consume(ctx, issued.Token, "the-new-password"); err != nil {
			t.Fatalf("Consume: %v", err)
		}
		if counter.calls != 1 {
			t.Fatalf("Consume opened %d transactions, want 1 -- the password write and the "+
				"revocation must share one", counter.calls)
		}
	})

	t.Run("when the transaction rolls back, nothing persists", func(t *testing.T) {
		admin := newAdminRow(t, d, tenantID, randEmail(t), "p", "active", "owner", "Rollback")
		before := readDigest(t, d, tenantID, admin)
		newSessionRow(t, d, tenantID, admin)
		newSessionRow(t, d, tenantID, admin)

		issuer := cheapResets(t, d) // issue on the real DB so the link really exists
		issued, err := issuer.Issue(ctx, tenantID, admin)
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}

		counter := &txCountingDB{inner: d, rollbackOnce: true}
		r := cheapResets(t, counter)
		if _, _, err := r.Consume(ctx, issued.Token, "a-password-that-must-not-land"); err == nil {
			t.Fatal("Consume reported success on a transaction that rolled back")
		} else if !errors.Is(err, errForcedRollback) {
			t.Fatalf("err = %v, want it to carry the rollback cause", err)
		}

		if got := readDigest(t, d, tenantID, admin); got != before {
			t.Fatal("the password survived a rolled-back transaction")
		}
		if n := liveSessions(t, d, tenantID, admin); n != 2 {
			t.Fatalf("%d of 2 sessions survived, want 2 -- the revocation escaped the transaction "+
				"and signed somebody out of a reset that never happened", n)
		}
		if tokenIsSpent(t, r, d, issued.Token) {
			t.Fatal("the token was spent by a transaction that rolled back")
		}

		// POSITIVE CONTROL: the same link, on an unwrapped Resets, still works -- so the
		// three assertions above are about the rollback and not about a dead fixture.
		if _, _, err := issuer.Consume(ctx, issued.Token, "the-real-new-password"); err != nil {
			t.Fatalf("the link was unusable afterwards: %v", err)
		}
		if n := liveSessions(t, d, tenantID, admin); n != 0 {
			t.Fatalf("%d sessions survived the real reset, want 0", n)
		}
	})
}
