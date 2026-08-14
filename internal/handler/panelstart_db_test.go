package handler

// panelstart_db_test.go -- M7-03 phase B against REAL Postgres.
//
// 🔴 WHY THE FAKE-LEDGER TESTS NEXT DOOR ARE NOT ENOUGH, and the reason is the one
// this repo has hit before: panelstart_test.go hands the handler a ledger.Plaques
// the TEST chose, so every one of its assertions would pass unchanged against a
// query that returns zeros for everybody, ignores the tenant filter, or was never
// wired to the section at all. Those are exactly the three ways this feature can be
// wrong. Only real rows in a real database measure them.
//
// FOUR PROPERTIES ARE MEASURED HERE:
//
//	1. the counts come from the DATABASE and move when the rows move;
//	2. §4.5 -- another tenant's plaques do not answer this tenant's question;
//	3. §4.7 -- the plaque uid never reaches the page (the notice prints counts);
//	4. the section is a READ -- rendering it writes nothing, counted before/after.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// newPlaqueUID mints a canonical 14-hex-digit uid (00013's CHECK is upper case).
func newPlaqueUID() string {
	return strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:14]
}

// seedStartupTenant gives a tenant one venue and no plaques at all -- the state a
// business is provisioned in (M7-02's Provision writes tenant, locations and the
// first admin, and no tags row).
func seedStartupTenant(t *testing.T, p *panelHarness, tenantID uuid.UUID, label string) uuid.UUID {
	t.Helper()
	locationID := uuid.New()
	if err := p.data.WithTenant(context.Background(), tenantID,
		func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx,
				`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`,
				locationID, tenantID, label+" Front Door")
			return e
		}); err != nil {
		t.Fatalf("seeding %s: %v", label, err)
	}
	return locationID
}

// TestPanelLandingDB_ReadsThePlaqueStateFromTheDatabase walks one business through
// the three states that matter, in the order a real customer meets them.
func TestPanelLandingDB_ReadsThePlaqueStateFromTheDatabase(t *testing.T) {
	p := newPanelHarness(t)
	locationID := seedStartupTenant(t, p, p.tenantID, "Startup")
	p.signIn(t)

	const (
		noneLoaded   = "No plaques have been loaded for this business yet"
		inStock      = "None of this business's plaques is mounted"
		outOfService = "Every plaque this business has is out of service"
	)

	// STEP 1 -- exactly as provisioned: a venue, no plaques.
	_, body := p.get(t, transactionsHref)
	if !strings.Contains(body, noneLoaded) {
		t.Fatalf("a freshly provisioned business is not told why nobody can tap.\n"+
			"This is the screen the wizard's \"Sign in to your dashboard\" button opens,\n"+
			"and before M7-03 phase B it said only \"Pick another day above\".\nGot:\n%s",
			excerpt(body))
	}

	// STEP 2 -- Tappa loads one plaque. It is in the box, not on a wall.
	uid := newPlaqueUID()
	mustExec(t, p, p.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, status, last_ctr, aes_key_ref)
		 VALUES ($1, $2, NULL, 'unassigned', 0, 'test-key-ref')`, uid, p.tenantID)

	_, body = p.get(t, transactionsHref)
	if !strings.Contains(body, inStock) {
		t.Errorf("a plaque in stock still reads as \"nothing loaded\"; the count did not "+
			"come from the database.\nGot:\n%s", excerpt(body))
	}
	if strings.Contains(body, noneLoaded) {
		t.Error("the screen says nothing has been loaded while a row exists")
	}
	// §4.7 -- THE UID IS NOT A SECRET BUT IT IS ALSO NOT NEEDED HERE, and a notice
	// that grew a plaque identifier would be a screen printing a value no pixel of
	// it renders. The assertion is cheap and the drift it catches is silent.
	if strings.Contains(body, uid) {
		t.Errorf("the landing section printed a plaque uid (%s); it needs counts, not "+
			"identifiers", uid)
	}

	// STEP 3 -- somebody mounts it. The notice must go away entirely.
	mustExec(t, p, p.tenantID,
		`UPDATE tags SET status = 'active', location_id = $2 WHERE uid = $1 AND tenant_id = $3`,
		uid, locationID, p.tenantID)

	_, body = p.get(t, transactionsHref)
	for _, claim := range []string{noneLoaded, inStock, outOfService} {
		if strings.Contains(body, claim) {
			t.Errorf("a business with a plaque on the wall is still told %q", claim)
		}
	}
	// POSITIVE CONTROL for step 3: the page really did render, so "the sentence is
	// gone" is not "the page is gone".
	if !strings.Contains(body, "No taps recorded on this day") {
		t.Errorf("the transactions section did not render its empty state; the check "+
			"above proves nothing.\nGot:\n%s", excerpt(body))
	}

	// STEP 4 -- the plaque is retired. Nothing is in service and nothing is in the
	// box, which is a THIRD sentence and not the first one.
	mustExec(t, p, p.tenantID,
		`UPDATE tags SET status = 'retired', retired_at = now() WHERE uid = $1 AND tenant_id = $2`,
		uid, p.tenantID)

	_, body = p.get(t, transactionsHref)
	if !strings.Contains(body, outOfService) {
		t.Errorf("a business whose only plaque is retired is not told so.\nGot:\n%s", excerpt(body))
	}
	if strings.Contains(body, inStock) {
		t.Error("a retired plaque is being counted as stock -- a manager would be sent " +
			"to look in an empty drawer. This is what counting in_stock rather than " +
			"subtracting exists to prevent.")
	}
}

// TestPanelLandingDB_AnotherTenantsPlaquesAnswerNothing is §4.5 on this new read,
// AND IT MEASURES THE BRACES RATHER THAN THE BELT.
//
// 🔴 THE COMMENT HERE USED TO CLAIM BOTH, AND CONTRADICTED ITSELF ONE LINE LATER.
// This test signs in as tenant A and asks for A's own screen, so RLS alone already
// hides B's plaque: an audit removed the explicit tenant predicate from the
// generated statement and EVERY TestPanelLandingDB_* test in this file stayed GREEN.
// What it proves is that the POLICY is in force on this read — which is worth
// proving, and is not the same thing.
//
// ⚠️ THAT SENTENCE USED TO CARRY A COUNT ("all three"), AND THE COUNT WAS STALE
// WITHIN ONE ROUND — this file holds four now. transactions.templ in this same
// package already records the rule: a count of things that grow is a fact about the
// calendar, not about the design. Today's number:
//
//	grep -c '^func TestPanelLandingDB_' internal/handler/panelstart_db_test.go
//
// THE BELT — the explicit `WHERE g.tenant_id = @tenant_id` §4.5 also requires — is
// measured in two other places, and an audit confirmed both bite:
//   - statically, on the SOURCE sql, by internal/domain/tenant's top-level-conjunct
//     scan (neutralising the predicate there, in either spelling, turns it red);
//   - at runtime, by TestBelt_Tags00013_PanelQueriesCarryTheirOwnTenantPredicate in
//     internal/db, which stays inside ONE tenant's context and passes the OTHER
//     tenant's id, so RLS is satisfied throughout and only the predicate can refuse.
//     CountTenantPlaques has a sub-test there.
//
// ⚠️ MUTATING A GENERATED FILE MEASURES NOTHING ABOUT THE NET THAT READS THE SOURCE.
// That is how the first round of this work convinced itself the belt was unguarded.
func TestPanelLandingDB_AnotherTenantsPlaquesAnswerNothing(t *testing.T) {
	p := newPanelHarness(t)
	seedStartupTenant(t, p, p.tenantID, "Alpha")

	other := uuid.New()
	mustExec(t, p, other,
		`INSERT INTO tenants (id, name, vat_number, business_type, structure)
		 VALUES ($1, 'Plaque Neighbour Ltd', $2, 'bar', 'single')`,
		other, "VAT-"+other.String())
	otherLocation := seedStartupTenant(t, p, other, "Neighbour")
	mustExec(t, p, other,
		`INSERT INTO tags (uid, tenant_id, location_id, status, last_ctr, aes_key_ref)
		 VALUES ($1, $2, $3, 'active', 0, 'test-key-ref')`,
		newPlaqueUID(), other, otherLocation)

	p.signIn(t)
	_, body := p.get(t, transactionsHref)
	if !strings.Contains(body, "No plaques have been loaded for this business yet") {
		t.Errorf("this business has no plaques of its own, but the neighbour's working "+
			"plaque silenced the notice -- the count is not tenant-scoped.\nGot:\n%s",
			excerpt(body))
	}
}

// TestPanelLandingDB_ManualRecordsKeepTheDatePickerOffered is the audit's finding,
// reproduced end to end.
//
// 🔴 THE FIRST VERSION WITHDREW "Pick another day above" WHEN NO PLAQUE HAD BEEN
// LOADED, on the assumption that no plaque means no record. A manager can type a
// record by hand — channel='manual', tag_uid NULL — in a business holding ZERO
// plaques, which is exactly the state the advice was being withdrawn in. So the one
// person whose records really were on another day was the one person not told to
// look there.
//
// The row below is written the way internal/domain/manual writes one: no plaque, no
// counter, a verdict of 'ok' and a date three days back. The screen is then read for
// TODAY, which is empty either way — the whole question is what it says about the
// other days.
func TestPanelLandingDB_ManualRecordsKeepTheDatePickerOffered(t *testing.T) {
	p := newPanelHarness(t)
	locationID := seedStartupTenant(t, p, p.tenantID, "Typed")
	p.signIn(t)

	const advice = "Pick another day above"

	// BEFORE: no plaques, no records. The advice is withdrawn, which is right.
	_, body := p.get(t, transactionsHref)
	if strings.Contains(body, advice) {
		t.Fatalf("a business with no records at all is still sent to the date picker")
	}
	// POSITIVE CONTROL: the empty state itself is on the page, so "the advice is
	// gone" is not "the page is gone".
	if !strings.Contains(body, "Nothing was recorded here on this day") {
		t.Fatalf("the empty state is missing; this test measures nothing.\nGot:\n%s", excerpt(body))
	}

	// A manager types last week in. NO PLAQUE IS INVOLVED.
	employeeID := uuid.New()
	adminID := uuid.New()
	mustExec(t, p, p.tenantID,
		`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
		 VALUES ($1, $2, $3, 'Typed Person', 'active')`, employeeID, p.tenantID, locationID)
	mustExec(t, p, p.tenantID,
		`INSERT INTO transactions
		   (tenant_id, employee_id, location_id, tag_uid, type, occurred_at,
		    sun_valid, ip_match, gps_match, trust, verdict, channel, practice, queued,
		    entered_by)
		 VALUES ($1, $2, $3, NULL, 'in', now() - interval '3 days',
		         false, false, false, 20, 'ok', 'manual', false, false, $4)`,
		p.tenantID, employeeID, locationID, adminID)

	// AFTER: still no plaques, but there IS a record — on another day.
	_, body = p.get(t, transactionsHref)
	if !strings.Contains(body, advice) {
		t.Errorf("a manager typed a record three days ago into a business with NO plaques, "+
			"and today's empty page still does not mention another day.\n"+
			"That manager's records exist and the screen is hiding the only control that "+
			"reaches them.\nGot:\n%s", excerpt(body))
	}
	// The plaque notice is UNAFFECTED — the business still has no plaque, and that is
	// still true and still worth saying. The two facts are independent by design.
	if !strings.Contains(body, "No plaques have been loaded for this business yet") {
		t.Error("the plaque notice disappeared when a manual record arrived; the two " +
			"readings are supposed to be independent")
	}
}

// TestPanelLandingDB_TheLandingSectionWritesNothing is the read-path proof.
//
// 🔴 IT COUNTS ROWS RATHER THAN TRUSTING THE SHAPE OF THE CODE. M6-09 phase B shipped
// a "do not write the same name twice" instruction as a SELECT EXISTS followed by an
// INSERT -- a read-then-write that needed migration 00015 to undo -- so "this is
// obviously a read" is not evidence in this repo. The three tables a plaque count
// could plausibly touch are counted before and after ten renders.
func TestPanelLandingDB_TheLandingSectionWritesNothing(t *testing.T) {
	p := newPanelHarness(t)
	seedStartupTenant(t, p, p.tenantID, "Quiet")
	p.signIn(t)

	tables := []string{"tags", "transactions", "audit_log"}
	before := tenantRowCounts(t, p, tables)

	for i := 0; i < 10; i++ {
		res, _ := p.get(t, transactionsHref)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("render %d answered %d, want 200", i, res.StatusCode)
		}
	}

	after := tenantRowCounts(t, p, tables)
	for _, table := range tables {
		if before[table] != after[table] {
			t.Errorf("%s went from %d rows to %d across ten renders of a READ section",
				table, before[table], after[table])
		}
	}

	// POSITIVE CONTROL FOR THE COUNTER. Without it, a counting helper that always
	// returned the same number would make this test pass forever.
	mustExec(t, p, p.tenantID,
		`INSERT INTO tags (uid, tenant_id, location_id, status, last_ctr, aes_key_ref)
		 VALUES ($1, $2, NULL, 'unassigned', 0, 'test-key-ref')`, newPlaqueUID(), p.tenantID)
	control := tenantRowCounts(t, p, tables)
	if control["tags"] != after["tags"]+1 {
		t.Fatalf("the row counter did not notice a row that was definitely inserted "+
			"(%d then %d); the comparison above measures nothing",
			after["tags"], control["tags"])
	}
}

// tenantRowCounts counts this tenant's rows in each table, THROUGH THE APPLICATION
// ROLE so RLS is in force -- the same view of the world the panel has.
//
// It builds on review_db_test.go's countRows rather than redeclaring it; the table
// names are this file's own literals and never input.
func tenantRowCounts(t *testing.T, p *panelHarness, tables []string) map[string]int {
	t.Helper()
	out := make(map[string]int, len(tables))
	for _, table := range tables {
		out[table] = countRows(t, p, p.tenantID,
			fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id = $1`, table))
	}
	return out
}

// mustExec runs one statement inside the given tenant's context.
func mustExec(t *testing.T, p *panelHarness, tenantID uuid.UUID, sql string, args ...any) {
	t.Helper()
	if err := p.data.WithTenant(context.Background(), tenantID,
		func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, sql, args...)
			return e
		}); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

// excerpt trims a rendered page down to something a failure message can carry.
func excerpt(body string) string {
	const max = 600
	i := strings.Index(body, "<main")
	if i < 0 {
		i = 0
	}
	if len(body)-i > max {
		return body[i : i+max]
	}
	return body[i:]
}
