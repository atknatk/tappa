package tenant

// policyprose_test.go -- WHO WRITES THE SENTENCE A TAP SHOWS (M8-04 FAZ B3,
// correction round 2).
//
// 🔴 WHY THIS FILE EXISTS, AND WHAT IT REPLACED. A round of M8-04 shipped comments
// in five places asserting that a tenant's own policy statement carries an
// "author-written `reason`" which becomes tap.Decision.Note verbatim, bounded only
// by internal/policy's maxReasonLen. That claim was measured on this tree and it is
// FALSE -- there is no production path by which a customer supplies the prose of a
// policy statement:
//
//   - copyOfShipped copies each shipped statement WHOLE (`copied := st`); only Sid
//     and Resource are replaced, so Effect, Action, Condition and Reason are the
//     ones this binary shipped.
//   - AuthorCommand has no Reason field, and no field of any other name that
//     reaches a statement's prose.
//   - the panel form behind it (internal/handler/policyactions.go) reads five
//     keys -- op, policy, resource, name, based_on, venue -- and uploads no
//     document at all.
//
// So the sentence is OURS. What the customer chooses is WHICH shipped statement
// applies and WHERE, which is a real choice and is exactly what
// transactions.policy_layer='tenant' records -- the fact ledger.Record.NoteIsTenants
// marks on a docket. The label reports whose RULE decided, not whose PROSE it is,
// and the two tests below are what keep that distinction true.
//
// ⚠️ THE FORWARD RISK IS COUNTED, NOT SILENCED. Q22 shipped the form-only editor on
// purpose and names the raw-JSON editor as M9-07 (see AuthorCommand's doc comment).
// The day a customer can post a document, `reason` becomes author-written and the
// falsified claim becomes true.
//
// WHAT WOULD SAY SO, BY THE SHAPE THE NEW PATH TAKES -- named rather than promised,
// because an earlier version of this paragraph said "whichever way it is built" and
// round 3's audit compiled three ways it was not built at all:
//
//   - it reuses copyOfShipped ->
//     TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs, below.
//   - it adds a query to db/queries ->
//     TestNoteProvenance_TheWriterListIsDerivedFromTheQueriesRatherThanNamed (cmd/tappa).
//   - it hand-writes a store method ->
//     TestNoteProvenance_TheStoreCarriesNoWriterTheQueriesDoNotName (cmd/tappa).
//   - it calls a writer from a new file, or puts the INSERT in shipped Go ->
//     TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument (cmd/tappa).
//
// Each of those four was injected and measured red (2026-08-20); the cmd/tappa file's
// header lists what it still cannot see. Any of the four forces this comment and its
// four siblings to be re-read before the editor ships.

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/policy"
)

// TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs is the BEHAVIOURAL half:
// it runs the only production document generator over every rule the panel offers
// and compares each produced statement with the shipped one, field by field.
//
// 🔴 IT IS NOT A TEXT SCAN. The claim being held is about what the code DOES, so
// the test calls it. A source-reading version would pass the day somebody added a
// second generator beside this one; the cmd/tappa sibling is what covers that, and
// it says so.
func TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs(t *testing.T) {
	t.Parallel()

	venue := uuid.New()
	shipped := AuthorableRules()

	// CONTROL: there is something to measure. An empty catalogue, or a catalogue of
	// statements that carry no prose, would let every assertion below pass while
	// proving nothing about reasons.
	if len(shipped) == 0 {
		t.Fatal("CONTROL FAILED: AuthorableRules() is empty, so nothing below is measured")
	}
	withProse := 0

	for _, src := range shipped {
		t.Run(src.Name, func(t *testing.T) {
			got, err := copyOfShipped(src.Name, "the customer's own label", []uuid.UUID{venue})
			if err != nil {
				t.Fatalf("copyOfShipped(%q): %v", src.Name, err)
			}
			if len(got.Statements) != len(src.Document.Statements) {
				t.Fatalf("the copy holds %d statements, the shipped document holds %d",
					len(got.Statements), len(src.Document.Statements))
			}
			for i, want := range src.Document.Statements {
				have := got.Statements[i]
				if want.Reason != "" {
					withProse++
				}

				// 🔴 THE ASSERTION THE FIVE CORRECTED COMMENTS REST ON. If this ever
				// fails, a customer can put words on an employee's screen and into an
				// IMMUTABLE record (§4.3), and every comment naming this test has to
				// be rewritten in the same change.
				if have.Reason != want.Reason {
					t.Errorf("statement %d's reason changed in the copy:\n got %q\nwant %q\n"+
						"A tenant document is a SCOPED COPY of prose we shipped. If prose can "+
						"now differ, tap.Decision.Note is no longer ours and the provenance "+
						"comments on ledger.Record.Note, DocketView.Note, ResultView.Note, "+
						"db/queries/transactions.sql and db/queries/reviews.sql are all wrong.",
						i, have.Reason, want.Reason)
				}
				if have.Effect != want.Effect {
					t.Errorf("statement %d's effect changed in the copy: got %q, want %q",
						i, have.Effect, want.Effect)
				}
				if !reflect.DeepEqual(have.Action, want.Action) {
					t.Errorf("statement %d's action list changed in the copy: got %v, want %v",
						i, have.Action, want.Action)
				}
				if !reflect.DeepEqual(have.Condition, want.Condition) {
					t.Errorf("statement %d's condition changed in the copy: got %v, want %v\n"+
						"The condition travels WITH the reason, which is why a copied sentence "+
						"cannot appear on a row it does not describe.",
						i, have.Condition, want.Condition)
				}

				// AND THE TWO THAT ARE THE CUSTOMER'S: the sid moves into their
				// namespace so a docket never reports a base: sid for a document we
				// did not write, and the resource list becomes the venues they picked.
				if have.Sid == want.Sid {
					t.Errorf("statement %d kept the shipped sid %q; a tenant document that "+
						"reports a base: sid makes matched_sid point at the wrong rule",
						i, have.Sid)
				}
				if got, want := have.Resource, []string{"location/" + venue.String()}; !reflect.DeepEqual(got, want) {
					t.Errorf("statement %d's resource is %v, want %v", i, got, want)
				}
			}
		})
	}

	if withProse == 0 {
		t.Error("CONTROL FAILED: not one shipped statement carries a reason, so the " +
			"reason comparison above compared empty strings and proved nothing")
	}
}

// TestAuthorCommand_CarriesNoProseDestinedForAStatement pins the INPUT side.
//
// 🔴 A FIELD IS HOW THE PROSE WOULD ARRIVE. copyOfShipped can only preserve what it
// is given, so the other half of "the customer cannot write the sentence" is that
// the command reaching it has nowhere to put one. The field set is compared whole
// rather than probed for a name containing "Reason", because the next such field
// might be called Message, Explanation or Note.
//
// ⚠️ COUNTED LIMIT: this is reflection over a struct, so it sees a new FIELD and not
// a new MEANING. Widening Name until it is pasted into a statement would pass here
// and fail the behavioural test above -- which is the division of labour, stated so
// neither test is mistaken for covering both.
func TestAuthorCommand_CarriesNoProseDestinedForAStatement(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"TenantID": true, // who
		"Actor":    true, // on whose authority
		"PolicyID": true, // which existing policy to append to
		"Name":     true, // the customer's label for the policy -- NOT a statement's prose
		"BasedOn":  true, // which shipped document to copy, looked up rather than trusted
		"Venues":   true, // where the copy applies
	}

	rt := reflect.TypeOf(AuthorCommand{})
	got := make(map[string]bool, rt.NumField())
	for i := range rt.NumField() {
		got[rt.Field(i).Name] = true
	}

	for name := range got {
		if !want[name] {
			t.Errorf("AuthorCommand has a new field %q. If it carries text that reaches a "+
				"policy statement, then a customer CAN write the sentence a tap shows and "+
				"the provenance comments naming this test are false -- fix them in the same "+
				"change. If it does not, add it to the list above with a comment saying why.",
				name)
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("AuthorCommand no longer has %q; this list is stale and is excusing "+
				"fields that no longer exist", name)
		}
	}
}

// TestPolicyStatement_HasExactlyOneProseField is the reason the two tests above are
// enough, made checkable: Reason is the ONE field of a statement that renders as
// human prose, so it is the one field whose provenance a docket has to answer for.
//
// A second prose field would need its own answer, and nothing else in the repository
// would notice it appearing.
func TestPolicyStatement_HasExactlyOneProseField(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"Sid": true, "Effect": true, "Action": true,
		"Resource": true, "Condition": true, "Reason": true,
	}
	rt := reflect.TypeOf(policy.Statement{})
	for i := range rt.NumField() {
		if name := rt.Field(i).Name; !want[name] {
			t.Errorf("policy.Statement has a new field %q. If it is prose that reaches a "+
				"screen, it needs the same provenance answer Reason has and the comments "+
				"naming this test have to say so.", name)
		}
	}
	if n := rt.NumField(); n != len(want) {
		t.Errorf("policy.Statement has %d fields, the list above names %d", n, len(want))
	}
}
