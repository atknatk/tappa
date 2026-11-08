package config_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestInternal_EveryKeyFieldIsARedactingType is TestConfig_EverySecretFieldIsARedactingType over all of
// internal/ (backlog T205, CLAUDE.md §4.7): a struct field whose NAME says key, kek, secret, hmac, token,
// password and whose TYPE holds plain bytes or a string is red, unless this file names it and says why.
// A %+v of any struct holding such a field by value prints the key, and no method on the struct can stop
// it (the measurement in secret.go).
//
// It reads source, not reflection, because most of these fields are unexported in packages this one
// cannot import without a cycle.
//
// A type from outside internal/ is refused by default: json.RawMessage, ed25519.PrivateKey, sql.NullString
// and atomic.Pointer[[]byte] all print what they hold, and this file cannot see inside them.
// foreignNoTextTypes is the short list of the ones that hold no text.
//
// WHAT IT CANNOT SEE, stated: it goes by the field's NAME. A key in a field called `b` is green. An
// embedded field has no name to read. A local variable, a closure's capture and an `any` or other
// interface are not looked inside. `hash` and `digest` are key names since backlog T209 (a token's keyed
// hash is the proof a definer takes, a bcrypt digest is a password's stand-in); a channel is not looked
// into because fmt prints a channel as its address (measured, 2026-11-08: a `keys chan []byte` holding a
// canary, 0 of 32 renderings, slog's text handler neither, encoding/json refuses the type).
//
// MUTANTS (measured, 2026-11-06, reverted, all red here): a new `kek2 []byte` field on sun.Verifier; a
// new `Password string` on a tenant struct; a sqlc-shaped `store.Tag.AesKey []byte`; Format removed from
// config.Key; config.Key over a plain string; a stale row in keyFieldExemptions; a new field of each
// of json.RawMessage, ed25519.PrivateKey, []struct{kek []byte}, atomic.Pointer[[]byte], sql.NullString.
// T209 (measured, 2026-11-08, reverted, all red): `hash|digest` dropped from keyAnywhere (the shape rows
// and the now stale RequestHash row); apikey.Checked dropped from redactingTypes; the RequestHash row
// deleted; a new `ProbeHash string` on apikey.WriteRequest; signup.Address over a plain string; the
// map case dropped from anonymousStruct. Before the widening the pattern found, once added, six plain
// fields: adminauth ClaimProof.hash, SignupClaim.hash, proofSession.hash, rowFacts.digest, and
// apikey Checked.hash, WriteRequest.RequestHash.
func TestInternal_EveryKeyFieldIsARedactingType(t *testing.T) {
	tree := readInternalTree(t, "..")
	report := tree.keyFieldProblems()
	for _, p := range report.problems {
		t.Errorf("%s (backlog T205, CLAUDE.md §4.7)", p)
	}
	for key := range keyFieldExemptions {
		if !report.exempted[key] {
			t.Errorf("keyFieldExemptions has %s, which is not a plain key-named field any more: delete the row", key)
		}
	}
	if report.redacting < 60 || report.storeShaped < 30 {
		t.Errorf("the walk saw %d redacting fields, %d store columns and %d key-named fields; it has gone blind",
			report.redacting, report.storeShaped, report.named)
	}
	for _, name := range redactingTypes {
		if p := tree.redactingTypeProblem(name); p != "" {
			t.Errorf("%s: %s (backlog T205)", name, p)
		}
	}
	t.Logf("%d key-named fields: %d redacting, %d exempt by name, %d store columns (plain text under the column-name rule), %d with no text in them",
		report.named, report.redacting, len(report.exempted), report.storeShaped, report.noText)
}

// TestKeyFieldGate_SeesTheShapes is the gate's own control: the shapes it has to refuse, the shapes it has
// to accept. Without it a broken resolver reads as a clean tree.
func TestKeyFieldGate_SeesTheShapes(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		red         bool
	}{
		{"bytes", "kek []byte", true},
		{"string", "HMACKey string", true},
		{"array", "key [32]byte", true},
		{"slice of slices", "keks [][]byte", true},
		{"pointer to bytes", "secret *[]byte", true},
		{"pointer to string", "Password *string", true},
		{"a map's value", "tokens map[string][]byte", true},
		{"a map's key", "keys map[string]bool", true},
		{"a named string", "token raw", true},
		{"a named bytes", "key rawBytes", true},
		{"an anonymous struct's field", "box struct{ kek []byte }", true},
		{"an all-caps kek", "TOTPKEK string", true},
		{"a foreign byte slice", "kekRaw json.RawMessage", true},
		{"a foreign private key", "signKey ed25519.PrivateKey", true},
		{"a slice of anonymous structs", "box []struct{ kek []byte }", true},
		{"a foreign generic over bytes", "dek atomic.Pointer[[]byte]", true},
		{"a foreign nullable string, a numbered name", "seed2 sql.NullString", true},
		{"a local generic over bytes", "key wrapper[[]byte]", true},
		{"key between capitals", "AESKeyRef []byte", true},
		{"a token's hash", "tokenHash string", true},
		{"a digest", "digest string", true},
		{"a hash in capitals", "RequestHASH []byte", true},
		{"a map of anonymous structs", "box map[string]struct{ kek []byte }", true},
		{"a redacting type", "kek config.Key", false},
		{"a slice of a redacting type", "keks []config.Key", false},
		{"a redacting type of its package", "token ok", false},
		{"a bool", "PasswordMatched bool", false},
		{"a func", "key func() []byte", false},
		{"an interface", "keys interface{ Get() string }", false},
		{"a struct with no key in it", "token notText", false},
		{"not a key's name", "body []byte", false},
		{"a word that only contains the letters", "monkey []byte", false},
		{"machine is not mac", "machine string", false},
		{"a listed foreign type", "key uuid.UUID", false},
		{"a redacted hash", "hash ok", false},
		{"a channel prints its address", "keys chan []byte", false},
		{"a listed foreign duration", "keyPeriod time.Duration", false},
		{"a local generic over nothing", "key wrapper[int]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `package probe
import (
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/google/uuid"
)
var _ config.Key
var _ uuid.UUID
var _ time.Duration
var _ json.RawMessage
var _ ed25519.PrivateKey
var _ sql.NullString
var _ atomic.Int32
type wrapper[T any] struct{ n int }
type raw string
type rawBytes []byte
type ok struct{ v *string }
type notText struct{ n int }
type holder struct { ` + tc.field + ` }
`
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "probe.go", src, 0)
			if err != nil {
				t.Fatal(err)
			}
			tree := newInternalTree(fset)
			tree.add("probe", f)
			tree.addRedactingForTest("probe.ok")
			tree.addRedactingForTest("config.Key")
			got := len(tree.keyFieldProblems().problems) > 0
			if got != tc.red {
				t.Errorf("field %q: refused = %v, want %v", tc.field, got, tc.red)
			}
		})
	}
}

// redactingTypes is the CLOSED list of types a key-named field may hold: each is one field behind a
// pointer or a closure (no fmt path opens it) and carries the five printing methods.
// redactingTypeProblem checks both.
var redactingTypes = []string{
	"config.Key", "config.DSN",
	"mail.Credential",
	"operatorauth.Key", "operatorauth.Secret", "operatorauth.SessionToken",
	"adminauth.Token", "adminauth.ResetToken", "adminauth.InviteToken",
	"session.Token",
	"apikey.Token", "apikey.Checked",
	"adminauth.proofHash",
	"domain/signup.Address",
	"webhook.SigningKey",
	"db.PasswordHash",
	"sun.WipeableKey",
}

// foreignNoTextTypes are the types from outside internal/ a key-named field may hold, because none prints a
// key: every other foreign type is refused, since this file cannot read its fields.
var foreignNoTextTypes = []string{"github.com/google/uuid.UUID", "time.Duration", "time.Time"}

// redactingMethods are the printing paths secret.go's measurement names.
var redactingMethods = []string{"Format", "String", "GoString", "LogValue", "MarshalText"}

// keyFieldExemptions names every key-NAMED field of plain text that holds no key, with the reason. A row
// whose field stops being plain, or stops existing, is red, so the list cannot rot.
var keyFieldExemptions = map[string]string{
	// Named key, holds no secret.
	"apikey.BudgetAlert.KeyID":           "a key's id, an audit-visible uuid in string form",
	"apikey.BudgetAlert.KeyName":         "the name its owner gave a key",
	"apikey.WriteRequest.IdempotencyKey": "the integrator's retry label, public by the API's contract",
	"domain/tenant.RuleCondition.Key":    "a rule condition's name in a policy document",
	"policy.Anomaly.Key":                 "a closed-set context name",
	"policy.Context.Keys":                "context names to values, no secret in the closed set",
	"mail.mailbox.key":                   "an HMAC digest of an address (RecipientCap's map key), not a key",
	// A comparison value, not a proof: no definer takes it to find a row by.
	"apikey.WriteRequest.RequestHash": "SHA-256 of the request's method, route and body, the idempotency ledger's comparison value " +
		"(a replay with another body is refused by it); it opens nothing",
	// Sealed under a KEK: printing it gives ciphertext the database row already holds.
	"db.ResolvedTag.AESKeyRef": "AES-256-GCM envelope under TAPPA_TAG_KEK; the plaque key is not recoverable from it",
	// A proof or a MAC, not a key.
	"adminauth.Proof.mac": "a MAC over a password digest, a value type compared with ==, printing redacted through its own methods; " +
		"a holder's unexported field still reaches it, see backlog T253",
	// A signed value that travels to the browser anyway.
	"handler.claimStep.token": "the signed moment in a confirmation form's hidden field; it is sent to the browser by design",
}

// storeKeyFieldShape is what a key-named column of a sqlc row may be called. internal/store is generated
// and cannot hold a redacting type, so its rule is the column's own: hashed, sealed, or an id. A
// `store.Tag.AesKey []byte` is red here, which is the shape of a plaque key stored in the clear.
var storeKeyFieldShape = regexp.MustCompile(`(Hash|Sealed|Ref|KeyID|KeyName|IdemKey)$`)

// keyAnywhere and keyWholeWord are what make a field key-named. kek, hmac and the like match anywhere,
// because they are spelled in capitals inside longer names (TOTPKEK, TokenHMACKey). key and mac only as a
// whole camelCase word, or monkey and machine would count.
var (
	keyAnywhere  = regexp.MustCompile(`(?i)kek|hmac|secret|passw|pwd|token|credential|cmac|apikey|dsn|pepper|hash|digest`)
	keyWholeWord = regexp.MustCompile(`^(?i:keys?|macs?|deks?|seeds?|salts?)\d*$`)
)

func keyNamed(name string) bool {
	if keyAnywhere.MatchString(name) {
		return true
	}
	return slices.ContainsFunc(camelWords(name), keyWholeWord.MatchString)
}

// camelWords splits an identifier at its case changes: AESKeyRef is AES, Key, Ref.
func camelWords(name string) []string {
	var words []string
	start := 0
	upper := func(i int) bool { return name[i] >= 'A' && name[i] <= 'Z' }
	for i := 1; i < len(name); i++ {
		if upper(i) && (!upper(i-1) || (i+1 < len(name) && !upper(i+1))) {
			words = append(words, name[start:i])
			start = i
		}
	}
	return append(words, name[start:])
}

// internalTree is every non-test declaration under internal/, with enough resolved to tell what a type
// is underneath: its packages are named by their path under internal/ ("handler/operator").
type internalTree struct {
	fset    *token.FileSet
	files   []treeFile
	types   map[string]ast.Expr
	methods map[string][]string
	allowed []string
}

type treeFile struct {
	pkg     string
	file    *ast.File
	imports map[string]string
}

const internalImportPrefix = "github.com/atknatk/tappa/internal/"

func newInternalTree(fset *token.FileSet) *internalTree {
	return &internalTree{fset: fset, types: map[string]ast.Expr{}, methods: map[string][]string{}, allowed: slices.Clone(redactingTypes)}
}

func (tr *internalTree) addRedactingForTest(name string) { tr.allowed = append(tr.allowed, name) }

func readInternalTree(t *testing.T, root string) *internalTree {
	t.Helper()
	tree := newInternalTree(token.NewFileSet())
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(tree.fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		tree.add(filepath.ToSlash(rel), f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func (tr *internalTree) add(pkg string, f *ast.File) {
	imports := map[string]string{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name] = path
	}
	tr.files = append(tr.files, treeFile{pkg, f, imports})
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					tr.types[pkg+"."+ts.Name.Name] = ts.Type
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) != 1 {
				continue
			}
			recv := d.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if id, ok := recv.(*ast.Ident); ok {
				tr.methods[pkg+"."+id.Name] = append(tr.methods[pkg+"."+id.Name], d.Name.Name)
			}
		}
	}
}

// fieldClass is what a field's type is underneath.
type fieldClass int

const (
	classNoText    fieldClass = iota // a bool, a func, a struct, a foreign type: nothing fmt prints as the key
	classRedacting                   // only redacting types hold text in it
	classPlainText                   // bytes or a string are reachable by fmt
	classForeign                     // a type from outside internal/ that foreignNoTextTypes does not list
)

func maxClass(a, b fieldClass) fieldClass {
	if a > b {
		return a
	}
	return b
}

func (tr *internalTree) classify(f treeFile, pkg string, e ast.Expr, seen map[string]bool) fieldClass {
	switch t := e.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string", "byte", "uint8":
			return classPlainText
		}
		return tr.classifyNamed(f, pkg+"."+t.Name, seen)
	case *ast.SelectorExpr:
		x, ok := t.X.(*ast.Ident)
		if !ok {
			return classNoText
		}
		path, ok := f.imports[x.Name]
		if !ok {
			return classNoText
		}
		if !strings.HasPrefix(path, internalImportPrefix) {
			if slices.Contains(foreignNoTextTypes, path+"."+t.Sel.Name) {
				return classNoText
			}
			return classForeign
		}
		return tr.classifyNamed(f, strings.TrimPrefix(path, internalImportPrefix)+"."+t.Sel.Name, seen)
	case *ast.IndexExpr:
		return maxClass(tr.classify(f, pkg, t.X, seen), tr.classify(f, pkg, t.Index, seen))
	case *ast.IndexListExpr:
		c := tr.classify(f, pkg, t.X, seen)
		for _, index := range t.Indices {
			c = maxClass(c, tr.classify(f, pkg, index, seen))
		}
		return c
	case *ast.StarExpr:
		return tr.classify(f, pkg, t.X, seen)
	case *ast.ArrayType:
		return tr.classify(f, pkg, t.Elt, seen)
	case *ast.MapType:
		return maxClass(tr.classify(f, pkg, t.Key, seen), tr.classify(f, pkg, t.Value, seen))
	}
	return classNoText
}

func (tr *internalTree) classifyNamed(f treeFile, name string, seen map[string]bool) fieldClass {
	if slices.Contains(tr.allowed, name) {
		return classRedacting
	}
	if seen[name] {
		return classNoText
	}
	seen[name] = true
	under, ok := tr.types[name]
	if !ok {
		return classNoText // a builtin (bool, int, error) or a foreign type
	}
	switch under.(type) {
	case *ast.StructType, *ast.InterfaceType, *ast.FuncType, *ast.ChanType:
		return classNoText // a struct's own fields are walked in their own right
	}
	return tr.classify(tr.fileOf(name, f), name[:strings.LastIndex(name, ".")], under, seen)
}

// fileOf is any file of name's package, for its imports: a named type's underlying type resolves there.
func (tr *internalTree) fileOf(name string, fallback treeFile) treeFile {
	pkg := name[:strings.LastIndex(name, ".")]
	for _, f := range tr.files {
		if f.pkg == pkg {
			return f
		}
	}
	return fallback
}

type keyFieldReport struct {
	problems                              []string
	exempted                              map[string]bool
	named, redacting, storeShaped, noText int
}

func (tr *internalTree) keyFieldProblems() keyFieldReport {
	r := keyFieldReport{exempted: map[string]bool{}}
	for _, f := range tr.files {
		f := f
		ast.Inspect(f.file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			owner := f.pkg + "." + ts.Name.Name
			if slices.Contains(tr.allowed, owner) {
				return true // its one field IS the redaction; redactingTypeProblem reads it
			}
			tr.walkFields(f, owner, st, &r)
			return true
		})
	}
	return r
}

func (tr *internalTree) walkFields(f treeFile, owner string, st *ast.StructType, r *keyFieldReport) {
	for _, field := range st.Fields.List {
		if inner := anonymousStruct(field.Type); inner != nil {
			tr.walkFields(f, owner, inner, r)
			continue
		}
		for _, name := range field.Names {
			if !keyNamed(name.Name) {
				continue
			}
			r.named++
			key := owner + "." + name.Name
			switch tr.classify(f, f.pkg, field.Type, map[string]bool{}) {
			case classRedacting:
				r.redacting++
			case classNoText:
				r.noText++
			case classPlainText:
				switch {
				case f.pkg == "store" && storeKeyFieldShape.MatchString(name.Name):
					r.storeShaped++
				case keyFieldExemptions[key] != "":
					r.exempted[key] = true
				default:
					r.problems = append(r.problems, tr.fset.Position(name.Pos()).String()+": "+key+
						" is named like a key and holds plain text; make it a redacting type (config.Key; sun.WipeableKey if it "+
						"has to be wiped) or name it in keyFieldExemptions with the reason")
				}
			case classForeign:
				r.problems = append(r.problems, tr.fset.Position(name.Pos()).String()+": "+key+
					" is named like a key and holds a type from outside internal/ that this gate cannot read; "+
					"make it a redacting type, or add the type to foreignNoTextTypes if it holds no text")
			}
		}
	}
}

// anonymousStruct is the struct literal a field's type holds through pointers, slices, arrays and a map's
// value, nil if none. The map's value was missing (backlog T209: `box map[string]struct{kek []byte}` was
// green, and fmt prints a map's values).
func anonymousStruct(e ast.Expr) *ast.StructType {
	for {
		switch t := e.(type) {
		case *ast.StructType:
			return t
		case *ast.StarExpr:
			e = t.X
		case *ast.ArrayType:
			e = t.Elt
		case *ast.MapType:
			e = t.Value
		default:
			return nil
		}
	}
}

// redactingTypeProblem says what is wrong with one of redactingTypes, "" when nothing is: the five
// methods, and a shape no fmt path opens -- one field, a *string or a func.
func (tr *internalTree) redactingTypeProblem(name string) string {
	under, ok := tr.types[name]
	if !ok {
		return "no such type under internal/"
	}
	st, ok := under.(*ast.StructType)
	if !ok || len(st.Fields.List) != 1 || len(st.Fields.List[0].Names) != 1 {
		return "not a struct of one field, so fmt can reach what it holds"
	}
	switch t := st.Fields.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); !ok || id.Name != "string" {
			return "its field is a pointer to something other than a string; fmt's badVerb opens a pointer to a slice, an array or a struct"
		}
	case *ast.FuncType:
	default:
		return "its field is neither a *string nor a func, so fmt prints what it holds from a caller's unexported field"
	}
	for _, m := range redactingMethods {
		if !slices.Contains(tr.methods[name], m) {
			return "it has no " + m + " method, so that printing path carries the value"
		}
	}
	return ""
}
