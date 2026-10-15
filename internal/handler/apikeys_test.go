package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

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
