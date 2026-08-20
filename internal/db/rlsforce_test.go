package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// rlsforce_test.go -- THE GATE FOR "RLS IS ON, AND IT REACHES THE OWNER TOO"
// (CLAUDE.md §6's five, items 2 and 3; §4.5).
//
// 🔴 WHY IT EXISTS: THE CLASS HAD NO GATE, AND scripts/redline-check.sh SAID IT DID.
// That script's class-to-door table named the RLS isolation suite as "the real door"
// for the RLS-removal class. Measured by a security audit in the 4th round, with
// `ALTER TABLE ... NO FORCE ROW LEVEL SECURITY` applied to the committed schema:
//
//	owner connection, under a FOREIGN app.tenant_id: 404 335 rows / 80 961 tenants
//	internal/db suite:  ok  github.com/atknatk/tappa/internal/db  8.802s   -- ALL GREEN
//
// The reason is structural and it is worth stating plainly, because it is why the
// map was wrong rather than merely stale: the isolation suite runs as tappa_app,
// tappa_app is NOT the owner of any table (ADR 0002 madde 1 requires exactly that),
// and the ONLY thing FORCE ROW LEVEL SECURITY does is apply the policies to the
// table's OWNER. A suite that never connects as an owner cannot observe FORCE. And
// the owner is not a hypothetical role: migrations run as tappa_owner, `make seed`
// runs as tappa_owner, and an operator's psql session is tappa_owner.
//
// 🔴 AND THE ASSERTION EXISTED FOR FIVE TABLES OUT OF SEVENTEEN. Before this file,
// `relforcerowsecurity` was read by rls_test.go (policies, policy_versions,
// policy_attachments), locations_test.go (locations) and tagsinventory_test.go
// (tags). Eleven tenant-scoped tables had no such assertion at all -- transactions,
// the product's evidence table, among them.
//
// 🔴 THE LIST IS DERIVED TWICE AND THE TWO MUST AGREE — WHICH IS THE 5th ROUND'S
// REPAIR, AND IT WAS MEASURED. Until this round the list came from db/migrations
// ALONE, and that derivation only reads a table's CREATE TABLE body. Measured on this
// tree: `CREATE TABLE zz_late_scoped (id uuid); ALTER TABLE zz_late_scoped ADD COLUMN
// tenant_id uuid NOT NULL;` left the derivation at seventeen names, the new table
// absent from every list, and this whole file GREEN — a tenant-scoped table with no
// row level security at all, watched by nothing. The isolation suite could not see it
// either: that suite walks seventeen names by hand.
//
// So the GATE'S list now comes from the LIVE CATALOGUE (tenantScopedTablesFromCatalogue:
// every table carrying a tenant_id right now), which closes the ADD COLUMN direction
// by construction — a table that acquires the column any way at all is in the list the
// moment it does. The migrations derivation is kept as the ANTI-VACUITY CROSS-CHECK,
// because a catalogue-derived list has the mirror weakness: a live
// `ALTER TABLE ... DROP COLUMN tenant_id` would silently SHRINK it, and the shrink is
// the same act this file exists to catch. The two are asserted equal
// (tenantScopedTables); a disagreement in either direction is a red test that names
// the direction.
//
// cmd/tappa's insertscope_test.go derives the migrations half the same way and for the
// same reason ("the scan and the schema cannot disagree about which tables are
// tenant-scoped"); it cannot be imported, because that file is in `package main`, so
// the derivation is re-stated rather than shared and both carry an anti-vacuity floor.
//
// The two derivations were MEASURED against each other rather than assumed to agree
// (2026-08-20): both produce the same seventeen names — admin_sessions, admin_users,
// audit_log, billing_periods, departments, employee_invites, employees, locations,
// password_resets, policies, policy_attachments, policy_versions, sessions, tags,
// tenants, transaction_reviews, transactions. The catalogue query returns sixteen of
// them (`tenants` carries no tenant_id column; it is scoped by its own primary key,
// which migration 00001's policy states, and both derivations add it by name for that
// reason). legal_documents is deliberately outside all three: it has no tenant_id at
// all and migration 00020 argues why.

// rlsFlags is the catalogue answer for one table.
type rlsFlags struct {
	table   string
	exists  bool
	enabled bool // pg_class.relrowsecurity
	forced  bool // pg_class.relforcerowsecurity
}

func (f rlsFlags) String() string {
	switch {
	case !f.exists:
		return fmt.Sprintf("%s: the table is not in the schema at all", f.table)
	case !f.enabled && !f.forced:
		return fmt.Sprintf("%s: row level security is DISABLED (relrowsecurity=false, "+
			"relforcerowsecurity=false) -- every tenant's rows are visible to every connection", f.table)
	case !f.enabled:
		return fmt.Sprintf("%s: row level security is DISABLED (relrowsecurity=false)", f.table)
	default:
		return fmt.Sprintf("%s: row level security is not FORCED (relforcerowsecurity=false) -- the "+
			"policies are skipped for the table's OWNER, which is the role migrations, seeds and "+
			"psql all connect as", f.table)
	}
}

// rlsFlagsQuery reads the two bits PostgreSQL actually acts on, for one table.
// to_regclass returns NULL rather than erroring for a name that is not there, so a
// dropped table is a finding instead of a failed scan.
const rlsFlagsQuery = `
	SELECT c.oid IS NOT NULL,
	       COALESCE(c.relrowsecurity, false),
	       COALESCE(c.relforcerowsecurity, false)
	  FROM (SELECT to_regclass('public.' || quote_ident($1))::oid AS oid) r
	  LEFT JOIN pg_class c ON c.oid = r.oid`

// scanRLSFlags returns one entry per table that is NOT both enabled and forced.
func scanRLSFlags(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tables []string,
) ([]rlsFlags, error) {
	var out []rlsFlags
	for _, t := range tables {
		var f = rlsFlags{table: t}
		if err := q.QueryRow(ctx, rlsFlagsQuery, t).Scan(&f.exists, &f.enabled, &f.forced); err != nil {
			return nil, fmt.Errorf("read the RLS flags of %s: %w", t, err)
		}
		if !f.exists || !f.enabled || !f.forced {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].table < out[j].table })
	return out, nil
}

// rlsFlagsRefusal turns a scan result into the failure the gate reports, or nil.
//
// 🔴 IT IS A FUNCTION SO THAT THE POSITIVE CONTROL DRIVES THE SHIPPED REFUSAL RATHER
// THAN A COPY OF IT -- viewsecurity_test.go's unsafeViewsRefusal carries the same
// argument, and this repository has paid twice for a control that re-implemented the
// thing it controlled.
func rlsFlagsRefusal(found []rlsFlags) error {
	if len(found) == 0 {
		return nil
	}
	var b strings.Builder
	for _, f := range found {
		fmt.Fprintf(&b, "\n  %s", f)
	}
	return fmt.Errorf("%d tenant-scoped table(s) are not carrying BOTH ENABLE and FORCE ROW LEVEL "+
		"SECURITY (CLAUDE.md §6's five, §4.5):%s\n"+
		"FORCE is the half no other test in this repository can see: the isolation suite connects "+
		"as tappa_app, which owns nothing, and FORCE is precisely what applies the policies to a "+
		"table's OWNER -- the role migrations, seeds and every operator psql session use",
		len(found), b.String())
}

// tenantScopedTablesQuery reads every table that CARRIES a tenant_id right now.
//
// attisdropped/attnum matter and are not decoration: a dropped column keeps its
// pg_attribute row under a mangled name, and system columns have negative attnum.
// relkind is restricted to ordinary and partitioned TABLES, because a view carrying a
// tenant_id has no relrowsecurity bit to assert and viewsecurity_test.go is its gate.
const tenantScopedTablesQuery = `
	SELECT DISTINCT c.relname
	  FROM pg_attribute a
	  JOIN pg_class     c ON c.oid = a.attrelid
	  JOIN pg_namespace n ON n.oid = c.relnamespace
	 WHERE a.attname = 'tenant_id'
	   AND NOT a.attisdropped
	   AND a.attnum > 0
	   AND c.relkind IN ('r', 'p')
	   AND n.nspname = 'public'`

// tenantScopedTablesFromCatalogue reads the tables that carry a tenant_id in the
// SCHEMA AS IT IS, plus `tenants`, which has no such column and is scoped by its own
// primary key instead (migration 00001's policy says so, and scripts/redline-check.sh
// and insertscope_test.go both carry the same exception for the same reason).
//
// It takes the querier rather than a pool so the ADD COLUMN control below can run it
// INSIDE its own BEGIN ... ROLLBACK and see the table it just made.
func tenantScopedTablesFromCatalogue(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
},
) ([]string, error) {
	rows, err := q.Query(ctx, tenantScopedTablesQuery)
	if err != nil {
		return nil, fmt.Errorf("read the tenant-scoped tables from the catalogue: %w", err)
	}
	defer rows.Close()

	seen := map[string]bool{"tenants": true}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan a catalogue row: %w", err)
		}
		seen[strings.ToLower(name)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the tenant-scoped tables from the catalogue: %w", err)
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// tenantScopedTables is the GATE'S list: the live catalogue, cross-checked against
// db/migrations. Either derivation alone has a blind direction (see the file header),
// so a disagreement is a failure that names which direction it is.
func tenantScopedTables(t *testing.T, ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
},
) []string {
	t.Helper()

	live, err := tenantScopedTablesFromCatalogue(ctx, q)
	if err != nil {
		t.Fatalf("catalogue derivation: %v", err)
	}
	// ANTI-VACUITY, the catalogue half. A query that stopped matching would leave an
	// empty list and every assertion below would pass on a schema nobody looked at.
	if len(live) < 17 {
		t.Fatalf("only %d tenant-scoped table(s) carry a tenant_id in the live schema (%v); "+
			"seventeen did when this floor was set, so either the catalogue query has gone "+
			"blind or tables have lost the column", len(live), live)
	}

	migrations := tenantScopedTablesFromMigrations(t)
	inLive := make(map[string]bool, len(live))
	for _, name := range live {
		inLive[name] = true
	}
	inMigrations := make(map[string]bool, len(migrations))
	for _, name := range migrations {
		inMigrations[name] = true
	}
	var lateScoped, missing []string
	for _, name := range live {
		if !inMigrations[name] {
			lateScoped = append(lateScoped, name)
		}
	}
	for _, name := range migrations {
		if !inLive[name] {
			missing = append(missing, name)
		}
	}
	if len(lateScoped) > 0 {
		t.Errorf("%v carr(y|ies) a tenant_id in the live schema but no CREATE TABLE in "+
			"db/migrations declares one. CLAUDE.md §6 says a tenant-scoped table is BORN with "+
			"the column; a table that acquires it by ALTER ... ADD COLUMN is invisible to every "+
			"other derivation in this repository (cmd/tappa's insertscope_test.go and "+
			"scripts/redline-check.sh both read the CREATE TABLE body). The gate below still "+
			"scans it — that is why the list comes from the catalogue — but nothing else does",
			lateScoped)
	}
	if len(missing) > 0 {
		t.Errorf("%v (is|are) created with a tenant_id in db/migrations but carr(y|ies) no such "+
			"column in the live schema. Either the table is gone or the column was dropped, and "+
			"a DROP COLUMN tenant_id is the exact act that would silently shrink a "+
			"catalogue-derived list", missing)
	}
	return live
}

// tenantScopedTablesFromMigrations reads the tables born with a tenant_id out of
// db/migrations, plus `tenants`, which is scoped by its own primary key (migration
// 00001's policy says so, and scripts/redline-check.sh and insertscope_test.go both
// carry the same exception for the same reason).
//
// ⚠️ IT READS THE CREATE TABLE BODY ONLY, WHICH IS WHY IT IS NO LONGER THE GATE'S
// LIST. A table scoped by a later ALTER ... ADD COLUMN is not in what it returns.
// That blindness is now a measured, asserted fact rather than an unnoticed one:
// tenantScopedTables cross-checks this against the catalogue, and
// TestRLS_TheGateSeesATableScopedAfterItWasCreated drives the case.
func tenantScopedTablesFromMigrations(t *testing.T) []string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	dir := filepath.Join(root, "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading db/migrations: %v", err)
	}
	createRE := regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:public\.)?"?([a-z_][a-z0-9_]*)"?\s*\(`)
	scopeRE := regexp.MustCompile(`(?is)(^|[(,\s])"?tenant_id"?\s+uuid`)

	seen := map[string]bool{"tenants": true}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("reading %s: %v", e.Name(), rerr)
		}
		// The Down section undoes the Up; reading it would add a table this schema no
		// longer has.
		body := string(raw)
		if i := strings.Index(strings.ToLower(body), "-- +goose down"); i >= 0 {
			body = body[:i]
		}
		for _, loc := range createRE.FindAllStringSubmatchIndex(body, -1) {
			cols, ok := balancedGroup(body, loc[1]-1)
			if !ok {
				continue
			}
			if scopeRE.MatchString(cols) {
				seen[strings.ToLower(body[loc[2]:loc[3]])] = true
			}
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)

	// ANTI-VACUITY. Without this the gate below passes on a tree where the regexp
	// stopped matching, which is the failure mode every scanner in this repository
	// has hit at least once.
	if len(out) < 17 {
		t.Fatalf("only %d tenant-scoped table(s) were derived from db/migrations (%v); seventeen were "+
			"there when this floor was set, so the derivation has gone blind and every assertion "+
			"below would be skipped", len(out), out)
	}
	return out
}

// balancedGroup returns the contents of the parenthesised group beginning at i.
func balancedGroup(s string, i int) (string, bool) {
	if i < 0 || i >= len(s) || s[i] != '(' {
		return "", false
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[i+1 : j], true
			}
		}
	}
	return "", false
}

// TestRLS_EveryTenantScopedTableIsEnabledAndForced is the gate itself.
//
// It reads the catalogue through the APPLICATION pool on purpose: pg_class is
// world-readable, the assertion is about the schema rather than about the connection,
// and using the owner pool would make the test skip wherever DATABASE_MIGRATE_URL is
// absent -- which is exactly where an operator is most likely to have run a stray
// ALTER.
func TestRLS_EveryTenantScopedTableIsEnabledAndForced(t *testing.T) {
	d := appDB(t)
	assertAppRole(t, d)

	ctx := context.Background()
	tables := tenantScopedTables(t, ctx, d.pool)
	found, err := scanRLSFlags(ctx, d.pool, tables)
	if err != nil {
		t.Fatalf("catalogue scan: %v", err)
	}
	if err := rlsFlagsRefusal(found); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d tenant-scoped tables carry ENABLE + FORCE ROW LEVEL SECURITY: %s",
		len(tables), strings.Join(tables, ", "))
}

// TestRLS_TheForceGateActuallyFires is the POSITIVE CONTROL, and without it the gate
// above is satisfied by a schema in which nothing was ever measured.
//
// 🔴 EVERY CASE RUNS INSIDE ONE BEGIN ... ROLLBACK, AND IT MUTATES A PROBE TABLE
// RATHER THAN A REAL ONE — WHICH IS A CHANGE MADE AFTER A MEASUREMENT, NOT A
// PREFERENCE. The first version of this control ran the mutations against
// password_resets and employee_invites, and `go test -race ./...` then failed like
// this:
//
//	appendonly_truncate_test.go:180: owner TRUNCATE tenants CASCADE: want the
//	  append-only trigger, got ERROR: deadlock detected (SQLSTATE 40P01)
//
// `ALTER TABLE ... DISABLE/NO FORCE ROW LEVEL SECURITY` takes ACCESS EXCLUSIVE, and
// so does `TRUNCATE ... CASCADE`; two transactions each holding what the other wants
// is a deadlock, and PostgreSQL picked the OTHER test as the victim. A control that
// makes an unrelated test flaky is not a control, it is a liability — role_test.go
// drew the same line for the same reason ("ALTER TABLE transactions OWNER TO would
// take an ACCESS EXCLUSIVE lock that every other test in the suite would queue
// behind").
//
// 🔴 WHAT IS LOST BY USING A PROBE, AND HOW IT WAS PAID FOR SEPARATELY. A probe
// proves the SCAN reads the catalogue bit; it does not prove that the gate's LIST
// contains a table an operator would actually break, nor that the gate's own
// `t.Fatal` is wired up. Both were measured out of band, on 2026-08-20, by applying
// the DDL to the COMMITTED schema, running the gate, and reverting it:
//
//	ALTER TABLE password_resets  NO FORCE ROW LEVEL SECURITY -> gate FAILED, naming
//	                                password_resets and saying "not FORCED"
//	ALTER TABLE employee_invites DISABLE  ROW LEVEL SECURITY -> gate FAILED, naming
//	                                employee_invites and saying "DISABLED"
//	both reverted; 18 of 18 tables read relrowsecurity AND relforcerowsecurity after
//
// The list half is ALSO asserted here, mechanically and every run, by
// TestRLS_TheGateScansTheTablesAnOperatorWouldBreak below — because an out-of-band
// measurement is a transcript, and a transcript is not a gate.
func TestRLS_TheForceGateActuallyFires(t *testing.T) {
	owner := ownerDB(t)
	assertOwnerRole(t, owner)

	ctx := context.Background()
	// DERIVED OUTSIDE THE TRANSACTION ON PURPOSE. The catalogue derivation would see
	// the probe table created below and the probe is appended by name here, so
	// deriving inside would list it twice and the "exactly one finding" assertion at
	// the end would be measuring the derivation instead of the mutation.
	real := tenantScopedTables(t, ctx, owner.pool)

	const probe = "zz_rlsforce_probe"
	scanned := append(append([]string{}, real...), probe)

	for _, tc := range []struct {
		name    string
		ddl     string
		wantMsg string
	}{
		{
			name:    "a table that loses FORCE is named",
			ddl:     "ALTER TABLE " + probe + " NO FORCE ROW LEVEL SECURITY",
			wantMsg: "not FORCED",
		},
		{
			name:    "a table that loses row level security altogether is named",
			ddl:     "ALTER TABLE " + probe + " DISABLE ROW LEVEL SECURITY",
			wantMsg: "DISABLED",
		},
		{
			// THE THIRD SHAPE, WHICH IS NEITHER: a table the migrations create and the
			// schema no longer has. A scan that erred here, or quietly skipped, would
			// report nothing for the most complete removal there is.
			name:    "a table that is gone entirely is named",
			ddl:     "DROP TABLE " + probe,
			wantMsg: "not in the schema at all",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := owner.pool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			// The rollback is the whole safety property of this test: no probe table
			// survives it, whatever happens below.
			defer func() {
				if err := tx.Rollback(context.Background()); err != nil && !strings.Contains(err.Error(), "closed") {
					t.Errorf("rollback: %v", err)
				}
			}()

			for _, stmt := range []string{
				`CREATE TABLE ` + probe + ` (tenant_id uuid NOT NULL)`,
				`ALTER TABLE ` + probe + ` ENABLE ROW LEVEL SECURITY`,
				`ALTER TABLE ` + probe + ` FORCE ROW LEVEL SECURITY`,
			} {
				if _, err := tx.Exec(ctx, stmt); err != nil {
					t.Fatalf("building the probe (%q): %v", stmt, err)
				}
			}

			// A CONTROL MEASUREMENT FIRST: inside this very transaction, with the probe
			// correctly configured, the shipped scan must be silent about EVERYTHING —
			// the seventeen real tables and the probe. Otherwise "it went red" says
			// nothing about the mutation.
			before, err := scanRLSFlags(ctx, tx, scanned)
			if err != nil {
				t.Fatalf("baseline scan: %v", err)
			}
			if err := rlsFlagsRefusal(before); err != nil {
				t.Fatalf("the scan was ALREADY failing before the mutation, so this control "+
					"cannot attribute anything to it:\n%v", err)
			}

			if _, err := tx.Exec(ctx, tc.ddl); err != nil {
				t.Fatalf("probe DDL (%s): %v", tc.ddl, err)
			}

			after, err := scanRLSFlags(ctx, tx, scanned)
			if err != nil {
				t.Fatalf("scan after the mutation: %v", err)
			}
			refusal := rlsFlagsRefusal(after)
			if refusal == nil {
				t.Fatalf("%q left the shipped refusal SILENT. The gate would be green on a schema "+
					"where that table is readable by its owner under any tenant id", tc.ddl)
			}
			// AND IT MUST SAY WHICH TABLE, because "something is wrong with row level
			// security" is not an actionable failure on a seventeen-table schema.
			if !strings.Contains(refusal.Error(), probe) {
				t.Errorf("the refusal does not name %s:\n%v", probe, refusal)
			}
			if !strings.Contains(refusal.Error(), tc.wantMsg) {
				t.Errorf("the refusal does not say %q, so it does not distinguish this from the "+
					"other two removals:\n%v", tc.wantMsg, refusal)
			}
			// AND NOTHING ELSE MOVED: exactly one table, the mutated one. This is what
			// keeps the seventeen real names in the scan honest rather than decorative.
			if len(after) != 1 {
				t.Errorf("the scan reported %d finding(s) for one mutation:\n%v", len(after), refusal)
			}
		})
	}
}

// TestRLS_TheGateSeesATableScopedAfterItWasCreated is the door for the class the
// 5th round found open, and it drives the exact DDL that measured it open.
//
// 🔴 WHAT WAS MEASURED. tenantScopedTablesFromMigrations reads a table's CREATE TABLE
// body, so a table scoped LATER —
//
//	CREATE TABLE zz_late_scoped (id uuid);
//	ALTER TABLE  zz_late_scoped ADD COLUMN tenant_id uuid NOT NULL;
//
// — stayed out of the derivation, out of every list in this file, and out of the
// isolation suite (which walks the names by hand). The result was a tenant-scoped
// table carrying NO row level security at all with this file GREEN. No table in this
// schema is shaped that way today; the CLASS had no gate, which is a different thing
// from the case being absent.
//
// It asserts three things about one transaction, and the third is the one that
// matters: the catalogue derivation FINDS the table, the migrations derivation does
// NOT (so the cross-check in tenantScopedTables really is load-bearing rather than
// two copies of one query), and the shipped refusal NAMES it.
//
// Everything runs inside BEGIN ... ROLLBACK against a probe table, for the reason
// TestRLS_TheForceGateActuallyFires records: DDL on a real table takes ACCESS
// EXCLUSIVE and deadlocks the append-only suite.
func TestRLS_TheGateSeesATableScopedAfterItWasCreated(t *testing.T) {
	owner := ownerDB(t)
	assertOwnerRole(t, owner)

	ctx := context.Background()
	const probe = "zz_late_scoped"

	tx, err := owner.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// The rollback is the whole safety property: no probe table survives this test,
	// whatever happens below.
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil && !strings.Contains(err.Error(), "closed") {
			t.Errorf("rollback: %v", err)
		}
	}()

	// A CONTROL MEASUREMENT FIRST: before the ALTER, the table exists and is NOT
	// tenant-scoped, so no derivation should mention it. Otherwise "it appeared"
	// says nothing about the ALTER.
	if _, err := tx.Exec(ctx, `CREATE TABLE `+probe+` (id uuid)`); err != nil {
		t.Fatalf("building the probe: %v", err)
	}
	before, err := tenantScopedTablesFromCatalogue(ctx, tx)
	if err != nil {
		t.Fatalf("catalogue derivation before the ALTER: %v", err)
	}
	if slices.Contains(before, probe) {
		t.Fatalf("%s is already listed as tenant-scoped before it has a tenant_id (%v)", probe, before)
	}

	if _, err := tx.Exec(ctx, `ALTER TABLE `+probe+` ADD COLUMN tenant_id uuid NOT NULL`); err != nil {
		t.Fatalf("scoping the probe: %v", err)
	}

	after, err := tenantScopedTablesFromCatalogue(ctx, tx)
	if err != nil {
		t.Fatalf("catalogue derivation after the ALTER: %v", err)
	}
	if !slices.Contains(after, probe) {
		t.Fatalf("%s carries a tenant_id and the catalogue derivation does not list it (%v). The "+
			"gate would scan every table BUT the one an operator just scoped, and its RLS bits "+
			"would be watched by nothing", probe, after)
	}

	// AND THE MIGRATIONS DERIVATION MUST NOT SEE IT, which is what makes the
	// cross-check a real check rather than a comparison of one query with itself.
	if fromMigrations := tenantScopedTablesFromMigrations(t); slices.Contains(fromMigrations, probe) {
		t.Errorf("the migrations derivation lists %s, which no CREATE TABLE in db/migrations "+
			"declares. The two derivations are then the same derivation and the cross-check in "+
			"tenantScopedTables proves nothing", probe)
	}

	// AND THE SHIPPED REFUSAL NAMES IT: the probe has no row level security at all,
	// so being in the list has to end in a failure that says which table and why.
	found, err := scanRLSFlags(ctx, tx, after)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	refusal := rlsFlagsRefusal(found)
	if refusal == nil {
		t.Fatalf("a tenant-scoped table with NO row level security left the shipped refusal "+
			"SILENT; every tenant's rows in %s would be visible to every connection", probe)
	}
	if !strings.Contains(refusal.Error(), probe) {
		t.Errorf("the refusal does not name %s:\n%v", probe, refusal)
	}
	if len(found) != 1 {
		t.Errorf("the scan reported %d finding(s) for one probe:\n%v", len(found), refusal)
	}
}

// TestRLS_TheGateScansTheTablesAnOperatorWouldBreak is the half a probe cannot carry:
// the gate's LIST must contain the tables that actually hold evidence. A scan that
// looked at nothing, or at a fresh probe only, would satisfy every assertion above.
//
// The names below are deliberately hard-coded HERE and derived THERE: this is the
// place where a hand-written expectation is the point, because the question is
// whether the derivation still finds them.
//
// ⚠️ IT ASSERTS AGAINST THE MIGRATIONS HALF AND THAT IS DELIBERATE, NOT AN OVERSIGHT.
// The gate's own list is the catalogue's, and tenantScopedTables asserts the two are
// EQUAL — so pinning the names on this half pins them on both, transitively. Reading
// files instead of a database is what keeps this particular assertion running on a
// machine with no Postgres, which is where a derivation that quietly returned nothing
// would otherwise go unnoticed longest.
func TestRLS_TheGateScansTheTablesAnOperatorWouldBreak(t *testing.T) {
	t.Parallel()

	tables := tenantScopedTablesFromMigrations(t)
	have := make(map[string]bool, len(tables))
	for _, name := range tables {
		have[name] = true
	}
	for _, want := range []string{
		"transactions",        // §4.3, the product's evidence
		"audit_log",           // §4.3
		"transaction_reviews", // §4.3
		"employees", "sessions", "tags", "locations", "departments",
		"admin_users", "admin_sessions", "password_resets",
		"policies", "policy_versions", "policy_attachments",
		"employee_invites", "billing_periods",
		"tenants", // scoped by its own primary key
	} {
		if !have[want] {
			t.Errorf("the gate does not scan %s (it scans %v). Every assertion in this file "+
				"would still pass, and that table's FORCE bit would be watched by nothing",
				want, tables)
		}
	}
}
