package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestServer_SetsNoWriteTimeoutThatCutsAConfirmation: the process's http.Server sets no
// WriteTimeout, in its literal or by assignment, anywhere in this package.
//
// WHY (WL-9 round 3, B1): once a tap's record has committed, POST /api/checkin renders
// its confirmation on a context without the request's deadline, after a brand read
// bounded by internal/handler's resultBrandWait -- so that response can end up to that
// bound after httpx.RequestTimeout. Measured on a real http.Server behind production's
// middleware order: without a WriteTimeout the late 200 reaches the client whole; with
// a WriteTimeout shorter than the handler the client gets EOF and the access record
// still says 200 -- a recorded tap whose confirmation never arrives, logged as a
// success. A WriteTimeout added later has to be longer than httpx.RequestTimeout plus
// that bound plus the render; this test stops the change until whoever makes it has
// done that arithmetic (ADR 0023, WL-9 note, Karar 2).
func TestServer_SetsNoWriteTimeoutThatCutsAConfirmation(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	fset := token.NewFileSet()
	servers := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if sel, ok := n.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Server" {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "http" {
						servers++
					}
				}
			case *ast.KeyValueExpr:
				if k, ok := n.Key.(*ast.Ident); ok && k.Name == "WriteTimeout" {
					t.Errorf("%s sets WriteTimeout: it must exceed httpx.RequestTimeout + resultBrandWait + the render, or a recorded tap's confirmation is cut (see this test's comment)", fset.Position(n.Pos()))
				}
			case *ast.SelectorExpr:
				if n.Sel.Name == "WriteTimeout" {
					t.Errorf("%s names WriteTimeout: it must exceed httpx.RequestTimeout + resultBrandWait + the render, or a recorded tap's confirmation is cut (see this test's comment)", fset.Position(n.Pos()))
				}
			}
			return true
		})
	}
	// Anti-vacuity: the scan found the server it is about.
	if servers == 0 {
		t.Fatal("no http.Server literal found in cmd/tappa: the scan looked at nothing")
	}
}
