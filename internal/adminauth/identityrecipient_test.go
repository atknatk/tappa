package adminauth

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIdentityRecipient_IsOneResolverRow is M14-01's inherited note (a) as a structural pin, read from
// this package's source (every non-test file): identity_open_reset is keyed by an identity id the
// function cannot check against an address, so the binding lives in Go, and these are its four walls --
//
//  1. identityRecipient is built in exactly ONE place, recipientOf, from one db.ResolvedIdentity
//     parameter's ID and Email;
//  2. recipientOf is called in exactly one place, on the resolver's result, and its value is
//     openForIdentity's argument;
//  3. OpenIdentityReset is called in exactly one place, openForIdentity, whose only parameters are a
//     context and an identityRecipient, with the recipient's id;
//  4. the grant openForIdentity returns names the recipient's address and the recipient's id.
//
// So the id a link is opened for and the address it is sent to are one resolver row's two columns.
// CONTROLS: each wall is checked against a mutated copy of the source (the typed address paired with
// the resolved id; a second literal; a second OpenIdentityReset call) and must find it.
func TestIdentityRecipient_IsOneResolverRow(t *testing.T) {
	files := packageSources(t)
	if f := identityRecipientFindings(files); len(f) > 0 {
		t.Fatalf("the per-identity recipient is not one resolver row:\n  %s", strings.Join(f, "\n  "))
	}
	src := files["identityreset.go"]
	for _, c := range []struct{ name, old, new string }{
		{"the typed address paired with the resolved id", `r.openForIdentity(ctx, recipientOf(ident))`,
			`r.openForIdentity(ctx, identityRecipient{id: ident.ID, address: email})`},
		{"a second OpenIdentityReset call", `	if !o.Issued {`,
			"	_, _ = r.data.OpenIdentityReset(ctx, who.id, h, r.now())\n	if !o.Issued {"},
		{"the grant naming another address", `		Recipient:  who.address,`, `		Recipient:  "x@example.test",`},
		{"recipientOf from another field", `return identityRecipient{id: r.ID, address: r.Email}`,
			`return identityRecipient{id: r.ID, address: r.PasswordHash.RevealForPasswordComparison()}`},
	} {
		if !strings.Contains(src, c.old) {
			t.Fatalf("CONTROL %q: the source no longer contains %q", c.name, c.old)
		}
		mutated := map[string]string{}
		for k, v := range files {
			mutated[k] = v
		}
		mutated["identityreset.go"] = strings.Replace(src, c.old, c.new, 1)
		if len(identityRecipientFindings(mutated)) == 0 {
			t.Errorf("CONTROL %q: the scan does not see it", c.name)
		}
	}
}

func packageSources(t *testing.T) map[string]string {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		out[n] = string(b)
	}
	return out
}

// identityRecipientFindings returns every way the sources break the four walls.
func identityRecipientFindings(files map[string]string) []string {
	var findings []string
	fset := token.NewFileSet()
	str := func(n ast.Node) string {
		var b strings.Builder
		_ = printer.Fprint(&b, fset, n)
		return b.String()
	}
	var literals, recipientOfCalls, openCalls, openForCalls []site
	var openFor *ast.FuncDecl
	for name, src := range files {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			return []string{"parse " + name + ": " + err.Error()}
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fd.Name.Name == "openForIdentity" {
				openFor = fd
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CompositeLit:
					if id, ok := x.Type.(*ast.Ident); ok && id.Name == "identityRecipient" {
						literals = append(literals, site{fd.Name.Name, str(x)})
					}
				case *ast.CallExpr:
					switch fun := x.Fun.(type) {
					case *ast.Ident:
						if fun.Name == "recipientOf" {
							recipientOfCalls = append(recipientOfCalls, site{fd.Name.Name, str(x)})
						}
					case *ast.SelectorExpr:
						switch fun.Sel.Name {
						case "OpenIdentityReset":
							openCalls = append(openCalls, site{fd.Name.Name, str(x)})
						case "openForIdentity":
							openForCalls = append(openForCalls, site{fd.Name.Name, str(x)})
						}
					}
				}
				return true
			})
		}
	}
	if len(literals) != 1 || literals[0].fn != "recipientOf" || literals[0].text != "identityRecipient{id: r.ID, address: r.Email}" {
		findings = append(findings, "identityRecipient literals: "+strings.TrimSpace(strings.ReplaceAll(fmtSites(literals), "\n", " ")))
	}
	if len(recipientOfCalls) != 1 || recipientOfCalls[0].text != "recipientOf(ident)" {
		findings = append(findings, "recipientOf calls: "+fmtSites(recipientOfCalls))
	}
	if len(openForCalls) != 1 || openForCalls[0].text != "r.openForIdentity(ctx, recipientOf(ident))" {
		findings = append(findings, "openForIdentity calls: "+fmtSites(openForCalls))
	}
	if len(openCalls) != 1 || openCalls[0].fn != "openForIdentity" || !strings.HasPrefix(openCalls[0].text, "r.data.OpenIdentityReset(ctx, who.id, ") {
		findings = append(findings, "OpenIdentityReset calls: "+fmtSites(openCalls))
	}
	if openFor == nil {
		return append(findings, "no openForIdentity")
	}
	params := openFor.Type.Params.List
	if len(params) != 2 || str(params[0].Type) != "context.Context" || str(params[1].Type) != "identityRecipient" ||
		len(params[1].Names) != 1 || params[1].Names[0].Name != "who" {
		findings = append(findings, "openForIdentity's parameters are not (ctx context.Context, who identityRecipient)")
	}
	var recipients, ids []string
	ast.Inspect(openFor.Body, func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok {
				switch k.Name {
				case "Recipient":
					recipients = append(recipients, str(kv.Value))
				case "IdentityID":
					ids = append(ids, str(kv.Value))
				}
			}
		}
		return true
	})
	if len(recipients) != 1 || recipients[0] != "who.address" {
		findings = append(findings, "openForIdentity's grant Recipient: "+strings.Join(recipients, ", "))
	}
	for _, id := range ids {
		if id != "who.id" {
			findings = append(findings, "openForIdentity names identity "+id)
		}
	}
	return findings
}

// site is one occurrence the scan found: the enclosing function and the expression's text.
type site struct{ fn, text string }

func fmtSites(s []site) string {
	var b strings.Builder
	for _, x := range s {
		b.WriteString(x.fn + ": " + x.text + "; ")
	}
	return b.String()
}
