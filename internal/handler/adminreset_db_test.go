package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// The WIRED recovery flow, over real HTTP against real Postgres.
//
// 🔴 WHY THIS FILE EXISTS BESIDE adminreset_test.go, which already drives every
// branch against fakes: the M5-04 lesson. A capability can be delivered, tested and
// DEAD in the wired product because two halves were never assembled — and three of
// the five M7-04 acceptance criteria are about things a fake cannot have. RLS decides
// which tenant a row lands in; the resolver's ordering decides who gets a link; the
// atomic consume statement decides whether a replayed link does anything. A double
// agrees with whatever it is told about all three.

// TestPanelRecoveryDB_EndToEnd walks the whole loop a locked-out operator walks, in
// one test, because the loop is the claim: sign in, ask for a link, open it, set a
// new password, discover the old session is dead, and find the trail carrying both
// halves.
//
// ⚠️ IT IS THE ONE TEST IN THIS PACKAGE THAT PAYS A SHIPPED-COST bcrypt (~11 s under
// -race, measured). The harness comment says why it cannot be lowered from here and
// why exactly one test spends it.
func TestPanelRecoveryDB_EndToEnd(t *testing.T) {
	p := newPanelHarness(t)

	// --- 1. a live panel session, so the revocation has something to revoke -------
	p.signIn(t)
	if res, _ := p.get(t, "/admin"); res.StatusCode != http.StatusOK {
		t.Fatalf("the signed-in panel answered %d, want 200 — this test needs a live session "+
			"or the revocation below proves nothing", res.StatusCode)
	}
	before := p.storedDigest(t)

	// --- 2. ask for a link -------------------------------------------------------
	res, body := p.get(t, adminResetPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", adminResetPath, res.StatusCode)
	}
	res, body = p.post(t, adminResetPath, url.Values{
		"csrf": {csrfFrom(t, body)}, "email": {strings.ToUpper(p.email)},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200", adminResetPath, res.StatusCode)
	}
	if strings.Contains(body, "Nothing was sent") {
		t.Fatal("the harness wired no delivery channel; this test would then measure the dark path")
	}
	// The send happens on the outbox's worker after the response (M10 EM-5).
	p.drainReset(t)

	delivered := p.mail.all()
	if len(delivered) != 1 {
		t.Fatalf("the channel received %d link(s), want exactly 1", len(delivered))
	}
	// CRITERION 3, against the real resolver: the address is the one ON THE ROW, and
	// the request deliberately typed a different spelling of it.
	if delivered[0].Recipient != p.email {
		t.Errorf("delivered to %q, want the address on the administrator's row (%q)",
			delivered[0].Recipient, p.email)
	}
	link := delivered[0].Link
	if !strings.Contains(link, adminResetNewPath+"?t=") {
		t.Fatalf("the link does not point at the recovery page: %q", link)
	}
	// AND THE LINK IS NOWHERE IN THE ANSWER. This is the sentence ADR 0015 says is
	// the only thing standing between minting and account takeover.
	if strings.Contains(body, link) {
		t.Fatal("the response carries the recovery link back to whoever asked for it")
	}

	// --- 3. open it and set a new password ---------------------------------------
	const newPassword = "a-brand-new-owner-password"
	res, _ = p.get(t, strings.TrimPrefix(link, p.server.URL))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("opening the link answered %d, want 303 (the token leaves the URL)", res.StatusCode)
	}
	res, form := p.get(t, adminResetNewPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the clean recovery URL answered %d, want 200", res.StatusCode)
	}
	res, _ = p.post(t, adminResetNewPath, url.Values{
		"csrf": {csrfFrom(t, form)}, "password": {newPassword}, "password_confirm": {newPassword},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("setting the password answered %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); !strings.HasPrefix(loc, adminLoginPath) {
		t.Errorf("Location = %q, want the sign-in form", loc)
	}

	// --- 4. the consequences -----------------------------------------------------
	if after := p.storedDigest(t); after == before {
		t.Error("the stored digest did not change")
	}
	if n := p.liveSessionCount(t); n != 0 {
		t.Errorf("%d live session(s) survived the recovery; a reset that leaves the old "+
			"cookies alive is a takeover that survives the victim's remedy", n)
	}
	// The browser that did the reset is signed out too — which is why the flow ends
	// at the sign-in form rather than in the panel.
	if res, _ := p.get(t, "/admin"); res.StatusCode != http.StatusSeeOther {
		t.Errorf("the panel still answered %d to the browser that did the reset, want 303",
			res.StatusCode)
	}

	// --- 5. the trail ------------------------------------------------------------
	for _, action := range []string{ActionAdminResetRequested, ActionAdminResetCompleted} {
		if n := p.auditCount(t, action); n != 1 {
			t.Errorf("%d %q row(s) in audit_log, want 1", n, action)
		}
	}
	// AND THE ROWS ARE IN THIS TENANT. A row anywhere else would mean the flow wrote
	// into a tenant the request never proved anything about (§4.5).
	if n := p.auditCountAnyTenant(t, ActionAdminResetCompleted); n != 1 {
		t.Errorf("%d completion row(s) across the whole table, want 1 — the row this "+
			"tenant sees must be the only one there is", n)
	}

	// --- 5b. the change is announced (M10 EM-9) ----------------------------------
	// Against the REAL resolver: the notice goes to the address ON THE ROW, read after
	// the change committed — the request typed another spelling of it — for this
	// administrator in this tenant, and leaves one sent row.
	notices := p.mail.allNotices()
	if len(notices) != 1 {
		t.Fatalf("the channel was handed %d change notice(s), want 1", len(notices))
	}
	if notices[0].Recipient != p.email || notices[0].TenantID != p.tenantID || notices[0].AdminUserID != p.adminID {
		t.Error("the change notice was not addressed to the row's address for this administrator in this tenant")
	}
	if n := p.auditCount(t, ActionAdminPasswordNoticeSent); n != 1 {
		t.Errorf("%d sent notice row(s) in audit_log, want 1", n)
	}

	// --- 6. the link is spent ----------------------------------------------------
	res, _ = p.get(t, strings.TrimPrefix(link, p.server.URL))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("re-opening the link answered %d, want 303", res.StatusCode)
	}
	res, form = p.get(t, adminResetNewPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the recovery form answered %d on a replay, want 200 (the GET must not "+
			"answer whether the link is live)", res.StatusCode)
	}
	res, replayed := p.post(t, adminResetNewPath, url.Values{
		"csrf": {csrfFrom(t, form)}, "password": {"yet-another-password"}, "password_confirm": {"yet-another-password"},
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("replaying the link answered %d, want 400", res.StatusCode)
	}
	if !strings.Contains(replayed, "no longer works") {
		t.Errorf("the replay did not get the refusal screen:\n%s", replayed)
	}
	if n := p.auditCount(t, ActionAdminResetRefused); n != 1 {
		t.Errorf("%d refusal row(s) in audit_log, want 1 — a failed recovery attempt has to "+
			"land somewhere (§4.6), and this one resolved, so it can be an attributable row",
			n)
	}
	// A refused replay changed nothing, so it announced nothing (M10 EM-9).
	if n := len(p.mail.allNotices()); n != 1 {
		t.Errorf("%d change notice(s) after the refused replay, want still 1", n)
	}
}

// TestPanelRecoveryDB_AnUnregisteredAddressIsIndistinguishable is criterion 5 against
// the REAL resolver, which is the only place the question is genuinely decided.
//
// It pays no bcrypt: nothing here consumes a link.
func TestPanelRecoveryDB_AnUnregisteredAddressIsIndistinguishable(t *testing.T) {
	p := newPanelHarness(t)

	ask := func(t *testing.T, email string) (int, string) {
		t.Helper()
		res, body := p.get(t, adminResetPath)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d", adminResetPath, res.StatusCode)
		}
		res, body = p.post(t, adminResetPath, url.Values{"csrf": {csrfFrom(t, body)}, "email": {email}})
		return res.StatusCode, body
	}

	knownCode, known := ask(t, p.email)
	unknownCode, unknown := ask(t, "nobody-"+uuid.NewString()+"@m7.example")
	p.drainReset(t)

	if knownCode != unknownCode {
		t.Errorf("registered answered %d and unregistered answered %d", knownCode, unknownCode)
	}
	if known != unknown {
		t.Errorf("the two answers differ.\nREGISTERED:\n%s\n\nUNREGISTERED:\n%s", known, unknown)
	}
	// ANTI-VACUITY: the registered arm really produced a link, so the equality above
	// is despite a difference rather than because there was none.
	if n := len(p.mail.all()); n != 1 {
		t.Fatalf("the channel received %d link(s) across both arms, want exactly 1 (the "+
			"registered one). If this is 0, both arms were the unregistered arm.", n)
	}
	// AND THE TRAIL SEPARATES THEM, which is where the difference is allowed to live.
	if n := p.auditCount(t, ActionAdminResetRequested); n != 1 {
		t.Errorf("%d requested row(s), want 1", n)
	}
}

// TestPanelRecoveryDB_EndToEndThroughTheSMTPTransport is M10 EM-5's re-run of the
// M7-04 loop with the delivery a deployment set to "email" really has: the e-mail
// channel and internal/mail's SMTP transport, over TCP and STARTTLS to an in-test
// relay, against real Postgres. The link is read out of the e-mail the relay received
// — not out of a recorder — and walked to a changed password.
//
// What it re-runs (M7-04 criteria): 3 — the message goes to the address on the
// administrator's ROW (the form typed it upper-cased); 2, audit half — the requested,
// completed and (on a replay of the e-mailed link) refused rows are in this tenant;
// 5, body half — the answer does not carry the link, and an unregistered address gets
// the byte-identical page and no message; the original "single use" — the e-mailed
// link spent once, refused on replay. The real relay (SES) is EM-5B's. Since M10 EM-9
// it also reads the "your password was changed" notice the spent link sends: to the
// row's address, with the configured sign-in page as its one URL.
func TestPanelRecoveryDB_EndToEndThroughTheSMTPTransport(t *testing.T) {
	relay := newFakeRelay(t, relayScript{})
	p := newPanelHarnessWithResetChannel(t, func(t *testing.T, cfg *config.Config) ResetChannel {
		user, pass := relayCredentials(t)
		ch, err := NewEmailResetChannel(relay.sender(t, user, pass, 5*time.Second), cfg.BaseURL, slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("NewEmailResetChannel: %v", err)
		}
		return ch
	})
	before := p.storedDigest(t)

	res, body := p.get(t, adminResetPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", adminResetPath, res.StatusCode)
	}
	res, body = p.post(t, adminResetPath, url.Values{"csrf": {csrfFrom(t, body)}, "email": {strings.ToUpper(p.email)}})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Check your email") {
		t.Fatalf("POST %s = %d, or not the deliverable page", adminResetPath, res.StatusCode)
	}
	p.drainReset(t)

	sessions := relay.completed()
	if len(sessions) != 1 {
		t.Fatalf("the relay received %d message(s), want 1", len(sessions))
	}
	if got := sessions[0].rcptTo; len(got) != 1 || got[0] != "RCPT TO:<"+p.email+">" {
		t.Errorf("RCPT %q, want the address on the administrator's row (%q)", got, p.email)
	}
	m := parseRelayed(t, sessions[0].message)
	linkRe := regexp.MustCompile(regexp.QuoteMeta(p.server.URL+adminResetNewPath+"?t=") + `[A-Za-z0-9_-]+`)
	link := linkRe.FindString(m.text)
	if link == "" || !strings.Contains(m.html, link) {
		t.Fatalf("the e-mail does not carry one recovery link in both parts (text has %q)", link)
	}
	if strings.Contains(body, link) {
		t.Fatal("the response carries the recovery link back to whoever asked for it")
	}
	if n := p.auditCount(t, ActionAdminResetRequested); n != 1 {
		t.Errorf("%d requested row(s), want 1", n)
	}

	const newPassword = "a-password-from-the-mailbox"
	if res, _ = p.get(t, strings.TrimPrefix(link, p.server.URL)); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("opening the e-mailed link answered %d, want 303", res.StatusCode)
	}
	res, form := p.get(t, adminResetNewPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the recovery form answered %d", res.StatusCode)
	}
	res, _ = p.post(t, adminResetNewPath, url.Values{
		"csrf": {csrfFrom(t, form)}, "password": {newPassword}, "password_confirm": {newPassword},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("setting the password answered %d, want 303", res.StatusCode)
	}
	if p.storedDigest(t) == before {
		t.Error("the stored digest did not change")
	}
	if n := p.auditCount(t, ActionAdminResetCompleted); n != 1 {
		t.Errorf("%d completed row(s), want 1", n)
	}

	// THE CHANGE IS ANNOUNCED (M10 EM-9), through the same transport, in the request
	// that spent the link — so the relay already holds it. It goes to the address ON
	// THE ROW (the form typed it upper-cased), its one URL in each part is the
	// configured sign-in page, and it carries none of the e-mailed link.
	sessions = relay.completed()
	if len(sessions) != 2 {
		t.Fatalf("the relay holds %d message(s) after the recovery, want 2 (the link, then the notice)", len(sessions))
	}
	if got := sessions[1].rcptTo; len(got) != 1 || got[0] != "RCPT TO:<"+p.email+">" {
		t.Errorf("the notice went to %d recipient(s) or another spelling, want the address on the row", len(got))
	}
	notice := parseRelayed(t, sessions[1].message)
	if got := notice.header.Get("Subject"); got != "Your Taptime password was changed" {
		t.Errorf("the notice's Subject is %q, want the fixed one", got)
	}
	for part, body := range map[string]string{"text": notice.text, "html": notice.html} {
		if urls := absoluteURLs.FindAllString(body, -1); len(urls) != 1 || urls[0] != p.server.URL+adminLoginPath {
			t.Errorf("the notice's %s part carries %d URL(s), want exactly the sign-in page", part, len(urls))
		}
		if strings.Contains(body, link) || strings.Contains(body, adminResetNewPath) {
			t.Errorf("the notice's %s part carries the recovery link or its path", part)
		}
	}
	if n := p.auditCount(t, ActionAdminPasswordNoticeSent); n != 1 {
		t.Errorf("%d sent notice row(s), want 1", n)
	}

	// THE E-MAILED LINK WORKS ONCE (criterion 2's audit half): replayed, it is refused
	// on the one refusal screen and the refusal is an attributable row.
	if res, _ = p.get(t, strings.TrimPrefix(link, p.server.URL)); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("re-opening the e-mailed link answered %d, want 303", res.StatusCode)
	}
	res, form = p.get(t, adminResetNewPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the recovery form answered %d on a replay", res.StatusCode)
	}
	res, replayed := p.post(t, adminResetNewPath, url.Values{
		"csrf": {csrfFrom(t, form)}, "password": {"yet-another-password"}, "password_confirm": {"yet-another-password"},
	})
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(replayed, "no longer works") {
		t.Errorf("replaying the e-mailed link answered %d without the refusal screen", res.StatusCode)
	}
	if n := p.auditCount(t, ActionAdminResetRefused); n != 1 {
		t.Errorf("%d refusal row(s), want 1", n)
	}

	// AND AN UNREGISTERED ADDRESS GETS THE SAME PAGE AND NO MESSAGE (criterion 5's
	// body half, with the SMTP transport behind the registered arm).
	_, page := p.get(t, adminResetPath)
	res, unknown := p.post(t, adminResetPath, url.Values{
		"csrf": {csrfFrom(t, page)}, "email": {"nobody-" + uuid.NewString() + "@m10.example"},
	})
	p.drainReset(t)
	if res.StatusCode != http.StatusOK || unknown != body {
		t.Errorf("an unregistered address answered %d with a page that differs from the registered one", res.StatusCode)
	}
	// Two: the link and the notice. The refused replay and the unregistered request
	// sent nothing.
	if n := len(relay.completed()); n != 2 {
		t.Errorf("the relay holds %d message(s) after the unregistered request, want still 2", n)
	}
}

// TestResetOutboxDB_AFullOutboxFitsTheWriteReserve measures what
// ResetDrainWriteReserve has to hold: a FULL outbox — one grant in flight and
// resetOutboxSize waiting — drained with a budget just over the reserve, so the sends
// stop at once and every row is written by the REAL audit recorder into real
// Postgres. The drain must finish inside its budget, with exactly one undelivered row
// per grant. The log line is the measurement the reserve is argued from.
func TestResetOutboxDB_AFullOutboxFitsTheWriteReserve(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required). Run `make test`.")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: withSmallPool(dsn)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	tenantID := uuid.New()
	if err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Outbox Drain Ltd', $2, 'bar', 'single')`, tenantID, "VAT-"+tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	inTenant := func(gs []adminauth.ResetGrant) []adminauth.ResetGrant {
		for i := range gs {
			gs[i].Issued.Reset.TenantID = tenantID
		}
		return gs
	}
	first := inTenant(grantsFor("first@drain.example.test", 1))
	queued := inTenant(grantsFor("queued@drain.example.test", resetOutboxSize))
	ch := newGateChannel()
	h, err := NewAdminReset(&fakeResets{grantsFor: map[string][]adminauth.ResetGrant{
		"first@drain.example.test": first, "queued@drain.example.test": queued,
	}}, ch, trail, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	stopWorkerAtCleanup(t, h)
	router := mountReset(h)
	requestReset(t, newBrowser(t, router), "first@drain.example.test")
	ch.waitStarted(t)
	requestReset(t, newBrowser(t, router), "queued@drain.example.test")

	const lead = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), ResetDrainWriteReserve+lead)
	defer cancel()
	start := time.Now()
	if err := h.Drain(ctx); err != nil {
		t.Fatalf("a full outbox did not drain inside ResetDrainWriteReserve (%v): %v", ResetDrainWriteReserve, err)
	}
	took := time.Since(start)
	rows := 1 + resetOutboxSize
	t.Logf("a full outbox: %d undelivered rows written in %v after the sends stopped (reserve %v)",
		rows, took-lead, ResetDrainWriteReserve)

	var n int
	if err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			tenantID, ActionAdminResetUndelivered).Scan(&n)
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != rows {
		t.Errorf("%d undelivered row(s) in the tenant, want %d — one per grant", n, rows)
	}
}

// --- harness helpers ------------------------------------------------------------

func (p *panelHarness) storedDigest(t *testing.T) string {
	t.Helper()
	var digest string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT password_hash FROM admin_users WHERE id = $1 AND tenant_id = $2`,
			p.adminID, p.tenantID).Scan(&digest)
	}); err != nil {
		t.Fatalf("read digest: %v", err)
	}
	return digest
}

func (p *panelHarness) liveSessionCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM admin_sessions
			 WHERE tenant_id = $1 AND admin_user_id = $2 AND revoked_at IS NULL`,
			p.tenantID, p.adminID).Scan(&n)
	}); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

// auditCountAnyTenant counts rows for this administrator ACROSS EVERY TENANT, through
// the migration role's pool — which is RLS-exempt (it owns the tables), so a row
// written into somebody else's tenant is visible here and invisible to the query
// above. That difference is the whole point of having both.
func (p *panelHarness) auditCountAnyTenant(t *testing.T, action string) int {
	t.Helper()
	pool := ownerPoolForTest(t)
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_log WHERE action = $1 AND target = $2`,
		action, p.adminID.String()).Scan(&n); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}
