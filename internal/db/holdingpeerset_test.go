package db

// holdingpeerset_test.go -- T124 at the database: migration 00060 (holding_peer's set form, and
// holding_site_locations and holding_directory reading the peer set through it) against real Postgres,
// everything rolled back (opTx). ADR 0027 §2 (the one predicate, K-33, the closed list) and its "T124
// uygulama notu". The catalogue, the body scan, the matrix, the canary, the call count and K-33's
// mutants reach the set form through the shared tables of holdingcatalog_test.go, holdingtap_test.go
// and holdingdirectory_test.go.

import (
	"context"
	"fmt"
	mrand "math/rand/v2"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// holdingPeerSetBodyMD5 is md5(prosrc) of holding_peer's set form as migration 00060 creates it. Like
// the pair form's pin (holdingPeerBodyMD5): a changed rule or a second mode is red here until an ADR
// 0027 change the user approved moves it.
const holdingPeerSetBodyMD5 = "15d124d22a6ca199ab3d387088d7c9b1"

// holdingPeerSetSource is the set form's CREATE statement as 00060's Up writes it (the controls below
// mutate it).
func holdingPeerSetSource(t *testing.T) string {
	t.Helper()
	up, _ := opMigrationSections(t, op00060File)
	src := regexp.MustCompile(`(?s)CREATE FUNCTION public\.holding_peer\(a uuid, mod text\).*?\n\$\$;`).FindString(up)
	if src == "" {
		t.Fatal("PREMISE: 00060's set form not found")
	}
	return strings.Replace(src, "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
}

// holdingPeerScenario is one random state of a pool of tenants in a few holdings: every tenant gets
// zero to three memberships, each in a random holding of the scenario's own, live (at most one: the
// unique index), ended long ago, ended inside the 60-second window (is_member yes, strict no), or
// ending in the future (a left_at past the wall clock -- strict says live; the schema allows it, and
// with it a tenant live in two holdings at once). Instants are seconds before the wall clock
// (negative: after it); the future ones are at least five minutes ahead, the window's at most 50 s
// behind, so no answer sits on the strict edge while the test runs.
type holdingPeerScenario struct {
	holding, member []uuid.UUID
	joinedAgo       []float64
	leftAgo         []*float64
}

func (s *holdingPeerScenario) add(rng *mrand.Rand, pool, holdings []uuid.UUID) {
	for _, m := range pool {
		n := rng.IntN(4)
		live := rng.IntN(2) == 0
		used := map[int]bool{}
		for i := range n {
			var left *float64
			if !live || i > 0 {
				var l float64
				switch rng.IntN(3) {
				case 0:
					l = float64(120 + rng.IntN(60000)) // ended long ago
				case 1:
					l = float64(5 + rng.IntN(45)) // inside the 60-second window
				default:
					l = -float64(300 + rng.IntN(6000)) // ends in the future
				}
				left = &l
			}
			base := 0
			if left != nil && *left > 0 {
				base = int(*left)
			}
			j := base + 1 + rng.IntN(30000)
			for used[j] {
				j++
			}
			used[j] = true
			s.holding = append(s.holding, holdings[rng.IntN(len(holdings))])
			s.member = append(s.member, m)
			s.joinedAgo = append(s.joinedAgo, float64(j))
			s.leftAgo = append(s.leftAgo, left)
		}
	}
}

// holdingPeerWorld is many scenarios side by side in ONE transaction: each its own pool of tenants and
// its own holdings, so no scenario can see another's (the comparison also checks that no set reaches
// outside its scenario's pool). Built with a handful of statements and no savepoint per scenario: a
// rolled-back savepoint per scenario left hundreds of aborted subtransactions and their dead
// membership rows behind, and every later visibility check paid for them (measured: the same test
// 10 s on a fresh database, 42 s and then 180 s -- the context's end -- as the dead rows of earlier
// runs piled up).
type holdingPeerWorld struct {
	scenario []int32     // the scenario of each pool tenant
	tenant   []uuid.UUID // the pool tenants
	outsider uuid.UUID   // a tenant in no holding, a caller and a candidate of every scenario
	unknown  uuid.UUID   // an id no tenant has, a candidate of every scenario
}

func holdingPeerNewWorld(t *testing.T, ctx context.Context, tx pgx.Tx, rng *mrand.Rand, scenarios, poolSize, holdingsEach int) holdingPeerWorld {
	t.Helper()
	w := holdingPeerWorld{outsider: opNewTenant(t, ctx, tx, "t124 outsider "+opToken(t), 0), unknown: uuid.New()}
	var holdings []uuid.UUID
	var memberships holdingPeerScenario
	for i := range scenarios {
		pool := make([]uuid.UUID, poolSize)
		for j := range pool {
			pool[j] = uuid.New()
			w.scenario, w.tenant = append(w.scenario, int32(i)), append(w.tenant, pool[j])
		}
		hs := make([]uuid.UUID, holdingsEach)
		for j := range hs {
			hs[j] = uuid.New()
		}
		holdings = append(holdings, hs...)
		memberships.add(rng, pool, hs)
	}
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure, plan, created_at)
		  SELECT x, 't124 pool ' || x, 'VAT-T124-' || x, 'restaurant', 'multi', 'standard', clock_timestamp() + interval '1 day'
		    FROM unnest($1::uuid[]) AS x`, []any{w.tenant}},
		{`INSERT INTO holdings (id, name) SELECT x, 't124 ' || x FROM unnest($1::uuid[]) AS x`, []any{holdings}},
		{`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at, left_at)
		  SELECT u.h, u.m, clock_timestamp() - make_interval(secs => u.j),
		         CASE WHEN u.l IS NULL THEN NULL ELSE clock_timestamp() - make_interval(secs => u.l) END
		    FROM unnest($1::uuid[], $2::uuid[], $3::float8[], $4::float8[]) AS u(h, m, j, l)`,
			[]any{memberships.holding, memberships.member, memberships.joinedAgo, memberships.leftAgo}},
	} {
		if _, err := tx.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("build the scenarios: %v", err)
		}
	}
	// ANALYZE inside this transaction counts the rows it just wrote (they are its own), and its lock
	// (SHARE UPDATE EXCLUSIVE, to the end of the transaction) keeps autovacuum's ANALYZE off the table
	// until the comparisons are done. T139: autovacuum re-analyzes holding_members about once a minute
	// during `go test ./...` (measured in the server log), from another session, to which these rows
	// are in progress and not live -- so the statistics said "empty" (reltuples 0 over ~50 pages,
	// measured) while the comparison walked ~4 000 rows. auto_explain caught the plan it then picks:
	// per caller, an index walked end to end with the caller as a FILTER (holding_members_pkey for
	// `member_tenant_id <> a`, holding_members_holding_idx -- (holding_id, member_tenant_id) -- for
	// `member_tenant_id = a`), nested -- quadratic in the scenarios: 24 s for 60 of them, past the
	// 180 s context for the property's 400 (measured with an ANALYZE every 2 s from another session).
	// The page and tuple counts ANALYZE writes stay past the rollback (they are written in place):
	// until the next autovacuum ANALYZE they describe a bigger table than the committed one, which
	// only tilts other sessions' plans toward the indexes.
	if _, err := tx.Exec(ctx, `ANALYZE public.holding_members`); err != nil {
		t.Fatalf("analyze holding_members: %v", err)
	}
	return w
}

// holdingPeerSetStats is what a comparison saw, for the anti-vacuity.
type holdingPeerSetStats struct {
	pairs, peers, windowOnly, futureLive, twoWays int64
}

// holdingPeerSetDivergences compares, as the owner (the only role that may call either form), the set
// form with the pair form over every scenario of w: for every caller (each pool tenant, the outsider,
// a NULL caller) and every candidate (each pool tenant -- the caller itself among them --, the
// outsider, an unknown id), b in holding_peer(a, 'strict') exactly when holding_peer(a, b, 'strict');
// no set holds a tenant outside its scenario's pool; and no set holds a tenant twice (a set is a set:
// a peer reached through two memberships -- the caller's live one and its future-ending one in the
// same holding -- is one peer, and the two callers read the set straight into their rows). Returns the
// first divergences (none when the two agree) and what the comparison exercised.
func holdingPeerSetDivergences(t *testing.T, ctx context.Context, q pgx.Tx, w holdingPeerWorld) ([]string, holdingPeerSetStats) {
	t.Helper()
	var out []string
	var st holdingPeerSetStats
	if err := opAs(t, ctx, q, "tappa_holdingdefiner", func(o pgx.Tx) error {
		const world = `
			WITH pool AS (SELECT * FROM unnest($1::int[], $2::uuid[]) AS p(scn, id)),
			     scns AS (SELECT DISTINCT scn FROM pool),
			     callers AS (SELECT scn, id AS a FROM pool
			                 UNION ALL SELECT scn, $3::uuid FROM scns
			                 UNION ALL SELECT scn, NULL::uuid FROM scns),
			     cands AS (SELECT scn, id AS b FROM pool
			               UNION ALL SELECT scn, $3::uuid FROM scns
			               UNION ALL SELECT scn, $4::uuid FROM scns),
			     sets AS MATERIALIZED (SELECT c.scn, c.a, coalesce((SELECT array_agg(p.id) FROM public.holding_peer(c.a, 'strict') AS p(id)), '{}') AS peers
			                FROM callers AS c)`
		var foreign, twice int64
		if err := o.QueryRow(ctx, world+`,
			     cmp AS MATERIALIZED (
			         SELECT s.scn, s.a, d.b, public.holding_peer(s.a, d.b, 'strict') AS pair, d.b = ANY (s.peers) AS in_set
			           FROM sets AS s JOIN cands AS d USING (scn))
			SELECT coalesce((SELECT array_agg(format('scenario %s, caller %s, candidate %s: the pair form says %s, the set form %s',
			                                         x.scn, coalesce(x.a::text, 'NULL'), x.b, x.pair, x.in_set))
			                   FROM (SELECT * FROM cmp WHERE pair IS DISTINCT FROM in_set LIMIT 5) AS x), '{}'),
			       (SELECT count(*) FROM sets AS s, unnest(s.peers) AS p(id)
			         WHERE NOT EXISTS (SELECT 1 FROM pool WHERE pool.scn = s.scn AND pool.id = p.id)),
			       (SELECT count(*) FROM cmp),
			       (SELECT count(*) FROM cmp WHERE pair),
			       (SELECT count(*) FROM sets AS s WHERE cardinality(s.peers) = 0 AND public.holding_peer(s.a, NULL, 'is_member')),
			       (SELECT count(*) FROM sets AS s WHERE cardinality(s.peers) > 0 AND EXISTS (
			            SELECT 1 FROM public.holding_members AS m
			             WHERE m.member_tenant_id = ANY (s.peers || s.a) AND m.left_at > clock_timestamp())),
			       (SELECT count(*) FROM sets AS s WHERE cardinality(s.peers) <> (SELECT count(DISTINCT p.id) FROM unnest(s.peers) AS p(id))),
			       (SELECT count(*) FROM sets AS s WHERE EXISTS (
			            SELECT 1 FROM public.holding_members AS ma
			              JOIN public.holding_members AS mb ON mb.holding_id = ma.holding_id
			             WHERE ma.member_tenant_id = s.a AND mb.member_tenant_id <> s.a
			               AND (ma.left_at IS NULL OR ma.left_at > clock_timestamp())
			               AND (mb.left_at IS NULL OR mb.left_at > clock_timestamp())
			             GROUP BY mb.member_tenant_id HAVING count(*) > 1))`,
			w.scenario, w.tenant, w.outsider, w.unknown).Scan(&out, &foreign, &st.pairs, &st.peers, &st.windowOnly, &st.futureLive, &twice, &st.twoWays); err != nil {
			return err
		}
		if foreign != 0 {
			out = append(out, fmt.Sprintf("the set form answered %d tenant(s) outside their scenario's pool", foreign))
		}
		if twice != 0 {
			out = append(out, fmt.Sprintf("the set form answered a peer twice for %d caller(s): cardinality(peers) is not cardinality(DISTINCT peers)", twice))
		}
		return nil
	}); err != nil {
		t.Fatalf("compare the two forms: %v", err)
	}
	return out, st
}

// TestHoldingPeer_TheSetFormIsThePairFormsAnswer is T124's property test (00060; ADR 0027 §2's one
// predicate kept in one place): 400 random scenarios side by side -- seven tenants and three holdings
// each, zero to three memberships a tenant, live, ended long ago, ended inside the 60-second window,
// ending in the future, rejoins and moves between holdings -- and for EVERY caller (each tenant of the
// pool, an outsider in no holding, a NULL caller) and EVERY candidate (each tenant of the pool, the
// caller itself, the outsider, an unknown id): b is in holding_peer(a, 'strict') exactly when
// holding_peer(a, b, 'strict') is true, and no set reaches outside its scenario. The seed is logged.
//
// No set holds a peer twice (cardinality(peers) = cardinality(DISTINCT peers)).
//
// Anti-vacuity: the scenarios produced strict pairs AND non-pairs, callers inside the exit window
// (is_member's tolerance yes, strict no), pairs that stand on a left_at in the future, and callers
// that reach a peer two ways (two strictly live memberships in one holding on either side).
// CONTROLS, each in a savepoint over the same scenarios, each found diverging: the set form LOOSENED
// (the peer's own exit ignored), the set form NARROWED (a caller whose left_at is in the future
// dropped), the set form without its DISTINCT (a peer reached two ways answered twice), and the pair
// form alone given K-33's M3 (the holding equality forgotten) -- a mutant of one
// form that the other does not share is what this test exists to see.
//
// T139 (2026-10-19): on CI run 39211982663 (`main`, f2cee61) the property and the first control took
// ~18 s, the second control's comparison was still running at 162 s when opTx's three-minute context
// ended, and the rest died on the closed connection. Reproduced in CI-shaped runs (a fresh database,
// `go test -race -count=1 ./...`, four CPUs): twice in four, once on a control and once on the
// property's own comparison; the stuck statement was active with no wait event (CPU, not a lock).
// The cause was the statistics, not the load: see holdingPeerNewWorld's ANALYZE. With an ANALYZE of
// all three tables every two seconds from another session (autovacuum's part, made constant), the
// property's comparison never finished before (180 s) and takes ~2.5 s with it. The phases log their
// durations, so a slow run says where.
func TestHoldingPeer_TheSetFormIsThePairFormsAnswer(t *testing.T) {
	ctx, tx := opTx(t)
	seed := uint64(time.Now().UnixNano())
	t.Logf("seed %d", seed)
	rng := mrand.New(mrand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	const scenarios = 400
	start := time.Now()
	w := holdingPeerNewWorld(t, ctx, tx, rng, scenarios, 7, 3)
	built := time.Now()
	diverged, st := holdingPeerSetDivergences(t, ctx, tx, w)
	for _, d := range diverged {
		t.Errorf("seed %d: %s", seed, d)
	}
	t.Logf("%d scenarios, %d (caller, candidate) pairs, %d strict peers, %d callers inside the window only, %d callers with a future-left pair, %d callers reaching a peer two ways (built in %v, compared in %v)",
		scenarios, st.pairs, st.peers, st.windowOnly, st.futureLive, st.twoWays,
		built.Sub(start).Round(time.Millisecond), time.Since(built).Round(time.Millisecond))
	if st.peers < 500 || st.pairs-st.peers < 500 || st.windowOnly < 50 || st.futureLive < 50 || st.twoWays < 50 {
		t.Fatalf("anti-vacuity: the scenarios exercised too little: %+v", st)
	}

	set := holdingPeerSetSource(t)
	for _, c := range []struct {
		name, from, to string
		pair           bool
	}{
		{"the set form loosened: the peer's own exit ignored", "\n           AND (mb.left_at IS NULL OR mb.left_at > v_now);", ";", false},
		{"the set form narrowed: a caller leaving in the future dropped", "AND (ma.left_at IS NULL OR ma.left_at > v_now)", "AND ma.left_at IS NULL", false},
		{"the set form without its DISTINCT: a peer reached two ways answered twice", "SELECT DISTINCT mb.member_tenant_id", "SELECT mb.member_tenant_id", false},
		{"the pair form alone given M3 (the holding equality forgotten)", "", "", true},
	} {
		t.Run("control/"+c.name, func(t *testing.T) {
			holdingPeerControlSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if c.pair {
					holdingInstallPairMutantOnly(t, ctx, sp, holdingPeerMutants["M3"])
				} else {
					if strings.Count(set, c.from) != 1 {
						t.Fatalf("PREMISE: %q is not in the set form once", c.from)
					}
					if _, err := sp.Exec(ctx, strings.Replace(set, c.from, c.to, 1)); err != nil {
						t.Fatal(err)
					}
				}
				start := time.Now()
				d, _ := holdingPeerSetDivergences(t, ctx, sp, w)
				if len(d) == 0 {
					t.Errorf("CONTROL: %d scenarios found no divergence (seed %d)", scenarios, seed)
				}
				t.Logf("%d divergence line(s), compared in %v", len(d), time.Since(start).Round(time.Millisecond))
			})
		})
	}
}

// holdingPeerControlSavepoint is holdingInSavepoint for the controls, whose transaction already holds
// the SHARE UPDATE EXCLUSIVE lock holdingPeerNewWorld's ANALYZE took on holding_members: the same
// rolled-back savepoint and the same check that the body took no strong lock on a table -- with that
// one lock alone excepted. The exception names the table AND the mode: excepting the table alone
// let a control body's ACCESS EXCLUSIVE on holding_members pass unseen (measured).
func holdingPeerControlSavepoint(t *testing.T, ctx context.Context, tx pgx.Tx, body func(sp pgx.Tx)) {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	defer func() {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
	}()
	body(sp)
	if t.Failed() {
		return
	}
	if sp.Conn().PgConn().TxStatus() == 'E' {
		t.Errorf("the savepoint ended aborted, so its locks cannot be read")
		return
	}
	if strong := strongTableLocksOutside(t, ctx, sp, []string{"holding_members"}); len(strong) > 0 {
		t.Errorf("the control took a strong lock on %v", strong)
	}
	rows, err := sp.Query(ctx, `
		SELECT DISTINCT l.mode FROM pg_catalog.pg_locks l
		 WHERE l.locktype = 'relation' AND l.granted AND l.pid = pg_backend_pid()
		   AND l.relation = 'public.holding_members'::regclass
		   AND l.mode IN ('ShareLock', 'ShareRowExclusiveLock', 'ExclusiveLock', 'AccessExclusiveLock')
		 ORDER BY 1`)
	if err != nil {
		t.Fatalf("read this backend's locks on holding_members: %v", err)
	}
	modes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read this backend's locks on holding_members: %v", err)
	}
	if len(modes) > 0 {
		t.Errorf("the control took %v on holding_members (only the ANALYZE's ShareUpdateExclusiveLock is expected)", modes)
	}
}

// holdingInstallPairMutantOnly replaces the pair form ALONE with a K-33 wrong filter (the set form
// left as shipped) -- the divergence the property test must see.
func holdingInstallPairMutantOnly(t *testing.T, ctx context.Context, sp pgx.Tx, filter string) {
	t.Helper()
	if _, err := sp.Exec(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION public.holding_peer(a uuid, b uuid, mod text)
		RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$
		BEGIN
		    IF mod = 'lock' THEN RETURN NULL; END IF;
		    IF mod = 'is_member' THEN
		        RETURN EXISTS (SELECT 1 FROM public.holding_members m
		                        WHERE m.member_tenant_id = a AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp()));
		    END IF;
		    IF a IS NULL OR b IS NULL OR a = b THEN RETURN false; END IF;
		    RETURN %s;
		END $$`, filter)); err != nil {
		t.Fatal(err)
	}
}

// holdingPeerSetAs calls holding_peer's set form as its owner, inside a savepoint of tx.
func holdingPeerSetAs(t *testing.T, ctx context.Context, tx pgx.Tx, a, mode any) ([]uuid.UUID, error) {
	t.Helper()
	var got []uuid.UUID
	err := opAs(t, ctx, tx, "tappa_holdingdefiner", func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, `SELECT p FROM public.holding_peer($1::uuid, $2::text) AS p`, a, mode)
		if err != nil {
			return err
		}
		got, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	return got, err
}

// TestHoldingPeer_TheSetFormIsStrictAlone: the set form's modes are a closed set of ONE (ADR 0027 §2,
// T124 note) -- its body is pinned by md5(prosrc), its guard lists exactly 'strict', and no other mode
// word is compared to mod in it; every other word (the pair form's four others among them), a NULL
// mode, another case or a space are 22023, never a default. A NULL caller is the empty set; a caller
// is never its own peer; two live peers find each other. Its catalogue row: owned by
// tappa_holdingdefiner, SECURITY INVOKER, VOLATILE, the search path alone, EXECUTE in its owner alone
// -- and tappa_app, tappa_operator, tappa_opdefiner and tappa_resolver calling it are 42501. It locks
// nothing and writes nothing (no FOR SHARE either: the lock mode stays the pair form's).
func TestHoldingPeer_TheSetFormIsStrictAlone(t *testing.T) {
	ctx, tx := opTx(t)
	var src, digest, owner, vol, config, execute, result string
	var secdef bool
	if err := tx.QueryRow(ctx, `
		SELECT p.prosrc, md5(p.prosrc), pg_get_userbyid(p.proowner), p.provolatile::text, array_to_string(p.proconfig, '|'), p.prosecdef,
		       (SELECT string_agg(pg_get_userbyid(a.grantee), ',' ORDER BY 1)
		          FROM aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) AS a WHERE a.privilege_type = 'EXECUTE'),
		       pg_get_function_result(p.oid)
		  FROM pg_proc p WHERE p.oid = 'public.holding_peer(uuid, text)'::regprocedure`).
		Scan(&src, &digest, &owner, &vol, &config, &secdef, &execute, &result); err != nil {
		t.Fatalf("read the set form: %v", err)
	}
	if digest != holdingPeerSetBodyMD5 {
		t.Errorf("holding_peer's set form changed (md5 %s, want %s): %s", digest, holdingPeerSetBodyMD5, holdingPeerChangeNeeds)
	}
	if owner != "tappa_holdingdefiner" || secdef || vol != "v" || config != "search_path=pg_catalog, pg_temp" || execute != "tappa_holdingdefiner" || result != "SETOF uuid" {
		t.Errorf("the set form: owner %s SECURITY DEFINER %v provolatile %s proconfig %q EXECUTE {%s} result %s; want tappa_holdingdefiner, INVOKER, v, the search path, {tappa_holdingdefiner}, SETOF uuid",
			owner, secdef, vol, config, execute, result)
	}
	body := strings.ReplaceAll(holdingStripComments(src), `"`, "")
	guard := regexp.MustCompile(`(?is)\bmod\s+is\s+null\s+or\s+mod\s+not\s+in\s*\(([^)]*)\)`).FindAllStringSubmatch(body, -1)
	if len(guard) != 1 || strings.TrimSpace(guard[0][1]) != "'strict'" {
		t.Errorf("the set form's guard is %v, want exactly ('strict'): %s", guard, holdingPeerChangeNeeds)
	}
	if n := len(regexp.MustCompile(`(?is)\bmod\b`).FindAllString(body, -1)); n != 2 {
		t.Errorf("the set form names mod %d times, want the guard's two: %s", n, holdingPeerChangeNeeds)
	}
	if m := holdingPeerForbiddenRE.FindString(body); m != "" {
		t.Errorf("the set form holds %q", strings.TrimSpace(m))
	}
	if m := regexp.MustCompile(`(?i)\bfor\s+(share|update|no\s+key)`).FindString(body); m != "" {
		t.Errorf("the set form takes a row lock (%q); it decides, the pair form's lock mode locks", m)
	}

	a, b := opNewTenant(t, ctx, tx, "t124 strict a "+opToken(t), 0), opNewTenant(t, ctx, tx, "t124 strict b "+opToken(t), 0)
	h := holdingNew(t, ctx, tx, "t124 strict "+opToken(t))
	holdingInsertMember(t, ctx, tx, h, holdingMember{a, "10 minutes", ""})
	holdingInsertMember(t, ctx, tx, h, holdingMember{b, "10 minutes", ""})
	if got, err := holdingPeerSetAs(t, ctx, tx, a, "strict"); err != nil || len(got) != 1 || got[0] != b {
		t.Errorf("strict(a) = %v (%v), want exactly {b}", got, err)
	}
	if got, err := holdingPeerSetAs(t, ctx, tx, nil, "strict"); err != nil || len(got) != 0 {
		t.Errorf("strict(NULL) = %v (%v), want the empty set", got, err)
	}
	for _, m := range []any{"strict_stmt", "tolerant_stmt", "lock", "is_member", "peers", nil, "STRICT", " strict", "strict ", ""} {
		_, err := holdingPeerSetAs(t, ctx, tx, a, m)
		opWant(t, err, sqlstateInvalidParameter, fmt.Sprintf("the set form's mode %v", m))
	}
	for _, role := range []string{"tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_resolver"} {
		opWant(t, opExecAs(t, ctx, tx, role, `SELECT * FROM public.holding_peer($1::uuid, 'strict')`, a),
			sqlstateInsufficientPrivi, role+" calling holding_peer's set form")
	}
	var may bool
	if err := tx.QueryRow(ctx, `SELECT has_function_privilege('tappa_app', 'public.holding_peer(uuid, text)', 'EXECUTE')`).Scan(&may); err != nil || may {
		t.Errorf("tappa_app may EXECUTE holding_peer's set form (%v)", err)
	}
}

// holdingWalkSites is every page of holding_site_locations in tenant's context, by its cursor.
func holdingWalkSites(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID) []string {
	t.Helper()
	var out []string
	if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		var cursor *uuid.UUID
		for range 1000 {
			rows, err := sp.Query(ctx, `SELECT site_tenant_id, location_id, location_name, location_external_ref, shift_start, shift_end, overnight
			                              FROM public.holding_site_locations($1)`, cursor)
			if err != nil {
				return err
			}
			n := 0
			for rows.Next() {
				var site, loc uuid.UUID
				var name string
				var ref *string
				var overnight bool
				var s0, e0 any
				if err := rows.Scan(&site, &loc, &name, &ref, &s0, &e0, &overnight); err != nil {
					rows.Close()
					return err
				}
				out = append(out, fmt.Sprintf("%s %s %q %v %v %v %v", site, loc, name, holdingStr(ref), s0, e0, overnight))
				cursor = &loc
				n++
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			if n < 1000 {
				return nil
			}
		}
		return fmt.Errorf("more than 1000 pages")
	}); err != nil {
		t.Fatalf("walk holding_site_locations: %v", err)
	}
	return out
}

// holdingWalkDirectory is every page of holding_directory in tenant's context, by its keyset
// (since, cursor) from the beginning, page sizes as the function gives them.
func holdingWalkDirectory(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID) []string {
	t.Helper()
	var out []string
	var since *time.Time
	var cursor *uuid.UUID
	for range 1000 {
		page := holdingDirectoryIn(t, ctx, tx, tenant, since, cursor)
		for _, r := range page {
			out = append(out, r.String())
		}
		if len(page) < 1000 {
			return out
		}
		last := page[len(page)-1]
		since, cursor = &last.updatedAt, &last.id
	}
	t.Fatal("holding_directory: more than 1000 pages")
	return nil
}

// TestHoldingFunctions_00060AnswersWhat00055Answered: the same data through both bodies -- 00060's
// (HEAD) and 00055's (00060's Down, in a savepoint) -- gives the same rows, in the same order, on the
// same pages: every page of holding_site_locations by its cursor and of holding_directory by its keyset,
// in the context of KF and KM (peers in H; KF given more than a page of staff and of venues, so both functions
// page), C and D (peers in H2), a business in no holding and no context -- in four membership states:
// as built; KM left H ten seconds ago (inside is_member's window); KM moved to H2; a membership of KF's
// that ends in the future; and KF live in H two ways at once (its live membership and a second one
// that ends in the future -- KM reached from KF, and KF from KM, through two pairs of memberships:
// a peer is one peer, its rows once). Each state's answers are non-empty somewhere (anti-vacuity), and the two
// bodies are told apart by their md5 (the Down really ran).
func TestHoldingFunctions_00060AnswersWhat00055Answered(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	alone := opNewTenant(t, ctx, tx, "t124 alone "+opToken(t), 0)
	for _, b := range []uuid.UUID{w.kf, w.km, w.c, w.d, alone} {
		holdingNewEmployee(t, ctx, tx, b, "active")
		holdingNewEmployee(t, ctx, tx, b, "deactivated")
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO employees (tenant_id, external_ref, employee_code, first_name, last_name, full_name, status, invited_at)
		SELECT $1, 't124-' || g, 'T' || g, 'First' || g, 'Last', 'First' || g || ' Last', CASE WHEN g % 7 = 0 THEN 'deactivated' ELSE 'active' END, now()
		  FROM generate_series(1, 2500) AS g`, w.kf); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO locations (tenant_id, name, external_ref, shift_start, shift_end, overnight)
		SELECT $1, 't124 venue ' || g, 'v' || g, CASE WHEN g % 2 = 0 THEN time '18:00' END, CASE WHEN g % 2 = 0 THEN time '02:00' END, g % 2 = 0
		  FROM generate_series(1, 1200) AS g`, w.kf); err != nil {
		t.Fatal(err)
	}
	contexts := []struct {
		who    string
		tenant uuid.UUID
	}{{"KF", w.kf}, {"KM", w.km}, {"C", w.c}, {"D", w.d}, {"alone", alone}, {"no context", uuid.Nil}}
	answers := func(q pgx.Tx) (map[string][]string, int) {
		out, n := map[string][]string{}, 0
		for _, c := range contexts {
			sites, dir := holdingWalkSites(t, ctx, q, c.tenant), holdingWalkDirectory(t, ctx, q, c.tenant)
			out[c.who+" sites"], out[c.who+" directory"] = sites, dir
			n += len(sites) + len(dir)
		}
		return out, n
	}
	bodies := func(q pgx.Tx) string {
		var s string
		if err := q.QueryRow(ctx, `SELECT md5(string_agg(md5(prosrc), ',' ORDER BY proname)) FROM pg_proc
		                           WHERE proname IN ('holding_site_locations', 'holding_directory')`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	_, down60 := opMigrationSections(t, op00060File)
	type stmt struct {
		sql  string
		args []any
	}
	leave := `UPDATE holding_members SET left_at = clock_timestamp() - $2::interval WHERE member_tenant_id = $1 AND left_at IS NULL`
	for _, state := range []struct {
		name string
		sql  []stmt
	}{
		{"as built", nil},
		{"KM left H ten seconds ago", []stmt{{leave, []any{w.km, "10 seconds"}}}},
		{"KM moved to H2", []stmt{{leave, []any{w.km, "2 minutes"}},
			{`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at) VALUES ($1, $2, clock_timestamp() - interval '1 minute')`, []any{w.h2, w.km}}}},
		{"KF's membership ends in the future", []stmt{{leave, []any{w.kf, "-1 hour"}}}},
		{"KF live in H twice: its live membership and one that ends in the future", []stmt{
			{`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at, left_at)
			  VALUES ($1, $2, clock_timestamp() - interval '3 hours', clock_timestamp() + interval '1 hour')`, []any{w.h, w.kf}}}},
	} {
		t.Run(state.name, func(t *testing.T) {
			holdingInSavepoint(t, tx, func(sp pgx.Tx) {
				for _, s := range state.sql {
					if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
						t.Fatalf("%s: %v", s.sql, err)
					}
				}
				head, n := answers(sp)
				if n == 0 {
					t.Fatal("anti-vacuity: no context read a row")
				}
				before := bodies(sp)
				holdingInSavepoint(t, sp, func(old pgx.Tx) {
					opRunSection(t, ctx, old, down60, "00060 Down")
					if bodies(old) == before {
						t.Fatal("PREMISE: 00060's Down did not change the two bodies")
					}
					was, _ := answers(old)
					for _, c := range contexts {
						for _, fn := range []string{" sites", " directory"} {
							k := c.who + fn
							if strings.Join(was[k], "\n") != strings.Join(head[k], "\n") {
								t.Errorf("%s: 00060 answers %d rows, 00055 answered %d -- not the same rows in the same order", k, len(head[k]), len(was[k]))
							}
						}
					}
				})
			})
		})
	}
}

// holdingPeerSetSurface is what 00060 changes, read off the catalogue: the two bodies, the pair form's
// body, and the set form (its result, owner, flags and ACL) or "-".
func holdingPeerSetSurface(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `SELECT
		(SELECT md5(prosrc) FROM pg_proc WHERE oid = 'public.holding_site_locations(uuid)'::regprocedure) || ' ' ||
		(SELECT md5(prosrc) FROM pg_proc WHERE oid = 'public.holding_directory(timestamptz, uuid)'::regprocedure) || ' ' ||
		(SELECT md5(prosrc) FROM pg_proc WHERE oid = 'public.holding_peer(uuid, uuid, text)'::regprocedure) || ' | ' ||
		coalesce((SELECT md5(prosrc) || ' ' || pg_get_function_result(oid) || ' ' || pg_get_userbyid(proowner) || ' ' || prosecdef::text || ' ' ||
		                 provolatile::text || ' ' || coalesce(array_to_string(proacl, ','), '') || ' ' || array_to_string(proconfig, '|') || ' ' || prorows::text
		            FROM pg_proc WHERE oid = to_regprocedure('public.holding_peer(uuid, text)')), '-') || ' | ' ||
		(SELECT count(*) FROM pg_proc WHERE proname = 'holding_peer')::text`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestHolding00060_DownGivesBackAndUpTakesItAgain: 00060's Down gives holding_site_locations and
// holding_directory 00055's bodies back byte for byte (the md5s 00060's precondition names) and drops
// the set form, the pair form untouched; its Up takes exactly HEAD's surface again. The precondition
// refuses a second run (the set form is there) and a pair form, a holding_site_locations or a
// holding_directory whose body is not the one it was written against.
func TestHolding00060_DownGivesBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00060File)
	pre := holdingPrecondition(t, op00060File)
	const at55 = "8401897e362b1ae818f67c5fe2272c0c 3778704c97ca47c3bf355af8ff0610ac " + holdingPeerBodyMD5 + " | - | 1"
	_, down62 := opMigrationSections(t, op00062File)
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		// GRP-1: at 00061 -- 00062's roster form is a third holding_peer, which 00060's precondition
		// refuses by design; this test is about 00060's own surface.
		opRunSection(t, ctx, sp, down62, "00062 Down")
		head := holdingPeerSetSurface(t, ctx, sp)
		if !strings.Contains(head, holdingPeerSetBodyMD5+" SETOF uuid tappa_holdingdefiner false v tappa_holdingdefiner=X/tappa_holdingdefiner search_path=pg_catalog, pg_temp 10 | 2") {
			t.Fatalf("PREMISE at 00061: %s", head)
		}
		opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00060's precondition at HEAD")
		opRunSection(t, ctx, sp, down, "00060 Down")
		if got := holdingPeerSetSurface(t, ctx, sp); got != at55 {
			t.Errorf("after 00060's Down: %s\nwant %s", got, at55)
		}
		if err := holdingNestedRun(t, ctx, sp, pre); err != nil {
			t.Fatalf("00060's precondition at 00059: %v", err)
		}
		for _, c := range []struct{ name, ddl string }{
			{"a pair form that is not 00041's", `CREATE OR REPLACE FUNCTION public.holding_peer(a uuid, b uuid, mod text)
				RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$ BEGIN RETURN false; END $$`},
			{"a holding_site_locations that is not 00055's", `CREATE OR REPLACE FUNCTION public.holding_site_locations(p_cursor uuid)
				RETURNS TABLE(site_tenant_id uuid, location_id uuid, location_name text, location_external_ref text,
				              shift_start time, shift_end time, overnight boolean)
				LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
				AS 'SELECT NULL::uuid, NULL::uuid, NULL::text, NULL::text, NULL::time, NULL::time, NULL::boolean WHERE false'`},
			{"a holding_directory that is not 00055's", `CREATE OR REPLACE FUNCTION public.holding_directory(p_updated_since timestamptz, p_cursor uuid)
				RETURNS TABLE(employee_id uuid, employer_tenant_id uuid, api_code text, external_ref text, employee_code text,
				              first_name text, last_name text, full_name text, active boolean, updated_at timestamptz)
				LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
				AS 'SELECT NULL::uuid, NULL::uuid, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::text, NULL::boolean, NULL::timestamptz WHERE false'`},
			{"a second holding_peer of another shape", `CREATE FUNCTION public.holding_peer(a uuid) RETURNS boolean LANGUAGE sql AS 'SELECT false'`},
		} {
			holdingInSavepoint(t, sp, func(bad pgx.Tx) {
				if _, err := bad.Exec(ctx, c.ddl); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
				opWant(t, holdingNestedRun(t, ctx, bad, pre), sqlstatePrerequisiteState, "00060's precondition over "+c.name)
			})
		}
		opRunSection(t, ctx, sp, up, "00060 Up again")
		if got := holdingPeerSetSurface(t, ctx, sp); got != head {
			t.Errorf("after the Up again:\n %s\nwant HEAD's\n %s", got, head)
		}
	})
}
