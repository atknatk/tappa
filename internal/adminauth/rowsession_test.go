package adminauth

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// rowsession_test.go -- T187, ADR 0036 (migration 00086): a panel session on a single admin_users row is
// born in identity_open_row_session, which holds the password proof in the database, and nowhere else.
// Until the contract revokes tappa_app's INSERT on admin_sessions, the only thing that keeps the binary
// from sending the old INSERT is this test.

// openRowSessionCallers names, by package directory and enclosing function, every non-test call of a
// function called OpenRowSession in internal/... and cmd/...: the sign-in (after its Proof held) and the
// legacy side of a link's proof (after the password matched the resolver's digest). A new entry is a
// new way to birth a session.
var openRowSessionCallers = map[string]bool{
	"internal/adminauth.Issue":                  true,
	"internal/adminauth.openLegacyProofSession": true,
}

// sessionInsert is an INSERT into admin_sessions in a string literal, any spelling the server accepts.
var sessionInsert = regexp.MustCompile(`(?is)\binsert\s+into\s+(?:(?:"public"|public)\s*\.\s*)?(?:"admin_sessions"|admin_sessions\b)`)

// TestOpenRowSession_HasItsTwoCallersAndTheOldInsertNone reads every non-test Go file of internal/ and
// cmd/: db.OpenRowSession is called from exactly openRowSessionCallers; the generated CreateAdminSession
// (the previous binary's statement, kept for the expand deploy) is called from nowhere; and no string
// literal outside internal/store (sqlc's output, where the generated text lives until the contract
// deletes the query) inserts into admin_sessions.
func TestOpenRowSession_HasItsTwoCallersAndTheOldInsertNone(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	var oldCalls, literals []string
	files := 0
	for _, root := range []string{filepath.Join(repo, "internal"), filepath.Join(repo, "cmd")} {
		err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			files++
			dir, err := filepath.Rel(repo, filepath.Dir(path))
			if err != nil {
				return err
			}
			dir = filepath.ToSlash(dir)
			generated := dir == "internal/store"
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || generated {
					return true
				}
				if v, err := strconv.Unquote(lit.Value); err == nil && sessionInsert.MatchString(v) {
					literals = append(literals, dir+"/"+filepath.Base(path))
				}
				return true
			})
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					switch sel.Sel.Name {
					case "OpenRowSession":
						found[dir+"."+fn.Name.Name] = true
					case "CreateAdminSession":
						oldCalls = append(oldCalls, dir+"."+fn.Name.Name)
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files < 100 {
		t.Fatalf("the scan read %d files; it is not reading the tree", files)
	}
	for caller := range found {
		if !openRowSessionCallers[caller] {
			t.Errorf("%s calls OpenRowSession; a panel session is born only by the sign-in and the link's proof (ADR 0036)", caller)
		}
	}
	for caller := range openRowSessionCallers {
		if !found[caller] {
			t.Errorf("%s is listed but the scan did not find it", caller)
		}
	}
	for _, c := range oldCalls {
		t.Errorf("%s reaches CreateAdminSession -- tappa_app's own INSERT; a session is born in identity_open_row_session (ADR 0036)", c)
	}
	for _, l := range literals {
		t.Errorf("%s holds an INSERT into admin_sessions; a session is born in identity_open_row_session (ADR 0036)", l)
	}
}

// TestOpenRowSession_TheInsertPatternSeesEverySpelling is the scanner's own control: each spelling the
// server accepts is seen, and an INSERT into another table is not.
func TestOpenRowSession_TheInsertPatternSeesEverySpelling(t *testing.T) {
	for _, s := range []string{
		"INSERT INTO admin_sessions (tenant_id) VALUES ($1)",
		"insert into public.admin_sessions SELECT 1",
		"INSERT\n  INTO \"admin_sessions\" (id) VALUES ($1)",
		`INSERT INTO "public"."admin_sessions" DEFAULT VALUES`,
		"-- name: X\nINSERT INTO admin_sessions AS s (id) VALUES ($1)",
	} {
		if !sessionInsert.MatchString(s) {
			t.Errorf("the pattern misses %q", s)
		}
	}
	for _, s := range []string{"INSERT INTO admin_sessions_archive (id) VALUES ($1)", "UPDATE admin_sessions SET revoked_at = now()", "INSERT INTO sessions (id) VALUES ($1)"} {
		if sessionInsert.MatchString(s) {
			t.Errorf("the pattern flags %q", s)
		}
	}
}
