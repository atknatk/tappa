package handler

// identityresetaudit_db_test.go -- M14-08 (its audit's second finding): a LINKED row's recovery is
// spent by identity_consume_row_reset (00074's body), which writes the business's
// admin.recovery.completed row itself, and the handler writes none (adminauth.ConsumedReset.Recorded).
// That row must be the one THIS package writes for an unlinked row (adminReset's success branch,
// adminResetDetail with its omitempty tags), key for key and value for value: a row of another
// shape would tell the business's owner the account is linked, on a surface the flag does not
// close. One owner transaction, rolled back, as identityaudit_db_test.go.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
)

func randomLinkHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestIdentityResetAudit_ALinkedRowsRecoveryRowIsTheHandlersRow: for a linked row's link spent
// through identity_consume_row_reset, the detail the database wrote in the link's business is, as
// jsonb text, exactly the detail the handler writes for the same reset id, retired count and revoked
// sessions -- with siblings and sessions (every key present) and without (reset_id alone beside the
// outcome: omitempty drops both counts).
//
// MUTATION (measured): 00072's body (revoked_sessions always, no reset_id, no retired_count) -> red.
func TestIdentityResetAudit_ALinkedRowsRecoveryRowIsTheHandlersRow(t *testing.T) {
	for _, c := range []struct {
		name               string
		siblings, sessions int
	}{
		{"one other live link and two sessions", 1, 2},
		{"nothing else to retire or revoke", 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			pool := ownerPoolForTest(t)
			ctx := context.Background()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := tx.Rollback(ctx); err != nil {
					t.Errorf("rollback: %v", err)
				}
			}()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := tx.Exec(ctx, sql, args...); err != nil {
					t.Fatalf("%s: %v", sql, err)
				}
			}
			// The same transaction-scoped shared lock as identityaudit_db_test.go: nothing is committed.
			exec(`SELECT pg_advisory_xact_lock_shared(hashtext($1))`, identityAuditTablesLock)
			tenant, admin := uuid.New(), uuid.New()
			exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'M14-08 reset audit', $2, 'restaurant', 'single')`,
				tenant, "VAT-M1408R-"+tenant.String())
			email := "m14-08-reset-" + uuid.NewString() + "@identity.example"
			var ident uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`, email, ownerDigest).Scan(&ident); err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
			      VALUES ($1, $2, 'Reset Person', $3, $4, 'owner', $5, 'backfill', now())`, admin, tenant, email, ownerDigest, ident)
			link := randomLinkHash(t)
			var resetID uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at)
			                            VALUES ($1, $2, $3, now() + interval '1 hour') RETURNING id`, tenant, admin, link).Scan(&resetID); err != nil {
				t.Fatal(err)
			}
			for range c.siblings {
				exec(`INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`,
					tenant, admin, randomLinkHash(t))
			}
			for range c.sessions {
				exec(`INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`, tenant, admin, randomLinkHash(t))
			}

			exec(`SET LOCAL SESSION AUTHORIZATION tappa_app`)
			var revoked int32
			var gotReset uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT reset_id, revoked_sessions FROM identity_consume_row_reset($1, $2)`, link, victimDigest).Scan(&gotReset, &revoked); err != nil {
				t.Fatalf("identity_consume_row_reset: %v", err)
			}
			exec(`SET LOCAL SESSION AUTHORIZATION DEFAULT`)
			if gotReset != resetID || int(revoked) != c.sessions {
				t.Fatalf("consumed %s revoking %d, want %s and %d", gotReset, revoked, resetID, c.sessions)
			}
			var definer string
			if err := tx.QueryRow(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2`, tenant, ActionAdminResetCompleted).Scan(&definer); err != nil {
				t.Fatalf("the database's row: %v", err)
			}

			rec, err := audit.New(noTenantDB{})
			if err != nil {
				t.Fatal(err)
			}
			// adminReset's success branch, field for field.
			ordinaryID, err := rec.RecordTx(ctx, tx, audit.Event{
				TenantID: tenant, ActorID: ptr(admin), Action: ActionAdminResetCompleted, Target: admin.String(),
				Detail: adminResetDetail{Outcome: "ok", ResetID: resetID.String(), RetiredCount: int64(c.siblings), RevokedSessions: c.sessions},
			})
			if err != nil {
				t.Fatal(err)
			}
			var ordinary string
			if err := tx.QueryRow(ctx, `SELECT detail::text FROM audit_log WHERE id = $1`, ordinaryID).Scan(&ordinary); err != nil {
				t.Fatal(err)
			}
			if definer != ordinary {
				t.Fatalf("a linked row's recovery row is not the handler's:\n database %s\n handler  %s", definer, ordinary)
			}
			var same int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log a JOIN audit_log b ON b.id = $2
			                            WHERE a.tenant_id = $1 AND a.action = $3 AND a.id <> b.id
			                              AND a.actor_id = b.actor_id AND a.target = b.target`, tenant, ordinaryID, ActionAdminResetCompleted).Scan(&same); err != nil || same != 1 {
				t.Fatalf("actor and target differ from the handler's row (%d, %v)", same, err)
			}
		})
	}
}
