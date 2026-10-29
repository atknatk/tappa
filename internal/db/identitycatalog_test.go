package db

// identitycatalog_test.go -- M14-04 (ADR 0031 §5.2): the catalogue half of the CLOSED identity_*
// list against real Postgres, owner session, everything rolled back (opTx):
//
//   - the SIGNATURE TABLE (ADR 0027 §2's method: pg_get_function_identity_arguments and
//     pg_get_function_result, verbatim), the owners, SECURITY DEFINER, the pinned search path, one
//     overload per name, and EXECUTE exactly {owner, tappa_app} -- or {owner, tappa_owner} for the
//     one named exception, identity_reconcile() (M14-06b);
//   - no operator role (tappa_operator, tappa_opdefiner), no other definer and not PUBLIC may
//     execute any of them;
//   - tappa_identitydefiner's verb x column MATRIX, held inside ADR 0031 §5.2's matrix, every grant
//     shown used (revoked one at a time -> a call refused);
//   - tappa_resolver's reach on the identity columns;
//   - the BELT: every reference to a tenant table in a BYPASSRLS body names its tenant;
//   - the query-text scans of R16 (identity_id selected by TouchAdminSession alone, linked_via by
//     nothing, no `*` on admin_users).
//
// 🔴 THIS FILE IS A SKELETON THE LATER CARDS FILL, NOT A LIST THEY REWRITE (holdingcatalog_test.go's
// shape). M14-05 (and every later card of ADR 0031 §5.2) adds, in the same change as its migration:
//   - its rows to a list of its own (identitySignaturesM1405, ...) and that list to identitySignatures;
//   - its grants to a matrix of its own (identityDefinerMatrixM1405, ...) and that map to
//     identityDefinerMatrixParts;
//   - its calls to identityExercise, so the "every grant is used" test reaches its grants;
//   - a named tenant-wide read, if it has one, to identityTenantWideReads.
// Every test below derives its subject from the CATALOGUE (every function the definer owns, every
// function named identity_* / resolve_identity_*, every grant), so a forgotten row is red, not silent.

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// identitySignature is one row of ADR 0031 §5.2's signature table.
type identitySignature struct {
	name, args, result, owner string
}

// identitySignaturesM1404 is M14-04's part of the table (migration 00071), verbatim.
var identitySignaturesM1404 = []identitySignature{
	// M14-14 (00075, ADR 0031 §5.2's 2026-10-26 note): redefined with the identity's registered address
	// appended -- the reset request's recipient, from the same row as the id.
	{"resolve_identity_by_email", "p_email citext",
		"TABLE(id uuid, password_hash text, email_verified_at timestamp with time zone, has_active_membership boolean, email text)", "tappa_resolver"},
	// p_verified_digest: the user's decision of 2026-10-23 (ADR 0031 §5.2) -- the proof re-checked under the lock.
	{"identity_open_session", "p_identity_id uuid, p_tenant uuid, p_token_hash text, p_verified_digest text",
		"TABLE(session_id uuid, tenant_id uuid, admin_user_id uuid)", "tappa_identitydefiner"},
	{"identity_memberships", "p_session_hash text",
		"TABLE(tenant_id uuid, tenant_name text, role text, suspended boolean, is_current boolean)", "tappa_identitydefiner"},
	{"identity_switch_session", "p_session_hash text, p_target_tenant uuid, p_new_hash text",
		"TABLE(session_id uuid, tenant_id uuid, admin_user_id uuid)", "tappa_identitydefiner"},
	{"identity_revoke_sessions", "p_session_hash text, p_keep_current boolean",
		"TABLE(revoked integer)", "tappa_identitydefiner"},
}

// identitySignaturesM1405 is M14-05's part (migration 00072), verbatim -- resolve_admin_by_email's
// redefinition (T2, T5) included.
var identitySignaturesM1405 = []identitySignature{
	{"resolve_admin_by_email", "p_email citext", "TABLE(id uuid, tenant_id uuid, password_hash text, status text, identity_id uuid)", "tappa_resolver"},
	{"resolve_identity_reset_by_token_hash", "p_token_hash text",
		"TABLE(id uuid, identity_id uuid, expires_at timestamp with time zone, used_at timestamp with time zone, cancelled_at timestamp with time zone)", "tappa_resolver"},
	{"resolve_identity_verification_by_token_hash", "p_token_hash text",
		"TABLE(id uuid, identity_id uuid, expires_at timestamp with time zone, used_at timestamp with time zone)", "tappa_resolver"},
	{"identity_password_for_session", "p_session_hash text", "TABLE(password_hash text)", "tappa_identitydefiner"},
	{"identity_set_password", "p_session_hash text, p_new_hash text", "TABLE(revoked_other_sessions integer)", "tappa_identitydefiner"},
	{"identity_consume_row_reset", "p_reset_hash text, p_new_hash text",
		"TABLE(admin_user_id uuid, tenant_id uuid, reset_id uuid, revoked_sessions integer)", "tappa_identitydefiner"},
	// M14-14 (00075): redefined with the identity's registered address appended -- the EM-9 notice's recipient.
	{"identity_consume_reset", "p_reset_hash text, p_new_hash text", "TABLE(identity_id uuid, revoked_sessions integer, email text)", "tappa_identitydefiner"},
	{"identity_link_legacy", "p_identity_session_hash text, p_legacy_session_hash text", "TABLE(admin_user_id uuid, tenant_id uuid)", "tappa_identitydefiner"},
	{"identity_unlink_backfilled", "p_session_hash text, p_tenant uuid", "TABLE(admin_user_id uuid, revoked_sessions integer)", "tappa_identitydefiner"},
	{"identity_relink_refused", "p_session_hash text, p_tenant uuid", "TABLE(admin_user_id uuid)", "tappa_identitydefiner"},
	// M14-22 (00084): identity_request_verification gained its purpose -- identitySignaturesM1422.
	{"identity_verify_email", "p_verification_hash text, p_session_hash text", "TABLE(first_verification boolean)", "tappa_identitydefiner"},
	{"identity_record_failed_login", "p_identity_id uuid, p_event text", "TABLE(recorded boolean)", "tappa_identitydefiner"},
	{"identity_sweep_events", "", "TABLE(deleted bigint)", "tappa_identitydefiner"},
	{"identity_open_reset", "p_identity_id uuid, p_token_hash text, p_expires_at timestamp with time zone",
		"TABLE(reset_id uuid, expires_at timestamp with time zone, issued boolean)", "tappa_identitydefiner"},
	{"identity_withdraw_reset", "p_token_hash text", "boolean", "tappa_identitydefiner"},
}

// identitySignaturesM1408 is M14-08's part (migration 00074): named exception 2, the registration's
// dual-write. Its answer -- did it link -- is a boolean the caller keeps to itself.
var identitySignaturesM1408 = []identitySignature{
	{"identity_create_for_signup", "p_tenant_id uuid, p_admin_user_id uuid", "boolean", "tappa_identitydefiner"},
}

// identitySignaturesM1418 is M14-18's part (migration 00079; ADR 0031 §5.2's note of 2026-10-27, the
// user's decision): the confirmation window's reader and its one-click close ("Keep all", T11).
var identitySignaturesM1418 = []identitySignature{
	{"identity_confirm_window", "p_session_hash text",
		"TABLE(verified boolean, scope text, open_until timestamp with time zone, tenant_id uuid, tenant_name text, role text, refused boolean, last_active_owner boolean)", "tappa_identitydefiner"},
	{"identity_keep_all", "p_session_hash text", "TABLE(kept boolean)", "tappa_identitydefiner"},
}

// identitySignaturesM1415 is M14-15's part (migration 00077): the invitation's md.7 resolver and its two
// acceptances -- ADR 0031 §5.2's rows (the resolver's argument named p_token_hash like
// resolve_identity_reset_by_token_hash's, the ADR's table writing both as p_hash). Each acceptance
// answers the membership it made and its business, or nothing.
var identitySignaturesM1415 = []identitySignature{
	{"resolve_admin_invite_by_token_hash", "p_token_hash text",
		"TABLE(id uuid, tenant_id uuid, role text, expires_at timestamp with time zone, used_at timestamp with time zone, cancelled_at timestamp with time zone)", "tappa_resolver"},
	{"identity_accept_invite", "p_invite_hash text, p_session_hash text", "TABLE(admin_user_id uuid, tenant_id uuid)", "tappa_identitydefiner"},
	{"identity_accept_invite_new", "p_invite_hash text, p_new_hash text", "TABLE(admin_user_id uuid, tenant_id uuid)", "tappa_identitydefiner"},
}

// identitySignaturesM1417 is M14-17's part (migration 00078): the verified registration (ADR 0031 §5.10).
// The resolver returns the draft last (the 2026-10-26 user decision, ADR 0031 §5.2); the open answers the
// draft's id or NULL; the claim the owner row; the sweep how many drafts went. 00080 (M14-17b, the user's
// decision of 2026-10-27): the claim's fourth argument, the identity's verified digest.
var identitySignaturesM1417 = []identitySignature{
	{"resolve_signup_claim_by_token_hash", "p_token_hash text",
		"TABLE(id uuid, email text, password_hash text, expires_at timestamp with time zone, used_at timestamp with time zone, draft jsonb)", "tappa_resolver"},
	{"identity_open_signup_claim", "p_token_hash text, p_email citext, p_draft jsonb, p_password_hash text, p_expires_at timestamp with time zone",
		"uuid", "tappa_identitydefiner"},
	{"identity_claim_signup", "p_claim_hash text, p_tenant_id uuid, p_session_hash text, p_verified_digest text", "TABLE(admin_user_id uuid)", "tappa_identitydefiner"},
	{"identity_sweep_signup_claims", "", "integer", "tappa_identitydefiner"},
}

// identitySignaturesM1416b is M14-16b's part (migration 00081; ADR 0031 §5.2's note of 2026-10-27, the
// user's decision): tappa_resolver's read of a business's owners who can still get a session -- ids
// only, for the caller's own tenant context (the ghost owner, backlog T171).
var identitySignaturesM1416b = []identitySignature{
	{"resolve_admin_owners_by_tenant", "p_tenant_id uuid", "TABLE(admin_user_id uuid)", "tappa_resolver"},
}

// identitySignaturesM1406b is M14-06b's part (migration 00082): the maintenance function, EXECUTE in
// tappa_owner alone (identityExecuteExceptions).
var identitySignaturesM1406b = []identitySignature{
	{"identity_reconcile", "", "integer", "tappa_identitydefiner"},
}

// identitySignaturesM1422 is M14-22's part (migration 00084; ADR 0031 §5.2's note of 2026-10-29, the
// user's decisions): the request's third argument, the link's purpose (DEFAULT 'verify' -- the binary
// before 00084 calls it with two; the identity arguments carry no default, as identity_claim_signup's
// do not), and the link by e-mail -- two secrets, the identity's address's rows.
var identitySignaturesM1422 = []identitySignature{
	{"identity_request_verification", "p_session_hash text, p_token_hash text, p_purpose text",
		"TABLE(verification_id uuid, expires_at timestamp with time zone)", "tappa_identitydefiner"},
	{"identity_link_by_email", "p_session_hash text, p_verification_hash text",
		"TABLE(linked integer, first_verification boolean, email text)", "tappa_identitydefiner"},
}

// identitySignatures is the union of every card's part: the whole closed list as far as it is built.
// The functions tappa_identitydefiner owns are EXACTLY its tappa_identitydefiner rows.
var identitySignatures = slices.Concat(identitySignaturesM1404, identitySignaturesM1405, identitySignaturesM1408, identitySignaturesM1415,
	identitySignaturesM1417, identitySignaturesM1418, identitySignaturesM1416b, identitySignaturesM1406b, identitySignaturesM1422)

// identitySignatureNames lists the table's names (resolve_admin_by_email is a row though neither
// identity_* nor the definer's -- M14-05's redefinition).
func identitySignatureNames() []string {
	names := make([]string, 0, len(identitySignatures))
	for _, s := range identitySignatures {
		names = append(names, s.name)
	}
	return names
}

// identityExecuteExceptions names the functions whose EXECUTE is NOT tappa_app's (ADR 0031 §5.2,
// ONAY note 1): identity_reconcile() -- M14-06b's maintenance function, EXECUTE in tappa_owner
// alone. Its signature row arrived with M14-06b (identitySignaturesM1406b, 00082). An EMPTY caller is
// a function no role but its owner may call: identity_create_for_signup since M14-19 (00083, S18) --
// no binary calls it, and it waits for contract B's DROP.
var identityExecuteExceptions = map[string]string{
	"identity_reconcile":         "tappa_owner",
	"identity_create_for_signup": "",
}

// identityCatalogFindings is the catalogue test over EVERY schema:
//
//	(i)   the functions tappa_identitydefiner owns are exactly sigs' tappa_identitydefiner rows, all
//	      in public; every row of sigs exists, in public, owned as it says;
//	(ii)  each one's argument and result text is the table's, verbatim;
//	(iii) one overload per name;
//	(iv)  every function named identity\_% or resolve\_identity\_% in any schema is a row of sigs;
//	(v)   prosecdef true, proconfig exactly the search path;
//	(vi)  EXECUTE (aclexplode over coalesce(proacl, acldefault) -- a NULL ACL is PUBLIC's EXECUTE)
//	      exactly {owner, tappa_app}, or {owner, <exception>} for a name identityExecuteExceptions holds
//	      ({owner} alone for an empty exception).
func identityCatalogFindings(ctx context.Context, q opQuerier, sigs []identitySignature) ([]string, error) {
	names := make([]string, 0, len(sigs))
	for _, s := range sigs {
		names = append(names, s.name)
	}
	rows, err := q.Query(ctx, `
		SELECT n.nspname, p.proname, pg_get_userbyid(p.proowner),
		       pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
		       coalesce(array_to_string(p.proconfig, '|'), ''), p.prosecdef,
		       coalesce((SELECT string_agg(CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, ','
		                                    ORDER BY CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END)
		                   FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) AS a
		                  WHERE a.privilege_type = 'EXECUTE'), ''),
		       count(*) OVER (PARTITION BY n.nspname, p.proname)
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE p.proowner = 'tappa_identitydefiner'::regrole
		    OR p.proname LIKE 'identity\_%' OR p.proname LIKE 'resolve\_identity\_%'
		    OR p.proname = ANY ($1)
		 ORDER BY 1, 2`, names)
	if err != nil {
		return nil, err
	}
	type fn struct {
		schema, name, owner, args, result, config, execute string
		secdef                                             bool
		overloads                                          int
	}
	fns, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (fn, error) {
		var f fn
		err := r.Scan(&f.schema, &f.name, &f.owner, &f.args, &f.result, &f.config, &f.secdef, &f.execute, &f.overloads)
		return f, err
	})
	if err != nil {
		return nil, err
	}
	want := map[string]identitySignature{}
	for _, s := range sigs {
		want["public."+s.name] = s
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range fns {
		q := f.schema + "." + f.name
		bad := func(format string, a ...any) { out = append(out, q+"("+f.args+"): "+fmt.Sprintf(format, a...)) }
		s, listed := want[q]
		if !listed {
			bad("(i)/(iv) owned by %s and not in ADR 0031 §5.2's signature table (a closed-list member needs the user's approval and the ADR)", f.owner)
			continue
		}
		seen[q] = true
		if f.owner != s.owner {
			bad("(i) owned by %s, the table says %s", f.owner, s.owner)
		}
		if f.overloads != 1 {
			bad("(iii) %d overloads; a second signature is a second door", f.overloads)
		}
		if f.args != s.args || f.result != s.result {
			bad("(ii) is (%s) -> %s, the table says (%s) -> %s", f.args, f.result, s.args, s.result)
		}
		if !f.secdef {
			bad("(v) not SECURITY DEFINER")
		}
		if f.config != "search_path=pg_catalog, pg_temp" {
			bad("(v) proconfig %q, want exactly search_path=pg_catalog, pg_temp", f.config)
		}
		caller := "tappa_app"
		if e, ok := identityExecuteExceptions[f.name]; ok {
			caller = e
		}
		wantExec := []string{f.owner}
		if caller != "" {
			wantExec = append(wantExec, caller)
		}
		slices.Sort(wantExec)
		if f.execute != strings.Join(wantExec, ",") {
			bad("(vi) EXECUTE is held by {%s}, want exactly {%s}", f.execute, strings.Join(wantExec, ","))
		}
	}
	for q := range want {
		if !seen[q] {
			out = append(out, q+": (i) in the signature table but not in the catalogue")
		}
	}
	sort.Strings(out)
	return out, nil
}

// TestIdentityCatalog_TheFunctionsAndTheirExactSignatures is the signature catalogue test (ADR 0031
// §5.2, ADR 0027 §2's method) with a positive control per item, each a mutant made in a savepoint
// that is rolled back: a result widened by one column, a function the table lacks, EXECUTE to
// tappa_operator and to PUBLIC, an identity_* of another schema and of another owner, a second
// overload, SECURITY INVOKER, a search path reaching public -- and identity_reconcile(): EXECUTE in
// tappa_owner alone passes once its row is in the table, EXECUTE to tappa_app is flagged.
func TestIdentityCatalog_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	findings, err := identityCatalogFindings(ctx, tx, identitySignatures)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	owned := 0
	for _, s := range identitySignatures {
		if s.owner == "tappa_identitydefiner" {
			owned++
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proowner = 'tappa_identitydefiner'::regrole`); n != int64(owned) || owned < 4 {
		t.Fatalf("anti-vacuity: tappa_identitydefiner owns %d functions, the table has %d", n, owned)
	}

	// M14-06b: identity_reconcile() is real (00082) and green above with EXECUTE in tappa_owner alone;
	// its controls act on it, and withoutReconcile is the table before its row arrived.
	withoutReconcile := slices.DeleteFunc(slices.Clone(identitySignatures), func(s identitySignature) bool { return s.name == "identity_reconcile" })
	controls := []struct {
		name  string
		sql   []string
		sigs  []identitySignature
		wantF string // "" = no finding
	}{
		{"identity_memberships returning the identity id", []string{
			`DROP FUNCTION public.identity_memberships(text)`,
			`CREATE FUNCTION public.identity_memberships(p_session_hash text)
			     RETURNS TABLE(tenant_id uuid, tenant_name text, role text, suspended boolean, is_current boolean, identity_id uuid)
			     LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
			     AS 'SELECT NULL::uuid, NULL::text, NULL::text, NULL::boolean, NULL::boolean, NULL::uuid'`,
			`ALTER FUNCTION public.identity_memberships(text) OWNER TO tappa_identitydefiner`,
			`REVOKE ALL ON FUNCTION public.identity_memberships(text) FROM PUBLIC`,
			`GRANT EXECUTE ON FUNCTION public.identity_memberships(text) TO tappa_app`}, identitySignatures, "(ii)"},
		{"a definer function not in the table", []string{
			`CREATE FUNCTION public.zz_identity_probe() RETURNS integer LANGUAGE sql SECURITY DEFINER
			     SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
			`ALTER FUNCTION public.zz_identity_probe() OWNER TO tappa_identitydefiner`}, identitySignatures, "(i)"},
		{"EXECUTE to tappa_operator", []string{
			`GRANT EXECUTE ON FUNCTION public.identity_switch_session(text, uuid, text) TO tappa_operator`}, identitySignatures, "(vi)"},
		{"EXECUTE to PUBLIC", []string{
			`GRANT EXECUTE ON FUNCTION public.resolve_identity_by_email(citext) TO PUBLIC`}, identitySignatures, "(vi)"},
		{"an identity_* in another schema", []string{
			`CREATE SCHEMA zz_identity_probe`,
			`CREATE FUNCTION zz_identity_probe.identity_memberships(p text) RETURNS integer LANGUAGE sql AS 'SELECT 1'`},
			identitySignatures, "(i)/(iv)"},
		{"an identity_* left with the migration role", []string{
			`CREATE FUNCTION public.identity_zz_forgotten() RETURNS boolean LANGUAGE sql SECURITY DEFINER AS 'SELECT true'`},
			identitySignatures, "(i)/(iv)"},
		{"a second overload", []string{
			`CREATE FUNCTION public.identity_memberships(p_session_hash text, p_more integer) RETURNS integer LANGUAGE sql AS 'SELECT 1'`,
			`ALTER FUNCTION public.identity_memberships(text, integer) OWNER TO tappa_identitydefiner`}, identitySignatures, "(iii)"},
		{"the resolver given to the definer", []string{
			`ALTER FUNCTION public.resolve_identity_by_email(citext) OWNER TO tappa_identitydefiner`}, identitySignatures, "(i) owned by tappa_identitydefiner"},
		{"SECURITY INVOKER", []string{
			`ALTER FUNCTION public.identity_open_session(uuid, uuid, text, text) SECURITY INVOKER`}, identitySignatures, "(v)"},
		{"a search path reaching public", []string{
			`ALTER FUNCTION public.identity_revoke_sessions(text, boolean) SET search_path = pg_catalog, public`}, identitySignatures, "(v)"},
		{"identity_reconcile, EXECUTE in tappa_owner alone (the named exception)", nil, identitySignatures, ""},
		{"identity_reconcile, EXECUTE to tappa_app", []string{
			`GRANT EXECUTE ON FUNCTION public.identity_reconcile() TO tappa_app`}, identitySignatures, "(vi)"},
		{"identity_reconcile, EXECUTE taken from tappa_owner", []string{
			`REVOKE EXECUTE ON FUNCTION public.identity_reconcile() FROM tappa_owner`}, identitySignatures, "(vi)"},
		{"identity_reconcile without its row in the table", nil, withoutReconcile, "(i)"},
		// M14-19 (00083): identity_create_for_signup's EXECUTE is its owner's alone; given back to
		// tappa_app (00083's Down, a later file's slip) it is flagged.
		{"identity_create_for_signup, EXECUTE in its owner alone (M14-19)", nil, identitySignatures, ""},
		{"identity_create_for_signup, EXECUTE given back to tappa_app", []string{isGrantSignupDoorBack}, identitySignatures, "(vi)"},
	}
	for _, c := range controls {
		t.Run("control/"+c.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				for _, s := range c.sql {
					aiExec(t, ctx, sp, s)
				}
				got, err := identityCatalogFindings(ctx, sp, c.sigs)
				if err != nil {
					t.Fatal(err)
				}
				if c.wantF == "" {
					if len(got) > 0 {
						t.Fatalf("a shape the ADR allows was flagged: %v", got)
					}
					return
				}
				if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, c.wantF) }) {
					t.Fatalf("the catalogue test did not flag it with %s; findings: %v", c.wantF, got)
				}
			})
		})
	}
}

// identityRolesThatMayNotExecute are the roles ADR 0031 §5.2 / §6 keeps off the closed list (and
// PUBLIC).
var identityRolesThatMayNotExecute = []string{"public", "tappa_operator", "tappa_opdefiner", "tappa_holdingdefiner", "tappa_feedwriter"}

// TestIdentityFunctions_NoOperatorRoleMayExecuteThem: has_function_privilege (which counts role
// membership and PUBLIC) is false for tappa_operator, tappa_opdefiner, the other definers and PUBLIC
// on every function of the list and every function named identity_* / resolve_identity_* -- and
// true for tappa_app on each of the list's but a named exception. CONTROL: EXECUTE handed to
// tappa_operator through a role it is a member of is flagged.
func TestIdentityFunctions_NoOperatorRoleMayExecuteThem(t *testing.T) {
	ctx, tx := opTx(t)
	scan := func(q opQuerier) []string {
		rows, err := q.Query(ctx, `
			SELECT p.proname, r.r, has_function_privilege(r.r, p.oid, 'EXECUTE')
			  FROM pg_proc p CROSS JOIN unnest($1::text[] || ARRAY['tappa_app']) AS r(r)
			 WHERE p.proowner = 'tappa_identitydefiner'::regrole
			    OR p.proname LIKE 'identity\_%' OR p.proname LIKE 'resolve\_identity\_%'
			    OR p.proname = ANY ($2)
			 ORDER BY 1, 2`, identityRolesThatMayNotExecute, identitySignatureNames())
		if err != nil {
			t.Fatal(err)
		}
		type cell struct {
			Fn, Role string
			May      bool
		}
		cells, err := pgx.CollectRows(rows, pgx.RowToStructByPos[cell])
		if err != nil {
			t.Fatal(err)
		}
		if len(cells) < len(identitySignatures)*(len(identityRolesThatMayNotExecute)+1) {
			t.Fatalf("anti-vacuity: %d cells", len(cells))
		}
		var out []string
		for _, c := range cells {
			_, exception := identityExecuteExceptions[c.Fn]
			want := c.Role == "tappa_app" && !exception
			if c.May != want {
				out = append(out, fmt.Sprintf("%s: %s may EXECUTE=%v, want %v", c.Fn, c.Role, c.May, want))
			}
		}
		return out
	}
	for _, f := range scan(tx) {
		t.Error(f)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `CREATE ROLE zz_identity_probe NOLOGIN`)
		aiExec(t, ctx, sp, `GRANT EXECUTE ON FUNCTION public.identity_memberships(text) TO zz_identity_probe`)
		aiExec(t, ctx, sp, `GRANT zz_identity_probe TO tappa_operator`)
		if got := scan(sp); !slices.ContainsFunc(got, func(f string) bool { return strings.HasPrefix(f, "identity_memberships: tappa_operator") }) {
			t.Errorf("CONTROL: EXECUTE through a membership was not flagged: %v", got)
		}
	})
}

// ------------------------------------------------------------- the definer's reach --

// identityADRMatrix is ADR 0031 §5.2's verb x column matrix for tappa_identitydefiner, as the ADR
// writes it -- the UPPER BOUND every card's grants stay inside. "*" = the ADR names the verb without
// a column list ("beş kimlik tablosunda gerektiği kadar"; admin_sessions SELECT; audit_log INSERT;
// admin_invites SELECT). The ADR's tenants `xmin` is a system column, outside what is read here.
var identityADRMatrix = map[string]string{
	"admin_identities:SELECT": "*", "admin_identities:INSERT": "*", "admin_identities:UPDATE": "*",
	"admin_identity_resets:SELECT": "*", "admin_identity_resets:INSERT": "*", "admin_identity_resets:UPDATE": "*",
	"admin_identity_verifications:SELECT": "*", "admin_identity_verifications:INSERT": "*", "admin_identity_verifications:UPDATE": "*",
	"admin_signup_claims:SELECT": "*", "admin_signup_claims:INSERT": "*", "admin_signup_claims:UPDATE": "*",
	"admin_identity_events:SELECT": "*", "admin_identity_events:INSERT": "*", "admin_identity_events:UPDATE": "*",
	"admin_users:SELECT":     "id,tenant_id,email,password_hash,role,status,identity_id,linked_via,linked_at,unlinked_at,created_at",
	"admin_users:INSERT":     "tenant_id,full_name,email,password_hash,role,identity_id,linked_via,linked_at",
	"admin_users:UPDATE":     "password_hash,identity_id,linked_via,linked_at,unlinked_at,last_login_at,status",
	"password_resets:SELECT": "id,tenant_id,admin_user_id,token_hash,expires_at,used_at,cancelled_at",
	"password_resets:UPDATE": "used_at,cancelled_at",
	"admin_sessions:SELECT":  "*",
	"admin_sessions:INSERT":  "tenant_id,admin_user_id,token_hash,authenticated_at",
	"admin_sessions:UPDATE":  "revoked_at",
	"tenants:SELECT":         "id,name,vat_number,suspended_at,created_at",
	"audit_log:INSERT":       "*",
	"admin_invites:SELECT":   "*",
	"admin_invites:UPDATE":   "used_at",
}

// identityDefinerMatrixM1404 is what 00071 grants tappa_identitydefiner: "table:VERB" -> the exact
// column set (order free). Every relation, verb and column not listed is "no privilege".
var identityDefinerMatrixM1404 = map[string]string{
	"admin_identities:SELECT": "id,password_hash,last_used_tenant",
	"admin_identities:UPDATE": "last_used_tenant,last_login_at",
	"admin_users:SELECT":      "id,tenant_id,role,status,created_at,identity_id,unlinked_at",
	"admin_users:UPDATE":      "last_login_at",
	"admin_sessions:SELECT":   "id,tenant_id,admin_user_id,token_hash,revoked_at,authenticated_at",
	"admin_sessions:INSERT":   "tenant_id,admin_user_id,token_hash,authenticated_at",
	"admin_sessions:UPDATE":   "revoked_at",
	"tenants:SELECT":          "id,name,suspended_at",
	"audit_log:INSERT":        "tenant_id,actor_id,action,target,detail",
}

// identityDefinerMatrixM1405 is what 00072 grants tappa_identitydefiner (its cells shared with 00071
// are unioned; 00072's Down revokes only what 00072 alone grants).
var identityDefinerMatrixM1405 = map[string]string{
	"admin_identities:SELECT":             "id,email,password_hash,email_verified_at,confirm_opened_at,confirm_open_until,confirm_scope",
	"admin_identities:UPDATE":             "password_hash,email_verified_at,confirm_opened_at,confirm_open_until,confirm_scope",
	"admin_identity_resets:SELECT":        "id,identity_id,token_hash,expires_at,used_at,cancelled_at",
	"admin_identity_resets:INSERT":        "identity_id,token_hash,expires_at",
	"admin_identity_resets:UPDATE":        "used_at,cancelled_at",
	"admin_identity_verifications:SELECT": "id,identity_id,token_hash,expires_at,used_at",
	"admin_identity_verifications:INSERT": "identity_id,token_hash,expires_at",
	"admin_identity_verifications:UPDATE": "used_at",
	"admin_identity_events:SELECT":        "created_at",
	"admin_identity_events:INSERT":        "identity_id,event,detail",
	"admin_users:SELECT":                  "id,tenant_id,email,status,identity_id,linked_via,linked_at,unlinked_at",
	"admin_users:UPDATE":                  "password_hash,status,identity_id,linked_via,linked_at,unlinked_at",
	"admin_sessions:SELECT":               "id,tenant_id,admin_user_id,token_hash,revoked_at,authenticated_at",
	"admin_sessions:UPDATE":               "revoked_at",
	"password_resets:SELECT":              "id,tenant_id,admin_user_id,token_hash,expires_at,used_at,cancelled_at",
	"password_resets:UPDATE":              "used_at,cancelled_at",
	"audit_log:INSERT":                    "tenant_id,actor_id,action,target,detail",
}

// identityDefinerMatrixM1408 is what 00074 grants tappa_identitydefiner for identity_create_for_signup:
// the identity's birth, the digest it is born with, and the business's birth instant (predicate 4).
// The row's xmin (predicate 3) is a SYSTEM column, outside opColumns' attnum > 0 -- identitySystemColumns
// pins it.
var identityDefinerMatrixM1408 = map[string]string{
	"admin_identities:INSERT": "email,password_hash",
	"admin_users:SELECT":      "password_hash",
	"tenants:SELECT":          "created_at",
}

// identityDefinerMatrixM1415 is what 00077 grants tappa_identitydefiner for the two acceptances: the
// invitation it reads and spends, and the membership row it births (admin_users INSERT -- the ADR
// matrix's eight columns). The refused row's relink uses 00072's UPDATE columns.
var identityDefinerMatrixM1415 = map[string]string{
	"admin_invites:SELECT": "id,tenant_id,email,full_name,role,token_hash,expires_at,used_at,cancelled_at",
	"admin_invites:UPDATE": "used_at",
	"admin_users:INSERT":   "tenant_id,full_name,email,password_hash,role,identity_id,linked_via,linked_at",
}

// identityDefinerMatrixM1417 is what 00078 grants tappa_identitydefiner for the verified registration:
// the draft table (its open, its claim and its sweep), the owner row identity_claim_signup writes (the
// ADR's admin_users INSERT cell, whole -- SHARED with M14-15's 00077, which owns it: 00078 restates the
// grant and its Down keeps it while an invite function exists), and the business's VAT number (predicate 5 -- its xmin, predicate
// 3, is a system column: identitySystemColumns).
var identityDefinerMatrixM1417 = map[string]string{
	"admin_signup_claims:SELECT": "id,token_hash,email,draft,password_hash,expires_at,used_at",
	"admin_signup_claims:INSERT": "token_hash,email,draft,password_hash,expires_at",
	"admin_signup_claims:UPDATE": "used_at",
	"admin_users:INSERT":         "tenant_id,full_name,email,password_hash,role,identity_id,linked_via,linked_at",
	"tenants:SELECT":             "vat_number",
}

// identitySystemColumns are the system columns tappa_identitydefiner may SELECT, "table.column": the
// whole list (ADR 0031 §5.2: admin_users' xmin for identity_create_for_signup -- M14-08; tenants' xmin
// for identity_claim_signup -- M14-17, S2/T7).
var identitySystemColumns = []string{"admin_users.xmin", "tenants.xmin"}

// identityDefinerTableVerbs are the table-level verbs the definer may hold, each with its one user:
// ADR 0031 §5.1 / R17's sweep (identitybelt_test.go's identityPersonTableDeleters names the same).
var identityDefinerTableVerbs = map[string]string{
	"admin_identity_events:DELETE": "identity_sweep_events -- the event log's 90-day sweep (ADR 0031 §5.1, R17)",
	"admin_signup_claims:DELETE":   "identity_sweep_signup_claims -- the drafts' sweep: expired or used (ADR 0031 §5.1, R17; M14-17)",
}

// identityDefinerMatrixM1406b is what identity_reconcile() (00082) reads and writes. 00082 grants
// nothing: admin_users SELECT (password_hash), the chosen row's digest, and admin_identities INSERT
// (email, password_hash), the identity's birth, are 00074's (identity_create_for_signup reads and
// writes the same two); the rest is 00071/00072's.
var identityDefinerMatrixM1406b = map[string]string{
	"admin_users:SELECT":      "id,tenant_id,email,password_hash,status,created_at,identity_id,unlinked_at",
	"admin_users:UPDATE":      "identity_id,linked_via,linked_at",
	"admin_identities:SELECT": "id,email",
	"admin_identities:INSERT": "email,password_hash",
}

// identityDefinerMatrixM1422 is what 00084 grants tappa_identitydefiner: the link's purpose, written by
// identity_request_verification and read by identity_link_by_email and identity_verify_email, and the
// link's created_at -- the candidates' bound (only rows born before the link). The link reads and writes nothing
// else that 00071/00072 did not grant (the identity's address, digest and window; the rows' link columns
// and digest; the sessions' revocation; the audit row; the email_verified event).
var identityDefinerMatrixM1422 = map[string]string{
	"admin_identity_verifications:SELECT": "purpose,created_at",
	"admin_identity_verifications:INSERT": "purpose",
}

// identityDefinerMatrixParts are the cards' matrices; identityDefinerMatrix is their union.
var identityDefinerMatrixParts = []map[string]string{identityDefinerMatrixM1404, identityDefinerMatrixM1405, identityDefinerMatrixM1408, identityDefinerMatrixM1415, identityDefinerMatrixM1417,
	identityDefinerMatrixM1406b, identityDefinerMatrixM1422}

// identityDefinerMatrix unions the parts, cell by cell, as sorted column sets.
func identityDefinerMatrix() map[string]string {
	sets := map[string][]string{}
	for _, part := range identityDefinerMatrixParts {
		for k, v := range part {
			sets[k] = append(sets[k], strings.Split(v, ",")...)
		}
	}
	out := map[string]string{}
	for k, cols := range sets {
		slices.Sort(cols)
		out[k] = strings.Join(slices.Compact(cols), ",")
	}
	return out
}

// identitySortedCols turns opColumns' attnum-ordered list into a sorted set.
func identitySortedCols(s string) string {
	if s == "" {
		return ""
	}
	cols := strings.Split(s, ",")
	slices.Sort(cols)
	return strings.Join(cols, ",")
}

// identityDefinerReachFindings: tappa_identitydefiner's privileges on every relation in public
// (SELECT/INSERT/UPDATE by column against matrix; DELETE/TRUNCATE/REFERENCES/TRIGGER never), on every
// sequence (none), on every schema but public (none; on public USAGE, not CREATE), and EXECUTE on
// functions it does not own (only PUBLIC's, and no SECURITY DEFINER).
func identityDefinerReachFindings(t *testing.T, ctx context.Context, q opQuerier, matrix map[string]string) []string {
	t.Helper()
	var out []string
	rels, err := q.Query(ctx, `SELECT c.relname FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace
	                             AND c.relkind IN ('r', 'p', 'v', 'm', 'f') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rels, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			if got, want := identitySortedCols(opColumns(t, ctx, q, "tappa_identitydefiner", table, priv)), matrix[table+":"+priv]; got != want {
				out = append(out, fmt.Sprintf("%s on %s = (%s), want (%s)", priv, table, got, want))
			}
		}
		// The system columns (M14-08): a grant on one is invisible to opColumns (attnum > 0).
		var system string
		if err := q.QueryRow(ctx, `SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attname), '') FROM pg_attribute a
		                            WHERE a.attrelid = ('public.' || quote_ident($1))::regclass AND a.attnum < 0
		                              AND has_column_privilege('tappa_identitydefiner', a.attrelid, a.attname, 'SELECT')`, table).Scan(&system); err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, c := range identitySystemColumns {
			if rel, col, _ := strings.Cut(c, "."); rel == table {
				want = append(want, col)
			}
		}
		slices.Sort(want)
		if system != strings.Join(want, ",") {
			out = append(out, fmt.Sprintf("SELECT on %s's system columns = (%s), identitySystemColumns names (%s)", table, system, strings.Join(want, ",")))
		}
		for _, priv := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			var may bool
			if err := q.QueryRow(ctx, `SELECT has_table_privilege('tappa_identitydefiner', 'public.' || quote_ident($1), $2)`, table, priv).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if _, named := identityDefinerTableVerbs[table+":"+priv]; may != named {
				out = append(out, fmt.Sprintf("%s on %s: held=%v, identityDefinerTableVerbs names it=%v", priv, table, may, named))
			}
		}
	}
	for _, check := range []struct{ what, sql string }{
		{"a sequence privilege", `SELECT s.relname FROM pg_class s WHERE s.relkind = 'S'
			AND (has_sequence_privilege('tappa_identitydefiner', s.oid, 'USAGE') OR has_sequence_privilege('tappa_identitydefiner', s.oid, 'SELECT')
			     OR has_sequence_privilege('tappa_identitydefiner', s.oid, 'UPDATE'))`},
		{"a schema privilege beyond USAGE on public", `SELECT n.nspname FROM pg_namespace n
			WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
			  AND (has_schema_privilege('tappa_identitydefiner', n.oid, 'CREATE')
			       OR (n.nspname <> 'public' AND has_schema_privilege('tappa_identitydefiner', n.oid, 'USAGE')))`},
		{"EXECUTE on another role's function PUBLIC may not call, or on a SECURITY DEFINER", `SELECT p.oid::regprocedure::text FROM pg_proc p
			WHERE p.proowner <> 'tappa_identitydefiner'::regrole
			  AND has_function_privilege('tappa_identitydefiner', p.oid, 'EXECUTE')
			  AND (p.prosecdef OR NOT has_function_privilege('public', p.oid, 'EXECUTE'))`},
	} {
		rows, err := q.Query(ctx, check.sql)
		if err != nil {
			t.Fatal(err)
		}
		hits, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hits {
			out = append(out, check.what+": "+h)
		}
	}
	return out
}

// TestIdentityDefiner_ColumnMatrix: tappa_identitydefiner's whole reach is the union of the cards'
// matrices, and every cell of that union lies inside ADR 0031 §5.2's matrix (no card may grant a
// verb or column the ADR does not name -- e.g. UPDATE (email), any DELETE). CONTROLS (savepoints): a
// column outside the matrix (admin_users.full_name), UPDATE (email), a table-level SELECT on
// admin_sessions, a DELETE, CREATE on public and EXECUTE on a resolver are each flagged.
func TestIdentityDefiner_ColumnMatrix(t *testing.T) {
	ctx, tx := opTx(t)
	matrix := identityDefinerMatrix()
	for cell, cols := range matrix {
		adr, ok := identityADRMatrix[cell]
		if !ok {
			t.Errorf("%s: the ADR's matrix has no such cell", cell)
			continue
		}
		if adr == "*" {
			continue
		}
		for _, c := range strings.Split(cols, ",") {
			if !slices.Contains(strings.Split(adr, ","), c) {
				t.Errorf("%s: %s is outside ADR 0031 §5.2's matrix (%s)", cell, c, adr)
			}
		}
	}
	for _, f := range identityDefinerReachFindings(t, ctx, tx, matrix) {
		t.Error(f)
	}
	for _, c := range []struct{ name, sql, want string }{
		{"a column outside the matrix", `GRANT SELECT (full_name) ON admin_users TO tappa_identitydefiner`, "SELECT on admin_users"},
		{"UPDATE (email)", `GRANT UPDATE (email) ON admin_users TO tappa_identitydefiner`, "UPDATE on admin_users"},
		{"a table-level SELECT on the sessions", `GRANT SELECT ON admin_sessions TO tappa_identitydefiner`, "SELECT on admin_sessions"},
		{"DELETE on the sessions", `GRANT DELETE ON admin_sessions TO tappa_identitydefiner`, "DELETE on admin_sessions"},
		{"CREATE on public", `GRANT CREATE ON SCHEMA public TO tappa_identitydefiner`, "schema privilege"},
		{"EXECUTE on a resolver", `GRANT EXECUTE ON FUNCTION public.resolve_admin_session_by_token_hash(text) TO tappa_identitydefiner`, "resolve_admin_session_by_token_hash"},
		// M14-08 / M14-17: a system column outside identitySystemColumns (tenants' xmin is granted since 00078;
		// locations' is nobody's).
		{"locations' xmin", `GRANT SELECT (xmin) ON locations TO tappa_identitydefiner`, "locations's system columns"},
	} {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			aiExec(t, ctx, sp, c.sql)
			if got := identityDefinerReachFindings(t, ctx, sp, matrix); !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, c.want) }) {
				t.Errorf("CONTROL %s: not flagged (%v)", c.name, got)
			}
		})
	}
}

// identityResolverColumns is tappa_resolver's reach on the identity columns (ADR 0031 §5.2, T6): what
// resolve_identity_by_email returns and filters on. M14-05 adds its token tables' columns here.
var identityResolverColumns = map[string]string{
	"admin_identities:SELECT": "id,email,password_hash,email_verified_at",
	// M14-16b (00081): + role -- resolve_admin_owners_by_tenant's owner filter.
	"admin_users:SELECT": "id,tenant_id,email,password_hash,role,status,created_at,identity_id,unlinked_at",
	// M14-05 (00072): the two token resolvers' columns.
	"admin_identity_resets:SELECT":        "id,identity_id,token_hash,expires_at,used_at,cancelled_at",
	"admin_identity_verifications:SELECT": "id,identity_id,token_hash,expires_at,used_at",
	// M14-15 (00077): what resolve_admin_invite_by_token_hash returns and filters on.
	"admin_invites:SELECT": "id,tenant_id,role,token_hash,expires_at,used_at,cancelled_at",
	// M14-17 (00078): the registration draft's resolver -- the key and the six columns it returns.
	"admin_signup_claims:SELECT": "id,token_hash,email,draft,password_hash,expires_at,used_at",
}

// TestIdentityResolver_ReachOnTheIdentityColumns pins tappa_resolver's SELECT on admin_identities
// and admin_users (00011/00017's six and 00071's two) and nothing written on either.
func TestIdentityResolver_ReachOnTheIdentityColumns(t *testing.T) {
	ctx, tx := opTx(t)
	for _, table := range []string{"admin_identities", "admin_users", "admin_identity_resets", "admin_identity_verifications", "admin_invites", "admin_signup_claims"} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			if got, want := opColumns(t, ctx, tx, "tappa_resolver", table, priv), identityResolverColumns[table+":"+priv]; got != want {
				t.Errorf("tappa_resolver %s on %s = (%s), want (%s)", priv, table, got, want)
			}
		}
	}
}

// ------------------------------------------------- every grant is used --

// identityStep is one call of the exercise: an owner statement first (a world change), then the call
// as tappa_app, which must answer at least one row.
type identityStep struct {
	name  string
	owner string // run as the owner first (a world change between calls)
	sql   string
	args  []any
	as    string // the calling role; "" = tappa_app (identity_reconcile: tappa_owner, its one caller)
}

// identityExerciseM1405 builds M14-05's world (an identity of its own: rows linked in C and D, an
// unlinked row of its address in E) and returns its calls, in an order where each answers a row:
// the digest, the password, the e-mail verification, the identity reset link (issued, kept, read,
// withdrawn, issued again, spent), the per-row link, unlink / relink / link, the failed login, the
// sweep, and the unlink's post-contract form (a stand-in for contract B's CHECK) -- every 00072
// column grant and the events DELETE on a path that runs. Sessions a freshness test reads are born
// right before their call, at clock_timestamp() (the transaction's now() ages over a long test).
func identityExerciseM1405(t *testing.T, ctx context.Context, sp pgx.Tx) []identityStep {
	t.Helper()
	c, d, e := idTenant(t, ctx, sp, "exercise C"), idTenant(t, ctx, sp, "exercise D"), idTenant(t, ctx, sp, "exercise E")
	email := aiEmail()
	ident := aiIdentity(t, ctx, sp, email)
	pc, pd, pe := aiAdmin(t, ctx, sp, c, email), aiAdmin(t, ctx, sp, d, email), aiAdmin(t, ctx, sp, e, email)
	aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() - interval '2 days',
	                           password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1`, pc, ident)
	aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'password', linked_at = now() - interval '2 days',
	                           password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1`, pd, ident)
	sc := idSession(t, ctx, sp, c, pc, nil)
	hV, hR, hR2, hR3, link, sFresh, sLegacy := opRandHex(t), opRandHex(t), opRandHex(t), opRandHex(t), opRandHex(t), opRandHex(t), opRandHex(t)
	var moment string
	if err := sp.QueryRow(ctx, `SELECT now()::text`).Scan(&moment); err != nil {
		t.Fatal(err)
	}
	session := func(tenant, admin uuid.UUID, hash string) string {
		return `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, authenticated_at) VALUES ('` + tenant.String() + `', '` + admin.String() + `', '` + hash + `', clock_timestamp())`
	}
	return []identityStep{
		{name: "M14-05 password for session", sql: `SELECT * FROM identity_password_for_session($1)`, args: []any{sc}},
		{name: "M14-05 set password", sql: `SELECT * FROM identity_set_password($1, $2)`, args: []any{sc, opFakeDigest("g")}},
		{name: "M14-05 request verification", sql: `SELECT * FROM identity_request_verification($1, $2)`, args: []any{sc, hV}},
		{name: "M14-05 resolve verification", sql: `SELECT * FROM resolve_identity_verification_by_token_hash($1)`, args: []any{hV}},
		{name: "M14-05 verify e-mail", sql: `SELECT * FROM identity_verify_email($1, $2)`, args: []any{hV, sc}},
		{name: "M14-05 open reset", sql: `SELECT * FROM identity_open_reset($1, $2, now() + interval '1 hour')`, args: []any{ident, hR}},
		{name: "M14-05 open reset, live", sql: `SELECT * FROM identity_open_reset($1, $2, now() + interval '1 hour')`, args: []any{ident, hR2}},
		{name: "M14-05 resolve reset", sql: `SELECT * FROM resolve_identity_reset_by_token_hash($1)`, args: []any{hR}},
		{name: "M14-05 withdraw reset", sql: `SELECT identity_withdraw_reset($1)`, args: []any{hR}},
		{name: "M14-05 open reset again", sql: `SELECT * FROM identity_open_reset($1, $2, now() + interval '1 hour')`, args: []any{ident, hR3}},
		{name: "M14-05 consume reset", sql: `SELECT * FROM identity_consume_reset($1, $2)`, args: []any{hR3, opFakeDigest("h")}},
		{name: "M14-05 consume row reset",
			owner: `INSERT INTO password_resets (tenant_id, admin_user_id, token_hash, expires_at) VALUES ('` + d.String() + `', '` + pd.String() + `', '` + link + `', now() + interval '1 hour')`,
			sql:   `SELECT * FROM identity_consume_row_reset($1, $2)`, args: []any{link, opFakeDigest("i")}},
		{name: "M14-05 unlink", owner: session(c, pc, sFresh), sql: `SELECT * FROM identity_unlink_backfilled($1, $2)`, args: []any{sFresh, d}},
		{name: "M14-05 relink", sql: `SELECT * FROM identity_relink_refused($1, $2)`, args: []any{sFresh, d}},
		{name: "M14-05 link legacy", owner: session(e, pe, sLegacy), sql: `SELECT * FROM identity_link_legacy($1, $2)`, args: []any{sFresh, sLegacy}},
		{name: "M14-05 failed login", sql: `SELECT * FROM identity_record_failed_login($1, 'login_failed')`, args: []any{ident}},
		{name: "M14-05 sweep",
			owner: fmt.Sprintf(`INSERT INTO admin_identity_events (identity_id, event, created_at) VALUES ('%s', 'login_failed', now() - interval '91 days')`, ident),
			sql:   `SELECT * FROM identity_sweep_events()`},
		{name: "M14-05 unlink after contract B",
			owner: `ALTER TABLE admin_users ADD CONSTRAINT zz_m14_05_contract_b CHECK (identity_id IS NOT NULL OR created_at < '` + moment + `'::timestamptz) NOT VALID`,
			sql:   `SELECT * FROM identity_unlink_backfilled($1, $2)`, args: []any{sFresh, d}},
	}
}

// identityExerciseM1418 builds M14-18's world (an identity of its own, a backfilled membership in F, a
// first_verify window open) and returns its calls: the window read (the header, the listed row and its
// last-owner test), then "Keep all" -- every column 00079's two bodies read or write is reached.
func identityExerciseM1418(t *testing.T, ctx context.Context, sp pgx.Tx) []identityStep {
	t.Helper()
	f, g := idTenant(t, ctx, sp, "exercise F"), idTenant(t, ctx, sp, "exercise G")
	email := aiEmail()
	ident := aiIdentity(t, ctx, sp, email)
	pf, pg := aiAdmin(t, ctx, sp, f, email), aiAdmin(t, ctx, sp, g, email)
	aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() - interval '2 days',
	                           password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1`, pf, ident)
	aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, pg)
	aiExec(t, ctx, sp, `UPDATE admin_identities SET email_verified_at = now(), confirm_opened_at = now() - interval '1 minute',
	                           confirm_open_until = now() + interval '7 days', confirm_scope = 'first_verify' WHERE id = $1`, ident)
	sf := idSession(t, ctx, sp, f, pf, nil)
	return []identityStep{
		{name: "M14-18 confirm window", sql: confirmWindowSQL, args: []any{sf}},
		{name: "M14-18 keep all", sql: keepAllSQL, args: []any{sf}},
	}
}

// identityExerciseM1415 is M14-15's world (00077): an identity with an UNVERIFIED address and a live
// session in business F, an open invitation of its address into business G (a new membership: the
// INSERT, the mailbox proof), one into business H where the address's row is REFUSED (the relink), and
// an invitation of a NEW address into G (identity_accept_invite_new: the identity's birth). Each
// invitation is read by the resolver first. Every 00077 column grant and the resolver's run.
func identityExerciseM1415(t *testing.T, ctx context.Context, sp pgx.Tx) []identityStep {
	t.Helper()
	f, g, h := idTenant(t, ctx, sp, "exercise F"), idTenant(t, ctx, sp, "exercise G"), idTenant(t, ctx, sp, "exercise H")
	email := aiEmail()
	ident := aiIdentity(t, ctx, sp, email)
	pf := aiAdmin(t, ctx, sp, f, email)
	aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now(),
	                           password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1`, pf, ident)
	session := idSession(t, ctx, sp, f, pf, nil)
	refused := aiAdmin(t, ctx, sp, h, email)
	// A refused MANAGER row: a manager invitation relinks only a row of its own role (00077).
	aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now(), role = 'manager' WHERE id = $1`, refused)
	ownerG, ownerH := aiAdmin(t, ctx, sp, g, aiEmail()), aiAdmin(t, ctx, sp, h, aiEmail())
	toG, toH, toNew := opRandHex(t), opRandHex(t), opRandHex(t)
	invite := func(tenant, by uuid.UUID, address, hash string) string {
		return fmt.Sprintf(`INSERT INTO admin_invites (tenant_id, email, full_name, role, token_hash, invited_by, expires_at)
		                    VALUES ('%s', '%s', 'M14 Invitee', 'manager', '%s', '%s', now() + interval '1 day')`, tenant, address, hash, by)
	}
	return []identityStep{
		{name: "M14-15 resolve invitation", owner: invite(g, ownerG, email, toG),
			sql: `SELECT * FROM resolve_admin_invite_by_token_hash($1)`, args: []any{toG}},
		{name: "M14-15 accept with a session (new row)", sql: `SELECT * FROM identity_accept_invite($1, $2)`, args: []any{toG, session}},
		{name: "M14-15 accept with a session (the refused row)", owner: invite(h, ownerH, email, toH),
			sql: `SELECT * FROM identity_accept_invite($1, $2)`, args: []any{toH, session}},
		{name: "M14-15 accept with a new password", owner: invite(g, ownerG, aiEmail(), toNew),
			sql: `SELECT * FROM identity_accept_invite_new($1, $2)`, args: []any{toNew, opFakeDigest("n")}},
	}
}

// identitySignupWorld is identity_create_for_signup's world (M14-08): a business and its one owner
// row, written by the test's TOP-LEVEL transaction -- the function links only a row this transaction
// wrote, and a row written inside a savepoint carries the subtransaction's xid (S13). Every exercise
// runs in a savepoint over it, so the link it makes is rolled back with that savepoint.
//
// M14-17: two more businesses born at the top level with NO membership -- what identity_claim_signup
// claims (predicates 1-3) -- one for a new address and one for an existing identity's session; M14-17b:
// a third for an existing identity with no active membership, claimed with its password (00080).
type identitySignupWorld struct {
	tenant, admin               uuid.UUID
	claimNew, claimOld, claimPw idClaimTenant
}

func newIdentitySignupWorld(t *testing.T, ctx context.Context, tx pgx.Tx) identitySignupWorld {
	t.Helper()
	w := identitySignupWorld{tenant: idTenant(t, ctx, tx, "exercise signup")}
	w.admin = aiAdmin(t, ctx, tx, w.tenant, aiEmail())
	w.claimNew = newIdClaimTenant(t, ctx, tx, "exercise claim new")
	w.claimOld = newIdClaimTenant(t, ctx, tx, "exercise claim known")
	w.claimPw = newIdClaimTenant(t, ctx, tx, "exercise claim password")
	return w
}

// idClaimTenant is a business written by the test's transaction with no membership, and the draft that
// names it (identity_claim_signup's predicate 5).
type idClaimTenant struct {
	id        uuid.UUID
	name, vat string
}

func newIdClaimTenant(t *testing.T, ctx context.Context, tx pgx.Tx, label string) idClaimTenant {
	t.Helper()
	c := idClaimTenant{id: idTenant(t, ctx, tx, label)}
	if err := tx.QueryRow(ctx, `SELECT name, vat_number FROM tenants WHERE id = $1`, c.id).Scan(&c.name, &c.vat); err != nil {
		t.Fatal(err)
	}
	return c
}

// draft is the closed-key registration draft (00078's CHECK) that names this business.
func (c idClaimTenant) draft() string {
	return fmt.Sprintf(`{"full_name": "M14 Owner", "business_name": %q, "vat_number": %q, "business_type": "restaurant", `+
		`"structure": "single", "vat_check": "unknown", "venues": ["Front door"]}`, c.name, c.vat)
}

// idOpenClaimSQL opens a draft with an hour to live and answers its id, 0 rows when refused.
const idOpenClaimSQL = `SELECT c FROM identity_open_signup_claim($1, $2::citext, $3::jsonb, $4, now() + interval '1 hour') AS c WHERE c IS NOT NULL`

// identityExercise runs every function of the list through every branch that reads a column, as
// tappa_app (identity_reconcile as tappa_owner, its one caller), on a world it builds in sp (as the
// owner). It returns the first error. With the full grants it succeeds; with any one column of the
// matrix revoked some call is refused (42501).
func identityExercise(t *testing.T, ctx context.Context, sp pgx.Tx, signup identitySignupWorld) error {
	t.Helper()
	a, b := idTenant(t, ctx, sp, "exercise A"), idTenant(t, ctx, sp, "exercise B")
	email := aiEmail()
	ident := aiIdentity(t, ctx, sp, email)
	ma, mb := aiAdmin(t, ctx, sp, a, email), aiAdmin(t, ctx, sp, b, email)
	aiLink(t, ctx, sp, ma, ident)
	aiLink(t, ctx, sp, mb, ident)
	plain := aiAdmin(t, ctx, sp, a, aiEmail())
	plainHash := idSession(t, ctx, sp, a, plain, nil)
	h1, h2, h3, h4 := opRandHex(t), opRandHex(t), opRandHex(t), opRandHex(t)

	steps := []identityStep{
		// M14-08 (00074) first: its row was written by the TOP-LEVEL transaction (identitySignupWorld) and
		// must still be the business's only one. `WHERE` turns its false (a refusal) into 0 rows.
		// M14-19 (00083) took tappa_app's EXECUTE; the definer cells the body uses stay until contract B,
		// so the step gives the grant back inside this savepoint alone.
		{name: "M14-08 create for signup",
			owner: `SELECT set_config('app.tenant_id', '` + signup.tenant.String() + `', true); ` + isGrantSignupDoorBack,
			sql:   `SELECT 1 WHERE ` + strings.TrimPrefix(createIdentityForSignupSQL, "SELECT "), args: []any{signup.tenant, signup.admin}},
		{name: "resolve", sql: resolveIdentityByEmailSQL, args: []any{email}},
		{name: "open, requested", sql: openIdentitySessionSQL, args: []any{ident, a, h1, opFakeDigest("a")}},
		{name: "open, last used", sql: openIdentitySessionSQL, args: []any{ident, nil, h2, opFakeDigest("a")}},
		{name: "open, oldest", owner: `UPDATE admin_identities SET last_used_tenant = NULL WHERE id = '` + ident.String() + `'`,
			sql: openIdentitySessionSQL, args: []any{ident, nil, h3, opFakeDigest("a")}},
		{name: "memberships", sql: identityMembershipsSQL, args: []any{h1}},
		{name: "memberships, unlinked", sql: identityMembershipsSQL, args: []any{plainHash}},
		{name: "switch", sql: switchIdentitySessionSQL, args: []any{h1, b, h4}},
		{name: "revoke", sql: revokeIdentitySessionsSQL, args: []any{h4, false}},
	}
	steps = append(steps, identityExerciseM1405(t, ctx, sp)...)
	steps = append(steps, identityExerciseM1415(t, ctx, sp)...)
	// M14-17 (00078): a draft for a new address opened, read and claimed; a draft for the identity above
	// claimed through a fresh session of its linked row in A (its email_verified_at is NULL, so the first
	// verification's branch runs); the sweep takes both used drafts.
	hNew, hOld, hSession, hPw := opRandHex(t), opRandHex(t), opRandHex(t), opRandHex(t)
	// M14-17b (00080): an identity with no membership at all (unverified, so the mailbox proof's branch
	// runs), its draft claimed with its digest.
	emailPw := aiEmail()
	aiIdentity(t, ctx, sp, emailPw)
	steps = append(steps,
		identityStep{name: "M14-17 open claim, new", sql: idOpenClaimSQL, args: []any{hNew, aiEmail(), signup.claimNew.draft(), opFakeDigest("n")}},
		identityStep{name: "M14-17 resolve claim", sql: resolveSignupClaimSQL, args: []any{hNew}},
		identityStep{name: "M14-17 claim, new",
			owner: `SELECT set_config('app.tenant_id', '` + signup.claimNew.id.String() + `', true)`,
			sql:   claimSignupSQL, args: []any{hNew, signup.claimNew.id, nil, nil}},
		identityStep{name: "M14-17 open claim, known", sql: idOpenClaimSQL, args: []any{hOld, email, signup.claimOld.draft(), opFakeDigest("o")}},
		identityStep{name: "M14-17 claim, known",
			owner: `SELECT set_config('app.tenant_id', '` + signup.claimOld.id.String() + `', true); ` +
				`INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ('` + a.String() + `', '` + ma.String() + `', '` + hSession + `')`,
			sql: claimSignupSQL, args: []any{hOld, signup.claimOld.id, hSession, nil}},
		identityStep{name: "M14-17b open claim, no membership", sql: idOpenClaimSQL, args: []any{hPw, emailPw, signup.claimPw.draft(), opFakeDigest("p")}},
		identityStep{name: "M14-17b claim, the identity's password",
			owner: `SELECT set_config('app.tenant_id', '` + signup.claimPw.id.String() + `', true)`,
			sql:   claimSignupSQL, args: []any{hPw, signup.claimPw.id, nil, opFakeDigest("a")}},
		identityStep{name: "M14-17 sweep", sql: sweepSignupClaimsSQL},
	)
	steps = append(steps, identityExerciseM1418(t, ctx, sp)...)
	steps = append(steps, identityExerciseM1422(t, ctx, sp)...)
	// M14-16b (00081): business A's owners who can sign in, in A's own context (plain is its owner).
	steps = append(steps, identityStep{name: "M14-16b owners who can sign in",
		owner: `SELECT set_config('app.tenant_id', '` + a.String() + `', true)`,
		sql:   adminOwnersByTenantSQL, args: []any{a}})
	// M14-06b: the reconciliation, last (it links every candidate group the savepoint sees), as its one
	// caller; a fresh group of two rows is its candidate, so every statement of the body runs.
	recMail := aiEmail()
	aiAdmin(t, ctx, sp, idTenant(t, ctx, sp, "exercise reconcile 1"), recMail)
	aiAdmin(t, ctx, sp, idTenant(t, ctx, sp, "exercise reconcile 2"), strings.ToUpper(recMail))
	steps = append(steps, identityStep{name: "M14-06b reconcile", sql: `SELECT n FROM public.identity_reconcile() AS n WHERE n > 0`, as: "tappa_owner"})
	for _, s := range steps {
		if s.owner != "" {
			aiExec(t, ctx, sp, s.owner)
		}
		as := s.as
		if as == "" {
			as = "tappa_app"
		}
		err := opAs(t, ctx, sp, as, func(app pgx.Tx) error {
			rows, err := app.Query(ctx, s.sql, s.args...)
			if err != nil {
				return err
			}
			n := 0
			for rows.Next() {
				n++
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("%s: 0 rows", s.name)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}
	return nil
}

// TestIdentityDefiner_EveryGrantIsUsed: the exercise succeeds on the full grants (positive control);
// then each column of the definer's matrix and each identity column of the resolver is revoked, alone,
// in a savepoint, and the exercise must be refused with 42501 -- a grant no statement needs would
// stay green and is flagged. ADR 0031 §5.2: the matrix grants "gerektiği kadar", no more.
//
// M14-06b: READ COMMITTED (bfTx), not opTx's REPEATABLE READ -- the exercise now ends with
// identity_reconcile(), which locks every committed candidate row of the shared database FOR UPDATE,
// and under REPEATABLE READ a row another package updated after this transaction's snapshot answers
// 40001 (adminbackfill_test.go's reason, the same statement shape).
func TestIdentityDefiner_EveryGrantIsUsed(t *testing.T) {
	ctx, tx := bfTx(t)
	signup := newIdentitySignupWorld(t, ctx, tx)
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if err := identityExercise(t, ctx, sp, signup); err != nil {
			t.Fatalf("positive control: the exercise failed on the full grants: %v", err)
		}
	})
	type grant struct{ role, table, verb, col string }
	var grants []grant
	for cell, cols := range identityDefinerMatrix() {
		table, verb, _ := strings.Cut(cell, ":")
		for _, c := range strings.Split(cols, ",") {
			grants = append(grants, grant{"tappa_identitydefiner", table, verb, c})
		}
	}
	// The system columns the definer reads (M14-08: admin_users' xmin).
	for _, c := range identitySystemColumns {
		table, col, _ := strings.Cut(c, ".")
		grants = append(grants, grant{"tappa_identitydefiner", table, "SELECT", col})
	}
	// The resolver's two NEW admin_users columns and its four admin_identities columns (00071); role
	// (00081, M14-16b): resolve_admin_owners_by_tenant's filter.
	for _, c := range []string{"identity_id", "unlinked_at", "role"} {
		grants = append(grants, grant{"tappa_resolver", "admin_users", "SELECT", c})
	}
	for _, table := range []string{"admin_identities", "admin_identity_resets", "admin_identity_verifications", "admin_invites", "admin_signup_claims"} {
		for _, c := range strings.Split(identityResolverColumns[table+":SELECT"], ",") {
			grants = append(grants, grant{"tappa_resolver", table, "SELECT", c})
		}
	}
	slices.SortFunc(grants, func(x, y grant) int {
		return strings.Compare(x.role+x.table+x.verb+x.col, y.role+y.table+y.verb+y.col)
	})
	if len(grants) < 25 {
		t.Fatalf("anti-vacuity: %d grants", len(grants))
	}
	for _, g := range grants {
		t.Run(fmt.Sprintf("%s %s(%s) on %s", g.role, g.verb, g.col, g.table), func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				aiExec(t, ctx, sp, fmt.Sprintf("REVOKE %s (%s) ON %s FROM %s", g.verb, g.col, g.table, g.role))
				err := identityExercise(t, ctx, sp, signup)
				if err == nil {
					t.Fatalf("every call succeeded without it: the grant is not used (ADR 0031 §5.2: no more than needed)")
				}
				if code, _ := opCode(err); code != sqlstateInsufficientPrivi {
					t.Fatalf("refused, but not by the privilege (want 42501): %v", err)
				}
			})
		})
	}
}

// The belt's text scan (every tenant-table reference in a BYPASSRLS body names its tenant) and the
// identity-table DELETE scan live in identitybelt_test.go.

// identityLineComment drops `--` comments from a generated statement before the query-text scans.
var identityLineComment = regexp.MustCompile(`--[^\n]*`)

// ------------------------------------------------------- query-text scans (R16) --

// identityStatements returns every generated statement (internal/store, sqlc's `-- name:` constants)
// by name, comments dropped.
func identityStatements(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "store", "*.sql.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("glob internal/store: %v (%d files)", err, len(files))
	}
	name := regexp.MustCompile(`^-- name: (\w+) :`)
	all := map[string]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, v := range spec.Values {
				if s, ok := aiConstString(v); ok {
					if m := name.FindStringSubmatch(s); m != nil {
						all[m[1]] = identityLineComment.ReplaceAllString(s, "")
					}
				}
			}
			return true
		})
	}
	return all
}

var (
	identityStar     = regexp.MustCompile(`(?i)\b(SELECT|RETURNING)\s+(DISTINCT\s+)?(\w+\.)?\*|,\s*(\w+\.)?\*`)
	identityIDColumn = regexp.MustCompile(`(?i)\bidentity_id\b`)
	identityHidden   = regexp.MustCompile(`(?i)\b(linked_via|linked_at|unlinked_at)\b`)
)

// identityQueryFindings: no statement but TouchAdminSession names identity_id; none names
// linked_via, linked_at or unlinked_at; none that names admin_users projects `*` or `<alias>.*`.
func identityQueryFindings(statements map[string]string) []string {
	var out []string
	for name, s := range statements {
		if identityIDColumn.MatchString(s) && name != "TouchAdminSession" {
			out = append(out, name+": names identity_id (only TouchAdminSession may -- ADR 0031 §5.1, R16)")
		}
		if identityHidden.MatchString(s) {
			out = append(out, name+": names a link column tappa_app may not read (R16)")
		}
		if strings.Contains(s, "admin_users") && identityStar.MatchString(s) {
			out = append(out, name+": a `*` projection over a statement on admin_users")
		}
	}
	sort.Strings(out)
	return out
}

// TestStoreQueries_IdentityColumnsStayOutOfTheApplication is R16's query-text scan over every
// generated statement, with TouchAdminSession's identity_id as the one allowed hit (positive: it is
// there). CONTROLS on synthetic statements: identity_id elsewhere, linked_via, `SELECT *` and
// `a.*` over admin_users are flagged; `count(*)` is not.
func TestStoreQueries_IdentityColumnsStayOutOfTheApplication(t *testing.T) {
	statements := identityStatements(t)
	if len(statements) < 100 {
		t.Fatalf("anti-vacuity: %d generated statements", len(statements))
	}
	if !identityIDColumn.MatchString(statements["TouchAdminSession"]) || !strings.Contains(statements["TouchAdminSession"], "t.name AS tenant_name") {
		t.Fatalf("TouchAdminSession does not return identity_id and the tenant's name (ADR 0031 §5.3)")
	}
	for _, f := range identityQueryFindings(statements) {
		t.Error(f)
	}
	for _, c := range []struct{ name, sql string }{
		{"identity_id elsewhere", `SELECT a.identity_id FROM admin_users a WHERE a.tenant_id = $1`},
		{"linked_via", `SELECT a.linked_via FROM admin_users a WHERE a.tenant_id = $1`},
		{"star", `SELECT * FROM admin_users WHERE tenant_id = $1`},
		{"alias star", `SELECT s.id, a.* FROM admin_sessions s JOIN admin_users a ON a.id = s.admin_user_id`},
		{"returning star", `UPDATE admin_users SET status = 'disabled' WHERE id = $1 RETURNING *`},
	} {
		if got := identityQueryFindings(map[string]string{"Probe": c.sql}); len(got) == 0 {
			t.Errorf("CONTROL %s: not flagged", c.name)
		}
	}
	if got := identityQueryFindings(map[string]string{"Probe": `SELECT count(*) FROM admin_users WHERE tenant_id = $1`}); len(got) != 0 {
		t.Errorf("CONTROL count(*): flagged %v", got)
	}
}

// ---------------------------------------------------------------- fixtures --

// identityAt00070 takes the test's transaction back to 00070 by running 00071's Down in it (rolled
// back with the test): M14-03's tests pin the catalogue as 00069/00070 left it. 00071's Down is
// GRANT / REVOKE / DROP FUNCTION alone -- no table lock (downTables finds none).
//
// M14-05: every migration above 00070, newest first (00072's Down before 00071's), the tables their
// Downs lock taken first in one statement (lockForDowns); 00071's own Down still locks nothing.
func identityAt00070(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	_, down := opMigrationSections(t, identity00071File)
	if tables := downTables(down); len(tables) > 0 {
		t.Fatalf("PREMISE: 00071's Down locks %v", tables)
	}
	later := aiLaterMigrations(t)
	var tables []string
	for _, f := range later {
		tables = append(tables, downTables(f.down)...)
	}
	if len(tables) > 0 {
		slices.Sort(tables)
		lockForDowns(t, ctx, tx, slices.Compact(tables))
	}
	for i := len(later) - 1; i >= 0; i-- {
		opRunSection(t, ctx, tx, later[i].down, later[i].name+" Down")
	}
}

const identity00071File = "00071_resolve_identities_and_switch_sessions.sql"

// idTenant inserts a tenant with a recognisable, unique name (the audit scans look for it).
func idTenant(t *testing.T, ctx context.Context, q pgx.Tx, label string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	aiExec(t, ctx, q, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
	                   VALUES ($1, $2, $3, 'restaurant', 'single')`, id, "M14 "+label+" "+id.String()[:8], "VAT-M14S-"+id.String())
	return id
}

// idSession inserts a live session as the owner and returns its token hash; authAt nil leaves
// authenticated_at to its DEFAULT.
func idSession(t *testing.T, ctx context.Context, q pgx.Tx, tenant, admin uuid.UUID, authAt any) string {
	t.Helper()
	h := opRandHex(t)
	if authAt == nil {
		aiExec(t, ctx, q, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`, tenant, admin, h)
	} else {
		aiExec(t, ctx, q, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash, authenticated_at) VALUES ($1, $2, $3, $4)`, tenant, admin, h, authAt)
	}
	return h
}
