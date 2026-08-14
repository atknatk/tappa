package tenant

// rulebook_db_test.go -- M6-09 phase A's read side against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and each property below is one a mock would
// agree with unconditionally:
//
//   - THE READ WRITES NOTHING. The claim is about ROW COUNTS in three tables, and
//     the only thing that can count them is a database. It is the property this
//     whole file exists for: the OTHER assembler of a policy set
//     (internal/domain/checkin) materialises the managed baseline when it finds it
//     missing, so the failure being guarded against is a panel that quietly
//     provisions a policy, a version and an attachment per shipped document for any tenant
//     somebody looked at -- into a table that is append-only, so the rows could
//     never be taken back.
//   - §4.5. One tenant's screen must not carry another's rule, and the reason it
//     does not has to be the policy plus the predicate rather than luck. The probe
//     comes with its POSITIVE CONTROL: the same row IS visible to its own tenant,
//     so an empty result cannot be mistaken for isolation.
//   - THE AUTHOR OF A VERSION. policy_versions.created_by is an FK-LESS uuid, so
//     "resolves to a name", "resolves to nobody" and "was written by the system"
//     are three states only a real join under RLS can produce.
//
// FIXTURES ARE NOT CLEANED UP, for the reason staff_db_test.go states: policies
// cannot be DELETEd by tappa_app (00007 revokes it) and policy_versions cannot be
// touched at all. Fresh random ids keep runs from colliding, and `make db-reset`
// clears the development database.

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/test/fixtures"
)

// rulebookFixture is one tenant, plus a SECOND to attack it from.
type rulebookFixture struct {
	data  *db.DB
	rules *Rulebook

	tenantID uuid.UUID
	adminID  uuid.UUID

	foreignTenant uuid.UUID
	foreignAdmin  uuid.UUID
}

func newRulebookFixture(t *testing.T) *rulebookFixture {
	return newRulebookFixtureWithPool(t, 0)
}

// newRulebookFixtureWithPool is the same fixture with the connection pool widened, for
// the concurrency probes only. poolMax == 0 leaves the DSN alone (pgx's default of
// max(4, NumCPU)).
func newRulebookFixtureWithPool(t *testing.T, poolMax int) *rulebookFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping rulebook tests (real Postgres required)")
	}
	// 🔴 THE WIDENED POOL IS FOR THE RACE PROBES ONLY, AND SCOPING IT COST A GREEN
	// SUITE ONCE. The first version applied it to EVERY fixture in this package, which
	// meant every rulebook test opened a pool of raceRacers+4; run under `make test`,
	// where packages go in parallel against one Postgres with max_connections=100, the
	// SUN counter races failed with `sorry, too many clients already` (SQLSTATE 53300).
	// The failure was in another package and the cause was here. internal/sun does it
	// the scoped way -- one raceDB helper for the race tests -- which is the pattern
	// copied here.
	if poolMax > 0 && !strings.Contains(dsn, "pool_max_conns") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "pool_max_conns=" + strconv.Itoa(poolMax)
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	// THE WINDOWS ARE THE NON-DEFAULT ONES, so an assertion that the screen shows
	// the configured value can tell "the parameter arrived" from "the default
	// happened to match" -- the M5-05 F3 lesson.
	rules, err := NewRulebook(data, testParams(), testGPSRadiusM, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRulebook: %v", err)
	}
	f := &rulebookFixture{
		data: data, rules: rules,
		tenantID: uuid.New(), adminID: uuid.New(),
		foreignTenant: uuid.New(), foreignAdmin: uuid.New(),
	}
	f.seedTenant(t, f.tenantID, f.adminID)
	f.seedTenant(t, f.foreignTenant, f.foreignAdmin)
	return f
}

func (f *rulebookFixture) seedTenant(t *testing.T, tenantID, adminID uuid.UUID) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
			 VALUES ($1, $2, 'Rita Camilleri', $3, $4, 'owner')`,
			adminID, tenantID, "rita-"+adminID.String()+"@example.test", fixtures.UnusablePasswordHash)
		return e
	})
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

// storePolicy writes one policy and (unless raw is nil) one version for it.
func (f *rulebookFixture) storePolicy(t *testing.T, tenantID uuid.UUID, name, layer string, enabled bool, raw []byte, author *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO policies (id, tenant_id, name, layer, enabled) VALUES ($1, $2, $3, $4, $5)`,
			id, tenantID, name, layer, enabled); e != nil {
			return e
		}
		if raw == nil {
			return nil
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO policy_versions (id, tenant_id, policy_id, version_no, document, created_by)
			 VALUES ($1, $2, $3, 1, $4, $5)`,
			uuid.New(), tenantID, id, raw, author); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO policy_attachments (tenant_id, policy_id, resource) VALUES ($1, $2, '*')`,
			tenantID, id)
		return e
	})
	if err != nil {
		t.Fatalf("store policy %q: %v", name, err)
	}
	return id
}

// counts reads the three policy tables for one tenant.
func (f *rulebookFixture) counts(t *testing.T, tenantID uuid.UUID) [3]int {
	t.Helper()
	var out [3]int
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT (SELECT count(*) FROM policies            WHERE tenant_id = $1),
			        (SELECT count(*) FROM policy_versions     WHERE tenant_id = $1),
			        (SELECT count(*) FROM policy_attachments  WHERE tenant_id = $1)`,
			tenantID).Scan(&out[0], &out[1], &out[2])
	})
	if err != nil {
		t.Fatalf("counting policy rows: %v", err)
	}
	return out
}

func shippedDoc(t *testing.T, i int) (name string, raw []byte) {
	t.Helper()
	docs := policy.Baseline()
	if i >= len(docs) {
		t.Fatalf("the binary ships %d baseline document(s); this test wants index %d", len(docs), i)
	}
	b, err := json.Marshal(docs[i].Document)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return docs[i].Name, b
}

// TestRulebookDB_RendersAVirginTenantWithoutWritingARow.
//
// 🔴 THE MEASUREMENT THIS FILE EXISTS FOR. A tenant with NO policy rows is not a
// hypothetical: measured 2026-08-11 against the development database, the seeded
// Kebab Manufacturing tenant has zero. Reading its policy screen must leave it with
// zero -- and the screen must still be able to say what state every shipped rule is
// in, because "not set up yet" is the answer a customer needs there.
//
// THE PROBE COMES WITH ITS POSITIVE CONTROL. A counter that cannot see a change
// would report "no writes" for a reader that wrote a hundred rows, so the same
// counting method is shown moving when a row IS inserted.
func TestRulebookDB_RendersAVirginTenantWithoutWritingARow(t *testing.T) {
	f := newRulebookFixture(t)
	before := f.counts(t, f.tenantID)
	if before != [3]int{0, 0, 0} {
		t.Fatalf("a fresh tenant already holds %v policy rows; the fixture is wrong", before)
	}

	screen, err := f.rules.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	if after := f.counts(t, f.tenantID); after != before {
		t.Errorf("reading the policies screen changed the stored rows: %v -> %v. A page view "+
			"must not provision anything -- policy_versions is append-only, so a row written "+
			"here could never be taken back.", before, after)
	}

	// The screen still answers, and the answer is the shipped list with every rule
	// marked as not set up.
	if len(screen.Baseline) != len(policy.Baseline()) {
		t.Fatalf("the screen lists %d baseline rule(s), the binary ships %d",
			len(screen.Baseline), len(policy.Baseline()))
	}
	for _, r := range screen.Baseline {
		if r.State != StateNotProvisioned {
			t.Errorf("%q on a virgin tenant is %q, want %q", r.Name, r.State, StateNotProvisioned)
		}
	}
	// AND THE FAIL-CLOSED CONSEQUENCE IS VISIBLE, which is the thing a customer in
	// this state must be able to see: with nothing stored, nothing is granted.
	for _, a := range screen.Authorities {
		if a.FailClosed && len(a.Grants) > 0 {
			t.Errorf("%s is granted by %d statement(s) on a tenant with no stored policy at "+
				"all; the screen must read the STORED documents, not the shipped baseline",
				a.Action, len(a.Grants))
		}
	}

	// POSITIVE CONTROL for the counter.
	name, raw := shippedDoc(t, 0)
	f.storePolicy(t, f.tenantID, name, "baseline", true, raw, nil)
	if got := f.counts(t, f.tenantID); got == before {
		t.Fatal("the row counter did not move after an INSERT, so the assertion above " +
			"could not have failed for a reader that wrote")
	}
}

// TestRulebookDB_TellsTheProvisioningStatesApartFromRealRows. Four of the five
// states are produced here by actual rows; the fifth (not provisioned) is every
// other shipped document.
func TestRulebookDB_TellsTheProvisioningStatesApartFromRealRows(t *testing.T) {
	f := newRulebookFixture(t)
	activeName, activeRaw := shippedDoc(t, 0)
	offName, offRaw := shippedDoc(t, 1)
	halfName, _ := shippedDoc(t, 2)
	brokenName, _ := shippedDoc(t, 3)

	f.storePolicy(t, f.tenantID, activeName, "baseline", true, activeRaw, nil)
	f.storePolicy(t, f.tenantID, offName, "baseline", false, offRaw, nil)
	// A CONTAINER WITH NO VERSION -- the state ListPolicySet cannot report, and the
	// reason the history query outer-joins where it inner-joins.
	f.storePolicy(t, f.tenantID, halfName, "baseline", true, nil, nil)
	// A DOCUMENT THE ENGINE CANNOT PARSE. `{}` is legal jsonb and 18 355 such rows
	// exist in the development database, so this is a real shape rather than an
	// invented one.
	f.storePolicy(t, f.tenantID, brokenName, "baseline", true, []byte(`{}`), nil)

	screen, err := f.rules.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	states := map[string]RuleState{}
	for _, r := range screen.Baseline {
		states[r.Name] = r.State
	}
	for _, tc := range []struct {
		name string
		want RuleState
	}{
		{activeName, StateActive},
		{offName, StateOff},
		{halfName, StateNoDocument},
		{brokenName, StateUnreadable},
	} {
		if got := states[tc.name]; got != tc.want {
			t.Errorf("%q: state %q, want %q", tc.name, got, tc.want)
		}
	}
	unprovisioned := 0
	for _, r := range screen.Baseline {
		if r.State == StateNotProvisioned {
			unprovisioned++
		}
	}
	if want := len(policy.Baseline()) - 4; unprovisioned != want {
		t.Errorf("%d rule(s) read as not provisioned, want %d", unprovisioned, want)
	}
}

// TestRulebookDB_ShowsNoOtherTenantsRule is §4.5, with its positive control.
//
// 🔴 AN EMPTY RESULT PROVES NOTHING ON ITS OWN. The same row is read back through
// its OWN tenant in the same test, so "the foreign rule is absent" cannot be
// satisfied by a reader that returns nothing at all.
func TestRulebookDB_ShowsNoOtherTenantsRule(t *testing.T) {
	f := newRulebookFixture(t)
	secret := "Rusty Bar night-shift exception " + uuid.NewString()
	doc := policy.Document{
		Version: policy.BaselineVersion, Name: secret,
		Statements: []policy.Statement{{
			Sid: "own:night", Effect: policy.EffectReview,
			Action: []policy.Action{policy.ActionTapRecord}, Resource: []string{"*"},
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f.storePolicy(t, f.foreignTenant, secret, "tenant", true, raw, &f.foreignAdmin)

	mine, err := f.rules.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen (own tenant): %v", err)
	}
	for _, r := range mine.Tenant {
		if r.Name == secret {
			t.Fatalf("tenant %s can see %s's policy %q", f.tenantID, f.foreignTenant, secret)
		}
	}
	if len(mine.Tenant) != 0 {
		t.Errorf("this tenant has written no policies of its own, and the screen lists %d",
			len(mine.Tenant))
	}

	// POSITIVE CONTROL: the row exists and IS visible to its owner.
	theirs, err := f.rules.Screen(context.Background(), f.foreignTenant)
	if err != nil {
		t.Fatalf("Screen (foreign tenant): %v", err)
	}
	found := false
	for _, r := range theirs.Tenant {
		if r.Name == secret {
			found = true
			if r.State != StateActive {
				t.Errorf("the owner sees their own policy as %q, want %q", r.State, StateActive)
			}
		}
	}
	if !found {
		t.Fatal("the foreign tenant cannot see its OWN policy either, so the absence above " +
			"is not evidence of isolation -- the reader is returning nothing")
	}
}

// TestRulebookDB_NamesTheAuthorOrSaysItCannot -- the three author cases, which only
// a real join under RLS can produce.
//
// 🔴 THE THIRD CASE IS §4.5 DOING ITS JOB, NOT A BUG. policy_versions.created_by is
// an FK-LESS uuid (00007 defers the admin FK), so a row CAN name an id this tenant
// cannot resolve -- and RLS is exactly what makes a cross-tenant id unresolvable.
// The screen has to say "we cannot tell you who", which is a different claim from
// "nobody", and it must not print the id.
func TestRulebookDB_NamesTheAuthorOrSaysItCannot(t *testing.T) {
	f := newRulebookFixture(t)
	mineName, mineRaw := shippedDoc(t, 0)
	ghostName, ghostRaw := shippedDoc(t, 1)
	systemName, systemRaw := shippedDoc(t, 2)

	f.storePolicy(t, f.tenantID, mineName, "baseline", true, mineRaw, &f.adminID)
	// AN ADMIN OF THE OTHER TENANT. The row is legal -- there is no FK to stop it --
	// and the join cannot resolve it, because RLS hides that admin from this tenant.
	f.storePolicy(t, f.tenantID, ghostName, "baseline", true, ghostRaw, &f.foreignAdmin)
	f.storePolicy(t, f.tenantID, systemName, "baseline", true, systemRaw, nil)

	screen, err := f.rules.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	authors := map[string]string{}
	for _, r := range screen.Baseline {
		if len(r.History) > 0 {
			authors[r.Name] = r.History[0].Author
		}
	}
	if got := authors[mineName]; got != "Rita Camilleri" {
		t.Errorf("a version written by this tenant's own admin is attributed to %q, want the "+
			"admin's name", got)
	}
	if got := authors[ghostName]; got != AuthorUnresolved {
		t.Errorf("a version whose author this tenant cannot resolve is attributed to %q, want "+
			"%q -- a blank would read as 'nobody', which is a different claim", got, AuthorUnresolved)
	}
	if got := authors[systemName]; got != AuthorSystem {
		t.Errorf("a system-provisioned version is attributed to %q, want %q", got, AuthorSystem)
	}
	// §4.7-adjacent: the unresolvable id must not be printed anywhere on the screen.
	// It identifies no person to the reader and answers no question a manager has.
	for _, r := range screen.Baseline {
		for _, h := range r.History {
			if strings.Contains(h.Author, f.foreignAdmin.String()) {
				t.Errorf("the screen printed the raw id of an unresolvable author: %q", h.Author)
			}
		}
	}
}

// TestRulebookDB_ShowsTheWindowsTheEngineWasGivenAndTheShippedGuardrailOrder -- the
// two derived facts, end to end through a real read.
func TestRulebookDB_ShowsTheWindowsTheEngineWasGivenAndTheShippedGuardrailOrder(t *testing.T) {
	f := newRulebookFixture(t)
	screen, err := f.rules.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	shipped := policy.Guardrails(testParams())
	if len(screen.Guardrails) != len(shipped) {
		t.Fatalf("the screen carries %d guardrail(s), the engine ships %d",
			len(screen.Guardrails), len(shipped))
	}
	for i, g := range screen.Guardrails {
		if g.Sid != shipped[i].Sid || g.Order != i+1 {
			t.Errorf("position %d: screen has (%d, %s), engine has (%d, %s)",
				i, g.Order, g.Sid, i+1, shipped[i].Sid)
		}
	}
	for _, g := range screen.Guardrails {
		if g.Sid != "sys:person-debounce" {
			continue
		}
		if g.Bounded == nil {
			t.Fatal("the debounce guardrail carries no tunable window")
		}
		if g.Bounded.Current != "90 s" {
			t.Errorf("the screen shows a debounce window of %q; the rulebook was given 90 s. "+
				"A screen that printed the shipped default instead would be describing a "+
				"guardrail nobody is running (hand-off N3, one layer out).", g.Bounded.Current)
		}
	}
	if screen.Zone == nil {
		t.Error("the screen carries no timezone, so version stamps would be rendered in " +
			"whatever the server's clock says (§6)")
	}
}

// TestRulebookDB_AnUnreadableDocumentReportsTheWholeLayerAsSilenced -- the blocking
// property, produced by REAL ROWS rather than by a fabricated screen.
//
// 🔴 THE STATE IS REACHABLE AND CHEAP TO REACH. policy_versions.document is jsonb
// and FREE-FORM at the database layer (00007, deliberately, for forward-version
// compatibility), so `{}` is a legal row -- and the development database holds tens
// of thousands of them. What it costs is not one rule: the tap engine reports the
// unparseable document as missing, cannot repair it (materialising conflicts on
// (tenant, policy, version_no) and does nothing) and decides on the guardrails
// ALONE, dropping every other baseline rule and the tenant's own policies with it.
// checkin.TestPolicySetDB_ACorruptStoredDocumentFallsBackToGuardrailsAlone pins
// that half against the same database.
func TestRulebookDB_AnUnreadableDocumentReportsTheWholeLayerAsSilenced(t *testing.T) {
	f := newRulebookFixture(t)
	brokenName, _ := shippedDoc(t, 0)
	okName, okRaw := shippedDoc(t, 1)
	f.storePolicy(t, f.tenantID, brokenName, "baseline", true, []byte(`{}`), nil)
	f.storePolicy(t, f.tenantID, okName, "baseline", true, okRaw, nil)

	screen, err := f.rules.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	if !screen.GuardrailsOnly {
		t.Fatal("with one unreadable stored document the screen does not report that the " +
			"engine is on guardrails alone; every other rule then reads as if it decided")
	}
	if len(screen.Unreadable) != 1 || screen.Unreadable[0] != brokenName {
		t.Errorf("Unreadable = %v, want exactly %q", screen.Unreadable, brokenName)
	}

	// POSITIVE CONTROL, on the same fixture and the same reader: a tenant whose
	// stored documents all parse reports the flag OFF. Without it, a field that is
	// always true would satisfy the assertion above.
	clean, err := f.rules.Screen(context.Background(), f.foreignTenant)
	if err != nil {
		t.Fatalf("Screen (control tenant): %v", err)
	}
	if clean.GuardrailsOnly || len(clean.Unreadable) != 0 {
		t.Errorf("a tenant with no unreadable document reports GuardrailsOnly=%v unreadable=%v",
			clean.GuardrailsOnly, clean.Unreadable)
	}
}
