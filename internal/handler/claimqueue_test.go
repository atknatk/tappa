package handler

// claimqueue_test.go -- the Spike's "Time claims" (M15-18) over the REAL router, middleware and
// confirmation primitive, with the claim service and the list faked. The fake judges approvals with
// the domain's own claim.Assess, so "outside the band" is the real rule; Postgres-backed twins are in
// claimqueue_db_test.go and internal/domain/claim.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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
	"github.com/atknatk/tappa/internal/domain/claim"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/web/templates/pages"
)

// fakeClaim is one claim the fake service knows.
type fakeClaim struct {
	in    claim.Inspection
	shift *tap.Shift
}

// fakeClaimQueue is the claim service and the list in one value.
type fakeClaimQueue struct {
	mu       sync.Mutex
	zone     *time.Location
	claims   []*fakeClaim
	decided  map[uuid.UUID]claim.Outcome
	calls    []claim.Decision
	written  []claim.Decision
	err      error // returned by Decide instead of judging, when set
	listErr  error
	inspects int
}

func newFakeClaimQueue(zone *time.Location, cs ...*fakeClaim) *fakeClaimQueue {
	return &fakeClaimQueue{zone: zone, claims: cs, decided: map[uuid.UUID]claim.Outcome{}}
}

func (f *fakeClaimQueue) find(id uuid.UUID) *fakeClaim {
	for _, c := range f.claims {
		if c.in.ClaimID == id {
			return c
		}
	}
	return nil
}

func (f *fakeClaimQueue) Claims(_ context.Context, tenantID uuid.UUID) (ledger.ClaimQueue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return ledger.ClaimQueue{}, f.listErr
	}
	q := ledger.ClaimQueue{Queried: true, Zone: f.zone}
	if tenantID != panelTestTenant {
		return q, nil
	}
	for _, c := range f.claims {
		if _, ok := f.decided[c.in.ClaimID]; ok {
			continue
		}
		q.Claims = append(q.Claims, ledger.PendingClaim{ID: c.in.ClaimID, EmployeeID: c.in.EmployeeID, EmployeeName: "Ahmet Demir",
			Kind: string(c.in.Kind), ClaimedAt: c.in.ClaimedAt, SentAt: c.in.CreatedAt, AnchorAt: c.in.Anchor.At})
	}
	return q, nil
}

func (f *fakeClaimQueue) Inspect(_ context.Context, tenantID, claimID uuid.UUID, at *time.Time) (claim.Inspection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspects++
	c := f.find(claimID)
	if c == nil || tenantID != panelTestTenant {
		return claim.Inspection{}, claim.ErrUnknownClaim
	}
	in := c.in
	moment := in.ClaimedAt
	if at != nil {
		moment = *at
	}
	if in.Assessment.At.IsZero() || at != nil {
		in.Assessment = claim.Assess(in.Kind, in.Anchor.At, moment, c.shift, in.Prefilled).WithAnchorChosen(in.AnchorChose)
	}
	if o, ok := f.decided[claimID]; ok {
		in.Decided, in.State = &o, claim.StateDecided
	}
	return in, nil
}

func (f *fakeClaimQueue) Decide(_ context.Context, d claim.Decision) (claim.Decided, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, d)
	if f.err != nil {
		return claim.Decided{}, f.err
	}
	c := f.find(d.ClaimID)
	if c == nil || d.TenantID != panelTestTenant {
		return claim.Decided{}, claim.ErrUnknownClaim
	}
	if _, ok := f.decided[d.ClaimID]; ok {
		return claim.Decided{}, claim.ErrAlreadyDecided
	}
	out := claim.Decided{ID: uuid.New(), Outcome: d.Outcome}
	if d.Outcome == claim.Approved {
		moment := c.in.ClaimedAt
		if d.At != nil {
			moment = *d.At
		}
		a := claim.Assess(c.in.Kind, c.in.Anchor.At, moment, c.shift, c.in.Prefilled).WithAnchorChosen(c.in.AnchorChose)
		out.Assessment = &a
		if a.RequiresConfirmation && d.Confirmation != claim.ConfirmationSubject(c.in.ClaimID, a) {
			return claim.Decided{Assessment: &a}, claim.ErrNeedsConfirmation
		}
	}
	f.decided[d.ClaimID] = d.Outcome
	f.written = append(f.written, d)
	return out, nil
}

func (f *fakeClaimQueue) writes() []claim.Decision {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]claim.Decision(nil), f.written...)
}

// claimPanel is the real router with the claim queue wired (and an identity the caller picks).
func claimPanel(t *testing.T, admins *fakeAdmins, claims *fakeClaimQueue) *browser {
	t.Helper()
	return claimPanelWith(t, admins, claims, newFakeLedger())
}

// claimPanelWith is claimPanel over a ledger the caller set up (the badge's counts).
func claimPanelWith(t *testing.T, admins *fakeAdmins, claims *fakeClaimQueue, records *fakeLedger) *browser {
	t.Helper()
	opts := []AdminAuthOption{}
	if claims != nil {
		opts = append(opts, PunchClaims(claims, claims))
	}
	h, err := NewAdminAuth(admins, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), slog.New(slog.DiscardHandler), opts...)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

func claimAdmins(role string, suspended bool) *fakeAdmins {
	return &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		r := adminauth.Resolved{SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin, Role: role, FullName: "KF Manager"}
		if suspended {
			r.Suspended = &adminauth.Suspension{Since: suspendedSince, Reason: string(db.SuspensionReasons()[0])}
		}
		return r, nil
	}}
}

// The scenario: a business in UTC, a 09:00–17:00 shift, Monday 2026-10-12.
var claimMonday = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)

func claimDay(d, hh, mm int) time.Time {
	return claimMonday.AddDate(0, 0, d).Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute)
}

var nineToFive = &tap.Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "UTC"}

// outClaim is a checkout claim on a check-in at anchorAt, claimed at `at`.
func outClaim(anchorAt, at time.Time, prefilled *time.Time) *fakeClaim {
	in := claim.Inspection{ClaimID: uuid.New(), EmployeeID: uuid.New(), Kind: claim.KindOut, ClaimedAt: at, Prefilled: prefilled,
		CreatedAt: at.Add(time.Hour), State: claim.StatePending,
		Anchor: claim.Event{ID: uuid.New(), Direction: tap.TypeIn, At: anchorAt, Channel: tap.ChannelNFC}}
	return &fakeClaim{in: in, shift: nineToFive}
}

// inClaim is a check-in claim on an orphan checkout at anchorAt the person chose ("Not right?").
func inClaim(anchorAt, at time.Time) *fakeClaim {
	in := claim.Inspection{ClaimID: uuid.New(), EmployeeID: uuid.New(), Kind: claim.KindIn, ClaimedAt: at, CreatedAt: anchorAt,
		State: claim.StatePending, AnchorChose: true,
		Anchor: claim.Event{ID: uuid.New(), Direction: tap.TypeOut, At: anchorAt, Channel: tap.ChannelNFC}}
	return &fakeClaim{in: in, shift: nineToFive}
}

func decisionForm(id uuid.UUID, outcome string, extra ...string) url.Values {
	v := url.Values{"id": {id.String()}, "outcome": {outcome}}
	for i := 0; i+1 < len(extra); i += 2 {
		v.Set(extra[i], extra[i+1])
	}
	return v
}

var stepHiddenRE = regexp.MustCompile(`<input type="hidden" name="(shown|confirm_token)" value="([^"]*)"`)

// stepFields lifts the confirmation step's two hidden values out of a rendered page.
func stepFields(t *testing.T, page string) (shown, token string) {
	t.Helper()
	for _, m := range stepHiddenRE.FindAllStringSubmatch(page, -1) {
		v := strings.ReplaceAll(m[2], "&#43;", "+")
		if m[1] == "shown" {
			shown = v
		} else {
			token = v
		}
	}
	if shown == "" || token == "" {
		t.Fatalf("the page carries no confirmation step (shown %q, token %q)", shown, token)
	}
	return shown, token
}

// TestClaimQueue_FocusAndApproveIsOnePostThatRedirects: an in-band claim is decided by ONE POST, the
// answer is a 303 (PRG), and the banner it leads to is read back from the decision.
func TestClaimQueue_FocusAndApproveIsOnePostThatRedirects(t *testing.T) {
	c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	f := newFakeClaimQueue(time.UTC, c)
	b := claimPanel(t, claimAdmins("manager", false), f)
	page := htmlOf(t, b.do(http.MethodGet, claimsHref, nil))
	if !strings.Contains(page, `name="id" value="`+c.in.ClaimID.String()+`"`) {
		t.Fatal("the focus form does not name the listed claim")
	}
	res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved"))
	if res.Code != http.StatusSeeOther {
		t.Fatalf("Approve answered %d, want 303", res.Code)
	}
	w := f.writes()
	if len(w) != 1 || w[0].Outcome != claim.Approved || w[0].At != nil || w[0].Confirmation != "" || w[0].DecidedBy != panelTestAdmin || w[0].TenantID != panelTestTenant {
		t.Fatalf("decisions written = %+v, want one approval of the claimed moment, by the session's admin, unconfirmed", w)
	}
	done := htmlOf(t, b.do(http.MethodGet, res.Header().Get("Location"), nil))
	if !strings.Contains(done, "Claim approved") {
		t.Errorf("the redirect does not confirm the decision (Location %s)", res.Header().Get("Location"))
	}
	if invented := htmlOf(t, b.do(http.MethodGet, claimsHref+"?done=approved&claim="+uuid.NewString(), nil)); strings.Contains(invented, "Claim approved") {
		t.Error("a banner was printed for a claim nobody decided")
	}
}

// TestClaimQueue_AnOutOfBandApprovalAsksTheStepAndOnlyItsPressWrites is G-Y1: the first press of
// Approve on a claim outside the band writes nothing and draws the step (interval, duration, both
// deviations); the step's own press writes, with the step's subject.
func TestClaimQueue_AnOutOfBandApprovalAsksTheStepAndOnlyItsPressWrites(t *testing.T) {
	c := inClaim(claimDay(0, 9, 0), claimDay(0, 5, 0))
	f := newFakeClaimQueue(time.UTC, c)
	b := claimPanel(t, claimAdmins("owner", false), f)
	first := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "note", "she opened up"))
	if first.Code != http.StatusOK {
		t.Fatalf("the first press answered %d, want 200 with the confirmation step", first.Code)
	}
	if n := len(f.writes()); n != 0 {
		t.Fatalf("the first press wrote %d decision(s); outside the band it must write none", n)
	}
	page := htmlOf(t, first)
	for _, want := range []string{"Confirm this interval", "in Mon 12 Oct 05:00 → out Mon 12 Oct 09:00 = 4 h 00 m",
		"Claimed time: 4 h 00 m before the shift starts", "Their record&#39;s time: within the shift", `value="she opened up"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the step does not say %q", want)
		}
	}
	shown, token := stepFields(t, page)
	second := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "shown", shown, "confirm_token", token))
	if second.Code != http.StatusSeeOther {
		t.Fatalf("the step's press answered %d, want 303", second.Code)
	}
	w := f.writes()
	if len(w) != 1 || w[0].Confirmation == "" || w[0].At == nil || !w[0].At.Equal(c.in.ClaimedAt) {
		t.Fatalf("decisions written = %+v, want one confirmed approval of the claimed moment", w)
	}
	// A second decision on the same claim is "already decided".
	third := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "rejected"))
	if loc := third.Header().Get("Location"); third.Code != http.StatusSeeOther || !strings.Contains(loc, "problem=decided") {
		t.Fatalf("a second decision answered %d %q, want 303 to problem=decided", third.Code, loc)
	}
	if !strings.Contains(htmlOf(t, b.do(http.MethodGet, third.Header().Get("Location"), nil)), "already decided") {
		t.Error("the second decision's page does not say it was already decided")
	}
}

// TestClaimQueue_ConfirmedIsNeverTakenFromTheFirstPost: every way of saying "confirmed" without the
// step's own signed moment writes nothing -- a "confirmed" field, a bare moment, a forged signature,
// a signature minted for another moment, one minted for another claim.
func TestClaimQueue_ConfirmedIsNeverTakenFromTheFirstPost(t *testing.T) {
	c := inClaim(claimDay(0, 9, 0), claimDay(0, 5, 0))
	other := inClaim(claimDay(1, 9, 0), claimDay(1, 5, 0))
	f := newFakeClaimQueue(time.UTC, c, other)
	b := claimPanel(t, claimAdmins("owner", false), f)
	_, otherToken := stepFields(t, htmlOf(t, b.do(http.MethodPost, claimsHref, decisionForm(other.in.ClaimID, "approved"))))
	shown, token := stepFields(t, htmlOf(t, b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved"))))
	at := c.in.ClaimedAt.UTC().Format(time.RFC3339Nano)
	later := c.in.ClaimedAt.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if shown != at {
		t.Fatalf("the step was drawn for %s, want the claimed moment %s", shown, at)
	}
	for name, form := range map[string]url.Values{
		"a confirmed field":          decisionForm(c.in.ClaimID, "approved", "confirmed", "1", "Confirmed", "true"),
		"a bare moment":              decisionForm(c.in.ClaimID, "approved", "shown", at),
		"a forged signature":         decisionForm(c.in.ClaimID, "approved", "shown", at, "confirm_token", "AAAA.BBBB"),
		"another moment's signature": decisionForm(c.in.ClaimID, "approved", "shown", later, "confirm_token", token),
		"another claim's signature":  decisionForm(c.in.ClaimID, "approved", "shown", at, "confirm_token", otherToken),
	} {
		t.Run(name, func(t *testing.T) {
			res := b.do(http.MethodPost, claimsHref, form)
			if n := len(f.writes()); n != 0 {
				t.Fatalf("%d decision(s) written", n)
			}
			if page := strings.ToLower(htmlOf(t, res)); res.Code != http.StatusOK ||
				!(strings.Contains(page, "nothing was recorded") || strings.Contains(page, "nothing has been recorded yet")) {
				t.Errorf("answered %d without saying nothing was recorded", res.Code)
			}
			for _, d := range f.calls {
				if d.Confirmation != "" {
					t.Errorf("Decide was called with a confirmation: %+v", d)
				}
			}
		})
	}
}

// TestClaimQueue_TheRefusalsAreSaidInTheirOwnWords: each refusal of Decide answers with its own
// sentence -- and another business's claim id is "not found" (the fake's tenant check is the
// domain's: every statement carries the tenant).
func TestClaimQueue_TheRefusalsAreSaidInTheirOwnWords(t *testing.T) {
	cases := []struct {
		err  error
		word string
		line string
	}{
		{claim.ErrAlreadyDecided, "decided", "already decided"},
		{claim.ErrUnknownClaim, "unknown", "That claim was not found"},
		{claim.ErrResolved, "resolved", "only Reject is left"},
		{claim.ErrExpired, "expired", "more than 90 days back"},
		{claim.ErrPartnershipEnded, "partnership", "The partnership with that venue has ended"},
		{claim.ErrNoVenue, "novenue", "There is no venue to file this record at"},
		{claim.ErrOutsideWindow, "outside", "outside what can be recorded"},
	}
	for _, tc := range cases {
		t.Run(tc.word, func(t *testing.T) {
			c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
			f := newFakeClaimQueue(time.UTC, c)
			f.err = tc.err
			b := claimPanel(t, claimAdmins("manager", false), f)
			res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved"))
			loc := res.Header().Get("Location")
			if res.Code != http.StatusSeeOther || !strings.Contains(loc, "problem="+tc.word) {
				t.Fatalf("answered %d %q, want 303 to problem=%s", res.Code, loc, tc.word)
			}
			if page := htmlOf(t, b.do(http.MethodGet, loc, nil)); !strings.Contains(page, htmlText(tc.line)) {
				t.Errorf("the page does not say %q", tc.line)
			}
		})
	}
	t.Run("another business's id", func(t *testing.T) {
		f := newFakeClaimQueue(time.UTC, outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil))
		b := claimPanel(t, claimAdmins("manager", false), f)
		res := b.do(http.MethodPost, claimsHref, decisionForm(uuid.New(), "approved"))
		if loc := res.Header().Get("Location"); !strings.Contains(loc, "problem=unknown") || len(f.writes()) != 0 {
			t.Errorf("a foreign id answered %q with %d write(s), want not found and none", loc, len(f.writes()))
		}
	})
	t.Run("a database failure", func(t *testing.T) {
		c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
		f := newFakeClaimQueue(time.UTC, c)
		f.err = errors.New("connection refused")
		b := claimPanel(t, claimAdmins("manager", false), f)
		res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved"))
		if res.Code != http.StatusInternalServerError || !strings.Contains(htmlOf(t, res), problemPanelWriteFailed.Title) {
			t.Errorf("a failure answered %d, want 500 with the writer's page", res.Code)
		}
	})
}

// TestClaimQueue_AResolvedOrExpiredClaimOffersOnlyReject (§C.5).
func TestClaimQueue_AResolvedOrExpiredClaimOffersOnlyReject(t *testing.T) {
	for _, st := range []claim.State{claim.StateResolved, claim.StateExpired} {
		c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
		c.in.State = st
		b := claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, c))
		page := htmlOf(t, b.do(http.MethodGet, claimsHref, nil))
		if strings.Contains(page, `value="approved"`) || !strings.Contains(page, `value="rejected"`) || !strings.Contains(page, "Only Reject is left") {
			t.Errorf("%s: the focus does not offer Reject alone", st)
		}
	}
}

// TestClaimQueue_HasOneFormAndNoBulkControl: the page renders ONE decision form for the claim in
// focus -- one id, two outcomes -- and nothing that selects several claims: no checkbox, no "all",
// no script, no access key (the flagged queue's decision, review.go).
func TestClaimQueue_HasOneFormAndNoBulkControl(t *testing.T) {
	f := newFakeClaimQueue(time.UTC, outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil), outClaim(claimDay(1, 9, 5), claimDay(1, 17, 0), nil),
		inClaim(claimDay(2, 9, 0), claimDay(2, 5, 0)))
	b := claimPanel(t, claimAdmins("manager", false), f)
	page := htmlOf(t, b.do(http.MethodGet, claimsHref, nil))
	main := page[strings.Index(page, "<main"):strings.Index(page, "</main>")]
	if n := strings.Count(main, `<form method="post"`); n != 1 {
		t.Errorf("the section renders %d decision forms, want 1", n)
	}
	if n := strings.Count(main, `name="id"`); n != 1 {
		t.Errorf("%d id fields, want 1", n)
	}
	if n := strings.Count(main, `name="outcome"`); n != 2 {
		t.Errorf("%d outcome buttons, want 2", n)
	}
	for _, banned := range []string{`type="checkbox"`, "accesskey", "<script", "Select all", "Approve all", "name=\"ids\""} {
		if strings.Contains(strings.ToLower(main), strings.ToLower(banned)) {
			t.Errorf("the section carries %q", banned)
		}
	}
	if n := strings.Count(main, `class="queue-item`); n != 3 {
		t.Errorf("the list renders %d links, want 3", n)
	}
}

// TestClaimQueue_TheTwoGY1CasesShowTheIntervalTheDeviationAndTheMark: Ahmet's orphan 09:00 checkout
// with a 05:00 check-in claim, and a Monday check-in claimed out on Tuesday 03:00 -- each in the
// focus with the interval, its duration, the shift and both ends' deviation; the first with the
// "they chose this direction" mark, the second without.
func TestClaimQueue_TheTwoGY1CasesShowTheIntervalTheDeviationAndTheMark(t *testing.T) {
	ahmet := inClaim(claimDay(0, 9, 0), claimDay(0, 5, 0))
	tuesday := outClaim(claimDay(0, 9, 12), claimDay(1, 3, 0), nil)
	for _, tc := range []struct {
		c    *fakeClaim
		want []string
		mark bool
	}{
		{ahmet, []string{"in Mon 12 Oct 05:00 → out Mon 12 Oct 09:00 = 4 h 00 m", "Mon 12 Oct 09:00 – 17:00",
			"4 h 00 m before the shift starts", "within the shift", "Approve will ask you to confirm"}, true},
		{tuesday, []string{"in Mon 12 Oct 09:12 → out Tue 13 Oct 03:00 = 17 h 48 m", "10 h 00 m after the shift ends",
			"within the shift", "Approve will ask you to confirm"}, false},
	} {
		b := claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, tc.c))
		page := htmlOf(t, b.do(http.MethodGet, claimsHref, nil))
		for _, w := range tc.want {
			if !strings.Contains(page, htmlText(w)) {
				t.Errorf("the focus does not say %q", w)
			}
		}
		if got := strings.Contains(page, "They chose this direction"); got != tc.mark {
			t.Errorf("the mark is drawn = %v, want %v", got, tc.mark)
		}
	}
}

// TestClaimQueue_DefaultNotChangedOnlyWhileAPrefillExists: the mark is a computation over prefilled_at
// (ADR 0032 §C.2) -- drawn for a claim that kept the offered shift end, never without a prefill, even
// were the assessment to say so.
func TestClaimQueue_DefaultNotChangedOnlyWhileAPrefillExists(t *testing.T) {
	end := claimDay(0, 17, 0)
	kept := outClaim(claimDay(0, 9, 5), end, &end)
	b := claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, kept))
	if page := htmlOf(t, b.do(http.MethodGet, claimsHref, nil)); !strings.Contains(page, "Default not changed") || !strings.Contains(page, "the shift&#39;s end") {
		t.Error("a claim that kept the offered end does not say so")
	}
	bare := outClaim(claimDay(0, 9, 5), end, nil)
	bare.in.Assessment = claim.Assess(claim.KindOut, bare.in.Anchor.At, end, nineToFive, nil)
	bare.in.Assessment.DefaultUnchanged = true // a stray true must not reach the page without a prefill
	b = claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, bare))
	if page := htmlOf(t, b.do(http.MethodGet, claimsHref, nil)); strings.Contains(page, "Default not changed") {
		t.Error("\"Default not changed\" was drawn for a claim the form offered no time for")
	}
}

// TestClaimQueue_TheNextRecordIsNoneWithin18Hours: Inspect reads the chain only 18 h past the anchor,
// so a missing After is "none within 18 h", never a bare "none".
func TestClaimQueue_TheNextRecordIsNoneWithin18Hours(t *testing.T) {
	b := claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)))
	page := htmlOf(t, b.do(http.MethodGet, claimsHref, nil))
	if !strings.Contains(page, "none within 18 h") {
		t.Error("an absent next record does not say how far the read looked")
	}
}

// TestClaimMoment_TheRepeatedAutumnHourCarriesItsOffset: 02:30 on Malta's autumn night is two
// instants, and each is shown with its offset; an ordinary hour is not.
func TestClaimMoment_TheRepeatedAutumnHourCarriesItsOffset(t *testing.T) {
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	early := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC) // 02:30 CEST
	late := time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)  // 02:30 CET
	if got := claimMoment(early, malta); got != "Sun 25 Oct 02:30 (UTC+02:00)" {
		t.Errorf("the first 02:30 = %q", got)
	}
	if got := claimMoment(late, malta); got != "Sun 25 Oct 02:30 (UTC+01:00)" {
		t.Errorf("the second 02:30 = %q", got)
	}
	if got := claimMoment(time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC), malta); got != "Sun 25 Oct 10:00" {
		t.Errorf("an ordinary hour = %q", got)
	}
}

// TestClaimQueue_AnEditIsResolvedInsideTheWindowAndHeldToTheBand: the Edit is a time of day,
// resolved in the business's zone inside the claim's window; an Edit outside the band asks the step
// like the claimed moment would, and an unreadable Edit writes nothing.
func TestClaimQueue_AnEditIsResolvedInsideTheWindowAndHeldToTheBand(t *testing.T) {
	c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	c.in.Window = &claim.Window{From: claim.Bound{At: c.in.Anchor.At}, To: claim.Bound{At: claimDay(1, 3, 5), Inclusive: true}}
	f := newFakeClaimQueue(time.UTC, c)
	b := claimPanel(t, claimAdmins("manager", false), f)
	if res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "at", "nonsense")); res.Code != http.StatusOK || len(f.calls) != 0 {
		t.Fatalf("an unreadable Edit answered %d after %d Decide call(s), want the form again and none", res.Code, len(f.calls))
	}
	out := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "at", "23:30"))
	if out.Code != http.StatusOK || !strings.Contains(htmlOf(t, out), "Your edited time:") || len(f.writes()) != 0 {
		t.Fatalf("an Edit outside the band answered %d with %d write(s), want the step and none", out.Code, len(f.writes()))
	}
	res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "at", "16:30"))
	w := f.writes()
	if res.Code != http.StatusSeeOther || len(w) != 1 || w[0].At == nil || !w[0].At.Equal(claimDay(0, 16, 30)) {
		t.Fatalf("an in-band Edit answered %d, writes %+v; want one approval at Mon 16:30", res.Code, w)
	}
}

// TestClaimQueue_ADecisionIsRefusedWhileTheBusinessIsSuspended (ADR 0025 §3): the POST is on the main
// writing group -- 403, and the claim service is never asked.
func TestClaimQueue_ADecisionIsRefusedWhileTheBusinessIsSuspended(t *testing.T) {
	for _, role := range []string{"owner", "manager"} {
		c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
		f := newFakeClaimQueue(time.UTC, c)
		b := claimPanel(t, claimAdmins(role, true), f)
		res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved"))
		if res.Code != http.StatusForbidden || len(f.calls) != 0 {
			t.Errorf("%s: a suspended business's decision answered %d after %d Decide call(s), want 403 and none", role, res.Code, len(f.calls))
		}
		if res := b.do(http.MethodGet, claimsHref, nil); res.Code != http.StatusOK {
			t.Errorf("%s: the queue itself (a read) answered %d while suspended, want 200", role, res.Code)
		}
	}
}

// TestClaimQueue_TheSpikeLinksToItOnlyWhenWired: the pair of links is drawn on the Spike when the
// claim service is wired, and the Spike is the page it was without it.
func TestClaimQueue_TheSpikeLinksToItOnlyWhenWired(t *testing.T) {
	with := htmlOf(t, claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC)).do(http.MethodGet, reviewHref, nil))
	if !strings.Contains(with, `href="`+claimsHref+`"`) {
		t.Error("the Spike does not link to Time claims")
	}
	without := claimPanel(t, claimAdmins("manager", false), nil)
	if strings.Contains(htmlOf(t, without.do(http.MethodGet, reviewHref, nil)), claimsHref) {
		t.Error("a panel without the claim service links to its queue")
	}
	if res := without.do(http.MethodGet, claimsHref, nil); res.Code != http.StatusServiceUnavailable {
		t.Errorf("the queue without the service answered %d, want 503", res.Code)
	}
	if res := without.do(http.MethodPost, claimsHref, decisionForm(uuid.New(), "approved")); res.Code != http.StatusServiceUnavailable {
		t.Errorf("the decision without the service answered %d, want 503", res.Code)
	}
}

// TestClaimQueue_AFailedListReadIsSaidNotDrawnEmpty (§4.6).
func TestClaimQueue_AFailedListReadIsSaidNotDrawnEmpty(t *testing.T) {
	f := newFakeClaimQueue(time.UTC)
	f.listErr = errors.New("timeout")
	res := claimPanel(t, claimAdmins("manager", false), f).do(http.MethodGet, claimsHref, nil)
	if body := htmlOf(t, res); res.Code != http.StatusInternalServerError || strings.Contains(body, "No time claim is waiting") {
		t.Errorf("a failed read answered %d and drew the empty state = %v", res.Code, strings.Contains(body, "No time claim is waiting"))
	}
}

// TestAnomaliesView_TheClaimPendingMarkIsDrawnOnItsOwnRowOnly: Odd shapes draws "Claim pending" on
// exactly the rows the read marked (by id) -- on both lists -- and the unmarked page is the page as it
// was (the holdingless golden, unchanged, is the byte-level half).
func TestAnomaliesView_TheClaimPendingMarkIsDrawnOnItsOwnRowOnly(t *testing.T) {
	period := ledger.WeekOf(ledger.Date{Year: 2026, Month: time.October, Day: 12}, time.UTC)
	at := claimDay(0, 9, 0)
	var a ledger.AnomalyScreen
	a.Queried, a.Period, a.Zone = true, period, time.UTC
	onIn := uuid.New()
	a.Open = []ledger.AnomalyOpen{{ID: uuid.New(), EmployeeName: "Marked In", At: at, ClaimPending: true, PendingClaimID: onIn}, {ID: uuid.New(), EmployeeName: "Plain In", At: at}}
	a.InMissing = []ledger.AnomalyInMissing{{ID: uuid.New(), EmployeeName: "Marked Out", At: at, ClaimPending: true}, {ID: uuid.New(), EmployeeName: "Plain Out", At: at}}
	var v pages.AnomaliesView
	fillAnomaliesView(&v, a, claimDay(7, 12, 0))
	var b strings.Builder
	if err := pages.AdminAnomalies(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	if n := strings.Count(page, "Claim pending"); n != 2 {
		t.Fatalf("the page draws %d marks, want 2", n)
	}
	// The mark whose claim the read named links to it in focus on the queue (review round).
	if !strings.Contains(page, `<a href="`+claimsHref+`?focus=`+onIn.String()+`" class="odd-chip tally--manual hit-link">Claim pending</a>`) {
		t.Error("the mark does not link to its claim on the queue")
	}
	for _, who := range []string{"Marked In", "Marked Out"} {
		i := strings.Index(page, who)
		next := strings.Index(page[i:], "</li>")
		if i < 0 || next < 0 || !strings.Contains(page[i:i+next], "Claim pending") {
			t.Errorf("%s's row carries no mark", who)
		}
	}
	for _, who := range []string{"Plain In", "Plain Out"} {
		i := strings.Index(page, who)
		next := strings.Index(page[i:], "</li>")
		if i < 0 || next < 0 || strings.Contains(page[i:i+next], "Claim pending") {
			t.Errorf("%s's row carries a mark it was not given", who)
		}
	}
}

// TestClaimQueue_AnUnreadableDecisionIsNeverAnApproval: a missing or unknown outcome, or an id that
// is not a uuid, is refused before the claim service is asked -- never defaulted to an approval.
func TestClaimQueue_AnUnreadableDecisionIsNeverAnApproval(t *testing.T) {
	c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	f := newFakeClaimQueue(time.UTC, c)
	b := claimPanel(t, claimAdmins("manager", false), f)
	for name, form := range map[string]url.Values{
		"no outcome":      {"id": {c.in.ClaimID.String()}},
		"unknown outcome": decisionForm(c.in.ClaimID, "maybe"),
		"id not a uuid":   {"id": {"nope"}, "outcome": {"approved"}},
	} {
		res := b.do(http.MethodPost, claimsHref, form)
		if loc := res.Header().Get("Location"); res.Code != http.StatusSeeOther || !strings.Contains(loc, "problem=unreadable") {
			t.Errorf("%s: answered %d %q, want 303 to problem=unreadable", name, res.Code, loc)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("the claim service was asked %d time(s)", len(f.calls))
	}
}

// mainOf is the page's <main>, where the section draws (the chrome's own forms are outside it).
func mainOf(page string) string {
	i, j := strings.Index(page, "<main"), strings.Index(page, "</main>")
	if i < 0 || j < i {
		return page
	}
	return page[i:j]
}

// TestClaimQueue_TheStepOpensTheSectionAndItsButtonsAreNotPinned (M15-18 review round, YÜKSEK-1): on
// the step the box -- interval, duration, deviations -- comes FIRST in the claim's section, its buttons
// right under it and before the record's facts, and nothing on the step is in the phone's pinned bar
// (.spike-actions), where the first Approve was pressed.
func TestClaimQueue_TheStepOpensTheSectionAndItsButtonsAreNotPinned(t *testing.T) {
	c := outClaim(claimDay(0, 9, 12), claimDay(1, 3, 0), nil)
	b := claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, c))
	first := mainOf(htmlOf(t, b.do(http.MethodGet, claimsHref, nil)))
	if !strings.Contains(first, `class="spike-actions"`) {
		t.Fatal("control: the focus form's first Approve is not in the pinned bar, so the check below would be vacuous")
	}
	page := mainOf(htmlOf(t, b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved"))))
	section := page[strings.Index(page, `aria-label="The claim being decided"`):]
	box := strings.Index(section, `id="claim-step"`)
	buttons := strings.Index(section, `id="claim-step-actions"`)
	facts := strings.Index(section, "Their record</dt>")
	pager := strings.Index(section, `aria-label="Records on this page"`)
	if box < 0 || buttons < 0 || facts < 0 {
		t.Fatalf("the step lacks a part: box %d, buttons %d, facts %d", box, buttons, facts)
	}
	if !(box < buttons && buttons < facts) {
		t.Errorf("the step's order is box %d, buttons %d, facts %d; want the box first, then its buttons, then the facts", box, buttons, facts)
	}
	if pager >= 0 && pager < box {
		t.Error("something comes before the step's box in the claim's section")
	}
	if strings.Contains(page, "spike-actions") {
		t.Error("the step carries .spike-actions: its press would sit where the first Approve was")
	}
	if i := strings.Index(section, "in Mon 12 Oct 09:12 → out Tue 13 Oct 03:00 = 17 h 48 m"); i < 0 || i > buttons {
		t.Error("the interval and its duration are not in the box above the step's buttons")
	}
}

// TestClaimQueue_TheStepNamesOneIntervalAndTheEditedTime (ORTA-1): an Edit's step names the edited
// moment as such and the screen shows ONE interval -- the one the press writes -- not the claimed one
// beside it.
func TestClaimQueue_TheStepNamesOneIntervalAndTheEditedTime(t *testing.T) {
	c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	c.in.Window = &claim.Window{From: claim.Bound{At: c.in.Anchor.At}, To: claim.Bound{At: claimDay(1, 3, 5), Inclusive: true}}
	b := claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, c))
	page := mainOf(htmlOf(t, b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "at", "23:30"))))
	if !strings.Contains(page, "Your edited time: 6 h 30 m after the shift ends") {
		t.Error("the step does not name the edited time as the user's edit")
	}
	if strings.Contains(page, "Claimed time:") || strings.Contains(page, "Approving makes") {
		t.Error("the step still names the claimed moment's line or the claimed interval")
	}
	if n := strings.Count(page, htmlText("→ out")); n != 1 {
		t.Errorf("the step shows %d intervals, want exactly one", n)
	}
	if !strings.Contains(page, htmlText("→ out Mon 12 Oct 23:30")) {
		t.Error("the one interval is not the edited one")
	}
}

// TestClaimQueue_AShiftChangedSinceTheStepIsNotConfirmed (D-1, the mutant that took the shift out
// of the signed subject): the step was drawn against 09:00–17:00; the shift is 09:00–18:00 at the
// press -- nothing is written and the page says why.
func TestClaimQueue_AShiftChangedSinceTheStepIsNotConfirmed(t *testing.T) {
	c := outClaim(claimDay(0, 9, 12), claimDay(1, 3, 0), nil)
	f := newFakeClaimQueue(time.UTC, c)
	b := claimPanel(t, claimAdmins("manager", false), f)
	shown, token := stepFields(t, htmlOf(t, b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved"))))
	f.mu.Lock()
	c.shift = &tap.Shift{Start: 9 * time.Hour, End: 18 * time.Hour, Timezone: "UTC"}
	f.mu.Unlock()
	res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "shown", shown, "confirm_token", token))
	if n := len(f.writes()); n != 0 {
		t.Fatalf("%d decision(s) written against a shift nobody was shown", n)
	}
	if page := htmlOf(t, res); !strings.Contains(page, "its shift or the time changed") {
		t.Error("the page does not say why nothing was recorded")
	}
}

// TestClaimQueue_AnUnsignedMomentIsNeverWritten (security DÜŞÜK-2): a posted step moment without a
// genuine signature is not the moment written -- not even inside the band, where no step is needed.
func TestClaimQueue_AnUnsignedMomentIsNeverWritten(t *testing.T) {
	c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	f := newFakeClaimQueue(time.UTC, c)
	b := claimPanel(t, claimAdmins("manager", false), f)
	res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "shown", claimDay(0, 16, 0).Format(time.RFC3339Nano)))
	if len(f.calls) != 0 || len(f.writes()) != 0 {
		t.Fatalf("an unsigned moment reached Decide (%d call(s))", len(f.calls))
	}
	page := htmlOf(t, res)
	if !strings.Contains(page, "The confirmation had expired or could not be read") || !strings.Contains(page, `value="16:00"`) {
		t.Error("the page does not say why, or lost the time to edit")
	}
}

// TestClaimQueue_AnEditThatCannotBeUsedIsNeverTheClaimedMoment (D-1/M4, D-2, D-3): a typed time outside
// the window -- or on a claim that takes no other time -- writes nothing (never the claimed moment, a
// record nobody could take back), keeps the typed time and names the times that fit.
func TestClaimQueue_AnEditThatCannotBeUsedIsNeverTheClaimedMoment(t *testing.T) {
	c := outClaim(claimDay(0, 10, 5), claimDay(0, 17, 0), nil)
	c.in.Window = &claim.Window{From: claim.Bound{At: c.in.Anchor.At}, To: claim.Bound{At: claimDay(1, 4, 5), Inclusive: true}}
	f := newFakeClaimQueue(time.UTC, c)
	b := claimPanel(t, claimAdmins("manager", false), f)
	res := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "at", "08:00"))
	if len(f.calls) != 0 {
		t.Fatalf("an Edit outside the window reached Decide: %+v", f.calls)
	}
	page := htmlOf(t, res)
	for _, want := range []string{`value="08:00"`, "Pick a time after Mon 12 Oct 10:05 up to Tue 13 Oct 04:05.",
		htmlText("That time is outside this claim's window")} {
		if !strings.Contains(page, want) {
			t.Errorf("the form shown again does not carry %q", want)
		}
	}
	closed := outClaim(claimDay(2, 9, 5), claimDay(2, 17, 0), nil)
	f2 := newFakeClaimQueue(time.UTC, closed)
	b2 := claimPanel(t, claimAdmins("manager", false), f2)
	res = b2.do(http.MethodPost, claimsHref, decisionForm(closed.in.ClaimID, "approved", "at", "16:00"))
	if len(f2.calls) != 0 || !strings.Contains(htmlOf(t, res), "can no longer be approved at another time") {
		t.Errorf("an Edit on a claim with no window reached Decide (%d call(s)) or was not refused in words", len(f2.calls))
	}
}

// TestClaimQueue_ABannerIsNeverPrintedForAPendingClaim (D-1/M10): ?done=approved names a decision; a
// claim nobody decided gets no banner.
func TestClaimQueue_ABannerIsNeverPrintedForAPendingClaim(t *testing.T) {
	c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	b := claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, c))
	if page := htmlOf(t, b.do(http.MethodGet, claimsHref+"?done=approved&claim="+c.in.ClaimID.String(), nil)); strings.Contains(page, "Claim approved") {
		t.Error("a banner was printed for a claim still waiting")
	}
}

// TestClaimQueue_TheFocusWordsSayEachThingOnce (D-4): no "Shift Shift", the Edit names what it
// records, and a claim only Reject is left for names no interval it cannot write.
func TestClaimQueue_TheFocusWordsSayEachThingOnce(t *testing.T) {
	c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	page := htmlOf(t, claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, c)).do(http.MethodGet, claimsHref, nil))
	if strings.Contains(page, "Shift Shift") || strings.Contains(page, ">Shift Mon") {
		t.Error("the shift's line repeats its label")
	}
	if !strings.Contains(page, "New clock-out time") {
		t.Error("the Edit does not name what it records")
	}
	r := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	r.in.State = claim.StateResolved
	page = htmlOf(t, claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, r)).do(http.MethodGet, claimsHref, nil))
	if strings.Contains(page, "Approving makes") || strings.Contains(page, "Approving writes") {
		t.Error("a claim only Reject is left for still names what approving would write")
	}
}

// TestClaimQueue_TheWaitingClaimsAreCountedInThreePlaces (user decision "Üç yerde sayı"): the Spike's
// queue link "Time claims · N", its empty state's sentence and The Pass's card -- for N, for 0 and for
// a count that failed (never printed as 0).
func TestClaimQueue_TheWaitingClaimsAreCountedInThreePlaces(t *testing.T) {
	cases := []struct {
		name                     string
		claims                   ledger.ClaimCount
		link, sentence, passLine string
	}{
		{"three", ledger.ClaimCount{Queried: true, N: 3}, "· 3", "3 time claims are waiting →", "3 time claims waiting"},
		{"one", ledger.ClaimCount{Queried: true, N: 1}, "· 1", "1 time claim is waiting →", "1 time claim waiting"},
		{"none", ledger.ClaimCount{Queried: true}, "· 0", "", "no time claims waiting"},
		{"failed", ledger.ClaimCount{Failed: true}, "· ?", "", "time claims not counted"},
		{"capped", ledger.ClaimCount{Queried: true, N: 100, Capped: true}, "· 100+", "100+ time claims are waiting →", "100+ time claims waiting"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := newFakeLedger()
			records.pending = ledger.Pending{Claims: tc.claims}
			b := claimPanelWith(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC), records)
			spike := mainOf(htmlOf(t, b.do(http.MethodGet, reviewHref, nil)))
			if !strings.Contains(spike, `<span class="chip-count">`+htmlText(tc.link)+`</span>`) {
				t.Errorf("the queue link does not carry %q", tc.link)
			}
			if !strings.Contains(spike, "No flagged tap is waiting") {
				t.Error("the empty Spike does not say which queue is empty")
			}
			if tc.sentence != "" && !strings.Contains(spike, htmlText(tc.sentence)) {
				t.Errorf("the empty Spike does not say %q", tc.sentence)
			}
			if tc.sentence == "" && strings.Contains(spike, "are waiting →") {
				t.Error("the empty Spike invents waiting claims")
			}
			pass := mainOf(htmlOf(t, b.do(http.MethodGet, transactionsHref, nil)))
			if !strings.Contains(pass, tc.passLine) {
				t.Errorf("The Pass's card does not say %q", tc.passLine)
			}
		})
	}
}

// TestClaimQueue_AnUnreadableEditIsNotCalledOutsideTheWindow (M15-18 last round): a time that cannot be
// read says so; a time that reads but does not fit says THAT, with the times that fit.
func TestClaimQueue_AnUnreadableEditIsNotCalledOutsideTheWindow(t *testing.T) {
	c := outClaim(claimDay(0, 10, 5), claimDay(0, 17, 0), nil)
	c.in.Window = &claim.Window{From: claim.Bound{At: c.in.Anchor.At}, To: claim.Bound{At: claimDay(1, 4, 5), Inclusive: true}}
	b := claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, c))
	unreadable := htmlOf(t, b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "at", "nonsense")))
	if !strings.Contains(unreadable, "We could not read that") || strings.Contains(unreadable, htmlText("outside this claim's window")) {
		t.Error("an unreadable time is not answered as unreadable")
	}
	outside := htmlOf(t, b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "at", "08:00")))
	if strings.Contains(outside, "We could not read that") || !strings.Contains(outside, htmlText("That time is outside this claim's window")) {
		t.Error("a time that reads but does not fit is called unreadable")
	}
}

// TestClaimQueue_S5AChosenRecordAsksTheStepAndSaysWhy (M15-19, S5): a check-in claim on an out the
// person chose, both ends inside the band (09:00 → 17:30) -- the first Approve draws the step and its
// box says why (their own choice, not the band); only the step's press writes. The band-only reason
// keeps its sentence.
func TestClaimQueue_S5AChosenRecordAsksTheStepAndSaysWhy(t *testing.T) {
	c := inClaim(claimDay(0, 17, 30), claimDay(0, 9, 0))
	f := newFakeClaimQueue(time.UTC, c)
	b := claimPanel(t, claimAdmins("owner", false), f)
	first := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved"))
	if first.Code != http.StatusOK || len(f.writes()) != 0 {
		t.Fatalf("the first press answered %d and wrote %d; want the step and nothing written", first.Code, len(f.writes()))
	}
	page := htmlOf(t, first)
	if !strings.Contains(page, "They chose this direction when they tapped, so it is not approved in one press.") {
		t.Error("the step does not say the person chose the record's direction")
	}
	if strings.Contains(page, "outside the shift&#39;s band") {
		t.Error("the step blames the band for an interval inside it")
	}
	shown, token := stepFields(t, page)
	if second := b.do(http.MethodPost, claimsHref, decisionForm(c.in.ClaimID, "approved", "shown", shown, "confirm_token", token)); second.Code != http.StatusSeeOther {
		t.Fatalf("the step's press answered %d, want 303", second.Code)
	}
	if n := len(f.writes()); n != 1 {
		t.Errorf("%d decisions written, want 1", n)
	}
}

// TestClaimStepWhy_NamesTheDomainsReasons: the box's line for each of the domain's two reasons and
// for both.
func TestClaimStepWhy_NamesTheDomainsReasons(t *testing.T) {
	for _, tc := range []struct {
		a    claim.Assessment
		want string
	}{
		{claim.Assessment{OutsideBand: true}, "It is outside the shift's band, or there is no shift, so it is not approved in one press."},
		{claim.Assessment{AnchorChosen: true}, "They chose this direction when they tapped, so it is not approved in one press."},
		{claim.Assessment{OutsideBand: true, AnchorChosen: true}, "It is outside the shift's band, or there is no shift, and they chose this direction when they tapped, so it is not approved in one press."},
	} {
		if got := claimStepWhy(tc.a); got != tc.want {
			t.Errorf("claimStepWhy(%+v) = %q, want %q", tc.a, got, tc.want)
		}
	}
}

// TestClaimQueue_AClaimWithNoWindowSaysSoAndOffersOnlyReject (M15-19 DÜŞÜK-3): a claim the domain
// judges outside any window because it drew none (in.Window nil) is not "a later record narrowed
// it": its own sentence, no Approve, no Edit -- only Reject. A claim whose window a later record
// narrowed still offers the Edit.
func TestClaimQueue_AClaimWithNoWindowSaysSoAndOffersOnlyReject(t *testing.T) {
	c := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	c.in.State, c.in.Window = claim.StateOutsideWindow, nil
	page := htmlOf(t, claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, c)).do(http.MethodGet, claimsHref, nil))
	if !strings.Contains(page, "its business day can no longer be measured") || !strings.Contains(page, "Only Reject is left") {
		t.Error("a windowless claim does not say it has no window")
	}
	if strings.Contains(page, "a later record narrowed") || strings.Contains(page, `value="approved"`) || strings.Contains(page, `name="at"`) {
		t.Error("a windowless claim offers an Approve or an Edit, or blames a later record")
	}
	if !strings.Contains(page, `value="rejected"`) {
		t.Error("a windowless claim offers no Reject")
	}
	narrowed := outClaim(claimDay(0, 9, 5), claimDay(0, 17, 0), nil)
	w := claim.Window{From: claim.Bound{At: claimDay(0, 9, 5)}, To: claim.Bound{At: claimDay(0, 16, 0)}}
	narrowed.in.State, narrowed.in.Window = claim.StateOutsideWindow, &w
	page = htmlOf(t, claimPanel(t, claimAdmins("manager", false), newFakeClaimQueue(time.UTC, narrowed)).do(http.MethodGet, claimsHref, nil))
	if !strings.Contains(page, "a later record narrowed") || !strings.Contains(page, `name="at"`) {
		t.Error("control: a narrowed window no longer offers the Edit")
	}
}
