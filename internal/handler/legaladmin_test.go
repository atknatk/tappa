package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/web/templates/pages"

	"log/slog"
)

// The OPERATOR's legal-text screen (M7-06). The obligations this file measures:
//
//	the allow-list is fail-closed        TestOperatorGate_EmptyAllowListAdmitsNobody
//	hiding is not the gate               TestOperatorGate_TheServerRefusesTheRouteItself
//	a refused write leaves a trail       TestLegalPublish_ARefusedAttemptIsRecorded
//	§4.5 — the caller's own tenant only  TestLegalPublish_NamesTheCallersOwnTenantAndNobodyElses
//	§4.5 — structurally, over the types  TestLegalStore_CannotNameATenantAtAll
//	the public surface stays off the DB  TestLegalReader_CannotReachTheDatabase
//	the public path writes nothing       TestLegalPublicPath_WritesNothing
//	slugs and pages are one set          TestLegalSlugs_AreExactlyTheDocumentsWithAPage

// legalBrowser wires the panel with an operator allow-list and an identity the test
// controls. The TRAIL is under the test's control so a refusal can be COUNTED.
func legalBrowser(t *testing.T, who uuid.UUID, allow []uuid.UUID, texts panelTexts, trail *fakeTrail) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: who,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	cfg := &config.Config{
		Env:              config.EnvDev,
		BaseURL:          testBaseURL,
		SessionHMACKey:   []byte("0123456789abcdef0123456789abcdef"),
		OperatorAdminIDs: allow,
	}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), texts, newFakeAccount(), cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// legalTestOperator is the admin the tests in this file put on the allow-list when
// the identity itself is not the subject.
var legalTestOperator = uuid.MustParse("7a1e0c55-0000-4000-8000-0000000000aa")

// publishForm is the body the operator screen posts.
func publishForm(slug, body string) url.Values {
	return url.Values{"slug": {slug}, "body": {body}}
}

// TestOperatorGate_EmptyAllowListAdmitsNobody is the FAIL-CLOSED proof.
//
// 🔴 THE FAILURE THIS EXISTS FOR IS ONE LINE LONG AND IT WRITES ITSELF: `if
// len(allow) == 0 { return true }` — "nobody configured, so let everybody in". A
// deployment that forgot the env var would hand every admin of every customer the
// power to rewrite Tappa's privacy policy, and it would look like it was working.
//
// 🔴 THE POSITIVE CONTROL'S INPUT IS INDEPENDENT OF THE THING IT CONTROLS, which is
// the shape this repository has been burned by. It does NOT construct the admitted
// id out of the allow-list, nor the allow-list out of the id: both are written out
// as literals below, by hand, and they happen to name the same person. A control
// built from the subject's own data asks "is x == x" and passes over any hole.
func TestOperatorGate_EmptyAllowListAdmitsNobody(t *testing.T) {
	t.Parallel()
	// Two INDEPENDENT literals. Nothing derives one from the other.
	theOperator := uuid.MustParse("7a1e0c55-0000-4000-8000-00000000ab01")
	allowList := []uuid.UUID{uuid.MustParse("7a1e0c55-0000-4000-8000-00000000ab01")}

	// POSITIVE CONTROL: with a list naming this admin, the screen opens. If this arm
	// ever fails, the refusals below prove nothing — they would be refusing something
	// that never worked.
	admitted := legalBrowser(t, theOperator, allowList, newFakeTexts(), &fakeTrail{})
	if rec := admitted.do(http.MethodGet, legalHref, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET %s with the id on the allow-list: %d, want 200. Every refusal "+
			"below is vacuous until this passes.", legalHref, rec.Code)
	}

	// THE PROPERTY: the SAME admin, the SAME everything, an EMPTY list.
	for _, empty := range [][]uuid.UUID{nil, {}} {
		b := legalBrowser(t, theOperator, empty, newFakeTexts(), &fakeTrail{})
		if rec := b.do(http.MethodGet, legalHref, nil); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s with an empty allow-list: %d, want 403. An unconfigured "+
				"allow-list must admit NOBODY; reading it as 'not configured, therefore "+
				"open' hands Tappa's own legal texts to every admin of every customer.",
				legalHref, rec.Code)
		}
		texts := newFakeTexts()
		w := legalBrowser(t, theOperator, empty, texts, &fakeTrail{})
		rec := w.do(http.MethodPost, legalHref, publishForm("privacy", "We would collect nothing."))
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s with an empty allow-list: %d, want 403", legalHref, rec.Code)
		}
		if n := len(texts.published()); n != 0 {
			t.Errorf("a refused POST still reached the writer %d time(s); the gate must run "+
				"BEFORE anything is written", n)
		}
	}

	// AND AN ADMIN WHO IS NOT ON A NON-EMPTY LIST is refused too — otherwise the arm
	// above could be passing because the list is ignored entirely.
	other := legalBrowser(t, uuid.MustParse("7a1e0c55-0000-4000-8000-00000000ffff"),
		allowList, newFakeTexts(), &fakeTrail{})
	if rec := other.do(http.MethodGet, legalHref, nil); rec.Code != http.StatusForbidden {
		t.Errorf("GET %s by an admin who is not on a populated allow-list: %d, want 403",
			legalHref, rec.Code)
	}

	// AND THE NIL uuid NEVER MATCHES, from either side. A session that did not resolve
	// carries uuid.Nil, and a list that ever held it would admit that non-identity.
	none := legalBrowser(t, uuid.Nil, []uuid.UUID{uuid.Nil}, newFakeTexts(), &fakeTrail{})
	if rec := none.do(http.MethodGet, legalHref, nil); rec.Code != http.StatusForbidden {
		t.Errorf("GET %s by the nil admin against a list containing the nil uuid: %d, want "+
			"403. Both halves have to hold: config refuses a nil entry and the gate "+
			"refuses a nil identity.", legalHref, rec.Code)
	}
}

// TestOperatorGate_IsNotJoinableByRegisteringABusiness is the regression test for the
// break a security audit found, and it is written from the ATTACKER's side.
//
// 🔴 THE FIRST VERSION OF THIS GATE KEYED ON admin_users.email AND WAS BREAKABLE END
// TO END. That column is unique only WITHIN a tenant
// (admin_users_tenant_email_key is UNIQUE (tenant_id, email)), /signup is public, and
// nothing verifies an address — so anybody who knew an allow-listed address could
// register their own business with that same address, sign in as its owner, and
// publish. Measured by the audit: 200 on GET, 303 on POST, and the published privacy
// policy replaced.
//
// WHAT THIS ASSERTS is the property that closed it: the allow-list is keyed on
// something the caller CANNOT DECLARE. Two different admins in two different tenants,
// same everything else — only the one whose DATABASE-ASSIGNED id is on the list gets
// in.
func TestOperatorGate_IsNotJoinableByRegisteringABusiness(t *testing.T) {
	t.Parallel()
	realOperator := uuid.MustParse("7a1e0c55-0000-4000-8000-000000000001")
	impostor := uuid.MustParse("7a1e0c55-0000-4000-8000-000000000002")
	allow := []uuid.UUID{realOperator}

	if rec := legalBrowser(t, realOperator, allow, newFakeTexts(), &fakeTrail{}).
		do(http.MethodGet, legalHref, nil); rec.Code != http.StatusOK {
		t.Fatalf("the real operator was refused (%d); the refusal below would prove nothing", rec.Code)
	}

	texts := newFakeTexts()
	b := legalBrowser(t, impostor, allow, texts, &fakeTrail{})
	if rec := b.do(http.MethodGet, legalHref, nil); rec.Code != http.StatusForbidden {
		t.Errorf("a second admin — everything identical but the id the DATABASE assigned "+
			"them — reached the screen (%d). The key must be one a caller cannot "+
			"declare; an email address in this schema is not.", rec.Code)
	}
	rec := b.do(http.MethodPost, legalHref, publishForm("privacy", "DEFACED"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST by a non-operator: %d, want 403", rec.Code)
	}
	if n := len(texts.published()); n != 0 {
		t.Errorf("a non-operator's POST reached the writer %d time(s)", n)
	}
}

// TestOperatorRefusal_ShowsTheCallerTheirOWNIdAndNobodyElses.
//
// The allow-list takes uuids, so the person who runs this deployment has to get
// theirs into an env file. The alternative is a database session, which is the "go
// and dig it out yourself" this whole task exists to end. What the page must NOT do
// is say anything about the list or about anybody else.
func TestOperatorRefusal_ShowsTheCallerTheirOWNIdAndNobodyElses(t *testing.T) {
	t.Parallel()
	me := uuid.MustParse("7a1e0c55-0000-4000-8000-00000000cafe")
	someoneElse := uuid.MustParse("7a1e0c55-0000-4000-8000-00000000beef")
	text := screenText(t, htmlOf(t, legalBrowser(t, me, []uuid.UUID{someoneElse}, newFakeTexts(), &fakeTrail{}).
		do(http.MethodGet, legalHref, nil)))

	if !strings.Contains(text, me.String()) {
		t.Errorf("the refusal page does not show the caller their own id, so the person who "+
			"runs this deployment has no way to configure it without a database "+
			"session.\n%s", text)
	}
	if strings.Contains(text, someoneElse.String()) {
		t.Error("the refusal page prints an id from the allow-list. Who the operators are is " +
			"a fact about the deployment and about other people; a refused caller is owed " +
			"neither.")
	}
	if strings.Contains(text, "TAPPA_OPERATOR_ADMIN_IDS=") {
		t.Error("the refusal page prints the variable as an assignment, which reads as though " +
			"it were showing its VALUE")
	}
}

// TestOperatorGate_TheServerRefusesTheRouteItself is M6-12's lesson, applied.
//
// 🔴 "HIDING THE LINK IS THE COURTESY HALF; THE SERVER REFUSES THE ROUTE ITSELF, AND
// NEITHER SUBSTITUTES FOR THE OTHER." Both halves are driven here, and the
// distinction between 403 and 404 is the load-bearing one: a 404 would mean the
// filter had been moved into the ROUTER, which breaks the property
// pages.PanelSections exists to guarantee (a tab whose link 404s is impossible to
// introduce) and makes the refusal indistinguishable from a typo.
func TestOperatorGate_TheServerRefusesTheRouteItself(t *testing.T) {
	t.Parallel()
	operator := uuid.MustParse("7a1e0c55-0000-4000-8000-0000000000a1")
	allow := []uuid.UUID{uuid.MustParse("7a1e0c55-0000-4000-8000-0000000000a1")}

	// 1. THE OPERATOR SEES THE LINK.
	opHTML := htmlOf(t, legalBrowser(t, operator, allow, newFakeTexts(), &fakeTrail{}).
		do(http.MethodGet, "/admin", nil))
	if !strings.Contains(opHTML, `href="`+legalHref+`"`) {
		t.Fatal("an operator's navigation carries no link to the legal-text screen")
	}

	// 2. AN ORDINARY ADMIN DOES NOT.
	other := legalBrowser(t, uuid.MustParse("7a1e0c55-0000-4000-8000-0000000000a2"),
		allow, newFakeTexts(), &fakeTrail{})
	mgrHTML := htmlOf(t, other.do(http.MethodGet, "/admin", nil))
	if strings.Contains(mgrHTML, `href="`+legalHref+`"`) {
		t.Error("an ordinary admin's navigation offers a link to a section the server refuses")
	}
	// AND ONLY THAT ONE IS MISSING — a filter that dropped more would be a different
	// bug wearing this one's clothes. (Billing is owner-only and this identity IS an
	// owner, so every other row must be present.)
	for _, s := range pages.PanelSections {
		if s.OperatorOnly {
			continue
		}
		if !strings.Contains(mgrHTML, `href="`+s.Href+`"`) {
			t.Errorf("an ordinary admin's navigation lost the %q tab, which is not operator-only", s.Label)
		}
	}

	// 3. THE ROUTE STILL EXISTS AND THE HANDLER IS WHAT REFUSES.
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		got := other.do(m, legalHref, publishForm("privacy", "text"))
		if got.Code == http.StatusNotFound {
			t.Errorf("%s %s answered 404 for a non-operator. The filter belongs to the "+
				"NAVIGATION; moving it into the mount breaks the one property "+
				"pages.PanelSections exists to guarantee.", m, legalHref)
		}
		if got.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d, want 403 from the handler", m, legalHref, got.Code)
		}
	}

	// 4. AND THE REFUSAL SAYS WHY (§4.6). A page that refuses without a reason sends
	//    somebody to support.
	text := screenText(t, htmlOf(t, other.do(http.MethodGet, legalHref, nil)))
	if !strings.Contains(text, "not part of your dashboard") {
		t.Errorf("the refusal page does not say what happened.\n%s", text)
	}
	// AND IT DOES NOT LEAK THE ALLOW-LIST.
	if strings.Contains(text, operator.String()) {
		t.Error("the refusal page prints an allow-listed id. Who the operators are is not " +
			"something a refused caller is told.")
	}
}

// TestLegalPublish_ARefusedAttemptIsRecorded — §4.3 and §4.6.
//
// 🔴 A REFUSED POST IS SOMEBODY ATTEMPTING TO REWRITE THE PRIVACY POLICY OF EVERY
// BUSINESS ON THIS DEPLOYMENT, and that attempt has to outlive a log rotation. A
// refused GET does not write, which is the read-path rule M6-12 recorded; both arms
// are asserted, because "writes a row" and "writes a row for the right thing" are
// different claims.
func TestLegalPublish_ARefusedAttemptIsRecorded(t *testing.T) {
	t.Parallel()
	trail := &fakeTrail{}
	b := legalBrowser(t, uuid.MustParse("7a1e0c55-0000-4000-8000-0000000000b1"),
		[]uuid.UUID{legalTestOperator}, newFakeTexts(), trail)

	if rec := b.do(http.MethodGet, legalHref, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("GET: %d, want 403", rec.Code)
	}
	if n := len(trail.events); n != 0 {
		t.Errorf("a refused GET wrote %d audit row(s). A read path does not write; the "+
			"process log is enough for a page nobody could have changed anything from.", n)
	}

	if rec := b.do(http.MethodPost, legalHref, publishForm("privacy", "text")); rec.Code != http.StatusForbidden {
		t.Fatalf("POST: %d, want 403", rec.Code)
	}
	if len(trail.events) != 1 {
		t.Fatalf("a refused POST wrote %d audit rows, want exactly 1", len(trail.events))
	}
	e := trail.events[0]
	if e.Action != ActionLegalPublishRefused {
		t.Errorf("the refusal was recorded as %q, want %q", e.Action, ActionLegalPublishRefused)
	}
	if e.TenantID != panelTestTenant {
		t.Errorf("the refusal was recorded under tenant %s, want the caller's own %s. §4.5: "+
			"the tenant is the SESSION's, never the request's.", e.TenantID, panelTestTenant)
	}
	if e.ActorID == nil || *e.ActorID != uuid.MustParse("7a1e0c55-0000-4000-8000-0000000000b1") {
		t.Errorf("the refusal names actor %v, want the caller's own", e.ActorID)
	}
	// §4.7 — no address and no allow-list travels into an append-only table.
	detail := detailJSON(t, e.Detail)
	if strings.Contains(detail, "@") || strings.Contains(detail, legalTestOperator.String()) {
		t.Errorf("the audit detail carries an address or an allow-listed id: %s\n"+
			"audit_log takes no DELETE, so anything written here is a disclosure with no "+
			"expiry, and neither adds anything an actor_id does not.", detail)
	}
}

// TestLegalPublish_NamesTheCallersOwnTenantAndNobodyElses is §4.5 on the write path.
//
// The whole feature rests on this: the document rows have no tenant, and the ONE
// tenant this path names is the publisher's own, on the audit row. A handler that
// took the tenant from the FORM would be the cross-tenant capability this design
// exists to prevent, and it would look identical from the outside.
func TestLegalPublish_NamesTheCallersOwnTenantAndNobodyElses(t *testing.T) {
	t.Parallel()
	texts := newFakeTexts()
	b := legalBrowser(t, legalTestOperator, []uuid.UUID{legalTestOperator}, texts, &fakeTrail{})

	// A form that TRIES to name another tenant and another actor.
	someoneElse := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	form := publishForm("privacy", "The finished text.")
	form.Set("tenant_id", someoneElse.String())
	form.Set("actor_id", someoneElse.String())
	form.Set("published_by", someoneElse.String())

	rec := b.do(http.MethodPost, legalHref, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d, want 303", legalHref, rec.Code)
	}
	calls := texts.published()
	if len(calls) != 1 {
		t.Fatalf("the writer was called %d times, want 1", len(calls))
	}
	if calls[0].TenantID != panelTestTenant {
		t.Errorf("the writer was given tenant %s, want the session's own %s", calls[0].TenantID, panelTestTenant)
	}
	if calls[0].ActorID != legalTestOperator {
		t.Errorf("the writer was given actor %s, want the session's own %s", calls[0].ActorID, legalTestOperator)
	}
	if calls[0].Body != "The finished text." {
		t.Errorf("the writer was given body %q", calls[0].Body)
	}
}

// TestLegalPublish_RefusesASlugTheProductDoesNotHave.
func TestLegalPublish_RefusesASlugTheProductDoesNotHave(t *testing.T) {
	t.Parallel()
	for _, slug := range []string{"", "admin", "privacy2", "../privacy", "PRIVACY"} {
		texts := newFakeTexts()
		b := legalBrowser(t, legalTestOperator, []uuid.UUID{legalTestOperator}, texts, &fakeTrail{})
		rec := b.do(http.MethodPost, legalHref, publishForm(slug, "some text"))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("POST slug=%q: %d, want a 303 back to the screen", slug, rec.Code)
		}
		if n := len(texts.published()); n != 0 {
			t.Errorf("POST slug=%q reached the writer %d time(s); the closed set is the "+
				"authority, not the hidden field", slug, n)
		}
	}
	// POSITIVE CONTROL, with a slug written out by hand rather than taken from
	// legal.Slugs: if this does not reach the writer, the loop above proves nothing.
	texts := newFakeTexts()
	b := legalBrowser(t, legalTestOperator, []uuid.UUID{legalTestOperator}, texts, &fakeTrail{})
	if rec := b.do(http.MethodPost, legalHref, publishForm("imprint", "Registered somewhere.")); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST slug=imprint: %d, want 303", rec.Code)
	}
	if n := len(texts.published()); n != 1 {
		t.Fatalf("a valid slug reached the writer %d time(s), want 1", n)
	}
}

// TestLegalPublish_RefusesAnEmptyBodyAndSaysSo.
//
// A document with nothing in it would say LESS than the placeholder it replaced, and
// the page would look published while telling a reader nothing.
func TestLegalPublish_RefusesAnEmptyBodyAndSaysSo(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "   ", "\n\n\t  \r\n"} {
		texts := newFakeTexts()
		b := legalBrowser(t, legalTestOperator, []uuid.UUID{legalTestOperator}, texts, &fakeTrail{})
		rec := b.do(http.MethodPost, legalHref, publishForm("terms", body))
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("POST body=%q: %d, want 303", body, rec.Code)
		}
		if n := len(texts.published()); n != 0 {
			t.Errorf("POST body=%q reached the writer %d time(s)", body, n)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=empty") {
			t.Errorf("POST body=%q redirected to %q, which does not carry the reason", body, loc)
		}
	}
	// The screen turns the word into OUR sentence.
	b := legalBrowser(t, legalTestOperator, []uuid.UUID{legalTestOperator}, newFakeTexts(), &fakeTrail{})
	text := screenText(t, htmlOf(t, b.do(http.MethodGet, legalHref+"?problem=empty", nil)))
	if !strings.Contains(text, "There was no text in the box") {
		t.Errorf("the screen does not explain an empty submission.\n%s", text)
	}
	// AND AN UNKNOWN WORD PRODUCES NO BANNER AT ALL — a banner that fires for any
	// junk in the query string is a banner somebody can aim at an operator.
	junk := screenText(t, htmlOf(t, b.do(http.MethodGet, legalHref+"?problem=<b>hacked</b>", nil)))
	if strings.Contains(junk, "hacked") {
		t.Error("a made-up problem word reached the page")
	}
	if strings.Contains(junk, "That was not published") {
		t.Error("a made-up problem word produced a refusal banner. The list is closed so that " +
			"nothing a caller types can put a notice on an operator's screen.")
	}
}

// TestLegalPublish_RefusesABodyBiggerThanTheCeiling.
//
// 🔴 THIS ROUTE WAS THE PANEL'S ONLY UNBOUNDED POST AND A SECURITY AUDIT MEASURED IT:
// 1 MiB, 4 MiB and 9 MiB bodies were all accepted AND STORED into a table that takes
// no UPDATE and no DELETE from either role — so the only remedy for a filled table is
// a migration. With adminSessionLimit at 300 requests per ten minutes that is ~2.7 GB
// per session per window, from somebody who has merely signed in. The second half is
// the public page: a published body is rendered on /legal/*, which is deliberately
// unmetered, so an oversized document becomes CPU on an anonymous GET.
//
// BOTH ARMS ARE DRIVEN: over the ceiling is refused and reaches no writer, and a
// generous-but-legal document still publishes — otherwise the refusal could be a
// route that accepts nothing.
func TestLegalPublish_RefusesABodyBiggerThanTheCeiling(t *testing.T) {
	t.Parallel()
	texts := newFakeTexts()
	b := legalBrowser(t, legalTestOperator, []uuid.UUID{legalTestOperator}, texts, &fakeTrail{})

	over := strings.Repeat("x", maxLegalBody+1)
	rec := b.do(http.MethodPost, legalHref, publishForm("privacy", over))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST with an oversized body: %d, want a 303 back to the screen", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=too-long") {
		t.Errorf("an oversized body redirected to %q, which does not carry the reason", loc)
	}
	if n := len(texts.published()); n != 0 {
		t.Errorf("an oversized body reached the writer %d time(s). legal_documents takes no "+
			"UPDATE and no DELETE, so a row written here can only be removed by a "+
			"migration.", n)
	}

	// POSITIVE CONTROL, sized INDEPENDENTLY of the ceiling: a 40 KiB document — bigger
	// than any real privacy policy — must still publish. Without this the refusal
	// above could be a route that refuses everything.
	ok := strings.Repeat("A legal sentence that goes on. ", 1400)
	if len(ok) >= maxLegalBody {
		t.Fatalf("the control body is %d bytes, which is not under the ceiling; it is "+
			"supposed to be a realistic document, not a probe of the limit", len(ok))
	}
	rec = b.do(http.MethodPost, legalHref, publishForm("privacy", ok))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST with a %d-byte document: %d, want 303", len(ok), rec.Code)
	}
	if n := len(texts.published()); n != 1 {
		t.Fatalf("a realistic document reached the writer %d time(s), want 1", n)
	}
	// AND THE SCREEN EXPLAINS THE REFUSAL IN OUR OWN WORDS.
	text := screenText(t, htmlOf(t, b.do(http.MethodGet, legalHref+"?problem=too-long", nil)))
	if !strings.Contains(text, "longer than this screen accepts") {
		t.Errorf("the screen does not explain an oversized submission.\n%s", text)
	}
}

// TestLegalStore_CannotNameATenantAtAll is the STRUCTURAL half of §4.5, and it is
// asserted over the GENERATED types rather than over the SQL.
//
// 🔴 MUTATING A GENERATED FILE DOES NOT TEST THE NETWORK THAT GENERATES IT, so this
// reads what sqlc produced and the fix for a failure is in db/queries/legal.sql. What
// it proves is narrow and worth exactly what it says: THERE IS NO PLACE TO PUT A
// TENANT ID ON THIS PATH. A cross-tenant read is not something this code refuses to
// do — it is something this code has no parameter to express.
func TestLegalStore_CannotNameATenantAtAll(t *testing.T) {
	t.Parallel()
	// The INSERT's params.
	rt := reflect.TypeOf(store.PublishLegalDocumentParams{})
	if rt.NumField() == 0 {
		t.Fatal("PublishLegalDocumentParams has no fields; this test would pass over anything")
	}
	for i := 0; i < rt.NumField(); i++ {
		if n := strings.ToLower(rt.Field(i).Name); strings.Contains(n, "tenant") {
			t.Errorf("store.PublishLegalDocumentParams.%s exists. legal_documents carries no "+
				"tenant (migration 00020) precisely so that this path has nothing to scope "+
				"and nothing to get wrong; a tenant parameter here is the cross-tenant write "+
				"capability the design exists to prevent.", rt.Field(i).Name)
		}
	}
	// The row type.
	row := reflect.TypeOf(store.LegalDocument{})
	for i := 0; i < row.NumField(); i++ {
		if n := strings.ToLower(row.Field(i).Name); strings.Contains(n, "tenant") {
			t.Errorf("store.LegalDocument.%s exists, so the table grew a tenant column", row.Field(i).Name)
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
	// POSITIVE CONTROL with an INDEPENDENT subject: the WRITE-side interface, which
	// genuinely does I/O, must fail both of the checks above. If it does not, the
	// checks are not measuring anything.
	wt := reflect.TypeOf((*panelTexts)(nil)).Elem()
	pub, ok := wt.MethodByName("Publish")
	if !ok {
		t.Fatal("handler.panelTexts has no Publish; the control is looking at the wrong type")
	}
	if pub.Type.NumIn() == 0 {
		t.Error("the control method takes no arguments, so 'takes no arguments' does not " +
			"distinguish an I/O method from a memory read here")
	}
	hasErr := false
	for i := 0; i < pub.Type.NumOut(); i++ {
		if pub.Type.Out(i).String() == "error" {
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
// 🔴 TWO HALVES, NEITHER SUFFICIENT ALONE. The STATIC half parses this package and
// requires that no function reachable from the public surface names the writer; the
// LIVE half drives every public URL and counts what the writer was asked to do. The
// static half catches a call added tomorrow on a branch no test exercises; the live
// half catches a write that arrives some other way than a call this scan can see.
//
// ⚠️ THE STATIC HALF'S LIMIT, COUNTED RATHER THAN CLOSED: it matches the SYNTAX
// `.Publish(` inside marketing.go, so a call reached through an intermediate value
// is invisible to it. Closing that needs type resolution (go/types or x/tools/go/ssa),
// a dependency this repository does not carry for one test. The live half is what
// stands meanwhile, and it does not care how a call was spelled.
func TestLegalPublicPath_WritesNothing(t *testing.T) {
	t.Parallel()

	// STATIC: marketing.go must not name the writer at all.
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
		case "Publish":
			t.Errorf("marketing.go calls .Publish at %s. The public legal pages are a READ; "+
				"a write reachable from an unauthenticated GET is one a crawler, a prefetch "+
				"or a retried request fires.", fset.Position(call.Pos()))
		case "Paragraphs":
			// 🔴 AND IT MUST NOT SPLIT THE TEXT EITHER, WHICH IS A MUTATION THAT SURVIVED
			// THE FIRST VERSION OF THIS SUITE. Restoring `legal.Paragraphs(d.Body)` here
			// changes no output at all — the two produce identical bytes — so no
			// behavioural test can see it, and both packages stayed green. What it changes
			// is WHERE the work happens: this surface is deliberately unmetered, so a
			// split per request is CPU an anonymous caller can ask for as often as they
			// like (a security audit measured 253 ms for a pathological body). The
			// paragraphs are computed once, when a publication installs the snapshot;
			// internal/domain/legal's DB tests assert that they arrive precomputed, and
			// this asserts that the reader does not redo it.
			t.Errorf("marketing.go calls legal.Paragraphs at %s. The split belongs to the "+
				"WRITE path (internal/domain/legal.Store.set), because this one is "+
				"unmetered — see the argument on handler.Marketing.", fset.Position(call.Pos()))
		case "Published":
			seenReader = true
		}
		return true
	})
	// ANTI-VACUITY: if the scan cannot even find the READ it is supposed to see past,
	// its silence about the write means nothing.
	if !seenReader {
		t.Fatal("the scan found no call to .Published in marketing.go, so it is not walking " +
			"the code it thinks it is and its silence about .Publish proves nothing")
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
	if n := len(texts.published()); n != 0 {
		t.Errorf("loading the public pages reached the writer %d time(s)", n)
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

// TestLegalScreen_ShowsWhatEachDocumentIsWaitingForAndWhatIsLive.
//
// 🔴 THE SCREEN'S PROGRESS LINE IS DERIVED, so it cannot disagree with the rows under
// it. That is asserted at BOTH ends: nothing published, and one published.
func TestLegalScreen_ShowsWhatEachDocumentIsWaitingForAndWhatIsLive(t *testing.T) {
	t.Parallel()
	operator := uuid.MustParse("7a1e0c55-0000-4000-8000-0000000000a1")
	allow := []uuid.UUID{uuid.MustParse("7a1e0c55-0000-4000-8000-0000000000a1")}

	empty := legalBrowser(t, operator, allow, newFakeTexts(), &fakeTrail{})
	text := screenText(t, htmlOf(t, empty.do(http.MethodGet, legalHref, nil)))
	if !strings.Contains(text, "None of the four is published yet") {
		t.Errorf("the screen does not say that nothing is published.\n%s", text)
	}
	for _, p := range pages.LegalPages {
		if !strings.Contains(text, p.Title) {
			t.Errorf("the screen offers no editor for %q, so it cannot be published", p.Title)
		}
	}
	// The facts each document is waiting for are shown HERE, beside the box, not only
	// on the public page — the person who has them is the person on this screen.
	for _, n := range pages.LegalPages[0].Needs {
		if !strings.Contains(text, n) {
			t.Errorf("the screen does not print %q, which the public page is waiting on", n)
		}
	}

	texts := newFakeTexts()
	texts.put("privacy", "The finished privacy policy.")
	one := legalBrowser(t, legalTestOperator, []uuid.UUID{legalTestOperator}, texts, &fakeTrail{})
	body := htmlOf(t, one.do(http.MethodGet, legalHref, nil))
	text = screenText(t, body)
	if !strings.Contains(text, "1 of 4 published") {
		t.Errorf("the progress line does not count the published document.\n%s", text)
	}
	// The textarea re-opens on what was published, VERBATIM, so an edit is an edit
	// rather than a retype.
	if !strings.Contains(body, "The finished privacy policy.") {
		t.Error("the form does not re-open on the published text")
	}
}

// TestLegalScreen_ReopensOnPastedMarkupWithoutExecutingIt — the escaping half, on the
// way BACK IN. The public page has its own test; this is the operator's own screen,
// which is where a pasted document is most likely to be looked at twice.
func TestLegalScreen_ReopensOnPastedMarkupWithoutExecutingIt(t *testing.T) {
	t.Parallel()
	const attack = `</textarea><script>alert(1)</script>`
	texts := newFakeTexts()
	texts.put("terms", attack)
	b := legalBrowser(t, legalTestOperator, []uuid.UUID{legalTestOperator}, texts, &fakeTrail{})
	body := htmlOf(t, b.do(http.MethodGet, legalHref, nil))
	if strings.Contains(body, "</textarea><script>") {
		t.Fatal("the operator's own textarea was closed by the text it re-opened on. A " +
			"document pasted from a website is the ordinary case here, not an attack.")
	}
	if !strings.Contains(body, "&lt;/textarea&gt;&lt;script&gt;") {
		t.Error("the escaped form is absent, so the assertion above may be passing because " +
			"the text never rendered at all")
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
