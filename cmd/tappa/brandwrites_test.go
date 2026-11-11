package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// brandWriteMethods are the store's five writes of tenant_branding. Each one takes a tenant id and
// values and checks nothing else: the rules live above them, in internal/domain/tenant/brand.go, which
// re-checks the colour (brand.Check), refuses a logo that is not Normalized(), asks the database who
// the actor is (mayEditBrand) and writes the audit row. A second caller skips all four, and
// storeSurface only holds the methods' signatures, so it does not see one.
var brandWriteMethods = []string{
	"ClearTenantAccent", "ClearTenantLogo", "EnsureTenantBrand", "SetTenantAccent", "SetTenantLogo",
}

// brandWriteCallSites is the whole permission: file -> method -> references. The four saves and
// clears each pass one closure to Brands.write, which calls EnsureTenantBrand once.
var brandWriteCallSites = map[string]map[string]int{
	"internal/domain/tenant/brand.go": {
		"ClearTenantAccent": 1, "ClearTenantLogo": 1, "EnsureTenantBrand": 1, "SetTenantAccent": 1, "SetTenantLogo": 1,
	},
}

// brandWriteSQLRE is a statement that writes the table, in SQL or in a Go string.
var brandWriteSQLRE = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM|MERGE\s+INTO|TRUNCATE(?:\s+TABLE)?|COPY)\s+(?:ONLY\s+)?(?:"?public"?\.)?"?tenant_branding\b`)

// brandWriteUses reads one Go file and returns how many times it names each write, and the string
// literals that carry a statement writing the table. It counts every selector and not only calls:
// a method value (w := q.SetTenantLogo) or a method expression ((*store.Queries).SetTenantLogo) is
// the same write with the call one step later, and a consumer-side interface still ends in a selector.
func brandWriteUses(path string, src []byte) (map[string]int, []string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, err
	}
	uses := map[string]int{}
	var statements []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if slices.Contains(brandWriteMethods, x.Sel.Name) {
				uses[x.Sel.Name]++
			}
		case *ast.BasicLit:
			if x.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(x.Value); err == nil && brandWriteSQLRE.MatchString(s) {
				statements = append(statements, strings.Join(strings.Fields(s), " "))
			}
		}
		return true
	})
	return uses, statements, nil
}

// brandQueryWriters answers which named queries of one .sql text write the table; a write under no
// name comes back with an empty name.
func brandQueryWriters(sqlText string) []string {
	var out []string
	for _, q := range splitNamedQueries(sqlText) {
		if brandWriteSQLRE.MatchString(stripSQLComments(q.body)) {
			out = append(out, q.name)
		}
	}
	return out
}

// TestBrandWrites_TheScannersSeeWhatTheyAreFor drives the two readers with the shapes they exist to
// catch and the ones they must leave alone, because the tree holds only the shapes it holds today.
func TestBrandWrites_TheScannersSeeWhatTheyAreFor(t *testing.T) {
	t.Parallel()
	const header = "package x\nfunc f() {\n"
	for _, tc := range []struct {
		name, body string
		method     string
		statement  bool
	}{
		{"a call", `q.SetTenantLogo(ctx, p)`, "SetTenantLogo", false},
		{"a method value", `w := q.ClearTenantAccent; _ = w`, "ClearTenantAccent", false},
		{"a method expression", `w := (*store.Queries).EnsureTenantBrand; _ = w`, "EnsureTenantBrand", false},
		{"raw SQL", "tx.Exec(ctx, `UPDATE tenant_branding SET accent = NULL`)", "", true},
		{"raw SQL, another spacing", "tx.Exec(ctx, \"insert\\n into  public.tenant_branding (tenant_id) VALUES ($1)\")", "", true},
		{"a quoted name", "tx.Exec(ctx, `UPDATE \"tenant_branding\" SET accent = NULL`)", "", true},
		{"a quoted schema and name", "tx.Exec(ctx, `UPDATE \"public\".\"tenant_branding\" SET accent = NULL`)", "", true},
		{"a bulk write", "tx.Exec(ctx, `COPY tenant_branding FROM STDIN`)", "", true},
		{"a bulk write, quoted", "tx.Exec(ctx, `COPY \"tenant_branding\" (tenant_id) FROM STDIN`)", "", true},
		{"the params struct is no write", `_ = store.SetTenantLogoParams{}`, "", false},
		{"a read is no write", "tx.Query(ctx, `SELECT accent FROM tenant_branding`)", "", false},
		{"a word in a string is no write", `_ = "SetTenantLogo"`, "", false},
		{"another method", `q.GetTenantBrandForUpdate(ctx, id)`, "", false},
		{"a comment", "// q.SetTenantLogo(ctx, p), UPDATE tenant_branding\n_ = 1", "", false},
	} {
		uses, statements, err := brandWriteUses("x.go", []byte(header+tc.body+"\n}\n"))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.method == "" && len(uses) != 0 || tc.method != "" && uses[tc.method] != 1 {
			t.Errorf("%s: uses %v, want one of %q", tc.name, uses, tc.method)
		}
		if (len(statements) > 0) != tc.statement {
			t.Errorf("%s: statements %q, want a statement = %v", tc.name, statements, tc.statement)
		}
	}
	const queries = `-- UPDATE tenant_branding in the file's header comment
-- name: Reads :one
SELECT accent FROM tenant_branding WHERE tenant_id = $1;
-- name: Writes :exec
-- a comment
UPDATE tenant_branding SET accent = NULL;
`
	if got := brandQueryWriters(queries); !slices.Equal(got, []string{"Writes"}) {
		t.Errorf("brandQueryWriters: %q, want only Writes", got)
	}
	if got := brandQueryWriters("DELETE FROM tenant_branding;\n-- name: Reads :one\nSELECT 1;\n"); !slices.Equal(got, []string{""}) {
		t.Errorf("brandQueryWriters, a write under no name: %q", got)
	}
}

// TestBrandWrites_TheCallSitesAreTheOnesInventoried holds the tree to brandWriteCallSites, both ways:
// a new file naming a write is surplus, a write leaving brand.go is missing. It reads non-test Go only
// (internal/db's and internal/domain/tenant's fixtures seed a brand on purpose) and skips internal/store,
// which declares the methods.
//
// A LIMIT: a statement built at run time, or split across a Sprintf, is not a literal this reads, and
// tappa_app holds the column grants (00028), so the database would not refuse it either.
func TestBrandWrites_TheCallSitesAreTheOnesInventoried(t *testing.T) {
	t.Parallel()

	got := map[string]map[string]int{}
	files := 0
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(repoRoot, path)
		if rerr != nil {
			return rerr
		}
		if info.IsDir() {
			if skipGoDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.HasPrefix(filepath.ToSlash(rel), "internal/store/") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		uses, statements, perr := brandWriteUses(path, src)
		if perr != nil {
			return fmt.Errorf("%s will not parse, so nothing in it was scanned: %w", rel, perr)
		}
		files++
		rel = filepath.ToSlash(rel)
		if len(uses) > 0 {
			got[rel] = uses
		}
		for _, s := range statements {
			t.Errorf("%s writes tenant_branding in its own SQL (%q). The writes are db/queries/branding.sql's "+
				"five, and only internal/domain/tenant/brand.go calls them", rel, s)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if files < 100 {
		t.Fatalf("only %d Go files read; the walk is not reaching the tree", files)
	}

	for file, want := range brandWriteCallSites {
		if have := got[file]; !maps.Equal(have, want) {
			t.Errorf("%s names the brand writes %v; the inventory records %v. If the write moved, move this "+
				"entry with it", file, have, want)
		}
	}
	for file, have := range got {
		if _, ok := brandWriteCallSites[file]; !ok {
			t.Errorf("%s names a brand write %v and is NOT inventoried. Calling one directly skips the colour "+
				"gate, Normalized(), the owner check and the audit row: go through tenant.Brands", file, have)
		}
	}
	// The inventory's size is ratcheted for the reason TestResolverAccess_TheCallSitesAreTheOnesInventoried
	// gives: one added line must not be all it takes for a second caller to pass.
	if len(brandWriteCallSites) != 1 {
		t.Errorf("brandWriteCallSites holds %d files; one was the whole permission when this was set "+
			"(T92). Say which caller and why, and change this number in the same edit", len(brandWriteCallSites))
	}

	// A new query beside the five writes the table under a name the walk above does not know.
	queryFiles, err := filepath.Glob(filepath.Join(repoRoot, "db", "queries", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(queryFiles) < 10 {
		t.Fatalf("only %d query files read; the glob is not reaching db/queries", len(queryFiles))
	}
	var writers []string
	for _, p := range queryFiles {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		writers = append(writers, brandQueryWriters(string(raw))...)
	}
	slices.Sort(writers)
	if !slices.Equal(writers, brandWriteMethods) {
		t.Errorf("db/queries writes tenant_branding under %q; the guarded five are %q. A sixth write needs "+
			"its name in brandWriteMethods and its caller in brandWriteCallSites", writers, brandWriteMethods)
	}
}
