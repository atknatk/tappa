package signup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// PROVISIONING, AGAINST REAL POSTGRES.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and each property below is one a mock would
// agree with unconditionally:
//
//   - RLS. A new tenant's rows must be invisible to every other tenant, and the
//     reason has to be the POLICY rather than a WHERE clause somebody wrote. CLAUDE.md
//     §6 is explicit that an isolation probe must NOT carry a tenant_id filter: with
//     one, zero rows proves the filter worked and the test stays green with RLS
//     switched off.
//   - THE SINGLE TRANSACTION. "No half tenant survives a failure" is a statement
//     about row counts after a rollback; only a database can count them.
//   - THE TENANT CONTEXT POINTING AT A ROW THAT DOES NOT EXIST YET. The `tenants`
//     policy scopes on `id`, so the INSERT only satisfies WITH CHECK because the
//     context already equals the id being written. Nothing but Postgres evaluates a
//     WITH CHECK.
//   - THE STORED DIGEST'S SHAPE. internal/adminauth names an admin row with a
//     non-bcrypt password_hash as a ~1.9-million-fold timing oracle and names M7-02
//     as one of the tasks that opens the write path. The schema does NOT enforce the
//     format (migration 00017 measures why it cannot be added today), so the
//     guarantee is "this write path only ever stores adminauth.Hash output" and this
//     is where that is checked against the row that was actually written.
//
// FIXTURES ARE NOT CLEANED UP, for the reason every other _db_test.go in this repo
// states: a tenant cannot be deleted by tappa_app (§4.6 and the grants), so a test
// that tried would be testing something the product refuses. Fresh random ids keep
// runs from colliding and `make db-reset` clears the development database.

// THE IDENTITIES A REGISTRATION BIRTHS ARE TAKEN BACK (M14-08). From migration 00074 on, Provision of
// a new address commits an identity and links the row to it (ADR 0031 §5.2 named exception 2). An
// identity left behind would make internal/db's Down walks refuse (00069's Down refuses while one
// exists) and, committed while those run, would change what they read -- so every fixture here holds
// internal/db's operator-tables test lock SHARED for the length of the test (cmd/opadmin's
// TestTablesLock_IsTakenOncePerTestTree lists this package) and, at the end, unlinks and deletes the
// identities of this package's addresses (signupIdentityAddresses). The tenants and rows stay, as
// every other fixture's do.

// operatorTablesTestLock is internal/db's schema-test lock; the spelling is read from its source
// (TestSignupFixtures_TakeTheLockInternalDBTakes).
const operatorTablesTestLock = "tappa/test/operator-tables"

// signupIdentityAddresses matches every address this package's fixtures register (draftFor) or plant.
const signupIdentityAddresses = "%@signup.example"

// sharedTablesLock takes the operator-tables lock SHARED on conn, held until conn closes.
func sharedTablesLock(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
}

// identityOwner is a dedicated owner connection holding the lock for the test, which unlinks and
// deletes this package's identities when the test ends. It returns the connection (the identity
// assertions read admin_users' link columns, which tappa_app may not).
func identityOwner(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; a registration births an identity and the fixture must take it back (real Postgres required)")
	}
	ctx := context.Background()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Errorf("close the owner connection: %v", err)
		}
	})
	sharedTablesLock(t, ctx, c)
	t.Cleanup(func() {
		for _, sql := range []string{
			`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL, unlinked_at = NULL
			  WHERE identity_id IN (SELECT id FROM admin_identities WHERE email LIKE $1)`,
			// M14-17b: the email_verified events a password claim of an unverified identity writes.
			`DELETE FROM admin_identity_events WHERE identity_id IN (SELECT id FROM admin_identities WHERE email LIKE $1)`,
			`DELETE FROM admin_identities WHERE email LIKE $1`,
			// M14-17: the drafts (a draft is not a record -- R17; the owner's cleanup, not the sweep's).
			`DELETE FROM admin_signup_claims WHERE email LIKE $1`,
		} {
			if _, err := c.Exec(context.Background(), sql, signupIdentityAddresses); err != nil {
				t.Errorf("take the registrations' identities back (%s): %v", sql, err)
			}
		}
	})
	return c
}

func newProvisionerFixture(t *testing.T) (*Provisioner, *db.DB) {
	t.Helper()
	p, data, _ := newProvisionerFixtureWithOwner(t)
	return p, data
}

// newProvisionerFixtureWithOwner is newProvisionerFixture for a test that also reads what only the
// owner may (an identity, a row's link) -- through the same locked connection.
func newProvisionerFixtureWithOwner(t *testing.T) (*Provisioner, *db.DB, *pgx.Conn) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping signup provisioning tests (real Postgres required)")
	}
	owner := identityOwner(t)
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	p, err := NewProvisioner(data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	return p, data, owner
}

// THE CONFIRMED DRAFT (M14-17). Provision takes a Claim -- in production adminauth.ClaimProof, which
// exists only after the re-entered password matched or with the address's own session (S12). These
// fixtures open the draft through the real identity_open_signup_claim and claim it through the real
// identity_claim_signup (db.ClaimSignup), with a MinCost digest: the shipped cost is adminauth's to
// hold (TestSignupClaims_OpenHashesAtTheShippedCost), and a cost-12 bcrypt is ~11 s under -race.

// fixturePassword / fixtureDigest are the drafts' password and its MinCost digest.
const fixturePassword = "a passphrase nobody guesses"

var fixtureDigest = func() string {
	b, err := bcrypt.GenerateFromPassword([]byte(fixturePassword), bcrypt.MinCost)
	if err != nil {
		panic(err)
	}
	return string(b)
}()

// testClaim is a confirmed draft: its hash, and the session of an existing identity (nil: a new address)
// or (M14-17b) the digest of an existing identity with no active membership (zero: none).
type testClaim struct {
	hash     string
	session  *string
	verified db.PasswordHash
	draft    []byte
}

func (c testClaim) Draft() []byte { return c.draft }

func (c testClaim) Claim(ctx context.Context, tx pgx.Tx, tenant uuid.UUID) (uuid.UUID, error) {
	return db.ClaimSignup(ctx, tx, c.hash, tenant, c.session, c.verified)
}

// rawDraft encodes d's closed keys WITHOUT validating them, so a test can store what the wizard would
// refuse and watch Provision refuse it.
func rawDraft(t *testing.T, d Draft) []byte {
	t.Helper()
	venues := make([]string, 0, len(d.Venues))
	for _, v := range d.Venues {
		venues = append(venues, v.Name)
	}
	b, err := json.Marshal(claimDraft{FullName: d.Account.FullName, BusinessName: d.Business.Name, VATNumber: d.Business.VATNumber,
		BusinessType: d.Business.BusinessType, Structure: d.Business.Structure, VATCheck: d.Business.VAT.String(), Venues: venues})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// openClaim opens d's draft for d.Account.Email through identity_open_signup_claim.
func openClaim(t *testing.T, data *db.DB, d Draft) testClaim {
	t.Helper()
	h := make([]byte, 32)
	if _, err := rand.Read(h); err != nil {
		t.Fatal(err)
	}
	c := testClaim{hash: hex.EncodeToString(h), draft: rawDraft(t, d)}
	if _, err := data.OpenSignupClaim(context.Background(), c.hash, d.Account.Email, c.draft, fixtureDigest, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("open the draft: %v", err)
	}
	return c
}

// provisionDraft opens d's draft and confirms it as a new address (Provision's real path).
func provisionDraft(t *testing.T, p *Provisioner, d Draft) (Result, error) {
	t.Helper()
	return p.Provision(context.Background(), openClaim(t, p.data.(*db.DB), d))
}

// ownerData opens a pool connecting as the MIGRATION role.
//
// 🔴 IT EXISTS BECAUSE MIGRATION 00017 TOOK A PRIVILEGE AWAY THAT THESE FIXTURES NEED,
// WHICH IS THE RIGHT OUTCOME RATHER THAN AN INCONVENIENCE. The fixtures below plant
// BACKDATED admin_users rows — that is the whole point, since the login resolver orders
// by created_at and a planted row has to be older than the registration it is crowding
// out. 00017 revoked created_at from tappa_app's INSERT grant (and from its UPDATE
// grant) precisely so nothing running as the application can choose a row's position in
// that order. A test fixture doing something the application is no longer allowed to do
// should have to say so out loud.
//
// internal/db/rls_test.go's ownerDB carries the same argument for the same reason;
// redline R5 forbids the migration role in PRODUCTION code and excludes _test.go, so
// the env key is a plain literal here.

// draftFor builds a registration with a VAT number nothing else uses.
func draftFor(t *testing.T, structure string, venues ...string) Draft {
	t.Helper()
	// A Maltese number is MT + eight digits; the eight come from a fresh uuid so
	// concurrent runs cannot collide on the global UNIQUE constraint.
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, uuid.NewString())
	if len(digits) < 8 {
		t.Fatalf("could not build a VAT number from %q", digits)
	}
	vs := make([]Venue, 0, len(venues))
	for _, v := range venues {
		vs = append(vs, Venue{Name: v})
	}
	return Draft{
		Business: Business{
			Name:         "Probe Catering " + uuid.NewString()[:8],
			VATNumber:    "MT" + digits[:8],
			BusinessType: "restaurant",
			Structure:    structure,
			VAT:          VATValid,
		},
		Venues: vs,
		Account: Account{
			FullName: "Probe Owner",
			Email:    "probe-" + uuid.NewString() + "@signup.example",
			Password: "a passphrase nobody guesses",
		},
	}
}

// TestSignupProvision_CreatesTheWholeBusinessInOneTransaction.
func TestSignupProvision_CreatesTheWholeBusinessInOneTransaction(t *testing.T) {
	p, data := newProvisionerFixture(t)
	ctx := context.Background()

	d := draftFor(t, StructureMulti, "St Julians", "Sliema", "Valletta")
	res, err := provisionDraft(t, p, d)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if res.TenantID == uuid.Nil || res.AdminUserID == uuid.Nil {
		t.Fatal("Provision returned no ids")
	}
	if len(res.VenueNames) != 3 {
		t.Fatalf("created %d venue(s), want 3", len(res.VenueNames))
	}

	var (
		tenants, locations, admins, trail int
		plan, tz, vat                     string
		price                             string
		vatVerified                       *bool
		vatCheckedAt                      *string
		digest                            string
		role, status                      string
	)
	if err := data.WithTenant(ctx, res.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx,
			`SELECT count(*), max(plan), max(timezone), max(vat_number),
			        max(price_per_employee_month)::text, bool_and(vat_verified),
			        max(vat_checked_at)::text
			 FROM tenants WHERE id = $1`, res.TenantID).
			Scan(&tenants, &plan, &tz, &vat, &price, &vatVerified, &vatCheckedAt); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE tenant_id = $1`,
			res.TenantID).Scan(&locations); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx,
			`SELECT count(*), max(password_hash), max(role), max(status)
			 FROM admin_users WHERE tenant_id = $1`, res.TenantID).
			Scan(&admins, &digest, &role, &status); e != nil {
			return e
		}
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`,
			res.TenantID, ActionSignupCompleted).Scan(&trail)
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}

	if tenants != 1 || locations != 3 || admins != 1 || trail != 1 {
		t.Errorf("rows: tenants=%d locations=%d admins=%d audit=%d; want 1/3/1/1",
			tenants, locations, admins, trail)
	}
	// THE THREE COLUMNS THE APPLICATION HAS NO GRANT FOR took their DEFAULTs — which
	// is migration 00016's decision enforced by privilege rather than by this code.
	if plan != "founding" {
		t.Errorf("plan is %q; a new customer must get the founding offer the landing page sells", plan)
	}
	if price != "1.50" {
		t.Errorf("price_per_employee_month is %q, want the schema DEFAULT 1.50", price)
	}
	if tz != DefaultTimezone {
		t.Errorf("timezone is %q, want %q", tz, DefaultTimezone)
	}
	if vat != d.Business.VATNumber {
		t.Errorf("vat_number stored as %q, want the normalised %q", vat, d.Business.VATNumber)
	}
	if vatVerified == nil || !*vatVerified {
		t.Errorf("vat_verified is %v; the draft carried a VIES `valid`", vatVerified)
	}
	if vatCheckedAt == nil || *vatCheckedAt == "" {
		t.Error("vat_checked_at was not stamped, so 'we asked and it was valid' is " +
			"indistinguishable from 'we never asked'")
	}
	if role != "owner" || status != "active" {
		t.Errorf("the first operator is role=%q status=%q, want owner/active", role, status)
	}

	// 🔴 THE DIGEST IS THE DRAFT'S, BYTE FOR BYTE -- the one the form's password was hashed into when
	// the draft was opened (adminauth.SignupClaims.Open, at the shipped cost:
	// TestSignupClaims_OpenHashesAtTheShippedCost holds the cost there) and the one the confirmation's
	// re-entered password was compared with (S12). Never the clear text.
	if digest != fixtureDigest {
		t.Errorf("password_hash is not the draft's digest (it begins %q)", safePrefix(digest))
	}
	if digest == d.Account.Password {
		t.Fatal("the password was stored in the clear")
	}

	// THE OTHER TWO VIES ANSWERS, THROUGH THE SAME Provision (OP-16C, round 2). The
	// draft above carries VATValid only, so until round 2 nothing on real Postgres
	// read what a registration stores when VIES did not answer: a CreateTenant call
	// that wrote `false` for VATUnknown left every test green (measured, E07). Each
	// row registers one more business and reads its two VIES columns back — migration
	// 00017's "never asked / no answer" (NULL, NULL) and "the register says no" (false,
	// stamped).
	for _, tc := range []struct {
		name         string
		status       VATStatus
		wantVerified *bool
		wantStamped  bool
	}{
		{"the register could not be reached", VATUnknown, nil, false},
		{"the register does not know the number", VATInvalid, new(bool), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dv := draftFor(t, StructureSingle, "Front door")
			dv.Business.VAT = tc.status
			r, err := provisionDraft(t, p, dv)
			if err != nil {
				t.Fatalf("Provision: %v", err)
			}
			var (
				verified *bool
				stamped  bool
			)
			if err := data.WithTenant(ctx, r.TenantID, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx,
					`SELECT vat_verified, vat_checked_at IS NOT NULL FROM tenants WHERE id = $1`,
					r.TenantID).Scan(&verified, &stamped)
			}); err != nil {
				t.Fatalf("reading back: %v", err)
			}
			switch {
			case tc.wantVerified == nil && verified != nil:
				t.Errorf("a %v draft stored vat_verified = %v; no answer must be stored as NULL, "+
					"never as a verdict", tc.status, *verified)
			case tc.wantVerified != nil && (verified == nil || *verified != *tc.wantVerified):
				t.Errorf("a %v draft stored vat_verified = %v, want %v", tc.status, verified, *tc.wantVerified)
			}
			if stamped != tc.wantStamped {
				t.Errorf("a %v draft stamped vat_checked_at: %v, want %v", tc.status, stamped, tc.wantStamped)
			}
		})
	}
}

// safePrefix reports the first few bytes of a digest for a failure message. It is a
// SHAPE report and not a value: seven characters of a bcrypt string are the cost
// marker, and a non-bcrypt value is by definition not a digest of anything.
func safePrefix(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// TestSignupProvision_IsInvisibleToEveryOtherTenant — the §4.5 isolation probe.
//
// 🔴 IT CARRIES NO tenant_id FILTER, AND THAT IS THE WHOLE POINT (CLAUDE.md §6).
// A production query must carry one — belt and braces — and an ISOLATION probe must
// not: with a filter, zero rows proves the WHERE worked and the test stays green with
// RLS switched off. Measured that way in this repo before, which is why the rule is
// written down.
//
// IT COMES WITH ITS POSITIVE CONTROL: the same filter-less reads run inside the NEW
// tenant's own context and must find the rows, so an empty result cannot be mistaken
// for isolation.
func TestSignupProvision_IsInvisibleToEveryOtherTenant(t *testing.T) {
	p, data := newProvisionerFixture(t)
	ctx := context.Background()

	newBusiness, err := provisionDraft(t, p, draftFor(t, StructureSingle, "Front door"))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	// A SECOND, UNRELATED business, created the same way. It is the attacker's seat:
	// a real tenant with a real context, not a made-up uuid.
	other, err := provisionDraft(t, p, draftFor(t, StructureSingle, "Their door"))
	if err != nil {
		t.Fatalf("Provision (the other business): %v", err)
	}

	// --- the probe: inside the OTHER tenant's context, look for the new one -------
	type counts struct{ tenants, locations, admins, trail int }
	var seen counts
	if err := data.WithTenant(ctx, other.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		// NO tenant_id PREDICATE ANYWHERE BELOW. The only thing that can return zero
		// here is the row-level policy.
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE vat_number = $1`,
			newBusinessVAT(t, data, newBusiness.TenantID)).Scan(&seen.tenants); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE name = 'Front door'`).
			Scan(&seen.locations); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE id = $1`,
			newBusiness.AdminUserID).Scan(&seen.admins); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE target = $1`,
			newBusiness.TenantID.String()).Scan(&seen.trail)
	}); err != nil {
		t.Fatalf("the isolation probe: %v", err)
	}
	if seen != (counts{}) {
		t.Errorf("a different tenant's context can see the new business: %+v.\n"+
			"This is §4.5: RLS, not a WHERE clause, is what must return nothing here.", seen)
	}

	// --- the positive control: the SAME filter-less reads, in the right context ---
	var own counts
	if err := data.WithTenant(ctx, newBusiness.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM tenants`).Scan(&own.tenants); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE name = 'Front door'`).
			Scan(&own.locations); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE id = $1`,
			newBusiness.AdminUserID).Scan(&own.admins); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE target = $1`,
			newBusiness.TenantID.String()).Scan(&own.trail)
	}); err != nil {
		t.Fatalf("the positive control: %v", err)
	}
	if own.tenants != 1 || own.locations != 1 || own.admins != 1 || own.trail != 1 {
		t.Errorf("the new tenant cannot see its OWN rows without a filter: %+v.\n"+
			"Without this control, the zero above would prove nothing.", own)
	}
}

// newBusinessVAT reads a tenant's VAT number from inside its own context, so the
// isolation probe above can look for a value that genuinely identifies one row.
func newBusinessVAT(t *testing.T, data *db.DB, tenantID uuid.UUID) string {
	t.Helper()
	var vat string
	if err := data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT vat_number FROM tenants WHERE id = $1`, tenantID).Scan(&vat)
	}); err != nil {
		t.Fatalf("reading the VAT number back: %v", err)
	}
	return vat
}

// TestSignupProvision_TwoPendingDraftsWithOneVATNumber (ADR 0031 §5.10): a draft reserves nothing.
// Two drafts carry one VAT number; the first CONFIRMED gets the business, the second confirmation gets
// today's VAT conflict (ErrVATTaken) and LEAVES NOTHING BEHIND -- no venue, no member, no identity, and
// its draft unspent (it never reached the claim).
//
// 🔴 THE HALF-TENANT IS THE WORST OUTCOME AVAILABLE HERE and it is what M7-03's card asks about
// ("Başarısızlıkta yarım tenant kalmıyor (rollback)"). The duplicate VAT number fails on the FIRST
// statement, so this also proves the venues and the claim never ran.
func TestSignupProvision_TwoPendingDraftsWithOneVATNumber(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()

	first := draftFor(t, StructureSingle, "Front door")
	second := draftFor(t, StructureSingle, "Another door")
	second.Business.VATNumber = first.Business.VATNumber
	// Both drafts pending at once: neither confirmation has run.
	c1, c2 := openClaim(t, data, first), openClaim(t, data, second)

	firstRes, err := p.Provision(ctx, c1)
	if err != nil {
		t.Fatalf("the first confirmation: %v", err)
	}
	if _, err := p.Provision(ctx, c2); !errors.Is(err, ErrVATTaken) {
		t.Fatalf("the second confirmation returned %v, want ErrVATTaken", err)
	}

	var locations, admins int
	if err := data.WithTenant(ctx, firstRes.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE name = 'Another door'`).Scan(&locations); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&admins)
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if locations != 0 || admins != 1 {
		t.Errorf("the refused confirmation left locations=%d, and the first business holds %d operator(s) (want 0 and 1)", locations, admins)
	}
	var identities, spent int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identities WHERE email = $1`, second.Account.Email).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_signup_claims WHERE token_hash = $1 AND used_at IS NOT NULL`, c2.hash).Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if identities != 0 || spent != 0 {
		t.Errorf("the refused confirmation birthed %d identit(ies) and spent its draft (%d): it must leave nothing", identities, spent)
	}
}

// TestSignupProvision_RefusesADraftThatDidNotValidateAndWritesNothing.
//
// Provision is the function that WRITES, so it re-checks the stored draft rather than trusting
// whichever caller opened it -- a rule enforced only by the wizard is a rule the next caller does not
// inherit. The drafts below pass the table's shape CHECK (00078) and fail the domain's rules.
//
// THE PROOF IS THE VAT NUMBER (globally UNIQUE): if a refused draft had created a tenant, provisioning
// the SAME number afterwards would fail. It succeeds.
func TestSignupProvision_RefusesADraftThatDidNotValidateAndWritesNothing(t *testing.T) {
	p, data := newProvisionerFixture(t)
	ctx := context.Background()

	bad := draftFor(t, StructureSingle, "Front door")
	bad.Business.VATNumber = "not-a-vat-number"
	if _, err := provisionDraft(t, p, bad); !errors.Is(err, ErrDraftInvalid) {
		t.Fatalf("Provision accepted a draft with an invalid VAT number (%v)", err)
	}
	twoDoors := draftFor(t, StructureSingle, "Front door", "Back door")
	if _, err := provisionDraft(t, p, twoDoors); !errors.Is(err, ErrDraftInvalid) {
		t.Fatalf("Provision accepted two venues for a single-place business (%v)", err)
	}
	// An unknown key cannot reach the table (its CHECK), so it is offered directly to Provision.
	extra := openClaim(t, data, draftFor(t, StructureSingle, "Front door"))
	extra.draft = []byte(strings.Replace(string(extra.draft), `{`, `{"password":"x",`, 1))
	if _, err := p.Provision(ctx, extra); !errors.Is(err, ErrDraftInvalid) {
		t.Fatalf("Provision accepted a draft with a key outside the closed set (%v)", err)
	}

	good := twoDoors
	good.Venues = good.Venues[:1]
	if _, err := provisionDraft(t, p, good); err != nil {
		t.Fatalf("the VAT number of a REFUSED registration is taken, so that registration wrote a tenant row after all: %v", err)
	}
}

// TestSignupProvision_AFailureInTheMiddleLeavesNothingBehind.
//
// HERE THE TRANSACTION GETS TO ITS LAST STATEMENT. The tenant row and its venues are written and then
// identity_claim_signup refuses (a spent draft), so the rollback takes the business back -- the shape
// M7-03's criterion is about ("Başarısızlıkta yarım tenant kalmıyor"), and the claim's refusal is
// ErrClaimRefused, never a half business.
//
// THE PROOF IS WITHOUT A TENANT CONTEXT: the owner (BYPASSRLS as superuser) counts the business by its
// VAT number across the whole database -- a row of the failed attempt carries the failed attempt's
// tenant id, invisible from any other tenant's context.
func TestSignupProvision_AFailureInTheMiddleLeavesNothingBehind(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()

	d := draftFor(t, StructureMulti, "Doomed door A", "Doomed door B")
	c := openClaim(t, data, d)
	if _, err := owner.Exec(ctx, `UPDATE admin_signup_claims SET used_at = now() WHERE token_hash = $1`, c.hash); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Provision(ctx, c); !errors.Is(err, ErrClaimRefused) {
		t.Fatalf("a spent draft returned %v, want ErrClaimRefused", err)
	}
	var tenants, locations int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE vat_number = $1`, d.Business.VATNumber).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM locations WHERE name LIKE 'Doomed door %' AND tenant_id NOT IN (SELECT id FROM tenants)`).Scan(&locations); err != nil {
		t.Fatal(err)
	}
	if tenants != 0 || locations != 0 {
		t.Fatalf("the refused confirmation left %d business(es) and %d orphan venue(s)", tenants, locations)
	}
	// And the same draft, opened afresh, provisions -- the number is free.
	if _, err := provisionDraft(t, p, d); err != nil {
		t.Fatalf("the VAT number of a rolled-back registration is taken: %v", err)
	}
}

// TestSignupProvision_AnAddressWithManyAccountsGetsOneIdentity is what retired signInBlocked
// (signup.sign_in_unreachable, M7-02 round 4): on the identity path an address names at most ONE
// identity (admin_identities.email is globally UNIQUE), so there is no candidate window for a new
// registration to fall outside of. An address already carrying adminauth.MaxCandidates+1 unlinked
// accounts, confirmed: one identity, the new owner row linked to it, and the md.7 resolver answers that
// one identity -- whatever the row count. (With TAPPA_ADMIN_IDENTITY off the legacy sign-in still
// reads the window; ADR 0031 §5.10/§8 -- the stuffing it needed now costs a mailbox per draft.)
func TestSignupProvision_AnAddressWithManyAccountsGetsOneIdentity(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()
	d := draftFor(t, StructureSingle, "Front door")
	for i := 0; i < adminauth.MaxCandidates+1; i++ {
		tenantID := uuid.New()
		if _, err := owner.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                              VALUES ($1, 'Crowd', $2, 'other', 'single')`, tenantID, "VAT-M1417C-"+tenantID.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(ctx, `INSERT INTO admin_users (tenant_id, full_name, email, password_hash, role, created_at)
		                              VALUES ($1, 'Crowd', $2, $3, 'owner', now() - interval '400 days')`, tenantID, d.Account.Email,
			"$2a$04$"+strings.Repeat("c", 53)); err != nil {
			t.Fatal(err)
		}
	}
	res, err := provisionDraft(t, p, d)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	ident, err := data.ResolveIdentityByEmail(ctx, d.Account.Email)
	if err != nil {
		t.Fatalf("the address resolves to no identity: %v", err)
	}
	var linked uuid.UUID
	if err := owner.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, res.AdminUserID).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked != ident.ID || !ident.HasActiveMembership {
		t.Fatalf("the new owner row is linked to %s, the address's identity is %s (active membership %v)", linked, ident.ID, ident.HasActiveMembership)
	}
}

// TestSignupProvision_TenantIDIsNeverTakenFromTheRequest.
//
// 🔴 THIS TEST IS NAMED IN THREE PLACES AND DID NOT EXIST. db/queries/tenants.sql
// asserts "TestSignupProvision_TenantIDIsNeverTakenFromTheRequest asserts it at the
// boundary", and sqlc copies that sentence into internal/store twice — a security
// claim citing a test a grep could not find. A security audit named it. The property
// is real (the audit proved it independently by injecting id/tenant_id into all three
// wizard steps and watching a fresh random id come out), but until now the only thing
// holding it was a SOURCE SCAN for the literal "uuid.NewRandom()", which stays green
// for an edit that keeps the string in the file and takes the id from a request field
// anyway.
//
// WHAT IT ASSERTS, structurally rather than by inspection:
//
//  1. Draft has NO field of type uuid.UUID at any depth — there is nothing for a
//     caller-supplied id to travel IN, whatever a handler does.
//  2. The SAME Draft provisioned twice yields two DIFFERENT tenant ids — so the id
//     is minted per call and cannot be a function of the input.
func TestSignupProvision_TenantIDIsNeverTakenFromTheRequest(t *testing.T) {
	p, _ := newProvisionerFixture(t)

	// 1. THE TYPE CANNOT CARRY ONE. Walked recursively: a uuid at any depth of Draft
	// would be a channel through which a request could name its own tenant.
	var walk func(reflect.Type, string)
	uuidType := reflect.TypeOf(uuid.UUID{})
	walk = func(typ reflect.Type, path string) {
		if typ == uuidType {
			t.Errorf("signup.Draft carries a uuid at %s. The tenant id must be minted by "+
				"Provision from crypto/rand and reachable from nowhere else — a field here "+
				"is a way for a request to name the tenant its transaction is scoped to.", path)
			return
		}
		switch typ.Kind() {
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				walk(f.Type, path+"."+f.Name)
			}
		case reflect.Slice, reflect.Array, reflect.Ptr:
			walk(typ.Elem(), path+"[]")
		case reflect.Map:
			walk(typ.Key(), path+"[key]")
			walk(typ.Elem(), path+"[val]")
		}
	}
	walk(reflect.TypeOf(Draft{}), "Draft")
	// M14-17: and the stored draft's closed keys carry none either -- the confirmation reads the
	// business from it.
	walk(reflect.TypeOf(claimDraft{}), "claimDraft")

	// ANTI-VACUITY: the walker must actually recognise a uuid where there is one.
	found := false
	walkProbe := struct {
		Nested struct{ ID uuid.UUID }
	}{}
	pt := reflect.TypeOf(walkProbe)
	var probe func(reflect.Type)
	probe = func(typ reflect.Type) {
		if typ == uuidType {
			found = true
			return
		}
		if typ.Kind() == reflect.Struct {
			for i := 0; i < typ.NumField(); i++ {
				probe(typ.Field(i).Type)
			}
		}
	}
	probe(pt)
	if !found {
		t.Fatal("the type walker does not recognise a nested uuid, so assertion 1 is vacuous")
	}

	// 2. THE SAME INPUT YIELDS A DIFFERENT TENANT EACH TIME. A VAT number is globally
	// unique, so the two drafts differ in exactly that field and in nothing else that
	// could seed an id.
	first := draftFor(t, StructureSingle, "Front door")
	second := draftFor(t, StructureSingle, "Front door")
	second.Business.Name = first.Business.Name
	second.Account.FullName = first.Account.FullName

	a, err := provisionDraft(t, p, first)
	if err != nil {
		t.Fatalf("Provision (first): %v", err)
	}
	b, err := provisionDraft(t, p, second)
	if err != nil {
		t.Fatalf("Provision (second): %v", err)
	}
	if a.TenantID == b.TenantID {
		t.Fatalf("two registrations produced the SAME tenant id (%s), so the id is derived "+
			"from the input rather than minted per call", a.TenantID)
	}
	if a.AdminUserID == b.AdminUserID {
		t.Fatalf("two registrations produced the same admin id (%s)", a.AdminUserID)
	}
}
