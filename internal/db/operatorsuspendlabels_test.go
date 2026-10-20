package db

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"testing"
)

// opOperatorReasonLabels is the operator screen's copy of the closed reason set (M10 OP-15 C; ADR
// 0025 §1, "Kopyalar" -- the fifth copy): the keys of internal/handler/operator's
// suspensionReasonWords, READ FROM THAT FILE'S SOURCE and resolved through this package's typed
// constants, sorted. It is read rather than imported because this test package cannot import
// internal/handler/operator (that package imports this one). Each key must be a selector of this
// package (db.<Name>) naming a SuspensionReason constant; anything else -- a string literal, a
// constant of another type, a computed key -- is a failure, not a skipped entry.
// TestOperator00063_TheReasonSetIsOneSetInEveryCopy compares the result with the CHECK.
func opOperatorReasonLabels(t *testing.T) []string {
	t.Helper()
	fset, files := opDBPackageFiles(t)
	consts, err := opTypedConstValues(fset, files, importer.ForCompiler(fset, "source", nil), "SuspensionReason")
	if err != nil {
		t.Fatalf("package db: %v", err)
	}
	path := filepath.Join("..", "handler", "operator", "suspension.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var lit *ast.CompositeLit
	found := 0
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if name.Name == "suspensionReasonWords" && i < len(vs.Values) {
				found++
				lit, _ = vs.Values[i].(*ast.CompositeLit)
			}
		}
		return true
	})
	if found != 1 || lit == nil {
		t.Fatalf("%s: %d declaration(s) of suspensionReasonWords as a composite literal, want one", path, found)
	}
	var out []string
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		sel, isSel := kv.Key.(*ast.SelectorExpr)
		if !ok || !isSel {
			t.Errorf("%s: a key of suspensionReasonWords is not a selector (db.<Name>)", path)
			continue
		}
		pkg, _ := sel.X.(*ast.Ident)
		v, known := consts["SuspensionReason"][sel.Sel.Name]
		if pkg == nil || pkg.Name != "db" || !known {
			t.Errorf("%s: the key %s of suspensionReasonWords is not a SuspensionReason constant of this package", path, sel.Sel.Name)
			continue
		}
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}
