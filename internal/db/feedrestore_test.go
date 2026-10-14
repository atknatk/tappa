package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// feedrestore_test.go -- M11 API-2: the restore half of the punch feed (ADR 0028 §5 "Geri
// yükleme"; plan §5 API-2 (c), (n), (q)) and the backfill's lock time ((l)).

// feedRestoreScript returns scripts/pg-restore-verify.sh's FEED_COUNTERS_SQL with its gap filled in,
// and the gap -- the text the operator's restore runs, cut from the file.
func feedRestoreScript(t *testing.T) (sql string, gap int64) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "pg-restore-verify.sh"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	g := regexp.MustCompile(`(?m)^FEED_RESTORE_GAP=([0-9]+)$`).FindStringSubmatch(src)
	q := regexp.MustCompile(`(?s)\nFEED_COUNTERS_SQL="(.*?)"\n`).FindStringSubmatch(src)
	if g == nil || q == nil {
		t.Fatal("scripts/pg-restore-verify.sh carries no FEED_RESTORE_GAP=<n> or FEED_COUNTERS_SQL=\"...\" line")
	}
	gap, err = strconv.ParseInt(g[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`if [ "$f_tenants" = "$f_counters" ] && [ "$f_missing" = "0" ] && [ "$f_undumped" = "0" ]; then`,
		`if [ "$f_behind" = "0" ]; then`,
		`elif ! grep -q '^feed_counter ' "$MANIFEST"; then`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("scripts/pg-restore-verify.sh section 6 no longer decides by %q", want)
		}
	}
	return strings.ReplaceAll(q[1], "$FEED_RESTORE_GAP", g[1]), gap
}

// feedVerdict is FEED_COUNTERS_SQL's five numbers.
type feedVerdict struct{ tenants, counters, missing, behind, undumped int64 }

func (v feedVerdict) pass() bool {
	return v.tenants == v.counters && v.missing == 0 && v.behind == 0 && v.undumped == 0
}

// feedVerifyScratch is the verifier's SQL with its three tables renamed to this test's scratch
// copies (feedScratchCopies). The verifier judges a RESTORED database -- the whole of it, every
// counter -- and a test on the shared database must not lock every tenant's counter row (a bump
// of all of them held every other package's ok taps past the triggers' 2 s lock_timeout --
// measured in the first full run: a handler test's tap answered 500).
var feedVerifyScratch = strings.NewReplacer("public.tenants", "zz_api2_rv_tenants", "public.feed_counters", "zz_api2_rv_counters",
	"public.punch_events", "zz_api2_rv_events")

func feedVerify(t *testing.T, sql string, q pgx.Tx) feedVerdict {
	t.Helper()
	var line string
	if err := q.QueryRow(t.Context(), feedVerifyScratch.Replace(sql)).Scan(&line); err != nil {
		t.Fatalf("FEED_COUNTERS_SQL: %v", err)
	}
	var v feedVerdict
	if _, err := fmt.Sscanf(line, "%d %d %d %d %d", &v.tenants, &v.counters, &v.missing, &v.behind, &v.undumped); err != nil {
		t.Fatalf("FEED_COUNTERS_SQL answered %q: %v", line, err)
	}
	return v
}

// feedScratchCopies copies, inside the test's transaction, this database's tenants (ids),
// counters and events (tenant, seq) into the scratch tables feedVerifyScratch names -- the
// restored database the verifier is shown -- and the counters again into zz_feed_dumped, the
// table the script fills from the manifest's feed_counter lines (the DUMP's values: a restore
// is the dump, so as restored they are equal).
func feedScratchCopies(t *testing.T, tx pgx.Tx) {
	t.Helper()
	for _, s := range []string{
		`CREATE TABLE zz_api2_rv_tenants AS SELECT id FROM tenants`,
		`CREATE TABLE zz_api2_rv_counters AS SELECT tenant_id, last_seq FROM feed_counters`,
		`CREATE TABLE zz_feed_dumped AS SELECT tenant_id, last_seq FROM feed_counters`,
		`CREATE INDEX ON zz_feed_dumped (tenant_id)`,
		`CREATE TABLE zz_api2_rv_events AS SELECT tenant_id, seq FROM punch_events`,
		`CREATE INDEX ON zz_api2_rv_events (tenant_id, seq)`,
		`CREATE INDEX ON zz_api2_rv_counters (tenant_id)`,
	} {
		if _, err := tx.Exec(t.Context(), s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// feedBumpStatement is the restore runbook's step (deploy/README.md, A YOLU 6 and B YOLU B4).
func feedBumpStatement(gap int64) string {
	return fmt.Sprintf("UPDATE public.feed_counters SET last_seq = last_seq + %d;", gap)
}

// TestRestoreVerify_CountersAheadOfEvents is plan §5 API-2 (c), ADR 0028 §5 "Geri yükleme": the
// verifier's own SQL (cut from scripts/pg-restore-verify.sh) judged against the states a restore
// can leave -- shown as scratch copies of this database's tenants, counters and events, each state
// in a savepoint:
//   - a database restored and NOT moved ahead (this one, as it is): every counter is behind --
//     FAIL;
//   - after the runbook's step (deploy/README.md carries it verbatim, with the script's gap, in
//     both restore paths and the drill): PASS;
//   - after the step, one tenant's counter set back to its last event (the step forgotten for a
//     tenant): FAIL, that one;
//   - after the step, an event written past a tenant's counter (a dump whose counter is older than
//     its events): FAIL, that one;
//   - two restores in a row, the second without its step (the dump holds counters an earlier
//     restore moved): FAIL by the dumped values (scripts/pg-backup.sh writes them to the
//     manifest), PASS with the step; a counter with no dumped value: FAIL.
func TestRestoreVerify_CountersAheadOfEvents(t *testing.T) {
	ctx, tx := opTx(t)
	sql, gap := feedRestoreScript(t)
	bump := feedBumpStatement(gap)
	readme, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(readme), bump); n < 3 {
		t.Errorf("deploy/README.md carries %q %d time(s), want it in A YOLU 6, B YOLU B4 and the drill (3)", bump, n)
	}
	w := feedWorldTx(t, ctx, tx, "")
	feedMust(t, ctx, tx, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok"))
	feedScratchCopies(t, tx)
	as := feedVerify(t, sql, tx)
	if as.pass() || as.behind != as.counters || as.counters == 0 {
		t.Errorf("this database, never moved ahead: %+v -- every counter must read as behind", as)
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, feedVerifyScratch.Replace(bump)); err != nil {
			t.Fatal(err)
		}
		if v := feedVerify(t, sql, sp); !v.pass() {
			t.Errorf("after the runbook's step: %+v, want PASS", v)
		}
		holdingInSavepoint(t, sp, func(q pgx.Tx) {
			if _, err := q.Exec(ctx, `UPDATE zz_api2_rv_counters SET last_seq = (SELECT max(seq) FROM zz_api2_rv_events WHERE tenant_id = $1) WHERE tenant_id = $1`, w.kf); err != nil {
				t.Fatal(err)
			}
			if v := feedVerify(t, sql, q); v.pass() || v.behind != 1 {
				t.Errorf("one tenant's step forgotten: %+v, want FAIL with 1 behind", v)
			}
		})
		holdingInSavepoint(t, sp, func(q pgx.Tx) {
			if _, err := q.Exec(ctx, `INSERT INTO zz_api2_rv_events SELECT tenant_id, last_seq + 1 FROM zz_api2_rv_counters WHERE tenant_id = $1`, w.kf); err != nil {
				t.Fatal(err)
			}
			if v := feedVerify(t, sql, q); v.pass() || v.behind != 1 {
				t.Errorf("an event past a moved counter: %+v, want FAIL with 1 behind", v)
			}
		})
		// TWO RESTORES IN A ROW (the security review's D1): the database above -- restored and
		// moved ahead -- is backed up with no event written since (its dump holds the MOVED
		// counters) and restored again; the step is skipped this time. Every counter still
		// stands GAP past its last event, so the first half alone passed it; the dumped values
		// fail it. CONTROL: with the step, PASS.
		holdingInSavepoint(t, sp, func(q pgx.Tx) {
			if _, err := q.Exec(ctx, `UPDATE zz_feed_dumped d SET last_seq = c.last_seq FROM zz_api2_rv_counters c WHERE c.tenant_id = d.tenant_id`); err != nil {
				t.Fatal(err)
			}
			if v := feedVerify(t, sql, q); v.pass() || v.behind != v.counters {
				t.Errorf("the second restore, its step skipped: %+v, want FAIL with every counter behind its dumped value", v)
			}
			if _, err := q.Exec(ctx, feedVerifyScratch.Replace(bump)); err != nil {
				t.Fatal(err)
			}
			if v := feedVerify(t, sql, q); !v.pass() {
				t.Errorf("CONTROL: the second restore with its step: %+v, want PASS", v)
			}
			if _, err := q.Exec(ctx, `DELETE FROM zz_feed_dumped WHERE tenant_id = $1`, w.kf); err != nil {
				t.Fatal(err)
			}
			if v := feedVerify(t, sql, q); v.pass() || v.undumped != 1 {
				t.Errorf("a counter with no dumped value in the manifest: %+v, want FAIL with 1 undumped", v)
			}
		})
	})
}

// TestRestoreVerify_EveryTenantHasACounter is plan §5 API-2 (q)'s verifier half: after the runbook's
// step, a tenant WITHOUT a counter row fails the restore -- one whose row was lost
// (count(tenants) <> count(feed_counters), and named as missing) and one born with the tenants
// trigger off (the same) -- and the step itself makes no row (an UPDATE). CONTROL: a tenant born
// with the trigger on has its row.
func TestRestoreVerify_EveryTenantHasACounter(t *testing.T) {
	ctx, tx := opTx(t)
	sql, gap := feedRestoreScript(t)
	lost := opNewTenant(t, ctx, tx, "api2 lost counter "+opToken(t), 0)
	if c := feedCounter(t, ctx, tx, lost); c != 0 {
		t.Fatalf("CONTROL: a new tenant's counter row is %d, want 0 (feed_open_counter)", c)
	}
	var unborn uuid.UUID
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `ALTER TABLE tenants DISABLE TRIGGER tenants_feed_counter`); err != nil {
			t.Fatal(err)
		}
		unborn = opNewTenant(t, ctx, sp, "api2 no counter "+opToken(t), 0)
		if c := feedCounter(t, ctx, sp, unborn); c != -1 {
			t.Errorf("a tenant born with the counter trigger off has a counter row (%d)", c)
		}
		feedScratchCopies(t, sp)
		if _, err := sp.Exec(ctx, feedVerifyScratch.Replace(feedBumpStatement(gap))); err != nil {
			t.Fatal(err)
		}
		if v := feedVerify(t, sql, sp); v.pass() || v.missing != 1 || v.tenants != v.counters+1 {
			t.Errorf("a tenant born with the counter trigger off: %+v, want FAIL, 1 missing", v)
		}
		q, err := sp.Begin(ctx) // nested in the locked savepoint, whose strong locks it shares
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := q.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		}()
		{
			if _, err := q.Exec(ctx, `INSERT INTO zz_api2_rv_counters VALUES ($1, $2)`, unborn, gap); err != nil {
				t.Fatal(err)
			}
			if _, err := q.Exec(ctx, `INSERT INTO zz_feed_dumped VALUES ($1, 0)`, unborn); err != nil {
				t.Fatal(err)
			}
			if v := feedVerify(t, sql, q); !v.pass() {
				t.Errorf("CONTROL: with its row the database passes: %+v", v)
			}
			if _, err := q.Exec(ctx, `DELETE FROM zz_api2_rv_counters WHERE tenant_id = $1`, lost); err != nil {
				t.Fatal(err)
			}
			if v := feedVerify(t, sql, q); v.pass() || v.missing != 1 || v.tenants != v.counters+1 {
				t.Errorf("a tenant whose counter row was lost: %+v, want FAIL, 1 missing", v)
			}
		}
	})
}

// ------------------------------------------------------------- (n) the fresh pod --

// feedRolesBlock returns the FEED ROLE block of scripts/db-init/01-roles.sql exactly as the
// runbook's sed extracts it (markers included).
func feedRolesBlock(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "db-init", "01-roles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	const begin, end = "-- >>> FEED ROLE (M11 API-2) >>>", "-- <<< FEED ROLE (M11 API-2) <<<"
	src := string(b)
	i, j := strings.Index(src, begin), strings.Index(src, end)
	if i < 0 || j < i {
		t.Fatalf("01-roles.sql does not carry the FEED ROLE markers in order (begin=%d end=%d)", i, j)
	}
	return src[i : j+len(end)]
}

// TestRestore_FreshPodCarriesTheFeedRole is plan §5 API-2 (n), ADR 0028 §4 (pg_dump carries no role,
// so the restore rehearsal is to a pod built from nothing): the role a fresh volume gets is
// 01-roles.sql's FEED ROLE block -- run here, cut as the runbook cuts it, on this cluster taken
// below 00048 with the role renamed away (a cluster that never had it): it creates
// tappa_feedwriter NOLOGIN NOSUPERUSER NOBYPASSRLS with USAGE on public; run again it is a no-op;
// run over a role that can log in and bypasses RLS it takes both away; and 00048's precondition
// then passes and 00048's and 00049's Ups apply against the fresh role, which then owns the two
// trigger functions. 00048's refusal names the runbook section, which exists and cuts the block by
// the same markers. (Measured on a throwaway Postgres built from scripts/db-init: the role was
// there and goose went to HEAD -- the API-2 report.)
func TestRestore_FreshPodCarriesTheFeedRole(t *testing.T) {
	ctx, tx := opTx(t)
	block := feedRolesBlock(t)
	readme, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	pre := holdingPrecondition(t, op00048File)
	if !strings.Contains(pre, `section "Feed role (M11 API-2)"`) || !strings.Contains(pre, "scripts/db-init/01-roles.sql") {
		t.Fatal("00048's refusal does not name the runbook section and the block")
	}
	if !regexp.MustCompile(`(?m)^#+ .*Feed role \(M11 API-2\)`).Match(readme) {
		t.Error(`deploy/README.md has no heading "Feed role (M11 API-2)"`)
	}
	for _, marker := range []string{">>> FEED ROLE (M11 API-2) >>>", "<<< FEED ROLE (M11 API-2) <<<"} {
		if !strings.Contains(string(readme), marker) {
			t.Errorf("deploy/README.md's runbook does not cut the block by %q", marker)
		}
	}
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		feedBelow48(t, ctx, sp)
		if _, err := sp.Exec(ctx, `ALTER ROLE tappa_feedwriter RENAME TO zz_api2_was_feedwriter`); err != nil {
			t.Fatal(err)
		}
		opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00048's precondition with no tappa_feedwriter")
		shape := func() (login, super, bypass, usage bool) {
			if err := sp.QueryRow(ctx, `SELECT rolcanlogin, rolsuper, rolbypassrls, has_schema_privilege(oid, 'public', 'USAGE')
			                             FROM pg_roles WHERE rolname = 'tappa_feedwriter'`).Scan(&login, &super, &bypass, &usage); err != nil {
				t.Fatalf("read the role: %v", err)
			}
			return
		}
		for k := 0; k < 2; k++ {
			opRunSection(t, ctx, sp, block, "the FEED ROLE block")
			if login, super, bypass, usage := shape(); login || super || bypass || !usage {
				t.Errorf("run %d of the block: login=%v super=%v bypassrls=%v usage=%v", k+1, login, super, bypass, usage)
			}
		}
		if _, err := sp.Exec(ctx, `ALTER ROLE tappa_feedwriter LOGIN BYPASSRLS`); err != nil {
			t.Fatal(err)
		}
		opWant(t, holdingNestedRun(t, ctx, sp, pre), sqlstatePrerequisiteState, "00048's precondition with a feed writer that logs in and bypasses RLS")
		opRunSection(t, ctx, sp, block, "the FEED ROLE block over a role that can log in and bypasses RLS")
		if login, _, bypass, _ := shape(); login || bypass {
			t.Errorf("the block left tappa_feedwriter login=%v bypassrls=%v", login, bypass)
		}
		if err := holdingNestedRun(t, ctx, sp, pre); err != nil {
			t.Errorf("00048's precondition refuses the role the block made: %v", err)
		}
		feedUpAgain(t, ctx, sp)
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE proowner = 'tappa_feedwriter'::regrole`); n != 2 {
			t.Errorf("after the Ups the fresh role owns %d functions, want punch_feed and feed_open_counter", n)
		}
	})
}

// ------------------------------------------------------------ (l) the backfill's time --

// feedBackfillRows is how many transactions rows TestFeedBackfill_WithinTwoSeconds backfills in
// CI. The production-sized measurement (ADR 0028's "API-2 uygulama notu") is the throwaway
// container's: 700 000 real rows, 595 000 events, 510 chunks, the longest 0.37 s.
const feedBackfillRows = 200000

// TestFeedBackfill_WithinTwoSeconds is plan §5 API-2 (l), ADR 0028 §4 ("eşik 2 sn"). The single-
// transaction backfill under 00049's lock measured 5.2 s at 700 000 rows (the API-2 note), so the
// history is 00050's, chunk by chunk; what a tap can wait behind is ONE chunk's hold of its
// tenant's counter row. Measured here on scratch copies (this test's transaction, rolled back; no
// other session waits on them) of transactions, transaction_reviews, punch_events and
// feed_counters holding feedBackfillRows punches over 200 tenants (90% ok, half the flags
// approved), the employees, venues and tenants joined being this database's: 00050's own block
// (COMMITs taken out) writes exactly one event per eligible punch, numbered 1..n densely per
// tenant in occurred_at order, every counter at its tenant's last seq, and its LONGEST chunk is
// under 2 s. And 00049's own work under its lock does not grow with the rows (no backfill in it).
func TestFeedBackfill_WithinTwoSeconds(t *testing.T) {
	var notice string
	ctx, tx := opTx(t, func(_ *pgconn.PgConn, n *pgconn.Notice) {
		if strings.HasPrefix(n.Message, "api2 chunks") {
			notice = n.Message
		}
	})
	up49, _ := opMigrationSections(t, op00049File)
	if strings.Contains(up49, "INSERT INTO public.punch_events (") && strings.Contains(up49, "row_number()") {
		t.Error("00049 writes history under its lock again -- the 2 s rule (00050 is where the backfill lives)")
	}
	block := feedBackfillBlock(t, false)
	var tenants []uuid.UUID
	for i := 0; i < 200; i++ {
		tenants = append(tenants, opNewTenant(t, ctx, tx, "api2 backfill "+strconv.Itoa(i)+" "+opToken(t), 0))
	}
	for _, s := range []string{
		`CREATE TABLE zz_api2_tx (LIKE transactions INCLUDING DEFAULTS)`,
		`CREATE TABLE zz_api2_rev (LIKE transaction_reviews INCLUDING DEFAULTS)`,
		`CREATE TABLE zz_api2_events (LIKE punch_events INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)`,
		`CREATE TABLE zz_api2_counters (LIKE feed_counters INCLUDING DEFAULTS INCLUDING INDEXES)`,
		`CREATE INDEX zz_api2_tx_tenant ON zz_api2_tx (tenant_id, occurred_at)`,
		`CREATE UNIQUE INDEX zz_api2_tx_key ON zz_api2_tx (id, tenant_id)`,
		`CREATE INDEX zz_api2_rev_tx ON zz_api2_rev (transaction_id)`,
		fmt.Sprintf(`INSERT INTO zz_api2_tx (id, tenant_id, employee_id, occurred_at, created_at, verdict, channel, type, practice)
		             SELECT gen_random_uuid(), ($1::uuid[])[1 + (g %% 200)], gen_random_uuid(), now() - make_interval(secs => g), now(),
		                    CASE WHEN g %% 10 = 0 THEN 'flag' ELSE 'ok' END, CASE WHEN g %% 3 = 0 THEN 'qr' ELSE 'nfc' END, 'in', false
		               FROM generate_series(1, %d) AS g`, feedBackfillRows),
		`INSERT INTO zz_api2_rev (tenant_id, transaction_id, reviewer_id, outcome, reviewed_at)
		 SELECT x.tenant_id, x.id, gen_random_uuid(), 'approved', now() FROM zz_api2_tx x WHERE x.verdict = 'flag' AND random() < 0.5`,
		`INSERT INTO zz_api2_counters SELECT unnest($1::uuid[]), 0`,
		`ANALYZE zz_api2_tx`, `ANALYZE zz_api2_rev`,
	} {
		var args []any
		if strings.Contains(s, "$1") {
			args = []any{tenants}
		}
		if _, err := tx.Exec(ctx, s, args...); err != nil {
			t.Fatalf("%.60s: %v", s, err)
		}
	}
	scratch := strings.NewReplacer("public.transactions", "zz_api2_tx", "public.transaction_reviews", "zz_api2_rev",
		"public.punch_events", "zz_api2_events", "public.feed_counters", "zz_api2_counters")
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE zz_api2_chunks (held double precision)`); err != nil {
		t.Fatal(err)
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		start := time.Now()
		if _, err := sp.Conn().PgConn().Exec(ctx, scratch.Replace(block)).ReadAll(); err != nil {
			t.Fatalf("00050's block on the scratch rows: %v", err)
		}
		took := time.Since(start)
		var chunks int
		var longest, window, lockwait float64
		if _, err := fmt.Sscanf(notice, "api2 chunks %d max %g window %g lockwait %g", &chunks, &longest, &window, &lockwait); err != nil {
			t.Fatalf("the block's timing notice %q: %v", notice, err)
		}
		want := opInt(t, ctx, sp, `SELECT count(*) FROM zz_api2_tx x WHERE x.verdict = 'ok'
		                             OR EXISTS (SELECT 1 FROM zz_api2_rev r WHERE r.transaction_id = x.id AND r.outcome = 'approved')`)
		if got := opInt(t, ctx, sp, `SELECT count(*) FROM zz_api2_events`); got != want {
			t.Errorf("the backfill wrote %d events, want %d (every ok tap and every approved flag)", got, want)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM (SELECT tenant_id FROM zz_api2_events GROUP BY tenant_id
		                                                  HAVING min(seq) <> 1 OR max(seq) <> count(*)) d`); n != 0 {
			t.Errorf("%d tenants' backfilled seqs are not 1..n", n)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM (SELECT e.seq, row_number() OVER (PARTITION BY e.tenant_id ORDER BY e.punch_time, e.punch_id) AS rn
		                                                    FROM zz_api2_events e) d WHERE d.seq <> d.rn`); n != 0 {
			t.Errorf("%d backfilled events are out of occurred_at order", n)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM zz_api2_counters c
		                             WHERE c.last_seq <> coalesce((SELECT max(seq) FROM zz_api2_events e WHERE e.tenant_id = c.tenant_id), 0)`); n != 0 {
			t.Errorf("%d counters do not stand at their tenant's last backfilled seq", n)
		}
		if chunks < 200 || time.Duration(longest*float64(time.Second)) > 2*time.Second {
			t.Errorf("00050's backfill: %d chunks, the longest held a tenant's counter %.3f s -- ADR 0028 §4's 2 s", chunks, longest)
		}
		t.Logf("00050's backfill on %d rows: %d chunks, the longest %.3f s (window %.3f s), %v in all", feedBackfillRows, chunks, longest, window, took)
	})
}

// feedBigTenantRows is the single tenant's history TestFeedBackfill_BigTenantLiveTapsLoseNothing
// backfills: 300 000 by default; TAPPA_API2_BACKFILL_TENANT_ROWS sets another (the throwaway
// container measured 650 000 -- ADR 0028's API-2 note).
func feedBigTenantRows(t *testing.T) int {
	t.Helper()
	if v := os.Getenv("TAPPA_API2_BACKFILL_TENANT_ROWS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1000 {
			t.Fatalf("TAPPA_API2_BACKFILL_TENANT_ROWS=%q is not a count of at least 1000", v)
		}
		return n
	}
	return 300000
}

// TestFeedBackfill_BigTenantLiveTapsLoseNothing is the API-2 review's O-1 (ADR 0028 §4, §5;
// CLAUDE.md §4.6): ONE tenant with feedBigTenantRows punches and no event, backfilled by 00050's
// own block (its COMMITs KEPT -- a connection of its own, at top level) while that tenant's own
// taps and holding taps with a peer arrive the whole time. The tables are a committed scratch
// schema (copies of transactions, transaction_reviews, punch_events and feed_counters with their
// indexes; the tenants, employees and venues joined are this database's) and the live writer is a
// trigger with the shipped counter logic and the shipped lock_timeout (2 s) -- the real triggers
// would write into the real feed. Measured and pinned:
//   - every chunk holds the tenant's counter row well under the triggers' 2 s, whatever the
//     tenant's size (the first version's chunk was O(tenant): a 650 000-row tenant measured 7.1 s);
//   - 0 live writes met a lock timeout (55P03) or a deadlock: no tap ever reached even its first
//     wait, let alone the second one that would lose an advanced record;
//   - the tenant's feed is 1..n, every eligible historical punch has exactly one event, every
//     live write its own.
func TestFeedBackfill_BigTenantLiveTapsLoseNothing(t *testing.T) {
	f := holdingLiveFixture(t)
	ctx := f.ctx
	rows := feedBigTenantRows(t)
	big, peer := f.tenant(t, "Europe/Malta", "BIG"), f.tenant(t, "Europe/Malta", "PEER")
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	lab := "zz_api2_bf_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		if _, err := f.owner.Exec(context.Background(), `DROP SCHEMA `+lab+` CASCADE`); err != nil {
			t.Errorf("drop the scratch schema: %v", err)
		}
	})
	l := func(s string) string { return strings.ReplaceAll(s, "L.", lab+".") }
	for _, s := range []string{
		`CREATE SCHEMA ` + lab,
		l(`CREATE TABLE L.tx (LIKE public.transactions INCLUDING DEFAULTS INCLUDING INDEXES)`),
		l(`CREATE TABLE L.rev (LIKE public.transaction_reviews INCLUDING DEFAULTS INCLUDING INDEXES)`),
		l(`CREATE TABLE L.ev (LIKE public.punch_events INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)`),
		l(`CREATE TABLE L.ctr (LIKE public.feed_counters INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES)`),
		fmt.Sprintf(l(`INSERT INTO L.tx (tenant_id, employee_id, occurred_at, created_at, verdict, channel, type, practice)
		   SELECT $1, gen_random_uuid(), now() - interval '1 day' - make_interval(secs => g), now(),
		          CASE WHEN g %% 10 = 0 THEN 'flag' WHEN g %% 10 = 1 THEN 'reject' ELSE 'ok' END,
		          CASE WHEN g %% 3 = 0 THEN 'qr' ELSE 'nfc' END,
		          CASE WHEN g %% 10 = 1 THEN NULL WHEN g %% 2 = 0 THEN 'in' ELSE 'out' END, false
		     FROM generate_series(1, %d) AS g`), rows),
		l(`INSERT INTO L.rev (tenant_id, transaction_id, reviewer_id, outcome) SELECT tenant_id, id, gen_random_uuid(), 'approved' FROM L.tx WHERE verdict = 'flag' AND random() < 0.5`),
		l(`INSERT INTO L.ctr VALUES ($1, 0), ($2, 0)`),
		l(`ANALYZE L.tx`), l(`ANALYZE L.rev`), l(`ANALYZE L.ev`),
		// The live writer: the shipped counter logic (punch_feed for an own row, holding_feed's
		// K-31 pre-lock for a holding row) with the shipped lock_timeout.
		l(`CREATE FUNCTION L.live() RETURNS trigger LANGUAGE plpgsql SET lock_timeout = '2s' AS $$
		    DECLARE s bigint; n integer;
		    BEGIN
		      IF NEW.verdict <> 'ok' THEN RETURN NULL; END IF;
		      IF NEW.site_tenant_id IS NULL THEN
		        INSERT INTO L.ctr AS c VALUES (NEW.tenant_id, 1) ON CONFLICT (tenant_id) DO UPDATE SET last_seq = c.last_seq + 1 RETURNING c.last_seq INTO s;
		        INSERT INTO L.ev (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction, employee_id)
		        VALUES (NEW.tenant_id, s, NEW.id, NEW.tenant_id, '{employer,location}', NEW.occurred_at, now(), NEW.channel, NEW.type, NEW.employee_id);
		      ELSE
		        INSERT INTO L.ctr VALUES (LEAST(NEW.tenant_id, NEW.site_tenant_id), 0), (GREATEST(NEW.tenant_id, NEW.site_tenant_id), 0) ON CONFLICT (tenant_id) DO NOTHING;
		        SELECT count(*) INTO n FROM (SELECT 1 FROM L.ctr WHERE tenant_id IN (NEW.tenant_id, NEW.site_tenant_id) ORDER BY tenant_id FOR UPDATE) k;
		        INSERT INTO L.ctr AS c VALUES (NEW.tenant_id, 1) ON CONFLICT (tenant_id) DO UPDATE SET last_seq = c.last_seq + 1 RETURNING c.last_seq INTO s;
		        INSERT INTO L.ev (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction, employee_id)
		        VALUES (NEW.tenant_id, s, NEW.id, NEW.tenant_id, '{employer}', NEW.occurred_at, now(), NEW.channel, NEW.type, NEW.employee_id);
		        INSERT INTO L.ctr AS c VALUES (NEW.site_tenant_id, 1) ON CONFLICT (tenant_id) DO UPDATE SET last_seq = c.last_seq + 1 RETURNING c.last_seq INTO s;
		        INSERT INTO L.ev (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction, employee_id)
		        VALUES (NEW.site_tenant_id, s, NEW.id, NEW.tenant_id, '{location}', NEW.occurred_at, now(), NEW.channel, NEW.type, NEW.employee_id);
		      END IF;
		      RETURN NULL;
		    END $$`),
		l(`CREATE TRIGGER live AFTER INSERT ON L.tx FOR EACH ROW EXECUTE FUNCTION L.live()`),
	} {
		var args []any
		switch {
		case strings.Contains(s, "$2"):
			args = []any{big, peer}
		case strings.Contains(s, "$1") && !strings.Contains(s, "$$"):
			args = []any{big}
		}
		if _, err := f.owner.Exec(ctx, s, args...); err != nil {
			t.Fatalf("build the scratch schema: %v\n%.200s", err, s)
		}
	}
	eligible := opInt(t, ctx, f.owner, l(`SELECT count(*) FROM L.tx x WHERE x.type IS NOT NULL AND (x.verdict = 'ok'
	                                     OR (x.verdict = 'flag' AND EXISTS (SELECT 1 FROM L.rev r WHERE r.transaction_id = x.id)))`))

	// The backfill: 00050's block on a connection of its own, at top level (its COMMITs run).
	block := strings.NewReplacer("public.transactions", lab+".tx", "public.transaction_reviews", lab+".rev",
		"public.punch_events", lab+".ev", "public.feed_counters", lab+".ctr").Replace(feedBackfillBlock(t, true))
	var notice string
	cfg, err := pgx.ParseConfig(opOwnerDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
		if strings.HasPrefix(n.Message, "api2 chunks") {
			notice = n.Message
		}
	}
	bf, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bf.Close(context.Background()) })
	if _, err := bf.Exec(ctx, `CREATE TEMP TABLE zz_api2_chunks (held double precision)`); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var mu sync.Mutex
	codes := map[string]int{}
	written := 0
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		c := f.connect(t, ctx)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for k := 0; ; k++ {
				select {
				case <-done:
					return
				default:
				}
				employer, site := big, any(nil)
				switch {
				case i >= 4 && k%2 == 0:
					site = peer
				case i >= 4:
					employer, site = peer, big
				}
				_, err := c.Exec(ctx, l(`INSERT INTO L.tx (tenant_id, site_tenant_id, employee_id, occurred_at, verdict, channel, type)
				                          VALUES ($1, $2, gen_random_uuid(), now(), 'ok', 'nfc', 'in')`), employer, site)
				mu.Lock()
				if err != nil {
					code, _ := opCode(err)
					codes[code]++
				} else {
					written++
				}
				mu.Unlock()
				time.Sleep(time.Duration(20+feedRandN(30)) * time.Millisecond)
			}
		}(i)
	}
	start := time.Now()
	_, bfErr := bf.PgConn().Exec(ctx, block).ReadAll()
	took := time.Since(start)
	close(done)
	wg.Wait()
	if bfErr != nil {
		t.Fatalf("00050's block: %v", bfErr)
	}
	var chunks int
	var longest, window, lockwait float64
	if _, err := fmt.Sscanf(notice, "api2 chunks %d max %g window %g lockwait %g", &chunks, &longest, &window, &lockwait); err != nil {
		t.Fatalf("the block's timing notice %q: %v", notice, err)
	}
	var median, p99 float64
	var over500 int64
	if err := bf.QueryRow(ctx, `SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY held), percentile_cont(0.99) WITHIN GROUP (ORDER BY held),
	                                   count(*) FILTER (WHERE held > 0.5) FROM pg_temp.zz_api2_chunks`).Scan(&median, &p99, &over500); err != nil {
		t.Fatal(err)
	}
	t.Logf("00050 on one tenant of %d rows (%d eligible): %d chunks, the counter held median %.3f s, p99 %.3f s, longest %.3f s, %d over 0.5 s "+
		"(the longest window read, unlocked, %.3f s; the longest wait for the counter %.3f s), %v in all; %d live writes, errors %v",
		rows, eligible, chunks, median, p99, longest, over500, window, lockwait, took, written, codes)
	if len(codes) != 0 {
		t.Errorf("live writes during the backfill met errors %v (55P03: a tap waited out the counter's lock_timeout)", codes)
	}
	if time.Duration(longest*float64(time.Second)) > 2*time.Second || chunks < rows/2000 {
		t.Errorf("%d chunks, the longest held the counter %.3f s -- over the triggers' 2 s", chunks, longest)
	}
	if written < 20 {
		t.Errorf("anti-vacuity: only %d live writes during the backfill", written)
	}
	if n := opInt(t, ctx, f.owner, l(`SELECT count(*) FROM (SELECT 1 FROM L.ev WHERE tenant_id = $1 HAVING min(seq) <> 1 OR max(seq) <> count(*)) d`), big); n != 0 {
		t.Error("the big tenant's feed is not 1..n")
	}
	if n := opInt(t, ctx, f.owner, l(`SELECT count(*) FROM L.tx x WHERE x.type IS NOT NULL AND (x.verdict = 'ok'
	                                   OR (x.verdict = 'flag' AND EXISTS (SELECT 1 FROM L.rev r WHERE r.transaction_id = x.id)))
	                                   AND x.tenant_id = $1 AND (SELECT count(*) FROM L.ev e WHERE e.punch_id = x.id AND e.tenant_id = $1) <> 1`), big); n != 0 {
		t.Errorf("%d of the big tenant's eligible punches do not have exactly one event in its feed", n)
	}
}
