package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/oauth"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE CONSENT HALF OF "CONNECTED APPS" (issue #37, ADR 0047 §3, §4): the owner of a business, signed
// in to the panel, lets an app the operator registered act for that ONE business with the scopes
// named on the page. Five routes, each on the chain ADR 0047 §4.2's table fixes:
//
//	GET  /oauth/authorize                   internal/handler/oauthapi's front door (its own address
//	                                        ceiling) -> 303 to /admin/oauth/authorize with the same
//	                                        query; no cookie read or written, no database
//	GET  /admin/oauth/authorize             floodGate("admin_oauth") -> oauthRequestGate (V1-V7; V1/V2
//	                                        refused with Taptime's page and no redirect, V3-V7 with a
//	                                        303 to the REGISTERED URI) -> RequireAdmin (signed out: the
//	                                        pending request cookie is written, 303 /admin/login) ->
//	                                        sessionGate -> the page (the cookie is written).
//	                                        No sameOriginGate (a navigation carries no Origin) and NO
//	                                        mintGate -- the named exception below
//	GET  /admin/oauth/authorize/continue    floodGate("admin_oauth") -> RequireAdmin (signed out: 303
//	                                        /admin/login, the cookie kept) -> sessionGate -> mintGate ->
//	                                        the page drawn from the cookie, V1/V2/V6 re-run on the live
//	                                        client
//	POST /admin/oauth/authorize             ProtectWriting (route class 1: the suspension ring refuses
//	                                        a suspended business) -- the approval
//	POST /admin/oauth/authorize/deny        protectWritingThroughSuspension (route class 2, ADR 0025's
//	                                        exception list by name) -- the refusal, which writes nothing
//
// 🔴 THE PAGE APPLIES THE COOKIE, NEVER THE QUERY (§4.2, O-4). Every request that passed V1-V7 is
// written -- signed by oauth.Hasher, ten minutes -- into tappa_oauth_request, signed in or not; the
// forms carry no request parameter, only the digest of the cookie the page was drawn from (rd). A
// second authorization request that overwrote the cookie in another tab makes the first page's
// "Allow" refuse ("This request changed") instead of approving the second app.
//
// 🔴 THE NAMED EXCEPTION TO T14/T19 (ADR 0047 §4.2, DÜŞÜK-6). GET /admin/oauth/authorize arrives
// cross-site by definition (a top-level navigation from the app's site) and writes a cookie, which
// every other cookie-writing panel GET may not do from another site (mintGate, fetchsite.go). What it
// writes is a signed PENDING REQUEST only: no grant, no code, no audit row, no tenant data; the request
// passed V1-V2 (a registered app and its registered URI); the approval is a separate same-origin POST
// behind the Origin check, the form binding and rd.
//
// 🔴 TWO NAMED EXCEPTIONS TO S14 (formbinding.go: "every write redirects to a fixed panel path"):
// the approval and the refusal answer a 303 to an EXTERNAL address -- the registered redirect URI the
// cookie holds, re-matched byte for byte against the live registry -- with code/error, state and iss
// only; the form binding never travels.
//
// THE TENANT IS THE SESSION'S (§4.3, ADR 0031 §3). The page names the business by its name and offers
// the person's other owner memberships as the ordinary switch forms (POST /admin/switch); a switch,
// like a sign-in, lands back on /continue when a valid pending request is in the browser.
//
// WHAT IS LOGGED: the app's client_id, the tenant and the admin's ids. Never the cookie, rd, the state,
// the code or the challenge (ADR 0047 §7.4).

// The consent routes.
const (
	oauthAuthorizeHref   = "/admin/oauth/authorize"
	oauthContinueHref    = "/admin/oauth/authorize/continue"
	oauthDenyHref        = "/admin/oauth/authorize/deny"
	oauthConsentMaxBody  = 4 << 10
	oauthDigestFieldName = "rd"
)

// panelOAuth is the slice of the authorization server the consent needs (*oauth.Server), declared at
// the consumer (§7).
type panelOAuth interface {
	LookupClient(ctx context.Context, id uuid.UUID) (oauth.Client, error)
	Approve(ctx context.Context, tenantID, adminID uuid.UUID, r oauth.Request) (oauth.Value, error)
	IntegrationsOff(ctx context.Context, tenantID uuid.UUID) (bool, error)
	Hasher() *oauth.Hasher
}

// OAuthConsent gives the panel the authorization server (cmd/tappa passes it on every configuration:
// TAPPA_OAUTH_HMAC_KEY is required, and NewAdminAuth refuses a configuration that holds the key
// without the server).
func OAuthConsent(o panelOAuth) AdminAuthOption {
	return func(a *AdminAuth) { a.oauth = o }
}

// The consent's problem pages. None names the app's URI, the state or anything of the request.
var (
	problemOAuthNoApp = pages.ProblemView{
		Title:   "This connection link is not valid",
		Message: "The app that sent you here is not registered with Taptime, is turned off, or asked to return to an address it did not register. Nothing was shared.",
		Hint:    "Go back to the app and start connecting again. If it keeps happening, contact the app's support.",
	}
	problemOAuthChanged = pages.ProblemView{
		Title:   "This request changed — start again from the app",
		Message: "The connection request on this page is not the one your browser holds any more, or it expired. Nothing was shared.",
		Hint:    "Go back to the app and press Connect again.",
	}
	problemOAuthUnavailable = pages.ProblemView{
		Title:   "Connecting apps is not available",
		Message: "Taptime could not reach the part of the panel that connects apps. Nothing was shared.",
		Hint:    "Try again in a minute.",
	}
)

// oauthRequestKey carries the validated, sealed request from oauthRequestGate to the handler.
type oauthRequestKey struct{}

type pendingOAuth struct {
	sealed string
	req    oauth.Request
	client oauth.Client
}

// oauthNow is the pending request's clock -- a field so a test can age a cookie.
func (a *AdminAuth) oauthNow() time.Time {
	if a.oauthClock != nil {
		return a.oauthClock()
	}
	return time.Now()
}

// issuer is RFC 9207's iss: this deployment's own origin (TAPTIME_OAUTH_BASE_URL on the client's
// side, https://taptime.mt in production).
func (a *AdminAuth) issuer() string { return a.baseURL }

// mountOAuthReads registers the two consent GETs; the two POSTs are in mountWriting (the census of
// write routes reads that function). The front door, GET /oauth/authorize, is outside the panel's
// cookie path and reads no session: internal/handler/oauthapi mounts it.
func (a *AdminAuth) mountOAuthReads(r chi.Router) {
	r.With(a.floodGate("admin_oauth"), a.oauthRequestGate,
		httpx.RequireAdmin(a.cookies, a.admins, http.HandlerFunc(a.oauthSignInFirst)), a.sessionGate).
		Get(oauthAuthorizeHref, a.oauthConsentPage)
	r.With(a.floodGate("admin_oauth"), a.requireAdmin(), a.sessionGate, a.mintGate(armsAlways)).
		Get(oauthContinueHref, a.oauthContinue)
}

// oauthRequestGate is V1-V7 ahead of the identity (§4.2): the cookie must be written for a signed-out
// browser too, so the request is validated before RequireAdmin.
func (a *AdminAuth) oauthRequestGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.oauth == nil {
			a.renderProblem(w, r, http.StatusServiceUnavailable, problemOAuthUnavailable)
			return
		}
		v, err := oauth.Authorize(r.Context(), r.URL.RawQuery, a.oauth.LookupClient)
		if err != nil {
			a.log.Error("panel: the app registry could not be read", "err", err)
			a.renderProblem(w, r, http.StatusServiceUnavailable, problemOAuthUnavailable)
			return
		}
		switch v.Verdict {
		case oauth.VerdictFatal:
			a.renderProblem(w, r, http.StatusBadRequest, problemOAuthNoApp)
			return
		case oauth.VerdictRedirectError:
			a.redirect(w, oauth.ErrorRedirect(v.RedirectURI, v.Error, v.State, a.issuer()))
			return
		}
		req := v.Request
		req.Created = a.oauthNow().Unix()
		sealed, err := a.oauth.Hasher().SealRequest(req)
		if errors.Is(err, oauth.ErrRequestTooLarge) {
			// The state (V7's parameter) is what can make it so: answered as V7's failure is -- the
			// registered URI, invalid_request, and the state NOT reflected.
			a.redirect(w, oauth.ErrorRedirect(v.RedirectURI, oauth.ErrorInvalidRequest, "", a.issuer()))
			return
		}
		if err != nil {
			a.renderProblem(w, r, http.StatusServiceUnavailable, problemOAuthUnavailable)
			return
		}
		p := pendingOAuth{sealed: sealed, req: req, client: v.Client}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), oauthRequestKey{}, p)))
	})
}

func pendingOf(r *http.Request) (pendingOAuth, bool) {
	p, ok := r.Context().Value(oauthRequestKey{}).(pendingOAuth)
	return p, ok
}

// writePending writes the pending request cookie (§4.2: HttpOnly, Secure outside local http, SameSite
// Lax, Path /admin, ten minutes). One pending request: a new one replaces the old.
func (a *AdminAuth) writePending(w http.ResponseWriter, sealed string) {
	a.short.set(w, oauth.RequestCookieName, sealed, int(oauth.RequestMaxAge/time.Second))
}

// oauthSignInFirst is RequireAdmin's signed-out branch on GET /admin/oauth/authorize: the request is
// kept in the cookie and the browser is sent to the sign-in page, whose success lands on /continue.
func (a *AdminAuth) oauthSignInFirst(w http.ResponseWriter, r *http.Request) {
	if p, ok := pendingOf(r); ok {
		a.writePending(w, p.sealed)
	}
	a.requireLogin(w, r)
}

// readPending opens the browser's pending request (signature, age) and re-runs V1, V2 and V6 on the
// live client (§4.2's items 1-8). ok false: the answer is written. refusing is the refusal's read: a
// V6 that no longer holds does not stop it (§4.5 -- the refusal is not an error path; the app hears
// access_denied either way), while V1/V2 still do (its redirect goes to a registered URI only).
func (a *AdminAuth) readPending(w http.ResponseWriter, r *http.Request, refusing bool) (pendingOAuth, bool) {
	if a.oauth == nil {
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemOAuthUnavailable)
		return pendingOAuth{}, false
	}
	c, err := r.Cookie(oauth.RequestCookieName)
	if err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemOAuthChanged)
		return pendingOAuth{}, false
	}
	req, err := a.oauth.Hasher().OpenRequest(c.Value, a.oauthNow())
	if err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemOAuthChanged)
		return pendingOAuth{}, false
	}
	client, err := a.oauth.LookupClient(r.Context(), req.ClientID)
	if errors.Is(err, oauth.ErrNoClient) {
		a.renderProblem(w, r, http.StatusBadRequest, problemOAuthNoApp)
		return pendingOAuth{}, false
	}
	if err != nil {
		a.log.Error("panel: the app registry could not be read", "err", err)
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemOAuthUnavailable)
		return pendingOAuth{}, false
	}
	redirectOK, scopesOK := oauth.Recheck(client, req)
	if !redirectOK {
		a.short.clear(w, oauth.RequestCookieName)
		a.renderProblem(w, r, http.StatusBadRequest, problemOAuthNoApp)
		return pendingOAuth{}, false
	}
	if !scopesOK && !refusing {
		a.short.clear(w, oauth.RequestCookieName)
		a.redirect(w, oauth.ErrorRedirect(req.RedirectURI, oauth.ErrorInvalidScope, req.State, a.issuer()))
		return pendingOAuth{}, false
	}
	return pendingOAuth{sealed: c.Value, req: req, client: client}, true
}

// oauthConsentPage is GET /admin/oauth/authorize for a signed-in browser.
func (a *AdminAuth) oauthConsentPage(w http.ResponseWriter, r *http.Request) {
	p, ok := pendingOf(r)
	if !ok {
		a.renderProblem(w, r, http.StatusBadRequest, problemOAuthChanged)
		return
	}
	a.writePending(w, p.sealed)
	a.renderConsent(w, r, p)
}

// oauthContinue is GET /admin/oauth/authorize/continue: after a sign-in or a switch, the page drawn
// from the cookie.
func (a *AdminAuth) oauthContinue(w http.ResponseWriter, r *http.Request) {
	p, ok := a.readPending(w, r, false)
	if !ok {
		return
	}
	a.renderConsent(w, r, p)
}

// consentStateOf is which page the session sees (§4.2's table, §11.4).
func (a *AdminAuth) consentStateOf(ctx context.Context, id httpx.AdminIdentity) (pages.OAuthConsentState, error) {
	off, err := a.oauth.IntegrationsOff(ctx, id.TenantID())
	if err != nil {
		return 0, err
	}
	switch {
	case off:
		return pages.OAuthConsentIntegrationsOff, nil
	case id.Admin.Role != adminRoleOwner:
		return pages.OAuthConsentOwnersOnly, nil
	}
	if _, suspended := id.Suspension(); suspended {
		return pages.OAuthConsentSuspended, nil
	}
	return pages.OAuthConsentAsk, nil
}

// renderConsent draws the page (or its owners-only, suspended, integrations-off states) for p.
func (a *AdminAuth) renderConsent(w http.ResponseWriter, r *http.Request, p pendingOAuth) {
	id := httpx.AdminOf(r)
	state, err := a.consentStateOf(r.Context(), id)
	if err != nil {
		a.log.Error("panel: the consent page could not read the business", "tenant_id", id.TenantID(), "err", err)
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemPanelUnavailable)
		return
	}
	v := pages.OAuthConsentView{
		State:        state,
		AppName:      p.client.Name,
		ReturnHost:   oauth.RedirectHost(p.req.RedirectURI),
		BusinessName: businessName(id.Admin.TenantName),
		AllowHref:    oauthAuthorizeHref,
		DenyHref:     oauthDenyHref,
		Digest:       a.oauth.Hasher().RequestDigest(p.sealed),
		Script:       oauthConsentScript,
	}
	for _, s := range p.req.Scopes {
		v.Scopes = append(v.Scopes, pages.OAuthScopeLine{Word: s, Label: oauth.ScopeLabel(s)})
	}
	if a.identityGate.Open(adminauth.SurfaceSwitch) {
		a.otherOwnerships(r, id, &v)
	}
	a.renderConsentPage(w, r, oauth.RedirectOrigin(p.req.RedirectURI), v)
}

// otherOwnerships fills the "Not this business?" list: the person's OTHER businesses where they are an
// owner and the business is not suspended (§4.3), each a switch form. A failure draws no list.
func (a *AdminAuth) otherOwnerships(r *http.Request, id httpx.AdminIdentity, v *pages.OAuthConsentView) {
	token, err := a.cookies.Read(r)
	if err != nil {
		return
	}
	ms, err := a.admins.Memberships(r.Context(), token)
	if err != nil {
		a.log.Warn("panel: the consent page could not list the person's businesses", "actor_id", id.AdminUserID(), "err", err)
		return
	}
	var others []pages.AdminSwitchBusiness
	for _, m := range ms {
		if m.Current || m.Suspended || m.Role != adminRoleOwner {
			continue
		}
		others = append(others, pages.AdminSwitchBusiness{TenantID: m.TenantID.String(), TenantName: m.TenantName, Role: m.Role})
	}
	if len(others) == 0 {
		return
	}
	csrf, err := a.switches.Mint(token, id.Admin.SessionID)
	if err != nil {
		return
	}
	v.Others, v.SwitchHref, v.SwitchCSRF = others, adminSwitchHref, csrf
}

// oauthConsentScript is the consent page's one script: the Allow button's arming delay after the page
// becomes visible and focused (the double-clickjacking defence, the ADR's "Uygulama notları").
const oauthConsentScript = "/static/js/oauthconsent.js"

// oauthConsentCSP is the consent page family's policy (§4.6, plan A): the panel's adminCSP with
// form-action widened by EXACTLY the origin of the redirect URI this request matched -- no other
// client's and no other registered URI's -- because a browser may apply form-action to the redirect
// chain of the form's submission; plus script-src 'self' for the one script. frame-ancestors 'none'
// (and X-Frame-Options: DENY) against clickjacking.
//
// ⚠️ NOT MEASURED IN A BROWSER (ADR 0047 §4.6, §17): whether Chrome, Firefox and Safari block the
// external 303 under 'self' alone, and whether plan A lets it through, is the delivery's open
// measurement; plan B (a 200 interstitial) is the fallback.
func oauthConsentCSP(returnOrigin string) string {
	formAction := "form-action 'self'"
	if returnOrigin != "" {
		formAction += " " + returnOrigin
	}
	return strings.Replace(adminCSP, "form-action 'self'", formAction, 1) + "; script-src 'self'"
}

// renderConsentPage writes the page with its headers.
func (a *AdminAuth) renderConsentPage(w http.ResponseWriter, r *http.Request, returnOrigin string, v pages.OAuthConsentView) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	a.renderWithPolicy(w, r, http.StatusOK, pages.OAuthConsent(v), oauthConsentCSP(returnOrigin))
}

// readConsentForm reads the approval's or refusal's body (bounded) and checks rd against the cookie.
func (a *AdminAuth) readConsentForm(w http.ResponseWriter, r *http.Request, refusing bool) (pendingOAuth, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, oauthConsentMaxBody)
	if err := r.ParseForm(); err != nil {
		a.renderProblem(w, r, http.StatusBadRequest, problemOAuthChanged)
		return pendingOAuth{}, false
	}
	p, ok := a.readPending(w, r, refusing)
	if !ok {
		return pendingOAuth{}, false
	}
	if !a.oauth.Hasher().SameRequest(p.sealed, r.PostForm.Get(oauthDigestFieldName)) {
		a.log.Warn("panel: a consent form was posted for another pending request", "client_id", oauth.ClientIDOf(p.req.ClientID))
		a.renderProblem(w, r, http.StatusConflict, problemOAuthChanged)
		return pendingOAuth{}, false
	}
	return p, true
}

// oauthApprove is POST /admin/oauth/authorize (§4.5): the ten-item list re-checked -- the cookie and
// rd, V1-V7 on the live client (readPending), the panel chain (ProtectWriting: Origin, identity, form
// binding, the suspension ring), and in the write's own transaction the active owner, the suspension
// and the operator's closing (oauth.Server.Approve) -- then the code and the 303.
func (a *AdminAuth) oauthApprove(w http.ResponseWriter, r *http.Request) {
	p, ok := a.readConsentForm(w, r, false)
	if !ok {
		return
	}
	id := httpx.AdminOf(r)
	if r.PostForm.Get("decision") != "allow" {
		a.renderProblem(w, r, http.StatusBadRequest, problemOAuthChanged)
		return
	}
	code, err := a.oauth.Approve(r.Context(), id.TenantID(), id.AdminUserID(), p.req)
	switch {
	case errors.Is(err, oauth.ErrNotOwner), errors.Is(err, oauth.ErrSuspended), errors.Is(err, oauth.ErrIntegrationsOff):
		a.short.clear(w, oauth.RequestCookieName)
		a.log.Info("panel: an app's consent was refused", "client_id", oauth.ClientIDOf(p.req.ClientID),
			"tenant_id", id.TenantID(), "actor_id", id.AdminUserID(), "reason", err)
		a.redirect(w, oauth.ErrorRedirect(p.req.RedirectURI, oauth.ErrorAccessDenied, p.req.State, a.issuer()))
		return
	case err != nil:
		a.log.Error("panel: an app's consent could not be written", "client_id", oauth.ClientIDOf(p.req.ClientID),
			"tenant_id", id.TenantID(), "err", err)
		a.renderProblem(w, r, http.StatusServiceUnavailable, problemPanelWriteFailed)
		return
	}
	a.short.clear(w, oauth.RequestCookieName)
	a.log.Info("panel: an app was approved", "client_id", oauth.ClientIDOf(p.req.ClientID),
		"tenant_id", id.TenantID(), "actor_id", id.AdminUserID())
	a.redirect(w, oauth.CodeRedirect(p.req.RedirectURI, code, p.req.State, a.issuer()))
}

// oauthDeny is POST /admin/oauth/authorize/deny (§4.5): no row is written; the cookie is cleared and
// the app gets error=access_denied (with the state and iss) at its registered URI -- in a suspended
// business too (this route is outside the suspension ring).
func (a *AdminAuth) oauthDeny(w http.ResponseWriter, r *http.Request) {
	p, ok := a.readConsentForm(w, r, true)
	if !ok {
		return
	}
	a.short.clear(w, oauth.RequestCookieName)
	a.redirect(w, oauth.ErrorRedirect(p.req.RedirectURI, oauth.ErrorAccessDenied, p.req.State, a.issuer()))
}

// landingAfterSignIn is where a sign-in or a switch sends the browser: /continue when it holds a valid
// pending request (§4.2: ONE fixed path, never read from the request -- no open redirect), else
// fallback.
func (a *AdminAuth) landingAfterSignIn(r *http.Request, fallback string) string {
	if a.oauth == nil {
		return fallback
	}
	c, err := r.Cookie(oauth.RequestCookieName)
	if err != nil {
		return fallback
	}
	if _, err := a.oauth.Hasher().OpenRequest(c.Value, a.oauthNow()); err != nil {
		return fallback
	}
	return oauthContinueHref
}
