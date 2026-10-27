package db

// adminowners_test.go -- migration 00081 (M14-16b; ADR 0031 §5.2 and §5.12, the notes of 2026-10-27):
//
//   - resolve_admin_owners_by_tenant, tappa_resolver's read-only answer to "which of this business's
//     owners can still get a session" -- the panel's last-owner rule counts these (the ghost owner,
//     backlog T171). The signature, owner, SECURITY DEFINER, search_path and EXECUTE pins are the
//     catalogue tests' (identitycatalog_test.go's identitySignaturesM1416b, privcatalog_test.go,
//     m11schema_test.go); its body is held to the belt (identitybelt_test.go);
//   - the two redefined session functions: their Down gives 00071's bodies back byte for byte. What the
//     new row lock does is internal/adminauth's TestMemberships_ASessionOpenedBesideADisableDoesNotOutliveIt
//     (it needs two committed transactions).
//
// Everything here runs in the owner's transaction (opTx), rolled back; the calls run as tappa_app.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const ao00081File = "00081_count_the_owners_who_can_sign_in.sql"

// aoOwners calls the resolver as tappa_app with the transaction's tenant context set to ctxTenant
// (uuid.Nil: no context) and returns the ids, sorted.
func aoOwners(t *testing.T, ctx context.Context, tx pgx.Tx, ctxTenant, asked uuid.UUID) []uuid.UUID {
	t.Helper()
	var out []uuid.UUID
	if err := aiApp(t, ctx, tx, ctxTenant, func(app pgx.Tx) error {
		rows, err := app.Query(ctx, adminOwnersByTenantSQL, asked)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatalf("tappa_app: resolve_admin_owners_by_tenant: %v", err)
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}

func aoSorted(ids ...uuid.UUID) []uuid.UUID {
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return ids
}

// TestAdminOwners00081_OnlyTheOwnersWhoCanSignIn: business A holds an active owner, a second active
// owner the person REFUSED (unlinked_at stamped, still 'active' -- before contract B's unlink), a
// disabled owner and an active manager. In A's own context the resolver answers the two that can get a
// session -- the real owner and nobody else; a refused owner is absent, not marked. MUTATIONS (measured,
// each red here): the body without `unlinked_at IS NULL` (the refused owner comes back), without
// `status = 'active'` (the disabled one), without `role = 'owner'` (the manager).
func TestAdminOwners00081_OnlyTheOwnersWhoCanSignIn(t *testing.T) {
	ctx, tx := opTx(t)
	a := aiTenant(t, ctx, tx)
	owner, owner2 := aiAdmin(t, ctx, tx, a, aiEmail()), aiAdmin(t, ctx, tx, a, aiEmail())
	refused, disabled, manager := aiAdmin(t, ctx, tx, a, aiEmail()), aiAdmin(t, ctx, tx, a, aiEmail()), aiAdmin(t, ctx, tx, a, aiEmail())
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, refused)
	aiExec(t, ctx, tx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, disabled)
	aiExec(t, ctx, tx, `UPDATE admin_users SET role = 'manager' WHERE id = $1`, manager)

	got := aoOwners(t, ctx, tx, a, a)
	if want := aoSorted(owner, owner2); !slices.Equal(got, want) {
		t.Fatalf("the owners who can sign in = %v, want exactly the two real owners %v (refused %s, disabled %s, manager %s)",
			got, want, refused, disabled, manager)
	}
	// The same rows as the panel sees them (RLS, A's context): five memberships, four of them owners.
	var rows int
	if err := aiApp(t, ctx, tx, a, func(app pgx.Tx) error {
		return app.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE role = 'owner' AND status = 'active'`).Scan(&rows)
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 3 {
		t.Fatalf("CONTROL: tappa_app sees %d active owner rows in A, want 3 (two real, one refused)", rows)
	}
}

// TestAdminOwners00081_AnswersOnlyForTheCallersOwnTenant is the isolation test: business B's owner is
// never returned to a transaction whose context is A -- asking for B by id from A's context, asking with
// no context at all, asking for A with no context, and asking for NULL are 0 rows; only B's own context
// answers B. MUTATION (measured): the body without its context check -> A's context reads B's owner,
// red. (No tenant filter in the test's reads: the function's own check is what is measured.)
func TestAdminOwners00081_AnswersOnlyForTheCallersOwnTenant(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	ownerA, ownerB := aiAdmin(t, ctx, tx, a, aiEmail()), aiAdmin(t, ctx, tx, b, aiEmail())

	if got := aoOwners(t, ctx, tx, b, b); !slices.Equal(got, []uuid.UUID{ownerB}) {
		t.Fatalf("CONTROL: B's own context = %v, want B's owner", got)
	}
	for _, c := range []struct {
		name           string
		context, asked uuid.UUID
	}{
		{"A's context asking for B", a, b},
		{"no context asking for B", uuid.Nil, b},
		{"no context asking for A", uuid.Nil, a},
		{"A's context asking for nothing", a, uuid.Nil},
	} {
		asked := any(c.asked)
		if c.asked == uuid.Nil {
			asked = nil
		}
		var got []uuid.UUID
		if err := aiApp(t, ctx, tx, c.context, func(app pgx.Tx) error {
			rows, err := app.Query(ctx, adminOwnersByTenantSQL, asked)
			if err != nil {
				return err
			}
			got, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			return err
		}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(got) != 0 {
			t.Errorf("%s = %v, want no rows (A's owner %s, B's owner %s)", c.name, got, ownerA, ownerB)
		}
	}
}

// TestIdentity00081_DownGivesBack00071ByteForByte: in the test's transaction (rolled back), 00081's Down
// gives identity_open_session and identity_switch_session back exactly as 00071 created them -- the body
// (prosrc) equal to 00071's text between the dollar quotes, and the owner, SECURITY DEFINER, the pinned
// search_path and the ACL equal to HEAD's -- drops the resolver and takes tappa_resolver's admin_users
// role back; the Up again gives HEAD's definitions, ACLs and grant back byte for byte. HEAD's bodies
// carry the lock (four FOR NO KEY UPDATE in all) and 00071's none. The Down locks no table.
func TestIdentity00081_DownGivesBack00071ByteForByte(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ao00081File)
	if tables := downTables(down); len(tables) > 0 {
		t.Fatalf("PREMISE: 00081's Down locks %v", tables)
	}
	type def struct{ src, owner, config, acl, full string }
	read := func(q pgx.Tx, fn string) def {
		t.Helper()
		var d def
		if err := q.QueryRow(ctx, `SELECT p.prosrc, p.proowner::regrole::text,
		                                  coalesce(array_to_string(p.proconfig, ','), '') || '/' || p.prosecdef::text,
		                                  coalesce(p.proacl::text, ''), pg_get_functiondef(p.oid)
		                             FROM pg_proc p WHERE p.oid = $1::regprocedure`, fn).Scan(&d.src, &d.owner, &d.config, &d.acl, &d.full); err != nil {
			t.Fatalf("read %s: %v", fn, err)
		}
		return d
	}
	const open, sw, resolver = "public.identity_open_session(uuid,uuid,text,text)", "public.identity_switch_session(text,uuid,text)",
		"public.resolve_admin_owners_by_tenant(uuid)"
	headO, headS, headR := read(tx, open), read(tx, sw), read(tx, resolver)
	locks := func(src string) int {
		return strings.Count(identityLineComment.ReplaceAllString(src, ""), "FOR NO KEY UPDATE")
	}
	if n := locks(headO.src) + locks(headS.src); n != 4 {
		t.Fatalf("PREMISE at HEAD: %d FOR NO KEY UPDATE in the two bodies, want 4 (three membership reads, one target read)", n)
	}
	roleGrant := func(q pgx.Tx) bool {
		var ok bool
		if err := q.QueryRow(ctx, `SELECT has_column_privilege('tappa_resolver', 'public.admin_users', 'role', 'SELECT')`).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !roleGrant(tx) {
		t.Fatal("PREMISE at HEAD: tappa_resolver does not read admin_users.role")
	}

	u71, _ := opMigrationSections(t, "00071_resolve_identities_and_switch_sessions.sql")
	body := func(name string) string {
		t.Helper()
		i := strings.Index(u71, "CREATE FUNCTION public."+name+"(")
		if i < 0 {
			t.Fatalf("00071 no longer creates %s", name)
		}
		rest := u71[i:]
		a, b := strings.Index(rest, "AS $$\n"), strings.Index(rest, "\n$$;")
		if a < 0 || b < a {
			t.Fatalf("00071: no dollar-quoted body for %s", name)
		}
		// The Down holds the very same block, byte for byte, as CREATE OR REPLACE.
		if block := "CREATE OR REPLACE FUNCTION public." + strings.TrimPrefix(rest[:b+len("\n$$;")], "CREATE FUNCTION public."); !strings.Contains(down, block) {
			t.Errorf("00081's Down does not hold 00071's %s block verbatim", name)
		}
		return rest[a+len("AS $$") : b+1]
	}
	wantO, wantS := body("identity_open_session"), body("identity_switch_session")
	if locks(wantO)+locks(wantS) != 0 {
		t.Fatal("PREMISE: 00071's bodies already lock")
	}

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00081 Down")
		o, s := read(sp, open), read(sp, sw)
		if o.src != wantO || s.src != wantS {
			t.Errorf("after the Down the bodies are not 00071's byte for byte (open %v, switch %v)", o.src == wantO, s.src == wantS)
		}
		for _, x := range []struct {
			what      string
			got, head def
		}{{"identity_open_session", o, headO}, {"identity_switch_session", s, headS}} {
			if x.got.owner != "tappa_identitydefiner" || x.got.config != "search_path=pg_catalog, pg_temp/true" || x.got.acl != x.head.acl {
				t.Errorf("%s after the Down: owner %s, config %q, acl %q; want tappa_identitydefiner, the pinned definer, HEAD's acl %q",
					x.what, x.got.owner, x.got.config, x.got.acl, x.head.acl)
			}
		}
		var gone bool
		if err := sp.QueryRow(ctx, `SELECT to_regprocedure($1) IS NULL`, resolver).Scan(&gone); err != nil || !gone {
			t.Errorf("after the Down the resolver is still there (%v)", err)
		}
		if roleGrant(sp) {
			t.Error("after the Down tappa_resolver still reads admin_users.role")
		}
		opRunSection(t, ctx, sp, up, "00081 Up again")
		if o2, s2, r2 := read(sp, open), read(sp, sw), read(sp, resolver); o2 != headO || s2 != headS || r2 != headR {
			t.Error("the Up again does not give HEAD's definitions and ACLs back byte for byte")
		}
		if !roleGrant(sp) {
			t.Error("the Up again does not give tappa_resolver admin_users.role back")
		}
	})
}
