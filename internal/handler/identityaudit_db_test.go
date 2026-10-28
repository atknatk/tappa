package handler

// identityaudit_db_test.go -- M14-04 (ADR 0031 §5.4 step 6, bulgu 17): the audit rows the identity
// definers write in SQL (migration 00071) are BYTE-IDENTICAL to the ordinary rows this package
// writes for a sign-in and a sign-out. It lives here, not in internal/db, because "ordinary" is
// defined here: adminLoginSucceededEvent and adminLoggedOutEvent, written through internal/audit
// exactly as the login and Logout handlers write them. Everything runs in one owner transaction that
// is rolled back (00069's Down refuses while an identity exists).

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
)

// noTenantDB satisfies audit.Database for a Recorder whose RecordTx alone is used: the caller holds
// the transaction, so WithTenant is never reached.
type noTenantDB struct{}

func (noTenantDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	return errors.New("not used: the events are written with RecordTx")
}

// TestIdentityAudit_TheDefinersRowsAreTheOrdinaryOnes: a person linked to businesses A and B opens a
// session in A (identity_open_session), switches it to B (identity_switch_session) and signs out
// everywhere (identity_revoke_sessions), all as tappa_app. Every row those calls wrote -- A's
// sign-in, A's sign-out, B's sign-in, B's sign-out -- is compared, column by column (tenant_id,
// actor_id, action, target, detail as text), with the row adminLoginSucceededEvent /
// adminLoggedOutEvent writes for the same tenant, admin and session. Equal, and nothing else written.
// identityAuditTablesLock is internal/db's operator-tables test lock (operatorschema_test.go).
const identityAuditTablesLock = "tappa/test/operator-tables"

func TestIdentityAudit_TheDefinersRowsAreTheOrdinaryOnes(t *testing.T) {
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
	// The identity below is never committed, but while this transaction holds it a schema test's
	// Down would wait on (or deadlock with) its locks: take turns with internal/db's exclusive opTx
	// the way internal/handler/api's rig does -- transaction-scoped, shared.
	exec(`SELECT pg_advisory_xact_lock_shared(hashtext($1))`, identityAuditTablesLock)
	a, b := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{a, b} {
		exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'restaurant', 'single')`,
			id, "M14 audit "+id.String()[:8], "VAT-M14A-"+id.String())
	}
	email := "m14-audit-" + uuid.NewString() + "@identity.example"
	var ident uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`, email, ownerDigest).Scan(&ident); err != nil {
		t.Fatal(err)
	}
	ma, mb := uuid.New(), uuid.New()
	for _, m := range []struct{ id, tenant uuid.UUID }{{ma, a}, {mb, b}} {
		exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
		      VALUES ($1, $2, 'Audit Person', $3, $4, 'owner', $5, 'backfill', now())`, m.id, m.tenant, email, ownerDigest, ident)
	}

	asApp := func(sql string, args ...any) []any {
		t.Helper()
		exec(`SET LOCAL SESSION AUTHORIZATION tappa_app`)
		defer exec(`SET LOCAL SESSION AUTHORIZATION DEFAULT`)
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		got, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) ([]any, error) { return r.Values() })
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: %d rows, %v", sql, len(got), err)
		}
		return got[0]
	}
	h1, h2 := uuid.NewString()+uuid.NewString(), uuid.NewString()+uuid.NewString()
	opened := asApp(`SELECT session_id FROM identity_open_session($1, $2, $3, $4)`, ident, a, h1, ownerDigest)
	s1 := uuid.UUID(opened[0].([16]byte))
	switched := asApp(`SELECT session_id FROM identity_switch_session($1, $2, $3)`, h1, b, h2)
	s2 := uuid.UUID(switched[0].([16]byte))
	if n := asApp(`SELECT revoked FROM identity_revoke_sessions($1, false)`, h2); n[0] != int32(1) {
		t.Fatalf("sign out everywhere revoked %v sessions, want 1", n[0])
	}

	type row struct {
		Tenant uuid.UUID
		Actor  *uuid.UUID
		Action string
		Target *string
		Detail string
	}
	read := func(where string) []row {
		t.Helper()
		rows, err := tx.Query(ctx, `SELECT tenant_id, actor_id, action, target, detail::text FROM audit_log
		                             WHERE tenant_id IN ($1, $2) AND `+where+` ORDER BY tenant_id, action, detail::text`, a, b)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	definers := read(`true`)
	if len(definers) != 4 {
		t.Fatalf("the three calls wrote %d audit rows in A and B, want 4: %+v", len(definers), definers)
	}

	rec, err := audit.New(noTenantDB{})
	if err != nil {
		t.Fatal(err)
	}
	ordinary := []audit.Event{
		adminLoginSucceededEvent(a, ma, s1), // the login lands in A
		adminLoggedOutEvent(a, ma, s1),      // the switch leaves A
		adminLoginSucceededEvent(b, mb, s2), // ... and enters B
		adminLoggedOutEvent(b, mb, s2),      // sign out everywhere
	}
	for _, e := range ordinary {
		if _, err := rec.RecordTx(ctx, tx, e); err != nil {
			t.Fatalf("record the ordinary %s: %v", e.Action, err)
		}
	}
	all := read(`true`)
	if len(all) != 8 {
		t.Fatalf("%d rows after the ordinary ones, want 8", len(all))
	}
	// Rows sort by (tenant, action, detail): each definer row is followed by its ordinary twin.
	for i := 0; i < len(all); i += 2 {
		d, o := all[i], all[i+1]
		if d.Tenant != o.Tenant || d.Action != o.Action || d.Detail != o.Detail ||
			d.Actor == nil || o.Actor == nil || *d.Actor != *o.Actor ||
			d.Target == nil || o.Target == nil || *d.Target != *o.Target {
			t.Errorf("not byte-identical:\n definer  %+v (%v, %v)\n ordinary %+v (%v, %v)", d, idDerefUUID(d.Actor), idDerefStr(d.Target), o, idDerefUUID(o.Actor), idDerefStr(o.Target))
		}
	}
}

func idDerefUUID(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return *p
}

func idDerefStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
