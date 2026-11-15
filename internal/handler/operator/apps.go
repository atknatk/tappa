package operator

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/oauth"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// THE APP REGISTRY (issue #37, ADR 0047 §10, OA-7 = (a); migration 00107). The platform operator
// registers the apps a business may connect to (the "Connected apps" consent, ADR 0047 §4), and only
// here: op_register_oauth_client, op_rotate_oauth_client_secret, op_set_oauth_client_redirects and
// op_disable_oauth_client are the registry's only writers, op_read_oauth_clients its only reader. Five
// routes in the console's group (routes.go, mount) -- host gate, security headers, flood gate,
// same-origin gate, requireOperator, sessionGate:
//
//	GET  /operator/apps                      the list (a two-phase read), and the sentence of the last
//	                                         write (the list's own read says what happened -- ADR 0021
//	                                         §2 v 7, holdings.go's rule)
//	POST /operator/apps                      register: name, redirect URIs, allowed scopes -> the
//	                                         client_id and the secret, shown ONCE (200, no-store)
//	POST /operator/apps/{id}/rotate          a new secret, shown once; the old one is refused from the
//	                                         next request on (issued tokens are untouched)
//	POST /operator/apps/{id}/redirects       the whole redirect list replaced
//	POST /operator/apps/{id}/disable         the app is closed for every business, one way
//
// 🔴 THE SECRET IS MINTED HERE AND SHOWN ONCE. ClientSecrets mints it and its HMAC under
// TAPPA_OAUTH_HMAC_KEY; the op_* gets the hash and the hint, never the secret. The secret leaves the
// process only in the register/rotate answer's body (oauth.Value.RevealToTheOperatorOnce) -- never a
// log line, an error, the operator's audit row (which names the client id alone,
// operator_audit_log_oauth_client_shape) or a redirect. There is no "show it again".
//
// 🔴 THE REDIRECT URI RULE IS GO'S AND SQL'S (ADR 0047 §10.2): oauth.RegistrationURIsError applies
// the SQL expression verbatim and adds to it, and refuses a loopback URI unless this is the sandbox
// build -- the one rule SQL cannot hold. webhooks:manage is not offered and the scope set is the
// closed one (§13).
//
// WHAT IS LOGGED: the app's id, the operator's id and internal/db's error (a call and a SQLSTATE).
// Never the session token or its hash, never the secret or its hash.

// OAuthClientStore is the operator database's slice the registry screen needs (*db.OperatorDB,
// operator.go), declared at the consumer (§7). sessionHash is operatorauth's SessionHash of the
// request's token. A write's nil says the call was ACCEPTED (ADR 0021 §2 v 7); a refused argument is
// SQLSTATE 22023; a dead session is db.ErrOperatorRefused.
type OAuthClientStore interface {
	OAuthClients(ctx context.Context, sessionHash string, page int32) ([]db.OAuthClientRow, error)
	RegisterOAuthClient(ctx context.Context, sessionHash, name string, redirectURIs, scopes []string, secretHash, secretHint string) (uuid.UUID, error)
	RotateOAuthClientSecret(ctx context.Context, sessionHash string, clientID uuid.UUID, secretHash, secretHint string) error
	SetOAuthClientRedirects(ctx context.Context, sessionHash string, clientID uuid.UUID, redirectURIs []string) error
	DisableOAuthClient(ctx context.Context, sessionHash string, clientID uuid.UUID) error
}

// ClientSecrets mints a client secret and its stored hash (*oauth.Hasher).
type ClientSecrets interface {
	MintClientSecret() (oauth.Value, oauth.Digest, error)
}

// The registry's bounds: a write detached from the client, a read of the list.
const (
	appWriteTimeout = 10 * time.Second
	appReadTimeout  = 5 * time.Second
	// maxAppName is 00104's oauth_clients_name_shape bound.
	maxAppName = 64
)

const pathApps = Prefix + "/apps"

// appDone is what a registry write asked for, as the 303 after it names it.
type appDone string

const (
	appRedirectsChanged appDone = "redirects"
	appDisabled         appDone = "disabled"
)

// appResultPath is the list with what a write asked for -- the 303's target: a closed word and the
// app's canonical id the path parsed (tenantID's rule), nothing a client sent but values the handler
// parsed.
func appResultPath(done appDone, app uuid.UUID) string {
	return pathApps + "?done=" + string(done) + "&app=" + app.String()
}

// appNotices are the list's sentences after a write, and a refused form's.
const (
	appNoticeNone = iota
	appNoticeName
	appNoticeRedirects
	appNoticeScopes
	appNoticeRedirectsChanged
	appNoticeDisabled
)

// appName is the posted name as op_register_oauth_client is sent it: trimmed, 1..64 characters, no
// control or bidi formatting character (00104's isolation rule), or false.
func appName(raw string) (string, bool) {
	n := strings.TrimSpace(raw)
	if n == "" || utf8.RuneCountInString(n) > maxAppName || !utf8.ValidString(n) {
		return "", false
	}
	for _, r := range n {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return "", false
		}
	}
	return n, true
}

// appRedirects is the posted list: one URI per line, blank lines dropped, each held to the registry
// rule (Go's, at least as strict as SQL's; loopback only in the sandbox build).
func appRedirects(raw string) ([]string, error) {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if u := strings.TrimSpace(line); u != "" {
			out = append(out, u)
		}
	}
	if err := oauth.RegistrationURIsError(out); err != nil {
		return nil, err
	}
	return out, nil
}

// appScopes is the posted checkboxes: words of the closed set, at least one, canonical.
func appScopes(vs []string) ([]string, bool) {
	if len(vs) == 0 {
		return nil, false
	}
	for _, v := range vs {
		if !oauth.Known(v) {
			return nil, false
		}
	}
	return oauth.Canonical(vs), true
}

// appsPage is GET /operator/apps.
func (s *Surface) appsPage(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	notice := appNoticeNone
	switch r.URL.Query().Get("done") {
	case string(appRedirectsChanged):
		notice = appNoticeRedirectsChanged
	case string(appDisabled):
		notice = appNoticeDisabled
	}
	var asked uuid.UUID
	if notice != appNoticeNone {
		if a, ok := tenantID(r.URL.Query().Get("app")); ok {
			asked = a
		} else {
			notice = appNoticeNone
		}
	}
	s.renderApps(w, r, http.StatusOK, id, hash, appsNotice{kind: notice, app: asked}, nil)
}

// appsNotice is what the list says above itself.
type appsNotice struct {
	kind int
	app  uuid.UUID
	// reason is a refused form's sentence (oauth's fixed error text for a redirect list).
	reason string
}

// renderApps reads the list (a read unit) and draws it with the notice and, when shown, the one-time
// secret. A ROTATION'S answer (shown with ExpectHint) has paid its read unit before the write
// (rotateAppSecret), and a read that fails after the write says the secret MAY have changed -- never
// that it did not.
func (s *Surface) renderApps(w http.ResponseWriter, r *http.Request, status int, id operatorauth.Identity, hash string,
	notice appsNotice, shown *operatorpages.AppSecretView) {
	rotation := shown != nil && shown.ExpectHint != ""
	if !rotation && !s.spendRead(w, r, id) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), appReadTimeout)
	defer cancel()
	rows, err := s.appStore.OAuthClients(ctx, hash, 1)
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return
	case err != nil && rotation:
		s.log.ErrorContext(r.Context(), "operator: the apps could not be read after a rotation", "app_id", shown.ClientID, "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemAppRotateUnconfirmed)
		return
	case err != nil:
		s.log.ErrorContext(r.Context(), "operator: the apps could not be read", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemAppsUnreadable)
		return
	}
	v := operatorpages.AppsView{Scopes: appScopeOptions()}
	for _, row := range rows {
		a := operatorpages.AppRow{
			ID: row.ClientID.String(), ClientID: oauth.ClientIDOf(row.ClientID), Name: row.Name,
			RedirectURIs: row.RedirectURIs, Scopes: oauth.Canonical(row.AllowedScopes), SecretHint: row.SecretHint,
			CreatedAt: row.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"), Disabled: row.DisabledAt != nil,
			RotatePath: pathApps + "/" + row.ClientID.String() + "/rotate", RedirectsPath: pathApps + "/" + row.ClientID.String() + "/redirects",
			DisablePath: pathApps + "/" + row.ClientID.String() + "/disable",
		}
		if row.DisabledAt != nil {
			a.DisabledAt = row.DisabledAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		v.Apps = append(v.Apps, a)
	}
	v.Notice = appNoticeText(notice, rows)
	if shown != nil && shown.ExpectHint != "" {
		// A rotation: the secret is shown only when the read shows it took (a live app whose hint is
		// the new secret's) -- the write's own answer says nothing (ADR 0021 §2 v 7).
		i := slices.IndexFunc(rows, func(row db.OAuthClientRow) bool { return oauth.ClientIDOf(row.ClientID) == shown.ClientID })
		switch {
		case i >= 0 && rows[i].DisabledAt != nil:
			// The database rotates no turned-off app: this one is the read's own fact.
			shown = nil
			v.Notice = operatorpages.AppNotice{Alert: true, Text: "No secret was changed: the app is turned off."}
		case i < 0 || rows[i].SecretHint != shown.ExpectHint:
			// Not on this page, or another rotation came after: the write may have changed the secret.
			shown = nil
			v.Notice = operatorpages.AppNotice{Alert: true, Text: "The secret may have been changed, and this page cannot show the new one. " +
				"Rotate it again and copy the secret that page shows."}
		}
	}
	v.Secret = shown
	s.render(w, r, status, operatorpages.Apps(v))
}

// appScopeOptions is the registration form's checkboxes: the closed set, each in words.
func appScopeOptions() []operatorpages.AppScopeOption {
	out := make([]operatorpages.AppScopeOption, 0, len(oauth.KnownScopes))
	for _, w := range oauth.KnownScopes {
		out = append(out, operatorpages.AppScopeOption{Word: w, Label: oauth.ScopeLabel(w)})
	}
	return out
}

// appNoticeText is the sentence the list's own read supports.
func appNoticeText(n appsNotice, rows []db.OAuthClientRow) operatorpages.AppNotice {
	find := func() (db.OAuthClientRow, bool) {
		i := slices.IndexFunc(rows, func(r db.OAuthClientRow) bool { return r.ClientID == n.app })
		if i < 0 {
			return db.OAuthClientRow{}, false
		}
		return rows[i], true
	}
	switch n.kind {
	case appNoticeName:
		return operatorpages.AppNotice{Alert: true, Text: "Nothing was saved: the name must be 1 to 64 characters, with no control characters."}
	case appNoticeRedirects:
		return operatorpages.AppNotice{Alert: true, Text: "Nothing was saved: " + strings.TrimPrefix(n.reason, "oauth: ") + "."}
	case appNoticeScopes:
		return operatorpages.AppNotice{Alert: true, Text: "Nothing was saved: pick at least one scope."}
	case appNoticeRedirectsChanged:
		if row, ok := find(); ok && row.DisabledAt == nil {
			return operatorpages.AppNotice{Text: "The redirect URIs of " + row.Name + " are now the list below."}
		}
		return operatorpages.AppNotice{Alert: true, Text: "The redirect URIs were not changed: the app is turned off or not on this page."}
	case appNoticeDisabled:
		if row, ok := find(); ok && row.DisabledAt != nil {
			return operatorpages.AppNotice{Text: row.Name + " is turned off: no business can connect to it, and its tokens stop working."}
		}
		return operatorpages.AppNotice{Alert: true, Text: "The app is not shown as turned off. Check the list before you try again."}
	}
	return operatorpages.AppNotice{}
}

// writeApp runs one registry write detached from the client, mapping a dead session to the sign-in
// and anything else to the fault page. It reports whether it was accepted.
func (s *Surface) writeApp(w http.ResponseWriter, r *http.Request, what string, app uuid.UUID, write func(context.Context) error) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), appWriteTimeout)
	defer cancel()
	err := write(ctx)
	switch {
	case errors.Is(err, db.ErrOperatorRefused):
		s.redirect(w, pathSignIn)
		return false
	case err != nil:
		s.log.ErrorContext(r.Context(), "operator: an app registry write failed", "what", what, "app_id", app.String(), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemAppNotSaved)
		return false
	}
	return true
}

// registerApp is POST /operator/apps.
func (s *Surface) registerApp(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemAppFormTooLarge) {
		return
	}
	r = answered(r)
	name, ok := appName(r.PostForm.Get("name"))
	if !ok {
		s.renderApps(w, r, http.StatusUnprocessableEntity, id, hash, appsNotice{kind: appNoticeName}, nil)
		return
	}
	redirects, err := appRedirects(r.PostForm.Get("redirect_uris"))
	if err != nil {
		s.renderApps(w, r, http.StatusUnprocessableEntity, id, hash, appsNotice{kind: appNoticeRedirects, reason: err.Error()}, nil)
		return
	}
	scopes, ok := appScopes(r.PostForm["scope"])
	if !ok {
		s.renderApps(w, r, http.StatusUnprocessableEntity, id, hash, appsNotice{kind: appNoticeScopes}, nil)
		return
	}
	secret, digest, err := s.secrets.MintClientSecret()
	if err != nil {
		s.log.ErrorContext(r.Context(), "operator: a client credential could not be minted", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemAppNotSaved)
		return
	}
	var app uuid.UUID
	if !s.writeApp(w, r, "register", uuid.Nil, func(ctx context.Context) error {
		var err error
		app, err = s.appStore.RegisterOAuthClient(ctx, hash, name, redirects, scopes, digest.RevealToTheDatabase(), secret.SecretHint())
		return err
	}) {
		return
	}
	s.log.InfoContext(r.Context(), "operator registered an app", "app_id", app.String(), "admin_id", id.AdminID.String())
	s.renderApps(w, r, http.StatusOK, id, hash, appsNotice{}, &operatorpages.AppSecretView{
		Heading: name + " is registered", ClientID: oauth.ClientIDOf(app), Secret: secret.RevealToTheOperatorOnce(),
	})
}

// appOf is the path's app id, or the 404.
func (s *Surface) appOf(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	app, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemAppNotAnID)
	}
	return app, ok
}

// rotateAppSecret is POST /operator/apps/{id}/rotate. The database rotates only an app that exists and
// is not disabled and answers the same either way, so the secret is shown only when the list read
// after the write shows the app live with the new hint.
func (s *Surface) rotateAppSecret(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	app, ok := s.appOf(w, r)
	if !ok {
		return
	}
	// The answer's read unit is paid BEFORE the write: a budget refused after it would hide a secret
	// that may already be the app's.
	if !s.spendRead(w, r, id) {
		return
	}
	r = answered(r)
	secret, digest, err := s.secrets.MintClientSecret()
	if err != nil {
		s.log.ErrorContext(r.Context(), "operator: a client credential could not be minted", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemAppNotSaved)
		return
	}
	if !s.writeApp(w, r, "rotate", app, func(ctx context.Context) error {
		return s.appStore.RotateOAuthClientSecret(ctx, hash, app, digest.RevealToTheDatabase(), secret.SecretHint())
	}) {
		return
	}
	s.log.InfoContext(r.Context(), "operator rotated an app's credential", "app_id", app.String(), "admin_id", id.AdminID.String())
	s.renderApps(w, r, http.StatusOK, id, hash, appsNotice{}, &operatorpages.AppSecretView{
		Heading: "A new secret", ClientID: oauth.ClientIDOf(app), Secret: secret.RevealToTheOperatorOnce(),
		ExpectHint: secret.SecretHint(),
	})
}

// setAppRedirects is POST /operator/apps/{id}/redirects.
func (s *Surface) setAppRedirects(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	app, ok := s.appOf(w, r)
	if !ok {
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemAppFormTooLarge) {
		return
	}
	r = answered(r)
	redirects, err := appRedirects(r.PostForm.Get("redirect_uris"))
	if err != nil {
		s.renderApps(w, r, http.StatusUnprocessableEntity, id, hash, appsNotice{kind: appNoticeRedirects, reason: err.Error()}, nil)
		return
	}
	if !s.writeApp(w, r, "redirects", app, func(ctx context.Context) error {
		return s.appStore.SetOAuthClientRedirects(ctx, hash, app, redirects)
	}) {
		return
	}
	s.log.InfoContext(r.Context(), "operator changed an app's redirect URIs", "app_id", app.String(), "admin_id", id.AdminID.String())
	s.redirect(w, appResultPath(appRedirectsChanged, app))
}

// disableApp is POST /operator/apps/{id}/disable.
func (s *Surface) disableApp(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	app, ok := s.appOf(w, r)
	if !ok {
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemAppFormTooLarge) {
		return
	}
	// One way and for every business: the suspension's confirm box (suspension.go), a wrong click
	// changes nothing.
	if r.PostForm.Get("confirm") != suspensionConfirmed {
		s.problem(w, r, http.StatusBadRequest, problemAppDisableUnconfirmed)
		return
	}
	r = answered(r)
	if !s.writeApp(w, r, "disable", app, func(ctx context.Context) error {
		return s.appStore.DisableOAuthClient(ctx, hash, app)
	}) {
		return
	}
	s.log.InfoContext(r.Context(), "operator turned an app off", "app_id", app.String(), "admin_id", id.AdminID.String())
	s.redirect(w, appResultPath(appDisabled, app))
}
