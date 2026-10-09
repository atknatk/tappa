package main

// mailconfig_test.go — M10 EM-3: the transactional e-mail configuration as it is
// SHIPPED. The settings themselves are internal/config's tests (mail_test.go there);
// these are what only the repository as a whole can show:
//   - ADR 0022 §2's ban on a private root pool, as its three pins: (a) no product code
//     outside internal/mail touches mail.Config's RootCAs (type-checked); (b) no
//     configuration variable names a certificate authority; (c) nothing in the
//     deployment moves the system roots (SSL_CERT_FILE / SSL_CERT_DIR);
//   - the manifests: the two credentials are optional Secret keys, the ConfigMap still
//     ships none/panel, and the ConfigMap's transport settings load in production;
//   - the binary: a delivery mode the configuration accepts and this build lacks stops
//     the boot before the database is dialled.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/atknatk/tappa/internal/config"
)

const (
	modulePath  = "github.com/atknatk/tappa"
	mailPkgPath = modulePath + "/internal/mail"
)

// modulePackage is one package of this module as `go list` describes it.
type modulePackage struct {
	ImportPath, Dir, Export           string
	GoFiles, IgnoredGoFiles, CgoFiles []string
}

// moduleTypes lists every package of this module with every dependency's EXPORT DATA
// and returns the module's own packages with an importer that reads it —
// internal/db's moduleExports, the precedent, with its guards: the go command must be
// the toolchain that built this test (export data is version-specific), the -race build
// asks for the -race export data, and every failure stops the test.
func moduleTypes(t *testing.T, fset *token.FileSet) ([]modulePackage, types.Importer) {
	t.Helper()
	if v, err := exec.Command("go", "env", "GOVERSION").Output(); err != nil {
		t.Fatalf("go env GOVERSION: %v", err)
	} else if got := strings.TrimSpace(string(v)); got != runtime.Version() {
		t.Fatalf("the go command is %s and this test was built by %s: its export data would not be this toolchain's",
			got, runtime.Version())
	}
	args := []string{"list", "-export", "-deps", "-json=ImportPath,Dir,Export,GoFiles,IgnoredGoFiles,CgoFiles"}
	if raceBuild {
		args = append(args, "-race")
	}
	cmd := exec.Command("go", append(args, modulePath+"/...")...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		var stderr string
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		t.Fatalf("go list -export: %v: %s", err, stderr)
	}
	exports := map[string]string{}
	var own []modulePackage
	for dec := json.NewDecoder(bytes.NewReader(out)); ; {
		var p modulePackage
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("go list output: %v", err)
		}
		exports[p.ImportPath] = p.Export
		if p.ImportPath == modulePath || strings.HasPrefix(p.ImportPath, modulePath+"/") {
			own = append(own, p)
		}
	}
	if exports[mailPkgPath] == "" || exports["crypto/tls"] == "" {
		t.Fatalf("PREMISE: go list named no export data for %s or crypto/tls; the load has gone blind", mailPkgPath)
	}
	return own, importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		f, ok := exports[path]
		if !ok || f == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(f)
	})
}

// rootPoolUses type-checks one package and returns every place it touches
// mail.Config's root pool, as "file:line: what":
//
//   - ANY USE of the RootCAs field of internal/mail's Config — a keyed composite literal
//     (mail.Config{RootCAs: …}), an assignment (c.RootCAs = …), taking its address, a
//     read; through a pointer, a promoted field of an embedding struct, or a type
//     defined from mail.Config (its fields are the same objects);
//   - a RootCAs field reached through a selector on, or keyed in a literal of, ANY struct
//     whose type is IDENTICAL to mail.Config's struct (round 2, the security audit's
//     MEDIUM and the third eye's X14): a struct declared elsewhere with the same fields
//     converts to and from mail.Config, so `s := shape(c); s.RootCAs = p;
//     return mail.Config(s)` sets the pool without naming mail.Config's field;
//   - a CONVERSION to mail.Config or *mail.Config from any other type — the step that
//     carries such a struct (or an unsafe.Pointer) back into the real type;
//   - a POSITIONAL composite literal of mail.Config's struct shape, which sets every
//     field — RootCAs included — without naming one.
//
// It is about mail.Config ONLY (ADR 0022 §2): crypto/tls's Config has a field of the
// same name, and a struct of any other shape may name a field RootCAs too; neither is
// this pin's business. What it does not see: reflect, unsafe arithmetic that writes the
// field's memory without converting to *mail.Config (unsafe.Add, an offset), and a whole
// mail.Config value built inside internal/mail and handed out.
func rootPoolUses(fset *token.FileSet, imp types.Importer, path string, files []*ast.File) ([]string, error) {
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	if _, err := (&types.Config{Importer: imp}).Check(path, fset, files, info); err != nil {
		return nil, err
	}
	mp, err := imp.Import(mailPkgPath)
	if err != nil {
		return nil, err
	}
	cfg := mp.Scope().Lookup("Config")
	if cfg == nil {
		return nil, fmt.Errorf("PREMISE: no Config in %s's export data", mailPkgPath)
	}
	target := cfg.Type()
	shape := target.Underlying()
	at := func(n ast.Node) string {
		p := fset.PositionFor(n.Pos(), false)
		return fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line)
	}
	var hits []string
	for id, obj := range info.Uses {
		v, ok := obj.(*types.Var)
		if ok && v.IsField() && v.Name() == "RootCAs" && v.Pkg() != nil && v.Pkg().Path() == mailPkgPath {
			hits = append(hits, at(id)+": uses mail.Config.RootCAs")
		}
	}
	for sel, s := range info.Selections {
		if s.Kind() == types.FieldVal && s.Obj().Name() == "RootCAs" {
			if holder := fieldHolder(s); holder != nil && types.Identical(holder, shape) {
				hits = append(hits, at(sel.Sel)+": RootCAs of a struct shaped like mail.Config")
			}
		}
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				tv, ok := info.Types[x]
				if !ok || tv.Type == nil || len(x.Elts) == 0 || !types.Identical(tv.Type.Underlying(), shape) {
					return true
				}
				if _, keyed := x.Elts[0].(*ast.KeyValueExpr); !keyed {
					hits = append(hits, at(x)+": a positional mail.Config literal")
					return true
				}
				for _, e := range x.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "RootCAs" {
							hits = append(hits, at(kv)+": RootCAs keyed in a literal shaped like mail.Config")
						}
					}
				}
			case *ast.CallExpr:
				fun, ok := info.Types[x.Fun]
				if !ok || !fun.IsType() || len(x.Args) != 1 {
					return true
				}
				to := types.Unalias(fun.Type)
				if !types.Identical(to, target) && !types.Identical(to, types.NewPointer(target)) {
					return true
				}
				if from, ok := info.Types[x.Args[0]]; !ok || from.Type == nil || !types.Identical(from.Type, to) {
					hits = append(hits, at(x)+": a conversion to mail.Config from another type")
				}
			}
			return true
		})
	}
	sort.Strings(hits)
	return hits, nil
}

// fieldHolder is the struct type that DECLARES the field a selection reaches — the end of
// its index path, through pointers and embedded structs — or nil.
func fieldHolder(s *types.Selection) *types.Struct {
	t := s.Recv()
	idx := s.Index()
	for i, k := range idx {
		t = types.Unalias(t)
		if p, ok := t.Underlying().(*types.Pointer); ok {
			t = p.Elem()
		}
		st, ok := t.Underlying().(*types.Struct)
		if !ok {
			return nil
		}
		if i == len(idx)-1 {
			return st
		}
		t = st.Field(k).Type()
	}
	return nil
}

// rootPoolSpecimen is a package that touches the root pool in every written form
// rootPoolUses claims to see, beside forms it must not report (crypto/tls's field of the
// same name, twice; a RootCAs field of a struct of another shape; a keyed literal that
// leaves RootCAs out; a conversion of a mail.Config to itself). Each reported line
// carries its form's name in a comment, so a miss names the form.
const rootPoolSpecimen = `package probe

import (
	"crypto/tls"
	"crypto/x509"
	"time"
	"unsafe"

	m "github.com/atknatk/tappa/internal/mail"
)

type wrapped struct{ m.Config }

type defined m.Config

type relayShape struct {
	Host               string
	Port               int
	Username, Password m.Credential
	From, ReplyTo      string
	RootCAs            *x509.CertPool
	Timeout            time.Duration
	RetryDelay         time.Duration
}

func keyed() m.Config { return m.Config{Host: "h", RootCAs: x509.NewCertPool()} } // HIT keyed

func assigned(c m.Config) m.Config { c.RootCAs = x509.NewCertPool(); return c } // HIT assigned

func pointer(c *m.Config) { c.RootCAs = nil } // HIT pointer

func address(c *m.Config) { p := &c.RootCAs; *p = nil } // HIT address

func promoted(w wrapped) wrapped { w.RootCAs = nil; return w } // HIT promoted

func definedType() defined { return defined{RootCAs: nil} } // HIT defined

func read(c m.Config) bool { return c.RootCAs == nil } // HIT read

func positional() m.Config { return m.Config{"h", 587, m.Credential{}, m.Credential{}, "f", "", nil, 0, 0} } // HIT positional

func viaShape(c m.Config, p *x509.CertPool) m.Config {
	s := relayShape(c)
	s.RootCAs = p      // HIT shape-field
	return m.Config(s) // HIT shape-conversion
}

func viaShapePointer(c *m.Config, p *x509.CertPool) { (*relayShape)(c).RootCAs = p } // HIT shape-pointer

func shapeLiteral(p *x509.CertPool) relayShape { return relayShape{RootCAs: p} } // HIT shape-literal

func viaAnonymous(p *x509.CertPool) m.Config { return m.Config(struct{ Host string; Port int; Username, Password m.Credential; From, ReplyTo string; RootCAs *x509.CertPool; Timeout, RetryDelay time.Duration }{RootCAs: p}) } // HIT anonymous

func viaUnsafe(s *relayShape) *m.Config { return (*m.Config)(unsafe.Pointer(s)) } // HIT unsafe-pointer

func tlsLiteral() *tls.Config { return &tls.Config{RootCAs: x509.NewCertPool()} }

func tlsAssign(c *tls.Config) { c.RootCAs = nil }

func otherShape(p *x509.CertPool) any { return struct{ RootCAs *x509.CertPool }{RootCAs: p} }

func keyedWithout() m.Config { return m.Config{Host: "h", Port: 587} }

func itself(c m.Config) m.Config { return m.Config(c) }
`

// TestRootPoolUses_SeesEveryWrittenForm is the control for the pin below, on a
// specimen written here: every form rootPoolUses claims is reported on its own line,
// and crypto/tls's RootCAs is not. The predicate is tested as code (the
// disallowedEnvFromKinds precedent: a check that lives only inside the test it serves
// can be neutered without anything noticing).
func TestRootPoolUses_SeesEveryWrittenForm(t *testing.T) {
	fset := token.NewFileSet()
	_, imp := moduleTypes(t, fset)
	f, err := parser.ParseFile(fset, "probe.go", rootPoolSpecimen, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := rootPoolUses(fset, imp, "example.test/probe", []*ast.File{f})
	if err != nil {
		t.Fatalf("type-checking the specimen: %v", err)
	}
	var want []string
	for i, line := range strings.Split(rootPoolSpecimen, "\n") {
		if strings.Contains(line, "// HIT ") {
			want = append(want, fmt.Sprintf("probe.go:%d", i+1))
		}
	}
	got := map[string]bool{}
	for _, h := range hits {
		got[h[:strings.Index(h, ": ")]] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("rootPoolUses missed the form on %s", w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("rootPoolUses reported %d lines, the specimen has %d forms (crypto/tls's field or a keyed literal "+
			"without RootCAs was reported): %v", len(got), len(want), hits)
	}
}

// TestMailConfig_NoProductCodeSetsTheRootPool is ADR 0022 §2's pin (a): in every package
// of this module except internal/mail — type-checked from its non-test files with the
// exact types of its dependencies — nothing touches mail.Config's RootCAs, in any form
// rootPoolUses lists. nil means the image's system roots; a private root pool would let
// whoever holds it read the SMTP credentials and every link (ADR 0022 §2, EM-K9).
//
// A file this platform's build leaves out (a build tag, another GOOS, cgo) cannot be
// type-checked here, so it is searched for the field's NAME instead, and any mention
// fails — crypto/tls's field of that name included: fail-closed, with the file named.
//
// CONTROLS: internal/config and cmd/tappa are among the packages checked, and the same
// scan run on internal/mail itself finds its own read of the field (mail.go, New) — so
// the scan's silence elsewhere is not blindness.
func TestMailConfig_NoProductCodeSetsTheRootPool(t *testing.T) {
	fset := token.NewFileSet()
	own, imp := moduleTypes(t, fset)
	nameRe := regexp.MustCompile(`\bRootCAs\b`)
	var checked []string
	for _, p := range own {
		parse := func(names []string) []*ast.File {
			var files []*ast.File
			for _, n := range names {
				f, err := parser.ParseFile(fset, filepath.Join(p.Dir, n), nil, 0)
				if err != nil {
					t.Fatalf("parsing %s: %v", n, err)
				}
				files = append(files, f)
			}
			return files
		}
		if len(p.GoFiles) == 0 {
			continue
		}
		hits, err := rootPoolUses(fset, imp, p.ImportPath, parse(p.GoFiles))
		if err != nil {
			t.Fatalf("type-checking %s: %v", p.ImportPath, err)
		}
		if p.ImportPath == mailPkgPath {
			if len(hits) == 0 {
				t.Errorf("CONTROL FAILED: the scan finds no use of RootCAs in internal/mail itself, whose New reads it")
			}
			continue
		}
		checked = append(checked, p.ImportPath)
		for _, h := range hits {
			t.Errorf("%s: %s — product code outside internal/mail must not choose the relay's root pool "+
				"(ADR 0022 §2: nil, the system roots, is the only production value)", p.ImportPath, h)
		}
		for _, n := range append(slices.Clone(p.IgnoredGoFiles), p.CgoFiles...) {
			if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(p.Dir, n))
			if err != nil {
				t.Fatal(err)
			}
			if nameRe.Match(b) {
				t.Errorf("%s/%s mentions RootCAs and is outside this platform's build, so it cannot be type-checked "+
					"here; the root-pool pin refuses it rather than guess", p.ImportPath, n)
			}
		}
	}
	for _, must := range []string{modulePath + "/internal/config", modulePath + "/cmd/tappa"} {
		if !slices.Contains(checked, must) {
			t.Fatalf("CONTROL FAILED: %s was not type-checked; the scan has gone blind", must)
		}
	}
	if len(checked) < 20 {
		t.Fatalf("only %d packages type-checked; the scan has gone blind", len(checked))
	}
	t.Logf("type-checked %d packages outside internal/mail", len(checked))
}

// certificateTokens are the words a variable that points at a certificate authority
// would be named with. One whole underscore-separated part must equal one of them, so
// TAPPA_TRUSTED_PROXIES and a CACHE are not hits. CAFILE and CAPATH are OpenSSL's
// -CAfile/-CApath and curl's --capath (round 2, the third eye's X15). And any part that
// CONTAINS "CERT" is a hit as well (ROOTCERTS, CERTFILE), because no word of this
// product's own variables carries it.
//
// THE LIST IS NOT CLOSED: a variable named with words outside it (a ..._POOL, an
// ..._ISSUER) passes. What keeps such a variable from reaching the relay is pin (a): it
// cannot be put into mail.Config.
var certificateTokens = []string{"CA", "CAS", "CAFILE", "CAPATH", "CABUNDLE", "CACERT", "CACERTS", "CERT", "CERTS",
	"CERTIFICATE", "CERTIFICATES", "ROOT", "ROOTS", "PEM", "X509", "TLS", "SSL", "ANCHOR", "ANCHORS", "BUNDLE",
	"TRUST", "TRUSTSTORE"}

// certificateVariable reports whether an environment variable's name says it carries a
// certificate authority or a trust store.
func certificateVariable(name string) bool {
	for _, part := range strings.Split(name, "_") {
		if slices.Contains(certificateTokens, part) || strings.Contains(part, "CERT") {
			return true
		}
	}
	return false
}

// TestCertificateVariable is the predicate's own table, so it can be broken and
// something notices.
func TestCertificateVariable(t *testing.T) {
	for name, want := range map[string]bool{
		"TAPPA_SMTP_CA_FILE": true, "TAPPA_SMTP_ROOT_CERTS": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
		"TAPPA_TLS_ROOTS": true, "TAPPA_MAIL_TRUST_BUNDLE": true, "TAPPA_SMTP_CACERT": true, "TAPPA_X509_POOL": true,
		"TAPPA_SMTP_CAFILE": true, "TAPPA_SMTP_CAPATH": true, "TAPPA_SMTP_ROOTCERTS": true, "TAPPA_RELAY_CERTFILE": true,
		"TAPPA_SMTP_TRUST": true, "TAPPA_MAIL_TRUSTSTORE": true,
		"TAPPA_TRUSTED_PROXIES": false, "TAPPA_SMTP_HOST": false, "TAPPA_CACHE_DIR": false, "TAPPA_TAG_KEK": false,
		"TAPPA_MAIL_FROM": false, "TAPPA_SMTP_PORT": false, "DATABASE_URL": false,
	} {
		if got := certificateVariable(name); got != want {
			t.Errorf("certificateVariable(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestPackaging_ConfigNamesNoCertificateVariable is ADR 0022 §2's pin (b): no variable
// internal/config names — read from every non-test source of the package, the same
// spelling the manifest test scans — is a certificate authority or a trust store. ADR
// 0022 §5: "Bir CA dosyası ya da özel kök anahtarı yoktur". And the package imports
// neither crypto/x509 nor crypto/tls, so it has no way to build a pool from one.
func TestPackaging_ConfigNamesNoCertificateVariable(t *testing.T) {
	srcs, err := filepath.Glob(filepath.Join(repoRoot, "internal", "config", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	nameRe := regexp.MustCompile(`"((?:TAPPA|DATABASE)_[A-Z0-9_]+)"`)
	files := 0
	for _, path := range srcs {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		files++
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range nameRe.FindAllStringSubmatch(string(b), -1) {
			names[m[1]] = true
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, b, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range f.Imports {
			if p := strings.Trim(im.Path.Value, `"`); p == "crypto/x509" || p == "crypto/tls" {
				t.Errorf("%s imports %s: internal/config has no business building a certificate pool", filepath.Base(path), p)
			}
		}
	}
	// CONTROLS: the scan saw the package and the variables EM-3 added.
	if files == 0 || len(names) < 25 {
		t.Fatalf("scanned %d files and found %d variables; the scan has gone blind", files, len(names))
	}
	for _, must := range []string{"TAPPA_SMTP_HOST", "TAPPA_SMTP_PORT", "TAPPA_MAIL_FROM", "TAPPA_MAIL_REPLY_TO"} {
		if !names[must] {
			t.Fatalf("CONTROL FAILED: %s is not among the variables found", must)
		}
	}
	for name := range names {
		if certificateVariable(name) {
			t.Errorf("internal/config names %s, a certificate authority or trust store: a private root for the "+
				"relay is forbidden (ADR 0022 §2)", name)
		}
	}
}

// THREAT MODEL of the manifest and Dockerfile pins below (round 3, written because each
// earlier round of text-scanning pins was walked around by a new piece of syntax): these
// pins are against DRIFT that enters the manifests and the Dockerfile by accident; a
// manifest or an image written on purpose to walk around a pin is code review's. So the
// pins are FAIL-CLOSED in one precise sense: they do not parse YAML or Dockerfile syntax,
// they read the text more widely than a parser would (comments included where that is
// cheap), and a legitimate future need turns them red and updates them in the same
// change. They refuse the forms their tables list, and no more: a name built from
// innocent pieces (ARG P=SSL_ + ENV ${P}CERT_FILE — the closing auditor's E01), a
// verbatim YAML tag (!<tag:yaml.org,2002:binary> — E02) and a root file COPY'd into the
// image (R11) pass them, and the tables say so in KNOWN LIMIT rows.

// squeezed is s with every whitespace character (unicode.IsSpace: blank, tab, CR, LF and
// the rest) and every backslash removed, lower-cased. The pins search THIS: a Dockerfile
// line continuation with or without blanks, a tab or a CR after the backslash (the closing
// auditor's D-cont-ws, measured to set SSL_CERT_FILE), a folded YAML scalar and a change of
// case all spell the word with characters in between or in another case, and the squeeze
// joins them back. A YAML escape is different: the squeeze drops only its backslash, so
// "SSL_CERT_\x46ILE" is read because SSL_CERT stands before the escape, while
// "\x53SL_CERT_FILE" (an escape INSIDE SSL_CERT) survives the squeeze — the manifests'
// backslash rule is what reads it.
func squeezed(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || r == '\\' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// withoutCommentLines drops every line whose first non-blank character is '#'. Only whole
// lines: a '#' later in a line may be inside a quoted value, and cutting there is how the
// earlier comment stripper was walked around (the closing auditor's C-hash, C-hash-sq,
// D-hash). Used only by the two rules that would otherwise trip on today's comments.
func withoutCommentLines(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// rootPinFindings is pin (c), the whole rule for one file, as code with its own table
// (TestRootPinFindings) — the closing auditor's N14b removed the backslash ban and the
// package stayed green, because nothing exercised it.
//
//   - EVERY FILE (the Dockerfile and each manifest): "ssl_cert" does not occur in the
//     squeezed RAW text, COMMENTS INCLUDED. With RootCAs nil Go reads the system roots, and
//     SSL_CERT_FILE / SSL_CERT_DIR move them (ADR 0022 B32). Comments are included so that
//     no comment syntax has to be understood; today no file mentions the name at all. An
//     ARG that holds the WHOLE word (ARG P=SSL_CERT or SSL_CERT_, for a later ENV ${P}…) is
//     read; one that holds only innocent pieces of it (ARG P=SSL_ + ENV ${P}CERT_FILE, E01)
//     is not — a KNOWN LIMIT row.
//   - MANIFESTS: outside whole-line comments, no backslash (a YAML escape — \x, \u, \U —
//     can spell a name the squeeze does not join: "\x53SL_CERT_FILE") and no "!!" (the
//     !!-shorthand tags, !!binary and !!str among them, whose value no text search reads).
//     A verbatim tag (!<tag:yaml.org,2002:binary>, E02) carries no "!!" and is not read —
//     a KNOWN LIMIT row. Today: 0 backslashes and 0 tags.
//
// THE COST, counted: a legitimate future backslash in a manifest (an ingress annotation
// regex) or tag turns this red, and that change updates the pin on purpose.
func rootPinFindings(manifest bool, src string) []string {
	var out []string
	if strings.Contains(squeezed(src), "ssl_cert") {
		out = append(out, "names SSL_CERT_… (the system roots' location)")
	}
	if manifest {
		body := withoutCommentLines(src)
		if strings.Contains(body, `\`) {
			out = append(out, "carries a backslash outside a whole-line comment (a YAML escape)")
		}
		if strings.Contains(body, "!!") {
			out = append(out, "carries a YAML tag (!!)")
		}
	}
	return out
}

// TestRootPinFindings is pin (c)'s table: every row is one form the rule claims, written
// here. A row that stops being red names the half of the rule that went missing.
func TestRootPinFindings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest bool
		src      string
		want     int
	}{
		{"env entry", true, "            - name: SSL_CERT_FILE\n              value: /x\n", 1},
		{"ConfigMap key", true, "  SSL_CERT_DIR: \"/x\"\n", 1},
		{"flow style after a quoted #", true, "            - {value: \"#\", name: SSL_CERT_FILE}\n", 1},
		{"single-quoted # first", true, "  A: '#'\n  SSL_CERT_FILE: /x\n", 1},
		{"named in a comment (comments are included)", true, "  # SSL_CERT_FILE\n  A: b\n", 1},
		{"lower case", true, "  ssl_cert_file: /x\n", 1},
		{"YAML escape inside the name", true, "  \"SSL_CERT_\\x46ILE\": /x\n", 2},
		{"YAML escape at the start (only the backslash rule sees it)", true, "  \"\\x53SL_CERT_FILE\": /x\n", 1},
		{"!!binary tag", true, "  A: !!binary U1NMX0NFUlRfRklMRQ==\n", 1},
		{"Dockerfile ENV", false, "ENV SSL_CERT_FILE=/x\n", 1},
		{"Dockerfile continuation (D-cont-ws)", false, "ENV SSL_CERT_\\ \nFILE=/x\n", 1},
		{"Dockerfile continuation, no blank", false, "ENV SSL_CERT_\\\nFILE=/x\n", 1},
		// The rows that need the SQUEEZE itself: the word is split INSIDE "SSL_CERT", so the
		// text as written never contains it (round 3: without these, searching the unsqueezed
		// text stayed green — N14d).
		{"Dockerfile continuation inside the word", false, "ENV SSL_C\\ \nERT_FILE=/x\n", 1},
		{"YAML folded plain scalar inside the word", true, "            - name: SSL_\n                CERT_FILE\n", 1},
		{"another case, split", false, "ENV ssl_Ce\\\nrt_file=/x\n", 1},
		{"Dockerfile comment line inside a continuation", false, "ENV SSL_CERT_\\\n# c\nFILE=/x\n", 1},
		{"Dockerfile ARG holding the prefix (D-arg)", false, "ARG P=SSL_CERT_\nENV ${P}FILE=/x\n", 1},
		{"Dockerfile quoted # first (D-hash)", false, "ENV A=\"#\" SSL_CERT_FILE=/x\n", 1},
		// Round 4: the closing auditor's witnesses — each a real file form the rule reads and
		// a mutant of the rule did not (S04, S08, S08b, S09, S11 stayed green without them).
		{"continuation with a TAB after the backslash (S04)", false, "ENV SSL_C\\\t\nERT_FILE=/x\n", 1},
		{"continuation with CR LF after the backslash (S04)", false, "ENV SSL_C\\\r\nERT_FILE=/x\n", 1},
		{"escape on a line that also carries a quoted # (S08)", true, "  \"\\x53SL_CERT_FILE\": '#'\n", 1},
		{"!!binary key with a trailing comment (S08)", true, "  ? !!binary U1NMX0NFUlRfRklMRQ==  # base64\n  : \"/x\"\n", 1},
		{"escape after a quoted # in a flow mapping (S08b)", true, "            - {value: '#', name: \"\\x53SL_CERT_FILE\"}\n", 1},
		{"ARG holding SSL_CERT without the underscore (S09)", false, "ARG P=SSL_CERT\nENV ${P}_FILE=/x\n", 1},
		{"\\u escape (S11)", true, "  \"\\u0053SL_CERT_FILE\": /x\n", 1},
		{"\\U escape (S11)", true, "  \"\\U00000053SL_CERT_FILE\": /x\n", 1},
		{"!!str tag (S10: the rule's word is !!, not !!binary)", true, "  TAPPA_X: !!str x\n", 1},
		// KNOWN LIMIT rows (round 4): measured forms the rule does NOT read. Green while the
		// limit stands; a change that closes one turns its row red and updates it here.
		{"KNOWN LIMIT: a name built from innocent pieces (E01)", false, "ARG P=SSL_\nENV ${P}CERT_FILE=/x\n", 0},
		{"KNOWN LIMIT: a verbatim YAML tag (E02)", true, "  ? !<tag:yaml.org,2002:binary> U1NMX0NFUlRfRklMRQ==\n  : \"/x\"\n", 0},
		{"KNOWN LIMIT: a root file COPY'd into the image (R11)", false, "COPY corp-root.pem /etc/ssl/certs/corp-root.pem\n", 0},
		// Negative rows: what the shipped files legitimately carry.
		{"a backslash in a whole-line manifest comment", true, "#   grep -i 'cf-ray\\|server'\n  A: b\n", 0},
		{"an ssl annotation", true, "    nginx.ingress.kubernetes.io/ssl-redirect: \"true\"\n", 0},
		{"Dockerfile continuations", false, "ENV GOOSE_DRIVER=postgres \\\n    GOOSE_MIGRATION_DIR=/migrations\n", 0},
		{"a Dockerfile backslash is not a manifest's", false, "RUN a \\\n  b\n", 0},
	} {
		if got := rootPinFindings(tc.manifest, tc.src); len(got) != tc.want {
			t.Errorf("%s: %d findings %v, want %d", tc.name, len(got), got, tc.want)
		}
	}
}

// pinFile is one file pin (c) reads, by its path from the repository root, and whether it
// is a manifest (the backslash and tag rules apply to manifests only).
type pinFile struct {
	rel      string
	manifest bool
}

// rootPinFiles is the set pin (c) reads: every TOP-LEVEL manifest (deploy/k8s/*.yaml — a
// subdirectory is not read, the closing auditor's R06c) and the Dockerfile. It is a
// function with its own test (TestRootPinFiles) because the third eye's W01 (the
// Dockerfile dropped from the loop) and W04 (20-app.yaml skipped) changed the set and left
// every rule row green.
func rootPinFiles(t *testing.T) []pinFile {
	t.Helper()
	manifests, err := filepath.Glob(filepath.Join(repoRoot, "deploy", "k8s", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var out []pinFile
	for _, m := range manifests {
		rel, err := filepath.Rel(repoRoot, m)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, pinFile{rel, true})
	}
	return append(out, pinFile{"Dockerfile", false})
}

// TestRootPinFiles: the set holds the three sources of the serving process's environment —
// the Dockerfile (the image's ENV, read as a Dockerfile), 20-app.yaml (env:) and
// 05-config.yaml (the envFrom ConfigMap), both read as manifests — and at least 8
// manifests in all.
func TestRootPinFiles(t *testing.T) {
	got := map[string]pinFile{}
	manifests := 0
	for _, f := range rootPinFiles(t) {
		got[f.rel] = f
		if f.manifest {
			manifests++
		}
	}
	for _, want := range []pinFile{
		{"Dockerfile", false},
		{filepath.Join("deploy", "k8s", "20-app.yaml"), true},
		{filepath.Join("deploy", "k8s", "05-config.yaml"), true},
	} {
		if f, ok := got[want.rel]; !ok || f != want {
			t.Errorf("pin (c)'s file set lacks %s as manifest=%v (got %+v, present=%v)", want.rel, want.manifest, f, ok)
		}
	}
	if manifests < 8 {
		t.Errorf("pin (c) reads only %d manifests; the scan has gone blind", manifests)
	}
}

// TestPackaging_NothingMovesTheSystemRoots is ADR 0022 §2's pin (c), FAIL-CLOSED (round 3;
// the threat model above): rootPinFindings over every file of rootPinFiles — the top-level
// manifests and the Dockerfile, whose ENV is the third source of the serving process's
// environment (a widening of the ADR's "hizmet konteynerinin manifesti"). Every file is
// counted after it is scanned, so a file skipped inside the loop is red too. Today all of
// them pass; the proof is this test being green on the shipped tree.
//
// WHAT THIS DOES NOT SEE: an image whose own root store was replaced or that COPYs a root
// file into /etc/ssl/certs (ADR 0022 counted limit 23), a manifest in a subdirectory or
// any channel outside these files (kustomize, Helm values, `kubectl set env`, a key added
// by hand to the live ConfigMap), and a name assembled from innocent pieces (E01) — the
// threat model's "written on purpose" and the KNOWN LIMIT rows above.
func TestPackaging_NothingMovesTheSystemRoots(t *testing.T) {
	files := rootPinFiles(t)
	scanned := 0
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(repoRoot, f.rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, finding := range rootPinFindings(f.manifest, string(b)) {
			t.Errorf("%s %s: that can move the roots the relay's certificate is verified against — the private "+
				"root pool ADR 0022 §2 forbids, without a line of Go", f.rel, finding)
		}
		scanned++
	}
	if scanned != len(files) {
		t.Errorf("scanned %d of the %d files of pin (c)'s set", scanned, len(files))
	}
}

// mountFindings is the mount pin's rule for the app manifest, FAIL-CLOSED: its squeezed
// RAW text, comments included, names no volumeMounts and no mountPath at all. What Go on
// Linux reads (go1.27.1 crypto/x509/root_linux.go, read): of certFiles — six bundle paths
// under /etc/ssl and /etc/pki — only the FIRST that exists ("stop after finding one"),
// and EVERY file of the two certDirectories, /etc/ssl/certs and /etc/pki/tls/certs. So a
// mount that replaces the first existing bundle, or adds one PEM to either directory (a
// subPath mount), is a trusted root, and so is a mount at an ancestor (/etc/ssl, /etc, /).
// "No mount at all" is wider than those paths, on the safe side. Round 2 parsed the mounts
// and compared paths; the closing auditor's M-flow (a flow sequence on the volumeMounts
// line, the style 20-app.yaml already uses for `drop: ["ALL"]`) went past the parser.
// Today the app manifest declares no mount (kubelet still mounts /etc/hosts,
// /etc/resolv.conf, /etc/hostname and /dev/termination-log itself — undeclared, not this
// pin's), so the rule is "no declared mount": the first legitimate one turns this red, and
// that change writes the path rule it needs.
func mountFindings(src string) []string {
	var out []string
	sq := squeezed(src)
	for _, word := range []string{"volumemounts", "mountpath"} {
		if strings.Contains(sq, word) {
			out = append(out, "names "+word)
		}
	}
	return out
}

// TestMountFindings is the mount rule's table.
func TestMountFindings(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want int
	}{
		{"block style (M-block)", "          volumeMounts:\n            - name: r\n              mountPath: /etc/ssl/certs/x.pem\n              subPath: x.pem\n", 2},
		{"flow style on the key's line (M-flow)", "          volumeMounts: [{name: r, mountPath: /etc/ssl/certs/x.pem, subPath: x.pem}]\n", 2},
		{"JSON style", "{\"volumeMounts\": [{\"mountPath\": \"/etc\"}]}\n", 2},
		{"another case", "          VolumeMounts: []\n", 1},
		{"named in a comment (comments are included)", "          # volumeMounts are not used here\n", 1},
		{"a pod without mounts", "      containers:\n        - name: tappa\n          image: x\n", 0},
		// Round 4 (the third eye's W02/W03 as a property of the rule): a whole pod, the mount
		// in one container only — the rule reads the whole text, so either is a finding.
		{"whole pod: clean init container, mounted serving container", "      initContainers:\n        - name: wait\n          image: x\n      containers:\n        - name: tappa\n          image: x\n          volumeMounts: [{name: r, mountPath: /etc/ssl/certs/x.pem, subPath: x.pem}]\n      volumes: [{name: r, configMap: {name: corp}}]\n", 2},
		{"whole pod: mounted init container, clean serving container", "      initContainers:\n        - name: wait\n          image: x\n          volumeMounts:\n            - name: r\n              mountPath: /etc/ssl\n      containers:\n        - name: tappa\n          image: x\n", 2},
	} {
		if got := mountFindings(tc.src); len(got) != tc.want {
			t.Errorf("%s: %d findings %v, want %d", tc.name, len(got), got, tc.want)
		}
	}
}

// TestPackaging_NoMountShadowsTheSystemRoots: the app manifest declares no mount
// (mountFindings, fail-closed; round 2 was the security audit's LOW — ADR 0022 counted limit
// 23 named only "a volume over /etc/ssl/certs"). Over-approximation, on purpose: the whole
// file is read, so a mount in the init container counts too.
//
// KNOWN LIMIT (round 4, the third eye's W02/W03): which text this test hands to
// mountFindings is its own oracle — a change that slices the file at the call site (only
// the containers after the first, only the first container) is not caught. The rule's
// table reads whole pods; this body's single call is code review's.
func TestPackaging_NoMountShadowsTheSystemRoots(t *testing.T) {
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range mountFindings(string(appSrc)) {
		t.Errorf("deploy/k8s/20-app.yaml %s: the app manifest declares no mount today, and a mount at, below or above "+
			"/etc/ssl or /etc/pki would replace or add to the system roots — the private root pool ADR 0022 §2 "+
			"forbids. A legitimate mount updates this pin with a path rule in the same change", finding)
	}
}

// configMapValues returns the tappa-config ConfigMap's keys with their values, one layer
// of quotes removed.
func configMapValues(t *testing.T, src string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s{2}([A-Z][A-Z0-9_]*):[ \t]*(.*?)[ \t]*$`).
		FindAllStringSubmatch(stripYAMLComments(src), -1) {
		out[m[1]] = unquote(m[2])
	}
	return out
}

// TestPackaging_TheSMTPCredentialsAreOptionalSecretKeys pins, for each variable in
// internal/config's OWN list of the two credentials, what 20-app.yaml's comment calls
// load-bearing: an env entry of the serving container, from a secretKeyRef to
// tappa-secrets under its own name, `optional: true` (the Secret holds neither until
// the runbook — kubelet would refuse the pod on a missing key); and NOT a ConfigMap key
// (a credential in a ConfigMap is not a secret).
func TestPackaging_TheSMTPCredentialsAreOptionalSecretKeys(t *testing.T) {
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfgSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	item := listItemNamed(blockUnder(stripYAMLComments(string(appSrc)), "containers:"), servingContainer)
	if item == nil {
		t.Fatalf("container %q not found", servingContainer)
	}
	envBlock := strings.Join(blockUnder(strings.Join(item, "\n"), "env:"), "\n")
	entry := func(name string) string {
		i := strings.Index(envBlock, "- name: "+name+"\n")
		if i < 0 {
			return ""
		}
		rest := envBlock[i+1:]
		if j := strings.Index(rest, "- name: "); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	cm := configMapKeys(t, string(cfgSrc))
	names := config.SMTPCredentialVariables()
	if len(names) != 2 {
		t.Fatalf("PREMISE: internal/config lists %d credential variables, want 2", len(names))
	}
	for _, name := range names {
		e := entry(name)
		switch {
		case e == "":
			t.Errorf("%s is not an env entry of the serving container", name)
			continue
		case !strings.Contains(e, "secretKeyRef:"):
			t.Errorf("%s does not come from a secretKeyRef", name)
		case !regexp.MustCompile(`(?m)^\s+name: tappa-secrets$`).MatchString(e):
			t.Errorf("%s does not come from tappa-secrets", name)
		case !regexp.MustCompile(`(?m)^\s+key: ` + name + `$`).MatchString(e):
			t.Errorf("%s does not read the Secret key of its own name", name)
		case !regexp.MustCompile(`(?m)^\s+optional: true$`).MatchString(e):
			t.Errorf("%s is not `optional: true`: every deploy before the e-mail runbook would stall on a missing key", name)
		}
		if cm[name] {
			t.Errorf("%s is a ConfigMap key: a credential there is in git and in every reader of the ConfigMap", name)
		}
	}
	// CONTROL: the slicing isolates ONE entry -- a required neighbour is not optional.
	if e := entry("TAPPA_TAG_KEK"); e == "" || strings.Contains(e, "optional: true") {
		t.Fatal("CONTROL FAILED: the entry slicer does not isolate TAPPA_TAG_KEK as a required entry")
	}
}

// TestPackaging_TheConfigMapShipsTodaysDelivery: EM-3 and EM-5 change no behaviour (ADR
// 0022 §5, §12). The ConfigMap's two delivery modes are none and panel, and neither is
// overridden by an explicit env entry of the serving container (which would win over
// envFrom). Turning a flow to email is a DEPLOY decision (ADR 0022 §12: after EM-5 and
// the user's external steps for resets, after EM-7 for invitations), and the change
// that makes it updates this test on purpose.
func TestPackaging_TheConfigMapShipsTodaysDelivery(t *testing.T) {
	cfgSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cm := configMapValues(t, string(cfgSrc))
	if cm["TAPPA_ENV"] != "prod" {
		t.Fatalf("CONTROL FAILED: the ConfigMap parse reads TAPPA_ENV as %q", cm["TAPPA_ENV"])
	}
	env := containerEnvEntries(t, string(appSrc), servingContainer)
	for name, want := range map[string]string{
		"TAPPA_RESET_DELIVERY":  config.ResetDeliveryNone,
		"TAPPA_INVITE_DELIVERY": config.InviteDeliveryPanel,
	} {
		got, ok := cm[name]
		switch {
		case !ok:
			t.Errorf("%s is not a ConfigMap key", name)
		case got != want:
			t.Errorf("the ConfigMap ships %s=%q; EM-3 ships %q — switching a flow to email is a deploy decision "+
				"taken after its channel exists and the user's SES steps are done (ADR 0022 §12; the reset channel "+
				"exists since EM-5, the invitation's since EM-7B), and the change that takes it updates this test",
				name, got, want)
		}
		if _, overridden := env[name]; overridden {
			t.Errorf("%s is an env entry of the serving container: it would override the ConfigMap's value", name)
		}
	}
}

// TestPackaging_TheConfigMapsMailSettingsLoadInProduction: the transport settings the
// ConfigMap ships are not read while both flows are off — so nothing at boot would say
// they are wrong until the day a flow is switched on. This loads the ConfigMap as a
// production process would see it, with both flows switched to email and stand-in
// credentials, and requires config.Load to accept it: a 465, an address for a host or a
// malformed sender in the ConfigMap is red HERE, not at that deploy.
func TestPackaging_TheConfigMapsMailSettingsLoadInProduction(t *testing.T) {
	cfgSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cm := configMapValues(t, string(cfgSrc))
	for _, must := range []string{"TAPPA_SMTP_HOST", "TAPPA_SMTP_PORT", "TAPPA_MAIL_FROM", "TAPPA_MAIL_REPLY_TO"} {
		if _, ok := cm[must]; !ok {
			t.Fatalf("%s is not a ConfigMap key", must)
		}
	}
	// The Secret's half, as stand-ins: obviously fake and distinct (agent-brief md. 2).
	for _, name := range append(append(config.OperatorSurfaceVariables(), "DATABASE_MIGRATE_URL", "TAPPA_TAG_KEK_PREVIOUS"),
		config.SMTPCredentialVariables()...) {
		t.Setenv(name, "")
	}
	t.Setenv("DATABASE_URL", "postgres://tappa_app@127.0.0.1:1/tappa?sslmode=disable")
	t.Setenv("TAPPA_SESSION_HMAC_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("S"), 32)))
	t.Setenv("TAPPA_TAG_KEK", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("K"), 32)))
	t.Setenv("TAPPA_INVITE_HMAC_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("I"), 32)))
	creds := config.SMTPCredentialVariables()
	t.Setenv(creds[0], "jkq4wvz8xqp2zkv6")
	t.Setenv(creds[1], "qzx7vjw9kpq3xzt8")
	for k, v := range cm {
		t.Setenv(k, v)
	}
	t.Setenv("TAPPA_RESET_DELIVERY", config.ResetDeliveryEmail)
	t.Setenv("TAPPA_INVITE_DELIVERY", config.InviteDeliveryEmail)
	c, err := config.Load()
	if err != nil {
		t.Fatalf("the ConfigMap's transport settings do not load in production with both flows on: %v", err)
	}
	if !c.IsProd() || c.Mail.Host != cm["TAPPA_SMTP_HOST"] || c.Mail.From != cm["TAPPA_MAIL_FROM"] {
		t.Errorf("PREMISE: prod=%v, host %q, from %q — not the ConfigMap's", c.IsProd(), c.Mail.Host, c.Mail.From)
	}
}

// TestUnbuiltDelivery_RefusesEmailForEitherFlow: the decision, as a table. Since M10
// EM-5 the reset flow's "email" is BUILT, and since M10 EM-7B the invitation's is too
// (each is a case in run()'s switches), so all four combinations config.Load accepts
// are accepted here. The test keeps its name — it is the same table, and its name is
// cited by ADR 0022 and the M10 card — and what it still refuses is a value OUTSIDE
// the closed sets, which is the shape the function exists for: a value added to
// internal/config without a channel in this build. The refusal names the variable,
// the two values to choose from, and never the value it was given.
func TestUnbuiltDelivery_RefusesEmailForEitherFlow(t *testing.T) {
	const stranger = "sms-zq7x"
	for _, tc := range []struct {
		reset, invite string
		want          []string // nil: accepted
	}{
		{config.ResetDeliveryNone, config.InviteDeliveryPanel, nil},
		{config.ResetDeliveryEmail, config.InviteDeliveryPanel, nil},
		{config.ResetDeliveryNone, config.InviteDeliveryEmail, nil},
		{config.ResetDeliveryEmail, config.InviteDeliveryEmail, nil},
		{stranger, config.InviteDeliveryPanel, []string{"TAPPA_RESET_DELIVERY", "set it to none or email"}},
		{config.ResetDeliveryNone, stranger, []string{"TAPPA_INVITE_DELIVERY", "set it to panel or email"}},
		{config.ResetDeliveryEmail, "", []string{"TAPPA_INVITE_DELIVERY"}},
	} {
		err := unbuiltDelivery(&config.Config{ResetDelivery: tc.reset, InviteDelivery: tc.invite})
		switch {
		case tc.want == nil && err != nil:
			t.Errorf("%s/%s: refused: %v", tc.reset, tc.invite, err)
		case tc.want != nil && err == nil:
			t.Errorf("%q/%q: accepted; this build implements no such delivery", tc.reset, tc.invite)
		case tc.want != nil:
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("%q/%q: the refusal does not say %q: %v", tc.reset, tc.invite, w, err)
				}
			}
			if strings.Contains(err.Error(), stranger) {
				t.Errorf("%q/%q: the refusal repeats the value it was given: %v", tc.reset, tc.invite, err)
			}
		}
	}
}

// TestArtifact_RefusesAnEmailDeliveryThisBuildLacks drives THE SHIPPED BINARY: with a
// complete, valid transport configuration, a delivery this build does not have stops
// the boot with a fatal line naming its variable — before the database is dialled
// (the database here is a closed port, so a boot that got past the refusal would fail
// on the dial and name no flow). And nothing it prints carries the credentials it was
// given.
//
// SINCE M10 EM-7B EVERY "email" IS BUILT — the reset flow's since EM-5, the
// invitation's since EM-7B — so the three e-mail rows now expect the opposite of what
// this test was written for: the boot gets past the refusal and fails on the dial,
// naming no flow. What a build still LACKS is a value outside the closed sets, and
// that row ("sms" for invitations) is refused at boot naming TAPPA_INVITE_DELIVERY
// (config.Load's refusal reaches the binary's fatal line first).
//
// CONTROL: the same environment with both flows off fails on the dial too, so the
// e-mail rows' answer is the dial, not a quieter refusal.
func TestArtifact_RefusesAnEmailDeliveryThisBuildLacks(t *testing.T) {
	bin := theArtifact(t)
	creds := config.SMTPCredentialVariables()
	// The same sentinels as internal/config's tests: no 4 consecutive characters of either
	// form a word, so every 4-byte window can be searched in the process's output.
	const user, pass = "jkq4wvz8xqp2zkv6", "qzx7vjw9kpq3xzt8"
	boot := func(reset, invite string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin)
		cmd.Dir = t.TempDir()
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"DATABASE_URL=postgres://tappa_app@127.0.0.1:1/tappa?sslmode=disable",
			"TAPPA_ADDR=127.0.0.1:0",
			"TAPPA_ENV=dev",
			"TAPPA_SESSION_HMAC_KEY=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("S"), 32)),
			"TAPPA_TAG_KEK=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("K"), 32)),
			"TAPPA_INVITE_HMAC_KEY=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("I"), 32)),
			"TAPPA_RETENTION_YEARS=2",
			"TAPPA_RESET_DELIVERY=" + reset,
			"TAPPA_INVITE_DELIVERY=" + invite,
			"TAPPA_SMTP_HOST=smtp.example.com",
			"TAPPA_MAIL_FROM=Taptime <no-reply@taptime.mt>",
			creds[0] + "=" + user,
			creds[1] + "=" + pass,
		}
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("%s/%s: the process exited 0 against a closed database port", reset, invite)
		}
		for _, cred := range []string{user, pass} {
			for i := 0; i+4 <= len(cred); i++ {
				if strings.Contains(string(out), cred[i:i+4]) {
					t.Errorf("%s/%s: the process printed 4 bytes of a credential it was given (offset %d)", reset, invite, i)
					break
				}
			}
		}
		return string(out)
	}
	// A delivery this build lacks: refused at boot, naming the variable.
	if out := boot(config.ResetDeliveryNone, "sms"); !regexp.MustCompile(`(?m)fatal.*TAPPA_INVITE_DELIVERY`).MatchString(out) {
		t.Errorf("none/sms: the boot did not stop on the delivery mode (looking for a fatal line naming "+
			"TAPPA_INVITE_DELIVERY). Its whole output was:\n%s", out)
	}
	// Every "email" is built: the boot passes the refusal and stops on the closed
	// database port, naming no flow (the reset flow's since EM-5, the invitation's
	// since EM-7B).
	for _, tc := range []struct{ reset, invite string }{
		{config.ResetDeliveryEmail, config.InviteDeliveryPanel},
		{config.ResetDeliveryNone, config.InviteDeliveryEmail},
		{config.ResetDeliveryEmail, config.InviteDeliveryEmail},
	} {
		if out := boot(tc.reset, tc.invite); !strings.Contains(out, "fatal") || strings.Contains(out, "_DELIVERY") {
			t.Errorf("%s/%s: this build implements both e-mail channels, so the boot should pass the delivery "+
				"refusal and fail on the database dial, naming no flow:\n%s", tc.reset, tc.invite, out)
		}
	}
	out := boot(config.ResetDeliveryNone, config.InviteDeliveryPanel)
	if !strings.Contains(out, "fatal") || strings.Contains(out, "_DELIVERY=") {
		t.Errorf("CONTROL FAILED: with both flows off the boot should fail on the database dial and name no flow:\n%s", out)
	}
}
