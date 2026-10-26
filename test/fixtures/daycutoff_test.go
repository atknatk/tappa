package fixtures

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tap"
)

// TestFarDayCutoff_NoBusinessDayBoundaryFallsNearNow is the helper's property, the half of
// "the suite does not depend on the hour" that can be proven without a database (M15-04): for
// every `now` on a 5-minute grid over whole days — ordinary days and the DST days of each zone —
// the cutoff FarDayCutoff gives puts no business-day boundary in [now − 12 h, now + 6 h]: the
// business day containing now starts before now − 12 h and ends after now + 6 h, by the engine's
// own S and Next (tap.BusinessDayStart, tap.NextBusinessDayStart). The value is also a legal
// cutoff (a minute of the day, below 24:00 — migration 00064's CHECK).
func TestFarDayCutoff_NoBusinessDayBoundaryFallsNearNow(t *testing.T) {
	days := map[string][]string{
		// Malta is every seeded and test-built tenant's zone (tenants.timezone's default).
		"Europe/Malta": {"2026-01-15", "2026-03-29", "2026-10-25", "2027-03-28", "2027-10-31"},
		"UTC":          {"2026-06-01"},
		// A half-hour offset and a half-hour DST step: the rounding and the step together.
		"America/St_Johns":    {"2026-03-08", "2026-11-01"},
		"Australia/Lord_Howe": {"2026-04-05", "2026-10-04"},
	}
	points, violations := 0, 0
	for zone, ds := range days {
		for _, d := range ds {
			day, err := time.Parse("2006-01-02", d)
			if err != nil {
				t.Fatal(err)
			}
			for now := day.Add(-12 * time.Hour); now.Before(day.Add(36 * time.Hour)); now = now.Add(5 * time.Minute) {
				points++
				c, err := FarDayCutoff(now, zone)
				if err != nil {
					t.Fatal(err)
				}
				if c < 0 || c >= 24*time.Hour || c%time.Minute != 0 {
					t.Fatalf("%s %v: cutoff %v is not a minute of the day", zone, now, c)
				}
				dc := tap.DayCutoff{Cutoff: c, Timezone: zone}
				s, ok := tap.BusinessDayStart(now, dc)
				n, ok2 := tap.NextBusinessDayStart(s, dc)
				if !ok || !ok2 {
					t.Fatalf("%s %v: cutoff %v unresolvable", zone, now, c)
				}
				if !s.Before(now.Add(-12*time.Hour)) || !n.After(now.Add(6*time.Hour)) {
					if violations++; violations <= 5 {
						t.Errorf("%s now %v: cutoff %v gives the day [%v, %v), which reaches into [now − 12 h, now + 6 h]",
							zone, now.UTC(), c, s, n)
					}
				}
			}
		}
	}
	if violations > 0 {
		t.Fatalf("%d of %d points violate", violations, points)
	}
	t.Logf("%d points, 0 violations", points)
}

// TestFarDayCutoff_EveryDirectionChainTestHasACutoffAwayFromNow is the source-level half (M15-04
// acceptance): the inventory below is every DB test that expects the engine to CLOSE a check-in
// — so turns red when a business-day boundary falls between its records — and each one reaches
// its employee's business day through one of three routes, which this test reads in the source:
//
//	far       the employee was created by a harness function that calls GiveFarDayCutoff (the
//	          chain is checked down to the call)
//	explicit  the test writes the cutoff it means (fixed wall clocks — the Rusty Bar night)
//	clock     the test fixes the service's clock and builds its own tenant's cutoffs
//	          (internal/domain/checkin/businessday_db_test.go)
//
// HOW THE INVENTORY WAS MADE, MEASURED (2026-10-21): the orchestrator was mutated to put every
// person's business-day boundary one microsecond before the tap's own instant, and every DB
// package that builds a checkin.Service was run (internal/domain/checkin, internal/handler,
// internal/handler/api, internal/handler/operator, internal/sandbox). Nine tests went red — the
// first nine below; the rest never expect the engine to close an entry. With the helper in place and
// the same mutation sparing a person-tier cutoff, the only red was the Rusty Bar night of
// TestSeedDB_ADayAtKFStJulians, which is the explicit route by design. A NEW direction-chain test
// belongs here; the mutation is how to find one.
func TestFarDayCutoff_EveryDirectionChainTestHasACutoffAwayFromNow(t *testing.T) {
	root := filepath.Join("..", "..")
	type entry struct {
		file, test string
		calls      []string // each must appear in the test's body
	}
	inventory := []entry{
		{"internal/domain/checkin/declared_at_db_test.go", "TestRecordDB_TheOpenCheckInIsReadAtTheDeclaredInstant", []string{"w.employee("}},
		{"internal/domain/checkin/feed_db_test.go", "TestFeedDirection_NightShiftInThenOutAcrossMidnight", []string{"w.farDayCutoff("}},
		{"internal/domain/checkin/holding_db_test.go", "TestHoldingTap_DirectionChainStaysAtTheEmployer", []string{"newHoldingWorld(", "w.employee("}},
		{"internal/handler/checkin_db_test.go", "TestCheckinDB_PracticeThenDirectionTogglesAgainstTheLastOpenCheckIn", []string{"h.newEmployee("}},
		{"internal/handler/e2e_db_test.go", "TestE2E_AClientCannotDeclareThePracticeFlag", []string{"h.newEmployee("}},
		{"internal/handler/day_db_test.go", "TestSeedDB_ADayAtKFStJulians", []string{"f.hire(", "f.setDayCutoff(t, ivan, \"05:00\")"}},
		{"internal/handler/practice_direction_db_test.go", "TestSeedDB_APracticeRowNeverHidesAnOlderOpenCheckIn", []string{"f.hire("}},
		{"internal/handler/practice_direction_db_test.go", "TestSeedDB_ASecondActivationWritesNoRecordAndLeavesTheChainAlone", []string{"f.hire("}},
		{"internal/handler/api/examples_db_test.go", "TestOpenAPI_TheExamplesAreTheHandlersAnswers", []string{"runExampleStory("}},
		// M15-16: the claim approval's lock against a live tap that must otherwise read the check-in open.
		{"internal/domain/claim/claim_db_test.go", "TestDecideDB_TheLockHoldsATapUntilTheApprovalCommits", []string{"fixtures.GiveFarDayCutoff("}},
		{"internal/handler/claim_db_test.go", "TestClaimDB_YesterdaysOpenCheckInIsAskedAndSentOnce", []string{"newTapHarness("}},
		{"internal/handler/claim_db_test.go", "TestClaimDB_AnAnchorWithNoBusinessDayShowsNoCard", []string{"newTapHarness("}},
		{"internal/handler/claim_db_test.go", "TestClaimDB_TheCardIsShownOnTwoTapsOnly", []string{"newTapHarness("}},
		{"internal/handler/claim_db_test.go", "TestClaimDB_AContextIsBoundToItsSessionAndItsLabel", []string{"newTapHarness("}},
		{"internal/handler/claim_db_test.go", "TestClaimDB_ASuspendedBusinessesEmployeeStillSendsTheClaim", []string{"newTapHarness("}},
		// The clock route: M15-04's own acceptance tests.
		{"internal/domain/checkin/businessday_db_test.go", "TestRecordDB_ACheckInOfThePreviousBusinessDayStaysOpen", []string{"recordAt("}},
		{"internal/domain/checkin/businessday_db_test.go", "TestRecordDB_TheRustyBarNightIsOneBusinessDay", []string{"recordAt("}},
		{"internal/domain/checkin/businessday_db_test.go", "TestRecordDB_TheBusinessDayIsTheProfileBranchesNotTheTappedOnes", []string{"recordAt("}},
		{"internal/domain/checkin/businessday_db_test.go", "TestHoldingTap_TheBusinessDayIsTheEmployersOnThePeersPlaque", []string{"recordAt("}},
	}
	// The harness functions the "far" route goes through, and what each must call: down to
	// GiveFarDayCutoff itself.
	harness := []entry{
		{"internal/domain/checkin/holding_db_test.go", "employee", []string{"w.farDayCutoff(tenant, id, time.Now())"}},
		{"internal/domain/checkin/holding_db_test.go", "farDayCutoff", []string{"fixtures.GiveFarDayCutoff("}},
		{"internal/handler/checkin_db_test.go", "newEmployee", []string{"fixtures.GiveFarDayCutoff("}},
		{"internal/handler/seedflow_db_test.go", "hire", []string{"f.insertEmployee("}},
		{"internal/handler/seedflow_db_test.go", "insertEmployee", []string{"fixtures.GiveFarDayCutoff("}},
		{"internal/handler/e2e_db_test.go", "newHarness", []string{"fixtures.GiveFarDayCutoff("}},
		{"internal/handler/tap_db_test.go", "newTag", []string{"fixtures.GiveFarDayCutoff("}},
		{"internal/handler/api/examples_db_test.go", "runExampleStory", []string{"fixtures.GiveFarDayCutoff("}},
	}
	bodies := map[string]map[string]string{}
	body := func(file, fn string) (string, bool) {
		if bodies[file] == nil {
			src, err := os.ReadFile(filepath.Join(root, file))
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, file, src, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			bodies[file] = map[string]string{}
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
					bodies[file][fd.Name.Name] = string(src[fset.Position(fd.Body.Pos()).Offset:fset.Position(fd.Body.End()).Offset])
				}
			}
		}
		b, ok := bodies[file][fn]
		return b, ok
	}
	for _, list := range [][]entry{inventory, harness} {
		for _, e := range list {
			b, ok := body(e.file, e.test)
			if !ok {
				t.Errorf("%s: no function %s — renamed or moved? update this inventory with it", e.file, e.test)
				continue
			}
			for _, c := range e.calls {
				if !strings.Contains(b, c) {
					t.Errorf("%s %s does not call %q: its business day is no longer kept away from now", e.file, e.test, c)
				}
			}
		}
	}
}

// TestGiveFarDayCutoff_RefusesEverySeededEmployee: every seeded person (the Emp* handles of
// ids.go, both tenants) is refused before anything is written; an employee a test creates is not.
func TestGiveFarDayCutoff_RefusesEverySeededEmployee(t *testing.T) {
	seeded := []uuid.UUID{
		EmpKFMariaBorg, EmpKFJosephCamilleri, EmpKFAntoineVella, EmpKFRitaZammit, EmpKFDavidGrech,
		EmpKFSofiaRossi, EmpKFAhmedHassan, EmpKFElenaPetrova, EmpKFMarcoBugeja, EmpKFNadiaFarrugia,
		EmpKFLiamOBrien, EmpKFPriyaSharma, EmpKFCarlosMendez, EmpKFGraceAttard, EmpKFPaulSpiteri,
		EmpKFAndreaMicallef, EmpKFRobertaFenech, EmpKFIvanPetrov, EmpKFEmmaCaruana, EmpKFJuliaSciberras,
		EmpKFOmarAziz, EmpKFGeorgeMuscat, EmpKFKevinBorg, EmpKFRyanGauci, EmpKFStephaniePace,
		EmpKMAntoniaGrima, EmpKMFrankDebono, EmpKMSaviourBonnici, EmpKMDraganNikolic, EmpKMCharleneVella,
		EmpKMPeterFalzon, EmpKMRosariaSchembri, EmpKMYusufDemir, EmpKMDorisMicallef, EmpKMAnaSilva,
		EmpKMJosephButtigieg,
	}
	for _, id := range seeded {
		if err := refuseSeedEmployee(id); err == nil {
			t.Errorf("the seeded employee %s was not refused", id)
		}
	}
	for i := 0; i < 100; i++ {
		if id := uuid.New(); refuseSeedEmployee(id) != nil {
			t.Fatalf("a test's own employee %s was refused", id)
		}
	}
}
