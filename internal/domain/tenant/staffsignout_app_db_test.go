package tenant

// staffsignout_app_db_test.go -- M16-09 against real Postgres: "Sign out their phones" closes the
// native app's devices too (ADR 0045 §2.3, Q40 (a)), in the same transaction as the browser
// sessions and the trail row; deactivation closes nothing (the 2026-08-07 decision, the app's
// twin); a foreign person's devices are untouched; the revocation is one way; a deadlock is
// retried rather than shown to the manager; and a sign-out racing the app's own requests leaves
// nothing spendable behind it.
//
// Nothing here prints a key, a signature, a nonce or its hash (§4.7, §7): values are compared.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/appdevice"
	"github.com/atknatk/tappa/internal/store"
)

// appPhone is one activated device: its id and the private key only the phone would hold.
type appPhone struct {
	id  uuid.UUID
	key *ecdsa.PrivateKey
}

// addAppDevice registers a fresh P-256 key for one person through the product's query.
func (f *staffFixture) addAppDevice(t *testing.T, tenantID, employeeID uuid.UUID) appPhone {
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
	err = f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).CreateAppDevice(ctx, store.CreateAppDeviceParams{
			TenantID: tenantID, EmployeeID: employeeID, Platform: "android", PublicKey: pub,
		})
		id = row.ID
		return e
	})
	if err != nil {
		t.Fatalf("create app device: %v", err)
	}
	return appPhone{id: id, key: k}
}

// liveAppDevices counts with no tenant_id beyond RLS's own, as liveSessions does.
func (f *staffFixture) liveAppDevices(t *testing.T, tenantID, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM app_devices WHERE employee_id = $1 AND revoked_at IS NULL`,
			employeeID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count live app devices: %v", err)
	}
	return n
}

// deviceRevoked reads one device's revoked_at IS NOT NULL.
func (f *staffFixture) deviceRevoked(t *testing.T, tenantID, deviceID uuid.UUID) bool {
	t.Helper()
	var revoked bool
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT revoked_at IS NOT NULL FROM app_devices WHERE tenant_id = $1 AND id = $2`,
			tenantID, deviceID).Scan(&revoked)
	})
	if err != nil {
		t.Fatalf("read the device: %v", err)
	}
	return revoked
}

// deviceNonces counts every nonce row a device holds, live or dead.
func (f *staffFixture) deviceNonces(t *testing.T, tenantID, deviceID uuid.UUID) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM app_request_nonces WHERE tenant_id = $1 AND device_id = $2`,
			tenantID, deviceID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count nonces: %v", err)
	}
	return n
}

func (f *staffFixture) verifier(t *testing.T) *appdevice.Verifier {
	t.Helper()
	v, err := appdevice.New(f.data)
	if err != nil {
		t.Fatalf("appdevice.New: %v", err)
	}
	return v
}

// signedTap is p's signed request over the nonce, as the app would send it.
func signedTap(t *testing.T, p appPhone, n appdevice.Nonce) appdevice.SignedRequest {
	t.Helper()
	const path = "/app/v1/tap"
	body := []byte(`{"want":"in"}`)
	text, err := appdevice.CanonicalText(http.MethodPost, path, body, n.Wire(), p.id.String())
	if err != nil {
		t.Fatalf("CanonicalText: %v", err)
	}
	digest := sha256.Sum256(text)
	sig, err := ecdsa.SignASN1(rand.Reader, p.key, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	req, err := appdevice.NewSignedRequest(http.MethodPost, path, body, p.id.String(), n.Wire(),
		base64.RawURLEncoding.EncodeToString(sig))
	if err != nil {
		t.Fatalf("NewSignedRequest: %v", err)
	}
	return req
}

func issueNonce(t *testing.T, v *appdevice.Verifier, p appPhone) appdevice.Nonce {
	t.Helper()
	n, err := v.IssueNonce(context.Background(), p.id)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	return n.Nonce
}

// TestStaffDB_SignOutClosesTheAppDevicesInTheSameAct: sessions and devices go together, the
// devices' live nonces go with them (RevokeAppDevicesForEmployee's one statement), and the trail
// row carries both counts and nothing else. Afterwards the app is §5 row 3's twin: the revoked
// device is refused before its signature is read, and is given no nonce.
func TestStaffDB_SignOutClosesTheAppDevicesInTheSameAct(t *testing.T) {
	f := newStaffFixture(t)
	v := f.verifier(t)
	f.seedSessions(t, f.tenantID, f.employeeID, 1)
	a, b := f.addAppDevice(t, f.tenantID, f.employeeID), f.addAppDevice(t, f.tenantID, f.employeeID)
	pending := issueNonce(t, v, a) // issued before the sign-out, sent after it
	issueNonce(t, v, b)

	n, err := f.staff.SignOutPhones(context.Background(), f.signOut())
	if err != nil {
		t.Fatalf("SignOutPhones: %v", err)
	}
	if n != (SignedOut{Sessions: 1, AppDevices: 2}) {
		t.Fatalf("revoked %+v, want 1 session and 2 app devices", n)
	}
	if live := f.liveSessions(t, f.tenantID, f.employeeID); live != 0 {
		t.Errorf("%d live session(s) after the sign-out, want 0", live)
	}
	if live := f.liveAppDevices(t, f.tenantID, f.employeeID); live != 0 {
		t.Errorf("%d live app device(s) after the sign-out, want 0", live)
	}
	for _, p := range []appPhone{a, b} {
		if got := f.deviceNonces(t, f.tenantID, p.id); got != 0 {
			t.Errorf("a revoked device kept %d nonce row(s), want 0 -- the revoke deletes them", got)
		}
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != 1 {
		t.Fatalf("%d trail row(s), want 1 -- one act, both halves", rows)
	}
	detail, _ := f.auditDetail(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID)
	if !containsAny(detail, `"sessions_revoked": 1`, `"sessions_revoked":1`) ||
		!containsAny(detail, `"app_devices_revoked": 2`, `"app_devices_revoked":2`) {
		t.Errorf("the trail detail is %s, want sessions_revoked 1 and app_devices_revoked 2", detail)
	}
	if strings.Count(detail, ":") != 2 || containsAny(detail, a.id.String(), b.id.String(), "key", "nonce", "label") {
		t.Errorf("the trail detail carries more than the counts: %s", detail)
	}

	// THE NEXT REQUEST: row 3's twin. The signed request over a nonce issued before the sign-out
	// is refused as revoked (401), and the device is given no new nonce.
	_, err = v.Verify(context.Background(), signedTap(t, a, pending))
	if !errors.Is(err, appdevice.ErrRevokedDevice) || appdevice.StatusOf(err) != http.StatusUnauthorized {
		t.Errorf("a signed-out device's request answered %v (status %d), want ErrRevokedDevice / 401",
			err, appdevice.StatusOf(err))
	}
	if _, err := v.IssueNonce(context.Background(), b.id); !errors.Is(err, appdevice.ErrRevokedDevice) {
		t.Errorf("a signed-out device was answered %v when it asked for a nonce, want ErrRevokedDevice", err)
	}
}

// A person signed in ONLY in the app -- with one "who" credential per person (Q40 (a)) that is
// everybody who uses it -- is signed out too, and a second press is no act.
func TestStaffDB_SignOutOfAnAppOnlyPerson(t *testing.T) {
	f := newStaffFixture(t)
	f.addAppDevice(t, f.tenantID, f.employeeID)

	n, err := f.staff.SignOutPhones(context.Background(), f.signOut())
	if err != nil || n != (SignedOut{AppDevices: 1}) {
		t.Fatalf("SignOutPhones answered (%+v, %v), want (1 app device, nil)", n, err)
	}
	n, err = f.staff.SignOutPhones(context.Background(), f.signOut())
	if !errors.Is(err, ErrNoLiveSessions) || n.Total() != 0 {
		t.Fatalf("the second press answered (%+v, %v), want (0, ErrNoLiveSessions)", n, err)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != 1 {
		t.Errorf("%d trail row(s) after two presses, want 1", rows)
	}
}

// ALL OR NOTHING: a trail that cannot be written rolls back BOTH revokes.
func TestStaffDB_AFailedTrailLeavesTheAppDevicesLive(t *testing.T) {
	f := newStaffFixture(t)
	f.seedSessions(t, f.tenantID, f.employeeID, 1)
	p := f.addAppDevice(t, f.tenantID, f.employeeID)
	issueNonce(t, f.verifier(t), p)
	broken, err := NewStaff(f.data, brokenTrail{err: errors.New("trail down")}, nil)
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	if _, err := broken.SignOutPhones(context.Background(), f.signOut()); err == nil {
		t.Fatal("a sign-out whose trail failed reported success")
	}
	if f.deviceRevoked(t, f.tenantID, p.id) || f.liveSessions(t, f.tenantID, f.employeeID) != 1 {
		t.Error("a failed sign-out revoked something -- the revokes must roll back with the trail")
	}
	if got := f.deviceNonces(t, f.tenantID, p.id); got != 1 {
		t.Errorf("a failed sign-out left %d nonce row(s), want 1 -- the nonce delete rolls back too", got)
	}
}

// §4.5: a manager of this business cannot sign out another business's person's devices.
func TestStaffDB_SignOutNeverTouchesAnotherBusinesssAppDevices(t *testing.T) {
	f := newStaffFixture(t)
	foreign := f.addAppDevice(t, f.foreignTenant, f.foreignEmployee)
	own := f.addAppDevice(t, f.tenantID, f.employeeID)

	_, err := f.staff.SignOutPhones(context.Background(), SignOutCommand{
		TenantID: f.tenantID, EmployeeID: f.foreignEmployee, ActorID: f.actorID,
	})
	if !errors.Is(err, ErrUnknownEmployee) {
		t.Fatalf("signing out another business's person answered %v, want ErrUnknownEmployee", err)
	}
	if f.deviceRevoked(t, f.foreignTenant, foreign.id) {
		t.Error("another business's person's app device was revoked")
	}
	// CONTROL: the own person's device is revoked by the same call with the own id.
	if n, err := f.staff.SignOutPhones(context.Background(), f.signOut()); err != nil || n.AppDevices != 1 {
		t.Fatalf("the control answered (%+v, %v), want 1 app device", n, err)
	}
	if !f.deviceRevoked(t, f.tenantID, own.id) || f.deviceRevoked(t, f.foreignTenant, foreign.id) {
		t.Error("the control revoked the wrong device")
	}
}

// DEACTIVATION REVOKES NOTHING (the 2026-08-07 decision, the app's twin): after Deactivate the
// device is still `revoked_at IS NULL` and still verifies -- so its next tap reaches §5 row 4 and is
// RECORDED (the handler package's TestAppDeviceDB_ADeactivatedPersonsAppTapIsRejectedAndRecorded
// measures the record) -- and the sign-out is refused for them and touches no device either.
func TestStaffDB_DeactivationLeavesTheAppDeviceLiveAndTheSignOutRefuses(t *testing.T) {
	f := newStaffFixture(t)
	v := f.verifier(t)
	p := f.addAppDevice(t, f.tenantID, f.employeeID)
	if _, err := f.staff.Deactivate(context.Background(), DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if f.deviceRevoked(t, f.tenantID, p.id) {
		t.Fatal("Deactivate revoked the app device: the next tap would be row 3's recordless path (§4.6)")
	}
	dev, err := v.Verify(context.Background(), signedTap(t, p, issueNonce(t, v, p)))
	if err != nil || dev.EmployeeID != f.employeeID || dev.TenantID != f.tenantID {
		t.Fatalf("a deactivated person's device verified as (%+v, %v), want their identity", dev, err)
	}

	_, err = f.staff.SignOutPhones(context.Background(), f.signOut())
	if !errors.Is(err, ErrSignOutDeactivated) {
		t.Fatalf("signing out a deactivated person answered %v, want ErrSignOutDeactivated", err)
	}
	if f.deviceRevoked(t, f.tenantID, p.id) {
		t.Error("the refused sign-out revoked the app device")
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != 0 {
		t.Errorf("%d trail row(s) for a refused sign-out", rows)
	}
}

// THE REVOCATION IS ONE WAY (00104's app_devices_revocation_one_way, 00099's pattern): the
// application role cannot bring a signed-out device back, nor move its stamp.
func TestStaffDB_ASignedOutAppDeviceCannotBeRevived(t *testing.T) {
	f := newStaffFixture(t)
	p := f.addAppDevice(t, f.tenantID, f.employeeID)
	if _, err := f.staff.SignOutPhones(context.Background(), f.signOut()); err != nil {
		t.Fatalf("SignOutPhones: %v", err)
	}
	for _, stmt := range []string{
		`UPDATE app_devices SET revoked_at = NULL WHERE tenant_id = $1 AND id = $2`,
		`UPDATE app_devices SET revoked_at = revoked_at + interval '1 day' WHERE tenant_id = $1 AND id = $2`,
	} {
		err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, stmt, f.tenantID, p.id)
			return e
		})
		if err == nil {
			t.Errorf("tappa_app moved a revoked device's revoked_at: %s", stmt)
		}
	}
	if !f.deviceRevoked(t, f.tenantID, p.id) {
		t.Fatal("the signed-out device is live again")
	}
}

// Concurrent presses: each device is revoked by exactly one of them.
func TestStaffDB_ConcurrentSignOutsRevokeEachAppDeviceOnce(t *testing.T) {
	f := newStaffFixture(t)
	const devices, racers = 3, 8
	for range devices {
		f.addAppDevice(t, f.tenantID, f.employeeID)
	}
	var wg sync.WaitGroup
	counts := make([]SignedOut, racers)
	errs := make([]error, racers)
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			counts[i], errs[i] = f.staff.SignOutPhones(context.Background(), f.signOut())
		}()
	}
	close(start)
	wg.Wait()
	total, winners := 0, 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
			total += counts[i].AppDevices
		case errors.Is(err, ErrNoLiveSessions):
		default:
			t.Errorf("racer %d failed: %v", i, err)
		}
	}
	if total != devices || winners < 1 {
		t.Errorf("winners revoked %d device(s) across %d winner(s), want %d", total, winners, devices)
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != winners {
		t.Errorf("%d trail row(s) for %d winner(s)", rows, winners)
	}
}

// TestStaffDB_SignOutRacingTheAppsRequests: rounds of the device's own verifies and nonce issues
// running at once with the manager's sign-out. The sign-out NEVER fails -- in particular no
// deadlock reaches the manager -- each app request either completed before the revoke or is
// refused, and once the sign-out has committed no nonce of the device is spendable and none is
// issued.
func TestStaffDB_SignOutRacingTheAppsRequests(t *testing.T) {
	f := newStaffFixture(t)
	v := f.verifier(t)
	ctx := context.Background()
	const rounds, verifies, issues = 10, 6, 6

	for round := range rounds {
		f.seedSessions(t, f.tenantID, f.employeeID, 1)
		p := f.addAppDevice(t, f.tenantID, f.employeeID)
		reqs := make([]appdevice.SignedRequest, verifies)
		for i := range reqs {
			reqs[i] = signedTap(t, p, issueNonce(t, v, p))
		}
		late := signedTap(t, p, issueNonce(t, v, p)) // issued before, sent after the sign-out
		// Dead nonces, so the issue path's sweep has rows to lock (M16-03's D1 shape).
		for range 3 {
			if _, err := v.Verify(ctx, signedTap(t, p, issueNonce(t, v, p))); err != nil {
				t.Fatalf("round %d: spend before the race: %v", round, err)
			}
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		var failures []string
		fail := func(s string) { mu.Lock(); failures = append(failures, s); mu.Unlock() }
		start := make(chan struct{})
		var signedOut SignedOut
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			n, err := f.staff.SignOutPhones(ctx, f.signOut())
			if err != nil {
				fail("the sign-out failed: " + err.Error())
			}
			signedOut = n
		}()
		for i := range verifies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := v.Verify(ctx, reqs[i]); err != nil &&
					!errors.Is(err, appdevice.ErrRevokedDevice) && !errors.Is(err, appdevice.ErrNonceRejected) {
					fail("a verify failed: " + err.Error())
				}
			}()
		}
		for range issues {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := v.IssueNonce(ctx, p.id); err != nil && !errors.Is(err, appdevice.ErrRevokedDevice) {
					fail("an issue failed: " + err.Error())
				}
			}()
		}
		close(start)
		wg.Wait()
		if len(failures) > 0 {
			t.Fatalf("round %d: %s", round, strings.Join(failures, "; "))
		}
		if signedOut != (SignedOut{Sessions: 1, AppDevices: 1}) {
			t.Fatalf("round %d: the sign-out revoked %+v, want 1 session and 1 device", round, signedOut)
		}
		// AFTER the commit: nothing spendable, nothing issued.
		if _, err := v.Verify(ctx, late); !errors.Is(err, appdevice.ErrRevokedDevice) {
			t.Fatalf("round %d: a nonce issued before the sign-out was answered %v after it, want ErrRevokedDevice", round, err)
		}
		if _, err := v.IssueNonce(ctx, p.id); !errors.Is(err, appdevice.ErrRevokedDevice) {
			t.Fatalf("round %d: the signed-out device was issued a nonce (%v)", round, err)
		}
	}
}

// TestStaffDB_ADeadlockedSignOutIsRetriedNotShown crosses the sign-out with a transaction that
// holds the device row and wants the session row the sign-out already revoked -- the deadlock a
// credential-swapping activation could meet. The crossing transaction runs as the owner with a
// long deadlock_timeout, so Postgres picks the SIGN-OUT as the victim (40P01, measured through the
// retry log line); the retry must then succeed with both counts, and the manager never sees it.
func TestStaffDB_ADeadlockedSignOutIsRetriedNotShown(t *testing.T) {
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the crossing transaction needs a role that may set deadlock_timeout")
	}
	f := newStaffFixture(t)
	ctx := context.Background()
	f.seedSessions(t, f.tenantID, f.employeeID, 1)
	p := f.addAppDevice(t, f.tenantID, f.employeeID)

	var logs bytes.Buffer
	var logMu sync.Mutex
	staff, err := NewStaff(f.data, f.trail, slog.New(slog.NewJSONHandler(&lockedWriter{w: &logs, mu: &logMu}, nil)))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	reached, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	staff.afterSessionsRevoked = func(context.Context) {
		once.Do(func() {
			close(reached)
			<-proceed
		})
	}

	type result struct {
		n   SignedOut
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := staff.SignOutPhones(ctx, f.signOut())
		done <- result{n, err}
	}()
	<-reached

	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	defer owner.Close(ctx)
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatalf("owner begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL deadlock_timeout = '10s'`); err != nil {
		t.Fatalf("set deadlock_timeout: %v", err)
	}
	var pid int
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("pid: %v", err)
	}
	// The device row first ...
	if _, err := tx.Exec(ctx, `UPDATE app_devices SET last_used_at = now() WHERE id = $1`, p.id); err != nil {
		t.Fatalf("lock the device row: %v", err)
	}
	// ... then the session row the sign-out holds: this waits.
	crossed := make(chan error, 1)
	go func() {
		_, e := tx.Exec(ctx,
			`UPDATE sessions SET last_used_at = now() WHERE tenant_id = $1 AND employee_id = $2`,
			f.tenantID, f.employeeID)
		crossed <- e
	}()
	waitForLockWait(t, ownerDSN, pid)
	close(proceed) // the sign-out now wants the device row: a deadlock, detected on its side

	// The sign-out's first try is the victim and rolls back, so the crossing transaction gets its
	// row; it commits, and only then can the retry take the rows the crossing one held.
	select {
	case err := <-crossed:
		if err != nil {
			t.Fatalf("the crossing transaction failed: %v (it was meant to win)", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the crossing transaction never got the session row")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit the crossing transaction: %v", err)
	}
	var r result
	select {
	case r = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the sign-out did not finish")
	}
	if r.err != nil {
		var pe *pgconn.PgError
		t.Fatalf("the sign-out surfaced %v (deadlock: %v) -- a 40P01 must be retried", r.err, errors.As(r.err, &pe) && pe.Code == "40P01")
	}
	if r.n != (SignedOut{Sessions: 1, AppDevices: 1}) {
		t.Fatalf("the retried sign-out revoked %+v, want 1 session and 1 device", r.n)
	}
	logMu.Lock()
	text := logs.String()
	logMu.Unlock()
	if !strings.Contains(text, "deadlocked; retrying") {
		t.Fatalf("no retry was logged -- the crossing transaction, not the sign-out, was the victim, so this run measured nothing:\n%s", text)
	}
	if !f.deviceRevoked(t, f.tenantID, p.id) || f.liveSessions(t, f.tenantID, f.employeeID) != 0 {
		t.Error("after the retry something is still live")
	}
	if rows := f.auditRows(t, f.tenantID, ActionEmployeePhonesSignedOut, f.employeeID); rows != 1 {
		t.Errorf("%d trail row(s), want 1 -- the rolled-back try wrote none", rows)
	}
}

// waitForLockWait polls until backend pid is waiting on a lock.
func waitForLockWait(t *testing.T, dsn string, pid int) {
	t.Helper()
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("observer connect: %v", err)
	}
	defer c.Close(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting bool
		if err := c.QueryRow(ctx,
			`SELECT coalesce(wait_event_type = 'Lock', false) FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&waiting); err != nil {
			t.Fatalf("observe: %v", err)
		}
		if waiting {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the crossing transaction never waited on the sign-out's lock")
}

// lockedWriter serialises the JSON log handler's writes for the test's read.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}

// TestStaffDB_TheSummaryCountsLiveAppDevices: the card's app count is the revoke's predicate --
// live devices of this person in this business, a revoked one not counted, a deactivated person's
// still counted (nothing revoked it), another business's never.
func TestStaffDB_TheSummaryCountsLiveAppDevices(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	f.addAppDevice(t, f.tenantID, f.employeeID)
	revoked := f.addAppDevice(t, f.tenantID, f.employeeID)
	f.addAppDevice(t, f.foreignTenant, f.foreignEmployee)
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: revoked.id, TenantID: f.tenantID})
		return e
	}); err != nil {
		t.Fatalf("revoke one: %v", err)
	}
	got, err := f.staff.Summary(ctx, f.tenantID, f.employeeID)
	if err != nil || got.State != Measured {
		t.Fatalf("Summary: (%+v, %v)", got, err)
	}
	if got.LiveAppDevices != 1 || got.LiveDevices != 0 {
		t.Fatalf("summary counts %d app device(s) and %d session(s), want 1 and 0", got.LiveAppDevices, got.LiveDevices)
	}
	if _, err := f.staff.Deactivate(ctx, DeactivateCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
	}); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if got, _ := f.staff.Summary(ctx, f.tenantID, f.employeeID); got.LiveAppDevices != 1 {
		t.Errorf("a deactivated person's summary counts %d app device(s), want 1 -- deactivation revokes nothing", got.LiveAppDevices)
	}
}
