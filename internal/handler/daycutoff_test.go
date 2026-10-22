package handler

// daycutoff_test.go -- M15-07's panel side over fakes: the two presses (the first writes
// nothing), the owner gate on the business level, the closed words, the review page's
// sentences, the status line's two modes, the person card's source line, and the move that
// changes a person's business day. What the domain reads and writes is measured against real
// Postgres in internal/domain/tenant/daycutoff_db_test.go and here in daycutoff_db_test.go.

import (
	"context"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/components"
)

// fakeBusinessDays records what the panel asked and answers what the test set.
type fakeBusinessDays struct {
	mu        sync.Mutex
	review    tenant.CutoffReview
	reviewErr error
	setErr    error
	person    tenant.PersonCutoff
	personErr error
	move      tenant.MoveCutoff
	moveErr   error
	sw        tenant.BusinessDaySwitch

	reviews []tenant.CutoffReviewCommand
	sets    []tenant.SetCutoffCommand
	moves   []tenant.MoveCutoffCommand
}

func (f *fakeBusinessDays) Review(_ context.Context, c tenant.CutoffReviewCommand) (tenant.CutoffReview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviews = append(f.reviews, c)
	if f.reviewErr != nil {
		return tenant.CutoffReview{}, f.reviewErr
	}
	r := f.review
	r.Level = c.Level
	if c.Unchanged {
		r.Proposed = r.Stored
	} else {
		r.Proposed = c.Value
	}
	return r, nil
}

func (f *fakeBusinessDays) Set(_ context.Context, c tenant.SetCutoffCommand) (tenant.CutoffChange, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets = append(f.sets, c)
	return tenant.CutoffChange{To: c.Value}, f.setErr
}

func (f *fakeBusinessDays) Person(_ context.Context, _, _ uuid.UUID) (tenant.PersonCutoff, error) {
	return f.person, f.personErr
}

func (f *fakeBusinessDays) Move(_ context.Context, c tenant.MoveCutoffCommand) (tenant.MoveCutoff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, c)
	return f.move, f.moveErr
}

func (f *fakeBusinessDays) Switch(time.Time) tenant.BusinessDaySwitch { return f.sw }

func (f *fakeBusinessDays) counts() (reviews, sets, moves int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reviews), len(f.sets), len(f.moves)
}

// dayBrowser is the panel with a session of the given role and the business-day service.
func dayBrowser(t *testing.T, role string, days *fakeBusinessDays, staff *fakeStaff, trail *fakeTrail) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "KF Admin",
		}, nil
	}}
	var opts []AdminAuthOption
	if days != nil {
		opts = append(opts, BusinessDays(days))
	}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		staff, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(),
		nil, &fakeNotices{}, adminTestConfig(), slog.New(slog.DiscardHandler), opts...)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

func cutoffAt(h, m int) *time.Duration {
	d := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
	return &d
}

var pastryConflict = tenant.CutoffConflict{
	Shift:  tenant.CutoffShift{Owner: "KM Pastry & Bakery", Start: 4 * time.Hour, End: 12 * time.Hour},
	Cutoff: 5 * time.Hour,
}

var hiddenRE = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)"`)

// formOf reads the hidden fields of the first form posting to action.
func formOf(t *testing.T, body, action string) url.Values {
	t.Helper()
	i := strings.Index(body, `action="`+action+`"`)
	if i < 0 {
		t.Fatalf("no form posts to %s:\n%s", action, body)
	}
	rest := body[i:]
	if j := strings.Index(rest, "</form>"); j >= 0 {
		rest = rest[:j]
	}
	out := url.Values{}
	for _, m := range hiddenRE.FindAllStringSubmatch(rest, -1) {
		out.Add(html.UnescapeString(m[1]), html.UnescapeString(m[2]))
	}
	return out
}

// TestBusinessDay_TheFirstPressWritesNothingAndTheConfirmedOneWritesOnce: the review press
// answers 200 with the review -- the conflict sentence, the suggestion, the open check-ins,
// the status -- mints the confirmation and calls Set ZERO times; the review's own form, posted
// back, calls Set exactly once with the session's tenant and actor (posted ones are ignored)
// and lands on the block with day=saved. Posting the apply route again with the cleared
// cookie writes nothing.
func TestBusinessDay_TheFirstPressWritesNothingAndTheConfirmedOneWritesOnce(t *testing.T) {
	venue := uuid.New()
	days := &fakeBusinessDays{review: tenant.CutoffReview{
		SubjectID: venue, SubjectName: "KM Plant", Zone: "Europe/Malta", Business: 5 * time.Hour,
		People: 4, Shifts: 2, Conflicts: []tenant.CutoffConflict{pastryConflict}, Suggested: cutoffAt(23, 0), OpenSplit: 3,
	}}
	b := dayBrowser(t, adminRoleOwner, days, &fakeStaff{}, &fakeTrail{})
	rec := b.do(http.MethodPost, businessDayHref, url.Values{
		"level": {"branch"}, "id": {venue.String()}, "cutoff": {"05:00"},
		"tenant_id": {uuid.NewString()}, "actor_id": {uuid.NewString()},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("the review answered %d, want 200", rec.Code)
	}
	body := htmlOf(t, rec)
	for _, want := range []string{
		"05:00 falls inside KM Pastry &amp; Bakery&#39;s shift (04:00–12:00): a 03:58 clock-in would count for the previous day and the 12:05 clock-out would be recorded as a clock-in.",
		"Suggested: 23:00 — outside every shift.",
		"Changing it now would also split 3 check-ins that are open right now.",
		"Not active yet — the business day starts applying when Taptime switches it on",
		"Review 23:00 instead",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the review does not say %q", want)
		}
	}
	if _, sets, _ := days.counts(); sets != 0 {
		t.Fatalf("the first press called Set %d time(s)", sets)
	}
	if b.cookies[adminConfirmCookieName] == "" {
		t.Fatal("the review minted no confirmation cookie")
	}
	form := formOf(t, body, businessDayApplyHref)
	if form.Get(confirmField) == "" || form.Get("cutoff") != "05:00" || form.Get("level") != "branch" || form.Get("id") != venue.String() {
		t.Fatalf("the review's form carries %v", form)
	}
	rec = b.do(http.MethodPost, businessDayApplyHref, form)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/locations?day=saved&venue="+venue.String()+"#business-day" {
		t.Fatalf("the confirmed press answered %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if _, sets, _ := days.counts(); sets != 1 {
		t.Fatalf("the confirmed press called Set %d time(s), want 1", sets)
	}
	got := days.sets[0]
	if got.TenantID != panelTestTenant || got.ActorID != panelTestAdmin || got.Level != tenant.CutoffBranch ||
		got.SubjectID != venue || got.Value == nil || *got.Value != 5*time.Hour {
		t.Fatalf("Set got %+v", got)
	}
	// The cookie was cleared on the way in: the same form again is refused and writes nothing.
	rec = b.do(http.MethodPost, businessDayApplyHref, form)
	if !strings.Contains(rec.Header().Get("Location"), "day=confirm-required") {
		t.Fatalf("a second press answered %q", rec.Header().Get("Location"))
	}
	if _, sets, _ := days.counts(); sets != 1 {
		t.Fatalf("a second press called Set (%d)", sets)
	}
}

// TestBusinessDay_TheConfirmationIsBoundToTheValueAndTheLevel: a confirmation served for
// 05:00 does not write 06:00, nor the same value at another level or subject; an apply with
// no confirmation at all writes nothing. Each is a closed word at the block.
func TestBusinessDay_TheConfirmationIsBoundToTheValueAndTheLevel(t *testing.T) {
	venue := uuid.New()
	days := &fakeBusinessDays{review: tenant.CutoffReview{SubjectID: venue, Zone: "Europe/Malta"}}
	b := dayBrowser(t, adminRoleOwner, days, &fakeStaff{}, &fakeTrail{})
	if rec := b.do(http.MethodPost, businessDayApplyHref, url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {"05:00"}}); !strings.Contains(rec.Header().Get("Location"), "day=confirm-required") {
		t.Fatalf("an apply with no review answered %q", rec.Header().Get("Location"))
	}
	for _, change := range []func(url.Values){
		func(v url.Values) { v.Set("cutoff", "06:00") },
		func(v url.Values) { v.Set("level", "department") },
		func(v url.Values) { v.Set("id", uuid.NewString()) },
	} {
		rec := b.do(http.MethodPost, businessDayHref, url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {"05:00"}})
		form := formOf(t, htmlOf(t, rec), businessDayApplyHref)
		change(form)
		rec = b.do(http.MethodPost, businessDayApplyHref, form)
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "day=confirm-") {
			t.Errorf("a changed statement answered %q", loc)
		}
	}
	if _, sets, _ := days.counts(); sets != 0 {
		t.Fatalf("Set was called %d time(s) without a matching confirmation", sets)
	}
}

// TestBusinessDay_TheBusinessLevelIsTheOwnersAlone: a manager's review and apply at the
// business level reach no domain read and no write, are told not-permitted on Account, and
// each leaves one refusal row; a manager may review a branch, a department and a person
// (who may save those today), and the owner the business.
func TestBusinessDay_TheBusinessLevelIsTheOwnersAlone(t *testing.T) {
	days := &fakeBusinessDays{review: tenant.CutoffReview{Zone: "Europe/Malta", Stored: cutoffAt(5, 0)}}
	trail := &fakeTrail{}
	b := dayBrowser(t, "manager", days, &fakeStaff{}, trail)
	for _, route := range []string{businessDayHref, businessDayApplyHref} {
		rec := b.do(http.MethodPost, route, url.Values{"level": {"business"}, "cutoff": {"23:00"}})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != accountReturn("", "not-permitted") {
			t.Fatalf("a manager's %s answered %d -> %q", route, rec.Code, rec.Header().Get("Location"))
		}
	}
	if reviews, sets, _ := days.counts(); reviews != 0 || sets != 0 {
		t.Fatalf("a manager reached the domain: %d review(s), %d set(s)", reviews, sets)
	}
	if n := trail.count(ActionBusinessDayRefused); n != 2 {
		t.Fatalf("%d refusal row(s), want 2", n)
	}
	for _, level := range []string{"branch", "department", "person"} {
		if rec := b.do(http.MethodPost, businessDayHref, url.Values{"level": {level}, "id": {uuid.NewString()}, "cutoff": {"23:00"}}); rec.Code != http.StatusOK {
			t.Errorf("a manager's %s review answered %d", level, rec.Code)
		}
	}
	owner := dayBrowser(t, adminRoleOwner, days, &fakeStaff{}, &fakeTrail{})
	if rec := owner.do(http.MethodPost, businessDayHref, url.Values{"level": {"business"}, "cutoff": {"23:00"}}); rec.Code != http.StatusOK {
		t.Fatalf("the owner's business review answered %d", rec.Code)
	}
}

// TestBusinessDay_EveryRefusalIsAWord: a time that is not a whole-minute time of day is
// refused before any read; an empty business value, a value already on file and each
// domain refusal land on a closed word; an unreadable level goes to Account.
func TestBusinessDay_EveryRefusalIsAWord(t *testing.T) {
	venue := uuid.New()
	for _, c := range []struct {
		name   string
		form   url.Values
		err    error
		review tenant.CutoffReview
		want   string
	}{
		{"not a time", url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {"25:00"}}, nil, tenant.CutoffReview{}, "day=bad-time"},
		{"seconds", url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {"05:00:30"}}, nil, tenant.CutoffReview{}, "day=bad-time"},
		{"the value on file", url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {"06:00"}}, nil, tenant.CutoffReview{Stored: cutoffAt(6, 0)}, "day=same"},
		{"empty business", url.Values{"level": {"business"}, "cutoff": {""}}, tenant.ErrDayCutoffRequired, tenant.CutoffReview{}, "day=required"},
		{"foreign venue", url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {"06:00"}}, tenant.ErrUnknownVenue, tenant.CutoffReview{}, "problem=unknown-venue"},
		{"foreign department", url.Values{"level": {"department"}, "id": {venue.String()}, "cutoff": {"06:00"}}, tenant.ErrUnknownDepartment, tenant.CutoffReview{}, "problem=unknown-department"},
		{"foreign person", url.Values{"level": {"person"}, "id": {venue.String()}, "cutoff": {"06:00"}}, tenant.ErrUnknownEmployee, tenant.CutoffReview{}, "problem=unknown"},
		{"no such level", url.Values{"level": {"tenant"}, "cutoff": {"06:00"}}, nil, tenant.CutoffReview{}, "problem=unreadable"},
		{"no subject", url.Values{"level": {"branch"}, "cutoff": {"06:00"}}, nil, tenant.CutoffReview{}, "problem=unreadable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			days := &fakeBusinessDays{review: c.review, reviewErr: c.err}
			rec := dayBrowser(t, adminRoleOwner, days, &fakeStaff{}, &fakeTrail{}).do(http.MethodPost, businessDayHref, c.form)
			if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), c.want) {
				t.Fatalf("answered %d -> %q, want %s", rec.Code, rec.Header().Get("Location"), c.want)
			}
			if _, sets, _ := days.counts(); sets != 0 {
				t.Fatalf("a refusal wrote")
			}
		})
	}
	// An empty branch value is "inherit", not 00:00.
	days := &fakeBusinessDays{review: tenant.CutoffReview{Stored: cutoffAt(6, 0), Zone: "Europe/Malta"}}
	dayBrowser(t, adminRoleOwner, days, &fakeStaff{}, &fakeTrail{}).do(http.MethodPost, businessDayHref, url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {""}})
	if len(days.reviews) != 1 || days.reviews[0].Value != nil {
		t.Fatalf("an empty branch value reached the domain as %v, want nil (inherit)", days.reviews)
	}
}

// TestBusinessDayStatus_TheTwoModes: the status line says the rule is not active while the
// switch is off or ahead, and "Active since" the switch's value in the business's zone.
func TestBusinessDayStatus_TheTwoModes(t *testing.T) {
	if got := businessDayStatus(tenant.BusinessDaySwitch{}, "Europe/Malta"); got != "Not active yet — the business day starts applying when Taptime switches it on" {
		t.Fatalf("off: %q", got)
	}
	since := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	if got := businessDayStatus(tenant.BusinessDaySwitch{Active: true, Since: since}, "Europe/Malta"); got != "Active since 21 October 2026, 05:00" {
		t.Fatalf("on: %q", got)
	}
	// And as the card renders it.
	for _, s := range []tenant.BusinessDaySwitch{{}, {Active: true, Since: since}} {
		var out strings.Builder
		v := components.DayCutoffView{Anchor: businessDayAnchor, ValueLine: "05:00 — the business setting", Status: businessDayStatus(s, "Europe/Malta")}
		if err := components.DayCutoffCard(v).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		want := "Not active yet"
		if s.Active {
			want = "Active since 21 October 2026, 05:00"
		}
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the card does not say %q:\n%s", want, out.String())
		}
	}
}

// TestBusinessDayCards_SayTheValueItsSourceAndTheStatus: the Account card (owner: a form;
// manager: the sentence, no form), and the person card's three sources -- "from <branch>",
// "from <department>", "set for this person" -- each with the status line; a panel without
// the service draws no block at all.
func TestBusinessDayCards_SayTheValueItsSourceAndTheStatus(t *testing.T) {
	since := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	days := &fakeBusinessDays{review: tenant.CutoffReview{Zone: "Europe/Malta", Business: 5 * time.Hour, Stored: cutoffAt(5, 0)},
		sw: tenant.BusinessDaySwitch{Active: true, Since: since}}
	owner := htmlOf(t, dayBrowser(t, adminRoleOwner, days, &fakeStaff{}, &fakeTrail{}).do(http.MethodGet, accountHref, nil))
	for _, want := range []string{"Business day", "05:00 — the business setting", "Active since 21 October 2026, 05:00", `action="` + businessDayHref + `"`} {
		if !strings.Contains(owner, want) {
			t.Errorf("the owner's Account does not carry %q", want)
		}
	}
	manager := htmlOf(t, dayBrowser(t, "manager", days, &fakeStaff{}, &fakeTrail{}).do(http.MethodGet, accountHref, nil))
	if !strings.Contains(manager, "The business day is changed by an owner of the business.") || strings.Contains(manager, `action="`+businessDayHref+`"`) {
		t.Errorf("a manager's Account offers the business day form or omits the sentence")
	}
	if strings.Contains(htmlOf(t, dayBrowser(t, adminRoleOwner, nil, &fakeStaff{}, &fakeTrail{}).do(http.MethodGet, accountHref, nil)), "Business day starts at") {
		t.Errorf("a panel without the service drew the block")
	}

	employee := uuid.New()
	for _, c := range []struct {
		person tenant.PersonCutoff
		want   string
	}{
		{tenant.PersonCutoff{Value: 5 * time.Hour, Source: tenant.CutoffSource{Level: tenant.CutoffBranch, Name: "Sliema"}, Zone: "Europe/Malta"}, "05:00 — from Sliema"},
		{tenant.PersonCutoff{Value: 14 * time.Hour, Source: tenant.CutoffSource{Level: tenant.CutoffDepartment, Name: "KM Meat Production"}, Zone: "Europe/Malta"}, "14:00 — from KM Meat Production"},
		{tenant.PersonCutoff{Value: 5 * time.Hour, Own: cutoffAt(5, 0), Source: tenant.CutoffSource{Level: tenant.CutoffPerson}, Zone: "Europe/Malta"}, "05:00 — set for this person"},
	} {
		d := &fakeBusinessDays{person: c.person}
		body := htmlOf(t, dayBrowser(t, "manager", d, &fakeStaff{}, &fakeTrail{}).do(http.MethodGet, employeesHref+"?manage="+employee.String(), nil))
		if !strings.Contains(body, c.want) || !strings.Contains(body, "Not active yet") {
			t.Errorf("the person card does not say %q with the status line", c.want)
		}
		form := formOf(t, body, businessDayHref)
		if form.Get("level") != "person" || form.Get("id") != employee.String() {
			t.Errorf("the person card's form carries %v", form)
		}
	}
}

// TestConflictSentence_TheCardsExample is ADR 0032 §A.7's sentence, word for word, and the
// other two lines of the warning.
func TestConflictSentence_TheCardsExample(t *testing.T) {
	want := "05:00 falls inside KM Pastry & Bakery's shift (04:00–12:00): a 03:58 clock-in would count for the " +
		"previous day and the 12:05 clock-out would be recorded as a clock-in."
	if got := conflictSentence(pastryConflict); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	// A cutoff at the band's upper end: the example clock-out is the cutoff itself.
	got := conflictSentence(tenant.CutoffConflict{Shift: tenant.CutoffShift{Owner: "KF St Julians", Start: 10 * time.Hour, End: 22 * time.Hour}, Cutoff: 0})
	if !strings.Contains(got, "a 09:58 clock-in") || !strings.Contains(got, "the 00:00 clock-out") {
		t.Fatalf("upper end: %q", got)
	}
	if got := splitsSentence(3, false); got != "Changing it now would also split 3 check-ins that are open right now." {
		t.Fatalf("splits: %q", got)
	}
	if got := splitsSentence(1, false); got != "Changing it now would also split 1 check-in that is open right now." {
		t.Fatalf("splits: %q", got)
	}
	if _, s := conflictWords([]tenant.CutoffConflict{pastryConflict}, nil); s != "No time of day is outside every one of these shifts, so there is no suggestion." {
		t.Fatalf("no suggestion: %q", s)
	}
	inherit := "each person uses their branch's or the business's setting"
	if got := changeSummary("KM Pastry & Bakery", nil, cutoffAt(23, 0), inherit); got != "The business day for KM Pastry & Bakery "+
		"would start at 23:00. Today it is not set here: "+inherit+". A check-in is closed only by a clock-out of the same business day." {
		t.Fatalf("summary: %q", got)
	}
	if got := changeSummary("Sliema", cutoffAt(6, 0), nil, "uses the business setting (05:00)"); !strings.Contains(got, "would no longer be set here: uses the business setting (05:00). Today it starts at 06:00.") {
		t.Fatalf("summary of a clearing: %q", got)
	}
}

// TestEmployeeMove_AMoveThatChangesTheBusinessDayIsReviewedFirst: a move whose destination
// gives the person another cutoff answers the warning ("Business day starts 05:00 → 23:00
// after the move") and moves nobody; its confirmed press moves once. A move that keeps the
// cutoff goes straight through, as before, with no warning.
func TestEmployeeMove_AMoveThatChangesTheBusinessDayIsReviewedFirst(t *testing.T) {
	employee, venue := uuid.New(), uuid.New()
	form := url.Values{"id": {employee.String()}, "to_location": {venue.String()}}
	days := &fakeBusinessDays{move: tenant.MoveCutoff{From: 5 * time.Hour, To: 23 * time.Hour, Zone: "Europe/Malta",
		FromSource: tenant.CutoffSource{Level: tenant.CutoffBusiness}, ToSource: tenant.CutoffSource{Level: tenant.CutoffBranch, Name: "Sliema"},
		Conflicts: []tenant.CutoffConflict{pastryConflict}, Suggested: cutoffAt(20, 30)}}
	staff := &fakeStaff{}
	b := dayBrowser(t, "manager", days, staff, &fakeTrail{})
	rec := b.do(http.MethodPost, employeeMoveHref, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("the move answered %d, want the warning", rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, "Business day starts 05:00 → 23:00 after the move") || !strings.Contains(body, "Suggested: 20:30 — outside every shift.") {
		t.Fatalf("the warning does not say the change:\n%s", body)
	}
	if len(staff.moves) != 0 {
		t.Fatalf("the warning moved the person")
	}
	confirm := formOf(t, body, employeeMoveHref)
	rec = b.do(http.MethodPost, employeeMoveHref, confirm)
	if rec.Code != http.StatusSeeOther || len(staff.moves) != 1 || staff.moves[0].LocationID != venue {
		t.Fatalf("the confirmed move answered %d with %d move(s)", rec.Code, len(staff.moves))
	}
	// A confirmation for another destination does not move.
	b.do(http.MethodPost, employeeMoveHref, form)
	other := formOf(t, htmlOf(t, b.do(http.MethodPost, employeeMoveHref, form)), employeeMoveHref)
	other.Set("to_location", uuid.NewString())
	if rec := b.do(http.MethodPost, employeeMoveHref, other); !strings.Contains(rec.Header().Get("Location"), "day=confirm-") || len(staff.moves) != 1 {
		t.Fatalf("a confirmation for another venue answered %q (%d moves)", rec.Header().Get("Location"), len(staff.moves))
	}

	same := &fakeBusinessDays{move: tenant.MoveCutoff{From: 5 * time.Hour, To: 5 * time.Hour}}
	staff2 := &fakeStaff{}
	rec = dayBrowser(t, "manager", same, staff2, &fakeTrail{}).do(http.MethodPost, employeeMoveHref, form)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "done=moved") || len(staff2.moves) != 1 {
		t.Fatalf("a move that keeps the cutoff answered %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
}

// TestBusinessDay_ASuspendedBusinessIsRefusedAtBothPresses: the two routes are in
// mountWriting's main group, behind ProtectWriting's suspension gate (ADR 0025): a suspended
// business's person-level review and apply answer 403 before any handler runs -- the
// tripwire panel's dependencies would panic if one did. (The route walk of
// TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException covers them too; this names them.)
func TestBusinessDay_ASuspendedBusinessIsRefusedAtBothPresses(t *testing.T) {
	for _, route := range []string{businessDayHref, businessDayApplyHref} {
		code := db.SuspensionNonPayment
		trail := &fakeTrail{}
		h := tripwirePanel(t, suspendedIdentityAs("manager", uuid.New(), uuid.New(), uuid.New(), &code), trail, discardLogger())
		req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(url.Values{
			"level": {"person"}, "id": {uuid.NewString()}, "cutoff": {"23:00"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", testBaseURL)
		req.RemoteAddr = "203.0.113.9:1234"
		req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: suspensionCookieValue})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s for a suspended business answered %d, want 403", route, rec.Code)
		}
	}
}
