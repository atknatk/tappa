package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/manual"
)

// The panel confirmation is NOT single-use (deactivateconfirm.go): within its TTL one minted
// value can be spent again by whoever holds the session's cookie jar. Backlog T12 was accepted
// on 2026-11-08 (user decision, ADR 0005 "Kabul edilen backlog bulguları") on the ground that
// every gate has a SECOND defence on the write itself. This inventory is where that ground is
// kept: every action the confirmation can authorise is classified here with its second
// defence and the tests that measure it, and a new gate that is not classified turns this red.

// confirmReplay is what a second spend of the same value does at a gate.
type confirmReplay int

const (
	// replayWritesNothing: the second spend meets a predicate, a constraint or a stale
	// subject and writes no product row and no audit row.
	replayWritesNothing confirmReplay = iota + 1
	// replayWritesAfterAnUndo: nothing straight after the first spend; once another route has
	// undone the act inside the TTL, the same value can do it again. The holder is the same
	// signed-in operator, who could mint a fresh value with one request of their own session
	// (the review page; a POST at most gates), so this re-arms nothing they could not already do.
	replayWritesAfterAnUndo
)

// There is no class for "a replay writes a second record": since backlog T258 (2026-11-09) every
// gate has a second defence, and a new gate without one is a user decision, not an entry here.

type confirmGate struct {
	replay confirmReplay
	// defence is the second defence, named where it lives.
	defence string
	// proofs are tests that measure it; each must exist.
	proofs []string
}

// confirmGates is keyed by the constant's NAME in deactivateconfirm.go.
var confirmGates = map[string]confirmGate{
	"confirmActionDeactivate": {replayWritesNothing,
		"DeactivateEmployee carries `status <> 'deactivated'` (db/queries/employees.sql); no row means ErrAlreadyDeactivated and no audit row, and no query sets an employee active again (ADR 0010)",
		[]string{"TestEmployeeDeactivate_TheOneShotDependsOnTheClient", "TestPanelEmployeesDB_ARepeatedConfirmationWritesOnlyOnce", "TestStaffDB_ASecondDeactivationWritesNothing"}},
	"confirmActionDeleteVenue": {replayWritesNothing,
		"DeleteLocation matches no row the second time: ErrUnknownVenue, no audit row",
		[]string{"TestVenues_ASecondRemovalOfTheSameRowWritesNothing"}},
	"confirmActionDeleteDepartment": {replayWritesNothing,
		"DeleteDepartment matches no row the second time: ErrUnknownDepartment, no audit row",
		[]string{"TestVenues_ASecondRemovalOfTheSameRowWritesNothing"}},
	"confirmActionReplacePlaque": {replayWritesNothing,
		"RetireTagForReplacement carries `status = 'active'` (db/queries/tags.sql) and retired is terminal, so a second spend retires nothing and spends no spare",
		[]string{"TestPlaquesDB_TenConcurrentReplacementsSpendExactlyONESpare"}},
	"confirmActionUnmountPlaque": {replayWritesAfterAnUndo,
		"UnmountTagFromWall carries `status = 'active'` (db/queries/tags.sql); an ungated mount of the same plaque re-arms it",
		[]string{"TestPlaquesDB_TenConcurrentUnmountsProduceExactlyONEWinner"}},
	"confirmActionRecordManual": {replayWritesNothing,
		"manual.Record reads ExistingManualRecord under the person's tap lock (LockEmployeeForTap, Within's first statement) and writes nothing when this author already has a manual row for this person, direction and instant: ErrAlreadyRecorded, and the panel says it was already saved or what the minute holds (T258)",
		[]string{"TestManualRecord_TheSameRecordPressedNTimesIsWrittenOnce", "TestManualEntryRecord_AReplayedConfirmationIsAnsweredAlreadySaved"}},
	"confirmActionPolicyEdit": {replayWritesAfterAnUndo,
		"on an existing rule the subject carries a digest of what the review page showed (policyChangeSubject), so a write makes the value stale; Bind and Unbind refuse a repeat (ErrAlreadyBound, ErrNotBound); a NEW rule shows no rule to digest, and there the defence is the UNIQUE name (00015): ErrNameTaken; an undo (or a rename) re-arms it",
		[]string{"TestPolicyConfirmation_IsBoundToWhatThePageShowed", "TestRuleWriterDB_BindingsAreWrittenAndRemovedWithATrail", "TestRuleWriterDB_AnEditIsAlwaysANewVersion"}},
	"confirmActionCloseBillingPeriod": {replayWritesNothing,
		"UNIQUE (tenant_id, period_month) on billing_periods (00016): the second close is ErrAlreadyClosed",
		[]string{"TestBillingClose_ASecondFreezeOfTheSameMonthChangesNothing", "TestBillingDB_ClosingRefusesWhatItMust"}},
	"confirmActionDayCutoff": {replayWritesNothing,
		"the subject carries the version (the value on file and the count of change rows, businessDaySubject), so any write makes the value stale; the domain refuses ErrDayCutoffStale and ErrSameDayCutoff",
		[]string{"TestBusinessDayDB_AConfirmationIsSpentByTheWriteAndStaleAfterAnyChange"}},
	"confirmActionMoveAcrossCutoff": {replayWritesAfterAnUndo,
		"Move refuses ErrSamePlacement while the person is still where the first spend put them; a move back re-arms it",
		[]string{"TestStaffDB_MovingToTheSamePlaceWritesNothing"}},
	// T59. AFTER AN UNDO, not "nothing": a replay revokes nothing and writes no audit row while no
	// session has appeared since, but a new session inside the TTL (the person re-activated -- the
	// route that undoes a sign-out) is revoked by the same value, with its own trail row.
	"confirmActionSignOutPhones": {replayWritesAfterAnUndo,
		"RevokeSessionsForEmployee carries `revoked_at IS NULL` (db/queries/sessions.sql) and SignOutPhones writes its trail row only when a session was revoked: a second spend is ErrNoLiveSessions (problem=no-phones) with no audit row; a session issued since (a re-activation) re-arms it",
		[]string{"TestStaffDB_ASecondSignOutWritesNothing", "TestPanelSignOutDB_TheConfirmedPostRevokesEverySessionAndTrailsTheCount", "TestStaffDB_ConcurrentSignOutsRevokeEachSessionOnce"}},
	"confirmActionApproveClaim": {replayWritesNothing,
		"a decided claim is ErrAlreadyDecided before the manual write (internal/domain/claim decide.go), and punch_claim_decisions carries UNIQUE (tenant_id, claim_id) (00068)",
		[]string{"TestDecideDB_AnApprovalIsOneRecordOneDecisionAndTwoTrailRows"}},
}

// confirmActionNamesFromSource is confirmActionsFromSource with the identifiers rather than
// the values: the inventory is keyed by what a reader greps for.
func confirmActionNamesFromSource(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "deactivateconfirm.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing deactivateconfirm.go: %v", err)
	}
	var names []string
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			for _, n := range spec.(*ast.ValueSpec).Names {
				if strings.HasPrefix(n.Name, "confirmAction") {
					names = append(names, n.Name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// TestConfirmationGates_EveryGateIsClassifiedWithItsSecondDefence fails when a confirmation
// action exists that the inventory does not classify (or the other way round), and when an entry
// names no replay class, no defence or a test that does not exist.
func TestConfirmationGates_EveryGateIsClassifiedWithItsSecondDefence(t *testing.T) {
	names := confirmActionNamesFromSource(t)
	if len(names) < 11 {
		t.Fatalf("read %d confirmation actions from deactivateconfirm.go, want at least the 11 of 2026-11-08", len(names))
	}
	for _, n := range names {
		if _, ok := confirmGates[n]; !ok {
			t.Errorf("%s is a confirmation gate the inventory does not classify. The confirmation is "+
				"not single-use (ADR 0005, T12): name the write's own second defence and a test that "+
				"measures a replay before adding it here", n)
		}
	}
	inSource := map[string]bool{}
	for _, n := range names {
		inSource[n] = true
	}
	tests := allTestFuncs(t)
	for name, g := range confirmGates {
		if !inSource[name] {
			t.Errorf("the inventory classifies %s, which deactivateconfirm.go no longer declares", name)
		}
		if g.replay < replayWritesNothing || g.replay > replayWritesAfterAnUndo || strings.TrimSpace(g.defence) == "" {
			t.Errorf("%s: no replay class or no second defence", name)
		}
		if len(g.proofs) == 0 {
			t.Errorf("%s names no test that measures its second defence", name)
		}
		for _, p := range g.proofs {
			if !tests[p] {
				t.Errorf("%s names %s, which is not a test in this repository", name, p)
			}
		}
	}
}

// TestConfirmationGates_EverySpendSiteNamesAClassifiedGate closes the inventory's blind spot:
// a gate is where a value is SPENT, and a spend that passed a bare string or another variable
// would be a gate confirmGates never sees. Every confirmationRefusalFor call, and every
// a.confirm.parse call outside that helper, must name a confirmAction* constant the inventory
// classifies. (Mint sites may pass the action through a variable -- removalView, plaqueCard --
// because a value minted for an unclassified action can only be spent at a spend site.)
func TestConfirmationGates_EverySpendSiteNamesAClassifiedGate(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}
	spends := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					isRefusal := sel.Sel.Name == "confirmationRefusalFor"
					isParse := false
					if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "confirm" && sel.Sel.Name == "parse" {
						isParse = fn.Name.Name != "confirmationRefusalFor"
					}
					if !isRefusal && !isParse {
						return true
					}
					spends++
					at := fset.Position(call.Pos())
					if len(call.Args) < 2 {
						t.Errorf("%s: a confirmation spend with %d argument(s)", at, len(call.Args))
						return true
					}
					id, ok := call.Args[1].(*ast.Ident)
					if !ok || !strings.HasPrefix(id.Name, "confirmAction") {
						t.Errorf("%s: a confirmation is spent for an action that is not a confirmAction* "+
							"constant, so confirmGates cannot classify the gate", at)
						return true
					}
					if _, ok := confirmGates[id.Name]; !ok {
						t.Errorf("%s spends %s, which confirmGates does not classify", at, id.Name)
					}
					return true
				})
			}
		}
	}
	if spends < 11 {
		t.Fatalf("found %d confirmation spend site(s), want at least one per gate (11); the walk is not seeing them", spends)
	}
}

// allTestFuncs is every `func TestXxx(` in the repository's _test.go files.
func allTestFuncs(t *testing.T) map[string]bool {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)
	out := map[string]bool{}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".tools", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
	if len(out) < 100 {
		t.Fatalf("found %d tests under %s; the walk is reading the wrong tree", len(out), root)
	}
	return out
}

// TestManualEntryRecord_AReplayedConfirmationIsAnsweredAlreadySaved is the panel's half of T258: a
// post that left with the confirmation cookie still in the jar -- the second press of a double
// click, or a re-printed cookie, which is the same request -- reaches Record again, and the answer
// to the domain's ErrAlreadyRecorded is the done notice with the way to the record, not a second
// record and not an error. (The fake recorder models the domain's rule; the rule itself is
// measured under the real lock in internal/domain/manual.)
func TestManualEntryRecord_AReplayedConfirmationIsAnsweredAlreadySaved(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	form := confirmedEntry(t, b, "out", "2026-08-05", "17:00", "phone was flat")
	minted := b.cookies[adminConfirmCookieName]
	if minted == "" {
		t.Fatal("the review step minted no confirmation cookie; nothing below is measured")
	}
	first := b.do(http.MethodPost, manualRecordHref, form)
	if first.Code != http.StatusSeeOther {
		t.Fatalf("the confirmed write answered %d, want 303", first.Code)
	}
	saved := first.Header().Get("Location")

	// THE SECOND PRESS: the same value printed back.
	b.cookies[adminConfirmCookieName] = minted
	second := b.do(http.MethodPost, manualRecordHref, form)
	if second.Code != http.StatusOK {
		t.Fatalf("the second press answered %d, want 200 with the notice", second.Code)
	}
	if n := len(rec.entries()); n != 1 {
		t.Fatalf("%d record(s) after two presses, want 1", n)
	}
	page := htmlOf(t, second)
	for _, want := range []string{htmlText("That record was already saved."),
		htmlText("Pressing the button again did not enter it twice."),
		`href="` + htmlText(saved) + `"`, ">See it on The Pass</a>"} {
		if !strings.Contains(page, want) {
			t.Errorf("the second press's page does not carry %q", want)
		}
	}
	if strings.Contains(page, htmlText("Nothing was recorded")) {
		t.Error("the second press is drawn as a refusal; the record exists")
	}
}

// TestManualEntryRecord_AMinuteThatHoldsAnotherVenueOrNoteSaysSo is the user decision of
// 2026-11-10: when this author's record for this person, direction and minute already exists with
// another venue or another note, nothing is written (T258's rule) and the notice says what the
// minute holds -- not "already saved", which would claim this press's venue or note was kept.
func TestManualEntryRecord_AMinuteThatHoldsAnotherVenueOrNoteSaysSo(t *testing.T) {
	stj, sliema := uuid.New(), uuid.New()
	venueless := manual.Subject{NeedsVenue: true, Venues: []manual.Venue{{ID: stj, Name: "KF St Julians"}, {ID: sliema, Name: "KF Sliema"}}}
	confirmAt := func(t *testing.T, b *browser, venue uuid.UUID, note string) url.Values {
		t.Helper()
		form := url.Values{"id": {entryID.String()}, "direction": {"in"}, "date": {"2026-08-05"}, "time": {"09:00"}, "location": {venue.String()}}
		if note != "" {
			form.Set("note", note)
		}
		form.Set(confirmField, confirmTokenIn(t, htmlOf(t, b.do(http.MethodPost, manualEntryHref, form))))
		return form
	}
	cases := []struct {
		name       string
		venue      uuid.UUID
		note       string
		wantLast   string
		wantAbsent string
	}{
		{"another venue", sliema, "", "To correct the venue, enter the right one at a different minute.", "The note you typed was not saved."},
		{"another note", stj, "came in early", "The note you typed was not saved.", "To correct the venue"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &fakeRecorder{subject: venueless}
			b := panelBrowserWithRecorder(t, rec)
			if res := b.do(http.MethodPost, manualRecordHref, confirmAt(t, b, stj, "")); res.Code != http.StatusSeeOther {
				t.Fatalf("the first record answered %d, want 303", res.Code)
			}
			second := b.do(http.MethodPost, manualRecordHref, confirmAt(t, b, c.venue, c.note))
			if second.Code != http.StatusOK {
				t.Fatalf("the second record answered %d, want 200 with the notice", second.Code)
			}
			if n := len(rec.entries()); n != 1 {
				t.Fatalf("%d record(s), want 1: the minute was already taken", n)
			}
			page := htmlOf(t, second)
			for _, want := range []string{
				htmlText("This minute already has a record"),
				htmlText("There's already a check-in for Maria Borg at ") + `<span class="font-mono">09:00</span>` + htmlText(" (KF St Julians). This one wasn't saved."),
				htmlText(c.wantLast),
			} {
				if !strings.Contains(page, want) {
					t.Errorf("the notice does not carry %q", want)
				}
			}
			for _, never := range []string{htmlText("That record was already saved."), htmlText("Pressing the button again"), htmlText(c.wantAbsent), htmlText("punch claim")} {
				if strings.Contains(page, never) {
					t.Errorf("the notice carries %q", never)
				}
			}
		})
	}
}

// TestManualEntryRecord_AMinuteAClaimHoldsSaysTheClaimHoldsIt: when the minute's row was written by
// approving the person's punch claim, nothing is written and the notice names the claim -- it never
// says the button was pressed twice, because nobody pressed it (user decision 2026-11-10).
func TestManualEntryRecord_AMinuteAClaimHoldsSaysTheClaimHoldsIt(t *testing.T) {
	rec := &fakeRecorder{subject: manual.Subject{LocationName: "KF St Julians"}, claimRows: true}
	b := panelBrowserWithRecorder(t, rec)
	if res := b.do(http.MethodPost, manualRecordHref, confirmedEntry(t, b, "out", "2026-08-05", "17:00", "")); res.Code != http.StatusSeeOther {
		t.Fatalf("the first record answered %d, want 303", res.Code)
	}
	second := b.do(http.MethodPost, manualRecordHref, confirmedEntry(t, b, "out", "2026-08-05", "17:00", ""))
	if second.Code != http.StatusOK || len(rec.entries()) != 1 {
		t.Fatalf("the second record answered %d with %d record(s), want 200 and 1", second.Code, len(rec.entries()))
	}
	page := htmlOf(t, second)
	for _, want := range []string{
		htmlText("This minute already has a record"),
		htmlText("There's already a checkout for Maria Borg at ") + `<span class="font-mono">17:00</span>` + htmlText(" (KF St Julians). This one wasn't saved."),
		htmlText("It was saved when Maria Borg's punch claim was approved."),
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the notice does not carry %q", want)
		}
	}
	for _, never := range []string{htmlText("That record was already saved."), htmlText("Pressing the button again"),
		htmlText("To correct the venue"), htmlText("The note you typed")} {
		if strings.Contains(page, never) {
			t.Errorf("the notice carries %q", never)
		}
	}
}
