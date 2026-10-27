package db

// identitysignup_test.go -- M14-08 (ADR 0031 §5.2 named exception 2, migration 00074):
// identity_create_for_signup against real Postgres, called AS tappa_app through the very SQL
// CreateIdentityForSignup sends, in opTx's transaction (rolled back -- 00069's Down refuses while an
// identity exists). internal/domain/signup's tests drive the same function through the real Provision
// path, committed, and two registrations at once.
//
// 🔴 THE WORLDS ARE WRITTEN AT THE TOP LEVEL OF THE TEST'S TRANSACTION, every call in a savepoint
// over them. The function links only a row THIS transaction wrote (xmin = pg_current_xact_id()), and
// a row inserted inside a savepoint carries the subtransaction's xid: a world built in a savepoint
// would be refused for that reason alone and prove nothing about the others. The S13 case below is
// exactly that shape, on purpose.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// isWorld is one registration: a business and its one owner row, both born in the test's transaction.
type isWorld struct {
	tenant, admin uuid.UUID
	email, digest string
}

// isNewWorld writes the world as the owner at the level of q; tenantAge / adminAge backdate the two
// created_at (zero = the transaction's now(), as a registration writes them).
func isNewWorld(t *testing.T, ctx context.Context, q pgx.Tx, tenantAge, adminAge string) isWorld {
	t.Helper()
	w := isWorld{tenant: uuid.New(), admin: uuid.New(), email: aiEmail(), digest: opFakeDigest("s")}
	if tenantAge == "" {
		tenantAge = "0"
	}
	if adminAge == "" {
		adminAge = "0"
	}
	aiExec(t, ctx, q, `INSERT INTO tenants (id, name, vat_number, business_type, structure, created_at)
	                   VALUES ($1, 'M14-08 signup', $2, 'restaurant', 'single', now() - $3::interval)`, w.tenant, "VAT-M1408-"+w.tenant.String(), tenantAge+" seconds")
	aiExec(t, ctx, q, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, created_at)
	                   VALUES ($1, $2, 'M14-08 Owner', $3, $4, 'owner', now() - $5::interval)`, w.admin, w.tenant, w.email, w.digest, adminAge+" seconds")
	return w
}

// isCall calls identity_create_for_signup AS tappa_app under the context guc (the business's own,
// unless the case says otherwise) and returns its answer.
func isCall(t *testing.T, ctx context.Context, q pgx.Tx, guc, tenant, admin uuid.UUID) bool {
	t.Helper()
	var linked bool
	err := aiApp(t, ctx, q, guc, func(sp pgx.Tx) error {
		var e error
		linked, e = CreateIdentityForSignup(ctx, sp, tenant, admin)
		return e
	})
	if err != nil {
		t.Fatalf("identity_create_for_signup: %v", err)
	}
	return linked
}

// isLinked reports the row's link: identity id (Nil when unlinked) and linked_via.
func isLinked(t *testing.T, ctx context.Context, q pgx.Tx, admin uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	var (
		ident *uuid.UUID
		via   *string
	)
	if err := q.QueryRow(ctx, `SELECT identity_id, linked_via FROM admin_users WHERE id = $1`, admin).Scan(&ident, &via); err != nil {
		t.Fatal(err)
	}
	if ident == nil {
		return uuid.Nil, ""
	}
	return *ident, *via
}

// TestIdentityCreateForSignup_BirthsAnIdentityForANewAddressAndNothingElse is named exception 2's
// behaviour. A new address in a business born in this transaction: true, ONE identity carrying the
// row's digest byte for byte (the dual-write invariant), the row linked (`signup`, linked_at now())
// and no audit row (the trail is the same for a new and a known address -- §5.10). Then every
// predicate, each case alone, refuses (false, nothing written):
//
//	the context        app.tenant_id is another business; no context at all
//	the row            another business's id for the row; a row already linked; a disabled row
//	S13                the row inserted under a SAVEPOINT (its xmin is the subtransaction's)
//	the row's birth    created_at before this transaction (a row written at the top level, backdated)
//	the business       born before this transaction; a SECOND membership row (T7, the main guarantee)
//	the address        no address; the address in an identity (its digest left byte-identical); in
//	                   another business's row; in another business's REFUSED row
//
// MUTATIONS, in the test (savepoints): each predicate removed from the body makes its case link --
// the case is about that predicate and nothing else.
func TestIdentityCreateForSignup_BirthsAnIdentityForANewAddressAndNothingElse(t *testing.T) {
	ctx, tx := opTx(t)
	fresh := isNewWorld(t, ctx, tx, "", "")
	oldRow := isNewWorld(t, ctx, tx, "", "86400")
	oldTenant := isNewWorld(t, ctx, tx, "86400", "")
	noMail := isNewWorld(t, ctx, tx, "", "")
	aiExec(t, ctx, tx, `UPDATE admin_users SET email = NULL WHERE id = $1`, noMail.admin)
	// The UPDATE wrote noMail's row in this transaction too; its xmin is still the top-level xid.

	t.Run("a new address: one identity, the row's digest, the link, no audit row", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			audits := opInt(t, ctx, sp, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, fresh.tenant)
			if !isCall(t, ctx, sp, fresh.tenant, fresh.tenant, fresh.admin) {
				t.Fatal("a registration of a new address linked nothing")
			}
			ident, via := isLinked(t, ctx, sp, fresh.admin)
			if ident == uuid.Nil || via != "signup" {
				t.Fatalf("the row is linked to %s via %q, want an identity via signup", ident, via)
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities i JOIN admin_users a ON a.identity_id = i.id
			                            WHERE a.id = $1 AND i.email = $2 AND i.password_hash = $3 AND a.password_hash = i.password_hash
			                              AND i.email_verified_at IS NULL AND i.confirm_scope IS NULL AND a.linked_at = now()`,
				fresh.admin, fresh.email, fresh.digest); n != 1 {
				t.Fatal("the identity is not the row's address and digest, unverified, linked now()")
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE email = $1`, fresh.email); n != 1 {
				t.Fatalf("%d identities for the address, want 1", n)
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, fresh.tenant); n != audits {
				t.Fatalf("the link wrote %d audit row(s); the trail must not tell a new address from a known one", n-audits)
			}
		})
	})

	type refusal struct {
		name   string
		world  isWorld
		setup  func(sp pgx.Tx) // in the case's savepoint, before the call (nil: none)
		guc    uuid.UUID       // the app.tenant_id the call runs under (uuid.Nil: none)
		tenant uuid.UUID       // the p_tenant_id passed
		cut    string          // the predicate a mutation removes ("" = no mutation for this case)
		put    string
	}
	otherTenant := aiTenant(t, ctx, tx)
	cases := []refusal{
		{name: "another business's context", world: fresh, guc: otherTenant, tenant: fresh.tenant,
			cut: "IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN", put: "IF false THEN"},
		{name: "no context at all", world: fresh, guc: uuid.Nil, tenant: fresh.tenant,
			cut: "IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN", put: "IF false THEN"},
		{name: "the row named under another business", world: fresh, guc: otherTenant, tenant: otherTenant},
		{name: "a row already linked", world: fresh, guc: fresh.tenant, tenant: fresh.tenant,
			setup: func(sp pgx.Tx) {
				aiLink(t, ctx, sp, fresh.admin, aiIdentity(t, ctx, sp, aiEmail()))
			}},
		{name: "a disabled row", world: fresh, guc: fresh.tenant, tenant: fresh.tenant,
			setup: func(sp pgx.Tx) {
				aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, fresh.admin)
			}},
		{name: "S13: the row written under a savepoint", guc: uuid.Nil,
			cut: "IF (v_xmin = pg_catalog.pg_current_xact_id()::xid\n        AND", put: "IF (true\n        AND"},
		{name: "a row born before this transaction", world: oldRow, guc: oldRow.tenant, tenant: oldRow.tenant,
			cut: "        AND v_born = pg_catalog.now()\n", put: "\n"},
		{name: "a business born before this transaction", world: oldTenant, guc: oldTenant.tenant, tenant: oldTenant.tenant,
			cut: "        AND v_tenant_born = pg_catalog.now()\n", put: "\n"},
		{name: "T7: a second membership row in the business", world: fresh, guc: fresh.tenant, tenant: fresh.tenant,
			setup: func(sp pgx.Tx) { aiAdmin(t, ctx, sp, fresh.tenant, aiEmail()) },
			cut:   "        AND v_rows = 1\n", put: "\n"},
		{name: "no address", world: noMail, guc: noMail.tenant, tenant: noMail.tenant},
		{name: "the address in an identity", world: fresh, guc: fresh.tenant, tenant: fresh.tenant,
			setup: func(sp pgx.Tx) {
				aiExec(t, ctx, sp, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2)`, strings.ToUpper(fresh.email), opFakeDigest("k"))
			}},
		{name: "the address in another business's row", world: fresh, guc: fresh.tenant, tenant: fresh.tenant,
			setup: func(sp pgx.Tx) { aiAdmin(t, ctx, sp, aiTenant(t, ctx, sp), strings.ToUpper(fresh.email)) },
			// M14-08a (00076): the read is a FOR loop that skips a row whose address is not printable
			// ASCII; its refusal is the IF inside it.
			cut: "IF (r.email::text ~ '^[\\x21-\\x7e]+$') IS NOT FALSE THEN", put: "IF false THEN"},
		{name: "the address in another business's refused row", world: fresh, guc: fresh.tenant, tenant: fresh.tenant,
			setup: func(sp pgx.Tx) {
				aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, aiAdmin(t, ctx, sp, aiTenant(t, ctx, sp), fresh.email))
			}},
	}
	for _, c := range cases {
		run := func(sp pgx.Tx) bool {
			w := c.world
			if c.name == "S13: the row written under a savepoint" {
				w = isNewWorld(t, ctx, sp, "", "")
				c.guc, c.tenant = w.tenant, w.tenant
			}
			if c.setup != nil {
				c.setup(sp)
			}
			before := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE password_hash = $1`, opFakeDigest("k"))
			linked := isCall(t, ctx, sp, c.guc, c.tenant, w.admin)
			if !linked {
				if ident, _ := isLinked(t, ctx, sp, w.admin); ident != uuid.Nil && c.name != "a row already linked" {
					t.Fatalf("refused, yet the row is linked to %s", ident)
				}
				if c.name == "the address in an identity" {
					if after := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE password_hash = $1`, opFakeDigest("k")); after != before {
						t.Fatal("the existing identity's digest changed")
					}
				}
			}
			return linked
		}
		t.Run("refused/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if run(sp) {
					t.Fatal("linked; want refused")
				}
			})
		})
		if c.cut == "" {
			continue
		}
		t.Run("mutation/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				var def string
				if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_create_for_signup(uuid, uuid)'::regprocedure)`).Scan(&def); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(def, c.cut) {
					t.Fatalf("PREMISE: the body lacks %q", c.cut)
				}
				aiExec(t, ctx, sp, strings.Replace(def, c.cut, c.put, 1))
				if !run(sp) {
					t.Fatal("with the predicate removed the case is still refused: it does not test that predicate")
				}
			})
		})
	}
}
