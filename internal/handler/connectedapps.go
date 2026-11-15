package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/oauth"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE "CONNECTED APPS" CARD (issue #37, ADR 0047 §12): under the Developers section, owner-only --
// read included, the API keys' rule (a connection exposes the business's records as a key does).
//
//	GET  /admin/api-keys/apps              the list (mountSections: the panel's read chain)
//	POST /admin/api-keys/apps/disconnect   the owner's Disconnect -- protectWritingThroughSuspension,
//	                                       route class 2: K-26's defence (it NARROWS what the business
//	                                       exposes, so a suspension must not stop it -- ADR 0025's
//	                                       exception list, by name; ADR 0047 §11.2)
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

var (
	connectedAppsDoneWords    = []string{"disconnected"}
	connectedAppsProblemWords = []string{"unreadable", "unknown-app"}
)

func connectedAppsDoneSentence(word string) string {
	if word == "disconnected" {
		return "The app is disconnected. It is refused from its next request, and connecting it again needs a new approval."
	}
	return ""
}

func connectedAppsProblemSentence(word string) string {
	switch word {
	case "unreadable":
		return "That form could not be read, so nothing was changed."
	case "unknown-app":
		return "That app is not connected to this business any more. Nothing was changed."
	default:
		return "That did not happen."
	}
}

// problemConnectedAppsOwnerOnly is what a manager sees. It names no app.
var problemConnectedAppsOwnerOnly = pages.ProblemView{
	Title:     "Connected apps are the owner's",
	Message:   "This page lists and disconnects the apps that reach this business's records, and it belongs to an owner of the business rather than to a manager. Nothing is wrong with your account.",
	Hint:      "If an app should be disconnected, ask an owner of the business.",
	OwnerOnly: true,
}

// connectedAppsSection renders the card.
func (a *AdminAuth) connectedAppsSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayManageAPIKeys(id) {
		a.log.Warn("panel connected apps page refused: this admin is not an owner",
			"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
		a.renderProblem(w, r, http.StatusForbidden, problemConnectedAppsOwnerOnly)
		return
	}
	v := pages.ConnectedAppsView{
		PanelChrome:    a.chrome(r, pages.TabAPIKeys),
		DisconnectHref: appDisconnectHref,
		KeysHref:       apiKeysHref,
	}
	v.Kicker = "Owner only · apps connected to this business"
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("done")), connectedAppsDoneWords...); word != "" {
		v.Done = connectedAppsDoneSentence(word)
	}
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("problem")), connectedAppsProblemWords...); word != "" {
		v.Problem = connectedAppsProblemSentence(word)
	}
	if isNil(a.oauth) {
		a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminConnectedApps(v))
		return
	}
	list, err := a.oauth.ListConnectedApps(r.Context(), id.TenantID(), id.Admin.AdminUserID)
	switch {
	case errors.Is(err, oauth.ErrNotOwner):
		a.log.Warn("panel connected apps page refused by the domain: the actor is not an active owner", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemConnectedAppsOwnerOnly)
		return
	case err != nil:
		a.log.Error("panel: could not read the connected apps", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	v.Available, v.IntegrationsOff = true, list.IntegrationsOff
	zone := a.webhookZone(r, id)
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
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminConnectedApps(v))
}

// appDisconnect is the owner's Disconnect of one connection of THIS business, named by its grant id.
func (a *AdminAuth) appDisconnect(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxConnectedAppsFm))
	if err := r.ParseForm(); err != nil {
		a.log.Warn("panel app disconnect refused: the form could not be read")
		a.redirect(w, connectedAppsHref+"?problem=unreadable")
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
		a.redirect(w, connectedAppsHref+"?problem=unreadable")
		return
	}
	err = a.oauth.Disconnect(r.Context(), id.TenantID(), id.Admin.AdminUserID, grantID)
	switch {
	case err == nil:
		a.log.Info("panel app disconnected", "actor_id", id.Admin.AdminUserID, "grant_id", grantID)
		a.redirect(w, connectedAppsHref+"?done=disconnected")
	case errors.Is(err, oauth.ErrUnknownGrant):
		a.log.Warn("panel app disconnect refused: no such live connection in this business", "grant_id", grantID)
		a.redirect(w, connectedAppsHref+"?problem=unknown-app")
	case errors.Is(err, oauth.ErrNotOwner):
		a.log.Warn("panel app disconnect refused by the domain: the actor is not an active owner", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemConnectedAppsOwnerOnly)
	default:
		a.log.Error("panel: could not disconnect an app", "err", err, "grant_id", grantID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}
