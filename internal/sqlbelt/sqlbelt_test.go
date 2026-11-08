package sqlbelt

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestUnreadable_NamesEveryFormItCannotRead pins the fail-closed half of T164: each form below
// can hide structure from this lexer, so it must be NAMED (and the belt then counts the statement
// as unscoped), and each readable control must not be.
func TestUnreadable_NamesEveryFormItCannotRead(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string // a substring of the form named; "" = readable
	}{
		{"E-string", `x <> E'a\'' OR true`, "E'...'"},
		{"lower-case e-string", `x <> e'a'`, "E'...'"},
		{"dollar quote", `x <> $$a$$`, "dollar"},
		{"tagged dollar quote", `x <> $q$a$q$`, "dollar"},
		{"block comment", `x = 1 /* c */`, "block comment"},
		{"quoted identifier holding a quote", `e."it's" IS NULL`, "quoted identifier"},
		{"quoted identifier holding a space", `e."a b" IS NULL`, "quoted identifier"},
		{"quoted identifier holding a doubled quote", `e."a""b" IS NULL`, "quoted identifier"},
		{"empty quoted identifier", `e."" IS NULL`, "quoted identifier"},
		{"unterminated literal", `x <> 'abc`, "unterminated string"},
		{"unterminated identifier", `e."abc`, "unterminated quoted identifier"},

		{"plain literal", `x <> 'a(b) OR c'`, ""},
		{"doubled quote in a literal", `x <> 'it''s'`, ""},
		{"backslash in a plain literal", `x <> 'a\'`, ""},
		{"line comment holding every form", "x = 1 -- E'a' $$ /* \"it's\"\nAND y = 2", ""},
		{"plain quoted identifier", `e."tenant_id" = @tenant_id`, ""},
		{"positional parameter", `e.id = $1`, ""},
		{"dollar inside an identifier", `e.a$b = 1`, ""},
		{"E as the last letter of a word before a literal", `x = somE'a'`, ""},
		{"block-comment opener inside a literal", `x <> '/*'`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Unreadable(tt.sql)
			if tt.want == "" && got != "" {
				t.Fatalf("Unreadable(%q) = %q, want readable", tt.sql, got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("Unreadable(%q) = %q, want a form naming %q", tt.sql, got, tt.want)
			}
		})
	}
}

// TestMask_BlanksLiteralsAndCommentsAndKeepsOffsets: the belts print clauses and slice the
// original body by offsets read off the masked one, so length and line breaks must survive.
func TestMask_BlanksLiteralsAndCommentsAndKeepsOffsets(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"literal", `a <> '(x) OR y'`, `a <> '` + strings.Repeat(" ", len("(x) OR y")) + `'`},
		{"doubled quote", `a <> 'it''s' AND b`, `a <> '     ' AND b`},
		{"line comment", "a = 1 -- ( OR\nAND b", "a = 1        \nAND b"},
		{"multi-line literal keeps its line break", "a <> 'x\ny' OR b", "a <> ' \n ' OR b"},
		{"quoted identifier untouched", `e."full_name" = 'z'`, `e."full_name" = ' '`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Mask(tt.in)
			if got != tt.want {
				t.Fatalf("Mask(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if len(got) != len(tt.in) {
				t.Fatalf("Mask changed the length: %d -> %d", len(tt.in), len(got))
			}
		})
	}
}

// TestPrepare_RefusesWhatItCannotRead: the entry point the six belts call returns an error (the
// belt then reports the statement as unscoped) exactly when Unreadable names a form.
func TestPrepare_RefusesWhatItCannotRead(t *testing.T) {
	if _, err := Prepare(`SELECT 1 WHERE a = $$x$$`); err == nil || !strings.Contains(err.Error(), "UNSCOPED") {
		t.Fatalf("Prepare accepted a dollar quote: %v", err)
	}
	got, err := Prepare(`SELECT 1 WHERE a = 'x' -- c`)
	if err != nil {
		t.Fatalf("Prepare refused readable SQL: %v", err)
	}
	if got != `SELECT 1 WHERE a = ' '     ` {
		t.Fatalf("Prepare did not mask: %q", got)
	}
}

// TestHasTopLevelOr_ReadsQuotesAndFailsClosed is the clause-level matcher's table: an OR inside
// a literal or comment is not one, a paren inside a literal is not one, and a clause written in a
// form the lexer cannot read answers true. The last group is the clause-level guard's own proof:
// these clauses never pass through Prepare when a matcher is driven directly.
func TestHasTopLevelOr_ReadsQuotesAndFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"top-level OR", "a = @tenant_id OR true", true},
		{"OR after the scope conjunct", "a = @tenant_id AND b OR c", true},
		{"parenthesised OR", "a = @tenant_id AND (b OR c)", false},
		{"OR inside a literal", "a = @tenant_id AND b <> 'x OR y'", false},
		{"OR inside a line comment", "a = @tenant_id -- or\nAND b", false},
		{"word containing OR", "a = @tenant_id AND origin = 1 AND color = 2", false},
		{"a quoted ( does not open a paren", "a = @tenant_id AND b <> '(' OR true", true},
		{"a quoted ) does not close one", "a = @tenant_id AND (b <> ')' OR c)", false},
		{"a stray ) does not hide what follows", "a = @tenant_id) OR (true", true},

		{"E-string", `a = @tenant_id AND b <> E'x\'' OR true`, true},
		{"E-string with no OR at all", `a = @tenant_id AND b <> E'x'`, true},
		{"dollar quote", `a = @tenant_id AND b <> $$it's$$ OR true`, true},
		{"block comment", `a = @tenant_id /* ( */ OR true`, true},
		{"quoted identifier holding a quote", `a = @tenant_id AND e."it's" IS NULL OR true`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasTopLevelOr(tt.in); got != tt.want {
				t.Fatalf("HasTopLevelOr(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitTopLevelAnd_ReadsQuotes(t *testing.T) {
	got := SplitTopLevelAnd("a = @tenant_id AND b <> 'x AND (y' AND (c AND d)")
	if len(got) != 3 {
		t.Fatalf("split into %d piece(s), want 3: %q", len(got), got)
	}
	if strings.TrimSpace(got[0]) != "a = @tenant_id" {
		t.Fatalf("first conjunct %q", got[0])
	}
	if strings.Contains(got[1], "x") {
		t.Fatalf("a literal's contents survived into a conjunct: %q", got[1])
	}
}

// TestMaskParens_RespectsQuotes is T164's second sentence: the tenant copy's maskParens masked
// the parens inside quotes, and the OR between them with them.
func TestMaskParens_RespectsQuotes(t *testing.T) {
	in := `e.tenant_id = @tenant_id AND x <> '(' OR true AND x <> ')'`
	got := MaskParens(in)
	if len(got) != len(in) {
		t.Fatalf("length changed: %d -> %d", len(in), len(got))
	}
	if !strings.Contains(got, " OR true ") {
		t.Fatalf("the OR between two quoted parens was masked: %q", got)
	}
	if g := MaskParens("a (b OR c)\n d"); g != "a "+strings.Repeat(" ", len("(b OR c)"))+"\n d" {
		t.Fatalf("a real span was not masked: %q", g)
	}
}

func TestParenSpansAndMaskNested_ReadQuotes(t *testing.T) {
	s := `(a <> ')' AND (b))`
	spans := ParenSpans(s)
	if len(spans) != 2 || spans[1] != [2]int{0, len(s) - 1} {
		t.Fatalf("ParenSpans(%q) = %v, want the inner span then the whole", s, spans)
	}
	if got := MaskNested(`x (WHERE y) z`); got != `x (       ) z` {
		t.Fatalf("MaskNested: %q", got)
	}
}

// TestWhereClauses_FilterLiteralsAndStops pins the shared WHERE reader: the aggregate FILTER is
// skipped in both directions (T24), a WHERE inside a literal is not one, a quoted paren does not
// end a clause, and RETURNING ends one.
func TestWhereClauses_FilterLiteralsAndStops(t *testing.T) {
	texts := func(body string) []string {
		var out []string
		for _, c := range WhereClauses(body) {
			out = append(out, c.Text)
		}
		return out
	}
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"FILTER beside a real WHERE",
			"SELECT count(*) FILTER (WHERE a OR b) FROM t WHERE t.tenant_id = @tenant_id",
			[]string{"t.tenant_id = @tenant_id"}},
		{"FILTER with spacing and case",
			"SELECT count(*) filter\n ( where a ) FROM t WHERE t.id = @id",
			[]string{"t.id = @id"}},
		{"a WHERE inside a literal",
			"SELECT 'x WHERE t.tenant_id = @tenant_id LIMIT 1' FROM t",
			nil},
		{"a quoted paren does not end the clause",
			"SELECT 1 FROM t WHERE t.tenant_id = @tenant_id AND t.n <> ')' OR true",
			[]string{"t.tenant_id = @tenant_id AND t.n <> ' ' OR true"}},
		{"RETURNING ends it",
			"UPDATE t SET x = 1 WHERE id = @id RETURNING tenant_id = @tenant_id",
			[]string{"id = @id"}},
		{"a subquery's clause ends at its paren",
			"SELECT 1 FROM t WHERE t.id IN (SELECT u.id FROM u WHERE u.tenant_id = @tenant_id) AND t.tenant_id = @tenant_id",
			[]string{"t.id IN (SELECT u.id FROM u WHERE u.tenant_id = @tenant_id) AND t.tenant_id = @tenant_id", "u.tenant_id = @tenant_id"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := texts(tt.body)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("WhereClauses:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
	// At is the offset of the WHERE keyword in the original body.
	body := "SELECT 'x' FROM tenants t WHERE t.id = @tenant_id"
	cs := WhereClauses(body)
	if len(cs) != 1 || !strings.HasPrefix(body[cs[0].At:], "WHERE") {
		t.Fatalf("At does not point at the WHERE: %+v", cs)
	}
}

// TestCorpus_IsNotVacuous: the six belts drive Planted and Clean; an emptied corpus would make
// all six corpus tests pass over nothing.
func TestCorpus_IsNotVacuous(t *testing.T) {
	if len(Planted) < 10 || len(Clean) < 6 {
		t.Fatalf("corpus shrank: %d planted, %d clean", len(Planted), len(Clean))
	}
	seen := map[string]bool{}
	for _, c := range append(append([]Case(nil), Planted...), Clean...) {
		if c.Name == "" || c.SQL == "" || seen[c.Name] {
			t.Fatalf("corpus case %q is empty or duplicated", c.Name)
		}
		seen[c.Name] = true
	}
	for _, c := range Clean {
		if bad := Unreadable(c.SQL); bad != "" {
			t.Errorf("clean case %q uses %s; a clean case must be written in readable forms", c.Name, bad)
		}
	}
}

// TestBelts_AllSixRunTheSharedCorpus: the corpus only keeps the six from drifting if all six run
// it. Each belt package must carry a TestBelt_SharedLexicalCorpus and import this package.
func TestBelts_AllSixRunTheSharedCorpus(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{
		"internal/adminauth", "internal/domain/billing", "internal/domain/ledger",
		"internal/domain/review", "internal/domain/signup", "internal/domain/tenant",
	} {
		raw, err := os.ReadFile(filepath.Join(root, dir, "query_test.go"))
		if err != nil {
			t.Fatalf("reading %s/query_test.go: %v", dir, err)
		}
		src := string(raw)
		for _, must := range []string{
			"func TestBelt_SharedLexicalCorpus(",
			`"github.com/atknatk/tappa/internal/sqlbelt"`,
			"sqlbelt.Planted", "sqlbelt.Clean", "sqlbelt.Prepare(",
		} {
			if !strings.Contains(src, must) {
				t.Errorf("%s/query_test.go does not carry %s", dir, must)
			}
		}
	}
}

// TestNoProductionImport: this package is a test helper. A non-test file that imported it would
// put a belt's scanner into the product binary.
func TestNoProductionImport(t *testing.T) {
	const self = "github.com/atknatk/tappa/internal/sqlbelt"
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".tools", "android":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		files++
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == self {
				t.Errorf("%s imports %s; only _test.go files may", path, self)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
	if files < 100 {
		t.Fatalf("parsed %d non-test Go file(s); the walk is reading the wrong directory", files)
	}
}
