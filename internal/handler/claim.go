package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/claim"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The result screen's claim card and POST /api/claim (ADR 0032 Faz C, §C.1–§C.5; M15-17, the
// user's §9 exception: one optional card, "Send to my manager", no skip button).
//
// A CLAIM IS NOT A RECORD (§C.1), and this file is a boundary like checkin.go: it parses, hands
// typed facts to internal/domain/claim and renders. Whether a record still misses its half, its
// window, the two showings (K4) and the prefill are the domain's.
//
// WHERE THE CARD IS READ (security lens G-O5). Not inside the tap's transaction -- a failing read
// there would roll the record back (internal/domain/checkin's gatherPerson, 25P02) -- and not under
// the request's context either -- a read that outlived the request deadline would leave a 200 with
// an EMPTY body over a written record (WL-9 S1, resultBrandWait). It is read AFTER Record returned,
// on the request's values without its cancellation, bounded by claimWait; an error or the bound
// drops the card and the result screen is drawn exactly as it would be without one (§4.6: the
// record and its confirmation never depend on the card).
//
// SUSPENSION (ADR 0025): POST /api/claim is a tap surface and does not know it, like the tap it
// follows. A claim writes no hour; only a manager's decision does, and that is a panel write.

// tapClaims is the slice of internal/domain/claim this surface needs (§7: at the consumer).
type tapClaims interface {
	Offer(ctx context.Context, tenantID, employeeID, tapID uuid.UUID) (*claim.Card, error)
	Submit(ctx context.Context, c claim.Claim) (claim.Submitted, error)
}

// ClaimPath is the card's form action.
const ClaimPath = "/api/claim"

// claimMaxBody bounds the claim form: a signed context of about 300 bytes and a time.
const claimMaxBody = 8 << 10

// claimAtField is the time the person sent: a wall clock in the business's zone.
const claimAtField = "at"

// resultClaimWait bounds the card's read on the result screen, resultBrandWait's reasoning and
// value: the read is a handful of indexed lookups on one person's two days (claim.Offer), three
// orders of magnitude under 2 s when healthy, and past 2 s the card is the thing to drop. The two
// reads run one after the other, so the confirmation can outlive the record by brandWait plus this
// plus the render: 4 s against httpx.RequestTimeout's 30 s (TestNewTap_BoundsTheClaimCardRead holds
// the two together at most a fifth of it).
const resultClaimWait = 2 * time.Second

// The claim's failure screens: Taptime's own look (pages.Problem), each true of THIS answer. None
// says "nothing was recorded": the tap was recorded before the card was drawn, and only the claim
// is in question here.
var (
	claimProblemExpired = pages.ProblemView{
		Title:   "This question has expired",
		Message: "Nothing was sent to your manager. Your tap itself is recorded.",
		Hint:    "Tell your manager what time it was.",
	}
	claimProblemResolved = pages.ProblemView{
		Title:   "Already resolved",
		Message: "That record no longer needs this time, so nothing was sent. Your tap itself is recorded.",
		Hint:    "Tell your manager if that looks wrong.",
	}
	claimProblemNotActive = pages.ProblemView{
		Title:   "Your account can't send this",
		Message: "Only an active employee can send a time to their manager. Nothing was sent.",
		Hint:    "Tell your manager what time it was.",
	}
	claimProblemBad = pages.ProblemView{
		Title:   "That didn't work",
		Message: "Nothing was sent to your manager. Your tap itself is recorded.",
		Hint:    "Tell your manager what time it was.",
	}
	claimProblemServer = pages.ProblemView{
		Title:   "Something went wrong on our side",
		Message: "Nothing was sent to your manager. Your tap itself is recorded.",
		Hint:    "Tell your manager what time it was.",
	}
)

// afterCommit runs one of the result screen's post-commit reads -- the brand, the claim card -- and
// answers its value, or the zero value when the read panics: the record is written and its
// confirmation does not depend on either read (G-O5, §4.6), so a panic in one of them costs that
// read and is logged, never the screen. Without it the panic unwound to the router's Recoverer and
// the person got a 500 over a tap that WAS recorded (M15-17 fix round, security lens). The log
// carries the panic's type and the stack -- function names, no value the panic was raised with.
func afterCommit[T any](ctx context.Context, t *Tap, what string, read func() T) (out T) {
	defer func() {
		if p := recover(); p != nil {
			var zero T
			out = zero
			t.log.ErrorContext(ctx, "checkin: a post-commit read panicked; drawing the screen without it",
				"read", what, "panic_type", fmt.Sprintf("%T", p), "stack", string(debug.Stack()))
		}
	}()
	return read()
}

// resultCard is the card the result screen draws and whether it asks about the check-in this very
// tap left open -- the one case its left-open note is not shown (product lens N6).
type resultCard struct {
	view         pages.ClaimCardView
	asksLeftOpen bool
}

// resultClaim is the card for this tap's result screen, or nil (ADR 0032 §C.1). ctx is the caller's:
// the request's values without its cancellation, bounded by claimWait.
//
// Only a directed record can show one (ok, flag): an ignored or refused tap is not in the chain and
// is never a showing (K4) -- Offer says the same; asking it is a read this screen does not need.
func (t *Tap) resultClaim(ctx context.Context, id httpx.Identity, res checkin.Result) *resultCard {
	if res.TransactionID == uuid.Nil || res.Decision.Type == nil || res.Decision.Practice {
		return nil
	}
	card, err := t.claims.Offer(ctx, id.Session.TenantID, id.Session.EmployeeID, res.TransactionID)
	if err != nil {
		t.log.ErrorContext(ctx, "checkin: reading the claim card failed; drawing the screen without it",
			"tenant_id", id.TenantID(), "employee_id", id.EmployeeID(), "transaction_id", res.TransactionID, "err", err)
		return nil
	}
	if card == nil {
		return nil
	}
	loc, err := time.LoadLocation(res.Timezone)
	if err != nil || res.Timezone == "" {
		// The card names wall clocks and reads one back; with no zone it can do neither honestly.
		t.log.ErrorContext(ctx, "checkin: no zone for the claim card; drawing the screen without it",
			"tenant_id", id.TenantID(), "employee_id", id.EmployeeID())
		return nil
	}
	cc := claimContext{Kind: card.Kind, AnchorID: card.AnchorID, AnchorAt: card.AnchorAt, Window: card.Window,
		Prefilled: card.Prefilled, TapAt: res.OccurredAt, Zone: res.Timezone}
	signed, err := t.claimContexts.mint(cc, id.Session.ID)
	if err != nil {
		t.log.ErrorContext(ctx, "checkin: signing the claim card failed; drawing the screen without it",
			"employee_id", id.EmployeeID(), "err", err)
		return nil
	}
	value := ""
	if card.Prefilled != nil {
		value = card.Prefilled.In(loc).Format("15:04")
	}
	out := &resultCard{view: claimCardView(cc, loc, signed, value, "")}
	lo := res.Decision.LeftOpen
	out.asksLeftOpen = card.Kind == claim.KindOut && lo != nil && lo.ID == card.AnchorID
	return out
}

// claimCardView writes the card's words (ADR 0032 §C.1's sentences, and the check-in card's lead
// by the user's decision of 2026-10-26, "Cümle ekle") from the signed facts:
//
//	checkout claim, same calendar day   "You clocked in at 11:00 and didn't clock out."
//	checkout claim, the day before      "Yesterday you clocked in at 09:12 but there's no clock-out."
//	checkout claim, earlier             "On Mon 19 Oct you clocked in at 09:12 but there's no clock-out."
//	                                    then "What time did you leave?"
//	check-in claim, same calendar day   "You clocked out at 12:00 but there's no clock-in."
//	check-in claim, the day before      "Yesterday you clocked out at 12:00 but there's no clock-in."
//	check-in claim, earlier             "On Mon 27 Jul you clocked out at 12:00 but there's no clock-in."
//	                                    then "What time did you come in?"
//
// Every check-in card names its record, the first showing as the second: on the second (the day's
// next `out`) "What time did you come in?" alone would not say WHICH clock-out has no clock-in.
// The day is the calendar day in the business's zone -- what "yesterday" means to the person --
// compared between the anchor and the tap that shows the card.
func claimCardView(cc claimContext, loc *time.Location, signed, value, problem string) pages.ClaimCardView {
	v := pages.ClaimCardView{Context: signed, Value: value, Problem: problem}
	a, tp := cc.AnchorAt.In(loc), cc.TapAt.In(loc)
	clock := a.Format("15:04")
	ay, am, ad := a.Date()
	ty, tm, td := tp.Date()
	py, pm, pd := time.Date(ty, tm, td-1, 12, 0, 0, 0, loc).Date()
	sameDay := ay == ty && am == tm && ad == td
	yesterday := ay == py && am == pm && ad == pd
	if cc.Kind == claim.KindIn {
		v.Question = "What time did you come in?"
		switch {
		case sameDay:
			v.Lead = "You clocked out at " + clock + " but there's no clock-in."
		case yesterday:
			v.Lead = "Yesterday you clocked out at " + clock + " but there's no clock-in."
		default:
			v.Lead = "On " + a.Format("Mon 2 Jan") + " you clocked out at " + clock + " but there's no clock-in."
		}
		return v
	}
	v.Question = "What time did you leave?"
	switch {
	case sameDay:
		v.Lead = "You clocked in at " + clock + " and didn't clock out."
	case yesterday:
		v.Lead = "Yesterday you clocked in at " + clock + " but there's no clock-out."
	default:
		v.Lead = "On " + a.Format("Mon 2 Jan") + " you clocked in at " + clock + " but there's no clock-out."
	}
	return v
}

// wallClockRE is the time field's value: HH:MM, or HH:MM:SS from a browser with a finer step.
var wallClockRE = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])(?::([0-5][0-9]))?$`)

// wallClock is a time of day the person sent.
type wallClock struct{ h, m, s int }

func (c wallClock) String() string {
	return time.Date(2000, 1, 1, c.h, c.m, c.s, 0, time.UTC).Format("15:04")
}

// parseWallClock reads the time field: exactly one value, HH:MM[:SS]. Anything else is no time.
func parseWallClock(values []string) (wallClock, bool) {
	if len(values) != 1 {
		return wallClock{}, false
	}
	m := wallClockRE.FindStringSubmatch(strings.TrimSpace(values[0]))
	if m == nil {
		return wallClock{}, false
	}
	h, _ := strconv.Atoi(m[1]) // the expression admits digits only
	mi, _ := strconv.Atoi(m[2])
	s := 0
	if m[3] != "" {
		s, _ = strconv.Atoi(m[3])
	}
	return wallClock{h, mi, s}, true
}

// resolveClaimedAt turns the person's wall clock into the instant they mean (ADR 0032 §C.3).
//
//  1. THE DEFAULT GOES BACK AS IT CAME: when the clock is the prefill's own wall clock, the answer
//     is the prefill's instant. A shift ending in the autumn hour that happens twice has its end
//     on that hour's LATER instant (tap.ShiftSpanAt), and only this keeps "default not changed"
//     true for the value the server chose (the M15-16 general lens's hand-over).
//  2. OTHERWISE every instant whose wall clock in the zone is this one, on a calendar date the
//     window touches, and inside the window -- the EARLIEST of them. A window is at most 18 h, so
//     two dates cannot both hold the same clock; the only second candidate is the repeated autumn
//     hour, and §C.3 takes the earlier. A spring clock that never happened has no candidate.
//
// ok is false when no candidate is inside the window: the form is shown again.
func resolveClaimedAt(c wallClock, cc claimContext, loc *time.Location) (time.Time, bool) {
	if p := cc.Prefilled; p != nil && c.s == 0 && p.In(loc).Format("15:04:05") == time.Date(2000, 1, 1, c.h, c.m, 0, 0, time.UTC).Format("15:04:05") && cc.Window.Contains(*p) {
		return *p, true
	}
	var best time.Time
	found := false
	from := cc.Window.From.At.In(loc)
	last := cc.Window.To.At.In(loc)
	y, mo, d := from.Date()
	ly, lm, ld := last.Date()
	for i := 0; i < 3; i++ {
		day := time.Date(y, mo, d+i, 12, 0, 0, 0, loc)
		dy, dm, dd := day.Date()
		for _, cand := range wallClockInstants(dy, dm, dd, c, loc) {
			if cc.Window.Contains(cand) && (!found || cand.Before(best)) {
				best, found = cand, true
			}
		}
		if dy == ly && dm == lm && dd == ld {
			break
		}
	}
	return best, found
}

// wallClockInstants is every instant whose wall clock in loc is (y, m, d, c): none for a clock the
// spring change skipped, two for the hour the autumn change repeats, one otherwise. time.Date picks
// one of two (Go documents the choice as not guaranteed), so the candidates are built from every
// offset the zone uses around that day and kept when they read back as the same wall clock.
func wallClockInstants(y int, m time.Month, d int, c wallClock, loc *time.Location) []time.Time {
	naive := time.Date(y, m, d, c.h, c.m, c.s, 0, time.UTC)
	var out []time.Time
	seen := map[int]bool{}
	for _, probe := range []time.Duration{-14 * time.Hour, 0, 14 * time.Hour} {
		_, off := naive.Add(probe).In(loc).Zone()
		if seen[off] {
			continue
		}
		seen[off] = true
		cand := naive.Add(-time.Duration(off) * time.Second)
		l := cand.In(loc)
		ly, lm, ld := l.Date()
		if ly == y && lm == m && ld == d && l.Hour() == c.h && l.Minute() == c.m && l.Second() == c.s {
			out = append(out, cand.UTC())
		}
	}
	return out
}

// Claim serves POST /api/claim (ADR 0032 §C.3, §C.5): the card's answer, sent to the manager.
func (t *Tap) Claim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // checkin.go's binding: one more level of paren depth for R7/R7b/R7c.
	id := httpx.IdentityOf(r)
	switch id.State {
	case httpx.SessionUnresolved:
		t.log.ErrorContext(ctx, "claim: no resolved identity on the request",
			"hint", "mount httpx.Identify in front of POST /api/claim", "err", id.Err, "path", r.URL.Path)
		t.renderProblem(w, r, http.StatusInternalServerError, claimProblemServer)
		return
	case httpx.SessionAbsent, httpx.SessionRevoked:
		// The claim context is signed over the session id; with no session there is nothing to
		// verify it with and nobody to attribute a claim to -- checkin.go's answer.
		t.redirectToActivation(w, r)
		return
	case httpx.SessionLive:
	default:
		t.log.ErrorContext(ctx, "claim: unknown session state", "state", int(id.State))
		t.renderProblem(w, r, http.StatusInternalServerError, claimProblemServer)
		return
	}
	// checkin.go's Origin rule, for its reasons: depth, not the defence (the session-bound MAC is).
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" &&
		!strings.EqualFold(strings.TrimRight(origin, "/"), strings.TrimRight(t.baseURL, "/")) {
		t.log.WarnContext(ctx, "claim: refusing a cross-origin post", "employee_id", id.EmployeeID())
		t.renderProblem(w, r, http.StatusForbidden, claimProblemBad)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, claimMaxBody)
	if err := r.ParseForm(); err != nil {
		t.log.InfoContext(ctx, "claim: unusable form body", "err", err)
		t.renderProblem(w, r, http.StatusBadRequest, claimProblemBad)
		return
	}
	signed := r.PostFormValue("ctx")
	cc, err := t.claimContexts.parse(signed, id.Session.ID)
	if err != nil {
		// Expired, tampered with, another phone's, or a tap context posted here: one screen for all
		// (tapProblemStale's no-oracle rule); the log tells them apart. Nothing is written.
		if errors.Is(err, errClaimContextExpired) {
			t.log.InfoContext(ctx, "claim: expired claim context", "employee_id", id.EmployeeID())
		} else {
			t.log.WarnContext(ctx, "claim: unusable claim context", "employee_id", id.EmployeeID(), "err", err)
		}
		t.renderProblem(w, r, http.StatusBadRequest, claimProblemExpired)
		return
	}
	loc, err := time.LoadLocation(cc.Zone)
	if err != nil {
		t.log.ErrorContext(ctx, "claim: the signed zone does not load", "employee_id", id.EmployeeID())
		t.renderProblem(w, r, http.StatusInternalServerError, claimProblemServer)
		return
	}
	// "ALREADY SENT" IS ASKED BEFORE THE WINDOW (ADR 0032 §C.4 note (5); M15-17 fix round): a time
	// the form could not take -- empty, not a time, or outside the signed window -- still goes to
	// Submit, as no moment (the zero time). Submit answers "already sent" before it looks at the
	// moment, so a second send is "Sent" whatever its time, and otherwise "outside", which shows
	// the form again with the sentence that fits.
	value, problem := "", "Enter the time, then send it."
	var at time.Time
	if clock, ok := parseWallClock(r.PostForm[claimAtField]); ok {
		value = clock.String()
		problem = "That time does not fit this record. It has to be between " +
			cc.Window.From.At.In(loc).Format("15:04") + " and " + cc.Window.To.At.In(loc).Format("15:04") + "."
		if resolved, ok := resolveClaimedAt(clock, cc, loc); ok {
			at = resolved
		}
	}
	sub, err := t.claims.Submit(ctx, claim.Claim{
		TenantID: id.Session.TenantID, EmployeeID: id.Session.EmployeeID,
		// From the signed context and from nowhere else: K4 is enforced by what was minted.
		AnchorID: cc.AnchorID, Kind: cc.Kind, ClaimedAt: at,
	})
	// From here the request's cancellation decides nothing (checkin.go's post).
	post := r.WithContext(context.WithoutCancel(ctx))
	switch {
	case err == nil:
		t.log.InfoContext(ctx, "claim: sent", "employee_id", id.EmployeeID(), "claim_id", sub.ID)
		t.render(w, post, http.StatusOK, pages.ClaimSent(), false)
	case errors.Is(err, claim.ErrAlreadySent):
		// §C.5: a second "Send" is the Sent screen, never an error -- the first one is on record.
		t.log.InfoContext(ctx, "claim: already sent", "employee_id", id.EmployeeID(), "anchor_id", cc.AnchorID)
		t.render(w, post, http.StatusOK, pages.ClaimSent(), false)
	case errors.Is(err, claim.ErrOutsideWindow):
		t.render(w, post, http.StatusUnprocessableEntity, pages.ClaimForm(claimCardView(cc, loc, signed, value, problem)), false)
	case errors.Is(err, claim.ErrExpired):
		t.renderProblem(w, post, http.StatusGone, claimProblemExpired)
	case errors.Is(err, claim.ErrResolved):
		t.renderProblem(w, post, http.StatusConflict, claimProblemResolved)
	case errors.Is(err, claim.ErrNotActive):
		t.log.WarnContext(ctx, "claim: refused for an employee who is not active", "employee_id", id.EmployeeID())
		t.renderProblem(w, post, http.StatusForbidden, claimProblemNotActive)
	default:
		t.log.ErrorContext(ctx, "claim: sending the claim failed; nothing was sent",
			"employee_id", id.EmployeeID(), "anchor_id", cc.AnchorID, "err", err)
		t.renderProblem(w, post, http.StatusInternalServerError, claimProblemServer)
	}
}
