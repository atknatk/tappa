package policy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestIsOldCrossLocationAllow_KnowsTheStoredShapeAndNothingElse: the panel's guard
// (tenant.crossLocationGuard) reads STORED documents, so the predicate is measured on
// documents that went through JSON and the parser: the cross-location statement with
// its pre-2026-10-18 allow is recognised in a baseline document and in a tenant copy
// (the customer's sid, a venue); the shipped note, every other shipped statement, and
// an allow on another condition are not.
func TestIsOldCrossLocationAllow_KnowsTheStoredShapeAndNothingElse(t *testing.T) {
	stored := func(doc Document, layer Layer) Document {
		t.Helper()
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		out, err := ParseAndValidate(raw, layer, DefaultLimits())
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return out
	}
	found := 0
	for _, d := range Baseline() {
		for _, st := range stored(d.Document, LayerBaseline).Statements {
			if IsOldCrossLocationAllow(st) {
				t.Errorf("the shipped %s is taken for the old allow", st.Sid)
			}
		}
		if d.Document.Statements[0].Sid != SidCrossLocationNote {
			continue
		}
		found++
		old := d.Document
		old.Statements = append([]Statement(nil), old.Statements...)
		old.Statements[0].Effect = EffectAllow
		if !IsOldCrossLocationAllow(stored(old, LayerBaseline).Statements[0]) {
			t.Error("the stored 2026-07-26 baseline statement is not recognised")
		}
		copied := old
		copied.Statements = append([]Statement(nil), old.Statements...)
		copied.Statements[0].Sid = "own:cross-location-note"
		copied.Statements[0].Resource = []string{"location/rusty-bar"}
		if !IsOldCrossLocationAllow(stored(copied, LayerTenant).Statements[0]) {
			t.Error("a tenant copy of the old allow is not recognised")
		}
		other := copied
		other.Statements = append([]Statement(nil), copied.Statements...)
		other.Statements[0].Condition = Condition{OpBool: {CtxTapIPMatch: true}}
		if IsOldCrossLocationAllow(stored(other, LayerTenant).Statements[0]) {
			t.Error("an allow on another condition is taken for the old allow")
		}
	}
	if found != 1 {
		t.Fatalf("PREMISE: %d shipped cross-location documents", found)
	}
}

// TestBaselineChanges_TheCrossLocationAnnouncementNamesEveryEvidenceRule pins the
// 2026-10-18 announcement (ADR 0029 follow-up): it names, by the name the policies
// screen shows, every rule of CrossLocationEvidenceSids -- the three states the old
// allow undercuts (GPS-only allowance off 1/128, IP proof off 4/128, no-evidence
// review off 1/128: the last one a silent approval) -- and says those cannot be
// switched off while the copy is on. Its bytes are pinned: a change to what tenants
// are told is red here until it is made on purpose.
func TestBaselineChanges_TheCrossLocationAnnouncementNamesEveryEvidenceRule(t *testing.T) {
	var summary string
	for _, c := range BaselineChanges() {
		if c.Version == "2026-10-18" && c.Document == crossLocationDocName {
			summary = c.Summary
		}
	}
	if summary == "" {
		t.Fatal("no 2026-10-18 announcement on the cross-location document")
	}
	for _, sid := range CrossLocationEvidenceSids() {
		for _, d := range Baseline() {
			if d.Document.Statements[0].Sid != sid {
				continue
			}
			shown := strings.TrimPrefix(d.Name, "Taptime baseline — ")
			if !strings.Contains(summary, `"`+shown+`"`) {
				t.Errorf("the announcement does not name %q (%s)", shown, sid)
			}
		}
	}
	if !strings.Contains(summary, "cannot be switched off") {
		t.Error("the announcement does not say the evidence rules are held while the copy is on")
	}
	const pinned = "f361d1d2701773a657db87b1d34d467dfd0bba2aba88793c34e1496b86b021bc"
	if sum := fmt.Sprintf("%x", sha256.Sum256([]byte(summary))); sum != pinned {
		t.Errorf("the announcement's text changed (sha256 %s, pinned %s):\n%s", sum, pinned, summary)
	}
}
