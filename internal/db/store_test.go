package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// mustAddr parses a literal IP into a netip.Addr (the type GetLocationByIP takes).
func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("parse addr %q: %v", s, err)
	}
	return a
}

// These tests run against a REAL Postgres (CLAUDE.md section 8 / Q04): the
// SECURITY DEFINER resolve path and the atomic counter advance cannot be proven
// against a fake DB. They connect as tappa_app via DATABASE_URL, so RLS is in
// force for every write they make.
//
// Fixtures are NOT cleaned up. Deleting them would require the owner/migration
// role (tappa_app has REVOKE DELETE on tags/sessions/employees, and tenants has
// ON DELETE RESTRICT), and reaching for that role in app/test code is exactly
// what redline R5 forbids. Instead every fixture uses a fresh random UUID (and a
// random tag uid), so repeated runs never collide and the rows stay inert; a
// periodic `make db-reset` clears the dev DB.

// appDB returns an UNPINNED tappa_app pool (unlike tenant_test's testDB which
// pins to one connection). The atomicity test needs several connections so two
// goroutines can hold two concurrent transactions.
func appDB(t *testing.T) *DB {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping store DB tests (real Postgres required)")
	}
	d, err := New(context.Background(), &config.Config{DatabaseURL: raw})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// randUID returns a fresh 14-hex-char tag uid (7 bytes), matching tags.uid's
// CHECK (^[0-9A-Fa-f]{14}$). Random so repeated runs never collide.
func randUID(t *testing.T) string {
	t.Helper()
	var b [7]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b[:])
}

// newTenant creates a committed tenant + one location as tappa_app (inside its
// own tenant context). The location carries static_ips '{203.0.113.0/24}' so the
// GetLocationByIP test has a range to match against.
func newTenant(t *testing.T, d *DB) (tenantID, locationID uuid.UUID) {
	t.Helper()
	tenantID = uuid.New()
	locationID = uuid.New()

	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'store-test', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()[:8]); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng)
			 VALUES ($1, $2, 'loc', '{203.0.113.0/24}', 35.899, 14.514)`,
			locationID, tenantID)
		return e
	})
	if err != nil {
		t.Fatalf("newTenant: %v", err)
	}
	return tenantID, locationID
}

// addTag inserts a tag with the given starting counter, as tappa_app in context.
func addTag(t *testing.T, d *DB, tenantID, locationID uuid.UUID, uid string, lastCtr int32) {
	t.Helper()
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, '\xDEAD', $4, 'active')`,
			uid, tenantID, locationID, lastCtr)
		return e
	})
	if err != nil {
		t.Fatalf("addTag: %v", err)
	}
}

// TestGetTagByUID_ResolvesContextLess: the hand-written resolver reads a tag
// WITHOUT a tenant context (context is the RESULT) and returns the right tenant,
// location and status. Proven by calling it on the bare pool (no WithTenant).
func TestGetTagByUID_ResolvesContextLess(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	uid := randUID(t)
	addTag(t, d, tenantID, locationID, uid, 3)

	got, err := d.GetTagByUID(context.Background(), uid)
	if err != nil {
		t.Fatalf("GetTagByUID: %v", err)
	}
	if got.TenantID != tenantID {
		t.Fatalf("tenant_id = %s, want %s (resolution must recover the tenant)", got.TenantID, tenantID)
	}
	if got.LocationID != locationID {
		t.Fatalf("location_id = %s, want %s", got.LocationID, locationID)
	}
	if got.UID != uid || got.Status != "active" || got.LastCtr != 3 {
		t.Fatalf("unexpected tag: %+v", got)
	}
	if len(got.AESKeyRef) == 0 {
		t.Fatal("aes_key_ref empty: wrapped key must be returned for SUN verification")
	}
}

// TestGetTagByUID_UnknownReturnsNoRows: an unknown uid yields pgx.ErrNoRows so
// the caller can reject the tap (never a silent empty struct).
func TestGetTagByUID_UnknownReturnsNoRows(t *testing.T) {
	d := appDB(t)
	_, err := d.GetTagByUID(context.Background(), randUID(t))
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetTagByUID(unknown) error = %v, want pgx.ErrNoRows", err)
	}
}

// TestGetEmployeeBySessionHash_ResolvesContextLess: the session resolver recovers
// the tenant and employee from just the token hash, context-less.
func TestGetEmployeeBySessionHash_ResolvesContextLess(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	employeeID := uuid.New()
	tokenHash := "hash-" + uuid.NewString()

	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, 'Emp', 'active')`,
			employeeID, tenantID, locationID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO sessions (tenant_id, employee_id, token_hash)
			 VALUES ($1, $2, $3)`,
			tenantID, employeeID, tokenHash)
		return e
	})
	if err != nil {
		t.Fatalf("insert employee/session: %v", err)
	}

	got, err := d.GetEmployeeBySessionHash(context.Background(), tokenHash)
	if err != nil {
		t.Fatalf("GetEmployeeBySessionHash: %v", err)
	}
	if got.TenantID != tenantID || got.EmployeeID != employeeID {
		t.Fatalf("resolved %+v, want tenant %s employee %s", got, tenantID, employeeID)
	}
	if got.RevokedAt != nil {
		t.Fatalf("revoked_at = %v, want nil (session not revoked)", got.RevokedAt)
	}
}

// TestGetLocationByIP_CidrMatch exercises the generated store query and the
// cidr[] path: an IP inside a configured /24 matches; one outside every range
// returns pgx.ErrNoRows. Runs tenant-scoped through WithTenant.
func TestGetLocationByIP_CidrMatch(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)

	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// Inside 203.0.113.0/24 -> match.
		loc, e := q.GetLocationByIP(ctx, store.GetLocationByIPParams{
			TenantID: tenantID, Src: mustAddr(t, "203.0.113.10"),
		})
		if e != nil {
			return e
		}
		if loc.ID != locationID {
			t.Errorf("matched location %s, want %s", loc.ID, locationID)
		}
		// Outside every range -> no rows.
		_, e = q.GetLocationByIP(ctx, store.GetLocationByIPParams{
			TenantID: tenantID, Src: mustAddr(t, "10.0.0.1"),
		})
		if !errors.Is(e, pgx.ErrNoRows) {
			t.Errorf("non-matching IP error = %v, want pgx.ErrNoRows", e)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTenant: %v", err)
	}
}

// TestAdvanceTagCounter_SuccessReplayAndConcurrency proves the replay guard
// (CLAUDE.md section 4.4) through the generated store:
//   - a strictly greater ctr advances and returns the correct gap;
//   - a replayed (equal) ctr returns pgx.ErrNoRows (the '<' comparison, not '>=');
//   - two concurrent taps with the SAME ctr yield EXACTLY one winner.
//
// The full N-goroutine -race proof belongs to M2-06; this is the basic probe.
func TestAdvanceTagCounter_SuccessReplayAndConcurrency(t *testing.T) {
	d := appDB(t)
	tenantID, locationID := newTenant(t, d)
	uid := randUID(t)
	addTag(t, d, tenantID, locationID, uid, 0)

	// Success: ctr 5 over last_ctr 0 -> gap 4 (skipped 1..4).
	var row store.AdvanceTagCounterRow
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{
			Ctr: 5, TenantID: tenantID, Uid: uid,
		})
		return e
	}); err != nil {
		t.Fatalf("advance ctr=5: %v", err)
	}
	if row.Uid != uid || row.CtrGap != 4 {
		t.Fatalf("advance ctr=5 -> %+v, want uid=%s gap=4", row, uid)
	}

	// Replay: ctr 5 again (last_ctr now 5) -> 0 rows -> pgx.ErrNoRows.
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{
			Ctr: 5, TenantID: tenantID, Uid: uid,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("replay ctr=5 error = %v, want pgx.ErrNoRows", err)
	}

	// Concurrency: two goroutines, same ctr 9 (last_ctr is 5) -> exactly one wins.
	const ctr = 9
	var wins, noRows int64
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, err := store.New(tx).AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{
					Ctr: ctr, TenantID: tenantID, Uid: uid,
				})
				return err
			})
			switch {
			case e == nil:
				atomic.AddInt64(&wins, 1)
			case errors.Is(e, pgx.ErrNoRows):
				atomic.AddInt64(&noRows, 1)
			default:
				t.Errorf("unexpected concurrent error: %v", e)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || noRows != 1 {
		t.Fatalf("concurrent same-ctr: wins=%d noRows=%d, want exactly 1 and 1", wins, noRows)
	}

	// Final state: last_ctr settled at 9 (read context-less via the resolver).
	got, err := d.GetTagByUID(context.Background(), uid)
	if err != nil {
		t.Fatalf("read final tag: %v", err)
	}
	if got.LastCtr != ctr {
		t.Fatalf("final last_ctr = %d, want %d", got.LastCtr, ctr)
	}
}
