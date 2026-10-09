package handler

// inviteemail_test.go — M10 EM-7B's panel side against fakes: the construction checks,
// every outcome's sentence and status, the owner gate, the panel mode left as it was,
// and the log of a failed send. The real database, the real relay and the real link are
// inviteemail_db_test.go's.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/web/templates/components"
)

// fakeMailSender is an inviteSender that records and answers as told. capped is its
// answer to the per-mailbox cap's question (M10 EM-7C); asked and askedScopes are what
// it was asked about, sentScopes the scope each send was counted under.
type fakeMailSender struct {
	mu          sync.Mutex
	sent        []mail.Message
	err         error
	capped      bool
	asked       []string
	askedScopes []string
	sentScopes  []string
}

func (f *fakeMailSender) Capped(scope, to string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, to)
	f.askedScopes = append(f.askedScopes, scope)
	return f.capped
}

func (f *fakeMailSender) Send(_ context.Context, scope string, m mail.Message) (mail.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentScopes = append(f.sentScopes, scope)
	if f.err != nil {
		return mail.Receipt{}, f.err
	}
	f.sent = append(f.sent, m)
	return mail.Receipt{MessageID: "fake-relay-id-01"}, nil
}

func (f *fakeMailSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// emailPanel is a signed-in browser over the panel in the e-mail mode, with the role
// the test chooses, and the panel's log.
func emailPanel(t *testing.T, role string, staff *fakeStaff, invites *fakeInviter, trail auditRecorder, sender *fakeMailSender) (*browser, *lockedBuffer) {
	t.Helper()
	logs := &lockedBuffer{}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "KF Admin",
		}, nil
	}}
	cfg := adminTestConfig()
	cfg.InviteDelivery = config.InviteDeliveryEmail
	transport, err := NewEmailInvitations(sender, cfg.BaseURL)
	if err != nil {
		t.Fatalf("NewEmailInvitations: %v", err)
	}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		staff, invites, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})), InvitationsByEmail(transport))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b, logs
}

// fakeEmailLink is a code-shaped link under adminTestConfig's base, so the real
// renderer accepts it.
const fakeEmailLink = testBaseURL + "/activate?code=FAKEemailCODEvalue0123456789abcdef"

// TestNewAdminAuth_TheInvitationModeMatchesTheConfiguration: the panel refuses an
// e-mail transport in the panel mode (it would mail links a deployment never switched
// on) and the e-mail mode without one (it would show every link on screen while the
// operator believed they were mailed); an unknown mode is refused; "" is the panel.
func TestNewAdminAuth_TheInvitationModeMatchesTheConfiguration(t *testing.T) {
	transport, err := NewEmailInvitations(&fakeMailSender{}, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	build := func(mode string, opts ...AdminAuthOption) error {
		cfg := adminTestConfig()
		cfg.InviteDelivery = mode
		records := newFakeLedger()
		_, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, records, records, &fakeReviewer{},
			&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
			newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, discardLogger(), opts...)
		return err
	}
	for _, tc := range []struct {
		mode   string
		opts   []AdminAuthOption
		accept bool
	}{
		{"", nil, true},
		{config.InviteDeliveryPanel, nil, true},
		{config.InviteDeliveryEmail, []AdminAuthOption{InvitationsByEmail(transport)}, true},
		{config.InviteDeliveryPanel, []AdminAuthOption{InvitationsByEmail(transport)}, false},
		{"", []AdminAuthOption{InvitationsByEmail(transport)}, false},
		{config.InviteDeliveryEmail, nil, false},
		{config.InviteDeliveryEmail, []AdminAuthOption{InvitationsByEmail(nil)}, false},
		{"sms", nil, false},
	} {
		err := build(tc.mode, tc.opts...)
		if (err == nil) != tc.accept {
			t.Errorf("mode %q with %d option(s): err %v, accept want %v", tc.mode, len(tc.opts), err, tc.accept)
		}
	}
}

// TestNewEmailInvitations_RefusesWhatItCannotBuild: a nil or typed-nil sender, and a base
// URL the invitation e-mail cannot carry (plain http off loopback, no host), are refused
// at boot; a good base and a loopback http base are accepted.
func TestNewEmailInvitations_RefusesWhatItCannotBuild(t *testing.T) {
	var typedNil *mail.RecipientCap
	if _, err := NewEmailInvitations(nil, testBaseURL); err == nil {
		t.Error("a nil sender was accepted")
	}
	if _, err := NewEmailInvitations(typedNil, testBaseURL); err == nil {
		t.Error("a typed-nil *mail.RecipientCap was accepted")
	}
	for _, base := range []string{"http://panel.example.test", "https://", "ftp://panel.example.test"} {
		if _, err := NewEmailInvitations(&fakeMailSender{}, base); err == nil {
			t.Errorf("base %q was accepted", base)
		}
	}
	for _, base := range []string{testBaseURL, "http://127.0.0.1:8080", "https://panel.example.test/"} {
		if _, err := NewEmailInvitations(&fakeMailSender{}, base); err != nil {
			t.Errorf("base %q was refused: %v", base, err)
		}
	}
}

// TestInviteEmail_EveryOutcomeIsASentenceAndNoneCarriesALink drives the e-mail mode's
// press through every outcome the handler writes, with internal/invite's errors in the
// shape IssueAndDeliver returns them: its status, its heading and sentence, no
// "/activate?code=" in the body, and — for the sent one — the address it went to and
// the lifetime. The log never carries the link or the address.
func TestInviteEmail_EveryOutcomeIsASentenceAndNoneCarriesALink(t *testing.T) {
	const address = "Maria.ZQ7.Borg@Example.test"
	wrap := func(err error) error { return fmt.Errorf("invite: issue: %w", err) }
	for _, tc := range []struct {
		name    string
		err     error
		sendErr error
		status  int
		want    []string
	}{
		{"sent", nil, nil, http.StatusOK, []string{"Invitation sent to Maria Borg", address, "It works for 7 days", "Sent means our mail service accepted it"}},
		{"no address", wrap(invite.ErrNoAddress), nil, http.StatusOK, []string{"No address on file", "Nothing was sent and no link was created"}},
		{"not ascii", wrap(invite.ErrAddressNotASCII), nil, http.StatusOK, []string{"accented or non-Latin letter"}},
		{"not deliverable", wrap(invite.ErrAddressRefused), nil, http.StatusOK, []string{"a plain address such as name@example.com"}},
		{"administrator", wrap(invite.ErrAddressIsAdministrators), nil, http.StatusOK, []string{"also the address of an administrator"}},
		{"person limit", wrap(invite.ErrEmployeeHourLimit), nil, http.StatusTooManyRequests, []string{"3 invitations", "were created for Maria Borg in the last hour", "Try again later"}},
		{"business hour", wrap(invite.ErrTenantHourLimit), nil, http.StatusTooManyRequests, []string{"This business has created", "50 invitations in the last hour", "Try again later"}},
		{"business day", wrap(invite.ErrTenantDayLimit), nil, http.StatusTooManyRequests, []string{"This business has created", "300 invitations in the last 24 hours", "Try again later"}},
		{"relay refused", nil, &mail.SendError{Class: mail.ClassRejected, SMTPCode: 554}, http.StatusServiceUnavailable,
			[]string{"The email may not have gone out", "Try again from their card", "has already stopped working"}},
		{"breaker open", nil, &mail.SendError{Class: mail.Class("breaker")}, http.StatusServiceUnavailable,
			[]string{"The email may not have gone out", "Try again from their card"}},
		// M10 EM-7C: the per-mailbox cap, asked before the mint (a refusal) and met at
		// the send after it (a race — minted, not sent).
		{"recipient cap", wrap(invite.ErrRecipientCapped), nil, http.StatusTooManyRequests,
			[]string{"Too many invitations to that address for now", "Nothing was sent and no link was created",
				"This business can send one inbox at most", "5 invitations an hour and 20 a",
				"the inbox at the address on file for Maria Borg has had that many from it", "Try again later"}},
		// EM-7C round 2: the cap met at the send has its own sentence — not "a few
		// minutes" (the slot frees within the hour, or the day).
		{"recipient cap at the send", nil, &mail.SendError{Class: mail.ClassRecipientCap}, http.StatusServiceUnavailable,
			[]string{"The invitation was created but not emailed", "reached the most invitations this business can",
				"5 an hour and 20 a day", "has no working link", "within an hour, or within a", "day if the daily limit was reached"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			staff := &fakeStaff{}
			invites := &fakeInviter{err: tc.err, link: fakeEmailLink,
				recipient: invite.Recipient{Address: address, EmployeeName: "Maria Borg", TenantName: "Kebab Factory", TenantVerified: true}}
			sender := &fakeMailSender{err: tc.sendErr}
			trail := &fakeTrail{}
			b, logs := emailPanel(t, adminRoleOwner, staff, invites, trail, sender)
			rec := b.do(http.MethodPost, employeeInviteHref, url.Values{"id": {uuid.NewString()}})
			body := htmlOf(t, rec)
			if rec.Code != tc.status {
				t.Fatalf("answered %d, want %d", rec.Code, tc.status)
			}
			// A failed send — the breaker's refusal among them (EM-7A) — is ONE
			// invite.undelivered row carrying the failure's class.
			var se *mail.SendError
			if errors.As(tc.sendErr, &se) {
				var undelivered []string
				for _, e := range trail.eventsSnapshot() {
					if e.Action == invite.ActionUndelivered {
						undelivered = append(undelivered, detailJSON(t, e.Detail))
					}
				}
				if len(undelivered) != 1 || !strings.Contains(undelivered[0], `"class":"`+string(se.Class)+`"`) {
					t.Errorf("invite.undelivered rows %v, want one with class %q", undelivered, se.Class)
				}
			}
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("the page does not say %q", w)
				}
			}
			// EM-7B round 3: the count is CREATED links (a failed send and an on-screen link
			// are rows too), so no limit sentence claims they were sent or blames the address.
			if tc.status == http.StatusTooManyRequests {
				for _, w := range []string{"has been sent", "has sent", "Check the address"} {
					if strings.Contains(body, w) {
						t.Errorf("a limit sentence says %q", w)
					}
				}
			}
			if tc.name == "recipient cap at the send" && strings.Contains(body, "few minutes") {
				t.Error("the cap met at the send says \"a few minutes\"; its slot frees within the hour, or the day")
			}
			if strings.Contains(body, "/activate?code=") || strings.Contains(body, "FAKEemailCODEvalue") {
				t.Error("the e-mail mode's page carries the activation link")
			}
			if tc.name != "sent" && strings.Contains(body, address) {
				t.Error("a page that sent nothing names the address")
			}
			out := strings.ToLower(logs.String())
			if strings.Contains(out, strings.ToLower(address)) || strings.Contains(out, "fakeemailcodevalue") {
				t.Errorf("the log carries the address or the link:\n%s", logs.String())
			}
			if n := len(invites.issuedParams()); n != 1 {
				t.Errorf("%d issue call(s), want 1", n)
			}
			if _, ok := invites.channels[0].(*invite.EmailChannel); !ok {
				t.Errorf("the press took a %T, want *invite.EmailChannel", invites.channels[0])
			}
		})
	}
}

// TestInviteEmail_TheFallbackIsTheOwnersAlone: in the e-mail mode a press with
// deliver=screen is the owner's on-screen fallback. A manager, a live session with no
// role and one with an undefined role are refused — no issue call, one
// invite.show_refused row whose role is the SESSION's, not a posted one, and no link
// on the page; the owner gets the panel channel and the link, once.
func TestInviteEmail_TheFallbackIsTheOwnersAlone(t *testing.T) {
	form := url.Values{"id": {uuid.NewString()}, inviteDeliverField: {inviteDeliverScreen}, "role": {"owner"}}
	for _, role := range []string{"manager", "", "auditor"} {
		trail := &fakeTrail{}
		invites := &fakeInviter{link: fakeEmailLink}
		b, _ := emailPanel(t, role, &fakeStaff{}, invites, trail, &fakeMailSender{})
		rec := b.do(http.MethodPost, employeeInviteHref, form)
		body := htmlOf(t, rec)
		if rec.Code != http.StatusOK || !strings.Contains(body, "Only the business owner can show an activation link") {
			t.Errorf("role %q: %d, want 200 and the owner sentence", role, rec.Code)
		}
		if strings.Contains(body, "/activate?code=") {
			t.Errorf("role %q: the refused fallback shows a link", role)
		}
		if n := len(invites.issuedParams()); n != 0 {
			t.Errorf("role %q: %d issue call(s) on a refused fallback", role, n)
		}
		rows := trail.eventsSnapshot()
		if len(rows) != 1 || rows[0].Action != ActionInviteShowRefused {
			t.Fatalf("role %q: trail %v, want one %s", role, rows, ActionInviteShowRefused)
		}
		d := detailJSON(t, rows[0].Detail)
		if !strings.Contains(d, `"role":"`+role+`"`) || !strings.Contains(d, `"required_role":"owner"`) || !strings.Contains(d, `"reason":"not_permitted"`) {
			t.Errorf("role %q: refusal detail %s", role, d)
		}
	}
	trail := &fakeTrail{}
	invites := &fakeInviter{link: fakeEmailLink}
	sender := &fakeMailSender{}
	b, _ := emailPanel(t, adminRoleOwner, &fakeStaff{}, invites, trail, sender)
	rec := b.do(http.MethodPost, employeeInviteHref, form)
	body := htmlOf(t, rec)
	if rec.Code != http.StatusOK || !strings.Contains(body, "FAKEemailCODEvalue") {
		t.Fatalf("owner fallback: %d, the link shown: %v", rec.Code, strings.Contains(body, "FAKEemailCODEvalue"))
	}
	if _, ok := invites.channels[0].(*invite.ManagerVisibleChannel); !ok || !invites.issuedParams()[0].OnlyWithoutAddress {
		t.Errorf("the owner's fallback took a %T without OnlyWithoutAddress=%v", invites.channels[0], invites.issuedParams()[0].OnlyWithoutAddress)
	}
	if n := trail.count(invite.ActionCodeShownToManager); n != 1 || sender.count() != 0 {
		t.Errorf("owner fallback: %d disclosure row(s), %d e-mail(s); want 1 and 0", n, sender.count())
	}
}

// TestInviteEmail_ThePanelModeNeverMails: with TAPPA_INVITE_DELIVERY=panel the press
// takes the panel channel whatever the form says (deliver=screen is not read), the card
// says what it always said, and no e-mail-mode words appear.
func TestInviteEmail_ThePanelModeNeverMails(t *testing.T) {
	invites := &fakeInviter{}
	staff := &fakeStaff{email: "Someone@example.test"}
	b := panelBrowserWithActions(t, staff, invites)
	for _, form := range []url.Values{{"id": {uuid.NewString()}}, {"id": {uuid.NewString()}, inviteDeliverField: {inviteDeliverScreen}}} {
		if rec := b.do(http.MethodPost, employeeInviteHref, form); rec.Code != http.StatusOK || !strings.Contains(htmlOf(t, rec), "FAKE-CODE-VALUE") {
			t.Fatalf("panel mode press answered %d without the link", rec.Code)
		}
	}
	for i, ch := range invites.channels {
		if _, ok := ch.(*invite.ManagerVisibleChannel); !ok {
			t.Errorf("press %d took a %T in the panel mode", i, ch)
		}
		if invites.issuedParams()[i].OnlyWithoutAddress {
			t.Errorf("press %d asked for the e-mail mode's fallback in the panel mode", i)
		}
	}
	card := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New()), nil))
	if !strings.Contains(card, "The link is shown once, on the next screen") {
		t.Error("the panel mode's card lost its help line")
	}
	for _, w := range []string{"goes by email", "Show the link on screen", "no link can be emailed"} {
		if strings.Contains(card, w) {
			t.Errorf("the panel mode's card says %q", w)
		}
	}
}

// TestInviteEmail_TheCardOffersOnlyWhatTheServerWouldDo: in the e-mail mode, somebody
// with an address gets the e-mail button; somebody without one gets no e-mail button
// and the sentence — and the on-screen fallback only on an owner's card.
func TestInviteEmail_TheCardOffersOnlyWhatTheServerWouldDo(t *testing.T) {
	for _, tc := range []struct {
		role, email                string
		mail, fallback, noAddrLine bool
	}{
		{adminRoleOwner, "Someone@example.test", true, false, false},
		{adminRoleOwner, "", false, true, true},
		{"manager", "", false, false, true},
	} {
		staff := &fakeStaff{email: tc.email}
		b, _ := emailPanel(t, tc.role, staff, &fakeInviter{}, &fakeTrail{}, &fakeMailSender{})
		card := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New()), nil))
		if got := strings.Contains(card, "goes by email to the address on file"); got != tc.mail {
			t.Errorf("%s/%q: the e-mail button's line shown %v, want %v", tc.role, tc.email, got, tc.mail)
		}
		if got := strings.Contains(card, `name="`+inviteDeliverField+`" value="`+inviteDeliverScreen+`"`); got != tc.fallback {
			t.Errorf("%s/%q: the fallback form shown %v, want %v", tc.role, tc.email, got, tc.fallback)
		}
		if got := strings.Contains(card, "no link can be emailed"); got != tc.noAddrLine {
			t.Errorf("%s/%q: the no-address line shown %v, want %v", tc.role, tc.email, got, tc.noAddrLine)
		}
		if strings.Contains(card, "The link is shown once, on the next screen. Taptime keeps no copy of it.") {
			t.Errorf("%s/%q: the e-mail mode's card says the link is shown on screen", tc.role, tc.email)
		}
	}
}

// TestRosterActions_ThePanelModeCardIsTheCardBeforeEM7B is K7B-10's pin: the action
// card in the panel mode renders the SAME BYTES it rendered before EM-7B. The four
// sha256 values were measured by rendering these four views with the templates of HEAD
// 08b1bd1 (a scratch copy of that tree) and are matched by this tree; any change to the
// panel branch of the card turns one red.
func TestRosterActions_ThePanelModeCardIsTheCardBeforeEM7B(t *testing.T) {
	want := []string{
		"446df1382c0f7923de4306928e4040cb82de9800a5d5a68df6600c851d42037a",
		"0a00fc3a337a32fadd2baf4ee6b940ccad0f58028987bc44030be7f41926ad41",
		"701e2f7d7336986f63472af2a91974d68ad313da3e0e4143c83607b4e2ee1fd6",
		"6d6d118abf2084d91a0404879406ece63b5e9791e435dbd6de6fba2d8c88fc05",
	}
	base := func() components.RosterActionsView {
		return components.RosterActionsView{
			Name: "Maria Borg", Status: "invited", Venue: "St Julians", Department: "Kitchen",
			Hidden:    []components.FormField{{Name: "id", Value: "6f1c2a51-8a43-4c1e-9d2b-3b0f5d7e9a10"}, {Name: "status", Value: "invited"}},
			CloseHref: "/admin/employees", CanInvite: true, InviteLabel: "Send invite", InviteAction: "/admin/employees/invite",
			RecordHref: "/admin/employees/record?id=6f1c2a51-8a43-4c1e-9d2b-3b0f5d7e9a10", CanDeactivate: true,
			ConfirmHref: "/admin/employees?manage=6f1c2a51-8a43-4c1e-9d2b-3b0f5d7e9a10&confirm=deactivate", ConfirmField: "confirm",
			DeactivateAction: "/admin/employees/deactivate", MoveAction: "/admin/employees/move",
			Locations:  []components.OptionView{{Value: "11111111-1111-4111-8111-111111111111", Label: "St Julians"}},
			LocationID: "11111111-1111-4111-8111-111111111111",
			Email:      "Maria.Borg@example.test", CanChangeEmail: true, EmailAction: "/admin/employees/email",
		}
	}
	a, b, c, d := base(), base(), base(), base()
	b.Email, b.CanChangeEmail = "", false
	c.Status, c.CanInvite, c.CanDeactivate = "deactivated", false, false
	d.Status, d.InviteLabel = "active", "Re-invite"
	for i, v := range []components.RosterActionsView{a, b, c, d} {
		var out strings.Builder
		if err := components.RosterActions(v).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(out.String()))
		if got := hex.EncodeToString(sum[:]); got != want[i] {
			t.Errorf("view %d: the panel mode's card renders %s, not the bytes measured before EM-7B (%s)", i, got, want[i])
		}
	}
}

// TestInviteEmail_AFailedSendLogsOnlyIdsClassAndCode: the log line of a send the relay
// refused carries exactly the ids, err_type, class and smtp_code (ADR 0022 §10) — the
// keys of the line, counted, and none of the address, the link or the relay's error
// text. A non-relay failure (a transport that is not internal/mail's) logs err_type
// without a class.
func TestInviteEmail_AFailedSendLogsOnlyIdsClassAndCode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sendErr   error
		wantClass bool
	}{
		{"relay", &mail.SendError{Class: mail.ClassThrottled, SMTPCode: 451}, true},
		{"other", errors.New("transport said maria.zq7@example.test https://x/activate?code=FAKE"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invites := &fakeInviter{link: fakeEmailLink, recipient: invite.Recipient{Address: "Maria.ZQ7@Example.test"}}
			b, logs := emailPanel(t, adminRoleOwner, &fakeStaff{}, invites, &fakeTrail{}, &fakeMailSender{err: tc.sendErr})
			if rec := b.do(http.MethodPost, employeeInviteHref, url.Values{"id": {uuid.NewString()}}); rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("answered %d, want 503", rec.Code)
			}
			line := ""
			for _, l := range strings.Split(logs.String(), "\n") {
				if strings.Contains(l, "the e-mail was not sent") {
					line = l
				}
			}
			if line == "" {
				t.Fatalf("no failure line in the log:\n%s", logs.String())
			}
			keys := jsonKeys(t, line)
			want := "employee_id,err_type,invite_id,level,msg,tenant_id,time"
			if tc.wantClass {
				want = "class,employee_id,err_type,invite_id,level,msg,smtp_code,tenant_id,time"
			}
			if keys != want {
				t.Errorf("failure line keys %q, want %q", keys, want)
			}
			low := strings.ToLower(logs.String())
			for _, leak := range []string{"maria.zq7", "activate?code", "fakeemailcode", "transport said"} {
				if strings.Contains(low, leak) {
					t.Errorf("the log carries %q:\n%s", leak, logs.String())
				}
			}
		})
	}
}

// inviteRowsFail is a trail that refuses the e-mail route's outcome rows and keeps
// every other.
type inviteRowsFail struct{ fakeTrail }

func (f *inviteRowsFail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	if e.Action == invite.ActionCodeEmailed || e.Action == invite.ActionUndelivered {
		return uuid.Nil, errors.New("audit: insert refused")
	}
	return f.fakeTrail.Record(ctx, e)
}

// TestInviteEmail_AnUnrecordedOutcomeIsLoggedNotSwallowed: §4.6 when the outcome row
// cannot be written. An accepted send whose invite.code_emailed row fails answers the
// "may not have gone out" page (the trail cannot confirm it) and logs that the relay
// accepted it but the row was not written; a refused send whose invite.undelivered row
// fails too logs the failure line AND a second line saying the row was not written —
// neither silent, neither carrying the address.
func TestInviteEmail_AnUnrecordedOutcomeIsLoggedNotSwallowed(t *testing.T) {
	const address = "Rowless.ZQ9@Example.test"
	for _, tc := range []struct {
		name    string
		sendErr error
		lines   []string
	}{
		{"accepted", nil, []string{"the relay accepted the e-mail but its trail row was not written"}},
		{"refused", &mail.SendError{Class: mail.ClassRejected, SMTPCode: 550},
			[]string{"the e-mail was not sent", "the undelivered row was not written either"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invites := &fakeInviter{link: fakeEmailLink, recipient: invite.Recipient{Address: address}}
			b, logs := emailPanel(t, adminRoleOwner, &fakeStaff{}, invites, &inviteRowsFail{}, &fakeMailSender{err: tc.sendErr})
			rec := b.do(http.MethodPost, employeeInviteHref, url.Values{"id": {uuid.NewString()}})
			if rec.Code != http.StatusServiceUnavailable || !strings.Contains(htmlOf(t, rec), "The email may not have gone out") {
				t.Fatalf("answered %d without the unsent page", rec.Code)
			}
			out := logs.String()
			for _, l := range tc.lines {
				if !strings.Contains(out, l) {
					t.Errorf("the log does not say %q:\n%s", l, out)
				}
			}
			if strings.Contains(strings.ToLower(out), strings.ToLower(address)) {
				t.Error("the log carries the address")
			}
		})
	}
}

// jsonKeys is the sorted, comma-joined top-level keys of one JSON log line.
func jsonKeys(t *testing.T, line string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// TestInviteEmail_AShowRefusalIsRecordedWhenTheVisitorLeaves (EM-7B round 3): a
// manager's on-screen fallback request whose context is ALREADY CANCELLED (the tab was
// closed) still leaves its one invite.show_refused row — the row is written on a
// context detached from the request's cancellation and bounded on its own, like every
// other row of the e-mail route. CONTROL: the recorder itself refuses a cancelled
// context, so a row written on the request's context would be lost.
func TestInviteEmail_AShowRefusalIsRecordedWhenTheVisitorLeaves(t *testing.T) {
	trail := &ctxTrail{}
	b, _ := emailPanel(t, "manager", &fakeStaff{}, &fakeInviter{link: fakeEmailLink}, trail, &fakeMailSender{})
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := trail.Record(gone, audit.Event{Action: "control"}); err == nil {
		t.Fatal("CONTROL: the recorder accepts a cancelled context, so this test measures nothing")
	}
	form := url.Values{"id": {uuid.NewString()}, inviteDeliverField: {inviteDeliverScreen}}
	req := httptest.NewRequest(http.MethodPost, employeeInviteHref, strings.NewReader(form.Encode())).WithContext(gone)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", b.origin)
	req.RemoteAddr = b.ip
	for name, value := range b.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	b.h.ServeHTTP(httptest.NewRecorder(), req)
	if n := trail.count(ActionInviteShowRefused); n != 1 {
		t.Errorf("%d invite.show_refused row(s) for a request whose visitor left, want 1", n)
	}
}

// TestInviteEmail_TheSinkAsksTheCapAboutTheAddressOnly is the seam M10 EM-7C adds to the
// invitation route: the per-press sink answers internal/invite's question by asking the
// per-mailbox cap about exactly the address it is given, FOR THE BUSINESS it is given
// (EM-7C round 3: the cap is counted per business and mailbox) — asking sends nothing —
// and a send is counted under the business that minted the invitation.
func TestInviteEmail_TheSinkAsksTheCapAboutTheAddressOnly(t *testing.T) {
	const address = "Maria.ZQ7.Borg+Shift@Example.test"
	business := uuid.New()
	sender := &fakeMailSender{capped: true}
	transport, err := NewEmailInvitations(sender, testBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	s := &emailLinkSink{mail: transport, log: discardLogger()}
	if !s.RecipientCapped(business, address) {
		t.Error("the sink says false while the cap says true")
	}
	sender.mu.Lock()
	sender.capped = false
	sender.mu.Unlock()
	if s.RecipientCapped(business, address) {
		t.Error("the sink says true while the cap says false")
	}
	sender.mu.Lock()
	if len(sender.asked) != 2 || sender.asked[0] != address || sender.asked[1] != address {
		t.Errorf("the cap was asked about %q, want exactly the address the sink was given, twice", sender.asked)
	}
	if len(sender.askedScopes) != 2 || sender.askedScopes[0] != business.String() || sender.askedScopes[1] != business.String() {
		t.Errorf("the cap was asked for scopes %q, want the business %s both times", sender.askedScopes, business)
	}
	if len(sender.sent) != 0 {
		t.Errorf("asking sent %d message(s)", len(sender.sent))
	}
	sender.mu.Unlock()

	// THE SEND IS COUNTED UNDER THE INVITATION'S OWN BUSINESS — the scope the question
	// was asked with inside the minting transaction.
	created := time.Now()
	inv := invite.Invite{ID: uuid.New(), TenantID: business, EmployeeID: uuid.New(), CreatedAt: created, ExpiresAt: created.Add(72 * time.Hour)}
	if _, err := s.SendInvitation(context.Background(), invite.Delivery{
		Invite:        inv,
		ActivationURL: testBaseURL + "/activate?code=FAKEemailCODEvalue",
		Recipient:     invite.Recipient{Address: "maria.borg@example.test", EmployeeName: "Maria Borg"},
	}); err != nil {
		t.Fatalf("SendInvitation: %v", err)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.sentScopes) != 1 || sender.sentScopes[0] != business.String() {
		t.Errorf("the send was counted under %q, want the invitation's business %s", sender.sentScopes, business)
	}
}
