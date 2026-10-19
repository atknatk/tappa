package db

// holdinggroup_test.go -- GRP-1 at the database: migration 00062 (holding_peer's roster form and
// holding_group, the panel's Group section's one read of the holding) against real Postgres,
// everything rolled back (opTx). ADR 0027 §2 (the function contract, the closed list, the one
// predicate), §3 (the matrix: holdings (id, name)), K-1d and its GRP-1 note. The catalogue, the body
// scan, the matrix, the canary, the call count and K-33's mutants reach the two functions through the
// shared tables of holdingcatalog_test.go and holdingtap_test.go.

import (
	"context"
	"fmt"
	mrand "math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// holdingPeerRosterBodyMD5 is md5(prosrc) of holding_peer's roster form as migration 00062 creates it
// -- the other two forms' pin: a changed rule is red here until an ADR 0027 change the user approved
// moves it.
const holdingPeerRosterBodyMD5 = "7cb1c21f916e80550cab2a478c11da11"

// holdingGroupSource returns one CREATE statement of 00062's Up (the controls mutate it), made
// CREATE OR REPLACE.
func holdingGroupSource(t *testing.T, fn string) string {
	t.Helper()
	up, _ := opMigrationSections(t, op00062File)
	src := regexp.MustCompile(`(?s)CREATE FUNCTION public\.` + regexp.QuoteMeta(fn) + `\(.*?\n\$\$;`).FindString(up)
	if src == "" {
		t.Fatalf("PREMISE: 00062's %s not found", fn)
	}
	return strings.Replace(src, "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
}

// holdingGroupWorld: H holds KF (code KF), KM (code KM), X (no code), F (a membership that ends in the
// future -- live by the wall clock) and L (left 10 seconds ago: inside K-29's window, strict no). H2
// holds C and D. Z is in no holding.
type holdingGroupWorld struct {
	h, h2                 uuid.UUID
	kf, km, x, f, l, c, d uuid.UUID
	z                     uuid.UUID
	hName, h2Name         string
	name                  map[uuid.UUID]string
}

func holdingGroupWorldTx(t *testing.T, ctx context.Context, tx pgx.Tx) holdingGroupWorld {
	t.Helper()
	tok := opToken(t)
	w := holdingGroupWorld{name: map[uuid.UUID]string{}, hName: "grp1 H " + tok, h2Name: "grp1 H2 " + tok}
	tenant := func(label string, code any) uuid.UUID {
		name := "grp1 " + label + " " + tok
		id := opNewTenant(t, ctx, tx, name, 0)
		if code != nil {
			if _, err := tx.Exec(ctx, `UPDATE tenants SET api_code = $2 WHERE id = $1`, id, code); err != nil {
				t.Fatalf("api_code: %v", err)
			}
		}
		w.name[id] = name
		return id
	}
	w.kf, w.km, w.x, w.f, w.l = tenant("KF", "KF"), tenant("KM", "KM"), tenant("X", nil), tenant("F", "FF"), tenant("L", "LL")
	w.c, w.d, w.z = tenant("C", "CC"), tenant("D", nil), tenant("Z", "ZZ")
	w.h, w.h2 = holdingNew(t, ctx, tx, w.hName), holdingNew(t, ctx, tx, w.h2Name)
	for _, m := range []struct {
		h uuid.UUID
		m holdingMember
	}{
		{w.h, holdingMember{w.kf, "30 days", ""}},
		{w.h, holdingMember{w.km, "20 days", ""}},
		{w.h, holdingMember{w.x, "10 days", ""}},
		{w.h, holdingMember{w.f, "5 days", "-1 hour"}},
		{w.h, holdingMember{w.l, "40 days", "10 seconds"}},
		{w.h2, holdingMember{w.c, "3 days", ""}},
		{w.h2, holdingMember{w.d, "2 days", ""}},
	} {
		holdingInsertMember(t, ctx, tx, m.h, m.m)
	}
	return w
}

// holdingGroupIn reads holding_group through the Go reader (HoldingOn(...).Group), as tappa_app in
// tenant's context (uuid.Nil: none), each row as "holding|member|code|self".
func holdingGroupIn(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID) ([]string, error) {
	t.Helper()
	var out []string
	err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		rows, err := HoldingOn(sp).Group(ctx)
		for _, r := range rows {
			code := "-"
			if r.APICode != nil {
				code = *r.APICode
			}
			out = append(out, fmt.Sprintf("%s|%s|%s|%v", r.HoldingName, r.MemberName, code, r.IsSelf))
		}
		return err
	})
	return out, err
}

// holdingGroupFindings is everything the world's contexts must (and must not) read through
// holding_group: each live member of H reads H's live members (itself first and marked, the left
// member never), C reads H2, and L (left), Z (no holding) and no context read nothing.
func holdingGroupFindings(t *testing.T, ctx context.Context, tx pgx.Tx, w holdingGroupWorld) []string {
	t.Helper()
	row := func(holding string, m uuid.UUID, code string, self bool) string {
		return fmt.Sprintf("%s|%s|%s|%v", holding, w.name[m], code, self)
	}
	hMembers := func(self uuid.UUID) []string {
		var out []string
		codes := map[uuid.UUID]string{w.kf: "KF", w.km: "KM", w.x: "-", w.f: "FF"}
		out = append(out, row(w.hName, self, codes[self], true))
		var rest []string
		for _, m := range []uuid.UUID{w.kf, w.km, w.x, w.f} {
			if m != self {
				rest = append(rest, row(w.hName, m, codes[m], false))
			}
		}
		slices.Sort(rest) // the names share the token, so the order is the label's
		return append(out, rest...)
	}
	var out []string
	for _, c := range []struct {
		who  string
		ctx  uuid.UUID
		want []string
	}{
		{"KM", w.km, hMembers(w.km)},
		{"KF", w.kf, hMembers(w.kf)},
		{"F (leaves in an hour)", w.f, hMembers(w.f)},
		{"C (H2)", w.c, []string{row(w.h2Name, w.c, "CC", true), row(w.h2Name, w.d, "-", false)}},
		{"L (left H 10 s ago)", w.l, nil},
		{"Z (no holding)", w.z, nil},
		{"no context", uuid.Nil, nil},
	} {
		got, err := holdingGroupIn(t, ctx, tx, c.ctx)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: %v", c.who, err))
			continue
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			out = append(out, fmt.Sprintf("%s read\n  %s\nwant\n  %s", c.who, strings.Join(got, "\n  "), strings.Join(c.want, "\n  ")))
		}
	}
	return out
}

// TestHoldingGroup_AMemberReadsItsOwnHoldingAndNothingElse is GRP-1's behaviour: a live member reads
// its holding's name and its LIVE members -- itself first and marked, the others by name, each with
// its company code (none for X) -- and never the member that left 10 s ago (inside K-29's window:
// strict says gone); a member whose own left_at is an hour ahead is still live. H2's member reads H2
// alone (one holding never sees another's), and the member that left, a business in no holding and
// no context read NOTHING -- the same answer. The joined dates are the memberships' own.
//
// MUTANTS, each in a savepoint by CREATE OR REPLACE of 00062's own text, each red here:
//   - the roster form without the caller's own live-membership condition (the "is_member" gate):
//     the member that left reads its old holding;
//   - the roster form without the member's live-membership condition: the left member is listed;
//   - holding_group reading every live membership instead of the caller's roster (the tenant
//     filter gone): a business in no holding reads other holdings.
func TestHoldingGroup_AMemberReadsItsOwnHoldingAndNothingElse(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingGroupWorldTx(t, ctx, tx)
	for _, f := range holdingGroupFindings(t, ctx, tx, w) {
		t.Error(f)
	}
	// The dates: each member's joined_at is its membership's, to the microsecond.
	if err := holdingAppIn(t, ctx, tx, w.km, func(sp pgx.Tx) error {
		rows, err := HoldingOn(sp).Group(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.JoinedAt.IsZero() || r.HoldingID != w.h {
				t.Errorf("KM's row %+v: a zero joined date or another holding", r)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var joined []time.Time
	if err := tx.QueryRow(ctx, `SELECT array_agg(joined_at ORDER BY member_tenant_id) FROM holding_members WHERE member_tenant_id = ANY($1)`,
		[]uuid.UUID{w.kf}).Scan(&joined); err != nil {
		t.Fatal(err)
	}
	if err := holdingAppIn(t, ctx, tx, w.km, func(sp pgx.Tx) error {
		rows, err := HoldingOn(sp).Group(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.MemberName == w.name[w.kf] && !r.JoinedAt.Equal(joined[0]) {
				t.Errorf("KF joined %v, holding_group says %v", joined[0], r.JoinedAt)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	roster := holdingGroupSource(t, "holding_peer")
	group := holdingGroupSource(t, "holding_group")
	for _, c := range []struct {
		name, src, from, to string
	}{
		{"the roster form without the caller's live-membership condition", roster,
			"           AND (ma.left_at IS NULL OR ma.left_at > v_now)\n", ""},
		{"the roster form without the member's live-membership condition", roster,
			"           AND (mb.left_at IS NULL OR mb.left_at > v_now)\n", ""},
		{"holding_group reading every live membership", group,
			"CROSS JOIN LATERAL public.holding_peer(c.id) AS r",
			"CROSS JOIN LATERAL (SELECT m.holding_id, m.member_tenant_id, m.joined_at FROM public.holding_members AS m WHERE m.left_at IS NULL) AS r"},
	} {
		t.Run("mutant/"+c.name, func(t *testing.T) {
			holdingInSavepoint(t, tx, func(sp pgx.Tx) {
				if strings.Count(c.src, c.from) != 1 {
					t.Fatalf("PREMISE: %q is not in the shipped text once", c.from)
				}
				if _, err := sp.Exec(ctx, strings.Replace(c.src, c.from, c.to, 1)); err != nil {
					t.Fatal(err)
				}
				if got := holdingGroupFindings(t, ctx, sp, w); len(got) == 0 {
					t.Error("MUTANT: every context still read what it should -- the test does not see it")
				}
			})
		})
	}
}

// TestHoldingGroup_TheContextIsTheOnlyInput: holding_group takes no argument; an empty app.tenant_id
// and an id no tenant has answer no row (the same answer as a business in no holding), and a value
// that is not a uuid is the cast's 22P02 -- whose message carries the value it was given and nothing
// of any holding or tenant (the sibling functions' shape: the context is the session's, never a
// client's). Roles other than tappa_app and the owner may not call it (42501).
func TestHoldingGroup_TheContextIsTheOnlyInput(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingGroupWorldTx(t, ctx, tx)
	for _, guc := range []string{"", uuid.NewString()} {
		var n int64
		if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
			if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, guc); err != nil {
				return err
			}
			return sp.QueryRow(ctx, `SELECT count(*) FROM public.holding_group()`).Scan(&n)
		}); err != nil || n != 0 {
			t.Errorf("app.tenant_id %q: %d rows (%v), want none", guc, n, err)
		}
	}
	err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', 'not-a-uuid', true)`); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `SELECT * FROM public.holding_group()`)
		return err
	})
	opWant(t, err, "22P02", "a malformed app.tenant_id")
	_, msg := opCode(err)
	for _, never := range []string{w.hName, w.h2Name, w.name[w.kf], w.h.String(), w.kf.String()} {
		if strings.Contains(err.Error(), never) || strings.Contains(msg, never) {
			t.Errorf("the malformed context's error carries %q: %v", never, err)
		}
	}
	for _, role := range []string{"tappa_operator", "tappa_opdefiner", "tappa_resolver", "tappa_feedwriter"} {
		opWant(t, opExecAs(t, ctx, tx, role, `SELECT * FROM public.holding_group()`), sqlstateInsufficientPrivi, role+" calling holding_group")
	}
	var args, result string
	if err := tx.QueryRow(ctx, `SELECT pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid) FROM pg_proc p
	                            WHERE p.oid = 'public.holding_group()'::regprocedure`).Scan(&args, &result); err != nil {
		t.Fatal(err)
	}
	if args != "" {
		t.Errorf("holding_group takes (%s); the caller is app.tenant_id, never an argument", args)
	}
	// The result is the closed list's (the signature table pins it too); none of K-1d's "never".
	for _, never := range []string{"vat", "email", "address", "employee", "plan", "billing", "api_enabled", "timezone", "left_at", "tenant_id"} {
		if strings.Contains(strings.ToLower(result), never) {
			t.Errorf("holding_group's result %s names %q", result, never)
		}
	}
}

// TestHoldingGroup_AVATColumnIsRedInTheCatalogue: holding_group widened by a VAT number (the K-1d
// "never" list) is red in ADR 0027 §2's catalogue test -- the result is not the table's row.
func TestHoldingGroup_AVATColumnIsRedInTheCatalogue(t *testing.T) {
	ctx, tx := opTx(t)
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		for _, s := range []string{
			`DROP FUNCTION public.holding_group()`,
			`CREATE FUNCTION public.holding_group()
			     RETURNS TABLE(holding_id uuid, holding_name text, member_name text, member_api_code text,
			                   joined_at timestamptz, is_self boolean, member_vat text)
			     LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
			     AS 'SELECT NULL::uuid, NULL::text, NULL::text, NULL::text, NULL::timestamptz, NULL::boolean, NULL::text WHERE false'`,
			`ALTER FUNCTION public.holding_group() OWNER TO tappa_holdingdefiner`,
			`REVOKE ALL ON FUNCTION public.holding_group() FROM PUBLIC`,
			`GRANT EXECUTE ON FUNCTION public.holding_group() TO tappa_app`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		got, err := holdingCatalogFindings(ctx, sp, holdingSignatures)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, "holding_group") && strings.Contains(f, "(ii)") }) {
			t.Errorf("a VAT column in holding_group's result is not flagged: %v", got)
		}
	})
}

// TestHoldingPeer_TheRosterFormIsStrictAndTheOwnersAlone: the roster form's catalogue row -- owned by
// tappa_holdingdefiner, SECURITY INVOKER, VOLATILE, the search path alone, EXECUTE in its owner alone
// (tappa_app, tappa_operator, tappa_opdefiner and tappa_resolver are 42501), ROWS 10 -- its body pinned
// by md5 and holding no mode, no row lock and no write; a NULL caller is no row.
func TestHoldingPeer_TheRosterFormIsStrictAndTheOwnersAlone(t *testing.T) {
	ctx, tx := opTx(t)
	var src, digest, owner, vol, config, execute string
	var secdef bool
	var rows float32
	if err := tx.QueryRow(ctx, `
		SELECT p.prosrc, md5(p.prosrc), pg_get_userbyid(p.proowner), p.provolatile::text, array_to_string(p.proconfig, '|'), p.prosecdef,
		       (SELECT string_agg(pg_get_userbyid(a.grantee), ',' ORDER BY 1)
		          FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) AS a WHERE a.privilege_type = 'EXECUTE'),
		       p.prorows
		  FROM pg_proc p WHERE p.oid = 'public.holding_peer(uuid)'::regprocedure`).
		Scan(&src, &digest, &owner, &vol, &config, &secdef, &execute, &rows); err != nil {
		t.Fatalf("read the roster form: %v", err)
	}
	if digest != holdingPeerRosterBodyMD5 {
		t.Errorf("holding_peer's roster form changed (md5 %s, want %s): %s", digest, holdingPeerRosterBodyMD5, holdingPeerChangeNeeds)
	}
	if owner != "tappa_holdingdefiner" || secdef || vol != "v" || config != "search_path=pg_catalog, pg_temp" || execute != "tappa_holdingdefiner" || rows != 10 {
		t.Errorf("the roster form: owner %s DEFINER %v provolatile %s proconfig %q EXECUTE {%s} ROWS %v", owner, secdef, vol, config, execute, rows)
	}
	body := strings.ReplaceAll(holdingStripComments(src), `"`, "")
	if m := holdingPeerForbiddenRE.FindString(body); m != "" {
		t.Errorf("the roster form holds %q", strings.TrimSpace(m))
	}
	if m := regexp.MustCompile(`(?i)\bfor\s+(share|update|no\s+key)|\bmod\b`).FindString(body); m != "" {
		t.Errorf("the roster form holds %q (a row lock or a mode)", m)
	}
	for _, role := range []string{"tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_resolver"} {
		opWant(t, opExecAs(t, ctx, tx, role, `SELECT * FROM public.holding_peer($1::uuid)`, uuid.New()),
			sqlstateInsufficientPrivi, role+" calling holding_peer's roster form")
	}
	if err := opAs(t, ctx, tx, "tappa_holdingdefiner", func(sp pgx.Tx) error {
		var n int64
		if err := sp.QueryRow(ctx, `SELECT count(*) FROM public.holding_peer(NULL::uuid)`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("roster(NULL) answered %d rows", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// holdingPeerRosterDivergences compares, as the owner, the roster form with the set form over every
// scenario of w (holdingpeerset_test.go's random world): for every caller (each pool tenant, the
// outsider, NULL) the roster's members other than the caller are EXACTLY the set form's peers; the
// caller is in its own roster exactly when it holds a live membership; every roster row's holding is
// one the caller is a live member of; and no (holding, member) is answered twice.
func holdingPeerRosterDivergences(t *testing.T, ctx context.Context, q pgx.Tx, w holdingPeerWorld) ([]string, [3]int64) {
	t.Helper()
	var out []string
	var st [3]int64 // callers with a peer, callers in their own roster, rows compared
	if err := opAs(t, ctx, q, "tappa_holdingdefiner", func(o pgx.Tx) error {
		return o.QueryRow(ctx, `
			WITH pool AS (SELECT * FROM unnest($1::int[], $2::uuid[]) AS p(scn, id)),
			     scns AS (SELECT DISTINCT scn FROM pool),
			     callers AS (SELECT scn, id AS a FROM pool
			                 UNION ALL SELECT scn, $3::uuid FROM scns
			                 UNION ALL SELECT scn, NULL::uuid FROM scns),
			     r AS MATERIALIZED (
			         SELECT c.scn, c.a,
			                coalesce((SELECT array_agg(p.id ORDER BY p.id) FROM public.holding_peer(c.a, 'strict') AS p(id)), '{}') AS peers,
			                coalesce((SELECT array_agg(DISTINCT x.member_tenant_id ORDER BY x.member_tenant_id)
			                            FROM public.holding_peer(c.a) AS x WHERE x.member_tenant_id IS DISTINCT FROM c.a), '{}') AS roster,
			                EXISTS (SELECT 1 FROM public.holding_peer(c.a) AS x WHERE x.member_tenant_id = c.a) AS self_in,
			                EXISTS (SELECT 1 FROM public.holding_members AS m
			                         WHERE m.member_tenant_id = c.a AND (m.left_at IS NULL OR m.left_at > clock_timestamp())) AS live,
			                (SELECT count(*) - count(DISTINCT (x.holding_id, x.member_tenant_id)) FROM public.holding_peer(c.a) AS x) AS twice,
			                (SELECT count(*) FROM public.holding_peer(c.a) AS x
			                  WHERE NOT EXISTS (SELECT 1 FROM public.holding_members AS m
			                                     WHERE m.member_tenant_id = c.a AND m.holding_id = x.holding_id
			                                       AND (m.left_at IS NULL OR m.left_at > clock_timestamp()))) AS foreign_holding,
			                (SELECT count(*) FROM public.holding_peer(c.a)) AS n
			           FROM callers AS c)
			SELECT coalesce((SELECT array_agg(format('scenario %s, caller %s: set %s, roster %s, self in roster %s, live %s, twice %s, foreign %s',
			                                         x.scn, coalesce(x.a::text, 'NULL'), x.peers, x.roster, x.self_in, x.live, x.twice, x.foreign_holding))
			                   FROM (SELECT * FROM r WHERE peers <> roster OR self_in <> live OR twice <> 0 OR foreign_holding <> 0 LIMIT 5) AS x), '{}'),
			       (SELECT count(*) FROM r WHERE cardinality(peers) > 0),
			       (SELECT count(*) FROM r WHERE self_in),
			       (SELECT sum(n) FROM r)`,
			w.scenario, w.tenant, w.outsider).Scan(&out, &st[0], &st[1], &st[2])
	}); err != nil {
		t.Fatalf("compare the roster and set forms: %v", err)
	}
	return out, st
}

// TestHoldingPeer_TheRosterFormIsTheSetFormsAnswer is GRP-1's property test, T124's in shape: 200 random
// scenarios (holdingPeerNewWorld -- live, ended, in-the-window and future-ending memberships, rejoins,
// moves, a tenant live in a holding twice) and, for every caller, the roster form answers the set
// form's peers plus the caller itself exactly when it is live -- the one predicate kept in one place.
// The seed is logged. CONTROLS, each over the same scenarios in a savepoint, each found diverging: the
// roster LOOSENED (the member's exit ignored), the roster without the caller (the self-join dropped),
// the roster with the holding equality forgotten, and the roster without its GROUP BY (a member live
// twice in one holding answered twice).
func TestHoldingPeer_TheRosterFormIsTheSetFormsAnswer(t *testing.T) {
	ctx, tx := opTx(t)
	seed := uint64(time.Now().UnixNano())
	t.Logf("seed %d", seed)
	rng := mrand.New(mrand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	const scenarios = 200
	w := holdingPeerNewWorld(t, ctx, tx, rng, scenarios, 7, 3)
	diverged, st := holdingPeerRosterDivergences(t, ctx, tx, w)
	for _, d := range diverged {
		t.Errorf("seed %d: %s", seed, d)
	}
	t.Logf("%d scenarios: %d callers with a peer, %d callers in their own roster, %d roster rows", scenarios, st[0], st[1], st[2])
	if st[0] < 200 || st[1] < 200 || st[2] < 500 {
		t.Fatalf("anti-vacuity: the scenarios exercised too little: %v", st)
	}
	roster := holdingGroupSource(t, "holding_peer")
	for _, c := range []struct{ name, from, to string }{
		{"the roster loosened: the member's exit ignored", "           AND (mb.left_at IS NULL OR mb.left_at > v_now)\n", ""},
		{"the roster without the caller", "         WHERE ma.member_tenant_id = a\n", "         WHERE ma.member_tenant_id = a AND mb.member_tenant_id <> a\n"},
		{"the holding equality forgotten", "JOIN public.holding_members AS mb ON mb.holding_id = ma.holding_id", "JOIN public.holding_members AS mb ON true"},
		{"the GROUP BY dropped", "SELECT mb.holding_id, mb.member_tenant_id, min(mb.joined_at)", "SELECT mb.holding_id, mb.member_tenant_id, mb.joined_at"},
	} {
		t.Run("control/"+c.name, func(t *testing.T) {
			holdingPeerControlSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if strings.Count(roster, c.from) != 1 {
					t.Fatalf("PREMISE: %q is not in the roster form once", c.from)
				}
				body := strings.Replace(roster, c.from, c.to, 1)
				if strings.Contains(c.name, "GROUP BY") {
					body = strings.Replace(body, "\n         GROUP BY mb.holding_id, mb.member_tenant_id;", ";", 1)
				}
				if _, err := sp.Exec(ctx, body); err != nil {
					t.Fatal(err)
				}
				if d, _ := holdingPeerRosterDivergences(t, ctx, sp, w); len(d) == 0 {
					t.Errorf("CONTROL: %d scenarios found no divergence (seed %d)", scenarios, seed)
				}
			})
		})
	}
}

// holdingGroupSurface is what 00062 changes, read off the catalogue: the roster form and holding_group
// (body md5, result, owner, ACL) or "-", the count of holding_peer forms, and the definer's SELECT on
// holdings.
func holdingGroupSurface(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `SELECT
		coalesce((SELECT md5(prosrc) || ' ' || pg_get_function_result(oid) || ' ' || pg_get_userbyid(proowner) || ' ' ||
		                 coalesce(array_to_string(proacl, ','), '') FROM pg_proc WHERE oid = to_regprocedure('public.holding_peer(uuid)')), '-') || ' | ' ||
		coalesce((SELECT md5(prosrc) || ' ' || pg_get_function_result(oid) || ' ' || pg_get_userbyid(proowner) || ' ' ||
		                 coalesce(array_to_string(proacl, ','), '') FROM pg_proc WHERE oid = to_regprocedure('public.holding_group()')), '-') || ' | ' ||
		(SELECT count(*) FROM pg_proc WHERE proname = 'holding_peer')::text`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s + " | holdings:" + opColumns(t, ctx, q, "tappa_holdingdefiner", "holdings", "SELECT")
}

// TestHolding00062_DownGivesBackAndUpTakesItAgain: 00062's Down drops holding_group and the roster form
// and takes the definer's SELECT on holdings back -- 00061's surface -- and its Up gives HEAD's surface
// back exactly. The precondition refuses a second run (the functions are there), a third holding_peer
// of another shape, and a definer that already reads holdings (the Down takes the table's SELECT back
// whole, so it may only take what this file gave).
func TestHolding00062_DownGivesBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00062File)
	pre := holdingPrecondition(t, op00062File)
	head := holdingGroupSurface(t, ctx, tx)
	if !strings.HasPrefix(head, holdingPeerRosterBodyMD5+" TABLE(holding_id uuid") || !strings.HasSuffix(head, " | 3 | holdings:id,name") {
		t.Fatalf("PREMISE at HEAD: %s", head)
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00062's precondition at HEAD")
		opRunSection(t, ctx, sp, down, "00062 Down")
		if got := holdingGroupSurface(t, ctx, sp); got != "- | - | 2 | holdings:" {
			t.Errorf("after 00062's Down: %s", got)
		}
		if err := holdingNestedRun(t, ctx, sp, pre); err != nil {
			t.Fatalf("00062's precondition at 00061: %v", err)
		}
		for _, c := range []struct{ name, ddl string }{
			{"a third holding_peer of another shape", `CREATE FUNCTION public.holding_peer(a uuid) RETURNS boolean LANGUAGE sql AS 'SELECT false'`},
			{"a definer that already reads holdings", `GRANT SELECT (name) ON holdings TO tappa_holdingdefiner`},
			{"a holding_group already there", `CREATE FUNCTION public.holding_group() RETURNS boolean LANGUAGE sql AS 'SELECT false'`},
		} {
			holdingInSavepoint(t, sp, func(bad pgx.Tx) {
				if _, err := bad.Exec(ctx, c.ddl); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
				opWant(t, holdingNestedRun(t, ctx, bad, pre), sqlstatePrerequisiteState, "00062's precondition over "+c.name)
			})
		}
		opRunSection(t, ctx, sp, up, "00062 Up again")
		if got := holdingGroupSurface(t, ctx, sp); got != head {
			t.Errorf("after the Up again:\n %s\nwant HEAD's\n %s", got, head)
		}
	})
}
