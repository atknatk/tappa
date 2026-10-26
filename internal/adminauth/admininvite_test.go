package adminauth

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestAdminInvites_IssuingTouchesNoIdentity is R18's exact half (the clock is internal/handler's
// TestAdminInvitesDB_TheAnswerTakesTheSameTimeForEveryAddress): Issue -- the owner's send -- calls
// NOTHING that reads an identity or a membership. Every call in its body is on a closed list: the
// token, the one transaction, the two statements of admininvites.sql it runs, the address
// and name checks, and the error and time helpers. A resolver, an identity function or a read of
// admin_users added to it is red here, whatever its timing. MUTATION (measured): a
// s.data.GetAdminInviteByTokenHash or q.GetAdminByEmail call added to Issue -> red.
func TestAdminInvites_IssuingTouchesNoIdentity(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "admininvite.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	allowed := []string{
		"CancelOpenAdminInvitesForAddress", "InsertAdminInvite", "Add", "Errorf", "Is", "New",
		"TrimSpace", "ValidRecipient", "WithTenant", "hash", "inviteNameOK", "isLookupableEmail", "isUniqueViolation",
		"newInviteToken", "now",
	}
	var calls []string
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Issue" {
			return true
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch x := c.Fun.(type) {
			case *ast.SelectorExpr:
				calls = append(calls, x.Sel.Name)
			case *ast.Ident:
				calls = append(calls, x.Name)
			}
			return true
		})
		return false
	})
	if !found {
		t.Fatal("no Issue in admininvite.go")
	}
	sort.Strings(calls)
	calls = slices.Compact(calls)
	if len(calls) < 8 {
		t.Fatalf("anti-vacuity: %v", calls)
	}
	for _, c := range calls {
		if !slices.Contains(allowed, c) {
			t.Errorf("Issue calls %s, which is not on R18's closed list -- the owner's send must read no identity and no membership", c)
		}
	}
	for _, c := range calls {
		if strings.Contains(strings.ToLower(c), "identity") || strings.Contains(c, "Admin") && c != "InsertAdminInvite" && c != "CancelOpenAdminInvitesForAddress" {
			t.Errorf("Issue calls %s", c)
		}
	}
}

// TestInviteToken_HashesUnderItsOwnKey: the invitation's derived key differs from the panel cookie's and
// the reset link's, so one string can never be two credentials; a junk value never reaches the database.
func TestInviteToken_HashesUnderItsOwnKey(t *testing.T) {
	ik, _ := deriveInviteKey(testKey())
	sk, _ := deriveKey(testKey())
	rk, _ := deriveResetKey(testKey())
	tok, err := newInviteToken()
	if err != nil {
		t.Fatal(err)
	}
	h1, err := tok.hash(ik)
	if err != nil || len(h1) != 64 {
		t.Fatalf("hash %q, %v", h1, err)
	}
	h2, _ := Token(tok).hash(sk)
	h3, _ := ResetToken(tok).hash(rk)
	if h1 == h2 || h1 == h3 {
		t.Error("the invitation hashes like another credential")
	}
	if _, err := ParseInviteToken("junk").hash(ik); err == nil {
		t.Error("a junk value was hashed")
	}
	if s := strings.Join([]string{tok.String(), tok.GoString()}, ""); strings.Contains(s, tok.reveal()) {
		t.Error("the token prints")
	}
}
