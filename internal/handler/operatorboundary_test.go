package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// operatorPackages are the platform operator's Go packages (M10 OP-6, OP-7). The
// customer panel imports neither (ADR 0021 §3.6).
var operatorPackages = []string{
	"github.com/atknatk/tappa/internal/operatorauth",
	"github.com/atknatk/tappa/internal/handler/operator",
}

// operatorDBNames are the EXPORTED names internal/db declares for the operator in its
// two operator files -- derived from their syntax trees, so a name added there is
// covered here without editing this test.
func operatorDBNames(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, file := range []string{"operator.go", "operatorpool.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join("..", "db", file), nil, 0)
		if err != nil {
			t.Fatalf("parse internal/db/%s: %v", file, err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					names[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							names[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								names[n.Name] = true
							}
						}
					}
				}
			}
		}
	}
	return names
}

// customerPanelViolations reads one directory's non-test Go files and reports every
// import of an operator package and every use of internal/db's operator names through
// whatever name that file imports internal/db under.
func customerPanelViolations(t *testing.T, dir string, dbNames map[string]bool) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	parsed := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		dbAlias := ""
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, op := range operatorPackages {
				if p == op {
					out = append(out, filepath.Base(path)+" imports "+p)
				}
			}
			if p == "github.com/atknatk/tappa/internal/db" {
				dbAlias = "db"
				if imp.Name != nil {
					dbAlias = imp.Name.Name
				}
			}
		}
		parsed++
		if dbAlias == "" {
			continue
		}
		full, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(full, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == dbAlias && dbNames[sel.Sel.Name] {
				out = append(out, filepath.Base(path)+" uses db."+sel.Sel.Name)
			}
			return true
		})
	}
	if parsed == 0 {
		t.Fatalf("no Go file read in %s; the scan has gone blind", dir)
	}
	return out
}

// TestCustomerPanel_ImportsNoOperatorPackage is ADR 0021 §3.6's "müşteri paneli
// operatör paketini import etmez (OP-7'de test)": no production file of internal/handler
// imports internal/operatorauth or internal/handler/operator, and none uses internal/db's
// operator names (OperatorDB, NewOperatorDB, the accessors, OperatorConn) -- so the
// operator's pool cannot be taken, built or used on the customer side of the code.
//
// ⚠️ WHAT IT DOES NOT SEE, BY NAME: an import a package of internal/handler gets
// TRANSITIVELY (internal/handler imports internal/httpx; ADR 0020 §4 names httpx as
// requireOperator's natural home in OP-8, after which httpx would import operatorauth
// and the customer panel would reach it through httpx's API, not by importing it); and
// a dot-import of internal/db, whose operator names would then be unqualified.
//
// CONTROLS: the operator surface's own directory IS flagged (it imports operatorauth),
// and the derived name set contains the pool's constructor.
func TestCustomerPanel_ImportsNoOperatorPackage(t *testing.T) {
	names := operatorDBNames(t)
	for _, want := range []string{"OperatorDB", "NewOperatorDB", "OperatorConn", "TouchOperatorSession"} {
		if !names[want] {
			t.Fatalf("PREMISE: internal/db's operator files no longer declare %s; the derivation has gone blind", want)
		}
	}
	if v := customerPanelViolations(t, ".", names); len(v) != 0 {
		t.Errorf("the customer panel reaches the operator's packages or pool (ADR 0021 §3.6):\n  %s", strings.Join(v, "\n  "))
	}
	if v := customerPanelViolations(t, "operator", names); len(v) == 0 {
		t.Fatal("CONTROL FAILED: the operator surface's own package imports operatorauth and the scan does not see it")
	}
}
