package db

// appkeyref_test.go -- the proofs for migration 00023 (ADR 0018): tags.app_key_ref,
// the SECOND per-plaque wrapped key (NTAG 424 DNA application key 0), stored in its
// own envelope column beside aes_key_ref (key 1).
//
// A NEW FILE, this package's pattern since 00013: every migration keeps its proofs
// in a file of its own (encodedat_test.go is 00022's). The FIXTURE HELPERS are
// reused unchanged from tagsinventory_test.go and store_test.go -- newTagTenant,
// addPlaque, execAs, wantSQLSTATE, randUID, appDB, ownerDB, assertAppRole,
// assertOwnerRole and the SQLSTATE constants are all package-level -- so nothing is
// duplicated; only the prose is separate.
//
// EVERYTHING HERE RUNS AGAINST REAL POSTGRES. A GRANT, a policy, a CHECK and a
// trigger are the four things a fake database cannot have (CLAUDE.md §8), and three
// of the four are what this migration IS.
//
// 🔴 app_key_ref IS A SECRET (CLAUDE.md §4.7). The fixtures use a FAKE 44-byte
// value (0xBEEF x22 / 0xC0DE x22) -- never a real key or KEK -- and every assertion
// that a value survived reads it as the OWNER, because the whole point of the
// migration is that tappa_app cannot. No test prints the bytes.
//
// 🔴 THE ISOLATION HALF CARRIES NO tenant_id FILTER (CLAUDE.md §6): with a filter,
// the 0 rows would be the WHERE's doing and the test would stay green with RLS off.
// It uses raw SQL through WithTenant, not the sqlc query, for the same reason --
// InsertUnassigned carries an explicit tenant predicate (§4.5's belt), so a store
// call could never isolate the policy. The app_key_ref READ wall is a different
// thing (a column privilege, not RLS) and is asserted BOTH raw and with the
// explicit predicate, because it must hold on every code path either way.

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fakeAppKeyRef and fakeAppKeyRefAlt are two DISTINCT 44-byte blobs shaped like the
// KEK-GCM envelope (nonce 12 || ciphertext 16 || tag 16). They are not real
// envelopes -- nothing here taps or unwraps them -- but they satisfy
// tags_app_key_ref_is_kek_envelope and stay recognisable in a byte dump.
func fakeAppKeyRef() []byte    { return bytes.Repeat([]byte{0xBE, 0xEF}, 22) }
func fakeAppKeyRefAlt() []byte { return bytes.Repeat([]byte{0xC0, 0xDE}, 22) }

// loadWithAppKey inserts one plaque carrying app_key_ref, in the shipped shape, as
// tappa_app in the given tenant's context. tappa_app writes it through its
// TABLE-WIDE INSERT (00004), which is the ONE privilege the encode flow needs.
func loadWithAppKey(t *testing.T, d *DB, tenant uuid.UUID, uid string, appKey []byte) error {
	t.Helper()
	_, err := execAs(t, d, tenant,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, app_key_ref, status)
		 VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), $3, 'unassigned')`,
		uid, tenant, appKey)
	return err
}

// ---------------------------------------------------------------------------
// RLS ISOLATION + THE COLUMN READ WALL
// ---------------------------------------------------------------------------

// TestRLS_AppKeyRef_TenantIsolation.
//
// 00023 adds a per-plaque wrapped master key. It is a fact about ONE customer's
// inventory AND a §4.7 secret, so it is protected on two independent axes and both
// are proved here:
//   - the ROW is invisible cross-tenant (RLS), and
//   - the COLUMN is unreadable by the application role at all (privilege),
//     so even the owning tenant's own connection cannot pull the wrapped key out.
func TestRLS_AppKeyRef_TenantIsolation(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := newTagTenant(t, app)
	b := newTagTenant(t, app)

	uid := randUID(t)
	if err := loadWithAppKey(t, app, a.tenantID, uid, fakeAppKeyRef()); err != nil {
		t.Fatalf("A loads a plaque carrying app_key_ref: %v", err)
	}

	// (1) RLS ROW isolation, RAW (no tenant predicate). B must not see A's plaque
	// at all, and therefore cannot learn that it carries a key 0. The positive
	// control in A's own context is what makes the 0 mean "policy", not "no row".
	seen := func(ctxTenant uuid.UUID) int {
		t.Helper()
		var n int
		if err := app.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1`, uid).Scan(&n)
		}); err != nil {
			t.Fatalf("count in tenant %s: %v", ctxTenant, err)
		}
		return n
	}
	if got := seen(b.tenantID); got != 0 {
		t.Fatalf("B sees %d of A's plaques, want 0", got)
	}
	if got := seen(a.tenantID); got != 1 {
		t.Fatalf("A sees %d of its own plaques, want 1 -- the fixture is broken, so the 0 above proves nothing", got)
	}

	// (2) THE COLUMN READ WALL. tappa_app has no SELECT on app_key_ref (mirrors
	// aes_key_ref, migration 00022), so a read answers `permission denied for table
	// tags` (42501) BEFORE RLS is even consulted. Asserted three ways so no code
	// path is left uncovered:
	//   - B, RAW (no predicate): a cross-tenant read cannot even begin.
	//   - B, with the §4.5 explicit tenant predicate: the belt does not open a hole.
	//   - A, in its OWN tenant: the wall is a COLUMN privilege, not RLS, so even the
	//     owner tenant's application connection cannot read the wrapped key.
	_, err := execAs(t, app, b.tenantID,
		`SELECT app_key_ref FROM tags WHERE uid = $1`, uid)
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "B reading app_key_ref (raw)")

	_, err = execAs(t, app, b.tenantID,
		`SELECT app_key_ref FROM tags WHERE tenant_id = $1 AND uid = $2`, a.tenantID, uid)
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "B reading app_key_ref (explicit tenant predicate)")

	_, err = execAs(t, app, a.tenantID,
		`SELECT app_key_ref FROM tags WHERE uid = $1`, uid)
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "A reading its OWN app_key_ref")

	// (3) THE WRITE SIDE. First a CONTROL: B loading a plaque carrying app_key_ref
	// into its OWN tenant succeeds, so the failure below is specifically the
	// cross-tenant refusal and not a broken statement.
	if err := loadWithAppKey(t, app, b.tenantID, randUID(t), fakeAppKeyRef()); err != nil {
		t.Fatalf("B loading a plaque with app_key_ref into its own tenant (control): %v", err)
	}
	// Now the forge: B's context, A's tenant_id in the row. RLS's WITH CHECK refuses
	// it loudly (42501, "new row violates row-level security policy"); the SQLSTATE
	// is pinned, not the message.
	_, err = execAs(t, app, b.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, app_key_ref, status)
		 VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), $3, 'unassigned')`,
		randUID(t), a.tenantID, fakeAppKeyRef())
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "B loading a plaque carrying app_key_ref into A's tenant")
}

// ---------------------------------------------------------------------------
// THE WRITE-ONCE GUARD
// ---------------------------------------------------------------------------

// TestAppKeyRef_WriteOnceForEveryRoleIncludingTheOwner.
//
// app_key_ref answers "what master key did we put on this chip". A chip's key 0 is
// changed once (ADR 0017 §5.1 step 8, irreversible), so a second envelope on one
// row is a claim about a key change that did not happen -- and it would strand the
// physical chip, whose key 0 would no longer match any envelope we hold (§4.7).
//
// Two mechanisms make it write-once and BOTH are proved:
//   - tappa_app has no UPDATE grant, so the application role cannot rewrite it at
//     all (permission denied, the structural half -- the same shape aes_key_ref
//     uses);
//   - the trigger binds every role INCLUDING the owner, whom no REVOKE can reach
//     (the belt 00005 puts over transactions, 00013 over the counter, 00022 over
//     encoded_at). The owner is a SUPERUSER, so FORCE ROW LEVEL SECURITY does not
//     bind it -- a trigger can.
//
// All four transitions are driven, because the guard's whole content is which two
// of them pass:
//
//	NULL  -> value ....... PASSES (a backfill / the mint)
//	value -> SAME value .. PASSES (an idempotent retry is not a rewrite)
//	value -> other value . REFUSED
//	value -> NULL ........ REFUSED  (IS DISTINCT FROM, not <>, catches this one)
func TestAppKeyRef_WriteOnceForEveryRoleIncludingTheOwner(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	uid := randUID(t)
	addPlaque(t, app, fx, uid, uuid.Nil, "unassigned", 0) // app_key_ref born NULL

	wrap := fakeAppKeyRef()
	wrap2 := fakeAppKeyRefAlt()

	// (0) THE APPLICATION-SIDE WRITE-ONCE: tappa_app cannot UPDATE app_key_ref at
	// all -- it is off the UPDATE grant (00013's five + 00022's encoded_at, and a
	// new column is not on that list). This is the structural half; permission
	// denied, not the trigger.
	_, err := execAs(t, app, fx.tenantID,
		`UPDATE tags SET app_key_ref = $2 WHERE uid = $1 AND tenant_id = $3`, uid, wrap, fx.tenantID)
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "tappa_app UPDATE app_key_ref")

	// The trigger is proved as the OWNER, the one role a grant cannot stop.
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	ctx := context.Background()

	// (a) NULL -> value. Owner bypasses RLS, so it addresses the row by uid alone.
	if tag, err := owner.pool.Exec(ctx,
		`UPDATE tags SET app_key_ref = $2 WHERE uid = $1`, uid, wrap); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("owner NULL->value: rows=%d err=%v, want 1/nil", tag.RowsAffected(), err)
	}

	// (b) value -> SAME value. Refusing this would turn a harmless retry into a
	// failed transaction (00011's BOUNDARY 2).
	if tag, err := owner.pool.Exec(ctx,
		`UPDATE tags SET app_key_ref = $2 WHERE uid = $1`, uid, wrap); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("owner value->SAME value: rows=%d err=%v, want 1/nil", tag.RowsAffected(), err)
	}

	// (c) value -> OTHER value.
	_, err = owner.pool.Exec(ctx,
		`UPDATE tags SET app_key_ref = $2 WHERE uid = $1`, uid, wrap2)
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "owner value->other value")
	if !strings.Contains(err.Error(), "write-once") {
		t.Fatalf("refused by something other than the write-once trigger: %v", err)
	}

	// 🔴 THE MESSAGE CARRIES NO SECRET. Unlike 00022's encoded_at guard (which
	// prints two timestamps, not secrets), this one is given ONLY the table name --
	// OLD and NEW are KEK-wrapped KEYS, and §4.7 forbids putting them in a log line.
	// Asserted rather than trusted, because the cheapest way to make an error more
	// helpful is to add the offending value to it.
	for _, secret := range []string{
		hex.EncodeToString(wrap), hex.EncodeToString(wrap2),
		"beef", "c0de", uid, fx.tenantID.String(),
	} {
		if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(secret)) {
			t.Fatalf("the write-once message names %q; it must carry the table name and nothing else.\nmessage: %v", secret, err)
		}
	}

	// (d) value -> NULL. The case a plain `<>` would let through (NULL comparison).
	_, err = owner.pool.Exec(ctx,
		`UPDATE tags SET app_key_ref = NULL WHERE uid = $1`, uid)
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "owner value->NULL")

	// (e) AN UNRELATED UPDATE STILL PASSES. The WHEN carries the whole condition,
	// so a bind, an unbind, a retire or a counter advance never enters the function.
	if tag, err := owner.pool.Exec(ctx,
		`UPDATE tags SET status = 'lost' WHERE uid = $1`, uid); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("unrelated UPDATE on a row with app_key_ref set: rows=%d err=%v, want 1/nil", tag.RowsAffected(), err)
	}
}

// ---------------------------------------------------------------------------
// GRANT MINIMALITY + THE ENVELOPE SHAPE + POSITIVE CONTROL
// ---------------------------------------------------------------------------

// TestAppKeyRef_AppGrantsAreMinimalAndTheEnvelopeIsWritten pins the privilege set
// and the CHECK, and proves the ONE write path works end to end.
//
// 🔴 ADDING A COLUMN IS EXACTLY THE CHANGE THAT WIDENS AN ACL BY ACCIDENT, so the
// privileges are read from the catalog rather than assumed. app_key_ref must be the
// TWIN of aes_key_ref: INSERT (table-wide, needs no line), and NEITHER SELECT nor
// UPDATE (least privilege, §4.7).
func TestAppKeyRef_AppGrantsAreMinimalAndTheEnvelopeIsWritten(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	// (1) THE PRIVILEGE SET, read as a whole. app_key_ref and aes_key_ref must be
	// identical: ins=true, sel=false, upd=false.
	for _, col := range []string{"app_key_ref", "aes_key_ref"} {
		var sel, upd, ins bool
		if err := app.pool.QueryRow(context.Background(),
			`SELECT has_column_privilege('tappa_app', 'tags', $1, 'SELECT'),
			        has_column_privilege('tappa_app', 'tags', $1, 'UPDATE'),
			        has_column_privilege('tappa_app', 'tags', $1, 'INSERT')`, col).Scan(&sel, &upd, &ins); err != nil {
			t.Fatalf("read tappa_app privileges on %s: %v", col, err)
		}
		if sel || upd || !ins {
			t.Fatalf("tappa_app on tags.%s: SELECT=%v UPDATE=%v INSERT=%v, want false/false/true "+
				"(a wrapped key is INSERT-only for the app role -- §4.7 least privilege, ADR 0018)", col, sel, upd, ins)
		}
	}

	// AND TABLE-WIDE SELECT/UPDATE MUST STILL BE GONE: a table-level privilege
	// overrides the column list, so the column check can read correct while the
	// protection is off (00022's own restore-experiment finding).
	var tblSel, tblUpd bool
	if err := app.pool.QueryRow(context.Background(),
		`SELECT has_table_privilege('tappa_app', 'tags', 'SELECT'),
		        has_table_privilege('tappa_app', 'tags', 'UPDATE')`).Scan(&tblSel, &tblUpd); err != nil {
		t.Fatalf("read table-wide privileges: %v", err)
	}
	if tblSel || tblUpd {
		t.Fatalf("tappa_app holds table-wide SELECT=%v UPDATE=%v on tags, which would override the "+
			"column grants above (00013/00022's REVOKE undone)", tblSel, tblUpd)
	}

	// (2) POSITIVE CONTROL: the shipped INSERT shape writes app_key_ref, and the
	// owner reads back exactly the envelope -- tappa_app cannot, which is the point.
	uid := randUID(t)
	wrap := fakeAppKeyRef()
	if err := loadWithAppKey(t, app, fx.tenantID, uid, wrap); err != nil {
		t.Fatalf("the shipped INSERT carrying app_key_ref was refused: %v", err)
	}
	owner := ownerDB(t)
	var got []byte
	if err := owner.pool.QueryRow(context.Background(),
		`SELECT app_key_ref FROM tags WHERE uid = $1 AND tenant_id = $2`, uid, fx.tenantID).Scan(&got); err != nil {
		t.Fatalf("owner reads back app_key_ref: %v", err)
	}
	if !bytes.Equal(got, wrap) || len(got) != 44 {
		t.Fatalf("app_key_ref round-tripped to %d bytes, want the 44-byte envelope it was written with", len(got))
	}

	// (3) THE ENVELOPE-SHAPE CHECK BITES. A 16-byte value -- the length of a PLAIN
	// AES-128 key -- is refused (23514), which is the §4.7-integrity point of the
	// CHECK: a plain key is not 44 bytes, so a leak of the raw key into this column
	// does not even land.
	_, err := execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, app_key_ref, status)
		 VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), $3, 'unassigned')`,
		randUID(t), fx.tenantID, bytes.Repeat([]byte{0xAB}, 16))
	wantSQLSTATE(t, err, sqlstateCheckViolation, "INSERT an app_key_ref of plain-key length (16 bytes)")

	// (4) NULL IS ALLOWED -- the pre-step-8 state (a row that has no key 0 yet). The
	// shipped loader omits app_key_ref until FAZ B wires the mint, so this must pass.
	if _, err := execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
		 VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned')`,
		randUID(t), fx.tenantID); err != nil {
		t.Fatalf("INSERT without app_key_ref (NULL, the pre-step-8 state) must be allowed: %v", err)
	}
}
