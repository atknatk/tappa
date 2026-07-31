package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// locations_test.go -- the proofs for locations.wifi_ssid (migration 00010), the
// network NAME the M5-02 activation page shows. These run against a REAL Postgres
// as tappa_app (CLAUDE.md section 8 / Q04), because every claim below is about
// the DATABASE: a CHECK, a row-level policy and a table-level privilege cannot be
// demonstrated against a fake.
//
// WHAT IS PROVEN HERE, and why each one needs proving:
//   - The 32-limit is measured in OCTETS, not characters. That is the whole point
//     of octet_length() in 00010 and it is invisible until a multi-byte name is
//     tried: 32 two-byte letters are 64 bytes and MUST be refused, while 16 of
//     them are exactly 32 bytes and must pass. A char_length() constraint would
//     accept the first and this file would go red.
//   - The empty string is refused, so "no network" has exactly ONE spelling
//     (NULL). Both the accepted-NULL and the rejected-'' case are asserted; one
//     without the other proves nothing.
//   - The CHECK bites on UPDATE too, not only on INSERT.
//   - The column did NOT need a new grant -- verified through the catalog AND by
//     really reading and writing it as tappa_app (M1-04 lesson: "it is in the
//     GRANT" and "the privilege exists" are different sentences, in both
//     directions).
//   - The column is inside the tenant boundary: the RLS probe carries NO
//     tenant_id predicate (section 6 -- a filtered probe would return 0 rows
//     because of the WHERE and would stay green with RLS off) and every negative
//     assertion is paired with a positive control in the owning tenant.
//
// Fixtures are not cleaned up, for the reason store_test.go states: deleting them
// would need the migration role, which redline R5 forbids outside migrations. Every
// row uses a fresh random UUID, so repeated runs never collide.

// Multi-byte runes with a purpose, spelled by code point so this file stays pure
// ASCII on disk and so the byte count is stated rather than eyeballed. U+0127 /
// U+0126 is a real Maltese letter (the barred h in the Maltese spelling of
// "Hamrun"), which makes a Maltese venue name the realistic way to reach this
// boundary; U+1D11E is here only because it is four bytes and nothing else in the
// test would exercise a 4-byte sequence.
const (
	hBarLower = string(rune(0x0127)) // 2 bytes in UTF-8
	hBarUpper = string(rune(0x0126)) // 2 bytes in UTF-8
	fourByte  = string(rune(0x1D11E))
)

func strPtr(s string) *string { return &s }

// insertLocationSSID inserts a fresh location carrying the given wifi_ssid (nil =
// SQL NULL) inside the tenant's own context, and returns the raw error so the
// caller can classify it. WithTenant rolls back on error, so a refused row leaves
// nothing behind.
func insertLocationSSID(t *testing.T, d *DB, tenantID, locationID uuid.UUID, ssid *string) error {
	t.Helper()
	return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, wifi_ssid)
			 VALUES ($1, $2, 'wifi-probe', $3)`,
			locationID, tenantID, ssid)
		return e
	})
}

// readSSID returns the stored value and its length in BOTH units, so a test can
// say which unit the constraint actually counted.
func readSSID(t *testing.T, d *DB, tenantID, locationID uuid.UUID) (value *string, octets, chars *int) {
	t.Helper()
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT wifi_ssid, octet_length(wifi_ssid), char_length(wifi_ssid)
			 FROM locations WHERE id = $1 AND tenant_id = $2`,
			locationID, tenantID).Scan(&value, &octets, &chars)
	}); err != nil {
		t.Fatalf("readSSID: %v", err)
	}
	return value, octets, chars
}

// TestLocations_WiFiSSIDCheck_CountsOctetsNotCharacters exercises every boundary
// of locations_wifi_ssid_len (00010). The multi-byte rows are the ones that
// distinguish an octet limit from a character limit; the ASCII rows pin the
// boundary itself; NULL and ” pin the "no network" representation.
func TestLocations_WiFiSSIDCheck_CountsOctetsNotCharacters(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	tenantID, _ := newTenant(t, d)

	cases := []struct {
		name       string
		ssid       *string
		wantOK     bool
		wantOctets int // only read when wantOK
		wantChars  int
	}{
		{"null is accepted -- the single spelling of 'no network'", nil, true, 0, 0},
		{"empty string is refused -- it would be a second spelling", strPtr(""), false, 0, 0},
		{"one octet", strPtr("a"), true, 1, 1},
		// A single space is legal in an 802.11 SSID, so the database has no basis
		// to refuse it. Named as a LIMIT, not an oversight: trimming belongs to the
		// phase-B form.
		{"a lone space is accepted (documented limit)", strPtr(" "), true, 1, 1},
		{"32 ascii octets -- the boundary", strPtr(strings.Repeat("a", 32)), true, 32, 32},
		{"33 ascii octets -- one past it", strPtr(strings.Repeat("a", 33)), false, 0, 0},
		// 16 x 2 bytes = exactly 32 octets: accepted, and only 16 CHARACTERS.
		{"16 two-byte runes = 32 octets", strPtr(strings.Repeat(hBarLower, 16)), true, 32, 16},
		{"17 two-byte runes = 34 octets", strPtr(strings.Repeat(hBarLower, 17)), false, 0, 0},
		// THE case that separates the two units: 32 characters, 64 bytes. A
		// char_length() constraint would have let this through.
		{"32 two-byte CHARACTERS = 64 octets", strPtr(strings.Repeat(hBarLower, 32)), false, 0, 0},
		{"8 four-byte runes = 32 octets", strPtr(strings.Repeat(fourByte, 8)), true, 32, 8},
		{"9 four-byte runes = 36 octets", strPtr(strings.Repeat(fourByte, 9)), false, 0, 0},
		// The realistic Maltese spelling of the Hamrun venue: 15 characters, 16 octets.
		{"KF-<H-with-stroke>amrun-Staff (the real Maltese spelling of the Hamrun venue)", strPtr("KF-" + hBarUpper + "amrun-Staff"), true, 16, 15},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			locationID := uuid.New()
			err := insertLocationSSID(t, d, tenantID, locationID, tc.ssid)

			if !tc.wantOK {
				pg := asPgErr(err)
				if pg == nil {
					t.Fatalf("insert accepted a value the CHECK must refuse (err = %v)", err)
				}
				if pg.Code != "23514" {
					t.Fatalf("SQLSTATE = %s, want 23514 (check_violation)", pg.Code)
				}
				if pg.ConstraintName != "locations_wifi_ssid_len" {
					t.Fatalf("constraint = %q, want locations_wifi_ssid_len (a DIFFERENT constraint refusing the row would make this test lie)", pg.ConstraintName)
				}
				return
			}

			if err != nil {
				t.Fatalf("insert refused a legal value: %v", err)
			}
			got, octets, chars := readSSID(t, d, tenantID, locationID)
			if tc.ssid == nil {
				if got != nil {
					t.Fatalf("stored %q, want SQL NULL", *got)
				}
				return
			}
			if got == nil || *got != *tc.ssid {
				t.Fatalf("stored %v, want %q (the value must round-trip byte for byte)", got, *tc.ssid)
			}
			if octets == nil || *octets != tc.wantOctets || chars == nil || *chars != tc.wantChars {
				t.Fatalf("stored octets=%v chars=%v, want %d/%d", octets, chars, tc.wantOctets, tc.wantChars)
			}
		})
	}
}

// TestLocations_WiFiSSIDCheck_AppliesToUpdate: a column constraint that only bit
// on INSERT would be worth little -- the panel edits an existing location. The
// accepted half is the positive control: the same statement with a legal value
// must succeed, otherwise "the UPDATE failed" would prove nothing about the CHECK.
func TestLocations_WiFiSSIDCheck_AppliesToUpdate(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)
	tenantID, locationID := newTenant(t, d)

	update := func(v *string) error {
		return d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx,
				`UPDATE locations SET wifi_ssid = $1 WHERE id = $2 AND tenant_id = $3`,
				v, locationID, tenantID)
			return e
		})
	}

	if err := update(strPtr("KF-StJulians-Staff")); err != nil {
		t.Fatalf("UPDATE with a legal SSID failed: %v (without this the rejection below proves nothing)", err)
	}
	got, _, _ := readSSID(t, d, tenantID, locationID)
	if got == nil || *got != "KF-StJulians-Staff" {
		t.Fatalf("stored %v after UPDATE, want KF-StJulians-Staff", got)
	}

	pg := asPgErr(update(strPtr(strings.Repeat("z", 33))))
	if pg == nil || pg.Code != "23514" || pg.ConstraintName != "locations_wifi_ssid_len" {
		t.Fatalf("33-octet UPDATE was not refused by locations_wifi_ssid_len (pgErr = %v)", pg)
	}
	// The refused UPDATE must not have changed anything.
	got, _, _ = readSSID(t, d, tenantID, locationID)
	if got == nil || *got != "KF-StJulians-Staff" {
		t.Fatalf("value after a refused UPDATE = %v, want the previous KF-StJulians-Staff", got)
	}
}

// TestGetLocationWiFi_ReturnsNameAndIsTenantScoped exercises the generated
// activation query. Three things at once, each with its counterpart:
//   - a configured location returns its SSID;
//   - a location with no network returns nil, not "" -- phase B must be able to
//     tell "skip the step" from "join a network called nothing";
//   - the same id read from ANOTHER tenant's context returns pgx.ErrNoRows. The
//     positive control (same id, own tenant) is the first assertion, so the
//     ErrNoRows cannot be a vacuous "row does not exist".
func TestGetLocationWiFi_ReturnsNameAndIsTenantScoped(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)

	tenantA, locationA := newTenant(t, d)
	tenantB, _ := newTenant(t, d)

	const ssid = "KF-StJulians-Staff"
	if err := d.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE locations SET wifi_ssid = $1 WHERE id = $2 AND tenant_id = $3`,
			ssid, locationA, tenantA)
		return e
	}); err != nil {
		t.Fatalf("set ssid: %v", err)
	}

	// Own tenant: the row and the name come back.
	if err := d.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetLocationWiFi(ctx, store.GetLocationWiFiParams{
			TenantID: tenantA, ID: locationA,
		})
		if e != nil {
			return e
		}
		if row.ID != locationA || row.TenantID != tenantA {
			t.Errorf("GetLocationWiFi returned %s/%s, want %s/%s", row.ID, row.TenantID, locationA, tenantA)
		}
		if row.WifiSsid == nil || *row.WifiSsid != ssid {
			t.Errorf("wifi_ssid = %v, want %q", row.WifiSsid, ssid)
		}
		return nil
	}); err != nil {
		t.Fatalf("GetLocationWiFi(own tenant): %v", err)
	}

	// A location with no network: nil, and no error.
	locationNull := uuid.New()
	if err := insertLocationSSID(t, d, tenantA, locationNull, nil); err != nil {
		t.Fatalf("insert null-ssid location: %v", err)
	}
	if err := d.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetLocationWiFi(ctx, store.GetLocationWiFiParams{
			TenantID: tenantA, ID: locationNull,
		})
		if e != nil {
			return e
		}
		if row.WifiSsid != nil {
			t.Errorf("wifi_ssid = %q for a location with no network, want nil", *row.WifiSsid)
		}
		return nil
	}); err != nil {
		t.Fatalf("GetLocationWiFi(null ssid): %v", err)
	}

	// Another tenant asking for A's location id: nothing. Both belts are in force
	// here (the explicit tenant_id predicate AND the policy); this asserts the
	// OUTCOME, and TestRLS_LocationWiFiSSID_IsolatedAcrossTenants below asserts
	// that RLS alone would also do it.
	err := d.WithTenant(context.Background(), tenantB, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).GetLocationWiFi(ctx, store.GetLocationWiFiParams{
			TenantID: tenantB, ID: locationA,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-tenant GetLocationWiFi error = %v, want pgx.ErrNoRows", err)
	}
}

// TestRLS_LocationWiFiSSID_IsolatedAcrossTenants: the new column is inside the
// tenant boundary. The probes are RAW -- they identify the row by its own id and
// carry NO tenant_id predicate (CLAUDE.md section 6) -- so a 0 can only come from
// the policy. Read and write are both probed, because a column added after the
// policy was written is exactly the case where one might imagine a gap.
func TestRLS_LocationWiFiSSID_IsolatedAcrossTenants(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)

	tenantA, locationA := newTenant(t, d)
	tenantB, _ := newTenant(t, d)

	const ssid = "RustyBar-Staff"
	if err := d.WithTenant(context.Background(), tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE locations SET wifi_ssid = $1 WHERE id = $2 AND tenant_id = $3`,
			ssid, locationA, tenantA)
		return e
	}); err != nil {
		t.Fatalf("set ssid: %v", err)
	}

	const raw = `SELECT count(*) FROM locations WHERE id = $1 AND wifi_ssid IS NOT NULL`
	if n := rawCountCtx(t, d, tenantA, raw, locationA); n != 1 {
		t.Fatalf("own tenant sees %d rows, want 1 -- the positive control failed, so the 0 below would mean nothing", n)
	}
	if n := rawCountCtx(t, d, tenantB, raw, locationA); n != 0 {
		t.Fatalf("tenant B sees %d rows of tenant A's location, want 0 (RLS)", n)
	}

	// Writing is invisible too: B's UPDATE matches no row, so it cannot rename
	// another tenant's network. Positive control first, on B's own location.
	updateFrom := func(ctxTenant uuid.UUID, targetLocation uuid.UUID) int64 {
		var affected int64
		if err := d.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			tag, e := tx.Exec(ctx,
				`UPDATE locations SET wifi_ssid = 'attacker-net' WHERE id = $1`, targetLocation)
			if e != nil {
				return e
			}
			affected = tag.RowsAffected()
			return nil
		}); err != nil {
			t.Fatalf("updateFrom: %v", err)
		}
		return affected
	}
	if n := updateFrom(tenantA, locationA); n != 1 {
		t.Fatalf("tenant A updating its own location affected %d rows, want 1 (positive control)", n)
	}
	if n := updateFrom(tenantB, locationA); n != 0 {
		t.Fatalf("tenant B updating tenant A's location affected %d rows, want 0", n)
	}
	// And A's value is whatever A last wrote, never B's.
	got, _, _ := readSSID(t, d, tenantA, locationA)
	if got == nil || *got != "attacker-net" {
		t.Fatalf("tenant A's value = %v, want the value A itself wrote", got)
	}
}

// TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy checks the two claims 00010 makes
// about why a column addition needed neither: the grant on locations is
// TABLE-level (so it reaches columns added later) and RLS is a ROW-level
// mechanism (so the existing policy still governs the whole row).
//
// The catalog answer alone is not accepted as evidence (M1-04): the privilege is
// also EXERCISED. And the attacl check is what makes the reasoning transferable --
// on employee_invites, where 00009 does grant column-level SELECT to
// tappa_resolver, this same conclusion would NOT follow.
func TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)

	var appSelect, appInsert, appUpdate, resolverSelect bool
	var colACLs, policies int
	var rowSec, forceSec bool
	if err := d.pool.QueryRow(context.Background(), `
		SELECT has_column_privilege('tappa_app', 'locations', 'wifi_ssid', 'SELECT'),
		       has_column_privilege('tappa_app', 'locations', 'wifi_ssid', 'INSERT'),
		       has_column_privilege('tappa_app', 'locations', 'wifi_ssid', 'UPDATE'),
		       has_column_privilege('tappa_resolver', 'locations', 'wifi_ssid', 'SELECT'),
		       (SELECT count(*) FROM pg_attribute
		         WHERE attrelid = 'locations'::regclass AND attnum > 0 AND attacl IS NOT NULL),
		       (SELECT count(*) FROM pg_policies WHERE tablename = 'locations'),
		       (SELECT relrowsecurity FROM pg_class WHERE oid = 'locations'::regclass),
		       (SELECT relforcerowsecurity FROM pg_class WHERE oid = 'locations'::regclass)`,
	).Scan(&appSelect, &appInsert, &appUpdate, &resolverSelect, &colACLs, &policies, &rowSec, &forceSec); err != nil {
		t.Fatalf("read catalog: %v", err)
	}

	if !appSelect || !appInsert || !appUpdate {
		t.Errorf("tappa_app on wifi_ssid: select=%v insert=%v update=%v, want all true (a column-level grant would have been needed)",
			appSelect, appInsert, appUpdate)
	}
	if resolverSelect {
		t.Error("tappa_resolver can read locations.wifi_ssid; the bounded-bypass role has no business on this table (ADR 0002 madde 7)")
	}
	if colACLs != 0 {
		t.Errorf("locations has %d column-level ACL(s); 00010's reasoning ('the table grant covers new columns') only holds while this is 0", colACLs)
	}
	if policies != 1 || !rowSec || !forceSec {
		t.Errorf("locations RLS state: policies=%d rowsecurity=%v force=%v, want 1/true/true (00010 must not have disturbed 00002)",
			policies, rowSec, forceSec)
	}

	// Exercise, not just inspect: read and write the column as tappa_app.
	tenantID, locationID := newTenant(t, d)
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`UPDATE locations SET wifi_ssid = 'KF-HQ-Staff' WHERE id = $1 AND tenant_id = $2`,
			locationID, tenantID); e != nil {
			return e
		}
		var got *string
		return tx.QueryRow(ctx,
			`SELECT wifi_ssid FROM locations WHERE id = $1 AND tenant_id = $2`,
			locationID, tenantID).Scan(&got)
	}); err != nil {
		t.Fatalf("tappa_app could not write+read wifi_ssid: %v", err)
	}
}
