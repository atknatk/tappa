package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The PUBLIC side of Tappa's own legal texts, and the customer panel's side of their
// publishing -- which, since M10 OP-10, has no route in the panel (/admin/legal answers
// the router's 404: TestCustomerPanel_EverySectionCarriesNoOperatorElement). The
// obligations:
//
//	the public surface stays off the DB  TestLegalReader_CannotReachTheDatabase
//	the public path writes nothing       TestLegalPublicPath_WritesNothing
//	slugs and pages are one set          TestLegalSlugs_AreExactlyTheDocumentsWithAPage
//	§4.5 — structurally, over the types  TestLegalStore_CannotNameATenantAtAll
//	no operator element on the panel     TestCustomerPanel_EverySectionCarriesNoOperatorElement
//
// The first four lived in legaladmin_test.go beside the M7-06 panel screen that
// published for an env allow-list of customer admins; that screen, its route
// (/admin/legal), its tab and its allow-list were removed in OP-10's phase B, and the
// publishing moved to the platform operator's own surface (internal/handler/operator,
// /operator/legal; ADR 0020 §7). They keep their names.

// TestLegalReader_CannotReachTheDatabase is what keeps handler.Marketing's rate-limit
// argument standing after M7-06 gave it a field.
//
// 🔴 THE SIGNATURE IS THE GUARANTEE. /legal/* is unmetered, unauthenticated and the
// most crawled part of this deployment; a per-request SELECT there would put it on
// the pool that check-in shares. The reader's one method takes no context.Context and
// returns no error, which is not a style choice — a method that cannot be cancelled
// and cannot report a failure is a method that is not doing I/O, and somebody
// implementing this interface with a query would have to change the signature to do
// it properly, which is what this reads.
//
// POSITIVE CONTROL, with a subject independent of legalReader (OP-10: the old control
// was the M7-06 panel's writer interface, removed with the panel screen): the real
// store's OTHER method, internal/domain/legal.Store.Refresh -- the read of the database
// that fills the snapshot -- must fail both checks, or the checks distinguish nothing.
func TestLegalReader_CannotReachTheDatabase(t *testing.T) {
	t.Parallel()
	rt := reflect.TypeOf((*legalReader)(nil)).Elem()
	if rt.NumMethod() != 1 {
		t.Fatalf("handler.legalReader has %d methods, want exactly 1. Every method on this "+
			"interface is something the PUBLIC surface can call.", rt.NumMethod())
	}
	m := rt.Method(0)
	if got := m.Type.NumIn(); got != 0 {
		t.Errorf("legalReader.%s takes %d argument(s). It must take none — in particular no "+
			"context.Context, because a method that can be cancelled is a method that is "+
			"doing I/O, and this one is called from the surface that argues it touches no "+
			"pool.", m.Name, got)
	}
	if got := m.Type.NumOut(); got != 1 {
		t.Errorf("legalReader.%s returns %d values. It must return exactly one — an error "+
			"return is what a query needs and what this must not have.", m.Name, got)
	}
	for i := 0; i < m.Type.NumOut(); i++ {
		if m.Type.Out(i).String() == "error" {
			t.Errorf("legalReader.%s returns an error, so it can fail, so it can do I/O", m.Name)
		}
	}
	// POSITIVE CONTROL: the store's database read. Its method type through the concrete
	// type carries the receiver as In(0), so "takes arguments" means more than one.
	refresh, ok := reflect.TypeOf((*legal.Store)(nil)).MethodByName("Refresh")
	if !ok {
		t.Fatal("legal.Store has no Refresh; the control is looking at the wrong type")
	}
	if refresh.Type.NumIn() <= 1 {
		t.Error("the control method takes no arguments, so 'takes no arguments' does not " +
			"distinguish an I/O method from a memory read here")
	}
	hasErr := false
	for i := 0; i < refresh.Type.NumOut(); i++ {
		if refresh.Type.Out(i).String() == "error" {
			hasErr = true
		}
	}
	if !hasErr {
		t.Error("the control method returns no error, so 'returns no error' does not " +
			"distinguish an I/O method here")
	}
}

// TestLegalPublicPath_WritesNothing — the public documents are a READ.
//
// 🔴 TWO HALVES, NEITHER SUFFICIENT ALONE. The STATIC half parses marketing.go and
// requires that it calls none of the snapshot's database-touching or writing methods by
// name -- Refresh (the store's read of the pool), Publish (the M7-06 writer, removed in
// OP-10) and PublishLegal (the operator's) -- nor the paragraph split; the LIVE half
// drives every public URL and counts what the snapshot was asked to do: reads, and no
// refresh.
//
// ⚠️ THE STATIC HALF'S LIMIT, COUNTED RATHER THAN CLOSED: it matches the SYNTAX of a
// selector call inside marketing.go, so a call reached through an intermediate value is
// invisible to it. The live half is what stands meanwhile, and it does not care how a
// call was spelled.
func TestLegalPublicPath_WritesNothing(t *testing.T) {
	t.Parallel()

	// STATIC: marketing.go must not name the store's I/O or a writer.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "marketing.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse marketing.go: %v", err)
	}
	seenReader := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Publish", "PublishLegal", "Refresh":
			t.Errorf("marketing.go calls .%s at %s. The public legal pages are a READ of the "+
				"snapshot; a write or a database read reachable from an unauthenticated GET is "+
				"one a crawler, a prefetch or a retried request fires.", sel.Sel.Name, fset.Position(call.Pos()))
		case "Paragraphs":
			// 🔴 AND IT MUST NOT SPLIT THE TEXT EITHER, WHICH IS A MUTATION THAT SURVIVED
			// THE FIRST VERSION OF THIS SUITE. Restoring `legal.Paragraphs(d.Body)` here
			// changes no output at all — the two produce identical bytes — so no
			// behavioural test can see it. What it changes is WHERE the work happens:
			// this surface is deliberately unmetered, so a split per request is CPU an
			// anonymous caller can ask for as often as they like (a security audit
			// measured 253 ms for a pathological body). The paragraphs are computed once,
			// when the snapshot is installed; internal/domain/legal's DB tests assert that
			// they arrive precomputed, and this asserts that the reader does not redo it.
			t.Errorf("marketing.go calls legal.Paragraphs at %s. The split belongs to the "+
				"snapshot (internal/domain/legal.Store.set), because this path is "+
				"unmetered — see the argument on handler.Marketing.", fset.Position(call.Pos()))
		case "Published":
			seenReader = true
		}
		return true
	})
	// ANTI-VACUITY: if the scan cannot even find the READ it is supposed to see past,
	// its silence about the rest means nothing.
	if !seenReader {
		t.Fatal("the scan found no call to .Published in marketing.go, so it is not walking " +
			"the code it thinks it is and its silence proves nothing")
	}

	// LIVE: drive every public URL and count.
	texts := newFakeTexts()
	texts.put("privacy", "A published text.")
	r := marketingRouterWithTexts(t, texts)
	for _, u := range marketingURLs() {
		if got := mustFetchMarketing(t, r, u); got == "" {
			t.Fatalf("GET %s rendered nothing", u)
		}
	}
	reads, refreshes := texts.counts()
	if refreshes != 0 {
		t.Errorf("loading the public pages reached the snapshot's database read %d time(s)", refreshes)
	}
	// CONTROL: the pages did read the snapshot, or "no refresh" is a page that read nothing.
	if reads == 0 {
		t.Fatal("CONTROL: loading the public pages read the snapshot 0 times")
	}
}

// TestLegalSlugs_AreExactlyTheDocumentsWithAPage holds the three closed sets
// together: 00020's CHECK, legal.Slugs, and pages.LegalPages.
//
// A slug with no page is a document nobody can read; a page with no slug is a
// document nobody can publish. Both are silent failures — the first renders a text
// at no URL, the second renders a placeholder forever.
func TestLegalSlugs_AreExactlyTheDocumentsWithAPage(t *testing.T) {
	t.Parallel()
	fromPages := map[string]bool{}
	for _, p := range pages.LegalPages {
		s := legalSlugOf(p.Path)
		if s == p.Path {
			t.Errorf("%q does not live under /legal/, so no slug can be derived from it", p.Path)
		}
		fromPages[s] = true
	}
	fromDomain := map[string]bool{}
	for _, s := range legal.Slugs {
		fromDomain[s] = true
	}
	for s := range fromPages {
		if !fromDomain[s] {
			t.Errorf("/legal/%s has a page and is not in legal.Slugs, so its text can never be "+
				"published and the page shows its placeholder forever", s)
		}
	}
	for s := range fromDomain {
		if !fromPages[s] {
			t.Errorf("%q is publishable and has no page, so its text would be stored at no URL", s)
		}
	}
	if len(fromPages) == 0 {
		t.Fatal("no slugs were derived at all; this test would pass over anything")
	}
	// AND THE MIGRATION'S CHECK IS THE THIRD SET. It is read out of the file rather
	// than retyped, so a fifth value added there without a page fails here.
	sql := mustReadRepoFile(t, "../../db/migrations/00020_create_legal_documents.sql")
	for s := range fromDomain {
		if !strings.Contains(sql, "'"+s+"'") {
			t.Errorf("00020's CHECK does not list %q, so the column would refuse it", s)
		}
	}
}

// TestLegalStore_CannotNameATenantAtAll is the STRUCTURAL half of §4.5, and it is
// asserted over the GENERATED types rather than over the SQL.
//
// 🔴 MUTATING A GENERATED FILE DOES NOT TEST THE NETWORK THAT GENERATES IT, so this
// reads what sqlc produced and the fix for a failure is in db/queries/legal.sql. What
// it proves is narrow and worth exactly what it says: THERE IS NO PLACE TO PUT A
// TENANT ID ON THIS PATH. (OP-10 removed the M7-06 INSERT and its params type with the
// panel screen; the read and the row types remain.)
func TestLegalStore_CannotNameATenantAtAll(t *testing.T) {
	t.Parallel()
	for _, rt := range []reflect.Type{reflect.TypeOf(store.LegalDocument{}), reflect.TypeOf(store.ListPublishedLegalDocumentsRow{})} {
		if rt.NumField() == 0 {
			t.Fatalf("%s has no fields; this test would pass over anything", rt)
		}
		for i := 0; i < rt.NumField(); i++ {
			if n := strings.ToLower(rt.Field(i).Name); strings.Contains(n, "tenant") {
				t.Errorf("store.%s.%s exists, so the table grew a tenant column", rt.Name(), rt.Field(i).Name)
			}
		}
	}
	// The READ takes a context and nothing else — no filter to pass, right or wrong.
	m, ok := reflect.TypeOf(&store.Queries{}).MethodByName("ListPublishedLegalDocuments")
	if !ok {
		t.Fatal("store.Queries has no ListPublishedLegalDocuments")
	}
	// receiver + ctx
	if got := m.Type.NumIn(); got != 2 {
		t.Errorf("ListPublishedLegalDocuments takes %d arguments beside its receiver, want "+
			"only a context. Anything else is something a caller can scope wrongly.", got-1)
	}
	// POSITIVE CONTROL, with an INDEPENDENT subject: a query that IS tenant-scoped
	// must fail every check above. Without this, the assertions would pass just as
	// happily if reflect were looking at the wrong thing.
	scoped := reflect.TypeOf(store.RecordAuditEventParams{})
	found := false
	for i := 0; i < scoped.NumField(); i++ {
		if strings.Contains(strings.ToLower(scoped.Field(i).Name), "tenant") {
			found = true
		}
	}
	if !found {
		t.Error("the positive control found no tenant field on store.RecordAuditEventParams, " +
			"which certainly has one. The scan above is not looking at what it thinks it is.")
	}
}

// panelOperatorMarkers are the platform operator's markers a customer page must not
// carry (M10 OP-10: the M7-06 legal tab, drawn for an env allow-list of customer admins,
// was the panel's one operator element and is gone): a path under the operator's
// surface, the operator chrome's band and lockup, the retired route and its tab's two
// spellings. Compared case-insensitively.
var panelOperatorMarkers = []string{
	"/operator", `class="op-bar"`, "TAPTIME <span", "/admin/legal", "Taptime legal texts", "Tappa legal texts",
}

// panelBrowserAs is the panel with a signed-in admin of role and the fake services.
func panelBrowserAs(t *testing.T, role string, accounts *fakeAccounts) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "KF Owner",
		}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), accounts, newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// operatorMarkersIn is the markers of panelOperatorMarkers that html carries.
func operatorMarkersIn(html string) []string {
	var out []string
	for _, m := range panelOperatorMarkers {
		if strings.Contains(strings.ToLower(html), strings.ToLower(m)) {
			out = append(out, m)
		}
	}
	return out
}

// TestCustomerPanel_EverySectionCarriesNoOperatorElement (M10 OP-10's acceptance "müşteri
// panel taramasında operatör öğesi yok"): every section of pages.PanelSections, as an
// owner and as a manager, renders with none of panelOperatorMarkers -- the navigation
// included, since it is part of every section's bytes; and the retired route
// /admin/legal is the router's 404 on GET and POST for an owner (it was the M7-06
// screen's). The navigation's links are not COUNTED: a link to a section that is not an
// operator's (none of the markers) is not this test's business.
//
// POSITIVE CONTROL through the same path: a business whose name carries one of the
// markers renders it on the Account section, and the scan finds it there.
//
// PART III -- the scan reads the sections' rendered bytes under these fakes for the six
// markers listed; a page outside PanelSections, a state these fakes do not produce, or a
// marker not in the list is code review's -- no completeness claim.
func TestCustomerPanel_EverySectionCarriesNoOperatorElement(t *testing.T) {
	for _, role := range []string{"owner", "manager"} {
		b := panelBrowserAs(t, role, newFakeAccount())
		swept := 0
		for _, s := range pages.PanelSections {
			rec := b.do(http.MethodGet, s.Href, nil)
			if role == "owner" && rec.Code != http.StatusOK {
				t.Errorf("%s: %s = %d, want 200", role, s.Href, rec.Code)
				continue
			}
			swept++
			if got := operatorMarkersIn(rec.Body.String()); len(got) != 0 {
				t.Errorf("%s: the %q section (%d) carries the operator marker(s) %q", role, s.Label, rec.Code, got)
			}
		}
		if swept != len(pages.PanelSections) {
			t.Errorf("%s: %d of %d sections swept", role, swept, len(pages.PanelSections))
		}
	}
	owner := panelBrowserAs(t, "owner", newFakeAccount())
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		var form url.Values
		if m == http.MethodPost {
			form = url.Values{"slug": {"privacy"}, "body": {"FAKE text"}}
		}
		if rec := owner.do(m, "/admin/legal", form); rec.Code != http.StatusNotFound {
			t.Errorf("%s /admin/legal (the retired M7-06 screen) = %d, want the router's 404", m, rec.Code)
		}
	}
	// CONTROL.
	acc := newFakeAccount()
	acc.settings.Name = "Rusty /operator/legal Bar"
	html := panelBrowserAs(t, "owner", acc).do(http.MethodGet, accountHref, nil).Body.String()
	if got := operatorMarkersIn(html); len(got) == 0 {
		t.Fatal("CONTROL: a business name carrying /operator was not found on the Account section; the scan is blind")
	}
}

// mustReadRepoFile reads a file relative to this package or fails.
func mustReadRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
