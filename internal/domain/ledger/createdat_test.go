package ledger

// createdat_test.go -- M15-05 step 10 (ADR 0032 §A.6 "Eşzamanlı yazım"; the security lens's F-4):
// transactions.created_at has ONE writer, the database (DEFAULT now(), 00005). The checkout
// classification's W compares two rows' created_at to tell a manager's correction (written later)
// from a forgotten return (written in order) -- a statement that wrote created_at would let a
// caller choose which of the two a checkout is.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// insertIntoTransactions finds `INSERT INTO [public.]transactions` and, when one follows, its
// column list.
var insertIntoTransactions = regexp.MustCompile(`(?is)\binsert\s+into\s+(?:public\.)?transactions\b(\s*\(([^)]*)\))?`)

// TestTransactions_NoStatementWritesCreatedAt scans every place a row of transactions can be
// written from: each `INSERT INTO transactions` of db/queries (sqlc's source; internal/store is its
// output and is scanned with the rest of the Go), of every non-test Go file, and of every
// migration (function bodies included). Each must NAME its columns -- a column-less INSERT writes
// created_at by position -- and none may name created_at. Measured when written (2026-10-21): three
// statements in db/queries/transactions.sql (InsertTransaction, InsertManualTransaction,
// InsertManualSiteTransaction), their generated copies, one comment in
// internal/domain/checkin/policyset.go, none in a migration. Test files are not scanned: fixtures
// write created_at into the past on purpose (age and debounce tests -- the reason ADR 0032 chose
// this test over a wall-clock trigger).
func TestTransactions_NoStatementWritesCreatedAt(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	queries := 0
	scan := func(path string, sql bool) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(root, path)
		for _, m := range insertIntoTransactions.FindAllStringSubmatch(string(raw), -1) {
			switch {
			case m[1] == "" && sql:
				t.Errorf("%s: an INSERT INTO transactions with no column list writes every column by position, created_at included: %q",
					rel, strings.Join(strings.Fields(m[0]), " "))
			case m[1] == "":
				// Go prose ("the INSERT into transactions, and ...") -- not a statement.
			case regexp.MustCompile(`(?i)\bcreated_at\b`).MatchString(m[2]):
				t.Errorf("%s: an INSERT INTO transactions names created_at -- the classification's W is the database's "+
					"write instant and nothing else may set it (ADR 0032 §A.6): %q", rel, strings.Join(strings.Fields(m[0]), " "))
			default:
				if strings.HasPrefix(rel, filepath.Join("db", "queries")) {
					queries++
				}
			}
		}
	}
	for _, dir := range []string{filepath.Join(root, "db", "queries"), filepath.Join(root, "db", "migrations")} {
		files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
		if err != nil || len(files) == 0 {
			t.Fatalf("PREMISE: no SQL under %s (%v)", dir, err)
		}
		for _, f := range files {
			scan(f, true)
		}
	}
	goFiles := 0
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
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			goFiles++
			scan(path, false)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if queries < 3 || goFiles < 100 {
		t.Errorf("PREMISE: %d column lists in db/queries (want the three writers at least), %d Go files scanned", queries, goFiles)
	}
}
