package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/webhook/endpoints"
)

// stubWebhook is a webhook service that answers nothing in particular.
type stubWebhook struct{}

func (stubWebhook) List(context.Context, uuid.UUID, uuid.UUID) (endpoints.Listing, error) {
	return endpoints.Listing{}, nil
}
func (stubWebhook) Create(context.Context, endpoints.CreateCommand) (endpoints.Created, error) {
	return endpoints.Created{}, nil
}
func (stubWebhook) Rotate(context.Context, uuid.UUID, uuid.UUID) (endpoints.Rotated, error) {
	return endpoints.Rotated{}, nil
}
func (stubWebhook) Readdress(context.Context, uuid.UUID, uuid.UUID, string) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (stubWebhook) Disable(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (stubWebhook) Enable(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (stubWebhook) Ping(context.Context, uuid.UUID, uuid.UUID) (endpoints.Pinged, error) {
	return endpoints.Pinged{}, nil
}

// TestNewAdminAuth_TheWebhookServiceMatchesTheConfiguration: a configuration that holds the webhook KEK
// (every one outside dev) builds no panel without the webhook service -- that panel would take away
// the owner's switch-off (K-26); with the service it builds; without the KEK it builds either way.
func TestNewAdminAuth_TheWebhookServiceMatchesTheConfiguration(t *testing.T) {
	build := func(withKEK bool, opts ...AdminAuthOption) error {
		cfg := adminTestConfig()
		if withKEK {
			cfg.WebhookKEK = []byte(strings.Repeat("w", 32))
		}
		_, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
			&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(),
			newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, discardLogger(), opts...)
		return err
	}
	if err := build(true); err == nil || !strings.Contains(err.Error(), "IntegrationWebhook") {
		t.Fatalf("the KEK configured and no webhook service: %v", err)
	}
	if err := build(true, IntegrationWebhook(stubWebhook{})); err != nil {
		t.Fatalf("the KEK configured and the service given: %v", err)
	}
	for _, opts := range [][]AdminAuthOption{nil, {IntegrationWebhook(stubWebhook{})}} {
		if err := build(false, opts...); err != nil {
			t.Fatalf("no KEK configured: %v", err)
		}
	}
}

// TestWebhookPanel_EveryClassHasASentence: each of the six classes and success has its own sentence;
// none of them names a host, an address or Go's words.
func TestWebhookPanel_EveryClassHasASentence(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range append([]egress.Class{""}, egress.Classes...) {
		s, ok := webhookPingSentence(c, 200)
		if s == "" || seen[s] || (c == "") != ok {
			t.Errorf("class %q: %q (delivered %v)", c, s, ok)
		}
		seen[s] = true
	}
	if s, _ := webhookPingSentence(egress.ClassHTTP4xx, 301); !strings.Contains(s, "redirect") {
		t.Errorf("a 3xx: %q", s)
	}
}
