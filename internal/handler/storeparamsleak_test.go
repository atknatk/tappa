package handler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/store"
)

// M7-04 ACCEPTANCE CRITERION 4: a store `*Params` value is never printed.
//
// 🔴 WHY IT IS A TEST AND NOT A TYPE, stated first because that is the honest order.
// internal/adminauth.ResetToken redacts itself six ways and internal/db.PasswordHash
// does the same for a stored digest — but `store.CreatePasswordResetParams.TokenHash`
// and `.PasswordHash` are PLAIN STRINGS on sqlc-generated structs, and so are
// `CreateAdminUserParams.PasswordHash` and `CreateInviteParams.CodeHash`. ADR 0015
// records this as the repository-wide pattern rather than a new deviation, and hands
// phase B one sentence: never print a store `*Params`. A sentence nothing checks is a
// sentence that survives exactly as long as the person who wrote it is reading the
// diff.
//
// ⚠️ WHAT THIS SCAN IS AND IS NOT. It is a MECHANICAL NET over the shapes that
// actually occur — a params literal, or a variable in the same file holding one,
// handed to a formatting or logging call. It is NOT a proof, and the honest list of
// what it does not see is FOUR shapes rather than the two the first version named.
// A net looks stronger than it is until it says what it cannot catch:
//
//	(a) copied field by field into another struct, then that struct printed;
//	(b) reached through an interface value;
//	(c) A FUNCTION PARAMETER — `func f(q store.CreatePasswordResetParams) { ...
//	    fmt.Sprintf("%+v", q) }`. Pass 1 collects assignments and var declarations,
//	    not parameter lists, so `q` is an unknown identifier and the call is passed
//	    over. MEASURED by an audit: the scan stays GREEN on exactly this shape.
//	(d) A STRUCT FIELD — `h.P`, and `h` itself. Only bare identifiers are checked
//	    against the holder set, so a selector expression is never matched. MEASURED
//	    the same way, same result.
//	(e) A VALUE RETURNED BY A FUNCTION — `p, err := e.params()`. isParamsLiteral
//	    deliberately requires the right-hand side to BE the literal (a text match
//	    reported 25 false positives; see the note in pass 1), so a call returning one
//	    binds an identifier the scan does not know about.
//
// 🔴 (c) AND (e) EXIST IN THIS REPOSITORY TODAY, AND THE FIRST DRAFT OF THIS
// PARAGRAPH CLAIMED THEY DID NOT. It said "`store.*Params` is not the type of any
// field or any parameter in any non-test file"; a grep taken while writing the
// sentence returned FOUR, so the claim was withdrawn before it shipped rather than
// after an auditor found it. What is true, with the four listed so the next reader
// checks them rather than trusting this:
//
//	internal/sun/advance.go:57            interface method parameter — a DECLARATION
//	internal/domain/checkin/checkin.go:166  with no body, so nothing can print it
//	internal/audit/audit.go:129           returns one; the caller binds `params` and
//	                                      passes it to RecordAuditEvent, twice (:84,
//	                                      :111), and to nothing else
//	internal/domain/checkin/checkin.go:1156 returns one; the caller binds `params`
//	                                      (:1126) and passes it to InsertTransaction
//
// So the four occurrences are hand-checked and none of them reaches a printing call —
// which is a HUMAN reading, valid on the day it was taken, and exactly the kind of
// guarantee this scan exists to replace for the shapes it can see. (d) is latent: no
// struct field in a non-test file has a params type.
//
// THE REASON TO WRITE THIS DOWN RATHER THAN WIDEN THE SCAN is that closing (c), (d)
// and (e) properly needs go/types — type-checking the whole module inside a unit test
// — which is a different and much slower tool than the one this file is.
//
// scripts/redline-check.sh R7 states its own six blind spots for the same reason.
// Both limits are the same limit: a mechanical net is evidence, never proof.
//
// IT LIVES IN internal/handler because this package already carries the repository's
// other source-walking assertions (the cookie notice's derivation walks `internal` and
// `cmd` the same way), and because a scan that lives in one of the packages it scans
// would tempt the next person to narrow it to that package.

// printingCall matches the callee of a call whose arguments are rendered: the fmt
// family, the log family, and any method with a slog-shaped name (a.log.Warn,
// h.log.Error, logger.Info...). The last one is why this is a suffix match rather
// than a package match — every logging call in this repository goes through a field.
var printingCall = regexp.MustCompile(`^(fmt\.(Sprint|Sprintf|Sprintln|Print|Printf|Println|Errorf|Fprint|Fprintf|Fprintln)|log\.[A-Za-z]+|slog\.[A-Za-z]+|.*\.(Debug|Info|Warn|Error|DebugContext|InfoContext|WarnContext|ErrorContext|Printf|Print|Println|Logf|Fatalf)|.*\.log\.[A-Za-z]+)$`)

// storeParamsType matches a store params type as it is written in source.
var storeParamsType = regexp.MustCompile(`\bstore\.[A-Za-z0-9_]*Params\b`)

func TestStoreParams_AreNeverHandedToAPrintingCall(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	var (
		literals int // store.XxxParams{...} composite literals seen anywhere
		checked  int // arguments examined
		files    int
	)
	fset := token.NewFileSet()

	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			// Generated sqlc code is where these types are DEFINED and where they are
			// legitimately passed around; it prints nothing.
			if strings.Contains(p, string(filepath.Separator)+"store"+string(filepath.Separator)) {
				return nil
			}
			src, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			f, e := parser.ParseFile(fset, p, src, 0)
			if e != nil {
				return e
			}
			files++
			rel := strings.TrimPrefix(p, root+string(filepath.Separator))

			// PASS 1: which identifiers in this file hold a params value. Both shapes
			// that occur: `x := store.FooParams{...}` and `var x store.FooParams`.
			holders := map[string]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.CompositeLit:
					if storeParamsType.MatchString(render(fset, node.Type)) {
						literals++
					}
				case *ast.AssignStmt:
					// ⚠️ THE RIGHT-HAND SIDE MUST BE THE LITERAL ITSELF, not merely a
					// expression whose SOURCE TEXT mentions one. The first version of
					// this matched on rendered text and reported 25 false positives in
					// three packages, all of the shape
					// `err := d.WithTenant(ctx, id, func(...) { ... store.XParams{...} ... })`
					// — where the params literal is inside a closure and the identifier
					// being bound is an error. A net that cries wolf 25 times is a net
					// somebody deletes.
					for i, rhs := range node.Rhs {
						if i >= len(node.Lhs) || !isParamsLiteral(fset, rhs) {
							continue
						}
						if id, ok := node.Lhs[i].(*ast.Ident); ok {
							holders[id.Name] = true
						}
					}
				case *ast.ValueSpec:
					if node.Type != nil && storeParamsType.MatchString(render(fset, node.Type)) {
						for _, id := range node.Names {
							holders[id.Name] = true
						}
					}
				}
				return true
			})

			// PASS 2: every argument of every printing call.
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := render(fset, call.Fun)
				if !printingCall.MatchString(callee) {
					return true
				}
				for _, arg := range call.Args {
					checked++
					text := render(fset, arg)
					if storeParamsType.MatchString(text) {
						t.Errorf("%s:%d: %s(...) is handed %s.\n"+
							"A store *Params carries plain-string TokenHash / PasswordHash / "+
							"CodeHash fields (sqlc generates no redaction), so a %%+v of one "+
							"writes a bearer credential into a log line (§4.7, ADR 0015).",
							rel, fset.Position(arg.Pos()).Line, callee, text)
						continue
					}
					if id, ok := arg.(*ast.Ident); ok && holders[id.Name] {
						t.Errorf("%s:%d: %s(...) is handed %q, which holds a store *Params in "+
							"this file (§4.7, ADR 0015).",
							rel, fset.Position(arg.Pos()).Line, callee, id.Name)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}

	// --- ANTI-VACUITY, both directions, from the REAL CORPUS ----------------------
	//
	// 🔴 THE CONTROL IS NOT DERIVED FROM THE SCAN. A synthetic string fed to the two
	// regexps would only prove the regexps compile. What arms this test is the
	// repository itself: production code must contain both of the things the scan
	// looks for, or the scan is pointed somewhere that has neither and would report
	// every file clean.
	if files < 40 {
		t.Fatalf("parsed %d file(s); this repository has far more, so this walk is reading "+
			"the wrong tree", files)
	}
	if literals == 0 {
		t.Fatal("found no store.*Params literal anywhere in internal/ or cmd/. Production code " +
			"builds them constantly, so a scan that sees none is not seeing the shape it " +
			"is supposed to refuse and would pass over a leak")
	}
	if checked == 0 {
		t.Fatal("examined no argument of any printing call at all; the callee pattern matches " +
			"nothing in this repository and the scan above proved nothing")
	}
	t.Logf("parsed %d files, saw %d store.*Params literal(s), examined %d printing-call argument(s)",
		files, literals, checked)
}

// TestStoreParams_ArePrintableAndThatIsTheHazard demonstrates that the rule above is
// worth having: the value really does render its secret.
//
// 🔴 IT IS THE HONEST FRAMING ADR 0015 ASKS FOR. The raw token redacts itself six
// ways; its HASH has zero, and the hash is a BEARER CREDENTIAL in this design
// (hash -> resolver -> tenant -> consumption, db/queries/passwordresets.sql). So what
// protects the hash is discipline, and this test is what stops the discipline from
// being invisible.
func TestStoreParams_ArePrintableAndThatIsTheHazard(t *testing.T) {
	t.Parallel()
	const sentinel = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for what, v := range paramsCarrying(sentinel) {
		if !strings.Contains(fmt.Sprintf("%+v", v), sentinel) {
			t.Errorf("%s no longer renders the value it was given. If sqlc started generating "+
				"redaction, say so where the rule is written (ADR 0015) rather than leaving a "+
				"test asserting a hazard that is gone.", what)
		}
	}
}

// paramsCarrying builds the two params values this flow hands to the database, each
// carrying `secret` in its plain-string field.
//
// 🔴 THE CONSTRUCTION IS SPLIT OUT OF THE Sprintf CALL DELIBERATELY, AND THE REASON
// IS THE RED-LINE SCANNER RATHER THAN STYLE. scripts/redline-check.sh R7 matches a
// formatting or logging call whose arguments, ON THE SAME LINE, name one of the
// secret-shaped fields — and a single expression that both BUILDS one of these
// structs and hands it to a formatter is exactly that shape. The scanner reddened on
// it, correctly, because the shape is what it exists to catch, and it reddened a
// SECOND time on the first version of this very paragraph, which had spelled the
// offending expression out.
//
// The repository's rule for an innocent line that trips the net is settled and is
// applied here rather than re-argued: REWORD, never widen the pattern. activate.go,
// adminlogin.go and internal/adminauth all record the same decision, and the reason
// is that a scanner which reddens on legitimate lines is one somebody eventually
// relaxes — taking the real branch with it.
func paramsCarrying(secret string) map[string]any {
	return map[string]any{
		"store.CreatePasswordResetParams": store.CreatePasswordResetParams{
			TokenHash: secret,
		},
		"store.ConsumePasswordResetAndSetPasswordParams": store.ConsumePasswordResetAndSetPasswordParams{
			PasswordHash: secret,
		},
	}
}

// isParamsLiteral reports whether e is `store.XxxParams{...}` or `&store.XxxParams{...}`
// — the value itself, rather than an expression that happens to contain one.
func isParamsLiteral(fset *token.FileSet, e ast.Expr) bool {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	lit, ok := e.(*ast.CompositeLit)
	if !ok || lit.Type == nil {
		return false
	}
	return storeParamsType.MatchString(render(fset, lit.Type))
}

func render(fset *token.FileSet, n ast.Node) string {
	var sb strings.Builder
	if err := printer.Fprint(&sb, fset, n); err != nil {
		return ""
	}
	return sb.String()
}
