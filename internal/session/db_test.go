package session

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// db_test.go -- the lifecycle proven against a REAL Postgres (CLAUDE.md §8): the
// SECURITY DEFINER resolution path, the RLS-scoped writes and the "nothing but a
// hash is stored" red line cannot be proven against a fake database.
//
// FIXTURES ARE NOT CLEANED UP, on purpose and for the same reason as
// internal/db's tests: tappa_app has REVOKE DELETE on sessions and employees, so
// teardown would need the migration role -- exactly what redline R5 forbids.
// Every fixture uses fresh random UUIDs, so repeated runs never collide and the
// rows stay inert; `make db-reset` clears the dev database.

// appDB opens a tappa_app pool (NOBYPASSRLS, not the table owner), so RLS is in
// force for everything below. Skips when no database is configured.
func appDB(t *testing.T) *db.DB {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping session DB tests (real Postgres required -- CLAUDE.md §8)")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: raw})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// tenantFixture is one tenant with a location and an ACTIVE employee -- the
// minimum FK chain a session needs.
type tenantFixture struct {
	tenantID   uuid.UUID
	locationID uuid.UUID
	employeeID uuid.UUID
}

func newFixture(t *testing.T, d *db.DB) tenantFixture {
	t.Helper()
	fx := tenantFixture{tenantID: uuid.New(), locationID: uuid.New(), employeeID: uuid.New()}
	err := d.WithTenant(context.Background(), fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'session-test', $2, 'bar', 'single')`,
			fx.tenantID, "VAT-"+fx.tenantID.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng)
			 VALUES ($1, $2, 'loc', '{203.0.113.0/24}', 35.899, 14.514)`,
			fx.locationID, fx.tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, 'Maria', 'active')`,
			fx.employeeID, fx.tenantID, fx.locationID)
		return e
	})
	if err != nil {
		t.Fatalf("newFixture: %v", err)
	}
	return fx
}

func dbManager(t *testing.T, d *db.DB) *Manager {
	t.Helper()
	m, err := New(d, testConfig("dev", "http://localhost:8080"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// TestRoundTrip is the card's whole lifecycle in one pass: issue -> verify ->
// refresh last_used_at -> revoke -> the next verification falls.
func TestRoundTrip(t *testing.T) {
	d := appDB(t)
	fx := newFixture(t, d)
	m := dbManager(t, d)
	ctx := context.Background()

	// --- issue ---------------------------------------------------------------
	issued, err := m.Issue(ctx, IssueParams{
		TenantID: fx.tenantID, EmployeeID: fx.employeeID, DeviceInfo: "iPhone Safari",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if issued.Session.ID == uuid.Nil {
		t.Fatal("Issue returned no session id")
	}
	if issued.Session.TenantID != fx.tenantID || issued.Session.EmployeeID != fx.employeeID {
		t.Fatalf("issued session = %+v, want tenant %s employee %s", issued.Session, fx.tenantID, fx.employeeID)
	}
	if issued.Session.DeviceInfo != "iPhone Safari" {
		t.Fatalf("device label = %q, want the coarse label the caller supplied", issued.Session.DeviceInfo)
	}
	if !issued.Session.Live() {
		t.Fatal("a freshly issued session is not Live()")
	}
	if issued.Session.LastUsedAt != nil {
		t.Fatal("a freshly issued session already has last_used_at")
	}

	// --- verify --------------------------------------------------------------
	res, err := m.Verify(ctx, issued.Token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.ID != issued.Session.ID || res.TenantID != fx.tenantID || res.EmployeeID != fx.employeeID {
		t.Fatalf("Verify resolved %+v, want session %s / tenant %s / employee %s",
			res, issued.Session.ID, fx.tenantID, fx.employeeID)
	}
	if res.RevokedAt != nil {
		t.Fatal("a live session resolved with a revocation stamp")
	}

	// --- refresh on use ------------------------------------------------------
	if err := m.Touch(ctx, res); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	list, err := m.ListForEmployee(ctx, fx.tenantID, fx.employeeID)
	if err != nil {
		t.Fatalf("ListForEmployee: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("listed %d sessions, want 1", len(list))
	}
	if list[0].LastUsedAt == nil {
		t.Fatal("Touch did not set last_used_at")
	}
	first := *list[0].LastUsedAt

	if err := m.Touch(ctx, res); err != nil {
		t.Fatalf("Touch (second): %v", err)
	}
	list, err = m.ListForEmployee(ctx, fx.tenantID, fx.employeeID)
	if err != nil {
		t.Fatalf("ListForEmployee: %v", err)
	}
	if second := *list[0].LastUsedAt; !second.After(first) {
		t.Fatalf("last_used_at did not advance: %s then %s", first, second)
	}

	// --- revoke --------------------------------------------------------------
	if err := m.Revoke(ctx, fx.tenantID, issued.Session.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// The next verification falls -- but it STILL returns the facts, so the tap
	// path can record the attempt instead of losing it (§4.6, §5 row 4).
	res2, err := m.Verify(ctx, issued.Token)
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("Verify after revoke error = %v, want ErrRevoked", err)
	}
	if res2.EmployeeID != fx.employeeID || res2.TenantID != fx.tenantID || res2.ID != issued.Session.ID {
		t.Fatalf("revoked verification returned %+v, want the resolved facts", res2)
	}
	if res2.RevokedAt == nil {
		t.Fatal("revoked verification returned a nil revocation stamp")
	}

	// A refresh of a dead session fails closed.
	if err := m.Touch(ctx, res); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Touch after revoke error = %v, want ErrNoSession", err)
	}

	// Revoking again is idempotent AND keeps the first stamp (audit truth).
	stamp := *res2.RevokedAt
	if err := m.Revoke(ctx, fx.tenantID, issued.Session.ID); err != nil {
		t.Fatalf("Revoke (repeat): %v", err)
	}
	res3, err := m.Verify(ctx, issued.Token)
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("Verify after second revoke error = %v, want ErrRevoked", err)
	}
	if !res3.RevokedAt.Equal(stamp) {
		t.Fatalf("repeated revoke rewrote the stamp: %s -> %s", stamp, *res3.RevokedAt)
	}

	// The row survives revocation -- revocation is a stamp, never a delete.
	list, err = m.ListForEmployee(ctx, fx.tenantID, fx.employeeID)
	if err != nil {
		t.Fatalf("ListForEmployee: %v", err)
	}
	if len(list) != 1 || list[0].Live() {
		t.Fatalf("after revoke the listing is %+v, want one revoked row", list)
	}
}

// TestVerify_UnknownAndWrongKey: a value we never issued does not resolve, and
// neither does a real value under a different HMAC key -- proof the column is
// keyed, not a bare digest a stolen dump could be replayed with.
func TestVerify_UnknownAndWrongKey(t *testing.T) {
	d := appDB(t)
	fx := newFixture(t, d)
	m := dbManager(t, d)
	ctx := context.Background()

	unissued, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	if _, err := m.Verify(ctx, unissued); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Verify(never issued) error = %v, want ErrNoSession", err)
	}

	issued, err := m.Issue(ctx, IssueParams{TenantID: fx.tenantID, EmployeeID: fx.employeeID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// Positive control: the right key resolves it.
	if _, err := m.Verify(ctx, issued.Token); err != nil {
		t.Fatalf("Verify with the issuing key: %v", err)
	}

	otherCfg := testConfig("dev", "")
	otherCfg.SessionHMACKey = fakeHMACKey2
	other, err := New(d, otherCfg)
	if err != nil {
		t.Fatalf("New (other key): %v", err)
	}
	if _, err := other.Verify(ctx, issued.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Verify under a different key error = %v, want ErrNoSession", err)
	}
}

// TestNoRawValueInTheDatabase is the §4.7 acceptance test on the storage side:
// the whole row is serialised and searched. It is written over row_to_json so a
// column added later is covered without touching this test.
func TestNoRawValueInTheDatabase(t *testing.T) {
	d := appDB(t)
	fx := newFixture(t, d)
	m := dbManager(t, d)
	ctx := context.Background()

	issued, err := m.Issue(ctx, IssueParams{
		TenantID: fx.tenantID, EmployeeID: fx.employeeID, DeviceInfo: "iPhone Safari",
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	raw := issued.Token.reveal()

	var rowJSON, stored string
	err = d.WithTenant(ctx, fx.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx,
			`SELECT row_to_json(s)::text FROM sessions s WHERE s.id = $1 AND s.tenant_id = $2`,
			issued.Session.ID, fx.tenantID).Scan(&rowJSON); e != nil {
			return e
		}
		return tx.QueryRow(ctx,
			`SELECT s.token_hash FROM sessions s WHERE s.id = $1 AND s.tenant_id = $2`,
			issued.Session.ID, fx.tenantID).Scan(&stored)
	})
	if err != nil {
		t.Fatalf("read the stored row: %v", err)
	}

	// POSITIVE CONTROL: the probe really read the row we just wrote. Without
	// this, "the value is absent" would also be true of an empty result.
	if !strings.Contains(rowJSON, stored) {
		t.Fatalf("the serialised row does not contain the stored hash -- the probe read nothing useful: %q", rowJSON)
	}
	if !strings.Contains(rowJSON, fx.employeeID.String()) {
		t.Fatalf("the serialised row does not contain the employee id -- wrong row: %q", rowJSON)
	}

	if strings.Contains(rowJSON, raw) {
		t.Fatal("the raw value is stored in the sessions row")
	}
	if len(raw) >= 12 && strings.Contains(rowJSON, raw[:12]) {
		t.Fatal("a prefix of the raw value is stored in the sessions row")
	}
	if h := hex.EncodeToString([]byte(raw)); strings.Contains(rowJSON, h) {
		t.Fatal("a hex-encoded form of the raw value is stored in the sessions row")
	}

	// And the stored value is exactly the keyed hash we expect.
	want, err := issued.Token.hash(fakeHMACKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if stored != want {
		t.Fatal("the stored column is not the keyed hash of the issued value")
	}
	if len(stored) != 64 {
		t.Fatalf("the stored column is %d chars, want 64 hex chars", len(stored))
	}
}

// TestNoLeakOnLivePaths: after a full round trip against the real database, no
// error message and no structured log line carries the raw value (§4.7).
func TestNoLeakOnLivePaths(t *testing.T) {
	d := appDB(t)
	fx := newFixture(t, d)
	m := dbManager(t, d)
	ctx := context.Background()

	issued, err := m.Issue(ctx, IssueParams{TenantID: fx.tenantID, EmployeeID: fx.employeeID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	raw := issued.Token.reveal()

	// Every error path a caller can reach with a value in hand.
	var errs []error
	push := func(e error) { errs = append(errs, e) }
	_, e1 := m.Verify(ctx, wrap("not-a-session"))
	push(e1)
	_, e2 := m.Verify(ctx, Token{})
	push(e2)
	unknown, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	_, e3 := m.Verify(ctx, unknown)
	push(e3)
	push(m.Touch(ctx, Resolved{ID: uuid.New(), TenantID: fx.tenantID}))
	push(m.Revoke(ctx, fx.tenantID, uuid.New()))
	if err := m.Revoke(ctx, fx.tenantID, issued.Session.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, e4 := m.Verify(ctx, issued.Token)
	push(e4)

	for i, e := range errs {
		if e == nil {
			t.Fatalf("error path %d unexpectedly succeeded", i)
		}
		if msg := e.Error(); strings.Contains(msg, raw) || strings.Contains(msg, unknown.reveal()) {
			t.Fatalf("error %d leaks a raw value: %q", i, msg)
		}
	}

	// A structured log of everything a handler would plausibly log.
	var buf bytes.Buffer
	lg := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	lg.Info("session issued", "issued", issued, "sess", issued.Token, "record", issued.Session)
	res, _ := m.Verify(ctx, issued.Token)
	lg.Info("session verified", "resolved", res)
	for i, e := range errs {
		lg.Error("session path failed", "i", i, "err", e)
	}
	if out := buf.String(); strings.Contains(out, raw) {
		t.Fatalf("a structured log line leaks the raw value: %q", out)
	}
	// POSITIVE CONTROL: the logger really wrote something about this session.
	if !strings.Contains(buf.String(), issued.Session.ID.String()) {
		t.Fatalf("the log buffer does not mention the session -- the probe logged nothing: %q", buf.String())
	}
}

// TestRevokeAllForEmployee: the instant-invalidation surface for a lost or stolen
// phone and for the second-activation decision (M5-02). It kills every live
// session, is idempotent, and leaves the rows in place.
//
// NOT called by deactivation (M6-05) — this comment used to say it was. Revoking
// on deactivation adds nothing to the reject (the guardrail reads
// employees.status directly) and pushes later taps onto the ErrRevoked branch,
// where a caller taking the obvious shortcut writes no record, breaking §4.6.
// See RevokeAllForEmployee's doc comment.
func TestRevokeAllForEmployee(t *testing.T) {
	d := appDB(t)
	fx := newFixture(t, d)
	m := dbManager(t, d)
	ctx := context.Background()

	var issued []Issued
	for i := 0; i < 3; i++ {
		is, err := m.Issue(ctx, IssueParams{TenantID: fx.tenantID, EmployeeID: fx.employeeID})
		if err != nil {
			t.Fatalf("Issue %d: %v", i, err)
		}
		issued = append(issued, is)
	}
	// Positive control: all three verify before the revocation.
	for i, is := range issued {
		if _, err := m.Verify(ctx, is.Token); err != nil {
			t.Fatalf("Verify %d before revoke: %v", i, err)
		}
	}

	n, err := m.RevokeAllForEmployee(ctx, fx.tenantID, fx.employeeID)
	if err != nil {
		t.Fatalf("RevokeAllForEmployee: %v", err)
	}
	if n != 3 {
		t.Fatalf("revoked %d sessions, want 3", n)
	}
	for i, is := range issued {
		if _, err := m.Verify(ctx, is.Token); !errors.Is(err, ErrRevoked) {
			t.Fatalf("Verify %d after revoke-all error = %v, want ErrRevoked", i, err)
		}
	}

	// Idempotent: nothing left to revoke.
	if n, err = m.RevokeAllForEmployee(ctx, fx.tenantID, fx.employeeID); err != nil || n != 0 {
		t.Fatalf("second RevokeAllForEmployee = (%d, %v), want (0, nil)", n, err)
	}

	// The rows are still there: revocation is a stamp, not a delete.
	list, err := m.ListForEmployee(ctx, fx.tenantID, fx.employeeID)
	if err != nil {
		t.Fatalf("ListForEmployee: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("listed %d sessions after revoke-all, want 3 (rows survive)", len(list))
	}
	for _, s := range list {
		if s.Live() {
			t.Fatalf("session %s is still live after revoke-all", s.ID)
		}
	}
}

// TestTenantIsolation is the §4.5 proof. Following internal/db/rls_test.go: the
// isolation probe is RAW SQL carrying NO tenant_id predicate (a filtered query
// would return 0 rows because of the WHERE, not because of RLS, and would stay
// green with RLS switched off), and every negative assertion is paired with a
// POSITIVE CONTROL in the correct context.
func TestTenantIsolation(t *testing.T) {
	d := appDB(t)
	a := newFixture(t, d)
	b := newFixture(t, d)
	m := dbManager(t, d)
	ctx := context.Background()

	issued, err := m.Issue(ctx, IssueParams{TenantID: a.tenantID, EmployeeID: a.employeeID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Raw, UNFILTERED probe -- the row is addressed by primary key only.
	const probe = `SELECT count(*) FROM sessions WHERE id = $1`

	var inA, inB int
	if err := d.WithTenant(ctx, a.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, probe, issued.Session.ID).Scan(&inA)
	}); err != nil {
		t.Fatalf("probe in tenant A: %v", err)
	}
	if err := d.WithTenant(ctx, b.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, probe, issued.Session.ID).Scan(&inB)
	}); err != nil {
		t.Fatalf("probe in tenant B: %v", err)
	}
	// POSITIVE CONTROL first: without this, inB == 0 proves nothing.
	if inA != 1 {
		t.Fatalf("tenant A sees %d rows of its own session, want 1 -- the probe is vacuous", inA)
	}
	if inB != 0 {
		t.Fatalf("tenant B sees %d rows of tenant A's session, want 0 (RLS)", inB)
	}

	// The package API must not offer a way around that. Listing tenant A's
	// employee from tenant B's context yields nothing...
	crossList, err := m.ListForEmployee(ctx, b.tenantID, a.employeeID)
	if err != nil {
		t.Fatalf("cross-tenant ListForEmployee: %v", err)
	}
	if len(crossList) != 0 {
		t.Fatalf("tenant B listed %d of tenant A's sessions, want 0", len(crossList))
	}
	// ...while the same call in the right context does (positive control).
	ownList, err := m.ListForEmployee(ctx, a.tenantID, a.employeeID)
	if err != nil {
		t.Fatalf("ListForEmployee: %v", err)
	}
	if len(ownList) != 1 {
		t.Fatalf("tenant A listed %d of its own sessions, want 1", len(ownList))
	}

	// Revoking across tenants must not touch the row...
	if err := m.Revoke(ctx, b.tenantID, issued.Session.ID); !errors.Is(err, ErrNoSession) {
		t.Fatalf("cross-tenant Revoke error = %v, want ErrNoSession", err)
	}
	if _, err := m.Verify(ctx, issued.Token); err != nil {
		t.Fatalf("a cross-tenant revoke killed the session: %v", err)
	}
	// ...and neither must a cross-tenant refresh.
	if err := m.Touch(ctx, Resolved{ID: issued.Session.ID, TenantID: b.tenantID}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("cross-tenant Touch error = %v, want ErrNoSession", err)
	}
	// POSITIVE CONTROL: the same operations succeed in the owning tenant.
	if err := m.Touch(ctx, Resolved{ID: issued.Session.ID, TenantID: a.tenantID}); err != nil {
		t.Fatalf("Touch in the owning tenant: %v", err)
	}
	if err := m.Revoke(ctx, a.tenantID, issued.Session.ID); err != nil {
		t.Fatalf("Revoke in the owning tenant: %v", err)
	}

	// RevokeAllForEmployee is tenant-scoped too: tenant B cannot mass-revoke
	// tenant A's employee.
	fresh, err := m.Issue(ctx, IssueParams{TenantID: a.tenantID, EmployeeID: a.employeeID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	n, err := m.RevokeAllForEmployee(ctx, b.tenantID, a.employeeID)
	if err != nil {
		t.Fatalf("cross-tenant RevokeAllForEmployee: %v", err)
	}
	if n != 0 {
		t.Fatalf("tenant B revoked %d of tenant A's sessions, want 0", n)
	}
	if _, err := m.Verify(ctx, fresh.Token); err != nil {
		t.Fatalf("a cross-tenant revoke-all killed the session: %v", err)
	}
	// POSITIVE CONTROL.
	if n, err = m.RevokeAllForEmployee(ctx, a.tenantID, a.employeeID); err != nil || n != 1 {
		t.Fatalf("owning-tenant RevokeAllForEmployee = (%d, %v), want (1, nil)", n, err)
	}
}

// TestIssue_RejectsCrossTenantEmployee: the composite FK plus the RLS WITH CHECK
// make a session in one tenant pointing at another tenant's employee impossible
// at the database level, not merely by convention.
func TestIssue_RejectsCrossTenantEmployee(t *testing.T) {
	d := appDB(t)
	a := newFixture(t, d)
	b := newFixture(t, d)
	m := dbManager(t, d)

	if _, err := m.Issue(context.Background(), IssueParams{
		TenantID: b.tenantID, EmployeeID: a.employeeID,
	}); err == nil {
		t.Fatal("issued a session for another tenant's employee")
	}
}
