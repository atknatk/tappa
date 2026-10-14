package db

// sitepunches_test.go -- M11 API-1c at the database: migration 00046 (site_punches, the projection
// half of holding_feed) against real Postgres. ADR 0027 §9 and "Test edilecekler → İzdüşüm ve
// paneller (API-1c)"; plan §5's API-1c acceptance core and the items it took over from API-1b.
//
// THE WORLD is holdingWorldTx's (holdingtap_test.go): KF and KM peers in H, C and D peers in H2, a
// KF venue with an active plaque at counter 100, a lost one and one on no wall, a KM employee.
// Every row is written as tappa_app in the EMPLOYER's context (spInsert), the way checkin writes
// it, and the projection is read as the owner (spOutcomes) -- or as tappa_app in the site's
// context where what the site may read is the subject.

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// spRow is one employer row naming a site, as a test writes it.
type spRow struct {
	employer, employee     uuid.UUID
	channel, verdict, kind string // kind: the direction ("" = none)
	sunValid               *bool
	ctr                    *int32
	matchedSid             string // "" = NULL
	policyContext          string // "" = NULL; JSON text otherwise
	occurredAt             string // "" = now(); an SQL timestamptz literal otherwise
	siteTenant, siteVenue  *uuid.UUID
	siteTag                *string
}

// spInsert writes r as tappa_app in r.employer's context, returning the row's id.
func spInsert(t *testing.T, ctx context.Context, tx pgx.Tx, r spRow) (uuid.UUID, error) {
	t.Helper()
	var id uuid.UUID
	err := holdingAppIn(t, ctx, tx, r.employer, func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `
			INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr,
			                          matched_sid, policy_context, site_tenant_id, site_location_id, site_tag_uid)
			VALUES ($1, $2, coalesce(NULLIF($3, '')::timestamptz, now()), $4, $5, NULLIF($6, ''), $7, $8,
			        NULLIF($9, ''), NULLIF($10, '')::jsonb, $11, $12, $13)
			RETURNING id`,
			r.employer, nilUUID(r.employee), r.occurredAt, r.verdict, r.channel, r.kind, r.sunValid, r.ctr,
			r.matchedSid, r.policyContext, r.siteTenant, r.siteVenue, r.siteTag).Scan(&id)
	})
	return id, err
}

// spMust is spInsert that fails the test on an error.
func spMust(t *testing.T, ctx context.Context, tx pgx.Tx, r spRow) uuid.UUID {
	t.Helper()
	id, err := spInsert(t, ctx, tx, r)
	if err != nil {
		t.Fatalf("the employer's row (%s %s): %v", r.channel, r.verdict, err)
	}
	return id
}

func nilUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

// spOutcomes is the projection's outcomes for one employer row, oldest first (owner read).
func spOutcomes(t *testing.T, ctx context.Context, q opQuerier, txid uuid.UUID) []string {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT outcome FROM site_punches WHERE transaction_id = $1 ORDER BY created_at, outcome`, txid)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// spCount counts site_punches rows of tenant matching where (an extra predicate, "" for none).
func spCount(t *testing.T, ctx context.Context, q opQuerier, tenant uuid.UUID, where string, args ...any) int64 {
	t.Helper()
	sql := `SELECT count(*) FROM site_punches WHERE tenant_id = $1`
	if where != "" {
		sql += " AND " + where
	}
	return opInt(t, ctx, q, sql, append([]any{tenant}, args...)...)
}

// spKFTap is a KM employee's row on KF's active plaque at KF's venue.
func spKFTap(w holdingWorld, channel, verdict, kind string, sun bool, ctr int32) spRow {
	r := spRow{employer: w.km, employee: w.kmEmployee, channel: channel, verdict: verdict, kind: kind,
		siteTenant: &w.kf, siteVenue: &w.kfVenue, siteTag: &w.kfTag}
	if channel != "manual" {
		r.sunValid = ptr(sun)
		if channel == "nfc" {
			r.ctr = ptr(ctr)
		}
	} else {
		r.siteTag = nil
	}
	return r
}

// ------------------------------------------------------------------ the catalogue --

// spColumns are site_punches' columns, in order (ADR 0027 §9's list, verbatim).
const spColumns = "tenant_id,id,transaction_id,employer_tenant_id,employer_api_code,employee_id,employee_ref,employee_code," +
	"employee_name,site_location_id,site_tag_uid,ctr,sun_valid,bucket_hour,occurred_at,channel,type,outcome,ctr_gap,created_at"

// spOutcomeSet is ADR 0027 §9's closed outcome set (K-1h).
var spOutcomeSet = []string{"approved", "counted", "employer_manual", "pending_review", "refused", "refused_tag_not_active", "rejected"}

// spEvidenceRE names what the projection must never carry (K-1d, K-1h, K-34's written-table pin):
// a coordinate, an address, a note, the policy context, the verdict, the matched rule, an e-mail.
var spEvidenceRE = regexp.MustCompile(`(?i)gps|lat$|lng$|ip|addr|note|policy|context|verdict|matched|sid$|mail|reason`)

// spCatalogFindings reads site_punches' shape from the catalogue: its columns exactly spColumns,
// none matching spEvidenceRE, and its outcome CHECK exactly spOutcomeSet.
func spCatalogFindings(t *testing.T, ctx context.Context, q opQuerier) []string {
	t.Helper()
	var out []string
	var cols string
	if err := q.QueryRow(ctx, `SELECT coalesce(string_agg(attname, ',' ORDER BY attnum), '') FROM pg_attribute
	                            WHERE attrelid = 'public.site_punches'::regclass AND attnum > 0 AND NOT attisdropped`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != spColumns {
		out = append(out, fmt.Sprintf("columns are (%s), want (%s)", cols, spColumns))
	}
	for _, c := range strings.Split(cols, ",") {
		if spEvidenceRE.MatchString(c) {
			out = append(out, "a column that names evidence or a reason: "+c)
		}
	}
	var def string
	if err := q.QueryRow(ctx, `SELECT coalesce(string_agg(pg_get_constraintdef(oid), ' | '), '') FROM pg_constraint
	                            WHERE conrelid = 'public.site_punches'::regclass AND contype = 'c' AND pg_get_constraintdef(oid) LIKE '%outcome%'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	got := regexp.MustCompile(`'([a-z_]+)'::text`).FindAllStringSubmatch(def, -1)
	var set []string
	for _, m := range got {
		set = append(set, m[1])
	}
	slices.Sort(set)
	if !slices.Equal(set, spOutcomeSet) {
		out = append(out, fmt.Sprintf("the outcome CHECK allows %v, want exactly %v", set, spOutcomeSet))
	}
	return out
}

// TestSitePunches_CatalogCarriesNoEvidenceColumns is ADR 0027 §3's "yazılan tabloların sütun pini"
// for the first written table and §9's closed outcome set: site_punches' columns are EXACTLY the
// ADR's list (a new column is red), none names a coordinate, an address, a note, the policy
// context, the verdict, the matched rule or an e-mail, and the outcome CHECK is the closed set of
// seven. CONTROLS (savepoints): a `note` column, a `verdict` column and an eighth outcome are each
// flagged.
func TestSitePunches_CatalogCarriesNoEvidenceColumns(t *testing.T) {
	ctx, tx := opTx(t)
	for _, f := range spCatalogFindings(t, ctx, tx) {
		t.Error(f)
	}
	for _, c := range []struct{ name, ddl, want string }{
		{"a note column", `ALTER TABLE site_punches ADD COLUMN note text`, "note"},
		{"a verdict column", `ALTER TABLE site_punches ADD COLUMN verdict text`, "verdict"},
		{"an eighth outcome", `ALTER TABLE site_punches DROP CONSTRAINT site_punches_outcome_check,
		     ADD CONSTRAINT site_punches_outcome_check CHECK (outcome IN ('counted', 'pending_review', 'approved', 'rejected',
		     'refused_tag_not_active', 'refused', 'employer_manual', 'refused_deactivated'))`, "outcome CHECK"},
	} {
		holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, c.ddl); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if got := spCatalogFindings(t, ctx, sp); !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, c.want) }) {
				t.Errorf("CONTROL %s: not flagged (%v)", c.name, got)
			}
		})
	}
}

// TestSitePunches_AppCannotReadSunValid is K-1h's "nedeni değil" at the grant (ADR 0027 §9, 11th
// round: "refused and sun_valid" would say "the signature was good"): as tappa_app in KF's
// context, every other column of the projection reads, sun_valid alone is 42501 (and so is
// SELECT *); tappa_app inserts, updates and deletes nothing there.
func TestSitePunches_AppCannotReadSunValid(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	spMust(t, ctx, tx, spKFTap(w, "nfc", "reject", "", true, 100))
	readable := strings.Replace(spColumns, ",sun_valid", "", 1)
	if err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
		var n int64
		return sp.QueryRow(ctx, `SELECT count(*) FROM (SELECT `+readable+` FROM site_punches) AS s`).Scan(&n)
	}); err != nil {
		t.Errorf("KF reads every column but sun_valid: %v", err)
	}
	for what, sql := range map[string]string{
		"sun_valid":    `SELECT sun_valid FROM site_punches`,
		"SELECT *":     `SELECT * FROM site_punches`,
		"in a filter":  `SELECT count(*) FROM site_punches WHERE sun_valid`,
		"an INSERT":    `INSERT INTO site_punches (tenant_id, transaction_id, employer_tenant_id, bucket_hour, occurred_at, channel, outcome) VALUES ($1, gen_random_uuid(), gen_random_uuid(), date_trunc('hour', now(), 'UTC'), now(), 'nfc', 'counted')`,
		"an UPDATE":    `UPDATE site_punches SET outcome = 'counted' WHERE tenant_id = $1`,
		"a DELETE":     `DELETE FROM site_punches WHERE tenant_id = $1`,
		"a TRUNCATE":   `TRUNCATE site_punches`,
		"the trigger":  `SELECT public.holding_feed()`,
		"a sun filter": `SELECT count(*) FROM site_punches WHERE sun_valid IS TRUE AND tenant_id = $1`,
	} {
		err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
			var args []any
			if strings.Contains(sql, "$1") {
				args = []any{w.kf}
			}
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
		if code, _ := opCode(err); code != sqlstateInsufficientPrivi {
			t.Errorf("tappa_app, %s: %v, want 42501", what, err)
		}
	}
}

// TestSitePunches_AppendOnly: site_punches takes no UPDATE, DELETE or TRUNCATE from anybody --
// the owner (a superuser, past every grant) is stopped by the row and statement triggers
// (restrict_violation, CLAUDE.md §4.3's treatment).
func TestSitePunches_AppendOnly(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	spMust(t, ctx, tx, spKFTap(w, "nfc", "ok", "in", true, 100))
	for _, sql := range []string{
		`UPDATE site_punches SET outcome = 'rejected' WHERE tenant_id = $1`,
		`DELETE FROM site_punches WHERE tenant_id = $1`,
		`TRUNCATE site_punches`,
	} {
		var args []any
		if strings.Contains(sql, "$1") {
			args = []any{w.kf}
		}
		opWant(t, opTry(t, ctx, tx, sql, args...), sqlstateRestrictViolation, "the owner: "+sql)
	}
}

// ------------------------------------------------------------------ the mapping --

// spCell is one verdict x channel x review cell of ADR 0027 §9's mapping and what the site gets.
type spCell struct {
	name    string
	row     func(w holdingWorld, employee uuid.UUID) spRow
	review  string   // "" none, else approved / rejected
	want    []string // the projection's outcomes for the row, oldest first; nil = not written
	written bool     // the employer's row is written (always, §4.6)
}

// spCells walks every cell the schema admits: nfc and qr x ok / flag / reject (two rules) /
// ignored, a flag approved and a flag rejected on each channel, and manual x ok / flag / reject /
// ignored and a manual flag decided. Each refusal cell has an employee of its own, so no cell's
// dedup reaches another's.
func spCells() []spCell {
	tap := func(channel, verdict, kind string, sun bool, sid string) func(w holdingWorld, e uuid.UUID) spRow {
		return func(w holdingWorld, e uuid.UUID) spRow {
			r := spKFTap(w, channel, verdict, kind, sun, 100)
			r.employee, r.matchedSid = e, sid
			return r
		}
	}
	var cells []spCell
	for _, ch := range []string{"nfc", "qr"} {
		sun := ch == "nfc"
		cells = append(cells,
			spCell{ch + "/ok", tap(ch, "ok", "in", sun, "base:ip-or-gps"), "", []string{"counted"}, true},
			spCell{ch + "/flag", tap(ch, "flag", "in", sun, "base:no-evidence-review"), "", []string{"pending_review"}, true},
			spCell{ch + "/flag/approved", tap(ch, "flag", "out", sun, "base:no-evidence-review"), "approved", []string{"pending_review", "approved"}, true},
			spCell{ch + "/flag/rejected", tap(ch, "flag", "in", sun, "base:no-evidence-review"), "rejected", []string{"pending_review", "rejected"}, true},
			spCell{ch + "/reject/tag-not-active", tap(ch, "reject", "", false, "sys:tag-not-active"), "", []string{"refused_tag_not_active"}, true},
			spCell{ch + "/reject/sun-invalid", tap(ch, "reject", "", false, "sys:sun-invalid"), "", []string{"refused"}, true},
			spCell{ch + "/reject/deactivated", tap(ch, "reject", "", sun, "sys:employee-deactivated"), "", []string{"refused"}, true},
			spCell{ch + "/reject/no-rule", tap(ch, "reject", "", false, ""), "", []string{"refused"}, true},
			spCell{ch + "/ignored", tap(ch, "ignored", "", sun, "sys:person-debounce"), "", nil, true},
		)
	}
	manual := func(verdict, kind string) func(w holdingWorld, e uuid.UUID) spRow {
		return func(w holdingWorld, e uuid.UUID) spRow {
			r := spKFTap(w, "manual", verdict, kind, false, 0)
			r.employee = e
			return r
		}
	}
	cells = append(cells,
		spCell{"manual/ok", manual("ok", "in"), "", []string{"employer_manual"}, true},
		spCell{"manual/flag", manual("flag", "out"), "", []string{"employer_manual"}, true},
		spCell{"manual/flag/approved", manual("flag", "in"), "approved", []string{"approved", "employer_manual"}, true},
		spCell{"manual/reject", manual("reject", ""), "", []string{"employer_manual"}, true},
		spCell{"manual/ignored", manual("ignored", ""), "", nil, true},
	)
	return cells
}

// spMappingFindings writes every cell (fresh employees, KM's context) on q and reports each one
// whose employer row was not written or whose projection is not the cell's.
func spMappingFindings(t *testing.T, ctx context.Context, q pgx.Tx, w holdingWorld, reviewer uuid.UUID) []string {
	t.Helper()
	var out []string
	for _, c := range spCells() {
		e := holdingNewEmployee(t, ctx, q, w.km, "active")
		id, err := spInsert(t, ctx, q, c.row(w, e))
		if err != nil {
			out = append(out, fmt.Sprintf("%s: the EMPLOYER's row was not written (§4.6): %v", c.name, err))
			continue
		}
		if c.review != "" {
			if err := holdingAppIn(t, ctx, q, w.km, func(sp pgx.Tx) error {
				_, err := sp.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, $4)`,
					w.km, id, reviewer, c.review)
				return err
			}); err != nil {
				out = append(out, fmt.Sprintf("%s: the EMPLOYER's review was not written (§4.6): %v", c.name, err))
				continue
			}
		}
		got := spOutcomes(t, ctx, q, id)
		want := slices.Clone(c.want)
		slices.Sort(want)
		gotSorted := slices.Clone(got)
		slices.Sort(gotSorted)
		if !slices.Equal(gotSorted, want) {
			out = append(out, fmt.Sprintf("%s: the site got %v, want %v", c.name, got, c.want))
		}
	}
	return out
}

// TestSitePunches_OutcomeMappingIsTotal is ADR 0027 §9's "eşleme — tam ve toplam" and §12.6: every
// verdict x channel x review cell the schema admits is written at the employer (§4.6) and gives the
// site exactly its outcome -- ok counted, flag pending_review then approved / rejected, a refusal
// by sys:tag-not-active refused_tag_not_active, every other refusal (the deactivated employee's
// included -- K-1h: the outcome, never the reason) refused, manual employer_manual -- or, for
// 'ignored', NOTHING, by an explicit branch and with no error.
// MUTANTS (holding_feed's body edited in a savepoint, the owner and grants kept):
//   - the 'ignored' branch removed and an unmatched cell made to raise (a CASE that "covers the
//     rest" by failing) -> the ignored cells' EMPLOYER rows are not written: red (§4.6);
//   - the 'ignored' branch alone removed -> a manual 'ignored' reaches the site as employer_manual;
//   - the sys:tag-not-active arm removed -> the lost plaque's cell reads 'refused'.
func TestSitePunches_OutcomeMappingIsTotal(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	reviewer := holdingNewAdmin(t, ctx, tx, w.km)
	for _, f := range spMappingFindings(t, ctx, tx, w, reviewer) {
		t.Error(f)
	}
	body := spFeedBody(t, ctx, tx)
	ignoredArm := "            WHEN NEW.verdict = 'ignored' THEN NULL\n"
	lostArm := "            WHEN NEW.verdict = 'reject' AND NEW.matched_sid IS NOT DISTINCT FROM 'sys:tag-not-active' THEN 'refused_tag_not_active'\n"
	for _, m := range []struct{ name, from, to, want string }{
		{"ignored made to raise", ignoredArm, "            WHEN NEW.verdict NOT IN ('ok', 'flag', 'reject') THEN (1 / (pg_catalog.random() * 0)::integer)::text\n", "EMPLOYER's row was not written"},
		{"the ignored branch removed", ignoredArm, "", "manual/ignored: the site got [employer_manual]"},
		{"the lost plaque's arm removed", lostArm, "", "reject/tag-not-active: the site got [refused]"},
	} {
		t.Run("MUTANT "+m.name, func(t *testing.T) {
			if !strings.Contains(body, m.from) {
				t.Fatalf("the mutant's anchor is not in holding_feed's body: %q", m.from)
			}
			holdingInSavepoint(t, tx, func(sp pgx.Tx) {
				if _, err := sp.Exec(ctx, strings.Replace(body, m.from, m.to, 1)); err != nil {
					t.Fatalf("install the mutant: %v", err)
				}
				got := spMappingFindings(t, ctx, sp, w, reviewer)
				if !slices.ContainsFunc(got, func(f string) bool { return strings.Contains(f, m.want) }) {
					t.Errorf("the mutant stayed green (findings %v)", got)
				}
			})
		})
	}
}

// spFeedBody is holding_feed's CREATE OR REPLACE text as the catalogue prints it.
func spFeedBody(t *testing.T, ctx context.Context, q opQuerier) string {
	t.Helper()
	var def string
	if err := q.QueryRow(ctx, `SELECT pg_get_functiondef('public.holding_feed()'::regprocedure)`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	return def
}

// --------------------------------------------------------------- the refusal dedup --

// TestSitePunches_ReplayURLCollapsesAtTheSite (ADR 0027 §9; plan §5 API-1c "aynı replay URL'si N
// kez → KF'de 1 satır, KM'de N red"): one employee replays one URL five times -- five refusals at
// KM, ONE row at KF. And TestSitePunches_DifferentEmployeesReplaysStaySeparate's half: a second
// employee replaying the same URL is a second row.
func TestSitePunches_ReplayURLCollapsesAtTheSite(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	var ids []uuid.UUID
	for range 5 {
		r := spKFTap(w, "nfc", "reject", "", false, 100)
		r.matchedSid = "sys:sun-invalid"
		ids = append(ids, spMust(t, ctx, tx, r))
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM transactions WHERE id = ANY($1)`, ids); n != 5 {
		t.Errorf("the employer holds %d refusals, want 5 (§4.6: every one written)", n)
	}
	if n := spCount(t, ctx, tx, w.kf, "site_tag_uid = $2", w.kfTag); n != 1 {
		t.Errorf("KF holds %d rows for one replayed URL, want 1", n)
	}
}

// TestSitePunches_DifferentEmployeesReplaysStaySeparate (plan §5 API-1c: "farklı çalışanların
// replay'i ayrı"): two employees, the same replayed URL, three times each -- two rows at KF, six at
// KM. The employee is in both dedup keys.
func TestSitePunches_DifferentEmployeesReplaysStaySeparate(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	second := holdingNewEmployee(t, ctx, tx, w.km, "active")
	for _, e := range []uuid.UUID{w.kmEmployee, second} {
		for range 3 {
			r := spKFTap(w, "nfc", "reject", "", false, 100)
			r.employee = e
			spMust(t, ctx, tx, r)
		}
	}
	if n := spCount(t, ctx, tx, w.kf, ""); n != 2 {
		t.Errorf("KF holds %d rows, want 2 (one per employee)", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND site_tenant_id = $2`, w.km, w.kf); n != 6 {
		t.Errorf("KM holds %d refusals, want 6", n)
	}
}

// TestSitePunches_ForgedCountersCollapsePerHourBucket (ADR 0027 §9, 11th round "200 sahte URL → 1
// satır"): 200 forged URLs for one plaque and one employee, their counters mixed below and above
// the plaque's last_ctr (the row's ctr is the attacker's), written in one statement -- 200 refusals
// at KM, ONE row at KF for the plaque + employee + hour. MUTANT (the hourly index's key widened by
// ctr, in a savepoint): 200 rows at KF -- red.
func TestSitePunches_ForgedCountersCollapsePerHourBucket(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	forge := func(q pgx.Tx) {
		t.Helper()
		if err := holdingAppIn(t, ctx, q, w.km, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, `
				INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, sun_valid, ctr, matched_sid,
				                          site_tenant_id, site_location_id, site_tag_uid)
				SELECT $1, $2, now(), 'reject', 'nfc', false, CASE WHEN g % 2 = 0 THEN 100 - g / 2 ELSE 100 + g END,
				       'sys:sun-invalid', $3, $4, $5
				  FROM generate_series(1, 200) AS g`, w.km, w.kmEmployee, w.kf, w.kfVenue, w.kfTag)
			return err
		}); err != nil {
			t.Fatalf("200 forged URLs: %v", err)
		}
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		forge(sp)
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND site_tag_uid = $2`, w.km, w.kfTag); n != 200 {
			t.Errorf("KM holds %d refusals, want 200", n)
		}
		if n := spCount(t, ctx, sp, w.kf, ""); n != 1 {
			t.Errorf("KF holds %d rows for 200 forged URLs, want 1", n)
		}
	})
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `DROP INDEX site_punches_other_refusal_hourly;
			CREATE UNIQUE INDEX site_punches_other_refusal_hourly ON site_punches (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour, ctr)
			    NULLS NOT DISTINCT WHERE sun_valid IS NOT TRUE AND outcome LIKE 'refused%'`); err != nil {
			t.Fatal(err)
		}
		body := spFeedBody(t, ctx, sp)
		mutant := strings.Replace(body, "ON CONFLICT (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour)",
			"ON CONFLICT (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour, ctr)", 1)
		if mutant == body {
			t.Fatal("the hourly target is not in holding_feed's body")
		}
		if _, err := sp.Exec(ctx, mutant); err != nil {
			t.Fatal(err)
		}
		forge(sp)
		if n := spCount(t, ctx, sp, w.kf, ""); n != 200 {
			t.Errorf("MUTANT (ctr in the hourly key): KF holds %d rows, want 200 -- the measure cannot tell", n)
		}
	})
}

// TestSitePunches_RealRefusalsAreKeptOneEach (ADR 0027 §9; plan §5 API-1c "deaktive çalışanın 3
// gerçek dokunuşu (sun_valid) → 3 satır"): a deactivated employee's three real taps -- the counter
// advanced each time (sun_valid, 101, 102, 103) and each refused -- are three rows at KF; the same
// (plaque, counter, employee) again adds nothing.
func TestSitePunches_RealRefusalsAreKeptOneEach(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	gone := holdingNewEmployee(t, ctx, tx, w.km, "deactivated")
	for _, ctr := range []int32{101, 102, 103, 101} {
		r := spKFTap(w, "nfc", "reject", "", true, ctr)
		r.employee, r.matchedSid = gone, "sys:employee-deactivated"
		spMust(t, ctx, tx, r)
	}
	if n := spCount(t, ctx, tx, w.kf, "employee_id = $2", gone); n != 3 {
		t.Errorf("KF holds %d rows for three real refusals (and a repeat), want 3", n)
	}
}

// TestSitePunches_DeactivatedEmployeeRefusalIsOnlyRefused (plan §5 API-1c: "deaktive KM
// çalışanının KF plaketindeki reddi KF'de yalnız refused"): the refusal of a deactivated KM
// employee's tap is 'refused' at KF, and nothing KF can read about it says why -- no column of
// KF's row, read as tappa_app, carries "deactivated" or the rule's name.
func TestSitePunches_DeactivatedEmployeeRefusalIsOnlyRefused(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	gone := holdingNewEmployee(t, ctx, tx, w.km, "deactivated")
	r := spKFTap(w, "nfc", "reject", "", true, 101)
	r.employee, r.matchedSid = gone, "sys:employee-deactivated"
	id := spMust(t, ctx, tx, r)
	if got := spOutcomes(t, ctx, tx, id); !slices.Equal(got, []string{"refused"}) {
		t.Errorf("KF got %v, want [refused]", got)
	}
	readable := strings.Replace(spColumns, ",sun_valid", "", 1)
	if err := holdingAppIn(t, ctx, tx, w.kf, func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, `SELECT `+readable+` FROM site_punches WHERE transaction_id = $1`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				return err
			}
			for i, v := range vals {
				s := strings.ToLower(fmt.Sprint(v))
				if strings.Contains(s, "deactivat") || strings.Contains(s, "sys:") {
					t.Errorf("KF's column %s carries the reason: %v", rows.FieldDescriptions()[i].Name, v)
				}
			}
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
}

// TestSitePunches_LostPlaqueRepeatsGiveOneRowAndOneAlert's DATABASE half (ADR 0027 §9; plan §5
// API-1c "kayıp plaketin aynı URL'si N kez → 1 refused_tag_not_active"): one URL on KF's LOST plaque
// tapped five times by one KM employee (the advance is never tried: sun_valid false) is five
// refusals at KM and ONE refused_tag_not_active row at KF. The alert's half is
// TestSitePanel_LostPlaqueAlertAtTheSite (internal/domain/ledger).
func TestSitePunches_LostPlaqueRepeatsGiveOneRowAndOneAlert(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	for range 5 {
		r := spKFTap(w, "nfc", "reject", "", false, 500)
		r.siteTag, r.matchedSid = &w.kfLost, "sys:tag-not-active"
		spMust(t, ctx, tx, r)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM transactions WHERE tenant_id = $1 AND site_tag_uid = $2`, w.km, w.kfLost); n != 5 {
		t.Errorf("KM holds %d refusals, want 5", n)
	}
	if n := spCount(t, ctx, tx, w.kf, "site_tag_uid = $2 AND outcome = 'refused_tag_not_active'", w.kfLost); n != 1 {
		t.Errorf("KF holds %d lost-plaque rows, want 1", n)
	}
}

// TestSitePunches_LostPlaqueAlertCannotBeSuppressed (ADR 0027 §9: "outcome anahtarda"; plan §5
// API-1c "aynı saatte önce sahte refused sonra kayıp plaket → ikisi de KF'de"): in one hour, a
// forged 'refused' on KF's lost plaque arrives FIRST, then the lost-plaque refusal -- both are at
// KF. MUTANT (outcome taken out of the hourly key): the forged row swallows the alert -- red.
func TestSitePunches_LostPlaqueAlertCannotBeSuppressed(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	both := func(q pgx.Tx) int64 {
		t.Helper()
		forged := spKFTap(w, "nfc", "reject", "", false, 500)
		forged.siteTag, forged.matchedSid = &w.kfLost, "sys:sun-invalid"
		spMust(t, ctx, q, forged)
		lost := forged
		lost.matchedSid = "sys:tag-not-active"
		spMust(t, ctx, q, lost)
		return spCount(t, ctx, q, w.kf, "site_tag_uid = $2 AND outcome = 'refused_tag_not_active'", w.kfLost)
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		if n := both(sp); n != 1 {
			t.Errorf("a forged refusal first, then the lost plaque: %d lost-plaque rows at KF, want 1", n)
		}
		if n := spCount(t, ctx, sp, w.kf, "site_tag_uid = $2", w.kfLost); n != 2 {
			t.Errorf("KF holds %d rows for the lost plaque, want 2 (the forged one and the alert)", n)
		}
	})
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `DROP INDEX site_punches_other_refusal_hourly;
			CREATE UNIQUE INDEX site_punches_other_refusal_hourly ON site_punches (tenant_id, site_tag_uid, employee_id, bucket_hour)
			    NULLS NOT DISTINCT WHERE sun_valid IS NOT TRUE AND outcome LIKE 'refused%'`); err != nil {
			t.Fatal(err)
		}
		// The trigger's ON CONFLICT names the real index's columns; the mutant's key infers no
		// arbiter for them, so the mutant's trigger is given the narrower target too.
		body := spFeedBody(t, ctx, sp)
		mutant := strings.Replace(body, "ON CONFLICT (tenant_id, site_tag_uid, employee_id, outcome, bucket_hour)",
			"ON CONFLICT (tenant_id, site_tag_uid, employee_id, bucket_hour)", 1)
		if mutant == body {
			t.Fatal("the hourly target is not in holding_feed's body")
		}
		if _, err := sp.Exec(ctx, mutant); err != nil {
			t.Fatal(err)
		}
		if n := both(sp); n != 0 {
			t.Errorf("MUTANT (outcome out of the key): %d lost-plaque rows, want 0 -- the measure cannot tell", n)
		}
	})
}

// TestSitePunches_BucketIsServerClockAndZoneFree (ADR 0027 §9; plan §5 API-1c "UTC ve Europe/Malta
// oturumu aynı kova"): the same refusal written from a UTC, a Europe/Malta and an Asia/Kolkata
// session -- each declaring a different occurred_at (2001, 2099, now) -- lands in ONE row: the
// bucket is date_trunc('hour', now(), 'UTC'), the server's hour, whatever the session's zone or the
// client's claim. Kolkata is there because a whole-hour zone cannot tell the three-argument form
// from the two-argument one (measured: 13:30+05:30 vs 14:00+05:30 for 08:xx UTC); MUTANT (the
// two-argument form) -> the Kolkata row is a second row: red.
func TestSitePunches_BucketIsServerClockAndZoneFree(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	write := func(q pgx.Tx) []time.Time {
		t.Helper()
		for _, z := range []struct{ zone, declared string }{
			{"UTC", "2001-01-01 00:00:00+00"}, {"Europe/Malta", "2099-12-31 23:59:00+00"}, {"Asia/Kolkata", ""},
		} {
			if _, err := q.Exec(ctx, `SELECT set_config('TimeZone', $1, true)`, z.zone); err != nil {
				t.Fatal(err)
			}
			r := spKFTap(w, "nfc", "reject", "", false, 100)
			r.occurredAt = z.declared
			spMust(t, ctx, q, r)
		}
		if _, err := q.Exec(ctx, `SELECT set_config('TimeZone', 'UTC', true)`); err != nil {
			t.Fatal(err)
		}
		rows, err := q.Query(ctx, `SELECT bucket_hour FROM site_punches WHERE tenant_id = $1 ORDER BY 1`, w.kf)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	holdingInSavepoint(t, tx, func(sp pgx.Tx) {
		var want time.Time
		if err := sp.QueryRow(ctx, `SELECT date_trunc('hour', now(), 'UTC')`).Scan(&want); err != nil {
			t.Fatal(err)
		}
		got := write(sp)
		if len(got) != 1 || !got[0].Equal(want) {
			t.Errorf("buckets %v, want one: the server's hour %v", got, want)
		}
	})
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		body := spFeedBody(t, ctx, sp)
		mutant := strings.ReplaceAll(body, "pg_catalog.date_trunc('hour', pg_catalog.now(), 'UTC')", "pg_catalog.date_trunc('hour', pg_catalog.now())")
		// The CHECK holds a bucket to a UTC hour, so the mutant's half-hour bucket is checked off too.
		if _, err := sp.Exec(ctx, mutant); err != nil {
			t.Fatal(err)
		}
		if err := opTry(t, ctx, sp, `ALTER TABLE site_punches DROP CONSTRAINT site_punches_bucket_is_an_hour`); err != nil {
			t.Fatal(err)
		}
		if got := write(sp); len(got) < 2 {
			t.Errorf("MUTANT (two-argument date_trunc): %d bucket(s), want 2 -- the measure cannot tell", len(got))
		}
	})
}

// TestSitePunches_NullHolesAreDeduplicated (ADR 0027 §9: NULLS NOT DISTINCT; plan §5 API-1c "NULL
// sun_valid/employee_id satırları da tekilleşir"): a refusal with NO employee (a session-less
// refusal is a legal row -- 00005) and a NULL sun_valid, three times, and the same with sun_valid
// false, is ONE row at KF; and a real refusal (sun_valid) with no employee, twice at one counter,
// is one row too.
func TestSitePunches_NullHolesAreDeduplicated(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	for _, sun := range []*bool{nil, nil, nil, ptr(false)} {
		r := spKFTap(w, "nfc", "reject", "", false, 100)
		r.employee, r.sunValid = uuid.Nil, sun
		spMust(t, ctx, tx, r)
	}
	if n := spCount(t, ctx, tx, w.kf, "employee_id IS NULL AND sun_valid IS NOT TRUE"); n != 1 {
		t.Errorf("NULL employee, NULL / false sun_valid: %d rows, want 1", n)
	}
	for range 2 {
		r := spKFTap(w, "nfc", "reject", "", true, 100)
		r.employee = uuid.Nil
		spMust(t, ctx, tx, r)
	}
	if n := spCount(t, ctx, tx, w.kf, "employee_id IS NULL AND sun_valid IS TRUE"); n != 1 {
		t.Errorf("NULL employee, a real refusal twice: %d rows, want 1", n)
	}
}

// ----------------------------------------------------------- K-1i, ctr_gap from NEW --

// TestSitePunches_CounterGapComesFromTheEmployersRow is K-1i's site half (ADR 0027 §9; plan §5
// API-1c, taken over from API-1b): the projection's ctr_gap is the employer row's tap:ctrGap --
// 49 when the context says 49 beside other keys -- and NULL when the key is missing, not a number,
// negative, fractional or past a 24-bit counter; none of those raises (§4.6: the employer's row is
// written each time). The definer still cannot SELECT policy_context (K-34's pin holds: the
// trigger reads NEW).
func TestSitePunches_CounterGapComesFromTheEmployersRow(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	for _, c := range []struct {
		context string
		want    *int32
	}{
		{`{"tap:ctrGap": 49, "tap:ipMatch": true, "tap:gpsDistanceM": 12.5}`, ptr(int32(49))},
		{`{"tap:ctrGap": 0}`, ptr(int32(0))},
		{`{"tap:ipMatch": true}`, nil},
		{`{"tap:ctrGap": "49"}`, nil},
		{`{"tap:ctrGap": -1}`, nil},
		{`{"tap:ctrGap": 4.5}`, nil},
		{`{"tap:ctrGap": 16777216}`, nil},
		{`{"tap:ctrGap": {"n": 1}}`, nil},
		{``, nil},
	} {
		r := spKFTap(w, "nfc", "ok", "in", true, 100)
		r.policyContext = c.context
		id := spMust(t, ctx, tx, r)
		var gap *int32
		if err := tx.QueryRow(ctx, `SELECT ctr_gap FROM site_punches WHERE transaction_id = $1`, id).Scan(&gap); err != nil {
			t.Fatalf("%s: %v", c.context, err)
		}
		if (gap == nil) != (c.want == nil) || (gap != nil && *gap != *c.want) {
			t.Errorf("policy_context %s: ctr_gap %v, want %v", c.context, holdingIntStr(gap), holdingIntStr(c.want))
		}
	}
	if opInt(t, ctx, tx, `SELECT has_column_privilege('tappa_holdingdefiner', 'public.transactions', 'policy_context', 'SELECT')::int`) != 0 {
		t.Error("tappa_holdingdefiner may SELECT transactions.policy_context (K-34)")
	}
}

func holdingIntStr(p *int32) string {
	if p == nil {
		return "NULL"
	}
	return fmt.Sprint(*p)
}

// spPolicyContextRE finds every mention of policy_context in a body; spCtrGapReadRE the one read
// K-1i allows: the tap:ctrGap key, by the -> operator.
var (
	spPolicyContextRE = regexp.MustCompile(`(?i)policy_context`)
	spCtrGapReadRE    = regexp.MustCompile(`(?i)NEW\.policy_context\s*->\s*'tap:ctrGap'`)
)

// spPolicyContextFindings is the body scan K-1i's devir asks for (plan §5 API-1c): in holding_feed
// (comments stripped) policy_context appears exactly as often as `NEW.policy_context ->
// 'tap:ctrGap'` does -- once -- so the body reads that one key of the context and nothing else of
// it (K-34's column pin cannot see a read of NEW).
func spPolicyContextFindings(body string) []string {
	body = holdingStripComments(body)
	all, gap := len(spPolicyContextRE.FindAllStringIndex(body, -1)), len(spCtrGapReadRE.FindAllStringIndex(body, -1))
	switch {
	case gap != 1:
		return []string{fmt.Sprintf("holding_feed reads tap:ctrGap %d times, want exactly once", gap)}
	case all != gap:
		return []string{fmt.Sprintf("holding_feed names policy_context %d times and reads tap:ctrGap %d -- it reads more of the context than K-1i allows", all, gap)}
	}
	return nil
}

// TestHoldingFeed_ReadsOnlyTheCounterGapOfThePolicyContext pins the scan on the catalogue's
// holding_feed, with MUTANTS (in a savepoint): a body that also reads tap:gpsDistanceM, one that
// copies the whole context, and one that reads the gap with ->> -- each flagged.
func TestHoldingFeed_ReadsOnlyTheCounterGapOfThePolicyContext(t *testing.T) {
	ctx, tx := opTx(t)
	body := spFeedBody(t, ctx, tx)
	for _, f := range spPolicyContextFindings(body) {
		t.Error(f)
	}
	anchor := "v_gap_json := NEW.policy_context -> 'tap:ctrGap';"
	if !strings.Contains(body, anchor) {
		t.Fatalf("the read is not where the scan expects it: %q", anchor)
	}
	for name, to := range map[string]string{
		"another key":       anchor + " v_gap_json := NEW.policy_context -> 'tap:gpsDistanceM';",
		"the whole context": anchor + " v_gap_json := NEW.policy_context;",
		"the text operator": "v_gap_json := to_jsonb(NEW.policy_context ->> 'tap:ctrGap');",
	} {
		mutant := strings.Replace(body, anchor, to, 1)
		if got := spPolicyContextFindings(mutant); len(got) == 0 {
			t.Errorf("MUTANT %s: not flagged", name)
		}
		holdingInSavepoint(t, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, mutant); err != nil {
				t.Errorf("MUTANT %s does not compile, so it proves nothing: %v", name, err)
			}
		})
	}
}

// ----------------------------------------------------------- who sees the projection --

// TestSitePunches_OutsiderSeesNothing is K-33 and RLS on the projection (plan §5 API-1c "C
// izdüşümden 0 satır"; API-1b's devir "KM manuel kaydı + C lokasyonu → C'nin site_punches'ı 0"):
// KM's manual record naming C's venue is refused at the gate and C's projection stays EMPTY; KF's
// rows are read by KF and by nobody else -- C's context, KM's (the employer's own projection is not
// a door to the site's) and no context each read 0 of them with NO tenant predicate (the RLS
// isolation probe; a filtered probe would be green with RLS off). CONTROL: KF's context reads them.
func TestSitePunches_OutsiderSeesNothing(t *testing.T) {
	ctx, tx := opTx(t)
	w := holdingWorldTx(t, ctx, tx, "")
	_, err := spInsert(t, ctx, tx, spRow{employer: w.km, employee: w.kmEmployee, channel: "manual", verdict: "ok", kind: "in",
		siteTenant: &w.c, siteVenue: &w.cVenue})
	holdingWantGate(t, err, "KM's manual record at C's venue")
	if n := spCount(t, ctx, tx, w.c, ""); n != 0 {
		t.Errorf("C's site_punches holds %d rows, want 0", n)
	}
	id := spMust(t, ctx, tx, spKFTap(w, "nfc", "ok", "in", true, 100))
	visible := func(tenant uuid.UUID) int64 {
		var n int64
		if err := holdingAppIn(t, ctx, tx, tenant, func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, `SELECT count(*) FROM site_punches WHERE transaction_id = $1`, id).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for who, tenant := range map[string]uuid.UUID{"C": w.c, "D": w.d, "KM (the employer)": w.km, "no context": uuid.Nil} {
		if n := visible(tenant); n != 0 {
			t.Errorf("%s reads %d of KF's projection rows, want 0", who, n)
		}
	}
	if n := visible(w.kf); n != 1 {
		t.Errorf("CONTROL: KF reads %d of its projection rows, want 1", n)
	}
}

// TestHoldingFeed_NameOrderIsFirst is ADR 0027 §8 / K-31 (and ADR 0028 §4's "ad sırasında ilk"):
// on transactions and on transaction_reviews, holding_feed's trigger is the FIRST of the table's
// AFTER INSERT row triggers in name order (C collation -- the order PostgreSQL fires them in).
// CONTROL: a trigger named to sort before it is flagged.
func TestHoldingFeed_NameOrderIsFirst(t *testing.T) {
	ctx, tx := opTx(t)
	first := func(q opQuerier, table string) string {
		t.Helper()
		var name string
		// tgtype bits: 1 ROW, 2 BEFORE, 4 INSERT; an AFTER INSERT ROW trigger has 1 and 4, not 2.
		if err := q.QueryRow(ctx, `SELECT coalesce((SELECT tgname FROM pg_trigger
		                             WHERE tgrelid = ('public.' || $1)::regclass AND NOT tgisinternal
		                               AND (tgtype & 1) <> 0 AND (tgtype & 2) = 0 AND (tgtype & 4) <> 0
		                             ORDER BY tgname COLLATE "C" LIMIT 1), '')`, table).Scan(&name); err != nil {
			t.Fatal(err)
		}
		return name
	}
	for table, want := range map[string]string{"transactions": "transactions_holding_feed", "transaction_reviews": "transaction_reviews_holding_feed"} {
		if got := first(tx, table); got != want {
			t.Errorf("%s: the first AFTER INSERT trigger is %q, want %q", table, got, want)
		}
	}
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		if _, err := sp.Exec(ctx, `CREATE TRIGGER transactions_feed AFTER INSERT ON transactions FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation()`); err != nil {
			t.Fatal(err)
		}
		if got := first(sp, "transactions"); got == "transactions_holding_feed" {
			t.Error("CONTROL: a trigger named transactions_feed sorts after holding_feed's -- the measure cannot tell")
		}
	})
}

// TestHolding00046And00047_DownGiveBackAndUpTakesItAgain: 00047's Down takes holding_site_locations
// away; 00046's takes holding_feed, its two triggers, the definer's transactions / employees /
// site_punches grants and the table away -- and REFUSES, changing nothing, while site_punches holds
// a row (the site's view of a recorded tap is not erased -- §4.3, §4.6). The Ups give HEAD's surface
// back. The site rows other tests committed are set aside in the savepoint (holdingBelow46's helper).
func TestHolding00046And00047_DownGiveBackAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up46, down46 := opMigrationSections(t, op00046File)
	up47, down47 := opMigrationSections(t, op00047File)
	read := func(q pgx.Tx) string {
		t.Helper()
		var s string
		if err := q.QueryRow(ctx, `SELECT
			coalesce((SELECT string_agg(proname, ',' ORDER BY proname) FROM pg_proc WHERE proowner = 'tappa_holdingdefiner'::regrole), '') || ' | ' ||
			coalesce((SELECT string_agg(tgname, ',' ORDER BY tgname) FROM pg_trigger
			           WHERE tgrelid IN ('public.transactions'::regclass, 'public.transaction_reviews'::regclass) AND NOT tgisinternal), '') || ' | ' ||
			coalesce(to_regclass('public.site_punches')::text, '-')`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		for _, tb := range []string{"transactions", "employees"} {
			s += " | " + tb + ":" + opColumns(t, ctx, q, "tappa_holdingdefiner", tb, "SELECT")
		}
		return s
	}
	head := read(tx)
	if !strings.Contains(head, "holding_feed") || !strings.Contains(head, "holding_site_locations") || !strings.Contains(head, "site_punches") {
		t.Fatalf("PREMISE at HEAD: %s", head)
	}
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		holdingSetAsideSiteRows(t, ctx, sp)
		opRunSection(t, ctx, sp, down47, "00047 Down")
		if s := read(sp); strings.Contains(s, "holding_site_locations") || !strings.Contains(s, "holding_feed") {
			t.Errorf("after 00047's Down: %s", s)
		}
		opRunSection(t, ctx, sp, down46, "00046 Down")
		s := read(sp)
		if strings.Contains(s, "holding_feed") || strings.Contains(s, "site_punches") || !strings.HasSuffix(s, "| transactions: | employees:") {
			t.Errorf("after 00046's Down: %s", s)
		}
		opRunSection(t, ctx, sp, up46, "00046 Up again")
		opRunSection(t, ctx, sp, up47, "00047 Up again")
		if again := read(sp); again != head {
			t.Errorf("after the Ups again:\n %s\nwant HEAD's\n %s", again, head)
		}
	})
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		holdingSetAsideSiteRows(t, ctx, sp)
		w := holdingWorldTx(t, ctx, sp, "")
		spMust(t, ctx, sp, spKFTap(w, "nfc", "ok", "in", true, 100))
		opRunSection(t, ctx, sp, down47, "00047 Down with a projection row")
		opWant(t, holdingTryDown(t, ctx, sp, down46), sqlstatePrerequisiteState, "00046's Down while site_punches holds a row")
		if s := read(sp); !strings.Contains(s, "holding_feed") || !strings.Contains(s, "site_punches") {
			t.Errorf("the refused Down changed something: %s", s)
		}
	})
}
