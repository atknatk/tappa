package tenant

// crosslocationguard_db_test.go -- ADR 0029's panel guard (user decision 2026-10-19)
// against real Postgres: the writer refuses to switch an evidence rule off while the
// cross-location rule's OLD allow is in force, from either source (a stored
// 2026-07-26 baseline, a tenant copy bound to a venue), writes the refusal to the
// trail and leaves the rule as it was; a business on the new baseline switches freely;
// the same state cannot be reached from the other side; and one business's old allow
// says nothing about another's switches. SetEnabled is what a POST to the policies
// apply route reaches, so a refusal here is a refusal of a direct POST too.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/policy"
)

// guardDocs are the shipped baseline's names by sid, and its cross-location document.
func guardDocs(t *testing.T) (names map[string]string, cross policy.BaselineDoc) {
	t.Helper()
	names = map[string]string{}
	for _, d := range policy.Baseline() {
		names[d.Document.Statements[0].Sid] = d.Name
		if d.Document.Statements[0].Sid == policy.SidCrossLocationNote {
			cross = d
		}
	}
	if cross.Name == "" {
		t.Fatal("PREMISE: no shipped cross-location document")
	}
	return names, cross
}

// oldCrossLocation is the cross-location statement as it shipped before 2026-10-18:
// the same document with effect allow.
func oldCrossLocation(st policy.Statement) policy.Statement {
	st.Effect = policy.EffectAllow
	return st
}

// seedOldBaseline stores all nine shipped documents as a business provisioned before
// 2026-10-18 holds them: stamp 2026-07-26, the cross-location rule an allow.
func (f *writerFixture) seedOldBaseline(t *testing.T, tenantID uuid.UUID) {
	t.Helper()
	for _, d := range policy.Baseline() {
		doc := d.Document
		doc.Version = "2026-07-26"
		doc.Statements = append([]policy.Statement(nil), doc.Statements...)
		if doc.Statements[0].Sid == policy.SidCrossLocationNote {
			doc.Statements[0] = oldCrossLocation(doc.Statements[0])
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		f.storePolicy(t, tenantID, d.Name, "baseline", true, raw, nil)
	}
}

// seedVenueCopy stores a tenant rule made from the cross-location document by the
// authoring form before 2026-10-18: the customer's sid, one venue, effect allow.
func (f *writerFixture) seedVenueCopy(t *testing.T, tenantID uuid.UUID, cross policy.BaselineDoc) uuid.UUID {
	t.Helper()
	st := oldCrossLocation(cross.Document.Statements[0])
	st.Sid = tenantSid(st.Sid)
	st.Resource = []string{"location/" + uuid.NewString()}
	doc := policy.Document{Version: "2026-07-26", Name: "Rusty Bar movers", Statements: []policy.Statement{st}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.ParseAndValidate(raw, policy.LayerTenant, policy.DefaultLimits()); err != nil {
		t.Fatalf("PREMISE: the old venue copy is not a valid tenant document: %v", err)
	}
	return f.storePolicy(t, tenantID, doc.Name, "tenant", true, raw, &f.adminID)
}

func (f *writerFixture) switchAs(tenantID uuid.UUID, actor Actor, id uuid.UUID, on bool) error {
	_, err := f.writer.SetEnabled(context.Background(), EnableCommand{TenantID: tenantID, Actor: actor, PolicyID: id, Enabled: on})
	return err
}

// TestCrossLocationGuardDB_EveryEvidenceRuleUnderEitherSourceOfTheOldAllow: for each of
// policy.CrossLocationEvidenceSids and each source of the old allow -- the stored
// 2026-07-26 baseline, and a venue-bound tenant copy over the new baseline -- switching
// the evidence rule off is ErrCrossLocationFirst, the rule stays on, no policy.disabled
// row is written and one policy.switch_refused row names the rule that stands in the
// way. Switching that rule off first lets the evidence rule go.
func TestCrossLocationGuardDB_EveryEvidenceRuleUnderEitherSourceOfTheOldAllow(t *testing.T) {
	names, cross := guardDocs(t)
	for _, source := range []string{"stored 2026-07-26 baseline", "venue copy over the new baseline"} {
		for _, sid := range policy.CrossLocationEvidenceSids() {
			t.Run(source+"/"+sid, func(t *testing.T) {
				f := newWriterFixture(t)
				var blocker uuid.UUID
				var blockerName string
				if strings.HasPrefix(source, "stored") {
					f.seedOldBaseline(t, f.tenantID)
					blocker, blockerName = f.baselineID(t, f.tenantID, cross.Name), cross.Name
				} else {
					f.seedAuthzDocument(t, f.tenantID)
					blocker, blockerName = f.seedVenueCopy(t, f.tenantID, cross), "Rusty Bar movers"
				}
				evidence := f.baselineID(t, f.tenantID, names[sid])
				err := f.switchAs(f.tenantID, f.owner, evidence, false)
				if !errors.Is(err, ErrCrossLocationFirst) {
					t.Fatalf("switching %s off returned %v, want ErrCrossLocationFirst", sid, err)
				}
				if !f.enabledOf(t, evidence) {
					t.Fatal("the refused switch changed the rule")
				}
				if rows := f.auditRows(t, f.tenantID, ActionPolicyDisabled); len(rows) != 0 {
					t.Errorf("a refused switch left %d policy.disabled row(s)", len(rows))
				}
				rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused)
				if len(rows) != 1 {
					t.Fatalf("the trail holds %d policy.switch_refused row(s), want 1", len(rows))
				}
				for _, want := range []string{`"reason": "` + reasonOldAllowInForce + `"`, `"requested_enabled": false`,
					`"was_enabled": true`, `"name": "` + names[sid] + `"`, blocker.String(), `"name": "` + blockerName + `"`} {
					if !strings.Contains(rows[0], want) {
						t.Errorf("the refusal row does not carry %s:\n%s", want, rows[0])
					}
				}
				// The way out the screen names.
				if err := f.switchAs(f.tenantID, f.owner, blocker, false); err != nil {
					t.Fatalf("switching the old allow off: %v", err)
				}
				if err := f.switchAs(f.tenantID, f.owner, evidence, false); err != nil {
					t.Fatalf("with the old allow off, switching %s off returned %v", sid, err)
				}
				if f.enabledOf(t, evidence) {
					t.Fatal("the evidence rule is still on")
				}
			})
		}
	}
}

// TestCrossLocationGuardDB_TheNewBaselineSwitchesFreely: a business on the 2026-10-18
// baseline (the note, no old copy) switches every evidence rule off and on again, and
// the trail holds no refusal.
func TestCrossLocationGuardDB_TheNewBaselineSwitchesFreely(t *testing.T) {
	names, _ := guardDocs(t)
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	for _, sid := range policy.CrossLocationEvidenceSids() {
		id := f.baselineID(t, f.tenantID, names[sid])
		if err := f.switchAs(f.tenantID, f.owner, id, false); err != nil {
			t.Fatalf("new baseline, %s off: %v", sid, err)
		}
	}
	for _, sid := range policy.CrossLocationEvidenceSids() {
		if f.enabledOf(t, f.baselineID(t, f.tenantID, names[sid])) {
			t.Errorf("%s is still on", sid)
		}
	}
	if rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused); len(rows) != 0 {
		t.Errorf("the new baseline left %d refusal row(s)", len(rows))
	}
}

// TestCrossLocationGuardDB_TheOldAllowCannotComeBackOverAnEvidenceRuleOff: the same
// state from the other side. The old baseline's cross-location rule switched off, then
// GPS-only allowance off (allowed: nothing stands in the way), then the old rule back
// on -> ErrEvidenceRuleOff with its trail row; with GPS-only allowance back on, it
// comes back. Switching an evidence rule ON is never refused.
func TestCrossLocationGuardDB_TheOldAllowCannotComeBackOverAnEvidenceRuleOff(t *testing.T) {
	names, cross := guardDocs(t)
	f := newWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	old, gps := f.baselineID(t, f.tenantID, cross.Name), f.baselineID(t, f.tenantID, names[policy.SidGPSOnlyAllow])
	for _, step := range []struct {
		id   uuid.UUID
		on   bool
		want error
	}{{old, false, nil}, {gps, false, nil}, {old, true, ErrEvidenceRuleOff}, {gps, true, nil}, {old, true, nil}} {
		if err := f.switchAs(f.tenantID, f.owner, step.id, step.on); !errors.Is(err, step.want) {
			t.Fatalf("switch %s to %v: %v, want %v", f.policyName(t, step.id), step.on, err, step.want)
		}
	}
	rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused)
	if len(rows) != 1 || !strings.Contains(rows[0], `"reason": "`+reasonEvidenceRuleOff+`"`) ||
		!strings.Contains(rows[0], gps.String()) || !strings.Contains(rows[0], `"requested_enabled": true`) {
		t.Fatalf("the trail holds %v; want one evidence_rule_off refusal naming GPS-only allowance", rows)
	}
}

// TestCrossLocationGuardDB_OneBusinesssOldAllowSaysNothingAboutAnother (§4.5): the
// fixture's business holds the old baseline and the other business the new one. The
// other business's owner switches its evidence rules off freely (the old allow it
// would be refused for is not its own); the first business is still refused for its
// own; and the first business's owner reaching for the other's rule gets
// ErrUnknownPolicy -- the guard reads nothing across the boundary and writes no row.
func TestCrossLocationGuardDB_OneBusinesssOldAllowSaysNothingAboutAnother(t *testing.T) {
	names, _ := guardDocs(t)
	f := newWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	for _, d := range policy.Baseline() {
		raw, err := json.Marshal(d.Document)
		if err != nil {
			t.Fatal(err)
		}
		f.storePolicy(t, f.foreignTenant, d.Name, "baseline", true, raw, nil)
	}
	foreignOwner := Actor{ID: f.foreignAdmin, Role: "owner"}
	foreignGPS := f.baselineID(t, f.foreignTenant, names[policy.SidGPSOnlyAllow])
	if err := f.switchAs(f.tenantID, f.owner, foreignGPS, false); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatalf("the old-baseline business switching the other's rule: %v, want ErrUnknownPolicy", err)
	}
	if err := f.switchAs(f.foreignTenant, foreignOwner, foreignGPS, false); err != nil {
		t.Fatalf("the new-baseline business switching its own evidence rule off: %v", err)
	}
	if err := f.switchAs(f.tenantID, f.owner, f.baselineID(t, f.tenantID, names[policy.SidGPSOnlyAllow]), false); !errors.Is(err, ErrCrossLocationFirst) {
		t.Fatalf("the old-baseline business switching its own evidence rule off: %v, want ErrCrossLocationFirst", err)
	}
	if rows := f.auditRows(t, f.foreignTenant, ActionPolicySwitchRefused); len(rows) != 0 {
		t.Errorf("the other business holds %d refusal row(s)", len(rows))
	}
	if rows := f.auditRows(t, f.tenantID, ActionPolicySwitchRefused); len(rows) != 1 {
		t.Errorf("the old-baseline business holds %d refusal row(s), want its own one", len(rows))
	}
}

// TestCrossLocationGuardDB_RefusesExactlyTheInventorysRules: under the stored
// 2026-07-26 baseline, every shipped tap rule is switched off in turn (and back on when
// it went). The refused ones are exactly ADR 0029's three inventory rows -- IP proof,
// GPS-only allowance, no-evidence review -- spelled here, not read from
// policy.CrossLocationEvidenceSids, so a rule dropped from that list is red here as
// well as in internal/policy's inventory test; every other rule switches freely.
func TestCrossLocationGuardDB_RefusesExactlyTheInventorysRules(t *testing.T) {
	f := newWriterFixture(t)
	f.seedOldBaseline(t, f.tenantID)
	var refused []string
	for _, d := range AuthorableRules() {
		sid := d.Document.Statements[0].Sid
		if sid == policy.SidCrossLocationNote {
			continue
		}
		id := f.baselineID(t, f.tenantID, d.Name)
		switch err := f.switchAs(f.tenantID, f.owner, id, false); {
		case errors.Is(err, ErrCrossLocationFirst):
			refused = append(refused, sid)
		case err != nil:
			t.Fatalf("%s off: %v", sid, err)
		default:
			if err := f.switchAs(f.tenantID, f.owner, id, true); err != nil {
				t.Fatalf("%s back on: %v", sid, err)
			}
		}
	}
	want := []string{policy.SidIPOrGPSOK, policy.SidGPSOnlyAllow, policy.SidNoEvidenceReview}
	if strings.Join(refused, ",") != strings.Join(want, ",") {
		t.Fatalf("refused %v, want exactly ADR 0029's three %v", refused, want)
	}
}
