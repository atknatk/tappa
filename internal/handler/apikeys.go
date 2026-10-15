package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE API KEYS SECTION -- M11 API-3 (ADR 0028 §3). The owner issues a key for the business's
// HR/ERP system (shown ONCE, on the response to "create"), lists the keys and revokes one or all.
//
// 🔴 OWNER-ONLY, READ INCLUDED -- Billing's shape. A manager gets 403 at all four routes; a refused
// WRITE also leaves a trail row (api_key.refused), a refused page view is logged only (billing.go's
// refuseBillingRead argument). The domain re-reads the actor's role inside its own transaction
// (internal/apikey's mayManage) and does not take this gate's word for it.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5): it comes from the session. A key id in a form is looked
// up in the SESSION's tenant -- another business's id finds no row (RLS and the explicit filter),
// so guessing ids revokes nothing (ApiKeyPanel_AnotherBusinessCannotRevokeByGuessingTheID).
//
// 🔴 §4.7 -- THE KEY IS PRINTED ON ONE RESPONSE AND LOGGED ON NONE. apikey.Created.Key is revealed
// here, once, into the page that answers "create"; the log lines below carry the tenant, the actor
// and the key's id -- never the key, its hash, its hint or the Authorization header. Every panel
// page is Cache-Control: no-store (a.render), so neither the back button nor a reload serves the
// key from a cache, and a re-posted form is refused by the live-name UNIQUE (ErrNameTaken) rather
// than issuing a second key.

// The section's own routes, read from the section table (accountHref's rule).
var (
	apiKeysHref         = mustSectionHref(pages.TabAPIKeys)
	apiKeyRevokeHref    = apiKeysHref + "/revoke"
	apiKeyRevokeAllHref = apiKeysHref + "/revoke-all"
)

// ActionAPIKeyRefused records a write the role gate refused (the `<subject>.<verb>` shape of the
// panel's other refusals). The success rows are internal/apikey's (api_key.created / .revoked).
const ActionAPIKeyRefused = "api_key.refused"

// maxAPIKeyBody bounds every form of this section: a name of 64 characters or one uuid.
const maxAPIKeyBody = 4 << 10

// panelAPIKeys is the slice of internal/apikey this section needs, declared at the consumer (§7).
type panelAPIKeys interface {
	List(ctx context.Context, tenantID, actorID uuid.UUID) (apikey.Listing, error)
	Create(ctx context.Context, c apikey.CreateCommand) (apikey.Created, error)
	Revoke(ctx context.Context, c apikey.RevokeCommand) (apikey.Revoked, error)
	RevokeAll(ctx context.Context, tenantID, actorID uuid.UUID) ([]apikey.Revoked, error)
}

// IntegrationKeys gives the panel the keys service. cmd/tappa always passes it; a panel built
// without it draws the section with a sentence saying keys are not available, and refuses the
// three writes with 503.
func IntegrationKeys(k panelAPIKeys) AdminAuthOption {
	return func(a *AdminAuth) { a.apiKeys = k }
}

// mayManageAPIKeys is the role gate: a live owner.
func mayManageAPIKeys(id httpx.AdminIdentity) bool {
	return id.Live() && id.Admin.Role == adminRoleOwner
}

var (
	apiKeysDoneWords    = []string{"revoked", "revoked-all", "none-live"}
	apiKeysProblemWords = []string{"unreadable", "unknown-key", "already-revoked", "api-closed", "not-confirmed"}
)

func apiKeysDoneSentence(word string) string {
	switch word {
	case "revoked":
		return "The key is revoked. Any system still using it is refused from its next request."
	case "revoked-all":
		return "Every live key is revoked. Every integration is refused from its next request until it is given a new key."
	case "none-live":
		return "There was no live key to revoke."
	default:
		return ""
	}
}

func apiKeysProblemSentence(word string) string {
	switch word {
	case "unreadable":
		return "That form could not be read, so nothing was changed."
	case "unknown-key":
		return "That key is not one of this business's keys. Nothing was changed."
	case "already-revoked":
		return "That key was already revoked. Nothing else was changed."
	case "api-closed":
		return "API access is off for this business, so no key was created. Taptime switches it on on request."
	case "not-confirmed":
		return "Tick the box to confirm, then press the button again. Nothing was revoked."
	default:
		return "That did not happen."
	}
}

// problemAPIKeysOwnerOnly is what a manager sees at any of this section's addresses. It names no
// key -- WHO may look, never WHAT they would have seen.
var problemAPIKeysOwnerOnly = pages.ProblemView{
	Title:   "API keys are the owner's",
	Message: "This section issues and revokes the keys other systems use to reach this business's records, and it belongs to an owner of the business rather than to a manager. Nothing is wrong with your account.",
	Hint:    "If a system needs a key, ask an owner of the business.",
}

// problemAPIKeysUnavailable answers a write on a panel built without the keys service.
var problemAPIKeysUnavailable = pages.ProblemView{
	Title:   "Integration keys are not available here",
	Message: "This deployment was started without the integration keys service, so nothing was created or revoked.",
	Hint:    "Tell us and we will look.",
}

// apiKeysSection renders the section: the gate's state, the create form, the list.
func (a *AdminAuth) apiKeysSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayManageAPIKeys(id) {
		a.log.Warn("panel api keys page refused: this admin is not an owner",
			"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
		a.renderProblem(w, r, http.StatusForbidden, problemAPIKeysOwnerOnly)
		return
	}
	v, ok := a.apiKeysView(w, r, id)
	if !ok {
		return
	}
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("done")), apiKeysDoneWords...); word != "" {
		v.Done = apiKeysDoneSentence(word)
	}
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("problem")), apiKeysProblemWords...); word != "" {
		v.Problem = apiKeysProblemSentence(word)
	}
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminAPIKeys(v))
}

// apiKeysView reads the keys and builds the screen; on a failed read it has answered already.
func (a *AdminAuth) apiKeysView(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity) (pages.APIKeysView, bool) {
	v, err := a.readAPIKeysView(r, id)
	if errors.Is(err, apikey.ErrNotPermitted) {
		a.log.Warn("panel api keys page refused by the domain: the actor is not an active owner",
			"actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemAPIKeysOwnerOnly)
		return v, false
	}
	if err != nil {
		a.log.Error("panel: could not read the api keys", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return v, false
	}
	return v, true
}

// readAPIKeysView builds the screen and answers nothing: the chrome and the hrefs are always
// there, the list only when its read succeeded (err is the read's failure).
func (a *AdminAuth) readAPIKeysView(r *http.Request, id httpx.AdminIdentity) (pages.APIKeysView, error) {
	v := pages.APIKeysView{
		PanelChrome:   a.chrome(r, pages.TabAPIKeys),
		PostHref:      apiKeysHref,
		RevokeHref:    apiKeyRevokeHref,
		RevokeAllHref: apiKeyRevokeAllHref,
		NameMaxLength: apikey.NameLimit,
	}
	if a.apiKeys == nil {
		return v, nil
	}
	v.Available = true
	list, err := a.apiKeys.List(r.Context(), id.TenantID(), id.Admin.AdminUserID)
	if err != nil {
		return v, err
	}
	zone := time.UTC
	if s, err := a.accounts.Settings(r.Context(), id.TenantID()); err != nil {
		a.log.Error("panel: could not read the business timezone for the api keys; showing UTC", "err", err)
	} else {
		zone = a.accountZone(s.Timezone)
	}
	v.APIEnabled = list.APIEnabled
	for _, k := range list.Keys {
		row := pages.APIKeyRow{
			ID: k.ID.String(), Name: k.Name, PrefixHint: k.PrefixHint,
			CreatedOn: k.CreatedAt.In(zone).Format("2 January 2006, 15:04"),
			LastUsed:  "Never",
		}
		if k.LastUsedAt != nil {
			row.LastUsed = k.LastUsedAt.In(zone).Format("2 January 2006, 15:04")
		}
		if k.RevokedAt != nil {
			row.RevokedOn = k.RevokedAt.In(zone).Format("2 January 2006, 15:04")
		}
		v.Keys = append(v.Keys, row)
	}
	return v, nil
}

// readAPIKeyForm bounds and parses a form of this section; false when it has answered.
func (a *AdminAuth) readAPIKeyForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAPIKeyBody)
	if err := r.ParseForm(); err != nil {
		// Classified, never printed: net/url's error quotes the input.
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel api key write refused: the form could not be read", "reason", reason)
		a.redirect(w, apiKeysHref+"?problem=unreadable")
		return false
	}
	return true
}

// writeGate is the three writes' common start: the role (with a trail row when refused) and the
// service's presence. False when it has answered.
func (a *AdminAuth) writeGate(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, act string) bool {
	if !mayManageAPIKeys(id) {
		a.refuseAPIKeyWrite(r, id, act)
		a.renderProblem(w, r, http.StatusForbidden, problemAPIKeysOwnerOnly)
		return false
	}
	if a.apiKeys == nil {
		a.log.Error("panel api key write refused: the panel was built without the keys service", "act", act)
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemAPIKeysUnavailable)
		return false
	}
	return true
}

// apiKeyCreate issues a key and answers with the page that shows it -- the one response that
// ever carries it. A refused name re-renders the form with what was typed (nothing was written).
func (a *AdminAuth) apiKeyCreate(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readAPIKeyForm(w, r) || !a.writeGate(w, r, id, "create") {
		return
	}
	name := r.PostFormValue("name")
	created, err := a.apiKeys.Create(r.Context(), apikey.CreateCommand{
		TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID, Name: name,
	})
	switch {
	case err == nil:
		a.log.Info("panel api key created", "actor_id", id.Admin.AdminUserID, "key_id", created.ID)
		// 🔴 THE KEY IS SHOWN WHATEVER THE LIST READ SAYS (second round, security note): the key
		// is live from the commit above and this response is the only one that can ever carry it,
		// so a failed list read must not swallow it -- it becomes a sentence beside the key.
		v, lerr := a.readAPIKeysView(r, id)
		if lerr != nil {
			a.log.Error("panel: an api key was created and the list could not be read after it", "err", lerr,
				"key_id", created.ID)
			v.ListFailed, v.Keys = true, nil
			v.Problem = "The key below was created and works. The list of this business's keys could not be read just now -- reload the page to see it."
		}
		v.NewKey = created.Key.RevealToTheOwnerOnce()
		v.NewKeyName = created.Name
		a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminAPIKeys(v))
	case errors.Is(err, apikey.ErrName), errors.Is(err, apikey.ErrNameTaken):
		taken := errors.Is(err, apikey.ErrNameTaken)
		a.log.Warn("panel api key refused: the name", "actor_id", id.Admin.AdminUserID, "taken", taken)
		v, ok := a.apiKeysView(w, r, id)
		if !ok {
			return
		}
		v.NameValue = strings.TrimSpace(name)
		v.NameError = "Give the key a name of 1 to 64 characters."
		if taken {
			v.NameError = "A live key of this business already has that name. Choose another, or revoke that key first."
		}
		a.renderPanel(w, r, http.StatusUnprocessableEntity, v.PanelChrome, pages.AdminAPIKeys(v))
	case errors.Is(err, apikey.ErrAPIClosed):
		a.log.Warn("panel api key refused: the business's integration API is not open", "actor_id", id.Admin.AdminUserID)
		a.redirect(w, apiKeysHref+"?problem=api-closed")
	case errors.Is(err, apikey.ErrNotPermitted):
		a.log.Warn("panel api key refused by the domain: the actor is not an active owner", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemAPIKeysOwnerOnly)
	default:
		a.log.Error("panel: could not create an api key", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// apiKeyRevoke revokes one key of THIS business, named by its id.
func (a *AdminAuth) apiKeyRevoke(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readAPIKeyForm(w, r) || !a.writeGate(w, r, id, "revoke") {
		return
	}
	keyID, err := uuid.Parse(strings.TrimSpace(r.PostFormValue("key_id")))
	if err != nil {
		a.log.Warn("panel api key revoke refused: the key id is not a uuid")
		a.redirect(w, apiKeysHref+"?problem=unreadable")
		return
	}
	_, err = a.apiKeys.Revoke(r.Context(), apikey.RevokeCommand{
		TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID, KeyID: keyID,
	})
	switch {
	case err == nil:
		a.log.Info("panel api key revoked", "actor_id", id.Admin.AdminUserID, "key_id", keyID)
		a.redirect(w, apiKeysHref+"?done=revoked")
	case errors.Is(err, apikey.ErrUnknownKey):
		a.log.Warn("panel api key revoke refused: no such key in this business", "key_id", keyID)
		a.redirect(w, apiKeysHref+"?problem=unknown-key")
	case errors.Is(err, apikey.ErrAlreadyRevoked):
		a.log.Info("panel api key revoke: already revoked", "key_id", keyID)
		a.redirect(w, apiKeysHref+"?problem=already-revoked")
	case errors.Is(err, apikey.ErrNotPermitted):
		a.log.Warn("panel api key revoke refused by the domain: the actor is not an active owner", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemAPIKeysOwnerOnly)
	default:
		a.log.Error("panel: could not revoke an api key", "err", err, "key_id", keyID)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// apiKeyRevokeAll revokes every live key of this business, after a ticked confirmation.
func (a *AdminAuth) apiKeyRevokeAll(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readAPIKeyForm(w, r) || !a.writeGate(w, r, id, "revoke-all") {
		return
	}
	if r.PostFormValue("confirm") != "yes" {
		a.redirect(w, apiKeysHref+"?problem=not-confirmed")
		return
	}
	revoked, err := a.apiKeys.RevokeAll(r.Context(), id.TenantID(), id.Admin.AdminUserID)
	switch {
	case err == nil && len(revoked) == 0:
		a.redirect(w, apiKeysHref+"?done=none-live")
	case err == nil:
		a.log.Info("panel api keys revoked, all", "actor_id", id.Admin.AdminUserID, "count", len(revoked))
		a.redirect(w, apiKeysHref+"?done=revoked-all")
	case errors.Is(err, apikey.ErrNotPermitted):
		a.log.Warn("panel api keys revoke-all refused by the domain: the actor is not an active owner", "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemAPIKeysOwnerOnly)
	default:
		a.log.Error("panel: could not revoke the api keys", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// refusedAPIKeyWriteDetail is the refusal row's payload: the act, the role that was not enough and
// the role required -- nothing that was posted.
type refusedAPIKeyWriteDetail struct {
	Outcome      string `json:"outcome"`
	Act          string `json:"act"`
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
	Via          string `json:"via"`
}

func (a *AdminAuth) refuseAPIKeyWrite(r *http.Request, id httpx.AdminIdentity, act string) {
	a.log.Warn("panel api key write refused: this admin is not an owner",
		"act", act, "actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
	a.record(r.Context(), audit.Event{
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionAPIKeyRefused,
		Detail: refusedAPIKeyWriteDetail{
			Outcome: "refused", Act: act, Role: id.Admin.Role, RequiredRole: adminRoleOwner, Via: "panel",
		},
	})
}
