package brand

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"testing"
)

// TestLogoDecode_CallsTheSniffedFormatsOwnDecoder is the syntax pin for ADR 0024 §1's
// third gate, which logo.go holds by construction (the decoder called is the sniffed
// format's own) and which no input can tell apart from the image package's registry
// while the sniff fixes the prefix. It reads logo.go and logo_resize.go and reports a
// call to the "image" package's Decode or DecodeConfig, under whatever name that
// import has in the file. The control: the same walk finds the four format-own calls
// (png.Decode, png.DecodeConfig, jpeg.Decode, jpeg.DecodeConfig) in logo.go.
func TestLogoDecode_CallsTheSniffedFormatsOwnDecoder(t *testing.T) {
	own := map[string]int{}
	for _, file := range []string{"logo.go", "logo_resize.go"} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		names := map[string]string{} // local name -> import path
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			local := path.Base(p)
			if imp.Name != nil {
				if imp.Name.Name == "." {
					t.Errorf("%s dot-imports %q", file, p)
				}
				local = imp.Name.Name
			}
			names[local] = p
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if sel.Sel.Name != "Decode" && sel.Sel.Name != "DecodeConfig" {
				return true
			}
			switch imported := names[pkg.Name]; imported {
			case "image":
				t.Errorf("%s:%d calls image.%s, the registry; the sniffed format's own decoder is the third gate",
					file, fset.Position(call.Pos()).Line, sel.Sel.Name)
			case "image/png", "image/jpeg":
				own[imported+"."+sel.Sel.Name]++
			}
			return true
		})
	}
	if len(own) != 4 {
		t.Fatalf("CONTROL FAILED: the walk found %v, want the four format-own calls", own)
	}
}
