package policy

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// validJSON is a well-formed baseline-style document reused across parse tests.
const validJSON = `{
  "version": "2026-07-24",
  "name": "GPS-only taps need review",
  "statements": [
    {
      "sid": "gps-only-review",
      "effect": "review",
      "action": ["tap:record"],
      "resource": ["location/*"],
      "condition": { "Bool": { "tap:ipMatch": false, "tap:gpsMatch": true } },
      "reason": "verified via GPS only — no network proof of place"
    }
  ]
}`

func TestParse_Valid(t *testing.T) {
	doc, err := Parse([]byte(validJSON), DefaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Version != "2026-07-24" || doc.Name == "" || len(doc.Statements) != 1 {
		t.Fatalf("parsed document mismatch: %+v", doc)
	}
	st := doc.Statements[0]
	if st.Sid != "gps-only-review" || st.Effect != EffectReview {
		t.Fatalf("statement mismatch: %+v", st)
	}
	if len(st.Action) != 1 || st.Action[0] != ActionTapRecord {
		t.Fatalf("action mismatch: %+v", st.Action)
	}
	// The Bool condition must survive parsing with its two predicates.
	args, ok := st.Condition[OpBool]
	if !ok || len(args) != 2 {
		t.Fatalf("condition mismatch: %+v", st.Condition)
	}
}

func TestParseAndValidate_Valid(t *testing.T) {
	if _, err := ParseAndValidate([]byte(validJSON), LayerBaseline, DefaultLimits()); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
}

// TestParseAndValidate_NullIsEmpty proves `null` — syntactically valid JSON that
// Parse accepts as a zero Document — is caught by Validate, not silently passed.
func TestParseAndValidate_NullIsEmpty(t *testing.T) {
	if _, err := Parse([]byte(`null`), DefaultLimits()); err != nil {
		t.Fatalf("Parse should accept syntactically-valid null: %v", err)
	}
	if _, err := ParseAndValidate([]byte(`null`), LayerTenant, DefaultLimits()); err == nil {
		t.Fatal("ParseAndValidate accepted an empty (null) document")
	}
}

func TestParse_ByteLimit(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxDocumentBytes = len(validJSON) // exactly at the limit

	if _, err := Parse([]byte(validJSON), lim); err != nil {
		t.Fatalf("document exactly at the byte limit was rejected: %v", err)
	}

	// One byte over: pad with a trailing space (still valid JSON shape-wise, but
	// the size guard must fire before decoding).
	over := validJSON + " "
	lim.MaxDocumentBytes = len(over) - 1
	_, err := Parse([]byte(over), lim)
	if err == nil {
		t.Fatal("document one byte over the limit was accepted")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != fieldDocument {
		t.Fatalf("want document-size ValidationError, got %v", err)
	}
}

func TestParse_Malformed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"truncated", `{"version":"1","name":"x","statements":[`},
		{"not an object", `["a","b"]`},
		{"garbage", `{@!#`},
		{"unknown field", `{"version":"1","name":"x","statements":[],"bogus":true}`},
		{"typoed field silently drops effect", `{"version":"1","name":"x","statements":[{"sid":"s","efect":"allow"}]}`},
		{"trailing data", validJSON + ` {"more":1}`},
		{"wrong type for statements", `{"version":"1","name":"x","statements":"nope"}`},
		{"condition not object", `{"version":"1","name":"x","statements":[{"sid":"s","effect":"allow","action":["tap:record"],"resource":["*"],"condition":"nope"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.raw), DefaultLimits()); err == nil {
				t.Fatalf("malformed input %q was accepted", tc.raw)
			}
		})
	}
}

// FuzzParse asserts Parse never panics on arbitrary bytes and that anything it
// accepts is a well-formed Document (same robustness contract as sun.FuzzParse).
// Short round: go test -run=xxx -fuzz=FuzzParse -fuzztime=20s ./internal/policy/...
func FuzzParse(f *testing.F) {
	f.Add([]byte(validJSON))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`{`))
	f.Add([]byte(`{"version":"1","name":"n","statements":[]}`))
	f.Add([]byte(`{"statements":[{"sid":"s","effect":"deny","action":["x"],"resource":["*"],"condition":{"Bool":{"k":true}}}]}`))
	f.Add([]byte(`{"version":"1","name":"n","statements":[{"condition":{"IpInPrefix":{"k":["zz"]}}}]}`))
	f.Add([]byte(`[1,2,3]`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		doc, err := Parse(raw, DefaultLimits())
		if err != nil {
			return // rejected inputs prove nothing beyond "did not panic"
		}
		// Accepted: Validate must also not panic on it, whatever the verdict.
		_ = Validate(doc, LayerTenant, DefaultLimits())
	})
}

// TestActions_CoverEveryDeclaredActionConstant holds allActions (and therefore
// Actions() and validActions, which are both derived from it) in step with the
// const block that declares the vocabulary.
//
// 🔴 IT PARSES THE SOURCE RATHER THAN LISTING THE NAMES, which is the whole point:
// a test that repeated the seven identifiers would be a FOURTH copy of the
// vocabulary and would go green on the day somebody added an eighth constant and
// updated neither. go/ast asks the file what it declares.
//
// BOTH DIRECTIONS ARE CHECKED. A constant missing from the slice is an authority
// the engine accepts but the panel never shows; a slice entry that is not a
// declared constant is a name that cannot be produced by a document.
func TestActions_CoverEveryDeclaredActionConstant(t *testing.T) {
	declared := declaredConstsOfType(t, "document.go", "Action")
	// ANTI-VACUITY: a walk that found nothing would agree with any slice at all.
	if len(declared) < 5 {
		t.Fatalf("the AST walk found %d Action constant(s) in document.go; there are more, "+
			"so it is reading the wrong thing", len(declared))
	}
	inSlice := map[string]bool{}
	for _, a := range Actions() {
		inSlice[string(a)] = true
	}
	for name, value := range declared {
		if !inSlice[value] {
			t.Errorf("%s (%q) is declared as an Action but is absent from allActions. "+
				"Valid() would reject it and the panel would never list the authority.",
				name, value)
		}
	}
	byValue := map[string]bool{}
	for _, v := range declared {
		byValue[v] = true
	}
	for _, a := range Actions() {
		if !byValue[string(a)] {
			t.Errorf("allActions carries %q, which no Action constant in document.go "+
				"declares", a)
		}
	}
	if got, want := len(Actions()), len(declared); got != want {
		t.Errorf("Actions() returns %d entries, document.go declares %d Action constants; "+
			"a duplicate in the slice would satisfy both loops above and still skew a count",
			got, want)
	}
	// The copy must be a copy: a caller that mutates what it gets back must not be
	// able to reach into the package's own vocabulary.
	first := Actions()
	first[0] = "mutated"
	if Actions()[0] == "mutated" {
		t.Error("Actions() handed out the package's own slice; a caller can rewrite the " +
			"action vocabulary")
	}
}

// declaredConstsOfType returns name -> string value for every const of the named
// type declared in one file of this package.
func declaredConstsOfType(t *testing.T, file, typeName string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			id, ok := vs.Type.(*ast.Ident)
			if !ok || id.Name != typeName {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquoting %s: %v", name.Name, err)
				}
				out[name.Name] = value
			}
		}
	}
	return out
}
