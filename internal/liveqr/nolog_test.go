package liveqr

// nolog_test.go -- "no log line, error message or audit_log detail carries a live QR token, a pairing
// code, a screen session or the key" (M16-20's card, CLAUDE.md §7), held three ways: structurally
// (the package has no logging call at all -- internal/appdevice's shape), by its error strings (fixed
// sentences), and at run time against real Postgres (db_test.go: TestScreens_TheTrailCarriesNoSecret
// and TestScreens_LogsNothingOfTheSecrets).

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPackage_HasNoLogger: no non-test file imports "log", and no file calls a slog logging
// function, fmt's Print family, the print builtins, or names os.Stdout / os.Stderr. log/slog IS
// imported -- for the redaction hooks -- so slog's selectors are limited to those.
//
// Counted limit (internal/appdevice's): it reads source, not behaviour; a logger handed in through an
// interface would pass. Screens takes none.
func TestPackage_HasNoLogger(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	slogAllowed := map[string]bool{"LogValuer": true, "Value": true, "StringValue": true}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "log" {
				t.Errorf("%s imports \"log\"", name)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch {
				case id.Name == "slog" && !slogAllowed[x.Sel.Name]:
					t.Errorf("%s: slog.%s -- this package logs nothing (§7)", fset.Position(x.Pos()), x.Sel.Name)
				case id.Name == "fmt" && (strings.HasPrefix(x.Sel.Name, "Print") || strings.HasPrefix(x.Sel.Name, "Fprint")):
					t.Errorf("%s: fmt.%s", fset.Position(x.Pos()), x.Sel.Name)
				case id.Name == "os" && (x.Sel.Name == "Stdout" || x.Sel.Name == "Stderr"):
					t.Errorf("%s: os.%s", fset.Position(x.Pos()), x.Sel.Name)
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") {
					t.Errorf("%s: %s builtin", fset.Position(x.Pos()), id.Name)
				}
			}
			return true
		})
	}
	if files < 5 {
		t.Fatalf("read %d production files; the scan is not reading the package", files)
	}
}

// TestErrors_CarryNoInput: every refusal this package returns for a code, a session or a token is a
// fixed sentence -- driven with real values, none of them appears in any error text.
func TestErrors_CarryNoInput(t *testing.T) {
	k := testKeys(t, "a")
	tok, err := k.Mint(uuid.New(), uuid.New(), time.Now(), TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	var errs []error
	_, e := testKeys(t, "b").Open(tok)
	errs = append(errs, e)
	_, e = k.Open(tok[:40])
	errs = append(errs, e)
	_, e = k.Open(strings.Repeat("Z", 79))
	errs = append(errs, e)
	v := tok[:42]
	_, e = k.SessionDigest(ScreenSession{v: &v})
	errs = append(errs, e)
	_, e = NewKeys([]byte(tok[:10]))
	errs = append(errs, e)
	for _, e := range errs {
		if e == nil {
			t.Fatal("PREMISE: a refusal returned no error")
		}
		for _, frag := range []string{tok, tok[:40], tok[:20], tok[:10]} {
			if strings.Contains(e.Error(), frag) {
				t.Fatalf("error %q carries the input", e)
			}
		}
	}
	for _, sentinel := range []error{ErrMalformed, ErrSignature, ErrCodeRefused, ErrNoScreen, ErrRevoked, ErrUnknownLocation, ErrUnknownScreen, ErrNoKey} {
		if errors.Unwrap(sentinel) != nil || strings.ContainsAny(sentinel.Error(), "%=") {
			t.Errorf("%q is not a fixed sentence", sentinel)
		}
	}
}
