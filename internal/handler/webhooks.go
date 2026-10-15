package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/webhook/endpoints"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE WEBHOOK PART OF THE API KEYS SECTION -- M11 API-9 (ADR 0028 §6, §7, §10). The owner keeps the
// business's ONE endpoint: creates it (its signing key shown ONCE, on that response), changes its
// address, rotates its key (the new key shown once, the old one dead), switches it off and on, and
// sends it a test ping. The deliveries and their log are API-10's.
//
// 🔴 OWNER-ONLY, LIKE THE REST OF THE SECTION (apikeys.go): a manager is 403 at every route and a
// refused write leaves a webhook_endpoint.refused row; the domain re-reads the role in its own
// transaction. Every write is on the panel's write chain (mountWriting: the Origin check before the
// resolver -- GATE-1).
//
// 🔴 THE ADDRESS IS SHOWN TO THE OWNER AND LOGGED NOWHERE (CLAUDE.md §7; ADR 0028 §3, §6). The page
// prints the owner's own address back to them; every log line here carries the tenant, the actor and
// the endpoint's id -- never the address, never a part of it, never Go's error text about it (the
// domain answers with fixed sentinels and a closed class). The key is printed on the two responses
// that create it and on none other, and every panel page is Cache-Control: no-store.
//
// SUSPENSION (ADR 0028 §10): OP-15 is not on this branch; the classes are internal/webhook/endpoints'
// OpenWhileSuspended (switching off open, everything else closed), which OP-15's gate reads.

// The webhook's routes, under the API keys section.
var (
	webhookHref        = apiKeysHref + "/webhook"
	webhookAddressHref = webhookHref + "/address"
	webhookRotateHref  = webhookHref + "/rotate"
	webhookDisableHref = webhookHref + "/disable"
	webhookEnableHref  = webhookHref + "/enable"
	webhookPingHref    = webhookHref + "/ping"
)

// ActionWebhookRefused records a webhook write the role gate refused.
const ActionWebhookRefused = "webhook_endpoint.refused"

// maxWebhookBody bounds every webhook form: an address of 2048 characters and a checkbox.
const maxWebhookBody = 8 << 10

// panelWebhook is the slice of internal/webhook/endpoints this section needs (§7: at the consumer).
type panelWebhook interface {
	List(ctx context.Context, tenantID, actorID uuid.UUID) (endpoints.Listing, error)
	Create(ctx context.Context, c endpoints.CreateCommand) (endpoints.Created, error)
	Rotate(ctx context.Context, tenantID, actorID uuid.UUID) (endpoints.Rotated, error)
	Readdress(ctx context.Context, tenantID, actorID uuid.UUID, address string) (uuid.UUID, error)
	Disable(ctx context.Context, tenantID, actorID uuid.UUID) (uuid.UUID, error)
	Enable(ctx context.Context, tenantID, actorID uuid.UUID) (uuid.UUID, error)
	Ping(ctx context.Context, tenantID, actorID uuid.UUID) (endpoints.Pinged, error)
}

// IntegrationWebhook gives the panel the webhook endpoint service. cmd/tappa passes it whenever
// TAPPA_WEBHOOK_KEK is set (always outside dev); without it the section says the webhook is not
// configured and its writes answer 503.
func IntegrationWebhook(w panelWebhook) AdminAuthOption {
	return func(a *AdminAuth) { a.webhook = w }
}

var (
	webhookDoneWords    = []string{"webhook-readdressed", "webhook-disabled", "webhook-enabled"}
	webhookProblemWords = []string{"webhook-unreadable", "webhook-api-closed", "webhook-exists", "webhook-none",
		"webhook-unchanged", "webhook-not-confirmed", "webhook-switched-off", "webhook-key-unreadable",
		"webhook-stored-address", "webhook-bucket-full"}
)

func webhookDoneSentence(word string) string {
	switch word {
	case "webhook-readdressed":
		return "The webhook's address is changed. Its signing key is the same."
	case "webhook-disabled":
		return "The webhook is switched off. Taptime sends it nothing until you switch it on again."
	case "webhook-enabled":
		return "The webhook is switched on."
	default:
		return ""
	}
}

func webhookProblemSentence(word string) string {
	switch word {
	case "webhook-unreadable":
		return "That form could not be read, so nothing was changed."
	case "webhook-api-closed":
		return "API access is off for this business, so the webhook was not changed. You can still switch it off."
	case "webhook-exists":
		return "This business already has its webhook. Change its address below instead."
	case "webhook-none":
		return "This business has no webhook yet."
	case "webhook-unchanged":
		return "The webhook was already in that state. Nothing was changed."
	case "webhook-not-confirmed":
		return "Tick the box to confirm, then press the button again. The key was not changed."
	case "webhook-switched-off":
		return "The webhook is switched off, so no test was sent. Switch it on first."
	case "webhook-key-unreadable":
		return "The webhook's signing key could not be read on this deployment, so nothing was sent. Create a new key below."
	case "webhook-stored-address":
		return "The webhook's saved address no longer passes Taptime's address rules, so nothing was sent. Give it a new address."
	case "webhook-bucket-full":
		return "This business has used its 10 test messages for this hour, so none was sent."
	default:
		return "That did not happen."
	}
}

// webhookAddressSentence is the owner's sentence for an address the rule refused.
func webhookAddressSentence(err error) string {
	switch {
	case errors.Is(err, egress.ErrNotHTTPS):
		return "The address must start with https://."
	case errors.Is(err, egress.ErrPort):
		return "The address must use the standard HTTPS port, 443: write no port."
	case errors.Is(err, egress.ErrIPLiteral):
		return "Use your system's host name, not an IP address."
	case errors.Is(err, egress.ErrUserinfo):
		return "The address must not carry a user name or a password."
	case errors.Is(err, egress.ErrQuery):
		return "The address must not carry a query string (nothing after a ?)."
	case errors.Is(err, egress.ErrFragment):
		return "The address must not carry a fragment (nothing after a #)."
	case errors.Is(err, egress.ErrCharacters):
		return "The address holds a space, a control character or a non-English letter. Paste it again."
	case errors.Is(err, egress.ErrTooLong):
		return "The address is longer than 2048 characters."
	case errors.Is(err, egress.ErrNotAnswered):
		return "Give the address your system receives webhooks at."
	default:
		return "The host must be a public DNS name such as hooks.example.com (letters, digits, hyphens and dots, no trailing dot)."
	}
}

// webhookPingSentence is what the owner reads about a ping, from the closed class and the status.
func webhookPingSentence(class egress.Class, status int) (string, bool) {
	code := strconv.Itoa(status)
	switch class {
	case "":
		return "Your system answered " + code + ". Taptime's webhooks will reach it.", true
	case egress.ClassTimeout:
		return "Your system did not answer within 10 seconds.", false
	case egress.ClassTLS:
		return "The secure connection failed: your system's certificate was not accepted, or it does not speak HTTPS on port 443.", false
	case egress.ClassRefused:
		return "Your system could not be reached: the name did not resolve, or nothing answered on port 443.", false
	case egress.ClassHTTP4xx:
		if status >= 300 && status < 400 {
			return "Your system answered " + code + ", a redirect. Taptime does not follow redirects: give the address it redirects to.", false
		}
		return "Your system answered " + code + ". Check the address, and that your system accepts Taptime's requests.", false
	case egress.ClassHTTP5xx:
		return "Your system answered " + code + ": an error on its side.", false
	case egress.ClassBlocked:
		return "That address leads to a network Taptime does not send to: a private, local or reserved address, or Taptime itself. Use your system's public address.", false
	default:
		return "The test did not go through.", false
	}
}

// problemWebhookUnavailable answers a write on a panel built without the webhook service.
var problemWebhookUnavailable = pages.ProblemView{
	Title:   "The webhook is not configured here",
	Message: "This deployment was started without the webhook's key, so nothing was created or changed.",
	Hint:    "Tell us and we will look.",
}

// readWebhookView fills the webhook part of the section, and the page-level sentences it owns.
// A failed read is the caller's to answer.
func (a *AdminAuth) readWebhookView(r *http.Request, id httpx.AdminIdentity, zone *time.Location) (pages.WebhookView, error) {
	v := pages.WebhookView{
		CreateHref: webhookHref, AddressHref: webhookAddressHref, RotateHref: webhookRotateHref,
		DisableHref: webhookDisableHref, EnableHref: webhookEnableHref, PingHref: webhookPingHref,
		AddressMaxLength: egress.MaxEndpointLength, Limit: endpoints.BucketLimit,
	}
	if a.webhook == nil {
		return v, nil
	}
	v.Configured = true
	l, err := a.webhook.List(r.Context(), id.TenantID(), id.Admin.AdminUserID)
	if err != nil {
		return v, err
	}
	v.APIEnabled = l.APIEnabled
	v.Spent = l.Bucket.Spent
	if !l.Bucket.NextAt.IsZero() {
		v.NextSlot = l.Bucket.NextAt.In(zone).Format("15:04")
	}
	if e := l.Endpoint; e != nil {
		v.Exists = true
		v.Active = e.Active
		v.AddressRefused = e.AddressRefused
		if !e.AddressRefused {
			v.Address = e.Address.ShowToTheOwner()
		}
		v.CreatedOn = e.CreatedAt.In(zone).Format("2 January 2006, 15:04")
		if e.RotatedAt != nil {
			v.RotatedOn = e.RotatedAt.In(zone).Format("2 January 2006, 15:04")
		}
	}
	if q := r.URL.Query(); q.Has("ping") {
		class := egress.Class(strings.TrimSpace(q.Get("ping")))
		if class == "ok" {
			class = ""
		}
		known := class == ""
		for _, c := range egress.Classes {
			known = known || c == class
		}
		status, _ := strconv.Atoi(q.Get("status"))
		if known && status >= 0 && status <= 599 {
			v.PingSentence, v.PingDelivered = webhookPingSentence(class, status)
			v.PingShown = true
		}
	}
	return v, nil
}

// webhookZone is the business's zone for the section's times (UTC if it cannot be read).
func (a *AdminAuth) webhookZone(r *http.Request, id httpx.AdminIdentity) *time.Location {
	s, err := a.accounts.Settings(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not read the business timezone for the webhook; showing UTC", "err", err)
		return time.UTC
	}
	return a.accountZone(s.Timezone)
}

// webhookWriteGate is every webhook write's start: the form bounded and parsed, the role (with a
// trail row when refused) and the service's presence. False when it has answered.
func (a *AdminAuth) webhookWriteGate(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, act string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
	if err := r.ParseForm(); err != nil {
		// Classified, never printed: net/url's error quotes the input -- here, possibly the address.
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel webhook write refused: the form could not be read", "act", act, "reason", reason)
		a.redirect(w, apiKeysHref+"?problem=webhook-unreadable")
		return false
	}
	if !mayManageAPIKeys(id) {
		a.refuseWebhookWrite(r, id, act)
		a.renderProblem(w, r, http.StatusForbidden, problemAPIKeysOwnerOnly)
		return false
	}
	if a.webhook == nil {
		a.log.Error("panel webhook write refused: the panel was built without the webhook service", "act", act)
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemWebhookUnavailable)
		return false
	}
	return true
}

// webhookRefusal answers the domain's shared refusals; false when err is none of them.
func (a *AdminAuth) webhookRefusal(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, act string, err error) bool {
	word := ""
	switch {
	case errors.Is(err, endpoints.ErrNotPermitted):
		a.log.Warn("panel webhook write refused by the domain: the actor is not an active owner", "act", act, "actor_id", id.Admin.AdminUserID)
		a.renderProblem(w, r, http.StatusForbidden, problemAPIKeysOwnerOnly)
		return true
	case errors.Is(err, endpoints.ErrAPIClosed):
		word = "webhook-api-closed"
	case errors.Is(err, endpoints.ErrExists):
		word = "webhook-exists"
	case errors.Is(err, endpoints.ErrNoEndpoint):
		word = "webhook-none"
	case errors.Is(err, endpoints.ErrUnchanged):
		word = "webhook-unchanged"
	case errors.Is(err, endpoints.ErrDisabled):
		word = "webhook-switched-off"
	case errors.Is(err, endpoints.ErrKeyUnreadable):
		word = "webhook-key-unreadable"
	case errors.Is(err, endpoints.ErrStoredAddress):
		word = "webhook-stored-address"
	default:
		return false
	}
	a.log.Warn("panel webhook write refused", "act", act, "actor_id", id.Admin.AdminUserID, "reason", word)
	a.redirect(w, apiKeysHref+"?problem="+word)
	return true
}

// renderWebhookAddressError re-renders the section with the owner's typed address and the rule's
// sentence (nothing was written).
func (a *AdminAuth) renderWebhookAddressError(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, typed string, err error) {
	v, ok := a.apiKeysView(w, r, id)
	if !ok {
		return
	}
	v.Webhook.AddressValue = strings.TrimSpace(typed)
	v.Webhook.AddressError = webhookAddressSentence(err)
	a.renderPanel(w, r, http.StatusUnprocessableEntity, v.PanelChrome, pages.AdminAPIKeys(v))
}

// renderWebhookKey answers create and rotate: the section with the key shown once. A failed read
// of the section after the write must not swallow the key (apiKeyCreate's rule): it becomes a
// sentence beside it.
func (a *AdminAuth) renderWebhookKey(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, key, done string) {
	v, err := a.readAPIKeysView(r, id)
	if err != nil {
		a.log.Error("panel: a webhook key was issued and the section could not be read after it", "err", err)
		v.ListFailed, v.Keys = true, nil
		v.Problem = "The key below is in force. The rest of this page could not be read just now -- reload it to see it."
	}
	v.Webhook.NewKey = key
	v.Done = done
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminAPIKeys(v))
}

// webhookCreate creates the endpoint and answers with the page that shows its key, once.
func (a *AdminAuth) webhookCreate(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.webhookWriteGate(w, r, id, "create") {
		return
	}
	typed := r.PostFormValue("address")
	c, err := a.webhook.Create(r.Context(), endpoints.CreateCommand{TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID, Address: typed})
	switch {
	case err == nil:
		a.log.Info("panel webhook created", "actor_id", id.Admin.AdminUserID, "endpoint_id", c.ID)
		a.renderWebhookKey(w, r, id, c.Key.RevealToTheOwnerOnce(), "The webhook is created.")
	case isAddressRefusal(err):
		a.log.Warn("panel webhook refused: the address rule", "actor_id", id.Admin.AdminUserID)
		a.renderWebhookAddressError(w, r, id, typed, err)
	case a.webhookRefusal(w, r, id, "create", err):
	default:
		a.log.Error("panel: could not create the webhook", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// webhookReaddress points the endpoint at a new address.
func (a *AdminAuth) webhookReaddress(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.webhookWriteGate(w, r, id, "readdress") {
		return
	}
	typed := r.PostFormValue("address")
	eid, err := a.webhook.Readdress(r.Context(), id.TenantID(), id.Admin.AdminUserID, typed)
	switch {
	case err == nil:
		a.log.Info("panel webhook readdressed", "actor_id", id.Admin.AdminUserID, "endpoint_id", eid)
		a.redirect(w, apiKeysHref+"?done=webhook-readdressed")
	case isAddressRefusal(err):
		a.log.Warn("panel webhook readdress refused: the address rule", "actor_id", id.Admin.AdminUserID)
		a.renderWebhookAddressError(w, r, id, typed, err)
	case a.webhookRefusal(w, r, id, "readdress", err):
	default:
		a.log.Error("panel: could not change the webhook's address", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// webhookRotate gives the endpoint a new key after a ticked confirmation, and shows it once.
func (a *AdminAuth) webhookRotate(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.webhookWriteGate(w, r, id, "rotate") {
		return
	}
	if r.PostFormValue("confirm") != "yes" {
		a.redirect(w, apiKeysHref+"?problem=webhook-not-confirmed")
		return
	}
	rot, err := a.webhook.Rotate(r.Context(), id.TenantID(), id.Admin.AdminUserID)
	switch {
	case err == nil:
		a.log.Info("panel webhook key rotated", "actor_id", id.Admin.AdminUserID, "endpoint_id", rot.ID)
		a.renderWebhookKey(w, r, id, rot.Key.RevealToTheOwnerOnce(), "The webhook has a new signing key. The old one signs nothing from now on.")
	case a.webhookRefusal(w, r, id, "rotate", err):
	default:
		a.log.Error("panel: could not rotate the webhook's key", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// webhookSwitch switches the endpoint off (open whatever K-22 says -- K-26) or on.
func (a *AdminAuth) webhookSwitch(on bool) http.HandlerFunc {
	act, done := "disable", "webhook-disabled"
	if on {
		act, done = "enable", "webhook-enabled"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id := httpx.AdminOf(r)
		if !a.webhookWriteGate(w, r, id, act) {
			return
		}
		do := a.webhook.Disable
		if on {
			do = a.webhook.Enable
		}
		eid, err := do(r.Context(), id.TenantID(), id.Admin.AdminUserID)
		switch {
		case err == nil:
			a.log.Info("panel webhook switched", "act", act, "actor_id", id.Admin.AdminUserID, "endpoint_id", eid)
			a.redirect(w, apiKeysHref+"?done="+done)
		case a.webhookRefusal(w, r, id, act, err):
		default:
			a.log.Error("panel: could not switch the webhook", "act", act, "err", err)
			a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
		}
	}
}

// webhookPing sends the test and redirects to the section with its class and status (so a reload
// shows the result again instead of sending -- and spending -- another).
func (a *AdminAuth) webhookPing(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.webhookWriteGate(w, r, id, "ping") {
		return
	}
	p, err := a.webhook.Ping(r.Context(), id.TenantID(), id.Admin.AdminUserID)
	var full *endpoints.BucketFull
	switch {
	case err == nil, errors.Is(err, endpoints.ErrResultNotRecorded):
		if err != nil {
			a.log.Error("panel: a webhook ping was sent and its result row could not be written", "err", err, "endpoint_id", p.ID)
		}
		word := string(p.Result.Class)
		if word == "" {
			word = "ok"
		}
		a.log.Info("panel webhook pinged", "actor_id", id.Admin.AdminUserID, "endpoint_id", p.ID,
			"class", word, "status", p.Result.Status)
		a.redirect(w, apiKeysHref+"?ping="+word+"&status="+strconv.Itoa(p.Result.Status))
	case errors.As(err, &full):
		a.log.Warn("panel webhook ping refused: the hour's bucket is spent", "actor_id", id.Admin.AdminUserID, "endpoint_id", p.ID)
		a.redirect(w, apiKeysHref+"?problem=webhook-bucket-full")
	case a.webhookRefusal(w, r, id, "ping", err):
	default:
		a.log.Error("panel: could not ping the webhook", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelWriteFailed)
	}
}

// isAddressRefusal reports one of the URL rule's sentinels.
func isAddressRefusal(err error) bool {
	for _, s := range []error{egress.ErrNotHTTPS, egress.ErrPort, egress.ErrIPLiteral, egress.ErrUserinfo, egress.ErrQuery,
		egress.ErrFragment, egress.ErrHost, egress.ErrCharacters, egress.ErrTooLong, egress.ErrNotAnswered} {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// refusedWebhookWriteDetail is the refusal row's payload: the act and the roles -- nothing posted.
type refusedWebhookWriteDetail struct {
	Outcome      string `json:"outcome"`
	Act          string `json:"act"`
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
	Via          string `json:"via"`
}

func (a *AdminAuth) refuseWebhookWrite(r *http.Request, id httpx.AdminIdentity, act string) {
	a.log.Warn("panel webhook write refused: this admin is not an owner",
		"act", act, "actor_id", id.Admin.AdminUserID, "role", id.Admin.Role)
	a.record(r.Context(), audit.Event{
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionWebhookRefused,
		Detail: refusedWebhookWriteDetail{
			Outcome: "refused", Act: act, Role: id.Admin.Role, RequiredRole: adminRoleOwner, Via: "panel",
		},
	})
}
