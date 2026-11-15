package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/oauth"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE "CONNECTED APPS" CARD (issue #37, ADR 0047 §12): a card of the Developers section, owner-only --
// read included, the API keys' rule (a connection exposes the business's records as a key does).
//
//	GET  /admin/api-keys                   the section draws the card (readAPIKeysView)
//	GET  /admin/api-keys/apps              the card's old page: 303 to the card's fragment (owner-only,
//	                                       as the page was -- a manager still reads the 403)
//	POST /admin/api-keys/apps/disconnect   the owner's Disconnect -- protectWritingThroughSuspension,
//	                                       route class 2: K-26's defence (it NARROWS what the business
//	                                       exposes, so a suspension must not stop it -- ADR 0025's
//	                                       exception list, by name; ADR 0047 §11.2)
//
// WHY THE OLD ADDRESS REDIRECTS (2026-11-15): the list was its own page behind a small link in the
// keys card's text, and an owner on the live panel did not find it. It moved onto the section; the
// address stays as a redirect so a bookmark, the guide's older copies and the route inventories keep
// one meaning, and Disconnect's route and class do not move.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5): the grant id in the form is looked up in the SESSION's
// business -- another business's id finds no row (RLS and the explicit filter), so guessing ids
// disconnects nothing. The role is re-read in the domain's own transaction (oauth.Server's
// activeOwner); this handler's gate is the first of two.
//
// WHAT IS LOGGED: the tenant, the actor and the grant's id. A connection has no secret to log.

var (
	connectedAppsHref  = apiKeysHref + "/apps"
	appDisconnectHref  = connectedAppsHref + "/disconnect"
	maxConnectedAppsFm = 4 << 10
)

// connectedAppsParam carries Disconnect's outcome back to the card -- its own parameter, drawn inside
// the card (brandParam's construction), so it never competes with the section's done/problem.
const connectedAppsParam = "app"

// connectedAppsOutcomes is the closed list of the parameter's words: done (true) or problem (false),
// and the sentence. A word outside it draws nothing.
var connectedAppsOutcomes = map[string]struct {
	done     bool
	sentence string
}{
	"disconnected": {true, "The app is disconnected. It is refused from its next request, and connecting it again needs a new approval."},
	"unreadable":   {false, "That form could not be read, so nothing was changed."},
	"unknown-app":  {false, "That app is not connected to this business any more. Nothing was changed."},
}

// connectedAppsReturn is the section's address with one outcome word, at the card's fragment. A word
// outside the list gives the bare fragment: server-built from a closed list, never an open redirect.
func connectedAppsReturn(word string) string {
	if _, ok := connectedAppsOutcomes[word]; !ok {
		return apiKeysHref + "#" + pages.ConnectedAppsAnchor
	}
	q := url.Values{}
	q.Set(connectedAppsParam, word)
	return apiKeysHref + "?" + q.Encode() + "#" + pages.ConnectedAppsAnchor
}

// problemConnectedAppsOwnerOnly is what a manager sees. It names no app.
var problemConnectedAppsOwnerOnly = pages.ProblemView{
	Title:     "Connected apps are the owner's",
	Message:   "This page lists and disconnects the apps that reach this business's records, and it belongs to an owner of the business rather than to a manager. Nothing is wrong with your account.",
	Hint:      "If an app should be disconnected, ask an owner of the business.",
	OwnerOnly: true,
}

// connectedAppsSection is the card's old page: the owner is sent to the card on the section.
func (a *AdminAuth) connectedAppsSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayManageAPIKeys(id) {
		a.log.Warn("panel connected apps page refused: this admin is not an owner",
			"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
		a.renderProblem(w, r, http.StatusForbidden, problemConnectedAppsOwnerOnly)
		return
	}
	a.redirect(w, connectedAppsReturn(""))
}

// readConnectedApps is the card, read for the Developers section: nil without an authorization
// server (no card); a failed read is the card's own sentence (and an ERROR), never the page's failure.
// The one error it returns is the domain's ErrNotOwner -- the section answers that as its 403.
func (a *AdminAuth) readConnectedApps(r *http.Request, id httpx.AdminIdentity, zone *time.Location) (*pages.ConnectedAppsCard, error) {
	if isNil(a.oauth) {
		return nil, nil
	}
	v := &pages.ConnectedAppsCard{DisconnectHref: appDisconnectHref}
	if o, ok := connectedAppsOutcomes[strings.TrimSpace(r.URL.Query().Get(connectedAppsParam))]; ok {
		if o.done {
			v.Done = o.sentence
		} else {
			v.Problem = o.sentence
		}
	}
	list, err := a.oauth.ListConnectedApps(r.Context(), id.TenantID(), id.Admin.AdminUserID)
	switch {
	case errors.Is(err, oauth.ErrNotOwner):
		return nil, err
	case err != nil:
		a.log.Error("panel: could not read the connected apps", "err", err)
		v.ReadFailed = true
		return v, nil
	}
	v.IntegrationsOff = list.IntegrationsOff
	for _, c := range list.Apps {
		row := pages.ConnectedAppRow{
			GrantID: c.GrantID.String(), AppName: c.AppName, AppDisabled: c.AppDisabled, Expired: c.Expired,
			ConnectedOn: c.ConnectedAt.In(zone).Format("2 Jan 2006, 15:04"), LastUsed: "Never",
			ApprovedBy: c.ApprovedBy, ApproverDisabled: c.ApproverDisabled,
		}
		if row.AppName == "" {
			row.AppName = "An app Taptime cannot name right now"
		}
		if c.LastUsedAt != nil {
			row.LastUsed = c.LastUsedAt.In(zone).Format("2 Jan 2006, 15:04")
		}
		for _, s := range c.Scopes {
			if l := oauth.ScopeLabel(s); l != "" {
				row.Scopes = append(row.Scopes, l)
			}
		}
		v.Apps = append(v.Apps, row)
	}
	return v, nil
}

// appDisconnect is the owner's Disconnect of one connection of THIS business, named by its grant id.
func (a *AdminAuth) appDisconnect(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxConnectedAppsFm))
	if err := r.ParseForm(); err != nil {
		a.log.Warn("panel app disconnect refused: the form could not be read")
		a.redirect(w, connectedAppsReturn("unreadable"))
		return
	}
	if !mayManageAPIKeys(id) {
		a.refuseAPIKeyWrite(r, id, "disconnect-app")
		a.renderProblem(w, r, http.StatusForbidden, problemConnectedAppsOwnerOnly)
		return
	}
	if isNil(a.oauth) {
		a.log.Error("panel app disconnect refused: the panel was built without the authorization server")
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemOAuthUnavailable)
		return
	}
	grantID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("grant_id")))
	if err != nil {
		a.log.Warn("panel app disconnect refused: the grant id is not a uuid")
		a.redirect(w, connectedAppsReturn("unreadable"))
		return
	}
	err = a.oauth.Disconnect(r.Context(), id.TenantID(), id.Admin.AdminUserID, grantID)
	switch {
	case err == nil:
		a.log.Info("panel app disconnected", "actor_id", id.Admin.AdminUserID, "grant_id", grantID)
		a.redirect(w, connectedAppsReturn("disconnected"))
	case errors.Is(err, oauth.ErrUnknownGrant):
		a.log.Warn("panel app disconnect refused: no such live connection in this business", "grant_id", grantID)
		a.redirect(w, connectedAppsReturn("unknown-app"))
	case errors.Is(err, oauth.ErrNotOwner):
		a.log.Warn("panel app disconnect refused by the domain: the actor is not an active owner", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemConnectedAppsOwnerOnly)
	default:
		a.log.Error("panel: could not disconnect an app", "err", err, "grant_id", grantID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}
