package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
)

// ping_test.go — DB.Ping reads no table, measured rather than argued.
//
// 🔴 pool.go CARRIES A LONG ARGUMENT FOR THIS AND NOTHING HELD IT: an audit
// replaced the ping with `SELECT count(*) FROM transactions` and the entire suite
// stayed green. The argument is that a table read cannot answer the readiness
// question at all — tappa_app is NOBYPASSRLS and there is no tenant context outside
// WithTenant, so a healthy, fully populated database answers 0 rows
// (internal/handler's TestReadyz_ATableReadCouldNotAnswerThisQuestion measures that
// half: owner 200 473 rows, application role 0). This file measures the other half.

// TestPing_DoesNotDependOnTheSchema points a pool at the MAINTENANCE database —
// where none of this product's tables exist — and pings it.
//
// THAT IS THE DISCRIMINATOR: a ping succeeds there, and any statement naming a
// tappa table fails with 42P01. So a Ping that grew a table read turns this red for
// the right reason, and it is behaviour rather than a source scan.
func TestPing_DoesNotDependOnTheSchema(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the readiness ping's schema-independence test (real Postgres required)")
	}
	maintenance, ok := swapDatabase(dsn, "postgres")
	if !ok {
		t.Skipf("DATABASE_URL (%d chars) is not in a form this test can repoint at the maintenance database", len(dsn))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	data, err := New(ctx, &config.Config{DatabaseURL: maintenance})
	if err != nil {
		// db.New pings during construction, so a Ping that read a table fails HERE.
		if strings.Contains(err.Error(), "42P01") || strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("Ping failed against a database with none of this product's tables: %v\n"+
				"It is reading a table. A readiness check that reads a tenant-scoped table answers 0 rows on a "+
				"HEALTHY database (RLS, no tenant context outside WithTenant), so its success would prove nothing.", err)
		}
		t.Skipf("cannot reach the maintenance database as the application role (%v); this measurement needs it", err)
	}
	defer data.Close()

	if err := data.Ping(ctx); err != nil {
		t.Errorf("Ping against the maintenance database: %v — readiness must depend on the connection, not on the schema", err)
	}

	// POSITIVE CONTROL: the database really is one where a table read would fail.
	// Without this, the success above could mean the tables were present after all.
	err = data.pool.QueryRow(ctx, "SELECT count(*) FROM transactions").Scan(new(int))
	if err == nil {
		t.Fatal("CONTROL FAILED: `transactions` is readable in the maintenance database, so this test cannot tell a ping from a table read")
	}
	t.Logf("control: a table read here fails with %v", err)
}

// swapDatabase rewrites the database name in a postgres URL. It returns false for
// any shape it does not recognise rather than guessing.
func swapDatabase(dsn, name string) (string, bool) {
	slash := strings.LastIndex(dsn, "/")
	if slash < 0 || !strings.HasPrefix(dsn, "postgres") {
		return "", false
	}
	rest := dsn[slash+1:]
	query := ""
	if q := strings.Index(rest, "?"); q >= 0 {
		query = rest[q:]
	}
	return dsn[:slash+1] + name + query, true
}
