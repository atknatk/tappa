package handler

// accountemail_db_test.go -- T173 EC-2 over HTTP against real Postgres, TAPPA_ADMIN_IDENTITY on: the
// harness's row linked to an identity whose address is verified, the panel e-mailing through a fake
// sender (the letters' queue) and the recovery flow's recording channel (the old address's notice and
// the K-E11 reset link).

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
)

// emailChangeHarness is a flag-on harness in the e-mail mode whose row is linked to a VERIFIED identity
// (identity_request_email_change refuses an unverified one). The identity's verification, event and
// reset rows are taken back before linkHarness takes the identity back.
func emailChangeHarness(t *testing.T, logs *lockedBuffer) (*panelHarness, *pgx.Conn, uuid.UUID, *fakeMailSender) {
	t.Helper()
	sender := &fakeMailSender{}
	o := panelHarnessOptions{adminIdentity: true, invitations: func(t *testing.T, cfg *config.Config) *EmailInvitations {
		inv, err := NewEmailInvitations(sender, cfg.BaseURL)
		if err != nil {
			t.Fatalf("NewEmailInvitations: %v", err)
		}
		return inv
	}}
	if logs != nil {
		o.log = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	p := newPanelHarnessWith(t, o)
	owner := linkHarness(t, p)
	ctx := context.Background()
	var ident uuid.UUID
	if err := owner.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, p.adminID).Scan(&ident); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM admin_identity_verifications WHERE identity_id = $1`,
			`DELETE FROM admin_identity_resets WHERE identity_id = $1`,
			`DELETE FROM admin_identity_events WHERE identity_id = $1`,
		} {
			if _, err := owner.Exec(context.Background(), sql, ident); err != nil {
				t.Errorf("take the identity's rows back (%s): %v", sql, err)
			}
		}
	})
	if _, err := owner.Exec(ctx, `UPDATE admin_identities SET email_verified_at = now() WHERE id = $1`, ident); err != nil {
		t.Fatal(err)
	}
	return p, owner, ident, sender
}

var (
	changeLinkRe = regexp.MustCompile(`https?://[^\s"<>]+/admin/change-email\?t=[A-Za-z0-9_-]+`)
	undoLinkRe   = regexp.MustCompile(`https?://[^\s"<>]+/admin/undo-email-change\?t=[A-Za-z0-9_-]+`)
)

// TestEmailChange_TheWholeLoopOverHTTP walks T173 on real Postgres: the request answers the one flash
// and the worker mails the change link to the typed address, built from TAPPA_BASE_URL (the request
// carried a hostile X-Forwarded-Host); the link's GET spends nothing; the press moves the identity and
// the row, writes the business ONE admin.email.changed row, and the old address gets ONE notice whose
// undo link is under the configured base; the undo puts the old address back, ends every session and
// hands ONE "every business" reset link to the outbox for the address it gave back (K-E11 (a)). No
// log line carries either address or a token.
func TestEmailChange_TheWholeLoopOverHTTP(t *testing.T) {
	logs := &lockedBuffer{}
	p, owner, ident, sender := emailChangeHarness(t, logs)
	ctx := context.Background()
	p.signIn(t)
	old := p.email
	newEmail := "moved-" + uuid.NewString() + "@t173.example"

	target := boundPath(p.server.Config.Handler, p.panelCookie(t), accountEmailHref)
	req, err := http.NewRequest(http.MethodPost, p.server.URL+target, strings.NewReader(url.Values{"new_email": {newEmail}, "current_password": {p.password}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", p.server.URL)
	req.Header.Set("X-Forwarded-Host", "evil.example")
	req.Header.Set("Forwarded", "host=evil.example")
	res, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != accountReturn("email-sent", "") {
		t.Fatalf("the request answered %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	waitSent(t, sender, 1)
	sender.mu.Lock()
	letter := sender.sent[0]
	sender.mu.Unlock()
	link := changeLinkRe.FindString(letter.Text)
	if letter.To != newEmail || !strings.HasPrefix(link, p.server.URL+emailChangeLandingPath+"?t=") || strings.Contains(letter.Text+letter.HTML, "evil.example") {
		t.Fatalf("the letter went to %q with link %q (want the typed address and the configured base)", letter.To, link)
	}
	if res, _ := p.get(t, strings.TrimPrefix(link, p.server.URL)); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the link's GET answered %d", res.StatusCode)
	}
	var stillOld string
	if err := owner.QueryRow(ctx, `SELECT email::text FROM admin_identities WHERE id = $1`, ident).Scan(&stillOld); err != nil || stillOld != old {
		t.Fatalf("the link's GET changed the address (%q, %v)", stillOld, err)
	}
	if _, page := p.get(t, accountHref); !strings.Contains(page, "Change your sign-in email to the address this link was sent to?") {
		t.Fatal("the account page does not offer the press")
	}
	res, _ = p.post(t, accountEmailConfirmHref, url.Values{})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != accountReturn("email-changed", "") {
		t.Fatalf("the press answered %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	var identEmail, rowEmail string
	if err := owner.QueryRow(ctx, `SELECT i.email::text, a.email::text FROM admin_identities i JOIN admin_users a ON a.identity_id = i.id WHERE i.id = $1 AND a.id = $2`, ident, p.adminID).Scan(&identEmail, &rowEmail); err != nil {
		t.Fatal(err)
	}
	if identEmail != newEmail || rowEmail != newEmail {
		t.Fatalf("after the press: identity %q, row %q, want %q", identEmail, rowEmail, newEmail)
	}
	if n := p.auditCount(t, "admin.email.changed"); n != 1 {
		t.Errorf("%d admin.email.changed rows, want the definer's 1", n)
	}
	p.mail.mu.Lock()
	notices := append([]EmailChangeNotice(nil), p.mail.emailNotices...)
	p.mail.mu.Unlock()
	if len(notices) != 1 || notices[0].Recipient != old || notices[0].IdentityID != ident ||
		!strings.HasPrefix(notices[0].UndoLink, p.server.URL+emailUndoPath+"?t=") {
		t.Fatalf("notices = %d, want ONE to the old address with an undo link under the configured base", len(notices))
	}
	if !undoLinkRe.MatchString(notices[0].UndoLink) || time.Until(notices[0].UndoExpiresAt) < 6*24*time.Hour {
		t.Errorf("the undo link %q lives until %v", notices[0].UndoLink, notices[0].UndoExpiresAt)
	}

	// The undo: from a browser with no session at all.
	q := p.anotherBrowser(t)
	if res, _ := q.get(t, strings.TrimPrefix(notices[0].UndoLink, p.server.URL)); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the undo link's GET answered %d", res.StatusCode)
	}
	_, page := q.get(t, emailUndoPath)
	res, page = q.post(t, emailUndoPath, url.Values{"csrf": {csrfFrom(t, page)}})
	if res.StatusCode != http.StatusOK || !strings.Contains(page, "We sent a link to set a new password.") {
		t.Fatalf("the undo answered %d", res.StatusCode)
	}
	if err := owner.QueryRow(ctx, `SELECT email::text FROM admin_identities WHERE id = $1`, ident).Scan(&identEmail); err != nil || identEmail != old {
		t.Fatalf("after the undo the identity holds %q (%v), want %q", identEmail, err, old)
	}
	if n := p.liveSessionCount(t); n != 0 {
		t.Errorf("%d live sessions survived the undo", n)
	}
	p.drainReset(t)
	resets := p.mail.all()
	if len(resets) != 1 || resets[0].Recipient != old || !resets[0].EveryBusiness {
		t.Fatalf("%d reset deliveries after the undo, want ONE every-business link to the old address", len(resets))
	}
	var ours []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "address change") || strings.Contains(line, "sign-in address") {
			ours = append(ours, line)
		}
	}
	if len(ours) < 3 {
		t.Fatalf("CONTROL: %d address change lines logged", len(ours))
	}
	joined := strings.ToLower(strings.Join(ours, "\n"))
	for _, secret := range []string{strings.ToLower(old), strings.ToLower(newEmail), strings.ToLower(link[strings.Index(link, "?t=")+3:])} {
		if strings.Contains(joined, secret) {
			t.Errorf("an address change line carries %q", secret)
		}
	}
	for _, line := range ours {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a log line is not JSON: %v", err)
		}
		for k := range rec {
			if !emailLogKeys[k] {
				t.Errorf("log key %q outside the closed set", k)
			}
		}
	}
}

// emailAnswer is one request's answer, as compared across the arms: the status, the headers (less the
// clock's) and the body.
type emailAnswer struct {
	status  int
	headers string
	body    string
}

func (p *panelHarness) emailAnswerOf(t *testing.T, newEmail string) (emailAnswer, time.Duration) {
	t.Helper()
	target := boundPath(p.server.Config.Handler, p.panelCookie(t), accountEmailHref)
	req, err := http.NewRequest(http.MethodPost, p.server.URL+target, strings.NewReader(url.Values{"new_email": {newEmail}, "current_password": {p.password}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", p.server.URL)
	start := time.Now()
	res, err := p.client.Do(req)
	d := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res)
	h := res.Header.Clone()
	h.Del("Date")
	var keys []string
	for k, v := range h {
		keys = append(keys, k+": "+strings.Join(v, ","))
	}
	sort.Strings(keys)
	return emailAnswer{status: res.StatusCode, headers: strings.Join(keys, "\n"), body: body}, d
}

// TestEmailChange_TheThreeArmsAnswerAlikeOverHTTP (ADR 0044 §5.4, card EC-2 item 4): a new address
// nobody holds, one another identity holds, and one with an unlinked row in another business -- each
// request writes one 'change' row and one event (CONTROL), and the three answers are the same status,
// headers and body.
func TestEmailChange_TheThreeArmsAnswerAlikeOverHTTP(t *testing.T) {
	p, owner, ident, _ := emailChangeHarness(t, nil)
	p.panel.emailChangeLimiter = newLimiter(1000, time.Hour)
	p.signIn(t)
	arms := emailChangeArms(t, p, owner)
	var first emailAnswer
	for i, addr := range arms {
		a, _ := p.emailAnswerOf(t, addr)
		if a.status != http.StatusSeeOther {
			t.Fatalf("arm %d answered %d", i, a.status)
		}
		if i == 0 {
			first = a
			continue
		}
		if a != first {
			t.Errorf("arm %d's answer differs from arm 0's:\n%+v\n%+v", i, a, first)
		}
	}
	var rows, events int
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM admin_identity_verifications WHERE identity_id = $1 AND purpose = 'change'`, ident).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'email_change_requested'`, ident).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if rows != len(arms) || events != len(arms) {
		t.Errorf("CONTROL: %d change rows and %d events for %d requests, want one of each per request", rows, events, len(arms))
	}
}

// emailChangeArms makes the three new addresses: nobody's; another identity's; and one with an
// unlinked, active row in another business.
func emailChangeArms(t *testing.T, p *panelHarness, owner *pgx.Conn) []string {
	t.Helper()
	ctx := context.Background()
	free := "free-" + uuid.NewString() + "@t173.example"
	taken := "taken-" + uuid.NewString() + "@t173.example"
	var other uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`, taken, ownerDigest).Scan(&other); err != nil {
		t.Fatalf("another identity: %v", err)
	}
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `DELETE FROM admin_identities WHERE id = $1`, other); err != nil {
			t.Errorf("take the other identity back: %v", err)
		}
	})
	_, rowed := p.addBusiness(t)
	return []string{free, taken, rowed}
}

// TestEmailChange_TheThreeArmsAreFlatOverHTTP (ADR 0044 §5.4, card EC-2 item 4): the three arms
// interleaved, every answer at or past emailChangeRequestFloor, the medians within 60 ms of each other
// (TestPanelIdentityReset_TimingIsFlatOverHTTP's band). Locally on macOS + Docker Desktop the database
// round trip can exceed the band (backlog T198); CI is the referee.
func TestEmailChange_TheThreeArmsAreFlatOverHTTP(t *testing.T) {
	p, owner, _, _ := emailChangeHarness(t, nil)
	p.panel.emailChangeLimiter = newLimiter(1000, time.Hour)
	p.signIn(t)
	arms := emailChangeArms(t, p, owner)
	const samples = 6
	times := make([][]time.Duration, len(arms))
	for i := 0; i < samples; i++ {
		for a, addr := range arms {
			ans, d := p.emailAnswerOf(t, addr)
			if ans.status != http.StatusSeeOther {
				t.Fatalf("arm %d answered %d", a, ans.status)
			}
			if d < emailChangeRequestFloor {
				t.Fatalf("an answer took %v, under the floor of %v", d, emailChangeRequestFloor)
			}
			times[a] = append(times[a], d)
		}
	}
	med := func(d []time.Duration) time.Duration {
		s := append([]time.Duration(nil), d...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return s[len(s)/2]
	}
	meds := []time.Duration{med(times[0]), med(times[1]), med(times[2])}
	spread := slices.Max(meds) - slices.Min(meds)
	t.Logf("medians: free %v, identity's %v, another business's row %v (spread %v, floor %v)", meds[0], meds[1], meds[2], spread, emailChangeRequestFloor)
	if spread > 60*time.Millisecond {
		t.Errorf("medians %v differ by %v: the arms leak through the clock -- or, on a local macOS + Docker Desktop "+
			"run, the database round trip does (backlog T198); CI is the referee", meds, spread)
	}
}
