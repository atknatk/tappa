package oauth

// access_db_test.go -- the /v1 half of the authorization server (issue #37 phase 3; ADR 0047 §8.1,
// §11.4, §12) against REAL Postgres: an access token's resolution decided on every request (no
// cache), the grant's last use, the owner's "Connected apps" list and Disconnect, and the 2nd phase's
// devolved finding on the tolerance's log line.

import (
	"context"
	"strings"
	"testing"
)

// access resolves an access token as the /v1 chain does: the shape, then the resolver.
func (w *serverWorld) access(v Value) (Access, error) {
	w.t.Helper()
	c, err := w.srv.CheckAccess(v)
	if err != nil {
		return Access{}, err
	}
	return w.srv.ResolveAccess(context.Background(), c)
}

// TestOAuthAccessDB_EveryRefusalIsDecidedOnTheNextRequest (ADR 0047 §7.3, §8.1 step 4, §11.4, §17):
// a live token resolves to its grant's tenant with ITS OWN scopes; then, each on the very next
// resolution -- an expired token, a revoked token, a token of a Disconnected grant, a token of a
// disabled app: ErrInvalidToken; the operator closing the business's API: ErrAPIClosed, and reopening
// it brings the same token back; a never-opened API (both K-22 columns NULL) is no refusal (OA-1); a
// refresh token and a junk value never pass the shape.
func TestOAuthAccessDB_EveryRefusalIsDecidedOnTheNextRequest(t *testing.T) {
	w := newServerWorld(t)
	ctx := context.Background()
	tok := w.connect(ScopeEmployeesWrite, ScopeTimesheetsRead)
	a, err := w.access(tok.Access)
	if err != nil {
		t.Fatalf("a live token: %v", err)
	}
	if a.TenantID != w.tenant || a.ClientID != w.client.ID || ScopeString(a.Scopes) != "employees:write timesheets:read" {
		t.Fatalf("the resolution: %+v", a)
	}
	if n := w.count(`SELECT count(*) FROM tenants WHERE id = $1 AND api_enabled_at IS NULL AND api_code IS NULL`, w.tenant); n != 1 {
		t.Fatal("CONTROL: the fixture business has its API opened -- OA-1 is not what this measures")
	}
	if _, err := w.srv.CheckAccess(tok.Refresh); !is(err, ErrNotAnAccessToken) {
		t.Fatalf("a refresh token as a bearer: %v", err)
	}
	// The operator closes the API (§11.4: api_code set, api_enabled_at NULL) -> 403; reopened -> back.
	w.exec(`UPDATE tenants SET api_code = 'OAX', api_enabled_at = now() WHERE id = $1`, w.tenant)
	if _, err := w.access(tok.Access); err != nil {
		t.Fatalf("an OPEN API: %v", err)
	}
	w.exec(`UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, w.tenant)
	if _, err := w.access(tok.Access); !is(err, ErrAPIClosed) {
		t.Fatalf("the operator closed the API: %v", err)
	}
	w.exec(`UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, w.tenant)
	if _, err := w.access(tok.Access); err != nil {
		t.Fatalf("reopened: %v", err)
	}
	// An expired access token (both instants written back by the owner; the lifetime CHECK holds).
	expired, _ := Mint(KindAccess)
	h, _ := w.srv.hasher.Hash(KindAccess, expired)
	w.exec(`INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, scopes, created_at, expires_at)
	        VALUES ($1, $2, 'access', $3, '{timesheets:read}', now() - interval '2 hours', now() - interval '1 hour')`, w.tenant, a.GrantID, h.reveal())
	if _, err := w.access(expired); !is(err, ErrInvalidToken) {
		t.Fatalf("an expired token: %v", err)
	}
	// The app revokes the access token alone (§6): that token dies, a fresh one of the grant lives.
	if err := w.srv.Revoke(ctx, w.client, tok.Access); err != nil {
		t.Fatal(err)
	}
	if _, err := w.access(tok.Access); !is(err, ErrInvalidToken) {
		t.Fatalf("a revoked token: %v", err)
	}
	t1, err := w.srv.Refresh(ctx, ctx, w.client, tok.Refresh, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.access(t1.Access); err != nil {
		t.Fatalf("the grant's next token: %v", err)
	}
	// The app is disabled by the operator: every token of it is refused, the grant row untouched.
	w.exec(`UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, w.client.ID)
	if _, err := w.access(t1.Access); !is(err, ErrInvalidToken) {
		t.Fatalf("a disabled app's token: %v", err)
	}
	if w.liveGrants() != 1 {
		t.Fatal("disabling the app revoked the grant (a tenant write -- §11.4 says it must not)")
	}
	if _, err := w.access(ParseValue(strings.Repeat("x", 54))); !is(err, ErrNotAnAccessToken) {
		t.Fatalf("junk: %v", err)
	}
}

// TestOAuthAccessDB_DisconnectEndsEveryTokenAndWritesTheTrail (ADR 0047 §12, §17): the owner's list
// names the app and the approver; Disconnect by the owner ends the grant ('owner', revoked_by the
// membership), the very next resolution of its token is ErrInvalidToken, and oauth.grant_revoked is
// written with the membership as actor. A manager can neither list nor disconnect (ErrNotOwner);
// another business's grant id finds nothing (ErrUnknownGrant) and ends nothing.
func TestOAuthAccessDB_DisconnectEndsEveryTokenAndWritesTheTrail(t *testing.T) {
	w := newServerWorld(t)
	ctx := context.Background()
	tok := w.connect()
	a, err := w.access(tok.Access)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := w.srv.TouchGrant(ctx, w.tenant, a.GrantID); err != nil || !ok {
		t.Fatalf("the first touch: %v %v", ok, err)
	}
	if ok, err := w.srv.TouchGrant(ctx, w.tenant, a.GrantID); err != nil || ok {
		t.Fatalf("a second touch within the minute wrote (%v %v)", ok, err)
	}
	list, err := w.srv.ListConnectedApps(ctx, w.tenant, w.admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Apps) != 1 || list.Apps[0].GrantID != a.GrantID || list.Apps[0].AppName != "OAuth Server Fixture App" ||
		list.Apps[0].ApprovedBy != "OAuth Admin" || list.Apps[0].LastUsedAt == nil || list.Apps[0].Expired || list.IntegrationsOff {
		t.Fatalf("the list: %+v", list)
	}
	if _, err := w.srv.ListConnectedApps(ctx, w.tenant, w.manager); !is(err, ErrNotOwner) {
		t.Fatalf("a manager's list: %v", err)
	}
	if err := w.srv.Disconnect(ctx, w.tenant, w.manager, a.GrantID); !is(err, ErrNotOwner) {
		t.Fatalf("a manager's Disconnect: %v", err)
	}
	other := newServerWorld(t)
	if err := other.srv.Disconnect(ctx, other.tenant, other.admin, a.GrantID); !is(err, ErrUnknownGrant) {
		t.Fatalf("another business's owner, this grant's id: %v", err)
	}
	if w.liveGrants() != 1 {
		t.Fatal("a refused Disconnect ended the grant")
	}
	if err := w.srv.Disconnect(ctx, w.tenant, w.admin, a.GrantID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.access(tok.Access); !is(err, ErrInvalidToken) {
		t.Fatalf("the token after Disconnect: %v", err)
	}
	if n := w.count(`SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_reason = 'owner' AND revoked_by = $2`, a.GrantID, w.admin); n != 1 {
		t.Fatal("the grant is not ended by the owner's membership")
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND actor_id = $3 AND target = $4
	                 AND detail->>'reason' = 'owner' AND detail->>'client_id' = $5`, w.tenant, ActionGrantRevoked, w.admin, a.GrantID.String(), ClientIDOf(w.client.ID)); n != 1 {
		t.Fatalf("%d grant_revoked rows naming the owner", n)
	}
	if err := w.srv.Disconnect(ctx, w.tenant, w.admin, a.GrantID); !is(err, ErrUnknownGrant) {
		t.Fatalf("a second Disconnect: %v", err)
	}
	if list, _ := w.srv.ListConnectedApps(ctx, w.tenant, w.admin); len(list.Apps) != 0 {
		t.Fatalf("the list after Disconnect: %+v", list.Apps)
	}
	if _, err := w.srv.Refresh(ctx, ctx, w.client, tok.Refresh, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("a refresh after Disconnect: %v", err)
	}
}

// TestOAuthAccessDB_TheListSaysTurnedOffAndExpired (§11.4, the "Uygulama notları"): a disabled app's
// grant is listed with AppDisabled; a grant whose refresh tokens are all gone is Expired; the
// operator's closing raises IntegrationsOff.
func TestOAuthAccessDB_TheListSaysTurnedOffAndExpired(t *testing.T) {
	w := newServerWorld(t)
	ctx := context.Background()
	tok := w.connect()
	a, _ := w.access(tok.Access)
	w.exec(`UPDATE oauth_tokens SET revoked_at = clock_timestamp() WHERE grant_id = $1 AND kind = 'refresh'`, a.GrantID)
	w.exec(`UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, w.client.ID)
	w.exec(`UPDATE tenants SET api_code = 'OAY' WHERE id = $1`, w.tenant)
	list, err := w.srv.ListConnectedApps(ctx, w.tenant, w.admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Apps) != 1 || !list.Apps[0].AppDisabled || !list.Apps[0].Expired || !list.IntegrationsOff {
		t.Fatalf("the list: %+v", list)
	}
}

// TestOAuthServerDB_AWideScopeInTheToleranceRollsTheRevocationBack is the 2nd phase's devolved finding
// (DÜŞÜK): R0 rotated once, then presented again within the 30 seconds asking for a scope WIDER than
// the grant's -> invalid_scope, and the tolerance's revocation of the other live tokens is ROLLED BACK
// (the first rotation's pair still lives) and the log carries no "revoked N" line for it.
//
// MUTANT (measured, phase 3 report): the log line moved back into the tolerance branch -> the
// "within the tolerance" line appears for the rolled-back request and this is red.
func TestOAuthServerDB_AWideScopeInTheToleranceRollsTheRevocationBack(t *testing.T) {
	w := newServerWorld(t)
	ctx := context.Background()
	t0 := w.connect(ScopeTimesheetsRead)
	t1, err := w.srv.Refresh(ctx, ctx, w.client, t0.Refresh, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.logs.Reset()
	if _, err := w.srv.Refresh(ctx, ctx, w.client, t0.Refresh, []string{ScopeEmployeesWrite}); !is(err, ErrInvalidScope) {
		t.Fatalf("a wide scope within the tolerance: %v", err)
	}
	if w.resolve(KindRefresh, t1.Refresh).TokenRevokedAt != nil || w.resolve(KindAccess, t1.Access).TokenRevokedAt != nil {
		t.Fatal("the refused request's revocation was committed")
	}
	if strings.Contains(w.logs.String(), "within the tolerance") {
		t.Fatalf("the log records a revocation that was rolled back: %s", w.logs.String())
	}
	// CONTROL: the same presentation without the scope is the tolerance, and its line is written.
	if _, err := w.srv.Refresh(ctx, ctx, w.client, t0.Refresh, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.logs.String(), "within the tolerance") || w.resolve(KindRefresh, t1.Refresh).TokenRevokedAt == nil {
		t.Fatalf("CONTROL: the committed tolerance: %s", w.logs.String())
	}
}
