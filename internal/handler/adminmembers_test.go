package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// countingMemberships is a panelMemberships that records the changes it is asked for.
type countingMemberships struct {
	changes []adminauth.MembershipAct
}

func (c *countingMemberships) List(context.Context, uuid.UUID) ([]adminauth.Member, error) {
	return nil, nil
}

func (c *countingMemberships) Change(_ context.Context, _, _, member uuid.UUID, act adminauth.MembershipAct) (adminauth.MembershipChange, error) {
	c.changes = append(c.changes, act)
	return adminauth.MembershipChange{Member: member}, nil
}

func membersBrowser(t *testing.T, scribe panelScribe, ms panelMemberships) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: adminRoleOwner, FullName: "Owner", TenantName: "Members Ltd"}, nil
	}}
	cfg := adminTestConfig()
	cfg.AdminIdentity = true
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), scribe,
		newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg,
		slog.New(slog.DiscardHandler), AdminMemberships(ms))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// TestAdminMembers_ThePolicyGateComesBeforeAnyChange: the four presses ask M14-15's owner gate
// (a.mayInvite -- policy:edit) first; refused, each answers 403 and the service is never asked.
// MUTATION (measured): the mayInvite check removed from membersChange -> red (four changes made).
func TestAdminMembers_ThePolicyGateComesBeforeAnyChange(t *testing.T) {
	ms := &countingMemberships{}
	b := membersBrowser(t, &fakeScribe{may: false}, ms)
	for _, path := range []string{membersDisableHref, membersEnableHref, membersMakeOwnerHref, membersMakeManagerHref} {
		rec := b.do(http.MethodPost, path, url.Values{"member_id": {uuid.NewString()}, "confirm": {"yes"}})
		if rec.Code != http.StatusForbidden {
			t.Errorf("POST %s by a refused actor answered %d, want 403", path, rec.Code)
		}
	}
	if len(ms.changes) != 0 {
		t.Fatalf("a refused actor reached the service: %v", ms.changes)
	}
}

// TestAdminMembers_SteppingDownNeedsTheTick: an owner's own make-manager press without confirm=yes is
// sent back with the "confirm" word and the service is not asked; with it, it is. Another person's
// make-manager press needs no tick. MUTATION (measured): the confirm check removed -> red.
func TestAdminMembers_SteppingDownNeedsTheTick(t *testing.T) {
	ms := &countingMemberships{}
	b := membersBrowser(t, newFakeScribe(), ms)
	rec := b.do(http.MethodPost, membersMakeManagerHref, url.Values{"member_id": {panelTestAdmin.String()}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != managersHref+"?problem=confirm" || len(ms.changes) != 0 {
		t.Fatalf("an untucked step-down: %d %q, %d change(s)", rec.Code, rec.Header().Get("Location"), len(ms.changes))
	}
	rec = b.do(http.MethodPost, membersMakeManagerHref, url.Values{"member_id": {panelTestAdmin.String()}, "confirm": {"yes"}})
	if rec.Code != http.StatusSeeOther || len(ms.changes) != 1 {
		t.Fatalf("a ticked step-down: %d, %d change(s)", rec.Code, len(ms.changes))
	}
	rec = b.do(http.MethodPost, membersMakeManagerHref, url.Values{"member_id": {uuid.NewString()}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != managersHref+"?done=made-mgr" || len(ms.changes) != 2 {
		t.Fatalf("another owner made manager: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// TestAdminMembers_MakingAnOwnerNeedsTheTick: "Make owner" hands over the whole business, so it asks
// for the same tick as a step-down; without confirm=yes the service is not asked. MUTATION
// (measured): the make-owner confirm check removed -> red.
func TestAdminMembers_MakingAnOwnerNeedsTheTick(t *testing.T) {
	ms := &countingMemberships{}
	b := membersBrowser(t, newFakeScribe(), ms)
	member := uuid.NewString()
	rec := b.do(http.MethodPost, membersMakeOwnerHref, url.Values{"member_id": {member}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != managersHref+"?problem=confirm-owner" || len(ms.changes) != 0 {
		t.Fatalf("an unticked make-owner: %d %q, %d change(s)", rec.Code, rec.Header().Get("Location"), len(ms.changes))
	}
	rec = b.do(http.MethodPost, membersMakeOwnerHref, url.Values{"member_id": {member}, "confirm": {"yes"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != managersHref+"?done=made-owner" || len(ms.changes) != 1 {
		t.Fatalf("a ticked make-owner: %d %q, %d change(s)", rec.Code, rec.Header().Get("Location"), len(ms.changes))
	}
}
