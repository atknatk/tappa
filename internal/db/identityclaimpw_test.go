package db

// identityclaimpw_test.go -- M14-17b (ADR 0031 §5.10, §5.2; migration 00080, the user's decision of
// 2026-10-27): an identity whose memberships are all inactive claims a registration with its PASSWORD.
// identity_claim_signup's fourth argument is the digest the application compared the re-entered password
// with; the definer claims only while it is still the identity's digest, under the person's lock, and only
// while the identity has no active membership. Called AS tappa_app through the very SQL this package
// sends, in opTx's transaction (rolled back), the businesses written at the top level as in
// identityclaim_test.go. internal/domain/signup drives the real Provision (the reset racing the claim,
// the address lock, concurrent claims), internal/handler the HTTP flow.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// icpwCall calls identity_claim_signup AS tappa_app under guc with a verified digest (and a session when
// set) and returns the owner row, uuid.Nil when refused.
func icpwCall(t *testing.T, ctx context.Context, q pgx.Tx, guc uuid.UUID, claim string, tenant uuid.UUID, session *string, digest string) uuid.UUID {
	t.Helper()
	var admin uuid.UUID
	err := aiApp(t, ctx, q, guc, func(sp pgx.Tx) error {
		var e error
		admin, e = ClaimSignup(ctx, sp, claim, tenant, session, NewPasswordHash(digest))
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil
	}
	if err != nil {
		t.Fatalf("identity_claim_signup: %v", err)
	}
	return admin
}

// icpwPerson is an identity with ONE membership, disabled: it cannot sign in (no active membership).
type icpwPerson struct {
	identity, tenant, admin uuid.UUID
	email, digest           string
}

func icpwNewPerson(t *testing.T, ctx context.Context, q pgx.Tx) icpwPerson {
	t.Helper()
	p := icpwPerson{email: aiEmail(), tenant: aiTenant(t, ctx, q), digest: opFakeDigest("a")}
	p.identity = aiIdentity(t, ctx, q, p.email)
	p.admin = aiAdmin(t, ctx, q, p.tenant, p.email)
	aiLink(t, ctx, q, p.admin, p.identity)
	aiExec(t, ctx, q, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, p.admin)
	return p
}

// icpwMutate replaces cut by put in identity_claim_signup's body in sp (rolled back with it).
func icpwMutate(t *testing.T, ctx context.Context, sp pgx.Tx, cut, put string) {
	t.Helper()
	var def string
	if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_claim_signup(text, uuid, text, text)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if strings.Count(def, cut) != 1 {
		t.Fatalf("PREMISE: the body holds %q %d times, want once", cut, strings.Count(def, cut))
	}
	aiExec(t, ctx, sp, strings.Replace(def, cut, put, 1))
}

// icpwNothingWritten fails when the refused claim wrote anything: the business has no membership and no
// trail row, the draft is unspent, the identity is as it was.
func icpwNothingWritten(t *testing.T, ctx context.Context, q pgx.Tx, b icBusiness, claim string, identity uuid.UUID) {
	t.Helper()
	if n := opInt(t, ctx, q, `SELECT count(*) FROM admin_users WHERE tenant_id = $1`, b.tenant); n != 0 {
		t.Fatalf("refused, yet %d membership row(s) in the business", n)
	}
	if n := opInt(t, ctx, q, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, b.tenant); n != 0 {
		t.Fatal("refused, yet a trail row was written")
	}
	if n := opInt(t, ctx, q, `SELECT count(*) FROM admin_signup_claims WHERE token_hash = $1 AND used_at IS NULL`, claim); n != 1 {
		t.Fatal("refused, yet the draft is spent")
	}
	if n := opInt(t, ctx, q, `SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at IS NULL AND confirm_scope IS NULL`, identity); n != 1 {
		t.Fatal("refused, yet the identity's mailbox proof or window changed")
	}
	if n := opInt(t, ctx, q, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1`, identity); n != 0 {
		t.Fatal("refused, yet an identity event was written")
	}
}

// TestIdentityClaimSignup00080_ANoMembershipIdentityClaimsWithItsPassword: the draft (its address in
// another spelling -- citext) claimed with the identity's digest -> ONE owner row, linked via `claim` to
// THAT identity, with the identity's REGISTERED address and its digest (the dual-write invariant); no
// second identity; the click is the mailbox proof: email_verified_at, a first_verify window and its
// email_verified event; the draft spent; the business's signup.completed row. An identity already
// verified is left as it was.
func TestIdentityClaimSignup00080_ANoMembershipIdentityClaimsWithItsPassword(t *testing.T) {
	ctx, tx := opTx(t)
	person := icpwNewPerson(t, ctx, tx)
	b := icNewBusiness(t, ctx, tx)
	h := icClaim(t, ctx, tx, strings.ToUpper(person.email), icDraft(b), opFakeDigest("q"), "1 hour")

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		admin := icpwCall(t, ctx, sp, b.tenant, h, b.tenant, nil, person.digest)
		if admin == uuid.Nil {
			t.Fatal("refused with the identity's own digest")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
		                            WHERE a.id = $1 AND i.id = $2 AND a.tenant_id = $3 AND a.role = 'owner' AND a.status = 'active'
		                              AND a.linked_via = 'claim' AND a.linked_at = now() AND a.unlinked_at IS NULL
		                              AND a.email = $4 AND a.password_hash = $5 AND i.password_hash = $5
		                              AND i.email_verified_at = now() AND i.confirm_scope = 'first_verify'
		                              AND i.confirm_opened_at = now() AND i.confirm_open_until = now() + interval '7 days'`,
			admin, person.identity, b.tenant, person.email, person.digest); n != 1 {
			t.Fatal("not linked to the identity with its registered address and digest, or the mailbox proof / window is wrong")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'email_verified'
		                            AND detail = '{"scope": "first_verify"}'`, person.identity); n != 1 {
			t.Fatal("no email_verified event")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE email = $1`, person.email); n != 1 {
			t.Fatal("a second identity was born")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_signup_claims WHERE token_hash = $1 AND used_at = now()`, h); n != 1 {
			t.Fatal("the draft is not spent")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'signup.completed' AND actor_id = $2`,
			b.tenant, admin); n != 1 {
			t.Fatal("no signup.completed row")
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE admin_identities SET email_verified_at = now() - interval '3 days' WHERE id = $1`, person.identity)
		if icpwCall(t, ctx, sp, b.tenant, h, b.tenant, nil, person.digest) == uuid.Nil {
			t.Fatal("a verified identity's claim was refused")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at = now() - interval '3 days'
		                            AND confirm_scope IS NULL`, person.identity); n != 1 {
			t.Fatal("a verified identity's proof or window changed")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1`, person.identity); n != 0 {
			t.Fatal("a verified identity got an event")
		}
	})
}

// TestIdentityClaimSignup00080_EveryGuardRefusesAlone: each case alone is 0 rows with NOTHING written --
// and, as a MUTATION in the test, the guard removed from the body makes exactly that case claim:
//
//	a wrong password             the digest is not the identity's            (the digest predicate)
//	an intervening reset         the identity's digest changed after the app compared  (the same predicate)
//	an active membership         the identity can sign in: its live session is its proof  (the membership test)
//	a session and a digest       two proofs in one call -- a person who can sign in, their own live session
//	                             AND their digest  (the one-proof guard; removed, the session path claims)
//	an address that is not ASCII the draft's address only FOLDS onto the identity's (a Kelvin sign)  (6b)
func TestIdentityClaimSignup00080_EveryGuardRefusesAlone(t *testing.T) {
	ctx, tx := opTx(t)
	person := icpwNewPerson(t, ctx, tx)
	stranger := icNewPerson(t, ctx, tx)
	b := icNewBusiness(t, ctx, tx)
	h := icClaim(t, ctx, tx, person.email, icDraft(b), opFakeDigest("q"), "1 hour")
	// Every business at the top level, before the first savepoint: a pgx savepoint rolled back stays
	// established, so a business written after one carries a subtransaction's xmin (S13 -- refused).
	both := icNewBusiness(t, ctx, tx)
	bothClaim := icClaim(t, ctx, tx, stranger.email, icDraft(both), opFakeDigest("q"), "1 hour")
	other := icNewBusiness(t, ctx, tx)
	otherClaim := icClaim(t, ctx, tx, stranger.email, icDraft(other), opFakeDigest("q"), "1 hour")
	fresh := icNewBusiness(t, ctx, tx)
	freshClaim := icClaim(t, ctx, tx, aiEmail(), icDraft(fresh), opFakeDigest("q"), "1 hour")
	const (
		digestCut = "           AND i.password_hash = p_verified_digest\n"
		memberCut = "        IF FOUND THEN\n            RETURN;\n        END IF;\n        -- The click is the mailbox proof here too"
		bothCut   = "    IF p_session_hash IS NOT NULL AND p_verified_digest IS NOT NULL THEN"
		asciiCut  = "    IF (pg_catalog.octet_length(v_email::text) <= 254"
	)
	cases := []struct {
		name     string
		setup    func(sp pgx.Tx)
		session  *string
		digest   string
		cut, put string
	}{
		{name: "a wrong password", digest: opFakeDigest("w"), cut: digestCut, put: "\n"},
		{name: "an intervening reset",
			setup: func(sp pgx.Tx) {
				aiExec(t, ctx, sp, `UPDATE admin_identities SET password_hash = $2 WHERE id = $1`, person.identity, opFakeDigest("r"))
			},
			digest: person.digest, cut: digestCut, put: "\n"},
		{name: "an active membership",
			setup: func(sp pgx.Tx) {
				aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'active' WHERE id = $1`, person.admin)
			},
			digest: person.digest, cut: memberCut, put: "        -- The click is the mailbox proof here too"},
		{name: "an active membership in another business",
			setup: func(sp pgx.Tx) {
				other := aiAdmin(t, ctx, sp, aiTenant(t, ctx, sp), person.email)
				aiLink(t, ctx, sp, other, person.identity)
			},
			digest: person.digest, cut: memberCut, put: "        -- The click is the mailbox proof here too"},
		{name: "an address that is not ASCII",
			setup: func(sp pgx.Tx) {
				// citext folds U+212A KELVIN SIGN onto 'k' (admininvite_test.go's premise): the draft's
				// address equals the identity's under OPERATOR(public.=) and is not ASCII.
				aiExec(t, ctx, sp, `UPDATE admin_identities SET email = $2 WHERE id = $1`, person.identity, "m14-17b-k-"+person.identity.String()+"@identity.example")
				folded := "m14-17b-" + kelvin + "-" + person.identity.String() + "@identity.example"
				aiExec(t, ctx, sp, `UPDATE admin_signup_claims SET email = $2 WHERE token_hash = $1`, h, folded)
				if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities i JOIN admin_signup_claims c ON i.email OPERATOR(public.=) c.email
				                       WHERE i.id = $1 AND c.token_hash = $2`, person.identity, h) != 1 {
					t.Fatal("PREMISE: citext does not fold the Kelvin sign onto the identity's address")
				}
			},
			digest: person.digest, cut: asciiCut, put: "    IF false AND (pg_catalog.octet_length(v_email::text) <= 254"},
	}
	for _, c := range cases {
		t.Run("refused/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if c.setup != nil {
					c.setup(sp)
				}
				if icpwCall(t, ctx, sp, b.tenant, h, b.tenant, c.session, c.digest) != uuid.Nil {
					t.Fatal("claimed; want refused")
				}
				icpwNothingWritten(t, ctx, sp, b, h, person.identity)
			})
		})
		t.Run("mutation/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if c.setup != nil {
					c.setup(sp)
				}
				icpwMutate(t, ctx, sp, c.cut, c.put)
				if icpwCall(t, ctx, sp, b.tenant, h, b.tenant, c.session, c.digest) == uuid.Nil {
					t.Fatal("with the guard removed the case is still refused: it does not test that guard")
				}
			})
		})
	}
	t.Run("a session and a digest", func(t *testing.T) {
		b2, h2 := both, bothClaim
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if icpwCall(t, ctx, sp, b2.tenant, h2, b2.tenant, &stranger.session, opFakeDigest("a")) != uuid.Nil {
				t.Fatal("two proofs in one call claimed")
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE tenant_id = $1`, b2.tenant); n != 0 {
				t.Fatal("refused, yet a membership row was written")
			}
		})
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if icCall(t, ctx, sp, b2.tenant, h2, b2.tenant, &stranger.session) == uuid.Nil {
				t.Fatal("PREMISE: the person's own session alone does not claim")
			}
		})
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			icpwMutate(t, ctx, sp, bothCut, "    IF false THEN")
			if icpwCall(t, ctx, sp, b2.tenant, h2, b2.tenant, &stranger.session, opFakeDigest("a")) == uuid.Nil {
				t.Fatal("MUTATION: without the guard two proofs are still refused -- the case does not test it")
			}
		})
	})
	// R18 / the two existing-identity paths stay apart: the person's digest never claims a draft of
	// ANOTHER address -- there is no identity of that address to hold it against.
	t.Run("refused/the digest of another address's identity", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			b2, h2 := other, otherClaim
			aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, stranger.admin)
			// The fixtures share one digest; the stranger's identity gets its own.
			aiExec(t, ctx, sp, `UPDATE admin_identities SET password_hash = $2 WHERE id = $1`, stranger.identity, opFakeDigest("s"))
			if icpwCall(t, ctx, sp, b2.tenant, h2, b2.tenant, nil, person.digest) != uuid.Nil {
				t.Fatal("one identity's digest claimed another address's draft")
			}
		})
	})
	t.Run("refused/a new address with a digest", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			b2, h2 := fresh, freshClaim
			before := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities`)
			if icpwCall(t, ctx, sp, b2.tenant, h2, b2.tenant, nil, opFakeDigest("q")) != uuid.Nil {
				t.Fatal("the password path birthed a new address's identity")
			}
			if opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities`) != before {
				t.Fatal("refused, yet an identity was born")
			}
		})
	})
}

// TestIdentityClaimSignup00080_TheOldBinarysCallStillClaims (expand, R8): the previous binary sends
// identity_claim_signup($1, $2, $3) -- three arguments; on 00080 that call resolves to the new function
// through the fourth argument's DEFAULT and claims a new address's draft as before.
func TestIdentityClaimSignup00080_TheOldBinarysCallStillClaims(t *testing.T) {
	ctx, tx := opTx(t)
	b := icNewBusiness(t, ctx, tx)
	email := aiEmail()
	h := icClaim(t, ctx, tx, email, icDraft(b), opFakeDigest("n"), "1 hour")
	const oldSQL = "SELECT admin_user_id\nFROM identity_claim_signup($1, $2, $3)"
	var admin uuid.UUID
	if err := aiApp(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, oldSQL, h, b.tenant, nil).Scan(&admin)
	}); err != nil {
		t.Fatalf("the old binary's call: %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                            WHERE a.id = $1 AND i.email = $2 AND a.linked_via = 'claim'`, admin, email); n != 1 {
		t.Fatal("the old binary's call did not birth the identity and its owner row")
	}
}

// TestIdentity00080_TheBodyIs00078sPlusThePasswordBranch: HEAD's body is 00080's text, and 00080's body
// is 00078's with exactly two insertions -- the one-proof guard and the password branch -- and nothing
// else changed (the session path, the new address's path, 6b, S2/T7 byte for byte).
func TestIdentity00080_TheBodyIs00078sPlusThePasswordBranch(t *testing.T) {
	ctx, tx := opTx(t)
	// T183: 00085 redefines this file's bodies again (the session lifetime); "HEAD" below is 00084.
	aslBelow00085(t, ctx, tx)
	up80, _ := opMigrationSections(t, ic00080File)
	up78, _ := opMigrationSections(t, "00078_verify_signups_by_e_mail.sql")
	body := func(src string) string {
		t.Helper()
		i := strings.Index(src, "CREATE FUNCTION public.identity_claim_signup(")
		if i < 0 {
			t.Fatal("no identity_claim_signup in the section")
		}
		a := strings.Index(src[i:], "AS $$") + i + len("AS $$")
		e := strings.Index(src[a:], "\n$$;") + a
		return src[a : e+1]
	}
	var prosrc string
	if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = 'public.identity_claim_signup(text,uuid,text,text)'::regprocedure`).Scan(&prosrc); err != nil {
		t.Fatal(err)
	}
	if prosrc != body(up80) {
		t.Fatal("HEAD's body is not 00080's")
	}
	b := body(up80)
	guard := "    -- ONE PROOF (M14-17b): a live session or a verified digest, never both -- no caller sends both,\n" +
		"    -- and neither path may decide for the other.\n" +
		"    IF p_session_hash IS NOT NULL AND p_verified_digest IS NOT NULL THEN\n        RETURN;\n    END IF;\n"
	if strings.Count(b, guard) != 1 {
		t.Fatal("PREMISE: the guard is not in 00080's body once")
	}
	b = strings.Replace(b, guard, "", 1)
	i := strings.Index(b, "    ELSIF p_verified_digest IS NOT NULL THEN\n")
	j := strings.Index(b, "    ELSE\n        -- A NEW ADDRESS")
	if i < 0 || j < i {
		t.Fatal("PREMISE: no password branch before the new address's")
	}
	if b[:i]+b[j:] != body(up78) {
		t.Fatal("00080's body differs from 00078's by more than the guard and the password branch")
	}
}

// TestIdentity00080_DownGivesBack00078sDefinitionByteForByte: in the test's transaction (rolled back),
// 00080's Down gives identity_claim_signup back exactly as 00078 created it -- the signature, the body
// (prosrc) equal to 00078's text, the result, the owner, SECURITY DEFINER, the pinned search_path and the
// ACL equal to HEAD's (00078's) -- and the Up again gives HEAD's definition and ACL back byte for byte.
// Neither locks a table (DROP / CREATE / GRANT on a function).
func TestIdentity00080_DownGivesBack00078sDefinitionByteForByte(t *testing.T) {
	ctx, tx := opTx(t)
	// T183: 00085 redefines this file's bodies again (the session lifetime); "HEAD" below is 00084.
	aslBelow00085(t, ctx, tx)
	up, down := opMigrationSections(t, ic00080File)
	if tables := downTables(down); len(tables) > 0 {
		t.Fatalf("PREMISE: 00080's Down locks %v", tables)
	}
	type def struct{ sig, src, result, owner, config, acl, full string }
	read := func(q pgx.Tx) def {
		t.Helper()
		var d def
		if err := q.QueryRow(ctx, `SELECT p.oid::regprocedure::text, p.prosrc, pg_get_function_result(p.oid), p.proowner::regrole::text,
		                                  coalesce(array_to_string(p.proconfig, ','), '') || '/' || p.prosecdef::text,
		                                  coalesce(p.proacl::text, ''), pg_get_functiondef(p.oid)
		                             FROM pg_proc p WHERE p.proname = 'identity_claim_signup'
		                              AND p.pronamespace = 'public'::regnamespace`).Scan(&d.sig, &d.src, &d.result, &d.owner, &d.config, &d.acl, &d.full); err != nil {
			t.Fatalf("read identity_claim_signup (exactly one): %v", err)
		}
		return d
	}
	head := read(tx)
	if head.sig != "identity_claim_signup(text,uuid,text,text)" {
		t.Fatalf("PREMISE at HEAD: %s", head.sig)
	}
	up78, _ := opMigrationSections(t, "00078_verify_signups_by_e_mail.sql")
	i := strings.Index(up78, "CREATE FUNCTION public.identity_claim_signup(")
	rest := up78[i:]
	block := rest[:strings.Index(rest, "\n$$;")+len("\n$$;")]
	if !strings.Contains(down, block) {
		t.Fatal("00080's Down does not hold 00078's CREATE block verbatim")
	}
	want78 := rest[strings.Index(rest, "AS $$")+len("AS $$") : strings.Index(rest, "\n$$;")+1]

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00080 Down")
		d := read(sp)
		if d.sig != "identity_claim_signup(text,uuid,text)" || d.src != want78 || d.result != "TABLE(admin_user_id uuid)" {
			t.Errorf("after the Down: %s, body 00078's %v, result %q", d.sig, d.src == want78, d.result)
		}
		if d.owner != "tappa_identitydefiner" || d.config != "search_path=pg_catalog, pg_temp/true" || d.acl != head.acl {
			t.Errorf("after the Down: owner %s, config %q, acl %q; want the definer, the pinned search_path, HEAD's acl %q",
				d.owner, d.config, d.acl, head.acl)
		}
		opRunSection(t, ctx, sp, up, "00080 Up again")
		if again := read(sp); again != head {
			t.Error("the Up again does not give HEAD's definition and ACL back byte for byte")
		}
	})
}
