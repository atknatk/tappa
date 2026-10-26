package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/claim"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The SPIKE'S SECOND SECTION, "Time claims" (M15-18; ADR 0032 §C.4, §C.5): a person's answer to the
// result screen's question about a missing clock-in or clock-out, waiting for a manager.
//
// 🔴 §4.3 -- WHAT THIS FILE DOES NOT DO. It writes no transaction itself and changes none. A
// decision is internal/domain/claim's Decide: an approval appends ONE manual record through the one
// writer of manual records (internal/domain/manual) plus the decision row and its trail rows, in one
// transaction; a rejection appends the decision and its trail row. Nothing is updated or removed.
//
// 🔴 THE TENANT AND THE DECIDER ARE NEVER INPUTS (§4.5): both come from httpx.AdminOf(r), resolved
// from the signed panel session. The client contributes a claim id, an outcome, an optional Edit, a
// note and -- on the confirmation step's press -- the moment it was shown and the server's signature
// over it. Another business's claim id reads as no claim (every statement carries the tenant), so it
// answers "not found" and writes nothing.
//
// 🔴 THE CONFIRMATION STEP IS BOUND HERE, BECAUSE THE DOMAIN DOES NOT BIND IT (M15-16's hand-over).
// Nothing about the step is read from the request as a fact: the first press of Approve on a claim
// outside the band gets ErrNeedsConfirmation back and draws the step, which mints an adminConfirm value
// over THE CLAIM, THE MOMENT the step shows and THE SHIFT INSTANCE it was judged against
// (claim.ConfirmationSubject), for THIS panel session, for ten minutes. The second press carries that
// subject to Decide only when the value verifies against the moment it posts and the shift the server
// reads again now -- and Decide writes only when the subject equals the one it computes ITSELF, under
// the person's lock (M15-18 review round, security DÜŞÜK-1: a shift changed between this check and the
// write asks the step again). A posted moment without a genuine value is never the moment written
// (security DÜŞÜK-2).
// Why a signature and not only a hidden moment: a hidden moment is the client's word, and a forged
// first POST could carry "shown=<the claimed moment>" and skip the step; a moment the server signed
// can only come from a step the server drew. Why not ALSO server state: the deactivation gate's
// argument (deactivateconfirm.go) -- the actor is the signed-in manager, who can always walk the step,
// so what is bought is that the write implies the step was RENDERED for this moment; a replay of the
// same value within ten minutes meets "already decided" (UNIQUE claim_id), so it writes nothing twice.
//
// THERE IS NO BULK APPROVAL (review.go's reason, the same decision): one claim, one press.
//
// SUSPENSION (ADR 0025 §3): the decision POST is on mountWriting's MAIN group, beside the flagged
// queue's decision, so a suspended business is refused it with 403. The reads are panel reads.
// ROLE: owner and manager, as the flagged queue (reviewDecision has no role check either).

// panelClaims is the slice of internal/domain/claim the queue needs, declared at the consumer (§7).
type panelClaims interface {
	Inspect(ctx context.Context, tenantID, claimID uuid.UUID, at *time.Time) (claim.Inspection, error)
	Decide(ctx context.Context, d claim.Decision) (claim.Decided, error)
}

// panelClaimQueue is the list read (internal/domain/ledger, the panel's read side).
type panelClaimQueue interface {
	Claims(ctx context.Context, tenantID uuid.UUID) (ledger.ClaimQueue, error)
}

// PunchClaims gives the panel the claim queue: the list (ledger) and the claim service (Inspect,
// Decide). cmd/tappa always passes both; a panel built without them draws no "Time claims" link on
// the Spike and answers the section's two routes with 503.
func PunchClaims(list panelClaimQueue, claims panelClaims) AdminAuthOption {
	return func(a *AdminAuth) {
		if list != nil && claims != nil {
			a.claimQueue, a.claims = list, claims
		}
	}
}

// claimsHref is the section's URL: under the Spike's own, read from the section table.
var claimsHref = reviewHref + "/claims"

// maxClaimBody bounds the decision POST: a uuid, a word, a time, a moment, a signature and at most
// maxReviewNote characters -- reviewDecision's bound and its reason.
const maxClaimBody = 8 << 10

// claimShownField carries the moment the confirmation step was drawn for (RFC 3339, UTC); its
// signature travels in confirmField.
const claimShownField = "shown"

// problemClaimsUnavailable answers the two routes on a panel built without the claim service.
var problemClaimsUnavailable = pages.ProblemView{
	Title:   "Time claims are not available here",
	Message: "This deployment was started without the claim service, so nothing was changed.",
	Hint:    "Tell us and we will look.",
}

// claimProblems is the closed vocabulary of ?problem= (a reflected parameter is never echoed).
var claimProblems = []string{"unreadable", "edit", "decided", "unknown", "resolved", "expired", "outside", "partnership", "novenue", "refused"}

// claimsSection serves GET /admin/review/claims.
func (a *AdminAuth) claimsSection(w http.ResponseWriter, r *http.Request) {
	if a.claims == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemClaimsUnavailable)
		return
	}
	q := r.URL.Query()
	focus, _ := uuid.Parse(q.Get("focus"))
	a.renderClaims(w, r, claimPageState{
		focus:   focus,
		done:    a.confirmedClaimDecision(r),
		clipped: q.Get("clipped") == "1",
		problem: oneOfWords(q.Get("problem"), claimProblems...),
	})
}

// claimPageState is what one render of the section needs besides the reads.
type claimPageState struct {
	focus   uuid.UUID
	done    string
	clipped bool
	problem string
	// note is the manager's sentence carried back into the form; confirm the step to draw.
	note    string
	confirm *claimStep
	// editOpen opens the Edit field; editValue is the time typed into it, carried back; editHint
	// names the times that fit (D-3).
	editOpen  bool
	editValue string
	editHint  string
}

// claimStep is the confirmation step to draw: the assessment it repeats, the signed moment and --
// when it is drawn AGAIN -- why (D-5).
type claimStep struct {
	assessment claim.Assessment
	token      string
	reason     string
}

// renderClaims reads the list and the focused claim and draws the page.
func (a *AdminAuth) renderClaims(w http.ResponseWriter, r *http.Request, st claimPageState) {
	id := httpx.AdminOf(r)
	page, err := a.claimQueue.Claims(r.Context(), id.TenantID())
	if err != nil {
		// §4.6: a failed read is said, never drawn as an empty queue.
		a.log.Error("panel: could not read the claim queue", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	zone := page.Zone
	if zone == nil {
		zone = time.UTC
	}
	v := pages.ClaimQueueView{
		PanelChrome: a.chrome(r, pages.TabReview),
		Queues:      pages.SpikeQueuesView{ReviewHref: reviewHref, ClaimsHref: claimsHref, OnClaims: true},
		Action:      claimsHref,
		Queried:     page.Queried,
		More:        page.More,
		ListCap:     ledger.ClaimQueueCap,
		ZoneLabel:   zone.String(),
		Done:        st.done,
		Clipped:     st.clipped,
		Problem:     st.problem,
	}
	v.Queues.ClaimsCount = v.PanelChrome.Pending.Claims.Count()
	v.PanelChrome.Title = "Time claims"
	v.PanelChrome.Kicker = "Oldest first · shrinks as you work it"
	v.PanelChrome.Crumbs = []pages.Crumb{{Label: pages.SectionLabel(pages.TabReview), Href: reviewHref}, {Label: "Time claims"}}

	focusHref := func(id uuid.UUID) string { return claimsHref + "?" + url.Values{"focus": {id.String()}}.Encode() }
	v.Items = make([]pages.ClaimQueueItem, 0, len(page.Claims))
	for _, c := range page.Claims {
		v.Items = append(v.Items, pages.ClaimQueueItem{ID: c.ID.String(), Who: personName(c.EmployeeName),
			What: claimKindWords(claim.Kind(c.Kind)), When: claimDirWord(claim.Kind(c.Kind)) + " " + claimMoment(c.ClaimedAt, zone),
			FocusHref: focusHref(c.ID)})
	}
	if len(page.Claims) > 0 {
		// 🔴 THE ADDRESS ONLY CHOOSES AMONG CLAIMS THIS PAGE LISTED (focusIndex's rule): another
		// business's id, a decided one or nonsense falls back to the first, and nothing is said
		// about that id.
		i := 0
		for j, c := range page.Claims {
			if c.ID == st.focus {
				i = j
			}
		}
		v.Items[i].Current = true
		v.Pager = pages.ReviewPager{Pos: i + 1, Of: len(v.Items)}
		if i > 0 {
			v.Pager.PrevHref = v.Items[i-1].FocusHref
		}
		if i+1 < len(v.Items) {
			v.Pager.NextHref = v.Items[i+1].FocusHref
		}
		c := page.Claims[i]
		in, err := a.claims.Inspect(r.Context(), id.TenantID(), c.ID, nil)
		if err != nil {
			a.log.Error("panel: could not read the claim in focus", "err", err, "claim_id", c.ID)
			a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
			return
		}
		f := claimFocusView(in, personName(c.EmployeeName), zone)
		f.Note = st.note
		if c.ID == st.focus {
			f.EditOpen = f.EditOpen || st.editOpen
			f.EditValue, f.EditHint = st.editValue, st.editHint
			if st.confirm != nil {
				f.Confirm = claimConfirmView(*st.confirm, in, zone)
			}
		}
		v.Focus = &f
	}
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminClaimQueue(v))
}

// confirmedClaimDecision returns the outcome to confirm, having READ IT BACK (confirmedDecision's rule:
// a banner printed from the query string alone is a claim, not a measurement), or "".
func (a *AdminAuth) confirmedClaimDecision(r *http.Request) string {
	want := oneOfWords(r.URL.Query().Get("done"), string(claim.Approved), string(claim.Rejected))
	if want == "" {
		return ""
	}
	claimID, err := uuid.Parse(r.URL.Query().Get("claim"))
	if err != nil {
		return ""
	}
	in, err := a.claims.Inspect(r.Context(), httpx.AdminOf(r).TenantID(), claimID, nil)
	if err != nil {
		if !errors.Is(err, claim.ErrUnknownClaim) {
			a.log.Error("panel: could not verify the claim decision being confirmed", "err", err)
		}
		return ""
	}
	if in.Decided == nil || string(*in.Decided) != want {
		return ""
	}
	return want
}

// claimDecision serves POST /admin/review/claims: one approval or rejection of one claim.
//
// Every outcome the caller's input selects answers 303 (PRG) -- except the confirmation step, which
// is the same page drawn again with the step (nothing written; a refresh draws it again), and a
// form that must be shown again with what the manager typed. A database failure is the writer's
// problem page (500), never a sentence about the claim.
func (a *AdminAuth) claimDecision(w http.ResponseWriter, r *http.Request) {
	if a.claims == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemClaimsUnavailable)
		return
	}
	id := httpx.AdminOf(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxClaimBody)
	if err := r.ParseForm(); err != nil {
		// Classified, not printed (reviewDecision's reason: net/url quotes the input).
		a.log.Warn("panel claim decision refused: the form could not be read")
		a.redirect(w, claimsHref+"?problem=unreadable")
		return
	}
	claimID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("id")))
	if err != nil {
		a.log.Warn("panel claim decision refused: the posted claim id is not a uuid")
		a.redirect(w, claimsHref+"?problem=unreadable")
		return
	}
	var outcome claim.Outcome
	switch r.PostFormValue("outcome") {
	case string(claim.Approved):
		outcome = claim.Approved
	case string(claim.Rejected):
		outcome = claim.Rejected
	default:
		// NEVER DEFAULTED: a truncated form is not an approval.
		a.log.Warn("panel claim decision refused: the posted outcome is not one of the two")
		a.redirect(w, claimsHref+"?problem=unreadable&focus="+claimID.String())
		return
	}
	note, clipped := reviewNote(r.PostFormValue("note"))
	back := func(problem string) {
		a.redirect(w, claimsHref+"?problem="+problem+"&focus="+claimID.String())
	}

	d := claim.Decision{TenantID: id.TenantID(), ClaimID: claimID, DecidedBy: id.Admin.AdminUserID, Outcome: outcome, Note: note}
	if outcome == claim.Approved {
		if strings.TrimSpace(r.PostFormValue(claimShownField)) != "" {
			// THE STEP'S PRESS. Its moment counts only with its signature (DÜŞÜK-2): otherwise nothing
			// is decided, and the focus says why (D-5).
			step, err := a.claimStepPress(r, id.TenantID(), claimID)
			if err != nil {
				a.claimDecisionError(w, r, claimID, back, err)
				return
			}
			if step.why != "" {
				a.log.Info("panel claim decision: the step's press was not confirmed", "claim_id", claimID, "reason", step.why)
				a.renderClaims(w, r, claimPageState{focus: claimID, problem: step.why, note: note,
					editOpen: step.editValue != "", editValue: step.editValue})
				return
			}
			d.At, d.Confirmation = &step.at, step.subject
		} else {
			at, st, err := a.claimEdit(r, id.TenantID(), claimID)
			if err != nil {
				a.claimDecisionError(w, r, claimID, back, err)
				return
			}
			if st.problem != "" {
				a.log.Warn("panel claim decision refused: the edited time could not be used", "claim_id", claimID)
				st.focus, st.note, st.editOpen = claimID, note, true
				a.renderClaims(w, r, st)
				return
			}
			d.At = at
		}
	}

	got, err := a.claims.Decide(r.Context(), d)
	if errors.Is(err, claim.ErrNeedsConfirmation) && got.Assessment != nil {
		// THE STEP: nothing was written. Its value is minted over the moment the domain assessed and
		// the shift it judged against. A step drawn although a verified subject was posted means the
		// shift changed between the check here and Decide's own, under the lock (DÜŞÜK-1) -- said.
		token, terr := a.setConfirmation(w, r, confirmActionApproveClaim, claim.ConfirmationSubject(claimID, *got.Assessment))
		if terr != nil {
			a.log.Error("panel: could not mint the claim confirmation", "err", terr)
			a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
			return
		}
		reason := ""
		if d.Confirmation != "" {
			reason = "The record's shift changed since you were shown the interval, so nothing was recorded. Check it again."
		}
		a.log.Info("panel claim decision: the approval asks the confirmation step", "claim_id", claimID)
		a.renderClaims(w, r, claimPageState{focus: claimID, note: note,
			confirm: &claimStep{assessment: *got.Assessment, token: token, reason: reason}})
		return
	}
	if err != nil {
		a.claimDecisionError(w, r, claimID, back, err)
		return
	}
	a.short.clear(w, adminConfirmCookieName)
	to := claimsHref + "?done=" + string(got.Outcome) + "&claim=" + claimID.String()
	if clipped {
		to += "&clipped=1"
	}
	a.redirect(w, to)
}

// claimStepResult is the step's press, read: the signed moment and its subject, or why it is not one.
type claimStepResult struct {
	at      time.Time
	subject string
	// why is "" for a genuine press; otherwise a problem word ("unreadable", "step-expired",
	// "step-changed") and nothing is decided.
	why string
	// editValue is the moment's time of day when it was not the claimed one, so the Edit the manager
	// made is not lost with the step (a form value only -- never a moment written).
	editValue string
}

// claimStepPress reads the confirmation step's press: the posted moment (claimShownField) is the
// moment written ONLY when its signature (confirmField, and its twin cookie) verifies for this claim,
// this moment and the shift the server assesses it against now, in this session. Otherwise the press
// decides nothing (security DÜŞÜK-2): an absent, forged or expired value is "step-expired", a value
// minted for another moment or another shift is "step-changed".
func (a *AdminAuth) claimStepPress(r *http.Request, tenantID, claimID uuid.UUID) (claimStepResult, error) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(r.PostFormValue(claimShownField)))
	if err != nil {
		return claimStepResult{why: "unreadable"}, nil
	}
	in, err := a.claims.Inspect(r.Context(), tenantID, claimID, &t)
	if err != nil {
		return claimStepResult{}, err
	}
	subject := claim.ConfirmationSubject(claimID, in.Assessment)
	switch a.confirmationRefusalFor(r, confirmActionApproveClaim, subject) {
	case "":
		return claimStepResult{at: t, subject: subject}, nil
	case "confirm-stale":
		return claimStepResult{why: "step-changed", editValue: a.claimEditValue(r.Context(), tenantID, t, in.ClaimedAt)}, nil
	default:
		return claimStepResult{why: "step-expired", editValue: a.claimEditValue(r.Context(), tenantID, t, in.ClaimedAt)}, nil
	}
}

// claimEditValue is the time of day to put back into the Edit field: "" for the claimed moment.
func (a *AdminAuth) claimEditValue(ctx context.Context, tenantID uuid.UUID, t, claimed time.Time) string {
	if t.Equal(claimed) {
		return ""
	}
	zone, err := a.claimZone(ctx, tenantID)
	if err != nil {
		return ""
	}
	return t.In(zone).Format("15:04")
}

// claimEdit is the manager's Edit: a time of day resolved inside the claim's window in the business's
// zone, or nil (no Edit: the claimed moment). A typed time that cannot be used is NEVER dropped in
// favour of the claimed moment (D-1/M4: that would write a moment the manager replaced -- §4.3, a
// record nobody can take back): it answers the form again, with the time kept and the times that fit
// named (D-3); with no window at all the claim takes no other time (D-2).
func (a *AdminAuth) claimEdit(r *http.Request, tenantID, claimID uuid.UUID) (*time.Time, claimPageState, error) {
	edit := strings.TrimSpace(r.PostFormValue("at"))
	if edit == "" {
		return nil, claimPageState{}, nil
	}
	refused := claimPageState{problem: "edit", editValue: edit}
	c, ok := parseWallClock([]string{edit})
	if !ok {
		return nil, refused, nil
	}
	in, err := a.claims.Inspect(r.Context(), tenantID, claimID, nil)
	if err != nil {
		return nil, claimPageState{}, err
	}
	if in.Window == nil {
		refused.problem = "edit-closed"
		return nil, refused, nil
	}
	zone, err := a.claimZone(r.Context(), tenantID)
	if err != nil {
		return nil, claimPageState{}, err
	}
	at, ok := resolveInWindow(c, *in.Window, nil, zone)
	if !ok {
		refused.problem = "edit-outside"
		refused.editHint = "Pick a time " + claimWindowPhrase(*in.Window, func(t time.Time) string { return claimMoment(t, zone) }) + "."
		return nil, refused, nil
	}
	return &at, claimPageState{}, nil
}

// claimWindowPhrase says a window's two ends the way the panel and the result card both say them: an
// excluded end "after" / "before", an included one "from" / "up to" (M15-19 DÜŞÜK-2: the card used to
// say "between 18:05 and 05:00" of a window whose 05:00 is the next business day's, so excluded).
func claimWindowPhrase(w claim.Window, say func(time.Time) string) string {
	from, to := "after ", " and before "
	if w.From.Inclusive {
		from = "from "
	}
	if w.To.Inclusive {
		to = " up to "
	}
	return from + say(w.From.At) + to + say(w.To.At)
}

// claimZone is the business's zone, read with the queue (the same snapshot rule the list keeps).
func (a *AdminAuth) claimZone(ctx context.Context, tenantID uuid.UUID) (*time.Location, error) {
	page, err := a.claimQueue.Claims(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if page.Zone == nil {
		return time.UTC, nil
	}
	return page.Zone, nil
}

// claimDecisionError maps a refusal to its sentence (a 303) and a failure to the writer's page.
func (a *AdminAuth) claimDecisionError(w http.ResponseWriter, r *http.Request, claimID uuid.UUID, back func(string), err error) {
	word := ""
	switch {
	case errors.Is(err, claim.ErrAlreadyDecided):
		word = "decided"
	case errors.Is(err, claim.ErrUnknownClaim):
		// Another business's id is this one too: no claim of THIS business has it.
		word = "unknown"
	case errors.Is(err, claim.ErrResolved):
		word = "resolved"
	case errors.Is(err, claim.ErrExpired):
		word = "expired"
	case errors.Is(err, claim.ErrOutsideWindow):
		word = "outside"
	case errors.Is(err, claim.ErrPartnershipEnded):
		word = "partnership"
	case errors.Is(err, claim.ErrNoVenue):
		word = "novenue"
	case errors.Is(err, claim.ErrSelfDecision), errors.Is(err, claim.ErrRefused), errors.Is(err, claim.ErrNeedsConfirmation):
		// Self-decision is unreachable today (G-O1: two id spaces) and gets no screen of its own.
		word = "refused"
	}
	if word == "" {
		a.log.Error("panel: could not record the claim decision", "err", err, "claim_id", claimID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return
	}
	a.log.Info("panel claim decision refused", "claim_id", claimID, "reason", word)
	if word == "unknown" {
		a.redirect(w, claimsHref+"?problem=unknown")
		return
	}
	back(word)
}

// --- the focus, in words -----------------------------------------------------------------------

// claimFocusView maps the domain's inspection onto the focus.
func claimFocusView(in claim.Inspection, who string, zone *time.Location) pages.ClaimFocusView {
	as := in.Assessment
	f := pages.ClaimFocusView{
		ID:            in.ClaimID.String(),
		Who:           who,
		Heading:       claimKindWords(in.Kind),
		AnchorDir:     strings.ToUpper(string(in.Anchor.Direction)),
		AnchorAt:      claimMoment(in.Anchor.At, zone),
		AnchorChannel: claimChannelWord(in.Anchor.Channel),
		AnchorChose:   in.AnchorChose,
		Before:        claimNeighbour(in.Before, zone, "none"),
		// 🔴 "none within 18 h", NOT "none": Inspect reads the chain only that far past the anchor
		// (tap.StaleOpenIn) -- a later record exists or not, the read cannot say.
		After:      claimNeighbour(in.After, zone, "none within "+strconv.Itoa(int(tap.StaleOpenIn/time.Hour))+" h"),
		ClaimedDir: claimDirWord(in.Kind),
		EditLabel:  claimEditLabel(in.Kind),
		ClaimedAt:  claimMoment(in.ClaimedAt, zone),
		Sent:       claimMoment(in.CreatedAt, zone),
		Interval:   claimInterval(as.Interval, zone),
		Shift:      claimShift(as.Shift, zone),
		// DefaultUnchanged only while a prefill existed (ADR 0032 §C.2: a computation over
		// prefilled_at, meaningless without it).
		DefaultUnchanged: in.Prefilled != nil && as.DefaultUnchanged,
		ClaimedDeviates:  claimDeviation(as.Deviation),
		AnchorDeviates:   claimDeviation(as.AnchorDeviation),
		NeedsStep:        as.RequiresConfirmation,
	}
	if in.Prefilled != nil {
		end := "end"
		if in.Kind == claim.KindIn {
			end = "start"
		}
		f.Offered = claimMoment(*in.Prefilled, zone) + ", the shift's " + end
	} else {
		f.Offered = "no time: there was no shift time inside what could be claimed, so they typed it"
	}
	switch {
	case in.State == claim.StatePending:
		f.CanApprove = true
	case in.State == claim.StateOutsideWindow && in.Window == nil:
		// M15-19 DÜŞÜK-3: no window at all is not "a later record narrowed it" -- WindowOf drew none
		// (the business day cannot be measured: no resolvable cutoff, or the chain around the record
		// cannot be read whole). No Edit can fit, so none is offered (the POST refuses it too:
		// "edit-closed").
		f.StateLine = "No time can be claimed for this record: its business day can no longer be measured, so there is no window to fit a time in. Only Reject is left."
	case in.State == claim.StateOutsideWindow:
		f.CanApprove, f.EditOpen = true, true
		f.StateLine = "The claimed time no longer fits: a later record narrowed what can be claimed. Edit a time that fits, or reject."
	case in.State == claim.StateResolved:
		f.StateLine = "Already resolved: the record no longer misses this half. Only Reject is left."
	case in.State == claim.StateExpired:
		f.StateLine = "Expired: the time is more than 90 days back and can no longer be recorded. Only Reject is left."
	case in.State == claim.StateDecided:
		f.StateLine = "Already decided."
	}
	return f
}

// claimConfirmView is the step's repeat of the assessment it was drawn for.
func claimConfirmView(st claimStep, in claim.Inspection, zone *time.Location) *pages.ClaimConfirmView {
	as := st.assessment
	edited := !as.At.Equal(in.ClaimedAt)
	label := "Claimed time"
	if edited {
		// ORTA-1: the step's first line names the moment it would write -- the Edit, not the claim.
		label = "Your edited time"
	}
	return &pages.ClaimConfirmView{
		Reason:          st.reason,
		ClaimedLabel:    label,
		Shown:           as.At.UTC().Format(time.RFC3339Nano),
		Token:           st.token,
		Edited:          edited,
		At:              claimMoment(as.At, zone),
		Interval:        claimInterval(as.Interval, zone),
		Shift:           claimShift(as.Shift, zone),
		ClaimedDeviates: claimDeviation(as.Deviation),
		AnchorDeviates:  claimDeviation(as.AnchorDeviation),
		Why:             claimStepWhy(as),
	}
}

// claimStepWhy says why the step is drawn -- the domain's two reasons (claim.Assessment's OutsideBand
// and AnchorChosen), never a third the panel invents: M15-19, S5 made the person's own choice of the
// record's direction a reason of its own.
func claimStepWhy(as claim.Assessment) string {
	const chose = "they chose this direction when they tapped"
	switch {
	case as.AnchorChosen && as.OutsideBand:
		return "It is outside the shift's band, or there is no shift, and " + chose + ", so it is not approved in one press."
	case as.AnchorChosen:
		return "They chose this direction when they tapped, so it is not approved in one press."
	default:
		return "It is outside the shift's band, or there is no shift, so it is not approved in one press."
	}
}

func claimKindWords(k claim.Kind) string {
	if k == claim.KindIn {
		return "Clock-in missing"
	}
	return "Clock-out missing"
}

func claimDirWord(k claim.Kind) string { return strings.ToUpper(string(k)) }

// claimEditLabel names the Edit field by what it records (D-4).
func claimEditLabel(k claim.Kind) string {
	if k == claim.KindIn {
		return "New clock-in time"
	}
	return "New clock-out time"
}

func claimChannelWord(c tap.Channel) string {
	switch c {
	case tap.ChannelNFC:
		return "plaque tap"
	case tap.ChannelQR:
		return "QR code"
	case "manual":
		return "entered by a manager"
	}
	return "unknown channel"
}

func claimNeighbour(e *claim.Event, zone *time.Location, none string) string {
	if e == nil {
		return none
	}
	return strings.ToUpper(string(e.Direction)) + " " + claimMoment(e.At, zone) + " · " + claimChannelWord(e.Channel)
}

// claimMoment is an instant as a manager reads it: the day and the time in the business's zone --
// and, in the autumn hour that happens twice, its UTC offset, because "02:30" alone names two
// instants there (ADR 0032 §C.3).
func claimMoment(t time.Time, zone *time.Location) string {
	l := t.In(zone)
	return l.Format("Mon 2 Jan 15:04") + claimOffset(l, zone)
}

// claimClock is claimMoment without the day.
func claimClock(t time.Time, zone *time.Location) string {
	l := t.In(zone)
	return l.Format("15:04") + claimOffset(l, zone)
}

func claimOffset(l time.Time, zone *time.Location) string {
	if len(wallClockInstants(l.Year(), l.Month(), l.Day(), wallClock{l.Hour(), l.Minute(), l.Second()}, zone)) > 1 {
		return " (UTC" + l.Format("-07:00") + ")"
	}
	return ""
}

func claimInterval(iv claim.Interval, zone *time.Location) string {
	return "in " + claimMoment(iv.In, zone) + " → out " + claimMoment(iv.Out, zone) + " = " + claimDuration(iv.Duration())
}

func claimShift(s *claim.Span, zone *time.Location) string {
	if s == nil {
		return "none set for this record, so there is nothing to compare with"
	}
	a, b := s.Start.In(zone), s.End.In(zone)
	end := claimClock(s.End, zone)
	if a.YearDay() != b.YearDay() || a.Year() != b.Year() {
		end = claimMoment(s.End, zone)
	}
	return claimMoment(s.Start, zone) + " – " + end
}

func claimDeviation(d claim.Deviation) string {
	switch d.Side {
	case claim.BeforeStart:
		return claimDuration(d.By) + " before the shift starts"
	case claim.AfterEnd:
		return claimDuration(d.By) + " after the shift ends"
	case claim.NoShift:
		return "no shift to compare with"
	default:
		return "within the shift"
	}
}

// claimDuration is "7 h 48 m", or "45 m" under an hour -- whole minutes, never a fraction (§6: no
// float for hours).
func claimDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	h, m := int(d/time.Hour), int((d%time.Hour)/time.Minute)
	if h == 0 {
		return strconv.Itoa(m) + " m"
	}
	mm := strconv.Itoa(m)
	if m < 10 {
		mm = "0" + mm
	}
	return strconv.Itoa(h) + " h " + mm + " m"
}
