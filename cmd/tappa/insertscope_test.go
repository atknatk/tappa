package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// query_test.go files in six packages already assert §4.5's BELT — an explicit
// tenant predicate beside RLS's braces — for the queries THEIR package calls. This
// file asserts the same property for the one statement shape those belts were built
// around and cannot see, across the WHOLE of db/queries.
//
// 🔴 WHY IT IS HERE AND NOT IN A SEVENTH query_test.go (backlog T20). The belts
// derive their query list from their own package's `q.X(` calls, which is the right
// shape for a WHERE-clause check and the wrong one for this: measured on this tree,
// the twelve `INSERT ... VALUES` statements in db/queries divide as
//
//	four in a belted package    AppendPolicyVersion, AttachPolicyResource,
//	                            CreateTenantPolicy (internal/domain/tenant, which
//	                            grew an INSERT arm in M6-09 phase B) and CreateTenant
//	                            (internal/domain/signup)
//	eight in a package with NO belt file at all
//	                            RecordAuditEvent (internal/audit) · CreateInvite
//	                            (internal/invite) · CreateSession (internal/session) ·
//	                            EnsureBaselinePolicy, EnsureBaselinePolicyVersion,
//	                            EnsurePolicyAttachment and InsertTransaction
//	                            (internal/domain/checkin)
//
// and the last of those is THE PRODUCT'S MAIN WRITE PATH — every attendance record
// (CLAUDE.md §4.3, immutable). Closing that by giving internal/domain/checkin its own
// derivation machinery would duplicate ~200 lines for one property; deriving from the
// SQL instead covers all twelve, and the next one, in one place.
//
// ⚠️ COUNTED LIMITS, ALL FALSE-NEGATIVE:
//   - It reads db/queries only. SQL written inline in Go is invisible here — the
//     rule against that is CLAUDE.md §6 ("handler icine ham SQL yazilmaz") and the
//     package belts, not this scan.
//   - It reads the STATEMENT, not the caller. That the parameter named @tenant_id
//     really holds the caller's tenant is what WithTenant and RLS's WITH CHECK
//     enforce; this proves only that the statement says so out loud, which is
//     exactly what "belt" means when the braces are already on.
//   - `INSERT ... SELECT` is deliberately out of scope: that form HAS a WHERE clause
//     and is the package belts' business. internal/domain/tenant's insertFindings
//     draws the same line for the same reason.
//
// 🔴 A MULTI-ROW `VALUES` USED TO BE A HOLE IN THIS LIST AND IN THE SCAN, and it is
// now closed rather than counted (measured, 2026-08-19). The scan read the FIRST
// parenthesised group only, so
//
//	INSERT INTO transactions (tenant_id, …) VALUES (@tenant_id, …), ('0000…0001', …)
//
// PASSED: row one named the caller's tenant, row two wrote a literal, and nothing
// looked at row two. The belt is the point of this file, so the fix is to read EVERY
// row. (The braces held: RLS's WITH CHECK refuses a cross-tenant INSERT, measured.
// A hole in the belt is still a hole in the belt, and the counted-limits list that
// did not mention it was the more misleading half.)
//
// insertValuesRE matches `INSERT INTO <table> ( <columns> ) VALUES (`. A column list
// is required, which is the shape db/queries is written in and the shape sqlc needs
// in order to name parameters.
var insertValuesRE = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+(?:public\.)?([a-z_][a-z0-9_]*)\s*\(([^)]*)\)\s*VALUES\s*\(`)

// TestQueriesInsertValuesNameTheTenantTheCallerNamed is the belt for the shape the
// package belts cannot see.
func TestQueriesInsertValuesNameTheTenantTheCallerNamed(t *testing.T) {
	t.Parallel()

	scoped := tenantScopedTables(t)
	dir := filepath.Join(repoRoot, "db", "queries")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading db/queries: %v", err)
	}

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("reading %s: %v", e.Name(), rerr)
		}
		findings, n := insertScopeFindings(e.Name(), string(raw), scoped)
		checked += n
		for _, f := range findings {
			t.Error(f)
		}
	}

	// ANTI-VACUITY. Without this the whole test passes on a tree where the regexp
	// stopped matching — which is the failure mode every scanner in this repo has
	// hit at least once.
	if checked < 10 {
		t.Fatalf("only %d INSERT ... VALUES row(s) were checked; eleven were scoped and "+
			"checkable when this floor was set, so this scan has gone blind", checked)
	}
	t.Logf("%d INSERT ... VALUES row(s) checked across db/queries", checked)
}

// insertScopeFindings is THE scan. Both the check above and its negative control
// call it, which is deliberate: the control used to carry its own copy of the loop,
// and a control that re-implements the thing it controls stops controlling it the
// moment the two drift. (This repository has paid for that twice — see M5-09's
// drift guard.)
//
// It returns one message per problem and the number of VALUES ROWS it managed to
// look at, which is what the anti-vacuity floor counts.
func insertScopeFindings(name, raw string, scoped map[string]bool) (findings []string, checked int) {
	body := stripSQLLineComments(raw)
	for _, loc := range insertValuesRE.FindAllStringSubmatchIndex(body, -1) {
		table := strings.ToLower(body[loc[2]:loc[3]])
		// The scope column of `tenants` is its own primary key (00001's policy
		// says so), and the parameter a caller names it with is @id.
		want, wantValue := "tenant_id", "@tenant_id"
		if table == "tenants" {
			want, wantValue = "id", "@id"
		} else if !scoped[table] {
			// A table with no tenant_id column is out of scope BY SCHEMA, not by
			// exemption — legal_documents is the one such table today and
			// migration 00020 argues at length why it has none.
			continue
		}
		where := name + ": INSERT INTO " + table
		cols := splitTopLevelCommas(body[loc[4]:loc[5]])

		// EVERY row of a multi-row VALUES, not just the first. See the header.
		at := loc[1] - 1
		for row := 1; ; row++ {
			values, endsAt, ok := balancedGroupAt(body, at)
			if !ok {
				findings = append(findings, where+" has a VALUES list this scan cannot read; that is "+
					"reported rather than passed over, because a statement a checker cannot parse is a "+
					"statement nothing is checking")
				break
			}
			checked++
			rowWhere := where
			if row > 1 {
				rowWhere = fmt.Sprintf("%s (VALUES row %d)", where, row)
			}
			findings = append(findings, rowScopeFindings(rowWhere, cols, values, want, wantValue)...)

			// A following `, (` opens the next row; anything else ends the list.
			next := at
			for endsAt++; endsAt < len(body); endsAt++ {
				c := body[endsAt]
				if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
					continue
				}
				if c != ',' {
					break
				}
				for endsAt++; endsAt < len(body); endsAt++ {
					if c := body[endsAt]; c != ' ' && c != '\t' && c != '\n' && c != '\r' {
						break
					}
				}
				if endsAt < len(body) && body[endsAt] == '(' {
					next = endsAt
				}
				break
			}
			if next == at {
				break
			}
			at = next
		}
	}
	return findings, checked
}

// rowScopeFindings checks ONE parenthesised VALUES row against the column list.
func rowScopeFindings(where string, cols []string, values, want, wantValue string) []string {
	vals := splitTopLevelCommas(values)
	if len(cols) != len(vals) {
		return []string{fmt.Sprintf("%s has %d column(s) and %d value(s); this scan cannot line them up, "+
			"so it refuses to say the row is scoped", where, len(cols), len(vals))}
	}
	at := -1
	for i, c := range cols {
		if strings.EqualFold(strings.Trim(strings.TrimSpace(c), `"`), want) {
			at = i
		}
	}
	if at < 0 {
		return []string{fmt.Sprintf("%s does not name %s at all, so the row it writes is scoped by nothing "+
			"the statement says.\ncolumns read: %s", where, want, strings.Join(cols, ", "))}
	}
	if got := strings.TrimSpace(vals[at]); got != wantValue {
		return []string{fmt.Sprintf("%s writes %s from %q rather than %s. A row written with a tenant the "+
			"CALLER did not name is the one write RLS cannot help with: WITH CHECK "+
			"refuses ANOTHER tenant's id, and happily accepts a literal, or a value "+
			"read from somewhere else that happens to be this one", where, want, got, wantValue)}
	}
	return nil
}

// tenantScopedTables reads the tables that are born with a tenant_id from the
// migrations, so this scan and the schema cannot disagree about which tables are
// tenant-scoped. A table with no such column (legal_documents) is not exempted here,
// it simply has no scope column to name.
func tenantScopedTables(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join(repoRoot, "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading db/migrations: %v", err)
	}
	createRE := regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:public\.)?([a-z_][a-z0-9_]*)\s*\(`)
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			t.Fatalf("reading %s: %v", e.Name(), rerr)
		}
		// The Down section undoes the Up; reading it would add nothing and could add
		// a table this schema no longer has.
		body := string(raw)
		if i := strings.Index(strings.ToLower(body), "-- +goose down"); i >= 0 {
			body = body[:i]
		}
		body = stripSQLLineComments(body)
		for _, loc := range createRE.FindAllStringSubmatchIndex(body, -1) {
			table := strings.ToLower(body[loc[2]:loc[3]])
			cols, _, ok := balancedGroupAt(body, loc[1]-1)
			if !ok {
				continue
			}
			if regexp.MustCompile(`(?is)(^|[(,\s])"?tenant_id"?\s+uuid`).MatchString(cols) {
				out[table] = true
			}
		}
	}
	if len(out) < 5 {
		t.Fatalf("found only %d tenant-scoped tables in db/migrations; the schema scan has "+
			"gone blind and every check above would be skipped", len(out))
	}
	return out
}

// stripSQLLineComments removes `--` comment lines. Whole lines only: a `--` inside a
// string literal is not a comment, and no query in db/queries carries a trailing
// comment on a line this scan reads.
func stripSQLLineComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			b.WriteByte('\n')
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// splitTopLevelCommas splits on commas that are not inside parentheses, so
// `gen_random_uuid(), @tenant_id` is two values rather than three.
func splitTopLevelCommas(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(s[start:]); last != "" {
		out = append(out, last)
	}
	return out
}

// balancedGroupAt returns the contents of the parenthesised group beginning at i and
// the index of its closing parenthesis. The end index is what lets the caller look
// for a following `, (` — the multi-row VALUES the first version of this scan could
// not see.
func balancedGroupAt(s string, i int) (string, int, bool) {
	if i < 0 || i >= len(s) || s[i] != '(' {
		return "", 0, false
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[i+1 : j], j, true
			}
		}
	}
	return "", 0, false
}

// TestInsertScopeScanFlagsTheShapesItClaims is the negative control: a scanner with
// no proof that it fires is a scanner that may have stopped reading.
//
// 🔴 IT CALLS THE SCAN, IT DOES NOT RE-IMPLEMENT IT. The first version carried its
// own copy of the loop in a closure named `findings`, so it proved that THE COPY
// fired — and a copy and its original drift. Measured on this tree: the copy and the
// real loop had already diverged in what they reported for a table this scan skips,
// and neither the control nor the scan could have told anybody. Everything below now
// drives insertScopeFindings, the exact function
// TestQueriesInsertValuesNameTheTenantTheCallerNamed drives.
func TestInsertScopeScanFlagsTheShapesItClaims(t *testing.T) {
	t.Parallel()

	// The schema map the real scan derives from db/migrations, stated here so the
	// control does not depend on the migrations to know that transactions is scoped.
	scoped := map[string]bool{"transactions": true}
	findings := func(sql string) int {
		out, _ := insertScopeFindings("control.sql", sql, scoped)
		return len(out)
	}

	for name, sql := range map[string]string{
		"the tenant column written from another parameter": `
			INSERT INTO transactions (tenant_id, employee_id) VALUES (@employee_id, @employee_id);`,
		"the tenant column written from a literal": `
			INSERT INTO transactions (tenant_id, employee_id)
			VALUES ('00000000-0000-0000-0000-000000000000', @employee_id);`,
		"the tenant column not named at all": `
			INSERT INTO transactions (employee_id, verdict) VALUES (@employee_id, 'ok');`,
		"a values list this scan cannot line up": `
			INSERT INTO transactions (tenant_id, employee_id, verdict) VALUES (@tenant_id);`,
		// 🔴 THE SHAPE THAT USED TO PASS. Row one is correct and row two writes a
		// literal; a scan that reads only the first group calls this clean.
		"a SECOND VALUES row that writes a literal tenant": `
			INSERT INTO transactions (tenant_id, employee_id)
			VALUES (@tenant_id, @employee_id), ('00000000-0000-0000-0000-000000000001', @employee_id);`,
		"a second row on its own line, indented": `
			INSERT INTO transactions (tenant_id, employee_id)
			VALUES
			  (@tenant_id, @employee_id),
			  ('00000000-0000-0000-0000-000000000001', @employee_id);`,
		"a third row that is the wrong one": `
			INSERT INTO transactions (tenant_id, employee_id)
			VALUES (@tenant_id, @employee_id), (@tenant_id, @employee_id), (@employee_id, @employee_id);`,
	} {
		if findings(sql) == 0 {
			t.Errorf("%s: the scan reported nothing.\nSQL: %s", name, sql)
		}
	}

	for name, sql := range map[string]string{
		"tenant first": `
			INSERT INTO transactions (tenant_id, employee_id) VALUES (@tenant_id, @employee_id);`,
		"tenant last": `
			INSERT INTO transactions (employee_id, tenant_id) VALUES (@employee_id, @tenant_id);`,
		"with a function call among the values": `
			INSERT INTO transactions (id, tenant_id, employee_id)
			VALUES (gen_random_uuid(), @tenant_id, @employee_id);`,
		// A correct multi-row insert must stay quiet, or the widened scan is noise.
		"two rows, both naming the caller's tenant": `
			INSERT INTO transactions (tenant_id, employee_id)
			VALUES (@tenant_id, @employee_id), (@tenant_id, @other_employee_id);`,
		"a row whose values contain a parenthesised call": `
			INSERT INTO transactions (id, tenant_id, employee_id)
			VALUES (gen_random_uuid(), @tenant_id, @a), (gen_random_uuid(), @tenant_id, @b);`,
	} {
		if n := findings(sql); n != 0 {
			t.Errorf("%s: the scan reported %d finding(s) on a CORRECT statement; a noisy "+
				"check is one the next author waters down.\nSQL: %s", name, n, sql)
		}
	}

	// AND THE ROW COUNT IS ASSERTED, because "no findings" is also what a scan that
	// stopped after row one reports.
	if _, checked := insertScopeFindings("control.sql", `
		INSERT INTO transactions (tenant_id, employee_id)
		VALUES (@tenant_id, @a), (@tenant_id, @b), (@tenant_id, @c);`, scoped); checked != 3 {
		t.Errorf("a three-row VALUES was counted as %d row(s); the scan is not reading past row one", checked)
	}
}
