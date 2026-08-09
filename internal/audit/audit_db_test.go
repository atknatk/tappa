package audit

// Tests against a REAL Postgres: the properties measured here (RLS isolation, the
// NOT NULL tenant, the append-only privileges from migration 00005) belong to the
// database and a fake would only agree with the code.
//
// Fixtures are NOT cleaned up — audit_log has REVOKE DELETE plus a BEFORE DELETE
// trigger, which is the point of the table. Fresh random UUIDs keep runs apart.

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping audit DB tests (real Postgres required)")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

func newTenant(t *testing.T, d *db.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := d.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Audit Test Ltd', $2, 'bar', 'single')`,
			id, "VAT-"+id.String())
		return e
	})
	if err != nil {
		t.Fatalf("newTenant: %v", err)
	}
	return id
}

type detail struct {
	Outcome string `json:"outcome"`
	Count   int    `json:"count"`
}

// TestRecord_WritesTheRowItWasGiven, detail included and well-formed as jsonb.
func TestRecord_WritesTheRowItWasGiven(t *testing.T) {
	d := testDB(t)
	tenant := newTenant(t, d)
	r, err := New(d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	target := uuid.New()

	id, err := r.Record(context.Background(), Event{
		TenantID: tenant,
		Action:   "activation.completed",
		Target:   target.String(),
		Detail:   detail{Outcome: "ok", Count: 2},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	var action, gotTarget, gotDetail string
	var actor *uuid.UUID
	err = d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT action, target, detail::text, actor_id FROM audit_log WHERE tenant_id = $1 AND id = $2`,
			tenant, id).Scan(&action, &gotTarget, &gotDetail, &actor)
	})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if action != "activation.completed" || gotTarget != target.String() {
		t.Errorf("row = %q / %q", action, gotTarget)
	}
	if actor != nil {
		t.Error("actor_id must stay NULL for an employee acting on themselves (00005: the column is for admins)")
	}
	var back detail
	if err := json.Unmarshal([]byte(gotDetail), &back); err != nil {
		t.Fatalf("detail is not well-formed json: %v", err)
	}
	if back.Outcome != "ok" || back.Count != 2 {
		t.Errorf("detail round-trip = %+v", back)
	}
}

// TestRecord_NilDetailBecomesAnEmptyObject, never SQL NULL: 00005 wants every row
// to carry a well-formed object.
func TestRecord_NilDetailBecomesAnEmptyObject(t *testing.T) {
	d := testDB(t)
	tenant := newTenant(t, d)
	r, _ := New(d)

	id, err := r.Record(context.Background(), Event{TenantID: tenant, Action: "test.no_detail"})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	var got string
	err = d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND id = $2`,
			tenant, id).Scan(&got)
	})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "{}" {
		t.Fatalf("detail = %q, want {}", got)
	}
}

// TestRecord_RefusesWhatItCannotStore. The unattributable event is the important
// one: audit_log.tenant_id is NOT NULL, so an activation attempt with a code that
// resolves to nothing CANNOT be written here, and the caller must handle that
// rather than discover it as a foreign-key error.
func TestRecord_RefusesWhatItCannotStore(t *testing.T) {
	d := testDB(t)
	r, _ := New(d)
	tenant := newTenant(t, d)

	if _, err := r.Record(context.Background(), Event{Action: "x"}); err == nil {
		t.Error("an event with no tenant must be refused before it reaches SQL")
	}
	if _, err := r.Record(context.Background(), Event{TenantID: tenant}); err == nil {
		t.Error("an event with no action must be refused")
	}
	// A detail that cannot be marshalled is an ERROR, never a dropped field or a
	// fmt.Sprintf fallback — that fallback is how a secret reaches a trail.
	_, err := r.Record(context.Background(), Event{
		TenantID: tenant, Action: "test.bad_detail", Detail: math.Inf(1),
	})
	if err == nil {
		t.Fatal("an unmarshallable detail must fail loudly")
	}
	if !strings.Contains(err.Error(), "test.bad_detail") {
		t.Error("the error must name the action so the caller can find it")
	}
	// THE MEASURED BOUNDARY, not a claim: encoding/json's error CAN quote the
	// offending value (+Inf here), and params() documents why that is acceptable
	// — every Go string marshals successfully, so no secret can reach this
	// branch. This assertion pins the reasoning: if a STRING ever started failing
	// to marshal, the premise would be wrong and this test would need revisiting.
	if b, jsonErr := json.Marshal("any string at all"); jsonErr != nil || string(b) == "" {
		t.Fatalf("a plain string failed to marshal (%v): the §4.7 argument in params() no longer holds", jsonErr)
	}
}

// TestRecord_IsTenantIsolated: tenant A's trail is invisible to tenant B. The
// probe deliberately carries NO tenant_id filter, so a zero result is RLS and not
// a WHERE clause (CLAUDE.md §6).
func TestRecord_IsTenantIsolated(t *testing.T) {
	d := testDB(t)
	a, b := newTenant(t, d), newTenant(t, d)
	r, _ := New(d)

	marker := "isolation." + uuid.NewString()
	if _, err := r.Record(context.Background(), Event{TenantID: a, Action: marker}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	count := func(tenant uuid.UUID) int {
		var n int
		if err := d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1`, marker).Scan(&n)
		}); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if got := count(b); got != 0 {
		t.Fatalf("tenant B sees %d of tenant A's audit rows", got)
	}
	// Positive control: the row really is there for its owner, so the zero above
	// is isolation rather than an insert that never happened.
	if got := count(a); got != 1 {
		t.Fatalf("tenant A sees %d of its own rows, want 1", got)
	}
}

// TestAuditLog_IsAppendOnlyForTheApp measures the privileges rather than assuming
// them (M1-04 lesson: "absent from the GRANT" is not "revoked").
func TestAuditLog_IsAppendOnlyForTheApp(t *testing.T) {
	d := testDB(t)
	tenant := newTenant(t, d)
	err := d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		for _, tc := range []struct {
			priv string
			want bool
		}{
			{"SELECT", true},
			{"INSERT", true},
			{"UPDATE", false},
			{"DELETE", false},
		} {
			var got bool
			if e := tx.QueryRow(ctx,
				`SELECT has_table_privilege('tappa_app', 'audit_log', $1)`, tc.priv).Scan(&got); e != nil {
				return e
			}
			if got != tc.want {
				t.Errorf("has_table_privilege(tappa_app, audit_log, %s) = %v, want %v", tc.priv, got, tc.want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("privilege probe: %v", err)
	}
}
