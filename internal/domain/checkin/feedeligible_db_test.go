package checkin

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// TestFeedEligible_IsPunchFeedsEntryRule holds feedEligible -- insert's choice of the savepoint and
// the one retry -- to the INSTALLED punch_feed trigger it mirrors (M16-19, Q38 (a): since 00112 the
// trigger takes the live QR channel too, and the Go copy must say so in the same commit). For every
// channel the transactions CHECK admits (and one it refuses) x verdict x practice x direction, a row
// without a site is written through the product's InsertTransaction as tappa_app in its tenant's
// context, and the trigger's answer (did it write an event?) is compared with feedEligible's: the
// trigger writes an event exactly when feedEligible says so and the row is directed (feedEligible does
// not read the direction -- a superset is safe: a savepoint for a row that takes no lock costs a
// savepoint, a missing one costs the record on a 55P03).
//
// Everything runs in one owner transaction that is rolled back; each row in a savepoint of its own (a
// transaction writes feed events under one counter set -- ADR 0028 §5 -- and here it is one tenant).
func TestFeedEligible_IsPunchFeedsEntryRule(t *testing.T) {
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" || os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; the trigger is the reference)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	// entered_by carries no key (a manual row's author is the trail's), so a fresh id stands in.
	tenant, venue, employee, manager := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'm16-19 feed', $2, 'restaurant', 'multi')`,
			[]any{tenant, "VAT-M1619-" + tenant.String()}},
		{`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm16-19 venue')`, []any{venue, tenant}},
		{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Maria Borg', 'active')`,
			[]any{employee, tenant, venue}},
		{`SET LOCAL ROLE tappa_app`, nil},
		{`SELECT set_config('app.tenant_id', $1, true)`, []any{tenant.String()}},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}

	in := string(tap.TypeIn)
	seen := map[string]int{}
	for _, channel := range []string{string(tap.ChannelNFC), string(tap.ChannelQR), string(tap.ChannelQRLive), string(tap.ChannelManual), "QR_LIVE"} {
		for _, verdict := range []tap.Verdict{tap.VerdictOK, tap.VerdictFlag, tap.VerdictReject, tap.VerdictIgnored} {
			for _, practice := range []bool{false, true} {
				for _, directed := range []bool{true, false} {
					if verdict == tap.VerdictOK && !directed {
						continue // transactions_ok_has_direction
					}
					if (verdict == tap.VerdictReject || verdict == tap.VerdictIgnored) && directed {
						continue // transactions_refusal_has_no_direction
					}
					p := store.InsertTransactionParams{
						TenantID: tenant, EmployeeID: &employee, LocationID: &venue,
						OccurredAt: time.Now().Add(-5 * time.Second), Verdict: string(verdict), Channel: channel, Practice: practice,
					}
					if directed {
						p.Type = &in
					}
					if channel == string(tap.ChannelManual) {
						p.EnteredBy = &manager
					}
					name := fmt.Sprintf("%s/%s/practice=%v/directed=%v", channel, verdict, practice, directed)
					event, written := feedEventOf(t, ctx, tx, p)
					if channel == "QR_LIVE" {
						if written {
							t.Errorf("%s: a channel outside the set was written", name)
						}
						continue
					}
					if !written {
						t.Fatalf("%s: the row was refused", name)
					}
					want := feedEligible(p) && directed
					if event != want {
						t.Errorf("%s: punch_feed wrote an event = %v, feedEligible (and a direction) = %v", name, event, want)
					}
					seen[fmt.Sprintf("%s event=%v", channel, event)]++
				}
			}
		}
	}
	for _, must := range []string{"nfc event=true", "qr event=true", "qr_live event=true", "qr_live event=false", "manual event=false"} {
		if seen[must] == 0 {
			t.Errorf("no row answered %q -- the comparison did not reach that case (seen %v)", must, seen)
		}
	}
}

// feedEventOf writes p in a savepoint of tx and reports whether punch_feed wrote an event for it and
// whether the row was written at all; the savepoint is rolled back either way.
func feedEventOf(t *testing.T, ctx context.Context, tx pgx.Tx, p store.InsertTransactionParams) (event, written bool) {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
	}()
	row, err := store.New(sp).InsertTransaction(ctx, p)
	if err != nil {
		return false, false
	}
	var n int
	if err := sp.QueryRow(ctx, `SELECT count(*) FROM punch_events WHERE tenant_id = $1 AND punch_id = $2`, p.TenantID, row.ID).Scan(&n); err != nil {
		t.Fatalf("count the events: %v", err)
	}
	return n == 1, true
}
