package handler

// policies_test.go -- M6-09 phase A's unit-level nets.
//
// WHAT IS HERE AND WHAT IS IN internal/domain/tenant/rulebook_db_test.go. Whether
// the screen agrees with the DATABASE -- §4.5, the four provisioning states, and the property that a page
// view writes nothing -- is measured against real Postgres, because a fake reader
// can only confirm what the test told it. What is here is what a fake CAN prove:
// the sentences this screen is obliged to print, the controls it is forbidden to
// offer, and the two ties that keep its claims honest as the product changes.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/web/templates/pages"
)

// fakeRules is panelRules under the test's control.
type fakeRules struct {
	screen tenant.PolicyScreen
	err    error
	calls  int
	tenant uuid.UUID

	version       tenant.VersionBody
	versionErr    error
	versionCalls  int
	versionPolicy uuid.UUID
	versionNo     int
}

func (f *fakeRules) Screen(_ context.Context, tenantID uuid.UUID) (tenant.PolicyScreen, error) {
	f.calls++
	f.tenant = tenantID
	if f.err != nil {
		return tenant.PolicyScreen{}, f.err
	}
	return f.screen, nil
}

func (f *fakeRules) Version(_ context.Context, tenantID, policyID uuid.UUID, no int) (tenant.VersionBody, error) {
	f.versionCalls++
	f.tenant, f.versionPolicy, f.versionNo = tenantID, policyID, no
	if f.versionErr != nil {
		return tenant.VersionBody{}, f.versionErr
	}
	return f.version, nil
}

// newFakeRules is the default one every other section's harness gets: a screen that
// answers, so a test about the employees section never fails because of this one.
//
// 🔴 ITS SCREEN CARRIES STORED POLICY IDS AND SCOPE TARGETS, WHICH IS NOT DECORATION.
// The brand nets (TestPanelScreens_EveryPressTargetCarriesATouchTargetClass and its
// stylesheet twin) build their corpus from THIS fixture, and a screen with no
// PolicyID renders no switch, no bind, no unbind and no version link — measured: one
// form on the page, all of it the shell's sign-out. Nine of this section's ten press
// targets were outside the touch-target net's subject while the net reported it had
// scanned the policies screen. The product was clean (every one carries `btn`, and
// `.btn` reserves 44px); what was not clean was the reach of the check.
func newFakeRules() *fakeRules { return &fakeRules{screen: policyScreenWithControls()} }

// policyScreenWithControls is the realistic screen: stored ids, bindings and venues,
// so a page rendered from it carries the controls a signed-in owner really sees.
func policyScreenWithControls() tenant.PolicyScreen {
	s := policyScreenFixture()
	s.ScopeTargets = []tenant.ScopeTarget{
		{Kind: "location", ID: uuid.New(), Name: "Rusty Bar"},
		{Kind: "department", ID: uuid.New(), Name: "Kitchen"},
	}
	for i := range s.Baseline {
		s.Baseline[i].PolicyID = uuid.New()
		s.Baseline[i].Attachments = []string{"*"}
	}
	// ONE RULE SWITCHED OFF, so the enable arm of the switch is in the corpus too.
	if len(s.Baseline) > 1 {
		s.Baseline[1].State = tenant.StateOff
		s.Baseline[1].Statements = nil
	}
	return s
}

// fakeScribe is panelScribe under the test's control. It RECORDS rather than acts,
// because what the handler owes is a correctly shaped command and a refusal that is
// not turned into a success -- whether the write is legal is measured against real
// Postgres in internal/domain/tenant/rulewriter_db_test.go, where the gate lives.
type fakeScribe struct {
	may bool
	// err is returned by whichever act is called next.
	err error
	// calls records every act in order, as "<op>", so a test can assert that a
	// refused request reached NO act at all.
	calls    []string
	enable   tenant.EnableCommand
	bind     tenant.BindCommand
	author   tenant.AuthorCommand
	refusals []string
	actor    tenant.Actor
}

// newFakeScribe is the default: an admin the engine allows. A harness for another
// section must not fail because this one refuses.
func newFakeScribe() *fakeScribe { return &fakeScribe{may: true} }

func (f *fakeScribe) May(_ context.Context, _ uuid.UUID, actor tenant.Actor) bool {
	f.actor = actor
	return f.may
}

// gate mirrors the production writer's FIRST act. Every exported write on
// tenant.RuleWriter calls authorise before it opens a transaction and answers
// ErrNotPermitted; this double did not, so an actor it reported as refused could
// still reach every act.
//
// 🔴 THE DIVERGENCE MADE A REAL PROTECTION DELETABLE. policyChangeApply deliberately
// does NOT re-ask May — it delegates to the domain gate — and maps ErrNotPermitted
// onto a recorded refusal. With a double that ignored `may`, that arm was dead code:
// an audit deleted the refusal recording at the apply step and internal/handler stayed
// green. This is the SECOND double in this task with the same illness (storedAuthority
// was the first), which is why the fix is to mirror the contract rather than to add
// one more assertion somewhere.
func (f *fakeScribe) gate() error {
	if !f.may {
		return tenant.ErrNotPermitted
	}
	return f.err
}

func (f *fakeScribe) SetEnabled(_ context.Context, c tenant.EnableCommand) (tenant.SwitchResult, error) {
	f.actor = c.Actor
	if err := f.gate(); err != nil {
		return tenant.SwitchResult{}, err
	}
	f.calls = append(f.calls, "setEnabled")
	f.enable = c
	return tenant.SwitchResult{PolicyID: c.PolicyID, Now: c.Enabled}, nil
}

func (f *fakeScribe) Bind(_ context.Context, c tenant.BindCommand) error {
	f.actor = c.Actor
	if err := f.gate(); err != nil {
		return err
	}
	f.calls = append(f.calls, "bind")
	f.bind = c
	return nil
}

func (f *fakeScribe) Unbind(_ context.Context, c tenant.BindCommand) error {
	f.actor = c.Actor
	if err := f.gate(); err != nil {
		return err
	}
	f.calls = append(f.calls, "unbind")
	f.bind = c
	return nil
}

func (f *fakeScribe) Author(_ context.Context, c tenant.AuthorCommand) (tenant.AuthoredRule, error) {
	f.actor = c.Actor
	if err := f.gate(); err != nil {
		return tenant.AuthoredRule{}, err
	}
	f.calls = append(f.calls, "author")
	f.author = c
	return tenant.AuthoredRule{PolicyID: c.PolicyID, Name: c.Name, VersionNo: 1}, nil
}

func (f *fakeScribe) RecordRefusal(_ context.Context, _ uuid.UUID, actor tenant.Actor, act, target string) error {
	f.refusals = append(f.refusals, act+"/"+target)
	f.actor = actor
	return nil
}

// policyScreenFixture is a realistic screen.
//
// 🔴 THE GUARDRAIL ROWS ARE BUILT FROM policy.Guardrails(), NOT TYPED OUT. A fixture
// carrying its own list of ten sids would be a second representation of a security
// boundary that has moved once already (ADR 0007), and the tests below assert the
// PAGE names them -- so a hand-written fixture would make those assertions pass over
// rules the engine no longer has.
func policyScreenFixture() tenant.PolicyScreen {
	s := tenant.PolicyScreen{
		Queried:         true,
		ShippedBaseline: policy.BaselineVersion,
		Zone:            time.UTC,
	}
	for i, g := range policy.Guardrails(policy.DefaultParams()) {
		row := tenant.Guardrail{
			Order: i + 1, Sid: g.Sid, Effect: string(g.Effect), Reason: g.Reason,
			Rule: "why " + g.Sid + " cannot be switched off",
		}
		if g.Sid == "sys:person-debounce" {
			row.Bounded = &tenant.BoundedParam{
				Label: "Repeat window for one person", Current: "60 s", Min: "30 s", Max: "300 s",
			}
		}
		s.Guardrails = append(s.Guardrails, row)
	}
	for _, doc := range policy.Baseline() {
		rule := tenant.Rule{
			Name: doc.Name, Managed: true, State: tenant.StateActive, VersionNo: 1,
			StoredStamp: policy.BaselineVersion, ShippedStamp: policy.BaselineVersion,
			Attachments: []string{"*"},
			History: []tenant.RuleVersion{{
				No: 1, At: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
				Author: tenant.AuthorSystem, Bytes: 320, Current: true,
			}},
			HistoryTotal: 1,
		}
		for _, st := range doc.Document.Statements {
			s := tenant.RuleStatement{
				Sid: st.Sid, Effect: string(st.Effect), Resources: st.Resource, Reason: st.Reason,
			}
			for _, a := range st.Action {
				s.Actions = append(s.Actions, string(a))
			}
			rule.Statements = append(rule.Statements, s)
		}
		s.Baseline = append(s.Baseline, rule)
	}
	for _, a := range policy.Actions() {
		effect := string(policy.Evaluate(policy.Set{}, policy.Context{Action: a}).Effect)
		s.Authorities = append(s.Authorities, tenant.Authority{
			Action: string(a), Default: effect, FailClosed: effect == string(policy.EffectDeny),
		})
	}
	return s
}

// policyBrowser is the panel browser wired to a rulebook the caller controls.
func policyBrowser(t *testing.T, rules panelRules) *browser {
	return policyBrowserWith(t, rules, newFakeScribe())
}

// policyBrowserWith is the same, with the WRITE side under the test's control too.
func policyBrowserWith(t *testing.T, rules panelRules, scribe panelScribe) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: policyTestAdminRole, FullName: policyTestAdminName,
		}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, rules, scribe,
		newFakeBooks(), newFakeTexts(), adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

func policyPage(t *testing.T, rules panelRules) string {
	t.Helper()
	rec := policyBrowser(t, rules).do(http.MethodGet, policiesHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", policiesHref, rec.Code)
	}
	return htmlOf(t, rec)
}

// policyStatePage renders ONE page state: the screen, the engine's answer about
// editing, and the refusal word the write path would have redirected with.
func policyStatePage(t *testing.T, st policyPageState) string {
	t.Helper()
	href := policiesHref
	if st.problem != "" {
		href += "?problem=" + st.problem
	}
	b := policyBrowserWith(t, &fakeRules{screen: st.screen}, &fakeScribe{may: st.mayEdit})
	rec := b.do(http.MethodGet, href, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", href, rec.Code)
	}
	return htmlOf(t, rec)
}

// TestPoliciesSection_TakesTheTenantFromTheSessionAndNothingElse (§4.5). The
// section accepts no parameter at all, so there is nothing a caller could bend --
// but the tenant it asks for still has to be the resolved one.
func TestPoliciesSection_TakesTheTenantFromTheSessionAndNothingElse(t *testing.T) {
	rules := newFakeRules()
	b := policyBrowser(t, rules)
	// A query string the section does not read, to show it changes nothing.
	rec := b.do(http.MethodGet, policiesHref+"?tenant="+uuid.New().String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d, want 200", rec.Code)
	}
	if rules.calls != 1 {
		t.Fatalf("the rulebook was read %d time(s), want 1", rules.calls)
	}
	if rules.tenant != panelTestTenant {
		t.Errorf("the section asked for tenant %s; the session resolved %s. The tenant is "+
			"never an input (§4.5).", rules.tenant, panelTestTenant)
	}
}

// TestPoliciesSection_SaysTheReadFailedRatherThanShowingAnEmptyRulebook (§4.6).
// "You have no policies" is a claim about a business and a timeout is not evidence
// for it -- and it is the WORST empty state in the panel, because a customer who
// believes it would go and set rules up that already exist.
func TestPoliciesSection_SaysTheReadFailedRatherThanShowingAnEmptyRulebook(t *testing.T) {
	rules := &fakeRules{err: errors.New("boom")}
	rec := policyBrowser(t, rules).do(http.MethodGet, policiesHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET with a failing read: %d, want 500", rec.Code)
	}
	body := htmlOf(t, rec)
	for _, forbidden := range []string{"Tappa guarantees", "Who may do what"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("a failed read rendered the section heading %q; it must render a problem "+
				"page instead", forbidden)
		}
	}
}

// 🔴 WHAT THIS NET HOLDS AND WHAT IT DOES NOT — the honest inventory. A file that
// overstates its own guarantee is how the NEXT control gets put where nobody is
// looking, and this one has been rewritten after every audit that beat it.
//
// ⚠️ NO COUNT OF THOSE DEFEATS APPEARS HERE ANY MORE. Four different figures were
// carried in this file at once ("six audits", "twelve times", "ten times", "three
// times") plus a "beaten repeatedly" that then listed four — every one of them a
// hand-kept number in a file whose subject is claims that stop being true. What each
// escape WAS is written where it is closed; that is the part a reader can act on.
//
// 🔴 PHASE B SPLIT THE SUBJECT IN TWO AND DID NOT WEAKEN EITHER HALF. Phase A could
// say "nothing in this section is interactive" because nothing wrote; phase B adds
// forms, so that sentence had to become two:
//
//	the GUARANTEE BLOCK (<div class="policy-guarantees">) keeps the ABSOLUTE claim,
//	  with the same allow-list and the same scanner phase A used. Not a disabled
//	  control, not a link, not an attribute — nothing. That is the card's first
//	  trap, and it is the one place the answer is still "zero".
//	the REST OF THE SECTION keeps an ALLOW-LIST TOO — the same scanner, widened by
//	  exactly the nine element names and eight attributes the write side renders —
//	  PLUS three derived checks: every form's action and every link's href must be a
//	  route the real router serves (chi.Walk), every rendered field name must be one
//	  the handler actually READS (go/ast over policyactions.go), and no control
//	  anywhere may name a `sys:` sid.
//
// 🔴 THE SECOND HALF USED TO BE THE DESTINATION CHECK ALONE AND AN AUDIT BEAT IT
// TWICE IN ONE ROUND (2026-08-12). Both escapes had ONE root: the net asked "does
// this control point at something the server honours" and had no answer for a
// control that pointed at NOTHING.
//
//	<div class="btn min-h-11" onclick="turnOff()" role="switch" contenteditable>
//	  Turn this guarantee off</div>          — no action, no href, in every docket
//	<form action="/admin/policies/change">
//	  <input name="gps_radius_m" min=25 max=1000>   — real route, ignored field
//
// The first is closed by putting the allow-list back over the whole section (an
// event handler, an ARIA role or a contenteditable is a finding wherever it is); the
// second by DERIVING both sides of the field-name question. Neither is a patch on a
// deny-list: both are the same two mechanisms this file already uses, applied to the
// region that had lost them.
//
// IT HOLDS (each measured by a mutation that goes red):
//   - ANY non-inert element or attribute inside the GUARANTEE block, including
//     inside its conditional arm (the tunable window).
//
//     🔴 THE BLOCK IS THE COMPONENT'S OWN RENDER, AND EVERY EARLIER SCHEME WAS
//     BEATEN. It is not a slice of the page: pages.PolicyGuarantees is rendered
//     alone and the bytes it produces — wrapper tags included — are the region, which
//     the page must contain exactly once. Each earlier scheme was beaten in a way the
//     next one had to answer: markup above the wrapper's opening tag; a FILLED DECOY
//     wrapper; `<!-- </div></div> -->` and then `@templ.Raw("<!x </div> >")`, both of
//     which made a tag counter disagree with a browser; and finally markup placed
//     BESIDE the call while the wrapper still lived in AdminPolicies. The counter is
//     gone (so no spelling can move a boundary) and the wrapper is back inside the
//     component (so nothing can sit between its tags but the component);
//   - ANY element or attribute in the section outside its own allow-list — the
//     auditor's contenteditable/onclick/role="switch" div included;
//   - a form with NO action at all (it posts to the page it is on — a shape this
//     section really shipped for a round);
//   - a control anywhere in the section whose action or href is not a route the
//     router mounts;
//   - a rendered field name the handler never reads;
//   - a control anywhere in the section that names a sys: sid;
//   - anything AdminPolicies renders outside its own wrapper -- the page must be
//     the panel shell byte for byte with this section's wrapper in the middle, so
//     even a control built from the shell's own form/button vocabulary fails;
//   - a new `if` with no fixture behind it;
//   - an `else` arm;
//   - markup written straight into the generated _templ.go;
//   - three self-checks: a dead entry in the element allow-list, a witness no
//     fixture renders, a witness every fixture renders.
//
// IT DOES NOT HOLD:
//   - a `switch`/`case` arm, or markup emitted from a plain Go function through
//     templ.Raw: the branch derivation reads lines and sees neither. `switch` is
//     used in three shipped templates, so this is a realistic gap, not a theoretical
//     one (policyTemplBranches);
//   - a BORROWED witness: the discrimination check proves a string separates
//     fixtures, not that the branch under test produced it (policyBranchWitness);
//   - a string added above the wrapper that happens to be a PREFIX of the shell's
//     own closing bytes (`</main></body></html>`): the comparison shifts and still
//     matches. Only closing tags can be built that way, so it carries no control --
//     it silently breaks the document structure instead;
//   - a control put in the panel SHELL rather than in this section. This net's
//     subject is AdminPolicies; the shell is other sections' ground too, and what
//     guards it is their own route and press-target nets;
//   - 🔴 A CONTROL BUILT LIKE THE SHIPPED ONE, ANYWHERE OUTSIDE THE GUARANTEE
//     WRAPPER. This is the widest counted limit in this file and it replaces a
//     narrower sentence that said "specifically an anchor with no fields" — an audit
//     refuted that by putting a WORKING off switch into the intro's `if v.MayEdit`
//     arm:
//
//	<form method="post" action="/admin/policies/change">
//	  <input type="hidden" name="op" value="disable">
//	  <input type="hidden" name="policy" value="<a real KF baseline uuid>">
//	  <button class="btn btn--quiet min-h-11" type="submit">Switch this Tappa
//	    guarantee off</button></form>
//
//     Every check passes because every check SHOULD pass: allowed elements, allowed
//     attributes, a route the router mounts, field names the handler reads, no sys:
//     sid, a touch-target class, and no new conditional arm — it is byte-for-byte the
//     shape of the switch this section legitimately ships on every rule.
//
//     🔴 IT IS COUNTED RATHER THAN CLOSED, AND THE REASON IS THAT IT CANNOT BE CLOSED
//     SYNTACTICALLY. Telling the two apart needs to know which PARAGRAPH a form is
//     allowed to live in, which is semantics; every syntactic rule either rejects both
//     (the product breaks) or accepts both. Every syntactic patch tried so far has
//     been beaten by the next audit, and one aimed at this shape would land here.
//
//     ⚠️ WHAT ACTUALLY MAKES A GUARANTEE UNSWITCHABLE IS NOT THIS NET. It is the
//     schema and the engine, and both were measured: `policies.layer` admits only
//     'baseline' and 'tenant', so `guardrail` cannot be spelled into a row even by a
//     superuser; and policy.Evaluate returns deny (sys:policy-edit-owner-only) for a
//     non-owner against a stored document that allows everything. This net is a
//     REGRESSION BRAKE on what the screen says, not the wall.
//   - 🔴 A BUTTON-LOOKING ELEMENT INSIDE THE ABSOLUTE REGION THAT USES NO CLASS FROM
//     THE DERIVED PRESS-TARGET VOCABULARY. Measured:
//
//	<span class="inline-block rounded bg-tappa-green px-4 py-3 font-display
//	  text-paper">Turn this guarantee off</span>
//
//     The class check is derived from panelTouchTargets, so it cannot rot — but its
//     REACH is that vocabulary. Utilities that happen to look like a button are
//     outside it, and bounding "looks pressable" by utility classes would be a
//     deny-list over Tailwind.
//
//     ⚠️ THIS ONE WAS MEASURED THROUGH THE HANDLER CHAIN (a rendered page from the
//     real router with a real session), NOT against a live server on a port. The
//     difference matters for what the measurement can claim: it shows the SERVER
//     RENDERS such an element without complaint, and it does not show what a browser
//     paints. Nothing was measured about the latter.
//   - 🔴 A LINK THAT CARRIES NO FIELDS. Measured by an audit on 2026-08-12, twice:
//
//	<a class="btn min-h-11" href="/admin/policies">Turn this guarantee off</a>
//	<a class="btn min-h-11" href="/admin/reports.csv">Download everything</a>
//
//     Both are outside the guarantee block, both point at routes this ROUTER really
//     serves, and neither posts a field — so the destination check approves them and
//     the name derivation has nothing to read. The first is a DEAD control (a link
//     cannot switch anything off; it navigates), which is the card's first trap
//     without the write. The second reaches ANOTHER section's route, and that route
//     is a GET that writes one audit_log row.
//
//     WHY IT IS COUNTED RATHER THAN CLOSED. The obvious fix is "a control in this
//     section may only point at this section's own routes", and that is a fourth
//     patch on a net that keeps being beaten — it
//     would have to carve out the shell's own links, the section table's tabs and
//     every legitimate cross-section link the panel already renders, and each
//     carve-out is where the next escape goes. What DOES catch the dangerous half is
//     already here: the same link with a FIELD on it (a form to another section's
//     write route) goes red on the name derivation, measured. So the gap is
//     specifically "an anchor with no fields", and it is written down rather than
//     half-closed.
//   - a field name built from a Go EXPRESSION rather than a literal
//     (`name={ v.ConfirmField }`, which the confirmation page uses): the template
//     side of the field derivation reads literals, so such a name is neither checked
//     nor claimed. The same is true in the other direction for a handler that read a
//     field name from a variable;
//   - the CONFIRMATION and VERSION pages (policychange.templ). This net's subject is
//     AdminPolicies, so their MARKUP is outside it.
//
//     ⚠️ THIS ENTRY USED TO ADD "held by their own route tests and by the shell's
//     press-target net", AND THAT WAS FALSE — measured, by stripping the touch-target
//     class from all three of their controls and watching the package stay green. The
//     press-target corpus is built from pages.PanelSections' hrefs, and neither page
//     is a section: one is behind a POST, the other behind a sub-route. They are in
//     that corpus now (panelNonSectionScreens), so the 44px rule really does reach
//     them; what is still true is that no allow-list, route derivation or field
//     derivation scans their markup;
//   - an allow-listed attribute carrying a meaningless value — `type="wingding"` is
//     not a finding. The list bounds which attributes may appear, not what they say.
//
// The gaps above are counted rather than patched: this net has been patched and beaten
// more than once, and a patch that half works is worse than a limit somebody can read.
//
// ⚠️ THIS SENTENCE SAID "BOTH GAPS" WHILE THE LIST ABOVE IT HAD GROWN TO FIVE, which
// is the same hand-kept-count defect the list itself warns about -- so it counts
// nothing now.

// TestPoliciesSection_RendersOnlyInertMarkup -- the M6-09 card's first trap,
// measured on the RENDERED page.
//
// 🔴 THE CARD SAYS THE OFF CONTROL IS ABSENT, NOT DISABLED, and this is the wall a
// type check cannot build: a template can always hard-code markup no view field
// feeds. Phase A writes nothing at all, so the honest assertion is the strong one --
// nothing in this section is interactive, by element OR by attribute.
//
// 🔴 IT DRIVES EVERY RENDERING BRANCH, AND THE FIRST VERSION DROVE ONE. It rendered
// the healthy fixture only, so `policyDegraded` and the `Capped` notice -- both
// behind an `if` -- were never in the scanned body at all. An auditor put a working
// control inside the degraded notice and the whole suite stayed green. Every
// conditional block on this section is now rendered by one of policyScreenStates,
// and the anchor is the section WRAPPER rather than a heading in the middle of it.
func TestPoliciesSection_RendersOnlyInertMarkup(t *testing.T) {
	shell := policyShellBaseline(t)
	for name, st := range policyPageStates(t) {
		screen := st.screen
		t.Run(name, func(t *testing.T) {
			body := policyStatePage(t, st)
			// 🔴 BOTH SIDES OF THE BOUNDARY ARE CHECKED, AND THE OUTSIDE IS CHECKED BY
			// DERIVATION. Three rounds of moving the boundary were each answered by
			// putting the control on the other side of it, and the fourth answer -- a
			// measured token list -- was answered by building the control out of the
			// shell's OWN vocabulary. So the outside is no longer described: it is the
			// panel shell rendered with an empty body, byte for byte.
			section := assertNothingOutsideTheSection(t, body, shell)
			// 🔴 AND NOTHING MAY SIT BETWEEN THE INTRO AND THE WRAPPER. The wrapper is inside
			// PolicyGuarantees now, so markup added anywhere in that component is in the
			// scanned bytes; this closes the other half — markup added in AdminPolicies
			// itself, one line above the call, which renders directly under the "Tappa
			// guarantees" heading and reads as part of the block. The assertion is byte
			// adjacency, the technique that closed the outside-the-section channel in
			// phase A.
			if screen.Queried {
				const wrapper = `<div class="policy-guarantees">`
				at := strings.Index(section, wrapper)
				if at < 0 {
					t.Fatalf("the section carries no %s", wrapper)
				}
				if before := strings.TrimSpace(section[:at]); !strings.HasSuffix(before, "</section>") {
					tail := before
					if len(tail) > 200 {
						tail = "…" + tail[len(tail)-200:]
					}
					t.Errorf("something is rendered between the intro and the guarantees wrapper.\n"+
						"Whatever it is renders directly under the “Tappa guarantees” heading and "+
						"reads as part of the block, while sitting outside the region that is "+
						"scanned absolutely — which is exactly where an auditor put a working off "+
						"switch.\n  bytes before the wrapper end with: %q", tail)
				}
			}
			// 🔴 THE ABSOLUTE REGION IS THE COMPONENT'S OWN BYTES, NOT A SLICE OF THE PAGE.
			// See policyGuaranteeBytes for the escapes that killed the slicing version.
			// The page must CONTAIN those bytes exactly once — which is both the proof that
			// what was scanned is what shipped, and the check that no second copy is hiding.
			if screen.Queried {
				want := policyGuaranteeBytes(t, screen, st.mayEdit)
				if n := strings.Count(section, want); n != 1 {
					t.Fatalf("the guarantee component's own bytes appear %d times in the section, "+
						"want exactly 1. The region is defined as what that component renders, so a "+
						"page that does not contain it verbatim is a page this scan cannot speak for.", n)
				}
				if bad := pressTargetClassesIn(want); len(bad) > 0 {
					t.Errorf("the guarantee component renders the press-target class(es) %v. "+
						"Nothing in this block is pressable, so anything DRESSED as pressable is the "+
						"card's first trap with the styling done for it.", bad)
				}
				if bad := interactiveMarkup(want); len(bad) > 0 {
					t.Errorf("the guarantee component renders %v.\n"+
						"These are the bytes the component itself produces, so this covers markup "+
						"written through templ.Raw as well — which is how an auditor last got a "+
						"working off switch in here, past a scan that counted <div> tags. What IS "+
						"structural is the schema and the engine: `guardrail` cannot be spelled into "+
						"policies.layer even by a superuser, and the guardrail denies regardless of "+
						"any stored document.", bad)
				}
			}
			// AND THE PAGE'S OWN STRUCTURE AROUND IT: exactly one wrapper, nothing between
			// it and the intro. Those are about where the component is MOUNTED, which its
			// own bytes cannot speak to.
			if _, ok := policyGuaranteeBlock(t, section); ok {
				// THE WRAPPER IS STILL CHECKED FOR SHAPE — exactly one anchor, and the byte
				// adjacency above — but nothing is SCANNED out of it any more. What is scanned
				// is the component's own render, above.
			} else if screen.Queried {
				t.Fatal("the section carries no policy-guarantees block, so the absolute half of " +
					"this net scanned nothing")
			}
			// AND THE REST OF THE SECTION KEEPS AN ALLOW-LIST TOO, widened by exactly what
			// the write side renders. This is what an auditor's second escape needed: an
			// element with NO action and NO href (a contenteditable div carrying onclick and
			// role="switch"), which the destination check below has nothing to look at.
			if bad := markupOutsideTheAllowList(section, policySectionAllowedTags,
				policySectionAllowedAttrs); len(bad) > 0 {
				t.Errorf("the policies section renders %v. Phase B added forms, so the list is "+
					"wider than phase A's -- but nothing on it can make an element BEHAVE: an "+
					"event handler, an ARIA role, a contenteditable or an hx-* attribute is how "+
					"a customer comes to press something the server never hears about.", bad)
			}
			// AND EVERY DESTINATION MUST BE A ROUTE THE SERVER REALLY MOUNTS, and nothing
			// may aim at a sys: sid.
			for _, bad := range undeclaredDestinations(t, section) {
				t.Errorf("the policies section renders %s. A control has to point at an address "+
					"this router serves; one that does not is either a dead button or a "+
					"guardrail-shaped one somebody hoped nobody would press.", bad)
			}
			// ANTI-VACUITY: the slice must hold the whole section, not a tail of it.
			// The `unqueried` fixture renders no body ON PURPOSE (it is what gives the
			// witness check something to be absent from), so the headings are asserted
			// only where a body exists -- and the branch derivation is what guarantees
			// every OTHER fixture reaches the blocks these headings live in.
			if screen.Queried {
				for _, heading := range []string{"Tappa guarantees", "Tappa baseline", "Who may do what"} {
					if !strings.Contains(section, heading) {
						t.Fatalf("the scanned slice does not contain %q, so the check above ran over "+
							"less than the section", heading)
					}
				}
			}
			// AND THE SHELL'S OWN CONTROLS MUST STILL BE THERE, or the comparison above
			// is a comparison of nothing. policyShellBaseline asserts it too; this says it
			// where the scan happens.
			if !strings.Contains(body, "<button") {
				t.Fatal("the page renders no button anywhere; the shell carries a sign-out " +
					"form, so this test is looking at the wrong document")
			}
		})
	}
}

// templBranch is one conditional arm declared in policies.templ.
type templBranch struct {
	// key is "<condition>@<ordinal>". It survives LINE MOVES, which is what it was
	// chosen for -- but not an INSERTION: adding another arm with the same condition
	// text ahead of an existing one renumbers every later one, and the witness map
	// then goes red for keys that did not really change. That is loud rather than
	// silent (the map is checked both ways), so it costs a puzzled author rather than
	// a hole. Written down because "stable" on its own was too strong a word.
	key  string
	line int
}

// policyTemplBranches derives the conditional arms this template's DIALECT declares.
//
// ⚠️ IT DOES NOT DERIVE EVERY ARM, AND THE HEADING USED TO SAY IT DID (it says what
// it does now). Measured by an auditor: this reads LINES, so it sees `if` /
// `} else if` / `} else` and it does NOT see a `switch`/`case`, nor markup emitted
// from a plain Go function through templ.Raw. Both were tried and both got through
// with the branch count unchanged. So this is a net over the dialect THIS template
// is written in, not over the language.
//
// 🔴 HOW BIG THAT GAP IS, IS COUNTED AT RUN TIME AND NOT WRITTEN DOWN. This comment
// said "three shipped templates already use one" and the real figure was three
// TIMES that -- a hand-counted N, which is the exact class this task has now paid
// for repeatedly. TestPolicyBranchDerivation_ReportsTheSizeOfItsOwnBlindSpot walks
// every .templ in web/templates with the SAME inTempl logic and logs the number,
// so it cannot rot; one of the switches it finds renders a <form>, so these are
// markup-producing arms rather than decoration.
//
// 🔴 THE LIST IS PARSED, NOT WRITTEN DOWN, AND THAT IS THE WHOLE POINT. The
// inert-markup net was beaten more than once, and the last two had one root cause: the
// net drove a hand-picked set of fixtures, so a block behind an `if` nobody had
// thought about was never in the scanned body at all. An auditor put a working
// control into `if a.Guardrail != ""` -- a block that renders on EVERY production
// page, twice -- and eighteen policy tests stayed green. A hand-written list of
// "branches we remember to drive" is the N-list class this repository has been bitten
// by repeatedly and has more than once closed by DERIVING instead (the action vocabulary
// with go/ast, the routes with chi.Walk). This is the third derivation.
//
// It reads the .templ rather than the generated Go because the .templ is what an
// author edits: a branch added there is in this list on the next run, whether or not
// anybody remembers this test exists.
func policyTemplBranches(t *testing.T) []templBranch {
	t.Helper()
	raw, err := os.ReadFile("../../web/templates/pages/policies.templ")
	if err != nil {
		t.Fatalf("reading policies.templ: %v", err)
	}
	inTempl := false
	seen := map[string]int{}
	var out []templBranch
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "templ "):
			inTempl = true
			continue
		case line == "}":
			// Column-0 close: end of a templ block or of a plain Go function. Either
			// way, conditionals after it are not this template's until the next
			// `templ`.
			inTempl = false
			continue
		}
		if !inTempl || strings.HasPrefix(trimmed, "//") {
			continue
		}
		var cond string
		switch {
		case strings.HasPrefix(trimmed, "if ") && strings.HasSuffix(trimmed, "{"):
			cond = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "if "), "{"))
		case strings.HasPrefix(trimmed, "} else if ") && strings.HasSuffix(trimmed, "{"):
			cond = "else if " + strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "} else if "), "{"))
		case trimmed == "} else {":
			cond = "else"
		default:
			continue
		}
		seen[cond]++
		out = append(out, templBranch{key: fmt.Sprintf("%s@%d", cond, seen[cond]), line: i + 1})
	}
	// ANTI-VACUITY: a parser that found nothing would agree with any fixture set.
	if len(out) < 15 {
		t.Fatalf("parsed %d conditional arm(s) from policies.templ; it has more, so this "+
			"derivation is reading the wrong file or the wrong shape", len(out))
	}
	return out
}

// policyBranchWitness maps each derived branch to a string a fixture renders when
// that branch is taken.
//
// ⚠️ IT IS NOT PROOF THAT THE BRANCH PRODUCED IT, AND THIS PARAGRAPH USED TO CLAIM
// IT WAS ("a string that appears in the rendered page ONLY when that branch is
// taken"). Measured: an auditor added a new block, gave it a witness that some OTHER
// block already renders, and the check passed -- because the discrimination test
// asks whether the witness separates FIXTURES, not whether the branch emitted it. A
// borrowed witness gets through. Closing that needs the branch itself to be
// identified in the output, which is a real templ parser away.
//
// 🔴 THE KEYS ARE DERIVED AND THE VALUES ARE NOT, WHICH IS THE ONLY HAND-WRITTEN
// PART LEFT -- and it is held to the derivation in BOTH directions: a branch with no
// entry is RED (so tomorrow's `if` cannot be fixture-less), and an entry for a branch
// that no longer exists is RED (so the map cannot rot). A witness must also
// DISCRIMINATE: present in at least one fixture's render and absent from at least
// one, or it is evidence of nothing.
//
// Several branches render nothing but an interpolation, so their witness is a
// sentinel the all-branches fixture puts in the data. That is deliberate: the
// realism of the page is measured elsewhere (the DB tests and the live page); what
// these fixtures are for is REACH.
var policyBranchWitness = map[string]string{
	"v.VersionsCapped@1":       "Some version history is not on this page",
	"v.AttachmentsCapped@1":    "Some scope bindings are not on this page",
	"!v.VersionsCapped@1":      "Everything else on this page is complete",
	"else@5":                   "Your version history is shortened too",
	"v.Queried@1":              "How your rules are decided",
	"v.GuardrailsOnly@1":       "Your rules are not being applied right now",
	`g.Tunable != ""@1`:        "may be set between",
	"len(v.Tenant) == 0@1":     "You have not written any policies of your own.",
	"v.BaselineInForce@1":      "the intended starting point rather than a gap",
	"else@1":                   "running on the Tappa guarantees alone",
	"else@2":                   "zz-branch-tenant-rule",
	"r.Managed@1":              "Managed by Tappa",
	"else@3":                   "Written by you",
	`r.Version != ""@1`:        "version 4241",
	`r.Stamp != ""@1`:          "zz-branch-stamp",
	`r.Outdated != ""@1`:       "zz-branch-shipped",
	`r.Problem != ""@1`:        "This rule is not being applied",
	`s.Reason != ""@1`:         "zz-branch-reason",
	"len(s.Conditions) > 0@1":  "Only when",
	"len(r.Attachments) > 0@1": "Bound to",
	"len(r.History) > 0@1":     "History",
	`h.Marker != ""@1`:         ">in use<",
	`r.HistoryNote != ""@1`:    "newest of 9 versions",
	"v.GuardrailsOnly@2":       "None of these grants is being applied today",
	"len(a.Grants) > 0@1":      "Granted to",
	"else@4":                   "so the engine would refuse it",
	`a.Guardrail != ""@1`:      "zz-branch-authz-guardrail",
	"len(v.Recording) > 0@1":   "The one that works the other way",
	`a.Guardrail != ""@2`:      "zz-branch-recording-guardrail",

	// --- M6-09 phase B: the write side ------------------------------------------
	"v.ScopeCapped@1":           "Some of your venues are not in the scope picker",
	`v.Problem != ""@1`:         "has been written to your audit trail",
	"v.MayEdit@1":               "recorded with your name against it",
	"else@6":                    "does not currently grant you the",
	"len(v.Settings) > 0@1":     "Settings that are not rules",
	`h.Href != ""@1`:            "Read it in full",
	`r.Switch.Label != ""@1`:    "Switch this rule off",
	"len(r.Unbind) > 0@1":       "Stop recording it at",
	"len(r.Bindable) > 0@1":     "Record the binding",
	"len(v.Authorable) > 0@1":   "Show me what this would do",
	"len(v.Supersedable) > 0@1": "A new version of",
}

// TestPolicyFixtures_DriveEveryConditionalBranchOfTheTemplate is the derivation's
// teeth: every arm policies.templ declares is rendered by at least one fixture the
// inert-markup net drives, and no arm is claimed that does not exist.
func TestPolicyFixtures_DriveEveryConditionalBranchOfTheTemplate(t *testing.T) {
	branches := policyTemplBranches(t)
	rendered := map[string]string{}
	for name, st := range policyPageStates(t) {
		section, _ := policySectionSlice(t, policyStatePage(t, st))
		rendered[name] = section
	}
	if len(rendered) < 4 {
		t.Fatalf("only %d fixture(s); the branch set needs more reach than that", len(rendered))
	}

	declared := map[string]bool{}
	for _, b := range branches {
		declared[b.key] = true
		witness, ok := policyBranchWitness[b.key]
		if !ok {
			t.Errorf("policies.templ:%d declares the branch %q and no fixture is known to "+
				"drive it. Add a witness AND a fixture that renders it -- a block no fixture "+
				"reaches is a block the inert-markup net cannot see, which is how a working "+
				"control survived eighteen green tests.", b.line, b.key)
			continue
		}
		hit, miss := "", ""
		for name, body := range rendered {
			if strings.Contains(body, witness) {
				hit = name
			} else {
				miss = name
			}
		}
		if hit == "" {
			t.Errorf("policies.templ:%d (%s): no fixture renders %q, so that branch is never "+
				"scanned", b.line, b.key, witness)
		}
		if miss == "" {
			t.Errorf("policies.templ:%d (%s): EVERY fixture renders %q, so it is not evidence "+
				"the branch was taken -- pick a witness only that arm produces", b.line, b.key, witness)
		}
	}
	for key := range policyBranchWitness {
		if !declared[key] {
			t.Errorf("policyBranchWitness names %q, which policies.templ no longer declares; "+
				"remove it, or the map rots into a description of a template that is gone", key)
		}
	}
	t.Logf("conditional arms derived from policies.templ: %d, fixtures: %d", len(branches), len(rendered))
}

// policySectionAllowedTags is the ALLOW-LIST of elements the policies section may
// render, and policySectionAllowedAttrs the attributes they may carry.
//
// 🔴 IT IS AN ALLOW-LIST BECAUSE THE DENY-LIST WAS BEATEN. The first version of the
// check below looked for `<form`, `<input`, `<button`, `<select` and `<textarea`,
// and an auditor put a working control into the GUARDRAIL block with neither:
//
//	<a href="#" hx-post="…/off" class="btn min-h-11">Turn this guarantee off</a>
//	<details><summary>Advanced</summary><p contenteditable="true">…</p></details>
//
// Both rendered, `go test ./internal/handler` was ok and redline-check exited 0. The
// most instructive part is what caught the FIRST attempt: not this test but
// TestPanelScreens_EveryPressTargetCarriesATouchTargetClass, i.e. the BRAND's touch
// target net — and adding a touch-target class silenced that too. So the card's red
// line was being held by an accessibility rule, by accident.
//
// A deny-list has to predict the next element; an allow-list does not, and this
// repository has watched escape-nets get patched and beaten more than once and beaten a fourth.
// The false-alarm cost was MEASURED before choosing it, on the real rendered
// section: nine element names and exactly ONE attribute (class). Widening it is
// therefore a visible, arguable edit rather than a guess about what is dangerous.
// ⚠️ EVERY ENTRY IS ONE THE SECTION ACTUALLY RENDERS, AND THAT IS ASSERTED. The
// first version listed thirteen while the page used nine -- four names admitted for
// nothing, which is the same shape as an exemption protecting nothing: it widens the
// allow-list without anybody arguing for it, and the next author adds a fifth.
// TestPolicySectionAllowList_HasNoDeadEntry is the brake, and it is deliberately the
// same brake policyEvaluateAllowed has.
// 🔴 THERE ARE TWO LISTS NOW, AND THAT IS THE ANSWER TO TWO AUDIT ESCAPES RATHER
// THAN A WIDENING (2026-08-12, phase B round 2). The first phase-B version applied the
// allow-list to the GUARANTEE BLOCK only and left the rest of the section to a
// destination check -- so an auditor put
//
//	<div class="btn min-h-11" onclick="turnOff()" role="switch" contenteditable>
//	  Turn this guarantee off</div>
//
// inside every rule's docket, WITH NO action AND NO href, and every policy test
// stayed green: the destination net had nothing to check. The lists below restore the
// allow-list over the WHOLE section, widened by exactly what the shipped page renders
// and by nothing else -- so `onclick`, `role`, `contenteditable`, `aria-*`, `hx-*`
// and `tabindex` are all findings again, everywhere, and widening the set is a
// visible edit somebody has to argue for.
//
// THE GUARANTEE LIST IS THE SMALLER ONE, and it is the phase-A claim unchanged: no
// form, no input, no button, no link, no attribute but class. It also fixes a real
// defect in its own brake -- when one list covered a region that no longer rendered
// dl/dt/dd/strong, those four became dead entries "protecting" a region nothing
// scanned. Each list is now held to the region it is APPLIED to.
var policyGuaranteeAllowedTags = map[string]bool{
	"section": true, "div": true, "p": true, "span": true, "h2": true,
}

var policyGuaranteeAllowedAttrs = map[string]bool{"class": true}

// policyGuaranteeForbiddenClasses is the VALUE half of the sentence above, and it
// closes an escape the attribute half could not see.
//
// 🔴 `class` IS THE ONLY ATTRIBUTE THIS REGION MAY CARRY, WHICH MEANT ITS VALUE WAS
// UNCHECKED — and in this repository the thing that turns an element into a button is
// exactly a class. An auditor put
//
//	<span class="btn btn--quiet min-h-11">Turn this guarantee off</span>
//
// inside every guardrail row and the scan reported nothing: `span` is allowed, `class`
// is allowed, and nothing looked at what the class SAID. The result is the card's
// first trap rendered in the one region whose answer is claimed to be zero — a
// customer sees a button, presses it, and nothing happens.
//
// THE VOCABULARY IS DERIVED FROM THE TOUCH-TARGET MAP, which dashboard_test.go
// already keeps in step with the compiled stylesheet in both directions (a class in
// that map must exist in app.css with its measured min-height). So this cannot rot
// into a list of class names somebody remembered, and a new press-target class is
// forbidden here on the day it is added rather than on the day somebody notices.
// `min-h-11` is added beside them because it is the 44px reservation itself: an
// element carrying it is dressed as something to press whatever else it says.
func policyGuaranteeForbiddenClasses() map[string]bool {
	out := map[string]bool{"min-h-11": true, "min-h-16": true}
	for name := range panelTouchTargets {
		out[name] = true
	}
	return out
}

// policySectionAllowedTags is the whole section's list. Every entry is one the
// shipped page renders (TestPolicySectionAllowList_HasNoDeadEntry), and the write
// side's nine additions are exactly the ones a no-JS form needs.
var policySectionAllowedTags = map[string]bool{
	"section": true, "div": true, "p": true, "span": true,
	"h2": true, "dl": true, "dt": true, "dd": true, "strong": true,
	// The write side (M6-09 phase B).
	"form": true, "input": true, "button": true, "select": true, "option": true,
	"label": true, "fieldset": true, "legend": true, "a": true,
}

// policySectionAllowedAttrs is deliberately SHORT. `type`, `name` and `value` are
// what a form field is; `method`, `action` and `href` are destinations and are
// checked AGAINST THE ROUTER separately; `maxlength` is the one constraint the
// authoring form declares. Nothing here can make an element behave: an event handler,
// an ARIA role, a contenteditable or an hx-* attribute is a finding.
var policySectionAllowedAttrs = map[string]bool{
	"class": true, "method": true, "action": true, "href": true,
	"type": true, "name": true, "value": true, "maxlength": true,
	// `required` is a BARE attribute (no value) and is the browser's own "do not
	// submit this empty", which the server re-checks anyway. It carries no behaviour a
	// handler does not already own.
	"required": true,
}

// TestPolicyInertMarkupCheck_FlagsTheControlsThatBeatTheDenyList is the negative
// control, and its two cases are the auditor's ACTUAL escapes rather than invented
// ones. They are kept permanently: a scanner that stops recognising the thing it was
// rebuilt for is a scanner nobody notices has stopped.
func TestPolicyInertMarkupCheck_FlagsTheControlsThatBeatTheDenyList(t *testing.T) {
	cases := map[string]string{
		"htmx on an anchor with a touch-target class": `<p class="x">ok</p>` +
			`<a href="#" hx-post="/admin/policies/sys:sun-invalid/off" class="btn min-h-11">Turn this guarantee off</a>`,
		"a disclosure widget with an editable body": `<p class="x">ok</p>` +
			`<details><summary>Advanced</summary><p contenteditable="true">sys:sun-invalid</p></details>`,
		"an ARIA switch built from a span": `<span role="switch" aria-checked="true" class="tally">off</span>`,
		// 🔴 THE SIXTH ESCAPE, AND THE ONE THAT BEAT THE SCANNER RATHER THAN THE
		// BOUNDARY: a raw `>` inside an attribute value truncates the tag, so every
		// attribute after it was invisible. Kept verbatim.
		"a raw > inside an attribute value": `<p class="docket-label>" contenteditable="true" role="switch" aria-checked="true">Turn this guarantee off</p>`,
		"an inline handler":                 `<span onclick="save()" class="tally">off</span>`,
		"the old deny-list's own shapes":    `<form method="post"><button>Off</button></form>`,
	}
	for name, markup := range cases {
		if bad := interactiveMarkup(markup); len(bad) == 0 {
			t.Errorf("%s: the scan reported nothing, so it would pass over a working control "+
				"in the guardrail block", name)
		}
	}
	// EVERY ESCAPE MUST BE FLAGGED BY THE SECTION'S LIST TOO, not only by the
	// guarantee block's. That is the whole of the 2026-08-12 escape: an auditor moved a
	// working control OUT of the guarantee block, where the only remaining check was
	// the destination one, and the control carried no destination.
	for name, markup := range cases {
		bad := markupOutsideTheAllowList(markup, policySectionAllowedTags, policySectionAllowedAttrs)
		bad = append(bad, undeclaredDestinations(t, markup)...)
		if len(bad) == 0 {
			t.Errorf("%s: the SECTION-wide checks reported nothing, so the same control works "+
				"anywhere outside the guarantee block -- which is exactly where an auditor "+
				"moved one on 2026-08-12", name)
		}
	}
	// AND NEITHER LIST MAY FLAG THE MARKUP ITS OWN REGION REALLY RENDERS, or the
	// allow-list is a change detector the next author widens until it sees nothing.
	if bad := markupOutsideTheAllowList(
		`<section class="docket"><dl><dt class="docket-label">A</dt>`+
			`<dd class="font-mono">b</dd></dl><p class="mt-1"><strong>c</strong></p></section>`+
			`<form method="post" action="/admin/policies/change">`+
			`<input type="hidden" name="op" value="disable"/>`+
			`<button class="btn btn--quiet" type="submit">Switch this rule off</button></form>`,
		policySectionAllowedTags, policySectionAllowedAttrs); len(bad) > 0 {
		t.Errorf("the section scan flagged the page's own inert markup or its own form: %v", bad)
	}
	if bad := interactiveMarkup(`<section class="border-l-4"><div class="flex">` +
		`<span class="tally tally--locked">Cannot be switched off</span></div>` +
		`<p class="mt-1">why</p></section>`); len(bad) > 0 {
		t.Errorf("the guarantee scan flagged the guarantee block's own markup: %v", bad)
	}
}

// TestPolicySection_EveryFieldNameIsOneTheServerREADS.
//
// 🔴 THE SECOND ESCAPE OF 2026-08-12, CLOSED BY DERIVATION. An auditor put
//
//	<form action="/admin/policies/change">
//	  <input type="number" name="gps_radius_m" min="25" max="1000">
//
// into policySettings and every test stayed green: the form's DESTINATION was a real
// route, its markup was ordinary, and nothing anywhere asked whether the server reads
// a field called gps_radius_m. It does not -- so a customer would type a number into
// a box, press a button, and be told the change was made. That is the shape this
// section's own limit-5 paragraph calls the most expensive mistake in the repository,
// shipped inside the screen that names it.
//
// BOTH SIDES ARE DERIVED. The rendered side is every literal `name="…"` in
// policies.templ; the read side is every string literal passed to PostFormValue or
// used to index r.PostForm in policyactions.go, parsed with go/ast rather than
// grepped. The difference must be empty in the direction that matters (rendered but
// never read); the other direction is REPORTED rather than failed, because a handler
// may legitimately accept a field an older page still posts.
//
// ⚠️ WHAT IT DOES NOT SEE, counted rather than papered over: a name built from a Go
// expression (`name={ v.ConfirmField }`, which policychange.templ uses) is invisible
// to the template side, and a form field read through a helper that takes the name as
// a PARAMETER would be invisible to the handler side. Both are shapes this section
// does not use today, and both would need a real templ parser or a call-graph walk.
func TestPolicySection_EveryFieldNameIsOneTheServerREADS(t *testing.T) {
	rendered := templFieldNames(t, "../../web/templates/pages/policies.templ")
	read := handlerFormReads(t, "policyactions.go")

	// ANTI-VACUITY, BOTH SIDES: a parser that found nothing on either side would agree
	// with any page at all.
	if len(rendered) < 4 || len(read) < 4 {
		t.Fatalf("derived %d rendered field name(s) %v and %d read name(s) %v; the section "+
			"has more of both, so one of these scans is reading the wrong file",
			len(rendered), rendered, len(read), read)
	}
	for _, name := range rendered {
		if !read[name] {
			t.Errorf("the policies section renders a field called %q and %s never reads it. "+
				"A box the server ignores is worse than no box: somebody types a value, "+
				"presses a button, and is told the change was made.", name, "policyactions.go")
		}
	}
	t.Logf("field names rendered: %v", rendered)
	for name := range read {
		found := false
		for _, r := range rendered {
			if r == name {
				found = true
			}
		}
		if !found {
			t.Logf("read but not rendered by this section: %q (a handler may accept a field an "+
				"older page still posts, so this is reported rather than failed)", name)
		}
	}
}

// templFieldNames returns every LITERAL name="…" in a .templ file.
func templFieldNames(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range regexp.MustCompile(`(?i)\bname\s*=\s*"([^"]*)"`).FindAllStringSubmatch(string(raw), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// handlerFormReads returns every form field name one handler file reads.
//
// IT PARSES RATHER THAN GREPS: a grep is answered by every comment that mentions a
// field, including the ones that exist to keep this honest. It reads two shapes --
// PostFormValue("x") and r.PostForm["x"] -- which are the two this package uses.
func handlerFormReads(t *testing.T, path string) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]bool{}
	lit := func(e ast.Expr) {
		if b, ok := e.(*ast.BasicLit); ok && b.Kind == token.STRING {
			out[strings.Trim(b.Value, `"`)] = true
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok &&
				(sel.Sel.Name == "PostFormValue" || sel.Sel.Name == "FormValue") && len(v.Args) == 1 {
				lit(v.Args[0])
			}
		case *ast.IndexExpr:
			// r.PostForm["venue"]
			if sel, ok := v.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "PostForm" {
				lit(v.Index)
			}
		}
		return true
	})
	// The confirmation field is read through a helper that takes the NAME from a
	// package constant rather than a literal, so it is added from that constant --
	// which is the same value the template is handed.
	out[confirmField] = true
	return out
}

// pressTargetClassesIn reports every press-target class used in a slice of markup.
func pressTargetClassesIn(html string) []string {
	forbidden := policyGuaranteeForbiddenClasses()
	seen := map[string]bool{}
	var bad []string
	for _, m := range regexp.MustCompile(`(?i)\bclass\s*=\s*"([^"]*)"`).FindAllStringSubmatch(html, -1) {
		for _, word := range strings.Fields(m[1]) {
			// A MODIFIER COUNTS AS ITS BASE: `btn--quiet` is a button because `btn` is,
			// and the auditor's escape used exactly that pair.
			base := word
			if i := strings.Index(base, "--"); i > 0 {
				base = base[:i]
			}
			if (forbidden[word] || forbidden[base]) && !seen[word] {
				seen[word] = true
				bad = append(bad, word)
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// commentsBlanked replaces the CONTENTS of every HTML comment with spaces, keeping
// byte offsets so a caller can still slice the original.
//
// AN UNTERMINATED COMMENT BLANKS TO THE END, which is the fail-closed direction: a
// scanner that cannot tell where a comment stops must not go on trusting the tags
// after it.
func commentsBlanked(html string) string {
	out := []byte(html)
	for i := 0; i+3 < len(out); {
		if out[i] != '<' || out[i+1] != '!' || out[i+2] != '-' || out[i+3] != '-' {
			i++
			continue
		}
		end := strings.Index(html[i:], "-->")
		stop := len(out)
		if end >= 0 {
			stop = i + end + 3
		}
		for j := i; j < stop; j++ {
			if out[j] != '\n' {
				out[j] = ' '
			}
		}
		i = stop
	}
	return string(out)
}

// TestPoliciesSection_RendersNoHTMLComment.
//
// 🔴 BLANKING THE COMMENTS IS NOT ENOUGH ON ITS OWN, AND THIS IS THE OTHER HALF. The
// scanners now read past a comment correctly — but a comment in this section has no
// legitimate job: templ's own `//` comments never reach the output, so an HTML
// comment is markup somebody wrote ON PURPOSE, and the only thing it can do to these
// nets is confuse the tag balancing they depend on. It is fail-closed: the shipped
// page renders none (measured), so the rule costs nothing today and refuses the tool
// an auditor used.
//
// ⚠️ IT IS A RULE ABOUT THIS SECTION, NOT ABOUT THE PANEL. Other sections may carry
// comments for all this test knows; widening it would be a claim about templates
// nobody has looked at.
func TestPoliciesSection_RendersNoHTMLComment(t *testing.T) {
	for name, st := range policyPageStates(t) {
		section, _ := policySectionSlice(t, policyStatePage(t, st))
		if at := strings.Index(section, "<!--"); at >= 0 {
			end := at + 120
			if end > len(section) {
				end = len(section)
			}
			t.Errorf("%s: the policies section renders an HTML comment at byte %d: %q\n"+
				"templ's own comments never reach the output, so this was written as markup. "+
				"The only thing a comment does to these nets is break the tag balancing that "+
				"finds the absolutely-scanned region — which is exactly how an auditor hid a "+
				"working off switch inside it.", name, at, section[at:end])
		}
	}
}

// policyGuaranteeBlock cuts the guarantees region out of the section.
//
// 🔴 WHERE THE REGION BEGINS, SAID OUT LOUD, BECAUSE IT USED TO BE THE HOLE. This
// returns the bytes BETWEEN the wrapper's tags — so for a whole round the "absolute"
// claim covered everything after the wrapper's OPENING tag, while the test's own
// error message said "no off switch for a guarantee anywhere in this product". An
// auditor put a working switch on the line above the opening tag and ten tests stayed
// green.
//
// Two things make the sentence true now, and neither is a patch: pages.PolicyGuarantees
// emits the wrapper ITSELF, so its tags are part of the bytes policyGuaranteeBytes
// compares and nothing can sit between them but the component; and the caller of this
// helper additionally asserts that the bytes before the wrapper end with the intro's
// own closing tag, so nothing can be slipped in above it either.
//
// ⚠️ IT NO LONGER FINDS A SCANNED REGION, AND ITS OLD HEADING SAID IT DID. It used to
// balance <div> tags to cut the absolute region out of the page; that scheme was
// beaten by a decoy wrapper, an HTML comment, an HTML5 bogus comment through
// templ.Raw, and markup beside the call and was replaced by policyGuaranteeBytes,
// which renders the component alone. Nothing is scanned out of this slice any more.
// What it is still for is SHAPE: the wrapper must be present, and its caller checks
// there is exactly one of it and that nothing sits between it and the intro.
// policyGuaranteeBytes renders the guarantee component ALONE and returns the bytes it
// produces — which ARE the absolute region, rather than a slice this test works out.
//
// ⚠️ THIS DOC OPENED WITH ANOTHER FUNCTION'S NAME FOR TWO ROUNDS, said the region was
// "the bytes BETWEEN the wrapper's tags" (it includes them) and credited AdminPolicies
// with emitting the wrapper "AROUND the call to policyGuardrails" — two claims that
// were already false and an identifier that does not exist in this repository. A doc
// on the wrong function is worse than none: it is read by somebody looking at the
// right one.
//
// 🔴 IT REPLACES A <div>-BALANCING SCAN THAT WAS BEATEN REPEATEDLY, EACH BY A
// DIFFERENT WAY OF MAKING THE COUNTER DISAGREE WITH A BROWSER: markup above the
// wrapper, a filled DECOY wrapper, `<!-- </div></div> -->`, and finally
// `@templ.Raw("<!x </div> >")` — an HTML5 BOGUS COMMENT. A browser swallows `<!`
// followed by anything that is not `--`, `DOCTYPE` or `[CDATA[` up to the first `>`
// and emits no close at all; the counter counted one, ended the region early, and a
// WORKING off switch rendered inside the block with the whole package green
// (measured: the switch at section offset 8165, at block offset -1).
//
// THE TECHNIQUE IS PHASE A'S, ONE LEVEL DOWN. That round closed the
// outside-the-section channel by rendering the panel shell with an EMPTY body and
// requiring the page to be shell-head + section + shell-tail byte for byte; nothing
// has reopened it since. Here the guarantee component is rendered by itself, its bytes
// ARE the region, and the page must contain them exactly once. There is no count to
// get wrong, so no escape sequence can move the boundary — whatever templ.Raw prints
// inside the component is inside the scanned bytes.
//
// ⚠️ WHAT IT DOES NOT COVER: markup rendered by AdminPolicies AROUND the call — after
// the component's closing bytes, i.e. below the guarantees block on screen. That is
// not this region and never was; what holds there is the section-wide allow-list plus
// the route and field derivations, with the counted limit at the top of this file
// about what all three still accept. Markup placed BETWEEN the wrapper's tags is a
// different matter and is now impossible to add without being seen: the wrapper is
// part of the compared bytes, so anything inserted between them turns
// strings.Count(section, want) from 1 into 0 and this test Fatals.
func policyGuaranteeBytes(t *testing.T, screen tenant.PolicyScreen, mayEdit bool) string {
	t.Helper()
	v := pages.PoliciesView{MayEdit: mayEdit}
	fillPoliciesView(&v, screen)
	var buf bytes.Buffer
	if err := pages.PolicyGuarantees(v).Render(context.Background(), &buf); err != nil {
		t.Fatalf("rendering the guarantee component alone: %v", err)
	}
	out := buf.String()
	// ANTI-VACUITY: an empty or trivial render would make every page conform, which is
	// the failure mode this approach exists to remove rather than inherit.
	if len(out) < 500 || !strings.Contains(out, "Tappa guarantees") {
		t.Fatalf("the guarantee component rendered %d bytes and does not name itself; the "+
			"region would then be nothing", len(out))
	}
	return out
}

// policyGuaranteeBlock locates the wrapper and returns what is between its tags.
//
// 🔴 IT DOES NOT DEFINE THE SCANNED REGION — policyGuaranteeBytes does, by rendering
// the component alone. This exists for SHAPE: the wrapper has to be there, there has
// to be exactly one of it, and (in the caller) nothing may sit between it and the
// intro. It had no doc of its own for two rounds because the block above it belongs
// to policyGuaranteeBytes, so a reader arriving here read a description of a different
// function.
func policyGuaranteeBlock(t *testing.T, section string) (string, bool) {
	t.Helper()
	const anchor = `<div class="policy-guarantees">`
	// 🔴 EXACTLY ONE, COUNTED — NOT "the first one found". strings.Index took the
	// FIRST wrapper, so a DECOY built from the five allowed element names and dropped
	// above the real one made the "absolute" region the decoy, and everything in the
	// real block went unscanned. Measured: a decoy with content in it turned the whole
	// guarantee net green while a working, MayEdit-gated off switch sat inside the real
	// wrapper. (An EMPTY decoy was already red — but only through the dead-entry
	// brake's anti-vacuity floor, i.e. the net was resting on the decoy being empty.)
	//
	// This is not a gate and it cannot open a channel: it removes an AMBIGUITY. A page
	// with two wrappers has no single answer to "which bytes are the absolute region",
	// and answering it by position is answering it by luck.
	// 🔴 COMMENTS ARE BLANKED BEFORE THE WRAPPER IS COUNTED, AND AN AUDIT IS WHY. This
	// helper no longer finds the scanned region — that is the component's own render —
	// but it still counts the wrapper, and a tag inside a comment is not a tag. When
	// the region WAS found by balancing, `<!-- </div></div> -->` closed it three
	// elements early and a working off switch rendered under the "Cannot be switched
	// off" chip, inside the real wrapper's DOM, outside the scanned slice: the block
	// came back 1128 bytes where the real one is ~11 kB, the control at section offset
	// 3467 and block offset -1, package green.
	//
	// See commentsBlanked for why a comment is ALSO a finding rather than merely being
	// ignored.
	section = commentsBlanked(section)
	if n := strings.Count(section, anchor); n > 1 {
		t.Fatalf("the section renders %d %s wrappers. The absolute region is defined as "+
			"THE guarantee wrapper, so a second one makes the question ambiguous — and a "+
			"scan that answers it by taking the first match can be pointed at a decoy.", n, anchor)
	}
	start := strings.Index(section, anchor)
	if start < 0 {
		return "", false
	}
	rest := section[start+len(anchor):]
	depth := 0
	for _, m := range regexp.MustCompile(`(?is)<\s*(/?)div\b[^>]*>`).FindAllStringSubmatchIndex(commentsBlanked(rest), -1) {
		if rest[m[2]:m[3]] == "/" {
			if depth == 0 {
				return rest[:m[0]], true
			}
			depth--
			continue
		}
		depth++
	}
	return "", false
}

// undeclaredDestinations reports every form action and every link href in the
// section that the ROUTER does not serve.
//
// 🔴 THE ROUTE SET IS DERIVED FROM THE REAL ROUTER WITH chi.Walk, not written down.
// A list of "addresses this section is allowed to post to" would be the N-list class
// this repository has been bitten by repeatedly and has more than once closed by
// deriving instead. Deriving it makes the check say something a vocabulary cannot: a
// form whose action is a plausible-looking string nothing mounts is a button that
// silently does nothing.
//
// ⚠️ IT DOES NOT DISTINGUISH SECTIONS, AND AN EARLIER VERSION OF THIS PARAGRAPH SAID
// IT DID ("a form aimed at another section's write route is a control this section
// has no business offering"). It is not: the set is every route the panel mounts, so
// a control aimed at the reports export or the review decision passes. What catches
// the dangerous shape is the FIELD derivation — a cross-section form carrying a field
// this handler never reads goes red, measured. A cross-section ANCHOR carrying no
// field does not, and that gap is in the inventory above.
//
// 🔴 AND NO CONTROL MAY NAME A sys: SID, ANYWHERE IN THE SECTION. That is the
// guarantee block's absolute rule restated so it survives a control being moved out
// of the block: a form, an input, a button or a link whose action, href or value
// mentions a guardrail is a switch for something that has no switch. It reads
// ATTRIBUTE VALUES rather than text, so the authority section printing
// "sys:no-session" as prose is untouched.
func undeclaredDestinations(t *testing.T, section string) []string {
	t.Helper()
	mounted := mountedRoutes(t)
	var bad []string
	seen := map[string]bool{}
	add := func(what string) {
		if !seen[what] {
			seen[what] = true
			bad = append(bad, what)
		}
	}
	for _, m := range regexp.MustCompile(`(?is)<\s*(form|a|input|button|select|option)\b([^>]*)>`).
		FindAllStringSubmatch(section, -1) {
		tag, attrs := strings.ToLower(m[1]), m[2]
		// 🔴 A FORM WITH NO action POSTS TO THE CURRENT URL, and that is not a
		// hypothetical: this section shipped one for a round (the authoring form's action
		// was never filled in), which would have posted to GET /admin/policies and
		// answered 405 -- a button that looks like it works and does nothing. It is also
		// the shape of the old deny-list escape (`<form method="post"><button>Off</button>`),
		// which every attribute-based check passes because there is no attribute to read.
		if tag == "form" && !regexp.MustCompile(`(?is)\baction\s*=`).MatchString(attrs) {
			add("<form> with no action, which posts to the page it is on")
		}
		// FAIL CLOSED ON A TAG THIS SCANNER CANNOT READ -- interactiveMarkup's sixth
		// escape, restated here because this scanner reads attributes too.
		if strings.Count(attrs, `"`)%2 != 0 {
			add("<" + tag + " …> (unreadable: a raw > inside an attribute value)")
			continue
		}
		for _, a := range regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9:_-]*)\s*=\s*"([^"]*)"`).
			FindAllStringSubmatch(attrs, -1) {
			name, value := strings.ToLower(a[1]), a[2]
			if strings.Contains(value, "sys:") {
				add("<" + tag + " " + name + "=\"" + value + "\">, which names a guardrail")
			}
			if name != "action" && name != "href" {
				continue
			}
			path := value
			if i := strings.IndexAny(path, "?#"); i >= 0 {
				path = path[:i]
			}
			if !mounted[path] {
				add("<" + tag + " " + name + "=\"" + value + "\">, which no route serves")
			}
		}
	}
	return bad
}

// mountedRoutes walks the REAL router and returns every path it serves.
//
// The paths are compared as written, so a route with a chi parameter in it would not
// match a rendered href -- there is none in this panel, and if one is added this goes
// red rather than quietly passing, which is the safe direction.
func mountedRoutes(t *testing.T) map[string]bool {
	t.Helper()
	r := chi.NewRouter()
	h, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, newFakeLedger(), newFakeLedger(),
		&fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{},
		&fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeTexts(), adminTestConfig(), discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	h.Mount(r)
	out := map[string]bool{}
	if err := chi.Walk(r, func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out[route] = true
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	// ANTI-VACUITY: a walk that found nothing would declare every destination valid.
	if len(out) < 5 {
		t.Fatalf("the router walk found %d route(s); the panel mounts more, so this check "+
			"would pass over any address at all", len(out))
	}
	// The static assets the shell links to are served by the OUTER router
	// (internal/httpx), which this walk does not see. They are named here because the
	// alternative -- walking the whole application router -- drags the tap and
	// activation flows into a panel test.
	out["/static/css/app.css"] = true
	return out
}

// interactiveMarkup returns every element or attribute in html that is not on the
// allow-lists -- i.e. everything by which this section could become interactive.
func interactiveMarkup(html string) []string {
	return markupOutsideTheAllowList(html, policyGuaranteeAllowedTags, policyGuaranteeAllowedAttrs)
}

// markupOutsideTheAllowList is interactiveMarkup with the lists as parameters, so the
// SAME scanner runs over both regions and neither can quietly get a different one.
func markupOutsideTheAllowList(html string, tags, allowedAttrs map[string]bool) []string {
	var bad []string
	seen := map[string]bool{}
	add := func(what string) {
		if !seen[what] {
			seen[what] = true
			bad = append(bad, what)
		}
	}
	// ⚠️ CLOSING TAGS ARE SKIPPED, AND THAT IS A MEASURED NARROWING RATHER THAN A
	// COURTESY. A closer carries no attributes and no behaviour, and the section
	// slice runs to the end of the document -- so it ends with the SHELL's
	// </main></body></html>, which the first draft reported as three violations.
	// An escape needs an OPENING tag, which is what this reads.
	for _, m := range regexp.MustCompile(`<\s*(/?)\s*([a-zA-Z][a-zA-Z0-9-]*)([^>]*)>`).FindAllStringSubmatch(html, -1) {
		if m[1] == "/" {
			continue
		}
		tag, rest := strings.ToLower(m[2]), m[3]
		// 🔴 FAIL CLOSED ON A TAG THIS SCANNER CANNOT READ, because silently reading
		// HALF of one is how it went blind. `[^>]*` stops at the first `>`, INCLUDING
		// one inside an attribute value -- so an auditor wrote, inside the wrapper:
		//
		//	<p class="docket-label>" contenteditable="true" role="switch" …>
		//
		// and this returned NOTHING. `rest` was ` class="docket-label`, `class=` is
		// allowed, the bare-word sweep removed the value, and every attribute AFTER the
		// injected `>` was invisible -- including the two escapes this file keeps as
		// its own permanent negative controls.
		//
		// The answer is not a real HTML parser: there is none in the standard library
		// and §1 forbids adding a dependency for it. The answer is to REFUSE what this
		// matcher cannot read. An unbalanced double quote in the attribute chunk means
		// the tag was cut short, and that is reported as a finding rather than passed
		// over. templ escapes `>` and `"` inside attribute values (`&gt;`, `&#34;`), so
		// well-formed output never trips it -- measured on every fixture.
		if strings.Count(rest, `"`)%2 != 0 {
			add("<" + tag + " …> (unreadable: a raw > inside an attribute value)")
			continue
		}
		if !tags[tag] {
			add("<" + tag + ">")
		}
		// ⚠️ THE VALUES ARE STRIPPED FIRST, AND ONE SWEEP READS WHAT IS LEFT. The first
		// draft had TWO sweeps -- a `name=` pass over the RAW attribute chunk and a bare
		// word pass over the stripped one -- and the raw pass fired on the inside of an
		// attribute VALUE: phase B added an href with a query string
		// (`?policy=…&no=…`) and the scanner reported `policy=` and `no=` as attributes
		// of the anchor. It fails in the noisy direction rather than the silent one, but
		// a check that reports things that are not there is a check the next person
		// waters down. Stripping first also keeps the original reason for the second
		// sweep: without it every word of every class list came back as an attribute.
		//
		// EVERY attribute is still checked, including on an allowed element:
		// `contenteditable` on a <p> and `onclick` on a <span> are how two escapes
		// worked, and both survive the strip because neither is inside a value.
		bare := regexp.MustCompile(`=\s*("[^"]*"|'[^']*'|[^\s>]+)`).ReplaceAllString(rest, "")
		for _, word := range strings.Fields(bare) {
			word = strings.ToLower(strings.Trim(word, `"'/`))
			if word != "" && !allowedAttrs[word] {
				add(word)
			}
		}
	}
	return bad
}

// policySectionSlice cuts a rendered panel page into this section and everything
// outside it.
//
// 🔴 THE ANCHOR IS THE SECTION WRAPPER, AND ITS FIRST VERSION WAS A HEADING FROM
// THE MIDDLE OF THE SECTION. It cut at "How your rules are decided", which
// policyIntro renders -- and policyDegraded renders ABOVE it. An auditor put a
// working control into the degraded notice and this scan never saw it: measured,
// 2769 bytes of the section were above the cut and landed in the "outside" half.
//
// ⚠️ AND THAT WAS STILL NOT ENOUGH, WHICH IS WHY THE CALLER NO LONGER TRUSTS THIS
// FUNCTION ALONE. The same auditor then put the control one line ABOVE the wrapper
// -- still inside AdminPolicies, still on the page, still outside the slice. A
// boundary is only as good as what is checked on the other side of it, so the outside
// is checked too -- by assertNothingOutsideTheSection, which compares it BYTE FOR BYTE
// against the panel shell rendered with an empty body. (An earlier version of this
// sentence named a `policyOutsideTheSection` helper that does not exist, and said the
// outside was checked "against a list": that list was the hole, and removing it is
// what the shell comparison replaced -- see policyShellBaseline.)
func policySectionSlice(t *testing.T, body string) (section, outside string) {
	t.Helper()
	const anchor = `<div class="policy-section">`
	cut := strings.Index(body, anchor)
	if cut < 0 {
		t.Fatalf("the rendered page does not contain %q, so this scan has nothing to read", anchor)
	}
	return body[cut:], body[:cut]
}

// The identity policyBrowser signs in as. They are constants because the shell
// baseline below has to be rendered with the SAME chrome, and two copies of a name
// is exactly the drift this file keeps paying for.
const (
	policyTestAdminName = "KF Owner"
	policyTestAdminRole = "owner"
)

// policyShellBaseline renders the panel shell with an EMPTY body: the exact bytes
// this section's page must consist of once its own wrapper is removed.
//
// 🔴 IT REPLACES A HAND-WRITTEN TOKEN LIST, AND THAT LIST WAS THE HOLE. The previous
// version exempted the 38 element/attribute tokens the shell was MEASURED to
// produce -- and an auditor then built a fully working POST control above the
// wrapper out of nothing but those tokens:
//
//	<form method="post" action="…/off"><button class="btn min-h-11" name="off"
//	  type="submit">Turn this guarantee off</button></form>
//
// Every token in it is one the shell's own sign-out form uses, so no token-set check
// could ever separate the two. The panel's whole no-JS control vocabulary was
// effectively unguarded outside the wrapper. What separates them is not vocabulary
// but PRESENCE: the shell is a different template, so rendering it alone gives the
// exact byte string the outside of this page is allowed to be. Anything at all added
// above the wrapper -- a form, a link, a paragraph -- makes the comparison fail.
//
// ⚠️ THE CHROME IS RECONSTRUCTED, WHICH IS THE ONE PLACE THIS MIRRORS PRODUCTION.
// AdminAuth.chrome fills three fields from the resolved session and a fourth from
// the queue reader; this builds the same four from the same constants and the SAME
// fake reader. If chrome ever grows a fifth, this comparison fails LOUDLY with a
// byte offset rather than silently passing -- the safe direction, and the reason it
// is acceptable to mirror three lines here.
func policyShellBaseline(t *testing.T) string {
	t.Helper()
	c := pages.PanelChrome{
		FullName: policyTestAdminName, Role: policyTestAdminRole, Tab: pages.TabPolicies,
		// ⚠️ Operator MIRRORS THE FIXTURE, NOT A WISH (M7-06). This baseline is compared
		// BYTE FOR BYTE against a page rendered through the real handler, and that
		// handler computes this bit from adminTestConfig's allow-list — which names
		// panelTestAdmin, so the nav it draws carries the operator tab. A baseline that
		// left this false would differ from every page in the suite by one <a>, and the
		// difference would be reported as "markup outside the section", which is the one
		// thing this test exists to catch. It must track the fixture.
		Operator: true,
	}
	p, err := newFakeLedger().Pending(context.Background(), panelTestTenant)
	if err != nil {
		t.Fatalf("the fake queue reader failed: %v", err)
	}
	c.Pending = pages.PendingBadge{Known: true, N: p.N, Capped: p.Capped}

	var buf bytes.Buffer
	if err := pages.PanelShell(c).Render(context.Background(), &buf); err != nil {
		t.Fatalf("rendering the shell alone: %v", err)
	}
	shell := buf.String()
	// ANTI-VACUITY: an empty or trivial baseline would make every page conform.
	if len(shell) < 500 || !strings.Contains(shell, "<form") {
		t.Fatalf("the shell baseline is %d bytes and carries no form; it renders a whole "+
			"document including sign-out, so this is not the shell", len(shell))
	}
	return shell
}

// assertNothingOutsideTheSection is the derived replacement for the token list.
//
// The page must be exactly: the shell's opening bytes, this section's wrapper, and
// the shell's closing bytes. It returns the section for the caller to scan.
func assertNothingOutsideTheSection(t *testing.T, body, shell string) string {
	t.Helper()
	const anchor = `<div class="policy-section">`
	cut := strings.Index(body, anchor)
	if cut < 0 {
		t.Fatalf("the rendered page does not contain %q", anchor)
	}
	head, tail := body[:cut], shell[min(cut, len(shell)):]
	if cut > len(shell) || head != shell[:cut] {
		n := 0
		for n < len(head) && n < len(shell) && head[n] == shell[n] {
			n++
		}
		t.Fatalf("the markup BEFORE this section's wrapper is not the panel shell. The shell "+
			"is a different template, so anything here was rendered by AdminPolicies itself "+
			"-- which is where an auditor put a working sign-out-shaped POST control that no "+
			"token check could tell from the real one.\n  first difference at byte %d\n"+
			"  page:  %q\n  shell: %q", n, safeSlice(head, n, 160), safeSlice(shell, n, 160))
	}
	if !strings.HasSuffix(body, tail) {
		t.Fatalf("the markup AFTER this section does not end with the shell's own closing "+
			"bytes (%q); something was rendered below the wrapper", tail)
	}
	return body[cut : len(body)-len(tail)]
}

func safeSlice(s string, at, n int) string {
	lo := at - 40
	if lo < 0 {
		lo = 0
	}
	return s[lo:min(at+n, len(s))]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// policyScreenStates returns one screen per RENDERING BRANCH this section has.
//
// 🔴 A NET THAT ONLY SEES THE HAPPY PATH IS A NET OVER THE HAPPY PATH. Everything
// behind an `if` is invisible to a scan driven by a healthy fixture -- which is how
// an auditor's control, placed in the degraded notice AND later in the guardrail
// line of every authority card, came back green twice. Which fixtures are ENOUGH is
// not a judgement any more: TestPolicyFixtures_DriveEveryConditionalBranchOfTheTemplate
// parses the template, derives every arm, and turns red for one nothing here drives.
//
// The sentinel values in the last fixture are deliberate. Several arms render
// nothing but an interpolation, so the only string that can prove one was taken is a
// value this fixture put there. Realism is measured elsewhere (the DB tests and the
// live page); what these are for is REACH.
func policyScreenStates(t *testing.T) map[string]tenant.PolicyScreen {
	t.Helper()
	out := map[string]tenant.PolicyScreen{}

	out["healthy"] = policyScreenFixture()

	// THE ABSENCE FIXTURE. Without a screen that renders none of it, a witness could
	// be present everywhere and prove nothing -- and six of them were.
	out["unqueried"] = tenant.PolicyScreen{Queried: false}

	degraded := policyScreenFixture()
	degraded.Baseline[0].State = tenant.StateUnreadable
	degraded.Baseline[0].Problem = `policy: unknown effect "teleport"`
	degraded.Baseline[0].Statements = nil
	degraded.Unreadable = []string{degraded.Baseline[0].Name}
	degraded.GuardrailsOnly = true
	for i := range degraded.Authorities {
		if degraded.Authorities[i].FailClosed {
			degraded.Authorities[i].Grants = []tenant.AuthorityGrant{
				{Role: "owner", Sid: policy.SidAuthzOwner, Resources: []string{"*"}, OtherConditions: 1},
			}
		}
	}
	out["guardrails-only"] = degraded

	capped := policyScreenFixture()
	capped.VersionsCapped = true
	for i := range capped.Baseline {
		capped.Baseline[i].State = tenant.StateUnknown
		capped.Baseline[i].Statements = nil
	}
	out["versions-capped"] = capped

	// THE OTHER CEILING, ON ITS OWN. It is reachable (rulebook_test.go proves it)
	// and it used to render the VERSION notice: three false sentences about a read
	// that had come back complete.
	scoped := policyScreenFixture()
	scoped.AttachmentsCapped = true
	out["attachments-capped"] = scoped

	// BOTH CEILINGS AT ONCE. The two flags are independent in assemble, so this state
	// is reachable -- and with one fixture per ceiling the page was measured claiming
	// "their history … complete" one paragraph under "some versions are not listed
	// below". A state nothing drives is a state nothing checks.
	both := policyScreenFixture()
	both.VersionsCapped, both.AttachmentsCapped = true, true
	out["both-capped"] = both

	// A tenant that has never been provisioned -- the seeded Kebab Manufacturing
	// state, and the one that reaches the "guarantees alone" empty state.
	bare := policyScreenFixture()
	for i := range bare.Baseline {
		bare.Baseline[i].State = tenant.StateNotProvisioned
		bare.Baseline[i].Statements = nil
		bare.Baseline[i].History = nil
		bare.Baseline[i].Attachments = nil
	}
	out["no-baseline"] = bare

	// Every other stored state, plus a rule of the customer's own.
	states := policyScreenFixture()
	for i, st := range []tenant.RuleState{
		tenant.StateActive, tenant.StateOff, tenant.StateNotProvisioned, tenant.StateNoDocument,
	} {
		if i < len(states.Baseline) {
			states.Baseline[i].State = st
		}
	}
	states.Tenant = []tenant.Rule{{
		Name: "zz-branch-tenant-rule", State: tenant.StateActive, VersionNo: 2,
		Statements: []tenant.RuleStatement{{Sid: "own:night", Effect: "review", Actions: []string{"tap:record"}}},
	}}
	out["mixed-states"] = states

	// THE SENTINEL FIXTURE: the arms whose only output is an interpolation.
	sentinels := policyScreenFixture()
	sentinels.Baseline = nil
	sentinels.Tenant = []tenant.Rule{
		{
			// Stamp WITHOUT Outdated: stored and shipped agree.
			Name: "zz-branch-stamped", State: tenant.StateActive, VersionNo: 4241,
			StoredStamp: "zz-branch-stamp", ShippedStamp: "zz-branch-stamp",
			HistoryTotal: 9,
			History: []tenant.RuleVersion{{
				No: 4241, At: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
				Author: tenant.AuthorSystem, Bytes: 320, Current: true,
			}},
			Statements: []tenant.RuleStatement{{
				Sid: "own:sentinel", Effect: "review", Actions: []string{"tap:record"},
				Reason:     "zz-branch-reason",
				Conditions: []tenant.RuleCondition{{Operator: "Bool", Key: "tap:ipMatch", Value: "false"}},
			}},
		},
		{
			// Outdated: stored and shipped disagree, which is the designed state.
			Name: "zz-branch-stale-rule", State: tenant.StateActive, VersionNo: 1,
			StoredStamp: "zz-branch-old", ShippedStamp: "zz-branch-shipped",
		},
	}
	for i := range sentinels.Authorities {
		if sentinels.Authorities[i].FailClosed {
			sentinels.Authorities[i].GuardrailSid = "zz-branch-authz-guardrail"
			continue
		}
		sentinels.Authorities[i].GuardrailSid = "zz-branch-recording-guardrail"
	}
	out["all-branches"] = sentinels

	return out
}

// policyPageStates returns one PAGE state per fixture, i.e. a screen PLUS the two
// things the handler adds that do not come from the domain: whether the engine
// allows this admin to edit, and a refusal sentence from the query string.
//
// 🔴 IT EXISTS BECAUSE PHASE B'S BRANCHES ARE NOT ALL REACHABLE FROM A SCREEN. The
// controls hang off v.MayEdit, which the handler asks panelScribe for -- so a fixture
// set built only from tenant.PolicyScreen values could never render a single form,
// and the inert-markup net would have gone on scanning a page with no controls on it
// while the product shipped seven. That is the same failure the branch derivation
// exists to prevent, one layer up: a block no fixture reaches is a block nothing
// scans.
type policyPageState struct {
	screen  tenant.PolicyScreen
	mayEdit bool
	problem string
}

func policyPageStates(t *testing.T) map[string]policyPageState {
	t.Helper()
	out := map[string]policyPageState{}
	for name, screen := range policyScreenStates(t) {
		// READ-ONLY BY DEFAULT, which keeps every phase-A assertion measuring what it
		// measured before: the manager's page is still a page with no controls on it.
		out[name] = policyPageState{screen: screen}
	}
	// THE EDITABLE PAGE. It carries scope targets and stored policy ids, because a
	// rule with no id gets no controls at all (policyControls) -- a fixture without
	// them would render the same page as the read-only one and prove nothing.
	editable := policyScreenFixture()
	editable.ScopeTargets = []tenant.ScopeTarget{
		{Kind: "location", ID: uuid.New(), Name: "zz-branch-venue"},
		{Kind: "department", ID: uuid.New(), Name: "zz-branch-department"},
	}
	editable.Settings = []tenant.Setting{{
		Label: "zz-branch-setting", Current: "175 m", Min: "25 m", Max: "1000 m",
		Uses: "zz-branch-setting-use",
	}}
	for i := range editable.Baseline {
		editable.Baseline[i].PolicyID = uuid.New()
		editable.Baseline[i].Attachments = []string{"*"}
	}
	// ONE RULE SWITCHED OFF, so the enable arm of the switch has a subject too.
	editable.Baseline[1].State = tenant.StateOff
	editable.Baseline[1].Statements = nil
	// AND ONE RULE OF THE CUSTOMER'S OWN, WITH AN ID — without it the form can only
	// ever offer "a new rule", which is the state that produced an unremovable
	// duplicate for a round.
	editable.Tenant = []tenant.Rule{{
		PolicyID: uuid.New(), Name: "zz-branch-own-rule", State: tenant.StateActive, VersionNo: 2,
		Statements: []tenant.RuleStatement{{Sid: "own:night", Effect: "review", Actions: []string{"tap:record"}}},
	}}
	out["editable"] = policyPageState{screen: editable, mayEdit: true}

	// THE CAPPED SCOPE PICKER AND THE REFUSAL SENTENCE, on a page that also has
	// controls -- both are states a customer reaches by pressing something.
	refused := editable
	refused.ScopeCapped = true
	// THE PROBLEM SENTENCE IS REACHED THE WAY A CUSTOMER REACHES IT -- through the
	// query word the write path redirects with. A sentinel string cannot be injected
	// here and that is the point: policyProblemSentence maps a CLOSED list of words
	// onto sentences this package owns, so a fixture that could inject its own text
	// would be testing a page the product cannot render.
	out["refused"] = policyPageState{screen: refused, mayEdit: true, problem: "not-permitted"}
	return out
}

// policyEvaluateAllowed names every place the panel MAY call policy.Evaluate, with
// the reason. It is an ALLOWLIST THAT MUST STAY SMALL: adding a name here is the
// only way to keep the page's sentence true while a new call appears, so it is an
// edit somebody has to defend.
// ⚠️ THE KEY IS file#function, AND FILE ALONE WAS NOT ENOUGH. An auditor put a
// phase-B-shaped gate in a NEW file and this went red -- but the same gate written
// INSIDE rulebook.go passed, and the log helpfully printed defaultEffectFor's
// exemption reason next to it, which is false for a gate. An exemption is a
// statement about one FUNCTION's argument, so that is what it is keyed on.
var policyEvaluateAllowed = map[string]string{
	"../domain/tenant/rulebook.go#defaultEffectFor": "evaluates an EMPTY Set with no " +
		"request context, so the only answer the engine can give is its own built-in " +
		"default -- the quantity the page PRINTS. Nothing is authorised by it.",
}

// TestPoliciesSection_SaysWhetherTheEngineIsConsulted is the tie that keeps the
// authorization section honest AS THE PRODUCT CHANGES.
//
// 🔴 THE SCAN COVERS BOTH PACKAGES THE PANEL IS MADE OF, AND THE FIRST VERSION DID
// NOT. It parsed internal/handler alone and found zero -- while the panel's own read
// side, internal/domain/tenant/rulebook.go, calls policy.Evaluate on EVERY render.
// That call is harmless (see the allowlist above) but the test's SHAPE was not:
// phase B puts authorization in that same package, so a real gate could have landed
// there with this test still green and the page still saying the dashboard does not
// consult the engine -- the exact inversion of what the test is for.
//
// So: every call in either package must be on the allowlist, and the page's sentence
// must agree with whether any call is NOT.
func TestPoliciesSection_SaysWhetherTheEngineIsConsulted(t *testing.T) {
	body := policyPage(t, newFakeRules())
	says := strings.Contains(strings.Join(strings.Fields(body), " "),
		"the dashboard does not consult it before an action")

	gates := 0
	for _, dir := range []string{".", "../domain/tenant"} {
		for _, at := range policyEvaluateCalls(t, dir) {
			if why, ok := policyEvaluateAllowed[at.key]; ok {
				t.Logf("allowed: %s (%s) -- %s", at.key, at.where, why)
				continue
			}
			gates++
			t.Logf("NOT on the allowlist: %s calls policy.Evaluate (%s)", at.key, at.where)
		}
	}
	switch {
	case gates == 0 && !says:
		t.Error("no panel code consults the policy engine as a gate, and the page does not " +
			"say so. The grants it prints are what the engine WOULD answer; a reader who " +
			"takes them for enforcement is being misled.")
	case gates > 0 && says:
		t.Errorf("%d call(s) to policy.Evaluate are not on policyEvaluateAllowed, but the "+
			"page still says the dashboard does not consult the engine. One of the two has "+
			"to change -- and the sentence is the one a customer reads.", gates)
	}
	// AN ALLOWLIST ENTRY THAT PROTECTS NOTHING IS A HOLE NOBODY IS WATCHING, so it is
	// held to its own size the same way the §4.5 belt's exemptions are.
	for key := range policyEvaluateAllowed {
		found := false
		for _, dir := range []string{".", "../domain/tenant"} {
			for _, at := range policyEvaluateCalls(t, dir) {
				if at.key == key {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("policyEvaluateAllowed names %s, which no longer calls policy.Evaluate; "+
				"remove the entry", key)
		}
	}
}

// evaluateCall is one policy.Evaluate call site.
type evaluateCall struct {
	// key is "<file>#<enclosing function>", which is what policyEvaluateAllowed is
	// keyed on. A call in a function nobody argued for is a gate, even if another
	// function in the same file is exempt.
	key   string
	where string // file:line:col, for the log
}

// policyEvaluateCalls finds calls to policy.Evaluate in one package's non-test
// files. It parses rather than greps, because a grep is answered by every comment
// that mentions the engine -- including the ones that exist to keep this honest.
func policyEvaluateCalls(t *testing.T, dir string) []evaluateCall {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", dir, err)
	}
	files := 0
	var out []evaluateCall
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			files++
			// THE WALK IS PER TOP-LEVEL DECLARATION, so every call carries the name of
			// the function it is in. A call outside any function (a package-level var
			// initialiser) gets "<file>#" and is therefore never on the allowlist --
			// fail-closed, and the shape somebody would reach for to hide one.
			for _, decl := range f.Decls {
				fn, _ := decl.(*ast.FuncDecl)
				owner := ""
				if fn != nil {
					owner = fn.Name.Name
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Evaluate" {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "policy" {
						out = append(out, evaluateCall{
							key:   filepath.ToSlash(name) + "#" + owner,
							where: fset.Position(call.Pos()).String(),
						})
					}
					return true
				})
			}
		}
	}
	// ANTI-VACUITY, PER DIRECTORY: a walk that parsed nothing would report a panel
	// that consults nothing.
	if files == 0 {
		t.Fatalf("the walk parsed no non-test file in %s", dir)
	}
	return out
}

// TestPolicyProblemWords_EachOneRendersAHeadingAndASentence.
//
// 🔴 A WORD WITH NO SENTENCE PRINTS NOTHING AT ALL, WHICH IS THE ONE OUTCOME THE
// BLOCK FORBIDS. policiesSection renders the refusal notice behind `if v.Problem !=
// ""`, and its own comment says a press that did nothing has to be visible — so a
// sixteenth word added to policyProblemWords without a sentence would redirect a
// customer to a page that silently says nothing happened. Measured: adding one and
// running the package stayed green.
//
// IT COVERS THE HEADING TOO, for the reason the lockout pair exists: the title is the
// largest sentence on the page, and it was wrong for `lockout-stands` while the body
// was right.
func TestPolicyProblemWords_EachOneRendersAHeadingAndASentence(t *testing.T) {
	// ANTI-VACUITY: an empty vocabulary would make this pass over nothing.
	if len(policyProblemWords) < 5 {
		t.Fatalf("policyProblemWords holds %d word(s); the section uses more",
			len(policyProblemWords))
	}
	for _, word := range policyProblemWords {
		heading, sentence := policyProblem(word)
		if sentence == "" {
			t.Errorf("the problem word %q has no sentence, so a refusal that redirects with "+
				"it renders NO notice at all — the page says nothing happened while something "+
				"was refused.", word)
		}
		if heading == "" {
			t.Errorf("the problem word %q has no heading; the notice would render with an "+
				"empty title", word)
		}
		// AND IT REACHES THE PAGE, not just the mapping.
		page := policyStatePage(t, policyPageState{screen: policyScreenFixture(), problem: word})
		if !strings.Contains(page, htmlText(sentence)) {
			t.Errorf("the page does not print the sentence for %q", word)
		}
		if !strings.Contains(page, htmlText(heading)) {
			t.Errorf("the page does not print the heading for %q", word)
		}
	}
	// AND A WORD OUTSIDE THE LIST RENDERS NOTHING — the closed-vocabulary half, or a
	// caller could put text on somebody's page.
	if h, s := policyProblem("anything-else"); h != "" || s != "" {
		t.Errorf("a word outside the closed list produced %q / %q", h, s)
	}
}

// TestPolicyGuardrailView_CarriesNoSwitch is the presentation-layer half of the
// structural guarantee (the domain half is
// tenant.TestGuardrail_CarriesNoSwitch). The view is what the template is handed,
// so this is the type that decides what a template CAN render.
func TestPolicyGuardrailView_CarriesNoSwitch(t *testing.T) {
	typ := reflect.TypeOf(pages.PolicyGuardrailView{})
	if typ.NumField() < 4 {
		t.Fatalf("PolicyGuardrailView has %d field(s); it carries more, so this reflection "+
			"is looking at the wrong type", typ.NumField())
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.String {
			t.Errorf("PolicyGuardrailView.%s is a %s. Every field here is a STRING on "+
				"purpose: a bool is one `if` away from a checkbox, and a guardrail has no "+
				"off switch anywhere in the product.", f.Name, f.Type.Kind())
		}
		lower := strings.ToLower(f.Name)
		// 🔴 THE SAME WORD LIST AS ITS TWIN, tenant.switchLikeFields. The two drifted
		// once already -- this one had seven words while the domain's had thirteen --
		// and a pair of walls with different standards is a pair where the weaker one
		// is the real one. They are two literals in two packages because the twin is a
		// test helper in a package this one must not import; the duplication is COUNTED
		// rather than hidden, and TestPolicyGuardrailWords_MatchTheDomainWall keeps
		// them in step by reading the other file.
		// ⚠️ THE MATCH IS PREFIX-OR-EQUAL, LIKE THE TWIN, AND CONTAINS WAS WRONG. With
		// `strings.Contains` the shared word "on" flagged `Reason` -- a false alarm on
		// a field this type must have. Two walls that share a vocabulary and differ in
		// how they apply it are still two different walls.
		for _, word := range policyGuardrailControlWords {
			if lower == word || strings.HasPrefix(lower, word) {
				t.Errorf("PolicyGuardrailView.%s reads like a control; the guardrail block "+
					"offers none (M6-09 card)", f.Name)
			}
		}
	}
}

// TestPolicyTones_AreClassesTheStylesheetDeclares.
//
// 🔴 A CHIP WHOSE CLASS THE STYLESHEET DOES NOT DECLARE RENDERS AS BARE TEXT, and it
// fails in the direction nobody notices: the word is still there, so the page reads
// correctly and only the distinction between a locked rule and a switched-off one
// disappears. The tones are joined onto a prefix in the template, so the Tailwind
// standalone CLI -- which scans templates as raw text -- cannot see them being used;
// this is what stands in for that.
func TestPolicyTones_AreClassesTheStylesheetDeclares(t *testing.T) {
	css, err := os.ReadFile("../../web/static/css/input.css")
	if err != nil {
		t.Fatalf("reading input.css: %v", err)
	}
	tmpl, err := os.ReadFile("../../web/templates/pages/policies.templ")
	if err != nil {
		t.Fatalf("reading policies.templ: %v", err)
	}
	tones := []string{
		pages.PolicyToneLocked, pages.PolicyToneRunning, pages.PolicyToneOff,
		pages.PolicyToneWaiting, pages.PolicyToneRefusing,
	}
	for _, tone := range tones {
		if !strings.Contains(string(css), ".tally--"+tone) {
			t.Errorf("the stylesheet declares no chip for the %q tone, so it renders as bare "+
				"text and the guardrail block stops being tellable apart from the baseline's",
				tone)
		}
		// 🔴 AND THE NAME MUST APPEAR LITERALLY IN A FILE TAILWIND SCANS. The CLI reads
		// ./web/templates/**/*.templ and ./web/static/js/**/*.js as RAW TEXT and does NOT
		// read Go files, so a class assembled from a Go constant is one the build drops --
		// and it fails silently, because the chip's WORD is still on the page. Measured
		// before policyToneClass existed: four of these five survived only because other
		// screens spell them out, which is a coincidence rather than a guarantee.
		if !strings.Contains(string(tmpl), "tally--"+tone) {
			t.Errorf("the %q chip class does not appear literally in policies.templ, so the "+
				"Tailwind build cannot see this section using it; if the other screen that "+
				"spells it out ever stops, the chip renders unstyled and nothing goes red", tone)
		}
	}
	// The lock tone must be its OWN declaration rather than sharing a status colour:
	// the whole point is that a guardrail is not a state.
	for _, status := range []string{pages.PolicyToneRunning, pages.PolicyToneOff, pages.PolicyToneWaiting, pages.PolicyToneRefusing} {
		if pages.PolicyToneLocked == status {
			t.Errorf("the lock tone is the same class as the %q status tone; a customer would "+
				"read the locked block as another list of things they could switch", status)
		}
	}
}

// TestPoliciesSection_TellsTheStatesApartOnThePage. Three of the five states look
// the same from a distance -- "not running" -- and each calls for a different act.
func TestPoliciesSection_TellsTheStatesApartOnThePage(t *testing.T) {
	screen := policyScreenFixture()
	// 🔴 StateUnknown IS IN THIS LIST BECAUSE ITS SENTENCE WAS UNPINNED. An auditor
	// changed "Cannot be determined on this page" to "Not set up yet" and all five
	// tests passed -- the DOMAIN state was pinned, the WORDS were not, and the two
	// states then shared one label on the page while this test's own "no two states
	// share a word" assertion never saw the newcomer.
	states := []tenant.RuleState{
		tenant.StateActive, tenant.StateOff, tenant.StateNotProvisioned,
		tenant.StateNoDocument, tenant.StateUnreadable, tenant.StateUnknown,
	}
	if len(screen.Baseline) < len(states) {
		t.Fatalf("the fixture holds %d rule(s); this case needs %d", len(screen.Baseline), len(states))
	}
	for i, s := range states {
		screen.Baseline[i].State = s
		if s == tenant.StateUnreadable {
			screen.Baseline[i].Problem = "policy: unknown effect \"nonsense\""
		}
	}
	body := policyPage(t, &fakeRules{screen: screen})
	seen := map[string]bool{}
	for _, s := range states {
		label, tone := stateWords(s)
		if !strings.Contains(body, label) {
			t.Errorf("state %q renders no label on the page", s)
		}
		if seen[label] {
			t.Errorf("state %q shares the label %q with another state; the three that are all "+
				"'not running' need different words because they need different acts", s, label)
		}
		seen[label] = true
		if !strings.Contains(body, "tally--"+tone) {
			t.Errorf("state %q renders no chip tone", s)
		}
	}
	if !strings.Contains(body, "unknown effect") {
		t.Error("an unreadable document does not say WHY. The engine is ignoring that rule, " +
			"and a customer who is not told believes it is running -- which ADR 0004 calls " +
			"the most dangerous failure there is.")
	}
}

// TestEffectSentence_TranslatesEveryEffectTheEngineHas. The keyword is shown beside
// the sentence because a docket carries the keyword; the sentence is for everybody
// else. An effect with no translation falls through as itself rather than blank --
// asserted here so the fallback is a decision rather than a discovery.
func TestEffectSentence_TranslatesEveryEffectTheEngineHas(t *testing.T) {
	for _, e := range []policy.Effect{
		policy.EffectAllow, policy.EffectReview, policy.EffectDeny,
		policy.EffectIgnore, policy.EffectRedirect,
	} {
		got := effectSentence(string(e))
		if got == string(e) {
			t.Errorf("%s has no sentence; a customer reading this page should not have to "+
				"know the engine's vocabulary", e)
		}
		if !strings.HasSuffix(got, ".") {
			t.Errorf("%s renders %q, which is not a sentence", e, got)
		}
	}
	if got := effectSentence("something-new"); got != "something-new" {
		t.Errorf("an unknown effect rendered %q; it must fall through as itself rather than "+
			"disappearing", got)
	}
}

// TestEffectSentence_DoesNotAssumeTheActionIsATap.
//
// 🔴 IT WAS ADDED BECAUSE THE RENDERED PAGE WAS MEASURED SAYING SOMETHING FALSE.
// Every deny used to read "Refuses the tap", and the page therefore told a customer
// that sys:policy-edit-owner-only and sys:no-self-review refuse taps -- two
// guardrails that have nothing to do with one, and the same wording was wrong on
// every authorization statement. The engine is general-purpose and its actions are
// namespaced for exactly that reason, so a sentence that names the action is wrong
// wherever the action is not that one.
func TestEffectSentence_DoesNotAssumeTheActionIsATap(t *testing.T) {
	for _, e := range []policy.Effect{
		policy.EffectAllow, policy.EffectReview, policy.EffectDeny, policy.EffectIgnore,
	} {
		if s := effectSentence(string(e)); strings.Contains(strings.ToLower(s), "tap") {
			t.Errorf("%s renders %q, which names the action. It is applied to guardrails and "+
				"statements whose action is policy:edit, record:review or report:export, and "+
				"the sentence is false for every one of them.", e, s)
		}
	}
	// redirect is the exception and it is DELIBERATE: sys:no-session and
	// sys:tenant-mismatch are the only two redirects and both are tap-only by
	// construction (one is gated on tap:record, the other needs a tag), and the
	// sentence has to say a person is sent somewhere. Pinned so the exception is a
	// decision rather than an oversight.
	if s := effectSentence(string(policy.EffectRedirect)); !strings.Contains(s, "activation page") {
		t.Errorf("redirect renders %q; it must still say where the person is sent", s)
	}
}

// TestPoliciesSection_SaysNothingIsDecidingWhenTheEngineHasDroppedTheLayer -- the
// blocking correction, measured on the rendered page.
//
// 🔴 EIGHT CORRECT-LOOKING LINES ARE WORSE THAN ONE WRONG ONE. When a single stored
// baseline document cannot be read the tap engine drops the WHOLE policy layer and
// decides on the guarantees alone, so a page that flagged only the broken rule would
// leave every other rule wearing an "In force" chip and every permission listed as
// granted -- while none of them decides anything. That is the failure ADR 0004 calls
// the most dangerous there is, printed with confidence.
func TestPoliciesSection_SaysNothingIsDecidingWhenTheEngineHasDroppedTheLayer(t *testing.T) {
	screen := policyScreenFixture()
	if len(screen.Baseline) < 2 {
		t.Fatalf("the fixture holds %d rule(s); this case needs at least two", len(screen.Baseline))
	}
	broken := screen.Baseline[0].Name
	screen.Baseline[0].State = tenant.StateUnreadable
	screen.Baseline[0].Problem = `policy: unknown effect "teleport"`
	screen.Baseline[0].Statements = nil
	screen.Unreadable = []string{broken}
	screen.GuardrailsOnly = true
	// The authority section must carry the same caveat, so give it something to say.
	for i := range screen.Authorities {
		if screen.Authorities[i].FailClosed {
			screen.Authorities[i].Grants = []tenant.AuthorityGrant{
				{Role: "owner", Sid: policy.SidAuthzOwner, Resources: []string{"*"}},
			}
		}
	}

	body := policyPage(t, &fakeRules{screen: screen})
	for _, want := range []string{
		"Your rules are not being applied right now",
		"not deciding anything today",
		broken,
		"None of these grants is being applied today",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
	if strings.Contains(body, "In force") {
		t.Error("a rule is still labelled \"In force\" while the engine is running on " +
			"guardrails alone. The chip is what a reader takes away from a page of nine " +
			"rules, and it is the page's most confident claim.")
	}
	// 🔴 AND THE SAME CLAIM ONE ELEMENT FURTHER DOWN, which the first fix missed:
	// every history strip went on saying "in use" in tappa-green about versions the
	// engine is using for nothing. A test that looks for ONE string is a test that
	// finds one of two identical lies.
	if strings.Contains(body, ">in use<") {
		t.Error("a version is still marked \"in use\" while the engine has dropped the " +
			"whole layer and is using no version of anything")
	}
	if !strings.Contains(body, "newest stored") {
		t.Error("the history strip says nothing at all about which version WOULD be used; " +
			"removing the false marker must not remove the fact with it")
	}
	if n := strings.Count(body, "tally--active"); n != 0 {
		t.Errorf("%d chip(s) still carry the running tone on a page where nothing is "+
			"running; colour reinforces the word and here it would contradict it", n)
	}

	// CONTROL: with nothing broken the page says none of it, and DOES say "In force".
	// Without this the assertions above would pass over a page that always warns.
	healthy := policyPage(t, newFakeRules())
	for _, forbidden := range []string{
		"Your rules are not being applied right now",
		"None of these grants is being applied today",
	} {
		if strings.Contains(healthy, forbidden) {
			t.Errorf("a healthy rulebook renders %q; the warning would then be noise a "+
				"reader learns to skip", forbidden)
		}
	}
	if !strings.Contains(healthy, "In force") {
		t.Error("a healthy rulebook labels nothing as in force, so the assertion above " +
			"could not have failed")
	}
	if !strings.Contains(healthy, ">in use<") {
		t.Error("a healthy rulebook marks no version as in use, so the marker assertion " +
			"above could not have failed either")
	}
}

// TestPoliciesSection_DoesNotTellABusinessWithNoBaselineThatTheBaselineIsCarryingIt.
//
// Measured on the seeded Kebab Manufacturing tenant, which has no policy rows at
// all: nine rules marked "Not set up yet" sat directly above a line saying the
// business was "running on the guarantees and the baseline above". A reassurance
// that is false on the very screen showing why is worse than none.
func TestPoliciesSection_DoesNotTellABusinessWithNoBaselineThatTheBaselineIsCarryingIt(t *testing.T) {
	screen := policyScreenFixture()
	for i := range screen.Baseline {
		screen.Baseline[i].State = tenant.StateNotProvisioned
		screen.Baseline[i].Statements = nil
	}
	body := policyPage(t, &fakeRules{screen: screen})
	if strings.Contains(body, "running on the guarantees and the baseline above") {
		t.Error("with no baseline rule in force the page still claims the baseline is " +
			"carrying the business, directly under a list saying every one of them is not " +
			"set up")
	}
	if !strings.Contains(body, "running on the Tappa guarantees alone") {
		t.Error("the page does not say what IS carrying the business; an empty state that " +
			"only removes a claim leaves the reader with nothing")
	}
	// CONTROL: the reassuring sentence is still there when it is true.
	if !strings.Contains(policyPage(t, newFakeRules()), "running on the guarantees and the baseline above") {
		t.Error("a tenant whose baseline IS in force no longer gets the sentence at all")
	}
}

// TestPoliciesSection_PrintsAGrantsFenceRatherThanJustItsRole.
func TestPoliciesSection_PrintsAGrantsFenceRatherThanJustItsRole(t *testing.T) {
	screen := policyScreenFixture()
	for i := range screen.Authorities {
		if !screen.Authorities[i].FailClosed {
			continue
		}
		screen.Authorities[i].Grants = []tenant.AuthorityGrant{{
			Role: "manager", Sid: "own:qr-approvals",
			Resources: []string{"location/rusty-bar"}, OtherConditions: 2,
		}}
	}
	body := policyPage(t, &fakeRules{screen: screen})
	for _, want := range []string{"manager — own:qr-approvals", "location/rusty-bar", "2 further conditions"} {
		if !strings.Contains(body, want) {
			t.Errorf("the grant line does not carry %q, so it describes a wider authority "+
				"than the statement gives", want)
		}
	}
}

// TestPoliciesSection_ShowsTheActionGuardrailOnTheRecordingRowToo.
//
// The domain derives an action-scoped guardrail for BOTH groups of actions, and only
// the fail-closed loop printed it -- so `tap:record -> sys:no-session` was computed
// on every render and shown nowhere. A screen that knows and does not say is the
// shape this whole section exists to avoid.
func TestPoliciesSection_ShowsTheActionGuardrailOnTheRecordingRowToo(t *testing.T) {
	screen := policyScreenFixture()
	found := false
	for i := range screen.Authorities {
		if !screen.Authorities[i].FailClosed {
			screen.Authorities[i].GuardrailSid = "sys:no-session"
			found = true
		}
	}
	if !found {
		t.Fatal("the fixture has no recording action, so this case proves nothing")
	}
	body := policyPage(t, &fakeRules{screen: screen})
	if !strings.Contains(body, "sys:no-session") {
		t.Error("the recording action's guardrail is not on the page, although the domain " +
			"worked it out")
	}
}

// TestPolicySectionAllowList_HasNoDeadEntry holds the markup allow-list to the
// markup, in both directions.
//
// 🔴 AN ALLOW-LIST ENTRY NOBODY USES IS A DOOR NOBODY IS WATCHING. It is the exact
// argument policyEvaluateAllowed's own dead-entry check makes, and the two lists
// should not have different standards. Widening this list stays possible -- it just
// has to come with the markup that needs it.
func TestPolicySectionAllowList_HasNoDeadEntry(t *testing.T) {
	// 🔴 IT DRIVES THE PAGE STATES, NOT THE SCREEN STATES, AND THE DIFFERENCE IS THE
	// WHOLE WRITE SIDE. policyScreenStates carries no MayEdit, so a scan built on it
	// renders no form at all -- every one of phase B's nine element names would have
	// read as a dead entry, and the honest repair for a "dead" entry is to DELETE it.
	// That is how a brake against over-wide lists turns into a hole.
	used, guaranteeUsed := map[string]bool{}, map[string]bool{}
	for _, st := range policyPageStates(t) {
		section, _ := policySectionSlice(t, policyStatePage(t, st))
		for _, m := range regexp.MustCompile(`<\s*([a-zA-Z][a-zA-Z0-9-]*)`).FindAllStringSubmatch(section, -1) {
			used[strings.ToLower(m[1])] = true
		}
		if g, ok := policyGuaranteeBlock(t, section); ok {
			for _, m := range regexp.MustCompile(`<\s*([a-zA-Z][a-zA-Z0-9-]*)`).FindAllStringSubmatch(g, -1) {
				guaranteeUsed[strings.ToLower(m[1])] = true
			}
		}
	}
	// ANTI-VACUITY: a scan that found nothing would call every entry dead.
	if len(used) < 5 || len(guaranteeUsed) < 3 {
		t.Fatalf("the rendered section uses %d element name(s) and the guarantee block %d; "+
			"both use more, so this scan is reading the wrong thing", len(used), len(guaranteeUsed))
	}
	// EACH LIST IS HELD TO THE REGION IT IS APPLIED TO. Holding both to the whole
	// section was the defect the split introduced: dl, dt, dd and strong sat in the
	// list that guards the GUARANTEE block, which renders none of them, so four
	// entries were "protecting" a region nothing scanned.
	for tag := range policySectionAllowedTags {
		if !used[tag] {
			t.Errorf("policySectionAllowedTags admits <%s>, which no rendering branch of this "+
				"section produces. Remove it: an allow-list entry nothing needs is a door "+
				"nobody is watching.", tag)
		}
	}
	for tag := range policyGuaranteeAllowedTags {
		if !guaranteeUsed[tag] {
			t.Errorf("policyGuaranteeAllowedTags admits <%s>, which the guarantee block does "+
				"not render.", tag)
		}
	}
	t.Logf("element names rendered by the section: %d, by the guarantee block: %d",
		len(used), len(guaranteeUsed))
}

// policyGuardrailControlWords is the field-name vocabulary a guardrail view may not
// use. It mirrors tenant.switchLikeFields' list; the test below reads that file and
// refuses to let them drift.
var policyGuardrailControlWords = []string{
	"enable", "disable", "toggle", "switch", "on", "off",
	"active", "state", "status", "control", "href", "action", "form",
}

// TestPolicyGuardrailWords_MatchTheDomainWall.
//
// 🔴 TWO WALLS WITH DIFFERENT STANDARDS ARE ONE WALL. The domain's reflect check and
// this one exist to refuse the same thing on two types, and they drifted the first
// time somebody strengthened one: the domain gained state/status/control and this
// list did not. Neither package may import the other's test helper, so the lists are
// two literals -- and this reads the other file's literal rather than trusting a
// comment to keep them equal.
func TestPolicyGuardrailWords_MatchTheDomainWall(t *testing.T) {
	raw, err := os.ReadFile("../domain/tenant/rulebook_test.go")
	if err != nil {
		t.Fatalf("reading the domain wall: %v", err)
	}
	// The twin writes its list as a []string literal inside switchLikeFields.
	m := regexp.MustCompile(`(?s)func switchLikeFields.*?\[\]string\{(.*?)\}`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("could not find the word list in tenant.switchLikeFields; if it moved, this " +
			"check has stopped comparing anything")
	}
	theirs := map[string]bool{}
	for _, q := range regexp.MustCompile(`"([a-z]+)"`).FindAllStringSubmatch(m[1], -1) {
		theirs[q[1]] = true
	}
	if len(theirs) < 5 {
		t.Fatalf("read %d word(s) from the domain wall; it carries more, so the extraction "+
			"is broken and this test would agree with anything", len(theirs))
	}
	mine := map[string]bool{}
	for _, w := range policyGuardrailControlWords {
		mine[w] = true
	}
	for w := range theirs {
		if !mine[w] {
			t.Errorf("the domain wall refuses a field name containing %q and this one does "+
				"not; the weaker of two walls is the real one", w)
		}
	}
	for w := range mine {
		if !theirs[w] {
			t.Errorf("this wall refuses %q and the domain one does not; same argument, other "+
				"direction", w)
		}
	}
}

// TestPoliciesSection_EachCeilingNamesTheListItActuallyShortened.
//
// 🔴 ONE NOTICE FOR TWO CEILINGS TOLD A CUSTOMER THREE FALSE THINGS. Measured with
// only the ATTACHMENT read truncated and the version read COMPLETE: the page said
// their policy HISTORY was too long to read, that some VERSIONS were missing below,
// and that a rule whose row fell outside the page would say so -- while no rule was
// reported unknown and the list that had actually been cut short ("Bound to") was
// printed as though it were whole. The domain had already split the flag; this is
// the same split arriving where somebody reads it.
func TestPoliciesSection_EachCeilingNamesTheListItActuallyShortened(t *testing.T) {
	const (
		versionNotice     = "Some version history is not on this page"
		attachmentNotice  = "Some scope bindings are not on this page"
		versionSentence   = "so some\n\t\t\t\t\t\tversions are not listed below"
		attachmentListing = "“Bound to” lists below are shortened"
	)
	states := policyScreenStates(t)

	versions := policyPage(t, &fakeRules{screen: states["versions-capped"]})
	if !strings.Contains(versions, versionNotice) {
		t.Error("a truncated VERSION read renders no notice about it")
	}
	if strings.Contains(versions, attachmentNotice) {
		t.Error("a truncated version read claims the scope bindings were shortened too; " +
			"the two reads have their own ceilings and their own consequences")
	}

	attachments := policyPage(t, &fakeRules{screen: states["attachments-capped"]})
	if !strings.Contains(attachments, attachmentNotice) {
		t.Error("a truncated ATTACHMENT read renders no notice at all, so the shortened " +
			"\"Bound to\" lists are printed as though they were complete")
	}
	if strings.Contains(attachments, versionNotice) {
		t.Errorf("a truncated attachment read still claims %q. The version read came back "+
			"COMPLETE, so that sentence is false, and so is everything it implies about "+
			"which rules could not be identified.", versionNotice)
	}
	if !strings.Contains(attachments, attachmentListing) {
		t.Error("the attachment notice does not name the list it shortened; a warning that " +
			"does not say WHAT is short is one a reader cannot act on")
	}
	if !strings.Contains(attachments, "Everything else on this page is complete") {
		t.Error("with only the bindings shortened, the page does not say the rest is whole -- " +
			"which it is, and a reader left guessing treats the whole page as unreliable")
	}

	// 🔴 BOTH CEILINGS AT ONCE: the page must not reassure and contradict itself in
	// consecutive paragraphs. Measured doing exactly that -- "some versions are not
	// listed below", then "their history … complete".
	bothPage := policyPage(t, &fakeRules{screen: states["both-capped"]})
	if !strings.Contains(bothPage, versionNotice) || !strings.Contains(bothPage, attachmentNotice) {
		t.Fatal("the both-capped fixture does not render both notices, so this case proves nothing")
	}
	if strings.Contains(bothPage, "Everything else on this page is complete") {
		t.Error("with BOTH reads truncated the page still claims everything else is complete, " +
			"one paragraph below its own notice saying the version history is not. A page " +
			"that contradicts itself is one a manager stops reading.")
	}
	if !strings.Contains(bothPage, "Your version history is shortened too") {
		t.Error("the bindings notice says nothing about the other truncation; removing a false " +
			"reassurance must not remove the fact with it")
	}
	// AND the rules must still report their real state -- the whole reason F4 split
	// the flags.
	if strings.Contains(attachments, "Cannot be determined on this page") {
		t.Error("a rule reads as undeterminable although the read that determines it came " +
			"back whole")
	}
}

// templSwitchArms counts the `switch`/`case` arms inside templ blocks under a
// directory, with the SAME inTempl bookkeeping policyTemplBranches uses.
func templSwitchArms(t *testing.T, root string) (files []string, arms int) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".templ") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		inTempl, found := false, 0
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "templ "):
				inTempl = true
				continue
			case line == "}":
				inTempl = false
				continue
			}
			if !inTempl || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.HasPrefix(trimmed, "switch ") || trimmed == "switch {" {
				found++
			}
		}
		if found > 0 {
			files = append(files, path)
			arms += found
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return files, arms
}

// TestPolicyBranchDerivation_ReportsTheSizeOfItsOwnBlindSpot.
//
// 🔴 THE LIMIT IS MEASURED ON EVERY RUN RATHER THAN QUOTED. The comment on
// policyTemplBranches used to say "three shipped templates already use a switch";
// the real figure was three times that, which is the hand-counted-N class this task
// has paid for repeatedly -- and paying for it INSIDE the paragraph that documents
// a blind spot is the worst place to do it, because a reader sizes the risk from it.
//
// This test asserts nothing about the number. It asserts that the blind spot is NOT
// EMPTY -- if it ever were, the limit paragraph should be deleted rather than left
// describing a gap that closed -- and it prints today's figure.
func TestPolicyBranchDerivation_ReportsTheSizeOfItsOwnBlindSpot(t *testing.T) {
	files, arms := templSwitchArms(t, "../../web/templates")
	if arms == 0 {
		t.Fatal("no templ template uses a switch any more. The blind spot this test sizes " +
			"is empty, so the limit paragraph on policyTemplBranches now overstates the " +
			"risk -- delete it rather than leave it describing a gap that closed.")
	}
	sort.Strings(files)
	t.Logf("switch arms invisible to policyTemplBranches: %d, across %d template(s):\n  %s",
		arms, len(files), strings.Join(files, "\n  "))

	// AND THIS TEMPLATE MUST NOT BE ONE OF THEM. The limit is bearable exactly while
	// the file the net guards is written in the dialect the net reads.
	for _, f := range files {
		if strings.HasSuffix(f, "policies.templ") {
			t.Errorf("%s now uses a switch, and policyTemplBranches cannot see its arms -- so "+
				"the fixture-coverage guarantee has a hole in the one file it exists for. "+
				"Either write the branch as an if, or teach the derivation to read a switch.", f)
		}
	}
}
