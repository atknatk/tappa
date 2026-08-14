package adminauth

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// query_test.go -- CLAUDE.md §4.5's SECOND defence, for the queries THIS package names.
//
// 🔴 IT EXISTS BECAUSE AN AUDIT MEASURED THAT NOBODY WAS READING THEM. Three copies of
// this scanner live under internal/domain/{ledger,review,tenant} and each derives the
// queries ITS OWN package names -- so `db/queries/passwordresets.sql` and
// `db/queries/admins.sql`, whose only Go callers are here, were scanned by NOTHING.
// `grep -rn 'passwordresets.sql' --include='*_test.go' .` returned 0.
//
// ⚠️ IT ALSO CORRECTS A SENTENCE IN db/queries/passwordresets.sql. That file gave two
// reasons for writing CreatePasswordReset as INSERT ... SELECT, the first being "the
// §4.5 belt scanner needs a predicate to read". That reason was FALSE FOR THAT FILE at
// the time it was written -- no scanner read it. It is true now, which is the honest
// way to make a justification true: build the thing it appeals to. (The second reason,
// which that file calls "the real one", was true all along: the INSERT cannot happen
// unless the caller's admin row is visible and ACTIVE under RLS.)
//
// WHAT IT DOES: parse THIS package with go/ast, collect every selector whose name is a
// query declared in db/queries, read that query's SQL body, and require an explicit
// top-level `tenant_id = @tenant_id` conjunct in at least one WHERE clause. RLS is the
// first defence; this is the belt for the code path where the RLS context was never
// established (ADR 0002 madde 4).
//
// AN AST, NOT A REGEX, and the reason is a measurement recorded in the three sibling
// copies: `\.([A-Z]\w+)\(` was beaten three ways in one audit (a newline between the
// dot and the name -- which gofmt does NOT join -- a space before the parenthesis, and
// taking the method as a value). All three are *ast.SelectorExpr nodes, so walking
// selectors closes all three.
//
// ⚠️ THE LIMITS, named rather than implied, and they are the siblings' limits verbatim:
// a call made through reflect.MethodByName is a STRING and is invisible here, and a
// query called on this package's behalf by a helper in ANOTHER package is not in this
// AST at all.
//
// 🔴 THE CONTEXT-LESS RESOLVERS ARE EXEMPT AND THE EXEMPTION IS EXPLICIT, not an
// accident of the scan: GetPasswordResetByTokenHash and the two admin lookups carry no
// tenant predicate BECAUSE THE TENANT IS THE RESULT (ADR 0002 madde 7). They are not
// sqlc queries, so they are not declared in db/queries and cannot appear in the list
// below -- but a reader must not conclude from a green test that every lookup here is
// tenant-scoped.

// tenantParam is the sqlc parameter every scoped query must compare tenant_id against.
const tenantParam = "@tenant_id"

// TestAdminAuthQueries_CarryAnExplicitTenantPredicate is the belt test itself.
func TestAdminAuthQueries_CarryAnExplicitTenantPredicate(t *testing.T) {
	declared := declaredQueries(t)
	names := storeQueryNames(t, declared)
	if len(names) == 0 {
		t.Fatal("this package names no sqlc query; the derivation is reading nothing")
	}
	// The three the reset flow introduced must be among them, or a rename has quietly
	// taken this file out of the loop.
	for _, must := range []string{"CreatePasswordReset", "ConsumePasswordResetAndSetPassword"} {
		found := false
		for _, n := range names {
			if n == must {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is not among the queries this package names (%v) -- the scan is not "+
				"covering the reset flow", must, names)
		}
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			file, body, ok := findQuery(t, name)
			if !ok {
				t.Fatalf("%s is declared somewhere but its body could not be read back", name)
			}
			clauses := whereClauses(body)
			if len(clauses) == 0 {
				t.Fatalf("%s (%s) has no WHERE clause at all: there is nothing for the §4.5 "+
					"belt to check", name, file)
			}
			// 🔴 EVERY CLAUSE, NOT "AT LEAST ONE" — AND THAT IS A DEVIATION FROM THE
			// THREE SIBLING COPIES, MADE BECAUSE A MUTATION WALKED THROUGH THEIR RULE.
			// Those files check `if any clause is scoped: pass`, which is sound for a
			// single-statement query and hollow for a multi-statement CTE. Measured on
			// this package: deleting `AND a.tenant_id = @tenant_id` from
			// CreatePasswordReset's INSERT ... SELECT -- leaving RLS as the only defence
			// on the statement that actually writes the row -- left the "any clause"
			// version GREEN, because the `retired` CTE's own WHERE still carried one.
			//
			// ⚠️ THE SIBLINGS ARE NOT CHANGED HERE and that is a named limit rather than
			// an oversight: none of the queries they cover is a multi-statement CTE
			// today (checked), so the weakness is latent there, and rewriting three
			// files in another milestone's packages is not this task's change.
			for _, c := range clauses {
				if scopedByTenant(c) || propagatesTenantScope(c) {
					continue
				}
				t.Errorf("%s (%s) has a WHERE clause that is neither scoped by `%s` nor "+
					"joined to an already-scoped relation on tenant_id; on that statement RLS "+
					"would be the ONLY defence. Clause: %q",
					name, file, tenantParam, c)
			}
		})
	}
}

// TestAdminAuthScopeCheck_IsNotVacuous is the negative control for the MATCHER, and
// TestAdminAuthQueryBody_IsBounded below is the one for the READER. The siblings carry
// both because an audit found only the first, and blinding the reader to return the
// whole file left the belt test green.
func TestAdminAuthScopeCheck_IsNotVacuous(t *testing.T) {
	unscoped := map[string]string{
		"no predicate at all":               "r.id = @id",
		"neutralised by OR":                 "(r.tenant_id = @tenant_id OR TRUE)",
		"OR at the top level":               "r.tenant_id = @tenant_id OR 1 = 1",
		"a literal, not the caller":         "r.tenant_id = '00000000-0000-0000-0000-000000000000'",
		"another column, not the parameter": "r.tenant_id = a.tenant_id",
		"only in a RETURNING list":          "r.id = @id RETURNING r.tenant_id = @tenant_id",
	}
	for why, where := range unscoped {
		if scopedByTenant(where) {
			t.Errorf("the scope check ACCEPTED %q (%s), which scopes nothing to the caller's tenant", where, why)
		}
	}
	scoped := map[string]string{
		"plain":            "r.tenant_id = @tenant_id",
		"unaliased":        "tenant_id = @tenant_id",
		"reversed":         "@tenant_id = r.tenant_id",
		"cast":             "r.tenant_id = @tenant_id::uuid",
		"second conjunct":  "r.id = @id AND r.tenant_id = @tenant_id",
		"parenthesised OR": "(r.used_at IS NULL OR r.cancelled_at IS NULL) AND r.tenant_id = @tenant_id",
	}
	for why, where := range scoped {
		if !scopedByTenant(where) {
			t.Errorf("the scope check REJECTED %q (%s), which does scope to the caller's tenant", where, why)
		}
	}
}

// TestAdminAuthQueryBody_IsBounded is the negative control for the READER: the body of
// one query must not bleed into the next, or a neighbour's tenant predicate would
// satisfy this test on behalf of a query that has none.
func TestAdminAuthQueryBody_IsBounded(t *testing.T) {
	_, body, ok := findQuery(t, "CreatePasswordReset")
	if !ok {
		t.Fatal("CreatePasswordReset not found")
	}
	if strings.Contains(body, "ConsumePasswordResetAndSetPassword") ||
		strings.Contains(strings.ToUpper(body), "LIST LIVE") {
		t.Fatalf("the reader ran past the end of the query: %q", body)
	}
	if !strings.Contains(body, "INSERT INTO password_resets") {
		t.Fatalf("the reader returned something that is not this query: %q", body)
	}
	// Comment lines must be stripped, or a tenant_id mentioned only in prose would
	// satisfy the belt.
	if strings.Contains(body, "-- name:") || strings.Contains(body, "🔴") {
		t.Fatalf("comment lines survived into the body: %q", body)
	}
}

// ------------------------------------------------------------------ helpers --
//
// These are the sibling copies' helpers, kept BYTE-FOR-BYTE equivalent on purpose: the
// three existing files say the four copies must not drift, because a matcher that is
// stricter in one package than another turns "the belt is checked" into a claim that
// depends on which package a query happens to be called from.

func whereClauses(body string) []string {
	var out []string
	whereRE := regexp.MustCompile(`(?is)\bWHERE\b`)
	stopRE := regexp.MustCompile(`(?is)\b(ORDER\s+BY|GROUP\s+BY|LIMIT|HAVING|RETURNING|FOR\s+NO\s+KEY\s+UPDATE|FOR\s+UPDATE|FOR\s+SHARE)\b`)
	for _, loc := range whereRE.FindAllStringIndex(body, -1) {
		rest := body[loc[1]:]
		depth, end := 0, len(rest)
		for i := 0; i < len(rest); i++ {
			if rest[i] == '(' {
				depth++
				continue
			}
			if rest[i] == ')' {
				if depth == 0 {
					end = i
					break
				}
				depth--
			}
		}
		clause := rest[:end]
		if k := stopRE.FindStringIndex(clause); k != nil {
			clause = clause[:k[0]]
		}
		clause = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(clause), ";"))
		if clause != "" {
			out = append(out, clause)
		}
	}
	return out
}

func scopedByTenant(where string) bool {
	for _, conjunct := range splitTopLevelAnd(where) {
		if isScopeEquality(conjunct) {
			return true
		}
	}
	return false
}

// propagatesTenantScope reports whether one top-level conjunct ties this statement's
// tenant to ANOTHER relation's tenant -- `p.tenant_id = c.tenant_id`, the shape a
// data-modifying CTE uses to inherit the scope its predecessor already established.
//
// IT IS DELIBERATELY NARROWER THAN "mentions tenant_id": both sides must be a
// tenant_id column, so `r.tenant_id = a.id` or a literal does not qualify. It is also
// the reason scopedByTenant stays strict -- a clause that ONLY propagates is accepted
// here because some clause upstream of it carried the caller's parameter, and the
// whole-query rule above is what makes that true rather than assumed.
func propagatesTenantScope(where string) bool {
	col := `(?:[a-z_][a-z0-9_]*\.)?tenant_id`
	re := regexp.MustCompile(`(?i)^` + col + ` = ` + col + `$`)
	for _, conjunct := range splitTopLevelAnd(where) {
		c := strings.TrimSpace(conjunct)
		c = regexp.MustCompile(`::\s*[a-zA-Z_]+`).ReplaceAllString(c, "")
		c = strings.Trim(c, "() \t\n")
		c = strings.Join(strings.Fields(c), " ")
		if re.MatchString(c) {
			return true
		}
	}
	return false
}

func splitTopLevelAnd(s string) []string {
	var parts []string
	depth, last := 0, 0
	upper := strings.ToUpper(s)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 {
			continue
		}
		if strings.HasPrefix(upper[i:], "AND") && wordBoundary(upper, i, 3) {
			parts = append(parts, s[last:i])
			last = i + 3
		}
	}
	return append(parts, s[last:])
}

func wordBoundary(s string, i, n int) bool {
	before := i == 0 || !isWordByte(s[i-1])
	after := i+n >= len(s) || !isWordByte(s[i+n])
	return before && after
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func isScopeEquality(conjunct string) bool {
	c := strings.TrimSpace(conjunct)
	c = regexp.MustCompile(`::\s*[a-zA-Z_]+`).ReplaceAllString(c, "")
	c = strings.Trim(c, "() \t\n")
	c = strings.Join(strings.Fields(c), " ")

	col := `(?:[a-z_][a-z0-9_]*\.)?tenant_id`
	param := regexp.QuoteMeta(tenantParam)
	forward := regexp.MustCompile(`(?i)^` + col + ` = ` + param + `$`)
	reverse := regexp.MustCompile(`(?i)^` + param + ` = ` + col + `$`)
	return forward.MatchString(c) || reverse.MatchString(c)
}

func queryMarker(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^-- name: ` + regexp.QuoteMeta(name) + ` `)
}

func queryBody(t *testing.T, path, name string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	loc := queryMarker(name).FindStringIndex(string(raw))
	if loc == nil {
		return "", false
	}
	rest := string(raw)[loc[1]:]
	if j := regexp.MustCompile(`(?m)^-- name: `).FindStringIndex(rest); j != nil {
		rest = rest[:j[0]]
	}
	var body []string
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		body = append(body, line)
	}
	return strings.Join(body, "\n"), true
}

func queriesDir() string { return filepath.Join("..", "..", "db", "queries") }

func findQuery(t *testing.T, name string) (file, body string, ok bool) {
	t.Helper()
	entries, err := os.ReadDir(queriesDir())
	if err != nil {
		t.Fatalf("reading db/queries: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if b, found := queryBody(t, filepath.Join(queriesDir(), e.Name()), name); found {
			return e.Name(), b, true
		}
	}
	return "", "", false
}

func declaredQueries(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(queriesDir())
	if err != nil {
		t.Fatalf("reading db/queries: %v", err)
	}
	names := map[string]bool{}
	re := regexp.MustCompile(`(?m)^-- name: (\w+) `)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(queriesDir(), e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			names[m[1]] = true
		}
	}
	if len(names) < 10 {
		t.Fatalf("found %d query name(s) in db/queries; the product has more, so this scan is "+
			"reading the wrong directory and would filter everything out", len(names))
	}
	return names
}

func storeQueryNames(t *testing.T, declared map[string]bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("go/ast parsed no non-test package here; the derivation is reading nothing")
	}
	seen := map[string]bool{}
	var out []string
	files := 0
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			files++
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name := sel.Sel.Name
				if declared[name] && !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
				return true
			})
		}
	}
	if files == 0 {
		t.Fatal("no non-test Go file in this package -- the derivation is reading nothing")
	}
	return out
}
