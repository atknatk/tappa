package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The ACCOUNT section's tests — M7-05.
//
// WHAT EACH BLOCK IS FOR, because this file answers the card's criteria one by one and
// a reader should be able to find the one they came for:
//
//	firma bilgileri            TestAccount_ShowsWhatIsOnFile
//	                           TestAccount_NeverPrintsTheCommercialTerms
//	VAT doğrulama durumu       TestAccount_VATStateSaysItsOwnWord
//	zaman dilimi ayarı         TestAccountSave_StoresTheZoneAndSaysWhatItMoves
//	                           TestAccountSave_RefusesAZoneThisBuildCannotLoad
//	marka mesajları görünür    TestAccount_ShowsTheSentencesThisBusinessesStaffRead
//	                           TestAccount_TheBrandPreviewIsTheTapScreensOwnComponent
//	audit_log                  TestAccountSave_RefusalIsRecorded (the success row is
//	                           the domain's, and account_db_test.go counts it)
//	the role gate              TestAccount_ManagerMayReadAndMayNotSave
//
// AND TWO THAT PIN DEFECTS THIS SCREEN ACTUALLY SHIPPED WITH FOR ONE BUILD, both found
// by measurement rather than by reading:
//
//	classes built in Go        TestAccount_TheVATMarkersClassesLiveWhereTailwindLooks
//	a field nothing may read   TestAccount_NeverShowsTheStructureAnswer

const (
	accountTestOwnerRole   = "owner"
	accountTestManagerRole = "manager"
)

// accountBrowser wires the panel with an account store the test controls.
func accountBrowser(t *testing.T, accounts panelAccounts, trail *fakeTrail, role string) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "Owner Person",
		}, nil
	}}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeTexts(), accounts, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// accountPage renders the section as an owner and fails loudly on anything but a 200.
func accountPage(t *testing.T, accounts panelAccounts) string {
	t.Helper()
	rec := accountBrowser(t, accounts, &fakeTrail{}, accountTestOwnerRole).do(http.MethodGet, accountHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", accountHref, rec.Code)
	}
	return htmlOf(t, rec)
}

// --- the business's own facts --------------------------------------------------------

// TestAccount_ShowsWhatIsOnFile is the card's "firma bilgileri" criterion, minus the
// two halves that were measured as already shipped (see below).
func TestAccount_ShowsWhatIsOnFile(t *testing.T) {
	html := accountPage(t, newFakeAccount())

	// 🔴 EACH VALUE IS READ FROM ITS OWN CELL, NOT SEARCHED FOR IN THE DOCUMENT, AND THE
	// FIRST DRAFT DID THE LATTER FOR ALL FOUR. Measured by an auditor: emptying the name
	// cell alone left this test — and the whole internal/handler package — GREEN, because
	// the form's value= attribute carries the same string on a plain load and the
	// "still X on file" notice carries it on a refused one. The docket is the block whose
	// heading says "On file"; an assertion about it must read it.
	if got := docketName(t, html); got != "Kebab Factory Ltd." {
		t.Errorf("the docket is headed %q, want the stored business name.\nThis block says "+
			"\"On file\"; the name is the one fact on it a customer checks first.", got)
	}
	for _, tc := range []struct{ label, want string }{
		{"VAT number", "MT20250001"},
		{"Timezone", "Europe/Malta"},
		{"Registered on", "1 February 2026"},
	} {
		if got := docketCell(t, html, tc.label); got != tc.want {
			t.Errorf("the %q cell reads %q, want %q.\nIt is the one screen that says what "+
				"Tappa has on file for this business; a fact that is stored and shown wrongly "+
				"is worse than one that is not shown.", tc.label, got, tc.want)
		}
	}
}

// TestAccount_NeverPrintsTheCommercialTerms is the measured HALF of the card's first
// criterion, and it is a NEGATIVE test on purpose.
//
// 🔴 THE CARD ASKED FOR "fatura verileri, plan görünümü" AND MEASUREMENT SAID THEY WERE
// ALREADY SHIPPED. /admin/billing prints the plan, the headcount, the unit price and
// the amount — behind a gate M6-12 phase B closed to OWNERS INCLUDING THE READ, because
// migration 00016 made the commercial terms the operator's rather than the customer's.
// Printing any of them on a settings page every manager can open would undo that gate,
// so this test is what stops a future session "completing" the criterion by copying
// them across.
func TestAccount_NeverPrintsTheCommercialTerms(t *testing.T) {
	// 🔴 THE CORPUS RUNS FOR BOTH ROLES, AND THE MANAGER IS THE CASE THAT MATTERS. The
	// owner may read every one of these words one click away at /admin/billing; the
	// manager may not read any of them anywhere, so a leak here is a leak only their
	// render can show. Running it for the owner too is what stops the check passing
	// because the page failed to render at all.
	for _, role := range []string{accountTestOwnerRole, accountTestManagerRole} {
		t.Run(role, func(t *testing.T) {
			rec := accountBrowser(t, newFakeAccount(), &fakeTrail{}, role).
				do(http.MethodGet, accountHref, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s as %s: %d, want 200", accountHref, role, rec.Code)
			}
			html := htmlOf(t, rec)
			// 🔴 EIGHT TERMS, AND THE COMMENT USED TO CLAIM EIGHT WHILE THE CORPUS
			// CARRIED FOUR. An auditor re-derived the missing four from source and found
			// them true TODAY — `plan`, `price` and `amount` appear in no rendered
			// template, and `Billing` only inside `if v.ShowBilling()` — but three of
			// them rested on nothing mechanical, which is this project's signature
			// defect: a measurement claim wider than the measurement. The corpus is now
			// the claim.
			//
			// ⚠️ THE LAST FOUR ARE BARE WORDS AND THAT IS DELIBERATELY BROAD. "plan" and
			// "price" would also fire on prose that merely mentions them, which is the
			// right sensitivity here: this screen has no business discussing either, so a
			// sentence that starts to is exactly what should turn this red. "Billing" is
			// checked for the MANAGER only — an owner is offered the link on purpose —
			// and that half is asserted below.
			forbidden := []string{
				"founding customer", // planLabel's output
				"standard plan",     // the other one
				"per person",        // the unit price's phrasing
				"€",                 // any amount at all
				"plan",              // the commercial term itself, in any sentence
				"price",             //
				"amount",            //
			}
			if role == accountTestManagerRole {
				// The owner is deliberately pointed at the money screen; the manager
				// must not be, because the section refuses them.
				forbidden = append(forbidden, "Billing")
			}
			for _, forbidden := range forbidden {
				if strings.Contains(html, htmlText(forbidden)) {
					t.Errorf("the account screen prints %q.\nThe plan and the price are the "+
						"operator's (migration 00016) and M6-12 phase B closed even READING them "+
						"to owners at /admin/billing. This section is readable by a manager, so a "+
						"commercial term here is that gate undone.", forbidden)
				}
			}
			// AND THE REGISTRATION DATE IS THERE WITHOUT ONE. It is an identity fact —
			// "since when has Tappa had this business on file" — and the round-2 decision
			// was to keep it while moving its justification off the invoice. If it ever
			// acquires a commercial sentence, the corpus above catches that; this half
			// catches somebody removing the field instead of the framing.
			if !strings.Contains(html, htmlText("Registered on")) {
				t.Errorf("the %s render has no registration date. It is the one date on the "+
					"screen nobody can move, and the block it sits in is a record because of it.",
					role)
			}
		})
	}
	// THE OWNER GETS THE POINTER TO THE MONEY AND THE MANAGER DOES NOT — the same gate,
	// from the other side. A manager sent to a page they will be refused is what
	// PanelSection.OwnerOnly's comment calls "clickable but forbidden".
	ownerHTML := htmlOf(t, accountBrowser(t, newFakeAccount(), &fakeTrail{}, accountTestOwnerRole).
		do(http.MethodGet, accountHref, nil))
	if !strings.Contains(ownerHTML, htmlText("Open Billing")) {
		t.Error("an owner is not offered the billing screen, so the criterion the card asked " +
			"for is met by nothing at all")
	}
	managerHTML := htmlOf(t, accountBrowser(t, newFakeAccount(), &fakeTrail{}, accountTestManagerRole).
		do(http.MethodGet, accountHref, nil))
	if strings.Contains(managerHTML, htmlText("Open Billing")) {
		t.Error("a manager is pointed at /admin/billing, which refuses them")
	}
}

// --- the VAT register ----------------------------------------------------------------

// TestAccount_VATStateSaysItsOwnWord is the card's 2026-08-13 addition: the columns
// migration 00017 wrote and NOTHING read.
//
// 🔴 FOUR STATES AND FOUR SENTENCES. A screen that collapsed them into a tick or a
// cross would write an EU outage down as an accusation — the exact thing
// internal/domain/signup/vies.go exists to prevent — so each state is asserted for its
// OWN word and for a sentence the other three do not carry.
func TestAccount_VATStateSaysItsOwnWord(t *testing.T) {
	yes, no := true, false
	checked := fakeVATChecked

	tests := []struct {
		name     string
		verified *bool
		at       *time.Time
		word     string
		phrase   string
		notWords []string
	}{
		{
			// ⚠️ THE PHRASE CHANGED IN ROUND 4 AND THE OLD ONE WAS "Nothing has asked".
			// C6 measured that claim FALSE for most businesses in this state:
			// signup.checkedAt stores NULL in both columns for VATUnknown, so a customer
			// whose registration could not reach the register lands here and was told
			// nobody had asked. The sentence now names what the record cannot tell apart,
			// and this phrase is that admission — the load-bearing half rather than the
			// decorative one.
			name: "never checked", verified: nil, at: nil,
			word: "Not checked", phrase: "cannot tell them apart",
			notWords: []string{"Confirmed", "Not found", "No answer"},
		},
		{
			name: "asked, no answer", verified: nil, at: &checked,
			word: "No answer", phrase: "did not answer",
			notWords: []string{"Confirmed", "Not found", "Not checked"},
		},
		{
			name: "the register says yes", verified: &yes, at: &checked,
			word: "Confirmed", phrase: "confirmed this number",
			notWords: []string{"Not found", "No answer", "Not checked"},
		},
		{
			name: "the register says no", verified: &no, at: &checked,
			word: "Not found", phrase: "did not recognise this number",
			notWords: []string{"Confirmed", "No answer", "Not checked"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			acc := newFakeAccount()
			acc.setVAT(tc.verified, tc.at)
			html := accountPage(t, acc)
			if !strings.Contains(html, htmlText(tc.word)) {
				t.Errorf("the VAT marker does not say %q.\nThe WORD is what carries the state; "+
					"a reader with no colour has nothing else (skill tappa-brand).", tc.word)
			}
			if !strings.Contains(html, htmlText(tc.phrase)) {
				t.Errorf("the VAT block does not carry the sentence for this state (%q).", tc.phrase)
			}
			for _, other := range tc.notWords {
				if strings.Contains(html, htmlText(other)) {
					t.Errorf("the VAT marker also says %q, so two states are on one screen.", other)
				}
			}
			// THE STAMP IS ONLY DRAWN WHEN SOMETHING WAS ACTUALLY ASKED. "Asked <date>"
			// under "Not checked" would be a date for an event that never happened.
			asked := strings.Contains(html, htmlText("Asked "))
			if want := tc.at != nil; asked != want {
				t.Errorf("the screen shows an asked-on date = %v, want %v", asked, want)
			}
		})
	}
}

// TestAccount_OffersNoVATRecheck is the measured DROP of the card's third ask, pinned
// so a future session finds the reasoning instead of the gap.
//
// 🔴 THE CARD ASKED FOR THE `UPDATE (vat_verified, vat_checked_at)` GRANT "for a
// re-check". It is not shipped, and
// TestAccountDB_TheAppRoleHoldsNoUpdateOnTheVATColumnsOrTheTerms (internal/domain/tenant)
// measures that the privilege is still absent. The reason is on the screen rather than
// in a comment: the VAT NUMBER cannot be changed here, so a re-check could only ever
// ask the register the same question again — and a customer-triggered outbound call to
// a service nobody here operates is a different threat model from the one
// internal/handler/signupratelimit.go budgets (an unauthenticated visitor, per address,
// at registration). What the customer gets instead is the answer and a way to reach us.
func TestAccount_OffersNoVATRecheck(t *testing.T) {
	html := accountPage(t, newFakeAccount())
	for _, forbidden := range []string{"Check again", "Re-check", "Recheck", "Verify now"} {
		if strings.Contains(html, htmlText(forbidden)) {
			t.Errorf("the account screen offers %q.\nNothing in the product can write "+
				"vat_verified after registration — migration 00017 granted INSERT and not "+
				"UPDATE — so a button saying so would be a control that cannot work.", forbidden)
		}
	}
	if !strings.Contains(html, htmlText("tell us")) {
		t.Error("the screen never tells a customer what to do about a VAT number that is wrong.\n" +
			"§4.6: a state the product will not act on must at least say who can.")
	}
}

// --- the brand messages ---------------------------------------------------------------

// TestAccount_ShowsTheSentencesThisBusinessesStaffRead is the card's third criterion,
// read as it can honestly be met today: the sentences are VISIBLE and the screen says
// they are not editable (M9-04).
func TestAccount_ShowsTheSentencesThisBusinessesStaffRead(t *testing.T) {
	tests := []struct {
		businessType string
		want         []string
		wantNot      []string
	}{
		{
			businessType: "restaurant",
			want: []string{
				"Have a great shift — keep those kebabs rolling!",
				"Great work today. See you next shift!",
			},
			wantNot: []string{"stay safe on the floor", "Thank you for your work today"},
		},
		{
			businessType: "production",
			want: []string{
				"Have a productive shift — stay safe on the floor!",
				"Shift complete. Thank you for your work today!",
			},
			wantNot: []string{"keep those kebabs rolling", "See you next shift"},
		},
		{
			// THE NEUTRAL FALL-THROUGH IS A CASE, NOT AN ACCIDENT. Six of the eight
			// business types land on it, so it is what MOST customers see.
			businessType: "hotel",
			want: []string{
				"Have a great shift!",
				"Thanks for today. See you next time!",
			},
			wantNot: []string{"keep those kebabs rolling", "stay safe on the floor"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.businessType, func(t *testing.T) {
			acc := newFakeAccount()
			acc.settings.BusinessType = tc.businessType
			html := accountPage(t, acc)
			for _, want := range tc.want {
				if !strings.Contains(html, htmlText(want)) {
					t.Errorf("a %s does not see %q on its own settings screen.\n"+
						"The criterion is that the brand messages are VISIBLE; this is the "+
						"only screen where a customer can find out what their staff read.",
						tc.businessType, want)
				}
			}
			for _, no := range tc.wantNot {
				if strings.Contains(html, htmlText(no)) {
					t.Errorf("a %s is shown %q, which belongs to another trade.", tc.businessType, no)
				}
			}
		})
	}
}

// TestAccount_SaysTheMessagesCannotBeEdited keeps the screen honest about the half it
// does NOT deliver. Writing per-business copy is M9-04 and needs a column that does not
// exist; a preview with no such sentence would read as a control somebody had forgotten
// to draw.
func TestAccount_SaysTheMessagesCannotBeEdited(t *testing.T) {
	html := accountPage(t, newFakeAccount())
	if !strings.Contains(html, htmlText("writing your own is not")) {
		t.Error("the screen shows the staff sentences and never says they cannot be changed.\n" +
			"§4.6 applied to a screen: a preview with no editor beside it and no sentence " +
			"explaining why is indistinguishable from a missing feature.")
	}
	if !strings.Contains(html, htmlText("every restaurant on Tappa reads the same one")) {
		t.Error("the screen does not state the LIMIT — that the sentences follow the kind of " +
			"business rather than the business. M5-05 recorded it as a known limit; a customer " +
			"who believes these are theirs alone has been misled.")
	}
}

// TestAccount_TheBrandPreviewIsTheTapScreensOwnComponent is the anti-drift property,
// and it is asserted by CONSTRUCTION rather than by comparing two strings.
//
// 🔴 A COPY OF THE FOUR SENTENCES ON THE SETTINGS PAGE WOULD BE A SECOND
// REPRESENTATION, and the worst kind: it would go stale silently and the screen would
// then tell a customer their staff read something they do not. account.templ calls
// @brandMessage — result.templ's own component — so the preview cannot diverge. This
// test renders BOTH surfaces from the same business type and requires the tap screen's
// sentence to appear on the settings screen verbatim.
func TestAccount_TheBrandPreviewIsTheTapScreensOwnComponent(t *testing.T) {
	for _, businessType := range []string{"restaurant", "production", "cafe", "hotel", "kiosk"} {
		t.Run(businessType, func(t *testing.T) {
			acc := newFakeAccount()
			acc.settings.BusinessType = businessType
			settings := accountPage(t, acc)

			for _, direction := range []string{"in", "out"} {
				// 🔴 THE CONTROL IS THE REAL TAP ENDPOINT, NOT A RE-RENDER OF THE SAME
				// VIEW. resultBody drives POST /t through the checkin handler, which is a
				// different surface, a different handler and a different view type from
				// the one under test — so the sentence it produces is not derived from the
				// mechanism this test is checking.
				tapScreen, code := resultBody(t, checkin.Result{
					Outcome:      checkin.OutcomeRecorded,
					Decision:     decisionOf("ok", direction, false),
					LocationName: "St Julians",
					BusinessType: businessType,
				}, nil)
				if code != http.StatusOK {
					t.Fatalf("the tap screen answered %d", code)
				}
				sentence := brandSentenceIn(t, tapScreen)
				if !strings.Contains(settings, sentence) {
					t.Errorf("the tap screen ends a %q with %q and the settings screen does not "+
						"carry it.\nThe preview must BE the component rather than a copy of its "+
						"output, or a customer is told something untrue about their own staff.",
						direction, sentence)
				}
			}
		})
	}
}

// brandSentenceIn pulls the send-off out of a rendered tap screen.
//
// 🔴 IT FINDS THE SENTENCE BY ITS MARKUP RATHER THAN BY A LIST OF KNOWN SENTENCES,
// which is what makes the test above a real cross-check. A helper that matched against
// the four strings would be a THIRD copy of them, and the test would then pass by
// agreeing with itself.
func brandSentenceIn(t *testing.T, html string) string {
	t.Helper()
	const open = `<p class="mt-4 font-display text-lg tracking-tight">`
	i := strings.Index(html, open)
	if i < 0 {
		t.Fatalf("no brand sentence in the rendered tap screen; the markup this helper keys on "+
			"has moved.\nbody: %s", html)
	}
	rest := html[i+len(open):]
	j := strings.Index(rest, "</p>")
	if j < 0 {
		t.Fatal("the brand sentence has no closing tag")
	}
	return rest[:j]
}

// --- the role gate --------------------------------------------------------------------

// TestAccount_ManagerMayReadAndMayNotSave is the asymmetry this section chose, and both
// halves are asserted because either alone would be a different product.
//
// 🔴 IT IS THE OPPOSITE SHAPE FROM BILLING'S, DELIBERATELY. Billing is owner-only
// INCLUDING the read, because everything on it is a commercial term. Nothing here is:
// a VAT number is public data (migration 00001 says so where the column is declared)
// and the TIMEZONE is the one fact a manager most needs to be able to check — it is the
// clock every date they read is worked out in.
func TestAccount_ManagerMayReadAndMayNotSave(t *testing.T) {
	acc := newFakeAccount()
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestManagerRole)

	rec := b.do(http.MethodGet, accountHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a manager GET %s: %d, want 200 — the read is open to both roles", accountHref, rec.Code)
	}
	html := htmlOf(t, rec)
	// 🔴 THE ASSERTION NAMES THIS FORM'S OWN FIELDS RATHER THAN `<form method="post"`,
	// AND THE FIRST DRAFT DID THE LATTER AND FAILED. The panel shell carries a sign-out
	// form on every page (admin.templ), so a check for any POST form is a check that can
	// never pass — which is the shape where somebody "fixes" the product to satisfy a
	// broken assertion.
	for _, control := range []string{`name="timezone"`, `name="business_type"`, `name="name"`} {
		if strings.Contains(html, control) {
			t.Errorf("a manager is shown the save form (%s).\nHiding it is the courtesy half of "+
				"the gate; the server refuses the POST on its own, but a button that always "+
				"fails teaches somebody a control exists that does not apply to them.", control)
		}
	}
	if !strings.Contains(html, htmlText("changed by an owner")) {
		t.Error("a manager is shown no form and no reason.\n§4.6: a control that simply " +
			"disappears tells nobody anything — see fillBillingControl's rule.")
	}
	// AND THE FACTS ARE STILL THERE, which is the point of opening the read.
	if !strings.Contains(html, htmlText("Europe/Malta")) {
		t.Error("a manager cannot see the timezone, which is the clock every date they read is " +
			"worked out in.")
	}
}

// TestAccountSave_RefusalIsRecorded — a manager who POSTs anyway is refused, nothing is
// written, and the OWNER can find out that it happened.
//
// 🔴 THE AUDIT ROW IS THE M6-12 PRECEDENT APPLIED (user decision, 2026-08-09): a
// structured log line has no tenant boundary, no retention story and no reader outside
// the operator, and the person who most needs to know that a manager has been trying to
// move the business's timezone is the owner.
func TestAccountSave_RefusalIsRecorded(t *testing.T) {
	acc := newFakeAccount()
	trail := &fakeTrail{}
	b := accountBrowser(t, acc, trail, accountTestManagerRole)

	rec := b.do(http.MethodPost, accountHref, url.Values{
		"name":          {"Somebody Else Ltd"},
		"business_type": {"cafe"},
		"timezone":      {"Europe/Istanbul"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST as a manager: %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.Contains(got, "problem=not-permitted") {
		t.Errorf("a refused manager is sent to %q, want the not-permitted word", got)
	}
	if n := len(acc.savedCommands()); n != 0 {
		t.Errorf("a refused POST reached the store %d time(s); it must reach it none", n)
	}
	// 🔴 AND IT COST NO DATABASE READ EITHER. The gate runs BEFORE the first read, which
	// is the ordering locationactions.go's removals use — a refusal that still paid for a
	// query would be a budget a stranger with a manager's cookie could spend.
	if n := acc.readCount(); n != 0 {
		t.Errorf("a refused POST performed %d read(s); the role gate must run before the "+
			"first database call", n)
	}
	if n := trail.count(ActionAccountUpdateRefused); n != 1 {
		t.Fatalf("the trail holds %d %s row(s), want exactly 1", n, ActionAccountUpdateRefused)
	}
	e := trail.eventsSnapshot()[0]
	if e.TenantID != panelTestTenant {
		t.Errorf("the refusal row names tenant %s, want the SESSION's %s (§4.5)", e.TenantID, panelTestTenant)
	}
	if e.Target != panelTestTenant.String() {
		t.Errorf("the refusal row targets %q, want the business itself", e.Target)
	}
	// 🔴 NOTHING THAT WAS POSTED IS IN THE ROW. audit_log is append-only and
	// tenant-scoped; letting anybody who can POST this route write text of their
	// choosing into it would be a bloat surface with no cleanup.
	detail, ok := e.Detail.(refusedAccountDetail)
	if !ok {
		t.Fatalf("the refusal detail is %T, want refusedAccountDetail", e.Detail)
	}
	if detail.Role != accountTestManagerRole || detail.RequiredRole != adminRoleOwner {
		t.Errorf("the refusal row records role=%q required=%q, want %q and %q",
			detail.Role, detail.RequiredRole, accountTestManagerRole, adminRoleOwner)
	}
}

// --- the save -------------------------------------------------------------------------

// TestAccountSave_StoresTheZoneAndSaysWhatItMoves is the card's timezone criterion, and
// the screen's own warning is asserted with it: the M7-05 card raises "does changing the
// zone move a closed month?" and the answer has two halves that must BOTH be on screen.
func TestAccountSave_StoresTheZoneAndSaysWhatItMoves(t *testing.T) {
	acc := newFakeAccount()
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)

	rec := b.do(http.MethodPost, accountHref, url.Values{
		"name":          {"Kebab Factory Ltd."},
		"business_type": {"restaurant"},
		"timezone":      {"Europe/Istanbul"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST as an owner: %d, want 303\nbody: %s", rec.Code, htmlOf(t, rec))
	}
	cmds := acc.savedCommands()
	if len(cmds) != 1 {
		t.Fatalf("the store saw %d save(s), want 1", len(cmds))
	}
	if cmds[0].Timezone != "Europe/Istanbul" {
		t.Errorf("the stored zone is %q, want Europe/Istanbul", cmds[0].Timezone)
	}
	// 🔴 §4.5 AND §4.7: THE TENANT AND THE ACTOR ARE THE SESSION'S. There is no form
	// field for either and this is what proves the handler did not invent one.
	if cmds[0].TenantID != panelTestTenant {
		t.Errorf("the command names tenant %s, want the session's %s", cmds[0].TenantID, panelTestTenant)
	}
	if cmds[0].ActorID != panelTestAdmin {
		t.Errorf("the command names actor %s, want the session's %s", cmds[0].ActorID, panelTestAdmin)
	}

	html := accountPage(t, acc)
	for _, want := range []string{
		"who was late",             // the shift half
		"has not been frozen yet",  // the unfrozen-month half
		"does not move",            // the frozen-month half — the one that is SAFE
		"keeps this zone for ever", // and the one that is PERMANENT
	} {
		if !strings.Contains(html, htmlText(want)) {
			t.Errorf("the timezone warning does not say %q.\nThe M7-05 card asks what changing "+
				"the zone moves, and the honest answer has both halves: a frozen month is "+
				"unaffected (migration 00016 stores its own dates AND its own zone) and an "+
				"unfrozen one moves with it.", want)
		}
	}
}

// TestAccountSave_RefusesAZoneThisBuildCannotLoad — the one input on this form that can
// silently break every screen in the business.
//
// 🔴 IT IS REFUSED RATHER THAN STORED AND FALLEN BACK FROM. Every reader of this column
// falls back to UTC when the name will not load, which is right for a value already in
// the database and wrong for one somebody is typing: "Europe/Maltaa" would move the
// whole panel to UTC with nothing but a warning in the operator's log.
func TestAccountSave_RefusesAZoneThisBuildCannotLoad(t *testing.T) {
	for _, bad := range []string{"Europe/Maltaa", "Local", "", "  "} {
		t.Run("zone="+bad, func(t *testing.T) {
			acc := newFakeAccount()
			b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)
			rec := b.do(http.MethodPost, accountHref, url.Values{
				"name":          {"Kebab Factory Ltd."},
				"business_type": {"restaurant"},
				"timezone":      {bad},
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("a bad zone answered %d, want 200 with the form again", rec.Code)
			}
			if n := len(acc.savedCommands()); n != 0 {
				t.Fatalf("a bad zone reached the store %d time(s)", n)
			}
			html := htmlOf(t, rec)
			// ⚠️ THIS USED TO CHECK strings.Contains(html, "timezone") AND MEASURED
			// NOTHING: the word is in the label's for=, the input's name=, the datalist
			// id, the help text and the warning notice, i.e. in EVERY render including a
			// plain load. The real assertion — that the message is attached to the field
			// it is about — is TestAccountSave_EachMessageIsAttachedToItsOwnField.
			if fieldError(t, html, "timezone") == "" {
				t.Error("the re-rendered form carries no message on the timezone field")
			}
			// AND THE OWNER KEEPS WHAT THEY TYPED. A redirect would have thrown the name
			// away to report that a zone has a typo in it.
			if !strings.Contains(html, htmlText("Kebab Factory Ltd.")) {
				t.Error("the re-rendered form lost what the owner typed")
			}
		})
	}
}

// TestAccountSave_RefusesABusinessTypeOutsideTheSchemasList — the <select> is drawn from
// the schema's own vocabulary, and a value that did not come from it is refused.
func TestAccountSave_RefusesABusinessTypeOutsideTheSchemasList(t *testing.T) {
	acc := newFakeAccount()
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)
	rec := b.do(http.MethodPost, accountHref, url.Values{
		"name":          {"Kebab Factory Ltd."},
		"business_type": {"nightclub"},
		"timezone":      {"Europe/Malta"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("an unknown business type answered %d, want 200 with the form again", rec.Code)
	}
	if n := len(acc.savedCommands()); n != 0 {
		t.Fatalf("an unknown business type reached the store %d time(s)", n)
	}
}

// TestAccount_TheTypeListIsTheSchemasVocabulary — the form offers exactly the eight the
// database accepts, and it is DERIVED rather than compared with a literal.
func TestAccount_TheTypeListIsTheSchemasVocabulary(t *testing.T) {
	html := accountPage(t, newFakeAccount())
	for _, want := range signup.BusinessTypes {
		if !strings.Contains(html, `value="`+want.Value+`"`) {
			t.Errorf("the kind-of-business list does not offer %q, which migration 00001's "+
				"CHECK accepts", want.Value)
		}
	}
	if strings.Contains(html, `value="nightclub"`) {
		t.Error("the list offers a value the schema would refuse")
	}
}

// TestAccountSave_RefusesANameTheInvoiceCannotCarry — an empty one, a whitespace-only
// one, and one rune over the domain's limit. A business with no name is what
// every invoice and every export header would then be made out to.
func TestAccountSave_RefusesANameTheInvoiceCannotCarry(t *testing.T) {
	tests := []struct{ name, value string }{
		{"empty", ""},
		{"whitespace only", "   "},
		{"one rune over the domain's limit", strings.Repeat("ħ", tenant.AccountNameLimit+1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			acc := newFakeAccount()
			b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)
			rec := b.do(http.MethodPost, accountHref, url.Values{
				"name":          {tc.value},
				"business_type": {"restaurant"},
				"timezone":      {"Europe/Malta"},
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("answered %d, want 200 with the form again", rec.Code)
			}
			if n := len(acc.savedCommands()); n != 0 {
				t.Fatalf("it reached the store %d time(s)", n)
			}
		})
	}
	// AND THE LIMIT ITSELF IS ACCEPTED, which is what stops the check above passing
	// because the boundary refuses everything.
	acc := newFakeAccount()
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)
	rec := b.do(http.MethodPost, accountHref, url.Values{
		"name":          {strings.Repeat("ħ", tenant.AccountNameLimit)},
		"business_type": {"restaurant"},
		"timezone":      {"Europe/Malta"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a name of exactly %d runes answered %d, want 303 — the boundary is refusing "+
			"the very length it advertises", tenant.AccountNameLimit, rec.Code)
	}
}

// TestAccountSave_BoundsTheBody — a bare ParseForm inherits net/http's 10 MB, and a
// valid session may spend adminSessionLimit requests per window.
func TestAccountSave_BoundsTheBody(t *testing.T) {
	acc := newFakeAccount()
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)
	body := "name=" + strings.Repeat("x", maxAccountBody+1)
	rec := b.doRaw(http.MethodPost, accountHref, body)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an oversized body answered %d, want 303 with the unreadable word", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.Contains(got, "problem=unreadable") {
		t.Errorf("an oversized body redirected to %q, want the unreadable word", got)
	}
	if n := len(acc.savedCommands()); n != 0 {
		t.Fatalf("an oversized body reached the store %d time(s)", n)
	}
}

// --- §4.6: a failed read is not an empty form -------------------------------------------

// TestAccount_AFailedReadIsAProblemPageAndNeverABlankForm.
//
// 🔴 A SETTINGS FORM PRE-FILLED WITH BLANKS IS AN INVITATION TO SAVE BLANKS over a row
// that read perfectly well a minute ago — and on this form the blanks would be the
// business's name and the zone every date in the panel is worked out in.
func TestAccount_AFailedReadIsAProblemPageAndNeverABlankForm(t *testing.T) {
	acc := newFakeAccount()
	acc.readErr = errors.New("boom")
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)

	rec := b.do(http.MethodGet, accountHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failed read answered %d, want 500", rec.Code)
	}
	html := htmlOf(t, rec)
	if strings.Contains(html, `name="timezone"`) {
		t.Error("a failed read still drew the form. Every box would be empty, and saving it " +
			"would overwrite the row with blanks.")
	}
	if !strings.Contains(html, htmlText("could not read")) {
		t.Error("a failed read does not say the read failed (§4.6)")
	}
}

// TestAccount_TheSectionIsInThePanelTable — the tab, the route and the label are one
// fact, so a link that 404s is not something this section can introduce.
func TestAccount_TheSectionIsInThePanelTable(t *testing.T) {
	var found bool
	for _, s := range pages.PanelSections {
		if s.Tab == pages.TabAccount {
			found = true
			if s.Href != accountHref {
				t.Errorf("the section table says %q and the handler uses %q", s.Href, accountHref)
			}
			if s.OwnerOnly {
				t.Error("the Account tab is marked OwnerOnly.\nThe READ is open to a manager " +
					"on purpose — nothing on the page is a commercial term — and hiding the tab " +
					"would leave a manager unable to check which zone their panel measures a " +
					"day in. The SAVE is what is gated.")
			}
			if s.OperatorOnly {
				t.Error("the Account tab is marked OperatorOnly; it is the customer's own screen")
			}
		}
	}
	if !found {
		t.Fatal("pages.PanelSections has no account section; this test is measuring nothing")
	}
}

// TestAccount_TheVATMarkersClassesLiveWhereTailwindLooks is a REGRESSION test for a
// bug this screen actually shipped with for one build.
//
// 🔴 TAILWIND SCANS .templ AND .js, NEVER GO. The first draft of fillAccountVAT
// assembled the marker's classes in Go ("border-line bg-line/10"), and the measurement
// was unambiguous: `bg-line/10` produced ZERO rules in a fresh app.css, so the "not
// checked" state rendered with a border and no tint — while the other three looked
// perfect, because their colours happen to be used by other templates. A bug visible in
// one state out of four is one nobody finds by looking.
//
// ⚠️ IT READS THE SOURCES RATHER THAN app.css, DELIBERATELY. app.css is a build
// artefact that `make check` does not produce (M5-06's lesson: two tests skipped for
// months because CI never ran `make css`), so a test keyed on it would be a test that
// passes by being absent. What can be checked always is WHERE the class names are
// written, which is the whole of the rule.
//
// 🔴 THE .templ HALF MATCHES A `class="…"` ATTRIBUTE AND NOT THE FILE, AND THE FIRST
// DRAFT MATCHED THE FILE. An auditor measured that half green after deleting the class
// from the MARKUP and leaving it in a comment — and then measured something sharper:
// Tailwind scans a .templ as PLAIN TEXT, comments included, so `make css` still emitted
// the rule. The stylesheet was therefore right and the test was still wrong, because it
// was claiming to measure that an ELEMENT carries the class. Both halves are closed:
// the comments in account.templ no longer spell any utility (skill tappa-brand's rule),
// and this walks the attributes.
func TestAccount_TheVATMarkersClassesLiveWhereTailwindLooks(t *testing.T) {
	tints := []string{
		"bg-tappa-green/10", "bg-tomato/10", "bg-saffron/10", "bg-line/10",
	}
	templSrc, err := os.ReadFile(filepath.Join("..", "..", "web", "templates", "pages", "account.templ"))
	if err != nil {
		t.Fatalf("read account.templ: %v", err)
	}
	// EVERY class="…" VALUE IN THE FILE, so prose cannot satisfy the check.
	attrs := regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(string(templSrc), -1)
	// ANTI-VACUITY: a regex that matched nothing would report every class missing, or —
	// worse, if the loop below were ever inverted — nothing missing at all.
	if len(attrs) < 10 {
		t.Fatalf("found %d class attribute(s) in account.templ; the screen has many more, "+
			"so this scan is reading the wrong file or the pattern has rotted", len(attrs))
	}
	var carried []string
	for _, m := range attrs {
		carried = append(carried, strings.Fields(m[1])...)
	}
	for _, class := range tints {
		if !slices.Contains(carried, class) {
			t.Errorf("no element in account.templ carries %q.\nTailwind's content globs are "+
				"web/templates/**/*.templ and web/static/js/**/*.js; a class no element uses "+
				"is a marker rendering unstyled. (A mention in PROSE does not count here, "+
				"even though Tailwind's plain-text scan would still emit the rule for it — "+
				"that keeps a dead rule alive and tells this test nothing about the markup.)",
				class)
		}
	}
	// AND THE GO SIDE MUST NOT CARRY THEM, which is the half that catches a well-meant
	// "tidy-up" moving the mapping back into a switch statement.
	for _, file := range []string{
		filepath.Join("account.go"),
		filepath.Join("..", "..", "web", "templates", "pages", "accountview.go"),
	} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, class := range tints {
			// The comment in each file NAMES bg-line/10 while explaining this rule, so the
			// check is for a class inside a Go STRING LITERAL rather than anywhere at all.
			if strings.Contains(string(src), `"`+class) || strings.Contains(string(src), class+`"`) {
				t.Errorf("%s builds the class %q in Go.\nTailwind does not scan Go, so that "+
					"class is compiled out of app.css — the exact failure view.go records for "+
					"the tap screen's stamps.", file, class)
			}
		}
	}
}

// TestAccount_NeverShowsTheStructureAnswer is a DROPPED feature, pinned so it stays
// dropped and the reason travels with it.
//
// 🔴 THE FIRST DRAFT SHOWED IT READ-ONLY AND A TRIPWIRE REFUSED.
// TestSignupStructure_DecidesNothingAfterSignUp walks the repository and bans any
// selector named Structure outside internal/domain/signup and internal/store, because
// the wizard's own wording promises the single/multi answer stops mattering the moment
// the row is written. A settings screen printing it would have made this package its
// first reader, and it would have had to print a sentence explaining that the value
// decides nothing — which is an admission that it does not belong on the screen. The
// column is no longer even selected: db/queries/tenants.sql carries that at the
// statement.
func TestAccount_NeverShowsTheStructureAnswer(t *testing.T) {
	html := accountPage(t, newFakeAccount())
	for _, forbidden := range []string{"several sites", "one site", "single", "multi"} {
		if strings.Contains(html, htmlText(forbidden)) {
			t.Errorf("the account screen prints %q.\nThe single/multi answer gates nothing "+
				"(TestSignupStructure_DecidesNothingAfterSignUp), and showing it makes the "+
				"panel the first reader of a field the wizard promises decides nothing.",
				forbidden)
		}
	}
}

// TestAccountSave_ARefusedSaveShowsBothTheFileAndTheTyping is the REGRESSION test for
// the defect this screen shipped with, and it is on the REFUSED path because that is
// where the defect lived — a plain load could never show it, since Form and OnFile are
// equal there.
//
// 🔴 WHAT WENT WRONG. renderAccountFormAgain re-read the row and then overwrote the
// whole Form with the submission, and the facts docket — the block headed "On file" —
// read Form. Measured before the fix, with a save that was refused so NOTHING was
// written: the typed name and the typed zone were printed under "On file", the stored
// name and zone appeared nowhere on the page, and a restaurant was shown a production
// send-off. Three false facts about a business, one of them the clock every date in the
// panel is worked out in, with no way for the owner to notice.
//
// 🔴 WHY THE OLD TESTS DID NOT CATCH IT. Several drove the refused path, but every one
// of them asked whether the TYPING survived — none asked what the FACTS block was
// printing. A page that keeps your typing and lies about your business passes all of
// them. So this asserts BOTH halves, in both directions.
func TestAccountSave_ARefusedSaveShowsBothTheFileAndTheTyping(t *testing.T) {
	acc := newFakeAccount()
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)

	rec := b.do(http.MethodPost, accountHref, url.Values{
		"name":          {"NOT THE STORED NAME Ltd"},
		"business_type": {"production"},
		"timezone":      {"Europe/Maltaa"}, // refused: not a zone this build can load
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a refused save answered %d, want 200 with the form again", rec.Code)
	}
	if n := len(acc.savedCommands()); n != 0 {
		t.Fatalf("a refused save reached the store %d time(s); every assertion below is "+
			"about a page on which NOTHING was written", n)
	}
	body := htmlOf(t, rec)

	// HALF ONE — THE FILE. The stored values must be on the page, because the block
	// they sit in says "On file".
	for _, want := range []string{"Kebab Factory Ltd.", "Europe/Malta"} {
		if !strings.Contains(body, htmlText(want)) {
			t.Errorf("the stored value %q is nowhere on the page after a refused save.\n"+
				"The docket is headed \"On file\"; if what is on file is absent, the owner "+
				"cannot see that their edit did not take.", want)
		}
	}
	// AND THE TYPED VALUES ARE NOT IN THE DOCKET. Substring checks alone cannot tell a
	// value in a box from a value in the facts block, so the two markup shapes the
	// docket uses are asserted directly.
	for _, forbidden := range []string{
		`tracking-tight text-ink">NOT THE STORED NAME Ltd<`,
		`font-mono text-sm text-ink">Europe/Maltaa<`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the facts docket is printing what was TYPED (%s).\nThat block answers "+
				"\"what does Tappa have on file\"; a rejected submission is not an answer to it.",
				forbidden)
		}
	}

	// HALF TWO — THE TYPING. It must survive, or a correction costs all three fields.
	for _, want := range []string{"NOT THE STORED NAME Ltd", "Europe/Maltaa"} {
		if !strings.Contains(body, htmlText(want)) {
			t.Errorf("the owner's typed value %q was thrown away", want)
		}
	}

	// HALF THREE — THE STAFF SENTENCE FOLLOWS THE FILE, NOT THE BOX. Nothing about the
	// employees' screen changed, so the preview must not change either.
	if !strings.Contains(body, htmlText("keep those kebabs rolling")) {
		t.Error("after a refused save the staff send-off is no longer the stored kind of " +
			"business's. Nothing was written, so nothing an employee reads has changed.")
	}
	if strings.Contains(body, htmlText("stay safe on the floor")) {
		t.Error("after a refused save the page previews the TYPED kind of business's " +
			"send-off, which no employee of this business will see.")
	}

	// HALF FOUR — THE DIFFERENCE IS NAMED, PER FIELD. Keeping the typing without saying
	// it was refused leaves a page that looks exactly like a successful save.
	if n := strings.Count(body, htmlText("Not saved — on file this is still")); n != 3 {
		t.Errorf("the page names %d unsaved field(s), want 3 — all three differ from the "+
			"row.\nbody: %s", n, body)
	}
}

// TestAccountSave_TheUnsavedNoticeNamesOnlyTheFieldsThatDIFFER is the discriminating
// half of the test above.
//
// 🔴 A NOTICE ON EVERY FIELD IS THE SAME AS NO NOTICE. If the sentence appeared beside
// each box whatever was typed, it would carry no information and the test above would
// pass on a page that says "not saved" about a field nobody touched.
func TestAccountSave_TheUnsavedNoticeNamesOnlyTheFieldsThatDIFFER(t *testing.T) {
	acc := newFakeAccount()
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)

	// Only the ZONE differs, and it is the field that is refused.
	rec := b.do(http.MethodPost, accountHref, url.Values{
		"name":          {"Kebab Factory Ltd."},
		"business_type": {"restaurant"},
		"timezone":      {"Europe/Maltaa"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a refused save answered %d, want 200", rec.Code)
	}
	body := htmlOf(t, rec)
	if n := strings.Count(body, htmlText("Not saved — on file this is still")); n != 1 {
		t.Errorf("one field differs from the row and the page names %d.\nA notice that "+
			"appears whatever was typed says nothing.", n)
	}
	if !strings.Contains(body, htmlText("still <span class=\"font-mono\">Europe/Malta</span>")) &&
		!strings.Contains(body, `still <span class="font-mono">Europe/Malta</span>`) {
		t.Errorf("the notice does not name the zone that is still on file.\nbody: %s", body)
	}
}

// TestAccount_APlainLoadNamesNothingAsUnsaved — the notice belongs to a refusal and to
// nothing else. A GET that printed it would be telling a customer their settings did
// not save when they never pressed anything.
func TestAccount_APlainLoadNamesNothingAsUnsaved(t *testing.T) {
	html := accountPage(t, newFakeAccount())
	if strings.Contains(html, htmlText("Not saved")) {
		t.Error("a plain page load says something was not saved")
	}
}

// TestAccount_TheSignUpConfirmationPointsAtThisScreen closes the loop M7-02 opened.
//
// 🔴 A CUSTOMER WHOSE VAT NUMBER WAS REFUSED WAS TOLD SO ONCE, ON A SCREEN THAT IS SHOWN
// AND THEN GONE. The M7-05 card's 2026-08-13 addition is exactly that: the verdict sat
// in the database and no screen read it. Both halves are now shipped — this section
// reads it, and the confirmation says where to look — so this test asserts the SECOND
// half against the FIRST rather than against a literal.
//
// ⚠️ IT ASSERTS THE LABEL FROM THE SECTION TABLE, NOT THE WORD "Account". A renamed tab
// must rename the sentence; a customer sent looking for a tab that no longer exists is
// the same defect in a smaller size.
func TestAccount_TheSignUpConfirmationPointsAtThisScreen(t *testing.T) {
	label := pages.SectionLabel(pages.TabAccount)
	if label == "" {
		t.Fatal("the section table has no label for the account tab; this test measures nothing")
	}
	for _, tc := range []struct {
		name string
		view pages.SignupDoneView
	}{
		{"the register refused the number", pages.SignupDoneView{VATRefused: true}},
		{"the register could not be reached", pages.SignupDoneView{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			note := tc.view.VATNote()
			if !strings.Contains(note, label) {
				t.Errorf("the confirmation's VAT note never names the %q page, so a customer "+
					"with an unconfirmed number is told about it once and then has nowhere to "+
					"look.\nnote: %s", label, note)
			}
		})
	}
	// AND THE CONFIRMED CASE STAYS SILENT ABOUT IT, deliberately: there is nothing to
	// act on, and a pointer on every outcome is a pointer nobody reads.
	if note := (pages.SignupDoneView{VATVerified: true}).VATNote(); strings.Contains(note, label) {
		t.Errorf("a confirmed number is sent to the %q page for no reason.\nnote: %s", label, note)
	}
	// 🔴 AND NOTHING PROMISES A RE-CHECK, which the product cannot do: migration 00017
	// withholds UPDATE on both columns and M7-05 measured that it should stay withheld.
	for _, v := range []pages.SignupDoneView{{VATRefused: true}, {}, {VATVerified: true}} {
		for _, forbidden := range []string{"check again", "re-check", "recheck", "try again"} {
			if strings.Contains(strings.ToLower(v.VATNote()), forbidden) {
				t.Errorf("the confirmation offers %q, which nothing in the product can do", forbidden)
			}
		}
	}
}

// TestAccount_DatesAreTheBUSINESSsClockAndNeverUTC is the regression test for a defect
// that told a business it registered on a day it was not yet a customer.
//
// 🔴 WHAT BROKE. The registration date was printed with `.UTC()` while the line three
// rows below it, under the same "On file" heading, says the business's zone. A business
// that registered at 00:30 Malta on 2 February — 2026-02-01T23:30:00Z — read
// "1 February 2026". CLAUDE.md §6 names the class, and this session had already fixed
// the same class once, in a ledger test whose commit is titled "resolve the day in the
// tenant's zone, not in UTC".
//
// 🔴 THE FIXTURE CROSSES MIDNIGHT ON PURPOSE AND OWES NOTHING TO THE CLOCK. Every
// instant here is a literal, and the zone is chosen so the UTC calendar date and the
// business's differ BY CONSTRUCTION — so this measures the same thing at 03:00 and at
// 23:00, which is the standard this session set for itself when it fixed the ledger
// flake. A test that only fails during the hours the bug fires is a test asleep for
// most of the day.
func TestAccount_DatesAreTheBUSINESSsClockAndNeverUTC(t *testing.T) {
	tests := []struct {
		name string
		zone string
		// created and asked are the same INSTANT in every case; only the zone moves.
		wantRegistered string
		wantAsked      string
	}{
		{
			// 23:30 UTC on 1 Feb is 00:30 on 2 Feb in Malta (UTC+1 in winter).
			name: "Malta, half an hour past its own midnight",
			zone: "Europe/Malta", wantRegistered: "2 February 2026",
			wantAsked: "2 February 2026, 00:30",
		},
		{
			// The same instant, a zone that is BEHIND: still 1 Feb, and late.
			name: "New York, still the previous day",
			zone: "America/New_York", wantRegistered: "1 February 2026",
			wantAsked: "1 February 2026, 18:30",
		},
		{
			// A business that really is in UTC reads the UTC day — which is what makes
			// the two cases above assertions about the ZONE rather than about the format.
			name: "UTC, where the two calendars agree",
			zone: "UTC", wantRegistered: "1 February 2026",
			wantAsked: "1 February 2026, 23:30",
		},
	}
	instant := time.Date(2026, 2, 1, 23, 30, 0, 0, time.UTC)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			acc := newFakeAccount()
			acc.settings.Timezone = tc.zone
			acc.settings.CreatedAt = instant
			verified := false
			acc.setVAT(&verified, &instant)

			html := accountPage(t, acc)
			// 🔴 THE REGISTRATION DATE IS READ OUT OF ITS OWN CELL, NOT SEARCHED FOR IN
			// THE PAGE, AND THE FIRST DRAFT DID THE LATTER AND WAS MEASURED USELESS. The
			// VAT stamp two blocks down carries the SAME date ("2 February 2026, 00:30"),
			// so a substring check for "2 February 2026" was satisfied by the wrong
			// element: the mutation that put `.UTC()` back left this assertion GREEN and
			// only the discriminating pair at the end of the test caught it. Reading the
			// cell is what makes the assertion about the field it names.
			if got := docketCell(t, html, "Registered on"); got != tc.wantRegistered {
				t.Errorf("a business in %s that registered at %s is told %q, want %q.\n"+
					"Its own zone is printed three rows away under the same \"On file\" "+
					"heading; a date beside it in another calendar is a fact about a "+
					"different day.", tc.zone, instant.Format(time.RFC3339), got, tc.wantRegistered)
			}
			if !strings.Contains(html, htmlText(tc.wantAsked)) {
				t.Errorf("the VAT check's stamp is not %q for a business in %s.\n"+
					"It used to print raw RFC3339 — honest, and the only machine timestamp "+
					"a customer was shown anywhere in the panel.", tc.wantAsked, tc.zone)
			}
			// AND NO RAW MACHINE TIME SURVIVES ANYWHERE ON THE PAGE.
			if strings.Contains(html, "2026-02-01T23:30:00Z") {
				t.Errorf("the page still carries a raw RFC3339 stamp")
			}
		})
	}
	// 🔴 THE DISCRIMINATING PAIR: the SAME instant renders as two different days in two
	// zones. Without this, every assertion above would still pass on a screen that
	// formatted in UTC and happened to agree.
	malta, ny := newFakeAccount(), newFakeAccount()
	malta.settings.Timezone, ny.settings.Timezone = "Europe/Malta", "America/New_York"
	malta.settings.CreatedAt, ny.settings.CreatedAt = instant, instant
	maltaCell := docketCell(t, accountPage(t, malta), "Registered on")
	nyCell := docketCell(t, accountPage(t, ny), "Registered on")
	if maltaCell == nyCell {
		t.Errorf("one instant renders as %q in BOTH Europe/Malta and America/New_York, so "+
			"the screen is not resolving it in the business's zone at all — it is "+
			"formatting a constant.", maltaCell)
	}
}

// docketCell reads ONE labelled value out of the "On file" docket, and docketName reads
// the block's heading.
//
// 🔴 THEY EXIST BECAUSE A WHOLE-DOCUMENT strings.Contains CANNOT NAME A FIELD, AND THAT
// WAS MEASURED TWICE ON THIS SCREEN. The registration date's assertion was satisfied by
// the VAT stamp two blocks down; the business name's was satisfied by the FORM's own
// value= attribute on a plain load and by the "still X on file" notice on a refused one
// — so emptying the name cell left the entire handler package green (218 s, 0
// failures). Every docket cell is now read from the cell that carries it.
func docketCell(t *testing.T, html, label string) string {
	t.Helper()
	m := regexp.MustCompile(regexp.QuoteMeta(label) + `</dt><dd[^>]*>([^<]*)</dd>`).
		FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no %q cell in the rendered page; the markup this helper keys on has "+
			"moved.\nbody: %s", label, html)
	}
	return m[1]
}

// docketName reads the docket's own heading, anchored on the section so the screen's
// three other <h2>s cannot answer for it.
func docketName(t *testing.T, html string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<section class="docket">.*?<h2[^>]*>([^<]*)</h2>`).
		FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no heading inside the \"On file\" docket; the markup this helper keys "+
			"on has moved.\nbody: %s", html)
	}
	return m[1]
}

// TestAccount_AnUnloadableZoneFallsBackToUTCRatherThanRefusingThePage.
//
// 🔴 THE ONE SCREEN THAT CAN FIX A BROKEN ZONE MUST NOT BE THE ONE THAT REFUSES TO
// RENDER BECAUSE OF IT. Every other reader of this column falls back to UTC and logs;
// here the stakes are higher, because this is where a customer would go to correct it.
// The value cannot be written through this screen — tenant.ValidTimezone refuses it —
// so what is under test is a row that already holds one.
func TestAccount_AnUnloadableZoneFallsBackToUTCRatherThanRefusingThePage(t *testing.T) {
	acc := newFakeAccount()
	acc.settings.Timezone = "Europe/Maltaa"
	acc.settings.CreatedAt = time.Date(2026, 2, 1, 23, 30, 0, 0, time.UTC)

	rec := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole).
		do(http.MethodGet, accountHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a business whose stored zone will not load answered %d, want 200 — the "+
			"screen that fixes it must render", rec.Code)
	}
	html := htmlOf(t, rec)
	if !strings.Contains(html, htmlText("1 February 2026")) {
		t.Error("the fallback did not render the date in UTC")
	}
	// AND THE BROKEN VALUE IS SHOWN, so the owner can see what to correct.
	if !strings.Contains(html, htmlText("Europe/Maltaa")) {
		t.Error("the unloadable zone is not shown, so nobody can see what to fix")
	}
}

// TestAccount_TheNoAnswerCohortIsCalledTheSameThingOnBothScreens is the link C6 asked
// for: two screens describing ONE event in words that agree.
//
// 🔴 WHAT THEY USED TO SAY. A customer whose registration could not reach VIES read
// "We could not reach the EU VAT register just now" on the confirmation, and then read
// "Nothing has asked the European Commission's VAT register about this number" on the
// account screen. One said we asked; the other said nobody did. Both were describing
// the same registration.
//
// 🔴 THE COHORT IS DERIVED FROM THE PRODUCT, NOT ASSUMED. internal/domain/signup's
// VATUnknown.Verified() is asked here rather than a nil being written down, and its
// answer is what decides which account-screen state this customer lands in — so if the
// wizard ever starts recording the two apart, this test changes with it instead of
// quietly pinning a stale mapping.
func TestAccount_TheNoAnswerCohortIsCalledTheSameThingOnBothScreens(t *testing.T) {
	// 1. WHAT THE WIZARD RECORDS for a check that could not be reached. Verified() is
	// the product's own function; checkedAt is unexported and its behaviour — NULL for
	// VATUnknown, which collapses "asked, no answer" into "never asked" — is stated in
	// its own comment and is why this cohort lands where it does.
	verified := signup.VATUnknown.Verified()
	if verified != nil {
		t.Fatalf("signup.VATUnknown.Verified() = %v, want nil; this test's whole mapping "+
			"rests on an unreachable check recording no verdict", *verified)
	}
	state := tenant.VATCheck{Verified: verified, CheckedAt: nil}.State()
	if state != tenant.VATNeverChecked {
		t.Fatalf("a registration that could not reach the register reads as %q; this test "+
			"is pinned to the wrong branch", state)
	}

	// 2. WHAT THE CONFIRMATION TELLS THAT CUSTOMER.
	note := (pages.SignupDoneView{}).VATNote()
	if !strings.Contains(note, "could not reach") {
		t.Fatalf("the confirmation's unknown-cohort sentence no longer says we could not "+
			"reach the register, so this test is comparing the wrong pair.\nnote: %s", note)
	}
	// 🔴 AND IT DOES NOT CLAIM THE REGISTER SAID ANYTHING. For this cohort it said
	// nothing at all, so "shows what the register last said" was an over-claim.
	if strings.Contains(note, "what the register last said") {
		t.Error("the confirmation promises the account page shows what the register said, " +
			"to a customer the register never answered for")
	}

	// 3. WHAT THE ACCOUNT SCREEN TELLS THE SAME CUSTOMER.
	acc := newFakeAccount()
	acc.setVAT(verified, nil)
	html := accountPage(t, acc)
	if strings.Contains(html, htmlText("Nothing has asked")) {
		t.Error("the account screen tells a customer nobody asked the register, while " +
			"their confirmation said we asked and could not reach it. Two screens, one " +
			"event, contradictory words.")
	}
	if !strings.Contains(html, htmlText("could not reach the register")) {
		t.Error("the account screen's no-answer state does not name the cohort the " +
			"confirmation names. A customer cannot tell that the two screens are about " +
			"the same thing.")
	}
	// AND IT ADMITS WHAT THE RECORD CANNOT TELL APART, rather than picking one half.
	if !strings.Contains(html, htmlText("cannot tell them apart")) {
		t.Error("the account screen states one of the two possibilities as fact; the " +
			"stored columns do not distinguish them (signup.checkedAt collapses both to NULL)")
	}
}

// TestAccount_TheUnsavedNoticeSpeaksTheCUSTOMERsWords — C7.
//
// 🔴 THE NOTICE USED TO PRINT THE STORED TOKEN. A café that typed "Bar" and was refused
// read "on file this is still cafe" while the <select> immediately above it showed
// "Café" — the machine's spelling of a value the customer has only ever seen the other
// way. The label list is already on the view, so there was nothing to look up.
func TestAccount_TheUnsavedNoticeSpeaksTheCUSTOMERsWords(t *testing.T) {
	acc := newFakeAccount()
	acc.settings.BusinessType = "cafe"
	b := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole)

	// The kind of business changes AND the zone is refused, so nothing is written and
	// the notice appears for the business type.
	rec := b.do(http.MethodPost, accountHref, url.Values{
		"name":          {"Kebab Factory Ltd."},
		"business_type": {"bar"},
		"timezone":      {"Europe/Maltaa"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a refused save answered %d, want 200", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, htmlText("still <span class=\"font-mono\">Café</span>")) &&
		!strings.Contains(body, `still <span class="font-mono">Café</span>`) {
		t.Errorf("the notice does not name the stored kind of business the way the form "+
			"does (\"Café\").\nbody: %s", body)
	}
	if strings.Contains(body, `<span class="font-mono">cafe</span>`) {
		t.Error("the notice prints the stored token rather than the label the customer sees")
	}
	// THE LABEL COMES FROM THE SAME LIST THE FORM WAS DRAWN FROM, so the two cannot
	// disagree — asserted by requiring the schema's own vocabulary to carry it.
	var found bool
	for _, ty := range signup.BusinessTypes {
		if ty.Value == "cafe" && ty.Label == "Café" {
			found = true
		}
	}
	if !found {
		t.Fatal("internal/domain/signup no longer labels `cafe` as `Café`; this test is " +
			"asserting a literal rather than the product's own list")
	}
}

// fieldError reads the message attached to ONE field, from that field's own error
// element — and only when the input actually points at it.
//
// 🔴 BOTH HALVES ARE REQUIRED AND THE SECOND IS THE ONE THAT WAS MISSING. An element
// carrying the text proves the message was rendered; `aria-describedby` naming that
// element proves it is attached to THIS input rather than merely near it in the source.
// A message a screen reader never reads out with the box is not "beside the input it is
// about", which is the claim validateAccountForm's comment makes.
func fieldError(t *testing.T, html, field string) string {
	t.Helper()
	id := "account-" + field + "-error"
	m := regexp.MustCompile(`id="` + regexp.QuoteMeta(id) + `"[^>]*>([^<]*)</p>`).FindStringSubmatch(html)
	if m == nil {
		// The element may carry its attributes in the other order; try that too before
		// concluding there is none.
		m = regexp.MustCompile(`<p[^>]*id="` + regexp.QuoteMeta(id) + `"[^>]*>([^<]*)</p>`).
			FindStringSubmatch(html)
	}
	if m == nil {
		return ""
	}
	if !regexp.MustCompile(`aria-describedby="[^"]*` + regexp.QuoteMeta(id) + `[^"]*"`).
		MatchString(html) {
		t.Errorf("the %q message is rendered but no input points at it with "+
			"aria-describedby; it is near the box rather than attached to it", field)
	}
	return m[1]
}

// TestAccountSave_EachMessageIsAttachedToItsOwnField is what C10 asked for, and it
// closes a gap an auditor measured: all three per-field messages could be DELETED and
// the whole TestAccount* set stayed green.
//
// 🔴 WHY THE OLD TESTS DID NOT NOTICE. Each refusal test asked only whether the request
// answered 200 and nothing was written. The one assertion that looked like it covered
// the message searched the document for the word "timezone" — which is in the label's
// for=, the input's name=, the datalist id, the help text and the warning notice, so it
// was true on every render, including a plain page load.
//
// 🔴 AND accountactions.go SPENDS A PARAGRAPH ON THIS CLAIM: validateAccountForm returns
// the FIELD so "a message renders beside the input it is about". That sentence had
// nothing measuring it — the lessons table's own rule about a design sentence defending
// a property nothing pins.
func TestAccountSave_EachMessageIsAttachedToItsOwnField(t *testing.T) {
	tests := []struct {
		name  string
		form  url.Values
		field string
		// others must carry NO message: a form that decorated every field would tell
		// the owner nothing about which one to fix.
		others []string
	}{
		{
			name:  "an empty business name",
			form:  url.Values{"name": {""}, "business_type": {"restaurant"}, "timezone": {"Europe/Malta"}},
			field: "name", others: []string{"business_type", "timezone"},
		},
		{
			name:  "a kind of business the schema does not know",
			form:  url.Values{"name": {"Kebab Factory Ltd."}, "business_type": {"nightclub"}, "timezone": {"Europe/Malta"}},
			field: "business_type", others: []string{"name", "timezone"},
		},
		{
			name:  "a zone this build cannot load",
			form:  url.Values{"name": {"Kebab Factory Ltd."}, "business_type": {"restaurant"}, "timezone": {"Europe/Maltaa"}},
			field: "timezone", others: []string{"name", "business_type"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			acc := newFakeAccount()
			rec := accountBrowser(t, acc, &fakeTrail{}, accountTestOwnerRole).
				do(http.MethodPost, accountHref, tc.form)
			if rec.Code != http.StatusOK {
				t.Fatalf("a refused save answered %d, want 200 with the form again", rec.Code)
			}
			if n := len(acc.savedCommands()); n != 0 {
				t.Fatalf("a refused save reached the store %d time(s)", n)
			}
			html := htmlOf(t, rec)

			got := fieldError(t, html, tc.field)
			if got == "" {
				t.Fatalf("the %q field carries no message.\nvalidateAccountForm returns the "+
					"FIELD precisely so the sentence lands on the box that is wrong; without "+
					"it the owner is told something failed and not what.", tc.field)
			}
			// THE MESSAGE SAYS WHAT TO DO, which is the brand's rule for an error.
			if len(got) < 20 {
				t.Errorf("the %q message is %q — too short to say what to do about it", tc.field, got)
			}
			for _, other := range tc.others {
				if msg := fieldError(t, html, other); msg != "" {
					t.Errorf("the %q field also carries a message (%q) when only %q is wrong.\n"+
						"A form that marks every box tells the owner nothing.", other, msg, tc.field)
				}
			}
		})
	}
	// A PAGE WITH NOTHING WRONG CARRIES NO MESSAGE AT ALL — the control that stops the
	// assertions above passing on a form that always decorates itself.
	html := accountPage(t, newFakeAccount())
	for _, field := range []string{"name", "business_type", "timezone"} {
		if msg := fieldError(t, html, field); msg != "" {
			t.Errorf("a plain page load carries a %q message (%q)", field, msg)
		}
	}
}
