package db

// identityclaim_test.go -- M14-17 (ADR 0031 §5.10, §5.2; migration 00078): the verified registration's
// four functions against real Postgres, called AS tappa_app through the very SQL this package sends, in
// opTx's transaction (rolled back). internal/domain/signup drives the same functions through the real
// registration (committed), internal/handler through the HTTP flow.
//
// 🔴 THE BUSINESSES ARE WRITTEN AT THE TOP LEVEL OF THE TEST'S TRANSACTION, every call in a savepoint
// over them: identity_claim_signup claims only a business THIS transaction wrote (tenants.xmin =
// pg_current_xact_id()), and a row inserted inside a savepoint carries the subtransaction's xid. The S13
// case below is exactly that shape, on purpose (identitysignup_test.go's discipline, M14-08).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// icBusiness is a business born in the test's transaction with no membership, and the draft naming it.
type icBusiness struct {
	tenant    uuid.UUID
	name, vat string
}

func icNewBusiness(t *testing.T, ctx context.Context, q pgx.Tx) icBusiness {
	t.Helper()
	b := icBusiness{tenant: uuid.New()}
	b.name, b.vat = "M14-17 claim "+b.tenant.String()[:8], "VAT-M1417-"+b.tenant.String()
	aiExec(t, ctx, q, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
	                   VALUES ($1, $2, $3, 'restaurant', 'single')`, b.tenant, b.name, b.vat)
	return b
}

// icDraft is the closed-key draft for b (00078's CHECK), with venues.
func icDraft(b icBusiness, venues ...string) []byte {
	if len(venues) == 0 {
		venues = []string{"Front door"}
	}
	d, err := json.Marshal(map[string]any{
		"full_name": "M14-17 Owner", "business_name": b.name, "vat_number": b.vat, "business_type": "restaurant",
		"structure": "single", "vat_check": "valid", "venues": venues,
	})
	if err != nil {
		panic(err)
	}
	return d
}

// icClaim inserts a draft as the owner (at the level of q) and returns its token hash.
func icClaim(t *testing.T, ctx context.Context, q pgx.Tx, email string, draft []byte, digest, ttl string) string {
	t.Helper()
	h := opRandHex(t)
	aiExec(t, ctx, q, `INSERT INTO admin_signup_claims (token_hash, email, draft, password_hash, expires_at)
	                   VALUES ($1, $2, $3::jsonb, $4, now() + $5::interval)`, h, email, string(draft), digest, ttl)
	return h
}

// icCall calls identity_claim_signup AS tappa_app under guc and returns the owner row (uuid.Nil when
// refused). Any error but the refusal fails the test.
func icCall(t *testing.T, ctx context.Context, q pgx.Tx, guc uuid.UUID, claim string, tenant uuid.UUID, session *string) uuid.UUID {
	t.Helper()
	var admin uuid.UUID
	err := aiApp(t, ctx, q, guc, func(sp pgx.Tx) error {
		var e error
		admin, e = ClaimSignup(ctx, sp, claim, tenant, session)
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

// icLinkedPerson is an existing identity with a linked, active row in its own business and a live session
// there.
type icLinkedPerson struct {
	identity, tenant, admin uuid.UUID
	email, session          string
}

func icNewPerson(t *testing.T, ctx context.Context, q pgx.Tx) icLinkedPerson {
	t.Helper()
	p := icLinkedPerson{email: aiEmail(), tenant: aiTenant(t, ctx, q)}
	p.identity = aiIdentity(t, ctx, q, p.email)
	p.admin = aiAdmin(t, ctx, q, p.tenant, p.email)
	aiLink(t, ctx, q, p.admin, p.identity)
	p.session = idSession(t, ctx, q, p.tenant, p.admin, nil)
	return p
}

// TestIdentityClaimSignup_ANewAddressBirthsTheIdentityAndTheOwnerRow: a new address's draft, claimed in a
// business born in this transaction under its own context -> ONE identity carrying the draft's digest,
// its mailbox proved (email_verified_at = the click), NO confirmation window (a fresh claim lists
// nothing: `claim` stays out of first_verify, S7); ONE owner row, linked via `claim`, the identity's
// address and a digest copy (the dual-write invariant); the draft spent; ONE signup.completed row whose
// detail is internal/domain/signup's key for key and names no address and no identity.
func TestIdentityClaimSignup_ANewAddressBirthsTheIdentityAndTheOwnerRow(t *testing.T) {
	ctx, tx := opTx(t)
	b := icNewBusiness(t, ctx, tx)
	email := aiEmail()
	h := icClaim(t, ctx, tx, email, icDraft(b, "Front door", "Back door"), opFakeDigest("n"), "1 hour")

	admin := icCall(t, ctx, tx, b.tenant, h, b.tenant, nil)
	if admin == uuid.Nil {
		t.Fatal("the claim of a new address was refused")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                            WHERE a.id = $1 AND a.tenant_id = $2 AND a.role = 'owner' AND a.status = 'active'
	                              AND a.full_name = 'M14-17 Owner' AND a.email = i.email AND a.password_hash = i.password_hash
	                              AND a.linked_via = 'claim' AND a.linked_at = now() AND a.unlinked_at IS NULL
	                              AND i.email = $3 AND i.password_hash = $4
	                              AND i.email_verified_at = now() AND i.confirm_opened_at IS NULL AND i.confirm_scope IS NULL`,
		admin, b.tenant, email, opFakeDigest("n")); n != 1 {
		t.Fatal("the owner row is not the identity's (claim, its address, its digest), or the identity is not the draft's, verified, without a window")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE tenant_id = $1`, b.tenant); n != 1 {
		t.Fatalf("%d membership rows in the new business, want 1", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_signup_claims WHERE token_hash = $1 AND used_at = now()`, h); n != 1 {
		t.Fatal("the draft is not spent")
	}
	var detail map[string]any
	var actor uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT actor_id, detail FROM audit_log WHERE tenant_id = $1 AND action = 'signup.completed'
	                             AND target = $2`, b.tenant, b.tenant.String()).Scan(&actor, &detail); err != nil {
		t.Fatalf("the signup.completed row: %v", err)
	}
	want := map[string]any{"vat_number": b.vat, "vat_check": "valid", "business_type": "restaurant", "structure": "single", "venues": float64(2)}
	if actor != admin || fmt.Sprint(detail) != fmt.Sprint(want) {
		t.Fatalf("the trail row is actor %s detail %v, want actor %s detail %v", actor, detail, admin, want)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, b.tenant); n != 1 {
		t.Fatalf("%d trail rows in the new business, want the one signup.completed", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND (detail::text ILIKE '%' || $2 || '%' OR detail::text LIKE '%' || $3 || '%')`,
		b.tenant, email, admin.String()); n != 0 {
		t.Fatal("the trail names the address or the owner's id")
	}

	// The same link a second time, in another new business: spent -> refused, nothing born.
	b2 := icNewBusiness(t, ctx, tx)
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE admin_signup_claims SET draft = $2::jsonb WHERE token_hash = $1`, h, string(icDraft(b2)))
		if got := icCall(t, ctx, sp, b2.tenant, h, b2.tenant, nil); got != uuid.Nil {
			t.Fatal("a spent link claimed a second business")
		}
	})
}

// TestIdentityClaimSignup_EveryPredicateRefusesAlone is S2/T7: each structural predicate, each case
// alone, refuses (0 rows, nothing written) -- and, as a MUTATION in the test, the predicate removed from
// the body makes exactly its case claim (the case tests that predicate and nothing else):
//
//	1 (MAIN)   the business already has a membership row
//	2          the business was born before this transaction (created_at backdated at the top level)
//	3 (S13)    the business was written under a SAVEPOINT (its xmin is the subtransaction's)
//	4          the call runs under another business's context; under none
//	5          the draft's VAT number; the draft's name
//	           a spent draft; an expired draft; an unknown token; an address that is not ASCII
//
// and, separately, an EXISTING business (committed by another connection before this test, no member):
// refused by 2 and 3 together; with both removed it would be claimed.
func TestIdentityClaimSignup_EveryPredicateRefusesAlone(t *testing.T) {
	// Committed BEFORE opTx's REPEATABLE READ snapshot, or the test's transaction would not see it at all
	// (measured: the mutation below stayed refused for that reason alone).
	existing := icCommittedBusiness(t)
	ctx, tx := opTx(t)
	type world struct {
		b     icBusiness
		claim string
	}
	fresh := func(q pgx.Tx) world {
		b := icNewBusiness(t, ctx, q)
		return world{b: b, claim: icClaim(t, ctx, q, aiEmail(), icDraft(b), opFakeDigest("p"), "1 hour")}
	}
	top := fresh(tx)
	old := fresh(tx)
	aiExec(t, ctx, tx, `UPDATE tenants SET created_at = now() - interval '1 day' WHERE id = $1`, old.b.tenant)
	other := aiTenant(t, ctx, tx)

	type refusal struct {
		name     string
		w        func(sp pgx.Tx) world // nil: top
		setup    func(sp pgx.Tx, w world)
		guc      func(w world) uuid.UUID // nil: the business's own
		cut, put string
	}
	const funcSig = "public.identity_claim_signup(text, uuid, text)"
	cases := []refusal{
		{name: "1: the business already has a membership row",
			setup: func(sp pgx.Tx, w world) { aiAdmin(t, ctx, sp, w.b.tenant, aiEmail()) },
			cut:   "    IF FOUND THEN\n        RETURN;\n    END IF;\n\n    IF p_session_hash IS NOT NULL THEN", put: "    IF p_session_hash IS NOT NULL THEN"},
		{name: "2: the business was born before this transaction", w: func(pgx.Tx) world { return old },
			cut: "        AND v_born = pg_catalog.now()\n", put: "\n"},
		{name: "3 (S13): the business was written under a savepoint", w: func(sp pgx.Tx) world { return fresh(sp) },
			cut: "        AND v_xmin = pg_catalog.pg_current_xact_id()::xid\n", put: "\n"},
		{name: "4: another business's context", guc: func(world) uuid.UUID { return other },
			cut: "IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN", put: "IF false THEN"},
		{name: "4: no context at all", guc: func(world) uuid.UUID { return uuid.Nil },
			cut: "IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN", put: "IF false THEN"},
		{name: "5: the draft's VAT number is another",
			setup: func(sp pgx.Tx, w world) {
				aiExec(t, ctx, sp, `UPDATE admin_signup_claims SET draft = jsonb_set(draft, '{vat_number}', '"VAT-OTHER"') WHERE token_hash = $1`, w.claim)
			},
			cut: "        AND v_vat = (v_draft ->> 'vat_number')) IS NOT TRUE THEN", put: "        ) IS NOT TRUE THEN"},
		{name: "5: the draft's name is another",
			setup: func(sp pgx.Tx, w world) {
				aiExec(t, ctx, sp, `UPDATE admin_signup_claims SET draft = jsonb_set(draft, '{business_name}', '"Another business"') WHERE token_hash = $1`, w.claim)
			},
			cut: "        AND v_name = (v_draft ->> 'business_name')\n", put: "\n"},
		{name: "a spent draft",
			setup: func(sp pgx.Tx, w world) {
				aiExec(t, ctx, sp, `UPDATE admin_signup_claims SET used_at = now() WHERE token_hash = $1`, w.claim)
			},
			cut: "    IF (v_used IS NULL AND v_expires > v_now) IS NOT TRUE THEN", put: "    IF false THEN"},
		{name: "an expired draft",
			setup: func(sp pgx.Tx, w world) {
				aiExec(t, ctx, sp, `UPDATE admin_signup_claims SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour' WHERE token_hash = $1`, w.claim)
			},
			cut: "    IF (v_used IS NULL AND v_expires > v_now) IS NOT TRUE THEN", put: "    IF false THEN"},
		{name: "an address that is not ASCII",
			setup: func(sp pgx.Tx, w world) {
				aiExec(t, ctx, sp, `UPDATE admin_signup_claims SET email = 'm14-' || gen_random_uuid() || '@gmaİl.example' WHERE token_hash = $1`, w.claim)
			},
			cut: "    IF (pg_catalog.octet_length(v_email::text) <= 254", put: "    IF false AND (pg_catalog.octet_length(v_email::text) <= 254"},
	}
	for _, c := range cases {
		run := func(sp pgx.Tx) uuid.UUID {
			w := top
			if c.w != nil {
				w = c.w(sp)
			}
			if c.setup != nil {
				c.setup(sp, w)
			}
			guc := w.b.tenant
			if c.guc != nil {
				guc = c.guc(w)
			}
			identities := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities`)
			admin := icCall(t, ctx, sp, guc, w.claim, w.b.tenant, nil)
			if admin == uuid.Nil {
				if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities`); n != identities {
					t.Fatal("refused, yet an identity was born")
				}
				if n := opInt(t, ctx, sp, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'signup.completed'`, w.b.tenant); n != 0 {
					t.Fatal("refused, yet a trail row was written")
				}
			}
			return admin
		}
		t.Run("refused/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if run(sp) != uuid.Nil {
					t.Fatal("claimed; want refused")
				}
			})
		})
		t.Run("mutation/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				var def string
				if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, funcSig).Scan(&def); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(def, c.cut) {
					t.Fatalf("PREMISE: the body lacks %q", c.cut)
				}
				aiExec(t, ctx, sp, strings.Replace(def, c.cut, c.put, 1))
				if run(sp) == uuid.Nil {
					t.Fatal("with the predicate removed the case is still refused: it does not test that predicate")
				}
			})
		})
	}
	t.Run("an unknown token", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if icCall(t, ctx, sp, top.b.tenant, opRandHex(t), top.b.tenant, nil) != uuid.Nil {
				t.Fatal("an unknown token claimed")
			}
		})
	})

	// AN EXISTING BUSINESS (the card's mutation: "var olan bir tenant'ın id'siyle çağrı -> 0 satır"),
	// committed by another connection before this test -- no member, so predicate 1 does not see it; 2 and
	// 3 do. Removing both claims it, so the two together are what refuses it.
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tenants WHERE id = $1 AND created_at < now()`, existing.tenant); n != 1 {
		t.Fatal("PREMISE: the test's transaction does not see the existing business as older than itself")
	}
	claim := icClaim(t, ctx, tx, aiEmail(), icDraft(existing), opFakeDigest("e"), "1 hour")
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if icCall(t, ctx, sp, existing.tenant, claim, existing.tenant, nil) != uuid.Nil {
			t.Fatal("an existing business was claimed")
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		var def string
		if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, funcSig).Scan(&def); err != nil {
			t.Fatal(err)
		}
		for _, cut := range []string{"        AND v_born = pg_catalog.now()\n", "        AND v_xmin = pg_catalog.pg_current_xact_id()::xid\n"} {
			if !strings.Contains(def, cut) {
				t.Fatalf("PREMISE: the body lacks %q", cut)
			}
			def = strings.Replace(def, cut, "\n", 1)
		}
		aiExec(t, ctx, sp, def)
		if icCall(t, ctx, sp, existing.tenant, claim, existing.tenant, nil) == uuid.Nil {
			t.Fatal("MUTATION: without predicates 2 and 3 the existing business is still refused -- the case does not test them")
		}
	})
}

// icCommittedBusiness commits a business with no member on its own connection (an EXISTING tenant, from
// before the test's transaction) and removes it when the test ends.
func icCommittedBusiness(t *testing.T) icBusiness {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	b := icBusiness{tenant: uuid.New()}
	b.name, b.vat = "M14-17 existing "+b.tenant.String()[:8], "VAT-M1417E-"+b.tenant.String()
	if _, err := conn.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'restaurant', 'single')`,
		b.tenant, b.name, b.vat); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(ctx, `DELETE FROM feed_counters WHERE tenant_id = $1`, b.tenant); err != nil {
			t.Errorf("remove the committed business's feed counter: %v", err)
		}
		if _, err := conn.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, b.tenant); err != nil {
			t.Errorf("remove the committed business: %v", err)
		}
		_ = conn.Close(ctx)
	})
	return b
}

// TestIdentityClaimSignup_AnExistingIdentityClaimsOnlyWithItsOwnSession is R18 and §5.10's second path:
// a draft for an address that has an identity is claimed ONLY through a live session of THAT identity --
// no session, another identity's session, a revoked one, a session of an unlinked row, a disabled row:
// 0 rows. With its own session: the owner row is linked (`claim`) to that identity with its digest, no
// second identity is born, and the click is the mailbox proof -- an unverified identity gets
// email_verified_at and a first_verify window (and its event), a verified one is left as it was.
func TestIdentityClaimSignup_AnExistingIdentityClaimsOnlyWithItsOwnSession(t *testing.T) {
	ctx, tx := opTx(t)
	person := icNewPerson(t, ctx, tx)
	stranger := icNewPerson(t, ctx, tx)
	b := icNewBusiness(t, ctx, tx)
	// The draft is the person's address in another spelling (citext): the identity's own address is the row's.
	h := icClaim(t, ctx, tx, strings.ToUpper(person.email), icDraft(b), opFakeDigest("q"), "1 hour")
	ptr := func(s string) *string { return &s }

	for _, c := range []struct {
		name    string
		session func(sp pgx.Tx) *string
	}{
		{"no session: a new address's path for an address that has an identity", func(pgx.Tx) *string { return nil }},
		{"another identity's session (R18)", func(pgx.Tx) *string { return ptr(stranger.session) }},
		{"an unknown session", func(pgx.Tx) *string { return ptr(opRandHex(t)) }},
		{"a revoked session", func(sp pgx.Tx) *string {
			aiExec(t, ctx, sp, `UPDATE admin_sessions SET revoked_at = now() WHERE token_hash = $1`, person.session)
			return ptr(person.session)
		}},
		{"a disabled row's session", func(sp pgx.Tx) *string {
			aiExec(t, ctx, sp, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, person.admin)
			return ptr(person.session)
		}},
		{"an unlinked row of the same address", func(sp pgx.Tx) *string {
			legacy := aiAdmin(t, ctx, sp, aiTenant(t, ctx, sp), person.email)
			return ptr(idSession(t, ctx, sp, mustTenantOf(t, ctx, sp, legacy), legacy, nil))
		}},
	} {
		t.Run("refused/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if icCall(t, ctx, sp, b.tenant, h, b.tenant, c.session(sp)) != uuid.Nil {
					t.Fatal("claimed; want refused")
				}
				if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE email = $1`, person.email); n != 1 {
					t.Fatalf("%d identities for the address, want the one", n)
				}
			})
		})
	}
	t.Run("its own session, unverified: the click proves the mailbox and opens first_verify", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			admin := icCall(t, ctx, sp, b.tenant, h, b.tenant, &person.session)
			if admin == uuid.Nil {
				t.Fatal("refused with the identity's own session")
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
			                            WHERE a.id = $1 AND i.id = $2 AND a.tenant_id = $3 AND a.linked_via = 'claim'
			                              AND a.email = i.email AND a.password_hash = i.password_hash
			                              AND i.email_verified_at = now() AND i.confirm_scope = 'first_verify'
			                              AND i.confirm_opened_at = now() AND i.confirm_open_until = now() + interval '7 days'`,
				admin, person.identity, b.tenant); n != 1 {
				t.Fatal("not linked to the person's identity with its digest, or the mailbox proof / window is wrong")
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'email_verified'
			                            AND detail = '{"scope": "first_verify"}'`, person.identity); n != 1 {
				t.Fatal("no email_verified event")
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE email = $1`, person.email); n != 1 {
				t.Fatal("a second identity was born")
			}
		})
	})
	t.Run("its own session, verified: nothing about the identity changes", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET email_verified_at = now() - interval '3 days' WHERE id = $1`, person.identity)
			if icCall(t, ctx, sp, b.tenant, h, b.tenant, &person.session) == uuid.Nil {
				t.Fatal("refused with the identity's own session")
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = $1 AND email_verified_at = now() - interval '3 days'
			                            AND confirm_scope IS NULL`, person.identity); n != 1 {
				t.Fatal("a verified identity's proof or window changed")
			}
		})
	})
	t.Run("mutation: without the address equality another identity's session claims", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			var def string
			if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_claim_signup(text, uuid, text)'::regprocedure)`).Scan(&def); err != nil {
				t.Fatal(err)
			}
			cut := "IF (FOUND AND v_id_email OPERATOR(public.=) v_email) IS NOT TRUE THEN"
			if !strings.Contains(def, cut) {
				t.Fatalf("PREMISE: the body lacks %q", cut)
			}
			aiExec(t, ctx, sp, strings.Replace(def, cut, "IF FOUND IS NOT TRUE THEN", 1))
			if icCall(t, ctx, sp, b.tenant, h, b.tenant, &stranger.session) == uuid.Nil {
				t.Fatal("still refused: the case does not test the equality")
			}
		})
	})
}

func mustTenantOf(t *testing.T, ctx context.Context, q pgx.Tx, admin uuid.UUID) uuid.UUID {
	t.Helper()
	var tenant uuid.UUID
	if err := q.QueryRow(ctx, `SELECT tenant_id FROM admin_users WHERE id = $1`, admin).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	return tenant
}

// TestIdentityOpenSignupClaim_WritesOnlyTheDraftAndRefusesWhatTheTableWouldNot: the open writes ONE draft
// row and touches no other table (R18: nothing is born before the click); the resolver reads it back --
// twice, spending nothing (the GET, R19); every value the table's CHECKs would refuse, and an address
// that is not ASCII, answers NULL (pgx.ErrNoRows here) with nothing written and NO error -- a CHECK
// violation would carry the digest in its DETAIL (00074's lesson).
func TestIdentityOpenSignupClaim_WritesOnlyTheDraftAndRefusesWhatTheTableWouldNot(t *testing.T) {
	ctx, tx := opTx(t)
	b := icNewBusiness(t, ctx, tx)
	draft := icDraft(b)
	open := func(sp pgx.Tx, hash, email string, d []byte, digest, ttl string) (uuid.UUID, error) {
		var id uuid.UUID
		err := aiApp(t, ctx, sp, uuid.Nil, func(app pgx.Tx) error {
			var got *uuid.UUID
			if err := app.QueryRow(ctx, `SELECT identity_open_signup_claim($1, $2::citext, $3::jsonb, $4, now() + $5::interval)`,
				hash, email, string(d), digest, ttl).Scan(&got); err != nil {
				return err
			}
			if got == nil {
				return pgx.ErrNoRows
			}
			id = *got
			return nil
		})
		return id, err
	}
	counts := func(sp pgx.Tx) string {
		var s string
		if err := sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM admin_identities) || '/' || (SELECT count(*) FROM admin_users) || '/' ||
		                                   (SELECT count(*) FROM tenants) || '/' || (SELECT count(*) FROM audit_log) || '/' ||
		                                   (SELECT count(*) FROM admin_identity_events)`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		before := counts(sp)
		h, email := opRandHex(t), aiEmail()
		id, err := open(sp, h, email, draft, opFakeDigest("o"), "1 hour")
		if err != nil || id == uuid.Nil {
			t.Fatalf("open: %v", err)
		}
		if after := counts(sp); after != before {
			t.Fatalf("the open touched another table: %s -> %s", before, after)
		}
		for i := 0; i < 2; i++ {
			var r ResolvedSignupClaim
			if err := aiApp(t, ctx, sp, uuid.Nil, func(app pgx.Tx) error {
				var digest string
				return app.QueryRow(ctx, resolveSignupClaimSQL, h).Scan(&r.ID, &r.Email, &digest, &r.ExpiresAt, &r.UsedAt, &r.Draft)
			}); err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if r.ID != id || r.Email != email || r.UsedAt != nil || !jsonEqual(t, r.Draft, draft) {
				t.Fatalf("read %d: %+v", i, r)
			}
		}
	})
	bad := func(edit func(m map[string]any)) []byte {
		var m map[string]any
		if err := json.Unmarshal(draft, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		out, _ := json.Marshal(m)
		return out
	}
	for _, c := range []struct {
		name, hash, email, digest, ttl string
		draft                          []byte
	}{
		{"an extra draft key", "", "", "", "", bad(func(m map[string]any) { m["password"] = "x" })},
		{"a missing draft key", "", "", "", "", bad(func(m map[string]any) { delete(m, "structure") })},
		{"no venue", "", "", "", "", bad(func(m map[string]any) { m["venues"] = []string{} })},
		{"a venue that is not a string", "", "", "", "", bad(func(m map[string]any) { m["venues"] = []any{1} })},
		{"an unknown VAT verdict", "", "", "", "", bad(func(m map[string]any) { m["vat_check"] = "maybe" })},
		{"a digest that is not bcrypt", "", "", "not-a-digest", "", nil},
		{"a token hash of another shape", "ABC", "", "", "", nil},
		{"a lifetime over 24 hours", "", "", "", "25 hours", nil},
		{"an expired lifetime", "", "", "", "-1 minute", nil},
		{"an address that is not ASCII (the dotted capital I)", "", "a@gmaİl.example", "", "", nil},
		{"an address that is not ASCII (the Kelvin sign)", "", "a@Kelvin.example", "", "", nil},
	} {
		t.Run("refused/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				hash, email, digest, ttl, d := opRandHex(t), aiEmail(), opFakeDigest("r"), "1 hour", draft
				if c.hash != "" {
					hash = c.hash
				}
				if c.email != "" {
					email = c.email
				}
				if c.digest != "" {
					digest = c.digest
				}
				if c.ttl != "" {
					ttl = c.ttl
				}
				if c.draft != nil {
					d = c.draft
				}
				before := opInt(t, ctx, sp, `SELECT count(*) FROM admin_signup_claims`)
				if _, err := open(sp, hash, email, d, digest, ttl); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("got %v, want the refusal (NULL) and no error", err)
				}
				if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_signup_claims`); n != before {
					t.Fatal("a refused draft was written")
				}
			})
		})
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return fmt.Sprint(x) == fmt.Sprint(y)
}

// TestIdentitySweepSignupClaims_TakesTheExpiredAndTheUsedAndNothingElse: the table's one delete path
// (R17) removes an expired and a used draft and keeps a live one.
func TestIdentitySweepSignupClaims_TakesTheExpiredAndTheUsedAndNothingElse(t *testing.T) {
	ctx, tx := opTx(t)
	b := icNewBusiness(t, ctx, tx)
	live := icClaim(t, ctx, tx, aiEmail(), icDraft(b), opFakeDigest("l"), "1 hour")
	used := icClaim(t, ctx, tx, aiEmail(), icDraft(b), opFakeDigest("u"), "1 hour")
	expired := icClaim(t, ctx, tx, aiEmail(), icDraft(b), opFakeDigest("x"), "1 hour")
	aiExec(t, ctx, tx, `UPDATE admin_signup_claims SET used_at = now() WHERE token_hash = $1`, used)
	aiExec(t, ctx, tx, `UPDATE admin_signup_claims SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE token_hash = $1`, expired)
	var n int32
	if err := aiApp(t, ctx, tx, uuid.Nil, func(app pgx.Tx) error {
		return app.QueryRow(ctx, sweepSignupClaimsSQL).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("the sweep took %d drafts, want at least the two", n)
	}
	for h, want := range map[string]int64{live: 1, used: 0, expired: 0} {
		if got := opInt(t, ctx, tx, `SELECT count(*) FROM admin_signup_claims WHERE token_hash = $1`, h); got != want {
			t.Errorf("draft %s...: %d rows, want %d", h[:8], got, want)
		}
	}
}

// TestSignupClaims_TappaAppReachesTheTableOnlyThroughTheFunctions: tappa_app holds nothing on
// admin_signup_claims (00078's REVOKE ALL; the default ACL would have given it arwd) and FORCE RLS with no
// policy leaves a role that obeys RLS nothing -- an RLS probe WITHOUT a filter of its own.
func TestSignupClaims_TappaAppReachesTheTableOnlyThroughTheFunctions(t *testing.T) {
	ctx, tx := opTx(t)
	b := icNewBusiness(t, ctx, tx)
	icClaim(t, ctx, tx, aiEmail(), icDraft(b), opFakeDigest("z"), "1 hour")
	for _, sql := range []string{
		`SELECT count(*) FROM admin_signup_claims`,
		`INSERT INTO admin_signup_claims (token_hash, email, draft, password_hash, expires_at) VALUES ('x', 'x', '{}', 'x', now())`,
		`UPDATE admin_signup_claims SET used_at = now()`,
		`DELETE FROM admin_signup_claims`,
	} {
		err := aiApp(t, ctx, tx, b.tenant, func(app pgx.Tx) error {
			_, err := app.Exec(ctx, sql)
			return err
		})
		if code, _ := opCode(err); code != sqlstateInsufficientPrivi {
			t.Errorf("%s as tappa_app: %v, want 42501", sql, err)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy WHERE polrelid = 'admin_signup_claims'::regclass`); n != 0 {
		t.Fatalf("%d policies on admin_signup_claims, want none", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class WHERE oid = 'admin_signup_claims'::regclass AND relrowsecurity AND relforcerowsecurity`); n != 1 {
		t.Fatal("admin_signup_claims is not ENABLE + FORCE ROW LEVEL SECURITY")
	}
}

// TestIdentityClaimSignup_AClaimRowIsOutOfFirstVerifyAndInReset is S7/T1 for the row this card births: an
// existing identity claims a business (its first verification opens a first_verify window) -- the `claim`
// membership is NOT one the first_verify window lists, so identity_unlink_backfilled refuses it; once a
// reset-scope window opens after the claim, the same membership IS listed and can be unlinked (a forwarded
// link the attacker's pre-planted identity used is visible to the address's real owner after a reset).
func TestIdentityClaimSignup_AClaimRowIsOutOfFirstVerifyAndInReset(t *testing.T) {
	ctx, tx := opTx(t)
	person := icNewPerson(t, ctx, tx)
	b := icNewBusiness(t, ctx, tx)
	h := icClaim(t, ctx, tx, person.email, icDraft(b), opFakeDigest("w"), "1 hour")
	if icCall(t, ctx, tx, b.tenant, h, b.tenant, &person.session) == uuid.Nil {
		t.Fatal("PREMISE: the person's claim was refused")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identities WHERE id = $1 AND confirm_scope = 'first_verify'`, person.identity); n != 1 {
		t.Fatal("PREMISE: the claim opened no first_verify window")
	}
	unlink := func(sp pgx.Tx) int64 {
		fresh := opRandHex(t)
		aiExec(t, ctx, sp, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, authenticated_at) VALUES ($1, $2, $3, clock_timestamp())`,
			person.tenant, person.admin, fresh)
		var n int64
		if err := aiApp(t, ctx, sp, uuid.Nil, func(app pgx.Tx) error {
			rows, err := app.Query(ctx, `SELECT * FROM identity_unlink_backfilled($1, $2)`, fresh, b.tenant)
			if err != nil {
				return err
			}
			for rows.Next() {
				n++
			}
			rows.Close()
			return rows.Err()
		}); err != nil {
			t.Fatalf("identity_unlink_backfilled: %v", err)
		}
		return n
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if n := unlink(sp); n != 0 {
			t.Fatal("the first_verify window lists the claim membership (S7)")
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE admin_identities SET confirm_scope = 'reset', confirm_opened_at = now() + interval '1 second',
		                           confirm_open_until = now() + interval '7 days' WHERE id = $1`, person.identity)
		if n := unlink(sp); n != 1 {
			t.Fatal("a reset window opened after the claim does not list the claim membership (T1)")
		}
	})
}
