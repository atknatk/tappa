package handler

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// health_db_test.go — the readiness endpoint against a REAL pgx pool.
//
// ⚠️ THESE SKIP WITHOUT DATABASE_URL, so a bare `go test` proves nothing here. Run
// them the way CI does: `make test`.

// TestReadyz_ARealDriverErrorNamesTheDeployment is the MEASUREMENT the response
// boundary rests on, and it needs no database: it points the real driver at a closed
// port and reads what it says.
//
// If this ever stops naming the user and the host, the argument in health.go about
// why the 503 body is a constant would be weaker — so the argument is measured here
// rather than asserted in a comment.
func TestReadyz_ARealDriverErrorNamesTheDeployment(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// Port 1 on the loopback: refused immediately, so this costs milliseconds. The
	// user and database names are invented for this test.
	_, err := db.New(ctx, &config.Config{
		DatabaseURL: "postgres://readyz_probe_user:pw@127.0.0.1:1/readyz_probe_db?sslmode=disable",
	})
	if err == nil {
		t.Fatal("connecting to a closed port succeeded")
	}
	t.Logf("the driver's own words: %v", err)
	for _, detail := range []string{"readyz_probe_user", "readyz_probe_db", "127.0.0.1:1"} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("the connection error does not mention %q; measured text: %v", detail, err)
		}
	}
	// The one thing it must never carry, whatever else it names.
	if strings.Contains(err.Error(), "pw") && strings.Contains(err.Error(), "password=") {
		t.Errorf("the driver error carries the password: %v", err)
	}

	// 🔴 AND THE SAME REAL ERROR, THROUGH THE REDUCTION THE RECORD ACTUALLY WRITES
	// (M8-03 round 4). Everything above measures what the DRIVER says; this measures
	// what an operator's log collector receives. It runs on the genuine article
	// rather than on a hand-built error, which is the difference that matters: a
	// future pgx release can change its wrapping, and a classifier tested only
	// against synthetic errors would keep passing while quietly falling through to
	// `other` — or, worse, start carrying the message.
	class, cause := readinessFailure(err)
	t.Logf("classified as: %s = %q, %s = %q", LogErrClassKey, class, LogErrCauseKey, cause)
	if class == errClassOther {
		t.Errorf("a refused connection classified as %q with cause %q — the record now tells the "+
			"operator nothing at all about the most ordinary readiness failure there is", class, cause)
	}
	for _, detail := range []string{"readyz_probe_user", "readyz_probe_db", "127.0.0.1", ":1"} {
		if strings.Contains(class+" "+cause, detail) {
			t.Errorf("the classified failure still carries %q (class=%q cause=%q). health.go refuses "+
				"to hand this to an unauthenticated HTTP caller; it must not hand it to a log "+
				"collector either.", detail, class, cause)
		}
	}
}

// TestReadyz_AgainstARealPool drives the endpoint against the development database
// and then takes the database away.
func TestReadyz_AgainstARealPool(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the readiness endpoint's real-pool test (real Postgres required)")
	}
	data, err := db.New(t.Context(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			data.Close()
		}
	})

	h := newHealth(t, data.Ping, nil)
	h.ttl = 0 // every request below must ask the database, not a cache
	srv := healthServer(t, h)

	get := func() (int, string) {
		t.Helper()
		res, err := srv.Client().Get(srv.URL + readyPath)
		if err != nil {
			t.Fatalf("GET %s: %v", readyPath, err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}

	if code, body := get(); code != http.StatusOK || body != readyBody {
		t.Fatalf("with a live database /readyz = %d %q, want 200 %q", code, body, readyBody)
	}

	// TAKE THE DATABASE AWAY. Closing the pool is the closest a test can get to the
	// deployment failure this endpoint exists for, and it exercises the real pgx
	// error path rather than a fake one.
	data.Close()
	closed = true

	code, body := get()
	if code != http.StatusServiceUnavailable {
		t.Errorf("with the pool closed /readyz = %d, want 503", code)
	}
	if body != notReadyBody {
		t.Errorf("body = %q, want %q", body, notReadyBody)
	}
	// The DSN's own parts must not appear in what a stranger receives.
	for _, part := range dsnFragments(dsn) {
		if strings.Contains(body, part) {
			t.Errorf("the 503 body carries %q from the connection string", part)
		}
	}
}

// dsnFragments pulls the identifying pieces out of a connection string so the leak
// check above uses THIS machine's real values rather than invented ones.
func dsnFragments(dsn string) []string {
	var out []string
	for _, sep := range []string{"://", "@", "/", ":"} {
		for _, part := range strings.Split(dsn, sep) {
			part = strings.TrimSpace(part)
			// Short fragments ("5432") produce noise; the point is the names.
			if len(part) >= 5 && !strings.Contains(part, "?") {
				out = append(out, part)
			}
		}
	}
	return out
}

// TestReadyz_ATableReadCouldNotAnswerThisQuestion is the §4.5 measurement behind
// internal/db.Ping's comment: it shows that the obvious alternative implementation
// of a readiness check — "SELECT from a table" — returns 0 rows on a healthy,
// populated database when it runs as tappa_app outside a tenant context.
//
// Without this, "Ping reads no table" would be a design preference in a comment.
// With it, a table-reading readiness check is measurably a check that cannot fail.
func TestReadyz_ATableReadCouldNotAnswerThisQuestion(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the RLS measurement behind /readyz (real Postgres required)")
	}
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the positive control needs the owner role to prove the table is not simply empty")
	}
	ctx := t.Context()

	appPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("app pool: %v", err)
	}
	defer appPool.Close()
	ownerPool, err := pgxpool.New(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("owner pool: %v", err)
	}
	defer ownerPool.Close()

	// POSITIVE CONTROL FIRST: the table really does hold rows. If it did not, the
	// zero below would mean nothing at all.
	var asOwner int
	if err := ownerPool.QueryRow(ctx, "SELECT count(*) FROM transactions").Scan(&asOwner); err != nil {
		t.Fatalf("counting as the owner: %v", err)
	}
	if asOwner == 0 {
		t.Skip("the development database holds no transactions (run `make seed`); without rows this measurement is vacuous")
	}

	var asApp int
	if err := appPool.QueryRow(ctx, "SELECT count(*) FROM transactions").Scan(&asApp); err != nil {
		t.Fatalf("counting as the application role: %v", err)
	}
	t.Logf("transactions visible to the owner: %d — to tappa_app with no tenant context: %d", asOwner, asApp)
	if asApp != 0 {
		t.Errorf("tappa_app saw %d rows with no tenant context; RLS should have hidden every one (CLAUDE.md §4.5)", asApp)
	}
}
