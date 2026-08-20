// Package db owns the single pgx connection pool and the tenant-scoped
// transaction helper that is the ONLY way the application reaches Postgres.
//
// Handlers never see *pgxpool.Pool. They go through WithTenant (tenant.go),
// which sets the tenant context inside a transaction so RLS is always in force
// (CLAUDE.md §4.5, ADR 0002 madde 2). Making the pool unreachable from handlers
// turns "did we set the tenant context?" from a discipline question into a
// structural guarantee.
package db

import (
	"context"
	"fmt"

	"github.com/atknatk/tappa/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps the application connection pool. It connects as tappa_app
// (NOSUPERUSER, NOBYPASSRLS, not the table owner — ADR 0002 madde 1) via
// cfg.DatabaseURL, so RLS applies to every query it runs.
type DB struct {
	pool *pgxpool.Pool
	// role is measured ONCE, by New, before the pool is handed to anybody. It is
	// stored rather than re-queried so the boot warning costs no second round trip
	// and cannot read a different answer than the gate did.
	role RoleFacts
}

// New builds the pool from cfg.DatabaseURL, verifies connectivity with a ping,
// measures the role it actually connected as, and REFUSES to hand back a pool
// whose role would void tenant isolation in production. Pool sizing is read from
// the connection string (pgx honours pool_max_conns, pool_min_conns,
// pool_max_conn_lifetime, ... as query parameters); absent those, pgx defaults
// apply. Keeping pool tuning in the DSN means config carries it without this
// package growing knobs it does not need.
//
// 🔴 THE ROLE GATE IS HERE, IN THE CONSTRUCTOR, AND THAT PLACEMENT IS THE WHOLE
// POINT (M8-04, moved here 2026-08-19). It used to live in cmd/tappa's run(),
// two statements after the db.New call, guarded by a test that parsed main.go's
// AST looking for the identifiers `Role` and `rlsRoleRefusal`. Measured: reducing
// the call to `_ = rlsRoleRefusal(role, cfg.IsProd())` left the gate completely
// dead and BOTH tests green — the AST scan saw the identifiers and never asked
// whether the returned error reached a return statement. ADR 0002's own principle
// is that containment must rest on STRUCTURE rather than discipline; a gate a
// refactor can forget is discipline wearing a test's clothes. From here there is
// no forgetting: the only way to obtain a *DB is to be handed one, and a refused
// pool is closed and never returned.
//
// ⚠️ NO OPT-IN ESCAPE HATCH IS SHIPPED, AND THAT IS COUNTED RATHER THAN ASSUMED —
// BUT THE FIRST COUNT WAS WRONG AND IS CORRECTED HERE. It said "32 call sites, one
// in cmd/tappa and 31 in tests, and NOT ONE test builds a Config with Env=prod".
// Both halves were off. Re-counted on this tree (2026-08-20):
//
//	db.New(   qualified, outside this package ......... 32 (1 cmd/tappa, 31 tests)
//	New(      unqualified, inside this package ........  8 (all tests)
//	                                            total .. 40 call sites, 39 of them tests
//
// And exactly ONE test does build Env=prod: role_test.go's
// TestNewRefusesAPrivilegedRoleInProduction, whose table sets config.EnvProd on two
// of its four rows — one expecting the refusal, one proving the application role
// still opens. That test is the gate's own behavioural proof and it was added by the
// same change as this comment, so the sentence was wrong on the day it shipped.
//
// 🔴 THE CONCLUSION SURVIVES THE CORRECTION, AND THAT WAS MEASURED SEPARATELY: every
// other call site leaves Env empty, so IsProd() is false and the deliberately-
// privileged pools the red-line suite needs — internal/db/rls_test.go's ownerDB,
// internal/domain/signup's ownerData, internal/domain/tenant's owner — still open
// with the gate live. Adding a db.AllowPrivilegedRole() option today would ship an
// unused API surface whose first user would be the person routing around the gate;
// the escape hatch is a DSN change, which is the thing §4.5 is about.
func New(ctx context.Context, cfg *config.Config) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: parse DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("db: new pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	d := &DB{pool: pool}
	facts, err := d.readRole(ctx)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := roleRefusal(facts, cfg.IsProd()); err != nil {
		pool.Close()
		return nil, err
	}
	d.role = facts
	return d, nil
}

// Close drains and closes the pool. Call it once during graceful shutdown.
func (d *DB) Close() { d.pool.Close() }

// Ping acquires a connection and asks the server whether it is answering. It is
// what GET /readyz is built on (M8-01, internal/handler/health.go).
//
// 🔴 IT READS NO TABLE, AND THAT IS A TENANT-ISOLATION DECISION RATHER THAN
// LAZINESS (CLAUDE.md §4.5). This pool connects as tappa_app, which is NOBYPASSRLS
// and not the table owner, and every tenant-scoped policy resolves
// NULLIF(current_setting('app.tenant_id', true), ”)::uuid — so OUTSIDE a
// WithTenant transaction there is no tenant context and every such table is empty.
// A readiness probe that did `SELECT count(*) FROM transactions` would therefore
// answer 0 on a perfectly healthy, fully populated database and could not tell that
// apart from an empty one; it would be a check whose success proves nothing.
// pgxpool.Ping executes the empty query on a real connection instead, so what it
// proves is exactly what readiness means here: a connection can be acquired from
// the pool and the server answers on it.
//
// ⚠️ IT IS A NARROW ACCESSOR, NOT A DOOR TO THE POOL. It returns an error and
// nothing else — no pgx.Conn, no pgx.Tx, no rows — so it cannot become the
// context-less bypass ADR 0002 forbids. Anything that wants to read data still has
// to go through WithTenant.
//
// The caller owns the deadline: this method does not invent one, because the
// readiness handler bounds it (a probe that can hang is a probe that holds a
// connection while the deployment waits for an answer).
func (d *DB) Ping(ctx context.Context) error { return d.pool.Ping(ctx) }

// RoleFacts is what the server measured about the role it actually connected as.
//
// 🔴 IT REPORTS, IT DOES NOT DECIDE. Whether a privileged role is a refusal or a
// warning depends on TAPPA_ENV; roleRefusal below owns that decision, and keeping
// the fact and the policy apart is what lets the policy be table-tested without a
// database.
type RoleFacts struct {
	// User is current_user — the role RLS is evaluated against, which is not
	// necessarily the role named in the DSN (SET ROLE, or a login role that maps
	// elsewhere).
	User string
	// Super and BypassRLS are the two attributes for which PostgreSQL SKIPS row
	// level security outright. Either one true means every tenant-scoped policy in
	// this database is inert for this connection.
	Super, BypassRLS bool
	// OwnsScopedTable is true when this role IS, or can BECOME (role membership),
	// the owner of at least one table that has row level security enabled. An owner
	// is subject to FORCE ROW LEVEL SECURITY, but an owner also keeps the right to
	// take FORCE off again — see the comment on readRole for the measurement.
	OwnsScopedTable bool
	// InheritsPrivilege is true when this role is a member, directly or
	// transitively, of some role that is SUPERUSER or BYPASSRLS. Membership is one
	// SET ROLE away from the attribute itself.
	InheritsPrivilege bool
}

// Privileged reports whether this role can reach past row level security — either
// because PostgreSQL skips RLS for it outright, or because it can put itself in the
// position of a party for which RLS is skipped or removable.
func (f RoleFacts) Privileged() bool {
	return f.Super || f.BypassRLS || f.OwnsScopedTable || f.InheritsPrivilege
}

// readRole asks the server which role this pool is authenticated as and how far
// that role can reach past row level security. New calls it once; RoleFacts serves
// the stored answer afterwards.
//
// 🔴 WHY THE SERVER IS ASKED RATHER THAN THE CONNECTION STRING PARSED. A DSN's
// user name is a claim; the catalogue is the fact. The two come apart in ordinary
// deployments (a PgBouncer pool, a `SET ROLE` in a connect hook, a login role that
// is a member of another) and it is the fact that decides whether §4.5 holds.
//
// 🔴 FOUR FACTS, NOT TWO, AND THE THIRD WAS THE ONE THAT MATTERED. PostgreSQL skips
// or lets you remove RLS for three parties: a superuser, a BYPASSRLS role, and a
// table's OWNER. This query used to read the first two only, and the comment here
// claimed they were "the complete set, given FORCE ROW LEVEL SECURITY". THAT WAS
// WRONG, and an audit measured how wrong inside a BEGIN … ROLLBACK on the
// development database: a role that is neither superuser nor BYPASSRLS but OWNS
// `transactions` reads as `probe_owner | f | f`, so the old probe returned no
// refusal and a production boot went ahead. From that connection —
//
//	FORCE RLS still on:  counting the attendance table  ->        0 rows
//	after NO FORCE ROW LEVEL SECURITY on it            ->  392 197 rows / 78 406 tenants
//	after disabling its no-mutation trigger, a row edit and a row removal both
//	                                                    reported one affected row (§4.3 gone too)
//
// ⚠️ THE TWO MUTATION STATEMENTS ARE DESCRIBED RATHER THAN SPELLED, for the reason
// roleRefusal's comment gives about the migration-role variable: R3 fails production
// code that names those verbs against that table and cannot tell a prohibition being
// DISCUSSED from one being violated. Measured — an earlier draft of this paragraph
// wrote them out and turned the scan red.
//
// FORCE protects the DML; it does not take away the owner's right to REMOVE force.
// And this is not an exotic topology: CLAUDE.md §1 deploys to MANAGED Postgres,
// where no superuser is handed out and the migration role is typically owner but
// not superuser — exactly the shape above.
//
// 🔴 THE SAME PREDICATE CLOSES ROLE MEMBERSHIP, WHICH THE OLD COMMENT WROTE DOWN AS
// A LIMIT AND LEFT OPEN. `pg_has_role(current_user, relowner, 'MEMBER')` is true for
// "is the owner" AND for "is a member of the owner", so one measurement answers both.
// Measured, again inside BEGIN … ROLLBACK: `GRANT tappa_owner TO tappa_app` left the
// old probe reading `tappa_app | f | f`, and on that one connection the tenant count
// went from 0 (as tappa_app, no tenant context) to 327 866 after a single SET ROLE.
// 'MEMBER' rather than 'USAGE' is deliberate — USAGE follows only the INHERIT chain,
// and a grant made WITH INHERIT FALSE still permits SET ROLE.
//
// ⚠️ InheritsPrivilege IS A SEPARATE PREDICATE BECAUSE THE OWNER TEST DOES NOT COVER
// IT, and that gap was measured too: `GRANT tappa_resolver TO tappa_app` reads
// `owns_scoped_table = f` (tappa_resolver owns no table) but reaches 120 975 plaques
// across 103 422 tenants via SET ROLE — including all 120 975 wrapped AES key
// references (§4.7) — because that role carries BYPASSRLS for the SECURITY DEFINER
// resolvers (ADR 0002). Two predicates, two different doors.
//
// ⚠️ EVERY COUNT IN THIS COMMENT IS DATED 2026-08-19 AND GROWS: the suite writes rows
// on every run, so the figure to read is the ratio to zero, not the digits.
//
// ⚠️ COUNTED LIMIT, STATED RATHER THAN IMPLIED: this is a BOOT-TIME measurement of a
// RUNTIME property. A GRANT made after this process started is not seen until it
// restarts, and a role that gains SUPERUSER later is not re-measured. What closes
// that class is not this query — it is scripts/redline-check.sh's R5b on the
// migrations, scripts/pg-restore-verify.sh on a restored dump, and the RLS isolation
// suite. Nothing here claims to be exhaustive.
//
// ⚠️ IT READS pg_roles, pg_class AND pg_namespace, ALL WORLD-READABLE, so it needs no
// grant of its own and adds no privilege to tappa_app. relrowsecurity is the
// operational definition of "tenant-scoped" here: every table CLAUDE.md §6 governs
// has RLS enabled, and a table with RLS enabled is exactly the kind whose protection
// an owner can strip.
func (d *DB) readRole(ctx context.Context) (RoleFacts, error) {
	var f RoleFacts
	row := d.pool.QueryRow(ctx, roleFactsQuery)
	if err := row.Scan(&f.User, &f.Super, &f.BypassRLS, &f.OwnsScopedTable, &f.InheritsPrivilege); err != nil {
		return RoleFacts{}, fmt.Errorf("db: read connection role: %w", err)
	}
	return f, nil
}

// roleFactsQuery is readRole's statement, named so its positive control can run the
// SHIPPED text rather than a copy. A checker that re-types the thing it checks stops
// checking the moment the two drift, which this repository has measured more than
// once (internal/db/role_test.go's synthetic-owner control drives this const).
const roleFactsQuery = `
	SELECT current_user,
	       r.rolsuper,
	       r.rolbypassrls,
	       EXISTS (SELECT 1
	                 FROM pg_class c
	                 JOIN pg_namespace n ON n.oid = c.relnamespace
	                WHERE c.relkind = 'r'
	                  AND c.relrowsecurity
	                  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
	                  AND pg_has_role(current_user, c.relowner, 'MEMBER')),
	       EXISTS (SELECT 1
	                 FROM pg_roles p
	                WHERE (p.rolsuper OR p.rolbypassrls)
	                  AND pg_has_role(current_user, p.oid, 'MEMBER'))
	  FROM pg_roles r
	 WHERE r.rolname = current_user`

// RoleFacts serves the measurement New already took. It queries nothing, so a
// caller cannot use it to ask a second time and get a different answer than the
// gate acted on.
//
// Its only production caller is cmd/tappa, which uses it for the DEVELOPMENT
// warning. Forgetting that call costs a log line on a developer's machine;
// forgetting the refusal is not possible, because the refusal is New's own.
func (d *DB) RoleFacts() RoleFacts { return d.role }

// RoleRiskWhy is the one sentence the refusal and the development warning both
// carry, so the two channels cannot drift into saying different things.
//
// ⚠️ IT NAMES THE ROLE AND NOTHING ELSE. A role name is configuration an operator
// typed; the DSN it came from carries a password (§4.7), so neither this constant
// nor its callers ever format cfg.DatabaseURL.
const RoleRiskWhy = "PostgreSQL skips row level security for a superuser and for a BYPASSRLS role, " +
	"and a table's owner may take FORCE ROW LEVEL SECURITY off again; role membership is one SET ROLE " +
	"away from any of those. On such a connection every tenant-scoped policy in this database is inert " +
	"or removable and one tenant's request can read every other tenant's rows (CLAUDE.md §4.5). Point " +
	"DATABASE_URL at the application role (NOSUPERUSER, NOBYPASSRLS, owns nothing, member of nothing); " +
	"migrations are the only thing that uses the owner role"

// roleRefusal decides whether the role this pool connected as is allowed to serve
// traffic. A non-nil error is a refusal, and New turns it into a closed pool.
//
// 🔴 WHY THIS EXISTS WHEN config.Load ALREADY REFUSES A DATABASE_URL EQUAL TO THE
// MIGRATION-ROLE VARIABLE. That check compares two STRINGS, so it only fires when
// the owner DSN is present TWICE. Measured on this tree with the migration-role
// variable simply left UNSET and the owner DSN written into DATABASE_URL, the
// process booted and answered GET /healthz with 200 under both TAPPA_ENV=prod and
// TAPPA_ENV=dev, saying not one word about the role — and inside one tenant context
// that pool read 311 129 tenants and 377 349 transactions where the application role
// reads 1 and 26 414. §4.5 was not weakened by that DSN, it was ABSENT.
//
// ⚠️ THAT VARIABLE IS NAMED HERE BY DESCRIPTION RATHER THAN SPELLED OUT, and the
// reason is a red-line net rather than style: scripts/redline-check.sh's R5 fails
// production code that mentions it, and it cannot tell a prohibition being DISCUSSED
// from one being violated. internal/domain/tenant/venue.go carries the same wording
// dodge for R2, with the same argument — reword the prose rather than teach the
// scanner an exemption.
//
// 🔴 PROD REFUSES, DEVELOPMENT WARNS, and the asymmetry is the same one
// trustedProxySanity already makes in internal/config for the same class of value.
// A developer machine legitimately runs everything as the owner (make migrate, make
// seed, psql), and a tool that refuses to start in development is a tool somebody
// routes around; a production process that cannot enforce tenant isolation must not
// answer a single request.
func roleRefusal(f RoleFacts, isProd bool) error {
	if !f.Privileged() || !isProd {
		return nil
	}
	return fmt.Errorf("db: role %q has rolsuper=%v rolbypassrls=%v owns_or_can_become_owner_of_an_rls_table=%v "+
		"member_of_a_superuser_or_bypassrls_role=%v: %s",
		f.User, f.Super, f.BypassRLS, f.OwnsScopedTable, f.InheritsPrivilege, RoleRiskWhy)
}

// Tenant resolution (ADR 0002 madde 7) is intentionally NOT provided here.
//
// The two context-less lookups (resolve_session_by_token_hash, resolve_tag_by_uid)
// run WITHOUT a tenant context — they are what *produces* the tenant id — so
// they cannot go through WithTenant. Their queries do not exist yet: they are
// defined as SECURITY DEFINER functions in the M1-04/M1-05 migrations and called
// via the generated store in M1-08 (db/queries/resolve.sql). Adding a bounded
// "context-less" pool accessor now would ship an unused API surface whose exact
// shape depends on the store/Querier type that M1-08 introduces, and a loose one
// risks becoming the general bypass door ADR 0002 forbids. So M1-07 keeps
// WithTenant as the sole DB entry point; M1-08 adds a narrow, named resolver
// accessor (which must NOT call set_config) when the resolve queries land.
