package db

// holdingcatalog_test.go -- M11 API-1a: the catalogue half of ADR 0027 §2 and §3 (the closed
// holding_* list, holding_peer, the holding triggers, tappa_holdingdefiner's verb x column
// matrix and its "never SELECT" list) against real Postgres, owner session, everything rolled
// back (opTx).
//
// 🔴 THIS FILE IS A SKELETON THE LATER TASKS FILL, NOT A LIST THEY REWRITE. API-1a brought two of
// the closed list's nine members (holding_peer, holding_timezone_lock); API-1b four more
// (holding_tap_site, holding_advance_tag_counter, holding_logo, holding_site_gate -- 00043).
// API-1c and API-2 add their functions, triggers and grants IN THE SAME CHANGE as their
// migration, by adding:
//   - a row to holdingSignatures (the ADR's signature table, verbatim) -- the catalogue,
//     EXECUTE, binding and canary tests read it;
//   - for a callable function, a row to holdingCanaryCalls (how to call it in a peer's context)
//     and, when it returns many rows, to holdingMultiRowCalls;
//   - for a trigger, a row to holdingTriggerBindings (the table and event it refuses to be
//     bound elsewhere for);
//   - its grants to holdingDefinerMatrix (each column re-earned by measurement -- ADR 0027 §3:
//     "matris kesin listenin üst sınırıdır");
//   - when a written table is born (site_punches, punch_events, webhook_deliveries,
//     feed_counters), nothing: holdingWrittenTables already names them and the canary reads each
//     one that exists.
// Each test below is red on a function, a grant or a table the lists do not name (derived from
// the catalogue, not from these lists), so forgetting the row is not silent.

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// holdingKind is how a holding function is reached.
type holdingKind int

const (
	// holdingCallable: a SECURITY DEFINER tappa_app calls (EXECUTE exactly {owner, tappa_app}).
	holdingCallable holdingKind = iota
	// holdingPredicate: holding_peer -- SECURITY INVOKER, EXECUTE in its owner alone.
	holdingPredicate
	// holdingTrigger: a trigger function -- SECURITY DEFINER, EXECUTE in its owner alone, a
	// lock_timeout attribute, and a TG_RELID assert.
	holdingTrigger
)

// holdingSignature is one row of ADR 0027 §2's signature table: the text of
// pg_get_function_identity_arguments and pg_get_function_result (typmod-free).
type holdingSignature struct {
	name, args, result string
	kind               holdingKind
}

// holdingSignatures is ADR 0027 §2's "İmza tablosu" -- since API-8 (00055) all of it. The set of
// functions tappa_holdingdefiner owns is EXACTLY this list (TestHoldingCatalog_TheFunctionsAndTheirExactSignatures).
var holdingSignatures = []holdingSignature{
	{"holding_peer", "a uuid, b uuid, mod text", "boolean", holdingPredicate},
	{"holding_timezone_lock", "", "trigger", holdingTrigger},
	// 00043 (M11 API-1b): the tap path's three callables and the site gate -- the ADR's rows, verbatim.
	{"holding_tap_site", "p_uid character",
		"TABLE(site_tenant_id uuid, tag_status text, location_id uuid, location_name text, location_external_ref text, " +
			"static_ips cidr[], gps_lat numeric, gps_lng numeric, shift_start time without time zone, shift_end time without time zone, " +
			"overnight boolean, timezone text, api_code text, accent character, logo_sha256 text, logo_mime text, logo_width integer, " +
			"logo_height integer, business_name text)", holdingCallable},
	// 00045 (the user's decision K-1i, 2026-10-14): ctr_gap joins the advance's result.
	{"holding_advance_tag_counter", "p_uid character, p_ctr integer", "TABLE(outcome text, tag_status text, ctr_gap integer)", holdingCallable},
	{"holding_logo", "p_uid character, p_digest text", "TABLE(logo bytea, logo_mime text)", holdingCallable},
	{"holding_site_gate", "", "trigger", holdingTrigger},
	// 00046 (M11 API-1c): the projection half of the one holding trigger (API-2 adds the events).
	{"holding_feed", "", "trigger", holdingTrigger},
	// 00047 (M11 API-1c): a peer's venues -- static_ips and GPS NEVER (K-39).
	{"holding_site_locations", "p_cursor uuid",
		"TABLE(site_tenant_id uuid, location_id uuid, location_name text, location_external_ref text, " +
			"shift_start time without time zone, shift_end time without time zone, overnight boolean)", holdingCallable},
	// 00055 (M11 API-8): a peer's employees -- never the caller's own, never an e-mail (K-1f, K-24, K-34).
	{"holding_directory", "p_updated_since timestamp with time zone, p_cursor uuid",
		"TABLE(employee_id uuid, employer_tenant_id uuid, api_code text, external_ref text, employee_code text, " +
			"first_name text, last_name text, active boolean, updated_at timestamp with time zone)", holdingCallable},
}

// holdingTriggerConfig is a holding trigger function's exact proconfig: the search path and
// the lock_timeout attribute (ADR 0028 §5's starting value, ADR 0027 §2 (v)).
var holdingTriggerConfig = []string{"search_path=pg_catalog, pg_temp", "lock_timeout=2s"}

// holdingCatalogFindings is ADR 0027 §2's "Katalog testi", items (i)-(vi), over EVERY schema:
//
//	(i)   the qualified names tappa_holdingdefiner owns are exactly sigs', all in public;
//	(ii)  each one's argument and result text is the table's, verbatim;
//	(iii) one overload per qualified name;
//	(iv)  every function named holding\_% in any schema is owned by tappa_holdingdefiner;
//	(v)   prosecdef true but for holding_peer (false), provolatile 'v' for holding_peer, proconfig
//	      exactly the search path (and, for a trigger, the lock_timeout too);
//	(vi)  EXECUTE (aclexplode over coalesce(proacl, acldefault) -- a NULL ACL is PUBLIC's
//	      EXECUTE) exactly {owner, tappa_app} for a callable one and {owner} for the predicate
//	      and the triggers.
func holdingCatalogFindings(ctx context.Context, q opQuerier, sigs []holdingSignature) ([]string, error) {
	type fn struct {
		schema, name, owner, args, result, vol, config, execute string
		secdef                                                  bool
		overloads                                               int
	}
	rows, err := q.Query(ctx, `
		SELECT n.nspname, p.proname, pg_get_userbyid(p.proowner),
		       pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
		       p.provolatile::text, coalesce(array_to_string(p.proconfig, '|'), ''), p.prosecdef,
		       coalesce((SELECT string_agg(CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END, ','
		                                    ORDER BY CASE WHEN a.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END)
		                   FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) AS a
		                  WHERE a.privilege_type = 'EXECUTE'), ''),
		       count(*) OVER (PARTITION BY n.nspname, p.proname)
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE p.proowner = 'tappa_holdingdefiner'::regrole OR p.proname LIKE 'holding\_%'
		 ORDER BY 1, 2`)
	if err != nil {
		return nil, err
	}
	var fns []fn
	for rows.Next() {
		var f fn
		if err := rows.Scan(&f.schema, &f.name, &f.owner, &f.args, &f.result, &f.vol, &f.config, &f.secdef, &f.execute, &f.overloads); err != nil {
			rows.Close()
			return nil, err
		}
		fns = append(fns, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	want := map[string]holdingSignature{}
	for _, s := range sigs {
		want["public."+s.name] = s
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range fns {
		q := f.schema + "." + f.name
		bad := func(format string, a ...any) { out = append(out, q+"("+f.args+"): "+fmt.Sprintf(format, a...)) }
		if f.owner != "tappa_holdingdefiner" {
			bad("(iv) named holding_* but owned by %s -- a holding_* the definer does not own is a definer of another role, possibly the superuser", f.owner)
			continue
		}
		s, ok := want[q]
		if !ok {
			bad("(i) owned by tappa_holdingdefiner and not in ADR 0027 §2's signature table (a closed-list member needs the user's approval and the ADR)")
			continue
		}
		seen[q] = true
		if f.overloads != 1 {
			bad("(iii) %d overloads; a second signature is a second door", f.overloads)
		}
		if f.args != s.args || f.result != s.result {
			bad("(ii) is (%s) -> %s, the table says (%s) -> %s", f.args, f.result, s.args, s.result)
		}
		switch s.kind {
		case holdingPredicate:
			if f.secdef || f.vol != "v" {
				bad("(v) SECURITY DEFINER=%v provolatile=%s, want SECURITY INVOKER and VOLATILE", f.secdef, f.vol)
			}
			if f.config != "search_path=pg_catalog, pg_temp" {
				bad("(v) proconfig %q, want exactly search_path=pg_catalog, pg_temp", f.config)
			}
		case holdingTrigger:
			if !f.secdef {
				bad("(v) not SECURITY DEFINER")
			}
			if f.config != strings.Join(holdingTriggerConfig, "|") {
				bad("(v) proconfig %q, want exactly %v", f.config, holdingTriggerConfig)
			}
		default:
			if !f.secdef {
				bad("(v) not SECURITY DEFINER")
			}
			if f.config != "search_path=pg_catalog, pg_temp" {
				bad("(v) proconfig %q, want exactly search_path=pg_catalog, pg_temp", f.config)
			}
		}
		wantExec := "tappa_holdingdefiner"
		if s.kind == holdingCallable {
			wantExec = "tappa_app,tappa_holdingdefiner"
		}
		if f.execute != wantExec {
			bad("(vi) EXECUTE is held by {%s}, want exactly {%s}", f.execute, wantExec)
		}
	}
	for q := range want {
		if !seen[q] {
			out = append(out, q+": (i) in the signature table but not owned by tappa_holdingdefiner in the catalogue")
		}
	}
	sort.Strings(out)
	return out, nil
}

// TestHoldingCatalog_TheFunctionsAndTheirExactSignatures is ADR 0027 §2's catalogue test (the
// six items above) on this database, with a positive control per item -- each a mutant the ADR
// names or measured, made inside a savepoint that is rolled back:
//   - a holding_site_locations whose result is widened by static_ips (1st round's mutant; checked
//     against the row the ADR writes for it, so the RESULT check fires and not only "not in the
//     table");
//   - EXECUTE on holding_peer granted to tappa_operator (2nd round: an "is false for these roles"
//     check stayed green);
//   - a holding_* in another schema, owned by the definer and by the owner;
//   - a second overload; holding_peer made SECURITY DEFINER; a search path with public; a
//     trigger without its lock_timeout.
func TestHoldingCatalog_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	findings, err := holdingCatalogFindings(ctx, tx, holdingSignatures)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proowner = 'tappa_holdingdefiner'::regrole`); n != int64(len(holdingSignatures)) {
		t.Fatalf("anti-vacuity: tappa_holdingdefiner owns %d functions, the table has %d", n, len(holdingSignatures))
	}

	controls := []struct {
		name  string
		sql   []string
		sigs  []holdingSignature
		wantF string
	}{
		{"holding_site_locations returning static_ips (the 1st round's mutant; 00047's function replaced)", []string{
			`DROP FUNCTION public.holding_site_locations(uuid)`,
			`CREATE FUNCTION public.holding_site_locations(p_cursor uuid)
			     RETURNS TABLE(site_tenant_id uuid, location_id uuid, location_name text, location_external_ref text,
			                   shift_start time, shift_end time, overnight boolean, static_ips cidr[])
			     LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
			     AS 'SELECT NULL::uuid, NULL::uuid, NULL::text, NULL::text, NULL::time, NULL::time, NULL::boolean, NULL::cidr[]'`,
			`ALTER FUNCTION public.holding_site_locations(uuid) OWNER TO tappa_holdingdefiner`,
			`REVOKE ALL ON FUNCTION public.holding_site_locations(uuid) FROM PUBLIC`,
			`GRANT EXECUTE ON FUNCTION public.holding_site_locations(uuid) TO tappa_app`},
			holdingSignatures, "(ii)"},
		{"a definer function not in the table at all", []string{
			`CREATE FUNCTION public.holding_zz_sites(p_cursor uuid) RETURNS TABLE(location_name text)
			     LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT NULL::text'`,
			`ALTER FUNCTION public.holding_zz_sites(uuid) OWNER TO tappa_holdingdefiner`},
			holdingSignatures, "(i)"},
		{"EXECUTE on holding_peer to tappa_operator", []string{
			`GRANT EXECUTE ON FUNCTION public.holding_peer(uuid, uuid, text) TO tappa_operator`}, holdingSignatures, "(vi)"},
		{"EXECUTE on holding_peer to PUBLIC", []string{
			`GRANT EXECUTE ON FUNCTION public.holding_peer(uuid, uuid, text) TO PUBLIC`}, holdingSignatures, "(vi)"},
		{"EXECUTE on the trigger to tappa_app", []string{
			`GRANT EXECUTE ON FUNCTION public.holding_timezone_lock() TO tappa_app`}, holdingSignatures, "(vi)"},
		{"a holding_* in another schema, the definer's", []string{
			`CREATE SCHEMA zz_holding_probe`,
			`CREATE FUNCTION zz_holding_probe.holding_peer(a uuid, b uuid, mod text) RETURNS boolean LANGUAGE sql AS 'SELECT true'`,
			`ALTER FUNCTION zz_holding_probe.holding_peer(uuid, uuid, text) OWNER TO tappa_holdingdefiner`},
			holdingSignatures, "(i)"},
		{"a holding_* in another schema, the owner's", []string{
			`CREATE SCHEMA zz_holding_probe`,
			`CREATE FUNCTION zz_holding_probe.holding_zz() RETURNS boolean LANGUAGE sql SECURITY DEFINER AS 'SELECT true'`},
			holdingSignatures, "(iv)"},
		{"a holding_* in public left with the migration role", []string{
			`CREATE FUNCTION public.holding_zz_forgotten() RETURNS boolean LANGUAGE sql SECURITY DEFINER AS 'SELECT true'`},
			holdingSignatures, "(iv)"},
		{"a second overload of holding_peer", []string{
			`CREATE FUNCTION public.holding_peer(a uuid, b uuid) RETURNS boolean LANGUAGE sql AS 'SELECT false'`,
			`ALTER FUNCTION public.holding_peer(uuid, uuid) OWNER TO tappa_holdingdefiner`},
			holdingSignatures, "(iii)"},
		{"holding_peer made SECURITY DEFINER", []string{
			`ALTER FUNCTION public.holding_peer(uuid, uuid, text) SECURITY DEFINER`}, holdingSignatures, "(v)"},
		{"holding_peer made STABLE", []string{
			`ALTER FUNCTION public.holding_peer(uuid, uuid, text) STABLE`}, holdingSignatures, "(v)"},
		{"a search path that reaches public", []string{
			`ALTER FUNCTION public.holding_peer(uuid, uuid, text) SET search_path = pg_catalog, public`}, holdingSignatures, "(v)"},
		{"a trigger without its lock_timeout", []string{
			`ALTER FUNCTION public.holding_timezone_lock() RESET lock_timeout`}, holdingSignatures, "(v)"},
	}
	for _, c := range controls {
		t.Run("control/"+c.name, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := sp.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
			}()
			for _, s := range c.sql {
				if _, err := sp.Exec(ctx, s); err != nil {
					t.Fatalf("%q: %v", s, err)
				}
			}
			got, err := holdingCatalogFindings(ctx, sp, c.sigs)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, c.wantF) }) {
				t.Fatalf("the catalogue test did not flag it with %s; findings: %v", c.wantF, got)
			}
		})
	}
}

// ------------------------------------------------------------------ holding_peer --

// holdingPeerAs calls holding_peer as its owner -- the only role that may -- inside a savepoint
// of tx. NULL answers come back as nil.
func holdingPeerAs(t *testing.T, ctx context.Context, tx pgx.Tx, a, b any, mode any) (*bool, error) {
	t.Helper()
	var got *bool
	err := opAs(t, ctx, tx, "tappa_holdingdefiner", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT public.holding_peer($1, $2, $3)`, a, b, mode).Scan(&got)
	})
	return got, err
}

// TestHoldingPeer_InvokerVolatileOwnedByDefinerExecutableOnlyByOwner is holding_peer's own
// catalogue row (ADR 0021's prosecdef owner test does not cover an INVOKER -- ADR 0027 §2):
// owned by tappa_holdingdefiner, SECURITY INVOKER, VOLATILE, proconfig exactly the search path,
// EXECUTE in the owner alone. By behaviour: the owner may call it; tappa_app, tappa_operator,
// tappa_opdefiner and tappa_resolver are 42501. And the 10th round's measurement: granted to
// tappa_app by mistake, it still reads nothing -- it is INVOKER, and tappa_app holds no privilege
// on holding_members (42501 from the table, not the function).
func TestHoldingPeer_InvokerVolatileOwnedByDefinerExecutableOnlyByOwner(t *testing.T) {
	ctx, tx := opTx(t)
	var owner, vol, config, execute string
	var secdef bool
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_userbyid(p.proowner), p.provolatile::text, array_to_string(p.proconfig, '|'), p.prosecdef,
		       (SELECT string_agg(pg_get_userbyid(a.grantee), ',' ORDER BY 1)
		          FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) AS a WHERE a.privilege_type = 'EXECUTE')
		  FROM pg_proc p WHERE p.oid = 'public.holding_peer(uuid, uuid, text)'::regprocedure`).Scan(&owner, &vol, &config, &secdef, &execute); err != nil {
		t.Fatalf("read holding_peer: %v", err)
	}
	if owner != "tappa_holdingdefiner" || secdef || vol != "v" || config != "search_path=pg_catalog, pg_temp" || execute != "tappa_holdingdefiner" {
		t.Errorf("holding_peer: owner %s SECURITY DEFINER %v provolatile %s proconfig %q EXECUTE {%s}; want tappa_holdingdefiner, INVOKER, v, the search path, {tappa_holdingdefiner}",
			owner, secdef, vol, config, execute)
	}
	a, b := uuid.New(), uuid.New()
	if _, err := holdingPeerAs(t, ctx, tx, a, b, "strict"); err != nil {
		t.Fatalf("CONTROL: the owner cannot call holding_peer: %v", err)
	}
	for _, role := range []string{"tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_resolver"} {
		opWant(t, opExecAs(t, ctx, tx, role, `SELECT public.holding_peer($1, $2, 'strict')`, a, b),
			sqlstateInsufficientPrivi, role+" calling holding_peer")
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `GRANT EXECUTE ON FUNCTION public.holding_peer(uuid, uuid, text) TO tappa_app`); err != nil {
		t.Fatal(err)
	}
	err = opExecAs(t, ctx, sp, "tappa_app", `SELECT public.holding_peer($1, $2, 'strict')`, a, b)
	opWant(t, err, sqlstateInsufficientPrivi, "tappa_app calling holding_peer granted by mistake")
	if _, msg := opCode(err); !strings.Contains(msg, "holding_members") {
		t.Errorf("granted by mistake, tappa_app's call was refused by %q -- want the table's refusal (INVOKER reads with the caller's rights)", msg)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestHoldingPeer_AppCannotCallIt is ADR 0027 API-1b (g)'s catalogue row, born with the
// function: tappa_app may not EXECUTE holding_peer, by privilege (has_function_privilege, which
// counts membership and PUBLIC) and by behaviour (42501) -- a free-argument membership oracle
// otherwise (9th round).
func TestHoldingPeer_AppCannotCallIt(t *testing.T) {
	ctx, tx := opTx(t)
	var may bool
	if err := tx.QueryRow(ctx, `SELECT has_function_privilege('tappa_app', 'public.holding_peer(uuid, uuid, text)', 'EXECUTE')`).Scan(&may); err != nil {
		t.Fatal(err)
	}
	if may {
		t.Error("tappa_app may EXECUTE holding_peer")
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `SELECT public.holding_peer(gen_random_uuid(), NULL, 'is_member')`),
		sqlstateInsufficientPrivi, "tappa_app asking holding_peer whether a tenant is a member")
}

// holdingPeerModes are ADR 0027 §2's five modes, in the order holding_peer's guard lists them.
var holdingPeerModes = []string{"strict", "strict_stmt", "tolerant_stmt", "lock", "is_member"}

// holdingPeerBodyMD5 is md5(prosrc) of holding_peer as migration 00041 creates it. The modes and
// their rules are ADR 0027 §2's closed set (CLAUDE.md §4.5): a body that changes -- a sixth mode
// above all -- is red here until an ADR 0027 change the user approved moves this pin with it.
const holdingPeerBodyMD5 = "8044a5597320487de4f1d19bed399a0e"

// holdingPeerChangeNeeds is what every failure of the body pin says.
const holdingPeerChangeNeeds = "holding_peer's modes and rules are ADR 0027 §2's closed set: a new mode or a changed rule " +
	"needs an ADR 0027 change and the user's approval (CLAUDE.md §4.5) before this pin moves"

// TestHoldingPeer_ModesAreAClosedSet: the five modes and nothing else.
//   - THE BODY (the API-1a review's ORTA-1: a sixth mode 'any', yes for any two live members,
//     stayed green everywhere): holding_peer's body is pinned by md5(prosrc), its guard lists
//     exactly the five modes, and no other mode word is compared to `mod` anywhere in it.
//   - EACH MODE IS ACCEPTED: a and b live in one holding -- strict, strict_stmt and tolerant_stmt
//     answer true, lock answers NULL without an error, is_member(a) answers true.
//   - THE REST IS REFUSED: an unknown mode, a NULL mode, a mode in another case or with a space
//     are 22023 (never a default); is_member takes ONE tenant (a second is 22023); a tenant is
//     never its own peer (a = b is false in the three deciding modes, and the lock mode answers
//     NULL); a NULL tenant is not a peer either.
func TestHoldingPeer_ModesAreAClosedSet(t *testing.T) {
	ctx, tx := opTx(t)
	var src, digest string
	if err := tx.QueryRow(ctx, `SELECT prosrc, md5(prosrc) FROM pg_proc WHERE oid = 'public.holding_peer(uuid, uuid, text)'::regprocedure`).
		Scan(&src, &digest); err != nil {
		t.Fatalf("read holding_peer: %v", err)
	}
	if digest != holdingPeerBodyMD5 {
		t.Errorf("holding_peer's body changed (md5 %s, want %s): %s", digest, holdingPeerBodyMD5, holdingPeerChangeNeeds)
	}
	body := strings.ReplaceAll(holdingStripComments(src), `"`, "")
	guard := regexp.MustCompile(`(?is)\bmod\s+is\s+null\s+or\s+mod\s+not\s+in\s*\(([^)]*)\)`).FindAllStringSubmatch(body, -1)
	var listed []string
	if len(guard) == 1 {
		for _, m := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(guard[0][1], -1) {
			listed = append(listed, m[1])
		}
	}
	if !slices.Equal(listed, holdingPeerModes) {
		t.Errorf("holding_peer's guard lists %v (%d guard(s)), want exactly %v: %s", listed, len(guard), holdingPeerModes, holdingPeerChangeNeeds)
	}
	compared := 0
	for _, m := range regexp.MustCompile(`(?is)\bmod\s*(?:=|<>|!=)\s*'([^']*)'|\bmod\s+(?:not\s+)?in\s*\(([^)]*)\)|\bcase\s+mod\b`).FindAllStringSubmatch(body, -1) {
		words := []string{m[1]}
		if m[1] == "" {
			words = nil
			for _, w := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(m[2], -1) {
				words = append(words, w[1])
			}
		}
		if m[0] != "" && strings.HasPrefix(strings.ToLower(m[0]), "case") {
			t.Errorf("holding_peer dispatches with CASE mod, which this scan does not read: %s", holdingPeerChangeNeeds)
		}
		for _, w := range words {
			compared++
			if !slices.Contains(holdingPeerModes, w) {
				t.Errorf("holding_peer compares mod to %q, not one of %v: %s", w, holdingPeerModes, holdingPeerChangeNeeds)
			}
		}
	}
	if compared < len(holdingPeerModes)+3 {
		t.Errorf("anti-vacuity: %d mode words compared to mod in holding_peer's body, want the guard's five and the branches'", compared)
	}

	// Each of the five is accepted.
	pa := opNewTenant(t, ctx, tx, "api1a modes a "+opToken(t), 0)
	pb := opNewTenant(t, ctx, tx, "api1a modes b "+opToken(t), 0)
	h := holdingNew(t, ctx, tx, "api1a modes "+opToken(t))
	holdingInsertMember(t, ctx, tx, h, holdingMember{pa, "10 minutes", ""})
	holdingInsertMember(t, ctx, tx, h, holdingMember{pb, "10 minutes", ""})
	for _, m := range holdingPeerModes {
		second := any(pb)
		if m == "is_member" {
			second = nil
		}
		got, err := holdingPeerAs(t, ctx, tx, pa, second, m)
		switch {
		case err != nil:
			t.Errorf("the mode %s was refused for two live peers: %v", m, err)
		case m == "lock" && got != nil:
			t.Errorf("lock(a, b) = %v, want NULL -- the lock mode decides nothing", *got)
		case m != "lock" && (got == nil || !*got):
			t.Errorf("%s(a, b) = %v for two live peers, want true", m, opDeref(got))
		}
	}

	a, b := uuid.New(), uuid.New()
	for _, m := range []any{"bogus", nil, "STRICT", " strict", "strict ", "Is_Member", ""} {
		_, err := holdingPeerAs(t, ctx, tx, a, b, m)
		opWant(t, err, sqlstateInvalidParameter, fmt.Sprintf("the mode %v", m))
	}
	_, err := holdingPeerAs(t, ctx, tx, a, b, "is_member")
	opWant(t, err, sqlstateInvalidParameter, "is_member with a second tenant")
	for _, m := range []string{"strict", "strict_stmt", "tolerant_stmt"} {
		for _, pair := range [][2]any{{a, a}, {a, nil}, {nil, b}} {
			got, err := holdingPeerAs(t, ctx, tx, pair[0], pair[1], m)
			if err != nil || got == nil || *got {
				t.Errorf("%s(%v, %v) = %v (%v), want false", m, pair[0], pair[1], opDeref(got), err)
			}
		}
	}
	if got, err := holdingPeerAs(t, ctx, tx, a, a, "lock"); err != nil || got != nil {
		t.Errorf("lock(a, a) = %v (%v), want NULL -- the lock mode decides nothing", opDeref(got), err)
	}
	if got, err := holdingPeerAs(t, ctx, tx, nil, nil, "is_member"); err != nil || got == nil || *got {
		t.Errorf("is_member(NULL) = %v (%v), want false", opDeref(got), err)
	}
}

// holdingFixture is a holding and its members, inserted DIRECTLY by the owner with chosen
// instants (the op_* write only the wall clock; these tests need the past). Each member is a
// new tenant of the given zone.
type holdingMember struct {
	tenant             uuid.UUID
	joinedAgo, leftAgo string // intervals before the wall clock; leftAgo "" = live
}

func holdingNew(t *testing.T, ctx context.Context, q opQuerier, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := q.QueryRow(ctx, `INSERT INTO holdings (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("insert holding: %v", err)
	}
	return id
}

func holdingInsertMember(t *testing.T, ctx context.Context, q opQuerier, holding uuid.UUID, m holdingMember) {
	t.Helper()
	var left any
	if m.leftAgo != "" {
		left = m.leftAgo
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO holding_members (holding_id, member_tenant_id, joined_at, left_at)
		VALUES ($1, $2, clock_timestamp() - $3::interval, clock_timestamp() - $4::interval)`,
		holding, m.tenant, m.joinedAgo, left); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
}

// TestHoldingPeer_TolerantModesRequireOverlappingMemberships is ADR 0027 §2's round-2 rule: the
// tolerant mode pairs two memberships only if they overlapped in time. A left H 10 s ago; B
// joined H 5 s ago -- after A left: within A's 60-second window, and still NOT A's peer in any
// mode (they were never members together). C was a member together with A and is live: within
// the window A and C are tolerant peers and not strict ones. D left 61 s ago together with E
// (live): past the window, peers in no mode. And the strict modes see the live pair B, C.
func TestHoldingPeer_TolerantModesRequireOverlappingMemberships(t *testing.T) {
	ctx, tx := opTx(t)
	h := holdingNew(t, ctx, tx, "api1a tolerant "+opToken(t))
	ta := opNewTenant(t, ctx, tx, "api1a A "+opToken(t), 0)
	tb := opNewTenant(t, ctx, tx, "api1a B "+opToken(t), 0)
	tc := opNewTenant(t, ctx, tx, "api1a C "+opToken(t), 0)
	td := opNewTenant(t, ctx, tx, "api1a D "+opToken(t), 0)
	te := opNewTenant(t, ctx, tx, "api1a E "+opToken(t), 0)
	for _, m := range []holdingMember{
		{ta, "300 seconds", "10 seconds"},
		{tb, "5 seconds", ""},
		{tc, "400 seconds", ""},
		{td, "500 seconds", "61 seconds"},
		{te, "600 seconds", ""},
	} {
		holdingInsertMember(t, ctx, tx, h, m)
	}
	cases := []struct {
		what                         string
		a, b                         uuid.UUID
		strict, strictStmt, tolerant bool
	}{
		{"A left before B joined (A's window)", ta, tb, false, false, false},
		{"the same, the other way round", tb, ta, false, false, false},
		{"A left 10 s ago, C was a member with A", ta, tc, false, false, true},
		{"the same, the other way round", tc, ta, false, false, true},
		{"D left 61 s ago, E was a member with D", td, te, false, false, false},
		{"B and C, both live", tb, tc, true, true, true},
	}
	for _, c := range cases {
		for mode, want := range map[string]bool{"strict": c.strict, "strict_stmt": c.strictStmt, "tolerant_stmt": c.tolerant} {
			got, err := holdingPeerAs(t, ctx, tx, c.a, c.b, mode)
			if err != nil || got == nil || *got != want {
				t.Errorf("%s: %s = %v (%v), want %v", c.what, mode, opDeref(got), err, want)
			}
		}
	}
	// is_member: A within its window, D past it, B live.
	for _, c := range []struct {
		tenant uuid.UUID
		want   bool
	}{{ta, true}, {td, false}, {tb, true}, {uuid.New(), false}} {
		if got, err := holdingPeerAs(t, ctx, tx, c.tenant, nil, "is_member"); err != nil || got == nil || *got != c.want {
			t.Errorf("is_member(%s) = %v (%v), want %v", c.tenant, opDeref(got), err, c.want)
		}
	}
	// A tenant of another holding is no peer, whatever the clocks say.
	h2 := holdingNew(t, ctx, tx, "api1a other "+opToken(t))
	tf := opNewTenant(t, ctx, tx, "api1a F "+opToken(t), 0)
	holdingInsertMember(t, ctx, tx, h2, holdingMember{tf, "700 seconds", ""})
	for _, mode := range []string{"strict", "strict_stmt", "tolerant_stmt"} {
		if got, err := holdingPeerAs(t, ctx, tx, tb, tf, mode); err != nil || got == nil || *got {
			t.Errorf("B and F (two holdings): %s = %v (%v), want false", mode, opDeref(got), err)
		}
	}
}

// ------------------------------------------------------------------ body scans --

// holdingStripComments removes -- comments and nested /* */ comments from SQL text (ADR 0027 §2,
// the body scan's item 1). String literals are not parsed: a comment marker inside one cuts the
// rest of its line, which can only make the scan see LESS of a literal -- and a literal naming
// holding_members is not what the rule is about.
func holdingStripComments(src string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(src); i++ {
		switch {
		case depth == 0 && strings.HasPrefix(src[i:], "--"):
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				return b.String()
			}
			i += j - 1
		case strings.HasPrefix(src[i:], "/*"):
			depth++
			i++
		case depth > 0 && strings.HasPrefix(src[i:], "*/"):
			depth--
			i++
		case depth == 0:
			b.WriteByte(src[i])
		}
	}
	return b.String()
}

// holdingMembersRE finds holding_members whatever its case or quoting: the text it runs over has
// its comments stripped and its double quotes removed.
var holdingMembersRE = regexp.MustCompile(`(?i)(^|[^a-z0-9_])holding_members([^a-z0-9_]|$)`)

// holdingMembersBodyFindings is ADR 0027 §2's body scan (items 1-3): EVERY function of every
// schema (aggregates and window functions aside -- pg_get_functiondef refuses them, and they
// have no body) whose definition, comments stripped and quotes removed, names holding_members
// must be public.holding_peer or an op_% owned by tappa_opdefiner; and every pg_depend edge from
// a function to holding_members (a BEGIN ATOMIC body records one; PL/pgSQL does not) must come
// from one of the same two classes.
func holdingMembersBodyFindings(ctx context.Context, q opQuerier) ([]string, int, error) {
	rows, err := q.Query(ctx, `
		SELECT n.nspname, p.proname, pg_get_userbyid(p.proowner), p.oid::regprocedure::text, pg_get_functiondef(p.oid)
		  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE p.prokind IN ('f', 'p')`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	allowed := func(schema, name, owner string) bool {
		return (schema == "public" && name == "holding_peer") ||
			(schema == "public" && strings.HasPrefix(name, "op_") && owner == "tappa_opdefiner")
	}
	var out []string
	walked := 0
	for rows.Next() {
		var schema, name, owner, sig, def string
		if err := rows.Scan(&schema, &name, &owner, &sig, &def); err != nil {
			return nil, 0, err
		}
		walked++
		text := strings.ReplaceAll(holdingStripComments(def), `"`, "")
		if holdingMembersRE.MatchString(text) && !allowed(schema, name, owner) {
			out = append(out, fmt.Sprintf("%s (owner %s) names holding_members in its body", sig, owner))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	deps, err := q.Query(ctx, `
		SELECT n.nspname, p.proname, pg_get_userbyid(p.proowner), p.oid::regprocedure::text
		  FROM pg_depend d
		  JOIN pg_proc p ON p.oid = d.objid AND d.classid = 'pg_proc'::regclass
		  JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE d.refclassid = 'pg_class'::regclass AND d.refobjid = 'public.holding_members'::regclass`)
	if err != nil {
		return nil, 0, err
	}
	defer deps.Close()
	for deps.Next() {
		var schema, name, owner, sig string
		if err := deps.Scan(&schema, &name, &owner, &sig); err != nil {
			return nil, 0, err
		}
		if !allowed(schema, name, owner) {
			out = append(out, fmt.Sprintf("%s (owner %s) depends on holding_members (pg_depend)", sig, owner))
		}
	}
	return out, walked, deps.Err()
}

// TestHoldingPeer_IsTheOnlyBodyThatTouchesHoldingMembers is ADR 0027 §2's body scan on this
// database, with its anti-vacuity (it read thousands of definitions, holding_peer's among them
// naming the table) and the round-2 auditor's four bypasses as positive controls -- each a
// function made inside a savepoint: the name behind an inline comment, a quoted and an upper-case
// identifier, and a BEGIN ATOMIC body; and the negative control: a function that names the table
// ONLY in a comment is not flagged (comments are stripped first). An op_% the migration role owns
// is flagged too (the exemption is the definer's op_*, not the prefix).
func TestHoldingPeer_IsTheOnlyBodyThatTouchesHoldingMembers(t *testing.T) {
	ctx, tx := opTx(t)
	findings, walked, err := holdingMembersBodyFindings(ctx, tx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	if walked < 1000 {
		t.Fatalf("anti-vacuity: the scan read %d definitions; pg_catalog alone has thousands", walked)
	}
	var def string
	if err := tx.QueryRow(ctx, `SELECT pg_get_functiondef('public.holding_peer(uuid, uuid, text)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if !holdingMembersRE.MatchString(holdingStripComments(def)) {
		t.Fatal("anti-vacuity: holding_peer's definition does not name holding_members as the scan reads it")
	}
	for _, c := range []struct {
		name string
		sql  []string
		want bool
	}{
		{"a comment only", []string{`CREATE FUNCTION public.zz_probe_hm() RETURNS int LANGUAGE plpgsql AS $$
			BEGIN
			    -- holding_members is not read here
			    /* nor /* nested */ holding_members here */
			    RETURN 1;
			END $$`}, false},
		{"behind an inline comment", []string{`CREATE FUNCTION public.zz_probe_hm() RETURNS bigint LANGUAGE plpgsql AS $$
			BEGIN RETURN (SELECT count(*) FROM public./* x */holding_members); END $$`}, true},
		{"a quoted identifier", []string{`CREATE FUNCTION public.zz_probe_hm() RETURNS bigint LANGUAGE plpgsql AS $$
			BEGIN RETURN (SELECT count(*) FROM "public"."holding_members"); END $$`}, true},
		{"an upper-case identifier", []string{`CREATE FUNCTION public.zz_probe_hm() RETURNS bigint LANGUAGE plpgsql AS $$
			BEGIN RETURN (SELECT count(*) FROM PUBLIC.HOLDING_MEMBERS); END $$`}, true},
		{"a BEGIN ATOMIC body", []string{`CREATE FUNCTION public.zz_probe_hm() RETURNS bigint LANGUAGE sql
			BEGIN ATOMIC SELECT count(*) FROM public.holding_members; END`}, true},
		{"an op_* the migration role owns", []string{`CREATE FUNCTION public.op_zz_probe_hm() RETURNS bigint LANGUAGE sql
			AS 'SELECT count(*) FROM public.holding_members'`}, true},
		{"an op_* the definer owns", []string{`CREATE FUNCTION public.op_zz_probe_hm() RETURNS bigint LANGUAGE sql
			AS 'SELECT count(*) FROM public.holding_members'`,
			`ALTER FUNCTION public.op_zz_probe_hm() OWNER TO tappa_opdefiner`}, false},
	} {
		t.Run("control/"+c.name, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := sp.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
			}()
			for _, s := range c.sql {
				if _, err := sp.Exec(ctx, s); err != nil {
					t.Fatalf("%v", err)
				}
			}
			got, _, err := holdingMembersBodyFindings(ctx, sp)
			if err != nil {
				t.Fatal(err)
			}
			hit := slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, "zz_probe_hm") })
			if hit != c.want {
				t.Fatalf("flagged=%v, want %v; findings: %v", hit, c.want, got)
			}
		})
	}
}

// holdingDefinerBodies returns every body tappa_holdingdefiner owns: the qualified name and the
// definition, comments stripped.
func holdingDefinerBodies(t *testing.T, ctx context.Context, q opQuerier) map[string]string {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT p.oid::regprocedure::text, pg_get_functiondef(p.oid) FROM pg_proc p
	                            WHERE p.proowner = 'tappa_holdingdefiner'::regrole AND p.prokind IN ('f', 'p')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var sig, def string
		if err := rows.Scan(&sig, &def); err != nil {
			t.Fatal(err)
		}
		out[sig] = holdingStripComments(def)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

var (
	holdingDynamicSQLRE = regexp.MustCompile(`(?i)(^|[^a-z0-9_])execute([^a-z0-9_]|$)`)
	holdingUnicodeRE    = regexp.MustCompile(`(?i)u&["']`)
)

// TestHoldingDefiner_NoDynamicSQLNoUnicodeIdentifiers is ADR 0027 §2's body scan item 4: no body
// tappa_holdingdefiner owns holds EXECUTE (dynamic SQL -- the function contract's item 1) or a
// U&"..." identifier (a Unicode escape that spells a table name no text scan matches). Controls:
// a definer-owned probe with each, in a savepoint.
func TestHoldingDefiner_NoDynamicSQLNoUnicodeIdentifiers(t *testing.T) {
	ctx, tx := opTx(t)
	scan := func(q opQuerier) []string {
		var out []string
		for sig, body := range holdingDefinerBodies(t, ctx, q) {
			if holdingDynamicSQLRE.MatchString(body) {
				out = append(out, sig+": EXECUTE (dynamic SQL)")
			}
			if holdingUnicodeRE.MatchString(body) {
				out = append(out, sig+": a U& escaped identifier or literal")
			}
		}
		return out
	}
	for _, f := range scan(tx) {
		t.Error(f)
	}
	if n := len(holdingDefinerBodies(t, ctx, tx)); n != len(holdingSignatures) {
		t.Fatalf("anti-vacuity: %d definer bodies read, the signature table has %d", n, len(holdingSignatures))
	}
	for _, c := range []struct{ name, sql string }{
		{"dynamic SQL", `CREATE FUNCTION public.zz_probe_dyn() RETURNS void LANGUAGE plpgsql AS $$
			BEGIN EXECUTE 'SELECT 1'; END $$`},
		{"a Unicode-escaped identifier", `CREATE FUNCTION public.zz_probe_dyn() RETURNS bigint LANGUAGE plpgsql AS $$
			BEGIN RETURN (SELECT count(*) FROM public.U&"holding\005fmembers"); END $$`},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{c.sql, `ALTER FUNCTION public.zz_probe_dyn() OWNER TO tappa_holdingdefiner`} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		}
		if got := scan(sp); !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, "zz_probe_dyn") }) {
			t.Errorf("control %s: not flagged (%v)", c.name, got)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// holdingPeerForbiddenRE is ADR 0027 §2's body scan item 5: holding_peer's text holds none of
// FOR UPDATE, FOR NO KEY UPDATE, FOR KEY SHARE, UPDATE, INSERT, DELETE, MERGE -- so its one row
// lock is FOR SHARE and it writes nothing.
var holdingPeerForbiddenRE = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(update|insert|delete|merge|for\s+key\s+share)([^a-z0-9_]|$)`)

// TestHoldingPeer_OnlyRowLockIsForShare is ADR 0027 §2's item 5 and §3's lock anchor, measured:
//   - the scan: holding_peer's definition (comments stripped) holds no forbidden word and holds
//     FOR SHARE; CONTROL: its own definition with FOR SHARE turned into FOR UPDATE is flagged;
//   - two sessions taking holding_peer's lock on the same pair in OPPOSITE argument order
//     (lock(a, b) holding, then lock(b, a)) do not wait for each other -- FOR SHARE shares;
//   - a remover's write of left_at (what op_remove_holding_member does, FOR NO KEY UPDATE on the
//     row) DOES wait for the share lock -- the ordering K-29's gate relies on;
//   - THE MUTANT, by its own statements (no DDL is committed to a shared database): the same
//     rows locked FOR UPDATE one at a time in opposite orders by two sessions deadlock (40P01) --
//     the 1st round's measurement, the reason FOR UPDATE may not appear in holding_peer.
//
// Committed fixtures: a holding and two members of two new tenants; the memberships and the
// holding are removed at the end (holdingLiveFixture), the tenants stay.
func TestHoldingPeer_OnlyRowLockIsForShare(t *testing.T) {
	f := holdingLiveFixture(t)
	var def string
	if err := f.owner.QueryRow(f.ctx, `SELECT pg_get_functiondef('public.holding_peer(uuid, uuid, text)'::regprocedure)`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	body := holdingStripComments(def)
	if m := holdingPeerForbiddenRE.FindString(body); m != "" {
		t.Errorf("holding_peer's definition holds %q", strings.TrimSpace(m))
	}
	share := regexp.MustCompile(`(?i)for\s+share`)
	if !share.MatchString(body) {
		t.Fatal("anti-vacuity: holding_peer holds no FOR SHARE -- the lock mode is gone")
	}
	if mutant := share.ReplaceAllString(body, "FOR UPDATE"); holdingPeerForbiddenRE.FindString(mutant) == "" {
		t.Error("CONTROL: holding_peer with FOR UPDATE in place of FOR SHARE is not flagged")
	}

	h := f.holding(t)
	a, b := f.tenant(t, "Europe/Malta", nil), f.tenant(t, "Europe/Malta", nil)
	f.member(t, h, a, "10 minutes", "")
	f.member(t, h, b, "10 minutes", "")
	c1, c2 := f.conn(t), f.conn(t)

	// Two lockers in opposite argument order share.
	t1 := holdingBegin(t, f.ctx, c1, "tappa_holdingdefiner")
	if _, err := t1.Exec(f.ctx, `SELECT public.holding_peer($1, $2, 'lock')`, a, b); err != nil {
		t.Fatalf("lock(a, b): %v", err)
	}
	t2 := holdingBegin(t, f.ctx, c2, "tappa_holdingdefiner")
	if _, err := t2.Exec(f.ctx, `SET LOCAL lock_timeout = '1s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := t2.Exec(f.ctx, `SELECT public.holding_peer($1, $2, 'lock')`, b, a); err != nil {
		t.Errorf("lock(b, a) while lock(a, b) is held: %v -- FOR SHARE must not make two lockers wait", err)
	}
	if err := t2.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	// The remover waits for it.
	t3 := holdingBegin(t, f.ctx, c2, "")
	if _, err := t3.Exec(f.ctx, `SET LOCAL lock_timeout = '500ms'`); err != nil {
		t.Fatal(err)
	}
	_, err := t3.Exec(f.ctx, `UPDATE holding_members SET left_at = clock_timestamp() WHERE holding_id = $1 AND member_tenant_id = $2 AND left_at IS NULL`, h, a)
	opWant(t, err, "55P03", "a remover's write of left_at while the gate's share lock is held")
	_ = t3.Rollback(f.ctx)
	if err := t1.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}

	// THE MUTANT: FOR UPDATE, one row at a time, in opposite orders.
	m1 := holdingBegin(t, f.ctx, c1, "")
	m2 := holdingBegin(t, f.ctx, c2, "")
	lockOne := `SELECT 1 FROM holding_members WHERE member_tenant_id = $1 FOR UPDATE`
	if _, err := m1.Exec(f.ctx, lockOne, a); err != nil {
		t.Fatal(err)
	}
	if _, err := m2.Exec(f.ctx, lockOne, b); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 1)
	go func() {
		_, err := m1.Exec(f.ctx, lockOne, b)
		errs <- err
	}()
	holdingWaitForWaiter(t, f, 1)
	_, err2 := m2.Exec(f.ctx, lockOne, a)
	err1 := <-errs
	c1code, _ := opCode(err1)
	c2code, _ := opCode(err2)
	if c1code != "40P01" && c2code != "40P01" {
		t.Errorf("FOR UPDATE in opposite orders: %v / %v, want one deadlock (40P01) -- the mutant's measurement", err1, err2)
	}
	_ = m1.Rollback(f.ctx)
	_ = m2.Rollback(f.ctx)
}

// ------------------------------------------------------------- EXECUTE and binding --

// holdingTriggerBindings are the holding triggers' own tables and events -- what each refuses
// to run anywhere else for (ADR 0027 §2, plan §4.0). API-1b added holding_site_gate (BEFORE
// INSERT ON transactions); API-1c/API-2 add holding_feed (AFTER INSERT ON transactions and
// transaction_reviews).
var holdingTriggerBindings = map[string]struct {
	tempTable, bind, fire string
}{
	"holding_timezone_lock": {
		tempTable: `CREATE TEMP TABLE zz_holding_bind (id uuid, timezone text)`,
		bind:      `CREATE TRIGGER zz_holding_bound BEFORE UPDATE ON zz_holding_bind FOR EACH ROW EXECUTE FUNCTION public.holding_timezone_lock()`,
		fire:      `INSERT INTO zz_holding_bind VALUES (gen_random_uuid(), 'UTC'); UPDATE zz_holding_bind SET timezone = 'Europe/Malta'`,
	},
	// 00043: the gate on a temp table shaped like the columns it reads -- a row that names a site
	// is what would make it ask holding_peer, and it refuses before that (55000).
	"holding_site_gate": {
		tempTable: `CREATE TEMP TABLE zz_holding_bind_gate (tenant_id uuid, site_tenant_id uuid, site_tag_uid character(14), channel text, verdict text, sun_valid boolean, ctr integer)`,
		bind:      `CREATE TRIGGER zz_holding_bound BEFORE INSERT ON zz_holding_bind_gate FOR EACH ROW EXECUTE FUNCTION public.holding_site_gate()`,
		fire:      `INSERT INTO zz_holding_bind_gate VALUES (gen_random_uuid(), gen_random_uuid(), 'AAAAAAAAAAAAAA', 'qr', 'flag', false, NULL)`,
	},
	// 00046: the projection on a temp table shaped like a site row -- it refuses before reading a
	// field (55000), so it never writes a site row for a table that is not transactions.
	"holding_feed": {
		tempTable: `CREATE TEMP TABLE zz_holding_bind_feed (id uuid, tenant_id uuid, site_tenant_id uuid, verdict text, channel text)`,
		bind:      `CREATE TRIGGER zz_holding_bound AFTER INSERT ON zz_holding_bind_feed FOR EACH ROW EXECUTE FUNCTION public.holding_feed()`,
		fire:      `INSERT INTO zz_holding_bind_feed VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'ok', 'nfc')`,
	},
}

// holdingTriggerDefs are the holding triggers as pg_get_triggerdef prints them, keyed by the
// trigger function: each function is bound exactly once, enabled ('O'), with its WHEN (the API-1a
// review's DÜŞÜK-2 -- the timezone lock fires only when the zone changes; without the WHEN it
// would ask holding_peer on every update of a tenant).
var holdingTriggerDefs = map[string][]string{
	"holding_timezone_lock": {`CREATE TRIGGER tenants_holding_timezone_lock BEFORE UPDATE ON public.tenants FOR EACH ROW ` +
		`WHEN ((old.timezone IS DISTINCT FROM new.timezone)) EXECUTE FUNCTION holding_timezone_lock()`},
	// 00043: only a row that names a site reaches the gate (the employer's own rows never ask
	// holding_peer).
	"holding_site_gate": {`CREATE TRIGGER transactions_holding_site_gate BEFORE INSERT ON public.transactions FOR EACH ROW ` +
		`WHEN ((new.site_tenant_id IS NOT NULL)) EXECUTE FUNCTION holding_site_gate()`},
	// 00046: the one holding trigger, bound TWICE (sorted as pg_get_triggerdef prints them): a
	// transactions row that names a site, and every approval (its NEW names no site -- the body
	// looks the decided row up). TestHoldingFeed_NameOrderIsFirst pins its place among the AFTER
	// INSERT triggers of each table (ADR 0027 §8, K-31).
	"holding_feed": {
		`CREATE TRIGGER transaction_reviews_holding_feed AFTER INSERT ON public.transaction_reviews FOR EACH ROW EXECUTE FUNCTION holding_feed()`,
		`CREATE TRIGGER transactions_holding_feed AFTER INSERT ON public.transactions FOR EACH ROW ` +
			`WHEN ((new.site_tenant_id IS NOT NULL)) EXECUTE FUNCTION holding_feed()`,
	},
}

// TestHoldingTriggers_ExecuteOnlyOwnerAndAssertRelid: every trigger function of the signature
// table has EXECUTE in its owner alone, a row in holdingTriggerBindings and one binding, enabled,
// equal to its pg_get_triggerdef in holdingTriggerDefs; bound by the owner to a temp table it
// refuses to run (55000 -- TG_RELID and friends); tappa_app, which has TEMP from PUBLIC, cannot
// bind it to a table of its own at all (42501 -- the 4th round's path); and holding_timezone_lock
// bound to tenants for INSERT (the right table, the wrong event) refuses too.
func TestHoldingTriggers_ExecuteOnlyOwnerAndAssertRelid(t *testing.T) {
	ctx, tx := opTx(t)
	for _, s := range holdingSignatures {
		if s.kind != holdingTrigger {
			continue
		}
		rows, err := tx.Query(ctx, `SELECT t.tgenabled::text || ' ' || pg_get_triggerdef(t.oid) FROM pg_trigger t
		                             WHERE t.tgfoid = ('public.' || $1 || '()')::regprocedure AND NOT t.tgisinternal ORDER BY 1`, s.name)
		if err != nil {
			t.Fatal(err)
		}
		defs, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, d := range holdingTriggerDefs[s.name] {
			want = append(want, "O "+d)
		}
		slices.Sort(defs)
		slices.Sort(want)
		if len(want) == 0 || !slices.Equal(defs, want) {
			t.Errorf("%s is bound as %q,\n want exactly these, enabled: %q", s.name, defs, want)
		}
	}
	n := 0
	for _, s := range holdingSignatures {
		if s.kind != holdingTrigger {
			continue
		}
		n++
		b, ok := holdingTriggerBindings[s.name]
		if !ok {
			t.Errorf("%s: a trigger function with no row in holdingTriggerBindings", s.name)
			continue
		}
		var execute string
		if err := tx.QueryRow(ctx, `SELECT string_agg(pg_get_userbyid(a.grantee), ',' ORDER BY 1)
		                             FROM pg_proc p, aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) AS a
		                            WHERE p.oid = ('public.' || $1 || '()')::regprocedure AND a.privilege_type = 'EXECUTE'`, s.name).Scan(&execute); err != nil {
			t.Fatal(err)
		}
		if execute != "tappa_holdingdefiner" {
			t.Errorf("%s: EXECUTE held by {%s}, want its owner alone", s.name, execute)
		}
		if err := opTry(t, ctx, tx, b.tempTable+"; "+b.bind); err != nil {
			t.Fatalf("%s: bind it as the owner: %v", s.name, err)
		}
		opWant(t, opTry(t, ctx, tx, b.fire), sqlstatePrerequisiteState, s.name+" fired on a table that is not its own")
		opWant(t, opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, strings.Replace(b.tempTable, "zz_holding_bind", "zz_holding_app", 1)); err != nil {
				return err
			}
			_, err := sp.Exec(ctx, strings.ReplaceAll(b.bind, "zz_holding_bind", "zz_holding_app"))
			return err
		}), sqlstateInsufficientPrivi, "tappa_app binding "+s.name+" to a table of its own")
	}
	if n == 0 {
		t.Fatal("anti-vacuity: the signature table names no trigger")
	}
	if err := opTry(t, ctx, tx, `CREATE TRIGGER zz_holding_wrong_event BEFORE INSERT ON tenants FOR EACH ROW EXECUTE FUNCTION public.holding_timezone_lock()`); err != nil {
		t.Fatal(err)
	}
	opWant(t, opTry(t, ctx, tx, `INSERT INTO tenants (name, vat_number, business_type, structure) VALUES ('zz', $1, 'restaurant', 'single')`,
		"VAT-HOLDING-"+opToken(t)), sqlstatePrerequisiteState, "holding_timezone_lock fired for INSERT")
}

// TestHoldingFunctions_ExecuteGrantedOnlyToApp: of everything tappa_holdingdefiner owns,
// tappa_app may EXECUTE exactly the callable functions of the signature table, and no other
// role but the owner may EXECUTE any of them (PUBLIC, tappa_operator, tappa_opdefiner,
// tappa_resolver); the predicate and the triggers are the owner's alone. Since API-1b the table
// has three callables (00043) -- and a CONTROL shows the scan fires: a definer function granted
// to tappa_app and tappa_operator is flagged for tappa_operator.
func TestHoldingFunctions_ExecuteGrantedOnlyToApp(t *testing.T) {
	ctx, tx := opTx(t)
	scan := func(q opQuerier, sigs []holdingSignature) []string {
		kinds := map[string]holdingKind{}
		for _, s := range sigs {
			kinds[s.name] = s.kind
		}
		rows, err := q.Query(ctx, `
			SELECT p.proname, r.rolname, has_function_privilege(r.oid, p.oid, 'EXECUTE')
			  FROM pg_proc p
			 CROSS JOIN pg_roles r
			 WHERE p.proowner = 'tappa_holdingdefiner'::regrole
			   AND r.rolname IN ('tappa_app', 'tappa_operator', 'tappa_opdefiner', 'tappa_resolver')
			UNION ALL
			SELECT p.proname, 'PUBLIC', has_function_privilege('public', p.oid, 'EXECUTE')
			  FROM pg_proc p WHERE p.proowner = 'tappa_holdingdefiner'::regrole`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var fn, role string
			var may bool
			if err := rows.Scan(&fn, &role, &may); err != nil {
				t.Fatal(err)
			}
			kind, listed := kinds[fn]
			want := listed && kind == holdingCallable && role == "tappa_app"
			if may != want {
				out = append(out, fmt.Sprintf("%s: %s may EXECUTE=%v, want %v", fn, role, may, want))
			}
		}
		return out
	}
	for _, f := range scan(tx, holdingSignatures) {
		t.Error(f)
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`CREATE FUNCTION public.holding_zz_callable(p_uid character) RETURNS TABLE(x int) LANGUAGE sql STABLE SECURITY DEFINER
		     SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
		`ALTER FUNCTION public.holding_zz_callable(character) OWNER TO tappa_holdingdefiner`,
		`REVOKE ALL ON FUNCTION public.holding_zz_callable(character) FROM PUBLIC`,
		`GRANT EXECUTE ON FUNCTION public.holding_zz_callable(character) TO tappa_app, tappa_operator`,
	} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	callable := append(slices.Clone(holdingSignatures), holdingSignature{"holding_zz_callable", "p_uid character", "TABLE(x integer)", holdingCallable})
	got := scan(sp, callable)
	if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, "holding_zz_callable: tappa_operator") }) ||
		slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, "holding_zz_callable: tappa_app") }) {
		t.Errorf("CONTROL: a callable granted to tappa_app and tappa_operator gave %v; want tappa_operator flagged, tappa_app not", got)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// ------------------------------------------------------------- the definer's reach --

// holdingDefinerMatrix is ADR 0027 §3's verb x column matrix AS FAR AS API-1a, API-1b, API-1c, API-2
// AND API-8 HAVE EARNED IT: "table:VERB" -> the exact column list in attnum order. Every relation,
// verb and column not listed is "no privilege". Each grant came with the function that needs it (ADR
// 0027 §3: "matris kesin listenin üst sınırıdır" -- API-8's 00055 brought employees' last four);
// TestHoldingDefiner_EveryGrantIsUsed revokes each column below, one at a time, and shows a
// function, the gate or the feed refused without it.
var holdingDefinerMatrix = map[string]string{
	"holding_members:SELECT": "holding_id,member_tenant_id,joined_at,left_at",
	// 00046 (API-1c): holding_feed -- the projection's columns, the two conflict targets (their
	// columns and their predicates' -- a targeted ON CONFLICT needs SELECT on both, measured), the
	// approval path's read of the decided row, and the employer's snapshot of who tapped.
	"site_punches:INSERT": "tenant_id,transaction_id,employer_tenant_id,employer_api_code,employee_id,employee_ref,employee_code," +
		"employee_name,site_location_id,site_tag_uid,ctr,sun_valid,bucket_hour,occurred_at,channel,type,outcome,ctr_gap",
	"site_punches:SELECT": "tenant_id,employee_id,site_tag_uid,ctr,sun_valid,bucket_hour,outcome",
	// 00049 (API-2) adds verdict and practice: K-4 on the approval arm.
	"transactions:SELECT": "id,tenant_id,employee_id,ctr,type,occurred_at,sun_valid,verdict,channel,practice,site_tenant_id,site_location_id,site_tag_uid",
	// 00055 (API-8) adds status, first_name, last_name and updated_at: holding_directory -- the ADR's row,
	// complete. email is K-34's "never SELECT" (TestHoldingDefiner_NeverSelectColumns).
	"employees:SELECT":       "id,tenant_id,full_name,status,external_ref,employee_code,first_name,last_name,updated_at",
	"holding_members:UPDATE": "lock_anchor",
	// 00043 (API-1b): holding_tap_site, holding_advance_tag_counter, holding_logo, holding_site_gate.
	"tags:SELECT":            "uid,tenant_id,location_id,last_ctr,status",
	"tags:UPDATE":            "last_ctr",
	"locations:SELECT":       "id,tenant_id,name,static_ips,gps_lat,gps_lng,shift_start,shift_end,overnight,external_ref",
	"tenant_branding:SELECT": "tenant_id,accent,logo,logo_sha256,logo_mime,logo_width,logo_height",
	// 00049 (API-2): api_enabled_at -- both targets' gate (K-22); the feed's four rows (K-31): the
	// endpoint's id, tenant and switch (never its url or key), the counters made to exist, locked
	// and raised, the two rows written.
	"tenants:SELECT":            "id,name,timezone,vat_verified,api_enabled_at,api_code",
	"webhook_endpoints:SELECT":  "tenant_id,id,active",
	"feed_counters:SELECT":      "tenant_id,last_seq",
	"feed_counters:INSERT":      "tenant_id,last_seq",
	"feed_counters:UPDATE":      "last_seq",
	"punch_events:INSERT":       feedEventInsertColumns,
	"webhook_deliveries:INSERT": "tenant_id,endpoint_id,punch_event_seq",
}

// holdingDefinerReachFindings: tappa_holdingdefiner's privileges on every relation in public
// (SELECT/INSERT/UPDATE by column, against matrix; DELETE/TRUNCATE/REFERENCES/TRIGGER by table:
// never), on every sequence (none), on every schema but public (none; on public USAGE and not
// CREATE), and EXECUTE on functions it does not own (only PUBLIC's, and none a SECURITY DEFINER).
func holdingDefinerReachFindings(t *testing.T, ctx context.Context, q opQuerier, matrix map[string]string) []string {
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
			if got, want := opColumns(t, ctx, q, "tappa_holdingdefiner", table, priv), matrix[table+":"+priv]; got != want {
				out = append(out, fmt.Sprintf("%s on %s = (%s), want (%s)", priv, table, got, want))
			}
		}
		for _, priv := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			var may bool
			if err := q.QueryRow(ctx, `SELECT has_table_privilege('tappa_holdingdefiner', 'public.' || quote_ident($1), $2)`, table, priv).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may {
				out = append(out, fmt.Sprintf("%s on %s", priv, table))
			}
		}
	}
	for _, check := range []struct{ what, sql string }{
		{"a sequence privilege", `SELECT s.relname FROM pg_class s WHERE s.relkind = 'S'
			AND (has_sequence_privilege('tappa_holdingdefiner', s.oid, 'USAGE') OR has_sequence_privilege('tappa_holdingdefiner', s.oid, 'SELECT')
			     OR has_sequence_privilege('tappa_holdingdefiner', s.oid, 'UPDATE'))`},
		{"a schema privilege beyond USAGE on public", `SELECT n.nspname FROM pg_namespace n
			WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
			  AND (has_schema_privilege('tappa_holdingdefiner', n.oid, 'CREATE')
			       OR (n.nspname <> 'public' AND has_schema_privilege('tappa_holdingdefiner', n.oid, 'USAGE')))`},
		{"EXECUTE on another role's function PUBLIC may not call, or on a SECURITY DEFINER", `SELECT p.oid::regprocedure::text FROM pg_proc p
			WHERE p.proowner <> 'tappa_holdingdefiner'::regrole
			  AND has_function_privilege('tappa_holdingdefiner', p.oid, 'EXECUTE')
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
	var usage bool
	if err := q.QueryRow(ctx, `SELECT has_schema_privilege('tappa_holdingdefiner', 'public', 'USAGE')`).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if !usage {
		out = append(out, "no USAGE on schema public (01-roles.sql's HOLDING ROLE block grants it)")
	}
	return out
}

// TestHoldingDefiner_ColumnMatrix is K-34's matrix on the catalogue (has_column_privilege /
// has_table_privilege, which count membership): tappa_holdingdefiner's whole reach is
// holdingDefinerMatrix. CONTROLS (savepoints): a column grant outside it (tags.aes_key_ref), a
// table-level UPDATE on holding_members (which would cover the membership columns), a DELETE, a
// schema CREATE and EXECUTE on a resolver are each flagged.
func TestHoldingDefiner_ColumnMatrix(t *testing.T) {
	ctx, tx := opTx(t)
	for _, f := range holdingDefinerReachFindings(t, ctx, tx, holdingDefinerMatrix) {
		t.Error(f)
	}
	for _, c := range []struct{ name, sql, want string }{
		{"a key column", `GRANT SELECT (aes_key_ref) ON tags TO tappa_holdingdefiner`, "SELECT on tags"},
		{"UPDATE on the whole membership table", `GRANT UPDATE ON holding_members TO tappa_holdingdefiner`, "UPDATE on holding_members"},
		{"DELETE on the membership table", `GRANT DELETE ON holding_members TO tappa_holdingdefiner`, "DELETE on holding_members"},
		{"CREATE on public", `GRANT CREATE ON SCHEMA public TO tappa_holdingdefiner`, "schema privilege"},
		{"EXECUTE on a resolver", `GRANT EXECUTE ON FUNCTION public.resolve_tag_by_uid(character) TO tappa_holdingdefiner`, "resolve_tag_by_uid"},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, c.sql); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := holdingDefinerReachFindings(t, ctx, sp, holdingDefinerMatrix); !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, c.want) }) {
			t.Errorf("CONTROL %s: not flagged (%v)", c.name, got)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// holdingNeverSelect is K-34's "asla SELECT" list (ADR 0027 §3) by name; every column whose name
// ends in _hash is added from the catalogue. A table the list names that is not born yet
// (webhook_endpoints is API-2's, api_keys API-3's) is checked the day it exists.
var holdingNeverSelect = [][2]string{
	{"tags", "aes_key_ref"}, {"tags", "app_key_ref"}, {"employees", "email"}, {"admin_users", "email"},
	{"platform_admins", "email"}, {"platform_admins", "totp_secret_sealed"},
	{"webhook_endpoints", "url"}, {"webhook_endpoints", "signing_key_sealed"},
	{"transactions", "gps_lat"}, {"transactions", "gps_lng"}, {"transactions", "source_ip"},
	{"transactions", "note"}, {"transactions", "policy_context"},
	{"transaction_reviews", "note"},
}

// TestHoldingDefiner_NeverSelectColumns pins K-34's list one column at a time
// (has_column_privilege(..., 'SELECT') = false), plus every *_hash column the catalogue holds --
// by query, not by name (at least ten today: the eight ADR 0027 §3 names and the two enrollment
// and consent hashes) -- and holds holdingDefinerMatrix itself to the list (no SELECT entry may
// name one). CONTROLS: a SELECT on sessions.token_hash and a table-level SELECT on transactions
// are flagged.
func TestHoldingDefiner_NeverSelectColumns(t *testing.T) {
	ctx, tx := opTx(t)
	scan := func(q opQuerier) (findings []string, hashes int) {
		cols := slices.Clone(holdingNeverSelect)
		rows, err := q.Query(ctx, `SELECT c.relname, a.attname FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		                            WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
		                              AND a.attnum > 0 AND NOT a.attisdropped AND a.attname LIKE '%\_hash' ORDER BY 1, 2`)
		if err != nil {
			t.Fatal(err)
		}
		pairs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) ([2]string, error) {
			var p [2]string
			err := r.Scan(&p[0], &p[1])
			return p, err
		})
		if err != nil {
			t.Fatal(err)
		}
		cols = append(cols, pairs...)
		for _, c := range cols {
			var exists bool
			if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_attribute a JOIN pg_class r ON r.oid = a.attrelid
			                             WHERE r.relnamespace = 'public'::regnamespace AND r.relname = $1 AND a.attname = $2 AND NOT a.attisdropped)`,
				c[0], c[1]).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if !exists {
				continue
			}
			var may bool
			if err := q.QueryRow(ctx, `SELECT has_column_privilege('tappa_holdingdefiner', 'public.' || $1, $2, 'SELECT')`, c[0], c[1]).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may {
				findings = append(findings, c[0]+"."+c[1])
			}
			if granted, ok := holdingDefinerMatrix[c[0]+":SELECT"]; ok && slices.Contains(strings.Split(granted, ","), c[1]) {
				findings = append(findings, "holdingDefinerMatrix names "+c[0]+"."+c[1])
			}
		}
		return findings, len(pairs)
	}
	findings, hashes := scan(tx)
	for _, f := range findings {
		t.Errorf("tappa_holdingdefiner may SELECT %s -- on K-34's never list", f)
	}
	if hashes < 10 {
		t.Fatalf("anti-vacuity: the catalogue query found %d *_hash columns", hashes)
	}
	for _, c := range []struct{ sql, want string }{
		{`GRANT SELECT (token_hash) ON sessions TO tappa_holdingdefiner`, "sessions.token_hash"},
		{`GRANT SELECT ON transactions TO tappa_holdingdefiner`, "transactions.source_ip"},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, c.sql); err != nil {
			t.Fatal(err)
		}
		if got, _ := scan(sp); !slices.Contains(got, c.want) {
			t.Errorf("CONTROL %q: not flagged (%v)", c.sql, got)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// TestHoldingDefiner_LockAnchorIsTheOnlyMembershipUpdate is ADR 0027 §3's "kilit çapası":
// tappa_holdingdefiner's ONLY UPDATE on holding_members is lock_anchor (no membership column, no
// table-level UPDATE); as the definer, writing lock_anchor = true is 23514 (the CHECK leaves one
// value: the one already there), writing false is accepted and changes nothing, writing a
// membership column is 42501; and no body it owns writes holding_members.
func TestHoldingDefiner_LockAnchorIsTheOnlyMembershipUpdate(t *testing.T) {
	ctx, tx := opTx(t)
	if got := opColumns(t, ctx, tx, "tappa_holdingdefiner", "holding_members", "UPDATE"); got != "lock_anchor" {
		t.Errorf("tappa_holdingdefiner UPDATE on holding_members = (%s), want (lock_anchor)", got)
	}
	var tableWide bool
	if err := tx.QueryRow(ctx, `SELECT has_table_privilege('tappa_holdingdefiner', 'public.holding_members', 'UPDATE')`).Scan(&tableWide); err != nil {
		t.Fatal(err)
	}
	if tableWide {
		t.Error("tappa_holdingdefiner holds a table-level UPDATE on holding_members")
	}
	h := holdingNew(t, ctx, tx, "api1a anchor "+opToken(t))
	tenant := opNewTenant(t, ctx, tx, "api1a anchor "+opToken(t), 0)
	holdingInsertMember(t, ctx, tx, h, holdingMember{tenant, "1 second", ""})
	opWant(t, opExecAs(t, ctx, tx, "tappa_holdingdefiner", `UPDATE public.holding_members SET lock_anchor = true WHERE member_tenant_id = $1`, tenant),
		sqlstateCheckViolation, "the definer writing lock_anchor = true")
	if err := opExecAs(t, ctx, tx, "tappa_holdingdefiner", `UPDATE public.holding_members SET lock_anchor = false WHERE member_tenant_id = $1`, tenant); err != nil {
		t.Errorf("CONTROL: the definer cannot write the anchor's one value: %v", err)
	}
	for _, col := range []string{"left_at = clock_timestamp()", "holding_id = gen_random_uuid()", "member_tenant_id = gen_random_uuid()", "joined_at = clock_timestamp()"} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_holdingdefiner", `UPDATE public.holding_members SET `+col+` WHERE member_tenant_id = $1`, tenant),
			sqlstateInsufficientPrivi, "the definer writing "+col)
	}
	write := regexp.MustCompile(`(?i)(update|insert\s+into|delete\s+from|merge\s+into)\s+(public\.)?holding_members`)
	for sig, body := range holdingDefinerBodies(t, ctx, tx) {
		if m := write.FindString(strings.ReplaceAll(body, `"`, "")); m != "" {
			t.Errorf("%s writes holding_members (%q)", sig, m)
		}
	}

	// WHY THE ANCHOR EXISTS AND WHY holding_peer IS VOLATILE -- ADR 0027 "Bağlam"'s two
	// measurements, re-measured here (each in a savepoint): the lock mode works as the definer
	// holds today; with UPDATE (lock_anchor) revoked, its FOR SHARE is 42501 (a row lock needs
	// UPDATE on some column of the table); and with the function made STABLE it is refused
	// outright (a locking SELECT in a non-volatile function).
	other := opNewTenant(t, ctx, tx, "api1a anchor peer "+opToken(t), 0)
	holdingInsertMember(t, ctx, tx, h, holdingMember{other, "1 second", ""})
	if _, err := holdingPeerAs(t, ctx, tx, tenant, other, "lock"); err != nil {
		t.Fatalf("CONTROL: the lock mode as the definer holds today: %v", err)
	}
	for _, c := range []struct{ setup, code, what string }{
		{`REVOKE UPDATE (lock_anchor) ON holding_members FROM tappa_holdingdefiner`, sqlstateInsufficientPrivi, "the lock mode without the anchor's UPDATE"},
		{`ALTER FUNCTION public.holding_peer(uuid, uuid, text) STABLE`, "0A000", "the lock mode in a STABLE holding_peer"},
	} {
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, c.setup); err != nil {
				t.Fatal(err)
			}
			_, err := holdingPeerAs(t, ctx, sp, tenant, other, "lock")
			opWant(t, err, c.code, c.what)
		})
	}
}

// TestHoldingDefiner_RoleShape is ADR 0027 §2's role: NOLOGIN, BYPASSRLS, NOSUPERUSER, NOCREATEDB,
// NOCREATEROLE, NOREPLICATION; no member and a member of nothing; no role-level setting; no
// default privilege naming it; it owns no relation, type or schema -- only the functions of the
// signature table, each with its fixed proconfig (the catalogue test pins those).
func TestHoldingDefiner_RoleShape(t *testing.T) {
	ctx, tx := opTx(t)
	var login, super, bypass, createdb, createrole, repl bool
	var members, memberOf, settings, defaults, owned int64
	if err := tx.QueryRow(ctx, `
		SELECT r.rolcanlogin, r.rolsuper, r.rolbypassrls, r.rolcreatedb, r.rolcreaterole, r.rolreplication,
		       (SELECT count(*) FROM pg_auth_members m WHERE m.roleid = r.oid),
		       (SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid),
		       (SELECT count(*) FROM pg_db_role_setting s WHERE s.setrole = r.oid),
		       (SELECT count(*) FROM pg_default_acl d WHERE d.defaclrole = r.oid OR d.defaclacl::text ~ '(^|[{,])tappa_holdingdefiner='),
		       (SELECT count(*) FROM pg_class c WHERE c.relowner = r.oid)
		       + (SELECT count(*) FROM pg_type y WHERE y.typowner = r.oid)
		       + (SELECT count(*) FROM pg_namespace n WHERE n.nspowner = r.oid)
		  FROM pg_roles r WHERE r.rolname = 'tappa_holdingdefiner'`).Scan(&login, &super, &bypass, &createdb, &createrole, &repl,
		&members, &memberOf, &settings, &defaults, &owned); err != nil {
		t.Fatalf("read tappa_holdingdefiner: %v", err)
	}
	if login || super || !bypass || createdb || createrole || repl {
		t.Errorf("tappa_holdingdefiner login=%v super=%v bypassrls=%v createdb=%v createrole=%v replication=%v; want NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION",
			login, super, bypass, createdb, createrole, repl)
	}
	if members != 0 || memberOf != 0 || settings != 0 || defaults != 0 || owned != 0 {
		t.Errorf("tappa_holdingdefiner: %d member(s), member of %d, %d role setting(s), %d default privilege(s), owns %d relation/type/schema(s); want 0 each",
			members, memberOf, settings, defaults, owned)
	}
}

// --------------------------------------------------------------- canary and peer set --

// holdingCanary is the distinctive evidence ADR 0027 §2's canary test seeds into a peer's
// location: a TEST-NET address and a GPS pair that appear nowhere else (the decimal spellings
// are what a leak would carry).
var holdingCanary = struct {
	cidr, lat, lng string
	needles        []string
}{"198.51.100.77/32", "35.111327", "14.222391", []string{"198.51.100.77", "35.111327", "14.222391"}}

// holdingCanaryContext is a policy context carrying the canary beside the one key holding_feed
// reads (the same three needles as holdingCanary, spelled out: a JSON document, not a format).
const holdingCanaryContext = `{"tap:ctrGap": 0, "tap:ipMatch": "198.51.100.77/32", "tap:gpsDistanceM": 35.111327, "tap:gpsHint": 14.222391}`

// holdingCanaryCall is how one callable holding_* is called in the PEER's context: the
// statement (run as tappa_app with app.tenant_id = the caller tenant) and its arguments, built
// from the fixture. exempt are the result columns the canary may appear in (ADR 0027 §2: only
// holding_tap_site's static_ips, gps_lat, gps_lng -- K-39's server-side decision inputs).
type holdingCanaryCall struct {
	sql    string
	args   func(f holdingCanaryFixture) []any
	exempt []string
}

// holdingCanaryFixture: the caller (KM) and its peer (KF) in one holding; KF's location carries
// the canary, and since API-1b a plaque of KF's hangs there (uid) and KF -- VIES verified -- holds
// a logo (digest), so every callable of the tap path answers a row in the caller's context. Since
// API-8 KF employs a person whose e-mail address carries the canary (K-34's "never SELECT" column),
// so holding_directory answers a row and its cells are searched for the address too.
type holdingCanaryFixture struct {
	caller, peer, location uuid.UUID
	uid, digest            string
}

// holdingCanaryCalls: API-1b's three (00043), each with its call; API-1c adds
// holding_site_locations, API-8 holding_directory (TestHoldingFunctions_SiteEvidenceCanaryNeverLeaks
// is red on a callable function the map does not name, and on a call that answers no row -- a
// canary that reads nothing proves nothing).
var holdingCanaryCalls = map[string]holdingCanaryCall{
	"holding_tap_site": {
		sql:    `SELECT * FROM public.holding_tap_site($1)`,
		args:   func(f holdingCanaryFixture) []any { return []any{f.uid} },
		exempt: []string{"static_ips", "gps_lat", "gps_lng"},
	},
	"holding_advance_tag_counter": {
		sql:  `SELECT * FROM public.holding_advance_tag_counter($1, $2)`,
		args: func(f holdingCanaryFixture) []any { return []any{f.uid, int32(901)} },
	},
	"holding_logo": {
		sql:  `SELECT * FROM public.holding_logo($1, $2)`,
		args: func(f holdingCanaryFixture) []any { return []any{f.uid, f.digest} },
	},
	// 00047 (API-1c): the peer's venues, the canary venue among them -- no exemption (K-39).
	"holding_site_locations": {
		sql:  `SELECT * FROM public.holding_site_locations(NULL)`,
		args: func(holdingCanaryFixture) []any { return nil },
	},
	// 00055 (API-8): the peer's employees, the canary's e-mail holder among them -- no exemption.
	"holding_directory": {
		sql:  `SELECT * FROM public.holding_directory(NULL, NULL)`,
		args: func(holdingCanaryFixture) []any { return nil },
	},
}

// holdingWrittenTables are the tables the holding trigger writes (ADR 0027 §3, "yazılan
// tablolar"): the canary is looked for in every row of each one that exists.
var holdingWrittenTables = []string{"site_punches", "punch_events", "webhook_deliveries", "feed_counters"}

// holdingCanaryFindings reports every cell of rows whose text carries a canary needle, outside
// the exempt columns, and how many rows it searched.
func holdingCanaryFindings(what string, rows pgx.Rows, exempt []string) ([]string, int, error) {
	defer rows.Close()
	var out []string
	fields := rows.FieldDescriptions()
	n := 0
	for rows.Next() {
		n++
		vals, err := rows.Values()
		if err != nil {
			return nil, n, err
		}
		for i, v := range vals {
			name := string(fields[i].Name)
			if slices.Contains(exempt, name) || v == nil {
				continue
			}
			text := fmt.Sprint(v)
			for _, n := range holdingCanary.needles {
				if strings.Contains(text, n) {
					out = append(out, fmt.Sprintf("%s: column %s carries %s", what, name, n))
				}
			}
		}
	}
	return out, n, rows.Err()
}

// TestHoldingFunctions_SiteEvidenceCanaryNeverLeaks is ADR 0027 §2's canary test as a skeleton
// that runs today: the canary is seeded into a PEER tenant's location (inside the test's
// transaction), every callable holding_* of holdingCanaryCalls is called in the caller's context
// and every output cell is searched for the address and the GPS decimals (holding_tap_site's
// three evidence columns aside), and every existing table of holdingWrittenTables is searched
// whole. Since API-1b the three tap callables are called (each must answer a row, or the canary
// searched nothing); no written table exists yet (API-1c/API-2). The anti-vacuity is the
// catalogue: every function tappa_holdingdefiner owns that tappa_app may EXECUTE must have a
// call here. CONTROLS (the ADR's mutant and its exemption): a holding_site_locations-shaped probe
// returning location_name || static_ips is flagged; a holding_tap_site-shaped probe returning the
// canary in static_ips / gps_lat / gps_lng is NOT; the same probe returning it in location_name
// IS.
func TestHoldingFunctions_SiteEvidenceCanaryNeverLeaks(t *testing.T) {
	ctx, tx := opTx(t)
	var f holdingCanaryFixture
	f.caller = opNewTenant(t, ctx, tx, "api1a canary caller "+opToken(t), 0)
	f.peer = opNewTenant(t, ctx, tx, "api1a canary peer "+opToken(t), 0)
	h := holdingNew(t, ctx, tx, "api1a canary "+opToken(t))
	holdingInsertMember(t, ctx, tx, h, holdingMember{f.caller, "1 minute", ""})
	holdingInsertMember(t, ctx, tx, h, holdingMember{f.peer, "1 minute", ""})
	if err := tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name, static_ips, gps_lat, gps_lng)
	                            VALUES ($1, 'api1a canary venue', ARRAY[$2::cidr], $3::numeric, $4::numeric) RETURNING id`,
		f.peer, holdingCanary.cidr, holdingCanary.lat, holdingCanary.lng).Scan(&f.location); err != nil {
		t.Fatalf("seed the canary: %v", err)
	}
	// API-1b: a plaque of the peer's at the canary venue, and the peer VIES verified with a logo.
	f.uid = holdingNewTag(t, ctx, tx, f.peer, &f.location, "active", 900)
	f.digest = holdingNewLogo(t, ctx, tx, f.peer, true)
	// API-8: a person of the peer's whose e-mail address carries every needle (employees.email is on
	// K-34's "never SELECT" list) -- holding_directory answers the row, never the address.
	if _, err := tx.Exec(ctx, `INSERT INTO employees (tenant_id, external_ref, full_name, email, status, invited_at)
	                           VALUES ($1, $2, 'Canary Staff', $3, 'active', now())`,
		f.peer, "canary-"+opToken(t), "canary-"+strings.Join(holdingCanary.needles, "-")+"@example.test"); err != nil {
		t.Fatalf("seed the canary's e-mail holder: %v", err)
	}
	// API-1c: the caller's employee taps the peer's plaque at the canary venue -- ok, flagged then
	// approved, and refused -- with the canary in EVERY evidence column of the employer's row
	// (source_ip, the phone's GPS, the note, the policy context beside the one key the projection
	// reads), so the rows holding_feed writes into the peer's site_punches are searched below.
	employee := holdingNewEmployee(t, ctx, tx, f.caller, "active")
	reviewer := holdingNewAdmin(t, ctx, tx, f.caller)
	// API-2: both APIs open with an active endpoint, so the ok tap and the approval are written into
	// the two remaining tables too -- punch_events and webhook_deliveries (and the counters).
	feedOpen(t, ctx, tx, f.caller, "KM", true)
	feedOpen(t, ctx, tx, f.peer, "KF", true)
	before := opInt(t, ctx, tx, `SELECT count(*) FROM site_punches WHERE tenant_id = $1`, f.peer)
	var flagged uuid.UUID
	if err := holdingAppIn(t, ctx, tx, f.caller, func(sp pgx.Tx) error {
		for _, v := range []struct{ verdict, kind string }{{"ok", "in"}, {"flag", "out"}, {"reject", ""}} {
			var id uuid.UUID
			if err := sp.QueryRow(ctx, `
				INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, source_ip,
				                          gps_lat, gps_lng, note, policy_context, site_tenant_id, site_location_id, site_tag_uid)
				VALUES ($1, $2, now(), $3, 'nfc', NULLIF($4, ''), true, 900, $5::inet, $6::numeric, $7::numeric, $8, $9::jsonb, $10, $11, $12)
				RETURNING id`,
				f.caller, employee, v.verdict, v.kind, holdingCanary.needles[0], holdingCanary.lat, holdingCanary.lng,
				"canary "+strings.Join(holdingCanary.needles, " "),
				holdingCanaryContext,
				f.peer, f.location, f.uid).Scan(&id); err != nil {
				return err
			}
			if v.verdict == "flag" {
				flagged = id
			}
		}
		_, err := sp.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome, note) VALUES ($1, $2, $3, 'approved', $4)`,
			f.caller, flagged, reviewer, "canary "+strings.Join(holdingCanary.needles, " "))
		return err
	}); err != nil {
		t.Fatalf("the canary holding taps: %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM site_punches WHERE tenant_id = $1`, f.peer) - before; n != 4 {
		t.Fatalf("anti-vacuity: the canary taps wrote %d site_punches rows, want 4 (counted, pending_review, refused, approved)", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM punch_events e JOIN webhook_deliveries d ON d.tenant_id = e.tenant_id AND d.punch_event_seq = e.seq
	                            WHERE e.punch_tenant_id = $1`, f.caller); n != 4 {
		t.Fatalf("anti-vacuity: the canary taps wrote %d delivered events, want 4 (the ok tap and the approval, each in both feeds)", n)
	}

	callFindings := func(q pgx.Tx, calls map[string]holdingCanaryCall) []string {
		var out []string
		for name, c := range calls {
			err := opAs(t, ctx, q, "tappa_app", func(sp pgx.Tx) error {
				if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, f.caller.String()); err != nil {
					return err
				}
				rows, err := sp.Query(ctx, c.sql, c.args(f)...)
				if err != nil {
					return err
				}
				got, n, err := holdingCanaryFindings(name, rows, c.exempt)
				out = append(out, got...)
				if err == nil && n == 0 {
					// A canary call that reads nothing proves nothing (anti-vacuity).
					out = append(out, name+": answered no row in the peer's context, so nothing was searched")
				}
				return err
			})
			if err != nil {
				out = append(out, name+": "+err.Error())
			}
		}
		return out
	}
	for _, x := range callFindings(tx, holdingCanaryCalls) {
		t.Error(x)
	}
	// Anti-vacuity: every callable holding_* has its call here.
	rows, err := tx.Query(ctx, `SELECT p.proname FROM pg_proc p WHERE p.proowner = 'tappa_holdingdefiner'::regrole
	                             AND has_function_privilege('tappa_app', p.oid, 'EXECUTE') ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	callable, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range callable {
		if _, ok := holdingCanaryCalls[name]; !ok {
			t.Errorf("%s is callable by tappa_app and has no canary call in holdingCanaryCalls", name)
		}
	}
	for _, table := range holdingWrittenTables {
		if opInt(t, ctx, tx, `SELECT count(*) FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relname = $1`, table) == 0 {
			continue
		}
		rows, err := tx.Query(ctx, `SELECT * FROM public.`+pgx.Identifier{table}.Sanitize())
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := holdingCanaryFindings(table, rows, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range got {
			t.Error(x)
		}
	}

	// CONTROLS: probes owned by the definer, granted to tappa_app, reading the canary location by
	// id (the probe stands for a body; its grants are the owner's -- created in a savepoint, the
	// SELECT on locations given to the definer there too).
	for _, c := range []struct {
		name, ddl string
		exempt    []string
		flagged   bool
	}{
		{"location_name || static_ips (the 1st round's mutant)", `CREATE FUNCTION public.holding_zz_canary(p_location uuid)
			RETURNS TABLE(location_id uuid, location_name text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
			AS 'SELECT l.id, l.name || array_to_string(l.static_ips, '','') FROM public.locations AS l WHERE l.id = p_location'`, nil, true},
		{"GPS in a renamed column", `CREATE FUNCTION public.holding_zz_canary(p_location uuid)
			RETURNS TABLE(location_id uuid, shift_hint text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
			AS 'SELECT l.id, l.gps_lat::text FROM public.locations AS l WHERE l.id = p_location'`, nil, true},
		{"holding_tap_site's three evidence columns (exempt)", `CREATE FUNCTION public.holding_zz_canary(p_location uuid)
			RETURNS TABLE(location_id uuid, static_ips cidr[], gps_lat numeric, gps_lng numeric) LANGUAGE sql STABLE SECURITY DEFINER
			SET search_path = pg_catalog, pg_temp
			AS 'SELECT l.id, l.static_ips, l.gps_lat, l.gps_lng FROM public.locations AS l WHERE l.id = p_location'`,
			[]string{"static_ips", "gps_lat", "gps_lng"}, false},
		{"the same exemption, the canary in location_name", `CREATE FUNCTION public.holding_zz_canary(p_location uuid)
			RETURNS TABLE(location_name text, static_ips cidr[]) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
			AS 'SELECT l.name || array_to_string(l.static_ips, '',''), l.static_ips FROM public.locations AS l WHERE l.id = p_location'`,
			[]string{"static_ips", "gps_lat", "gps_lng"}, true},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{
			`GRANT SELECT (id, name, static_ips, gps_lat, gps_lng) ON locations TO tappa_holdingdefiner`,
			c.ddl,
			`ALTER FUNCTION public.holding_zz_canary(uuid) OWNER TO tappa_holdingdefiner`,
			`REVOKE ALL ON FUNCTION public.holding_zz_canary(uuid) FROM PUBLIC`,
			`GRANT EXECUTE ON FUNCTION public.holding_zz_canary(uuid) TO tappa_app`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		}
		got := callFindings(sp, map[string]holdingCanaryCall{"holding_zz_canary": {
			sql:    `SELECT * FROM public.holding_zz_canary($1)`,
			args:   func(f holdingCanaryFixture) []any { return []any{f.location} },
			exempt: c.exempt,
		}})
		if (len(got) > 0) != c.flagged {
			t.Errorf("CONTROL %s: findings %v, want flagged=%v", c.name, got, c.flagged)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// holdingMultiRowCall is how one multi-row callable holding_* is driven: its call (run as
// tappa_app in the caller's context) and a statement that adds data rows it returns for the
// peer ($1 = the peer tenant) -- locations for holding_site_locations, employees for
// holding_directory.
type holdingMultiRowCall struct {
	sql, grow string
}

// holdingMultiRowCalls are the callable holding_* that return many rows (ADR 0027 §2's
// "Maliyet": holding_site_locations -- API-1c -- and holding_directory -- API-8). Each must compute
// the peer set ONCE (a materialised CTE over tenants), not call holding_peer per data row.
var holdingMultiRowCalls = map[string]holdingMultiRowCall{
	// 00047 (API-1c): the peer's venues.
	"holding_site_locations": {
		sql:  `SELECT * FROM public.holding_site_locations(NULL)`,
		grow: `INSERT INTO locations (tenant_id, name) SELECT $1, 'api1c peerset ' || g FROM generate_series(1, 50) AS g`,
	},
	// 00055 (API-8): the peer's employees (with refs: K-14 needs a home venue or a ref).
	"holding_directory": {
		sql: `SELECT * FROM public.holding_directory(NULL, NULL)`,
		grow: `INSERT INTO employees (tenant_id, external_ref, full_name, status, invited_at)
		       SELECT $1, 'api8-peerset-' || gen_random_uuid(), 'Peerset Staff', 'active', now() FROM generate_series(1, 50) AS g`,
	},
}

// holdingSingleRowCalls are the set-returning callables keyed by ONE primary key -- a plaque's
// uid (tags_pkey) -- so at most one data row exists to answer: API-1b's three (00043). For them
// "the peer set once" is "holding_peer exactly once per call", and that is what is counted: the
// key's tenant against the caller's, in one materialised CTE ($1 is the peer's plaque).
var holdingSingleRowCalls = map[string]string{
	"holding_tap_site":            `SELECT * FROM public.holding_tap_site('%s')`,
	"holding_advance_tag_counter": `SELECT * FROM public.holding_advance_tag_counter('%s', 2000000)`,
	"holding_logo":                `SELECT * FROM public.holding_logo('%s', repeat('0', 64))`,
}

// holdingPeerCalls counts holding_peer's calls made by stmt (run as tappa_app in tenant's
// context), with function tracking on for the test's transaction (track_functions is the
// superuser's to set -- the owner session is one).
func holdingPeerCalls(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, stmt string) int64 {
	t.Helper()
	var calls int64
	count := `SELECT coalesce((SELECT calls FROM pg_stat_xact_user_functions
	                            WHERE funcid = 'public.holding_peer(uuid, uuid, text)'::regprocedure), 0)`
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
			return err
		}
		before := opInt(t, ctx, sp, count)
		rows, err := sp.Query(ctx, stmt)
		if err != nil {
			return err
		}
		for rows.Next() {
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		calls = opInt(t, ctx, sp, count) - before
		return nil
	}); err != nil {
		t.Fatalf("count holding_peer's calls: %v", err)
	}
	return calls
}

// holdingPeerSetFindings drives each call twice, around 50 more data rows for the peer: a
// function that computes the peer set once makes as many holding_peer calls the second time as
// the first; one that asks per data row makes at least 50 more.
//
// The peer is given its first 50 data rows BEFORE the first count (the API-1a review's DÜŞÜK-1):
// with no data row at all the count depends on the plan, not on the function -- measured on a
// seedless database (empty locations): a hash join that finds its other side empty stops after the
// first peer the CTE yields or never reads the CTE ("63 then 173", "0 then 2", "0 then 230"), and
// the CONTROL that computes the peer set once was flagged. With data on both sides every join
// strategy reads the materialised peer set to its end.
func holdingPeerSetFindings(t *testing.T, ctx context.Context, tx pgx.Tx, caller, peer uuid.UUID, calls map[string]holdingMultiRowCall) []string {
	t.Helper()
	var out []string
	for name, c := range calls {
		if _, err := tx.Exec(ctx, c.grow, peer); err != nil {
			t.Fatalf("%s: the peer's first data rows: %v", name, err)
		}
		first := holdingPeerCalls(t, ctx, tx, caller, c.sql)
		if _, err := tx.Exec(ctx, c.grow, peer); err != nil {
			t.Fatalf("%s: grow the data: %v", name, err)
		}
		second := holdingPeerCalls(t, ctx, tx, caller, c.sql)
		if first == 0 || second != first {
			out = append(out, fmt.Sprintf("%s: %d holding_peer calls, then %d after 50 more data rows -- the peer set is not computed once", name, first, second))
		}
	}
	return out
}

// TestHoldingFunctions_PeerSetComputedOnce is ADR 0027 §2's "eş kümesi bir kez" (2nd round: 5.54 s
// -> 42 ms) as a call count: a multi-row holding_* calls holding_peer the same number of times
// however many data rows it returns -- once per peer CANDIDATE, never once per data row. No
// multi-row function exists yet (API-1c, API-8); the anti-vacuity is the catalogue (every
// set-returning holding_* tappa_app may call is in exactly one of holdingMultiRowCalls and
// holdingSingleRowCalls), API-1b's three single-row callables are counted at exactly one
// holding_peer call each, and the CONTROLS show the measure works: a probe that asks holding_peer
// for every location row grows with the rows (flagged), a probe that computes the peer set once in
// a materialised CTE does not.
func TestHoldingFunctions_PeerSetComputedOnce(t *testing.T) {
	ctx, tx := opTx(t)
	if _, err := tx.Exec(ctx, `SET LOCAL track_functions = 'all'`); err != nil {
		t.Fatalf("track_functions: %v", err)
	}
	caller := opNewTenant(t, ctx, tx, "api1a peerset caller "+opToken(t), 0)
	peer := opNewTenant(t, ctx, tx, "api1a peerset peer "+opToken(t), 0)
	h := holdingNew(t, ctx, tx, "api1a peerset "+opToken(t))
	holdingInsertMember(t, ctx, tx, h, holdingMember{caller, "1 minute", ""})
	holdingInsertMember(t, ctx, tx, h, holdingMember{peer, "1 minute", ""})
	rows, err := tx.Query(ctx, `SELECT p.proname FROM pg_proc p WHERE p.proowner = 'tappa_holdingdefiner'::regrole AND p.proretset
	                             AND has_function_privilege('tappa_app', p.oid, 'EXECUTE')`)
	if err != nil {
		t.Fatal(err)
	}
	multi, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range multi {
		_, isMulti := holdingMultiRowCalls[name]
		_, isSingle := holdingSingleRowCalls[name]
		if isMulti == isSingle {
			t.Errorf("%s returns rows to tappa_app and is in %d of holdingMultiRowCalls / holdingSingleRowCalls, want exactly one", name, map[bool]int{true: 2, false: 0}[isMulti])
		}
	}
	for _, x := range holdingPeerSetFindings(t, ctx, tx, caller, peer, holdingMultiRowCalls) {
		t.Error(x)
	}
	// The single-row callables: a peer's plaque, and holding_peer asked exactly once per call.
	var wall uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'api1b peerset venue') RETURNING id`, peer).Scan(&wall); err != nil {
		t.Fatal(err)
	}
	uid := holdingNewTag(t, ctx, tx, peer, &wall, "active", 10)
	for name, stmt := range holdingSingleRowCalls {
		if n := holdingPeerCalls(t, ctx, tx, caller, fmt.Sprintf(stmt, uid)); n != 1 {
			t.Errorf("%s asked holding_peer %d times for one plaque, want exactly 1 (the peer set once)", name, n)
		}
	}
	grow := `INSERT INTO locations (tenant_id, name) SELECT $1, 'api1a peerset ' || g FROM generate_series(1, 50) AS g`
	for _, c := range []struct {
		name, body string
		flagged    bool
	}{
		{"holding_peer per data row", `SELECT l.id FROM public.locations AS l
			WHERE public.holding_peer(NULLIF(current_setting('app.tenant_id', true), '')::uuid, l.tenant_id, 'strict')`, true},
		{"the peer set once", `WITH peers AS MATERIALIZED (
			    SELECT t.id FROM public.tenants AS t
			     WHERE public.holding_peer(NULLIF(current_setting('app.tenant_id', true), '')::uuid, t.id, 'strict'))
			SELECT l.id FROM public.locations AS l JOIN peers AS p ON p.id = l.tenant_id`, false},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{
			`GRANT SELECT (id, tenant_id) ON locations TO tappa_holdingdefiner`,
			`GRANT SELECT (id) ON tenants TO tappa_holdingdefiner`,
			`CREATE FUNCTION public.holding_zz_multi() RETURNS TABLE(location_id uuid) LANGUAGE sql VOLATILE SECURITY DEFINER
			     SET search_path = pg_catalog, pg_temp AS $q$` + c.body + `$q$`,
			`ALTER FUNCTION public.holding_zz_multi() OWNER TO tappa_holdingdefiner`,
			`REVOKE ALL ON FUNCTION public.holding_zz_multi() FROM PUBLIC`,
			`GRANT EXECUTE ON FUNCTION public.holding_zz_multi() TO tappa_app`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		}
		got := holdingPeerSetFindings(t, ctx, sp, caller, peer, map[string]holdingMultiRowCall{
			"holding_zz_multi": {sql: `SELECT * FROM public.holding_zz_multi()`, grow: grow}})
		if (len(got) > 0) != c.flagged {
			t.Errorf("CONTROL %s: findings %v, want flagged=%v", c.name, got, c.flagged)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
