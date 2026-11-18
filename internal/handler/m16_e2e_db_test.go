package handler

// m16_e2e_db_test.go -- M16's Faz A closing scenario (M16-10, ADR 0045): the signed app surface end to
// end, with no app, against a real Postgres. The fake client is appHarness's appPhone (a fresh P-256
// key, the canonical texts from appdevice's exported builders -- the ones test/fixtures/
// appdevice_vectors.json pins -- and signatures from crypto/ecdsa); every request goes through the
// real /app/v1 router with the REAL appdevice verifier, invitation manager, sun verifier (genuine
// SUN reads of one AN12196-pinned plaque), checkin service and policy set.
//
// WHY NOT THE VECTORS' OWN KEY. The vectors pin the TEXT the client signs (TestCanonicalText_
// KnownAnswers, TestCanonicalReadText_KnownAnswers) and the signature format (TestVerifySignature_
// KnownAnswers); their nonces are fixed, and a live request's nonce is the server's. Their key is
// fixed too, and app_devices.key_digest is globally UNIQUE on rows nobody may delete: the second run
// of a test activating it would be answered new_key_required. So the client signs the pinned texts
// with a fresh key per phone, as a phone does on every activation.
//
// TWO ROUTERS, ONE BUSINESS (m15_e2e_db_test.go's shape). The tap router (appHarness: /t,
// /api/checkin, /activate, /app/v1) and the panel (panelHarness: /admin/...) are separate harnesses;
// the panel is signed in as an owner written into the app harness's business, so the manager's
// sign-out, deactivation and manual record land on the same people and the same rows the app and the
// browser write.
//
// THE COUNT IS THE BUSINESS'S, AT EVERY STEP. The business is fresh, so `transactions` for it starts
// at 0 and every step names the exact number it must leave behind: a step that writes nothing (the
// activation, a signed-out device, the panel's acts) is measured as "no change", not assumed.
//
// The harness's debounce is 120 s, not the shipped 60 s (tap_db_test.go's harnessDebounce says why);
// the repeat below is sent at once, so it is inside either window.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/checkin"
)

// activateApp walks lookup -> bind -> complete for code with a fresh phone and returns the phone,
// active, holding its device id.
func (h *appHarness) activateApp(t *testing.T, code string) *appPhone {
	t.Helper()
	look := h.unsigned(t, AppActivateLookupPath, jsonBody(t, appLookupRequest{Code: code}))
	if look.Code != http.StatusOK {
		t.Fatalf("lookup = %d %s", look.Code, look.Body.String())
	}
	phone := newAppPhone(t)
	b := h.bind(t, phone, code)
	w := h.serve(h.completeRequest(t, phone, phone, b, code, h.nextSUN(t)))
	if w.Code != http.StatusOK {
		t.Fatalf("complete = %d %s", w.Code, w.Body.String())
	}
	var done appCompleteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &done); err != nil {
		t.Fatal(err)
	}
	phone.id = uuid.MustParse(done.DeviceID)
	return phone
}

// TestM16DB_TheAppSurfaceSignedEndToEnd is M16-10's first criterion, one business, in order:
//
//  1. activation (lookup -> bind -> complete on the first genuine NFC read): no row;
//  2. nonce -> tap/start -> tap: ONE row, verdict ok, channel nfc, client 'app';
//  3. the same person again at once: §5 row 5, a RECORDED ignored, client 'app';
//  4. the panel's "Sign out their phones": the device is revoked, and the held tap (a context and a
//     nonce taken before), a new tap/start, a nonce and /me are each 401 with an empty body -- no
//     row (§5 row 3's twin), and the unattributed warning line;
//  5. a SEPARATE person activated through the app, deactivated through the panel: the device is NOT
//     revoked, and their tap is §5 row 4 -- reject, sys:employee-deactivated, RECORDED, client
//     'app', one security alert;
//  6. the browser: GET /t with a genuine read, POST /api/checkin with the page's context -- one row,
//     client 'browser';
//  7. the panel's manual record: one row, channel manual, entered by the owner, client IS NULL
//     (M16-04's proof without a CHECK that forces a tap's client).
func TestM16DB_TheAppSurfaceSignedEndToEnd(t *testing.T) {
	h := newAppHarness(t)
	p := newPanelHarnessOwning(t, h.tenantID, panelHarnessOptions{})
	rows := func(step string, want int) {
		t.Helper()
		got := h.transactionCount(t)
		t.Logf("after %s: transactions = %d", step, got)
		if got != want {
			t.Fatalf("after %s: the business holds %d transactions rows, want %d", step, got, want)
		}
	}
	rows("the fixture", 0)

	// 1. Activation.
	rita, code := h.invitedEmployee(t, "invited")
	phone := h.activateApp(t, code)
	if h.employeeStatus(t, rita) != "active" || h.liveDevices(t, rita) != 1 {
		t.Fatalf("after the activation: status %q, %d live devices; want active and 1", h.employeeStatus(t, rita), h.liveDevices(t, rita))
	}
	rows("the activation (ADR 0026: the activating touch is not attendance)", 0)

	// 2. nonce -> tap/start -> tap.
	nonce := h.nonce(t, phone)
	start, w := h.tapStart(t, phone, h.nextSUN(t))
	if w.Code != http.StatusOK || start.EmployeeName != "Rita Vella" || start.Context == "" {
		t.Fatalf("tap/start = %d %s", w.Code, w.Body.String())
	}
	rows("tap/start (GET /t's twin moves nothing)", 0)
	w = h.serve(h.signedRequest(t, phone, phone.id.String(), nonce, AppTapPath,
		jsonBody(t, map[string]any{"context": start.Context, "want": "in"})))
	var res appTapResponse
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &res) != nil || res.Outcome != "recorded" || res.Verdict != "ok" {
		t.Fatalf("the app's tap = %d %s, want a recorded ok", w.Code, w.Body.String())
	}
	rows("the app's tap", 1)
	first := h.lastRecord(t, rita)
	if first.Channel != "nfc" || first.Verdict != "ok" || first.Type == nil || *first.Type != "in" {
		t.Fatalf("the app's row: channel %q, verdict %q, type %v; want nfc, ok, in", first.Channel, first.Verdict, first.Type)
	}
	if c := h.clientOf(t, first.ID); c == nil || *c != "app" {
		t.Fatalf("the app's row has client %s, want 'app'", clientText(c))
	}

	// 3. The repeat inside the window.
	again, w := h.tap(t, phone, nil)
	if w.Code != http.StatusOK || again.Verdict != "ignored" {
		t.Fatalf("the repeat = %d %+v, want a recorded ignored (§5 row 5)", w.Code, again)
	}
	rows("the repeat (§5 row 5 is recorded)", 2)
	if rep := h.lastRecord(t, rita); rep.ID == first.ID || rep.Verdict != "ignored" || deref(rep.MatchedSid) != "sys:person-debounce" {
		t.Fatalf("the repeat's row: %+v", rep)
	} else if c := h.clientOf(t, rep.ID); c == nil || *c != "app" {
		t.Fatalf("the repeat's row has client %s, want 'app'", clientText(c))
	}

	// 4. The panel signs Rita's phones out. The app was mid-press: it holds a tap context and a nonce.
	held, w := h.tapStart(t, phone, h.nextSUN(t))
	if w.Code != http.StatusOK {
		t.Fatalf("PREMISE: tap/start before the sign-out = %d %s", w.Code, w.Body.String())
	}
	heldNonce := h.nonce(t, phone)
	_, page := p.get(t, employeesHref+"?manage="+rita.String()+"&confirm=signout")
	out, _ := p.post(t, employeeSignOutHref, url.Values{"id": {rita.String()}, confirmField: {confirmTokenIn(t, page)}})
	if loc := out.Header.Get("Location"); !strings.Contains(loc, "done=signed-out") {
		t.Fatalf("the panel's sign-out redirected to %q, want done=signed-out", loc)
	}
	if !appDeviceRevoked(t, h.data, h.tenantID, phone.id) {
		t.Fatal("the panel's sign-out left the app device live")
	}
	if detail, _ := signOutAudit(t, p, rita); !strings.Contains(detail, `"app_devices_revoked": 1`) {
		t.Errorf("the sign-out's trail row is %s, want app_devices_revoked 1", detail)
	}
	rows("the panel's sign-out", 2)
	mark := len(h.logs.String())
	_, startW := h.tapStart(t, phone, h.nextSUN(t))
	for name, rw := range map[string]*httptest.ResponseRecorder{
		"the held tap": h.serve(h.signedRequest(t, phone, phone.id.String(), heldNonce, AppTapPath,
			jsonBody(t, map[string]any{"context": held.Context}))),
		"a new tap/start": startW,
		"a nonce":         h.read(t, phone, http.MethodPost, AppNoncePath, ""),
		"/me":             h.read(t, phone, http.MethodGet, AppMePath, ""),
	} {
		if rw.Code != http.StatusUnauthorized || rw.Body.Len() != 0 {
			t.Fatalf("%s from the signed-out device = %d %q, want 401 with an empty body", name, rw.Code, rw.Body.String())
		}
	}
	rows("the signed-out device's four requests (§5 row 3's twin)", 2)
	if !strings.Contains(h.logs.String()[mark:], signedOutAppTap) {
		t.Error("no warning line for the signed-out device's tap")
	}

	// 5. A separate person: activated through the app, deactivated through the panel, taps.
	joe, codeJoe := h.invitedEmployee(t, "invited")
	joePhone := h.activateApp(t, codeJoe)
	rows("the second activation", 2)
	if res := harnessDeactivate(t, p, joe); !strings.Contains(res.Header.Get("Location"), "done=deactivated") {
		t.Fatalf("the panel's deactivation redirected to %q, want done=deactivated", res.Header.Get("Location"))
	}
	if h.employeeStatus(t, joe) != "deactivated" {
		t.Fatalf("after the panel's deactivation the person is %q", h.employeeStatus(t, joe))
	}
	if appDeviceRevoked(t, h.data, h.tenantID, joePhone.id) {
		t.Fatal("the deactivation revoked the app device: the tap would take row 3's recordless path (§4.6)")
	}
	rows("the panel's deactivation", 2)
	alerts := h.auditCount(t, checkin.ActionTapSecurityAlert)
	gone, w := h.tap(t, joePhone, nil)
	if w.Code != http.StatusOK || gone.Verdict != "reject" {
		t.Fatalf("the deactivated person's app tap = %d %+v, want a recorded reject (§5 row 4)", w.Code, gone)
	}
	rows("the deactivated person's tap (§5 row 4 is recorded)", 3)
	rejected := h.lastRecord(t, joe)
	if rejected.Verdict != "reject" || deref(rejected.MatchedSid) != "sys:employee-deactivated" {
		t.Fatalf("the row 4 row: verdict %q, sid %q; want reject, sys:employee-deactivated", rejected.Verdict, deref(rejected.MatchedSid))
	}
	if c := h.clientOf(t, rejected.ID); c == nil || *c != "app" {
		t.Fatalf("the row 4 row has client %s, want 'app'", clientText(c))
	}
	if got := h.auditCount(t, checkin.ActionTapSecurityAlert) - alerts; got != 1 {
		t.Fatalf("%d security alerts for the deactivated tap, want 1", got)
	}

	// 6. The browser: a genuine read through GET /t, the page's context to POST /api/checkin.
	cookie := h.sessionCookieFor(t) // the harness's own active person, Maria Borg
	signed := h.signedContextFrom(t, "/t?"+h.nextSUN(t), cookie)
	rows("GET /t", 3)
	if bw := h.postSigned(t, signed, cookie, onSiteAddr, nil); bw.Code != http.StatusOK {
		t.Fatalf("the browser's POST /api/checkin = %d", bw.Code)
	}
	rows("the browser's tap", 4)
	browser := h.lastRecord(t, h.employeeID)
	if browser.Verdict != "ok" || browser.Channel != "nfc" {
		t.Fatalf("the browser's row: verdict %q, channel %q; want ok, nfc", browser.Verdict, browser.Channel)
	}
	if c := h.clientOf(t, browser.ID); c == nil || *c != "browser" {
		t.Fatalf("the browser's row has client %s, want 'browser'", clientText(c))
	}

	// 7. The manager's manual record, through the panel's two steps.
	sam := h.newEmployee(t, "active")
	malta, err := time.LoadLocation("Europe/Malta") // the business's zone (the migration default)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Now().In(malta).AddDate(0, 0, -1).Format("2006-01-02")
	form := url.Values{"id": {sam.String()}, "direction": {"in"}, "date": {day}, "time": {"09:00"}}
	review, reviewPage := p.post(t, manualEntryHref, form)
	if review.StatusCode != http.StatusOK {
		t.Fatalf("the manual record's review step = %d", review.StatusCode)
	}
	rows("the manual record's review step", 4)
	form.Set(confirmField, confirmTokenIn(t, reviewPage))
	if res, _ := p.post(t, manualRecordHref, form); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("the manual record = %d, want 303", res.StatusCode)
	}
	rows("the manual record", 5)
	manualRow := h.lastRecord(t, sam)
	if manualRow.Channel != "manual" {
		t.Fatalf("the manual row's channel is %q", manualRow.Channel)
	}
	if by := enteredByOf(t, h, manualRow.ID); by == nil || *by != p.adminID {
		t.Fatalf("the manual row was entered by %v, want the signed-in owner %s", by, p.adminID)
	}
	if c := h.clientOf(t, manualRow.ID); c != nil {
		t.Fatalf("the manual row has client %s, want NULL", clientText(c))
	}
}

// enteredByOf reads transactions.entered_by of one row, as the tenant (RLS on).
func enteredByOf(t *testing.T, h *appHarness, id uuid.UUID) *uuid.UUID {
	t.Helper()
	var by *uuid.UUID
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT entered_by FROM transactions WHERE tenant_id = $1 AND id = $2`, h.tenantID, id).Scan(&by)
	}); err != nil {
		t.Fatalf("read entered_by: %v", err)
	}
	return by
}
