package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
)

const op00099File = "00099_make_session_revocation_one_way.sql"

// sessionRevocationFixture is one employee with two live tap sessions, made as tappa_app.
func sessionRevocationFixture(t *testing.T, d *DB) (tenantID, employee, first, second uuid.UUID) {
	t.Helper()
	tenantID, venue := m11Tenant(t, d)
	employee, first, second = uuid.New(), uuid.New(), uuid.New()
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, status)
		                          VALUES ($1, $2, $3, 'Maria Borg', 'active')`, employee, tenantID, venue); e != nil {
			return e
		}
		for _, id := range []uuid.UUID{first, second} {
			if _, e := tx.Exec(ctx, `INSERT INTO sessions (id, tenant_id, employee_id, token_hash) VALUES ($1, $2, $3, $4)`,
				id, tenantID, employee, opRandHex(t)); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("session fixture: %v", err)
	}
	return tenantID, employee, first, second
}

// refusedAs23001 runs one statement in a savepoint and reports whether the trigger refused it.
func refusedAs23001(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) bool {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	_, err = sp.Exec(ctx, sql, args...)
	var pg *pgconn.PgError
	refused := errors.As(err, &pg) && pg.Code == "23001"
	if err != nil && !refused {
		t.Errorf("%s: %v, want 23001 or success", sql, err)
	}
	if rbErr := sp.Rollback(ctx); rbErr != nil {
		t.Fatalf("rollback savepoint: %v", rbErr)
	}
	return refused
}

// TestSessions_RevocationIsOneWayForTheApp (T59 security review ORTA-2; migration 00099): as tappa_app,
// a revoked tap session cannot be made live again -- the measured shape `SET revoked_at = NULL ...
// RETURNING` -- nor re-stamped, and a first stamp cannot be back-dated or set in the future. The
// product's own three statements still work: the bulk revoke, the idempotent single revoke (same
// stamp back) and the last-use refresh.
//
// MUTANT (measured): 00099's Down applied -> the un-revoke succeeds, red.
func TestSessions_RevocationIsOneWayForTheApp(t *testing.T) {
	d := appDB(t)
	tenantID, employee, first, second := sessionRevocationFixture(t, d)
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		for _, tc := range []struct{ name, sql string }{
			{"a back-dated first stamp", `UPDATE sessions SET revoked_at = now() - interval '1 hour' WHERE id = $1`},
			{"a first stamp in the future", `UPDATE sessions SET revoked_at = clock_timestamp() + interval '1 hour' WHERE id = $1`},
		} {
			if !refusedAs23001(t, ctx, tx, tc.sql, first) {
				t.Errorf("%s on a live session was accepted", tc.name)
			}
		}
		// The product's bulk revoke (now(), `revoked_at IS NULL`) passes the trigger.
		ids, err := q.RevokeSessionsForEmployee(ctx, store.RevokeSessionsForEmployeeParams{TenantID: tenantID, EmployeeID: employee})
		if err != nil || len(ids) != 2 {
			t.Fatalf("RevokeSessionsForEmployee: %d revoked, %v; want 2, nil", len(ids), err)
		}
		var stamp time.Time
		if err := tx.QueryRow(ctx, `SELECT revoked_at FROM sessions WHERE id = $1`, first).Scan(&stamp); err != nil {
			return err
		}
		for _, tc := range []struct{ name, sql string }{
			{"un-revoking (the measured shape)", `UPDATE sessions SET revoked_at = NULL WHERE id = $1 RETURNING id`},
			{"back-dating the stamp", `UPDATE sessions SET revoked_at = revoked_at - interval '1 minute' WHERE id = $1`},
			{"re-stamping it now", `UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1`},
		} {
			if !refusedAs23001(t, ctx, tx, tc.sql, first) {
				t.Errorf("%s on a revoked session was accepted", tc.name)
			}
		}
		// The idempotent single revoke writes the same stamp back and does not fire the trigger.
		row, err := q.RevokeSession(ctx, store.RevokeSessionParams{ID: first, TenantID: tenantID})
		if err != nil || row.RevokedAt == nil || !row.RevokedAt.Equal(stamp) {
			t.Errorf("RevokeSession on a revoked session: %v, %v; want the first stamp %v back", row.RevokedAt, err, stamp)
		}
		var live int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id IN ($1, $2) AND revoked_at IS NULL`, first, second).Scan(&live); err != nil {
			return err
		}
		if live != 0 {
			t.Errorf("%d session(s) live again", live)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A live session's last-use refresh never enters the function.
	tenant2, _, s1, _ := sessionRevocationFixture(t, d)
	if err := d.WithTenant(context.Background(), tenant2, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).TouchSession(ctx, store.TouchSessionParams{ID: s1, TenantID: tenant2})
		return e
	}); err != nil {
		t.Errorf("TouchSession on a live session: %v", err)
	}
}

// TestSessions00099_DownTakesTheTriggerAndUpPutsItBack: the Down removes the trigger and its function;
// the Up binds them again, enabled, BEFORE UPDATE FOR EACH ROW with the WHEN clause, EXECUTE in the
// owner alone.
func TestSessions00099_DownTakesTheTriggerAndUpPutsItBack(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00099File)
	def := `SELECT coalesce((SELECT pg_get_triggerdef(oid) FROM pg_trigger WHERE tgname = 'sessions_revocation_one_way' AND tgenabled = 'O'), '')`
	var got string
	if err := tx.QueryRow(ctx, def).Scan(&got); err != nil {
		t.Fatal(err)
	}
	const want = "CREATE TRIGGER sessions_revocation_one_way BEFORE UPDATE ON public.sessions FOR EACH ROW WHEN ((new.revoked_at IS DISTINCT FROM old.revoked_at)) EXECUTE FUNCTION sessions_revocation_is_one_way()"
	if got != want {
		t.Fatalf("the trigger is\n  %q\nwant\n  %q", got, want)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname = 'sessions_revocation_is_one_way'
	                           AND NOT prosecdef AND proacl::text = '{tappa_owner=X/tappa_owner}'`); n != 1 {
		t.Fatalf("the function is not an INVOKER function with EXECUTE in its owner alone (%d)", n)
	}
	opRunSection(t, ctx, tx, down, "00099 Down")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_trigger WHERE tgname = 'sessions_revocation_one_way'`) +
		opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname = 'sessions_revocation_is_one_way'`); n != 0 {
		t.Fatalf("after the Down %d trigger/function object(s) remain", n)
	}
	opRunSection(t, ctx, tx, up, "00099 Up again")
	if err := tx.QueryRow(ctx, def).Scan(&got); err != nil || got != want {
		t.Fatalf("after the Up the trigger is %q (%v)", got, err)
	}
}
