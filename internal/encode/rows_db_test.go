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
	"strings"
	"testing"

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

	if err := rows.InsertUnassigned(context.Background(), tenant, uid, wrappedFor(t, uid), actor); err != nil {
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
	if ev.actorID != nil {
		t.Errorf("actor_id = %v, want NULL: the encode actor is a caller-supplied STRING that "+
			"nothing authenticated, and audit_log.actor_id is joined to admin_users to render a "+
			"NAME on a manager's screen", ev.actorID)
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

	if err := rows.InsertUnassigned(context.Background(), tenant, uid, first, "operator-alpha"); err != nil {
		t.Fatalf("the first load failed: %v", err)
	}
	second := wrappedFor(t, uid)
	err := rows.InsertUnassigned(context.Background(), tenant, uid, second, "operator-beta")
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
	insertErr := rows.InsertUnassigned(ctx, tenant, uid, wrappedFor(t, uid), "operator-alpha")
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
	if err := good.InsertUnassigned(ctx, tenant, uid2, wrappedFor(t, uid2), "operator-alpha"); err != nil {
		t.Fatalf("the fixture load failed: %v", err)
	}
	markErr := rows.MarkEncoded(ctx, tenant, uid2, "operator-alpha")
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
	if err := good.InsertUnassigned(ctx, tenant, uid3, wrappedFor(t, uid3), "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
	if err := good.MarkEncoded(ctx, tenant, uid3, "operator-alpha"); err != nil {
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

	if err := rows.InsertUnassigned(ctx, tenant, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, tenant, uid, "operator-alpha"); err != nil {
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
	err := rows.InsertUnassigned(context.Background(), tenant, uid, short, "operator-alpha")
	if err == nil {
		t.Fatal("a 43-byte envelope was accepted; 00021's tags_aes_key_ref_is_kek_envelope " +
			"would have refused it in the database and printed the whole tuple doing so")
	}
	assertCarriesNoBytes(t, err, short)

	if err := rows.InsertUnassigned(context.Background(), tenant, uid, ref, "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
	dup := rows.InsertUnassigned(context.Background(), tenant, uid, ref, "operator-alpha")
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

	if err := rows.InsertUnassigned(ctx, tenant, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, tenant, uid, "operator-alpha"); err != nil {
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

	if err := rows.MarkEncoded(ctx, tenant, uid, "operator-alpha"); err != nil {
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

	if err := rows.InsertUnassigned(ctx, a, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, b, uid, "operator-beta"); err == nil {
		t.Fatal("tenant B marked tenant A's plaque as encoded")
	}
	// A's row is untouched and A can still mark it -- the positive control that
	// separates "the policy worked" from "the fixture was broken".
	if err := rows.MarkEncoded(ctx, a, uid, "operator-alpha"); err != nil {
		t.Fatalf("the owning tenant could not mark its own plaque: %v", err)
	}
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

	if err := rows.InsertUnassigned(ctx, a, uid, wrappedFor(t, uid), "operator-alpha"); err != nil {
		t.Fatalf("InsertUnassigned: %v", err)
	}
	if err := rows.MarkEncoded(ctx, a, uid, "operator-alpha"); err != nil {
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
		{"insert", func(tn uuid.UUID, u, a string) error { return rows.InsertUnassigned(ctx, tn, u, ref, a) }},
		{"mark", func(tn uuid.UUID, u, a string) error { return rows.MarkEncoded(ctx, tn, u, a) }},
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
	if err := rows.InsertUnassigned(ctx, tenant, uid, ref, "operator-alpha"); err != nil {
		t.Fatalf("the positive control failed: %v", err)
	}
	if err := rows.MarkEncoded(ctx, tenant, uid, "operator-alpha"); err != nil {
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
