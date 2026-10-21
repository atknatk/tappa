package adminauth

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/db"
)

// suspension_db_test.go -- ADR 0025 §3's hot path against real Postgres: the
// suspension rides on TouchAdminSession, it is the session's OWN business's, and
// reading it never waits for the lock a suspension being written holds.
//
// FIXTURES. The two suspension columns are writable by the operator's definer and by
// the table owner only (migration 00063; tappa_app's UPDATE is three other columns),
// so a fixture suspends through the OWNER role, in a test-made tenant, and lifts the
// suspension in t.Cleanup -- 00063's Down refuses while any tenant is suspended
// (ADR 0025 K15-6). No real tenant is touched.

// suspensionOwnerPool connects as tappa_owner via DATABASE_MIGRATE_URL -- the one
// role besides the operator's definer that may write a suspension. Asserted at run
// time, so a .env pointing it at tappa_app fails loudly instead of measuring nothing.
func suspensionOwnerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("DATABASE_MIGRATE_URL")
	if raw == "" {
		if os.Getenv("DATABASE_URL") == "" {
			t.Skip("no database configured; skipping (real Postgres required)")
		}
		t.Fatal("DATABASE_URL is set but DATABASE_MIGRATE_URL is not: a suspension can only be " +
			"written as the owner, and skipping would drop ADR 0025's hot-path measurement silently")
	}
	pool, err := pgxpool.New(context.Background(), withSmallPool(raw))
	if err != nil {
		t.Fatalf("owner pool: %v", err)
	}
	t.Cleanup(pool.Close)
	var user string
	if err := pool.QueryRow(context.Background(), `SELECT current_user`).Scan(&user); err != nil || user != "tappa_owner" {
		t.Fatalf("owner pool is %q (%v), want tappa_owner", user, err)
	}
	return pool
}

// suspendFixture suspends a TEST-MADE tenant as the owner and lifts it at cleanup.
func suspendFixture(t *testing.T, owner *pgxpool.Pool, tenantID uuid.UUID, at time.Time, reason db.SuspensionReason) {
	t.Helper()
	t.Cleanup(func() { liftFixture(t, owner, tenantID) })
	tag, err := owner.Exec(context.Background(),
		`UPDATE tenants SET suspended_at = $2, suspension_reason = $3 WHERE id = $1`, tenantID, at, string(reason))
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("suspend fixture %v: %v (%d rows)", tenantID, err, tag.RowsAffected())
	}
}

func liftFixture(t *testing.T, owner *pgxpool.Pool, tenantID uuid.UUID) {
	t.Helper()
	if _, err := owner.Exec(context.Background(),
		`UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, tenantID); err != nil {
		t.Errorf("lift fixture %v: %v", tenantID, err)
	}
}

// TestVerifyDB_ReturnsTheBusinesssSuspension: a live session of a suspended business
// resolves WITH the suspension, exactly as stored; lifted, without it; and another
// business's suspension never reaches this session (the join is by the session's own
// tenant, under its own RLS context).
func TestVerifyDB_ReturnsTheBusinesssSuspension(t *testing.T) {
	d := testDB(t)
	owner := suspensionOwnerPool(t)
	m := testManager(t, d)
	ctx := context.Background()

	tenantID := newTenantRow(t, d, "Suspension Read Ltd")
	other := newTenantRow(t, d, "Another Suspended Ltd")
	email := randEmail(t)
	const pw = "the-right-password"
	adminID := newAdminRow(t, d, tenantID, email, pw, "active", "owner", "Suspension Owner")
	auth, err := m.Authenticate(ctx, email, pw)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	issued, err := m.Issue(ctx, auth.Verified()[0])
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	verify := func() Resolved {
		t.Helper()
		res, err := m.Verify(ctx, issued.Token)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if res.TenantID != tenantID || res.AdminUserID != adminID {
			t.Fatalf("Verify resolved %v/%v", res.TenantID, res.AdminUserID)
		}
		return res
	}

	if res := verify(); res.Suspended != nil {
		t.Fatalf("control: an unsuspended business resolves with %+v", *res.Suspended)
	}

	suspendFixture(t, owner, other, time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC), db.SuspensionNonPayment)
	if res := verify(); res.Suspended != nil {
		t.Fatalf("ANOTHER business's suspension reached this session: %+v", *res.Suspended)
	}

	at := time.Date(2026, 10, 8, 9, 15, 0, 0, time.UTC)
	suspendFixture(t, owner, tenantID, at, db.SuspensionTermsBreach)
	res := verify()
	if res.Suspended == nil || !res.Suspended.Since.Equal(at) || res.Suspended.Reason != string(db.SuspensionTermsBreach) {
		t.Fatalf("a suspended business resolves with %+v, want since %v, reason terms_breach", res.Suspended, at)
	}

	liftFixture(t, owner, tenantID)
	if res := verify(); res.Suspended != nil {
		t.Fatalf("a lifted suspension still resolves: %+v", *res.Suspended)
	}
}

// TestVerifyDB_DoesNotWaitForTheSuspensionsRowLock is the measurement ADR 0025 §3
// leaves to phase B ("Okuyucu askının satır kilidini beklemez; MVCC"): while another
// transaction holds the tenant row -- first as op_suspend_tenant takes it (FOR NO KEY
// UPDATE), then with the suspension's UPDATE written and not yet committed -- the
// panel's per-request statement still answers at once, with the COMMITTED state (no
// suspension yet). UPDATE ... FROM locks the target's rows, not the joined table's.
// After the holder rolls back, nothing changed.
func TestVerifyDB_DoesNotWaitForTheSuspensionsRowLock(t *testing.T) {
	d := testDB(t)
	owner := suspensionOwnerPool(t)
	m := testManager(t, d)
	ctx := context.Background()

	tenantID := newTenantRow(t, d, "Suspension Lock Ltd")
	email := randEmail(t)
	const pw = "the-right-password"
	newAdminRow(t, d, tenantID, email, pw, "active", "owner", "Lock Owner")
	auth, err := m.Authenticate(ctx, email, pw)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	issued, err := m.Issue(ctx, auth.Verified()[0])
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	for _, hold := range []struct {
		name, sql string
	}{
		{"the lock op_suspend_tenant takes first", `SELECT t.id FROM tenants AS t WHERE t.id = $1 FOR NO KEY UPDATE`},
		{"the suspension written and not committed", `UPDATE tenants SET suspended_at = clock_timestamp(), suspension_reason = 'security_hold' WHERE id = $1`},
	} {
		t.Run(hold.name, func(t *testing.T) {
			holder, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := holder.Rollback(ctx); err != nil {
					t.Errorf("rollback: %v", err)
				}
			}()
			if _, err := holder.Exec(ctx, hold.sql, tenantID); err != nil {
				t.Fatalf("hold the row: %v", err)
			}
			bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			start := time.Now()
			res, err := m.Verify(bounded, issued.Token)
			took := time.Since(start)
			if errors.Is(err, context.DeadlineExceeded) || took > 2*time.Second {
				t.Fatalf("Verify waited %v behind the held tenant row (err %v)", took, err)
			}
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if res.Suspended != nil {
				t.Fatalf("Verify read an uncommitted suspension: %+v", *res.Suspended)
			}
			t.Logf("Verify answered in %v while the tenant row was held", took)
		})
	}
}
