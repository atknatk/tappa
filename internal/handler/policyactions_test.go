package handler

// policyactions_test.go -- M6-09 phase B's HTTP boundary.
//
// WHAT IS HERE AND WHAT IS IN internal/domain/tenant/rulewriter_db_test.go. Whether
// a change is LEGAL -- the engine's answer, the append-only rule, the version race,
// §4.5 -- is measured against real Postgres in the package that owns the gate. What
// is here is what a fake can prove and a database cannot: that the routes take the
// WRITING chain, that nothing is written without the confirmation step, that a
// refusal reaches the trail rather than being swallowed, and that the page offers a
// manager no control the server would turn away.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// policyWriteBrowser is a signed-in owner whose scribe is under the test's control.
func policyWriteBrowser(t *testing.T, scribe *fakeScribe) *browser {
	t.Helper()
	return policyBrowserWith(t, newFakeRules(), scribe)
}

// TestPolicyRoutes_TakeTheWritingChain.
//
// 🔴 THE MEASUREMENT IS COST, NOT OUTCOME -- TestSameOriginGate_RefusesBeforeTheResolver's
// argument, applied to this section's two routes. ProtectWriting runs the Origin
// check BEFORE the resolver; a route registered in mountSections would take the READ
// chain and run the resolver first, which is the defect an audit measured on POST
// /admin/review (300 cross-origin posts spending a signed-in manager's whole session
// budget and charging 301 unbudgeted lookups on the way). The OUTCOME is a redirect
// either way, so the only thing that can tell the two chains apart is the number of
// resolver calls.
func TestPolicyRoutes_TakeTheWritingChain(t *testing.T) {
	for _, href := range []string{policyChangeHref, policyApplyHref} {
		t.Run(href, func(t *testing.T) {
			scribe := newFakeScribe()
			b, resolved := policyOriginBrowser(t, scribe)
			b.origin = "https://evil.example"
			b.do(http.MethodPost, href, url.Values{"op": {opDisable}, "policy": {uuid.NewString()}})
			if *resolved != 0 {
				t.Errorf("the resolver ran %d time(s) for a cross-origin POST to %s; the "+
					"same-origin refusal must happen BEFORE it or this route has no cheap "+
					"defence at all", *resolved, href)
			}
			if len(scribe.calls) > 0 || len(scribe.refusals) > 0 {
				t.Errorf("a cross-origin POST reached the writer: %v / %v", scribe.calls, scribe.refusals)
			}
			// POSITIVE CONTROL: the SAME request from this origin does reach the resolver,
			// so the zero above is the gate rather than a broken harness.
			b2, resolved2 := policyOriginBrowser(t, newFakeScribe())
			b2.do(http.MethodPost, href, url.Values{"op": {opDisable}, "policy": {uuid.NewString()}})
			if *resolved2 == 0 {
				t.Errorf("a SAME-origin POST to %s did not reach the resolver either", href)
			}
		})
	}
}

// policyOriginBrowser is policyBrowserWith plus a resolver-call counter.
func policyOriginBrowser(t *testing.T, scribe panelScribe) (*browser, *int) {
	t.Helper()
	calls := 0
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		calls++
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: policyTestAdminRole, FullName: policyTestAdminName,
		}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		scribe, adminTestConfig(), discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b, &calls
}

// TestPolicyApply_WritesNothingWithoutTheConfirmationStep.
//
// 🔴 THE CONFIRMATION IS THE CARD'S "TAKES EFFECT IMMEDIATELY" ANSWER, so a write
// that could skip it would remove the only answer v1 has (the simulator is M9-06).
// The assertion is on the SCRIBE rather than on the status code: a redirect with a
// problem word looks the same whether or not the domain was called.
func TestPolicyApply_WritesNothingWithoutTheConfirmationStep(t *testing.T) {
	scribe := newFakeScribe()
	b := policyWriteBrowser(t, scribe)
	rec := b.do(http.MethodPost, policyApplyHref, url.Values{
		"op": {opDisable}, "policy": {uuid.NewString()},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d, want 303", policyApplyHref, rec.Code)
	}
	if len(scribe.calls) != 0 {
		t.Errorf("an unconfirmed apply reached the writer: %v", scribe.calls)
	}
	if to := rec.Header().Get("Location"); !strings.Contains(to, "problem=confirm-required") {
		t.Errorf("an unconfirmed apply redirected to %q, which does not say why", to)
	}
}

// TestPolicyApply_CarriesTheChangeTheWarningDescribed.
//
// The confirmation is minted by the review step, so this drives the two steps in
// order rather than forging a token: what is being measured is that the pair works
// end to end and that the command the domain receives is the one the screen showed.
func TestPolicyApply_CarriesTheChangeTheWarningDescribed(t *testing.T) {
	scribe := newFakeScribe()
	b := policyWriteBrowser(t, scribe)
	target := uuid.New()

	form := url.Values{"op": {opDisable}, "policy": {target.String()}}
	rec := b.do(http.MethodPost, policyChangeHref, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("the review step answered %d, want 200", rec.Code)
	}
	if len(scribe.calls) != 0 {
		t.Fatalf("the REVIEW step wrote something: %v", scribe.calls)
	}
	page := htmlOf(t, rec)
	token := hiddenValue(t, page, confirmField)
	if token == "" {
		t.Fatal("the confirmation screen rendered no token, so the write cannot be reached")
	}
	// THE WARNING HAS TO SAY WHAT IT CANNOT TELL YOU. This is the sentence that
	// replaces the simulator the card originally demanded.
	if !strings.Contains(page, htmlText("cannot tell you how many of last week")) {
		t.Error("the confirmation screen does not say that the consequences were NOT checked; " +
			"without it the page implies a simulation that does not exist")
	}

	form.Set(confirmField, token)
	rec = b.do(http.MethodPost, policyApplyHref, form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the apply step answered %d, want 303", rec.Code)
	}
	if len(scribe.calls) != 1 || scribe.calls[0] != "setEnabled" {
		t.Fatalf("the apply step called %v, want exactly one setEnabled", scribe.calls)
	}
	if scribe.enable.PolicyID != target || scribe.enable.Enabled {
		t.Errorf("the command reached the domain as %+v, want policy %s switched OFF",
			scribe.enable, target)
	}
	// 🔴 THE ACTOR IS THE SESSION'S, NOT THE FORM'S. The role is what the guardrail
	// keys on, so a role a caller could post would be an attacker's own claim about
	// themselves.
	if scribe.actor.ID != panelTestAdmin || scribe.actor.Role != policyTestAdminRole {
		t.Errorf("the actor reached the domain as %+v, want the resolved session's admin and role",
			scribe.actor)
	}
}

// TestPolicyChange_ARefusedActorReachesNoWriteAndIsRecorded.
//
// 🔴 THE REFUSAL IS WRITTEN DOWN, which is the half a role check in a handler
// normally forgets. The screen never offers these controls to a manager, so reaching
// the route means the UI was bypassed -- and the person who most needs to know that
// is the owner, who cannot read a process log.
func TestPolicyChange_ARefusedActorReachesNoWriteAndIsRecorded(t *testing.T) {
	scribe := &fakeScribe{may: false}
	b := policyWriteBrowser(t, scribe)
	target := uuid.New()

	rec := b.do(http.MethodPost, policyChangeHref, url.Values{
		"op": {opDisable}, "policy": {target.String()},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a refused review step answered %d, want 303", rec.Code)
	}
	if to := rec.Header().Get("Location"); !strings.Contains(to, "problem=not-permitted") {
		t.Errorf("the refusal redirected to %q, which does not say why", to)
	}
	if len(scribe.calls) != 0 {
		t.Errorf("a refused actor reached the writer: %v", scribe.calls)
	}
	if want := opDisable + "/" + target.String(); len(scribe.refusals) != 1 || scribe.refusals[0] != want {
		t.Errorf("the trail recorded %v, want one entry %q", scribe.refusals, want)
	}
	// AND NO CONFIRMATION IS MINTED FOR SOMEBODY WHO MAY NOT ACT. A token handed to a
	// refused caller would be a token they could spend if the answer ever changed
	// between two requests.
	for _, c := range rec.Result().Cookies() {
		if c.Name == adminConfirmCookieName && c.Value != "" {
			t.Error("a refused actor was given a confirmation cookie")
		}
	}
}

// TestPolicyChange_RefusesAnOperationOutsideItsVocabulary.
func TestPolicyChange_RefusesAnOperationOutsideItsVocabulary(t *testing.T) {
	for _, op := range []string{"", "delete", "DISABLE", "enable-all", "drop"} {
		scribe := newFakeScribe()
		b := policyWriteBrowser(t, scribe)
		rec := b.do(http.MethodPost, policyChangeHref, url.Values{
			"op": {op}, "policy": {uuid.NewString()},
		})
		if rec.Code != http.StatusSeeOther || len(scribe.calls) != 0 {
			t.Errorf("op=%q answered %d and called %v; a word outside the closed list must be "+
				"refused, never defaulted", op, rec.Code, scribe.calls)
		}
	}
}

// TestPolicyBind_RefusesAScopeTheGrammarDoesNotAdmit.
//
// The grammar is the DOMAIN's (tenant.ValidResource), asked rather than repeated --
// so this measures that the boundary asks it, which is what stops a binding to
// `location/rusty bar`: a string that binds to nothing, silently, forever.
func TestPolicyBind_RefusesAScopeTheGrammarDoesNotAdmit(t *testing.T) {
	bad := []string{"", "location", "location/", "location/not-a-uuid", "tenant/" + uuid.NewString(),
		"*/" + uuid.NewString(), "location/" + uuid.NewString() + "/extra"}
	for _, resource := range bad {
		scribe := newFakeScribe()
		b := policyWriteBrowser(t, scribe)
		rec := b.do(http.MethodPost, policyChangeHref, url.Values{
			"op": {opBind}, "policy": {uuid.NewString()}, "resource": {resource},
		})
		if rec.Code != http.StatusSeeOther || len(scribe.calls) != 0 {
			t.Errorf("resource=%q answered %d and called %v, want a refusal", resource, rec.Code, scribe.calls)
		}
	}
	// POSITIVE CONTROL: the two shapes the picker really offers get through, or the
	// loop above is measuring a route that refuses everything.
	for _, good := range []string{"*", "location/" + uuid.NewString(), "department/" + uuid.NewString()} {
		scribe := newFakeScribe()
		b := policyWriteBrowser(t, scribe)
		rec := b.do(http.MethodPost, policyChangeHref, url.Values{
			"op": {opBind}, "policy": {uuid.NewString()}, "resource": {good},
		})
		if rec.Code != http.StatusOK {
			t.Errorf("resource=%q answered %d, want the confirmation screen", good, rec.Code)
		}
	}
}

// TestPolicyConfirmation_IsBoundToTheWHOLEChange.
//
// 🔴 A CONFIRMATION MINTED FOR ONE CHANGE MUST NOT OPEN ANOTHER. Every other gate in
// this panel binds a row's identity; a policy change is a STATEMENT (an operation, a
// policy, a scope), so a token bound to the policy alone would let a warning served
// for "switch this off" be spent on "bind it everywhere". That is manualentry.go's
// compound-subject argument arriving on a different screen.
func TestPolicyConfirmation_IsBoundToTheWHOLEChange(t *testing.T) {
	target := uuid.New()
	base := url.Values{"op": {opDisable}, "policy": {target.String()}}

	scribe := newFakeScribe()
	b := policyWriteBrowser(t, scribe)
	rec := b.do(http.MethodPost, policyChangeHref, base)
	token := hiddenValue(t, htmlOf(t, rec), confirmField)
	if token == "" {
		t.Fatal("no confirmation was minted; this test would measure nothing")
	}

	// 🔴 THE SAME BROWSER IS REUSED, AND THE FIRST VERSION OF THIS TEST DID NOT DO
	// THAT — WHICH MADE IT VACUOUS. The confirmation has TWO halves: the MAC in the
	// form and its twin in a cookie. A fresh browser per case carries no cookie, so
	// every case was refused as `confirm-required` whatever the subject said — and a
	// mutation that removed the OPERATION from the subject entirely stayed GREEN.
	// Measured, then fixed: this reuses b, which holds the cookie the mint set.
	others := []url.Values{
		{"op": {opEnable}, "policy": {target.String()}},
		{"op": {opDisable}, "policy": {uuid.NewString()}},
		{"op": {opBind}, "policy": {target.String()}, "resource": {"*"}},
	}
	for _, form := range others {
		f := url.Values{}
		for k, v := range form {
			f[k] = v
		}
		f.Set(confirmField, token)
		scribe.calls = nil
		rec := b.do(http.MethodPost, policyApplyHref, f)
		if len(scribe.calls) != 0 {
			t.Errorf("a confirmation minted for %v opened %v: the writer was called (%v)",
				base, form, scribe.calls)
		}
		if to := rec.Header().Get("Location"); !strings.Contains(to, "problem=confirm") {
			t.Errorf("spending a confirmation on %v redirected to %q, which does not name the "+
				"confirmation as the reason", form, to)
		}
	}
	// ANTI-VACUITY: the SAME browser and the SAME token DO open the gate for the
	// change they were minted for. Without this the loop above would pass for a
	// harness that simply never carries a confirmation at all — which is exactly how
	// its first version passed a mutation that deleted the operation from the subject.
	f := url.Values{}
	for k, v := range base {
		f[k] = v
	}
	f.Set(confirmField, token)
	scribe.calls = nil
	if b.do(http.MethodPost, policyApplyHref, f); len(scribe.calls) != 1 {
		t.Fatalf("the confirmation does not open its OWN gate (%v); every refusal above is "+
			"then a refusal of the harness rather than of the binding", scribe.calls)
	}
}

// TestPoliciesSection_OffersAManagerNoControlAtAll.
//
// The hiding is the COURTESY and the gate is the guarantee (that half is measured
// against real Postgres). What this pins is that the courtesy is real: a page that
// showed the buttons and let the server refuse them would teach a manager that the
// panel is broken.
func TestPoliciesSection_OffersAManagerNoControlAtAll(t *testing.T) {
	editable := policyScreenFixture()
	for i := range editable.Baseline {
		editable.Baseline[i].PolicyID = uuid.New()
		editable.Baseline[i].Attachments = []string{"*"}
	}
	editable.ScopeTargets = []tenant.ScopeTarget{{Kind: "location", ID: uuid.New(), Name: "Rusty Bar"}}

	allowed := policyStatePage(t, policyPageState{screen: editable, mayEdit: true})
	refused := policyStatePage(t, policyPageState{screen: editable, mayEdit: false})

	// ANTI-VACUITY FIRST: the allowed page must really carry the controls, or the
	// assertion below passes because the fixture renders nothing either way.
	for _, want := range []string{policyChangeHref, "Switch this rule off", "Record the binding"} {
		if !strings.Contains(allowed, htmlText(want)) {
			t.Fatalf("the EDITABLE page does not carry %q, so this test measures nothing", want)
		}
	}
	for _, forbidden := range []string{policyChangeHref, policyApplyHref, "Switch this rule off",
		"Record the binding", "Show me what this would do"} {
		if strings.Contains(refused, htmlText(forbidden)) {
			t.Errorf("a page the engine refuses policy:edit still offers %q", forbidden)
		}
	}
	// AND IT SAYS WHY, in the engine's terms rather than in role terms -- an owner
	// whose rulebook has never been set up lands on the same page.
	if !strings.Contains(refused, htmlText("does not currently grant you")) {
		t.Error("the read-only page does not say why there are no controls")
	}
}

// TestPolicyVersionPage_IsBoundedAndSaysWhichVersionItIs -- obligation 7.
func TestPolicyVersionPage_IsBoundedAndSaysWhichVersionItIs(t *testing.T) {
	rules := newFakeRules()
	rules.version = tenant.VersionBody{
		PolicyID: uuid.New(), PolicyName: "Nights at the Rusty Bar", Layer: "tenant",
		Enabled: true, VersionNo: 2, Author: tenant.AuthorSystem, Bytes: 412, Current: false,
		Statements: []tenant.RuleStatement{{
			Sid: "own:night", Effect: "review", Actions: []string{"tap:record"},
			Resources: []string{"location/x"}, Reason: "zz-version-reason",
		}},
	}
	b := policyBrowser(t, rules)
	rec := b.do(http.MethodGet, policyVersionHref+"?policy="+rules.version.PolicyID.String()+"&no=2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the version page: %d, want 200", rec.Code)
	}
	page := htmlOf(t, rec)
	for _, want := range []string{"version 2", "zz-version-reason", "an earlier version"} {
		if !strings.Contains(page, htmlText(want)) {
			t.Errorf("the version page does not carry %q", want)
		}
	}
	// 🔴 "AN EARLIER VERSION" IS NOT "WRONG" AND NOT "DRAFT". It is what some records
	// were judged by, which is the reason the page exists at all.
	if !strings.Contains(page, htmlText("decides nothing today")) {
		t.Error("the version page does not say that a superseded version decides nothing")
	}
	if rules.versionNo != 2 || rules.versionPolicy != rules.version.PolicyID {
		t.Errorf("the read asked for policy %s version %d", rules.versionPolicy, rules.versionNo)
	}

	// A BAD REQUEST IS A REDIRECT WITH A WORD, never a 500 and never a page about
	// somebody else's rule.
	for _, bad := range []string{"?policy=nope&no=1", "?policy=" + uuid.NewString() + "&no=0", "?no=1"} {
		rec := policyBrowser(t, newFakeRules()).do(http.MethodGet, policyVersionHref+bad, nil)
		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s answered %d, want 303", policyVersionHref+bad, rec.Code)
		}
	}
}

// TestPolicyVersionPage_AnotherBusinessesVersionIsTheSameAnswerAsNoVersion.
//
// ⚠️ WHAT THIS MEASURES IS THE ROUTE HALF, NOT THE DATA HALF, AND THE FIRST VERSION
// OF THIS COMMENT OVERSTATED IT. It said the fake "is what the domain does under RLS"
// — which is an assumption dressed as a measurement, because the fake is told what to
// return. What is measured here: given the SAME domain answer for a foreign id and
// for an absent one, the route produces the SAME response, with no way to tell them
// apart. That the domain really answers the same for both is measured elsewhere —
// against real Postgres in internal/domain/tenant (the reader's own tenant filter
// plus RLS), and observed live on the development database (a KM version id requested
// as KF answers exactly as a version that never existed).
//
// The distinction matters because the two halves fail differently: this one would go
// red on a handler that leaked the difference into a status code or a query word, and
// it would stay green on a reader that leaked it into an error type.
func TestPolicyVersionPage_AnotherBusinessesVersionIsTheSameAnswerAsNoVersion(t *testing.T) {
	rules := newFakeRules()
	rules.versionErr = tenant.ErrUnknownVersion

	foreign, absent := uuid.New(), uuid.New()
	var seen []*httptest.ResponseRecorder
	for _, id := range []uuid.UUID{foreign, absent} {
		b := policyBrowser(t, rules)
		rec := b.do(http.MethodGet, policyVersionHref+"?policy="+id.String()+"&no=1", nil)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("GET a version for %s answered %d, want 303", id, rec.Code)
		}
		seen = append(seen, rec)
	}
	if a, b := seen[0].Header().Get("Location"), seen[1].Header().Get("Location"); a != b {
		t.Errorf("another business's version answers %q and a non-existent one answers %q; "+
			"the difference is an oracle for whether an id exists somewhere else", a, b)
	}
	if to := seen[0].Header().Get("Location"); !strings.Contains(to, "problem=no-such-version") {
		t.Errorf("the refusal redirected to %q, which does not say why", to)
	}
	// AND THE TENANT THE READ RAN IN IS THE SESSION'S, never the request's — there is
	// no tenant parameter on this route, so the only thing to check is that the
	// resolved one is what reached the domain.
	if rules.tenant != panelTestTenant {
		t.Errorf("the version read ran in tenant %s, want the session's %s",
			rules.tenant, panelTestTenant)
	}
}

// TestPolicyApply_TheTwoLockoutOutcomesGetOppositeSentences.
//
// The domain has two sentinels because the two outcomes need opposite words. This is
// the boundary half: each must reach a DIFFERENT problem word, and the page must
// print the matching sentence — a customer told "it was put back" when it was not is
// the worst reading available on this screen.
func TestPolicyApply_TheTwoLockoutOutcomesGetOppositeSentences(t *testing.T) {
	// 🔴 THE HEADING IS CHECKED TOO, AND IT WAS NOT FOR A ROUND. This test read only
	// the body, so the notice's TITLE stayed "That change was not made" for
	// `lockout-stands` — where the change WAS made and could not be undone. The string
	// appeared in no test in the repository; the protection built for exactly this
	// defect was not reading the element the defect was in.
	cases := map[error]struct{ word, heading, says string }{
		tenant.ErrWouldLockOut:  {"lockout", "That change was not made", "it was put back"},
		tenant.ErrLockoutStands: {"lockout-stands", "That change was made, and it locked you out", "putting it back FAILED"},
	}
	for err, want := range cases {
		scribe := newFakeScribe()
		scribe.err = err
		b := policyWriteBrowser(t, scribe)
		form := url.Values{"op": {opDisable}, "policy": {uuid.NewString()}}
		rec := b.do(http.MethodPost, policyChangeHref, form)
		token := hiddenValue(t, htmlOf(t, rec), confirmField)
		form.Set(confirmField, token)
		rec = b.do(http.MethodPost, policyApplyHref, form)
		to := rec.Header().Get("Location")
		if !strings.Contains(to, "problem="+want.word) {
			t.Errorf("%v redirected to %q, want problem=%s", err, to, want.word)
		}
		// AND THE PAGE PRINTS THE MATCHING HEADING AND SENTENCE.
		page := policyStatePage(t, policyPageState{screen: policyScreenFixture(), problem: want.word})
		if !strings.Contains(page, htmlText(want.says)) {
			t.Errorf("problem=%s does not print %q", want.word, want.says)
		}
		if !strings.Contains(page, htmlText(want.heading)) {
			t.Errorf("problem=%s does not print the heading %q. The notice's title is the "+
				"largest sentence on the page; a body that says the change stands under a "+
				"title that says it was not made is worse than either alone.",
				want.word, want.heading)
		}
	}
	// AND THE TWO HEADINGS ARE DIFFERENT, or the loop passes for a page that prints one
	// title for both outcomes — which is exactly what it did.
	stood := policyStatePage(t, policyPageState{screen: policyScreenFixture(), problem: "lockout-stands"})
	if strings.Contains(stood, htmlText("That change was not made")) {
		t.Error("the page titles a change that STANDS with “That change was not made”")
	}
	// AND THE TWO SENTENCES ARE NOT THE SAME ONE. Without this the loop passes for a
	// page that prints "it was put back" for both.
	stands := policyStatePage(t, policyPageState{screen: policyScreenFixture(), problem: "lockout-stands"})
	if strings.Contains(stands, htmlText("so it was put back")) {
		t.Error("the page tells an owner whose change STANDS that it was put back")
	}
}

// TestPolicyConfirmation_BindsTheRuleNameTheScreenShowed.
//
// 🔴 THE NAME WAS OUTSIDE THE MAC FOR FOUR ROUNDS, AND THE SUBJECT'S OWN COMMENT
// ARGUED FOR IT ("it is a label, it decides nothing"). An audit measured the cost: a
// token minted while the confirmation screen said `Called: Alpha` was spent with
// `name=Beta-a-completely-different-rule`, the write answered 303, and Beta was
// stored. The whole claim of the confirmation step is that it shows the TEXT of what
// will be written; a field it does not bind is a field the screen can be wrong about.
func TestPolicyConfirmation_BindsTheRuleNameTheScreenShowed(t *testing.T) {
	basedOn := tenant.AuthorableRules()[0].Name
	form := url.Values{"op": {opAuthor}, "name": {"Alpha"}, "based_on": {basedOn}}

	scribe := newFakeScribe()
	b := policyWriteBrowser(t, scribe)
	rec := b.do(http.MethodPost, policyChangeHref, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("the review step answered %d, want 200", rec.Code)
	}
	page := htmlOf(t, rec)
	token := hiddenValue(t, page, confirmField)
	if token == "" || !strings.Contains(page, htmlText("Alpha")) {
		t.Fatalf("the confirmation screen did not show the name it minted for (token=%q)", token)
	}

	// THE SWAP, on the SAME browser so the confirmation cookie travels.
	swapped := url.Values{}
	for k, v := range form {
		swapped[k] = v
	}
	swapped.Set("name", "Beta-a-completely-different-rule")
	swapped.Set(confirmField, token)
	scribe.calls = nil
	rec = b.do(http.MethodPost, policyApplyHref, swapped)
	if len(scribe.calls) != 0 {
		t.Errorf("a confirmation minted while the screen said “Alpha” wrote %q. The screen "+
			"showed one name and the store received another.", scribe.author.Name)
	}
	if to := rec.Header().Get("Location"); !strings.Contains(to, "problem=confirm") {
		t.Errorf("the swapped name redirected to %q, which does not name the confirmation "+
			"as the reason", to)
	}
	// ANTI-VACUITY: the UNSWAPPED name still opens its own gate on the same browser,
	// so the refusal above is the binding rather than a harness that never confirms.
	same := url.Values{}
	for k, v := range form {
		same[k] = v
	}
	same.Set(confirmField, token)
	scribe.calls = nil
	b.do(http.MethodPost, policyApplyHref, same)
	if len(scribe.calls) != 1 || scribe.author.Name != "Alpha" {
		t.Fatalf("the confirmation does not open its OWN gate (calls=%v name=%q)",
			scribe.calls, scribe.author.Name)
	}
}

// TestPolicyApply_ARefusedActorAtTheWRITEStepIsRecorded.
//
// 🔴 THE APPLY STEP'S REFUSAL ARM WAS DEAD CODE AND NOTHING NOTICED. policyChangeApply
// deliberately does not re-ask May — the domain gate is the guarantee and asking twice
// would be a second representation of it — so its ErrNotPermitted arm is reached only
// when the WRITER refuses. The double did not refuse, so an audit deleted
// a.refusePolicyEdit from that arm and the whole package stayed green, leaving
// obligation 4 ("a refused attempt is written down too") unproven at the step where
// the write would have happened.
//
// This drives the pair with a scribe that refuses the way production does: the review
// step mints a confirmation for an actor the engine still allowed, the answer changes
// underneath (a rulebook can change between two requests, which is exactly why the
// write re-asks), and the apply must write nothing and record the attempt.
func TestPolicyApply_ARefusedActorAtTheWRITEStepIsRecorded(t *testing.T) {
	scribe := newFakeScribe()
	b := policyWriteBrowser(t, scribe)
	target := uuid.New()
	form := url.Values{"op": {opDisable}, "policy": {target.String()}}

	rec := b.do(http.MethodPost, policyChangeHref, form)
	token := hiddenValue(t, htmlOf(t, rec), confirmField)
	if token == "" {
		t.Fatal("the review step minted no confirmation")
	}
	// THE ENGINE'S ANSWER CHANGES between the two requests.
	scribe.may = false
	scribe.calls, scribe.refusals = nil, nil

	form.Set(confirmField, token)
	rec = b.do(http.MethodPost, policyApplyHref, form)
	if len(scribe.calls) != 0 {
		t.Errorf("the write went through for an actor the writer refuses: %v", scribe.calls)
	}
	if to := rec.Header().Get("Location"); !strings.Contains(to, "problem=not-permitted") {
		t.Errorf("the refusal redirected to %q, which does not say why", to)
	}
	if want := opDisable + "/" + target.String(); len(scribe.refusals) != 1 || scribe.refusals[0] != want {
		t.Errorf("the trail recorded %v, want one entry %q. A refusal at the WRITE step is "+
			"the one the owner most needs to see: it means the UI was bypassed or the "+
			"rulebook changed between the warning and the button.", scribe.refusals, want)
	}
}

// TestPolicyChange_DescribesEveryShapeOf00008.
//
// 🔴 THE SENTENCE THIS PINS HAS BEEN WRONG THREE TIMES, EACH TIME ABOUT A DIFFERENT
// ARM OF transactions_policy_decision_consistent. It promised a stored version for
// every record (false for the guardrail arm and for the no-decision arm); then it was
// corrected to promise that every record names a RULE (false for the no-decision arm);
// and that arm is not hypothetical — the manual-entry path shipped one task earlier
// and takes it, because db/queries/transactions.sql leaves the four policy columns out
// of its column list entirely. Measured on the development database: 234 of 546 manual
// rows name no rule at all, and 8869 of 18576 rows overall.
//
// A customer reads this on the last screen before a change they cannot take back, and
// what they take from it is "the past explains itself". It has to be true of all four
// shapes 00008 admits, so the assertion is about the vocabulary the sentence uses:
// every claim about naming must be CONDITIONAL, and the hand-entered case must be
// named. This is the fourth-version brake.
func TestPolicyChange_DescribesEveryShapeOf00008(t *testing.T) {
	scribe := newFakeScribe()
	b := policyWriteBrowser(t, scribe)
	rec := b.do(http.MethodPost, policyChangeHref,
		url.Values{"op": {opDisable}, "policy": {uuid.NewString()}})
	if rec.Code != http.StatusOK {
		t.Fatalf("the confirmation step answered %d, want 200", rec.Code)
	}
	page := htmlOf(t, rec)

	// THE THREE SHAPES THE SENTENCE MUST ADMIT, in the words it uses for them.
	for what, phrase := range map[string]string{
		"a rule decided it (conditional, not universal)": "Where a rule decided one",
		"a guarantee decided it — no stored version":     "no stored version to name",
		"a record typed in by hand — no rule at all":     "names the person who entered it",
	} {
		if !strings.Contains(page, htmlText(phrase)) {
			t.Errorf("the confirmation screen does not describe %s (looked for %q).\n"+
				"migration 00008 admits four shapes and this sentence has been wrong about a "+
				"different one three times; a customer reads it on the last screen before an "+
				"act with no route back.", what, phrase)
		}
	}
	// AND IT MUST NOT MAKE THE UNIVERSAL CLAIM AGAIN, in any of the three forms it has
	// taken. This is the half a positive check cannot do.
	for _, forbidden := range []string{
		"Each one names the rule that judged it",
		"every one of them names the exact version",
		"names the exact version of the rule that judged it",
	} {
		if strings.Contains(page, htmlText(forbidden)) {
			t.Errorf("the confirmation screen is back to the universal claim %q, which is "+
				"false for a manual record (no rule at all) and for a guardrail decision "+
				"(no stored version).", forbidden)
		}
	}
}

// hiddenValue pulls one hidden input's value out of a rendered page.
func hiddenValue(t *testing.T, page, name string) string {
	t.Helper()
	marker := `name="` + name + `" value="`
	i := strings.Index(page, marker)
	if i < 0 {
		return ""
	}
	rest := page[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return ""
	}
	return rest[:j]
}
