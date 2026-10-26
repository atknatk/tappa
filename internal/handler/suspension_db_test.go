package handler

// ADR 0025 (OP-15 phase B) against a REAL Postgres: the panel's suspension gate as the
// wired product runs it -- adminauth.Manager over the real pool, TouchAdminSession
// reading the two tenants columns, the real audit trail -- and the other half of the
// decision, §4.6: what a suspension must NOT stop.
//
// FIXTURES. A suspension can be written only by the operator's definer or the table
// owner (migration 00063), so these tests suspend a TEST-MADE tenant through the owner
// role (ownerPoolForTest) and lift it in t.Cleanup -- 00063's Down refuses while any
// tenant is suspended (ADR 0025 K15-6). No real tenant is suspended. Like every DB test
// in this package the tenants, people, invitations and records they create are not
// deleted (tappa_app cannot); `make db-reset` clears the dev database.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/pages"
)

// suspendAsOwner suspends a test-made tenant and lifts it when the test ends.
func suspendAsOwner(t *testing.T, owner *pgxpool.Pool, tenantID uuid.UUID, reason db.SuspensionReason) {
	t.Helper()
	t.Cleanup(func() { liftAsOwner(t, owner, tenantID) })
	tag, err := owner.Exec(context.Background(),
		`UPDATE tenants SET suspended_at = clock_timestamp(), suspension_reason = $2 WHERE id = $1`,
		tenantID, string(reason))
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("suspend %v: %v (%d rows)", tenantID, err, tag.RowsAffected())
	}
}

func liftAsOwner(t *testing.T, owner *pgxpool.Pool, tenantID uuid.UUID) {
	t.Helper()
	if _, err := owner.Exec(context.Background(),
		`UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`, tenantID); err != nil {
		t.Errorf("lift %v: %v", tenantID, err)
	}
}

// refusalRows reads the tenant's own tenant.write_refused rows, in its own context.
type refusalRow struct {
	Actor  *uuid.UUID
	Target string
	Detail map[string]any
}

func (p *panelHarness) refusalRows(t *testing.T) []refusalRow {
	t.Helper()
	var out []refusalRow
	err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT actor_id, target, detail FROM audit_log
			  WHERE tenant_id = $1 AND action = $2 ORDER BY at`, p.tenantID, ActionTenantWriteRefused)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r refusalRow
			var raw []byte
			if err := rows.Scan(&r.Actor, &r.Target, &raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &r.Detail); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read refusal rows: %v", err)
	}
	return out
}

// venuesNamed counts this tenant's locations with one name.
func (p *panelHarness) venuesNamed(t *testing.T, name string) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE tenant_id = $1 AND name = $2`,
			p.tenantID, name).Scan(&n)
	}); err != nil {
		t.Fatalf("count venues: %v", err)
	}
	return n
}

// TestPanelSuspensionDB_TheNextPostOfTheSameSessionIsRefused is ADR 0025 §3's
// end-to-end pin (and the second of K15-9's two): ONE signed-in session --
//
//	before the suspension  a gated write (saving a new location) goes through
//	suspended              the SAME session's next gated write answers 403 with the
//	                       sentence, writes nothing, and leaves exactly one row in the
//	                       tenant's own audit_log: this admin, the route's pattern,
//	                       detail exactly {"reason": "non_payment"}
//	lifted                 the same write goes through again
//
// The suspension is committed between two requests of one session, so what is
// measured is that the gate reads it per request (no cache) from the statement the
// session is resolved by.
func TestPanelSuspensionDB_TheNextPostOfTheSameSessionIsRefused(t *testing.T) {
	p := newPanelHarness(t)
	owner := ownerPoolForTest(t)
	p.signIn(t)

	save := func(name string) (*http.Response, string) {
		t.Helper()
		return p.post(t, venueSaveHref, url.Values{"name": {name}})
	}

	if res, _ := save("Before Suspension"); res.StatusCode != http.StatusSeeOther || p.venuesNamed(t, "Before Suspension") != 1 {
		t.Fatalf("control: an unsuspended business's location save answered %d", res.StatusCode)
	}

	suspendAsOwner(t, owner, p.tenantID, db.SuspensionNonPayment)
	res, body := save("During Suspension")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("the same session's next write answered %d for a suspended business, want 403", res.StatusCode)
	}
	if !strings.Contains(body, suspensionHeading) || !strings.Contains(body, htmlText(suspensionSentenceNonPayment)) {
		t.Errorf("the refusal does not say why")
	}
	if n := p.venuesNamed(t, "During Suspension"); n != 0 {
		t.Fatalf("a refused write wrote %d location(s)", n)
	}
	rows := p.refusalRows(t)
	if len(rows) != 1 {
		t.Fatalf("%d refusal row(s) in the tenant's audit_log, want 1", len(rows))
	}
	r := rows[0]
	if r.Actor == nil || *r.Actor != p.adminID || r.Target != venueSaveHref ||
		len(r.Detail) != 1 || r.Detail["reason"] != string(db.SuspensionNonPayment) {
		t.Errorf("the refusal row is actor %v target %q detail %v; want this admin, %q, {reason: non_payment}",
			r.Actor, r.Target, r.Detail, venueSaveHref)
	}

	liftAsOwner(t, owner, p.tenantID)
	if res, _ := save("After Suspension"); res.StatusCode != http.StatusSeeOther || p.venuesNamed(t, "After Suspension") != 1 {
		t.Fatalf("after the suspension was lifted the same write answered %d", res.StatusCode)
	}
	if n := len(p.refusalRows(t)); n != 1 {
		t.Errorf("lifting the suspension left %d refusal row(s), want still 1", n)
	}
}

// TestPanelSuspensionDB_TheRecordPathsStayOpenAndWrite is ADR 0025 §3's exception
// list and K-B1 = (i) measured by what each route WRITES, for a business suspended
// before the admin even signs in:
//
//	sign in                      a session is issued
//	adding a person              the employees row exists, 'invited'
//	re-inviting them             a pending invitation exists and its link is shown
//	opening that link            the wizard records consent (ADR 0026; the activating
//	                             tap is the tap path's, §4, untouched)
//	the manual record, 2 steps   one channel='manual' record with entered_by = this
//	                             admin, and its record.entered row
//	the admin's own password     the stored digest is a different one
//
// and not one refusal row. Somebody who starts work during a suspension can therefore
// be added, invited, brought to the activating tap, and have a missed shift entered by hand: the record is
// not lost (§4.6).
func TestPanelSuspensionDB_TheRecordPathsStayOpenAndWrite(t *testing.T) {
	p := newPanelHarness(t)
	owner := ownerPoolForTest(t)
	venue := seedPanelVenue(t, p, "Suspended Branch")
	suspendAsOwner(t, owner, p.tenantID, db.SuspensionSecurityHold)
	p.signIn(t)

	res, _ := p.post(t, employeeAddHref, url.Values{"full_name": {"Starts During Suspension"}, "location_id": {venue.String()}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("adding a person to a suspended business answered %d, want 303", res.StatusCode)
	}
	employee := addedEmployeeID(t, res)
	if got := employeeStatusOf(t, p, p.tenantID, employee); got != "invited" {
		t.Fatalf("the person added during the suspension is %q, want invited", got)
	}

	res, body := p.post(t, employeeInviteHref, url.Values{"id": {employee.String()}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("re-inviting during the suspension answered %d, want 200", res.StatusCode)
	}
	code := codeFromLink(t, activationLinkIn(t, body))
	if n := pendingInvitations(t, p, employee); n != 1 {
		t.Fatalf("%d pending invitation(s), want 1", n)
	}
	// The link reaches a recorded consent (ADR 0026: the step that makes a browser able
	// to activate on its first NFC tap). The activating tap itself is the tap path's,
	// measured for a suspended business in suspensiontap_db_test.go
	// (TestSuspensionDB_TapsAreDecidedAsIfThereWereNoSuspension, "activation tap").
	if !consentOverHTTP(t, p, code) {
		t.Fatal("the invitation minted during the suspension could not even record consent")
	}

	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	day := time.Now().In(malta).AddDate(0, 0, -1).Format("2006-01-02")
	form := url.Values{"id": {employee.String()}, "direction": {"in"}, "date": {day}, "time": {"09:00"}, "note": {"entered during a suspension"}}
	res, page := p.post(t, manualEntryHref, form)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the manual record's review step answered %d during the suspension, want 200", res.StatusCode)
	}
	form.Set(confirmField, confirmTokenIn(t, page))
	if res, _ := p.post(t, manualRecordHref, form); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the manual record answered %d during the suspension, want 303", res.StatusCode)
	}
	var records, trails int
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2
			    AND channel = 'manual' AND entered_by = $3`, p.tenantID, employee, p.adminID).Scan(&records); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			p.tenantID, manual.Action).Scan(&trails)
	}); err != nil {
		t.Fatalf("read the manual record: %v", err)
	}
	if records != 1 || trails != 1 {
		t.Fatalf("the manual record during the suspension left %d record(s) and %d %s row(s), want 1 and 1",
			records, trails, manual.Action)
	}

	before := p.storedDigest(t)
	const next = "a-new-password-chosen-while-suspended"
	if res, _ := p.post(t, accountPasswordHref, url.Values{
		"current_password": {p.password}, "new_password": {next}, "confirm_password": {next},
	}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("changing one's own password during the suspension answered %d, want 303", res.StatusCode)
	}
	if after := p.storedDigest(t); after == before {
		t.Fatal("the password change answered 303 and the stored digest is the old one")
	}

	if n := len(p.refusalRows(t)); n != 0 {
		t.Errorf("the record paths wrote %d refusal row(s); none of them is gated", n)
	}
}

// TestPanelSuspensionDB_ReadsExportsSignInAndResetStayOpen is ADR 0025 §4's other
// half for the panel: a suspended business's admin signs in, reads every section (each
// carrying the strip), downloads both exports -- the hours export still writes its
// report.exported row -- signs out, and recovers a password through the reset flow,
// which is outside the panel's chain altogether. Nothing here writes a refusal row.
func TestPanelSuspensionDB_ReadsExportsSignInAndResetStayOpen(t *testing.T) {
	p := newPanelHarness(t)
	owner := ownerPoolForTest(t)
	suspendAsOwner(t, owner, p.tenantID, db.SuspensionTermsBreach)
	p.signIn(t)

	for _, s := range pages.PanelSections {
		res, body := p.get(t, s.Href)
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d for a suspended business, want 200", s.Href, res.StatusCode)
			continue
		}
		if !strings.Contains(body, suspensionHeading) || !strings.Contains(body, htmlText(suspensionSentenceTermsBreach)) {
			t.Errorf("GET %s does not carry the suspension strip", s.Href)
		}
	}

	exported := func() int {
		var n int
		if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
				p.tenantID, ActionReportExported).Scan(&n)
		}); err != nil {
			t.Fatalf("count exports: %v", err)
		}
		return n
	}
	before := exported()
	for _, href := range []string{reportsCSVHref, billingCSVHref} {
		res, _ := p.get(t, href)
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/csv") {
			t.Errorf("GET %s = %d %q for a suspended business, want a 200 CSV", href, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	if got := exported() - before; got != 1 {
		t.Errorf("the hours export wrote %d %s row(s) during the suspension, want 1", got, ActionReportExported)
	}

	if res, _ := p.post(t, "/admin/logout", url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Errorf("signing out of a suspended business answered %d, want 303", res.StatusCode)
	}
	if res, _ := p.get(t, transactionsHref); res.StatusCode != http.StatusSeeOther {
		t.Errorf("after signing out the panel answered %d, want 303 to the sign-in page", res.StatusCode)
	}

	res, body := p.get(t, adminResetPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", adminResetPath, res.StatusCode)
	}
	if res, _ = p.post(t, adminResetPath, url.Values{"csrf": {csrfFrom(t, body)}, "email": {p.email}}); res.StatusCode != http.StatusOK {
		t.Fatalf("asking for a recovery link during the suspension answered %d, want 200", res.StatusCode)
	}
	p.drainReset(t)
	delivered := p.mail.all()
	if len(delivered) != 1 {
		t.Fatalf("%d recovery link(s) delivered during the suspension, want 1", len(delivered))
	}
	if res, _ := p.get(t, strings.TrimPrefix(delivered[0].Link, p.server.URL)); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("opening the recovery link answered %d", res.StatusCode)
	}
	res, form := p.get(t, adminResetNewPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the recovery form answered %d", res.StatusCode)
	}
	const recovered = "a-password-recovered-while-suspended"
	digest := p.storedDigest(t)
	if res, _ := p.post(t, adminResetNewPath, url.Values{
		"csrf": {csrfFrom(t, form)}, "password": {recovered}, "password_confirm": {recovered},
	}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("setting the recovered password during the suspension answered %d, want 303", res.StatusCode)
	}
	if p.storedDigest(t) == digest {
		t.Fatal("the recovery answered 303 and the stored digest did not change")
	}
	p.password = recovered
	p.signIn(t)

	if n := len(p.refusalRows(t)); n != 0 {
		t.Errorf("reading, exporting, signing in and out and recovering wrote %d refusal row(s)", n)
	}
}

// suspendedOwnerBrowser is webhookPanelFixture.browserAs for a business the identity
// reports suspended (the suspension rides on the session statement in production --
// TestPanelSuspensionDB_TheNextPostOfTheSameSessionIsRefused measures that path; here
// the keys service, the webhook service and the trail are the real ones over Postgres).
func (f *webhookPanelFixture) suspendedOwnerBrowser(t *testing.T, code db.SuspensionReason) *browser {
	t.Helper()
	trail, err := audit.New(f.data)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := tenant.NewAccounts(f.data, trail, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: uuid.New(), TenantID: f.tenantID, AdminUserID: f.owner, Role: "owner",
			FullName: "Hook Admin", Suspended: &adminauth.Suspension{Since: time.Now(), Reason: string(code)}}, nil
	}}
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{},
		&fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), accounts, newFakeBrands(),
		newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), discardLogger(),
		IntegrationKeys(f.keys), IntegrationWebhook(f.svc))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// TestPanelSuspensionDB_TheDefencesStayOpenAndTakeEffect is K-26 (user decision
// 2026-10-10; ADR 0025's M11 note, plan §4.7) measured by what each write DOES for a
// suspended business, over the real keys and webhook services: revoking one key,
// switching the webhook off and revoking every key go through and change the rows;
// creating a key, re-addressing, rotating, switching the webhook back on, the test ping
// and the resend each answer 403, change nothing, send nothing and leave exactly one
// tenant.write_refused row naming the route.
func TestPanelSuspensionDB_TheDefencesStayOpenAndTakeEffect(t *testing.T) {
	f := newWebhookPanelFixture(t)
	setup := f.browserAs(t, f.owner, "owner", true)
	for _, name := range []string{"First", "Second", "Third"} {
		if rec := setup.do(http.MethodPost, apiKeysHref, url.Values{"name": {name}}); rec.Code != http.StatusOK {
			t.Fatalf("control: creating key %s answered %d", name, rec.Code)
		}
	}
	f.create(t, setup)
	url0, sealed0, active := f.endpointRow(t)
	if !active {
		t.Fatal("control: the webhook is not on")
	}

	suspendAsOwner(t, ownerPoolForTest(t), f.tenantID, db.SuspensionSecurityHold)
	b := f.suspendedOwnerBrowser(t, db.SuspensionSecurityHold)
	refused := func() int { return len(f.trail(t, ActionTenantWriteRefused)) }

	closed := []struct {
		path string
		form url.Values
	}{
		{apiKeysHref, url.Values{"name": {"During Suspension"}}},
		{webhookAddressHref, url.Values{"address": {"https://other.example.com/"}}},
		{webhookRotateHref, url.Values{"confirm": {"yes"}}},
		{webhookPingHref, url.Values{}},
		{webhookResendHref, url.Values{"delivery": {uuid.NewString()}}},
	}
	for _, c := range closed {
		before := refused()
		if rec := b.do(http.MethodPost, c.path, c.form); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), suspensionHeading) {
			t.Errorf("POST %s for a suspended business: %d, want the suspension's 403", c.path, rec.Code)
		}
		if n := refused() - before; n != 1 {
			t.Errorf("POST %s left %d refusal row(s), want 1", c.path, n)
		}
	}
	if keys := f.keysOnFile(t); len(keys) != 3 {
		t.Fatalf("%d key(s) on file after a refused create, want 3", len(keys))
	}
	if url1, sealed1, _ := f.endpointRow(t); url1 != url0 || sealed1 != sealed0 || f.sender.count() != 0 {
		t.Fatal("a refused webhook write changed the address or the key, or sent something")
	}

	// Revoking ONE key: open, and it takes effect.
	victim := f.keysOnFile(t)[0]
	if rec := b.do(http.MethodPost, apiKeyRevokeHref, url.Values{"key_id": {victim.ID.String()}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("revoking a key while suspended answered %d, want 303", rec.Code)
	}
	revoked := 0
	for _, k := range f.keysOnFile(t) {
		if k.RevokedAt != nil {
			revoked++
			if k.ID != victim.ID {
				t.Errorf("revoking one key revoked %v too", k.ID)
			}
		}
	}
	if revoked != 1 {
		t.Fatalf("%d key(s) revoked after revoking one, want 1", revoked)
	}

	// Switching the webhook off: open; switching it back on: closed.
	if rec := b.do(http.MethodPost, webhookDisableHref, url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("switching the webhook off while suspended answered %d, want 303", rec.Code)
	}
	if _, _, on := f.endpointRow(t); on {
		t.Fatal("the webhook is still on after a 303 to switching it off")
	}
	before := refused()
	if rec := b.do(http.MethodPost, webhookEnableHref, url.Values{}); rec.Code != http.StatusForbidden {
		t.Errorf("switching the webhook on while suspended answered %d, want 403", rec.Code)
	}
	if _, _, on := f.endpointRow(t); on || refused()-before != 1 {
		t.Error("switching the webhook on while suspended took effect or left no refusal row")
	}

	// Revoking EVERY key: open, and it takes effect.
	if rec := b.do(http.MethodPost, apiKeyRevokeAllHref, url.Values{"confirm": {"yes"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("revoking every key while suspended answered %d, want 303", rec.Code)
	}
	for _, k := range f.keysOnFile(t) {
		if k.RevokedAt == nil {
			t.Errorf("key %v survived revoking every key", k.ID)
		}
	}
	if got, want := refused(), len(closed)+1; got != want {
		t.Errorf("%d refusal row(s) in all, want %d -- one per closed write, none for a defence", got, want)
	}
	for _, d := range f.trail(t, ActionTenantWriteRefused) {
		if d != `{"reason": "security_hold"}` {
			t.Errorf("a refusal row's detail is %s, want exactly the reason", d)
		}
	}
}

// TestPanelSuspensionDB_NoPanelReadWritesBusinessData is ADR 0025 §4's read half seen
// from the other side: the panel's GETs are not behind the suspension gate, so a GET
// that wrote business data would be a write a suspension does not stop. For a
// SUSPENDED business's signed-in admin, every GET AdminAuth.Mount serves (chi.Walk) and
// the query-string forms its handlers read (a person's card and its deactivation
// confirmation, a venue's card and its removal confirmation, the removal receipt, a
// month, a week, a review item, a policy version, a plaque) is requested, and every row
// of THIS tenant created or updated meanwhile is counted per table, by xmin -- the
// transaction ids handed out during the request. Allowed: the session's own
// last_used_at (admin_sessions, one row -- also the control that the measurement sees
// a write at all) and, on the two exports only, their own trail row (report.exported,
// billing.exported -- checked by action). Anything else is red.
//
// PART III: the scan sees INSERTs and UPDATEs (a new row version carries the writer's
// xmin), not DELETEs, and only tables with a tenant_id column (plus tenants by id).
func TestPanelSuspensionDB_NoPanelReadWritesBusinessData(t *testing.T) {
	p := newPanelHarness(t)
	owner := ownerPoolForTest(t)
	ctx := context.Background()
	venue, _, person := seedPanelPerson(t, p, p.tenantID, "Read Probe", "active")
	suspendAsOwner(t, owner, p.tenantID, db.SuspensionNonPayment)
	p.signIn(t)

	var tables []string
	rows, err := owner.Query(ctx, `SELECT quote_ident(c.table_name) FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = 'public' AND c.column_name = 'tenant_id' AND t.table_type = 'BASE TABLE' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	if len(tables) < 20 {
		t.Fatalf("only %d tenant tables found; the scan is not reading the schema", len(tables))
	}
	nextXID := func() int64 {
		var x int64
		if err := owner.QueryRow(ctx, `SELECT pg_snapshot_xmax(pg_current_snapshot())::text::bigint`).Scan(&x); err != nil {
			t.Fatal(err)
		}
		return x
	}
	written := func(from, to int64) map[string]int {
		out := map[string]int{}
		count := func(name, where string) {
			var n int
			q := `SELECT count(*) FROM ` + name + ` WHERE ` + where +
				` AND xmin::text::bigint >= ($2 % 4294967296) AND xmin::text::bigint < ($3 % 4294967296)`
			if err := owner.QueryRow(ctx, q, p.tenantID, from, to).Scan(&n); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if n > 0 {
				out[name] = n
			}
		}
		for _, tb := range tables {
			count(tb, "tenant_id = $1")
		}
		count("tenants", "id = $1")
		return out
	}

	walk := newAdminRouterWithActions(t, suspendedIdentity(uuid.New(), uuid.New(), uuid.New(), nil),
		&fakeTrail{}, newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{})
	var paths []string
	for _, mr := range walkedRoutes(t, walk) {
		if path, ok := strings.CutPrefix(mr, "GET "); ok {
			paths = append(paths, path)
		}
	}
	paths = append(paths,
		employeesHref+"?manage="+person.String(), employeesHref+"?manage="+person.String()+"&confirm=deactivate",
		manualEntryHref+"?id="+person.String(), locationsHref+"?venue="+venue.String(),
		locationsHref+"?venue="+venue.String()+"&confirm=remove", locationsHref+"?done=location-removed&id="+venue.String(),
		billingHref+"?month=2026-09", "/admin/anomalies?week=2026-10-12", reportsHref+"?week=2026-10-12",
		reviewHref+"?for="+uuid.NewString(), "/admin/policies/version?policy="+uuid.NewString()+"&no=1",
		locationsHref+"?plaque=04A1B2C3D4E5F6", billingCSVHref+"?month=2026-09", reportsCSVHref+"?week=2026-10-12",
	)
	exports := map[string]bool{billingCSVHref: true, reportsCSVHref: true}
	for _, path := range paths {
		from := nextXID()
		res, _ := p.get(t, path)
		got := written(from, nextXID())
		bare, _, _ := strings.Cut(path, "?")
		if bare == adminLoginPath || bare == "/admin/login/choose" {
			if len(got) != 0 {
				t.Errorf("GET %s wrote %v", path, got)
			}
			continue
		}
		if got["admin_sessions"] != 1 {
			t.Errorf("GET %s (%d): the session touch is not seen (%v) -- the measurement is blind", path, res.StatusCode, got)
		}
		delete(got, "admin_sessions")
		if exports[bare] && got["audit_log"] == 1 {
			var n int
			if err := owner.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action IN ($2, $3)
				AND xmin::text::bigint >= ($4 % 4294967296)`, p.tenantID, ActionReportExported, ActionBillingExported, from).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n == 1 {
				delete(got, "audit_log")
			}
		}
		if len(got) != 0 {
			t.Errorf("GET %s for a suspended business wrote %v; a panel read is not behind the suspension gate", path, got)
		}
	}
	if len(paths) < 25 {
		t.Fatalf("only %d GET(s) driven", len(paths))
	}
}
