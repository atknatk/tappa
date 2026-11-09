package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ============================================================================
// Migration 00093 (T216) -- tenants_name_keeps_its_isolate: the business name's character rule
// (internal/domain/tenant's NameBreaksIsolation) in the schema. That the predicate is the Go rule
// over every code point is internal/domain/tenant's test (this package cannot import that one);
// here: the catalogue, the table refusing each rune of the set, and the file's two sections.
// ============================================================================

const (
	tenantNameFile       = "00093_keep_tenant_names_inside_their_isolate.sql"
	tenantNameConstraint = "tenants_name_keeps_its_isolate"
	tenantNameRule       = "CHECK ((name !~ '[\\u0001-\\u001F\\u007F-\\u009F\\u2028\\u2029\\u202A-\\u202E\\u2066-\\u2069]'::text))"
)

// tenantNameSet is the rule's 75 storable runes by range: Cc without U+0000 (text cannot hold it),
// the two separators, the five embeddings and overrides, the four isolates.
var tenantNameSet = [][2]rune{{0x01, 0x1f}, {0x7f, 0x9f}, {0x2028, 0x2029}, {0x202a, 0x202e}, {0x2066, 0x2069}}

// tenantNameState is the constraint as the catalogue holds it, or "absent".
func tenantNameState(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var def string
	var validated bool
	err := q.QueryRow(ctx, `SELECT pg_get_constraintdef(oid), convalidated FROM pg_constraint
	                         WHERE conrelid = 'public.tenants'::regclass AND conname = $1`, tenantNameConstraint).Scan(&def, &validated)
	if errors.Is(err, pgx.ErrNoRows) {
		return "absent"
	}
	if err != nil {
		t.Fatalf("read %s: %v", tenantNameConstraint, err)
	}
	return fmt.Sprintf("%s validated=%v", def, validated)
}

// TestTenants00093_TheNameRuleIsValidated: the constraint is there, by its exact text, and
// VALIDATED -- every stored name, not only those written after it, is outside the set. (The search's
// half of T216 rests on that: op_read_tenants matches the stored name, the screens print it without
// the set, and the two are one text only while no stored name carries one.)
func TestTenants00093_TheNameRuleIsValidated(t *testing.T) {
	ctx, tx := opTx(t)
	if got, want := tenantNameState(t, ctx, tx), tenantNameRule+" validated=true"; got != want {
		t.Fatalf("%s is %s, want %s", tenantNameConstraint, got, want)
	}
}

// TestTenants00093_TheTableRefusesEveryRuneOfTheSet drives the table itself: each of the 75 runes
// inside a name is 23514 on tenants_name_keeps_its_isolate, and U+0000 never reaches it (54000 from chr(0): text
// cannot hold it). CONTROL: each range's two neighbours, and the runes the rule leaves alone on
// purpose -- an RLM, a ZWSP, a ZWNJ, a ZWJ, an Arabic and a Hebrew letter -- are stored.
func TestTenants00093_TheTableRefusesEveryRuneOfTheSet(t *testing.T) {
	ctx, tx := opTx(t)
	tenant := aiTenant(t, ctx, tx)
	rename := func(r rune) error {
		var err error
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err = sp.Exec(ctx, `UPDATE tenants SET name = 'Rusty' || chr($1) || 'Bar' WHERE id = $2`, int32(r), tenant)
		})
		return err
	}
	refused := 0
	for _, span := range tenantNameSet {
		for r := span[0]; r <= span[1]; r++ {
			var pg *pgconn.PgError
			if err := rename(r); !errors.As(err, &pg) || pg.Code != sqlstateCheckViolation || pg.ConstraintName != tenantNameConstraint {
				t.Errorf("U+%04X inside a name: %v, want 23514 on %s", r, err, tenantNameConstraint)
				continue
			}
			refused++
		}
	}
	if refused != 75 {
		t.Errorf("%d runes refused, want 75", refused)
	}
	var pg *pgconn.PgError
	if err := rename(0); !errors.As(err, &pg) || pg.Code != "54000" {
		t.Errorf("U+0000: %v, want 54000, chr(0): text cannot carry it", err)
	}
	accepted := []rune{0x20, 0xa0, 0x2027, 0x202f, 0x2065, 0x206a, 0x200b, 0x200c, 0x200d, 0x200e, 0x200f, 0x061c, 0x0645, 0x05e9}
	for _, span := range tenantNameSet {
		if span[0] > 0x01 {
			accepted = append(accepted, span[0]-1)
		}
		accepted = append(accepted, span[1]+1)
	}
	for _, r := range accepted {
		if r >= 0x01 && r <= 0x1f || r >= 0x7f && r <= 0x9f || r >= 0x2028 && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			continue // a neighbour of one range inside the next (U+2028-U+202E are adjacent)
		}
		if err := rename(r); err != nil {
			t.Errorf("CONTROL: U+%04X inside a name: %v, want it stored", r, err)
		}
	}
}

// TestTenants00093_TheDownAndTheUpAndTheRefusal: both sections open with the lock_timeout; the Down
// drops the constraint and the Up puts it back, validated. Over a planted name of the set -- a
// database whose name predates the rule -- the Up refuses with 23514 naming 00093 and the COUNT, no
// id and no byte of the name in the message, detail, hint or context, and leaves no constraint
// behind (NOT VALID included: a CHECK binds every UPDATE of that tenant's row, whatever it changes).
func TestTenants00093_TheDownAndTheUpAndTheRefusal(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, tenantNameFile)
	for name, section := range map[string]string{"Up": up, "Down": down} {
		var first string
		for _, line := range strings.Split(section, "\n")[1:] {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "--") {
				first = line
				break
			}
		}
		if first != "SET LOCAL lock_timeout = '1s';" {
			t.Errorf("00093's %s opens with %q, want the lock_timeout", name, first)
		}
	}
	lockForDowns(t, ctx, tx, []string{"tenants"})
	head := tenantNameRule + " validated=true"
	if got := tenantNameState(t, ctx, tx); got != head {
		t.Fatalf("HEAD: %s, want %s", got, head)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00093 Down")
		if got := tenantNameState(t, ctx, sp); got != "absent" {
			t.Fatalf("after the Down: %s, want it absent", got)
		}
		opRunSection(t, ctx, sp, up, "00093 Up")
		if got := tenantNameState(t, ctx, sp); got != head {
			t.Fatalf("Down then Up: %s, want %s", got, head)
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00093 Down")
		tenant := aiTenant(t, ctx, sp)
		const marker = "Plantedname"
		if _, err := sp.Exec(ctx, `UPDATE tenants SET name = $1 || chr(8238) || 'Bar' WHERE id = $2`, marker, tenant); err != nil {
			t.Fatalf("plant a name from before the rule: %v", err)
		}
		inner, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = inner.Conn().PgConn().Exec(ctx, up).ReadAll()
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != sqlstateCheckViolation || !strings.Contains(pg.Message, "migration 00093: 1 tenants rows") {
			t.Fatalf("00093's Up over a planted name answered %v, want 23514 naming 00093 and the count", err)
		}
		for _, text := range []string{pg.Message, pg.Detail, pg.Hint, pg.Where} {
			if strings.Contains(text, marker) || strings.Contains(text, tenant.String()) || strings.Contains(text, tenant.String()[:8]) {
				t.Fatalf("the refusal names the row or its name: %q", text)
			}
		}
		if err := inner.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if got := tenantNameState(t, ctx, sp); got != "absent" {
			t.Fatalf("after the refusal: %s, want no constraint left behind", got)
		}
	})
}
