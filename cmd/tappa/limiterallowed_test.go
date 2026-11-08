package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestLimiterAllowed_IsCalledByNoProductionCode pins backlog T90's shape: a budget decides in ONE
// locked step (httpx.Limiter.TryCharge or Reserve), never "Allowed now, Charge later" -- two locked
// steps that simultaneous requests all pass. Production code makes no call to a method named Allowed
// (the 2026-11-07 audit's grep: 0); a test still may (the twin in TestLimiter_ReserveDecidesAsAllowedThenChargeDid).
//
// It is a name scan over every non-test .go file under cmd/ and internal/, without types: a second
// type with an Allowed method would trip it too, and would then have to be named here. WHAT IT DOES
// NOT CATCH: a method value taken without a call (f := l.Allowed), and a two-step shape spelled
// with other names.
func TestLimiterAllowed_IsCalledByNoProductionCode(t *testing.T) {
	scanned := 0
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			scanned++
			ast.Inspect(f, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Allowed" {
						t.Errorf("%s: a call to Allowed -- decide with TryCharge or Reserve (backlog T90)", fset.Position(c.Pos()))
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scanned < 100 {
		t.Fatalf("anti-vacuity: scanned %d production files", scanned)
	}
}
