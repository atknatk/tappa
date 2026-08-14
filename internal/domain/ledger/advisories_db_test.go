package ledger

// advisories_db_test.go -- M7-03 phase B round 4: what happens when one of the
// landing section's two ADVISORY reads fails.
//
// 🔴 THIS TEST DRIVES Screen, WITH THE COMMIT, AND ROUND 3's DID NOT. That is the
// whole finding. Round 3 called advisories() directly and returned a sentinel from
// the callback, so db.WithTenant ROLLED BACK and the commit path never ran -- and
// the commit path is where the defect lived: Postgres aborts the transaction on any
// statement error, so tx.Commit() answered pgx.ErrTxCommitRollback, WithTenant
// returned non-nil, Screen returned an error and the handler rendered the 500 the
// degradation was supposed to prevent. Swallowing the Go error achieved nothing.
//
// ⚠️ AND THE SHAPE OF THAT MISTAKE IS THE ONE ROUND 3 ITSELF REPORTED: "the path
// that was measured is not the path production runs". It was reported about the
// Queried flags and then repeated in the fix for them.
//
// HOW THE FAILURE IS INJECTED, AND WHY THIS WAY. A TEMP TABLE named `tags` shadows
// the real one for THIS SESSION ONLY (pg_temp precedes public in search_path), and
// it has none of the columns the query names, so CountTenantPlaques fails with
// 42703 -- a genuine statement error of exactly the class the code names (a
// permission slip, a timeout, a bug in the count). A table LOCK would have produced
// the same error class but would have blocked every other session touching `tags`,
// including the other packages `go test ./...` runs in parallel.
//
// THE SPLIT IS THE POINT. Only `tags` is shadowed, so the plaque count fails while
// the history probe (which reads `transactions`) succeeds -- which is why the two
// advisory reads get SEPARATE savepoints rather than one around both.

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// shadowTagsDB is the production Database with one statement inserted at the top of
// every transaction. It calls the REAL db.WithTenant -- real pool.Begin, real
// `SET LOCAL app.tenant_id`, real Commit -- because the commit is the thing under
// test and a hand-rolled imitation of it would prove nothing about the wiring.
type shadowTagsDB struct{ real *db.DB }

func (d shadowTagsDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`CREATE TEMP TABLE tags (unused int) ON COMMIT DROP`); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

// TestAdvisoriesDB_AFailedAdviceCostsTheSentenceAndNeverTheRecords drives the whole
// section the way the handler does.
func TestAdvisoriesDB_AFailedAdviceCostsTheSentenceAndNeverTheRecords(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required). " +
			"Run `make test` -- a bare `go test` skips this silently and proves nothing.")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	tenantID := seedOneRecordedTenant(t, ctx, data)
	day := Date{Year: time.Now().UTC().Year(), Month: time.Now().UTC().Month(), Day: time.Now().UTC().Day()}

	// --- the degradation, through Screen, through Commit ------------------------
	var logged logCounter
	broken, err := NewReader(shadowTagsDB{real: data}, slog.New(&logged))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	screen, err := broken.Screen(ctx, tenantID, Filter{Date: day})
	if err != nil {
		t.Fatalf("Screen returned %v.\n"+
			"A failed ADVISORY read took the whole evidence page away: the day's records "+
			"had already been read one statement earlier, and the manager gets a 500.\n"+
			"This is what the savepoints in Reader.advisories exist to stop -- without "+
			"them Postgres aborts the transaction and Commit answers "+
			"pgx.ErrTxCommitRollback.", err)
	}

	// THE RECORDS SURVIVED. This is the property; everything else is bookkeeping.
	if !screen.Queried || len(screen.Records) == 0 {
		t.Fatalf("the page came back queried=%v with %d record(s); the evidence is gone "+
			"even though Screen returned no error", screen.Queried, len(screen.Records))
	}
	// The advice that failed is unmeasured, so the screen says nothing about plaques.
	if screen.Plaques.Queried {
		t.Error("the plaque reading came back Queried after its statement failed; the " +
			"screen would then make a claim about a business nobody measured")
	}
	// THE SPLIT: the other advisory read is on a different table and must be
	// unaffected. One savepoint around both would lose this.
	if !screen.History.Queried {
		t.Error("the history reading was lost too, though nothing was wrong with it -- " +
			"the two advisory reads are supposed to fail independently")
	}
	if n := logged.errors(); n != 1 {
		t.Errorf("%d error(s) logged, want exactly 1 (§7 forbids swallowing; dropping a "+
			"sentence silently is how a broken read becomes invisible)", n)
	}

	// --- the positive control ---------------------------------------------------
	//
	// WITHOUT IT EVERY ASSERTION ABOVE PASSES AGAINST A READER THAT MEASURES NOTHING.
	// The same tenant, the same day, the real Database: both readings must come back
	// measured and no error may be logged.
	var quiet logCounter
	healthy, err := NewReader(data, slog.New(&quiet))
	if err != nil {
		t.Fatalf("NewReader (control): %v", err)
	}
	ctrl, err := healthy.Screen(ctx, tenantID, Filter{Date: day})
	if err != nil {
		t.Fatalf("control Screen: %v", err)
	}
	if !ctrl.Plaques.Queried || !ctrl.History.Queried {
		t.Fatalf("control: plaques.Queried=%v history.Queried=%v, want both true -- the "+
			"checks above are passing because advisories never measures anything",
			ctrl.Plaques.Queried, ctrl.History.Queried)
	}
	if len(ctrl.Records) == 0 {
		t.Fatal("control: no records; the fixture is not what this test thinks it is")
	}
	if n := quiet.errors(); n != 0 {
		t.Errorf("control logged %d error(s) on a healthy read, want 0", n)
	}
}

// seedOneRecordedTenant creates a tenant with one venue and one recorded tap, so the
// page under test has something to lose.
func seedOneRecordedTenant(t *testing.T, ctx context.Context, data *db.DB) uuid.UUID {
	t.Helper()
	tenantID, locationID := uuid.New(), uuid.New()
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Advisory Probe Ltd', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Probe Door')`,
			locationID, tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions (tenant_id, location_id, occurred_at, verdict, channel)
			 VALUES ($1, $2, now(), 'reject', 'nfc')`, tenantID, locationID)
		return e
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	return tenantID
}

// TestAdvisories_TheSavepointIsWhatMakesTheDegradationReal is the unit-level half:
// it names the mechanism, so a reader who deletes the savepoints learns why they
// were there from a failing test rather than from a comment.
func TestAdvisories_TheSavepointIsWhatMakesTheDegradationReal(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required). " +
			"Run `make test` -- a bare `go test` skips this silently and proves nothing.")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	stop := errors.New("rollback: this test writes nothing")

	// HALF ONE -- WITHOUT A SAVEPOINT, ONE FAILED STATEMENT POISONS EVERYTHING AFTER
	// IT, INCLUDING THE COMMIT. This is the fact the round-3 fix did not account for.
	if err := data.WithTenant(ctx, uuid.New(), func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "SELECT 1/0"); e == nil {
			t.Fatal("SELECT 1/0 succeeded; this test cannot create the state it measures")
		}
		if _, e := tx.Exec(ctx, "SELECT 1"); e == nil {
			t.Error("a plain SELECT succeeded after a failed statement; this Postgres " +
				"does not abort transactions and the savepoints would be pointless")
		}
		// 🔴 AND A SAVEPOINT CANNOT BE OPENED ONCE THE TRANSACTION IS ALREADY ABORTED
		// -- SAVEPOINT is itself a command and is refused with 25P02. That is why
		// inSavepoint takes the savepoint BEFORE the read rather than reaching for one
		// after a failure, and it is worth an assertion: a "repair it afterwards"
		// version of this code would look reasonable and would never work.
		if _, e := tx.Begin(ctx); e == nil {
			t.Error("a SAVEPOINT was accepted inside an aborted transaction; the ordering " +
				"argument in inSavepoint would then be unnecessary")
		}
		return stop
	}); err != nil && !errors.Is(err, stop) {
		t.Fatalf("WithTenant (poisoned half): %v", err)
	}

	// HALF TWO -- A SAVEPOINT TAKEN BEFOREHAND CLEARS IT, and the transaction is
	// usable again afterwards. This is exactly inSavepoint's shape.
	if err := data.WithTenant(ctx, uuid.New(), func(ctx context.Context, tx pgx.Tx) error {
		sp, e := tx.Begin(ctx) // SAVEPOINT, before anything can fail
		if e != nil {
			t.Fatalf("opening a savepoint on a healthy transaction: %v", e)
		}
		if _, e := sp.Exec(ctx, "SELECT 1/0"); e == nil {
			t.Fatal("SELECT 1/0 succeeded inside the savepoint")
		}
		_ = sp.Rollback(context.Background()) // ROLLBACK TO SAVEPOINT
		if _, e := tx.Exec(ctx, "SELECT 1"); e != nil {
			t.Errorf("still poisoned after ROLLBACK TO SAVEPOINT: %v -- the whole "+
				"degradation rests on this clearing the aborted state", e)
		}
		return stop
	}); err != nil && !errors.Is(err, stop) {
		t.Fatalf("WithTenant (savepoint half): %v", err)
	}
}

// logCounter is a slog.Handler that counts ERROR records. It replaces the reader's
// logger so the "nothing is swallowed" half of the property is measured rather than
// assumed.
type logCounter struct{ n int }

func (c *logCounter) Enabled(context.Context, slog.Level) bool { return true }
func (c *logCounter) Handle(_ context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelError {
		c.n++
	}
	return nil
}
func (c *logCounter) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *logCounter) WithGroup(string) slog.Handler      { return c }
func (c *logCounter) errors() int                        { return c.n }
