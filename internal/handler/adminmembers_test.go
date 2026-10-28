package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// countingMemberships is a panelMemberships that records the changes it is asked for.
type countingMemberships struct {
	changes []adminauth.MembershipAct
	list    []adminauth.Member
}

func (c *countingMemberships) List(context.Context, uuid.UUID) ([]adminauth.Member, error) {
	return c.list, nil
}

func (c *countingMemberships) Change(_ context.Context, _, _, member uuid.UUID, act adminauth.MembershipAct) (adminauth.MembershipChange, error) {
	c.changes = append(c.changes, act)
	return adminauth.MembershipChange{Member: member}, nil
}

// emptyInvites is a panelAdminInvites with no invitation: the managers page draws its membership
// section (any other call panics through the nil embedded interface).
type emptyInvites struct{ panelAdminInvites }

func (emptyInvites) List(context.Context, uuid.UUID) ([]adminauth.AdminInvite, error) {
	return nil, nil
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
		slog.New(slog.DiscardHandler), AdminMemberships(ms), AdminInvitations(emptyInvites{}))
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

// TestAdminMembers_ARefusedOwnerIsNotCountedOnThePage (M14-16b): the reader is an owner, and the only
// other owner row is one the person refused (adminauth.Member.RefusedOwner). The page counts owners
// the way the service does: no "Step down" for the reader, the "only owner" notice, and the refused row
// says it cannot sign in and does not count. CONTROL: the same page with that row a real owner offers
// the step-down and no notice. MUTATION (measured): membersView counting every active owner -> red.
func TestAdminMembers_ARefusedOwnerIsNotCountedOnThePage(t *testing.T) {
	const stepDown, only, refusedNote = "Step down to manager", "You are the only owner", "Can&#39;t sign in"
	for _, refused := range []bool{true, false} {
		ms := &countingMemberships{list: []adminauth.Member{
			{ID: panelTestAdmin, FullName: "Owner", Email: "owner@members.example", Role: "owner", Status: "active"},
			{ID: uuid.New(), FullName: "Other", Email: "other@members.example", Role: "owner", Status: "active", RefusedOwner: refused},
		}}
		b := membersBrowser(t, newFakeScribe(), ms)
		rec := b.do(http.MethodGet, managersHref, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("refused=%v: GET %s = %d", refused, managersHref, rec.Code)
		}
		body := rec.Body.String()
		if got := strings.Contains(body, stepDown); got == refused {
			t.Errorf("refused=%v: the page offers the step-down: %v", refused, got)
		}
		if got := strings.Contains(body, only); got != refused {
			t.Errorf("refused=%v: the only-owner notice shown: %v", refused, got)
		}
		if got := strings.Contains(body, refusedNote) && strings.Contains(body, "do not count as one of its owners"); got != refused {
			t.Errorf("refused=%v: the refused row's words shown: %v", refused, got)
		}
	}
}
