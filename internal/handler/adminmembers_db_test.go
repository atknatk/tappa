package handler

// adminmembers_db_test.go -- M14-16 end to end: the real panel, the real adminauth.Memberships over the
// real pool. Flag off, the four presses are paths the panel never had; flag on, the owner's Managers
// page lists the business's people, a disable signs the person out of this business, an ownership
// transfer is two presses, and the last owner cannot step down.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// keysNote is the sentence beside every press that disables or demotes an owner (backlog T174).
const keysNote = "does not stop the API keys they created"

// membersHarness is a flag-on panel with its owner signed in.
func membersHarness(t *testing.T, label string) *panelHarness {
	t.Helper()
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: label + " Ltd", inviteSender: &fakeMailSender{}})
	p.signInTo(t, p.tenantID)
	return p
}

// addMember inserts a membership of tenant with role, as the application does, and one live session.
func (p *panelHarness) addMember(t *testing.T, tenant uuid.UUID, role, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	if err := p.data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, $3, $4, $5, $6)`,
			id, tenant, name, "m14-16-"+uuid.NewString()+"@identity.example", ownerDigest, role); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`, tenant, id, hex.EncodeToString(raw))
		return e
	}); err != nil {
		t.Fatalf("insert a member: %v", err)
	}
	return id
}

func (p *panelHarness) memberState(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var role, status string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT role, status FROM admin_users WHERE tenant_id = $1 AND id = $2`, p.tenantID, id).Scan(&role, &status)
	}); err != nil {
		t.Fatalf("read a member: %v", err)
	}
	return role + "/" + status
}

func (p *panelHarness) press(t *testing.T, path string, member uuid.UUID, extra ...string) (int, string) {
	t.Helper()
	form := url.Values{"member_id": {member.String()}}
	for i := 0; i+1 < len(extra); i += 2 {
		form.Set(extra[i], extra[i+1])
	}
	res, _ := p.post(t, path, form)
	return res.StatusCode, res.Header.Get("Location")
}

// TestAdminMembersDB_TheSurfaceIsAPathThePanelNeverHadWithTheFlagOff (ADR 0031 §8): with
// TAPPA_ADMIN_IDENTITY off the four presses answer what a path the panel never had answers.
func TestAdminMembersDB_TheSurfaceIsAPathThePanelNeverHadWithTheFlagOff(t *testing.T) {
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: false})
	p.signInTo(t, p.tenantID)
	unknown, unknownBody := p.post(t, "/admin/login/zz-never-a-route", url.Values{})
	for _, path := range []string{membersDisableHref, membersEnableHref, membersMakeOwnerHref, membersMakeManagerHref} {
		res, body := p.post(t, path, url.Values{"member_id": {p.adminID.String()}})
		if res.StatusCode != http.StatusNotFound || res.StatusCode != unknown.StatusCode || body != unknownBody {
			t.Errorf("POST %s with the flag off: %d, body equal to an unknown path's %v", path, res.StatusCode, body == unknownBody)
		}
	}
}

// TestAdminMembersDB_AnOwnerDisablesAManagerAndOnlyThatPersonIsSignedOut: the owner's page lists the
// manager with a Disable press; the press answers the done redirect, the manager's session in this
// business ends, the owner stays signed in, one trail row; the page then offers Enable, which works.
// A press naming another business's membership changes nothing there.
func TestAdminMembersDB_AnOwnerDisablesAManagerAndOnlyThatPersonIsSignedOut(t *testing.T) {
	p := membersHarness(t, "M14-16 Disable")
	mgr := p.addMember(t, p.tenantID, "manager", "Mia Manager")
	_, page := p.get(t, managersHref)
	if !strings.Contains(page, "People who run this business") || !strings.Contains(page, "Mia Manager") || !strings.Contains(page, membersDisableHref) {
		t.Fatalf("the managers page does not list the manager with a Disable press")
	}
	if code, loc := p.press(t, membersDisableHref, mgr); code != http.StatusSeeOther || loc != managersHref+"?done=disabled" {
		t.Fatalf("disable: %d %q", code, loc)
	}
	if got := p.memberState(t, mgr); got != "manager/disabled" {
		t.Fatalf("the manager is %s", got)
	}
	if n := p.liveSessionsOf(t, p.tenantID, mgr); n != 0 {
		t.Errorf("the disabled manager keeps %d live session(s)", n)
	}
	if !p.signedIn(t) {
		t.Error("the owner was signed out")
	}
	var trail int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'admin.membership.disabled' AND target = $2 AND actor_id = $3`,
			p.tenantID, mgr.String(), p.adminID).Scan(&trail)
	}); err != nil || trail != 1 {
		t.Errorf("the trail holds %d admin.membership.disabled row(s) for the press (%v), want 1", trail, err)
	}
	_, page = p.get(t, managersHref+"?done=disabled")
	if !strings.Contains(page, "can no longer sign in to this business") || !strings.Contains(page, membersEnableHref) {
		t.Error("after the disable the page does not say so or offer Enable")
	}
	if code, loc := p.press(t, membersEnableHref, mgr); code != http.StatusSeeOther || loc != managersHref+"?done=enabled" {
		t.Fatalf("enable: %d %q", code, loc)
	}

	elsewhere, _ := p.addBusiness(t)
	theirs := p.addMember(t, elsewhere, "manager", "Not Ours")
	if code, loc := p.press(t, membersDisableHref, theirs); code != http.StatusSeeOther || loc != managersHref+"?problem=unknown" {
		t.Fatalf("another business's membership: %d %q", code, loc)
	}
	if n := p.liveSessionsOf(t, elsewhere, theirs); n != 1 {
		t.Errorf("the other business's person lost their session (%d live)", n)
	}
}

// TestAdminMembersDB_OwnershipIsHandedOverInTwoPresses: stepping down needs the tick, then is refused
// while the presser is the only owner; make the manager an owner (press one), step down (press two)
// -- the presser lands on the account page as a manager, and their next press is refused.
func TestAdminMembersDB_OwnershipIsHandedOverInTwoPresses(t *testing.T) {
	p := membersHarness(t, "M14-16 Transfer")
	heir := p.addMember(t, p.tenantID, "manager", "Hana Heir")
	if code, loc := p.press(t, membersMakeManagerHref, p.adminID); loc != managersHref+"?problem=confirm" {
		t.Fatalf("a step-down without the tick: %d %q", code, loc)
	}
	if code, loc := p.press(t, membersMakeManagerHref, p.adminID, "confirm", "yes"); loc != managersHref+"?problem=last-owner" {
		t.Fatalf("the only owner stepping down: %d %q", code, loc)
	}
	if _, page := p.get(t, managersHref); !strings.Contains(page, "You are the only owner") {
		t.Error("the page does not tell the only owner how a hand-over goes")
	}
	if _, page := p.get(t, managersHref); strings.Contains(page, keysNote) {
		t.Error("the API-key sentence is drawn while no press here disables or demotes an owner")
	}
	if code, loc := p.press(t, membersMakeOwnerHref, heir); loc != managersHref+"?problem=confirm-owner" {
		t.Fatalf("press one without the tick: %d %q", code, loc)
	}
	if code, loc := p.press(t, membersMakeOwnerHref, heir, "confirm", "yes"); loc != managersHref+"?done=made-owner" {
		t.Fatalf("press one: %d %q", code, loc)
	}
	// Two owners now: the heir's row (Disable, Make manager) and the presser's own (Step down) each
	// carry the sentence that the owner's API keys keep working, with the API Keys link.
	if _, page := p.get(t, managersHref); strings.Count(page, keysNote) != 2 || !strings.Contains(page, `href="`+apiKeysHref+`"`) {
		t.Errorf("the API-key sentence appears %d time(s), want 2, with a link to %s", strings.Count(page, keysNote), apiKeysHref)
	}
	if code, loc := p.press(t, membersMakeManagerHref, p.adminID, "confirm", "yes"); code != http.StatusSeeOther || loc != accountReturn("stepped-down", "") {
		t.Fatalf("press two: %d %q", code, loc)
	}
	if got := p.memberState(t, p.adminID) + " " + p.memberState(t, heir); got != "manager/active owner/active" {
		t.Fatalf("after the hand-over: %s", got)
	}
	// The former owner presses no more: the service's belt (the harness's policy double allows anybody,
	// so this is the actor-is-an-active-owner check under the owners' lock answering).
	if code, _ := p.press(t, membersDisableHref, heir); code != http.StatusForbidden {
		t.Errorf("the former owner disabling the new one answered %d, want 403", code)
	}
	if got := p.memberState(t, heir); got != "owner/active" {
		t.Errorf("the new owner is %s", got)
	}
	if _, page := p.get(t, accountReturn("stepped-down", "")); !strings.Contains(page, "You stepped down") {
		t.Error("the account page does not say the presser stepped down")
	}
}
