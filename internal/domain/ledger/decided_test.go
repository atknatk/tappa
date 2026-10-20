package ledger

// decided_test.go -- M12-25: The Spike's "N decided today". The pure half (which instant
// "today" starts at) and the degradation half against real Postgres (a failed count costs the
// figure and never the queue).

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

func TestLocalMidnight_IsTheBusinesssDayNotUTCs(t *testing.T) {
	t.Parallel()
	zone := malta(t)
	cases := []struct {
		name string
		now  time.Time
		zone *time.Location
		want time.Time
	}{
		{
			// 00:30 in Malta (CEST) is still 22:30 the day before in UTC: "today" is Malta's.
			name: "just after the local midnight",
			now:  time.Date(2026, time.June, 3, 22, 30, 0, 0, time.UTC),
			zone: zone, want: time.Date(2026, time.June, 3, 22, 0, 0, 0, time.UTC),
		},
		{
			name: "late in the local evening",
			now:  time.Date(2026, time.June, 3, 21, 59, 0, 0, time.UTC),
			zone: zone, want: time.Date(2026, time.June, 2, 22, 0, 0, 0, time.UTC),
		},
		{
			// 25 October 2026: clocks go back at 03:00 CEST. The day began at 00:00 CEST =
			// 22:00 UTC on the 24th, though the day is 25 hours long.
			name: "the fall-back day starts at its own midnight",
			now:  time.Date(2026, time.October, 25, 20, 0, 0, 0, time.UTC),
			zone: zone, want: time.Date(2026, time.October, 24, 22, 0, 0, 0, time.UTC),
		},
		{
			// 29 March 2026: clocks go forward at 02:00 CET. The day began at 00:00 CET = 23:00 UTC.
			name: "the spring-forward day starts at its own midnight",
			now:  time.Date(2026, time.March, 29, 12, 0, 0, 0, time.UTC),
			zone: zone, want: time.Date(2026, time.March, 28, 23, 0, 0, 0, time.UTC),
		},
		{
			name: "no zone is UTC, as loadZone's fallback",
			now:  time.Date(2026, time.June, 3, 22, 30, 0, 0, time.UTC),
			zone: nil, want: time.Date(2026, time.June, 3, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := localMidnight(c.now, c.zone); !got.Equal(c.want) || got.Location() != time.UTC {
				t.Errorf("localMidnight(%s) = %s, want %s (UTC)", c.now, got, c.want)
			}
		})
	}
}

// shadowReviewsDB shadows transaction_reviews for one transaction with the two columns
// ListFlaggedForReview's anti-join reads, so the queue succeeds and CountReviewsSince fails with
// 42703 on reviewed_at -- a genuine statement error, the advisories_db_test.go method.
type shadowReviewsDB struct{ real *db.DB }

func (d shadowReviewsDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`CREATE TEMP TABLE transaction_reviews (transaction_id uuid, tenant_id uuid) ON COMMIT DROP`); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

func TestQueueDB_AFailedDecidedCountCostsTheFigureAndNeverTheQueue(t *testing.T) {
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

	tenantID, venue, employee := uuid.New(), uuid.New(), uuid.New()
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			  VALUES ($1, 'Spike Probe Ltd', $2, 'bar', 'single', $3)`, []any{tenantID, "VAT-" + tenantID.String(), maltaTenantZone}},
			{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Spike Door')`, []any{venue, tenantID}},
			{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Spike Worker', 'active')`,
				[]any{employee, tenantID, venue}},
			{`INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at, verdict, channel, queued)
			  VALUES ($1, $2, $3, 'in', now(), 'flag', 'nfc', true)`, []any{tenantID, employee, venue}},
		} {
			if _, e := tx.Exec(ctx, stmt.sql, stmt.args...); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// CONTROL: the healthy read measures 0 decisions -- a NUMBER, because it was counted.
	healthy, err := NewReader(data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := healthy.Queue(ctx, tenantID, nil)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if !ok.Decided.Queried || ok.Decided.Failed || ok.Decided.N != 0 || len(ok.Records) != 1 {
		t.Fatalf("CONTROL: decided %+v with %d record(s); want a measured 0 beside the one flag", ok.Decided, len(ok.Records))
	}

	var logged logCounter
	broken, err := NewReader(shadowReviewsDB{real: data}, slog.New(&logged))
	if err != nil {
		t.Fatal(err)
	}
	got, err := broken.Queue(ctx, tenantID, nil)
	if err != nil {
		t.Fatalf("Queue returned %v: a failed count took the queue away -- the savepoint in decidedToday is what stops that", err)
	}
	if !got.Queried || len(got.Records) != 1 {
		t.Fatalf("the queue did not survive: queried %v, %d record(s)", got.Queried, len(got.Records))
	}
	if got.Decided.Queried || !got.Decided.Failed || got.Decided.N != 0 {
		t.Errorf("Decided = %+v, want Failed and no number -- a failed count is 'not counted', never 0", got.Decided)
	}
	if n := logged.errors(); n != 1 {
		t.Errorf("%d error(s) logged, want exactly 1 (§7: a failed read is logged, not swallowed)", n)
	}
}
