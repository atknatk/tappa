package api

// oauthchain_test.go -- the fakes behind the OAuth access token's half of the chain (issue #37, ADR
// 0047 §8.1) for the database-free rig: the authorization server's three calls, counted. The shape
// check is the shipped oauth.Value.Is -- the fake does not decide what an access token looks like.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/oauth"
)

// fakeGrants is Grants: CheckAccess is the real shape (no database -- and counted, so a test can say
// "zero lookups"), ResolveAccess answers what answer says and counts, TouchGrant counts.
type fakeGrants struct {
	mu       sync.Mutex
	resolves int
	touches  int
	answer   func() (oauth.Access, error)
}

func (f *fakeGrants) CheckAccess(v oauth.Value) (oauth.CheckedAccess, error) {
	if !v.Is(oauth.KindAccess) {
		return oauth.CheckedAccess{}, oauth.ErrNotAnAccessToken
	}
	return oauth.CheckedAccess{}, nil
}

func (f *fakeGrants) ResolveAccess(context.Context, oauth.CheckedAccess) (oauth.Access, error) {
	f.mu.Lock()
	f.resolves++
	answer := f.answer
	f.mu.Unlock()
	return answer()
}

func (f *fakeGrants) TouchGrant(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	f.mu.Lock()
	f.touches++
	f.mu.Unlock()
	return true, nil
}

func (f *fakeGrants) set(answer func() (oauth.Access, error)) {
	f.mu.Lock()
	f.answer = answer
	f.mu.Unlock()
}

func (f *fakeGrants) counts() (resolves, touches int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resolves, f.touches
}

// accessToken is a well-formed access token of this build, built at run time (redline R7d reads a
// token-shaped literal in source as a leak).
func accessToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return oauth.Prefix(oauth.KindAccess) + base64.RawURLEncoding.EncodeToString(b)
}

// refuseBusiness is a Business for tests that read no business card.
type refuseBusiness struct{}

func (refuseBusiness) APIBusiness(context.Context, uuid.UUID) (tenant.APIBusiness, error) {
	return tenant.APIBusiness{}, errors.New("this rig reads no business")
}

// grantOf is a live access token's resolution: the scopes given (canonical), fresh ids.
func grantOf(scopes ...string) func() (oauth.Access, error) {
	a := oauth.Access{TenantID: uuid.New(), GrantID: uuid.New(), ClientID: uuid.New(), TokenID: uuid.New(), Scopes: oauth.Canonical(scopes)}
	return func() (oauth.Access, error) { return a, nil }
}

// sandboxAccessToken is an access token of the OTHER build's shape (tt_at_test_ in production).
func sandboxAccessToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "tt_at_" + map[string]string{"live": "test", "test": "live"}[oauth.Env] + "_" + base64.RawURLEncoding.EncodeToString(b)
}

// recordingBusiness answers a business card and records the tenant it was read for.
type recordingBusiness struct {
	mu sync.Mutex
	id uuid.UUID
}

func (b *recordingBusiness) APIBusiness(_ context.Context, id uuid.UUID) (tenant.APIBusiness, error) {
	b.mu.Lock()
	b.id = id
	b.mu.Unlock()
	return tenant.APIBusiness{ID: id, Name: "Recorded Ltd"}, nil
}

func (b *recordingBusiness) tenant() uuid.UUID {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.id
}
