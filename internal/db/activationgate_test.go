package db

// activationgate_test.go -- the proofs for migration 00025 (M10 Faz 0 F0-6b): the
// trigger tags_active_requires_recorded_encode, which refuses to move a plaque INTO
// `active` unless its encode was recorded (encoded_at set) before the statement.
//
// WHY IT EXISTS: F0-6 put `encoded_at IS NOT NULL` into AssignTagToLocation's own
// WHERE after incident A-1 (an unstamped plaque mounted at Rusty Bar: 12 taps, 0
// valid). That gate binds one statement. The F0-6 security audit named the residue:
// a hand-written UPDATE as tappa_owner, or the next bind-shaped query, never reads
// it. A trigger binds every role and every statement.
//
// Every case below is one row of the table in the migration's header, and each runs
// against REAL POSTGRES (CLAUDE.md §8): a trigger is exactly what a fake database
// cannot have. Fixture helpers (newTagTenant, addPlaque, stampPlaque, execAs,
// wantSQLSTATE, randUID, the SQLSTATE constants) are tagsinventory_test.go's.
//
// THE ASSERTIONS NAME THE SQLSTATE (23001) AND, where two triggers could both
// refuse, the phrase this trigger's message carries -- so a refusal by the wrong
// guard does not pass for this one.
//
// Fixtures are not cleaned up (tappa_app has REVOKE DELETE on tags -- §4.6).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// activationRefusal is a fragment of tappa_forbid_unencoded_activation()'s message.
const activationRefusal = "encode was not recorded"

// wantActivationRefused asserts err is THIS trigger's refusal and that the message
// carries no plaque or tenant identifier (the migration prints OLD.status only).
func wantActivationRefused(t *testing.T, err error, fx tagFixture, uid, what string) {
	t.Helper()
	wantSQLSTATE(t, err, sqlstateRestrictViolation, what)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || !strings.Contains(pg.Message, activationRefusal) {
		t.Fatalf("%s: refused, but not by tags_active_requires_recorded_encode: %v", what, err)
	}
	for _, id := range []string{uid, fx.tenantID.String(), fx.locationID.String()} {
		if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(id)) {
			t.Fatalf("%s: the refusal names %q; it must carry the old status and nothing else\n"+
				"message: %v", what, id, err)
		}
	}
}

// tagState reads (status, location_id, encoded_at-is-set, last_ctr) as tappa_app.
func tagState(t *testing.T, d *DB, fx tagFixture, uid string) (status string, wall *uuid.UUID, stamped bool, ctr int32) {
	t.Helper()
	if err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status, location_id, encoded_at IS NOT NULL, last_ctr
			   FROM tags WHERE tenant_id = $1 AND uid = $2`, fx.tenantID, uid).
			Scan(&status, &wall, &stamped, &ctr)
	}); err != nil {
		t.Fatalf("read plaque %s: %v", uid, err)
	}
	return status, wall, stamped, ctr
}

// rawBind is AssignTagToLocation WITHOUT its `encoded_at IS NOT NULL` predicate --
// the shape of "the next bind query somebody adds", and of a hand-typed repair.
const rawBind = `UPDATE tags SET location_id = $1, status = 'active'
                  WHERE tenant_id = $2 AND uid = $3 AND status = 'unassigned'`

// TestTags00025_AnUnencodedPlaqueCannotGoIntoService: every transition INTO
// `active` on a row whose encode was never recorded is refused for tappa_app, and
// nothing is written.
func TestTags00025_AnUnencodedPlaqueCannotGoIntoService(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	// unassigned -> active: Rusty Bar's own shape before it was mounted.
	stock := randUID(t)
	addPlaque(t, app, fx, stock, uuid.Nil, "unassigned", 0)
	_, err := execAs(t, app, fx.tenantID, rawBind, fx.locationID, fx.tenantID, stock)
	wantActivationRefused(t, err, fx, stock, "binding an unstamped stock plaque (no stamp predicate)")
	if status, wall, stamped, _ := tagState(t, app, fx, stock); status != "unassigned" || wall != nil || stamped {
		t.Fatalf("after the refusal: (%s, %v, stamped=%v), want (unassigned, nil, false) -- 0 UPDATE",
			status, wall, stamped)
	}

	// lost -> active and retired -> active: "found it, put it back" on a plaque whose
	// encode was never recorded is the same act by another door.
	for _, from := range []string{"lost", "retired"} {
		uid := randUID(t)
		addPlaque(t, app, fx, uid, fx.locationID, from, 0)
		_, err := execAs(t, app, fx.tenantID,
			`UPDATE tags SET status = 'active' WHERE tenant_id = $1 AND uid = $2`, fx.tenantID, uid)
		wantActivationRefused(t, err, fx, uid, "returning an unstamped "+from+" plaque to service")
		if status, _, _, _ := tagState(t, app, fx, uid); status != from {
			t.Fatalf("after the refusal the plaque reads %q, want %q", status, from)
		}
	}

	// 🔴 THE STAMP WRITTEN BY THE SAME STATEMENT DOES NOT COUNT. This is the case the
	// migration's WHEN reads OLD.encoded_at for: with a NEW-only condition this one
	// statement forges step 9's record and mounts in one go (measured: it PASSED).
	forged := randUID(t)
	addPlaque(t, app, fx, forged, uuid.Nil, "unassigned", 0)
	_, err = execAs(t, app, fx.tenantID,
		`UPDATE tags SET location_id = $1, status = 'active', encoded_at = now()
		  WHERE tenant_id = $2 AND uid = $3`, fx.locationID, fx.tenantID, forged)
	wantActivationRefused(t, err, fx, forged, "stamping and mounting in ONE statement")
	if status, wall, stamped, _ := tagState(t, app, fx, forged); status != "unassigned" || wall != nil || stamped {
		t.Fatalf("after the refusal: (%s, %v, stamped=%v), want (unassigned, nil, false) -- the "+
			"stamp must roll back with the mount", status, wall, stamped)
	}
}

// TestTags00025_TheOwnerIsBoundToo: tappa_owner is a SUPERUSER, so neither FORCE
// ROW LEVEL SECURITY nor any REVOKE reaches it -- the hand-typed repair at a psql
// prompt that the F0-6 audit named. The trigger does. No tenant context on purpose.
func TestTags00025_TheOwnerIsBoundToo(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	fx := newTagTenant(t, app)

	uid := randUID(t)
	addPlaque(t, app, fx, uid, uuid.Nil, "unassigned", 0)
	_, err := owner.pool.Exec(context.Background(),
		`UPDATE tags SET location_id = $1, status = 'active' WHERE uid = $2`, fx.locationID, uid)
	wantActivationRefused(t, err, fx, uid, "tappa_owner mounting an unstamped plaque")
	if status, wall, _, _ := tagState(t, app, fx, uid); status != "unassigned" || wall != nil {
		t.Fatalf("after the owner's refused mount: (%s, %v), want (unassigned, nil)", status, wall)
	}

	// POSITIVE CONTROL, same role, same statement, once the encode is recorded: the
	// trigger refuses the missing stamp, not the owner.
	stampPlaque(t, app, fx, uid)
	tag, err := owner.pool.Exec(context.Background(),
		`UPDATE tags SET location_id = $1, status = 'active' WHERE uid = $2`, fx.locationID, uid)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("tappa_owner mounting a STAMPED plaque: rows=%d err=%v, want 1/nil",
			tag.RowsAffected(), err)
	}
}

// TestTags00025_AStampedPlaqueStillMounts: the shipped bind (store.AssignTagToLocation)
// and the raw bind both pass once step 9 has stamped the row.
func TestTags00025_AStampedPlaqueStillMounts(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	shipped := randUID(t)
	addPlaque(t, app, fx, shipped, uuid.Nil, "unassigned", 0)
	stampPlaque(t, app, fx, shipped)
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: fx.locationID, TenantID: fx.tenantID, Uid: shipped,
		})
		return e
	}); err != nil {
		t.Fatalf("the shipped bind on a stamped plaque: %v", err)
	}

	raw := randUID(t)
	addPlaque(t, app, fx, raw, uuid.Nil, "unassigned", 0)
	stampPlaque(t, app, fx, raw)
	if n, err := execAs(t, app, fx.tenantID, rawBind, fx.locationID, fx.tenantID, raw); err != nil || n != 1 {
		t.Fatalf("the raw bind on a stamped plaque: rows=%d err=%v, want 1/nil", n, err)
	}

	for _, uid := range []string{shipped, raw} {
		if status, wall, stamped, _ := tagState(t, app, fx, uid); status != "active" || wall == nil || !stamped {
			t.Fatalf("plaque %s after the bind: (%s, %v, stamped=%v), want (active, wall, true)",
				uid, status, wall, stamped)
		}
	}
}

// TestTags00025_AnUnstampedPlaqueAlreadyInServiceKeepsWorking is Rusty Bar's row as
// it stands on production: `active`, no stamp.
//
// 🔴 §4.6 IS THE REASON THE WHEN READS OLD.status. Every tap on this row runs
// AdvanceTagCounter, an UPDATE whose NEW.status is `active`; a condition on NEW alone
// would refuse it and the tap would be lost. The repair paths -- retire it, take it
// off the wall -- must pass too, because they are how the incident is fixed. And
// once it is off the wall it cannot come back without a recorded encode, which is
// F0-6's rule now holding for every statement.
func TestTags00025_AnUnstampedPlaqueAlreadyInServiceKeepsWorking(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)
	fx := newTagTenant(t, app)
	ctx := context.Background()

	// (1) THE TAP: the shipped §4.4 statement, as tappa_app.
	rusty := randUID(t)
	addPlaque(t, app, fx, rusty, fx.locationID, "active", 5)
	if err := app.WithTenant(ctx, fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{
			Ctr: 6, TenantID: fx.tenantID, Uid: rusty,
		})
		return e
	}); err != nil {
		t.Fatalf("a counter advance on an unstamped `active` plaque: %v -- every tap on "+
			"Rusty Bar's plaque would now be lost (§4.6)", err)
	}
	// ... and as the owner, without RLS, in the §4.4 shape (the strict predicate is
	// what redline R4 demands of every counter write, tests included).
	if tag, err := owner.pool.Exec(ctx, `UPDATE tags SET last_ctr = 7 WHERE uid = $1 AND last_ctr < 7`, rusty); err != nil ||
		tag.RowsAffected() != 1 {
		t.Fatalf("owner counter advance on an unstamped `active` plaque: rows=%d err=%v, want 1/nil",
			tag.RowsAffected(), err)
	}
	if status, _, stamped, ctr := tagState(t, app, fx, rusty); status != "active" || stamped || ctr != 7 {
		t.Fatalf("after two advances: (%s, stamped=%v, ctr=%d), want (active, false, 7)", status, stamped, ctr)
	}

	// (2) THE REPAIR BY REPLACEMENT: retire it (the shipped statement). The successor
	// row only needs to exist for the replaced_by FK.
	successor := randUID(t)
	addPlaque(t, app, fx, successor, uuid.Nil, "unassigned", 0)
	if err := app.WithTenant(ctx, fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RetireTagForReplacement(ctx, store.RetireTagForReplacementParams{
			ReplacedBy: successor, TenantID: fx.tenantID, Uid: rusty,
		})
		return e
	}); err != nil {
		t.Fatalf("retiring an unstamped `active` plaque: %v -- the incident's own repair is blocked", err)
	}

	// (3) THE REPAIR BY UNMOUNT, then the way back is shut.
	second := randUID(t)
	addPlaque(t, app, fx, second, fx.locationID, "active", 0)
	if err := app.WithTenant(ctx, fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).UnmountTagFromWall(ctx, store.UnmountTagFromWallParams{
			TenantID: fx.tenantID, Uid: second,
		})
		return e
	}); err != nil {
		t.Fatalf("unmounting an unstamped `active` plaque: %v", err)
	}
	if status, wall, _, _ := tagState(t, app, fx, second); status != "unassigned" || wall != nil {
		t.Fatalf("after the unmount: (%s, %v), want (unassigned, nil)", status, wall)
	}
	_, err := execAs(t, app, fx.tenantID, rawBind, fx.locationID, fx.tenantID, second)
	wantActivationRefused(t, err, fx, second, "re-mounting an unstamped plaque that was just taken down")
}

// TestTags00025_TheShippedMountStillAnswersNoRowsAndTheTriggerCatchesTheRest pins
// how the two layers divide the work, because the domain's error mapping depends on
// it.
//
// The SHIPPED statement never reaches the trigger: its WHERE excludes an unstamped
// row, so it matches nothing and answers pgx.ErrNoRows -- which classifyMount
// (internal/domain/tenant/plaque.go) turns into the refusal sentence and audit row.
// Were the trigger to fire first, the panel would see a 23001 it has no sentence
// for. A statement WITHOUT the predicate, on the same row, is what the trigger
// catches.
func TestTags00025_TheShippedMountStillAnswersNoRowsAndTheTriggerCatchesTheRest(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)
	uid := randUID(t)
	addPlaque(t, app, fx, uid, uuid.Nil, "unassigned", 0)

	err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: fx.locationID, TenantID: fx.tenantID, Uid: uid,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the shipped bind on an unstamped plaque = %v, want pgx.ErrNoRows (its own "+
			"WHERE must refuse first; a 23001 here would reach the panel unmapped)", err)
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		t.Fatalf("the shipped bind raised %s; the trigger must be unreachable from it", pg.Code)
	}

	_, err = execAs(t, app, fx.tenantID, rawBind, fx.locationID, fx.tenantID, uid)
	wantActivationRefused(t, err, fx, uid, "the same bind without the stamp predicate")
}
