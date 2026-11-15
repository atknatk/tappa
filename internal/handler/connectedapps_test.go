package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/oauth"
)

// unreadableApps is an authorization server whose connected apps cannot be read; nothing else of it
// is asked on the Developers page.
type unreadableApps struct{ panelOAuth }

func (unreadableApps) ListConnectedApps(context.Context, uuid.UUID, uuid.UUID) (oauth.ConnectedApps, error) {
	return oauth.ConnectedApps{}, errors.New("the database is gone")
}

// developersPage renders the Developers section as an owner, on a panel built with opts.
func developersPage(t *testing.T, opts ...AdminAuthOption) (int, string) {
	t.Helper()
	tenantID := uuid.New()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: uuid.New(), TenantID: tenantID, AdminUserID: uuid.New(), Role: "owner", FullName: "Owner"}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(),
		newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), discardLogger(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	rec := b.do(http.MethodGet, apiKeysHref, nil)
	return rec.Code, rec.Body.String()
}

// TestConnectedApps_AnUnreadableListLeavesTheRestOfTheSection (the 2026-11-15 correction): the card
// has its own read. When the list cannot be read the Developers page is still 200 with the keys and
// the webhook, and the card says it could not read the apps (no list, no empty-state sentence -- that
// would claim none is connected). A panel without an authorization server draws no card at all.
//
// MUTANT (measured): the card's read failure returned as the section's error -> the 500 problem page;
// red.
func TestConnectedApps_AnUnreadableListLeavesTheRestOfTheSection(t *testing.T) {
	code, page := developersPage(t, IntegrationKeys(stubAPIKeys{}), OAuthConsent(unreadableApps{}))
	if code != http.StatusOK {
		t.Fatalf("the Developers page with an unreadable app list: %d, want 200", code)
	}
	for _, want := range []string{`id="api-keys-title"`, `id="webhook-title"`, `id="connected-apps"`, "The connected apps could not be read just now"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(page, "No app is connected to this business") {
		t.Error("an unread list is drawn as an empty one")
	}
	code, page = developersPage(t, IntegrationKeys(stubAPIKeys{}))
	if code != http.StatusOK || strings.Contains(page, `id="connected-apps"`) || strings.Contains(page, "Connected apps") {
		t.Fatalf("a panel without an authorization server: %d, and it draws the card", code)
	}
}
