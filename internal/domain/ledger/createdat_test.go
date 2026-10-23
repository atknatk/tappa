package ledger

// createdat_test.go -- M15-05 step 10 (ADR 0032 §A.6 "Eşzamanlı yazım"; the security lens's F-4):
// transactions.created_at has ONE writer, the database (DEFAULT now(), 00005). The checkout
// classification's W compares two rows' created_at to tell a manager's correction (written later)
// from a forgotten return (written in order) -- a statement that wrote created_at would let a
// caller choose which of the two a checkout is.
//
// THE SCAN READS STATEMENTS, NOT LINES (the M15-05 review round: the first version's one regex
// missed seven spellings). SQL files are read with their comments removed; Go files are PARSED
// (go/ast) and only their string literals -- concatenations folded, a non-literal operand read as
// "?" -- are read as SQL, so prose in a Go comment is never a statement and a statement split
// across `+` is still one. A spelling the scan cannot judge is red, not skipped. Each spelling is
// a probe below, and the real sources must pass the same function.
//
// THE LAST TURN (the security lens's second pass): a comment is removed only OUTSIDE a '...' literal
// ("SELECT '--'; INSERT ..." was green -- the cleaner ate the statement as a comment); every .sql
// file of the repository is read, not only db/; and an INDIRECT table name -- the name in a string
// literal of its own beside an INSERT/COPY and a created_at (Go: one file; SQL: one dollar-quoted
// body with an EXECUTE) -- cannot be judged and is red, as is any pgx.Identifier not spelled in
// literals.
//
// 🔴 THIS IS A NET, NOT A LOCK. A scan of source text can be walked around (a table name assembled
// in another file, read from configuration, built byte by byte); what it catches is the ordinary
// spelling and the review's seven plus five. The lasting closure is on the database's side -- a
// column privilege (REVOKE INSERT (created_at) ON transactions FROM tappa_app, with INSERT granted
// column by column) or a BEFORE INSERT trigger that overwrites created_at with now() -- and both
// were weighed and set aside on purpose (ADR 0032 §A.6): test fixtures write created_at into the
// past (the age and debounce tests, e.g. internal/domain/checkin/holding_db_test.go), and a column
// grant needs maintenance on every new transactions column.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	// dollarQuote is a dollar-quote delimiter ($$, $body$): the text between two is a function body,
	// SQL like the rest, so it is read -- and it is the unit the indirect-name rule judges.
	dollarQuote = regexp.MustCompile(`\$[A-Za-z_]*\$`)
	// tableNameLiteral is a literal that is the table's name and nothing else (an indirect name).
	tableNameLiteral = regexp.MustCompile(`(?i)^\s*(?:"?public"?\s*\.\s*)?"?transactions"?\s*$`)
	writesWord       = regexp.MustCompile(`(?i)\b(?:insert|copy)\b`)
	executeWord      = regexp.MustCompile(`(?i)\bexecute\b`)
	triggerExecute   = regexp.MustCompile(`(?i)\bexecute\s+(?:function|procedure)\b`)
	transactionsWord = regexp.MustCompile(`(?i)\btransactions\b`)
	// txTable is the table's name, bare or quoted, with or without its schema.
	txTable = `(?:"?public"?\s*\.\s*)?"?transactions"?`
	// insertInto finds an INSERT INTO the table; what follows is read by hand (alias, column list).
	insertInto = regexp.MustCompile(`(?is)\binsert\s+into\s+` + txTable)
	// otherWriters are statements that write rows of the table without an INSERT's column list
	// this scan could read: MERGE ... INSERT, COPY ... FROM, and dynamic SQL in a migration.
	otherWriters = []*regexp.Regexp{
		regexp.MustCompile(`(?is)\bmerge\s+into\s+` + txTable + `\b`),
		regexp.MustCompile(`(?is)\bcopy\s+` + txTable + `[\s(]`),
		regexp.MustCompile(`(?is)\bexecute\s+(?:format\s*\(\s*)?'[^;]*\binsert\b[^;]*\btransactions\b`),
	}
	aliasWord   = regexp.MustCompile(`(?i)^(?:as\s+)?([a-z_][a-z0-9_]*)`)
	notAnAlias  = map[string]bool{"select": true, "values": true, "default": true, "overriding": true, "on": true, "with": true}
	createdAtRE = regexp.MustCompile(`(?i)\bcreated_at\b`)
)

// stripSQLComments removes `--` and /* */ comments OUTSIDE single-quoted literals and quoted
// identifiers -- a '--' inside a literal is text, not a comment (the second pass's real defect).
// Dollar quotes are left as they are: a function body is SQL and its comments are comments.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(sql) {
				if sql[j] == c {
					if j+1 < len(sql) && sql[j+1] == c { // '' or "" escapes the quote
						j += 2
						continue
					}
					break
				}
				j++
			}
			end := min(j+1, len(sql))
			b.WriteString(sql[i:end])
			i = end
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			j := strings.Index(sql[i+2:], "*/")
			if j < 0 {
				i = len(sql)
			} else {
				i += j + 4
			}
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// sqlWriterProblems judges one piece of SQL: each INSERT INTO transactions must name its columns,
// none of them created_at, and no other kind of writer may appear; and a dollar-quoted body that
// EXECUTEs dynamic SQL naming transactions beside an INSERT/COPY and created_at cannot be judged.
func sqlWriterProblems(sql string) []string {
	sql = stripSQLComments(sql)
	var out []string
	// The pieces between dollar quotes alternate outside/inside; only an INSIDE piece -- a function or
	// DO body -- can run dynamic SQL (outside, EXECUTE is a trigger's EXECUTE FUNCTION).
	for i, body := range dollarQuote.Split(sql, -1) {
		if i%2 == 0 {
			continue
		}
		body = triggerExecute.ReplaceAllString(body, " ")
		if executeWord.MatchString(body) && transactionsWord.MatchString(body) && writesWord.MatchString(body) && createdAtRE.MatchString(body) {
			out = append(out, "dynamic SQL names transactions beside an INSERT/COPY and created_at -- it cannot be judged: "+
				strings.Join(strings.Fields(body[:min(len(body), 120)]), " "))
		}
	}
	for _, loc := range insertInto.FindAllStringIndex(sql, -1) {
		rest := sql[loc[1]:]
		if rest != "" && isWordByte(rest[0]) {
			continue // transactions_something: another table
		}
		rest = strings.TrimLeft(rest, " \t\r\n")
		if m := aliasWord.FindStringSubmatch(rest); m != nil && !notAnAlias[strings.ToLower(m[1])] {
			rest = strings.TrimLeft(rest[len(m[0]):], " \t\r\n")
		}
		stmt := strings.Join(strings.Fields(sql[loc[0]:min(len(sql), loc[1]+80)]), " ")
		if !strings.HasPrefix(rest, "(") {
			out = append(out, "an INSERT INTO transactions with no column list writes every column by position, created_at included: "+stmt)
			continue
		}
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			out = append(out, "an INSERT INTO transactions whose column list does not close: "+stmt)
			continue
		}
		if createdAtRE.MatchString(rest[:end]) {
			out = append(out, "an INSERT INTO transactions names created_at: "+stmt)
		}
	}
	for _, re := range otherWriters {
		if m := re.FindString(sql); m != "" {
			out = append(out, "a writer of transactions this scan cannot read a column list from: "+strings.Join(strings.Fields(m), " "))
		}
	}
	return out
}

// goWriterProblems parses one Go file and judges its string literals as SQL, plus any
// pgx.Identifier naming the table (CopyFrom's target).
func goWriterProblems(name string, src []byte) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	// The indirect-name rule's three facts, over the whole file.
	var namesTable, writes, namesCreatedAt bool
	// fold returns the text of a string concatenation, "?" for a non-literal operand; ok is false
	// when no operand is a string literal (an arithmetic +).
	var fold func(e ast.Expr) (string, bool)
	fold = func(e ast.Expr) (string, bool) {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				s, err := strconv.Unquote(v.Value)
				return s, err == nil
			}
		case *ast.BinaryExpr:
			if v.Op == token.ADD {
				l, lok := fold(v.X)
				r, rok := fold(v.Y)
				return l + r, lok || rok
			}
		case *ast.ParenExpr:
			return fold(v.X)
		}
		return "?", false
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				namesTable = namesTable || tableNameLiteral.MatchString(s)
				writes = writes || writesWord.MatchString(s)
				namesCreatedAt = namesCreatedAt || createdAtRE.MatchString(s)
			}
		}
		return true
	})
	if namesTable && writes && namesCreatedAt {
		out = append(out, "the table's name is a string literal of its own in a file that also spells an INSERT/COPY and "+
			"created_at -- an indirect name (Sprintf, concatenation) cannot be judged")
	}
	isIdentifier := func(e ast.Expr) bool {
		switch t := e.(type) {
		case *ast.SelectorExpr:
			return t.Sel.Name == "Identifier"
		case *ast.Ident:
			return t.Name == "Identifier"
		}
		return false
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			if isIdentifier(v.Fun) {
				out = append(out, "a conversion to pgx.Identifier: the table it names is not spelled out -- it cannot be judged")
			}
		case *ast.BinaryExpr, *ast.BasicLit:
			if s, ok := fold(n.(ast.Expr)); ok {
				out = append(out, sqlWriterProblems(s)...)
				return false // its operands are inside s already
			}
		case *ast.CompositeLit:
			if !isIdentifier(v.Type) {
				return true
			}
			for _, el := range v.Elts {
				lit, ok := el.(*ast.BasicLit)
				if !ok {
					out = append(out, "a pgx.Identifier with an element that is not a literal -- it cannot be judged")
					continue
				}
				if s, err := strconv.Unquote(lit.Value); err == nil && s == "transactions" {
					out = append(out, "a pgx.Identifier names transactions (a COPY writes every column it is given, created_at included)")
				}
			}
		}
		return true
	})
	return out, nil
}

// TestTransactions_NoStatementWritesCreatedAt runs the scan over every place a row of
// transactions can be written from: db/queries (sqlc's source), every migration (function bodies
// included) and every non-test Go file (internal/store, sqlc's output, among them). Measured when
// written (2026-10-21): three statements in db/queries/transactions.sql (InsertTransaction,
// InsertManualTransaction, InsertManualSiteTransaction) and their generated copies; none in a
// migration. Test files are not scanned: fixtures write created_at into the past on purpose (age
// and debounce tests -- the reason ADR 0032 chose this test over a wall-clock trigger). The probes
// first: every spelling the review found must be red, and the ordinary ones green.
func TestTransactions_NoStatementWritesCreatedAt(t *testing.T) {
	t.Parallel()
	probes := []struct {
		name  string
		goSrc bool
		text  string
		red   bool
	}{
		{"the writers' own shape", false, "INSERT INTO transactions (tenant_id, occurred_at) VALUES ($1, $2)", false},
		{"created_at in the list", false, "INSERT INTO transactions (tenant_id, created_at) VALUES ($1, $2)", true},
		{"a quoted name", false, `INSERT INTO "transactions" (tenant_id, created_at) VALUES ($1, $2)`, true},
		{"schema and a quoted name", false, `INSERT INTO public."transactions" (tenant_id, created_at) VALUES ($1, $2)`, true},
		{"an alias", false, "INSERT INTO transactions AS t (tenant_id, created_at) VALUES ($1, $2)", true},
		{"no column list, SELECT", false, "INSERT INTO transactions SELECT * FROM transactions_backup", true},
		{"no column list, VALUES", false, "INSERT INTO public.transactions VALUES ($1, $2)", true},
		{"MERGE ... INSERT", false, "MERGE INTO transactions t USING s ON false WHEN NOT MATCHED THEN INSERT (tenant_id, created_at) VALUES (s.a, s.b)", true},
		{"COPY", false, "COPY transactions (tenant_id, created_at) FROM STDIN", true},
		{"dynamic SQL in a migration", false, "EXECUTE format('INSERT INTO %I (tenant_id, created_at) VALUES ($1, $2)', 'transactions');", true},
		{"prose in a SQL comment", false, "-- the INSERT INTO transactions below\nSELECT 1", false},
		{"another table", false, "INSERT INTO transactions_archive SELECT 1", false},
		{"Go: an alias in a literal", true, "package p\nconst q = `INSERT INTO transactions AS t (tenant_id, created_at) VALUES ($1, $2)`\n", true},
		{"Go: a concatenation", true, "package p\nconst q = \"INSERT INTO transactions (tenant_id, \" + \"created_at) VALUES ($1, $2)\"\n", true},
		{"Go: a concatenation with no literal list", true, "package p\nfunc f(cols string) string { return \"INSERT INTO transactions \" + cols }\n", true},
		{"Go: CopyFrom's identifier", true, "package p\nimport \"github.com/jackc/pgx/v5\"\nvar id = pgx.Identifier{\"transactions\"}\n", true},
		{"Go: prose in a comment", true, "package p\n// the INSERT into transactions, and every trigger behind it\nconst q = 1\n", false},
		{"Go: the writers' own shape", true, "package p\nconst q = `INSERT INTO transactions (\n    tenant_id, occurred_at\n) VALUES ($1, $2)`\n", false},
		// The second pass's five.
		{"a '--' inside a literal is not a comment", false, "SELECT '--'; INSERT INTO transactions (tenant_id, created_at) VALUES ($1, $2)", true},
		{"a '/*' inside a literal is not a comment", false, "SELECT '/*'; INSERT INTO transactions (tenant_id, created_at) VALUES ($1, $2); SELECT '*/'", true},
		{"dynamic SQL with quote_ident", false, "DO $$ DECLARE v text; BEGIN v := 'INSERT INTO ' || quote_ident('transactions') || ' (tenant_id, created_at) VALUES (1, now())'; EXECUTE v; END $$;", true},
		{"dynamic SQL with format and a variable", false, "DO $$ DECLARE v text := 'transactions'; BEGIN EXECUTE format('INSERT INTO %I (tenant_id, created_at) VALUES (1, now())', v); END $$;", true},
		{"a function body that names transactions and only reads it", false, "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN (SELECT count(*) FROM transactions); END $$;", false},
		{"Go: Sprintf with the name as an argument", true, "package p\nimport \"fmt\"\nvar q = fmt.Sprintf(\"INSERT INTO %s (tenant_id, created_at) VALUES ($1, $2)\", \"transactions\")\n", true},
		{"Go: a concatenation with the name in a constant", true, "package p\nconst table = \"transactions\"\nfunc f() string { return \"INSERT INTO \" + table + \" (tenant_id, created_at) VALUES ($1, $2)\" }\n", true},
		{"Go: a conversion to pgx.Identifier", true, "package p\nimport \"github.com/jackc/pgx/v5\"\nfunc f(tbl string) pgx.Identifier { return pgx.Identifier([]string{tbl}) }\n", true},
		{"Go: a pgx.Identifier of a variable", true, "package p\nimport \"github.com/jackc/pgx/v5\"\nfunc f(tbl string) pgx.Identifier { return pgx.Identifier{tbl} }\n", true},
		{"Go: the table's name as a tab name, nothing written", true, "package p\nconst TabTransactions = \"transactions\"\n", false},
	}
	for _, p := range probes {
		var got []string
		if p.goSrc {
			var err error
			if got, err = goWriterProblems(p.name+".go", []byte(p.text)); err != nil {
				t.Fatalf("probe %q does not parse: %v", p.name, err)
			}
		} else {
			got = sqlWriterProblems(p.text)
		}
		if (len(got) > 0) != p.red {
			t.Errorf("PROBE %q: findings %q, want red=%v -- the scan cannot be trusted on the real sources", p.name, got, p.red)
		}
	}

	root := filepath.Join("..", "..", "..")
	queries, sqlFiles, goFiles := 0, 0, 0
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".tools", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".sql") {
			// EVERY .sql file of the repository (db/, scripts/, test/fixtures/ ...), not only db/.
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sqlFiles++
			for _, p := range sqlWriterProblems(string(raw)) {
				t.Errorf("%s: %s", path, p)
			}
			if strings.Contains(filepath.ToSlash(path), "db/queries/") {
				queries += len(insertInto.FindAllString(stripSQLComments(string(raw)), -1))
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := goWriterProblems(path, raw)
		if err != nil {
			t.Errorf("%s does not parse: %v", path, err)
			return nil
		}
		goFiles++
		for _, p := range found {
			t.Errorf("%s: %s", path, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if queries < 3 || goFiles < 100 || sqlFiles < 60 {
		t.Errorf("PREMISE: %d INSERT INTO transactions in db/queries (want the three writers at least), %d Go files parsed, %d .sql files read",
			queries, goFiles, sqlFiles)
	}
}
