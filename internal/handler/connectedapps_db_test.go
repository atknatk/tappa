package handler

// connectedapps_db_test.go -- the Developers section's "Connected apps" card (issue #37 phase 3, ADR
// 0047 §12) over HTTP against REAL Postgres, with the real oauth.Server behind the panel: a grant made
// by the product's consent and code exchange, listed and disconnected by the owner.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/oauth"
)

// connect is the owner's consent and the code exchange: a live grant of app with scopes.
func (f *consentFixture) connect(app uuid.UUID, scopes ...string) (uuid.UUID, oauth.Tokens) {
	f.t.Helper()
	ctx := context.Background()
	c, err := f.srv.LookupClient(ctx, app)
	if err != nil {
		f.t.Fatal(err)
	}
	verifier := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(verifier))
	code, err := f.srv.Approve(ctx, f.tenantID, f.ownerID, oauth.Request{ClientID: app, RedirectURI: c.RedirectURIs[0], ResponseType: "code",
		Method: oauth.MethodS256, Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), Scopes: oauth.Canonical(scopes)})
	if err != nil {
		f.t.Fatal(err)
	}
	tok, err := f.srv.ExchangeCode(ctx, ctx, c, code, verifier, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	var grant uuid.UUID
	if err := f.owner.QueryRow(ctx, `SELECT id FROM oauth_grants WHERE tenant_id = $1 AND client_id = $2 AND revoked_at IS NULL`, f.tenantID, app).Scan(&grant); err != nil {
		f.t.Fatal(err)
	}
	return grant, tok
}

// TestConnectedApps_TheOwnerListsAndDisconnects (ADR 0047 §12, §17): the owner's card names the app,
// its scopes in words, the approver and "Never" for an unused grant; the API keys page links to it;
// Disconnect -- a bound POST -- ends the grant ('owner', the membership), writes oauth.grant_revoked
// with the owner as actor, and 303s back with the done sentence; the app's token then resolves to
// nothing. A manager gets 403 on both routes and nothing ends; another business's grant id is
// "not connected" and nothing ends; a disabled app is "Turned off by Taptime", the operator's closing
// raises the banner.
func TestConnectedApps_TheOwnerListsAndDisconnects(t *testing.T) {
	f := newConsentFixture(t)
	grant, tok := f.connect(f.client, oauth.ScopeTimesheetsRead, oauth.ScopeEmployeesWrite)
	f.connect(f.clientB, oauth.ScopeEmployeesRead)
	b := f.browser()
	keys := htmlOf(t, b.do(http.MethodGet, apiKeysHref, nil))
	if !strings.Contains(keys, `href="`+connectedAppsHref+`"`) {
		t.Fatal("the API keys page does not link to Connected apps")
	}
	rec := b.do(http.MethodGet, connectedAppsHref, nil)
	page := htmlOf(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("the card: %d", rec.Code)
	}
	for _, want := range []string{"Connected apps", "Malta Calculator", "Another App", oauth.ScopeLabel(oauth.ScopeTimesheetsRead),
		oauth.ScopeLabel(oauth.ScopeEmployeesWrite), "by Consent Admin", "Last used Never", ">Connected<", `name="grant_id" value="` + grant.String() + `"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the card lacks %q", want)
		}
	}
	if strings.Contains(page, "<script") {
		t.Error("the card loads a script")
	}

	// A manager: 403 on the page and on the press; nothing ends.
	f.set(func(f *consentFixture) { f.as.AdminUserID, f.as.Role = f.manager, "manager" })
	if rec := b.do(http.MethodGet, connectedAppsHref, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("a manager's card: %d", rec.Code)
	}
	if rec := b.do(http.MethodPost, appDisconnectHref, url.Values{"grant_id": {grant.String()}}); rec.Code != http.StatusForbidden {
		t.Fatalf("a manager's Disconnect: %d", rec.Code)
	}
	f.set(func(f *consentFixture) { f.as.AdminUserID, f.as.Role = f.ownerID, "owner" })
	if n := f.count(`SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_at IS NULL`, grant); n != 1 {
		t.Fatal("a refused Disconnect ended the grant")
	}
	// Another business's grant id finds nothing here.
	rec = b.do(http.MethodPost, appDisconnectHref, url.Values{"grant_id": {uuid.NewString()}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != connectedAppsHref+"?problem=unknown-app" {
		t.Fatalf("an unknown grant id: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// The owner's Disconnect.
	rec = b.do(http.MethodPost, appDisconnectHref, url.Values{"grant_id": {grant.String()}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != connectedAppsHref+"?done=disconnected" {
		t.Fatalf("Disconnect: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := f.count(`SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_reason = 'owner' AND revoked_by = $2`, grant, f.ownerID); n != 1 {
		t.Fatal("the grant was not ended by the owner")
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'oauth.grant_revoked' AND actor_id = $2 AND target = $3`,
		f.tenantID, f.ownerID, grant.String()); n != 1 {
		t.Fatalf("%d grant_revoked rows", n)
	}
	c, _ := f.srv.CheckAccess(tok.Access)
	if _, err := f.srv.ResolveAccess(context.Background(), c); err == nil {
		t.Fatal("the disconnected app's token still resolves")
	}
	page = htmlOf(t, b.do(http.MethodGet, connectedAppsHref+"?done=disconnected", nil))
	if strings.Contains(page, "Malta Calculator") || !strings.Contains(page, "The app is disconnected") {
		t.Fatal("the card after Disconnect")
	}
	// The remaining app turned off; the operator closes the business's API.
	f.exec(`UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, f.clientB)
	f.exec(`UPDATE tenants SET api_code = 'CAX', api_enabled_at = NULL WHERE id = $1`, f.tenantID)
	page = htmlOf(t, b.do(http.MethodGet, connectedAppsHref, nil))
	if !strings.Contains(page, "Turned off by Taptime") || !strings.Contains(page, "Integrations are turned off for this business by Taptime") ||
		!strings.Contains(page, "Disconnect") {
		t.Fatal("the turned-off states are not drawn (with Disconnect still offered)")
	}
	if strings.Contains(f.logs.String(), tok.Access.RevealInTokenResponse()) || strings.Contains(f.logs.String(), tok.Refresh.RevealInTokenResponse()) {
		t.Fatal("a token reached the log")
	}
}
