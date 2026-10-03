package legal

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/test/fixtures"
)

// These run against a REAL Postgres (CLAUDE.md §8): the properties below are the
// TABLE's, not the package's — append-only is a privilege plus a trigger, and "no
// tenant scope" is only interesting if a real RLS-bound role proves it.
//
// 🔴 THE ONE PROPERTY WORTH THE MOST HERE: legal_documents is the FIRST table in this
// product with no tenant_id, so the question "does that hole let one tenant see
// another's rows" has to be answered against the live catalog rather than argued.
// The answer is that there is nothing tenant-shaped to see — the rows belong to
// Tappa — and TestLegalDB_TheTableIsVisibleToEveryTenantAndScopedToNone measures
// exactly that, in both directions, so a later reader cannot mistake it for an
// isolation failure.
//
// ⚠️ SINCE MIGRATION 00027 (M10 OP-10) THE APPLICATION ROLE CANNOT WRITE THIS TABLE:
// tappa_app lost INSERT, and the one writer is the operator's op_publish_legal (its
// tests are internal/db's operatorlegal_test.go; the screen that calls it,
// internal/handler/operator's). So the versions these tests need are appended through
// the OWNER's connection (ownerPublish) -- a stand-in for that writer, because every
// property below belongs to the TABLE (who can see it, who can change it, what the boot
// read finds), not to the writer. The M7-06 panel's Store.Publish was removed in OP-10's
// phase B; the application role's refused INSERT is measured below
// (TestLegalDB_TheTableTakesNoUpdateAndNoDelete) and in internal/db
// (TestOperator00027_TheApplicationCanNoLongerWriteALegalText).

type dbFixture struct {
	data  *db.DB
	store *Store
	// two independent tenants, each with an admin, so a cross-tenant claim can be
	// made about something real.
	tenantA, adminA uuid.UUID
	tenantB, adminB uuid.UUID
}

func newDBFixture(t *testing.T) *dbFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping legal DB tests (real Postgres required — CLAUDE.md §8)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	s, err := NewStore(data)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	f := &dbFixture{
		data: data, store: s,
		tenantA: uuid.New(), adminA: uuid.New(),
		tenantB: uuid.New(), adminB: uuid.New(),
	}
	f.seedTenant(t, f.tenantA, f.adminA)
	f.seedTenant(t, f.tenantB, f.adminB)
	return f
}

func (f *dbFixture) seedTenant(t *testing.T, tenantID, adminID uuid.UUID) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			 VALUES ($1, 'Legal Test Ltd', $2, 'restaurant', 'single', 'Europe/Malta')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return fmt.Errorf("tenant: %w", e)
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
			 VALUES ($1, $2, 'Operator', $3, $4, 'owner')`,
			adminID, tenantID, "op-"+uuid.NewString()+"@legal.example", fixtures.UnusablePasswordHash)
		return e
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// ownerPublish appends one version through the OWNER's connection and commits it -- the
// stand-in for op_publish_legal (see the file header). by is written as published_by,
// the value the M7-06 panel wrote (a customer admin's id).
func ownerPublish(t *testing.T, slug, body string, by uuid.UUID) {
	t.Helper()
	if got := ownerScalar(t, `WITH v AS (INSERT INTO legal_documents (slug, body, published_by)
	                                     VALUES ($1, $2, $3) RETURNING id)
	                         SELECT count(*)::text FROM v`, slug, body, by); got != "1" {
		t.Fatalf("the owner appended %s versions, want 1", got)
	}
}

// TestLegalDB_TheApplicationCannotReadWhoPublished — the column grant, measured.
//
// 🔴 THE TABLE HAS NO TENANT SCOPE AND ITS POLICY IS `USING (true)`, so every
// tenant's connection can see every row. A security audit measured what that meant
// for published_by: a foreign tenant's context returned 37 rows carrying admin uuids
// while the same context saw 0 rows of admin_users — a weak cross-tenant existence
// oracle, and exactly the thing ADR 0016 refuses an FK for. 00020 now grants
// tappa_app INSERT on that column and NO SELECT, so the application writes the
// provenance and can never read it; a forensic read is the owner's.
func TestLegalDB_TheApplicationCannotReadWhoPublished(t *testing.T) {
	f := newDBFixture(t)
	ctx := context.Background()
	ownerPublish(t, "privacy", "FAKE text for the grant probe.", f.adminA)
	err := f.data.WithTenant(ctx, f.tenantB, func(ctx context.Context, tx pgx.Tx) error {
		var n int
		return tx.QueryRow(ctx, `SELECT count(published_by) FROM legal_documents`).Scan(&n)
	})
	if err == nil {
		t.Error("tappa_app can read legal_documents.published_by. The table is visible to " +
			"every tenant by design, so a readable admin uuid is a fact about another " +
			"business that any customer's connection could fetch.")
	} else if !strings.Contains(err.Error(), "42501") && !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the read failed with %v, want a privilege error (42501)", err)
	}
	// POSITIVE CONTROL: the columns the product DOES read are readable, or the
	// refusal above could be a table the application cannot see at all.
	err = f.data.WithTenant(ctx, f.tenantB, func(ctx context.Context, tx pgx.Tx) error {
		var n int
		return tx.QueryRow(ctx, `SELECT count(body) FROM legal_documents`).Scan(&n)
	})
	if err != nil {
		t.Fatalf("tappa_app cannot read legal_documents.body either (%v); the refusal above "+
			"proves nothing about the column grant", err)
	}
}

// ownerScalar runs one scalar query through the migrate (superuser) connection.
//
// 🔴 IT EXISTS BECAUSE 00020 TOOK SELECT ON published_by AWAY FROM tappa_app. A test
// that reads a column the application deliberately cannot read is doing an OWNER's
// job, so it connects as one — the same split billing_db_test.go's ownerExec makes,
// and for the same reason: if this could be read as the application role, the test
// above would be asserting something that is not true.
func ownerScalar(t *testing.T, sql string, args ...any) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; skipping (the owner connection is required to " +
			"read a column the application may not)")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	defer conn.Close(ctx)
	var out string
	if err := conn.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
		t.Fatalf("owner query: %v", err)
	}
	return out
}

// TestLegalDB_TheTableTakesNoUpdateAndNoDelete — §4.3, both belts.
//
// A published legal text that can be edited in place has no history, and "what did
// the policy say on the day of the complaint" is the only question anybody will ever
// ask of it. A correction is a NEW ROW -- appended, since 00027, by the operator's
// op_publish_legal and not by this role, whose INSERT is refused as well.
func TestLegalDB_TheTableTakesNoUpdateAndNoDelete(t *testing.T) {
	f := newDBFixture(t)
	ctx := context.Background()
	first := "FAKE first version. " + uuid.NewString()
	ownerPublish(t, "terms", first, f.adminA)

	probes := []struct{ name, sql string }{
		{"UPDATE the body", `UPDATE legal_documents SET body = 'rewritten' WHERE slug = 'terms'`},
		{"UPDATE the date", `UPDATE legal_documents SET published_at = now() WHERE slug = 'terms'`},
		{"DELETE the row", `DELETE FROM legal_documents WHERE slug = 'terms'`},
		{"INSERT a version (00027)", `INSERT INTO legal_documents (slug, body) VALUES ('terms', 'FAKE text')`},
	}
	for _, p := range probes {
		err := f.data.WithTenant(ctx, f.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, p.sql)
			return e
		})
		if err == nil {
			t.Errorf("%s succeeded as tappa_app. 00020 revokes UPDATE and DELETE precisely so "+
				"that a correction has to be a new row, and 00027 revokes INSERT so that the "+
				"operator's op_publish_legal is the one writer.", p.name)
			continue
		}
		if !strings.Contains(err.Error(), "42501") && !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s failed with %v, want a privilege error (42501)", p.name, err)
		}
	}

	// POSITIVE CONTROL: the role still READS the table, or the refusals above could be a
	// table it cannot reach at all.
	var n int
	if err := f.data.WithTenant(ctx, f.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM legal_documents WHERE body = $1`, first).Scan(&n)
	}); err != nil || n != 1 {
		t.Fatalf("tappa_app reads %d copies of the first version (%v), want 1", n, err)
	}
	// A correction is a new row, and THE LATEST WINS on the next read of the snapshot.
	second := "FAKE second version. " + uuid.NewString()
	ownerPublish(t, "terms", second, f.adminA)
	if err := f.store.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := f.store.Published()["terms"].Body; got != second {
		t.Errorf("the snapshot serves %q after a correction; the newest version must win", got)
	}
}

// TestLegalDB_TheTableIsVisibleToEveryTenantAndScopedToNone.
//
// 🔴 THIS IS THE TEST THAT ANSWERS THE §4.5 QUESTION, AND ITS EXPECTED RESULT IS THE
// OPPOSITE OF EVERY OTHER RLS TEST IN THIS REPOSITORY. Everywhere else, A's
// connection must NOT see B's row. Here there is no A's row and no B's row: the
// document belongs to Tappa, every reader gets the same text, and a page with no
// identity at all serves it. So both tenants must see the SAME single version —
// and that is not an isolation failure, it is the absence of anything to isolate.
//
// The probe carries NO tenant filter, deliberately (CLAUDE.md §6: an isolation probe
// that filters proves nothing about RLS).
func TestLegalDB_TheTableIsVisibleToEveryTenantAndScopedToNone(t *testing.T) {
	f := newDBFixture(t)
	ctx := context.Background()
	body := "FAKE imprint written by " + f.tenantA.String()
	ownerPublish(t, "imprint", body, f.adminA)

	read := func(tenantID uuid.UUID) string {
		t.Helper()
		var got string
		err := f.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT body FROM legal_documents WHERE slug = 'imprint'
				 ORDER BY published_at DESC, id DESC LIMIT 1`).Scan(&got)
		})
		if err != nil {
			t.Fatalf("read under %s: %v", tenantID, err)
		}
		return got
	}
	if got := read(f.tenantA); got != body {
		t.Errorf("the publishing tenant reads %q", got)
	}
	if got := read(f.tenantB); got != body {
		t.Errorf("a DIFFERENT tenant reads %q, want the same published text. These documents "+
			"belong to Tappa and every reader gets the same one; a per-tenant answer here "+
			"would mean the privacy policy said different things to different customers.", got)
	}

	// AND THE COMPARISON THAT MAKES IT MEAN SOMETHING: a table that IS tenant-scoped
	// must behave the other way under exactly the same two contexts. Without this the
	// assertions above would pass just as happily on a database with RLS switched off.
	var leaked int
	err := f.data.WithTenant(ctx, f.tenantB, func(ctx context.Context, tx pgx.Tx) error {
		// No tenant filter — RLS is what must return 0.
		return tx.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE id = $1`, f.adminA).Scan(&leaked)
	})
	if err != nil {
		t.Fatalf("control probe: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("tenant B's connection can see tenant A's admin row (%d). RLS is not in force "+
			"in this database, so nothing this test says about legal_documents means anything.",
			leaked)
	}
}

// TestLegalDB_RefreshNeedsNoTenantAndSeesNothingElse.
//
// 🔴 THE BOOT READ RUNS UNDER A UUID THAT MATCHES NO TENANT, and that is containment
// rather than a placeholder: inside that context every RLS-scoped table is empty, so
// the only thing reachable is the table whose policy is `USING (true)`. Both halves
// are measured — the legal text IS readable, and a tenant-scoped table is NOT.
func TestLegalDB_RefreshNeedsNoTenantAndSeesNothingElse(t *testing.T) {
	f := newDBFixture(t)
	ctx := context.Background()
	body := "FAKE cookie notice framing. " + uuid.NewString()
	ownerPublish(t, "cookies", body, f.adminA)

	// A FRESH store, so the snapshot can only come from the read.
	fresh, err := NewStore(f.data)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if n := len(fresh.Published()); n != 0 {
		t.Fatalf("a fresh store already reports %d documents; the read below would prove nothing", n)
	}
	if err := fresh.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	reloaded := fresh.Published()["cookies"]
	if reloaded.Body != body {
		t.Errorf("Refresh read %q, want the published text", reloaded.Body)
	}
	// The BOOT path must precompute too, or a restart would silently move the
	// splitting cost back onto every anonymous request.
	if len(reloaded.Paragraphs) == 0 {
		t.Error("a document loaded at start-up carries no paragraphs")
	}

	// THE CONTAINMENT HALF: under the same context, a tenant-scoped table is empty.
	var n int
	err = f.data.WithTenant(ctx, readContext, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&n)
	})
	if err != nil {
		t.Fatalf("containment probe: %v", err)
	}
	if n != 0 {
		t.Errorf("the boot context can see %d admin_users rows. It names a uuid that matches no "+
			"tenant precisely so that a read at start-up cannot reach a customer's data; if "+
			"this is not zero, that containment is not real.", n)
	}
}

// TestLegalDB_AnEmptyBodyIsRefusedByTheColumnAndNotOnlyByGo.
//
// The Go check exists so an operator gets a sentence rather than a 23514; the COLUMN
// check exists so the sentence cannot be the only thing standing between a blank
// document and a public page. Both are measured, because a guard that only lives in
// the language it was written in is a guard the next caller skips.
//
// THE GO HALF MOVED WITH THE WRITER (M10 OP-10, phase B). It was Store.Publish's
// ErrEmptyBody; the one writer is now the operator's screen, and its refusal of a body
// with no visible text -- before any store call -- is internal/handler/operator's
// TestLegalPublish_RefusesAnEmptyBodyAndSaysSo. This test keeps the COLUMN half.
//
// Since 00027 tappa_app cannot INSERT at all, so the column is driven from the OWNER's
// connection -- the CHECK binds every writer, op_publish_legal's definer included --
// inside a transaction that is rolled back (no row stays). That the application's own
// INSERT is refused before any CHECK is asserted too.
func TestLegalDB_AnEmptyBodyIsRefusedByTheColumnAndNotOnlyByGo(t *testing.T) {
	f := newDBFixture(t)
	ctx := context.Background()

	err := f.data.WithTenant(ctx, f.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO legal_documents (slug, body) VALUES ('privacy', '   ')`)
		return e
	})
	if err == nil || !strings.Contains(err.Error(), "42501") {
		t.Errorf("tappa_app's INSERT answered %v, want the privilege refusal (42501) since 00027", err)
	}

	// STRAIGHT AT THE COLUMN, bypassing every guard but the schema's.
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the column is driven from the owner's connection")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	try := func(sql string) error {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, e := sp.Exec(ctx, sql)
		if e != nil {
			if err := sp.Rollback(ctx); err != nil {
				t.Fatalf("rollback to savepoint: %v", err)
			}
			return e
		}
		if err := sp.Commit(ctx); err != nil {
			t.Fatalf("release savepoint: %v", err)
		}
		return nil
	}
	if err := try(`INSERT INTO legal_documents (slug, body) VALUES ('privacy', '   ')`); err == nil {
		t.Error("the column accepted a whitespace-only body. A document with nothing in it " +
			"renders as a page that is neither a placeholder nor a text.")
	} else if !strings.Contains(err.Error(), "23514") {
		t.Errorf("the column refused with %v, want a check violation (23514)", err)
	}
	// AND A FIFTH SLUG.
	if err := try(`INSERT INTO legal_documents (slug, body) VALUES ('refunds', 'text')`); err == nil {
		t.Error("the column accepted a slug with no page. Its text would be stored at no URL.")
	}
	// POSITIVE CONTROL: a real slug with real text goes in, so the refusals above are
	// not a table that refuses everything.
	if err := try(`INSERT INTO legal_documents (slug, body) VALUES ('terms', 'FAKE control text')`); err != nil {
		t.Fatalf("a valid insert was refused (%v); every refusal above is vacuous", err)
	}
}
