package handler

// appdevicesignout_db_test.go -- M16-09 over real HTTP and real Postgres: the panel's "Sign out
// their phones" closes the native app's devices too (ADR 0045 §2.3, Q40 (a)), and the two §5
// consequences the card names hold for the app as they hold for the browser:
//
//	§5 row 3's twin: a signed-out device's next signed request is refused as revoked (401) and
//	nothing reaches the write path -- `transactions` does not move;
//	§5 row 4: a DEACTIVATED person's device is NOT revoked (deactivation revokes nothing, the
//	2026-08-07 decision), so it still verifies, and the tap it carries is rejected, RECORDED and
//	alerted on by employees.status + sys:employee-deactivated -- the same outcome as the
//	browser's twin, through the same checkin.Service.Record.
//
// The /app/v1 endpoint itself is M16-07's; these drive the verifier and the service directly, the
// way mocklocation_db_test.go does. Nothing here prints a key, a signature or a nonce (§7).

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/appdevice"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/store"
)

// testAppDevice is one activated device: its id and the key only the phone would hold.
type testAppDevice struct {
	id  uuid.UUID
	key *ecdsa.PrivateKey
}

// registerAppDevice gives one person a device through the product's query.
func registerAppDevice(t *testing.T, d *db.DB, tenantID, employeeID uuid.UUID) testAppDevice {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pub, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("public key bytes: %v", err)
	}
	var id uuid.UUID
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).CreateAppDevice(ctx, store.CreateAppDeviceParams{
			TenantID: tenantID, EmployeeID: employeeID, Platform: "ios", PublicKey: pub,
		})
		id = row.ID
		return e
	}); err != nil {
		t.Fatalf("create app device: %v", err)
	}
	return testAppDevice{id: id, key: k}
}

// appDeviceRevoked reads revoked_at IS NOT NULL under RLS alone (no tenant_id in the WHERE).
func appDeviceRevoked(t *testing.T, d *db.DB, tenantID, deviceID uuid.UUID) bool {
	t.Helper()
	var revoked bool
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM app_devices WHERE id = $1`, deviceID).Scan(&revoked)
	}); err != nil {
		t.Fatalf("read the device: %v", err)
	}
	return revoked
}

// signedAppRequest issues a nonce for the device and signs a tap request over it.
func signedAppRequest(t *testing.T, v *appdevice.Verifier, dev testAppDevice) appdevice.SignedRequest {
	t.Helper()
	issued, err := v.IssueNonce(context.Background(), dev.id)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	return signOver(t, dev, issued.Nonce.Wire())
}

func signOver(t *testing.T, dev testAppDevice, nonceWire string) appdevice.SignedRequest {
	t.Helper()
	const path = "/app/v1/tap"
	body := []byte(`{"want":"in"}`)
	text, err := appdevice.CanonicalText(http.MethodPost, path, body, nonceWire, dev.id.String())
	if err != nil {
		t.Fatalf("CanonicalText: %v", err)
	}
	digest := sha256.Sum256(text)
	sig, err := ecdsa.SignASN1(rand.Reader, dev.key, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	req, err := appdevice.NewSignedRequest(http.MethodPost, path, body, dev.id.String(), nonceWire,
		base64.RawURLEncoding.EncodeToString(sig))
	if err != nil {
		t.Fatalf("NewSignedRequest: %v", err)
	}
	return req
}

func appVerifier(t *testing.T, d *db.DB) *appdevice.Verifier {
	t.Helper()
	v, err := appdevice.New(d)
	if err != nil {
		t.Fatalf("appdevice.New: %v", err)
	}
	return v
}

// TestPanelSignOutDB_AnAppOnlyPersonIsOfferedTheActAndSignedOut: a person signed in ONLY in the
// app (no browser session -- with one credential per person that is every app user) is offered
// the act with the app's count, the confirmed POST revokes the device, and the trail row carries
// both counts.
func TestPanelSignOutDB_AnAppOnlyPersonIsOfferedTheActAndSignedOut(t *testing.T) {
	p := newPanelHarness(t)
	_, _, employee := seedPanelPerson(t, p, p.tenantID, "Maria Borg", "active")
	dev := registerAppDevice(t, p.data, p.tenantID, employee)
	p.signIn(t)

	_, card := p.get(t, employeesHref+"?manage="+employee.String())
	if !strings.Contains(card, "confirm=signout") || !strings.Contains(card, "In the app: 1 phone signed in") {
		t.Fatalf("the card does not offer the sign-out beside the app's count:\n%s", card)
	}
	_, page := p.get(t, employeesHref+"?manage="+employee.String()+"&confirm=signout")
	if !strings.Contains(page, "This signs out the app on the phone Maria Borg uses.") {
		t.Errorf("the warning does not say it signs the app out")
	}
	form := url.Values{"id": {employee.String()}, confirmField: {confirmTokenIn(t, page)}}
	res, _ := p.post(t, employeeSignOutHref, form)
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "done=signed-out") {
		t.Fatalf("the confirmed sign-out redirected to %q, want done=signed-out", loc)
	}
	if !appDeviceRevoked(t, p.data, p.tenantID, dev.id) {
		t.Fatal("the app device is still live after the panel's sign-out")
	}
	detail, actor := signOutAudit(t, p, employee)
	if !strings.Contains(detail, `"sessions_revoked": 0`) || !strings.Contains(detail, `"app_devices_revoked": 1`) {
		t.Errorf("the trail detail is %s, want sessions_revoked 0 and app_devices_revoked 1", detail)
	}
	if actor == nil || *actor != p.adminID {
		t.Errorf("the trail's actor is %v, want the signed-in admin %s", actor, p.adminID)
	}
	// The banner is shown beside a MEASURED zero of both.
	_, after := p.get(t, res.Header.Get("Location"))
	if flat := strings.Join(strings.Fields(after), " "); !strings.Contains(flat, "No phone is signed in as Maria Borg now, in the browser or in the app.") {
		t.Error("the page after the sign-out does not say nobody is signed in, in either")
	}
}

// §4.5 at the panel: a manager of business A, with a confirmation for their own person, cannot
// sign out business B's person's app devices -- and B's device stays live.
func TestPanelSignOutDB_AnotherBusinessesAppDevicesAreUntouched(t *testing.T) {
	p := newPanelHarness(t)
	_, _, own := seedPanelPerson(t, p, p.tenantID, "Alpha Person", "active")
	ownDev := registerAppDevice(t, p.data, p.tenantID, own)
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
	foreignDev := registerAppDevice(t, p.data, other, foreign)

	_, page := p.get(t, employeesHref+"?manage="+foreign.String()+"&confirm=signout")
	if strings.Contains(page, confirmField) {
		t.Error("the panel minted a sign-out confirmation for ANOTHER business's employee id")
	}
	_, ownPage := p.get(t, employeesHref+"?manage="+own.String()+"&confirm=signout")
	res, _ := p.post(t, employeeSignOutHref, url.Values{
		"id": {foreign.String()}, confirmField: {confirmTokenIn(t, ownPage)},
	})
	if loc := res.Header.Get("Location"); !strings.Contains(loc, "problem=") {
		t.Errorf("a sign-out for a foreign id redirected to %q with no problem word", loc)
	}
	if appDeviceRevoked(t, p.data, other, foreignDev.id) {
		t.Error("another business's person's app device was revoked")
	}
	if appDeviceRevoked(t, p.data, p.tenantID, ownDev.id) {
		t.Error("the own person's device was revoked by a refused request")
	}
}

// TestAppDeviceDB_ASignedOutDeviceIsRefusedAndRecordsNothing: §5 row 3's twin. A device that
// verified once is signed out by the panel's domain; its next request -- including one over a
// nonce issued BEFORE the sign-out -- is ErrRevokedDevice, answered 401, and no identity reaches
// checkin.Service.Record, so `transactions` does not move.
func TestAppDeviceDB_ASignedOutDeviceIsRefusedAndRecordsNothing(t *testing.T) {
	h := newTapHarness(t)
	v := appVerifier(t, h.data)
	emp := h.newEmployee(t, "active")
	dev := registerAppDevice(t, h.data, h.tenantID, emp)

	// CONTROL: before the sign-out the device verifies as the person.
	got, err := v.Verify(context.Background(), signedAppRequest(t, v, dev))
	if err != nil || got.EmployeeID != emp {
		t.Fatalf("the control request verified as (%+v, %v), want the person", got, err)
	}
	held := signedAppRequest(t, v, dev) // what an app mid-request holds

	staff, err := tenant.NewStaff(h.data, h.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	n, err := staff.SignOutPhones(context.Background(), tenant.SignOutCommand{
		TenantID: h.tenantID, EmployeeID: emp, ActorID: uuid.New(),
	})
	if err != nil || n != (tenant.SignedOut{AppDevices: 1}) {
		t.Fatalf("SignOutPhones answered (%+v, %v), want (1 app device, nil)", n, err)
	}

	records := h.countFor(t, emp)
	_, err = v.Verify(context.Background(), held)
	if !errors.Is(err, appdevice.ErrRevokedDevice) || appdevice.StatusOf(err) != http.StatusUnauthorized {
		t.Fatalf("a signed-out device's request answered %v (status %d), want ErrRevokedDevice / 401",
			err, appdevice.StatusOf(err))
	}
	if _, err := v.IssueNonce(context.Background(), dev.id); !errors.Is(err, appdevice.ErrRevokedDevice) {
		t.Errorf("a signed-out device was answered %v for a nonce, want ErrRevokedDevice", err)
	}
	if got := h.countFor(t, emp); got != records {
		t.Errorf("records went %d -> %d: a signed-out device must write nothing (§5 row 3)", records, got)
	}
}

// TestAppDeviceDB_ADeactivatedPersonsAppTapIsRejectedAndRecorded: §5 row 4 for the app. The
// person is deactivated through the product's Deactivate; the device is NOT revoked, still
// verifies, and the tap it carries goes to the same Record as the browser's -- rejected, RECORDED
// with client 'app', alerted on -- and the browser twin, through the same service, decides the
// same way.
func TestAppDeviceDB_ADeactivatedPersonsAppTapIsRejectedAndRecorded(t *testing.T) {
	h := newTapHarness(t)
	svc := h.checkinService(t)
	v := appVerifier(t, h.data)
	emp := h.newEmployee(t, "active")
	dev := registerAppDevice(t, h.data, h.tenantID, emp)

	staff, err := tenant.NewStaff(h.data, h.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	if _, err := staff.Deactivate(context.Background(), tenant.DeactivateCommand{
		TenantID: h.tenantID, EmployeeID: emp, ActorID: uuid.New(),
	}); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if appDeviceRevoked(t, h.data, h.tenantID, dev.id) {
		t.Fatal("Deactivate revoked the app device: its tap would take row 3's recordless path (§4.6)")
	}
	who, err := v.Verify(context.Background(), signedAppRequest(t, v, dev))
	if err != nil || who.EmployeeID != emp || who.TenantID != h.tenantID {
		t.Fatalf("the deactivated person's device verified as (%+v, %v), want their identity", who, err)
	}

	for _, client := range []tap.Client{tap.ClientApp, tap.ClientBrowser} {
		before := h.countFor(t, emp)
		alertsBefore := h.auditCount(t, "tap.security_alert")
		req := checkinRequestForTest(h, tap.ChannelQR, nil)
		req.SessionTenantID, req.EmployeeID, req.Client = who.TenantID, who.EmployeeID, client
		if _, err := svc.Record(context.Background(), req); err != nil {
			t.Fatalf("%s: Record: %v", client, err)
		}
		if after := h.countFor(t, emp); after != before+1 {
			t.Fatalf("%s: rows went %d -> %d, want +1 -- a deactivated attempt is RECORDED", client, before, after)
		}
		rec := h.lastRecord(t, emp)
		if rec.Verdict != "reject" || deref(rec.MatchedSid) != "sys:employee-deactivated" {
			t.Fatalf("%s: verdict/sid = %q/%q, want reject/sys:employee-deactivated", client, rec.Verdict, deref(rec.MatchedSid))
		}
		if got := h.auditCount(t, "tap.security_alert") - alertsBefore; got != 1 {
			t.Fatalf("%s: security alerts = %d, want 1", client, got)
		}
		if c := h.clientOf(t, rec.ID); c == nil || *c != string(client) {
			t.Fatalf("%s: client column = %s", client, clientText(c))
		}
	}
	// And the device is still live after both: nothing on this path revokes.
	if appDeviceRevoked(t, h.data, h.tenantID, dev.id) {
		t.Error("the rejected tap revoked the device")
	}
}
