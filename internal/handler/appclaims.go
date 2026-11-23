package handler

// appclaims.go -- the staff app's "Missing times" (M16-43, ADR 0050 (i)): POST /app/v1/claims lists
// the records that take a claim now and the person's own claims; POST /app/v1/claims/send sends one.
//
// NO SECOND WRITE PATH. Sending runs Tap.Claim's order and rules word for word -- the signed claim
// context (bound to the device here, to the session there), the wall clock resolved in the signed
// window, claim.Service.Submit under the person's lock -- through the ONE shared step, claimSend.
// The manager's queue and its approval are untouched.
//
// THE OUTCOMES ARE DATA, NOT ERRORS (the tap answer's pattern): 200 {"result": …}, the closed set
// below. A context that does not verify for this device is the surface's own stale_context.
//
// NOTHING ABOUT A TIME IS LOGGED: opaque ids and the kind only (§7).

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/claim"
)

// appClaims is the consumer's view of claim.Service (CLAUDE.md §7).
type appClaims interface {
	Open(ctx context.Context, tenantID, employeeID uuid.UUID) ([]claim.Card, error)
	Submit(ctx context.Context, c claim.Claim) (claim.Submitted, error)
	Sent(ctx context.Context, tenantID, employeeID uuid.UUID, showHours bool) ([]claim.SentClaim, string, error)
}

// ServeClaims turns the app's "Missing times" on: the two routes and /me's missingTimes. cmd/tappa
// wires it with the SAME claim.Service the browser's card uses. Off, the routes answer not_found
// and missingTimes is 0.
func (a *App) ServeClaims(c appClaims, cfg *config.Config) error {
	if isNil(c) {
		return errors.New("handler: nil claim service")
	}
	ctxs, err := newClaimContexts(cfg)
	if err != nil {
		return err
	}
	a.claims, a.claimContexts = c, ctxs
	return nil
}

type appClaimBound struct {
	At        string `json:"at"`
	Inclusive bool   `json:"inclusive"`
}

type appOpenClaim struct {
	Context string `json:"context"`
	Kind    string `json:"kind"`
	// AnchorDate and AnchorAt are the record's local date and wall clock; Day is that date against
	// the server's today, so the app builds the browser card's sentence without a zone.
	AnchorDate string        `json:"anchorDate"`
	AnchorAt   string        `json:"anchorAt"`
	Day        string        `json:"day"`
	WindowFrom appClaimBound `json:"windowFrom"`
	WindowTo   appClaimBound `json:"windowTo"`
	Prefill    *string       `json:"prefill"`
}

type appSentClaim struct {
	Kind       string  `json:"kind"`
	AnchorDate string  `json:"anchorDate"`
	AnchorAt   string  `json:"anchorAt"`
	ClaimedAt  string  `json:"claimedAt"`
	State      string  `json:"state"`
	ApprovedAt *string `json:"approvedAt"`
}

type appClaimsResponse struct {
	Open []appOpenClaim `json:"open"`
	Sent []appSentClaim `json:"sent"`
}

// dayWord says the anchor's local date against the local today: today, yesterday or earlier.
func dayWord(at, now time.Time, loc *time.Location) string {
	ay, am, ad := at.In(loc).Date()
	ny, nm, nd := now.In(loc).Date()
	py, pm, pd := time.Date(ny, nm, nd-1, 12, 0, 0, 0, loc).Date()
	switch {
	case ay == ny && am == nm && ad == nd:
		return "today"
	case ay == py && am == pm && ad == pd:
		return "yesterday"
	default:
		return "earlier"
	}
}

// openClaimsOf signs one context per card, for this device.
func (a *App) openClaimsOf(cards []claim.Card, deviceID uuid.UUID, now time.Time) ([]appOpenClaim, error) {
	out := make([]appOpenClaim, 0, len(cards))
	for _, c := range cards {
		loc, err := time.LoadLocation(c.Zone)
		if err != nil || c.Zone == "" {
			return nil, errors.New("handler: a claim card carries no loadable zone")
		}
		signed, err := a.claimContexts.mintForDevice(claimContext{Kind: c.Kind, AnchorID: c.AnchorID, AnchorAt: c.AnchorAt,
			Window: c.Window, Prefilled: c.Prefilled, TapAt: now, Zone: c.Zone}, deviceID)
		if err != nil {
			return nil, err
		}
		o := appOpenClaim{
			Context: signed, Kind: string(c.Kind),
			AnchorDate: c.AnchorAt.In(loc).Format("2006-01-02"), AnchorAt: c.AnchorAt.In(loc).Format("15:04"),
			Day:        dayWord(c.AnchorAt, now, loc),
			WindowFrom: appClaimBound{At: c.Window.From.At.In(loc).Format("15:04"), Inclusive: c.Window.From.Inclusive},
			WindowTo:   appClaimBound{At: c.Window.To.At.In(loc).Format("15:04"), Inclusive: c.Window.To.Inclusive},
		}
		if c.Prefilled != nil {
			p := c.Prefilled.In(loc).Format("15:04")
			o.Prefill = &p
		}
		out = append(out, o)
	}
	return out, nil
}

// Claims serves POST /app/v1/claims: a signed read (a POST, because a signed read refuses a query
// string and a later page rides in the body). Body {}.
func (a *App) Claims(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	if a.claims == nil {
		writeAppError(w, appErrNotFound)
		return
	}
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	dev, ok := a.authRead(w, r, body, "claims", "")
	if !ok {
		return
	}
	if !decodeAppJSON(body, &struct{}{}) {
		writeAppError(w, appErrBadRequest)
		return
	}
	cards, err := a.claims.Open(ctx, dev.TenantID, dev.EmployeeID)
	if err != nil {
		a.log.ErrorContext(ctx, "app: reading the missing times failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	// The approved HOUR follows the owner's "show staff their hours" switch (Q50 (5) (b), M16-30);
	// that switch does not exist yet, so it is off: a decision is shown, its hour is not.
	sent, zone, err := a.claims.Sent(ctx, dev.TenantID, dev.EmployeeID, false)
	if err != nil {
		a.log.ErrorContext(ctx, "app: reading the sent claims failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	now := time.Now()
	open, err := a.openClaimsOf(cards, dev.ID, now)
	if err != nil {
		a.log.ErrorContext(ctx, "app: signing the missing times failed", "employee_id", dev.EmployeeID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	resp := appClaimsResponse{Open: open, Sent: make([]appSentClaim, 0, len(sent))}
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "" {
		loc = time.UTC
		if len(sent) > 0 {
			a.log.ErrorContext(ctx, "app: the business's zone does not load; sent claims in UTC", "tenant_id", dev.TenantID)
		}
	}
	for _, c := range sent {
		s := appSentClaim{Kind: string(c.Kind), AnchorDate: c.AnchorAt.In(loc).Format("2006-01-02"),
			AnchorAt: c.AnchorAt.In(loc).Format("15:04"), ClaimedAt: c.ClaimedAt.In(loc).Format("15:04"), State: string(c.State)}
		if c.ApprovedAt != nil {
			v := c.ApprovedAt.In(loc).Format("15:04")
			s.ApprovedAt = &v
		}
		resp.Sent = append(resp.Sent, s)
	}
	a.writeAppJSON(w, r, http.StatusOK, resp)
}

type appClaimSendRequest struct {
	Context string `json:"context"`
	At      string `json:"at"`
}

type appClaimSendResponse struct {
	Result string `json:"result"`
}

// The closed result set of POST /app/v1/claims/send.
const (
	appClaimSent          = "sent"
	appClaimAlreadySent   = "already_sent"
	appClaimOutsideWindow = "outside_window"
	appClaimExpired       = "expired"
	appClaimResolved      = "resolved"
	appClaimNotActive     = "not_active"
	appClaimStillOpen     = "still_open"
)

// ClaimsSend serves POST /app/v1/claims/send: a nonce-signed request, {"context","at"}.
func (a *App) ClaimsSend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // see checkin.go: one level of paren depth for redline R7.
	if a.claims == nil {
		writeAppError(w, appErrNotFound)
		return
	}
	body, ok := readAppBody(w, r)
	if !ok {
		return
	}
	dev, ok := a.authRequest(w, r, body, "claims/send", "")
	if !ok {
		return
	}
	var req appClaimSendRequest
	if !decodeAppJSON(body, &req) {
		writeAppError(w, appErrBadRequest)
		return
	}
	cc, err := a.claimContexts.parseForDevice(req.Context, dev.ID)
	if err != nil {
		a.log.InfoContext(ctx, "app: unusable claim context", "employee_id", dev.EmployeeID)
		writeAppError(w, appErrStaleContext)
		return
	}
	res, err := claimSend(ctx, a.claims, dev.TenantID, dev.EmployeeID, cc, []string{req.At})
	if err != nil {
		a.log.ErrorContext(ctx, "app: sending the claim failed; nothing was sent",
			"employee_id", dev.EmployeeID, "anchor_id", cc.AnchorID, "err", err)
		writeAppError(w, appErrServer)
		return
	}
	a.log.InfoContext(ctx, "app: claim answered", "employee_id", dev.EmployeeID, "kind", string(cc.Kind), "result", res.word)
	a.writeAppJSON(w, r, http.StatusOK, appClaimSendResponse{Result: res.word})
}

// claimSendResult is the one shared step's answer: the app's word, and for the browser the parts
// it re-renders a form with.
type claimSendResult struct {
	word    string
	sent    claim.Submitted
	value   string // the wall clock as read ("" when it could not be)
	problem string // the form's sentence for outside_window
}

// claimSubmitter is the one method claimSend needs.
type claimSubmitter interface {
	Submit(ctx context.Context, c claim.Claim) (claim.Submitted, error)
}

// claimSend is Tap.Claim's and ClaimsSend's ONE step after the context verified: read the wall
// clock, resolve it in the signed window, Submit, and say the outcome. err is only a failure that
// is neither a refusal nor an outcome.
func claimSend(ctx context.Context, claims claimSubmitter, tenantID, employeeID uuid.UUID, cc claimContext, at []string) (claimSendResult, error) {
	loc, err := time.LoadLocation(cc.Zone)
	if err != nil {
		return claimSendResult{}, errors.New("handler: the signed zone does not load")
	}
	out := claimSendResult{problem: "Enter the time, then send it."}
	var claimedAt time.Time
	if clock, ok := parseWallClock(at); ok {
		out.value = clock.String()
		out.problem = "That time does not fit this record. It has to be " +
			claimWindowPhrase(cc.Window, func(t time.Time) string { return t.In(loc).Format("15:04") }) + "."
		if resolved, ok := resolveClaimedAt(clock, cc, loc); ok {
			claimedAt = resolved
		}
	}
	out.sent, err = claims.Submit(ctx, claim.Claim{
		TenantID: tenantID, EmployeeID: employeeID, AnchorID: cc.AnchorID, Kind: cc.Kind, ClaimedAt: claimedAt,
	})
	switch {
	case err == nil:
		out.word = appClaimSent
	case errors.Is(err, claim.ErrAlreadySent):
		out.word = appClaimAlreadySent
	case errors.Is(err, claim.ErrOutsideWindow):
		out.word = appClaimOutsideWindow
	case errors.Is(err, claim.ErrExpired):
		out.word = appClaimExpired
	case errors.Is(err, claim.ErrResolved):
		out.word = appClaimResolved
	case errors.Is(err, claim.ErrNotActive):
		out.word = appClaimNotActive
	case errors.Is(err, claim.ErrStillOpen):
		out.word = appClaimStillOpen
	default:
		return out, err
	}
	return out, nil
}

// missingTimes is /me's count: Open's length, 0 when the claims are off or the read failed (the
// failure is logged; /me still answers).
func (a *App) missingTimes(ctx context.Context, tenantID, employeeID uuid.UUID) int {
	if a.claims == nil {
		return 0
	}
	cards, err := a.claims.Open(ctx, tenantID, employeeID)
	if err != nil {
		a.log.ErrorContext(ctx, "app: counting the missing times failed; answering 0", "employee_id", employeeID, "err", err)
		return 0
	}
	return len(cards)
}
