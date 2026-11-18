package appdevice

// nolog_test.go -- "the package writes no log line carrying a signature, a nonce, a public key or
// a body" (M16-05's card, CLAUDE.md §7), held two ways: structurally (there is no logging call in
// the package at all) and at run time (the default logger sees none of those values while every
// path runs against real Postgres -- which also covers what internal/db logs on our behalf).

import (
	"bytes"
	"context"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestPackage_HasNoLogger: no non-test file imports "log", and no file calls a slog logging
// function, fmt's Print family, the print builtins, or names os.Stdout / os.Stderr. log/slog
// itself IS imported -- for the slog.LogValuer redaction hooks -- so the rule is on the calls:
// slog's selectors are limited to the three the redaction needs.
//
// Counted limit: it reads source, not behaviour; a logger handed in through an interface would
// pass. The package takes none (Verifier's only dependency is Database), and adding one is the
// edit this test is here to make visible.
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

// TestVerify_LogsNothingOfTheRequest: with the default logger captured, every outcome runs
// against real Postgres -- a verified request, a replay, a bad signature, a revoked and an
// unknown device -- and the capture holds none of the request's nonce, signature, public key or
// body. A marker line logged through the same default proves the capture works.
func TestVerify_LogsNothingOfTheRequest(t *testing.T) {
	d := appDB(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	v := dbVerifier(t, d)
	ctx := context.Background()
	w := newWorld(t, d)
	p := addPhone(t, d, w)
	var material []string
	note := func(parts parts, sig string) {
		material = append(material, parts.nonce, sig, hex.EncodeToString(mustDecode(t, sig)), string(parts.body))
	}

	n := issue(t, v, p)
	parts := defaultParts(p.id, n.Wire())
	sig := sign(t, p.key, parts)
	note(parts, sig)
	req := request(t, parts, sig)
	if _, err := v.Verify(ctx, req); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if _, err := v.Verify(ctx, req); err == nil {
		t.Fatal("replay accepted")
	}
	other := defaultParts(p.id, issue(t, v, p).Wire())
	otherSig := sign(t, testKey(t), other)
	note(other, otherSig)
	if _, err := v.Verify(ctx, request(t, other, otherSig)); err == nil {
		t.Fatal("bad signature accepted")
	}
	revoke(t, d, p)
	if _, err := v.Verify(ctx, request(t, other, otherSig)); err == nil {
		t.Fatal("revoked accepted")
	}
	ghost := defaultParts(uuid.New(), randomNonceWire(t))
	ghostSig := sign(t, testKey(t), ghost)
	note(ghost, ghostSig)
	if _, err := v.Verify(ctx, request(t, ghost, ghostSig)); err == nil {
		t.Fatal("unknown accepted")
	}
	material = append(material, hex.EncodeToString(rawPublic(t, p.key)), n.hash())

	slog.Info("appdevice-log-capture-marker")
	got := buf.String()
	if !strings.Contains(got, "appdevice-log-capture-marker") {
		t.Fatal("positive control: the captured logger did not see the marker")
	}
	for _, m := range material {
		if m != "" && strings.Contains(got, m) {
			t.Fatal("the log carries request material (nonce, signature, key, body or nonce hash)")
		}
	}
}
