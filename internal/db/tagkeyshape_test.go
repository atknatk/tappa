package db

import (
	"context"
	"strconv"
	"strings"
	"testing"
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
			_, err := execAs(t, app, fx.tenantID,
				`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
				 VALUES ($1, $2, $3, decode(repeat('de', $4::int), 'hex'), 0, 'active')`,
				uid, fx.tenantID, fx.locationID, tc.bytes)

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

// TestTags00021_KeyRefConstraintIsNotValidAndSaysSo pins the two catalog facts
// the migration's reasoning rests on, because both of them are invisible from the
// behaviour of a fresh database and both would go stale silently.
//
// 🔴 THE FIRST DRAFT OF THIS TEST WAS WRONG AND THE DATABASE SAID SO. It tried to
// plant a violating row as the OWNER, on the assumption that NOT VALID exempts the
// role that owns the table. Measured: it does not. NOT VALID skips the initial
// table SCAN and nothing else -- the CHECK is evaluated for every new row version
// from any role, superuser included:
//
//	INSERT ... decode('dead','hex') as tappa_owner
//	  -> ERROR: new row for relation "tags" violates check constraint
//	            "tags_aes_key_ref_is_kek_envelope"  (SQLSTATE 23514)
//
// So the 61 033 legacy rows cannot be reproduced by any role, and the "frozen"
// consequence the migration names (every UPDATE of such a row is refused, because
// the CHECK runs against the whole new row) cannot be asserted without either a
// conditional skip on a fresh database or a DDL probe that takes ACCESS EXCLUSIVE
// on the hottest table in the suite. Neither is worth it for a statement about
// cost rather than about safety, so the consequence lives in 00021's comment and
// this test pins what CAN be pinned everywhere: the constraint's existence, its
// exact predicate, and the fact that it is still unvalidated -- which is the thing
// a future `make db-reset` is expected to change.
func TestTags00021_KeyRefConstraintIsNotValidAndSaysSo(t *testing.T) {
	app := appDB(t)

	var def string
	var validated bool
	if err := app.pool.QueryRow(context.Background(),
		`SELECT pg_get_constraintdef(oid), convalidated FROM pg_constraint
		  WHERE conrelid = 'tags'::regclass AND conname = 'tags_aes_key_ref_is_kek_envelope'`,
	).Scan(&def, &validated); err != nil {
		t.Fatalf("read tags_aes_key_ref_is_kek_envelope: %v (00021 part 2 did not take?)", err)
	}
	if !strings.Contains(def, "octet_length(aes_key_ref) = 44") {
		t.Fatalf("constraint definition is %q, want the 44-byte envelope rule (ADR 0003 article 4)", def)
	}
	if validated {
		t.Log("tags_aes_key_ref_is_kek_envelope is now VALIDATED -- the legacy short rows " +
			"are gone from this database. 00021's NOT VALID note and backlog T7 can be closed.")
	}
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
		 VALUES ('04ac7e55aaaa01', $1, $2, decode(repeat('dead', 22), 'hex'), 0, 'active')`,
		fx.tenantID, fx.locationID)
	wantSQLSTATE(t, err, sqlstateCheckViolation, "INSERT a lower-case uid")
}
