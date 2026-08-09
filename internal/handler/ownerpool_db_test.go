package handler

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ownerPoolForTest opens a pool that connects as tappa_owner via the
// migration-role URL, for the ONE thing an application-role pool must not be able
// to do: simulate a database-side corruption of a row the application may not
// write.
//
// WHY IT EXISTS AT ALL -- migration 00013 / backlog T9. Before 00013, tappa_app
// held table-wide UPDATE on `tags`, so a test could rewrite aes_key_ref through
// the ordinary harness pool. That was never a test property; it was the privilege
// T9 measured as "overwrite a plaque's wrapped key -> every tap answers 500,
// silently". 00013 revokes it. A test that needs a CORRUPT row therefore has to
// ask the role that owns the schema, exactly as internal/db/rls_test.go's ownerDB
// does for the append-only trigger cases.
//
// Using the migration role is legitimate here and only here: redline R5 forbids it
// in PRODUCTION code (it bypasses RLS entirely -- tappa_owner is a superuser) and
// excludes _test.go, so the env key is a plain literal.
//
// It asserts the role at RUNTIME rather than trusting the URL: a .env that pointed
// this at tappa_app would turn a proof into a tautology (the statement would fail
// for the RIGHT reason and the test would report the WRONG one).
//
// 🔴 IT DOES NOT INTRODUCE A SILENT SKIP, AND THAT TOOK A DELIBERATE CHOICE. The
// caller (TestQRDB_ABrokenKeyRefBlocksNFCButNotQR) used to run off the harness pool
// and always executed; routing it through the owner role could have made it skip
// halfway through on any machine that sets DATABASE_URL but not
// DATABASE_MIGRATE_URL. A test that used to run and now quietly does not is a
// finding this project has already made twice. So the two situations are separated:
//
//	NO database configured at all ....... SKIP, like every other DB test here.
//	DATABASE_URL set, migrate URL not ... FATAL. That is a half-configured
//	  environment, not an environment without Postgres, and the thing it would
//	  silence is a section 4.7 assertion -- that tappa_app may NOT rewrite a
//	  plaque's wrapped key.
func ownerPoolForTest(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("DATABASE_MIGRATE_URL")
	if raw == "" {
		if os.Getenv("DATABASE_URL") == "" {
			t.Skip("no database configured; skipping the owner-role fixture (real Postgres required -- CLAUDE.md section 8, Q04)")
		}
		t.Fatal("DATABASE_URL is set but DATABASE_MIGRATE_URL is not. This fixture needs the " +
			"migration role to simulate a corrupt row, because migration 00013 revoked that " +
			"privilege from tappa_app (backlog T9). Skipping here would silently drop a " +
			"section 4.7 assertion on a machine that HAS a database, so it fails instead.")
	}
	pool, err := pgxpool.New(context.Background(), raw)
	if err != nil {
		t.Fatalf("owner pool: %v", err)
	}
	t.Cleanup(pool.Close)

	var user string
	var super bool
	if err := pool.QueryRow(context.Background(),
		`SELECT current_user, rolsuper FROM pg_roles WHERE rolname = current_user`,
	).Scan(&user, &super); err != nil {
		t.Fatalf("owner pool: read role: %v", err)
	}
	if user != "tappa_owner" || !super {
		t.Fatalf("owner pool current_user=%q rolsuper=%v, want tappa_owner/true "+
			"(this fixture exists precisely because tappa_app may NOT write the column it writes)", user, super)
	}
	return pool
}
