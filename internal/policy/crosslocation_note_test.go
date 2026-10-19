package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// This file is backlog T125 / ADR 0029: base:cross-location-note is a NOTE, and a
// note decides nothing. It used to be an allow, and Q16's "require IP" lever --
// switching base:gps-only-allow off -- left a GPS-only tap at ANOTHER branch
// allowed by that note while the same tap at home went to review. The test that
// measured the gap now asserts the fixed outcome under a new name,
// TestBaseline_RequiringIPHoldsACrossLocationGPSTapForReview.

// crossNoteReason is the sentence the shipped note adds.
func crossNoteReason(t *testing.T) string {
	t.Helper()
	for _, d := range Baseline() {
		for _, st := range d.Document.Statements {
			if st.Sid == SidCrossLocationNote {
				return st.Reason
			}
		}
	}
	t.Fatal("no shipped cross-location note")
	return ""
}

// baselineWithout is the full managed baseline minus the documents whose statement
// sids are listed -- a tenant that switched those rules off (a disabled policy is
// simply absent from the Set, checkin.assembleBaseline).
func baselineWithout(off ...string) []Policy {
	skip := map[string]bool{}
	for _, s := range off {
		skip[s] = true
	}
	var out []Policy
	for _, p := range fullBaseline() {
		if !skip[p.Document.Statements[0].Sid] {
			out = append(out, p)
		}
	}
	return out
}

// oldCrossLocationAllow is the baseline as it shipped on 2026-07-26: the same
// documents with base:cross-location-note's effect put back to allow -- the copy
// every tenant provisioned before 2026-10-18 still has stored.
func oldCrossLocationAllow(ps []Policy) []Policy {
	out := make([]Policy, 0, len(ps))
	for _, p := range ps {
		if p.Document.Statements[0].Sid == SidCrossLocationNote {
			doc := p.Document
			doc.Statements = append([]Statement(nil), doc.Statements...)
			doc.Statements[0].Effect = EffectAllow
			p.Document = doc
		}
		out = append(out, p)
	}
	return out
}

// TestBaseline_CrossLocationIsANoteNotAVerdict: alone in a Set, the note matches a
// cross-location tap and DECIDES NOTHING -- the tap falls to the fail-to-review
// default, which the note rides on; at home it does not even match.
func TestBaseline_CrossLocationIsANoteNotAVerdict(t *testing.T) {
	set := Set{Guardrails: DefaultGuardrails(), Baseline: baselineDocFor(t, SidCrossLocationNote)}
	d := Evaluate(set, benignTap(map[ContextKey]any{CtxEmployeeCrossLocation: true}))
	if d.Effect != EffectReview || d.MatchedSid != "default" || d.PolicyVersionID != nil {
		t.Fatalf("note alone: got %q via %q (version %v), want the default review -- a note must not decide",
			d.Effect, d.MatchedSid, d.PolicyVersionID)
	}
	if len(d.Notes) != 1 || d.Notes[0].Sid != SidCrossLocationNote || d.Notes[0].Reason != crossNoteReason(t) {
		t.Fatalf("note alone: Notes = %+v, want the cross-location note", d.Notes)
	}
	home := Evaluate(set, benignTap(map[ContextKey]any{CtxEmployeeCrossLocation: false}))
	if len(home.Notes) != 0 {
		t.Fatalf("a tap at home carries notes %+v", home.Notes)
	}
}

// TestBaseline_RequiringIPHoldsACrossLocationGPSTapForReview is T125's acceptance
// table: every evidence shape × home / another location × base:gps-only-allow on /
// off. At another location the DECISION -- effect, sid, layer, reason, version --
// is the one the same evidence gets at home; the only difference is the note. The
// row the backlog measured (other location, GPS only, gps-only-allow off) is now a
// review by default, like at home.
func TestBaseline_RequiringIPHoldsACrossLocationGPSTapForReview(t *testing.T) {
	type shape struct {
		name       string
		channel    string
		ip, gps    bool
		wantOn     Effect // gps-only-allow on (the default baseline)
		wantOnSid  string
		wantOff    Effect // gps-only-allow off (Q16: require IP)
		wantOffSid string
	}
	shapes := []shape{
		{"NFC + IP", channelNFC, true, false, EffectAllow, SidIPOrGPSOK, EffectAllow, SidIPOrGPSOK},
		{"NFC + IP + GPS", channelNFC, true, true, EffectAllow, SidIPOrGPSOK, EffectAllow, SidIPOrGPSOK},
		{"NFC + GPS only", channelNFC, false, true, EffectAllow, SidGPSOnlyAllow, EffectReview, "default"},
		{"NFC + no evidence", channelNFC, false, false, EffectReview, SidNoEvidenceReview, EffectReview, SidNoEvidenceReview},
		{"QR + IP", channelQR, true, false, EffectAllow, SidIPOrGPSOK, EffectAllow, SidIPOrGPSOK},
		{"QR + GPS only", channelQR, false, true, EffectReview, SidQRRequiresIP, EffectReview, SidQRRequiresIP},
		{"QR + no evidence", channelQR, false, false, EffectReview, SidNoEvidenceReview, EffectReview, SidNoEvidenceReview},
	}
	for _, sh := range shapes {
		for _, gpsOnlyOn := range []bool{true, false} {
			var baseline []Policy
			want, wantSid := sh.wantOn, sh.wantOnSid
			if gpsOnlyOn {
				baseline = fullBaseline()
			} else {
				baseline = baselineWithout(SidGPSOnlyAllow)
				want, wantSid = sh.wantOff, sh.wantOffSid
			}
			set := Set{Guardrails: DefaultGuardrails(), Baseline: baseline}
			name := fmt.Sprintf("%s, gps-only-allow on=%v", sh.name, gpsOnlyOn)
			t.Run(name, func(t *testing.T) {
				at := func(cross bool) Decision {
					return Evaluate(set, benignTap(map[ContextKey]any{
						CtxTapChannel: sh.channel, CtxTapIPMatch: sh.ip, CtxTapGPSMatch: sh.gps,
						CtxEmployeeCrossLocation: cross,
					}))
				}
				home, other := at(false), at(true)
				if home.Effect != want || home.MatchedSid != wantSid {
					t.Fatalf("home: got %q via %q, want %q via %q", home.Effect, home.MatchedSid, want, wantSid)
				}
				if other.Effect != home.Effect || other.MatchedSid != home.MatchedSid || other.Layer != home.Layer ||
					other.Reason != home.Reason || !sameVersion(other.PolicyVersionID, home.PolicyVersionID) {
					t.Fatalf("another location decided differently from home:\n home  %q via %q (%s)\n other %q via %q (%s)",
						home.Effect, home.MatchedSid, home.Reason, other.Effect, other.MatchedSid, other.Reason)
				}
				if len(home.Notes) != 0 {
					t.Fatalf("home carries notes %+v", home.Notes)
				}
				if len(other.Notes) != 1 || other.Notes[0].Sid != SidCrossLocationNote {
					t.Fatalf("another location: Notes = %+v, want exactly the cross-location note", other.Notes)
				}
			})
		}
	}
}

func sameVersion(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// tapShapes enumerates every combination of the inputs the tap-evidence baseline
// reads: channel, IP, GPS, GPS conflict, a queued skew, a counter gap and the
// cross-location fact -- 2^7 = 128 contexts.
func tapShapes() []map[ContextKey]any {
	var out []map[ContextKey]any
	for bits := 0; bits < 1<<7; bits++ {
		on := func(i int) bool { return bits&(1<<i) != 0 }
		channel, skew, gap := channelNFC, float64(5), float64(0)
		if on(0) {
			channel = channelQR
		}
		if on(4) {
			skew = float64(BaselineQueuedSkewSeconds + 60)
		}
		if on(5) {
			gap = 1
		}
		out = append(out, map[ContextKey]any{
			CtxTapChannel: channel, CtxTapIPMatch: on(1), CtxTapGPSMatch: on(2), CtxTapGPSConflict: on(3),
			CtxTapOccurredAtSkewSeconds: skew, CtxTapCtrGap: gap, CtxEmployeeCrossLocation: on(6),
		})
	}
	return out
}

// TestBaseline_NoteOnlyChangesNoOutcomeUnderTheDefaultBaseline is T125's behaviour
// inventory, as a test. Under the FULL default baseline (a tenant that changed
// nothing), the old allow and the new note give the same effect, sid, layer and
// reason on all 128 input shapes: the old allow never won (the evidence allows
// precede it in document order; every review out-restricts it). So a customer who
// switched nothing sees no verdict change -- only the note on the record.
//
// Then every one of the 2^7 ways to switch the SEVEN other tap documents off is
// walked: wherever the outcome differs, the old one was an allow decided by the
// cross-location note and the new one is a review -- the change only ever TIGHTENS,
// and only where a tenant switched an evidence rule off.
func TestBaseline_NoteOnlyChangesNoOutcomeUnderTheDefaultBaseline(t *testing.T) {
	shapes := tapShapes()
	for i, keys := range shapes {
		newD := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline()}, benignTap(keys))
		oldD := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: oldCrossLocationAllow(fullBaseline())}, benignTap(keys))
		if newD.Effect != oldD.Effect || newD.MatchedSid != oldD.MatchedSid || newD.Layer != oldD.Layer || newD.Reason != oldD.Reason {
			t.Fatalf("shape %d %v: default baseline changed outcome: old %q via %q, new %q via %q",
				i, keys, oldD.Effect, oldD.MatchedSid, newD.Effect, newD.MatchedSid)
		}
	}

	others := []string{SidIPOrGPSOK, SidGPSOnlyAllow, SidNoEvidenceReview, SidQRRequiresIP,
		SidQueuedWindow, SidCtrGapReview, SidGPSConflictReview}
	diffsBySubset := map[string]int{}
	for mask := 0; mask < 1<<len(others); mask++ {
		var off []string
		for i, sid := range others {
			if mask&(1<<i) != 0 {
				off = append(off, sid)
			}
		}
		newSet := Set{Guardrails: DefaultGuardrails(), Baseline: baselineWithout(off...)}
		oldSet := Set{Guardrails: DefaultGuardrails(), Baseline: oldCrossLocationAllow(baselineWithout(off...))}
		for _, keys := range shapes {
			newD, oldD := Evaluate(newSet, benignTap(keys)), Evaluate(oldSet, benignTap(keys))
			if newD.Effect == oldD.Effect && newD.MatchedSid == oldD.MatchedSid {
				continue
			}
			if oldD.Effect != EffectAllow || oldD.MatchedSid != SidCrossLocationNote || newD.Effect != EffectReview {
				t.Fatalf("off=%v %v: old %q via %q -> new %q via %q; a change must only take away the note's allow",
					off, keys, oldD.Effect, oldD.MatchedSid, newD.Effect, newD.MatchedSid)
			}
			if keys[CtxEmployeeCrossLocation] != true {
				t.Fatalf("off=%v %v: a tap at HOME changed outcome", off, keys)
			}
			diffsBySubset[strings.Join(off, "+")]++
		}
	}
	if diffsBySubset[""] != 0 {
		t.Fatalf("the default baseline changed %d outcomes", diffsBySubset[""])
	}
	// The measured row (T125): gps-only-allow off alone changes exactly the
	// cross-location NFC GPS-only shapes.
	if diffsBySubset[SidGPSOnlyAllow] == 0 {
		t.Fatal("switching gps-only-allow off changed nothing: the T125 row is not being exercised")
	}
	// The panel's guard list IS this inventory (ADR 0029 follow-up): every rule that
	// changes an outcome when switched off ALONE is on CrossLocationEvidenceSids, and
	// every rule on it does change one -- a rule dropped from the list, or a fourth
	// rule that starts to matter, is red here.
	var alone []string
	for _, sid := range others {
		if diffsBySubset[sid] > 0 {
			alone = append(alone, sid)
		}
	}
	if got, want := strings.Join(alone, ","), strings.Join(CrossLocationEvidenceSids(), ","); got != want {
		t.Fatalf("the rules that change an outcome switched off alone are %s; CrossLocationEvidenceSids says %s", got, want)
	}
	for k, n := range diffsBySubset {
		for _, rule := range CrossLocationEvidenceSids() {
			if strings.Contains(k, rule) {
				goto ok
			}
		}
		t.Fatalf("off=%s changed %d outcomes without an evidence rule switched off", k, n)
	ok:
	}
	if testing.Verbose() {
		for _, single := range CrossLocationEvidenceSids() {
			t.Logf("only %s off: %d of %d shapes change (all cross-location, all allow -> review)",
				single, diffsBySubset[single], len(shapes))
		}
		t.Logf("subsets with a change: %d of %d", len(diffsBySubset), 1<<len(others))
	}
}

// TestEvaluate_NoteNeverDecides: a note statement -- baseline or tenant, at any
// specificity, holding or not -- added to a Set never changes the Decision's
// effect, sid, layer, reason or version, over every input shape.
func TestEvaluate_NoteNeverDecides(t *testing.T) {
	notes := []Policy{
		pol(LayerTenant, Statement{Sid: "t:note-exact", Effect: EffectNote, Action: []Action{ActionTapRecord},
			Resource: []string{"location/rusty-bar"}, Reason: "exact note"}),
		pol(LayerTenant, Statement{Sid: "t:note-global", Effect: EffectNote, Action: []Action{ActionTapRecord},
			Resource: []string{"*"}, Reason: "global note"}),
	}
	for _, baseline := range [][]Policy{fullBaseline(), nil, baselineWithout(SidNoEvidenceReview, SidGPSOnlyAllow)} {
		for _, keys := range tapShapes() {
			ctx := benignTap(keys)
			without := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: baseline}, ctx)
			with := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: baseline, Tenant: notes}, ctx)
			if with.Effect != without.Effect || with.MatchedSid != without.MatchedSid || with.Layer != without.Layer ||
				with.Reason != without.Reason || !sameVersion(with.PolicyVersionID, without.PolicyVersionID) {
				t.Fatalf("%v: a note changed the decision: %q via %q -> %q via %q",
					keys, without.Effect, without.MatchedSid, with.Effect, with.MatchedSid)
			}
			if got := len(with.Notes) - len(without.Notes); got != 2 {
				t.Fatalf("%v: the two tenant notes added %d notes, want 2", keys, got)
			}
		}
	}
}

// TestEvaluate_NotesOrderDedupeAndGuardrails: notes come in the fixed walk order
// (baseline before tenant, document order), a tenant copy of the shipped note says
// its sentence once, and a guardrail -- terminal -- carries none.
func TestEvaluate_NotesOrderDedupeAndGuardrails(t *testing.T) {
	reason := crossNoteReason(t)
	tenant := []Policy{pol(LayerTenant,
		Statement{Sid: "t:first", Effect: EffectNote, Action: []Action{ActionTapRecord}, Resource: []string{"*"}, Reason: "first"},
		Statement{Sid: "own:cross-location-note", Effect: EffectNote, Action: []Action{ActionTapRecord},
			Resource: []string{"location/rusty-bar"}, Condition: Condition{OpBool: {CtxEmployeeCrossLocation: true}}, Reason: reason},
	)}
	set := Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline(), Tenant: tenant}
	d := Evaluate(set, benignTap(map[ContextKey]any{CtxTapIPMatch: true, CtxEmployeeCrossLocation: true}))
	if d.Effect != EffectAllow || d.MatchedSid != SidIPOrGPSOK {
		t.Fatalf("got %q via %q, want allow via %s", d.Effect, d.MatchedSid, SidIPOrGPSOK)
	}
	want := []Note{{Sid: SidCrossLocationNote, Reason: reason}, {Sid: "t:first", Reason: "first"}}
	if fmt.Sprint(d.Notes) != fmt.Sprint(want) {
		t.Fatalf("Notes = %+v, want %+v", d.Notes, want)
	}
	retired := benignTap(map[ContextKey]any{CtxTagStatus: "retired", CtxEmployeeCrossLocation: true})
	if g := Evaluate(set, retired); g.Layer != LayerGuardrail || len(g.Notes) != 0 {
		t.Fatalf("guardrail decision: layer %q, notes %+v; want a guardrail with no notes", g.Layer, g.Notes)
	}
}

// TestValidate_NoteIsBounded: a note is admitted in both layers on tap:record with
// a reason, and refused on an authorization action or without words.
func TestValidate_NoteIsBounded(t *testing.T) {
	doc := func(st Statement) Document { return Document{Version: "v", Name: "n", Statements: []Statement{st}} }
	ok := Statement{Sid: "t:n", Effect: EffectNote, Action: []Action{ActionTapRecord}, Resource: []string{"*"}, Reason: "noted"}
	for _, layer := range []Layer{LayerTenant, LayerBaseline} {
		if err := Validate(doc(ok), layer, DefaultLimits()); err != nil {
			t.Fatalf("%s: a tap:record note with a reason is refused: %v", layer, err)
		}
	}
	authz := ok
	authz.Action = []Action{ActionTapRecord, ActionPolicyEdit}
	if err := Validate(doc(authz), LayerTenant, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "only annotates") {
		t.Fatalf("a note on policy:edit: err = %v, want a refusal", err)
	}
	silent := ok
	silent.Reason = "  "
	if err := Validate(doc(silent), LayerTenant, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "needs a reason") {
		t.Fatalf("a note without words: err = %v, want a refusal", err)
	}
}

// --- the announcement ---------------------------------------------------------

// TestBaseline_EveryBumpIsAnnounced: the newest BaselineChange IS the shipped
// BaselineVersion (a bump with no sentence for the customer, or a sentence for a
// release that was never stamped, is red); entries are in order and name shipped
// documents; and BaselineChangesSince pairs a stored stamp with what came after it.
func TestBaseline_EveryBumpIsAnnounced(t *testing.T) {
	changes := BaselineChanges()
	if len(changes) == 0 || changes[len(changes)-1].Version != BaselineVersion {
		t.Fatalf("BaselineVersion %q has no BaselineChange announcing it", BaselineVersion)
	}
	names := map[string]bool{}
	for _, d := range Baseline() {
		names[d.Name] = true
	}
	prev := "2026-07-26"
	for _, c := range changes {
		if c.Version <= "2026-07-26" || c.Version < prev || c.Version > BaselineVersion {
			t.Fatalf("change %q out of order (after %q, at most %q)", c.Version, prev, BaselineVersion)
		}
		prev = c.Version
		if !names[c.Document] {
			t.Fatalf("change %q names %q, which is not a shipped document", c.Version, c.Document)
		}
		if strings.TrimSpace(c.Summary) == "" || strings.Contains(c.Summary, "base:") {
			t.Fatalf("change %q: the summary is customer copy and must say something without a sid", c.Version)
		}
	}
	cross := ""
	for _, d := range Baseline() {
		if d.Document.Statements[0].Sid == SidCrossLocationNote {
			cross = d.Name
		}
	}
	if got := BaselineChangesSince(cross, "2026-07-26"); len(got) != 1 || got[0].Version != "2026-10-18" {
		t.Fatalf("a tenant on 2026-07-26 is told %+v about the cross-location note, want the 2026-10-18 change", got)
	}
	if got := BaselineChangesSince(cross, BaselineVersion); len(got) != 0 {
		t.Fatalf("a tenant on the shipped stamp is told %+v", got)
	}
	for _, d := range Baseline() {
		if d.Name != cross && len(BaselineChangesSince(d.Name, "2026-07-26")) != 0 {
			t.Fatalf("%q did not change on 2026-10-18 but is announced", d.Name)
		}
	}
}

// shippedDigests pins the managed baseline's CONTENT to its stamp: the sha256 of
// Baseline() serialised with every document's Version blanked. A change to any
// effect, condition, action, resource, reason or name changes the digest; the
// stamp it is filed under must then be NEW (bump BaselineVersion, add a
// BaselineChange) -- the gate baseline.go's BaselineVersion doc said was missing.
// Add a line per bump; never edit an old one.
var shippedDigests = map[string]string{
	"2026-10-18": "4fbc6395db2cb3829f934883d180f84a4981a0d25ef7e75955d15e2aba4758d9",
}

func TestBaseline_ContentIsPinnedToItsStamp(t *testing.T) {
	docs := Baseline()
	for i := range docs {
		docs[i].Document.Version = ""
	}
	raw, err := json.Marshal(docs)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	want, ok := shippedDigests[BaselineVersion]
	if !ok {
		t.Fatalf("BaselineVersion %q has no pinned digest; add %q to shippedDigests", BaselineVersion, got)
	}
	if got != want {
		t.Fatalf("the shipped baseline changed under stamp %q (digest %s, pinned %s): bump BaselineVersion, "+
			"add a BaselineChange, and pin the new digest under the new stamp", BaselineVersion, got, want)
	}
}

// TestBaseline_AnOldTenantCopyOfTheAllowStillDecidesAtItsVenue MEASURES what ADR
// 0029 does not close. The panel's "write your own" form copies a shipped document
// whole (tenant.copyOfShipped) and scopes it to venues; a copy of the cross-location
// rule taken before 2026-10-18 is a stored ALLOW at location/<id>, and at that venue
// it out-ranks every location/* review by specificity (ADR 0004 §7) -- a
// no-evidence tap from another branch is let through there. Stored documents are
// not rewritten (ADR 0004 §9), so the fix is the tenant's (switch it off) and the
// size is a production count (the T125 report's query). A copy taken now is a note
// and decides nothing -- the second half of this test.
func TestBaseline_AnOldTenantCopyOfTheAllowStillDecidesAtItsVenue(t *testing.T) {
	reason := crossNoteReason(t)
	copyAt := func(eff Effect) []Policy {
		return []Policy{pol(LayerTenant, Statement{Sid: "own:cross-location-note", Effect: eff,
			Action: []Action{ActionTapRecord}, Resource: []string{"location/rusty-bar"},
			Condition: Condition{OpBool: {CtxEmployeeCrossLocation: true}}, Reason: reason})}
	}
	noEvidence := benignTap(map[ContextKey]any{CtxTapIPMatch: false, CtxTapGPSMatch: false, CtxEmployeeCrossLocation: true})
	old := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline(), Tenant: copyAt(EffectAllow)}, noEvidence)
	if old.Effect != EffectAllow || old.MatchedSid != "own:cross-location-note" {
		t.Fatalf("old copy: got %q via %q -- if this is no longer allow, ADR 0029's limit paragraph is stale", old.Effect, old.MatchedSid)
	}
	now := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline(), Tenant: copyAt(EffectNote)}, noEvidence)
	if now.Effect != EffectReview || now.MatchedSid != SidNoEvidenceReview {
		t.Fatalf("new copy: got %q via %q, want review via %s", now.Effect, now.MatchedSid, SidNoEvidenceReview)
	}
}
