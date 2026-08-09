package adminauth

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// manager_db_test.go -- the panel authentication proofs, against a REAL Postgres
// (CLAUDE.md section 8 / Q04). RLS, the two SECURITY DEFINER resolvers and the
// in-SQL status guards cannot be proven against a fake database, and the one that
// matters most here -- the cross-tenant authentication bypass migration 00011
// measured -- is only meaningful when the rows really exist in two tenants.
//
// Fixtures are NOT cleaned up, for the reason internal/db's tests give: tappa_app
// has REVOKE DELETE on the admin tables, so the impossibility of teardown IS the
// audit guarantee. Every fixture uses a fresh random UUID and a fresh random
// email, so repeated runs never collide.
//
// No test here holds a real credential: passwords are literals written in this
// file and used only to build a digest that lives for the length of one test.

func testDB(t *testing.T) *db.DB {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping adminauth DB tests (real Postgres required). " +
			"Run `make test`, which loads .env — a bare `go test` proves nothing about RLS.")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: withSmallPool(raw)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

func testManager(t *testing.T, d *db.DB) *Manager {
	t.Helper()
	m, err := New(d, testConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// newTenantRow creates a committed tenant as tappa_app, inside its own context.
func newTenantRow(t *testing.T, d *db.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := d.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, $2, $3, 'bar', 'single')`,
			id, name, "VAT-"+id.String())
		return e
	})
	if err != nil {
		t.Fatalf("newTenantRow: %v", err)
	}
	return id
}

// fixtureDigest builds a REAL bcrypt digest at bcrypt.MinCost instead of at Cost.
//
// 🔬 MEASURED, AND IT IS THE DIFFERENCE BETWEEN A SUITE THAT RUNS AND ONE THAT
// TIMES OUT. `make test` runs with -race, and the race detector instruments every
// memory access in blowfish's key schedule: ONE cost-12 comparison measured
// 11.34 s under -race against 0.60 s without, a 19x slowdown. The first version of
// this file called Hash (cost 12) once per fixture and once per Authenticate, and
// internal/adminauth hit Go's 10-minute per-package timeout at 609 s -- a real
// failure, caused by this file.
//
// NOTHING IS WEAKENED BY THE LOWER COST HERE, and that is the reason it is
// legitimate rather than a shortcut: a bcrypt digest CARRIES ITS OWN COST, so
// CompareHashAndPassword reads cost 4 out of the string and verifies exactly the
// same way it verifies a cost-12 row. These tests measure RESOLUTION, VERIFICATION
// OUTCOMES and TENANT ISOLATION, none of which is a function of the work factor.
//
// THE ONE THING THAT DOES DEPEND ON THE COST is the timing obligation, and it is
// measured at the SHIPPED cost in manager_timing_test.go -- never here.
func fixtureDigest(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	return string(h)
}

// newAdminRow inserts an admin_users row carrying a REAL bcrypt digest of
// password. There is deliberately no CreateAdminUser query yet (panel-side admin
// management is M6-05), so the fixture writes the row directly.
func newAdminRow(t *testing.T, d *db.DB, tenantID uuid.UUID, email, password, status, role, fullName string) uuid.UUID {
	t.Helper()
	digest := fixtureDigest(t, password)
	id := uuid.New()
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, tenantID, fullName, email, digest, role, status)
		return e
	})
	if err != nil {
		t.Fatalf("newAdminRow: %v", err)
	}
	return id
}

func randEmail(t *testing.T) string {
	t.Helper()
	// The lookup under test is GLOBAL, so a shared address would make two tests
	// see each other's rows.
	return "panel-" + uuid.NewString() + "@m6.example"
}

// TestAuthenticate_TheThreeOutcomes walks every row of PHASE B OBLIGATION 1
// against real data and asserts that all three produce the SAME sentinel.
func TestAuthenticate_TheThreeOutcomes(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	tenantID := newTenantRow(t, d, "Obligation One Ltd")
	const good = "the-right-password"
	activeEmail := randEmail(t)
	disabledEmail := randEmail(t)
	newAdminRow(t, d, tenantID, activeEmail, good, "active", "owner", "Active Owner")
	newAdminRow(t, d, tenantID, disabledEmail, good, "disabled", "manager", "Disabled Manager")

	tests := []struct {
		name     string
		email    string
		password string
	}{
		{"unknown email", randEmail(t), good},
		{"wrong password", activeEmail, "not-the-right-password"},
		{"disabled admin, correct password", disabledEmail, good},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth, err := m.Authenticate(ctx, tc.email, tc.password)
			if !errors.Is(err, ErrBadCredentials) {
				t.Fatalf("err = %v, want ErrBadCredentials", err)
			}
			if got := len(auth.Verified()); got != 0 {
				t.Fatalf("Verified() returned %d identities", got)
			}
		})
	}

	// POSITIVE CONTROL: the same manager, the same tenant, the right credentials.
	// Without this, all three rows above could be passing because Authenticate
	// never succeeds.
	t.Run("control: the right password on an active account", func(t *testing.T) {
		auth, err := m.Authenticate(ctx, activeEmail, good)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		v := auth.Verified()
		if len(v) != 1 {
			t.Fatalf("Verified() returned %d identities, want 1", len(v))
		}
		if v[0].TenantID != tenantID {
			t.Fatalf("verified tenant %v, want %v", v[0].TenantID, tenantID)
		}
	})
}

// TestAuthenticate_IsCaseInsensitive. Migration 00011 measured the citext trap:
// under the pinned search_path the resolver silently becomes case SENSITIVE
// unless the operator is schema-qualified, and an owner typing their own address
// in lower case could not log in. This is that guarantee, from the login path.
func TestAuthenticate_IsCaseInsensitive(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	tenantID := newTenantRow(t, d, "Citext Ltd")
	const good = "the-right-password"

	mixed := "Owner-" + uuid.NewString() + "@M6.Example"
	newAdminRow(t, d, tenantID, mixed, good, "active", "owner", "Mixed Case Owner")

	tests := []struct{ name, email string }{
		{"exactly as stored", mixed},
		{"all lower case", lower(mixed)},
		{"all upper case", upper(mixed)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth, err := m.Authenticate(context.Background(), tc.email, good)
			if err != nil {
				t.Fatalf("Authenticate: %v", err)
			}
			if len(auth.Verified()) != 1 {
				t.Fatalf("Verified() returned %d identities, want 1", len(auth.Verified()))
			}
		})
	}
}

func lower(s string) string { return mapASCII(s, 'A', 'Z', 32) }
func upper(s string) string { return mapASCII(s, 'a', 'z', -32) }

func mapASCII(s string, lo, hi byte, delta int) string {
	b := []byte(s)
	for i, c := range b {
		if c >= lo && c <= hi {
			b[i] = byte(int(c) + delta)
		}
	}
	return string(b)
}

// 🔴 TestAuthenticate_RefusesTheCrossTenantBypass is PHASE B OBLIGATION 5, driven
// as the LIVE ATTACK migration 00011 measured rather than as a unit assertion.
//
// THE SETUP IS THE ATTACK, exactly: an attacker owns a tenant (which M7-02's public
// sign-up wizard will make free) and writes the VICTIM'S EMAIL, with a password
// THEY know, into their OWN admin_users. RLS permits it — it is their tenant.
//
// THE MEASUREMENT: logging in with that address resolves TWO candidates, and the
// password matches the ATTACKER's row. Everything below asserts what the verified
// set contains, because the verified set is what the picker is built from and what
// Issue accepts.
func TestAuthenticate_RefusesTheCrossTenantBypass(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	victimTenant := newTenantRow(t, d, "VictimCo")
	attackerTenant := newTenantRow(t, d, "AttackerCo")

	email := randEmail(t)
	const victimPassword = "the-victims-own-password"
	const attackerPassword = "the-attackers-own-password"

	victimAdmin := newAdminRow(t, d, victimTenant, email, victimPassword, "active", "owner", "Victim Owner")
	attackerAdmin := newAdminRow(t, d, attackerTenant, email, attackerPassword, "active", "owner", "Attacker Owner")

	// The resolver really does return both — otherwise the test would be passing
	// because the attack was never set up.
	candidates, err := d.GetAdminByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetAdminByEmail: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("control: the resolver returned %d candidates, want 2 — the attack is not set up", len(candidates))
	}

	t.Run("the attacker's password verifies ONLY the attacker's identity", func(t *testing.T) {
		auth, err := m.Authenticate(ctx, email, attackerPassword)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		if len(auth.Attempts) != 2 {
			t.Fatalf("compared %d candidates, want 2", len(auth.Attempts))
		}
		v := auth.Verified()
		if len(v) != 1 {
			t.Fatalf("Verified() returned %d identities, want exactly 1 — the picker is built from "+
				"this list and a second entry IS the cross-tenant bypass", len(v))
		}
		if v[0].TenantID != attackerTenant || v[0].AdminUserID != attackerAdmin {
			t.Fatalf("verified identity is %v/%v, want the attacker's %v/%v",
				v[0].TenantID, v[0].AdminUserID, attackerTenant, attackerAdmin)
		}
		// The victim's tenant must not be anywhere in the verified set.
		for _, got := range v {
			if got.TenantID == victimTenant {
				t.Fatalf("the VICTIM's tenant is in the verified set — section 4.5 cross-tenant bypass")
			}
		}
	})

	t.Run("and symmetrically, the victim's password verifies only the victim", func(t *testing.T) {
		auth, err := m.Authenticate(ctx, email, victimPassword)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		v := auth.Verified()
		if len(v) != 1 || v[0].TenantID != victimTenant || v[0].AdminUserID != victimAdmin {
			t.Fatalf("verified set = %+v, want exactly the victim's identity", v)
		}
	})

	t.Run("the SAME password in both businesses is the picker's real case", func(t *testing.T) {
		// This is why "stop at the first match" was rejected: a person who owns two
		// businesses very likely uses one password, and the picker must offer both.
		shared := randEmail(t)
		const same = "one-password-two-businesses"
		a := newAdminRow(t, d, victimTenant, shared, same, "active", "owner", "Shared Owner A")
		b := newAdminRow(t, d, attackerTenant, shared, same, "active", "manager", "Shared Owner B")
		auth, err := m.Authenticate(ctx, shared, same)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		v := auth.Verified()
		if len(v) != 2 {
			t.Fatalf("Verified() returned %d identities, want 2 — the picker exists for this case", len(v))
		}
		found := map[uuid.UUID]bool{}
		for _, got := range v {
			found[got.AdminUserID] = true
		}
		if !found[a] || !found[b] {
			t.Fatalf("verified set %+v does not carry both identities", v)
		}
	})
}

// TestTenantChoices_OnlyShowsWhatVerified is the picker's half of OBLIGATION 5:
// the display data is read per VERIFIED identity, in that tenant's own context.
//
// It also measures the claim db/queries/admins.sql asks phase B to size: the loop
// is bounded by the verified set, so the 500 transactions 00011 worried about are
// not reachable.
func TestTenantChoices_OnlyShowsWhatVerified(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	tenantA := newTenantRow(t, d, "Choice Alpha")
	tenantB := newTenantRow(t, d, "Choice Beta")
	email := randEmail(t)
	const pw = "one-password-two-businesses"
	adminA := newAdminRow(t, d, tenantA, email, pw, "active", "owner", "Alpha Owner")
	newAdminRow(t, d, tenantB, email, pw, "active", "manager", "Beta Manager")

	auth, err := m.Authenticate(ctx, email, pw)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	verified := auth.Verified()
	if len(verified) != 2 {
		t.Fatalf("control: Verified() returned %d, want 2", len(verified))
	}

	choices, err := m.TenantChoices(ctx, verified)
	if err != nil {
		t.Fatalf("TenantChoices: %v", err)
	}
	if len(choices) != 2 {
		t.Fatalf("TenantChoices returned %d rows, want 2", len(choices))
	}
	byTenant := map[uuid.UUID]Choice{}
	for _, c := range choices {
		byTenant[c.TenantID] = c
	}
	if got := byTenant[tenantA]; got.TenantName != "Choice Alpha" || got.Role != "owner" {
		t.Fatalf("tenant A choice = %+v, want name 'Choice Alpha' role 'owner'", got)
	}
	if got := byTenant[tenantB]; got.TenantName != "Choice Beta" || got.Role != "manager" {
		t.Fatalf("tenant B choice = %+v, want name 'Choice Beta' role 'manager'", got)
	}

	t.Run("a single verified identity yields a single row", func(t *testing.T) {
		one, err := m.TenantChoices(ctx, []Verified{{AdminUserID: adminA, TenantID: tenantA}})
		if err != nil {
			t.Fatalf("TenantChoices: %v", err)
		}
		if len(one) != 1 {
			t.Fatalf("returned %d rows, want 1", len(one))
		}
	})

	t.Run("an identity that was disabled in between is dropped, not offered", func(t *testing.T) {
		disabledTenant := newTenantRow(t, d, "Disabled Later Ltd")
		disabledAdmin := newAdminRow(t, d, disabledTenant, randEmail(t), pw, "disabled", "owner", "Gone")
		got, err := m.TenantChoices(ctx, []Verified{{AdminUserID: disabledAdmin, TenantID: disabledTenant}})
		if err != nil {
			t.Fatalf("TenantChoices: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("returned %d rows for a disabled admin, want 0 — the picker must not show a "+
				"door CreateAdminSession would refuse to open", len(got))
		}
	})
}

// TestIssueAndVerify_RoundTrip: a session issued for a verified identity resolves
// on the next request, carries the right tenant and role, and stamps last_login_at.
func TestIssueAndVerify_RoundTrip(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	tenantID := newTenantRow(t, d, "Round Trip Ltd")
	email := randEmail(t)
	const pw = "the-right-password"
	adminID := newAdminRow(t, d, tenantID, email, pw, "active", "owner", "Round Owner")

	// last_login_at starts NULL.
	if got := lastLoginAt(t, d, tenantID, adminID); got != nil {
		t.Fatalf("control: last_login_at is %v before any login, want NULL", got)
	}

	auth, err := m.Authenticate(ctx, email, pw)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	issued, err := m.Issue(ctx, auth.Verified()[0])
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if issued.Session.TenantID != tenantID || issued.Session.AdminUserID != adminID {
		t.Fatalf("session is %+v, want tenant %v admin %v", issued.Session, tenantID, adminID)
	}
	if issued.Session.RevokedAt != nil {
		t.Fatalf("a fresh session is already revoked")
	}
	if got := lastLoginAt(t, d, tenantID, adminID); got == nil {
		t.Fatalf("last_login_at was not stamped by Issue")
	}

	res, err := m.Verify(ctx, issued.Token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	switch {
	case res.SessionID != issued.Session.ID:
		t.Fatalf("session id %v, want %v", res.SessionID, issued.Session.ID)
	case res.TenantID != tenantID:
		t.Fatalf("tenant %v, want %v", res.TenantID, tenantID)
	case res.AdminUserID != adminID:
		t.Fatalf("admin %v, want %v", res.AdminUserID, adminID)
	case res.Role != "owner":
		t.Fatalf("role %q, want owner", res.Role)
	case res.FullName != "Round Owner":
		t.Fatalf("full name %q", res.FullName)
	}

	t.Run("revoking it stops the next request", func(t *testing.T) {
		if err := m.Revoke(ctx, tenantID, issued.Session.ID); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		if _, err := m.Verify(ctx, issued.Token); !errors.Is(err, ErrNoSession) {
			t.Fatalf("Verify after revoke: err = %v, want ErrNoSession", err)
		}
	})
}

// TestVerify_RefusesADisabledAdminWithoutRevocation is the guarantee
// db/queries/admins.sql claims for TouchAdminSession: ONE row change kills every
// session that admin holds, present and future, without touching admin_sessions.
func TestVerify_RefusesADisabledAdminWithoutRevocation(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	tenantID := newTenantRow(t, d, "Kill Switch Ltd")
	email := randEmail(t)
	const pw = "the-right-password"
	adminID := newAdminRow(t, d, tenantID, email, pw, "active", "manager", "Soon Disabled")

	auth, err := m.Authenticate(ctx, email, pw)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	issued, err := m.Issue(ctx, auth.Verified()[0])
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	// CONTROL: it works before the flag flips.
	if _, err := m.Verify(ctx, issued.Token); err != nil {
		t.Fatalf("control: Verify failed before the admin was disabled: %v", err)
	}

	if err := d.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1 AND tenant_id = $2`,
			adminID, tenantID)
		return e
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	if _, err := m.Verify(ctx, issued.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a disabled admin still passes Verify: err = %v, want ErrNoSession", err)
	}
	// The session row itself was NOT revoked — the refusal comes from the join.
	if revoked := sessionRevokedAt(t, d, tenantID, issued.Session.ID); revoked != nil {
		t.Fatalf("the session was revoked (%v); the point is that it was not", revoked)
	}
}

// TestIssue_RefusesIncompleteIdentities covers the two shapes that would mean a
// caller built a Verified by hand rather than getting one from Verified().
func TestIssue_RefusesIncompleteIdentities(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Incomplete Ltd")
	adminID := newAdminRow(t, d, tenantID, randEmail(t), "the-right-password", "active", "owner", "Owner")

	tests := []struct {
		name string
		v    Verified
	}{
		{"the zero value", Verified{}},
		{"no tenant", Verified{AdminUserID: adminID}},
		{"no admin", Verified{TenantID: tenantID}},
		{"a real admin paired with somebody else's tenant", Verified{AdminUserID: adminID, TenantID: uuid.New()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := m.Issue(ctx, tc.v); err == nil {
				t.Fatalf("Issue accepted %s", tc.name)
			}
		})
	}
}

// TestVerify_RefusesAForeignToken is M1-11's structural separation measured from
// the panel side: a token issued for an EMPLOYEE session cannot resolve as an
// admin session, because the two live in different tables reached by different
// functions and hashed under different keys.
func TestVerify_RefusesAForeignToken(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)

	tests := []struct {
		name  string
		value string
	}{
		{"a well-formed token this deployment never issued", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"a value of the wrong shape", "nope"},
		{"empty", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := m.Verify(context.Background(), wrap(tc.value)); !errors.Is(err, ErrNoSession) {
				t.Fatalf("err = %v, want ErrNoSession", err)
			}
		})
	}
}

func lastLoginAt(t *testing.T, d *db.DB, tenantID, adminID uuid.UUID) *string {
	t.Helper()
	var out *string
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetAdminByID(ctx, store.GetAdminByIDParams{ID: adminID, TenantID: tenantID})
		if e != nil {
			return e
		}
		if row.LastLoginAt != nil {
			s := row.LastLoginAt.String()
			out = &s
		}
		return nil
	})
	if err != nil {
		t.Fatalf("GetAdminByID: %v", err)
	}
	return out
}

func sessionRevokedAt(t *testing.T, d *db.DB, tenantID, sessionID uuid.UUID) *string {
	t.Helper()
	var out *string
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var revoked *string
		e := tx.QueryRow(ctx,
			`SELECT revoked_at::text FROM admin_sessions WHERE id = $1 AND tenant_id = $2`,
			sessionID, tenantID).Scan(&revoked)
		if errors.Is(e, pgx.ErrNoRows) {
			return errors.New("session row vanished")
		}
		out = revoked
		return e
	})
	if err != nil {
		t.Fatalf("read revoked_at: %v", err)
	}
	return out
}

// withSmallPool caps this package's test pool.
// 🔬 THE POOL IS DELIBERATELY SMALL, AND THE REASON IS ARITHMETIC ABOUT THE WHOLE
// SUITE, not about this package.
//
// Postgres here runs max_connections=100 with superuser_reserved_connections=3, so
// 97 slots are available to tappa_app. Two PRE-EXISTING red-line race tests each
// open a pool of 54 (50 racing goroutines + 4):
//
//	internal/db/invites_test.go   raceInviteGoroutines = 50  (section 4.4 single-use)
//	internal/sun/advance_test.go  raceGoroutines       = 50  (section 4.4 replay)
//
// 54 + 54 = 108 > 97 ON THEIR OWN, so whenever those two packages overlap the
// suite is already over the limit. Measured once during M6-01 phase B: a full
// `make test` failed with "FATAL: sorry, too many clients already (SQLSTATE
// 53300)" inside the invite race, while this package was running concurrently.
//
// THIS PACKAGE IS NOT THE CAUSE and could not be — but it is one more concurrent
// pool, and pgx's default is max(4, NumCPU), which on this machine is more than
// this package will ever use: every test here is sequential. Capping it at 4 keeps
// M6-01's marginal contribution to the peak as small as it can honestly be made.
// The underlying collision belongs to internal/db and internal/sun and is NOT
// fixed here: lowering either goroutine count would weaken a red-line test.
func withSmallPool(dsn string) string {
	if strings.Contains(dsn, "pool_max_conns") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "pool_max_conns=4"
}

// 🔴 TestAuthenticate_RefusesAnUnlookupableAddress_RealDB is the net the fake could
// never be.
//
// WHY THIS FILE AND NOT manager_timing_test.go. That file's twin drives
// newFakeManager, whose resolver ACCEPTS EVERY BYTE SEQUENCE — so with the
// isLookupableEmail guard deleted it still returns zero candidates, still pays the
// dummy, still yields ErrBadCredentials, and still passes. A closing audit deleted
// the guard and measured the result: the WHOLE SUITE stayed green, while the real
// stack answered HTTP 500 in 2.2-3.0 ms against 401 in 308 ms for a known address
// — a 140x spread with a different status AND a different body. The test named the
// branch and never saw it.
//
// A SCAN OF THE SAME SHAPE: six top-level tests in this package drive
// newFakeManager (eight call sites). Five of them are legitimate — they measure
// bcrypt cost, the candidate-loop bound and the dummy, none of which the database
// has any say in. This was the one that asserted something ONLY Postgres can
// refuse, and it is the third time in this task that a fix's own test net turned
// out to be blind.
//
// WHAT POSTGRES ACTUALLY DOES with these bytes, which is the whole point:
//
//	ERROR: invalid byte sequence for encoding "UTF8": 0x00 (SQLSTATE 22021)
//
// so without the guard the driver error propagates and the handler reports 500.
func TestAuthenticate_RefusesAnUnlookupableAddress_RealDB(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	ctx := context.Background()

	// A real tenant and admin, so the resolver has something to find for the
	// control below — this must not pass because the database is empty.
	tenantID := newTenantRow(t, d, "Unlookupable Ltd")
	email := randEmail(t)
	const pw = "the-right-password"
	newAdminRow(t, d, tenantID, email, pw, "active", "owner", "Lookup Owner")

	tests := []struct {
		name  string
		email string
	}{
		// THREE ROWS, ONE PER CLASS, AND THE COUNT IS A COST DECISION. Each row
		// pays a real cost-12 dummy comparison (~2.3 s under -race), so five rows
		// cost `make test-short` ~5 s for no extra coverage: rows 1 and 3 are the
		// NUL class (which Go considers valid UTF-8, so it needs its own check) and
		// row 2 is the invalid-UTF-8 class. The NUL-only sub-mutation still reddens
		// exactly rows 1 and 3 and leaves row 2 green, which is how the two halves
		// of isLookupableEmail are told apart.
		{"a NUL byte", "victim\x00@example.test"},
		{"invalid UTF-8 (lone 0xff)", "victim\xff@example.test"},
		{"NUL appended to a REAL address", email + "\x00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth, err := m.Authenticate(ctx, tc.email, "whatever")
			if !errors.Is(err, ErrBadCredentials) {
				t.Fatalf("err = %v, want ErrBadCredentials.\nWithout the isLookupableEmail "+
					"guard this is a WRAPPED DATABASE ERROR (SQLSTATE 22021), which the handler "+
					"reports as HTTP 500 — a different status and a different body from every "+
					"other refusal (PHASE B OBLIGATION 1).", err)
			}
			// It must be the sentinel, not a database failure dressed as one.
			if strings.Contains(err.Error(), "SQLSTATE") || strings.Contains(err.Error(), "resolve admin by email") {
				t.Fatalf("the error is a database failure, not a credential refusal: %v", err)
			}
			if len(auth.Attempts) != 0 {
				t.Fatalf("an unlookupable address produced %d attempts", len(auth.Attempts))
			}
		})
	}

	// POSITIVE CONTROL 1: the same manager, the same database, a REAL address —
	// the resolver is reached and the password verifies. Without this the table
	// above could pass because nothing works at all.
	t.Run("control: the real address still authenticates", func(t *testing.T) {
		auth, err := m.Authenticate(ctx, email, pw)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		if len(auth.Verified()) != 1 {
			t.Fatalf("Verified() returned %d identities, want 1", len(auth.Verified()))
		}
	})

	// POSITIVE CONTROL 2: a NON-ASCII but valid UTF-8 address reaches the resolver
	// and comes back as an ordinary unknown — the guard refuses bytes Postgres
	// refuses, not addresses that merely look unusual.
	t.Run("control: valid non-ASCII reaches the resolver", func(t *testing.T) {
		_, err := m.Authenticate(ctx, "öwner-"+uuid.NewString()+"@kebabfactory.mt", "whatever")
		if !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("a valid non-ASCII address failed differently: %v", err)
		}
	})
}
