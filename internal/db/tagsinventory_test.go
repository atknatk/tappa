package db

// tagsinventory_test.go -- the proofs for migration 00013: the canonical uid
// (backlog T8), the column-scoped write grant plus the monotonic read counter
// (backlog T9), and the inventory state a plaque needs before it is mounted
// (user decision 2026-08-08).
//
// EVERYTHING HERE RUNS AS tappa_app AGAINST REAL POSTGRES. A privilege, a CHECK
// and a trigger are the three things a fake database cannot have; asserting them
// anywhere but against the real server would be asserting the test's own opinion
// (CLAUDE.md section 8).
//
// 🔴 THE ISOLATION PROBES AT THE BOTTOM CARRY NO tenant_id FILTER, and that is the
// design (CLAUDE.md section 6). The shipped statements in db/queries/tags.sql all
// carry one -- section 4.5, belt and braces -- but an isolation probe that carries
// one proves nothing: the 0 would be the WHERE's doing and the test would stay
// green with row-level security switched off. Every negative assertion is paired
// with a positive control in the owning tenant, or a broken fixture would look
// like a working policy.
//
// Fixtures are not cleaned up, for the reason store_test.go states (tappa_app has
// REVOKE DELETE on tags -- section 4.6).
//
// ⚠️ A LIMIT OF `make audit` THAT THIS FILE MADE VISIBLE, RECORDED HERE BECAUSE
// NOTHING ELSE RECORDS IT. R4 forbids an unconditional write to the tag counter
// column (a TOCTOU replay hole) and it is a LINE-LOCAL grep: it reads one line at a
// time, so the same statement split across lines is invisible to it. Measured, in
// PRODUCTION locations, not just here:
//
// (This paragraph is worded to describe the pattern rather than spell it, because
// R4 scans comments too -- a note quoting the forbidden shape turns the net red on
// the text that explains it. M6-06 phase A hit the same trap on R2 and fixed it the
// same way: reword the prose, never loosen the net.)
//
//	one line, db/queries/tags.sql ................. rc=1, FLAGGED
//	one line, a Go const in internal/db ........... rc=1, FLAGGED
//	`UPDATE tags t` / `SET last_ctr = @ctr` / ..... rc=0, SILENT
//	the same split inside a Go raw string ......... rc=0, SILENT
//
// The third shape is `AdvanceTagCounter`'s OWN formatting, i.e. the product's most
// safety-critical statement is written in the shape the net cannot read. The rule
// still holds -- what makes the advance safe is its strict `prev.old_ctr < @ctr`
// predicate and the concurrency tests over it, never this grep -- but "R4 would
// catch it" is a sentence nobody should write without this paragraph.
//
// The three counter probes below are therefore written multi-line, in the repo's own
// SQL style. An earlier version of this task wrote them on one line and LOOSENED R4
// with a `_test.go` exemption instead; that was reverted. A red-line net relaxed for
// a test's convenience is a relaxed net, and R4's real gap is the line-locality
// above, which an exemption would have hidden rather than exposed.
//
// ("counter probes", not "rewind probes" -- an earlier wording called all three
// rewinds and only two are. The middle one writes 501 over 501, which is an
// IDEMPOTENT REWRITE and is asserted to SUCCEED; calling it a rewind would have the
// reader expect the opposite verdict from the one the test demands.)

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

// SQLSTATEs the assertions below name rather than match on message text: a
// message can be reworded, a code cannot.
const (
	sqlstateCheckViolation      = "23514" // a CHECK refused the row
	sqlstateInsufficientPrivi   = "42501" // permission denied
	sqlstateRestrictViolation   = "23001" // RAISE ... USING ERRCODE = 'restrict_violation'
	sqlstateForeignKeyViolation = "23503"
)

// tagFixture is one tenant with one location and, optionally, one plaque.
type tagFixture struct {
	tenantID   uuid.UUID
	locationID uuid.UUID
}

// newTagTenant commits a tenant + location as tappa_app inside its own context.
func newTagTenant(t *testing.T, d *DB) tagFixture {
	t.Helper()
	fx := tagFixture{tenantID: uuid.New(), locationID: uuid.New()}
	if err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'tags-00013', $2, 'bar', 'single')`,
			fx.tenantID, "VAT-"+fx.tenantID.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
			 VALUES ($1, $2, 'loc', 35.899, 14.514)`,
			fx.locationID, fx.tenantID)
		return e
	}); err != nil {
		t.Fatalf("newTagTenant: %v", err)
	}
	return fx
}

// addPlaque inserts one plaque in fx's context. location may be uuid.Nil to mean
// "unassigned stock" (location_id NULL), in which case status must be
// 'unassigned' -- the CHECK pair added by 00013.
func addPlaque(t *testing.T, d *DB, fx tagFixture, uid string, location uuid.UUID, status string, ctr int32) {
	t.Helper()
	var loc any
	if location != uuid.Nil {
		loc = location
	}
	if err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, '\xDEAD', $4, $5)`,
			uid, fx.tenantID, loc, ctr, status)
		return e
	}); err != nil {
		t.Fatalf("addPlaque(%s, %s): %v", uid, status, err)
	}
}

// execAs runs one statement in a tenant context and returns the raw error, so a
// caller can assert on its SQLSTATE. Rows affected comes back too, because the
// three failure shapes differ: an INSERT that breaks a policy RAISES, an UPDATE
// that breaks one matches NOTHING.
func execAs(t *testing.T, d *DB, tenant uuid.UUID, sql string, args ...any) (int64, error) {
	t.Helper()
	var n int64
	err := d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, sql, args...)
		if e != nil {
			return e
		}
		n = tag.RowsAffected()
		return nil
	})
	return n, err
}

// wantSQLSTATE fails unless err is a Postgres error with exactly this code.
func wantSQLSTATE(t *testing.T, err error, code, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: statement SUCCEEDED, want SQLSTATE %s", what, code)
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		t.Fatalf("%s: error is not a PgError (%T: %v), want SQLSTATE %s", what, err, err, code)
	}
	if pg.Code != code {
		t.Fatalf("%s: SQLSTATE = %s (%s), want %s", what, pg.Code, pg.Message, code)
	}
}

// ---------------------------------------------------------------------------
// T8 -- ONE SPELLING PER PLAQUE
// ---------------------------------------------------------------------------

// TestTags00013_UIDHasOneCanonicalSpelling.
//
// 🔴 WHY THIS IS NOT COSMETIC. hex.DecodeString is case-insensitive and the GCM
// AAD of a wrapped key is the RAW 7 BYTES (internal/sun/keys.go), so before 00013
// '04AC7E55000601' and '04ac7e55000601' were two DIFFERENT primary key rows with
// the SAME AAD: one plaque's envelope opened on the other's row and the two rows
// carried INDEPENDENT last_ctr values -- two independent replay windows for one
// physical chip. The latent cost was a DEAD PLAQUE (M8-05): an operator types the
// spelling printed on the plaque, the row is accepted, and no tap ever arrives.
//
// The mixed-case case is here on purpose: a constraint written as "not all lower"
// rather than "all upper" would pass it.
func TestTags00013_UIDHasOneCanonicalSpelling(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	// ⚠️ THE LAST TWO DIGITS ARE FORCED TO LETTERS, AND WITHOUT THAT THIS TEST IS
	// FLAKY IN THE DIRECTION THAT MATTERS. A random 14-hex uid is all DIGITS about
	// once in 280 runs ((10/16)^14), and for such a uid ToLower is a no-op -- so
	// the two negative cases would silently become the positive one and the test
	// would fail while the schema was correct. Forcing 'AB' makes all three
	// spellings genuinely different strings.
	base := randUID(t)[:12] + "AB"
	if base != strings.ToUpper(base) {
		t.Fatalf("randUID returned %q: the fixture helper itself must emit the canonical spelling", base)
	}

	// The three spellings are of the SAME uid on purpose: that is T8's scenario
	// exactly. Before 00013 the second and third would have been accepted as
	// SEPARATE primary key rows carrying the SAME wrapped-key AAD. They are
	// different strings, so a rejection below can only come from the CHECK -- never
	// from a primary key clash.
	for _, tc := range []struct {
		name string
		uid  string
		ok   bool
	}{
		{"canonical upper case", base, true},
		{"lower case", strings.ToLower(base), false},
		{"mixed case", base[:13] + "b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid := tc.uid
			_, err := execAs(t, app, fx.tenantID,
				`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref)
				 VALUES ($1, $2, $3, '\xDEAD')`,
				uid, fx.tenantID, fx.locationID)
			if tc.ok {
				if err != nil {
					t.Fatalf("canonical uid %q was REFUSED: %v", uid, err)
				}
				return
			}
			wantSQLSTATE(t, err, sqlstateCheckViolation, "non-canonical uid "+uid)
			if !strings.Contains(err.Error(), "tags_uid_canonical_hex") {
				t.Fatalf("refused by the wrong constraint: %v\n"+
					"want tags_uid_canonical_hex -- 00004's tags_uid_check accepts both cases, "+
					"so a rejection from it would mean 00013's constraint is absent", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T9 -- WHAT tappa_app MAY AND MAY NOT WRITE
// ---------------------------------------------------------------------------

// TestTags00013_AppMayNotWriteTheFourProtectedColumns.
//
// Measured before 00013: tappa_app held UPDATE on ALL NINE columns. Two of those
// are silent denial of service (rewrite aes_key_ref and every tap on the plaque
// answers 500; rename uid and the URL printed on the wall resolves to nothing),
// and tenant_id was held back only by the RLS WITH CHECK. After 00013 all four
// fail at the PRIVILEGE layer, which fails earlier and more loudly.
func TestTags00013_AppMayNotWriteTheFourProtectedColumns(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)
	uid := randUID(t)
	addPlaque(t, app, fx, uid, fx.locationID, "active", 0)

	for _, tc := range []struct {
		name string
		sql  string
	}{
		// 🔴 The wrapped key. T9's first measured capability.
		{"aes_key_ref", `UPDATE tags SET aes_key_ref = '\xBEEF' WHERE uid = $1 AND tenant_id = $2`},
		// 🔴 The identity printed on the wall.
		{"uid", `UPDATE tags SET uid = 'FFFFFFFFFFFFFF' WHERE uid = $1 AND tenant_id = $2`},
		// 🔴 The scope key. Previously refused by RLS; now refused before that.
		{"tenant_id", `UPDATE tags SET tenant_id = gen_random_uuid() WHERE uid = $1 AND tenant_id = $2`},
		// The load timestamp: an audit fact, not application state.
		{"created_at", `UPDATE tags SET created_at = now() WHERE uid = $1 AND tenant_id = $2`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execAs(t, app, fx.tenantID, tc.sql, uid, fx.tenantID)
			wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "UPDATE tags."+tc.name)
		})
	}
}

// TestTags00013_CounterIsMonotonicButRetireIsNot.
//
// 🔴 THE TRAP THIS TEST EXISTS FOR. Backlog T9 proposes the trigger condition
// `NEW.last_ctr > OLD.last_ctr`. As a REQUIREMENT that is wrong: retiring a
// plaque writes status, retired_at and replaced_by and leaves last_ctr ALONE, so
// a strict-increase requirement would refuse the central write of the M6-06
// "Replace tag" flow. What must be forbidden is going BACKWARDS.
//
// Both directions are asserted here, in one test, on one row, because they are
// one decision: (a) a retire that does not touch the counter must PASS, (b) a
// rewind must be REFUSED.
func TestTags00013_CounterIsMonotonicButRetireIsNot(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	uid := randUID(t)
	successor := randUID(t)
	addPlaque(t, app, fx, uid, fx.locationID, "active", 500)
	addPlaque(t, app, fx, successor, fx.locationID, "active", 0)

	// (a) FORWARD is allowed.
	if n, err := execAs(t, app, fx.tenantID,
		`UPDATE tags SET last_ctr = 501 WHERE uid = $1 AND tenant_id = $2 AND last_ctr < 501`,
		uid, fx.tenantID); err != nil || n != 1 {
		t.Fatalf("advance 500 -> 501: rows=%d err=%v, want 1/nil", n, err)
	}

	// (b) STANDING STILL is allowed: an idempotent rewrite is not a rewind, and
	// refusing it would turn a harmless retry into a failed transaction (00011's
	// BOUNDARY 2 -- a monotonicity guard that fires on a concurrent duplicate
	// makes the caller report failure for work that succeeded).
	if n, err := execAs(t, app, fx.tenantID,
		`UPDATE tags
		 SET last_ctr = 501
		 WHERE uid = $1 AND tenant_id = $2`,
		uid, fx.tenantID); err != nil || n != 1 {
		t.Fatalf("rewrite 501 -> 501: rows=%d err=%v, want 1/nil", n, err)
	}

	// (c) 🔴 THE RETIRE THAT DOES NOT TOUCH last_ctr MUST PASS. This is the
	// assertion that fails if the trigger is written as "> OLD".
	if n, err := execAs(t, app, fx.tenantID,
		`UPDATE tags SET status = 'retired', retired_at = now(), replaced_by = $3
		 WHERE uid = $1 AND tenant_id = $2`,
		uid, fx.tenantID, successor); err != nil || n != 1 {
		t.Fatalf("retire without touching last_ctr: rows=%d err=%v, want 1/nil\n"+
			"a trigger written as NEW.last_ctr > OLD.last_ctr refuses exactly this statement "+
			"-- the central write of the Replace tag flow", n, err)
	}

	// (d) 🔴 THE REWIND MUST BE REFUSED. This is the section 4.4 capability the
	// column grant alone cannot close: last_ctr HAS to stay writable, and the
	// heaviest thing to do with it is to move it back.
	//
	// ⚠️ THE LINE BREAKS ARE NOT COSMETIC AND THEY ARE NOT AN EVASION -- read the
	// note at the head of this file. `make audit`'s R4 rule ("an unconditional
	// last_ctr write is a TOCTOU replay hole") is LINE-LOCAL: written on one line
	// this statement trips it, split across lines it does not. The repo's own SQL is
	// written this way (db/queries/tags.sql), so this is the house style rather than
	// a shape chosen to slip past -- but the scanner cannot tell those two apart,
	// which is precisely the limit recorded at the top.
	_, err := execAs(t, app, fx.tenantID,
		`UPDATE tags
		 SET last_ctr = 0
		 WHERE uid = $1 AND tenant_id = $2`, uid, fx.tenantID)
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "rewind last_ctr 501 -> 0")
	if !strings.Contains(err.Error(), "monotonic") {
		t.Fatalf("rewind refused by something other than the counter trigger: %v", err)
	}
}

// TestTags00013_CounterTriggerBindsTheOwnerToo.
//
// A REVOKE cannot reach tappa_owner -- it is the schema owner and a superuser, so
// FORCE ROW LEVEL SECURITY does not bind it either (M0-03). The trigger does. This
// is the same belt 00005 puts over transactions/audit_log and 00011 over
// admin_sessions, and without this test the protection would be "tappa_app cannot,
// and we hope nobody uses the other role".
//
// It is defence in depth, not an absolute: a superuser can still DISABLE the
// trigger. That is stated in the migration and is not what this test claims.
func TestTags00013_CounterTriggerBindsTheOwnerToo(t *testing.T) {
	app := appDB(t)
	owner := ownerDB(t)
	assertOwnerRole(t, owner)

	fx := newTagTenant(t, app)
	uid := randUID(t)
	addPlaque(t, app, fx, uid, fx.locationID, "active", 900)

	// No tenant context: the owner bypasses RLS, which is exactly why the trigger
	// has to be the thing that stops it. (Line breaks: see the R4 note in the file
	// header.)
	_, err := owner.pool.Exec(context.Background(),
		`UPDATE tags
		 SET last_ctr = 1
		 WHERE uid = $1`, uid)
	wantSQLSTATE(t, err, sqlstateRestrictViolation, "rewind as tappa_owner")
}

// TestTags00013_TheShippedAdvanceStillWorks is the section 4.4 REGRESSION guard.
//
// The column grant and the trigger are both new obstacles on the single most
// critical statement in the product, and two properties of it are NOT obvious:
//
//  1. store.AdvanceTagCounter takes a ROW LOCK (SELECT ... FOR UPDATE in its prev
//     CTE) and Postgres requires an UPDATE privilege for row locking -- a
//     COLUMN-level grant turning out to be enough was probed, not assumed;
//  2. the trigger must be unreachable from this statement, because its WHERE
//     already demands a strict increase, so a replay matches zero rows and the
//     caller sees pgx.ErrNoRows rather than an exception. A caller that started
//     seeing 23001 instead would break every replay path in the product.
//
// The full N-goroutine race proof lives in internal/sun/advance_test.go and
// store_test.go; this asserts the two shapes 00013 could have broken.
func TestTags00013_TheShippedAdvanceStillWorks(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)
	uid := randUID(t)
	addPlaque(t, app, fx, uid, fx.locationID, "active", 10)

	var gap int32
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{
			Ctr: 15, TenantID: fx.tenantID, Uid: uid,
		})
		gap = row.CtrGap
		return e
	}); err != nil {
		t.Fatalf("AdvanceTagCounter 10 -> 15: %v", err)
	}
	if gap != 4 {
		t.Fatalf("ctr gap = %d, want 4 (15 - 10 - 1)", gap)
	}

	// A replay must still be pgx.ErrNoRows, NOT the trigger's exception.
	err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).AdvanceTagCounter(ctx, store.AdvanceTagCounterParams{
			Ctr: 15, TenantID: fx.tenantID, Uid: uid,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("replay error = %v, want pgx.ErrNoRows -- the strict guard in the "+
			"statement must reject it BEFORE any UPDATE is attempted, so the trigger never fires", err)
	}
}

// ---------------------------------------------------------------------------
// THE INVENTORY MODEL
// ---------------------------------------------------------------------------

// TestTags00013_StockPlaqueHasNoWallAndActivePlaqueMustHaveOne.
//
// The inventory model (user decision 2026-08-08) needs a plaque to exist before
// it is mounted: Tappa encodes it and LOADS the row, the panel only BINDS it. So
// location_id became nullable -- and two CHECKs keep the nullability from
// producing a second, contradictory representation of "is this plaque in
// service":
//
//	an ACTIVE plaque must have a location    (it is on a wall)
//	an UNASSIGNED plaque must not            (it is stock)
//
// retired and lost are deliberately unconstrained: a retired plaque keeps the
// location it served at (the audit trail needs it) and a plaque can be lost
// before it is ever mounted.
func TestTags00013_StockPlaqueHasNoWallAndActivePlaqueMustHaveOne(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	for _, tc := range []struct {
		name       string
		status     string
		withLoc    bool
		ok         bool
		constraint string
	}{
		{name: "unassigned stock, no wall", status: "unassigned", withLoc: false, ok: true},
		{name: "active on a wall", status: "active", withLoc: true, ok: true},
		{name: "lost while still in stock", status: "lost", withLoc: false, ok: true},
		{name: "retired keeps its wall", status: "retired", withLoc: true, ok: true},
		{
			name: "active with no wall", status: "active", withLoc: false, ok: false,
			constraint: "tags_active_requires_location",
		},
		{
			name: "unassigned holding a wall", status: "unassigned", withLoc: true, ok: false,
			constraint: "tags_unassigned_has_no_location",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var loc any
			if tc.withLoc {
				loc = fx.locationID
			}
			_, err := execAs(t, app, fx.tenantID,
				`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
				 VALUES ($1, $2, $3, '\xDEAD', $4)`,
				randUID(t), fx.tenantID, loc, tc.status)
			if tc.ok {
				if err != nil {
					t.Fatalf("%s was REFUSED: %v", tc.name, err)
				}
				return
			}
			wantSQLSTATE(t, err, sqlstateCheckViolation, tc.name)
			if !strings.Contains(err.Error(), tc.constraint) {
				t.Fatalf("refused by the wrong constraint (want %s): %v", tc.constraint, err)
			}
		})
	}
}

// TestTags00013_ConstraintsNoOtherTestNames covers the two guarantees 00013 makes
// that nothing else in the suite exercises.
//
// ⚠️ IT EXISTS BECAUSE AN AUDIT COUNTED: tags_replaced_by_canonical_hex appeared in
// ZERO test files. The "no test fails without it" limit was written for the T11
// index, deliberately; it was never meant to cover a CORRECTNESS constraint, and a
// constraint nothing names is one a future migration can drop in silence.
func TestTags00013_ConstraintsNoOtherTestNames(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	// (1) replaced_by takes the SAME canonical form as uid. 00004's constraint
	// accepts both cases; 00013's does not.
	//
	// The successor is inserted in canonical form FIRST, so the lower-case attempt
	// is refused for its SPELLING and not merely for pointing at a row that does not
	// exist -- the assertion checks which constraint fired, because the composite
	// self-FK would also refuse it and would prove something else entirely.
	//
	// 🔴 THE SUCCESSOR MUST CONTAIN A HEX LETTER, AND A PLAIN randUID DOES NOT ALWAYS.
	// randUID is 7 random bytes rendered as 14 upper-case hex digits, so with
	// probability (10/16)^14 = 1/721 every character is 0-9 and strings.ToLower below
	// is a NO-OP: the statement then carries a canonical value, is correctly accepted,
	// and this test fails for a reason that has nothing to do with the constraint it
	// names. Measured -- it fired during an M7-06 verification run of `make check`
	// ("lower-case replaced_by: statement SUCCEEDED"). An assertion about SPELLING
	// needs a value whose spelling can differ, so the letter is forced rather than
	// hoped for. Forced here and not inside randUID: other tests want it uniform.
	successor := uidWithALetter(t)
	addPlaque(t, app, fx, successor, fx.locationID, "active", 0)
	old := randUID(t)
	addPlaque(t, app, fx, old, fx.locationID, "active", 0)

	_, err := execAs(t, app, fx.tenantID,
		`UPDATE tags SET replaced_by = $3 WHERE uid = $1 AND tenant_id = $2`,
		old, fx.tenantID, strings.ToLower(successor))
	wantSQLSTATE(t, err, sqlstateCheckViolation, "lower-case replaced_by")
	if !strings.Contains(err.Error(), "tags_replaced_by_canonical_hex") {
		t.Fatalf("refused by the wrong constraint: %v\nwant tags_replaced_by_canonical_hex "+
			"-- 00004's tags_replaced_by_check accepts both cases, so a rejection from it "+
			"would mean 00013's constraint is absent", err)
	}

	// (2) A retired plaque KEEPS the wall it served at. The migration claims this in
	// prose ("the audit trail needs it"); before the constraint, prose was all there
	// was -- measured, the UPDATE below used to succeed.
	_, err = execAs(t, app, fx.tenantID,
		`UPDATE tags SET status = 'retired', retired_at = now(), location_id = NULL
		 WHERE uid = $1 AND tenant_id = $2`,
		old, fx.tenantID)
	wantSQLSTATE(t, err, sqlstateCheckViolation, "retire that discards the location")
	if !strings.Contains(err.Error(), "tags_retired_keeps_its_location") {
		t.Fatalf("refused by the wrong constraint: %v", err)
	}

	// Control: a retire that KEEPS the location is still allowed, so the constraint
	// refuses the discard rather than retirement itself.
	if n, e := execAs(t, app, fx.tenantID,
		`UPDATE tags SET status = 'retired', retired_at = now()
		 WHERE uid = $1 AND tenant_id = $2`,
		old, fx.tenantID); e != nil || n != 1 {
		t.Fatalf("ordinary retire: rows=%d err=%v, want 1/nil", n, e)
	}

	// Control: a LOST plaque may still have no location -- it can be lost while
	// still in stock, which is why the constraint names 'retired' alone.
	if _, e := execAs(t, app, fx.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
		 VALUES ($1, $2, NULL, '\xDEAD', 'lost')`,
		randUID(t), fx.tenantID); e != nil {
		t.Fatalf("lost-in-stock plaque was refused: %v -- the constraint is too wide", e)
	}
}

// TestTags00013_BindAndRetireAreAtomicTransitions.
//
// Both shipped statements carry their precondition IN the WHERE rather than in
// the caller (`status = 'unassigned'` for the bind, `status = 'active'` for the
// retire). That is the section 4.4 shape applied to a different column: two
// managers acting on the same plaque produce ONE winner and one pgx.ErrNoRows,
// with no read-then-write window between the check and the write.
//
// The second call of each is the assertion that matters. A caller that checked
// the status first and then updated would let both succeed.
func TestTags00013_BindAndRetireAreAtomicTransitions(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	stock := randUID(t)
	successor := randUID(t)
	addPlaque(t, app, fx, stock, uuid.Nil, "unassigned", 0)
	addPlaque(t, app, fx, successor, fx.locationID, "active", 0)

	// BIND -- moves location_id and status in ONE statement, because the CHECK
	// pair refuses the intermediate state.
	var bound store.AssignTagToLocationRow
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		bound, e = store.New(tx).AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: fx.locationID, TenantID: fx.tenantID, Uid: stock,
		})
		return e
	}); err != nil {
		t.Fatalf("AssignTagToLocation: %v", err)
	}
	if bound.Status != "active" || bound.LocationID == nil || *bound.LocationID != fx.locationID {
		t.Fatalf("after bind: status=%q location=%v, want active/%v", bound.Status, bound.LocationID, fx.locationID)
	}

	// The SECOND bind must find nothing: the row is no longer 'unassigned'.
	err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: fx.locationID, TenantID: fx.tenantID, Uid: stock,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second bind error = %v, want pgx.ErrNoRows (the precondition is in the statement)", err)
	}

	// RETIRE -- and last_ctr must survive it untouched (the counter is evidence of
	// how many times the chip was read; retirement is not a reset).
	var retired store.RetireTagForReplacementRow
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		retired, e = store.New(tx).RetireTagForReplacement(ctx, store.RetireTagForReplacementParams{
			ReplacedBy: successor, TenantID: fx.tenantID, Uid: stock,
		})
		return e
	}); err != nil {
		t.Fatalf("RetireTagForReplacement: %v", err)
	}
	if retired.Status != "retired" || retired.RetiredAt == nil {
		t.Fatalf("after retire: status=%q retired_at=%v, want retired/non-nil", retired.Status, retired.RetiredAt)
	}
	if retired.ReplacedBy == nil || *retired.ReplacedBy != successor {
		t.Fatalf("replaced_by = %v, want %q -- the audit chain to the new plaque", retired.ReplacedBy, successor)
	}
	if retired.LocationID == nil {
		t.Fatalf("retire cleared location_id: a retired plaque keeps the wall it served at (audit)")
	}

	// The SECOND retire must find nothing, so "already retired" stays
	// distinguishable from "just retired" and retired_at is never re-stamped.
	err = app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RetireTagForReplacement(ctx, store.RetireTagForReplacementParams{
			ReplacedBy: successor, TenantID: fx.tenantID, Uid: stock,
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second retire error = %v, want pgx.ErrNoRows", err)
	}
}

// TestTags00013_RetireRefusesANonCanonicalSuccessor.
//
// 🔴 char(14) TRUNCATES SILENTLY AND THAT TURNS A TYPO INTO A WRONG AUDIT CHAIN.
// Measured: 'AABBCCDDEEFF01ZZZZZZ'::char(14) -> 'AABBCCDDEEFF01', length 14, no
// error. So an over-long successor uid does not fail -- it becomes a DIFFERENT
// plaque, and if that plaque exists the self-FK accepts it and `replaced_by` points
// at the wrong successor forever. The reverse of the usual worry: not a rejected
// good value, but an ACCEPTED wrong one.
//
// The over-long case is built to truncate onto a REAL plaque, because a probe that
// truncates onto a non-existent uid would be caught by the foreign key and would
// prove nothing about the guard.
func TestTags00013_RetireRefusesANonCanonicalSuccessor(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	old := randUID(t)
	successor := randUID(t)
	addPlaque(t, app, fx, old, fx.locationID, "active", 0)
	addPlaque(t, app, fx, successor, fx.locationID, "active", 0)

	retire := func(replacedBy string) error {
		return app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := store.New(tx).RetireTagForReplacement(ctx, store.RetireTagForReplacementParams{
				ReplacedBy: replacedBy, TenantID: fx.tenantID, Uid: old,
			})
			return e
		})
	}

	for _, tc := range []struct {
		name string
		arg  string
	}{
		// Truncates to `successor`, which EXISTS -- the dangerous shape.
		{"over-long, truncating onto a real plaque", successor + "ZZZZZZ"},
		{"lower case", strings.ToLower(successor)},
		{"too short", successor[:10]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := retire(tc.arg); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("retire(%q) = %v, want pgx.ErrNoRows -- a successor uid that is not "+
					"in canonical form must match nothing, not be trimmed into a different plaque", tc.arg, err)
			}
		})
	}

	// Positive control: the canonical successor still works, so the guard rejects
	// malformed input rather than everything.
	if err := retire(successor); err != nil {
		t.Fatalf("retire with the canonical successor: %v -- the guard is refusing valid input", err)
	}
}

// TestTags00013_BindRefusesAnotherTenantsLocation.
//
// The bind's only argument that is not a key is a location id, and the panel gets
// it from a form. The composite FK (location_id, tenant_id) -> locations
// (id, tenant_id) is what refuses one belonging to another business -- structural,
// not a predicate somebody has to remember to write. 23503 is the CORRECT outcome;
// the screen turns it into a sentence.
func TestTags00013_BindRefusesAnotherTenantsLocation(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	a := newTagTenant(t, app)
	b := newTagTenant(t, app)

	stock := randUID(t)
	addPlaque(t, app, a, stock, uuid.Nil, "unassigned", 0)

	err := app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).AssignTagToLocation(ctx, store.AssignTagToLocationParams{
			LocationID: b.locationID, TenantID: a.tenantID, Uid: stock,
		})
		return e
	})
	wantSQLSTATE(t, err, sqlstateForeignKeyViolation, "bind A's plaque to B's location")
}

// ---------------------------------------------------------------------------
// RLS ISOLATION -- the probes below carry NO tenant_id filter (CLAUDE.md §6)
// ---------------------------------------------------------------------------

// TestRLS_Tags00013_StockIsolation.
//
// 00013 introduces a row shape that did not exist before -- a plaque with NO
// location -- and it is the shape most likely to be treated as "global stock" by
// a future reader. It is not: it belongs to the tenant it was shipped to, and
// every one of the three statement shapes must say so.
//
// READ and UPDATE fail SILENTLY when a policy is wrong (0 rows), INSERT raises
// 42501. All three are probed, each against a positive control in the owning
// tenant, and NONE of the probes carries a tenant predicate -- so a 0 here is the
// policy's doing and not the WHERE's.
func TestRLS_Tags00013_StockIsolation(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := newTagTenant(t, app)
	b := newTagTenant(t, app)

	stock := randUID(t)
	addPlaque(t, app, a, stock, uuid.Nil, "unassigned", 0)

	// (1) READ -- B must not see A's stock; A must.
	count := func(ctxTenant uuid.UUID) int {
		t.Helper()
		var n int
		if err := app.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1`, stock).Scan(&n)
		}); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if got := count(b.tenantID); got != 0 {
		t.Fatalf("B sees %d of A's stock plaques, want 0", got)
	}
	if got := count(a.tenantID); got != 1 {
		t.Fatalf("A sees %d of its own stock plaques, want 1 -- the fixture is broken, "+
			"so the 0 above proves nothing", got)
	}

	// (2) UPDATE -- B must not be able to bind A's stock to B's own wall. This is
	// the statement that would fail silently: it does not raise, it matches
	// nothing.
	n, err := execAs(t, app, b.tenantID,
		`UPDATE tags SET location_id = $1, status = 'active' WHERE uid = $2`, b.locationID, stock)
	if err != nil {
		t.Fatalf("B's bind attempt errored (%v); what is under test is that it matches NOTHING", err)
	}
	if n != 0 {
		t.Fatalf("B's bind touched %d rows of A's stock, want 0", n)
	}
	// Positive control: the same statement in A's context does work.
	if n, err := execAs(t, app, a.tenantID,
		`UPDATE tags SET location_id = $1, status = 'active' WHERE uid = $2`, a.locationID, stock); err != nil || n != 1 {
		t.Fatalf("A's own bind: rows=%d err=%v, want 1/nil -- without this the 0 above "+
			"could be a broken statement rather than a working policy", n, err)
	}

	// (3) INSERT -- B must not be able to forge a plaque stamped with A's tenant.
	_, err = execAs(t, app, b.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status)
		 VALUES ($1, $2, NULL, '\xDEAD', 'unassigned')`,
		randUID(t), a.tenantID)
	wantSQLSTATE(t, err, sqlstateInsufficientPrivi, "B forging a plaque into A's tenant")
}

// TestTags00013_LastSeenIsAbsentNotZeroForANeverTappedPlaque pins the shape that
// keeps "last seen" honest, and it exists because the obvious design FAILED here
// first.
//
// 🔴 THE OBVIOUS DESIGN: put last_seen on ListTagsForTenant as a correlated
// subquery. Measured -- sqlc v1.28 types it either as interface{} (useless) or,
// with a cast, as a NON-POINTER time.Time, and the latter compiles and then dies at
// run time on precisely the rows this milestone adds:
//
//	can't scan into dest[8] (col: last_seen): cannot scan NULL into *time.Time
//
// 🔴 AND THE "FIX" OF ACCEPTING A ZERO time.Time WOULD REPEAT THE BUG THIS
// MILESTONE IS ALREADY HANDING OVER. 00013 made tags.location_id nullable, and
// google/uuid scans SQL NULL into uuid.Nil WITHOUT AN ERROR -- so an unbound plaque
// reaches the tap path looking like a plaque bound to the zero location. A zero
// time.Time meaning "never tapped" is the same silent lie in a different type.
//
// So absence is represented by ABSENCE: ListTagLastSeen returns a row only for
// plaques that HAVE been tapped, its GROUP BY makes time.Time genuinely correct,
// and a caller learns "never" from the key being missing. This test asserts exactly
// that -- a tapped plaque appears with a real timestamp, an untapped one does not
// appear at all -- because it is the property a future "just add it to the list"
// change would break.
func TestTags00013_LastSeenIsAbsentNotZeroForANeverTappedPlaque(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	tapped := randUID(t)
	never := randUID(t)
	addPlaque(t, app, fx, tapped, fx.locationID, "active", 3)
	addPlaque(t, app, fx, never, uuid.Nil, "unassigned", 0)

	// One recorded tap on the first plaque. verdict/channel are the minimum the
	// table's CHECKs accept for a row with no employee.
	if _, err := execAs(t, app, fx.tenantID,
		`INSERT INTO transactions (tenant_id, location_id, tag_uid, occurred_at, verdict, channel)
		 VALUES ($1, $2, $3, now(), 'reject', 'nfc')`,
		fx.tenantID, fx.locationID, tapped); err != nil {
		t.Fatalf("insert tap: %v", err)
	}

	// 🔴 A MANUAL ROW -- tag_uid NULL -- AND WITHOUT IT THE NULL ASSERTION BELOW IS
	// UNFALSIFIABLE. Measured: delete `AND x.tag_uid IS NOT NULL` from the generated
	// statement, and with only the tap above in the fixture the whole package stays
	// GREEN, because a fresh tenant with no manual row can never form a NULL group.
	// The t.Fatal below NAMED that mutation while being unable to see it -- this
	// project's signature defect class, and the rule it produced: if a failure
	// message names a mutation, RUN that mutation.
	//
	// channel='manual' with no tag_uid is the real shape (a manager types a shift;
	// there was no plaque), and it is exactly what the query's IS NOT NULL predicate
	// exists to exclude.
	if _, err := execAs(t, app, fx.tenantID,
		`INSERT INTO transactions (tenant_id, location_id, tag_uid, occurred_at, verdict, channel)
		 VALUES ($1, $2, NULL, now(), 'reject', 'manual')`,
		fx.tenantID, fx.locationID); err != nil {
		t.Fatalf("insert manual row: %v", err)
	}

	var rows []store.ListTagLastSeenRow
	if err := app.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).ListTagLastSeen(ctx, fx.tenantID)
		return e
	}); err != nil {
		t.Fatalf("ListTagLastSeen: %v", err)
	}

	seen := map[string]bool{}
	for _, r := range rows {
		if r.TagUid == nil {
			t.Fatal("a row came back with a NULL tag_uid; the IS NOT NULL predicate is gone " +
				"and the caller has a row it can never join")
		}
		if r.LastSeen.IsZero() {
			t.Fatalf("%s came back with a ZERO timestamp -- the whole point of this shape "+
				"is that a returned row always carries a real instant", *r.TagUid)
		}
		seen[*r.TagUid] = true
	}
	if !seen[tapped] {
		t.Fatalf("the tapped plaque %s is missing from %d row(s); the fixture or the "+
			"query is broken, so the assertion below would prove nothing", tapped, len(rows))
	}
	if seen[never] {
		t.Fatalf("the never-tapped plaque %s came back with a timestamp -- 'never' must be "+
			"ABSENCE, never a zero value", never)
	}
}

// TestTags00013_VenueNamesAreBoundedBySchemaNotOnlyByCode pins the constraint
// migration 00013 adds on behalf of M6-06 phase A, whose own comment named this
// migration as the debt's home ("a CHECK constraint would be the right home for it
// the next time this schema is touched").
//
// It mirrors venueName's TWO rules, and both halves are asserted because writing
// only the length half would leave '   ' storable -- a schema rule weaker than the
// code rule teaches the next reader the wrong bound.
//
// The rune/byte distinction is asserted with a real Maltese name: the brand ships
// ċ ġ ħ ż precisely because venue names are not ASCII, and a byte bound would refuse
// ordinary names at four fifths of the stated length.
func TestTags00013_VenueNamesAreBoundedBySchemaNotOnlyByCode(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	fx := newTagTenant(t, app)

	const limit = 80 // tenant.MaxVenueNameRunes, duplicated here as the schema's copy

	for _, tc := range []struct {
		name       string
		value      string
		ok         bool
		constraint string
	}{
		{name: "ordinary name", value: "St Julians", ok: true},
		{name: "exactly at the limit", value: strings.Repeat("a", limit), ok: true},
		// 80 MALTESE runes = 160 bytes. Under a byte bound this is refused; under a
		// rune bound it is fine, and the product needs it to be fine.
		{name: "80 multi-byte runes", value: strings.Repeat("ħ", limit), ok: true},
		{name: "one rune over", value: strings.Repeat("a", limit+1), ok: false,
			constraint: "locations_name_bounded"},
		{name: "one multi-byte rune over", value: strings.Repeat("ħ", limit+1), ok: false,
			constraint: "locations_name_bounded"},
		{name: "blank", value: "   ", ok: false, constraint: "locations_name_bounded"},
		{name: "empty", value: "", ok: false, constraint: "locations_name_bounded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := execAs(t, app, fx.tenantID,
				`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
				 VALUES (gen_random_uuid(), $1, $2, 35.9, 14.5)`,
				fx.tenantID, tc.value)
			if tc.ok {
				if err != nil {
					t.Fatalf("%q (%d runes) was REFUSED: %v", tc.name, len([]rune(tc.value)), err)
				}
				return
			}
			wantSQLSTATE(t, err, sqlstateCheckViolation, tc.name)
			if !strings.Contains(err.Error(), tc.constraint) {
				t.Fatalf("refused by the wrong constraint (want %s): %v", tc.constraint, err)
			}
		})
	}

	// departments carry the identical rule; the symmetry is deliberate (a manager
	// must not learn two bounds for two lists).
	_, err := execAs(t, app, fx.tenantID,
		`INSERT INTO departments (id, tenant_id, location_id, name)
		 VALUES (gen_random_uuid(), $1, $2, $3)`,
		fx.tenantID, fx.locationID, strings.Repeat("a", limit+1))
	wantSQLSTATE(t, err, sqlstateCheckViolation, "department name one rune over")
	if !strings.Contains(err.Error(), "departments_name_bounded") {
		t.Fatalf("refused by the wrong constraint: %v", err)
	}
}

// TestRLS_Tags00013_ForceAndNullifArePinned closes two holes an audit found by
// MUTATING THE SCHEMA rather than the code: `ALTER TABLE tags NO FORCE ROW LEVEL
// SECURITY` and rewriting the policy's NULLIF into a bare ::uuid cast BOTH left the
// whole internal/db package GREEN. Neither is 00013's doing -- they arrived with
// 00004 -- but both are guarantees CLAUDE.md section 6 states outright (FORCE is one
// of the five elements; the NULLIF is "required, not decoration", ADR 0002 / Q27),
// and this is the migration that hardens this table.
//
// 🔴 THE FORCE HALF CANNOT BE TESTED BEHAVIOURALLY, AND SAYING SO IS THE POINT.
// FORCE only binds the table's OWNER, and this schema's owner (tappa_owner) is the
// bootstrap SUPERUSER, which bypasses RLS unconditionally -- M0-03 measured exactly
// that. So there is no query whose result changes when FORCE is dropped, and a
// behavioural test would be impossible to write rather than merely absent. What CAN
// be pinned is the catalog fact, and that is what the shipped guarantee actually
// is: the day tappa_owner stops being a superuser (or a second, non-superuser owner
// appears), FORCE is the only thing standing between them and every tenant's rows.
//
// 🔴 THE NULLIF HALF IS TESTED BOTH WAYS, because a catalog assertion alone would
// pass a policy that spells NULLIF and means nothing. The behavioural half needs the
// connection state that distinguishes them, and that state is NOT a fresh
// connection: with a never-written GUC both spellings yield NULL and 0 rows. The
// difference appears only on a connection where the GUC HAS been written and the
// transaction has ended -- Postgres leaves ” behind, not NULL (Q27, measured
// there) -- and on that connection a bare cast raises 22P02 instead of returning
// nothing. That is why this test runs a real tenant transaction first and then
// queries on the SAME pinned backend.
func TestRLS_Tags00013_ForceAndNullifArePinned(t *testing.T) {
	app := testDB(t) // pinned to ONE connection, so the GUC state below is observable
	assertAppRole(t, app)

	// --- (1) the catalog facts, for both halves -----------------------------
	var enabled, forced bool
	if err := app.pool.QueryRow(context.Background(),
		`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE relname = 'tags'`,
	).Scan(&enabled, &forced); err != nil {
		t.Fatalf("read pg_class for tags: %v", err)
	}
	if !enabled {
		t.Fatal("tags has ROW LEVEL SECURITY disabled -- the first of section 6's five elements is gone")
	}
	if !forced {
		t.Fatal("tags is not FORCE ROW LEVEL SECURITY. That is one of section 6's five " +
			"elements, and it is the ONLY thing that would bind a non-superuser owner. " +
			"No query result changes today (tappa_owner is a superuser and bypasses RLS " +
			"regardless -- M0-03), which is precisely why nothing else can notice it going missing.")
	}

	var using, withCheck string
	if err := app.pool.QueryRow(context.Background(),
		`SELECT pg_get_expr(polqual, polrelid), pg_get_expr(polwithcheck, polrelid)
		   FROM pg_policy WHERE polrelid = 'tags'::regclass`,
	).Scan(&using, &withCheck); err != nil {
		t.Fatalf("read the tags policy: %v", err)
	}
	for label, expr := range map[string]string{"USING": using, "WITH CHECK": withCheck} {
		if !strings.Contains(expr, "NULLIF") {
			t.Fatalf("the tags policy's %s clause has no NULLIF: %s\n"+
				"A bare ::uuid cast ERRORS on a connection whose GUC is '' rather than "+
				"returning no rows (ADR 0002 / Q27), so the behaviour becomes a function of "+
				"the connection's history.", label, expr)
		}
	}

	// --- (2) the behavioural half -------------------------------------------
	// Establish and end a tenant transaction on this pinned backend, which leaves
	// app.tenant_id as '' rather than NULL.
	fx := newTagTenant(t, app)
	uid := randUID(t)
	addPlaque(t, app, fx, uid, fx.locationID, "active", 0)

	var guc *string
	if err := app.pool.QueryRow(context.Background(),
		`SELECT current_setting('app.tenant_id', true)`).Scan(&guc); err != nil {
		t.Fatalf("read the GUC after the transaction: %v", err)
	}
	if guc == nil || *guc != "" {
		t.Fatalf("app.tenant_id is %v after a finished transaction, want the empty string -- "+
			"this test needs THAT state, because a never-written GUC cannot tell the two "+
			"policy spellings apart", guc)
	}

	// The row exists and belongs to fx, but there is no context: NULLIF collapses
	// '' to NULL, nothing matches, and the read must SUCCEED with zero rows. Under
	// a bare cast this same statement raises 22P02.
	var n int
	if err := app.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM tags WHERE uid = $1`, uid).Scan(&n); err != nil {
		t.Fatalf("context-less read of tags ERRORED: %v\n"+
			"With NULLIF this must be 0 rows and no error; an invalid-input-syntax error "+
			"here is the signature of a bare ::uuid cast on an emptied GUC.", err)
	}
	if n != 0 {
		t.Fatalf("context-less read returned %d row(s); RLS must fail closed", n)
	}
}

// TestBelt_Tags00013_PanelQueriesCarryTheirOwnTenantPredicate measures the BELT --
// the explicit `WHERE tenant_id = @tenant_id` section 4.5 requires ON TOP of RLS --
// for ALL FIVE panel statements 00013 ships.
//
// ⚠️ IT COVERED FOUR AND SAID SO IN A HEADER THAT MEANT FIVE, which is how the two
// nets ended up counting different sets: the static net in
// internal/domain/tenant/query_test.go says "all five of its statements" and this
// one said "all four shipped panel statements". The odd one out was
// ListTagLastSeen, whose belt was STATIC ONLY -- an auditor neutralised its
// predicate and the whole internal/db package stayed green. The two headers now
// name the same set, and the runtime half actually covers it. (AdvanceTagCounter is
// the sixth query in that file and is deliberately NOT here: it is section 4.4's
// statement, covered by TestTags00013_TheShippedAdvanceStillWorks and by the
// concurrency proofs in internal/sun -- the static net does count it.)
//
// 🔴 THE SHAPE IS THE WHOLE POINT AND AN EARLIER VERSION OF THIS TEST GOT IT WRONG.
// That version ran each query FROM B'S CONTEXT while naming A's tenant, and claimed
// "a regression that dropped WHERE tenant_id would leave the RLS test green, so this
// one is the belt". It was not: in B's context RLS ALREADY hides A's rows, so the
// predicate contributes NOTHING to the outcome and deleting it changes nothing. An
// auditor replaced the predicate in all four generated statements with
// `$n::uuid IS NOT NULL` and the whole internal/db package stayed GREEN. The test
// measured the braces twice and called one of them the belt.
//
// 🔴 SO EVERY CALL BELOW RUNS IN **A's** CONTEXT AND PASSES **B's** TENANT ID
// against **A's own row**. RLS is satisfied throughout -- the row is visible, the
// role may write it -- so the ONLY thing that can refuse the statement is its own
// predicate. Delete the predicate and each of these succeeds, which is exactly what
// CLAUDE.md section 6 means by "an isolation test and a production query want
// DIFFERENT shapes": TestRLS_Tags00013_StockIsolation above carries NO predicate and
// measures the policy; this one stays inside one tenant and measures the predicate.
//
// Each negative is paired with the same call under A's own id, so a broken fixture
// or a mistyped uid cannot masquerade as a working belt.
func TestBelt_Tags00013_PanelQueriesCarryTheirOwnTenantPredicate(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)

	a := newTagTenant(t, app)
	b := newTagTenant(t, app)
	mounted := randUID(t)
	stock := randUID(t)
	successor := randUID(t)
	addPlaque(t, app, a, mounted, a.locationID, "active", 7)
	addPlaque(t, app, a, stock, uuid.Nil, "unassigned", 0)
	addPlaque(t, app, a, successor, a.locationID, "active", 0)

	// One recorded tap on A's mounted plaque, so ListTagLastSeen has something to
	// return: a sub-test that asserts "0 rows" against a tenant with no history at
	// all would pass with the predicate deleted too.
	if _, err := execAs(t, app, a.tenantID,
		`INSERT INTO transactions (tenant_id, location_id, tag_uid, occurred_at, verdict, channel)
		 VALUES ($1, $2, $3, now(), 'reject', 'nfc')`,
		a.tenantID, a.locationID, mounted); err != nil {
		t.Fatalf("insert tap: %v", err)
	}

	// inA runs fn inside tenant A's context. Everything in this test does.
	inA := func(fn func(ctx context.Context, q *store.Queries) error) error {
		return app.WithTenant(context.Background(), a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return fn(ctx, store.New(tx))
		})
	}

	t.Run("GetTagForTenant", func(t *testing.T) {
		// A's row, A's context, B's tenant id. Only the predicate can refuse it.
		err := inA(func(ctx context.Context, q *store.Queries) error {
			_, e := q.GetTagForTenant(ctx, store.GetTagForTenantParams{TenantID: b.tenantID, Uid: mounted})
			return e
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("got %v, want pgx.ErrNoRows -- the row IS visible here (A's context), "+
				"so returning it means the query has no tenant predicate of its own", err)
		}
		// Positive control: the same call with A's id returns the row.
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			row, e := q.GetTagForTenant(ctx, store.GetTagForTenantParams{TenantID: a.tenantID, Uid: mounted})
			if e == nil && row.Uid != mounted {
				t.Fatalf("control returned %q, want %q", row.Uid, mounted)
			}
			return e
		}); err != nil {
			t.Fatalf("control: %v -- without it the ErrNoRows above could be a bad fixture", err)
		}
	})

	t.Run("ListTagsForTenant", func(t *testing.T) {
		var rows []store.ListTagsForTenantRow
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			var e error
			rows, e = q.ListTagsForTenant(ctx, b.tenantID)
			return e
		}); err != nil {
			t.Fatalf("list with B's id from A's context: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("listed %d rows for B's tenant id while inside A's context, want 0 -- "+
				"those are A's plaques, returned because the query has no tenant predicate", len(rows))
		}
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			var e error
			rows, e = q.ListTagsForTenant(ctx, a.tenantID)
			return e
		}); err != nil {
			t.Fatalf("control: %v", err)
		}
		if len(rows) != 3 {
			t.Fatalf("control listed %d rows, want 3 -- without it the 0 above proves nothing", len(rows))
		}
		// The list orders stock first (location_id NULLS FIRST), which the screen relies on.
		if rows[0].LocationID != nil {
			t.Fatalf("first row is bound to %v, want the unassigned plaque first", rows[0].LocationID)
		}
	})

	t.Run("ListTagLastSeen", func(t *testing.T) {
		// The fifth statement, and the one that had NO runtime belt at all until an
		// auditor measured it: neutralising its predicate left internal/db green
		// because only the static net in internal/domain/tenant was watching it.
		//
		// A's plaque HAS been tapped (the fixture below), so a query with no tenant
		// predicate returns that row from inside A's own context.
		var rows []store.ListTagLastSeenRow
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			var e error
			rows, e = q.ListTagLastSeen(ctx, b.tenantID)
			return e
		}); err != nil {
			t.Fatalf("last seen with B's id from A's context: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("returned %d row(s) for B's tenant id while inside A's context, want 0 -- "+
				"that is A's tap history, returned because the query has no tenant predicate", len(rows))
		}
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			var e error
			rows, e = q.ListTagLastSeen(ctx, a.tenantID)
			return e
		}); err != nil {
			t.Fatalf("control: %v", err)
		}
		if len(rows) != 1 || rows[0].TagUid == nil || *rows[0].TagUid != mounted {
			t.Fatalf("control returned %d row(s) (%+v), want exactly A's tapped plaque %s -- "+
				"without it the 0 above proves nothing", len(rows), rows, mounted)
		}
	})

	t.Run("CountTenantPlaques", func(t *testing.T) {
		// M7-03 phase B's read, and it is HERE rather than only in the static net for
		// the reason ListTagLastSeen's sub-test above records: a statement whose belt
		// is watched statically and nowhere at runtime is the shape an auditor already
		// neutralised once in this package without turning anything red.
		//
		// A COUNT ALWAYS RETURNS A ROW, so the assertion is on the NUMBERS. Inside A's
		// context, asking for B's id must count nothing: A's three plaques are visible
		// to RLS here, so any non-zero answer is the query counting rows it was not
		// asked about.
		var got store.CountTenantPlaquesRow
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			var e error
			got, e = q.CountTenantPlaques(ctx, b.tenantID)
			return e
		}); err != nil {
			t.Fatalf("count with B's id from A's context: %v", err)
		}
		if got.Loaded != 0 || got.InService != 0 || got.InStock != 0 {
			t.Fatalf("counted %+v for B's tenant id while inside A's context, want all zero -- "+
				"those are A's plaques, counted because the query has no tenant predicate", got)
		}
		// Positive control, and it also pins the three FILTERs against a fixture whose
		// shape is known: A holds two active plaques and one in stock.
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			var e error
			got, e = q.CountTenantPlaques(ctx, a.tenantID)
			return e
		}); err != nil {
			t.Fatalf("control: %v", err)
		}
		if got.Loaded != 3 || got.InService != 2 || got.InStock != 1 {
			t.Fatalf("control counted %+v, want loaded=3 in_service=2 in_stock=1 -- without "+
				"it the zeroes above prove nothing, and the three FILTERs are unmeasured", got)
		}
	})

	t.Run("TenantHasAnyTransaction", func(t *testing.T) {
		// M7-03 phase B's SECOND read, and it is here because its sibling
		// CountTenantPlaques got a runtime belt in the same change and this one did
		// not — the exact asymmetry this file's header records as a shipped defect
		// for ListTagLastSeen. An audit measured the gap the same way: with the
		// predicate neutralised in the generated statement, the whole suite stayed
		// green.
		//
		// A BOOLEAN CANNOT RETURN "NO ROWS", so the assertion is on the ANSWER. The
		// fixture above inserted ONE transaction, and it belongs to A. Asking for B's
		// id from inside A's context must therefore be false: A's row is visible to
		// RLS here, so `true` means the query counted a row it was not asked about.
		var any bool
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			var e error
			any, e = q.TenantHasAnyTransaction(ctx, b.tenantID)
			return e
		}); err != nil {
			t.Fatalf("probe with B's id from A's context: %v", err)
		}
		if any {
			t.Fatalf("answered true for B's tenant id while inside A's context -- B has no "+
				"records at all, so that is A's tap, seen because the query has no tenant "+
				"predicate of its own (tenant A=%s B=%s)", a.tenantID, b.tenantID)
		}
		// Positive control: A's own id finds A's row. Without it the false above
		// would also be produced by a fixture that never inserted anything.
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			var e error
			any, e = q.TenantHasAnyTransaction(ctx, a.tenantID)
			return e
		}); err != nil {
			t.Fatalf("control: %v", err)
		}
		if !any {
			t.Fatal("control answered false for A's own id, but A has a recorded tap -- " +
				"the false above proves nothing")
		}
	})

	t.Run("AssignTagToLocation", func(t *testing.T) {
		// A WRITE fails differently from a read: no error, no rows. Both are asserted
		// through the :one shape, which turns "no rows" into ErrNoRows.
		err := inA(func(ctx context.Context, q *store.Queries) error {
			_, e := q.AssignTagToLocation(ctx, store.AssignTagToLocationParams{
				LocationID: a.locationID, TenantID: b.tenantID, Uid: stock,
			})
			return e
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("got %v, want pgx.ErrNoRows -- A's stock is writable in this context, "+
				"so a success means B's tenant id bound nothing", err)
		}
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			_, e := q.AssignTagToLocation(ctx, store.AssignTagToLocationParams{
				LocationID: a.locationID, TenantID: a.tenantID, Uid: stock,
			})
			return e
		}); err != nil {
			t.Fatalf("control: %v", err)
		}
	})

	t.Run("RetireTagForReplacement", func(t *testing.T) {
		err := inA(func(ctx context.Context, q *store.Queries) error {
			_, e := q.RetireTagForReplacement(ctx, store.RetireTagForReplacementParams{
				ReplacedBy: successor, TenantID: b.tenantID, Uid: mounted,
			})
			return e
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("got %v, want pgx.ErrNoRows -- retiring A's plaque under B's tenant id "+
				"must bind nothing", err)
		}
		if err := inA(func(ctx context.Context, q *store.Queries) error {
			_, e := q.RetireTagForReplacement(ctx, store.RetireTagForReplacementParams{
				ReplacedBy: successor, TenantID: a.tenantID, Uid: mounted,
			})
			return e
		}); err != nil {
			t.Fatalf("control: %v", err)
		}
	})
}
