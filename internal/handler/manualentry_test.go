package handler

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/web/templates/pages"
)

// manualentry_test.go -- the HTTP half of M6-08.
//
// 🔴 WHAT THESE TESTS CAN AND CANNOT SEE, said first because the temptation to
// over-read them is real. They drive the panel against a fakeRecorder, so what they
// measure is the SHAPE OF THE FLOW: which screen is rendered, which word a refusal
// carries, what a form posts, whether the gate is demanded before the domain is
// reached at all. They cannot see a single column of `transactions` -- not the literal
// verdict, not the NULL evidence columns, not section 4.5's scope, not the audit row's
// shared fate. Every one of those is measured against real Postgres in
// manualentry_db_test.go, because a double agrees with whatever it is told and this
// task's whole subject is what reaches an immutable table.

// entryID is the person every test in this file records for. Fixed, for the reason
// the panel test identities are fixed: a fresh uuid per test would make "the
// confirmation is bound to THIS person" unassertable.
var entryID = uuid.MustParse("d0d0d0d0-0000-4000-8000-00000000e001")

// panelBrowserWithRecorder is panelBrowser with the manual write side under the
// caller's control.
func panelBrowserWithRecorder(t *testing.T, rec *fakeRecorder) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, rec, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeTexts(), newFakeAccount(), adminTestConfig(),
		slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// manualFormHref is the entry screen for the test person.
func manualFormHref() string { return manualEntryHref + "?id=" + entryID.String() }

// maltaAt is the UTC instant a Maltese wall clock names, built the way the handler
// builds it so a test cannot drift from the thing it is checking.
func maltaAt(t *testing.T, date, clock string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatalf("loading Europe/Malta: %v", err)
	}
	at, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, loc)
	if err != nil {
		t.Fatalf("parsing %s %s: %v", date, clock, err)
	}
	return at.UTC()
}

// confirmedEntry walks the review step exactly as a manager does and returns the form
// a write must carry.
//
// 🔴 IT WALKS THE SCREEN RATHER THAN FORGING THE TOKEN, which is deactivateConfirmed's
// rule and the reason the gate means anything: a helper that minted its own value
// would be a test proving the product works against a door it propped open itself.
func confirmedEntry(t *testing.T, b *browser, direction, date, clock, note string) url.Values {
	t.Helper()
	form := url.Values{
		"id": {entryID.String()}, "direction": {direction},
		"date": {date}, "time": {clock},
	}
	if note != "" {
		form.Set("note", note)
	}
	page := htmlOf(t, b.do(http.MethodPost, manualEntryHref, form))
	form.Set(confirmField, confirmTokenIn(t, page))
	return form
}

// TestManualEntryForm_IsAGETThatWritesAndMINTSNothing.
//
// 🔴 THE SECOND HALF IS THE ONE WORTH ASSERTING. "It writes no record" is obvious from
// the handler; "it mints no confirmation" is not, and it is what makes the roster's
// link safe for a crawler and for a prefetch. An audit on the plaque section made a
// card mint a token without rendering one and the whole suite stayed green, so this
// asserts on the COOKIE rather than on the page.
func TestManualEntryForm_IsAGETThatWritesAndMINTSNothing(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	res := b.do(http.MethodGet, manualFormHref(), nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET the form: status %d, want 200", res.Code)
	}
	if _, ok := b.cookies[adminConfirmCookieName]; ok {
		t.Error("the form GET minted a confirmation. Following the roster's link would then " +
			"be half of an irreversible write, and a prefetch would do it silently.")
	}
	if n := len(rec.entries()); n != 0 {
		t.Errorf("the form GET wrote %d record(s); a GET writes none", n)
	}
	body := htmlOf(t, res)
	if !strings.Contains(body, "Review this record") {
		t.Error("the form does not offer the review step, so there is no way forward from it")
	}
	// 🔴 THE WRITE ADDRESS MUST NOT BE ON THE FIRST STEP. What makes that true is the
	// template's `if v.Confirming`, not the empty field -- see the sibling test below,
	// which is the one a mutation forced into existence.
	if strings.Contains(body, `action="`+manualRecordHref+`"`) {
		t.Error("the first step carries the WRITE address. The confirmation step is then " +
			"decoration: a form built from this page could post straight past the warning.")
	}
	// AND THE ZONE IS ON THE PAGE. This screen is a person typing a wall clock and a
	// server deciding which instant that is; not naming the clock invites the one
	// mistake the flow cannot take back.
	if !strings.Contains(body, "Europe/Malta") {
		t.Error("the form does not say which clock the time is read in")
	}
}

// TestManualEntryReview_ShowsTheRecordAndMintsAConfirmationBoundToIt.
//
// 🔴 THE BINDING IS THE POINT, AND IT IS THE ONE THING THIS GATE DOES THAT NO OTHER
// GATE IN THE PANEL DOES. Every other confirmation binds a ROW's identity; this one
// binds a STATEMENT -- person, direction and instant -- because a record has no
// identity until it exists. The subtests parse the minted value back through the
// server's own verifier at a DIFFERENT statement and require a refusal.
func TestManualEntryReview_ShowsTheRecordAndMintsAConfirmationBoundToIt(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	res := b.do(http.MethodPost, manualEntryHref, url.Values{
		"id": {entryID.String()}, "direction": {"out"},
		"date": {"2026-08-05"}, "time": {"17:00"},
	})
	if res.Code != http.StatusOK {
		t.Fatalf("the review step: status %d, want 200 (it renders, it does not redirect)", res.Code)
	}
	if n := len(rec.entries()); n != 0 {
		t.Fatalf("the review step wrote %d record(s); it writes none -- that is what makes "+
			"a browser refresh of it harmless", n)
	}
	body := htmlOf(t, res)
	for _, want := range []string{
		"Wednesday 5 August 2026", // the weekday, which is what a manager can check
		"Record it",
		"cannot be edited or removed",
		"shorten the shift but never",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the confirmation screen does not say %q, so the manager is asked to "+
				"press a permanent button without being told what it costs", want)
		}
	}
	token := confirmTokenIn(t, body)
	at := maltaAt(t, "2026-08-05", "17:00")

	cases := []struct {
		name    string
		subject string
		wantOK  bool
	}{
		{"the record it was minted for", manualConfirmSubject(entryID, manual.Out, at), true},
		{"the other direction", manualConfirmSubject(entryID, manual.In, at), false},
		{"one hour later", manualConfirmSubject(entryID, manual.Out, at.Add(time.Hour)), false},
		{"one minute later", manualConfirmSubject(entryID, manual.Out, at.Add(time.Minute)), false},
		{"another person", manualConfirmSubject(uuid.New(), manual.Out, at), false},
	}
	confirm, err := newAdminConfirm(adminTestConfig())
	if err != nil {
		t.Fatalf("newAdminConfirm: %v", err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := confirm.parse(token, confirmActionRecordManual, c.subject, panelTestSession)
			if got := err == nil; got != c.wantOK {
				t.Errorf("confirmation verified=%v for %s (err=%v), want %v -- a confirmation "+
					"that verifies for a different statement is a warning served about one "+
					"record and spent on another", got, c.name, err, c.wantOK)
			}
		})
	}
	// AND IT IS BOUND TO THE ACT AS WELL. A value minted to deactivate somebody must
	// not open this gate; adminConfirm's part 12 exists because an audit spent one
	// gate's value at another's.
	if err := confirm.parse(token, confirmActionDeactivate, entryID.String(), panelTestSession); err == nil {
		t.Error("a manual-entry confirmation verifies at the DEACTIVATION gate")
	}
}

// TestManualEntryRecord_RefusesWithoutTheConfirmationAndWritesNothing.
//
// Section 4.6's question about a refusal is "what is the manager told, and what is
// left in the database". Both halves are asserted: the word, and the count.
func TestManualEntryRecord_RefusesWithoutTheConfirmationAndWritesNothing(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	res := b.do(http.MethodPost, manualRecordHref, url.Values{
		"id": {entryID.String()}, "direction": {"out"},
		"date": {"2026-08-05"}, "time": {"17:00"},
	})
	// 🔴 IT RE-RENDERS (200) RATHER THAN REDIRECTING, AND THE CHANGE WAS MEASURED
	// INTO EXISTENCE. A 303 here threw away the note, the date and the time -- they do
	// not travel in a query string (§4.7) -- so a manager whose ten-minute confirmation
	// expired got a blank form. The sibling test drives that scenario; this one holds
	// the two things that must be true either way.
	if res.Code != http.StatusOK {
		t.Fatalf("an unconfirmed write: status %d, want 200 with the form re-rendered", res.Code)
	}
	page := htmlOf(t, res)
	if !strings.Contains(page, htmlText("the record was not confirmed")) {
		t.Errorf("the manager is not told the confirmation was what was missing")
	}
	if n := len(rec.entries()); n != 0 {
		t.Fatalf("an unconfirmed write appended %d record(s) to an immutable table", n)
	}
}

// TestManualEntryRecord_WritesWhatTheFormSaidAndLandsOnTheRecordItself.
//
// 🔴 THE DESTINATION IS ASSERTED AS WELL AS THE WRITE, and it is not decoration. M6-04
// shipped a banner printed from a query string alone and an audit produced the URL
// with zero rows behind it. This flow makes no claim at all: it lands on the
// transactions section, filtered to the record's own local day and to the manual
// channel, so what the manager sees is the row.
func TestManualEntryRecord_WritesWhatTheFormSaidAndLandsOnTheRecordItself(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	form := confirmedEntry(t, b, "out", "2026-08-05", "17:00", "phone was flat")
	res := b.do(http.MethodPost, manualRecordHref, form)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("the write: status %d, want 303 -- a rendered POST means a refresh "+
			"appends a second permanent record", res.Code)
	}
	entries := rec.entries()
	if len(entries) != 1 {
		t.Fatalf("the write produced %d record(s), want 1", len(entries))
	}
	e := entries[0]
	if e.Direction != manual.Out {
		t.Errorf("direction = %q, want out", e.Direction)
	}
	if e.EmployeeID != entryID {
		t.Errorf("employee = %s, want %s", e.EmployeeID, entryID)
	}
	if e.TenantID != panelTestTenant {
		t.Errorf("tenant = %s, want the SESSION's %s. A tenant that can be posted is not a "+
			"tenant boundary (section 4.5).", e.TenantID, panelTestTenant)
	}
	if e.EnteredBy != panelTestAdmin {
		t.Errorf("entered_by = %s, want the SESSION's admin %s. The author must never come "+
			"from the form: transactions.entered_by may only hold an admin_users id, and the "+
			"whole argument for verdict='ok' rests on it naming somebody answerable.",
			e.EnteredBy, panelTestAdmin)
	}
	if e.Note != "phone was flat" {
		t.Errorf("note = %q, want the manager's sentence", e.Note)
	}
	if want := maltaAt(t, "2026-08-05", "17:00"); !e.At.Equal(want) {
		t.Errorf("instant = %s, want %s -- reading the wall clock in the wrong zone puts an "+
			"hour or two on somebody's payslip", e.At.UTC(), want)
	}
	to := res.Header().Get("Location")
	if !strings.Contains(to, "date=2026-08-05") || !strings.Contains(to, "channel=manual") {
		t.Errorf("the write redirected to %q; it must land on the record's own day and the "+
			"manual channel, so the confirmation is the ROW rather than a sentence about it", to)
	}
}

// TestManualEntry_LandsOnAFilterTheSectionACCEPTS.
//
// 🔴 THE REDIRECT AND THE DROPDOWN ARE TWO REPRESENTATIONS OF ONE VOCABULARY, AND ONLY
// ONE OF THEM IS VALIDATED. parseTransactionFilter drops a channel `oneOf` does not
// recognise -- silently, which is right for a bookmark and wrong for this: the manager
// would land on an unfiltered day and reasonably conclude the record went somewhere
// else. This holds the two together at the value manualRecordReturn actually emits.
func TestManualEntry_LandsOnAFilterTheSectionACCEPTS(t *testing.T) {
	// 15:00 UTC is 17:00 in Malta, still the 5th. The conversion is asserted on the
	// LOCAL day because an instant near midnight lands on a different one.
	to := manualRecordReturn(
		manual.Recorded{At: time.Date(2026, time.August, 5, 15, 0, 0, 0, time.UTC)}, "Europe/Malta")
	u, err := url.Parse(to)
	if err != nil {
		t.Fatalf("the redirect is not a url: %v", err)
	}
	f := parseTransactionFilter(httptest.NewRequest(http.MethodGet, to, nil))
	if f.Channel != u.Query().Get("channel") {
		t.Fatalf("the section kept channel=%q from a redirect that sent %q. A dropped filter "+
			"lands the manager on the whole day and looks like the record was not written.",
			f.Channel, u.Query().Get("channel"))
	}
	if f.Channel != string(tap.ChannelManual) {
		t.Fatalf("channel = %q, want %q", f.Channel, tap.ChannelManual)
	}
	if got := u.Query().Get("date"); got != "2026-08-05" {
		t.Fatalf("date = %q, want 2026-08-05 (the record's LOCAL day)", got)
	}
	// And the day the section RESOLVED is the same one, so the filter is honoured
	// rather than merely carried.
	if got := f.Date.String(); got != "2026-08-05" {
		t.Fatalf("the section resolved the day as %q, want 2026-08-05", got)
	}
}

// TestManualEntry_RefusesEveryTimeTheDomainRefuses is the table section 4.6 asks for:
// one row per refusal, each with its own sentence, and each writing nothing.
func TestManualEntry_RefusesEveryTimeTheDomainRefuses(t *testing.T) {
	// wantSays is the SENTENCE the manager reads, not just the machine word. Two
	// refusals that render the same paragraph are one refusal with two names, which is
	// the section 4.6 defect this table exists to catch -- so every fragment below is
	// distinct and the test asserts they stay that way.
	cases := []struct {
		name        string
		direction   string
		date        string
		clock       string
		boundsErr   error
		wantProblem string
		wantSays    string
	}{
		{"no direction", "", "2026-08-05", "17:00", nil, "direction", "either a check-in or a checkout"},
		{"a direction nobody offered", "sideways", "2026-08-05", "17:00", nil, "direction", "either a check-in or a checkout"},
		{"no date", "out", "", "17:00", nil, "when", "could not read that date and time"},
		{"no time", "out", "2026-08-05", "", nil, "when", "could not read that date and time"},
		{"a date that is not one", "out", "the fifth", "17:00", nil, "when", "could not read that date and time"},
		{"a time that is not one", "out", "2026-08-05", "half five", nil, "when", "could not read that date and time"},
		{"ahead of the server's clock", "out", "2026-08-05", "17:00", manual.ErrFuture, "future", "has not happened yet"},
		{"further back than the bound", "out", "2026-08-05", "17:00", manual.ErrTooOld, "too-old", "further back than this screen accepts"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &fakeRecorder{boundsErr: c.boundsErr}
			b := panelBrowserWithRecorder(t, rec)
			res := b.do(http.MethodPost, manualEntryHref, url.Values{
				"id": {entryID.String()}, "direction": {c.direction},
				"date": {c.date}, "time": {c.clock},
			})
			if n := len(rec.entries()); n != 0 {
				t.Fatalf("a refused submission wrote %d record(s)", n)
			}
			// A refused DIRECTION is a redirect: there is no valid direction to echo
			// back into a form. The others re-render in place so the manager keeps what
			// they typed and fixes one field.
			var got string
			switch res.Code {
			case http.StatusSeeOther:
				got = res.Header().Get("Location")
			case http.StatusOK:
				got = htmlOf(t, res)
				if !strings.Contains(got, "Nothing was recorded") {
					t.Error("the refusal does not say nothing was recorded, so a manager is " +
						"left to wonder whether half a record went in")
				}
			default:
				t.Fatalf("status %d, want 303 or 200", res.Code)
			}
			if !strings.Contains(got, "problem="+c.wantProblem) &&
				!strings.Contains(got, htmlText(c.wantSays)) {
				t.Errorf("the refusal for %q names neither problem=%s nor its own sentence "+
					"(%q). Section 4.6: every refusal is a different sentence, and a shared "+
					"one sends the manager to fix the wrong field.",
					c.name, c.wantProblem, c.wantSays)
			}
			// AND NO CONFIRMATION IS MINTED FOR A RECORD THAT COULD NOT BE WRITTEN.
			if _, ok := b.cookies[adminConfirmCookieName]; ok {
				t.Error("a refused submission still minted a confirmation, so a manager could " +
					"hold a warning about a record the server would never accept")
			}
		})
	}
}

// TestManualEntry_TellsTheManagerWhenSomebodyIsDeactivated.
//
// 🔴 SAID, NOT REFUSED, AND BOTH HALVES ARE ASSERTED. Refusing would create an
// unpayable last shift with no way back -- deactivation is one-way (docs/adr/0010) and
// this table takes no UPDATE -- so the control stays and the screen carries the fact.
// A test that only checked the sentence would let the control quietly disappear.
func TestManualEntry_TellsTheManagerWhenSomebodyIsDeactivated(t *testing.T) {
	b := panelBrowserWithRecorder(t, &fakeRecorder{
		subject: manual.Subject{Name: "Maria Borg", Status: "deactivated", Zone: "Europe/Malta"},
	})
	res := b.do(http.MethodGet, manualFormHref(), nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 -- a deactivated person's last shift still has to be "+
			"payable", res.Code)
	}
	body := htmlOf(t, res)
	if !strings.Contains(body, "has been deactivated") {
		t.Error("the screen does not say the person has been deactivated")
	}
	if !strings.Contains(body, "Review this record") {
		t.Error("the screen refuses to offer the record. Nothing on the server refuses it, " +
			"so this would be a control removed for a rule that does not exist -- and the " +
			"final shift would be unpayable, permanently.")
	}

	// POSITIVE CONTROL: an active person gets no such warning, so the sentence above is
	// about the STATUS rather than about the screen always saying it.
	b2 := panelBrowserWithRecorder(t, &fakeRecorder{
		subject: manual.Subject{Name: "Maria Borg", Status: "active", Zone: "Europe/Malta"},
	})
	if body2 := htmlOf(t, b2.do(http.MethodGet, manualFormHref(), nil)); strings.Contains(body2, "has been deactivated") {
		t.Error("an ACTIVE person's form carries the deactivation warning, so the warning " +
			"says nothing about anybody")
	}
}

// TestManualEntryRoutes_TakeTheWRITINGChainAndTheFormTakesTheREADOne.
//
// 🔴 THIS IS THE TRAP IN ITS EXACT SHAPE. A POST registered in mountSections silently
// gets the READ chain: the resolver runs BEFORE the Origin check, which is the defect
// an audit measured on POST /admin/review (300 cross-origin posts spending a signed-in
// manager's whole session budget, 301 unbudgeted lookups). The test drives a
// cross-origin request at each route and requires the two POSTs to be refused and the
// GET not to be.
func TestManualEntryRoutes_TakeTheWRITINGChainAndTheFormTakesTheREADOne(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		path     string
		wantGate bool
	}{
		{"the form is a read", http.MethodGet, manualFormHref(), false},
		{"the review step mints state", http.MethodPost, manualEntryHref, true},
		{"the write appends a permanent record", http.MethodPost, manualRecordHref, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &fakeRecorder{}
			b := panelBrowserWithRecorder(t, rec)
			b.origin = "https://evil.example"
			var form url.Values
			if c.method == http.MethodPost {
				form = url.Values{"id": {entryID.String()}}
			}
			res := b.do(c.method, c.path, form)
			// THE GATE REDIRECTS RATHER THAN 403s (sameOriginGate sends /admin), so the
			// refusal is measured as the pair rather than as a status alone -- an
			// unconfirmed write also answers 303, to a different place.
			refused := res.Code == http.StatusSeeOther && res.Header().Get("Location") == "/admin"
			if refused != c.wantGate {
				t.Fatalf("cross-origin %s %s: status %d -> %q, refused=%v want %v. A POST on "+
					"the READ chain runs the session resolver before the Origin check.",
					c.method, c.path, res.Code, res.Header().Get("Location"), refused, c.wantGate)
			}
			if c.wantGate {
				// AND NOTHING WAS MINTED ON THE WAY. The status says the request stopped;
				// this says it stopped BEFORE the part that costs something.
				if _, ok := b.cookies[adminConfirmCookieName]; ok {
					t.Error("a cross-origin POST still minted a confirmation")
				}
				if n := len(rec.entries()); n != 0 {
					t.Errorf("a cross-origin POST wrote %d record(s)", n)
				}
			}
		})
	}
}

// TestManualEntryAction_HitsNoGuardrailAndIsGrantedToBothPanelRoles is the measurement
// behind the decision NOT to call the policy engine on this path.
//
// 🔴 IT EXISTS BECAUSE A COMMENT IN internal/policy NAMED THIS TASK. sys:occurred-at-
// bound's block explains that manual entry is exempt from the tap's occurred_at bound
// because it is "the separate record:manual action". That is true of the EXEMPTION and
// was, until M6-08, decorated with the word "authorized" -- which describes an
// authorisation step nothing performs. Rather than call the engine to make one word
// true, this measures exactly what calling it would buy, so the decision rests on a
// number. Two facts:
//
//	no guardrail matches record:manual   so the question falls to the baseline
//	the baseline grants it to owner AND
//	  manager, and admin_users' CHECK
//	  admits no third role                so a gate would refuse nobody today
//
// If the baseline ever narrows, this goes red and the decision is retaken. It is
// M6-07 phase B's TestReportExportAuthority shape, applied to the action whose
// exemption sentence made the same claim in stronger words.
func TestManualEntryAction_HitsNoGuardrailAndIsGrantedToBothPanelRoles(t *testing.T) {
	rails := policy.Guardrails(policy.DefaultParams())
	if len(rails) < 10 {
		t.Fatalf("the guardrail list holds %d entries; section 5 and ADR 0007 declare ten, "+
			"so this is reading the wrong thing", len(rails))
	}
	// The context is the fullest one a manual entry could ever produce: a signed-in
	// panel admin, a manual channel, and the guardrail-only field a panel request can
	// carry. Anything this does NOT set, a guardrail cannot read either.
	tenantID := panelTestTenant
	for _, role := range []string{"owner", "manager"} {
		ctx := policy.Context{
			Action:    policy.ActionRecordManual,
			Resources: []string{"*"},
			Keys: map[policy.ContextKey]any{
				policy.CtxActorRole:   role,
				policy.CtxTapChannel:  string(tap.ChannelManual),
				policy.CtxTapSunValid: false,
			},
			SessionTenantID: &tenantID,
		}
		for _, g := range rails {
			if g.Match(ctx) {
				t.Errorf("guardrail %s fires on %s for a %s. The exemption sentence in "+
					"guardrails.go assumes it does not, and a firing guardrail is TERMINAL.",
					g.Sid, policy.ActionRecordManual, role)
			}
		}
	}

	// ANTI-VACUITY: the same walk with a TAP action must fire something, or "no
	// guardrail matches" is a statement about a broken Match rather than about the
	// action. A deactivated employee tapping is section 5 row 4 and cannot fall through.
	tapCtx := policy.Context{
		Action:    policy.ActionTapRecord,
		Resources: []string{"*"},
		Keys: map[policy.ContextKey]any{
			policy.CtxEmployeeStatus: "deactivated",
			policy.CtxTapChannel:     "nfc",
			policy.CtxTapSunValid:    true,
		},
		SessionTenantID: &tenantID,
	}
	fired := false
	for _, g := range rails {
		if g.Match(tapCtx) {
			fired = true
		}
	}
	if !fired {
		t.Fatal("no guardrail fires on a DEACTIVATED employee's tap either, so this test " +
			"is measuring a broken walk rather than the action")
	}

	// And the baseline half: who may perform the action if anybody ever asks.
	granted := map[string]string{}
	statements := 0
	for _, doc := range policy.Baseline() {
		for _, st := range doc.Document.Statements {
			statements++
			carries := false
			for _, a := range st.Action {
				if a == policy.ActionRecordManual {
					carries = true
				}
			}
			if !carries || st.Effect != policy.EffectAllow {
				continue
			}
			role, _ := st.Condition[policy.OpStringEquals][policy.CtxActorRole].(string)
			granted[role] = st.Sid
		}
	}
	if statements < 5 {
		t.Fatalf("the baseline walk saw %d statement(s); it holds more", statements)
	}
	for _, role := range []string{"owner", "manager"} {
		if sid, ok := granted[role]; !ok {
			t.Errorf("the baseline does NOT grant %s to %q. A role gate on manual entry "+
				"would now refuse somebody, so this handler's reason for having none has "+
				"expired.", policy.ActionRecordManual, role)
		} else {
			t.Logf("%s -> %s (%s)", role, policy.ActionRecordManual, sid)
		}
	}
}

// TestManualNote_IsBoundedAndCleanedByTheSAMEFunctionTheReviewNoteIs.
//
// 🔴 THE THIRD COPY OF THESE THREE STEPS IS WHAT THIS TEST EXISTS TO PREVENT. A NUL
// byte cannot be stored in a PostgreSQL text column (the driver refuses the whole
// statement) and truncating by BYTE cuts a multi-byte character in half -- both were
// real 500s, both are written up at cleanBoundedText, and Maltese ċ ġ ħ ż and Turkish
// ı ş ğ make the second one ordinary rather than adversarial.
func TestManualNote_IsBoundedAndCleanedByTheSAMEFunctionTheReviewNoteIs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"a NUL byte is removed", "abc\x00def", "abcdef"},
		{"surrounding space is trimmed", "  forgot to tap out  ", "forgot to tap out"},
		{"invalid utf-8 is dropped", "Bo\xffrg", "Borg"},
		{"Maltese and Turkish survive", "Ċikku ġie, İşçi geldi", "Ċikku ġie, İşçi geldi"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := cleanBoundedText(c.in, maxManualNote)
			if got != c.want {
				t.Fatalf("cleaned to %q, want %q", got, c.want)
			}
		})
	}
	// TRUNCATION IS BY RUNE. maxManualNote+1 two-byte characters is over twice that in
	// bytes; a byte slice at the limit would leave half a rune, which the driver
	// refuses -- so the record would be LOST rather than shortened.
	long := strings.Repeat("ċ", maxManualNote+1)
	got, clipped := cleanBoundedText(long, maxManualNote)
	if !clipped {
		t.Fatal("an over-long note was not reported as clipped, so a manager would never " +
			"learn part of what they wrote was dropped")
	}
	if n := utf8.RuneCountInString(got); n != maxManualNote {
		t.Fatalf("kept %d rune(s), want %d", n, maxManualNote)
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncation produced invalid UTF-8")
	}
	// AND THE REVIEW NOTE STILL GOES THROUGH THE SAME FUNCTION. The extraction is only
	// worth anything if both callers use it; a copy left behind in review.go would make
	// this whole test a statement about one screen.
	if rn, rc := reviewNote(long); rn != got || rc != clipped {
		t.Fatalf("reviewNote and cleanBoundedText disagree (%d runes/%v vs %d runes/%v), so "+
			"there are two cleaners again", utf8.RuneCountInString(rn), rc,
			utf8.RuneCountInString(got), clipped)
	}
}

// TestManualForm_NeverRendersTheWriteFormNoMatterWhatTheViewCarries.
//
// 🔴 IT EXISTS BECAUSE A MUTATION SHOWED THE ORIGINAL CLAIM WAS ABOUT THE WRONG THING.
// manualentry.templ said the write button could not be rendered early because
// RecordAction was empty on the first step -- so setting it should have been visible.
// It was not: a mutation that put the write address on the first step left every test
// green, because the first step does not render that field at all. The barrier is the
// template's `if v.Confirming`, so this drives the template with EVERY write-side field
// populated and Confirming false, which is the only shape that can tell the two apart.
func TestManualForm_NeverRendersTheWriteFormNoMatterWhatTheViewCarries(t *testing.T) {
	v := pages.ManualEntryView{
		Action:       manualEntryHref,
		RecordAction: manualRecordHref, // the write address, deliberately present
		ConfirmField: confirmField,
		ConfirmToken: "a-token-that-should-not-be-rendered-yet",
		EmployeeID:   entryID.String(),
		Name:         "Maria Borg",
		Zone:         "Europe/Malta",
		Directions:   manualDirections,
		MaxNote:      maxManualNote,
		Confirming:   false,
	}
	var buf strings.Builder
	if err := pages.AdminManualEntry(v).Render(context.Background(), &buf); err != nil {
		t.Fatalf("rendering the form: %v", err)
	}
	body := buf.String()
	if strings.Contains(body, `action="`+manualRecordHref+`"`) {
		t.Error("the first step rendered the WRITE form even though Confirming is false. The " +
			"confirmation step is then decoration: a manager reaches an irreversible write " +
			"without the warning ever being served.")
	}
	if strings.Contains(body, v.ConfirmToken) {
		t.Error("the first step rendered the confirmation token, so the gate could be walked " +
			"past without the warning being shown")
	}
	if strings.Contains(body, "Record it") {
		t.Error("the first step rendered the write BUTTON")
	}
	// POSITIVE CONTROL: the same view WITH Confirming renders all three, so the three
	// assertions above are about the branch rather than about a template that never
	// renders them.
	v.Confirming = true
	buf.Reset()
	if err := pages.AdminManualEntry(v).Render(context.Background(), &buf); err != nil {
		t.Fatalf("rendering the confirmation: %v", err)
	}
	confirmed := buf.String()
	for _, want := range []string{`action="` + manualRecordHref + `"`, v.ConfirmToken, "Record it"} {
		if !strings.Contains(confirmed, want) {
			t.Errorf("the CONFIRMATION step does not render %q either, so the assertions "+
				"above are measuring a template that renders nothing", want)
		}
	}
}

// TestManualEntry_IsREACHABLEFromTheRoster.
//
// 🔴 IT EXISTS BECAUSE A MUTATION DELETED THE ONLY WAY IN AND EVERYTHING STAYED GREEN.
// TestEmployeesSection_EveryControlLeadsSomewhereThatExists drives every control on
// that page through the real router -- which is a net against a control that leads
// NOWHERE, and no net at all against a control that is not there. Removing the roster's
// link left the entry screen served, tested and unreachable from the panel: the M5-04
// shape (a capability delivered, approved and dead in the wired product) in its exact
// form.
//
// It asserts the link AND that the link works, because either half alone is the defect
// the other one catches.
func TestManualEntry_IsREACHABLEFromTheRoster(t *testing.T) {
	b := panelBrowserWithRecorder(t, &fakeRecorder{})
	page := htmlOf(t, b.do(http.MethodGet, employeesHref+"?manage="+entryID.String(), nil))
	want := manualEntryHref + "?id=" + entryID.String()
	if !strings.Contains(page, `href="`+want+`"`) {
		t.Fatalf("the roster's action card does not link to %q. The entry screen is then "+
			"served and unreachable, which is a capability that exists only in its tests.", want)
	}
	if !strings.Contains(page, "Enter a record by hand") {
		t.Error("the link has no label a manager could recognise")
	}
	res := b.do(http.MethodGet, want, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("following the roster's own link answered %d; the control leads nowhere", res.Code)
	}
}

// TestManualEntry_CleansTheNoteAtTheBOUNDARYNotJustInATestOfItsOwn.
//
// 🔴 A MUTATION FOUND THIS GAP TOO. TestManualNote_... exercises cleanBoundedText
// directly, so removing the call from readManualForm left it green -- and a NUL byte
// would then reach PostgreSQL, which refuses the whole statement ("invalid byte
// sequence for encoding UTF8") and turns a manager's record into a 500. §7 puts the
// validation at the BOUNDARY, so the boundary is what has to be measured.
func TestManualEntry_CleansTheNoteAtTheBOUNDARYNotJustInATestOfItsOwn(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	// A NUL byte, an over-long tail and a surrounding space, in one submission.
	hostile := "  before\x00after " + strings.Repeat("ż", maxManualNote)
	form := confirmedEntry(t, b, "out", "2026-08-05", "17:00", hostile)
	form.Set("note", hostile)
	if res := b.do(http.MethodPost, manualRecordHref, form); res.Code != http.StatusSeeOther {
		t.Fatalf("the write: status %d, want 303", res.Code)
	}
	entries := rec.entries()
	if len(entries) != 1 {
		t.Fatalf("records = %d, want 1", len(entries))
	}
	got := entries[0].Note
	if strings.Contains(got, "\x00") {
		t.Error("a NUL byte reached the domain. PostgreSQL text cannot hold one, so the " +
			"statement is refused and the manager gets a 500 instead of a record.")
	}
	if n := utf8.RuneCountInString(got); n > maxManualNote {
		t.Errorf("the note reached the domain at %d runes, over the %d bound. It lands in an "+
			"append-only table nobody can clean up, on a route a session may post to 300 times "+
			"per window.", n, maxManualNote)
	}
	if !utf8.ValidString(got) {
		t.Error("the note reached the domain as invalid UTF-8")
	}
	if strings.HasPrefix(got, " ") || strings.HasSuffix(got, " ") {
		t.Errorf("the note kept its surrounding space: %q", got)
	}
}

// TestManualEntry_ShowsTheCLIPPEDNoteBeforeAnythingIsWritten.
//
// 🔴 M6-04's RULE IS THAT SILENCE ABOUT A TRUNCATION IS A DEFECT, and readManualForm
// discards the clip flag -- so the obligation has to be discharged some other way or
// not at all. It is: the confirmation screen renders the CLEANED note back before the
// write, so a manager reads exactly what will be stored. This asserts the mechanism
// rather than trusting the comment that describes it.
func TestManualEntry_ShowsTheCLIPPEDNoteBeforeAnythingIsWritten(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	long := strings.Repeat("z", maxManualNote) + "THIS-TAIL-IS-CUT"
	page := htmlOf(t, b.do(http.MethodPost, manualEntryHref, url.Values{
		"id": {entryID.String()}, "direction": {"out"},
		"date": {"2026-08-05"}, "time": {"17:00"}, "note": {long},
	}))
	if strings.Contains(page, "THIS-TAIL-IS-CUT") {
		t.Fatal("the confirmation screen shows the note the manager typed rather than the " +
			"one that will be stored, so the cut is invisible until it is permanent")
	}
	// 🔴 THE HIDDEN FIELD DOES NOT COUNT, AND A MUTATION IS WHY. The first version of
	// this assertion searched the whole page, and the confirm form carries the note in
	// an <input type="hidden"> that the manager cannot read -- so deleting the VISIBLE
	// paragraph left it green. Every <input> is stripped before looking.
	visible := stripInputs(page)
	if !strings.Contains(visible, strings.Repeat("z", 50)) {
		t.Fatal("the confirmation screen does not SHOW the note -- a hidden field is not " +
			"showing it. Discarding the clip flag is only defensible because this screen " +
			"shows a manager what will be written.")
	}
	if n := len(rec.entries()); n != 0 {
		t.Fatalf("the review step wrote %d record(s)", n)
	}
}

// stripInputs removes every <input ...> element, so an assertion about what a manager
// can READ is not satisfied by a hidden field carrying the same text.
func stripInputs(page string) string {
	var out strings.Builder
	for {
		i := strings.Index(page, "<input")
		if i < 0 {
			out.WriteString(page)
			return out.String()
		}
		out.WriteString(page[:i])
		j := strings.Index(page[i:], ">")
		if j < 0 {
			return out.String()
		}
		page = page[i+j+1:]
	}
}

// TestManualEntryConfirm_SaysBOTHMeasuredConsequencesAndOnlyWhereTheyApply.
//
// 🔴 THE SCREEN OWES A MANAGER TWO SENTENCES, NOT ONE, and until an audit measured it
// there was only the money one. ADR 0011: a later record can only SHORTEN a shift, AND
// correcting a CHECK-IN leaves an entry that no later checkout can ever close. A
// manager told only the first would keep pressing at the second.
//
// THE SECOND SENTENCE IS CONDITIONAL, so the test drives BOTH directions — a warning
// that appears everywhere is a warning nobody reads, and one that appears nowhere is
// the defect.
func TestManualEntryConfirm_SaysBOTHMeasuredConsequencesAndOnlyWhereTheyApply(t *testing.T) {
	const (
		money       = "shorten the shift but never"
		uncloseable = "nothing you enter afterwards can close it"
	)
	cases := []struct {
		direction   string
		wantUnclose bool
	}{
		{"in", true},
		{"out", false},
	}
	for _, c := range cases {
		t.Run(c.direction, func(t *testing.T) {
			b := panelBrowserWithRecorder(t, &fakeRecorder{})
			page := htmlOf(t, b.do(http.MethodPost, manualEntryHref, url.Values{
				"id": {entryID.String()}, "direction": {c.direction},
				"date": {"2026-08-05"}, "time": {"17:00"},
			}))
			if !strings.Contains(page, money) {
				t.Error("the confirmation does not carry the measured MONEY consequence " +
					"(ADR 0011: a later record can only shorten)")
			}
			if got := strings.Contains(page, uncloseable); got != c.wantUnclose {
				t.Errorf("the uncloseable-entry warning present=%v, want %v for a %q. It is "+
					"specific to a check-in: correcting one leaves an entry the report can "+
					"never close, and every attempt writes another permanent row.",
					got, c.wantUnclose, c.direction)
			}
		})
	}
}

// TestManualEntryRecord_KeepsWhatTheManagerTypedWhenTheConfirmationHasEXPIRED.
//
// 🔴 THE SCENARIO IS ORDINARY AND WAS MEASURED AS A LOSS. Fill the form, read the two
// warnings, get interrupted for ten minutes, press "Record it" — the confirmation has
// expired, and the flow used to answer with a 303 to a BLANK form. The note, the date
// and the time deliberately do not travel in a query string (§4.7), so all three went
// with it: the manager is punished for reading slowly.
//
// The expiry is driven by moving the SERVER's clock inside the signed payload rather
// than by sleeping, which is what makes this a test rather than an eleven-minute wait.
func TestManualEntryRecord_KeepsWhatTheManagerTypedWhenTheConfirmationHasEXPIRED(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	const note = "phone was flat, Ċikku confirmed the hours"
	form := confirmedEntry(t, b, "out", "2026-08-05", "17:00", note)

	// EXPIRE IT. A value minted more than adminConfirmTTL ago is refused by parse, and
	// building it here (with the server's own key) is the only way to reach the branch
	// without waiting for the clock.
	stale, err := staleConfirmation(t, entryID, manual.Out, maltaAt(t, "2026-08-05", "17:00"))
	if err != nil {
		t.Fatalf("minting a stale confirmation: %v", err)
	}
	form.Set(confirmField, stale)
	b.cookies[adminConfirmCookieName] = stale

	res := b.do(http.MethodPost, manualRecordHref, form)
	if res.Code != http.StatusOK {
		t.Fatalf("an expired confirmation answered %d; it must re-render the form (200) "+
			"rather than redirect to an empty one", res.Code)
	}
	if n := len(rec.entries()); n != 0 {
		t.Fatalf("an expired confirmation wrote %d record(s)", n)
	}
	page := htmlOf(t, res)
	for _, want := range []string{
		htmlText(note),       // the sentence they wrote
		`value="2026-08-05"`, // the date
		`value="17:00"`,      // the time
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the re-rendered form lost %q. A manager who read the warning too "+
				"slowly has to type everything again, which is the cost of the redirect this "+
				"branch used to answer with.", want)
		}
	}
	if !strings.Contains(page, htmlText("Nothing was recorded")) {
		t.Error("the re-rendered form does not say nothing was recorded")
	}
	// AND THE DIRECTION IS STILL SELECTED, so "keeps what they typed" covers all four
	// fields rather than three.
	if !strings.Contains(page, `value="out" checked`) {
		t.Error("the re-rendered form lost the chosen direction")
	}
}

// staleConfirmation mints a genuine confirmation stamped eleven minutes ago, using the
// server's own key. It is the only way to reach the expiry branch without sleeping past
// adminConfirmTTL.
func staleConfirmation(t *testing.T, employeeID uuid.UUID, d manual.Direction, at time.Time) (string, error) {
	t.Helper()
	c, err := newAdminConfirm(adminTestConfig())
	if err != nil {
		return "", err
	}
	c.now = func() time.Time { return time.Now().Add(-(adminConfirmTTL + time.Minute)) }
	return c.mint(confirmActionRecordManual, manualConfirmSubject(employeeID, d, at), panelTestSession)
}

// TestManualEntry_AWriteFailureSaysNothingWasWrittenRatherThanNothingIsSHOWING.
//
// 🔴 THE SHIPPED SENTENCE DESCRIBES A SCREEN, AND THE MANAGER WAS PRESSING A BUTTON.
// problemPanelUnavailable says "this page is not showing anything", which is true of a
// failed READ and meaningless to somebody whose write just failed: the two facts they
// need are whether it happened and whether pressing again is safe. This task converts
// its own call sites; the rest are COUNTED BY A CENSUS rather than by a number in a
// comment — see the sibling test below, which derives them every run.
func TestManualEntry_AWriteFailureSaysNothingWasWrittenRatherThanNothingIsSHOWING(t *testing.T) {
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	form := confirmedEntry(t, b, "out", "2026-08-05", "17:00", "")
	// The domain fails with something that is NOT one of the refusals the screen
	// explains, which is the only way to reach the 500 branch.
	rec.recordErr = errors.New("the database is unreachable")

	res := b.do(http.MethodPost, manualRecordHref, form)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("a database failure answered %d, want 500 -- redirecting it into a "+
			"refusal would tell a manager their record was rejected when nothing was asked",
			res.Code)
	}
	page := htmlOf(t, res)
	if !strings.Contains(page, htmlText("nothing was written")) {
		t.Error("the failure page does not say nothing was written, which is the first " +
			"thing somebody who pressed a permanent button needs to know")
	}
	if strings.Contains(page, htmlText("this page is not showing anything")) {
		t.Error("the failure page tells somebody who was WRITING that a page they were " +
			"not reading is empty")
	}
	if n := len(rec.entries()); n != 0 {
		t.Errorf("%d record(s) were written despite the failure", n)
	}

	// POSITIVE CONTROL: the READ path keeps the reader's sentence, so the two are not
	// one message with two names.
	b2 := panelBrowserWithRecorder(t, &fakeRecorder{subjectErr: errors.New("unreachable")})
	readPage := htmlOf(t, b2.do(http.MethodGet, manualFormHref(), nil))
	if !strings.Contains(readPage, htmlText("this page is not showing anything")) {
		t.Error("the READ path lost the reader's sentence, so the split bought nothing")
	}
}

// TestManualEntryForm_UsesTheBrandAccentOnItsNativeControls.
//
// 🔴 A NATIVE RADIO PAINTS ITSELF THE USER AGENT'S BLUE, which is the one colour this
// product does not use (skill tappa-brand: stay inside the palette, no invented hex).
// The activation screen already fixed the same class on its own checkboxes, so this is
// the second instance rather than a new rule — and it was MISSING here until an audit
// looked at the rendered page rather than at the palette file.
//
// It reads the ATTRIBUTE ON THE CONTROL rather than searching the page, because
// `accent-tappa-green` appears in the stylesheet for another screen's sake and a page-
// wide match would pass on a control that carries nothing.
func TestManualEntryForm_UsesTheBrandAccentOnItsNativeControls(t *testing.T) {
	b := panelBrowserWithRecorder(t, &fakeRecorder{})
	page := htmlOf(t, b.do(http.MethodGet, manualFormHref(), nil))
	radios := 0
	for _, tag := range strings.Split(page, "<input") {
		if !strings.Contains(strings.SplitN(tag, ">", 2)[0], `type="radio"`) {
			continue
		}
		radios++
		if !strings.Contains(strings.SplitN(tag, ">", 2)[0], "accent-tappa-green") {
			t.Errorf("a radio on this screen carries no brand accent, so the browser paints "+
				"it its own blue: %s", strings.SplitN(tag, ">", 2)[0])
		}
	}
	// ANTI-VACUITY: a page with no radios would pass the loop above forever. The
	// direction control offers exactly the two members of the closed vocabulary.
	if radios != len(manualDirections) {
		t.Fatalf("found %d radio(s), want %d — this test is reading the wrong page",
			radios, len(manualDirections))
	}
}

// TestManualEntryRecord_ARefreshOfARefusedWriteCannotAppendASecondRecord.
//
// 🔴 THE PROPERTY THE OLD "EVERY OUTCOME ANSWERS 303" SENTENCE WAS REALLY ABOUT. Five
// branches render (200) since the round that stopped throwing away a manager's input,
// so a browser refresh RE-POSTS them -- and the question "can that write a second
// permanent row" stopped being answered by the status code. It is answered by WHERE
// the confirmation cookie is cleared, and the two refusal shapes get there differently:
//
//	a refused confirmation  returns before Record and WITHOUT clearing the cookie, so
//	                        the re-post is refused by the same gate
//	a domain refusal        happens AFTER the clear, so the re-post is refused as
//	                        `confirm-required` -- the mint is spent though no record
//	                        exists, which is the safe direction
//
// Either way the count of written records stays where it was, which is what the test
// asserts. A status-code assertion could not see any of this.
func TestManualEntryRecord_ARefreshOfARefusedWriteCannotAppendASecondRecord(t *testing.T) {
	cases := []struct {
		name string
		// dropConfirmation reaches the branch that returns BEFORE Record and WITHOUT
		// clearing the cookie; recordErr reaches the ones AFTER the clear. They are
		// separate fields rather than inferred from the name, which is how the first
		// version of this table quietly drove one shape twice.
		dropConfirmation bool
		recordErr        error
	}{
		{"a refused confirmation (the gate returns before Record)", true, nil},
		{"a domain refusal (the cookie has already been cleared)", false, manual.ErrTooOld},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &fakeRecorder{}
			b := panelBrowserWithRecorder(t, rec)
			form := confirmedEntry(t, b, "out", "2026-08-05", "17:00", "phone was flat")
			if c.dropConfirmation {
				// THE CONFIRMATION IS THROWN AWAY, which is the expiry/never-confirmed
				// shape. The cookie goes with it so the browser is in the same state a
				// real one would be.
				form.Del(confirmField)
				delete(b.cookies, adminConfirmCookieName)
			}
			rec.recordErr = c.recordErr

			first := b.do(http.MethodPost, manualRecordHref, form)
			if first.Code != http.StatusOK {
				t.Fatalf("the refusal answered %d, want 200 with the form re-rendered", first.Code)
			}
			after := len(rec.entries())
			if after != 0 {
				t.Fatalf("the refused write appended %d record(s)", after)
			}

			// 🔴 THE REFRESH. Exactly the same body, exactly the same cookie jar -- which
			// is what a browser does on F5 over a rendered POST.
			second := b.do(http.MethodPost, manualRecordHref, form)
			if n := len(rec.entries()); n != 0 {
				t.Fatalf("re-posting the refused write appended %d record(s) to an "+
					"immutable table; a rendered POST is only safe while this stays 0", n)
			}
			page := htmlOf(t, second)
			if !strings.Contains(page, htmlText("Nothing was recorded")) {
				t.Errorf("the re-post does not say nothing was recorded (status %d)", second.Code)
			}
		})
	}

	// POSITIVE CONTROL: the same form, CONFIRMED, does write exactly one record — so
	// the zeros above are about the refusal rather than about a flow that never works.
	rec := &fakeRecorder{}
	b := panelBrowserWithRecorder(t, rec)
	form := confirmedEntry(t, b, "out", "2026-08-05", "17:00", "phone was flat")
	if res := b.do(http.MethodPost, manualRecordHref, form); res.Code != http.StatusSeeOther {
		t.Fatalf("the confirmed write answered %d, want 303", res.Code)
	}
	if n := len(rec.entries()); n != 1 {
		t.Fatalf("the confirmed write produced %d record(s), want 1", n)
	}
}

// TestPanelProblemPages_CountTheWriteRoutesStillTellingReadersTheirPageIsEmpty is the
// CENSUS that replaced three hand-written integers.
//
// 🔴 IT EXISTS BECAUSE THE HAND-WRITTEN ONE WAS WRONG THE FIRST TIME IT WAS WRITTEN.
// A grep for `problemPanelUnavailable` counts the mentions inside comments too, so the
// delivery said 33 where the real figure is smaller; an audit caught it. A number
// describing a set the code owns belongs to a walk over that code, which is the same
// rule mountWriting's comment now follows after drifting three times.
//
// IT ASSERTS ALMOST NOTHING ON PURPOSE. The debt is pre-existing and belongs to the
// screens that own those handlers (M6-04/05/06); what this holds is that THIS task's
// own handlers are not part of it, and that the census is not silently reading nothing.
func TestPanelProblemPages_CountTheWriteRoutesStillTellingReadersTheirPageIsEmpty(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing internal/handler: %v", err)
	}

	// THE WRITE ROUTES ARE DERIVED FROM mountWriting ITSELF, so a route added there is
	// classified without anybody editing this test.
	writers := map[string]bool{}
	var census []string
	unavailable, writeFailed := 0, 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok || fn.Name.Name != "mountWriting" {
					return true
				}
				ast.Inspect(fn, func(m ast.Node) bool {
					call, ok := m.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Post" || len(call.Args) != 2 {
						return true
					}
					if h, ok := call.Args[1].(*ast.SelectorExpr); ok {
						writers[h.Sel.Name] = true
					}
					return true
				})
				return false
			})
		}
	}
	if len(writers) < 5 {
		t.Fatalf("derived %d write route handler(s) from mountWriting; it registers more, "+
			"so this census is reading the wrong function", len(writers))
	}

	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok {
					return true
				}
				ast.Inspect(fn, func(m ast.Node) bool {
					id, ok := m.(*ast.Ident)
					if !ok {
						return true
					}
					switch id.Name {
					case "problemPanelUnavailable":
						unavailable++
						if writers[fn.Name.Name] {
							census = append(census, fn.Name.Name+" ("+name+")")
						}
					case "problemPanelWriteFailed":
						writeFailed++
					}
					return true
				})
				return false
			})
		}
	}
	// ANTI-VACUITY: a walk that found neither identifier would report a clean panel.
	if unavailable < 10 || writeFailed < 2 {
		t.Fatalf("the census saw %d use(s) of the reader's problem page and %d of the "+
			"writer's; both are larger, so this walk is reading the wrong package",
			unavailable, writeFailed)
	}
	t.Logf("panel problem pages: %d uses of problemPanelUnavailable (%d of them inside a "+
		"handler mountWriting registers), %d uses of problemPanelWriteFailed.\n"+
		"still telling a writer their page is empty: %s",
		unavailable, len(census), writeFailed, strings.Join(census, ", "))

	// 🔴 THE ONE ASSERTION: THIS TASK'S HANDLERS ARE NOT ON THAT LIST. The rest is
	// somebody else's screen and is printed rather than failed.
	for _, entry := range census {
		if strings.HasPrefix(entry, "manualEntry") {
			t.Errorf("%s still shows a WRITER the reader's problem page", entry)
		}
	}
}
