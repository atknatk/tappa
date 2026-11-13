package db

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// tagstatusdefault_test.go -- migration 00090 (T76, step 1 of 3): a tags row that does not name its
// status is born 'unassigned'. Before it the default was 00004's 'active', so an INSERT that omitted
// the status and named a location landed the A-1 shape -- on a wall, 'active', never stamped -- by
// omission (00025's header). Steps 2 (fixtures load -> stamp -> mount) and 3 (00100's BEFORE INSERT
// trigger, tags_is_born_in_stock) came later and are tagbirth_test.go's; this file keeps step 1's
// default, and its CONTROL now meets step 3.

const tagStatusDefaultFile = "00090_plaques_are_born_unassigned.sql"

// tsdDefault reads tags.status's DEFAULT as the catalogue spells it.
func tsdDefault(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var def string
	if err := q.QueryRow(ctx, `SELECT pg_get_expr(d.adbin, d.adrelid) FROM pg_attrdef d
	                           JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
	                          WHERE d.adrelid = 'public.tags'::regclass AND a.attname = 'status'`).Scan(&def); err != nil {
		t.Fatalf("read tags.status DEFAULT: %v", err)
	}
	return def
}

// TestTags00090_AnOmittedStatusIsUnassigned: the catalogue's default is 'unassigned'; as tappa_app in
// its own tenant's context an INSERT with no status and no location is an 'unassigned' row, and one
// with no status and a location is refused -- the A-1 shape cannot be reached by omission. Since
// 00100 the refusal is tags_is_born_in_stock's (23001: a BEFORE trigger fires before any CHECK);
// 00013's tags_unassigned_has_no_location still stands behind it (tagsinventory_test.go measures it
// on the UPDATE path). The same row SAYING 'active' is refused by 00100 too (until step 3 it
// landed: that was this test's control).
func TestTags00090_AnOmittedStatusIsUnassigned(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)
	if def := tsdDefault(t, context.Background(), app.pool); def != "'unassigned'::text" {
		t.Fatalf("tags.status DEFAULT is %s, want 'unassigned' (00090)", def)
	}

	stock := randUID(t)
	if _, err := execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, aes_key_ref) VALUES ($1, $2, decode(repeat('de', 44), 'hex'))`,
		stock, fx.tenantID); err != nil {
		t.Fatalf("an INSERT with no status and no location: %v, want an unassigned row", err)
	}
	var status string
	var located bool
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status, location_id IS NOT NULL FROM tags WHERE tenant_id = $1 AND uid = $2`, fx.tenantID, stock).Scan(&status, &located)
	}); err != nil {
		t.Fatal(err)
	}
	if status != "unassigned" || located {
		t.Errorf("the row with no status: status %q, located %v; want unassigned on no wall", status, located)
	}

	_, err := execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref) VALUES ($1, $2, $3, decode(repeat('de', 44), 'hex'))`,
		randUID(t), fx.tenantID, fx.locationID)
	wantBornInStockRefusal(t, err, "an INSERT with no status and a location")

	_, err = execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('de', 44), 'hex'), 'active')`,
		randUID(t), fx.tenantID, fx.locationID)
	wantBornInStockRefusal(t, err, "the same row saying 'active'")
}

// TestTags00090_TheDownGivesBackActiveAndTheUpTakesItAgain: both sections open with the
// lock_timeout (ALTER COLUMN ... SET DEFAULT takes ACCESS EXCLUSIVE on tags, on every NFC tap's path);
// the Down gives back 00004's 'active' and the Up 'unassigned'. Nothing else of tags changes: its
// constraints read the same before and after.
func TestTags00090_TheDownGivesBackActiveAndTheUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, tagStatusDefaultFile)
	for name, section := range map[string]string{"Up": up, "Down": down} {
		var first string
		for _, line := range strings.Split(section, "\n")[1:] {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "--") {
				first = line
				break
			}
		}
		if first != "SET LOCAL lock_timeout = '1s';" {
			t.Errorf("00090's %s opens with %q, want the lock_timeout", name, first)
		}
	}
	if got := downTables(up + down); strings.Join(got, ",") != "tags,tags" {
		t.Fatalf("00090's sections name %v, want tags in each", got)
	}
	lockForDowns(t, ctx, tx, []string{"tags"})
	constraints := func(q opQuerier) string {
		t.Helper()
		var s string
		if err := q.QueryRow(ctx, `SELECT string_agg(conname || ' ' || pg_get_constraintdef(oid), '; ' ORDER BY conname)
		                           FROM pg_constraint WHERE conrelid = 'public.tags'::regclass`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := constraints(tx)
	if def := tsdDefault(t, ctx, tx); def != "'unassigned'::text" {
		t.Fatalf("HEAD: tags.status DEFAULT is %s, want 'unassigned'", def)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00090 Down")
		if def := tsdDefault(t, ctx, sp); def != "'active'::text" {
			t.Fatalf("after the Down: tags.status DEFAULT is %s, want 00004's 'active'", def)
		}
		opRunSection(t, ctx, sp, up, "00090 Up")
		if def := tsdDefault(t, ctx, sp); def != "'unassigned'::text" {
			t.Fatalf("Down then Up: tags.status DEFAULT is %s, want 'unassigned'", def)
		}
		if after := constraints(sp); after != before {
			t.Fatalf("the constraints of tags changed:\n %s\nwant\n %s", after, before)
		}
	})
}
