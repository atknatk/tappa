package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ============================================================================
// Migration 00021 part 2 (T7) -- the schema now knows what a wrapped key LOOKS
// like, and part 3 (T15) -- the canonical-uid rule is finally validated.
//
// WHY THE SHAPE MATTERS AT ALL, in one sentence: a tags row whose aes_key_ref is
// not 44 bytes is a plaque the panel calls encoded and the tap path answers 500
// for, with NO transactions row written -- so the evidence of the attempt never
// exists, which is a section 4.6 hole on that path rather than a cosmetic
// mismatch. ADR 0003 article 4 fixes the envelope: nonce(12) || ciphertext(16) ||
// tag(16).
// ============================================================================

// TestTags00021_KeyRefMustHaveTheEnvelopeShape drives the boundary from both
// sides. 43 and 45 are here rather than only 2, because an "is it non-empty"
// constraint would pass the two-byte case for the wrong reason; the rule is an
// exact length and the test asks for exactly that.
func TestTags00021_KeyRefMustHaveTheEnvelopeShape(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	for _, tc := range []struct {
		name  string
		bytes int
		ok    bool
	}{
		{"the_old_two_byte_marker", 2, false},
		{"one_byte_short", 43, false},
		{"one_byte_long", 45, false},
		{"empty", 0, false},
		{"the_envelope", 44, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid := randUID(t)
			// decode(repeat('de', n), 'hex') yields exactly n bytes -- the length is
			// the variable under test, so it is computed in SQL rather than hidden in
			// a Go literal a reader would have to count.
			// The row is born in stock (00100, T76): the key's length is the variable, and a
			// CHECK is evaluated after every BEFORE trigger, so only a stock row reaches it.
			_, err := execAs(t, app, fx.tenantID,
				`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
				 VALUES ($1, $2, NULL, decode(repeat('de', $3::int), 'hex'), 0, 'unassigned')`,
				uid, fx.tenantID, tc.bytes)

			if tc.ok {
				if err != nil {
					t.Fatalf("a %d-byte aes_key_ref was REFUSED (%v) -- the constraint must "+
						"accept the shape ADR 0003 defines", tc.bytes, err)
				}
				return
			}
			wantSQLSTATE(t, err, sqlstateCheckViolation,
				"INSERT tags with a "+strconv.Itoa(tc.bytes)+"-byte aes_key_ref")
		})
	}
}

// TestTags00089_KeyRefConstraintIsValidated pins the catalog facts 00021 and 00089 rest on: the
// constraint exists, its predicate is exactly the 44-byte envelope rule, and since 00089 (T7) it is
// VALIDATED -- every row of tags, not only those written after 00021, is known to have the shape.
// (Until 00089 this test pinned the NOT VALID state and only logged the day the flag turned; a
// validated flag is now the claim, so its absence is red.)
//
// 🔴 00021'S LESSON STILL HOLDS: NOT VALID never exempted a new row, the owner's included, so a
// violating row cannot be planted by an INSERT. The refusal case below therefore drops the
// constraint inside the rolled-back transaction, plants the row, and puts 00021's NOT VALID back --
// the state a database that still had a pre-00021 row would be in -- and runs 00089's Up over it.
func TestTags00089_KeyRefConstraintIsValidated(t *testing.T) {
	app := appDB(t)

	var def string
	var validated bool
	if err := app.pool.QueryRow(context.Background(),
		`SELECT pg_get_constraintdef(oid), convalidated FROM pg_constraint
		  WHERE conrelid = 'tags'::regclass AND conname = 'tags_aes_key_ref_is_kek_envelope'`,
	).Scan(&def, &validated); err != nil {
		t.Fatalf("read tags_aes_key_ref_is_kek_envelope: %v (00021 part 2 did not take?)", err)
	}
	// pg_get_constraintdef appends " NOT VALID" to an unvalidated CHECK; the rule is read without it
	// and the flag on its own, so each failure says which of the two it is.
	if rule := strings.TrimSuffix(def, " NOT VALID"); rule != "CHECK ((octet_length(aes_key_ref) = 44))" {
		t.Fatalf("constraint definition is %q, want the 44-byte envelope rule (ADR 0003 article 4)", def)
	}
	if !validated {
		t.Fatal("tags_aes_key_ref_is_kek_envelope is NOT VALID -- 00089 did not take, or a Down left it (backlog T7)")
	}
}

const keyEnvelopeValidateFile = "00089_validate_the_key_envelope_shape.sql"

// TestTags00089_TheDownGivesBack00021AndTheUpRefusesAShortKey: both sections open with the
// lock_timeout; the Down leaves 00021's constraint by the same text, NOT VALID, and the Up validates
// it again. Over a planted 2-byte key the Up refuses with 23514 naming 00089 and the COUNT -- no
// uid, no byte of a key in the message, the detail or the hint -- and leaves the constraint NOT VALID.
func TestTags00089_TheDownGivesBack00021AndTheUpRefusesAShortKey(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, keyEnvelopeValidateFile)
	for name, section := range map[string]string{"Up": up, "Down": down} {
		var first string
		for _, line := range strings.Split(section, "\n")[1:] {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "--") {
				first = line
				break
			}
		}
		if first != "SET LOCAL lock_timeout = '1s';" {
			t.Errorf("00089's %s opens with %q, want the lock_timeout", name, first)
		}
	}
	lockForDowns(t, ctx, tx, []string{"tags"})
	state := func(q opQuerier) string {
		t.Helper()
		var def string
		var validated bool
		if err := q.QueryRow(ctx, `SELECT pg_get_constraintdef(oid), convalidated FROM pg_constraint
		                           WHERE conrelid = 'public.tags'::regclass AND conname = 'tags_aes_key_ref_is_kek_envelope'`).Scan(&def, &validated); err != nil {
			t.Fatalf("read the constraint: %v", err)
		}
		return fmt.Sprintf("%s validated=%v", def, validated)
	}
	const head = "CHECK ((octet_length(aes_key_ref) = 44)) validated=true"
	const at21 = "CHECK ((octet_length(aes_key_ref) = 44)) NOT VALID validated=false"
	if got := state(tx); got != head {
		t.Fatalf("HEAD: %s, want %s", got, head)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00089 Down")
		if got := state(sp); got != at21 {
			t.Fatalf("after the Down: %s, want 00021's %s", got, at21)
		}
		opRunSection(t, ctx, sp, up, "00089 Up")
		if got := state(sp); got != head {
			t.Fatalf("Down then Up: %s, want %s", got, head)
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		tenant := aiTenant(t, ctx, sp)
		uid := randUID(t)
		for _, sql := range []string{
			`ALTER TABLE tags DROP CONSTRAINT tags_aes_key_ref_is_kek_envelope`,
			`INSERT INTO tags (uid, tenant_id, aes_key_ref, status) VALUES ('` + uid + `', '` + tenant.String() + `', decode('dead', 'hex'), 'unassigned')`,
			`ALTER TABLE tags ADD CONSTRAINT tags_aes_key_ref_is_kek_envelope CHECK (octet_length(aes_key_ref) = 44) NOT VALID`,
		} {
			if _, err := sp.Exec(ctx, sql); err != nil {
				t.Fatalf("plant a pre-00021 row: %s: %v", sql, err)
			}
		}
		inner, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = inner.Conn().PgConn().Exec(ctx, up).ReadAll()
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != sqlstateCheckViolation || !strings.Contains(pg.Message, "migration 00089: 1 tags rows") {
			t.Fatalf("00089's Up over a 2-byte key answered %v, want 23514 naming 00089 and the count", err)
		}
		for _, text := range []string{pg.Message, pg.Detail, pg.Hint, pg.Where} {
			if strings.Contains(strings.ToUpper(text), uid) || strings.Contains(strings.ToLower(text), "dead") {
				t.Fatalf("the refusal names the row or its key: %q", text)
			}
		}
		if err := inner.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if got := state(sp); got != at21 {
			t.Fatalf("after the refusal: %s, want it left NOT VALID", got)
		}
	})
}

// TestTags00021_CanonicalUidConstraintIsValidated is T15's whole content: the
// rule 00013 could only half-enforce is now known to hold for every row.
//
// convalidated is read from pg_constraint rather than inferred from an INSERT
// succeeding or failing -- new rows were ALREADY checked before 00021 (NOT VALID
// never exempted them), so an INSERT-based test would have passed before the
// change and proved nothing.
func TestTags00021_CanonicalUidConstraintIsValidated(t *testing.T) {
	app := appDB(t)

	var validated bool
	if err := app.pool.QueryRow(context.Background(),
		`SELECT convalidated FROM pg_constraint
		  WHERE conrelid = 'tags'::regclass AND conname = 'tags_uid_canonical_hex'`,
	).Scan(&validated); err != nil {
		t.Fatalf("read tags_uid_canonical_hex: %v", err)
	}
	if !validated {
		t.Fatal("tags_uid_canonical_hex is still NOT VALID -- 00021 part 3 did not take, " +
			"or a Down left the schema half-way (backlog T15)")
	}

	// And the fact the validation asserts: zero rows in the whole table violate it.
	// Read with the OWNER? No: this is a whole-table claim and tappa_app sees only
	// its tenant, so the count is done with the constraint's own predicate through
	// the catalog above. What tappa_app CAN prove is that the rule still bites.
	fx := newTagTenant(t, app)
	_, err := execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
		 VALUES ('04ac7e55aaaa01', $1, NULL, decode(repeat('dead', 22), 'hex'), 0, 'unassigned')`,
		fx.tenantID)
	wantSQLSTATE(t, err, sqlstateCheckViolation, "INSERT a lower-case uid")
}
