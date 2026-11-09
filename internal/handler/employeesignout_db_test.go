package handler

// employeesignout_db_test.go -- T59's "Sign out their phones" over real HTTP and real Postgres.
//
// WHAT IS MEASURED HERE AND NOWHERE ELSE:
//
//	the panel's whole path: the card shows the count the database holds, the warning mints the
//	confirmation, the POST revokes every live session and writes ONE audit row with the actor
//	and the count, and a replayed confirmation (cookie re-printed by hand) revokes nothing and
//	writes nothing;
//	§4.5: another business's employee id, posted by a signed-in manager, revokes nothing;
//	§5 row 3: the signed-out phone's next tap -- the page AND the check-in POST -- goes to the
//	activation flow and writes no `transactions` row.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// seedLiveSessions gives an employee n live sessions with random hashes; the panel never sees a
// hash, it counts rows.
func seedLiveSessions(t *testing.T, p *panelHarness, tenantID, employee uuid.UUID, n int) {
	t.Helper()
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		for i := 0; i < n; i++ {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO sessions (tenant_id, employee_id, token_hash) VALUES ($1, $2, $3)`,
				tenantID, employee, hex.EncodeToString(b)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
}

// panelLiveSessions counts under RLS alone: no tenant_id in the WHERE, so a foreign row could only
// be counted if RLS let it through.
func panelLiveSessions(t *testing.T, p *panelHarness, tenantID, employee uuid.UUID) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM sessions WHERE employee_id = $1 AND revoked_at IS NULL`, employee).Scan(&n)
	}); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

func signOutAudit(t *testing.T, p *panelHarness, employee uuid.UUID) (detail string, actor *uuid.UUID) {
	t.Helper()
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT detail::text, actor_id FROM audit_log
			 WHERE tenant_id = $1 AND action = $2 AND target = $3 ORDER BY at DESC LIMIT 1`,
			p.tenantID, tenant.ActionEmployeePhonesSignedOut, employee.String()).Scan(&detail, &actor)
	}); err != nil {
		t.Fatalf("read the sign-out trail row: %v", err)
	}
	return detail, actor
}

func TestPanelSignOutDB_TheConfirmedPostRevokesEverySessionAndTrailsTheCount(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Maria Borg", "active")
	seedLiveSessions(t, p, p.tenantID, employee, 2)
	p.signIn(t)

	// The card offers the act with the count the database holds.
	_, card := p.get(t, employeesHref+"?manage="+employee.String())
	if !strings.Contains(card, "confirm=signout") || !strings.Contains(card, "2 signed in") {
		t.Fatalf("the card does not offer the sign-out beside its count of 2:\n%s", card)
	}
	_, page := p.get(t, employeesHref+"?manage="+employee.String()+"&confirm=signout")
	if !strings.Contains(page, "all 2 phones Maria Borg is signed in on") {
		t.Errorf("the warning does not say the count it is about to revoke")
	}
	token := confirmTokenIn(t, page)
	form := url.Values{"id": {employee.String()}, confirmField: {token}}

	res, _ := p.post(t, employeeSignOutHref, form)
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "done=signed-out") {
		t.Fatalf("the confirmed sign-out redirected to %q, want done=signed-out", loc)
	}
	if live := panelLiveSessions(t, p, p.tenantID, employee); live != 0 {
		t.Fatalf("%d live session(s) after the sign-out, want 0", live)
	}
	if rows := employeeAuditRows(t, p, tenant.ActionEmployeePhonesSignedOut, employee); rows != 1 {
		t.Fatalf("%d trail row(s), want 1", rows)
	}
	detail, actor := signOutAudit(t, p, employee)
	if !strings.Contains(detail, `"sessions_revoked": 2`) {
		t.Errorf("the trail detail is %s, want sessions_revoked 2", detail)
	}
	if actor == nil || *actor != p.adminID {
		t.Errorf("the trail's actor is %v, want the signed-in admin %s", actor, p.adminID)
	}

	// The banner, read back from the database: nobody is signed in now, and the next step.
	_, after := p.get(t, res.Header.Get("Location"))
	flat := strings.Join(strings.Fields(after), " ")
	for _, want := range []string{"No phone is signed in as Maria Borg now.", "they need a new activation link"} {
		if !strings.Contains(flat, want) {
			t.Errorf("the page after the sign-out does not say %q", want)
		}
	}
	if !strings.Contains(after, employeeInviteHref) {
		t.Error("the page after the sign-out offers no way to send a new activation link")
	}

	// THE SECOND DEFENCE, ON THE SERVER: the cookie re-printed by hand (what a script does),
	// the same value spent again. It must revoke nothing and write nothing.
	p.setCookie(t, adminConfirmCookieName, token)
	res, _ = p.post(t, employeeSignOutHref, form)
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "problem=no-phones") {
		t.Errorf("the replayed sign-out redirected to %q, want problem=no-phones", loc)
	}
	if rows := employeeAuditRows(t, p, tenant.ActionEmployeePhonesSignedOut, employee); rows != 1 {
		t.Errorf("trail rows went 1 -> %d on a replay; a press that revokes nothing is no act", rows)
	}
}

func TestPanelSignOutDB_AnotherBusinessesPhonesAreUntouched(t *testing.T) {
	p := newPanelHarness(t)
	_, _, own := seedPanelPerson(t, p, p.tenantID, "Alpha Person", "active")
	seedLiveSessions(t, p, p.tenantID, own, 1)
	p.signIn(t)

	other := uuid.New()
	if err := p.data.WithTenant(context.Background(), other, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Bravo Ltd', $2, 'bar', 'single')`, other, "VAT-"+other.String())
		return e
	}); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}
	_, _, foreign := seedPanelPerson(t, p, other, "Bravo Person", "active")
	seedLiveSessions(t, p, other, foreign, 2)

	// The warning is not minted for an id this business cannot read.
	_, page := p.get(t, employeesHref+"?manage="+foreign.String()+"&confirm=signout")
	if strings.Contains(page, confirmField) {
		t.Error("the panel minted a sign-out confirmation for ANOTHER business's employee id")
	}
	// A real confirmation for the own person, posted with the foreign id: the binding refuses it.
	_, ownPage := p.get(t, employeesHref+"?manage="+own.String()+"&confirm=signout")
	res, _ := p.post(t, employeeSignOutHref, url.Values{
		"id": {foreign.String()}, confirmField: {confirmTokenIn(t, ownPage)},
	})
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "problem=") {
		t.Errorf("a sign-out for a foreign id redirected to %q with no problem word", loc)
	}
	// And the domain itself, past the gate, with this tenant and the foreign id.
	trail, err := audit.New(p.data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	staff, err := tenant.NewStaff(p.data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	if _, err := staff.SignOutPhones(context.Background(), tenant.SignOutCommand{
		TenantID: p.tenantID, EmployeeID: foreign, ActorID: p.adminID,
	}); err == nil || !strings.Contains(err.Error(), "no such employee") {
		t.Errorf("the domain answered %v for another business's person, want ErrUnknownEmployee", err)
	}
	if live := panelLiveSessions(t, p, other, foreign); live != 2 {
		t.Errorf("the other business's person has %d live session(s), want 2", live)
	}
	if live := panelLiveSessions(t, p, p.tenantID, own); live != 1 {
		t.Errorf("the own person lost a session to a refused request (%d live)", live)
	}
}

// TestTapDB_ASignedOutPhoneGoesToActivationAndWritesNothing is the act's consequence, end to end:
// a phone that tapped once is signed out by the panel's domain, and its next tap -- the page and
// the check-in POST, NFC and QR -- reaches §5 row 3: the activation flow, no record, no counter spent.
func TestTapDB_ASignedOutPhoneGoesToActivationAndWritesNothing(t *testing.T) {
	h := newTapHarness(t)
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)

	// CONTROL: before the sign-out the phone records time.
	before := h.countFor(t, emp)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("the control tap answered %d, want 200", w.Code)
	}
	if h.countFor(t, emp) != before+1 {
		t.Fatal("the control tap wrote no record; the measurement below would prove nothing")
	}
	// A context signed for this session, minted while it was live: what an open tap page holds.
	sid := h.sessionIDOf(t, cookie)
	held, err := h.tap.contexts.mint(h.nfcContext(uint32(h.startCtr)+2), sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	staff, err := tenant.NewStaff(h.data, h.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	n, err := staff.SignOutPhones(context.Background(), tenant.SignOutCommand{
		TenantID: h.tenantID, EmployeeID: emp, ActorID: uuid.New(),
	})
	if err != nil || n != 1 {
		t.Fatalf("SignOutPhones answered (%d, %v), want (1, nil)", n, err)
	}

	records, ctr := h.countFor(t, emp), h.lastCtr(t, h.tagUID)
	logs := h.captureTapLog(t)
	for _, target := range []string{nfcPage(t, h.tagUID, h.lastCtr(t, h.tagUID)), qrURLFor(h.tagUID)} {
		w := h.get(t, target, cookie)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != activationSignedOut {
			t.Errorf("GET %s with a signed-out phone answered %d %q, want 303 %q",
				target, w.Code, w.Header().Get("Location"), activationSignedOut)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(url.Values{"ctx": {held}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != activationSignedOut {
		t.Errorf("POST /api/checkin with a signed-out phone answered %d %q, want 303 %q",
			w.Code, w.Header().Get("Location"), activationSignedOut)
	}
	if got := h.countFor(t, emp); got != records {
		t.Errorf("records went %d -> %d: a signed-out phone must write nothing (§5 row 3)", records, got)
	}
	if got := h.lastCtr(t, h.tagUID); got != ctr {
		t.Errorf("last_ctr moved %d -> %d on a signed-out phone's tap", ctr, got)
	}
	if got := h.auditCount(t, tenant.ActionEmployeePhonesSignedOut); got != 1 {
		t.Errorf("%d sign-out trail row(s), want 1", got)
	}

	// NOT NO TRACE (T59 security review, DÜŞÜK-1): one WARN per signed-out tap, carrying the tenant,
	// the employee and the channel and nothing else -- no plaque uid, GPS, cookie or hash.
	var channels []string
	for _, line := range logLines(t, logs) {
		msg, _ := line["msg"].(string)
		if !strings.Contains(msg, "a signed-out phone tapped") {
			continue
		}
		keys := make([]string, 0, len(line))
		for k := range line {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if strings.Join(keys, ",") != "channel,employee_id,level,msg,tenant_id,time" {
			t.Errorf("the signed-out tap's log line carries %v, want exactly tenant_id, employee_id and channel", keys)
		}
		if line["level"] != "WARN" || line["tenant_id"] != h.tenantID.String() || line["employee_id"] != emp.String() {
			t.Errorf("the signed-out tap's log line is %v", line)
		}
		channels = append(channels, fmt.Sprint(line["channel"]))
	}
	if strings.Join(channels, ",") != "nfc,qr,unverified" {
		t.Errorf("signed-out tap log lines name the channels %v, want nfc, qr, unverified (GET NFC, GET QR, POST)", channels)
	}
	for _, never := range []string{h.tagUID, cookie.Value, held} {
		if strings.Contains(logs.String(), never) {
			t.Error("the tap log carries the plaque uid, the cookie or the signed context")
		}
	}
}
