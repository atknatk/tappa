package db

// identitylinkemail_test.go -- M14-22 (migration 00084; ADR 0031 §5.2's and §5.7's notes of 2026-10-29)
// against real Postgres, owner session, everything rolled back (opTx): identity_link_by_email's
// refusals and guards, each guard with its MUTATION in a savepoint (the body rewritten without it ->
// the case it holds turns), the request's purpose and the old binary's two-argument call, and the Down.
// The behaviour end to end (1, 2 and 10 businesses, concurrency, lock order) is
// internal/adminauth/identitylinkemail_db_test.go.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const ileFile = "00084_link_businesses_by_email.sql"

// identityExerciseM1422 builds M14-22's world (an identity with a session in I and an unlinked row of its
// address in J) and returns its calls: a link link requested (the purpose's INSERT) and spent (the
// purpose's SELECT, the link) -- every 00084 grant on a path that runs.
func identityExerciseM1422(t *testing.T, ctx context.Context, sp pgx.Tx) []identityStep {
	t.Helper()
	i, j := idTenant(t, ctx, sp, "exercise I"), idTenant(t, ctx, sp, "exercise J")
	email := aiEmail()
	ident := aiIdentity(t, ctx, sp, email)
	pi := aiAdmin(t, ctx, sp, i, email)
	aiAdmin(t, ctx, sp, j, email)
	aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now(),
	                           password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1`, pi, ident)
	si := idSession(t, ctx, sp, i, pi, nil)
	h := opRandHex(t)
	return []identityStep{
		{name: "M14-22 request link", sql: requestIdentityLinkSQL, args: []any{si, h}},
		{name: "M14-22 link by e-mail", sql: linkByEmailSQL + ` WHERE linked = 1`, args: []any{si, h}},
	}
}

// ileWorld is one person for these tests: an identity, its linked row and live session in A, and the
// businesses of its address the test adds.
type ileWorld struct {
	email   string
	ident   uuid.UUID
	a       uuid.UUID
	session string
}

func newILEWorld(t *testing.T, ctx context.Context, tx pgx.Tx) ileWorld {
	t.Helper()
	w := ileWorld{email: aiEmail()}
	w.ident = aiIdentity(t, ctx, tx, w.email)
	w.a = idTenant(t, ctx, tx, "M14-22 A")
	pa := aiAdmin(t, ctx, tx, w.a, w.email)
	aiExec(t, ctx, tx, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now(),
	                           password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1`, pa, w.ident)
	w.session = idSession(t, ctx, tx, w.a, pa, nil)
	return w
}

// candidate adds a business with an active, unlinked row of address (its own digest).
func (w ileWorld) candidate(t *testing.T, ctx context.Context, tx pgx.Tx, address string) (tenant, row uuid.UUID) {
	t.Helper()
	tenant = idTenant(t, ctx, tx, "M14-22 candidate")
	row = uuid.New()
	aiExec(t, ctx, tx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
	                   VALUES ($1, $2, 'M14-22 Row', $3, $4, 'owner')`, row, tenant, address, opFakeDigest("z"))
	return tenant, row
}

// request writes a link of purpose for w's session, as tappa_app; it returns the link's hash.
func (w ileWorld) request(t *testing.T, ctx context.Context, tx pgx.Tx, purpose string) string {
	t.Helper()
	h := opRandHex(t)
	if err := opAs(t, ctx, tx, "tappa_app", func(app pgx.Tx) error {
		var id uuid.UUID
		return app.QueryRow(ctx, `SELECT verification_id FROM identity_request_verification($1, $2, $3)`, w.session, h, purpose).Scan(&id)
	}); err != nil {
		t.Fatalf("request a %s link: %v", purpose, err)
	}
	return h
}

// link calls identity_link_by_email as tappa_app: (linked, true) or (0, false) for 0 rows.
func ileLink(t *testing.T, ctx context.Context, tx pgx.Tx, session, hash string) (int, bool) {
	t.Helper()
	var (
		n     int32
		first bool
		email string
		ok    bool
	)
	if err := opAs(t, ctx, tx, "tappa_app", func(app pgx.Tx) error {
		err := app.QueryRow(ctx, linkByEmailSQL, session, hash).Scan(&n, &first, &email)
		if err == pgx.ErrNoRows {
			return nil
		}
		ok = err == nil
		return err
	}); err != nil {
		t.Fatalf("identity_link_by_email: %v", err)
	}
	return int(n), ok
}

func ileLinkedTo(t *testing.T, ctx context.Context, tx pgx.Tx, row uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	var (
		id  *uuid.UUID
		via *string
	)
	if err := tx.QueryRow(ctx, `SELECT identity_id, linked_via FROM admin_users WHERE id = $1`, row).Scan(&id, &via); err != nil {
		t.Fatal(err)
	}
	if id == nil {
		return uuid.Nil, ""
	}
	return *id, *via
}

// ileRewrite replaces cut with put in identity_link_by_email's live body (in sp, rolled back after).
func ileRewrite(t *testing.T, ctx context.Context, sp pgx.Tx, cut, put string) {
	t.Helper()
	var def string
	if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_link_by_email(text,text)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if strings.Count(def, cut) == 0 {
		t.Fatalf("PREMISE: the body lacks %q", cut)
	}
	aiExec(t, ctx, sp, strings.ReplaceAll(def, cut, put))
}

// TestIdentityLinkByEmail00084_EveryGuardHoldsItsCase is criteria 3, 9, 10 at the function, each guard
// with its mutation: for each case the link is refused or the row is left alone; with the guard cut out
// of the body (in a savepoint) the SAME case links -- the guard is what held it.
//
//	a homoglyph row (U+0130)          6b                          -> never linked
//	a verification link               purpose = 'link'            -> refused
//	another identity's session        a.identity_id = v_identity  -> refused
//	a refused row                     unlinked_at IS NULL (both)  -> never linked
//	a disabled row                    status = 'active'           -> never linked
//	an expired link                   the expiry under the lock   -> refused
func TestIdentityLinkByEmail00084_EveryGuardHoldsItsCase(t *testing.T) {
	ctx, tx := opTx(t)
	rule := func(sp pgx.Tx) string {
		var def string
		if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_link_by_email(text,text)'::regprocedure)`).Scan(&def); err != nil {
			t.Fatal(err)
		}
		i := strings.Index(def, "IF (pg_catalog.octet_length(r.email::text) <= 254")
		j := strings.Index(def[i:], "END IF;")
		if i < 0 || j < 0 {
			t.Fatal("PREMISE: the body carries no 6b block")
		}
		return def[i : i+j+len("END IF;")]
	}
	cases := []struct {
		name string
		// set up the case: the session and the hash to press with, and the row that must stay unlinked
		// (uuid.Nil: the press itself must be refused).
		setup    func(sp pgx.Tx) (session, hash string, row uuid.UUID)
		cut, put string // the guard, and what the mutation writes in its place ("" put: rule-derived)
	}{
		{name: "a homoglyph row", setup: func(sp pgx.Tx) (string, string, uuid.UUID) {
			w := newILEWorld(t, ctx, sp)
			_, row := w.candidate(t, ctx, sp, strings.Replace(w.email, "i", "İ", 1))
			return w.session, w.request(t, ctx, sp, "link"), row
		}},
		{name: "a verification link", setup: func(sp pgx.Tx) (string, string, uuid.UUID) {
			w := newILEWorld(t, ctx, sp)
			w.candidate(t, ctx, sp, w.email)
			return w.session, w.request(t, ctx, sp, "verify"), uuid.Nil
		}, cut: "AND v.purpose = 'link'", put: ""},
		{name: "another identity's session", setup: func(sp pgx.Tx) (string, string, uuid.UUID) {
			w, other := newILEWorld(t, ctx, sp), newILEWorld(t, ctx, sp)
			w.candidate(t, ctx, sp, w.email)
			return other.session, w.request(t, ctx, sp, "link"), uuid.Nil
		}, cut: "AND a.identity_id = v_identity", put: "AND a.identity_id IS NOT NULL"},
		{name: "a refused row", setup: func(sp pgx.Tx) (string, string, uuid.UUID) {
			w := newILEWorld(t, ctx, sp)
			_, row := w.candidate(t, ctx, sp, w.email)
			aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, row)
			return w.session, w.request(t, ctx, sp, "link"), row
		}, cut: "AND a.unlinked_at IS NULL", put: "AND a.id IS NOT NULL"}, // the candidates' AND the link's
		{name: "a disabled row", setup: func(sp pgx.Tx) (string, string, uuid.UUID) {
			w := newILEWorld(t, ctx, sp)
			_, row := w.candidate(t, ctx, sp, w.email)
			aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, row)
			return w.session, w.request(t, ctx, sp, "link"), row
		}, cut: "           AND a.status = 'active'\n         ORDER BY", put: "         ORDER BY"},
		{name: "an expired link", setup: func(sp pgx.Tx) (string, string, uuid.UUID) {
			w := newILEWorld(t, ctx, sp)
			w.candidate(t, ctx, sp, w.email)
			h := w.request(t, ctx, sp, "link")
			aiExec(t, ctx, sp, `UPDATE admin_identity_verifications SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE token_hash = $1`, h)
			return w.session, h, uuid.Nil
		}, cut: "IF (v_found AND v_expires > v_now) IS NOT TRUE THEN", put: "IF (v_found) IS NOT TRUE THEN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				session, hash, row := c.setup(sp)
				n, ok := ileLink(t, ctx, sp, session, hash)
				if row == uuid.Nil && ok {
					t.Fatalf("the press was accepted (%d linked), want 0 rows", n)
				}
				if row != uuid.Nil {
					if id, _ := ileLinkedTo(t, ctx, sp, row); id != uuid.Nil {
						t.Fatal("the row was linked")
					}
				}
			})
			t.Run("mutation", func(t *testing.T) {
				aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
					cut, put := c.cut, c.put
					if cut == "" { // 6b: the whole refusal out
						cut, put = rule(sp), ""
					}
					if c.name == "an expired link" {
						// The lock-free read refuses an expired link first; the mutation cuts both checks.
						ileRewrite(t, ctx, sp, "       AND v.expires_at > v_now;", ";")
					}
					ileRewrite(t, ctx, sp, cut, put)
					session, hash, row := c.setup(sp)
					n, ok := ileLink(t, ctx, sp, session, hash)
					if row == uuid.Nil && (!ok || n == 0) {
						t.Fatalf("with the guard cut the press is still refused (%d, %v): the case does not test it", n, ok)
					}
					if row != uuid.Nil {
						if id, _ := ileLinkedTo(t, ctx, sp, row); id == uuid.Nil {
							t.Fatal("with the guard cut the row is still left alone: the case does not test it")
						}
					}
				})
			})
		})
	}
}

// TestIdentityLinkByEmail00084_ABusinessWithAMembershipIsLeftAlone (criterion 3): the identity already
// has a membership in a business (another row of another spelling would breach UNIQUE (tenant_id,
// identity_id)); the press links the rest and leaves that business as it was -- no 500, no second
// membership. MUTATION: the per-business test cut -> the press is the one refusal (the unique_violation
// handler: nothing stays), red.
func TestIdentityLinkByEmail00084_ABusinessWithAMembershipIsLeftAlone(t *testing.T) {
	ctx, tx := opTx(t)
	build := func(sp pgx.Tx) (ileWorld, string, uuid.UUID, uuid.UUID) {
		w := newILEWorld(t, ctx, sp)
		// Business M: the identity's membership under ANOTHER address (an invitation's), and an unlinked
		// row of the identity's address -- the shape only the defence handles.
		m := idTenant(t, ctx, sp, "M14-22 M")
		member := aiAdmin(t, ctx, sp, m, aiEmail())
		aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'invite', linked_at = now(),
		                           password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1`, member, w.ident)
		row := uuid.New()
		aiExec(t, ctx, sp, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
		                   VALUES ($1, $2, 'M14-22 Row', $3, $4, 'owner')`, row, m, w.email, opFakeDigest("z"))
		_, other := w.candidate(t, ctx, sp, w.email)
		return w, w.request(t, ctx, sp, "link"), row, other
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		w, h, row, other := build(sp)
		n, ok := ileLink(t, ctx, sp, w.session, h)
		if !ok || n != 1 {
			t.Fatalf("the press: %d, %v; want the other business alone linked", n, ok)
		}
		if id, _ := ileLinkedTo(t, ctx, sp, row); id != uuid.Nil {
			t.Error("the row in a business where the identity already is was linked")
		}
		if id, via := ileLinkedTo(t, ctx, sp, other); id != w.ident || via != "email" {
			t.Errorf("the other business: %s/%q", id, via)
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		ileRewrite(t, ctx, sp, "        IF v_member THEN\n            CONTINUE;\n        END IF;\n", "")
		w, h, _, _ := build(sp)
		if n, ok := ileLink(t, ctx, sp, w.session, h); ok {
			t.Fatalf("MUTATION: with the per-business test cut the press still linked %d", n)
		}
	})
}

// TestIdentityRequestVerification00084_ThePurposeAndTheOldBinarysCall (criteria 2, 11): the binary
// before 00084 calls identity_request_verification with TWO arguments -- it resolves to the new function
// through the DEFAULT and keeps M14-18's rule (a 'verify' row for an unverified identity, nothing for a
// verified one); 'link' is issued for a verified identity too; any other purpose is refused.
func TestIdentityRequestVerification00084_ThePurposeAndTheOldBinarysCall(t *testing.T) {
	ctx, tx := opTx(t)
	w := newILEWorld(t, ctx, tx)
	call := func(sql string, args ...any) (uuid.UUID, bool) {
		var id uuid.UUID
		found := false
		if err := opAs(t, ctx, tx, "tappa_app", func(app pgx.Tx) error {
			err := app.QueryRow(ctx, sql, args...).Scan(&id)
			if err == pgx.ErrNoRows {
				return nil
			}
			found = err == nil
			return err
		}); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return id, found
	}
	purpose := func(id uuid.UUID) string {
		var p string
		if err := tx.QueryRow(ctx, `SELECT purpose FROM admin_identity_verifications WHERE id = $1`, id).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := `SELECT verification_id FROM identity_request_verification($1, $2)`
	if id, ok := call(old, w.session, opRandHex(t)); !ok || purpose(id) != "verify" {
		t.Fatalf("the old binary's call on an unverified identity: %v; want a 'verify' row", ok)
	}
	if _, ok := call(`SELECT verification_id FROM identity_request_verification($1, $2, 'other')`, w.session, opRandHex(t)); ok {
		t.Error("an unknown purpose wrote a row")
	}
	aiExec(t, ctx, tx, `UPDATE admin_identities SET email_verified_at = now() WHERE id = $1`, w.ident)
	if _, ok := call(old, w.session, opRandHex(t)); ok {
		t.Error("the old binary's call on a verified identity wrote a row (M14-18's rule)")
	}
	if id, ok := call(`SELECT verification_id FROM identity_request_verification($1, $2, 'link')`, w.session, opRandHex(t)); !ok || purpose(id) != "link" {
		t.Errorf("a link for a verified identity: %v; want a 'link' row", ok)
	}
}

// TestIdentity00084_DownGivesBack00072sRequestByteForByte: the Down puts 00072's
// identity_request_verification back (body, result, owner, SECURITY DEFINER, search_path, ACL as
// before 00084: the ACL is HEAD's minus nothing), drops identity_link_by_email, the purpose column and
// linked_via's 'email'; the Up again gives HEAD back byte for byte. And it REFUSES while a business is
// linked by e-mail.
func TestIdentity00084_DownGivesBack00072sRequestByteForByte(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ileFile)
	type def struct{ sig, src, result, owner, config, acl string }
	read := func(q pgx.Tx) def {
		t.Helper()
		var d def
		if err := q.QueryRow(ctx, `SELECT p.oid::regprocedure::text, p.prosrc, pg_get_function_result(p.oid), p.proowner::regrole::text,
		                                  coalesce(array_to_string(p.proconfig, ','), '') || '/' || p.prosecdef::text, coalesce(p.proacl::text, '')
		                             FROM pg_proc p WHERE p.proname = 'identity_request_verification'
		                              AND p.pronamespace = 'public'::regnamespace`).Scan(&d.sig, &d.src, &d.result, &d.owner, &d.config, &d.acl); err != nil {
			t.Fatalf("read identity_request_verification (exactly one): %v", err)
		}
		return d
	}
	head := read(tx)
	if head.sig != "identity_request_verification(text,text,text)" {
		t.Fatalf("PREMISE at HEAD: %s", head.sig)
	}
	up72, _ := opMigrationSections(t, "00072_set_identity_passwords_and_link_legacy_rows.sql")
	i := strings.Index(up72, "CREATE FUNCTION public.identity_request_verification(")
	rest := up72[i:]
	block := rest[:strings.Index(rest, "\n$$;")+len("\n$$;")]
	if !strings.Contains(down, block) {
		t.Fatal("00084's Down does not hold 00072's CREATE block verbatim")
	}
	want72 := rest[strings.Index(rest, "AS $$")+len("AS $$") : strings.Index(rest, "\n$$;")+1]

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		lockForDowns(t, ctx, sp, downTables(down))
		opRunSection(t, ctx, sp, down, "00084 Down")
		d := read(sp)
		if d.sig != "identity_request_verification(text,text)" || d.src != want72 {
			t.Errorf("after the Down: %s, body 00072's %v", d.sig, d.src == want72)
		}
		if d.owner != "tappa_identitydefiner" || d.config != "search_path=pg_catalog, pg_temp/true" || d.acl != head.acl {
			t.Errorf("after the Down: owner %s, config %q, acl %q; want the definer, the pinned search_path, HEAD's acl %q", d.owner, d.config, d.acl, head.acl)
		}
		var gone bool
		if err := sp.QueryRow(ctx, `SELECT to_regprocedure('public.identity_link_by_email(text,text)') IS NULL
		                              AND NOT EXISTS (SELECT 1 FROM pg_attribute WHERE attrelid = 'public.admin_identity_verifications'::regclass
		                                               AND attname = 'purpose' AND NOT attisdropped)`).Scan(&gone); err != nil || !gone {
			t.Errorf("after the Down: the link function or the purpose column is still there (%v)", err)
		}
		opRunSection(t, ctx, sp, up, "00084 Up again")
		if again := read(sp); again != head {
			t.Error("the Up again does not give HEAD's definition and ACL back byte for byte")
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		w := newILEWorld(t, ctx, sp)
		w.candidate(t, ctx, sp, w.email)
		if n, ok := ileLink(t, ctx, sp, w.session, w.request(t, ctx, sp, "link")); !ok || n != 1 {
			t.Fatalf("PREMISE: the link: %d, %v", n, ok)
		}
		lockForDowns(t, ctx, sp, downTables(down))
		inner, err := sp.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = inner.Conn().PgConn().Exec(ctx, down).ReadAll()
		if err == nil || !strings.Contains(err.Error(), "linked by e-mail") {
			t.Errorf("the Down with a business linked by e-mail: %v; want the refusal", err)
		}
		if err := inner.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
