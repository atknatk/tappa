package db

// holdingdirectory_test.go -- M11 API-8 at the database: migrations 00055 (holding_directory, its
// four employees grants, the membership gate) and 00056 (employees_tenant_updated_idx) against real
// Postgres. ADR 0027 §2 (the
// closed list's fifth callable, its contract, "eş kümesi bir kez"), §3 (K-34), §6 (strict reads);
// ADR 0028 §2 (the directory is two sources; the peers' never names the caller), §9; plan §5 API-8 and
// ADR 0027's "Test edilecekler" (HoldingDirectory_ReturnsPeersNeverTheCaller, handed over from API-1b).
// The catalogue, the matrix, the canary, the peer-set count and K-33's mutants reach holding_directory
// through the shared tables of holdingcatalog_test.go and holdingtap_test.go.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	op00055File = "00055_list_a_peers_staff.sql"
	op00056File = "00056_index_the_directory.sql"
	// T124: holding_peer's set form, and the two multi-row functions reading the peer set through it.
	op00060File = "00060_ask_a_members_peers_once.sql"
)

// holdingDirRow is one row of holding_directory, every column.
type holdingDirRow struct {
	id, employer                    uuid.UUID
	apiCode, ref, code, first, last *string
	full                            string
	active                          bool
	updatedAt                       time.Time
}

func (r holdingDirRow) String() string {
	s := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	return fmt.Sprintf("{%s of %s %s %s %s %s %s %q active=%v %s}", r.id, r.employer, s(r.apiCode), s(r.ref), s(r.code), s(r.first), s(r.last),
		r.full, r.active, r.updatedAt.UTC().Format(time.RFC3339Nano))
}

// holdingDirectoryIn calls holding_directory(since, cursor) as tappa_app in tenant's context.
func holdingDirectoryIn(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, since *time.Time, cursor *uuid.UUID) []holdingDirRow {
	t.Helper()
	var out []holdingDirRow
	if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, `SELECT employee_id, employer_tenant_id, api_code, external_ref, employee_code, first_name, last_name, full_name,
		                                   active, updated_at
		                              FROM public.holding_directory($1, $2)`, since, cursor)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (holdingDirRow, error) {
			var d holdingDirRow
			err := r.Scan(&d.id, &d.employer, &d.apiCode, &d.ref, &d.code, &d.first, &d.last, &d.full, &d.active, &d.updatedAt)
			return d, err
		})
		return err
	}); err != nil {
		t.Fatalf("holding_directory: %v", err)
	}
	return out
}

// holdingDirTruth is what the owner (no RLS) reads of tenants' employees, in the directory's order --
// the answer holding_directory must give for them.
func holdingDirTruth(t *testing.T, ctx context.Context, q opQuerier, tenants ...uuid.UUID) []holdingDirRow {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT e.id, e.tenant_id, t.api_code, e.external_ref, e.employee_code, e.first_name, e.last_name, e.full_name,
	                                  e.status <> 'deactivated', e.updated_at
	                             FROM employees e JOIN tenants t ON t.id = e.tenant_id
	                            WHERE e.tenant_id = ANY($1) ORDER BY e.updated_at, e.id`, tenants)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (holdingDirRow, error) {
		var d holdingDirRow
		err := r.Scan(&d.id, &d.employer, &d.apiCode, &d.ref, &d.code, &d.first, &d.last, &d.full, &d.active, &d.updatedAt)
		return d, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func holdingDirEqual(a, b []holdingDirRow) bool {
	return slices.EqualFunc(a, b, func(x, y holdingDirRow) bool { return x.String() == y.String() })
}

// holdingDirStaff gives tenant the four shapes of a person the directory must answer: named and
// active, deactivated, invited (active), and panel-made (no ref, no names -- a home venue instead).
// Every one has an e-mail address carrying marker (K-34: the function must never answer it).
func holdingDirStaff(t *testing.T, ctx context.Context, q opQuerier, tenant, venue uuid.UUID, marker string) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	for i, p := range []struct {
		status      string
		ref, code   any
		first, last any
		withHome    bool
	}{
		{"active", "dir-a-" + marker, "C-A", "Ada", "Active", false},
		{"deactivated", "dir-d-" + marker, "C-D", "Dee", "Gone", false},
		{"invited", "dir-i-" + marker, nil, "Ivo", "Invited", false},
		{"active", nil, nil, nil, nil, true},
	} {
		var home any
		if p.withHome {
			home = venue
		}
		var id uuid.UUID
		if err := q.QueryRow(ctx, `INSERT INTO employees (tenant_id, location_id, external_ref, employee_code, first_name, last_name, full_name, email, status, invited_at)
		                           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now()) RETURNING id`,
			tenant, home, p.ref, p.code, p.first, p.last, holdingDirFullName(i, marker), fmt.Sprintf("dir%d-%s@example.test", i, marker), p.status).Scan(&id); err != nil {
			t.Fatalf("insert a %s person: %v", p.status, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// holdingDirFullName is the full name holdingDirStaff gives its i-th person: distinct per business,
// so a full_name cell can be matched to the person it names. The 4th (i = 3) is the panel-made one
// -- no first or last name, the full name its only name (the user's decision, 2026-10-15).
func holdingDirFullName(i int, marker string) string {
	return fmt.Sprintf("Dir Staff %d %s", i, marker)
}

// holdingSetUpdatedAt writes updated_at as the owner with the triggers off (00037's clock overwrites
// any value a statement writes) -- the lab's way to put rows at chosen places of the order.
func holdingSetUpdatedAt(t *testing.T, ctx context.Context, q opQuerier, at time.Time, ids ...uuid.UUID) {
	t.Helper()
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`SET LOCAL session_replication_role = replica`, nil},
		{`UPDATE employees SET updated_at = $1 WHERE id = ANY($2)`, []any{at, ids}},
		{`SET LOCAL session_replication_role = origin`, nil},
	} {
		if _, err := q.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
}

// TestHoldingDirectory_ReturnsPeersNeverTheCaller is ADR 0027's HoldingDirectory_ReturnsPeersNeverTheCaller
// (API-1b's hand-over) and plan §5 API-8's database half, on holdingWorld (KF and KM in H; C and D in
// H2 -- K-33's fixture). Each of KF, KM, C and D employs the directory's four shapes of person (named,
// deactivated, invited, panel-made), each with an e-mail address. In each one's context
// holding_directory answers EXACTLY its peer's people, every column as the owner reads it (the peer's
// company code; active false for the deactivated one -- K-1h; the panel-made person by its full name,
// first and last null -- the user's decision, 2026-10-15), and never one of its own; no e-mail
// address appears in any cell (K-1f, K-24, K-34). No context answers nothing. Ten seconds after KM left
// H -- inside the 60-second window the gate tolerates -- neither KM nor KF reads the other (§6: the reads
// are strict).
func TestHoldingDirectory_ReturnsPeersNeverTheCaller(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	marker := strings.ToLower(opToken(t))
	venues := map[uuid.UUID]uuid.UUID{w.kf: w.kfVenue, w.c: w.cVenue}
	peerCode := map[uuid.UUID]string{w.kf: "KF", w.km: "KM", w.c: "CC", w.d: "DD"}
	for _, b := range []struct {
		id   uuid.UUID
		code string
	}{{w.kf, "KF"}, {w.km, "KM"}, {w.c, "CC"}, {w.d, "DD"}} {
		if _, err := tx.Exec(ctx, `UPDATE tenants SET api_code = $2 WHERE id = $1`, b.id, b.code); err != nil {
			t.Fatal(err)
		}
		venue, ok := venues[b.id]
		if !ok {
			if err := tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'Dir Venue') RETURNING id`, b.id).Scan(&venue); err != nil {
				t.Fatal(err)
			}
		}
		holdingDirStaff(t, ctx, tx, b.id, venue, marker+"-"+strings.ToLower(b.code))
	}
	for _, c := range []struct {
		who       string
		ctx, peer uuid.UUID
	}{{"KM", w.km, w.kf}, {"KF", w.kf, w.km}, {"C", w.c, w.d}, {"D", w.d, w.c}} {
		got := holdingDirectoryIn(t, ctx, tx, c.ctx, nil, nil)
		want := holdingDirTruth(t, ctx, tx, c.peer)
		if len(want) < 4 || !holdingDirEqual(got, want) {
			t.Errorf("%s's context answered\n %v\nwant its peer's people, exactly\n %v", c.who, got, want)
		}
		inactive := 0
		for _, r := range got {
			if r.employer == c.ctx {
				t.Errorf("%s's context answered its OWN employee %s", c.who, r.id)
			}
			if !r.active {
				inactive++
			}
			if strings.Contains(r.String(), "@") {
				t.Errorf("%s's context answered an e-mail address: %v", c.who, r)
			}
		}
		if inactive != 1 {
			t.Errorf("%s's context answered %d inactive people, want exactly the deactivated one (K-1h)", c.who, inactive)
		}
		// The peer's panel-made person -- no first or last name -- is named by full_name (the user's
		// decision, 2026-10-15), in that cell.
		panelMade := holdingDirFullName(3, marker+"-"+strings.ToLower(peerCode[c.peer]))
		found := false
		for _, r := range got {
			if r.full == panelMade {
				found = r.first == nil && r.last == nil
			}
		}
		if !found {
			t.Errorf("%s's context did not answer its peer's panel-made person by full name %q, with no first or last name", c.who, panelMade)
		}
	}
	if got := holdingDirectoryIn(t, ctx, tx, uuid.Nil, nil, nil); len(got) != 0 {
		t.Errorf("no context answered %d rows, want none", len(got))
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `UPDATE holding_members SET left_at = clock_timestamp() - interval '10 seconds'
		                           WHERE member_tenant_id = $1 AND left_at IS NULL`, w.km); err != nil {
			t.Fatal(err)
		}
		for who, tenant := range map[string]uuid.UUID{"KM, 10 s after it left": w.km, "KF, 10 s after KM left": w.kf} {
			if got := holdingDirectoryIn(t, ctx, sp, tenant, nil, nil); len(got) != 0 {
				t.Errorf("%s: %d rows, want none (the read is strict)", who, len(got))
			}
		}
	})
}

// TestHoldingDirectory_IsAKeysetOnUpdatedAtAndID: holding_directory's page is ADR 0027 §2's "at most
// 1000, paged" as a keyset on (updated_at, id) over EVERY peer at once. KM's holding has two peers, KF
// (1 150 rows) and X (900); 40 of them -- 20 of each -- share one updated_at, and the rest are spread
// so the two peers interleave. In KM's context:
//   - the walk (first page (NULL, NULL), then each page after its last row's (updated_at, id)) answers
//     the owner's ordering of the two peers' 2 050 rows exactly, 1000 + 1000 + 50, nothing of KM's;
//   - a cursor inside the tie group answers the rest of the group in id order, then what follows;
//   - since without a cursor is "updated_at >= since": a row AT since is in, one microsecond later
//     it is not;
//   - a cursor with no since is a position after the beginning of time: everything after its id at
//     -infinity, i.e. all of it.
func TestHoldingDirectory_IsAKeysetOnUpdatedAtAndID(t *testing.T) {
	ctx, tx := opTx(t)
	km := opNewTenant(t, ctx, tx, "api8 keyset KM "+opToken(t), 0)
	kf := opNewTenant(t, ctx, tx, "api8 keyset KF "+opToken(t), 0)
	x := opNewTenant(t, ctx, tx, "api8 keyset X "+opToken(t), 0)
	h := holdingNew(t, ctx, tx, "api8 keyset "+opToken(t))
	for _, m := range []uuid.UUID{km, kf, x} {
		holdingInsertMember(t, ctx, tx, h, holdingMember{m, "1 minute", ""})
	}
	base := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	tie := base.Add(30*time.Minute + 500*time.Millisecond) // no spread row lands on it
	// grow inserts n people of tenant and spreads their updated_at from base+offset, step apart, in id
	// order (one statement, the triggers off -- holdingSetUpdatedAt's reason).
	grow := func(tenant uuid.UUID, n int, step time.Duration, offset time.Duration) []uuid.UUID {
		if _, err := tx.Exec(ctx, `INSERT INTO employees (tenant_id, external_ref, full_name, status, invited_at)
		                           SELECT $1, 'ks-' || gen_random_uuid(), 'Keyset Staff', 'active', now() FROM generate_series(1, $2)`, tenant, n); err != nil {
			t.Fatal(err)
		}
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`SET LOCAL session_replication_role = replica`, nil},
			{`UPDATE employees e SET updated_at = $2::timestamptz + n.i * $3::interval
			    FROM (SELECT id, row_number() OVER (ORDER BY id) - 1 AS i FROM employees WHERE tenant_id = $1) AS n
			   WHERE e.id = n.id`, []any{tenant, base.Add(offset), fmt.Sprintf("%d milliseconds", step.Milliseconds())}},
			{`SET LOCAL session_replication_role = origin`, nil},
		} {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				t.Fatalf("%s: %v", s.sql, err)
			}
		}
		rows, err := tx.Query(ctx, `SELECT id FROM employees WHERE tenant_id = $1 ORDER BY id`, tenant)
		if err != nil {
			t.Fatal(err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}
	kfIDs := grow(kf, 1150, 2*time.Second, 0)
	xIDs := grow(x, 900, 3*time.Second, time.Second)
	grow(km, 30, time.Second, 0)
	holdingSetUpdatedAt(t, ctx, tx, tie, append(slices.Clone(kfIDs[100:120]), xIDs[100:120]...)...)
	want := holdingDirTruth(t, ctx, tx, kf, x)
	if len(want) != 2050 {
		t.Fatalf("PREMISE: %d peer rows", len(want))
	}

	var got []holdingDirRow
	var since *time.Time
	var cursor *uuid.UUID
	var sizes []int
	for range 10 {
		page := holdingDirectoryIn(t, ctx, tx, km, since, cursor)
		sizes = append(sizes, len(page))
		got = append(got, page...)
		if len(page) < 1000 {
			break
		}
		last := page[len(page)-1]
		since, cursor = &last.updatedAt, &last.id
	}
	if !holdingDirEqual(got, want) || !slices.Equal(sizes, []int{1000, 1000, 50}) {
		t.Errorf("the walk answered %d rows in pages %v; want the owner's order of the peers' 2050 in 1000, 1000, 50", len(got), sizes)
	}

	group := -1
	for i, r := range want {
		if r.updatedAt.Equal(tie) {
			group = i
			break
		}
	}
	if group < 0 || !want[group+39].updatedAt.Equal(tie) || want[group+40].updatedAt.Equal(tie) {
		t.Fatalf("PREMISE: the tie group of 40 is not where it was put")
	}
	mid := want[group+17]
	if after := holdingDirectoryIn(t, ctx, tx, km, &mid.updatedAt, &mid.id); !holdingDirEqual(after, want[group+18:min(group+18+1000, len(want))]) {
		t.Errorf("a cursor inside the tie group answered %d rows starting %v; want the group's rest in id order, then what follows (%v)",
			len(after), after[:min(3, len(after))], want[group+18])
	}

	at := want[500].updatedAt
	from := holdingDirectoryIn(t, ctx, tx, km, &at, nil)
	if len(from) == 0 || !from[0].updatedAt.Equal(at) || from[0].id != want[500].id {
		t.Errorf("since = a row's updated_at did not start AT that row: %v", from[:min(1, len(from))])
	}
	later := at.Add(time.Microsecond)
	if from := holdingDirectoryIn(t, ctx, tx, km, &later, nil); len(from) > 0 && !from[0].updatedAt.After(at) {
		t.Errorf("since one microsecond later still answered the row at since: %v", from[0])
	}
	if all := holdingDirectoryIn(t, ctx, tx, km, nil, &want[0].id); len(all) != 1000 || all[0].id != want[0].id {
		t.Errorf("a cursor without since answered %d rows from %v; want everything (a position after -infinity)", len(all), all[:min(1, len(all))])
	}
}

// TestHoldingDirectory_TheCallerIsKeptOutTwice: the caller's own people are kept out by holding_peer
// (a is never in its own peer set -- the one predicate; since 00060 the set form the body asks) AND by
// the peer set's own `s.id <> c.id`. Each layer is shown to hold the line alone, in savepoints, by
// CREATE OR REPLACE:
//   - holding_peer's set form answering a among its own peers (a mutant of the predicate): the belt
//     keeps KM's people out;
//   - the belt removed from holding_directory (the shipped body less that line): holding_peer keeps
//     them out;
//   - both: KM's context answers KM's own people -- what this test is red on.
func TestHoldingDirectory_TheCallerIsKeptOutTwice(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	up, _ := opMigrationSections(t, op00060File)
	body := regexp.MustCompile(`(?s)CREATE OR REPLACE FUNCTION public\.holding_directory.*?\n\$\$;`).FindString(up)
	const belt = "         WHERE s.id <> c.id\n"
	if body == "" || strings.Count(body, belt) != 1 {
		t.Fatalf("PREMISE: 00060's body or its belt line not found")
	}
	noBelt := strings.Replace(body, belt, "", 1)
	selfPeer := `CREATE OR REPLACE FUNCTION public.holding_peer(a uuid, mod text)
		RETURNS SETOF uuid LANGUAGE plpgsql VOLATILE SECURITY INVOKER SET search_path = pg_catalog, pg_temp AS $$
		BEGIN
		    IF a IS NULL THEN RETURN; END IF;
		    RETURN QUERY SELECT DISTINCT mb.member_tenant_id
		                   FROM public.holding_members ma JOIN public.holding_members mb ON mb.holding_id = ma.holding_id
		                  WHERE ma.member_tenant_id = a AND ma.left_at IS NULL AND mb.left_at IS NULL;
		END $$`
	own := func(q pgx.Tx) int {
		n := 0
		for _, r := range holdingDirectoryIn(t, ctx, q, w.km, nil, nil) {
			if r.employer == w.km {
				n++
			}
		}
		return n
	}
	if n := own(tx); n != 0 {
		t.Fatalf("as shipped KM's context answered %d of its own people", n)
	}
	for _, c := range []struct {
		name   string
		ddl    []string
		wantIn bool
	}{
		{"holding_peer says a tenant is its own peer", []string{selfPeer}, false},
		{"the belt removed", []string{noBelt}, false},
		{"both", []string{selfPeer, noBelt}, true},
	} {
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			for _, s := range c.ddl {
				if _, err := sp.Exec(ctx, s); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
			}
			if n := own(sp); (n > 0) != c.wantIn {
				t.Errorf("%s: KM's context answered %d of its own people; want them answered=%v", c.name, n, c.wantIn)
			}
		})
	}
}

// holdingDirSurface is what 00055 changes, read off the catalogue.
func holdingDirSurface(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `SELECT
		coalesce((SELECT pg_get_function_result(p.oid) || ' ' || coalesce(array_to_string(p.proacl, ','), '') || ' ' || p.prosecdef::text
		            FROM pg_proc p WHERE p.proname = 'holding_directory'), '-') || ' | ' ||
		coalesce((SELECT md5(prosrc) || ' ' || pg_get_function_result(oid) || ' ' || coalesce(array_to_string(proacl, ','), '') || ' ' ||
		                 pg_get_userbyid(proowner)
		            FROM pg_proc WHERE proname = 'holding_site_locations'), '-')`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s + " | employees:" + opColumns(t, ctx, q, "tappa_holdingdefiner", "employees", "SELECT")
}

// TestHolding00055_DownGivesBackAndUpTakesItAgain: 00055's Down removes holding_directory and the
// definer's four employees columns, and gives holding_site_locations 00047's
// body back byte for byte (its md5 -- the owner, the result and the grants unchanged), and nothing else
// of the definer's reach; its Up gives back exactly HEAD's surface. The precondition refuses a second
// run (the function is there), a holding_site_locations whose body is not 00047's, and -- the Down
// revokes what the Up grants -- a definer that already reaches one of the four columns, or the e-mail.
func TestHolding00055_DownGivesBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00055File)
	head := holdingDirSurface(t, ctx, tx)
	if !strings.HasPrefix(head, "TABLE(") || strings.Contains(head, "63bf5ecfca8a794ac9d8125d19dce0c1") || !strings.HasSuffix(head,
		"employees:id,tenant_id,full_name,status,external_ref,employee_code,first_name,last_name,updated_at") {
		t.Fatalf("PREMISE at HEAD: %s", head)
	}
	pre := holdingPrecondition(t, op00055File)
	up60, down60 := opMigrationSections(t, op00060File)
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00055's precondition at HEAD")
		// T124: 00060's Down first -- it gives back the bodies 00055 left (TestHolding00060_DownGivesBackAndUpTakesItAgain).
		opRunSection(t, ctx, sp, down60, "00060 Down")
		opRunSection(t, ctx, sp, down, "00055 Down")
		at47 := strings.Split(head, " | ")[1]
		at47 = "63bf5ecfca8a794ac9d8125d19dce0c1" + at47[strings.Index(at47, " "):]
		if got := holdingDirSurface(t, ctx, sp); got != "- | "+at47+" | employees:id,tenant_id,full_name,external_ref,employee_code" {
			t.Errorf("after 00055's Down: %s", got)
		}
		if _, err := sp.Exec(ctx, `CREATE OR REPLACE FUNCTION public.holding_site_locations(p_cursor uuid)
			RETURNS TABLE(site_tenant_id uuid, location_id uuid, location_name text, location_external_ref text,
			              shift_start time, shift_end time, overnight boolean)
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp
			AS 'SELECT NULL::uuid, NULL::uuid, NULL::text, NULL::text, NULL::time, NULL::time, NULL::boolean WHERE false'`); err != nil {
			t.Fatal(err)
		}
		opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00055's precondition over a holding_site_locations that is not 00047's")
		_, down47 := opMigrationSections(t, op00047File)
		opRunSection(t, ctx, sp, down47, "00047 Down")
		up47, _ := opMigrationSections(t, op00047File)
		opRunSection(t, ctx, sp, up47, "00047 Up again")
		for _, col := range []string{"status", "email"} {
			if _, err := sp.Exec(ctx, fmt.Sprintf(`GRANT SELECT (%s) ON employees TO tappa_holdingdefiner`, col)); err != nil {
				t.Fatal(err)
			}
			opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00055's precondition with the definer already reaching "+col)
			if _, err := sp.Exec(ctx, fmt.Sprintf(`REVOKE SELECT (%s) ON employees FROM tappa_holdingdefiner`, col)); err != nil {
				t.Fatal(err)
			}
		}
		opRunSection(t, ctx, sp, up, "00055 Up again")
		opRunSection(t, ctx, sp, up60, "00060 Up again")
		if got := holdingDirSurface(t, ctx, sp); got != head {
			t.Errorf("after the Up again:\n %s\nwant HEAD's\n %s", got, head)
		}
	})
}

// TestEmployees00056_TheDirectoryIndexGoesBack: the directory's index is (tenant_id, updated_at, id) on
// employees; 00056's Down removes it and its Up builds it again (00054's test's shape -- this file's
// Down is the one that takes employees' ACCESS EXCLUSIVE lock, so it is walked here alone, never in
// the holding Down walks). Behind other tests' employee writes the Down's own lock_timeout can fire:
// it is retried, each attempt in a savepoint of its own.
func TestEmployees00056_TheDirectoryIndexGoesBack(t *testing.T) {
	ctx, tx := opTx(t)
	def := `SELECT coalesce((SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'employees_tenant_updated_idx'), '')`
	var got string
	if err := tx.QueryRow(ctx, def).Scan(&got); err != nil || !strings.Contains(got, "ON public.employees USING btree (tenant_id, updated_at, id)") {
		t.Fatalf("the directory's index is %q (%v)", got, err)
	}
	up, down := opMigrationSections(t, op00056File)
	run := func(section, what string) {
		for attempt := 1; ; attempt++ {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = sp.Conn().PgConn().Exec(ctx, section).ReadAll(); err == nil {
				if err := sp.Commit(ctx); err != nil { // RELEASE: the section's effect stays in the test's transaction
					t.Fatal(err)
				}
				return
			}
			if rerr := sp.Rollback(ctx); rerr != nil {
				t.Fatal(rerr)
			}
			if code, _ := opCode(err); code != "55P03" || attempt == 30 {
				t.Fatalf("%s: %v", what, err)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	run(down, "00056 Down")
	if err := tx.QueryRow(ctx, def).Scan(&got); err != nil || got != "" {
		t.Fatalf("after the Down the index is %q (%v)", got, err)
	}
	run(up, "00056 Up again")
	if err := tx.QueryRow(ctx, def).Scan(&got); err != nil || got == "" {
		t.Fatalf("after the Up the index is missing (%v)", err)
	}
}

// TestEmployees00056_GivesUpAfterASecondBehindAnOpenWrite (00054's rule, deploy/README.md): 00056's
// first statement is SET LOCAL lock_timeout = '1s', so behind an open employees write the index build
// gives up with 55P03 after about a second instead of queueing every later employee write behind its
// SHARE lock. Measured with 00056's own Up (the index renamed, so the existing one does not answer
// first) while another connection holds an INSERT open.
//
// MUTANT (measured): the SET LOCAL removed -> the build waits for the holder (released after 4 s), red.
func TestEmployees00056_GivesUpAfterASecondBehindAnOpenWrite(t *testing.T) {
	d := appDB(t)
	tenantID, venue := m11Tenant(t, d)
	ctx, tx := opTx(t)
	up, _ := opMigrationSections(t, op00056File)
	first, build := strings.Index(up, "SET LOCAL lock_timeout = '1s';"), strings.Index(up, "CREATE INDEX employees_tenant_updated_idx")
	if first < 0 || build < 0 || first > build {
		t.Fatal("PREMISE: 00056's Up does not open with SET LOCAL lock_timeout = '1s', or has no CREATE INDEX")
	}
	stmt := strings.Replace(up, "employees_tenant_updated_idx", "employees_tenant_updated_probe", 1)

	held, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- d.WithTenant(context.Background(), tenantID, func(ctx context.Context, htx pgx.Tx) error {
			if _, err := htx.Exec(ctx, `INSERT INTO employees (tenant_id, location_id, full_name) VALUES ($1, $2, 'API-8 lock probe')`, tenantID, venue); err != nil {
				return err
			}
			close(held)
			<-release
			return errors.New("rolled back on purpose")
		})
	}()
	<-held
	timer := time.AfterFunc(4*time.Second, func() { close(release) })
	defer func() {
		if timer.Stop() {
			close(release)
		}
		if err := <-holder; err == nil || err.Error() != "rolled back on purpose" {
			t.Errorf("the holder: %v", err)
		}
	}()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = sp.Exec(ctx, stmt)
	took := time.Since(start)
	_ = sp.Rollback(ctx)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55P03" || took > 3*time.Second {
		t.Fatalf("00056's index build behind an open employees write: %v after %v, want 55P03 within about a second", err, took)
	}
}

// TestHoldingFunctions_AskThePeerSetOnceNeverPerTenant is T124's pin (00060, ADR 0027 §2 and its T124
// note): holding_directory and holding_site_locations ask holding_peer's SET form exactly once and its
// pair form NEVER, in every context -- a member's (KM), a business in no holding, no context at all --
// however many tenants the database holds. Before 00060 a member's call asked the pair form once per
// tenant (the tenants walk: ~1 s at 21 000 tenants), and 00055's gate spared only the business in no
// holding. And the set form decides strictly: ten seconds after KM left (is_member's 60-second
// tolerance would still say yes) both answer KM no row.
//
// MUTANT (measured, ADR 0027's T124 note): either body given back its tenants walk (00055's peers CTE,
// the gate included) -- that function asks the pair form once per tenant in the member's context, red;
// and with the gate gone too, in every context.
func TestHoldingFunctions_AskThePeerSetOnceNeverPerTenant(t *testing.T) {
	ctx, tx := opTx(t)
	if _, err := tx.Exec(ctx, `SET LOCAL track_functions = 'all'`); err != nil {
		t.Fatal(err)
	}
	w := holdingWorldTx(t, ctx, tx, "")
	alone := opNewTenant(t, ctx, tx, "t124 alone "+opToken(t), 0)
	holdingNewEmployee(t, ctx, tx, w.kf, "active")
	if _, err := tx.Exec(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'api8 alone venue')`, alone); err != nil {
		t.Fatal(err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM tenants`); n < 5 {
		t.Fatalf("PREMISE: %d tenants", n)
	}
	for name, stmt := range map[string]string{
		"holding_directory":      `SELECT * FROM public.holding_directory(NULL, NULL)`,
		"holding_site_locations": `SELECT * FROM public.holding_site_locations(NULL)`,
	} {
		for _, c := range []struct {
			who    string
			tenant uuid.UUID
		}{{"a member (KM)", w.km}, {"a business in no holding", alone}, {"no context", uuid.Nil}} {
			if pair, set := holdingPeerCallsByForm(t, ctx, tx, c.tenant, stmt); pair != 0 || set != 1 {
				t.Errorf("%s in the context of %s asked holding_peer's pair form %d times and its set form %d times; want 0 and exactly 1",
					name, c.who, pair, set)
			}
		}
	}
	if got := holdingDirectoryIn(t, ctx, tx, w.km, nil, nil); len(got) == 0 {
		t.Fatal("PREMISE: KM reads KF's people while a member")
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `UPDATE holding_members SET left_at = clock_timestamp() - interval '10 seconds'
		                           WHERE member_tenant_id = $1 AND left_at IS NULL`, w.km); err != nil {
			t.Fatal(err)
		}
		var member bool
		if err := sp.QueryRow(ctx, `SELECT public.holding_peer($1, NULL, 'is_member')`, w.km).Scan(&member); err != nil || !member {
			t.Fatalf("PREMISE: is_member 10 s after leaving: %v %v", member, err)
		}
		if got := holdingDirectoryIn(t, ctx, sp, w.km, nil, nil); len(got) != 0 {
			t.Errorf("holding_directory 10 s after KM left: %d rows, want none", len(got))
		}
		var n int64
		if err := holdingAppIn(t, ctx, sp, w.km, func(q pgx.Tx) error {
			return q.QueryRow(ctx, `SELECT count(*) FROM public.holding_site_locations(NULL)`).Scan(&n)
		}); err != nil || n != 0 {
			t.Errorf("holding_site_locations 10 s after KM left: %d rows (%v), want none", n, err)
		}
	})
}
