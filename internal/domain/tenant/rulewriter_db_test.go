package tenant

// rulewriter_db_test.go -- M6-09 phase B's WRITE side against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and that is not a slogan here: the gate's
// answer for an OWNER depends on rows in the database (the shipped authorization
// document has to be stored for the owner's allow to exist), the append-only rule is
// enforced by a GRANT and a TRIGGER, the version race is a unique index, and §4.5 is
// RLS. Every one of those is a property a mock agrees with unconditionally.
//
// FIXTURES ARE NOT CLEANED UP, for rulebook_db_test.go's reason: policies cannot be
// DELETEd by tappa_app (00007 revokes it) and policy_versions cannot be touched at
// all. Fresh random ids keep runs from colliding; `make db-reset` clears the
// development database.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

// storedAuthority is the PolicyAuthority a RuleWriter gets in these tests: the
// tenant's OWN stored rulebook, assembled the way checkin.StoredSet assembles it and
// materialising nothing.
//
// ⚠️ IT IS A SECOND ASSEMBLER AND THAT IS A COUNTED COST OF THE TEST, NOT OF THE
// PRODUCT. Production passes checkin.Service, which is the one assembler; importing
// that package here would be an import cycle in the making (checkin already reaches
// for tenant-shaped things), so the test rebuilds the two lines it needs from the
// SAME query the engine reads (ListPolicySet) and the SAME parser. What it must not
// do is invent a policy set out of Go literals -- then the gate would be measured
// against a rulebook no database ever held.
type storedAuthority struct {
	data   Database
	params policy.Params
}

func (s storedAuthority) StoredSet(ctx context.Context, tenantID uuid.UUID) policy.Set {
	set := policy.Set{Guardrails: policy.Guardrails(s.params)}
	var rows []store.ListPolicySetRow
	if err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		rows, e = store.New(tx).ListPolicySet(ctx, tenantID)
		return e
	}); err != nil {
		return set
	}
	// 🔴 ALL OR NOTHING ON THE BASELINE, BECAUSE PRODUCTION IS. checkin's stored-set
	// read reports an unparseable or absent SHIPPED document as missing and then
	// returns the GUARDRAILS ALONE -- a partial baseline is a different policy, not a
	// weaker one. The first version of this double skipped bad documents one by one,
	// which made it disagree with the thing it stands in for on the one property a
	// test of the gate cares about: an owner whose rulebook has one corrupt document is
	// refused in production and was allowed here. Measured when the test for that state
	// was written and came back green for the wrong reason.
	stored := map[string]store.ListPolicySetRow{}
	for _, r := range rows {
		if policy.Layer(r.Layer) == policy.LayerBaseline {
			stored[r.Name] = r
		}
	}
	for _, doc := range policy.Baseline() {
		row, ok := stored[doc.Name]
		if !ok {
			return policy.Set{Guardrails: set.Guardrails}
		}
		if !row.Enabled {
			// A deliberate opt-out costs nothing, exactly as assembleBaseline treats it.
			continue
		}
		parsed, err := policy.ParseAndValidate(row.Document, policy.LayerBaseline, policy.DefaultLimits())
		if err != nil {
			return policy.Set{Guardrails: set.Guardrails}
		}
		set.Baseline = append(set.Baseline, policy.Policy{
			VersionID: row.VersionID, Layer: policy.LayerBaseline, Document: parsed,
		})
	}
	// 🔴 THE TENANT LAYER IS SORTED (name, id) LIKE PRODUCTION'S. checkin.tenantPolicies
	// does it so the evaluator's full-tie break — first encountered in a fixed walk — is
	// deterministic rather than dependent on how uuids happened to sort. It does not
	// change the gate's ANSWER (policy:edit is decided by the guardrail or the baseline
	// authz document), but a double that drops a determinism property of the thing it
	// stands in for is a double that will disagree the first time a test cares.
	var own []store.ListPolicySetRow
	for _, r := range rows {
		if policy.Layer(r.Layer) == policy.LayerTenant && r.Enabled {
			own = append(own, r)
		}
	}
	sort.Slice(own, func(i, j int) bool {
		if own[i].Name != own[j].Name {
			return own[i].Name < own[j].Name
		}
		return own[i].PolicyID.String() < own[j].PolicyID.String()
	})
	for _, r := range own {
		doc, err := policy.ParseAndValidate(r.Document, policy.LayerTenant, policy.DefaultLimits())
		if err != nil {
			continue
		}
		set.Tenant = append(set.Tenant, policy.Policy{
			VersionID: r.VersionID, Layer: policy.LayerTenant, Document: doc,
		})
	}
	return set
}

// writerFixture is a rulebookFixture plus the writer under test.
type writerFixture struct {
	*rulebookFixture
	writer *RuleWriter
	owner  Actor
	// manager is a SECOND admin of the SAME business, with the role that the
	// guardrail refuses. It is a real row rather than a made-up id because the role
	// this product reads is the one in admin_users.
	manager Actor
}

// raceRacers is how many goroutines the concurrency probes run.
//
// 🔴 THE POOL IS WIDENED TO MATCH, WHICH IS THIS REPOSITORY'S OWN RECIPE
// (internal/sun/advance_test.go and internal/db/invites_test.go both do it, with the
// same reason: "without pool_max_conns the goroutines would queue on pgx's small
// default pool"). It was NOT done here for a round, and the cost was measured rather
// than guessed: with the default pool of max(4, NumCPU), only NumCPU racers are truly
// concurrent, so on a 16-core machine 16 racers could not falsify a broken guard while
// 64 could, and an auditor's 16 on other hardware did. Pinning the pool makes the
// probe's concurrency a property of the TEST rather than of the machine it runs on.
//
// ⚠️ 32 RATHER THAN 64, AND THE CEILING IS THE SERVER'S RATHER THAN THE PROBE'S.
// Postgres here runs with max_connections=100 and `make test` runs packages in
// PARALLEL, so a 68-connection pool plus internal/sun's 54 plus everything else
// exhausts the slots -- measured, as `sorry, too many clients already` failing the SUN
// counter races, a package this change had not touched.
//
// 🔴 WHAT 32 COSTS, SAID PLAINLY: as a FALSIFIER it is intermittent. With the index
// dropped, three runs gave 1 / 1 / 3 rows -- so a single green run at 32 would not
// prove the guard sound. At 64 the same mutation gave 16 rows three times out of
// three, and that is the measurement migration 00015's argument rests on. As a
// REGRESSION test the count does not matter: with the index present every run leaves
// exactly one row, and losing the index is what this is here to notice.
const raceRacers = 32

func newWriterFixture(t *testing.T) *writerFixture {
	return newWriterFixtureWithPool(t, 0)
}

// newRaceWriterFixture widens the pool so the probes below are really concurrent. It
// is a SEPARATE constructor because the widened pool must not reach the ordinary
// tests: `make test` runs packages in parallel against one Postgres with
// max_connections=100, and a package-wide widening was measured taking the SUN counter
// races down with `sorry, too many clients already`.
func newRaceWriterFixture(t *testing.T) *writerFixture {
	return newWriterFixtureWithPool(t, raceRacers+4)
}

func newWriterFixtureWithPool(t *testing.T, poolMax int) *writerFixture {
	t.Helper()
	base := newRulebookFixtureWithPool(t, poolMax)
	trail, err := audit.New(base.data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	w, err := NewRuleWriter(base.data, trail,
		storedAuthority{data: base.data, params: testParams()},
		testParams(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRuleWriter: %v", err)
	}
	f := &writerFixture{
		rulebookFixture: base,
		writer:          w,
		owner:           Actor{ID: base.adminID, Role: "owner"},
	}
	f.manager = Actor{ID: f.seedManager(t, base.tenantID), Role: "manager"}
	return f
}

// seedManager writes a real manager row.
//
// ⚠️ THE ROW IS NOT WHAT THE GATE READS, AND SAYING SO MATTERS. Actor.Role reaches the
// writer as a Go value from the caller; in production that value is filled by
// adminauth from admin_users.role during session resolution, and NOTHING in this
// package's tests exercises that chain — so "the role comes from the database" is
// pinned in internal/adminauth and by the handler's actorOf, not here. This row exists
// so the fixture describes a real business (an owner and a manager) rather than to
// prove where the role came from.
func (f *writerFixture) seedManager(t *testing.T, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
			 VALUES ($1, $2, 'Joe Manager', $3, $4, 'manager')`,
			id, tenantID, "joe-"+id.String()+"@example.test", fixtures.UnusablePasswordHash)
		return e
	})
	if err != nil {
		t.Fatalf("seed manager: %v", err)
	}
	return id
}

// seedAuthzDocument stores EVERY shipped baseline document for a tenant and returns
// the id of the one that grants policy:edit.
//
// 🔴 ALL NINE, NOT JUST THE AUTHORIZATION ONE, AND THAT IS NOT THOROUGHNESS. The
// stored set is all-or-nothing: a business missing ANY shipped document falls back to
// the guardrails alone, so a fixture holding one document is a fixture in which the
// owner is refused for a reason the test did not intend. It reproduces a PROVISIONED
// business, which is the state every one of these tests means to be in.
func (f *writerFixture) seedAuthzDocument(t *testing.T, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	var authz uuid.UUID
	for _, doc := range policy.Baseline() {
		raw, err := json.Marshal(doc.Document)
		if err != nil {
			t.Fatalf("marshal %q: %v", doc.Name, err)
		}
		id := f.storePolicy(t, tenantID, doc.Name, "baseline", true, raw, nil)
		if mentionsPolicyEdit(doc) {
			authz = id
		}
	}
	if authz == uuid.Nil {
		t.Fatal("no shipped baseline document grants policy:edit; the gate's premise is gone")
	}
	return authz
}

func mentionsPolicyEdit(doc policy.BaselineDoc) bool {
	for _, st := range doc.Document.Statements {
		for _, a := range st.Action {
			if a == policy.ActionPolicyEdit {
				return true
			}
		}
	}
	return false
}

// baselineID finds a shipped baseline policy this fixture already seeded.
//
// 🔴 THE TESTS ASK FOR IT RATHER THAN STORING IT AGAIN, and migration 00015 is why:
// (tenant_id, layer, name) is UNIQUE now, so a second INSERT of a shipped name is a
// 23505 -- which is the index doing exactly what it was added for, on the fixtures
// first. Reading the row back is also closer to production, where these nine rows are
// written once by provisioning.
func (f *writerFixture) baselineID(t *testing.T, tenantID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id FROM policies WHERE tenant_id = $1 AND layer = 'baseline' AND name = $2`,
			tenantID, name).Scan(&id)
	})
	if err != nil {
		t.Fatalf("reading the seeded baseline policy %q: %v", name, err)
	}
	return id
}

// appendVersion adds a further version to a policy, which is the ONLY way to change
// what it says (policy_versions is append-only by privilege and by trigger).
func (f *writerFixture) appendVersion(t *testing.T, tenantID, policyID uuid.UUID, no int, raw []byte) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document)
			 VALUES ($1, $2, $3, $4, $5)`, uuid.New(), tenantID, policyID, no, raw)
		return e
	})
	if err != nil {
		t.Fatalf("appending version %d: %v", no, err)
	}
}

// auditRows reads this tenant's trail for one action.
func (f *writerFixture) auditRows(t *testing.T, tenantID uuid.UUID, action string) []string {
	t.Helper()
	var out []string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2 ORDER BY at`,
			tenantID, action)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var d string
			if e := rows.Scan(&d); e != nil {
				return e
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading the trail: %v", err)
	}
	return out
}

// TestRuleWriterDB_TheEngineRefusesAManagerAndAllowsTheOwner -- obligation 6, the one
// three tasks in a row deferred.
//
// 🔴 BOTH HALVES ARE MEASURED AGAINST THE SAME ROWS, which is what makes either half
// worth anything. A test that only showed the manager refused would agree with a gate
// that refuses everybody -- including the owner, which is the SPECIFIC failure the
// engine's own comment warns about ("without an owner allow here the owner's
// policy:edit would fall to the fail-closed default deny and the owner could never
// configure anything -- a lockout"). A naive gate that evaluated an EMPTY Set, which
// is what internal/domain/tenant/rulebook.go's defaultEffectFor does on purpose,
// produces exactly that.
func TestRuleWriterDB_TheEngineRefusesAManagerAndAllowsTheOwner(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	name, _ := shippedDoc(t, 0)
	target := f.baselineID(t, f.tenantID, name)

	// THE MANAGER. Refused, and NOTHING is written -- neither the policy row nor a
	// baseline materialised on the way to being refused.
	before := f.counts(t, f.tenantID)
	if _, err := f.writer.SetEnabled(context.Background(), EnableCommand{
		TenantID: f.tenantID, Actor: f.manager, PolicyID: target, Enabled: false,
	}); !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("a manager's SetEnabled returned %v, want ErrNotPermitted", err)
	}
	// ⚠️ WHAT THIS COUNT DOES AND DOES NOT RULE OUT, corrected after an audit measured
	// the difference. It shows that a refused request through THIS writer changes no
	// policy row — worth keeping. It does NOT rule out the substitution it was cited
	// for (a gate reading through checkin's MATERIALISING forTenant), for two reasons
	// measured in the same round: the fixture seeds all nine shipped documents before
	// this point, and forTenant only writes what is MISSING, so with nine of nine
	// present there is nothing to materialise and the count cannot move; and the
	// authority behind this writer is a test DOUBLE, which does not materialise because
	// it was never written to. The falsifier lives where the writing would happen:
	// checkin.TestPolicySetDB_StoredSetProvisionsNobody, which calls the production
	// method against a virgin tenant and goes red when StoredSet is aliased to
	// forTenant (measured — a one-line change that left `make test` 17/17 green before
	// that test existed).
	if after := f.counts(t, f.tenantID); after != before {
		t.Errorf("a REFUSED request changed the policy tables: %v -> %v.", before, after)
	}
	if got := f.enabledOf(t, target); !got {
		t.Error("the manager's refused request switched the rule off anyway")
	}

	// THE OWNER. Allowed, and the row really moves.
	if _, err := f.writer.SetEnabled(context.Background(), EnableCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: target, Enabled: false,
	}); err != nil {
		t.Fatalf("the OWNER's SetEnabled failed: %v.\nThis is the lockout the engine's own "+
			"baseline comment warns about: sys:policy-edit-owner-only only DENIES "+
			"non-owners, so an owner denied here means the gate is not reading the stored "+
			"authorization document.", err)
	}
	if f.enabledOf(t, target) {
		t.Error("the owner's SetEnabled reported success and the row is still enabled")
	}
}

// TestRuleWriterDB_AnOwnerWithNoStoredRulebookIsRefused -- the counted bound, measured
// rather than asserted in prose.
//
// The owner's permission lives in a STORED document. A business whose baseline has
// never been materialised has no such document, so its owner is refused -- which is
// fail-closed and correct, and is also the reason the screen tells an owner "the
// engine does not currently grant you policy:edit" rather than "only owners may".
func TestRuleWriterDB_AnOwnerWithNoStoredRulebookIsRefused(t *testing.T) {
	f := newWriterFixture(t)
	if f.writer.May(context.Background(), f.tenantID, f.owner) {
		t.Fatal("an owner of a business with NO stored policy rows was allowed policy:edit; " +
			"the allow can only come from a stored document, so this means the gate is " +
			"answering from somewhere else")
	}
	f.seedAuthzDocument(t, f.tenantID)
	if !f.writer.May(context.Background(), f.tenantID, f.owner) {
		t.Fatal("the same owner is STILL refused after the authorization document is stored; " +
			"the two halves of this test are the positive control for each other")
	}
}

// TestRuleWriterDB_AnOwnerIsRefusedWhenOneStoredDocumentCannotBeRead.
//
// 🔴 THE STATE THE SCREEN'S WORST SENTENCE WAS REACHED FROM, produced rather than
// argued about. checkin's stored-set read is ALL-OR-NOTHING: one baseline document
// this release cannot parse makes the whole layer fall back to the guardrails alone,
// which drops the AUTHORIZATION document with it — so an OWNER, whose allow lives in
// that document, is refused policy:edit. For a round the panel answered that owner
// with "changing the rules is reserved for the organisation owner", which is both
// false about them and points at the wrong problem.
//
// This pins the DOMAIN half: the owner really is refused, and the refusal is recorded
// as a rulebook-stage refusal with rules stored (not zero) — the two facts the
// screen's corrected sentence rests on.
func TestRuleWriterDB_AnOwnerIsRefusedWhenOneStoredDocumentCannotBeRead(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	if !f.writer.May(context.Background(), f.tenantID, f.owner) {
		t.Fatal("the owner is refused before the corrupt document exists; the control is gone")
	}

	// ONE shipped baseline document, stored with a body this release cannot validate.
	// A LATER VERSION with a body this release cannot validate. It has to be a NEW
	// version rather than an edit: policy_versions takes no UPDATE, from anybody, and
	// ListPolicySet reads the HIGHEST version_no -- which is also how this state
	// arrives in production (a document written by a newer release, read by an older).
	name, _ := shippedDoc(t, 0)
	f.appendVersion(t, f.tenantID, f.baselineID(t, f.tenantID, name), 2, []byte(`{}`))

	if f.writer.May(context.Background(), f.tenantID, f.owner) {
		t.Fatal("the owner is STILL allowed with an unreadable stored baseline document; " +
			"checkin's stored set is all-or-nothing, so this means the gate is not reading it")
	}
	if err := f.writer.RecordRefusal(context.Background(), f.tenantID, f.owner,
		"disable", uuid.New().String()); err != nil {
		t.Fatalf("RecordRefusal: %v", err)
	}
	rows := f.auditRows(t, f.tenantID, ActionPolicyEditRefused)
	if len(rows) != 1 {
		t.Fatalf("the trail holds %d refusal row(s), want 1", len(rows))
	}
	// 🔴 THE ROW MUST NOT READ AS A ROLE PROBLEM. stage=rulebook with rules STORED is
	// what tells this apart from "never provisioned" and from "you are not an owner",
	// and it is the distinction the screen's sentence now makes too.
	for _, want := range []string{`"role": "owner"`, `"stage": "rulebook"`} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("an owner's refusal row does not carry %s.\nrow: %s", want, rows[0])
		}
	}
	// 🔴 stored_rules IS 0 HERE AND THAT IS CORRECT — it counts USABLE rules, and the
	// stored set is all-or-nothing. What must NOT happen is the sentence claiming the
	// rulebook was never set up: nine documents are stored and one of them cannot be
	// read, which is a different problem needing a different act. The gate cannot tell
	// the two apart without a second query, so the sentence names both and points at
	// the screen, which does tell them apart rule by rule.
	if strings.Contains(rows[0], "has never been set up, or one of its documents cannot") {
		return
	}
	t.Errorf("the refusal reason does not admit that a stored-but-unreadable rulebook looks "+
		"identical to an absent one from here.\nrow: %s", rows[0])
}

// TestRuleWriterDB_ARefusalIsWrittenToTheTrail -- obligation 4 for the case that
// matters most: the act that did NOT happen.
func TestRuleWriterDB_ARefusalIsWrittenToTheTrail(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	if err := f.writer.RecordRefusal(context.Background(), f.tenantID, f.manager,
		"disable", uuid.New().String()); err != nil {
		t.Fatalf("RecordRefusal: %v", err)
	}
	rows := f.auditRows(t, f.tenantID, ActionPolicyEditRefused)
	if len(rows) != 1 {
		t.Fatalf("the trail holds %d refusal row(s), want 1", len(rows))
	}
	// 🔴 THE ROW SAYS WHICH STAGE REFUSED AND WHAT THE ENGINE MATCHED. The first
	// version wrote a fixed sentence plus `required_role: "owner"` for every refusal,
	// and audit_log takes no UPDATE and no DELETE — so an OWNER of a business with no
	// stored rulebook got a permanent row reading role=owner / required_role=owner,
	// contradicting itself and hiding the real cause.
	for _, want := range []string{
		`"outcome": "refused"`, `"role": "manager"`, `"act": "disable"`,
		`"stage": "guardrail"`, `"matched_sid": "sys:policy-edit-owner-only"`,
	} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("the refusal row does not carry %s.\nrow: %s", want, rows[0])
		}
	}
	if strings.Contains(rows[0], "required_role") {
		t.Errorf("the refusal row still carries required_role, which was the field that "+
			"contradicted itself for an owner.\nrow: %s", rows[0])
	}

	// THE OTHER STAGE, ON A BUSINESS THAT HAS NO STORED RULEBOOK AT ALL. This is the
	// case that produced the self-contradicting row, so it is the one that has to be
	// measured rather than argued about: the actor is an OWNER and the refusal is not
	// about their role.
	virgin := Actor{ID: f.foreignAdmin, Role: "owner"}
	if err := f.writer.RecordRefusal(context.Background(), f.foreignTenant, virgin,
		"disable", uuid.New().String()); err != nil {
		t.Fatalf("RecordRefusal (virgin tenant): %v", err)
	}
	vrows := f.auditRows(t, f.foreignTenant, ActionPolicyEditRefused)
	if len(vrows) != 1 {
		t.Fatalf("the other business's trail holds %d refusal row(s), want 1", len(vrows))
	}
	for _, want := range []string{
		`"role": "owner"`, `"stage": "rulebook"`, `"stored_rules": 0`,
		"no stored rule is usable",
	} {
		if !strings.Contains(vrows[0], want) {
			t.Errorf("an OWNER's refusal row does not carry %s — this is the row that used to "+
				"say the required role was owner while the actor's role WAS owner.\nrow: %s",
				want, vrows[0])
		}
	}

	// AND A REFUSAL IS NOT WRITTEN FOR SOMEBODY THE ENGINE ALLOWS. A row that says
	// "refused" about an act nothing refused is the same defect one direction over.
	if err := f.writer.RecordRefusal(context.Background(), f.tenantID, f.owner,
		"disable", uuid.New().String()); err == nil {
		t.Error("a refusal was recorded for an actor the engine allows")
	}
	if got := f.auditRows(t, f.tenantID, ActionPolicyEditRefused); len(got) != 1 {
		t.Errorf("the trail holds %d refusal row(s) after that attempt, want the original 1",
			len(got))
	}
	// §4.7: the row must not carry anything a session could be reconstructed from.
	for _, forbidden := range []string{"token", "cookie", "session"} {
		if strings.Contains(strings.ToLower(rows[0]), forbidden) {
			t.Errorf("the refusal row mentions %q: %s", forbidden, rows[0])
		}
	}
}

// TestRuleWriterDB_EveryChangeIsInTheTrailWithTheStateItChanged -- obligation 4.
func TestRuleWriterDB_EveryChangeIsInTheTrailWithTheStateItChanged(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	name, _ := shippedDoc(t, 1)
	target := f.baselineID(t, f.tenantID, name)

	if _, err := f.writer.SetEnabled(context.Background(), EnableCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: target, Enabled: false,
	}); err != nil {
		t.Fatalf("SetEnabled(off): %v", err)
	}
	rows := f.auditRows(t, f.tenantID, ActionPolicyDisabled)
	if len(rows) != 1 {
		t.Fatalf("the trail holds %d disable row(s), want 1", len(rows))
	}
	// THE BEFORE STATE IS THE PART A LOCKED READ BUYS. Without GetPolicyForUpdate two
	// presses would both record was=true, and one of them would be describing a change
	// that never happened.
	for _, want := range []string{`"was_enabled": true`, `"now_enabled": false`} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("the trail row does not carry %s.\nrow: %s", want, rows[0])
		}
	}

	// A SECOND PRESS IN THE SAME DIRECTION WRITES NOTHING AT ALL -- neither the row
	// nor a trail entry. An audit row for a change that did not happen is the same
	// defect as a wrong one.
	if _, err := f.writer.SetEnabled(context.Background(), EnableCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: target, Enabled: false,
	}); err != nil {
		t.Fatalf("the second SetEnabled(off) failed: %v", err)
	}
	if rows := f.auditRows(t, f.tenantID, ActionPolicyDisabled); len(rows) != 1 {
		t.Errorf("a no-op press appended a trail row: %d rows now", len(rows))
	}
}

// TestRuleWriterDB_AnEditIsAlwaysANewVersion -- obligations 2 and the §4.3 half.
func TestRuleWriterDB_AnEditIsAlwaysANewVersion(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	src := AuthorableRules()
	if len(src) == 0 {
		t.Fatal("no shipped rule is authorable, so this section can produce no tenant policy")
	}

	first, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, Name: "Nights at the Rusty Bar",
		BasedOn: src[0].Name,
	})
	if err != nil {
		t.Fatalf("Author (create): %v", err)
	}
	if !first.Created || first.VersionNo != 1 {
		t.Fatalf("first Author returned created=%v version=%d, want true and 1",
			first.Created, first.VersionNo)
	}
	second, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: first.PolicyID,
		Name: "Nights at the Rusty Bar", BasedOn: src[0].Name, Venues: []uuid.UUID{uuid.New()},
	})
	if err != nil {
		t.Fatalf("Author (edit): %v", err)
	}
	if second.Created || second.VersionNo != 2 {
		t.Fatalf("the second Author returned created=%v version=%d, want false and 2 -- an "+
			"edit that is not a new version would be rewriting why past records were flagged",
			second.Created, second.VersionNo)
	}
	// BOTH VERSIONS ARE STILL THERE. §4.3 is enforced by the schema, so this is the
	// observable half of it.
	if n := f.versionCount(t, first.PolicyID); n != 2 {
		t.Errorf("the policy has %d version(s) after two saves, want 2", n)
	}

	// 🔴 SAVING THE SAME NAME AGAIN AS A *NEW* RULE IS REFUSED, AND THAT IS LOSS
	// PREVENTION RATHER THAN TIDINESS. The panel's authoring form carried no policy id
	// for a round, so every save took the CREATE branch: two rows with one name, both
	// in the decision set, neither in the other's history — and `policies` grants no
	// DELETE, so neither could ever be removed. The handler's own comment claimed the
	// second save was "reversible by appending a third"; it was not.
	if _, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, Name: "Nights at the Rusty Bar",
		BasedOn: src[0].Name,
	}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("creating a SECOND rule with an existing name returned %v, want ErrNameTaken", err)
	}
	if n := f.policyCountNamed(t, "Nights at the Rusty Bar"); n != 1 {
		t.Errorf("this business now holds %d policies called \"Nights at the Rusty Bar\", want 1. "+
			"A duplicate cannot be removed by anything this product can do.", n)
	}
	// AND THE SUPPORTED MOVE STILL WORKS: the same name with the policy's ID appends a
	// version, which is the whole point of refusing the other one.
	if _, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: first.PolicyID,
		Name: "Nights at the Rusty Bar", BasedOn: src[0].Name,
	}); err != nil {
		t.Errorf("appending a version with the same name failed: %v — the name check must "+
			"apply to CREATE only, or every legitimate edit is refused", err)
	}

	// 🔴 THE NAME THE CONFIRMATION SHOWED IS THE NAME THAT LANDS. The supersede branch
	// used to append the version and leave `policies.name` alone, so an owner typed a
	// name, read it back on the confirmation screen, pressed the button — and the
	// screen, the picker and the version page all went on showing the old one.
	renamed, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: first.PolicyID,
		Name: "Nights at the Rusty Bar (evenings)", BasedOn: src[0].Name,
	})
	if err != nil {
		t.Fatalf("Author (rename): %v", err)
	}
	if got := f.policyName(t, first.PolicyID); got != "Nights at the Rusty Bar (evenings)" {
		t.Errorf("after a save under a new name the row is still called %q", got)
	}
	// A RENAME IS STILL A NEW VERSION: the numbers only go up, and the earlier
	// documents stay readable for the records they decided.
	if renamed.VersionNo <= second.VersionNo {
		t.Errorf("the rename produced version %d, which is not after %d",
			renamed.VersionNo, second.VersionNo)
	}
	// AND A RENAME ONTO A NAME THIS BUSINESS ALREADY USES IS THE SAME REFUSAL A CREATE
	// GETS. The unique index protects both; without that mapping the loser of a rename
	// would be told the panel is broken.
	other, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, Name: "A second rule", BasedOn: src[0].Name,
	})
	if err != nil {
		t.Fatalf("Author (second rule): %v", err)
	}
	if _, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: other.PolicyID,
		Name: "Nights at the Rusty Bar (evenings)", BasedOn: src[0].Name,
	}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("renaming onto an existing name returned %v, want ErrNameTaken", err)
	}

	// 🔴 AND A RULE TAPPA MAINTAINS CANNOT BE RENAMED, WHICH IS A BELT NOTHING PINNED.
	// RenameTenantPolicy carries `AND layer = 'tenant'` in the statement; deleting that
	// clause left every shipped test green (measured), while a renamed BASELINE row
	// loses the name-join checkin.assembleBaseline pairs it by — and because that
	// assembly is all-or-nothing, the business falls to the guardrails alone. So the
	// cost of the missing clause is not a cosmetic rename, it is the whole rulebook.
	//
	// Author refuses a managed policy before it gets that far (ErrNotYours), so this
	// drives the STATEMENT: a rename aimed at a baseline row must affect no row.
	shippedName, _ := shippedDoc(t, 4)
	managedID := f.baselineID(t, f.tenantID, shippedName)
	err = f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RenameTenantPolicy(ctx, store.RenameTenantPolicyParams{
			TenantID: f.tenantID, ID: managedID, Name: "hijacked",
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("renaming a MANAGED policy returned %v, want pgx.ErrNoRows (no row matched). "+
			"Without the layer clause the row would be renamed and the tap path would stop "+
			"pairing it with the document this binary ships.", err)
	}
	if got := f.policyName(t, managedID); got != shippedName {
		t.Errorf("the managed policy is now called %q, want %q", got, shippedName)
	}

	// A RULE TAPPA MAINTAINS IS NOT REWRITTEN HERE.
	name, _ := shippedDoc(t, 0)
	managed := f.baselineID(t, f.tenantID, name)
	if _, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: managed,
		Name: "my version", BasedOn: src[0].Name,
	}); !errors.Is(err, ErrNotYours) {
		t.Errorf("appending a version to a MANAGED policy returned %v, want ErrNotYours", err)
	}
}

// TestRuleWriterDB_TheAuthoredDocumentCannotNamePolicyEdit -- the reason the v1 editor
// generates documents instead of accepting them.
func TestRuleWriterDB_TheAuthoredDocumentCannotNamePolicyEdit(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	// The authorization document is a SHIPPED document, so a form that let a customer
	// name any shipped document by name would reach it. It must not be offered.
	for _, doc := range AuthorableRules() {
		if mentionsPolicyEdit(doc) {
			t.Fatalf("%q is offered as a rule a customer may copy, and it grants policy:edit. "+
				"A customer could then generate a statement about their own authority.", doc.Name)
		}
	}
	// 🔴 THE NAME IS ONE THIS BINARY REALLY SHIPS, ASKED RATHER THAN TYPED. A literal
	// would give the same ErrBadResource as any unknown string, so the test would pass
	// even if the authorization document were renamed and quietly became authorable.
	var authzName string
	for _, doc := range policy.Baseline() {
		if mentionsPolicyEdit(doc) {
			authzName = doc.Name
		}
	}
	if authzName == "" {
		t.Fatal("no shipped document grants policy:edit; this case has no subject")
	}
	if _, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, Name: "sneaky", BasedOn: authzName,
	}); !errors.Is(err, ErrBadResource) {
		t.Errorf("basing a rule on %q (a document this binary SHIPS) returned %v, want a "+
			"refusal", authzName, err)
	}
	// POSITIVE CONTROL: a name that IS authorable gets through, so the refusal above is
	// about which document it is rather than about the name being unknown.
	if _, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, Name: "an allowed copy",
		BasedOn: AuthorableRules()[0].Name,
	}); err != nil {
		t.Errorf("basing a rule on an AUTHORABLE shipped document failed: %v", err)
	}
}

// TestRuleWriterDB_TwoConcurrentSavesProduceOneVersionAndOneRefusal.
//
// 🔴 THE RACE MIGRATION 00007 LEAVES OPEN, MEASURED RATHER THAN REASONED ABOUT.
// version_no is "monotonic per policy, assigned by the app" against
// UNIQUE (tenant_id, policy_id, version_no), so two saves that read the same MAX both
// try N+1 and one must lose. The point of the test is not that a save fails -- it is
// that the loser is a NAMED refusal rather than a 500, and that the winner's document
// is not quietly renumbered on top of anybody.
func TestRuleWriterDB_TwoConcurrentSavesProduceOneVersionAndOneRefusal(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	src := AuthorableRules()
	base, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, Name: "raced rule", BasedOn: src[0].Name,
	})
	if err != nil {
		t.Fatalf("Author (create): %v", err)
	}

	const racers = 8
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		ok    int
		raced int
		other []error
	)
	start.Add(1)
	for i := 0; i < racers; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, err := f.writer.Author(context.Background(), AuthorCommand{
				TenantID: f.tenantID, Actor: f.owner, PolicyID: base.PolicyID,
				Name: "raced rule", BasedOn: src[0].Name,
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrVersionRace):
				raced++
			default:
				other = append(other, err)
			}
		}()
	}
	start.Done()
	done.Wait()

	if len(other) > 0 {
		t.Errorf("%d racer(s) failed with something other than ErrVersionRace: %v.\n"+
			"A 23505 on policy_versions_no_key has to become a sentence an owner can act "+
			"on (\"somebody else saved first\"), never a 500 that says the panel is broken.",
			len(other), other)
	}
	if ok == 0 {
		t.Fatal("no racer succeeded; the contention is total and nothing was measured")
	}
	// WHAT IS ASSERTED IS THE INVARIANT, NOT THE SPLIT. GetPolicyForUpdate serialises
	// the racers on the parent row, so most runs see every save succeed with a distinct
	// version number -- which is the BETTER outcome and must not be a failure. What
	// must hold either way: the version numbers are 1..N with no gap and no repeat,
	// which is what the unique index and the lock are for together.
	t.Logf("racers=%d succeeded=%d refused-as-race=%d", racers, ok, raced)
	if got, want := f.versionCount(t, base.PolicyID), 1+ok; got != want {
		t.Errorf("the policy holds %d version(s) after %d successful save(s) on top of the "+
			"first, want %d", got, ok, want)
	}
	if gaps := f.versionGaps(t, base.PolicyID); gaps != "" {
		t.Errorf("the version sequence is not 1..N: %s", gaps)
	}
}

// TestRuleWriterDB_ConcurrentSavesOfOneNameProduceOneRow.
//
// 🔴 THE PROBE THAT SHOWED THE APPLICATION GUARD WAS A LOOK RATHER THAN A LOCK, kept
// as the regression test for the index that replaced it. PolicyNameTaken is a
// read-then-write; measured before migration 00015, 8 racers gave 1 insert and 7
// refusals (the guard appeared to hold) and 16 racers gave 16 INSERTS (it did not).
// `policies` GRANTs no DELETE, so every one of those rows was permanent.
//
// 🔴 THE RACER COUNT IS A MEASUREMENT RATHER THAN A GUESS, AND IT IS NAMED ONCE
// (raceRacers) SO THE TEST'S TITLE CANNOT DRIFT FROM IT. An eight-racer probe PASSED against the broken code. So did sixteen, ON THIS
// MACHINE -- while an auditor's sixteen-racer run on different hardware produced 16
// duplicate rows. The mechanism was measured rather than argued about: the duplicate
// count equals the number of TRULY CONCURRENT connections, and pgx's default pool is
// max(4, NumCPU). With the index dropped, 64 racers on a 16-core machine produce
// exactly 16 rows, reproducibly, three runs out of three. A probe that does not reach
// the concurrency level proves only that the failure does not happen at that level.
//
// WHAT IS ASSERTED IS THE INVARIANT THE INDEX GIVES -- exactly one row survives --
// rather than a particular split of winners and losers, because that split is
// hardware-dependent and the invariant is not.
//
// ⚠️ THE CLEAN-UP AFTER THAT MUTATION IS ITSELF THE ARGUMENT FOR THE MIGRATION: the
// 16 duplicate rows could not be removed. `policies` REVOKEs DELETE from tappa_app,
// and deleting them as the owner is refused by the composite foreign key from
// policy_versions, whose rows cannot be deleted by anybody (00007's trigger binds the
// owner too). The development database had to be rebuilt from migrations.
func TestRuleWriterDB_ConcurrentSavesOfOneNameProduceOneRow(t *testing.T) {
	f := newRaceWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	src := AuthorableRules()
	name := "raced name " + uuid.NewString()

	const racers = raceRacers
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		ok    int
		taken int
		other []error
	)
	start.Add(1)
	for i := 0; i < racers; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, err := f.writer.Author(context.Background(), AuthorCommand{
				TenantID: f.tenantID, Actor: f.owner, Name: name, BasedOn: src[0].Name,
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrNameTaken):
				taken++
			default:
				other = append(other, err)
			}
		}()
	}
	start.Done()
	done.Wait()

	t.Logf("racers=%d created=%d refused-as-name-taken=%d other=%v", racers, ok, taken, other)
	if n := f.policyCountNamed(t, name); n != 1 {
		t.Errorf("%d concurrent saves of one name left %d policy rows, want 1. A duplicate "+
			"cannot be removed by anything this product can do (00007 revokes DELETE), so "+
			"this is the one write in the panel that has to be settled by the database.",
			racers, n)
	}
	if ok != 1 {
		t.Errorf("%d racers reported success, want 1", ok)
	}
	// 🔴 EVERY LOSER GETS THE SAME REFUSAL, whether it lost to the early check or to
	// the index. A 23505 that escaped as a wrapped driver error would tell the loser
	// the panel is broken while the winner is told nothing happened.
	if len(other) > 0 {
		t.Errorf("%d racer(s) failed with something other than ErrNameTaken: %v", len(other), other)
	}
	if taken != racers-1 {
		t.Errorf("%d racers were refused as name-taken, want %d", taken, racers-1)
	}
}

// TestRuleWriterDB_ConcurrentVersionsAreOneToNWithNoGap.
//
// ⚠️ THE 23505 ARM IS STILL NOT EXERCISED, AND THAT IS WRITTEN DOWN RATHER THAN
// PAPERED OVER. Eight racers printed `succeeded=8 refused-as-race=0`; sixteen and then
// sixty-four (with the pool widened to match) print the same shape --
// GetPolicyForUpdate takes a row lock on the parent, so the racers SERIALISE and each
// one reads a MAX the previous one has already committed. So ErrVersionRace's mapping
// is NOT proven by this probe. What IS proven is the invariant the lock and the unique
// key give together: version numbers 1..N with no gap and no repeat, and no racer
// failing with anything else.
func TestRuleWriterDB_ConcurrentVersionsAreOneToNWithNoGap(t *testing.T) {
	f := newRaceWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	src := AuthorableRules()
	base, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner,
		Name: "raced versions " + uuid.NewString(), BasedOn: src[0].Name,
	})
	if err != nil {
		t.Fatalf("Author (create): %v", err)
	}

	const racers = raceRacers
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		ok    int
		raced int
		other []error
	)
	start.Add(1)
	for i := 0; i < racers; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, err := f.writer.Author(context.Background(), AuthorCommand{
				TenantID: f.tenantID, Actor: f.owner, PolicyID: base.PolicyID,
				Name: "raced versions", BasedOn: src[0].Name,
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrVersionRace):
				raced++
			default:
				other = append(other, err)
			}
		}()
	}
	start.Done()
	done.Wait()

	t.Logf("racers=%d succeeded=%d refused-as-race=%d other=%v", racers, ok, raced, other)
	if len(other) > 0 {
		t.Errorf("%d racer(s) failed with something other than ErrVersionRace: %v.\n"+
			"A 23505 on policy_versions_no_key has to become a sentence an owner can act "+
			"on, never a 500 that says the panel is broken.", len(other), other)
	}
	if ok == 0 {
		t.Fatal("no racer succeeded; contention is total and nothing was measured")
	}
	if got, want := f.versionCount(t, base.PolicyID), 1+ok; got != want {
		t.Errorf("the policy holds %d version(s) after %d successful save(s) on top of the "+
			"first, want %d", got, ok, want)
	}
	if gaps := f.versionGaps(t, base.PolicyID); gaps != "" {
		t.Errorf("the version sequence is not 1..N: %s", gaps)
	}
}

// TestRuleWriterDB_ALockoutLeavesTwoTrailRowsAndTheRuleAsItWas.
//
// 🔴 THE SCREEN HAS DESCRIBED THIS PATH WRONGLY TWICE, IN OPPOSITE DIRECTIONS, so it
// is pinned rather than argued about. Switching off the rule that grants policy:edit
// is not refused up front — it CANNOT be, because deciding "would this deny me?" in
// advance means evaluating a rulebook that does not exist yet. So the change happens,
// the gate is re-asked, the answer has flipped, and the writer puts it back. What a
// customer sees afterwards: the rule as it was, and TWO trail rows naming them.
//
// The page said first that every refusal is recorded (false for the cheap refusals)
// and then that every refusal other than the engine's "changes nothing and names
// nobody" (false for exactly this one). This test is what makes the third attempt
// stay true.
func TestRuleWriterDB_ALockoutLeavesTwoTrailRowsAndTheRuleAsItWas(t *testing.T) {
	f := newWriterFixture(t)
	authz := f.seedAuthzDocument(t, f.tenantID)

	before := len(f.auditRows(t, f.tenantID, ActionPolicyDisabled)) +
		len(f.auditRows(t, f.tenantID, ActionPolicyEnabled))
	if before != 0 {
		t.Fatalf("the fixture already holds %d switch row(s); the count below would be "+
			"measuring somebody else's writes", before)
	}

	_, err := f.writer.SetEnabled(context.Background(), EnableCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: authz, Enabled: false,
	})
	if !errors.Is(err, ErrWouldLockOut) {
		t.Fatalf("switching off the authorization rule returned %v, want ErrWouldLockOut", err)
	}
	// THE RULE IS AS IT WAS.
	if !f.enabledOf(t, authz) {
		t.Error("the rule is still switched off after the lockout check; the undo did not run")
	}
	// AND THE TRAIL CARRIES BOTH HALVES, each naming the actor. A customer reading
	// "nothing was recorded" on the screen would be looking at two rows.
	off := f.auditRows(t, f.tenantID, ActionPolicyDisabled)
	on := f.auditRows(t, f.tenantID, ActionPolicyEnabled)
	if len(off) != 1 || len(on) != 1 {
		t.Errorf("the trail holds %d disable and %d enable row(s), want 1 and 1", len(off), len(on))
	}
	for _, row := range append(append([]string{}, off...), on...) {
		if !strings.Contains(row, "panel access by role") {
			t.Errorf("a lockout trail row does not name the rule: %s", row)
		}
	}
	// AND THE OWNER CAN STILL ACT, which is the whole point of putting it back.
	if !f.writer.May(context.Background(), f.tenantID, f.owner) {
		t.Error("the owner is locked out after the change was supposedly undone")
	}
}

// TestRuleWriterDB_BindingsAreWrittenAndRemovedWithATrail -- obligation 3.
func TestRuleWriterDB_BindingsAreWrittenAndRemovedWithATrail(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	name, _ := shippedDoc(t, 2)
	target := f.baselineID(t, f.tenantID, name)
	venue := "location/" + uuid.NewString()

	cmd := BindCommand{TenantID: f.tenantID, Actor: f.owner, PolicyID: target, Resource: venue}
	if err := f.writer.Bind(context.Background(), cmd); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := f.writer.Bind(context.Background(), cmd); !errors.Is(err, ErrAlreadyBound) {
		t.Errorf("binding twice returned %v, want ErrAlreadyBound -- a press for something "+
			"that was already true must not report a change", err)
	}
	if rows := f.auditRows(t, f.tenantID, ActionPolicyBound); len(rows) != 1 {
		t.Errorf("the trail holds %d bind row(s) after one real bind and one refusal, want 1",
			len(rows))
	}
	if err := f.writer.Unbind(context.Background(), cmd); err != nil {
		t.Fatalf("Unbind: %v", err)
	}
	if err := f.writer.Unbind(context.Background(), cmd); !errors.Is(err, ErrNotBound) {
		t.Errorf("unbinding twice returned %v, want ErrNotBound", err)
	}
	if rows := f.auditRows(t, f.tenantID, ActionPolicyUnbound); len(rows) != 1 {
		t.Errorf("the trail holds %d unbind row(s), want 1", len(rows))
	}
	// A MANAGER MAY NOT BIND EITHER. The gate is on every method, not on the toggle.
	if err := f.writer.Bind(context.Background(), BindCommand{
		TenantID: f.tenantID, Actor: f.manager, PolicyID: target, Resource: venue,
	}); !errors.Is(err, ErrNotPermitted) {
		t.Errorf("a manager's Bind returned %v, want ErrNotPermitted", err)
	}
}

// TestRuleWriterDB_AnotherBusinessesPolicyIsNotReachable -- §4.5, with its positive
// control.
func TestRuleWriterDB_AnotherBusinessesPolicyIsNotReachable(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	name, raw := shippedDoc(t, 3)
	// The row belongs to the OTHER business, which has NOT been seeded -- so this one
	// really is stored here rather than read back.
	foreign := f.storePolicy(t, f.foreignTenant, name, "baseline", true, raw, nil)

	if _, err := f.writer.SetEnabled(context.Background(), EnableCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: foreign, Enabled: false,
	}); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatalf("switching another business's policy returned %v, want ErrUnknownPolicy", err)
	}
	if err := f.writer.Bind(context.Background(), BindCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: foreign, Resource: "*",
	}); !errors.Is(err, ErrUnknownPolicy) {
		t.Errorf("binding another business's policy returned %v, want ErrUnknownPolicy", err)
	}
	// POSITIVE CONTROL: the same row IS reachable from its own tenant, so the refusals
	// above cannot be mistaken for "nothing works".
	if !f.enabledOfIn(t, f.foreignTenant, foreign) {
		t.Fatal("the foreign policy is not readable in its OWN tenant either; the probe " +
			"proves nothing about isolation")
	}
	// ⚠️ AND THE ATTEMPT WROTE NO TRAIL ROW INTO THE OTHER BUSINESS — which is the
	// WEAKER half of what this used to claim ("wrote nothing into either business").
	// A row in the OTHER tenant is structurally impossible: audit.RecordTx runs inside
	// db.WithTenant and RLS's WITH CHECK refuses a row for any other tenant, so this
	// assertion cannot fail for the reason it appears to test. It is kept as a cheap
	// tripwire on that structure, not as evidence about this code path. The half that
	// IS about this code path is the one above: the write returned ErrUnknownPolicy.
	if rows := f.auditRows(t, f.foreignTenant, ActionPolicyDisabled); len(rows) != 0 {
		t.Errorf("the cross-tenant attempt left %d trail row(s) in the other business", len(rows))
	}
	// AND NOTHING WAS WRITTEN INTO THE CALLER'S OWN BUSINESS EITHER, which is the half
	// that can actually fail: a handler that fell through to "disable something" would
	// leave a row right here.
	if rows := f.auditRows(t, f.tenantID, ActionPolicyDisabled); len(rows) != 0 {
		t.Errorf("a cross-tenant attempt left %d trail row(s) in the CALLER's business", len(rows))
	}
}

// TestRuleWriterDB_TheInsertsWriteTheCallersTenant -- the §4.5 belt half the DERIVED
// scan could not see until this task taught it to (backlog T20), pinned behaviourally
// as well.
//
// A row written with the WRONG tenant is refused by RLS's WITH CHECK; what this
// measures is the other direction -- that the row lands in the caller's tenant and is
// visible there and nowhere else.
func TestRuleWriterDB_TheInsertsWriteTheCallersTenant(t *testing.T) {
	f := newWriterFixture(t)
	f.seedAuthzDocument(t, f.tenantID)
	src := AuthorableRules()
	made, err := f.writer.Author(context.Background(), AuthorCommand{
		TenantID: f.tenantID, Actor: f.owner, Name: "scoped to me", BasedOn: src[0].Name,
	})
	if err != nil {
		t.Fatalf("Author: %v", err)
	}
	if err := f.writer.Bind(context.Background(), BindCommand{
		TenantID: f.tenantID, Actor: f.owner, PolicyID: made.PolicyID,
		Resource: "location/" + uuid.NewString(),
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// 🔴 ALL THREE TABLES, NOT ONE. The first version of this test checked `policies`
	// alone -- which is the table whose INSERT is easiest to get right, because it is
	// the one with no parent. policy_versions and policy_attachments each carry a
	// COMPOSITE same-tenant foreign key, so a wrong tenant there fails differently, and
	// "different" is not "checked".
	for _, c := range []struct{ table, column, id string }{
		{"policies", "id", made.PolicyID.String()},
		{"policy_versions", "policy_id", made.PolicyID.String()},
		{"policy_attachments", "policy_id", made.PolicyID.String()},
	} {
		if got := f.tenantOfRow(t, f.tenantID, c.table, c.column, c.id); got != f.tenantID {
			t.Errorf("%s carries tenant %s, want %s", c.table, got, f.tenantID)
		}
		// AND THE SAME ROW IS INVISIBLE FROM THE OTHER BUSINESS'S CONNECTION, which is
		// the positive control for the line above: a query that returned nothing for
		// both would agree with any tenant at all.
		if f.tenantOfRow(t, f.foreignTenant, c.table, c.column, c.id) != uuid.Nil {
			t.Errorf("a %s row written for one business is visible from the other's connection",
				c.table)
		}
	}
}

// --- fixture helpers -----------------------------------------------------------

func (f *writerFixture) enabledOf(t *testing.T, id uuid.UUID) bool {
	return f.enabledOfIn(t, f.tenantID, id)
}

func (f *writerFixture) enabledOfIn(t *testing.T, tenantID, id uuid.UUID) bool {
	t.Helper()
	var enabled bool
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT enabled FROM policies WHERE tenant_id = $1 AND id = $2`,
			tenantID, id).Scan(&enabled)
	})
	if err != nil {
		t.Fatalf("reading enabled: %v", err)
	}
	return enabled
}

func (f *writerFixture) policyName(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var name string
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT name FROM policies WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, id).Scan(&name)
	})
	if err != nil {
		t.Fatalf("reading a policy's name: %v", err)
	}
	return name
}

func (f *writerFixture) policyCountNamed(t *testing.T, name string) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM policies WHERE tenant_id = $1 AND name = $2`,
			f.tenantID, name).Scan(&n)
	})
	if err != nil {
		t.Fatalf("counting policies by name: %v", err)
	}
	return n
}

func (f *writerFixture) versionCount(t *testing.T, policyID uuid.UUID) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM policy_versions WHERE tenant_id = $1 AND policy_id = $2`,
			f.tenantID, policyID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("counting versions: %v", err)
	}
	return n
}

// versionGaps reports "" when the version numbers are exactly 1..N.
func (f *writerFixture) versionGaps(t *testing.T, policyID uuid.UUID) string {
	t.Helper()
	var nos []int32
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT version_no FROM policy_versions WHERE tenant_id = $1 AND policy_id = $2
			 ORDER BY version_no`, f.tenantID, policyID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var n int32
			if e := rows.Scan(&n); e != nil {
				return e
			}
			nos = append(nos, n)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading version numbers: %v", err)
	}
	for i, n := range nos {
		if int(n) != i+1 {
			return "version numbers are not contiguous from 1"
		}
	}
	return ""
}

// tenantOfRow reads one row's tenant_id through a given tenant's connection, or
// uuid.Nil when that connection cannot see it.
//
// THE TABLE AND COLUMN ARE INTERPOLATED AND THAT IS SAFE HERE FOR A REASON WORTH
// STATING: both come from a literal slice three lines up, never from data. A test
// helper that took them from a row would be the shape that teaches somebody to build
// SQL from input.
func (f *writerFixture) tenantOfRow(t *testing.T, readAs uuid.UUID, table, column, id string) uuid.UUID {
	t.Helper()
	var out uuid.UUID
	err := f.data.WithTenant(context.Background(), readAs, func(ctx context.Context, tx pgx.Tx) error {
		e := tx.QueryRow(ctx,
			`SELECT tenant_id FROM `+table+` WHERE `+column+` = $1 LIMIT 1`, id).Scan(&out)
		if errors.Is(e, pgx.ErrNoRows) {
			out = uuid.Nil
			return nil
		}
		return e
	})
	if err != nil {
		t.Fatalf("reading %s's tenant: %v", table, err)
	}
	return out
}
