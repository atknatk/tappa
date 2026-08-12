package tenant

// rulebook_test.go -- the table-based half of M6-09 phase A's read side.
//
// The properties here are the ones a real-Postgres test cannot see, because they
// are about what the ASSEMBLY does with rows rather than about which rows come
// back: the ordering rule, the five states, the derived guardrail order, and the
// two structural guarantees (no switch on a guardrail, no write in this file).
// rulebook_db_test.go carries the half a fake cannot see.

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
)

// testParams are deliberately NOT policy.DefaultParams(): every window differs
// from the shipped fallback, so an assertion that a screen shows the configured
// value can tell "the parameter arrived" from "the default happened to match".
// That is the M5-05 F3 lesson, which is what let the debounce wiring survive an
// audit while doing nothing.
func testParams() policy.Params {
	return policy.Params{
		DebounceWindow:    90 * time.Second,
		FreshnessWindow:   120 * time.Second,
		OccurredAtSkewMax: 48 * time.Hour,
	}
}

func testRulebook(t *testing.T) *Rulebook {
	t.Helper()
	r, err := NewRulebook(stubDatabase{}, testParams(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRulebook: %v", err)
	}
	return r
}

// stubDatabase satisfies Database for the assembly tests, which never reach it.
// A call would be a bug in the test rather than in the code, so it panics.
type stubDatabase struct{}

func (stubDatabase) WithTenant(_ context.Context, _ uuid.UUID, _ db.TxFunc) error {
	panic("rulebook_test: the assembly tests must not touch the database")
}

// TestNewRulebook_RefusesAnOutOfRangeWindow. A screen that printed a window
// outside the ADR 0004 §11 range would be describing a service that cannot run,
// and the constructor is the last place that can say so.
func TestNewRulebook_RefusesAnOutOfRangeWindow(t *testing.T) {
	t.Parallel()
	if _, err := NewRulebook(nil, testParams(), slog.New(slog.DiscardHandler)); err == nil {
		t.Error("NewRulebook accepted a nil database")
	}
	bad := testParams()
	bad.DebounceWindow = time.Duration(policy.DebounceMaxSeconds+1) * time.Second
	if _, err := NewRulebook(stubDatabase{}, bad, slog.New(slog.DiscardHandler)); err == nil {
		t.Errorf("NewRulebook accepted a debounce window of %v, outside [%d, %d] s",
			bad.DebounceWindow, policy.DebounceMinSeconds, policy.DebounceMaxSeconds)
	}
}

// TestGuardrail_CarriesNoSwitch is the M6-09 card's first trap made STRUCTURAL.
//
// 🔴 THE CARD SAYS THE OFF CONTROL IS NOT ON THE SCREEN -- "not merely disabled,
// absent" -- and a comment asking the next author not to add one is worth nothing.
// This reflects over the type the template renders and refuses any field a switch
// could be built from: a bool, or a name that reads like one.
//
// ⚠️ WHAT IT DOES NOT DO, MEASURED RATHER THAN IMPLIED. An earlier version of this
// comment said "a type with no field for it cannot be persuaded otherwise later",
// and an auditor falsified it in one line: adding `State string` to Guardrail left
// this test GREEN, because a plain string with an innocuous name is not a shape
// reflection can refuse. So this is the FIRST of three walls and the weakest of
// them: it refuses the obvious shapes (and now the state-like names too), the view
// type refuses everything that is not a string
// (handler.TestPolicyGuardrailView_CarriesNoSwitch), and the one that actually
// caught the auditor is the third -- an ALLOW-LIST over the rendered markup
// (handler.TestPoliciesSection_RendersOnlyInertMarkup), which is the only one that
// can see a control the template hard-codes with no model field at all.
//
// IT IS NOT VACUOUS: the negative control below is a struct with exactly the field
// this forbids, and the same check flags it.
func TestGuardrail_CarriesNoSwitch(t *testing.T) {
	t.Parallel()
	type withSwitch struct {
		Sid     string
		Enabled bool
	}
	if bad := switchLikeFields(reflect.TypeOf(withSwitch{})); len(bad) == 0 {
		t.Fatal("the negative control was not flagged, so this check cannot fail: it " +
			"would pass over a Guardrail with an Enabled field")
	}
	if bad := switchLikeFields(reflect.TypeOf(Guardrail{})); len(bad) > 0 {
		t.Errorf("tenant.Guardrail carries %v. A guardrail has no off switch anywhere in "+
			"the product (CLAUDE.md §4, ADR 0004 §4); a field here is one a template can "+
			"render, and a control a customer cannot move costs more trust than no control "+
			"at all (M6-09 card).", bad)
	}
	// The same must hold for whatever the template is actually handed. The view
	// type lives in web/templates/pages and is checked there
	// (TestPolicyGuardrailView_CarriesNoSwitch), because this package must not
	// import the presentation layer.
}

// switchLikeFields returns the fields of t that could become an on/off control:
// any bool, and any field whose name reads like one.
func switchLikeFields(t reflect.Type) []string {
	var bad []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Type.Kind() == reflect.Bool {
			bad = append(bad, f.Name+" (bool)")
			continue
		}
		lower := strings.ToLower(f.Name)
		// 🔴 "state" AND "status" ARE ON THIS LIST BECAUSE AN AUDITOR ADDED
		// `State string` AND THIS TEST STAYED GREEN. A guardrail HAS no state -- it is
		// structure, not a setting -- so a field claiming one is either meaningless or
		// the first half of a control, and both are worth refusing. What actually
		// caught the auditor's field was the rendered-markup allow-list two layers
		// out; that is the wall that holds, and this one is now closer to matching its
		// own claim.
		for _, word := range []string{"enable", "disable", "toggle", "switch", "on", "off",
			"active", "state", "status", "control", "href", "action", "form"} {
			if lower == word || strings.HasPrefix(lower, word) {
				bad = append(bad, f.Name)
				break
			}
		}
	}
	return bad
}

// TestGuardrails_AreTheShippedSliceInTheShippedOrder.
//
// 🔴 THE ORDER IS A SECURITY BOUNDARY (ADR 0004 §5) AND IT HAS MOVED ONCE: ADR
// 0007 renumbered four guardrails after a measurement showed the timing rules were
// deleting §5 row 4's security alert. A screen carrying its own copy of the order
// would have gone on printing the old one. This asserts the screen's list is the
// shipped slice, position for position -- so a filter, a sort or an insertion in
// the panel's rendering is red rather than merely wrong.
func TestGuardrails_AreTheShippedSliceInTheShippedOrder(t *testing.T) {
	t.Parallel()
	shipped := policy.Guardrails(testParams())
	got := testRulebook(t).guardrails()
	if len(got) != len(shipped) {
		t.Fatalf("the screen lists %d guardrail(s), the engine ships %d", len(got), len(shipped))
	}
	// ANTI-VACUITY: an empty engine slice would make every comparison below pass.
	if len(shipped) < 5 {
		t.Fatalf("policy.Guardrails returned %d entries; the engine ships more, so this "+
			"test is reading the wrong thing", len(shipped))
	}
	for i, g := range got {
		if g.Order != i+1 {
			t.Errorf("guardrail %d has Order %d; positions must be 1..N in slice order, "+
				"because the number on the screen is what a manager matches against a "+
				"refused record", i, g.Order)
		}
		if g.Sid != shipped[i].Sid {
			t.Errorf("position %d: screen says %q, engine says %q", i+1, g.Sid, shipped[i].Sid)
		}
		if g.Effect != string(shipped[i].Effect) || g.Reason != shipped[i].Reason {
			t.Errorf("%s: screen shows (%s, %q), engine has (%s, %q)",
				g.Sid, g.Effect, g.Reason, shipped[i].Effect, shipped[i].Reason)
		}
	}
}

// TestGuardrailRules_CoverTheShippedSlice holds the "why it cannot be switched
// off" sentences to the shipped guardrails in BOTH directions. A lock with no
// reason is the card's trap one step removed; a reason for a guardrail that no
// longer ships is a rule a reader would believe is running.
func TestGuardrailRules_CoverTheShippedSlice(t *testing.T) {
	t.Parallel()
	shipped := map[string]bool{}
	for _, g := range policy.Guardrails(testParams()) {
		shipped[g.Sid] = true
		if strings.TrimSpace(guardrailRules[g.Sid]) == "" {
			t.Errorf("%s ships but has no reason text: the screen would draw a lock and "+
				"say nothing about what it protects", g.Sid)
		}
	}
	if len(shipped) < 5 {
		t.Fatalf("read %d shipped guardrail(s); there are more", len(shipped))
	}
	for sid := range guardrailRules {
		if !shipped[sid] {
			t.Errorf("guardrailRules explains %q, which the engine no longer ships", sid)
		}
	}
}

// TestBoundedParams_CoverEveryWindowTheEngineHolds ties the tunable rows on the
// screen to policy.Params by COUNTING ITS FIELDS.
//
// 🔴 A FOURTH BOUNDED WINDOW ADDED TO THE ENGINE MUST NOT BE ABLE TO GO UNSHOWN.
// This section exists to answer "what can I actually change", so a window the
// engine tunes and the screen omits is the one a customer would never find. The
// tie is a field count rather than a list, so it cannot be satisfied by adding a
// name to a list nobody checks.
func TestBoundedParams_CoverEveryWindowTheEngineHolds(t *testing.T) {
	t.Parallel()
	r := testRulebook(t)
	bounded := 0
	for _, g := range r.guardrails() {
		if g.Bounded != nil {
			bounded++
		}
	}
	if want := reflect.TypeOf(policy.Params{}).NumField(); bounded != want {
		t.Errorf("the screen shows %d tunable window(s); policy.Params holds %d. Every "+
			"window the engine tunes belongs on the screen that lists what a customer may "+
			"change (ADR 0004 §11).", bounded, want)
	}
}

// TestBoundedParams_ShowTheWindowsTheEngineWasGiven. The current value must come
// from the Params handed in, not from policy.DefaultParams() -- hand-off N3 was
// exactly this: a configured window that never reached the guardrail, so the
// shipped constant decided and nothing said so. testParams() differs from the
// defaults in all three, so a getter that fell back is red here.
func TestBoundedParams_ShowTheWindowsTheEngineWasGiven(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"sys:person-debounce":   "90 s",
		"sys:tap-freshness":     "2 min",
		"sys:occurred-at-bound": "48 h",
	}
	seen := 0
	for _, g := range testRulebook(t).guardrails() {
		if g.Bounded == nil {
			continue
		}
		seen++
		w, ok := want[g.Sid]
		if !ok {
			t.Errorf("%s carries a tunable window and this test does not know it; add the "+
				"expected value rather than widening the check", g.Sid)
			continue
		}
		if g.Bounded.Current != w {
			t.Errorf("%s shows %q, want %q (the value the engine was given)", g.Sid, g.Bounded.Current, w)
		}
		if g.Bounded.Min == "" || g.Bounded.Max == "" {
			t.Errorf("%s shows a window with no range; the pattern is 'cannot be switched "+
				"off, MAY be tuned inside a range' and half of it is a worse answer than "+
				"neither", g.Sid)
		}
	}
	if seen != len(want) {
		t.Errorf("found %d tunable window(s), expected %d", seen, len(want))
	}
}

// TestActionScopedGuardrails_AreTheOnesTheEngineScopesByAction pins the
// derivation AND today's answer.
//
// TWO GUARDRAILS ARE ACTION GATES TODAY and the pairing is the substance of the
// authorization section: policy:edit is owner-only, which is the ONE thing a
// manager cannot do (ADR 0004 K2). If the derivation broke and returned nothing,
// the screen would quietly stop saying so.
func TestActionScopedGuardrails_AreTheOnesTheEngineScopesByAction(t *testing.T) {
	t.Parallel()
	got := testRulebook(t).actionScopedGuardrails()
	want := map[string]string{
		string(policy.ActionTapRecord):  "sys:no-session",
		string(policy.ActionPolicyEdit): "sys:policy-edit-owner-only",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("action-scoped guardrails = %v, want %v. These are the guardrails that "+
			"fire on an EMPTY context for exactly one action; a change here is a change to "+
			"which authorities the screen says are gated in code.", got, want)
	}
}

// TestAuthorities_SplitOnTheEngineDefaultRatherThanOnTheName.
//
// 🔴 ADR 0004 §3'S RULE IS "TAP RECORDING FAILS TO REVIEW, EVERY OTHER ACTION
// FAILS CLOSED", AND THE PREFIX IS A TRAP: tap:approve carries tap: and is an
// authorization action. The M3-04 card had to correct that misreading in writing.
// The screen splits on the answer the ENGINE gives, so it cannot repeat it.
func TestAuthorities_SplitOnTheEngineDefaultRatherThanOnTheName(t *testing.T) {
	t.Parallel()
	got := testRulebook(t).authorities(nil, nil)
	if len(got) != len(policy.Actions()) {
		t.Fatalf("the screen lists %d action(s), the engine knows %d", len(got), len(policy.Actions()))
	}
	failClosed := 0
	for _, a := range got {
		want := string(policy.Evaluate(policy.Set{}, policy.Context{Action: policy.Action(a.Action)}).Effect)
		if a.Default != want {
			t.Errorf("%s: screen says the default is %q, the engine answers %q", a.Action, a.Default, want)
		}
		if a.FailClosed != (want == string(policy.EffectDeny)) {
			t.Errorf("%s: FailClosed=%v with default %q", a.Action, a.FailClosed, want)
		}
		if a.FailClosed {
			failClosed++
		}
		if len(a.Grants) != 0 {
			t.Errorf("%s: with no stored policy at all the screen carries %d grant(s); a "+
				"tenant with nothing materialised is in the fail-closed state and must be "+
				"shown as such", a.Action, len(a.Grants))
		}
	}
	// tap:approve is the case the prefix would get wrong.
	for _, a := range got {
		if a.Action == string(policy.ActionTapApprove) && !a.FailClosed {
			t.Error("tap:approve was sorted with the recording actions because of its prefix; " +
				"ADR 0004 §3 puts it under the fail-closed defaults")
		}
	}
	if failClosed != len(policy.Actions())-1 {
		t.Errorf("%d of %d actions fail closed; ADR 0004 §3 makes exactly one (tap recording) "+
			"fail to review", failClosed, len(policy.Actions()))
	}
}

// TestAuthorities_ReadTheStoredDocumentsRatherThanTheShippedBaseline.
//
// A tenant who switched the authorization document OFF, or never had it
// materialised, has no grants at all -- and reading policy.Baseline() would show
// them a set of authorities they do not have. That is the ADR 0004 failure mode
// with the worst consequence: "my rule works" when it does not.
func TestAuthorities_ReadTheStoredDocumentsRatherThanTheShippedBaseline(t *testing.T) {
	t.Parallel()
	r := testRulebook(t)
	authz := authzDocName(t)

	on := r.assemble(setRows(t, rowSpec{name: authz, enabled: true}), nil, nil)
	off := r.assemble(setRows(t, rowSpec{name: authz, enabled: false}), nil, nil)

	grantsOn := grantsFor(on.Authorities, string(policy.ActionPolicyEdit))
	if len(grantsOn) == 0 {
		t.Fatalf("with the authorization document stored and ON, nobody is granted %s; "+
			"the fixture or the reader is wrong", policy.ActionPolicyEdit)
	}
	if got := grantsFor(off.Authorities, string(policy.ActionPolicyEdit)); len(got) != 0 {
		t.Errorf("with the SAME document switched off the screen still grants %s to %v. "+
			"Disabling is the tenant's off switch (00007) and the engine honours it; a "+
			"screen that does not would tell a customer they have an authority they lost.",
			policy.ActionPolicyEdit, got)
	}
}

func grantsFor(auths []Authority, action string) []string {
	for _, a := range auths {
		if a.Action != action {
			continue
		}
		var out []string
		for _, g := range a.Grants {
			out = append(out, g.Role)
		}
		return out
	}
	return nil
}

// TestAssemble_KeepsTheShippedBaselineOrderWhateverTheRowsSay is T4: the row
// order is NOT the reading order.
//
// 🔴 ListPolicySet COMES BACK ORDERED BY A RANDOM uuid -- its own comment says so,
// and checkin re-orders against the canonical list for the same reason. A screen
// that trusted the query would reshuffle a customer's rules between page loads,
// and a test that fed rows in the canonical order would stay green while it did.
// So the fixture is REVERSED on purpose.
func TestAssemble_KeepsTheShippedBaselineOrderWhateverTheRowsSay(t *testing.T) {
	t.Parallel()
	shipped := policy.Baseline()
	specs := make([]rowSpec, 0, len(shipped))
	for i := len(shipped) - 1; i >= 0; i-- {
		specs = append(specs, rowSpec{name: shipped[i].Name, enabled: true})
	}
	got := testRulebook(t).assemble(setRows(t, specs...), nil, nil)
	if len(got.Baseline) != len(shipped) {
		t.Fatalf("assembled %d baseline rule(s), the binary ships %d", len(got.Baseline), len(shipped))
	}
	for i, rule := range got.Baseline {
		if rule.Name != shipped[i].Name {
			t.Errorf("position %d: screen shows %q, internal/policy declares %q. The reading "+
				"order must come from the shipped list, not from the rows.",
				i, rule.Name, shipped[i].Name)
		}
	}
}

// TestAssemble_TellsTheFiveStatesApart. Three of them look the same from a
// distance -- "not running" -- and each calls for a different act, which is the
// whole reason ListPolicyVersions outer-joins where ListPolicySet does not.
func TestAssemble_TellsTheFiveStatesApart(t *testing.T) {
	t.Parallel()
	shipped := policy.Baseline()
	if len(shipped) < 4 {
		t.Fatalf("the binary ships %d baseline document(s); this table needs at least four", len(shipped))
	}
	active, off, unreadable, half := shipped[0].Name, shipped[1].Name, shipped[2].Name, shipped[3].Name
	// The fifth (not provisioned) is every OTHER shipped document: no row at all.

	set := setRows(t,
		rowSpec{name: active, enabled: true},
		rowSpec{name: off, enabled: false},
		rowSpec{name: unreadable, enabled: true, raw: []byte(`{"version":"x","name":"x","statements":[{"sid":"x","effect":"nonsense","action":["tap:record"],"resource":["*"]}]}`)},
	)
	halfID := uuid.New()
	versions := []store.ListPolicyVersionsRow{{
		PolicyID: halfID, Name: half, Layer: string(policy.LayerBaseline), Enabled: true,
	}}

	got := testRulebook(t).assemble(set, versions, nil)
	states := map[string]RuleState{}
	for _, r := range got.Baseline {
		states[r.Name] = r.State
	}
	for _, tc := range []struct {
		name string
		want RuleState
	}{
		{active, StateActive},
		{off, StateOff},
		{unreadable, StateUnreadable},
		{half, StateNoDocument},
		{shipped[len(shipped)-1].Name, StateNotProvisioned},
	} {
		if got := states[tc.name]; got != tc.want {
			t.Errorf("%q: state %q, want %q", tc.name, got, tc.want)
		}
	}
	for _, r := range got.Baseline {
		if r.State == StateUnreadable && strings.TrimSpace(r.Problem) == "" {
			t.Errorf("%q is unreadable and the screen says nothing about why; an unreadable "+
				"document is one the engine is IGNORING, and a customer who is not told "+
				"believes their rule is running", r.Name)
		}
		if r.State == StateActive && len(r.Statements) == 0 {
			t.Errorf("%q is active but shows no statements: a rule nobody can read is one "+
				"nobody can check", r.Name)
		}
	}
}

// TestAssemble_ShowsAStoredBaselineOlderThanTheShippedOne. Existing tenants are
// deliberately NOT auto-updated (a silent baseline change would rewrite why past
// taps were flagged -- policy.BaselineVersion), so the two stamps differing is the
// DESIGNED state and the screen has to be able to say it.
func TestAssemble_ShowsAStoredBaselineOlderThanTheShippedOne(t *testing.T) {
	t.Parallel()
	shipped := policy.Baseline()[0]
	doc := shipped.Document
	doc.Version = "2020-01-01"
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := testRulebook(t).assemble(setRows(t, rowSpec{name: shipped.Name, enabled: true, raw: raw}), nil, nil)
	rule := got.Baseline[0]
	if rule.StoredStamp != "2020-01-01" {
		t.Errorf("stored stamp = %q, want the stamp inside the STORED document", rule.StoredStamp)
	}
	if rule.ShippedStamp != policy.BaselineVersion {
		t.Errorf("shipped stamp = %q, want %q", rule.ShippedStamp, policy.BaselineVersion)
	}
	if rule.State != StateActive {
		t.Errorf("an older accepted baseline is still RUNNING; state = %q", rule.State)
	}
}

// TestAssemble_HistoryIsNewestFirstAndNamesItsAuthors -- the three author cases,
// which exist because policy_versions.created_by is an FK-LESS uuid.
func TestAssemble_HistoryIsNewestFirstAndNamesItsAuthors(t *testing.T) {
	t.Parallel()
	shipped := policy.Baseline()[0]
	set := setRows(t, rowSpec{name: shipped.Name, enabled: true})
	id := set[0].PolicyID
	admin, ghost := uuid.New(), uuid.New()
	name := "Rita Camilleri"
	at := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	v3, v2, v1 := int32(3), int32(2), int32(1)
	vid := uuid.New()
	versions := []store.ListPolicyVersionsRow{
		{PolicyID: id, Name: shipped.Name, Layer: string(policy.LayerBaseline), Enabled: true,
			VersionID: &vid, VersionNo: &v3, VersionCreatedAt: &at, CreatedBy: &admin, AuthorName: &name, DocumentBytes: 400},
		{PolicyID: id, Name: shipped.Name, Layer: string(policy.LayerBaseline), Enabled: true,
			VersionID: &vid, VersionNo: &v2, VersionCreatedAt: &at, CreatedBy: &ghost, DocumentBytes: 380},
		{PolicyID: id, Name: shipped.Name, Layer: string(policy.LayerBaseline), Enabled: true,
			VersionID: &vid, VersionNo: &v1, VersionCreatedAt: &at, DocumentBytes: 360},
	}
	got := testRulebook(t).assemble(set, versions, nil).Baseline[0]
	if got.HistoryTotal != 3 {
		t.Fatalf("history total = %d, want 3", got.HistoryTotal)
	}
	want := []struct {
		no     int
		author string
		cur    bool
	}{
		{3, name, true},
		{2, AuthorUnresolved, false},
		{1, AuthorSystem, false},
	}
	for i, w := range want {
		if got.History[i].No != w.no || got.History[i].Author != w.author || got.History[i].Current != w.cur {
			t.Errorf("history[%d] = (%d, %q, current=%v), want (%d, %q, current=%v)",
				i, got.History[i].No, got.History[i].Author, got.History[i].Current, w.no, w.author, w.cur)
		}
	}
}

// TestAssemble_ShortensALongHistoryAndSaysSo. A rule with two hundred versions
// must not bury the rules, and a shortened strip must not read as the whole story.
func TestAssemble_ShortensALongHistoryAndSaysSo(t *testing.T) {
	t.Parallel()
	shipped := policy.Baseline()[0]
	set := setRows(t, rowSpec{name: shipped.Name, enabled: true})
	id := set[0].PolicyID
	at := time.Now().UTC()
	vid := uuid.New()
	total := versionsShownPerPolicy * 3
	var versions []store.ListPolicyVersionsRow
	for n := total; n >= 1; n-- {
		no := int32(n)
		versions = append(versions, store.ListPolicyVersionsRow{
			PolicyID: id, Name: shipped.Name, Layer: string(policy.LayerBaseline), Enabled: true,
			VersionID: &vid, VersionNo: &no, VersionCreatedAt: &at, DocumentBytes: 300,
		})
	}
	got := testRulebook(t).assemble(set, versions, nil).Baseline[0]
	if len(got.History) != versionsShownPerPolicy {
		t.Errorf("shown %d version(s), want %d", len(got.History), versionsShownPerPolicy)
	}
	if got.HistoryTotal != total {
		t.Errorf("history total = %d, want %d -- the count must be the real one so the "+
			"screen can say the strip is short", got.HistoryTotal, total)
	}
	if got.History[0].No != total {
		t.Errorf("the strip starts at version %d, want the newest (%d)", got.History[0].No, total)
	}
}

// TestConditions_AreOrderedDeterministically. A condition block is two nested Go
// maps and Go randomises map iteration, so an un-sorted rendering would reshuffle
// a customer's rules between page loads -- and would make any test that reads the
// page flaky in the way that invites the wrong repair (M3-04's trap).
func TestConditions_AreOrderedDeterministically(t *testing.T) {
	t.Parallel()
	cond := policy.Condition{
		policy.OpBool:         {policy.CtxTapIPMatch: false, policy.CtxTapGPSMatch: true},
		policy.OpStringEquals: {policy.CtxTapChannel: "qr", policy.CtxActorRole: "owner"},
	}
	first := conditionsOf(cond)
	if len(first) != 4 {
		t.Fatalf("flattened %d condition(s), want 4", len(first))
	}
	for i := 0; i < 50; i++ {
		if got := conditionsOf(cond); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d produced a different order:\n got %v\nwant %v", i, got, first)
		}
	}
}

// TestDisplayValue_PrintsNumbersTheWayAManagerWroteThem. Every number arrives from
// encoding/json as a float64, so the threshold a customer typed as 120 must not
// come back as 1.2e+02.
func TestDisplayValue_PrintsNumbersTheWayAManagerWroteThem(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   any
		want string
	}{
		{float64(120), "120"},
		{float64(0), "0"},
		{float64(1.5), "1.5"},
		{float64(259200), "259200"},
		{true, "true"},
		{"qr", "qr"},
		{[]any{"10.0.0.0/8", "192.168.0.0/16"}, "10.0.0.0/8, 192.168.0.0/16"},
		{nil, ""},
	} {
		if got := displayValue(tc.in); got != tc.want {
			t.Errorf("displayValue(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRulebook_IssuesOnlySelects is T3 made mechanical.
//
// 🔴 THE OTHER ASSEMBLER OF A POLICY SET WRITES. internal/domain/checkin's
// policySets materialises the managed baseline when it is missing, and it is right
// to: a tap that cannot name a policy_versions row cannot be recorded (§4.6). The
// panel has no such need, and acquiring one would mean a page view INSERTing a
// policy, a version and an attachment per shipped document into a tenant somebody
// merely looked at -- into a table that is append-only, so the rows could never be taken
// back. This parses rulebook.go, collects every sqlc query it names, and reads
// each one's SQL out of db/queries: the first keyword must be SELECT.
//
// IT READS THE SQL RATHER THAN THE NAME, because Ensure* is a convention and
// conventions do not fail builds.
func TestRulebook_IssuesOnlySelects(t *testing.T) {
	t.Parallel()
	declared := declaredQueries(t)
	named := queriesNamedInFile(t, "rulebook.go", declared)
	// ANTI-VACUITY: a walk that found nothing would report a clean read path.
	if len(named) < 3 {
		t.Fatalf("rulebook.go names %d store quer(y/ies) (%v); it makes three reads, so "+
			"this scan is reading the wrong file", len(named), named)
	}
	for _, name := range named {
		file, body, ok := findQuery(t, name)
		if !ok {
			t.Errorf("rulebook.go names %q, which no file in db/queries declares", name)
			continue
		}
		first := firstKeyword(body)
		if first != "SELECT" && first != "WITH" {
			t.Errorf("rulebook.go calls %s (%s), whose statement starts with %s. The panel's "+
				"policy screen must not write: a page view would append rows to "+
				"policy_versions, which nothing can delete.", name, file, first)
		}
	}
	t.Logf("read-only check covered: %s", strings.Join(named, ", "))
}

// TestRulebookSelectCheck_IsNotVacuous is the negative control for the check
// above: pointed at a file that DOES write, it must flag it.
func TestRulebookSelectCheck_IsNotVacuous(t *testing.T) {
	t.Parallel()
	if got := firstKeyword("\n\nINSERT INTO policies (id) VALUES ($1)\n"); got != "INSERT" {
		t.Errorf("firstKeyword returned %q for an INSERT; the read-only check would pass "+
			"over a write", got)
	}
	if got := firstKeyword("  \n  select 1\n"); got != "SELECT" {
		t.Errorf("firstKeyword returned %q for a lowercase select", got)
	}
}

// firstKeyword returns the first SQL keyword of a query body, upper-cased.
//
// ⚠️ IT SKIPS A LEADING sqlc ANNOTATION, and that is a measured correction rather
// than defensive coding. queryBody's marker regexp consumes `-- name: X ` and
// leaves the `:many` that follows it on the first line, so the first draft of this
// check reported EVERY query as starting with ":MANY" -- three false alarms, and
// the kind that trains a reader to widen the check until it stops seeing anything.
func firstKeyword(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, ":") {
				continue
			}
			return strings.ToUpper(field)
		}
	}
	return ""
}

// queriesNamedInFile is storeQueryNames narrowed to ONE file of this package.
func queriesNamedInFile(t *testing.T, file string, declared map[string]bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	seen := map[string]bool{}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if declared[sel.Sel.Name] && !seen[sel.Sel.Name] {
			seen[sel.Sel.Name] = true
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}

// --- fixtures ---------------------------------------------------------------

// rowSpec describes one stored policy for the assembly tests.
type rowSpec struct {
	name    string
	enabled bool
	// raw overrides the document; empty means "the shipped one for this name".
	raw []byte
}

// setRows builds ListPolicySet rows for the named SHIPPED baseline documents,
// with fresh ids, in the order given.
func setRows(t *testing.T, specs ...rowSpec) []store.ListPolicySetRow {
	t.Helper()
	shipped := map[string]policy.BaselineDoc{}
	for _, d := range policy.Baseline() {
		shipped[d.Name] = d
	}
	out := make([]store.ListPolicySetRow, 0, len(specs))
	for _, s := range specs {
		raw := s.raw
		if raw == nil {
			doc, ok := shipped[s.name]
			if !ok {
				t.Fatalf("setRows: %q is not a shipped baseline document", s.name)
			}
			var err error
			if raw, err = json.Marshal(doc.Document); err != nil {
				t.Fatalf("marshal %q: %v", s.name, err)
			}
		}
		out = append(out, store.ListPolicySetRow{
			PolicyID: uuid.New(), Name: s.name, Layer: string(policy.LayerBaseline),
			Enabled: s.enabled, VersionID: uuid.New(), VersionNo: 1, Document: raw,
		})
	}
	return out
}

// authzDocName finds the shipped document that grants panel actions, by looking
// for the sid rather than by naming the document -- the name is display copy and
// the sid is the identifier internal/policy exports for exactly this purpose.
func authzDocName(t *testing.T) string {
	t.Helper()
	for _, d := range policy.Baseline() {
		for _, st := range d.Document.Statements {
			if st.Sid == policy.SidAuthzOwner {
				return d.Name
			}
		}
	}
	t.Fatalf("no shipped baseline document carries %s", policy.SidAuthzOwner)
	return ""
}

// TestAssemble_OneUnreadableDocumentReportsTheWholeLayerAsSilenced is the blocking
// property this screen was missing.
//
// 🔴 ONE BROKEN DOCUMENT DROPS THE WHOLE POLICY LAYER, NOT ONE RULE. The tap engine
// reports an unparseable stored baseline document as missing, cannot repair it
// (materialising is an ON CONFLICT DO NOTHING no-op once the version exists), and
// then decides on the guardrails ALONE -- pinned against real Postgres by
// checkin.TestPolicySetDB_ACorruptStoredDocumentFallsBackToGuardrailsAlone, which
// asserts len(set.Baseline) == 0. The first version of this screen flagged the
// broken rule and left the other eight wearing an "In force" chip, and the
// authority section listing grants nobody was applying -- correct on one line,
// wrong on every other line of the page.
func TestAssemble_OneUnreadableDocumentReportsTheWholeLayerAsSilenced(t *testing.T) {
	t.Parallel()
	shipped := policy.Baseline()
	if len(shipped) < 3 {
		t.Fatalf("the binary ships %d baseline document(s); this case needs at least three", len(shipped))
	}
	specs := make([]rowSpec, 0, len(shipped))
	for i, doc := range shipped {
		spec := rowSpec{name: doc.Name, enabled: true}
		if i == 0 {
			spec.raw = []byte(`{}`) // legal jsonb, unusable document -- the shape that exists in the wild
		}
		specs = append(specs, spec)
	}
	r := testRulebook(t)

	broken := r.assemble(setRows(t, specs...), nil, nil)
	if !broken.GuardrailsOnly {
		t.Fatal("with one unreadable baseline document the screen does not report that the " +
			"engine is running on guardrails alone; every other rule on the page then reads " +
			"as if it were deciding, which is the failure ADR 0004 calls the most dangerous " +
			"there is")
	}
	if len(broken.Unreadable) != 1 || broken.Unreadable[0] != shipped[0].Name {
		t.Errorf("Unreadable = %v, want exactly %q so the notice can point at it",
			broken.Unreadable, shipped[0].Name)
	}
	// The OTHER rules are still reported as stored and valid -- the flag is about what
	// is DECIDING, not about what is stored, and phase B's controls act on storage.
	active := 0
	for _, rule := range broken.Baseline {
		if rule.State == StateActive {
			active++
		}
	}
	if active != len(shipped)-1 {
		t.Errorf("%d rule(s) read as active beside the broken one, want %d: the storage really "+
			"is fine and the screen must not pretend otherwise either", active, len(shipped)-1)
	}

	// THE CONTROL: with every document readable the flag is off. Without this the
	// assertion above would be satisfied by a field that is always true.
	whole := r.assemble(setRows(t, specsAllGood(shipped)...), nil, nil)
	if whole.GuardrailsOnly || len(whole.Unreadable) != 0 {
		t.Errorf("a complete, readable baseline reports GuardrailsOnly=%v unreadable=%v",
			whole.GuardrailsOnly, whole.Unreadable)
	}
}

func specsAllGood(docs []policy.BaselineDoc) []rowSpec {
	out := make([]rowSpec, 0, len(docs))
	for _, d := range docs {
		out = append(out, rowSpec{name: d.Name, enabled: true})
	}
	return out
}

// TestAssemble_ACappedReadDoesNotClaimARuleWasNeverSetUp.
//
// 🔴 THE CAP CAN FABRICATE A STATE. ListPolicyVersions is limited over the WHOLE
// tenant, and a container with no version is ONE row in that stream -- so a
// truncated read drops it, containersByName has nothing, and the rule prints as
// "never set up" when it is really "set up with no rules in it". By this package's
// own argument those two need different acts, so under a cap the honest answer is
// that the state is unknown.
func TestAssemble_ACappedReadDoesNotClaimARuleWasNeverSetUp(t *testing.T) {
	t.Parallel()
	r := testRulebook(t)
	// Enough version rows to trip the cap, all belonging to a policy that is not one
	// of the shipped documents -- which is exactly the state that pushes a real
	// container out of the window.
	noise := make([]store.ListPolicyVersionsRow, 0, policyRowLimit+1)
	at := time.Now().UTC()
	for i := 0; i <= policyRowLimit; i++ {
		id, vid, no := uuid.New(), uuid.New(), int32(1)
		noise = append(noise, store.ListPolicyVersionsRow{
			PolicyID: id, Name: "noise", Layer: string(policy.LayerTenant), Enabled: true,
			VersionID: &vid, VersionNo: &no, VersionCreatedAt: &at, DocumentBytes: 10,
		})
	}
	capped := r.assemble(nil, noise, nil)
	if !capped.VersionsCapped {
		t.Fatal("the read did not report its VERSION ceiling as hit, so this case proves nothing")
	}
	for _, rule := range capped.Baseline {
		if rule.State == StateNotProvisioned {
			t.Errorf("%q reads as %q under a capped version read. The row that tells "+
				"'never set up' from 'set up with no rules' may have been truncated, and "+
				"those two need different acts.", rule.Name, rule.State)
		}
		if rule.State != StateUnknown {
			t.Errorf("%q reads as %q under a cap, want %q", rule.Name, rule.State, StateUnknown)
		}
	}
	// CONTROL: without the cap the same absence is reported as the definite state.
	uncapped := r.assemble(nil, nil, nil)
	if uncapped.VersionsCapped {
		t.Fatal("an empty version read reported itself as capped")
	}
	for _, rule := range uncapped.Baseline {
		if rule.State != StateNotProvisioned {
			t.Errorf("%q reads as %q with a complete read, want %q -- the unknown state must "+
				"be reachable ONLY under a cap", rule.Name, rule.State, StateNotProvisioned)
		}
	}
}

// TestAuthorities_GrantsCarryTheirScopeAndTheirRemainingConditions.
//
// 🔴 "role — sid" OVERSTATES. A grant is an IAM statement with a resource list and a
// condition block; the role is only the part of the condition that keys on
// actor:role. A statement fenced to one branch and to one channel printed as a flat
// grant of the whole action, so a customer would believe they had handed out an
// authority they had actually fenced twice.
func TestAuthorities_GrantsCarryTheirScopeAndTheirRemainingConditions(t *testing.T) {
	t.Parallel()
	doc := policy.Document{
		Version: policy.BaselineVersion, Name: "own",
		Statements: []policy.Statement{{
			Sid:      "own:qr-approvals",
			Effect:   policy.EffectAllow,
			Action:   []policy.Action{policy.ActionTapApprove},
			Resource: []string{"location/rusty-bar"},
			Condition: policy.Condition{
				policy.OpStringEquals: {policy.CtxActorRole: "manager", policy.CtxTapChannel: "qr"},
			},
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rows := setRows(t, rowSpec{name: policy.Baseline()[0].Name, enabled: true, raw: raw})
	rows[0].Layer = string(policy.LayerTenant)
	rows[0].Name = "own"

	screen := testRulebook(t).assemble(rows, nil, nil)
	var got AuthorityGrant
	for _, a := range screen.Authorities {
		if a.Action == string(policy.ActionTapApprove) && len(a.Grants) == 1 {
			got = a.Grants[0]
		}
	}
	if got.Sid != "own:qr-approvals" {
		t.Fatalf("the statement granted no authority the screen could see (got %+v)", got)
	}
	if got.Role != "manager" {
		t.Errorf("role = %q, want manager", got.Role)
	}
	if len(got.Resources) != 1 || got.Resources[0] != "location/rusty-bar" {
		t.Errorf("resources = %v; a grant fenced to one branch that prints as unscoped "+
			"describes a wider authority than the statement gives", got.Resources)
	}
	if got.OtherConditions != 1 {
		t.Errorf("OtherConditions = %d, want 1 (the channel predicate). A condition the "+
			"summary cannot name must be COUNTED, not dropped.", got.OtherConditions)
	}
}

// TestAssemble_ASwitchedOffRuleIsOffEvenIfItsDocumentIsUnreadable.
//
// 🔴 THE PANEL MUST CLASSIFY THE WAY THE ENGINE DOES, AND FOR ONE ROUND IT DID NOT.
// storedRule parsed before it read `enabled`, so a policy the tenant had SWITCHED
// OFF whose document also failed to parse came back as StateUnreadable -- which
// raises GuardrailsOnly and puts the page's loudest sentence on a screen where it is
// false. Measured against the same rows: panel said state=unreadable,
// GuardrailsOnly=true; checkin.assembleBaseline said baseline=8, missing=0, i.e.
// eight rules genuinely deciding. The engine tests `!row.Enabled` and continues
// BEFORE parsing, so a disabled document is a deliberate absence that costs nothing
// and is never looked at; this asserts the panel makes the same cut in the same
// order.
func TestAssemble_ASwitchedOffRuleIsOffEvenIfItsDocumentIsUnreadable(t *testing.T) {
	t.Parallel()
	shipped := policy.Baseline()
	specs := make([]rowSpec, 0, len(shipped))
	for i, doc := range shipped {
		spec := rowSpec{name: doc.Name, enabled: true}
		if i == 0 {
			spec.enabled = false
			spec.raw = []byte(`{}`) // switched off AND unreadable
		}
		specs = append(specs, spec)
	}
	got := testRulebook(t).assemble(setRows(t, specs...), nil, nil)
	if got.Baseline[0].State != StateOff {
		t.Errorf("a switched-off rule with an unreadable document reads as %q, want %q. "+
			"The engine never parses a disabled document, so calling it unreadable is the "+
			"panel inventing a problem the engine does not have.", got.Baseline[0].State, StateOff)
	}
	if got.GuardrailsOnly || len(got.Unreadable) != 0 {
		t.Errorf("GuardrailsOnly=%v unreadable=%v. Disabling costs the engine NOTHING "+
			"(assembleBaseline skips it rather than reporting it missing), so the page's "+
			"loudest sentence must not fire.", got.GuardrailsOnly, got.Unreadable)
	}
	// CONTROL: the SAME unreadable document, switched ON, still raises the flag --
	// otherwise this fix would have silently disarmed B1.
	specs[0].enabled = true
	on := testRulebook(t).assemble(setRows(t, specs...), nil, nil)
	if !on.GuardrailsOnly {
		t.Error("with the same unreadable document switched ON the flag no longer fires; " +
			"the enabled check has been placed so early that it swallows the real case")
	}
}

// TestAssemble_OnlyTheVersionCeilingCanFabricateAnUnknownState.
//
// 🔴 ONE FLAG FOR TWO READS HID AN ANSWER THE READER HAD. Hitting the ATTACHMENT
// ceiling used to make every rule report "cannot be determined", although the only
// read that can distinguish "never set up" from "set up with no rules" -- the
// version read -- had come back complete. Safe direction, real loss: this screen
// exists to answer that question.
func TestAssemble_OnlyTheVersionCeilingCanFabricateAnUnknownState(t *testing.T) {
	t.Parallel()
	r := testRulebook(t)
	attachments := make([]store.ListPolicyAttachmentsRow, 0, attachmentRowLimit+1)
	for i := 0; i <= attachmentRowLimit; i++ {
		attachments = append(attachments, store.ListPolicyAttachmentsRow{PolicyID: uuid.New(), Resource: "*"})
	}
	got := r.assemble(nil, nil, attachments)
	if !got.AttachmentsCapped || got.VersionsCapped {
		t.Fatalf("attachmentsCapped=%v versionsCapped=%v; the fixture trips only the "+
			"attachment ceiling", got.AttachmentsCapped, got.VersionsCapped)
	}
	// 🔴 NO COMBINED FLAG IS ASSERTED, BECAUSE THERE IS NO LONGER ONE. The screen
	// renders a notice PER CEILING, each naming the list it shortened; a single
	// "something was capped" boolean was dead the moment that split landed, and this
	// assertion used to be the only thing reading it.
	if !got.AttachmentsCapped {
		t.Error("the attachment ceiling was hit and not reported; the screen would then " +
			"print the 'Bound to' lists as though they were complete")
	}
	for _, rule := range got.Baseline {
		if rule.State != StateNotProvisioned {
			t.Errorf("%q reads as %q with a COMPLETE version read; only a truncated version "+
				"read can leave this reader unable to tell, and it has one", rule.Name, rule.State)
		}
	}
	// CONTROL: the version ceiling still does raise the unknown state, or this split
	// would have quietly disarmed the correction it came from.
	versions := make([]store.ListPolicyVersionsRow, 0, policyRowLimit+1)
	at := time.Now().UTC()
	for i := 0; i <= policyRowLimit; i++ {
		id, vid, no := uuid.New(), uuid.New(), int32(1)
		versions = append(versions, store.ListPolicyVersionsRow{
			PolicyID: id, Name: "noise", Layer: string(policy.LayerTenant), Enabled: true,
			VersionID: &vid, VersionNo: &no, VersionCreatedAt: &at, DocumentBytes: 10,
		})
	}
	capped := r.assemble(nil, versions, nil)
	if !capped.VersionsCapped || capped.Baseline[0].State != StateUnknown {
		t.Errorf("versionsCapped=%v state=%q; the version ceiling must still admit "+
			"ignorance", capped.VersionsCapped, capped.Baseline[0].State)
	}
}
