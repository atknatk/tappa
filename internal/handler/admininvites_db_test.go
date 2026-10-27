package handler

// admininvites_db_test.go -- M14-15 end to end: the real panel, the real adminauth.AdminInvites over
// the real pool, 00077's functions, a fake mail sender (the transport is EM-7B's, measured there).
//
//   - flag off: every route of the surface is a path the panel never had (404, the bytes of an
//     unknown path, per method) and the account page draws no "Managers" card;
//   - R18: an invitation to an address WITH a Taptime sign-in and one to an address WITHOUT get the
//     same status, Location and body, the same e-mail words, the same row state on the list, and --
//     on the clock -- the same time; before any acceptance the business holds no row of either;
//   - R19: opening the link (GET) three times consumes nothing; the press accepts; the link is then
//     dead.

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var inviteLinkRe = regexp.MustCompile(`/admin/invite\?t=([A-Za-z0-9_-]+)`)

// inviteOwner is a flag-on panel, its owner linked and signed in, with the fake sender.
func inviteOwner(t *testing.T, label string) (*panelHarness, *fakeMailSender, *pgx.Conn) {
	t.Helper()
	sender := &fakeMailSender{}
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: label + " Ltd", inviteSender: sender})
	owner := linkHarness(t, p)
	p.signInTo(t, p.tenantID)
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `DELETE FROM admin_invites WHERE tenant_id = $1`, p.tenantID); err != nil {
			t.Errorf("cleanup invites: %v", err)
		}
	})
	return p, sender, owner
}

// identityFor births an identity for address (taken back at cleanup).
func identityFor(t *testing.T, owner *pgx.Conn, address string) {
	t.Helper()
	var id uuid.UUID
	if err := owner.QueryRow(context.Background(), `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`, address, ownerDigest).Scan(&id); err != nil {
		t.Fatalf("birth an identity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `DELETE FROM admin_identities WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup identity: %v", err)
		}
	})
}

// cleanBirthOf takes back an identity an acceptance birthed for address.
func cleanBirthOf(t *testing.T, owner *pgx.Conn, address string) {
	t.Helper()
	t.Cleanup(func() {
		for _, sql := range []string{
			`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE identity_id IN (SELECT id FROM admin_identities WHERE email = $1)`,
			`DELETE FROM admin_identities WHERE email = $1`,
		} {
			if _, err := owner.Exec(context.Background(), sql, address); err != nil {
				t.Errorf("cleanup %s: %v", sql, err)
			}
		}
	})
}

func (p *panelHarness) inviteManager(t *testing.T, address string) (*http.Response, string) {
	t.Helper()
	return p.post(t, managersInviteHref, url.Values{"email": {address}, "full_name": {"New Manager"}})
}

// TestAdminInvitesDB_TheSurfaceIsAPathThePanelNeverHadWithTheFlagOff (R12, ADR 0031 §8): with
// TAPPA_ADMIN_IDENTITY off, the owner's page, its two writes and the invitee's page answer exactly
// what a path the panel never had answers, method by method, and the account page has no card.
func TestAdminInvitesDB_TheSurfaceIsAPathThePanelNeverHadWithTheFlagOff(t *testing.T) {
	p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: false, inviteSender: &fakeMailSender{}})
	p.signInTo(t, p.tenantID)
	unknownGet, unknownGetBody := p.get(t, "/admin/zz-never-a-route")
	unknownPost, unknownPostBody := p.post(t, "/admin/login/zz-never-a-route", url.Values{})
	for _, path := range []string{managersHref, adminInvitePath, adminInvitePath + "?t=" + strings.Repeat("A", 43)} {
		res, body := p.get(t, path)
		if res.StatusCode != http.StatusNotFound || res.StatusCode != unknownGet.StatusCode || body != unknownGetBody {
			t.Errorf("GET %s with the flag off: %d, body equal to an unknown path's %v", path, res.StatusCode, body == unknownGetBody)
		}
	}
	for _, path := range []string{managersInviteHref, managersCancelHref, adminInvitePath} {
		res, body := p.post(t, path, url.Values{"email": {"a@b.example"}, "full_name": {"x"}})
		if res.StatusCode != http.StatusNotFound || res.StatusCode != unknownPost.StatusCode || body != unknownPostBody {
			t.Errorf("POST %s with the flag off: %d, body equal to an unknown path's %v", path, res.StatusCode, body == unknownPostBody)
		}
	}
	if _, page := p.get(t, accountHref); strings.Contains(page, managersHref) || strings.Contains(page, "Open Managers") {
		t.Error("the account page draws the Managers card with the flag off")
	}
}

// TestAdminInvitesDB_TheOwnerLearnsNothingAboutTheAddress is R18 and "kabul edilmeden admin_users
// satırı 0": one address with a Taptime sign-in, one without; each gets the same 303, Location and body,
// the same e-mail words (but the link), the same "Open" on the list, and the business holds no row of
// either until an acceptance.
func TestAdminInvitesDB_TheOwnerLearnsNothingAboutTheAddress(t *testing.T) {
	p, sender, owner := inviteOwner(t, "M14-15 R18")
	known := "m14-15-known-" + uuid.NewString() + "@identity.example"
	stranger := "m14-15-new-" + uuid.NewString() + "@identity.example"
	identityFor(t, owner, known)
	type answer struct {
		code     int
		location string
		body     string
	}
	got := map[string]answer{}
	for _, address := range []string{known, stranger} {
		res, body := p.inviteManager(t, address)
		got[address] = answer{res.StatusCode, res.Header.Get("Location"), body}
	}
	if got[known] != got[stranger] || got[known].code != http.StatusSeeOther || got[known].location != managersHref+"?done=sent" {
		t.Fatalf("the two answers differ or are not the sent redirect: known %+v, new %+v", got[known], got[stranger])
	}
	if sender.count() != 2 {
		t.Fatalf("%d e-mails, want 2", sender.count())
	}
	words := func(i int) string { return inviteLinkRe.ReplaceAllString(sender.sent[i].Text, "<link>") }
	if words(0) != words(1) || sender.sent[0].Subject != sender.sent[1].Subject {
		t.Error("the two e-mails' words differ")
	}
	if sender.sent[0].To != known || sender.sent[1].To != stranger {
		t.Errorf("the e-mails went to %q and %q", sender.sent[0].To, sender.sent[1].To)
	}
	_, page := p.get(t, managersHref)
	if !strings.Contains(page, "The invitation is on its way") && !strings.Contains(page, "Invitations") {
		t.Fatalf("the managers page: %s", page)
	}
	if strings.Count(page, ">Open<") != 2 || strings.Contains(page, "/admin/invite?t=") {
		t.Errorf("the list does not show both as Open, or shows a link: %s", page)
	}
	// One trail row per invitation, the same shape for both, no address.
	sent := tenantRows(t, owner, p.tenantID, ActionAdminInviteSent)
	if len(sent) != 2 {
		t.Fatalf("%d %s rows, want 2", len(sent), ActionAdminInviteSent)
	}
	for _, row := range sent {
		if !regexp.MustCompile(`^\{"invite_id": "[0-9a-f-]{36}"\}$`).MatchString(row.detail) || strings.Contains(row.text, known) || strings.Contains(row.text, stranger) {
			t.Errorf("a sent row: %s", row.text)
		}
	}
	var n int
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM admin_users WHERE tenant_id = $1 AND email IN ($2, $3)`, p.tenantID, known, stranger).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the business holds %d rows of the invited addresses before any acceptance (%v), want 0", n, err)
	}
}

// TestAdminInvitesDB_TheAnswerTakesTheSameTimeForEveryAddress is R18 on the clock: interleaved rounds
// of an invitation to an address with a sign-in and to one without, the MINIMUM of each as the
// estimator (identity_timing_db_test.go's technique). The send path reads nothing about the address,
// so the two are one path; the gate catches a branch that does real work for one of them.
func TestAdminInvitesDB_TheAnswerTakesTheSameTimeForEveryAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: the wall-clock sample; adminauth's TestAdminInvites_IssuingTouchesNoIdentity holds the path exactly")
	}
	p, _, owner := inviteOwner(t, "M14-15 Timing")
	known := "m14-15-tk-" + uuid.NewString() + "@identity.example"
	stranger := "m14-15-tn-" + uuid.NewString() + "@identity.example"
	identityFor(t, owner, known)
	for i := 0; i < 4; i++ {
		p.inviteManager(t, known)
		p.inviteManager(t, stranger)
	}
	var tk, tn []time.Duration
	for round := 0; round < 20; round++ {
		order := []string{known, stranger}
		if round%2 == 1 {
			order = []string{stranger, known}
		}
		for _, address := range order {
			start := time.Now()
			res, _ := p.inviteManager(t, address)
			d := time.Since(start)
			if res.StatusCode != http.StatusSeeOther {
				t.Fatalf("round %d: %d", round, res.StatusCode)
			}
			if address == known {
				tk = append(tk, d)
			} else {
				tn = append(tn, d)
			}
		}
	}
	a, b := minDuration(tk), minDuration(tn)
	ratio := float64(a) / float64(b)
	if ratio < 1 {
		ratio = 1 / ratio
	}
	t.Logf("with a sign-in: min %v · without: min %v · spread %.2fx (gate %.2fx)", a, b, ratio, inviteTimingGate)
	if ratio >= inviteTimingGate {
		t.Fatalf("the two answers are told apart by time: %.2fx", ratio)
	}
}

// inviteTimingGate bounds the spread of the two minima. Measured on the reference stack (17.10 in
// Docker, 20 interleaved rounds after 4 warm-up pairs, the fake sender):
//
//	noise    plain 1.05-1.11x (5 runs) · -race 1.01-1.02x (2) · the resolver read added for EVERY
//	         address (same work both ways) 1.07x (2)
//	signal   the resolver read plus 8 ms of database work for an address WITH a sign-in: 1.73-1.78x (2)
//
// 1.5x sits between. A difference smaller than ~5 ms is not seen here -- the exact guard is
// adminauth's TestAdminInvites_IssuingTouchesNoIdentity (the send path calls nothing that reads an
// identity), and with a real relay the send's time dwarfs the database's in both cases alike.
const inviteTimingGate = 1.5

func minDuration(d []time.Duration) time.Duration {
	m := d[0]
	for _, x := range d[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

// TestAdminInvitesDB_OpeningTheLinkConsumesNothingAndThePressAccepts (R19): the invitee opens the
// link three times -- the forms each time, the invitation still open -- then chooses a password: the
// answer names no session (the jar holds no panel cookie for them) and the link is dead afterwards.
func TestAdminInvitesDB_OpeningTheLinkConsumesNothingAndThePressAccepts(t *testing.T) {
	p, sender, owner := inviteOwner(t, "M14-15 Accept")
	address := "m14-15-acc-" + uuid.NewString() + "@identity.example"
	cleanBirthOf(t, owner, address)
	if res, _ := p.inviteManager(t, address); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("invite: %d", res.StatusCode)
	}
	m := inviteLinkRe.FindStringSubmatch(sender.sent[0].Text)
	if m == nil {
		t.Fatal("no link in the e-mail")
	}
	link := adminInvitePath + "?t=" + m[1]
	invitee := p.anotherBrowser(t)
	// D2: the first hop moves the token into the cookie and answers 303 to the bare path; the token is
	// in no later URL and in no body.
	res, _ := invitee.get(t, link)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != adminInvitePath {
		t.Fatalf("the link's first hop: %d %q, want 303 to %s", res.StatusCode, res.Header.Get("Location"), adminInvitePath)
	}
	var page string
	for i := 0; i < 3; i++ {
		var res *http.Response
		res, page = invitee.get(t, adminInvitePath)
		if res.StatusCode != http.StatusOK || !strings.Contains(page, "Accept and set my password") || res.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("open %d: %d", i, res.StatusCode)
		}
		if strings.Contains(page, m[1]) {
			t.Fatal("the page body carries the invitation token")
		}
	}
	var open int
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM admin_invites WHERE tenant_id = $1 AND used_at IS NULL AND cancelled_at IS NULL`, p.tenantID).Scan(&open); err != nil || open != 1 {
		t.Fatalf("after three opens %d open invitations (%v), want 1", open, err)
	}
	res, body := invitee.post(t, adminInvitePath, url.Values{"csrf": {csrfFrom(t, page)}, "mode": {"new"},
		"password": {"a fine new password"}, "password_confirm": {"a fine new password"}})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "You&#39;re in") && !strings.Contains(body, "You're in") {
		t.Fatalf("the press: %d %s", res.StatusCode, body)
	}
	if invitee.panelCookie(t) != "" {
		t.Error("the acceptance set a panel session")
	}
	invitee.get(t, link)
	if _, after := invitee.get(t, adminInvitePath); !strings.Contains(after, "That link no longer works") {
		t.Error("the spent link still offers the forms")
	}
	var rows int
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM admin_users WHERE tenant_id = $1 AND email = $2 AND linked_via = 'invite' AND role = 'manager'`, p.tenantID, address).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("%d invited memberships (%v), want 1", rows, err)
	}
	if n := len(tenantRows(t, owner, p.tenantID, "admin.invite.accepted")); n != 1 {
		t.Errorf("%d admin.invite.accepted rows, want 1", n)
	}
}
