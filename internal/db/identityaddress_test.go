package db

// identityaddress_test.go -- migration 00075 (M14-14, ADR 0031 §5.2's 2026-10-26 note): the identity's
// REGISTERED address reaches tappa_app in exactly two places, resolve_identity_by_email (the reset
// request's recipient) and identity_consume_reset (the notice after a per-identity link was spent).
// The signatures are identitycatalog_test.go's (identitySignaturesM1404 / M1405); the owner, SECURITY
// DEFINER, search_path and EXECUTE pins are the catalogue tests' (privcatalog_test.go,
// m11schema_test.go), which run on every definer.

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const ia00075File = "00075_return_the_identity_address_for_resets.sql"

// TestIdentityAddress00075_BothReturnTheRegisteredSpelling, as tappa_app with no tenant context: the
// identity's address is stored in one spelling, looked up in another; resolve_identity_by_email's
// email is the stored one (not the lookup key), and identity_consume_reset returns the same stored
// spelling with the identity it wrote. MUTATION (measured red on the dev database): either body
// returning its argument / a row's address instead of admin_identities.email.
func TestIdentityAddress00075_BothReturnTheRegisteredSpelling(t *testing.T) {
	ctx, tx := opTx(t)
	w := ipNewResetWorld(t, ctx, tx)
	registered := strings.ToUpper(w.p.email)
	aiExec(t, ctx, tx, `UPDATE admin_identities SET email = $2 WHERE id = $1`, w.p.id, registered)
	typed := strings.ToUpper(w.p.email[:1]) + w.p.email[1:]

	rows, err := ipApp(t, ctx, tx, `SELECT id, email FROM resolve_identity_by_email($1::citext)`, typed)
	if err != nil || len(rows) != 1 || uuid.UUID(rows[0][0].([16]byte)) != w.p.id || rows[0][1] != registered {
		t.Fatalf("resolve_identity_by_email(%q) = %v, %v; want the identity and %q", typed, rows, err, registered)
	}
	rows, err = ipApp(t, ctx, tx, `SELECT identity_id, revoked_sessions, email FROM identity_consume_reset($1, $2)`, w.identityLink, w.newDigest)
	if err != nil || len(rows) != 1 || uuid.UUID(rows[0][0].([16]byte)) != w.p.id || rows[0][2] != registered {
		t.Fatalf("identity_consume_reset = %v, %v; want the identity and %q", rows, err, registered)
	}
	// The old binary's selection (internal/db before 00075) still works on the wider row.
	if n := ipRows(t, ctx, tx, `SELECT id, password_hash, email_verified_at, has_active_membership FROM resolve_identity_by_email($1::citext)`, typed); n != 1 {
		t.Errorf("the pre-00075 selection answers %d rows, want 1 (expand: the old pod reads by name)", n)
	}
}

// TestIdentity00075_DownGivesBackTheOldDefinitionsByteForByte: in the test's transaction (rolled back),
// 00075's Down gives the two functions back exactly as 00071 / 00072 created them -- the body
// (prosrc) equal to the file's text between the dollar quotes, the result, the owner, SECURITY
// DEFINER, the pinned search_path and the ACL equal to what they were before 00075 (recorded from the
// Down itself, which recreates them from the very blocks the two files hold) -- and the Up again gives
// HEAD's definitions and ACLs back byte for byte. The Down locks no table (it is DROP / CREATE /
// GRANT on functions alone).
func TestIdentity00075_DownGivesBackTheOldDefinitionsByteForByte(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ia00075File)
	if tables := downTables(down); len(tables) > 0 {
		t.Fatalf("PREMISE: 00075's Down locks %v", tables)
	}
	type def struct{ src, result, owner, config, acl, full string }
	read := func(q pgx.Tx, fn string) def {
		t.Helper()
		var d def
		if err := q.QueryRow(ctx, `SELECT p.prosrc, pg_get_function_result(p.oid), p.proowner::regrole::text,
		                                  coalesce(array_to_string(p.proconfig, ','), '') || '/' || p.prosecdef::text,
		                                  coalesce(p.proacl::text, ''), pg_get_functiondef(p.oid)
		                             FROM pg_proc p WHERE p.oid = $1::regprocedure`, fn).Scan(&d.src, &d.result, &d.owner, &d.config, &d.acl, &d.full); err != nil {
			t.Fatalf("read %s: %v", fn, err)
		}
		return d
	}
	const resolver, consume = "public.resolve_identity_by_email(citext)", "public.identity_consume_reset(text,text)"
	headR, headC := read(tx, resolver), read(tx, consume)
	if !strings.HasSuffix(headR.result, ", email text)") || !strings.HasSuffix(headC.result, ", email text)") {
		t.Fatalf("PREMISE at HEAD: the results are %q / %q", headR.result, headC.result)
	}

	// The two bodies as their files wrote them.
	body := func(file, create string) string {
		t.Helper()
		u, _ := opMigrationSections(t, file)
		i := strings.Index(u, create)
		if i < 0 {
			t.Fatalf("%s no longer creates %q", file, create)
		}
		rest := u[i:]
		a := strings.Index(rest, "AS $$\n")
		b := strings.Index(rest, "\n$$;")
		if a < 0 || b < a {
			t.Fatalf("%s: no dollar-quoted body after %q", file, create)
		}
		// The Down must hold the very same CREATE block, byte for byte.
		block := rest[:b+len("\n$$;")]
		if !strings.Contains(down, block) {
			t.Errorf("00075's Down does not hold %s's %q block verbatim", file, create)
		}
		return rest[a+len("AS $$") : b+1]
	}
	wantR := body("00071_resolve_identities_and_switch_sessions.sql", "CREATE FUNCTION public.resolve_identity_by_email(")
	wantC := body("00072_set_identity_passwords_and_link_legacy_rows.sql", "CREATE FUNCTION public.identity_consume_reset(")

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00075 Down")
		r, c := read(sp, resolver), read(sp, consume)
		if r.src != wantR || c.src != wantC {
			t.Errorf("after the Down the bodies are not 00071's / 00072's byte for byte (resolver %v, consume %v)", r.src == wantR, c.src == wantC)
		}
		if r.result != "TABLE(id uuid, password_hash text, email_verified_at timestamp with time zone, has_active_membership boolean)" ||
			c.result != "TABLE(identity_id uuid, revoked_sessions integer)" {
			t.Errorf("after the Down the results are %q / %q", r.result, c.result)
		}
		for _, x := range []struct {
			what      string
			got, head def
			wantOwner string
		}{{"resolve_identity_by_email", r, headR, "tappa_resolver"}, {"identity_consume_reset", c, headC, "tappa_identitydefiner"}} {
			if x.got.owner != x.wantOwner || x.got.config != "search_path=pg_catalog, pg_temp/true" || x.got.acl != x.head.acl {
				t.Errorf("%s after the Down: owner %s, config %q, acl %q; want %s, the pinned definer, HEAD's acl %q",
					x.what, x.got.owner, x.got.config, x.got.acl, x.wantOwner, x.head.acl)
			}
		}
		opRunSection(t, ctx, sp, up, "00075 Up again")
		if r2, c2 := read(sp, resolver), read(sp, consume); r2 != headR || c2 != headC {
			t.Error("the Up again does not give HEAD's definitions and ACLs back byte for byte")
		}
	})
}
