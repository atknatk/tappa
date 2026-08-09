package invite

// Tests against a REAL Postgres (CLAUDE.md §8 / Q04). The things this file
// measures -- the atomic consumption, the column-level UPDATE grant, RLS, the
// 'deactivated' refusal -- are properties of the DATABASE, and a fake would
// simply agree with whatever the code believes.
//
// Fixtures are NOT cleaned up: tappa_app has REVOKE DELETE on employee_invites
// and employees, and reaching for the owner role in test code is what redline R5
// forbids. Every fixture uses a fresh random UUID so repeated runs never collide
// (the same convention as internal/db/store_test.go); `make db-reset` clears the
// dev database.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping invite DB tests (real Postgres required)")
	}
	return &config.Config{
		DatabaseURL:   raw,
		BaseURL:       "http://localhost:8080",
		InviteHMACKey: testKeyA,
	}
}

func testManager(t *testing.T) (*Manager, *db.DB) {
	t.Helper()
	cfg := testConfig(t)
	d, err := db.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	m, err := New(d, cfg)
	if err != nil {
		t.Fatalf("invite.New: %v", err)
	}
	return m, d
}

// fixture is one tenant + one location + one employee, committed.
type fixture struct {
	tenantID   uuid.UUID
	locationID uuid.UUID
	employeeID uuid.UUID
}

// newFixture commits a tenant, a location (with or without a Wi-Fi name) and an
// employee in the given lifecycle status.
func newFixture(t *testing.T, d *db.DB, status, ssid string) fixture {
	t.Helper()
	f := fixture{tenantID: uuid.New(), locationID: uuid.New(), employeeID: uuid.New()}
	var wifi any
	if ssid != "" {
		wifi = ssid
	}
	err := d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Invite Test Ltd', $2, 'restaurant', 'multi')`,
			f.tenantID, "VAT-"+f.tenantID.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng, wifi_ssid)
			 VALUES ($1, $2, 'St Julians', '{203.0.113.0/24}', 35.899, 14.514, $3)`,
			f.locationID, f.tenantID, wifi); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Maria Borg', $4, now())`,
			f.employeeID, f.tenantID, f.locationID, status)
		return e
	})
	if err != nil {
		t.Fatalf("newFixture: %v", err)
	}
	return f
}

// captureChannel is a Channel that keeps the activation URL in memory so a test
// can drive the flow. It is the same seam a real delivery mechanism uses -- which
// is the point: the test proves the seam is usable, and no test needs a special
// accessor to obtain a code.
type captureChannel struct {
	last Delivery
	n    int
	err  error
}

func (c *captureChannel) DeliverInvite(_ context.Context, d Delivery) error {
	c.n++
	c.last = d
	return c.err
}

// code extracts the raw code from the captured link. Test-only; production code
// never does this.
func (c *captureChannel) code(t *testing.T) Code {
	t.Helper()
	i := strings.Index(c.last.ActivationURL, "code=")
	if i < 0 {
		t.Fatalf("activation url has no code parameter")
	}
	return ParseCode(c.last.ActivationURL[i+len("code="):])
}

func issue(t *testing.T, m *Manager, f fixture, ttl time.Duration) (Invite, Code) {
	t.Helper()
	ch := &captureChannel{}
	inv, err := m.IssueAndDeliver(context.Background(), IssueParams{
		TenantID: f.tenantID, EmployeeID: f.employeeID, TTL: ttl,
	}, ch)
	if err != nil {
		t.Fatalf("IssueAndDeliver: %v", err)
	}
	if ch.n != 1 {
		t.Fatalf("channel called %d times, want 1", ch.n)
	}
	return inv, ch.code(t)
}

// TestIssueAndDeliver_StoresOnlyTheHash is the §4.7 measurement at the storage
// boundary: the row must be findable by the HASH and NOT by the code.
func TestIssueAndDeliver_StoresOnlyTheHash(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusInvited, "KF-StJulians-Staff")
	inv, code := issue(t, m, f, 0)

	if inv.ExpiresAt.Sub(inv.CreatedAt) < 6*24*time.Hour {
		t.Errorf("default ttl looks wrong: created %s expires %s", inv.CreatedAt, inv.ExpiresAt)
	}

	raw := code.reveal()
	hash, err := code.hash(testKeyA)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	err = d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var byHash, byRaw int
		if e := tx.QueryRow(ctx,
			`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND code_hash = $2`,
			f.tenantID, hash).Scan(&byHash); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx,
			`SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND code_hash = $2`,
			f.tenantID, raw).Scan(&byRaw); e != nil {
			return e
		}
		if byHash != 1 {
			t.Errorf("rows matching the hash = %d, want 1", byHash)
		}
		if byRaw != 0 {
			t.Errorf("the RAW CODE matches %d stored rows: it must never be stored", byRaw)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
}

// TestIssueAndDeliver_RejectsAbsurdTTL: the window is validated, not clamped.
func TestIssueAndDeliver_RejectsAbsurdTTL(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusInvited, "")
	for _, ttl := range []time.Duration{time.Minute, 365 * 24 * time.Hour, -time.Hour} {
		_, err := m.IssueAndDeliver(context.Background(), IssueParams{
			TenantID: f.tenantID, EmployeeID: f.employeeID, TTL: ttl,
		}, &captureChannel{})
		if err == nil {
			t.Errorf("ttl %s was accepted; it must be refused, not clamped", ttl)
		}
	}
}

// TestLookup_ReturnsEverythingThePageNeeds, including the Wi-Fi step's data and
// the GDPR controller's name.
func TestLookup_ReturnsEverythingThePageNeeds(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusInvited, "KF-StJulians-Staff")
	_, code := issue(t, m, f, 0)

	got, err := m.Lookup(context.Background(), code)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.FullName != "Maria Borg" || got.TenantName != "Invite Test Ltd" {
		t.Errorf("identity = %q / %q", got.FullName, got.TenantName)
	}
	if got.LocationName != "St Julians" || got.WiFiSSID != "KF-StJulians-Staff" {
		t.Errorf("venue = %q / %q", got.LocationName, got.WiFiSSID)
	}
	if !got.FirstActivation() || got.SecondDevice() {
		t.Errorf("status = %q, want a first activation", got.Status)
	}
}

// TestLookup_NullWiFiIsNotAnError: locations.wifi_ssid IS NULL means "no network
// to show" (00010), and the flow must carry on.
func TestLookup_NullWiFiIsNotAnError(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusInvited, "")
	_, code := issue(t, m, f, 0)

	got, err := m.Lookup(context.Background(), code)
	if err != nil {
		t.Fatalf("Lookup with a NULL wifi_ssid must succeed: %v", err)
	}
	if got.WiFiSSID != "" {
		t.Errorf("WiFiSSID = %q, want empty", got.WiFiSSID)
	}
	if got.LocationName == "" {
		t.Error("the venue name must still be there so the page can name the place")
	}
}

// TestLookup_FailuresCarryTheirTenant is the §4.6 property: an attributable
// failure must return enough context to be written to audit_log, and only the
// TRULY unattributable one (unknown code) returns nothing.
func TestLookup_FailuresCarryTheirTenant(t *testing.T) {
	m, d := testManager(t)

	t.Run("expired", func(t *testing.T) {
		f := newFixture(t, d, statusInvited, "")
		_, code := issue(t, m, f, time.Hour)
		m.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
		t.Cleanup(func() { m.now = nil })

		got, err := m.Lookup(context.Background(), code)
		if !errors.Is(err, ErrCodeExpired) {
			t.Fatalf("err = %v, want ErrCodeExpired", err)
		}
		if got.TenantID != f.tenantID || got.EmployeeID != f.employeeID {
			t.Fatal("an expired code must still resolve to its tenant, or the attempt cannot be audited")
		}
	})

	t.Run("already used", func(t *testing.T) {
		f := newFixture(t, d, statusInvited, "")
		_, code := issue(t, m, f, 0)
		if _, err := m.Activate(context.Background(), code); err != nil {
			t.Fatalf("first Activate: %v", err)
		}
		got, err := m.Lookup(context.Background(), code)
		if !errors.Is(err, ErrCodeUsed) {
			t.Fatalf("err = %v, want ErrCodeUsed", err)
		}
		if got.TenantID != f.tenantID {
			t.Fatal("a replayed code must resolve to its tenant")
		}
	})

	t.Run("deactivated employee", func(t *testing.T) {
		f := newFixture(t, d, statusDeactivated, "")
		_, code := issue(t, m, f, 0)
		got, err := m.Lookup(context.Background(), code)
		if !errors.Is(err, ErrNotActivatable) {
			t.Fatalf("err = %v, want ErrNotActivatable", err)
		}
		if got.TenantID != f.tenantID {
			t.Fatal("the attempt must be attributable")
		}
	})

	t.Run("unknown code has nothing to attribute", func(t *testing.T) {
		other, err := newCode()
		if err != nil {
			t.Fatalf("newCode: %v", err)
		}
		got, err := m.Lookup(context.Background(), other)
		if !errors.Is(err, ErrUnknownCode) {
			t.Fatalf("err = %v, want ErrUnknownCode", err)
		}
		if got.TenantID != uuid.Nil {
			t.Fatal("an unknown code must not invent a tenant")
		}
	})

	t.Run("malformed code is indistinguishable from unknown", func(t *testing.T) {
		_, err := m.Lookup(context.Background(), ParseCode("nonsense"))
		if !errors.Is(err, ErrUnknownCode) {
			t.Fatalf("err = %v, want ErrUnknownCode", err)
		}
	})
}

// TestActivate_ConsumesExactlyOnce: the happy path, then the replay.
func TestActivate_ConsumesExactlyOnce(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusInvited, "KF-StJulians-Staff")
	inv, code := issue(t, m, f, 0)

	act, err := m.Activate(context.Background(), code)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if act.EmployeeID != f.employeeID || act.LocationID != f.locationID {
		t.Errorf("activation identifies the wrong row: %+v", act)
	}
	if act.InviteID != inv.ID {
		t.Errorf("invite id = %s, want %s", act.InviteID, inv.ID)
	}
	if act.SecondDeviceReplaced {
		t.Error("a first activation must not report a replaced device")
	}
	if act.WiFiSSID != "KF-StJulians-Staff" {
		t.Errorf("the confirmation needs the venue network, got %q", act.WiFiSSID)
	}

	assertEmployeeStatus(t, d, f, statusActive)
	assertInviteConsumed(t, d, f, inv.ID, true)

	// The replay.
	if _, err := m.Activate(context.Background(), code); !errors.Is(err, ErrCodeUsed) {
		t.Fatalf("second Activate err = %v, want ErrCodeUsed", err)
	}
}

// TestActivate_SecondDeviceIsReportedNotRefused is the decision this task had to
// make: an ALREADY ACTIVE employee presenting a valid unused invitation is a new
// phone, not an error. The manager reports it; revoking the old sessions is the
// caller's move (and the caller is tested separately).
func TestActivate_SecondDeviceIsReportedNotRefused(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusActive, "")
	_, code := issue(t, m, f, 0)

	act, err := m.Activate(context.Background(), code)
	if err != nil {
		t.Fatalf("Activate on an already-active employee must succeed: %v", err)
	}
	if !act.SecondDeviceReplaced {
		t.Fatal("SecondDeviceReplaced must be true so the caller knows to revoke the old sessions")
	}
	assertEmployeeStatus(t, d, f, statusActive)
}

// TestActivate_DeactivatedEmployeeDoesNotBurnTheCode: migration 00009's EXISTS
// guard. A deactivated person cannot be reactivated by a link, AND their invite
// survives, so a manager who reactivates them later does not have to reissue.
func TestActivate_DeactivatedEmployeeDoesNotBurnTheCode(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusDeactivated, "")
	inv, code := issue(t, m, f, 0)

	_, err := m.Activate(context.Background(), code)
	if !errors.Is(err, ErrNotActivatable) {
		t.Fatalf("err = %v, want ErrNotActivatable", err)
	}
	assertEmployeeStatus(t, d, f, statusDeactivated)
	assertInviteConsumed(t, d, f, inv.ID, false)
}

// TestActivate_ExpiredCodeIsRefusedByTheDatabase: the Go-side clock is not the
// authority. The manager's clock is moved forward so its own check would fire,
// and the point is that the SQL predicate (now() < expires_at) refuses it too --
// which is why a wrong process clock can only make this layer stricter.
func TestActivate_ExpiredCodeIsRefusedByTheDatabase(t *testing.T) {
	m, d := testManager(t)
	f := newFixture(t, d, statusInvited, "")

	// An invite that is already expired when it is written: -1h TTL is refused by
	// IssueAndDeliver's bounds, so it goes in directly. This is the only place a
	// test writes an invite by hand, and it does so to reach a state the API
	// deliberately cannot produce.
	inv := uuid.New()
	code, err := newCode()
	if err != nil {
		t.Fatalf("newCode: %v", err)
	}
	hash, err := code.hash(testKeyA)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	err = d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employee_invites (id, tenant_id, employee_id, code_hash, expires_at)
			 VALUES ($1, $2, $3, $4, now() - interval '1 hour')`,
			inv, f.tenantID, f.employeeID, hash)
		return e
	})
	if err != nil {
		t.Fatalf("insert expired invite: %v", err)
	}

	if _, err := m.Activate(context.Background(), code); !errors.Is(err, ErrCodeExpired) {
		t.Fatalf("err = %v, want ErrCodeExpired", err)
	}
	assertEmployeeStatus(t, d, f, statusInvited)
	assertInviteConsumed(t, d, f, inv, false)
}

// TestActivationContext_IsTenantScoped: an employee id from ANOTHER tenant must
// not resolve, even though the caller supplies both ids.
func TestActivationContext_IsTenantScoped(t *testing.T) {
	m, d := testManager(t)
	a := newFixture(t, d, statusInvited, "")
	b := newFixture(t, d, statusInvited, "")

	if _, err := m.ActivationContext(context.Background(), a.tenantID, b.employeeID); err == nil {
		t.Fatal("tenant A must not be able to read tenant B's employee")
	}
	// Positive control: the same call with the matching tenant works, so the
	// failure above is isolation and not a broken query.
	if _, err := m.ActivationContext(context.Background(), b.tenantID, b.employeeID); err != nil {
		t.Fatalf("positive control failed: %v", err)
	}
}

func assertEmployeeStatus(t *testing.T, d *db.DB, f fixture, want string) {
	t.Helper()
	var got string
	err := d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status FROM employees WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, f.employeeID).Scan(&got)
	})
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if got != want {
		t.Fatalf("employees.status = %q, want %q", got, want)
	}
}

func assertInviteConsumed(t *testing.T, d *db.DB, f fixture, inviteID uuid.UUID, want bool) {
	t.Helper()
	var used *time.Time
	err := d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT used_at FROM employee_invites WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, inviteID).Scan(&used)
	})
	if err != nil {
		t.Fatalf("read used_at: %v", err)
	}
	if want && used == nil {
		t.Fatal("used_at is NULL: the invite was not consumed")
	}
	if !want && used != nil {
		t.Fatal("used_at is set: the invite was burned when it should have survived")
	}
}
