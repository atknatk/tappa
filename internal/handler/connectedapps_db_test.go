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

// appsCardOf is the Connected apps card's markup on a Developers page: from its section's opening tag to
// its closing one (the card holds no nested section).
func appsCardOf(t *testing.T, page string) string {
	t.Helper()
	i := strings.Index(page, `id="connected-apps"`)
	if i < 0 {
		t.Fatal("the Developers page draws no Connected apps card")
	}
	i = strings.LastIndex(page[:i], "<section")
	j := strings.Index(page[i:], "</section>")
	if j < 0 {
		t.Fatal("the card's section does not close")
	}
	return page[i : i+j]
}

// TestConnectedApps_TheOwnerListsAndDisconnects (ADR 0047 §12, §17; the 2026-11-15 correction): the
// Developers page itself draws the card -- under the integration keys and before the webhook -- with
// the app, its scopes in words, the approver and "Never" for an unused grant, and the Disconnect form;
// the card's old address answers 303 to the card's fragment and no link to it is left. Disconnect --
// a bound POST -- ends the grant ('owner', the membership), writes oauth.grant_revoked with the owner as
// actor, and 303s back to the card with its done sentence inside the card; the app's token then
// resolves to nothing. A manager gets 403 on the section, the old address and the press, and nothing
// ends; another business's grant id is "not connected" and nothing ends; a disabled app is "Turned off
// by Taptime", the operator's closing raises the banner; with no app left the card says so.
//
// MUTANT (measured): the card's call removed from the Developers page -> red at the first check.
func TestConnectedApps_TheOwnerListsAndDisconnects(t *testing.T) {
	f := newConsentFixture(t)
	grant, tok := f.connect(f.client, oauth.ScopeTimesheetsRead, oauth.ScopeEmployeesWrite)
	grantB, _ := f.connect(f.clientB, oauth.ScopeEmployeesRead)
	b := f.browser()
	rec := b.do(http.MethodGet, apiKeysHref, nil)
	page := htmlOf(t, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("the Developers page: %d", rec.Code)
	}
	card := appsCardOf(t, page)
	for _, want := range []string{"Connected apps", "Malta Calculator", "Another App", oauth.ScopeLabel(oauth.ScopeTimesheetsRead),
		oauth.ScopeLabel(oauth.ScopeEmployeesWrite), "by Consent Admin", "Last used Never", ">Connected<",
		`action="` + appDisconnectHref, `name="grant_id" value="` + grant.String() + `"`, `name="grant_id" value="` + grantB.String() + `"`} {
		if !strings.Contains(card, want) {
			t.Errorf("the card lacks %q", want)
		}
	}
	keysAt, cardAt, hookAt := strings.Index(page, `id="api-keys-title"`), strings.Index(page, `id="connected-apps-title"`), strings.Index(page, `id="webhook-title"`)
	if keysAt < 0 || hookAt < 0 || !(keysAt < cardAt && cardAt < hookAt) {
		t.Errorf("the card is not between the keys and the webhook: keys %d, card %d, webhook %d", keysAt, cardAt, hookAt)
	}
	if strings.Contains(page, `href="`+connectedAppsHref+`"`) {
		t.Error("the page still links to the card's old page")
	}
	if strings.Contains(page, "<script") {
		t.Error("the page loads a script")
	}
	// The old address: to the card.
	rec = b.do(http.MethodGet, connectedAppsHref, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != apiKeysHref+"#connected-apps" {
		t.Fatalf("the old address: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// A manager: 403 on the section, the old address and the press; nothing ends.
	f.set(func(f *consentFixture) { f.as.AdminUserID, f.as.Role = f.manager, "manager" })
	for _, at := range []string{apiKeysHref, connectedAppsHref} {
		if rec := b.do(http.MethodGet, at, nil); rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Malta Calculator") {
			t.Fatalf("a manager at %s: %d", at, rec.Code)
		}
	}
	if rec := b.do(http.MethodPost, appDisconnectHref, url.Values{"grant_id": {grant.String()}}); rec.Code != http.StatusForbidden {
		t.Fatalf("a manager's Disconnect: %d", rec.Code)
	}
	f.set(func(f *consentFixture) { f.as.AdminUserID, f.as.Role = f.ownerID, "owner" })
	if n := f.count(`SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_at IS NULL`, grant); n != 1 {
		t.Fatal("a refused Disconnect ended the grant")
	}
	// Another business's grant id finds nothing here; the card says so.
	rec = b.do(http.MethodPost, appDisconnectHref, url.Values{"grant_id": {uuid.NewString()}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != apiKeysHref+"?app=unknown-app#connected-apps" {
		t.Fatalf("an unknown grant id: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if card := appsCardOf(t, htmlOf(t, b.do(http.MethodGet, apiKeysHref+"?app=unknown-app", nil))); !strings.Contains(card, "That app is not connected to this business any more") {
		t.Fatal("the card does not say the app is not connected")
	}
	// The owner's Disconnect.
	rec = b.do(http.MethodPost, appDisconnectHref, url.Values{"grant_id": {grant.String()}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != apiKeysHref+"?app=disconnected#connected-apps" {
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
	page = htmlOf(t, b.do(http.MethodGet, apiKeysHref+"?app=disconnected", nil))
	card = appsCardOf(t, page)
	if strings.Contains(card, "Malta Calculator") || !strings.Contains(card, "The app is disconnected") || !strings.Contains(card, "Another App") {
		t.Fatal("the card after Disconnect")
	}
	// The remaining app turned off; the operator closes the business's API.
	f.exec(`UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, f.clientB)
	f.exec(`UPDATE tenants SET api_code = 'CAX', api_enabled_at = NULL WHERE id = $1`, f.tenantID)
	card = appsCardOf(t, htmlOf(t, b.do(http.MethodGet, apiKeysHref, nil)))
	if !strings.Contains(card, "Turned off by Taptime") || !strings.Contains(card, "Integrations are turned off for this business by Taptime") ||
		!strings.Contains(card, "Disconnect") {
		t.Fatal("the turned-off states are not drawn (with Disconnect still offered)")
	}
	// The last one disconnected: the card's empty state.
	if rec := b.do(http.MethodPost, appDisconnectHref, url.Values{"grant_id": {grantB.String()}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("the second Disconnect: %d", rec.Code)
	}
	card = appsCardOf(t, htmlOf(t, b.do(http.MethodGet, apiKeysHref, nil)))
	if !strings.Contains(card, "No app is connected to this business.") || strings.Contains(card, `name="grant_id"`) {
		t.Fatal("the card's empty state")
	}
	if strings.Contains(f.logs.String(), tok.Access.RevealInTokenResponse()) || strings.Contains(f.logs.String(), tok.Refresh.RevealInTokenResponse()) {
		t.Fatal("a token reached the log")
	}
}
