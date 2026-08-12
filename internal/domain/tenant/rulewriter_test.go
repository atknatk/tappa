package tenant

// rulewriter_test.go -- the properties the GATE rests on, measured without a
// database because they are properties of the ENGINE rather than of any rulebook.

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/policy"
)

// fixedAuthority is a PolicyAuthority that hands back whatever Set the test names.
type fixedAuthority struct{ set policy.Set }

func (f fixedAuthority) StoredSet(context.Context, uuid.UUID) policy.Set { return f.set }

// errDatabaseReached is what a write returns when it got past the gate.
//
// 🔴 IT IS AN ERROR RATHER THAN A PANIC (rulebook_test.go's stubDatabase panics)
// BECAUSE THE DISTINCTION IS THE MEASUREMENT. This file needs to tell "refused before
// the database" from "reached the database and then failed", and a panic makes the
// second unobservable.
var errDatabaseReached = errors.New("test: the write reached the database")

type refusingDatabase struct{}

func (refusingDatabase) WithTenant(_ context.Context, _ uuid.UUID, _ db.TxFunc) error {
	return errDatabaseReached
}

// stubTrail is never reached: every call in this file is refused before a
// transaction is opened, or fails when one is.
type stubTrail struct{}

func (stubTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, errors.New("test: the trail must not be reached here")
}

func testWriter(t *testing.T, set policy.Set) *RuleWriter {
	t.Helper()
	w, err := NewRuleWriter(refusingDatabase{}, stubTrail{}, fixedAuthority{set: set},
		testParams(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRuleWriter: %v", err)
	}
	return w
}

// TestRuleWriter_AGuardrailDenyIsTerminalWhateverIsStored.
//
// 🔴 THIS IS THE PROPERTY THE GATE'S FIRST STAGE RESTS ON, AND IT IS THE ENGINE'S,
// NOT THIS PACKAGE'S. authorise asks the guardrails ALONE first, and treats a
// guardrail match as final -- which is only sound because policy.Evaluate walks the
// guardrails before anything else and returns on the first match. If that ever
// stopped being true, a manager could be refused by stage 1 while the full rulebook
// would have allowed them: fail-CLOSED, so the direction is safe, but the sentence
// in authorise's comment would be false and somebody would build on it.
//
// The stored sets below are the ones that would otherwise ALLOW -- the shipped
// authorization document, and a tenant document that grants everything to everyone --
// so a stage-1 short circuit that was not terminal would show up as an allow here.
func TestRuleWriter_AGuardrailDenyIsTerminalWhateverIsStored(t *testing.T) {
	manager := Actor{ID: uuid.New(), Role: "manager"}
	tenantID := uuid.New()

	sets := map[string]policy.Set{
		"nothing stored":       {},
		"the shipped rulebook": {Baseline: shippedPolicies(t)},
		"a tenant document that allows everyone everything": {
			Tenant: []policy.Policy{{
				VersionID: uuid.New(),
				Layer:     policy.LayerTenant,
				Document: policy.Document{
					Version: policy.BaselineVersion, Name: "wide open",
					Statements: []policy.Statement{{
						Sid:      "own:everything",
						Effect:   policy.EffectAllow,
						Action:   []policy.Action{policy.ActionPolicyEdit},
						Resource: []string{"*"},
					}},
				},
			}},
		},
	}
	for name, set := range sets {
		set.Guardrails = policy.Guardrails(testParams())
		t.Run(name, func(t *testing.T) {
			// THE ENGINE'S OWN ANSWER FIRST -- the property, stated directly.
			in := policyEditContext(tenantID, manager)
			if d := policy.Evaluate(set, in); d.Effect == policy.EffectAllow {
				t.Fatalf("the ENGINE allowed a manager policy:edit with %s stored (sid %q). "+
					"sys:policy-edit-owner-only is supposed to deny every non-owner "+
					"terminally; if that has changed, the gate's first stage is unsound.",
					name, d.MatchedSid)
			}
			// AND THE GATE AGREES.
			if testWriter(t, set).May(context.Background(), tenantID, manager) {
				t.Errorf("the gate allowed a manager with %s stored", name)
			}
		})
	}

	// POSITIVE CONTROL: with the SHIPPED rulebook stored, an OWNER is allowed -- so
	// the refusals above are the guardrail rather than a gate that refuses everybody.
	// This is the lockout internal/policy's authzDoc warns about, measured.
	owner := Actor{ID: uuid.New(), Role: "owner"}
	full := policy.Set{Guardrails: policy.Guardrails(testParams()), Baseline: shippedPolicies(t)}
	if !testWriter(t, full).May(context.Background(), tenantID, owner) {
		t.Fatal("an OWNER is refused with the shipped rulebook stored. That is the lockout " +
			"internal/policy's authzDoc comment names: the guardrail only DENIES non-owners, " +
			"so the owner's allow has to come from the stored baseline document.")
	}
	// AND WITH NOTHING STORED THE OWNER IS REFUSED TOO, which is the counted bound the
	// screen has to explain rather than a bug: the allow is a stored document.
	if testWriter(t, policy.Set{Guardrails: policy.Guardrails(testParams())}).
		May(context.Background(), tenantID, owner) {
		t.Error("an owner with NO stored rulebook was allowed; the allow can only come from " +
			"a stored document, so this would mean the gate is answering from a role")
	}
}

// TestRuleWriter_EveryWriteRefusesBeforeItOpensATransaction.
//
// 🔴 THE GATE IS STRUCTURAL, NOT A CONVENTION. refusingDatabase FAILS every call, so a
// method that reached the database would return that failure instead of
// ErrNotPermitted -- which is how this test can tell "refused" from "tried and then
// refused". It walks every exported write, so a seventh method that forgot to
// authorise is red here rather than on a customer's rulebook.
func TestRuleWriter_EveryWriteRefusesBeforeItOpensATransaction(t *testing.T) {
	w := testWriter(t, policy.Set{Guardrails: policy.Guardrails(testParams())})
	manager := Actor{ID: uuid.New(), Role: "manager"}
	tenantID, policyID := uuid.New(), uuid.New()

	acts := map[string]func() error{
		"SetEnabled": func() error {
			_, err := w.SetEnabled(context.Background(), EnableCommand{
				TenantID: tenantID, Actor: manager, PolicyID: policyID, Enabled: false})
			return err
		},
		"Bind": func() error {
			return w.Bind(context.Background(), BindCommand{
				TenantID: tenantID, Actor: manager, PolicyID: policyID, Resource: "*"})
		},
		"Unbind": func() error {
			return w.Unbind(context.Background(), BindCommand{
				TenantID: tenantID, Actor: manager, PolicyID: policyID, Resource: "*"})
		},
		"Author": func() error {
			_, err := w.Author(context.Background(), AuthorCommand{
				TenantID: tenantID, Actor: manager, Name: "x", BasedOn: AuthorableRules()[0].Name})
			return err
		},
	}
	// 🔴 THE MAP IS HELD TO THE TYPE, NOT TRUSTED AS A LIST. This test's own doc said it
	// "walks every exported write"; it walked four entries somebody typed, and a fifth
	// method added tomorrow would be silently uncovered — the N-list class this task
	// keeps paying for. reflect gives the real set; the exemptions are named with a
	// reason, which is an edit somebody has to defend.
	exempt := map[string]string{
		"May":           "asks the gate and returns its answer; it IS the read, not a write",
		"RecordRefusal": "writes only a trail row, and only for an actor the gate has ALREADY refused",
	}
	typ := reflect.TypeOf(w)
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		if _, ok := acts[name]; ok {
			continue
		}
		if why, ok := exempt[name]; ok {
			t.Logf("exempt: %s — %s", name, why)
			continue
		}
		t.Errorf("*RuleWriter exports %s and this test does not drive it. Every write must "+
			"refuse before it opens a transaction; a method nobody drives here is a method "+
			"whose gate nothing checks.", name)
	}

	for name, act := range acts {
		if err := act(); !errors.Is(err, ErrNotPermitted) {
			t.Errorf("%s returned %v for a manager, want ErrNotPermitted. refusingDatabase fails "+
				"every call, so any other error means the method reached the database before "+
				"asking the engine.", name, err)
		}
	}
	// ANTI-VACUITY: the same acts DO reach the database for somebody the engine
	// allows, so the refusals above are the gate rather than a broken constructor.
	allowed := testWriter(t, policy.Set{
		Guardrails: policy.Guardrails(testParams()), Baseline: shippedPolicies(t),
	})
	owner := Actor{ID: uuid.New(), Role: "owner"}
	_, err := allowed.SetEnabled(context.Background(), EnableCommand{
		TenantID: tenantID, Actor: owner, PolicyID: policyID, Enabled: false})
	if !errors.Is(err, errDatabaseReached) {
		t.Errorf("an ALLOWED owner's SetEnabled returned %v, want the database's own failure. "+
			"With a database that refuses every call, anything else means the gate turned the "+
			"owner away too -- and this test could not tell a gate from a wall.", err)
	}
}

// TestLockoutCheck_SaysWhichOfTHOSEHappened.
//
// 🔴 THE RARE ARM IS TRIGGERED RATHER THAN READ. lockoutCheck has two outcomes and
// they need OPPOSITE sentences: the change was put back, or the change STANDS and the
// author really is locked out. One sentinel served both for a round, and the screen's
// message for it says "so it was put back" — exactly wrong on the second, which is
// the half where somebody needs to act.
func TestLockoutCheck_SaysWhichOfTHOSEHappened(t *testing.T) {
	// An authority that refuses everybody, so the post-change re-ask always denies.
	w := testWriter(t, policy.Set{Guardrails: policy.Guardrails(testParams())})
	owner := Actor{ID: uuid.New(), Role: "owner"}
	tenantID := uuid.New()

	// THE UNDO WORKS: the change was put back.
	undone := false
	err := w.lockoutCheck(context.Background(), tenantID, owner, func(context.Context) error {
		undone = true
		return nil
	})
	if !errors.Is(err, ErrWouldLockOut) || errors.Is(err, ErrLockoutStands) {
		t.Errorf("a successful undo returned %v, want ErrWouldLockOut and NOT ErrLockoutStands", err)
	}
	if !undone {
		t.Error("the undo was never called")
	}

	// THE UNDO FAILS: the change stands.
	err = w.lockoutCheck(context.Background(), tenantID, owner, func(context.Context) error {
		return errors.New("the undo could not be written")
	})
	if !errors.Is(err, ErrLockoutStands) {
		t.Fatalf("a FAILED undo returned %v, want ErrLockoutStands. The screen's message for "+
			"ErrWouldLockOut says the change was put back, which is the one sentence that "+
			"would send an owner away believing the opposite of what happened.", err)
	}
	if errors.Is(err, ErrWouldLockOut) {
		t.Error("ErrLockoutStands also matches ErrWouldLockOut, so a handler switching on the " +
			"first arm it finds would print the wrong sentence anyway")
	}

	// AND NEITHER FIRES FOR SOMEBODY THE ENGINE STILL ALLOWS -- the positive control,
	// or the two cases above are a function that always refuses.
	allowed := testWriter(t, policy.Set{
		Guardrails: policy.Guardrails(testParams()), Baseline: shippedPolicies(t),
	})
	if err := allowed.lockoutCheck(context.Background(), tenantID, owner,
		func(context.Context) error { return errors.New("must not be called") }); err != nil {
		t.Errorf("lockoutCheck refused an actor the engine still allows: %v", err)
	}
}

// TestValidResource_AdmitsTheEnginesGrammarAndNothingElse.
func TestValidResource_AdmitsTheEnginesGrammarAndNothingElse(t *testing.T) {
	id := uuid.NewString()
	good := []string{"*", "location/" + id, "department/" + id, "employee/" + id}
	bad := []string{"", "location", "location/", "location/x", "tenant/" + id,
		"policy/" + id, "location/" + id + "/x", "*/*", " *"}
	for _, s := range good {
		if !ValidResource(s) {
			t.Errorf("ValidResource(%q) = false; the picker offers this shape", s)
		}
	}
	for _, s := range bad {
		if ValidResource(s) {
			t.Errorf("ValidResource(%q) = true; a binding to a string nothing resolves is a "+
				"binding to nothing, silently, forever", s)
		}
	}
}

// TestAuthorableRules_ExcludeEveryDocumentThatMentionsAFailClosedAction.
//
// 🔴 THE FILTER IS DERIVED AND THIS IS WHAT IT BUYS. A name list would have to be
// kept in step with internal/policy; this asks the engine which actions fail closed
// and drops any shipped document that mentions one -- so the authorization document
// is excluded on the day it is written rather than on the day somebody remembers.
func TestAuthorableRules_ExcludeEveryDocumentThatMentionsAFailClosedAction(t *testing.T) {
	authorable := AuthorableRules()
	if len(authorable) == 0 {
		t.Fatal("no shipped rule is authorable, so the v1 editor can produce nothing")
	}
	if len(authorable) >= len(policy.Baseline()) {
		t.Fatalf("%d of %d shipped documents are authorable; the authorization document must "+
			"not be one of them", len(authorable), len(policy.Baseline()))
	}
	for _, doc := range authorable {
		for _, st := range doc.Document.Statements {
			for _, a := range st.Action {
				got := policy.Evaluate(policy.Set{}, policy.Context{Action: a}).Effect
				if got == policy.EffectDeny {
					t.Errorf("%q is offered for copying and mentions %s, whose engine default is "+
						"deny -- i.e. an AUTHORIZATION action. A customer could then generate a "+
						"statement about who may do what by pressing buttons.", doc.Name, a)
				}
			}
		}
	}
}

// TestCopyOfShipped_KeepsTheRulesAndChangesOnlyTheScopeAndTheSid.
func TestCopyOfShipped_KeepsTheRulesAndChangesOnlyTheScopeAndTheSid(t *testing.T) {
	src := AuthorableRules()[0]
	venue := uuid.New()
	doc, err := copyOfShipped(src.Name, "my night rule", []uuid.UUID{venue})
	if err != nil {
		t.Fatalf("copyOfShipped: %v", err)
	}
	if doc.Name != "my night rule" || len(doc.Statements) != len(src.Document.Statements) {
		t.Fatalf("the copy is %+v, want the same statements under a new name", doc)
	}
	for i, st := range doc.Statements {
		orig := src.Document.Statements[i]
		if st.Effect != orig.Effect {
			t.Errorf("statement %d changed effect from %s to %s; the form offers no effect, so "+
				"a copy that changed one would be a rule the customer did not choose",
				i, orig.Effect, st.Effect)
		}
		// 🔴 THE SID MOVES INTO THE CUSTOMER'S NAMESPACE. A record carries matched_sid so
		// a manager can find the rule that produced it; a tenant copy keeping the shipped
		// sid would send them to a Tappa rule with a different scope.
		if st.Sid == orig.Sid || !hasPrefix(st.Sid, "own:") {
			t.Errorf("statement %d kept the sid %q; a customer document must not report a "+
				"base: sid on a docket", i, st.Sid)
		}
		if len(st.Resource) != 1 || st.Resource[0] != "location/"+venue.String() {
			t.Errorf("statement %d is scoped to %v, want only the chosen venue", i, st.Resource)
		}
	}
	// AND THE RESULT IS VALID BY THE SAME PARSER THE TAP PATH USES, because a stored
	// document that will not parse takes the WHOLE policy layer down for that business.
	if _, err := copyOfShipped("no such rule", "x", nil); err == nil {
		t.Error("copyOfShipped accepted a document name the binary does not ship")
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// shippedPolicies is the whole shipped baseline as evaluator policies, parsed by the
// real parser rather than assembled by hand.
func shippedPolicies(t *testing.T) []policy.Policy {
	t.Helper()
	var out []policy.Policy
	for _, doc := range policy.Baseline() {
		out = append(out, policy.Policy{
			VersionID: uuid.New(), Layer: policy.LayerBaseline, Document: doc.Document,
		})
	}
	return out
}
