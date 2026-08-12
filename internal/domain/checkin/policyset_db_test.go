package checkin

// The policy Set against a REAL Postgres, because the thing under test is a
// database state: whether an ordinary check-in can be RECORDED at all.
//
// 🔴 THE MEASUREMENT THIS FILE EXISTS FOR is TestPolicySetDB_BaselineDecisionIsUnwritableWithoutAVersion
// below. It reproduces, permanently, the state the whole task started from: with
// no materialised baseline, a `policy_layer='baseline'` transaction is refused by
// the composite FK with 23503, which means CLAUDE.md §5's rows 6 and 7 — the
// ordinary `ok` and the ordinary `flag` — could not be persisted. That is a §4.6
// record loss on the most common outcome the product has.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/policy"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newPolicyHarness opens a real pool and creates a fresh tenant with NO policy
// rows — the state every tenant in the dev database is actually in (measured:
// the seeded Kebab Factory tenant has 0 policies and 0 policy_versions).
func newPolicyHarness(t *testing.T) (*db.DB, uuid.UUID, policySets) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping policy-set tests (real Postgres required)")
	}
	cfg := &config.Config{
		Env:             config.EnvDev,
		BaseURL:         "http://localhost:8080",
		DatabaseURL:     dsn,
		SessionHMACKey:  []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"),
		InviteHMACKey:   []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		TagKEK:          make([]byte, 32),
		RetentionYears:  2,
		GPSRadiusMeters: 150,
		Debounce:        60 * time.Second,
	}
	data, err := db.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	tenantID := uuid.New()
	if err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi')`,
			tenantID, "VAT-"+tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("fixture tenant: %v", err)
	}
	return data, tenantID, policySets{data: data, params: policy.DefaultParams(), log: quietLogger()}
}

func countRows(t *testing.T, data *db.DB, tenantID uuid.UUID, table string) int {
	t.Helper()
	var n int
	err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE tenant_id = $1`, tenantID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestPolicySetDB_BaselineDecisionIsUnwritableWithoutAVersion is the measurement,
// kept as a test so it cannot quietly stop being true.
//
// BEFORE materialising: an ok/flag row — a BASELINE decision — is refused by the
// database, whichever version id it names, because none exists. AFTER: the very
// same row inserts. Nothing about the tap changed; what changed is that the
// tenant now has the policy layer the schema requires a baseline decision to cite.
func TestPolicySetDB_BaselineDecisionIsUnwritableWithoutAVersion(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	ctx := context.Background()

	locationID, employeeID := uuid.New(), uuid.New()
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'St Julians')`,
			locationID, tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, 'Maria Borg', 'active')`,
			employeeID, tenantID, locationID)
		return e
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	insertBaselineRow := func(versionID uuid.UUID) error {
		return data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `
				INSERT INTO transactions (tenant_id, employee_id, location_id, type, occurred_at,
				    verdict, channel, practice, queued, policy_version_id, matched_sid, policy_layer)
				VALUES ($1, $2, $3, 'in', now(), 'ok', 'nfc', false, false, $4, 'base:ip-or-gps-ok', 'baseline')`,
				tenantID, employeeID, locationID, versionID)
			return e
		})
	}

	// BEFORE. A plausible id, and the uuid.Nil the N4 hand-off warned about.
	for name, id := range map[string]uuid.UUID{"a random version id": uuid.New(), "uuid.Nil": uuid.Nil} {
		err := insertBaselineRow(id)
		if err == nil {
			t.Fatalf("%s: a baseline decision was written with no policy_versions row behind it", name)
		}
		var pgErr *pgconn.PgError
		if !asPgError(err, &pgErr) || pgErr.Code != "23503" {
			t.Fatalf("%s: error = %v, want 23503 foreign_key_violation", name, err)
		}
	}

	// MATERIALISE, exactly as the first tap for this tenant would.
	set := sets.forTenant(ctx, tenantID)
	if len(set.Baseline) != len(policy.Baseline()) {
		t.Fatalf("baseline has %d documents, want %d", len(set.Baseline), len(policy.Baseline()))
	}

	// AFTER. The same row, citing a version that now exists.
	if err := insertBaselineRow(set.Baseline[0].VersionID); err != nil {
		t.Fatalf("a baseline decision is STILL unwritable after materialising: %v", err)
	}
}

// asPgError is errors.As without dragging the import into every call site.
func asPgError(err error, target **pgconn.PgError) bool {
	for err != nil {
		if pe, ok := err.(*pgconn.PgError); ok {
			*target = pe
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestPolicySetDB_MaterialisingIsIdempotent. policy_versions is append-only at two
// levels, so "write it again" must be a NO-OP rather than a second row — running
// the tap path a hundred times must not leave a hundred versions of the baseline.
func TestPolicySetDB_MaterialisingIsIdempotent(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	ctx := context.Background()
	want := len(policy.Baseline())

	for i := 0; i < 5; i++ {
		set := sets.forTenant(ctx, tenantID)
		if len(set.Baseline) != want {
			t.Fatalf("run %d: baseline has %d documents, want %d", i, len(set.Baseline), want)
		}
	}

	if got := countRows(t, data, tenantID, "policies"); got != want {
		t.Fatalf("policies = %d after five runs, want %d", got, want)
	}
	if got := countRows(t, data, tenantID, "policy_versions"); got != want {
		t.Fatalf("policy_versions = %d after five runs, want %d — append-only means a repeat must be a no-op", got, want)
	}
	if got := countRows(t, data, tenantID, "policy_attachments"); got != want {
		t.Fatalf("policy_attachments = %d, want %d", got, want)
	}

	// The version ids must also be STABLE across runs: a record pins one, and an
	// id that changed between taps would make two identical decisions cite two
	// different versions.
	first := sets.forTenant(ctx, tenantID)
	second := sets.forTenant(ctx, tenantID)
	for i := range first.Baseline {
		if first.Baseline[i].VersionID != second.Baseline[i].VersionID {
			t.Fatalf("version id for %q changed between runs", first.Baseline[i].Document.Name)
		}
		if first.Baseline[i].Document.Name != second.Baseline[i].Document.Name {
			t.Fatalf("the baseline came back in a different ORDER: the evaluator's tiebreak would drift")
		}
	}
}

// TestPolicySetDB_ConcurrentFirstTapsMaterialiseOnce. A shift change is many taps
// at once, and for a tenant that has never tapped they are all "the first one".
// The deterministic ids plus ON CONFLICT DO NOTHING are what turn that into one
// set of rows instead of N.
func TestPolicySetDB_ConcurrentFirstTapsMaterialiseOnce(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	const racers = 8
	want := len(policy.Baseline())

	var wg sync.WaitGroup
	sizes := make([]int, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sizes[i] = len(sets.forTenant(context.Background(), tenantID).Baseline)
		}(i)
	}
	wg.Wait()

	for i, n := range sizes {
		if n != want {
			t.Fatalf("racer %d assembled %d baseline documents, want %d", i, n, want)
		}
	}
	if got := countRows(t, data, tenantID, "policies"); got != want {
		t.Fatalf("policies = %d after %d concurrent first taps, want %d", got, racers, want)
	}
	if got := countRows(t, data, tenantID, "policy_versions"); got != want {
		t.Fatalf("policy_versions = %d, want %d", got, want)
	}
}

// TestPolicySetDB_DisabledBaselineIsExcludedAndNotResurrected. enabled=false is
// the tenant's own off switch (ADR 0004, 00007). Two things must hold and they
// pull in opposite directions: the disabled statement must LEAVE the Set, and it
// must NOT be treated as missing — otherwise every tap would re-materialise it
// forever and the switch would do nothing.
func TestPolicySetDB_DisabledBaselineIsExcludedAndNotResurrected(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	ctx := context.Background()
	want := len(policy.Baseline())

	sets.forTenant(ctx, tenantID) // materialise

	disabled := policy.Baseline()[0].Name
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE policies SET enabled = false WHERE tenant_id = $1 AND name = $2`,
			tenantID, disabled)
		return e
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	set := sets.forTenant(ctx, tenantID)
	if len(set.Baseline) != want-1 {
		t.Fatalf("baseline has %d documents, want %d: a disabled policy must leave the Set", len(set.Baseline), want-1)
	}
	for _, p := range set.Baseline {
		if p.Document.Name == disabled {
			t.Fatalf("the disabled policy %q is still being evaluated", disabled)
		}
	}
	if got := countRows(t, data, tenantID, "policy_versions"); got != want {
		t.Fatalf("policy_versions = %d, want %d: a disabled policy was re-materialised", got, want)
	}
}

// TestPolicySetDB_ACorruptStoredDocumentFallsBackToGuardrailsAlone.
//
// A PARTIAL BASELINE IS A DIFFERENT POLICY, NOT A SAFER ONE. Drop
// base:qr-requires-ip while keeping base:gps-only-allow and a QR tap with only a
// GPS fix becomes `ok` instead of `flag` — the Q15 decision, silently reversed.
// So when any enabled baseline document cannot be evaluated, the whole layer is
// dropped and every tap lands on the fail-to-review default: still RECORDED
// (§4.6), still in front of a human, never quietly approved.
func TestPolicySetDB_ACorruptStoredDocumentFallsBackToGuardrailsAlone(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	ctx := context.Background()

	sets.forTenant(ctx, tenantID) // materialise a good baseline

	// Append a version 2 that will not validate. policy_versions is append-only,
	// so this is the only shape such a state can take — and it is the shape a
	// future authoring bug would produce.
	name := policy.Baseline()[0].Name
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var policyID uuid.UUID
		if e := tx.QueryRow(ctx, `SELECT id FROM policies WHERE tenant_id = $1 AND name = $2`,
			tenantID, name).Scan(&policyID); e != nil {
			return e
		}
		junk, _ := json.Marshal(map[string]any{
			"version": "x", "name": name,
			"statements": []map[string]any{{"sid": "base:broken", "effect": "teleport", "action": []string{"tap:record"}, "resource": []string{"*"}}},
		})
		_, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (tenant_id, policy_id, version_no, document) VALUES ($1, $2, 2, $3)`,
			tenantID, policyID, junk)
		return e
	}); err != nil {
		t.Fatalf("inject: %v", err)
	}

	set := sets.forTenant(ctx, tenantID)
	if len(set.Baseline) != 0 {
		t.Fatalf("baseline has %d documents with one unusable: a PARTIAL baseline is a different policy, "+
			"so the whole layer must be dropped", len(set.Baseline))
	}
	if len(set.Guardrails) == 0 {
		t.Fatal("the guardrails were dropped too: the red lines must survive any policy problem")
	}

	// POSITIVE CONTROL: the fallback really is fail-to-review rather than a silent
	// pass. Nothing matches, so tap:record defaults to review -> a RECORDED flag.
	dec := policy.Evaluate(set, policy.Context{
		Action:    policy.ActionTapRecord,
		Resources: []string{"location/" + uuid.New().String()},
		Keys: map[policy.ContextKey]any{
			policy.CtxTapChannel: "nfc", policy.CtxTapSunValid: true,
			policy.CtxTapIPMatch: true, policy.CtxTapGPSMatch: false,
			policy.CtxTagStatus: "active", policy.CtxEmployeeStatus: "active",
		},
		SessionTenantID: &tenantID,
		TagTenantID:     &tenantID,
	})
	if dec.Effect != policy.EffectReview || dec.MatchedSid != "default" {
		t.Fatalf("fallback decision = %s/%s, want review/default", dec.Effect, dec.MatchedSid)
	}
}

// TestPolicySetDB_MaterialisedDocumentsAreTheShippedOnes. What is written must be
// what internal/policy declares, byte for byte after a round trip — otherwise a
// record pins a version whose rules are not the rules that decided it.
func TestPolicySetDB_MaterialisedDocumentsAreTheShippedOnes(t *testing.T) {
	_, tenantID, sets := newPolicyHarness(t)
	set := sets.forTenant(context.Background(), tenantID)

	shipped := policy.Baseline()
	if len(set.Baseline) != len(shipped) {
		t.Fatalf("baseline has %d documents, want %d", len(set.Baseline), len(shipped))
	}
	for i, want := range shipped {
		got := set.Baseline[i]
		if got.Layer != policy.LayerBaseline {
			t.Fatalf("%s: layer = %q", want.Name, got.Layer)
		}
		if got.Document.Version != policy.BaselineVersion {
			t.Fatalf("%s: version = %q, want %q", want.Name, got.Document.Version, policy.BaselineVersion)
		}
		gotJSON, _ := json.Marshal(got.Document)
		wantJSON, _ := json.Marshal(want.Document)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("%s: the stored document is not the shipped one\n got: %s\nwant: %s",
				want.Name, gotJSON, wantJSON)
		}
	}
}

// TestPolicySetDB_TenantLayerIsEmptyToday holds the honest statement to its word:
// nothing in this repo can author a tenant policy yet (the panel is M6-09), so
// the tenant layer is empty in every deployment as it stands — and the assembly
// still reads it, so the day something can write one it is applied rather than
// silently ignored.
func TestPolicySetDB_TenantLayerIsEmptyToday(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	ctx := context.Background()

	if got := sets.forTenant(ctx, tenantID); len(got.Tenant) != 0 {
		t.Fatalf("tenant layer has %d policies on a fresh tenant", len(got.Tenant))
	}

	// POSITIVE CONTROL: a tenant policy written directly IS picked up, so the
	// emptiness above is a fact about the product's write paths and not about the
	// loader ignoring the layer.
	doc := policy.Document{
		Version: "2026-07-31", Name: "Night shift review",
		Statements: []policy.Statement{{
			Sid: "tenant:night-review", Effect: policy.EffectReview,
			Action: []policy.Action{policy.ActionTapRecord}, Resource: []string{"location/*"},
			Condition: policy.Condition{policy.OpBool: {policy.CtxTapIPMatch: false}},
			Reason:    "night taps are reviewed",
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var policyID uuid.UUID
		if e := tx.QueryRow(ctx,
			`INSERT INTO policies (tenant_id, name, layer) VALUES ($1, 'Night shift review', 'tenant') RETURNING id`,
			tenantID).Scan(&policyID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (tenant_id, policy_id, version_no, document) VALUES ($1, $2, 1, $3)`,
			tenantID, policyID, raw)
		return e
	}); err != nil {
		t.Fatalf("write a tenant policy: %v", err)
	}

	got := sets.forTenant(ctx, tenantID)
	if len(got.Tenant) != 1 {
		t.Fatalf("tenant layer has %d policies, want 1: a stored tenant policy is being ignored", len(got.Tenant))
	}
	if got.Tenant[0].Document.Statements[0].Sid != "tenant:night-review" {
		t.Fatalf("wrong tenant policy loaded: %+v", got.Tenant[0].Document)
	}
}

// TestPolicySetDB_StoredSetIsAllOrNothing pins the property the PANEL's gate rests on
// against the REAL method.
//
// 🔴 THE BRAKE THIS COVERS IS DELETABLE WITHOUT IT. stored() reports a missing or
// unparseable SHIPPED document by returning the guardrails ALONE — a partial baseline
// is a DIFFERENT policy, not a weaker one. Drop that (take assembleBaseline's second
// return value and use the partial slice anyway) and three packages stay green:
// StoredSet's only other test runs against a VIRGIN tenant, where assembleBaseline
// returns nothing regardless, and the panel test that produces the corrupt-document
// state runs against a DOUBLE. So "an owner is refused when one document cannot be
// read" was proven only where it was simulated.
//
// TWO STATES, because they fail differently: a rulebook with a document MISSING, and
// one with a document STORED BUT UNREADABLE. The second is the one the panel's screen
// shouts about, and the one that cannot self-heal (materialising is a no-op once the
// version exists).
func TestPolicySetDB_StoredSetIsAllOrNothing(t *testing.T) {
	shipped := policy.Baseline()
	if len(shipped) < 2 {
		t.Fatalf("this binary ships %d baseline document(s); the probe needs at least two",
			len(shipped))
	}

	t.Run("one document missing", func(t *testing.T) {
		data, tenantID, sets := newPolicyHarness(t)
		svc := &Service{policies: sets}
		// Everything EXCEPT the last shipped document.
		for _, doc := range shipped[:len(shipped)-1] {
			seedPolicyDocument(t, data, tenantID, doc.Name, doc.Document)
		}
		set := svc.StoredSet(context.Background(), tenantID)
		if len(set.Baseline) != 0 {
			t.Errorf("a rulebook missing one shipped document returned %d baseline policies; "+
				"a PARTIAL baseline is a different policy, so the answer is the guardrails alone",
				len(set.Baseline))
		}
		if len(set.Guardrails) == 0 {
			t.Error("no guardrails either; the fallback is guardrails-ALONE, not nothing")
		}
	})

	t.Run("one document stored but unreadable", func(t *testing.T) {
		data, tenantID, sets := newPolicyHarness(t)
		svc := &Service{policies: sets}
		for _, doc := range shipped {
			seedPolicyDocument(t, data, tenantID, doc.Name, doc.Document)
		}
		// POSITIVE CONTROL FIRST: with all nine readable, the set is NOT empty. Without
		// it, "0 baseline policies" below would also be what a broken seed produces.
		if n := len(svc.StoredSet(context.Background(), tenantID).Baseline); n != len(shipped) {
			t.Fatalf("a fully provisioned business returned %d baseline policies, want %d; the "+
				"seed is wrong and the assertion below would pass for the wrong reason",
				n, len(shipped))
		}
		// Now append a LATER version this release cannot validate. It has to be a new
		// version: policy_versions takes no UPDATE from anybody.
		corruptLatestVersion(t, data, tenantID, shipped[0].Name)
		set := svc.StoredSet(context.Background(), tenantID)
		if len(set.Baseline) != 0 {
			t.Errorf("a rulebook with ONE unreadable document returned %d baseline policies. "+
				"This is the state the panel tells a customer about (\"judged by the Tappa "+
				"guarantees alone\") and the state in which an OWNER is refused policy:edit; "+
				"if the engine kept the other eight here, both claims would be false.",
				len(set.Baseline))
		}
	})
}

// seedPolicyDocument stores one baseline document as version 1.
func seedPolicyDocument(t *testing.T, data *db.DB, tenantID uuid.UUID, name string, doc policy.Document) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal %q: %v", name, err)
	}
	err = data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if e := tx.QueryRow(ctx,
			`INSERT INTO policies (tenant_id, name, layer) VALUES ($1, $2, 'baseline') RETURNING id`,
			tenantID, name).Scan(&id); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (tenant_id, policy_id, version_no, document)
			 VALUES ($1, $2, 1, $3)`, tenantID, id, raw)
		return e
	})
	if err != nil {
		t.Fatalf("seeding %q: %v", name, err)
	}
}

// corruptLatestVersion appends a version whose document this release cannot validate.
func corruptLatestVersion(t *testing.T, data *db.DB, tenantID uuid.UUID, name string) {
	t.Helper()
	err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if e := tx.QueryRow(ctx,
			`SELECT id FROM policies WHERE tenant_id = $1 AND layer = 'baseline' AND name = $2`,
			tenantID, name).Scan(&id); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (tenant_id, policy_id, version_no, document)
			 VALUES ($1, $2, 2, '{}'::jsonb)`, tenantID, id)
		return e
	})
	if err != nil {
		t.Fatalf("appending an unreadable version to %q: %v", name, err)
	}
}

// TestPolicySetDB_StoredSetProvisionsNobody is the measurement the PANEL's own tests
// were believed to be making and were not.
//
// 🔴 IT EXERCISES THE PRODUCTION METHOD. Service.StoredSet is what cmd/tappa wires the
// policy gate to, and until this test existed NOTHING called it: every test of the
// gate went through a double in internal/domain/tenant, and a double does not
// materialise because it was not written to. An audit measured the cost by making
// StoredSet an alias for forTenant -- a one-line change -- and `make test` stayed
// 17/17 green with -race against real Postgres. With that regression in place, a
// REFUSED manager's POST would write 9 policies, 9 policy_versions and 9
// policy_attachments into a business: policy_versions is append-only (§4.3) and
// policies GRANTs no DELETE (§4.6), so none of it could ever be taken back.
//
// ⚠️ AND THE TEST THAT WAS SUPPOSED TO COVER IT CANNOT.
// tenant.TestRuleWriterDB_TheEngineRefusesAManagerAndAllowsTheOwner counts the three
// tables around a refused request -- but its fixture now seeds all nine shipped
// documents (it has to; the stored set is all-or-nothing), and forTenant only writes
// what is MISSING. With nine of nine present there is nothing to materialise, so that
// count cannot fall against the very substitution it was cited as ruling out. The
// falsifier has to run where the writing would happen, which is here.
//
// WHAT IS ASSERTED: a tenant with no policy rows still has none after StoredSet, and
// the Set that comes back is the guardrails alone -- fail-safe, and provisioning
// nobody.
func TestPolicySetDB_StoredSetProvisionsNobody(t *testing.T) {
	data, tenantID, sets := newPolicyHarness(t)
	svc := &Service{policies: sets}

	before := [3]int{
		countRows(t, data, tenantID, "policies"),
		countRows(t, data, tenantID, "policy_versions"),
		countRows(t, data, tenantID, "policy_attachments"),
	}
	if before != [3]int{0, 0, 0} {
		t.Fatalf("the fixture tenant already holds %v policy rows; this probe needs a virgin "+
			"tenant or it cannot tell writing from not writing", before)
	}

	set := svc.StoredSet(context.Background(), tenantID)

	after := [3]int{
		countRows(t, data, tenantID, "policies"),
		countRows(t, data, tenantID, "policy_versions"),
		countRows(t, data, tenantID, "policy_attachments"),
	}
	if after != before {
		t.Errorf("StoredSet wrote policy rows: %v -> %v.\n"+
			"It is the read the AUTHORISATION gate uses, so an admin who is about to be "+
			"REFUSED must not provision the business on the way -- into two tables nothing "+
			"in this product can clean up.", before, after)
	}
	if len(set.Baseline) != 0 || len(set.Tenant) != 0 {
		t.Errorf("StoredSet returned %d baseline and %d tenant policies for a business with no "+
			"rows; it must fall back to the guardrails alone",
			len(set.Baseline), len(set.Tenant))
	}
	if len(set.Guardrails) == 0 {
		t.Error("StoredSet returned no guardrails either; the fallback is guardrails-ALONE, " +
			"not nothing")
	}

	// POSITIVE CONTROL: the counter can see a write, and forTenant IS the method that
	// makes one. Without this, "after == before" would also be what a broken counter
	// says. It runs second so the assertions above are made against a virgin tenant.
	sets.forTenant(context.Background(), tenantID)
	wrote := [3]int{
		countRows(t, data, tenantID, "policies"),
		countRows(t, data, tenantID, "policy_versions"),
		countRows(t, data, tenantID, "policy_attachments"),
	}
	if wrote == before {
		t.Fatalf("forTenant wrote nothing either (%v), so the comparison above proves nothing "+
			"about StoredSet", wrote)
	}
	// THE LABEL IS DERIVED, NOT ASSUMED: this line printed "(unchanged)" even on the run
	// where the mutation made it [0 0 0] -> [9 9 9], which is a log that argues with its
	// own numbers.
	state := "unchanged"
	if after != before {
		state = "CHANGED"
	}
	t.Logf("StoredSet: %v -> %v (%s); forTenant on the same tenant: %v", before, after, state, wrote)
}
