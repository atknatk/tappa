package handler

// inviteemail_db_test.go — M10 EM-7B end to end: real HTTP, real Postgres, the real
// invite.Manager, and the real mail.SMTP over TCP and STARTTLS to the in-test relay
// (fakesmtp_test.go). A press in the e-mail mode is walked the way a manager and an
// employee walk it: press, read the message the relay received, open its link.
//
// WHAT ONE RUN LEAVES BEHIND (fixtures are not cleaned up; tappa_app cannot delete
// them — the panel harness's convention): per test one to three fresh tenants
// ("Panel E2E Ltd" or a test-chosen name, vat_verified as the test sets it), each with
// one owner and at most one manager (their addresses under m6.example, the panel
// harness's), a venue, a department and a few employees (their addresses under
// EM7B.example.test, or none), the invitations the presses minted (a few per person),
// the sessions an activation creates, and their audit_log rows. Every id is a
// fresh random UUID; no real tenant's data is touched, and the relay is in-process —
// nothing leaves the machine.

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"mime/quotedprintable"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/mail"
)

// emailHarness is the panel harness in the e-mail mode, its transport the real
// mail.SMTP talking to relay.
func emailHarness(t *testing.T, relay *fakeRelay, o panelHarnessOptions) (p *panelHarness, user, pass string) {
	t.Helper()
	user, pass = relayCredentials(t)
	o.invitations = func(t *testing.T, cfg *config.Config) *EmailInvitations {
		inv, err := NewEmailInvitations(relay.sender(t, user, pass, 5*time.Second), cfg.BaseURL)
		if err != nil {
			t.Fatalf("NewEmailInvitations: %v", err)
		}
		return inv
	}
	return newPanelHarnessWith(t, o), user, pass
}

// setAddress writes the address on file straight into the row, as tappa_app in the
// tenant's context: the panel's own forms would refuse some of what these tests need
// on file (that is the point of the refusals).
func setAddress(t *testing.T, p *panelHarness, employee uuid.UUID, address string) {
	t.Helper()
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE employees SET email = $3 WHERE tenant_id = $1 AND id = $2`, p.tenantID, employee, address)
		return e
	}); err != nil {
		t.Fatalf("setAddress: %v", err)
	}
}

func invitationRows(t *testing.T, p *panelHarness, employee uuid.UUID) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2`,
			p.tenantID, employee).Scan(&n)
	}); err != nil {
		t.Fatalf("invitationRows: %v", err)
	}
	return n
}

// trailDetails reads the detail of every row of one action about one person.
func trailDetails(t *testing.T, p *panelHarness, action string, employee uuid.UUID) []string {
	t.Helper()
	var out []string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3 ORDER BY at`,
			p.tenantID, action, employee.String())
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if e := rows.Scan(&s); e != nil {
				return e
			}
			out = append(out, s)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("trailDetails: %v", err)
	}
	return out
}

func trailKeys(t *testing.T, p *panelHarness, action string, employee uuid.UUID) string {
	t.Helper()
	var keys []string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT array_agg(DISTINCT k ORDER BY k) FROM audit_log, jsonb_object_keys(detail) k
			  WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			p.tenantID, action, employee.String()).Scan(&keys)
	}); err != nil {
		t.Fatalf("trailKeys: %v", err)
	}
	return strings.Join(keys, ",")
}

// linkInText is the activation link on its own line of the text part.
func linkInText(t *testing.T, text string) string {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "/activate?code=") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("the text part carries no activation link")
	return ""
}

// emailAddress is a fresh MIXED-CASE address under example.test (EM-6 round 2: a
// lower-case fixture cannot tell a faithful read from a lower-cased one).
func emailAddress(label string) string {
	return label + "." + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10]) + "@EM7B.example.test"
}

// TestInviteEmailDB_OnePressOneMessageCarryingTheMintedLink is ADR 0022 §7 walked end to
// end: the press answers "sent to <address>, works for 7 days" and carries no link;
// the relay received exactly ONE message, enveloped to the address on the ROW (mixed
// case, byte for byte); the link in it is the one IssueAndDeliver minted — opened on a
// fresh browser it ACTIVATES the employee; the business's trail holds one
// invite.code_emailed row with exactly its five keys, the relay's message id and no
// address, and no invite.code_shown_to_manager row.
func TestInviteEmailDB_OnePressOneMessageCarryingTheMintedLink(t *testing.T) {
	relay := newFakeRelay(t, relayScript{})
	p, _, _ := emailHarness(t, relay, panelHarnessOptions{})
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Rita Zammit", "invited")
	address := emailAddress("Rita")
	setAddress(t, p, employee, address)
	p.signIn(t)

	res, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("press answered %d, want 200", res.StatusCode)
	}
	for _, w := range []string{"Invitation sent to Rita Zammit", address, "It works for 7 days"} {
		if !strings.Contains(body, w) {
			t.Errorf("the page does not say %q", w)
		}
	}
	if strings.Contains(body, "/activate?code=") || strings.Contains(body, "code=") {
		t.Fatal("the e-mail mode's answer carries an activation link")
	}
	sessions := relay.completed()
	if len(sessions) != 1 {
		t.Fatalf("the relay received %d connection(s), want 1", len(sessions))
	}
	s := sessions[0]
	if len(s.rcptTo) != 1 || !strings.Contains(s.rcptTo[0], "<"+address+">") {
		t.Fatalf("the envelope recipient is %v, want the address on the row", s.rcptTo)
	}
	msg := parseRelayed(t, s.message)
	if to := msg.header.Get("To"); to != address {
		t.Errorf("To: %q, want the address on the row", to)
	}
	link := linkInText(t, msg.text)
	if !strings.HasPrefix(link, p.server.URL+"/activate?code=") || !strings.Contains(msg.html, `href="`+link+`"`) {
		t.Fatalf("the message's link %q is not this deployment's activation link in both parts", link)
	}
	if !consentOverHTTP(t, p, codeFromLink(t, link)) {
		t.Fatal("the emailed link did not activate the employee: it is not the link IssueAndDeliver minted")
	}
	rows := trailDetails(t, p, invite.ActionCodeEmailed, employee)
	if len(rows) != 1 {
		t.Fatalf("%d invite.code_emailed row(s), want 1", len(rows))
	}
	if keys := trailKeys(t, p, invite.ActionCodeEmailed, employee); keys != "channel,employee_id,expires_at,invite_id,message_id" {
		t.Errorf("code_emailed keys %q", keys)
	}
	if strings.Contains(rows[0], "@") || !strings.Contains(rows[0], `"message_id": "`+relayMessageID+`"`) {
		t.Errorf("code_emailed detail %s", rows[0])
	}
	if n := employeeAuditRows(t, p, invite.ActionCodeShownToManager, employee); n != 0 {
		t.Errorf("%d invite.code_shown_to_manager row(s) in the e-mail mode, want 0", n)
	}
}

// TestInviteEmailDB_NamesOnlyForAVerifiedBusiness is the user's decision of 2026-10-09
// end to end: the SAME hostile names (call-back prose with a phone number for the
// business, and for the person) are in the e-mail of a business VIES verified, and in
// neither part of the e-mail of a business VIES refused (vat_verified = false) or never
// answered (NULL) — where the neutral words stand instead.
func TestInviteEmailDB_NamesOnlyForAVerifiedBusiness(t *testing.T) {
	const business = "Your account is suspended Call 21234567 now"
	const person = "Call 21234567 today"
	verified, refused := true, false
	for _, tc := range []struct {
		label string
		vat   *bool
		named bool
	}{{"verified", &verified, true}, {"refused", &refused, false}, {"never asked", nil, false}} {
		relay := newFakeRelay(t, relayScript{})
		p, _, _ := emailHarness(t, relay, panelHarnessOptions{tenantName: business, vatVerified: tc.vat})
		_, _, employee := seedPanelPerson(t, p, p.tenantID, person, "invited")
		setAddress(t, p, employee, emailAddress("named"))
		p.signIn(t)
		if res, _ := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}}); res.StatusCode != http.StatusOK {
			t.Fatalf("%s: press answered %d", tc.label, res.StatusCode)
		}
		sessions := relay.completed()
		if len(sessions) != 1 {
			t.Fatalf("%s: %d message(s)", tc.label, len(sessions))
		}
		msg := parseRelayed(t, sessions[0].message)
		for part, text := range map[string]string{"text": msg.text, "html": msg.html} {
			shows := strings.Contains(text, business) && strings.Contains(text, "Hello "+person+",")
			if shows != tc.named {
				t.Errorf("%s: the %s part names the business and the person: %v, want %v", tc.label, part, shows, tc.named)
			}
			if !tc.named {
				if strings.Contains(text, "21234567") || strings.Contains(text, person) {
					t.Errorf("%s: the %s part carries a name or the number", tc.label, part)
				}
				if !strings.Contains(text, "Your employer has invited you to Taptime") || !strings.Contains(text, "Hello,") {
					t.Errorf("%s: the %s part lacks the neutral words", tc.label, part)
				}
			}
		}
	}
}

// TestInviteEmailDB_EachAddressRefusalMintsNothing: no address, a non-ASCII address, an
// ASCII address the relay's rule refuses, and the business owner's own address in
// other capitals — each press answers its sentence, mints no invitation row, opens no
// relay connection, and writes one invite.email_refused row with its reason and no
// address.
func TestInviteEmailDB_EachAddressRefusalMintsNothing(t *testing.T) {
	relay := newFakeRelay(t, relayScript{})
	p, _, _ := emailHarness(t, relay, panelHarnessOptions{})
	p.signIn(t)
	for _, tc := range []struct {
		label, address, reason, sentence string
	}{
		{"no address", "", "no_address", "has no email address on"},
		{"not ASCII", "Mária." + uuid.NewString()[:6] + "@EM7B.example.test", "address_not_ascii", "accented or non-Latin letter"},
		{"relay refuses", "two@@" + uuid.NewString()[:6] + ".example.test", "address_not_deliverable", "a plain address such as"},
		{"the owner's", strings.ToUpper(p.email), "address_is_an_administrators", "also the address of an administrator"},
	} {
		_, _, employee := seedPanelPerson(t, p, p.tenantID, "Refused "+tc.label, "invited")
		if tc.address != "" {
			setAddress(t, p, employee, tc.address)
		}
		res, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
		if res.StatusCode != http.StatusOK || !strings.Contains(body, tc.sentence) || !strings.Contains(body, "Nothing was sent and no link was created") {
			t.Errorf("%s: %d without its sentence", tc.label, res.StatusCode)
		}
		if strings.Contains(body, "/activate?code=") {
			t.Errorf("%s: the refusal carries a link", tc.label)
		}
		if n := invitationRows(t, p, employee); n != 0 {
			t.Errorf("%s: %d invitation row(s), want 0", tc.label, n)
		}
		rows := trailDetails(t, p, invite.ActionEmailRefused, employee)
		if len(rows) != 1 || !strings.Contains(rows[0], `"reason": "`+tc.reason+`"`) || strings.Contains(rows[0], "@") {
			t.Errorf("%s: refusal rows %v", tc.label, rows)
		}
	}
	relay.mu.Lock()
	dials := len(relay.sessions)
	relay.mu.Unlock()
	if dials != 0 {
		t.Errorf("the relay was dialled %d time(s) for presses that were all refused", dials)
	}
}

// TestInviteEmailDB_TheFourthPressInAnHourIsRefusedWithASentence: §9's per-person limit
// over HTTP. Three presses send three messages; the fourth answers 429 with the
// sentence counting CREATED links (round 3: no "sent", no "check the address"), mints
// nothing, sends nothing and writes one refusal row.
func TestInviteEmailDB_TheFourthPressInAnHourIsRefusedWithASentence(t *testing.T) {
	relay := newFakeRelay(t, relayScript{})
	p, _, _ := emailHarness(t, relay, panelHarnessOptions{})
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Pressed Often", "invited")
	setAddress(t, p, employee, emailAddress("Often"))
	p.signIn(t)
	for i := 1; i <= invite.EmployeeHourLimit; i++ {
		if res, _ := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}}); res.StatusCode != http.StatusOK {
			t.Fatalf("press %d answered %d", i, res.StatusCode)
		}
	}
	res, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusTooManyRequests || !strings.Contains(body, "were created for Pressed Often in the last hour") || strings.Contains(body, "Check the address") {
		t.Fatalf("the fourth press answered %d without the limit's sentence", res.StatusCode)
	}
	if n := invitationRows(t, p, employee); n != invite.EmployeeHourLimit {
		t.Errorf("%d invitation row(s), want %d", n, invite.EmployeeHourLimit)
	}
	if n := len(relay.completed()); n != invite.EmployeeHourLimit {
		t.Errorf("%d message(s) sent, want %d", n, invite.EmployeeHourLimit)
	}
	if rows := trailDetails(t, p, invite.ActionEmailRefused, employee); len(rows) != 1 || !strings.Contains(rows[0], "person_hourly_limit") {
		t.Errorf("refusal rows %v", rows)
	}
}

// TestInviteEmailDB_AFailedSendIsRecordedAndNeverLogsTheMessage: the relay refuses the
// message at the end of data with a reply that QUOTES the address (upper-cased), the
// link, the bare code and the link in base64. The press answers 500 "may not have gone
// out"; one invite.undelivered row carries class rejected and code 554 and no address;
// the minted row stands (B12); and the panel's log — every line, JSON — holds the ids,
// the class and the code, and none of: the address in any case, the link, the code, the
// base64, a sentence of the body, the relay's words, the SMTP username or password.
func TestInviteEmailDB_AFailedSendIsRecordedAndNeverLogsTheMessage(t *testing.T) {
	linkRe := regexp.MustCompile(`https?://[^\s"<>]+/activate\?code=[A-Za-z0-9_-]+`)
	var quoted struct{ link, code string }
	relay := newFakeRelay(t, relayScript{endReplyFn: func(message string) string {
		decoded, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(message)))
		link := linkRe.FindString(string(decoded))
		quoted.link, quoted.code = link, link[strings.Index(link, "code=")+len("code="):]
		to := ""
		for _, l := range strings.Split(message, "\r\n") {
			if strings.HasPrefix(l, "To: ") {
				to = strings.TrimPrefix(l, "To: ")
			}
		}
		return "554 5.6.0 zqrelayword refused <" + strings.ToUpper(to) + "> " + link + " " + quoted.code + " " +
			base64.StdEncoding.EncodeToString([]byte(link))
	}})
	logs := &lockedBuffer{}
	p, user, pass := emailHarness(t, relay, panelHarnessOptions{log: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Unlucky Borg", "invited")
	address := emailAddress("Unlucky")
	setAddress(t, p, employee, address)
	p.signIn(t)

	res, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, "The email may not have gone out") {
		t.Fatalf("press answered %d without the unsent sentence", res.StatusCode)
	}
	if quoted.link == "" {
		t.Fatal("PREMISE: the relay never saw the message, so its reply quoted nothing")
	}
	if strings.Contains(body, "/activate?code=") || strings.Contains(body, address) {
		t.Error("the failure page carries the link or the address")
	}
	rows := trailDetails(t, p, invite.ActionUndelivered, employee)
	if len(rows) != 1 || !strings.Contains(rows[0], `"class": "rejected"`) || !strings.Contains(rows[0], `"smtp_code": 554`) || strings.Contains(rows[0], "@") {
		t.Errorf("invite.undelivered rows %v", rows)
	}
	if n := employeeAuditRows(t, p, invite.ActionCodeEmailed, employee); n != 0 {
		t.Errorf("%d code_emailed row(s) for a refused send", n)
	}
	if n := invitationRows(t, p, employee); n != 1 {
		t.Errorf("%d invitation row(s): the minted row should stand (B12)", n)
	}
	out := logs.String()
	if !strings.Contains(out, `"class":"rejected"`) || !strings.Contains(out, `"smtp_code":554`) || !strings.Contains(out, employee.String()) {
		t.Errorf("the failure line does not carry the class, the code and the person:\n%s", out)
	}
	low := strings.ToLower(out)
	for label, leak := range map[string]string{
		"the address": strings.ToLower(address), "the link": strings.ToLower(quoted.link), "the code": strings.ToLower(quoted.code),
		"the base64 link": strings.ToLower(base64.StdEncoding.EncodeToString([]byte(quoted.link))),
		"the body":        "has invited you", "the relay's words": "zqrelayword",
		"the SMTP username": strings.ToLower(user), "the SMTP password": strings.ToLower(pass),
	} {
		if strings.Contains(low, leak) {
			t.Errorf("the log carries %s", label)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.Contains(line, "the e-mail was not sent") {
			continue
		}
		if keys := jsonKeys(t, line); keys != "class,employee_id,err_type,invite_id,level,msg,smtp_code,tenant_id,time" {
			t.Errorf("failure line keys %q, want ADR 0022 §10's set", keys)
		}
	}
}

// TestInviteEmailDB_OnlyTheOwnerSeesALinkOnScreen is K3 end to end. For somebody with
// no address the owner's card offers the on-screen fallback, and pressing it shows the
// link once with one invite.code_shown_to_manager row on channel manager_panel and no
// e-mail. A manager of the same business sees no fallback on the card, and a POST for it
// is refused: no row minted, no link, one invite.show_refused row naming the role. For
// somebody WITH an address the owner's fallback is refused too (has_address), minting
// nothing.
func TestInviteEmailDB_OnlyTheOwnerSeesALinkOnScreen(t *testing.T) {
	relay := newFakeRelay(t, relayScript{})
	p, _, _ := emailHarness(t, relay, panelHarnessOptions{})
	_, _, noAddress := seedPanelPerson(t, p, p.tenantID, "No Address", "invited")
	_, _, hasAddress := seedPanelPerson(t, p, p.tenantID, "Has Address", "invited")
	setAddress(t, p, hasAddress, emailAddress("Has"))
	screen := func(employee uuid.UUID) url.Values {
		return url.Values{"id": {employee.String()}, inviteDeliverField: {inviteDeliverScreen}}
	}

	// A MANAGER of the same business, signed in on a browser of their own.
	managerEmail := "manager-" + uuid.NewString() + "@m6.example"
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'E2E Manager', $3, $4, 'manager', 'active')`,
			uuid.New(), p.tenantID, managerEmail, victimDigest)
		return e
	}); err != nil {
		t.Fatalf("insert manager: %v", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := *p
	m.client = &http.Client{Jar: jar, CheckRedirect: p.client.CheckRedirect}
	m.email, m.password = managerEmail, victimPassword
	m.signIn(t)
	_, card := m.get(t, managedRosterHref(noAddress))
	if strings.Contains(card, `value="`+inviteDeliverScreen+`"`) || !strings.Contains(card, "no link can be emailed") {
		t.Error("the manager's card offers the on-screen fallback, or does not say why there is no button")
	}
	res, body := m.post(t, employeeInviteHref, screen(noAddress))
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Only the business owner can show an activation link") || strings.Contains(body, "/activate?code=") {
		t.Fatalf("the manager's fallback answered %d, or showed a link", res.StatusCode)
	}
	if invitationRows(t, p, noAddress) != 0 {
		t.Error("the manager's refused fallback minted an invitation")
	}
	refusals := trailDetails(t, p, ActionInviteShowRefused, noAddress)
	if len(refusals) != 1 || !strings.Contains(refusals[0], `"role": "manager"`) || !strings.Contains(refusals[0], `"reason": "not_permitted"`) {
		t.Errorf("the manager's refusal rows %v", refusals)
	}

	// THE OWNER.
	p.signIn(t)
	_, card = p.get(t, managedRosterHref(noAddress))
	if !strings.Contains(card, `value="`+inviteDeliverScreen+`"`) {
		t.Fatal("the owner's card does not offer the on-screen fallback for somebody with no address")
	}
	res, body = p.post(t, employeeInviteHref, screen(noAddress))
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the owner's fallback answered %d", res.StatusCode)
	}
	if !consentOverHTTP(t, p, codeFromLink(t, activationLinkIn(t, body))) {
		t.Error("the link the owner was shown does not activate")
	}
	shown := trailDetails(t, p, invite.ActionCodeShownToManager, noAddress)
	if len(shown) != 1 || !strings.Contains(shown[0], `"channel": "manager_panel"`) {
		t.Errorf("code_shown_to_manager rows %v", shown)
	}
	if n := len(relay.completed()); n != 0 {
		t.Errorf("the fallback sent %d e-mail(s)", n)
	}

	res, body = p.post(t, employeeInviteHref, screen(hasAddress))
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "has an email address on file now") || strings.Contains(body, "/activate?code=") {
		t.Errorf("the owner's fallback for somebody with an address answered %d without its sentence, or with a link", res.StatusCode)
	}
	if invitationRows(t, p, hasAddress) != 0 {
		t.Error("the refused fallback minted an invitation for somebody with an address")
	}
	if rows := trailDetails(t, p, ActionInviteShowRefused, hasAddress); len(rows) != 1 || !strings.Contains(rows[0], "has_address") {
		t.Errorf("has_address refusal rows %v", rows)
	}
}

// fillableSender is what the test's breaker wraps: while filling it drops every message
// (as if other flows had sent them), afterwards it hands each to the real transport.
type fillableSender struct {
	mu      sync.Mutex
	filling bool
	real    *mail.SMTP
}

func (s *fillableSender) Send(ctx context.Context, m mail.Message) (mail.Receipt, error) {
	s.mu.Lock()
	filling := s.filling
	s.mu.Unlock()
	if filling {
		return mail.Receipt{}, nil
	}
	return s.real.Send(ctx, m)
}

// TestInviteEmailDB_ABreakerRefusalIsUndeliveredWithItsClass is the EM-7A breaker as an
// invitation meets it, end to end: the REAL mail.Breaker (ADR 0022 §9) on an INJECTED
// clock, in front of the real mail.SMTP and the in-test relay, given to the panel as
// cmd/tappa gives it. With mail.BreakerLimit sends already let through in the window
// (other flows, on the frozen clock), a press mints the invitation (B12), the relay is
// never dialled, the business's trail holds ONE invite.undelivered row with class
// "breaker" and reply code 0 (written explicitly — the action's one key set), the manager reads the blameless "may not have gone out —
// try again in a few minutes" page, and the panel's log names the class. CONTROL: with
// the clock moved past mail.BreakerWindow, the same press is sent and recorded
// invite.code_emailed — so the refusal above was the breaker's window and nothing else.
func TestInviteEmailDB_ABreakerRefusalIsUndeliveredWithItsClass(t *testing.T) {
	relay := newFakeRelay(t, relayScript{})
	user, pass := relayCredentials(t)
	var clockMu sync.Mutex
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	fill := &fillableSender{filling: true}
	var breaker *mail.Breaker
	logs := &lockedBuffer{}
	p := newPanelHarnessWith(t, panelHarnessOptions{
		log: slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		invitations: func(t *testing.T, cfg *config.Config) *EmailInvitations {
			fill.real = relay.sender(t, user, pass, 5*time.Second)
			b, err := mail.NewBreaker(fill, mail.BreakerConfig{Now: clock, Log: slog.New(slog.DiscardHandler)})
			if err != nil {
				t.Fatalf("mail.NewBreaker: %v", err)
			}
			breaker = b
			inv, err := NewEmailInvitations(b, cfg.BaseURL)
			if err != nil {
				t.Fatalf("NewEmailInvitations: %v", err)
			}
			return inv
		},
	})
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Breaker Borg", "invited")
	setAddress(t, p, employee, emailAddress("Breaker"))
	p.signIn(t)

	for i := 0; i < mail.BreakerLimit; i++ {
		if _, err := breaker.Send(context.Background(), mail.Message{To: "fill@other-flow.example.test"}); err != nil {
			t.Fatalf("fill send %d was refused: %v", i+1, err)
		}
	}
	fill.mu.Lock()
	fill.filling = false
	fill.mu.Unlock()

	res, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusServiceUnavailable || !strings.Contains(body, "The email may not have gone out") ||
		!strings.Contains(body, "Try again from their card in a few minutes") {
		t.Fatalf("the press behind an open breaker answered %d without the unsent sentence", res.StatusCode)
	}
	relay.mu.Lock()
	dials := len(relay.sessions)
	relay.mu.Unlock()
	if dials != 0 {
		t.Errorf("the relay was dialled %d time(s) while the breaker was open", dials)
	}
	rows := trailDetails(t, p, invite.ActionUndelivered, employee)
	if len(rows) != 1 || !strings.Contains(rows[0], `"class": "breaker"`) || !strings.Contains(rows[0], `"smtp_code": 0`) || strings.Contains(rows[0], "@") {
		t.Fatalf("invite.undelivered rows %v, want one with class breaker and reply code 0", rows)
	}
	if keys := trailKeys(t, p, invite.ActionUndelivered, employee); keys != "channel,class,employee_id,expires_at,invite_id,smtp_code" {
		t.Errorf("undelivered keys %q", keys)
	}
	if n := invitationRows(t, p, employee); n != 1 {
		t.Errorf("%d invitation row(s): the minted row should stand (B12)", n)
	}
	if !strings.Contains(logs.String(), `"class":"breaker"`) {
		t.Errorf("the failure line does not name the breaker's class:\n%s", logs.String())
	}

	// CONTROL: a whole window later the breaker lets the same press through.
	clockMu.Lock()
	now = now.Add(mail.BreakerWindow + time.Minute)
	clockMu.Unlock()
	if res, _ := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}}); res.StatusCode != http.StatusOK {
		t.Fatalf("CONTROL: the press after the window answered %d", res.StatusCode)
	}
	if n := len(relay.completed()); n != 1 {
		t.Errorf("CONTROL: %d message(s) reached the relay after the window, want 1", n)
	}
	if n := employeeAuditRows(t, p, invite.ActionCodeEmailed, employee); n != 1 {
		t.Errorf("CONTROL: %d invite.code_emailed row(s), want 1", n)
	}
}

// refusalRowsFail is the panel's audit recorder with invite.email_refused rows
// refused; every other row is written by the real recorder.
type refusalRowsFail struct{ next auditRecorder }

func (f refusalRowsFail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	if e.Action == invite.ActionEmailRefused {
		return uuid.Nil, errors.New("audit: insert refused")
	}
	return f.next.Record(ctx, e)
}

// TestInviteEmailDB_ARefusalWhoseRowFailsStillSaysWhy (EM-7B round 3): when the
// refusal's own trail row cannot be written, the press is STILL the refusal — the
// manager reads its sentence (200, not an unexplained 500), no invitation is minted,
// and the panel's log says in a line of its own that the refusal row was not written,
// with the ids and the fixed reason and without the address on file.
func TestInviteEmailDB_ARefusalWhoseRowFailsStillSaysWhy(t *testing.T) {
	relay := newFakeRelay(t, relayScript{})
	logs := &lockedBuffer{}
	p, _, _ := emailHarness(t, relay, panelHarnessOptions{
		log:       slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		wrapTrail: func(next auditRecorder) auditRecorder { return refusalRowsFail{next: next} },
	})
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Rowless Refusal", "invited")
	address := "Mária." + uuid.NewString()[:6] + "@EM7B.example.test"
	setAddress(t, p, employee, address)
	p.signIn(t)

	res, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "accented or non-Latin letter") ||
		!strings.Contains(body, "Nothing was sent and no link was created") {
		t.Fatalf("a refusal whose row failed answered %d without its sentence", res.StatusCode)
	}
	if n := invitationRows(t, p, employee); n != 0 {
		t.Errorf("%d invitation row(s), want 0", n)
	}
	if n := employeeAuditRows(t, p, invite.ActionEmailRefused, employee); n != 0 {
		t.Errorf("PREMISE: %d refusal row(s) were written although the recorder refuses them", n)
	}
	out := logs.String()
	if !strings.Contains(out, "its refusal row was not written") || !strings.Contains(out, `"reason":"address_not_ascii"`) ||
		!strings.Contains(out, employee.String()) {
		t.Errorf("the log does not say the refusal row was not written, with the person and the reason:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), strings.ToLower(address)) || strings.Contains(out, "mária") {
		t.Error("the log carries the address on file")
	}
	relay.mu.Lock()
	dials := len(relay.sessions)
	relay.mu.Unlock()
	if dials != 0 {
		t.Errorf("the relay was dialled %d time(s) for a refused press", dials)
	}
}
