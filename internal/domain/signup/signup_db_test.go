package signup

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
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

func newProvisionerFixture(t *testing.T) (*Provisioner, *db.DB) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping signup provisioning tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	p, err := NewProvisioner(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	return p, data
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
func ownerData(t *testing.T) *db.DB {
	t.Helper()
	raw := os.Getenv("DATABASE_MIGRATE_URL")
	if raw == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; skipping the fixtures that must plant backdated rows")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: raw})
	if err != nil {
		t.Fatalf("db.New(owner): %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// assertAppCannotBackdate is the tripwire that rides with ownerData: if the
// application role regains the ability to plant a backdated identity, 00017's ordering
// guarantee is gone and this fixture is the first thing that should notice.
//
// 🔴 IT CREATES A REAL TENANT FIRST, AND THE FIRST VERSION DID NOT — WHICH MADE IT
// VACUOUS. It scoped to a fresh random uuid, so the INSERT was refused by the FOREIGN
// KEY to `tenants` rather than by the privilege, and the mutation that restores
// table-wide INSERT came back GREEN. A refusal proves nothing unless you know WHAT
// refused. The tenant is written by the OWNER so the probe's own setup cannot be the
// thing being tested.
//
// AND IT DISTINGUISHES THE TWO OUTCOMES BY SQLSTATE: 42501 is insufficient_privilege,
// which is the one this asserts. Anything else means the probe stopped measuring what
// it claims to.
func assertAppCannotBackdate(t *testing.T, data *db.DB, owner *db.DB, email string) {
	t.Helper()
	ctx := context.Background()
	tenantID := uuid.New()
	if err := owner.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id,name,vat_number,business_type,structure)
			 VALUES ($1::uuid,'Tripwire','MT'||substr(replace($2,'-',''),1,8),'other','single')`,
			tenantID, tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("the tripwire could not create its own tenant: %v", err)
	}

	err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id,tenant_id,full_name,email,password_hash,role,created_at)
			 VALUES ($1,$2,'Probe',$3,$4,'owner', now() - interval '400 days')`,
			uuid.New(), tenantID, email, "$2a$04$"+strings.Repeat("c", 53))
		return e
	})
	if err == nil {
		t.Fatal("tappa_app planted a BACKDATED admin_users row into a tenant that EXISTS. " +
			"Migration 00017 revoked created_at from its INSERT grant so that nothing " +
			"running as the application can put a row in front of an existing customer in " +
			"the login resolver's order — the account lockout that migration exists to " +
			"close. The grant has been widened.")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("the backdated INSERT was refused by %v, not by insufficient_privilege "+
			"(42501). A refusal for another reason means this tripwire is not measuring the "+
			"grant.", err)
	}
}

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
	res, err := p.Provision(ctx, d)
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

	// 🔴 THE DIGEST'S SHAPE — internal/adminauth's fourth timing arm, closed at the
	// write path because the schema cannot enforce it today (migration 00017 measures
	// why). A row with an empty or malformed password_hash answers about a million
	// times faster than an unregistered address.
	if !strings.HasPrefix(digest, "$2a$12$") {
		t.Errorf("password_hash does not begin $2a$12$ (it begins %q); the wizard must only "+
			"ever write adminauth.Hash output", safePrefix(digest))
	}
	if digest == d.Account.Password {
		t.Fatal("the password was stored in the clear")
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

	newBusiness, err := p.Provision(ctx, draftFor(t, StructureSingle, "Front door"))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	// A SECOND, UNRELATED business, created the same way. It is the attacker's seat:
	// a real tenant with a real context, not a made-up uuid.
	other, err := p.Provision(ctx, draftFor(t, StructureSingle, "Their door"))
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

// TestSignupProvision_ADuplicateVATLeavesNOTHINGBEHIND.
//
// 🔴 THE HALF-TENANT IS THE WORST OUTCOME AVAILABLE HERE and it is what M7-03's card
// asks about ("Başarısızlıkta yarım tenant kalmıyor (rollback)"). A `tenants` row
// with no operator cannot be signed into, cannot be deleted, and holds the VAT number
// against the customer's own second attempt.
//
// The duplicate VAT number is used as the failure because it is the one failure a
// real customer will actually hit, and because it fails on the FIRST statement — so
// this also proves the venues and the admin never got written.
func TestSignupProvision_ADuplicateVATLeavesNothingBehind(t *testing.T) {
	p, data := newProvisionerFixture(t)
	ctx := context.Background()

	first := draftFor(t, StructureSingle, "Front door")
	firstRes, err := p.Provision(ctx, first)
	if err != nil {
		t.Fatalf("the first registration: %v", err)
	}

	second := draftFor(t, StructureSingle, "Another door")
	second.Business.VATNumber = first.Business.VATNumber
	if _, err := p.Provision(ctx, second); !errors.Is(err, ErrVATTaken) {
		t.Fatalf("the second registration returned %v, want ErrVATTaken", err)
	}

	// NOTHING FROM THE SECOND ATTEMPT EXISTS. The probe runs inside the FIRST
	// tenant's context — the only context in which a row carrying that VAT number
	// could be visible at all — and looks for the values that were unique to the
	// refused attempt.
	var locations, admins int
	if err := data.WithTenant(ctx, firstRes.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM locations WHERE name = 'Another door'`).
			Scan(&locations); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&admins)
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if locations != 0 {
		t.Errorf("the failed registration left %d location(s) behind", locations)
	}
	if admins != 1 {
		t.Errorf("the first tenant holds %d operator(s), want exactly its own 1; the failed "+
			"registration must have written none", admins)
	}
}

// TestSignupProvision_RefusesADraftThatDidNotValidateAndWritesNothing.
//
// Provision is the function that WRITES, so it re-checks rather than trusting
// whichever caller assembled the draft — a rule enforced only by the wizard is a rule
// the next caller (an API, M7-03) does not inherit.
//
// 🔴 "IT WROTE NOTHING" IS PROVED WITHOUT A GLOBAL COUNT, because there is no honest
// way to take one: tappa_app is RLS-bound and sees one tenant at a time, so a
// `SELECT count(*) FROM tenants` from the application role returns zero whatever
// happened. The proof used instead is the VAT number itself — it is globally UNIQUE,
// so if a refused draft had created a tenant, registering the SAME number afterwards
// would fail. It succeeds, which is a stronger statement than a count: the row is not
// merely invisible, it does not exist.
func TestSignupProvision_RefusesADraftThatDidNotValidateAndWritesNothing(t *testing.T) {
	p, _ := newProvisionerFixture(t)
	ctx := context.Background()

	bad := draftFor(t, StructureSingle, "Front door")
	bad.Business.VATNumber = "not-a-vat-number"
	if _, err := p.Provision(ctx, bad); err == nil {
		t.Fatal("Provision accepted a draft with an invalid VAT number")
	}

	// A draft whose password reaches adminauth.Hash and is refused there. A
	// registration that got past it would store a digest of the empty string, i.e. an
	// account whose password is "press enter".
	noPassword := draftFor(t, StructureSingle, "Front door")
	if _, err := p.Provision(ctx, noPassword.withPassword("")); err == nil {
		t.Fatal("Provision accepted a draft with no password")
	}
	// A draft whose venue list contradicts its structure. It validates in the DOMAIN,
	// not in the database, so nothing but this check refuses it.
	twoDoors := draftFor(t, StructureSingle, "Front door", "Back door")
	if _, err := p.Provision(ctx, twoDoors); err == nil {
		t.Fatal("Provision accepted two venues for a single-place business")
	}

	// THE PROOF: the VAT numbers those three attempts carried are still free.
	for i, d := range []Draft{noPassword, twoDoors} {
		good := d
		good.Account.Password = "a passphrase nobody guesses"
		good.Venues = good.Venues[:1]
		good.Business.Structure = StructureSingle
		if _, err := p.Provision(ctx, good); err != nil {
			t.Fatalf("attempt %d: the VAT number of a REFUSED registration is taken, so that "+
				"registration wrote a tenant row after all: %v", i, err)
		}
	}
}

// withPassword returns the draft with a different password, for the table above.
func (d Draft) withPassword(p string) Draft {
	d.Account.Password = p
	return d
}

// failingTrail is a Trail whose RecordTx fails. It is the ONLY way to make a
// registration fail in the MIDDLE of its transaction: the audit row is the last
// statement, so by the time it runs the tenant, its venues and its operator have all
// been written.
type failingTrail struct{ err error }

func (f failingTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, f.err
}

// Record is the OTHER half of Trail (the sign-in reachability observation, which is
// taken after the transaction commits and so cannot share it). It fails too: this
// double is for the rollback test, and a failure there must not be rescued by a write
// that happens to succeed.
func (f failingTrail) Record(context.Context, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, f.err
}

// TestSignupProvision_AFailureInTheMiddleLeavesNothingBehind.
//
// 🔴 THE GAP THIS CLOSES WAS NAMED BY AN AUDIT AND IT WAS REAL. The other rollback
// test uses a duplicate VAT number, which violates the UNIQUE constraint on the
// FIRST statement — so nothing had been written yet and "no half tenant survives"
// was proved for the one case where there was nothing to roll back. That is not the
// case M7-03's criterion is about.
//
// HERE THE TRANSACTION GETS ALL THE WAY TO ITS LAST STATEMENT. The tenant row, its
// venues and its first operator are written and then the trail write fails, so the
// rollback has to take four kinds of row back at once — including one in `locations`
// and one in `admin_users`, neither of which the application can DELETE (the grants
// and §4.6 see to that). If ROLLBACK did not do it, nothing could.
//
// THE PROOF IS THE VAT NUMBER, not a count: it is globally UNIQUE, so if the failed
// registration had left a `tenants` row behind, registering the same number again
// would be refused. It is not.
func TestSignupProvision_AFailureInTheMiddleLeavesNothingBehind(t *testing.T) {
	_, data := newProvisionerFixture(t)
	ctx := context.Background()

	boom := errors.New("the trail could not be written")
	broken, err := NewProvisioner(data, failingTrail{err: boom}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}

	d := draftFor(t, StructureMulti, "Doomed door A", "Doomed door B")
	if _, err := broken.Provision(ctx, d); err == nil {
		t.Fatal("Provision succeeded although the audit write failed; the trail and the " +
			"business must share one fate")
	} else if !strings.Contains(err.Error(), boom.Error()) {
		t.Fatalf("Provision failed with %v, which does not carry the underlying cause", err)
	}

	// 1. THE VAT NUMBER IS STILL FREE, so no tenant row survived.
	good, err := NewProvisioner(data, mustTrail(t, data), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	res, err := good.Provision(ctx, d)
	if err != nil {
		t.Fatalf("the VAT number of a rolled-back registration is taken, so a `tenants` row "+
			"survived the failure: %v", err)
	}

	// 2. THE SURVIVING TENANT HOLDS EXACTLY ITS OWN ROWS.
	var locations, admins, trail int
	if err := data.WithTenant(ctx, res.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM locations`).Scan(&locations); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&admins); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&trail)
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if locations != 2 || admins != 1 || trail != 1 {
		t.Errorf("the surviving tenant holds locations=%d admins=%d audit=%d, want 2/1/1",
			locations, admins, trail)
	}

	// 3. 🔴 AND NO ORPHAN SURVIVED **ANYWHERE**, WHICH IS WHAT (2) DOES NOT PROVE. An
	// audit named the gap: a row written by the FAILED attempt would carry the FAILED
	// attempt's tenant id, so it is invisible from inside the surviving tenant's
	// context whether it exists or not — the check above would pass over it. The only
	// way to see it is to look WITHOUT a tenant context at all.
	//
	// THE RESOLVER IS WHAT MAKES THAT POSSIBLE WITHOUT A SECOND ROLE. The email is
	// unique to this test, and resolve_admin_by_email is SECURITY DEFINER over
	// admin_users with no tenant scope (migration 00011), so it sees every row that
	// exists anywhere. Exactly ONE identity may carry this address: the one the
	// SUCCESSFUL registration wrote.
	rows, err := data.GetAdminByEmail(ctx, d.Account.Email)
	if err != nil {
		t.Fatalf("GetAdminByEmail: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%d admin identit(ies) carry this address across the WHOLE database, want 1. "+
			"The rolled-back attempt wrote one that survived, and no tenant-scoped read "+
			"could have seen it.", len(rows))
	}
	if rows[0].TenantID != res.TenantID {
		t.Errorf("the surviving identity belongs to tenant %s, not to the registration that "+
			"succeeded (%s)", rows[0].TenantID, res.TenantID)
	}
	if rows[0].ID != res.AdminUserID {
		t.Errorf("the surviving identity is %s, not the one the successful registration "+
			"returned (%s)", rows[0].ID, res.AdminUserID)
	}
}

// mustTrail builds a real recorder for the second half of the test above.
func mustTrail(t *testing.T, data *db.DB) Trail {
	t.Helper()
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	return trail
}

// TestSignupProvision_TellsACustomerWhoCannotSignIn.
//
// 🔴 THE OTHER HALF OF MIGRATION 00017's TRADE, WHICH WAS ARGUED AND NOT BUILT. The
// incumbent-first ordering protects an EXISTING customer and, deterministically, puts
// a NEW registration last — so an address already carrying adminauth.MaxCandidates
// identities produces an account nobody can sign into. 00017 and ADR 0013 defended
// that as acceptable because the customer "finds out immediately, at a moment when
// they are on our page and can be told something"; an audit measured that the product
// said nothing at all: 303, APPROVED, "Sign in", then 401 on the one screen that must
// not explain itself.
//
// THE POSITIVE CONTROL IS THE FIRST HALF OF THE TABLE: an ordinary registration, and
// one against an address that already has SOME accounts but not enough to fill the
// window, must both come back false — or the warning would fire for people who are
// perfectly able to sign in.
//
// 🔴 AND THE TABLE IS ALSO THE MEASUREMENT OF THE CHANNEL THIS CHECK OPENS, which five
// places in this repository used to deny. Read the last two rows as an ATTACKER: plant
// MaxCandidates-1 rows, then complete one more registration. If the address was
// unknown the total is MaxCandidates and the flag is false; if ONE account was already
// there the total is MaxCandidates+1 and the flag is true. That is the "7 -> false,
// 8 -> true" pair below, and the 8 is the attacker's seven plus the victim's one.
// signInBlocked carries the price and the trail; nothing here closes it.
func TestSignupProvision_TellsACustomerWhoCannotSignIn(t *testing.T) {
	p, data := newProvisionerFixture(t)
	ctx := context.Background()

	// plant writes an admin row for `email` in a tenant of its own, older than
	// anything this test creates afterwards.
	owner := ownerData(t)
	plant := func(email string) {
		t.Helper()
		tenantID := uuid.New()
		if err := owner.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx,
				`INSERT INTO tenants (id,name,vat_number,business_type,structure)
				 VALUES ($1::uuid,'Crowd',  'MT'||substr(replace($2,'-',''),1,8),'other','single')`,
				tenantID, tenantID.String()); e != nil {
				return e
			}
			_, e := tx.Exec(ctx,
				`INSERT INTO admin_users (id,tenant_id,full_name,email,password_hash,role,created_at)
				 VALUES ($1,$2,'Crowd',$3,$4,'owner', now() - interval '400 days')`,
				uuid.New(), tenantID, email, "$2a$04$"+strings.Repeat("c", 53))
			return e
		}); err != nil {
			t.Fatalf("planting: %v", err)
		}
	}

	// The tripwire that rides with ownerData: if tappa_app regains the ability to plant
	// a backdated identity, 00017's ordering guarantee is gone.
	assertAppCannotBackdate(t, data, ownerData(t), "tripwire-"+uuid.NewString()+"@probe.example")

	for _, tc := range []struct {
		name    string
		planted int
		want    bool
	}{
		{"an ordinary registration", 0, false},
		{"an address with a few existing accounts", 3, false},
		// THE ORACLE, read as the attacker reads it: these two rows differ by ONE
		// pre-existing identity and the flag differs with it.
		{"an address one short of the window", adminauth.MaxCandidates - 1, false},
		{"an address that already fills the window", adminauth.MaxCandidates, true},
		{"an address stuffed well past the window", adminauth.MaxCandidates + 5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := draftFor(t, StructureSingle, "Front door")
			for i := 0; i < tc.planted; i++ {
				plant(d.Account.Email)
			}
			res, err := p.Provision(ctx, d)
			if err != nil {
				t.Fatalf("Provision: %v", err)
			}
			if res.SignInBlocked != tc.want {
				t.Fatalf("SignInBlocked = %v, want %v (%d row(s) planted before the "+
					"registration, window is %d)",
					res.SignInBlocked, tc.want, tc.planted, adminauth.MaxCandidates)
			}
			// AND THE MEASUREMENT IS REAL, not a count: when it says blocked, the login
			// resolver genuinely does not carry this identity in its compared window.
			rows, err := data.GetAdminByEmail(ctx, d.Account.Email)
			if err != nil {
				t.Fatalf("GetAdminByEmail: %v", err)
			}
			inWindow := false
			for i, r := range rows {
				if i >= adminauth.MaxCandidates {
					break
				}
				if r.ID == res.AdminUserID {
					inWindow = true
				}
			}
			if inWindow == res.SignInBlocked {
				t.Errorf("the flag says blocked=%v while the resolver puts the new row "+
					"in-window=%v across %d resolved row(s)",
					res.SignInBlocked, inWindow, len(rows))
			}
		})
	}
}

// TestSignupProvision_AnUnreachableAccountIsObservable — E1's "counted" half.
//
// 🔴 THE RULE THIS ENFORCES: a channel that is not closed must at least be VISIBLE.
// signInBlocked reports the edge of the comparison window to the caller, which an
// audit showed is an oracle for "does this address already have an account" (and, by
// varying the plant count, for how many). It is kept because removing it restores a
// certain loss to a real customer — but until this test it left no trail at all, which
// is the one thing internal/handler.AdminAuth.recordCandidateProbe exists to avoid on
// the login side.
func TestSignupProvision_AnUnreachableAccountIsObservable(t *testing.T) {
	p, data := newProvisionerFixture(t)
	ctx := context.Background()

	d := draftFor(t, StructureSingle, "Front door")
	// The planted rows must be BACKDATED (the resolver orders by created_at), which
	// 00017 removed from tappa_app's grant — see ownerData.
	owner := ownerData(t)
	assertAppCannotBackdate(t, data, owner, d.Account.Email)
	// Fill the window before this registration arrives.
	for i := 0; i < adminauth.MaxCandidates; i++ {
		tenantID := uuid.New()
		if err := owner.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx,
				`INSERT INTO tenants (id,name,vat_number,business_type,structure)
				 VALUES ($1::uuid,'Crowd','MT'||substr(replace($2,'-',''),1,8),'other','single')`,
				tenantID, tenantID.String()); e != nil {
				return e
			}
			_, e := tx.Exec(ctx,
				`INSERT INTO admin_users (id,tenant_id,full_name,email,password_hash,role,created_at)
				 VALUES ($1,$2,'Crowd',$3,$4,'owner', now() - interval '400 days')`,
				uuid.New(), tenantID, d.Account.Email, "$2a$04$"+strings.Repeat("c", 53))
			return e
		}); err != nil {
			t.Fatalf("planting: %v", err)
		}
	}

	res, err := p.Provision(ctx, d)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !res.SignInBlocked {
		t.Fatal("the fixture did not reproduce the unreachable case")
	}

	var rows int
	var detail string
	if err := data.WithTenant(ctx, res.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*), coalesce(string_agg(detail::text, ' '), '') FROM audit_log
			 WHERE tenant_id = $1 AND action = $2`,
			res.TenantID, ActionSignupUnreachable).Scan(&rows, &detail)
	}); err != nil {
		t.Fatalf("reading the trail: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d %s row(s), want exactly 1 — an unclosed channel that leaves no trace "+
			"is one nobody can tell was used", rows, ActionSignupUnreachable)
	}
	// §4.7: counts, never the address.
	if strings.Contains(detail, d.Account.Email) {
		t.Error("the trail row carries the email address; audit_log cannot be deleted from")
	}
	for _, want := range []string{`"resolved"`, `"compared"`} {
		if !strings.Contains(detail, want) {
			t.Errorf("the trail row carries no %s, so an investigator cannot see how far past "+
				"the window the address was stuffed", want)
		}
	}

	// THE NEGATIVE CONTROL: an ordinary registration writes no such row, or the signal
	// would be drowned by every customer who ever signed up.
	other, err := p.Provision(ctx, draftFor(t, StructureSingle, "Front door"))
	if err != nil {
		t.Fatalf("Provision (ordinary): %v", err)
	}
	var none int
	if err := data.WithTenant(ctx, other.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1`,
			ActionSignupUnreachable).Scan(&none)
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if none != 0 {
		t.Errorf("an ordinary registration wrote %d %s row(s)", none, ActionSignupUnreachable)
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
	ctx := context.Background()

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

	a, err := p.Provision(ctx, first)
	if err != nil {
		t.Fatalf("Provision (first): %v", err)
	}
	b, err := p.Provision(ctx, second)
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
