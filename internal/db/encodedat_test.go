package db

// encodedat_test.go -- the proofs for migration 00022 and for the two statements
// db/queries/tags.sql grew alongside it: InsertUnassigned (ADR 0017 §5.1 step 3,
// the row written BEFORE the chip's first irreversible command) and
// MarkTagEncoded (step 9, the mark that says the chip took its keys).
//
// 🔴 A NEW FILE RATHER THAN A SECTION OF tagsinventory_test.go, and the choice is
// this package's own pattern rather than a preference. Every migration since
// 00013 has its proofs in a file of its own -- appendonly_truncate_test.go and
// tagkeyshape_test.go and auditindex_test.go are 00021's three parts -- while
// tagsinventory_test.go is 58 kB of 00013 and opens with a header about R4's
// line-locality that has nothing to do with this column. Its FIXTURE HELPERS are
// reused unchanged (newTagTenant, addPlaque, execAs, wantSQLSTATE, randUID and
// the SQLSTATE constants all live there and are package-level), so nothing is
// duplicated; only the prose is separate.
//
// EVERYTHING HERE RUNS AGAINST REAL POSTGRES. A GRANT, a policy and a trigger are
// the three things a fake database cannot have (CLAUDE.md §8), and two of the
// three are what this migration IS.
//
// 🔴 THE ISOLATION TEST CARRIES NO tenant_id FILTER, which is CLAUDE.md §6's own
// instruction and is not an oversight: with a filter, the 0 rows would be the
// WHERE's doing and the test would stay GREEN with row-level security switched
// off. It uses raw SQL through WithTenant rather than the sqlc query for the same
// reason -- every shipped statement carries an explicit tenant predicate (§4.5's
// belt), so a store call could never isolate the policy. The store call is
// exercised separately, in TestTags00022_MarkTagEncodedTouchesNothingInAnotherTenant,
// which is explicitly NOT an isolation proof.
//
// NON-VACUITY WAS MEASURED, NOT ASSUMED (task report, 2026-08-24): with
// `ALTER TABLE tags DISABLE ROW LEVEL SECURITY` in force the isolation test turns
// RED on its first negative assertion, and green again once ENABLE + FORCE are
// restored. A test whose failure has never been seen is a test nobody has proven
// can fail.
//
// Fixtures are not cleaned up, for the reason store_test.go states (tappa_app has
// REVOKE DELETE on tags -- §4.6).

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// sqlstateUniqueViolation is what a second row for one physical chip answers.
// Named rather than matched on message text, for the reason the constants in
// tagsinventory_test.go give: a message can be reworded, a code cannot.
const sqlstateUniqueViolation = "23505" // duplicate key value violates unique constraint

// ---------------------------------------------------------------------------
// RLS ISOLATION -- no tenant predicate anywhere below (CLAUDE.md §6)
// ---------------------------------------------------------------------------

// TestRLS_Tags00022_EncodedMarkerIsIsolatedByTenant.
//
// 00022 adds a column that answers "was this physical chip personalised, and
// when". That is a fact about ONE customer's inventory, and the three statement
// shapes that can reach it must each say so.
//
// READ and UPDATE fail SILENTLY when a policy is wrong (0 rows, no error), so
// each negative assertion is paired with a POSITIVE CONTROL in the owning tenant
// -- otherwise a broken fixture and a working policy look identical. INSERT
// raises instead of matching nothing, so it is asserted on its SQLSTATE.
//
// ⚠️ ONE HONEST LIMIT: this test proves the POLICY covers the new column, which
// it does by covering the row. It cannot prove a column-level rule, because
// PostgreSQL has none -- RLS is per row. What keeps encoded_at from being written
// by the wrong statement is the column GRANT and the write-once trigger, and both
// have their own tests below.
func TestRLS_Tags00022_EncodedMarkerIsIsolatedByTenant(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := newTagTenant(t, app)
	b := newTagTenant(t, app)

	stock := randUID(t)
	addPlaque(t, app, a, stock, uuid.Nil, "unassigned", 0)

	// (1) READ. B must not see A's plaque at all -- and therefore must not learn
	// whether it has been encoded.
	seen := func(ctxTenant uuid.UUID) int {
		t.Helper()
		var n int
		if err := app.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1`, stock).Scan(&n)
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

	// (2) UPDATE -- the statement this migration makes possible. B must not be
	// able to stamp A's plaque as encoded. This is the one that fails quietly: it
	// does not raise, it matches nothing.
	n, err := execAs(t, app, b.tenantID,
		`UPDATE tags SET encoded_at = now() WHERE uid = $1`, stock)
	if err != nil {
		t.Fatalf("B's stamp attempt errored (%v); what is under test is that it matches NOTHING", err)
	}
	if n != 0 {
		t.Fatalf("B stamped %d rows of A's inventory, want 0", n)
	}

	// POSITIVE CONTROL: the same statement, same shape, in A's own context.
	// Without it the 0 above could be a broken statement rather than a working
	// policy.
	if n, err := execAs(t, app, a.tenantID,
		`UPDATE tags SET encoded_at = now() WHERE uid = $1`, stock); err != nil || n != 1 {
		t.Fatalf("A's own stamp: rows=%d err=%v, want 1/nil", n, err)
	}

	// (3) AND B STILL CANNOT READ THE RESULT. A stamped row is exactly the row a
	// cross-tenant reader would find interesting, so the read is re-asserted AFTER
	// the write rather than only before it.
	var marked bool
	if err := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT encoded_at IS NOT NULL FROM tags WHERE uid = $1`, stock).Scan(&marked)
	}); err != nil || !marked {
		t.Fatalf("A reading back its own stamp: marked=%v err=%v, want true/nil", marked, err)
	}
	if got := seen(b.tenantID); got != 0 {
		t.Fatalf("after the stamp B sees %d of A's plaques, want 0", got)
	}

	// (4) INSERT -- B must not be able to load a plaque stamped with A's tenant.
	//
	// 🔴 THE REASON THIS COMMENT USED TO GIVE WAS MEASURABLY FALSE AND TAUGHT THE
	// OPPOSITE OF THE MODEL T16 RESTS ON (audit, 2026-08-24). It said "42501 rather
	// than a policy error: 00013's column grants mean the PRIVILEGE LAYER refuses it
	// before WITH CHECK is consulted". Both halves are wrong:
	//
	//   * THERE ARE NO INSERT COLUMN GRANTS ON `tags`. Measured -- pg_attribute.attacl
	//     carries only `tappa_app=w` (UPDATE, 00013's five plus 00022's encoded_at)
	//     and `tappa_resolver=r`; INSERT is TABLE-WIDE (pg_class.relacl =
	//     `tappa_app=ar`) and has_column_privilege(...,'INSERT') is `t` for ALL TEN
	//     columns, aes_key_ref and encoded_at included.
	//   * THE ERROR IS THE POLICY ITSELF. Measured with VERBOSITY verbose:
	//     `SQLSTATE=42501  MSG=new row violates row-level security policy for table
	//     "tags"`. 42501 is insufficient_privilege, and RLS raises it too -- the code
	//     does not distinguish the two layers, so it cannot be read as evidence for
	//     either.
	//
	// It also contradicted migration 00022's own paragraph in the SAME change set
	// ("THE HONEST WEAK POINT ... tappa_app holds TABLE-WIDE INSERT on `tags`,
	// aes_key_ref included"), which is what makes it worth this much text: a green
	// test that teaches a wrong privilege model is worse than a missing one.
	//
	// WHAT IS ACTUALLY ASSERTED: a cross-tenant load is refused, loudly, by RLS's
	// WITH CHECK -- and the SQLSTATE is pinned rather than the message, because a
	// message can be reworded and a code cannot.
	//
	// ⚠️ THE STATEMENT NO LONGER CARRIES encoded_at, AND THE CHANGE IS LOAD-BEARING
	// RATHER THAN COSMETIC. It used to insert `..., now()` while asserting the
	// TENANT rule, which conflated two refusals; once 00022 grew
	// tags_encoded_at_not_settable_at_insert, the BEFORE INSERT trigger fires FIRST
	// and this case started answering restrict_violation -- a green-to-red that
	// proved the assertion had never been about the tenant at all. The stamp-at-
	// INSERT rule now has its own case below.
	_, err = execAs(t, app, b.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
		 VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned')`,
		randUID(t), a.tenantID)
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "B loading a plaque into A's tenant")
}

// TestTags00022_ARowIsBornUnstampedAndNoCallerMaySayOtherwise.
//
// 🔴 THE COLUMN COMMENT USED TO CLAIM THIS AND THE SCHEMA DID NOT ENFORCE IT (audit,
// 2026-08-24). It said "Server-side now() only, and write-once", which was true of
// the shipped UPDATE and false of the COLUMN: the guard was BEFORE UPDATE, so
// nothing watched INSERT, and tappa_app -- which holds TABLE-WIDE INSERT, measured
// -- could load a plaque with a fabricated stamp:
//
//	INSERT INTO tags (..., encoded_at) VALUES (..., '1999-01-01 00:00:00+00')  ACCEPTED
//
// That matters because encoded_at is the one column whose entire job is to say when
// a PHYSICAL CHIP was personalised. A row that arrives already stamped is a claim
// about silicon nobody touched, and migration 00022's own Down comment leans on it
// ("every record of which plaques were confirmed personalised").
//
// ⚠️ THE ONE-WORD FIX WAS MEASURED IMPOSSIBLE: widening the write-once trigger to
// BEFORE INSERT OR UPDATE answers "INSERT trigger's WHEN condition cannot reference
// OLD values". The rules are different shapes, so they are two triggers.
//
// EVERY ARM HAS ITS POSITIVE CONTROL, because a guard that refuses everything and a
// guard that refuses the right thing fail this test identically.
func TestTags00022_ARowIsBornUnstampedAndNoCallerMaySayOtherwise(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	own := newTagTenant(t, app)

	// (1) THE VIOLATION: an INSERT that stamps.
	_, err := execAs(t, app, own.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status, encoded_at)
		 VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned', $3)`,
		randUID(t), own.tenantID, time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC))
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "an INSERT carrying a fabricated encoded_at")

	// (2) now() IS NO MORE ACCEPTABLE THAN 1999. The rule is "born unstamped", not
	// "born with a plausible stamp" -- a caller that can pass now() can pass
	// anything, and the trigger has no way to tell the two apart.
	_, err = execAs(t, app, own.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status, encoded_at)
		 VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned', now())`,
		randUID(t), own.tenantID)
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "an INSERT stamping with now()")

	// (3) POSITIVE CONTROL: the SHIPPED shape loads, and the SHIPPED update stamps.
	uid := randUID(t)
	if _, err := execAs(t, app, own.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
		 VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned')`,
		uid, own.tenantID); err != nil {
		t.Fatalf("the shipped INSERT was refused: %v", err)
	}
	if _, err := execAs(t, app, own.tenantID,
		`UPDATE tags SET encoded_at = coalesce(encoded_at, now()) WHERE uid = $1 AND tenant_id = $2`,
		uid, own.tenantID); err != nil {
		t.Fatalf("the shipped UPDATE was refused: %v", err)
	}
	var stamped bool
	if err := app.WithTenant(context.Background(), own.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT encoded_at IS NOT NULL FROM tags WHERE uid = $1`, uid).Scan(&stamped)
	}); err != nil || !stamped {
		t.Fatalf("the marker did not land: stamped=%v err=%v", stamped, err)
	}

	// (4) AND AN UPDATE THAT DOES NOT TOUCH THE COLUMN IS UNAFFECTED -- the INSERT
	// guard must not have become a general brake on the table.
	if _, err := execAs(t, app, own.tenantID,
		`UPDATE tags SET status = 'lost' WHERE uid = $1 AND tenant_id = $2`, uid, own.tenantID); err != nil {
		t.Fatalf("an unrelated UPDATE was refused: %v", err)
	}
}

// ---------------------------------------------------------------------------
// THE WRITE-ONCE GUARD
// ---------------------------------------------------------------------------

// TestTags00022_EncodedAtIsWriteOnceForEveryRoleIncludingTheOwner.
//
// A column grant says WHICH column may be written and never HOW MANY TIMES. For
// this column the dangerous value is a SECOND one: encoded_at answers "when did
// this physical chip get its keys", a chip is personalised once, and a second
// timestamp is a claim about an event that did not happen written over the record
// of one that did.
//
// All four transitions are driven, because the guard's whole content is which
// two of them pass:
//
//	NULL  -> value ....... PASSES (the one write the flow performs)
//	value -> SAME value .. PASSES (a retry is not a rewrite -- 00011's BOUNDARY 2)
//	value -> other value . REFUSED
//	value -> NULL ........ REFUSED
//
// 🔴 THE LAST ONE IS WHY THE `WHEN` USES `IS DISTINCT FROM` AND NOT `<>`. The
// column is NULLABLE, so a plain inequality against NULL evaluates to NULL, the
// WHEN would not fire, and un-marking an encoded plaque would be silently
// allowed. 00013's counter trigger can use a plain `<` precisely because both of
// its operands are NOT NULL; copying that shape here would have left the hole.
//
// The owner half is the same belt 00005 puts over transactions and 00013 over the
// counter: a REVOKE cannot reach tappa_owner (it is a SUPERUSER, so FORCE ROW
// LEVEL SECURITY does not bind it either -- M0-03), and a trigger can. It is
// defence in depth, not an absolute: a superuser can still DISABLE the trigger,
// which the migration says and this test does not claim otherwise.
func TestTags00022_EncodedAtIsWriteOnceForEveryRoleIncludingTheOwner(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	uid := randUID(t)
	addPlaque(t, app, fx, uid, uuid.Nil, "unassigned", 0)

	// (a) NULL -> value.
	if n, err := execAs(t, app, fx.tenantID,
		`UPDATE tags SET encoded_at = now() WHERE uid = $1 AND tenant_id = $2`,
		uid, fx.tenantID); err != nil || n != 1 {
		t.Fatalf("first stamp: rows=%d err=%v, want 1/nil", n, err)
	}

	var stamped time.Time
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT encoded_at FROM tags WHERE uid = $1 AND tenant_id = $2`, uid, fx.tenantID).Scan(&stamped)
	}); err != nil {
		t.Fatalf("read back the stamp: %v", err)
	}

	// (b) value -> SAME value. Refusing this would turn a harmless retry into a
	// failed transaction, and MarkTagEncoded's coalesce() lands here on every
	// second call.
	if n, err := execAs(t, app, fx.tenantID,
		`UPDATE tags SET encoded_at = $3 WHERE uid = $1 AND tenant_id = $2`,
		uid, fx.tenantID, stamped); err != nil || n != 1 {
		t.Fatalf("idempotent re-stamp with the SAME value: rows=%d err=%v, want 1/nil\n"+
			"a guard that fires on a duplicate makes the caller report failure for work "+
			"that already succeeded (00011's BOUNDARY 2)", n, err)
	}

	// (c) value -> OTHER value.
	_, err := execAs(t, app, fx.tenantID,
		`UPDATE tags SET encoded_at = $3 WHERE uid = $1 AND tenant_id = $2`,
		uid, fx.tenantID, stamped.Add(time.Hour))
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "re-stamp encoded_at with a different time")
	if !strings.Contains(err.Error(), "write-once") {
		t.Fatalf("refused by something other than the write-once trigger: %v", err)
	}

	// 🔴 AND THE MESSAGE CARRIES NO ROW VALUE BEYOND THE TWO TIMES. 00021 Part 2
	// recorded that a CHECK violation's DETAIL line is the WHOLE failing tuple and
	// that on this table the tuple holds aes_key_ref (§4.7). A RAISE has no DETAIL,
	// so this guard prints only what it is given -- and it is given two timestamps.
	// Asserted rather than trusted, because the cheapest way to make this message
	// more helpful is to add the uid to it.
	//
	// ⚠️ ONE QUALIFIER ON THE PREMISE (audit, 2026-08-24): that DETAIL channel is
	// real for tappa_owner and NOT for tappa_app. Measured on two roles, with
	// `employees` as a control -- a role that is neither rolsuper nor rolbypassrls
	// gets no DETAIL at all under RLS. It does not weaken this assertion, which is
	// about what the RAISE itself formats, and it is noted so the citation does not
	// carry more than 00021 measured.
	for _, secret := range []string{uid, fx.tenantID.String(), "dead"} {
		if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(secret)) {
			t.Fatalf("the write-once message names %q; it must carry the two timestamps and "+
				"nothing else.\nmessage: %v", secret, err)
		}
	}

	// (d) value -> NULL. The case a `<>` condition would let through.
	_, err = execAs(t, app, fx.tenantID,
		`UPDATE tags SET encoded_at = NULL WHERE uid = $1 AND tenant_id = $2`, uid, fx.tenantID)
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "un-stamp encoded_at back to NULL")

	// (e) AN UNRELATED UPDATE STILL PASSES. The WHEN carries the whole condition,
	// so a bind, an unbind, a retire or a counter advance never enters the
	// function -- and if this assertion ever fails, every panel write on an
	// encoded plaque is broken.
	if n, err := execAs(t, app, fx.tenantID,
		`UPDATE tags SET status = 'lost' WHERE uid = $1 AND tenant_id = $2`,
		uid, fx.tenantID); err != nil || n != 1 {
		t.Fatalf("unrelated UPDATE on a stamped row: rows=%d err=%v, want 1/nil", n, err)
	}

	// (f) THE OWNER IS BOUND TOO. No tenant context: tappa_owner bypasses RLS,
	// which is exactly why the trigger has to be the thing that stops it.
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	_, err = owner.pool.Exec(context.Background(),
		`UPDATE tags SET encoded_at = $2 WHERE uid = $1`, uid, stamped.Add(2*time.Hour))
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "re-stamp encoded_at as tappa_owner")
}

// TestTags00022_TheAppMayStampTheMarkerAndStillNotTheFourProtectedColumns is the
// mechanical gate on 00022's GRANT.
//
// 🔴 IT EXISTS BECAUSE ADDING A COLUMN IS EXACTLY THE CHANGE THAT WIDENS AN ACL
// BY ACCIDENT. 00013 had to REVOKE table-wide UPDATE before it could grant five
// columns, and its own DIKKAT note says a narrowing that skips the REVOKE does
// nothing. This migration takes the opposite step -- an ADDITIVE sixth column on
// an already-narrowed set -- and the failure it could produce is silent: a
// mis-written REVOKE/GRANT pair here would restore table-wide UPDATE and hand the
// HTTP-facing role aes_key_ref and uid back, with every existing test still green.
//
// So the privilege set is read from the catalog and compared as a WHOLE, not
// probed one column at a time.
func TestTags00022_TheAppMayStampTheMarkerAndStillNotTheFourProtectedColumns(t *testing.T) {
	app := appDB(t)

	var updatable string
	if err := app.pool.QueryRow(context.Background(),
		`SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attnum), '')
		   FROM pg_attribute a
		  WHERE a.attrelid = 'tags'::regclass AND a.attnum > 0 AND NOT a.attisdropped
		    AND has_column_privilege('tappa_app', 'tags', a.attname, 'UPDATE')`,
	).Scan(&updatable); err != nil {
		t.Fatalf("read tappa_app's column privileges on tags: %v", err)
	}
	const want = "location_id,last_ctr,status,retired_at,replaced_by,encoded_at"
	if updatable != want {
		t.Fatalf("tappa_app may UPDATE (%s)\nwant                      (%s)\n"+
			"00013's five plus 00022's encoded_at, and NOTHING else -- the four that must "+
			"never move are uid, tenant_id, aes_key_ref and created_at", updatable, want)
	}

	// AND TABLE-WIDE UPDATE MUST STILL BE GONE. A table-level privilege OVERRIDES
	// the column list, so the check above can read correct while the protection is
	// off -- measured in scripts/db-init/01-roles.sql's own restore experiment,
	// where a pg_dump reload handed tappa_app tags.aes_key_ref UPDATE = true.
	var tableWide bool
	if err := app.pool.QueryRow(context.Background(),
		`SELECT has_table_privilege('tappa_app', 'tags', 'UPDATE')`).Scan(&tableWide); err != nil {
		t.Fatalf("read table-wide UPDATE: %v", err)
	}
	if tableWide {
		t.Fatal("tappa_app holds TABLE-WIDE UPDATE on tags, which overrides every column " +
			"grant above -- 00013's REVOKE has been undone")
	}
}

// ---------------------------------------------------------------------------
// THE TWO SHIPPED STATEMENTS
// ---------------------------------------------------------------------------

// TestTags00022_InsertUnassignedLoadsStockThatNoTapCanUse drives the FIRST INSERT
// over `tags` in db/queries (measured: `grep -c "INSERT INTO tags" db/queries/*.sql`
// was 0 on all seventeen files before this change).
//
// It asserts the three properties the row has to be born with, because each one
// is a different way for a boxed plaque to look like a working one:
//
//	status = 'unassigned' .. 'active' would show a plaque still in its box as in
//	                         service, AND would break 00013's
//	                         tags_active_requires_location
//	location_id IS NULL .... 00013's tags_unassigned_has_no_location demands it,
//	                         and CLAUDE.md §5 row 1 rejects a tap on this status
//	encoded_at IS NULL ..... ADR 0017 §5.2: the row is written BEFORE the chip's
//	                         first irreversible command, so at this instant "the
//	                         row exists" means "we intended to encode this"
func TestTags00022_InsertUnassignedLoadsStockThatNoTapCanUse(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	uid := randUID(t)
	var got store.InsertUnassignedRow
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		got, e = store.New(tx).InsertUnassigned(ctx, store.InsertUnassignedParams{
			Uid: uid, TenantID: fx.tenantID, AesKeyRef: make([]byte, 44),
		})
		return e
	}); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if got.Uid != uid {
		t.Fatalf("RETURNING gave uid %q, want %q", got.Uid, uid)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("created_at came back zero; the row's own timestamp is the server's clock and must be real")
	}

	var status string
	var location *uuid.UUID
	var encodedAt *time.Time
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status, location_id, encoded_at FROM tags WHERE uid = $1 AND tenant_id = $2`,
			uid, fx.tenantID).Scan(&status, &location, &encodedAt)
	}); err != nil {
		t.Fatalf("read back the loaded row: %v", err)
	}
	if status != "unassigned" {
		t.Fatalf("status = %q, want unassigned -- an active row shows a boxed plaque as in service", status)
	}
	if location != nil {
		t.Fatalf("location_id = %v, want NULL -- a freshly encoded plaque is not on anybody's wall", location)
	}
	if encodedAt != nil {
		t.Fatalf("encoded_at = %v, want NULL -- the row is written BEFORE the chip's first "+
			"irreversible command (ADR 0017 §5.2), so it cannot claim the chip is encoded", encodedAt)
	}
}

// TestTags00022_InsertUnassignedRefusesADuplicateUIDRatherThanOverwriting.
//
// 🔴 THE ALTERNATIVE IS NOT "HARMLESS IDEMPOTENCE", WHICH IS WHY THIS IS ITS OWN
// TEST. tags.uid is a GLOBAL primary key and it is PUBLIC -- printed on the
// plaque, carried in the tap URL -- and ADR 0017 §5.1 takes the uid FROM THE PHONE
// while §2.2 says to assume the phone is compromised. An upsert would therefore
// let one encode round overwrite the aes_key_ref of an EXISTING plaque, which
// would brick a chip already on a wall: the row would hold a key the chip does not
// have, every tap would fail CMAC verification, and no statement would report
// anything wrong. Rows.InsertUnassigned's contract says it "must fail rather than
// overwrite" and this is the gate on that sentence.
//
// ⚠️ WHAT IT DOES NOT CLOSE, named because ADR 0017 §6 md. 12 counts it: 23505 on
// a uid the caller cannot SELECT is a cross-tenant EXISTENCE ORACLE (RLS hides the
// row, the primary key reveals that it exists). The mitigation is authorisation
// and a rate limit at the endpoint, not this statement -- and the only cleanup for
// an occupied uid today is tappa_owner by hand.
func TestTags00022_InsertUnassignedRefusesADuplicateUIDRatherThanOverwriting(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	a := newTagTenant(t, app)
	b := newTagTenant(t, app)

	uid := randUID(t)
	first := make([]byte, 44)
	first[0] = 0x11
	second := make([]byte, 44)
	second[0] = 0x22

	load := func(fx tagFixture, ref []byte) error {
		return app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := store.New(tx).InsertUnassigned(ctx, store.InsertUnassignedParams{
				Uid: uid, TenantID: fx.tenantID, AesKeyRef: ref,
			})
			return e
		})
	}
	if err := load(a, first); err != nil {
		t.Fatalf("first load: %v", err)
	}

	// (1) The SAME tenant, again.
	wantSQLSTATE(t, load(a, second), sqlstateUniqueViolation, "re-loading a uid this tenant already holds")

	// (2) ANOTHER tenant -- the §6 md. 12 axis. It must fail too, and for the same
	// structural reason: the primary key is global.
	wantSQLSTATE(t, load(b, second), sqlstateUniqueViolation, "loading a uid another tenant already holds")

	// (3) AND THE KEY WAS NOT TOUCHED. This is the assertion that would catch an
	// upsert: the failures above could be reported by anything, but a surviving
	// first envelope proves nothing overwrote it.
	// 🔴 READ AS tappa_owner, AND THE CHANGE IS THE PROOF THAT 00022 BIT. This used
	// to read through the application pool; after the migration revoked
	// tags.aes_key_ref from tappa_app's SELECT privilege it answers `permission
	// denied for table tags (SQLSTATE 42501)` -- measured. The assertion is about a
	// fact the PRODUCT may no longer see, so it reads it the way the rotatekek
	// runbook does. Skips loudly rather than falling back, for role_test.go's reason.
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; this assertion reads aes_key_ref, which 00022 " +
			"revoked from tappa_app")
	}
	oc, err := pgx.Connect(context.Background(), ownerDSN)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	defer func() { _ = oc.Close(context.Background()) }()
	var ref []byte
	if err := oc.QueryRow(context.Background(),
		`SELECT aes_key_ref FROM tags WHERE uid = $1 AND tenant_id = $2`, uid, a.tenantID).Scan(&ref); err != nil {
		t.Fatalf("read back aes_key_ref as the owner: %v", err)
	}
	if len(ref) != 44 || ref[0] != 0x11 {
		t.Fatalf("aes_key_ref is %d bytes starting %#x; want the FIRST envelope (44 bytes, 0x11). "+
			"A rewritten key ref means the chip on the wall can never be verified again", len(ref), ref[0])
	}
}

// TestTags00022_MarkTagEncodedIsIdempotentAndStampsTheServerClock.
//
// Two properties in one test because they are one design decision -- the value is
// `coalesce(encoded_at, now())` and each half of that expression carries one:
//
//  1. IDEMPOTENT. A second call returns the FIRST timestamp, not a new one, so
//     the answer to "when was this chip personalised" cannot drift because a
//     button was pressed twice or a proxy replayed a request. It is also the one
//     UPDATE the write-once trigger permits without firing.
//  2. THE SERVER'S CLOCK. now() comes from the database, never from the caller --
//     the same rule audit_log.at and transactions.created_at follow, and the
//     reason is not stylistic: ADR 0017 §2.2 says to assume the phone driving this
//     flow is compromised, and a caller-supplied time would let it backdate the
//     record. The parameter list is the gate that makes this structural: there is
//     no timestamp parameter to pass.
func TestTags00022_MarkTagEncodedIsIdempotentAndStampsTheServerClock(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	uid := randUID(t)
	addPlaque(t, app, fx, uid, uuid.Nil, "unassigned", 0)

	mark := func() store.MarkTagEncodedRow {
		t.Helper()
		var row store.MarkTagEncodedRow
		if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			var e error
			row, e = store.New(tx).MarkTagEncoded(ctx, store.MarkTagEncodedParams{
				Uid: uid, TenantID: fx.tenantID,
			})
			return e
		}); err != nil {
			t.Fatalf("MarkTagEncoded: %v", err)
		}
		return row
	}

	// The server's own reading, taken through the same pool, so the comparison is
	// against the database's clock and not this machine's.
	var before time.Time
	if err := app.pool.QueryRow(context.Background(), `SELECT now()`).Scan(&before); err != nil {
		t.Fatalf("read the server clock: %v", err)
	}

	first := mark()
	if first.EncodedAt == nil {
		t.Fatal("MarkTagEncoded returned a NULL encoded_at; the statement's whole job is to set it")
	}
	if first.EncodedAt.Before(before) {
		t.Fatalf("encoded_at %v is BEFORE the server's own now() %v -- the stamp is not the "+
			"database's clock", *first.EncodedAt, before)
	}

	// The retry. Same statement, same parameters, and it must answer with the same
	// instant rather than a fresh one.
	second := mark()
	if second.EncodedAt == nil || !second.EncodedAt.Equal(*first.EncodedAt) {
		t.Fatalf("the retry returned %v, want the ORIGINAL %v -- coalesce() is what keeps "+
			"'when was this chip personalised' from drifting on a replayed request",
			second.EncodedAt, *first.EncodedAt)
	}

	// And the row agrees with what was returned.
	var stored time.Time
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT encoded_at FROM tags WHERE uid = $1 AND tenant_id = $2`, uid, fx.tenantID).Scan(&stored)
	}); err != nil {
		t.Fatalf("read back encoded_at: %v", err)
	}
	if !stored.Equal(*first.EncodedAt) {
		t.Fatalf("stored %v, returned %v -- RETURNING and the row disagree", stored, *first.EncodedAt)
	}
}

// TestTags00022_MarkTagEncodedTouchesNothingInAnotherTenant.
//
// The SHIPPED statement, run for a uid that belongs to somebody else. It carries
// an explicit tenant predicate (§4.5's belt) AND runs under the policy (RLS's
// braces), so this is not an isolation proof -- the isolation proof is the raw,
// unfiltered probe at the top of this file. What it proves is the CALLER-VISIBLE
// contract: zero rows, surfaced as pgx.ErrNoRows.
//
// 🔴 AND THAT ErrNoRows HAS EXACTLY ONE MEANING, which is why the statement does
// NOT carry `AND encoded_at IS NULL`. With that predicate a retry and a
// cross-tenant uid would be indistinguishable, and a caller that treated
// ErrNoRows as "fine, already done" would swallow the second case in silence.
// Here a retry SUCCEEDS (the test above) and ErrNoRows means only "no such plaque
// in THIS tenant".
func TestTags00022_MarkTagEncodedTouchesNothingInAnotherTenant(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	a := newTagTenant(t, app)
	b := newTagTenant(t, app)

	uid := randUID(t)
	addPlaque(t, app, b, uid, uuid.Nil, "unassigned", 0)

	// A asks for B's plaque.
	err := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).MarkTagEncoded(ctx, store.MarkTagEncodedParams{Uid: uid, TenantID: a.tenantID})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("A stamping B's plaque returned %v, want pgx.ErrNoRows", err)
	}

	// 🔴 AND THE ROW IS UNTOUCHED, asserted in B's own context. "It returned an
	// error" and "it changed nothing" are different claims, and only the second one
	// is what §4.5 is about.
	var encodedAt *time.Time
	if err := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT encoded_at FROM tags WHERE uid = $1 AND tenant_id = $2`, uid, b.tenantID).Scan(&encodedAt)
	}); err != nil {
		t.Fatalf("read B's plaque: %v", err)
	}
	if encodedAt != nil {
		t.Fatalf("B's plaque came back stamped %v after A's attempt, want NULL", *encodedAt)
	}

	// POSITIVE CONTROL: B can stamp its own. Without it the ErrNoRows above could
	// be a broken statement rather than a working scope.
	if err := app.WithTenant(context.Background(), b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).MarkTagEncoded(ctx, store.MarkTagEncodedParams{Uid: uid, TenantID: b.tenantID})
		if e == nil && row.EncodedAt == nil {
			t.Error("B's own stamp returned a NULL encoded_at")
		}
		return e
	}); err != nil {
		t.Fatalf("B stamping its own plaque: %v", err)
	}
}
