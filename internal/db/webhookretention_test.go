package db

// webhookretention_test.go -- T130 (00058, 00059; ADR 0028 "T130 uygulama notu"): a delivered or dead
// webhook delivery is deleted 90 days after it settled, a pending one never. Against real Postgres,
// inside opTx's rolled-back transaction (no row of these tests is ever committed). Each of the three
// guards is measured ALONE -- the query (as the owner, who is not held by the policies), the
// restrictive policy (a raw, unfiltered DELETE as tappa_app), and the trigger that is settled_at's
// one writer -- so removing any one of them is red here even while the other two still hold. The
// sweep on the worker's turn, its throttle and the concurrent halves are internal/egress's
// (webhook_retention_db_test.go); the Go constant's agreement with the policy's 90 is
// internal/webhook/delivery's.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
)

// retentionDays is delivery.RetentionDays (internal/db cannot import that package: it imports this
// one). internal/webhook/delivery's TestWebhookRetention_TheQueryAndThePolicyAgree pins both to the
// policy's 90.
const retentionDays = 90

// retentionWorld is feedWorldTx's KF and KM, each with its API open and an active endpoint, so every
// tap of theirs writes one delivery into its own tenant.
func retentionWorld(t *testing.T, ctx context.Context, tx pgx.Tx) feedWorld {
	t.Helper()
	w := feedWorldTx(t, ctx, tx, "")
	feedOpen(t, ctx, tx, w.kf, "KF", true)
	feedOpen(t, ctx, tx, w.km, "KM", true)
	return w
}

// retentionTaps writes n taps and returns tenant's (KF's or KM's) deliveries of them, oldest first.
func retentionTaps(t *testing.T, ctx context.Context, tx pgx.Tx, w feedWorld, tenant uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	// A holding punch (KM's employee on KF's plaque): one event in each feed, so one delivery in each
	// business -- and ONE counter set for the whole transaction (00049's TF001: a transaction's events
	// stay in the set its first event locked), whichever business a test's rows are for.
	row := w.kmAtKF("nfc", "ok")
	var out []uuid.UUID
	for range n {
		punch := feedMust(t, ctx, tx, row)
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT d.id FROM webhook_deliveries d
		                            JOIN punch_events e ON e.tenant_id = d.tenant_id AND e.seq = d.punch_event_seq
		                            WHERE d.tenant_id = $1 AND e.punch_id = $2`, tenant, punch).Scan(&id); err != nil {
			t.Fatalf("the tap's delivery: %v", err)
		}
		out = append(out, id)
	}
	return out
}

// retentionForge writes a delivery's state and settled_at as the owner with the triggers OFF
// (00058's trigger keeps settled_at on any write that does not change the state, and dates a change
// itself -- so a lab row at a chosen age is written around it). ago "" is NULL. A pending row with a
// date is a FORGERY the trigger cannot produce: the case that proves the other two guards alone.
func retentionForge(t *testing.T, ctx context.Context, q opQuerier, id uuid.UUID, state, ago string) {
	t.Helper()
	var at any
	if ago != "" {
		at = ago
	}
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`SET LOCAL session_replication_role = replica`, nil},
		{`UPDATE webhook_deliveries SET state = $2, settled_at = statement_timestamp() - $3::interval,
		         delivered_at = CASE WHEN $2 = 'delivered' THEN statement_timestamp() - $3::interval END WHERE id = $1`, []any{id, state, at}},
		{`SET LOCAL session_replication_role = origin`, nil},
	} {
		if _, err := q.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
}

// retentionCase is one lab row: its state and age, and whether a correct guard deletes it.
type retentionCase struct {
	name, state, ago string
	gone             bool
}

// retentionCases are the edges: 91 days settled goes, 89 stays, in both states; pending stays at
// any age -- a real one (no date) and a forged one dated 200 days back.
var retentionCases = []retentionCase{
	{"delivered 91 days ago", "delivered", "91 days", true},
	{"dead 91 days ago", "dead", "91 days", true},
	{"delivered 89 days ago", "delivered", "89 days", false},
	{"dead 89 days ago", "dead", "89 days", false},
	{"pending (never settled)", "pending", "", false},
	{"pending FORGED with a date 200 days back", "pending", "200 days", false},
}

// retentionLab writes retentionCases into tenant and returns each case's delivery id.
func retentionLab(t *testing.T, ctx context.Context, tx pgx.Tx, w feedWorld, tenant uuid.UUID) map[string]uuid.UUID {
	t.Helper()
	ids := retentionTaps(t, ctx, tx, w, tenant, len(retentionCases))
	out := map[string]uuid.UUID{}
	for i, c := range retentionCases {
		retentionForge(t, ctx, tx, ids[i], c.state, c.ago)
		out[c.name] = ids[i]
	}
	return out
}

func retentionLeft(t *testing.T, ctx context.Context, q opQuerier, id uuid.UUID) bool {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM webhook_deliveries WHERE id = $1`, id) == 1
}

// retentionJudge reports every case whose row is not where a correct guard leaves it.
func retentionJudge(t *testing.T, ctx context.Context, q opQuerier, ids map[string]uuid.UUID, guard string) {
	t.Helper()
	for _, c := range retentionCases {
		if left := retentionLeft(t, ctx, q, ids[c.name]); left == c.gone {
			t.Errorf("%s: %s -- left=%v, want gone=%v", guard, c.name, left, c.gone)
		}
	}
}

// TestWebhookRetention_TheQueryAloneKeepsWhatIsOwed: DeleteSettledWebhookDeliveries run by the OWNER
// -- whom neither policy holds -- is the query's own reach: in KF, the two settled rows past 90 days
// and nothing else (the 89-day ones, the pending one, the forged pending one stay); KM's rows of every
// age stay (the explicit tenant_id). The answer counts what it deleted.
//
// MUTANTS (measured, T130): the state clause removed -> the forged pending row goes, red; the bound at
// 9 days -> the 89-day rows go, red; the tenant_id predicate removed -> KM's 91-day rows go, red.
func TestWebhookRetention_TheQueryAloneKeepsWhatIsOwed(t *testing.T) {
	ctx, tx := opTx(t)
	w := retentionWorld(t, ctx, tx)
	kf, km := retentionLab(t, ctx, tx, w, w.kf), retentionLab(t, ctx, tx, w, w.km)
	n, err := store.New(tx).DeleteSettledWebhookDeliveries(ctx, store.DeleteSettledWebhookDeliveriesParams{
		TenantID: w.kf, RetentionDays: retentionDays, MaxRows: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("the query deleted %d rows, want KF's two settled past 90 days", n)
	}
	retentionJudge(t, ctx, tx, kf, "the query (KF)")
	for name, id := range km {
		if !retentionLeft(t, ctx, tx, id) {
			t.Errorf("the query for KF deleted KM's %s", name)
		}
	}
}

// TestWebhookRetention_ThePolicyAloneKeepsWhatIsOwed: a DELETE with NO predicate at all -- a code bug's
// shape -- run as tappa_app in KF's context removes exactly what the rule allows: KF's settled rows past
// 90 days. The restrictive policy is the only thing between that statement and the table.
//
// MUTANTS (measured, T130): the policy's state clause removed -> the forged pending row goes, red; its
// interval at 9 days -> the 89-day rows go, red; the policy dropped (only the permissive
// tenant_isolation left) -> every KF row goes, red.
func TestWebhookRetention_ThePolicyAloneKeepsWhatIsOwed(t *testing.T) {
	ctx, tx := opTx(t)
	w := retentionWorld(t, ctx, tx)
	kf := retentionLab(t, ctx, tx, w, w.kf)
	var deleted int64
	if err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
		tag, err := sp.Exec(ctx, `DELETE FROM webhook_deliveries`)
		deleted = tag.RowsAffected()
		return err
	}); err != nil {
		t.Fatalf("tappa_app's unfiltered DELETE: %v", err)
	}
	if deleted != 2 {
		t.Errorf("tappa_app's unfiltered DELETE removed %d rows, want 2", deleted)
	}
	retentionJudge(t, ctx, tx, kf, "the policy")
}

// TestRLS_WebhookDeliveries_ADeleteReachesNoOtherTenant is CLAUDE.md §8's RLS test for T130's new verb
// -- the isolation shape (no tenant_id predicate in the statement, so 0 rows is RLS's doing, not a
// WHERE's): KM's context deletes its own settled rows past 90 days and not one of KF's, though KF's
// are just as old; no context deletes nothing; the same statement in KF's context then takes KF's
// (the CONTROL that KF's rows were deletable).
func TestRLS_WebhookDeliveries_ADeleteReachesNoOtherTenant(t *testing.T) {
	ctx, tx := opTx(t)
	w := retentionWorld(t, ctx, tx)
	kf, km := retentionLab(t, ctx, tx, w, w.kf), retentionLab(t, ctx, tx, w, w.km)
	del := func(tenant uuid.UUID) int64 {
		t.Helper()
		var n int64
		if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
			tag, err := sp.Exec(ctx, `DELETE FROM webhook_deliveries`)
			n = tag.RowsAffected()
			return err
		}); err != nil {
			t.Fatalf("DELETE in %s's context: %v", tenant, err)
		}
		return n
	}
	if n := del(uuid.Nil); n != 0 {
		t.Errorf("no context deleted %d rows", n)
	}
	if n := del(w.km); n != 2 {
		t.Errorf("KM's context deleted %d rows, want its own two", n)
	}
	for name, id := range kf {
		if !retentionLeft(t, ctx, tx, id) {
			t.Errorf("KM's context deleted KF's %s", name)
		}
	}
	retentionJudge(t, ctx, tx, km, "KM's own")
	if n := del(w.kf); n != 2 {
		t.Errorf("CONTROL: KF's own context deleted %d of its rows, want 2", n)
	}
}

// TestWebhookRetention_TheClockIsTheDatabases is 00058's trigger: settled_at's one writer.
//   - a new delivery (pending) has none;
//   - tappa_app cannot write it -- not on a pending row, not on a settled one (42501: no column grant);
//   - the worker's result (state delivered or dead, the shape RecordWebhookDeliveryResult writes)
//     dates it with the database's clock; a retry's result (pending -> pending) leaves it NULL;
//   - a resend (dead -> pending) clears it, and the next death dates it anew;
//   - the owner naming the column on a write that does not change the state does not move it;
//   - bound to another table it refuses to fire (55000), and tappa_app cannot bind it at all (42501).
//
// MUTANT (measured, T130): the ELSE arm (keep OLD) removed -> the owner's backdating moves it, red;
// the trigger dropped -> the result leaves it NULL, red.
func TestWebhookRetention_TheClockIsTheDatabases(t *testing.T) {
	ctx, tx := opTx(t)
	w := retentionWorld(t, ctx, tx)
	id := retentionTaps(t, ctx, tx, w, w.kf, 1)[0]
	settled := func() *time.Time {
		t.Helper()
		var at *time.Time
		if err := tx.QueryRow(ctx, `SELECT settled_at FROM webhook_deliveries WHERE id = $1`, id).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	now := func() time.Time {
		t.Helper()
		var at time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return at
	}
	asApp := func(sql string, args ...any) error {
		t.Helper()
		return holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
	}
	if at := settled(); at != nil {
		t.Fatalf("a new delivery is dated %v", at)
	}
	opWant(t, asApp(`UPDATE webhook_deliveries SET settled_at = now() - interval '1 year' WHERE tenant_id = $1 AND id = $2`, w.kf, id),
		sqlstateInsufficientPrivi, "tappa_app dating a pending delivery")
	if err := asApp(`UPDATE webhook_deliveries SET attempt = attempt + 1, state = 'pending', next_attempt_at = clock_timestamp() + interval '1 minute'
	                 WHERE tenant_id = $1 AND id = $2`, w.kf, id); err != nil {
		t.Fatal(err)
	}
	if at := settled(); at != nil {
		t.Fatalf("a retry's result dated the delivery %v", at)
	}
	before := now()
	if err := asApp(`UPDATE webhook_deliveries SET attempt = attempt + 1, state = 'dead' WHERE tenant_id = $1 AND id = $2`, w.kf, id); err != nil {
		t.Fatal(err)
	}
	died := settled()
	if died == nil || died.Before(before) || died.After(now()) {
		t.Fatalf("a death dated the delivery %v, want the database's clock after %v", died, before)
	}
	opWant(t, asApp(`UPDATE webhook_deliveries SET settled_at = now() - interval '1 year' WHERE tenant_id = $1 AND id = $2`, w.kf, id),
		sqlstateInsufficientPrivi, "tappa_app backdating a dead delivery")
	if _, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET settled_at = now() - interval '1 year' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if at := settled(); at == nil || !at.Equal(*died) {
		t.Fatalf("the owner's write without a change of state moved settled_at from %v to %v", died, at)
	}
	if err := asApp(`UPDATE webhook_deliveries SET state = 'pending', next_attempt_at = clock_timestamp(), lease_until = NULL
	                 WHERE tenant_id = $1 AND id = $2`, w.kf, id); err != nil {
		t.Fatal(err)
	}
	if at := settled(); at != nil {
		t.Fatalf("a resend left the delivery dated %v", at)
	}
	before = now()
	if err := asApp(`UPDATE webhook_deliveries SET attempt = attempt + 1, state = 'delivered', delivered_at = clock_timestamp()
	                 WHERE tenant_id = $1 AND id = $2`, w.kf, id); err != nil {
		t.Fatal(err)
	}
	if at := settled(); at == nil || at.Before(before) {
		t.Fatalf("the arrival after a resend dated the delivery %v, want after %v", at, before)
	}

	for _, s := range []string{
		`CREATE TEMP TABLE zz_t130_bind (state text, settled_at timestamptz)`,
		`CREATE TRIGGER zz_t130_bound BEFORE INSERT ON zz_t130_bind FOR EACH ROW EXECUTE FUNCTION public.webhook_delivery_settle()`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	opWant(t, opTry(t, ctx, tx, `INSERT INTO zz_t130_bind VALUES ('dead', NULL)`), sqlstatePrerequisiteState,
		"webhook_delivery_settle fired on a table that is not webhook_deliveries")
	opWant(t, opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `CREATE TEMP TABLE zz_t130_app (state text, settled_at timestamptz)`); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `CREATE TRIGGER zz_t130_app_bound BEFORE INSERT ON zz_t130_app FOR EACH ROW EXECUTE FUNCTION public.webhook_delivery_settle()`)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app binding webhook_delivery_settle to a table of its own")
}

// TestWebhookRetention_TheBatchIsBounded: with 105 settled rows past 90 days, one sweep deletes 100 --
// the OLDEST 100 (it walks settled_at) -- the next the last 5, the third none.
func TestWebhookRetention_TheBatchIsBounded(t *testing.T) {
	ctx, tx := opTx(t)
	w := retentionWorld(t, ctx, tx)
	ids := retentionTaps(t, ctx, tx, w, w.kf, 105)
	for i, id := range ids {
		// ids[0] the oldest: 200 days back, then one minute younger each.
		retentionForge(t, ctx, tx, id, "delivered", fmt.Sprintf("%d minutes", 200*24*60-i))
	}
	sweep := func() int64 {
		t.Helper()
		var n int64
		if err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
			var err error
			n, err = store.New(sp).DeleteSettledWebhookDeliveries(ctx, store.DeleteSettledWebhookDeliveriesParams{
				TenantID: w.kf, RetentionDays: retentionDays, MaxRows: 100,
			})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := sweep(); n != 100 {
		t.Fatalf("the first sweep deleted %d rows, want the batch of 100", n)
	}
	for i, id := range ids {
		if left := retentionLeft(t, ctx, tx, id); left != (i >= 100) {
			t.Errorf("row %d (the %d-th oldest): left=%v -- the batch is not the oldest first", i, i+1, left)
		}
	}
	if n := sweep(); n != 5 {
		t.Errorf("the second sweep deleted %d rows, want the last 5", n)
	}
	if n := sweep(); n != 0 {
		t.Errorf("the third sweep deleted %d rows, want none", n)
	}
}

// TestWebhookRetention_TheFeedAndTheRecordsAreUntouched: the sweep deletes delivery rows and nothing
// else -- the records (KM's: the punches are holding punches) and both feeds' punch_events (append-only,
// CLAUDE.md §4.3) are the same rows before and after, the deleted deliveries' events included; the
// endpoints stay.
func TestWebhookRetention_TheFeedAndTheRecordsAreUntouched(t *testing.T) {
	ctx, tx := opTx(t)
	w := retentionWorld(t, ctx, tx)
	ids := retentionTaps(t, ctx, tx, w, w.kf, 4)
	for _, id := range ids {
		retentionForge(t, ctx, tx, id, "dead", "120 days")
	}
	shape := func() string {
		t.Helper()
		var s string
		if err := tx.QueryRow(ctx, `SELECT
			(SELECT count(*) || ':' || coalesce(md5(string_agg(id::text || verdict || occurred_at::text, ',' ORDER BY id)), '') FROM transactions WHERE tenant_id IN ($1, $2)) || ' | ' ||
			(SELECT count(*) || ':' || coalesce(md5(string_agg(tenant_id::text || seq::text || punch_id::text || version::text || status, ',' ORDER BY tenant_id, seq)), '')
			   FROM punch_events WHERE tenant_id IN ($1, $2)) || ' | ' ||
			(SELECT count(*) FROM webhook_endpoints WHERE tenant_id IN ($1, $2))`, w.kf, w.km).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := shape()
	if err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
		n, err := store.New(sp).DeleteSettledWebhookDeliveries(ctx, store.DeleteSettledWebhookDeliveriesParams{
			TenantID: w.kf, RetentionDays: retentionDays, MaxRows: 100,
		})
		if err == nil && n != int64(len(ids)) {
			err = fmt.Errorf("deleted %d, want %d", n, len(ids))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if after := shape(); after != before {
		t.Errorf("the sweep changed the records or the feed:\n before %s\n after  %s", before, after)
	}
	if strings.HasPrefix(before, "0:") {
		t.Fatalf("PREMISE: KF has no record (%s)", before)
	}
}

// shippedWebhookSQL is the text sqlc generated for one statement of webhooks.sql (internal/store),
// the bytes the product sends.
func shippedWebhookSQL(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "store", "webhooks.sql.go"))
	if err != nil {
		t.Fatal(err)
	}
	src, head := string(b), "const "+name+" = `"
	i := strings.Index(src, head)
	if i < 0 {
		t.Fatalf("no %s in internal/store/webhooks.sql.go", name)
	}
	rest := src[i+len(head):]
	return rest[:strings.Index(rest, "`")]
}

// TestWebhookRetention_TheSweepReadsTheSettledIndex: the sweep's statement, as a GENERIC prepared
// statement (the plan pgx's cache settles on) run as tappa_app in KF's context, walks
// webhook_deliveries_tenant_settled_idx (00059) with the business AND the age bound as its index
// condition -- so a sweep that finds nothing, every sweep in steady state, reads one index page however
// many rows the business keeps. Sequential and bitmap scans are priced out (the claim is about the
// text, not the planner's pick for a small table).
//
// MUTANT (measured, T130): the state clause removed from the query -> the partial index no longer
// applies, red.
func TestWebhookRetention_TheSweepReadsTheSettledIndex(t *testing.T) {
	ctx, tx := opTx(t)
	w := retentionWorld(t, ctx, tx)
	retentionTaps(t, ctx, tx, w, w.kf, 3)
	stmt := shippedWebhookSQL(t, "deleteSettledWebhookDeliveries")
	var plan []string
	if err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
		for _, s := range []string{
			`SET LOCAL plan_cache_mode = force_generic_plan`, `SET LOCAL enable_seqscan = off`, `SET LOCAL enable_bitmapscan = off`,
			`PREPARE t130_sweep(uuid, integer, integer) AS ` + stmt,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return fmt.Errorf("%s: %w", s, err)
			}
		}
		rows, err := sp.Query(ctx, fmt.Sprintf(`EXPLAIN (COSTS OFF) EXECUTE t130_sweep('%s', %d, 100)`, w.kf, retentionDays))
		if err != nil {
			return err
		}
		if plan, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		_, err = sp.Exec(ctx, `DEALLOCATE t130_sweep`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(plan, "\n")
	for _, want := range []string{
		"webhook_deliveries_tenant_settled_idx on webhook_deliveries s",
		"Index Cond: ((tenant_id = $1) AND (settled_at < (statement_timestamp() - make_interval(0, 0, 0, $2, 0, 0, '0'::double precision))))",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the sweep's plan has no %q:\n%s", want, text)
		}
	}
}

// TestWebhookRetention00058And00059_DownGiveBackAndUpTakesItAgain: 00059's Down drops the index and
// 00058's takes the column, the trigger and its function, the policy and tappa_app's DELETE away; the
// Ups give HEAD's surface back. Up again over rows already there dates every settled row at the
// migration's now() -- the catalogue's default, no rewrite -- and leaves the pending ones NULL. A
// second Up refuses (55000) and changes nothing.
func TestWebhookRetention00058And00059_DownGiveBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up58, down58 := opMigrationSections(t, op00058File)
	up59, down59 := opMigrationSections(t, op00059File)
	surface := func(q opQuerier) string {
		t.Helper()
		var s string
		if err := q.QueryRow(ctx, `SELECT
			coalesce((SELECT format_type(atttypid, atttypmod) || ' default=' || coalesce(pg_get_expr(d.adbin, d.adrelid), '-')
			            FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
			           WHERE a.attrelid = 'public.webhook_deliveries'::regclass AND a.attname = 'settled_at' AND NOT a.attisdropped), 'no column') || ' | ' ||
			coalesce((SELECT string_agg(pg_get_triggerdef(oid), ';') FROM pg_trigger WHERE tgrelid = 'public.webhook_deliveries'::regclass AND NOT tgisinternal), 'no trigger') || ' | ' ||
			coalesce((SELECT md5(prosrc) FROM pg_proc WHERE proname = 'webhook_delivery_settle'), 'no function') || ' | ' ||
			coalesce((SELECT string_agg(polname || ':' || polpermissive::text || ':' || polcmd::text || ':' || pg_get_expr(polqual, polrelid), ';' ORDER BY polname)
			            FROM pg_policy WHERE polrelid = 'public.webhook_deliveries'::regclass), '') || ' | ' ||
			coalesce((SELECT pg_get_indexdef(indexrelid) FROM pg_index WHERE indexrelid = to_regclass('public.webhook_deliveries_tenant_settled_idx')), 'no index') || ' | ' ||
			has_table_privilege('tappa_app', 'public.webhook_deliveries', 'DELETE')::text`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s + " | " + opColumns(t, ctx, q, "tappa_app", "webhook_deliveries", "SELECT") + " / " + opColumns(t, ctx, q, "tappa_app", "webhook_deliveries", "UPDATE")
	}
	head := surface(tx)
	if strings.Contains(head, "no column") || strings.Contains(head, "no trigger") || strings.Contains(head, "no function") ||
		strings.Contains(head, "no index") || !strings.Contains(head, "webhook_deliveries_delete_settled:false:d") || !strings.Contains(head, " | true | ") {
		t.Fatalf("PREMISE at HEAD: %s", head)
	}
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		w := retentionWorld(t, ctx, sp)
		ids := retentionTaps(t, ctx, sp, w, w.kf, 2)
		retentionForge(t, ctx, sp, ids[0], "delivered", "3 days")
		opRunSection(t, ctx, sp, down59, "00059 Down")
		opRunSection(t, ctx, sp, down58, "00058 Down")
		if s := surface(sp); !strings.Contains(s, "no column | no trigger | no function") || !strings.Contains(s, "no index | false") ||
			strings.Contains(s, "delete_settled") || strings.Contains(s, "settled_at") {
			t.Errorf("after the Downs: %s", s)
		}
		opRunSection(t, ctx, sp, up58, "00058 Up again")
		opRunSection(t, ctx, sp, up59, "00059 Up again")
		if again := surface(sp); again != head {
			t.Errorf("after the Ups:\n %s\nwant HEAD's\n %s", again, head)
		}
		var settledIsNow, pendingIsNull bool
		if err := sp.QueryRow(ctx, `SELECT (SELECT settled_at = now() FROM webhook_deliveries WHERE id = $1),
		                                   (SELECT settled_at IS NULL FROM webhook_deliveries WHERE id = $2)`, ids[0], ids[1]).Scan(&settledIsNow, &pendingIsNull); err != nil {
			t.Fatal(err)
		}
		if !settledIsNow || !pendingIsNull {
			t.Errorf("the Up over existing rows: the delivered row dated at the migration=%v, the pending row NULL=%v", settledIsNow, pendingIsNull)
		}
		err := holdingNestedRun(t, ctx, sp, up58)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != sqlstatePrerequisiteState {
			t.Errorf("a second 00058 Up: %v, want 55000", err)
		}
	})
}

// TestWebhookRetention00058And00059_GiveUpAfterASecondBehindAnOpenWrite: each file's FIRST statement
// is SET LOCAL lock_timeout = '1s' (Up and Down), and behind an open delivery write -- a tap's
// transaction holds webhook_deliveries ROW EXCLUSIVE until it ends -- 00059's index build and 00058's
// Down give up with 55P03 in about a second instead of queueing every later tap behind their lock
// (deploy/README.md's rule for what a tap waits on: the feed triggers' 2 s). 00058's Up cannot be run
// at HEAD (its precondition refuses first, by design); its first statement is the same and its ALTER
// takes the same ACCESS EXCLUSIVE its Down's DROP POLICY does.
func TestWebhookRetention00058And00059_GiveUpAfterASecondBehindAnOpenWrite(t *testing.T) {
	for _, f := range []string{op00058File, op00059File} {
		up, down := opMigrationSections(t, f)
		for _, part := range []string{up, down} {
			body := strings.TrimSpace(part[strings.Index(part, "\n")+1:])
			for strings.HasPrefix(body, "--") {
				body = strings.TrimSpace(body[strings.Index(body, "\n")+1:])
			}
			if !strings.HasPrefix(body, "SET LOCAL lock_timeout = '1s';") {
				t.Errorf("%s: a section does not open with SET LOCAL lock_timeout = '1s'", f)
			}
		}
	}
	d := appDB(t)
	tenantID, _ := m11Tenant(t, d)
	ctx, tx := opTx(t)
	_, down58 := opMigrationSections(t, op00058File)
	up59, _ := opMigrationSections(t, op00059File)
	held, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- d.WithTenant(context.Background(), tenantID, func(ctx context.Context, htx pgx.Tx) error {
			// The tap's lock on the table without a delivery row: a DELETE that matches nothing still
			// takes ROW EXCLUSIVE on webhook_deliveries, as the feed trigger's INSERT does.
			if _, err := htx.Exec(ctx, `DELETE FROM webhook_deliveries WHERE tenant_id = $1`, tenantID); err != nil {
				return err
			}
			close(held)
			<-release
			return errors.New("rolled back on purpose")
		})
	}()
	<-held
	timer := time.AfterFunc(8*time.Second, func() { close(release) })
	defer func() {
		if timer.Stop() {
			close(release)
		}
		if err := <-holder; err == nil || err.Error() != "rolled back on purpose" {
			t.Errorf("the holder: %v", err)
		}
	}()
	for _, c := range []struct{ what, sql string }{
		{"00059's Up", strings.Replace(up59, "webhook_deliveries_tenant_settled_idx", "webhook_deliveries_tenant_settled_probe", 1)},
		{"00058's Down", down58},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_, err = sp.Conn().PgConn().Exec(ctx, c.sql).ReadAll()
		took := time.Since(start)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "55P03" || took > 3*time.Second {
			t.Errorf("%s behind an open delivery write: %v after %v, want 55P03 within about a second", c.what, err, took)
		}
	}
}
