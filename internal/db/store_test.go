package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/netip"
	"os"
	"strings"
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

// randUID returns a fresh 14-hex-char tag uid (7 bytes) in the CANONICAL UPPER
// case that migration 00013's tags_uid_canonical_hex requires. Random so repeated
// runs never collide.
//
// ToUpper is not cosmetic and this helper is why backlog T8 had 18 010 rows to
// name: hex.EncodeToString emits lower case, and until 00013 the schema accepted
// both spellings as SEPARATE primary keys with the SAME wrapped-key AAD. The
// helpers on the handler side already wrapped it in strings.ToUpper (the shape
// internal/sun.Parse produces); these two did not, so every run of this package
// planted rows the canonical constraint can no longer be validated against.
func randUID(t *testing.T) string {
	t.Helper()
	var b [7]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return strings.ToUpper(hex.EncodeToString(b[:]))
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
			tenantID, "VAT-"+tenantID.String()); e != nil {
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
//
// THE KEY REF IS 44 BYTES OF NONSENSE, NOT TWO. Migration 00021's
// tags_aes_key_ref_is_kek_envelope demands octet_length(aes_key_ref) = 44 (ADR
// 0003 article 4: nonce 12 || ciphertext 16 || tag 16), so the old two-byte
// '\xDEAD' marker is now a 23514 on INSERT. decode(repeat('dead', 22), 'hex')
// keeps the marker recognisable in a byte dump AND satisfies the shape; the bytes
// are still not a real envelope, which is fine because none of these fixtures taps
// the plaque (the ones that do use a genuine wrap from test/fixtures/tagkeys.go).
// This helper is the reason backlog T7 counted 45 728 two-byte rows: it and its
// siblings planted one per run.
func addTag(t *testing.T, d *DB, tenantID, locationID uuid.UUID, uid string, lastCtr int32) {
	t.Helper()
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), $4, 'active')`,
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
	// A POINTER since M6-06 phase B: nil is "not mounted", which is a real state
	// (00013's inventory model) and must not be able to satisfy this assertion.
	if got.LocationID == nil || *got.LocationID != locationID {
		t.Fatalf("location_id = %v, want %s", got.LocationID, locationID)
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

// TestGetLocationByIP_MatchesEverybodyWhenARangeDoes is a COUNTED LIMIT PINNED AS A
// TEST, not a guarantee. It asserts the hazard so that a future caller meets it here
// rather than in production (backlog T40, M8-04 phase B2).
//
// 🔴 WHAT IT SAYS. `@src::inet <<= ANY(static_ips)` is pure containment, so a stored
// "0.0.0.0/0" makes this query answer "yes, that address belongs to this venue" for
// EVERY address on earth — and the union spelling ("0.0.0.0/1" + "128.0.0.0/1") does
// the same across two entries. netx.TooWideForProofOfPlace refuses such a list at save
// time and tap.ipMatches refuses to read one as evidence, but neither of those runs
// inside SQL.
//
// 🔴 WHY NO PREDICATE WAS ADDED HERE, MEASURED RATHER THAN PREFERRED. The cheap SQL
// fix (`AND NOT EXISTS (SELECT 1 FROM unnest(static_ips) q WHERE masklen(q) = 0)`)
// closes the single-entry spelling and NOT the union — nor the two wider spellings
// the 5th round measured, which carry no "/0" at all (everything but 25.0.0.0/8 in
// eight entries; everything but RFC 5737's 192.0.2.0/24 in twenty-four). SQL would
// therefore carry a rule strictly weaker than the Go one, and this card exists partly
// because a second, weaker copy of exactly this predicate was what let the union
// through (tap.ipMatches had its own per-entry rule). Summing widths in SQL would be a
// second full implementation of netx.TooWideForProofOfPlace; two predicates drift.
//
// 🔴 AND THE REACH IS MEASURED: this query has NO production caller. Measured on
// this tree, the only caller of store.GetLocationByIP is this file; the tap path
// uses GetLocationForTap, which is keyed by the RESOLVED TAG and hands static_ips to
// Go, where the one predicate lives. So this is a sleeping surface, and the test
// below is the sign hung on it: wiring this query into a decision without the Go
// check would put "the source IP matches the location" into an IMMUTABLE row (§4.3)
// for a tap from anywhere.
func TestGetLocationByIP_MatchesEverybodyWhenARangeDoes(t *testing.T) {
	d := appDB(t)
	tenantID := uuid.New()
	universalID := uuid.New()

	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'store-test-universal', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		// ONE of the spellings netx.TooWideForProofOfPlace refuses — the plainest,
		// "0.0.0.0/0" — stored directly, which is what a row written before that
		// refusal existed looks like. The across-entry spellings it also refuses
		// ("0.0.0.0/1"+"128.0.0.0/1", and the 8- and 24-line complements) are driven
		// where the predicate itself is under test, in internal/netx and
		// internal/domain/tap; what THIS test pins is that the SQL alone does not care.
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips)
			 VALUES ($1, $2, 'universal', '{0.0.0.0/0}')`,
			universalID, tenantID); e != nil {
			return e
		}
		q := store.New(tx)
		for _, addr := range []string{"203.0.113.10", "10.0.0.1", "198.51.100.200"} {
			loc, e := q.GetLocationByIP(ctx, store.GetLocationByIPParams{
				TenantID: tenantID, Src: mustAddr(t, addr),
			})
			if e != nil {
				t.Fatalf("GetLocationByIP(%s): %v — the limit this test pins has CHANGED; if the "+
					"query now excludes universal ranges, say so here and check whether it also "+
					"excludes the union spelling, which netx.TooWideForProofOfPlace does", addr, e)
			}
			if loc.ID != universalID {
				t.Fatalf("GetLocationByIP(%s) returned %s, want the universal venue %s",
					addr, loc.ID, universalID)
			}
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

// uidWithALetter is randUID constrained to contain at least one hex letter, so a
// caller that lower-cases it gets a value that actually DIFFERS. randUID alone
// yields 14 upper-case hex digits and is all-numeric with probability
// (10/16)^14 = 1/721, which is rare enough to survive review and common enough to
// turn an assertion about spelling into an assertion about nothing.
//
// The retry loop terminates for the same reason the flake is rare: each draw has a
// 720/721 chance of containing a letter.
func uidWithALetter(t *testing.T) string {
	t.Helper()
	for range 64 {
		if u := randUID(t); strings.ContainsAny(u, "ABCDEF") {
			return u
		}
	}
	t.Fatal("uidWithALetter: 64 draws without a hex letter -- randUID is not random")
	return ""
}
