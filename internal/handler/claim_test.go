package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/claim"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web"
)

// claim_test.go -- the result screen's claim card and POST /api/claim against fakes (M15-17,
// ADR 0032 §C.1–§C.5). claim_db_test.go drives the same surface through real Postgres.

// noClaims is the claim service of a business where no record misses its half: Offer answers
// "no card" and Submit is never reached. Every result screen that existed before M15-17 renders
// through it, which is why their goldens did not move.
type noClaims struct{}

func (noClaims) Offer(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*claim.Card, error) {
	return nil, nil
}
func (noClaims) Submit(context.Context, claim.Claim) (claim.Submitted, error) {
	return claim.Submitted{}, errors.New("noClaims: Submit was called")
}

// fakeClaims answers Offer with card (or offerErr, or blocks until the context ends) and records
// every call; Submit answers submitErr or a fixed Submitted.
type fakeClaims struct {
	mu         sync.Mutex
	card       *claim.Card
	offerErr   error
	offerBlock bool
	offerPanic bool
	offers     []uuid.UUID
	submitErr  error
	// sent: a claim is already on the record -- Submit answers ErrAlreadySent before it looks at
	// the moment, as the domain does.
	sent    bool
	submits []claim.Claim
}

func (f *fakeClaims) Offer(ctx context.Context, tenantID, employeeID, tapID uuid.UUID) (*claim.Card, error) {
	f.mu.Lock()
	f.offers = append(f.offers, tapID)
	f.mu.Unlock()
	// A context that has already ended fails the read at once, as a real query does (pgx checks it
	// before it sends anything) -- fakeDirectory.ResultBrand's rule.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.offerBlock {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.offerPanic {
		panic("fakeClaims: Offer panicked")
	}
	return f.card, f.offerErr
}

func (f *fakeClaims) Submit(_ context.Context, c claim.Claim) (claim.Submitted, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submits = append(f.submits, c)
	switch {
	case f.sent:
		return claim.Submitted{}, claim.ErrAlreadySent
	case f.submitErr != nil:
		return claim.Submitted{}, f.submitErr
	case c.ClaimedAt.IsZero():
		return claim.Submitted{}, claim.ErrOutsideWindow // the domain's answer to no moment
	}
	return claim.Submitted{ID: uuid.MustParse("77777777-7777-4777-8777-777777777777")}, nil
}

func (f *fakeClaims) submitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submits)
}

func newClaimHandler(t *testing.T, svc *fakeCheckins, claims tapClaims) (http.Handler, *Tap) {
	t.Helper()
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		fixedSessions(), svc, claims, noActivation{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	r := chi.NewRouter()
	tp.Mount(r)
	return r, tp
}

// The card's scenario, in Malta (CEST in July): Thursday 09:12 in, left open; the tap that shows
// the card is Friday 14:03:22. Shift 09:00–17:00, so the prefill is Thursday 17:00 local.
var (
	cardAnchorID  = uuid.MustParse("88888888-8888-4888-8888-888888888888")
	cardTapID     = uuid.MustParse("99999999-9999-4999-8999-999999999999")
	cardTapAt     = time.Date(2026, 7, 31, 12, 3, 22, 0, time.UTC)
	cardAnchorAt  = time.Date(2026, 7, 30, 7, 12, 0, 0, time.UTC)
	cardPrefilled = time.Date(2026, 7, 30, 15, 0, 0, 0, time.UTC)
	cardWindow    = claim.Window{From: claim.Bound{At: cardAnchorAt}, To: claim.Bound{At: time.Date(2026, 7, 31, 3, 0, 0, 0, time.UTC)}}
)

func outCard(prefilled bool) *claim.Card {
	c := &claim.Card{Kind: claim.KindOut, AnchorID: cardAnchorID, AnchorAt: cardAnchorAt, Window: cardWindow}
	if prefilled {
		p := cardPrefilled
		c.Prefilled = &p
	}
	return c
}

// cardResult is a recorded `in` that left cardAnchorID open, with the engine's two screen notes.
func cardResult(verdict string) checkin.Result {
	d := decisionOf(verdict, "in", false)
	d.Note = noteIPMatched + "; " + noteLeftOpenRecord
	d.EmployeeNote = noteIPMatched + "; " + noteLeftOpenScreen
	d.EmployeeNoteAsked = noteIPMatched
	d.LeftOpen = &tap.LeftOpen{ID: cardAnchorID, Reason: tap.LeftOpenPreviousBusinessDay}
	return checkin.Result{Outcome: checkin.OutcomeRecorded, Decision: d, TransactionID: cardTapID,
		LocationName: "St Julians", Timezone: "Europe/Malta", OccurredAt: cardTapAt, BusinessType: "restaurant"}
}

// postTapThrough posts one signed tap through h and returns the response.
func postTapThrough(t *testing.T, h http.Handler, tp *Tap) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"ctx": {mintedContext(t, tp, tapFixedSessionID, tapContext{
		UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true, TagTenantID: testTenant, LocationID: tapLocation,
	})}}
	return postForm(t, h, form, sessionCookie())
}

// TestNewTap_RefusesANilClaimService: the card and POST /api/claim both need the service, so its
// absence is a boot error -- an untyped nil and a typed one alike.
func TestNewTap_RefusesANilClaimService(t *testing.T) {
	var typedNil *claim.Service
	for name, c := range map[string]tapClaims{"untyped nil": nil, "typed nil": typedNil} {
		_, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
			&fakeSessions{}, &fakeCheckins{}, c, noActivation{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err == nil {
			t.Errorf("%s: a nil claim service was accepted", name)
		}
	}
}

// TestResultScreen_AFailedOrLateCardReadDrawsTheScreenWithoutIt is the card's error-injection
// criterion (security lens G-O5): the card's read failing, or outliving its bound, costs the card
// and nothing else -- 200, the whole body (byte for byte the screen without a card), and the ONE
// record the tap made. The read happens after Record returned; it is never a condition of it.
func TestResultScreen_AFailedOrLateCardReadDrawsTheScreenWithoutIt(t *testing.T) {
	res := cardResult("ok")
	hPlain, tpPlain := newClaimHandler(t, &fakeCheckins{result: res}, noClaims{})
	want := postTapThrough(t, hPlain, tpPlain).Body.String()
	if strings.Contains(want, "data-claim-card") {
		t.Fatal("control: the screen without a card has a card")
	}
	for name, fc := range map[string]*fakeClaims{
		"the read fails":     {offerErr: errors.New("connection reset by peer")},
		"the read times out": {offerBlock: true, card: outCard(true)},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &fakeCheckins{result: res}
			h, tp := newClaimHandler(t, svc, fc)
			tp.claimWait = 50 * time.Millisecond
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- postTapThrough(t, h, tp) }()
			var w *httptest.ResponseRecorder
			select {
			case w = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the result screen waited on the card's read past its bound")
			}
			if w.Code != http.StatusOK || w.Body.String() != want {
				t.Fatalf("status %d, body:\n%s\nwant 200 and the screen without a card:\n%s", w.Code, w.Body.String(), want)
			}
			if len(svc.calls) != 1 || len(fc.offers) != 1 || fc.offers[0] != cardTapID {
				t.Errorf("records %d, card reads %v; want one record and one read of the tap %s", len(svc.calls), fc.offers, cardTapID)
			}
		})
	}
}

// TestResultScreen_TheCardIsAskedOnlyOnADirectedRecord (K4, ADR 0032 §C.1): `ok` and `flag` show
// the card; `ignored` and `reject` carry no direction, are no showing, and are not even asked.
func TestResultScreen_TheCardIsAskedOnlyOnADirectedRecord(t *testing.T) {
	for _, c := range []struct {
		verdict, direction string
		card               bool
	}{{"ok", "in", true}, {"flag", "in", true}, {"ignored", "", false}, {"reject", "", false}} {
		t.Run(c.verdict, func(t *testing.T) {
			res := cardResult(c.verdict)
			if c.direction == "" {
				res.Decision.Type, res.Decision.LeftOpen = nil, nil
			}
			fc := &fakeClaims{card: outCard(true)}
			h, tp := newClaimHandler(t, &fakeCheckins{result: res}, fc)
			body := postTapThrough(t, h, tp).Body.String()
			if got := strings.Contains(body, "data-claim-card"); got != c.card {
				t.Errorf("card shown = %v, want %v", got, c.card)
			}
			if !c.card && len(fc.offers) != 0 {
				t.Errorf("an undirected tap asked for a card %d time(s)", len(fc.offers))
			}
		})
	}
}

// TestResultScreen_OnFlagTheClosingSentenceStaysBelowTheCard (product lens N5): the card sits
// between the docket and the closing sentence on both verdicts that show one, so on `flag` the
// "needs your manager's approval" sentence is read after the question, not over it.
func TestResultScreen_OnFlagTheClosingSentenceStaysBelowTheCard(t *testing.T) {
	for verdict, closing := range map[string]string{
		"ok":   "All done — you can close this page.",
		"flag": "Recorded — it needs your manager's approval before it counts toward your",
	} {
		t.Run(verdict, func(t *testing.T) {
			h, tp := newClaimHandler(t, &fakeCheckins{result: cardResult(verdict)}, &fakeClaims{card: outCard(true)})
			body := postTapThrough(t, h, tp).Body.String()
			docket, card, end := strings.Index(body, "</section>"), strings.Index(body, "data-claim-card"), strings.Index(body, closing)
			if docket < 0 || card < 0 || end < 0 || !(docket < card && card < end) {
				t.Fatalf("docket at %d, card at %d, closing sentence at %d; want docket < card < closing\n%s", docket, card, end, body)
			}
		})
	}
}

// TestResultScreen_TheCardTakesTheLeftOpenNoteOutStructurally is product lens N6 with the
// 2026-10-23 correction: with a card about the check-in this tap left open, the screen's note is
// Decision.EmployeeNoteAsked -- the same parts without the left-open one -- and every other part
// is exactly as the engine wrote it, the separator included. The shape here starts WITH the
// left-open part, so a handler that cut "; <left-open>" out of the string would leave it standing.
// Without a card, or with a card about another record, the note is the whole note.
func TestResultScreen_TheCardTakesTheLeftOpenNoteOutStructurally(t *testing.T) {
	res := cardResult("ok")
	res.Decision.EmployeeNote = noteLeftOpenScreen + "; " + tap.ChoseClockInEmployeeNote
	res.Decision.EmployeeNoteAsked = tap.ChoseClockInEmployeeNote
	other := outCard(true)
	other.AnchorID = uuid.New()
	for _, c := range []struct {
		name   string
		claims tapClaims
		note   string
	}{
		{"a card about the left-open check-in", &fakeClaims{card: outCard(true)}, tap.ChoseClockInEmployeeNote},
		{"no card", noClaims{}, noteLeftOpenScreen + "; " + tap.ChoseClockInEmployeeNote},
		{"a card about another record", &fakeClaims{card: other}, noteLeftOpenScreen + "; " + tap.ChoseClockInEmployeeNote},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, tp := newClaimHandler(t, &fakeCheckins{result: res}, c.claims)
			body := postTapThrough(t, h, tp).Body.String()
			if want := `<p class="mt-2 text-ink/70">` + c.note + `</p>`; !strings.Contains(body, want) {
				t.Fatalf("the screen's note is not %q:\n%s", c.note, body)
			}
			if c.note == tap.ChoseClockInEmployeeNote && strings.Contains(body, noteLeftOpenScreen) {
				t.Error("the left-open sentence is on the screen beside the card that asks it")
			}
		})
	}
}

// TestResultScreen_TheCardReadsNoPosition: the card's page loads no script -- the tap page's
// script, the one place a position is read (§4.2), is not on it -- and the card asks a time.
func TestResultScreen_TheCardReadsNoPosition(t *testing.T) {
	h, tp := newClaimHandler(t, &fakeCheckins{result: cardResult("flag")}, &fakeClaims{card: outCard(false)})
	body := postTapThrough(t, h, tp).Body.String()
	for _, banned := range []string{"<script", "getCurrentPosition", "tap.js", "lat", "lng"} {
		if strings.Contains(body, banned) {
			t.Errorf("the result screen with a card contains %q", banned)
		}
	}
	if !strings.Contains(body, `action="/api/claim"`) || !strings.Contains(body, `type="time"`) {
		t.Fatal("control: the card is not on the screen")
	}
}

// TestClaimCard_SaysWhichRecordItAsksAbout: ADR 0032 §C.1's sentences, by the calendar day in the
// business's zone between the record and the tap that shows the card.
func TestClaimCard_SaysWhichRecordItAsksAbout(t *testing.T) {
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	at := func(s string) time.Time { return mustTime(t, s) }
	for _, c := range []struct {
		name           string
		kind           claim.Kind
		anchor, tapAt  time.Time
		lead, question string
	}{
		{"yesterday", claim.KindOut, at("2026-07-30T07:12:00Z"), at("2026-07-31T07:00:00Z"),
			"Yesterday you clocked in at 09:12 but there's no clock-out.", "What time did you leave?"},
		{"the same day", claim.KindOut, at("2026-07-31T09:00:00Z"), at("2026-07-31T16:00:00Z"),
			"You clocked in at 11:00 and didn't clock out.", "What time did you leave?"},
		{"earlier", claim.KindOut, at("2026-07-27T07:12:00Z"), at("2026-07-31T07:00:00Z"),
			"On Mon 27 Jul you clocked in at 09:12 but there's no clock-out.", "What time did you leave?"},
		{"yesterday across a month", claim.KindOut, at("2026-07-31T20:00:00Z"), at("2026-08-01T08:00:00Z"),
			"Yesterday you clocked in at 22:00 but there's no clock-out.", "What time did you leave?"},
		{"a check-in claim, the same day", claim.KindIn, at("2026-07-31T10:00:00Z"), at("2026-07-31T14:00:00Z"),
			"You clocked out at 12:00 but there's no clock-in.", "What time did you come in?"},
		{"a check-in claim, yesterday", claim.KindIn, at("2026-07-30T10:00:00Z"), at("2026-07-31T07:00:00Z"),
			"Yesterday you clocked out at 12:00 but there's no clock-in.", "What time did you come in?"},
		{"a check-in claim, earlier", claim.KindIn, at("2026-07-27T10:00:00Z"), at("2026-07-31T07:00:00Z"),
			"On Mon 27 Jul you clocked out at 12:00 but there's no clock-in.", "What time did you come in?"},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := claimCardView(claimContext{Kind: c.kind, AnchorAt: c.anchor, TapAt: c.tapAt}, malta, "ctx", "", "")
			if v.Lead != c.lead || v.Question != c.question {
				t.Errorf("lead %q, question %q; want %q, %q", v.Lead, v.Question, c.lead, c.question)
			}
		})
	}
}

// autumnContext is a checkout claim over Malta's autumn change (Sunday 2026-10-25: 03:00 CEST
// becomes 02:00 CET, so 02:00–02:59 happens twice -- 00:00–00:59 UTC and 01:00–01:59 UTC). The
// anchor is Saturday 20:00 local (18:00 UTC), the window runs to Sunday 05:00 local, and the
// shift ends at 02:30: tap.ShiftSpanAt builds that end with time.Date, which takes the LATER of
// the two (01:30 UTC).
func autumnContext(t *testing.T) (claimContext, *time.Location) {
	t.Helper()
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 10, 25, 2, 30, 0, 0, malta)
	if !end.Equal(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("precondition: time.Date gives %v for the repeated 02:30, the fixture assumes the later instant", end.UTC())
	}
	shift := tap.Shift{Start: 18 * time.Hour, End: 2*time.Hour + 30*time.Minute, Overnight: true, Timezone: "Europe/Malta"}
	_, spanEnd, ok := tap.ShiftSpanAt(time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC), shift)
	if !ok || !spanEnd.Equal(end) {
		t.Fatalf("precondition: the shift's end is %v (%v), the prefill this fixture carries is %v", spanEnd, ok, end.UTC())
	}
	p := end.UTC()
	return claimContext{Kind: claim.KindOut, AnchorID: cardAnchorID, AnchorAt: time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC),
		Window:    claim.Window{From: claim.Bound{At: time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)}, To: claim.Bound{At: time.Date(2026, 10, 25, 4, 0, 0, 0, time.UTC)}},
		Prefilled: &p, TapAt: time.Date(2026, 10, 25, 7, 0, 0, 0, time.UTC), Zone: "Europe/Malta"}, malta
}

// TestResolveClaimedAt_TheEarlierOfARepeatedAutumnHourAndTheDefaultAsItCame is ADR 0032 §C.3's
// clock rule and the M15-16 general lens's hand-over, together: a wall clock in the repeated autumn
// hour is the EARLIER candidate inside the window; the prefill's own clock is the prefill's own
// instant (the later one), so "default not changed" holds for the value the server chose; a clock
// on either side of midnight lands on the date the window gives it; a spring clock that never
// happened, and one outside the window, have no answer.
func TestResolveClaimedAt_TheEarlierOfARepeatedAutumnHourAndTheDefaultAsItCame(t *testing.T) {
	autumn, malta := autumnContext(t)
	// The anchor at 02:20 CEST (00:20 UTC): the repeated hour's first 02:15 is BEFORE it, its second
	// (01:15 UTC, CET) inside -- the earlier candidate is outside, so the answer is the later one.
	lateOnly := claimContext{Window: claim.Window{From: claim.Bound{At: mustTime(t, "2026-10-25T00:20:00Z")}, To: claim.Bound{At: mustTime(t, "2026-10-25T04:00:00Z")}}}
	spring := claimContext{Window: claim.Window{From: claim.Bound{At: mustTime(t, "2026-03-28T20:00:00Z")}, To: claim.Bound{At: mustTime(t, "2026-03-29T04:00:00Z")}}}
	for _, c := range []struct {
		name  string
		cc    claimContext
		clock wallClock
		want  string // RFC 3339 UTC, or "" for none
	}{
		{"the default goes back as the instant it came from", autumn, wallClock{2, 30, 0}, "2026-10-25T01:30:00Z"},
		{"another clock in the repeated hour is its earlier instant", autumn, wallClock{2, 15, 0}, "2026-10-25T00:15:00Z"},
		{"the evening before midnight is Saturday", autumn, wallClock{23, 0, 0}, "2026-10-24T21:00:00Z"},
		{"after midnight is Sunday", autumn, wallClock{1, 10, 0}, "2026-10-24T23:10:00Z"},
		{"before the anchor is outside", autumn, wallClock{19, 0, 0}, ""},
		{"the earlier candidate is outside the window: the later one", lateOnly, wallClock{2, 15, 0}, "2026-10-25T01:15:00Z"},
		{"the spring clock that never happened", spring, wallClock{2, 30, 0}, ""},
		{"a spring clock after the change", spring, wallClock{3, 30, 0}, "2026-03-29T01:30:00Z"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := resolveClaimedAt(c.clock, c.cc, malta)
			if c.want == "" {
				if ok {
					t.Fatalf("resolved %v, want no instant", got)
				}
				return
			}
			if !ok || !got.Equal(mustTime(t, c.want)) {
				t.Fatalf("resolved %v (%v), want %s", got.UTC(), ok, c.want)
			}
		})
	}
	// Without the prefill, the repeated 02:30 is its earlier instant like any other clock.
	noDefault := autumn
	noDefault.Prefilled = nil
	if got, ok := resolveClaimedAt(wallClock{2, 30, 0}, noDefault, malta); !ok || !got.Equal(mustTime(t, "2026-10-25T00:30:00Z")) {
		t.Errorf("02:30 with no prefill resolved %v (%v), want the earlier instant 00:30 UTC", got.UTC(), ok)
	}
}

// claimPost posts the claim form to h with the fixed session's cookie.
func claimPost(t *testing.T, h http.Handler, form url.Values, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, ClaimPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(sessionCookie())
	for _, m := range mutate {
		m(req)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func mintClaim(t *testing.T, tp *Tap, sessionID uuid.UUID, cc claimContext) string {
	t.Helper()
	v, err := tp.claimContexts.mint(cc, sessionID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return v
}

func julyContext() claimContext {
	p := cardPrefilled
	return claimContext{Kind: claim.KindOut, AnchorID: cardAnchorID, AnchorAt: cardAnchorAt, Window: cardWindow,
		Prefilled: &p, TapAt: cardTapAt, Zone: "Europe/Malta"}
}

// TestClaimContext_RoundTripsEveryField: what the card signs is what the send reads, to the
// microsecond (the window's open ends and the prefill are compared as instants).
func TestClaimContext_RoundTripsEveryField(t *testing.T) {
	_, tp := newClaimHandler(t, &fakeCheckins{}, noClaims{})
	for _, cc := range []claimContext{julyContext(), func() claimContext {
		c := julyContext()
		c.Kind, c.Prefilled = claim.KindIn, nil
		c.AnchorAt = c.AnchorAt.Add(123456 * time.Microsecond)
		c.Window.To.Inclusive = true
		return c
	}()} {
		got, err := tp.claimContexts.parse(mintClaim(t, tp, tapFixedSessionID, cc), tapFixedSessionID)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		cc.IssuedAt = got.IssuedAt
		if got.Kind != cc.Kind || got.AnchorID != cc.AnchorID || !got.AnchorAt.Equal(cc.AnchorAt) || !got.TapAt.Equal(cc.TapAt) ||
			got.Zone != cc.Zone || got.Window.From.Inclusive != cc.Window.From.Inclusive || got.Window.To.Inclusive != cc.Window.To.Inclusive ||
			!got.Window.From.At.Equal(cc.Window.From.At) || !got.Window.To.At.Equal(cc.Window.To.At) ||
			(got.Prefilled == nil) != (cc.Prefilled == nil) || (got.Prefilled != nil && !got.Prefilled.Equal(*cc.Prefilled)) {
			t.Errorf("round trip:\n got %+v\nwant %+v", got, cc)
		}
	}
}

// TestClaim_TheAnchorAndTheKindComeOnlyFromTheSignedContext is K4 at the send (the M15-16
// security lens's hand-over): Submit is called with the anchor and the kind the card signed, and a
// form that also names another anchor, another kind or a prefill changes nothing. The answer is
// "Sent — your manager will review it.".
func TestClaim_TheAnchorAndTheKindComeOnlyFromTheSignedContext(t *testing.T) {
	fc := &fakeClaims{}
	h, tp := newClaimHandler(t, &fakeCheckins{}, fc)
	w := claimPost(t, h, url.Values{
		"ctx": {mintClaim(t, tp, tapFixedSessionID, julyContext())}, "at": {"17:00"},
		"anchor": {uuid.New().String()}, "anchor_id": {uuid.New().String()}, "kind": {"in"}, "prefilled_at": {"2026-07-30T10:00:00Z"},
	})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Sent — your manager will review it.") {
		t.Fatalf("status %d, body %s; want 200 and the Sent screen", w.Code, w.Body.String())
	}
	if len(fc.submits) != 1 {
		t.Fatalf("Submit called %d times", len(fc.submits))
	}
	got := fc.submits[0]
	if got.AnchorID != cardAnchorID || got.Kind != claim.KindOut || got.TenantID != testTenant || got.EmployeeID != testEmployee ||
		!got.ClaimedAt.Equal(cardPrefilled) {
		t.Errorf("Submit got %+v; want the signed anchor %s, kind out, the session's tenant and employee, 17:00 local", got, cardAnchorID)
	}
}

// TestClaim_ACrossLabelOrForeignContextIsRefusedAndNothingIsSent is the card's cross-label
// negative test: a tap context at /api/claim and a claim context at /api/checkin are refused, and
// so is a value FORGED for one under the other's signer (the payload shaped right, the key and the
// label wrong -- only domain separation refuses it); another session's claim context is a 400; an
// unsigned one too. Nothing reaches Submit or Record.
func TestClaim_ACrossLabelOrForeignContextIsRefusedAndNothingIsSent(t *testing.T) {
	fc := &fakeClaims{}
	svc := &fakeCheckins{result: cardResult("ok")}
	h, tp := newClaimHandler(t, svc, fc)
	cc := julyContext()
	tapCtx := mintedContext(t, tp, tapFixedSessionID, tapContext{UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
		TagTenantID: testTenant, LocationID: tapLocation})
	forge := func(payload string, sign func(string, uuid.UUID) []byte) string {
		return b64(payload) + "." + b64(string(sign(payload, tapFixedSessionID)))
	}
	cc.IssuedAt = time.Now().Truncate(time.Second)
	claimShaped := forge(tp.claimContexts.payload(cc), tp.contexts.sign)
	tapShaped := forge(tp.contexts.payload(tapContext{UID: tapUID, Ctr: 642, Channel: "nfc", CMACVerified: true,
		TagTenantID: testTenant, LocationID: tapLocation, IssuedAt: time.Now()}), tp.claimContexts.sign)
	other := uuid.MustParse("12121212-1212-4212-8212-121212121212")
	for name, v := range map[string]string{
		"a tap context":                          tapCtx,
		"a claim payload under the tap's signer": claimShaped,
		"another session's claim context":        mintClaim(t, tp, other, julyContext()),
		"an unsigned value":                      b64(tp.claimContexts.payload(cc)) + ".AAAA",
		"nothing":                                "",
	} {
		t.Run("at /api/claim: "+name, func(t *testing.T) {
			w := claimPost(t, h, url.Values{"ctx": {v}, "at": {"17:00"}})
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "This question has expired") {
				t.Fatalf("status %d; want 400 and the expired screen", w.Code)
			}
		})
	}
	if fc.submitCount() != 0 {
		t.Fatalf("Submit was called %d times", fc.submitCount())
	}
	for name, v := range map[string]string{
		"a claim context":                        mintClaim(t, tp, tapFixedSessionID, julyContext()),
		"a tap payload under the claim's signer": tapShaped,
	} {
		t.Run("at /api/checkin: "+name, func(t *testing.T) {
			w := postForm(t, h, url.Values{"ctx": {v}}, sessionCookie())
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d; want 400", w.Code)
			}
		})
	}
	if len(svc.calls) != 0 {
		t.Fatalf("Record was called %d times", len(svc.calls))
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// TestClaim_AnExpiredContextSaysTheQuestionHasExpired: fifteen minutes after the card was drawn
// its context is refused -- "This question has expired" -- and nothing is sent.
func TestClaim_AnExpiredContextSaysTheQuestionHasExpired(t *testing.T) {
	fc := &fakeClaims{}
	h, tp := newClaimHandler(t, &fakeCheckins{}, fc)
	minted := time.Now()
	tp.claimContexts.now = func() time.Time { return minted }
	v := mintClaim(t, tp, tapFixedSessionID, julyContext())
	tp.claimContexts.now = func() time.Time { return minted.Add(claimContextTTL + time.Second) }
	w := claimPost(t, h, url.Values{"ctx": {v}, "at": {"17:00"}})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "This question has expired") || fc.submitCount() != 0 {
		t.Fatalf("status %d, submits %d; want 400, the expired screen and nothing sent", w.Code, fc.submitCount())
	}
	tp.claimContexts.now = func() time.Time { return minted.Add(claimContextTTL - time.Second) }
	if w := claimPost(t, h, url.Values{"ctx": {v}, "at": {"17:00"}}); w.Code != http.StatusOK {
		t.Fatalf("control: inside the fifteen minutes the send is %d", w.Code)
	}
}

// TestClaim_EveryRefusalHasItsOwnAnswer is the M15-16 hand-over's error map (ADR 0032 §C.5): a
// second send is the Sent screen (200, never a 500); a time outside the window shows the form
// again; an expired one says the question has expired; a resolved record and an inactive person
// have their own sentences; anything else is a safe general screen. None of them says nothing was
// recorded -- the tap's own record is.
func TestClaim_EveryRefusalHasItsOwnAnswer(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		says   string
	}{
		{nil, http.StatusOK, "Sent — your manager will review it."},
		{claim.ErrAlreadySent, http.StatusOK, "Sent — your manager will review it."},
		{claim.ErrOutsideWindow, http.StatusUnprocessableEntity, "That time does not fit this record. It has to be between 09:12 and 05:00."},
		{claim.ErrExpired, http.StatusGone, "This question has expired"},
		{claim.ErrResolved, http.StatusConflict, "Already resolved"},
		{claim.ErrNotActive, http.StatusForbidden, "Only an active employee can send a time to their manager."},
		{claim.ErrUnknownAnchor, http.StatusInternalServerError, "Something went wrong on our side"},
		{errors.New("connection reset by peer"), http.StatusInternalServerError, "Something went wrong on our side"},
	} {
		name := "sent"
		if c.err != nil {
			name = c.err.Error()
		}
		t.Run(name, func(t *testing.T) {
			fc := &fakeClaims{submitErr: c.err}
			h, tp := newClaimHandler(t, &fakeCheckins{}, fc)
			w := claimPost(t, h, url.Values{"ctx": {mintClaim(t, tp, tapFixedSessionID, julyContext())}, "at": {"17:00"}})
			body := w.Body.String()
			if w.Code != c.status || !strings.Contains(body, c.says) {
				t.Fatalf("status %d; want %d and %q in\n%s", w.Code, c.status, c.says, body)
			}
			if strings.Contains(body, "Nothing was recorded") {
				t.Error("a claim's answer says nothing was recorded; the tap was")
			}
			if c.err == claim.ErrOutsideWindow && (!strings.Contains(body, `action="/api/claim"`) || !strings.Contains(body, `value="17:00"`)) {
				t.Error("the form is not shown again with the time that was sent")
			}
		})
	}
}

// TestClaim_ATimeTheFormCannotTakeShowsTheFormAgain: an empty field, a value that is not a time,
// two values, and a time outside the signed window are answered with the form, its context
// intact. Submit is asked with NO moment (the zero time) -- so that "already sent" is answered
// before the window -- and nothing else.
func TestClaim_ATimeTheFormCannotTakeShowsTheFormAgain(t *testing.T) {
	fc := &fakeClaims{}
	h, tp := newClaimHandler(t, &fakeCheckins{}, fc)
	v := mintClaim(t, tp, tapFixedSessionID, julyContext())
	for name, at := range map[string][]string{
		"empty": {""}, "not a time": {"5pm"}, "two values": {"17:00", "18:00"}, "absent": nil,
		"before the anchor": {"08:00"},
	} {
		t.Run(name, func(t *testing.T) {
			form := url.Values{"ctx": {v}}
			if at != nil {
				form["at"] = at
			}
			w := claimPost(t, h, form)
			body := w.Body.String()
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `action="/api/claim"`) || !strings.Contains(body, `value="`+v+`"`) {
				t.Fatalf("status %d; want 422 and the form with its context\n%s", w.Code, body)
			}
		})
	}
	for _, c := range fc.submits {
		if !c.ClaimedAt.IsZero() {
			t.Errorf("Submit was given the moment %v for a time the form could not take", c.ClaimedAt)
		}
	}
}

// TestClaim_TheAutumnDefaultGoesBackAsTheInstantItCameFrom is the hand-over's DST case through the
// handler: the card for a shift ending at the repeated 02:30 shows "02:30"; sent unchanged it is
// the prefill's own (later) instant -- the one Submit compares for "default not changed" -- and
// 02:15 is the earlier instant of 02:15.
func TestClaim_TheAutumnDefaultGoesBackAsTheInstantItCameFrom(t *testing.T) {
	cc, malta := autumnContext(t)
	if got := cc.Prefilled.In(malta).Format("15:04"); got != "02:30" {
		t.Fatalf("precondition: the card shows %s", got)
	}
	for clock, want := range map[string]string{"02:30": "2026-10-25T01:30:00Z", "02:15": "2026-10-25T00:15:00Z"} {
		fc := &fakeClaims{}
		h, tp := newClaimHandler(t, &fakeCheckins{}, fc)
		if w := claimPost(t, h, url.Values{"ctx": {mintClaim(t, tp, tapFixedSessionID, cc)}, "at": {clock}}); w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", clock, w.Code)
		}
		if len(fc.submits) != 1 || !fc.submits[0].ClaimedAt.Equal(mustTime(t, want)) {
			t.Errorf("%s was sent as %v, want %s", clock, fc.submits, want)
		}
	}
}

// TestClaim_TheSurfaceRefusesWhatCheckinRefuses: a cross-origin post is a 403, a request with no
// session goes to activation -- POST /api/checkin's rules -- and neither reaches Submit.
func TestClaim_TheSurfaceRefusesWhatCheckinRefuses(t *testing.T) {
	fc := &fakeClaims{}
	h, tp := newClaimHandler(t, &fakeCheckins{}, fc)
	form := url.Values{"ctx": {mintClaim(t, tp, tapFixedSessionID, julyContext())}, "at": {"17:00"}}
	if w := claimPost(t, h, form, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }); w.Code != http.StatusForbidden {
		t.Errorf("cross-origin: status %d, want 403", w.Code)
	}
	if w := claimPost(t, h, form, func(r *http.Request) { r.Header.Set("Origin", "http://localhost:8080") }); w.Code != http.StatusOK {
		t.Errorf("same origin: status %d, want 200", w.Code)
	}
	fc.submits = nil
	req := httptest.NewRequest(http.MethodPost, ClaimPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	hNoSession, _ := newTapHandlerWithClaims(t, fc)
	hNoSession.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/activate") {
		t.Errorf("no session: status %d to %q, want 303 to activation", w.Code, w.Header().Get("Location"))
	}
	if fc.submitCount() != 0 {
		t.Errorf("Submit was called %d times", fc.submitCount())
	}
}

// newTapHandlerWithClaims is a Tap whose sessions resolve nobody.
func newTapHandlerWithClaims(t *testing.T, claims tapClaims) (http.Handler, *Tap) {
	t.Helper()
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		&fakeSessions{}, &fakeCheckins{}, claims, noActivation{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	r := chi.NewRouter()
	tp.Mount(r)
	return r, tp
}

// TestNewTap_BoundsTheClaimCardRead pins the card's read bound (resultClaimWait, G-O5): 2 s, the
// field NewTap sets, and the two post-commit reads together -- the brand's and the card's, one
// after the other -- at most a fifth of httpx.RequestTimeout.
func TestNewTap_BoundsTheClaimCardRead(t *testing.T) {
	_, tp := newClaimHandler(t, &fakeCheckins{}, noClaims{})
	if tp.claimWait != resultClaimWait || resultClaimWait != 2*time.Second {
		t.Fatalf("claimWait = %v, resultClaimWait = %v; want both 2 s", tp.claimWait, resultClaimWait)
	}
	if (resultBrandWait+resultClaimWait)*5 > httpx.RequestTimeout {
		t.Fatalf("the two post-commit reads (%v) are more than a fifth of httpx.RequestTimeout %v",
			resultBrandWait+resultClaimWait, httpx.RequestTimeout)
	}
}

// TestClaim_ASecondSendIsSentWhateverItsTime is ADR 0032 §C.4 note (5) through the handler (the
// M15-17 security lens's spec deviation): once a claim is on the record, a second send is "Sent"
// even when its time is outside the window, empty or not a time -- "already sent" is asked before
// the window, never after it.
func TestClaim_ASecondSendIsSentWhateverItsTime(t *testing.T) {
	for name, at := range map[string]string{"outside the window": "08:00", "empty": "", "not a time": "5pm", "inside": "17:00"} {
		t.Run(name, func(t *testing.T) {
			fc := &fakeClaims{sent: true}
			h, tp := newClaimHandler(t, &fakeCheckins{}, fc)
			w := claimPost(t, h, url.Values{"ctx": {mintClaim(t, tp, tapFixedSessionID, julyContext())}, "at": {at}})
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Sent — your manager will review it.") {
				t.Fatalf("status %d; want 200 and the Sent screen\n%s", w.Code, w.Body.String())
			}
		})
	}
}

// panickyBrand is a directory whose brand read panics.
type panickyBrand struct{ *fakeDirectory }

func (panickyBrand) ResultBrand(context.Context, uuid.UUID) (tenant.PageBrand, error) {
	panic("panickyBrand: ResultBrand panicked")
}

// TestResultScreen_APanickingPostCommitReadDrawsTheScreenWithoutIt (M15-17 fix round, security
// lens): the card's read and the brand's read run after the record committed, so a panic in either
// costs that read -- 200, the whole screen without it, the one record -- and never reaches the
// router's Recoverer as a 500 over a recorded tap.
func TestResultScreen_APanickingPostCommitReadDrawsTheScreenWithoutIt(t *testing.T) {
	res := cardResult("ok")
	hPlain, tpPlain := newClaimHandler(t, &fakeCheckins{result: res}, noClaims{})
	want := postTapThrough(t, hPlain, tpPlain).Body.String()
	for name, build := range map[string]func(svc *fakeCheckins) (http.Handler, *Tap){
		"the card's read": func(svc *fakeCheckins) (http.Handler, *Tap) {
			return newClaimHandler(t, svc, &fakeClaims{offerPanic: true})
		},
		"the brand's read": func(svc *fakeCheckins) (http.Handler, *Tap) {
			tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, panickyBrand{&fakeDirectory{facts: okFacts()}},
				fixedSessions(), svc, noClaims{}, noActivation{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			return mountTap(tp), tp
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &fakeCheckins{result: res}
			h, tp := build(svc)
			var w *httptest.ResponseRecorder
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("the panic left the handler: %v", p)
					}
				}()
				w = postTapThrough(t, h, tp)
			}()
			if w.Code != http.StatusOK || w.Body.String() != want || len(svc.calls) != 1 {
				t.Fatalf("status %d, %d records, body:\n%s\nwant 200, one record and the screen without it:\n%s",
					w.Code, len(svc.calls), w.Body.String(), want)
			}
		})
	}
}

// TestResultScreen_ARecordCommittedAtTheDeadlineKeepsItsCard is
// TestResultScreen_ARecordCommittedAtTheDeadlineKeepsItsLogo's twin for the card (M15-17 fix round,
// R15): the record commits after the request deadline (chi's Timeout at 800 ms, the commit at 1 s)
// and the card's read is healthy, so the card is drawn. The read runs on the request's values
// without its cancellation; one derived from the request's context would find it already ended
// and drop the card of a tap that counted.
func TestResultScreen_ARecordCommittedAtTheDeadlineKeepsItsCard(t *testing.T) {
	svc := &fakeCheckins{result: cardResult("ok")}
	fc := &fakeClaims{card: outCard(true)}
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, fixedSessions(),
		lateCommit{svc, time.Second}, fc, noActivation{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	h := httpx.RequestID(middleware.Timeout(800 * time.Millisecond)(mountTap(tp)))
	form := url.Values{"ctx": {mintedContext(t, tp, tapFixedSessionID, tapContext{
		UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true, TagTenantID: testTenant, LocationID: tapLocation,
	})}}
	req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(sessionCookie())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "data-claim-card") || !strings.Contains(body, "All done — you can close this page.") {
		t.Fatalf("status %d: the card of a record committed past the deadline is missing:\n%s", rec.Code, body)
	}
	if len(svc.calls) != 1 || len(fc.offers) != 1 {
		t.Fatalf("record %d, card read %d; want 1 and 1", len(svc.calls), len(fc.offers))
	}
}

// TestClaimContext_LivesFifteenMinutes pins the card's lifetime as a NUMBER (ADR 0032 §C.3: 15 min;
// M15-17 fix round, R14): a context minted 14 m 59 s ago is read, one minted 15 m 01 s ago is "this
// question has expired". Measured against the literal, not against claimContextTTL -- a test that
// aged the context by the constant followed the constant wherever it moved.
func TestClaimContext_LivesFifteenMinutes(t *testing.T) {
	_, tp := newClaimHandler(t, &fakeCheckins{}, noClaims{})
	minted := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	tp.claimContexts.now = func() time.Time { return minted }
	v := mintClaim(t, tp, tapFixedSessionID, julyContext())
	for age, want := range map[time.Duration]error{
		15*time.Minute - time.Second: nil,
		15*time.Minute + time.Second: errClaimContextExpired,
	} {
		tp.claimContexts.now = func() time.Time { return minted.Add(age) }
		if _, err := tp.claimContexts.parse(v, tapFixedSessionID); !errors.Is(err, want) && !(want == nil && err == nil) {
			t.Errorf("a context %v old: %v, want %v", age, err, want)
		}
	}
}

// TestClaimCard_TheFieldIsDescribedAndMarkedOnlyOnAProblem (M15-17 fix round, accessibility): the
// time field is the shared form field, described by the lead naming the record and, when the form
// is shown again, by the problem sentence; only then is it marked invalid and focused -- by plain
// HTML, no script -- and the page has its own heading. On the result screen there is no problem,
// no invalid mark and no focus taken from the confirmation.
func TestClaimCard_TheFieldIsDescribedAndMarkedOnlyOnAProblem(t *testing.T) {
	h, tp := newClaimHandler(t, &fakeCheckins{result: cardResult("ok")}, &fakeClaims{card: outCard(true)})
	result := postTapThrough(t, h, tp).Body.String()
	field := regexp.MustCompile(`<input id="claim-at"[^>]*>`)
	f := field.FindString(result)
	if !strings.Contains(f, `aria-describedby="claim-lead"`) || !strings.Contains(f, `class="field-input field-input--mono field-input--claim"`) ||
		strings.Contains(f, "aria-invalid") || strings.Contains(f, "autofocus") || !strings.Contains(result, `<p id="claim-lead"`) {
		t.Errorf("the result screen's field: %s", f)
	}
	again := claimPost(t, h, url.Values{"ctx": {mintClaim(t, tp, tapFixedSessionID, julyContext())}, "at": {"08:00"}}).Body.String()
	f = field.FindString(again)
	if !strings.Contains(f, `aria-describedby="claim-lead claim-problem"`) || !strings.Contains(f, `aria-invalid="true"`) ||
		!strings.Contains(f, "autofocus") || !strings.Contains(again, `<p id="claim-problem"`) || !strings.Contains(again, "<h1") {
		t.Errorf("the form shown again: field %s\n%s", f, again)
	}
	if strings.Contains(again, "<script") || strings.Contains(result, "<script") {
		t.Error("a claim screen loads a script")
	}
}

// fontSizePx reads a font-size value in px or rem (16 px to the rem, Tailwind's base).
func fontSizePx(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	for unit, scale := range map[string]float64{"px": 1, "rem": 16} {
		if n, ok := strings.CutSuffix(v, unit); ok {
			f, err := strconv.ParseFloat(n, 64)
			return f * scale, err == nil
		}
	}
	return 0, false
}

// TestCompiledCSS_TheClaimFieldIsSixteenPixels (M15-17 closing round, orchestrator decision): the
// claim card's time field is on the employee's own phone, and iOS Safari zooms the page into a
// field whose text is under 16 px -- which .field-input's panel size (14 px) is. The card's field
// carries .field-input--claim, the stylesheet's source declares it the 16 px size, and, wherever a
// stylesheet has been built, the compiled rule sets a font-size of at least 16 px and comes AFTER
// .field-input's (same specificity: the later rule wins). The panel's .field-input is not touched.
func TestCompiledCSS_TheClaimFieldIsSixteenPixels(t *testing.T) {
	src, err := fs.ReadFile(web.Static(), "css/input.css")
	if err != nil {
		t.Fatalf("read the stylesheet's source: %v", err)
	}
	if !regexp.MustCompile(`\.field-input--claim\s*\{\s*@apply\s+text-(base|lg|xl)\s*;\s*\}`).Match(src) {
		t.Fatal("input.css does not declare .field-input--claim at text-base (16 px) or larger")
	}
	h, tp := newClaimHandler(t, &fakeCheckins{result: cardResult("ok")}, &fakeClaims{card: outCard(true)})
	if f := regexp.MustCompile(`<input id="claim-at"[^>]*>`).FindString(postTapThrough(t, h, tp).Body.String()); !strings.Contains(f, "field-input--claim") {
		t.Fatalf("the card's field does not carry the 16 px modifier: %s", f)
	}
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Logf("no compiled stylesheet (%v): the source and the markup were checked, the compiled rule was not", err)
		return
	}
	css := string(raw)
	base, claimAt := strings.Index(css, ".field-input{"), strings.Index(css, ".field-input--claim{")
	if base < 0 || claimAt < 0 || claimAt < base {
		t.Fatalf(".field-input at %d, .field-input--claim at %d: the modifier must be compiled, after the field", base, claimAt)
	}
	decls := css[claimAt : claimAt+strings.Index(css[claimAt:], "}")]
	m := regexp.MustCompile(`font-size:([^;}]*)`).FindStringSubmatch(decls)
	if m == nil {
		t.Fatalf("the compiled .field-input--claim sets no font-size: %s}", decls)
	}
	if px, ok := fontSizePx(m[1]); !ok || px < 16 {
		t.Fatalf("the claim field's font-size is %q (%v px): under 16 px a phone zooms into it", m[1], px)
	}
}
