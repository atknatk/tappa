package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
