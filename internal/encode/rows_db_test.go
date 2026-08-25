package encode

// rows_db_test.go -- DBRows against REAL Postgres.
//
// 🔴 A REAL DATABASE IS NOT A PREFERENCE HERE (CLAUDE.md §8). The three things this
// file measures -- the RLS policy, migration 00013's column GRANT and 00022's
// write-once trigger -- are the three things a fake cannot have, and each of them
// is silent when it is wrong: a policy that stopped applying returns rows instead
// of raising, and a grant that widened returns success.
//
// ⚠️ WHAT THESE TESTS DO **NOT** PROVE, said first because the whole package carries
// the same limit: no chip has been encoded (ADR 0017 §6 md. 1). Everything here is
// about the ROW and the TRAIL, driven directly through the port, with no silicon and
// no relay anywhere in it.
//
// Fixtures are NOT cleaned up, for the reason internal/db/store_test.go gives:
// tappa_app has REVOKE DELETE on `tags` (§4.6) and audit_log is append-only for
// every role including the owner (00005's trigger). Fresh random uids and tenants
// keep runs apart.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping encode DB tests (real Postgres required)")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// newDBRows builds the real port over the real recorder -- no doubles below this
// line, which is what makes the RLS and GRANT assertions mean anything.
func newDBRows(t *testing.T, d *db.DB) *DBRows {
	t.Helper()
	rec, err := audit.New(d)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	r, err := NewDBRows(d, rec)
	if err != nil {
		t.Fatalf("NewDBRows: %v", err)
	}
	return r
}

// ownerRead runs one query as tappa_owner.
//
// 🔴 IT EXISTS BECAUSE MIGRATION 00022 TOOK aes_key_ref OUT OF tappa_app's SELECT
// PRIVILEGE, AND THESE TWO ASSERTIONS ARE THE PROOF THAT IT BIT. Both used to read
// the envelope through the ordinary application pool; after the revoke they answer
// `permission denied for table tags (SQLSTATE 42501)` -- measured, and it is the
// whole point of the change. The assertions are about a fact the PRODUCT may no
// longer see, so they read it the way the rotatekek runbook does: as the owner.
//
// ⚠️ IT IS NOT A WAY AROUND THE WALL. Nothing in internal/encode uses this; it is a
// test-only reader over DATABASE_MIGRATE_URL, and it SKIPS rather than falling back if
// that is unset.
//
// ⚠️ AND "SKIPS" IS ALL IT DOES -- AN EARLIER VERSION OF THIS PARAGRAPH SAID THE SKIP
// WAS LOUD AND AN AUDIT MEASURED IT SILENT (2026-08-24). With DATABASE_URL set and
// DATABASE_MIGRATE_URL unset, `go test ./internal/encode/` prints `ok` and nothing
// else; t.Skip is invisible without -v. What makes that survivable is not this
// helper: it is the Makefile's require-db-env, which refuses `make test` unless BOTH
// are set, and CI sets both. A claim about volume belongs to the mechanism that
// provides it.
func ownerRead(t *testing.T, fn func(ctx context.Context, c *pgx.Conn) error) {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; this assertion reads aes_key_ref, which 00022 " +
			"revoked from tappa_app")
	}
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	defer func() { _ = c.Close(ctx) }()
	if err := fn(ctx, c); err != nil {
		t.Fatalf("owner read: %v", err)
	}
}

func newEncodeTenant(t *testing.T, d *db.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := d.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Encode Test Ltd', $2, 'bar', 'single')`,
			id, "VAT-"+id.String())
		return e
	})
	if err != nil {
		t.Fatalf("newEncodeTenant: %v", err)
	}
	return id
}

// newEncodeAdmin creates an admin row so a trail entry has a real actor to name.
//
// audit_log.actor_id carries NO foreign key (00005 keeps it polymorphic — measured on
// the live catalogue), so this row is not strictly required to write the id. It is
// created anyway because db/queries/audit.sql LEFT JOINs admin_users to render a NAME,
// and a test that wrote an id nothing could resolve would assert the column and not
// the thing the column is for.
func newEncodeAdmin(t *testing.T, d *db.DB, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, email, password_hash, full_name, role, status)
			 VALUES ($1, $2, $3, $4, 'Encode Operator', 'owner', 'active')`,
			id, tenant, "encode-"+id.String()+"@example.test",
			"$2a$12$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0")
		return e
	})
	if err != nil {
		t.Fatalf("newEncodeAdmin: %v", err)
	}
	return id
}

// newUID mints a uid in the CANONICAL form migration 00013 requires and 00021
// validated -- upper-case hex, exactly 14 characters.
func newUID(t *testing.T) string {
	t.Helper()
	var b [7]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("mint a uid: %v", err)
	}
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

// wrappedFor seals a throwaway plaque key so the row carries a GENUINE 44-byte
// envelope rather than a placeholder. Migration 00021's
// tags_aes_key_ref_is_kek_envelope refuses anything else, and using a real one
// keeps this test from being the thing that would have to change if the shape did.
func wrappedFor(t *testing.T, uidHex string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(uidHex)
	if err != nil {
		t.Fatalf("decode uid: %v", err)
	}
	w, err := NewKEKWrapper(bytesOf(sun.KEKLen, 0x3B))
	if err != nil {
		t.Fatalf("NewKEKWrapper: %v", err)
	}
	ref, err := w.WrapKey(raw, bytesOf(16, 0x77))
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}
	return ref
}

// --- ADR 0017 §5.1 step 3: the row and its trail entry --------------------------

// TestDBRows_InsertLoadsStockAndWritesItsTrailEntryInOneTransaction.
//
// The row must land as ADR 0017 §5.2 and the M8-05 card's second trap require:
// status 'unassigned' (NEVER 'active' -- that would show a boxed plaque as in
// service), no location, a full-size envelope, and encoded_at STILL NULL, because
// the chip has not been touched at this point in the round.
func TestDBRows_InsertLoadsStockAndWritesItsTrailEntryInOneTransaction(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	tenant := newEncodeTenant(t, d)
	uid := newUID(t)
	const actor = "operator-alpha"

	if err := rows.InsertUnassigned(context.Background(), tenant, uuid.Nil, uid, wrappedFor(t, uid), actor); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}

	var (
		status   string
		location *uuid.UUID
		refLen   int
		encoded  *string
	)
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status, location_id, encoded_at::text
			   FROM tags WHERE uid = $1 AND tenant_id = $2`, uid, tenant).
			Scan(&status, &location, &encoded)
	})
	// The envelope's LENGTH is an owner-only fact as of 00022 -- see ownerRead.
	ownerRead(t, func(ctx context.Context, c *pgx.Conn) error {
		return c.QueryRow(ctx,
			`SELECT octet_length(aes_key_ref) FROM tags WHERE uid = $1 AND tenant_id = $2`,
			uid, tenant).Scan(&refLen)
	})
	if status != "unassigned" {
		t.Errorf("status = %q, want unassigned -- 'active' would show a plaque still in its box "+
			"as in service, and 00013's tags_active_requires_location would refuse it anyway", status)
	}
	if location != nil {
		t.Errorf("location_id = %v, want NULL -- a freshly encoded plaque is on nobody's wall", location)
	}
	if refLen != sun.WrappedKeyLen {
		t.Errorf("aes_key_ref is %d bytes, want %d (ADR 0003 md. 4)", refLen, sun.WrappedKeyLen)
	}
	if encoded != nil {
		t.Errorf("encoded_at = %v after step 3; the chip has not been touched yet and that is "+
			"the whole distinction migration 00022 exists to make", *encoded)
	}

	// THE TRAIL ENTRY. ADR 0017 §5.2 says the round belongs in audit_log and §6
	// md. 8 left the event undefined; this is the definition.
	ev := onlyTrailEntry(t, d, tenant, uid, ActionPlaqueLoaded)
	// 🔴 THIS ASSERTS THE uuid.Nil ARM AND NOTHING MORE, AND THE MESSAGE THAT USED TO
	// BE HERE ASSERTED A RETRACTED RULE (audit, 2026-08-24). It read: "actor_id = %v,
	// want NULL: the encode actor is a caller-supplied STRING that nothing
	// authenticated" — the exact rationale internal/encode/rows.go now carries two
	// screens of retraction for. It stayed green only because this test passes
	// uuid.Nil, and it sat in the SAME FILE as an assertion demanding a real admin id.
	// A later reader repairing that binding would have found a live test telling them
	// nil was correct: the M2-08 trap, where a test's own words steer somebody into
	// reverting their own fix.
	//
	// WHAT IT REALLY TESTS, and what it is kept for: uuid.Nil means SYSTEM and reaches
	// the column as SQL NULL. That behaviour is shipped and right — see actorIDOf.
	// The admin arm is TestDBRows_EveryTrailRowOfARoundNamesTheAdmin.
	if ev.actorID != nil {
		t.Errorf("actor_id = %v, want NULL for a uuid.Nil admin: actorIDOf maps the nil UUID to "+
			"a NULL column ('the system'), and writing 00000000-… into a column that is joined "+
			"to admin_users would render an empty name on a manager's screen", ev.actorID)
	}
	var detail loadedDetail
	if err := json.Unmarshal(ev.detail, &detail); err != nil {
		t.Fatalf("the detail is not the struct this package writes: %v (%s)", err, ev.detail)
	}
	if detail.ClaimedBy != actor {
		t.Errorf("detail.claimed_by = %q, want %q", detail.ClaimedBy, actor)
	}
	if detail.KeyBytes != sun.WrappedKeyLen {
		t.Errorf("detail.key_bytes = %d, want %d", detail.KeyBytes, sun.WrappedKeyLen)
	}
}

// TestDBRows_ADuplicateUIDFailsAndTakesItsTrailEntryWithIt.
//
// 🔴 TWO PROPERTIES IN ONE CASE BECAUSE THEY ARE ONE MECHANISM. tags.uid is the
// PRIMARY KEY, so a second row for one physical chip must FAIL rather than
// overwrite (Rows.InsertUnassigned's contract, ADR 0017 §6 md. 12) -- an overwrite
// would replace a live plaque's wrapped key with another round's and brick it. And
// because the row and its trail entry share one transaction (see the trail port),
// the refused INSERT must leave NO second plaque.loaded entry: a trail row is
// evidence, and evidence for something that did not happen is worse than silence.
func TestDBRows_ADuplicateUIDFailsAndTakesItsTrailEntryWithIt(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	tenant := newEncodeTenant(t, d)
	uid := newUID(t)
	first := wrappedFor(t, uid)

	if err := rows.InsertUnassigned(context.Background(), tenant, uuid.Nil, uid, first, "operator-alpha"); err != nil {
		t.Fatalf("the first load failed: %v", err)
	}
	second := wrappedFor(t, uid)
	err := rows.InsertUnassigned(context.Background(), tenant, uuid.Nil, uid, second, "operator-beta")
	if err == nil {
		t.Fatal("a second load for the same uid SUCCEEDED; tags.uid is a PRIMARY KEY and " +
			"Rows.InsertUnassigned's contract is to fail rather than overwrite")
	}

	// The first envelope survived, byte for byte.
	var stored []byte
	ownerRead(t, func(ctx context.Context, c *pgx.Conn) error {
		return c.QueryRow(ctx, `SELECT aes_key_ref FROM tags WHERE uid = $1 AND tenant_id = $2`,
			uid, tenant).Scan(&stored)
	})
	if string(stored) != string(first) {
		t.Error("the stored envelope is not the first one; the second round overwrote a plaque's key")
	}

	if n := countTrail(t, d, tenant, uid, ActionPlaqueLoaded); n != 1 {
		t.Errorf("%d plaque.loaded entries after one success and one refusal, want exactly 1 -- "+
			"the entry must share the row's transaction", n)
	}
}

// TestDBRows_AFailedTrailWriteTAKESTHEROWWITHIT.
//
// 🔴 THIS IS THE MIRROR OF TestDBRows_ADuplicateUIDFailsAndTakesItsTrailEntryWithIt
// AND IT WAS MISSING, WHICH AN AUDIT MEASURED WITH ONE MUTATION (2026-08-24).
// Replacing the trail's error check with `_ = err` in InsertUnassigned left BOTH
// internal/encode and internal/db entirely GREEN, and neither `go vet` nor
// staticcheck saw it (measured: both exit 0). The other direction was pinned; this
// one was not, because every double in this file used the REAL audit.Recorder or a
// stub that cannot fail.
//
// WHAT THE MUTATION COSTS, end to end: the INSERT COMMITS, the trail entry is
// never written, and the caller is told SUCCESS. ADR 0017 §6 md. 8's decision is
// that the entry lives inside the row's transaction precisely so those two facts
// cannot disagree — and a swallowed error makes them disagree silently, which is
// the §4.6-shaped loss the trail exists to prevent.
//
// ⚠️ `_ = err` IS ALSO CLAUDE.md §7's EXPLICIT PROHIBITION ("Sessizce yutma yok;
// `_ = err` yok"), and the measurement above is why a prohibition needs a test:
// the two linters this repository runs do not enforce it.
func TestDBRows_AFailedTrailWriteTAKESTHEROWWITHIT(t *testing.T) {
	d := testDB(t)
	tenant := newEncodeTenant(t, d)

	boom := errors.New("the trail is unavailable")
	rows, err := NewDBRows(d, failingTrail{err: boom})
	if err != nil {
		t.Fatalf("NewDBRows: %v", err)
	}
	ctx := context.Background()

	// (1) INSERT: the error must surface AND the row must not survive.
	uid := newUID(t)
	insertErr := rows.InsertUnassigned(ctx, tenant, uuid.Nil, uid, wrappedFor(t, uid), "operator-alpha")
	if insertErr == nil {
		t.Fatal("InsertUnassigned reported SUCCESS while its trail entry failed. The row is " +
			"committed, the trail is empty, and the caller believes the plaque is loaded")
	}
	if !errors.Is(insertErr, boom) {
		t.Errorf("the trail's error was not propagated: %v", insertErr)
	}
	var rowsForUID int
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1 AND tenant_id = $2`,
			uid, tenant).Scan(&rowsForUID)
	})
	if rowsForUID != 0 {
		t.Errorf("%d tags row(s) survived a failed trail write; the entry and the row share one "+
			"transaction (ADR 0017 §6 md. 8) so neither may outlive the other", rowsForUID)
	}

	// (2) MARK: same rule on the other statement. The row is loaded by a WORKING
	// port first, so the only thing failing is the trail.
	good := newDBRows(t, d)
	uid2 := newUID(t)
	if err := good.InsertUnassigned(ctx, tenant, uuid.Nil, uid2, wrappedFor(t, uid2), "operator-alpha"); err != nil {
		t.Fatalf("the fixture load failed: %v", err)
	}
	markErr := rows.MarkEncoded(ctx, tenant, uuid.Nil, uid2, "operator-alpha")
	if markErr == nil {
		t.Fatal("MarkEncoded reported SUCCESS while its trail entry failed")
	}
	if !errors.Is(markErr, boom) {
		t.Errorf("the trail's error was not propagated: %v", markErr)
	}
	var stamped bool
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT encoded_at IS NOT NULL FROM tags WHERE uid = $1 AND tenant_id = $2`,
			uid2, tenant).Scan(&stamped)
	})
	if stamped {
		t.Error("the marker survived a failed trail write; the UPDATE and the entry share one " +
			"transaction")
	}

	// POSITIVE CONTROL: with a WORKING trail the same two calls succeed. Without
	// it, a port that failed unconditionally would pass everything above.
	uid3 := newUID(t)
	if err := good.InsertUnassigned(ctx, tenant, uuid.Nil, uid3, wrappedFor(t, uid3), "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
	if err := good.MarkEncoded(ctx, tenant, uuid.Nil, uid3, "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
}

// failingTrail is the double the suite did not have: an audit recorder that
// RETURNS AN ERROR. stubTrail (ports_test.go) always succeeds, and the real
// audit.Recorder only fails when the database does, so neither can drive the arm
// above.
type failingTrail struct{ err error }

func (f failingTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, f.err
}

func (f failingTrail) Record(context.Context, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, f.err
}

// TestDBRows_TheTrailDetailKeysAreTheDecidedONES.
//
// 🔴 IT READS THE RAW jsonb KEYS, NOT THE STRUCT, AND THAT IS THE ENTIRE POINT
// (audit, 2026-08-24). ADR 0017 §6 md. 8 decided the field is named `claimed_by`
// and NOT `actor`, so that nobody is invited to promote an unverified string into
// audit_log.actor_id — the column a manager's screen renders as a name. Nothing
// pinned it: rows_db_test.go unmarshals `detail` into the SAME struct the writer
// marshals from, so renaming the json tag renames BOTH SIDES and the suite stays
// green. Measured, as the auditor's mutation N6.
//
// Reading the keys out of PostgreSQL closes that: the database has no opinion about
// Go struct tags.
func TestDBRows_TheTrailDetailKeysAreTheDecidedONES(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	tenant := newEncodeTenant(t, d)
	uid := newUID(t)
	ctx := context.Background()

	if err := rows.InsertUnassigned(ctx, tenant, uuid.Nil, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, tenant, uuid.Nil, uid, "operator-alpha"); err != nil {
		t.Fatalf("MarkEncoded: %v", err)
	}

	for _, c := range []struct {
		action string
		want   []string
	}{
		{ActionPlaqueLoaded, []string{"claimed_by", "key_bytes"}},
		{ActionPlaqueEncoded, []string{"claimed_by"}},
	} {
		var keys []string
		readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT array_agg(k ORDER BY k) FROM audit_log a, jsonb_object_keys(a.detail) k
				  WHERE a.tenant_id = $1 AND a.target = $2 AND a.action = $3`,
				tenant, uid, c.action).Scan(&keys)
		})
		if strings.Join(keys, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s detail keys = %v, want %v. The names are ADR 0017 §6 md. 8's decision: "+
				"`claimed_by` says the value is a CLAIM by the caller, which is why it must not "+
				"be called `actor` -- audit_log.actor_id is the column a manager reads as "+
				"\"who did this\", and this value is authenticated by nothing",
				c.action, keys, c.want)
		}
		// 🔴 AND THE FORBIDDEN NAME IS NAMED, so the rule reads as a rule rather
		// than as an accident of the list above.
		for _, k := range keys {
			if k == "actor" || k == "actor_id" {
				t.Errorf("%s detail carries a key called %q", c.action, k)
			}
		}
	}
}

// TestDBRows_TheRefusalOfACrossTenantLoadCarriesNoKeyBytes.
//
// 🔴 §4.7 ON THE ERROR PATH, WHICH IS WHERE IT IS EASIEST TO LOSE. Two refusals are
// driven -- a mis-sized envelope (refused in Go, before SQL) and a duplicate uid
// (refused by Postgres) -- and neither message may carry envelope bytes. The second
// is the one that needs measuring: migration 00021 recorded that a CHECK
// violation's DETAIL line is the WHOLE FAILING TUPLE, aes_key_ref included, and
// that pgconn's Error() excludes Detail. This asserts the consequence rather than
// trusting the reading.
func TestDBRows_TheRefusalOfACrossTenantLoadCarriesNoKeyBytes(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	tenant := newEncodeTenant(t, d)
	uid := newUID(t)
	ref := wrappedFor(t, uid)

	short := ref[:len(ref)-1]
	err := rows.InsertUnassigned(context.Background(), tenant, uuid.Nil, uid, short, "operator-alpha")
	if err == nil {
		t.Fatal("a 43-byte envelope was accepted; 00021's tags_aes_key_ref_is_kek_envelope " +
			"would have refused it in the database and printed the whole tuple doing so")
	}
	assertCarriesNoBytes(t, err, short)

	if err := rows.InsertUnassigned(context.Background(), tenant, uuid.Nil, uid, ref, "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
	dup := rows.InsertUnassigned(context.Background(), tenant, uuid.Nil, uid, ref, "operator-alpha")
	if dup == nil {
		t.Fatal("the duplicate was accepted")
	}
	assertCarriesNoBytes(t, dup, ref)
}

// --- ADR 0017 §5.1 step 9: the marker ------------------------------------------

// TestDBRows_MarkEncodedStampsTheServerClockAndIsIdempotent.
//
// IDEMPOTENCE IS A REQUIREMENT AND NOT A CONVENIENCE: step 9 runs after the keys
// are wiped, it runs even on a cancelled context (Store.Step), and Progress.Done
// tells the caller not to re-run -- so the one repeat the flow can produce is a
// retried marker. A retry that FAILED would send an operator to a chip that is
// already personalised; a retry that MOVED the timestamp would make "when was this
// chip personalised" a function of how many times somebody pressed a button.
func TestDBRows_MarkEncodedStampsTheServerClockAndIsIdempotent(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	tenant := newEncodeTenant(t, d)
	uid := newUID(t)
	ctx := context.Background()

	if err := rows.InsertUnassigned(ctx, tenant, uuid.Nil, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, tenant, uuid.Nil, uid, "operator-alpha"); err != nil {
		t.Fatalf("MarkEncoded: %v", err)
	}

	var first string
	var status string
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT encoded_at::text, status FROM tags WHERE uid = $1 AND tenant_id = $2`,
			uid, tenant).Scan(&first, &status)
	})
	if first == "" {
		t.Fatal("encoded_at is still NULL after MarkEncoded")
	}
	// 🔴 THE STATUS MUST NOT HAVE MOVED. The M8-05 card's second trap is writing
	// 'active' here; the marker records that the CHIP is done, not that the plaque
	// is on a wall.
	if status != "unassigned" {
		t.Errorf("status = %q after the marker, want unassigned", status)
	}

	if err := rows.MarkEncoded(ctx, tenant, uuid.Nil, uid, "operator-alpha"); err != nil {
		t.Fatalf("a second MarkEncoded failed: %v -- a retry must not look like a failure", err)
	}
	var second string
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT encoded_at::text FROM tags WHERE uid = $1 AND tenant_id = $2`,
			uid, tenant).Scan(&second)
	})
	if second != first {
		t.Errorf("the marker moved on a retry: %s -> %s. coalesce(encoded_at, now()) is what "+
			"keeps it still, and 00022's trigger is what keeps every OTHER writer still", first, second)
	}

	// Both entries are in the trail, and they are two DIFFERENT facts.
	if n := countTrail(t, d, tenant, uid, ActionPlaqueLoaded); n != 1 {
		t.Errorf("plaque.loaded entries = %d, want 1", n)
	}
	if n := countTrail(t, d, tenant, uid, ActionPlaqueEncoded); n != 2 {
		t.Errorf("plaque.encoded entries = %d, want 2 -- the marker is idempotent on the ROW, "+
			"and audit_log is append-only, so a second attempt is a second (true) entry", n)
	}
}

// TestDBRows_MarkEncodedRefusesAPlaqueThatIsNotThisTenants.
//
// It is NOT an RLS isolation proof -- the statement carries an explicit tenant
// predicate (§4.5's belt), so zero rows here would be the WHERE's doing. The
// isolation proof is TestRLS_DBRows_ATenantCannotLoadOrMarkAnothersPlaque below,
// and it carries no filter at all. What this asserts is the PORT's behaviour: a
// miss is an ERROR the caller can act on, never a silent success.
func TestDBRows_MarkEncodedRefusesAPlaqueThatIsNotThisTenants(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	a, b := newEncodeTenant(t, d), newEncodeTenant(t, d)
	uid := newUID(t)
	ctx := context.Background()

	if err := rows.InsertUnassigned(ctx, a, uuid.Nil, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, b, uuid.Nil, uid, "operator-beta"); err == nil {
		t.Fatal("tenant B marked tenant A's plaque as encoded")
	}
	// A's row is untouched and A can still mark it -- the positive control that
	// separates "the policy worked" from "the fixture was broken".
	if err := rows.MarkEncoded(ctx, a, uuid.Nil, uid, "operator-alpha"); err != nil {
		t.Fatalf("the owning tenant could not mark its own plaque: %v", err)
	}
	// ⚠️ THIS ASSERTION IS NARROWER THAN THE SENTENCE ABOVE IT, AND THE GAP IS
	// DELIBERATE RATHER THAN OVERLOOKED (eleventh audit, 2026-08-25). It counts
	// plaque.encoded only. B's refused marking DOES write a row — plaque.unmarked,
	// in B — because ErrNoRows leaves the write path intact and the compensation
	// therefore fires. So "wrote nothing for a plaque it does not own" is not what is
	// measured here, and tightening it is a PRODUCT decision rather than a test one:
	// that row is a false claim ("a chip was personalised") in an append-only table.
	// Counted at MarkEncoded's ErrNoRows comment and on the hand-over list as md. 28.
	//
	// What this assertion DOES hold is the isolation property it was written for:
	// tenant B cannot mark, and cannot record a MARKING, for tenant A's plaque.
	if n := countTrail(t, d, b, uid, ActionPlaqueEncoded); n != 0 {
		t.Errorf("tenant B wrote %d plaque.encoded entr(ies) for a plaque it does not own", n)
	}
}

// --- RLS isolation (CLAUDE.md §8) ----------------------------------------------

// TestRLS_DBRows_ATenantCannotLoadOrMarkAnothersPlaque.
//
// 🔴 NO EXPLICIT tenant_id FILTER APPEARS IN THIS TEST, AND THAT IS CLAUDE.md §6'S
// OWN INSTRUCTION rather than an omission: with a filter, the 0 rows would be the
// WHERE's doing, and the test would stay GREEN with row-level security switched
// off. That is why it uses raw SQL through WithTenant instead of the shipped
// statements -- every shipped statement carries the belt by rule, so no store call
// can isolate the policy.
//
// NON-VACUITY WAS MEASURED, NOT ASSUMED: with
// `ALTER TABLE tags DISABLE ROW LEVEL SECURITY` in force this test turns RED, and
// green again once ENABLE + FORCE are restored. Both runs are in the task report.
//
// EVERY NEGATIVE ASSERTION IS PAIRED WITH A POSITIVE CONTROL in the owning tenant,
// because a read that is wrongly filtered and a policy that works look identical
// from the failing side.
func TestRLS_DBRows_ATenantCannotLoadOrMarkAnothersPlaque(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	a, b := newEncodeTenant(t, d), newEncodeTenant(t, d)
	uid := newUID(t)
	ctx := context.Background()

	if err := rows.InsertUnassigned(ctx, a, uuid.Nil, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, a, uuid.Nil, uid, "operator-alpha"); err != nil {
		t.Fatalf("MarkEncoded: %v", err)
	}

	// B's connection, filter-less.
	if err := d.WithTenant(ctx, b, func(ctx context.Context, tx pgx.Tx) error {
		var seen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1`, uid).Scan(&seen); err != nil {
			return err
		}
		if seen != 0 {
			t.Errorf("tenant B sees %d row(s) of tenant A's plaque with no tenant predicate at all", seen)
		}
		var marked int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE encoded_at IS NOT NULL`).Scan(&marked); err != nil {
			return err
		}
		if marked != 0 {
			t.Errorf("tenant B sees %d encoded marker(s) that are not its own", marked)
		}
		// An UPDATE is the silent one: with the policy gone it reports success.
		ct, err := tx.Exec(ctx, `UPDATE tags SET status = 'lost' WHERE uid = $1`, uid)
		if err != nil {
			return err
		}
		if n := ct.RowsAffected(); n != 0 {
			t.Errorf("tenant B updated %d row(s) of tenant A's plaque", n)
		}
		// And the trail: B must not see A's entries either.
		var trail int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE target = $1`, uid).Scan(&trail); err != nil {
			return err
		}
		if trail != 0 {
			t.Errorf("tenant B sees %d of tenant A's audit entries for this plaque", trail)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant B's probe failed for a reason that is not isolation: %v", err)
	}

	// POSITIVE CONTROL, also filter-less: A sees exactly its own row and its two
	// trail entries. Without this the whole test passes on a broken fixture.
	if err := d.WithTenant(ctx, a, func(ctx context.Context, tx pgx.Tx) error {
		var seen, marked, trail int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1`, uid).Scan(&seen); err != nil {
			return err
		}
		if seen != 1 {
			t.Errorf("tenant A sees %d row(s) of its OWN plaque, want 1", seen)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE uid = $1 AND encoded_at IS NOT NULL`, uid).Scan(&marked); err != nil {
			return err
		}
		if marked != 1 {
			t.Errorf("tenant A sees %d marker(s) on its own plaque, want 1", marked)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE target = $1`, uid).Scan(&trail); err != nil {
			return err
		}
		if trail != 2 {
			t.Errorf("tenant A sees %d trail entr(ies) for its own plaque, want 2 "+
				"(plaque.loaded and plaque.encoded)", trail)
		}
		return nil
	}); err != nil {
		t.Fatalf("tenant A's control failed: %v", err)
	}
}

// --- The port's argument checks -------------------------------------------------

// TestDBRows_BothMethodsRefuseAMissingTenantBeforeTouchingTheDatabase.
//
// The nil UUID is a valid uuid literal. db.WithTenant refuses it, so this check is
// belt over an existing brace -- but the two produce DIFFERENT sentences, and the
// one that names the argument is the one an operator can act on.
func TestDBRows_BothMethodsRefuseAMissingTenantBeforeTouchingTheDatabase(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	tenant := newEncodeTenant(t, d)
	uid := newUID(t)
	ref := wrappedFor(t, uid)
	ctx := context.Background()

	cases := []struct {
		name string
		run  func(uuid.UUID, string, string) error
	}{
		{"insert", func(tn uuid.UUID, u, a string) error {
			return rows.InsertUnassigned(ctx, tn, uuid.Nil, u, ref, a)
		}},
		{"mark", func(tn uuid.UUID, u, a string) error { return rows.MarkEncoded(ctx, tn, uuid.Nil, u, a) }},
	}
	for _, c := range cases {
		if err := c.run(uuid.Nil, uid, "operator-alpha"); err == nil {
			t.Errorf("%s accepted the nil tenant", c.name)
		}
		if err := c.run(tenant, "", "operator-alpha"); err == nil {
			t.Errorf("%s accepted an empty uid", c.name)
		}
		if err := c.run(tenant, uid, ""); err == nil {
			t.Errorf("%s accepted an empty actor label", c.name)
		}
		if err := c.run(tenant, uid, strings.Repeat("a", MaxActorLen+1)); err == nil {
			t.Errorf("%s accepted an actor label over MaxActorLen; audit_log.detail is "+
				"append-only for every role", c.name)
		}
	}

	// POSITIVE CONTROL: the same arguments, correct, are accepted -- otherwise a
	// port that refused everything would pass every case above.
	if err := rows.InsertUnassigned(ctx, tenant, uuid.Nil, uid, ref, "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
	if err := rows.MarkEncoded(ctx, tenant, uuid.Nil, uid, "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
}

// TestNewDBRows_RefusesToShipWithoutATrail.
//
// A Rows that skipped the trail would satisfy the interface and silently drop ADR
// 0017 §5.2's obligation -- and the loss would only be visible the day somebody
// asked who loaded a plaque.
func TestNewDBRows_RefusesToShipWithoutATrail(t *testing.T) {
	d := testDB(t)
	if _, err := NewDBRows(nil, nil); err == nil {
		t.Error("NewDBRows accepted a nil database")
	}
	if _, err := NewDBRows(d, nil); err == nil {
		t.Error("NewDBRows accepted a nil trail; the audit entry is not optional (ADR 0017 §5.2)")
	}
	rec, err := audit.New(d)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	if _, err := NewDBRows(d, rec); err != nil {
		t.Errorf("NewDBRows refused a correct pair: %v", err)
	}
}

// --- helpers --------------------------------------------------------------------

type trailEntry struct {
	actorID *uuid.UUID
	detail  []byte
}

func onlyTrailEntry(t *testing.T, d *db.DB, tenant uuid.UUID, uid, action string) trailEntry {
	t.Helper()
	var out trailEntry
	var n int
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = $3`,
			tenant, uid, action).Scan(&n); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`SELECT actor_id, detail FROM audit_log
			  WHERE tenant_id = $1 AND target = $2 AND action = $3 ORDER BY at DESC LIMIT 1`,
			tenant, uid, action).Scan(&out.actorID, &out.detail)
	})
	if n != 1 {
		t.Fatalf("%d %s entr(ies) for %s, want exactly 1", n, action, uid)
	}
	return out
}

func countTrail(t *testing.T, d *db.DB, tenant uuid.UUID, uid, action string) int {
	t.Helper()
	var n int
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = $3`,
			tenant, uid, action).Scan(&n)
	})
	return n
}

func readAs(t *testing.T, d *db.DB, tenant uuid.UUID, fn db.TxFunc) {
	t.Helper()
	if err := d.WithTenant(context.Background(), tenant, fn); err != nil {
		t.Fatalf("read as %s: %v", tenant, err)
	}
}

// assertCarriesNoBytes is the §4.7 check on an error path: no run of four or more
// bytes from the value may appear in the message, in raw or hex form.
//
// ⚠️ IT IS A HEURISTIC AND IT OVER-REPORTS BY DESIGN. Four bytes is short enough
// that a coincidence is possible and long enough that no accidental echo of an
// envelope survives it. It cannot prove the message is free of every derivative of
// the value; what it catches is the shape that actually happens -- a %v or a %x of
// the argument.
func assertCarriesNoBytes(t *testing.T, err error, value []byte) {
	t.Helper()
	msg := err.Error()
	hexed := hex.EncodeToString(value)
	for i := 0; i+4 <= len(value); i++ {
		if strings.Contains(msg, string(value[i:i+4])) {
			t.Errorf("the error carries raw envelope bytes at offset %d: %q", i, msg)
			return
		}
	}
	for i := 0; i+8 <= len(hexed); i += 2 {
		if strings.Contains(strings.ToLower(msg), hexed[i:i+8]) {
			t.Errorf("the error carries hex-encoded envelope bytes at offset %d: %q", i/2, msg)
			return
		}
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
}

// TestDBRows_AFailedMarkerLeavesAPERMANENTTraceThatSaysDoNotReEncode is the third
// round's security finding, closed (tappa-security-auditor F3, 2026-08-24).
//
// 🔴 THE TWO STATES THIS SEPARATES WERE BYTE-IDENTICAL IN THE DATABASE AND HAVE
// OPPOSITE RECOVERY INSTRUCTIONS:
//
//	the chip WAS personalised and the row could not be marked  ->  do NOT re-run
//	the round died before touching the chip                    ->  DO re-run
//
// Both left `tags` at status 'unassigned' with a NULL location and a NULL
// encoded_at, and both left exactly one plaque.loaded in audit_log. The only thing
// telling them apart was one line in the PROCESS LOG — the shape M8-03 refused, since
// log rotation removes it and audit_log (which no role may delete, 00005's trigger)
// never heard about the event.
//
// This drives the real port against real Postgres with a trail whose IN-TRANSACTION
// half fails, which is exactly the shape a marker failure has.
func TestDBRows_AFailedMarkerLeavesAPERMANENTTraceThatSaysDoNotReEncode(t *testing.T) {
	d := testDB(t)
	tenant := newEncodeTenant(t, d)
	ctx := context.Background()

	// The row is loaded by a working port; the chip is personalised at this point in
	// the round, which is why MarkEncoded is being called at all.
	good := newDBRows(t, d)
	uid := newUID(t)
	admin := newEncodeAdmin(t, d, tenant)
	if err := good.InsertUnassigned(ctx, tenant, admin, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("the fixture load failed: %v", err)
	}

	// A trail whose RecordTx fails (the marker's transaction rolls back) but whose
	// Record succeeds — the split the port now makes.
	rec, err := audit.New(d)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	rows, err := NewDBRows(d, halfFailingTrail{real: rec, err: errors.New("trail boom")})
	if err != nil {
		t.Fatalf("NewDBRows: %v", err)
	}

	if err := rows.MarkEncoded(ctx, tenant, admin, uid, "operator-alpha"); err == nil {
		t.Fatal("MarkEncoded reported SUCCESS while its trail entry failed")
	}

	// The row is unchanged — that half is the existing guarantee.
	var stamped bool
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT encoded_at IS NOT NULL FROM tags WHERE uid = $1 AND tenant_id = $2`,
			uid, tenant).Scan(&stamped)
	})
	if stamped {
		t.Error("the marker survived a failed trail write")
	}

	// 🔴 AND THE PERMANENT RECORD NOW SAYS WHICH OF THE TWO STATES THIS IS.
	var actions []string
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		r, e := tx.Query(ctx,
			`SELECT action FROM audit_log WHERE tenant_id = $1 AND target = $2 ORDER BY action`,
			tenant, uid)
		if e != nil {
			return e
		}
		defer r.Close()
		for r.Next() {
			var a string
			if e := r.Scan(&a); e != nil {
				return e
			}
			actions = append(actions, a)
		}
		return r.Err()
	})

	want := []string{ActionPlaqueLoaded, ActionPlaqueUnmarked}
	if !reflect.DeepEqual(actions, want) {
		t.Fatalf("audit_log for %s holds %v, want %v.\n"+
			"Without the second entry this row is indistinguishable from a round that never "+
			"touched the chip, and the two have OPPOSITE recovery instructions. The signal "+
			"cannot live only in the process log — that is the M8-03 shape.", uid, actions, want)
	}

	// It is attributable and it carries no error text: one field, the operator label.
	var detail map[string]any
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		var raw []byte
		if e := tx.QueryRow(ctx,
			`SELECT detail FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = $3`,
			tenant, uid, ActionPlaqueUnmarked).Scan(&raw); e != nil {
			return e
		}
		return json.Unmarshal(raw, &detail)
	})
	// 🔴 AND THE ROW IS ATTRIBUTED TO THE ADMIN, NOT TO "THE SYSTEM" (audit N3). The
	// port now carries a RESOLVED admin id and writes it to audit_log.actor_id; until
	// the FIFTH round it wrote nil and the plaque card said "by the system" for
	// something a person did. (This said "the fourth round"; the fourth was the
	// security audit that opened the finding, the fifth is where the binding shipped.)
	var gotActor *uuid.UUID
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT actor_id FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = $3`,
			tenant, uid, ActionPlaqueUnmarked).Scan(&gotActor)
	})
	if gotActor == nil || *gotActor != admin {
		t.Fatalf("actor_id = %v, want %s — the trail must name the admin who drove the round",
			gotActor, admin)
	}

	if len(detail) != 1 || detail["claimed_by"] != "operator-alpha" {
		t.Fatalf("the %s detail is %v, want exactly {claimed_by: operator-alpha}. A database "+
			"error's text belongs in the log, not on a row nobody can ever delete",
			ActionPlaqueUnmarked, detail)
	}

	// POSITIVE CONTROL: a marker that SUCCEEDS writes plaque.encoded and no unmarked
	// entry, so the two arms are genuinely different rather than both firing.
	uid2 := newUID(t)
	if err := good.InsertUnassigned(ctx, tenant, uuid.Nil, uid2, wrappedFor(t, uid2), "operator-alpha"); err != nil {
		t.Fatalf("the positive control load failed: %v", err)
	}
	if err := good.MarkEncoded(ctx, tenant, uuid.Nil, uid2, "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
	var control []string
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		r, e := tx.Query(ctx,
			`SELECT action FROM audit_log WHERE tenant_id = $1 AND target = $2 ORDER BY action`,
			tenant, uid2)
		if e != nil {
			return e
		}
		defer r.Close()
		for r.Next() {
			var a string
			if e := r.Scan(&a); e != nil {
				return e
			}
			control = append(control, a)
		}
		return r.Err()
	})
	if !reflect.DeepEqual(control, []string{ActionPlaqueEncoded, ActionPlaqueLoaded}) {
		t.Fatalf("a SUCCESSFUL round wrote %v; the unmarked entry must not fire on the happy "+
			"path or it stops distinguishing anything", control)
	}
}

// halfFailingTrail fails the IN-TRANSACTION write and succeeds the own-transaction
// one — the exact shape a marker failure has, and the only way to drive the arm
// where the row rolls back but the evidence must not.
type halfFailingTrail struct {
	real *audit.Recorder
	err  error
}

func (h halfFailingTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, h.err
}

func (h halfFailingTrail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	return h.real.Record(ctx, e)
}

// TestDBRows_EveryTrailRowOfARoundNamesTheAdmin is ADR 0017 §6 md. 8's actor decision
// at the COLUMN, on the path a round actually takes.
//
// 🔴 IT EXISTS BECAUSE THE BINDING WAS UNDEFENDED ON THE SUCCESS PATH AND AN AUDITOR
// PROVED IT (2026-08-24). Reverting BOTH `ActorID: actorIDOf(adminID)` sites in
// InsertUnassigned and MarkEncoded to nil left the entire repository green — 25
// packages, zero failures, real Postgres. Three things measured "up to" the column and
// none measured it: the failure-path test pinned only plaque.unmarked, the handler test
// pinned that the id reaches the PORT (against a fake), and the AST pin fixed the
// EXPRESSION at the call site. The one round-trip nobody asserted was the one that
// matters.
//
// 🔴 AND IT IS WRITTEN OVER THE ROWS THAT EXIST, NOT OVER A LIST OF THEM. The
// temptation was "assert plaque.loaded's actor, then plaque.encoded's" — which is a
// place-count, and a place-count goes silently short the day a fourth event is added.
// That is hand-over item 14's own lesson. So the claim is: FOR EVERY audit_log row this
// round produced, whatever its action, actor_id is the admin. A new event is covered
// the day it is written, without this file being touched.
//
// ⚠️ THE ONE THING THE QUERY STILL NAMES, counted rather than glossed: it selects on
// `target = uid`. All three shipped events use the plaque's uid as their target, so
// "every row of this round" and "every row for this uid" are the same set today. An
// event targeting something else — a location, a session handle — would be outside
// this net. Nothing does; and this is a narrower assumption than a list of actions,
// which is why it is the one that was kept.
func TestDBRows_EveryTrailRowOfARoundNamesTheAdmin(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	tenant := newEncodeTenant(t, d)
	admin := newEncodeAdmin(t, d, tenant)
	uid := newUID(t)
	ctx := context.Background()
	const actor = "operator-alpha"

	// A COMPLETE, SUCCESSFUL round: the row, then the marker. Both write a trail entry.
	if err := rows.InsertUnassigned(ctx, tenant, admin, uid, wrappedFor(t, uid), actor); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, tenant, admin, uid, actor); err != nil {
		t.Fatalf("MarkEncoded: %v", err)
	}

	type row struct {
		action  string
		actorID *uuid.UUID
	}
	var got []row
	readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
		r, err := tx.Query(ctx,
			`SELECT action, actor_id FROM audit_log
			  WHERE tenant_id = $1 AND target = $2 ORDER BY action`, tenant, uid)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var one row
			if err := r.Scan(&one.action, &one.actorID); err != nil {
				return err
			}
			got = append(got, one)
		}
		return r.Err()
	})

	// ANTI-VACUITY: a round that wrote nothing would satisfy "every row names the
	// admin" for free. Two is what a successful round owes (§5.2 and §5.1 step 9).
	if len(got) < 2 {
		t.Fatalf("a successful round left %d trail row(s) for %s (%v); it owes at least two, "+
			"and without them the assertion below is vacuous", len(got), uid, got)
	}

	for _, one := range got {
		if one.actorID == nil {
			t.Errorf("%s has actor_id NULL. Every row a round writes must name the admin who "+
				"drove it — the plaque card renders NULL as 'by the system', which is untrue of "+
				"an encode a person ran", one.action)
			continue
		}
		if *one.actorID != admin {
			t.Errorf("%s has actor_id %s, want the round's admin %s", one.action, *one.actorID, admin)
		}
	}
	// Guarded: under the B1 mutation this line printed "all naming the admin" directly
	// beneath two t.Errorf calls saying the opposite. A wrong verdict stops the next
	// reader looking — this session's own lesson, applied to the test that carries it.
	if !t.Failed() {
		t.Logf("%d trail row(s) for this round, all naming the admin: %v", len(got), got)
	}
}

// TestDBRows_TheUnmarkedTrailSurvivesACancelledRequest pins the PORT's own guarantee:
// whatever context DBRows is handed, a failed marking still leaves a durable record.
//
// 🔴 ITS RATIONALE WAS REWRITTEN IN THE NINTH ROUND, AND THE OLD ONE IS RETRACTED.
// This test used to say it was driving "the ORDINARY trigger ... the phone posting its
// last R-APDU and hanging up". That trigger no longer reaches here: session.go's
// markEncoded detaches from the request, so an ordinary hang-up is REPAIRED and this
// port is never asked to record anything. Leaving the old sentence would have taught
// the next reader that a hung-up phone still files damage, which is exactly backwards.
//
// WHAT THIS TEST IS FOR NOW, and it is a different and smaller thing: DBRows must not
// assume its caller detached. A cancelled context arriving HERE means the layer above
// changed, or a future caller passed one straight through, and in that case the chip
// may already be personalised while the row is not marked — the state whose recovery
// instruction is the opposite of "re-encode". The port keeps its own guarantee rather
// than borrowing one from a caller two files away.
//
// ⚠️ WHY THE ORIGINAL DEFECT WAS INVISIBLE, kept because it is the lesson: the
// pre-existing test forced the failure with halfFailingTrail, a shape only a DATABASE
// fault produces. There was no context.WithCancel anywhere in this file.
func TestDBRows_TheUnmarkedTrailSurvivesACancelledRequest(t *testing.T) {
	d := testDB(t)
	rows := newDBRows(t, d)
	tenant := newEncodeTenant(t, d)
	admin := newEncodeAdmin(t, d, tenant)

	unmarked := func(uid string) int {
		t.Helper()
		var n int
		readAs(t, d, tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = $3`,
				tenant, uid, ActionPlaqueUnmarked).Scan(&n)
		})
		return n
	}

	// THE PROBE: a cancelled request context, which is what a relay hanging up looks
	// like from here. The marking must fail and the evidence must survive.
	uid := newUID(t)
	if err := rows.InsertUnassigned(context.Background(), tenant, admin, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("the fixture load failed: %v", err)
	}
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rows.MarkEncoded(dead, tenant, admin, uid, "operator-alpha"); err == nil {
		t.Fatal("MarkEncoded reported SUCCESS on a cancelled context; the probe below would " +
			"be measuring the happy path")
	}
	if n := unmarked(uid); n != 1 {
		t.Fatalf("a cancelled request left %d %s row(s), want 1.\n"+
			"The marking and its EVIDENCE must not fail for the same reason: this row is the "+
			"only thing distinguishing 'the chip is personalised and the row is not marked' "+
			"from 'the round never touched the chip', and those two have OPPOSITE recovery "+
			"instructions.", n, ActionPlaqueUnmarked)
	}

	// PAIRED ARM: the same failure on a LIVE context still records. Without this the
	// assertion above would pass over a port that wrote the row unconditionally.
	rec, err := audit.New(d)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	half, err := NewDBRows(d, halfFailingTrail{real: rec, err: errors.New("trail boom")})
	if err != nil {
		t.Fatalf("NewDBRows: %v", err)
	}
	uid2 := newUID(t)
	if err := rows.InsertUnassigned(context.Background(), tenant, admin, uid2, wrappedFor(t, uid2), "operator-alpha"); err != nil {
		t.Fatalf("the paired-arm load failed: %v", err)
	}
	if err := half.MarkEncoded(context.Background(), tenant, admin, uid2, "operator-alpha"); err == nil {
		t.Fatal("the paired arm did not fail its marking")
	}
	if n := unmarked(uid2); n != 1 {
		t.Fatalf("the live-context arm left %d %s row(s), want 1", n, ActionPlaqueUnmarked)
	}

	// NEGATIVE CONTROL: a SUCCESSFUL marking writes no unmarked row at all, so the two
	// assertions above are distinguishing something rather than always firing.
	uid3 := newUID(t)
	if err := rows.InsertUnassigned(context.Background(), tenant, admin, uid3, wrappedFor(t, uid3), "operator-alpha"); err != nil {
		t.Fatalf("the negative-control load failed: %v", err)
	}
	if err := rows.MarkEncoded(context.Background(), tenant, admin, uid3, "operator-alpha"); err != nil {
		t.Fatalf("the negative control failed: %v", err)
	}
	if n := unmarked(uid3); n != 0 {
		t.Fatalf("a SUCCESSFUL round wrote %d %s row(s); the event must fire only when the "+
			"marking failed", n, ActionPlaqueUnmarked)
	}
}

// TestDBStore_AnOrdinaryHangUpIsREPAIRED_NotFiledAsDamage is the ninth audit's own
// two-arm probe, shipped as a permanent test, against real Postgres.
//
// 🔴 THE EIGHTH ROUND FIXED THE WRONG HALF, AND THE NINTH MEASURED THE RIGHT ONE.
// Round eight detached the trail entry that DOCUMENTS a failed marking, and called
// that closed. The auditor applied the same one-line mechanism to the MARKING itself
// and got:
//
//	ARM1  shipped (request ctx)   err=true   encoded_at=<nil>  unmarked=1
//	ARM2  detached (this)         err=false  encoded_at=set    unmarked=0
//
// At the trigger this package's own comment calls "ORDINARY for an HTTP relay", the
// round was filing a REPORT OF DAMAGE that the same mechanism could simply avoid.
// tags.encoded_at is the only record that a chip was personalised; past Progress.Done
// the chip is irreversibly written, so letting a hung-up phone decide whether the
// database catches up was never the right trade.
//
// ⚠️ AND THE HONEST QUALIFIER, IN THE AUDITOR'S OWN WORDS: round eight was not a
// regression — before it the row was both unmarked AND unrecorded; after it, unmarked
// but recorded, which is strictly better. What this test adds is the state where it is
// neither.
func TestDBStore_AnOrdinaryHangUpIsREPAIRED_NotFiledAsDamage(t *testing.T) {
	d := testDB(t)
	tenant := newEncodeTenant(t, d)
	admin := newEncodeAdmin(t, d, tenant)

	wrapper, err := NewKEKWrapper(bytesOf(sun.KEKLen, 0x3B))
	if err != nil {
		t.Fatalf("NewKEKWrapper: %v", err)
	}
	fc := newFakeClock()
	armed := false
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	st, err := NewStore(Config{
		Rows:    newDBRows(t, d),
		Wrapper: wrapper,
		BaseURL: testBaseURL,
		// The cancellation must land AFTER checkout and INSIDE the final exchange —
		// checkout reads ctx.Err() first, so any earlier cancellation stops the round
		// before it can reach the marking at all.
		Clock: cancellingClock{inner: fc, cancel: cancel, armed: &armed},
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if cerr := st.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	})

	chip := newFakeChip(t)
	// 🔴 A UNIQUE UID PER RUN, AND THIS IS NOT COSMETIC. newFakeChip carries a FIXED
	// uid, which is fine against the in-memory ports every other round test uses and
	// WRONG here: tags.uid is a global PRIMARY KEY in a database that persists between
	// runs, so a shared-uid test passes exactly once and then fails on a duplicate key
	// for ever. Caught by re-running this test's own mutation restore, which reported
	// FAIL on a tree that was correct.
	if _, err := rand.Read(chip.uid); err != nil {
		t.Fatalf("mint a chip uid: %v", err)
	}
	chip.uid[0] = 0x04 // NXP, like every real NTAG 424 DNA

	id, p, err := st.Begin(ctx, tenant, admin, "operator-alpha")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for i := 0; i < len(roundSteps)-1; i++ {
		p, err = st.Step(ctx, id, chip.Transceive(p.Command))
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if p.Done {
			t.Fatalf("the round finished early, at step %d", i)
		}
	}
	armed = true // the phone posts its last R-APDU and hangs up
	p, err = st.Step(ctx, id, chip.Transceive(p.Command))

	uid := strings.ToUpper(hex.EncodeToString(chip.uid))
	if !p.Done {
		t.Fatalf("Done=false after the chip was personalised (err=%v)", err)
	}
	// ARM2, the whole point: the ordinary hang-up now completes.
	if err != nil {
		t.Fatalf("an ORDINARY cancelled request failed the round: %v\n"+
			"Past Progress.Done the chip is already personalised. Honouring the cancellation "+
			"here does not abandon work cleanly — it abandons the DATABASE's only record of a "+
			"physical fact that is already true.", err)
	}

	var encodedAt *time.Time
	readAs(t, d, tenant, func(c context.Context, tx pgx.Tx) error {
		return tx.QueryRow(c, `SELECT encoded_at FROM tags WHERE uid = $1`, uid).Scan(&encodedAt)
	})
	if encodedAt == nil {
		t.Errorf("encoded_at is NULL after a completed round. It is the ONLY record that this " +
			"chip was personalised, and the row is now indistinguishable from a plaque nobody " +
			"has touched — whose recovery instruction is the opposite one")
	}

	// ...and no damage was filed, because none was done. This is the assertion that
	// separates a REPAIR from a RECORD: reverting the detach in markEncoded turns both
	// of these red at once.
	var unmarked int
	readAs(t, d, tenant, func(c context.Context, tx pgx.Tx) error {
		return tx.QueryRow(c,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = $3`,
			tenant, uid, ActionPlaqueUnmarked).Scan(&unmarked)
	})
	if unmarked != 0 {
		t.Errorf("a repaired round still filed %d %s row(s). That event means 'the marking "+
			"failed after the chip was already personalised'; a phone hanging up is not that, "+
			"and recording it as such trains the operator to ignore the one signal that "+
			"matters", unmarked, ActionPlaqueUnmarked)
	}
}
