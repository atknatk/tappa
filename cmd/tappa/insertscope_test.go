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
	body := stripSQLComments(raw)
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
		body = stripSQLComments(body)
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

// The states stripSQLComments walks. A comment is only a comment in the first one,
// which is the whole point of lexing rather than pattern-matching lines.
const (
	sqlCode         = iota
	sqlLineComment  // `--` … end of line
	sqlBlockComment // `/*` … `*/`
	sqlString       // '…'
	sqlQuotedIdent  // "…"
	sqlDollarBody   // $tag$ … $tag$
)

// blockOpen and blockClose are the two-character block-comment tokens, spelled as
// concatenations ON PURPOSE.
//
// 🔴 A LITERAL OPEN TOKEN IN THIS FILE HIDES THE TESTS BELOW IT FROM ANOTHER GATE, and
// the measurement is why this looks silly rather than because it does.
// TestEveryNamedTestExists collects test declarations after stripping block comments
// out of the source with a regexp, and that regexp cannot tell a Go string literal
// from code -- so an open token inside a string runs to the next close token wherever
// that happens to be. Measured (2026-08-20): with the open token written out in
// stripSQLComments below, the strip ran from that line to the next close token, which
// sits past TestInsertScopeScanFlagsTheShapesItClaims; the declaration disappeared,
// this file's own citation of it became dangling, and the ratchet went 53 -> 55.
//
// The gate is not wrong to strip -- it exists because a test can be COMMENTED OUT and
// leave its citation behind -- and a file whose whole subject is SQL comments is the
// one file that cannot spell those two characters freely. So every open token in this
// file comes from here, and the ones inside prose comments close on their own line.
const (
	blockOpen  = "/" + "*"
	blockClose = "*" + "/"
)

// stripSQLComments blanks every SQL comment -- `--` to end of line WHEREVER on the
// line it starts, and `/* … */` across however many lines it spans -- while leaving
// string literals, quoted identifiers and dollar-quoted bodies byte for byte as they
// were. Every newline survives in place, so a line number counted on the result still
// points at the line it came from.
//
// 🔴 IT USED TO BE `strings.HasPrefix(trimmed, "--")`, AND THAT WAS A HOLE IN BOTH
// SCANS THAT CALL IT. Whole comment LINES were blanked; a comment that began after
// some code was not. Both callers match a statement HEAD across whitespace
// (`INSERT\s+INTO\s+…`), and a comment is whitespace to PostgreSQL and was not
// whitespace to `\s+`, so these two parse as writes on this database and matched
// nothing here:
//
//	INSERT INTO /* audit */ policy_versions (…)
//	INSERT INTO -- audit
//	policy_versions (…)
//
// Measured end to end (2026-08-20, FAZ B3's security audit): the first form appended
// to db/queries/policies.sql and regenerated left `go build ./...`,
// `go test ./cmd/tappa/` and `scripts/redline-check.sh` ALL at exit 0, while the same
// query written without the comment turned the gate red. The escape needed no
// hand-written Go either -- sqlc v1.28.0, the version the Makefile pins, copies the
// comment into the generated constant verbatim, so it arrives through the `make gen`
// step CLAUDE.md §2 makes mandatory. Both forms are now controls:
// TestStripSQLComments_ReadsCommentsTheWayPostgresDoes and the comment cases in
// TestNoteProvenanceScan_ReportsTheBypassesItExistsToReport and
// TestInsertScopeScanFlagsTheShapesItClaims.
//
// A comment becomes ONE SPACE rather than nothing, because PostgreSQL treats it as a
// token separator: `INSERT INTO/*x*/policy_versions` is two tokens there, and deleting
// the comment outright would fuse them into an identifier that matches nothing.
//
// ⚠️ COUNTED LIMITS, all of them in the OVER-report direction, which is the one a
// detector can afford:
//   - BLOCK COMMENTS DO NOT NEST HERE and they do in PostgreSQL. `/* a /* b */ c */`
//     is one comment there and closes at the first `*/` here, so the tail is read as
//     code and would be REPORTED. That is strictly less text removed than PostgreSQL
//     removes, so no statement PostgreSQL would run can hide behind it. It is also
//     what scripts/redline-check.sh's own sql_lex does, deliberately.
//   - `E'…\'…'` (a backslash-escaped quote, which needs the E prefix and
//     standard_conforming_strings) would be read as a string that ended early. db/
//     carries none today (2026-08-20: 33 hits for `E'`, every one of them prose inside
//     a `--` line -- `UPDATE'tir`, `DATABASE's`), and the failure mode is again
//     over-reporting: text PostgreSQL calls data would be read as code.
//   - An UNBALANCED quote leaves the walk inside a string to the end of the input, so
//     comments after it are kept as code. Over-report again.
func stripSQLComments(s string) string {
	out, _ := stripSQLCommentsState(s)
	return out
}

// stripSQLCommentsState is stripSQLComments plus the state the walk ENDED in, which is
// the one symptom of a swallowed file that has no innocent cause. An unbalanced quote
// or an odd `$$` leaves the lexer inside a literal to the end of the input, and from
// that point on nothing is stripped and nothing is checked. Only the tree-wide control
// in TestStripSQLComments_ReadsCommentsTheWayPostgresDoes reads it; the scans do not,
// because for them the failure is already the safe direction.
func stripSQLCommentsState(s string) (string, int) {
	var b strings.Builder
	b.Grow(len(s))
	state, tag := sqlCode, ""
	for i := 0; i < len(s); {
		c := s[i]
		two := ""
		if i+1 < len(s) {
			two = s[i : i+2]
		}
		switch state {
		case sqlLineComment:
			if c == '\n' {
				state = sqlCode
				b.WriteByte(c)
			}
			i++
		case sqlBlockComment:
			switch {
			case two == blockClose:
				// The separating space was written when the comment OPENED; a
				// second one here would only make the output harder to compare.
				state = sqlCode
				i += 2
			case c == '\n':
				b.WriteByte(c)
				i++
			default:
				i++
			}
		case sqlString, sqlQuotedIdent:
			// `''` inside a string closes and reopens, which is the same text out.
			b.WriteByte(c)
			if (state == sqlString && c == '\'') || (state == sqlQuotedIdent && c == '"') {
				state = sqlCode
			}
			i++
		case sqlDollarBody:
			if strings.HasPrefix(s[i:], tag) {
				b.WriteString(tag)
				i += len(tag)
				state = sqlCode
				continue
			}
			b.WriteByte(c)
			i++
		default: // sqlCode
			switch {
			case two == "--":
				state = sqlLineComment
				b.WriteByte(' ')
				i += 2
			case two == blockOpen:
				state = sqlBlockComment
				b.WriteByte(' ')
				i += 2
			case c == '\'':
				state = sqlString
				b.WriteByte(c)
				i++
			case c == '"':
				state = sqlQuotedIdent
				b.WriteByte(c)
				i++
			default:
				if t, ok := dollarQuoteTagAt(s, i); ok {
					state, tag = sqlDollarBody, t
					b.WriteString(t)
					i += len(t)
					continue
				}
				b.WriteByte(c)
				i++
			}
		}
	}
	// A line comment that runs to the end of the input has ended, not been left open.
	if state == sqlLineComment {
		state = sqlCode
	}
	return b.String(), state
}

// dollarQuoteTagAt reports the dollar-quote tag opening at i -- `$$` or `$name$` --
// and nothing for a parameter placeholder, whose `$1` would need a tag starting with a
// digit. It matters because db/migrations holds 38 `$$` bodies (2026-08-20) whose
// plpgsql carries both `--` comments and apostrophes, and db/queries is written in
// sqlc's `$1` form; reading one as the other would swallow the rest of the file.
func dollarQuoteTagAt(s string, i int) (string, bool) {
	if i >= len(s) || s[i] != '$' {
		return "", false
	}
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '$':
			return s[i : j+1], true
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && j > i+1:
		default:
			return "", false
		}
	}
	return "", false
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
		// 🔴 AND THE SHAPES A WHOLE-LINE COMMENT STRIPPER WALKED PAST (FAZ B3). A
		// comment is whitespace to PostgreSQL; it was not whitespace to this scan,
		// so a literal tenant hid behind one.
		"a literal tenant behind a trailing line comment": `
			INSERT INTO transactions (tenant_id, employee_id) -- rebuilt nightly
			VALUES ('00000000-0000-0000-0000-000000000001', @employee_id);`,
		"a literal tenant behind a block comment inside the statement": `
			INSERT INTO /* audit */ transactions (tenant_id, employee_id)
			VALUES ('00000000-0000-0000-0000-000000000001', @employee_id);`,
		"a block comment spanning lines inside the column list": `
			INSERT INTO transactions (tenant_id, /* the person
			   who tapped */ employee_id)
			VALUES ('00000000-0000-0000-0000-000000000001', @employee_id);`,
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
		// 🔴 THE OTHER HALF OF THE COMMENT FIX, and the half that keeps it from
		// becoming noise: a `--` or a block-comment open INSIDE a string literal is
		// data. If the
		// stripper took either for a comment it would eat the rest of the row and
		// report a VALUES list it "cannot read" on a statement that is correct.
		"a value containing what looks like a line comment": `
			INSERT INTO transactions (tenant_id, employee_id, note)
			VALUES (@tenant_id, @employee_id, '-- not a comment');`,
		"a value containing what looks like a block comment": `
			INSERT INTO transactions (tenant_id, employee_id, note)
			VALUES (@tenant_id, @employee_id, '/* not a comment */');`,
		"a correct row with a comment between the columns and VALUES": `
			INSERT INTO transactions (tenant_id, employee_id) -- one row per tap
			VALUES (@tenant_id, @employee_id);`,
		// A statement that is entirely commented out is not a statement.
		"a wrong statement that is wholly inside a block comment": `
			/* INSERT INTO transactions (tenant_id, employee_id)
			   VALUES ('00000000-0000-0000-0000-000000000001', @employee_id); */`,
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

// TestStripSQLComments_ReadsCommentsTheWayPostgresDoes pins the lexer both scans in
// this package rest on, directly rather than through them.
//
// 🔴 IT EXISTS BECAUSE THE OLD ONE'S DOC COMMENT WAS A CLAIM NOTHING CHECKED. It said
// whole-line stripping was enough because "no query in db/queries carries a trailing
// comment on a line this scan reads" -- true about that day's tree, and nothing at all
// about tomorrow's. The two halves below are the two ways a lexer fails: a comment it
// does not see (a write walks past the scan) and a comment it invents inside a string
// (a correct statement is reported, and a noisy check is one the next author waters
// down).
func TestStripSQLComments_ReadsCommentsTheWayPostgresDoes(t *testing.T) {
	t.Parallel()

	// GONE, and gone as a SPACE: PostgreSQL separates tokens with a comment, so the
	// text either side must not fuse.
	for name, tc := range map[string]struct{ in, want string }{
		"a whole comment line":        {"-- gone\nSELECT 1\n", " \nSELECT 1\n"},
		"a trailing comment":          {"SELECT 1 -- gone\n", "SELECT 1  \n"},
		"a comment with no newline":   {"SELECT 1 -- gone", "SELECT 1  "},
		"a block comment":             {"SELECT /* gone */ 1", "SELECT   1"},
		"a block comment with no gap": {"INSERT INTO/*gone*/policy_versions", "INSERT INTO policy_versions"},
		"a block comment over lines":  {"INSERT INTO /* a\nb */ policy_versions", "INSERT INTO  \n policy_versions"},
		"a comment inside a comment":  {"-- a " + blockOpen + " b\nSELECT 1", " \nSELECT 1"},
	} {
		if got := stripSQLComments(tc.in); got != tc.want {
			t.Errorf("%s: stripSQLComments(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}

	// KEPT, byte for byte: none of these is a comment, and a stripper that thought
	// otherwise would eat the rest of a correct statement.
	for name, in := range map[string]string{
		"a line comment inside a string":      "SELECT '-- kept' FROM t",
		"a block comment inside a string":     "SELECT '/* kept */' FROM t",
		"a doubled quote then a comment mark": "SELECT 'it''s -- kept' FROM t",
		"a quoted identifier":                 `SELECT "od--d" FROM t`,
		"a dollar-quoted body":                "DO $$ BEGIN -- kept\nEND $$",
		"a tagged dollar body":                "DO $b$ /* kept */ $b$",
		"a bcrypt hash, which is not a tag":   "SELECT '$2a$10$abc/*x*/' FROM t",
		"sqlc placeholders, not a tag":        "VALUES ($1, $2)",
	} {
		if got := stripSQLComments(in); got != in {
			t.Errorf("%s: stripSQLComments(%q) = %q, want it unchanged", name, in, got)
		}
	}

	// AND THE TAG READER UNDER IT, because `$1` and `$$` differ by one character and
	// reading the first as the second would swallow db/queries from that point on.
	for in, want := range map[string]string{
		"$$ x $$":    "$$",
		"$b$ x $b$":  "$b$",
		"$_1$ x":     "$_1$",
		"$1, $2)":    "",
		"$2a$10$":    "",
		"$ x":        "",
		"$":          "",
		"no dollar":  "",
		"$unclosed ": "",
	} {
		got, ok := dollarQuoteTagAt(in, 0)
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("dollarQuoteTagAt(%q) = %q, want %q", in, got, want)
		}
	}

	// LINE NUMBERS SURVIVE. policyVersionWrites reports a position by counting the
	// newlines before a match, so a stripper that dropped one would point the reader
	// at the wrong line -- and a finding nobody can locate is a finding nobody fixes.
	for name, in := range map[string]string{
		"a block comment spanning three lines": "a\n/* b\nc\nd */e\nf\n",
		"line comments throughout":             "-- a\nb -- c\n-- d\ne\n",
		"a dollar body spanning lines":         "DO $$\n-- x\n$$;\n",
	} {
		if got, want := strings.Count(stripSQLComments(in), "\n"), strings.Count(in, "\n"); got != want {
			t.Errorf("%s: %d newline(s) survived, want %d.\ninput: %q\noutput: %q",
				name, got, want, in, stripSQLComments(in))
		}
	}

	// AND THE TREE IT ACTUALLY READS, because a control built only out of hand-written
	// snippets proves the lexer works on hand-written snippets. Every .sql this package
	// reads is walked, and two things are asserted about each: the newline count is
	// unchanged (a position reported against the file has to name the right line), and
	// the walk ENDS IN CODE STATE.
	//
	// 🔴 THE SECOND ONE REPLACED A CHECK THAT WAS SIMPLY FALSE, and the measurement is
	// worth keeping. It first read "an even number of `'` survives", on the reasoning
	// that an odd one means the walk ended inside a string. Six migrations failed it
	// (2026-08-20), and all six were right and the check was wrong: the apostrophes sit
	// in Turkish prose inside `$$` bodies -- 00003's "domain'de", 00004's "M1-08'in" --
	// where PostgreSQL reads the whole body as one literal and an apostrophe is just a
	// character. The end state is the property that was actually meant: an unbalanced
	// quote or an odd `$$` leaves the walk inside a literal to EOF, and from there
	// nothing is stripped and nothing is checked.
	files := 0
	for _, dir := range []string{"queries", "migrations"} {
		entries, err := os.ReadDir(filepath.Join(repoRoot, "db", dir))
		if err != nil {
			t.Fatalf("reading db/%s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			at := filepath.Join(repoRoot, "db", dir, e.Name())
			raw, rerr := os.ReadFile(at)
			if rerr != nil {
				t.Fatalf("reading %s: %v", e.Name(), rerr)
			}
			files++
			out, end := stripSQLCommentsState(string(raw))
			if got, want := strings.Count(out, "\n"), strings.Count(string(raw), "\n"); got != want {
				t.Errorf("db/%s/%s: %d newline(s) survived stripping, want %d; a position "+
					"reported against this file would name the wrong line", dir, e.Name(), got, want)
			}
			if end != sqlCode {
				t.Errorf("db/%s/%s: the lexer ended in state %d rather than code, so it ran off "+
					"the end inside a literal or a comment and everything after that point was "+
					"read as neither", dir, e.Name(), end)
			}
		}
	}
	if files < 30 {
		t.Fatalf("only %d .sql file(s) were read under db/; 38 were there when this floor was "+
			"set (2026-08-20, measured by this test), so nothing above was measured", files)
	}
	t.Logf("%d .sql file(s) lexed under db/", files)
}
