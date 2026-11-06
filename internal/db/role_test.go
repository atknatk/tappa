package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/jackc/pgx/v5"
)

// TestRoleRefusal is the table for the boot gate M8-04 added: the product used to
// serve traffic on a DSN for which PostgreSQL skips row level security entirely,
// and said nothing about it.
//
// Each row is a (role facts, environment) pair; the question is only ever "does
// New refuse to hand back a pool".
func TestRoleRefusal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		facts      RoleFacts
		isProd     bool
		wantRefuse bool
	}{
		{
			name:   "the application role in production — the shipped arrangement",
			facts:  RoleFacts{User: "tappa_app"},
			isProd: true,
		},
		{
			name:       "a SUPERUSER in production — RLS is skipped, refuse to serve",
			facts:      RoleFacts{User: "tappa_owner", Super: true},
			isProd:     true,
			wantRefuse: true,
		},
		{
			name:       "a BYPASSRLS role in production — same effect, different attribute",
			facts:      RoleFacts{User: "reporting", BypassRLS: true},
			isProd:     true,
			wantRefuse: true,
		},
		{
			// THE ROW THE FIRST ATTEMPT DID NOT HAVE. On managed Postgres nobody is
			// given SUPERUSER, so this is the SHAPE a migration role actually takes
			// there — and an owner may take FORCE ROW LEVEL SECURITY back off.
			name:       "an OWNER that is neither superuser nor BYPASSRLS — the managed-Postgres shape",
			facts:      RoleFacts{User: "tappa_owner", OwnsScopedTable: true},
			isProd:     true,
			wantRefuse: true,
		},
		{
			// GRANT tappa_owner TO tappa_app leaves both attributes false and is one
			// SET ROLE away from everything.
			name:       "a role that is a MEMBER of a privileged role",
			facts:      RoleFacts{User: "tappa_app", InheritsPrivilege: true},
			isProd:     true,
			wantRefuse: true,
		},
		{
			// M10 OP-7, 2nd round (F1): the owner's DSN with `role=tappa_app` -- every
			// attribute of current_user is clean, and the session is one SET ROLE NONE
			// from the superuser it signed in as.
			name:       "a session that signed in as another role, in production",
			facts:      RoleFacts{User: "tappa_app", Session: "tappa_owner"},
			isProd:     true,
			wantRefuse: true,
		},
		{
			name:   "a session that signed in as another role, outside production -- warn, do not refuse",
			facts:  RoleFacts{User: "tappa_app", Session: "tappa_owner"},
			isProd: false,
		},
		{
			name:   "the application role, signed in as itself, in production",
			facts:  RoleFacts{User: "tappa_app", Session: "tappa_app"},
			isProd: true,
		},
		{
			// T82: nothing here reaches past RLS, and nothing here can serve a request: 42501.
			name:       "the operator role in production -- unprivileged and still the wrong role",
			facts:      RoleFacts{User: "tappa_operator", Session: "tappa_operator"},
			isProd:     true,
			wantRefuse: true,
		},
		{
			name:       "an unprivileged role of any other name in production",
			facts:      RoleFacts{User: "reporting", Session: "reporting"},
			isProd:     true,
			wantRefuse: true,
		},
		{
			name:   "the operator role outside production -- development opens any role",
			facts:  RoleFacts{User: "tappa_operator", Session: "tappa_operator"},
			isProd: false,
		},
		{
			name:       "every attribute at once",
			facts:      RoleFacts{User: "tappa_owner", Super: true, BypassRLS: true, OwnsScopedTable: true, InheritsPrivilege: true},
			isProd:     true,
			wantRefuse: true,
		},
		{
			// A developer machine runs migrations, seeds and psql as the owner. The
			// warning is the channel there; refusing would push somebody to delete
			// the check rather than fix the DSN.
			name:   "a SUPERUSER outside production — warn, do not refuse",
			facts:  RoleFacts{User: "tappa_owner", Super: true},
			isProd: false,
		},
		{
			name:   "a BYPASSRLS role outside production",
			facts:  RoleFacts{User: "reporting", BypassRLS: true},
			isProd: false,
		},
		{
			name:   "an owner outside production",
			facts:  RoleFacts{User: "tappa_owner", OwnsScopedTable: true},
			isProd: false,
		},
		{
			name:   "the application role outside production",
			facts:  RoleFacts{User: "tappa_app"},
			isProd: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := roleRefusal(tc.facts, tc.isProd)
			if (err != nil) != tc.wantRefuse {
				t.Fatalf("roleRefusal(%+v, isProd=%v) = %v, want refusal = %v",
					tc.facts, tc.isProd, err, tc.wantRefuse)
			}
			if err == nil {
				return
			}
			// The message has to be actionable AND has to stay clear of §4.7: a
			// role name is configuration, a DSN carries a password.
			if !strings.Contains(err.Error(), tc.facts.User) {
				t.Errorf("the refusal does not name the role it refused: %v", err)
			}
			for _, forbidden := range []string{"postgres://", "password=", "DATABASE_URL="} {
				if strings.Contains(err.Error(), forbidden) {
					t.Errorf("the refusal leaks %q, which would put a DSN in the operator's log (CLAUDE.md §4.7): %v",
						forbidden, err)
				}
			}
		})
	}
}

// TestRoleFactsPrivilegedIsAnyReachPastRLS pins the predicate the gate and the
// warning both read. Its rows are the whole truth table: one clear case and one
// per attribute, because each attribute is a SEPARATE door and a predicate that
// dropped any one of them would still pass a test built only from the others.
func TestRoleFactsPrivilegedIsAnyReachPastRLS(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		facts RoleFacts
		want  bool
	}{
		{"nothing at all", RoleFacts{}, false},
		{"superuser", RoleFacts{Super: true}, true},
		{"bypassrls", RoleFacts{BypassRLS: true}, true},
		{"owns an RLS table", RoleFacts{OwnsScopedTable: true}, true},
		{"member of a privileged role", RoleFacts{InheritsPrivilege: true}, true},
		{"all four", RoleFacts{Super: true, BypassRLS: true, OwnsScopedTable: true, InheritsPrivilege: true}, true},
		{"signed in as another role (role= at start-up)", RoleFacts{User: "tappa_app", Session: "tappa_owner"}, true},
		{"signed in as itself", RoleFacts{User: "tappa_app", Session: "tappa_app"}, false},
	} {
		if got := tc.facts.Privileged(); got != tc.want {
			t.Errorf("%s: RoleFacts%+v.Privileged() = %v, want %v", tc.name, tc.facts, got, tc.want)
		}
	}
}

// TestNewRefusesAPrivilegedRoleInProduction is the BEHAVIOURAL half, and it exists
// because the table above passes perfectly on a tree where nothing calls the
// function.
//
// 🔴 IT REPLACES AN AST SCAN THAT PROVED NOTHING. The first attempt put the gate in
// cmd/tappa's run() and guarded it with a test that parsed main.go looking for the
// identifiers `Role` and `rlsRoleRefusal`. Measured: reducing the call to
// `_ = rlsRoleRefusal(role, cfg.IsHardened())` left the gate entirely dead and BOTH the
// table test and the wiring test green. An allowlist of identifiers cannot see
// whether a returned error reaches a return statement.
//
// This drives the real constructor against a real server instead, so the only way
// to make it green is for New to actually refuse.
//
// ⚠️ IT NEEDS BOTH DSNs, because the two halves are a refusal and a REGRESSION: an
// over-eager gate that refused the application role would break every deployment,
// and that is the failure this test would otherwise hide.
func TestNewRefusesAPrivilegedRoleInProduction(t *testing.T) {
	t.Parallel()

	appDSN := os.Getenv("DATABASE_URL")
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required; skipping the boot role gate (real Postgres required)")
	}

	for _, tc := range []struct {
		name       string
		dsn        string
		env        string
		wantRefuse bool
	}{
		{
			name:       "the privileged role in production — no pool is handed back",
			dsn:        ownerDSN,
			env:        config.EnvProd,
			wantRefuse: true,
		},
		{
			// THE REGRESSION. The shipped arrangement must still boot.
			name: "the application role in production — still opens",
			dsn:  appDSN,
			env:  config.EnvProd,
		},
		{
			// The developer loop: migrations, seeds and psql all run as the owner.
			name: "the privileged role outside production — warns elsewhere, opens here",
			dsn:  ownerDSN,
			env:  config.EnvDev,
		},
		{
			name: "the application role outside production",
			dsn:  appDSN,
			env:  config.EnvDev,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, err := New(context.Background(), &config.Config{DatabaseURL: config.NewDSN(tc.dsn), Env: tc.env})
			if d != nil {
				t.Cleanup(d.Close)
			}
			if tc.wantRefuse {
				if err == nil {
					t.Fatalf("New returned a pool for a role that can reach past row level security, "+
						"with TAPPA_ENV=%s. Every tenant-scoped policy is inert or removable on that "+
						"connection (CLAUDE.md §4.5)", tc.env)
				}
				if d != nil {
					t.Errorf("New refused AND returned a pool; a refused pool must be closed and dropped")
				}
				return
			}
			if err != nil {
				t.Fatalf("New refused a pool it must open (TAPPA_ENV=%s): %v", tc.env, err)
			}
			if d == nil {
				t.Fatal("New returned no error and no pool")
			}
		})
	}
}

// TestNewRefusesAnyRoleButTheApplicationRoleInProduction is T82 against a real server: a role that
// clears every privilege fact and cannot serve a single tenant query. tappa_operator is born NOLOGIN,
// so the connection signs in as the owner and SET SESSION AUTHORIZATION moves both identities, the
// way openOperatorDB's tests reach it; every other step is New's.
//
// CONTROLS: the same connection on TAPPA_ENV=dev opens and reports the role with no privilege fact,
// so the refusal is the new rule's and not the older one's; the application role opens on prod; the
// superuser keeps its own message in prod and still opens in dev.
//
// MUTANT (measured, 2026-11-06, reverted): the `f.User != applicationRole` branch removed -- red.
func TestNewRefusesAnyRoleButTheApplicationRoleInProduction(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres required)")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	asOperator := func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET SESSION AUTHORIZATION tappa_operator")
		return err
	}
	ownerCfg := func(env string) *config.Config {
		return &config.Config{DatabaseURL: config.NewDSN(ownerDSN), Env: env}
	}

	d, err := newDB(ctx, ownerCfg(config.EnvProd), asOperator)
	if err == nil {
		d.Close()
		t.Fatal("New opened a production pool as tappa_operator; its first tenant query would answer 42501")
	}
	for _, want := range []string{`role "tappa_operator"`, `session_user "tappa_operator"`, "must sign in as tappa_app"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	for _, leak := range []string{"postgres://", "password=", "DATABASE_URL=", ownerDSN} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the refusal carries %q; a DSN holds a password (CLAUDE.md §4.7)", "a DSN form")
		}
	}
	if strings.Contains(err.Error(), "rolsuper=") {
		t.Errorf("this is the privilege refusal, not the role refusal; the test no longer reaches the new rule: %v", err)
	}

	dev, err := newDB(ctx, ownerCfg(config.EnvDev), asOperator)
	if err != nil {
		t.Fatalf("development refused a pool on an unprivileged role: %v", err)
	}
	f := dev.RoleFacts()
	dev.Close()
	if f.User != "tappa_operator" || f.Session != "tappa_operator" || f.Privileged() {
		t.Fatalf("CONTROL: the connection is not the unprivileged operator role: %+v", f)
	}

	app, err := New(ctx, &config.Config{DatabaseURL: config.NewDSN(appDSN), Env: config.EnvProd})
	if err != nil {
		t.Fatalf("production refused the application role, which is every deployment: %v", err)
	}
	app.Close()

	if d, err := New(ctx, ownerCfg(config.EnvProd)); err == nil {
		d.Close()
		t.Error("production opened the superuser")
	} else if !strings.Contains(err.Error(), "rolsuper=true") {
		t.Errorf("the superuser's refusal changed: %v", err)
	}
	owner, err := New(ctx, ownerCfg(config.EnvDev))
	if err != nil {
		t.Fatalf("development refused the owner, which it opens for migrations and seeds: %v", err)
	}
	owner.Close()
}

// TestReadRoleSeesTheOwnerAndTheApplicationRoleDifferently is the positive control
// for the measurement itself: a probe that answered "not privileged" for everything
// would make the gate above green for the wrong reason.
//
// 🔴 IT IS THE ROW THAT USED TO READ FALSE. tappa_owner on the development database
// is a superuser, so Super alone would carry this assertion; OwnsScopedTable is
// asserted SEPARATELY because it is the attribute that a managed-Postgres migration
// role has WITHOUT superuser, and the field that the first attempt did not have.
func TestReadRoleSeesTheOwnerAndTheApplicationRoleDifferently(t *testing.T) {
	t.Parallel()

	appDSN := os.Getenv("DATABASE_URL")
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required; skipping the role measurement (real Postgres required)")
	}

	app, err := New(context.Background(), &config.Config{DatabaseURL: config.NewDSN(appDSN)})
	if err != nil {
		t.Fatalf("New(application role): %v", err)
	}
	t.Cleanup(app.Close)
	owner, err := New(context.Background(), &config.Config{DatabaseURL: config.NewDSN(ownerDSN)})
	if err != nil {
		t.Fatalf("New(owner role): %v", err)
	}
	t.Cleanup(owner.Close)

	af := app.RoleFacts()
	if af.User == "" {
		t.Error("RoleFacts carries no role name; New did not measure, or did not store what it measured")
	}
	if af.Privileged() {
		t.Errorf("the APPLICATION role reads as privileged (%+v); either the DSN is wrong or the probe "+
			"is over-broad, and an over-broad probe refuses production", af)
	}
	if af.OwnsScopedTable {
		t.Errorf("the application role owns (or can become the owner of) a table with row level "+
			"security: %+v — ADR 0002 madde 1 says it must not", af)
	}

	of := owner.RoleFacts()
	if !of.Privileged() {
		t.Errorf("the OWNER role reads as unprivileged (%+v); the probe cannot see what it is for", of)
	}
	if !of.OwnsScopedTable {
		t.Errorf("the owner role does not read as the owner of any table with row level security: %+v.\n"+
			"That is the attribute a managed-Postgres migration role carries WITHOUT superuser, and it "+
			"is the one the first version of this probe could not see", of)
	}
}

// TestReadRoleSeesAnOwnerThatIsNotASuperuser is the positive control for the ONE
// attribute the development database cannot otherwise exercise, and it is the exact
// shape the audit exploited.
//
// 🔴 WHY IT HAS TO BE SYNTHESISED. On this machine tappa_owner is a SUPERUSER, so
// every "privileged" assertion above is carried by rolsuper alone and would stay
// green with the owner predicate deleted. CLAUDE.md §1 deploys to MANAGED Postgres,
// where nobody is granted SUPERUSER and the migration role is typically owner but
// not superuser — a shape that exists in production and in no test until this one.
//
// It runs the SHIPPED statement (roleFactsQuery) against a role created inside a
// transaction that is ROLLED BACK, so the cluster is unchanged and no row count
// moves. It creates its OWN table rather than re-owning a real one: ALTER TABLE
// transactions OWNER TO would take an ACCESS EXCLUSIVE lock that every other test
// in the suite would queue behind.
func TestReadRoleSeesAnOwnerThatIsNotASuperuser(t *testing.T) {
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; skipping the synthetic-owner control (real Postgres required)")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Skipf("cannot reach Postgres as the migration role (%v); this control needs it", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	// A per-run suffix so a concurrent run of this test cannot collide on the name.
	var suffix string
	if err := conn.QueryRow(ctx, `SELECT replace(gen_random_uuid()::text, '-', '')`).Scan(&suffix); err != nil {
		t.Fatalf("generating a probe name: %v", err)
	}
	role, table := "probe_owner_"+suffix[:12], "probe_rls_"+suffix[:12]

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	// 🔴 ROLLBACK IS THE CONTRACT WITH THE SHARED DATABASE. Everything below is DDL
	// on objects this transaction created; nothing here touches a product table.
	defer func() { _ = tx.Rollback(context.Background()) }()

	for _, stmt := range []string{
		`CREATE ROLE ` + role + ` NOSUPERUSER NOBYPASSRLS NOLOGIN`,
		`CREATE TABLE ` + table + ` (tenant_id uuid NOT NULL)`,
		`ALTER TABLE ` + table + ` ENABLE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + table + ` FORCE ROW LEVEL SECURITY`,
		`ALTER TABLE ` + table + ` OWNER TO ` + role,
		`SET LOCAL ROLE ` + role,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			t.Skipf("this control needs to create a role and a table inside a transaction (%q): %v", stmt, err)
		}
	}

	var f RoleFacts
	if err := tx.QueryRow(ctx, roleFactsQuery).
		Scan(&f.User, &f.Super, &f.BypassRLS, &f.OwnsScopedTable, &f.InheritsPrivilege, &f.MemberOfARole); err != nil {
		t.Fatalf("running the shipped role probe as %s: %v", role, err)
	}

	// THE DISCRIMINATOR: the two attributes the first version of this probe read are
	// BOTH false here. If Privileged() still depended on them alone, a production
	// boot on this role would go ahead.
	if f.Super || f.BypassRLS {
		t.Fatalf("the synthetic role was not built as intended (%+v); this control proves nothing "+
			"unless both attributes are false", f)
	}
	if !f.OwnsScopedTable {
		t.Errorf("the probe does not see that %s OWNS a table with row level security: %+v.\n"+
			"Measured on this database, from exactly such a role: FORCE keeps SELECT at 0 rows, and then\n"+
			"  ALTER TABLE transactions NO FORCE ROW LEVEL SECURITY;   -> 392 197 rows / 78 406 tenants\n"+
			"  ALTER TABLE transactions DISABLE TRIGGER transactions_no_mutation; UPDATE … -> UPDATE 1\n"+
			"FORCE protects the DML; it does not take away the owner's right to remove FORCE (§4.5, §4.3)", role, f)
	}
	if !f.Privileged() {
		t.Errorf("Privileged() is false for an owner of a row-level-security table (%+v); a production "+
			"boot on this role would be allowed", f)
	}
}
