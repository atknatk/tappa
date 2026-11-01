package apikey_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
)

// TestApiKeyAuth_ADisabledCreatorsKeyKeepsWorking pins T174's decision (user, 2026-11-01; ADR 0028's
// note of that day): a key is the BUSINESS's, not its creator's. A second owner creates a key, an
// owner disables that membership through the panel's own path (adminauth.Memberships, which also
// revokes the membership's sessions), and the key still authenticates -- the integration does not
// break silently. The list says who created it and that the membership is disabled; the remaining
// owner revokes it as before, and from then on it is refused. CONTROL: the active owner's key reads
// CreatorActive.
//
// MUTANTS (measured): a "creator must be active" check in Authenticator.Resolve -> the disabled
// creator's key is refused, red; ListAPIKeys printing another admin's name as the creator's -> red.
func TestApiKeyAuth_ADisabledCreatorsKeyKeepsWorking(t *testing.T) {
	d := appDB(t)
	ctx := context.Background()
	s := newService(t, d, apikey.LivePrefix)
	b := newBusiness(t, d)
	setAPI(t, b.tenant, true)

	trail, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	members, err := adminauth.NewMemberships(d, trail)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's disabled owner ("API-3 Second Owner" -- every fixture admin has a name of its own)
	// is enabled, creates a key, and is disabled again.
	creator := b.disabledOwner
	if _, err := members.Change(ctx, b.tenant, b.owner, creator, adminauth.MemberEnable); err != nil {
		t.Fatalf("enable the creator: %v", err)
	}
	theirs, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: creator, Name: "Payroll"})
	if err != nil {
		t.Fatalf("create by the second owner: %v", err)
	}
	mine, err := s.keys.Create(ctx, apikey.CreateCommand{TenantID: b.tenant, ActorID: b.owner, Name: "ERP"})
	if err != nil {
		t.Fatalf("create by the owner: %v", err)
	}
	header := "Bearer " + theirs.Key.RevealToTheOwnerOnce()
	if _, err := members.Change(ctx, b.tenant, b.owner, creator, adminauth.MemberDisable); err != nil {
		t.Fatalf("disable the creator: %v", err)
	}

	p, err := s.authenticate(ctx, header)
	if err != nil {
		t.Fatalf("the key of a disabled creator: %v, want it to keep working (T174)", err)
	}
	if p.TenantID != b.tenant || p.KeyID != theirs.ID {
		t.Fatalf("principal %v/%v, want the key's tenant and id", p.TenantID, p.KeyID)
	}

	l, err := s.keys.List(ctx, b.tenant, b.owner)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := map[string]apikey.Key{}
	for _, k := range l.Keys {
		seen[k.ID.String()] = k
	}
	if k := seen[theirs.ID.String()]; k.CreatedBy != creator || k.CreatorActive || k.CreatorName != "API-3 Second Owner" {
		t.Errorf("the disabled creator's key lists creator %v active %v name %q; want the creator, inactive, by name", k.CreatedBy, k.CreatorActive, k.CreatorName)
	}
	if k := seen[mine.ID.String()]; k.CreatedBy != b.owner || !k.CreatorActive || k.CreatorName != "API-3 Owner" {
		t.Errorf("CONTROL: the active owner's key lists creator %v active %v name %q", k.CreatedBy, k.CreatorActive, k.CreatorName)
	}

	if _, err := s.keys.Revoke(ctx, apikey.RevokeCommand{TenantID: b.tenant, ActorID: b.owner, KeyID: theirs.ID}); err != nil {
		t.Fatalf("the remaining owner revokes the key: %v", err)
	}
	if _, err := s.authenticate(ctx, header); !errors.Is(err, apikey.ErrRevoked) {
		t.Fatalf("after the revocation: %v, want ErrRevoked", err)
	}
}
