package legal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
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
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	s, err := NewStore(data, trail)
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

// TestLegalDB_PublishWritesTheTextTheTrailAndTheSnapshotOrNoneOfThem.
//
// 🔴 §4.3 — THE AUDIT ROW SHARES THE TRANSACTION. Publish uses RecordTx rather than
// Record, so a published text with no trail entry and a trail entry for a
// publication that rolled back are both unreachable. Both facts are counted.
func TestLegalDB_PublishWritesTheTextTheTrailAndTheSnapshotOrNoneOfThem(t *testing.T) {
	f := newDBFixture(t)
	ctx := context.Background()

	const body = "This is a FAKE placeholder text written by a test. It is not a policy."
	doc, err := f.store.Publish(ctx, f.tenantA, f.adminA, "privacy", body)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if doc.Body != body {
		t.Errorf("the stored body is %q, want the text as typed", doc.Body)
	}
	// 🔴 published_by IS WRITTEN AND CANNOT BE READ BACK BY THIS ROLE, which is a
	// grant rather than an omission: the table has no tenant scope, so a readable
	// admin uuid would be a fact about somebody else's business that any tenant's
	// connection could fetch (measured by a security audit). So the value is checked
	// through the OWNER connection, and the application's inability to read it is
	// asserted as its own property below.
	if got := ownerScalar(t, `SELECT published_by::text FROM legal_documents WHERE id = $1`, docIDOf(t, f, "privacy")); got != f.adminA.String() {
		t.Errorf("published_by = %s, want the publisher %s", got, f.adminA)
	}

	// The SNAPSHOT was replaced in the same call, so the public pages can serve it
	// with no second round trip.
	got, ok := f.store.Published()["privacy"]
	if !ok || got.Body != body {
		t.Errorf("the snapshot does not carry the text that was just published: %+v", got)
	}
	// 🔴 AND IT CARRIES THE PARAGRAPHS, ALREADY SPLIT. This is the assertion that makes
	// the precompute real rather than decorative: /legal/* is unmetered, so the split
	// happens once here instead of on every anonymous GET (a security audit measured
	// 253 ms per request for a pathological body). It is asserted on the STORE's own
	// output, because the handler package's double computes them itself — a mutation
	// that deleted the precompute from Store.set survived every handler test and was
	// caught only here.
	if len(got.Paragraphs) == 0 {
		t.Error("the snapshot carries a body and no paragraphs, so the public page would " +
			"either render nothing or have to split the text on every anonymous request")
	}
	if want := Paragraphs(body); len(got.Paragraphs) != len(want) {
		t.Errorf("the snapshot carries %d paragraphs, want %d", len(got.Paragraphs), len(want))
	}

	// The TRAIL row is there, under the PUBLISHER'S OWN tenant.
	var action, target string
	var actor *uuid.UUID
	err = f.data.WithTenant(ctx, f.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT action, target, actor_id FROM audit_log
			 WHERE tenant_id = $1 AND action = $2 ORDER BY at DESC LIMIT 1`,
			f.tenantA, ActionPublished).Scan(&action, &target, &actor)
	})
	if err != nil {
		t.Fatalf("reading the trail: %v", err)
	}
	if target != "privacy" {
		t.Errorf("the trail names target %q, want the document", target)
	}
	if actor == nil || *actor != f.adminA {
		t.Errorf("the trail names actor %v, want the publisher %s", actor, f.adminA)
	}

	// AND THE OTHER TENANT'S TRAIL IS UNTOUCHED. Publishing is not something that
	// happens to a customer.
	var n int
	err = f.data.WithTenant(ctx, f.tenantB, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			f.tenantB, ActionPublished).Scan(&n)
	})
	if err != nil {
		t.Fatalf("counting B's trail: %v", err)
	}
	if n != 0 {
		t.Errorf("tenant B's trail carries %d legal.published rows; a publication belongs to "+
			"the tenant of the admin who made it and to nobody else", n)
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
	if _, err := f.store.Publish(ctx, f.tenantA, f.adminA, "privacy", "FAKE text for the grant probe."); err != nil {
		t.Fatalf("Publish: %v", err)
	}
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

// docIDOf returns the newest row id for a slug, through the owner connection.
func docIDOf(t *testing.T, f *dbFixture, slug string) string {
	t.Helper()
	return ownerScalar(t, `SELECT id::text FROM legal_documents WHERE slug = $1
	                       ORDER BY published_at DESC, id DESC LIMIT 1`, slug)
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
// ask of it. A correction is a NEW ROW.
func TestLegalDB_TheTableTakesNoUpdateAndNoDelete(t *testing.T) {
	f := newDBFixture(t)
	ctx := context.Background()
	if _, err := f.store.Publish(ctx, f.tenantA, f.adminA, "terms", "FAKE first version."); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	probes := []struct{ name, sql string }{
		{"UPDATE the body", `UPDATE legal_documents SET body = 'rewritten' WHERE slug = 'terms'`},
		{"UPDATE the date", `UPDATE legal_documents SET published_at = now() WHERE slug = 'terms'`},
		{"DELETE the row", `DELETE FROM legal_documents WHERE slug = 'terms'`},
	}
	for _, p := range probes {
		err := f.data.WithTenant(ctx, f.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, p.sql)
			return e
		})
		if err == nil {
			t.Errorf("%s succeeded as tappa_app. 00020 revokes UPDATE and DELETE precisely so "+
				"that a correction has to be a new row.", p.name)
			continue
		}
		if !strings.Contains(err.Error(), "42501") && !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s failed with %v, want a privilege error (42501)", p.name, err)
		}
	}

	// POSITIVE CONTROL: an INSERT still works, or the refusals above could be a table
	// nothing can touch at all.
	if _, err := f.store.Publish(ctx, f.tenantA, f.adminA, "terms", "FAKE second version."); err != nil {
		t.Fatalf("a second version could not be appended: %v — a correction is a new row and "+
			"that path must stay open", err)
	}
	// AND THE LATEST WINS.
	if got := f.store.Published()["terms"].Body; got != "FAKE second version." {
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
	if _, err := f.store.Publish(ctx, f.tenantA, f.adminA, "imprint", body); err != nil {
		t.Fatalf("Publish: %v", err)
	}

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
	body := "FAKE cookie notice framing."
	if _, err := f.store.Publish(ctx, f.tenantA, f.adminA, "cookies", body); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// A FRESH store, so the snapshot can only come from the read.
	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	fresh, err := NewStore(f.data, trail)
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
func TestLegalDB_AnEmptyBodyIsRefusedByTheColumnAndNotOnlyByGo(t *testing.T) {
	f := newDBFixture(t)
	ctx := context.Background()

	if _, err := f.store.Publish(ctx, f.tenantA, f.adminA, "privacy", "   "); !errors.Is(err, ErrEmptyBody) {
		t.Errorf("Publish with a whitespace body returned %v, want ErrEmptyBody", err)
	}
	// STRAIGHT AT THE COLUMN, bypassing the Go guard entirely.
	err := f.data.WithTenant(ctx, f.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO legal_documents (slug, body) VALUES ('privacy', '   ')`)
		return e
	})
	if err == nil {
		t.Error("the column accepted a whitespace-only body. A document with nothing in it " +
			"renders as a page that is neither a placeholder nor a text.")
	} else if !strings.Contains(err.Error(), "23514") {
		t.Errorf("the column refused with %v, want a check violation (23514)", err)
	}
	// AND A FIFTH SLUG.
	err = f.data.WithTenant(ctx, f.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO legal_documents (slug, body) VALUES ('refunds', 'text')`)
		return e
	})
	if err == nil {
		t.Error("the column accepted a slug with no page. Its text would be stored at no URL.")
	}
	// POSITIVE CONTROL: a real slug with real text goes in, so the refusals above are
	// not a table that refuses everything.
	err = f.data.WithTenant(ctx, f.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO legal_documents (slug, body) VALUES ('terms', 'FAKE control text')`)
		return e
	})
	if err != nil {
		t.Fatalf("a valid insert was refused (%v); every refusal above is vacuous", err)
	}
}
