package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/web/templates/pages"
	"github.com/go-chi/chi/v5"
)

// The BILLING WRITE path's tests — M6-12 phase B, obligation 4.
//
// WHAT IS BEING DEFENDED, in one sentence: freezing a month is the only act in this
// product that can be neither corrected nor repeated. migration 0016 revokes UPDATE
// and DELETE on billing_periods from tappa_app, binds the table owner with a trigger,
// AND refuses a second row per month — so unlike `transactions`, which §4.3 gives a
// correction shape for, a wrong figure here is permanent.

var billingTestMonth = billing.Month{Year: 2026, Month: time.July}

// billingCloseFlow walks the two steps the way a browser does and returns the token
// the warning screen minted.
func billingCloseFlow(t *testing.T, b *browser, month billing.Month) string {
	t.Helper()
	rec := b.do(http.MethodPost, billingCloseHref, billingForm(month.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: %d, want 200 (the warning screen). Location: %q",
			billingCloseHref, rec.Code, rec.Header().Get("Location"))
	}
	// THE EXISTING HELPER IS REUSED RATHER THAN RE-WRITTEN (locations_test.go's): it
	// already un-escapes the rendered value, which a fresh copy would forget, and this
	// repository's standing lesson is that a half-copied helper is where the difference
	// hides.
	token := confirmTokenFrom(t, htmlOf(t, rec))
	if token == "" {
		t.Fatal("the warning screen rendered no confirmation token")
	}
	return token
}

// TestBillingClose_TheHappyPathTakesTwoStepsAndFreezesOnce is the shape, end to end.
func TestBillingClose_TheHappyPathTakesTwoStepsAndFreezesOnce(t *testing.T) {
	books := newFakeBooks()
	b := billingBrowser(t, books, billingTestOwnerRole)

	// STEP 1 WRITES NOTHING. It renders the warning and mints the confirmation.
	token := billingCloseFlow(t, b, billingTestMonth)
	if n := books.closeCount(); n != 0 {
		t.Fatalf("the warning step froze %d period(s); it must write nothing", n)
	}

	// STEP 2 FREEZES, ONCE, AND LANDS ON THE MONTH ITSELF.
	rec := b.do(http.MethodPost, billingFreezeHref, billingForm(billingTestMonth.String(), token))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s: %d, want 303", billingFreezeHref, rec.Code)
	}
	if got, want := rec.Header().Get("Location"), billingMonthHref(billingTestMonth); got != want {
		t.Errorf("after freezing it redirects to %q, want %q — M6-04's lesson: where it goes "+
			"afterwards is the thing itself, not a sentence about it", got, want)
	}
	if n := books.closeCount(); n != 1 {
		t.Fatalf("the write step froze %d period(s), want exactly 1", n)
	}
	if got := books.closedMonths[0]; got != billingTestMonth.String() {
		t.Errorf("it froze %q, want %q", got, billingTestMonth.String())
	}
}

// TestBillingClose_WritesNothingWithoutTheConfirmationStep is the gate itself.
//
// 🔴 EVERY ARM ASSERTS ON THE WRITE COUNT AND NOT ONLY ON THE STATUS. A route that
// redirected with a problem word AND froze the month would satisfy a status assertion
// perfectly, and this is the one act where that mistake is permanent.
func TestBillingClose_WritesNothingWithoutTheConfirmationStep(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, b *browser, books *fakeBooks) (month, token string)
		want    string
	}{
		{
			name: "no token at all",
			prepare: func(*testing.T, *browser, *fakeBooks) (string, string) {
				return billingTestMonth.String(), ""
			},
			want: "confirm-required",
		},
		{
			name: "a token this server did not mint",
			prepare: func(*testing.T, *browser, *fakeBooks) (string, string) {
				return billingTestMonth.String(), "bm90LWEtdG9rZW4.bm90LWEtc2ln"
			},
			want: "confirm-required",
		},
		{
			// THE SECOND TAB. A genuine, unexpired, this-session token minted for ANOTHER
			// month. The signature verifies and the BINDING does not, which is why it gets
			// its own word rather than "not confirmed".
			name: "a genuine token minted for a different month",
			prepare: func(t *testing.T, b *browser, _ *fakeBooks) (string, string) {
				other := billing.Month{Year: 2026, Month: time.June}
				return billingTestMonth.String(), billingCloseFlow(t, b, other)
			},
			want: "confirm-stale",
		},
		{
			name: "no month",
			prepare: func(t *testing.T, b *browser, _ *fakeBooks) (string, string) {
				return "", billingCloseFlow(t, b, billingTestMonth)
			},
			want: "unreadable",
		},
		{
			name: "a month that is not a year-month",
			prepare: func(t *testing.T, b *browser, _ *fakeBooks) (string, string) {
				return "2026-07-13", billingCloseFlow(t, b, billingTestMonth)
			},
			want: "unreadable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			books := newFakeBooks()
			b := billingBrowser(t, books, billingTestOwnerRole)
			month, token := tc.prepare(t, b, books)
			before := books.closeCount()

			rec := b.do(http.MethodPost, billingFreezeHref, billingForm(month, token))
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s: %d, want 303", billingFreezeHref, rec.Code)
			}
			if got, want := rec.Header().Get("Location"), billingReturn(tc.want); got != want {
				t.Errorf("Location %q, want %q", got, want)
			}
			if n := books.closeCount() - before; n != 0 {
				t.Errorf("%d period(s) were frozen despite the refusal.\n"+
					"billing_periods takes no UPDATE and no DELETE: a row written here is permanent.", n)
			}
		})
	}
}

// TestBillingClose_OnlyAnOwnerMayFreezeAMonth is S1's answer, asserted at BOTH steps.
//
// 🔴 THE SECOND ASSERTION IS THE LOAD-BEARING ONE. A gate on the warning step alone
// would be a courtesy — the write route is what creates the row, and it must refuse on
// its own without depending on which page the request came from. A manager who obtains
// a token some other way must still be refused where it counts.
func TestBillingClose_OnlyAnOwnerMayFreezeAMonth(t *testing.T) {
	// The owner's own flow, so the manager case is not passing for the wrong reason.
	ownerBooks := newFakeBooks()
	ownerBrowser := billingBrowser(t, ownerBooks, "owner")
	token := billingCloseFlow(t, ownerBrowser, billingTestMonth)

	for _, step := range []struct {
		name string
		href string
		form func() string
	}{
		{"the warning step", billingCloseHref, func() string { return "" }},
		{"the write step", billingFreezeHref, func() string { return token }},
	} {
		t.Run(step.name, func(t *testing.T) {
			books := newFakeBooks()
			b := billingBrowser(t, books, "manager")
			rec := b.do(http.MethodPost, step.href, billingForm(billingTestMonth.String(), step.form()))
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s as a manager: %d, want 303", step.href, rec.Code)
			}
			if got, want := rec.Header().Get("Location"), billingReturn("not-permitted"); got != want {
				t.Errorf("Location %q, want %q", got, want)
			}
			if n := books.closeCount(); n != 0 {
				t.Errorf("a manager froze %d period(s)", n)
			}
		})
	}

	// AND THE WHOLE SECTION IS CLOSED TO A MANAGER (O1), not merely the control — the
	// courtesy half, which after round 3 is a whole-section refusal rather than a hidden
	// button. Both halves are required and neither substitutes for the other
	// (locationactions.go's mayRemove makes the same argument for the role check that
	// already shipped).
	books := newFakeBooks()
	rec := billingBrowser(t, books, "manager").do(http.MethodGet, billingHref, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET %s as a manager: %d, want 403", billingHref, rec.Code)
	}
	html := htmlOf(t, rec)
	if strings.Contains(html, `action="`+billingCloseHref+`"`) {
		t.Error("the section offers a manager a form posting to the close route")
	}
	if !strings.Contains(html, htmlText("Billing is the owner's")) {
		t.Error("the section is simply closed to a manager with no sentence saying why.\n" +
			"§4.6: a refusal that says nothing is indistinguishable from a fault.")
	}
}

// TestBilling_TheSectionAndItsExportAreOwnerOnly is O1, asserted as the pair it is: a
// manager is refused at BOTH addresses and an owner is served at both.
//
// 🔴 THE POSITIVE CONTROL IS THE HALF THAT MATTERS. Without it a router that refused
// everybody — or that had lost the section altogether — would satisfy the refusals
// perfectly.
func TestBilling_TheSectionAndItsExportAreOwnerOnly(t *testing.T) {
	for _, path := range []string{billingHref, billingCSVHref} {
		t.Run(path, func(t *testing.T) {
			// NEGATIVE: a manager gets 403 and a sentence, and no figure at all.
			mgrBooks := newFakeBooks()
			mgrTrail := &fakeTrail{}
			rec := billingBrowserWithTrail(t, mgrBooks, mgrTrail, "manager").do(http.MethodGet, path, nil)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("GET %s as a manager: %d, want 403", path, rec.Code)
			}
			// 🔴 A REFUSED *READ* WRITES NO TRAIL ROW, which is the deliberate asymmetry
			// with the refused WRITE (refuseBillingClose does write one). A GET is
			// reachable by reload, by a crawler and by a prefetch; giving any signed-in
			// manager a way to append audit rows by refreshing a URL would be a bloat
			// surface the POST gate does not have, since that one is reached only after
			// the Origin check. The refusal is still loud — in the process log (§7).
			if n := mgrTrail.total(); n != 0 {
				t.Errorf("a refused GET %s wrote %d audit row(s), want 0", path, n)
			}
			body := htmlOf(t, rec)
			if !strings.Contains(body, htmlText("Billing is the owner's")) {
				t.Error("the refusal does not say why (§4.6)")
			}
			for _, leak := range []string{"People counted", "Frozen months", "€"} {
				if strings.Contains(body, leak) {
					t.Errorf("the refusal leaks %q from the section it refused", leak)
				}
			}
			// THE GATE RUNS BEFORE THE FIRST READ, so a refused request costs no query.
			if p, q, h := mgrBooks.readCounts(); p+q+h != 0 {
				t.Errorf("a refused read cost %d register call(s), want 0", p+q+h)
			}

			// POSITIVE: an owner is served.
			ownerBooks := newFakeBooks()
			rec = billingBrowser(t, ownerBooks, billingTestOwnerRole).do(http.MethodGet, path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s as an owner: %d, want 200 — the refusals above would "+
					"otherwise pass on a section that answers nobody", path, rec.Code)
			}
		})
	}
}

// TestBillingTab_IsHiddenFromAManagerAndTheRoutingTableIsUntouched is O1's navigation
// half, and the SECOND assertion is the one that matters.
//
// 🔴 THE PROPERTY THIS TABLE EXISTS FOR MUST SURVIVE. internal/handler mounts by
// RANGING over pages.PanelSections, so "a tab whose link 404s" is structurally
// unavailable; a role filter applied to the ROUTING would have destroyed exactly that.
// The filter is applied to the NAVIGATION only, so every section still has a route and
// the manager's refusal comes from the handler rather than from a missing mount.
//
// ⚠️ WHY HIDING RATHER THAN SHOWING-AND-REFUSING. A tab a manager can see and cannot
// open is "clickable but forbidden": it teaches somebody a control exists, costs a page
// load to discover it does not apply to them, and does it on every visit. The refusal
// page still exists for a shared link or a stale bookmark, which is where §4.6's
// obligation to say WHY is discharged.
func TestBillingTab_IsHiddenFromAManagerAndTheRoutingTableIsUntouched(t *testing.T) {
	// 1. THE OWNER SEES THE TAB (positive control first — without it, "the manager does
	//    not see it" would pass on a panel that had lost the tab altogether).
	ownerHTML := htmlOf(t, billingBrowser(t, newFakeBooks(), billingTestOwnerRole).
		do(http.MethodGet, "/admin", nil))
	if !strings.Contains(ownerHTML, `href="`+billingHref+`"`) {
		t.Fatal("an owner's navigation carries no link to the billing section")
	}

	// 2. THE MANAGER DOES NOT.
	mgrHTML := htmlOf(t, billingBrowser(t, newFakeBooks(), "manager").
		do(http.MethodGet, "/admin", nil))
	if strings.Contains(mgrHTML, `href="`+billingHref+`"`) {
		t.Error("a manager's navigation offers a link to a section the server will refuse")
	}
	// AND ONLY THAT ONE IS MISSING — a filter that dropped more would be a different bug
	// wearing this one's clothes.
	for _, s := range pages.PanelSections {
		// ⚠️ OperatorOnly JOINED OwnerOnly HERE IN M7-06 AND IT IS A SECOND CONDITION,
		// NOT A LOOSENING. This browser's identity is not on the operator allow-list
		// either, so the legal-texts tab is correctly absent for a reason that has
		// nothing to do with the role filter this test is about. Skipping it keeps this
		// assertion aimed at what it is named for; the operator filter has its own test
		// in legaladmin_test.go, which drives BOTH arms.
		if s.OwnerOnly || s.OperatorOnly {
			continue
		}
		if !strings.Contains(mgrHTML, `href="`+s.Href+`"`) {
			t.Errorf("a manager's navigation lost the %q tab, which is not owner-only", s.Label)
		}
	}

	// 3. THE ROUTE STILL EXISTS. The manager gets a 403 from the HANDLER, never a 404
	//    from chi — which is what proves the routing table was derived from the full
	//    section list and not from the filtered one.
	rec := billingBrowser(t, newFakeBooks(), "manager").do(http.MethodGet, billingHref, nil)
	if rec.Code == http.StatusNotFound {
		t.Error("the billing route is missing for a manager.\n" +
			"The role filter belongs to the NAVIGATION; filtering the mount would break " +
			"the one property pages.PanelSections exists to guarantee.")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET %s as a manager: %d, want 403", billingHref, rec.Code)
	}
}

// TestBillingExport_LeavesAnAccessRow is O2.
//
// 🔴 THE SIBLING ROUTE WRITES ONE AND THIS ONE DID NOT — measured by a security audit
// as `after_billing_csv=743, after_reports_csv=744`. The deciding argument is
// CONSISTENCY rather than confidentiality: two sibling CSV routes in one repository
// answering "does a bulk export leave a trail?" differently is how the precedent gets
// bent by the section after next.
func TestBillingExport_LeavesAnAccessRow(t *testing.T) {
	books := newFakeBooks()
	trail := &fakeTrail{}
	b := billingBrowserWithTrail(t, books, trail, billingTestOwnerRole)

	// THE SECTION ITSELF WRITES NOTHING — only the export does, which is what makes
	// this a bounded exception rather than "the read path writes now".
	if rec := b.do(http.MethodGet, billingHref, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET the section: %d", rec.Code)
	}
	if n := trail.total(); n != 0 {
		t.Errorf("rendering the section wrote %d audit row(s), want 0", n)
	}

	if rec := b.do(http.MethodGet, billingCSVHref, nil); rec.Code != http.StatusOK {
		t.Fatalf("GET the export: %d", rec.Code)
	}
	if n := trail.count(ActionBillingExported); n != 1 {
		t.Fatalf("the export wrote %d %q row(s), want exactly 1", n, ActionBillingExported)
	}
	ev := trail.eventsSnapshot()[0]
	if ev.ActorID == nil || *ev.ActorID != panelTestAdmin {
		t.Error("the access row does not name who downloaded it — which is the whole point")
	}
	d, ok := ev.Detail.(billingExportRow)
	if !ok {
		t.Fatalf("the detail is %T, not the purpose-built struct", ev.Detail)
	}
	// §4.7 / §6: COUNTS AND FLAGS ONLY, and the amount as an exact decimal STRING — a
	// JSON number is a float64 in every reader that will ever parse it.
	if d.AmountDue != books.draft.AmountDue.String() || d.Currency != "EUR" {
		t.Errorf("the row does not carry the amount exactly: %+v", d)
	}
	if d.Bytes <= 0 || d.Month != billingTestMonth.String() {
		t.Errorf("the row does not describe the document it recorded: %+v", d)
	}
}

// TestBillingClose_RefusesAMonthThatCannotBeFrozen covers the three conditions the SQL
// enforces itself, from the screen's side: the warning must not be served for a month
// the statement would refuse, because a warning for an act that cannot happen is a
// warning about nothing.
func TestBillingClose_RefusesAMonthThatCannotBeFrozen(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(b *fakeBooks)
		want   string
	}{
		{"a month that has not ended", func(b *fakeBooks) { b.draft.HasEnded = false }, "not-ended"},
		{"a month before signup", func(b *fakeBooks) { b.draft.AfterSignup = false }, "before-signup"},
		{"a month already frozen", func(b *fakeBooks) {
			b.frozen[billingTestMonth.String()] = frozenDraftFor(billingTestMonth, 24, 3600)
		}, "already-closed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			books := newFakeBooks()
			tc.mutate(books)
			b := billingBrowser(t, books, billingTestOwnerRole)
			rec := b.do(http.MethodPost, billingCloseHref, billingForm(billingTestMonth.String(), ""))
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s: %d, want 303 (a refusal, not a warning screen)", billingCloseHref, rec.Code)
			}
			if got, want := rec.Header().Get("Location"), billingReturn(tc.want); got != want {
				t.Errorf("Location %q, want %q", got, want)
			}
			if n := books.closeCount(); n != 0 {
				t.Errorf("%d period(s) were frozen", n)
			}
			// AND THE SECTION TURNS THE WORD BACK INTO A SENTENCE, so the manager is not
			// left reading a query string.
			html := htmlOf(t, b.do(http.MethodGet, rec.Header().Get("Location"), nil))
			if !strings.Contains(html, htmlText(billingProblemSentence(tc.want))) {
				t.Errorf("the section does not explain %q in words", tc.want)
			}
		})
	}
}

// TestBillingClose_ASecondFreezeOfTheSameMonthChangesNothing is the replay case.
//
// 🔴 THE ONE-SHOT PROPERTY IS THE DATABASE'S, NOT THE COOKIE'S. deactivateconfirm.go
// counts what a client that re-prints the cookie can still do: it can spend one mint
// repeatedly until it expires. What makes that harmless HERE is
// UNIQUE (tenant_id, period_month) — the second close raises 23505 and the domain
// reports ErrAlreadyClosed, so no second row, no second figure and no second audit row
// exists. This asserts the outcome rather than the mechanism.
func TestBillingClose_ASecondFreezeOfTheSameMonthChangesNothing(t *testing.T) {
	books := newFakeBooks()
	b := billingBrowser(t, books, billingTestOwnerRole)
	token := billingCloseFlow(t, b, billingTestMonth)

	if rec := b.do(http.MethodPost, billingFreezeHref, billingForm(billingTestMonth.String(), token)); rec.Code != http.StatusSeeOther {
		t.Fatalf("first freeze: %d, want 303", rec.Code)
	}
	frozenAfterFirst := books.frozen[billingTestMonth.String()]

	// RE-PRINT THE COOKIE, exactly as the audit's probe did.
	b.cookies[adminConfirmCookieName] = token
	rec := b.do(http.MethodPost, billingFreezeHref, billingForm(billingTestMonth.String(), token))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("second freeze: %d, want 303", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), billingReturn("already-closed"); got != want {
		t.Errorf("the replay redirected to %q, want %q", got, want)
	}
	if got := books.frozen[billingTestMonth.String()]; got.ID != frozenAfterFirst.ID {
		t.Error("the replay replaced the stored row. A month has exactly one answer.")
	}
	if len(books.history) != 1 {
		t.Errorf("the history holds %d rows after a replayed freeze, want 1", len(books.history))
	}
}

// TestBillingClose_WritesNoAuditRowFromTheHandler is obligation 5.
//
// 🔴 THE TRAIL FOR THIS ACT IS WRITTEN BY internal/domain/billing.Close, INSIDE the
// transaction that inserts the frozen row, so the two share a fate. A second row
// written from the handler would be a second representation of ONE event on a table
// that takes no UPDATE and no DELETE — i.e. permanently, and uncorrectably.
//
// The double for the audit sink counts every Record call, so this is a count rather
// than an inspection.
func TestBillingClose_WritesNoAuditRowFromTheHandler(t *testing.T) {
	books := newFakeBooks()
	trail := &fakeTrail{}
	b := billingBrowserWithTrail(t, books, trail, billingTestOwnerRole)

	token := billingCloseFlow(t, b, billingTestMonth)
	if rec := b.do(http.MethodPost, billingFreezeHref, billingForm(billingTestMonth.String(), token)); rec.Code != http.StatusSeeOther {
		t.Fatalf("freeze: %d, want 303", rec.Code)
	}
	if n := books.closeCount(); n != 1 {
		t.Fatalf("the month was not frozen (%d closes), so this test would assert over nothing", n)
	}
	if n := trail.total(); n != 0 {
		t.Errorf("the handler wrote %d audit row(s) for a close.\n"+
			"billing.Close already writes `billing.period_closed` in the same transaction as "+
			"the frozen row; a second row is a duplicate that can never be removed.", n)
	}
}

// TestBillingClose_ARefusedAttemptIsRecordedAtBothSteps is K1.
//
// 🔴 THE ROW EXISTS FOR A READER THE PROCESS LOG DOES NOT HAVE. A structured log line
// has no tenant boundary, no retention story and no reader outside the operator; the
// person who most needs to know that a manager has been trying to freeze months is the
// business's OWNER. That is a user decision (2026-08-09) taken for the removal gate,
// and three shipped actions already follow it.
//
// 🔴 BOTH STEPS, AND THE COUNT IS THE ASSERTION. A manager refused at the warning step
// never receives a token, so a POST to the write route afterwards is a second, separate
// deliberate act — and it is the one that would have created the permanent row.
// Recording only the first would hide the more serious half.
func TestBillingClose_ARefusedAttemptIsRecordedAtBothSteps(t *testing.T) {
	steps := []struct {
		name string
		href string
		want string
	}{
		{"the warning step", billingCloseHref, billingStepWarning},
		{"the write step", billingFreezeHref, billingStepWrite},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			books := newFakeBooks()
			trail := &fakeTrail{}
			b := billingBrowserWithTrail(t, books, trail, "manager")

			rec := b.do(http.MethodPost, step.href, billingForm(billingTestMonth.String(), ""))
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("POST %s as a manager: %d, want 303", step.href, rec.Code)
			}
			if n := trail.count(ActionPeriodCloseRefused); n != 1 {
				t.Fatalf("a refused close at %s wrote %d %q row(s), want exactly 1",
					step.href, n, ActionPeriodCloseRefused)
			}
			// THE PAYLOAD IS READ, not just counted: a row that named the wrong month, the
			// wrong step or no role at all would satisfy a count and tell an owner nothing.
			ev := trail.eventsSnapshot()[0]
			if ev.Target != billingTestMonth.String() {
				t.Errorf("the row's target is %q, want the month %q", ev.Target, billingTestMonth)
			}
			if ev.ActorID == nil || *ev.ActorID != panelTestAdmin {
				t.Errorf("the row does not name the actor who was refused")
			}
			d, ok := ev.Detail.(refusedCloseDetail)
			if !ok {
				t.Fatalf("the detail is %T, not the purpose-built struct", ev.Detail)
			}
			if d.Step != step.want {
				t.Errorf("the row records step %q, want %q — without it the two rows an "+
					"attempt at both steps produces are indistinguishable", d.Step, step.want)
			}
			if d.Role != "manager" || d.RequiredRole != adminRoleOwner || d.Month != billingTestMonth.String() {
				t.Errorf("the row does not carry the substance of the refusal: %+v", d)
			}
			if d.Outcome != "refused" {
				t.Errorf("outcome is %q, want %q", d.Outcome, "refused")
			}
			if n := books.closeCount(); n != 0 {
				t.Errorf("%d period(s) were frozen despite the refusal", n)
			}
		})
	}
}

// TestBillingClose_AnOwnerLeavesNoRefusalRow is the negative half, and without it the
// test above would pass on a handler that recorded a refusal for EVERYBODY.
//
// It also pins the split obligation 5 turns on: the SUCCESS row is
// internal/domain/billing.Close's and is written in ITS transaction, so a successful
// freeze driven through this handler leaves no row from the handler at all — neither a
// duplicate success nor a spurious refusal.
func TestBillingClose_AnOwnerLeavesNoRefusalRow(t *testing.T) {
	books := newFakeBooks()
	trail := &fakeTrail{}
	b := billingBrowserWithTrail(t, books, trail, billingTestOwnerRole)

	token := billingCloseFlow(t, b, billingTestMonth)
	if rec := b.do(http.MethodPost, billingFreezeHref, billingForm(billingTestMonth.String(), token)); rec.Code != http.StatusSeeOther {
		t.Fatalf("freeze: %d, want 303", rec.Code)
	}
	if n := books.closeCount(); n != 1 {
		t.Fatalf("the month was not frozen (%d closes); this test would assert over nothing", n)
	}
	if n := trail.count(ActionPeriodCloseRefused); n != 0 {
		t.Errorf("an OWNER's successful close wrote %d refusal row(s), want 0", n)
	}
	if n := trail.total(); n != 0 {
		t.Errorf("the handler wrote %d audit row(s) for a successful close, want 0 — "+
			"billing.Close writes the one row, in the transaction that froze the period", n)
	}
}

// TestBillingCSV_UsesTheSameEscapingBoundaryAsTheHoursExport is the anti-half-copy
// measurement.
//
// 🔴 THE ESCAPING IS NOT REWRITTEN HERE AND THIS PROVES IT ON VALUES, NOT ON IMPORTS.
// Three classes, one from each of the three audits that widened reportscsv.go's
// boundary: an ASCII formula trigger, a FORMAT character (Cf) hiding one, and a
// DEFAULT-IGNORABLE LETTER (U+3164 HANGUL FILLER, category Lo) hiding one. A second
// writer built by hand would almost certainly catch the first and miss the third —
// that is exactly the history reportscsv.go records.
func TestBillingCSV_UsesTheSameEscapingBoundaryAsTheHoursExport(t *testing.T) {
	// EVERY INVISIBLE RUNE IS WRITTEN AS AN ESCAPE, not as itself. staticcheck's
	// ST1018 asked for it and it is right for a reason beyond the lint: a literal
	// zero-width space in source is a character no reviewer can see, which is the
	// same property that makes it a hiding place for a formula in the first place.
	cases := []struct {
		name string
		in   string
	}{
		{"a bare formula", "=1+1"},
		{"a formula behind a zero-width space", "\u200b=1+1"}, // Cf
		{"a formula behind a Hangul filler", "\u3164=1+1"},    // Lo, Default_Ignorable
		{"a formula behind a soft hyphen", "\u00ad=1+1"},      // Cf
		{"a formula behind a combining acute", "\u0301=1+1"},  // Mn
		{"a formula behind a braille blank", "\u2800=1+1"},    // So, the named exception
		{"a full-width equals", "＝1+1"},                       // a compatibility trigger
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			books := newFakeBooks()
			// The tenant NAME is the one free-text value a customer controls that reaches
			// this document, which is why it carries the payload.
			books.draft.TenantName = tc.in

			file := billingFile(t, books)
			rows := parseBillingCSV(t, file)
			got := billingCell(t, rows, "Business")
			if !strings.HasPrefix(got, "'") {
				t.Errorf("the cell %q was written as %q with no leading apostrophe.\n"+
					"reportscsv.go's spreadsheetSafe neutralises this class; a billing export "+
					"that re-implemented escaping would be the eighth half-copy in this repository.",
					tc.in, got)
			}
			// AND THE HOURS EXPORT AGREES, cell for cell — the two surfaces share one
			// function, so this is a check that they really do.
			if want := spreadsheetSafe(tc.in); got != want {
				t.Errorf("billing wrote %q, the shared escaper produces %q", got, want)
			}
		})
	}
}

// TestBillingCSV_CarriesTheBOMAndPutsItOnATitle inherits reportCSVBOM's decision AND
// its mitigation: the mark lands on a human-readable title, never on a column heading a
// machine keys on.
func TestBillingCSV_CarriesTheBOMAndPutsItOnATitle(t *testing.T) {
	file := billingFile(t, newFakeBooks())
	if !strings.HasPrefix(file, reportCSVBOM) {
		t.Fatal("the billing export carries no BOM.\n" +
			"reportCSVBOM's reason applies verbatim: both markets this product ships to are the " +
			"ones a missing BOM damages — Maltese ħ ġ ċ ż and Turkish ı ş ğ ç in a business name.")
	}
	rows := parseBillingCSV(t, file)
	if len(rows) == 0 {
		t.Fatal("the export has no rows")
	}
	// The first record is the title, and it is not a heading anything keys on.
	if first := rows[0][0]; first != "Tappa — monthly billing" {
		t.Errorf("the BOM lands on %q, which is not the document's title row", first)
	}
	// EVERY MACHINE-READABLE HEADING IS UNTOUCHED, which is the whole of the mitigation.
	for _, heading := range []string{"measure", "month", "what"} {
		if strings.Contains(file, reportCSVBOM+heading) {
			t.Errorf("the byte order mark sits in front of the %q heading", heading)
		}
	}
}

// TestBillingRoutes_AreRefusedCrossOriginBeforeTheResolverRuns is the structural half
// of obligation 4: both POSTs must be on ProtectWriting's chain rather than
// mountSections' READ chain.
//
// 🔴 IT COUNTS RESOLVER READS, NOT STATUS CODES, and the first draft of this test did
// the wrong thing — it asserted that a cross-origin POST is NOT answered 303, which is
// exactly what the Origin gate does answer (a.redirect to /admin). The status is the
// same on both chains; the whole difference the gate's POSITION buys is COST.
// employeeactions_test.go records the measurement: with the gate after the resolver,
// 300 cross-origin POSTs spent a signed-in manager's session budget and locked them
// out of their own panel, and a test asserting only "303 to /admin" passed on that
// code.
func TestBillingRoutes_AreRefusedCrossOriginBeforeTheResolverRuns(t *testing.T) {
	books := newFakeBooks()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: billingTestOwnerRole, FullName: "Owner Person",
		}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), books, newFakeTexts(), newFakeAccount(), adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	// POSITIVE CONTROL FIRST. Without it, a router that never reaches the resolver at
	// all would satisfy the zero below.
	if rec := b.do(http.MethodPost, billingCloseHref, billingForm(billingTestMonth.String(), "")); rec.Code != http.StatusOK {
		t.Fatalf("same-origin POST answered %d, want the warning screen", rec.Code)
	}
	baseline := admins.verifiedCount()
	if baseline == 0 {
		t.Fatal("a served POST resolved no session; the counter is not wired")
	}

	b.origin = "https://evil.example"
	for _, href := range []string{billingCloseHref, billingFreezeHref} {
		rec := b.do(http.MethodPost, href, billingForm(billingTestMonth.String(), ""))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
			t.Errorf("cross-origin POST %s: %d %q, want 303 /admin",
				href, rec.Code, rec.Header().Get("Location"))
		}
	}
	if got := admins.verifiedCount() - baseline; got != 0 {
		t.Errorf("two cross-origin POSTs cost %d resolver read(s), want 0.\n"+
			"The Origin check must run BEFORE requireAdmin; a POST registered in "+
			"mountSections would silently take the READ chain.", got)
	}
	if n := books.closeCount(); n != 0 {
		t.Errorf("a cross-origin POST froze %d period(s)", n)
	}
}

// TestBillingClose_TheWarningNamesWhatWillBeFrozenAndWhatItCannotPromise is the
// confirmation screen's own contract: it must show the figures, say the act is
// irreversible, AND say the two things a reader would otherwise assume — that the
// printed figures are not what freezes, and that the count is not an itemisation.
func TestBillingClose_TheWarningNamesWhatWillBeFrozenAndWhatItCannotPromise(t *testing.T) {
	books := newFakeBooks()
	books.draft.UnstampedEmployees = 2
	b := billingBrowser(t, books, billingTestOwnerRole)

	rec := b.do(http.MethodPost, billingCloseHref, billingForm(billingTestMonth.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: %d, want the warning screen", billingCloseHref, rec.Code)
	}
	html := htmlOf(t, rec)
	for _, want := range []string{
		"Freeze July 2026",                     // which month, in words
		"2026-07",                              // and machine-readably, as the hidden field
		"never be edited, removed or replaced", // the irreversibility
		"Freezing counts again at that moment", // the figures are not the ones that freeze
		"NUMBER of people, not which people",   // the promise it cannot keep
		"upper bound",                          // the floor admission travels here too
	} {
		if !strings.Contains(html, htmlText(want)) {
			t.Errorf("the warning screen does not carry %q", want)
		}
	}
	// AND IT POSTS TO THE WRITE ROUTE, not back to itself.
	if !strings.Contains(html, `action="`+billingFreezeHref+`"`) {
		t.Errorf("the warning's form does not post to %s", billingFreezeHref)
	}
}
