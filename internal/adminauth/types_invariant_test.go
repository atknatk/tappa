package adminauth

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// types_invariant_test.go — the STRUCTURAL half of "no §4.7 value travels in a
// type this package hands out" (user decision, 2026-08-03).
//
// 🔴 WHY IT EXISTS BESIDE TestPanelTypes_CarryNoSecretField, which already checks
// seven types field by field. That test is a FIXED TYPE LIST, and this repo has
// written the class down (M5-10): "a fixed-list test is not a net, it is a CHANGE
// DETECTOR — it catches reordering and is helpless against ADDITION, because the
// natural repair for the red test is to update the list, and that is precisely the
// wrong move."
//
// MEASURED, not argued. An audit added a plausible new type —
// `AdminProfile{AdminUserID, FullName, PasswordHash string}`, exactly the shape
// M6-05 (panel-side admin management) will bring — and the fixed-list test stayed
// GREEN. This check FLAGGED it. The same class bit this one task three times:
// adminauth.CookiePath, adminauth.MaxCandidates, and the type list itself.
//
// WHAT IT CHECKS: a PROPERTY, over whatever types the package actually declares,
// discovered from the source rather than listed here. No exported struct in this
// package may carry an exported field of a raw, printable type whose NAME looks
// like a credential. That is deliberately blunt — see the limit below.
//
// COST, measured: 10 structs parsed in 1.9–2.6 ms, and 0 false positives on the
// package as it stands (5 exported string fields, none flagged).
//
// ⚠️ KNOWN LIMIT, WRITTEN RATHER THAN IMPLIED: the check is NAME-BASED. A field
// called `Value string`, `Blob string` or `Payload []byte` holding a digest sails
// straight through. It catches the shape somebody writes by accident ("I'll just
// add PasswordHash here for logging"), not the one somebody writes on purpose.
// The type-level protection for values that MUST be carried is still the redacting
// wrapper (db.PasswordHash, adminauth.Token) — this is a tripwire, not a boundary.
var credentialishFieldName = regexp.MustCompile(
	`(?i)(password|passwd|hash|digest|token|secret|credential|apikey|api_key|privkey|cmac|salt|nonce)`)

// isRawCarrier reports the field types a credential could travel in AS A VALUE.
// A field of type Token or PasswordHash is fine — those redact themselves — which
// is why this matches only the raw builtins.
func isRawCarrier(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "string"
	case *ast.ArrayType:
		if id, ok := t.Elt.(*ast.Ident); ok && t.Len == nil {
			return id.Name == "byte"
		}
	}
	return false
}

// packageExportedStructs discovers every exported struct type declared in this
// package's NON-TEST sources.
func packageExportedStructs(t *testing.T) map[string]*ast.StructType {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	out := map[string]*ast.StructType{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil || !ast.IsExported(ts.Name.Name) {
					return true
				}
				out[ts.Name.Name] = st
				return true
			})
		}
	}
	return out
}

// TestPackageTypes_NoExportedCredentialField is the invariant.
func TestPackageTypes_NoExportedCredentialField(t *testing.T) {
	structs := packageExportedStructs(t)
	if len(structs) < 5 {
		t.Fatalf("only %d exported structs discovered — the parser is not seeing the package, "+
			"so this invariant would pass vacuously", len(structs))
	}

	var checked int
	var bad []string
	for name, st := range structs {
		for _, f := range st.Fields.List {
			if !isRawCarrier(f.Type) {
				continue
			}
			for _, fn := range f.Names {
				if !ast.IsExported(fn.Name) {
					continue
				}
				checked++
				if credentialishFieldName.MatchString(fn.Name) {
					bad = append(bad, name+"."+fn.Name)
				}
			}
		}
	}
	t.Logf("scanned %d exported structs, %d exported raw-carrier fields, %d flagged",
		len(structs), checked, len(bad))
	if len(bad) > 0 {
		t.Fatalf("these exported fields could carry a §4.7 value as a plain, printable "+
			"value: %s.\nIf the value really must be carried, wrap it in a redacting type "+
			"(see db.PasswordHash and adminauth.Token); if the name is innocent, rename it.",
			strings.Join(bad, ", "))
	}
}

// TestPackageTypes_InvariantCatchesANewType is the NEGATIVE CONTROL, and the whole
// reason this file exists: it proves the check would have caught the type the
// fixed-list test missed.
//
// It runs the same predicate over a synthetic declaration rather than editing the
// package, so it cannot leave anything behind.
func TestPackageTypes_InvariantCatchesANewType(t *testing.T) {
	const probe = `package adminauth
type AdminProfile struct {
	AdminUserID  string
	FullName     string
	PasswordHash string
}
type Innocent struct {
	TenantName string
	Role       string
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "probe.go", probe, 0)
	if err != nil {
		t.Fatalf("parse probe: %v", err)
	}
	var flagged []string
	var seen int
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok || !ast.IsExported(ts.Name.Name) {
			return true
		}
		seen++
		for _, fld := range st.Fields.List {
			if !isRawCarrier(fld.Type) {
				continue
			}
			for _, fn := range fld.Names {
				if ast.IsExported(fn.Name) && credentialishFieldName.MatchString(fn.Name) {
					flagged = append(flagged, ts.Name.Name+"."+fn.Name)
				}
			}
		}
		return true
	})
	if seen != 2 {
		t.Fatalf("the probe declared %d exported structs, want 2", seen)
	}
	if len(flagged) != 1 || flagged[0] != "AdminProfile.PasswordHash" {
		t.Fatalf("the invariant flagged %v, want exactly [AdminProfile.PasswordHash] — "+
			"a new type carrying a credential field is the case the fixed-list test misses",
			flagged)
	}
	// And the innocent type is NOT flagged, so the check is not a blanket refusal.
	for _, f := range flagged {
		if strings.HasPrefix(f, "Innocent.") {
			t.Fatalf("the invariant flagged an innocent display field: %s", f)
		}
	}
}
