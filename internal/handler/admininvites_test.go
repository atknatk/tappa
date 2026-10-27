package handler

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
)

// TestAdminInvite_ThePathIsTheLinksPath: the route the invitee's page is mounted on is the path the
// link adminauth mints opens (IssuedInvite.Link), and the templates' two forms post there.
func TestAdminInvite_ThePathIsTheLinksPath(t *testing.T) {
	if adminInvitePath != adminauth.InvitePath {
		t.Fatalf("the route is %q, the link opens %q", adminInvitePath, adminauth.InvitePath)
	}
}

// managersInviteCalls is R18's closed list for the owner's send (managersInvite): every call it makes.
// None reads an identity or a membership -- the business's own settings (its name for the e-mail), the
// policy gate (the session's own actor), the mailbox cap and breaker of THIS business, the issue, the
// render, the send, the trail and the redirect. adminauth's TestAdminInvites_IssuingTouchesNoIdentity
// holds Issue to the same rule.
var managersInviteCalls = []string{
	"AdminOf", "Cancel", "Capped", "Context", "Errorf", "Error", "Info", "Is", "Issue", "Link", "PostFormValue",
	"Record", "RenderAdminInvitation", "Refusing", "Round", "Send", "Settings", "String", "Sub", "TenantID",
	"TrimSpace", "Warn", "WithTimeout", "WithoutCancel", "append", "cancel", "mayInvite", "readManagersForm",
	"recordInvite", "redirect", "refuseManagers", "renderProblem", "sendErrorClass", "Sprintf",
}

// TestAdminInvite_TheSendCallsNothingThatReadsAnIdentity is R18's exact half at the handler (the
// clock is TestAdminInvitesDB_TheAnswerTakesTheSameTimeForEveryAddress): managersInvite's every call is
// on managersInviteCalls. A resolver, an identity function or a membership read added to the send is
// red here, whatever its timing. MUTATION (measured): a call to a.admins' identity resolution added ->
// red.
func TestAdminInvite_TheSendCallsNothingThatReadsAnIdentity(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "admininvites.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "managersInvite" {
			return true
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				switch x := c.Fun.(type) {
				case *ast.SelectorExpr:
					calls = append(calls, x.Sel.Name)
				case *ast.Ident:
					calls = append(calls, x.Name)
				}
			}
			return true
		})
		return false
	})
	sort.Strings(calls)
	calls = slices.Compact(calls)
	if len(calls) < 15 {
		t.Fatalf("anti-vacuity: %v", calls)
	}
	for _, c := range calls {
		if !slices.Contains(managersInviteCalls, c) {
			t.Errorf("managersInvite calls %s, which is not on R18's closed list -- the owner's send must read no identity and no membership", c)
		}
	}
}

// floodCountingInvites is a panelAdminInvites that counts the lookups the invitee's page makes.
type floodCountingInvites struct {
	panelAdminInvites
	usable int
}

func (c *floodCountingInvites) Usable(context.Context, adminauth.InviteToken) error {
	c.usable++
	return nil // usable: the page keeps the cookie, so every request reaches the lookup
}

// TestAdminInvite_TheInviteePageIsBehindTheFloodCeiling (the 2026-10-27 re-audit): the invitee's page
// is outside the panel's chains, and a bare GET carrying a cookie the caller writes costs one resolver
// read. From one address, the first adminFloodLimit requests are answered (each one lookup) and the
// next is 429 with NO lookup -- for the GET and for the POST alike. MUTATION (measured): the GET's
// flooded call removed -> red (request limit+1 answers and reads).
func TestAdminInvite_TheInviteePageIsBehindTheFloodCeiling(t *testing.T) {
	cfg := adminTestConfig()
	cfg.AdminIdentity = true
	fake := &floodCountingInvites{}
	h, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, slog.New(slog.DiscardHandler), AdminInvitations(fake))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.MountInviteAcceptance(r)
	b := newBrowser(t, r)
	b.ip = "198.51.100.77:1234"
	b.cookies[adminInviteCookieName] = "csrf.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	for i := 1; i <= adminFloodLimit; i++ {
		if code := b.do(http.MethodGet, adminInvitePath, nil).Code; code == http.StatusTooManyRequests {
			t.Fatalf("request %d was refused before the ceiling of %d", i, adminFloodLimit)
		}
	}
	if fake.usable != adminFloodLimit {
		t.Fatalf("PREMISE: %d lookups for %d answered requests", fake.usable, adminFloodLimit)
	}
	if code := b.do(http.MethodGet, adminInvitePath, nil).Code; code != http.StatusTooManyRequests {
		t.Fatalf("GET %d answered %d, want 429", adminFloodLimit+1, code)
	}
	if code := b.do(http.MethodPost, adminInvitePath, url.Values{"csrf": {"csrf"}, "mode": {"new"}}).Code; code != http.StatusTooManyRequests {
		t.Fatalf("POST past the ceiling answered %d, want 429", code)
	}
	if fake.usable != adminFloodLimit {
		t.Errorf("past the ceiling the page still looked the link up (%d lookups)", fake.usable)
	}
}
