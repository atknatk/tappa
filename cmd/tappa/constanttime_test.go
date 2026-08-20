package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// constantTimeInventory is every PRODUCTION file that compares a secret in
// constant time, and how many such comparisons it performs.
//
// 🔴 WHY A NUMBER PER FILE AND NOT JUST "IT IMPORTS crypto/subtle" (backlog T53).
// M2-08 measured the failure this exists for: replacing
// subtle.ConstantTimeCompare with bytes.Equal in internal/sun/verify_mac.go left
// the ENTIRE SUITE GREEN, and scripts/redline-check.sh reports that class as a WARN
// — which `make audit` does not fail on. That one file was then pinned by
// internal/sun/verify_mac_test.go reading its own source. The rest had nothing
// holding them, and they are the ones an ATTACKER FEEDS: CSRF tokens,
// signed-cookie verifications, a password-reset token, an employee action
// confirmation, a tenant match.
//
// ⚠️ THE BACKLOG SAID "eighteen production sites in ten files" AND THAT NUMBER WAS
// A GREP'S, NOT A COMPILER'S. Re-measured here through go/ast: 14 CALLS in 9 files.
// The four missing ones are COMMENTS that name the function (one in
// internal/config/config.go, two in internal/sun/verify_mac.go, one in
// internal/session/manager.go), and the tenth "file" is internal/session/manager.go,
// which does not import crypto/subtle at all — its comment says, deliberately, that
// this package does NO Go-side comparison of a hash or token because the database
// does the matching. It is therefore absent from the inventory below rather than
// listed with a zero: a zero would assert "nothing here", which is exactly the
// state a deletion produces.
//
// The two in internal/config are a lighter class and are deliberately still here:
// they compare TWO SECRETS with each other (session key against invite key, KEK
// against previous KEK), so there is no attacker input to drive a timing channel —
// but a rule that exempts "the ones we think are safe" is a rule that has to be
// re-argued every time somebody edits it.
//
// 🔴 IT RATCHETS BOTH WAYS, for the reason cmd/tappa's dangling-citation budget
// gives: with "at least", the number is a high-water mark and a deleted comparison
// buys itself an exemption. Adding a comparison also fails until this map is
// edited, which is the visible-in-review step that is the whole point.
//
// ⚠️ COUNTED LIMIT, THE SAME ONE verify_mac_test.go WRITES DOWN: a source-reading
// test reads TEXT, not semantics. Moving a comparison into a helper in another file
// moves it out of this map's sight — the count in the file that lost it goes down
// and this test goes red, but the count in the file that gained it would have to be
// added by hand, and nothing here proves the helper is still constant time. What it
// does hold is the mutation that actually happens: somebody "simplifies" a
// comparison in place.
var constantTimeInventory = map[string]int{
	// Two SECRET-TO-SECRET comparisons: the session key against the invite key,
	// and the KEK against the previous KEK. No attacker input reaches either.
	"internal/config/config.go": 2,
	// The password-reset token: presented by whoever holds the link.
	"internal/handler/adminreset.go": 1,
	// The panel CSRF token.
	"internal/handler/cookies.go": 1,
	// The deactivation confirmation: a CSRF token and a signed cookie.
	"internal/handler/deactivateconfirm.go": 2,
	// The employee-action confirmation token.
	"internal/handler/employeeactions.go": 1,
	// The login state: CSRF, the signed cookie, and the tenant match.
	"internal/handler/logincontext.go": 3,
	// The signup state: CSRF and the signed cookie.
	"internal/handler/signupstate.go": 2,
	// The tap context's signed cookie.
	"internal/handler/tapcontext.go": 1,
	// The chip's SDM MAC (CLAUDE.md §4.7). Also pinned, more strictly, by
	// internal/sun/verify_mac_test.go, which asserts the SPELLING inside verifyMAC.
	"internal/sun/verify_mac.go": 1,
}

// TestConstantTimeComparisonsAreWhereTheyWere generalises internal/sun's
// source-reading invariant to every production file that compares a secret.
func TestConstantTimeComparisonsAreWhereTheyWere(t *testing.T) {
	t.Parallel()

	got := map[string]int{}
	var unguarded []string

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			rel, rerr := filepath.Rel(repoRoot, path)
			if rerr == nil && skipScanDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		// Generated code and tests are out of scope for the same reason
		// TestObservability_EveryLoggerCallSiteIsSpelledLog leaves them out: this is
		// a rule about the PRODUCTION comparison, and a test may legitimately write
		// bytes.Equal to demonstrate the difference.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.HasSuffix(path, "_templ.go") || strings.Contains(path, "/internal/store/") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // the compiler owns syntax
		}
		rel, _ := filepath.Rel(repoRoot, path)
		rel = filepath.ToSlash(rel)

		// A comparison is GUARDED when its result is compared with the literal 1.
		// subtle.ConstantTimeCompare returns an int, so `if subtle.…(a, b) {` does
		// not compile — but `_ = subtle.…(a, b)`, or comparing against a variable
		// that happens to hold 0, does, and both would be a comparison that decides
		// nothing while looking like a guard.
		guarded := map[ast.Node]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			bin, ok := n.(*ast.BinaryExpr)
			if !ok || (bin.Op != token.EQL && bin.Op != token.NEQ) {
				return true
			}
			for _, pair := range [][2]ast.Expr{{bin.X, bin.Y}, {bin.Y, bin.X}} {
				if isConstantTimeCall(pair[0]) && isLiteralOne(pair[1]) {
					guarded[pair[0]] = true
				}
			}
			return true
		})

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isConstantTimeCall(call) {
				return true
			}
			got[rel]++
			if !guarded[call] {
				unguarded = append(unguarded,
					fmt.Sprintf("%s:%d", rel, fset.Position(call.Pos()).Line))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}

	// CONTROL: the walk really reached production code. Without this the whole test
	// could pass on an empty result set if the traversal ever regressed.
	if len(got) < 5 {
		t.Fatalf("only %d files hold a constant-time comparison; the walk has gone blind", len(got))
	}

	for _, u := range unguarded {
		t.Errorf("%s: the result of subtle.ConstantTimeCompare is not compared with 1, so the "+
			"call decides nothing while looking like a guard", u)
	}

	for _, rel := range unionKeys(got, constantTimeInventory) {
		want, inInventory := constantTimeInventory[rel]
		have := got[rel]
		switch {
		case !inInventory:
			t.Errorf("%s performs %d constant-time comparison(s) and is not in "+
				"constantTimeInventory. Add it in this change, with a line saying WHAT it "+
				"compares — the inventory is what makes a deletion visible", rel, have)
		case have == 0:
			t.Errorf("%s is in constantTimeInventory expecting %d comparison(s) and now has "+
				"NONE. If a secret comparison was replaced by bytes.Equal or ==, that is the "+
				"timing leak this test exists for; if it moved, lower the number HERE in the "+
				"same change", rel, want)
		case have != want:
			t.Errorf("%s performs %d constant-time comparison(s), inventory says %d. Growing "+
				"is not free either: say what the new one compares", rel, have, want)
		}
	}
	for rel, want := range constantTimeInventory {
		if _, ok := got[rel]; !ok {
			t.Errorf("constantTimeInventory expects %d constant-time comparison(s) in %s, "+
				"which now has none at all (renamed, deleted, or the comparison went away)",
				want, rel)
		}
	}
	t.Logf("%d production files, %d constant-time comparisons, %d unguarded",
		len(got), totalCount(got), len(unguarded))
}

func isConstantTimeCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "ConstantTimeCompare" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "subtle"
}

func isLiteralOne(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "1"
}

func unionKeys(a, b map[string]int) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]int{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func totalCount(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}
