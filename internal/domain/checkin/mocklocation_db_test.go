package checkin

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/policy"
)

// This file is ADR 0045 Bedel 10 against the real tables (M16-06): a release that ADDS a
// baseline document (base:mock-location-review, 2026-11-14) reaches a business set up before it
// without an accept step -- on its next tap, or on the panel's next authorisation question,
// whichever comes first, through the ONE materialiser -- and changes no browser verdict.

const preAdditionStamp = "2026-10-18" // the stamp every document carried before the addition

// mockLocationDocName finds the added document, and the shipped set without it.
func addedDocAndBefore(t *testing.T) (string, []policy.BaselineDoc) {
	t.Helper()
	var name string
	var before []policy.BaselineDoc
	for _, d := range policy.Baseline() {
		if d.Document.Statements[0].Sid == policy.SidMockLocationReview {
			name = d.Name
			continue
		}
		before = append(before, d)
	}
	if name == "" || len(before) != len(policy.Baseline())-1 {
		t.Fatal("this binary does not ship the mock-location document")
	}
	return name, before
}

// seedPreAdditionBaseline stores the business's baseline as a tap materialised it BEFORE
// 2026-11-14 -- the same statements and derived ids (materialise itself), stamped 2026-10-18,
// without the added document.
func seedPreAdditionBaseline(t *testing.T, sets policySets, tenantID uuid.UUID) []policy.BaselineDoc {
	t.Helper()
	_, before := addedDocAndBefore(t)
	old := make([]policy.BaselineDoc, len(before))
	for i, d := range before {
		old[i] = d
		old[i].Document.Version = preAdditionStamp
	}
	if err := sets.materialise(context.Background(), tenantID, old); err != nil {
		t.Fatalf("seed the pre-addition baseline: %v", err)
	}
	return before
}

// baselineRows reads every stored baseline document's name, stamp and version number.
func baselineRows(t *testing.T, data *db.DB, tenantID uuid.UUID) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	if err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `
			SELECT p.name, v.document->>'version'
			FROM policies p JOIN policy_versions v ON v.policy_id = p.id AND v.tenant_id = p.tenant_id
			WHERE p.tenant_id = $1 AND p.layer = 'baseline'`, tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var name, stamp string
			if e := rows.Scan(&name, &stamp); e != nil {
				return e
			}
			out[name] = append(out[name], stamp)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read the stored baseline: %v", err)
	}
	return out
}

// assertCompletedOnce: the added document is stored ONCE, stamped with this release, and every
// other document is the one stored before, untouched.
func assertCompletedOnce(t *testing.T, data *db.DB, tenantID uuid.UUID, added string, before []policy.BaselineDoc) {
	t.Helper()
	got := baselineRows(t, data, tenantID)
	if s := got[added]; len(s) != 1 || s[0] != policy.BaselineVersion {
		t.Fatalf("the added document is stored as %v, want exactly one version stamped %s", s, policy.BaselineVersion)
	}
	for _, d := range before {
		if s := got[d.Name]; len(s) != 1 || s[0] != preAdditionStamp {
			t.Fatalf("%q is stored as %v, want its one %s version untouched", d.Name, s, preAdditionStamp)
		}
	}
	for _, table := range []string{"policies", "policy_versions", "policy_attachments"} {
		if n := countRows(t, data, tenantID, table); n != len(policy.Baseline()) {
			t.Fatalf("%s holds %d rows, want %d -- one per shipped document", table, n, len(policy.Baseline()))
		}
	}
}

// tapContext is a tap:record context for one evidence shape at a fresh location.
func tapContext(tenantID uuid.UUID, keys map[policy.ContextKey]any) policy.Context {
	base := map[policy.ContextKey]any{
		policy.CtxTapChannel: "nfc", policy.CtxTapSunValid: true, policy.CtxTapGPSConflict: false,
		policy.CtxTapOccurredAtSkewSeconds: float64(0), policy.CtxTapCtrGap: 0,
		policy.CtxTagStatus: "active", policy.CtxEmployeeStatus: "active",
		policy.CtxEmployeeCrossLocation: false,
	}
	for k, v := range keys {
		base[k] = v
	}
	return policy.Context{
		Action: policy.ActionTapRecord, Resources: []string{"location/" + uuid.New().String()},
		Keys: base, SessionTenantID: &tenantID, TagTenantID: &tenantID,
	}
}

// browserShapes are the place-evidence shapes of a browser tap (no tap:mockLocation).
var browserShapes = []map[policy.ContextKey]any{
	{policy.CtxTapIPMatch: true, policy.CtxTapGPSMatch: false, policy.CtxTapClient: "browser"},
	{policy.CtxTapIPMatch: false, policy.CtxTapGPSMatch: true, policy.CtxTapClient: "browser"},
	{policy.CtxTapIPMatch: false, policy.CtxTapGPSMatch: false, policy.CtxTapClient: "browser"},
	{policy.CtxTapChannel: "qr", policy.CtxTapSunValid: false, policy.CtxTapIPMatch: false, policy.CtxTapGPSMatch: true, policy.CtxTapClient: "browser"},
}

// withoutAdded is set minus the added document: the rulebook the business held before.
func withoutAdded(set policy.Set) policy.Set {
	old := set
	old.Baseline = nil
	for _, p := range set.Baseline {
		if p.Document.Statements[0].Sid != policy.SidMockLocationReview {
			old.Baseline = append(old.Baseline, p)
		}
	}
	return old
}

// assertBrowserUnchanged: every browser shape decides under set exactly as under the set the
// business held before -- effect, sid and the version that decided -- and an app tap reporting a
// mock position is a review via the added rule, citing its stored version.
func assertBrowserUnchanged(t *testing.T, tenantID uuid.UUID, set policy.Set) {
	t.Helper()
	if len(set.Baseline) != len(policy.Baseline()) {
		t.Fatalf("the set carries %d baseline documents, want %d", len(set.Baseline), len(policy.Baseline()))
	}
	old := withoutAdded(set)
	for _, ev := range browserShapes {
		c := tapContext(tenantID, ev)
		got, want := policy.Evaluate(set, c), policy.Evaluate(old, c)
		if got.Effect != want.Effect || got.MatchedSid != want.MatchedSid || !samePolicyVersion(got.PolicyVersionID, want.PolicyVersionID) {
			t.Fatalf("%v: a browser tap is %q via %q after the addition, %q via %q before",
				ev, got.Effect, got.MatchedSid, want.Effect, want.MatchedSid)
		}
	}
	d := policy.Evaluate(set, tapContext(tenantID, map[policy.ContextKey]any{
		policy.CtxTapIPMatch: true, policy.CtxTapGPSMatch: true,
		policy.CtxTapClient: "app", policy.CtxTapMockLocation: true,
	}))
	if d.Effect != policy.EffectReview || d.MatchedSid != policy.SidMockLocationReview || d.PolicyVersionID == nil {
		t.Fatalf("an app tap reporting a mock position is %q via %q (version %v), want a review via %s with its stored version",
			d.Effect, d.MatchedSid, d.PolicyVersionID, policy.SidMockLocationReview)
	}
}

// TestPolicySetDB_AnAddedBaselineDocumentReachesAnExistingTenantOnItsNextTap: the tap path. The
// pure stored-set read stays all-or-nothing (StoredSet answers on the guardrails alone while one
// document is missing); the next tap writes the one missing document and nothing else.
func TestPolicySetDB_AnAddedBaselineDocumentReachesAnExistingTenantOnItsNextTap(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	svc := &Service{policies: sets}
	ctx := context.Background()
	added, _ := addedDocAndBefore(t)
	before := seedPreAdditionBaseline(t, sets, tenantID)

	if n := len(svc.StoredSet(ctx, tenantID).Baseline); n != 0 {
		t.Fatalf("StoredSet over a baseline one document short has %d baseline policies; it is all-or-nothing, want 0", n)
	}
	set := sets.forTenant(ctx, tenantID)
	assertCompletedOnce(t, data, tenantID, added, before)
	assertBrowserUnchanged(t, tenantID, set)
	if n := len(svc.StoredSet(ctx, tenantID).Baseline); n != len(policy.Baseline()) {
		t.Fatalf("StoredSet after the next tap has %d baseline policies, want %d", n, len(policy.Baseline()))
	}
}

// writerOver is the production RuleWriter wired to the real Service, as cmd/tappa wires it.
func writerOver(t *testing.T, data *db.DB, svc *Service) *tenant.RuleWriter {
	t.Helper()
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	w, err := tenant.NewRuleWriter(data, trail, svc, policy.DefaultParams(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRuleWriter: %v", err)
	}
	return w
}

// TestPolicySetDB_TheOwnerOfAnExistingBusinessIsNotLockedOutByAnAddedRule: the panel path. Right
// after the deploy, with NO tap, the owner of a business holding the 2026-10-18 baseline may edit
// the rules and invite a manager; the gate's own question completed the baseline with the added
// document, once, through the tap path's materialiser; a manager is still refused (the guardrail);
// asking again writes nothing; and the tap that follows decides browser taps as before.
func TestPolicySetDB_TheOwnerOfAnExistingBusinessIsNotLockedOutByAnAddedRule(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	svc := &Service{policies: sets}
	ctx := context.Background()
	added, _ := addedDocAndBefore(t)
	before := seedPreAdditionBaseline(t, sets, tenantID)
	w := writerOver(t, data, svc)
	owner := tenant.Actor{ID: uuid.New(), Role: "owner"}
	manager := tenant.Actor{ID: uuid.New(), Role: "manager"}

	if !w.May(ctx, tenantID, owner) {
		t.Fatal("the owner is refused policy:edit right after the deploy -- the added rule locked them out")
	}
	assertCompletedOnce(t, data, tenantID, added, before)
	if !w.MayInviteManager(ctx, tenantID, owner) {
		t.Fatal("the owner is refused the manager invitation")
	}
	if w.May(ctx, tenantID, manager) || w.MayInviteManager(ctx, tenantID, manager) {
		t.Fatal("a manager passes the policy:edit gate")
	}
	assertCompletedOnce(t, data, tenantID, added, before) // asked four times, written once

	// The invitation gate alone completes it too (a fresh business, the same state).
	other := newPolicyTenant(t, data)
	seedPreAdditionBaseline(t, sets, other)
	if !w.MayInviteManager(ctx, other, owner) {
		t.Fatal("the owner is refused the manager invitation right after the deploy")
	}
	assertCompletedOnce(t, data, other, added, before)

	assertBrowserUnchanged(t, tenantID, sets.forTenant(ctx, tenantID))
	assertCompletedOnce(t, data, tenantID, added, before) // the tap found nothing to write
}

// TestPolicySetDB_PanelSetProvisionsNobody is StoredSetProvisionsNobody for the gate's read: a
// business with NOTHING stored is not set up by a panel question -- not by policy:edit (still
// refused, the policies screen's state) and not by the invitation gate (decided on the shipped
// baseline, ADR 0031 §5.11). The panel completes a baseline; it never provisions one.
func TestPolicySetDB_PanelSetProvisionsNobody(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	svc := &Service{policies: sets}
	ctx := context.Background()
	w := writerOver(t, data, svc)
	owner := tenant.Actor{ID: uuid.New(), Role: "owner"}

	if set := svc.PanelSet(ctx, tenantID); len(set.Baseline) != 0 || len(set.Guardrails) == 0 {
		t.Fatalf("PanelSet over a business with nothing stored: %d baseline, %d guardrails; want guardrails alone",
			len(set.Baseline), len(set.Guardrails))
	}
	if w.May(ctx, tenantID, owner) {
		t.Fatal("policy:edit is granted on a business with nothing stored")
	}
	if !w.MayInviteManager(ctx, tenantID, owner) {
		t.Fatal("the invitation gate no longer answers a fresh business from the shipped baseline")
	}
	for _, table := range []string{"policies", "policy_versions", "policy_attachments"} {
		if n := countRows(t, data, tenantID, table); n != 0 {
			t.Fatalf("a panel question provisioned the business: %s holds %d rows", table, n)
		}
	}
}

// TestPolicySetDB_PanelAndTapCompletingTheBaselineTogetherWriteItOnce is T121's race with the
// panel in it: on a business one added document short, N callers at once -- half the tap path
// (forTenant), half the panel's gate (PanelSet) -- all get the whole baseline, none logs a
// failure, the added document is written exactly once, and every tap's decision is the one the
// stored rulebook gives.
func TestPolicySetDB_PanelAndTapCompletingTheBaselineTogetherWriteItOnce(t *testing.T) {
	const (
		racers   = 8
		attempts = 10
	)
	data := openPolicyDB(t)
	logs := &lockedBuffer{}
	sets := policySets{data: data, params: policy.DefaultParams(),
		log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))}
	svc := &Service{policies: sets}
	added, _ := addedDocAndBefore(t)
	want := len(policy.Baseline())

	for attempt := 1; attempt <= attempts; attempt++ {
		tenantID := newPolicyTenant(t, data)
		before := seedPreAdditionBaseline(t, sets, tenantID)

		results := make([]policy.Set, racers)
		var ready, done sync.WaitGroup
		start := make(chan struct{})
		for i := range racers {
			ready.Add(1)
			done.Add(1)
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				if i%2 == 0 {
					results[i] = sets.forTenant(context.Background(), tenantID)
				} else {
					results[i] = svc.PanelSet(context.Background(), tenantID)
				}
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		for i, set := range results {
			if len(set.Baseline) != want {
				path := "tap"
				if i%2 == 1 {
					path = "panel"
				}
				t.Fatalf("attempt %d/%d: the %s racer %d got %d baseline documents, want %d. Logged:\n%s",
					attempt, attempts, path, i, len(set.Baseline), want, logs.String())
			}
			if i%2 == 0 {
				assertBrowserUnchanged(t, tenantID, set)
			}
		}
		assertCompletedOnce(t, data, tenantID, added, before)
	}
	if strings.TrimSpace(logs.String()) != "" {
		t.Fatalf("the racers logged at WARN or above:\n%s", logs.String())
	}
}

func samePolicyVersion(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
