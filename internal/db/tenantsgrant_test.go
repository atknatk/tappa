package db

// tenantsgrant_test.go -- the proofs for migration 00024 (M10 Faz 0 OP-3,
// architecture finding A-4): tappa_app's UPDATE on `tenants` narrowed to the three
// columns the product writes (name, business_type, timezone), and its table-wide
// DELETE revoked.
//
// A file of its own, for the reason encodedat_test.go gives: every migration since
// 00013 keeps its proofs in one. The fixture helpers (execAs, wantSQLSTATE and the
// SQLSTATE constants) are tagsinventory_test.go's and are reused unchanged.
//
// EVERYTHING HERE RUNS AGAINST REAL POSTGRES AS tappa_app: a GRANT is one of the
// things a fake database cannot have (CLAUDE.md §8).
//
// Fixtures are not cleaned up -- and after this migration they CANNOT be by this
// role, which is the point of the second half.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// newBareTenant commits ONE tenant row and nothing else, as tappa_app in its own
// context.
//
// 🔴 NO CHILD ROWS, AND THAT IS WHAT KEEPS THE DELETE ASSERTION BELOW HONEST. All
// sixteen foreign keys that reference `tenants` are ON DELETE RESTRICT, so a tenant
// with even one venue refuses a DELETE with 23503 whatever the privileges say. A
// fixture with a location would therefore make "the DELETE failed" true with or
// without 00024, and the test would prove nothing. Measured before the migration:
// on exactly this shape of row, tappa_app's DELETE answered DELETE 1.
func newBareTenant(t *testing.T, d *DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := d.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'tenants-00024', $2, 'bar', 'single')`,
			id, "VAT-00024-"+id.String())
		return e
	}); err != nil {
		t.Fatalf("newBareTenant: %v", err)
	}
	return id
}

// TestTenants00024_TheAppMayUpdateThreeColumnsAndDeleteNothing is the mechanical
// gate on 00024, read from the catalogue AND exercised as statements.
//
// THE CATALOGUE HALF compares each privilege's column set as a WHOLE rather than
// probing one column at a time -- the shape TestTags00022_TheAppMayStampTheMarkerAndStillNotTheFourProtectedColumns
// uses, for its reason: a mis-written REVOKE/GRANT pair can widen a list silently,
// and only a whole-list comparison sees a column that should not be there.
//
// The INSERT list is pinned too, although 00024 does not touch it, because it is
// the one list this migration could have broken by accident and the sign-up wizard
// depends on every column of it (CreateTenant, M7-02).
//
// THE STATEMENT HALF is the measurement from the migration's header, re-run on
// every `make test`: before 00024 each refused statement below answered UPDATE 1 /
// DELETE 1 as tappa_app in its own tenant. The positive control is what keeps the
// refusals from being a blanket revoke that also broke the account screen (M7-05).
func TestTenants00024_TheAppMayUpdateThreeColumnsAndDeleteNothing(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	ctx := context.Background()

	columns := func(priv string) string {
		t.Helper()
		var got string
		if err := app.pool.QueryRow(ctx,
			`SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attnum), '')
			   FROM pg_attribute a
			  WHERE a.attrelid = 'tenants'::regclass AND a.attnum > 0 AND NOT a.attisdropped
			    AND has_column_privilege('tappa_app', 'tenants', a.attname, $1)`, priv,
		).Scan(&got); err != nil {
			t.Fatalf("read tappa_app's %s column privileges on tenants: %v", priv, err)
		}
		return got
	}
	if got, want := columns("UPDATE"), "name,business_type,timezone"; got != want {
		t.Fatalf("tappa_app may UPDATE tenants (%s)\nwant                       (%s)\n"+
			"00024 closed the list to the three columns UpdateTenantAccount writes. "+
			"vat_number is globally UNIQUE (writing it refuses another business its "+
			"registration) and structure decides nothing after sign-up", got, want)
	}
	if got, want := columns("INSERT"),
		"id,name,vat_number,business_type,structure,timezone,vat_verified,vat_checked_at"; got != want {
		t.Fatalf("tappa_app may INSERT tenants (%s)\nwant                       (%s)\n"+
			"00016 + 00017's list; 00024 must not have touched it -- the sign-up wizard "+
			"writes every one of these columns", got, want)
	}

	table := func(priv string) bool {
		t.Helper()
		var may bool
		if err := app.pool.QueryRow(ctx,
			`SELECT has_table_privilege('tappa_app', 'tenants', $1)`, priv).Scan(&may); err != nil {
			t.Fatalf("read table-wide %s on tenants: %v", priv, err)
		}
		return may
	}
	// A table-wide UPDATE would override the column list above, so the list can read
	// correctly while the protection is off.
	for priv, want := range map[string]bool{"SELECT": true, "UPDATE": false, "DELETE": false, "TRUNCATE": false} {
		if got := table(priv); got != want {
			t.Errorf("has_table_privilege(tappa_app, tenants, %s) = %v, want %v", priv, got, want)
		}
	}

	// --- the statements ---------------------------------------------------------
	id := newBareTenant(t, app)

	_, err := execAs(t, app, id, `UPDATE tenants SET vat_number = $2 WHERE id = $1`,
		id, "VAT-TAKEN-"+id.String())
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "tappa_app rewriting its own vat_number")

	_, err = execAs(t, app, id, `UPDATE tenants SET structure = 'multi' WHERE id = $1`, id)
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "tappa_app rewriting its own structure")

	_, err = execAs(t, app, id, `DELETE FROM tenants WHERE id = $1`, id)
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "tappa_app deleting its own childless tenant row")

	// POSITIVE CONTROL, same row: the three columns the account screen saves. Without
	// it the refusals above could be a role with no UPDATE at all.
	n, err := execAs(t, app, id,
		`UPDATE tenants SET name = 'tenants-00024 renamed', business_type = 'cafe',
		                    timezone = 'Europe/Rome' WHERE id = $1`, id)
	if err != nil || n != 1 {
		t.Fatalf("the account screen's own three columns: rows=%d err=%v, want 1/nil -- "+
			"00024 narrowed too far and M7-05 can no longer save", n, err)
	}

	// AND NOTHING THE REFUSED STATEMENTS TOUCHED MOVED. The row is still there, and
	// its VAT number and structure are the ones it was born with.
	var vat, structure string
	if err := app.WithTenant(ctx, id, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT vat_number, structure FROM tenants WHERE id = $1`, id).
			Scan(&vat, &structure)
	}); err != nil {
		t.Fatalf("reading the row back after the refused DELETE: %v", err)
	}
	if !strings.HasPrefix(vat, "VAT-00024-") || structure != "single" {
		t.Fatalf("after the refused writes: vat_number=%q structure=%q, want the values the "+
			"row was born with", vat, structure)
	}
}
