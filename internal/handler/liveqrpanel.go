package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PANEL'S LIVE QR SCREENS (M16-20, ADR 0045 §2.5): in a venue's detail, its screens -- each with
// its state and a Remove -- and "Add a live QR screen", which answers with the one page that shows
// the new screen's pairing code. The screen page itself is livescreen.go.
//
// WHO MAY (measured from this panel's own pattern, SEÇİM): the owner AND the manager, like mounting
// and unmounting a plaque (plaqueactions.go checks no role; only a removal of a venue or a
// department is owner-only, mayRemove). A screen is the live QR's plaque: an act at the venue, by
// whoever runs the venue, and recorded in audit_log with its actor -- the creation and the
// revocation by the admin, the pairing by nobody (it is the screen's act). base:authz-manager is not
// involved: like the plaque acts, these never went through the policy engine (M16-39 measures the
// consequence for the store review: path (c) is open to a manager).
//
// THE CODE IS SHOWN ONCE, ON THE ANSWER TO THE PRESS (apikeys.go's shape for a raw key): never in a
// URL (a 303 to a page that shows it would put it in the address bar and the history), never in a
// log, never in the trail; the database holds only its HMAC digest.
//
// BOTH WRITES ARE ON THE MAIN WRITE CHAIN (mountWriting's first group): a suspended business can add
// no screen -- and cannot remove one either, because ADR 0025 §3's list of writes a suspension does
// not stop is closed and names no screen. Opening the removal to a suspended business is that
// decision's edit (M16-24 carries it as an open item); a suspended business's screens keep
// showing codes, and whether their taps count is the tap path's suspension rule (M16-21).
type panelLiveQR interface {
	Create(ctx context.Context, c liveqr.CreateCommand) (liveqr.Created, error)
	List(ctx context.Context, tenantID, locationID uuid.UUID) ([]liveqr.Listed, error)
	Revoke(ctx context.Context, c liveqr.RevokeCommand) (liveqr.Revoked, error)
}

// LiveQRScreens gives the panel the live QR screens and the address a screen opens (TAPPA_BASE_URL
// + /screen). cmd/tappa passes it exactly when TAPPA_LIVE_QR_KEY is set; without it the venue detail
// draws no screens block and the two routes are not mounted.
func LiveQRScreens(s panelLiveQR, screenURL string) AdminAuthOption {
	return func(a *AdminAuth) {
		if s != nil && screenURL != "" {
			a.liveQR, a.liveQRScreenURL = s, screenURL
			a.liveQRCreates = httpx.NewLimiter(liveQRCreateLimit, liveQRCreatePeriod)
		}
	}
}

// The two write routes, under the locations section's own URL.
var (
	liveQRCreateHref = locationsHref + "/live-qr"
	liveQRRevokeHref = locationsHref + "/live-qr/revoke"
)

// liveQRCreateLimit bounds how many screens one business may add per hour: a screen row is never
// deleted (00111: tappa_app holds no DELETE; the trail of which screen stood where stays), so a
// stuck button or a script must not be able to grow the table without bound. 30 an hour is far
// above a venue's handful of screens and the odd retyped code.
const (
	liveQRCreateLimit  = 30
	liveQRCreatePeriod = time.Hour
)

// maxLiveQRBody bounds both POSTs: a uuid and the form binding.
const maxLiveQRBody = 2 << 10

var (
	problemLiveQRLimit = pages.ProblemView{
		Title:   "That is a lot of new screens",
		Message: "This business has added many live QR screens in the last hour, so no new one was added. Nothing else was changed.",
		Hint:    "Try again in an hour, or use one of the codes you already made.",
	}
	problemLiveQRUnknownScreen = pages.ProblemView{
		Title:   "We could not find that screen",
		Message: "No live QR screen with that address belongs to this business. Nothing was changed.",
		Hint:    "Open the venue again to see its screens.",
	}
)

// liveQRCreate is the "Add a live QR screen" press: a new screen waiting for its code, and the one
// page that shows the code.
func (a *AdminAuth) liveQRCreate(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	venueID, ok := a.readLiveQRForm(w, r, "location_id")
	if !ok {
		a.redirect(w, venueReturn("", "unknown-venue"))
		return
	}
	if within, n := a.liveQRCreates.TryCharge(id.TenantID().String()); !within {
		if a.liveQRCreates.FirstOverLimit(n) {
			a.log.Warn("panel live QR screen refused: the business's hourly budget is spent",
				"limit", a.liveQRCreates.Limit(), "actor_id", id.Admin.AdminUserID)
		}
		w.Header().Set("Retry-After", "3600")
		a.renderProblem(w, r, http.StatusTooManyRequests, problemLiveQRLimit)
		return
	}
	ven, err := a.venues.Venue(r.Context(), id.TenantID(), venueID)
	if errors.Is(err, tenant.ErrUnknownVenue) {
		a.log.Warn("panel live QR screen refused: no such venue in this business", "location_id", venueID)
		a.redirect(w, venueReturn("", "unknown-venue"))
		return
	}
	if err != nil {
		a.log.Error("panel: could not read the venue for a live QR screen", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return
	}
	created, err := a.liveQR.Create(r.Context(), liveqr.CreateCommand{
		TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID, LocationID: venueID,
	})
	switch {
	case errors.Is(err, liveqr.ErrUnknownLocation):
		a.log.Warn("panel live QR screen refused: no such venue in this business", "location_id", venueID)
		a.redirect(w, venueReturn("", "unknown-venue"))
		return
	case err != nil:
		a.log.Error("panel: could not add a live QR screen", "err", err, "location_id", venueID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return
	}
	a.log.Info("panel live QR screen added", "display_id", created.ID, "location_id", venueID,
		"actor_id", id.Admin.AdminUserID)
	v := pages.LiveQRCodeView{
		PanelChrome: a.chrome(r, pages.TabLocations),
		VenueName:   ven.Name,
		Code:        created.Code.ForThePanelOnce(),
		Minutes:     int(liveqr.PairingLifetime / time.Minute),
		ScreenURL:   a.liveQRScreenURL,
		BackHref:    openVenueAt(locationsHref, venueID),
	}
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminLiveQRCode(v))
}

// liveQRRevoke is a screen's Remove: revoked now, its page stops showing a code at its next poll.
func (a *AdminAuth) liveQRRevoke(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	displayID, ok := a.readLiveQRForm(w, r, "display_id")
	if !ok {
		a.renderProblem(w, r, http.StatusNotFound, problemLiveQRUnknownScreen)
		return
	}
	res, err := a.liveQR.Revoke(r.Context(), liveqr.RevokeCommand{
		TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID, DisplayID: displayID,
	})
	switch {
	case errors.Is(err, liveqr.ErrUnknownScreen):
		a.log.Warn("panel live QR screen removal refused: no such screen in this business", "display_id", displayID)
		a.renderProblem(w, r, http.StatusNotFound, problemLiveQRUnknownScreen)
		return
	case err != nil:
		a.log.Error("panel: could not remove a live QR screen", "err", err, "display_id", displayID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		return
	}
	a.log.Info("panel live QR screen removed", "display_id", displayID, "already", res.Already,
		"actor_id", id.Admin.AdminUserID)
	// The venue's detail lists the screen as removed: the row beneath is the acknowledgement.
	a.redirect(w, openVenueAt(locationsHref, res.LocationID))
}

// readLiveQRForm reads the bounded form and the one uuid field it names.
func (a *AdminAuth) readLiveQRForm(w http.ResponseWriter, r *http.Request, field string) (uuid.UUID, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLiveQRBody)
	if err := r.ParseForm(); err != nil {
		a.log.Warn("panel live QR form refused: the body could not be read")
		return uuid.Nil, false
	}
	v, err := uuid.Parse(strings.TrimSpace(r.PostFormValue(field)))
	if err != nil || v == uuid.Nil {
		a.log.Warn("panel live QR form refused: the id could not be read", "field", field)
		return uuid.Nil, false
	}
	return v, true
}

// fillLiveQR draws the open venue's screens block, when the live QR is configured. A failed read is
// an error (§4.6): "this venue has no screens" is a claim the page makes only after asking.
func (a *AdminAuth) fillLiveQR(r *http.Request, v *pages.LocationsView, zone *time.Location) error {
	if a.liveQR == nil || v.Detail == nil {
		return nil
	}
	venueID, err := uuid.Parse(v.Detail.ID)
	if err != nil {
		return nil // a venue row without an id is a render of nothing to list
	}
	rows, err := a.liveQR.List(r.Context(), httpx.AdminOf(r).TenantID(), venueID)
	if err != nil {
		return err
	}
	if zone == nil {
		zone = time.UTC
	}
	block := &components.LiveQRBlockView{
		VenueID:      venueID.String(),
		AddAction:    liveQRCreateHref,
		RevokeAction: liveQRRevokeHref,
		ScreenURL:    a.liveQRScreenURL,
	}
	for _, s := range rows {
		block.Screens = append(block.Screens, liveQRRowView(s, zone))
	}
	v.Detail.LiveQR = block
	return nil
}

// liveQRRowView is one screen as the block lists it: its state as a word, the moment that state
// began and -- for a paired screen -- when it last asked for a code (00114, to ten minutes), in the
// business's time zone. "Not seen since pairing" is a measured answer, not a blank: a screen that
// never polled after pairing, or a copied cookie's original that went dark, shows here.
func liveQRRowView(s liveqr.Listed, zone *time.Location) components.LiveQRScreenRowView {
	at := func(t time.Time) string { return t.In(zone).Format("2 Jan 15:04") }
	row := components.LiveQRScreenRowView{ID: s.ID.String(), Added: at(s.CreatedAt)}
	switch s.State {
	case liveqr.StatePaired:
		row.State, row.Since, row.CanRemove = "Showing codes", "paired "+at(*s.PairedAt), true
		row.LastSeen = "not seen since pairing"
		if s.LastSeenAt != nil {
			row.LastSeen = "last seen " + at(*s.LastSeenAt)
		}
	case liveqr.StateLapsed:
		// Removable (the re-audit's DÜŞÜK): a run-out screen shows no code, but Remove is how it
		// leaves the list's live states -- "Removed" is a terminal word, "Pairing ran out" is not.
		row.State, row.Since, row.CanRemove = "Pairing ran out", "paired "+at(*s.PairedAt)+", 180 days ago or more", true
		if s.LastSeenAt != nil {
			row.LastSeen = "last seen " + at(*s.LastSeenAt)
		}
	case liveqr.StateWaiting:
		row.State, row.Since, row.CanRemove = "Waiting for its code", "code works until "+at(s.PairingExpiresAt), true
	case liveqr.StateExpired:
		row.State, row.Since = "Code expired, never paired", "expired "+at(s.PairingExpiresAt)
	default:
		row.State = "Removed"
		if s.RevokedAt != nil {
			row.Since = "removed " + at(*s.RevokedAt)
		}
	}
	return row
}
