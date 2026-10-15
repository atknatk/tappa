package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
)

const op00053File = "00053_keep_the_trail_time_the_databases.sql"

// TestAuditLog_TheAppCannotWriteTheTime (M11 API-6 review; migration 00053): audit_log.at carries
// K-25's budget window (ADR 0028 §12), so tappa_app may not choose it. As tappa_app, an INSERT naming
// `at` -- an hour back, the shape that would take a deactivation out of the budget -- is refused
// with 42501 and writes nothing; the product's own statement (RecordAuditEvent, `at` its DEFAULT)
// still writes, and an INSERT naming `id` (tests do) too.
//
// MUTANT (measured): 00053's narrowing reverted (its Down) -> the backdated row is accepted, red.
func TestAuditLog_TheAppCannotWriteTheTime(t *testing.T) {
	d := appDB(t)
	tenantID, _ := m11Tenant(t, d)
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = sp.Exec(ctx, `INSERT INTO audit_log (tenant_id, actor_id, action, target, detail, at)
		                       VALUES ($1, $2, 'employee.deactivated', 'x', '{"via":"api"}', now() - interval '61 minutes')`, tenantID, uuid.New())
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			_ = sp.Rollback(ctx)
			t.Errorf("tappa_app naming audit_log.at: %v, want 42501", err)
			return nil
		}
		if err := sp.Rollback(ctx); err != nil {
			return err
		}
		if _, err := store.New(tx).RecordAuditEvent(ctx, store.RecordAuditEventParams{
			TenantID: tenantID, Action: "audit.time_probe", Detail: []byte(`{}`),
		}); err != nil {
			t.Errorf("the product's own INSERT (at its DEFAULT) was refused: %v", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO audit_log (id, tenant_id, action) VALUES ($1, $2, 'audit.id_probe')`, uuid.New(), tenantID); err != nil {
			t.Errorf("an INSERT naming id was refused: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestAuditLog00053_DownGivesBackAndUpTakesItAgain: 00053's Down gives tappa_app its table-level
// INSERT back (the time writable again), the Up takes it away again with the six columns kept.
func TestAuditLog00053_DownGivesBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00053File)
	at := `SELECT has_column_privilege('tappa_app', 'public.audit_log', 'at', 'INSERT')::int`
	opRunSection(t, ctx, tx, down, "00053 Down")
	if opInt(t, ctx, tx, at) != 1 || opInt(t, ctx, tx, `SELECT has_table_privilege('tappa_app', 'public.audit_log', 'INSERT')::int`) != 1 {
		t.Fatal("after the Down tappa_app does not hold the table-level INSERT it had before 00053")
	}
	opRunSection(t, ctx, tx, up, "00053 Up again")
	if opInt(t, ctx, tx, at) != 0 {
		t.Fatal("after the Up tappa_app can write audit_log.at")
	}
	if got := opColumns(t, ctx, tx, "tappa_app", "audit_log", "INSERT"); got != "id,tenant_id,actor_id,action,target,detail" {
		t.Fatalf("tappa_app inserts %q after the Up", got)
	}
}

const op00054File = "00054_index_the_invitation_deliveries.sql"

// TestAuditLog00054_TheDeliveryIndexIsPartialAndGoesBack (API-6 review D-3): the index the activation
// e-mail's state is read through exists on (tenant_id, target, at) for the four delivery actions
// alone; 00054's Down removes it and the Up builds it again. The time it buys is measured, not
// pinned here (a plan depends on the data): ADR 0028's API-6 note.
func TestAuditLog00054_TheDeliveryIndexIsPartialAndGoesBack(t *testing.T) {
	ctx, tx := opTx(t)
	def := `SELECT coalesce((SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'audit_log_invite_delivery_idx'), '')`
	var got string
	if err := tx.QueryRow(ctx, def).Scan(&got); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(tenant_id, target, at)", "invite.code_emailed", "invite.code_shown_to_manager", "invite.undelivered", "invite.email_refused"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the delivery index %q does not carry %q", got, want)
		}
	}
	up, down := opMigrationSections(t, op00054File)
	opRunSection(t, ctx, tx, down, "00054 Down")
	if err := tx.QueryRow(ctx, def).Scan(&got); err != nil || got != "" {
		t.Fatalf("after the Down the index is %q (%v)", got, err)
	}
	opRunSection(t, ctx, tx, up, "00054 Up again")
	if err := tx.QueryRow(ctx, def).Scan(&got); err != nil || got == "" {
		t.Fatalf("after the Up the index is missing (%v)", err)
	}
}
