package manual

// within_db_test.go -- M15-16: Within, the variant of Record that takes a caller's work into the
// person's transaction (a punch claim's approval, internal/domain/claim), and Entry.AnchorID, the
// record filed at the claim's anchor's venue. Real Postgres, the race world's own tenant.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (w *raceWorld) count(sql string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.owner.QueryRow(w.ctx, sql, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// TestWithin_TheCallersFailureTakesTheRecordWithIt: a record written through write and then a
// failure of the caller's own work -- neither the record nor its trail row stays (one transaction).
func TestWithin_TheCallersFailureTakesTheRecordWithIt(t *testing.T) {
	w := newRaceWorld(t)
	at := time.Now().Add(-2 * time.Hour).UTC()
	boom := errors.New("the caller's own row failed")
	err := w.rec.Within(w.ctx, w.tenant, w.who, func(ctx context.Context, tx pgx.Tx, write Write) error {
		if _, err := write(Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: Out, At: at}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Within = %v, want the caller's error", err)
	}
	if n := w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual'`, w.tenant); n != 0 {
		t.Errorf("%d manual records survived the rollback", n)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, w.tenant, Action); n != 0 {
		t.Errorf("%d record.entered rows survived the rollback", n)
	}
	// and the same work without the failure commits both
	if err := w.rec.Within(w.ctx, w.tenant, w.who, func(ctx context.Context, tx pgx.Tx, write Write) error {
		_, err := write(Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: Out, At: at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual'`, w.tenant); n != 1 {
		t.Errorf("%d manual records, want 1", n)
	}
}

// TestWithin_WritesOnlyTheLockedPersonsRecord: an entry for somebody other than the person whose lock
// the transaction holds is refused before anything is written; Write's refusals are Record's.
func TestWithin_WritesOnlyTheLockedPersonsRecord(t *testing.T) {
	w := newRaceWorld(t)
	at := time.Now().Add(-time.Hour).UTC()
	err := w.rec.Within(w.ctx, w.tenant, w.who, func(ctx context.Context, tx pgx.Tx, write Write) error {
		if _, err := write(Entry{TenantID: w.tenant, EmployeeID: uuid.New(), EnteredBy: w.admin, Direction: Out, At: at}); err == nil {
			t.Error("an entry for another person was written under this person's lock")
		}
		if _, err := write(Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: Out, At: at.Add(-MaxBackdate - time.Hour)}); !errors.Is(err, ErrTooOld) {
			t.Errorf("a moment past MaxBackdate: %v, want ErrTooOld", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.rec.Within(w.ctx, uuid.Nil, w.who, func(context.Context, pgx.Tx, Write) error { return nil }); err == nil {
		t.Error("Within without a tenant ran")
	}
	if err := w.rec.Within(w.ctx, w.tenant, uuid.Nil, func(context.Context, pgx.Tx, Write) error { return nil }); !errors.Is(err, ErrUnknownEmployee) {
		t.Errorf("Within without a person: %v", err)
	}
	if err := w.rec.Within(w.ctx, w.tenant, w.who, nil); err == nil {
		t.Error("Within with nothing to run ran")
	}
	if n := w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual'`, w.tenant); n != 0 {
		t.Errorf("%d manual records", n)
	}
}

// TestWithin_AnAnswerToAClaimIsFiledAtItsAnchorsVenue (ADR 0032 §C.4, "mekân = çapanın mekânı"): with
// Entry.AnchorID the record goes to the venue the anchor was tapped at, not the person's home; an
// anchor filed at no local venue writes nothing (ErrVenue).
func TestWithin_AnAnswerToAClaimIsFiledAtItsAnchorsVenue(t *testing.T) {
	w := newRaceWorld(t)
	var elsewhere uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name, static_ips) VALUES ($1, 'm15-16 elsewhere', '{198.51.100.0/24}') RETURNING id`,
		w.tenant).Scan(&elsewhere); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-5 * time.Hour).UTC().Truncate(time.Second)
	anchor := func(loc *uuid.UUID) uuid.UUID {
		var id uuid.UUID
		if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, verdict, channel, type, trust, entered_by)
			                         VALUES ($1, $2, $3, $4, 'ok', 'manual', 'in', 20, $5) RETURNING id`, w.tenant, w.who, loc, at, w.admin).Scan(&id)
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	there := anchor(&elsewhere)
	got, err := w.rec.Record(w.ctx, Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: Out,
		At: at.Add(time.Hour), AnchorID: there})
	if err != nil {
		t.Fatal(err)
	}
	var loc uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `SELECT location_id FROM transactions WHERE id = $1`, got.ID).Scan(&loc); err != nil || loc != elsewhere {
		t.Errorf("filed at %s (%v), want the anchor's venue %s (home %s)", loc, err, elsewhere, w.venue)
	}
	nowhere := anchor(nil)
	if _, err := w.rec.Record(w.ctx, Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: Out,
		At: at.Add(2 * time.Hour), AnchorID: nowhere}); !errors.Is(err, ErrVenue) {
		t.Errorf("an anchor at no venue: %v, want ErrVenue", err)
	}
	if n := w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual' AND entered_by = $2 AND type = 'out'`, w.tenant, w.admin); n != 1 {
		t.Errorf("%d answers written, want 1", n)
	}
}

// TestWithin_AnAnswerToAnotherPersonsAnchorWritesNothing (M15-16 review D-2): the statement reads the
// anchor under the tenant AND the person -- another person's check-in is no anchor for this one, so
// the record is refused (ErrVenue: no row) and neither person gets a row. Without the person
// predicate the JOIN would file the record for the anchor's person.
func TestWithin_AnAnswerToAnotherPersonsAnchorWritesNothing(t *testing.T) {
	w := newRaceWorld(t)
	other := uuid.New()
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at, location_id) VALUES ($1, $2, $3, 'Other Staff', 'active', now(), $4)`,
		other, w.tenant, "ext-"+other.String()[:8], w.venue)
	at := time.Now().Add(-5 * time.Hour).UTC().Truncate(time.Second)
	var anchor uuid.UUID
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, verdict, channel, type, trust, entered_by)
		                         VALUES ($1, $2, $3, $4, 'ok', 'manual', 'in', 20, $5) RETURNING id`, w.tenant, other, w.venue, at, w.admin).Scan(&anchor)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.rec.Record(w.ctx, Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin, Direction: Out,
		At: at.Add(time.Hour), AnchorID: anchor}); !errors.Is(err, ErrVenue) {
		t.Errorf("another person's anchor: %v, want ErrVenue", err)
	}
	if n := w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND channel = 'manual' AND type = 'out'`, w.tenant); n != 0 {
		t.Errorf("%d answers written, want none", n)
	}
}
