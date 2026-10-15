package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/apikey"
)

// stubAPIKeys is a keys service that answers nothing; the test below only asks whether one was
// given.
type stubAPIKeys struct{}

func (stubAPIKeys) List(context.Context, uuid.UUID, uuid.UUID) (apikey.Listing, error) {
	return apikey.Listing{}, nil
}
func (stubAPIKeys) Create(context.Context, apikey.CreateCommand) (apikey.Created, error) {
	return apikey.Created{}, nil
}
func (stubAPIKeys) Revoke(context.Context, apikey.RevokeCommand) (apikey.Revoked, error) {
	return apikey.Revoked{}, nil
}
func (stubAPIKeys) RevokeAll(context.Context, uuid.UUID, uuid.UUID) ([]apikey.Revoked, error) {
	return nil, nil
}

// TestNewAdminAuth_TheKeysServiceMatchesTheConfiguration (M11 API-3, second round D3): a
// configuration holding TAPPA_API_KEY_HMAC_KEY -- every production one, config.Load requires it
// -- refuses a panel built without IntegrationKeys (the owner would lose the revocation K-26
// keeps open); with the service it builds; a configuration without the key (the handler tests'
// hand-built one) builds either way.
//
// MUTANT (measured): the refusal removed -> the first case builds; red.
func TestNewAdminAuth_TheKeysServiceMatchesTheConfiguration(t *testing.T) {
	build := func(withKey bool, opts ...AdminAuthOption) error {
		cfg := adminTestConfig()
		if withKey {
			cfg.APIKeyHMACKey = []byte(strings.Repeat("a", 32))
		}
		_, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
			&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(),
			newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, discardLogger(), opts...)
		return err
	}
	if err := build(true); err == nil || !strings.Contains(err.Error(), "IntegrationKeys") {
		t.Fatalf("the key configured and no keys service: %v, want a refusal naming IntegrationKeys", err)
	}
	if err := build(true, IntegrationKeys(stubAPIKeys{})); err != nil {
		t.Fatalf("the key configured and the service given: %v", err)
	}
	for _, opts := range [][]AdminAuthOption{nil, {IntegrationKeys(stubAPIKeys{})}} {
		if err := build(false, opts...); err != nil {
			t.Fatalf("no key configured: %v", err)
		}
	}
}

// failingListKeys creates a key and then cannot list.
type failingListKeys struct {
	stubAPIKeys
	key string
}

func (f failingListKeys) Create(context.Context, apikey.CreateCommand) (apikey.Created, error) {
	return apikey.Created{Key: apikey.ParseToken(f.key), ID: uuid.New(), Name: "Payroll"}, nil
}
func (failingListKeys) List(context.Context, uuid.UUID, uuid.UUID) (apikey.Listing, error) {
	return apikey.Listing{}, errors.New("the database is gone")
}

// TestApiKeyPanel_TheKeyIsShownEvenWhenTheListCannotBeRead (second round, security note): a key
// created and committed is live, and the creation answer is the only response that can ever carry
// it -- so when the list read after it fails, the answer is still 200 with the key, a sentence
// saying the list could not be read, and no list, form or gate note (none of them would be true).
//
// MUTANT (measured): the first round's shape -- the list read's failure answering the 500
// problem page -- is red: the key is lost.
func TestApiKeyPanel_TheKeyIsShownEvenWhenTheListCannotBeRead(t *testing.T) {
	raw := apikey.LivePrefix + strings.Repeat("Q", 42) + "A"
	cfg := adminTestConfig()
	cfg.APIKeyHMACKey = []byte(strings.Repeat("a", 32))
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(), Role: "owner", FullName: "Owner"}, nil
	}}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(),
		newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, discardLogger(), IntegrationKeys(failingListKeys{key: raw}))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	rec := b.do(http.MethodPost, apiKeysHref, url.Values{"name": {"Payroll"}})
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Count(body, raw) != 1 {
		t.Fatalf("create with a failing list: %d, the key %d times; want 200 and once", rec.Code, strings.Count(body, raw))
	}
	if !strings.Contains(body, "could not be read just now") {
		t.Fatal("the answer does not say the list could not be read")
	}
	for _, never := range []string{"API access is off for this business", `name="name"`, "No key has been created", "not available on this deployment"} {
		if strings.Contains(body, never) {
			t.Errorf("the answer draws %q although the list was not read", never)
		}
	}
}
